// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//
// The Nef Output Exception (see the LICENSE-EXCEPTION file at the repository
// root) applies to this file: code you write and the binaries you compile with
// it are not subject to the Apache License, and you have no obligation to
// open-source them.

// regex.go 实现 regular_expression 组的匹配机（语言定下的"最小可用"子集）。
//
// 形状与 internal/rtre 的参考实现逐字对齐：Thompson 的 NFA ＋ 一条按优先级排的线程表
// （Pike 的 VM 去掉捕获），不递归回溯——式子有多少状态就最多多少条线程，线性时间。
// 两处只为机器码改了写法，语义不动：
//
//   - **位置是字节偏移，每一步是一个码点**（按首字节解出它占几字节）。先把文本拆成码点
//     数组再匹配，等于多要两份与文本同长的表，而匹配本身只需要"这个位置是哪个码点、占几字节"。
//     每个记号还是吃一个码点，`.` 还是一个字符（含换行），切出来的区间必然落在码点边界上。
//   - **程序与线程表放静态槽位，只增不减**：一次调用先按式子的码点数算出上界，装不下就整块
//     换新（换新块时不必搬内容，这一刻还没有内容）。产物里没有第二个线程会同时操作这几块，
//     用户代码里也没有任何东西能在匹配途中回调回来，所以这一套静态复用是安全的。
//
// 一条指令占 56 字节、七个槽位：
//
//	+0 操作｜+8 参数（字面码点，或字符组取不取反）｜+16 out1｜+24 out2
//	+32 字符组头一段范围在哪｜+40 几段范围｜+48 这个位置进过没有
//
// 出边里存的是**那条指令的地址**而不是下标：省掉每一趟都要乘一次 56 的那一步。0 表示"这一侧
// 没接上"——堆地址不会是 0，用它当空值最省。字符组的范围是 op=9 的那些指令，只被 +32 那个槽位
// 指着，没有边通向它们，所以匹配时永远走不到。
//
// 贪婪与"`|` 先写的分支优先"都由 split 的出边顺序产生：先走"再吃一个"／左边那一支，走不通
// 才走"跳过"／右边那一支。遇到一条收尾指令（op=8）就封闭本轮的后半段——排在它后面的线程优先级
// 都比它低，不可能再改变结果；已经进表的都比它优先，让它们走完。这就是不需要回溯的理由。
//
// 这一份文件里的例程分两层：@rx_ensure／@rx_compile／@rx_at／@rx_scan 是内部例程，
// @regex_* 才是对外的四个入口。
//
// en: regex.go is the matcher for the regular_expression group (the "minimum usable" subset
// the language defines). Its shape aligns word for word with the internal/rtre reference
// implementation: Thompson's NFA plus a priority-ordered thread list (Pike's VM without
// captures), no recursive backtracking - at most as many threads as the pattern has states,
// linear time. Only two things were rewritten for machine code, semantics untouched: POSITIONS
// ARE BYTE OFFSETS and one step is one code point (its width decoded from the lead byte) -
// splitting the text into a code point array first would demand two extra tables as long as the
// text, while matching only ever needs "which code point is here and how many bytes does it
// take"; every token still consumes one code point, `.` is still one character (newline
// included), so any slice cut this way lands on a code point boundary. THE PROGRAM AND THREAD
// TABLES LIVE IN STATIC SLOTS AND ONLY GROW: an upper bound is computed per call from the
// pattern's code point count and, when it does not fit, the whole block is replaced (nothing
// needs moving, since at that instant there is no content). No second thread in the product
// touches these blocks and no user code can call back mid-match, so this static reuse is safe.
// One instruction occupies 56 bytes, seven slots: +0 opcode, +8 operand (a literal code point,
// or whether a class is negated), +16 out1, +24 out2, +32 the first range of a class, +40 how
// many ranges, +48 whether this position already entered the table. Out edges store THE ADDRESS
// of the target instruction, not an index, saving a multiply by 56 in every pass; 0 means "this
// side is not connected" - a heap address is never 0, so it is the cheapest null. A class's
// ranges are instructions with op=9 referenced only from +32, with no edges leading to them, so
// matching never walks into them. Greediness and "the branch written first wins" both come from
// the order of split's out edges: try eat-one-more/left first, and only if that fails
// skip/right. Reaching a finish instruction (op=8) closes the second half of this pass -
// everything behind it has lower priority and cannot change the result, while everything
// already in the table is more preferred and gets to finish. That is why no backtracking is
// needed. The routines in this file are two layers: @rx_ensure/@rx_compile/@rx_at/@rx_scan are
// internal routines, while @regex_* are the four public entry points.
package runtime

import "neflang.org/nf/internal/x64"

// 那十种操作与一条指令里七个槽位的位置。编号与 internal/rtre/compile.go 一致。
//
// en: The ten opcodes and the positions of the seven slots in an instruction. The numbering is
// shared with internal/rtre/compile.go.
const (
	rxRune  = 1 // 一个码点，等于 +8 那个槽位
	rxClass = 2 // 一个码点，落在 +32 起的那几段范围里（+8 非 0 就取反）
	rxAny   = 3 // 任意一个码点
	rxSplit = 4 // out1 优先于 out2
	rxJmp   = 5
	rxBol   = 6 // ^ ——只匹配文本开头，没有多行标志
	rxEol   = 7 // $ ——只匹配文本末尾
	rxMatch = 8
	rxRange = 9 // 字符组里的一段闭区间：+8 下界、+16 上界

	rxISz  = 56
	rxOp   = 0
	rxArg  = 8
	rxOut1 = 16
	rxOut2 = 24
	rxClLo = 32
	rxClN  = 40
	rxSeen = 48

	codeRx = 10071 // 式子写错
	tagRx  = 0x5258
)

// 一层嵌套占七个槽位：已累积的片段、待合并的左分支、刚消费的原子。
//
// en: One nesting level holds seven slots: the accumulated run, the left branch waiting to be
// merged, and the atom just consumed.
const (
	fPendS = 0
	fPendH = 8
	fAltS  = 16
	fAltH  = 24
	fLastS = 32
	fLastH = 40
	fLastQ = 48
	fSz    = 56
)

// regexData 定义六个静态槽位与 10071 号要用的报错文本。
//
// 静态槽位初值全 0：rxProgCap＝0 意味着首次调用一定要换新块；rxSeen 那些槽位靠每次匹配开头自己清。
//
// en: regexData is the six static slots and the sentences code 10071 needs. Static slots start
// all zero: rxProgCap = 0 forces the very first call to take a new block, and the rxSeen slots
// are cleared at the start of each match.
func regexData(put func(name string, bs []byte)) {
	put("rxProg", make([]byte, 8))    // 指令块
	put("rxProgCap", make([]byte, 8)) // 装得下几条指令
	put("rxLen", make([]byte, 8))     // 这次编出来的那些指令占几字节
	put("rxEntry", make([]byte, 8))   // 入口那条指令的地址
	put("rxScr", make([]byte, 8))     // 线程表：dfs 栈＋两条轮换的表
	put("rxPar", make([]byte, 8))     // 编译时的帧栈
	put("sRxEmpty", strObject("regular_expression 的式子是空的"))
	put("sRxBrace", strObject("regular_expression 这一版不支持 {m,n}：量词只有 *、+、?"))
	put("sRxGroup", strObject("regular_expression 这一版不支持 (?...)，也不支持空的 ()：命名组、标志、先行断言都没有"))
	put("sRxParen", strObject("regular_expression 的式子括号没配上"))
	put("sRxBracket", strObject("regular_expression 的字符组没有收尾的 ]"))
	put("sRxNegCls", strObject("regular_expression 的 \\D \\W \\S 放进字符组里没有一致的说法，写在组外头可以用"))
	put("sRxRange", strObject("regular_expression 的字符组范围反了：后界在前界前面"))
	put("sRxSlash", strObject("regular_expression 的式子以一个反斜杠收尾"))
	put("sRxQuant", strObject("regular_expression 的量词前面没有可以量的东西，或者那个东西已经挂过一个量词"))
	put("sRxBound", strObject(`regular_expression 这一版不支持 \b \B 这类词边界`))
	put("sRxRef", strObject(`regular_expression 这一版不支持反向引用（\1 那种）`))
	put("sRxTooBig", strObject("regular_expression 的式子太大，这一版装不下"))
}

// rxIn 说明 emit 的一个入参从哪儿来：一个常数，或者某一帧槽里的值。
//
// en: rxIn says where an emit position operand comes from: a constant, or the value in a given
// frame slot.
type rxIn struct {
	imm   int64
	slot  int
	isImm bool
}

func rxi(v int64) rxIn { return rxIn{imm: v, isImm: true} }
func rxs(n int) rxIn   { return rxIn{slot: n} }

// 那六个预定义类的范围表。`\d \w \s` 只覆盖 ASCII 那一段——非 ASCII 码点不属于任何预定义类，
// 要匹配它就写字面或 `.`。`\s` 那一段用 9～13 加空格，与 rtre 参考实现的六个字符一致。
//
// en: The range tables of the six predefined classes. `\d \w \s` cover the ASCII band only - a
// non-ASCII code point falls into no predefined class, so match it by literal or `.`.
// The `\s` band uses 9-13 plus space, matching the six characters of the rtre copy.
var (
	rxD = [][2]int64{{'0', '9'}}
	rxW = [][2]int64{{'0', '9'}, {'a', 'z'}, {'A', 'Z'}, {'_', '_'}}
	rxS = [][2]int64{{9, 13}, {' ', ' '}}
)

// @rx_ensure(rcx=要装得下几条指令)：三块公用的内存不够大就整块换新。
//
// 换新块不搬内容：所有调用者都从第一个位置重新往上添，老块这一刻还没有内容。只增不减，
// 与容器扩容、文本拼接同一规则。
//
// en: @rx_ensure (rcx = how many instructions must fit): replaces the whole block when the
// three shared areas are not big enough. Replacing moves nothing: every caller starts appending
// from the first position again, so the old block was not in use yet. It only grows and never
// shrinks - the same rule as container resizing and text concatenation.
func rxEnsure(b *rt) {
	const eNeed = 0
	a := b.a
	b.prologue()
	b.put(eNeed, x64.RCX)
	same := b.label()
	b.loadRIP(x64.RAX, "rxProgCap")
	b.get(x64.RCX, eNeed)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, same)

	// 指令块
	b.get(x64.RCX, eNeed)
	a.MovImm(x64.RDX, rxISz)
	a.Imul(x64.RCX, x64.RDX)
	a.MovImm(x64.RDX, tagRx)
	b.call("@alloc")
	b.storeRIP(x64.RAX, "rxProg")

	// 线程表：dfs 栈（2c＋4）＋cur（c＋2）＋next（c＋2）＝3c＋8 个槽位，按 4c＋12 要——
	// dfs 那一侧留一倍余量：同一个槽可能被压两次（进过的判断在取出来的那一刻才做，
	// 那一条顺序不能变，变了就成了"后到的分支优先"）
	b.get(x64.RCX, eNeed)
	a.MovImm(x64.RDX, 4)
	a.Imul(x64.RCX, x64.RDX)
	a.AddImm(x64.RCX, 12)
	a.MovImm(x64.RDX, 8)
	a.Imul(x64.RCX, x64.RDX)
	a.MovImm(x64.RDX, tagRx)
	b.call("@alloc")
	b.storeRIP(x64.RAX, "rxScr")

	// 编译时的帧栈：每层 56 字节，层数上界取 need／4——一个 `(` 至少占一个码点，
	// 而 need＝6m＋16（m 是式子的码点数），这个上界比 m＋2 还宽
	b.get(x64.RCX, eNeed)
	a.ShrImm(x64.RCX, 2)
	a.AddImm(x64.RCX, 8)
	a.MovImm(x64.RDX, fSz)
	a.Imul(x64.RCX, x64.RDX)
	a.MovImm(x64.RDX, tagRx)
	b.call("@alloc")
	b.storeRIP(x64.RAX, "rxPar")

	b.get(x64.RCX, eNeed)
	b.storeRIP(x64.RCX, "rxProgCap")
	a.Mark(same)
	b.epilogue()
}

// @rx_compile(rcx=式子文本)：编进静态那块指令块，完成后设置 rxEntry／rxLen。
// 式子非法当场抛 10071 号，措辞见 regexData。
//
// 编译**不走递归**：一层嵌套一帧，帧放在 rxPar 的栈区里，所以不占 CPU 栈，也不存在
// "式子嵌套过深把栈击穿"的问题。语法的形状与 rtre 参考实现一样：alt = concat（'|' concat）*，
// concat = piece*，piece = atom 量词?。两处实现上的分别：
//
//   - 片段只留**一个出口**（一个洞）。两个出口要合成一个的地方（`|`、`?`、`+`）就补一条 jmp
//     当汇合点，多花一条指令，换掉"洞是一张表"那一整套簿记。
//   - 量词挂在"刚消费的那个原子"上，而原子要向后看到量词才能定性，所以每层留一个 last 槽位：
//     读到下一个原子或结构记号时，才把上一个并进已累积的片段（lFlush）。
//
// 约定：那五条子程序（lEmit／lFlush／lEmpty／lMergeAlt／lAdd）可随意破坏寄存器，
// 活跃值一律在帧槽里。主循环的 cmp 链中间没有 call，所以 kCP 可以先取到 RSI 用完。
//
// en: @rx_compile (rcx = pattern text): compiles into the static instruction block and leaves
// rxEntry/rxLen set afterwards. A malformed pattern throws code 10071 on the spot; the wording
// is in regexData. Compilation DOES NOT RECURSE: one nesting level per frame, frames kept in
// the rxPar stack, so it uses no CPU stack and a deeply nested pattern cannot run the stack
// through. The grammar shape is rtre's: alt = concat ('|' concat)*, concat = piece*,
// piece = atom quantifier?. Two implementation differences: a fragment keeps ONE EXIT (one
// hole), and where two exits must be merged (`|`, `?`, `+`) a jmp is emitted as the join - one
// extra instruction in exchange for dropping the whole "the hole is a table" bookkeeping; and a
// quantifier attaches to "the atom just consumed" while an atom must look ahead to the
// quantifier to be classified, so each level keeps a last slot: the previous atom is merged
// into the accumulated run (lFlush) only when the next atom or structural token is read. Rule:
// the five subroutines (lEmit/lFlush/lEmpty/lMergeAlt/lAdd) may clobber registers freely, and
// every live value of the processor sits in a frame slot. The cmp chain in the main loop
// contains no call, so kCP may be fetched into RSI and used up.
func rxCompile(b *rt) {
	const (
		kPat   = iota
		kData  // 式子的字节起点
		kN     // 几字节
		kI     // 游标：字节下标
		kCur   // 下一条指令的地址
		kEnd   // 指令块末尾
		kFrame // 当前这一层的帧
		kDepth // 第几层
		kCP    // 刚解出来的那个码点
		kFragS // 临时：一个片段的起点
		kFragH // 临时：它的出口
		kTmp1
		kTmp2
		kEOp // emit 的六格入参，连着摆
		kEArg
		kEOut1
		kEOut2
		kEClLo
		kEClN
		kClLo  // 字符组：头一段范围在哪
		kClN   // 字符组：已经攒了几段
		kNeg   // 字符组：取不取反
		kFirst // 字符组：是否仍在第一位（这里的 ] 属于内容本身）
	)
	a := b.a
	b.prologue()
	b.put(kPat, x64.RCX)

	lEmit := b.label() // 下面那几个闭包都要 call 它，所以它得先有一个确定的标号

	// kon／inc：往帧槽写一个常数、帧槽加一个常数。后面大量依赖它们。
	kon := func(slot int, v int64) {
		a.MovImm(x64.RAX, v)
		b.put(slot, x64.RAX)
	}
	inc := func(slot int, v int64) {
		b.get(x64.RAX, slot)
		a.AddImm(x64.RAX, int32(v))
		b.put(slot, x64.RAX)
	}
	// prep 摆好 emit 的六格入参。
	prep := func(op int64, arg, out1, out2, clLo, clN rxIn) {
		for i, v := range [6]rxIn{{imm: op, isImm: true}, arg, out1, out2, clLo, clN} {
			if v.isImm {
				a.MovImm(x64.RAX, v.imm)
			} else {
				b.get(x64.RAX, v.slot)
			}
			b.put(kEOp+i, x64.RAX)
		}
	}
	// blank 发一条其余入参全为 0 的指令（jmp／any／锚／match）：六个入参里只有操作码不是 0。
	// 它要在 kEOut1／kEOut2 已经存好之后再用，否则会把那两个槽清掉。
	blank := func(op int64) {
		prep(op, rxi(0), rxi(0), rxi(0), rxi(0), rxi(0))
		a.Call(lEmit)
	}
	// clsOnly 发一条字符组：范围在 [kClLo, +kClN)，是否取反看 kNeg。
	clsOnly := func() {
		prep(rxClass, rxs(kNeg), rxi(0), rxi(0), rxs(kClLo), rxs(kClN))
		a.Call(lEmit)
	}
	// decode 解游标处的一个码点，游标前移一个码点。码点进 kCP。
	decode := func() {
		b.get(x64.RAX, kData)
		b.get(x64.RCX, kI)
		a.Add(x64.RAX, x64.RCX)
		decodeRune(b)
		b.put(kCP, x64.RSI)
		b.get(x64.RAX, kI)
		a.Add(x64.RAX, x64.RDI)
		b.put(kI, x64.RAX)
	}
	// peek 把游标处那一个**字节**放进 RAX。只用来识别 `)`、`?`、`-`、`]`、`^` 这几个 ASCII 记号：
	// 它们的字节值不会出现在多字节字符的任何一个字节里（UTF-8 的续字节都 ≥ 0x80）。
	peek := func() {
		b.get(x64.RAX, kData)
		b.get(x64.RCX, kI)
		a.Add(x64.RAX, x64.RCX)
		a.MovZX8(x64.RAX, x64.RAX, 0)
	}
	// rest 检查游标之后是否还有内容：没有就跳到 at。
	rest := func(at x64.Label) {
		b.get(x64.RAX, kI)
		b.get(x64.RCX, kN)
		a.Cmp(x64.RAX, x64.RCX)
		a.Jcc(x64.AE, at)
	}
	// setNeg 把 kNeg 置为 v（识别 \D \W \S 那一大写的一支）
	setNeg := func(v int64) { kon(kNeg, v) }

	// 标号先声明齐：处理分支之间有前向跳转。
	lTop := b.label()
	lFlush := b.label()
	lFlushRet := b.label()
	lFlushChain := b.label()
	lFlushClear := b.label()
	lEmpty := b.label()
	lMergeAlt := b.label()
	lMergeGo := b.label()
	lMergeRet := b.label()
	lLeafTail := b.label() // rax＝刚发出去那条的地址：挂成 last 再回主循环
	lSetLast := b.label()  // 把 kFragS／kFragH 那一个片段挂成 last
	lRune := b.label()
	lAny := b.label()
	lBol := b.label()
	lEol := b.label()
	lParen := b.label()
	lRParen := b.label()
	lPendOK := b.label()
	lBar := b.label()
	lAltOK := b.label()
	lStar := b.label()
	lPlus := b.label()
	lMay := b.label()
	lEsc := b.label()
	lEscRef := b.label()
	lCls := b.label()
	lClsCare := b.label()
	lClsTop := b.label()
	lClsNotClose := b.label()
	lClsLo := b.label()
	lClsOne := b.label()
	lClsRange := b.label()
	lClsAdd := b.label()
	lClsDone := b.label()
	lClsEmit := b.label()
	lClsBS := b.label()
	lEscD, lEscW, lEscS := b.label(), b.label(), b.label()
	lInD, lInW, lInS := b.label(), b.label(), b.label()
	lDone := b.label()
	lDoneGo := b.label()

	// bad 留一个"抛 10071 号"的落点：那几段指令统一放在主流程后面，不夹在处理分支中间。
	//
	// en: bad leaves one landing point for "throw code 10071": those runs of instructions are
	// emitted after the main flow rather than wedged between the handlers.
	var badLbl []x64.Label
	var badMsg []string
	bad := func(name string) x64.Label {
		l := b.label()
		badLbl = append(badLbl, l)
		badMsg = append(badMsg, name)
		return l
	}

	// ---- 准备：量一下式子、换一块、摆好游标与第 0 层 ----
	b.get(x64.RAX, kPat)
	a.MovLoad(x64.RCX, x64.RAX, StrBytes)
	b.put(kN, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	b.put(kData, x64.RAX)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, bad("sRxEmpty"))
	// 上界：一个式子记号最多贡献六条指令。占最多的是 `\s`（两个码点要七条指令：一条字符组加
	// 六段范围），系数 6 是照着它定的；真超了 lEmit 的边界检查会当场抛错，不会越界写到别处。
	b.get(x64.RAX, kPat)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	a.MovImm(x64.RCX, 6)
	a.Imul(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, 16)
	a.Mov(x64.RCX, x64.RAX)
	b.call("@rx_ensure")
	b.loadRIP(x64.RAX, "rxProg")
	b.put(kCur, x64.RAX)
	b.put(kTmp1, x64.RAX)
	b.loadRIP(x64.RAX, "rxProgCap")
	a.MovImm(x64.RCX, rxISz)
	a.Imul(x64.RAX, x64.RCX)
	b.get(x64.RCX, kTmp1)
	a.Add(x64.RAX, x64.RCX)
	b.put(kEnd, x64.RAX)
	b.loadRIP(x64.RAX, "rxPar")
	b.put(kFrame, x64.RAX)
	kon(kDepth, 0)
	kon(kI, 0)
	b.get(x64.RCX, kFrame)
	a.Xor(x64.RAX, x64.RAX)
	for _, off := range [7]int32{fPendS, fPendH, fAltS, fAltH, fLastS, fLastH, fLastQ} {
		a.MovStore(x64.RCX, off, x64.RAX)
	}

	// ---- 主循环：识别一个记号 ----
	a.Mark(lTop)
	rest(lDone)
	decode()
	b.get(x64.RSI, kCP)
	for _, c := range []struct {
		ch rune
		l  x64.Label
	}{
		{'(', lParen}, {')', lRParen}, {'|', lBar}, {'*', lStar}, {'+', lPlus}, {'?', lMay},
		{'[', lCls}, {'\\', lEsc}, {'.', lAny}, {'^', lBol}, {'$', lEol},
	} {
		a.CmpImm(x64.RSI, int32(c.ch))
		a.Jcc(x64.E, c.l)
	}
	a.CmpImm(x64.RSI, '{')
	a.Jcc(x64.E, bad("sRxBrace"))
	a.Jmp(lRune)

	// ---- 一个字面码点：kCP 就是它 ----
	a.Mark(lRune)
	a.Call(lFlush)
	prep(rxRune, rxs(kCP), rxi(0), rxi(0), rxi(0), rxi(0))
	a.Call(lEmit)
	a.Jmp(lLeafTail)

	// ---- `.`：任意一个码点，含换行 ----
	a.Mark(lAny)
	a.Call(lFlush)
	blank(rxAny)
	a.Jmp(lLeafTail)

	// ---- 锚：只匹配整条文本的首尾 ----
	a.Mark(lBol)
	a.Call(lFlush)
	blank(rxBol)
	a.Jmp(lLeafTail)
	a.Mark(lEol)
	a.Call(lFlush)
	blank(rxEol)
	a.Jmp(lLeafTail)

	// ---- `(`：开一层。新层清零，老层原地留着——退层只需把帧指针挪回去 ----
	a.Mark(lParen)
	a.Call(lFlush)
	rest(bad("sRxParen"))
	peek()
	a.CmpImm(x64.RAX, ')')
	a.Jcc(x64.E, bad("sRxGroup"))
	a.CmpImm(x64.RAX, '?')
	a.Jcc(x64.E, bad("sRxGroup"))
	b.get(x64.RCX, kFrame)
	a.AddImm(x64.RCX, fSz)
	b.put(kFrame, x64.RCX)
	a.Xor(x64.RAX, x64.RAX)
	for _, off := range [7]int32{fPendS, fPendH, fAltS, fAltH, fLastS, fLastH, fLastQ} {
		a.MovStore(x64.RCX, off, x64.RAX)
	}
	inc(kDepth, 1)
	a.Jmp(lTop)

	// ---- `)`：这一层收完，它的片段成为上一层的 last ----
	a.Mark(lRParen)
	a.Call(lFlush)
	a.Call(lMergeAlt)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fPendS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lPendOK)
	a.Call(lEmpty) // `(|x)`、`(a|)` 那种空的一支
	a.Mark(lPendOK)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fPendS)
	b.put(kFragS, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, fPendH)
	b.put(kFragH, x64.RAX)
	a.AddImm(x64.RCX, -fSz)
	b.put(kFrame, x64.RCX)
	inc(kDepth, -1)
	b.get(x64.RAX, kDepth)
	a.CmpImm(x64.RAX, 0)
	a.Jcc(x64.L, bad("sRxParen")) // 多出来的一只 )
	a.Jmp(lSetLast)

	// ---- `|`：这一段结束，与左支合成为一支，合成结果等待右边的分支 ----
	a.Mark(lBar)
	a.Call(lFlush)
	a.Call(lMergeAlt)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fPendS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lAltOK)
	a.Call(lEmpty)
	a.Mark(lAltOK)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RDX, x64.RCX, fPendS)
	a.MovStore(x64.RCX, fAltS, x64.RDX)
	a.MovLoad(x64.RDX, x64.RCX, fPendH)
	a.MovStore(x64.RCX, fAltH, x64.RDX)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RCX, fPendS, x64.RDX)
	a.MovStore(x64.RCX, fPendH, x64.RDX)
	a.Jmp(lTop)

	// ---- 量词三条。都先拒绝"前面没有可量的东西"与"同一个原子挂两个量词"（a**、a*? 都在这一道） ----
	quantHead := func() {
		b.get(x64.RCX, kFrame)
		a.MovLoad(x64.RAX, x64.RCX, fLastS)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, bad("sRxQuant"))
		a.MovLoad(x64.RAX, x64.RCX, fLastQ)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.NE, bad("sRxQuant"))
		a.MovImm(x64.RAX, 1)
		a.MovStore(x64.RCX, fLastQ, x64.RAX)
	}
	splitToLast := func() { // split 的 out1＝last 的起点，其余入参清零
		b.get(x64.RCX, kFrame)
		a.MovLoad(x64.RAX, x64.RCX, fLastS)
		b.put(kEOut1, x64.RAX)
		kon(kEOp, rxSplit)
		kon(kEArg, 0)
		kon(kEOut2, 0)
		kon(kEClLo, 0)
		kon(kEClN, 0)
		a.Call(lEmit)
	}
	a.Mark(lStar)
	quantHead()
	// sp = split(out1＝e 的起点)；e 的出口接回 sp；这一支的洞换成 sp.out2
	splitToLast()
	b.put(kTmp1, x64.RAX)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RDX, x64.RCX, fLastH)
	b.get(x64.RAX, kTmp1)
	a.MovStore(x64.RDX, 0, x64.RAX)
	a.MovStore(x64.RCX, fLastS, x64.RAX)
	a.AddImm(x64.RAX, rxOut2)
	a.MovStore(x64.RCX, fLastH, x64.RAX)
	a.Jmp(lTop)

	a.Mark(lPlus)
	quantHead()
	// J = jmp；sp = split(out1＝e 的起点)；J.out1 → sp；e 的出口 → J；洞 = sp.out2
	blank(rxJmp)
	b.put(kTmp1, x64.RAX) // J
	splitToLast()
	b.put(kTmp2, x64.RAX) // sp
	b.get(x64.RAX, kTmp1)
	b.get(x64.RDX, kTmp2)
	a.MovStore(x64.RAX, rxOut1, x64.RDX)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.R8, x64.RCX, fLastH)
	a.MovStore(x64.R8, 0, x64.RAX) // e 的出口 → J
	b.get(x64.RDX, kTmp2)
	a.AddImm(x64.RDX, rxOut2)
	a.MovStore(x64.RCX, fLastH, x64.RDX)
	a.Jmp(lTop)

	a.Mark(lMay)
	quantHead()
	// sp = split(out1＝e 的起点)；J = jmp；sp.out2 → J；e 的出口 → J；这一支 = {sp, J.out1}
	splitToLast()
	b.put(kTmp1, x64.RAX) // sp
	blank(rxJmp)
	b.put(kTmp2, x64.RAX) // J
	b.get(x64.RAX, kTmp1)
	b.get(x64.RDX, kTmp2)
	a.MovStore(x64.RAX, rxOut2, x64.RDX)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.R8, x64.RCX, fLastH)
	a.MovStore(x64.R8, 0, x64.RDX) // e 的出口 → J
	a.MovStore(x64.RCX, fLastS, x64.RAX)
	a.AddImm(x64.RDX, rxOut1)
	a.MovStore(x64.RCX, fLastH, x64.RDX)
	a.Jmp(lTop)

	// ---- `\`：那六个类、两处明确不支持的记号，其余一律按字面 ----
	a.Mark(lEsc)
	rest(bad("sRxSlash"))
	decode()
	b.get(x64.RSI, kCP)
	for _, c := range []struct {
		ch  rune
		neg int64
		tgt x64.Label
	}{
		{'d', 0, lEscD}, {'w', 0, lEscW}, {'s', 0, lEscS},
		{'D', 1, lEscD}, {'W', 1, lEscW}, {'S', 1, lEscS},
	} {
		a.CmpImm(x64.RSI, int32(c.ch))
		yes := b.label()
		a.Jcc(x64.NE, yes)
		setNeg(c.neg)
		a.Jmp(c.tgt)
		a.Mark(yes)
	}
	for _, c := range []struct {
		ch rune
		l  x64.Label
	}{{'b', bad("sRxBound")}, {'B', bad("sRxBound")}} {
		a.CmpImm(x64.RSI, int32(c.ch))
		a.Jcc(x64.E, c.l)
	}
	a.CmpImm(x64.RSI, '1')
	a.Jcc(x64.B, lRune) // 比 '1' 小：不是数字，按字面走
	a.CmpImm(x64.RSI, '9')
	a.Jcc(x64.A, lRune)
	a.Jmp(lEscRef)
	a.Mark(lEscRef)
	b.throwWith(codeRx, "sRxRef")

	// ---- 组外的三个类：范围＋一条字符组 ----
	wholeClass := func(l x64.Label, tabs [][2]int64) {
		a.Mark(l)
		a.Call(lFlush)
		b.get(x64.RAX, kCur)
		b.put(kClLo, x64.RAX)
		for _, t := range tabs {
			prep(rxRange, rxi(t[0]), rxi(t[1]), rxi(0), rxi(0), rxi(0))
			a.Call(lEmit)
		}
		kon(kClN, int64(len(tabs)))
		a.Jmp(lClsEmit)
	}
	wholeClass(lEscD, rxD)
	wholeClass(lEscW, rxW)
	wholeClass(lEscS, rxS)

	// ---- 字符组 `[...]` ----
	a.Mark(lCls)
	a.Call(lFlush)
	kon(kNeg, 0)
	kon(kClN, 0)
	kon(kFirst, 1)
	b.get(x64.RAX, kCur)
	b.put(kClLo, x64.RAX) // 头一段范围就落在这儿
	rest(bad("sRxBracket"))
	peek()
	a.CmpImm(x64.RAX, '^')
	noNeg := b.label()
	a.Jcc(x64.NE, noNeg)
	inc(kI, 1)
	kon(kNeg, 1)
	a.Mark(noNeg)
	a.Jmp(lClsTop)

	a.Mark(lClsTop)
	rest(bad("sRxBracket"))
	decode()
	b.get(x64.RSI, kCP)
	a.CmpImm(x64.RSI, ']')
	a.Jcc(x64.NE, lClsNotClose)
	b.get(x64.RAX, kFirst)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lClsDone) // 不在第一位的 ]：收尾
	a.Mark(lClsNotClose)
	kon(kFirst, 0)
	b.get(x64.RSI, kCP)
	a.CmpImm(x64.RSI, '\\')
	a.Jcc(x64.NE, lClsLo)

	// ---- 组里的反斜杠 ----
	a.Mark(lClsCare)
	rest(bad("sRxSlash"))
	decode()
	b.get(x64.RSI, kCP)
	for _, c := range []struct {
		ch rune
		l  x64.Label
	}{{'d', lInD}, {'w', lInW}, {'s', lInS}} {
		a.CmpImm(x64.RSI, int32(c.ch))
		a.Jcc(x64.E, c.l)
	}
	for _, c := range []struct {
		ch rune
		l  x64.Label
	}{{'D', bad("sRxNegCls")}, {'W', bad("sRxNegCls")}, {'S', bad("sRxNegCls")},
		{'B', bad("sRxBound")}, {'b', lClsBS}} {
		a.CmpImm(x64.RSI, int32(c.ch))
		a.Jcc(x64.E, c.l)
	}
	a.Jmp(lClsLo) // \\、\.、\] 那一种：反斜杠后面那一个字符本身
	a.Mark(lClsBS)
	kon(kCP, 8) // \b 在组内表示退格控制字符
	a.Jmp(lClsLo)

	// 组里的 \d \w \s：只添范围，不发新的字符组（kClLo 在头一段之前就已经摆好）
	inClass := func(l x64.Label, tabs [][2]int64) {
		a.Mark(l)
		for _, t := range tabs {
			prep(rxRange, rxi(t[0]), rxi(t[1]), rxi(0), rxi(0), rxi(0))
			a.Call(lEmit)
		}
		inc(kClN, int64(len(tabs)))
		a.Jmp(lClsTop)
	}
	inClass(lInD, rxD)
	inClass(lInW, rxW)
	inClass(lInS, rxS)

	// ---- kCP 是这一段范围的下界，检查紧跟的是否构成一段范围 ----
	a.Mark(lClsLo)
	b.get(x64.RAX, kCP)
	b.put(kTmp1, x64.RAX) // 下界先存着：解上界要冲掉 kCP
	rest(lClsOne)
	peek()
	a.CmpImm(x64.RAX, '-')
	a.Jcc(x64.NE, lClsOne)
	b.get(x64.RAX, kI)
	a.AddImm(x64.RAX, 1)
	b.get(x64.RCX, kN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, lClsOne) // '-' 后面什么都不剩：它按字面算它自己
	b.get(x64.RAX, kData)
	b.get(x64.RCX, kI)
	a.Add(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, 1)
	a.MovZX8(x64.RAX, x64.RAX, 0)
	a.CmpImm(x64.RAX, ']')
	a.Jcc(x64.E, lClsOne) // `[a-]`：末尾的 '-' 按字面算它自己
	inc(kI, 1)
	rest(bad("sRxSlash"))
	decode()
	b.get(x64.RSI, kCP)
	a.CmpImm(x64.RSI, '\\')
	a.Jcc(x64.NE, lClsRange)
	rest(bad("sRxSlash"))
	decode() // `[a-\]]`：上界也可以是从反斜杠来的那一个字符
	a.Mark(lClsRange)
	b.get(x64.RSI, kCP)
	b.get(x64.RDX, kTmp1)
	a.Cmp(x64.RSI, x64.RDX)
	a.Jcc(x64.L, bad("sRxRange"))
	b.get(x64.RAX, kCP)
	b.put(kTmp2, x64.RAX)
	a.Jmp(lClsAdd)

	a.Mark(lClsOne)
	b.get(x64.RAX, kCP)
	b.put(kTmp2, x64.RAX)
	a.Mark(lClsAdd)
	prep(rxRange, rxs(kTmp1), rxs(kTmp2), rxi(0), rxi(0), rxi(0))
	a.Call(lEmit)
	inc(kClN, 1)
	a.Jmp(lClsTop)

	a.Mark(lClsDone)
	a.Mark(lClsEmit) // 收尾那条字符组：范围已经在 [kClLo, +kClN) 那几个槽里
	clsOnly()
	a.Jmp(lLeafTail)

	// ---- 收尾：最外层的片段与待合并的左支合成为一支，出口接到 iMatch ----
	a.Mark(lDone)
	b.get(x64.RAX, kDepth)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, bad("sRxParen")) // `(abc` 这类没闭合的
	a.Call(lFlush)
	a.Call(lMergeAlt)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fPendS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lDoneGo)
	a.Call(lEmpty)
	a.Mark(lDoneGo)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RDX, x64.RCX, fPendS)
	b.put(kTmp1, x64.RDX)
	a.MovLoad(x64.RDX, x64.RCX, fPendH)
	b.put(kTmp2, x64.RDX)
	blank(rxMatch) // 收尾指令：发出时是最新一个槽位，位置在片段出口之后
	b.get(x64.RDX, kTmp2)
	a.MovStore(x64.RDX, 0, x64.RAX)
	b.get(x64.RAX, kTmp1)
	b.storeRIP(x64.RAX, "rxEntry")
	b.get(x64.RAX, kCur)
	b.loadRIP(x64.RCX, "rxProg")
	a.Sub(x64.RAX, x64.RCX)
	b.storeRIP(x64.RAX, "rxLen")
	b.epilogue()

	// ---- last 的两个槽：叶子刚发出、组刚收完，都从这里挂上 ----
	a.Mark(lLeafTail)
	b.put(kFragS, x64.RAX)
	a.AddImm(x64.RAX, rxOut1)
	b.put(kFragH, x64.RAX)
	a.Mark(lSetLast)
	b.get(x64.RAX, kFragS)
	b.get(x64.RCX, kFrame)
	a.MovStore(x64.RCX, fLastS, x64.RAX)
	b.get(x64.RDX, kFragH)
	a.MovStore(x64.RCX, fLastH, x64.RDX)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RCX, fLastQ, x64.RDX)
	a.Jmp(lTop)

	// ---- 五条子程序 ----
	a.Mark(lEmit)
	// 往 [kCur] 发一条指令：六个入参从 kEOp.. 那六个槽取，返回 rax＝这条指令的地址。
	b.get(x64.RAX, kCur)
	b.get(x64.RCX, kEnd)
	a.Mov(x64.RDX, x64.RAX)
	a.AddImm(x64.RDX, rxISz)
	a.Cmp(x64.RDX, x64.RCX)
	a.Jcc(x64.A, bad("sRxTooBig"))
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RAX, rxSeen, x64.RDX)
	for i, off := range [6]int32{rxOp, rxArg, rxOut1, rxOut2, rxClLo, rxClN} {
		b.get(x64.RCX, kEOp+i)
		a.MovStore(x64.RAX, off, x64.RCX)
	}
	// 游标前移一个位置，RAX 留着是这条指令的地址——mov 到内存不改寄存器，所以不必另存一份
	a.Mov(x64.RCX, x64.RAX)
	a.AddImm(x64.RCX, rxISz)
	b.put(kCur, x64.RCX)
	a.Ret()

	a.Mark(lFlush)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fLastS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lFlushRet)
	a.MovLoad(x64.RDX, x64.RCX, fPendS)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, lFlushChain)
	a.MovStore(x64.RCX, fPendS, x64.RAX)
	a.MovLoad(x64.RDX, x64.RCX, fLastH)
	a.MovStore(x64.RCX, fPendH, x64.RDX)
	a.Jmp(lFlushClear)
	a.Mark(lFlushChain)
	a.MovLoad(x64.RDX, x64.RCX, fPendH)
	a.MovStore(x64.RDX, 0, x64.RAX) // 上一个出口接过来
	a.MovLoad(x64.RDX, x64.RCX, fLastH)
	a.MovStore(x64.RCX, fPendH, x64.RDX)
	a.Mark(lFlushClear)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RCX, fLastS, x64.RDX)
	a.MovStore(x64.RCX, fLastH, x64.RDX)
	a.Mark(lFlushRet)
	a.Ret()

	a.Mark(lEmpty)
	// 什么都不吃的一段：一条 jmp，它的 out1 就是这一段的出口。
	blank(rxJmp)
	b.get(x64.RCX, kFrame)
	a.MovStore(x64.RCX, fPendS, x64.RAX)
	a.AddImm(x64.RAX, rxOut1)
	a.MovStore(x64.RCX, fPendH, x64.RAX)
	a.Ret()

	a.Mark(lMergeAlt)
	// 待合并的左支与累积那一段合成一支：split(out1＝左, out2＝右)，两条尾巴都跳到一条汇合
	// 用的 jmp，那条 jmp 的 out1 是这一支的出口。没遇到过 `|` 时它空转。
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fAltS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lMergeRet)
	a.MovLoad(x64.RAX, x64.RCX, fPendS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lMergeGo)
	a.Call(lEmpty)
	a.Mark(lMergeGo)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RAX, x64.RCX, fAltS)
	b.put(kEOut1, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, fPendS)
	b.put(kEOut2, x64.RAX)
	kon(kEOp, rxSplit)
	kon(kEArg, 0)
	kon(kEClLo, 0)
	kon(kEClN, 0)
	a.Call(lEmit)
	b.put(kTmp1, x64.RAX)
	blank(rxJmp)
	b.put(kTmp2, x64.RAX)
	b.get(x64.R8, kTmp2)
	b.get(x64.RCX, kFrame)
	a.MovLoad(x64.RDX, x64.RCX, fAltH)
	a.MovStore(x64.RDX, 0, x64.R8)
	a.MovLoad(x64.RDX, x64.RCX, fPendH)
	a.MovStore(x64.RDX, 0, x64.R8)
	b.get(x64.RDX, kTmp1)
	a.MovStore(x64.RCX, fPendS, x64.RDX)
	a.AddImm(x64.R8, rxOut1)
	a.MovStore(x64.RCX, fPendH, x64.R8)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RCX, fAltS, x64.RDX)
	a.MovStore(x64.RCX, fAltH, x64.RDX)
	a.Mark(lMergeRet)
	a.Ret()

	// ---- 十二处 10071 号的抛出点 ----
	for i, l := range badLbl {
		a.Mark(l)
		b.throwWith(codeRx, badMsg[i])
	}
}

// @rx_at(rcx=从第几字节起, rdx=文本)：从这个位置起试一次，返回 rax=是否匹配、
// rdx=结束在第几字节（空匹配也算数：那时 rdx＝rcx）。
//
// 式子从静态槽位 rxEntry 读取，所以必须在 @rx_compile 之后 call。
//
// 线程表每前移一个位置重排一次：cur／next 两条表轮换，dfs 栈走"不吃字符的边"（split、jmp、
// 锚）——同一个槽进入过就不再进入（指令里 +48 那个位置），每次匹配开头统一清一遍（块不大，
// 比每趟乘一次代号便宜，也不会有"代号回绕到 0"的问题）。
// 遇到收尾指令就封闭本轮：已在表内的线程优先级都更高，让它们走完；后面未走的都不许再进表。
//
// en: @rx_at (rcx = byte offset, rdx = text): try once from that position, returning rax =
// whether it matched and rdx = where it ended (an empty match counts: rdx = rcx). The pattern
// is read from the static slot rxEntry, so this must be called after @rx_compile. The thread
// list is re-laid one position at a time: cur/next alternate, and the dfs stack walks the edges
// that consume nothing (split, jmp, anchors) - a slot that already entered does not enter again
// (the +48 position in the instruction), cleared wholesale at the start of each match (the
// block is small, which is cheaper than multiplying by an id every pass and avoids
// id-wrap-around bookkeeping). Reaching a finish instruction closes the rest of this pass:
// everything already in the table is more preferred and gets to run, everything not yet walked
// may no longer enter.
func rxAt(b *rt) {
	const (
		aText = iota
		aData
		aN
		aPos  // 正在吃的那个位置
		aAPos // 这一趟闭包所在的位置（锚看它，不看 aPos）
		aDfs
		aTop
		aCur
		aCurN
		aNext
		aNextN
		aList // lAdd 的落点：往哪条表里添
		aListN
		aPC // lAdd 的入参
		aEnd
		aFound
		aSealed
		aRune // 这个位置解出的码点
		aW    // 它占几字节
		aWalk
		aStop
		aProg
		aPEnd
	)
	a := b.a
	b.prologue()
	b.put(aPos, x64.RCX)
	b.put(aText, x64.RDX)

	kon := func(slot int, v int64) {
		a.MovImm(x64.RAX, v)
		b.put(slot, x64.RAX)
	}
	lAdd := b.label()
	lAddTop := b.label()
	lAddRet := b.label()
	lAddSealed := b.label()
	lSplit := b.label()
	lJmp := b.label()
	lBol := b.label()
	lEol := b.label()
	lMatch := b.label()
	lLeaf := b.label()
	lTop := b.label()
	lStep := b.label()
	lStepDone := b.label()
	lOk := b.label()
	lTryCls := b.label()
	lClsTop := b.label()
	lClsNext := b.label()
	lClsDone := b.label()
	lClsNeg := b.label()
	lOut := b.label()

	// push 沿一条出边入栈：0 是"这头没接上"，直接跳过。
	push := func(base int, off int32) {
		if off == 0 {
			a.Mov(x64.RDX, base)
		} else {
			a.MovLoad(x64.RDX, base, off)
		}
		yes := b.label()
		a.Test(x64.RDX, x64.RDX)
		a.Jcc(x64.E, yes)
		b.get(x64.RAX, aTop)
		b.get(x64.R8, aDfs)
		a.MovImm(x64.R9, 8)
		a.Imul(x64.RAX, x64.R9)
		a.Add(x64.RAX, x64.R8)
		a.MovStore(x64.RAX, 0, x64.RDX)
		b.get(x64.RAX, aTop)
		a.AddImm(x64.RAX, 1)
		b.put(aTop, x64.RAX)
		a.Mark(yes)
	}

	// 文本的两个槽
	b.get(x64.RAX, aText)
	a.MovLoad(x64.RCX, x64.RAX, StrBytes)
	b.put(aN, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	b.put(aData, x64.RAX)

	// 清"这个位置进过没有"：每一次沿"不吃字符的边"走之前都要清一遍（与 rtre 参考实现
	// 每格换一次代号同一个意思）。不清就等于"a+ 的第二轮进不去"——同一个槽在两个位置上都要能进表。
	lClear := b.label()
	a.Call(lClear)

	// 线程表三块：dfs(2c＋4)｜cur(c＋2)｜next(c＋2)
	b.loadRIP(x64.RAX, "rxScr")
	b.put(aDfs, x64.RAX)
	b.loadRIP(x64.RDX, "rxProgCap")
	a.MovImm(x64.R8, 2)
	a.Imul(x64.RDX, x64.R8)
	a.AddImm(x64.RDX, 4)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RDX, x64.R8)
	b.get(x64.RAX, aDfs)
	a.Add(x64.RAX, x64.RDX)
	b.put(aCur, x64.RAX)
	b.loadRIP(x64.RDX, "rxProgCap")
	a.AddImm(x64.RDX, 2)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RDX, x64.R8)
	a.Add(x64.RAX, x64.RDX)
	b.put(aNext, x64.RAX)

	kon(aFound, 0)
	kon(aEnd, 0)
	kon(aSealed, 0)
	kon(aTop, 0)
	kon(aCurN, 0)
	b.get(x64.RAX, aPos)
	b.put(aAPos, x64.RAX)
	b.get(x64.RAX, aCur)
	b.put(aList, x64.RAX)
	kon(aListN, 0)
	b.loadRIP(x64.RAX, "rxEntry")
	b.put(aPC, x64.RAX)
	a.Call(lAdd)
	b.get(x64.RAX, aListN) // 起步这一轮闭包添了几条，cur 就是几条
	b.put(aCurN, x64.RAX)
	a.Jmp(lTop)

	// ---- 一个位置一个位置往前走 ----
	a.Mark(lTop)
	b.get(x64.RAX, aCurN)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lOut)
	b.get(x64.RAX, aPos)
	b.get(x64.RCX, aN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, lOut)
	a.Call(lClear) // 每个槽重开一轮闭包：进过的标记到这里就该作废
	b.get(x64.RAX, aData)
	b.get(x64.RCX, aPos)
	a.Add(x64.RAX, x64.RCX)
	decodeRune(b) // rsi＝码点，rdi＝几字节
	b.put(aRune, x64.RSI)
	b.put(aW, x64.RDI)
	kon(aNextN, 0)
	kon(aSealed, 0)
	b.get(x64.RAX, aCur)
	b.put(aWalk, x64.RAX)
	b.get(x64.RDX, aCurN)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RDX, x64.R8)
	a.Add(x64.RAX, x64.RDX)
	b.put(aStop, x64.RAX)
	b.get(x64.RAX, aPos)
	b.get(x64.RCX, aW)
	a.Add(x64.RAX, x64.RCX)
	b.put(aAPos, x64.RAX)

	a.Mark(lStep)
	b.get(x64.RAX, aWalk)
	b.get(x64.RCX, aStop)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, lStepDone)
	a.MovLoad(x64.RCX, x64.RAX, 0) // 这一条线程停在哪条指令上
	a.AddImm(x64.RAX, 8)
	b.put(aWalk, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, rxOp)
	a.CmpImm(x64.RAX, rxAny)
	a.Jcc(x64.E, lOk)
	a.CmpImm(x64.RAX, rxRune)
	a.Jcc(x64.NE, lTryCls)
	a.MovLoad(x64.RDX, x64.RCX, rxArg)
	b.get(x64.RSI, aRune)
	a.Cmp(x64.RSI, x64.RDX)
	a.Jcc(x64.E, lOk)
	a.Jmp(lStep)
	a.Mark(lTryCls)
	a.CmpImm(x64.RAX, rxClass)
	a.Jcc(x64.NE, lStep)
	// 字符组：线性扫那几段范围（范围数量是个位数，比建位图划算）
	b.get(x64.RSI, aRune)
	a.MovLoad(x64.RDX, x64.RCX, rxClLo)
	a.MovLoad(x64.R8, x64.RCX, rxClN)
	a.Xor(x64.R9, x64.R9)
	a.Mark(lClsTop)
	a.Test(x64.R8, x64.R8)
	a.Jcc(x64.E, lClsDone)
	a.MovLoad(x64.RAX, x64.RDX, rxArg)
	a.Cmp(x64.RSI, x64.RAX)
	a.Jcc(x64.B, lClsNext)
	a.MovLoad(x64.RAX, x64.RDX, rxOut1)
	a.Cmp(x64.RSI, x64.RAX)
	a.Jcc(x64.A, lClsNext)
	a.MovImm(x64.R9, 1)
	a.Jmp(lClsDone)
	a.Mark(lClsNext)
	a.AddImm(x64.RDX, rxISz)
	a.AddImm(x64.R8, -1)
	a.Jmp(lClsTop)
	a.Mark(lClsDone)
	a.MovLoad(x64.RAX, x64.RCX, rxArg)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lClsNeg)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.NE, lOk)
	a.Jmp(lStep)
	a.Mark(lClsNeg)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, lOk)
	a.Jmp(lStep)

	a.Mark(lOk)
	a.MovLoad(x64.RDX, x64.RCX, rxOut1)
	b.put(aPC, x64.RDX)
	b.get(x64.RAX, aNext)
	b.put(aList, x64.RAX)
	b.get(x64.RAX, aNextN)
	b.put(aListN, x64.RAX)
	a.Call(lAdd)
	b.get(x64.RAX, aListN)
	b.put(aNextN, x64.RAX)
	a.Jmp(lStep)

	a.Mark(lStepDone)
	b.get(x64.RAX, aPos)
	b.get(x64.RCX, aW)
	a.Add(x64.RAX, x64.RCX)
	b.put(aPos, x64.RAX)
	b.get(x64.RAX, aCur)
	b.get(x64.RCX, aNext)
	b.put(aCur, x64.RCX)
	b.put(aNext, x64.RAX)
	b.get(x64.RAX, aCurN)
	b.get(x64.RCX, aNextN)
	b.put(aCurN, x64.RCX)
	b.put(aNextN, x64.RAX)
	a.Jmp(lTop)

	a.Mark(lOut)
	b.get(x64.RAX, aFound)
	b.get(x64.RDX, aEnd)
	b.epilogue()

	// ---- 清进过的标记：只用静态那两个槽与寄存器，不碰帧槽 ----
	a.Mark(lClear)
	cTop := b.label()
	cRet := b.label()
	b.loadRIP(x64.RAX, "rxProg")
	b.loadRIP(x64.RCX, "rxLen")
	a.Add(x64.RAX, x64.RCX) // 尾后一字节
	a.Mov(x64.RCX, x64.RAX)
	b.loadRIP(x64.RAX, "rxProg")
	a.Mark(cTop)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, cRet)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RAX, rxSeen, x64.RDX)
	a.AddImm(x64.RAX, rxISz)
	a.Jmp(cTop)
	a.Mark(cRet)
	a.Ret()

	// ---- 沿"不吃字符的边"走的那一轮闭包：显式一个栈，不占 CPU 栈 ----
	a.Mark(lAdd)
	b.get(x64.RDX, aPC)
	push(x64.RDX, 0) // 入参那条边；push 自己会跳过 0（"这头没接上"）
	a.Mark(lAddTop)
	b.get(x64.RAX, aTop)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lAddRet)
	a.AddImm(x64.RAX, -1)
	b.put(aTop, x64.RAX)
	b.get(x64.RDX, aTop)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RDX, x64.R8)
	b.get(x64.RCX, aDfs)
	a.Add(x64.RDX, x64.RCX)
	a.MovLoad(x64.RCX, x64.RDX, 0)
	b.get(x64.RAX, aSealed)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lAddSealed)
	a.MovLoad(x64.RAX, x64.RCX, rxSeen)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lAddTop)
	a.MovImm(x64.RAX, 1)
	a.MovStore(x64.RCX, rxSeen, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, rxOp)
	a.CmpImm(x64.RAX, rxSplit)
	a.Jcc(x64.E, lSplit)
	a.CmpImm(x64.RAX, rxJmp)
	a.Jcc(x64.E, lJmp)
	a.CmpImm(x64.RAX, rxBol)
	a.Jcc(x64.E, lBol)
	a.CmpImm(x64.RAX, rxEol)
	a.Jcc(x64.E, lEol)
	a.CmpImm(x64.RAX, rxMatch)
	a.Jcc(x64.E, lMatch)
	a.Jmp(lLeaf)

	a.Mark(lSplit)
	push(x64.RCX, rxOut2) // 先压"跳过"那一头，out1 后压先出——优先级就是这么来的
	push(x64.RCX, rxOut1)
	a.Jmp(lAddTop)
	a.Mark(lJmp)
	push(x64.RCX, rxOut1)
	a.Jmp(lAddTop)
	a.Mark(lBol)
	b.get(x64.RAX, aAPos)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, lAddTop)
	push(x64.RCX, rxOut1)
	a.Jmp(lAddTop)
	a.Mark(lEol)
	b.get(x64.RAX, aAPos)
	b.get(x64.RDX, aN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, lAddTop)
	push(x64.RCX, rxOut1)
	a.Jmp(lAddTop)
	a.Mark(lMatch)
	b.get(x64.RDX, aAPos)
	b.put(aEnd, x64.RDX)
	a.MovImm(x64.RAX, 1)
	b.put(aFound, x64.RAX)
	b.put(aSealed, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(aTop, x64.RAX)
	a.Ret()
	a.Mark(lAddSealed)
	a.Xor(x64.RDX, x64.RDX)
	b.put(aTop, x64.RDX)
	a.Ret()
	a.Mark(lLeaf)
	b.get(x64.RAX, aList)
	b.get(x64.RDX, aListN)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RDX, x64.R8)
	a.Add(x64.RDX, x64.RAX)
	a.MovStore(x64.RDX, 0, x64.RCX)
	b.get(x64.RAX, aListN)
	a.AddImm(x64.RAX, 1)
	b.put(aListN, x64.RAX)
	a.Jmp(lAddTop)
	a.Mark(lAddRet)
	a.Ret()
}

// @rx_scan(rcx=从第几字节起, rdx=文本, r9=1 表示只接受非空的)：返回 rax=匹配段的起点
// （没有匹配返回 -1）、rdx=终点。
//
// 查找方式与 rtre 参考实现一致：从最靠前的位置起试，不行就前移**一个码点**再试。
// 只接受非空是语言里的一条规则——能匹配空的式子不在每个码点处占一个空匹配，
// find／replace／split 三条都经过这一步，所以那条规则只在这里落实一次。
//
// en: @rx_scan (rcx = byte offset, rdx = text, r9 = 1 to accept non-empty only): returns rax =
// the start of the match (-1 when there is none) and rdx = the end. It searches the way rtre
// does: try from the frontmost position, otherwise move forward ONE CODE POINT and try again.
// The non-empty-only rule says a pattern that can match empty does not take a slot at every
// code point; find/replace/split all come through this step, so the rule is enforced in exactly
// one place.
func rxScan(b *rt) {
	const (
		sFrom = iota
		sText
		sMode
		sN
		sEnd
	)
	a := b.a
	b.prologue()
	b.put(sFrom, x64.RCX)
	b.put(sText, x64.RDX)
	b.put(sMode, x64.R9)
	b.get(x64.RAX, sText)
	a.MovLoad(x64.RCX, x64.RAX, StrBytes)
	b.put(sN, x64.RCX)

	sTop := b.label()
	sTry := b.label()
	sAdv := b.label()
	sHit := b.label()
	sNone := b.label()

	a.Mark(sTop)
	b.get(x64.RAX, sFrom)
	b.get(x64.RCX, sN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.A, sNone) // 已越过末尾
	b.get(x64.RDX, sMode)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, sTry) // 允许空匹配：末尾那个位置也要试（`$` 就落在那儿）
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, sNone) // 只接受非空：从最后一格起必然吃不出东西
	a.Mark(sTry)
	b.get(x64.RCX, sFrom)
	b.get(x64.RDX, sText)
	b.call("@rx_at")
	b.put(sEnd, x64.RDX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, sAdv)
	b.get(x64.RDX, sMode)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, sHit)
	b.get(x64.RAX, sFrom)
	b.get(x64.RDX, sEnd)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.A, sHit) // 非空才接受
	a.Mark(sAdv)
	b.get(x64.RAX, sFrom)
	b.get(x64.RCX, sN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, sNone)
	b.get(x64.RAX, sText)
	b.get(x64.RCX, sFrom)
	a.Add(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	decodeRune(b) // rdi＝这个位置占几字节
	b.get(x64.RAX, sFrom)
	a.Add(x64.RAX, x64.RDI)
	b.put(sFrom, x64.RAX)
	a.Jmp(sTop)

	a.Mark(sHit)
	b.get(x64.RAX, sFrom)
	b.get(x64.RDX, sEnd)
	b.epilogue()
	a.Mark(sNone)
	a.MovImm(x64.RAX, -1)
	a.Xor(x64.RDX, x64.RDX)
	b.epilogue()
}

// @regex_match(rcx=文本, rdx=式子)：返回 rax=1 匹配／0 不匹配。
//
// 不要求整串匹配——需要时由用户自己在两头加 `^`、`$`。空匹配也算匹配。
//
// en: @regex_match (rcx = text, rdx = pattern): returns rax = 1 when it matches / 0 when it
// does not. Matching the whole string is not required - add `^` and `$` at the two ends when
// needed. An empty match counts as matching.
func regexMatch(b *rt) {
	const (
		mText = iota
		mPat
	)
	a := b.a
	b.prologue()
	b.put(mText, x64.RCX)
	b.put(mPat, x64.RDX)
	b.get(x64.RCX, mPat)
	b.call("@rx_compile")
	a.MovImm(x64.RCX, 0)
	b.get(x64.RDX, mText)
	a.Xor(x64.R9, x64.R9)
	b.call("@rx_scan")
	// 返回 -1 就是没有匹配；setcc 之前不能用 xor 去清标志位（那条坑在发射器里也出过一回）
	a.CmpImm(x64.RAX, -1)
	a.Setcc(x64.NE, x64.RAX)
	a.MovZX8Reg(x64.RAX)
	b.epilogue()
}

// @regex_find(rcx=文本, rdx=式子)：返回 rax=list<string>，各段互不重叠、按出现先后排。
//
// 每一个元素都是 @str_sub 新建的一段，所以改不动原来那一块（文本本来也不许改）。
// 只能匹配空的式子在这里返回空 list——这是"空匹配不重复占位"规则的另一面。
//
// en: @regex_find (rcx = text, rdx = pattern): returns rax = list<string>, non-overlapping and
// in order of appearance. Every element is a run @str_sub just created, so the original block
// cannot be modified (and text may not be modified anyway). A pattern that can only match empty
// returns an empty list here - the other face of the "an empty match does not occupy a place
// repeatedly" rule.
func regexFind(b *rt) {
	const (
		fText = iota
		fPat
		fFrom
		fOut
		fStart
		fEnd
	)
	a := b.a
	b.prologue()
	b.put(fText, x64.RCX)
	b.put(fPat, x64.RDX)
	b.get(x64.RCX, fPat)
	b.call("@rx_compile")
	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, 7) // list 的类型字（编号规则在降级那一边）
	b.call("@list_new")
	b.put(fOut, x64.RAX)
	a.MovImm(x64.RAX, 0)
	b.put(fFrom, x64.RAX)
	loop := b.label()
	done := b.label()
	a.Mark(loop)
	b.get(x64.RCX, fFrom)
	b.get(x64.RDX, fText)
	a.MovImm(x64.R9, 1)
	b.call("@rx_scan")
	a.CmpImm(x64.RAX, -1)
	a.Jcc(x64.E, done)
	b.put(fStart, x64.RAX)
	b.put(fEnd, x64.RDX)
	b.get(x64.RCX, fText)
	b.get(x64.RDX, fStart)
	b.get(x64.R8, fEnd)
	a.Sub(x64.R8, x64.RDX)
	b.call("@str_sub")
	a.Mov(x64.RDX, x64.RAX)
	b.get(x64.RCX, fOut)
	b.call("@list_add")
	b.get(x64.RAX, fEnd)
	b.put(fFrom, x64.RAX)
	a.Jmp(loop)
	a.Mark(done)
	b.get(x64.RAX, fOut)
	b.epilogue()
}

// @regex_split(rcx=文本, rdx=式子)：返回 rax=list<string>。
//
// 切点自己丢掉，紧挨着的两段该是空串就留空串（与 s.split 同一条）；遇到一次**空的**匹配
// 就往前挪一个码点接着找，不会像 `s.split("")` 那样逐码点拆开。
//
// en: @regex_split (rcx = text, rdx = pattern): returns rax = list<string>. The cut point
// itself is dropped and adjacent pieces that should be empty stay empty (the same rule as
// s.split); after an EMPTY match the search moves one code point forward, so it does not split
// every code point the way `s.split("")` would.
func regexSplit(b *rt) {
	const (
		sText = iota
		sPat
		sFrom
		sOut
		sCut
		sStart
		sEnd
	)
	a := b.a
	b.prologue()
	b.put(sText, x64.RCX)
	b.put(sPat, x64.RDX)
	b.get(x64.RCX, sPat)
	b.call("@rx_compile")
	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, 7) // list 的类型字
	b.call("@list_new")
	b.put(sOut, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(sFrom, x64.RAX)
	b.put(sCut, x64.RAX)

	// 每一段都是 @str_sub 现造的，再造一条 list_add 挂上去；两条路（撞上切点／收尾那一段）
	// 各摆一份，为的是收尾那一段挂完就该出去，不能再跳回循环。
	loop := b.label()
	tail := b.label()
	done := b.label()

	a.Mark(loop)
	b.get(x64.RCX, sFrom)
	b.get(x64.RDX, sText)
	a.MovImm(x64.R9, 1)
	b.call("@rx_scan")
	a.CmpImm(x64.RAX, -1)
	a.Jcc(x64.E, tail)
	b.put(sStart, x64.RAX)
	b.put(sEnd, x64.RDX)
	// 切点之前那一段，整段照抄（可以是空的）
	b.get(x64.RCX, sText)
	b.get(x64.RDX, sCut)
	b.get(x64.R8, sStart)
	a.Sub(x64.R8, x64.RDX)
	b.call("@str_sub")
	a.Mov(x64.RDX, x64.RAX)
	b.get(x64.RCX, sOut)
	b.call("@list_add")
	b.get(x64.RAX, sEnd)
	b.put(sFrom, x64.RAX)
	b.put(sCut, x64.RAX)
	a.Jmp(loop)

	a.Mark(tail)
	// 最后那一段：从上一个切点到文本末尾
	b.get(x64.RCX, sText)
	b.get(x64.RDX, sCut)
	b.get(x64.R8, sText)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	a.Sub(x64.R8, x64.RDX)
	b.call("@str_sub")
	a.Mov(x64.RDX, x64.RAX)
	b.get(x64.RCX, sOut)
	b.call("@list_add")
	a.Mark(done)
	b.get(x64.RAX, sOut)
	b.epilogue()
}

// @regex_replace(rcx=文本, rdx=式子, r8=替身)：返回 rax=换好的新文本。
//
// 每个非空的匹配都换掉，替身里没有 `$1` 那一种引用（这一版不做捕获）。
// 文本对象是「字节数＋码点数＋一整块」连在一起的一块，落笔之前得先知道要多大，
// 所以走两趟：第一趟只数长度（字节数与码点数各一笔），第二趟照同样的顺序抄一遍。
// 两趟跑的是同一台匹配机、同一个式子、同一条"从头面找第一处"的规矩，所以对得上。
//
// en: @regex_replace (rcx = text, rdx = pattern, r8 = replacement): returns rax = new text with
// the replacements made. Every non-empty match is replaced, and the replacement has no `$1`
// references - captures are not implemented. A text object is one block of "byte count
// + code point count + the whole body" laid out together, so the size must be known
// before writing: two passes, the first counting only lengths (bytes and code points
// separately), the second copying in the same order. Both passes run the same matcher, same
// pattern, same "find the first from the front" rule, so they agree.
func regexReplace(b *rt) {
	const (
		xText = iota
		xPat
		xRep
		xTotal // 结果几字节
		xRunes // 结果几个码点
		xFrom
		xCut // 抄到哪儿了
		xStart
		xEnd
		xNew
		xDst
		xRepData
		xRepN
		xRepR
		xN
		xData
		xA // 子程序的入参：这一段的头一个字节下标
		xB // 与尾后一个字节下标
	)
	a := b.a
	b.prologue()
	b.put(xText, x64.RCX)
	b.put(xPat, x64.RDX)
	b.put(xRep, x64.R8)
	b.get(x64.RCX, xPat)
	b.call("@rx_compile")

	kon := func(slot int, v int64) {
		a.MovImm(x64.RAX, v)
		b.put(slot, x64.RAX)
	}
	// 替身与文本的两个槽：字节数、码点数、字节起点
	b.get(x64.RAX, xRep)
	a.MovLoad(x64.RCX, x64.RAX, StrBytes)
	b.put(xRepN, x64.RCX)
	a.MovLoad(x64.RCX, x64.RAX, StrCount)
	b.put(xRepR, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	b.put(xRepData, x64.RAX)
	b.get(x64.RAX, xText)
	a.MovLoad(x64.RCX, x64.RAX, StrBytes)
	b.put(xN, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	b.put(xData, x64.RAX)

	lCount := b.label()
	lStat := b.label()
	lCopy := b.label()
	lCopyRep := b.label()
	p1 := b.label()
	p1Tail := b.label()
	p2 := b.label()
	p2Tail := b.label()
	done := b.label()

	// ---- 第一趟：数长度 ----
	kon(xTotal, 0)
	kon(xRunes, 0)
	kon(xFrom, 0)
	kon(xCut, 0)
	a.Mark(p1)
	b.get(x64.RCX, xFrom)
	b.get(x64.RDX, xText)
	a.MovImm(x64.R9, 1)
	b.call("@rx_scan")
	a.CmpImm(x64.RAX, -1)
	a.Jcc(x64.E, p1Tail)
	b.put(xStart, x64.RAX)
	b.put(xEnd, x64.RDX)
	b.get(x64.RAX, xCut)
	b.put(xA, x64.RAX)
	b.get(x64.RAX, xStart)
	b.put(xB, x64.RAX)
	a.Call(lStat)
	b.get(x64.RAX, xTotal)
	b.get(x64.RCX, xRepN)
	a.Add(x64.RAX, x64.RCX)
	b.put(xTotal, x64.RAX)
	b.get(x64.RAX, xRunes)
	b.get(x64.RCX, xRepR)
	a.Add(x64.RAX, x64.RCX)
	b.put(xRunes, x64.RAX)
	b.get(x64.RAX, xEnd)
	b.put(xFrom, x64.RAX)
	b.put(xCut, x64.RAX)
	a.Jmp(p1)
	a.Mark(p1Tail)
	b.get(x64.RAX, xCut)
	b.put(xA, x64.RAX)
	b.get(x64.RAX, xN)
	b.put(xB, x64.RAX)
	a.Call(lStat)

	// 一块拿足：假头两个槽（字节数、码点数）＋正文
	b.get(x64.RCX, xTotal)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(xNew, x64.RAX)
	a.AddImm(x64.RAX, strHead)
	b.put(xDst, x64.RAX)

	// ---- 第二趟：照同样的顺序抄 ----
	kon(xFrom, 0)
	kon(xCut, 0)
	a.Mark(p2)
	b.get(x64.RCX, xFrom)
	b.get(x64.RDX, xText)
	a.MovImm(x64.R9, 1)
	b.call("@rx_scan")
	a.CmpImm(x64.RAX, -1)
	a.Jcc(x64.E, p2Tail)
	b.put(xStart, x64.RAX)
	b.put(xEnd, x64.RDX)
	b.get(x64.RAX, xCut)
	b.put(xA, x64.RAX)
	b.get(x64.RAX, xStart)
	b.put(xB, x64.RAX)
	a.Call(lCopy)
	a.Call(lCopyRep)
	b.get(x64.RAX, xEnd)
	b.put(xFrom, x64.RAX)
	b.put(xCut, x64.RAX)
	a.Jmp(p2)
	a.Mark(p2Tail)
	b.get(x64.RAX, xCut)
	b.put(xA, x64.RAX)
	b.get(x64.RAX, xN)
	b.put(xB, x64.RAX)
	a.Call(lCopy)

	a.Mark(done)
	b.get(x64.RAX, xNew)
	b.get(x64.RCX, xTotal)
	a.MovStore(x64.RAX, 0, x64.RCX)
	b.get(x64.RCX, xRunes)
	a.MovStore(x64.RAX, 8, x64.RCX)
	b.epilogue()

	// ---- 三段子程序：数码点、数那一段、抄那一段 ----
	a.Mark(lCount)
	// 数 [RCX, RDX) 里的码点数，加进 xRunes。规则与 @str_sub 同一句：不是续字节的就是一个码点。
	cTop := b.label()
	cSkip := b.label()
	cRet := b.label()
	a.Mark(cTop)
	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.AE, cRet)
	a.MovZX8(x64.RSI, x64.RCX, 0)
	a.AndImm(x64.RSI, leadMask)
	a.CmpImm(x64.RSI, contByte)
	a.Jcc(x64.E, cSkip)
	b.get(x64.RAX, xRunes)
	a.AddImm(x64.RAX, 1)
	b.put(xRunes, x64.RAX)
	a.Mark(cSkip)
	a.AddImm(x64.RCX, 1)
	a.Jmp(cTop)
	a.Mark(cRet)
	a.Ret()

	a.Mark(lStat)
	// [xA, xB) 那一段：字节数直接减出来，码点数交给 lCount
	b.get(x64.RAX, xTotal)
	b.get(x64.RCX, xA)
	b.get(x64.RDX, xB)
	a.Sub(x64.RDX, x64.RCX)
	a.Add(x64.RAX, x64.RDX)
	b.put(xTotal, x64.RAX)
	b.get(x64.RCX, xData)
	b.get(x64.RAX, xA)
	a.Add(x64.RCX, x64.RAX)
	b.get(x64.RDX, xData)
	b.get(x64.RAX, xB)
	a.Add(x64.RDX, x64.RAX)
	a.Call(lCount)
	a.Ret()

	a.Mark(lCopy)
	// [xA, xB) 那一段抄到 [xDst]，抄完 xDst 自己往前挪
	b.get(x64.RDI, xDst)
	b.get(x64.RSI, xData)
	b.get(x64.RAX, xA)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RCX, xA)
	b.get(x64.RDX, xB)
	a.Sub(x64.RDX, x64.RCX)
	a.Mov(x64.RCX, x64.RDX)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.put(xDst, x64.RDI)
	a.Ret()

	a.Mark(lCopyRep)
	b.get(x64.RDI, xDst)
	b.get(x64.RSI, xRepData)
	b.get(x64.RCX, xRepN)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.put(xDst, x64.RDI)
	a.Ret()
}

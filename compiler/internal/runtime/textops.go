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

// textops.go 是挂在文本与 list 上的那批动作：找、判在不在、变大写小写、去两头、
// 换掉、按码点切一段、拆成码点 list、把 list 拼成串。
//
// 它们共用三个小函数：@str_index（从某个位置起找第一处出现的字节偏移）、decodeRune（在某一个位置字节上
// 解出码点与它占几字节）、copyRun（一段字节往哪儿抄）。文本按 UTF-8 存，这些动作全部在
// **字节**上走；只有涉及"第几个码点"时才用 decodeRune 一个位置一个位置跳——与 `s[i]` 是 O(长度)
// 同一个理由。
//
// 大小写与空白都只识别 ASCII 那一段：非 ASCII 字节一律原样过去。UTF-8 的续字节都 ≥ 0x80，
// 不会等于 ASCII 字母的字节值，所以逐字节扫不会切进一个多字节字符中间。中文没有大小写，
// 这条限制对它没有影响。
//
// 要新造文本的那几条（去两头、换、拼成串）都是**先算长度再 @alloc**：文本对象是
// 「字节数＋码点数＋一整块」连在一起的一块，落笔之前就得知道要多大。
//
// en: textops.go is the batch hanging on text and list: find, contains, upper, lower,
// trim, replace, slice by code point, split into a code point list, join a list into text.
// Three helpers are shared: @str_index (the byte offset of the first occurrence from a
// position), decodeRune (the code point at a byte offset and how many slots it takes) and
// copyRun (where a run of bytes is copied). Text is stored as UTF-8 and all of these work on
// BYTES; decodeRune is used only where "which code point number" is involved, hopping one
// position at a time - the same reason `s[i]` is O(length). Case folding and
// whitespace recognise ASCII only, and every non-ASCII byte passes through unchanged: UTF-8
// continuation bytes are all >= 0x80 and can never equal an ASCII letter's byte value,
// so a byte-wise scan cannot cut into the middle of a multi-byte character. Chinese has no
// case, so the limitation costs it nothing.
// The members that build new text (trim, replace, join) all COMPUTE THE LENGTH FIRST and then
// @alloc: a text object is one block of "byte count + code point count + the whole body",
// so the size must be known before writing.
package runtime

import "neflang.org/nf/internal/x64"

// copyRun 把 [src] 起的 n 个字节抄到 [dst]，三条游标各自往后走。
//
// 抄完 rax 与这三个寄存器都作废（编码器没有"内存到内存"这一条，一个字节过一次寄存器），
// 所以调用它之前该存进帧槽的都要先存好。
//
// en: copyRun copies n bytes from [src] into [dst], advancing three cursors. Afterwards rax and
// all three registers are dead - the encoder has no memory-to-memory instruction, so every byte
// passes through a register - so anything to keep across the call must be parked in a frame
// slot first.
func copyRun(b *rt, dst, src, n int) {
	a := b.a
	top := b.label()
	done := b.label()
	a.Mark(top)
	a.Test(n, n)
	a.Jcc(x64.E, done)
	a.MovZX8(x64.RAX, src, 0)
	a.MovStore8(dst, 0, x64.RAX)
	a.AddImm(src, 1)
	a.AddImm(dst, 1)
	a.AddImm(n, -1)
	a.Jmp(top)
	a.Mark(done)
}

// @str_index(rcx=文本, rdx=要找的那一段, r8=从哪个字节起)：
// 返回 rax=头一处出现的**字节**偏移，一处都没有返回 -1。
//
// 要找的那一段是空的，在字节偏移 0 就命中。剩下的不够一段长就提前结束——再往后肯定放不下。
//
// en: @str_index (rcx = text, rdx = the needle, r8 = from which byte): returns rax = the BYTE
// offset of the first occurrence, -1 when there is none. An empty needle matches at offset
// zero. When what remains is shorter than the needle the search stops early - it certainly
// cannot fit any further along.
func strIndex(b *rt) {
	const (
		iHay  = 0 // 文本
		iNeed = 1 // 要找的那一段
		iN    = 2 // 文本几字节
		iM    = 3 // 那段几字节
		iTop  = 4 // 起点最多到哪儿（n-m）
		iI    = 5 // 当前起点
	)
	a := b.a
	b.prologue()
	b.put(iHay, x64.RCX)
	b.put(iNeed, x64.RDX)
	b.put(iI, x64.R8)
	none := b.label()
	b.get(x64.RAX, iHay)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(iN, x64.RAX)
	b.get(x64.RAX, iNeed)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(iM, x64.RAX)

	b.get(x64.RAX, iM)
	b.get(x64.RDX, iN)
	a.Cmp(x64.RAX, x64.RDX) // 比的是 m 与 n
	a.Jcc(x64.A, none)      // 那段比整条还长，不用再看

	b.get(x64.RAX, iN)
	b.get(x64.RDX, iM)
	a.Sub(x64.RAX, x64.RDX) // top = n-m
	b.put(iTop, x64.RAX)

	top := b.label()
	cmp := b.label()
	found := b.label()
	miss := b.label()
	a.Mark(top)
	b.get(x64.RAX, iI)
	b.get(x64.RDX, iTop)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, none) // 起点已经越过最后一个放得下的位置

	b.get(x64.RSI, iHay)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, iI)
	a.Add(x64.RSI, x64.RDX)
	b.get(x64.RDI, iNeed)
	a.AddImm(x64.RDI, strHead)
	b.get(x64.R9, iM)
	a.Mark(cmp)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, found)
	a.MovZX8(x64.R10, x64.RSI, 0)
	a.MovZX8(x64.R11, x64.RDI, 0)
	a.Cmp(x64.R10, x64.R11)
	a.Jcc(x64.NE, miss)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.R9, -1)
	a.Jmp(cmp)

	a.Mark(found)
	b.get(x64.RAX, iI)
	b.epilogue()

	a.Mark(miss)
	b.get(x64.RAX, iI)
	a.AddImm(x64.RAX, 1)
	b.put(iI, x64.RAX)
	a.Jmp(top)

	a.Mark(none)
	a.MovImm(x64.RAX, -1)
	b.epilogue()
}

// @str_contains(rcx=文本, rdx=要找的那一段)：返回 rax=1 在／0 不在。
//
// 偏移是 -1 就是没有。清高位只能 setcc 之后再 movzx，不能先 xor——
// 那条 xor 会把 setcc 正要看的标志位冲掉（发射器里 setFlag 有过同一个问题）。
//
// en: @str_contains (rcx = text, rdx = the needle): returns rax = 1 present / 0 absent. An
// offset of -1 means it is not there. The upper bits may only be cleared by setcc followed by
// movzx, never by xor first - that xor would wipe the very flags setcc is about to read (the
// same trap as setFlag in the emitter).
func strContains(b *rt) {
	a := b.a
	b.prologue()
	a.Xor(x64.R8, x64.R8) // 从位置 0 起找
	b.call("@str_index")
	a.CmpImm(x64.RAX, -1)
	a.Setcc(x64.NE, x64.RAX)
	a.MovZX8Reg(x64.RAX)
	b.epilogue()
}

// @str_case(rcx=文本, rdx=0 变大写／1 变小写)：返回 rax=新文本。
//
// 只有落在 ASCII 字母那两段里的字节才动，其余原样抄。码点数那个位置直接抄过来：
// 改写的是一字节对一字节，个数不变。
//
// en: @str_case (rcx = text, rdx = 0 upper / 1 lower): returns rax = new text. Only bytes
// inside the two ASCII letter ranges are changed and the rest are copied as they are. The code
// point count is copied straight over: what is rewritten is one byte for one byte, so the count
// cannot change.
func strCase(b *rt) {
	const (
		cSrc = 0
		cUp  = 1 // 0 变大写／别的变小写
		cN   = 2
		cRun = 3
		cNew = 4
		cI   = 5
	)
	a := b.a
	b.prologue()
	b.put(cSrc, x64.RCX)
	b.put(cUp, x64.RDX)
	b.get(x64.RAX, cSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(cN, x64.RAX)
	b.get(x64.RAX, cSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.put(cRun, x64.RAX)

	b.get(x64.RCX, cN)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(cNew, x64.RAX)
	b.get(x64.RDI, cNew)
	b.get(x64.RAX, cN)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	b.get(x64.RAX, cRun)
	a.MovStore(x64.RDI, StrCount, x64.RAX)

	// 变大写动的是 a~z，变小写动的是 A~Z。两支各写一遍圈，不在每一个槽上判方向。
	// 判"落在 [lo,hi] 里"用一条无符号比：先把 lo 减掉，比 lo 小的会变成一个极大的数一起出局。
	body := func(lo, hi byte, delta int32) {
		top := b.label()
		done := b.label()
		skip := b.label()
		a.MovImm(x64.RAX, 0)
		b.put(cI, x64.RAX)
		a.Mark(top)
		b.get(x64.RAX, cI)
		b.get(x64.RDX, cN)
		a.Cmp(x64.RAX, x64.RDX)
		a.Jcc(x64.AE, done)
		b.get(x64.RSI, cSrc)
		a.AddImm(x64.RSI, strHead)
		b.get(x64.RDX, cI)
		a.Add(x64.RSI, x64.RDX)
		b.get(x64.RDI, cNew)
		a.AddImm(x64.RDI, strHead)
		b.get(x64.RDX, cI)
		a.Add(x64.RDI, x64.RDX)
		a.MovZX8(x64.RAX, x64.RSI, 0)
		a.Mov(x64.R9, x64.RAX)
		a.AddImm(x64.R9, -int32(lo))
		a.CmpImm(x64.R9, int32(hi-lo))
		a.Jcc(x64.A, skip)
		a.AddImm(x64.RAX, delta)
		a.Mark(skip)
		a.MovStore8(x64.RDI, 0, x64.RAX)
		b.get(x64.RAX, cI)
		a.AddImm(x64.RAX, 1)
		b.put(cI, x64.RAX)
		a.Jmp(top)
		a.Mark(done)
	}
	upper := b.label()
	end := b.label()
	b.get(x64.RAX, cUp)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, upper)
	body('A', 'Z', 32) // 不是 0：变小写
	a.Jmp(end)
	a.Mark(upper)
	body('a', 'z', -32)
	a.Mark(end)
	b.get(x64.RAX, cNew)
	b.epilogue()
}

// @str_trim(rcx=文本)：返回 rax=去掉两头空白的新文本。
//
// 空白指这六个 ASCII 字节：空格、\t、\n、\v、\f、\r。全角空格（U+3000）不在里头——
// 它是三个字节，按字节扫识别不出来，这一条语言规则尚未定。
// 头游标往前顶、尾游标往后退，两个游标碰上就停；最后那一截整段交给 @str_sub，
// 码点数由它自己数，不必在这里再数一遍。
//
// en: @str_trim (rcx = text): returns rax = new text with whitespace removed from both ends.
// Whitespace means these six ASCII bytes: space, \t, \n, \v, \f, \r. The ideographic space
// (U+3000) is not among them - it is three bytes and a byte-wise scan cannot recognise it;
// that language rule is not yet settled. The head cursor pushes
// forward and the tail cursor backs off, stopping when they meet; the remaining run is handed
// whole to @str_sub, which counts the code points itself, so this is not the place to count
// again.
func strTrim(b *rt) {
	const (
		tSrc = 0
		tN   = 1
		tLo  = 2 // 头游标：第一个还留着的字节
		tHi  = 3 // 尾游标：最后一个还留着的字节
	)
	a := b.a
	b.prologue()
	b.put(tSrc, x64.RCX)
	b.get(x64.RAX, tSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(tN, x64.RAX)

	// [rsi] 那个位置是空白吗：是就跳 yes，不是就跳 no。两个标号都由调用方给，
	// 因为"是"与"不是"接下来做的事在两头的圈里正好相反。
	isSpace := func(yes, no x64.Label) {
		a.MovZX8(x64.RAX, x64.RSI, 0)
		a.CmpImm(x64.RAX, ' ')
		a.Jcc(x64.E, yes)
		a.Mov(x64.R9, x64.RAX)
		a.AddImm(x64.R9, -'\t')
		a.CmpImm(x64.R9, 4) // \t \n \v \f \r 是连着的五个
		a.Jcc(x64.BE, yes)
		a.Jmp(no)
	}

	headTop := b.label()
	headYes := b.label()
	headNo := b.label()
	headDone := b.label()
	a.MovImm(x64.RAX, 0)
	b.put(tLo, x64.RAX)
	a.Mark(headTop)
	b.get(x64.RAX, tLo)
	b.get(x64.RDX, tN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, headDone) // 一路全是空白：头已经顶到尾巴后面
	b.get(x64.RSI, tSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, tLo)
	a.Add(x64.RSI, x64.RDX)
	isSpace(headYes, headNo)
	a.Mark(headYes) // 是空白，往前挪一格
	b.get(x64.RAX, tLo)
	a.AddImm(x64.RAX, 1)
	b.put(tLo, x64.RAX)
	a.Jmp(headTop)
	a.Mark(headNo)
	a.Mark(headDone)

	tailTop := b.label()
	tailYes := b.label()
	tailNo := b.label()
	tailDone := b.label()
	b.get(x64.RAX, tN)
	a.AddImm(x64.RAX, -1)
	b.put(tHi, x64.RAX)
	a.Mark(tailTop)
	b.get(x64.RAX, tHi)
	b.get(x64.RDX, tLo)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.B, tailDone) // 尾退到头的后面：中间已经没有了
	b.get(x64.RSI, tSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, tHi)
	a.Add(x64.RSI, x64.RDX)
	isSpace(tailYes, tailNo)
	a.Mark(tailYes) // 是空白，往后退一格
	b.get(x64.RAX, tHi)
	a.AddImm(x64.RAX, -1)
	b.put(tHi, x64.RAX)
	a.Jmp(tailTop)
	a.Mark(tailNo)
	a.Mark(tailDone)

	// [lo, hi] 那一截。整条都是空白时 hi 正好停在 lo-1，长度算出来就是零，不必另开一支
	b.get(x64.RCX, tSrc)
	b.get(x64.RDX, tLo)
	b.get(x64.R8, tHi)
	b.get(x64.RAX, tLo)
	a.Sub(x64.R8, x64.RAX)
	a.AddImm(x64.R8, 1)
	b.call("@str_sub")
	b.epilogue()
}

// @str_rune_off(rcx=文本, rdx=第几个码点)：返回 rax=那个码点落在第几个字节上。
//
// 允许 rdx 正好等于码点数：那个位置返回的是总字节数，`s.slice(start, end)` 的止用得上。
// 负数或超过码点数当场 10031 号。一个位置一个位置跳用的就是 decodeRune 返回的那个长度。
//
// en: @str_rune_off (rcx = text, rdx = which code point): returns rax = the byte offset where
// that code point lies. rdx equal to the code point count is allowed and returns the total byte
// count, which is what the end bound of `s.slice(start, end)` needs. A negative or a larger
// number takes code 10031 on the spot. The one-position-at-a-time hop uses exactly the length
// decodeRune returns.
func strRuneOff(b *rt) {
	const (
		oSrc = 0
		oWan = 1 // 要第几个码点
		oRun = 2
		oI   = 3
		oOff = 4
	)
	a := b.a
	b.prologue()
	b.put(oSrc, x64.RCX)
	b.put(oWan, x64.RDX)
	bad := b.label()
	b.get(x64.RAX, oSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.put(oRun, x64.RAX)

	b.get(x64.RAX, oWan)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RDX, oRun)
	a.Cmp(x64.RAX, x64.RDX) // 比的是 want 与码点数
	a.Jcc(x64.A, bad)       // 只放到"正好是码点数"那个位置

	a.MovImm(x64.RAX, 0)
	b.put(oI, x64.RAX)
	b.put(oOff, x64.RAX)
	top := b.label()
	done := b.label()
	a.Mark(top)
	b.get(x64.RAX, oI)
	b.get(x64.RDX, oWan)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RAX, oSrc)
	a.AddImm(x64.RAX, strHead)
	b.get(x64.RDX, oOff)
	a.Add(x64.RAX, x64.RDX)
	decodeRune(b) // rdi＝这个位置占几字节，rax 保持是那个位置的地址
	b.get(x64.RAX, oOff)
	a.Add(x64.RAX, x64.RDI)
	b.put(oOff, x64.RAX)
	b.get(x64.RAX, oI)
	a.AddImm(x64.RAX, 1)
	b.put(oI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, oOff)
	b.epilogue()
	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
	b.epilogue()
}

// @str_replace(rcx=文本, rdx=旧, r8=新)：返回 rax=换完的新文本（s.replace）。
//
// 全换，不是只换头一处。旧的那一段是空的就原样拷一份返回——"在每个字节中间插一段东西"
// 这种写法语言没有定，也没有哪段代码指望它，与其猜一条不如少做一条。
//
// 两遍走：先数命中几次、算出结果多长，再一趟抄过去。码点数也按次数折算：
// 换掉一处就是少 oldRunes 个、多 newRunes 个，两边各自的码点数都在自己那个位置里。
//
// en: @str_replace (rcx = text, rdx = old, r8 = new): returns rax = new text with the
// replacements made (s.replace). All occurrences, not only the first. An empty needle
// returns a copy unchanged - "insert something between every byte" is not defined by the
// language and no code depends on it, so not guessing is cheaper than guessing. Two passes:
// first count
// the hits and compute the result's length, then copy in one pass. The code point count is
// converted the same way: replacing one occurrence removes oldRunes and adds newRunes, and both
// counts are on hand in their own objects.
func strReplace(b *rt) {
	const (
		rSrc   = 0
		rOld   = 1
		rNew   = 2
		rN     = 3 // 源几字节
		rM     = 4 // 旧几字节
		rR     = 5 // 新几字节
		rCnt   = 6 // 命中次数
		rI     = 7 // 游标
		rTotal = 8 // 结果几字节
		rRunes = 9 // 结果几个码点
		rObj   = 10
		rW     = 11 // 抄写的落点
	)
	a := b.a
	b.prologue()
	b.put(rSrc, x64.RCX)
	b.put(rOld, x64.RDX)
	b.put(rNew, x64.R8)
	b.get(x64.RAX, rSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(rN, x64.RAX)
	b.get(x64.RAX, rOld)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(rM, x64.RAX)
	b.get(x64.RAX, rNew)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(rR, x64.RAX)

	b.get(x64.RAX, rM)
	a.Test(x64.RAX, x64.RAX)
	noop := b.label()
	a.Jcc(x64.E, noop)

	// 第一遍数命中次数，每命中一次往后跳过一整段旧的（所以 "aaa" 换掉 "aa" 只换头一段）
	a.MovImm(x64.RAX, 0)
	b.put(rCnt, x64.RAX)
	b.put(rI, x64.RAX)
	count := b.label()
	cEnd := b.label()
	a.Mark(count)
	b.get(x64.RCX, rSrc)
	b.get(x64.RDX, rOld)
	b.get(x64.R8, rI)
	b.call("@str_index")
	a.CmpImm(x64.RAX, -1)
	a.Jcc(x64.E, cEnd)
	b.get(x64.RAX, rCnt)
	a.AddImm(x64.RAX, 1)
	b.put(rCnt, x64.RAX)
	b.get(x64.RAX, rI)
	b.get(x64.RDX, rM)
	a.Add(x64.RAX, x64.RDX)
	b.put(rI, x64.RAX)
	a.Jmp(count)
	a.Mark(cEnd)

	// 总字节数＝源长 ＋ 次数 ×(新长 - 旧长)
	b.get(x64.RDX, rM)
	b.get(x64.RAX, rR)
	a.Sub(x64.RDX, x64.RAX) // 旧 - 新
	a.Neg(x64.RDX)          // 新 - 旧
	b.get(x64.RAX, rCnt)
	a.Imul(x64.RAX, x64.RDX)
	b.get(x64.RDX, rN)
	a.Add(x64.RAX, x64.RDX)
	b.put(rTotal, x64.RAX)

	// 码点数同一条算式，两边读各自的码点格
	b.get(x64.RAX, rOld)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.get(x64.RDX, rNew)
	a.MovLoad(x64.RDX, x64.RDX, StrCount)
	a.Sub(x64.RAX, x64.RDX) // 旧的几个 - 新的几个
	a.Neg(x64.RAX)
	b.get(x64.RDX, rCnt)
	a.Imul(x64.RAX, x64.RDX)
	b.get(x64.RDX, rSrc)
	a.MovLoad(x64.RDX, x64.RDX, StrCount)
	a.Add(x64.RAX, x64.RDX)
	b.put(rRunes, x64.RAX)

	b.get(x64.RCX, rTotal)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(rObj, x64.RAX)
	b.get(x64.RDI, rObj)
	b.get(x64.RAX, rTotal)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	b.get(x64.RAX, rRunes)
	a.MovStore(x64.RDI, StrCount, x64.RAX)
	a.AddImm(x64.RDI, strHead)
	b.put(rW, x64.RDI)

	// 第二遍抄：当前位置正好命中的就抄一段新的、跳过一段旧的，否则抄一个字节
	a.MovImm(x64.RAX, 0)
	b.put(rI, x64.RAX)
	top := b.label()
	done := b.label()
	at := b.label()
	one := b.label()
	a.Mark(top)
	b.get(x64.RAX, rI)
	b.get(x64.RDX, rN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RCX, rSrc)
	b.get(x64.RDX, rOld)
	b.get(x64.R8, rI)
	b.call("@str_index")
	b.get(x64.RDX, rI)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.E, at)
	a.Jmp(one)

	a.Mark(at)
	b.get(x64.RDI, rW)
	b.get(x64.RSI, rNew)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RCX, rR)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.put(rW, x64.RDI)
	b.get(x64.RAX, rI)
	b.get(x64.RDX, rM)
	a.Add(x64.RAX, x64.RDX)
	b.put(rI, x64.RAX)
	a.Jmp(top)

	a.Mark(one)
	b.get(x64.RDI, rW)
	b.get(x64.RSI, rSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, rI)
	a.Add(x64.RSI, x64.RDX)
	a.MovImm(x64.RCX, 1)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.put(rW, x64.RDI)
	b.get(x64.RAX, rI)
	a.AddImm(x64.RAX, 1)
	b.put(rI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, rObj)
	b.epilogue()

	a.Mark(noop)
	b.get(x64.RCX, rSrc)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, rN)
	b.call("@str_sub")
	b.epilogue()
}

// @str_slice(rcx=文本, rdx=起码点, r8=止码点)：返回 rax=含起不含止的那一截。
//
// 两个下标都先换成字节偏移（@str_rune_off 自己管"负数与超过码点数"那两种 10031 号），
// 再比一次：止在起前面同样是越界，规则只写了"越界是 10031 号"，没给反着写留另一条。
// 这一条按同一条越界处理。
//
// en: @str_slice (rcx = text, rdx = from code point, r8 = to code point): returns rax = the
// run, from included and to excluded. Both indices are first turned into byte offsets
// (@str_rune_off handles its own two 10031 cases, negative and past the count), then compared
// once more: an end before the start is equally out of range - the rule writes only "out of
// range is code 10031" and leaves no second rule for the reversed case, so it is treated as
// out of range here too.
func strSlice(b *rt) {
	const (
		uSrc  = 0
		uFrom = 1
		uTo   = 2
		uA    = 3 // 起在字节上落在哪儿
		uB    = 4 // 止在字节上落在哪儿
	)
	a := b.a
	b.prologue()
	b.put(uSrc, x64.RCX)
	b.put(uFrom, x64.RDX)
	b.put(uTo, x64.R8)
	reverse := b.label()

	b.get(x64.RCX, uSrc)
	b.get(x64.RDX, uFrom)
	b.call("@str_rune_off")
	b.put(uA, x64.RAX)

	b.get(x64.RCX, uSrc)
	b.get(x64.RDX, uTo)
	b.call("@str_rune_off")
	b.put(uB, x64.RAX)

	b.get(x64.RAX, uB)
	b.get(x64.RDX, uA)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.B, reverse)

	b.get(x64.RCX, uSrc)
	b.get(x64.RDX, uA)
	b.get(x64.R8, uB)
	a.Sub(x64.R8, x64.RDX)
	b.call("@str_sub")
	b.epilogue()

	a.Mark(reverse)
	b.throwWith(codeIndex, "sIndex")
	b.epilogue()
}

// @str_to_list(rcx=文本, rdx=类型字)：返回 rax=list，一个位置一个码点。
//
// 类型字由降级给（编号的规则只有一处）。位置里放的是码点本身，8 字节一个位置，
// 与别的 list 同一种搬法。规则说热点里要反复随机取字符就先转它——
// 转完 `lst[i]` 是 O(1)，而 `s[i]` 每次都得从字节 0 跳过去。
//
// en: @str_to_list (rcx = text, rdx = type word): returns rax = a list with one code point per
// slot. The type word comes from lowering (the numbering rule exists in one place). Each slot
// holds the code point itself, 8 bytes, moved exactly like any other list. The rule says
// that hot code reading characters at random repeatedly should convert first - after
// converting, `lst[i]` is O(1) while `s[i]` must skip from byte 0 every time.
func strToList(b *rt) {
	const (
		lSrc = 0
		lTag = 1
		lRun = 2
		lLst = 3
		lI   = 4
		lOff = 5
	)
	a := b.a
	b.prologue()
	b.put(lSrc, x64.RCX)
	b.put(lTag, x64.RDX)
	b.get(x64.RAX, lSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.put(lRun, x64.RAX)

	// 空的开头，靠 @list_add 一个位置一个位置填：@list_new 那个"个数"是给字面量直接摆位置用的，
	// 从这里传非零数会把长度先记上，add 再往后添就成了双倍（与 @str_split 同一条写法）
	a.MovImm(x64.RCX, 0)
	b.get(x64.RDX, lTag)
	b.call("@list_new")
	b.put(lLst, x64.RAX)

	a.MovImm(x64.RAX, 0)
	b.put(lI, x64.RAX)
	b.put(lOff, x64.RAX)
	top := b.label()
	done := b.label()
	a.Mark(top)
	b.get(x64.RAX, lI)
	b.get(x64.RDX, lRun)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RAX, lSrc)
	a.AddImm(x64.RAX, strHead)
	b.get(x64.RDX, lOff)
	a.Add(x64.RAX, x64.RDX)
	decodeRune(b) // rsi＝码点，rdi＝它占几字节
	// 先把游标推进再调 @list_add：那条 call 会把 rdi 用掉
	b.get(x64.RAX, lOff)
	a.Add(x64.RAX, x64.RDI)
	b.put(lOff, x64.RAX)
	b.get(x64.RCX, lLst)
	a.Mov(x64.RDX, x64.RSI)
	b.call("@list_add")
	b.get(x64.RAX, lI)
	a.AddImm(x64.RAX, 1)
	b.put(lI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, lLst)
	b.epilogue()
}

// @list_join(rcx=list<string>, rdx=分隔)：返回 rax=拼出来的文本（text.join）。
//
// 分隔只在两个元素之间插，头尾各少一根。所以数长度时每一个槽都先加一根分隔再加它自己，
// 转完正好多算了一根，末尾扣回去。
// list 里那个位置是空句柄就当场 10092 号——join 要读它的长度，与"对着空句柄取成员或调用方法"同一条路。
//
// en: @list_join (rcx = list<string>, rdx = separator): returns rax = the text joined out
// (text.join). The separator goes only BETWEEN elements, so none at either end. Hence
// the length count adds one separator plus the element itself for every slot, which comes out
// exactly one separator too many and is subtracted at the end. An empty handle inside the list
// takes code 10092 on the spot - join has to read its length, which is dereferencing to take a
// member or call a method.
func listJoin(b *rt) {
	const (
		jLst   = 0
		jSep   = 1
		jCnt   = 2
		jData  = 3 // 数据块起点
		jSBC   = 4 // 分隔几字节
		jSRC   = 5 // 分隔几个码点
		jTotal = 6
		jRunes = 7
		jObj   = 8
		jI     = 9
		jW     = 10 // 抄写的落点
	)
	a := b.a
	b.prologue()
	b.put(jLst, x64.RCX)
	b.put(jSep, x64.RDX)
	b.get(x64.RAX, jLst)
	a.MovLoad(x64.RAX, x64.RAX, CellLen)
	b.put(jCnt, x64.RAX)
	b.get(x64.RAX, jLst)
	a.MovLoad(x64.RAX, x64.RAX, CellData)
	b.put(jData, x64.RAX)
	b.get(x64.RAX, jSep)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(jSBC, x64.RAX)
	b.get(x64.RAX, jSep)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.put(jSRC, x64.RAX)

	// 空list：结果就是空文本，一根分隔都不该沾
	b.get(x64.RAX, jCnt)
	a.Test(x64.RAX, x64.RAX)
	emptyList := b.label()
	nullOne := b.label()
	a.Jcc(x64.E, emptyList)

	// 第一遍数长度：每格都先算上一根分隔，末尾那一根多出来的再扣掉
	a.MovImm(x64.RAX, 0)
	b.put(jTotal, x64.RAX)
	b.put(jRunes, x64.RAX)

	a.MovImm(x64.RAX, 0)
	b.put(jI, x64.RAX)
	sum := b.label()
	sumEnd := b.label()
	a.Mark(sum)
	b.get(x64.RAX, jI)
	b.get(x64.RDX, jCnt)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, sumEnd)
	// 句柄在位置里，位置在数据块上，每格 8 字节
	b.get(x64.RSI, jData)
	b.get(x64.RDX, jI)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RSI, x64.RSI, 0)
	a.Test(x64.RSI, x64.RSI)
	a.Jcc(x64.E, nullOne)
	b.get(x64.RAX, jTotal)
	b.get(x64.RDX, jSBC)
	a.Add(x64.RAX, x64.RDX)
	a.MovLoad(x64.RDX, x64.RSI, StrBytes)
	a.Add(x64.RAX, x64.RDX)
	b.put(jTotal, x64.RAX)
	b.get(x64.RAX, jRunes)
	b.get(x64.RDX, jSRC)
	a.Add(x64.RAX, x64.RDX)
	a.MovLoad(x64.RDX, x64.RSI, StrCount)
	a.Add(x64.RAX, x64.RDX)
	b.put(jRunes, x64.RAX)
	b.get(x64.RAX, jI)
	a.AddImm(x64.RAX, 1)
	b.put(jI, x64.RAX)
	a.Jmp(sum)
	a.Mark(sumEnd)

	// 多算的那一根扣掉
	b.get(x64.RAX, jTotal)
	b.get(x64.RDX, jSBC)
	a.Sub(x64.RAX, x64.RDX)
	b.put(jTotal, x64.RAX)
	b.get(x64.RAX, jRunes)
	b.get(x64.RDX, jSRC)
	a.Sub(x64.RAX, x64.RDX)
	b.put(jRunes, x64.RAX)

	b.get(x64.RCX, jTotal)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(jObj, x64.RAX)
	b.get(x64.RDI, jObj)
	b.get(x64.RAX, jTotal)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	b.get(x64.RAX, jRunes)
	a.MovStore(x64.RDI, StrCount, x64.RAX)
	a.AddImm(x64.RDI, strHead)
	b.put(jW, x64.RDI)

	// 第二遍抄：第二格起先补一根分隔，再补它自己那一段
	a.MovImm(x64.RAX, 0)
	b.put(jI, x64.RAX)
	cp := b.label()
	cpEnd := b.label()
	noSep := b.label()
	a.Mark(cp)
	b.get(x64.RAX, jI)
	b.get(x64.RDX, jCnt)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, cpEnd)
	b.get(x64.RAX, jI)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noSep) // 头一格前面不补分隔
	b.get(x64.RDI, jW)
	b.get(x64.RSI, jSep)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RCX, jSBC)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.put(jW, x64.RDI)
	a.Mark(noSep)
	b.get(x64.RSI, jData)
	b.get(x64.RDX, jI)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RSI, x64.RSI, 0) // 元素自己那块文本
	b.get(x64.RDI, jW)
	a.MovLoad(x64.RCX, x64.RSI, StrBytes)
	a.AddImm(x64.RSI, strHead)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.put(jW, x64.RDI)
	b.get(x64.RAX, jI)
	a.AddImm(x64.RAX, 1)
	b.put(jI, x64.RAX)
	a.Jmp(cp)
	a.Mark(cpEnd)
	b.get(x64.RAX, jObj)
	b.epilogue()

	a.Mark(emptyList)
	a.MovImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(jObj, x64.RAX)
	b.get(x64.RDI, jObj)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	a.MovStore(x64.RDI, StrCount, x64.RAX)
	b.get(x64.RAX, jObj)
	b.epilogue()

	a.Mark(nullOne)
	b.throwWith(codeNullRef, "sNullRef")
	b.epilogue()
}

// @str_index_of(rcx=文本, rdx=要找的那一段)：返回 rax=头一处出现的**码点**序号，一处都没有返回 -1。
//
// 底子是 @str_index，可它数的是字节；这里的序号要和 s[i]、length(s) 用同一口径，
// 所以再数一遍"前面那几字节里有几个码点"（@count_runes）。
// 要找的那一段是空的，就命中位置 0，与 @str_index 同一句。
//
// en: @str_index_of (rcx = text, rdx = the needle): returns rax = the CODE POINT index of the
// first occurrence, -1 when there is none. The base is @str_index, which counts bytes; this
// index has to use the same scale as s[i] and length(s), so "how many code points are
// in the bytes before it" is counted once more (@count_runes). An empty needle matches at slot
// 0, the same sentence as @str_index.
func strIndexOf(b *rt) {
	const (
		hHay  = 0 // 文本
		hNeed = 1 // 要找的那一段
		hOff  = 2 // @str_index 交回的字节偏移
	)
	a := b.a
	b.prologue()
	b.put(hHay, x64.RCX)
	b.put(hNeed, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.call("@str_index") // rcx、rdx 还是进来那两个
	minus := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, minus)
	b.put(hOff, x64.RAX)
	b.get(x64.RCX, hHay)
	a.AddImm(x64.RCX, strHead)
	b.get(x64.RDX, hOff)
	b.call("@count_runes")
	b.epilogue()

	a.Mark(minus)
	b.epilogue() // rax 已经是 -1
}

// @str_edges(rcx=文本, rdx=那一段, r8=0 看开头／1 看结尾)：返回 rax=1 是、0 不是。
//
// 比的是字节：文本在内存里就是 UTF-8，一段合法 UTF-8 的开头或结尾不会与中间的
// 续字节互相冒充，所以不用先解码。那一段比整个文本还长，直接判 0；那一段是空的，两头都算命中。
//
// en: @str_edges (rcx = text, rdx = the run, r8 = 0 at the start / 1 at the end): returns rax =
// 1 yes / 0 no. The comparison is on bytes: text in memory IS UTF-8 and the start or
// end of a legal UTF-8 run cannot be impersonated by a continuation byte in the middle, so no
// decoding is needed. A needle longer than the whole text is simply 0; an empty needle matches
// at both ends.
func strEdges(b *rt) {
	const (
		gHay  = 0 // 文本
		gNeed = 1 // 那一段
		gMode = 2 // 0 看开头、1 看结尾
		gN    = 3 // 文本几字节
		gM    = 4 // 那一段几字节
		gBase = 5 // 从文本第几个字节起比
		gI    = 6 // 比到那一段的第几个字节
	)
	a := b.a
	b.prologue()
	b.put(gHay, x64.RCX)
	b.put(gNeed, x64.RDX)
	b.put(gMode, x64.R8)
	b.get(x64.RAX, gHay)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(gN, x64.RAX)
	b.get(x64.RAX, gNeed)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(gM, x64.RAX)

	nope := b.label()
	ok := b.label()
	b.get(x64.RAX, gM)
	b.get(x64.RDX, gN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, nope) // 那一段更长，肯定不是

	// 起点：看开头是 0，看结尾是 n 减 m
	a.Xor(x64.RAX, x64.RAX)
	b.get(x64.RCX, gMode)
	a.Test(x64.RCX, x64.RCX)
	headWay := b.label()
	a.Jcc(x64.E, headWay) // 0 就是开头，RAX 已经是 0
	b.get(x64.RAX, gN)
	b.get(x64.RDX, gM)
	a.Sub(x64.RAX, x64.RDX)
	a.Mark(headWay)
	b.put(gBase, x64.RAX)

	a.Xor(x64.RAX, x64.RAX)
	b.put(gI, x64.RAX)
	top := b.label()
	a.Mark(top)
	b.get(x64.RAX, gI)
	b.get(x64.RDX, gM)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, ok)
	b.get(x64.RSI, gHay)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RAX, gBase)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RAX, gI) // 加完起点再把 i 叠上去
	a.Add(x64.RSI, x64.RAX)
	a.MovZX8(x64.R9, x64.RSI, 0)
	b.get(x64.RDI, gNeed)
	a.AddImm(x64.RDI, strHead)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX8(x64.R10, x64.RDI, 0)
	a.Cmp(x64.R9, x64.R10)
	a.Jcc(x64.NE, nope)
	b.get(x64.RAX, gI)
	a.AddImm(x64.RAX, 1)
	b.put(gI, x64.RAX)
	a.Jmp(top)

	a.Mark(ok)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
	a.Mark(nope)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
}

// @str_pad(rcx=文本, rdx=宽度, r8=填充, r9=0 补在头／1 补在尾)：返回 rax=新文本。
//
// 宽度数的是码点，与 `length(s)`、`s[i]` 同一口径。已经够长就原样返回一份新的，
// 不拿旧句柄冒充新的那一份——两个人都能析构同一块。
// 填充反复取用，最后不够一整段就只取到齐数为止，所以结果的码点数正好是宽度（与 JavaScript
// 的 padStart 同一条）。填充是空文本时补不上，也原样返回一份新的。
//
// 凑填充那一段用翻倍：一次 `@str_cat(cur, cur)` 把段长翻一番，几轮就够，不会为拼一米长的
// 补位而重复抄写同一截字节。
//
// en: @str_pad (rcx = text, rdx = width, r8 = filler, r9 = 0 in front / 1 behind): returns rax
// = new text. The width counts code points, the same scale as `length(s)` and
// `s[i]`. Already long enough, a NEW copy is returned rather than passing the old
// handle off as new - that would let two holders dismantle one block. The filler is reused
// repeatedly and the last pass takes only as much as reaches the count, so the result has
// exactly width code points (the same rule as JavaScript's padStart). An empty filler cannot
// pad anything and also returns a fresh copy unchanged. Building the filler run uses doubling:
// one `@str_cat(cur, cur)` doubles the run per round, so a metre of padding does not re-copy
// the same bytes over and over.
func strPad(b *rt) {
	const (
		dSrc  = 0 // 原来那个文本
		dW    = 1 // 宽度（码点）
		dPad  = 2 // 填充那一段
		dMode = 3 // 0 补在头、1 补在尾
		dCur  = 4 // 生成的填充段
		dCut  = 5 // 还要补几个码点
		dOld  = 6 // 翻倍与裁齐时手上那一份，交回新的一份之后就还掉
	)
	a := b.a
	b.prologue()
	b.put(dSrc, x64.RCX)
	b.put(dW, x64.RDX)
	b.put(dPad, x64.R8)
	b.put(dMode, x64.R9)

	same := b.label()
	round := b.label()
	enough := b.label()
	join := b.label()
	out := b.label()

	// 还要补几个码点 = 宽度 − 现有的码点；不到 0 就没得补
	b.get(x64.RAX, dSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.get(x64.RDX, dW)
	a.Sub(x64.RDX, x64.RAX)
	a.Jcc(x64.LE, same)
	b.put(dCut, x64.RDX)
	// 填充是空文本：码点 0，补不动
	b.get(x64.RAX, dPad)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, same)

	// 先取一份填充作为种子
	b.get(x64.RCX, dPad)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, dPad)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	b.put(dCur, x64.RAX)

	a.Mark(round)
	b.get(x64.RCX, dCur)
	a.MovLoad(x64.RAX, x64.RCX, StrCount)
	b.get(x64.RDX, dCut)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, enough)
	// 翻倍：旧的这一份翻完就没用了，当场还掉，不然每一轮都留一份在路上
	b.get(x64.RAX, dCur)
	b.put(dOld, x64.RAX)
	b.get(x64.RCX, dCur)
	b.get(x64.RDX, dCur)
	b.call("@str_cat")
	b.put(dCur, x64.RAX)
	b.get(x64.RCX, dOld)
	b.call("@free")
	a.Jmp(round)

	// 齐数：前 dCut 个码点占几字节，取那一截（取完那一长段就没用了）
	a.Mark(enough)
	b.get(x64.RCX, dCur)
	b.get(x64.RDX, dCut)
	b.call("@str_rune_off")
	b.put(dW, x64.RAX)
	b.get(x64.RAX, dCur)
	b.put(dOld, x64.RAX)
	b.get(x64.RCX, dCur)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, dW)
	b.call("@str_sub")
	b.put(dCur, x64.RAX)
	b.get(x64.RCX, dOld)
	b.call("@free")
	a.Jmp(join)

	// 一样都不用补：把原文本抄一份返回（不交旧句柄），后面那两步 concat 与这一条无关
	a.Mark(same)
	b.get(x64.RCX, dSrc)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, dSrc)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	a.Jmp(out)

	a.Mark(join)
	b.get(x64.RAX, dMode)
	a.Test(x64.RAX, x64.RAX)
	head := b.label()
	a.Jcc(x64.E, head) // 0 补在头：填充在前
	b.get(x64.RCX, dSrc)
	b.get(x64.RDX, dCur)
	b.call("@str_cat")
	b.put(dOld, x64.RAX)
	b.get(x64.RCX, dCur)
	b.call("@free")
	b.get(x64.RAX, dOld)
	a.Jmp(out)
	a.Mark(head)
	b.get(x64.RCX, dCur)
	b.get(x64.RDX, dSrc)
	b.call("@str_cat")
	b.put(dOld, x64.RAX)
	b.get(x64.RCX, dCur)
	b.call("@free")
	b.get(x64.RAX, dOld)
	a.Mark(out)
	b.epilogue()
}

// @str_rindex(rcx=文本, rdx=要找的那一段)：返回 rax=最后一处出现的**码点**序号，一处都没有返回 -1。
//
// 从最后一个放得下的起点往前比，命中就是答案，所以空的这一段命中位置 0——与 index_of 同一条。
// 返回的序号要和 s[i]、length(s) 用同一口径，命中的字节偏移再数一遍前面的码点（@count_runes）。
//
// en: @str_rindex (rcx = text, rdx = the needle): returns rax = the CODE POINT index of the
// last occurrence, -1 when there is none. Comparison starts at the last position where the
// needle still fits and walks backwards, so the first hit is the answer - which means an empty
// needle matches at slot 0, the same rule as index_of. The returned index must use the same
// ruler as s[i] and length(s), so the matched byte offset is converted by counting the code
// points before it (@count_runes).
func strRIndex(b *rt) {
	const (
		rHay  = 0
		rNeed = 1
		rN    = 2 // 文本几字节
		rM    = 3 // 那段几字节
		rI    = 4 // 当前起点（会从 n-m 一路减到 -1）
	)
	a := b.a
	b.prologue()
	b.put(rHay, x64.RCX)
	b.put(rNeed, x64.RDX)
	b.get(x64.RAX, rHay)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(rN, x64.RAX)
	b.get(x64.RAX, rNeed)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(rM, x64.RAX)

	none := b.label()
	b.get(x64.RAX, rM)
	b.get(x64.RDX, rN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, none) // 那段比整条还长

	b.get(x64.RAX, rN)
	b.get(x64.RDX, rM)
	a.Sub(x64.RAX, x64.RDX)
	b.put(rI, x64.RAX)

	top, hit, miss, found := b.label(), b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, rI)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, none) // 有符号：减过头就是 -1
	b.get(x64.RSI, rHay)
	a.AddImm(x64.RSI, strHead)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RDI, rNeed)
	a.AddImm(x64.RDI, strHead)
	b.get(x64.R9, rM)
	a.Mark(hit)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, found)
	a.MovZX8(x64.R10, x64.RSI, 0)
	a.MovZX8(x64.R11, x64.RDI, 0)
	a.Cmp(x64.R10, x64.R11)
	a.Jcc(x64.NE, miss)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.R9, -1)
	a.Jmp(hit)

	a.Mark(found)
	b.get(x64.RCX, rHay)
	a.AddImm(x64.RCX, strHead)
	b.get(x64.RDX, rI)
	b.call("@count_runes")
	b.epilogue()

	a.Mark(miss)
	b.get(x64.RAX, rI)
	a.AddImm(x64.RAX, -1)
	b.put(rI, x64.RAX)
	a.Jmp(top)

	a.Mark(none)
	a.MovImm(x64.RAX, -1)
	b.epilogue()
}

// trimOneSide 是只去一头的两种写法共用的那一段：一头是空白就往前顶（或往后退），
// 剩下那一截整段交给 @str_sub，码点数由它自己数。空白只认 ASCII 那五个加空格，与 trim 同一条。
//
// en: trimOneSide is the section shared by the two one-sided spellings: while that end is
// whitespace, push in (or back off), then hand the remaining run to @str_sub, which counts the
// code points itself. Whitespace means the same five ASCII bytes plus space as trim.
func trimOneSide(b *rt, fromHead bool) {
	const (
		tSrc = 0
		tN   = 1
		tLo  = 2 // 第一个还留着的字节
		tHi  = 3 // 最后一个还留着的字节
	)
	a := b.a
	b.prologue()
	b.put(tSrc, x64.RCX)
	b.get(x64.RAX, tSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(tN, x64.RAX)

	isSpace := func(yes, no x64.Label) {
		a.MovZX8(x64.RAX, x64.RSI, 0)
		a.CmpImm(x64.RAX, ' ')
		a.Jcc(x64.E, yes)
		a.Mov(x64.R9, x64.RAX)
		a.AddImm(x64.R9, -'\t')
		a.CmpImm(x64.R9, 4) // \t \n \v \f \r 是连着的五个
		a.Jcc(x64.BE, yes)
		a.Jmp(no)
	}

	if fromHead {
		a.Xor(x64.RAX, x64.RAX)
		b.put(tLo, x64.RAX)
		top, yes, no, done := b.label(), b.label(), b.label(), b.label()
		a.Mark(top)
		b.get(x64.RAX, tLo)
		b.get(x64.RDX, tN)
		a.Cmp(x64.RAX, x64.RDX)
		a.Jcc(x64.AE, done) // 一路全是空白：已经顶到尾巴后面
		b.get(x64.RSI, tSrc)
		a.AddImm(x64.RSI, strHead)
		b.get(x64.RDX, tLo)
		a.Add(x64.RSI, x64.RDX)
		isSpace(yes, no)
		a.Mark(yes)
		b.get(x64.RAX, tLo)
		a.AddImm(x64.RAX, 1)
		b.put(tLo, x64.RAX)
		a.Jmp(top)
		a.Mark(no)
		a.Mark(done)
		// 剩下的那一截：从 lo 到末尾
		b.get(x64.RCX, tSrc)
		b.get(x64.RDX, tLo)
		b.get(x64.R8, tN)
		b.get(x64.RAX, tLo)
		a.Sub(x64.R8, x64.RAX)
		b.call("@str_sub")
		b.epilogue()
		return
	}

	// 只处理尾巴一侧：hi 从最后一个字节起，遇到空白就退一格
	b.get(x64.RAX, tN)
	a.AddImm(x64.RAX, -1)
	b.put(tHi, x64.RAX)
	b.put(tLo, x64.RAX) // 空文本时 hi 是 -1，lo 跟着也是 -1，长度算出来是零
	top, yes, no, done := b.label(), b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, tHi)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, done) // 有符号：一路全是空白时退到 -1
	b.get(x64.RSI, tSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, tHi)
	a.Add(x64.RSI, x64.RDX)
	isSpace(yes, no)
	a.Mark(yes)
	b.get(x64.RAX, tHi)
	a.AddImm(x64.RAX, -1)
	b.put(tHi, x64.RAX)
	a.Jmp(top)
	a.Mark(no)
	a.Mark(done)
	b.get(x64.RCX, tSrc)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, tHi)
	a.AddImm(x64.R8, 1)
	b.call("@str_sub")
	b.epilogue()
}

// @str_trim_head(rcx=文本)：只去头那一侧的空白。
//
// en: @str_trim_head (rcx = text): remove whitespace on the head side only.
func strTrimHead(b *rt) { trimOneSide(b, true) }

// @str_trim_tail(rcx=文本)：只去尾那一侧的空白。
//
// en: @str_trim_tail (rcx = text): remove whitespace on the tail side only.
func strTrimTail(b *rt) { trimOneSide(b, false) }

// @str_repeat(rcx=文本, rdx=次数)：返回 rax=这一截连着排几份出来的新文本。
//
// 次数不是正数就返回一份空的文本，与"排零份"是同一个答复，不抛。
// 拼接走 @str_cat 那一条：每一次都是新块，上一次的中间块交完就还掉，不留账；
// 码点数与字节数都由 @str_cat 自己算，这里不数第二遍。
//
// en: @str_repeat (rcx = text, rdx = count): returns rax = new text with that run laid out
// count times. A count that is not positive returns empty text - the same answer as
// "repeat zero times" - and throws nothing. Joining goes through @str_cat: every step is a new
// block, the previous intermediate is returned as soon as it is handed over, leaving no ledger;
// the code point and byte counts are both computed by @str_cat and not counted a second time
// here.
func strRepeat(b *rt) {
	const (
		pSrc = 0
		pK   = 1 // 还剩几份要排
		pAcc = 2
		pNew = 3
	)
	a := b.a
	b.prologue()
	b.put(pSrc, x64.RCX)
	b.put(pK, x64.RDX)

	empty, loop, done := b.label(), b.label(), b.label()
	b.get(x64.RAX, pK)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.L, empty) // 零份与负数：一份空的交回去
	b.get(x64.RCX, pSrc)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, pSrc)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub") // 先拷一份当底子，绝不把传进来的那一个句柄交出去
	b.put(pAcc, x64.RAX)
	b.get(x64.RAX, pK)
	a.AddImm(x64.RAX, -1)
	b.put(pK, x64.RAX)
	a.Mark(loop)
	b.get(x64.RAX, pK)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, done)
	b.get(x64.RCX, pAcc)
	b.get(x64.RDX, pSrc)
	b.call("@str_cat")
	b.put(pNew, x64.RAX)
	b.get(x64.RCX, pAcc)
	b.call("@free") // 中间那一份没人拿着了：不还它，排几千次就是几千块
	b.get(x64.RAX, pNew)
	b.put(pAcc, x64.RAX)
	b.get(x64.RAX, pK)
	a.AddImm(x64.RAX, -1)
	b.put(pK, x64.RAX)
	a.Jmp(loop)

	a.Mark(done)
	b.get(x64.RAX, pAcc)
	b.epilogue()

	a.Mark(empty)
	b.get(x64.RCX, pSrc)
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.call("@str_sub")
	b.epilogue()
}

// @str_to_bytes(rcx=文本)：返回 rax=`bytes`，一个字节一个位置。
//
// 从前这一条返回的是 `list<byte>`：每格 8 字节躺的那一个字节本身，文本本来就是 UTF-8，
// 这里不重新编码。换成 `bytes` 之后正文就是那一段连着摆的字节，一个位置一字节，
// 一段 5.6 MB 的文本从 45 MB 的容器变成 5.6 MB 的一块。空文本返回空的块，不另开号。
//
// en: @str_to_bytes (rcx = text): returns rax = `bytes`, one byte per slot.
// This used to return `list<byte>`: each slot's 8 bytes holding
// the byte itself, with no re-encoding since text is already UTF-8. As `bytes` the body is
// simply that contiguous run, one byte per slot, so a 5.6 MB text goes from a 45 MB container
// to a 5.6 MB block. Empty text returns an empty block, with no new code.
func strToBytes(b *rt) {
	const (
		ySrc = 0
		yN   = 1
		yOut = 2
	)
	a := b.a
	b.prologue()
	b.put(ySrc, x64.RCX)
	b.get(x64.RAX, ySrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(yN, x64.RAX)

	b.get(x64.RCX, yN)
	b.call("@bytes_new")
	b.put(yOut, x64.RAX)

	// 逐字节抄：长度为空的一侧先结束，那一段循环是"抄完一个再数一个"的形状
	b.get(x64.RCX, yN)
	a.Test(x64.RCX, x64.RCX)
	done := b.label()
	a.Jcc(x64.E, done)
	b.get(x64.RSI, ySrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDI, yOut)
	a.AddImm(x64.RDI, BytesBody)
	top := b.label()
	a.Mark(top)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RCX, -1)
	a.Jcc(x64.NE, top)
	a.Mark(done)

	b.get(x64.RAX, yOut)
	b.epilogue()
}

// @bytes_str(rcx=bytes)：返回 rax=那一串字节当文本的样子；不是合法的 UTF-8 当场 10044 号。
//
// 从前那一版要逐格（每格 8 字节）把低八位收进一块新文本，收完再验；换成 `bytes` 之后正文
// 本来就是一截连着的字节，所以三步都是现成的：@utf8_ok 验、@count_runes 数码点、@str_wrap 造文本。
// 验不过的时候还没造那一块，不必还谁的东西——从前是"填好了再验，验完把刚填的那块还掉再抛"。
//
// en: @bytes_str (rcx = bytes): returns rax = that run of bytes as text, taking code 10044 on
// the spot when it is not valid UTF-8. The earlier version collected the low eight
// bits of each 8-byte slot into a new text and validated afterwards; as `bytes` the body is
// already a contiguous run, so all three steps are ready-made: @utf8_ok validates, @count_runes
// counts, @str_wrap builds. When validation fails the block has not been created yet, so
// nothing has to be returned - previously it was "fill, then validate, then return the block
// just filled and throw".
func bytesToStr(b *rt) {
	const (
		qSrc = 0
		qN   = 1
	)
	a := b.a
	b.prologue()
	b.put(qSrc, x64.RCX)
	b.get(x64.RAX, qSrc)
	a.MovLoad(x64.RAX, x64.RAX, BytesLen)
	b.put(qN, x64.RAX)

	bad := b.label()
	b.get(x64.RCX, qSrc)
	a.AddImm(x64.RCX, BytesBody)
	b.get(x64.RDX, qN)
	b.call("@utf8_ok")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad)

	b.get(x64.RCX, qSrc)
	a.AddImm(x64.RCX, BytesBody)
	b.get(x64.RDX, qN)
	b.call("@count_runes") // rax＝几个码点，@str_wrap 要的那一格
	a.Mov(x64.R8, x64.RAX)
	b.get(x64.RCX, qSrc)
	a.AddImm(x64.RCX, BytesBody)
	b.get(x64.RDX, qN)
	b.call("@str_wrap")
	b.epilogue()

	a.Mark(bad)
	b.throwWith(codeNotUTF8, "sNotUTF8")
}

// @str_json_escape(rcx=文本)：返回 rax=转义过的那一条新文本（`text.escape_json`）。
//
// 只管 ASCII 那一段：`"`、`\` 与控制字符。控制字符里 \b \t \n \f \r 五种走两个字符的短写法，
// 其余一律 `\u00XX`。别的字节原样过去——UTF-8 的续字节都 ≥ 0x80，不等于这些字节值，所以逐字节
// 扫不会切进一个多字节字符中间（与本文件头上那一条同一个理由）。中文不转 `\uXXXX`：JSON 允许
// 字符串里直接放 UTF-8，绕一圈反而难读。
//
// 两趟走同一条判断。第一趟只数两个数：出去几字节、其中原样抄出去的续字节几个（文本对象是
// 「字节数＋码点数＋一整块」连在一起的一块，落笔之前就得知道要多大，与 `@str_trim`、
// `@str_replace` 同一套做法）。码点数是这两个数之差：escape 出来的每一个位置都是 ASCII、各算一个
// 码点，原样抄出去的字节也算一个，只有续字节不算。
//
// en: @str_json_escape (rcx = text): returns rax = the escaped new text
// (text.escape_json). Only the ASCII band is handled: `"`, `\` and the control characters. Of
// the control characters, \b \t \n \f \r take the two-character short form and every other one
// is written `\u00XX`. All other bytes pass through: UTF-8 continuation bytes are >= 0x80 and
// cannot collide with those byte values, so a byte-wise scan cannot cut into a multi-byte
// character (the same reasoning as the note at the top of this file). Chinese is not written as
// \uXXXX: JSON allows UTF-8 directly inside a string, and converting it would only make it
// harder to read. Both passes apply the same test. The first counts only two numbers: how many
// bytes go out and how many of them are copied continuation bytes (a text object is one block
// of "byte count + code point count + the whole body", so the size must be known before writing
// - the same craft as @str_trim and @str_replace). The code point count is the difference of
// those two: every character produced by escaping is ASCII and counts as one code point, each
// copied whole byte counts as one, and only continuation bytes do not.
func strJsonEscape(b *rt) {
	const (
		jSrc  = 0 // 原文本
		jN    = 1 // 它几个字节
		jI    = 2 // 读到第几个字节
		jOut  = 3 // 出去几字节
		jCont = 4 // 抄出去的续字节几个
		jNew  = 5 // 新文本
		jW    = 6 // 写到第几格
	)
	a := b.a
	b.prologue()
	b.put(jSrc, x64.RCX)
	b.get(x64.RAX, jSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(jN, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(jI, x64.RAX)
	b.put(jOut, x64.RAX)
	b.put(jCont, x64.RAX)

	// byteInto 把 jI 那个位置字节取到指定的寄存器里，rsi 与 rdx 是这一小段的游标。
	byteInto := func(dst int) {
		b.get(x64.RSI, jSrc)
		a.AddImm(x64.RSI, strHead)
		b.get(x64.RDX, jI)
		a.Add(x64.RSI, x64.RDX)
		a.MovZX8(dst, x64.RSI, 0)
	}
	// addN 往 jOut 上加一个数（escape 出来的每一个位置都是一个码点，不用另记）。
	addN := func(n int32) {
		b.get(x64.RAX, jOut)
		a.AddImm(x64.RAX, n)
		b.put(jOut, x64.RAX)
	}

	// 1) 第一趟：数长度。
	top, done, ctrl, two, six, copied, adv := b.label(), b.label(), b.label(), b.label(), b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, jI)
	b.get(x64.RDX, jN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	byteInto(x64.RAX)
	a.CmpImm(x64.RAX, 0x20)
	a.Jcc(x64.L, ctrl)
	a.CmpImm(x64.RAX, '"')
	a.Jcc(x64.E, two)
	a.CmpImm(x64.RAX, '\\')
	a.Jcc(x64.E, two)
	a.Jmp(copied)

	a.Mark(ctrl) // 0x08 0x09 0x0A 0x0C 0x0D 走短写法，其余控制字符六个字符
	a.CmpImm(x64.RAX, '\b')
	a.Jcc(x64.E, two)
	a.CmpImm(x64.RAX, '\t')
	a.Jcc(x64.E, two)
	a.CmpImm(x64.RAX, '\n')
	a.Jcc(x64.E, two)
	a.CmpImm(x64.RAX, '\f')
	a.Jcc(x64.E, two)
	a.CmpImm(x64.RAX, '\r')
	a.Jcc(x64.E, two)
	a.Jmp(six)

	a.Mark(two)
	addN(2)
	a.Jmp(adv)
	a.Mark(six)
	addN(6)
	a.Jmp(adv)

	a.Mark(copied) // 原样抄出去的一格：一个字节、可能是一个码点，也可能是一个续字节
	addN(1)
	byteInto(x64.RAX)
	b.get(x64.RDX, jCont)
	a.AndImm(x64.RAX, leadMask)
	a.CmpImm(x64.RAX, contByte)
	a.Jcc(x64.NE, adv)
	a.AddImm(x64.RDX, 1)
	b.put(jCont, x64.RDX)

	a.Mark(adv)
	b.get(x64.RAX, jI)
	a.AddImm(x64.RAX, 1)
	b.put(jI, x64.RAX)
	a.Jmp(top)
	a.Mark(done)

	// 2) 造那一块：头两格先摆上字节数与码点数，游标全部归零再走第二趟。
	b.get(x64.RCX, jOut)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(jNew, x64.RAX)
	b.get(x64.RAX, jOut)
	a.Mov(x64.RDX, x64.RAX)
	b.get(x64.R8, jCont)
	a.Sub(x64.RDX, x64.R8) // 码点数＝字节数 - 续字节数
	b.get(x64.RDI, jNew)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	a.MovStore(x64.RDI, 8, x64.RDX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(jI, x64.RAX)
	b.put(jW, x64.RAX)

	// putCh 把 rax 里那一个字节写出去，游标挪一个位置；rax 之后作废。
	putCh := func() {
		b.get(x64.RDI, jNew)
		b.get(x64.RDX, jW)
		a.AddImm(x64.RDI, strHead)
		a.Add(x64.RDI, x64.RDX)
		a.MovStore8(x64.RDI, 0, x64.RAX)
		b.get(x64.RAX, jW)
		a.AddImm(x64.RAX, 1)
		b.put(jW, x64.RAX)
	}
	// hexCh 把 rax 里 0～15 那一个数写成十六进制的一位。
	hexCh := func() {
		letter, past := b.label(), b.label()
		a.CmpImm(x64.RAX, 10)
		a.Jcc(x64.B, letter)
		a.AddImm(x64.RAX, 'a'-10)
		a.Jmp(past)
		a.Mark(letter)
		a.AddImm(x64.RAX, '0')
		a.Mark(past)
	}

	// 3) 第二趟：照同一条判断写出去。字节留在 r8——putCh 要借 rax、rdx、rdi。
	top2, done2, ctrl2, esc2, adv2 := b.label(), b.label(), b.label(), b.label(), b.label()
	letB, letT, letN, letF, letR := b.label(), b.label(), b.label(), b.label(), b.label()
	a.Mark(top2)
	b.get(x64.RAX, jI)
	b.get(x64.RDX, jN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done2)
	byteInto(x64.R8)
	a.Mov(x64.RAX, x64.R8)
	a.CmpImm(x64.RAX, 0x20)
	a.Jcc(x64.L, ctrl2)
	a.Mov(x64.RAX, x64.R8)
	a.CmpImm(x64.RAX, '"')
	a.Jcc(x64.E, esc2)
	a.Mov(x64.RAX, x64.R8)
	a.CmpImm(x64.RAX, '\\')
	a.Jcc(x64.E, esc2)
	a.Mov(x64.RAX, x64.R8)
	putCh() // 原样抄
	a.Jmp(adv2)

	a.Mark(esc2) // `"` 与 `\`：反斜杠加它自己
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.Mov(x64.RAX, x64.R8)
	putCh()
	a.Jmp(adv2)

	a.Mark(ctrl2)
	a.CmpImm(x64.R8, '\b')
	a.Jcc(x64.E, letB)
	a.CmpImm(x64.R8, '\t')
	a.Jcc(x64.E, letT)
	a.CmpImm(x64.R8, '\n')
	a.Jcc(x64.E, letN)
	a.CmpImm(x64.R8, '\f')
	a.Jcc(x64.E, letF)
	a.CmpImm(x64.R8, '\r')
	a.Jcc(x64.E, letR)
	// 六个字符的那一种：\u00XX。
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.MovImm(x64.RAX, 'u')
	putCh()
	a.MovImm(x64.RAX, '0')
	putCh()
	a.MovImm(x64.RAX, '0')
	putCh()
	a.Mov(x64.RAX, x64.R8)
	a.ShrImm(x64.RAX, 4)
	a.AndImm(x64.RAX, 15)
	hexCh()
	putCh()
	a.Mov(x64.RAX, x64.R8)
	a.AndImm(x64.RAX, 15)
	hexCh()
	putCh()
	a.Jmp(adv2)

	// 五种短写法：反斜杠加那一个字母，字母由比较的结果决定。
	a.Mark(letB)
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.MovImm(x64.RAX, 'b')
	putCh()
	a.Jmp(adv2)
	a.Mark(letT)
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.MovImm(x64.RAX, 't')
	putCh()
	a.Jmp(adv2)
	a.Mark(letN)
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.MovImm(x64.RAX, 'n')
	putCh()
	a.Jmp(adv2)
	a.Mark(letF)
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.MovImm(x64.RAX, 'f')
	putCh()
	a.Jmp(adv2)
	a.Mark(letR)
	a.MovImm(x64.RAX, '\\')
	putCh()
	a.MovImm(x64.RAX, 'r')
	putCh()

	a.Mark(adv2)
	b.get(x64.RAX, jI)
	a.AddImm(x64.RAX, 1)
	b.put(jI, x64.RAX)
	a.Jmp(top2)
	a.Mark(done2)
	b.get(x64.RAX, jNew)
	b.epilogue()
}

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

// json.go 是那一组 json 的解析器：它是**用中间码写出来的几支函数**，
// 名字都叫 nef.json.*，第一次降到 json 的成员时才排出来。
//
// 为什么不手搓运行期例程：这一组函数要递归、要一个位置一个位置看字节、要造文本、要按交出账
// 拆树——这些活儿在中间码这一层全是现成的（用户函数本来就能递归，帧上那一条栈线也是
// 发射器摆的），而翻成机器码每一行都可能错在字节上。打印机与析构器走的是同一条路、
// 同一个理由（print.go、destroy.go 头上各写着一次）。
//
// 语义的对照表是 `internal/rtjson`：哪一片该合、哪一片该坏、坏了说的是哪一句，
// 都由它定；`internal/lower/json_e2e_test.go` 拿同一批样例实际运行，把这一组函数与它逐行比对。
//
// 三条与那张对照表对得上的设定：
//
//   - **游标与错误记在七个静态槽里**（jstate）。拆解到一半没有别人会插进来：语言里没有回调
//     也没有线程，所以不必把位置当参数一层层传。
//   - **嵌套深度不另设上限**。递归下降每下一层是一条真调用，那条线就是语言承诺的栈：
//     每一支函数开场那一次比较是发射器给每一帧摆的，撞到就走 10093 号那一句。
//   - **坏了整片返回空句柄**，不新增错误码。哪一层的毛病都记进 jErr／jErrPos 再往上退；
//     已经拆出来的那半棵树由 nef.json.free 拆干净，一处不留。
//
// en: json.go is the parser for the json group: it is a set of FUNCTIONS WRITTEN IN INTERMEDIATE
// CODE, all named nef.json.*, laid out the first time a json member is lowered. Why not
// hand-code runtime routines: this set must recurse, inspect bytes one position at
// a time, build text and dismantle a tree per the handover ledger - all of which are
// ready-made at the intermediate-code layer (user functions can already recurse, and the stack
// line on the frame is arranged by the emitter), whereas translating to machine code can be
// wrong in any single byte. The printer and destructor took the same road for the same reason
// (each says so at the top of print.go and destroy.go). The semantic answer key is
// internal/rtjson: which text parses, which is broken, and which sentence a break says are
// all decided there; internal/lower/json_e2e_test.go runs the same samples for real and
// compares this function set against it line by line. Three settings that match that answer
// key: the CURSOR AND THE FAULT LIVE IN SEVEN STATIC SLOTS (jstate) - nothing interrupts a parse
// halfway, since the language has neither callbacks nor threads, so there is no need to pass
// positions down as parameters; NO EXTRA NESTING LIMIT - each level of recursive descent is
// a real call, and that line IS the stack the language promises: the comparison at the start
// of every function is the one the emitter places in every frame, and hitting it walks out
// with code 10093; A BROKEN TEXT RETURNS AN EMPTY HANDLE, with no new error code - whichever
// layer found the fault records it in jErr/jErrPos and unwinds, and the half-built tree is
// dismantled completely by nef.json.free, leaving nothing behind.
package lower

import (
	"fmt"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

// 树上一个节点是一块 48 字节的堆内存，六格，每格 8 字节（句柄指在头之后的第一个位置）。
// 键表与值表都用现成的 `list`，所以对象的键顺序天然按原文留着；数组只用值表。
//
// en: One node of the tree is a 48-byte heap block of six slots, 8 bytes each (the
// handle points at the first position after the header). Both the key table and the value table
// use the ready-made `list`, so an object's key order is preserved as written for free; an
// array uses only the value table.
const (
	jsonNodeSize = 48
	jsonKind     = 0  // 下面那八种之一
	jsonInt      = 8  // 整数面那一个数
	jsonFloat    = 16 // 那一个小数的 64 位位型；整数面也顺手存一份
	jsonText     = 24 // 文本节点解过码的值
	jsonKeys     = 32 // 对象的键（list<string>）
	jsonVals     = 40 // 数组的元素、对象的值（list）
)

// 节点的种类。这八个数是这一组的内部事：两种布尔都叫 boolean，两种数都叫 number，
// 站上说的是 json.kind 返回的那六个词。
//
// en: The kinds of a node. These eight numbers are internal to this group: both booleans answer
// boolean and both numbers answer number - what the site shows are the six words json.kind
// returns.
const (
	jsonNull = iota
	jsonTrue
	jsonFalse
	jsonIntKind   // 整数面的数（3、-12）
	jsonFloatKind // 小数面（3.0、1e9），也包括装不进 int64 的整数面
	jsonTextKind
	jsonArrayKind
	jsonObjectKind
)

// 坏在哪一类。编号与 `rtjson` 那一份一字不差，测试逐条比（json_parity_test.go）。
//
// en: Which kind of problem. The numbering is identical to the rtjson copy and is compared
// entry by entry by json_parity_test.go.
const (
	jsonCut = iota + 1
	jsonValue
	jsonObjEnd
	jsonKey
	jsonColon
	jsonArrEnd
	jsonQuote
	jsonControl
	jsonEscape
	jsonHex
	jsonSurrogate
	jsonNumber
	jsonNullLit
	jsonTrueLit
	jsonFalseLit
	jsonJunk
)

// jsonReasons 是那十七句说明文本的正文，下标就是上面那个编号（0 号是"没毛病"，正文空着）。
// json.explain 按编号取那一句；这一份与 rtjson 的 reasonText 是同一个事实的两处记录，
// 测试逐条比字符串，改一处就会红另一处。
//
// en: jsonReasons holds the body of those seventeen plain-language sentences, indexed by the
// number above (index 0 means "nothing wrong" and its body is empty). json.explain picks the
// sentence by number; this file and rtjson's reasonText are two records of one fact, and the
// tests compare the strings entry by entry, so changing one turns the other red.
var jsonReasons = []string{
	"",
	"这一片到这儿就断了，后头还差东西",
	"这里该是一个值",
	"这里该是一个逗号或者右花括号",
	"这里该有一个引号起头的键",
	"键的后面该有一个冒号",
	"这里该是一个逗号或者右方括号",
	"文本少了收尾的那一个引号",
	"文本里有一个没写转义的控制字符",
	"反斜杠后面跟着一个用不了的转义",
	"u 后面该是四个十六进制的数字",
	"这里有一个配不成对的半个代理字符",
	"这个数的写法不合规矩",
	"null 拼到一半就断了",
	"true 拼到一半就断了",
	"false 拼到一半就断了",
	"值的尾巴上多出来了东西",
}

// jsonKinds 是 json.kind 返回的六个词，按种类编号排（两种布尔、两种数各自共用一个）。
//
// en: jsonKinds is the six words json.kind returns, arranged by kind number (the two booleans
// share one, the two numbers share one).
var jsonKinds = []string{"null", "boolean", "boolean", "number", "number", "string", "array", "object"}

// nef.json.* 那几支的名字。用户函数名不允许含点，撞不上。
//
// en: The names of the nef.json.* functions. User function names contain no dot, so they
// cannot collide.
const (
	fnJSONNew     = "nef.json.new"      // (种类) → 一块节点，六格清零
	fnJSONLit     = "nef.json.lit"      // (那一个字的正文, 种类, 拼错了算哪一类) → 节点或空
	fnJSONHex     = "nef.json.hex4"     // (绝对位置) → 码点，对不上是 -1
	fnJSONUnesc   = "nef.json.unesc"    // () → 码点，读不成是 -1
	fnJSONText    = "nef.json.textof"   // () → 解过码的一条文本，读不成是空句柄
	fnJSONString  = "nef.json.string"   // () → 文本节点
	fnJSONNumber  = "nef.json.number"   // () → 数节点
	fnJSONValue   = "nef.json.value"    // () → 一个值（递归下降的总入口）
	fnJSONObject  = "nef.json.object"   // () → 对象节点
	fnJSONArray   = "nef.json.array"    // () → 数组节点
	fnJSONPair    = "nef.json.pair"     // (对象, 键, 值) → 收下这一对（重复键后覆盖前）
	fnJSONParse   = "nef.json.parse"    // (文本) → 树根或空句柄
	fnJSONFree    = "nef.json.free"     // (节点) → 从里到外拆干净
	fnJSONRender  = "nef.json.render"   // (节点) → 那一段 JSON 文本
	fnJSONQuote   = "nef.json.quote"    // (文本) → 带引号、把该转的转掉
	fnJSONKind    = "nef.json.kind"     // (节点) → 六个词之一
	fnJSONCount   = "nef.json.count"    // (节点) → 有几样
	fnJSONKeys    = "nef.json.keys"     // (节点) → list<string>，键的原文顺序
	fnJSONHas     = "nef.json.has"      // (节点, 键) → 真／假
	fnJSONAt      = "nef.json.at"       // (节点, 键) → 子节点（读走那一格）
	fnJSONItem    = "nef.json.item"     // (节点, 位置) → 子节点（读走那一格）
	fnJSONToText  = "nef.json.to_text"  // (节点) → 那一段文本的拷贝，不是文本就空句柄
	fnJSONToInt   = "nef.json.to_int"   // (节点, 默认) → 整数面的数，否则默认
	fnJSONToFloat = "nef.json.to_float" // (节点, 默认位型) → 小数位型，否则默认
	fnJSONToBool  = "nef.json.to_bool"  // (节点, 默认) → 真／假，否则默认
	fnJSONExplain = "nef.json.explain"  // (文本) → 那一句人话，没毛病是空文本
)

// jsonFns 是这一组要排的全部函数。第一次降到 json 的成员时一次排齐：互相回叫的那几支
// （value 与 object／array／string／number）得先在，发射器按名字找标号才找得到。
//
// en: jsonFns is every function this group lays out. They are all emitted at once the first
// time a json member is lowered: the ones that call back into each other (value with
// object/array/string/number) must exist first, so the emitter can find their labels by name.
var jsonFns = []string{
	fnJSONNew, fnJSONLit, fnJSONHex, fnJSONUnesc, fnJSONText, fnJSONString,
	fnJSONNumber, fnJSONValue, fnJSONObject, fnJSONArray, fnJSONPair, fnJSONParse,
	fnJSONFree, fnJSONRender, fnJSONQuote, fnJSONKind, fnJSONCount, fnJSONKeys,
	fnJSONHas, fnJSONAt, fnJSONItem, fnJSONToText, fnJSONToInt, fnJSONToFloat,
	fnJSONToBool, fnJSONExplain,
	fnJSONFreeKeys, fnJSONFreeVals, fnJSONRenderList, fnJSONHexChar,
}

// jstate 是这十几支共用那几个静态格子。一次都不用的程序一个位置也不多要（ensureJSON 才占）。
//
// en: jstate is the group of static slots these dozen functions share. A program that never
// calls json asks for none of them (ensureJSON claims them).
type jstate struct {
	src   ir.G // 正在拆的那一段文本（句柄）
	buf   ir.G // 正文起点：src 往后 16 个字节
	len   ir.G // 有几个字节
	pos   ir.G // 看到第几个字节了
	err   ir.G // 坏在哪一类（0 是没毛病）
	errAt ir.G // 坏在第几个字节
	esc   ir.G // 刚读出来的那一个转义解出来占几格（只给数长度用）
}

// jsonTextBody 是一条文本的正文起点：句柄指在「字节数 8 ＋ 码点数 8」那两个位置之后。
//
// en: jsonTextBody is where a text's body starts: the handle points at the position after "byte
// count 8 + code point count 8".
const jsonTextBody = int64(runtime.StrCount) + 8

// ensureJSON 把这一组的函数都排出来（重复调用不做处理）。
//
// en: ensureJSON lays out every function of this group (calling it again does nothing).
func ensureJSON(l *L) {
	if l.json != nil {
		return
	}
	l.json = &jstate{
		src: l.hidden(), buf: l.hidden(), len: l.hidden(), pos: l.hidden(),
		err: l.hidden(), errAt: l.hidden(), esc: l.hidden(),
	}
	for _, name := range jsonFns {
		l.p.Func(name) // 先占名字：互相回叫的那几支要在别人找它之前先在场
	}
	jsonBuildNew(l)
	jsonBuildLit(l)
	jsonBuildHex(l)
	jsonBuildUnesc(l)
	jsonBuildText(l)
	jsonBuildString(l)
	jsonBuildNumber(l)
	jsonBuildValue(l)
	jsonBuildObject(l)
	jsonBuildArray(l)
	jsonBuildPair(l)
	jsonBuildParse(l)
	jsonBuildFree(l)
	jsonBuildFreeKeys(l)
	jsonBuildFreeVals(l)
	jsonBuildHexChar(l)
	jsonBuildQuote(l)
	jsonBuildRender(l)
	jsonBuildRenderList(l)
	jsonBuildKind(l)
	jsonBuildCount(l)
	jsonBuildKeys(l)
	jsonBuildHas(l)
	jsonBuildAt(l)
	jsonBuildItem(l)
	jsonBuildToText(l)
	jsonBuildToInt(l)
	jsonBuildToFloat(l)
	jsonBuildToBool(l)
	jsonBuildExplain(l)
}

// ---- 发中间码的小工具箱 ----

// jb 是"往 nef.json 的某一支函数里发中间码"的上下文。它带着打印机那一套工具箱（pb），
// 只是 selfFn 用不上：这一组递归叫的是各不相同的名字，直接写在 call 里。
//
// en: jb is the context for "emit intermediate code into one of the nef.json functions". It
// carries the printer's whole toolbox (pb); only selfFn goes unused, because this group
// recurses into different names, written directly in the call.
type jb struct {
	*pb
	s *jstate
}

func openJSON(l *L, name string, params int) (*jb, bool) {
	f := l.p.Func(name)
	if len(f.Body) > 0 {
		return nil, false
	}
	f.Params = params
	f.PT = make([]ir.T, params)
	for i := range f.PT {
		f.PT[i] = ir.TInt
	}
	// 与打印机同一条规则：参数占用前几个槽，Slots 必须设为它们之后的位置，否则第一个 Temp 会覆盖传入的值。
	f.Slots = f.Params
	return &jb{pb: &pb{f: f, msg: l.msg, selfFn: name}, s: l.json}, true
}

// param 是这一支的第 i 个入参——它在帧上，读法就是 ir.L(i)（发射器一进门把寄存器落到那里）。
//
// en: param is the i-th incoming argument of this function - it lives on the frame, read as
// ir.L(i) (the emitter lands the registers there on entry).
func param(i int) ir.L { return ir.L(i) }

func (j *jb) mov(d, s ir.Val) { j.emit(&ir.Mov{D: d, S: s, T: ir.TInt}) }
func (j *jb) br(op string, a, b ir.Val, to ir.Label) {
	j.emit(&ir.Br{Op: op, A: a, B: b, To: to, T: ir.TInt})
}
func (j *jb) jnz(v ir.Val, to ir.Label) { j.emit(&ir.Jnz{V: v, To: to}) }
func (j *jb) addOne(d ir.Val) {
	j.emit(&ir.Bin{Op: "+", D: d, A: d, B: j.num(1), T: ir.TInt})
}
func (j *jb) minus(a, b ir.Val) ir.Val { return j.bin("-", a, b) }

// call 叫一支运行期例程或 nef.json 的另一支，返回结果的槽。参数一律按 8 个字节搬：
// 句柄、个数、种类编号，以及小数的位型——与降级那头把小数当位型递过去是同一条做法。
//
// en: call invokes a runtime routine or another nef.json function and returns a slot for the
// result. Arguments are always moved as 8 bytes: handle, count, kind number, and the bit
// pattern of a float - the same convention the lowering side uses to pass a float as a bit
// pattern.
func (j *jb) call(fn string, args ...ir.Val) ir.Val {
	d := j.t()
	ft := make([]ir.T, len(args))
	for i := range ft {
		ft[i] = ir.TInt
	}
	j.emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: args, FT: ft, RT: ir.TInt})
	return d
}

func (j *jb) void(fn string, args ...ir.Val) {
	ft := make([]ir.T, len(args))
	for i := range ft {
		ft[i] = ir.TInt
	}
	j.emit(&ir.Call{Fn: fn, Args: args, FT: ft})
}

func (j *jb) pos() ir.Val     { return j.g(j.s.pos) }
func (j *jb) setPos(v ir.Val) { j.setG(j.s.pos, v) }
func (j *jb) textLen() ir.Val { return j.g(j.s.len) }

// adv 把游标推出去 k 格。
//
// en: adv pushes the cursor forward k slots.
func (j *jb) adv(k int64) { j.setPos(j.add(j.pos(), j.num(k))) }

// byteAt 读某个地址上那一个字节（无符号扩开：0～255，不会变成负数）。
//
// en: byteAt reads the one byte at an address, zero-extended: 0-255, never negative.
func (j *jb) byteAt(addr ir.Val) ir.Val {
	d := j.t()
	j.emit(&ir.Load{D: d, Base: addr, Off: 0, T: ir.TInt, Size: 1})
	return d
}

func (j *jb) stByte(addr, v ir.Val) {
	j.emit(&ir.Store{Base: addr, Off: 0, S: v, T: ir.TInt, Size: 1})
}

// char 返回游标往后第 k 格那一个字节；走到尽头返回 -1。
//
// en: char returns the byte k positions past the cursor, or -1 past the end.
func (j *jb) char(k int64) ir.Val {
	at := j.add(j.pos(), j.num(k))
	out := j.t()
	j.mov(out, j.num(-1))
	over := j.fresh()
	j.br(">=", at, j.textLen(), over)
	j.mov(out, j.byteAt(j.add(j.g(j.s.buf), at)))
	j.at(over)
	return out
}

// wsIn 发一段"跳过空白"：空格、制表、换行、回车，别的一律不算（这一组只认这四个）。
//
// 做法是先挪一个位置再回头看是不是空白：不是就把那个位置退回去。这样四种空白只比四次，
// 不必为每一种各写一段"挪一格、再看下一格"。
//
// en: wsIn emits a "skip whitespace" run: space, tab, line feed, carriage return - nothing else
// counts (only these four are accepted).
func (j *jb) wsIn() {
	top, done := j.fresh(), j.fresh()
	j.at(top)
	p := j.pos()
	j.br(">=", p, j.textLen(), done)
	j.setPos(j.add(p, j.num(1)))
	c := j.byteAt(j.add(j.g(j.s.buf), p))
	for _, w := range []int64{' ', '\t', '\n', '\r'} {
		j.br("==", c, j.num(w), top)
	}
	j.setPos(p) // 不是空白：游标回到它身上
	j.at(done)
}

// errIn 发一段"记下坏在哪一类"。只认头一次：往下退的每一层都会再说一句"我这也不是值"，
// 那一句会把真正的原因盖掉（rtjson 的 fail 同一条）。
//
// en: errIn emits "record which kind of problem". Only the first one counts: every layer
// unwinding would otherwise say "not a value either", which buries the real reason (same as
// rtjson's fail).
func (j *jb) errIn(code int64) { j.errVal(j.num(code)) }

// errVal 是同一件事，只是那一个编号是算出来的（@json.lit 的"拼错了算哪一类"是传进来的）。
//
// en: errVal is the same thing except that the number is computed (@json.lit's "which kind of
// problem is a misspelling" is passed in).
func (j *jb) errVal(code ir.Val) {
	already := j.fresh()
	j.jnz(j.g(j.s.err), already)
	j.setG(j.s.err, code)
	j.setG(j.s.errAt, j.pos())
	j.at(already)
}

// freshText 把文本池里那一条抄成一份新的：@str_wrap 造对象，三条都从那条静态文本的头里读。
//
// 需要新造的原因是：交出去的那一条文本会被接住它的那个名字登记成持有者，作用域结束就 @free，
// 而静态池里那一条不该被任何人归还。
//
// en: freshText copies one entry of the text pool into a fresh string: @str_wrap builds the
// object and the three fields are read off that static text's header. The copy is needed
// because a text handed over gets registered as held by the name that receives it and is
// @free'd when that scope ends, and the text pool's entry must not be returned by anybody.
func (j *jb) freshText(which ir.Val) ir.Val {
	n := j.ld(which, runtime.StrBytes)
	r := j.ld(which, runtime.StrCount)
	return j.call("@str_wrap", j.add(which, j.num(jsonTextBody)), n, r)
}

// ---- 建节点与读字面量 ----

// nef.json.new(种类)：向堆要一块 48 字节、六格清零、把种类写进去。
//
// 类型字给 0：节点从不装进盒子，也没人去印它自己（印的是 nef.json.render 拼出来那一段文本），
// 头上那个位置只用来让 @alloc 的账记得下去。
//
// en: nef.json.new(kind): ask the heap for 48 bytes, zero all six slots, and write the kind in.
// The type word is given 0: a node is never boxed and nothing prints the node itself (what
// prints is the text nef.json.render assembles), so that header position exists only so @alloc
// can keep its books.
func jsonBuildNew(l *L) {
	j, ok := openJSON(l, fnJSONNew, 1)
	if !ok {
		return
	}
	n := j.call("@alloc", j.num(jsonNodeSize), j.zero())
	for off := 0; off < jsonNodeSize; off += 8 {
		j.st(n, off, j.zero())
	}
	j.st(n, jsonKind, param(0))
	j.ret(n)
}

// nef.json.lit(正文, 种类, 拼错了算哪一类)：认 true／false／null 那三个一个字长的字面量。
//
// 一个位置一个位置比，比到第几格对不上，游标就停在第几格上——那一句"第几列"说的是第一个不对的地方，
// 不是这一个字的开头（rtjson 的 lit 同一件事）。
//
// en: nef.json.lit(body, kind, which problem a misspelling is): recognises the three one-word
// literals true/false/null. Compared one position at a time, and wherever the comparison first
// fails is where the cursor stays - the "column n" in that sentence points at the first wrong
// character, not at the start of this word (same as rtjson's lit).
func jsonBuildLit(l *L) {
	j, ok := openJSON(l, fnJSONLit, 3)
	if !ok {
		return
	}
	word, kind, why := param(0), param(1), param(2)
	n := j.ld(word, runtime.StrBytes) // 那一个字有几个字节
	k := j.t()
	j.mov(k, j.zero())
	top, bad, done := j.fresh(), j.fresh(), j.fresh()
	j.at(top)
	j.br(">=", k, n, done)
	at := j.add(j.pos(), k) // 比到原文第几格了
	j.br(">=", at, j.textLen(), bad)
	got := j.byteAt(j.add(j.g(j.s.buf), at))
	want := j.byteAt(j.add(j.add(word, j.num(jsonTextBody)), k))
	j.br("!=", got, want, bad)
	j.addOne(k)
	j.jmp(top)
	j.at(bad)
	j.setPos(j.add(j.pos(), k)) // 停在比不下去的那一格上
	j.errVal(why)
	j.ret(j.zero())
	j.at(done)
	j.setPos(j.add(j.pos(), n))
	j.ret(j.call(fnJSONNew, kind))
}

// nef.json.hex4(绝对位置)：读那四个十六进制的数字，返回码点；对不上返回 -1，
// 并且把游标摆到那一个对不上的字节上——那句话的"第几列"要说的是第一个坏字符，
// 而这一读失败整片解析就结束了，游标此后没人再用（rtjson 的 hex4 也是一个位置一个位置往前挪的）。
//
// 越界不算在这里：叫它的那一支先比过"后面还剩四个字节"。
//
// en: nef.json.hex4(absolute position): reads the four hexadecimal digits and returns the code
// point, or -1 when they do not match, leaving the cursor at the byte that failed - the "column
// n" in that sentence is about the first bad character, and once this read fails the whole
// parse is over, so nobody uses the cursor afterwards (rtjson's hex4 also advances one
// position at a time). Running past the end is not handled here: the caller already checked
// that four bytes remain.
func jsonBuildHex(l *L) {
	j, ok := openJSON(l, fnJSONHex, 1)
	if !ok {
		return
	}
	base := j.add(j.g(j.s.buf), param(0)) // 那四个字节在正文里的地址
	cp := j.t()
	j.mov(cp, j.zero())
	// 每一位读完要跳开：不跳的话就从累加那一步直落到"读不成"那一段里去。
	// 最后一位读完了没地方可去，就是收尾那一个 ret。
	var next [4]ir.Label
	for k := range next {
		next[k] = j.fresh()
	}
	for k := 0; k < 4; k++ {
		// 先比界，再读那个位置：走到尽头就是"这一片到这儿就断了"，由叫这一支的那一侧看游标挑那一句
		idx := j.add(param(0), j.num(int64(k)))
		eof, got := j.fresh(), j.t()
		j.br(">=", idx, j.textLen(), eof)
		j.mov(got, j.byteAt(j.add(base, j.num(int64(k)))))
		v := j.t()
		bad, digit, low, up, have := j.fresh(), j.fresh(), j.fresh(), j.fresh(), j.fresh()
		// 三段：0～9、a～f、A～F。都落不进去就是坏。
		j.br("<=", got, j.num('9'), digit)
		j.br(">", got, j.num('f'), bad)
		j.br(">=", got, j.num('a'), low)
		j.br(">", got, j.num('F'), bad)
		j.br("<", got, j.num('A'), bad)
		j.jmp(up)
		j.at(digit)
		j.br("<", got, j.num('0'), bad)
		j.mov(v, j.minus(got, j.num('0')))
		j.jmp(have)
		j.at(low)
		j.mov(v, j.add(j.minus(got, j.num('a')), j.num(10)))
		j.jmp(have)
		j.at(up)
		j.mov(v, j.add(j.minus(got, j.num('A')), j.num(10)))
		j.at(have)
		j.mov(cp, j.add(j.bin("*", cp, j.num(16)), v))
		j.jmp(next[k])
		j.at(bad)
		j.setPos(idx) // 游标摆到第 k 个字节上
		j.ret(j.num(-1))
		j.at(eof)
		j.setPos(j.textLen())
		j.ret(j.num(-1))
		j.at(next[k])
	}
	j.ret(cp)
}

// ---- 文本、数、与递归下降 ----

// nef.json.unesc()：读游标上那一个反斜杠起头的转义序列。
//
// 成了：返回码点，游标推过整个序列，jEsc=这一个解出来占几格（只给数长度那一趟用）。
// 读不成：返回 -1，并且**已经自己记好账**（停在哪一个位置、哪一类毛病），caller 只管往上退。
// 九个一个字的转义、`\uXXXX`、以及代理字符那一对合成一个码点，全在这一支里——
// 文本那两趟扫描共用它，规矩就不会有两份。
//
// en: nef.json.unesc(): reads the escape sequence starting at the backslash under the cursor.
// On success it returns the code point, advances the cursor past the whole sequence and sets
// jEsc to how many slots this one occupies (used only by the length-counting pass). On failure
// it returns -1 and HAS ALREADY KEPT THE BOOKS itself (which position it stopped at and which
// kind of problem), so the caller only unwinds. The nine single-character escapes, \uXXXX, and
// a surrogate pair combining into one code point are all in this one function - the two passes
// over a text share it, so the rule exists only once.
func jsonBuildUnesc(l *L) {
	j, ok := openJSON(l, fnJSONUnesc, 0)
	if !ok {
		return
	}
	var (
		pairNo = j.fresh() // 高的一半没配上低的一半
		adv6   = j.fresh() // 平常的码点：走过这六个字节
	)
	e := j.char(1)
	// 反斜杠后面什么都没有：游标停在那个位置上，说的是"到这儿就断了"
	there := j.fresh()
	j.br("!=", e, j.num(-1), there)
	j.adv(1)
	j.errIn(jsonCut)
	j.ret(j.num(-1))
	j.at(there)
	for _, one := range []struct {
		c, out int64
	}{
		{'"', '"'}, {'\\', '\\'}, {'/', '/'}, {'b', 0x08}, {'f', 0x0C},
		{'n', '\n'}, {'r', '\r'}, {'t', '\t'},
	} {
		no := j.fresh()
		j.br("!=", e, j.num(one.c), no)
		j.setG(j.s.esc, j.num(1))
		j.adv(2)
		j.ret(j.num(one.out))
		j.at(no)
	}
	// 不是这八个：是 u 就走那一条，也不是就说这个转义用不了
	isU := j.fresh()
	j.br("==", e, j.num('u'), isU)
	j.adv(1) // 游标停在那个字母上
	j.errIn(jsonEscape)
	j.ret(j.num(-1))
	j.at(isU)
	cp := j.call(fnJSONHex, j.add(j.pos(), j.num(2)))
	isHex := j.fresh()
	j.br("!=", cp, j.num(-1), isHex) // 游标已经被 hex4 摆到那一个对不上的数字上
	cutHalf := j.fresh()
	j.br("<", j.pos(), j.textLen(), cutHalf)
	j.errIn(jsonCut) // 摆在尽头：那四个数字没读全就到尾巴上了
	j.ret(j.num(-1))
	j.at(cutHalf)
	j.errIn(jsonHex)
	j.ret(j.num(-1))
	j.at(isHex)
	// 代理区那三段：D800～DBFF 是高的一半，DC00～DFFF 是低的一半，别的都是平常的码点
	notHigh := j.fresh()
	lowAlone := j.fresh()
	j.br("<", cp, j.num(0xD800), notHigh)
	isHigh := j.fresh()
	j.br("<=", cp, j.num(0xDBFF), isHigh)
	j.br("<=", cp, j.num(0xDFFF), lowAlone)
	j.jmp(notHigh)

	// 高的一半：紧跟的必须又是一个 \u 起头的低的一半
	j.at(isHigh)
	pair := j.fresh()
	j.br("<=", j.add(j.pos(), j.num(12)), j.textLen(), pair)
	j.jmp(pairNo)
	j.at(pair)
	j.br("!=", j.char(6), j.num('\\'), pairNo)
	j.br("!=", j.char(7), j.num('u'), pairNo)
	low := j.call(fnJSONHex, j.add(j.pos(), j.num(8)))
	lowOk := j.fresh()
	j.br("!=", low, j.num(-1), lowOk)
	lowCut := j.fresh()
	j.br("<", j.pos(), j.textLen(), lowCut)
	j.errIn(jsonCut)
	j.ret(j.num(-1))
	j.at(lowCut)
	j.errIn(jsonHex) // 同上：游标已经在那一个对不上的数字上
	j.ret(j.num(-1))
	j.at(lowOk)
	pairBad := j.fresh()
	j.br("<", low, j.num(0xDC00), pairBad)
	j.br(">", low, j.num(0xDFFF), pairBad)
	// 合成那一个码点：0x10000 + (高-0xD800)*0x400 + (低-0xDC00)
	joined := j.add(j.num(0x10000),
		j.add(j.bin("*", j.minus(cp, j.num(0xD800)), j.num(0x400)), j.minus(low, j.num(0xDC00))))
	j.setG(j.s.esc, j.num(4))
	j.adv(12)
	j.ret(joined)

	j.at(pairBad) // 低的一半读到了却配不成：位置停在两串 `\uXXXX` 之后
	j.adv(12)
	j.errIn(jsonSurrogate)
	j.ret(j.num(-1))

	j.at(pairNo) // 高的一半后面没配上低的一半
	j.adv(6)
	j.errIn(jsonSurrogate)
	j.ret(j.num(-1))
	j.at(lowAlone) // 低的一半自己起头
	j.adv(6)
	j.errIn(jsonSurrogate)
	j.ret(j.num(-1))

	// 平常的那一个码点：按大小数它占几格
	j.at(notHigh)
	j.setG(j.s.esc, j.num(1))
	j.br("<", cp, j.num(0x80), adv6)
	j.setG(j.s.esc, j.num(2))
	j.br("<", cp, j.num(0x800), adv6)
	j.setG(j.s.esc, j.num(3))
	j.at(adv6)
	j.adv(6)
	j.ret(cp)
}

// jsonMemberFn 是那十二条成员各对应哪一支发出来的函数。
//
// en: jsonMemberFn maps each of the twelve members to the function that implements it.
var jsonMemberFn = map[string]string{
	"json.parse":      fnJSONParse,
	"json.explain":    fnJSONExplain,
	"json.kind":       fnJSONKind,
	"json.count":      fnJSONCount,
	"json.keys":       fnJSONKeys,
	"json.has":        fnJSONHas,
	"json.at":         fnJSONAt,
	"json.item":       fnJSONItem,
	"json.to_text":    fnJSONToText,
	"json.to_integer": fnJSONToInt,
	"json.to_number":  fnJSONToFloat,
	"json.to_boolean": fnJSONToBool,
}

// jsonCall 把一次 json.* 调用降成"叫那一支发出来的函数"。
//
// 参数一律按只读借用算，返回的那一份归接住它的那一侧。at 与 item 是例外中的
// 例外：它们返回的是**树里那个位置**，所以那两支函数在返回之前先把那个位置抹成 0，
// 于是接住它的那个名字是这一块唯一的持有者——与 `xs[0]`、`o.f` 同一本账。
//
// 小数那一条（to_number）传进去、返回来的都是那 64 位的位型：槽里放的就是这个数，
// 与 @str_to_float、@f_sqrt 一样的走法。
//
// en: jsonCall lowers one json.* call into "call that emitted function". Arguments are all
// treated as read-only borrows and whatever comes back belongs to the receiving side.
// at and item are exceptions within the exception: they return THE POSITION INSIDE THE TREE, so
// those two functions zero that position before returning, which makes the
// receiving name the block's only holder - the same ledger as `xs[0]` and `o.f`. The
// float member (to_number) passes and receives the 64-bit pattern: that is what sits in the
// slot, same convention as @str_to_float and @f_sqrt.
func (c *fc) jsonCall(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	ensureJSON(c.l)
	fn := jsonMemberFn[ref.Std]
	if fn == "" {
		return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
	}
	var args []ir.Val
	for _, e := range x.Args {
		v, _, err := c.expr(e)
		if err != nil {
			return nil, t, err
		}
		if v == nil {
			return nil, t, fmt.Errorf("这里需要一个值，但 %s 不返回任何东西", describe(e))
		}
		// 空值检查只针对引用位置上的值：to_integer 的默认值是 int，不是句柄
		if et, ok := c.l.f.Types[e]; ok && !et.ByValue() {
			c.guardNotNull(v)
		}
		args = append(args, v)
	}
	fts := make([]ir.T, len(args))
	for i := range fts {
		fts[i] = ir.TInt
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: args, FT: fts, RT: ir.TInt})
	return d, t, nil
}

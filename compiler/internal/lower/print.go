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

// print.go 是那个**用中间码写出来的打印机**：函数名 `nef.print`，收"那一个值的 8 个字节"
// 与"它的形状描述地址"，返回一段新造的文本（`console.print` 与 `number.to_string`
// 收整只容器、盒子时走的都是它）。
//
// 不手搓运行期例程的原因是：打印机要递归、要扫桶、要拼串、要一张"走到哪填到哪"的身份表。
// 这些在中间码这一层全是现成的（用户函数本来就能递归），而翻成机器码按 @put_float 那一次的
// 实测是五六百行指令，每一行都可能在字节上出错。走中间码只需多写一段"生成代码的代码"。
//
// 三条规则：
//
//   - 形状照字面量的写法：list `[3, 1, 2]`、字典与集合 `{"甲": 1}`／`{1, 2}`、
//     盒子拆开印里面那一个值、空的返回 `[]`／`{}`、空句柄返回 `null`、文本字段带引号。
//   - 撞上回环（`a.peers` 里装着 `b`、`b.peers` 里又装着 `a`）时，绕回来那个位置印 `...`：
//     靠一张"这一路上开印了哪些对象"的表识别，只记身份、不记内容。
//   - 环之外再叠一个深度上限（32 层），防的是合法但极深的真实数据把输出撑爆。
//     两种给的是同一个记号——读的人不关心是哪一种，多给一种写法只是多空一档要解释的。
//
// en: print.go is the PRINTER WRITTEN IN INTERMEDIATE CODE: a function named nef.print that
// takes "the 8 bytes of that value" plus "the address of its shape description" and returns a
// newly built text (console.print and number.to_string both route a whole container or box
// through it). It is not a hand-coded runtime routine because the printer must recurse, scan
// buckets, concatenate strings and keep an identity table filled as it walks. All of that is
// ready-made at the intermediate-code layer (user functions already recurse), while translating
// it to machine code measured five or six hundred instructions on the @put_float exercise, any
// one of which can be wrong in a byte. Going through intermediate code only means writing one
// more piece of code-generating code. Three rules: shapes follow the literal spelling - list
// `[3, 1, 2]`, dict and hashset `{"a": 1}` / `{1, 2}`, a box unboxed and its inner value
// printed, empty returning `[]`/`{}`, an empty handle returning `null`, and text fields
// quoted; on a cycle (a.peers holds b and b.peers holds a) the position that comes back around
// prints `...`, recognised by a table of "which objects were opened on this path" recording
// identity only, never content; and beyond cycle detection a depth limit of 32 levels guards
// against legal but extremely deep real data flooding the output. Both hits give the same
// marker - a reader does not care which, and a second spelling only adds one thing to explain.
package lower

import (
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

// printFn 是这一个函数的名字。用户函数名不允许含点，所以撞不上。
//
// en: printFn is this function's name. User function names contain no dot, so it cannot
// collide.
const printFn = "nef.print"

// 形状表里各格的位置。数值与 internal/codegen/data.go 那一份是同一张表，改一处要改两处。
//
// en: The position of each field in the shape table. These numbers are the same table as the
// one in internal/codegen/data.go, so a change must be made in both places.
const (
	pdKind   = 0
	pdTag    = 8
	pdName   = 16
	pdElem   = 24
	pdKey    = 32
	pdNField = 40
	pdFields = 48
	pdNTag   = 56
	pdTags   = 64

	pFieldSz = 24
	pfName   = 0
	pfOff    = 8
	pfType   = 16

	pTagSz = 16
	ptTag  = 0
	ptDesc = 8
)

// 打印出来那个位置给的字。三个 ASCII 点：产物里不必先编 UTF-8，也与 Python／Go 的 `...` 一致。
//
// en: The text written at the position reached by the printed limit: three ASCII dots, so the
// product needs no UTF-8 assembled first, and it matches Python's and Go's `...`.
const (
	pEllipsis = "..."
	pNull     = "null"
	pQuote    = `"`
)

// pb 是"往 nef.print 那一个函数里发中间码"的小工具箱。它跟降级里那个 fc 各走各的：
// 这里的函数是编译器自己写的，帧上只有那几个槽，没有作用域栈、也没有交出账。
//
// en: pb is the small toolbox for "emitting intermediate code into the nef.print function". It
// runs separately from the lowering's fc: this function is written by the compiler itself, so
// its frame holds only those few slots - no scope stack and no handover ledger.
type pb struct {
	f      *ir.Func
	msg    func(string) ir.S // 静态文本池：与降级共用，同一条字面量整个程序只有一份
	trail  ir.G              // 一格：那一串身份的堆地址（打印机第一次要用的时候现拿）
	depth  ir.G              // 走到第几层
	selfFn string            // 递归时叫自己的哪一个名字：nef.print 与 nef.destroy 共用这一套工具箱
}

func (b *pb) emit(in ir.Inst)            { b.f.Emit(in) }
func (b *pb) t() ir.Val                  { return b.f.Temp() }
func (b *pb) fresh() ir.Label            { return b.f.Fresh() }
func (b *pb) at(l ir.Label)              { b.emit(&ir.Lbl{At: l}) }
func (b *pb) jmp(l ir.Label)             { b.emit(&ir.Jump{To: l}) }
func (b *pb) ret(v ir.Val)               { b.emit(&ir.Ret{D: v, Has: true, T: ir.TInt}) }
func (b *pb) ret0()                      { b.emit(&ir.Ret{}) }
func (b *pb) num(v int64) ir.Val         { return ir.K(v) }
func (b *pb) str(s string) ir.Val        { return b.msg(s) }
func (b *pb) zero() ir.Val               { return b.num(0) }
func (b *pb) eq(a, c ir.Val, l ir.Label) { b.emit(&ir.Br{Op: "==", A: a, B: c, To: l, T: ir.TInt}) }
func (b *pb) ne(a, c ir.Val, l ir.Label) { b.emit(&ir.Br{Op: "!=", A: a, B: c, To: l, T: ir.TInt}) }
func (b *pb) ge(a, c ir.Val, l ir.Label) { b.emit(&ir.Br{Op: ">=", A: a, B: c, To: l, T: ir.TInt}) }
func (b *pb) jz(v ir.Val, l ir.Label)    { b.emit(&ir.Jz{V: v, To: l}) }

// ld／st 读写"某个地址＋常数偏移"那个位置。中间码没有"拿寄存器当基址"这一条，
// 所以算出来的指针一律先落一个位置再读。
//
// en: ld/st read and write "an address plus a constant offset". Intermediate code has no
// "register as base" form, so a computed pointer is always landed in a slot before being read.
func (b *pb) ld(base ir.Val, off int) ir.Val {
	d := b.t()
	b.emit(&ir.Load{D: d, Base: base, Off: off, T: ir.TInt})
	return d
}

func (b *pb) ldSz(base ir.Val, off int, size int) ir.Val {
	d := b.t()
	b.emit(&ir.Load{D: d, Base: base, Off: off, T: ir.TInt, Size: size})
	return d
}

func (b *pb) st(base ir.Val, off int, src ir.Val) {
	b.emit(&ir.Store{Base: base, Off: off, S: src, T: ir.TInt})
}

// st32 只写四个字节：形状表头上那个位置（类型字）窄窄地住着，抹它不能碰隔壁的尺寸。
//
// en: st32 writes only four bytes: the type word at the head of a shape description lives
// narrow, and wiping it must not touch the size next door.
func (b *pb) st32(base ir.Val, off int, src ir.Val) {
	b.emit(&ir.Store{Base: base, Off: off, S: src, T: ir.TInt, Size: 4})
}

// g／setG 读写一个位置静态槽。
//
// 静态槽只能通过 `Mov` 进出：`Load`／`Store` 的基址含义是"把那个值当地址再解一次引用"，
// 拿它当基址等于去读槽里那个数所指的内存——头一次槽里是 0，就是拿空指针去解引用。
//
// en: g/setG read and write a static slot. A static slot may only be entered and left through
// `Mov`: the base of Load/Store means "treat that value as an address and dereference", which
// would read the memory the number in the slot points at - and the first of them is 0, i.e.
// dereferencing a null pointer.
func (b *pb) g(slot ir.G) ir.Val {
	d := b.t()
	b.emit(&ir.Mov{D: d, S: slot, T: ir.TInt})
	return d
}

func (b *pb) setG(slot ir.G, src ir.Val) {
	b.emit(&ir.Mov{D: slot, S: src, T: ir.TInt})
}

func (b *pb) bin(op string, a, c ir.Val) ir.Val {
	d := b.t()
	b.emit(&ir.Bin{Op: op, D: d, A: a, B: c, T: ir.TInt})
	return d
}

func (b *pb) add(a, c ir.Val) ir.Val { return b.bin("+", a, c) }
func (b *pb) sub(a, c ir.Val) ir.Val { return b.bin("-", a, c) }
func (b *pb) idx(base, i ir.Val, size int64) ir.Val {
	return b.add(base, b.bin("*", i, b.num(size)))
}

// cat 拼两段文本（@str_cat 拼完不动两边，返回的是新造的一份）。
//
// en: cat joins two pieces of text (@str_cat leaves both operands alone and returns a freshly
// built one).
func (b *pb) cat(a, c ir.Val) ir.Val {
	d := b.t()
	b.emit(&ir.Call{D: d, HasD: true, Fn: "@str_cat", Args: []ir.Val{a, c},
		FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	return d
}

// catFree 拼两段、**把前一段归还回去**：打印机一路攒文本，每拼一次旧的那一段就没人再拿着。
//
// 不分情况处理静态字面量：文本池里那几条头上那格尺寸是 0，`@free` 走到就原地返回
// （同一份块头记录），所以"还没拼过"的那个位置（空文本、左括号）也能照交。
//
// en: catFree joins two pieces and RETURNS THE FIRST: the printer accumulates text the whole
// way, and after each join the older piece is held by nobody. Static literals are not
// special-cased: entries in the text pool carry size 0 in their header, so @free returns
// immediately (the same header record), which means the "not yet joined" position (empty text,
// the opening bracket) can be passed the same way.
func (b *pb) catFree(a, c ir.Val) ir.Val {
	d := b.cat(a, c)
	b.free(a)
	return d
}

// wrap 用两头把中间那一段包起来：`[3, 1, 2]`、`{1, 2}`。
// 中间那一段是刚拼出来的一份，包进新的一整段之后原处没人再拿着，一律还回去。
//
// en: wrap sandwiches a middle piece between two ends: `[3, 1, 2]`, `{1, 2}`. The middle piece
// was just built, and once it is inside the new whole nobody holds the original, so it is
// always returned.
func (b *pb) wrap(open string, mid ir.Val, closing string) ir.Val {
	head := b.cat(b.str(open), mid)
	b.free(mid)
	return b.catFree(head, b.str(closing))
}

func (b *pb) call1(fn string, v ir.Val) ir.Val {
	d := b.t()
	b.emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: []ir.Val{v}, FT: []ir.T{ir.TInt}, RT: ir.TInt})
	return d
}

// call2 返回"叫一条两个入参的运行期例程"的结果（@map_next 走这一步）。
//
// en: call2 returns the result of calling a runtime routine with two parameters (@map_next goes
// through this).
func (b *pb) call2(fn string, x, y ir.Val) ir.Val {
	d := b.t()
	b.emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: []ir.Val{x, y},
		FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	return d
}

// self 递归叫自己一次：那一个值按那一条描述走一遍。叫的是哪一个函数看 selfFn——
// 打印机走到哪儿就递归到哪儿，析构器也一样，两边共用这一套工具箱。
//
// en: self recurses one step: that value is walked once per that description. Which function is
// called depends on selfFn - the printer recurses wherever it goes and so does the destructor,
// and both share this toolbox.
func (b *pb) self(v, desc ir.Val) ir.Val {
	d := b.t()
	b.emit(&ir.Call{D: d, HasD: true, Fn: b.selfFn, Args: []ir.Val{v, desc},
		FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	return d
}

// free 还回堆里（@free 返回 nothing：还不还、是不是那块，都由运行期那头判）。
//
// en: free hands a block back to the heap (@free returns nothing: whether it was returned, or
// whether that was even the block, is judged by the runtime).
func (b *pb) free(v ir.Val) {
	b.emit(&ir.Call{Fn: "@free", Args: []ir.Val{v}, FT: []ir.T{ir.TInt}})
}

// orNull 返回"句柄是空的就是 null，不是空的就算 body 那一段"。
//
// 判空必须在读字段、数位置**之前**：空句柄取成员就是拿 0 加偏移，运行期报 10092 号。
// 打印时不让程序崩溃——一行里印三个值，其中一个是空句柄，那个位置给 `null`
// 就够了，不必因此少印另外两个。
//
// en: orNull returns "null if the handle is empty, otherwise whatever body computes". The empty
// test must come BEFORE reading fields or counting positions: dereferencing an empty handle is
// adding an offset to 0, reported by the runtime as code 10092. Printing must not crash the
// program: a line printing three values where one is an empty handle should show `null` at
// that position and still print the other two.
func (b *pb) orNull(v ir.Val, body func() ir.Val) ir.Val {
	out := b.t()
	notNull, done := b.fresh(), b.fresh()
	b.ne(v, b.zero(), notNull)
	b.emit(&ir.Mov{D: out, S: b.str(pNull), T: ir.TInt})
	b.jmp(done)
	b.at(notNull)
	b.emit(&ir.Mov{D: out, S: body(), T: ir.TInt})
	b.at(done)
	return out
}

// guarded 是"识别环＋限制深度"那一步：这一路上的身份记在 trail 那一串里，撞回来、
// 或者已经深过 maxPrintDepth 层，这个位置就返回 `...`。
//
// trail 那块第一次用到才去拿（程序一启动就 alloc 的话，不印容器的程序白付 256 字节）。
// 退出时只减计数、不清槽位：槽里那个地址在 depth 之外，没人会去读。
//
// en: guarded is the "cycle check plus depth cap" step: the identities on this path are
// recorded in the trail chain, and coming back around - or already being deeper than
// maxPrintDepth - makes this position return `...`. The trail block is allocated only on first
// use (allocating it at start-up would ask 256 bytes of every program that never prints a
// container). Leaving only decrements the counter without clearing the position: the address
// stored there is outside the depth and nobody reads it.
func (b *pb) guarded(ident ir.Val, body func() ir.Val) ir.Val {
	out := b.t()
	yes, hit, done, top := b.fresh(), b.fresh(), b.fresh(), b.fresh()

	tr := b.t()
	got, after := b.fresh(), b.fresh()
	b.emit(&ir.Mov{D: tr, S: b.g(b.trail), T: ir.TInt})
	b.ne(tr, b.zero(), got)
	// 身份那一串：一次最多 maxPrintDepth 格，一次拿足就不必再搬。类型字填 0 没关系——
	// 这块内存从不装进盒子，也没人会去印它，头上那格只用来让 @alloc 的账记得下去。
	fresh := b.t()
	b.emit(&ir.Call{D: fresh, HasD: true, Fn: "@alloc",
		Args: []ir.Val{b.num(maxPrintDepth * 8), b.num(0)}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	b.setG(b.trail, fresh)
	b.emit(&ir.Mov{D: tr, S: fresh, T: ir.TInt})
	b.jmp(after)
	// 第二条路（已经不是头一次）必须回到"从静态槽重读"这一句上：直接往下走会带着
	// 上一条语句留下的那个寄存器继续，第二次以后那个槽位里留下的是别处的值。
	b.at(got)
	b.emit(&ir.Mov{D: tr, S: b.g(b.trail), T: ir.TInt})
	b.at(after)

	depth := b.g(b.depth)
	// 上限先到就不必扫表了：那一串只有 maxPrintDepth 格，越过去就是越界写。
	b.ge(depth, b.num(maxPrintDepth), hit)
	i := b.t()
	b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
	b.at(top)
	b.ge(i, depth, yes)
	b.eq(b.ld(b.idx(tr, i, 8), 0), ident, hit)
	b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
	b.jmp(top)

	b.at(yes)
	b.st(b.idx(tr, depth, 8), 0, ident)
	b.setG(b.depth, b.add(depth, b.num(1)))
	inner := body()
	b.setG(b.depth, b.sub(b.g(b.depth), b.num(1)))
	b.emit(&ir.Mov{D: out, S: inner, T: ir.TInt})
	b.jmp(done)

	b.at(hit)
	b.emit(&ir.Mov{D: out, S: b.str(pEllipsis), T: ir.TInt})
	b.at(done)
	return out
}

// buildPrint 排出 nef.print。用户代码里那几句 call 按名字找它的地址，所以先建后用都行；
// 重复调用不做处理（同一份程序只排一次）。
//
// en: buildPrint lays out nef.print. The call sites in user code look its address up by name,
// so building it after use is fine; calling it again does nothing (one program lays it out
// once).
func buildPrint(l *L) {
	f := l.p.Func(printFn)
	if len(f.Body) > 0 {
		return
	}
	f.Params, f.PT = 2, []ir.T{ir.TInt, ir.TInt}
	// 参数就占头两个槽（发射器一进门把 rcx／rdx 落到 %0、%1），所以要先把 Slots 抬到它们后面：
	// 不抬的话第一个 Temp 就是 %0，会把传进来的那个值覆盖——打印机印的全是种类数而不是值。
	f.Slots = f.Params
	b := &pb{f: f, msg: l.msg, trail: l.hidden(), depth: l.hidden(), selfFn: printFn}
	v, desc := ir.L(0), ir.L(1)
	kind := b.ld(desc, pdKind)
	// 形状表里相互指向的那几个槽存的都是从本条描述起算的字节偏移，使用前一律要加回本条的地址。
	child := func(off int) ir.Val { return b.bin("+", desc, b.ld(desc, off)) }

	// 分派：逐个比较到各自那一段，段尾自己 ret。kinds 里没有的那个种类落到最后一行返回 `...`
	// ——形状表是编译器自己排的，那种事不该发生；真发生了，一个位置的省略号比静默少打印一整行好排查。
	//
	// 带句柄的那几种（文本、三种容器、struct、class、盒子）头上都套一层 orNull：空句柄印 `null`，
	// 不进去读长度、数位置，也不往块头写"正在被印"那个记号（空句柄印 `null` 是定下的答复）。
	cases := []struct {
		k  ir.DescKind
		fn func() ir.Val
	}{
		{ir.KindInt, func() ir.Val { return b.call1("@int_str", v) }},
		{ir.KindByte, func() ir.Val { return b.call1("@int_str", v) }}, // byte 也是十进制
		{ir.KindBool, func() ir.Val { return b.call1("@bool_str", v) }},
		{ir.KindChar, func() ir.Val { return b.call1("@char_str", v) }},
		{ir.KindFloat, func() ir.Val { return b.call1("@float_str", v) }},
		// 文本本身已经是文本，套一对引号就是它在容器里的样子。引号里头不转义：
		// 这一版没有字符串转义表，`console.print` 印文本也是原样，两处同一个规则。
		{ir.KindString, func() ir.Val {
			return b.orNull(v, func() ir.Val { return b.catFree(b.cat(b.str(pQuote), v), b.str(pQuote)) })
		}},
		{ir.KindList, func() ir.Val {
			return b.orNull(v, func() ir.Val {
				return b.guarded(v, func() ir.Val {
					n, data := b.ld(v, runtime.CellLen), b.ld(v, runtime.CellData)
					return b.wrap("[", b.walkList(n, data, child(pdElem)), "]")
				})
			})
		}},
		{ir.KindSet, func() ir.Val {
			return b.orNull(v, func() ir.Val {
				return b.guarded(v, func() ir.Val {
					return b.wrap("{", b.walkTable(v, child(pdElem), nil), "}")
				})
			})
		}},
		{ir.KindDict, func() ir.Val {
			return b.orNull(v, func() ir.Val {
				return b.guarded(v, func() ir.Val {
					kd, vd := child(pdKey), child(pdElem)
					return b.wrap("{", b.walkTable(v, kd, vd), "}")
				})
			})
		}},
		// struct 与 class 用同一份字段表、同一种拼法。值类型每次交出都整份拷新的，
		// 同一块不会被两处同时指着，所以 struct 绕不成环——但深度照数，仍走 guarded。
		{ir.KindStruct, func() ir.Val {
			return b.orNull(v, func() ir.Val { return b.guarded(v, func() ir.Val { return b.shape(v, desc) }) })
		}},
		{ir.KindClass, func() ir.Val {
			return b.orNull(v, func() ir.Val { return b.guarded(v, func() ir.Val { return b.shape(v, desc) }) })
		}},
		// 盒子里那一个值自己有形状、自己识别环（class／struct 的句柄就是这一块，再进内层
		// 还要按同一个地址记一次身份，当场就会被环检测查出来）。所以这一层不重复记。
		{ir.KindBox, func() ir.Val {
			return b.orNull(v, func() ir.Val { return b.boxed(v, child(pdElem)) })
		}},
		// Json：那棵树打印成它自己的 JSON 文本。空句柄不用 orNull 包——
		// nef.json.render 走到空句柄那一条返回的就是 null，与容器空句柄同一个答复。
		{ir.KindJson, func() ir.Val { return b.call1(fnJSONRender, v) }},
		// bytes 种类打印成 `[65, 66, 67]`：与 `list<byte>` 从前那个答复一字不差——
		// 这一档换的是内存里的摆法（一个位置 8 字节变成一个位置 1 字节），不是给人看的那一行字。
		{ir.KindBytes, func() ir.Val {
			return b.orNull(v, func() ir.Val {
				return b.guarded(v, func() ir.Val {
					return b.wrap("[", b.walkBytes(b.ld(v, runtime.BytesLen), v), "]")
				})
			})
		}},
		// 句柄一类印的就是它自己的名字：`Socket`、`Listener`、`Process`。
		// 不打印那个数值——它是系统发的，换一次运行就换一个数，钉不住例子也帮不上读代码的人；
		// 也不套 guarded：句柄里没有下属，绕不出环，那一层身份表对它没有意义。
		// 子进程那一种另有一个种类号：印法一字不差，收尾那一条才是两套（见 destroy.go）。
		{ir.KindHandle, func() ir.Val {
			return b.orNull(v, func() ir.Val { return b.strOf(desc, pdName) })
		}},
		{ir.KindProc, func() ir.Val {
			return b.orNull(v, func() ir.Val { return b.strOf(desc, pdName) })
		}},
	}
	body := make([]ir.Label, len(cases))
	for i, c := range cases {
		body[i] = b.fresh()
		b.eq(kind, b.num(int64(c.k)), body[i])
	}
	last := b.fresh()
	b.jmp(last)
	for i, c := range cases {
		b.at(body[i])
		b.ret(c.fn())
	}
	b.at(last)
	b.ret(b.str(pEllipsis))
}

// walkList 走一遍 list 的位置，返回中间那几段（不含方括号）。每格一律 8 字节
// （窄元素进容器也占满一个位置，块记录里就是这么定的）。
//
// en: walkList walks a list's positions and returns the inner pieces (without the brackets).
// Every slot is always 8 bytes (a narrow element still fills a slot in the block record).
func (b *pb) walkList(n, data, ed ir.Val) ir.Val {
	acc := b.t()
	b.emit(&ir.Mov{D: acc, S: b.str(""), T: ir.TInt})
	i := b.t()
	b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
	top, end, notFirst := b.fresh(), b.fresh(), b.fresh()
	b.at(top)
	b.ge(i, n, end)
	b.eq(i, b.zero(), notFirst)
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, b.str(", ")), T: ir.TInt})
	b.at(notFirst)
	item := b.self(b.ld(b.idx(data, i, 8), 0), ed)
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, item), T: ir.TInt})
	b.free(item)
	b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
	b.jmp(top)
	b.at(end)
	return acc
}

// walkBytes 走一遍 `bytes` 的那一段正文，返回中间那几段（不含方括号）。
//
// 与 walkList 的分别是格宽：那一侧一律 8 字节，这一侧一个位置一个字节，
// 所以取的时候要明写宽度 1——整份搬八个字节会把后面那七个格一起读进来。
// 进来的 h 是句柄：正文从它往后第 BytesBody 个字节起，一个位置一个，所以第 i 格在 h＋8＋i。
// 每格打印成十进制（一个字节就是 0～255 那一个数），逗号与空格与 walkList 同一条。
//
// en: walkBytes walks the body of a `bytes` block and returns the inner pieces (without the
// brackets). The difference from walkList is the slot width: over there it is always 8 bytes,
// here one byte per position, so the read must state width 1 - moving eight bytes
// whole would read the seven following positions too. The incoming h is a handle: the body
// starts BytesBody bytes past it, one byte per position, so index i sits at h+8+i. Each byte
// prints as a decimal (one byte is a number in 0-255), with the comma and space rules shared
// with walkList.
func (b *pb) walkBytes(n, h ir.Val) ir.Val {
	acc := b.t()
	b.emit(&ir.Mov{D: acc, S: b.str(""), T: ir.TInt})
	i := b.t()
	b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
	top, end, notFirst := b.fresh(), b.fresh(), b.fresh()
	b.at(top)
	b.ge(i, n, end)
	b.eq(i, b.zero(), notFirst)
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, b.str(", ")), T: ir.TInt})
	b.at(notFirst)
	item := b.call1("@int_str", b.ldSz(b.add(h, i), runtime.BytesBody, 1))
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, item), T: ir.TInt})
	b.free(item)
	b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
	b.jmp(top)
	b.at(end)
	return acc
}

// walkTable 走一遍哈希表那一串桶：只印键（hashset）或者 `键: 值`（dict，val 给非 nil 时）。
//
// 空格与墓碑由 @map_next 跳过，顺序不保证——所以字典与集合的印法不保证先后，
// 站上的例子只能用单键的表。"是不是头一格"用桶地址与数组头比，省一个计数器。
//
// en: walkTable walks the bucket run of a hash table, printing only keys (hashset) or `key:
// value` (dict, when val is not nil). Empty and tombstone slots are skipped by @map_next and
// order is not guaranteed, so a dict or hashset's printed order is unspecified and the
// site's examples can only use a single-key table. "Is this the first entry" is answered by
// comparing the bucket address against the array head, which saves a counter.
func (b *pb) walkTable(h, kd, vd ir.Val) ir.Val {
	acc := b.t()
	empty := b.str("")
	b.emit(&ir.Mov{D: acc, S: empty, T: ir.TInt})
	cur := b.ld(h, runtime.CellData)
	top, end, sep := b.fresh(), b.fresh(), b.fresh()
	b.at(top)
	bk := b.t()
	b.emit(&ir.Call{D: bk, HasD: true, Fn: "@map_next", Args: []ir.Val{h, cur},
		FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	b.jz(bk, end)
	b.emit(&ir.Bin{Op: "+", D: cur, A: bk, B: b.num(runtime.BucketPad), T: ir.TInt})
	// "是不是头一格"不能拿桶地址与数组头比：表是散列的，头一个位置occupied 往往不在 0 号桶上。
	// 攒出来的那段还等于空文本那一条，就说明什么都没拼过。
	b.eq(acc, empty, sep)
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, b.str(", ")), T: ir.TInt})
	b.at(sep)
	one := b.self(b.ld(bk, runtime.BucketKey), kd)
	if vd != nil {
		head := b.catFree(one, b.str(": "))
		val := b.self(b.ld(bk, runtime.BucketVal), vd)
		one = b.catFree(head, val)
		b.free(val)
	}
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, one), T: ir.TInt})
	b.free(one)
	b.jmp(top)
	b.at(end)
	return acc
}

// strOf 把形状表里那一格"从本条描述起算的字节偏移"换成一条文本句柄。
//
// 偏移指向的就是文本池里那一条（「字节数＋码点数＋UTF-8」那一整块的头），加出来已经是句柄了，
// 不再去解一次引用——多解那一次拿到的是"这条文本几个字节"，拿它当地址去拼串就是空指针同级的事。
//
// en: strOf turns the shape table's "byte offset measured from this description" into a
// text handle. The offset points exactly at the text pool entry (the header of the "byte
// count + code point count + UTF-8" block), so adding it already yields a handle -
// dereferencing once more would read "how many bytes this text has", and using that as an
// address for concatenation is the same class of null pointer.
func (b *pb) strOf(base ir.Val, off int) ir.Val {
	return b.bin("+", base, b.ld(base, off))
}

// shape 拼 `Name { a: 1, b: 甲 }`。挑这个形状的理由与写字面量用的那一个相同：
// 不新造记号。一个字段也没有的那一类返回 `Name { }`（空 struct 是允许的）。
//
// en: shape assembles `Name { a: 1, b: text }`. This form was chosen for the same reason as
// the literal spelling: no new notation. A type with no fields returns `Name { }` (an empty
// struct is allowed).
func (b *pb) shape(v, desc ir.Val) ir.Val {
	acc := b.cat(b.strOf(desc, pdName), b.str(" {"))
	n := b.ld(desc, pdNField)
	arr := b.bin("+", desc, b.ld(desc, pdFields))
	i := b.t()
	b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
	top, end, notFirst, sep := b.fresh(), b.fresh(), b.fresh(), b.fresh()
	b.at(top)
	b.ge(i, n, end)
	b.ne(i, b.zero(), notFirst)
	// 头一个位置前面是一个空格而不是逗号：`Point { x: 1, y: 2 }`，一个字段都没有时返回 `E { }`。
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, b.str(" ")), T: ir.TInt})
	b.jmp(sep)
	b.at(notFirst)
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, b.str(", ")), T: ir.TInt})
	b.at(sep)
	e := b.idx(arr, i, pFieldSz)
	foff := b.ld(e, pfOff)
	fdesc := b.bin("+", desc, b.ld(e, pfType))
	// 字段名那个位置也是"从本条描述算起的差"，加出来就是句柄（strOf 那一句讲的同一件事）。
	// 它指向的是形状表那块静态数据，不是堆上的一块，所以这一段用 `cat` 不用 `catFree`。
	name := b.cat(b.bin("+", desc, b.ld(e, pfName)), b.str(": "))
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, name), T: ir.TInt})
	b.free(name)
	fv := b.self(b.fieldVal(v, foff, fdesc), fdesc)
	b.emit(&ir.Mov{D: acc, S: b.catFree(acc, fv), T: ir.TInt})
	b.free(fv)
	b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
	b.jmp(top)
	b.at(end)
	return b.catFree(acc, b.str(" }"))
}

// fieldVal 从块里取一个字段的 8 字节。
//
// 窄字段（boolean、byte 占一字节，unicode 占四字节）必须按宽度取——整份搬八个字节会把
// 后面那个位置的头几个字节读进来。宽度不在描述里另存一个位置：它就是字段的种类，
// 于是按种类分头发三条不同宽度的读取，读进同一个槽。
//
// en: fieldVal takes the 8 bytes of one field out of a block. A narrow field (boolean and byte
// one byte, unicode four) must be read at its width - moving eight whole would pull in the
// first bytes of the next position. The width is not stored as another field in the
// description: it IS the field's kind, so three reads of different widths branch on the kind
// and land in the same slot.
func (b *pb) fieldVal(v, off, fdesc ir.Val) ir.Val {
	out := b.t()
	kind := b.ld(fdesc, pdKind)
	base := b.bin("+", v, off)
	narrow, one, four := b.fresh(), b.fresh(), b.fresh()
	b.eq(kind, b.num(int64(ir.KindBool)), one)
	b.eq(kind, b.num(int64(ir.KindByte)), one)
	b.eq(kind, b.num(int64(ir.KindChar)), four)
	b.emit(&ir.Load{D: out, Base: base, Off: 0, T: ir.TInt})
	b.jmp(narrow)
	b.at(one)
	b.emit(&ir.Load{D: out, Base: base, Off: 0, T: ir.TInt, Size: 1})
	b.jmp(narrow)
	b.at(four)
	b.emit(&ir.Load{D: out, Base: base, Off: 0, T: ir.TInt, Size: 4})
	b.at(narrow)
	return out
}

// boxed 印盒子里那一个值：先去"类型字→形状"那张表里找到里面那一个值的形状（boxDesc），
// 再按它走。值类型装箱时另起了一块，正文在头后面，所以那几号要从块里再取一层；
// class 的句柄本身就是盒子、struct 装箱也是一整块，这两种直接拿着块地址往下走。
//
// en: boxed prints the value inside a box: first find its shape in the "type word -> shape"
// table (boxDesc), then walk by it. A value type boxed gets a separate block whose body follows
// the header, so those numbers must be taken out of the block one more level; a class's handle
// IS the box and a boxed struct is a whole block, so those two continue with the block address
// directly.
func (b *pb) boxed(v, tab ir.Val) ir.Val {
	d := b.boxDesc(v, tab)
	return b.orNull2(d, func() ir.Val {
		k := b.ld(d, pdKind)
		val := b.t()
		// class 的句柄与 struct 的那一整块就是盒子本身，直接往下走；别的（int、byte、
		// bool、unicode、float、string）装在块里，要先从块的第 0 格取出来。
		direct, done := b.fresh(), b.fresh()
		b.eq(k, b.num(int64(ir.KindStruct)), direct)
		b.eq(k, b.num(int64(ir.KindClass)), direct)
		b.emit(&ir.Mov{D: val, S: b.ld(v, 0), T: ir.TInt})
		b.jmp(done)
		b.at(direct)
		b.emit(&ir.Mov{D: val, S: v, T: ir.TInt})
		b.at(done)
		return b.self(val, d)
	}, pEllipsis) // 表里没有这个类型字：只可能是内存被写坏了，给一格省略号不去猜
}

// orNull2 与 orNull 同一步，只是"是空的"那一侧给的是别的字（这里用不上 null）。
//
// en: orNull2 is the same step as orNull but with different text on the empty side (null is not
// what this one wants).
func (b *pb) orNull2(v ir.Val, body func() ir.Val, orWhat string) ir.Val {
	out := b.t()
	yes, done := b.fresh(), b.fresh()
	b.ne(v, b.zero(), yes)
	b.emit(&ir.Mov{D: out, S: b.str(orWhat), T: ir.TInt})
	b.jmp(done)
	b.at(yes)
	b.emit(&ir.Mov{D: out, S: body(), T: ir.TInt})
	b.at(done)
	return out
}

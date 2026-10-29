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

// destroy.go 实现析构例程 `nef.destroy(那一个值, 它的形状描述)`：把一块堆内存从里到外
// 拆解干净，再按 8 字节的块头把这一块归还给堆。
//
// 与打印机（print.go）用同一套工具和同一张形状表：递归、扫桶、按字段表逐位置前进，
// 两边做法一致，区别只在打印机返回一段文本、这一支不返回任何值。
//
// 三条规则：
//
//   - **一块只拆一次**。进入一块之前，先把块头里的类型字改写成"正在被拆"（DestroyingTag），
//     看到这个记号就直接不做处理——于是字段指回自己、同一块被两处指着时，都只拆一次。
//     已经在空闲链表上的块（FreeSentinel）同样不做处理。打印机为同一件事记录过一张
//     "遍历中已打开过哪些对象"的表；析构不需要：记号写在块自己身上，跟着块移动，
//     不占栈帧也不占堆。
//   - **文本这一版不还**。静态字面量带有占位头（@free 识别出尺寸为 0 时不做处理），
//     但 `struct` 的字段可以持有文本，而值类型每次交出都整份拷贝——两处拿着同一份文本时，
//     谁都无法确定自己是不是持有者。这一条由归属决定，不是遗漏：
//     `class` 一侧，读 `class` 的引用类型字段算交出，字段就是那块文本唯一的持有者，
//     能正常析构；现行规则维持不还，每圈 40.0 字节的实测由 `TestHeapSlopeByKind` 钉住。
//   - **不认识的形状不做处理**。数、布尔、码点、小数、`enum` 不在堆上（装箱的那一块
//     由盒子那一侧归还），文本按上一条不还，所以它们都落到最后一条：不做处理。
//
// en: destroy.go implements the destructor routine `nef.destroy(that value, its shape
// description)`: dismantle one heap block thoroughly from the inside out, then hand it back to
// the heap per its 8-byte header. It uses the same toolbox and the same shape table as the
// printer (print.go): recursing, scanning buckets and walking a field table one position at a
// time are identical jobs on both sides; the only difference is that the printer returns a
// piece of text and this routine returns nothing. Three rules:
//
//   - ONE BLOCK IS DISMANTLED ONCE. Before entering a block, the type word in its header is
//     rewritten to "being dismantled" (DestroyingTag); seeing that mark means no processing,
//     so a field pointing back at itself or one block referenced from two places is still
//     dismantled only once. A block already on the free list (FreeSentinel) is likewise left
//     untouched. The printer used to keep a table of "which objects were opened during
//     traversal"; dismantling needs none - the mark is written on the block itself, travels
//     with it, and costs neither frame nor heap.
//   - TEXT IS NOT RETURNED IN THIS VERSION. A static literal does carry a placeholder header
//     (@free recognises size 0 and does nothing), but a `struct` field may hold text and a
//     value type is copied wholesale on every handover, so when two places hold the same text
//     neither can tell whether it is the holder. This rule is pinned on ownership, not an
//     oversight: on the `class` side, reading a reference-typed `class` field counts as a
//     handover, so the field IS the sole holder of that text and can be dismantled; the
//     current rule keeps text un-returned, and the 40.0-bytes-per-iteration measurement is
//     pinned by TestHeapSlopeByKind.
//   - UNKNOWN SHAPES ARE NOT PROCESSED. Numbers, booleans, code points, floats and `enum`
//     are not on the heap (a boxed one is returned by the box side) and text is not returned
//     per the previous rule, so all of them fall through to the last case: no processing.
package lower

import (
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

// destroyFn 是这支例程的名字。用户函数名不允许含点，与这个名字撞不上。
//
// en: destroyFn is this routine's name. A user function name may not contain a dot, so it
// cannot collide.
const destroyFn = "nef.destroy"

// buildDestroy 排出 nef.destroy。重复调用不做处理（同一份程序只排一次）。
//
// en: buildDestroy lays out nef.destroy. Calling it again does nothing (one program lays it out
// once).
func buildDestroy(l *L) {
	f := l.p.Func(destroyFn)
	if len(f.Body) > 0 {
		return
	}
	f.Params, f.PT = 2, []ir.T{ir.TInt, ir.TInt}
	// 与打印机同一条规则：参数占用前两个槽，Slots 必须设为它们之后的位置，否则第一个 Temp 会覆盖传入的值。
	f.Slots = f.Params
	b := &pb{f: f, msg: l.msg, selfFn: destroyFn}
	v, desc := ir.L(0), ir.L(1)
	kind := b.ld(desc, pdKind)
	// 形状表里相互指向的那几个槽存的都是从本条描述起算的字节偏移，使用前一律加上本条描述的地址。
	child := func(off int) ir.Val { return b.bin("+", desc, b.ld(desc, off)) }

	cases := []struct {
		k  ir.DescKind
		fn func()
	}{
		// list：逐格向内拆解，再归还那一段数组，最后归还容器对象（归还对象那一步在 block 的收尾里）。
		{ir.KindList, func() {
			b.block(v, func() {
				ed := child(pdElem)
				n, data := b.ld(v, runtime.CellLen), b.ld(v, runtime.CellData)
				i := b.t()
				b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
				top, end := b.fresh(), b.fresh()
				b.at(top)
				b.ge(i, n, end)
				b.cellVal(b.ld(b.idx(data, i, 8), 0), ed)
				b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
				b.jmp(top)
				b.at(end)
				b.free(data) // 那一段数组先是它自己的持有者：扩缩时老的那一段由 @list_add 直接还
			})
		}},
		{ir.KindSet, func() { b.block(v, func() { b.walkKeys(v, child(pdElem), nil) }) }},
		{ir.KindDict, func() { b.block(v, func() { b.walkKeys(v, child(pdKey), child(pdElem)) }) }},
		// struct 与 class 用同一份字段表。值类型每次交出都另起一块，所以两处不会拿着同一块——
		// 但"字段指回自己"这种情况仍要靠"正在被拆"那个记号来拒绝重复拆解（交出记录管不到环）。
		{ir.KindStruct, func() { b.block(v, func() { b.walkFields(v, desc) }) }},
		{ir.KindClass, func() { b.block(v, func() { b.walkFields(v, desc) }) }},
		// bytes 种类：块里没有第二个持有者——正文就是那一段字节本身，一个字节一个位置，
		// 格子里住的又是值类型。所以拆它只有一步：把这一块归还给堆。
		// 仍套一层 block，为的是"正在被拆"那一记：同一块被两处指着时（只有归属规则出错才会这样）
		// 也只拆一次，不会破坏空闲链表。
		{ir.KindBytes, func() { b.block(v, func() {}) }},
		// 句柄一类：不套 block——"正在被拆"那一记写的是块头上的四个字节，而句柄不是堆上的一块，
		// 它头上的八个字节属于操作系统。收尾就是一次 closesocket；
		// 0（已经关过、或本来就没连上）由 @net_close 识别后不做处理。
		{ir.KindHandle, func() {
			b.emit(&ir.Call{Fn: "@net_close", Args: []ir.Val{v}, FT: []ir.T{ir.TInt}})
		}},
		// 子进程句柄：同一摆法，收尾叫 @proc_close——没等待过的就把那个进程结束掉。
		// 0（已经收过、或本来就没启动）由该例程识别后不做处理。
		{ir.KindProc, func() {
			b.emit(&ir.Call{Fn: "@proc_close", Args: []ir.Val{v}, FT: []ir.T{ir.TInt}})
		}},
		// 文本**不进这一张表**：从容器位置、字段、`d["k"]` 里读出来的串，那个位置仍是持有者
		// （读走不算交出），而句柄同时躺在别人手里——顺着形状表往里拆会把别人手里那一块拆掉。
		// 所以串只在"直接持有者"那一侧归还：降级那边给登记为持有者的 string
		// 名字发的是 `@free` 而不是 `nef.destroy`（`lower.own.direct`），这一支走到
		// 认不出的种类时不做处理。
		// 盒子：里面那一个值自己说了算。class 与 struct 装进盒子用的就是它们自己那一块
		// （句柄本身就带标记），所以整件事交给里面那一条描述去做；值类型装箱另起了一块，
		// 那一块就是盒子本身，直接归还。
		{ir.KindBox, func() {
			done := b.fresh()
			b.jz(v, done) // 空句柄：盒子里什么都没有
			d2 := b.boxDesc(v, child(pdElem))
			ref := b.t() // 里面是不是"句柄就是这一块"的那一类（class／struct）
			b.emit(&ir.Mov{D: ref, S: b.zero(), T: ir.TInt})
			notBox, isRef := b.fresh(), b.fresh()
			b.jz(d2, notBox) // 表里没有这个类型字：只可能是内存被写坏了，收这一块账就好
			k := b.ld(d2, pdKind)
			b.ne(k, b.num(int64(ir.KindClass)), isRef)
			b.eq(k, b.num(int64(ir.KindStruct)), isRef)
			b.jmp(notBox)
			b.at(isRef)
			b.emit(&ir.Mov{D: ref, S: b.num(1), T: ir.TInt})
			b.at(notBox)
			byInner := b.fresh()
			b.jz(ref, byInner)
			b.self(v, d2)
			b.jmp(done)
			b.at(byInner)
			b.free(v)
			b.at(done)
		}},
		// Json：那棵树的形状只有它自己知道，整件事交给 nef.json.free 去拆解。
		// 那一支自己识别"正在被拆"与"已在自由链上"两个记号，所以这里不套 block：
		// 树里每一块都是节点，抹记号由它逐块完成。
		{ir.KindJson, func() {
			b.emit(&ir.Call{Fn: fnJSONFree, Args: []ir.Val{v}, FT: []ir.T{ir.TInt}})
		}},
	}
	// 分派：逐个比较到各自那一段，段尾各自 ret；认不出的种类（数、文本、空句柄那些）落到
	// 这串比较之后——不做处理。
	body := make([]ir.Label, len(cases))
	for i, c := range cases {
		body[i] = b.fresh()
		b.eq(kind, b.num(int64(c.k)), body[i])
	}
	b.ret0()
	for i, c := range cases {
		b.at(body[i])
		c.fn()
		b.ret0() // 每一段自己完成：不这么做，走完这一段会顺着掉进下一段
	}
}

// block 是"这一块由我拆解"那段流程：句柄为空就不做处理；块头带有两个记号之一也不做
// 处理；否则先把块头标记为"正在被拆"，执行 body（它只负责向内拆解并归还里面的东西），
// 最后归还这一块本身。
//
// 判空必须发生在读块头之前：空句柄取成员就是拿 0 加偏移，运行期报 10092 号。
// 析构不同于打印——错误拆解比留下一块无人认领的内存严重得多，所以空句柄在这里直接返回。
//
// en: block is the "this block is mine to dismantle" sequence: an empty handle is left
// untouched; a header carrying either of the two marks is left untouched; otherwise the block
// is first stamped "being dismantled", the body runs (it only goes inward and returns what is
// inside), and the block itself is handed back at the end. The empty test must come before
// reading the header: dereferencing an empty handle adds an offset to 0, which the runtime
// reports as code 10092. Destruction is not printing - tearing a block down wrongly is far
// worse than leaving one block nobody claims, so an empty handle simply returns here.
func (b *pb) block(v ir.Val, body func()) {
	done := b.fresh()
	b.jz(v, done)
	tag := b.ldSz(v, -8, 4)
	b.eq(tag, b.num(runtime.FreeSentinel), done)
	b.eq(tag, b.num(runtime.DestroyingTag), done)
	b.st32(v, -8, b.num(runtime.DestroyingTag))
	body()
	b.free(v)
	b.at(done)
}

// walkKeys 走一遍哈希表那一串桶：键一律向内拆解，值只在 vd 不是 nil 时拆（dict 才有值）。
// 空格与墓碑由 @map_next 跳过；走法与打印那一边（walkTable）逐字相同，只是不拼串。
// 收尾归还的是桶数组那一段的**起点**——游标已经走过末端，停在尽头之后，不是起点。
//
// en: walkKeys walks the bucket run of a hash table: keys are always dismantled inward, values
// only when vd is not nil (only a dict has values). Empty and tombstone slots are skipped by
// @map_next; the traversal is word-for-word the printer's (walkTable), just without building a
// string. What is finally returned is the START of the bucket array - the cursor walked past
// the end.
func (b *pb) walkKeys(h, kd, vd ir.Val) {
	data := b.ld(h, runtime.CellData)
	// 游标必须放在**另一个位置**：写成 `cur := data` 的话，下面推进游标的那一句就把起点本身覆盖了
	// （中间码的 Temp 是同一个槽位、不是一个值），收尾归还的就是"最后一桶+24"那一个无效地址。
	cur := b.t()
	b.emit(&ir.Mov{D: cur, S: data, T: ir.TInt})
	top, end := b.fresh(), b.fresh()
	b.at(top)
	bk := b.call2("@map_next", h, cur)
	b.jz(bk, end)
	b.emit(&ir.Bin{Op: "+", D: cur, A: bk, B: b.num(runtime.BucketPad), T: ir.TInt})
	b.cellVal(b.ld(bk, runtime.BucketKey), kd)
	if vd != nil {
		b.cellVal(b.ld(bk, runtime.BucketVal), vd)
	}
	b.jmp(top)
	b.at(end)
	b.free(data)
}

// cellVal 拆解容器位置里的那一块。文本位置不走形状表递归（串没有下属），直接 @free；
// 其余种类沿形状表向内处理。
//
// 容器位置能拆、字段不能拆：引用类型进入容器那个位置就是交出，读出来又是**读走**
// （那个位置之后返回空句柄），所以除那个位置外没有第二个出处；读字段不算交出，
// 那一块可能正被别处持有，拆它就是拆解仍活着的对象。
//
// en: cellVal dismantles the block sitting in a container position. The text position does not
// recurse through the shape table (a string has no subordinates) and goes straight to @free;
// every other kind follows the shape table inward. A container position may be dismantled but
// a field may not: a reference type going into a container position is a handover, and reading
// it out is a READ-OUT (the position afterwards returns an empty handle), so there is no
// second source besides that position; reading a field out is not a handover, that block may
// be held elsewhere, and dismantling it would tear down a live object.
func (b *pb) cellVal(val, ed ir.Val) {
	notText, done := b.fresh(), b.fresh()
	k := b.t()
	b.emit(&ir.Mov{D: k, S: b.ld(ed, pdKind), T: ir.TInt})
	b.ne(k, b.num(int64(ir.KindString)), notText)
	b.free(val)
	b.jmp(done)
	b.at(notText)
	b.self(val, ed)
	b.at(done)
}

// walkFields 按登记的字段表逐位置向内拆解。读字段用的是与 `x.f` 和打印机相同的取法
// （同一个偏移与同一个窄宽度），不会出现读宽了的情况。
//
// en: walkFields follows the registered field table inward one position at a time. Reading a
// field uses the same path as `x.f` and the printer (same offset, same narrow width), so it
// cannot "read too wide here".
func (b *pb) walkFields(v, desc ir.Val) {
	n := b.ld(desc, pdNField)
	arr := b.bin("+", desc, b.ld(desc, pdFields))
	i := b.t()
	b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
	top, end := b.fresh(), b.fresh()
	b.at(top)
	b.ge(i, n, end)
	e := b.idx(arr, i, pFieldSz)
	foff := b.ld(e, pfOff)
	fdesc := b.bin("+", desc, b.ld(e, pfType))
	b.self(b.fieldVal(v, foff, fdesc), fdesc)
	b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
	b.jmp(top)
	b.at(end)
}

// boxDesc 拿块头上那四个字节（类型字）去"类型字→形状"那张表里找它的形状，
// 返回描述地址；表里没有就返回 0。打印机与析构器共用这一步（print.go 的 boxed 走的同一句）。
//
// en: boxDesc takes the four bytes at the head of a block (the type word), looks it up
// in the "type word -> shape" table and returns the description address, or 0 when the table
// has no such row. The printer and the destructor share this step (boxed in print.go calls the
// same thing).
func (b *pb) boxDesc(v, tab ir.Val) ir.Val {
	tag := b.ldSz(v, -8, 4)
	n := b.ld(tab, pdNTag)
	arr := b.bin("+", tab, b.ld(tab, pdTags))
	i := b.t()
	b.emit(&ir.Mov{D: i, S: b.zero(), T: ir.TInt})
	d := b.t()
	b.emit(&ir.Mov{D: d, S: b.zero(), T: ir.TInt})
	top, loop, end := b.fresh(), b.fresh(), b.fresh()
	b.at(top)
	b.ge(i, n, end)
	e := b.idx(arr, i, pTagSz)
	b.ne(b.ld(e, ptTag), tag, loop)
	b.emit(&ir.Mov{D: d, S: b.bin("+", tab, b.ld(e, ptDesc)), T: ir.TInt})
	b.jmp(end)
	b.at(loop)
	b.emit(&ir.Bin{Op: "+", D: i, A: i, B: b.num(1), T: ir.TInt})
	b.jmp(top)
	b.at(end)
	return d
}

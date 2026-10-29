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

package runtime

// listops.go 是挂在 list 上的六个动作：反转、按位置插入、按位置移除、
// 整只追加到尾部、取一段、去重成一个新表。六条都不另行检查边界与容量——插入与追加都走 @list_add，
// 按位置移除的边界在 @list_at 那一步已经检查过。
//
// en: listops.go holds the six actions attached to list: reverse, insert at a position, remove
// at a position, append a whole list to the tail, take a range, and deduplicate into a new
// table. None of the six rechecks bounds or capacity - insert and append both go through
// @list_add, and the bounds for removal by position were already checked at @list_at.

import (
	"neflang.org/nf/internal/x64"
)

// bumpVer 把"改过几次"推进一个计数。这段序列在 Go 侧发出，避免运行期多出三条只做这一件事的例程。
//
// en: bumpVer advances the "how many times mutated" counter. It is factored to the Go side
// and emitted as a short run, so the runtime does not gain three routines that only do this.
func bumpVer(b *rt, tabSlot int) {
	a := b.a
	b.get(x64.RDI, tabSlot)
	a.MovLoad(x64.RAX, x64.RDI, cVer)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, cVer, x64.RAX)
}

// @list_reverse(rcx=句柄)：首尾配对交换。空表与只有一个元素的表原样不动，版本号照常推进——仍算一次修改。
//
// en: @list_reverse (rcx = handle): swap head against tail pair by pair. An empty list and a
// one-element list stay as they are, but the version counter still advances - it was a
// mutation.
func listReverse(b *rt) {
	const (
		vTab = 0
		vLo  = 1
		vHi  = 2
	)
	a := b.a
	b.prologue()
	b.put(vTab, x64.RCX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(vLo, x64.RAX)
	b.get(x64.RAX, vTab)
	a.MovLoad(x64.RAX, x64.RAX, cLen)
	a.AddImm(x64.RAX, -1)
	b.put(vHi, x64.RAX)

	top, done := b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, vLo)
	b.get(x64.RDX, vHi)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.GE, done) // 有符号：长度是 0 时尾游标是 -1，无符号比会把它当成一个极大的数
	// 两处各是 数据块 + 游标*8，换一对
	b.get(x64.RSI, vTab)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, vLo)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	b.get(x64.RDI, vTab)
	a.MovLoad(x64.RDI, x64.RDI, cData)
	b.get(x64.R9, vHi)
	a.ShlImm(x64.R9, 3)
	a.Add(x64.RDI, x64.R9)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.MovLoad(x64.R8, x64.RDI, 0)
	a.MovStore(x64.RSI, 0, x64.R8)
	a.MovStore(x64.RDI, 0, x64.RAX)
	b.get(x64.RAX, vLo)
	a.AddImm(x64.RAX, 1)
	b.put(vLo, x64.RAX)
	b.get(x64.RAX, vHi)
	a.AddImm(x64.RAX, -1)
	b.put(vHi, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	bumpVer(b, vTab)
	b.epilogue()
}

// @list_insert(rcx=句柄, rdx=位置, r8=元素)：插进那一个位置前面，位置等于长度就是追加到末尾。
//
// 位置为负或比长度大当场抛 10031 号，与 `xs[i]` 同一条边界。追加走的是完整的 @list_add：
// 容量翻倍、旧数据块归还堆、数目与次数各推进一次，这些都不必重新计算；
// 追加完元素在末尾，再逐个位置往前交换，换到目标位置为止。
//
// en: @list_insert (rcx = handle, rdx = position, r8 = the element): insert before that
// position; a position equal to the length appends at the end. A negative or over-length
// position is code 10031 on the spot, the same boundary as `xs[i]`. The append itself reuses
// @list_add whole: capacity doubling, the old data block returned to the heap, count and
// version each advanced - none of it recounted. After appending the element sits at the tail
// and is then swapped one position left until it reaches the requested one.
func listInsert(b *rt) {
	const (
		iTab = 0
		iAt  = 1
		iVal = 2
		iCur = 3
	)
	a := b.a
	b.prologue()
	b.put(iTab, x64.RCX)
	b.put(iAt, x64.RDX)
	b.put(iVal, x64.R8)

	bad := b.label()
	b.get(x64.RDX, iAt)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.S, bad)
	b.get(x64.RAX, iTab)
	a.MovLoad(x64.RAX, x64.RAX, cLen)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.A, bad)

	b.get(x64.RCX, iTab)
	b.get(x64.RDX, iVal)
	b.call("@list_add")

	b.get(x64.RAX, iTab)
	a.MovLoad(x64.RAX, x64.RAX, cLen)
	a.AddImm(x64.RAX, -1)
	b.put(iCur, x64.RAX)
	roll, done := b.label(), b.label()
	a.Mark(roll)
	b.get(x64.RAX, iCur)
	b.get(x64.RDX, iAt)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.BE, done)
	b.get(x64.RSI, iTab)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, iCur)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.MovLoad(x64.RDX, x64.RSI, -8)
	a.MovStore(x64.RSI, 0, x64.RDX)
	a.MovStore(x64.RSI, -8, x64.RAX)
	b.get(x64.RAX, iCur)
	a.AddImm(x64.RAX, -1)
	b.put(iCur, x64.RAX)
	a.Jmp(roll)

	a.Mark(done)
	b.epilogue()

	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
	b.epilogue()
}

// @list_shift_out(rcx=句柄, rdx=位置)：rdx 之后的每个位置向前移动一格、数目减一、次数加一。
//
// 边界在 @list_at 那一步已经检查过（`xs.remove_at(i)` 先按 i 取到那个位置的地址），这里不再检查第二遍。
// 读走那个位置是降级侧的规则（引用类型读完把那个位置写空），前移一趟正好覆盖写空的这一处。
// 退下来的最后一个位置不做处理，与 @list_remove 现在的做法一致：长度之外的位置，遍历与析构都到不了。
//
// en: @list_shift_out (rcx = handle, rdx = position): every position after rdx moves one left,
// the count drops one, the version advances. The boundary was already checked at the @list_at
// step (`xs.remove_at(i)` first resolves the address by i), so it is not checked twice. Reading
// out that position is the lowering's rule (a reference type is emptied after
// reading) and the shifting pass writes exactly over the emptied slot. The vacated last
// position is left alone, consistent with what @list_remove does now: neither iteration nor
// destruction ever reaches a position outside the length.
func listShiftOut(b *rt) {
	const (
		oTab = 0
		oAt  = 1
		oCur = 2
	)
	a := b.a
	b.prologue()
	b.put(oTab, x64.RCX)
	b.put(oAt, x64.RDX)

	b.get(x64.RAX, oAt)
	b.put(oCur, x64.RAX)
	roll, done := b.label(), b.label()
	a.Mark(roll)
	b.get(x64.RAX, oCur)
	b.get(x64.RDX, oTab)
	a.MovLoad(x64.RDX, x64.RDX, cLen)
	a.AddImm(x64.RDX, -1)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, oTab)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, oCur)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RAX, x64.RSI, 8)
	a.MovStore(x64.RSI, 0, x64.RAX)
	b.get(x64.RAX, oCur)
	a.AddImm(x64.RAX, 1)
	b.put(oCur, x64.RAX)
	a.Jmp(roll)

	a.Mark(done)
	b.get(x64.RDI, oTab)
	// 退长度之前先把末尾那个位置写成 0：前移之后它与新的最后一个位置指向同一块，
	// 留着就是"一个值两个持有者"，这个 list 析构时同一块会被析构两次
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.AddImm(x64.RAX, -1)
	b.get(x64.RSI, oTab)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore(x64.RSI, 0, x64.RCX)

	b.get(x64.RDI, oTab)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.AddImm(x64.RAX, -1)
	a.MovStore(x64.RDI, cLen, x64.RAX)
	bumpVer(b, oTab)
	b.epilogue()
}

// @list_add_all(rcx=目标, rdx=来源, r8=0 原样／1 文本每个元素拷一份)：把来源的每个元素追加到尾部。
//
// 来源只被读取，不修改任何元素；进入目标的元素归目标持有。句柄型的元素在检查阶段就被拒绝，
// 能走到这里的只有值类型与文本两种（"交不出一份自己的拷贝"那条规则）。
//
// en: @list_add_all (rcx = target, rdx = source, r8 = 0 as-is / 1 copy each text element):
// append every element of the source to the tail. The source is only read, not modified;
// each element entering the target belongs to the target. Handle-typed elements were refused at
// check time, so only value types and text reach here (the "hand over a copy of your
// own" rule).
func listAddAll(b *rt) {
	const (
		nDst   = 0
		nSrc   = 1
		nWay   = 2
		nI     = 3
		nCount = 4
		nOne   = 5
	)
	a := b.a
	b.prologue()
	b.put(nDst, x64.RCX)
	b.put(nSrc, x64.RDX)
	b.put(nWay, x64.R8)

	b.get(x64.RAX, nSrc)
	a.MovLoad(x64.RAX, x64.RAX, cLen)
	b.put(nCount, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(nI, x64.RAX)

	top, done, raw := b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, nI)
	b.get(x64.RDX, nCount)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, nSrc)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, nI)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.put(nOne, x64.RAX)

	b.get(x64.RDX, nWay)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, raw)
	// 文本：@str_sub(那个对象, 起 0, 它自己的字节数) 拷一份出来作为末尾的元素
	b.get(x64.RCX, nOne)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, nOne)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	b.put(nOne, x64.RAX)
	a.Mark(raw)
	b.get(x64.RCX, nDst)
	b.get(x64.RDX, nOne)
	b.call("@list_add")
	b.get(x64.RAX, nI)
	a.AddImm(x64.RAX, 1)
	b.put(nI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.epilogue()
}

// @list_slice(rcx=句柄, rdx=起, r8=止, r9=0 原样／1 拷文本, 第五个参数槽=类型字)：取一段，含起不含止。
//
// 边界与 `s.slice` 完全一致：起或止为负、止超出长度、起比止大，都是 10031 号；起等于止返回一只空的。
//
// en: @list_slice (rcx = handle, rdx = from, r8 = to, r9 = 0 as-is / 1 copy text, fifth slot =
// type word): take a range, from included and to excluded. The bounds are identical to
// `s.slice`: negative from or to, to beyond the length, from greater than to are all 10031, and
// from equal to returns an empty list.
func listSlice(b *rt) {
	const (
		zTab   = 0
		zFrom  = 1
		zTo    = 2
		zWay   = 3
		zTag   = 4
		zOut   = 5
		zI     = 6
		zOne   = 7
		zCount = 8 // 要搬几个 = 止 - 起
	)
	a := b.a
	b.prologue()
	b.put(zTab, x64.RCX)
	b.put(zFrom, x64.RDX)
	b.put(zTo, x64.R8)
	b.put(zWay, x64.R9)
	a.MovLoad(x64.RAX, x64.RBP, arg5)
	b.put(zTag, x64.RAX)

	bad := b.label()
	b.get(x64.RDX, zFrom)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.S, bad)
	b.get(x64.RAX, zTo)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RDX, zTab)
	a.MovLoad(x64.RDX, x64.RDX, cLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, bad) // 止超出长度
	b.get(x64.RDX, zTo)
	b.get(x64.R8, zFrom)
	a.Cmp(x64.RDX, x64.R8)
	a.Jcc(x64.B, bad) // 起比止大

	a.Xor(x64.RCX, x64.RCX)
	b.get(x64.RDX, zTag)
	b.call("@list_new")
	b.put(zOut, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(zI, x64.RAX)
	b.get(x64.RAX, zTo)
	b.get(x64.RDX, zFrom)
	a.Sub(x64.RAX, x64.RDX)
	b.put(zCount, x64.RAX) // 含起不含止，起等于止就是空的那一只

	top, done, raw := b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, zI)
	b.get(x64.RDX, zCount)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, zTab)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, zFrom)
	a.Add(x64.RDX, x64.RAX) // 源下标 = 起 + 已经搬了几个
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.put(zOne, x64.RAX)

	b.get(x64.RDX, zWay)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, raw)
	b.get(x64.RCX, zOne)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, zOne)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	b.put(zOne, x64.RAX)
	a.Mark(raw)
	b.get(x64.RCX, zOut)
	b.get(x64.RDX, zOne)
	b.call("@list_add")
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1)
	b.put(zI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, zOut)
	b.epilogue()

	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
	b.epilogue()
}

// @list_to_set(rcx=句柄, rdx=0 原样／1 拷文本, r8=键的比法, r9=类型字)：去重后返回一只新表。
//
// 每一个元素先查一次是否在表里（@map_contains），已经在就跳过，不在才拷一份交进去——
// 与降级侧 mapStore 的"命中已有键时未收下的那把键要析构"是同一条判法，这里把检查放在拷之前，
// 所以那一份拷贝根本不会发生。来源那一只 list 一个元素都不动。
//
// en: @list_to_set (rcx = handle, rdx = 0 as-is / 1 copy text, r8 = how keys compare, r9 = type
// word): deduplicate into a new table. Each element first asks whether it is already present
// (@map_contains); present is skipped, absent gets one copy and is inserted - the same rule
// as lowering's mapStore "the key not accepted when a key already matches must be destructed",
// only checked before copying, so that copy never happens at all. The source list is
// untouched.
func listToSet(b *rt) {
	const (
		wTab  = 0
		wWay  = 1
		wMode = 2
		wTag  = 3
		wOut  = 4
		wI    = 5
		wOne  = 6
	)
	a := b.a
	b.prologue()
	b.put(wTab, x64.RCX)
	b.put(wWay, x64.RDX)
	b.put(wMode, x64.R8)
	b.put(wTag, x64.R9)

	a.Xor(x64.RCX, x64.RCX)
	b.get(x64.RDX, wTag)
	b.get(x64.R8, wMode)
	b.call("@map_new")
	b.put(wOut, x64.RAX)

	a.Xor(x64.RAX, x64.RAX)
	b.put(wI, x64.RAX)
	top, done, take := b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, wI)
	b.get(x64.RDX, wTab)
	a.MovLoad(x64.RDX, x64.RDX, cLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, wTab)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, wI)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.put(wOne, x64.RAX)

	b.get(x64.RCX, wOut)
	b.get(x64.RDX, wOne)
	b.call("@map_contains")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, take) // 已经在表里：这一个元素不拷也不放
	b.get(x64.RDX, wWay)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, take)
	b.get(x64.RCX, wOne)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wOne)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	b.put(wOne, x64.RAX)
	a.Mark(take)
	b.get(x64.RCX, wOut)
	b.get(x64.RDX, wOne)
	a.Xor(x64.R8, x64.R8)
	b.call("@map_put")
	b.get(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	b.put(wI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, wOut)
	b.epilogue()
}

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

// bytes.go 是内建那一档 `bytes` 的运行期零件。
//
// 这一档存在的理由：原先二进制数据只有一种载体 `list<byte>`，而容器每个位置一律 8 字节，
// 实测过——读一个 5,607,424 字节的文件，累计要 98,205,840 字节。TCP 收发要的也是一段
// 连着摆的字节，每收到一片再把 8N 的容器撑开一次代价更大。这一档把一个位置换成一个字节：
// 要多少字节，堆上就是多少。
//
// 块的形状与文本同一摆法（句柄指着自己的头一个位置，8 字节的块头在句柄之前）：
//
//	-8 │ 类型字 tagBytes │ -4 │ 尺寸 │ +0 │ 字节数 │ +8 │ 正文（一个位置一字节）│
//
// 只有"字节数"这一个位置：`bytes.new(容量)` 定下长度之后不再增减，所以没有"能装几样"那个位置，
// 也没有容器那一个"改过几次"（10033 号管的是遍历途中改容器，`bytes` 没有付费的遍历写法）。
//
// en: bytes.go is the runtime parts of the built-in `bytes` tier. Why the tier exists: binary
// data used to have one carrier only, `list<byte>`, and a container slot is always 8 bytes.
// Measured: reading a 5,607,424-byte file accumulated 98,205,840 bytes. TCP send and receive
// also want a contiguous run of bytes, and growing an 8N container once per chunk received is
// worse still. This tier makes the position one byte: whatever byte count is asked for is what
// the heap holds. The block's shape follows text exactly (the handle points at its own first
// position, with the 8-byte header before it): -8 type word tagBytes, -4 size, +0 byte count,
// +8 body (one byte per position). There is only the byte-count field:
// `bytes.new(capacity)` fixes the length and nothing grows or shrinks afterwards, so there is
// no "how many fit" slot and no container mutation counter (code 10033 guards modifying a
// container mid-iteration, and `bytes` has no paid iterating form).
package runtime

import "neflang.org/nf/internal/x64"

// 这一档在内存里的位置与它的类型字。编号规则只有一处，在降级那边（`internal/lower/lower.go`
// 的 tagBool…tagSet），这里照同一个号写一遍——与 `tagString = 6` 同一先例。
//
// en: This tier's positions in memory and its type word. The numbering rule lives in one place,
// on the lowering side (tagBool ... tagSet in internal/lower/lower.go); the same numbers are
// written out again here, following the precedent of `tagString = 6`.
const (
	BytesLen  = 0  // 有几个字节：`length(b)` 读这个位置，与 CellLen／StrBytes 同一个偏移
	BytesBody = 8  // 正文起点
	bytesHead = 8  // 句柄往上数，正文之前的那几个字节
	tagBytes  = 10 // 块头上那四个字节
)

// @bytes_new(rcx=字节数)：返回 rax=新的 `bytes` 句柄，那一段先清零。
//
// 负数在造块之前就拒绝：`@alloc` 收到的是一个回绕后的大数，会一路走到 bump 分配那条路上，
// 报的是"要不到内存"（10091 号）——那一句说的是这台机器给不出这么多，不是符号写错了。
// 号取容器那一段的兜底 10030 号：下标、slice 那三条都在那一段里，这一条不必另立一段。
//
// en: @bytes_new (rcx = byte count): returns rax = a new `bytes` handle, with the run zeroed
// first. A negative count is rejected before allocating: @alloc would
// receive a wrapped-around huge number and walk all the way to the bump allocation path,
// reporting "cannot allocate memory" (code 10091) - that sentence means this machine cannot
// supply that much, not "you wrote a sign wrong". The number chosen is 10030, the container
// band's fallback: subscript and the three slice members are all in that band, so this one
// needs no new band.
func bytesNew(b *rt) {
	const (
		zN   = 0 // 要几个字节
		zNew = 1
	)
	a := b.a
	b.prologue()
	b.put(zN, x64.RCX)
	neg := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.S, neg)

	b.get(x64.RCX, zN)
	a.AddImm(x64.RCX, bytesHead)
	a.MovImm(x64.RDX, tagBytes)
	b.call("@alloc")
	b.put(zNew, x64.RAX)
	b.get(x64.RDI, zNew)
	b.get(x64.RAX, zN)
	a.MovStore(x64.RDI, BytesLen, x64.RAX)
	a.Mov(x64.RAX, x64.RDI)
	b.epilogue()

	a.Mark(neg)
	b.throwWith(codeCell, "sBytesSize")
}

// @bytes_at(rcx=句柄, rdx=下标)：返回 rax=那一个字节在内存中的地址。
//
// 返回地址而不是值，与 `@list_at`、`@map_at` 同一条路：读、写两件事共用这一道越界检查（10031 号）。
// 负下标与不小于长度的那一个都算越界，与容器同一条判定。
//
// en: @bytes_at (rcx = handle, rdx = index): returns rax = the address of that byte in memory.
// An address rather than a value, the same path as @list_at and @map_at: reading and writing
// share this one range check (code 10031). A negative index and one not below the length are
// both out of range, the same rule as the containers.
func bytesAt(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)

	b.get(x64.RAX, 1)
	bad := b.label()
	ok := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R8, x64.RDI, BytesLen)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.B, ok)
	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
	a.Mark(ok)
	a.AddImm(x64.RAX, bytesHead)
	b.get(x64.RSI, 0)
	a.Add(x64.RSI, x64.RAX)
	a.Mov(x64.RAX, x64.RSI)
	b.epilogue()
}

// @bytes_slice(rcx=句柄, rdx=起, r8=止)：返回 rax=新的一块，含起不含止。
//
// 边界与 `@list_slice`、`@str_slice` 同一条：负、止比长度大、起比止大都算越界（10031 号），
// 起等于止返回空的一块。三个 slice 各按各的规则判定，否则使用者会分不清第二个位置是长度还是止。
//
// en: @bytes_slice (rcx = handle, rdx = from, r8 = to): returns rax = a new block, from
// included and to excluded. The bounds are the same rule as @list_slice and @str_slice:
// negative, to larger than the length, from larger than to are all out of range (10031), and
// from equal to returns an empty block. The three slices each apply their own bounds rule,
// otherwise a writer cannot tell whether the second position is a length or an end.
func bytesSlice(b *rt) {
	const (
		zSrc  = 0
		zFrom = 1
		zTo   = 2
		zOut  = 3
		zI    = 4
	)
	a := b.a
	b.prologue()
	b.put(zSrc, x64.RCX)
	b.put(zFrom, x64.RDX)
	b.put(zTo, x64.R8)

	bad := b.label()
	b.get(x64.RAX, zFrom)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RDX, zTo)
	b.get(x64.RSI, zSrc)
	a.MovLoad(x64.R8, x64.RSI, BytesLen)
	a.Cmp(x64.RDX, x64.R8)
	a.Jcc(x64.A, bad) // 止比长度大
	b.get(x64.RAX, zFrom)
	a.Cmp(x64.RDX, x64.RAX) // 止 < 起
	a.Jcc(x64.B, bad)

	a.Sub(x64.RDX, x64.RAX) // 要抄几个字节
	b.put(zI, x64.RDX)      // 先借一个槽位存长度
	b.get(x64.RCX, zI)
	b.call("@bytes_new")
	b.put(zOut, x64.RAX)
	b.get(x64.RAX, zI)
	a.Test(x64.RAX, x64.RAX)
	empty := b.label()
	a.Jcc(x64.E, empty)

	// 逐字节抄：源从"句柄＋8＋起"起走，落点从"新句柄＋8"起走。
	b.get(x64.RSI, zSrc)
	a.AddImm(x64.RSI, bytesHead)
	b.get(x64.RAX, zFrom)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RDI, zOut)
	a.AddImm(x64.RDI, bytesHead)
	b.get(x64.RCX, zI)
	top := b.label()
	a.Mark(top)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RCX, -1)
	a.Jcc(x64.NE, top)
	a.Mark(empty)

	b.get(x64.RAX, zOut)
	b.epilogue()

	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
}

// @bytes_join(rcx=清单)：返回 rax=把这些块连着摆的那一块新字节。
//
// 两遍走：第一遍数一共几个字节，第二遍造一块够大的、按清单的顺序一段一段抄进去。
// 中间不插任何东西——要把分隔插进去，就把分隔自己也做成一块 `bytes` 摆进清单，
// 同一例程的另一种结果，不必这一条例程自己管插什么。
//
// 这一例程存在的理由：`bytes` 的长度在 `bytes.new` 那一步定下之后不再增减，
// 而 TCP 上一段一段收来的数据要连着摆——base64 解出来的一块、拼好的一条 WebSocket 帧、
// 收来的半条消息，都得有一处"两块并成一块"的地方。没有这一条，那一侧就只能自己再开一块大的、
// 一段一段用下标搬，而搬这一件事在语言里看不见（`bytes` 没有 `write_at`）。
//
// 清单里那个位置是空句柄时：与 `text.join` 同一条路，当场抛 10092 号。空的清单是合法的空字节块，
// 与空的 `text.join` 返回空文本同一摆法。
//
// en: @bytes_join (rcx = list): returns rax = a new block with these runs laid out end to end.
// Two passes: the first counts the total bytes, the second allocates
// one big enough and copies each run in list order. Nothing is inserted between them - to
// insert a separator, make the separator a `bytes` block of its own and put it in the list;
// that is the same routine producing a different result, so this routine needs no separator
// logic. Why this routine exists: a `bytes` length is fixed by `bytes.new` and never grows,
// yet pieces received over TCP must be appended - a decoded base64 block, an
// assembled WebSocket frame, half a message all need one place where "two blocks become one".
// Without it that side would have to allocate a big block itself and copy by index, and copying
// is invisible in the language (`bytes` has no write_at). An empty handle in the list: the same
// path as text.join, code 10092 on the spot. An empty list is a legal empty block, the
// same shape as an empty text.join returning empty text.
func bytesJoin(b *rt) {
	const (
		kLst   = 0 // 那个清单对象
		kCnt   = 1 // 几块
		kData  = 2 // 数据块起点
		kTotal = 3 // 一共几个字节
		kI     = 4 // 走到第几块
		kNew   = 5 // 新造的那一块
		kW     = 6 // 抄写的落点
	)
	a := b.a
	b.prologue()
	b.put(kLst, x64.RCX)
	b.get(x64.RAX, kLst)
	a.MovLoad(x64.RAX, x64.RAX, CellLen)
	b.put(kCnt, x64.RAX)
	b.get(x64.RAX, kLst)
	a.MovLoad(x64.RAX, x64.RAX, CellData)
	b.put(kData, x64.RAX)

	// 第一遍：一共几个字节。句柄存在位置里、位置在数据块上、一个位置 8 字节
	nullOne := b.label()
	a.Xor(x64.RAX, x64.RAX)
	b.put(kTotal, x64.RAX)
	b.put(kI, x64.RAX)
	sum, sumEnd := b.label(), b.label()
	a.Mark(sum)
	b.get(x64.RAX, kI)
	b.get(x64.RDX, kCnt)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, sumEnd)
	b.get(x64.RSI, kData)
	b.get(x64.RDX, kI)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RSI, x64.RSI, 0)
	a.Test(x64.RSI, x64.RSI)
	a.Jcc(x64.E, nullOne)
	a.MovLoad(x64.RDX, x64.RSI, BytesLen)
	b.get(x64.RAX, kTotal)
	a.Add(x64.RAX, x64.RDX)
	b.put(kTotal, x64.RAX)
	b.get(x64.RAX, kI)
	a.AddImm(x64.RAX, 1)
	b.put(kI, x64.RAX)
	a.Jmp(sum)
	a.Mark(sumEnd)

	// 第二遍：造一块够大的，落点从正文起走，一段抄完就把落点往前挪那么多个字节
	b.get(x64.RCX, kTotal)
	b.call("@bytes_new")
	b.put(kNew, x64.RAX)
	b.get(x64.RDI, kNew)
	a.AddImm(x64.RDI, bytesHead)
	b.put(kW, x64.RDI)
	a.Xor(x64.RAX, x64.RAX)
	b.put(kI, x64.RAX)
	copyTop, copyEnd := b.label(), b.label()
	a.Mark(copyTop)
	b.get(x64.RAX, kI)
	b.get(x64.RDX, kCnt)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, copyEnd)
	b.get(x64.RSI, kData)
	b.get(x64.RDX, kI)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RSI, x64.RSI, 0) // 第一遍已验过不是空句柄
	a.MovLoad(x64.RCX, x64.RSI, BytesLen)
	a.AddImm(x64.RSI, bytesHead)
	b.get(x64.RDI, kW)
	one, oneEnd := b.label(), b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, oneEnd)
	a.Mark(one)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RCX, -1)
	a.Jcc(x64.NE, one)
	a.Mark(oneEnd)
	b.put(kW, x64.RDI)
	b.get(x64.RAX, kI)
	a.AddImm(x64.RAX, 1)
	b.put(kI, x64.RAX)
	a.Jmp(copyTop)
	a.Mark(copyEnd)

	b.get(x64.RAX, kNew)
	b.epilogue()

	a.Mark(nullOne)
	b.throwWith(codeNullRef, "sNullRef")
}

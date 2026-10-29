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

// text.go 是挂在文本上的几个运行期例程：取一截、按分隔拆成 list、把文本读成一个数。
//
// 文本对象的形状与内存布局约定一致：句柄后面第 0 个位置是字节数、第 8 个是码点数，再往后才是 UTF-8 字节。
// 切分都按字节进行，只在返回之前统计一次码点——码点的判定只有一条规则：
// 不是续字节（`(b & 0xC0) != 0x80`）的就是一个码点的开头。
//
// en: text.go holds the runtime routines on text: take a slice, split on a separator into
// a list, read text as a number. A text object follows the memory layout: the slot at offset 0
// after the handle is the byte count, offset 8 is the code point count, and only then come the
// UTF-8 bytes. Cutting and splitting work on bytes, and code points are counted once before
// handing the result back - one rule decides them: anything that is not a continuation byte
// (`(b & 0xC0) != 0x80`) starts a code point.
package runtime

import "neflang.org/nf/internal/x64"

// 与文本有关的内建错号与文本的类型字（编号规则只有一处，在降级那边）。
// 错号一律 10000 往上，0～9999 留给程序自己。
//
// en: The built-in error codes involving text and text's type word (the numbering rule
// exists in one place, on the lowering side). Codes are always 10000 and up; 0-9999 are left
// for the program.
const (
	codeNotNum  = 10012
	codeRange   = 10013
	codeNullRef = 10092
	tagString   = 6
	strHead     = 16 // 字节数 8 ＋ 码点数 8
	leadMask    = 0xC0
	contByte    = 0x80
)

// @str_cmp(rcx=文本甲, rdx=文本乙)：返回 rax=-1／0／1，按字节比较。
//
// UTF-8 的字节序就是码点序，所以不必先解码再比较——这是选择 UTF-8 的好处之一。
// 长度不等而较短的是较长者的前缀时，较短的算小。对空句柄做大小比较就是解引用它，
// 当场抛 10092 号；与空句柄判等不是同一条路径，判等在降级侧直接比较句柄，不会走到这里。
//
// en: @str_cmp (rcx = text a, rdx = text b): returns rax = -1/0/1, compared byte by byte.
// UTF-8's byte order is code point order, so no decoding is needed before comparing - one of
// the advantages of choosing UTF-8. When the lengths differ and the shorter is a prefix of the
// longer, the shorter is smaller. Comparing a null handle for order dereferences it and
// takes code 10092 on the spot; comparing a null handle for equality is a different path,
// handled by lowering comparing handles directly, which never reaches here.
func strCmp(b *rt) {
	const (
		xLA = 0
		xLB = 1
		xN  = 2
	)
	a := b.a
	b.prologue()
	less := b.label()
	greater := b.label()
	die92 := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, die92)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, die92)
	a.MovLoad(x64.R9, x64.RCX, 0)
	a.MovLoad(x64.R8, x64.RDX, 0)
	a.Mov(x64.R10, x64.R9)
	a.Cmp(x64.R10, x64.R8)
	byLen := b.label()
	a.Jcc(x64.B, byLen)
	a.Mov(x64.R10, x64.R8)
	a.Mark(byLen)
	tail := b.label()
	a.Mov(x64.RAX, x64.R9)
	b.put(xLA, x64.RAX)
	a.Mov(x64.RAX, x64.R8)
	b.put(xLB, x64.RAX)
	a.Mov(x64.RAX, x64.R10)
	b.put(xN, x64.RAX)

	a.LeaMem(x64.RSI, x64.RCX, strHead)
	a.LeaMem(x64.RDI, x64.RDX, strHead)
	top := b.label()
	a.Mark(top)
	b.get(x64.RAX, xN)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, tail)
	a.MovZX8(x64.R10, x64.RSI, 0)
	a.MovZX8(x64.R11, x64.RDI, 0)
	a.Cmp(x64.R10, x64.R11)
	a.Jcc(x64.B, less)
	a.Jcc(x64.A, greater)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RAX, -1)
	b.put(xN, x64.RAX)
	a.Jmp(top)

	// 一路都一样长：短的算小，一样长才算相等
	a.Mark(tail)
	b.get(x64.RAX, xLA)
	b.get(x64.RDX, xLB)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.B, less)
	a.Jcc(x64.A, greater)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
	a.Mark(less)
	a.MovImm(x64.RAX, -1)
	b.epilogue()
	a.Mark(greater)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
	a.Mark(die92)
	b.throwWith(codeNullRef, "sNullRef")
}

// @str_sub(rcx=文本对象, rdx=起字节, r8=几个字节)：返回 rax=新的文本对象。
//
// 越界不在这里挡：叫它的两条路（`s.slice` 与 `s.split`）自己把范围算好了，
// 这里再查一遍就是把同一件事说两遍。
//
// en: @str_sub (rcx = text object, rdx = start byte, r8 = byte count): returns rax = a new text
// object. Range checking is not done here: the two callers (`s.slice` and `s.split`) compute
// their bounds themselves, and checking again would state the same thing twice.
func strSub(b *rt) {
	const (
		xSrc = 0
		xOff = 1
		xLen = 2
		xNew = 3
	)
	a := b.a
	b.prologue()
	b.put(xSrc, x64.RCX)
	b.put(xOff, x64.RDX)
	b.put(xLen, x64.R8)

	// 新对象＝16 字节的头＋取来的那些字节，头里的标记是文本
	b.get(x64.RCX, xLen)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(xNew, x64.RAX)

	b.get(x64.RDI, xNew)
	b.get(x64.RAX, xLen)
	a.MovStore(x64.RDI, 0, x64.RAX)

	// 码点数：扫描这一段，统计不是续字节的个数
	b.get(x64.RSI, xSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RAX, xOff)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.R11, xLen)
	a.MovImm(x64.R8, leadMask)
	a.MovImm(x64.R9, contByte)
	a.Xor(x64.R10, x64.R10)
	a.Xor(x64.RAX, x64.RAX)
	cnt := b.label()
	skip := b.label()
	cdone := b.label()
	a.Mark(cnt)
	a.Test(x64.R11, x64.R11)
	a.Jcc(x64.E, cdone)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.And(x64.RAX, x64.R8)
	a.Cmp(x64.RAX, x64.R9)
	a.Jcc(x64.E, skip)
	a.AddImm(x64.R10, 1)
	a.Mark(skip)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.R11, -1)
	a.Jmp(cnt)
	a.Mark(cdone)
	b.get(x64.RDI, xNew)
	a.MovStore(x64.RDI, 8, x64.R10)

	// 整段搬过去：一次 8 字节，尾部不足 8 字节的再逐字节搬
	b.get(x64.RSI, xSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RAX, xOff)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RDI, xNew)
	a.AddImm(x64.RDI, strHead)
	b.get(x64.RCX, xLen)
	a.Mov(x64.RAX, x64.RCX)
	a.ShrImm(x64.RAX, 3) // 有几个完整的 8 字节块
	a.Mov(x64.R9, x64.RCX)
	a.AndImm(x64.R9, 7) // 尾部余下几个字节
	w8 := b.label()
	w8d := b.label()
	a.Mark(w8)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, w8d)
	a.MovLoad(x64.R11, x64.RSI, 0)
	a.MovStore(x64.RDI, 0, x64.R11)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.RDI, 8)
	a.AddImm(x64.RAX, -1)
	a.Jmp(w8)
	a.Mark(w8d)
	t1 := b.label()
	t1d := b.label()
	a.Mark(t1)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, t1d)
	a.MovZX8(x64.R11, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.R11)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.R9, -1)
	a.Jmp(t1)
	a.Mark(t1d)

	b.get(x64.RAX, xNew)
	b.epilogue()
}

// @str_split(rcx=文本对象, rdx=分隔文本对象)：返回 rax=list<string> 句柄。
//
// 语言层面只定义了"按分隔切开"，下面两条边界规则由运行期补齐：
// 分隔为空时按码点切，一个码点一个位置；整条文本没有匹配到分隔时，原样返回一个位置。
// 分隔两侧的位置留空：`a,b,` 切出 "a"、"b"、""，末尾那个空文本不丢弃。
//
// en: @str_split (rcx = text object, rdx = separator object): returns rax = a list<string>
// handle. The language defines only "cut on the separator"; the two boundary rules below are
// filled in by the runtime: an empty separator splits by code point, one element each, and
// a text containing no separator returns one element unchanged. The slots on both sides of a
// separator stay empty: `a,b,` splits into "a", "b", "" - the trailing empty text is kept.
func strSplit(b *rt) {
	const (
		xSrc  = 0
		xSep  = 1
		xN    = 2
		xM    = 3
		xOut  = 4
		xI    = 5
		xFrom = 6
	)
	a := b.a
	b.prologue()
	final := b.label()
	b.put(xSrc, x64.RCX)
	b.put(xSep, x64.RDX)
	a.MovLoad(x64.RAX, x64.RCX, 0)
	b.put(xN, x64.RAX)
	a.MovLoad(x64.RAX, x64.RDX, 0)
	b.put(xM, x64.RAX)

	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, tagString)
	b.call("@list_new")
	b.put(xOut, x64.RAX)
	a.MovImm(x64.RAX, 0)
	b.put(xI, x64.RAX)
	b.put(xFrom, x64.RAX)

	// 分隔为空走码点那一条
	b.get(x64.RAX, xM)
	a.Test(x64.RAX, x64.RAX)
	bySep := b.label()
	a.Jcc(x64.NE, bySep)

	rTop := b.label()
	rStep := b.label()
	a.Mark(rTop)
	b.get(x64.RAX, xI)
	b.get(x64.RDX, xN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, final)
	// 若是新码点的开头、且已跨过一个位置，就把上一个位置收尾
	b.get(x64.RSI, xSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, xI)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.MovImm(x64.R8, leadMask)
	a.And(x64.RAX, x64.R8)
	a.MovImm(x64.R9, contByte)
	a.Cmp(x64.RAX, x64.R9)
	a.Jcc(x64.E, rStep)
	b.get(x64.RAX, xI)
	b.get(x64.RDX, xFrom)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.E, rStep)
	b.get(x64.RCX, xSrc)
	b.get(x64.RDX, xFrom)
	b.get(x64.R8, xI)
	a.Sub(x64.R8, x64.RDX) // 这个位置有多长
	b.call("@str_sub")
	b.get(x64.RCX, xOut)
	a.Mov(x64.RDX, x64.RAX)
	b.call("@list_add")
	b.get(x64.RAX, xI)
	b.put(xFrom, x64.RAX)
	a.Mark(rStep)
	b.get(x64.RAX, xI)
	a.AddImm(x64.RAX, 1)
	b.put(xI, x64.RAX)
	a.Jmp(rTop)

	// 按分隔切：i 向前推进，每匹配一次产出一个位置
	a.Mark(bySep)
	mTop := b.label()
	mEdge := b.label()
	mMiss := b.label()
	mSame := b.label()
	cmp := b.label()
	a.Mark(mTop)
	// 剩余长度不足一条分隔，后面不可能再匹配到分隔
	b.get(x64.RAX, xI)
	b.get(x64.RDX, xN)
	a.Sub(x64.RDX, x64.RAX)
	b.get(x64.R8, xM)
	a.Cmp(x64.RDX, x64.R8)
	a.Jcc(x64.B, mEdge)
	b.get(x64.RSI, xSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RAX, xI)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RDI, xSep)
	a.AddImm(x64.RDI, strHead)
	b.get(x64.R9, xM)
	a.Mark(cmp)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, mSame)
	a.MovZX8(x64.R10, x64.RSI, 0)
	a.MovZX8(x64.R11, x64.RDI, 0)
	a.Cmp(x64.R10, x64.R11)
	a.Jcc(x64.NE, mMiss)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.R9, -1)
	a.Jmp(cmp)
	a.Mark(mSame)
	b.get(x64.RCX, xSrc)
	b.get(x64.RDX, xFrom)
	b.get(x64.R8, xI)
	a.Sub(x64.R8, x64.RDX)
	b.call("@str_sub")
	b.get(x64.RCX, xOut)
	a.Mov(x64.RDX, x64.RAX)
	b.call("@list_add")
	b.get(x64.RAX, xI)
	b.get(x64.R8, xM)
	a.Add(x64.RAX, x64.R8)
	b.put(xI, x64.RAX)
	b.put(xFrom, x64.RAX)
	a.Jmp(mTop)
	a.Mark(mMiss)
	b.get(x64.RAX, xI)
	a.AddImm(x64.RAX, 1)
	b.put(xI, x64.RAX)
	a.Jmp(mTop)
	a.Mark(mEdge)

	// 收尾：最后一段（可能为空）追加进去
	a.Mark(final)
	b.get(x64.RCX, xSrc)
	b.get(x64.RDX, xFrom)
	b.get(x64.R8, xN)
	a.Sub(x64.R8, x64.RDX)
	b.call("@str_sub")
	b.get(x64.RCX, xOut)
	a.Mov(x64.RDX, x64.RAX)
	b.call("@list_add")
	b.get(x64.RAX, xOut)
	b.epilogue()
}

// @str_to_int(rcx=文本对象)：返回 rax=那个数。
//
// 只接受"可选正负号＋一串十进制数字"，其余形式一律抛 10012 号；首尾空白不自动去掉
// （要去空白先调 `s.trim()`）。
// 超出 `int` 的值域抛 10013 号。上界按符号分开：正号到 2^63-1，负号到 2^63，
// 所以绝对值最大的那个负数算合法。乘法之前先比较商，发生回绕的比较不参与结果。
//
// en: @str_to_int (rcx = text object): returns rax = the number. Only an optional sign followed
// by a run of decimal digits is accepted; any other form is code 10012. Leading and trailing
// whitespace is not stripped automatically (call `s.trim()` first if whitespace must go).
// Past the range of `int` is code 10013. The ceiling splits by sign: up to 2^63-1 positive,
// up to 2^63 negative, so the most negative value is legal. The comparison against the
// quotient happens before the multiply, so a wrapped-around comparison never counts.
func strToInt(b *rt) {
	const (
		xLen  = 0
		xBase = 1
		xI    = 2
		xNeg  = 3
		xAcc  = 4
		xAny  = 5
	)
	a := b.a
	b.prologue()
	top := b.label()
	done := b.label()
	over := b.label()
	bad := b.label()
	sign := b.label()
	kept := b.label()
	havelim := b.label()
	a.MovLoad(x64.RAX, x64.RCX, 0)
	b.put(xLen, x64.RAX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // 空文本无法转成数
	a.AddImm(x64.RCX, strHead)
	b.put(xBase, x64.RCX)
	a.MovImm(x64.RAX, 0)
	b.put(xI, x64.RAX)
	b.put(xNeg, x64.RAX)
	b.put(xAcc, x64.RAX)
	b.put(xAny, x64.RAX)

	// 可选的符号
	b.get(x64.RSI, xBase)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, '-')
	notNeg := b.label()
	a.Jcc(x64.NE, notNeg)
	a.MovImm(x64.RAX, 1)
	b.put(xNeg, x64.RAX)
	b.get(x64.RAX, xI)
	a.AddImm(x64.RAX, 1)
	b.put(xI, x64.RAX)
	a.Jmp(sign)
	a.Mark(notNeg)
	a.CmpImm(x64.RAX, '+')
	a.Jcc(x64.NE, sign)
	b.get(x64.RAX, xI)
	a.AddImm(x64.RAX, 1)
	b.put(xI, x64.RAX)
	a.Mark(sign)

	a.Mark(top)
	b.get(x64.RAX, xI)
	b.get(x64.RDX, xLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, xBase)
	b.get(x64.RDX, xI)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.AddImm(x64.RAX, -'0')
	a.CmpImm(x64.RAX, 9)
	a.Jcc(x64.A, bad) // 无符号比较：不在 0～9 内的字符都落到上界之外
	// 先与上界的十分之一比较，再乘；乘完再比已经晚了，回绕后的数值看不出来
	b.get(x64.R11, xAcc)
	a.MovImm(x64.R8, int64(922337203685477580))
	a.Cmp(x64.R11, x64.R8)
	a.Jcc(x64.A, over)
	a.MovImm(x64.R9, 10)
	a.Imul(x64.R11, x64.R9)
	a.Add(x64.R11, x64.RAX) // RAX 是这一位
	// 累加值正好等于上界的十分之一时，最后一位还要看符号：正号到 7，负号到 8
	b.get(x64.RCX, xAcc)
	a.Cmp(x64.RCX, x64.R8)
	a.Jcc(x64.NE, kept)
	b.get(x64.RDX, xNeg)
	a.Test(x64.RDX, x64.RDX)
	a.MovImm(x64.RDX, 7)
	a.Jcc(x64.E, havelim)
	a.MovImm(x64.RDX, 8)
	a.Mark(havelim)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, over)
	a.Mark(kept)
	b.put(xAcc, x64.R11)
	a.MovImm(x64.RAX, 1)
	b.put(xAny, x64.RAX)
	b.get(x64.RAX, xI)
	a.AddImm(x64.RAX, 1)
	b.put(xI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, xAny)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // 空文本、只有一个符号
	b.get(x64.RAX, xAcc)
	b.get(x64.RDX, xNeg)
	a.Test(x64.RDX, x64.RDX)
	negate := b.label()
	a.Jcc(x64.E, negate)
	a.Neg(x64.RAX)
	a.Mark(negate)
	b.epilogue()
	a.Mark(over)
	b.throwWith(codeRange, "sRange")
	a.Mark(bad)
	b.throwWith(codeNotNum, "sNotNum")
}

// @str_int_base(rcx=文本, rdx=进制)：返回 rax=那一个整数的 64 位。
//
// 接受的形式与 text.to_int 相同，只多一个进制：可选的 +／- 号，后面一串该进制下合法的数字，
// 不允许空白、不允许 0x 前缀、不允许小数点。0～9 之外，a～z 与 A～Z 按 10～35 计值，
// 该值小于进制才算合法（进制在检查阶段已限定为 2、8、10、16 四个值，这里不再校验它）。
//
// 无法识别的形式一律抛 10012 号；超出 int 的值域抛 10013 号，与 text.to_int 同样是这两条。
// 累加走无符号：上界是 (2^64-1)/base，乘之前比较一次、乘加之后再比较一次是否回绕；
// 符号读完之后再比较有符号那一侧（负号最大到 2^63，那正是 int 的最小值）。
//
// en: @str_int_base (rcx = text, rdx = radix): returns rax = the 64 bits of that integer.
// The accepted form is text.to_int's with a radix added: an optional + or
// -, then digits legal in that radix, with no whitespace, no 0x prefix and no decimal point.
// Beyond 0-9, a-z and A-Z count as 10-35, and a digit is legal only if its value is below the
// radix (the radix itself was already limited to 2, 8, 10 or 16 at check time, so it is not
// re-validated here). An unrecognised form is the 10012 message and anything past the range
// of int is 10013, the same two as text.to_int. Accumulation is unsigned: the ceiling is
// (2^64-1)/base, compared once before multiplying and again after the multiply-add for
// wrap-around; once the sign is read, the signed side is compared too (a negative may reach
// 2^63, which is exactly int's minimum).
func strToIntBase(b *rt) {
	const (
		zLen  = 0
		zBase = 1
		zI    = 2
		zNeg  = 3
		zAcc  = 4
		zAny  = 5
		zSrc  = 6 // 第一个数字的地址
		zLim  = 7 // (2^64-1)/base
		zProd = 8 // 已乘、尚未加上本位的值
	)
	a := b.a
	b.prologue()
	b.put(zBase, x64.RDX)
	b.put(zSrc, x64.RCX)
	top := b.label()
	done := b.label()
	over := b.label()
	bad := b.label()
	minusWay := b.label()

	a.MovLoad(x64.RAX, x64.RCX, 0)
	b.put(zLen, x64.RAX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // 空文本无法转成数
	a.AddImm(x64.RCX, strHead)
	b.put(zSrc, x64.RCX)

	// 上界先算一次：(2^64-1)/base，rcx 是除数、rdx 先摆 0
	a.MovImm(x64.RAX, -1)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.RCX, zBase)
	a.Div1(x64.RCX)
	b.put(zLim, x64.RAX)

	a.Xor(x64.RAX, x64.RAX)
	b.put(zI, x64.RAX)
	b.put(zNeg, x64.RAX)
	b.put(zAcc, x64.RAX)
	b.put(zAny, x64.RAX)

	// 可选的符号，与 text.to_int 同一条
	b.get(x64.RSI, zSrc)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, '-')
	notNeg := b.label()
	a.Jcc(x64.NE, notNeg)
	a.MovImm(x64.RAX, 1)
	b.put(zNeg, x64.RAX)
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1)
	b.put(zI, x64.RAX)
	a.Jmp(top)
	a.Mark(notNeg)
	a.CmpImm(x64.RAX, '+')
	a.Jcc(x64.NE, top)
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1)
	b.put(zI, x64.RAX)

	a.Mark(top)
	b.get(x64.RAX, zI)
	b.get(x64.RDX, zLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, zSrc)
	b.get(x64.RDX, zI)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.R9, x64.RSI, 0) // 这一个字符
	// 转成数值：'0'～'9' 减 '0'；不在这十个里就按字母处理，减 'a' 加 10，不是字母就报错
	a.Mov(x64.RAX, x64.R9)
	a.AddImm(x64.RAX, -'0')
	a.CmpImm(x64.RAX, 9)
	notDigit, haveValue := b.label(), b.label()
	a.Jcc(x64.A, notDigit) // 无符号比较：不在 0～9 内的字符都落到上界之外
	a.Jmp(haveValue)
	a.Mark(notDigit)
	a.Mov(x64.RAX, x64.R9)
	a.OrImm(x64.RAX, 0x20)
	a.AddImm(x64.RAX, -'a')
	a.CmpImm(x64.RAX, 25)
	a.Jcc(x64.A, bad)
	a.AddImm(x64.RAX, 10)
	a.Mark(haveValue)
	b.get(x64.RDX, zBase)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, bad) // 这一位的数值不小于进制：整串到这儿就不合法了
	// 乘之前比上界，乘加之后比绕回
	b.get(x64.R11, zAcc)
	b.get(x64.RCX, zLim)
	a.Cmp(x64.R11, x64.RCX)
	a.Jcc(x64.A, over)
	b.get(x64.RCX, zBase)
	a.Imul(x64.R11, x64.RCX)
	b.put(zProd, x64.R11) // 先把乘好的数放一边：加完要比一次，绕回就是变小了
	a.Add(x64.R11, x64.RAX)
	b.get(x64.RCX, zProd)
	a.Cmp(x64.R11, x64.RCX)
	a.Jcc(x64.B, over) // 加完反而小了，是无符号绕回
	b.put(zAcc, x64.R11)
	a.MovImm(x64.RAX, 1)
	b.put(zAny, x64.RAX)
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1)
	b.put(zI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, zAny)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // 空文本、只有一个符号
	b.get(x64.RAX, zAcc)
	b.get(x64.RDX, zNeg)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, minusWay)
	// 正号一侧上界是 2^63-1
	a.MovImm(x64.RDX, int64(9223372036854775807))
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, over)
	b.epilogue()
	a.Mark(minusWay)
	// 负号一侧到 2^63 为止，该数取负正好是 int 的最小值
	a.MovImm(x64.RDX, int64(-9223372036854775807-1))
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, over)
	a.Neg(x64.RAX)
	b.epilogue()

	a.Mark(over)
	b.throwWith(codeRange, "sRange")
	a.Mark(bad)
	b.throwWith(codeNotNum, "sNotNum")
}

// @str_to_float(rcx=文本对象)：返回 rax=那一个小数的 64 位位型。
//
// 接受的形式：可选正负号、一串十进制数字、中间最多一个小数点、末尾可以带 e/E
// 加一个带符号的指数。其余形式一律抛 10012 号，与 `text.to_int` 同一条；首尾不跳过空白
// （要去空白先调 `s.trim()`）。
//
// 有效数字最多取 17 位：double 装不下更多，超出的位数不参与运算。整数部分每丢掉一位
// 就把指数加上一位，数量级因此不会偏移；小数部分丢掉的那几位本来就超出精度。
// 大于 double 返回 ±无穷、小于最小规格化档返回 0，与 IEEE 一致，不抛错。
//
// 10 的幂用快速幂计算：k 次乘法降为 log(k) 次，每次乘法引入一次舍入。
// 逐位相乘在 `1e300` 这类数上会累积出几百个 ulp，本版不采用那种做法。
//
// en: @str_to_float (rcx = text object): returns rax = the 64-bit pattern of that float.
// Accepted form: optional sign, a run of decimal digits, at most one decimal
// point, and an optional trailing e/E with a signed exponent. Any other form is code 10012, as
// in `text.to_int`, and whitespace is not skipped (call `s.trim()` first). Significant digits
// are taken to 17 at most: a double cannot hold more, so the later digits do not participate;
// on the integer
// side each dropped digit adds one to the exponent so the magnitude does not drift, and on the
// fraction side the dropped digits were never reachable. Larger than a double returns
// +/-infinity, smaller than the least normal band returns 0, matching IEEE, and nothing throws.
// Powers of ten are computed by exponentiation by squaring: k multiplications become log(k),
// one rounding per multiply; digit-by-digit multiply would accumulate hundreds of ulp on a
// number like `1e300`, and this version does not do that.
func strToFloat(b *rt) {
	const (
		fLen   = 0  // 文本的字节数
		fBase  = 1  // 第一个字节的地址
		fI     = 2  // 当前检查到第几个字节
		fNeg   = 3  // 整个数的符号
		fSig   = 4  // 已取的有效位数
		fScale = 5  // 要乘上 10 的几次方（可正可负）
		fDot   = 6  // 已经跨过小数点了
		fExpV  = 7  // 指数部分累加出的值
		fExpN  = 8  // 指数的符号
		fExpS  = 9  // 指数部分是否出现过数字
		fDir   = 10 // 数量级方向：0 乘、非 0 除
		fAny   = 11 // 是否出现过数字（一个都没有是 10012 号）
	)
	a := b.a
	b.prologue()
	bad := b.label()
	mant := b.label()
	mantStep := b.label()
	dotWay := b.label()
	expTop := b.label()
	expSign := b.label()
	expLoop := b.label()
	expDone := b.label()
	keepWay := b.label()
	fracWay := b.label()
	tail := b.label()
	finish := b.label()
	minusWay := b.label()
	absWay := b.label()
	powLoop := b.label()
	powBit := b.label()
	powDone := b.label()
	mulWay := b.label()
	negWay := b.label()
	out := b.label()

	a.MovLoad(x64.RAX, x64.RCX, StrBytes)
	b.put(fLen, x64.RAX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // 空文本无法转成数
	a.AddImm(x64.RCX, strHead)
	b.put(fBase, x64.RCX)
	clear := func(n int) {
		a.Xor(x64.RAX, x64.RAX)
		b.put(n, x64.RAX)
	}
	clear(fI)
	clear(fNeg)
	clear(fSig)
	clear(fScale)
	clear(fDot)
	clear(fExpV)
	clear(fExpN)
	clear(fExpS)
	clear(fDir)
	clear(fAny)

	a.Xorpd(0, 0) // 累加的有效数字从 0.0 起
	a.MovImm(x64.RCX, 0x4024000000000000)
	a.MovsdFromGPR(2, x64.RCX) // 10.0，逐位乘加都用它

	// 可选的符号
	b.get(x64.RSI, fBase)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	notNeg := b.label()
	a.CmpImm(x64.RAX, '-')
	a.Jcc(x64.NE, notNeg)
	a.MovImm(x64.RAX, 1)
	b.put(fNeg, x64.RAX)
	b.get(x64.RAX, fI)
	a.AddImm(x64.RAX, 1)
	b.put(fI, x64.RAX)
	a.Jmp(mant)
	a.Mark(notNeg)
	a.CmpImm(x64.RAX, '+')
	a.Jcc(x64.NE, mant)
	b.get(x64.RAX, fI)
	a.AddImm(x64.RAX, 1)
	b.put(fI, x64.RAX)

	// 尾数部分：数字、最多一个小数点，遇到 e/E 就切到指数部分
	a.Mark(mant)
	b.get(x64.RAX, fI)
	b.get(x64.RDX, fLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, tail)
	b.get(x64.RSI, fBase)
	b.get(x64.RDX, fI)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, '.')
	a.Jcc(x64.E, dotWay)
	a.CmpImm(x64.RAX, 'e')
	a.Jcc(x64.E, expTop)
	a.CmpImm(x64.RAX, 'E')
	a.Jcc(x64.E, expTop)
	a.AddImm(x64.RAX, -'0') // RAX 从这一格起装的是这一位的值
	a.CmpImm(x64.RAX, 9)
	a.Jcc(x64.A, bad) // 无符号比较：不在 0～9 内的字符都落到上界之外
	// 前 17 位有效数字进累加值，之后的丢掉——整数部分每丢掉一位，数量级要补回来。
	// 前导零不计入有效数字，否则 "0.000000000000000000001" 会在第一个 1 之前就到上限。
	b.get(x64.RCX, fSig)
	a.CmpImm(x64.RCX, 17)
	a.Jcc(x64.B, keepWay)
	b.get(x64.RAX, fDot)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, mantStep) // 小数那一段丢的位不影响数量级
	b.get(x64.RAX, fScale)
	a.AddImm(x64.RAX, 1)
	b.put(fScale, x64.RAX)
	a.Jmp(mantStep)
	a.Mark(keepWay)
	a.MovImm(x64.R8, 1)
	b.put(fAny, x64.R8)
	a.Mulsd(0, 2)
	a.CvtSI2SD(1, x64.RAX)
	a.Addsd(0, 1)
	// 前导零不占有效位：还没有见到有效位时，0 这一位只把小数点后的数量级前移一位
	countSig := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NE, countSig)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, fracWay)
	a.Mark(countSig)
	a.AddImm(x64.RCX, 1)
	b.put(fSig, x64.RCX)
	a.Mark(fracWay)
	b.get(x64.RAX, fDot)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, mantStep)
	b.get(x64.RAX, fScale)
	a.AddImm(x64.RAX, -1)
	b.put(fScale, x64.RAX)
	a.Jmp(mantStep)

	// 小数点：只能有一个
	a.Mark(dotWay)
	b.get(x64.RAX, fDot)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, bad)
	a.MovImm(x64.RAX, 1)
	b.put(fDot, x64.RAX)
	a.Mark(mantStep)
	b.get(x64.RAX, fI)
	a.AddImm(x64.RAX, 1)
	b.put(fI, x64.RAX)
	a.Jmp(mant)

	// 指数那一段：e/E 后面必须有至少一位数字
	a.Mark(expTop)
	b.get(x64.RAX, fI)
	a.AddImm(x64.RAX, 1)
	b.put(fI, x64.RAX)
	a.Mark(expSign)
	b.get(x64.RAX, fI)
	b.get(x64.RDX, fLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, expLoop)
	b.get(x64.RSI, fBase)
	b.get(x64.RDX, fI)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, '-')
	notExpNeg := b.label()
	a.Jcc(x64.NE, notExpNeg)
	a.MovImm(x64.RAX, 1)
	b.put(fExpN, x64.RAX)
	b.get(x64.RAX, fI)
	a.AddImm(x64.RAX, 1)
	b.put(fI, x64.RAX)
	a.Jmp(expLoop)
	a.Mark(notExpNeg)
	a.CmpImm(x64.RAX, '+')
	a.Jcc(x64.NE, expLoop)
	b.get(x64.RAX, fI)
	a.AddImm(x64.RAX, 1)
	b.put(fI, x64.RAX)

	a.Mark(expLoop)
	b.get(x64.RAX, fI)
	b.get(x64.RDX, fLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, expDone)
	b.get(x64.RSI, fBase)
	b.get(x64.RDX, fI)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.AddImm(x64.RAX, -'0')
	a.CmpImm(x64.RAX, 9)
	a.Jcc(x64.A, bad)
	b.get(x64.RDX, fI)
	a.AddImm(x64.RDX, 1)
	b.put(fI, x64.RDX)
	a.MovImm(x64.RDX, 1)
	b.put(fExpS, x64.RDX)
	b.get(x64.RDX, fExpV)
	a.CmpImm(x64.RDX, 100000)
	a.Jcc(x64.AE, expLoop) // 此时已超出 double 范围，继续累加没有意义，只需记录"见过数字"
	a.MovImm(x64.R8, 10)
	a.Imul(x64.RDX, x64.R8)
	a.Add(x64.RDX, x64.RAX)
	b.put(fExpV, x64.RDX)
	a.Jmp(expLoop)

	a.Mark(expDone)
	b.get(x64.RAX, fExpS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // `1e` 这类写法：指数部分没有任何数字
	a.Jmp(tail)

	// 两段都读完了，游标必须正好停在串尾
	a.Mark(tail)
	b.get(x64.RAX, fAny)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bad) // 没有任何数字（空文本、只有符号、只有小数点）
	b.get(x64.RAX, fI)
	b.get(x64.RDX, fLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, bad)

	a.Mark(finish)
	// 小数点产生的负档与指数部分合并，得到 10 的幂次
	b.get(x64.RAX, fScale)
	b.get(x64.RCX, fExpV)
	b.get(x64.RDX, fExpN)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, minusWay)
	a.Add(x64.RAX, x64.RCX)
	b.put(fScale, x64.RAX)
	a.Jmp(absWay)
	a.Mark(minusWay)
	a.Sub(x64.RAX, x64.RCX)
	b.put(fScale, x64.RAX)

	// 取绝对值，同时记录是按乘处理还是按除处理
	a.Mark(absWay)
	a.Mov(x64.RDX, x64.RAX)
	a.SarImm(x64.RDX, 63)
	b.put(fDir, x64.RDX)
	a.Xor(x64.RAX, x64.RDX)
	a.Sub(x64.RAX, x64.RDX)
	a.Mov(x64.RDX, x64.RAX)

	a.MovImm(x64.RAX, 0x3FF0000000000000)
	a.MovsdFromGPR(2, x64.RAX) // 累积的幂从 1.0 起
	a.MovImm(x64.RCX, 0x4024000000000000)
	a.MovsdFromGPR(1, x64.RCX) // 每次平方的是 10.0

	a.Mark(powLoop)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, powDone)
	a.Mov(x64.RAX, x64.RDX)
	a.AndImm(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, powBit)
	a.Mulsd(2, 1)
	a.Mark(powBit)
	a.Mulsd(1, 1)
	a.ShrImm(x64.RDX, 1)
	a.Jmp(powLoop)

	a.Mark(powDone)
	b.get(x64.RAX, fDir)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, mulWay)
	a.Divsd(0, 2)
	a.Jmp(negWay)
	a.Mark(mulWay)
	a.Mulsd(0, 2)

	a.Mark(negWay)
	b.get(x64.RAX, fNeg)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, out)
	a.MovImm(x64.RCX, bits(0x8000000000000000))
	a.MovsdFromGPR(1, x64.RCX)
	a.Xorpd(0, 1)

	a.Mark(out)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
	a.Mark(bad)
	b.throwWith(codeNotNum, "sNotNum")
}

// decodeRune 从 [RAX] 起解一个码点：码点进 RSI，占几个字节进 RDI。
//
// 只看首字节的高几位定长度（`0xxxxxxx`＝1、`110xxxxx`＝2、`1110xxxx`＝3、`11110xxx`＝4），
// 再把后续字节的低 6 位拼接上去。这里不再校验 UTF-8 的合法性：
// 能走到这一步的文本，要么是字面量（构造时就是合法的），要么是 `@file_read` 校验过的一整块，
// 要么是 `@str_split` 切出的片段（切点都在码点边界上）。
//
// 全程只用调用方的寄存器、不 call 任何东西，所以使用它的例程都不必先存槽位。
//
// en: decodeRune decodes one code point starting at [RAX]: the code point goes to RSI and its
// byte width to RDI. Only the lead byte's high bits decide the length (`0xxxxxxx`=1,
// `110xxxxx`=2, `1110xxxx`=3, `11110xxx`=4), and the low 6 bits of the following bytes are
// appended. UTF-8 validity is not rechecked here: any text reaching this step was either a
// literal (legal when built), a block validated by @file_read, or a piece cut by @str_split
// (every cut lands on a code point boundary). It uses only caller-saved registers and calls
// nothing, so routines using it need not park values in slots first.
func decodeRune(b *rt) {
	a := b.a
	one := b.label()
	two := b.label()
	three := b.label()
	done := b.label()

	a.MovZX8(x64.R8, x64.RAX, 0)
	a.MovImm(x64.RDI, 1)
	a.CmpImm(x64.R8, 0x80)
	a.Jcc(x64.B, one)
	a.CmpImm(x64.R8, 0xE0)
	a.Jcc(x64.B, two)
	a.CmpImm(x64.R8, 0xF0)
	a.Jcc(x64.B, three)

	a.MovImm(x64.R9, 0x07)
	a.And(x64.R8, x64.R9)
	a.Mov(x64.RSI, x64.R8)
	a.ShlImm(x64.RSI, 18)
	a.MovZX8(x64.R8, x64.RAX, 1)
	a.MovImm(x64.R9, 0x3F)
	a.And(x64.R8, x64.R9)
	a.ShlImm(x64.R8, 12)
	a.Or(x64.RSI, x64.R8)
	a.MovZX8(x64.R8, x64.RAX, 2)
	a.And(x64.R8, x64.R9)
	a.ShlImm(x64.R8, 6)
	a.Or(x64.RSI, x64.R8)
	a.MovZX8(x64.R8, x64.RAX, 3)
	a.And(x64.R8, x64.R9)
	a.Or(x64.RSI, x64.R8)
	a.MovImm(x64.RDI, 4)
	a.Jmp(done)

	a.Mark(three)
	a.MovImm(x64.R9, 0x0F)
	a.And(x64.R8, x64.R9)
	a.Mov(x64.RSI, x64.R8)
	a.ShlImm(x64.RSI, 12)
	a.MovZX8(x64.R8, x64.RAX, 1)
	a.MovImm(x64.R9, 0x3F)
	a.And(x64.R8, x64.R9)
	a.ShlImm(x64.R8, 6)
	a.Or(x64.RSI, x64.R8)
	a.MovZX8(x64.R8, x64.RAX, 2)
	a.And(x64.R8, x64.R9)
	a.Or(x64.RSI, x64.R8)
	a.MovImm(x64.RDI, 3)
	a.Jmp(done)

	a.Mark(two)
	a.MovImm(x64.R9, 0x1F)
	a.And(x64.R8, x64.R9)
	a.Mov(x64.RSI, x64.R8)
	a.ShlImm(x64.RSI, 6)
	a.MovZX8(x64.R8, x64.RAX, 1)
	a.MovImm(x64.R9, 0x3F)
	a.And(x64.R8, x64.R9)
	a.Or(x64.RSI, x64.R8)
	a.MovImm(x64.RDI, 2)
	a.Jmp(done)

	a.Mark(one)
	a.Mov(x64.RSI, x64.R8)
	a.Mark(done)
}

// @str_at(rcx=文本, rdx=第几个码点)：返回 rax=那个码点。
//
// 文本按 UTF-8 存储，第 i 个码点没有现成的地址，只能从字节 0 起逐个跳过 i 个码点——
// 这是 `O(位置)` 的操作。越界（含负数）当场抛 10031 号。
//
// en: @str_at (rcx = text, rdx = which code point): returns rax = that code point. Text is
// stored as UTF-8 and the i-th code point has no ready address, so it can only be found by
// skipping i code points from byte 0 - an O(index) operation. Out of range (negatives included)
// throws code 10031 on the spot.
func strAt(b *rt) {
	const (
		xStr = 0
		xIdx = 1
	)
	a := b.a
	b.prologue()
	b.put(xStr, x64.RCX)
	b.put(xIdx, x64.RDX)

	bad := b.label()
	walk := b.label()
	b.get(x64.RAX, xIdx)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RDI, xStr)
	a.MovLoad(x64.R8, x64.RDI, 8) // 码点数
	b.get(x64.RAX, xIdx)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.AE, bad)

	a.Mark(walk)
	b.get(x64.RAX, xStr)
	a.AddImm(x64.RAX, strHead)
	b.get(x64.R11, xIdx)
	skip := b.label()
	step := b.label()
	a.Mark(skip)
	a.Test(x64.R11, x64.R11)
	a.Jcc(x64.E, step)
	decodeRune(b)
	a.Add(x64.RAX, x64.RDI)
	a.AddImm(x64.R11, -1)
	a.Jmp(skip)

	a.Mark(step)
	decodeRune(b)
	a.Mov(x64.RAX, x64.RSI)
	b.epilogue()

	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
	b.epilogue()
}

// @str_dec(rcx=文本, rdx=字节偏移)：返回 rax=从那个位置起的那个码点。
//
// 遍历走的是这一条：游标本身就是字节偏移，不必每次都从头跳过去。
//
// en: @str_dec (rcx = text, rdx = byte offset): returns rax = the code point at that offset.
// Iteration goes through this routine: the cursor is already a byte offset, so there is no need
// to skip from the start every time.
func strDec(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	a.Add(x64.RAX, x64.RDX)
	decodeRune(b)
	a.Mov(x64.RAX, x64.RSI)
	b.epilogue()
}

// @str_adj(rcx=文本, rdx=当前字节偏移)：返回 rax=下一个位置的字节偏移。
//
// 与 @str_dec 拆成两条而不是打包一次返回，是因为一条中间码调用只返回一个值；
// 两条都只读首字节那个位置，拆开不增加运行开销。
//
// en: @str_adj (rcx = text, rdx = current byte offset): returns rax = the byte offset of the
// next position. This and @str_dec are two instructions rather than one call returning a pair
// because an intermediate-code call returns a single value; both only read the lead byte, so
// splitting them adds no runtime cost.
func strAdj(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.R10, x64.RDX) // 保存传入的偏移
	a.Mov(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	a.Add(x64.RAX, x64.RDX)
	decodeRune(b)
	a.Mov(x64.RAX, x64.R10)
	a.Add(x64.RAX, x64.RDI)
	b.epilogue()
}

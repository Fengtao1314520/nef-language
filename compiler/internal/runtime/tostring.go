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

// tostring.go 是"把一个值变成文本"的那几条运行期例程，服务 number.to_string。
//
// 造文本只有一条路：@alloc 要一块「字节数 8 ＋ 码点数 8 ＋ UTF-8」，标记是文本。
// 这一步在四个 @*_str 里重复四遍没有意义，所以抽成 @str_wrap——它收的是"一段已经编好的字节"，
// 不关心它是十进制、一个码点还是 true 那四个字母。
//
// 字节从哪儿来：整数用 @put_int 的除 10 取余路径（同一个 intChars），小数用 @put_float
// 定完位数之后的 bufOut（同一个 putFloat，只有收尾的去向不同），码点用 putChar 的现场 UTF-8 编码。
// 因此"输出到屏幕"与"转成文本"两条路径产生的字符完全一致。
//
// en: tostring.go holds the runtime routines that turn a value into text, serving
// number.to_string. Text is built one way: @alloc takes a block of "byte count 8 + code point
// count 8 + UTF-8", tagged as text. Repeating that step in four @*_str routines is pointless, so
// it became @str_wrap, which takes "a run of already encoded bytes" and does not care whether it
// is decimal, one code point, or the four letters of true. Where the bytes come from: integers
// use @put_int's divide-by-ten path (the same intChars), floats use putFloat's bufOut after the
// digit count is settled (the same putFloat, only the ending goes elsewhere), and code points use
// putChar's on-the-spot UTF-8 encoder. So the characters produced by "print to screen" and
// "convert to text" are guaranteed to be one and the same.
package runtime

import "neflang.org/nf/internal/x64"

// @str_wrap(rcx=字节地址, rdx=字节数, r8=码点数)：返回 rax=一个新的文本对象。
//
// 码点数由调用方给：那几条来源里字节数与码点数的关系各不相同（十进制与 true/false 相等，
// 一个码点是 1，其余需要另行统计），在这里数一遍等于把规则写死。
// 空的一段同样造得出来——长度两个槽写零，后面一个字节都没有。
//
// en: @str_wrap (rcx = byte address, rdx = byte count, r8 = code point count): returns rax = a
// new text object. The code point count comes from the caller, because its relation to the byte
// count differs per source (decimal and true/false are equal, one code point is 1, others must
// be counted) and counting here would hardcode that rule. An empty run is built the same way:
// both length slots written zero and not one byte after them.
func strWrap(b *rt) {
	const (
		wSrc   = 0 // 字节起点
		wN     = 1 // 字节数
		wRunes = 2 // 码点数
		wNew   = 3 // 新对象
	)
	a := b.a
	b.prologue()
	b.put(wSrc, x64.RCX)
	b.put(wN, x64.RDX)
	b.put(wRunes, x64.R8)

	b.get(x64.RCX, wN)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(wNew, x64.RAX)

	b.get(x64.RDI, wNew)
	b.get(x64.RAX, wN)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	b.get(x64.RAX, wRunes)
	a.MovStore(x64.RDI, StrCount, x64.RAX)
	a.AddImm(x64.RDI, strHead) // 抄写的落点
	b.get(x64.RSI, wSrc)

	// 逐字节抄。来源都在自己那一帧里或者静态区，长度不超过一次调用摆得下的量。
	loop := b.label()
	done := b.label()
	b.get(x64.RCX, wN)
	a.Mark(loop)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, done)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RCX, -1)
	a.Jmp(loop)
	a.Mark(done)

	b.get(x64.RAX, wNew)
	b.epilogue()
}

// @int_str(rcx=整数)：返回 rax=文本对象。数字的写法与 @put_int 一字不差，只是收尾不写 stdout。
//
// en: @int_str (rcx = integer): returns rax = a text object, the digits identical to
// @put_int's, only the ending writes nothing to stdout.
func intStr(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	intChars(b)
	a.Mov(x64.R8, x64.RDX) // 十进制与减号都是一个字节一个码点
	b.call("@str_wrap")
	b.epilogue()
}

// @char_str(rcx=码点)：返回 rax=装着这一个码点的文本，编 UTF-8 的那四档与 @put_char 同一条。
//
// en: @char_str (rcx = code point): returns rax = text holding that single code point, using
// the same four-band UTF-8 encoder as @put_char.
func charStr(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	a.MovLoad(x64.RAX, x64.RBP, -8)
	l1, l2, l3 := b.label(), b.label(), b.label()
	a.CmpImm(x64.RAX, 0x80)
	a.Jcc(x64.B, l1)
	a.CmpImm(x64.RAX, 0x800)
	a.Jcc(x64.B, l2)
	a.CmpImm(x64.RAX, 0x10000)
	a.Jcc(x64.B, l3)
	b.utf8(4)
	emit := b.label()
	a.Jmp(emit)
	a.Mark(l1)
	b.utf8(1)
	a.Jmp(emit)
	a.Mark(l2)
	b.utf8(2)
	a.Jmp(emit)
	a.Mark(l3)
	b.utf8(3)
	a.Mark(emit)
	a.MovImm(x64.R8, 1) // 一段 UTF-8 就是一个码点
	b.call("@str_wrap")
	b.epilogue()
}

// @bool_str(rcx=0|1)：返回 rax=true 或 false 那两个字面造出来的文本。
//
// en: @bool_str (rcx = 0|1): returns rax = text built from the two literals true or false.
func boolStr(b *rt) {
	a := b.a
	b.prologue()
	isFalse := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, isFalse)
	b.leaRIP(x64.RCX, "sTrue")
	a.MovImm(x64.RDX, 4)
	done := b.label()
	a.Jmp(done)
	a.Mark(isFalse)
	b.leaRIP(x64.RCX, "sFalse")
	a.MovImm(x64.RDX, 5)
	a.Mark(done)
	a.Mov(x64.R8, x64.RDX)
	b.call("@str_wrap")
	b.epilogue()
}

// @num_str_base(rcx=整数, rdx=进制)：返回 rax=那一个整数按这一进制写出来的文本。
//
// 负数写法与 @int_str 同一条：减号摆在最前头，绝对值按无符号取（int 的最小值取负正好是 2^63，
// 那一个数在无符号这一侧装得下，不必另开一支）。数字 0～9 之后接 a～f，全用小写。
// 进制在检查阶段卡在 2、8、10、16 四个数上，这里只当它是一个 2～36 之间的除数。
//
// 数字是一位一位往后产的，所以先往帧里那块 80 字节的缓冲从尾巴往前写，写完连着减号整段交给
// @str_wrap；缓冲在帧里，不向堆要第二块。
//
// en: @num_str_base (rcx = integer, rdx = radix): returns rax = that integer written in the
// given radix. Negatives follow @int_str: the minus goes first and the absolute
// value is taken unsigned (negating int's minimum gives exactly 2^63, which fits on the
// unsigned side, so no extra branch). After 0-9 the digits are a-f, all lowercase. The radix
// was pinned to 2, 8, 10 or 16 at check time, so here it is merely a divisor between 2 and 36.
// Digits are produced least significant first, so they are written backwards into an 80-byte
// frame buffer and the whole run, minus sign included, is handed to @str_wrap - the buffer is
// on the frame, so no second heap block is asked for.
func numToStrBase(b *rt) {
	const (
		sVal  = 0 // 剩下的那一段（无符号）
		sBase = 1 // 进制
		sNeg  = 2 // 要不要摆减号
		sEnd  = 3 // 缓冲的尾巴（一个字符之后）
		sCur  = 4 // 当前写到哪儿
	)
	a := b.a
	b.prologue()
	b.put(sBase, x64.RDX)
	a.Xor(x64.RDX, x64.RDX)
	b.put(sNeg, x64.RDX)
	a.Test(x64.RCX, x64.RCX)
	negWay := b.label()
	a.Jcc(x64.NS, negWay)
	a.MovImm(x64.RDX, 1)
	b.put(sNeg, x64.RDX)
	a.Neg(x64.RCX)
	a.Mark(negWay)
	b.put(sVal, x64.RCX)

	// 缓冲的落点：[rbp-0xA0, rbp-0x50)，从后往前写
	a.Mov(x64.RDI, x64.RBP)
	a.AddImm(x64.RDI, -0x50)
	b.put(sEnd, x64.RDI)
	b.put(sCur, x64.RDI)

	zero := b.label()
	top := b.label()
	wrap := b.label()
	a.Mark(top)
	b.get(x64.RAX, sVal)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, zero) // 一位都没有：整数 0 也要输出一个 "0"
	b.get(x64.RCX, sBase)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.RCX) // rax＝商，rdx＝这一位的数值
	b.put(sVal, x64.RAX)
	a.AddImm(x64.RDX, '0')
	isDigit := b.label()
	a.CmpImm(x64.RDX, '9')
	a.Jcc(x64.BE, isDigit)
	a.AddImm(x64.RDX, 39) // 10 往上：'0'+10+39 = 'a'+v-10，一个小加法换到字母那一段
	a.Mark(isDigit)
	a.AddImm(x64.RDI, -1)
	a.MovStore8(x64.RDI, 0, x64.RDX)
	b.put(sCur, x64.RDI)
	a.Jmp(top)

	a.Mark(zero)
	b.get(x64.RAX, sCur)
	b.get(x64.RDX, sEnd)
	a.Cmp(x64.RAX, x64.RDX) // 还在尾巴上就说明一个字节都没写（进来的就是 0）
	signed := b.label()
	a.Jcc(x64.NE, signed)
	a.Mov(x64.RDI, x64.RAX)
	a.AddImm(x64.RDI, -1)
	a.MovImm(x64.RDX, '0')
	a.MovStore8(x64.RDI, 0, x64.RDX)
	b.put(sCur, x64.RDI)
	a.Mark(signed)
	b.get(x64.RDX, sNeg)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, wrap)
	b.get(x64.RDI, sCur)
	a.AddImm(x64.RDI, -1)
	a.MovImm(x64.RDX, '-')
	a.MovStore8(x64.RDI, 0, x64.RDX)
	b.put(sCur, x64.RDI)
	a.Mark(wrap)
	b.get(x64.RCX, sCur)
	b.get(x64.RDX, sEnd)
	a.Sub(x64.RDX, x64.RCX) // 几个字符
	a.Mov(x64.R8, x64.RDX)  // 全是 ASCII：字节数就是码点数
	b.call("@str_wrap")
	b.epilogue()
}

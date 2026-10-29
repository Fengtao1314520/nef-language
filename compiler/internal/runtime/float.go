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

// float.go 实现 @put_float：把双精度打印成"最短往返、一定带小数点"的十进制。
//
// 步骤与 internal/rtfloat 里逐值比对过的两份实现一致，只是把定长二进制大整数换成
// "一个槽位九位十进制"的数组：三个数各展开一次，试位数的那一圈就只剩十进制里的运算——
// 截位、看下一位、比大小，没有一次多字乘法。
//
//	① 值与上下两个端点各展开成十进制数组（K×2^J：J≥0 就乘 2、幂记 0，J<0 就乘 5、幂记 J）。
//	② p 从 1 试到 17：截前 p 位、按"最近、平手取偶"舍入，与两端比大小，
//	   第一个落进区间内的就是答案——"照抄回去还是同一个数"的要求就是这个判定。
//	③ 按位数与幂摆出小数点，交 @write 写出去。
//
// 每个数都用同一种表示：控制块的四个槽位依次是「数组起点、使用的格数、十进制位数、
// 最低那一位对应 10 的几次方」。比大小先比最高位落在哪个量级（位数＋幂），
// 同一量级才从最高位往下逐位比，短的一侧按零补齐。
//
// 端点里最关键的一种情形：v 正好是 2 的整数次幂时，它的下邻居距离只有半个 ulp，
// 下端要按 (4m-1)×2^(e-2) 算。这个情形是在与 Go 那两份实现逐值比对时发现的，这里沿用同一套分支。
//
// en: float.go implements @put_float: print a double as the shortest roundtripping decimal that
// always carries a decimal point. The steps follow the two value-by-value verified
// implementations in internal/rtfloat, only the fixed-width binary big integer becomes an array
// of "nine decimal digits per slot": with the value and both endpoints expanded once into
// decimal, the digit-counting loop is pure decimal work - truncate, look at the next digit,
// compare - with not one multi-word multiply. 1) Expand the value and its two endpoints into
// decimal arrays (K x 2^J: for J >= 0 multiply by 2 and record power 0; for J < 0 multiply by 5
// and record J). 2) Try p from 1 to 17: take the first p digits, round them
// nearest/ties-to-even, compare against both endpoints, and the first that lands inside the
// interval is the answer - that test IS the "typed back it is still the same number"
// requirement. 3) Place the decimal point by digit count and power and hand it to @write. Every
// number uses the same representation: a four-slot control block of [array start, slots used,
// decimal digits, power of ten of the lowest digit]. Comparing first asks which magnitude the
// leading digit occupies (digits + power); only within the same magnitude do the digits compare
// from the top, the shorter side padded with zeroes. The key endpoint case: when v is an exact
// power of two, its lower neighbour is only half an ulp away, so the lower end must be computed
// as (4m-1) x 2^(e-2). That case was found during the value-by-value comparison against the Go
// implementations, and the same branch is kept here.
package runtime

import (
	"encoding/binary"

	"neflang.org/nf/internal/x64"
)

// 大整数的表示：一个槽位装九位十进制。最宽的一个数是 2^-1074 展开成十进制（768 位上下），
// 110 个槽位够它再涨好几轮，余量留给进位。
//
// en: The big-integer shape: one slot holds nine decimal digits. The widest number is 2^-1074
// expanded into decimal (around 768 digits), and 110 slots leave it room to grow several times
// over, with headroom kept for carries.
const (
	limbBase = 1000000000
	limbN    = 110

	cPtr  = 0  // 控制块：数字数组的起点
	cNL   = 8  // 使用的格数
	cND   = 16 // 十进制总位数
	cTail = 24 // 最低那一位对应 10 的几次方
	ctlSz = 32
)

// 静态缓冲的名字，dataBytes 里摆的就是这几块。
//
// en: The names of the static buffers; dataBytes lays out exactly these blocks.
const (
	bufV   = "fV"
	bufLo  = "fLo"
	bufHi  = "fHi"
	bufC   = "fC"
	bufOut = "fOut"
	ctlV   = "fCtlV"
	ctlLo  = "fCtlLo"
	ctlHi  = "fCtlHi"
	ctlC   = "fCtlC"
)

// floatData 摆放小数这一支的静态数据。名字与顺序都由这里定，
// 避免 Go map 的随机遍历顺序把偏移变成"每次编译都不一样"。
//
// en: floatData lays out the static data of the float branch. Names and order are fixed here so
// that Go's random map ordering cannot turn an offset into something that differs per compile.
func floatData(put func(name string, bs []byte)) {
	for _, n := range []string{bufV, bufLo, bufHi, bufC} {
		put(n, make([]byte, limbN*8))
	}
	put(bufOut, make([]byte, 512))
	for _, n := range []string{ctlV, ctlLo, ctlHi, ctlC} {
		put(n, make([]byte, ctlSz))
	}
}

// @f_mul(rcx=数组, rdx=格数, r8=乘数)：整个数组乘上一个小整数，有进位就在末尾增加一个格，
// 返回 rax=新的格数。
//
// 乘数最大是 2^29，一个格不到 10^9，乘积离 64 位的上界还很远，所以普通乘法就够。
//
// en: @f_mul (rcx = array, rdx = slots, r8 = multiplier): multiply the whole run by a small
// integer, appending a slot at the tail when there is a carry, and return rax = the new slot
// count. The largest multiplier is 2^29 and a slot is under 10^9, so the product stays well
// below the 64-bit bound and ordinary multiplication is enough.
func fMul(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.RSI, x64.RCX) // 游标
	a.Mov(x64.R10, x64.RDX) // 剩余格数
	a.Mov(x64.R9, x64.RDX)  // 总格数，返回时要交回
	a.Mov(x64.RDI, x64.R8)  // 乘数
	a.MovImm(x64.R8, limbBase)
	a.Xor(x64.R11, x64.R11) // 进位
	top := b.label()
	done := b.label()
	fin := b.label()
	a.Mark(top)
	a.Test(x64.R10, x64.R10)
	a.Jcc(x64.E, done)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.Imul(x64.RAX, x64.RDI)
	a.Add(x64.RAX, x64.R11)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8) // 商是新的进位，余数就是这个格的值
	a.MovStore(x64.RSI, 0, x64.RDX)
	a.Mov(x64.R11, x64.RAX)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.R10, -1)
	a.Jmp(top)
	a.Mark(done)
	a.Mov(x64.RAX, x64.R9)
	a.Test(x64.R11, x64.R11)
	a.Jcc(x64.E, fin)
	a.MovStore(x64.RSI, 0, x64.R11)
	a.AddImm(x64.RAX, 1)
	a.Mark(fin)
	b.epilogue()
}

// @f_hi(rcx=数组, rdx=格数, r8=要丢弃几位)：返回 rax=丢弃后剩下的数，
// rdx=被丢弃的那一截里是否有非零内容（1 有、0 全零）。
//
// 除以 10^m 不需要真做除法：m = 9·wl + wr，先整格丢弃，再在停住的那个格除一次 10^wr。
// Horner 只累 wl 以上的格，最后乘上 10^(9-wr) 把那个格的高位接上——
// 这样中间值总是商的一个前缀，绝不会比商本身大，18 位装得下。
//
// en: @f_hi (rcx = array, rdx = slots, r8 = how many digits to drop): returns rax = the number
// left after dropping, rdx = whether the dropped part held anything (1 yes, 0 all zeroes).
// Dividing by 10^m is not a real division: with m = 9*wl + wr, whole slots are discarded first
// and one slot is divided by 10^wr. Horner accumulates only the slots above wl and finally
// multiplies by 10^(9-wr) to re-attach that slot's high digits, which keeps every intermediate
// a prefix of the quotient - never larger than the quotient itself, so it fits in 18 digits.
func fHi(b *rt) {
	const (
		sPtr  = 0
		sNL   = 1
		sM    = 2
		sWL   = 3
		sWR   = 4
		sPowW = 5
		sPowL = 6
		sHi   = 7
		sLo   = 8
		sNZ   = 9
	)
	a := b.a
	b.prologue()
	b.put(sPtr, x64.RCX)
	b.put(sNL, x64.RDX)
	b.put(sM, x64.R8)
	a.MovImm(x64.R8, 9)
	b.get(x64.RAX, sM)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8) // 商是整格数，余数是那个格里要丢弃的位数
	b.put(sWL, x64.RAX)
	b.put(sWR, x64.RDX)
	// 10^wr，以及 10^(9-wr)＝一个格的上界除以它
	a.MovImm(x64.RAX, 1)
	b.put(sPowW, x64.RAX)
	b.get(x64.RCX, sWR)
	a.MovImm(x64.R8, 10)
	pw := b.label()
	pwd := b.label()
	a.Mark(pw)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, pwd)
	b.get(x64.RAX, sPowW)
	a.Imul(x64.RAX, x64.R8)
	b.put(sPowW, x64.RAX)
	a.AddImm(x64.RCX, -1)
	a.Jmp(pw)
	a.Mark(pwd)
	a.MovImm(x64.RAX, limbBase)
	b.get(x64.R8, sPowW)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8)
	b.put(sPowL, x64.RAX)
	// 停住的那个格拆开：高位那截并入商，低位那截计入"是否有非零内容"
	b.get(x64.RSI, sPtr)
	b.get(x64.RAX, sWL)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.get(x64.R8, sPowW)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8)
	b.put(sHi, x64.RAX)
	b.put(sLo, x64.RDX)
	a.Xor(x64.R11, x64.R11)
	a.Test(x64.RDX, x64.RDX)
	lo := b.label()
	a.Jcc(x64.E, lo)
	a.MovImm(x64.R11, 1)
	a.Mark(lo)
	// 被整格丢弃的那些里是否有非零内容
	b.get(x64.RSI, sPtr)
	b.get(x64.RCX, sWL)
	low := b.label()
	lowd := b.label()
	set := b.label()
	a.Mark(low)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, lowd)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, set)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.RCX, -1)
	a.Jmp(low)
	a.Mark(set)
	a.MovImm(x64.R11, 1)
	a.Mark(lowd)
	b.put(sNZ, x64.R11)
	// wl 以上的格做 Horner，再乘上 10^(9-wr) 接上那个格的高位
	b.get(x64.RSI, sPtr)
	b.get(x64.RAX, sNL)
	a.AddImm(x64.RAX, -1)
	a.Mov(x64.RCX, x64.RAX) // 当前停在哪个格
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.R10, sWL)
	a.Xor(x64.R9, x64.R9)
	hn := b.label()
	hnd := b.label()
	a.Mark(hn)
	a.Cmp(x64.RCX, x64.R10)
	a.Jcc(x64.LE, hnd)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.MovImm(x64.R8, limbBase)
	a.Imul(x64.R9, x64.R8)
	a.Add(x64.R9, x64.RAX)
	a.AddImm(x64.RSI, -8)
	a.AddImm(x64.RCX, -1)
	a.Jmp(hn)
	a.Mark(hnd)
	b.get(x64.R8, sPowL)
	a.Imul(x64.R9, x64.R8)
	b.get(x64.RAX, sHi)
	a.Add(x64.R9, x64.RAX)
	a.Mov(x64.RAX, x64.R9)
	b.get(x64.RDX, sNZ)
	b.epilogue()
}

// @f_expand(rcx=控制块, rdx=系数 K, r8=2 的幂 J，可以带负号)：
// 把 K×2^J 展开成十进制，格数、位数、幂一并填进控制块。
//
// 乘法次数最多一千出头，逐格相乘太慢：底是 5 时一次乘 5^9，
// 底是 2 时一次乘 2^29，零头逐次乘底本身——两种都还在"一格乘一个小整数"的安全范围里。
//
// en: @f_expand (rcx = control block, rdx = coefficient K, r8 = power of two J, possibly
// negative): expand K x 2^J into decimal and fill slot count, digit count and power into the
// control block. Multiplication happens just over a thousand times at most, so one slot at a
// time would be too slow: with base 5 multiply by 5^9 at a time, with base 2 multiply by 2^29,
// and the remainder by the base once - both stay inside the safe range of "one slot times a
// small integer".
func fExpand(b *rt) {
	const (
		xCtl  = 0
		xK    = 1
		xJ    = 2
		xPtr  = 3
		xNL   = 4
		xN    = 5
		xNeg  = 6
		xBase = 7
		xStep = 8
		xBig  = 9
		xOut  = 10
		xIn   = 11
		xND   = 12
		xTail = 13
	)
	a := b.a
	b.prologue()
	b.put(xCtl, x64.RCX)
	b.put(xK, x64.RDX)
	b.put(xJ, x64.R8)
	b.get(x64.RAX, xCtl)
	a.MovLoad(x64.RAX, x64.RAX, cPtr)
	b.put(xPtr, x64.RAX)
	// K 展开成三格：一格九位，2^54 也装得下
	b.get(x64.RSI, xPtr)
	b.get(x64.R9, xK)
	a.MovImm(x64.R8, limbBase)
	a.MovImm(x64.RCX, 3)
	sp := b.label()
	a.Mark(sp)
	a.Mov(x64.RAX, x64.R9)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8)
	a.MovStore(x64.RSI, 0, x64.RDX)
	a.Mov(x64.R9, x64.RAX)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.RCX, -1)
	a.Jcc(x64.NE, sp)
	a.MovImm(x64.RAX, 3)
	b.put(xNL, x64.RAX)
	// J 的符号一起决定底、要乘几次和幂
	b.get(x64.RAX, xJ)
	a.Test(x64.RAX, x64.RAX)
	neg := b.label()
	jn := b.label()
	a.Jcc(x64.S, neg)
	a.MovImm(x64.RAX, 0)
	b.put(xNeg, x64.RAX)
	b.put(xTail, x64.RAX) // 值本来就是整数：小数点不用挪
	b.get(x64.RAX, xJ)
	b.put(xN, x64.RAX)
	a.Jmp(jn)
	a.Mark(neg)
	a.MovImm(x64.RAX, 1)
	b.put(xNeg, x64.RAX)
	b.get(x64.RAX, xJ)
	a.Neg(x64.RAX)
	b.put(xN, x64.RAX)
	b.get(x64.RAX, xJ)
	b.put(xTail, x64.RAX)
	a.Mark(jn)
	b.get(x64.RAX, xNeg)
	a.Test(x64.RAX, x64.RAX)
	two := b.label()
	bd := b.label()
	a.Jcc(x64.E, two)
	a.MovImm(x64.RAX, 5)
	b.put(xBase, x64.RAX)
	a.MovImm(x64.RAX, 9)
	b.put(xStep, x64.RAX)
	a.MovImm(x64.RAX, 1953125) // 5^9
	b.put(xBig, x64.RAX)
	a.Jmp(bd)
	a.Mark(two)
	a.MovImm(x64.RAX, 2)
	b.put(xBase, x64.RAX)
	a.MovImm(x64.RAX, 29)
	b.put(xStep, x64.RAX)
	a.MovImm(x64.RAX, 536870912) // 2^29
	b.put(xBig, x64.RAX)
	a.Mark(bd)
	b.get(x64.RAX, xN)
	b.get(x64.R8, xStep)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8) // 批量乘几轮、零头几次
	b.put(xOut, x64.RAX)
	b.put(xIn, x64.RDX)
	oTop := b.label()
	oDone := b.label()
	a.Mark(oTop)
	b.get(x64.RAX, xOut)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, oDone)
	b.get(x64.RCX, xPtr)
	b.get(x64.RDX, xNL)
	b.get(x64.R8, xBig)
	b.call("@f_mul")
	b.put(xNL, x64.RAX)
	b.get(x64.RAX, xOut)
	a.AddImm(x64.RAX, -1)
	b.put(xOut, x64.RAX)
	a.Jmp(oTop)
	a.Mark(oDone)
	iTop := b.label()
	iDone := b.label()
	a.Mark(iTop)
	b.get(x64.RAX, xIn)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, iDone)
	b.get(x64.RCX, xPtr)
	b.get(x64.RDX, xNL)
	b.get(x64.R8, xBase)
	b.call("@f_mul")
	b.put(xNL, x64.RAX)
	b.get(x64.RAX, xIn)
	a.AddImm(x64.RAX, -1)
	b.put(xIn, x64.RAX)
	a.Jmp(iTop)
	a.Mark(iDone)
	// 从最高格往下找第一个非零格，位数就是「完整格数×9＋这一格有几位」
	b.get(x64.RSI, xPtr)
	b.get(x64.RAX, xNL)
	a.AddImm(x64.RAX, -1)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RCX, xNL)
	ts := b.label()
	allz := b.label()
	fnd := b.label()
	fin := b.label()
	a.Mark(ts)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, allz)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, fnd)
	a.AddImm(x64.RSI, -8)
	a.AddImm(x64.RCX, -1)
	a.Jmp(ts)
	a.Mark(fnd)
	a.MovImm(x64.R9, 1) // 这一格的位数
	a.MovImm(x64.R10, 10)
	a.MovImm(x64.RDI, 10)
	dg := b.label()
	dgd := b.label()
	a.Mark(dg)
	a.Cmp(x64.RAX, x64.R10)
	a.Jcc(x64.L, dgd)
	a.Imul(x64.R10, x64.RDI)
	a.AddImm(x64.R9, 1)
	a.Jmp(dg)
	a.Mark(dgd)
	a.Mov(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, -1)
	a.MovImm(x64.R8, 9)
	a.Imul(x64.RAX, x64.R8)
	a.Add(x64.RAX, x64.R9)
	b.put(xND, x64.RAX)
	b.put(xNL, x64.RCX)
	a.Jmp(fin)
	a.Mark(allz)
	a.MovImm(x64.RAX, 1)
	b.put(xND, x64.RAX)
	b.put(xNL, x64.RAX)
	a.Mark(fin)
	b.get(x64.RDI, xCtl)
	b.get(x64.RAX, xNL)
	a.MovStore(x64.RDI, cNL, x64.RAX)
	b.get(x64.RAX, xND)
	a.MovStore(x64.RDI, cND, x64.RAX)
	b.get(x64.RAX, xTail)
	a.MovStore(x64.RDI, cTail, x64.RAX)
	b.epilogue()
}

// @f_round(rcx=值的控制块, rdx=候选的控制块, r8=要留几位)：
// 按"最近、平手取偶"把值收成 p 位有效数字，填进候选那一个控制块。
//
// 平手判定看的是保留的最后一位是奇是偶，不是看 v 的尾数——两边都在中点时，
// 谁离偶数一侧近就归谁，Go 的参考实现也是这么写的，两边必须同一规则。
//
// en: @f_round (rcx = value's control block, rdx = candidate's control block, r8 = digits to
// keep): round the value to p significant digits, nearest with ties to even, and fill the
// candidate block. The tie is decided by whether the digit kept is odd or even, not by v's
// mantissa - when both sides sit on the midpoint, whoever is nearer the even side takes it,
// which is also how the Go reference is written, and the two must follow one rule.
func fRound(b *rt) {
	const (
		sV    = 0
		sC    = 1
		sP    = 2
		sND   = 3
		sTV   = 4
		sPtr  = 5
		sNL   = 6
		sK    = 7
		sT    = 8
		sNZ   = 9
		sQ    = 10
		sNext = 11
		sTail = 12
		sND2  = 13
	)
	a := b.a
	b.prologue()
	b.put(sV, x64.RCX)
	b.put(sC, x64.RDX)
	b.put(sP, x64.R8)
	b.get(x64.RCX, sV)
	a.MovLoad(x64.RAX, x64.RCX, cND)
	b.put(sND, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, cTail)
	b.put(sTV, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, cPtr)
	b.put(sPtr, x64.RAX)
	a.MovLoad(x64.RAX, x64.RCX, cNL)
	b.put(sNL, x64.RAX)
	fill := b.label()
	whole := b.label()
	b.get(x64.RAX, sND)
	b.get(x64.R8, sP)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.LE, whole)
	// 要丢弃 k 位；先取前 (p+1) 位，末位就是"下一位"，其余丢弃部分计入非零标志
	b.get(x64.RAX, sND)
	b.get(x64.R8, sP)
	a.Sub(x64.RAX, x64.R8)
	b.put(sK, x64.RAX)
	b.get(x64.RCX, sPtr)
	b.get(x64.RDX, sNL)
	b.get(x64.R8, sK)
	a.AddImm(x64.R8, -1)
	b.call("@f_hi")
	b.put(sT, x64.RAX)
	b.put(sNZ, x64.RDX)
	b.get(x64.RAX, sT)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.R8, 10)
	a.Div1(x64.R8) // 商是收好的 p 位，余数是下一位
	b.put(sQ, x64.RAX)
	b.put(sNext, x64.RDX)
	up := b.label()
	noUp := b.label()
	a.CmpImm(x64.RDX, 5)
	a.Jcc(x64.G, up)
	a.CmpImm(x64.RDX, 5)
	a.Jcc(x64.NE, noUp)
	b.get(x64.RAX, sNZ)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, up)
	b.get(x64.RAX, sQ)
	a.AndImm(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, up)
	a.Jmp(noUp)
	a.Mark(up)
	b.get(x64.RAX, sQ)
	a.AddImm(x64.RAX, 1)
	b.put(sQ, x64.RAX)
	a.Mark(noUp)
	b.get(x64.RAX, sTV)
	b.get(x64.R8, sK)
	a.Add(x64.RAX, x64.R8)
	b.put(sTail, x64.RAX)
	a.Jmp(fill)
	a.Mark(whole)
	// 位数本来就不满 p：整个数就是它，不做舍入
	b.get(x64.RCX, sPtr)
	b.get(x64.RDX, sNL)
	a.MovImm(x64.R8, 0)
	b.call("@f_hi")
	b.put(sQ, x64.RAX)
	b.get(x64.RAX, sTV)
	b.put(sTail, x64.RAX)
	a.Mark(fill)
	// 这一串有几位
	b.get(x64.R9, sQ)
	a.MovImm(x64.RAX, 1)
	a.MovImm(x64.R10, 10)
	a.MovImm(x64.RDI, 10)
	dg := b.label()
	dgd := b.label()
	a.Mark(dg)
	a.Cmp(x64.R9, x64.R10)
	a.Jcc(x64.L, dgd)
	a.Imul(x64.R10, x64.RDI)
	a.AddImm(x64.RAX, 1)
	a.Jmp(dg)
	a.Mark(dgd)
	b.put(sND2, x64.RAX)
	// 末尾的零去掉、幂跟着往上挪——"最短"的要求就靠这一步
	tr := b.label()
	trd := b.label()
	a.Mark(tr)
	b.get(x64.RAX, sND2)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.LE, trd)
	b.get(x64.RAX, sQ)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.R8, 10)
	a.Div1(x64.R8)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, trd)
	b.put(sQ, x64.RAX)
	b.get(x64.RAX, sND2)
	a.AddImm(x64.RAX, -1)
	b.put(sND2, x64.RAX)
	b.get(x64.RAX, sTail)
	a.AddImm(x64.RAX, 1)
	b.put(sTail, x64.RAX)
	a.Jmp(tr)
	a.Mark(trd)
	// 拆回每格九位的数组，写入候选那一个控制块
	b.get(x64.RSI, sC)
	a.MovLoad(x64.RSI, x64.RSI, cPtr)
	b.get(x64.R9, sQ)
	a.MovImm(x64.R8, limbBase)
	a.Xor(x64.R10, x64.R10)
	spl := b.label()
	spld := b.label()
	a.Mark(spl)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, spld)
	a.Mov(x64.RAX, x64.R9)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8)
	a.MovStore(x64.RSI, 0, x64.RDX)
	a.Mov(x64.R9, x64.RAX)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.R10, 1)
	a.Jmp(spl)
	a.Mark(spld)
	a.Test(x64.R10, x64.R10)
	zr := b.label()
	a.Jcc(x64.NE, zr)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RSI, 0, x64.RDX)
	a.AddImm(x64.R10, 1)
	a.Mark(zr)
	b.get(x64.RCX, sC)
	a.MovStore(x64.RCX, cNL, x64.R10)
	b.get(x64.RAX, sND2)
	a.MovStore(x64.RCX, cND, x64.RAX)
	b.get(x64.RAX, sTail)
	a.MovStore(x64.RCX, cTail, x64.RAX)
	b.epilogue()
}

// @f_digit(rcx=数组, rdx=第几位（从最低那端数）)：返回 rax=那一位是几。
//
// en: @f_digit (rcx = array, rdx = which digit counted from the low end): returns rax = that
// digit.
func fDigit(b *rt) {
	const (
		sPtr = 0
		sJ   = 1
		sWR  = 2
	)
	a := b.a
	b.prologue()
	b.put(sPtr, x64.RCX)
	b.put(sJ, x64.RDX)
	a.MovImm(x64.R8, 9)
	b.get(x64.RAX, sJ)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8) // 商是格号，余数是格里的第几位
	b.put(sWR, x64.RDX)
	a.ShlImm(x64.RAX, 3)
	b.get(x64.RSI, sPtr)
	a.Add(x64.RSI, x64.RAX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.get(x64.RDX, sWR)
	a.MovImm(x64.R8, 1)
	a.MovImm(x64.R9, 0)
	a.MovImm(x64.RDI, 10)
	pw := b.label()
	pwd := b.label()
	a.Mark(pw)
	a.Cmp(x64.R9, x64.RDX)
	a.Jcc(x64.GE, pwd)
	a.Imul(x64.R8, x64.RDI)
	a.AddImm(x64.R9, 1)
	a.Jmp(pw)
	a.Mark(pwd)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8) // 去掉低的几位
	a.MovImm(x64.R8, 10)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R8) // 余数就是那一位
	a.Mov(x64.RAX, x64.RDX)
	b.epilogue()
}

// @f_cmp(rcx=甲的控制块, rdx=乙的控制块)：返回 rax=-1／0／1。
//
// 两边都是规格化过的十进制串，所以先比最高位落在哪个量级（位数＋幂）：
// 不在同一量级，量级高的一定大；同一量级才从最高位往下逐位比，短的一侧按零补齐。
// "补齐之后是否还有非零内容"正是 @f_hi 的第二个返回值，不必再扫一遍低位。
//
// en: @f_cmp (rcx = a's control block, rdx = b's control block): returns rax = -1/0/1. Both
// sides are normalised decimal strings, so compare first which magnitude the leading digit
// occupies (digits + power): a higher magnitude is the larger number, and only within the same
// magnitude do the digits compare from the top with the shorter side zero-padded. "Is anything
// left after padding" is exactly @f_hi's second return value, so the low digits never need
// rescanning.
func fCmp(b *rt) {
	const (
		sA  = 0
		sB  = 1
		sI  = 2
		sDA = 3
		sDB = 4
	)
	a := b.a
	b.prologue()
	b.put(sA, x64.RCX)
	b.put(sB, x64.RDX)
	less := b.label()
	greater := b.label()
	aEnd := b.label()
	bEnd := b.label()
	eq := b.label()
	b.get(x64.RAX, sA)
	a.MovLoad(x64.R8, x64.RAX, cND)
	a.MovLoad(x64.R9, x64.RAX, cTail)
	a.Add(x64.R8, x64.R9) // 甲的量级
	b.get(x64.RAX, sB)
	a.MovLoad(x64.R10, x64.RAX, cND)
	a.MovLoad(x64.R11, x64.RAX, cTail)
	a.Add(x64.R10, x64.R11)
	a.Cmp(x64.R8, x64.R10)
	a.Jcc(x64.L, less)
	a.Jcc(x64.G, greater)
	a.MovImm(x64.RAX, 0)
	b.put(sI, x64.RAX)
	top := b.label()
	a.Mark(top)
	b.get(x64.RAX, sI)
	b.get(x64.RSI, sA)
	a.MovLoad(x64.R8, x64.RSI, cND)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.GE, aEnd)
	b.get(x64.RAX, sI)
	b.get(x64.RSI, sB)
	a.MovLoad(x64.R9, x64.RSI, cND)
	a.Cmp(x64.RAX, x64.R9)
	a.Jcc(x64.GE, bEnd)
	// 两边都还有位：各自取从最高往下第 i 位
	b.get(x64.RSI, sA)
	a.MovLoad(x64.RCX, x64.RSI, cPtr)
	a.MovLoad(x64.RDX, x64.RSI, cND)
	b.get(x64.RAX, sI)
	a.Sub(x64.RDX, x64.RAX)
	a.AddImm(x64.RDX, -1)
	b.call("@f_digit")
	b.put(sDA, x64.RAX)
	b.get(x64.RSI, sB)
	a.MovLoad(x64.RCX, x64.RSI, cPtr)
	a.MovLoad(x64.RDX, x64.RSI, cND)
	b.get(x64.RAX, sI)
	a.Sub(x64.RDX, x64.RAX)
	a.AddImm(x64.RDX, -1)
	b.call("@f_digit")
	b.put(sDB, x64.RAX)
	b.get(x64.RAX, sDA)
	b.get(x64.RDX, sDB)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.L, less)
	a.Jcc(x64.G, greater)
	b.get(x64.RAX, sI)
	a.AddImm(x64.RAX, 1)
	b.put(sI, x64.RAX)
	a.Jmp(top)
	a.Mark(aEnd)
	// 甲先到头：乙剩余部分有非零内容就是乙大
	b.get(x64.RAX, sI)
	b.get(x64.RSI, sB)
	a.MovLoad(x64.R8, x64.RSI, cND)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.GE, eq)
	a.MovLoad(x64.RCX, x64.RSI, cPtr)
	a.MovLoad(x64.RDX, x64.RSI, cNL)
	b.get(x64.R8, sI)
	a.MovLoad(x64.R9, x64.RSI, cND)
	a.Sub(x64.R9, x64.R8)
	a.Mov(x64.R8, x64.R9)
	b.call("@f_hi")
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, eq)
	a.Jmp(less)
	a.Mark(bEnd)
	// 乙先到头：甲剩余部分有非零内容就是甲大
	a.MovLoad(x64.R8, x64.RSI, cND)
	b.get(x64.RAX, sI)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.GE, eq)
	b.get(x64.RSI, sA)
	a.MovLoad(x64.RCX, x64.RSI, cPtr)
	a.MovLoad(x64.RDX, x64.RSI, cNL)
	b.get(x64.R8, sI)
	a.MovLoad(x64.R9, x64.RSI, cND)
	a.Sub(x64.R9, x64.R8)
	a.Mov(x64.R8, x64.R9)
	b.call("@f_hi")
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, eq)
	a.Jmp(greater)
	a.Mark(eq)
	a.MovImm(x64.RAX, 0)
	b.epilogue()
	a.Mark(less)
	a.MovImm(x64.RAX, -1)
	b.epilogue()
	a.Mark(greater)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
}

// @f_layout(rcx=候选的控制块, rdx=要不要负号)：返回 rax=字节数，字节写入 bufOut。
//
// 幂说的是最后一位的位置，所以"小数点前占几位"＝位数＋幂。三种排法：
// 全在小数点后面（开头补零）、小数点落在串中间、末尾补零再写".0"——
// 输出不用指数写法，而且一定带一个小数点。
//
// en: @f_layout (rcx = candidate's control block, rdx = whether a sign is needed): returns rax
// = byte count, with the bytes placed in bufOut. The power says where the LAST digit sits, so
// "digits before the point" = digit count + power. Three arrangements: everything after the
// point (leading zeroes), the point inside the string, or zeroes appended and a ".0" - the
// output uses no exponent notation and always carries a decimal point.
func fLayout(b *rt) {
	const (
		sC    = 0
		sPtr  = 1
		sND   = 2
		sTail = 3
		sHead = 4
		sI    = 5
		sJ    = 6
		sEnd  = 7
		sZ    = 8
		sD    = 9
		sOut  = 10
	)
	a := b.a
	b.prologue()
	b.put(sC, x64.RCX)
	b.put(sZ, x64.RDX) // 负号先借 sZ 存一下，下面立刻取出来
	b.leaRIP(x64.RAX, bufOut)
	b.put(sOut, x64.RAX)
	a.MovImm(x64.RAX, 0)
	b.put(sI, x64.RAX)
	b.get(x64.RAX, sC)
	a.MovLoad(x64.R8, x64.RAX, cPtr)
	b.put(sPtr, x64.R8)
	a.MovLoad(x64.R8, x64.RAX, cND)
	b.put(sND, x64.R8)
	a.MovLoad(x64.R9, x64.RAX, cTail)
	b.put(sTail, x64.R9)
	a.Add(x64.R8, x64.R9)
	b.put(sHead, x64.R8)
	// 往缓冲里写一个字节，下标跟着往前移
	out := func(ch int64) {
		b.get(x64.RSI, sOut)
		b.get(x64.RAX, sI)
		a.Add(x64.RSI, x64.RAX)
		a.MovImm(x64.RDX, ch)
		a.MovStore8(x64.RSI, 0, x64.RDX)
		b.get(x64.RAX, sI)
		a.AddImm(x64.RAX, 1)
		b.put(sI, x64.RAX)
	}
	// sJ 从当前位置走到 sEnd，把那几位逐个写出去
	digits := func() {
		top := b.label()
		done := b.label()
		a.Mark(top)
		b.get(x64.RAX, sJ)
		b.get(x64.RDX, sEnd)
		a.Cmp(x64.RAX, x64.RDX)
		a.Jcc(x64.GE, done)
		b.get(x64.RCX, sPtr)
		b.get(x64.RDX, sND)
		b.get(x64.RAX, sJ)
		a.Sub(x64.RDX, x64.RAX)
		a.AddImm(x64.RDX, -1)
		b.call("@f_digit")
		b.put(sD, x64.RAX)
		b.get(x64.RSI, sOut)
		b.get(x64.RAX, sI)
		a.Add(x64.RSI, x64.RAX)
		b.get(x64.RAX, sD)
		a.AddImm(x64.RAX, '0')
		a.MovStore8(x64.RSI, 0, x64.RAX)
		b.get(x64.RAX, sI)
		a.AddImm(x64.RAX, 1)
		b.put(sI, x64.RAX)
		b.get(x64.RAX, sJ)
		a.AddImm(x64.RAX, 1)
		b.put(sJ, x64.RAX)
		a.Jmp(top)
		a.Mark(done)
	}
	// sZ 还有几就补几个零
	zeros := func() {
		top := b.label()
		done := b.label()
		a.Mark(top)
		b.get(x64.RAX, sZ)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, done)
		b.get(x64.RSI, sOut)
		b.get(x64.RAX, sI)
		a.Add(x64.RSI, x64.RAX)
		a.MovImm(x64.RDX, '0')
		a.MovStore8(x64.RSI, 0, x64.RDX)
		b.get(x64.RAX, sI)
		a.AddImm(x64.RAX, 1)
		b.put(sI, x64.RAX)
		b.get(x64.RAX, sZ)
		a.AddImm(x64.RAX, -1)
		b.put(sZ, x64.RAX)
		a.Jmp(top)
		a.Mark(done)
	}
	from := func(v int64) {
		a.MovImm(x64.RAX, v)
		b.put(sJ, x64.RAX)
	}
	b.get(x64.RAX, sZ)
	a.Test(x64.RAX, x64.RAX)
	noSign := b.label()
	a.Jcc(x64.E, noSign)
	out('-')
	a.Mark(noSign)
	front := b.label()
	mid := b.label()
	tail := b.label()
	end := b.label()
	b.get(x64.RAX, sHead)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.L, front)
	b.get(x64.RAX, sHead)
	b.get(x64.RDX, sND)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.L, mid)
	a.Mark(tail)
	from(0)
	b.get(x64.RAX, sND)
	b.put(sEnd, x64.RAX)
	digits()
	b.get(x64.RAX, sHead)
	b.get(x64.RDX, sND)
	a.Sub(x64.RAX, x64.RDX)
	b.put(sZ, x64.RAX)
	zeros()
	out('.')
	out('0')
	a.Jmp(end)
	a.Mark(front)
	out('0')
	out('.')
	b.get(x64.RAX, sHead)
	a.Neg(x64.RAX)
	b.put(sZ, x64.RAX)
	zeros()
	from(0)
	b.get(x64.RAX, sND)
	b.put(sEnd, x64.RAX)
	digits()
	a.Jmp(end)
	a.Mark(mid)
	from(0)
	b.get(x64.RAX, sHead)
	b.put(sEnd, x64.RAX)
	digits()
	out('.')
	b.get(x64.RAX, sHead)
	b.put(sJ, x64.RAX)
	b.get(x64.RAX, sND)
	b.put(sEnd, x64.RAX)
	digits()
	a.Mark(end)
	b.get(x64.RAX, sI)
	b.epilogue()
}

// @fmod(rcx=被除数的 64 位, rdx=除数的 64 位)：返回 rax=余数的 64 位，符号跟被除数。
//
// 不能走 x - trunc(x/y)*y 那条最直接的路：商一旦超过 2^53，trunc(x/y)*y 本身就放不进一个
// double，余数会整体失真（818245.655 % 166.97 直接错成除数）。这里按 C 的 fmod 逐级处理：
// 把除数倍增到不超过被除数，再逐级折半回来做减法——
// 每一级都保持「余数 < 2×当前这级」，于是「余数 ≥ 这级就减掉它」那一步两个数相差不到一倍，
// 减法结果一定是精确的（Sterbenz），全程没有一次舍入。
//
// 正 double 的位型按无符号排序正好与数值一致，所以判大小不必真的动小数；
// 除数是 0／±Inf 与被除数是 NaN／±Inf 这几种先按位型判出，返回 NaN 或被除数本身，不抛。
//
// en: @fmod (rcx = dividend's 64 bits, rdx = divisor's 64 bits): returns rax = the remainder's
// 64 bits, sign following the dividend. The most direct x - trunc(x/y)*y cannot be used: once
// the quotient exceeds 2^53, trunc(x/y)*y itself does not fit in a double and the remainder
// drifts entirely (818245.655 % 166.97 comes out as the divisor). This follows C's fmod by
// doubling the divisor up to just under the dividend, then halving back and subtracting - each
// step keeps "remainder < 2 x current step", so when the remainder is at least that step the
// two numbers differ by less than a factor of two and the subtraction is exact (Sterbenz).
// No rounding happens anywhere. Positive doubles ordered as unsigned bit patterns match their
// numeric order, so comparisons never have to touch the fraction; a divisor of 0 or +/-Inf and
// a dividend of NaN or +/-Inf are picked out by pattern first, returning NaN or the dividend
// itself, without throwing.
func fMod(b *rt) {
	const (
		sAx  = 0 // |被除数| 的位型
		sAy  = 1 // |除数| 的位型
		sD   = 2 // 当前这级：2^k·|除数|
		sR   = 3 // 余数，始终是正的
		sCnt = 4 // 还剩几级要折回来
	)
	const (
		absMask = int64(0x7fffffffffffffff)
		infMask = int64(0x7ff0000000000000)
		nanBits = int64(0x7ff8000000000000)
		two     = int64(0x4000000000000000) // 2.0：倍增用
		half    = int64(0x3fe0000000000000) // 0.5：折回用
	)
	a := b.a
	b.prologue()
	toNaN := b.label()
	backToX := b.label()
	up := b.label()
	arm := b.label()
	down := b.label()
	skip := b.label()
	signed := b.label()
	paste := b.label()

	a.MovImm(x64.R10, absMask)
	a.Mov(x64.RAX, x64.RCX)
	a.And(x64.RAX, x64.R10)
	b.put(sAx, x64.RAX)
	a.Mov(x64.R11, x64.RDX)
	a.And(x64.R11, x64.R10)
	b.put(sAy, x64.R11)

	// 先把不需要逐级处理的几种挑出来，全按位型判断
	a.MovImm(x64.R10, infMask)
	a.Cmp(x64.RAX, x64.R10)
	a.Jcc(x64.AE, toNaN) // 被除数是 NaN 或 ±Inf
	a.Cmp(x64.R11, x64.R10)
	a.Jcc(x64.A, toNaN)   // 除数是 NaN
	a.Jcc(x64.E, backToX) // 除数是 ±Inf：一级都减不动
	a.Test(x64.R11, x64.R11)
	a.Jcc(x64.E, toNaN) // 除数是 0：按 IEEE 给 NaN，不抛
	a.Cmp(x64.RAX, x64.R11)
	a.Jcc(x64.B, backToX) // 被除数的绝对值小于除数，含 ±0

	b.get(x64.RAX, sAx)
	b.put(sR, x64.RAX)
	b.get(x64.RAX, sAy)
	b.put(sD, x64.RAX)
	a.MovImm(x64.RAX, 0)
	b.put(sCnt, x64.RAX)

	// 倍增：翻倍之后仍不超过被除数就继续翻倍
	a.Mark(up)
	b.get(x64.RAX, sD)
	a.MovsdFromGPR(0, x64.RAX)
	a.MovImm(x64.R11, two) // 常数每轮重放一次：跨 call 的 xmm 一律不可信
	a.MovsdFromGPR(1, x64.R11)
	a.Mulsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.get(x64.RDX, sAx)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, arm)
	b.put(sD, x64.RAX)
	b.get(x64.R10, sCnt)
	a.AddImm(x64.R10, 1)
	b.put(sCnt, x64.R10)
	a.Jmp(up)

	// 级数＝翻了几倍＋1（|除数| 自己那一级也要减一次）
	a.Mark(arm)
	b.get(x64.R10, sCnt)
	a.AddImm(x64.R10, 1)
	b.put(sCnt, x64.R10)

	// 折回：够这级就减掉，再减半；减与不减都保持「余数 < 2×当前级」
	a.Mark(down)
	b.get(x64.RAX, sR)
	b.get(x64.RDX, sD)
	// 两边都是正的、有限的，正 double 的位型按无符号比就是数值比，不必动小数
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.B, skip)
	a.MovsdFromGPR(0, x64.RAX)
	a.MovsdFromGPR(1, x64.RDX)
	a.Subsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.put(sR, x64.RAX)
	a.Mark(skip)
	b.get(x64.R10, sCnt)
	a.AddImm(x64.R10, -1)
	b.put(sCnt, x64.R10)
	a.Jcc(x64.E, signed)
	b.get(x64.RAX, sD)
	a.MovsdFromGPR(0, x64.RAX)
	a.MovImm(x64.R11, half)
	a.MovsdFromGPR(1, x64.R11)
	a.Mulsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.put(sD, x64.RAX)
	a.Jmp(down)

	a.Mark(signed)
	b.get(x64.RAX, sR)
	a.Jmp(paste)

	a.Mark(backToX)
	b.get(x64.RAX, sAx)

	// 到这里 RAX 都是正的，符号按被除数那一侧贴（余数的符号跟被除数）
	a.Mark(paste)
	a.Mov(x64.RDX, x64.RCX)
	a.MovImm(x64.R10, -absMask-1)
	a.And(x64.RDX, x64.R10)
	a.Or(x64.RAX, x64.RDX)
	b.epilogue()

	a.Mark(toNaN)
	a.MovImm(x64.RAX, nanBits)
	b.epilogue()
}

// @f_order(rcx=甲的 64 位, rdx=乙的 64 位)：返回 rax=比较结果的 bitmask。
//
//	1＝甲小，2＝相等，4＝甲大，0＝无序（有一边是 NaN）
//
// 全程只用整数指令：这台机器的 ucomisd 一执行就当非法指令（编译器换过两种操作码编码都试不出来，
// 索性不依赖它），而小数比较需要的语义用位型就能完整做出来——
// 两边的指数尾数都是非负整数，正数按位型无符号比就是数值比，负数反着来，
// ±0 的位型只差一个符号位、必须单独判成相等。
//
// 返回 bitmask 而不是 −1／0／1，是为了让发射器一条 test 就能得出条件：
// "<" 查 1、"==" 查 2、">=" 查 2|4，无序那种各位自然全不命中（NaN 与任何数比较都是假）。
//
// en: @f_order (rcx = a's 64 bits, rdx = b's 64 bits): returns rax = the comparison bitmask: 1
// = a less, 2 = equal, 4 = a greater, 0 = unordered (one side is NaN). Only integer
// instructions are used, because this machine treats ucomisd as an illegal instruction as soon
// as it executes (two operand encodings were tried by the compiler with no luck, so it is
// simply not depended on), and the float comparison semantics come out complete from bit
// patterns: exponent and mantissa are both non-negative integers, a positive compares
// numerically as unsigned, a negative reverses, and the two patterns of +/-0 differ only in the
// sign bit and must be compared equal explicitly. A bitmask rather than -1/0/1 is returned so
// the emitter produces a condition with one test: "<" tests 1, "==" tests 2, ">=" tests 2|4,
// and the unordered case fails all bits naturally - NaN compares false against everything.
func fOrder(b *rt) {
	const (
		lBit = 1
		eBit = 2
		gBit = 4
	)
	const (
		absBits  = int64(0x7fffffffffffffff)
		infBits  = int64(0x7ff0000000000000)
		mantBits = int64(0x000fffffffffffff)
	)
	a := b.a
	b.prologue()
	unord := b.label()
	eq := b.label()
	notZero := b.label()
	same := b.label()
	magLt := b.label()
	setL := b.label()
	setG := b.label()
	ret := b.label()

	a.MovImm(x64.R10, absBits)
	a.Mov(x64.R8, x64.RCX)
	a.And(x64.R8, x64.R10) // |甲| 的位型
	a.Mov(x64.R9, x64.RDX)
	a.And(x64.R9, x64.R10) // |乙| 的位型

	// 指数占满且尾数还有内容＝NaN，任何一边是 NaN 都判无序
	a.MovImm(x64.R10, infBits)
	a.MovImm(x64.R11, mantBits)
	a.Cmp(x64.R8, x64.R10)
	skipA := b.label()
	a.Jcc(x64.B, skipA)
	a.Mov(x64.RAX, x64.R8)
	a.And(x64.RAX, x64.R11)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, unord)
	a.Mark(skipA)
	a.Cmp(x64.R9, x64.R10)
	skipB := b.label()
	a.Jcc(x64.B, skipB)
	a.Mov(x64.RAX, x64.R9)
	a.And(x64.RAX, x64.R11)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, unord)
	a.Mark(skipB)

	// ±0 只差一个符号位，先单独判成相等
	a.Test(x64.R8, x64.R8)
	a.Jcc(x64.NE, notZero)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, eq)
	a.Mark(notZero)

	// 符号位：甲的存 rax，乙的存 r10
	a.Mov(x64.RAX, x64.RCX)
	a.ShrImm(x64.RAX, 63)
	a.Mov(x64.R10, x64.RDX)
	a.ShrImm(x64.R10, 63)
	a.Cmp(x64.RAX, x64.R10)
	a.Jcc(x64.E, same)
	// 异号：负的那一方一定小（到了这一步两边都不会是 0）
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, setL)
	a.Jmp(setG)

	// 同号：比绝对值；都是负数时方向反过来
	a.Mark(same)
	a.Cmp(x64.R8, x64.R9)
	a.Jcc(x64.B, magLt)
	a.Jcc(x64.E, eq)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, setL)
	a.Jmp(setG)
	a.Mark(magLt)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, setG)
	a.Jmp(setL)

	a.Mark(setL)
	a.MovImm(x64.RAX, lBit)
	a.Jmp(ret)
	a.Mark(eq)
	a.MovImm(x64.RAX, eBit)
	a.Jmp(ret)
	a.Mark(setG)
	a.MovImm(x64.RAX, gBit)
	a.Jmp(ret)
	a.Mark(unord)
	a.MovImm(x64.RAX, 0)
	a.Mark(ret)
	b.epilogue()
}

// leBytes 把一小串字节按 little endian 拼成一个 64 位立即数，一次写进输出缓冲的开头。
//
// en: leBytes packs a few bytes little-endian into one 64-bit immediate and writes it to the
// head of the output buffer in one store.
func leBytes(s string) int64 {
	var buf [8]byte
	copy(buf[:], s)
	return int64(binary.LittleEndian.Uint64(buf[:]))
}

// fixedOut 是特殊值的几条固定路径：串直接写入输出缓冲，长度写死在指令里。
//
// asString 那一支不写 stdout，而是把这一小串包成一个文本对象留在 rax。
//
// en: fixedOut is the special-value shortcuts: the string goes straight into the output buffer
// and its length is written into the instruction. The asString branch does not write to stdout
// but wraps the few characters into a text object left in rax.
func fixedOut(b *rt, s string, asString bool) {
	b.a.MovImm(x64.RAX, leBytes(s))
	b.storeRIP(x64.RAX, bufOut)
	b.leaRIP(x64.RCX, bufOut)
	b.a.MovImm(x64.RDX, int64(len(s)))
	if asString {
		b.a.Mov(x64.R8, x64.RDX) // NaN／Inf／0.0 这几串全是 ASCII
		b.call("@str_wrap")
		return
	}
	b.call("@write")
}

// putFloat 把双精度变成"最短往返、一定带小数点"的那一串字符。
//
// asString 决定最终的去向：false 就是 @put_float，写 stdout；true 就是 @float_str，
// 把 bufOut 里那一串包成文本对象返回。算术那一段两条路共用，所以打印出来与转成文本的
// 一定是同一个写法——"照抄回去还是同一个数"的要求只在这里落实一次。
//
// 值本身按 m×2^e 展开，区间两端是"与左邻居的中点"和"与右邻居的中点"。
// 端点的归属按平手取偶判定：尾数是奇数时两个端点都归邻居，是偶数时都归自己。
// 最大的那个 double 也照样有上端——它上面是 +Inf，而舍入的界线就在 v＋半个 ulp，
// 那正是 (2m+1)×2^(e-1)，所以上端不必另开一支。
//
// en: putFloat turns a double into the shortest roundtripping decimal that always carries a
// point. asString decides where it ends: false is @put_float writing to stdout, true is
// @float_str wrapping bufOut's characters into a returned text object. The arithmetic half is
// shared, so what prints and what converts to text are guaranteed the same spelling - the
// "typed back it is the same number" requirement is enforced once, here. The value is expanded
// as m x 2^e and the interval's ends are the midpoints to the left and right neighbours. The
// endpoint ownership goes to even: with an odd mantissa both ends belong to the neighbours,
// with an even one both belong to v. Even the largest double has an upper end - above it is
// +Inf and the rounding boundary is exactly v + half an ulp, which is (2m+1) x 2^(e-1), so no
// separate branch is needed.
func putFloat(b *rt, asString bool) {
	const (
		sBits = 0
		sExp  = 1
		sMant = 2
		sSign = 3
		sM    = 4
		sE    = 5
		sOdd  = 6
		sLoK  = 7
		sLoJ  = 8
		sHiK  = 9
		sHiJ  = 10
		sP    = 11
	)
	a := b.a
	b.prologue()
	b.put(sBits, x64.RCX)
	setCtl := func(ctl, buf string) {
		b.leaRIP(x64.RAX, ctl)
		b.leaRIP(x64.RDX, buf)
		a.MovStore(x64.RAX, cPtr, x64.RDX)
	}
	setCtl(ctlV, bufV)
	setCtl(ctlLo, bufLo)
	setCtl(ctlHi, bufHi)
	setCtl(ctlC, bufC)
	epi := b.label()
	b.get(x64.RAX, sBits)
	a.MovImm(x64.RCX, 63)
	a.Shr(x64.RAX)
	b.put(sSign, x64.RAX)
	b.get(x64.RAX, sBits)
	a.MovImm(x64.RCX, 52)
	a.Shr(x64.RAX)
	a.AndImm(x64.RAX, 0x7FF)
	b.put(sExp, x64.RAX)
	b.get(x64.RAX, sBits)
	a.MovImm(x64.R8, int64(uint64(1)<<52-1))
	a.And(x64.RAX, x64.R8)
	b.put(sMant, x64.RAX)
	// 特殊值：不是数、无穷、零，各是一条固定的串
	norm := b.label()
	isNaN := b.label()
	isInf := b.label()
	posInf := b.label()
	b.get(x64.RAX, sExp)
	a.CmpImm(x64.RAX, 0x7FF)
	a.Jcc(x64.NE, norm)
	b.get(x64.RAX, sMant)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, isNaN)
	a.Jmp(isInf)
	a.Mark(isNaN)
	fixedOut(b, "NaN", asString)
	a.Jmp(epi)
	a.Mark(isInf)
	b.get(x64.RAX, sSign)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, posInf)
	fixedOut(b, "-Inf", asString)
	a.Jmp(epi)
	a.Mark(posInf)
	fixedOut(b, "Inf", asString)
	a.Jmp(epi)
	a.Mark(norm)
	zero := b.label()
	isZero := b.label()
	b.get(x64.RAX, sExp)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, zero)
	b.get(x64.RAX, sMant)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, zero)
	a.Jmp(isZero)
	a.Mark(zero)
	// 正规数补上隐含的最高位；次正规没有它，指数固定为 -1074
	sub := b.label()
	md := b.label()
	b.get(x64.RAX, sExp)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, sub)
	b.get(x64.RAX, sMant)
	a.MovImm(x64.R8, int64(uint64(1)<<52))
	a.Or(x64.RAX, x64.R8)
	b.put(sM, x64.RAX)
	b.get(x64.RAX, sExp)
	a.AddImm(x64.RAX, -1075)
	b.put(sE, x64.RAX)
	a.Jmp(md)
	a.Mark(sub)
	b.get(x64.RAX, sMant)
	b.put(sM, x64.RAX)
	a.MovImm(x64.RAX, -1074)
	b.put(sE, x64.RAX)
	a.Mark(md)
	b.get(x64.RAX, sM)
	a.AndImm(x64.RAX, 1)
	b.put(sOdd, x64.RAX)
	// 两个端点：v 与左右邻居各自的中点，写成"系数×2 的幂"
	b.get(x64.RAX, sM)
	a.MovImm(x64.R8, 2)
	a.Imul(x64.RAX, x64.R8)
	a.AddImm(x64.RAX, -1)
	b.put(sLoK, x64.RAX)
	b.get(x64.RAX, sE)
	a.AddImm(x64.RAX, -1)
	b.put(sLoJ, x64.RAX)
	// 正好是 2 的整数次幂：下邻居只隔半个 ulp，最小那个正规数不算（它下面就是次正规）
	loDone := b.label()
	b.get(x64.RAX, sM)
	a.MovImm(x64.R8, int64(uint64(1)<<52))
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.NE, loDone)
	b.get(x64.RAX, sE)
	a.CmpImm(x64.RAX, -1074)
	a.Jcc(x64.LE, loDone)
	b.get(x64.RAX, sM)
	a.MovImm(x64.R8, 4)
	a.Imul(x64.RAX, x64.R8)
	a.AddImm(x64.RAX, -1)
	b.put(sLoK, x64.RAX)
	b.get(x64.RAX, sE)
	a.AddImm(x64.RAX, -2)
	b.put(sLoJ, x64.RAX)
	a.Mark(loDone)
	b.get(x64.RAX, sM)
	a.MovImm(x64.R8, 2)
	a.Imul(x64.RAX, x64.R8)
	a.AddImm(x64.RAX, 1)
	b.put(sHiK, x64.RAX)
	b.get(x64.RAX, sE)
	a.AddImm(x64.RAX, -1)
	b.put(sHiJ, x64.RAX)
	b.leaRIP(x64.RCX, ctlV)
	b.get(x64.RDX, sM)
	b.get(x64.R8, sE)
	b.call("@f_expand")
	b.leaRIP(x64.RCX, ctlLo)
	b.get(x64.RDX, sLoK)
	b.get(x64.R8, sLoJ)
	b.call("@f_expand")
	b.leaRIP(x64.RCX, ctlHi)
	b.get(x64.RDX, sHiK)
	b.get(x64.R8, sHiJ)
	b.call("@f_expand")
	a.MovImm(x64.RAX, 1)
	b.put(sP, x64.RAX)
	// 一位一位往上试：第一个抄回来仍是同一个数的就是答案
	pTop := b.label()
	found := b.label()
	nextP := b.label()
	pDone := b.label()
	upOK := b.label()
	a.Jmp(pTop)
	// 零没有区间可套，短到只有一个写法
	a.Mark(isZero)
	b.get(x64.RAX, sSign)
	a.Test(x64.RAX, x64.RAX)
	posZero := b.label()
	a.Jcc(x64.E, posZero)
	fixedOut(b, "-0.0", asString)
	a.Jmp(epi)
	a.Mark(posZero)
	fixedOut(b, "0.0", asString)
	a.Jmp(epi)
	a.Mark(pTop)
	b.get(x64.RAX, sP)
	a.CmpImm(x64.RAX, 17)
	a.Jcc(x64.G, pDone)
	b.leaRIP(x64.RCX, ctlV)
	b.leaRIP(x64.RDX, ctlC)
	b.get(x64.R8, sP)
	b.call("@f_round")
	b.leaRIP(x64.RCX, ctlC)
	b.leaRIP(x64.RDX, ctlLo)
	b.call("@f_cmp")
	a.CmpImm(x64.RAX, 0)
	a.Jcc(x64.L, nextP)
	a.Jcc(x64.G, upOK)
	b.get(x64.RAX, sOdd) // 恰好等于下端点：尾数是奇数时这一票归左邻居
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, nextP)
	a.Mark(upOK)
	b.leaRIP(x64.RCX, ctlC)
	b.leaRIP(x64.RDX, ctlHi)
	b.call("@f_cmp")
	a.CmpImm(x64.RAX, 0)
	a.Jcc(x64.G, nextP)
	a.Jcc(x64.NE, found)
	b.get(x64.RAX, sOdd) // 恰好等于上端点：同理
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, nextP)
	a.Jmp(found)
	a.Mark(nextP)
	b.get(x64.RAX, sP)
	a.AddImm(x64.RAX, 1)
	b.put(sP, x64.RAX)
	a.Jmp(pTop)
	// 17 位一定够，走到这里是给算术留的保险：把最后一轮的候选原样交出
	a.Mark(pDone)
	a.Mark(found)
	b.leaRIP(x64.RCX, ctlC)
	b.get(x64.RDX, sSign)
	b.call("@f_layout")
	b.leaRIP(x64.RCX, bufOut)
	a.Mov(x64.RDX, x64.RAX)
	if asString {
		a.Mov(x64.R8, x64.RDX) // @f_layout 返回的全是 ASCII
		b.call("@str_wrap")
	} else {
		b.call("@write")
	}
	a.Mark(epi)
	b.epilogue()
}

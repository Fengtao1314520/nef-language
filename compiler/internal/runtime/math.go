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

// math.go 实现数学组的八个成员：absolute_value、floor、ceiling、round、minimum、maximum、power、random。
//
// 整数那几条走通用寄存器；小数这一支全在**位型**上定义运算——发射器把小数交给运行期时
// 本来就是"把那 64 位当整数搬过来"（xmm 与通用寄存器之间那条 movq），运行期再自己搬回去。
// 比较也不使用 `ucomisd`：这台机器一执行它就是非法指令，所以 minimum／maximum 用的是 @f_order
// 那个整数位型的方向掩码。
//
// floor／ceiling／round 三条共用同一个实现体，差别只在外面那一个布尔：
// 先 cvttsd2si 向零截断，再按符号决定要不要补一个单位——
// 负数向下要补（-3.7 截成 -3，还要往下）、正数向上要补，另一侧本来就是对的。
// 判断"本来是不是整数"用的是一条不碰浮点比较的做法：把截出来的整数再转回小数，
// 比两者的**位型大小**——|t| 严格小于 |x| 就说明截掉了一段小数。
// 这样 ±0.0、正好是整数的、以及 2^63 那种边界都不会多走一步。
//
// 值域检查比 CPU 更严：cvttsd2si 装不下时返回的是 0x8000000000000000 那个"无定数"，
// 与真正的 -2^63 一模一样，无法区分。所以先按位型判范围：负号那侧允许正好等于 2^63
// （它就是 int 的下界），正号那侧必须严格小于，NaN 与 ±Inf 的位型都大过上限，一并判出局抛 10013 号。
//
// en: math.go implements the eight members of the math group: absolute_value, floor, ceiling,
// round, minimum, maximum, power, random. The integer members use general-purpose registers;
// the float branch speaks entirely in BIT PATTERNS - the emitter hands a float to the runtime
// by moving those 64 bits as an integer (the movq between xmm and a general register) and the
// runtime moves it back. Comparisons never use `ucomisd`: this machine faults it as an illegal
// instruction, so minimum/maximum use @f_order's integer bit-pattern direction mask.
// floor/ceiling/round share one body, differing only in the boolean outside it: cvttsd2si
// truncates toward zero first, then one unit is added or not by sign - a negative rounding
// down needs it (-3.7 truncates to -3 and must still go lower), a positive rounding up needs
// it, and the other side was already right. Whether the value was already an integer is
// decided without any float comparison: convert the truncated integer back to a float and
// compare the BIT PATTERNS - |t| strictly below |x| means a fractional part was cut. That
// keeps ±0.0, exact integers and the 2^63 boundary from moving one step too far.
// The range check is stricter than the CPU: when cvttsd2si cannot hold a value it returns the
// indefinite 0x8000000000000000, indistinguishable from a real -2^63. So the range is judged
// by pattern first: the negative side may equal 2^63 exactly (that is int's lower bound), the
// positive side must be strictly below, and the patterns of NaN and ±Inf both exceed the
// ceiling, so they leave and throw code 10013.
package runtime

import "neflang.org/nf/internal/x64"

// int64AsDoubleBits 是 2^63 那个小数的 64 位位型，值域判定的上限。
//
// en: int64AsDoubleBits is the 64-bit pattern of the double 2^63, the ceiling of the range
// test.
const int64AsDoubleBits = 0x43E0000000000000

// absOf 把 [reg] 那个槽位的符号位清掉，返回它的绝对值的位型（rax 是那个源值）。
//
// 左移一位再右移一位，比 AND 一个 63 个 1 的掩码更经济——imm32 装不下那么大的数。
//
// en: absOf clears the sign bit of the slot [reg] and returns the pattern of its absolute value
// (rax holds the source). Shifting left one bit and back right is cheaper than AND with a
// 63-ones mask, which imm32 cannot hold.
func absOf(a *x64.Asm, reg int) {
	a.ShlImm(reg, 1)
	a.ShrImm(reg, 1)
}

// @i_abs(rcx=整数)：返回 rax=它的绝对值。
//
// 最小的那个负数取反还是它自己，那条 neg 会置起溢出位——正好落进"整数运算溢出"这一类，
// 抛 10021 号，与 `0 - (-9223372036854775808)` 同一种处置。
//
// en: @i_abs (rcx = integer): returns rax = its absolute value. Negating the most negative
// value yields itself, and that neg sets the overflow bit - landing exactly on the "integer
// arithmetic overflowed" case, throwing 10021, the same outcome as
// `0 - (-9223372036854775808)`.
func iAbs(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.RAX, x64.RCX)
	a.Test(x64.RAX, x64.RAX)
	done := b.label()
	a.Jcc(x64.NS, done)
	a.Neg(x64.RAX)
	bad := b.label()
	a.Jcc(x64.O, bad)
	a.Mark(done)
	b.epilogue()
	a.Mark(bad)
	b.throwWith(codeOverflow, "sOverflow")
	b.epilogue()
}

// @f_abs(rcx=那 64 位)：返回 rax=清掉符号位的那 64 位。
//
// NaN 也走这条路径：清掉符号位还是 NaN。±0.0 都返回 0.0。
//
// en: @f_abs (rcx = those 64 bits): returns rax = those 64 bits with the sign bit cleared. NaN
// takes this path too: clearing the sign bit still leaves NaN. Both ±0.0 return 0.0.
func fAbs(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.RAX, x64.RCX)
	absOf(a, x64.RAX)
	b.epilogue()
}

// pick 是 minimum／maximum 的共用实现体：比完标志位取一个。
//
// en: pick is the shared body of minimum/maximum: compare the flags, then take one.
func iPick(b *rt, takeFirstIf x64.Cond) {
	a := b.a
	b.prologue()
	a.Mov(x64.RAX, x64.RCX)
	a.Cmp(x64.RAX, x64.RDX)
	keep := b.label()
	a.Jcc(takeFirstIf, keep)
	a.Mov(x64.RAX, x64.RDX)
	a.Mark(keep)
	b.epilogue()
}

// @i_min(rcx=甲, rdx=乙)／@i_max：返回 rax=小的那一个／大的那一个。
// 相等时取甲——两个相同的数，取哪个都是对的。
//
// en: @i_min (rcx = a, rdx = b) and @i_max: return rax = the smaller / the larger. When equal,
// a is taken - with two identical numbers either choice is correct.
func iMin(b *rt) { iPick(b, x64.LE) }
func iMax(b *rt) { iPick(b, x64.GE) }

// @f_min(rcx=甲那 64 位, rdx=乙那 64 位)／@f_max：返回 rax=取中那一个的位型。
//
// 方向由 @f_order 给出（整数位型那一套，掩码 1=小、2=等、4=大、0=无序），
// 不使用 ucomisd。NaN 参与时返回甲——"谁小"在这里没有定义，取第一个是本版本定下的规则。
//
// en: @f_min (rcx = a's 64 bits, rdx = b's 64 bits) and @f_max: return rax = the pattern of the
// one picked. Direction comes from @f_order (the integer bit-pattern scheme, mask 1 = less,
// 2 = equal, 4 = greater, 0 = unordered) and never ucomisd. With NaN involved, a is returned -
// "which is smaller" has no definition here, and taking the first is the rule this version set.
func fPick(b *rt, wantLess bool) {
	const (
		pA = 0
		pB = 1
	)
	a := b.a
	b.prologue()
	b.put(pA, x64.RCX)
	b.put(pB, x64.RDX)
	b.call("@f_order") // rcx、rdx 仍是入参那两个
	// minimum 要的是"不是大"，maximum 要的是"不是小"：各自清掉对应那一位后看剩余
	bit := int32(4)
	if !wantLess {
		bit = 1
	}
	a.AndImm(x64.RAX, bit)
	pick := b.label()
	a.Jcc(x64.E, pick) // 那一位是 0 → 取的就是甲
	b.get(x64.RAX, pB)
	b.epilogue()
	a.Mark(pick)
	b.get(x64.RAX, pA)
	b.epilogue()
}

func fMin(b *rt) { fPick(b, true) }
func fMax(b *rt) { fPick(b, false) }

// @i_pow(rcx=底, rdx=指数)：返回 rax=底的指数次方（只有整数这一种）。
//
// 指数为负抛 10012 号——10012 对应的那句"文本读不成数字"用在这儿不成立，
// 所以这一条路自己带一句"指数不能是负的"。
// 反复平方，每步 imul 之后检查溢出位：置起就抛 10021 号。循环只跑到指数剩 1 为止，
// 最后那一次平方不做（比如 power(2, 62) 中途会把底平方到 2^32，再平方就伪溢出了）。
// power(0, 0) 返回 1，与大多数语言一致。
//
// en: @i_pow (rcx = base, rdx = exponent): returns rax = base to the exponent (integer only).
// A negative exponent throws 10012 - the "text is not a number" message behind that code does
// not fit here, so this path carries its own "the exponent may not be negative". Repeated
// squaring, with the overflow bit checked after every imul and 10021 thrown when set. The loop
// stops when one exponent remains, skipping the final squaring (otherwise power(2, 62) would
// have squared the base to 2^32 and then overflow spuriously). power(0, 0) returns 1, matching
// most languages.
func iPow(b *rt) {
	const (
		pBase = 0
		pExp  = 1
		pRes  = 2
	)
	a := b.a
	b.prologue()
	b.put(pBase, x64.RCX)
	b.put(pExp, x64.RDX)

	a.Test(x64.RDX, x64.RDX)
	negExp := b.label()
	a.Jcc(x64.S, negExp)
	a.MovImm(x64.RAX, 1)
	b.put(pRes, x64.RAX)

	top := b.label()
	mulRes := b.label()
	square := b.label()
	tail := b.label()
	done := b.label()
	boom := b.label()
	a.Jmp(top)

	// res = res × 底；溢出位置起就抛
	a.Mark(mulRes)
	b.get(x64.RAX, pRes)
	b.get(x64.RDX, pBase)
	a.Imul(x64.RAX, x64.RDX)
	a.Jcc(x64.O, boom)
	b.put(pRes, x64.RAX)

	a.Mark(square)
	b.get(x64.RAX, pBase)
	b.get(x64.RDX, pBase)
	a.Imul(x64.RAX, x64.RDX)
	a.Jcc(x64.O, boom)
	b.put(pBase, x64.RAX)
	b.get(x64.RAX, pExp)
	a.ShrImm(x64.RAX, 1) // 指数已经判过非负，逻辑右移就是它的一半
	b.put(pExp, x64.RAX)

	// 循环：指数还剩多少
	a.Mark(top)
	b.get(x64.RAX, pExp)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.LE, tail)
	b.get(x64.RAX, pExp)
	a.AndImm(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, mulRes)
	a.Jmp(square)

	// 收尾：最后那一次乘要做，平方不要。这一段是照着 mulRes 复制的而不是 jmp 过去——
	// 从循环里 jmp 过来会顺着 fall through 再平方一次，power(2, 62) 那种就把底平方成了 2^64，
	// 明明装得下的结果反倒抛了 10021 号。
	a.Mark(tail)
	b.get(x64.RAX, pExp)
	a.CmpImm(x64.RAX, 0)
	a.Jcc(x64.E, done)
	b.get(x64.RAX, pRes)
	b.get(x64.RDX, pBase)
	a.Imul(x64.RAX, x64.RDX)
	a.Jcc(x64.O, boom)
	b.put(pRes, x64.RAX)

	a.Mark(done)
	b.get(x64.RAX, pRes)
	b.epilogue()
	a.Mark(boom)
	b.throwWith(codeOverflow, "sOverflow")
	b.epilogue()
	a.Mark(negExp)
	b.throwWith(codeNotNum, "sNegExp")
	b.epilogue()
}

// roundBody 是 @f_floor／@f_ceil／@f_round 共用的实现体。
//
// isFloor 决定补哪个方向：向下取整要往负方向补、向上取整要往正方向补。
// plusHalf 是 round 那一条：语言规则定的是".5 一律往数轴正方向进"，等价于 floor(x + 0.5)，
// 而 SSE 的就近取整是"平手取偶"（2.5 会得 2），所以不能用它。
//
// en: roundBody is the body shared by @f_floor, @f_ceil and @f_round. isFloor decides which
// unit to add: rounding down steps toward negative, rounding up toward positive. plusHalf is
// the round case: the language rule fixes ".5 always advances toward the positive end of the
// number line", which is equivalent to floor(x + 0.5), and SSE's round-to-nearest is
// ties-to-even (2.5 would give 2), so it cannot be used.
func roundBody(b *rt, isFloor, plusHalf bool) {
	const (
		qBits  = 0 // 进来的那 64 位
		qTrunc = 1 // 向零截断出来的那个整数
	)
	a := b.a
	b.prologue()
	b.put(qBits, x64.RCX)

	// 值域：先比位型的绝对值与 2^63
	out := b.label()
	edge := b.label()
	legal := b.label()
	b.get(x64.RAX, qBits)
	absOf(a, x64.RAX)
	a.MovImm(x64.R9, int64AsDoubleBits)
	a.Cmp(x64.RAX, x64.R9)
	a.Jcc(x64.A, out) // 比 2^63 还大：NaN、±Inf、以及真正超界的数
	a.Jcc(x64.NE, legal)
	a.Mark(edge) // 正好 2^63：负号那侧是 int 的下界，接受；正号那侧超出一个单位
	b.get(x64.RAX, qBits)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, legal)
	a.Mark(out)
	b.throwWith(codeRange, "sRange")
	b.epilogue()
	a.Mark(legal)

	// 进 xmm0，round 先加上 0.5
	b.get(x64.RAX, qBits)
	a.MovsdFromGPR(0, x64.RAX)
	if plusHalf {
		b.loadRIP(x64.RAX, "fHalf")
		a.MovsdFromGPR(1, x64.RAX)
		a.Addsd(0, 1)
		a.MovsdToGPR(x64.RAX, 0)
		b.put(qBits, x64.RAX) // 加过 0.5 的那 64 位替换原来的，下面的判断一律用它
		a.MovsdFromGPR(0, x64.RAX)
	}
	a.CvtTSD2SI(x64.RDX, 0) // 向零截断
	b.put(qTrunc, x64.RDX)

	// 把截出来的整数**转成小数**再比一次位型：|t| < |x| 就是截掉了一段小数。
	// 这里必须是 cvtsi2sd 而不是 movq——movq 搬的是那 64 位本身，-2 那个整数搬过去
	// 就成了 0xFFFFFFFFFFFFFFFE，按位型比的话比任何真数都大，需要调整的那一支永远不会走到。
	a.CvtSI2SD(1, x64.RDX)
	a.MovsdToGPR(x64.RSI, 1)
	absOf(a, x64.RSI) // rsi = |t| 的位型
	b.get(x64.R10, qBits)
	absOf(a, x64.R10) // r10 = |x| 的位型
	a.Cmp(x64.RSI, x64.R10)
	done := b.label()
	a.Jcc(x64.AE, done) // 没有截掉小数：截断值就是要的整数
	// 确实截掉了一段：负数一侧 floor 要往下，正数一侧 ceiling 要往上
	b.get(x64.RAX, qBits)
	a.Test(x64.RAX, x64.RAX)
	negative := b.label()
	a.Jcc(x64.NS, negative)
	if isFloor {
		a.AddImm(x64.RDX, -1)
	}
	a.Jmp(done)
	a.Mark(negative)
	if !isFloor {
		a.AddImm(x64.RDX, 1)
	}
	a.Mark(done)
	// 上面两个槽位的加减都落在 rdx 上；没走到那两条路时 rdx 就是截断值本身
	a.Mov(x64.RAX, x64.RDX)
	b.epilogue()
}

// @f_floor(rcx=那 64 位)：返回 rax=向下取整的那个整数。
//
// en: @f_floor (rcx = those 64 bits): returns rax = the integer rounded down.
func intFloor(b *rt) { roundBody(b, true, false) }

// @f_ceil(rcx=那 64 位)：返回 rax=向上取整的那个整数。
//
// en: @f_ceil (rcx = those 64 bits): returns rax = the integer rounded up.
func intCeil(b *rt) { roundBody(b, false, false) }

// @f_round(rcx=那 64 位)：返回 rax=就近取整的那个整数，.5 往数轴正方向进。
//
// 名字与 @f_round 那个"收成 p 位小数"的实现（float.go 里那条）不是同一个功能：那一条改的是
// 小数的位数，这一条返回的是整数。两者并存，各用各的注册名。
//
// en: @f_round (rcx = those 64 bits): returns rax = the integer rounded to nearest with .5
// advancing toward positive. It is not the same routine as @f_round in float.go, which rounds
// to p decimal digits: that one changes how many decimal places a float has, this one returns
// an integer. Both exist, each under its own registered name.
func intRound(b *rt) { roundBody(b, true, true) }

// @i_random(rcx=起, rdx=止)：返回 rax=「起 ≤ 它 < 止」里的一个整数（区间不含止，与 `until` 同一条规则）。
//
// 种子只取一次，存在静态槽位里：首次调用时向高精度计数器取当前 tick。那条计数器是
// `@time_elapsed` 也在用的 kernel32 名字，但这里**不需要频率**——只要它每次启动程序取值不同，
// 两次调用之间又一直在走。若取回来的值恰好是 0，就替换成一个无理数常数：xorshift 的状态
// 不能是 0（0 推进一百次还是 0，那就成了"永远返回同一个数"的伪随机）。
//
// 状态推进是 xorshift64*：三次"移位带异或"更新状态，再乘一个大奇数作为输出——这个生成器
// 低位质量最差，乘一下就打散。要更好的分布可以替换这一步，外部看不见。
//
// 空范围（起 ≥ 止，含起 == 止：取不到止）当场抛 **10020 号**，那是数值组的兜底码——
// 错误码分配里 21 是溢出、22 是除以 0，没有"范围为空"的编号。
//
// 宽度按**无符号回绕**算：只要 止 > 起，两者之差的真值必然落在 1～2^64−1 之间，回绕出来的
// 那个无符号数就是它，所以 `math.random(最小的 int, 最大的 int)` 这种满量程不必另开一条路。
// 取模走无符号 div，最后 `起 + 余数` 在补码下正好落在 [起, 止) 里。
//
// en: @i_random (rcx = from, rdx = to): returns rax = an integer with from <= it < to (to is
// excluded, the same rule as `until`). The seed is taken once and kept in a static slot: on
// the first call it asks the high-resolution counter for the current tick. That counter is a
// kernel32 name @time_elapsed also uses, but NO FREQUENCY is needed here - it only has to
// differ per program start and keep moving between calls. If the tick happens to come back 0,
// an irrational constant substitutes: a xorshift state of 0 stays 0 for a hundred steps, which
// would be fake randomness that "always returns the same number". Advancing is xorshift64*:
// three shift-xor steps update the state, then multiply by a large odd number for the output -
// that generator's low bits are its worst, and the multiply spreads them. A better distribution
// can replace this step later with nothing visible outside. An empty range (from >= to,
// including from == to since to is excluded) throws code 10020 on the spot, the numeric
// group's fallback: the code table assigns 21 to overflow and 22 to division by zero, leaving
// no number for "the range is empty". The width is computed with unsigned wrap-around: as long
// as to > from, the true difference lies between 1 and 2^64-1 and the wrapped unsigned number
// IS it, so the full range `math.random(smallest int, largest int)` needs no special path. The
// modulus uses unsigned div, and `from + remainder` in two's complement lands exactly inside
// [from, to).
func iRandom(b *rt) {
	const (
		rLo   = 0 // 起
		rHi   = 1 // 止
		rTick = 2 // 计数器返回值的槽位
	)
	a := b.a
	b.prologue()
	b.put(rLo, x64.RCX)
	b.put(rHi, x64.RDX)

	empty := b.label()
	stepped := b.label()
	fresh := b.label()

	b.get(x64.RAX, rLo)
	b.get(x64.RCX, rHi)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.GE, empty)

	b.loadRIP(x64.RAX, "rndSeed")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, stepped)
	a.LeaMem(x64.RCX, x64.RBP, int32(-8*(rTick+1)))
	b.impCall("QueryPerformanceCounter")
	b.get(x64.RAX, rTick)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, fresh)
	a.MovImm(x64.RAX, -0x61c8864680b583ab) // 0x9E3779B97F4A7C15：黄金比例的 64 位
	a.Mark(fresh)
	b.storeRIP(x64.RAX, "rndSeed")

	a.Mark(stepped)
	b.loadRIP(x64.RAX, "rndSeed")
	a.Mov(x64.RCX, x64.RAX)
	a.ShlImm(x64.RCX, 13)
	a.Xor(x64.RAX, x64.RCX)
	a.Mov(x64.RCX, x64.RAX)
	a.ShrImm(x64.RCX, 7)
	a.Xor(x64.RAX, x64.RCX)
	a.Mov(x64.RCX, x64.RAX)
	a.ShlImm(x64.RCX, 17)
	a.Xor(x64.RAX, x64.RCX)
	b.storeRIP(x64.RAX, "rndSeed")
	a.MovImm(x64.RCX, 0x2545F4914F6CDD1D)
	a.Imul(x64.RAX, x64.RCX)

	b.get(x64.RCX, rHi)
	b.get(x64.RDX, rLo)
	a.Sub(x64.RCX, x64.RDX) // 无符号回绕：止 > 起 时这就是准确的宽度
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.RCX) // rdx = 余数
	b.get(x64.RAX, rLo)
	a.Add(x64.RAX, x64.RDX)
	b.epilogue()

	a.Mark(empty)
	b.throwWith(codeEmptyRange, "sEmptyRange")
}

// @f_sqrt(rcx=那 64 位)：返回 rax=它的平方根。
//
// 负数不抛，返回 NaN：IEEE 就是这么规定的，而 NaN 本来就是这台机器支持的三个特殊值之一。
// 正无穷返回正无穷，0 与 -0.0 各返回自己。
//
// en: @f_sqrt (rcx = those 64 bits): returns rax = its square root. A negative does not throw;
// it returns NaN - that is what IEEE specifies, and NaN is already one of this machine's three
// special values. Positive infinity returns positive infinity, and 0 and -0.0 each return
// themselves.
func fSqrt(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.Sqrtsd(0, 0)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_pi()：返回 rax=圆周率的那 64 位。
//
// 做成一个零参数函数，是因为组名下面挂的成员只有函数这一种写法，
// 与 `time.now()`、`environment.current_directory()` 同形；CPU 自己那个 π 是 80 位的
// （fldpi），拿它再落成双精度就不是 3.141592653589793 了。
//
// en: @f_pi(): returns rax = the 64 bits of pi. It is a zero-argument function because a
// member hanging on a group name can only be a function, the same shape as time.now() and
// environment.current_directory(); the machine's own pi is 80 bits (fldpi) and rounding it
// into a double would no longer be 3.141592653589793.
func fPi(b *rt) {
	a := b.a
	b.prologue()
	a.MovImm(x64.RAX, 0x400921FB54442D18) // 3.141592653589793
	b.epilogue()
}

// @f_sine(rcx=那 64 位)：返回 rax=它的正弦，参数是弧度。
//
// en: @f_sine (rcx = those 64 bits): returns rax = its sine, the argument in radians.
func fSine(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.MovsdStore(x64.RBP, -8, 0)
	a.FldDouble(x64.RBP, -8)
	a.Fsin()
	a.FstpDouble(x64.RBP, -8)
	a.MovsdLoad(0, x64.RBP, -8)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_cosine(rcx=那 64 位)：返回 rax=它的余弦，参数是弧度。
//
// en: @f_cosine (rcx = those 64 bits): returns rax = its cosine, the argument in radians.
func fCosine(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.MovsdStore(x64.RBP, -8, 0)
	a.FldDouble(x64.RBP, -8)
	a.Fcos()
	a.FstpDouble(x64.RBP, -8)
	a.MovsdLoad(0, x64.RBP, -8)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_natural_log(rcx=那 64 位)：返回 rax=它的自然对数。
//
// x87 没有"直接算 ln"的那条指令，但 fyl2x 算的是 ST(1) × log₂(ST(0))：先压 1 再压 x，
// 就得到 log₂(x)，出来之后乘上 ln(2) 那个常数就是 ln(x)。常数走的是已经验证过的路径
// （movabs 放进通用寄存器、再 movsd 搬到 xmm，@f_sqrt 收参数就是这么做的），
// 不在 FPU 栈上再压一个常数——压常数与除以常数那两条指令的编码在测试里对不上，宁可少用一条。
//
// 0 返回负无穷、负数是 NaN，都不抛——与 `math.square_root` 负数那一侧同一条路径。
//
// en: @f_natural_log (rcx = those 64 bits): returns rax = its natural logarithm. x87 has no
// "compute ln" instruction, but fyl2x computes ST(1) x log2(ST(0)): push 1 then push x to get
// log2(x), and multiply the result by the ln(2) constant. The constant travels the already
// verified path of "movabs into a general register, then movsd into xmm" (the way @f_sqrt
// takes its argument), rather than pushing another constant onto the FPU stack - pushing it
// and dividing by it both failed to match encodings in the tests, so one fewer instruction is
// used. 0 returns negative infinity and a negative returns NaN, neither throwing - the same
// route as math.square_root's negative side.
func fNaturalLog(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.MovsdStore(x64.RBP, -8, 0)
	a.Fld1()
	a.FldDouble(x64.RBP, -8)
	a.Fyl2x()
	a.FstpDouble(x64.RBP, -8)
	a.MovsdLoad(0, x64.RBP, -8)
	a.MovImm(x64.RCX, 0x3FE62E42FEFA39F4) // 0.6931471805599453＝ln(2)
	a.MovsdFromGPR(1, x64.RCX)
	a.Mulsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_random()：返回 rax=[0,1) 之间那一个小数的 64 位。
//
// 状态推进与 `math.random` 完全一致（同一颗种子、同三步 xorshift、同一个乘数），
// 区别只在收尾：取那 64 位低的 53 位当尾数，除以 2^53。53 位是 double 能精确表示的
// 位数上限，所以返回的每个数都是 k/2^53 那一个，两端各至多差半个 ulp。
//
// en: @f_random(): returns rax = the 64 bits of a float in [0,1). The state advance is
// identical to math.random (same seed, same three xorshift steps, same multiplier); only the
// ending differs: the low 53 bits of those 64 become the mantissa divided by 2^53. 53 bits is
// the most a double represents exactly, so every returned value is k/2^53, each end off by at
// most half an ulp.
func fRandomFloat(b *rt) {
	a := b.a
	b.prologue()
	// 种子未初始化时先用计数器取一次（与 @i_random 同一条路径）
	b.loadRIP(x64.RAX, "rndSeed")
	a.Test(x64.RAX, x64.RAX)
	stepped := b.label()
	a.Jcc(x64.NE, stepped)
	a.LeaMem(x64.RCX, x64.RBP, -24)
	b.impCall("QueryPerformanceCounter")
	b.get(x64.RAX, 2)
	fresh := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, fresh)
	a.MovImm(x64.RAX, -0x61c8864680b583ab) // 0x9E3779B97F4A7C15：黄金比例的 64 位
	a.Mark(fresh)
	b.storeRIP(x64.RAX, "rndSeed")

	a.Mark(stepped)
	b.loadRIP(x64.RAX, "rndSeed")
	a.Mov(x64.RCX, x64.RAX)
	a.ShlImm(x64.RCX, 13)
	a.Xor(x64.RAX, x64.RCX)
	a.Mov(x64.RCX, x64.RAX)
	a.ShrImm(x64.RCX, 7)
	a.Xor(x64.RAX, x64.RCX)
	a.Mov(x64.RCX, x64.RAX)
	a.ShlImm(x64.RCX, 17)
	a.Xor(x64.RAX, x64.RCX)
	b.storeRIP(x64.RCX, "rndSeed")
	a.MovImm(x64.RCX, 0x2545F4914F6CDD1D)
	a.Imul(x64.RAX, x64.RCX)

	a.ShlImm(x64.RAX, 11) // 只留低的 53 位
	a.ShrImm(x64.RAX, 11)
	a.CvtSI2SD(0, x64.RAX)
	a.MovImm(x64.RCX, 0x4340000000000000) // 2^53
	a.MovsdFromGPR(1, x64.RCX)
	a.Divsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @i_sign(rcx=整数)：返回 rax=正、负还是零。
//
// 三档结果：比零大返回 1、比零小返回 -1、正好是零返回 0。判据就是既有比较用的那两条符号位，
// 不另定义一套"什么算正"。
//
// en: @i_sign (rcx = integer): returns rax = positive, negative or zero. Three results: above
// zero 1, below zero -1, exactly zero 0. It uses the two comparison sign bits already in use
// rather than inventing a definition of "what counts as positive".
func iSign(b *rt) {
	a := b.a
	b.prologue()
	isZero, isNeg, done := b.label(), b.label(), b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, isZero)
	a.Jcc(x64.S, isNeg)
	a.MovImm(x64.RAX, 1)
	a.Jmp(done)
	a.Mark(isNeg)
	a.MovImm(x64.RAX, -1)
	a.Jmp(done)
	a.Mark(isZero)
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(done)
	b.epilogue()
}

// @f_sign(rcx=那 64 位)：返回 rax=那一个小数正、负还是零。
//
// 比较在整数位型上进行：去掉符号位，剩下的就是幅度——幅度为 0 就是 ±0.0，符号位那一个 1 定正负。
// 不使用比较指令（Ucomisd）：这台机器执行它仍然报非法指令，x64/asm.go 里有记录。
// -0.0 去掉符号位也是 0，所以两个零都返回 0，与"小数按 IEEE 规则比较"的既定语义一致。
// NaN 返回 0：这是该成员定下的结果——它的幅度比无穷还大，既不算正也不算负，
// 三档结果里只剩零这一档；该规则由测试固定。
//
// en: @f_sign (rcx = those 64 bits): returns rax = whether that float is positive, negative or
// zero. The comparison is on the integer pattern: removing the sign bit leaves the magnitude -
// magnitude 0 means ±0.0, and the sign bit's 1 decides direction. The comparison instruction
// (Ucomisd) is not used: this machine still faults it, as recorded in x64/asm.go. Removing
// -0.0's sign bit also leaves 0, so both zeros return 0, consistent with the rule that floats
// compare per IEEE. NaN returns 0: that is this member's fixed answer - its magnitude exceeds
// even infinity, it is neither positive nor negative, and of the three results only zero is
// left; the tests pin this rule.
func fSign(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	isZero, isNeg, done := b.label(), b.label(), b.label()
	// 幅度：去掉符号位（左移一位再逻辑右移一位），剩下指数与尾数
	b.get(x64.RDX, 0)
	a.ShlImm(x64.RDX, 1)
	a.ShrImm(x64.RDX, 1)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, isZero)
	// 幅度大于无穷（0x7FF0…）就是 NaN：按上面定下的结果与零同走 isZero
	a.MovImm(x64.RAX, 0x7FF0000000000000)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.A, isZero)
	// 剩下的只有符号位
	b.get(x64.RAX, 0)
	a.ShrImm(x64.RAX, 63)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.E, isNeg)
	a.MovImm(x64.RAX, 1)
	a.Jmp(done)
	a.Mark(isNeg)
	a.MovImm(x64.RAX, -1)
	a.Jmp(done)
	a.Mark(isZero)
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(done)
	b.epilogue()
}

// @set_seed(rcx=那一个数)：设置随机数的状态，返回 nothing。
//
// 交进来的那一个数成为 xorshift 的状态，往后每一次 math.random／random_float 都从它推进，
// 所以同一份程序跑两次返回同一串——复现实验、做测试要的就是这一条。
// 入参为 0 时替换成一个常数：状态是 0 时 xorshift 推进一百次还是 0，就成了"永远返回同一个数"
// 的伪随机，与首次调用时计数器恰好取回 0 那一分支是同一处理（见 iRandom）。
//
// en: @set_seed (rcx = that number): sets the random state and returns nothing. The number
// passed in becomes the xorshift state, and every later math.random/random_float advances from
// it, so the same program run twice returns the same sequence - which is exactly what
// reproducing an experiment or writing a test needs. 0 gets a constant substituted: a state of
// 0 stays 0 after a hundred steps, which is fake randomness that "always returns the same
// number", the same treatment as the branch where the counter returns 0 on the first call (the
// rule inside iRandom).
func setSeed(b *rt) {
	a := b.a
	b.prologue()
	a.Test(x64.RCX, x64.RCX)
	fresh := b.label()
	a.Jcc(x64.NE, fresh)
	a.MovImm(x64.RCX, -0x61c8864680b583ab) // 0x9E3779B97F4A7C15：黄金比例的 64 位
	a.Mark(fresh)
	b.storeRIP(x64.RCX, "rndSeed")
	b.epilogue()
}

// @f_tangent(rcx=那 64 位)：返回 rax=正切，参数是弧度。
//
// CPU 没有现成的正切指令，但有 fsin 与 fcos，相除即得。
// 余弦为 0 的那一侧（π/2 那种）返回 ±无穷，除法在 IEEE 下自然给出这个结果，不抛——
// 与 math.square_root 负数返回 NaN、math.natural_log 在 0 返回 -Inf 同一条路径。
//
// en: @f_tangent (rcx = those 64 bits): returns rax = the tangent, the argument in radians.
// There is no ready-made tangent instruction, but fsin and fcos each exist and dividing them
// gives it. Where the cosine is 0 (pi/2 and friends) the result is ±infinity, which is what the
// division gives under IEEE, and nothing is thrown - the same route as sqrt of a negative
// returning NaN and natural_log at 0 returning -Inf.
func fTangent(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.MovsdStore(x64.RBP, -8, 0) // 参数留在帧里，两趟各取一次
	a.FldDouble(x64.RBP, -8)
	a.Fsin()
	a.FstpDouble(x64.RBP, -16) // 正弦
	a.FldDouble(x64.RBP, -8)
	a.Fcos()
	a.FstpDouble(x64.RBP, -24) // 余弦
	a.MovsdLoad(0, x64.RBP, -16)
	a.MovsdLoad(1, x64.RBP, -24)
	a.Divsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_arctangent(rcx=那 64 位)：返回 rax=反正切，落在 -π/2～π/2 区间。
//
// fpatan 算的是 ST(1)/ST(0) 的反正切并 pop，所以先压参数、再压 1.0：
// 栈顶那个位置成为分母 1，下面那个位置是 x，出来的就是 atan(x)。
// ±无穷那一侧返回 ±π/2——那是 fpatan 本来就给出的结果，不抛。
//
// en: @f_arctangent (rcx = those 64 bits): returns rax = the arctangent, in the range -pi/2 to
// pi/2. fpatan computes the arctangent of ST(1)/ST(0) and pops, so push the argument first and
// then 1.0: the top of the stack becomes the denominator 1, the one below is x, and what comes
// out is atan(x). ±infinity returns ±pi/2, which fpatan already gives, so nothing throws.
func fArctangent(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.MovsdStore(x64.RBP, -8, 0)
	a.FldDouble(x64.RBP, -8)
	a.Fld1()
	a.Fpatan()
	a.FstpDouble(x64.RBP, -8)
	a.MovsdLoad(0, x64.RBP, -8)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_log_base_10(rcx=那 64 位)：返回 rax=以 10 为底的对数。
//
// fyl2x 算的是 log₂，结果乘 log₁₀(2) 就换成以 10 为底：与 @f_natural_log 乘 ln(2) 是同一种做法，
// 一次换底、一次舍入。0 返回 -Inf、负数返回 NaN，都不抛（这两个结果已由自然对数定下）。
//
// en: @f_log_base_10 (rcx = those 64 bits): returns rax = the base-10 logarithm. fyl2x computes
// log base 2, so multiplying the result by log10(2) converts it to base 10 - the same move as
// @f_natural_log multiplying by ln(2), one base change costing one rounding. 0 returns -Inf and
// a negative returns NaN, neither throwing (both answers were already fixed by natural log).
func fLogBase10(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX)
	a.MovsdStore(x64.RBP, -8, 0)
	a.Fld1()
	a.FldDouble(x64.RBP, -8)
	a.Fyl2x()
	a.FstpDouble(x64.RBP, -8)
	a.MovsdLoad(0, x64.RBP, -8)
	a.MovImm(x64.RCX, 0x3FD34413509F79FF) // 0.30102999566398120＝log₁₀(2)
	a.MovsdFromGPR(1, x64.RCX)
	a.Mulsd(0, 1)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

// @f_exponential(rcx=那 64 位)：返回 rax=e 的 x 次方。
//
// x87 只有算 2 的幂那条 f2xm1，且只在 -1～1 之间精确，所以先换底：
// k = x/ln2 就近取整，r = x - k·ln2（|r| ≤ ln2/2，落在 f2xm1 的精确范围里），
// e^x = 2^k × e^r，而 e^r = 1 + f2xm1(r·log₂e)。补 2^k 用 fscale，它取指数时只用整数部分。
// k 与 r 各引入一次舍入，所以结果与最精确值差几个 ulp——与 natural_log 同一种容差说明，
// 测试比较的是区间，不固定最后两位。
//
// 三个边界情形先排除，不让它们进入上面那条路径：
// NaN 返回 NaN；x 大于 709.782712893384 返回 +Inf；x 小于 -745.1332191019411 返回 0.0。
// 后两条正是 IEEE 的结果（超出 double 能容纳的最大值、低于最小一档），不抛——
// 与 text.to_float 超过 double 返回 ±无穷是同一条路径。
//
// 这三种判断走整数位型，不使用比较指令（Ucomisd 在这台机器上执行就报非法指令，x64/asm.go 有记录）：
// 去掉符号位剩下的就是幅度，正数一侧幅度与值同序，负数一侧还需要再检查符号位。
//
// en: @f_exponential (rcx = those 64 bits): returns rax = e raised to x. x87 only has f2xm1
// for powers of two and it is only accurate between -1 and 1, so the base is changed first:
// k = x/ln2 rounded to nearest, r = x - k*ln2 (|r| <= ln2/2, inside f2xm1's accurate range),
// e^x = 2^k * e^r with e^r = 1 + f2xm1(r*log2 e). The 2^k is supplied by fscale, whose exponent
// uses only the integer part. k and r are one rounding each, so the tail differs from the exact
// value by a few ulp - stated the same way as the natural_log entry; the tests compare against
// a band and do not pin the last two digits. Three corner cases are excluded before they reach
// that path: NaN returns NaN, x above 709.782712893384 returns +Inf, x below -745.1332191019411
// returns 0.0. The last two are exactly IEEE's results (above the largest double, below the
// smallest band) and throw nothing - the same route as text.to_float returning ±infinity past a
// double. Those three are judged on integer bit patterns, never through the comparison
// instruction (Ucomisd faults as an illegal instruction on this machine, as recorded in
// x64/asm.go): removing the sign bit leaves the magnitude, which on the positive side orders
// like the value and on the negative side needs the sign bit looked at afterwards.
func fExponential(b *rt) {
	a := b.a
	b.prologue()
	a.MovsdFromGPR(0, x64.RCX) // x 留在 xmm0，后面的路径还要用它
	isNum, notBig, notSmall := b.label(), b.label(), b.label()
	a.Mov(x64.RDX, x64.RCX)
	a.ShlImm(x64.RDX, 1)
	a.ShrImm(x64.RDX, 1)

	// NaN：幅度比无穷（0x7FF0…）还大
	a.MovImm(x64.RAX, 0x7FF0000000000000)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.BE, isNum)
	a.MovImm(x64.RAX, 0x7FF8000000000000)
	b.epilogue()
	a.Mark(isNum)

	// 上界：幅度超过 709.782712893384 的位型，且符号位是 0，就是 +Inf
	// 负的那一侧幅度同样能超过这条线，所以必须再检查一次符号位
	a.MovImm(x64.RAX, 0x40862E42FEFA39EF)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.BE, notBig)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.S, notBig)
	a.MovImm(x64.RAX, 0x7FF0000000000000)
	b.epilogue()
	a.Mark(notBig)

	// 下界：幅度超过 745.1332191019411 的位型，且符号位是 1，就是 0.0
	a.MovImm(x64.RAX, 0x40874910D52D3051)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.BE, notSmall)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NS, notSmall)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
	a.Mark(notSmall)

	a.MovsdStore(x64.RBP, -8, 0) // 原始的 x 留在帧里
	// k = x/ln2 就近取整，留在帧里给后面 fscale 当指数用
	a.MovImm(x64.RCX, 0x3FE62E42FEFA39EF) // ln(2)
	a.MovsdFromGPR(1, x64.RCX)
	a.MovsdLoad(0, x64.RBP, -8)
	a.Divsd(0, 1)
	a.Roundsd(0, 0, 0) // 就近取整（平手取偶），与 math.round 同一档
	a.MovsdStore(x64.RBP, -16, 0)
	// r = x - k·ln2：k 还在 xmm0，x 从帧里再取一次
	a.Mulsd(0, 1) // xmm0 = k·ln2
	a.MovsdLoad(1, x64.RBP, -8)
	a.Subsd(1, 0) // xmm1 = r，|r| ≤ ln2/2
	// e^r = 1 + f2xm1(r·log₂e)
	a.MovImm(x64.RCX, 0x3FF71547652B82FE) // log₂(e)
	a.MovsdFromGPR(0, x64.RCX)
	a.Mulsd(1, 0)
	a.MovsdStore(x64.RBP, -24, 1)
	a.FldDouble(x64.RBP, -24)
	a.F2xm1()
	a.Fld1()
	a.FAddP()
	a.FstpDouble(x64.RBP, -24) // e^r
	// 乘上 2^k：fscale 要求栈顶位置作数值、次顶位置作指数
	a.FldDouble(x64.RBP, -16) // k（双精度，fscale 取它的整数部分）
	a.FldDouble(x64.RBP, -24) // e^r
	a.Fscale()
	a.FstpDouble(x64.RBP, -24)
	// fscale 只修改栈顶，次顶的 k 还压在 x87 栈上：不取走的话每调一次就占一个位置，
	// 八次之后栈满，后面每次调用都算出 NaN。这里用帧里的一个空槽把它 pop 掉。
	a.FstpDouble(x64.RBP, -32)
	a.MovsdLoad(0, x64.RBP, -24)
	a.MovsdToGPR(x64.RAX, 0)
	b.epilogue()
}

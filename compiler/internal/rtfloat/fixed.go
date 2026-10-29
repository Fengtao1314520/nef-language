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

// fixed.go 是 Shortest 的"能在机器码里写出来"的那一版：
// 不用 math/big，只用定长的 32 位 limb 数组与四种动作（乘小数、移位、比大小、除小数）。
//
// 它是 @put_float 的图纸：机器码版按这里的步骤逐条翻译，两边用同一个逐值比对测试验收。
// 先在这一层完成的原因是最短往返的错误不触发编译错——先用 Go 排查"定长够不够、
// 平手怎么舍、边界算不算"这些问题，机器码一侧就只剩翻译工作。
//
// 精度上不做妥协：所有比较都是整数的精确比较，不假设浮点近似够用。
//
// en: fixed.go is the version of Shortest that can be written out in machine code: no math/big,
// only a fixed-length array of 32-bit limbs and four operations (multiply by a power of ten,
// shift, compare, divide by a power of ten). It is the drawing for @put_float: the machine-code
// version follows these steps one by one, and the two sides are accepted by the same
// value-by-value comparison test. A shortest-roundtrip bug never fails the compile,
// so Go resolves the open questions first (is the fixed width enough, how does a tie round,
// does a boundary count) and only translation is left for machine code. Nothing is
// approximated: every comparison is an exact integer comparison, with no assumption that
// floating point is good enough.
package rtfloat

import (
	"math"
	"strconv"
	"strings"
)

// limbs 是定长大整数的宽度。最宽的一种情形是 2^-1074 展开成十进制（751 位有效数字，
// 约 2494 位二进制），再加上比对时两边乘上的 2^1080·5^1080（约 3570 位），
// 所以留到 130 个 32 位（4160 位），两端都有余量。
//
// en: limbs is the width of the fixed-size big integer. The widest case is 2^-1074 spread out
// in decimal (751 significant digits, about 2494 bits), plus the 2^1080·5^1080 both sides are
// multiplied by during comparison (about 3570 bits), so 130 32-bit limbs (4160 bits) leave room
// at both ends.
const limbs = 130

type bn struct{ a [limbs]uint32 }

func (z *bn) setU64(v uint64) {
	for i := range z.a {
		z.a[i] = 0
	}
	z.a[0] = uint32(v)
	z.a[1] = uint32(v >> 32)
}

func (z *bn) mulSmall(k uint32) {
	var carry uint64
	for i := range z.a {
		p := uint64(z.a[i])*uint64(k) + carry
		z.a[i] = uint32(p)
		carry = p >> 32
	}
	if carry != 0 {
		panic("定长大整数溢出：limbs 开小了")
	}
}

func (z *bn) shlBits(n uint) {
	if n == 0 {
		return
	}
	w := int(n / 32)
	b := n % 32
	for i := limbs - 1; i >= 0; i-- {
		var v uint64
		if i >= w {
			v = uint64(z.a[i-w]) << b
			if b > 0 && i > w {
				v |= uint64(z.a[i-w-1]) >> (32 - b)
			}
		}
		z.a[i] = uint32(v)
	}
}

// divSmall 把 z 除以 k，返回余数，商留在 z 里。取十进制位就是一位一位除 10。
//
// en: divSmall divides z by k, returns the remainder and leaves the quotient in z. Taking
// decimal digits one at a time is dividing by 10 repeatedly.
func (z *bn) divSmall(k uint32) uint32 {
	var rem uint64
	for i := limbs - 1; i >= 0; i-- {
		cur := rem<<32 | uint64(z.a[i])
		z.a[i] = uint32(cur / uint64(k))
		rem = cur % uint64(k)
	}
	return uint32(rem)
}

func (z *bn) cmp(y *bn) int {
	for i := limbs - 1; i >= 0; i-- {
		if z.a[i] != y.a[i] {
			if z.a[i] > y.a[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func (z *bn) bits() uint {
	for i := limbs - 1; ; i-- {
		if z.a[i] != 0 {
			return uint(i)*32 + uint(32-bitsLeadingZero32(z.a[i]))
		}
		if i == 0 {
			return 0
		}
	}
}

func bitsLeadingZero32(v uint32) int {
	n := 0
	for v&0x80000000 == 0 && v != 0 {
		v <<= 1
		n++
	}
	if v == 0 {
		return 32
	}
	return n
}

// mulPow 把 z 乘上 2^p 再乘上 5^q：两边要拉到同一个分母上就靠它。
//
// en: mulPow multiplies z by 2^p and then by 5^q: this is how two sides get pulled onto a
// common denominator.
func (z *bn) mulPow(p, q uint) {
	z.shlBits(p)
	for i := uint(0); i < q; i++ {
		z.mulSmall(5)
	}
}

// ShortestFixed 返回跟 Shortest 一样的字符串，只是全程使用定长大整数。
// 两个实现在测试里逐值比对，所以这一版可以作为机器码的蓝本。
//
// en: ShortestFixed returns the same string as Shortest, only it stays on fixed-size big
// integers the whole way. The two implementations are compared value by value in the tests, so
// this one can serve as the blueprint for machine code.
func ShortestFixed(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	sign := ""
	if math.Signbit(v) {
		sign = "-"
		v = -v
	}
	if v == 0 {
		return sign + "0.0"
	}

	m, e := decompose(v) // v = m × 2^e，m 是整数

	// 精确的十进制数字串。两条路：
	//   e ≥ 0：值本来就是整数，直接左移，小数点不用挪；
	//   e < 0：2^e = 5^|e| ÷ 10^|e|，所以乘 5 再把小数点往左推 |e| 位。
	// 乘 5 这一步不能两条路共用：对 e>0 也乘 5 会让位数膨胀到 10^1665 量级。
	//
	// en: The exact decimal digit string, by two routes: when e >= 0 the value is already an
	// integer, so shift left and the decimal point does not move; when e < 0, 2^e = 5^|e| /
	// 10^|e|, so multiply by 5 and push the point |e| places to the left. The multiply-by-five
	// step cannot be shared: doing it for e > 0 too inflates the width to something like 10^1665.
	var z bn
	z.setU64(m)
	scale := 0
	if e >= 0 {
		z.shlBits(uint(e))
	} else {
		for i := 0; i < -e; i++ {
			z.mulSmall(5)
		}
		scale = e
	}
	digits := decimalDigits(&z) // 最高位在前
	e10 := len(digits) - 1 + scale

	// 区间两端：c 落在 (lo, hi) 之内就一定收成 v；正好落在端点上时按"平手取偶"判归属。
	//
	// lo 要分两种：v 是某个 2 的整数次幂（尾数正好 2^52）时，它的**下邻居**离得只有半个
	// ulp——上一档的尾数多一位、指数少一档，所以下面的间隔是 2^(e-1)，中点就是 v - 2^(e-2)。
	// 最小那个正规数（2^-1022）不算：它的下邻居是次正规，间隔与常规相同。
	// hi 不用分：上面永远是 2^e，只有最大的那个 double 上面没有邻居。
	loK, loJ := uint64(2*m-1), e-1
	if m == 1<<52 && e > -1074 {
		loK, loJ = 4*m-1, e-2
	}
	hiK, hiJ := uint64(2*m+1), e-1

	for p := 1; p <= 17; p++ {
		d, s, grew := roundSignificant(digits, p, e10)
		if d == "" {
			continue
		}
		_ = grew
		switch {
		case tiesAway(d, s, u2{loK, loJ}, u2{hiK, hiJ}, e, m):
			continue // 正好落在中点上，而按取偶规则归属邻居
		case inside(d, s, u2{loK, loJ}, u2{hiK, hiJ}, e):
			return sign + layout(d, s)
		}
	}
	// 17 位一定够，走到这里是给算术留的保险：把整串（最多 17 位）原样返回。
	tail := digits
	if len(tail) > 17 {
		tail = tail[:17]
	}
	return sign + layout(string(trimZeros(tail)), e10-len(tail)+1)
}

// decompose 把 v 分解为 m × 2^e，m 是 53 位以内的整数（含隐式那一位）。
//
// en: decompose splits v into m × 2^e, where m is an integer of at most 53 bits (including the
// implicit one).
func decompose(v float64) (uint64, int) {
	bits := math.Float64bits(v)
	exp := int((bits >> 52) & 0x7FF)
	mant := bits & ((uint64(1) << 52) - 1)
	if exp == 0 { // 次正规：没有隐式那一位，指数固定为 -1074
		return mant, -1074
	}
	return mant | (uint64(1) << 52), exp - 1075
}

// roundSignificant 从 digits 取 p 位有效数字，按"最近、平手取偶"舍入。
// 返回舍入后的数字串（去掉末尾的 0）、该串应乘的 10 的幂，以及进位是否使位数增加一位。
//
// en: roundSignificant takes p significant digits off digits, rounding to nearest with ties to
// even. It returns the rounded digit string (trailing zeroes removed), the power of ten that
// string should be multiplied by, and whether a carry pushed it one digit wider.
func roundSignificant(digits []byte, p, e10 int) (string, int, bool) {
	head := p
	grew := false
	if len(digits) <= p {
		head = len(digits) // 位数不足：整串即结果，无需看后续
	}
	q := make([]byte, head)
	copy(q, digits[:head])
	if head == p && len(digits) > p {
		next := digits[p]
		sticky := anyNonZero(digits[p+1:])
		// 超过一半就进位；恰好落在中点上时看已保留末位的奇偶，为奇才进（取偶）
		if next > '5' || (next == '5' && (sticky || isOdd(q))) {
			grew = carryUp(q)
		}
	}
	trimmed := trimZeros(q)
	// 数字串的最高位落在 10^(e10+grew) 这一档，所以末尾的幂随之调整
	s := e10 + boolToInt(grew) - (len(q) - 1)
	s += len(q) - len(trimmed)
	return string(trimmed), s, grew
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// u2 是一个二进制端点：系数 × 2^幂。区间两端各是一个这样的数，系数是 uint64
// 装得下的小整数（2m±1 这种），只在比较时随公共缩放放大为大整数。
//
// en: u2 is one binary endpoint: coefficient × 2^power. Each end of the interval is such a
// number, with a small coefficient that fits in uint64 (the 2m±1 kind); only during comparison
// does it grow into a big integer along with the common scale.
type u2 struct {
	k uint64
	j int
}

// inside 判断 d × 10^s 是否落在"会收成 v 的那个范围"内。
//
// 两侧都同时含 2 的幂与 5 的幂，比较前先同乘 2^u·5^v 化为整数——不做任何近似。
// 下界取"闭"：恰好等于端点的情形已由 tiesAway 先筛过，到这里剩下的都确实比 v 更近。
//
// en: inside decides whether d × 10^s falls into the range that rounds to v. Each side carries
// both a power of two and a power of five, so both are multiplied by 2^u·5^v to become integers
// first - no approximation at all. The lower bound counts as closed: the
// exactly-on-the-endpoint case was already screened by tiesAway, so what reaches here genuinely
// lies nearer to v.
func inside(d string, s int, lo, hi u2, e int) bool {
	var L, R bn
	u, v := commonScale(s, lo.j)
	L.setU64(parseU64(d))
	L.mulPow(scaled(s, u), scaled(s, v))
	R.setU64(lo.k)
	R.mulPow(scaled(lo.j, u), v)
	if L.cmp(&R) < 0 {
		return false
	}
	if e >= 1023 { // 最大的 double：上方没有邻居，不设上界
		return true
	}
	R.setU64(hi.k)
	R.mulPow(scaled(hi.j, u), v)
	return L.cmp(&R) <= 0
}

// tiesAway 判断 d × 10^s 是否恰好落在某个端点上、且按"平手取偶"不归 v。
// v 的尾数 m 是偶数时两个端点都归 v，直接放行；m 是奇数时两端都归邻居。
//
// en: tiesAway decides whether d × 10^s sits exactly on an endpoint and, under
// round-half-to-even, does not belong to v. When v's mantissa m is even, both endpoints round
// to v and it passes; when m is odd, both sides must be given to the neighbour.
func tiesAway(d string, s int, lo, hi u2, e int, m uint64) bool {
	if m%2 == 0 {
		return false
	}
	var L, R bn
	u, v := commonScale(s, lo.j)
	L.setU64(parseU64(d))
	L.mulPow(scaled(s, u), scaled(s, v))
	R.setU64(lo.k)
	R.mulPow(scaled(lo.j, u), v)
	if L.cmp(&R) == 0 {
		return true
	}
	R.setU64(hi.k)
	R.mulPow(scaled(hi.j, u), v)
	return L.cmp(&R) == 0
}

// commonScale 返回 u 与 v：两边同乘 2^u·5^v 之后全是整数。
// 十进制侧 10^s = 2^s·5^s，s 为负就要补两份；二进制侧的 j 同理。
// jmin 传入的是下界的 2 指数，上界的指数不会更小，所以一次计算即可覆盖。
//
// en: commonScale returns u and v such that multiplying both sides by 2^u·5^v makes everything
// an integer. On the decimal side 10^s = 2^s·5^s, so a negative s needs two top-ups; the
// exponent j on the binary side is handled the same way. jmin is the lower endpoint's power of
// two, and the upper one is never smaller, so one computation covers both.
func commonScale(s, jmin int) (uint, uint) {
	u, v := uint(0), uint(0)
	if s < 0 {
		u = uint(-s)
		v = uint(-s)
	}
	if -jmin > int(u) {
		u = uint(-jmin)
	}
	return u, v
}

// scaled 把"某一侧自带的 2^幂"与公共的 2^u 合起来。指数为负时必须先相加再转无符号：
// 直接 uint(n) 会回绕成极大值，移位一遍就会使整个比较失真——不报错，
// 只表现为打印出来的位数异常增多。
//
// en: scaled combines the 2^power a side already carries with the common 2^u. A negative
// exponent must be added before converting to unsigned: uint(n) would wrap into an astronomical
// number and the shift would corrupt the whole comparison - no error is raised, the printed
// value simply carries extra digits.
func scaled(n int, u uint) uint {
	m := n + int(u)
	if m < 0 {
		panic("公共缩放算小了：2 的指数还是负的")
	}
	return uint(m)
}

func anyNonZero(d []byte) bool {
	for _, c := range d {
		if c != '0' {
			return true
		}
	}
	return false
}

func isOdd(d []byte) bool { return len(d) > 0 && (d[len(d)-1]-'0')%2 == 1 }

// carryUp 把这串数字加一，返回有没有撑出一位（999 → 1000）。
//
// en: carryUp adds one to this digit string and reports whether it grew a digit (999 -> 1000).
func carryUp(d []byte) bool {
	for i := len(d) - 1; i >= 0; i-- {
		if d[i] == '9' {
			d[i] = '0'
			continue
		}
		d[i]++
		return false
	}
	d[0] = '1' // 全为 9：位数增加一位，最高位为 1，其余已是 0
	return true
}

func trimZeros(d []byte) []byte {
	i := len(d)
	for i > 1 && d[i-1] == '0' {
		i--
	}
	return d[:i]
}

// decimalDigits 返回 z 的十进制数字，最高位在前：逐位除 10 收集余数。
//
// en: decimalDigits returns the decimal digits of z, most significant first, by dividing by 10
// one digit at a time and collecting the remainders.
func decimalDigits(z *bn) []byte {
	var work bn
	work = *z
	var out []byte
	for work.bits() > 0 {
		r := work.divSmall(10)
		out = append(out, byte('0')+byte(r))
	}
	if len(out) == 0 {
		out = append(out, '0')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func parseU64(d string) uint64 {
	v, err := strconv.ParseUint(d, 10, 64)
	if err != nil {
		panic("有效数字串读不回整数：" + d)
	}
	return v
}

// layout 把"d 这串数字 × 10^s"摆成规则要求的形状：不用指数写法，而且一定带一个小数点。
//
// s 是这串数字**最后一位**所在的档位，所以"小数点前有几位"是 位数+s。三种摆法：
// 全在小数点后面（补零开头）、小数点落在串中间、末尾补零再写 ".0"。
//
// en: layout arranges "the digits d × 10^s" into the shape the rules require: no exponent
// notation, and always a decimal point. s is the position of the LAST digit of the string, so
// the number of digits before the point is len(digits)+s. Three arrangements: everything lands
// after the point (leading zeroes), the point falls inside the string, or zeroes are appended
// and a ".0" is written.
func layout(d string, s int) string {
	head := len(d) + s // 小数点前面占几位
	switch {
	case head <= 0:
		return "0." + strings.Repeat("0", -head) + d
	case head >= len(d):
		return d + strings.Repeat("0", head-len(d)) + ".0"
	default:
		return d[:head] + "." + d[head:]
	}
}

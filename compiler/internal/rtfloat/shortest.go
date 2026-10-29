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

// Package rtfloat 是 `float` 打印的参考实现：最短往返、并且永远带一个小数点。
//
// 规则要先行写对：`@put_float` 要在裸机器码里做同一件事，而十进制最短往返
// 错起来极隐蔽——`0.1+0.2` 少打印一位、或者 `3.0` 被打印成 `3`，都不会让编译失败。
// 所以这里先用 Go 把规则定死并跟 strconv 逐值比对十万随机双精度，机器码那一版必须把它逐步
// 翻译出来；两边的逐值比对测试就是这个翻译的验收测试。
//
// 这一版跑的是 math/big 的精确算术，不为了省几步用浮点近似——近似出来的位数正是
// "看着对、抄回去不是同一个数"的那种错。
//
// en: Package rtfloat is the reference implementation of `float` printing: shortest roundtrip,
// and always with a decimal point. The rule had to be written
// correctly before being encoded: @put_float does the same thing in bare machine code, and a
// shortest-roundtrip mistake is extremely hard to see - printing one digit too few for
// `0.1+0.2`, or printing `3` for `3.0`, never fails a compile. So this side fixes the rules in
// Go and compares against strconv over a hundred thousand random doubles; the machine-code
// version must translate it step by step, and the value-by-value comparison between the two IS
// that translation's acceptance test. This version uses exact arithmetic from math/big and
// never takes a floating-point shortcut, because an approximate digit count is exactly what
// produces the "looks right but is not the same number when typed back" bug.
package rtfloat

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Shortest 返回 v 的十进制写法。三条硬性规则：
//
//   - 最短：位数是"照抄回去还是同一个数"的前提下的最小值；
//   - 一定带小数点：`3.0` 不许打印成 `3`；
//   - 不藏误差：`0.1 + 0.2` 打印成 `0.30000000000000004`。
//
// 无穷与非数写 `Inf`／`-Inf`／`NaN`（`Inf` 保留负号，`NaN` 不带符号），与 Go 的 `fmt`、
// C 的 `printf` 同形。机器码那一版 `@put_float` 采用同一条写法。
//
// en: Shortest returns the decimal form of v, under three hard rules: shortest - the
// digit count is the minimum that still reads back as the same number; always with a decimal
// point - `3.0` may not print as `3`; and no hidden error - `0.1 + 0.2` prints as
// `0.30000000000000004`. Infinity and not-a-number are written `Inf` / `-Inf` / `NaN` (the sign
// is kept for Inf, dropped for NaN), the same spelling Go's
// `fmt` and C's `printf` use. The machine-code @put_float follows the same wording.
func Shortest(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 1) {
		return "Inf"
	}
	if math.IsInf(v, -1) {
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

	// 从一位有效数字往上试：第一个能原样抄回同一个数的位数就是答案。
	// 17 位一定够——任何双精度值用 17 位有效数字都能表示。
	for p := 1; p <= 17; p++ {
		s := RoundTo(v, p)
		if back, err := strconv.ParseFloat(s, 64); err == nil && back == v {
			return sign + withDot(s)
		}
	}
	return sign + withDot(strconv.FormatFloat(v, 'f', 16, 64))
}

// RoundTo 把 v 往"最近"的方向收成 p 位有效数字，返回一个不带符号、不带指数的十进制串。
// 末尾的 0 一并去掉（`1.500` 收成 `1.5`）：位数要短，值要一样。
//
// 导出它是为了让测试能逐个位数地试——"还能不能更短"只能这样验证。
//
// en: RoundTo rounds v to p significant digits toward nearest and returns a decimal string with
// no sign and no exponent. Trailing zeroes are dropped (`1.500` becomes `1.5`):
// fewer digits, same value. It is exported so the tests can try digit counts one by one -
// "could it be shorter" can only be checked that way.
func RoundTo(v float64, p int) string {
	r := new(big.Rat).SetFloat64(v) // v 的精确值：双精度是有理数，这里一点不损失
	e := decExp(r)                  // 第一位有效数字落在 10^e 这一档
	digits, e := roundDigits(r, e, p)

	// 小数点该插在哪儿：整数部分占 e+1 位。
	switch head := e + 1; {
	case head >= len(digits):
		return digits + strings.Repeat("0", head-len(digits))
	case head <= 0:
		return "0." + strings.Repeat("0", -head) + digits
	default:
		return digits[:head] + "." + digits[head:]
	}
}

// roundDigits 返回 p 位（或进位之后的 p+1 位）有效数字，以及第一位所在的档位。
//
// en: roundDigits returns p significant digits (or p+1 after a carry) and the position of the
// first digit.
func roundDigits(r *big.Rat, e, p int) (string, int) {
	// 把 v 乘上 10^(p-1-e)，使整数部分正好 p 位，再按最近值取整。
	scale := new(big.Rat).Mul(r, pow10(p-1-e))
	num, den := scale.Num(), scale.Denom()
	n := new(big.Int).Quo(num, den)
	rem := new(big.Int).Rem(num, den)
	// 取最近值；正好落在中点上时往**偶数**那一侧收。
	// 平手的舍入方向语言规则未作规定（规则只定了"最短往返"），这里跟 strconv 与 IEEE 的
	// 默认取整保持一致：同一段数字反复抄录不会变样，也不向一侧引入偏置。
	switch twice := new(big.Int).Lsh(rem, 1).Cmp(den); {
	case twice > 0:
		n.Add(n, big.NewInt(1))
	case twice == 0 && n.Bit(0) == 1:
		n.Add(n, big.NewInt(1))
	}
	digits := n.String()
	if len(digits) > p { // 9.99 收成两位是 10：位数增加一位，小数点跟着右移
		digits = digits[:len(digits)-1]
		e++
	}
	digits = strings.TrimRight(digits, "0")
	if digits == "" {
		digits = "0"
	}
	return digits, e
}

// decExp 返回 e，使得 10^e ≤ v < 10^(e+1)。全程用整数幂比大小，不做任何近似。
// 双精度的范围在 10^-324 到 10^308 之间，所以二分边界取 ±340 就够了。
//
// en: decExp returns the e with 10^e <= v < 10^(e+1), comparing exact integer powers the whole
// way with no approximation. Double precision spans 10^-324 to 10^308, so a bisection box of
// ±340 is enough.
func decExp(r *big.Rat) int {
	lo, hi := -340, 340
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if r.Cmp(pow10(mid)) >= 0 {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// pow10 返回 10^n 的精确有理数。n 可以为负：10^-3 就是 1/1000。
//
// 双精度落在 10^-340 到 10^340 之间，所以把这一整段预先算好存下来：
// 确定位数要拿它比大小，逐个位数尝试要调用上百次，每次现场构造一个 340 位大整数开销过大。
//
// en: pow10 returns the exact rational 10^n. n may be negative: 10^-3 is 1/1000. Doubles stay
// between 10^-340 and 10^340, so that whole band is computed once and kept: digit counting
// compares against it, and trying one digit at a time calls it hundreds of times - building a
// 340-digit integer each time would be wasteful.
func pow10(n int) *big.Rat { return powers[n+340] }

var powers = func() [681]*big.Rat {
	var out [681]*big.Rat
	for i := range out {
		n := i - 340
		a := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(n))), nil)
		if n >= 0 {
			out[i] = new(big.Rat).SetInt(a)
		} else {
			out[i] = new(big.Rat).SetFrac(big.NewInt(1), a)
		}
	}
	return out
}()

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// withDot 保证小数点始终存在：`3` 要写成 `3.0`、`1000000` 要写成 `1000000.0`
// （规则要求永远带一个小数点）。
//
// en: withDot guarantees the decimal point is always present: `3` is written `3.0` and
// `1000000` is written `1000000.0` - the rule requires always having a decimal point.
func withDot(s string) string {
	if strings.Contains(s, ".") {
		return s
	}
	return s + ".0"
}

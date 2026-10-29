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

// float.go 是 @str_to_float 在对照表这一侧的同一份算法。
//
// 不用 Go 的 strconv.ParseFloat：那一版按 IEEE 正确舍入，与本语言规定的做法
// 差一个 ulp——这里只取前 17 位**有效**数字进累积数，往后丢掉的整数位每丢一位把数量级
// 添上一个位置，10 的幂用翻平方算（一次乘法一个舍入）。`9223372036854775808` 那个值最看得出来：
// 它正好等于 2^63，正确舍入的那一版答得准，这一版因为累积数已经先舍过一轮，差一个 ulp。
// 对照表要钉的是这台机器的答复，所以三步照抄 `internal/runtime/text.go` 的 strToFloat。
//
// en: float.go is the same algorithm as @str_to_float, kept on the comparison-table side.
// strconv.ParseFloat is not used: that version rounds correctly per IEEE and differs from the
// method defined by the language by one ulp. Here only the first 17 SIGNIFICANT digits enter the
// accumulator, and every integer digit dropped afterwards adds one position to the magnitude;
// powers of ten are computed by squaring (one multiplication, one rounding). The value
// `9223372036854775808` shows it best: it is exactly 2^63, the correctly rounded version
// answers it exactly while this one is off by one ulp because the accumulator already rounded
// once. The comparison table has to pin what THIS machine answers, so the three steps are
// copied straight from strToFloat in internal/runtime/text.go.
package rtjson

func parseFloat(face string) float64 {
	i := 0
	neg := false
	if i < len(face) && (face[i] == '-' || face[i] == '+') {
		neg = face[i] == '-'
		i++
	}
	mant := 0.0
	sig, scale, dot := 0, 0, false
	hasExp := false
	for i < len(face) {
		c := face[i]
		if c == '.' {
			dot = true
			i++
			continue
		}
		if c == 'e' || c == 'E' {
			hasExp = true
			break
		}
		d := int(c - '0')
		i++
		if sig < 17 {
			mant = mant*10 + float64(d)
			if sig != 0 || d != 0 { // 前导零不占有效位
				sig++
			}
			if dot {
				scale-- // 小数点后面的每一位都要靠那一次除法退回来
			}
		} else if !dot {
			scale++ // 整数那一段每丢掉一位，数量级补上一档
		}
	}

	expV, expNeg, expSeen := 0, false, false
	if hasExp { // i 停在 e 或 E 的位置上
		i++
		if i < len(face) && (face[i] == '-' || face[i] == '+') {
			expNeg = face[i] == '-'
			i++
		}
		for i < len(face) && face[i] >= '0' && face[i] <= '9' {
			expSeen = true
			d := int(face[i] - '0')
			i++
			if expV < 100000 { // 早已出界，再累加没有意义，只需记住见过数字
				expV = expV*10 + d
			}
		}
		if !expSeen {
			return 0 // 指数那一段一个数字都没有：运行期是 10012 号，JSON 这一侧已先拒绝
		}
	}
	if expNeg {
		scale -= expV
	} else {
		scale += expV
	}

	k := scale
	div := false
	if k < 0 {
		k, div = -k, true
	}
	pow, base := 1.0, 10.0
	for k > 0 {
		if k&1 == 1 {
			pow *= base
		}
		base *= base
		k >>= 1
	}
	v := mant * pow
	if div {
		v = mant / pow
	}
	if neg {
		v = -v
	}
	return v
}

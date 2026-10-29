// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package rtfloat_test

import (
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/rtfloat"
)

// 这一组测试是 `@put_float` 的验收测试：参考实现先要跟 strconv 逐位一致，
// 还要自证"最短"——少一位就必须抄不回来。机器码那一版翻译完成后拿同样的标准再跑一遍。

// mustParse 把打印出来的字符串读回 double：抄不回来就是测试自己写错了形状。
func mustParse(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("%q 读不回来：%v", s, err)
	}
	return v
}

// digitsOf 挑出有效数字串：比的是数字，不比写法（`1e21` 与 `1000000000000000000000.0` 是一个数）。
func digitsOf(s string) string {
	s = strings.TrimPrefix(s, "-")
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		s = s[:i]
	}
	s = strings.Replace(s, ".", "", 1)
	s = strings.TrimLeft(s, "0")
	s = strings.TrimRight(s, "0")
	if s == "" {
		return "0"
	}
	return s
}

// v01/v02 必须是**变量**：写成 0.1 + 0.2 那个字面式，Go 会在编译期把常量折成 0.3，
// 测的就不是运行时那一次加法了。打印规则要求的正是运行时的形状。
var (
	v01 = 0.1
	v02 = 0.2
)

// 打印规则规定的几个形状：3.0 要带小数点、0.1+0.2 不许隐藏误差。
func TestSpecifiedShapes(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{3.0, "3.0"},
		{v01 + v02, "0.30000000000000004"},
		{-2.5, "-2.5"},
		{0.5, "0.5"},
		{100, "100.0"},
		{1e21, "1000000000000000000000.0"}, // 最短就是一位有效数字，后面的零是占位
		{1.5e-7, "0.00000015"},
		{0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{2.0 / 3.0, "0.6666666666666666"},
	}
	for _, tc := range cases {
		if got := rtfloat.Shortest(tc.v); got != tc.want {
			t.Errorf("%v：得到 %q，要 %q", tc.v, got, tc.want)
		}
	}
	// 最大与最小那两个非零值只验"抄回同一个数＋带小数点＋位数与 strconv 一致"：
	// 三百多位数字手动抄入必然出错，不把它当作标准。
	for _, v := range []float64{math.MaxFloat64, math.SmallestNonzeroFloat64} {
		s := rtfloat.Shortest(v)
		back, err := strconv.ParseFloat(s, 64)
		if err != nil || math.Float64bits(back) != math.Float64bits(v) {
			t.Fatalf("%v 抄不回来：%q（%v）", v, s, err)
		}
		if !strings.Contains(s, ".") {
			t.Fatalf("%v 印成 %q：少了那个小数点", v, s)
		}
		if digitsOf(s) != digitsOf(strconv.FormatFloat(v, 'g', -1, 64)) {
			t.Fatalf("%v：位数与 strconv 对不上", v)
		}
	}
}

// TestFuzzAgainstStrconv 是翻译到机器码之前的那道测试：十万个随机双精度，每一个都要
// ①抄回同一个数、②带上那个小数点、③有效数字与 strconv 的最短写法一位不差。
// 三条合在一起才是"最短往返"，分开验证都无法发现"看着对"的那类错。
func TestFuzzAgainstStrconv(t *testing.T) {
	for _, v := range sampleValues(100000) {
		got := rtfloat.Shortest(v)
		back, err := strconv.ParseFloat(got, 64)
		if err != nil {
			t.Fatalf("%v 印成 %q，读不回来：%v", v, got, err)
		}
		if math.Float64bits(back) != math.Float64bits(v) {
			t.Fatalf("%v（%x）印成 %q，抄回来是 %v（%x）", v, math.Float64bits(v), got, back, math.Float64bits(back))
		}
		if !strings.Contains(got, ".") && !strings.ContainsAny(got, "a-zA-Z") {
			t.Fatalf("%v 印成 %q：少了那个小数点", v, got)
		}
		want := strconv.FormatFloat(v, 'g', -1, 64)
		if digitsOf(got) != digitsOf(want) {
			t.Fatalf("%v：得到 %q（%s），参照是 %q（%s）", v, got, digitsOf(got), want, digitsOf(want))
		}
	}
}

// TestIsShortest 自证"最短"：少一位有效数字，就必须抄不回同一个数。
// 这一条比上一条更强——参照本身也可能偏宽，只有"再短就回不来"才是定义。
func TestIsShortest(t *testing.T) {
	for _, v := range sampleValues(10000) {
		n := len(digitsOf(rtfloat.Shortest(v)))
		for k := 1; k < n; k++ {
			s := strconv.FormatFloat(v, 'g', k, 64)
			if back, err := strconv.ParseFloat(s, 64); err == nil && math.Float64bits(back) == math.Float64bits(v) {
				t.Fatalf("%v：%d 位有效数字（%q）已经能抄回来了，却还是印了 %d 位", v, k, s, n)
			}
		}
	}
}

// sampleValues 造一批双精度：随机尾数配上覆盖整个值域的指数，
// 再加入边界值。指数用 Ldexp 构造（2 的幂是精确的），这样得到的数都是精确的 double。
func sampleValues(n int) []float64 {
	r := rand.New(rand.NewSource(20260930))
	out := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.5, 3.0, 0.1 + 0.2, 1.0 / 3.0,
		math.MaxFloat64, math.SmallestNonzeroFloat64, math.MaxFloat32, float64(int64(1) << 53),
		1e21, 1e22, 1e23, 5e-324, 1e-7, 1.5e-7, 2.2250738585072011e-308,
		1234567890123456789, -0.000001, 1e100, 1e-100, math.Pi, math.E,
	}
	for len(out) < n {
		m := r.Uint64() & ((uint64(1) << 53) - 1)
		exp := r.Intn(2100) - 1075
		v := math.Ldexp(float64(m), exp)
		if r.Intn(2) == 0 {
			v = -v
		}
		if math.IsInf(v, 0) || math.IsNaN(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

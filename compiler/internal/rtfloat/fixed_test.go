// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package rtfloat_test

import (
	"fmt"
	"math"
	"testing"

	"neflang.org/nf/internal/rtfloat"
)

// 定长那一版是 @put_float 的图纸：它必须与 math/big 那一版逐字一致，
// 而且专挑最难的地方撞——2 的整数次幂（它上下邻居的间隔不一样宽）、次正规、
// 每个 binade 减一个位置的那一个。这些地方对了，机器码照抄就是对的。

// TestFixedMatchesBigReference 是"蓝本与参考实现逐值一致"这条测试。
func TestFixedMatchesBigReference(t *testing.T) {
	bad := 0
	for _, v := range sampleValues(100000) {
		want := rtfloat.Shortest(v)
		got, err := tryFixed(v)
		if err != nil {
			t.Fatalf("%v（%x）：定长版炸了：%v", v, math.Float64bits(v), err)
		}
		if got != want {
			bad++
			if bad <= 10 {
				t.Errorf("%v（%x）：定长版给 %q，参考版给 %q", v, math.Float64bits(v), got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("定长版与参考版不一致的有 %d 个", bad)
	}
}

// TestFixedOnEveryPowerOfTwo 把每一个 2 的幂连同它上下各两个槽都撞一遍：
// 这些位置摊成十进制最长、邻居间隔又与别处不同，最短往返的边角几乎全挤在这里。
func TestFixedOnEveryPowerOfTwo(t *testing.T) {
	n := 0
	for e := -1074; e <= 1023; e++ {
		v := math.Ldexp(1, e)
		if math.IsInf(v, 0) || v == 0 {
			continue
		}
		for _, d := range []int64{-2, -1, 0, 1, 2} {
			x := math.Float64frombits(math.Float64bits(v) + uint64(d))
			if math.IsInf(x, 0) || math.IsNaN(x) {
				continue
			}
			want, got := rtfloat.Shortest(x), mustFixed(t, x)
			if got != want {
				t.Fatalf("%v（%x）：定长版 %q，参考版 %q", x, math.Float64bits(x), got, want)
			}
			if back := mustParse(t, got); back != x {
				t.Fatalf("%v（%x）印成 %q，抄回来是 %v", x, math.Float64bits(x), got, back)
			}
			n++
		}
	}
	// 每个 binade 减一个位置的那一个：尾数全 1
	for e := -1070; e <= 1022; e++ {
		v := math.Nextafter(math.Ldexp(1, e), 0)
		if v == 0 || math.IsNaN(v) {
			continue
		}
		if got, want := mustFixed(t, v), rtfloat.Shortest(v); got != want {
			t.Fatalf("2^%d 减一格：定长版 %q，参考版 %q", e, got, want)
		}
	}
	if n < 10000 {
		t.Fatalf("这一轮才撞了 %d 个值，不像话", n)
	}
}

// TestFixedOnSubnormals 单独走一遍次正规区：那一片没有隐式那一位，
// 邻居间隔与正规区不同，最容易把"平手取偶"那条规则算歪。
func TestFixedOnSubnormals(t *testing.T) {
	for i := uint64(1); i < 1<<12; i++ {
		v := math.Float64frombits(i)
		if got, want := mustFixed(t, v), rtfloat.Shortest(v); got != want {
			t.Fatalf("%v（%x）：定长版 %q，参考版 %q", v, i, got, want)
		}
	}
	for i := uint64(1) << 50; i < (uint64(1)<<50)+(1<<12); i++ {
		v := math.Float64frombits(i)
		if got, want := mustFixed(t, v), rtfloat.Shortest(v); got != want {
			t.Fatalf("%v（%x）：定长版 %q，参考版 %q", v, i, got, want)
		}
	}
}

// tryFixed 把定长版的 panic 接住并报出是哪个值——宽度算小了这种事必须点名，
// 不然测试只说"炸了"，人还得自己猜是哪一步撑爆。
func tryFixed(v float64) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return rtfloat.ShortestFixed(v), nil
}

func mustFixed(t *testing.T, v float64) string {
	t.Helper()
	s, err := tryFixed(v)
	if err != nil {
		t.Fatalf("%v（%x）：定长版炸了：%v", v, math.Float64bits(v), err)
	}
	return s
}

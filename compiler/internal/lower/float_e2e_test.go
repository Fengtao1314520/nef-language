// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/rtfloat"
)

// TestPrintFloatMatchesReference 把机器码实现与 Go 的参考实现逐值逐字符比对：
// 打印出的串必须完全一致。"照抄回去还是同一个数"这条规则违反时不报编译错，
// 只会默默多一位或少一位，所以这条测试覆盖次正规、2 的整数次幂、上下端点这些角落。
func TestPrintFloatMatchesReference(t *testing.T) {
	var one, zero, v01, v02 = 1.0, 0.0, 0.1, 0.2
	vals := []float64{
		0, math.Copysign(0, -1), one, -one, 0.5, -0.5, 3, 100, 1e6,
		v01, v02, 0.3, v01 + v02, one / 3, 3.141592653589793, 2.718281828459045,
		123456789.123456789, 1e22, 1e23, 9007199254740992.0, 9007199254740994.0,
		math.MaxFloat64, math.SmallestNonzeroFloat64, math.MaxFloat64 / 2, math.Sqrt(2),
		one / zero, -one / zero, zero / zero,
	}
	// 每一个 2 的整数次幂，外加它上下各一个 ulp：半个 ulp 那条分支与平手取偶都藏在这儿
	for _, e := range []int{-1074, -1073, -1023, -1022, -1021, -1000, -750, -500, -324,
		-309, -308, -200, -100, -53, -52, -27, -4, -1, 0, 1, 2, 3, 21, 22, 23, 60,
		100, 500, 971, 1022, 1023} {
		v := math.Ldexp(1, e)
		vals = append(vals, v, math.Nextafter(v, math.Inf(1)), math.Nextafter(v, math.Inf(-1)))
	}
	// 再补一批随机值：最短表示最容易在"位数刚好够"的角落出错，规律值覆盖不到
	rnd := rand.New(rand.NewSource(20260930))
	for i := 0; i < 400; i++ {
		v := math.Float64frombits(rnd.Uint64())
		if math.IsNaN(v) {
			continue
		}
		vals = append(vals, v)
	}

	var src, want strings.Builder
	texts := make([]string, len(vals))
	for i, v := range vals {
		texts[i] = literal(v)
		src.WriteString("console.print(" + texts[i] + ")\n")
		want.WriteString(rtfloat.Shortest(v) + "\n")
	}
	got, code := run(t, src.String())
	if code != 0 {
		t.Fatalf("退出码 %d，要 0", code)
	}
	gots := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	wants := strings.Split(strings.TrimSuffix(want.String(), "\n"), "\n")
	if len(gots) != len(wants) {
		t.Fatalf("印了 %d 行，要 %d 行：\n%s", len(gots), len(wants), got)
	}
	bad := 0
	for i, g := range gots {
		if g != wants[i] {
			bad++
			if bad <= 12 {
				t.Errorf("%s（%v）：机器码给 %q，参考版给 %q",
					texts[i], vals[i], trim(g), trim(wants[i]))
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d／%d 个值不一致", bad, len(gots))
	}
}

// TestFloatCompareMatchesIEEE 验证小数比较规则：六个比较全按 IEEE 走，
// NaN 参与大小比较结果一律为假、NaN != 任何东西为真、-0.0 == 0.0。
// 这条测试的由来：曾有一次 ucomisd 编码出错，只有打印可用、全部比较成为非法指令，
// 而当时没有任何测试抓到，所以这里把六个算符 × 一批角落值全铺一遍。
func TestFloatCompareMatchesIEEE(t *testing.T) {
	inf, negInf, nan := math.Inf(1), math.Inf(-1), math.NaN()
	vals := []float64{0, math.Copysign(0, -1), 1, -1, 0.5, -0.5, 2, -2, inf, negInf, nan}
	ops := []string{"<", "<=", ">", ">=", "==", "!="}

	var src strings.Builder
	var want []string
	for _, x := range vals {
		for _, y := range vals {
			for _, op := range ops {
				a, b := literal(x), literal(y)
				src.WriteString("console.print(" + a + " " + op + " " + b + ")\n")
				want = append(want, ieee(x, y, op))
			}
		}
	}
	got, code := run(t, src.String())
	if code != 0 {
		t.Fatalf("退出码 %d，要 0", code)
	}
	gots := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(gots) != len(want) {
		t.Fatalf("印了 %d 行，要 %d 行", len(gots), len(want))
	}
	bad := 0
	for i, g := range gots {
		if g != want[i] {
			bad++
			if bad <= 12 {
				t.Errorf("%s（第 %d 行）：机器码给 %q，IEEE 给 %q", opAt(ops, i), i+1, g, want[i])
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d／%d 个比较对不上 IEEE", bad, len(gots))
	}
}

// ieee 是 Go 侧的参照：Go 的 float64 比较本身按 IEEE 走，NaN 的行为也一致。
func ieee(x, y float64, op string) string {
	var r bool
	switch op {
	case "<":
		r = x < y
	case "<=":
		r = x <= y
	case ">":
		r = x > y
	case ">=":
		r = x >= y
	case "==":
		r = x == y
	case "!=":
		r = x != y
	}
	return strconv.FormatBool(r)
}

// opAt 在失败时按行号定位算符：六个算符轮流出现，行号取模换算回去。
func opAt(ops []string, i int) string { return ops[i%len(ops)] }

// literal 把双精度写成 Nef 可识别的字面量：只有"数字.数字"一种形式，没有指数写法。
// 无穷与 NaN 写不出字面量，用除法构造——语言规则是这条除法不报错，按 IEEE 语义走。
func literal(v float64) string {
	switch {
	case math.IsNaN(v):
		return "0.0 / 0.0"
	case math.IsInf(v, 1):
		return "1.0 / 0.0"
	case math.IsInf(v, -1):
		return "-1.0 / 0.0"
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		return s + ".0"
	}
	if strings.HasSuffix(s, ".") {
		return s + "0"
	}
	return s
}

// trim 报错时只展示首尾各几位：次规格式字符串有三百多个零，完全铺开会淹没真正的差别。
func trim(s string) string {
	if len(s) <= 40 {
		return s
	}
	return s[:20] + "…" + s[len(s)-19:]
}

// TestFloatModulo 验证小数的 `%` 规则：按 fmod 走，符号跟随被除数，除数为 0.0 不抛出、得 NaN。
// Go 的 math.Mod 就是 fmod，拿它当参照，正负四对角落加一批随机值。
//
// 特意把"商极大"那几对摆在里面：后端那一版是拆档精确减，不靠 x - trunc(x/y)*y，
// 1e300 %% 7.0 这种一算就丢精度的写法也要跟 fmod 一模一样。
func TestFloatModulo(t *testing.T) {
	pairs := [][2]float64{
		{7.5, 2.0}, {-7.5, 2.0}, {7.5, -2.0}, {-7.5, -2.0},
		{5.0, 2.5}, {1.0, 3.0}, {2.0, 2.0}, {0.0, 3.0},
		{7.5, 0.0}, {-7.5, 0.0}, {0.0, 0.0},
		{math.Inf(1), 2.0}, {0.1, 0.3}, {1023.75, 3.0}, {-8.5, 4.0},
		{1e300, 7.0}, {math.MaxFloat64, 1e-300}, {-1e-300, 3.0},
		{math.Copysign(0, -1), 2.0}, {2.0, math.Inf(1)},
	}
	rnd := rand.New(rand.NewSource(20260930))
	for i := 0; i < 60; i++ {
		a := (float64(rnd.Int63n(1<<30)) / 1024.0) * float64(rnd.Intn(2)*2-1)
		b := float64(rnd.Int63n(1<<20)) / 4096.0
		if b == 0 {
			continue
		}
		pairs = append(pairs, [2]float64{a, b})
	}

	var src, want strings.Builder
	for _, p := range pairs {
		src.WriteString("console.print((" + literal(p[0]) + ") % (" + literal(p[1]) + "))\n")
		want.WriteString(rtfloat.Shortest(math.Mod(p[0], p[1])) + "\n")
	}
	got, code := run(t, src.String())
	if code != 0 {
		t.Fatalf("退出码 %d，要 0", code)
	}
	gots := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	wants := strings.Split(strings.TrimSuffix(want.String(), "\n"), "\n")
	if len(gots) != len(wants) {
		t.Fatalf("印了 %d 行，要 %d 行：\n%s", len(gots), len(wants), got)
	}
	for i, g := range gots {
		if g != wants[i] {
			t.Errorf("第 %d 对 %v %% %v：机器码给 %q，Go 的 fmod 给 %q",
				i+1, pairs[i][0], pairs[i][1], trim(g), trim(wants[i]))
		}
	}
}

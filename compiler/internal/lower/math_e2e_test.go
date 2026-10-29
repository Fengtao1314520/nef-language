// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestMathGroupRun 覆盖 absolute_value／floor／ceiling／round／minimum／maximum／power 这一组。
// 期望串来自实际运行；取整那几行重点验证语言定下的两条角落：
// `.5` 一律往数轴正方向进（round(-2.5) 是 -2 不是 -3），以及负数一侧 floor 与向零截断分离。
func TestMathGroupRun(t *testing.T) {
	got, code := run(t, `console.print(math.absolute_value(-7), math.absolute_value(-7.5), math.absolute_value(7))
let x:float = math.absolute_value(-3.25)
console.print(x)
console.print(math.floor(2.9), math.ceiling(2.1), math.round(2.5))
console.print(math.floor(-2.1), math.ceiling(-2.9), math.round(-2.5), math.round(-2.6))
console.print(math.floor(-3.0), math.ceiling(-3.0), math.round(-0.5), math.round(0.4))
console.print(math.floor(-0.0), math.ceiling(-0.0), math.floor(0.0))
console.print(math.minimum(3, 7), math.maximum(3.5, 1.25), math.minimum(-1, -5), math.maximum(0, 0))
console.print(math.power(2, 10), math.power(0, 0), math.power(-2, 3), math.power(5, 1))
console.print(math.power(2, 62), math.power(3, 20), math.power(10, 18), math.power(1, 1000))
let b:byte = 200
console.print(math.absolute_value(b))
let f:float = 2.0
console.print(math.minimum(f, 5.0), math.maximum(f, 5.0))
`)
	want := "7 7.5 7\n3.25\n2 3 3\n-3 -2 -2 -3\n-3 -3 0 0\n0 0 0\n" +
		"3 3.5 -5 0\n1024 1 -8 5\n4611686018427387904 3486784401 1000000000000000000 1\n200\n2.0 5.0\n"
	if got != want || code != 0 {
		t.Fatalf("math 那七条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestMathGroupThrows 固定溢出与值域几条规则：错误码与其他内建检查同一套。
func TestMathGroupThrows(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		// absolute_value：最小的那个负数取反还是它自己
		{"absolute_value 下界", "let low:int = 0 - 9223372036854775807 - 1\nconsole.print(math.absolute_value(low))\n", "出错了（10021）"},
		// power：底与指数都是正数，结果摆不进 int
		{"power 超界", "console.print(math.power(2, 63))\n", "出错了（10021）"},
		// power：负指数抛 10012 号
		{"power 负指数", "console.print(math.power(2, 0 - 3))\n", "出错了（10012）：指数不能是负的"},
		// 取整：装不进 int 的小数，含 NaN 与 ±Inf（位型都大过 2^63 那个上限）
		{"floor 超界", "console.print(math.floor(10000000000000000000000.0))\n", "出错了（10013）"},
		{"floor 是 NaN", "console.print(math.floor(0.0 / 0.0))\n", "出错了（10013）"},
		{"ceiling 是 Inf", "console.print(math.ceiling(1.0 / 0.0))\n", "出错了（10013）"},
	}
	for _, one := range cases {
		got, c := run(t, one.src)
		if c != 1 || !strings.Contains(got, one.want) {
			t.Fatalf("%s：got %q 码 %d，要含 %q 码 1", one.name, got, c, one.want)
		}
	}
	// -2^63 恰好是 int 的下界，floor 返回它自己，不越界
	got, c := run(t, "console.print(math.floor(-9223372036854775808.0))\n")
	if c != 0 || got != "-9223372036854775808\n" {
		t.Fatalf("floor 到 int 的下界不该抛：got %q 码 %d", got, c)
	}
}

// TestMathGroupRefuses 固定三类编译期拒绝：类型不一致、非数字的参数种类、以及此前未实现的那条。
func TestMathGroupRefuses(t *testing.T) {
	// minimum／maximum 两侧必须同型：混着写不会自动转换
	if err := compileErr("console.print(math.minimum(3, 3.5))\n"); err == nil ||
		!strings.Contains(err.Error(), "必须同类型") {
		t.Fatalf("minimum 两边不同型该拦：err=%v", err)
	}
	// 布尔、码点、enum 取绝对值没有意义
	if err := compileErr("console.print(math.absolute_value(true))\n"); err == nil ||
		!strings.Contains(err.Error(), "收 int、float 或 byte") {
		t.Fatalf("absolute_value 收布尔该拦：err=%v", err)
	}
	// floor／ceiling／round 只收小数：传整数进来不会自动补小数点
	if err := compileErr("console.print(math.floor(3))\n"); err == nil ||
		!strings.Contains(err.Error(), "这里要的是 float") {
		t.Fatalf("floor 收整数该拦：err=%v", err)
	}
	// random 已实现（种子与空范围的规则都已定）：整型两端通过检查，文本类型仍被拒绝
	if err := compileErr("console.print(math.random(1, 10))\n"); err != nil {
		t.Fatalf("random 现在该过检查：err=%v", err)
	}
	if err := compileErr("console.print(math.random(\"1\", 10))\n"); err == nil ||
		!strings.Contains(err.Error(), "这里要的是 int") {
		t.Fatalf("random 收文本该拦：err=%v", err)
	}
}

// TestRandomRun 验证 `math.random(low, high)` 的区间规则：取到起、取不到止。
//
// 只有"范围内只有一个值"能把输出写死（`random(7, 8)` 只能是 7），所以它们排在首行；
// 其余断言只能验证"是否落在区间内"与"两次是否不同"——种子取系统的高精度计数器，
// 每次运行结果不同，写死任何中间值都会造成依赖环境的不稳定失败（`time.now()` 同一处理）。
func TestRandomRun(t *testing.T) {
	got, code := run(t, `console.print(math.random(7, 8), math.random(-3, -2), math.random(0, 1))
let ok:boolean = true
let spread:boolean = false
let seen:int = math.random(0, 1000000)
for (i:int in 0 until 200) {
    let v:int = math.random(0, 1000000)
    if (v < 0 || v >= 1000000) {
        ok = false
    }
    if (v != seen) {
        spread = true
    }
}
let around:boolean = true
for (i:int in 0 until 60) {
    let w:int = math.random(-5, 5)
    if (w < -5 || w >= 5) {
        around = false
    }
}
console.print(ok, spread, around)
`)
	want := "7 -3 0\ntrue true true\n"
	if got != want || code != 0 {
		t.Fatalf("math.random 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestRandomEmptyRangeThrows 验证空范围规则：上界取不到，所以 `起 == 止` 也是空范围；
// 范围反写、跨过零往回写，三种都报数值组的兜底 10020 号。
func TestRandomEmptyRangeThrows(t *testing.T) {
	for _, src := range []string{
		"console.print(math.random(5, 5))\n",
		"console.print(math.random(6, 5))\n",
		"console.print(math.random(0, -1))\n",
	} {
		got, code := run(t, src)
		if code != 1 || !strings.Contains(got, "出错了（10020）") || !strings.Contains(got, "范围是空的") {
			t.Fatalf("%s 该报 10020 号那一句：got %q 码 %d", src, got, code)
		}
	}
}

// TestRandomCatchSees20 是同一规则的另一面：random 无法算出结果的那种失败是可被接住的异常。
func TestRandomCatchSees20(t *testing.T) {
	got, code := run(t, `let lo:int = 3
try {
    console.print(math.random(lo, lo))
} catch (c, msg) {
    console.print(c, length(msg) > 0)
}
`)
	want := "10020 true\n"
	if got != want || code != 0 {
		t.Fatalf("接住的该是 10020 号：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

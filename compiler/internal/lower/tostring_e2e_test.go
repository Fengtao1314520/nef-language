// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"math"
	"strings"
	"testing"

	"neflang.org/nf/internal/rtfloat"
)

// TestFloatToStringMatchesPrint 固定最容易做错的对应关系：`number.to_string(x)`
// 与 `console.print(x)` 的输出必须逐字相同。两者共用同一算法（putFloat 的两个去向），
// 差别只在收尾：一个写 stdout，一个把同一串字节包成文本对象。
//
// 因此这里不写死期望串，而是三重互查：to_string 的行 == print 的行，
// 两行都要 == Go 侧逐值核对过的 rtfloat.Shortest。特殊值（±0、±Inf、NaN）也在用例里；
// 它们走 fixedOut 那条独立出口，是最容易只改一侧的分叉。
func TestFloatToStringMatchesPrint(t *testing.T) {
	var one, zero = 1.0, 0.0
	vals := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.5, 0.1 + 0.2, one / 3, 1e22, 1e23,
		9007199254740992.0, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Sqrt(2),
		one / zero, -one / zero, zero / zero,
	}
	var src strings.Builder
	for _, v := range vals {
		lit := literal(v)
		src.WriteString("console.print(number.to_string(" + lit + "))\n")
		src.WriteString("console.print(" + lit + ")\n")
	}
	got, code := run(t, src.String())
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, got)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2*len(vals) {
		t.Fatalf("印了 %d 行，要 %d 行：\n%s", len(lines), 2*len(vals), got)
	}
	for i, v := range vals {
		asText, asPrint := lines[2*i], lines[2*i+1]
		ref := rtfloat.Shortest(v)
		if asText != ref {
			t.Errorf("第 %d 个值 to_string 与参考实现对不上：got %s want %s", i, trim(asText), trim(ref))
		}
		if asText != asPrint {
			t.Errorf("第 %d 个值两条路不一样：to_string=%s print=%s", i, trim(asText), trim(asPrint))
		}
	}
}

// TestNumberToStringBasicTypes 覆盖其余各类型的转文本：整数（含 int 最小值）、
// byte、布尔、码点（文本的 length 计的是码点）、enum。
func TestNumberToStringBasicTypes(t *testing.T) {
	got, code := run(t, `let n:int = 42
console.print(number.to_string(n) + "，" + number.to_string(-7))
console.print(number.to_string(0), number.to_string(-1))
let b:byte = 200
console.print(number.to_string(b))
console.print(number.to_string(true), number.to_string(false))
console.print(number.to_string('中'), length(number.to_string('中')))
console.print(number.to_string('A'), length(number.to_string('A')))
enum Level { low, mid, high }
console.print(number.to_string(Level.mid))
console.print(length(number.to_string(9223372036854775807)))
`)
	want := "42，-7\n0 -1\n200\ntrue false\n中 1\nA 1\n1\n19\n"
	if got != want || code != 0 {
		t.Fatalf("number.to_string 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestToStringResultIsAString 验证返回值是真正的文本：能拼接、能切分、能按下标取码点，
// 也能赋给一个名字继续使用——不是只够打印一行的临时缓冲。
func TestToStringResultIsAString(t *testing.T) {
	got, code := run(t, `let s:string = number.to_string(12024)
console.print(s + "/" + s)
let parts:list<string> = s.split("2")
console.print(length(parts), parts[0], parts[1], parts[2])
console.print(s[1], length(s))
let t:string = s        // 文本是引用类型：交出去之后 s 就作废了
console.print(t, length(t))
`)
	want := "12024/12024\n3 1 0 4\n2 5\n12024 5\n"
	if got != want || code != 0 {
		t.Fatalf("to_string 的结果当文本用不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 返回值是真正的文本，不是临时缓冲：交出那一步同样要按规则拒绝
	if err := compileErr("let s:string = number.to_string(1)\nlet t:string = s\nconsole.print(s)\n"); err == nil ||
		!strings.Contains(err.Error(), "已经交出去") {
		t.Fatalf("to_string 的结果交出后该拦下原名：err=%v", err)
	}
}

// TestToStringNamesWhatItCannotDo 固定：文本进 `number.to_string` 由**检查**拒绝，
// 不再进"后端尚未实现"那一组——此前是检查放行、发码那一步才报错，一条语言规则被说成了实现缺口。
// 容器、盒子、`struct`、`class` 都已实现，见 print_e2e_test.go；int、byte、enum、boolean、
// unicode、float 这一批各分支都接受，逐种实测过，没有落到发码那句护栏上。
func TestToStringNamesWhatItCannotDo(t *testing.T) {
	err := compileErr(`console.print(number.to_string("甲"))` + "\n")
	if err == nil || !strings.Contains(err.Error(), "不接 string") {
		t.Fatalf("文本进 to_string 该点名：err=%v", err)
	}
	if strings.Contains(err.Error(), "后端还没做到这些") {
		t.Fatalf("这一条已改成前端规则，不该再进后端那一组：%v", err)
	}
}

// TestStructToString 覆盖"按登记的形状打印"：`struct` 返回 `Name { 字段: 值, … }`，
// 形状与字面量所用的一致，不引入新的记号。
//
// 三个重点：嵌套的 struct 要递归拼进去；字段里的小数与布尔值仍走与 `console.print` 相同的分支
// （同一个 @float_str／@bool_str，不会内外两套写法）；文本字段带引号——否则空文本没有可见形态。
func TestStructToString(t *testing.T) {
	got, code := run(t, `struct Point { x:int
  y:float
  tag:string
  ok:boolean }
struct Line { a:Point
  b:Point }

let p:Point = Point { x: 3, y: 1.5, tag: "起点", ok: true }
console.print(number.to_string(p))
let l:Line = Line { a: p, b: Point { x: 7, y: 0.0, tag: "", ok: false } }
console.print(number.to_string(l))
let back:string = number.to_string(p)
console.print(back.contains("x: 3"), back.contains("1.5"), length(back) > 0)
`)
	want := "Point { x: 3, y: 1.5, tag: \"起点\", ok: true }\n" +
		"Line { a: Point { x: 3, y: 1.5, tag: \"起点\", ok: true }, b: Point { x: 7, y: 0.0, tag: \"\", ok: false } }\n" +
		"true true true\n"
	if got != want || code != 0 {
		t.Fatalf("struct 的 to_string 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestClassToString 覆盖 `class` 这一半，重点是默认值：`new Hold()` 里的 `struct` 字段与
// 文本字段都是空句柄，拼接前必须先判空——判空包住整个"读字段"段，
// 不是读完再判（空句柄读字段等于拿 0 加偏移）。
func TestClassToString(t *testing.T) {
	got, code := run(t, `struct Point { x:int
  y:int }
class Hold { p:Point
  n:int
  label:string }

let h:Hold = new Hold()
console.print(number.to_string(h))
h.p = Point { x: 1, y: 2 }
h.label = "带上了"
console.print(number.to_string(h))
h.n = 7
console.print(number.to_string(h))
`)
	want := "Hold { p: null, n: 0, label: null }\n" +
		"Hold { p: Point { x: 1, y: 2 }, n: 0, label: \"带上了\" }\n" +
		"Hold { p: Point { x: 1, y: 2 }, n: 7, label: \"带上了\" }\n"
	if got != want || code != 0 {
		t.Fatalf("class 的 to_string 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 同一个槽空句柄，直接读它就是 10092 号——与 `null.x` 同一条，不是 to_string 另立规矩
	g2, c2 := run(t, "struct Point { x:int\n  y:int }\nclass Hold { p:Point\n  n:int }\nlet h:Hold = new Hold()\nconsole.print(h.p.x)\n")
	if c2 != 1 || !strings.Contains(g2, "出错了（10092）") {
		t.Fatalf("空句柄读字段该 10092 号：got %q 码 %d", g2, c2)
	}
}

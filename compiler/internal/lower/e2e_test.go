// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/exetest"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

// build 走完整条链：词法 → 语法 → 静态检查 → 中间码 → 机器码 → PE。
// 任何一步失败都打印中间产物，以免失败时只剩一句"跑不出来"。
func build(t *testing.T, src string) []byte {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: "t.nf"}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("前端：%v", errs)
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		t.Fatalf("降级：%v", err)
	}
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		t.Fatalf("发射：%v\n%s", err, ir.Dump(p))
	}
	exe, err := l.Build(d.User, entry)
	if err != nil {
		t.Fatalf("装配：%v\n%s", err, ir.Dump(p))
	}
	return exe
}

func run(t *testing.T, src string) (string, int) {
	t.Helper()
	r := exetest.Run(t, build(t, src), "t.exe")
	return r.Stdout, r.Code
}

func TestPrintArithmetic(t *testing.T) {
	got, code := run(t, "console.print(1 + 2 * 3)\n")
	if got != "7\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 \"7\\n\" 码 0", got, code)
	}
}

func TestPrintSeveralValues(t *testing.T) {
	got, _ := run(t, "console.print(1, 2, 3)\nconsole.print()\n")
	if got != "1 2 3\n\n" {
		t.Fatalf("得到 %q，要 \"1 2 3\\n\\n\"", got)
	}
}

func TestLetAndLoop(t *testing.T) {
	got, _ := run(t, `
let total:int = 0
for (i:int in 0 until 10) {
    total = total + i
}
console.print(total)
`)
	if got != "45\n" {
		t.Fatalf("得到 %q，要 \"45\\n\"", got)
	}
}

func TestRecursion(t *testing.T) {
	got, _ := run(t, `
method fib(n:int):int {
    if (n < 2) {
        return n
    }
    return fib(n - 1) + fib(n - 2)
}
console.print(fib(10), fib(1))
`)
	if got != "55 1\n" {
		t.Fatalf("得到 %q，要 \"55 1\\n\"", got)
	}
}

func TestStringsAndChars(t *testing.T) {
	got, _ := run(t, `
let name:string = "阿明"
console.print(name, '中', true, false)
`)
	if got != "阿明 中 true false\n" {
		t.Fatalf("得到 %q", got)
	}
}

func TestComparisonsAndLogic(t *testing.T) {
	got, _ := run(t, `
let n:int = 7
if (n > 5 && n < 10) {
    console.print("在区间里")
}
if (n == 1 || n == 7) {
    console.print(n != 7)
}
`)
	if got != "在区间里\nfalse\n" {
		t.Fatalf("得到 %q", got)
	}
}

func TestExitCode(t *testing.T) {
	_, code := run(t, "exit (100, \"要给我一个路径\")\n")
	if code != 100 {
		t.Fatalf("退出码 %d，要 100", code)
	}
}

// 溢出与除零必须当场抛出，形状与手写 throw 完全相同。
func TestIntOverflowThrows(t *testing.T) {
	got, code := run(t, "console.print(9223372036854775807 + 1)\n")
	if code != 1 || !strings.Contains(got, "出错了（10021）") {
		t.Fatalf("得到 %q 码 %d，要印「出错了（10021）」并以 1 收场", got, code)
	}
}

func TestDivZeroThrows(t *testing.T) {
	got, code := run(t, `
let d:int = 0
console.print(6 / d)
`)
	if code != 1 || !strings.Contains(got, "出错了（10022）") {
		t.Fatalf("得到 %q 码 %d，要印「出错了（10022）」并以 1 收场", got, code)
	}
}

// 最小值除以 -1 在 CPU 上是硬件异常，必须被捕获并转成 10021 号。
func TestMinDivMinusOneThrows(t *testing.T) {
	got, code := run(t, `
let a:int = -9223372036854775807 - 1
let b:int = 0 - 1
console.print(a / b)
`)
	if code != 1 || !strings.Contains(got, "出错了（10021）") {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// 不支持的写法要当场点明是哪一条，不许悄悄发出少做检查的码。
// 样本随实现进度更换：list、dict、switch、struct、`++`／`--`、参数默认值、number.to_string、
// 文本动作那一组、算数组（含 `math.random`）、时钟那两条、path 与 environment 两组、
// 往外写的那四条、regular_expression 组的基本款、整只容器与盒子的打印法
// （见 print_e2e_test.go）都已实现；剩的是它的完整语法——这一版只做子集，所以样本换成它。
// 式子是运行期的值，编译期拒绝不了，接住它的是 10071 号那句点名的报错。
func TestUnsupportedSaysSo(t *testing.T) {
	got, code := run(t, "console.print(regular_expression.match(\"a\", "+nfLit("(?=a)")+"))\n")
	if code != 1 || !strings.Contains(got, "出错了（10071）") || !strings.Contains(got, "先行断言") {
		t.Fatalf("没做的那一条该点名，实际 got %q 码 %d", got, code)
	}
	// 容器的打印法已实现：同一写法现在要实际运行得出数值
	got, code = run(t, "let items:list<string> = [\"甲\"]\nconsole.print(items, number.to_string(items))\n")
	if got != "[\"甲\"] [\"甲\"]\n" || code != 0 {
		t.Fatalf("整只容器的印法没跑对：got %q 码 %d", got, code)
	}
	got, code = run(t, "console.print(regular_expression.match(\"abc\", \"\\\\d\"), \"|\", regular_expression.match(\"a1\", \"\\\\d\"))\n")
	if got != "false | true\n" || code != 0 {
		t.Fatalf("regular_expression 没跑对：got %q 码 %d", got, code)
	}
	got, code = run(t, "console.print(math.random(7, 8))\n")
	if got != "7\n" || code != 0 {
		t.Fatalf("math.random 没跑对：got %q 码 %d", got, code)
	}
	got, code = run(t, "let i:int = 0\ni++\ni++\ni--\nconsole.print(i)\n")
	if got != "1\n" || code != 0 {
		t.Fatalf("++ 没跑对：got %q 码 %d", got, code)
	}
	got, code = run(t, "let n:int = 3\nconsole.print(number.to_string(n) + \" 件\")\n")
	if got != "3 件\n" || code != 0 {
		t.Fatalf("number.to_string 没跑对：got %q 码 %d", got, code)
	}
}

// TestClassFieldAndMethod 走一遍 class：new 给初值、读字段、方法里直接写字段名、
// 写字段、交给另一个名字，以及 `new C()` 不给初值时字段一律取零值。
func TestClassFieldAndMethod(t *testing.T) {
	got, code := run(t, `
class Counter {
    n:int
    step:int
    method bump():int {
        n = n + step
        return n
    }
}

let c:Counter = new Counter(n: 1, step: 2)
console.print(c.n, c.step)
console.print(c.bump())
console.print(c.bump())
c.step = 10
console.print(c.bump())
let d:Counter = new Counter()
console.print(d.n, d.step)
let e:Counter = c
console.print(e.n)
`)
	if got != "1 2\n3\n5\n15\n0 0\n15\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestMethodWithArgs 验证接收者与参数同时进来：rcx 是 this，rdx 与 r8 是前两条参数。
func TestMethodWithArgs(t *testing.T) {
	got, _ := run(t, `
class Adder {
    seed:int
    method add(a:int, b:int):int { return seed + a + b }
}

let x:Adder = new Adder(seed: 100)
console.print(x.add(1, 2), x.add(10, 20))
`)
	if got != "103 130\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestThisAndBareField 验证 this.字段 与直接写字段名等价。
func TestThisAndBareField(t *testing.T) {
	got, _ := run(t, `
class Box {
    w:int
    method wide():int { return this.w * 2 }
    method set(v:int) {
        this.w = v
    }
}

let b:Box = new Box(w: 21)
b.set(100)
console.print(b.wide(), b.w)
`)
	if got != "200 100\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestBoxAndUnbox 装箱与拆盒：class 的句柄本来就指向一块带类型字的堆对象，
// 盒子就是那个句柄；拆回来还是同一个对象，字段读得到。
func TestBoxAndUnbox(t *testing.T) {
	got, _ := run(t, `
class Point {
    x:int
    method hello():int { return x + 1 }
}

let p:Point = new Point(x: 41)
let o:object = p as object
let back:Point = o as Point
console.print(back.hello(), back.x)
`)
	if got != "42 41\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestBoxValueTypes 值类型的装箱要另起一块盒子：int、boolean、unicode 各走各的类型字。
func TestBoxValueTypes(t *testing.T) {
	got, _ := run(t, `
let n:int = 7
let nb:object = n as object
let ok:boolean = true
let okb:object = ok as object
let ch:unicode = '中'
let chb:object = ch as object
console.print(nb as int, okb as boolean, chb as unicode)
`)
	if got != "7 true 中\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestIsAsks 验证 `is` 只判断、不改动：类型不对也不抛出，答 false。
func TestIsAsks(t *testing.T) {
	got, _ := run(t, `
class A {
    v:int
}

class B {
    v:int
}

let a:A = new A(v: 1)
let box:object = a as object
console.print(box is A, box is B)
`)
	if got != "true false\n" {
		t.Fatalf("得到 %q", got)
	}
}

// 拆盒拆不开抛 10011 号：形状与其他运行期错误一致，打印一行、退出码 1。
func TestUnboxWrongThrows(t *testing.T) {
	got, code := run(t, `
class A {
    v:int
}

class B {
    v:int
}

let a:A = new A(v: 1)
let box:object = a as object
let b:B = box as B
console.print(b.v)
`)
	if got != "出错了（10011）：拆开的类型对不上\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// 未判空就访问成员抛 10092 号：读字段与调方法两条路都有这项检查。
func TestNullDerefThrows(t *testing.T) {
	for i, src := range []string{
		"class A {\n    v:int\n}\n\nlet a:A = null\nconsole.print(a.v)\n",
		"class A {\n    v:int\n    method get():int { return v }\n}\n\nlet a:A = null\nconsole.print(a.get())\n",
	} {
		got, code := run(t, src)
		want := "出错了（10092）：对着空句柄取成员或调用方法\n"
		if got != want || code != 1 {
			t.Fatalf("第 %d 条得到 %q 码 %d，要 %q 码 1", i, got, code, want)
		}
	}
}

// TestMethodCallsMethod 验证方法里再调用自己的方法（this.名字），以及把句柄作为参数传给其他函数。
func TestMethodCallsMethod(t *testing.T) {
	got, code := run(t, `
class Chain {
    v:int
    method double():int { return v * 2 }
    method report():int { return this.double() + this.double() }
}

method peek(c:Chain):int { return c.v }

let ch:Chain = new Chain(v: 10)
console.print(ch.report())
console.print(peek(ch))
`)
	if got != "40\n10\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// buildAt 与 build 相同，但传入一个真实文件名作入口——读样例文件时要用它自己的路径作作用域根。
func buildAt(t *testing.T, path, src string) []byte {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := parser.Parse(path, toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: path}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("前端：%v", errs)
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		t.Fatalf("降级：%v", err)
	}
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		t.Fatalf("发射：%v\n%s", err, ir.Dump(p))
	}
	exe, err := l.Build(d.User, entry)
	if err != nil {
		t.Fatalf("装配：%v\n%s", err, ir.Dump(p))
	}
	return exe
}

// TestListMoveOut 验证"读走就是交出"：读完 a[0]，那个位置之后再读返回 null。
// 再打印它就是打印一个空句柄——打印 `null`、程序不崩溃（该行为已覆盖所有类型）。
func TestListMoveOut(t *testing.T) {
	got, code := run(t, `
let a:list<string> = ["甲", "乙"]
let s:string = a[0]
console.print(s)
console.print(a[0])
`)
	if got != "甲\nnull\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestListBounds 越界读抛 10031 号：容器与文本同一条规则，当场抛出、不给零值。
func TestListBounds(t *testing.T) {
	got, code := run(t, `
let a:list<int> = [1, 2]
console.print(a[2])
`)
	if got != "出错了（10031）：下标越界\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestListMutateWhileIterating 验证：遍历途中添加一个元素，下一圈圈头抛 10033 号。
func TestListMutateWhileIterating(t *testing.T) {
	got, code := run(t, `
let a:list<int> = [1, 2]
for (v:int in a) {
    a.add(9)
}
console.print(length(a))
`)
	if got != "出错了（10033）：遍历途中改了容器\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestListIterateKeepsCells 遍历交出的是只读借用，走完一圈容器仍是满的、
// 每个槽位的元素都还在——这条与 TestListMoveOut 互为对照。
func TestListIterateKeepsCells(t *testing.T) {
	got, _ := run(t, `
let a:list<string> = ["书包", "笔记本", "雨伞"]
let n:int = 0
for (s:string in a) {
    n = n + length(s)
}
console.print(n, length(a))
console.print(a[0])
`)
	if got != "7 3\n书包\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestListRemoveByContent 文本按内容删除（判等比内容），删除之后后面的元素前移；
// 目标不存在时原样返回，不抛出。
func TestListRemoveByContent(t *testing.T) {
	got, _ := run(t, `
let a:list<string> = ["书包", "笔记本", "雨伞"]
a.remove("笔记本")
console.print(length(a), a[0], a[1])
a.remove("不存在的东西")
console.print(length(a))
`)
	if got != "2 书包 雨伞\n2\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestSampleShopping 把 samples/shopping.nf 直接编成产物运行一遍。
// 样例文件本身就是验收标准：它声称的输出与实际产物不一致时，这条测试应当失败。
func TestSampleShopping(t *testing.T) {
	const path = "../../../samples/shopping.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	src := string(raw)
	r := exetest.Run(t, buildAt(t, path, src), "shopping.exe")
	want := "共 3 件，字符数合计: 7\n算完了\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("shopping.nf跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

// TestSampleShapes 运行 shapes.nf：接口、装箱、拆盒、`is`、小数打印都在这份样例里。
// 小数打印是最后实现的一项——@put_float 实现之前它连编译都通不过。
func TestSampleShapes(t *testing.T) {
	const path = "../../../samples/shapes.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	src := string(raw)
	r := exetest.Run(t, buildAt(t, path, src), "shapes.exe")
	want := "圆: 12.56636 方: 12.0\n拆回来: 12.56636\n第 0 格是空的\n是个方，周长: 12.0\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("shapes.nf跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

// TestStrSplitAndToInt 覆盖文本这一支的三个例程：按分隔符切分（含空分隔与末尾空段）、
// 整串未命中分隔符、空文本，再把文本转成整数（正负号两种）。
func TestStrSplitAndToInt(t *testing.T) {
	got, code := run(t, `
let parts:list<string> = "a,b,c".split(",")
console.print(length(parts), parts[0], parts[2])
let none:list<string> = "abc".split(",")
console.print(length(none), none[0])
let each:list<string> = "abc".split("")
console.print(length(each), each[0], each[2])
let trail:list<string> = "a,b,".split(",")
console.print(length(trail), trail[2] == "")
let one:list<string> = "".split(",")
console.print(length(one), one[0] == "")
let cn:list<string> = "汉字".split("")
console.print(length(cn), cn[1])
console.print(text.to_int("42"), text.to_int("-7"), text.to_int("+8"), text.to_int("0"))
`)
	want := "3 a c\n1 abc\n3 a c\n3 true\n1 true\n2 字\n42 -7 8 0\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestStrToIntThrows 固定两个错误号：不是数字抛 10012 号，超出 int 的值域抛 10013 号。
// 没人接住时按运行期错误的统一行为走"退出码 1 并打印一句"。
func TestStrToIntThrows(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`console.print(text.to_int("12x"))`, "10012）：文本读不成数字"},
		{`console.print(text.to_int(""))`, "10012）：文本读不成数字"},
		{`console.print(text.to_int("-"))`, "10012）：文本读不成数字"},
		{`console.print(text.to_int(" 12"))`, "10012）：文本读不成数字"},
		{`console.print(text.to_int("9223372036854775808"))`, "10013）：超出了 int 的值域"},
		{`console.print(text.to_int("-9223372036854775809"))`, "10013）：超出了 int 的值域"},
	}
	for _, c := range cases {
		got, code := run(t, c.src+"\n")
		if code != 1 || !strings.Contains(got, c.want) {
			t.Fatalf("%s 得到 %q 码 %d，要含 %q", c.src, got, code, c.want)
		}
	}
}

// TestStrCompareContent 固定文本判等比的是内容不是句柄：切出来的片段与内容相同的字面量
// 必须算相等（"文本判等比内容"），大小按字节序比较——UTF-8 的字节序就是码点序。
// 与 null 判等仍比句柄（显式判空），不受内容判等影响。
func TestStrCompareContent(t *testing.T) {
	got, code := run(t, `
let parts:list<string> = "alpha,beta".split(",")
let a:string = parts[0]
let b:string = parts[1]
console.print(a == "alpha", b == "beta", a != b, a < b)
console.print("" < "a", "ab" < "b", "abc" < "abd", "abd" < "ab")
console.print(a == "beta", a == null, b == "beta")
`)
	want := "true true true true\ntrue true true false\nfalse false true\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestTryCatchChain 走处理器帧链：跨函数抛出、内建错与手写 throw 同一种形状、
// 接住块里再往外抛由外层接住、try 正常走完不影响后面的代码。
func TestTryCatchChain(t *testing.T) {
	got, code := run(t, `
method pick(items:list<int>, i:int):int {
    return items[i]
}

method risky(n:int) {
    if (n > 0) {
        throw (101, "数太大了")
    }
    let s:string = null
    console.print(length(s))
}

try {
    console.print("体里先印一句")
    let v:int = pick([1, 2], 9)
    console.print("这一行跑不到", v)
} catch (c, m) {
    console.print("接住:", c, m)
}

try {
    risky(1)
} catch (c1, m1) {
    console.print("手写的那一个:", c1, m1)
}

try {
    risky(0)
} catch (c2, m2) {
    console.print("空引用:", c2, m2)
}

try {
    try {
        throw (102, "里面的")
    } catch (ci, mi) {
        console.print("里层接住，再往外抛:", ci, mi)
        throw (ci, "外面这层也处理不了")
    }
} catch (co, mo) {
    console.print("外层接住:", co, mo)
}

console.print("都接住了，程序继续")
`)
	want := "体里先印一句\n接住: 10031 下标越界\n手写的那一个: 101 数太大了\n空引用: 10092 对着空句柄取成员或调用方法\n" +
		"里层接住，再往外抛: 102 里面的\n外层接住: 102 外面这层也处理不了\n都接住了，程序继续\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestTryNotThrowing try 体没有抛出：接住块一次都不进入，后面的代码照常执行。
func TestTryNotThrowing(t *testing.T) {
	got, code := run(t, `
try {
    console.print("平平稳稳")
} catch (c, m) {
    console.print("不该走到这里", c, m)
}
console.print("后面照旧")
`)
	if got != "平平稳稳\n后面照旧\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestDictBasics 覆盖 dict 这一套：字面量建表、按键取值、`d[k]=v` 是改值、
// 写一个不存在的键就是添加一条、contains 与 remove、length。
func TestDictBasics(t *testing.T) {
	got, code := run(t, `
let d:dict<string, int> = {"a": 1, "bb": 2}
console.print(length(d), d["a"])
d["cc"] = 3
console.print(length(d), d.contains("cc"), d.contains("zz"))
d["a"] = 10
console.print(d["a"])
d.remove("bb")
console.print(length(d), d.contains("bb"))
`)
	if got != "2 1\n3 true false\n10\n2 false\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestHashsetBasics 覆盖 hashset：与 dict 共用同一张表，值的位置空置不用。
func TestHashsetBasics(t *testing.T) {
	got, _ := run(t, `
let s:hashset<int> = {7, 8, 9}
console.print(length(s), s.contains(7), s.contains(100))
s.add(100)
s.add(7)
console.print(length(s))
s.remove(7)
console.print(length(s), s.contains(7))
`)
	if got != "3 true false\n4\n3 false\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestDictAndSetIterate 遍历 dict 只交出键、hashset 交出成员，顺序不保证，
// 所以这里只把拿到的值求和——结果与顺序无关，正对应"不保证顺序"这条规则。
func TestDictAndSetIterate(t *testing.T) {
	got, _ := run(t, `
let d:dict<string, int> = {"a": 1, "bb": 2, "ccc": 3}
let sum:int = 0
for (k:string in d) {
    sum = sum + d[k]
}
let s:hashset<int> = {1, 2, 3}
let total:int = 0
for (v:int in s) {
    total = total + v
}
console.print(sum, total)
`)
	if got != "6 6\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestDictMissingKey 读一个不存在的键抛 10032 号，写一个不存在的键是添加一条。
func TestDictMissingKey(t *testing.T) {
	got, code := run(t, `
let d:dict<string, int> = {"a": 1}
console.print(d["zz"])
`)
	if got != "出错了（10032）：没有这个键\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestDictMutateWhileIterating 验证：遍历途中 add，下一圈抛 10033 号。
func TestDictMutateWhileIterating(t *testing.T) {
	got, code := run(t, `
let d:dict<string, int> = {}
d.add("a", 1)
for (k:string in d) {
    d.add("b", 2)
}
console.print(length(d))
`)
	if got != "出错了（10033）：遍历途中改了容器\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestDictManyKeys 添加到触发多次翻倍再散列：每一个键都必须查得到，数目不能错。
func TestDictManyKeys(t *testing.T) {
	var body strings.Builder
	body.WriteString("let d:dict<int, int> = {}\n")
	var want []string
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&body, "d.add(%d, %d)\n", i, i*3)
		want = append(want, fmt.Sprintf("%d", i*3))
	}
	fmt.Fprintf(&body, "console.print(length(d))\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&body, "console.print(d[%d])\n", i)
	}
	got, _ := run(t, body.String())
	wantStr := "40\n" + strings.Join(want, "\n") + "\n"
	if got != wantStr {
		t.Fatalf("40 个键对不上：got %q", got)
	}
}

// TestSwitchShapes 覆盖 switch 的四种判定与两种收尾：几个标签共用一个分支体、default、
// 枚举把成员列全所以不必写 default、文本按内容比较。
func TestSwitchShapes(t *testing.T) {
	got, code := run(t, `
enum Door { open, closed, broken }

method describe(d:Door):string {
    switch (d) {
        Door.open { return "开着" }
        Door.closed { return "关着" }
        Door.broken { return "坏了" }
    }
}

let score:int = 82
switch (score / 10) {
    9, 10 { console.print("甲") }
    8 { console.print("乙") }
    default { console.print("还要练") }
}

let k:string = "b"
switch (k) {
    "a" { console.print(1) }
    "b", "c" { console.print(2) }
    default { console.print(0) }
}

let t:boolean = true
switch (t) {
    true { console.print("是") }
    false { console.print("否") }
}

console.print(describe(Door.closed), describe(Door.broken))
`)
	want := "乙\n2\n是\n关着 坏了\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestSwitchNoFallthrough 验证 switch 不贯穿：命中一个分支就只执行该支，
// 执行完跳出，不会连着把下面一支也执行。
func TestSwitchNoFallthrough(t *testing.T) {
	got, _ := run(t, `
let i:int = 8
switch (i) {
    8 { console.print("只印这一行") }
    9 { console.print("不该来") }
    default { console.print("也不该来") }
}
`)
	if got != "只印这一行\n" {
		t.Fatalf("得到 %q", got)
	}
}

// TestSwitchInLoop 把 switch 放进循环里，同时确认 break 跳出的是循环而不是 switch：
// 检查器只校验"break 必须在循环内"，跳转落点由这一条测试验证。
func TestSwitchInLoop(t *testing.T) {
	got, _ := run(t, `
let i:int = 0
while (i < 10) {
    switch (i) {
        3 { break }
        default { }
    }
    i = i + 1
}
console.print(i)
`)
	if got != "3\n" {
		t.Fatalf("得到 %q，要 3（break 该跳出循环）", got)
	}
}

// TestCrossFileShared 把 testdata/shared 这份两文件的小工程整体编成产物运行一遍：
// `export shared` 的 list 在主文件里添加两项、在被引文件的函数里取长度——
// 两边必须落在同一个静态槽上，否则取到的就是 0。
func TestCrossFileShared(t *testing.T) {
	r := exetest.Run(t, buildFiles(t,
		srcFile{"main.nf", "../../testdata/shared/main.nf"},
		srcFile{"registry.nf", "../../testdata/shared/registry.nf"},
	), "shared.exe")
	want := "带了: 书包\n带了: 雨伞\n一共 2 件\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("跨文件共用那一份list不对：\n got %q 码 %d\nwant %q", r.Stdout, r.Code, want)
	}
}

// TestCrossFileImportedConst 覆盖"引入的名字在函数体里也要可解析"：
// 检查器原先在查完本文件所有定义之后才登记 import 的名字，于是 `return step * 2`
// 这种写法在函数体里查不到那个 const，报出"这里要的是 int，给的是 step"。
// 登记挪到定义之前之后，这里既要通过检查，也要实际运行得出正确的数。
func TestCrossFileImportedConst(t *testing.T) {
	r := exetest.Run(t, buildFiles(t,
		srcFile{"main.nf", "../../testdata/constimport/main.nf"},
		srcFile{"dep.nf", "../../testdata/constimport/dep.nf"},
	), "constimport.exe")
	want := "14 2\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("跨文件在函数体里用引来的 const 不对：\n got %q 码 %d\nwant %q", r.Stdout, r.Code, want)
	}
}

// srcFile 是一份源文件：name 是它在 import 路径里用的名字，path 是磁盘上的位置。
type srcFile struct{ name, path string }

// buildFiles 与 build 相同，但一次编多份文件（入口排最后，其余按 import 的顺序先处理）。
func buildFiles(t *testing.T, files ...srcFile) []byte {
	t.Helper()
	var parsed []*ast.File
	for _, f := range files {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("读 %s 失败：%v", f.path, err)
		}
		toks, err := lexer.Lex(string(raw))
		if err != nil {
			t.Fatalf("%s 词法：%v", f.name, err)
		}
		file, err := parser.Parse(f.name, toks)
		if err != nil {
			t.Fatalf("%s 语法：%v", f.name, err)
		}
		parsed = append(parsed, file)
	}
	prog := &check.Program{Files: parsed, Entry: parsed[0].Path}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("前端：%v", errs)
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		t.Fatalf("降级：%v", err)
	}
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		t.Fatalf("发射：%v\n%s", err, ir.Dump(p))
	}
	exe, err := l.Build(d.User, entry)
	if err != nil {
		t.Fatalf("装配：%v\n%s", err, ir.Dump(p))
	}
	return exe
}

// TestStrConcat 验证文本拼接：拼出来的那一段是新造的对象，字节数与码点数都必须正确；
// 空文本参与拼接不能把长度算错（对象形状）。
// `+` 两侧同型是语言规矩，所以这里不把数字混进拼接。
func TestStrConcat(t *testing.T) {
	got, code := run(t, `
let a:string = "书"
let b:string = "包"
let one:string = ""
console.print(a + b, length(a + b))
console.print(one + b, length(one + b), b + one)
let twice:string = a + b + a + b
console.print(twice, length(twice))
`)
	want := "书包 2\n包 1 包\n书包书包 4\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestStrConcatNull 覆盖拼接一侧的判空：读一段文本的长度就是访问它的成员，空句柄当场抛 10092 号。
func TestStrConcatNull(t *testing.T) {
	got, code := run(t, `
let a:string = "书"
let b:string = null
console.print(a + b)
`)
	if got != "出错了（10092）：对着空句柄取成员或调用方法\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// compileOnly 只运行到降级，用于验证编译期拒绝这条路。
func compileOnly(src string) (any, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		return nil, err
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: "t.nf"}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		return nil, fmt.Errorf("前端：%v", errs)
	}
	_, err = lower.Run(prog, facts)
	return nil, err
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

// TestStructValues 覆盖 struct 的一整套：造值、字段读写、每一次交出都整份拷贝、
// 当 class 的字段、装进盒子再拆回来、放进 list 里读出来。
//
// 形状是"堆上一整块＋手里一个句柄"，所以这条测试的核心是**拷贝有没有做**：
// 少拷一次，改这一份就会影响那一份，输出立刻错位。
func TestStructValues(t *testing.T) {
	got, code := run(t, `struct Size {
    w:int
    h:int
}

method area(s:Size):int {
    return s.w * s.h
}

method twice(s:Size):Size {
    s.w = s.w * 2
    return s
}

let a:Size = Size { w: 3, h: 4 }
let b:Size = a
b.w = 10
console.print(a.w, a.h, b.w, area(a))

let big:Size = twice(a)
console.print(a.w, big.w, area(big))

class Banner {
    size:Size
    title:string
}

let bn:Banner = new Banner(size: a, title: "标题")
bn.size.h = 99
console.print(a.h, bn.size.h, length(bn.title))

let box:list<object> = []
box.add(a as object)
let one:object = box.remove_at(0)
console.print(one is Size)
let back:Size = one as Size
console.print(back.w, back.h)

let sizes:list<Size> = [a, b]
sizes[0].h = 77
console.print(sizes[0].w, sizes[0].h, a.h, length(sizes))
`)
	want := "3 4 10 12\n" + // 赋值是另一份：改 b 不动 a
		"3 6 24\n" + // 传参一份、返回一份：a 还是 3
		"4 99 2\n" + // 字段是 struct：对象里那一块与 a 各走各的
		"true\n" +
		"3 4\n" +
		"3 77 4 2\n" // list里的格各是一份，改它不动 a
	if got != want || code != 0 {
		t.Fatalf("struct 的整份拷贝不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStructNarrowPadKeepsItsBlock 钉住"块大小与字段偏移是同一套排法"。
//
// 形状是 `{boolean, int, boolean, string}`：各字段宽度之和是 18，按自然对齐排出来是
// 0、8、16、24，整块要 32。块若按 18 造（@alloc 只把尺寸垫到 8 的倍数，垫出来的不是缺的
// 那一截），最后那个字段写在自己偏 24 的位置上就冲出块尾 8 字节，正压进下一块的头——
// 那 8 字节是类型字与数据尺寸，被写成文本句柄的两半。三种症状都是这一处：整只打印要按
// 字段表读这块，读到的邻居已经毁了；整份拷贝（@copy_block 拿同一个数当长度）漏掉最后一个
// 字段；json.from_value 与打印机同一张字段表、同一条路。
//
// 这条测试用的是"看起来只是多一个 boolean"的形状，而原先的 TestStructValues 全由 8 字节
// 字段构成，宽度之和恰好等于布局，所以那一处一直没露。
func TestStructNarrowPadKeepsItsBlock(t *testing.T) {
	got, code := run(t, `struct Beat {
    visible:boolean
    count:int
    fallback:boolean
    owner:string
}

let b:Beat = Beat { visible: true, count: 3, fallback: false, owner: "nef" }
console.print(b)

let c:Beat = b
console.print(c.visible, c.count, c.fallback, c.owner)

let xs:list<Beat> = [b]
console.print(xs[0].owner, json.from_value(xs[0]).to_json())
`)
	want := "Beat { visible: true, count: 3, fallback: false, owner: \"nef\" }\n" +
		"true 3 false nef\n" +
		"nef {\"visible\":true,\"count\":3,\"fallback\":false,\"owner\":\"nef\"}\n"
	if got != want || code != 0 {
		t.Fatalf("窄字段让出的填空没算进块里：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStructLocalsDieAtTheClosingBrace 钉住"带返回值的方法走到最后一个 } 时"那一段。
//
// 这是报告里最像的那一条症状：一个 `method check(n:int):boolean` 里造一个带窄字段的
// struct、一个 list、一棵 Json，最后 `return`；进程不是在校验那几句上挂，而是在**方法要出去
// 的那一下**无声无息地没了。原因是块按"各字段宽度之和"造小了，最后那个字段的写越出块尾
// 8 字节，压进**邻块的头**（类型字与数据尺寸）；那条路走完之前没人再碰邻块，所以它一路跑
// 到 `}` 才发作——`}` 正是这个方法的局部量被拆掉、邻块第一次被还回自由链的地方。
//
// 三种收尾各钉一条：
//   - 方法返回 boolean，局部量里同时有 struct、list 与 Json（正常走到 `}`）；
//   - 方法从 while 循环里提前 return（提前收尾那条路）；
//   - 方法把 struct 变成 Json 当返回值交出去（返回值与局部量各一份）。
//
// 三条都要跑很多轮：一块坏内存什么时候被撞上取决于这一轮分配了什么，跑一轮是撞不着的。
// 这条钉的是**形状走通**（返回值、提前收尾、把树交出去都不出错）；能确定性抓住那处算错的
// 是 `TestStructNarrowPadKeepsItsBlock`——那边最后一个字段的值会当场变，判得出来。
// 堆被压坏这件事本身没有确定的观测点：这八个字节落在哪一块上、那一块什么时候被还回去，
// 取决于这一刻分配了什么，所以它表现为"偶发、无报错"。
func TestStructLocalsDieAtTheClosingBrace(t *testing.T) {
	got, code := run(t, `struct Pad {
    on:boolean
    n:int
    off:boolean
    s:string
}

method check(n:int):boolean {
    let p:Pad = Pad { on: true, n: n, off: false, s: "v" + number.to_string(n) }
    let xs:list<int> = [n, n + 1, n + 2]
    let tree:Json = json.from_value(p)
    let rendered:string = tree.to_json()
    let ok:boolean = length(rendered) > 10 && length(xs) == 3 && p.n == n && p.s == "v" + number.to_string(n)
    return ok
}

method find(target:int):int {
    let p:Pad = Pad { on: true, n: target, off: false, s: "v" }
    let xs:list<int> = [1, 2, 3]
    let tree:Json = json.from_value(p)
    let rendered:string = tree.to_json()
    let i:int = 0
    while (i < 40) {
        if (i == target) {
            return i + length(xs) + length(rendered)
        }
        i = i + 1
    }
    return -1
}

method body_of(n:int):Json {
    let p:Pad = Pad { on: true, n: n, off: false, s: "v" }
    let tree:Json = json.from_value(p)
    let box:Json = json.empty_object()
    box.set("body", tree)
    box.set("seq", n)
    return box
}

let good:int = 0
for (i:int from 0 until 300) {
    if (check(i)) {
        good = good + 1
    }
}
console.print("checked:", good)

let scans:int = 0
for (i:int from 0 until 300) {
    scans = scans + find(i % 40)
}
console.print("scanned:", scans)

let total:int = 0
for (i:int from 0 until 300) {
    let j:Json = body_of(i)
    total = total + length(j.to_json())
}
console.print("bodies:", total > 0)
`)
	want := "checked: 300\nscanned: 17870\nbodies: true\n"
	if got != want || code != 0 {
		t.Fatalf("带返回值的方法收尾这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStructRefusals 固定字段规则：装着"句柄型"字段的 struct 被拒绝——
// 整份拷贝拷的就是那 8 个字节，拷完两个持有者指向同一个对象，值类型就不再是值类型。
// 这项检查在编译期由前端完成，不是"这一版还没实现"。
func TestStructRefusals(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"字段是 class", `class Note {
    body:string
}

struct Holder {
    n:Note
}

let h:Holder = Holder { n: new Note(body: "便条") }
console.print(h.n.body)
`, "struct 的字段只能放值类型、string 或另一个 struct"},
		{"字段是盒子", `struct Holder {
    o:object
}
`, "struct 的字段只能放值类型、string 或另一个 struct"},
		{"字段是list", `struct Bag {
    items:list<int>
}
`, "struct 的字段只能放值类型、string 或另一个 struct"},
	}
	for _, tc := range cases {
		err := compileErr(tc.src)
		if err == nil {
			t.Errorf("%s：这一条该在编译期就拦下来，结果放行了", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s：拦是拦了，话不对：%v", tc.name, err)
		}
	}
}

// TestStructNestedCopy 覆盖字段嵌套 struct 的那一层：外层拷贝一份时，里面的那一块也要另起，
// 改这一份的内层字段不能影响那一份。检查器只拒绝"字段直接指回自己"，所以环要这条测试自己构造。
func TestStructNestedCopy(t *testing.T) {
	got, code := run(t, `struct Inner {
    v:int
}

struct Outer {
    i:Inner
    tag:int
}

let a:Outer = Outer { i: Inner { v: 1 }, tag: 2 }
let b:Outer = a
b.i.v = 99
b.tag = 7
console.print(a.i.v, a.tag, b.i.v, b.tag)
`)
	want := "1 2 99 7\n"
	if got != want || code != 0 {
		t.Fatalf("嵌了一层 struct 的整份拷贝不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStructEquality 覆盖判等：两份独立造出来的拷贝，字段相同就应算相等。
// struct 在内存里是一整块，比句柄会给出看似合理实则错误的结果，所以这里逐字段比较；
// 文本位置比较内容（@str_cmp），小数位置走 @f_order——NaN 与任何数都不相等这条随之成立。
func TestStructEquality(t *testing.T) {
	got, code := run(t, `struct Point {
    x:int
    y:int
}

struct Tag {
    name:string
    ratio:float
}

let a:Point = Point { x: 1, y: 2 }
let b:Point = a
let c:Point = Point { x: 1, y: 3 }
console.print(a == b, a == c, a != c)

let t1:Tag = Tag { name: "甲", ratio: 0.5 }
let t2:Tag = Tag { name: "甲", ratio: 0.5 }
let t3:Tag = Tag { name: "乙", ratio: 0.5 }
let n1:Tag = Tag { name: "甲", ratio: 0.0 / 0.0 }
let n2:Tag = Tag { name: "甲", ratio: 0.0 / 0.0 }
console.print(t1 == t2, t1 == t3, t1 != t3, n1 == n2)
`)
	want := "true false true\ntrue false true false\n"
	if got != want || code != 0 {
		t.Fatalf("struct 的判等不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if err := compileErr("struct P {\n    x:int\n}\n\nlet a:P = P { x: 1 }\nconsole.print(a < a)\n"); err == nil ||
		!strings.Contains(err.Error(), "只有 ==、!= 两条") {
		t.Fatalf("struct 比大小该在编译期就拦，实际 err=%v", err)
	}
}

// TestStructCopiesAreOwned 验证 struct 值经过声明这一步留下的两块归属：
//
//   - `let r:Row = Row { ... }`：初值就是当场新造的一块，不应再多拷一份。
//   - `let q:Row = r`：这一拷是必要的（r 之后还要能用），拷出来的那一块归 `q`，名字到作用域尽头就析构。
//
// 两万圈实测：前一条修复前 480,160 字节（每轮一块），现在 160；两条一起写修复前 960,160，现在 184。
// 还有一处早已存在、本轮未动：覆盖一个 struct 名字（`r = Row { ... }`）既不析构被覆盖的那一块、
// 手上又多出一块，两万圈 960,160 字节（每轮 48）。
func TestStructCopiesAreOwned(t *testing.T) {
	for _, one := range []struct{ name, src string }{
		{"字面量接住", `
struct Row {
    a:int
    b:int
}
for (i:int from 0 until 20000) {
    let r:Row = Row { a: 1, b: 2 }
}
`},
		{"名字接名字", `
struct Row {
    a:int
    b:int
}
for (i:int from 0 until 20000) {
    let r:Row = Row { a: 1, b: 2 }
    let q:Row = r
}
`},
	} {
		if used := heapAt(t, one.src); used > 4000 {
			t.Errorf("%s：两万圈峰值 %d 字节，那一块大概还没人拆", one.name, used)
		}
	}
}

// compileErr 走完整条链但把错误返回，不像 build 那样当场让测试失败——
// 这类测试要验证的正是"它到底拒不拒绝、说的是哪一句报错"。
func compileErr(src string) error {
	toks, err := lexer.Lex(src)
	if err != nil {
		return err
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		return err
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: "t.nf"}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		return &errs[0]
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		return err
	}
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		return err
	}
	_, err = l.Build(d.User, entry)
	return err
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 整只容器与盒子的打印法（`console.print` 与 `number.to_string` 两个出口）。
//
// 三条规则：形状照字面量的写法；命中回环的那个位置打印 `...`；
// 回环之外另有 32 层的深度上限，两者给出同一个记号。
// 期望串全部来自实际运行结果，语义改动会先在这里变红。

// TestPrintContainers 验证 list、dict、set 的外壳与里面的每一个槽位。
//
// dict 与 set 只放一个键：哈希表的遍历不保证先后顺序，多键的表在这里会漂移。
func TestPrintContainers(t *testing.T) {
	got, code := run(t, `
let ps:list<string> = ["甲", "乙"]
let nested:list<list<int>> = [[1, 2], [3]]
let nums:list<int> = [3, 1, 2]
let empty_list:list<int> = []
let d:dict<string, int> = {"甲": 7}
let empty_dict:dict<string, int> = {}
let s:hashset<int> = {7}
let empty_set:hashset<int> = {}
console.print(ps)
console.print(nested)
console.print(nums)
console.print(empty_list)
console.print(d)
console.print(empty_dict)
console.print(s)
console.print(empty_set)
console.print("甲", ps, "乙")
`)
	want := `["甲", "乙"]
[[1, 2], [3]]
[3, 1, 2]
[]
{"甲": 7}
{}
{7}
{}
甲 ["甲", "乙"] 乙
`
	if got != want || code != 0 {
		t.Fatalf("容器印法不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintShapes 验证 struct 与 class：`Name { a: 1, b: "甲" }`，没有字段的是 `E { }`，
// 空句柄打印 `null`，窄字段（boolean／byte／unicode）按自身宽度读取、不把相邻字段的值读进来。
func TestPrintShapes(t *testing.T) {
	got, code := run(t, `
struct Point {
    x:int
    y:int
}
struct Empty {
}
struct Narrow {
    on:boolean
    b:byte
    c:unicode
    tail:int
}
class Bag {
    name:string
    items:list<int>
}
let p:Point = Point { x: 1, y: 2 }
let e:Empty = Empty { }
let n:Narrow = Narrow { on: true, b: 200, c: '甲', tail: 7 }
let g:Bag = new Bag(name: "甲", items: [1, 2])
let none:Bag = null
console.print(p)
console.print(e)
console.print(n)
console.print(g)
console.print(none)
console.print(number.to_string(p))
`)
	want := `Point { x: 1, y: 2 }
Empty { }
Narrow { on: true, b: 200, c: 甲, tail: 7 }
Bag { name: "甲", items: [1, 2] }
null
Point { x: 1, y: 2 }
`
	if got != want || code != 0 {
		t.Fatalf("struct／class 印法不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintBoxes 验证盒子：拆开打印里面那一个值自身的形状。
//
// 值类型装箱另起一块（正文在类型字后面），class 与 struct 的句柄本身就是那块——两条路取值的
// 层次不一样，所以每种都要覆盖一条用例，不然只有一条在实际运行。空句柄装进盒子再打印仍是 `null`。
func TestPrintBoxes(t *testing.T) {
	got, code := run(t, `
struct Point {
    x:int
    y:int
}
class Bag {
    name:string
}
let p:Point = Point { x: 1, y: 2 }
let g:Bag = new Bag(name: "甲")
let box:object = p as object
let none:Bag = null
console.print(box)
console.print(g as object)
console.print(7 as object)
console.print(true as object)
console.print('甲' as object)
console.print(1.5 as object)
console.print(none as object)
`)
	want := `Point { x: 1, y: 2 }
Bag { name: "甲" }
7
true
甲
1.5
null
`
	if got != want || code != 0 {
		t.Fatalf("盒子印法不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintBoxedEnum 验证装在盒子里的 `enum`：打印结果必须与不装箱时完全相同。
//
// 这一条源于一个真实缺陷：`enum` 是值类型、`x as object` 通过检查，但"类型字→形状"那张表
// 里没有 `enum` 这一行（`fillTagTable` 当时只按 `objKind` 识别 `struct`／`class`），打印机在表里
// 找不到就落到"识别不出"那个分支——打印出深度上限用的 `...`，程序仍返回退出码 0。
// 比崩溃更难排查：屏幕上给出的是错误内容。因此这条测试覆盖四个方面：与不装箱对照、单独一个盒子、
// 装进 list、当 dict 的值，另加一条走 `number.to_string` 的出口（它与 `console.print` 共用同一台打印机）。
func TestPrintBoxedEnum(t *testing.T) {
	got, code := run(t, `
enum Color { red, green }
console.print(Color.red)
console.print(Color.red as object)
console.print([Color.red as object, Color.green as object])
let byName:dict<string, object> = {"甲": Color.green as object}
console.print(byName)
console.print(number.to_string(Color.green as object))
`)
	want := `0
0
[0, 1]
{"甲": 1}
1
`
	if got != want || code != 0 {
		t.Fatalf("盒子里的 enum 印法不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintSideBySide 验证"并列出现的重复不是环"：同一只容器出现在两个并列位置，两处都应打印完整——
// 打印机只在嵌套引用这条路上记录对象身份，不根据内容是否重复来判断。
func TestPrintSideBySide(t *testing.T) {
	got, code := run(t, `
class Bag {
    name:string
    leaves:list<Leaf>
}
class Leaf {
    tag:string
}
let two:list<Bag> = [new Bag(name: "甲", leaves: []), new Bag(name: "乙", leaves: [])]
console.print(two, two)
`)
	want := "[Bag { name: \"甲\", leaves: [] }, Bag { name: \"乙\", leaves: [] }] " +
		"[Bag { name: \"甲\", leaves: [] }, Bag { name: \"乙\", leaves: [] }]\n"
	if got != want || code != 0 {
		t.Fatalf("并列印法不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestCycleNoLongerBuildable 记录交出规则带来的一个结果：
// 回环在语言里**已经无法构造**。此前唯一那条正常路径是"把一只 Bag 放进字段，再从字段读出来"
// （旧规则：读不算交出，那个位置还是持有者）——`let r = w.keep` 之后 `w.keep` 与 `r` 是两个出处。
// 现行规则下这一读就是交出，读完那个位置为空，绕回来的那条路当场断掉。
//
// 打印机"回到同一处印 `...`"那一条、以及 `nef.destroy` 的 dest 标记，从此没有语言层面的路径能走到，
// 留在机器码里只作护栏（多层保护的同一种做法）。
// TestPrintDepthCap 仍覆盖 `...` 的另一半——深度上限这条路照旧走得到。
func TestCycleNoLongerBuildable(t *testing.T) {
	got, code := run(t, `
class Bag {
    name:string
    leaves:list<int>
}
class Wrap {
    keep:Bag
}
let w:Wrap = new Wrap(keep: new Bag(name: "丙", leaves: []))
let r:Bag = w.keep
w.keep.leaves.add(1)
console.print(r.name)
`)
	if code != 1 || !strings.Contains(got, "出错了（10092）：对着空句柄取成员或调用方法") {
		t.Fatalf("接走字段之后源头就空了，这一句该抛 10092：got %q 码 %d", got, code)
	}
}

// TestPrintDepthCap 验证深度上限：回环之外再限 32 层，防止合法但极深的数据把输出撑爆。
//
// 一层是一个 class 加它的 list，计作两层，所以 41 个节点的链在第 17 个节点触及上限。
// 两者给出同一个记号 `...`——读的人不需要区分是回环还是超出深度。
func TestPrintDepthCap(t *testing.T) {
	got, code := run(t, `
class Node {
    kids:list<Node>
}
method grow(n:int):Node {
    let x:Node = new Node(kids: [])
    if (n > 0) {
        x.kids.add(grow(n - 1))
    }
    return x
}
console.print(grow(40))
console.print(grow(6))
`)
	if code != 0 {
		t.Fatalf("跑挂了：码 %d", code)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("该两行：%q", got)
	}
	wantDeep := strings.Repeat("Node { kids: [", 16) + "..." + strings.Repeat("] }", 16)
	if lines[0] != wantDeep {
		t.Fatalf("40 层该被上限截住：\n got %q\nwant %q", lines[0], wantDeep)
	}
	wantSix := strings.Repeat("Node { kids: [", 7) + strings.Repeat("] }", 7)
	if lines[1] != wantSix {
		t.Fatalf("7 个节点没到上限，该印满：\n got %q\nwant %q", lines[1], wantSix)
	}
}

// TestToStringMatchesPrint 验证两个出口走同一段代码：打印到屏幕的与转成文本的逐字相同，
// 容器套容器、容器里装盒子也一样。
func TestToStringMatchesPrint(t *testing.T) {
	got, code := run(t, `
struct Point {
    x:int
    y:int
}
let ps:list<string> = ["甲", "乙"]
let shapes:list<Point> = [Point { x: 1, y: 2 }]
let d:dict<string, list<int>> = {"甲": [1, 2]}
let mixed:list<object> = []
mixed.add(Point { x: 3, y: 4 } as object)
mixed.add(5 as object)
mixed.add('乙' as object)
console.print(number.to_string(ps) + "!")
console.print(shapes)
console.print(d)
console.print(mixed)
console.print(number.to_string(mixed))
console.print(number.to_string(shapes[0]))
`)
	want := `["甲", "乙"]!
[Point { x: 1, y: 2 }]
{"甲": [1, 2]}
[Point { x: 3, y: 4 }, 5, 乙]
[Point { x: 3, y: 4 }, 5, 乙]
Point { x: 1, y: 2 }
`
	if got != want || code != 0 {
		t.Fatalf("两处出口不一致：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintNullInside 验证"空句柄嵌在容器里"那个位置：打印 `null`，不让程序崩溃。
//
// 一行打印三个值、其中一个是空句柄：那个位置给出 `null` 已足够明确，另外两个照常打印。
func TestPrintNullInside(t *testing.T) {
	got, code := run(t, `
class Bag {
    name:string
}
let some:Bag = new Bag(name: "甲")
let none:Bag = null
let box:list<Bag> = [some, none]
console.print(box)
// list 字面量算交出：some、none 装进那个位置就交出去了，
// 所以要试"拿着空句柄访问字段"，得另外声明一个还是空的。
let other:Bag = null
console.print(other.name)
`)
	if code == 0 || !strings.Contains(got, "出错了（10092）") {
		t.Fatalf("空句柄点字段该报 10092 号：got %q 码 %d", got, code)
	}
	if !strings.Contains(got, `[Bag { name: "甲" }, null]`) {
		t.Fatalf("list里那个位置空句柄该印 null：got %q", got)
	}
}

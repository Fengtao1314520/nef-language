// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 规则：一个式子中间那几步当场产生的值，语句走到尽头按产生的反序释放。
//
// 这一族的错误形态不是"泄漏"而是"释放过早"：中间那一步的值若被下一句、桶里那个位置、
// 或接住它的一侧还要使用，当场释放就是继续使用一个已释放的句柄。所以每一段都先把值
// 读出来核对，再量堆。读一个位置不交出那个值，中间那几步里
// 拼接、切出、拷出的那几块仍按这一条归还；下面每一段都只把同一个位置读一次或读两次，
// 读两遍的段落用来证明那个位置没被读空。
//
// 顺序有要求：run() 的断言都放在 heapSlope 之前——heapSlope 会设 NEF_HEAP，
// 之后的 run() 输出会多一行 `heap …`。

// TestMidReadValuesStillRight 九种写法各跑一遍，值一个都不能变。
func TestMidReadValuesStillRight(t *testing.T) {
	got, code := run(t, `let xs:list<list<int>> = [[1, 2], [3, 4]]
xs[0][0];
console.print(length(xs), xs[1][0])
`)
	if got != "2 3\n" || code != 0 {
		t.Fatalf("这一句没人接住、中间那一步读走的那一条：\n got %q 码 %d\nwant \"2 3\\n\"", got, code)
	}

	got, code = run(t, `let d:dict<string, list<int>> = {"甲": [1, 2], "乙": [3]}
d["甲"][0];
console.print(length(d), d["乙"][0])
`)
	if got != "2 3\n" || code != 0 {
		t.Fatalf("字典那一格再点一下：\n got %q 码 %d\nwant \"2 3\\n\"", got, code)
	}

	got, code = run(t, `struct Rec {
    n:int
}
let cs:list<Rec> = [Rec { n: 7 }, Rec { n: 8 }]
cs[0].n;
cs[1].n;
console.print(cs[0].n, cs[1].n)
`)
	if got != "7 8\n" || code != 0 {
		t.Fatalf("值类型那一格读出来的拷贝拆掉之后，那一格照旧读得到：\n got %q 码 %d\nwant \"7 8\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2], [3, 4]]
console.print(xs[0][0], xs[0][1])
console.print(length(xs))
`)
	if got != "1 2\n2\n" || code != 0 {
		t.Fatalf("读两遍都读得到，容器那一格不空：\n got %q 码 %d\nwant \"1 2\\n2\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2]]
let t:int = xs[0][0];
console.print(t)
`)
	if got != "1\n" || code != 0 {
		t.Fatalf("声明的初值里中间那一步：\n got %q 码 %d\nwant \"1\\n\"", got, code)
	}

	got, code = run(t, `method tail(rows:list<list<int>>):list<int> {
    return rows.remove_at(0);
}
let back:list<int> = tail([[5, 6]])
console.print(back[0], back[1])
`)
	if got != "5 6\n" || code != 0 {
		t.Fatalf("return 交回的是摘出来的那一格：\n got %q 码 %d\nwant \"5 6\\n\"", got, code)
	}

	got, code = run(t, `class Node {
    v:int

    method twice():int {
        return v + v
    }
}
let xs:list<Node> = [new Node(v: 3), new Node(v: 4)]
console.print(xs[0].twice(), xs[1].twice())
`)
	if got != "6 8\n" || code != 0 {
		t.Fatalf("读出来当方法接收者：\n got %q 码 %d\nwant \"6 8\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2], [3, 4]]
for (v:int in xs[0]) {
    console.print(v)
}
console.print(length(xs))
`)
	if got != "1\n2\n2\n" || code != 0 {
		t.Fatalf("遍历的是读走的那一条：\n got %q 码 %d\nwant \"1\\n2\\n2\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[3, 4], [1, 2]]
let sum:int = 0
for (j:int in 0 until xs[1][0]) {
    sum = sum + j + 10
}
console.print(sum, length(xs))
`)
	if got != "10 2\n" || code != 0 {
		t.Fatalf("计数边界里中间那一步：\n got %q 码 %d\nwant \"10 2\\n\"", got, code)
	}
}

// TestMidReadIntoCell 交进容器那个位置的值不算"没人接住"：`add`、`insert_at`、`d[键]=值`、
// `d.add(key, value)` 四个落点都把句柄留在位置里，持有者就是那个位置。窗口逻辑要把它们挑出去，
// 其余的中间步骤照常释放。
func TestMidReadIntoCell(t *testing.T) {
	got, code := run(t, `let ys:list<list<int>> = [[1, 2], [3, 4]]
let xs:list<list<int>> = [[9]]
xs.add(ys.remove_at(0))
xs.insert_at(0, ys.remove_at(0))
console.print(length(xs), length(ys))
console.print(xs[0][0], xs[0][1], xs[2][0], xs[2][1])
`)
	if got != "3 0\n3 4 1 2\n" || code != 0 {
		t.Fatalf("add／insert_at 交进去的那两条还在容器里：\n got %q 码 %d\nwant \"3 0\\n3 4 1 2\\n\"", got, code)
	}

	got, code = run(t, `let ks:list<string> = ["甲", "乙"]
let d:dict<string, int> = {}
d[ks[0] + ""] = 7
d.add(ks[1] + "", 8)
console.print(d["甲"], d["乙"], length(d), length(ks))
`)
	if got != "7 8 2 2\n" || code != 0 {
		t.Fatalf("键复制一份进桶里之后，源那一头的名字还在：\n got %q 码 %d\nwant \"7 8 2 2\\n\"", got, code)
	}

	got, code = run(t, `class Bag {
    xs:list<int>
}
let bags:list<Bag> = [new Bag(xs: [1]), new Bag(xs: [2])]
bags[0].xs.add(99)
console.print(bags[0].xs[1], length(bags[0].xs))
`)
	if got != "99 2\n" || code != 0 {
		t.Fatalf("读出来的一格当接收者，add 之后那一格照旧读得到：\n got %q 码 %d\nwant \"99 2\\n\"", got, code)
	}

	got, code = run(t, `let vs:list<list<int>> = [[7, 8]]
let xs:list<list<int>> = [[1]]
xs.add(vs.remove_at(0))
console.print(xs[1][0], xs[1][1])
`)
	if got != "7 8\n" || code != 0 {
		t.Fatalf("摘出来的那一条进另一条 list 的一格：\n got %q 码 %d\nwant \"7 8\\n\"", got, code)
	}
}

// TestMidReadRemoveAndContains remove／contains 的键只被比较一次，不留在表里：例程结束即释放。
// 释放时机必须排在 `@map_remove`／`@map_contains` 发出之后，早一步比较的对象就是一段已释放的文本。
func TestMidReadRemoveAndContains(t *testing.T) {
	got, code := run(t, `let ks:list<string> = ["甲", "乙"]
let d:dict<string, int> = {"甲": 1, "乙": 2, "丙": 3}
console.print(d.contains(ks[0]))
d.remove(ks[1])
console.print(length(d), d["丙"])
`)
	if got != "true\n2 3\n" || code != 0 {
		t.Fatalf("contains／remove 的键比完才还：\n got %q 码 %d\nwant \"true\\n2 3\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2], [3, 4]]
let a:list<int> = xs.remove_at(0)
console.print(a[0], length(a))
a.remove_at(0)
console.print(a[0], length(a))
`)
	if got != "1 2\n2 1\n" || code != 0 {
		t.Fatalf("remove_at 之后那一条还在原处：\n got %q 码 %d\nwant \"1 2\\n2 1\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2], [3, 4]]
let a:list<int> = xs.remove_at(1)
console.print(a.index_of(4), a.index_of(9), length(a))
`)
	if got != "1 -1 2\n" || code != 0 {
		t.Fatalf("index_of 找不着的那个键值不影响后面：\n got %q 码 %d\nwant \"1 -1 2\\n\"", got, code)
	}
}

// TestDictGetStillKeepsBothPaths `d.get(键, 默认)` 两条路读回来的都是借来的那一份：命中就是字典那个位置，
// 没命中就是调用方自己带进来的那一个默认值。两条都不许接给名字，读了字典也不变。
func TestDictGetStillKeepsBothPaths(t *testing.T) {
	got, code := run(t, `let d:dict<string, list<int>> = {"甲": [1, 2]}
console.print(d.get("甲", [9])[0], d.get("甲", [9])[1])
console.print(d.get("乙", [7, 8])[0], d.get("乙", [7, 8])[1], length(d))
`)
	if got != "1 2\n7 8 1\n" || code != 0 {
		t.Fatalf("命中与没命中两条路：\n got %q 码 %d\nwant \"1 2\\n7 8 1\\n\"", got, code)
	}
}

// TestMidReadSweep 每轮各读一次：两种圈数之差除以圈数就是每轮留下的字节数，应为 0。
// 一个 list<int> 连头带那两个位置是 88 字节，一个 class 对象是 16 字节——修复之前每轮就留下这么多。
func TestMidReadSweep(t *testing.T) {
	const stmt = `let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    xs[0][0];
    total = total + 1
}
console.print(total)
`
	const decl = `let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    let t:int = xs[0][0];
    total = total + t
}
console.print(total)
`
	// 等号左边那一条路径此前直接写成 `xs[0][0] = 7;`，现行规则在检查阶段就拒绝它（连续取下标的
	// 写入不支持）。读一个位置现在也不交出来，所以要改内层那一条就把它摘下来再改：`remove_at` 交出的
	// 那一条归这个名字，改完要么放回去，要么就让它出作用域。
	const assign = `let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[1, 1], [1, 1]]
    let row:list<int> = xs.remove_at(0)
    row[0] = 7
    total = total + row[0] + length(xs)
}
console.print(total)
`
	const recv = `class Node {
    v:int

    method ping() {
        console.print(v)
    }
}
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<Node> = [new Node(v: 7), new Node(v: 8)]
    xs[0].ping()
    total = total + 1
}
console.print(total)
`
	// 印得多的那几段只跑三圈，先把输出核对完再量堆
	if got, code := run(t, strings.ReplaceAll(recv, "@N@", "3")); got != "7\n7\n7\n3\n" || code != 0 {
		t.Fatalf("接收者那一段跑坏了：码 %d got %q", code, got)
	}
	for _, one := range []struct {
		name string
		tmpl string
	}{
		{"这一句没人接住", stmt},
		{"声明的初值", decl},
		{"先摘出来再写那个位置", assign},
		{"读出来的接收者", recv},
	} {
		per := heapSlope(t, one.tmpl)
		t.Logf("%-16s 每轮 %8.1f 字节", one.name, per)
		if per > 2 {
			t.Errorf("%s：每轮还留 %g 字节，中间那一步没登记上", one.name, per)
		}
	}
}

// TestUnboxOfABorrowKeepsTheBox 覆盖这一条：顺着读的那一次装箱与拆箱得到的还是同一份借用，
// 盒子与容器那个位置都不改动。同一个盒子里的值读两遍都得是它，走完一遍容器仍然是满的。
func TestUnboxOfABorrowKeepsTheBox(t *testing.T) {
	got, code := run(t, `class Circle {
    r:float
}
let box:list<object> = [new Circle(r: 2.0) as object, new Circle(r: 3.0) as object]
console.print((box[0] as Circle).r, (box[0] as Circle).r, length(box))
for (one:object in box) {
    console.print((one as Circle).r, (one as Circle).r, one is Circle)
}
console.print(length(box), box[1] is Circle)
`)
	const want = "2.0 2.0 2\n2.0 2.0 true\n3.0 3.0 true\n2 true\n"
	if got != want || code != 0 {
		t.Fatalf("顺着拆盒子跑坏了：码 %d\n got %q\nwant %q", code, got, want)
	}

	const circleMethod = `class Circle {
    r:float

    method kind():int {
        return 1
    }
}
let total:int = 0
for (i:int in 0 until @N@) {
    let box:list<object> = [new Circle(r: 2.0) as object]
    for (one:object in box) {
        total = total + (one as Circle).kind()
    }
}
console.print(total)
`
	// `kind()` 只是把一个读出来的数带回来，避免这一段因为不打印而把整条链优化掉
	if got, code := run(t, strings.ReplaceAll(circleMethod, "@N@", "3")); got != "3\n" || code != 0 {
		t.Fatalf("拆出来调方法那一段跑坏了：码 %d got %q", code, got)
	}
	per := heapSlope(t, circleMethod)
	t.Logf("%-16s 每轮 %8.1f 字节", "顺着拆盒子借用", per)
	if per > 2 {
		t.Errorf("顺着拆盒子：每轮还留 %g 字节，借来的那一份被当成接手了", per)
	}
}

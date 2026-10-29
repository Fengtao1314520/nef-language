// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 规则：一个式子中间那几步读走的值，语句走到尽头按读走的反序释放。
//
// 这一族的错误形态不是"泄漏"而是"释放过早"：中间那一步读走的那一块若被下一句、桶里那个位置、
// 或接住它的一侧还要使用，当场释放就是继续使用一个已释放的句柄。所以每一段都先把值
// 读出来核对，再量堆。规则是一个位置一份拷贝，把引用类型那个位置读走之后该位置为空，
// 下面每一段都只读同一个位置一次。
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
let a:list<int> = xs[0];
console.print(a[0], a[1])
`)
	if got != "1 2\n" || code != 0 {
		t.Fatalf("中间那一步读走的交给名字：\n got %q 码 %d\nwant \"1 2\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2]]
let t:int = xs[0][0];
console.print(t)
`)
	if got != "1\n" || code != 0 {
		t.Fatalf("声明的初值里中间那一步：\n got %q 码 %d\nwant \"1\\n\"", got, code)
	}

	got, code = run(t, `method tail(rows:list<list<int>>):list<int> {
    return rows[0];
}
let xs:list<list<int>> = [[5, 6]]
let back:list<int> = tail(xs)
console.print(back[0], back[1])
`)
	if got != "5 6\n" || code != 0 {
		t.Fatalf("return 交回读走的那一格：\n got %q 码 %d\nwant \"5 6\\n\"", got, code)
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
xs.add(ys[0])
xs.insert_at(0, ys[1])
console.print(length(xs))
let head:list<int> = xs[0]
let tail:list<int> = xs[2]
console.print(head[0], head[1], tail[0], tail[1])
`)
	if got != "3\n3 4 1 2\n" || code != 0 {
		t.Fatalf("add／insert_at 交进去的那两条还在容器里：\n got %q 码 %d\nwant \"3\\n3 4 1 2\\n\"", got, code)
	}

	got, code = run(t, `let ks:list<string> = ["甲", "乙"]
let d:dict<string, int> = {}
d[ks[0]] = 7
d.add(ks[1], 8)
console.print(d["甲"], d["乙"], length(d))
`)
	if got != "7 8 2\n" || code != 0 {
		t.Fatalf("读走的那一把键住进桶里之后还印得出来：\n got %q 码 %d\nwant \"7 8 2\\n\"", got, code)
	}

	got, code = run(t, `class Bag {
    xs:list<int>
}
let bags:list<Bag> = [new Bag(xs: [1]), new Bag(xs: [2])]
let b0:Bag = bags[0]
b0.xs.add(99)
console.print(b0.xs[1], length(b0.xs))
`)
	if got != "99 2\n" || code != 0 {
		t.Fatalf("接收者读走之后 add、那一格还能接着读：\n got %q 码 %d\nwant \"99 2\\n\"", got, code)
	}

	got, code = run(t, `let vs:list<list<int>> = [[7, 8]]
let xs:list<list<int>> = [[1]]
xs.add(vs[0])
let moved:list<int> = xs[1]
console.print(moved[0], moved[1])
`)
	if got != "7 8\n" || code != 0 {
		t.Fatalf("读走的那一条进另一条 list 的一格：\n got %q 码 %d\nwant \"7 8\\n\"", got, code)
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
let a:list<int> = xs[0]
console.print(a[0], length(a))
a.remove_at(0)
console.print(a[0], length(a))
`)
	if got != "1 2\n2 1\n" || code != 0 {
		t.Fatalf("remove_at 之后那一条还在原处：\n got %q 码 %d\nwant \"1 2\\n2 1\\n\"", got, code)
	}

	got, code = run(t, `let xs:list<list<int>> = [[1, 2], [3, 4]]
let a:list<int> = xs[1]
console.print(a.index_of(4), a.index_of(9), length(a))
`)
	if got != "1 -1 2\n" || code != 0 {
		t.Fatalf("index_of 找不着的那个键值不影响后面：\n got %q 码 %d\nwant \"1 -1 2\\n\"", got, code)
	}
}

// TestDictGetStillKeepsBothPaths `d.get(键, 默认)` 不在这一条规则的改动范围：命中返回的是那个位置里读走的
// 那一份、没命中返回的是默认那一份，两个都不是结果所在的位置，窗口无法判定该留哪一笔。
// 这一段验证"未改动的行为仍成立"：两条路各自的值都读得到。
func TestDictGetStillKeepsBothPaths(t *testing.T) {
	got, code := run(t, `let d:dict<string, list<int>> = {"甲": [1, 2]}
let hit:list<int> = d.get("甲", [9])
console.print(hit[0], hit[1])
let miss:list<int> = d.get("乙", [7, 8])
console.print(miss[0], miss[1], length(d))
`)
	if got != "1 2\n7 8 1\n" || code != 0 {
		t.Fatalf("命中与没命中两条路：\n got %q 码 %d\nwant \"1 2\\n7 8 1\\n\"", got, code)
	}
}

// TestMidReadSweep 每圈各读一次：两种圈数之差除以圈数就是每圈留下的字节数，应为 0。
// 一个 list<int> 连头带那两个位置是 88 字节，一个 class 对象是 16 字节——修复之前每圈就留下这么多。
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
	// 等号左边那一条路径此前直接写成 `xs[0][0] = 7;`，现行规则在检查阶段就拒绝它（读走那个位置即抹空，
	// 写不进容器里那个位置）。这里换成规则给出的写法：先绑一个名字，再改那个名字持有的那一份。
	const assign = `let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[1, 1], [1, 1]]
    let row:list<int> = xs[0]
    row[0] = 7
    total = total + row[0]
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
		{"先绑名字再写那一格", assign},
		{"读走的接收者", recv},
	} {
		per := heapSlope(t, one.tmpl)
		t.Logf("%-16s 每圈 %8.1f 字节", one.name, per)
		if per > 2 {
			t.Errorf("%s：每圈还留 %g 字节，中间那一步没登记上", one.name, per)
		}
	}
}

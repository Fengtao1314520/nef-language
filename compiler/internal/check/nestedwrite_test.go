// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// 写入与自增的路径中间还要再下一层时，在检查阶段拒绝（判成不支持）。
//
// 先前这两样过了检查，运行才出问题：`xs[0][0] = 7;` 写的那一行已经从 `xs` 上摘下来了
// （读走那个位置就把它置空），想要的数不在 `xs` 上；`xs[0][0]++;` 把同一条路径读两遍，
// 第二遍拿到的是空句柄，当场抛 10092。`class` 那一侧同一形状（`a[0].v = 7;`）。
// 要改先绑一个名字。值类型那个位置读走不影响源头，所以 `xs[0].n = 3`、`d["甲"].n = 4` 不在这一条里。
func TestNestedWritePathRefused(t *testing.T) {
	const want = "中间那个位置读走就空了，改不到它的下一层：先给它绑一个名字"
	for _, src := range []string{
		`let xs:list<list<int>> = [[1, 2]]
xs[0][0] = 7`,
		`let xs:list<list<int>> = [[1, 2]]
xs[0][0]++`,
		`let xs:list<list<int>> = [[1, 2]]
xs[0][0]--`,
		`let g:list<list<list<int>>> = [[[1]]]
g[0][0][0] = 7`,
		`let m:dict<string, dict<string, int>> = {"甲": {"乙": 1}}
m["甲"]["乙"] = 2`,
		`class Row {
    v:int
}
let a:list<Row> = [new Row(v: 1)]
a[0].v = 7`,
		`class Row {
    v:int
}
let a:list<Row> = [new Row(v: 1)]
a[0].v++`,
		`class Row {
    v:int
    kids:list<int>
}
let a:list<Row> = [new Row(v: 1, kids: [9])]
a[0].kids[0] = 7`,
	} {
		expectErr(t, src, want)
	}
	for _, src := range []string{
		`let xs:list<list<int>> = [[1, 2]]
let row:list<int> = xs[0]
row[0] = 7
console.print(row[0])`,
		`let xs:list<int> = [1, 2]
xs[0] = 7
xs[0]++`,
		`struct Rec {
    n:int
}
let xs:list<Rec> = [Rec { n: 1 }]
xs[0].n = 3`,
		`struct Rec {
    n:int
}
let d:dict<string, Rec> = {"甲": Rec { n: 1 }}
d["甲"].n = 4`,
		// 中间那一步是字段读走：句柄指的就是对象本身那一块，写得进去，不在这一条里
		`class Row {
    kids:list<int>
}
let b:Row = new Row(kids: [9])
b.kids[0] = 7`,
	} {
		expectOK(t, src)
	}
}

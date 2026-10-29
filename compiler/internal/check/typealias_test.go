// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import (
	"testing"

	"neflang.org/nf/internal/ast"
)

// threeFiles 是 twoFiles 那一个的三份版：跨文件的别名要连着三份才试得准
// （入口引别名、中间那份定义别名并从第三份引一只类型、第三份只定义那一只类型）。
func threeFiles(t *testing.T, a, b, c string) []Error {
	t.Helper()
	return Run(&Program{
		Files: []*ast.File{mustFile(t, "a.nf", a), mustFile(t, "b.nf", b), mustFile(t, "c.nf", c)},
		Entry: "a.nf",
	})
}

// TestTypeAliasInTypePositions 覆盖别名"能用的地方"这一半：别名写在哪一处，解出来的
// 都是右边那一个，参数、返回、字段、尖括号、默认值那几处都算。
func TestTypeAliasInTypePositions(t *testing.T) {
	expectOK(t, `type Names = list<string>
method first(xs:Names):string { return xs[0] }
let a:Names = ["甲", "乙"]
console.print(first(a))
`)
	// 尖括号里套别名，别名再套别名
	expectOK(t, `type K = string
type Scores = dict<K, int>
type Groups = list<Scores>
let g:Groups = [{"甲": 1}]
console.print(length(g), g[0]["甲"])
`)
	// 字段类型写别名（struct 与 class 各一处）
	expectOK(t, `type Count = int
struct Row {
    n:Count
}
class Box {
    n:Count
}
let r:Row = Row { n: 3 }
let b:Box = new Box()
b.n = r.n
console.print(r.n, b.n)
`)
	// 参数默认值那一个位置也按同一份类型查
	expectOK(t, `type Count = int
method twice(n:Count = 2):Count { return n + n }
console.print(twice(3), twice())
`)
}

// TestTypeAliasEqualsFullName 覆盖"与写全名一模一样"那一半：造值、取枚举成员、装进盒子再判回来，
// 写别名与写全名走的是同一条路。
func TestTypeAliasEqualsFullName(t *testing.T) {
	expectOK(t, `struct Row {
    x:int
}
class Box {
    n:int
}
type Row2 = Row
type Box2 = Box
let r:Row2 = Row2 { x: 1 }
let b:Box2 = new Box2()
b.n = r.x
console.print(r.x, b.n)
`)
	// 枚举成员通过别名取，`switch` 的标签也照样算列全
	expectOK(t, `enum D { a, b }
type Choice = D
let c:Choice = Choice.b
switch (c) {
    Choice.a { console.print("甲") }
    Choice.b { console.print("乙") }
}
`)
	// 装进盒子之后 `is` 写别名与写全名指的是同一只
	expectOK(t, `struct P {
    x:int
}
type P2 = P
let box:object = P2 { x: 1 } as object
console.print(box is P2, box is P)
`)
	// 别名指内建那只 `Calendar`
	expectOK(t, `type Cal = Calendar
let c:Cal = time.calendar(0)
console.print(c.year)
`)
}

// TestTypeAliasRejected 固定别名不该成立的几处：带尖括号、环、重名、右边没有那一只、
// 指接口、当值用。每一句都按整句判。
func TestTypeAliasRejected(t *testing.T) {
	expectErr(t, "type Names = list<string>\nlet ys:Names<int> = 1\n", "Names 是类型别名，不带尖括号参数")
	expectErr(t, "type A = A\n", "类型别名 A 绕回了自己")
	expectErr(t, "type A = B\ntype B = A\n", "类型别名 A 绕回了自己")
	expectErr(t, "struct P {\n    x:int\n}\ntype P = int\n", "P 已经定义过了")
	expectErr(t, "type P = int\nstruct P {\n    x:int\n}\n", "P 已经定义过了")
	expectErr(t, "type Names = list<string>\ntype Names = list<int>\n", "Names 已经定义过了")
	expectErr(t, "type Q = NoSuch\n", "没有 NoSuch 这个类型")
	expectErr(t, "type I = MyIface\ninterface MyIface {\n    f:int\n}\n", "MyIface 是接口名，不能当类型写")
	expectErr(t, "type let = int\n", "let 是关键字，不能拿去做名字")
	expectErr(t, "type int = string\n", "int 是内建名字，不能拿去做名字")
	expectErr(t, "type Count = int\nconsole.print(Count)\n", "Count 是类型名，不是值（int）")
	expectErr(t, "struct Point {\n    x:int\n}\ntype Pair = Point\nconsole.print(Pair(1))\n", "Pair 是类型名，要用 new 造值")
}

// TestTypeAliasAcrossFiles 覆盖跨文件的别名三件事：`export type` 引得来；引来的名字只在
// 别名右边用一次也算用上了（不然多报一句"import 了却没用到"）；右边写错那一只时，
// 那一句错报在定义它的 b.nf，不是碰巧第一个用到它的 a.nf。
func TestTypeAliasAcrossFiles(t *testing.T) {
	errs := threeFiles(t,
		"import { Names } from \"./b.nf\"\nlet xs:Names = []\nconsole.print(length(xs))\n",
		"import { Thing } from \"./c.nf\"\nexport type Names = list<Thing>\n",
		"export struct Thing {\n    x:int\n}\n")
	if len(errs) > 0 {
		t.Fatalf("跨文件的别名不该报错：%v", msgs(errs))
	}
	again := threeFiles(t,
		"import { Bad } from \"./b.nf\"\nlet x:Bad = 1\nconsole.print(x)\n",
		"export type Bad = NoSuch\n",
		"export struct Thing {\n    x:int\n}\n")
	if len(again) != 1 || again[0].File != "b.nf" || again[0].Msg != "没有 NoSuch 这个类型" {
		t.Fatalf("那句错该只报在 b.nf：%v", msgs(again))
	}
}

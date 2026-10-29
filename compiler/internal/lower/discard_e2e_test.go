// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// discard_e2e_test.go 覆盖"无人接住的值"的两处归属规则：
//
//   - 一句里没人接住的那一份，到这一句尽头就析构——从容器位置读走的那一份（`xs[0];`、`xs.first();`、
//     `xs.remove_at(0);`）、拼出来的一串（`"甲乙" + "丙丁";`）、标准库现造的那一份（`s.trim();`）。
//   - `d.remove(key)` 删掉的那一条，值位置里的那一块也要析构。
//
// 两处此前都每圈留一块，两万圈实测（`NEF_HEAP` 累计那一列，修复前 → 修复后；修复前那一批数字是把下面
// 三处改动退回之后由同一批测试跑出来的）：
//
//   - `xs[0];`、`xs.first();`、`xs.remove_at(0);` 各 800,224 → 264；`d["键"];` 800,384 → 424；
//     `"甲乙" + "丙丁";` 800,136 → 176；`s.trim();` 640,136 → 168；`new Node(n: 1);` 320,136 → 152；
//     `[1, 2];` 3,333,008 → 224；`xs.slice(0, 1);` 4,235,280 → 344；`d.values();` 3,514,584 → 536。
//     同一句改成用名字接住（`let one:string = xs[0];`）修复前后都是 264——这条规则只处理没人接住的那些。
//   - `d.remove(key)` 删掉的那一条：值是文本时 800,384 → 424、值是 list 时 1,760,408 → 472。
//     `s.remove(那一项)` 走同一条运行期例程，集合的值位置一直是 0，修复前后都是 424。

package lower_test

import "testing"

func TestDiscardedValueFreedAtStatementEnd(t *testing.T) {
	cases := []struct{ name, src string }{
		{"下标读走不接住", `
for (i:int in 0 until 20000) {
    let xs:list<string> = ["甲乙" + "丙丁", "尾"]
    xs[0];
}
`},
		{"dict 的下标读走不接住", `
for (i:int in 0 until 20000) {
    let d:dict<string, string> = {}
    d.add("键", "甲乙" + "丙丁")
    d["键"];
}
`},
		{"first 读走不接住", `
for (i:int in 0 until 20000) {
    let xs:list<string> = ["甲乙" + "丙丁", "尾"]
    xs.first();
}
`},
		{"remove_at 删下来不接住", `
for (i:int in 0 until 20000) {
    let xs:list<string> = ["甲乙" + "丙丁", "尾"]
    xs.remove_at(0);
}
`},
		{"拼出来的一串不接住", `
for (i:int in 0 until 20000) {
    "甲乙" + "丙丁";
}
`},
		{"标准库现造的一串不接住", `
for (i:int in 0 until 20000) {
    let s:string = "  甲乙  "
    s.trim();
}
`},
		{"new 出来的一块不接住", `
class Node {
    n:int
}
for (i:int in 0 until 20000) {
    new Node(n: 1);
}
`},
		{"list 字面量不接住", `
for (i:int in 0 until 20000) {
    [1, 2];
}
`},
		{"取一段不接住", `
for (i:int in 0 until 20000) {
    let xs:list<string> = ["甲", "乙"]
    xs.slice(0, 1);
}
`},
		{"取全部值不接住", `
for (i:int in 0 until 20000) {
    let d:dict<string, string> = {"甲": "乙" + "丙"}
    d.values();
}
`},
	}
	for _, one := range cases {
		if used := heapAt(t, one.src); used > 4000 {
			t.Errorf("%s：两万圈峰值 %d 字节，那一份大概还没人拆", one.name, used)
		}
	}
}

// TestDiscardLeavesLiveHoldersAlone 验证反方向：只有"没人接住"的那一份才析构。
// 名字接住了归那个名字、字段读出来那一处还是持有者、print 的只读借用自己那一路已经释放过——
// 这三种都不应在语句尽头规则下再析构一次。
func TestDiscardLeavesLiveHoldersAlone(t *testing.T) {
	got, code := run(t, `
class Box {
    v:string
}
let b:Box = new Box(v: "内" + "容")
b.v;
console.print(b.v, b)

let xs:list<string> = ["甲", "乙"]
console.print(xs[0])
console.print(xs)

struct Row {
    a:string
}
let rs:list<Row> = [Row { a: "甲" + "乙" }]
rs[0];
console.print(rs)
let head:Row = rs.first()
console.print(head)
`)
	want := `内容 Box { v: "内容" }
甲
[null, "乙"]
[Row { a: "甲乙" }]
Row { a: "甲乙" }
`
	if got != want || code != 0 {
		t.Fatalf("有人接住的那几处不该被这一句尽头的拆碰掉：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestDictRemoveFreesTheValue 验证 `d.remove(key)` 删掉那一条时对其值位置的析构。`hashset.remove` 走的是
// 同一条运行期例程，值位置一直是 0，所以没有需要析构的东西。
func TestDictRemoveFreesTheValue(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲", "乙", "丙"]
xs[0];
console.print(length(xs), xs)
xs.remove_at(0);
console.print(length(xs), xs)

let d:dict<string, string> = {}
d.add("一", "甲" + "乙")
d.add("二", "丙" + "丁")
d.remove("一")
console.print(length(d), d)
d.remove("没有")
console.print(length(d), d)

let dn:dict<string, int> = {}
dn.add("k", 1)
dn.remove("k")
dn.remove("k")
console.print(dn)

let s:hashset<string> = {}
s.add("甲" + "乙")
s.remove("甲乙")
s.remove("甲乙")
console.print(length(s), s)
`)
	want := `3 [null, "乙", "丙"]
2 ["乙", "丙"]
1 {"二": "丙丁"}
1 {"二": "丙丁"}
{}
0 {}
`
	if got != want || code != 0 {
		t.Fatalf("读走不接住与删成的那一条之后的样子不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// heapAt 会把 NEF_HEAP 设进这一条测试的子进程，跑完再比对别的输出就会多出一行，所以放最后
	for _, one := range []struct{ name, elem, val string }{
		{"值是文本", "string", `"甲乙" + "丙丁"`},
		{"值是list", "list<int>", "[1, 2, 3]"},
	} {
		if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let d:dict<string, `+one.elem+`> = {}
    d.add("键", `+one.val+`)
    d.remove("键")
}
`); used > 4000 {
			t.Errorf("%s：两万圈删了又造，峰值 %d 字节，被删那一条的值大概还没人拆", one.name, used)
		}
	}
}

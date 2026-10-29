// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"fmt"
	"strings"
	"testing"
)

// sortsearch_e2e_test.go 覆盖排序与查找相关的成员：
// list.sort、list.index_of、string.index_of、string.starts_with、string.ends_with、dict.keys 与 dict.values，
// 外加 math.square_root。适用范围（哪些类型能排、keys 与 values 返回什么）由 internal/check 的
// member_range_test.go 验证，这一份只核对实际运行结果。

// TestListSortByKind 五种可比较类型各一次，再加一个 20 个元素、逆序排列的 list：
// 归并排序要跑多轮，宽度超过 n 才结束，逆序输入对它代价最大。
func TestListSortByKind(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [5, 3, 9, 1, 7]
xs.sort()
console.print(xs)
let ws:list<string> = ["梨", "苹果", "桃", "苹果"]
ws.sort()
console.print(ws)
let fl:list<float> = [2.5, -1.25, 0.5, 2.5]
fl.sort()
console.print(fl)
let bs:list<byte> = [200 as byte, 3 as byte, 100 as byte]
bs.sort()
console.print(bs)
let us:list<unicode> = "丙甲乙".to_list()
us.sort()
console.print(us)
let many:list<int> = []
for (i:int in 0 until 20) {
    many.add(19 - i)
}
many.sort()
let back:list<int> = []
for (i:int in 0 until 20) {
    back.add(i)
}
back.sort()
console.print(many[0], many[19], back[0], back[19])
let one:list<int> = [7]
one.sort()
console.print(one, length(one))
let none:list<int> = []
none.sort()
console.print(none)
`)
	// 文本按 UTF-8 字节排：苹果 E8 打头，比 桃(E6 A1) 与 梨(E6 A8) 都大；
	// 码点那一列同理，丙 4E19 < 乙 4E59 < 甲 7532。
	want := "[1, 3, 5, 7, 9]\n" +
		"[\"桃\", \"梨\", \"苹果\", \"苹果\"]\n" +
		"[-1.25, 0.5, 2.5, 2.5]\n" +
		"[3, 100, 200]\n" +
		"[丙, 乙, 甲]\n" +
		"0 19 0 19\n" +
		"[7] 1\n" +
		"[]\n"
	if got != want || code != 0 {
		t.Fatalf("五种排序不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestListSortNonDecreasing 排完后每个位置都不大于其后位置，这是排序的核心不变量。
// 输入含"重复值与负数"，两者都容易触发归并的边界错误。
func TestListSortNonDecreasing(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [3, -1, 3, 0, -7, 12, -1, 0]
xs.sort()
let bad:int = 0
for (i:int in 1 until length(xs)) {
    if (xs[i] < xs[i - 1]) {
        bad = bad + 1
    }
}
console.print(bad, length(xs))
`)
	if got != "0 8\n" || code != 0 {
		t.Fatalf("排完还有逆序：got %q 码 %d", got, code)
	}
}

// TestListSortBumpsVersion 排序一次就是就地修改，正在遍历这个容器的循环下一轮抛 10033 号。
func TestListSortBumpsVersion(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [3, 1, 2]
for (v:int in xs) {
    xs.sort()
    console.print(v)
}
console.print("跑完了")
`)
	if code != 1 || !strings.Contains(got, "10033") || strings.Contains(got, "跑完了") {
		t.Fatalf("遍历途中排序该撞 10033 号：%q 码 %d", got, code)
	}
}

// TestIndexRuns 验证找位置：整数按 64 位比较、文本按内容比较（与 remove 同一规则），找不到都返回 -1。
func TestIndexRuns(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [4, 8, 15, 16, 23, 42]
console.print(xs.index_of(16), xs.index_of(99))
let ws:list<string> = ["甲", "乙", "丙"]
console.print(ws.index_of("丙"), ws.index_of("丁"))
let s:string = "第一段/第二段"
console.print(s.index_of("/"), s.index_of("没有"), s.index_of(""))
console.print(s.index_of("第二"), s.index_of("段"))
`)
	// 计的是码点不是字节：第二段从第 4 个码点起；"段"第一次命中在第 2 个位置。
	want := "3 -1\n2 -1\n3 -1 0\n4 2\n"
	if got != want || code != 0 {
		t.Fatalf("找位置不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStringEdges 验证前后缀判断：匹配为真、不匹配为假、空串在两端都算匹配。
func TestStringEdges(t *testing.T) {
	got, code := run(t, `
let s:string = "第一段/第二段"
console.print(s.starts_with("第一"), s.starts_with("第二段"), s.starts_with("第一第一段"))
console.print(s.ends_with("段"), s.ends_with("二段"), s.ends_with("第一"))
console.print(s.starts_with(""), s.ends_with(""))
let e:string = ""
console.print(e.starts_with(""), e.ends_with("甲"), e.starts_with("甲"))
console.print(s.starts_with(s), s.ends_with(s))
`)
	want := "true false false\ntrue true false\ntrue true\ntrue false false\ntrue true\n"
	if got != want || code != 0 {
		t.Fatalf("两头判断不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestDictKeysAndValues 键与值各返回一份 list。顺序本不保证，所以两份都排序后再比较；
// keys 返回的是拷贝，对它排序不影响原来的 dict。
func TestDictKeysAndValues(t *testing.T) {
	got, code := run(t, `
let d:dict<string, int> = {"b": 2, "a": 1, "c": 3}
let ks:list<string> = d.keys()
let vs:list<int> = d.values()
ks.sort()
vs.sort()
console.print(ks, vs)
console.print(length(d), d["a"], d["c"])
let back:list<int> = []
for (k:string in d.keys()) {
    back.add(d[k])
}
back.sort()
console.print(back)
let e:dict<int, string> = {7: "甲", 8: "乙"}
let ek:list<int> = e.keys()
let ev:list<string> = e.values()
ek.sort()
ev.sort()
console.print(ek, ev)
let empty:dict<string, int> = {}
console.print(length(empty.keys()), length(empty.values()))
`)
	// 值那一条：乙 E4 打头、甲 E7 打头，按字节排乙在前。
	want := "[\"a\", \"b\", \"c\"] [1, 2, 3]\n" +
		"3 1 3\n" +
		"[1, 2, 3]\n" +
		"[7, 8] [\"乙\", \"甲\"]\n" +
		"0 0\n"
	if got != want || code != 0 {
		t.Fatalf("键与值不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestListIndexOnMovedSlot 读走一个位置后该位置变为 null，index_of 不应错误地匹配它。
// 这里验证一件事：被读走后，原来的值再也找不到（返回 -1），后面的元素仍在。
func TestListIndexOnMovedSlot(t *testing.T) {
	got, code := run(t, `
let ws:list<string> = ["甲", "乙", "丙"]
let first:string = ws[0]
console.print(ws.index_of("甲"), ws.index_of("乙"), first)
`)
	want := "-1 1 甲\n"
	if got != want || code != 0 {
		t.Fatalf("读走一格之后找不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestMathSquareRoot 开方：正数照 IEEE、零还是零、负数返回 NaN（不抛）。
func TestMathSquareRoot(t *testing.T) {
	got, code := run(t, `
console.print(math.square_root(9.0), math.square_root(2.0))
console.print(math.square_root(0.0), math.square_root(0.0 - 1.0))
console.print(math.square_root(math.square_root(16.0)))
let side:float = math.square_root(25.0)
console.print(side, math.square_root(side * side))
`)
	want := "3.0 1.4142135623730951\n0.0 NaN\n2.0\n5.0 5.0\n"
	if got != want || code != 0 {
		t.Fatalf("开方不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestKeysValuesOfBigDict 四十个键必然触发再散列搬家；搬完之后再取一次 keys，
// 游标计算错误会在这里暴露。键按 UTF-8 字节排序，第一个是"键0"、最后一个是"键9"。
func TestKeysValuesOfBigDict(t *testing.T) {
	var src strings.Builder
	src.WriteString("let d:dict<string, int> = {}\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&src, "d.add(\"键%d\", %d)\n", i, i)
	}
	src.WriteString(`
let ks:list<string> = d.keys()
let vs:list<int> = d.values()
ks.sort()
vs.sort()
let sum:int = 0
for (v:int in vs) {
    sum = sum + v
}
console.print(length(ks), length(vs), sum)
console.print(ks[0], ks[39])
`)
	got, code := run(t, src.String())
	want := "40 40 780\n键0 键9\n" // 0+1+…+39 = 780
	if got != want || code != 0 {
		t.Fatalf("四十个键的dict不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

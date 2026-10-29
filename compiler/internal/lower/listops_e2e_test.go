// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// listops_e2e_test.go 覆盖六个 list 成员：反转、按下标插入、按下标移除、整只追加、取片段、
// 去重成 hashset。每一条都是实际运行：编译、启动子进程、比对打印出的文本。
// 哪类元素不被这些成员支持（句柄型元素）由 internal/check 的范围测试验证，这一份不覆盖。

func TestListReverse(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [3, 5, 9, 1]
xs.reverse()
console.print(xs[0], xs[1], xs[2], xs[3], length(xs))
let ss:list<string> = ["甲", "乙", "丙"]
ss.reverse()
console.print(ss)
let es:list<int> = []
es.reverse()
console.print(length(es))
let one:list<int> = [7]
one.reverse()
console.print(one[0])
`)
	want := "1 9 5 3 4\n[\"丙\", \"乙\", \"甲\"]\n0\n7\n"
	if got != want || code != 0 {
		t.Fatalf("list 翻个儿不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 反转是一次整体修改：正在遍历这个容器的循环，下一轮抛出 10033
	got, code = run(t, `
let xs:list<int> = [1, 2, 3]
for (v:int in xs) {
    xs.reverse()
}
`)
	if code != 1 || !strings.Contains(got, "10033）：遍历途中改了容器") {
		t.Fatalf("遍历途中翻个儿得到 %q 码 %d，要 10033 那一句", got, code)
	}
}

func TestListInsertAt(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [2, 3]
xs.insert_at(0, 1)
xs.insert_at(2, 9)
xs.insert_at(4, 10)
console.print(xs)
let ss:list<string> = ["乙"]
ss.insert_at(0, "甲")
console.print(ss, length(ss))
let es:list<int> = []
es.insert_at(0, 5)
console.print(es)
`)
	want := "[1, 2, 9, 3, 10]\n[\"甲\", \"乙\"] 2\n[5]\n"
	if got != want || code != 0 {
		t.Fatalf("按位置插不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	for _, which := range []string{"4", "-1", "0 - 1"} {
		got, code = run(t, `
let xs:list<int> = [1, 2, 3]
xs.insert_at(`+which+`, 9)
`)
		if code != 1 || !strings.Contains(got, "10031）：下标越界") {
			t.Fatalf("插到 %s 得到 %q 码 %d，要 10031 那一句", which, got, code)
		}
	}

	// 插进去的元素归那只 list：两万圈各插一头一尾峰值不增长，才能说明容量翻倍搬走的旧块也被释放
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let xs:list<string> = []
    xs.insert_at(0, "甲" + "乙")
    xs.insert_at(1, "丙" + "丁")
}
`); used > 2000 {
		t.Fatalf("两万圈各插两格，峰值 %d 字节：那一格里的文本或翻倍的旧块大概没拆", used)
	}
}

func TestListRemoveAt(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [1, 2, 3, 4]
console.print(xs.remove_at(0), xs.remove_at(1), length(xs))
console.print(xs)
let ss:list<string> = ["甲", "乙", "丙"]
let head:string = ss.remove_at(0)
console.print(head, ss)
let last:string = ss.remove_at(1)
console.print(last, length(ss))
`)
	want := "1 3 2\n[2, 4]\n甲 [\"乙\", \"丙\"]\n丙 1\n"
	if got != want || code != 0 {
		t.Fatalf("按位置删不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	for _, which := range []string{"3", "-1"} {
		got, code = run(t, `
let xs:list<int> = [1, 2, 3]
console.print(xs.remove_at(`+which+`))
`)
		if code != 1 || !strings.Contains(got, "10031）：下标越界") {
			t.Fatalf("删掉 %s 得到 %q 码 %d，要 10031 那一句", which, got, code)
		}
	}

	// 删走的那个位置归调用方、后面的元素前移；两万圈峰值不涨才说明前移这一步没有丢失归属
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let xs:list<string> = []
    xs.add("甲" + "乙")
    xs.add("丙" + "丁")
    let gone:string = xs.remove_at(0)
}
`); used > 2000 {
		t.Fatalf("两万圈各删走一格的文本，峰值 %d 字节：删走的那一份或挪过来的那一格大概没拆", used)
	}
}

func TestListAddAll(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [1, 2]
let ys:list<int> = [3, 4, 5]
xs.add_all(ys)
console.print(xs, length(ys))
let ss:list<string> = ["甲"]
let tt:list<string> = ["乙", "乙"]
ss.add_all(tt)
console.print(ss, length(tt))
let es:list<int> = []
es.add_all(ys)
console.print(es)
console.print(xs[0])
`)
	want := "[1, 2, 3, 4, 5] 3\n[\"甲\", \"乙\", \"乙\"] 2\n[3, 4, 5]\n1\n"
	if got != want || code != 0 {
		t.Fatalf("整只接到尾巴不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 来源容器一个位置都不改动，接过来的每个位置是自己的拷贝：两万圈各追加一次峰值不增长
	if used := heapAt(t, `
let src:list<string> = []
src.add("甲" + "乙")
for (i:int in 0 until 20000) {
    let xs:list<string> = []
    xs.add_all(src)
}
`); used > 2000 {
		t.Fatalf("两万圈各接一只文本 list，峰值 %d 字节：拷出来的那一份或目标的老块大概没拆", used)
	}
}

func TestListSlice(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [1, 2, 3, 4, 5]
let mid:list<int> = xs.slice(1, 4)
console.print(mid, length(mid))
console.print(xs.slice(0, 0), xs.slice(0, 5), xs.slice(4, 5))
let ss:list<string> = ["甲", "乙", "丙"]
let tail:list<string> = ss.slice(1, 3)
console.print(tail)
`)
	want := "[2, 3, 4] 3\n[] [1, 2, 3, 4, 5] [5]\n[\"乙\", \"丙\"]\n"
	if got != want || code != 0 {
		t.Fatalf("list 取一段不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	for _, pair := range [][2]string{{"-1", "2"}, {"1", "6"}, {"3", "2"}} {
		got, code = run(t, `
let xs:list<int> = [1, 2, 3]
console.print(xs.slice(`+pair[0]+`, `+pair[1]+`))
`)
		if code != 1 || !strings.Contains(got, "10031）：下标越界") {
			t.Fatalf("切 %s 到 %s 得到 %q 码 %d，要 10031 那一句", pair[0], pair[1], got, code)
		}
	}

	// 切出来的每个位置是自己的拷贝：源与结果各析构一次，两万圈峰值不增长
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let xs:list<string> = []
    xs.add("甲" + "乙")
    xs.add("丙" + "丁")
    let cut:list<string> = xs.slice(0, 2)
}
`); used > 2000 {
		t.Fatalf("两万圈各切一段文本，峰值 %d 字节：切出来的那两份大概没拆", used)
	}
}

func TestListToSet(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [1, 2, 2, 3, 3, 3]
let s:hashset<int> = xs.to_set()
console.print(length(s), s.contains(1), s.contains(2), s.contains(3), s.contains(4))
let ss:list<string> = ["甲", "甲", "乙"]
let t:hashset<string> = ss.to_set()
console.print(length(t), t.contains("甲"), t.contains("乙"), length(ss))
let es:list<int> = []
console.print(length(es.to_set()))
`)
	want := "3 true true true false\n2 true true 3\n0\n"
	if got != want || code != 0 {
		t.Fatalf("去重成一只表不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 重复的键检测到一次就跳过：那个位置不必拷出来；两万圈自己拷自己峰值不增长
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let xs:list<string> = []
    xs.add("甲" + "乙")
    xs.add("甲" + "乙")
    let s:hashset<string> = xs.to_set()
}
`); used > 2000 {
		t.Fatalf("两万圈各去重一次，峰值 %d 字节：跳过的那一格或新表里那一格大概没拆", used)
	}
}

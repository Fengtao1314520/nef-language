// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// 规则：打印空句柄时，五种引用类型给出同一个输出。
//
// 语言行为早已定为"打印空句柄不导致程序崩溃：该位置输出 `null`"，实现曾漏了两处：
// `console.print` 在交给 `@put_str` 前先拒绝空值（抛 10092），而 `list`／`dict`／`hashset`
// 三种在打印机里直接进"数位置、读长度"那段——产物按 0 加偏移后被操作系统终止（退出码 139）。
// 下面每一段都实际运行过，输出按实测结果填写。

// TestPrintEmptyHandleEveryKind 五种类型各一处，都印 `null`，程序正常走完。
func TestPrintEmptyHandleEveryKind(t *testing.T) {
	got, code := run(t, `class Bag {
    v:int
}
let xs:list<list<int>> = [[1, 2]]
let one:list<int> = xs[0]
console.print(xs[0], one)
let s:string = null
console.print(s, 1, true)
let bs:list<Bag> = [new Bag(v: 3)]
let b:Bag = bs[0]
console.print(bs[0], b)
let ds:list<dict<string, int>> = [{"甲": 4}]
let d:dict<string, int> = ds[0]
console.print(ds[0], d)
let hs:list<hashset<int>> = [{5, 6}]
let h:hashset<int> = hs[0]
console.print(hs[0], length(h))
`)
	want := "null [1, 2]\nnull 1 true\nnull Bag { v: 3 }\nnull {\"甲\": 4}\nnull 2\n"
	if got != want || code != 0 {
		t.Fatalf("印空句柄的形状：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintEmptyHandleInsideContainer 空句柄存在容器里也打印 `null`：整个容器仍完整输出。
func TestPrintEmptyHandleInsideContainer(t *testing.T) {
	got, code := run(t, `let xs:list<list<int>> = [[1], [2]]
let a:list<int> = xs[0]
console.print(xs)
let m:dict<string, list<int>> = {"甲": [1], "乙": [2]}
let b:list<int> = m["乙"]
console.print(m)
`)
	want := "[null, [2]]\n{\"甲\": [1], \"乙\": null}\n"
	if got != want || code != 0 {
		t.Fatalf("容器里那一格空了：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestPrintIsNotAPoint 验证打印不等于访问成员：空句柄能打印出 `null`，访问它的成员照旧抛 10092。
// 这一条确认该规则没有取消判空检查——`length`、成员方法、拼接一侧都仍有各自的检查。
func TestPrintIsNotAPoint(t *testing.T) {
	for _, one := range []struct {
		name string
		src  string
	}{
		{"数长度", "let s:string = null\nconsole.print(length(s))\n"},
		{"点成员", "let s:string = null\nconsole.print(s.trim())\n"},
		{"拼进去", "let s:string = null\nconsole.print(\"甲\" + s)\n"},
		{"当路径", "let s:string = null\nconsole.print(path.file_name(s))\n"},
	} {
		got, code := run(t, one.src)
		if got != "出错了（10092）：对着空句柄取成员或调用方法\n" || code != 1 {
			t.Fatalf("%s：这一处仍该挡在 10092，got %q 码 %d", one.name, got, code)
		}
	}
}

// TestPrintEmptyHandleSameOnBothPaths 直跑与产物两条路径结果一致。
// 被修复的正是产物路径：容器那三种在 v0.2.0 上是退出码 139，直跑同样 139。
func TestPrintEmptyHandleSameOnBothPaths(t *testing.T) {
	const src = `let xs:list<string> = ["甲"]
let s:string = xs[0]
console.print(xs[0], s)
`
	got, code := run(t, src)
	if got != "null 甲\n" || code != 0 {
		t.Fatalf("直跑那一条的形状不对：got %q 码 %d", got, code)
	}
	r := exetest.Run(t, build(t, src), "t.exe")
	if r.Stdout != "null 甲\n" || r.Code != 0 {
		t.Fatalf("产物与直跑不一样：got %q 码 %d", r.Stdout, r.Code)
	}
}

// TestPrintEmptyHandleKeepsHeap 验证每圈打印一次空句柄不多要内存：换出的那段文本每句都释放。
// 圈数取 200 与 5000 之差；`console.print` 现造的那一整段由释放窗口那一层归还。
func TestPrintEmptyHandleKeepsHeap(t *testing.T) {
	const tmpl = `let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i]]
    let one:list<int> = xs[0]
    console.print(xs[0])
    total = total + length(one)
}
console.print(total)
`
	got, code := run(t, strings.ReplaceAll(tmpl, "@N@", "3"))
	if got != "null\nnull\nnull\n3\n" || code != 0 {
		t.Fatalf("先核对输出：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, tmpl); per > 2 {
		t.Fatalf("每圈还留 %g 字节", per)
	}
}

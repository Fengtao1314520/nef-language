// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import "testing"

// 规则：覆盖 `class` 的引用类型字段时，被换下来的那一个当场析构，
// 与容器那个位置适用同一套逻辑。此前只有容器位置析构旧值，字段一侧每圈留 16 字节
// （`heap_e2e_test.go` 每圈增长表第 18 行），两套规则并存。
//
// 容器那个位置能直接析构，是因为读出来就是读走（之后那个位置返回空句柄，没有第二个持有者）。
// 字段读出来不算交出，所以析构前要比一次句柄：`b.inner = b.inner` 要析构的正是
// 即将住进去的那一块，比较相等就不析构。这比"新增一条语言规则拦住自赋值"代价小，
// 也顺带覆盖了以后可能出现的同类写法。

// TestFieldOverwriteFreesOld 运行"覆盖很多次再读那个位置"：析构旧值若多析构或漏析构，
// 要么内存当场被破坏（退出码不是 0），要么读出来的数值不对。
func TestFieldOverwriteFreesOld(t *testing.T) {
	got, code := run(t, `
class Row {
    n:int
}
class Box {
    inner:Row
    tag:string
}
let b:Box = new Box(inner: new Row(n: 1), tag: "头一份")
for (i:int in 0 until 200) {
    b.inner = new Row(n: i)
    b.tag = "第 " + number.to_string(i)
}
let last:Row = b.inner
console.print(last.n, length(b.tag))
`)
	if got != "199 5\n" || code != 0 {
		t.Fatalf("覆盖字段之后再读不对：\n got %q 码 %d\nwant %q", got, code, "199 5\n")
	}
}

// TestFieldSelfAssignKeepsValue 覆盖那道句柄比较：把字段里住着的那一个再写回同一个字段
// （`b.inner = b.inner`，以及方法体里 `inner = inner`），要析构的正是即将住进去的那一块。
// 这种情形目前在语言里构造不出来：顺着读那一次已按交出规则记成"接走"，字段先被抹空，没有旧块可析构；
// 即使删掉句柄比较这一支，这条与上面两条测试仍然是绿的。
// 保留这次比较是因为它只花一次比较的代价：一旦出现"从别处也持有同一块"的写法（例如
// 以后允许 `b.inner = c.inner` 且 `b` 与 `c` 是同一个对象），少了它就会析构正在住进去的对象。
func TestFieldSelfAssignKeepsValue(t *testing.T) {
	got, code := run(t, `
class Row {
    n:int
}
class Box {
    inner:Row
    method keepSelf() {
        inner = inner
    }
}
let b:Box = new Box(inner: new Row(n: 4))
b.inner = b.inner
let c:Row = new Row(n: 99)
console.print(b.inner.n, c.n)
b.keepSelf()
let d:Row = new Row(n: 7)
console.print(b.inner.n, d.n)
`)
	if got != "4 99\n4 7\n" || code != 0 {
		t.Fatalf("字段自己写回自己不拆坏：\n got %q 码 %d\nwant %q", got, code, "4 99\n4 7\n")
	}
}

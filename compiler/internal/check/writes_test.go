// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// writes_test.go 钉住"借出来的一份与整只容器一起交进去"这一条检查阶段的拒绝。
//
// 从前它要到运行期才炸：容器改写那一刻释放元素，借出去的那一份被收回，`@free_guarded`
// 撞上借用表（10097）。现在给每条函数算一份"哪个形参被改写"的摘要，调用处对一下实参表就能拒。
//
// en: writes_test.go pins the compile-time refusal of "a borrowed element handed in together with
// the whole container". It used to blow up only at runtime: rewriting the container released the
// element, the borrowed piece was taken back, and @free_guarded met the borrow table (10097). Each
// function now carries a summary of which parameters it rewrites, and a call site can refuse.
package check

import "testing"

const writesHead = `class Item {
    tag:string
}
`

func TestBorrowedElementWithContainerRefused(t *testing.T) {
	// 容器在前、借出来的一份在后
	expectErr(t, writesHead+`
method sink(xs:list<Item>, one:Item) {
    xs.clear()
}
let a:list<Item> = [new Item(tag: "甲")]
sink(a, a[0])
`, "a[...] 借出来的那一份与 a 一起交了进去，而被调方会改写它：那一份可能当场被收回")
	// 实参顺序反过来、被调方的形参顺序也跟着反过来：一样拒
	expectErr(t, writesHead+`
method sink(one:Item, xs:list<Item>) {
    xs[0] = new Item(tag: "乙")
}
let a:list<Item> = [new Item(tag: "甲")]
sink(a[0], a)
`, "a[...] 借出来的那一份与 a 一起交了进去，而被调方会改写它：那一份可能当场被收回")
	// dict 的 set 覆盖同样会释放旧值
	expectErr(t, writesHead+`
method sink(one:Item, table:dict<string, Item>) {
    table.set("k", new Item(tag: "乙"))
}
let d:dict<string, Item> = {"k": new Item(tag: "甲")}
sink(d["k"], d)
`, "d[...] 借出来的那一份与 d 一起交了进去，而被调方会改写它：那一份可能当场被收回")
}

func TestBorrowedElementWithReadOnlyCalleeAccepted(t *testing.T) {
	// 被调方只读：借出来的一份与容器一起交进去没有问题
	expectOK(t, writesHead+`
method show(xs:list<Item>, one:Item) {
    console.print(length(xs), one.tag)
}
let a:list<Item> = [new Item(tag: "甲")]
show(a, a[0])
`)
	// 两个不同的容器：改的是另一只，借出来那一份不受影响
	expectOK(t, writesHead+`
method sink(xs:list<Item>, one:Item) {
    xs.clear()
}
let a:list<Item> = [new Item(tag: "甲")]
let b:list<Item> = [new Item(tag: "乙")]
sink(b, a[0])
`)
}

// TestWriteSummarySeesForwardCalls 钉住转交那一层：`outer` 自己没写 `xs`，
// 但它把 `xs` 交给了会写的 `sink`，摘要也算它写。
func TestWriteSummarySeesForwardCalls(t *testing.T) {
	expectErr(t, writesHead+`
method sink(xs:list<Item>) {
    xs.clear()
}

method outer(xs:list<Item>, one:Item) {
    sink(xs)
}

let a:list<Item> = [new Item(tag: "甲")]
outer(a, a[0])
`, "a[...] 借出来的那一份与 a 一起交了进去，而被调方会改写它：那一份可能当场被收回")
}

// TestWriteSummaryForwardBeforeDefinition 定义写在调用之后也一样：摘要在查任何函数体之前算齐。
func TestWriteSummaryForwardBeforeDefinition(t *testing.T) {
	expectErr(t, writesHead+`
let a:list<Item> = [new Item(tag: "甲")]
sink(a, a[0])

method sink(xs:list<Item>, one:Item) {
    xs.clear()
}
`, "a[...] 借出来的那一份与 a 一起交了进去，而被调方会改写它：那一份可能当场被收回")
}

// TestBorrowedElementThroughField 钉住字段那一层：容器挂在 class 字段上时，
// `box` 与 `box.xs[0]` 不是同一个名字，但被调方写的正是那个字段，检查阶段也要拒。
// 摘要按形参的字段路径算：`b.xs[0] = v`、`b.xs.clear()` 都记 "xs"。
func TestBorrowedElementThroughField(t *testing.T) {
	const head = `class Node {
    tag:string
}
class Box {
    xs:list<Node>
    ys:list<Node>
}
`
	// 字段上的位置被覆盖：拒
	expectErr(t, head+`
method probe(b:Box, n:Node) {
    b.xs[0] = new Node(tag: "乙")
}
let box:Box = new Box(xs: [new Node(tag: "甲")], ys: [])
probe(box, box.xs[0])
`, "box[...] 借出来的那一份与 box 一起交了进去，而被调方会改写它：那一份可能当场被收回")
	// 字段上的 clear：拒
	expectErr(t, head+`
method drain(b:Box, n:Node) {
    b.xs.clear()
}
let box:Box = new Box(xs: [new Node(tag: "甲")], ys: [])
drain(box, box.xs[0])
`, "box[...] 借出来的那一份与 box 一起交了进去，而被调方会改写它：那一份可能当场被收回")
	// 容器本身就是字段：`box.xs` 与 `box.xs[0]` 也认得出来
	expectErr(t, head+`
method sink(xs:list<Node>, n:Node) {
    xs.clear()
}
let box:Box = new Box(xs: [new Node(tag: "甲")], ys: [])
sink(box.xs, box.xs[0])
`, "box[...] 借出来的那一份与 box 一起交了进去，而被调方会改写它：那一份可能当场被收回")
	// 误报护栏：写的是 ys，借出来的是 xs —— 两个不同字段，不能拒
	expectOK(t, head+`
method drain(b:Box, n:Node) {
    b.ys.clear()
}
let box:Box = new Box(xs: [new Node(tag: "甲")], ys: [])
drain(box, box.xs[0])
`)
	// 误报护栏：两个字段各自读一份、交给只改第一个字段的函数
	expectOK(t, head+`
method bump(b:Box, n:Node) {
    b.xs.clear()
}
let box:Box = new Box(xs: [new Node(tag: "甲")], ys: [new Node(tag: "乙")])
bump(box, box.ys[0])
`)
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 规则：借出去的那一块，在借着的时候不能收回。
//
// 借用来自实参、方法接收者、遍历变量与读一个位置。借着的这一块要是被覆盖位置、
// 置空字段、`clear` 那一族写法当场释放，随后通过那个借用的名字读写就是读已经归还的内存。
// 实测（本机 `nf.exe`，声明都在使用之前、参数不与外层名字重名）：被借出的那个对象在
// `nodes[0] = build("BBBB")` 这一句释放，紧接着一笔申请复用同一地址，两句打印同值。
// 运行期检查按线程一份的借用表走，命中抛 10097。
//
// en: A block handed out as a borrow must not be released while it is borrowed. The borrow
// comes from an argument, a receiver, a loop variable, or a position read; releasing that block
// - by overwriting its container cell, clearing the container, or the like - leaves the borrower
// reading memory already returned to the heap.

// 容器挂在一个 class 的字段上：`box.xs` 与 `box.xs[0]` 不是同一个名字，但字段路径摘要
// 认得出"被调方写的正是那个字段"，所以这一条在检查阶段就被拒；运行期那道护栏仍是
// 跨包、方法、以及摘要看不到的写法的默认处理。
//
// en: the container hangs on a class field: `box.xs` and `box.xs[0]` are not the same name, but
// the field-path summary sees that the callee rewrites that very field, so this is refused during
// checking; the runtime guard still backs up cross-package calls, methods and unseen writes.
const borrowFreeProbe = `class Node {
    tag:string
}

class Box {
    xs:list<Node>
}

method make_tag(t:string):string { return t.slice(0, 4) }

method build(t:string):Node { return new Node(tag: make_tag(t)) }

method probe(b:Box, n:Node) {
    b.xs[0] = build("BBBB")
    console.print("n.tag:", n.tag)
}

let box:Box = new Box(xs: [build("AAAA")])
probe(box, box.xs[0])
`

// TestBorrowedBlockNotReleasedRefused 借出去的那一块与整只对象一起交进去、被调方改写那个字段：
// 检查阶段就拒，不必等运行期撞借用表。
func TestBorrowedBlockNotReleasedRefused(t *testing.T) {
	if err := compileErr(borrowFreeProbe); err == nil ||
		!strings.Contains(err.Error(), "box[...] 借出来的那一份与 box 一起交了进去") {
		t.Fatalf("字段上的借出+改写该在检查阶段拒：err=%v", err)
	}
}

// TestBorrowReleasedAfterScopeEndsQuietly 借出去的那一块没被覆盖，用完照常归还，不报错。
//
// 这一条钉的是误报：借用表里留着别的帧的句柄，或者收回的并不是借出去的那一块时，
// 正常程序不能被拦下。
func TestBorrowReleasedAfterScopeEndsQuietly(t *testing.T) {
	got, code := run(t, `class Node {
    tag:string
}

method show(n:Node):string { return n.tag }

let box:list<Node> = [new Node(tag: "甲"), new Node(tag: "乙")]
console.print(show(box[0]), show(box[1]), length(box))
`)
	if got != "甲 乙 2\n" || code != 0 {
		t.Fatalf("只是读一读，谁都不该报错：\n got %q 码 %d\nwant \"甲 乙 2\\n\"", got, code)
	}
}

// TestBorrowedBlockClearedRefused `clear` 那一条收回路径同样在检查阶段拦下：
// `b.xs.clear()` 记成字段 "xs" 的改写，调用处 `drain(bag, bag.xs[0])` 当场拒。
// 文本那一档走的是另一条路：交进去时那块就标成共用，`clear` 归还是空操作，所以不拦
// （`TestSharedTextSurvivesClear` 钉那一半）。
//
// en: the clear path is refused during checking too: b.xs.clear() records a write of field "xs",
// so drain(bag, bag.xs[0]) is refused at the call site. Text goes the other way: the block carries
// the shared mark, so clearing is a no-op - TestSharedTextSurvivesClear pins that half.
func TestBorrowedBlockClearedRefused(t *testing.T) {
	if err := compileErr(`class Tag {
    s:string
}

class Bag {
    xs:list<Tag>
}

method drain(b:Bag, one:Tag) {
    b.xs.clear()
    console.print(one.s)
}

let bag:Bag = new Bag(xs: [new Tag(s: "甲"), new Tag(s: "乙")])
drain(bag, bag.xs[0])
`); err == nil || !strings.Contains(err.Error(), "bag[...] 借出来的那一份与 bag 一起交了进去") {
		t.Fatalf("字段上的 clear 该在检查阶段拒：err=%v", err)
	}
}

// TestSharedTextSurvivesClear 文本交进被调方之后被 `clear` 收回：那一块标着共用，归还它是空操作，
// 被调方手里那一个名字读到的还是原来那一份。
//
// en: text passed into a callee and then cleared by the container: that block carries the shared
// mark, returning it is a no-op, and the callee's name still reads the original value.
func TestSharedTextSurvivesClear(t *testing.T) {
	got, code := run(t, `class BagS {
    xs:list<string>
}

method drain(b:BagS, one:string) {
    b.xs.clear()
    console.print(one)
}

let bag:BagS = new BagS(xs: ["甲" + "", "乙" + ""])
drain(bag, bag.xs[0])
`)
	if got != "甲\n" || code != 0 {
		t.Fatalf("共用的文本被 clear 收回之后读坏了：\n got %q 码 %d\nwant %q\n", got, code, "甲\n")
	}
}

// TestStaticLiteralBorrowIsNotGuarded 借用表里同一个地址来自写死的那一段字面量时不算借出。
//
// 库里的真实形状：`press_key(key_name)` 把一段字面量借进去，再交给 `key_by_name(name)`，
// 那一帧把同一段字面量存进局部量。帧尾归还那一个局部量时，外面那一帧的借用还挂在表上，
// 两个地址是同一段字面量，护栏于是拦下了一次本来就不会归还任何东西的释放——
// `@free` 对着头里尺寸是 0 的地址走的是空。
//
// en: The same address coming from baked-in text is not a borrow. The library's real shape:
// press_key(key_name) borrows a literal in, then hands it to key_by_name(name), and that
// frame stores the same literal into a local. When the frame tail releases the local the outer
// frame's entry is still in the table, both addresses are the same literal, and the guard stops a
// release that returns nothing anyway - @free passes an address whose header size is zero.
func TestStaticLiteralBorrowIsNotGuarded(t *testing.T) {
	got, code := run(t, `method key_by_name(name:string) {
    let lowered:string = name.to_lower()
    let label:string = ""
    if (lowered == "enter") {
        label = "Enter"
    }
    console.print(label)
}

method press_key(key_name:string) {
    key_by_name(key_name)
}

press_key("Enter")
`)
	if got != "Enter\n" || code != 0 {
		t.Fatalf("字面量走了借用表这一趟：\n got %q 码 %d\nwant \"Enter\\n\"，退出码 0\n", got, code)
	}
}

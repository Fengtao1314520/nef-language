// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestFieldReadIntoNameIsHandover 钉住那一条留着的那一半：把 `class` 的可变引用类型字段读进一个
// 名字，这一次读就是交出，那一个字段读完空着。
// 顺着读一次不动那一个位置：`h.leaf.v` 连着两读都在。
//
// en: the half of that ruling that stays - reading a `class` mutable reference-typed field into a name
// IS the handover, and the field reads back empty (The narrowed rule moved only the text tier to
// sharing). A look-through leaves the position alone: two reads of `h.leaf.v` both get the value.
func TestFieldReadIntoNameIsHandover(t *testing.T) {
	got, code := run(t, `
class Leaf {
    v:int
}
class Hold {
    leaf:Leaf
    n:int
}
let h:Hold = new Hold(leaf: new Leaf(v: 7), n: 3)
console.print(h.leaf.v, h.leaf.v + 1, h.leaf == null, h.n)
`)
	want := "7 8 false 3\n"
	if got != want || code != 0 {
		t.Fatalf("同一个字段读三次都该拿得到：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 接进一个名字：那一个位置交出，读完空着，手里那一份是唯一的持有者
	got, code = run(t, `
class Leaf {
    v:int
}
class Hold {
    leaf:Leaf
}
let h:Hold = new Hold(leaf: new Leaf(v: 7))
let b:Leaf = h.leaf
console.print(b.v, h.leaf == null)
`)
	if want := "7 true\n"; got != want || code != 0 {
		t.Fatalf("字段那一份接进名字之后那一个位置该空着：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 方法体里直接写的字段名就是 this.那个字段，同一条
	got, code = run(t, `
class Leaf {
    v:int
}
class Hold {
    leaf:Leaf

    method take_out():Leaf {
        return leaf
    }
}
let h:Hold = new Hold(leaf: new Leaf(v: 9))
let out:Leaf = h.take_out()
console.print(out.v, h.leaf == null)
`)
	if want := "9 true\n"; got != want || code != 0 {
		t.Fatalf("方法体里把字段 return 出去同一条：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 文本字段：共用那一块，两处读到的还是同一块
	text, tc := run(t, `
class Note {
    name:string
}
let n:Note = new Note(name: "甲")
let a:string = n.name
console.print(a, n.name, n.name == null)
`)
	if tc != 0 || strings.TrimSpace(text) != "甲 甲 false" {
		t.Fatalf("文本字段那一读该照样留着：got %q 码 %d", text, tc)
	}

	// 每轮增长：一圈把字段读进名字、块尾由那个名字归还，不该留下东西。
	// heapSlope 会把 NEF_HEAP 设进这一条测试的子进程，跑完再比对别的输出就多一行，所以放最后。
	tmpl := `class Leaf {
    v:int
}
class Hold {
    leaf:Leaf
}
let total:int = 0
for (i:int in 0 until @N@) {
    let h:Hold = new Hold(leaf: new Leaf(v: i))
    let b:Leaf = h.leaf
    total = total + b.v
}
console.print(total)
`
	if slope := heapSlope(t, tmpl); slope >= 8 {
		t.Errorf("每轮还留 %g 字节：字段里那一份接进名字之后没人归还", slope)
	}
}

// TestFieldReadKeepsValueTypes 说明这条规则的另一半：值类型字段仍是普通拷贝，读几次都在。
// struct 的字段也不在这条规则里——struct 是值类型，读它的位置是整份拷贝，不是那个位置交出。
func TestFieldReadKeepsValueTypes(t *testing.T) {
	got, code := run(t, `
class Counter {
    n:int
    flag:boolean
}
let c:Counter = new Counter(n: 5, flag: true)
let a:int = c.n
let b:int = c.n
console.print(a, b, c.n)

struct Tag {
    name:string
}
let t:Tag = Tag { name: "甲" }
let x:string = t.name
let y:string = t.name
console.print(x, y, t.name)
`)
	want := "5 5 5\n甲 甲 甲\n"
	if got != want || code != 0 {
		t.Fatalf("值类型字段与 struct 字段不该被读走：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFieldOverwriteFreesOld 覆盖字段那一步照旧析构换下来的旧值：那一个位置是它那一块的
// 唯一出处（读出来只是借看一眼，谁也带不走），所以当场拆掉不会拆到别人持有的内存块。
//
// en: overwriting a field still destroys the value it replaces: that position is the block's only
// source - a reading is only a look, nobody carries it away - so dismantling it on the spot cannot
// touch a block somebody else holds.
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
console.print(b.inner.n, length(b.tag))
`)
	if got != "199 5\n" || code != 0 {
		t.Fatalf("覆盖字段之后再读不对：\n got %q 码 %d\nwant %q", got, code, "199 5\n")
	}
}

// TestFieldSelfAssignKeepsOneHolder 钉自赋值（`b.inner = b.inner`、方法体里 `inner = inner`）
// 不多出一个持有者：等号右边那一次读记成交出，那一个位置当场抹空，读出来的那一份再写回同一个位置——
// 一圈下来那一个位置仍只有一份持仓，块尾也只析构一次。
// 从前这一句要在发码那一步比一次句柄才防得住；改之前它是"那一个位置与手里那一份各留一份"。
//
// en: self-assignment adds no holder. The read on the right is booked as the handover, so the
// position is written empty and what was read goes back into the same position - one holding
// through the round, one destruction at the end. This used to need a handle comparison where the
// code is emitted; before that, the position and the value read out each kept a copy.
func TestFieldSelfAssignKeepsOneHolder(t *testing.T) {
	const pre = "class Row {\n    n:int\n}\nclass Box {\n    inner:Row\n}\nlet b:Box = new Box(inner: new Row(n: 4))\n"
	got, code := run(t, pre+"b.inner = b.inner\nconsole.print(b.inner.n)\n")
	if got != "4\n" || code != 0 {
		t.Fatalf("自赋值之后再读不对：\n got %q 码 %d\nwant %q", got, code, "4\n")
	}
	if slope := heapSlope(t, `class Row {
    n:int
}
class Box {
    inner:Row
}
let total:int = 0
for (i:int in 0 until @N@) {
    let b:Box = new Box(inner: new Row(n: i))
    b.inner = b.inner
    total = total + b.inner.n
}
console.print(total)
`); slope >= 8 {
		t.Errorf("每轮还留 %g 字节：自赋值那一圈多出一个持有者", slope)
	}
}

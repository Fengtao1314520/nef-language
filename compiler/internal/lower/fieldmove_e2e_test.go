// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestFieldReadMovesOut 验证：读 `class` 的引用类型字段就是**交出**，读完那个字段返回 `null`，
// 与容器那个位置同一套规则。修改之前同一个字段读两次会拿到两个都能写的名字，
// 而首页与第 5 课写着"一个值同时只有一个名字能指它"。
func TestFieldReadMovesOut(t *testing.T) {
	got, code := run(t, `
class Leaf {
    v:int
}
class Hold {
    leaf:Leaf
    n:int
}
let a:Leaf = new Leaf(v: 7)
let h:Hold = new Hold(leaf: a, n: 3)
let b:Leaf = h.leaf
console.print(h.leaf == null, b.v, h.n)
`)
	want := "true 7 3\n"
	if got != want || code != 0 {
		t.Fatalf("读走字段之后那个字段该是 null：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 第二次读拿到的是 null，再访问它的成员就是 10092 号
	second, sc := run(t, `
class Leaf {
    v:int
}
class Hold {
    leaf:Leaf
}
let h:Hold = new Hold(leaf: new Leaf(v: 7))
let b:Leaf = h.leaf
let c:Leaf = h.leaf
console.print(c.v)
`)
	if sc != 1 || !strings.Contains(second, "出错了（10092）：对着空句柄取成员或调用方法") {
		t.Fatalf("同一个字段读第二次该拿到 null：got %q 码 %d", second, sc)
	}

	// 文本字段遵循同一规则：读走之后那个字段是 null
	text, tc := run(t, `
class Note {
    name:string
}
let n:Note = new Note(name: "甲")
let a:string = n.name
console.print(a, n.name == null)
`)
	if tc != 0 || strings.TrimSpace(text) != "甲 true" {
		t.Fatalf("文本字段该照样读走：got %q 码 %d", text, tc)
	}
}

// TestFieldReadKeepsValueTypes 说明这条规则不适用的另一半：值类型字段仍是普通拷贝，读几次都在。
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

// TestFieldMoveIntoReturn 覆盖这条规则带来的归属结果：方法 `return leaf` 交出去之后，
// 字段已写空，源头不再持有它，所以接住返回值的那一端是唯一持有者——
// 此前为"字段里的句柄要留着源头"保留的 104 字节／圈从此归零（每圈分配量表里那一行跟着变）。
func TestFieldMoveIntoReturn(t *testing.T) {
	got, code := run(t, `
class Leaf {
    v:int
}
class Holder {
    leaf:Leaf

    method take():Leaf {
        return leaf
    }
}
let h:Holder = new Holder(leaf: new Leaf(v: 4))
let out:Leaf = h.take()
console.print(out.v, h.leaf == null)
`)
	want := "4 true\n"
	if got != want || code != 0 {
		t.Fatalf("return 字段就是把那一格交出去：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

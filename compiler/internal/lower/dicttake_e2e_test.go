// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 字典那一个位置摘出来的写法：字典的值位置上一份"摘出来"的写法。
//
// 读一个位置交出的是只读借用，所以 `let one:C = d["甲"]` 接不住那一份；
// 清单上有 `remove_at(位置)`、树上有 `json.take`，字典那一个位置从前只能读、不能留下。
// `d.take(键)` 补的就是这一处：键在的时候把值交给接住它的名字，桶里那一个位置读回来是 `null`，
// 那条记录本身留在容器里；键不在就抛现成的 10032，那一步排在写空之前，摘不走半份。
//
// en: Item 122 - a take-out spelling for a dict's value position. Reading a position hands back a
// read-only borrow (item 111), so `let one:C = d[key]` cannot keep that value, and while a list has
// remove_at and a tree has json.take a dict had no way to keep one. d.take is that way: with the
// key present the value goes to the name that receives it and the bucket position reads back as
// null, while the entry itself stays in the container; a missing key throws the existing 10032,
// which happens before anything is cleared, so nothing is half taken.

// TestDictTakeHandsTheValueOver 摘出来那一份归接住它的名字，桶里那个位置读回 null。
func TestDictTakeHandsTheValueOver(t *testing.T) {
	got, code := run(t, `class Note {
    body:string
}

let d:dict<string, Note> = {}
d.add("甲", new Note(body: "第一段"))
d.add("乙", new Note(body: "第二段"))
let one:Note = d.take("甲")
console.print(one.body, d.contains("甲"), length(d), d.get("甲", new Note(body: "补")), d["乙"].body)
`)
	if code != 0 {
		t.Fatalf("摘出来那一步：\n got %q 码 %d\nwant 退出码 0\n", got, code)
	}
	want := "第一段 true 2 null 第二段\n"
	if got != want {
		t.Fatalf("摘出来那一步：\n got %q\nwant %q\n", got, want)
	}
}

// TestDictTakeMissingKeyThrows 键不在抛 10032，容器一个字也不动。
func TestDictTakeMissingKeyThrows(t *testing.T) {
	got, code := run(t, `class Note {
    body:string
}

let d:dict<string, Note> = {}
d.add("甲", new Note(body: "第一段"))
try {
    let one:Note = d.take("丙")
    console.print("走不到这一句", one.body)
} catch (c, m, at) {
    console.print(c, m, length(d))
}
`)
	if code != 0 || !strings.Contains(got, "10032") || !strings.Contains(got, " 1") {
		t.Fatalf("缺键那一条：\n got %q 码 %d\nwant 话里有 10032，容器还剩一条\n", got, code)
	}
}

// TestDictTakeEndsNoDoubleRelease 摘走之后容器自己结束，那一个位置已经是空的，不许拆第二次。
//
// 这一条钉的是写空那一步：桶里那一个位置留着摘走之前的句柄时，容器析构与名字的帧尾归还
// 会拆同一块两次。每一圈造一个 Note、摘出来、名字到圈尾归还，净增长应当是零。
func TestDictTakeEndsNoDoubleRelease(t *testing.T) {
	const tmpl = `class Note {
    body:string
}

let d:dict<string, Note> = {}
for (i:int in 0 until @N@) {
    d.add("甲", new Note(body: "第一段"))
    let one:Note = d.take("甲")
    if (one.body == null) {
        console.print("接不住")
    }
}
`
	if grew := heapSlope(t, tmpl); grew != 0 {
		t.Fatalf("摘出来那一步每圈遗留 %.1f 字节（要 0：那一份由名字的圈尾归还，桶里那一个位置已经空着）\n", grew)
	}
}

// TestDictTakeOnValueTypeIsRefused 值类型那一个位置没有"持仓"可摘：编译期拒绝。
//
// `d["甲"]` 读一次就在自己手上（那是拷贝），要那条记录不在，写 `remove`。这一条落在检查阶段，
// 所以只编译，不运行。
func TestDictTakeOnValueTypeIsRefused(t *testing.T) {
	src := "let d:dict<string, int> = {\"甲\": 1}\nlet n:int = d.take(\"甲\")\n"
	if err := compileErr(src); err == nil {
		t.Fatalf("值类型那一个位置不该有摘出来的写法：\n %s 编译通过了\n", src)
	}
}

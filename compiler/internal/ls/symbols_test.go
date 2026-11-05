// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// symbols_test.go 钉大纲那一条：顶层的类型、函数与常量列不列得出，身上的字段与方法排不排得进去，
// 一条声明的结束行有没有走到下一项开始之前，以及 initialize 有没有把这一条本事报给编辑器。
//
// en: symbols_test.go pins the outline: whether top-level types, functions and constants are
// listed, fields and methods nest under them, a declaration's range runs up to where the next item
// starts, and whether initialize advertises the capability.
package ls

import (
	"encoding/json"
	"testing"
)

const outlineSample = `class Note {
    body:string
    method shout():string {
        return this.body
    }
}

struct Point {
    x:int
    y:int
}

enum Color {
    red = 0,
    green
}

method twice(n:int):int {
    return n * 2
}

const limit:int = 3
`

func TestOutlineListsDeclarationsAndMembers(t *testing.T) {
	syms := Outline("p.nf", outlineSample)
	if len(syms) != 5 {
		t.Fatalf("顶层该有五条（Note、Point、Color、twice、limit），得到 %d 条：%v", len(syms), namesOf(syms))
	}
	names := namesOf(syms)
	for i, want := range []string{"Note", "Point", "Color", "twice", "limit"} {
		if names[i] != want {
			t.Fatalf("第 %d 条该是 %s，得到 %v", i+1, want, names)
		}
	}
	if syms[0].Kind != symKind.class {
		t.Errorf("class 该报 SymbolKind 5，得到 %d", syms[0].Kind)
	}
	if syms[1].Kind != symKind.structKind {
		t.Errorf("struct 该报 SymbolKind 23，得到 %d", syms[1].Kind)
	}
	if syms[4].Kind != symKind.constant {
		t.Errorf("const 该报 SymbolKind 14，得到 %d", syms[4].Kind)
	}
	if got := namesOf(syms[0].Children); len(got) != 2 || got[0] != "body" || got[1] != "shout" {
		t.Errorf("Note 身上的字段与方法没排进去：%v", got)
	}
	if syms[0].Children[1].Kind != symKind.method {
		t.Errorf("方法该报 SymbolKind 6，得到 %d", syms[0].Children[1].Kind)
	}
	if got := namesOf(syms[2].Children); len(got) != 2 || got[0] != "red" {
		t.Errorf("枚举成员没排进去：%v", got)
	}
}

func TestOutlineRangesCoverTheWholeDeclaration(t *testing.T) {
	syms := Outline("p.nf", outlineSample)
	by := map[string]DocumentSymbol{}
	for _, s := range syms {
		by[s.Name] = s
	}
	// class Note 从第 1 行走到 struct Point 之前的空行（第 6 行），折起来正好罩住这一身。
	if n := by["Note"].Range; n.Start.Line != 0 || n.End.Line < 5 {
		t.Errorf("Note 的范围没罩住整条：%+v", n)
	}
	// 名字那一段要落在写名字的那一列上，大纲的跳转与重命名按它定位。
	sel := by["twice"].SelectionRange
	if sel.Start.Line != 17 || sel.Start.Character != 7 {
		t.Errorf("twice 的名字该在第 18 行第 8 列，得到 %+v", sel)
	}
	// 身上的方法占的是它自己那一段，不能一路走到文件末尾。
	shout := by["Note"].Children[1]
	if r := shout.Range; r.Start.Line != 2 || r.End.Line > 4 {
		t.Errorf("shout 的范围越出了 class：%+v", r)
	}
}

func TestOutlineKeepsMultiLineValuesInside(t *testing.T) {
	const text = `let rows:list<int> = [
    1,
    2,
]
console.print(length(rows))
`
	syms := Outline("p.nf", text)
	if len(syms) != 1 {
		t.Fatalf("顶层只有一条 let，得到 %v", namesOf(syms))
	}
	if r := syms[0].Range; r.Start.Line != 0 || r.End.Line < 3 {
		t.Errorf("跨行的字面量没被整条圈进来：%+v", r)
	}
}

func TestOutlineRecoversUnfinishedBrackets(t *testing.T) {
	// 括号还没闭合是打字打到一半的常态：补上右括号再算，大纲不该整片消失。
	syms := Outline("p.nf", "class Note {\n")
	if len(syms) != 1 || syms[0].Name != "Note" {
		t.Fatalf("没闭合的 class 该照样列出来，得到 %+v", namesOf(syms))
	}
	// 补不出来的那一种（字符串没有收尾）就答空：那时屏幕上没有可靠的结构可列。
	if syms := Outline("p.nf", "let a:string = \"甲\n"); syms != nil {
		t.Errorf("字符串没收尾就该答空，得到 %v", namesOf(syms))
	}
}

func TestServeAnswersDocumentSymbols(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+
			`","languageId":"nf","version":1,"text":"method twice(n:int):int {\n    return n * 2\n}\n"}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":"`+uri+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var res initResult
	if err := json.Unmarshal(replyOf(t, msgs, "1").Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	if !res.Capabilities.DocumentSymbolProvider {
		t.Error("initialize 没答应 documentSymbolProvider：编辑器就不会问大纲")
	}
	var syms []DocumentSymbol
	if err := json.Unmarshal(replyOf(t, msgs, "4").Result, &syms); err != nil {
		t.Fatalf("大纲的答复形状不对：%v", err)
	}
	if len(syms) != 1 || syms[0].Name != "twice" {
		t.Fatalf("答复里没有 twice：%+v", syms)
	}
}

func namesOf(syms []DocumentSymbol) []string {
	out := make([]string, 0, len(syms))
	for _, s := range syms {
		out = append(out, s.Name)
	}
	return out
}

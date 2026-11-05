// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// tokens_test.go 钉语义着色那一条：声明出来的名字、参数、字段、调用点与读到的字段各报哪一档，
// 关键字与内建名字一律不报（那一档由语法文件负责），以及 initialize 的图例与序号是不是同一份约定。
//
// en: tokens_test.go pins semantic colours: which kind a declared name, a parameter, a field, a
// call site and a field read each report, that keywords and built-in names are never reported (the
// grammar owns those), and that initialize's legend and the numbers sent share one convention.
package ls

import (
	"encoding/json"
	"testing"
)

const tokensSample = `class Note {
    body:string
    method shout():string {
        return this.body
    }
}

method twice(n:int):int {
    return n * 2
}

let rows:list<int> = [1]
console.print(twice(1), rows)
`

// oneToken 是一条解码后的记号：位置、长度、种类与附加属性。
type oneToken struct {
	line, char, length, kind, modifiers int
}

// decodeTokens 把协议里的差值串还原成绝对位置，方便一条条比对。
//
// en: decodeTokens turns the protocol's deltas back into absolute positions so each token can be
// compared one by one.
func decodeTokens(t *testing.T, data []int) []oneToken {
	t.Helper()
	if len(data)%5 != 0 {
		t.Fatalf("语义着色的数据不是五个数一组：%d 个数", len(data))
	}
	out := make([]oneToken, 0, len(data)/5)
	line, char := 0, 0
	for i := 0; i < len(data); i += 5 {
		line += data[i]
		if data[i] == 0 {
			char += data[i+1]
		} else {
			char = data[i+1]
		}
		out = append(out, oneToken{line, char, data[i+2], data[i+3], data[i+4]})
	}
	return out
}

func (o oneToken) at(line, char, kind int) bool {
	return o.line == line && o.char == char && o.kind == kind
}

func TestSemanticTokensClassifyNames(t *testing.T) {
	list := decodeTokens(t, SemanticTokens("p.nf", tokensSample))
	wants := []struct {
		what               string
		line, char, kind   int
		wantDeclarationMod bool
	}{
		{"class 的名字", 0, 6, tokType.class, true},
		{"字段的名字", 1, 4, tokType.property, true},
		{"方法的名字", 2, 11, tokType.method, true},
		{"函数名", 7, 7, tokType.function, true},
		{"参数名", 7, 13, tokType.parameter, true},
		{"顶层 let 的名字", 11, 4, tokType.variable, true},
	}
	for _, w := range wants {
		var hit *oneToken
		for i := range list {
			if list[i].at(w.line, w.char, w.kind) {
				hit = &list[i]
			}
		}
		if hit == nil {
			t.Errorf("%s 没报成第 %d 档（第 %d 行第 %d 列）：%+v", w.what, w.kind, w.line+1, w.char+1, list)
			continue
		}
		declared := hit.modifiers&modDeclaration != 0
		if declared != w.wantDeclarationMod {
			t.Errorf("%s 的 declaration 属性不对：%+v", w.what, hit)
		}
	}
}

func TestSemanticTokensClassifyUses(t *testing.T) {
	list := decodeTokens(t, SemanticTokens("p.nf", tokensSample))
	// 调用点上被叫的名字：第 12 行 `twice(1)` 里那一个，从第 14 列起。
	var callHit *oneToken
	for i := range list {
		if list[i].at(12, 14, tokType.function) {
			callHit = &list[i]
		}
	}
	if callHit == nil {
		t.Fatalf("调用点上的 twice 没报成 function：%+v", list)
	}
	if callHit.modifiers&modDeclaration != 0 {
		t.Errorf("调用点不是声明，不该带 declaration：%+v", callHit)
	}
	// `this.body` 里读到的字段：第 4 行 `        return this.body` 里 body 从第 21 列起。
	var fieldHit *oneToken
	for i := range list {
		if list[i].at(3, 20, tokType.property) {
			fieldHit = &list[i]
		}
	}
	if fieldHit == nil {
		t.Fatalf("this.body 里的 body 没报成 property：%+v", list)
	}
	// `console.print(` 里的那个动作：点号后面接括号，报 method（第 13 行第 9 列起）。
	var printHit *oneToken
	for i := range list {
		if list[i].at(12, 8, tokType.method) {
			printHit = &list[i]
		}
	}
	if printHit == nil {
		t.Fatalf("console.print 的 print 没报成 method：%+v", list)
	}
}

// TestSemanticTokensAskTheCheckerForMembers 钉"点号后面那一个名字问检查器"：方法就是方法、
// 字段就是字段，形状不参与判断。`p.body("乙")` 那一句编译不过（字段不能加括号调用），
// 检查器认出来的仍然是字段，屏幕上也就是字段那一档——形状判法在这里会判错。
//
// en: the name after a dot comes from the checker: a method is a method, a field is a field, and
// appearance plays no part. The line p.body("乙") does not compile (a field cannot be called), yet
// the checker still resolves body as a field, so the editor paints a field - the case where
// classifying by shape gets it wrong.
func TestSemanticTokensAskTheCheckerForMembers(t *testing.T) {
	const src = `class Note {
    body:string
    method shout():string {
        return this.body
    }
}

enum Color { red = 1, blue = 2 }

let p:Note = new Note(body: "甲")
let c:Color = Color.red
p.body("乙")
console.print(p.shout(), p.body, c)
`
	list := decodeTokens(t, SemanticTokens("p.nf", src))
	wants := []struct {
		what       string
		line, char int
		kind       int
	}{
		{"自己写的方法的调用点", 12, 16, tokType.method},
		{"读出来的字段", 12, 27, tokType.property},
		{"字段后面写了左括号", 11, 2, tokType.property},
		{"枚举的成员", 10, 20, tokType.enumMember},
	}
	for _, w := range wants {
		var hit *oneToken
		for i := range list {
			if list[i].at(w.line, w.char, w.kind) {
				hit = &list[i]
			}
		}
		if hit == nil {
			t.Errorf("%s 没报成第 %d 档（第 %d 行第 %d 列）：%+v", w.what, w.kind, w.line+1, w.char+1, list)
		}
	}
}

func TestSemanticTokensLeaveBuiltinsToTheGrammar(t *testing.T) {
	list := decodeTokens(t, SemanticTokens("p.nf", tokensSample))
	for _, o := range list {
		if o.line == 11 && o.char == 9 { // `list` 那一个内建类型名（第 12 行 `let rows:list<int>`）
			t.Errorf("内建类型名该由语法文件着色，语义着色多报了一处：%+v", o)
		}
		if o.line == 12 && o.char == 0 { // 组名 console：语法文件那一档管它
			t.Errorf("组名 console 不该被报成函数：%+v", o)
		}
	}
}

func TestSemanticTokensOnUnfinishedText(t *testing.T) {
	// 括号没闭合：补上右括号照样报已经写下来的那一些名字。
	if data := SemanticTokens("p.nf", "class Note {\n"); len(data) != 5 {
		t.Errorf("没闭合的 class 也该报出 Note 这一档，得到 %+v", data)
	}
	// 字符串没有收尾：补不出来，整片不报，由语法文件那一套颜色负责。
	if data := SemanticTokens("p.nf", "let a:string = \"甲\n"); data != nil {
		t.Errorf("字符串没收尾就该答空，得到 %+v", data)
	}
}

func TestServeAnswersSemanticTokens(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+
			`","languageId":"nf","version":1,"text":"method twice(n:int):int {\n    return n * 2\n}\n"}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/semanticTokens/full","params":{"textDocument":{"uri":"`+uri+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var res initResult
	if err := json.Unmarshal(replyOf(t, msgs, "1").Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	legend := res.Capabilities.SemanticTokensProvider
	if legend == nil || !legend.Full || len(legend.Legend.TokenTypes) == 0 {
		t.Fatalf("initialize 没答应 semanticTokensProvider（或图例是空的）：%+v", legend)
	}
	var got SemanticTokensResult
	if err := json.Unmarshal(replyOf(t, msgs, "4").Result, &got); err != nil {
		t.Fatalf("语义着色的答复形状不对：%v", err)
	}
	if len(got.Data) == 0 || len(got.Data)%5 != 0 {
		t.Fatalf("twice 与 n 都没报出来：%+v", got.Data)
	}
	for i := 0; i < len(got.Data); i += 5 {
		if got.Data[i+3] >= len(legend.Legend.TokenTypes) {
			t.Fatalf("第 %d 组的种类序号 %d 出了图例的范围（图例 %d 条）", i/5+1, got.Data[i+3], len(legend.Legend.TokenTypes))
		}
	}
}

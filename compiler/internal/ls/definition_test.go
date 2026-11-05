// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// definition_test.go 钉"跳转定义"这一条：名字在哪一份文件的哪一行被认出来，
// 以及 initialize 有没有把这一条本事报给编辑器、答复的形状对不对。
//
// en: definition_test.go pins go-to-definition - which file and line a name resolves to - and
// whether initialize reports the capability and answers in the protocol's own shape.
package ls

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefinitionOfLocalAndFunction(t *testing.T) {
	const text = `method twice(n:int):int {
    return n * 2
}

let total:int = 4
console.print(twice(total))
`
	locs := Definition("p.nf", text, Position{Line: 5, Character: 15})
	if len(locs) != 1 {
		t.Fatalf("twice 的定义没找出来：%v", locs)
	}
	if locs[0].Range.Start.Line != 0 {
		t.Fatalf("twice 该指到第 1 行，得到 %+v", locs[0])
	}
	locs = Definition("p.nf", text, Position{Line: 5, Character: 21})
	if len(locs) != 1 || locs[0].Range.Start.Line != 4 {
		t.Fatalf("total 该指到第 5 行，得到 %v", locs)
	}
}

func TestDefinitionUnknownNameIsSilent(t *testing.T) {
	if locs := Definition("p.nf", "console.print(1)\n", Position{Line: 0, Character: 8}); len(locs) != 0 {
		t.Fatalf("标准库的组名没有写在源码里的一行，不该指过去：%v", locs)
	}
}

func TestServeAnswersDefinition(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	const text = "let total:int = 4\\nconsole.print(total)\\n"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+`","languageId":"nf","version":1,"text":"`+text+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/definition","params":{"textDocument":{"uri":"`+uri+`"},"position":{"line":1,"character":15}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var res initResult
	if err := json.Unmarshal(replyOf(t, msgs, "1").Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	if !res.Capabilities.DefinitionProvider {
		t.Error("initialize 没答应 definitionProvider：编辑器就不会发跳转定义的请求")
	}

	var locs []Location
	if err := json.Unmarshal(replyOf(t, msgs, "4").Result, &locs); err != nil {
		t.Fatalf("跳转定义的答复形状不对：%v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 0 {
		t.Fatalf("total 该指到第 1 行，得到 %v", locs)
	}
	if !strings.HasPrefix(locs[0].URI, "file:///") {
		t.Errorf("答复里的文件要写成 file: URI，得到 %q", locs[0].URI)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// folding_test.go 钉折叠那一条：代码块的折叠与大纲同一份范围、连着的行注释与跨行的块注释
// 各成一段、开头那一串 import 合成一段，以及 initialize 有没有把这一条本事报给编辑器。
//
// en: folding_test.go pins folding: code ranges come from the same outline, consecutive line
// comments and a multi-line block comment each become one region, the leading imports collapse
// into one, and initialize advertises the capability.
package ls

import (
	"encoding/json"
	"testing"
)

func TestFoldingCoversBlocksCommentsAndImports(t *testing.T) {
	const text = `import { helper } from "./util.nf"
import { more } from "./more.nf"

// 这一行是要折的
// 连着第二行
class Note {
    method shout():string {
        return this.body
    }
}

/* 块注释
   第二行还在里面 */
let body:string = "甲"
`
	ranges := FoldingRanges("p.nf", text)
	var kinds = map[string]int{}
	for _, r := range ranges {
		kinds[r.Kind]++
	}
	if kinds[foldImports] != 1 {
		t.Errorf("两行 import 该合成一段折叠，得到 %+v", ranges)
	}
	if kinds[foldComment] < 2 {
		t.Errorf("连着的行注释与跨行的块注释各该有一段折叠，得到 %+v", ranges)
	}
	var found bool
	for _, r := range ranges {
		if r.Kind == "" && r.StartLine == 5 && r.EndLine >= 9 {
			found = true
		}
	}
	if !found {
		t.Errorf("class 那一身没有一段折叠：%+v", ranges)
	}
	// 里面的方法也得有自己的折叠，比外层短一截。
	for _, r := range ranges {
		if r.Kind == "" && r.StartLine == 6 {
			if r.EndLine <= r.StartLine || r.EndLine > 8 {
				t.Errorf("方法的折叠范围不对：%+v", r)
			}
			break
		}
	}
}

func TestFoldingSkipsSingleLineThings(t *testing.T) {
	const text = "method twice(n:int):int {\n    return n * 2\n}\nlet a:int = 1\n"
	for _, r := range FoldingRanges("p.nf", text) {
		if r.EndLine <= r.StartLine {
			t.Fatalf("一行长的东西不该有折叠：%+v", r)
		}
	}
}

func TestServeAnswersFoldingRange(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+
			`","languageId":"nf","version":1,"text":"class Note {\n    body:string\n}\n"}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/foldingRange","params":{"textDocument":{"uri":"`+uri+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var res initResult
	if err := json.Unmarshal(replyOf(t, msgs, "1").Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	if !res.Capabilities.FoldingRangeProvider {
		t.Error("initialize 没答应 foldingRangeProvider：编辑器就不会问折叠")
	}
	var ranges []FoldingRange
	if err := json.Unmarshal(replyOf(t, msgs, "4").Result, &ranges); err != nil {
		t.Fatalf("折叠的答复形状不对：%v", err)
	}
	if len(ranges) == 0 {
		t.Fatal("class 那一身没有返回折叠")
	}
}

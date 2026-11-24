// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// workspace_test.go 钉新增的两条：工作区按名字找符号只扫已打开的文档，
// 报文连 JSON 都不是时回解析错误码，不再悄悄丢掉。
//
// en: workspace_test.go pins the two additions: workspace symbol search scans only open
// documents, and a message that is not JSON gets a parse-error reply instead of being dropped.
package ls

import (
	"encoding/json"
	"testing"
)

func TestServeAnswersWorkspaceSymbol(t *testing.T) {
	src := `class Note { body:string }
method twice(n:int):int {
    return n * 2
}
const limit:int = 3
`
	esc, _ := json.Marshal(src) // 文本里有换行，按 JSON 串转义后再拼进报文
	text := string(esc)
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/ws.nf","languageId":"nf","version":1,"text":`+text+`}}}`),
		frame(`{"jsonrpc":"2.0","id":7,"method":"workspace/symbol","params":{"query":"tw"}}`),
	)
	var answer []SymbolInformation
	for _, m := range msgs {
		if string(m.ID) == "7" {
			if err := json.Unmarshal(m.Result, &answer); err != nil {
				t.Fatalf("workspace/symbol 的答复拆不开：%v", err)
			}
		}
	}
	if len(answer) != 1 || answer[0].Name != "twice" {
		t.Fatalf("按 tw 只该找到 twice，得到 %+v", answer)
	}
	if answer[0].Location.URI != "file:///p/ws.nf" {
		t.Fatalf("符号指错了文件：%s", answer[0].Location.URI)
	}
}

func TestServeRepliesParseErrorForBrokenJSON(t *testing.T) {
	msgs := exchange(t, frame(`{"jsonrpc":"2.0",`))
	if len(msgs) != 1 || msgs[0].Error == nil || msgs[0].Error.Code != ErrParseError {
		t.Fatalf("坏 JSON 该回解析错误 %d，得到 %+v", ErrParseError, msgs)
	}
}

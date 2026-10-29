// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// server_test.go 把这一层当作两端都测：把一串报文喂进去，把返回来的一串报文拆开看。
// 编辑器那一侧（VS Code）测不到，所以能在这里固定的都钉在这里。
package ls

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
)

// frame 拼出一条报文，与协议里那一侧一模一样的分帧。
func frame(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

type got struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// exchange 跑一趟服务：喂进已经分好帧的报文，读到 exit 或 EOF 为止，返回全部答复与通知。
func exchange(t *testing.T, requests ...string) []got {
	t.Helper()
	var in strings.Builder
	for _, r := range requests {
		in.WriteString(r)
	}
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in.String()), &out); err != nil {
		t.Fatalf("服务这一趟就断了：%v", err)
	}

	c := NewConn(bytes.NewReader(out.Bytes()), io.Discard)
	var msgs []got
	for {
		body, err := c.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("拆不开交回来的第 %d 条报文：%v", len(msgs)+1, err)
		}
		var m got
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("交回来的不是合法 JSON：%v\n%s", err, body)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func find(t *testing.T, msgs []got, method string) got {
	t.Helper()
	for _, m := range msgs {
		if m.Method == method {
			return m
		}
	}
	t.Fatalf("%s 一条也没交回来：%+v", method, msgs)
	return got{}
}

const brokenText = "method main(): int {\\n    let n: int = tru\\n    return n\\n}\\n"

func TestInitializeAnswersCapabilities(t *testing.T) {
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"initialized","params":{}}`),
		frame(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var reply *got
	for i, m := range msgs {
		if m.ID != nil && string(m.ID) == "1" {
			reply = &msgs[i]
		}
	}
	if reply == nil {
		t.Fatalf("initialize 没有答复：%+v", msgs)
	}
	var res initResult
	if err := json.Unmarshal(reply.Result, &res); err != nil {
		t.Fatalf("答复形状不对：%v（%s）", err, reply.Result)
	}
	if !res.Capabilities.TextDocumentSync.OpenClose {
		t.Error("要声明 openClose，否则 didClose 那一头的清理不会被通知")
	}
	if res.Capabilities.TextDocumentSync.Change != syncFull {
		t.Errorf("同步方式该是整份文本（1），得到 %d", res.Capabilities.TextDocumentSync.Change)
	}
	if res.ServerInfo.Name != "nf" {
		t.Errorf("服务器名字该是 nf，得到 %q", res.ServerInfo.Name)
	}
	// shutdown 回 null，exit 之后不再有任何报文。
	if len(msgs) != 2 {
		t.Errorf("只该有两条答复（initialize 与 shutdown），得到 %d 条：%+v", len(msgs), msgs)
	}
}

func TestDidOpenPublishesDiagnostics(t *testing.T) {
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/p1.nf","languageId":"nf","version":1,"text":"`+brokenText+`"}}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	n := find(t, msgs, "textDocument/publishDiagnostics")
	var p diagParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		t.Fatalf("诊断通知的形状不对：%v", err)
	}
	if p.URI != "file:///p/p1.nf" {
		t.Errorf("诊断发到了别的文件上：%s", p.URI)
	}
	if len(p.Diagnostics) != 1 || p.Diagnostics[0].Message != "没有 tru 这个名字" {
		t.Fatalf("诊断内容不对：%+v", p.Diagnostics)
	}
}

// 改一处就重新算一份：didChange 带的是整份新文本，波浪线要跟着新文本走。
func TestDidChangeRepublishes(t *testing.T) {
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/p1.nf","languageId":"nf","version":1,"text":"`+brokenText+`"}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///p/p1.nf","version":2},"contentChanges":[{"text":"method main(): int {\n    let n: int = 1\n    return n\n}\n"}]}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var last diagParams
	n := 0
	for _, m := range msgs {
		if m.Method == "textDocument/publishDiagnostics" {
			if err := json.Unmarshal(m.Params, &last); err != nil {
				t.Fatalf("诊断通知的形状不对：%v", err)
			}
			n++
		}
	}
	if n != 2 {
		t.Fatalf("该有两次推送（打开一次、改动一次），得到 %d 次", n)
	}
	if len(last.Diagnostics) != 0 {
		t.Errorf("改对了还留着波浪线：%+v", last.Diagnostics)
	}
}

// 关掉页签要把它那个位置擦干净，不然下次打开同一份文件，旧的波浪线还挂在原位。
func TestDidCloseClearsDiagnostics(t *testing.T) {
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/p1.nf","languageId":"nf","version":1,"text":"`+brokenText+`"}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":"file:///p/p1.nf"}}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var last diagParams
	n := 0
	for _, m := range msgs {
		if m.Method == "textDocument/publishDiagnostics" {
			if err := json.Unmarshal(m.Params, &last); err != nil {
				t.Fatalf("诊断通知的形状不对：%v", err)
			}
			n++
		}
	}
	if n != 2 {
		t.Fatalf("该有两次推送（打开一次、关闭一次），得到 %d 次", n)
	}
	if last.Diagnostics == nil || len(last.Diagnostics) != 0 {
		t.Errorf("关闭那一趟要交回一个空的诊断表，得到 %+v", last.Diagnostics)
	}
}

// 认不出的方法要按 JSON-RPC 的规矩回 -32601，并且不能因此断掉这一趟对话。
func TestUnknownMethodRepliesErrorAndKeepsGoing(t *testing.T) {
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"textDocument/whatever","params":{}}`),
		frame(`{"jsonrpc":"2.0","method":"$/myNotification","params":{}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var errReply *got
	for i, m := range msgs {
		if m.Error != nil {
			errReply = &msgs[i]
		}
	}
	if errReply == nil {
		t.Fatalf("认不出的方法没有回错误：%+v", msgs)
	}
	if errReply.Error.Code != ErrMethodNotFound {
		t.Errorf("错误码不对：得到 %d", errReply.Error.Code)
	}
}

// 编辑器那头的 file URI 是 file:///c%3A/… 这种写法，而检查器认的是斜杠路径。
func TestURIBecomesSlashPath(t *testing.T) {
	cases := []struct {
		uri, path string
	}{
		{"file:///p/p1.nf", "/p/p1.nf"},
		{"file:///c%3A/proj/main.nf", "c:/proj/main.nf"},
		{"file:///c%3A/proj/%E7%94%B2.nf", "c:/proj/甲.nf"},
		{"file:///c%3A/a%20b/main.nf", "c:/a b/main.nf"},
	}
	for _, c := range cases {
		if got := pathFromURI(c.uri); got != c.path {
			t.Errorf("%s 该翻成 %s，得到 %s", c.uri, c.path, got)
		}
		if got := uriFromPath(c.path); got != c.uri {
			t.Errorf("%s 该翻成 %s，得到 %s", c.path, c.uri, got)
		}
	}
}

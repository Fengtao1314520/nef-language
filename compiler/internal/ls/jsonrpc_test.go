// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// jsonrpc_test.go 固定 stdio 上那一层分帧：LSP 的报文是"Content-Length: 字节数"加一个空行
// 再加一段 JSON。一条报文可能拆在好几次 read 里到达，也可能两条挤在一次到达。
// 这一层的对错决定编辑器那边收得到收不到答复，所以先把它单独固定。
package ls

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestReadOneFrame(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	in := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body

	c := NewConn(strings.NewReader(in), &bytes.Buffer{})
	got, err := c.Read()
	if err != nil {
		t.Fatalf("读一条报文失败：%v", err)
	}
	if string(got) != body {
		t.Errorf("报文内容不对\n想要：%s\n得到：%s", body, string(got))
	}
}

func TestReadTwoFramesBackToBack(t *testing.T) {
	a := `{"id":1}`
	b := `{"id":2,"method":"exit"}`
	in := "Content-Length: " + strconv.Itoa(len(a)) + "\r\n\r\n" + a +
		"Content-Length: " + strconv.Itoa(len(b)) + "\r\n\r\n" + b

	c := NewConn(strings.NewReader(in), &bytes.Buffer{})
	first, err := c.Read()
	if err != nil {
		t.Fatalf("读第一条失败：%v", err)
	}
	second, err := c.Read()
	if err != nil {
		t.Fatalf("读第二条失败：%v", err)
	}
	if string(first) != a || string(second) != b {
		t.Errorf("两条报文串了位\n想要：%s / %s\n得到：%s / %s", a, b, first, second)
	}
}

// 头字段的名字大小写不算，且允许带 Content-Type（有的客户端会带）。
func TestReadFrameHeaderVariants(t *testing.T) {
	body := `{"id":3}`
	in := "content-length: 8\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n" + body

	c := NewConn(strings.NewReader(in), &bytes.Buffer{})
	got, err := c.Read()
	if err != nil {
		t.Fatalf("读报文失败：%v", err)
	}
	if string(got) != body {
		t.Errorf("报文体不对：想要 %s，得到 %s", body, string(got))
	}
}

// 没有 Content-Length 就不知道一条报文到哪儿结束，这时候不能闷着等下去。
func TestReadFrameNeedsContentLength(t *testing.T) {
	in := "Content-Type: application/vscode-jsonrpc\r\n\r\n{}"

	c := NewConn(strings.NewReader(in), &bytes.Buffer{})
	if _, err := c.Read(); err == nil {
		t.Fatal("缺 Content-Length 这一条报文本该读不出来")
	}
}

// 报文体里是 UTF-8 的中文时，Content-Length 数的是字节数不是字数。
func TestReadFrameCountsBytesNotRunes(t *testing.T) {
	body := `{"message":"这里要一个表达式"}`
	in := "Content-Length: " + strconv.Itoa(len([]byte(body))) + "\r\n\r\n" + body

	c := NewConn(strings.NewReader(in), &bytes.Buffer{})
	got, err := c.Read()
	if err != nil {
		t.Fatalf("读报文失败：%v", err)
	}
	if string(got) != body {
		t.Errorf("中文报文被切断了\n想要：%s\n得到：%s", body, string(got))
	}
}

func TestWriteNotification(t *testing.T) {
	var out bytes.Buffer
	c := NewConn(strings.NewReader(""), &out)

	if err := c.Notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         "file:///a.nf",
		"diagnostics": []any{},
	}); err != nil {
		t.Fatalf("写通知失败：%v", err)
	}

	text := out.String()
	if !strings.HasPrefix(text, "Content-Length: ") {
		t.Fatalf("写出来的报文没有 Content-Length 头：%s", text)
	}
	header, body, ok := strings.Cut(text, "\r\n\r\n")
	if !ok {
		t.Fatalf("头与体之间少了空行：%s", text)
	}
	length := strings.TrimPrefix(header, "Content-Length: ")
	if length != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length 写的数是 %s，体实际 %d 字节", length, len(body))
	}
	var msg struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		ID      *int   `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("体不是合法 JSON：%v", err)
	}
	if msg.JSONRPC != "2.0" || msg.Method != "textDocument/publishDiagnostics" {
		t.Errorf("通知的形状不对：%+v", msg)
	}
	if msg.ID != nil {
		t.Errorf("通知不该带 id：%s", body)
	}
}

// 答复要把 id 原样返回：客户端靠它对上号，id 是数字还是字符串都不能改。
func TestReplyEchoesID(t *testing.T) {
	var out bytes.Buffer
	c := NewConn(strings.NewReader(""), &out)

	if err := c.Reply(json.RawMessage(`"7"`), map[string]any{"ok": true}); err != nil {
		t.Fatalf("写答复失败：%v", err)
	}
	_, body, ok := strings.Cut(out.String(), "\r\n\r\n")
	if !ok {
		t.Fatal("答复没有分帧头")
	}
	if !strings.Contains(body, `"id":"7"`) {
		t.Errorf("字符串 id 被改写了：%s", body)
	}
	if !strings.Contains(body, `"result"`) {
		t.Errorf("答复里少了 result：%s", body)
	}
}

// 方法认不出时回的是 JSON-RPC 的 -32601，编辑器靠这个号决定要不要报错给用户。
func TestReplyErrorUsesMethodNotFoundCode(t *testing.T) {
	var out bytes.Buffer
	c := NewConn(strings.NewReader(""), &out)

	if err := c.ReplyError(json.RawMessage(`4`), ErrMethodNotFound, "没有这个方法"); err != nil {
		t.Fatalf("写错误答复失败：%v", err)
	}
	_, body, ok := strings.Cut(out.String(), "\r\n\r\n")
	if !ok {
		t.Fatal("错误答复没有分帧头")
	}
	var msg struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("错误答复不是合法 JSON：%v", err)
	}
	if msg.Error.Code != -32601 {
		t.Errorf("错误码不对：想要 -32601，得到 %d（%s）", msg.Error.Code, body)
	}
}

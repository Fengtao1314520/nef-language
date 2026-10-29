// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// server.go 是那一条循环：读一条报文，该答的答，该推诊断的推，直到编辑器说 exit
// 或者把那一侧关掉（`nf lsp`）。
//
// 只处理这一截用得上的方法。无法识别的请求按 JSON-RPC 回 -32601，无法识别的通知直接丢：
// 编辑器会发一大串这边根本没有的方法（画参考、格式化、拖拽），回它一条"没有这个方法"
// 比断掉那条对话好。
//
// en: server.go is the loop: read one message, answer what needs answering, push diagnostics
// when they changed, until the editor says exit or closes that end (`nf lsp`).
// Only the methods this slice uses are handled: an unrecognised request gets JSON-RPC -32601,
// an unrecognised notification is dropped outright. Editors send a long list of methods this
// server does not have (references, formatting, rename), and one "no such method" reply is
// better than killing the conversation.
package ls

import (
	"encoding/json"
	"fmt"
	"io"
)

// Serve 从 r 读报文、往 w 写答复，读到 exit 或 r 到尽头就返回。
//
// en: Serve reads messages from r and writes replies to w, returning when exit arrives or r
// reaches its end.
func Serve(r io.Reader, w io.Writer) error {
	s := &server{conn: NewConn(r, w), docs: map[string]string{}, checked: map[string][]string{}}
	for {
		body, err := s.conn.Read()
		if err != nil {
			if isEOF(err) {
				return nil
			}
			return err
		}
		var m request
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		if s.handle(m) {
			return nil
		}
	}
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type server struct {
	conn *Conn
	// docs 记着编辑器当前打开的那几份文件的文本：键是斜杠路径，值是整份文本。
	// 当前这一份不读盘，盘上那份可能是上一次保存的旧内容。
	docs map[string]string
	// checked 记每一回检查顺带读进来的那些文件，为的是关掉页签时能把它们的波浪线一起擦掉。
	checked map[string][]string
}

// handle 返回 true 就说这一趟对话到此为止（exit）。
//
// 每条报文都垫一层 recover：一份写到一半的程序碰到检查器算不动的那个位置，
// 塌下来的栈不该让整条对话断掉——编辑器那一侧从此没有波浪线，比多一句报错严重得多。
//
// en: handle returns true to say this conversation is over (exit). Every message is wrapped in
// a recover: when a half-written program hits a position the checker cannot compute, the
// collapsing stack must not kill the conversation - an editor that stops showing squiggles is
// far worse than one extra error message.
func (s *server) handle(m request) (stop bool) {
	defer func() {
		if r := recover(); r != nil {
			if m.ID != nil {
				s.conn.ReplyError(m.ID, ErrInternal, "nf 的服务器算这一条时报了意外："+fmt.Sprint(r))
			}
		}
	}()

	switch m.Method {
	case "initialize":
		s.conn.Reply(m.ID, initResult{
			Capabilities: capabilities{
				TextDocumentSync:   syncOptions{OpenClose: true, Change: syncFull},
				CompletionProvider: &completionOptions{TriggerCharacters: []string{"."}},
				HoverProvider:      true,
			},
			ServerInfo: serverInfo{Name: "nf"},
		})
	case "initialized", "textDocument/didSave", "workspace/didChangeConfiguration":
		// 没有要做的事：这一版没有建库、没有后台预热，改动就是重算。
	case "shutdown":
		s.conn.Reply(m.ID, nil)
	case "exit":
		return true
	case "textDocument/didOpen":
		var p openParams
		if json.Unmarshal(m.Params, &p) != nil {
			s.badParams(m)
			return false
		}
		s.docs[p.TextDocument.URI] = p.TextDocument.Text
		s.publish(pathFromURI(p.TextDocument.URI), p.TextDocument.Text)
	case "textDocument/didChange":
		var p changeParams
		if json.Unmarshal(m.Params, &p) != nil {
			s.badParams(m)
			return false
		}
		if len(p.ContentChanges) == 0 {
			return false
		}
		// 同步方式在 initialize 里答的是整份文本（change=1），所以最后那一条就是新的全文。
		text := p.ContentChanges[len(p.ContentChanges)-1].Text
		s.docs[p.TextDocument.URI] = text
		s.publish(pathFromURI(p.TextDocument.URI), text)
	case "textDocument/didClose":
		var p closeParams
		if json.Unmarshal(m.Params, &p) != nil {
			s.badParams(m)
			return false
		}
		uri := p.TextDocument.URI
		delete(s.docs, uri)
		s.publishTo(uri, []Diagnostic{})
		for _, other := range s.checked[uri] {
			if other == uri {
				continue // 上一行刚清空过这一份文件
			}
			if _, stillOpen := s.docs[other]; !stillOpen {
				s.publishTo(other, []Diagnostic{})
			}
		}
		delete(s.checked, uri)
	case "textDocument/completion":
		var p positionParams
		if json.Unmarshal(m.Params, &p) != nil {
			s.badParams(m)
			return false
		}
		text, ok := s.docs[p.TextDocument.URI]
		if !ok {
			s.conn.Reply(m.ID, []CompletionItem{})
			return false
		}
		items := Completion(pathFromURI(p.TextDocument.URI), text, p.Position)
		if items == nil {
			items = []CompletionItem{}
		}
		s.conn.Reply(m.ID, items)
	case "textDocument/hover":
		var p positionParams
		if json.Unmarshal(m.Params, &p) != nil {
			s.badParams(m)
			return false
		}
		text, ok := s.docs[p.TextDocument.URI]
		if !ok {
			s.conn.Reply(m.ID, nil)
			return false
		}
		s.conn.Reply(m.ID, Hover(pathFromURI(p.TextDocument.URI), text, p.Position))
	default:
		if m.ID != nil {
			s.conn.ReplyError(m.ID, ErrMethodNotFound, "nf 的服务器没有这个方法："+m.Method)
		}
	}
	return false
}

func (s *server) badParams(m request) {
	if m.ID != nil {
		s.conn.ReplyError(m.ID, ErrInvalidParams, "这一条报文的参数读不出来："+m.Method)
	}
}

// publish 走完前端，把每一参与的文件各自的波浪线推到各自的 URI 上。
// 顺带记下这一次涉及哪几份文件，关掉页签时才知道要擦哪几格。
//
// en: publish runs the front end and pushes each file's squiggles to that file's own URI. On
// the way it records which files were involved, so closing a tab knows which entries to clear.
func (s *server) publish(path, text string) {
	diags := Diagnostics(path, text)
	var involved []string
	for file, list := range diags {
		uri := uriFromPath(file)
		if list == nil {
			list = []Diagnostic{}
		}
		s.publishTo(uri, list)
		involved = append(involved, uri)
	}
	s.checked[uriFromPath(path)] = involved
}

func (s *server) publishTo(uri string, list []Diagnostic) {
	s.conn.Notify("textDocument/publishDiagnostics", diagParams{URI: uri, Diagnostics: list})
}

type openParams struct {
	TextDocument struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"textDocument"`
}

type changeParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type closeParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
}

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

// Package ls 是 nf 的语言服务器：编辑器把整份文本推过来，这里走完 词法 → 语法 → 静态检查，
// 把带行列的答复推回去（`nf lsp`）。
//
// 协议那一层只用得到 LSP 的一小截：分帧、几条方法、一条诊断通知。类型全部手写，
// 为的是这一份跟编译器一样不依赖任何东西，编出来的还是那一个 `nf.exe`。
//
// en: Package ls is nf's language server: the editor pushes a whole text, this side runs lexer,
// parser and static check, and pushes the answers with line and column back (`nf lsp`).
// The protocol layer uses only a slice of LSP: framing, a handful of methods, one
// diagnostic notification. Every type is handwritten so that this package, like the compiler,
// depends on nothing and the build is still the same single nf.exe.
package ls

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// 下面是 JSON-RPC 2.0 定好的错误码，不是这边自己取的数：编辑器认这几个号。
//
// en: The error codes below are the ones JSON-RPC 2.0 fixed, not numbers chosen here: the
// editor recognises these.
const (
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
	ErrNotInitialized = -32002
)

// Conn 是 stdio 上那一条报文通道。写要带锁：答复与诊断通知可能由两处先后写出，
// 半条报文混进另一条中间就整条废掉。
//
// en: Conn is the message channel over stdio. Writing takes a lock: a response and a diagnostic
// notification may be emitted from two places in sequence, and half a message interleaved into
// another ruins both.
type Conn struct {
	r  *bufio.Reader
	w  *bufio.Writer
	mu sync.Mutex
}

func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{r: bufio.NewReader(r), w: bufio.NewWriter(w)}
}

// Read 读出下一条报文的 JSON 体。头里只认 Content-Length（大小写不算），其余字段一律忽略。
//
// en: Read returns the JSON body of the next message. Only Content-Length in the header is
// honoured (case-insensitive); every other field is ignored.
func (c *Conn) Read() ([]byte, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("报文头里的长度不是个数：%q", line)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("报文头里没有 Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, fmt.Errorf("报文只差 %d 字节就断了：%w", length, err)
	}
	return body, nil
}

// message 是发出去的那一条报文。数字、字符串两种 id 都直接用 RawMessage 原样带着走，
// 客户端靠 id 对号，这边改了就对不上。
//
// en: message is one outgoing message. Both the numeric and the string form of id travel as-is
// inside a RawMessage: the client matches replies by id, so rewriting it here would break the
// pairing.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Conn) send(m message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body))
	if _, err := c.w.Write(body); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *Conn) Notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(message{JSONRPC: "2.0", Method: method, Params: p})
}

// Reply 返回一个答复；result 给 nil 就是 JSON 的 null（shutdown 要的就是这个）。
//
// en: Reply returns one response; a nil result is JSON null (which is exactly what shutdown
// wants).
func (c *Conn) Reply(id json.RawMessage, result any) error {
	var r json.RawMessage
	if result == nil {
		r = json.RawMessage("null")
	} else if raw, ok := result.(json.RawMessage); ok {
		r = raw
	} else if b, err := json.Marshal(result); err != nil {
		return err
	} else {
		r = b
	}
	return c.send(message{JSONRPC: "2.0", ID: id, Result: r})
}

func (c *Conn) ReplyError(id json.RawMessage, code int, msg string) error {
	return c.send(message{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

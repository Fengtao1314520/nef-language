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

// wire.go 是"运行程序的那一侧"与"跟编辑器说话的那一侧"之间的几条消息。
//
// 分成两侧是因为程序跑完会自己叫操作系统结束整个进程（exit），要是它与调试协议
// 在同一个进程里，程序一结束协议就断了。所以真正被调试的程序另起一个子进程，两侧用
// 本机回环上的一条线通信：一行一条 JSON。
//
// en: wire.go holds the few messages between the side that runs the program and the side that
// talks to the editor. They are split in two because a finished program asks the operating
// system to end the whole process (exit): if that lived in the same process as the debug
// protocol, the protocol would end with it. So the debugged program runs in a separate child
// process and the two sides speak over a local loopback connection, one JSON message per line.
package debug

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Message 是这条线上的每一条消息。哪个字段相关就填哪个，其余留空。
//
// en: Message is one line on that wire. Whichever field is relevant is filled, the rest stay
// empty.
type Message struct {
	Event  string `json:"event,omitempty"`  // ready（已就绪）｜stopped（已停住）｜exit（程序已结束）
	Cmd    string `json:"cmd,omitempty"`    // start｜continue｜step｜stack｜locals｜globals｜breakpoints｜terminate
	Reply  bool   `json:"reply,omitempty"`  // 真：这一条是答复，不是主动上报的事件
	Reason string `json:"reason,omitempty"` // breakpoint｜step｜exit

	Mode  string `json:"mode,omitempty"`  // 单步的三种走法：over｜in｜out
	Frame int    `json:"frame,omitempty"` // 要看第几帧

	Name    string `json:"name,omitempty"`  // 悬停问的那个名字
	Value   string `json:"value,omitempty"` // 那个位置当前的值（求值请求的答复）
	Found   bool   `json:"found,omitempty"` // 那一帧上有没有这个名字
	Address uint64 `json:"addr,omitempty"`  // 那个位置在内存里的地址
	Count   int    `json:"count,omitempty"` // 内存请求要抄几个字节
	Data    string `json:"data,omitempty"`  // 抄出来的字节，base64

	File     string      `json:"file,omitempty"`
	Line     int         `json:"line,omitempty"` // 停住的那个位置在源码的第几行第几列
	Col      int         `json:"col,omitempty"`
	Lines    []int       `json:"lines,omitempty"`
	Verified []int       `json:"verified,omitempty"`
	Frames   []FrameInfo `json:"frames,omitempty"`
	Vars     []VarInfo   `json:"vars,omitempty"`
	Code     int         `json:"code,omitempty"` // 程序的退出码
}

// FrameInfo 是调用栈上的一个位置。ID 由这一侧发出去，回来时按它找那一帧。
//
// en: FrameInfo is one position in the call stack. ID is issued by this side and used to find
// that frame when it comes back.
type FrameInfo struct {
	ID   int
	Name string
	File string
	Line int
	Col  int
}

// VarInfo 是一个位置变量。值已解码成可读文本，编辑器直接显示；Address 是那个位置在内存里的位置，
// 编辑器的内存视图拿它当起点。
//
// en: VarInfo is one named position. Its value is already decoded into human-readable text, so
// the editor only prints it; Address is where that position lives in memory, which is where the
// editor's memory view starts.
type VarInfo struct {
	Name    string
	Type    string
	Value   string
	Address uint64
}

// WriteMessage 写一条：一行一条，末尾一个换行。
//
// en: WriteMessage writes one message: one per line, ending with a newline.
func WriteMessage(w io.Writer, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// ReadMessage 读一条。对端关闭时返回 io.EOF。
//
// en: ReadMessage reads one message, and returns io.EOF when the other end closes.
func ReadMessage(r *bufio.Reader) (Message, error) {
	var m Message
	line, err := r.ReadBytes('\n')
	if err != nil {
		return m, err
	}
	if len(line) == 0 {
		return m, io.EOF
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("这一条话读不成：%w", err)
	}
	return m, nil
}

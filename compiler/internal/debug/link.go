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

// link.go 是父这一侧握着的那条线：等子进程连上来，读它报上来的话，把"问一句等一句"的
// 那几条对上号，其余的话（停住了、程序结束了）转给事件那一侧。
//
// en: link.go is the wire held by the parent side: it waits for the child process to connect,
// reads what it reports, matches the "ask one, answer one" requests to their replies, and
// forwards everything else (stopped, program ended) to the event side.
package debug

import (
	"bufio"
	"net"
	"sync"
)

// Link 是这一条线。Ask 一次只允许一个在等：编辑器停住时本来就是一条一条问的。
//
// en: Link is that wire. Ask allows only one reply outstanding at a time: while the editor is
// stopped it asks one thing at a time anyway.
type Link struct {
	ln      net.Listener
	conn    net.Conn
	wmu     sync.Mutex
	rmu     sync.Mutex
	replies chan Message
}

// Wait 等子进程连上来，并开始读它。events 收的是子进程主动报的话。
//
// en: Wait waits for the child process to connect and starts reading it. events receives what
// the child reports on its own initiative.
func Wait(ln net.Listener, events chan<- Message) (*Link, error) {
	conn, err := ln.Accept()
	if err != nil {
		return nil, err
	}
	l := &Link{ln: ln, conn: conn, replies: make(chan Message, 8)}
	go l.readLoop(bufio.NewReader(conn), events)
	return l, nil
}

func (l *Link) readLoop(r *bufio.Reader, events chan<- Message) {
	for {
		m, err := ReadMessage(r)
		if err != nil {
			// 对端断开：多半是程序跑完自己叫操作系统结束了进程。
			// 向上报一条 exit 后停止读取，不关 events——编辑器随后还会发一条 disconnect，
			// 关掉那条通道会让这一侧的主循环提前结束。
			events <- Message{Event: "exit"}
			return
		}
		if m.Reply {
			l.replies <- m
			continue
		}
		events <- m
	}
}

// Send 写一条不等回话的命令。
//
// en: Send writes one command without waiting for a reply.
func (l *Link) Send(m Message) error {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	return WriteMessage(l.conn, m)
}

// Ask 写一条并等它的回话。那头断了就返回 false。
//
// en: Ask writes one command and waits for its reply. If the other end has closed, it returns
// false.
func (l *Link) Ask(m Message) (Message, bool) {
	l.rmu.Lock()
	defer l.rmu.Unlock()
	m.Reply = false
	if err := l.Send(m); err != nil {
		return Message{}, false
	}
	got, ok := <-l.replies
	return got, ok
}

func (l *Link) Close() {
	if l.conn != nil {
		l.conn.Close()
	}
	if l.ln != nil {
		l.ln.Close()
	}
}

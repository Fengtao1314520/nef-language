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

// Package dap 是跟编辑器说话的那一侧：标准输入收调试请求，标准输出回答复与事件。
//
// 报文形状是调试协议定好的：与语言服务器同一套分帧（Content-Length 加一段 JSON），
// 里面是 seq、type（request／response／event）与 command。请求要么回成功要么回失败，
// 停住与程序结束是这一侧主动报出去的事件。
//
// 真正在跑的程序不在本进程里：它跑完会自己叫操作系统结束整个进程（exit），
// 同一进程的话协议就跟着它一起断了。所以这一侧只管翻译，子进程由命令行那一侧起，
// 两侧之间是本机回环上的一条线（internal/debug 的 Link）。
//
// en: Package dap is the side that talks to the editor: it takes debug requests on standard
// input and answers with responses and events on standard output. The message shape is fixed by
// the Debug Adapter Protocol: the same framing as the language server (Content-Length plus a
// JSON body), carrying seq, type (request/response/event) and command. A request gets either a
// success or a failure back; stopping and program end are events this side sends on its own
// initiative. The program being debugged does not live in this process: when it finishes it
// asks the operating system to end the whole process (exit), and in the same
// process the protocol would end along with it. So this side only translates; the child process
// is started by the command line, and the two sides share a local loopback connection
// (internal/debug's Link).
package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"neflang.org/nf/internal/debug"
)

// Start 由命令行那一侧交进来：起一个被调试的子进程。port 是这一侧等它连回来的端口，
// onOutput 收程序写到标准输出与标准错误上的文本，category 是 "stdout" 或 "stderr"。
// 返回的 wait 等那个进程结束，给出它的退出码。
//
// en: Start is handed in by the command-line side and starts one debugged child process. port
// is the port this side waits for it to connect back to, onOutput receives the text the program
// writes to the screen and to standard error, and category is "stdout" or "stderr". The
// returned wait waits for that process to end and gives its exit code.
type Start func(program string, args []string, port string, onOutput func(category, text string)) (wait func() int, err error)

// 与编辑器约定的那一个线程号：这一版程序只有一条跑 nf 代码的线程。
//
// en: The thread id agreed with the editor: this version of the language has only one thread
// running nf code.
const threadID = 1

type server struct {
	out   *writer
	start Start

	link  *debug.Link
	wait  func() int
	seq   int
	seqMu sync.Mutex
}

// Serve 跑这一趟对话：读到标准输入关掉、或者收到 disconnect 为止。
//
// en: Serve runs one conversation: until standard input closes or a disconnect arrives.
func Serve(in io.Reader, w io.Writer, start Start) error {
	s := &server{out: &writer{w: w}, start: start}
	requests := make(chan map[string]any)
	go func() {
		defer close(requests)
		r := newReader(in)
		for {
			m, err := r.read()
			if err != nil {
				return
			}
			requests <- m
		}
	}()
	events := make(chan debug.Message)
	eventsOpen := true

	for eventsOpen {
		select {
		case m, ok := <-requests:
			if !ok {
				return nil
			}
			if s.handle(m, events) {
				return nil
			}
		case ev, ok := <-events:
			if !ok {
				eventsOpen = false
				continue
			}
			s.report(ev)
		}
	}
	return nil
}

// handle 识别调试协议里那十来条请求。识别不了的回一句"没有这一条"，不能不回：
// 不回话时编辑器那一侧会一直等待。返回 true 说的是这一趟对话到此为止（disconnect）。
//
// en: handle recognizes the dozen or so requests of the debug protocol. Anything unrecognised
// gets a "no such request" back rather than silence - without a reply the editor waits
// indefinitely. Returning true means this conversation is over (disconnect).
func (s *server) handle(m map[string]any, events chan<- debug.Message) bool {
	cmd, _ := m["command"].(string)
	seq := intOf(m["seq"])
	args, _ := m["arguments"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}
	switch cmd {
	case "initialize":
		s.reply(seq, cmd, true, map[string]any{
			"supportsConfigurationDoneRequest": true,
			"supportsTerminateRequest":         true,
			// pause：运行时每一条语句都会向宿主报一次到，所以暂停请求能生效。
			"supportsSuspendRequest": true,
			// 悬停：只查已有的名字，表达式无法求值（需要一个求值器）。
			"supportsEvaluateForHovers": true,
			// 内存视图：每一个位置变量都带着它在内存里的地址。
			"supportsMemoryReferences":  true,
			"supportsReadMemoryRequest": true,
		})
		s.event("initialized", map[string]any{})
	case "launch":
		if err := s.launch(args, events); err != nil {
			s.reply(seq, cmd, false, map[string]any{"message": err.Error()})
			return false
		}
		s.reply(seq, cmd, true, map[string]any{})
	case "setBreakpoints":
		s.reply(seq, cmd, true, map[string]any{"breakpoints": s.setBreakpoints(args)})
	case "configurationDone":
		s.toChild(debug.Message{Cmd: "start"})
		s.reply(seq, cmd, true, map[string]any{})
	case "threads":
		s.reply(seq, cmd, true, map[string]any{
			"threads": []map[string]any{{"id": threadID, "name": "nf 程序"}},
		})
	case "stackTrace":
		s.stackTrace(seq, args)
	case "scopes":
		// 一帧两个作用域：这一帧自己的局部量，加上顶层那些住在静态槽里的名字
		// （停在文件顶层时，`let k` 就在那一批里，光看帧上是空的）。
		// 引用号与帧号成对：局部＝帧号×2−1（奇数），静态＝帧号×2（偶数）。
		// 帧号从 1 起，所以两个号都不是 0——协议里 0 说的是"没有可取的内容"。
		n := intOf(args["frameId"])
		s.reply(seq, cmd, true, map[string]any{
			"scopes": []map[string]any{
				{"name": "局部", "kind": "locals", "variablesReference": n*2 - 1, "expensive": false},
				{"name": "静态", "kind": "static", "variablesReference": n * 2, "expensive": false},
			},
		})
	case "variables":
		s.variables(seq, args)
	case "continue":
		s.toChild(debug.Message{Cmd: "continue"})
		s.reply(seq, cmd, true, map[string]any{"allThreadsContinued": true})
	case "next", "stepIn", "stepOut":
		mode := map[string]string{"next": "over", "stepIn": "in", "stepOut": "out"}[cmd]
		s.toChild(debug.Message{Cmd: "step", Mode: mode})
		s.reply(seq, cmd, true, map[string]any{})
	case "pause":
		s.toChild(debug.Message{Cmd: "pause"})
		s.reply(seq, cmd, true, map[string]any{})
	case "evaluate":
		s.evaluate(seq, args)
	case "readMemory":
		s.readMemory(seq, args)
	case "terminate":
		s.toChild(debug.Message{Cmd: "terminate"})
		s.reply(seq, cmd, true, map[string]any{})
	case "disconnect":
		s.toChild(debug.Message{Cmd: "terminate"})
		s.reply(seq, cmd, true, map[string]any{})
		return true
	default:
		s.reply(seq, cmd, false, map[string]any{"message": "nf 的调试器没有这一条请求：" + cmd})
	}
	return false
}

// launch 起子进程、等它连上来，并把那条线上主动报的话转成事件。
//
// en: launch starts the child process, waits for it to connect, and turns whatever it reports
// on the wire into events.
func (s *server) launch(args map[string]any, events chan<- debug.Message) error {
	program, _ := args["program"].(string)
	if program == "" {
		return fmt.Errorf("launch 里没给 program：要调试得说清哪一份 .nf")
	}
	var progArgs []string
	if list, ok := args["args"].([]any); ok {
		for _, v := range list {
			progArgs = append(progArgs, fmt.Sprint(v))
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("开不出本机那条调试用的线：%w", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	onOutput := func(category, text string) {
		s.event("output", map[string]any{"category": category, "output": text})
	}
	wait, err := s.start(program, progArgs, port, onOutput)
	if err != nil {
		ln.Close()
		return err
	}
	link, err := debug.Wait(ln, events)
	if err != nil {
		ln.Close()
		return fmt.Errorf("等不到被调试的那个进程：%w", err)
	}
	s.link = link
	s.wait = wait
	return nil
}

func (s *server) toChild(m debug.Message) {
	if s.link == nil {
		return
	}
	s.link.Send(m)
}

func (s *server) askChild(m debug.Message) (debug.Message, bool) {
	if s.link == nil {
		return debug.Message{}, false
	}
	return s.link.Ask(m)
}

// setBreakpoints 把这一份文件的全部断点报给子进程（一行钉不住要照实说钉不住，
// 不然编辑器画一个实心点，程序跑到那儿却不停）。
//
// en: setBreakpoints reports every breakpoint of this file to the child process. A line that
// cannot be pinned is reported as unpinned, otherwise the editor draws a solid dot and the
// program runs straight past it without stopping.
func (s *server) setBreakpoints(args map[string]any) []map[string]any {
	src, _ := args["source"].(map[string]any)
	path, _ := src["path"].(string)
	var lines []int
	list, _ := args["breakpoints"].([]any)
	for _, b := range list {
		bm, _ := b.(map[string]any)
		if bm == nil {
			continue
		}
		lines = append(lines, intOf(bm["line"]))
	}
	verified := []int{}
	if reply, ok := s.askChild(debug.Message{Cmd: "breakpoints", File: path, Lines: lines}); ok {
		verified = reply.Verified
	}
	out := []map[string]any{}
	for _, line := range lines {
		ok := false
		for _, v := range verified {
			if v == line {
				ok = true
			}
		}
		msg := ""
		if !ok {
			msg = "这一行上没有语句，钉不住"
		}
		out = append(out, map[string]any{
			"verified": ok, "line": line,
			"source":  map[string]any{"path": path},
			"message": msg,
		})
	}
	return out
}

func (s *server) stackTrace(seq int, args map[string]any) {
	frames := []map[string]any{}
	if reply, ok := s.askChild(debug.Message{Cmd: "stack"}); ok {
		for _, f := range reply.Frames {
			frames = append(frames, map[string]any{
				// 帧号从 1 起：协议里 0 说的是"没有可取的内容"，
				// 用它当第一帧的号，编辑器就不会发起变量请求。
				"id": f.ID + 1, "name": f.Name, "line": f.Line, "column": f.Col,
				"source": map[string]any{"path": f.File, "name": baseName(f.File)},
			})
		}
	}
	s.reply(seq, "stackTrace", true, map[string]any{"stackFrames": frames, "totalFrames": len(frames)})
}

func (s *server) variables(seq int, args map[string]any) {
	vars := []map[string]any{}
	// 引用号是作用域那一条发出去的：奇数是局部（还原成子进程的帧下标），偶数是静态。
	ref := intOf(args["variablesReference"])
	cmd, frame := "locals", (ref-1)/2
	if ref%2 == 0 {
		cmd, frame = "globals", 0
	}
	if reply, ok := s.askChild(debug.Message{Cmd: cmd, Frame: frame}); ok {
		for _, v := range reply.Vars {
			vars = append(vars, map[string]any{
				"name": v.Name, "value": v.Value, "type": v.Type, "variablesReference": 0,
				"memoryReference": fmt.Sprintf("0x%x", v.Address),
			})
		}
	}
	s.reply(seq, "variables", true, map[string]any{"variables": vars})
}

// evaluate 是悬停那一条：只接受已有名字的位置——这一帧上的局部量与顶层那些静态槽里的名字。
// 查不到时回失败而不是回空串，编辑器才会把这一行灰掉，而不是显示一个假值。
//
// en: evaluate is the hover request: it only accepts positions of names that already exist -
// locals in this frame and names in the top-level static slots. When a name is not found, it
// answers failure rather than an empty string, so the editor greys the row out instead of
// showing a phantom value.
func (s *server) evaluate(seq int, args map[string]any) {
	name, _ := args["expression"].(string)
	frame := intOf(args["frameId"]) - 1 // 编辑器那边的帧号从 1 起，子进程里从 0 起
	reply, ok := s.askChild(debug.Message{Cmd: "evaluate", Frame: frame, Name: name})
	if !ok || !reply.Found {
		s.reply(seq, "evaluate", false, map[string]any{
			"message": "这一帧和顶层都没有 " + name + " 这个东西。表达式还算不了：要看中间结果，先 let 一个名字出来。",
		})
		return
	}
	s.reply(seq, "evaluate", true, map[string]any{"result": reply.Value, "variablesReference": 0})
}

// readMemory 给内存视图抄一段字节。地址是编辑器存回去的那一串十六进制，加上它给的偏移。
//
// en: readMemory copies a run of bytes for the memory view. The address is the hex string the
// editor handed back, plus the offset it supplies.
func (s *server) readMemory(seq int, args map[string]any) {
	ref, _ := args["memoryReference"].(string)
	addr, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(ref, "0x"), "0X"), 16, 64)
	if err != nil {
		s.reply(seq, "readMemory", false, map[string]any{"message": "内存地址读不成数：" + ref})
		return
	}
	addr += uint64(intOf(args["offset"]))
	count := intOf(args["count"])
	if count <= 0 {
		count = 256
	}
	reply, ok := s.askChild(debug.Message{Cmd: "memory", Address: addr, Count: count})
	if !ok || reply.Data == "" {
		s.reply(seq, "readMemory", false, map[string]any{"message": "那块内存读不到"})
		return
	}
	s.reply(seq, "readMemory", true, map[string]any{"data": reply.Data, "address": ref})
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// 答复与事件都带这一侧自己的序号；写要带锁：事件与答复可能同时往外挤。
//
// en: Responses and events both carry this side's own sequence number; writing takes a lock,
// because an event and a response may try to go out at the same moment.
type writer struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *writer) send(m map[string]any) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprintf(w.w, "Content-Length: %d\r\n\r\n", len(b))
	w.w.Write(b)
}

func (s *server) reply(seq int, cmd string, ok bool, body map[string]any) {
	s.seqMu.Lock()
	s.seq++
	n := s.seq
	s.seqMu.Unlock()
	m := map[string]any{
		"seq": n, "type": "response", "request_seq": seq,
		"command": cmd, "success": ok,
	}
	if body != nil {
		m["body"] = body
	}
	s.out.send(m)
}

func (s *server) event(name string, body map[string]any) {
	s.seqMu.Lock()
	s.seq++
	n := s.seq
	s.seqMu.Unlock()
	s.out.send(map[string]any{"seq": n, "type": "event", "event": name, "body": body})
}

// report 把子进程报上来的消息翻译成编辑器能识别的事件。
//
// en: report translates what the child process sent up into events the editor understands.
func (s *server) report(ev debug.Message) {
	switch ev.Event {
	case "stopped":
		s.event("stopped", map[string]any{
			"reason": ev.Reason, "threadId": threadID, "allThreadsStopped": true,
		})
	case "exit":
		code := 0
		if s.wait != nil {
			code = s.wait()
		}
		s.event("exited", map[string]any{"exitCode": code})
		s.event("terminated", map[string]any{})
	}
}

// 读那一侧：分帧与语言服务器同一套（Content-Length 加一段 JSON）。
//
// en: The reading side: framing is the same as the language server's (Content-Length plus a
// JSON body).
type reader struct {
	r *bufio.Reader
}

func newReader(r io.Reader) *reader { return &reader{r: bufio.NewReader(r)} }

func (rd *reader) read() (map[string]any, error) {
	length := -1
	for {
		line, err := rd.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("报文头里的长度不是个数：%q", line)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("报文头里没有 Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(rd.r, body); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

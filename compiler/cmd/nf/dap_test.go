// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// dap_test.go 端到端跑一趟调试：真起一个 `nf dap`（它就是适配器），按调试协议跟它说话，
// 让它去起被调试的那个子进程。断点要真停、变量要真读得出来、继续之后程序要实际运行完。
//
// 程序的屏幕输出走协议里的 output 事件回来，测试这一侧不另去接子进程的管道：
// 子进程的标准输出由适配器握着。
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 最后那两行是给"暂停"留的：停在 console.print(k) 上之后再按暂停，得还能停在下一句上。
const dapProgram = "method twice(n: int): int {\n    let m: int = n * 2\n    return m\n}\n\nlet k: int = twice(21)\nconsole.print(k)\nconsole.print(k + 1)\n"

type dapClient struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	seq     int
	replies chan map[string]any
	events  chan map[string]any
}

func startDAP(t *testing.T, exe string) *dapClient {
	t.Helper()
	cmd := exec.Command(exe, "dap")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("接不上标准输入：%v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("接不上标准输出：%v", err)
	}
	c := &dapClient{t: t, cmd: cmd, stdin: stdin,
		replies: make(chan map[string]any, 32), events: make(chan map[string]any, 32)}
	if err := cmd.Start(); err != nil {
		t.Fatalf("起不来 nf dap：%v", err)
	}
	go c.readLoop(bufio.NewReader(stdout))
	return c
}

// readLoop 拆适配器写回来的报文：答复归 replies，事件归 events。
func (c *dapClient) readLoop(r *bufio.Reader) {
	defer close(c.replies)
	defer close(c.events)
	for {
		body, err := readFrame(r)
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal([]byte(body), &m) != nil {
			continue
		}
		if m["type"] == "response" {
			c.replies <- m
			continue
		}
		c.events <- m
	}
}

func readFrame(r *bufio.Reader) (string, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return "", err
			}
			length = n
		}
	}
	if length < 0 {
		return "", fmt.Errorf("报文头里没有 Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return "", err
	}
	return string(body), nil
}

// request 写一条请求并等它的答复：调试协议靠 request_seq 对号，这里一条一条来，
// 对不上号就是适配器自己乱了。
func (c *dapClient) request(cmd string, args map[string]any) map[string]any {
	c.t.Helper()
	c.seq++
	m := map[string]any{"seq": c.seq, "type": "request", "command": cmd}
	if args != nil {
		m["arguments"] = args
	}
	body, err := json.Marshal(m)
	if err != nil {
		c.t.Fatalf("拼不出请求：%v", err)
	}
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatalf("写不进请求：%v", err)
	}
	select {
	case got, ok := <-c.replies:
		if !ok {
			c.t.Fatalf("等 %s 的答复时那一头断了", cmd)
		}
		if num(got["request_seq"]) != c.seq {
			c.t.Fatalf("%s 的答复对不上号：要 %d，得到 %v", cmd, c.seq, got["request_seq"])
		}
		if got["success"] != true {
			c.t.Fatalf("%s 回的是失败：%v", cmd, got["body"])
		}
		return got
	case <-time.After(60 * time.Second):
		c.t.Fatalf("等 %s 的答复超时", cmd)
	}
	return nil
}

// requestFail 与 request 一样写一条请求等答复，但允许那一侧回失败：
// 用来钉"该拒绝的地方要真拒绝"。返回 (body, success)。
func (c *dapClient) requestFail(cmd string, args map[string]any) (map[string]any, bool) {
	c.t.Helper()
	c.seq++
	m := map[string]any{"seq": c.seq, "type": "request", "command": cmd}
	if args != nil {
		m["arguments"] = args
	}
	body, err := json.Marshal(m)
	if err != nil {
		c.t.Fatalf("拼不出请求：%v", err)
	}
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatalf("写不进请求：%v", err)
	}
	select {
	case got, ok := <-c.replies:
		if !ok {
			c.t.Fatalf("等 %s 的答复时那一头断了", cmd)
		}
		return bodyOf(got), got["success"] == true
	case <-time.After(60 * time.Second):
		c.t.Fatalf("等 %s 的答复超时", cmd)
	}
	return nil, false
}

// wantEvent 等一条事件；kind 对不上就接着等（协议里事件会串着来）。
func (c *dapClient) wantEvent(kind string) map[string]any {
	c.t.Helper()
	select {
	case got, ok := <-c.events:
		if !ok {
			c.t.Fatalf("等事件 %s 时那一头断了", kind)
		}
		if got["event"] != kind {
			c.t.Fatalf("等事件 %s，先来的是 %v", kind, got["event"])
		}
		return got
	case <-time.After(60 * time.Second):
		c.t.Fatalf("等事件 %s 超时", kind)
	}
	return nil
}

// takeEvent 在一段时间里收事件，收满指定的那一种就返回；顺手把 output 攒进 out。
func (c *dapClient) takeEvent(kind string, out *strings.Builder) map[string]any {
	c.t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case got, ok := <-c.events:
			if !ok {
				c.t.Fatalf("等事件 %s 时那一头断了", kind)
			}
			if got["event"] == kind {
				return got
			}
			if got["event"] == "output" {
				b, _ := got["body"].(map[string]any)
				out.WriteString(fmt.Sprint(b["output"]))
			}
		case <-deadline:
			c.t.Fatalf("等事件 %s 超时，已经攒到的输出：%q", kind, out.String())
		}
	}
}

func num(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return -1
}

func bodyOf(m map[string]any) map[string]any {
	b, _ := m["body"].(map[string]any)
	if b == nil {
		return map[string]any{}
	}
	return b
}

// listOf 把答复里那一段数组取出来；不是数组就当空，让调用方的断言去说"少了什么"。
func listOf(v any) []any {
	list, _ := v.([]any)
	return list
}

func TestDebugSessionEndToEnd(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "main.nf")
	if err := os.WriteFile(src, []byte(dapProgram), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	c := startDAP(t, nf)
	defer c.cmd.Process.Kill()

	c.request("initialize", map[string]any{"adapterID": "nf"})
	c.wantEvent("initialized")
	c.request("launch", map[string]any{"program": filepath.ToSlash(src)})

	// 第 3 行是 `return m`：那会儿 m 刚算出来，正好看得到值。
	got := c.request("setBreakpoints", map[string]any{
		"source":      map[string]any{"path": filepath.ToSlash(src)},
		"breakpoints": []map[string]any{{"line": 3}},
	})
	list, _ := bodyOf(got)["breakpoints"].([]any)
	if len(list) != 1 {
		t.Fatalf("一条断点该回一条，得到 %v", bodyOf(got)["breakpoints"])
	}
	if bp, _ := list[0].(map[string]any); bp["verified"] != true {
		t.Fatalf("第 3 行该钉得住，实际：%v", bp)
	}
	c.request("configurationDone", nil)

	stop := c.wantEvent("stopped")
	if bodyOf(stop)["reason"] != "breakpoint" {
		t.Errorf("停下来的理由不对：%v", bodyOf(stop))
	}

	st := c.request("stackTrace", map[string]any{"threadId": 1})
	frames, _ := bodyOf(st)["stackFrames"].([]any)
	if len(frames) < 1 {
		t.Fatalf("调用栈是空的：%v", bodyOf(st))
	}
	top, _ := frames[0].(map[string]any)
	if num(top["line"]) != 3 || top["name"] != "twice" {
		t.Errorf("栈顶不对：%v", top)
	}

	sc := c.request("scopes", map[string]any{"frameId": num(top["id"])})
	scopes, _ := bodyOf(sc)["scopes"].([]any)
	if len(scopes) != 2 {
		t.Fatalf("一帧该有局部与静态两个作用域，得到 %v", bodyOf(sc))
	}
	ref := num(scopes[0].(map[string]any)["variablesReference"])
	name0, _ := scopes[0].(map[string]any)["name"].(string)
	if name0 != "局部" {
		t.Errorf("第一个作用域该叫局部，得到 %q", name0)
	}
	// 协议里 variablesReference 为 0 说的是"这一格没有可取的内容"，
	// 编辑器就干脆不来问第二遍——变量栏会整个空着。第一帧的号必须非 0。
	if ref == 0 {
		t.Fatalf("作用域的引用号是 0，编辑器会当成没有变量可取：%v", bodyOf(sc))
	}
	vr := c.request("variables", map[string]any{"variablesReference": ref})
	vars, _ := bodyOf(vr)["variables"].([]any)
	want := map[string]string{"n": "21", "m": "42"}
	if len(vars) != len(want) {
		t.Fatalf("该看到 n 与 m 两格，得到 %v", bodyOf(vr))
	}
	refs := map[string]string{}
	for _, v := range vars {
		vm, _ := v.(map[string]any)
		name, _ := vm["name"].(string)
		refs[name], _ = vm["memoryReference"].(string)
		if w, ok := want[name]; !ok {
			t.Errorf("多出来一个名字 %q", name)
		} else if vm["value"] != w {
			t.Errorf("%s = %v，要 %s", name, vm["value"], w)
		}
	}

	// 悬停：鼠标停在 m 上问一句，该拿到 42。
	ev := c.request("evaluate", map[string]any{"expression": "m", "frameId": num(top["id"]), "context": "hover"})
	if bodyOf(ev)["result"] != "42" {
		t.Errorf("悬停 m 该得到 42，实际 %v", bodyOf(ev))
	}
	// 问一个这一帧上没有的名字：要回失败，不能回一个空值装作读出来了。
	if body, ok := c.requestFail("evaluate", map[string]any{"expression": "没这个", "frameId": num(top["id"]), "context": "hover"}); ok {
		t.Errorf("认不得的名字该回失败，实际回了成功：%v", body)
	}

	// 内存视图：从 m 那个位置的地址抄 8 个字节，应当是 42 的小端摆法。
	if refs["m"] == "" {
		t.Fatal("变量那一格没带内存地址，内存视图开不起来")
	}
	mem := c.request("readMemory", map[string]any{"memoryReference": refs["m"], "count": 8})
	raw, err := base64.StdEncoding.DecodeString(fmt.Sprint(bodyOf(mem)["data"]))
	if err != nil || len(raw) != 8 || binary.LittleEndian.Uint64(raw) != 42 {
		t.Errorf("抄 m 那一格得到 %x（err %v），要 42 的小端八字节", raw, err)
	}

	// 单步一次：从被调函数的 return 走回上一层，理由该是 step。
	c.request("next", nil)
	if bodyOf(c.wantEvent("stopped"))["reason"] != "step" {
		t.Errorf("单步之后报的理由不对")
	}

	// 走回顶层那一帧：`let k` 不在帧上，它住在静态槽里，"静态"那一栏列的就是它。
	st2 := c.request("stackTrace", map[string]any{"threadId": 1})
	frames2, _ := bodyOf(st2)["stackFrames"].([]any)
	if len(frames2) < 1 {
		t.Fatalf("单步之后调用栈是空的：%v", bodyOf(st2))
	}
	f0, _ := frames2[0].(map[string]any)
	if num(f0["line"]) == 3 {
		t.Fatalf("单步该走出 twice，栈顶还在第 3 行：%v", f0)
	}
	sc2 := c.request("scopes", map[string]any{"frameId": num(f0["id"])})
	scopes2, _ := bodyOf(sc2)["scopes"].([]any)
	staticRef := 0
	for _, sc := range scopes2 {
		sm, _ := sc.(map[string]any)
		if sm["name"] == "静态" {
			staticRef = num(sm["variablesReference"])
		}
	}
	if staticRef == 0 {
		t.Fatalf("两个作用域里没有叫静态的那一个：%v", bodyOf(sc2))
	}
	gv := c.request("variables", map[string]any{"variablesReference": staticRef})
	staticVars := map[string]string{}
	for _, v := range listOf(bodyOf(gv)["variables"]) {
		vm, _ := v.(map[string]any)
		staticVars[fmt.Sprint(vm["name"])] = fmt.Sprint(vm["value"])
	}
	if staticVars["k"] != "42" {
		t.Errorf("静态那一栏该列出 k = 42，得到 %v", staticVars)
	}
	// 悬停同一个名字：顶层那一帧上问 k，也该答得出来。
	hv := c.request("evaluate", map[string]any{"expression": "k", "frameId": num(f0["id"]), "context": "hover"})
	if bodyOf(hv)["result"] != "42" {
		t.Errorf("悬停 k 该得到 42，实际 %v", bodyOf(hv))
	}

	// 暂停：先按暂停再放行，下一条语句就该停住，理由报 pause。
	// （跑的时候每句都回宿主问一次，所以这块牌子下一句就看得见。）
	// 等的时候中间会先夹一条输出事件——被停住的那一句本身要往屏幕写字，正常事。
	c.request("pause", nil)
	c.request("continue", nil)
	var out strings.Builder
	paused := c.takeEvent("stopped", &out)
	if bodyOf(paused)["reason"] != "pause" {
		t.Errorf("按了暂停该停在下一句，理由该是 pause，实际 %v", bodyOf(paused))
	}

	c.request("continue", nil)
	c.takeEvent("terminated", &out)
	if !strings.Contains(out.String(), "42") {
		t.Errorf("程序自己印的东西没看到，只看到 %q", out.String())
	}
}

// 一行上没有语句时，断点要照实说钉不住——不然编辑器画一个实心点，程序跑到那儿却不停。
func TestBreakpointOnALineWithNoStatement(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "main.nf")
	if err := os.WriteFile(src, []byte(dapProgram), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	c := startDAP(t, nf)
	defer c.cmd.Process.Kill()

	c.request("initialize", nil)
	c.wantEvent("initialized")
	c.request("launch", map[string]any{"program": filepath.ToSlash(src)})
	got := c.request("setBreakpoints", map[string]any{
		"source":      map[string]any{"path": filepath.ToSlash(src)},
		"breakpoints": []map[string]any{{"line": 4}}, // 那一行只有一个右花括号
	})
	list, _ := bodyOf(got)["breakpoints"].([]any)
	if len(list) != 1 {
		t.Fatalf("该回一条，得到 %v", bodyOf(got))
	}
	bp, _ := list[0].(map[string]any)
	if bp["verified"] == true {
		t.Errorf("只有右花括号的一行钉不住，别报成钉上了：%v", bp)
	}
	if msg, _ := bp["message"].(string); msg == "" {
		t.Errorf("钉不住要说清为什么：%v", bp)
	}
}

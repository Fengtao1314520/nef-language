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

// countEvent 在两秒里把送进来的事件数一遍，看指定那一种来了几条。
// 停过一次之后再放行，程序就该一路跑到结束；同一条抛出被问第二遍会在这儿露出来。
func (c *dapClient) countEvent(kind string) int {
	c.t.Helper()
	n := 0
	for {
		select {
		case got, ok := <-c.events:
			if !ok {
				return n
			}
			if got["event"] == kind {
				n++
			}
		case <-time.After(2 * time.Second):
			return n
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
	// 第 6 行写下的是 `let k: int = twice(21)`：上一层的位置就是那一次调用之后的第一个字节，
	// 所以它该说第 6 行——不是再外面一层的位置（早先在这儿读错了一格）。
	if len(frames) > 1 {
		f1, _ := frames[1].(map[string]any)
		if num(f1["line"]) != 6 {
			t.Errorf("上一层该在第 6 行，得到 %v", f1)
		}
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
	// 协议里 variablesReference 为 0 说的是"那个位置没有可取的内容"，
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

// dapThrowProgram 是抛出那一条路的样例：第 7 行那一次有人接住，第 12 行那一次没人接住，
// 两次都由运行期自己抛出（第 2 行那个下标越界 10031）——最常见的就是这一种。
// 停下时要看的栈顶是 pick 那一帧（第 2 行），上一层是写下那一次调用的第 12 行。
const dapThrowProgram = "method pick(xs: list<int>, at: int): int {\n" +
	"    return xs[at]\n" +
	"}\n" +
	"\n" +
	"let nums: list<int> = [1, 2, 3]\n" +
	"try {\n" +
	"    console.print(pick(nums, 9))\n" +
	"} catch (code, msg, at) {\n" +
	"    console.print(\"有人接住\", code)\n" +
	"}\n" +
	"console.print(\"照旧往下跑\")\n" +
	"console.print(pick(nums, 9))\n"

// startThrowDebug 起一趟调试，launch 里带不带 breakOnThrow 由调用方说。
func startThrowDebug(t *testing.T, throwStop bool) (*dapClient, string, func()) {
	t.Helper()
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "p.nf")
	if err := os.WriteFile(src, []byte(dapThrowProgram), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	c := startDAP(t, nf)
	c.request("initialize", map[string]any{"adapterID": "nf"})
	c.wantEvent("initialized")
	launch := map[string]any{"program": filepath.ToSlash(src)}
	if throwStop {
		launch["breakOnThrow"] = true
	}
	c.request("launch", launch)
	c.request("configurationDone", nil)
	return c, filepath.Base(src), func() { c.cmd.Process.Kill() }
}

// breakOnThrow 开着：有人接住的那一次照旧跑完、不停，没人接住的那一次停在抛出那一句上。
// 停住时栈与那一帧的局部量都读得出来——陷阱在 @throw 那一步、往下走之前，中间那几帧还没被复用。
func TestBreakOnThrowStopsOnlyUncaught(t *testing.T) {
	c, name, kill := startThrowDebug(t, true)
	defer kill()

	var out strings.Builder
	stop := c.takeEvent("stopped", &out)
	if bodyOf(stop)["reason"] != "exception" {
		t.Fatalf("该停在抛出上，理由却是 %v（已经攒到的输出：%q）", bodyOf(stop), out.String())
	}

	st := c.request("stackTrace", map[string]any{"threadId": 1})
	frames := listOf(bodyOf(st)["stackFrames"])
	if len(frames) < 2 {
		t.Fatalf("栈里至少要有 pick 与顶层两层，得到 %v", bodyOf(st))
	}
	f0, _ := frames[0].(map[string]any)
	if f0["name"] != "pick" || num(f0["line"]) != 2 {
		t.Errorf("栈顶该是 pick 的第 2 行，得到 %v", f0)
	}
	f1, _ := frames[1].(map[string]any)
	if num(f1["line"]) != 12 {
		t.Errorf("上一层该是写下那一次调用的第 12 行，得到 %v", f1)
	}

	sc := c.request("scopes", map[string]any{"frameId": num(f0["id"])})
	ref := 0
	for _, s := range listOf(bodyOf(sc)["scopes"]) {
		sm, _ := s.(map[string]any)
		if sm["name"] == "局部" {
			ref = num(sm["variablesReference"])
		}
	}
	if ref == 0 {
		t.Fatalf("那一帧没有可问的局部作用域：%v", bodyOf(sc))
	}
	vr := c.request("variables", map[string]any{"variablesReference": ref})
	vals := map[string]string{}
	for _, v := range listOf(bodyOf(vr)["variables"]) {
		vm, _ := v.(map[string]any)
		vals[fmt.Sprint(vm["name"])] = fmt.Sprint(vm["value"])
	}
	if vals["at"] != "9" {
		t.Errorf("停住那一刻 at 该是 9，得到 %v", vals)
	}

	// 放行：这一次抛出走到最外面，屏幕上照旧是那一句"出错了…"与整条调用链。
	// 名字里带盘符那一段的位置文本只留文件名，所以链上那两行按 p.nf 认。
	c.request("continue", nil)
	c.takeEvent("terminated", &out)
	got := out.String()
	if !strings.Contains(got, "有人接住 10031") || !strings.Contains(got, "照旧往下跑") {
		t.Errorf("有人接住的那一次不该停，实际输出：%q", got)
	}
	want := []string{"出错了（10031）", name + " 2:14 pick", name + " 12:15 " + name}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("放行之后要看到的 %q 没看到，实际输出：%q", w, got)
		}
	}
}

// dapFinallyProgram 是往外走的那一条路上带着一段 `finally` 的那种：第 7 行那一次抛出没人接住，
// 走到第 9 行那一段收尾，收完之后由 `@rethrow` 接着往外走。
// 只写 `try / finally` 的那一层在处理器链上也是一条接住记录（落点是收尾那一段的开头），
// 所以 `@throw` 那一步判成"有人接住"、不问；问话落在 `@rethrow` 那一步，
// 停的位置是这一层收尾，不是最里那一处抛出。同一条抛出只问一次。
const dapFinallyProgram = "method pick(xs: list<int>, at: int): int {\n" +
	"    return xs[at]\n" +
	"}\n" +
	"\n" +
	"let nums: list<int> = [1, 2, 3]\n" +
	"try {\n" +
	"    console.print(pick(nums, 9))\n" +
	"} finally {\n" +
	"    console.print(\"收尾这一次\")\n" +
	"}\n"

// 没人接住的抛出带着一段收尾：只停一次，停过之后收尾照跑、程序按 1 结束。
func TestBreakOnThrowStopsOnceThroughFinally(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "q.nf")
	if err := os.WriteFile(src, []byte(dapFinallyProgram), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	c := startDAP(t, nf)
	defer c.cmd.Process.Kill()

	c.request("initialize", map[string]any{"adapterID": "nf"})
	c.wantEvent("initialized")
	c.request("launch", map[string]any{"program": filepath.ToSlash(src), "breakOnThrow": true})
	c.request("configurationDone", nil)

	var out strings.Builder
	stop := c.takeEvent("stopped", &out)
	if bodyOf(stop)["reason"] != "exception" {
		t.Fatalf("该停在没人接住的那一次抛出上，理由却是 %v（攒到的输出：%q）", bodyOf(stop), out.String())
	}
	st := c.request("stackTrace", map[string]any{"threadId": 1})
	frames := listOf(bodyOf(st)["stackFrames"])
	if len(frames) < 1 {
		t.Fatalf("调用栈是空的：%v", bodyOf(st))
	}
	c.request("continue", nil)
	c.takeEvent("terminated", &out)
	got := out.String()
	if !strings.Contains(got, "收尾这一次") {
		t.Errorf("停过之后那一段收尾要跑完，实际输出：%q", got)
	}
	if !strings.Contains(got, "出错了（10031）") {
		t.Errorf("放行之后要打出的那一句没看到，实际输出：%q", got)
	}
	// 同一条抛出不能问两遍：`@throw` 判成有人接住、`@rethrow` 才问，一次正好。
	if extra := c.countEvent("stopped"); extra != 0 {
		t.Errorf("同一条抛出停了两遍（多 %d 次）：%q", extra, got)
	}
}

// 开关关着（默认那一种）：一次也不停——程序一路跑到退出，
// 屏幕上仍是那一句"出错了…"与整条调用链。
func TestBreakOnThrowOffRunsThrough(t *testing.T) {
	c, name, kill := startThrowDebug(t, false)
	defer kill()

	var out strings.Builder
	c.takeEvent("terminated", &out)
	got := out.String()
	for _, w := range []string{"有人接住 10031", "照旧往下跑", "出错了（10031）", name + " 2:14 pick"} {
		if !strings.Contains(got, w) {
			t.Errorf("关着开关时也要看到的 %q 没看到，实际输出：%q", w, got)
		}
	}
}

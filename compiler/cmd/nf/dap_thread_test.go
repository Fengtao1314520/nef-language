// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package main

// dap_thread_test.go 钉的是 `nf dap` 只跟程序那一条线程那一条答复的落地。
//
// 实现落在宿主这一头（`cmd/nf/dap_windows.go` 的回调），不在产物的机器码里：那四条检查点指令
// 每一条都多两次内存访问不值当，而"要不要停"这件事本来就是宿主定的。回调进来时先比线程号，
// 不是程序那一条线程就直接放行。
//
// en: dap_thread_test.go pins the answer that nf dap follows the program's own
// thread only. The guard sits on the host side - in the callback in cmd/nf/dap_windows.go -
// rather than in the product's machine code, because paying two more memory reads at every
// checkpoint is not worth it and whether to stop was the host's decision anyway. The callback
// compares thread numbers first and lets anything that is not the program's own thread through.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workerBreakProgram 里只有那一条线程走得到第 3 行：主线程起完线程就等它，一行断点也不经过。
const workerBreakProgram = `
method work(n:int):int {
    let k:int = n + 1
    return k
}
let t:Thread<int> = thread.start(work, 1)
console.print(t.wait())
`

// TestDebugIgnoresWorkerThread 钉的是"不停"那一边：断点写在只有那一条线程走的第 3 行上，
// 宿主一次也不该停，程序照旧跑完并按 0 结束。
//
// 少了那一判，回调会从第二条线程走进来，会话把它的帧基当成程序那一条线程的现场记下来，
// 往线上送出一次 stopped，然后停在 `<-resume` 上等命令头的回话。这一份程序不会有人在
// 主线程上走到断点，命令头也就永远不会来：那一条线程一直挂着，程序走不到结束，
// 这里先红的是"程序没走到结束"那一句。反向验过一遍，去掉那一判实测就是这样。
//
// en: TestDebugIgnoresWorkerThread pins the "does not stop" side. The breakpoint sits on line 3,
// which only that thread reaches, so the host must never stop and the program must still finish
// with code 0. Without the check the callback arrives from the second thread, the session records
// its frame base as if it were the program's own scene, sends one stopped event on the wire and
// then waits on the resume channel for a command. Nobody reaches the breakpoint on the main
// thread in this program, so no command ever comes: that thread hangs, the program never finishes,
// and what turns red here first is "the program never reached the end". Verified in reverse once -
// removing the check reproduces exactly that.
func TestDebugIgnoresWorkerThread(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "main.nf")
	if err := os.WriteFile(src, []byte(workerBreakProgram), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	c := startDAP(t, nf)
	defer c.cmd.Process.Kill()

	c.request("initialize", map[string]any{"adapterID": "nf"})
	c.wantEvent("initialized")
	c.request("launch", map[string]any{"program": filepath.ToSlash(src)})
	got := c.request("setBreakpoints", map[string]any{
		"source":      map[string]any{"path": filepath.ToSlash(src)},
		"breakpoints": []map[string]any{{"line": 3}},
	})
	list := listOf(bodyOf(got)["breakpoints"])
	if len(list) != 1 {
		t.Fatalf("一条断点该回一条，得到 %v", list)
	}
	if bp, _ := list[0].(map[string]any); bp["verified"] != true {
		t.Fatalf("第 3 行该钉得住，实际：%v", bp)
	}
	c.request("configurationDone", nil)

	var out strings.Builder
	if ev := c.takeEvent("terminated", &out); ev == nil {
		t.Fatal("程序没走到结束")
	}
	if n := c.countEvent("stopped"); n != 0 {
		t.Fatalf("只有那一条线程经过断点，宿主一次也不该停，实际停了 %d 次\n输出 %q", n, out.String())
	}
	if !strings.Contains(out.String(), "2\n") {
		t.Fatalf("屏幕上没读到那一条线程交回来的数：%q", out.String())
	}
}

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

// dap.go 是 `nf dap` 那一条的入口。它有两种角色：不带文件就是跟编辑器说话的那一侧，
// 带一份 .nf 加 --debuggee 端口号就是被调试的那个子进程（那一种角色在 dap_windows.go）。
//
// 分成两个进程是因为程序跑完会自己调用 exit 叫操作系统结束整个进程：
// 同一进程里，程序一结束，跟编辑器的对话就跟着断了，连"程序结束了"这一句都发不出去。
//
// en: dap.go is the entry point of `nf dap`. It has two roles: with no file it is the side that
// talks to the editor; with a .nf plus --debuggee and a port it is the debugged child process
// (that role lives in dap_windows.go). Two processes because a finished program itself calls
// exit and asks the operating system to end the whole process: inside one process, the moment
// the program ends the conversation with the editor dies with it, and even "program ended"
// never gets sent.
package main

import (
	"io"
	"os"
	"os/exec"
	"strings"

	"neflang.org/nf/internal/dap"
)

func cmdDAP(args []string) int {
	if len(args) > 0 && strings.HasSuffix(args[0], ".nf") {
		return cmdDebuggee(args)
	}
	if err := dap.Serve(os.Stdin, os.Stdout, startDebuggee); err != nil {
		return 1
	}
	return 0
}

// startDebuggee 起的还是自己这一个可执行文件，只是换了角色。
// 程序往屏幕与标准错误上写的字逐段转给这一侧，由这一侧报成调试协议里的 output 事件。
//
// en: startDebuggee launches the very same executable, only in the other role. The text the
// program writes to the screen and to standard error is forwarded chunk by chunk to this side,
// which reports it as Debug Adapter Protocol output events.
func startDebuggee(program string, progArgs []string, port string, onOutput func(category, text string)) (func() int, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	argv := append([]string{"dap", program, "--debuggee", port}, progArgs...)
	cmd := exec.Command(exe, argv...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go pumpPipe(stdout, "stdout", onOutput)
	go pumpPipe(stderr, "stderr", onOutput)
	done := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		switch e := err.(type) {
		case nil:
			done <- 0
		case *exec.ExitError:
			done <- e.ExitCode()
		default:
			done <- -1
		}
	}()
	return func() int { return <-done }, nil
}

func pumpPipe(r io.Reader, category string, onOutput func(string, string)) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			onOutput(category, string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

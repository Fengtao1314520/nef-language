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

// Package exetest 把"把产物写到临时目录、真跑一遍、核对输出"这段共用出去。
//
// 后端每一层都要靠它验收：pe 要验封装、运行期要验例程、codegen 要验整条链。
// 这段逻辑原先在 pe 与 runtime 的测试里各有一份，一份比另一份少核退出码。
//
// en: Package exetest shares out the routine of writing a build product into a temporary
// directory, running it for real, and checking what it printed. Every backend layer uses it to
// accept its own work: pe checks the container, the runtime checks the routines, codegen checks
// the whole chain. This logic used to exist once in the pe tests and once in the runtime tests,
// and one of the two forgot to check the exit code.
package exetest

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Write 把 exe 字节落进一个只有本测试能碰到的临时目录，返回路径。
//
// en: Write puts the exe bytes into a temporary directory that only this test touches, and
// returns the path.
func Write(t *testing.T, exe []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, exe, 0o755); err != nil {
		t.Fatalf("写 %s 失败：%v", name, err)
	}
	return path
}

// Run 实际运行一个产物，返回它的标准输出、标准错误与退出码。
//
// 非零退出码不是失败——后端要拿它核对 `exit (码, 话)` 与"没人接的异常"，
// 所以只有进程起不来才算测试出错。
//
// en: Run executes a built product and returns its standard output, standard error and exit
// code. A non-zero exit code is not a failure: the backend uses it to check `exit (code, text)`
// and an exception nobody caught, so only a process that cannot start counts as a test error.
func Run(t *testing.T, exe []byte, name string, args ...string) Result {
	t.Helper()
	r := RunPath(t, Write(t, exe, name), args...)
	return r
}

// RunPath 同 Run，但跑一个已经在磁盘上的路径。
//
// en: RunPath is Run, but for a path that is already on disk.
func RunPath(t *testing.T, path string, args ...string) Result {
	t.Helper()
	return goRun(t, path, nil, args...)
}

// RunStdin 同 Run，另外把喂进去的字节接到产物的标准输入上。
// `console.read` 只能这样验：测试里没有真的键盘等一个回车。
//
// en: RunStdin is Run, but it also feeds the given bytes to the product's standard input.
// `console.read` can only be checked this way: a test has no real keyboard waiting for Enter.
func RunStdin(t *testing.T, exe []byte, name string, stdin string, args ...string) Result {
	t.Helper()
	return goRun(t, Write(t, exe, name), strings.NewReader(stdin), args...)
}

// goRun 是两条路共用的那一段：起进程、收两个流、把非零退出码当成结果而不是错误。
//
// en: goRun is the part both paths share: start the process, collect the two streams, and treat
// a non-zero exit code as a result rather than an error.
func goRun(t *testing.T, path string, stdin io.Reader, args ...string) Result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(path, args...)
	cmd.Stdin = stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: 0}
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("跑不起来 %s：%v（stdout=%q stderr=%q）", path, err, stdout.String(), stderr.String())
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: ee.ExitCode()}
}

// Result 是一次运行的结果。
//
// en: Result is the outcome of one run.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Want 核对标准输出与退出码，不对就把两边都打出来。
//
// en: Want checks the standard output and the exit code, and prints both sides when they
// disagree.
func (r Result) Want(t *testing.T, stdout string, code int) {
	t.Helper()
	if r.Stdout != stdout {
		t.Fatalf("输出不对：\n got %q\nwant %q", r.Stdout, stdout)
	}
	if r.Code != code {
		t.Fatalf("退出码是 %d，要 %d（输出 %q）", r.Code, code, r.Stdout)
	}
}

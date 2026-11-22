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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFmtSubcommandPicksUpTheEditorFormatter 钉 `nf fmt 文件.nf`：排好的文本与编辑器
// textDocument/formatting 用的那一份是同一套（缩进按花括号深度、记号间空白收齐），默认
// 打到标准输出，-w 时原地写回；不给文件是码 2，读不到是码 1。
//
// en: TestFmtSubcommandPicksUpTheEditorFormatter pins `nf fmt file.nf`: the laid-out text comes
// from the same formatter the editor's textDocument/formatting uses, goes to standard output by
// default, and is written back with -w; no file is exit 2 and an unreadable file is exit 1.
func TestFmtSubcommandPicksUpTheEditorFormatter(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "乱.nf")
	const raw = "method run(n:int):int {\nlet sum:int=0\nif(n>0) {\nsum = sum+n\n}\nreturn sum\n}\n"
	const want = "method run(n:int):int {\n    let sum:int = 0\n    if (n>0) {\n        sum = sum + n\n    }\n    return sum\n}\n"
	if err := os.WriteFile(src, []byte(raw), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := runNf(t, nf, "fmt", src)
	if code != 0 {
		t.Fatalf("`nf fmt` 退出码 %d，要 0", code)
	}
	if got = strings.ReplaceAll(got, "\r\n", "\n"); got != want {
		t.Fatalf("`nf fmt` 给\n%s\n要\n%s", got, want)
	}
	if _, code := runNf(t, nf, "fmt"); code != 2 {
		t.Fatalf("`nf fmt` 不给文件该是码 2，得到 %d", code)
	}
	if _, code := runNf(t, nf, "fmt", filepath.Join(dir, "不存在.nf")); code != 1 {
		t.Fatalf("`nf fmt` 读不到文件该是码 1，得到 %d", code)
	}
}

func TestFmtSubcommandWritesInPlace(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "乱.nf")
	const raw = "let a:int=1\nconsole.print(a)\n"
	const want = "let a:int = 1\nconsole.print(a)\n"
	if err := os.WriteFile(src, []byte(raw), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := runNf(t, nf, "fmt", "-w", src)
	if code != 0 {
		t.Fatalf("`nf fmt -w` 退出码 %d，要 0", code)
	}
	if got != "" {
		t.Fatalf("`nf fmt -w` 不该往标准输出写东西，得到 %q", got)
	}
	back, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if string(back) != want {
		t.Fatalf("写回的内容给 %q，要 %q", string(back), want)
	}
}

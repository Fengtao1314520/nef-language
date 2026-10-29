// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// runStdin 与 run 相同，另外把输入的字节传给产物进程的标准输入。
func runStdin(t *testing.T, src, stdin string) (string, int) {
	t.Helper()
	r := exetest.RunStdin(t, build(t, src), "t.exe", stdin)
	return r.Stdout, r.Code
}

// TestFileReadWhole 整份文件读入。一起覆盖三处边界行为：
// length 数的是码点不是字节、开头的 BOM 被去掉、`\r\n` 原样保留。
// 路径中含中文，同时验证 UTF-8→UTF-16 的那次转换。
func TestFileReadWhole(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "有中文.txt")
	if err := os.WriteFile(p, append([]byte("\xEF\xBB\xBF"), []byte("第一行\r\n第二行\n")...), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := run(t, `
let body:string = file.read("`+filepath.ToSlash(p)+`")
console.print(length(body))
console.print(body == "第一行`+"\\u000D"+`\n第二行\n")
console.print(length(file.read("`+filepath.ToSlash(p)+`")) )
`)
	if code != 0 {
		t.Fatalf("码 %d，输出 %q", code, got)
	}
	want := "9\ntrue\n9\n"
	if got != want {
		t.Fatalf("得到 %q，要 %q", got, want)
	}
}

// TestFileReadFails 固定两个错误码：打不开是 10041 号，读入内容不是合法 UTF-8 是 10044 号。
func TestFileReadFails(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "坏字节.bin")
	if err := os.WriteFile(bad, []byte{0xFF, 0xFE, 0x41}, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	cases := []struct{ src, want string }{
		{`console.print(file.read("` + filepath.ToSlash(bad) + `"))`, "10044）：文件不是合法的 UTF-8"},
		{`console.print(file.read("` + filepath.ToSlash(filepath.Join(dir, "没有这个文件.txt")) + `"))`, "10041）：读不到这个文件"},
	}
	for _, c := range cases {
		got, code := run(t, c.src+"\n")
		if code != 1 || !strings.Contains(got, c.want) {
			t.Fatalf("%s 得到 %q 码 %d，要含 %q", c.src, got, code, c.want)
		}
	}
}

// TestConsoleReadLines 验证按行读取：行尾的 `\n` 不进结果，它前面的 `\r` 也一并去掉；
// 后面没有可读内容时返回空文本。
func TestConsoleReadLines(t *testing.T) {
	got, code := runStdin(t, `
let a:string = console.read()
let b:string = console.read()
let c:string = console.read()
console.print(a == "hello", b == "带中文的一行", c == "")
`, "hello\r\n带中文的一行\n")
	want := "true true true\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestSampleLineCount 运行 linecount.nf：args、file.read、split、dict、try／catch 都在这份样例里。
// dict 的遍历顺序不保证，所以核对的是"预期的行都出现了没有"，不是先后顺序。
func TestSampleLineCount(t *testing.T) {
	const path = "../../../samples/linecount.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	src := string(raw)
	dir := t.TempDir()
	p := filepath.Join(dir, "喂给linecount.nf.txt")
	if err := os.WriteFile(p, []byte("甲\n乙丙\n\n丁\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, src), "linecount.exe", filepath.ToSlash(p))
	lines := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n") {
		lines[l] = true
	}
	want := []string{"长 1 的行有 2 行", "长 2 的行有 1 行", "长 0 的行有 2 行"}
	for _, w := range want {
		if !lines[w] {
			t.Fatalf("linecount.nf跑出来是 %q 码 %d，缺了 %q", r.Stdout, r.Code, w)
		}
	}
	if r.Code != 0 {
		t.Fatalf("linecount.nf退出码 %d，要 0（输出 %q）", r.Code, r.Stdout)
	}

	// 一个参数都不给：走 exit (100, …) 那条路径
	none := exetest.Run(t, buildAt(t, path, src), "linecount.exe")
	if !strings.Contains(none.Stdout, "要给我一个文件路径") || none.Code != 100 {
		t.Fatalf("不给参数时得到 %q 码 %d，要含那句提醒、码 100", none.Stdout, none.Code)
	}
}

// TestArgsFromCommandLine 实际携带参数运行产物：脚本名本身不计入；中文参数、含空格的一项、
// 含反斜杠的一项都要原样返回（按 Windows 的参数解析规则）。
func TestArgsFromCommandLine(t *testing.T) {
	exe := build(t, `
console.print(length(args))
for (i:int in 0 until length(args)) {
    console.print(i, args[i])
}
`)
	r := exetest.Run(t, exe, "t.exe", "甲", "带 空格", `a\b`, `"引号里的"`)
	want := "4\n0 甲\n1 带 空格\n2 a\\b\n3 \"引号里的\"\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", r.Stdout, r.Code, want)
	}
	none := exetest.Run(t, exe, "t.exe")
	if none.Stdout != "0\n" || none.Code != 0 {
		t.Fatalf("一个参数都不给时得到 %q 码 %d，要 \"0\\n\" 码 0", none.Stdout, none.Code)
	}
}

// TestConsoleReadEmpty 没有输入时：第一次读取就应得到空文本，不抛出也不阻塞。
func TestConsoleReadEmpty(t *testing.T) {
	got, code := runStdin(t, "console.print(console.read() == \"\")\n", "")
	if got != "true\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// diagnostic_test.go 固定"报错原话 + 行列"翻成编辑器那个位置波浪线的换算。
//
// 两件容易弄错的：nf 的列是 1 起的**字节**列（internal/lexer/lexer.go 的 advance 每吃一个字节
// 加一），LSP 的 character 是 0 起的 **UTF-16** 单元数，所以一行里只要有过中文或 emoji，
// 两个数就差好几；另一件是报错指到行尾之外时不能画出一条倒着走的波浪线。
package ls

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func onlyDiag(t *testing.T, path, text string) Diagnostic {
	t.Helper()
	got := Diagnostics(path, text)
	list := got[path]
	if len(list) != 1 {
		t.Fatalf("%s 上该只有一条诊断，实际 %d 条：%+v", path, len(list), list)
	}
	return list[0]
}

func wantRange(t *testing.T, d Diagnostic, line, from, to int) {
	t.Helper()
	if d.Range.Start.Line != line || d.Range.Start.Character != from || d.Range.End.Character != to {
		t.Errorf("波浪线位置不对\n想要：第 %d 行 %d～%d\n得到：第 %d 行 %d～%d",
			line, from, to, d.Range.Start.Line, d.Range.Start.Character, d.Range.End.Character)
	}
}

// 名字找不到：波浪线盖住那一个名字。
func TestCheckErrorUnderlinesTheName(t *testing.T) {
	const text = "method main(): int {\n    let n: int = tru\n    return n\n}\n"

	d := onlyDiag(t, "/p/p1.nf", text)
	if d.Message != "没有 tru 这个名字" {
		t.Errorf("报错原话不对：%s", d.Message)
	}
	// 源码第 2 行第 18 列（1 起）就是 tru 那三个字母。
	wantRange(t, d, 1, 17, 20)
	if d.Severity != 1 {
		t.Errorf("诊断等级该是 1（错），得到 %d", d.Severity)
	}
	if d.Source != "nf" {
		t.Errorf("来源该写 nf，得到 %q", d.Source)
	}
}

// 同一行前面有中文与 emoji 时，字节列要换成 UTF-16 列：甲乙丙各算一个位置，😀 算两格。
func TestByteColumnBecomesUTF16Column(t *testing.T) {
	const text = "method main(): int {\n    let s: string = \"甲乙😀丙\" + tru\n    return 0\n}\n"

	d := onlyDiag(t, "/p/p4.nf", text)
	if d.Message != "没有 tru 这个名字" {
		t.Errorf("报错原话不对：%s", d.Message)
	}
	wantRange(t, d, 1, 30, 33)
}

// 文本字面量的波浪线要连引号一起盖住：那一个记号在源码里是 "abc" 五个字符。
func TestStringLiteralUnderlinesQuotes(t *testing.T) {
	const text = "method main(): int {\n    let n: int = \"abc\"\n    return n\n}\n"

	d := onlyDiag(t, "/p/p6.nf", text)
	if d.Message != `这里要的是 int，给的是 string；类型之间不自动转换` {
		t.Errorf("报错原话不对：%s", d.Message)
	}
	wantRange(t, d, 1, 17, 22)
}

// 括号漏了，报错落在行尾之后：这时候退一个位置盖住行尾那一个字符，不能画在下一行去。
func TestErrorPastEndOfLineClampsToLastChar(t *testing.T) {
	const text = "method main(): int {\n    let n: int = (1 + 2\n    return n\n}\n"

	d := onlyDiag(t, "/p/p2.nf", text)
	if d.Message != `想要 ")"，实际看到 行尾` {
		t.Errorf("语法错的原文不对：%s", d.Message)
	}
	wantRange(t, d, 1, 22, 23)
}

// 词法错时整个记号流都拿不到，这一条也得画出来，不能一句答复都没有。
func TestLexerErrorIsStillReported(t *testing.T) {
	const text = "method main(): int {\n    console.print(\"abc\n    return 0\n}\n"

	d := onlyDiag(t, "/p/p3.nf", text)
	if d.Message != "字符串没有完成就换行了" {
		t.Errorf("词法错的原文不对：%s", d.Message)
	}
	if d.Range.Start.Line != 1 {
		t.Errorf("词法错该画在第 2 行，得到第 %d 行", d.Range.Start.Line+1)
	}
	if d.Range.End.Character <= d.Range.Start.Character {
		t.Errorf("波浪线是空的：%+v", d.Range)
	}
}

func TestCleanProgramHasNoDiagnostics(t *testing.T) {
	const text = "method main(): int {\n    let xs: list<int> = [1, 2]\n    return length(xs)\n}\n"

	got := Diagnostics("/p/clean.nf", text)
	if len(got["/p/clean.nf"]) != 0 {
		t.Errorf("一份没毛病的程序不该有诊断：%+v", got["/p/clean.nf"])
	}
}

// import 那一侧读不进来时，要说清是哪一句 import 找不到文件，别只报"没有这个名字"。
func TestMissingImportFilePointsAtTheImport(t *testing.T) {
	const text = "import { half } from \"./gone.nf\"\n\nmethod main(): int {\n    return 0\n}\n"

	got := Diagnostics("/p/main.nf", text)
	var found *Diagnostic
	for i, d := range got["/p/main.nf"] {
		if d.Message == `读不到 ./gone.nf 这个文件` {
			found = &got["/p/main.nf"][i]
		}
	}
	if found == nil {
		t.Fatalf("少了那一条读不到文件的诊断：%+v", got["/p/main.nf"])
	}
	if found.Range.Start.Line != 0 || found.Range.Start.Character != 21 {
		t.Errorf("该盖住 import 里那个路径，得到 %+v", found.Range)
	}
}

// 错在 import 进来的那一份文件里时，诊断要按文件分开返回，编辑器才能画到对的那一个页签上。
func TestErrorsAreGroupedByFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.nf")
	utilPath := filepath.Join(dir, "util.nf")
	write(t, mainPath, "import { half } from \"./util.nf\"\n\nmethod main(): int {\n    return half(6)\n}\n")
	write(t, utilPath, "export method half(x: int): int {\n    return x / 2 + nope\n}\n")

	entry := filepath.ToSlash(mainPath)
	other := filepath.ToSlash(utilPath)
	got := Diagnostics(entry, read(t, mainPath))

	if len(got[entry]) != 0 {
		t.Errorf("main.nf 本身没错，却有诊断：%+v", got[entry])
	}
	list := got[other]
	if len(list) != 1 {
		t.Fatalf("util.nf 上该有一条诊断，实际 %d 条：%+v", len(list), list)
	}
	if list[0].Message != "没有 nope 这个名字" {
		t.Errorf("报错原话不对：%s", list[0].Message)
	}
	if list[0].Range.Start.Line != 1 || list[0].Range.Start.Character != 19 {
		t.Errorf("行列不对：%+v", list[0].Range)
	}
	if len(got) != 2 {
		t.Errorf("该只有这两份文件有账，得到 %d 份：%v", len(got), got)
	}
}

// 整块挂名那一句 `import greet from "libs/greet"` 走的是 pkgload 那套规则：那一个
// 路径是一个目录或一份容器，不是一份 .nf 文件。装载自己另算一份路径就会说"读不到 libs/greet
// 这个文件"，而命令行那一条编得过——两处各算各的就会出现这种不一致。
func TestPackageImportFollowsTheLoaderRules(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, filepath.Join("libs", "greet", "nf.json"), `{"name":"greet","version":"1.0.0"}`)
	writeIn(t, dir, filepath.Join("libs", "greet", "greet.nf"),
		"export method twice(n: int): int {\n    return n * 2 + gone\n}\n")
	entry := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	src := "import greet from \"libs/greet\"\n\nmethod main(): int {\n    return greet.twice(3)\n}\n"

	got := Diagnostics(entry, src)
	for _, d := range got[entry] {
		if strings.Contains(d.Message, "读不到") {
			t.Errorf("那一条路径是包目录，不该说读不到：%s", d.Message)
		}
	}
	var inside []Diagnostic
	for file, list := range got {
		if strings.HasSuffix(file, "greet.nf") {
			inside = list
		}
	}
	if len(inside) != 1 || inside[0].Message != "没有 gone 这个名字" {
		t.Fatalf("包里那一句该报在 libs/greet/greet.nf 上，实际分到的文件与诊断：%+v", got)
	}
}

func writeIn(t *testing.T, dir, rel, text string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("开目录 %s 失败：%v", filepath.Dir(full), err)
	}
	write(t, full, text)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("写 %s 失败：%v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	return string(src)
}

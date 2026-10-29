// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// webtest_examples_test.go 把 `webtest/libs/` 那两个包里的注释示例逐条编译一遍。
//
// 示例写在声明上面那一段注释里（`例：` 起头那一段，悬停把它显示成一个 nf 围栏），
// 句子讲的是"脚本里怎么用这一条"。它们只要求过检查，不要求真跑：
// 真跑要起浏览器，而这里要的只是"示例里的名字与写法对得上库现在的样子"。
//
// 每条示例都拼在同一个底子上检查：两句 import 加四个把手。底子放在临时目录里，
// 但那两句 import 要按 `libs/` 那条路径规则找得到包，所以临时目录里做的是
// `libs/web_test` 与 `libs/test_run` 两份包目录的拷贝（包不大，四份 .nf 与两份 nf.json）。
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// exampleProbe 是那层底子。名字与示例里用的那几个把手要对得上：改把手的名字要一起改示例。
const exampleProbe = `import wt from "libs/web_test"
import tr from "libs/test_run"

type Browser = wt.Browser
type Page = wt.Page
type Element = wt.Element
type Wire = wt.Frame

let browser: Browser = wt.current_browser()
let page: Page = wt.page_at(0)
let element: Element = page.find_element_by_css("h1")
let run: tr.Run = tr.begin_run("示例这一组", "https://example.com/", "out")
let key: bytes = bytes.new(4)
let block: bytes = bytes.new(8)
let answer: string = "{}"

method probe() {
%s
}
`

// libExampleDirs 是这两个包里的 .nf 门面；示例只从这两份里收（frame.nf 与 wire.nf 是包内的手脚，
// 外面叫不到，那两份的注释只配英文，不配示例）。
var libExampleFiles = []string{
	"libs/web_test/web_test.nf",
	"libs/test_run/test_run.nf",
}

var egMark = regexp.MustCompile(`^\s*//\s*(例：|Example:)`)

func TestWebTestCommentExamplesCompile(t *testing.T) {
	nf := nfExe(t)
	root := webtestDir(t)
	dir := t.TempDir()
	copyDir(t, filepath.Join(root, "libs"), filepath.Join(dir, "libs"))
	for _, f := range []string{"libs/web_test/nf.json", "libs/web_test/frame.nf", "libs/web_test/wire.nf",
		"libs/web_test/web_test.nf", "libs/test_run/nf.json", "libs/test_run/test_run.nf"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Fatalf("底子那份包没拷全（%s）：%v", f, err)
		}
	}
	checked := 0
	for _, rel := range libExampleFiles {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("读不到 %s：%v", rel, err)
		}
		for _, eg := range examplesIn(string(raw)) {
			probe := filepath.Join(dir, "probe"+itoa(checked)+".nf")
			if err := os.WriteFile(probe, []byte(strings.Replace(exampleProbe, "%s", indent(eg.lines), 1)), 0o644); err != nil {
				t.Fatalf("写不下那一份底：%v", err)
			}
			out, code := runNf(t, nf, "check", probe)
			checked++
			if code != 0 {
				t.Errorf("%s 里 %s 那条示例编译不过：%s\n示例原文：\n%s", rel, eg.name, tail(out), strings.Join(eg.lines, "\n"))
			}
		}
	}
	if checked < 60 {
		t.Fatalf("只收到 %d 条示例，收法大概错了（两个包的门面上贴了 80 来条）", checked)
	}
	t.Logf("%s 那两份门面里的 %d 条示例逐条过了检查", "webtest/libs", checked)
}

type namedExample struct {
	name  string
	lines []string
}

// examplesIn 从一份 .nf 的文本里收"例："那一段：从那一行往下，连续的行注释都算示例，
// 空一行或注释段结束就停。声明的名字取注释段下面第一行那一个 method。
func examplesIn(text string) []namedExample {
	lines := strings.Split(text, "\n")
	var out []namedExample
	for i, l := range lines {
		if !egMark.MatchString(l) {
			continue
		}
		var body []string
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(t, "//") {
				break
			}
			body = append(body, strings.TrimSpace(strings.TrimPrefix(t, "//")))
		}
		if len(body) == 0 {
			continue
		}
		name := ""
		for j := i + len(body) + 1; j < len(lines) && j < i+12; j++ {
			if m := declName.FindStringSubmatch(lines[j]); m != nil {
				name = m[1] + "#" + itoa(len(out))
				break
			}
		}
		if name == "" {
			name = "#" + itoa(len(out))
		}
		out = append(out, namedExample{name: strings.TrimPrefix(name, "."), lines: body})
	}
	return out
}

var declName = regexp.MustCompile(`^\s*(?:export\s+)?(?:method|class|const|let|type|enum)\s+([A-Za-z_]*)`)

func indent(lines []string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, "    "+l)
	}
	return strings.Join(out, "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// copyDir 把一份目录整个拷过去（包很小：两份 .nf 与一份 nf.json，一个目录三个文件）。
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("读不到 %s：%v", from, err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("开不了 %s：%v", to, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			copyDir(t, filepath.Join(from, e.Name()), filepath.Join(to, e.Name()))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatalf("读不到 %s：%v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), raw, 0o644); err != nil {
			t.Fatalf("写不下 %s：%v", e.Name(), err)
		}
	}
}

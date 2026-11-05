// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// decl_test.go 钉按名字找注释这一条：调试器手上只有名字与它在哪一份文件里，答复要的是编辑器
// 悬停里看到的同一段话（中文、英文、示例三段）；名字认不出、文件读不到都答空串。
//
// en: decl_test.go pins finding a comment by name: the debug adapter holds a name and a file, and
// must get back the same three sections the editor's hover shows. An unknown name or an unreadable
// file answer with nothing.
package ls

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const docSample = `// 把两个数加起来。
// en: Adds two numbers together.
// 例：
// let s:int = add(1, 2)
method add(a:int, b:int):int {
    return a + b
}

// 这一次运行的总数。
let total:int = add(1, 2)
console.print(total)
`

func TestDocForNameReturnsTheSameThreeSections(t *testing.T) {
	path := writeSample(t, docSample)
	got := DocForName(path, "add")
	for _, want := range []string{"把两个数加起来", "Adds two numbers together", "let s:int = add(1, 2)"} {
		if !strings.Contains(got, want) {
			t.Errorf("add 的注释里没有 %q，得到：%q", want, got)
		}
	}
	if !strings.Contains(DocForName(path, "total"), "这一次运行的总数") {
		t.Errorf("顶层 let 的注释没读出来：%q", DocForName(path, "total"))
	}
}

func TestDocForNameIsQuietWhenThereIsNothingToShow(t *testing.T) {
	path := writeSample(t, docSample)
	if got := DocForName(path, "missing_name"); got != "" {
		t.Errorf("认不出的名字该答空，得到 %q", got)
	}
	// 上面没写注释的声明：那一个参数就没有注释。
	path2 := writeSample(t, "method twice(n:int):int {\n    return n * 2\n}\n")
	if got := DocForName(path2, "twice"); got != "" {
		t.Errorf("声明上面没写注释就该答空，得到 %q", got)
	}
	if got := DocForName(filepath.Join(filepath.Dir(path), "no-such-file.nf"), "add"); got != "" {
		t.Errorf("文件读不到就该答空，得到 %q", got)
	}
}

// writeSample 把一份文本落成临时目录里的 .nf 文件，返回它的路径（测试结束由临时目录一起收走）。
//
// en: writeSample puts one text into a .nf file in a temporary directory and returns its path; the
// directory cleans up with the test.
func writeSample(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.nf")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("写样本失败：%v", err)
	}
	return filepath.ToSlash(path)
}

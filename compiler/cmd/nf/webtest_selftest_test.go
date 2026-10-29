// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// 自动化库那份 README（`webtest/README.md`）里写了每份自测有多少项检查。
//
// 公开文档里的数字要么当场能核对，要么就会悄悄变旧，所以这一条把两边钉在一起：
// 每份自测里以汉字序号开头的检查条数，等于这张表里的数，也等于 README 那一行写的数。
// 加一项检查就要在 README 那一行改一个数，两边不一起改就检查失败。
//
// en: The library README states how many checks each self-test runs. A number in a published
// document is only trustworthy if it can be verified, so this test ties the two sides together:
// the count of Chinese-numeral check labels in each self-test must equal both the table below and
// the figure printed in that README line.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// selftestCounts 是每份自测应有的检查条数，以及 README 里那一行的原文。
var selftestCounts = []struct {
	file string
	n    int
	line string
}{
	{"frame_check.nf", 24, "`frame_check.nf`（24 项定值）"},
	{"socket_check.nf", 8, "`socket_check.nf`（8 项，用一对回环套接字）"},
	{"page_check.nf", 10, "`page_check.nf`（10 项，使用 `data/` 那四份真实答复原文）"},
	{"error_check.nf", 2, "`error_check.nf`（2 项，浏览器启动失败的两种情形）"},
	{"element_check.nf", 39, "`element_check.nf`（页面与元素部分，39 项"},
	{"site_check.nf", 8, "`site_check.nf`（8 项，打开真实站点"},
}

// caseLabel 认出一条检查的开头：写在字符串字面量最前面的汉字序号加顿号。
var caseLabel = regexp.MustCompile(`"([一二三四五六七八九十]+)、`)

func countCaseLabels(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	return len(caseLabel.FindAllStringIndex(strings.ReplaceAll(string(data), "\r\n", "\n"), -1))
}

func TestWebTestSelftestCountsMatchReadme(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "webtest", "selftest")
	readme, err := os.ReadFile(filepath.Join(root, "webtest", "README.md"))
	if err != nil {
		t.Fatalf("读 webtest/README.md 失败：%v", err)
	}
	text := strings.ReplaceAll(string(readme), "\r\n", "\n")
	for _, want := range selftestCounts {
		got := countCaseLabels(t, filepath.Join(dir, want.file))
		if got != want.n {
			t.Errorf("%s 实到 %d 项，表里写 %d 项：改动这一份自测之后要同时更新 `webtest/README.md` 那一行",
				want.file, got, want.n)
			continue
		}
		if !strings.Contains(text, want.line) {
			t.Errorf("webtest/README.md 里没有 %q 这一段：那份 README 标的条数（%d 项）与这张表不同步",
				want.line, want.n)
		}
	}
}

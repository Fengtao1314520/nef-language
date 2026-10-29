// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/lexer"
)

// TestSpecCodeBlocks 把本地那份规范文本里每一个 ```nef 代码块拿出来过一遍词法与语法。
// 规范里写出来的样子必须是编译器识别的写法；这条测试钉的是规范与实现之间的这条约束。
//
// 这条测试读的是本地规范文本，公开检出不带这一份；站上同一件事由
// `site/tools-site/check-site.mjs` 做——它把站上每个 ```nf 块真送进编译器过一遍。
// 所以这里区分两种情况：规范文本所在的目录整个不存在，是公开仓库的检出，跳过并说明；
// 目录在而文件读不出来，是本地出了问题，测试必须失败——这条约束一旦悄悄断开就查不出来。
func TestSpecCodeBlocks(t *testing.T) {
	specPath := filepath.Join("..", "..", "..", "internaldocs", "001_SPEC.md")
	src, err := os.ReadFile(specPath)
	if err != nil {
		if os.IsNotExist(err) {
			if _, dirErr := os.Stat(filepath.Join("..", "..", "..", "internaldocs")); os.IsNotExist(dirErr) {
				t.Skip("这份检出没有那一份规范全文（公开仓库不带它）：站上的 ```nf 块由 site/tools-site/check-site.mjs 逐段过编译器，是同一件事在公开那一头的做法")
			}
		}
		t.Fatalf("读不到规范文件，这条测试就没在测任何东西：%v", err)
	}
	blocks := specBlocks(string(src))
	if len(blocks) < 20 {
		t.Fatalf("从 %s 里只切出 %d 个 nef 代码块，切法大概错了", specPath, len(blocks))
	}
	seenFragment := 0
	for i, b := range blocks {
		if isFragment(b) {
			seenFragment++
			continue
		}
		toks, lerr := lexer.Lex(b)
		if lerr != nil {
			t.Errorf("第 %d 块词法报错：%v\n%s", i+1, lerr, head(b))
			continue
		}
		if _, perr := Parse("spec.nf", toks); perr != nil {
			t.Errorf("第 %d 块语法报错：%v\n%s", i+1, perr, head(b))
		}
	}
	t.Logf("共 %d 块，其中 %d 块是片段（跳过）", len(blocks), seenFragment)
}

func filepath_(parts ...string) ([]byte, error) {
	return os.ReadFile(strings.Join(cleanParts(parts), "/"))
}

func cleanParts(parts []string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p)
	}
	return out
}

// specBlocks 切出所有 ```nef 围栏里的内容。
func specBlocks(md string) []string {
	var out []string
	lines := strings.Split(md, "\n")
	var cur []string
	in := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		switch {
		case !in && t == "```nef":
			in, cur = true, nil
		case in && t == "```":
			in = false
			out = append(out, strings.Join(cur, "\n"))
		case in:
			cur = append(cur, ln)
		}
	}
	return out
}

// isFragment 识别那些只写了半句的代码块：规范里用来说明形状的片段。
func isFragment(b string) bool {
	s := strings.TrimSpace(b)
	if s == "" {
		return true
	}
	// 以赋值或表达式开头、没有语句结构的，算片段
	if strings.Contains(s, "\u0000占位") {
		return true
	}
	// 带中文"一类的"、`...` 省略号说明是形状示意
	if strings.Contains(s, "...") || strings.Contains(s, "一类") {
		return true
	}
	// 括号明显配不平
	if strings.Count(s, "{") != strings.Count(s, "}") {
		return true
	}
	return false
}

func head(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

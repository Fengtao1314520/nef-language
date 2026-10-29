// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// bilingual_test.go 钉住"中英双语注释"这条约定。
//
// 三个地方各查一件事：
//
//	compiler/ 里贴在顶层声明与文件头上的那一段中文注释，同一下面要有一段以 `// en: ` 起头的英文；
//	webtest/libs 那两个包里 export 出去的声明，注释里要有 en:，两份门面上的还要有 例：那一段；
//	samples/ 那七份示例程序的文件头要有 en:（这一份文件本身就是用法示例，不再另要示例）。
//
// Go 那一条按目录开：一个目录全做完了才进 bilingualDone，进去之后漏一处就红。
// 表里还没进去的目录只统计、不判，为的是让进度看得见而不是把没做完的事藏起来。
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// bilingualDone 是英文那一段已经配齐的 Go 目录（相对 compiler/）。
var bilingualDone = []string{
	"cmd/nf",
	"internal/ast",
	"internal/check",
	"internal/codegen",
	"internal/dap",
	"internal/debug",
	"internal/exetest",
	"internal/ir",
	"internal/lexer",
	"internal/lower",
	"internal/ls",
	"internal/parser",
	"internal/pe",
	"internal/pkgfmt",
	"internal/pkgload",
	"internal/rtfloat",
	"internal/rtjson",
	"internal/rtre",
	"internal/runtime",
	"internal/x64",
}

// 三个记号都要逐行找：注释是一段一段的行拼起来的，en: 那一句从来不是第一行。
// Go 的正则里 ^ 默认只认整段文字的开头，不加 (?m) 就等于只看第一行。
var goEnMark = regexp.MustCompile(`(?m)^\s*//\s*en:`)
var nfEnMark = regexp.MustCompile(`(?m)^\s*//\s*en:`)
var nfEgMark = regexp.MustCompile(`(?m)^\s*//\s*(例：|Example:)`)
var goDecl = regexp.MustCompile(`^(func|type|var|const|package)\b`)
var nfExport = regexp.MustCompile(`^export (class|method|const|type) (\w+)`)
var nfMember = regexp.MustCompile(`^    method (\w+)`)

func TestGoTopCommentsAreBilingual(t *testing.T) {
	root := filepath.Join(repoRoot(t), "compiler")
	done := map[string]bool{}
	for _, d := range bilingualDone {
		done[filepath.ToSlash(d)] = true
	}
	missing := map[string]int{}
	checked := map[string]int{}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读不到 %s：%v", dir, err)
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if e.Name() != "testdata" {
					walk(p)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(os.PathSeparator)))
			rel = strings.TrimPrefix(rel, "compiler/")
			list := strings.Split(rel, "/")
			dirRel := strings.Join(list[:len(list)-1], "/")
			for _, b := range topBlocks(p) {
				if !done[dirRel] {
					continue
				}
				checked[dirRel] += 1
				if !b.hasEn {
					missing[dirRel]++
					t.Errorf("%s 的 %s 那一段没有英文（要一行 // en: ...）：%s:%d", dirRel, b.decl, rel, b.line)
				}
			}
		}
	}
	walk(root)
	left := 0
	for _, d := range allGoDirs(t, root) {
		if !done[d] {
			left++
		}
	}
	t.Logf("已开的目录里 %d 段，逐段核过；还没开的目录 %d 个", sum(checked), left)
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

type block struct {
	decl  string
	line  int
	hasEn bool
}

// topBlocks 取一份 .go 文件里贴在顶层声明与文件头上的那一段注释。
// 判断照 go/ast 那条最朴素的读法：连续的行注释，下面紧跟着 func／type／var／const／package，
// 而且这一段前面是空行或文件开头（贴在代码中间的碎注释不算，那是给一行代码写的）。
func topBlocks(path string) []block {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(raw), "\n")
	var out []block
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			continue
		}
		j := i
		for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "//") {
			j++
		}
		if j >= len(lines) || !goDecl.MatchString(strings.TrimSpace(lines[j])) {
			i = j
			continue
		}
		if i > 0 && strings.TrimSpace(lines[i-1]) != "" && !strings.HasPrefix(strings.TrimSpace(lines[i-1]), "//") {
			i = j
			continue
		}
		seg := strings.Join(lines[i:j], "\n")
		out = append(out, block{
			decl:  strings.TrimSpace(lines[j]),
			line:  i + 1,
			hasEn: goEnMark.MatchString(seg),
		})
		i = j
	}
	return out
}

func allGoDirs(t *testing.T, root string) []string {
	seen := map[string]bool{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel := strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(root)+"/")
		rel = strings.TrimPrefix(rel, "compiler/")
		i := strings.LastIndex(rel, "/")
		if i > 0 {
			seen[rel[:i]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("走不动 compiler/：%v", err)
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// TestGoFilesCarryLicenseHeaders 查 compiler/ 里每一份 .go 文件的头一行。
// compiler/ 里全部 .go 文件已按 `nef-licensing/templates/FILE_HEADER.txt` 的模板加上许可头，
// 这条检查管的是往后的新文件：漏一处就红，不用等合规扫描工具在仓库外面报。
// `testdata/` 里的东西是编译器的输入样本，不进这一条。
func TestGoFilesCarryLicenseHeaders(t *testing.T) {
	root := filepath.Join(repoRoot(t), "compiler")
	mark := "// Copyright 2026 Feng Tao (Nate)"
	var missing []string
	seen := 0
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		seen++
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(raw), mark) {
			missing = append(missing, strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(root)+"/"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("走不动 compiler/：%v", err)
	}
	if len(missing) > 0 {
		t.Errorf("%d 份 .go 文件头上没有许可那一行：%s", len(missing), strings.Join(missing, " "))
	}
	t.Logf("compiler/ 里 %d 份 .go 文件全部带许可头", seen)
}

// TestWebTestExportsAreBilingual 查两个包的门面：export 出去的那一半要有英文与示例。
func TestWebTestExportsAreBilingual(t *testing.T) {
	root := filepath.Join(repoRoot(t), "webtest", "libs")
	facades := map[string]bool{
		filepath.Join(root, "web_test", "web_test.nf"): true,
		filepath.Join(root, "test_run", "test_run.nf"): true,
	}
	var files []string
	for _, dir := range []string{"web_test", "test_run"} {
		fs, err := filepath.Glob(filepath.Join(root, dir, "*.nf"))
		if err != nil {
			t.Fatalf("找不到包 %s：%v", dir, err)
		}
		files = append(files, fs...)
	}
	sort.Strings(files)
	total := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读不到 %s：%v", f, err)
		}
		lines := strings.Split(string(raw), "\n")
		cls := ""
		for i, l := range lines {
			isExport := nfExport.MatchString(l)
			isMember := cls != "" && nfMember.MatchString(l)
			if m := nfExport.FindStringSubmatch(l); m != nil && m[1] == "class" {
				cls = m[2]
			} else if strings.HasPrefix(l, "}") {
				cls = ""
			}
			if !isExport && !isMember {
				continue
			}
			k := i - 1
			for k >= 0 && strings.HasPrefix(strings.TrimSpace(lines[k]), "//") {
				k--
			}
			seg := strings.Join(lines[k+1:i], "\n")
			if !nfEnMark.MatchString(seg) {
				t.Errorf("%s 第 %d 行那条对外声明没有英文：%s", filepath.Base(f), i+1, strings.TrimSpace(l))
			}
			total++
			if !facades[f] {
				continue // 包内的手脚（frame.nf 与 wire.nf）只要英文：外面叫不到它们，示例写了也没法照抄
			}
			if !nfEgMark.MatchString(seg) {
				t.Errorf("%s 第 %d 行那条门面声明没有 例：那一段：%s", filepath.Base(f), i+1, strings.TrimSpace(l))
			}
		}
	}
	if total < 90 {
		t.Fatalf("只查到 %d 条对外声明，读法大概错了", total)
	}
	t.Logf("webtest 两个包里 %d 条对外声明都配了英文，两份门面上的还配了示例", total)
}

// TestSampleHeadersAreBilingual 查七份示例程序的文件头。
func TestSampleHeadersAreBilingual(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "samples")
	fs, err := filepath.Glob(filepath.Join(dir, "*.nf"))
	if err != nil || len(fs) == 0 {
		t.Fatalf("找不到 samples/ 里的示例程序：%v", err)
	}
	for _, f := range fs {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读不到 %s：%v", f, err)
		}
		lines := strings.Split(string(raw), "\n")
		i := 0
		for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			i++
		}
		if i == 0 {
			t.Errorf("%s 没有文件头注释：每一份示例程序头上都要有中文段与 `// en:` 那一段", filepath.Base(f))
			continue
		}
		if !strings.Contains(strings.Join(lines[:i], "\n"), "// en:") {
			t.Errorf("%s 的文件头没有英文那一段", filepath.Base(f))
		}
	}
}

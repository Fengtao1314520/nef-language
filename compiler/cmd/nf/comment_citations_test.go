// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// comment_citations_test.go 钉一条规矩：代码注释里不引用规范章节、裁定编号与内部文档路径。
//
// 注释要说的是这段代码做什么、为什么这么做、什么情况下不成立。规范与决定记录是另一层材料：
// 它们不在公开仓库里，读者点不到；写进注释既查不出对错，也会在规则改动之后变成假引用。
// 因此这一条一律拒绝，出现一处就红，报出文件与行号。
//
// 扫描范围是这批改过注释的代码文件：compiler/ 的 .go（testdata 除外，那些是编译器的输入样本）、
// samples/ 与 webtest/ 的 .nf、vscode 的 .ts 与 .mjs、site/tools-site 的 .mjs。
// 只看注释那一部分：整行注释（行首 //）与代码行之后的那段行尾注释。字符串字面量不参与——
// 报错原话与测试期望值是接口和断言的一部分，这一条管不着，也不该去动它们。
//
// 术语表里的词（交出、读走、带走、持有者、句柄、借用）不在拒绝之列：它们是这个项目定下的说法，
// 注释要和其他材料用同一套词。
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// citationPatterns 是注释里拒绝出现的写法：中文与英文两侧都要列，注释是双语的。
var citationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`§`),
	regexp.MustCompile(`\bSPEC\b`),
	regexp.MustCompile(`\bthe spec\b`),
	regexp.MustCompile(`决定记录`),
	regexp.MustCompile(`裁定记录`),
	regexp.MustCompile(`开发日志`),
	regexp.MustCompile(`里程碑`),
	regexp.MustCompile(`性能基线`),
	regexp.MustCompile(`中间码文档`),
	regexp.MustCompile(`编译器文档`),
	regexp.MustCompile(`文法文档`),
	regexp.MustCompile(`运行期例程文档`),
	regexp.MustCompile(`示例程序文档`),
	regexp.MustCompile(`测试与检查文档`),
	regexp.MustCompile(`internaldocs`),
	regexp.MustCompile(`docs/0\d\d`),
	regexp.MustCompile(`第 \d+ 条`),
	regexp.MustCompile(`第 \d+ 判`),
	regexp.MustCompile(`ruling \d+`),
	regexp.MustCompile(`decision log`),
	regexp.MustCompile(`development journal`),
	regexp.MustCompile(`performance baseline`),
	regexp.MustCompile(`intermediate-code document`),
	regexp.MustCompile(`release procedure document`),
	regexp.MustCompile(`compiler document`),
	regexp.MustCompile(`grammar document`),
	regexp.MustCompile(`testing document`),
	regexp.MustCompile(`sample program document`),
}

// commentPart 取一行里属于注释的那一段：整行注释返回全行，代码行返回第一个不在引号里的
// `//` 之后的内容，没有就返回空串。
func commentPart(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "//") {
		return t
	}
	var quote byte
	for i := 0; i < len(line)-1; i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			continue
		}
		if c == '/' && line[i+1] == '/' {
			return strings.TrimSpace(line[i:])
		}
	}
	return ""
}

func TestCommentsDoNotCiteSpecOrInternalDocs(t *testing.T) {
	root := repoRoot(t)
	var files []string

	compilerDir := filepath.Join(root, "compiler")
	if err := filepath.Walk(compilerDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("走不动 compiler/：%v", err)
	}

	for _, dir := range []string{"samples", "webtest", filepath.Join("vscode", "src"), filepath.Join("vscode", "tools"), filepath.Join("site", "tools-site")} {
		if err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".nf") || strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".mjs") {
				files = append(files, p)
			}
			return nil
		}); err != nil {
			t.Fatalf("走不动 %s：%v", dir, err)
		}
	}

	seen := map[string]bool{}
	var uniq []string
	for _, p := range files {
		// 这一份文件自己写着那些被禁的写法（它就是靠这些写法做检查的），不参与扫描。
		if filepath.Base(p) == "comment_citations_test.go" {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		uniq = append(uniq, p)
	}
	sort.Strings(uniq)

	hits := 0
	perFile := map[string]int{}
	for _, p := range uniq {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读不到 %s：%v", p, err)
		}
		rel := strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(root)+"/")
		for i, line := range strings.Split(string(raw), "\n") {
			comment := commentPart(line)
			if comment == "" {
				continue
			}
			for _, re := range citationPatterns {
				if re.MatchString(comment) {
					hits++
					perFile[rel]++
					t.Errorf("%s:%d 注释里引用了规范或内部文档（%s）：%s", rel, i+1, re.String(), comment)
					break
				}
			}
		}
	}
	t.Logf("扫了 %d 个文件，命中 %d 处，分布在 %d 个文件里", len(uniq), hits, len(perFile))
}

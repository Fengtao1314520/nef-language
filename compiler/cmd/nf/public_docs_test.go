// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// 公开目录里的文档不许引用只在本地留存的材料。
//
// 仓库根的忽略清单把过程记录与技术文档那两个目录挡在公开仓库之外。公开的那批文件——两份 README、
// 站上全部页面、扩展与自动化库的 README——只要写进规范章节号、决定条号或那两个目录里的路径，
// 别人克隆下来就打不开，那句话变成查无出处的自述。命中就报文件与行号。
// 唯一放行的是外部许可文本自己的条款号：同一行写着 Apache 或许可证时，那个号指的是那份文本的条目。
//
// en: Published files must not cite material that stays local. The repository's ignore list keeps
// the process records and the internal technical documents out of the public tree, so a section
// number, a ruling number or a path into those directories, written in a README or a site page,
// is a reference nobody who clones the repository can follow. Each hit prints its file and line.
// The one exemption is a clause number of an external license text: on a line naming Apache or a
// license, that number refers to a clause of that text.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// localOnlyCitations 是公开文件里不该出现的写法，每一项后面跟着它错在哪。
var localOnlyCitations = []*regexp.Regexp{
	regexp.MustCompile(`§`),
	regexp.MustCompile(`\bSPEC\b`),
	regexp.MustCompile(`internaldocs`),
	regexp.MustCompile(`docs/0\d\d`),
	regexp.MustCompile(`决定记录`),
	regexp.MustCompile(`开发日志`),
	regexp.MustCompile(`里程碑`),
	regexp.MustCompile(`测试与检查\.md`),
	regexp.MustCompile(`核对清单`),
	regexp.MustCompile(`第 ?\d+ ?条`),
}

// externalClauseLine 认出那一行讲的是外部许可文本自己的条款。
var externalClauseLine = regexp.MustCompile(`(?i)apache|许可证|许可条款|条款`)

func collectPublicDocs(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	out := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "README.zh-CN.md"),
		filepath.Join(root, "CHANGELOG.md"),
		filepath.Join(root, "webtest", "README.md"),
		filepath.Join(root, "vscode", "README.md"),
	}
	site := filepath.Join(root, "site")
	err := filepath.WalkDir(site, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == "node_modules" || name == ".vitepress" || name == "dist" || name == "cache" {
				return filepath.SkipDir
			}
			return nil
		}
		// site/README.md 是给在仓库里翻这个目录的人看的 readme，不是发布出去的页面。
		if strings.HasSuffix(name, ".md") && name != "README.md" {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("走站点目录失败：%v", err)
	}
	sort.Strings(out)
	return out
}

func TestPublicDocsDoNotCiteLocalOnlyMaterial(t *testing.T) {
	files := collectPublicDocs(t)
	if len(files) < 50 {
		t.Fatalf("公开文件只收到 %d 份：站点目录没走全？", len(files))
	}
	root := repoRoot(t)
	var hits []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s 失败：%v", f, err)
		}
		rel, _ := filepath.Rel(root, f)
		for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			for _, re := range localOnlyCitations {
				m := re.FindString(line)
				if m == "" {
					continue
				}
				if re.String() == `第 ?\d+ ?条` && externalClauseLine.MatchString(line) {
					continue
				}
				hits = append(hits, fmt.Sprintf("%s:%d  「%s」  %s",
					filepath.ToSlash(rel), i+1, m, clip(line, 88)))
			}
		}
	}
	if len(hits) > 0 {
		t.Errorf("公开文件里有 %d 处引用了只在本地留存的材料（别人克隆下来点不到）：\n%s",
			len(hits), strings.Join(hits, "\n"))
		return
	}
	t.Logf("扫了 %d 份公开文件，没有一处引用只在本地留存的材料", len(files))
}

// clip 把一行截到若干个 rune，报出来的时候不淹没终端。
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

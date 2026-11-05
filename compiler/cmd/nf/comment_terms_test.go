// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// 注释里的用词检查：本仓库定过的叫法不许退回旧名，也不许出现大厂黑话与 AI 口语。
//
// 与同目录另两份检查的分工：那一条管注释里不许引用只在本地留存的材料，`site/tools-site/rhythm.mjs`
// 的表管站上正文，这一条管其余代码注释（Go、Nef、TS、MJS）。新词进哪一张表由它可能出现的位置决定；
// 两处都会出现时两张表一起加，并注明另一处已经加过。
//
// 只扫注释部分，注释里被引号或反引号括起来的那一段（编译器原话、标识符）不参与比对：那一段是
// 程序输出或语言里的记号，动它要走版本那一轮。2026-10-31 之后这张表里的词在报错原话里一个都没有，
// 所以豁免只对将来新增的句子有效，不是给旧词留的口子。
//
// en: Terminology check for code comments. The other two checks cover citations in comments and the
// prose of the published site; this one covers Go, Nef, TypeScript and JavaScript comments. Quoted
// spans inside a comment (a compiler sentence, an identifier) are exempt, because that text is
// program output and changing it is a language-visible decision.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// bannedInComments 是注释里不许出现的写法，键是要找的词，值是应当改成的说法。
var bannedInComments = map[string]string{
	"交回": "返回", "那一格": "那个位置", "这一格": "这个位置", "空出一格": "留出一个位置",
	"真跑": "实际运行", "把手": "句柄", "每圈": "每轮", "拆盒": "拆箱", "答案卷": "对照表",
	"对拍": "双向比对", "只借不交": "只读借用", "挂上": "附加", "挂到": "附加到",
	"兜底": "默认处理", "口径": "计数范围", "落盘": "写入文件", "透传": "原样传给下游",
	"链路": "这条路径", "水位": "实测水平", "打通": "放开", "联动": "一起触发",
	"沉淀": "积累", "抓手": "关键措施", "闭环": "完整流程", "赋能": "提供", "收口": "结束",
	"盖穿": "写坏", "操心": "另外照看",
	"就红": "就失败", "门面交出的东西": "门面导出的类型",
	// "交出"只管所有权转移，`export` 那件事写"导出"。这几个旧说法不许回到注释里。
	"包交出来": "包导出", "对外交出": "导出", "挂 export": "写 export", "挂过包": "导入过",
	"交出来的数": "导出的数", "交出来的类型别名": "导出的类型别名",
}

var quotedSpan = regexp.MustCompile(`"[^"]*"|'[^']*'|` + "`[^`]*`")

func commentTermsExemptPaths(rel string) bool {
	// 编译器输入样本与测试数据：那里的文字是别的检查的输入，不参与用词统一。
	return strings.Contains(rel, filepath.Join("testdata", "proj")) ||
		strings.Contains(rel, "selftest"+string(os.PathSeparator)+"data")
}

func TestCommentsUseTheAgreedTerms(t *testing.T) {
	root := repoRoot(t)
	exts := map[string]bool{".go": true, ".nf": true, ".ts": true, ".mts": true, ".mjs": true}
	skipDirs := map[string]bool{"node_modules": true, ".git": true, "out": true, "dist": true,
		"cache": true, "profiles": true, "release": true}
	var hits []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(p)
		if ext == ".go" && strings.Contains(p, "testdata") {
			return nil // 编译器输入样本，注释随样本一起喂给编译器
		}
		if !exts[ext] {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if commentTermsExemptPaths(rel) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			comment := quotedSpan.ReplaceAllString(commentPart(line), "")
			if comment == "" {
				continue
			}
			for word, want := range bannedInComments {
				if strings.Contains(comment, word) {
					text := []rune(strings.TrimSpace(comment))
					if len(text) > 80 {
						text = append(text[:80], []rune("…")...)
					}
					hits = append(hits, filepath.ToSlash(rel)+":"+strconv.Itoa(i+1)+" 「"+word+"」应作「"+want+"」  "+string(text))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("走仓库失败：%v", err)
	}
	if len(hits) > 0 {
		t.Errorf("注释里有 %d 处旧叫法或黑话：\n%s", len(hits), strings.Join(hits, "\n"))
	}
}

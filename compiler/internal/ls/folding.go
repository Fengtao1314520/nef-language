// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// folding.go 是编辑器里左侧那一列折叠箭头：哪些行能折起来、折起来留下哪一行。
//
// 代码块的折叠直接用大纲算好的范围（同一棵树，两处不各算一遍）；注释的折叠看行的形状——
// 连着两行行注释、或者一段 `/*` 起 `*/` 收的块注释。这一条只认行首那一点形状，不去重新走
// 一遍词法层：折错了最多是一处箭头不该在，不会把程序的含义读歪。
//
// en: folding.go feeds the gutter's fold arrows: which lines collapse and which line stays visible.
// Code blocks reuse the ranges the outline already computed - one tree, not two - while comments
// go by line shape: two or more consecutive line comments, or a /* ... */ run. This looks only at
// the start of a line instead of re-lexing: a wrong arrow is cosmetic and cannot change what the
// program means.
package ls

import (
	"strings"
)

// FoldingRange 是一段能折起来的行：从哪一行到哪一行（都算进去，从 0 数），种类给编辑器看
// 是要按注释还是按导入来显示。
//
// en: FoldingRange is one collapsible run of lines (both ends included, 0-based); Kind tells the
// editor whether to treat it as a comment or an import block.
type FoldingRange struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Kind      string `json:"kind,omitempty"`
}

// 协议里 FoldingRangeKind 的三个值，这一份用得到后两个。
//
// en: The three FoldingRangeKind values of the protocol; this file uses the last two.
const (
	foldComment = "comment"
	foldImports = "imports"
)

// FoldingRanges 返回一份文件里所有能折起来的地方。
//
// en: FoldingRanges returns every collapsible run in one file.
func FoldingRanges(path, text string) []FoldingRange {
	src := newChecked(path, text)
	var out []FoldingRange
	out = append(out, blockFolds(src.outline())...)
	out = append(out, src.commentFolds()...)
	out = append(out, src.importFold()...)
	return out
}

// blockFolds 把大纲里的每一条变成一段折叠：占两行以上的都算，孩子跟着排进去，
// 编辑器按嵌套画箭头。
//
// en: blockFolds turns every outline entry into a fold: anything spanning two or more lines,
// children included, so the editor draws the arrows nested.
func blockFolds(syms []DocumentSymbol) []FoldingRange {
	var out []FoldingRange
	for _, s := range syms {
		if s.Range.End.Line > s.Range.Start.Line {
			out = append(out, FoldingRange{StartLine: s.Range.Start.Line, EndLine: s.Range.End.Line})
		}
		out = append(out, blockFolds(s.Children)...)
	}
	return out
}

// commentFolds 找两种注释形状：连着两行以上的行注释，以及跨行的块注释。
//
// en: commentFolds finds the two comment shapes: two or more consecutive line comments, and a
// block comment spanning lines.
func (s source) commentFolds() []FoldingRange {
	var out []FoldingRange
	run := -1
	for i, line := range s.lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") {
			if run < 0 {
				run = i
			}
			continue
		}
		if run >= 0 {
			if i-1 > run {
				out = append(out, FoldingRange{StartLine: run, EndLine: i - 1, Kind: foldComment})
			}
			run = -1
		}
		if strings.HasPrefix(t, "/*") && !strings.Contains(t, "*/") {
			for j := i + 1; j < len(s.lines); j++ {
				if strings.Contains(s.lines[j], "*/") {
					out = append(out, FoldingRange{StartLine: i, EndLine: j, Kind: foldComment})
					break
				}
			}
		}
	}
	if run >= 0 && len(s.lines)-1 > run {
		out = append(out, FoldingRange{StartLine: run, EndLine: len(s.lines) - 1, Kind: foldComment})
	}
	return out
}

// importFold 把开头那一串 import 合成一段折叠：文件读到一半想找引了谁，折掉正文只看这一串。
//
// en: importFold collapses the run of imports at the top: when a reader is halfway through a file
// and wants to know what it pulls in, the body folds away and that list stays.
func (s source) importFold() []FoldingRange {
	for _, f := range s.files() {
		if f.Path != s.path || len(f.Imports) < 2 {
			continue
		}
		first, last := f.Imports[0].Pos.Line-1, f.Imports[len(f.Imports)-1].Pos.Line-1
		if f.Imports[len(f.Imports)-1].PathPos.Line-1 > last {
			last = f.Imports[len(f.Imports)-1].PathPos.Line - 1
		}
		if last > first {
			return []FoldingRange{{StartLine: first, EndLine: last, Kind: foldImports}}
		}
	}
	return nil
}

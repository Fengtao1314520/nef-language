// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// symbols.go 是编辑器的大纲那一条：它问"这份文件里有什么"，答复是一棵树——顶层的
// 类型、函数与常量各自带着身上的字段与方法。
//
// 树的形状取自解析结果（`ast`），名字的行列取自词法层的记号，一块的结束取自那一对花括号
// 配上的位置：这一份不另定一套"什么算声明"的规则，编辑器看到的大纲与编译器认的是同一份结构。
// 括号还没闭合的那一份文本先补上右括号再算（见 repair.go）；补不出来的（字符串没有收尾）就答空表。
//
// en: symbols.go answers documentSymbol: the editor asks what a file holds and gets a tree - top
// level types, functions and constants, each carrying its own fields and methods. The shape comes
// from the parsed AST, a name's position from the lexer's tokens, and the end of a block from where
// its braces match up; nothing here re-decides what counts as a declaration, so the outline and the
// compiler read the same structure. Text with a bracket still open gets its closers appended before
// the count (see repair.go); when that cannot recover it, the answer is an empty list.
package ls

import (
	"sort"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
)

// 协议里 SymbolKind 的那些数，写在这儿是为了这份文件里每一处都能对着读。
//
// en: The SymbolKind numbers of the protocol, kept here so every use in this file can be read
// against one table.
var symKind = struct {
	class, method, field, enum, intf, function, variable, constant, enumMember, structKind int
}{5, 6, 8, 10, 11, 12, 13, 14, 22, 23}

// DocumentSymbol 是大纲里的一条：名字、种类、整条声明占的行、名字本身占的行，加上孩子。
//
// en: DocumentSymbol is one outline entry: the name, its kind, the lines the whole declaration
// spans, the lines of the name itself, and the children.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// source 是一份文件在算大纲、折叠、语义着色与参数提示时要反复用到的三样东西：整份文本、
// 它的每一行、它切出来的记号。检查算得出来时再带上那两份结果（程序与账），算不出来就是空的。
//
// en: source carries the three things the outline, folding, semantic colours and parameter hints
// keep needing: the whole text, its lines and its tokens. When the check does run it also carries
// those two results - the program and its tables; otherwise they stay empty.
type source struct {
	text  string
	lines []string
	toks  []lexer.Tok
	path  string
	prog  *check.Program
	facts *check.Facts
}

func newSource(text string) source {
	toks, _ := lexer.Lex(text)
	return source{text: text, lines: splitLines(text), toks: toks}
}

// newChecked 走一遍前端，把这一份文件的解析结果与账一并带上。原文过不去就先补上没配对的
// 右括号再走一次（那一篇的说明在 repair.go）：屏幕上的文本与行列仍以原文为准。
//
// en: newChecked runs the front end and attaches this file's parsed program and tables. When the
// text as written cannot be parsed it first appends the unmatched closing brackets and tries again
// (see repair.go); the text on screen and every line and column still come from the original.
func newChecked(path, text string) source {
	s := newSource(text)
	s.path = path
	s.prog, s.facts = inspect(path, text)
	if s.prog == nil {
		if fixed := closeOpenBrackets(text); fixed != text {
			s.prog, s.facts = inspect(path, fixed)
		}
	}
	return s
}

// Outline 返回一份文件的大纲。大纲那一条协议与折叠那一条都走它，两处看到的形状因此同一份。
//
// en: Outline returns the tree for one file. Both documentSymbol and foldingRange use it, so the
// two views are built from one shape.
func Outline(path, text string) []DocumentSymbol {
	return newChecked(path, text).outline()
}

// outline 用这一份文件已经带上的解析结果排大纲；语法过不去就答空表（编辑器按缩进给的
// 默认折叠与语法文件的着色照旧在），有一处写坏就整片没有，不如不报。
//
// en: outline builds the tree from the program already attached to this source. When the syntax
// cannot be recovered the answer is no list at all: the editor's indentation folds and the grammar's
// colours still work, and reporting a half-wrong tree is worse than reporting none.
func (s source) outline() []DocumentSymbol {
	var file *ast.File
	for _, f := range s.files() {
		if f.Path == s.path {
			file = f
			break
		}
	}
	if file == nil {
		return nil
	}

	// 顶层的 let 在语句流里，类型、函数与 const 在定义那一列里：两处合起来按书写的行排序。
	// 同一份声明可能两处都登记（写死的常量既在定义那一列，也在语句流里占一处），按位置去掉重复。
	//
	// en: A top-level let lives in the statement list while types, functions and const live in the
	// definition list: both are merged and sorted by the line they were written on. One declaration
	// can be registered in both places, so duplicates are dropped by position.
	var tops []ast.Def
	seen := map[ast.Pos]bool{}
	for _, d := range file.Defs {
		if !seen[d.Where()] {
			seen[d.Where()] = true
			tops = append(tops, d)
		}
	}
	for _, st := range file.Stmts {
		d, ok := st.(*ast.Decl)
		if !ok || seen[d.Where()] {
			continue
		}
		seen[d.Where()] = true
		tops = append(tops, d)
	}
	sort.SliceStable(tops, func(i, j int) bool { return tops[i].Where().Line < tops[j].Where().Line })
	starts := make([]int, 0, len(tops))
	for _, d := range tops {
		starts = append(starts, d.Where().Line)
	}
	sort.Ints(starts)

	out := make([]DocumentSymbol, 0, len(tops))
	for _, d := range tops {
		out = append(out, s.symbolOfDef(d, nextStartOf(starts, d.Where().Line, len(s.lines)), s.lastFoldLine()))
	}
	return out
}

// symbolOfDef 把一条顶层定义写成大纲里的一条，身上的字段与方法排进第二层。
//
// en: symbolOfDef writes one top-level definition as one outline entry, with its fields and
// methods in the second level.
func (s source) symbolOfDef(d ast.Def, endLine, lastLine int) DocumentSymbol {
	pos := d.Where()
	idx, ok := s.nameTokenOf(pos.Line, pos.Col, d.Name())
	sym := DocumentSymbol{
		Name:  d.Name(),
		Range: s.lineRange(pos.Line-1, endLine-1),
	}
	if ok {
		sym.SelectionRange = s.tokenRange(s.toks[idx])
	} else {
		sym.SelectionRange = sym.Range
	}
	switch x := d.(type) {
	case *ast.Object:
		sym.Kind = symKind.class
		if x.Kind == "struct" {
			sym.Kind = symKind.structKind
		}
		sym.Children = s.membersOf(x.Fields, x.Methods, s.braceEnd(idx, endLine), endLine)
	case *ast.Interface:
		sym.Kind = symKind.intf
		sym.Children = s.membersOf(x.Fields, x.Methods, s.braceEnd(idx, endLine), endLine)
	case *ast.Enum:
		sym.Kind = symKind.enum
		sym.Children = s.enumMembers(x.Members)
	case *ast.TypeAlias:
		sym.Kind = symKind.class
	case *ast.Func:
		sym.Kind = symKind.function
		if own := s.braceEnd(idx, endLine); own > 0 {
			sym.Range = s.lineRange(pos.Line-1, own-1)
		}
	case *ast.Decl:
		sym.Kind = symKind.variable
		if x.IsConst || x.Shared {
			sym.Kind = symKind.constant
		}
	}
	return sym
}

// membersOf 排大纲的第二层：每一项从自己那一行开始，到下一项开始的前一行为止，
// 最后一项走到这一身花括号合上的那一行。
//
// en: membersOf builds the second level: each item runs from its own line to the line before the
// next item starts, the last one reaching the line where the braces close.
func (s source) membersOf(fields []*ast.Field, methods []*ast.Func, closeLine, fallback int) []DocumentSymbol {
	// own 说这一条的范围是自己算出来的（字段占一行、带体的方法走到自己的花括号），
	// 不再按"到下一项开始之前"改写；接口里那些只有签名的方法要走后者。
	type item struct {
		line int
		sym  DocumentSymbol
		own  bool
	}
	var items []item
	for _, f := range fields {
		sym := DocumentSymbol{Name: f.NameText, Kind: symKind.field}
		if idx, ok := s.nameTokenOf(f.Pos.Line, f.Pos.Col, f.NameText); ok {
			sym.Range = s.tokenRange(s.toks[idx])
			sym.SelectionRange = sym.Range
		} else {
			sym.Range = s.lineRange(f.Pos.Line-1, f.Pos.Line-1)
			sym.SelectionRange = sym.Range
		}
		items = append(items, item{f.Pos.Line, sym, true})
	}
	for _, m := range methods {
		sym := DocumentSymbol{Name: m.NameText, Kind: symKind.method}
		if idx, ok := s.nameTokenOf(m.Pos.Line, m.Pos.Col, m.NameText); ok {
			sym.SelectionRange = s.tokenRange(s.toks[idx])
			sym.Range = s.lineRange(m.Pos.Line-1, m.Pos.Line-1)
			// 带体的方法走到自己那一对花括号；接口里只有签名，没有体，留给下一条规则。
			if own := s.braceEnd(idx, 0); own > 0 {
				sym.Range = s.lineRange(m.Pos.Line-1, own-1)
				items = append(items, item{m.Pos.Line, sym, true})
				continue
			}
		} else {
			sym.Range = s.lineRange(m.Pos.Line-1, m.Pos.Line-1)
			sym.SelectionRange = sym.Range
		}
		items = append(items, item{m.Pos.Line, sym, false})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].line < items[j].line })
	lines := make([]int, len(items))
	for i, it := range items {
		lines[i] = it.line
	}
	out := make([]DocumentSymbol, 0, len(items))
	for _, it := range items {
		if it.own {
			out = append(out, it.sym)
			continue
		}
		end := nextStartOf(lines, it.line, closeLine)
		if end <= 0 {
			end = fallback
		}
		if closeLine > 0 && closeLine < end {
			end = closeLine
		}
		it.sym.Range = s.lineRange(it.line-1, end-1)
		out = append(out, it.sym)
	}
	return out
}

// enumMembers 是枚举身上的成员，每个占自己那一行。
//
// en: enumMembers are the members inside an enum, each on its own line.
func (s source) enumMembers(ms []ast.EnumMember) []DocumentSymbol {
	out := make([]DocumentSymbol, 0, len(ms))
	for _, m := range ms {
		idx, ok := s.nameTokenOf(m.Pos.Line, m.Pos.Col, m.NameText)
		if !ok {
			continue
		}
		r := s.tokenRange(s.toks[idx])
		out = append(out, DocumentSymbol{Name: m.NameText, Kind: symKind.enumMember, Range: r, SelectionRange: r})
	}
	return out
}

// nameTokenOf 从声明开头那一个记号往后找，取第一个名字对得上的记号：`class`、`method`、`let`
// 这些关键字占的是声明自己的位置，名字在它们后面。
//
// en: nameTokenOf walks forward from the token a declaration starts on and takes the first token
// whose text is that name - the keywords class, method and let hold the declaration's own
// position, the name follows them.
func (s source) nameTokenOf(line, col int, name string) (int, bool) {
	for i, t := range s.toks {
		if t.Line < line || (t.Line == line && t.Col < col) {
			continue
		}
		if t.Kind == lexer.EOF {
			break
		}
		if t.Kind == lexer.Name && t.Text == name {
			return i, true
		}
	}
	return 0, false
}

// braceEnd 从这一个记号往后找第一对配上的花括号，返回右括号那一行；没有体（接口里的方法
// 声明）或者括号没配上就返回 0。limit 是这一条允许走到的最后一行，0 表示不设。
//
// en: braceEnd looks forward from a token for the first brace pair that balances and returns the
// closing brace's line; no body (an interface method declaration) or an unmatched brace gives 0.
// limit is the last line this may reach, 0 meaning no limit.
func (s source) braceEnd(from, limit int) int {
	if from <= 0 || from >= len(s.toks) {
		return 0
	}
	depth := 0
	for i := from; i < len(s.toks); i++ {
		t := s.toks[i]
		if t.Kind == lexer.EOF {
			break
		}
		if t.Kind != lexer.Op {
			continue
		}
		switch t.Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				if limit > 0 && t.Line > limit {
					return 0
				}
				if t.Line > len(s.lines) {
					// 补出来的那一颗：括号没配上时按文件的最后一行算，别把不存在的行报给编辑器。
					return len(s.lines)
				}
				return t.Line
			}
		}
	}
	return 0
}

// lineRange 给从 0 数、首尾都算进去的两行，拼一个协议里的范围：结束落在后一行的行尾。
//
// en: lineRange turns two 0-based inclusive lines into a protocol range ending at the end of the
// later line.
func (s source) lineRange(startLine, endLine int) Range {
	if startLine < 0 {
		startLine = 0
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine >= len(s.lines) {
		endLine = len(s.lines) - 1
	}
	return Range{
		Start: Position{Line: startLine, Character: 0},
		End:   Position{Line: endLine, Character: utf16Len(s.lines[endLine])},
	}
}

// tokenRange 是一个记号自己占的范围。词法层的列按字符从 1 数，协议里的列按 UTF-16 从 0 数。
//
// en: tokenRange is the range one token covers. The lexer counts columns in characters from 1,
// the protocol in UTF-16 units from 0.
func (s source) tokenRange(t lexer.Tok) Range {
	line := ""
	if t.Line-1 >= 0 && t.Line-1 < len(s.lines) {
		line = s.lines[t.Line-1]
	}
	start := utf16Len(string([]rune(line)[:minInt(t.Col-1, len([]rune(line)))]))
	return Range{
		Start: Position{Line: t.Line - 1, Character: start},
		End:   Position{Line: t.Line - 1, Character: start + utf16Len(t.Text)},
	}
}

// files 是这一次编译涉及的那几份文件；前端过不去就是空表。
//
// en: files are the files this run covers; an empty list when the front end did not get through.
func (s source) files() []*ast.File {
	if s.prog == nil {
		return nil
	}
	return s.prog.Files
}

// lastFoldLine 是这一份文件的最后一行（从 1 数），给"最后一项走到文件末尾"那条规则用。
//
// en: lastFoldLine is the file's last line (1-based), used by the rule that the final item runs to
// the end of the file.
func (s source) lastFoldLine() int { return len(s.lines) }

func nextStartOf(starts []int, line, fallback int) int {
	for _, st := range starts {
		if st > line {
			return st - 1
		}
	}
	return fallback
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// tokens.go 是语义着色那一条：语法文件（TextMate）按字面的样子着色，认不出"这一个 `half`
// 是声明还是调用"、"这一个 `n` 是参数还是局部量"。这一条把检查器已经算出来的答案交给编辑器，
// 于是同一个名字在屏幕上有它该有的那一档颜色。
//
// 答案全部来自现成的三处：顶层定义取自解析结果，名字绑到哪儿取自 `Facts.Binds`，
// 关键字与内建名字取自词法层那两张表（这一路跳过它们，那一档由语法文件负责）。
// 括号还没闭合的文本先补右括号再算（见 repair.go）；补不出来的（字符串没有收尾）就整片不报，
// 屏幕上仍然有语法文件给的那一套颜色。
//
// en: tokens.go is semantic highlighting. A TextMate grammar colours by appearance and cannot tell
// whether this `half` is a declaration or a call, or whether this `n` is a parameter or a local.
// This hands the editor what the checker already worked out, so each name gets the colour it
// should have. Everything comes from tables that already exist: top-level definitions from the
// parsed AST, what a name refers to from Facts.Binds, and keywords plus built-in names from the
// lexer's two tables - those are skipped here and left to the grammar. Text with a bracket still
// open gets its closers appended first (see repair.go); when that cannot recover it, nothing is
// sent and the grammar's colours still paint the screen.
package ls

import (
	"sort"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
)

// 协议里的记号种类，顺序就是发给编辑器那张表里的顺序：改这里的顺序要同步改那张表。
//
// en: The token kinds of the protocol; their order is the order of the legend sent to the editor,
// so changing it here means changing the legend too.
var tokType = struct {
	typ, class, structKind, intf, enum, enumMember, parameter, variable, constant, property, function, method int
}{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}

// 记号的附加属性：declaration 是"这一个是名字被写下来的地方"，readonly 是写定之后不能再改的那两个。
//
// en: The token modifiers: declaration marks where a name was written, readonly marks the two
// forms that cannot be assigned again.
const (
	modDeclaration = 1 << iota
	modReadonly
)

// mark 是一个记号要发出去的那一档颜色。
//
// en: mark is the colour one token is sent with.
type mark struct {
	kind      int
	modifiers int
}

// SemanticLegend 是这一份答复的图例：编辑器拿它把下面的序号换成主题里的颜色。
//
// en: SemanticLegend is the key for the numbers below; the editor turns them into theme colours.
type SemanticLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// semanticLegend 交出这张表的内容：种类与附加属性各自的顺序，与发出去的序号是同一份约定。
//
// en: semanticLegend hands over the table itself: the order of kinds and modifiers, the same
// convention the emitted numbers are written against.
func semanticLegend() SemanticLegend {
	return SemanticLegend{
		TokenTypes: []string{
			"type", "class", "struct", "interface", "enum", "enumMember",
			"parameter", "variable", "constant", "property", "function", "method",
		},
		TokenModifiers: []string{"declaration", "readonly"},
	}
}

// SemanticTokens 是协议要求的整份记号表：五个数一组（行差、列差、长度、种类、附加属性），
// 按出现顺序排，每一项相对前一项记差值。文本里的记号位置一律按 UTF-16 列从 0 数。
//
// en: SemanticTokens is the protocol's whole-document list: five integers per token (delta line,
// delta start, length, type, modifiers), in order, each relative to the previous one, with
// UTF-16 columns counted from 0.
func SemanticTokens(path, text string) []int {
	src := newChecked(path, text)
	if src.prog == nil {
		return nil
	}
	found := map[int]mark{}

	// 一、顶层定义与身上的成员：这些位置是名字被写下来的那一个地方。
	for _, f := range src.files() {
		if f.Path != path {
			continue
		}
		for _, d := range f.Defs {
			src.markDef(found, d)
		}
		for _, st := range f.Stmts {
			if d, ok := st.(*ast.Decl); ok {
				src.markDecl(found, d)
			}
		}
	}

	// 二、名字写到别处时绑的是谁：参数、局部量、字段、顶层那两份都在这一张表里。
	// 位置对不上这一份文件的记号就丢掉——那是这一次编译里另一份文件里的同一行号。
	if src.facts != nil {
		for n, b := range src.facts.Binds {
			p := n.Where()
			idx, ok := src.tokenIndexOf(p.Line, p.Col)
			if !ok || src.toks[idx].Text != n.Text {
				continue
			}
			if _, taken := found[idx]; taken {
				continue
			}
			found[idx] = markOfBind(b)
		}
	}

	// 三、剩下的问检查器：点号后面那一个名字，认成员那一步已经说出它是字段还是方法，
	// 照它给档；检查器没记上的（这一趟没走到那一步）才退回按字面形状补——后面接左括号的是方法，
	// 不接的是字段。再剩下的名字是这一份或别处定义过的类型名。
	//
	// en: The rest goes to the checker: for the name after a dot it already worked out whether it
	// is a field or a method, so that answer paints the token; only what the table misses (the
	// stretch checking never reached) falls back on appearance - a bracket after it means method,
	// none means field. Remaining names are type types declared here or elsewhere.
	notes := src.memberOwnerNotes()
	for i, t := range src.toks {
		if t.Kind == lexer.EOF {
			break
		}
		if t.Kind != lexer.Name || lexer.Keywords[t.Text] || lexer.Reserved[t.Text] {
			continue
		}
		if _, taken := found[i]; taken {
			continue
		}
		afterDot := i > 0 && src.toks[i-1].Kind == lexer.Op && src.toks[i-1].Text == "."
		callOpen := i+1 < len(src.toks) && src.toks[i+1].Kind == lexer.Op && src.toks[i+1].Text == "("
		switch {
		case afterDot && callOpen:
			if note, ok := notes[i]; ok {
				found[i] = markOfMember(note)
			} else {
				found[i] = mark{kind: tokType.method}
			}
		case afterDot:
			if note, ok := notes[i]; ok {
				found[i] = markOfMember(note)
			} else {
				found[i] = mark{kind: tokType.property}
			}
		case callOpen:
			found[i] = mark{kind: tokType.function}
		default:
			if k, ok := declaredTypeOf(src.facts, t.Text); ok {
				found[i] = mark{kind: k}
			}
		}
	}

	return encodeTokens(src, found)
}

// memberOwnerNotes 把检查器认出来的每一次 `.名字` 按记号位置排成一张表。
//
// en: memberOwnerNotes lays the checker's resolved `.name` answers out by token position.
func (s source) memberOwnerNotes() map[int]check.MemberNote {
	out := map[int]check.MemberNote{}
	if s.facts == nil {
		return out
	}
	for m, note := range s.facts.Members {
		idx, ok := s.tokenIndexOf(m.Pos.Line, m.Pos.Col)
		if !ok || s.toks[idx].Text != m.NameText {
			continue
		}
		out[idx] = note
	}
	return out
}

// markOfMember 把检查器那一种成员换成协议里的一档。
//
// en: markOfMember turns one kind the checker named into the protocol's colour.
func markOfMember(note check.MemberNote) mark {
	switch note.Kind {
	case check.MemberMethod, check.MemberStd, check.MemberPkgFunc:
		return mark{kind: tokType.method}
	case check.MemberEnumValue:
		return mark{kind: tokType.enumMember}
	case check.MemberPkgConst:
		return mark{kind: tokType.constant}
	}
	return mark{kind: tokType.property}
}

// markOfBind 把检查器那条绑定换成协议里的一档。
//
// en: markOfBind turns one binding the checker recorded into a protocol kind.
func markOfBind(b check.Bind) mark {
	switch b.By {
	case check.BindParam:
		return mark{kind: tokType.parameter}
	case check.BindField, check.BindThis:
		return mark{kind: tokType.property}
	case check.BindGlobalConst, check.BindShared:
		return mark{kind: tokType.constant}
	}
	return mark{kind: tokType.variable}
}

// declaredTypeOf 把一个写在源码里的类型名换成协议里的那一档。
//
// en: declaredTypeOf turns a type name written in the source into the matching protocol kind.
func declaredTypeOf(facts *check.Facts, name string) (int, bool) {
	if facts == nil {
		return 0, false
	}
	d, ok := facts.Declared[name]
	if !ok {
		return 0, false
	}
	switch d.Kind {
	case "class":
		return tokType.class, true
	case "struct":
		return tokType.structKind, true
	case "interface":
		return tokType.intf, true
	case "enum":
		return tokType.enum, true
	}
	return tokType.typ, true
}

// markDef 记一条顶层定义的名字，顺带把它身上的字段、方法与参数都记上。
//
// en: markDef records a top-level definition's name and, along the way, its fields, methods and
// parameters.
func (s source) markDef(found map[int]mark, d ast.Def) {
	switch x := d.(type) {
	case *ast.Object:
		s.markName(found, x.Pos.Line, x.Pos.Col, x.NameText, kindOfObject(x.Kind))
		for _, f := range x.Fields {
			s.markName(found, f.Pos.Line, f.Pos.Col, f.NameText, tokType.property)
		}
		for _, m := range x.Methods {
			s.markFunc(found, m)
		}
	case *ast.Interface:
		s.markName(found, x.Pos.Line, x.Pos.Col, x.NameText, tokType.intf)
		for _, f := range x.Fields {
			s.markName(found, f.Pos.Line, f.Pos.Col, f.NameText, tokType.property)
		}
		for _, m := range x.Methods {
			s.markFunc(found, m)
		}
	case *ast.Enum:
		s.markName(found, x.Pos.Line, x.Pos.Col, x.NameText, tokType.enum)
		for _, m := range x.Members {
			s.markName(found, m.Pos.Line, m.Pos.Col, m.NameText, tokType.enumMember)
		}
	case *ast.TypeAlias:
		s.markName(found, x.Pos.Line, x.Pos.Col, x.NameText, tokType.typ)
	case *ast.Func:
		s.markFunc(found, x)
	case *ast.Decl:
		s.markDecl(found, x)
	}
}

func (s source) markFunc(found map[int]mark, f *ast.Func) {
	kind := tokType.function
	if f.IsMethod {
		kind = tokType.method
	}
	s.markName(found, f.Pos.Line, f.Pos.Col, f.NameText, kind)
	for _, p := range f.Params {
		s.markName(found, p.Pos.Line, p.Pos.Col, p.NameText, tokType.parameter)
	}
}

func (s source) markDecl(found map[int]mark, d *ast.Decl) {
	kind, mods := tokType.variable, 0
	if d.IsConst || d.Shared {
		kind, mods = tokType.constant, modReadonly
	}
	s.markName(found, d.Pos.Line, d.Pos.Col, d.NameText, kind, mods)
}

func kindOfObject(kind string) int {
	if kind == "struct" {
		return tokType.structKind
	}
	return tokType.class
}

// markName 从声明开头往后找到那一个名字的记号：关键字在前，名字在后。
//
// en: markName walks forward from where a declaration starts to the token that is the name: the
// keyword comes first, the name after it.
func (s source) markName(found map[int]mark, line, col int, name string, kind int, mods ...int) {
	idx, ok := s.nameTokenOf(line, col, name)
	if !ok {
		return
	}
	all := modDeclaration
	for _, extra := range mods {
		all |= extra
	}
	found[idx] = mark{kind: kind, modifiers: all}
}

// tokenIndexOf 按行列找到那一个记号；找不到就是别的文件里同样位置的那一个，不该着色。
//
// en: tokenIndexOf finds a token by line and column; no match means that position belongs to
// another file in this run and must not be coloured here.
func (s source) tokenIndexOf(line, col int) (int, bool) {
	for i := range s.toks {
		t := s.toks[i]
		if t.Kind == lexer.EOF {
			break
		}
		if t.Line == line && t.Col == col {
			return i, true
		}
	}
	return 0, false
}

// encodeTokens 把这一张表按协议排好序、记成差值串。
//
// en: encodeTokens sorts that table and writes it as the protocol's delta list.
func encodeTokens(s source, found map[int]mark) []int {
	list := make([]int, 0, len(found))
	for idx := range found {
		list = append(list, idx)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := s.toks[list[i]], s.toks[list[j]]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return s.u16Col(a.Line, a.Col) < s.u16Col(b.Line, b.Col)
	})
	out := make([]int, 0, len(list)*5)
	prevLine, prevChar := 0, 0
	for _, idx := range list {
		t := s.toks[idx]
		line, char := t.Line-1, s.u16Col(t.Line, t.Col)
		dChar := char - prevChar
		if line != prevLine {
			dChar = char // 换行之后给的是绝对列，协议按这一条走
		}
		out = append(out, line-prevLine, dChar, utf16Len(t.Text), found[idx].kind, found[idx].modifiers)
		prevLine, prevChar = line, char
	}
	return out
}

// u16Col 把词法层按字符计的列（从 1 数）换成协议按 UTF-16 计的列（从 0 数）。名字都是 ASCII，
// 这一步只在同一行里前面出现过非 ASCII 字符（字符串、注释）时才真的起作用。
//
// en: u16Col converts the lexer's 1-based character column into the protocol's 0-based UTF-16
// column. Names are ASCII, so this only matters when the same line holds a non-ASCII string or
// comment before the name.
func (s source) u16Col(line, col int) int {
	runes := []rune(s.lineAt(line))
	n := minInt(col-1, len(runes))
	return utf16Len(string(runes[:n]))
}

// u16LenOfToken 是一个记号占的 UTF-16 长度。
//
// en: u16LenOfToken is how many UTF-16 units one token spans.
func (s source) u16LenOfToken(t lexer.Tok) int {
	if t.Kind != lexer.Name {
		return utf16Len(t.Text)
	}
	return utf16Len(t.Text)
}

func (s source) lineAt(line int) string {
	if line-1 < 0 || line-1 >= len(s.lines) {
		return ""
	}
	return s.lines[line-1]
}

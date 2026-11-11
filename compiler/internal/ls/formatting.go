// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// formatting.go 是"格式化文档"那一条：缩进按花括号深度重排，行尾空白去掉，连续空行并成
// 一行，同一行里记号之间的空白按几条不会认错的规则收齐。字符串、字符与注释的原文一个字节
// 不动：记号只用来划出代码与它们各自的边界，重排的是记号之间的空白，不是记号本身。
//
// 不做的那几样也写在这儿：尖括号（`<`、`>`、`<<`、`>>`）前后的空白不动——类型实参与比较
// 运算共用这几个记号，只凭词法分不清；冒号后面原来有没有空格照旧（声明里 `x:int` 与
// struct 字面量里 `x: 1` 都是对的）；块注释内部不重排。
//
// en: formatting.go answers format-document: indentation follows brace depth, trailing
// whitespace goes, runs of blank lines collapse to one, and whitespace between tokens on a line
// is tidied by a few rules that cannot misread. Strings, characters and comments keep every byte
// of their original text: tokens only mark where code and each of them begin and end; what is
// rearranged is the whitespace between tokens, not the tokens themselves.
//
// What it does not do is also written here: whitespace around angle brackets (`<`, `>`, `<<`,
// `>>`) stays - type arguments and comparisons share those tokens and lexing alone cannot tell
// them apart; whether a colon was followed by a space stays (`x:int` in a declaration and `x: 1`
// in a struct literal are both right); the inside of a block comment is not re-laid.
package ls

import (
	"strings"

	"neflang.org/nf/internal/lexer"
)

// FormattingOptions 是编辑器随格式化请求给的两个选项：一级缩进几个空格、用空格还是制表符。
//
// en: FormattingOptions are the two options the editor sends with a formatting request: how
// many spaces one level indents, and spaces or tabs.
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// Formatting 返回把整份文本排好的那一条改动。词法过不去（字符串没写完这些）返回 nil：
// 半截文本的边界划不准，不动比动错好。排完与原文相同也返回 nil。
//
// en: Formatting returns the one edit that lays out the whole text. When lexing fails (an
// unfinished string and the like) it returns nil: the boundaries of a half-written text cannot
// be marked reliably, and leaving it alone beats moving it wrong. A result identical to the
// original also returns nil.
func Formatting(path, text string, opts FormattingOptions) []TextEdit {
	toks, err := lexer.Lex(text)
	if err != nil {
		return nil
	}
	var code []lexer.Tok
	for _, t := range toks {
		if t.Kind != lexer.NL && t.Kind != lexer.EOF {
			code = append(code, t)
		}
	}
	if len(code) == 0 {
		return nil
	}
	unit := opts.TabSize
	if unit <= 0 {
		unit = 4
	}
	indent := strings.Repeat(" ", unit)
	if !opts.InsertSpaces {
		indent = "\t"
	}
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}

	starts := lineStarts(text)
	span := func(t lexer.Tok) (int, int) {
		start := starts[t.Line-1] + t.Col - 1
		return start, start + rawLen(text, start, t)
	}

	f := &formatter{indent: indent, nl: nl}
	prev := lexer.Tok{}
	prevEnd := 0
	prevUnary := false
	depth := 0
	for i, t := range code {
		start, end := span(t)
		gap := text[prevEnd:start]
		if i == 0 {
			f.leading(gap)
		} else {
			f.between(gap, prev, t, depth, prevUnary)
		}
		f.out.WriteString(text[start:end])
		if t.Kind == lexer.Op {
			switch t.Text {
			case "{":
				depth++
			case "}":
				depth--
				if depth < 0 {
					depth = 0
				}
			}
			prevUnary = (t.Text == "-" || t.Text == "!" || t.Text == "~") && unaryContext(prev)
		} else {
			prevUnary = false
		}
		prev, prevEnd = t, end
	}
	f.trailing(text[prevEnd:])

	out := strings.TrimRight(f.out.String(), " \t\r\n") + nl
	if out == text {
		return nil
	}
	lines := splitLines(text)
	last := lines[len(lines)-1]
	return []TextEdit{{
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: len(lines) - 1, Character: utf16Len(last)},
		},
		NewText: out,
	}}
}

// lineStarts 是每一行（1 起）在文本里的字节起点。
//
// en: lineStarts is the byte offset at which each line (1-based) starts in the text.
func lineStarts(text string) []int {
	var out []int
	off := 0
	for {
		out = append(out, off)
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return out
		}
		off += i + 1
	}
}

// rawLen 是一个记号在原文里占的字节数。名字、运算符与数字按记号上的文字数；字符串与字符
// 记的是解码后的内容，要从开头的引号扫到结尾的引号，转义照词法那一套跳过。
//
// en: rawLen is how many bytes a token occupies in the original text. Names, operators and
// numbers count by the text on the token; strings and characters hold decoded content, so the
// raw span is scanned from the opening quote to the closing one, skipping escapes the way the
// lexer does.
func rawLen(text string, start int, t lexer.Tok) int {
	switch t.Kind {
	case lexer.Str, lexer.Char:
		quote := text[start]
		i := start + 1
		for i < len(text) {
			c := text[i]
			if c == '\\' {
				i += 2
				if i-1 < len(text) && text[i-1] == 'u' {
					i += 4
				}
				continue
			}
			i++
			if c == quote {
				return i - start
			}
		}
		return len(text) - start
	case lexer.Num:
		return len(t.Digits)
	default:
		return len(t.Text)
	}
}

// formatter 攒排好的文本：缩进单位、换行符、写到哪儿了、上一行是不是空行。
//
// en: formatter accumulates the laid-out text: the indent unit, the newline, how far it has
// written, and whether the previous line was blank.
type formatter struct {
	indent    string
	nl        string
	out       strings.Builder
	lastBlank bool
}

func (f *formatter) line(s string) {
	f.out.WriteString(s)
	f.out.WriteString(f.nl)
	f.lastBlank = s == ""
}

func (f *formatter) indentOf(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat(f.indent, depth)
}

// leading 排第一颗记号之前的那一段：文件开头的注释行与空行。
//
// en: leading lays out the stretch before the first token: comment lines and blank lines at
// the file's start.
func (f *formatter) leading(gap string) {
	segs := strings.Split(gap, "\n")
	for i, seg := range segs {
		if i == len(segs)-1 {
			f.out.WriteString(f.indentOf(0)) // 第一颗记号顶格起
			return
		}
		f.commentLine(seg, 0)
	}
}

// between 排两颗记号之间的那一段。同一行里没注释就按记号对收空白；跨行就把上一行写完、
// 中间各行按注释或空行排、下一行按深度缩进。
//
// en: between lays out the stretch between two tokens. On one line without a comment the
// whitespace follows the token pair; across lines the previous line is closed out, the middle
// lines are laid out as comments or blanks, and the next line is indented by depth.
func (f *formatter) between(gap string, prev, cur lexer.Tok, depth int, prevUnary bool) {
	if !strings.Contains(gap, "\n") {
		if strings.Contains(gap, "//") || strings.Contains(gap, "/*") {
			f.out.WriteString(gap) // 行尾注释与代码之间的空白照旧
			return
		}
		f.out.WriteString(spacing(gap, prev, cur, prevUnary))
		return
	}
	segs := strings.Split(gap, "\n")
	f.out.WriteString(trimRightSpace(segs[0]))
	f.out.WriteString(f.nl)
	f.lastBlank = false
	for _, seg := range segs[1 : len(segs)-1] {
		f.commentLine(seg, depth)
	}
	d := depth
	if cur.Kind == lexer.Op && cur.Text == "}" {
		d--
	}
	last := segs[len(segs)-1]
	if strings.Contains(last, "//") || strings.Contains(last, "/*") {
		f.out.WriteString(f.indentOf(d) + strings.TrimSpace(last) + " ")
		return
	}
	f.out.WriteString(f.indentOf(d))
}

// trailing 排最后一颗记号之后的那一段：行尾注释留下，后面的注释行与空行排好，
// 文件最后只留一个换行（那一步在 Formatting 末尾做）。
//
// en: trailing lays out the stretch after the last token: a trailing comment stays, following
// comment lines and blanks are laid out, and the file keeps a single final newline (that step
// happens at the end of Formatting).
func (f *formatter) trailing(gap string) {
	segs := strings.Split(gap, "\n")
	f.out.WriteString(trimRightSpace(segs[0]))
	if len(segs) > 1 {
		f.out.WriteString(f.nl)
	}
	for _, seg := range segs[1:] {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		f.commentLine(seg, 0)
	}
}

// commentLine 排空档里的一整行：空行并成一行，注释行按深度缩进，块注释内部照旧。
//
// en: commentLine lays out one full line inside a gap: blank lines collapse to one, comment
// lines indent by depth, the inside of a block comment stays as written.
func (f *formatter) commentLine(seg string, depth int) {
	body := strings.TrimSpace(seg)
	if body == "" {
		if !f.lastBlank {
			f.line("")
		}
		return
	}
	f.line(f.indentOf(depth) + trimRightSpace(seg))
}

func trimRightSpace(s string) string { return strings.TrimRight(s, " \t\r") }

// 控制关键字后面那一对圆括号前有一个空格（`if (`、`for (`），调用与声明后面没有。
//
// en: A control keyword's parenthesis pair takes a space before it (`if (`, `for (`); a call's
// or a declaration's does not.
var controlKw = map[string]bool{"if": true, "while": true, "for": true, "catch": true, "switch": true}

// 尖括号那四个记号前后的空白不动：类型实参与比较运算共用，词法分不清。
//
// en: Whitespace around the four angle-bracket tokens stays: type arguments and comparisons
// share them and lexing cannot tell them apart.
var angleOps = map[string]bool{"<": true, ">": true, "<<": true, ">>": true}

var binaryOps = map[string]bool{
	"=": true, "+": true, "-": true, "*": true, "/": true, "%": true,
	"==": true, "!=": true, "<=": true, ">=": true, "&&": true, "||": true,
	"&": true, "|": true, "^": true,
}

// spacing 收同一行里两颗记号之间的空白。gap 是原来的空白：拿不准的对（尖括号、冒号后）
// 照旧，其余按这几条规则。prevUnary 说前一颗是不是一元运算符：是的话后面紧跟操作数，
// 不留空白。
//
// en: spacing tidies the whitespace between two tokens on one line. gap is the original
// whitespace: pairs it cannot judge (angle brackets, after a colon) stay as written, the rest
// follow these rules. prevUnary says the previous token was a unary operator: its operand
// follows directly, with no whitespace.
func spacing(gap string, prev, cur lexer.Tok, prevUnary bool) string {
	p, c := prev.Text, cur.Text
	if prev.Kind == lexer.Op && angleOps[p] || cur.Kind == lexer.Op && angleOps[c] {
		return gap
	}
	if prevUnary {
		return ""
	}
	if prev.Kind == lexer.Op {
		switch p {
		case "(", "[":
			return ""
		case ",":
			return " "
		case ".":
			return ""
		case ":":
			if gap == "" {
				return ""
			}
			return " "
		case "{":
			if c == "}" {
				return ""
			}
			return " "
		case ";":
			return " "
		}
		if binaryOps[p] {
			return " "
		}
	}
	if cur.Kind == lexer.Op {
		switch c {
		case ")", "]", ",", ".", ":", ";", "++", "--":
			return ""
		case "[":
			return ""
		case "(":
			if prev.Kind == lexer.Name && controlKw[p] {
				return " "
			}
			return ""
		case "{", "}":
			return " "
		}
		if c == "-" || c == "!" || c == "~" {
			if unaryContext(prev) {
				if prev.Kind == lexer.Op && (p == "(" || p == "[") {
					return ""
				}
				return " "
			}
		}
		if binaryOps[c] {
			return " "
		}
	}
	return " "
}

// unaryContext 判前一颗记号后面跟的是不是一个一元运算符：前面是运算符（不是闭合的那几个）、
// 逗号、或者 return／case／in／until 这几个带表达式的关键字时，后面的 `-`、`!`、`~` 是一元的。
//
// en: unaryContext decides whether what follows the previous token is a unary operator: after
// an operator (not the closing ones), a comma, or the expression-carrying keywords
// return/case/in/until, a following `-`, `!`, `~` is unary.
func unaryContext(prev lexer.Tok) bool {
	if prev.Kind == lexer.Op {
		switch prev.Text {
		case ")", "]", "}", "++", "--":
			return false
		}
		return true
	}
	switch prev.Text {
	case "return", "case", "in", "until":
		return true
	}
	return false
}

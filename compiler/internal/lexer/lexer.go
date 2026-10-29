// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package lexer 把 .nf 源码切成记号。
//
// 记号种类：nl（行尾）、name（标识符与关键字）、num、str、char、op、eof。
// 语句到行尾结束，所以 nl 是一种真记号，交给语法层当分隔符用。
//
// en: Package lexer splits .nf source code into tokens.
package lexer

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Kind int

const (
	NL Kind = iota
	Name
	Num
	Str
	Char
	Op
	EOF
)

func (k Kind) String() string {
	switch k {
	case NL:
		return "nl"
	case Name:
		return "name"
	case Num:
		return "num"
	case Str:
		return "str"
	case Char:
		return "char"
	case Op:
		return "op"
	case EOF:
		return "eof"
	}
	return "?"
}

// Tok 是一个记号。Text 对 name 是标识符、对 op 是运算符本身、对 str/char 是解码后的内容。
//
// en: Tok is one token. For name it holds the identifier, for op the operator itself, for
// str/char the decoded contents.
type Tok struct {
	Kind Kind
	Text string
	Line int
	Col  int

	// IsFloat 只对 Num 有意义：带小数点的字面量；本语言不支持科学计数法。
	IsFloat bool
	// Digits 只对 Num 有意义：原始写法，数字里可能含 `_`，进制可能是 0x。
	Digits string
}

// Keywords 是关键字表：这些词不能作为名字使用。
//
// en: Keywords is the keyword table: none of these words may be used as a name.
var Keywords = map[string]bool{}

// Reserved 收录内建类型名、容器名、内建函数名、组名、顶层预定义名。
// 它们不是关键字，但同样不能作为名字使用。
//
// en: Reserved holds built-in type names, container names, built-in function names, group
// names and top-level predefined names. They are not keywords, but they
// too may not be used as names.
var Reserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields("let const method struct class interface enum type " +
		"import export shared ref base this new return " +
		"as is throw try catch " +
		"if else while for in until switch default break continue " +
		"true false null") {
		Keywords[w] = true
	}
	for _, w := range strings.Fields("boolean int float byte unicode string object " +
		"list dict hashset Calendar Json bytes Socket Listener Process Response " +
		"length exit " +
		"console text number math time file path environment regular_expression json net http process " +
		"args") {
		Reserved[w] = true
	}
}

// operators 按长度从长到短排列，保证最长匹配。
//
// en: operators is sorted longest first so that matching takes the longest operator first.
var operators = []string{
	"++", "--", "==", "!=", "<=", ">=", "&&", "||", "<<", ">>",
	"+", "-", "*", "/", "%", "!", "=", "<", ">", "&", "|", "^", "~",
	"(", ")", "{", "}", "[", "]", ":", ",", ";", ".",
}

// opBytes 与 operators 一一对应，预先转成字节。识别每个标点都要拿源码剩余部分
// 比较一轮，剩余部分是 []byte，比较前先拷贝会把整趟扫描的代价抬成
// 记号数 × 文件长度（3000 行的程序，仅词法扫描 2.9 秒；不拷贝是 7.7 毫秒）。
//
// en: opBytes matches operators one for one, converted to bytes up front. Recognising each
// punctuation mark compares it against the remaining source, which is a []byte; copying that
// remainder before comparing turns the whole pass into tokens × file length (a 3000-line
// program took 2.9 s in the lexer alone; without the copy, 7.7 ms).
var opBytes = func() [][]byte {
	v := make([][]byte, len(operators))
	for i, op := range operators {
		v[i] = []byte(op)
	}
	return v
}()

type Error struct {
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("第 %d 行第 %d 列：%s", e.Line, e.Col, e.Msg) }

type Lexer struct {
	src  []byte
	pos  int
	line int
	col  int
}

func Lex(src string) ([]Tok, error) {
	l := &Lexer{src: []byte(src), line: 1, col: 1}
	var out []Tok
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		if t.Kind == EOF {
			return out, nil
		}
	}
}

func (l *Lexer) at(off int) byte {
	if l.pos+off >= len(l.src) {
		return 0
	}
	return l.src[l.pos+off]
}

func (l *Lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *Lexer) err(format string, args ...any) error {
	return &Error{Line: l.line, Col: l.col, Msg: fmt.Sprintf(format, args...)}
}

func (l *Lexer) next() (Tok, error) {
	// 跳过行内空格与制表符；\r 按空白处理（CRLF 文件常见）
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\r' {
			l.advance()
			continue
		}
		break
	}
	if l.pos >= len(l.src) {
		return Tok{Kind: EOF, Line: l.line, Col: l.col}, nil
	}

	c := l.src[l.pos]
	line, col := l.line, l.col

	if c == '\n' {
		l.advance()
		return Tok{Kind: NL, Text: "\n", Line: line, Col: col}, nil
	}

	// 注释：// 到行尾；/* */ 成一段，块注释不支持嵌套
	if c == '/' && l.at(1) == '/' {
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.advance()
		}
		return l.next()
	}
	if c == '/' && l.at(1) == '*' {
		l.advance()
		l.advance()
		for {
			if l.pos >= len(l.src) {
				return Tok{}, l.err("块注释没有完成")
			}
			if l.src[l.pos] == '*' && l.at(1) == '/' {
				l.advance()
				l.advance()
				return l.next()
			}
			l.advance()
		}
	}

	if isNameStart(c) {
		var b strings.Builder
		for l.pos < len(l.src) && isNamePart(l.src[l.pos]) {
			b.WriteByte(l.advance())
		}
		word := b.String()
		if l.pos < len(l.src) && l.src[l.pos] >= 0x80 {
			r, _ := utf8.DecodeRune(l.src[l.pos:])
			if isLetterRune(r) {
				return Tok{}, l.err("标识符不许含非 ASCII 字符：%s", word+string(r))
			}
		}
		return Tok{Kind: Name, Text: word, Line: line, Col: col}, nil
	}

	if isDigit(c) {
		return l.number(line, col)
	}

	if c == '"' {
		s, err := l.quoted('"')
		if err != nil {
			return Tok{}, err
		}
		return Tok{Kind: Str, Text: s, Line: line, Col: col}, nil
	}
	if c == '\'' {
		s, err := l.quoted('\'')
		if err != nil {
			return Tok{}, err
		}
		rs := []rune(s)
		if len(rs) != 1 {
			return Tok{}, &Error{Line: line, Col: col, Msg: fmt.Sprintf("单引号里必须恰好一个字符，这里是 %q", s)}
		}
		r := rs[0]
		if r >= 0xD800 && r <= 0xDFFF {
			return Tok{}, &Error{Line: line, Col: col, Msg: "代理区码点不能单独成一个字符"}
		}
		return Tok{Kind: Char, Text: s, Line: line, Col: col}, nil
	}

	rest := l.src[l.pos:]
	for i, op := range operators {
		if bytes.HasPrefix(rest, opBytes[i]) {
			for range op {
				l.advance()
			}
			return Tok{Kind: Op, Text: op, Line: line, Col: col}, nil
		}
	}
	if c >= 0x80 {
		r, _ := utf8.DecodeRune(l.src[l.pos:])
		if isLetterRune(r) {
			return Tok{}, l.err("标识符不许含非 ASCII 字符：看到 %q", string(r))
		}
		return Tok{}, l.err("认不出的字符 %q", string(r))
	}
	return Tok{}, l.err("认不出的字符 %q", string(c))
}

// number 扫一个数字字面量：十进制、`_` 分组、`0x` 十六进制、一个小数点。
// 不支持二进制、八进制、科学计数法。
//
// en: number scans a numeric literal: decimal, `_` digit groups, `0x` hexadecimal, one decimal
// point. There is no binary, no octal, no scientific notation.
func (l *Lexer) number(line, col int) (Tok, error) {
	start := l.pos
	if l.at(0) == '0' && (l.at(1) == 'x' || l.at(1) == 'X') {
		l.advance()
		l.advance()
		if !isHexDigit(l.src[l.pos]) {
			return Tok{}, l.err("0x 后面要跟十六进制数字")
		}
		for l.pos < len(l.src) && (isHexDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
			l.advance()
		}
	} else {
		intStart := l.pos
		for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
			l.advance()
		}
		// 不支持八进制；0 开头后还跟着多位数字的写法（如 0644）不能静默按十进制接受
		raw := strings.ReplaceAll(string(l.src[intStart:l.pos]), "_", "")
		if len(raw) > 1 && raw[0] == '0' {
			return Tok{}, l.err("%s 这种 0 开头的写法不认：没有八进制，要写十进制就照实写", string(l.src[intStart:l.pos]))
		}
	}
	isFloat := false
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		// `1.` 后面不接数字时不算小数点：该点属于成员访问
		if l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
			isFloat = true
			l.advance()
			for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.advance()
			}
		}
	}
	digits := string(l.src[start:l.pos])
	if l.pos < len(l.src) && (isNameStart(l.src[l.pos])) {
		r := l.src[l.pos]
		return Tok{}, l.err("数字后面直接跟了名字（%s 认不出）", digits+string(r))
	}
	return Tok{Kind: Num, Text: digits, Digits: digits, IsFloat: isFloat, Line: line, Col: col}, nil
}

// quoted 扫一段带引号的内容，接受五个转义序列，其余报错。
//
// en: quoted scans a quoted literal, accepting the five escape sequences and reporting an
// error for anything else.
func (l *Lexer) quoted(quote byte) (string, error) {
	l.advance() // 起始引号
	var b strings.Builder
	for {
		if l.pos >= len(l.src) || l.src[l.pos] == '\n' {
			return "", l.err("字符串没有完成就换行了")
		}
		c := l.advance()
		if c == quote {
			return b.String(), nil
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if l.pos >= len(l.src) {
			return "", l.err("反斜杠后面没有东西")
		}
		switch n := l.advance(); n {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'u':
			if l.pos+4 > len(l.src) {
				return "", l.err("\\u 后面要跟四个十六进制数字")
			}
			var v int
			for i := 0; i < 4; i++ {
				h := l.src[l.pos+i]
				if !isHexDigit(h) {
					return "", l.err("\\u 后面要跟四个十六进制数字")
				}
				v = v*16 + hexVal(h)
			}
			l.pos += 4
			l.col += 4
			b.WriteRune(rune(v))
		default:
			return "", l.err("不认识的转义 \\%s", string(n))
		}
	}
}

func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }
func isNameStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isNamePart(c byte) bool { return isNameStart(c) || isDigit(c) }

// isLetterRune 判断一个非 ASCII 字符是否形似字母，只用于在标识符之后报告"含非 ASCII 字符"。
// 取值覆盖 Unicode 的字母与汉字区段，满足该用途且不必引入 unicode 表。
//
// en: isLetterRune decides whether a non-ASCII character looks like a letter. It is only used
// to report "identifier contains non-ASCII characters" after an identifier, so the Unicode
// letter and ideograph ranges are enough and no unicode table needs to be imported.
func isLetterRune(r rune) bool {
	switch {
	case r >= 0x00C0 && r <= 0x024F: // 拉丁扩展
		return true
	case r >= 0x0400 && r <= 0x04FF: // 西里尔
		return true
	case r >= 0x3040 && r <= 0x30FF: // 日文假名
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // 汉字
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // 韩文
		return true
	}
	return false
}

func hexVal(c byte) int {
	switch {
	case c <= '9':
		return int(c - '0')
	case c <= 'F':
		return int(c-'A') + 10
	default:
		return int(c-'a') + 10
	}
}

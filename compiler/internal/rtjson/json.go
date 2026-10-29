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

// Package rtjson 是 json 组的 Go 参考实现。
//
// 它只做两件事：判断一段文本是否为严格 JSON、把它解析成一棵树。真正运行在产物里的实现
// 在 `internal/runtime/json.go`，由 Go 发机器码；这一份留着做双向比对，理由与 `internal/rtre`、
// `internal/rtfloat` 一样——按位翻译出的指令每一行都可能错在字节上，需要一份可读的对照表。
//
// 四条与 Go 标准库不一样的地方，测试里逐条固定：
//
//   - 单个代理字符（`\ud800` 无法构成代理对）判为非法；Go 用 U+FFFD 替换后继续解析。
//   - 键的先后顺序照原文保留，不像 `map[string]any` 那样打乱。
//   - 同一个键写两次：后一个覆盖前一个，位置仍是第一次那一个。
//   - 数只有两种面：整数面（`3`、`-12`）与小数面（`3.0`、`1e9`）。整数面装不进 int64
//     时退化为小数面，与机器码版走法一致。
//
// en: Package rtjson is the Go reference implementation of the json group. It does
// exactly two things: decide whether a piece of text is strict JSON, and break it into a tree.
// The implementation that actually runs in the product is internal/runtime/json.go, emitted as
// machine code by Go. This copy stays for the two-way comparison, for the same reason as
// internal/rtre and internal/rtfloat - instructions translated bit by bit can be wrong in any
// single byte, so a readable table of answers has to exist. Four places differ from the Go
// standard library, each fixed by a test: a lone surrogate (`\ud800` that forms no pair) is
// rejected, while Go substitutes U+FFFD and carries on; key order is preserved as written rather
// than shuffled like map[string]any; a key written twice - the later value overwrites the
// earlier one but the position stays the first one; and numbers have only two faces, an integer
// face (`3`, `-12`) and a float face (`3.0`, `1e9`) - an integer face that does not fit in
// int64 falls back to the float face, exactly as the machine-code version does.
package rtjson

import (
	"strconv"
	"unicode/utf8"
)

// Kind 是树上一个节点的种类。编号与机器码那一份共用，改一处要改两处。
//
// en: Kind is the kind of one node in the tree. The numbers are shared with the machine-code
// copy, so a change must be made in both places.
type Kind uint8

const (
	KNull Kind = iota // JSON 的 null
	KTrue
	KFalse
	KInt   // 整数面的数
	KFloat // 带小数点或指数的数，也包括装不进 int64 的整数面
	KStr
	KArr
	KObj
)

// kindNames 是 json.kind 返回的那六个词；KTrue 与 KFalse 都指 boolean，KInt 与 KFloat 都指 number。
//
// en: kindNames is the six words json.kind returns; KTrue and KFalse both answer boolean, KInt
// and KFloat both answer number.
var kindNames = [...]string{"null", "boolean", "boolean", "number", "number", "string", "array", "object"}

// Name 返回 json.kind 该说的那一个词。
//
// en: Name returns the word json.kind should say.
func (k Kind) Name() string { return kindNames[k] }

// Reason 表示错误类别。0 表示没有错误。
//
// en: Reason names the kind of error. 0 means no error.
type Reason int

const (
	RNone      Reason = iota
	RTruncated        // 文本到头了，该值还没读完
	RValue            // 这个位置不是任何一种值的开头
	RObjClose         // 对象里这里该是一个逗号或者右花括号
	RKeyQuote         // 对象里这里该有一个引号起头的键
	RColon            // 键后面该有一个冒号
	RArrClose         // 数组里这里该是一个逗号或者右方括号
	RStrClose         // 文本少了收尾的那一个引号
	RControl          // 文本里有一个没写转义的控制字符
	REScape           // 反斜杠后面跟着一个用不了的转义
	RHex              // \u 后面不是四个十六进制的数字
	RSurrogate        // 半个代理字符，无法构成代理对
	RNumber           // 这个数的写法不合规则
	RNullLit          // null 拼到一半就断了
	RTrueLit          // true 拼到一半就断了
	RFalseLit         // false 拼到一半就断了
	RJunk             // 值的末尾多出来了东西
)

// reasonText 是面向用户的说明正文，"第几行第几列"由 Explain 拼在前面。
//
// en: reasonText holds the body of the user-facing message; Explain prepends "line n, column m".
var reasonText = [...]string{
	RNone:      "",
	RTruncated: "这一片到这儿就断了，后头还差东西",
	RValue:     "这里该是一个值",
	RObjClose:  "这里该是一个逗号或者右花括号",
	RKeyQuote:  "这里该有一个引号起头的键",
	RColon:     "键的后面该有一个冒号",
	RArrClose:  "这里该是一个逗号或者右方括号",
	RStrClose:  "文本少了收尾的那一个引号",
	RControl:   "文本里有一个没写转义的控制字符",
	REScape:    "反斜杠后面跟着一个用不了的转义",
	RHex:       "u 后面该是四个十六进制的数字",
	RSurrogate: "这里有一个配不成对的半个代理字符",
	RNumber:    "这个数的写法不合规矩",
	RNullLit:   "null 拼到一半就断了",
	RTrueLit:   "true 拼到一半就断了",
	RFalseLit:  "false 拼到一半就断了",
	RJunk:      "值的尾巴上多出来了东西",
}

// ReasonText 返回说明正文；0 号返回空文本。
//
// en: ReasonText returns the body; reason 0 returns empty text.
func ReasonText(r Reason) string {
	if r <= RNone || int(r) >= len(reasonText) {
		return ""
	}
	return reasonText[r]
}

// Node 是树上的一个节点。Keys 与 Vals 等长；KInt 同时存一份小数表示，
// 与机器码里那两个槽位（整数一个、小数一个）保持同一形状。
//
// en: Node is one node of the tree. Keys and Vals are always the same length, and KInt also
// keeps a float on the side - the same shape as the two positions in machine code (one for the
// integer, one for the float).
type Node struct {
	Kind  Kind
	Int   int64
	Float float64
	Text  string // KStr 解过码的值
	Keys  []string
	Vals  []*Node
}

// put 收下写入的一对：键已存在就换掉那个位置的值，位置不动（重复键规则）。
//
// en: put stores a pair being written: if the key is already there, the value at that position
// is replaced and the position itself does not move (the duplicate-key rule).
func (n *Node) put(key string, v *Node) {
	for i, k := range n.Keys {
		if k == key {
			n.Vals[i] = v
			return
		}
	}
	n.Keys = append(n.Keys, key)
	n.Vals = append(n.Vals, v)
}

// Tree 是一趟解析的答复。Root 为空指针就说明这段文本不是合法 JSON，Reason 与 Pos 指出错在哪。
//
// en: Tree is the answer of one parse. A nil Root means the text is not valid JSON, and Reason
// with Pos say where it broke.
type Tree struct {
	Root   *Node
	Reason Reason
	Pos    int // 错在哪一个字节上（从 0 数）
}

// 下面这些常量是一个节点在堆上的字段偏移，机器码那份按同一布局摆放；改一处要改两处。
//
// en: The constants below are the field offsets of one node on the heap; the machine-code copy
// lays it out from the same table, so a change must be made in both places.
const (
	NodeSz = 48
	IKind  = 0
	IInt   = 8
	IFloat = 16
	IText  = 24
	IKeys  = 32
	IVals  = 40
)

// 数的上界：正数到 2^63-1，负数绝对值到 2^63，与 text.to_int 相同的两条界限。
//
// en: Bounds for numbers: positive up to 2^63-1, negative magnitude up to 2^63 - the same two
// limits as text.to_int.
const (
	intLim  = int64(922337203685477580)
	intMax7 = 7
	intMax8 = 8
)

// parser 是这台机器的一次运行。位置按字节数，与机器码那一版完全一致。
//
// en: parser is one run of this machine. Positions are counted in bytes, identical to the
// machine-code version.
type parser struct {
	s   string
	i   int
	bad Reason
}

// note 记录错误类别与字节位置，返回空节点让上层原样回退。
// 首次记录的那一条生效：后续各层只是在回退，不再改写。
//
// 名字不叫 fail：check 里"编译器对用户说的每一句话"的清单按 `fail` 收集句子
// （messages_test.go），而这一份是给用户看的对照表，它输出的中文不计入那份清单。
//
// en: note records which kind of problem, at which byte, and returns an empty node so the level
// above retreats unchanged. The first note recorded takes effect: every later level is only
// unwinding and must not overwrite it. The name is not `fail` because the check that collects
// "every sentence the compiler says to the user" gathers sentences by the name fail
// (messages_test.go), and this file is a comparison table for humans - its Chinese is not
// counted in that ledger.
func (p *parser) note(at Reason) *Node {
	if p.bad == RNone {
		p.bad = at
	}
	return nil
}

// Parse 解析一整段文本。返回的 Root 为空就是这段文本不合法。
//
// en: Parse reads a whole piece of text. An empty Root means the text is invalid.
func Parse(text string) Tree {
	p := &parser{s: text}
	root := p.value()
	if root == nil {
		return Tree{Reason: p.bad, Pos: p.i}
	}
	p.ws()
	if p.i < len(p.s) {
		p.bad = RJunk
		return Tree{Reason: p.bad, Pos: p.i}
	}
	return Tree{Root: root}
}

// ws 跳过 JSON 允许的四种空白：空格、制表、换行、回车。其他字符（换页、竖 tab）不算空白。
//
// en: ws skips the four whitespace characters JSON accepts: space, tab, line feed, carriage
// return. Others (form feed, vertical tab) are not whitespace.
func (p *parser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

// value 解析一个值。所有调用它的路径都已先跳过空白，所以这一层不再跳。
//
// en: value parses one value. Every path that calls it skips whitespace first, so this level
// does not skip again.
func (p *parser) value() *Node {
	p.ws()
	if p.i >= len(p.s) {
		return p.note(RTruncated)
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		s, ok := p.text()
		if !ok {
			return nil
		}
		return &Node{Kind: KStr, Text: s}
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c == 't':
		return p.lit("true", KTrue, RTrueLit)
	case c == 'f':
		return p.lit("false", KFalse, RFalseLit)
	case c == 'n':
		return p.lit("null", KNull, RNullLit)
	default:
		return p.note(RValue)
	}
}

// lit 识别那三个整词字面量：整段比中才前移位置。
//
// en: lit recognises the three single-word literals, and only advances the position when the
// whole span matched.
func (p *parser) lit(word string, k Kind, off Reason) *Node {
	rest := p.s[p.i:]
	n := 0
	for n < len(word) && n < len(rest) && rest[n] == word[n] {
		n++
	}
	if n == len(word) {
		p.i += n
		return &Node{Kind: k}
	}
	p.i += n // 停在第一个不匹配的字节上；逐字节都对上但文本更短，那就是文本尽头
	return p.note(off)
}

// text 解析字符串本体：以引号开始、以引号结束，返回解码后的内容。键与值都走这一步。
//
// 全程没有转义时把原文整段交出（机器码版就是 @str_sub，一次分配）；
// 有转义才逐位累积。
//
// en: text parses the body of a string: it starts at a quote, ends at a quote, and returns the
// decoded contents. Both keys and values take this step. When nothing needed escaping the
// original span is handed over whole (the machine-code version is @str_sub, one allocation);
// only when there is an escape does it accumulate one position at a time.
func (p *parser) text() (string, bool) {
	start := p.i + 1 // 正文起点：头一个引号不算
	p.i++
	var out []byte
	esc := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			if !esc {
				return p.s[start : p.i-1], true
			}
			return string(out), true
		case c < 0x20:
			p.note(RControl)
			return "", false
		case c != '\\':
			if esc { // 已开始逐位累积，未转义的字节也要接上
				out = append(out, c)
			}
			p.i++
		default:
			if !esc {
				out = append(out, p.s[start:p.i]...)
				esc = true
			}
			p.i++ // 吃掉反斜杠
			if p.i >= len(p.s) {
				p.note(RTruncated)
				return "", false
			}
			if !p.escape(&out) {
				return "", false
			}
		}
	}
	p.note(RStrClose)
	return "", false
}

// escape 处理反斜杠后面那一段，把解出来的字节接在 *out 上。走进来时 p.i 就指着那一个字母。
//
// en: escape handles what follows the backslash and appends the decoded bytes to *out. On entry
// p.i already points at that letter.
func (p *parser) escape(out *[]byte) bool {
	e := p.s[p.i]
	switch e {
	case '"', '\\', '/':
		*out, p.i = append(*out, e), p.i+1
	case 'b':
		*out, p.i = append(*out, 0x08), p.i+1
	case 'f':
		*out, p.i = append(*out, 0x0C), p.i+1
	case 'n':
		*out, p.i = append(*out, '\n'), p.i+1
	case 'r':
		*out, p.i = append(*out, '\r'), p.i+1
	case 't':
		*out, p.i = append(*out, '\t'), p.i+1
	case 'u':
		p.i++
		cp, ok := p.hex4()
		if !ok {
			return false
		}
		switch {
		case cp >= 0xDC00 && cp <= 0xDFFF: // 低位代理单独起头，无法构成代理对
			p.note(RSurrogate)
			return false
		case cp >= 0xD800 && cp <= 0xDBFF:
			if p.i+1 >= len(p.s) || p.s[p.i] != '\\' || p.s[p.i+1] != 'u' {
				p.note(RSurrogate)
				return false
			}
			p.i += 2
			low, ok := p.hex4()
			if !ok {
				return false
			}
			if low < 0xDC00 || low > 0xDFFF {
				p.note(RSurrogate)
				return false
			}
			*out = append(*out, encode(0x10000+(cp-0xD800)*0x400+(low-0xDC00))...)
		default:
			*out = append(*out, encode(cp)...)
		}
	default:
		p.note(REScape) // 位置就停在这个字母上
		return false
	}
	return true
}

// hex4 读取 \u 后面的四个十六进制数字，返回对应的码点。
//
// en: hex4 reads the four hexadecimal digits after \u and returns that code point.
func (p *parser) hex4() (uint32, bool) {
	var cp uint32
	for k := 0; k < 4; k++ {
		if p.i >= len(p.s) {
			p.note(RTruncated)
			return 0, false
		}
		v, ok := hexVal(p.s[p.i])
		if !ok {
			p.note(RHex)
			return 0, false
		}
		cp = cp*16 + v
		p.i++
	}
	return cp, true
}

func hexVal(c byte) (uint32, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint32(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint32(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return uint32(c-'A') + 10, true
	}
	return 0, false
}

// encode 把一个码点编码为 UTF-8，算法与机器码里 x64 的 utf8 那一步完全一致：
// 首字节按长度或上不同的前缀掩码（0、0xC0、0xE0、0xF0），后续每个字节取 6 个位再或上 0x80。
//
// en: encode encodes one code point as UTF-8, using byte-for-byte the same algorithm as the
// utf8 step in x64: the lead byte ORs in a different mask depending on length (0, 0xC0,
// 0xE0, 0xF0) and each following byte takes six bits plus 0x80.
func encode(cp uint32) []byte {
	n := 1
	switch {
	case cp < 0x80:
	case cp < 0x800:
		n = 2
	case cp < 0x10000:
		n = 3
	default:
		n = 4
	}
	lead := []uint32{0x00, 0xC0, 0xE0, 0xF0}[n-1]
	out := make([]byte, n)
	for j := 0; j < n; j++ {
		v := cp >> uint(6*(n-1-j))
		if j > 0 {
			v = v&0x3F | 0x80
		} else {
			v |= lead
		}
		out[j] = byte(v)
	}
	return out
}

// number 解析一个数：先校验写法，再决定它取整数面还是小数面。
//
// en: number parses one number: the spelling is validated first, then it is decided whether the
// value takes the integer face or the float face.
func (p *parser) number() *Node {
	start := p.i
	neg := false
	if p.s[p.i] == '-' {
		neg = true
		p.i++
	}
	if p.i >= len(p.s) {
		p.note(RTruncated)
		return nil
	}
	if p.s[p.i] == '0' { // 以 0 起头时只能是一个 0，不接受 01
		p.i++
		if p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.note(RNumber)
			return nil
		}
	} else if !p.digits() {
		return p.note(RNumber)
	}
	frac, exp := false, false
	if p.i < len(p.s) && p.s[p.i] == '.' {
		frac = true
		p.i++
		if !p.digits() {
			return p.note(RNumber)
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		exp = true
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if !p.digits() {
			return p.note(RNumber)
		}
	}
	face := p.s[start:p.i]
	if frac || exp {
		return &Node{Kind: KFloat, Float: parseFloat(face)}
	}
	v, ok := accum(face, neg)
	if !ok { // 装不进 int64 的整数面退化为小数面，与机器码那一版同一个走法
		return &Node{Kind: KFloat, Float: parseFloat(face)}
	}
	return &Node{Kind: KInt, Int: v, Float: float64(v)}
}

// parseFloat 在 float.go：那一版与运行期 @str_to_float 同一算法，不采用 Go 的正确舍入。

// digits 统计一串十进制数字，一个都没有时返回 false。
//
// en: digits counts a run of decimal digits and returns false when there is none.
func (p *parser) digits() bool {
	n := 0
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
		n++
	}
	return n > 0
}

// accum 把整数面的数字串折叠为 int64；溢出时返回 false。
// 乘法之前先与上界的十分之一比较，恰好等于该值时最后一位还要与符号界限比较——与 @str_to_int 同一套。
//
// en: accum folds the integer-face digits into an int64, returning false when it no longer
// fits. The comparison against one tenth of the bound happens BEFORE the multiplication, and
// when the value sits exactly on that line the last digit is checked against the sign - the
// same scheme as @str_to_int.
func accum(face string, neg bool) (int64, bool) {
	i := 0
	if i < len(face) && face[i] == '-' {
		i++
	}
	var acc int64
	for ; i < len(face); i++ {
		d := int64(face[i] - '0')
		lim := int64(intMax7)
		if neg {
			lim = intMax8
		}
		if acc > intLim || (acc == intLim && d > lim) {
			return 0, false
		}
		acc = acc*10 + d
	}
	if neg {
		acc = -acc
	}
	return acc, true
}

// object 解析一个 `{...}`。
//
// en: object parses a `{...}`.
func (p *parser) object() *Node {
	n := &Node{Kind: KObj}
	p.i++ // 吃掉 {
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return n
	}
	for {
		p.ws()
		if p.i >= len(p.s) {
			return p.note(RTruncated)
		}
		if p.s[p.i] != '"' {
			return p.note(RKeyQuote)
		}
		key, ok := p.text()
		if !ok {
			return nil
		}
		p.ws()
		if p.i >= len(p.s) {
			return p.note(RTruncated)
		}
		if p.s[p.i] != ':' {
			return p.note(RColon)
		}
		p.i++
		v := p.value()
		if v == nil {
			return nil
		}
		n.put(key, v)
		p.ws()
		if p.i >= len(p.s) {
			return p.note(RTruncated)
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return n
		default:
			return p.note(RObjClose)
		}
	}
}

// array 解析一个 `[...]`。
//
// en: array parses a `[...]`.
func (p *parser) array() *Node {
	n := &Node{Kind: KArr}
	p.i++ // 吃掉 [
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return n
	}
	for {
		v := p.value()
		if v == nil {
			return nil
		}
		n.Vals = append(n.Vals, v)
		p.ws()
		if p.i >= len(p.s) {
			return p.note(RTruncated)
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return n
		default:
			return p.note(RArrClose)
		}
	}
}

// Explain 返回面向用户的说明："第 3 行第 12 列：这里该是一个值"。文本无误时返回空串。
//
// en: Explain returns the user-facing message: "line 3, column 12: expected a value here". Valid
// text returns an empty string.
func (t Tree) Explain(text string) string {
	if t.Reason == RNone {
		return ""
	}
	line, col := LineCol(text, t.Pos)
	return "第 " + strconv.Itoa(line) + " 行第 " + strconv.Itoa(col) + " 列：" + ReasonText(t.Reason)
}

// LineCol 把字节位置换算为行号与列号，两者都从 1 起。列按码点计：UTF-8 的续字节
// 一律落在 0x80～0xBF，跳过它们即按字符计数。
//
// en: LineCol turns a byte position into a line and column, both counted from 1. The column
// counts code points: UTF-8 continuation bytes always fall in 0x80-0xBF, so skipping them means
// counting characters.
func LineCol(text string, at int) (line, col int) {
	if at > len(text) {
		at = len(text)
	}
	begin := 0
	for i := 0; i < at; i++ {
		if text[i] == '\n' {
			line++
			begin = i + 1
		}
	}
	line++
	col = 1
	for i := begin; i < at; i++ {
		if c := text[i]; c < 0x80 || c >= 0xC0 {
			col++
		}
	}
	return
}

// Runes 统计一段 UTF-8 中的码点数。
//
// en: Runes counts the code points in a piece of UTF-8.
func Runes(s string) int { return utf8.RuneCountInString(s) }

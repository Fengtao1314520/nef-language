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

// doc.go 取写在声明上面那一段注释，交给悬停与补全的说明栏。
//
// 注释在语言里没有语义：词法层把 `//` 与 `/* */` 两种注释整个丢掉，检查器与后端
// 从来看不见它们。这一头要的只是"人写在这一个声明上面的那几句话"，所以不回前端去改词法与
// 语法——那会把注释变成语言的一部分——而是在文本上按行列把它读回来。
//
// 读法照主流编辑器那一条：紧贴着声明上面、中间不隔空行的那一段算这一个声明的文档注释。
// `/** ... */`、`/* ... */`、`//`、`///` 四种都收：行首的 `*` 与两头的记号去掉。
//
// 段里认两个前缀（只是编辑器约定，语言自己不看它们）：`en: ` 起的那一段是英文，
// `例：`（`Example:` 也算）起的那一段是用法示例。屏幕上得到三段——中文、英文、一个 nf 围栏里的
// 例子，两个前缀都不显示。`@param`、`@return` 这一类标签语言没规定意思，工具替人编一个
// 意思比留原话更容易错，所以照原样显示。
//
// en: doc.go takes the comment written above a declaration and hands it to hover and the
// completion detail column. Comments carry no meaning in the language: the lexer discards both
// // and /* */ entirely, and the checker and backend never see them. All this needs is
// "the few lines a person wrote above this declaration", so the front end is not changed - that
// would make comments part of the language - and the text is re-read by line and column
// instead. The reading rule is the mainstream editor's: the block that sits directly above the
// declaration with no blank line between is its doc comment. All four forms are accepted - /**
// ... */, /* ... */, //, /// - with a leading * on each line and the surrounding markers
// stripped. Inside the block two prefixes are recognised (an editor convention; the language
// itself never looks at them): a section starting with `en: ` is English, one starting
// with `例：` (or `Example:`) is a usage example. The screen gets three sections - Chinese,
// English, an example inside an nf fence - and neither prefix is displayed. Tags such as
// `@param` and `@return` have no meaning defined by the language, and a tool inventing one is
// more wrong than showing the text as written, so they are displayed verbatim.
package ls

import (
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/pkgload"
)

// declPos 找那一个节点写在哪一份文件的第几行。只认顶层的东西：函数、`class`／`struct`／
// `interface`／`enum`／类型别名，与顶层的 `let`／`const`／`shared`。方法挂在它所属的类上，
// 由调用方把方法节点 itself 传进来——它在 Object.Defs 里跟着类一起遍历到。
//
// en: declPos finds which line of which file a node was written on. Only top-level things are
// recognised: functions, class/struct/interface/enum/type alias, and top-level
// let/const/shared. A method hangs on the class it belongs to, and the caller passes the method
// node itself in - it is reached while walking Object.Defs alongside the class.
func declPos(prog *check.Program, node any) (file string, at ast.Pos, ok bool) {
	if prog == nil || node == nil {
		return "", ast.Pos{}, false
	}
	for _, f := range prog.Files {
		for _, d := range f.Defs {
			if sameNode(d, node) {
				return f.Path, d.Where(), true
			}
			if o, isObj := d.(*ast.Object); isObj {
				for _, m := range o.Methods {
					if sameNode(m, node) {
						return f.Path, m.Where(), true
					}
				}
				for _, fl := range o.Fields {
					if sameNode(fl, node) {
						return f.Path, fl.Pos, true
					}
				}
			}
		}
		for _, s := range f.Stmts {
			if dd, isDecl := s.(*ast.Decl); isDecl && sameNode(dd, node) {
				return f.Path, dd.Where(), true
			}
		}
	}
	return "", ast.Pos{}, false
}

// sameNode 比的是节点的指针：同一份语法树里同一个节点才是同一个东西。
//
// en: sameNode compares node pointers: within one syntax tree, the same node is the same thing.
func sameNode(node, want any) bool {
	return node != nil && node == want
}

// docAbove 交回写在第 line 行（1 起）上面那一段注释；没有就交回空串。
//
// en: docAbove returns the comment written above line `line` (1-based), or an empty string when
// there is none.
func docAbove(lines []string, line int) string {
	i := line - 2 // line 是 1 起的声明那一行，减一回 0 起，再往上一行才是注释
	if i < 0 {
		return ""
	}
	if strings.TrimSpace(lines[i]) == "" {
		return "" // 隔了一个空行：那一段是给别的东西写的，或者随手留的备注
	}
	var out []string
	if strings.HasSuffix(strings.TrimSpace(lines[i]), "*/") {
		start := i
		for start >= 0 && !strings.Contains(lines[start], "/*") {
			start--
		}
		if start < 0 {
			return ""
		}
		out = append(out, cleanDoc(lines[start:i+1])...)
		i = start - 1
	}
	for i >= 0 {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "//") {
			break
		}
		out = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "//"))}, out...)
		i--
	}
	return strings.Join(trimBlankEdges(out), "\n")
}

// cleanDoc 去掉一段块注释的外壳：两头的 `/*`、`*/`，每一行行首的 `*`。
//
// en: cleanDoc removes the shell of a block comment: the opening /* and closing */, plus a
// leading * on every line.
func cleanDoc(block []string) []string {
	out := make([]string, 0, len(block))
	for _, l := range block {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "/*")
		l = strings.TrimSuffix(l, "*/")
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "*")
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

func trimBlankEdges(in []string) []string {
	out := in
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// docFor 问一句：这一个节点上面写了什么注释。找不到的那几种（局部量、参数、内建那一条动作）
// 交回空串。交回的是排好版的 markdown：中文一句、英文一句、示例一个围栏（见 renderDoc）。
//
// en: docFor asks what comment is written above this node. The kinds with nothing to find
// (locals, parameters, built-in actions) return an empty string. What comes back is laid-out
// markdown: a Chinese sentence, an English sentence, an example in a fence (see renderDoc).
func docFor(prog *check.Program, entry, text string, node any) string {
	file, at, ok := declPos(prog, node)
	if !ok {
		return ""
	}
	lines := fileLines(prog, entry, text, file)
	if lines == nil || at.Line < 1 || at.Line > len(lines)+1 {
		return ""
	}
	return renderDoc(docAbove(lines, at.Line))
}

// 注释里的两个前缀（编辑器约定，不参与语言本身）：
//
//	// en: Records a step; a passed case prints nothing.
//	// 例：
//	// run.step("点开登录")
//
// 中文那一段在前，`en: ` 起的是英文，`例：`（或 `Example:`）起的是用法示例。两个前缀都不上屏：
// 屏幕上得到的是三段——中文、英文、一个 nf 围栏里的例子。
//
// en: The two prefixes inside a comment (an editor convention, not part of the
// language): // en: Records a step; a passed case prints nothing. // 例： // run.step("点开登录") The
// Chinese section comes first, `en: ` starts the English one, `例：` (or `Example:`) starts the
// usage example. Neither prefix reaches the screen: what shows is three sections - Chinese,
// English, and one example inside an nf fence.
const (
	markEn  = "en:"
	markEg  = "例："
	markEg2 = "例:"
	markEg3 = "example:"
)

// docParts 把一段注释按那两个前缀分成三块。落在哪个前缀之后的普通行就归哪一块；
// 没有前缀的开头那几行归中文那一段（库里绝大多数注释本来就是这样写的）。
//
// en: docParts splits one comment into three blocks by those two prefixes. A plain line belongs
// to whichever prefix precedes it; the leading lines with no prefix belong to the Chinese
// section, which is how most comments in the libraries are already written.
func docParts(block string) (zh, en, eg string) {
	var bZh, bEn, bEg []string
	here := "zh"
	for _, l := range strings.Split(block, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case hasPrefixFold(t, markEn):
			here = "en"
			bEn = append(bEn, strings.TrimSpace(t[len(markEn):]))
			continue
		case strings.HasPrefix(t, markEg) || strings.HasPrefix(t, markEg2) || hasPrefixFold(t, markEg3):
			here = "eg"
			bEg = append(bEg, trimExampleMark(t))
			continue
		}
		switch here {
		case "en":
			bEn = append(bEn, t)
		case "eg":
			bEg = append(bEg, l) // 示例里的缩进照原样，别把写代码的人排好的形状拆掉
		default:
			bZh = append(bZh, t)
		}
	}
	return joinEdge(bZh), joinEdge(bEn), joinEdge(bEg)
}

// trimExampleMark 去掉 `例：` 那一个前缀，留着后面可能跟在同一行的那一段代码。
//
// en: trimExampleMark removes the `例：` prefix while keeping any code that may follow it on the
// same line.
func trimExampleMark(t string) string {
	for _, m := range []string{markEg, markEg2, markEg3} {
		if len(t) >= len(m) && strings.EqualFold(t[:len(m)], m) {
			return strings.TrimSpace(t[len(m):])
		}
	}
	return t
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func joinEdge(in []string) string {
	return strings.Join(trimBlankEdges(in), "\n")
}

// renderDoc 把三块排成 markdown：中文、英文各一段，示例单独一个 nf 围栏。
// 三块都空就交回空串——屏幕上只留签名那一行，不凑话。
//
// en: renderDoc lays the three blocks out as markdown: Chinese and English each a paragraph,
// the example as its own nf fence. If all three are empty it returns an empty string - the
// screen keeps just the signature line rather than manufacturing prose.
func renderDoc(block string) string {
	zh, en, eg := docParts(block)
	var parts []string
	for _, p := range []string{zh, en} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if eg != "" {
		parts = append(parts, "```nf\n"+strings.TrimRight(eg, "\n")+"\n```")
	}
	return strings.Join(parts, "\n\n")
}

// fileLines 交回那一份文件的行：手上这一份用编辑器推过来的文本，别的花名从盘上读
// （`import` 进来的那些本来就在盘上，检查走的是同一份内容）。
//
// en: fileLines returns the lines of a file: the one in hand uses the text the editor pushed,
// other names are read from disk (the files behind `import` are on disk anyway, and the check
// runs over the same content).
func fileLines(prog *check.Program, entry, text, path string) []string {
	if path == entry {
		return splitLines(text)
	}
	for _, f := range prog.Files {
		if f.Path != path {
			continue
		}
		src, ok := pkgload.New().ReadSource(path)
		if !ok {
			return nil
		}
		return splitLines(src)
	}
	return nil
}

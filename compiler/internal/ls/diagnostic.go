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

// diagnostic.go 把前端那一串带行列的报错翻成编辑器画得出来的波浪线。
//
// 两处换算是这里的全部学问：
//   - nf 的列是 1 起的字节列（internal/lexer/lexer.go 的 advance 每吃一个字节加一），
//     LSP 的列是 0 起的 UTF-16 单元数，行里有过中文就得换算，不然波浪线整体右移。
//   - 报错常常落在行尾之后（"想要 )，实际看到 行尾"就指在换行那个位置），
//     这时倒着退一个位置盖住行尾最后一个字符，不然画出一条零宽的线，屏幕上什么都看不见。
//
// 每一份参与检查的文件都自带自己的文本与记号宽度：错在 import 那一侧时，拿当前文件的行
// 去换另一份文件的列，画出来的是错地方。
//
// en: diagnostic.go turns the front end's line-and-column errors into squiggles an editor can
// draw. The whole craft is two conversions: nf columns are 1-based byte columns (advance in
// internal/lexer/lexer.go adds one per byte) while LSP columns count 0-based UTF-16 units, so
// any line containing Chinese has to be converted or every squiggle shifts right; and errors
// often land past the end of the line ("expected ), saw end of line" points at the newline), in
// which case the position steps back one character to cover the last real character - otherwise
// a zero-width squiggle is drawn and nothing is visible on screen. Every file that took part in
// the check carries its own text lines and token widths: when the error is on the import side,
// using the current file's lines to convert another file's column draws the squiggle in the
// wrong place.
package ls

import (
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/parser"
	"neflang.org/nf/internal/pkgload"
)

// unit 是一份参与这一趟检查的文件，带着它自己的文本行与记号宽度。
//
// en: unit is one file taking part in this check pass, carrying its own text lines and token
// widths.
type unit struct {
	lines  []string
	widths map[[2]int]int
}

// at 把一处 1 起、按字节数的行列报错翻成一条诊断。
//
// en: at converts one 1-based, byte-counted line/column error into a diagnostic.
func (u *unit) at(line, col int, msg string) Diagnostic {
	if line < 1 {
		line = 1
	}
	if line > len(u.lines) {
		line = len(u.lines)
	}
	text := u.lines[line-1]

	byteCol := col - 1
	if byteCol < 0 {
		byteCol = 0
	}
	if byteCol > len(text) {
		byteCol = len(text)
	}
	width := u.widths[[2]int{line, col}]
	if width < 0 {
		width = 0
	}
	if byteCol+width > len(text) {
		width = len(text) - byteCol
	}
	start := utf16Len(text[:byteCol])
	end := utf16Len(text[:byteCol+width])
	if end != start {
		return diag(line-1, start, end, msg)
	}
	// 零宽的那几种（落在行尾之后、落在 EOF 上）退一个位置，盖住行尾最后一个字符。
	if byteCol == 0 {
		return diag(line-1, 0, utf16Len(text), msg)
	}
	prev := prevRuneStart(text, byteCol)
	return diag(line-1, utf16Len(text[:prev]), start, msg)
}

func diag(line, from, to int, msg string) Diagnostic {
	return Diagnostic{
		Range:    Range{Start: Position{Line: line, Character: from}, End: Position{Line: line, Character: to}},
		Severity: SeverityError,
		Message:  msg,
		Source:   "nf",
	}
}

// Diagnostics 从内存里这一份文本走完 词法 → 语法 → 静态检查，返回按文件分开的诊断。
//
// 当前这一份文件不读盘；`import` 指到的那些才从盘上读，读不进来就在这一句 import 上记一条。
// 返回的表里每一参与的文件都有一个键（哪怕那一份没问题），为的是编辑器能把上一轮的波浪线擦掉。
//
// en: Diagnostics runs lexer, parser and static check over the text held in memory and returns
// diagnostics grouped by file. The current file is not read from disk; only the files `import`
// points at are, and one that cannot be read is noted on that import line. Every file that took
// part gets a key in the returned table, even a clean one, so the editor can erase last round's
// squiggles.
func Diagnostics(path, text string) map[string][]Diagnostic {
	fe := frontEnd(path, text)
	out := map[string][]Diagnostic{path: {}}
	if fe.early != nil {
		out[path] = []Diagnostic{*fe.early}
		return out
	}
	for _, e := range check.Run(fe.prog) {
		file := e.File
		u, ok := fe.units[file]
		if !ok {
			file, u = path, fe.entry
		}
		out[file] = append(out[file], u.at(e.Line, e.Col, e.Msg))
	}
	for _, m := range fe.missing {
		out[path] = append(out[path], fe.entry.at(m.line, m.col, m.msg))
	}
	for p := range fe.units {
		if _, ok := out[p]; !ok {
			out[p] = []Diagnostic{}
		}
	}
	return out
}

// front 是一份文本走完词法与语法的结果，加上这一趟参与检查的那几份文件。
// early 非空说的是前两步就过不去：那时候没有程序可查，波浪线只有那一条。
//
// en: front is the result of lexing and parsing one text, plus the files that took part in this
// pass. A non-empty early means the first two steps already failed: there is no program to
// check then, and the only squiggle is that one.
type front struct {
	prog    *check.Program
	units   map[string]*unit
	entry   *unit
	missing []plain
	early   *Diagnostic
}

// frontEnd 是波浪线、补全与悬停共用的那一次装载：三份算法要的是同一份文本、同一套 import 找法，
// 各写一遍迟早会漂。
//
// en: frontEnd is the single load shared by squiggles, completion and hover: all three want the
// same text and the same import resolution, and writing it three times would drift.
func frontEnd(path, text string) *front {
	entry := &unit{lines: splitLines(text)}
	fe := &front{units: map[string]*unit{path: entry}, entry: entry}
	toks, err := lexer.Lex(text)
	if err != nil {
		d := entry.at(errPos(err), errCol(err), errText(err))
		fe.early = &d
		return fe
	}
	entry.widths = tokenWidths(entry.lines, toks)
	f, err := parser.Parse(path, toks)
	if err != nil {
		d := entry.at(errPos(err), errCol(err), errText(err))
		fe.early = &d
		return fe
	}
	fe.prog = &check.Program{Entry: path, Files: []*ast.File{f}}
	fe.missing = loadImports(fe.prog, fe.units, map[string]bool{path: true}, pkgload.New(), f, "")
	return fe
}

// inspect 走完前端并留下结论表：补全与悬停问的是"检查器算出了什么"，不是"哪里报错"。
// 过不去词法语法时返回 nil——那一句写到一半，检查器根本没机会算出点号左边是什么类型。
//
// en: inspect runs the front end and keeps the resulting tables: completion and hover ask "what
// did the checker compute", not "where did it error". It returns nil when lexing or parsing
// fails - a half-written statement never gives the checker a chance to work out the type left
// of the dot.
func inspect(path, text string) (*check.Program, *check.Facts) {
	fe := frontEnd(path, text)
	if fe.early != nil {
		return nil, nil
	}
	_, facts := check.RunWithFacts(fe.prog, true)
	return fe.prog, facts
}

type plain struct {
	line, col int
	msg       string
}

// loadImports 递归把 import 指到的文件读进来，连它们自己的 import 一起读。
//
// 读不进来（没有这个文件、词法或语法过不去）只在这一句 import 上记一条，不去拦别的文件：
// 新建一份还没写的文件是最常见的写法，这时候要的是一行提示，不是一屏"没有 xxx 这个名字"。
//
// root 是这一份文件所属的包根（工程自己的文件为空），整块挂名那一种引入由 pk 解析。
//
// en: loadImports recursively reads the files import points at, together with their own
// imports. A file that cannot be read (missing, or failing lex or parse) is noted on that
// import line only and does not block the other files: creating a new file before writing it is
// the most common case, and what is wanted then is one hint, not a screen full of "no such name
// xxx". root is the package root this file belongs to (empty for the project's own files), and
// whole-module imports are resolved by pk.
func loadImports(prog *check.Program, units map[string]*unit, seen map[string]bool, pk *pkgload.Loader, f *ast.File, root string) []plain {
	var missing []plain
	if prog.PkgRefs == nil {
		prog.PkgRefs = map[string]string{}
	}
	if prog.PkgRoots == nil {
		prog.PkgRoots = map[string]string{}
	}
	for _, imp := range f.Imports {
		if imp.Package != "" {
			newRoot, facade, err := pk.Resolve(prog, f.Path, imp.Path, root)
			if err != nil {
				missing = append(missing, plain{line: imp.PathPos.Line, col: imp.PathPos.Col, msg: err.Error()})
				continue
			}
			if seen[facade] {
				continue
			}
			if g, u, ok := parseFile(pk, facade); ok {
				g.Path = facade
				seen[facade] = true
				prog.PkgRoots[facade] = newRoot
				units[facade] = u
				prog.Files = append(prog.Files, g)
				missing = append(missing, loadImports(prog, units, seen, pk, g, newRoot)...)
			} else {
				seen[facade] = true
			}
			continue
		}
		// 这一句不去盘上找：不是 ./ 或 ../ 开头的路径，检查那一侧会把它说清楚，
		// 这里再画一条"读不到"只会盖掉那句更有用的话。
		if !check.IsLocalImportPath(imp.Path) {
			continue
		}
		target := check.ResolveImportPath(f.Path, imp.Path)
		if seen[target] {
			continue
		}
		seen[target] = true
		bad := plain{line: imp.PathPos.Line, col: imp.PathPos.Col, msg: "读不到 " + imp.Path + " 这个文件"}

		g, u, ok := parseFile(pk, target)
		if !ok {
			missing = append(missing, bad)
			continue
		}
		// 包里的文件引包内另一片才算这个包的东西；伸到包外面去的不能登记成这个包的，
		// 否则"包里的文件只能引同一个包里的文件"这一条在编辑器里画不出来。
		next := root
		if root != "" && !strings.HasPrefix(target, root+"/") {
			next = ""
		}
		if next != "" {
			prog.PkgRoots[target] = next
		}
		units[target] = u
		prog.Files = append(prog.Files, g)
		missing = append(missing, loadImports(prog, units, seen, pk, g, root)...)
	}
	return missing
}

// parseFile 读一份源文件并走完词法与语法。读不到、过不去都返回 ok 为假：
// 编辑器这一侧只想知道"这一句 import 有没有东西可画"。
//
// en: parseFile reads one source file and runs lex and parse. An unreadable file or a failure
// returns a false ok: the editor side only wants to know whether this import line has anything
// to draw.
func parseFile(pk *pkgload.Loader, path string) (*ast.File, *unit, bool) {
	src, ok := pk.ReadSource(path)
	if !ok {
		return nil, nil, false
	}
	toks, lerr := lexer.Lex(src)
	if lerr != nil {
		return nil, nil, false
	}
	g, perr := parser.Parse(path, toks)
	if perr != nil {
		return nil, nil, false
	}
	lines := splitLines(src)
	return g, &unit{lines: lines, widths: tokenWidths(lines, toks)}, true
}

// tokenWidths 记下每个记号在源码里占几个字节。文本与字符字面量的 Text 是解码后的内容，
// 与源码那一侧的长度不等，所以那两样要照着源码重新数一遍引号。
//
// en: tokenWidths records how many bytes each token occupies in the source. The Text of a
// string and of a character literal is the decoded content, whose length differs from the
// source form, so those two kinds are re-counted against the source including the quotes.
func tokenWidths(lines []string, toks []lexer.Tok) map[[2]int]int {
	w := map[[2]int]int{}
	for _, t := range toks {
		switch t.Kind {
		case lexer.NL, lexer.EOF:
			w[[2]int{t.Line, t.Col}] = 0
		case lexer.Num:
			w[[2]int{t.Line, t.Col}] = len(t.Digits)
		case lexer.Str, lexer.Char:
			w[[2]int{t.Line, t.Col}] = quotedLen(lineAt(lines, t.Line), t.Col-1)
		default:
			w[[2]int{t.Line, t.Col}] = len(t.Text)
		}
	}
	return w
}

// quotedLen 从 line 的第 from 个字节（那应当是一个引号）数到配对的引号，两头都算进去；
// 数不出来就算到行尾。
//
// en: quotedLen counts from byte `from` of line (which should be a quote) to the matching
// quote, counting both ends; if no match is found it counts to the end of the line.
func quotedLen(line string, from int) int {
	if from < 0 || from >= len(line) {
		return 0
	}
	quote := line[from]
	for i := from + 1; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == quote {
			return i - from + 1
		}
	}
	return len(line) - from
}

func lineAt(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}

// splitLines 按词法那一侧的行切法切：换行结束一行，行尾那个 \r 不算进列。
//
// en: splitLines splits the way the lexer does: a line feed ends a line and the trailing \r
// does not count into the column.
func splitLines(text string) []string {
	var out []string
	for {
		i := indexByte(text, '\n')
		if i < 0 {
			return append(out, trimCR(text))
		}
		out = append(out, trimCR(text[:i]))
		text = text[i+1:]
	}
}

func trimCR(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

// prevRuneStart 返回 pos 之前那一个字符的起始字节；pos 是 0 起的字节位，且大于 0。
//
// en: prevRuneStart returns the starting byte of the character before pos, where pos is a
// 0-based byte offset greater than 0.
func prevRuneStart(s string, pos int) int {
	_, size := utf8.DecodeLastRuneInString(s[:pos])
	return pos - size
}

// errPos、errCol、errText 从词法与语法那两种错误里取行列与原文；
// 取不到就退回第一行第一列，把错误对象的话原样当报错文字。
//
// en: errPos, errCol and errText pull line, column and the original text out of the two kinds
// of lexer and parser errors; when they cannot, the fallback is line 1 column 1 with the error
// object's message as the reported text.
func errPos(err error) int {
	var le *lexer.Error
	if errors.As(err, &le) {
		return le.Line
	}
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe.Line
	}
	return 1
}

func errCol(err error) int {
	var le *lexer.Error
	if errors.As(err, &le) {
		return le.Col
	}
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe.Col
	}
	return 1
}

func errText(err error) string {
	var le *lexer.Error
	if errors.As(err, &le) {
		return le.Msg
	}
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe.Msg
	}
	return err.Error()
}

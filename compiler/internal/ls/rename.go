// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// rename.go 是"重命名"那一条：把光标处那一个名字写过的每一处一起换成新名字。
// 找哪几处与"找引用"走同一份收集（references.go），这一份只做三件事：拦改不了的情形、
// 把每一处排成编辑器认的改动、改完先在内存里编译一遍，编译不过就不返回改动。
//
// en: rename.go is the rename request: every place the name under the cursor is written changes
// to the new name together. Which places count comes from the same collection as find-references
// (references.go); this file does three things: refuse what cannot be renamed, lay each site out
// as an edit the editor understands, and compile the result in memory first - nothing is handed
// over if it does not compile.
package ls

import (
	"fmt"
	"sort"
	"strings"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/parser"
	"neflang.org/nf/internal/pkgload"
)

// PrepareRename 回答"这个名字能不能改"：能改就返回光标处那一个名字的范围，编辑器拿它做
// 改名的输入框；不能改（标准库、内建、认不出来）返回 nil。
//
// en: PrepareRename answers whether this name can be renamed: if it can, the range of the name
// under the cursor is returned for the editor's input box; if it cannot (standard library,
// built-in, unrecognised), nil.
func PrepareRename(path, text string, at Position) *Range {
	if _, _, _, ok := symbolUnderCursor(path, text, at); !ok {
		return nil
	}
	e, ok := edgeOf(text, at)
	if !ok {
		return nil
	}
	line := lineOf(text, at.Line)
	col := byteColOf(line, at.Character)
	to := col
	for to < len(line) && isNameByte(line[to]) {
		to++
	}
	if e.word >= to {
		return nil
	}
	return &Range{
		Start: Position{Line: at.Line, Character: utf16Len(line[:e.word])},
		End:   Position{Line: at.Line, Character: utf16Len(line[:to])},
	}
}

// Rename 把光标处那一个名字改成 newName。返回不了改动时第二个返回值是一句说明原因的话：
// 名字不合法、写在只读的包容器里、或者改完编译不过。程序本来就编译不过时不做改后校验
// （没有干净的基准可比），只拦前两种。
//
// en: Rename changes the name under the cursor to newName. When no edit can be handed over the
// second return value says why: an invalid name, one written in a read-only package container,
// or a result that does not compile. A program that already does not compile gets no after-check
// (there is no clean baseline to compare against); only the first two refusals apply.
func Rename(path, text string, at Position, newName string) (*WorkspaceEdit, string) {
	sym, prog, facts, ok := symbolUnderCursor(path, text, at)
	if !ok {
		return nil, "这个名字没有写在源码里的声明，改不了"
	}
	if newName == sym.name {
		return &WorkspaceEdit{Changes: map[string][]TextEdit{}}, ""
	}
	if msg := checkNewName(newName); msg != "" {
		return nil, msg
	}
	sites := collectSites(prog, facts, path, text, sym, true)
	if len(sites) == 0 {
		return nil, "这个名字没有写在源码里的声明，改不了"
	}
	for _, site := range sites {
		if strings.Contains(site.file, ".nfpack/") {
			return nil, "这个名字写在包容器里，容器是只读的：把包换成源文件目录那种再改"
		}
	}

	// 每一处排成一条改动。手上这一份用编辑器推过来的文本，别的那几份从盘上读。
	texts := map[string]string{path: text}
	changes := map[string][]TextEdit{}
	for _, site := range sites {
		src, have := texts[site.file]
		if !have {
			s, ok := pkgload.New().ReadSource(site.file)
			if !ok {
				return nil, "读不到 " + site.file + "，没有改"
			}
			texts[site.file] = s
			src = s
		}
		lines := splitLines(src)
		if site.line < 1 || site.line > len(lines) {
			continue
		}
		line := lines[site.line-1]
		if site.col < 1 || site.col-1 > len(line) {
			continue
		}
		start := utf16Len(line[:site.col-1])
		uri := uriFromPath(site.file)
		changes[uri] = append(changes[uri], TextEdit{
			Range: Range{
				Start: Position{Line: site.line - 1, Character: start},
				End:   Position{Line: site.line - 1, Character: start + utf16Len(site.name)},
			},
			NewText: newName,
		})
	}
	if msg := renameCompiles(prog, path, text, texts, sites, newName); msg != "" {
		return nil, msg
	}
	for _, edits := range changes {
		sort.Slice(edits, func(i, j int) bool {
			if edits[i].Range.Start.Line != edits[j].Range.Start.Line {
				return edits[i].Range.Start.Line < edits[j].Range.Start.Line
			}
			return edits[i].Range.Start.Character < edits[j].Range.Start.Character
		})
	}
	return &WorkspaceEdit{Changes: changes}, ""
}

// checkNewName 拦不合法的新名字：形状、关键字与内建名字三关。重名与改完编译不过由
// 改后校验拦住，不在这里另写一套作用域规则。
//
// en: checkNewName rejects an invalid new name at three gates: shape, keywords, built-in names.
// Clashes and other compile breakage are caught by the after-check, not by a second set of
// scope rules here.
func checkNewName(name string) string {
	if name == "" {
		return "新名字是空的"
	}
	for i := 0; i < len(name); i++ {
		if !isNameByte(name[i]) || (i == 0 && name[i] >= '0' && name[i] <= '9') {
			return fmt.Sprintf("%s 不是合法的名字：只能用字母、数字、下划线与 $，开头不能是数字", name)
		}
	}
	if lexer.Keywords[name] {
		return name + " 是关键字，不能拿去做名字"
	}
	if lexer.Reserved[name] {
		return name + " 是内建名字，不能拿去做名字"
	}
	return ""
}

// renameCompiles 在内存里把改动落下去再编译一遍：原来编译得过而改完编译不过，就不返回改动。
// 接口的方法改名时实现它的类要一起改（找引用那一家已经带上）；漏了哪一处、与哪一个名字重名，
// 都由这一步改后校验现形。
//
// en: renameCompiles applies the edits in memory and compiles again: if the program compiled
// before and does not after, nothing is handed over. Renaming an interface method already
// carries the implementing classes (the find-references family); this is the safety net - a
// missed site or a clashed name shows up here.
func renameCompiles(prog *check.Program, entry, text string, texts map[string]string, sites []refSite, newName string) string {
	if errs, _ := check.RunWithFacts(prog, false); len(errs) > 0 {
		return "" // 本来就编译不过：没有干净的基准，改后校验不做
	}
	edits := map[string][]refSite{}
	for _, site := range sites {
		edits[site.file] = append(edits[site.file], site)
	}
	next := &check.Program{
		Entry: prog.Entry, PkgRefs: prog.PkgRefs, PkgRoots: prog.PkgRoots,
		Facades: prog.Facades, PkgFacades: prog.PkgFacades,
	}
	for _, f := range prog.Files {
		src := texts[f.Path]
		if src == "" && f.Path != entry {
			s, ok := pkgload.New().ReadSource(f.Path)
			if !ok {
				return "读不到 " + f.Path + "，没有改"
			}
			src = s
		}
		if f.Path == entry {
			src = text
		}
		list := edits[f.Path]
		if len(list) == 0 {
			next.Files = append(next.Files, f)
			continue
		}
		renamed := applySiteEdits(src, list, newName)
		toks, err := lexer.Lex(renamed)
		if err != nil {
			return "改成 " + newName + "之后程序编译不过，没有改"
		}
		nf, err := parser.Parse(f.Path, toks)
		if err != nil {
			return "改成 " + newName + "之后程序编译不过，没有改"
		}
		next.Files = append(next.Files, nf)
	}
	if errs, _ := check.RunWithFacts(next, false); len(errs) > 0 {
		return "改成 " + newName + " 之后程序编译不过：" + errs[0].Error()
	}
	return ""
}

// applySiteEdits 把一处文件里的那几处名字换成新名字：按字节位置从后往前换，前面的位置不动。
//
// en: applySiteEdits replaces the name sites in one file with the new name: splicing back to
// front by byte offset keeps the earlier positions still.
func applySiteEdits(src string, sites []refSite, newName string) string {
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].line != sites[j].line {
			return sites[i].line > sites[j].line
		}
		return sites[i].col > sites[j].col
	})
	for _, site := range sites {
		off := lineOffset(src, site.line-1) + site.col - 1
		if off < 0 || off+len(site.name) > len(src) {
			continue
		}
		src = src[:off] + newName + src[off+len(site.name):]
	}
	return src
}
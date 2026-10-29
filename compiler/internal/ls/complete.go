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

// complete.go 是编辑器里那两件事：打到一个名字后面时能补出什么，鼠标停在一个名字上看到什么。
//
// 一条规矩：这里不另算一遍语言规则。能点出来的名字全部问 `internal/check`——容器与文本身上
// 有哪几条写在 `stdMember` 与 `stdFuncs` 那两张表里，自己写的类、枚举与包问检查这一趟返回的
// 那几张表（`Facts.Sigs`、`Facts.Declared`、`Facts.Fields`、`Facts.Packages`）。
// 补全与波浪线依据同一套检查结果，不会补出一个编译不过的名字。
//
// 这里有一处无法回避的情况：光标那一句是写到一半的，`page.` 直接交给检查器，语法那一侧当它没写完。
// 所以在光标处插一个占位名再走一遍前端——插在后面，它前面那些名字的行列一个都不动，
// 而检查器要把点号左边那一段算出类型，必须走到那一段。占位名只为这一次问答存在，
// 编辑器看到的文本始终是原来那一份。
//
// en: complete.go covers the two things an editor asks for: what can be completed after a name
// being typed, and what the mouse sees when it rests on a name. One rule: this file does not
// recompute language rules. Every name it can offer is asked of internal/check - which members
// a container or a piece of text has is written in the stdMember and stdFuncs tables, and
// hand-written classes, enums and packages are read from the tables the checking pass returns
// (Facts.Sigs, Facts.Declared, Facts.Fields, Facts.Packages). Completion and squiggles
// therefore read the same check results, so completion can never offer a name that fails to
// compile. One case cannot be avoided: the line under the cursor is half-written, and handing
// `page.` straight to the checker makes the parser treat it as incomplete. So a placeholder name
// is inserted at the cursor and the front end runs again - inserted AFTER the cursor, none of
// the names before it change line or column, and the checker has to reach the left side of the
// dot in order to type it. The placeholder exists only for this one round trip; the text the
// editor sees is always the original.
package ls

import (
	"sort"
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
)

// probe 是插进光标处的那一个占位名。语言里的名字都不是这个样子，无需处理撞名的情况。
//
// en: probe is the placeholder name inserted at the cursor. No name in the language looks like
// this, so a name collision needs no handling.
const probe = "nfEditorProbe"

// 补全条目右边那一个图标，用协议里的 CompletionItemKind 那几个数。
//
// en: The icon shown to the right of a completion entry, using the CompletionItemKind numbers
// from the protocol.
const (
	kindMethod = 2
	kindFunc   = 3
	kindField  = 5
	kindVar    = 6
	kindClass  = 7
	kindIface  = 8
	kindModule = 9
	kindEnum   = 13
	kindKeyw   = 14
)

// Completion 返回光标那个位置能补出来的东西。已经打出来的那一段不在这里筛：编辑器自己按前缀筛，
// 少一处筛法就少一处与编辑器不一样的可能。
//
// en: Completion returns everything that can be completed at the cursor position. Filtering
// against what has already been typed does not happen here: the editor filters by prefix
// itself, and one less filter is one less way to disagree with the editor.
func Completion(path, text string, at Position) []CompletionItem {
	edge, ok := edgeOf(text, at)
	if !ok {
		return nil
	}
	if edge.dot >= 0 {
		return memberCompletion(path, text, at, edge)
	}
	return nameCompletion(path, text)
}

// hoverEdge 是光标那一个名字的位置与它前面那一个点号（没有点号是 -1）。
//
// en: hoverEdge is the position of the name under the cursor plus the dot before it (-1 when
// there is no dot).
type hoverEdge struct {
	// word 是光标处那一个完整名字的起点（0 起的字节位）。
	word int
	// dot 是紧挨着这个名字前面那一个点号的字节位；没有点号是 -1。
	dot int
	// recv 是点号左边那一个名字（`wt.begin_run(` 里点 `begin_run` 时 recv 是 `wt`）。
	recv string
	// recvCol 是 recv 在这一行里的字节起点（0 起）：拿它去检查那一趟的结论表里问类型。
	recvCol int
}

// edgeOf 找光标那个位置的上下文。行号越界返回 ok 为假。
//
// en: edgeOf finds the context of the cursor position. An out-of-range line number returns a
// false ok.
func edgeOf(text string, at Position) (hoverEdge, bool) {
	lines := splitLines(text)
	if at.Line < 0 || at.Line >= len(lines) {
		return hoverEdge{}, false
	}
	line := lines[at.Line]
	col := byteColOf(line, at.Character)
	if col > len(line) {
		col = len(line)
	}
	from := col
	for from > 0 && isNameByte(line[from-1]) {
		from--
	}
	e := hoverEdge{word: from, dot: -1}
	if from > 0 && line[from-1] == '.' {
		e.dot = from - 1
		end := e.dot
		start := end
		for start > 0 && isNameByte(line[start-1]) {
			start--
		}
		e.recv = line[start:end]
		e.recvCol = start
	}
	return e, true
}

// memberCompletion 补点号后面那一段。
//
// en: memberCompletion completes what follows a dot.
func memberCompletion(path, text string, at Position, e hoverEdge) []CompletionItem {
	if e.recv == "" {
		return nil
	}
	patched := insertAtLine(text, at.Line, byteColOf(lineOf(text, at.Line), at.Character), probe)
	prog, facts := inspect(path, patched)
	if facts == nil {
		return nil
	}
	var list []check.Member
	kind := kindMethod
	prefix := "" // 那一条动作的定义节点在这张表里的键前缀；内建的那一些没有定义节点
	shape := check.Type{}
	if root, isPkg := check.ReceiverIsPackage(path, e.recv, facts); isPkg {
		kind = kindFunc
		prefix = root + "/"
		list = check.PackageMembers(root, facts)
	} else {
		shape = receiverType(facts, at.Line+1, e.recvCol+1, e.recv)
		if check.IsGroup(e.recv) {
			kind = kindFunc
		} else {
			prefix = shape.Name + "."
		}
		if shape.Kind == check.Enum {
			kind = kindEnum
		}
		list = check.MembersOn(e.recv, shape, facts)
	}
	out := make([]CompletionItem, 0, len(list))
	for _, m := range list {
		k := kind
		if m.Field && kind != kindEnum {
			k = kindField
		}
		doc := ""
		if fn := facts.Funcs[prefix+m.Name]; fn != nil {
			doc = docFor(prog, path, text, fn) // 查的是编辑器那一份文本：占位名插在光标处，声明在它上面，行列没动
		} else if m.Doc != nil {
			// 包交出来的数：注释写在包那份文件的声明上面，节点由 check 那一张表带过来。
			doc = docFor(prog, path, text, m.Doc)
		} else {
			// 内建的那一条没有定义节点：中文、英文与示例在 check 那张说明表里。
			doc = renderDoc(check.StdDocFor(e.recv, shape, m.Name))
		}
		out = append(out, CompletionItem{Label: m.Name, Detail: m.Detail, Kind: k, Documentation: doc})
	}
	return out
}

// nameCompletion 补一个还没打完整的名字：这一份文件里写过的名字、引进来的名字、
// 这一趟里认识的类型名、标准库的组名，以及关键字与内建名。
//
// en: nameCompletion completes a name that is not fully typed yet: names written in this file,
// names imported, type names known to this pass, standard-library group names, plus keywords
// and built-in names.
func nameCompletion(path, text string) []CompletionItem {
	prog, facts := inspect(path, text)
	byName := map[string]CompletionItem{}
	add := func(name, detail string, kind int, doc ...string) {
		if name == "" {
			return
		}
		old, seen := byName[name]
		if seen && (old.Detail != "" || detail == "") {
			return // 已经有一条带说明的：后来那条空的顶不掉
		}
		extra := ""
		if len(doc) > 0 {
			extra = doc[0]
		}
		byName[name] = CompletionItem{Label: name, Detail: detail, Kind: kind, Documentation: extra}
	}
	if facts != nil {
		// 先登有出处的三类：引进来的名字（它从哪一份文件来）、函数与方法的签名、认识的类型名。
		for key, nm := range facts.Imported {
			file, local, ok := splitNameKey(key)
			if !ok || file != path {
				continue
			}
			add(local, importedDetail(nm, facts), importedKind(nm.Kind), docFor(prog, path, text, nm.Decl))
		}
		// 包的挂名（`import tr from "libs/web_test"` 里的 tr）：它不是函数也不是类型，
		// 右边那一栏写的是这句 import 里那一条路径。不要把装载算出的绝对路径放进这一栏——
		// 测试里的包落在临时目录，绝对路径太长，一行放不下。
		for pkgName, impPath := range importPaths(prog, path) {
			add(pkgName, "包 "+impPath, kindModule)
		}
		for key, sg := range facts.Sigs {
			kind := kindFunc
			if sg.Method {
				kind = kindMethod
			}
			doc := ""
			if fn := facts.Funcs[key]; fn != nil {
				doc = docFor(prog, path, text, fn)
			}
			add(check.SealedName(key), facts.Signature(key), kind, doc)
		}
		for key, d := range facts.Declared {
			name := check.SealedName(key)
			add(name, check.Declaration(name, d), importedKindOfDeclared(d.Kind), docFor(prog, path, text, declNode(prog, name)))
		}
		// 最后才是这一份文件里出现过的名字：它们与已经打出来的那一段对得上，但只有类型、没有出处。
		// 包里的与别的文件的东西只能经 import 引用到，这里不替检查器预筛——补出来的名字
		// 无法解析时，波浪线会当场说清。
		for _, tok := range nameTokens(text) {
			d := ""
			if b, ok := bindAt(facts, tok.Line, tok.Col); ok {
				d = bindLine(tok.Text, b)
			}
			add(tok.Text, d, kindVar)
		}
	}
	// 组名先登：它们也在 `lexer.Reserved` 那一张表里，晚一步就只剩"内建名字"这一栏，
	// 而"这是一个标准库的组"比"这是一个内建名字"更有用。
	for _, g := range check.GroupNames() {
		add(g, "标准库组", kindModule)
	}
	for name := range lexer.Keywords {
		add(name, "关键字", kindKeyw)
	}
	for name := range lexer.Reserved {
		add(name, "内建名字", kindKeyw)
	}
	out := make([]CompletionItem, 0, len(byName))
	for _, it := range byName {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// Hover 返回光标那个位置上的那一句说明。认不出来返回 nil：宁可什么都不显示，
// 也不显示一句从别处错认来的话。
//
// en: Hover returns the sentence shown at the cursor position. When nothing is recognised it
// returns nil: showing nothing is better than showing a sentence misread from somewhere else.
func Hover(path, text string, at Position) *HoverResult {
	e, ok := edgeOf(text, at)
	if !ok {
		return nil
	}
	lines := splitLines(text)
	line := lines[at.Line]
	col := byteColOf(line, at.Character)
	to := col
	for to < len(line) && isNameByte(line[to]) {
		to++
	}
	name := line[e.word:to]
	if name == "" {
		return nil
	}
	prog, facts := inspect(path, text)
	if facts == nil {
		return nil
	}
	ln, bc := at.Line+1, e.word+1
	// 点号左边那一段的类型：挂在形状身上的成员（`xs.add(3)`）要靠它把元素类型落成真那一个，
	// 不然签名只能写成 `list.add(value: T)`。
	var recvT check.Type
	if e.dot >= 0 && e.recv != "" {
		recvT = receiverType(facts, ln, e.recvCol+1, e.recv)
	}
	doc := func(node any) string { return docFor(prog, path, text, node) }
	if sig, node, stdDoc := callHover(facts, recvT, ln, bc, name); sig != "" {
		return markHover(sig, firstOf(stdDoc, doc(node)))
	}
	if e.dot >= 0 {
		if sig, node, stdDoc := memberHover(path, facts, ln, e.recvCol+1, e.recv, name); sig != "" {
			return markHover(sig, firstOf(stdDoc, doc(node)))
		}
	}
	if b, ok := bindAt(facts, ln, bc); ok {
		return markHover(bindLine(name, b), doc(b.Decl))
	}
	for key, d := range facts.Declared {
		if check.SealedName(key) == name {
			return markHover(facts.DeclaredHover(key, d), doc(declNode(prog, name)))
		}
	}
	for key, nm := range facts.Imported {
		if file, local, isOk := splitNameKey(key); isOk && local == name && file == path {
			return markHover(importedDetail(nm, facts), doc(nm.Decl))
		}
	}
	if check.IsGroup(name) {
		sig, body := groupHover(name)
		return markHover(sig, body)
	}
	return nil
}

// groupHover 交回鼠标停在标准库的组名上那两行：组名本身，加上它下面那一些动作的签名。
// 组名不是一个声明，语言里没有写它的那一行，凑不出一行签名——列出它有什么东西才是有用答复。
//
// en: groupHover answers the two lines shown when the mouse rests on a standard-library group
// name: the group itself plus the signatures of the actions under it. A group name is not a
// declaration - the language has no line that writes it, so no signature can be assembled;
// listing what it contains is the useful answer.
func groupHover(name string) (string, string) {
	var parts []string
	for _, m := range check.MembersOn(name, check.Type{}, nil) {
		parts = append(parts, "`"+name+"."+m.Detail+"`")
	}
	zh, en := check.GroupDoc(name)
	var body []string
	if zh != "" {
		body = append(body, zh)
	}
	if en != "" {
		body = append(body, en)
	}
	if len(parts) > 0 {
		body = append(body, "里面那一些动作："+strings.Join(parts, "、"))
	}
	if len(body) == 0 {
		return name, ""
	}
	return name, strings.Join(body, "\n\n")
}

// importPaths 交出这一份文件里每一句整块挂名的 import：挂名 → 写出来的那一段路径。
// 补全里"包 …"那一栏要用它，因为检查器记的包根是装载算出来的绝对路径，那一行给人看太长了。
//
// en: importPaths returns every whole-module import in this file: the alias -> the path as
// written. The completion column "package ..." needs it, because the package root the checker
// records is the absolute path computed while loading, which is far too long for one line of
// screen.
func importPaths(prog *check.Program, path string) map[string]string {
	out := map[string]string{}
	if prog == nil {
		return out
	}
	for _, f := range prog.Files {
		if f.Path != path {
			continue
		}
		for _, imp := range f.Imports {
			if imp.Package != "" {
				out[imp.Package] = imp.Path
			}
		}
	}
	return out
}

// declNode 按名字找那一个顶层定义的节点：要找的注释写在定义上面，定义节点才带行列。
//
// en: declNode finds the node of a given top-level definition by name: the comment being looked
// for is written above the definition, and only the definition node carries line and column.
func declNode(prog *check.Program, name string) any {
	if prog == nil {
		return nil
	}
	for _, f := range prog.Files {
		for _, d := range f.Defs {
			if d.Name() == name {
				return d
			}
		}
	}
	return nil
}

// callHover 认名字正好是一个调用点上被叫的那一个：`console.print(...)`、`p.open(...)`、
// `twice(...)` 三种写法都在这一条里问出来——检查器把每一次调用记在 Facts.Calls 上，
// 键上被叫名字的位置正好落在光标处即视为命中。
//
// en: callHover recognises the case where the name is exactly the callee of a call site:
// `console.print(...)`, `p.open(...)`, `twice(...)` - all three spellings are answered here,
// because the checker records every call in Facts.Calls and the name position on the key
// landing right at the cursor is a match.
func callHover(facts *check.Facts, recv check.Type, line, col int, name string) (string, any, string) {
	for call, ref := range facts.Calls {
		at := ast.Pos{}
		switch n := call.Callee.(type) {
		case *ast.Name:
			at = n.Pos
		case *ast.Member:
			at = n.Pos
		default:
			continue
		}
		if at.Line != line || at.Col != col {
			continue
		}
		if ref.Std != "" {
			// 挂在形状身上的那一条（`xs.add(3)`）先按这一只容器自己算：元素类型要落成具体的
			// 那一个类型，不该写成 T。挂在组名上的（`console.print`、`file.read`）只有一种落点名。
			if s := check.MemberSignature(recv, afterDot(ref.Std)); s != "" {
				return s, nil, renderDoc(check.StdDocFor("", recv, afterDot(ref.Std)))
			}
			if s := check.StdSignature(ref.Std); s != "" {
				return s, nil, renderDoc(check.StdDocRaw(ref.Std))
			}
		}
		if ref.Func != "" {
			if s := facts.Signature(ref.Func); s != "" {
				return s, facts.Funcs[ref.Func], ""
			}
		}
	}
	return "", nil, ""
}

// memberHover 认点号后面那一个名字：先问左边那一段是什么，再问那一样身上这一条怎么签名。
// 第二个答复是那一条动作的定义节点，悬停拿它去找写在上面的注释；内建的那一些没有定义节点，交回 nil。
//
// en: memberHover handles the name after a dot: first ask what the left side is, then ask how
// this member is signed on that thing. The second answer is the definition node of that action,
// which hover uses to find the comment written above it; built-ins have no definition node and
// return nil.
func memberHover(path string, facts *check.Facts, line, recvCol int, recv, name string) (string, any, string) {
	if root, isPkg := check.ReceiverIsPackage(path, recv, facts); isPkg {
		for _, m := range check.PackageMembers(root, facts) {
			if m.Name == name {
				if fn := facts.Funcs[root+"/"+name]; fn != nil {
					return recv + "." + m.Detail, fn, ""
				}
				// 包交出来的数：定义节点在 check 那一张表上带着，注释写在它上面。
				return recv + "." + m.Detail, m.Doc, ""
			}
		}
		return "", nil, ""
	}
	t := receiverType(facts, line, recvCol, recv)
	for _, m := range check.MembersOn(recv, t, facts) {
		if m.Name == name {
			// 自己写的类身上那条有定义节点，注释写在它上面；内建的那一些在说明表里。
			if fn := facts.Funcs[t.Name+"."+name]; fn != nil {
				return recv + "." + m.Detail, fn, ""
			}
			return recv + "." + m.Detail, nil, renderDoc(check.StdDocFor(recv, t, name))
		}
	}
	return "", nil, ""
}

// typeAtLineCol 问写在这一行这一列的那一段是什么类型：先问名字绑在哪儿（那份答复更准，
// 顶层 `let page: Page` 的 `page` 就在那一侧），再退到表达式类型表。
//
// en: typeAtLineCol asks what type is written at this line and column: first ask where the name
// is bound (that answer is more accurate - the `page` of a top-level `let page: Page` lives on
// that side), then fall back to the expression type table.
func typeAtLineCol(facts *check.Facts, line, col int) check.Type {
	if b, ok := bindAt(facts, line, col); ok {
		return b.Typ
	}
	for e, t := range facts.Types {
		if p := e.Where(); p.Line == line && p.Col == col {
			return t
		}
	}
	return check.Type{}
}

// receiverType 问点号左边那一段是什么。三条路按准头排：那一段是一个值（变量、参数、字段、
// 一次调用的结果）就按位置问检查器算出来的类型；是一个类型名（`Kind.add`、`Page.new` 那种写法）
// 就照类型表认；都不是就是零值——补全那一侧会退回什么都不列，不猜。
//
// en: receiverType asks what the left side of a dot is. The three routes are ordered by
// accuracy: if it is a value (variable, parameter, field, the result of a call), ask the
// checker for the type computed at that position; if it is a type name (the `Kind.add`,
// `Page.new` spelling), read the type table; if neither, return the zero value - completion
// then lists nothing instead of guessing.
func receiverType(facts *check.Facts, line, col int, name string) check.Type {
	if t := typeAtLineCol(facts, line, col); t.Kind != 0 {
		return t
	}
	for key, d := range facts.Declared {
		if check.SealedName(key) != name {
			continue
		}
		switch d.Kind {
		case "enum":
			return check.Type{Kind: check.Enum, Name: key}
		case "class":
			return check.Type{Kind: check.Class, Name: key}
		case "struct":
			return check.Type{Kind: check.Struct, Name: key}
		case "interface":
			return check.Type{Kind: check.Iface, Name: key}
		}
	}
	return check.Type{}
}

func bindAt(facts *check.Facts, line, col int) (check.Bind, bool) {
	for n, b := range facts.Binds {
		if p := n.Where(); p.Line == line && p.Col == col {
			return b, true
		}
	}
	return check.Bind{}, false
}

// bindLine 把一个名字的所在写成一行人看得懂的话：`let page: Page`、`n: int`、`const limit: int`。
// 照编程语言的写法来，不写"顶层"、"参数"这种身份说明：那一行越像一个声明，越不需要人再解释一句。
// 参数与字段只写"名字: 类型"——它们是哪一个身份，从这一行在屏幕上的位置与函数签名里就看得出来。
//
// en: bindLine writes where a name comes from as a line a person recognises: `let page: Page`,
// `n: int`, `const limit: int`. It follows the programming-language spelling and does not add
// identity labels such as "top level" or "parameter": the more the line looks like a
// declaration, the less it needs explaining. Parameters and fields are written as "name: type"
// only - which identity they are is already visible from where the line sits on screen and from
// the function signature.
func bindLine(name string, b check.Bind) string {
	switch b.By {
	case check.BindParam, check.BindField:
		return name + ": " + b.Typ.String()
	case check.BindThis:
		return "this: " + b.Typ.String()
	case check.BindGlobalConst:
		return "const " + name + ": " + b.Typ.String()
	case check.BindShared:
		return "shared " + name + ": " + b.Typ.String()
	case check.BindArgs:
		return "args: " + b.Typ.String()
	}
	return "let " + name + ": " + b.Typ.String()
}

// declarationOf 把一个引进来的值写成一行声明：`const limit: int`、`shared counter: int`。
//
// en: declarationOf writes an imported value as one declaration line: `const limit: int`,
// `shared counter: int`.
func declarationOf(nm check.ImportedName) string {
	head := "let"
	switch nm.By {
	case check.BindGlobalConst:
		head = "const"
	case check.BindShared:
		head = "shared"
	}
	return head + " " + nm.Name + ": " + nm.Val.String()
}

func importedDetail(nm check.ImportedName, f *check.Facts) string {
	switch nm.Kind {
	case check.FuncT:
		if nm.FuncNm == "" {
			if nm.Val.Kind != 0 {
				return declarationOf(nm)
			}
			return ""
		}
		// 签名表上的键是"包根/名字"或者光一个名字（工程里的文件没有包根），而引进来的那一条
		// 记的是它从哪一份文件来。先按定义它的那个包根找，找不到再认那一个唯一同名的。
		if sg := sigFrom(f, nm.FuncNm, nm.From); sg != "" {
			return sg
		}
		return nm.FuncNm
	case check.Class, check.Struct, check.Iface, check.Enum, check.Alias:
		if d, ok := declaredFor(f, nm.TypeNm, nm.From); ok {
			return check.Declaration(nm.TypeNm, d)
		}
		return kindWord(nm.Kind) + " " + nm.TypeNm
	}
	if nm.Val.Kind != 0 {
		return declarationOf(nm)
	}
	return "从 " + nm.From + " 引进来"
}

// sigFrom 与 declaredFor 在两张表里找引进来的那一条。键有两种写法：包里的写作"包根/名字"，
// 工程里的文件只写名字，所以拿到 nm.From（那一份文件的完整路径）时先试包根那一种；
// 两边都叫这个名字的东西不止一条时不猜，宁可可露那一栏只有名字。
//
// en: sigFrom and declaredFor look up one imported item in the two tables. Keys have two
// spellings: inside a package it is "package root/name", in a project's own file just the name,
// so when nm.From (the full path of that file) is in hand the package-root form is tried first.
// When more than one thing under that name matches, nothing is guessed - it is better for the
// detail column to carry only the name.
func sigFrom(f *check.Facts, name, from string) string {
	if sg := f.Signature(name); sg != "" {
		return sg
	}
	var hit string
	n := 0
	for key := range f.Sigs {
		if check.SealedName(key) != name {
			continue
		}
		sg := f.Signature(key)
		if sg == "" {
			continue
		}
		if sameOwner(key, from) {
			return sg
		}
		hit, n = sg, n+1
	}
	if n == 1 {
		return hit
	}
	return ""
}

func declaredFor(f *check.Facts, name, from string) (check.Declared, bool) {
	if d, ok := f.Declared[name]; ok {
		return d, true
	}
	var hit check.Declared
	n := 0
	for key, d := range f.Declared {
		if check.SealedName(key) != name {
			continue
		}
		if sameOwner(key, from) {
			return d, true
		}
		hit, n = d, n+1
	}
	if n == 1 {
		return hit, true
	}
	return check.Declared{}, false
}

// sameOwner 问这条键是不是来自那一份文件：包根是文件路径的前一段就算对上。
//
// en: sameOwner asks whether this key comes from that file: the package root matching the
// beginning of the file path counts.
func sameOwner(key, from string) bool {
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return false
	}
	return strings.Contains(from, key[:i]+"/")
}

func importedKind(k check.Kind) int {
	switch k {
	case check.FuncT:
		return kindFunc
	case check.Iface:
		return kindIface
	case check.Enum:
		return kindEnum
	}
	return kindClass
}

func importedKindOfDeclared(kind string) int {
	switch kind {
	case "interface":
		return kindIface
	case "enum":
		return kindEnum
	}
	return kindClass
}

func kindWord(k check.Kind) string {
	switch k {
	case check.Class:
		return "class"
	case check.Struct:
		return "struct"
	case check.Iface:
		return "interface"
	case check.Enum:
		return "enum"
	case check.Alias:
		return "类型别名"
	}
	return "名字"
}

// nameTokens 返回这一份文本里所有像名字的记号。补全拿它当"这份文件里出现过的名字"的清单：
// 把语法树整个走一遍只为数名字不值，记号这一侧已经把它们都分好了。
//
// en: nameTokens returns every name-like token in this text. Completion uses it as "names that
// appear in this file": walking the whole syntax tree just to count names is not worth it when
// the token stream already separates them.
func nameTokens(text string) []lexer.Tok {
	toks, err := lexer.Lex(text)
	if err != nil {
		return nil
	}
	var out []lexer.Tok
	for _, t := range toks {
		if t.Kind == lexer.Name && t.Text != "" {
			out = append(out, t)
		}
	}
	return out
}

// insertAtLine 在第 line 行（0 起）的那一个字节位上插进一段文本；越界就贴到这一份文本的末尾。
//
// en: insertAtLine inserts text at the given byte column of line `line` (0-based); out of
// range, it is appended to the end of the text.
func insertAtLine(text string, line, col int, insert string) string {
	if line < 0 || line >= len(splitLines(text)) {
		return text + insert
	}
	off := lineOffset(text, line)
	if col < 0 {
		col = 0
	}
	if off+col > len(text) {
		return text[:len(text)] + insert
	}
	return text[:off+col] + insert + text[off+col:]
}

// lineOf 返回第 n 行（0 起）的文本；越界返回空行。
//
// en: lineOf returns the text of line n (0-based); out of range it returns an empty line.
func lineOf(text string, n int) string {
	lines := splitLines(text)
	if n < 0 || n >= len(lines) {
		return ""
	}
	return lines[n]
}

// lineOffset 是第 n 行（0 起）在这一份文本里的字节起点。
//
// en: lineOffset is the byte offset at which line n (0-based) starts in this text.
func lineOffset(text string, n int) int {
	off := 0
	for i := 0; i < n; i++ {
		j := strings.IndexByte(text[off:], '\n')
		if j < 0 {
			return len(text)
		}
		off += j + 1
	}
	return off
}

// byteColOf 把编辑器给的 UTF-16 列换成这一行里的字节位；越界就贴到行尾。
// 诊断那一路是反过来的换算（字节列 → UTF-16 列），这一条是它的那一半。
//
// en: byteColOf converts the UTF-16 column the editor sends into a byte column within that
// line; out of range it lands at the end of the line. The diagnostics path converts the other
// way (byte column -> UTF-16 column); this is its counterpart.
func byteColOf(line string, u16 int) int {
	if u16 <= 0 {
		return 0
	}
	n := 0
	for i, r := range line {
		if n >= u16 {
			return i
		}
		n += utf16Len(string(r))
	}
	return len(line)
}

// firstOf 交回第一个不是空串的答复：内建那一条的说明与声明上面的注释不会同时存在。
//
// en: firstOf returns the first non-empty answer: a built-in action's doc and a declaration's
// own comment are never both present.
func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func isNameByte(c byte) bool {
	return c == '_' || c == '$' ||
		c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// splitNameKey 把 check.NameKey 那种写法拆回（哪一份文件, 那一个名字）。
//
// en: splitNameKey unpacks a check.NameKey spelling back into (which file, which name).
func splitNameKey(key string) (file, name string, ok bool) {
	i := strings.IndexByte(key, '\x00')
	if i < 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

// markHover 拼悬停那一段 Markdown：第一格是那一行签名（代码块里，编辑器按代码高亮），
// 后面才是写在声明上面的那段注释（原样，不翻译也不重排）。
//
// en: markHover assembles the hover markdown: the first block is the signature line (inside a
// code fence, so the editor highlights it as code), and only after it comes the comment written
// above the declaration - verbatim, neither translated nor reflowed.
func markHover(sig, doc string) *HoverResult {
	v := "```nf\n" + sig + "\n```"
	if doc != "" {
		v += "\n\n" + doc
	}
	return &HoverResult{Contents: markup{Kind: "markdown", Value: v}}
}

// afterDot 取落点名里名字那一段：`list.add` → `add`。
//
// en: afterDot takes the member part of a dotted name: `list.add` -> `add`.
func afterDot(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

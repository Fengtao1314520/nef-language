// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// references.go 是"找引用"那一条：编辑器把光标停在一个名字上问"这个名字都在哪儿写过"。
// 认名字与认声明都读检查器留下的那几份结论（Facts），这一份不另定一套规则：
// 名字绑到哪儿问 Binds，调用落到哪儿问 Calls，点号后面是什么问 Members，
// 函数当值交出去问 FuncRefs。声明那一行上的名字按语法节点的位置认。
//
// 泛型的模板与实例算同一个符号：实例的节点是复制出来的，位置与模板相同，
// 规范名去掉尖括号那一段（check.CallableBase）之后与模板相同，两边都按这一条归并。
//
// en: references.go answers find-references: the editor rests the cursor on a name and asks
// everywhere that name is written. Recognising names and declarations reads the checker's own
// conclusions (Facts) instead of a second set of rules: Binds for what a name binds to, Calls for
// where a call lands, Members for what follows a dot, FuncRefs for a function handed over as a
// value. A name on a declaration line is recognised by the syntax node's position.
//
// A generic's template and its instances count as one symbol: an instance's nodes are copies
// carrying the template's positions, and its canonical name without the bracket section
// (check.CallableBase) equals the template's, so both sides merge under that rule.
package ls

import (
	"sort"
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/pkgload"
)

// 符号的种类：认出来的是哪一样东西，决定收集引用时比哪一张表、按什么比。
//
// en: The kind of symbol: which thing was recognised decides which table collection reads and
// what it compares.
const (
	symLocal      = iota // 局部量、参数、catch 的名字：同一份文件里按登记位置认
	symGlobal            // 顶层 let／const／shared 与包导出的数：按声明节点认，跨文件
	symFunc              // 顶层函数（含包导出的）：按规范名认
	symMethod            // 类与接口的方法：按所属类型加方法名认，base 链上同名的算一家
	symField             // 字段：点出来按所属类型认，方法体里直接写的按登记位置认
	symType              // class／struct／interface／enum／type 别名：按规范名认
	symEnumMember        // 枚举成员：按枚举规范名加成员名认
)

// symbol 是"光标处那一个名字指的是谁"的答复。file 与 at 只有按声明位置认的那两类用；
// key 是函数与类型的规范名；owner 是方法、字段与枚举成员所属的那一个类型。
//
// en: symbol answers which thing the name under the cursor refers to. file and at are used only
// by the two kinds matched by declaration position; key is the canonical name of a function or
// type; owner is the type a method, field or enum member belongs to.
type symbol struct {
	kind  int
	name  string
	file  string
	at    ast.Pos
	key   string
	owner string
	node  *ast.Decl // symGlobal：声明它的那个节点，跨文件的使用按指针认
}

// refSite 是一处引用：哪一份文件、名字那一个记号在哪（1 起的行列）、写出来那一截多长。
// 名字只含 ASCII（词法层拒了别的），长度按字节数就是按字符数。
//
// en: refSite is one reference: which file, where the name token sits (1-based line and column),
// and how long the written name is. Names are ASCII-only (the lexer rejects anything else), so
// the byte length is also the character count.
type refSite struct {
	file string
	line int
	col  int
	name string
}

// References 返回光标处那一个名字写过的每一处。includeDeclaration 为真时声明那一处也算。
// 认不出名字、或者那是标准库与内建的名字（没有写在源码里的声明），返回空。
//
// en: References returns everywhere the name under the cursor is written. includeDeclaration
// counts the declaration itself. An unrecognised name, or a standard-library or built-in name
// (no declaration written in source), returns nothing.
func References(path, text string, at Position, includeDecl bool) []Location {
	sym, prog, facts, ok := symbolUnderCursor(path, text, at)
	if !ok {
		return nil
	}
	sites := collectSites(prog, facts, path, text, sym, includeDecl)
	return sitesToLocations(sites, path, text)
}

// symbolUnderCursor 把光标处那一个名字认成一个符号，顺带返回这一次检查的程序与结论表。
//
// en: symbolUnderCursor recognises the name under the cursor as a symbol, also returning this
// run's program and conclusion tables.
func symbolUnderCursor(path, text string, at Position) (symbol, *check.Program, *check.Facts, bool) {
	e, ok := edgeOf(text, at)
	if !ok {
		return symbol{}, nil, nil, false
	}
	line := lineOf(text, at.Line)
	col := byteColOf(line, at.Character)
	to := col
	for to < len(line) && isNameByte(line[to]) {
		to++
	}
	name := line[e.word:to]
	if name == "" {
		return symbol{}, nil, nil, false
	}
	prog, facts := inspect(path, text)
	if facts == nil {
		return symbol{}, nil, nil, false
	}
	ln, bc := at.Line+1, e.word+1
	if sym, ok := declSymbolAt(prog, facts, path, text, ln, bc, name); ok {
		return sym, prog, facts, true
	}
	sym, ok := useSymbolAt(prog, facts, path, ln, bc, name)
	return sym, prog, facts, ok
}

// declSymbolAt 认声明那一行上的名字：光标正压在函数、方法、类型、字段、参数、局部量、
// catch 的名字、枚举成员或 import 引进来的那一个名字自己写出来的地方。只看光标所在那一份文件。
//
// en: declSymbolAt recognises a name on its declaration line: the cursor rests right on the
// written name of a function, method, type, field, parameter, local, catch name, enum member or
// imported name. Only the file the cursor is in counts.
func declSymbolAt(prog *check.Program, facts *check.Facts, path, text string, ln, bc int, name string) (symbol, bool) {
	var file *ast.File
	for _, f := range prog.Files {
		if f.Path == path {
			file = f
			break
		}
	}
	if file == nil {
		return symbol{}, false
	}
	s := newSource(text)
	root := prog.PkgRoots[path]
	nameAt := func(pos ast.Pos) bool {
		np, ok := namePosAfter(s.toks, pos)
		return ok && np.Line == ln && np.Col == bc
	}
	for _, imp := range file.Imports {
		for _, in := range imp.Names {
			if in.Pos.Line == ln && in.Pos.Col == bc {
				local := in.Alias
				if local == "" {
					local = in.Name
				}
				if nm, ok := facts.Imported[check.NameKey(path, local)]; ok {
					return importedSymbol(nm, name), true
				}
			}
		}
	}
	for _, d := range file.Defs {
		switch n := d.(type) {
		case *ast.Func:
			if defNameAt(n, ln, bc, name) {
				return funcSymbol(root, "", n), true
			}
			if sym, ok := paramOrBodySym(s.toks, path, root, "", n, ln, bc, name); ok {
				return sym, true
			}
		case *ast.Object:
			key := qualKeyLocal(root, n.NameText)
			if nameAt(n.Pos) {
				return symbol{kind: symType, name: name, key: key}, true
			}
			if sym, ok := fieldSymAt(key, path, n.Fields, ln, bc, name); ok {
				return sym, true
			}
			for _, m := range n.Methods {
				if defNameAt(m, ln, bc, name) {
					return funcSymbol(root, key, m), true
				}
				if sym, ok := paramOrBodySym(s.toks, path, root, key, m, ln, bc, name); ok {
					return sym, true
				}
			}
		case *ast.Interface:
			key := qualKeyLocal(root, n.NameText)
			if nameAt(n.Pos) {
				return symbol{kind: symType, name: name, key: key}, true
			}
			if sym, ok := fieldSymAt(key, path, n.Fields, ln, bc, name); ok {
				return sym, true
			}
			for _, m := range n.Methods {
				if defNameAt(m, ln, bc, name) {
					return funcSymbol(root, key, m), true
				}
				if sym, ok := paramOrBodySym(s.toks, path, root, key, m, ln, bc, name); ok {
					return sym, true
				}
			}
		case *ast.Enum:
			key := qualKeyLocal(root, n.NameText)
			if nameAt(n.Pos) {
				return symbol{kind: symType, name: name, key: key}, true
			}
			for _, m := range n.Members {
				if m.Pos.Line == ln && m.Pos.Col == bc && m.NameText == name {
					return symbol{kind: symEnumMember, name: name, owner: key}, true
				}
			}
		case *ast.TypeAlias:
			if nameAt(n.Pos) {
				return symbol{kind: symType, name: name, key: qualKeyLocal(root, n.NameText)}, true
			}
		case *ast.Decl:
			if nameAt(n.Pos) {
				return symbol{kind: symGlobal, name: name, file: path, at: n.Where(), node: n}, true
			}
		}
	}
	// 顶层语句流里的块也能声明局部量（顶层 if 体里那一些），与函数体里走同一条认法。
	//
	// en: Blocks in the top-level statement flow also declare locals (those in a top-level if
	// body), recognised the same way as in a function body.
	var found symbol
	hit := false
	walkStmts(file.Stmts, func(st ast.Stmt) bool {
		if sym, ok := localSymInStmt(s.toks, path, st, ln, bc, name); ok {
			found, hit = sym, true
			return false
		}
		return true
	})
	return found, hit
}

// paramOrBodySym 认一条函数或方法的参数与体里的声明：参数、局部 let／const、循环变量、
// catch 的那几个名字。参数与 catch 名字的登记位置是函数与 try 那一个关键字的位置
// （declareKind 收到的就是它），同一组里名字各不相同，位置加名字就是身份。
//
// en: paramOrBodySym recognises a function or method's parameters and the declarations in its
// body: parameters, local let/const, loop variables, catch names. A parameter's or catch name's
// registered position is the function's or try's keyword position (what declareKind received);
// names within one group differ, so position plus name is the identity.
func paramOrBodySym(toks []lexer.Tok, path, root, owner string, fn *ast.Func, ln, bc int, name string) (symbol, bool) {
	for _, p := range fn.Params {
		if p.Pos.Line == ln && p.Pos.Col == bc && p.NameText == name {
			return symbol{kind: symLocal, name: name, file: path, at: fn.Where()}, true
		}
	}
	var found symbol
	ok := false
	walkStmts(fn.Body, func(st ast.Stmt) bool {
		if sym, hit := localSymInStmt(toks, path, st, ln, bc, name); hit {
			found, ok = sym, true
			return false
		}
		return true
	})
	return found, ok
}

// localSymInStmt 认一条语句里声明的名字：局部 let／const、循环变量、catch 的那几个。
//
// en: localSymInStmt recognises a name declared in one statement: a local let/const, a loop
// variable, or one of the catch names.
func localSymInStmt(toks []lexer.Tok, path string, st ast.Stmt, ln, bc int, name string) (symbol, bool) {
	switch n := st.(type) {
	case *ast.Decl:
		np, valid := namePosAfter(toks, n.Pos)
		if valid && np.Line == ln && np.Col == bc && n.NameText == name {
			return symbol{kind: symLocal, name: name, file: path, at: n.Where()}, true
		}
	case *ast.For:
		np, valid := namePosAfter(toks, n.Var.Pos)
		if valid && np.Line == ln && np.Col == bc && n.Var.NameText == name {
			return symbol{kind: symLocal, name: name, file: path, at: n.Var.Where()}, true
		}
	case *ast.Try:
		for _, cn := range catchNames(toks, n) {
			if cn.pos.Line == ln && cn.pos.Col == bc && cn.name == name {
				return symbol{kind: symLocal, name: name, file: path, at: n.Where()}, true
			}
		}
	}
	return symbol{}, false
}

// fieldSymAt 认光标压在一个字段写出来的名字上。
//
// en: fieldSymAt recognises the cursor resting on a field's written name.
func fieldSymAt(owner, path string, fields []*ast.Field, ln, bc int, name string) (symbol, bool) {
	for _, f := range fields {
		if f.Pos.Line == ln && f.Pos.Col == bc && f.NameText == name {
			return symbol{kind: symField, name: name, owner: owner, file: path, at: f.Pos}, true
		}
	}
	return symbol{}, false
}

// useSymbolAt 认使用处的名字：调用点、点号后面、绑到声明的名字、类型名、引进来的名字。
// 顺序与悬停那一条相同，先认得准的在前。
//
// en: useSymbolAt recognises a name at a use site: a call, after a dot, a name bound to a
// declaration, a type name, an imported name. The order matches hover: the more precise routes
// come first.
func useSymbolAt(prog *check.Program, facts *check.Facts, path string, ln, bc int, name string) (symbol, bool) {
	for call, ref := range facts.Calls {
		var at ast.Pos
		switch n := call.Callee.(type) {
		case *ast.Name:
			at = n.Pos
		case *ast.Member:
			at = n.Pos
		default:
			continue
		}
		if at.Line != ln || at.Col != bc {
			continue
		}
		if ref.Func == "" {
			return symbol{}, false // 标准库的动作没有写在源码里的声明
		}
		return symbol{kind: funcOrMethodKind(ref.Func), name: name, key: check.CallableBase(ref.Func), owner: ownerOfKey(ref.Func)}, true
	}
	for m, note := range facts.Members {
		if m.Pos.Line != ln || m.Pos.Col != bc || m.NameText != name {
			continue
		}
		switch note.Kind {
		case check.MemberMethod:
			return symbol{kind: symMethod, name: name, owner: check.CallableBase(note.OwnerName)}, true
		case check.MemberField:
			sym := symbol{kind: symField, name: name, owner: check.CallableBase(note.OwnerName)}
			sym.file, sym.at = fieldDeclPos(prog, sym.owner, name)
			return sym, true
		case check.MemberEnumValue:
			return symbol{kind: symEnumMember, name: name, owner: note.OwnerName}, true
		case check.MemberPkgFunc:
			if root, isPkg := check.ReceiverIsPackage(path, note.OwnerName, facts); isPkg {
				return symbol{kind: symFunc, name: name, key: root + "/" + name}, true
			}
		case check.MemberPkgConst:
			if root, isPkg := check.ReceiverIsPackage(path, note.OwnerName, facts); isPkg {
				if tv, ok := facts.TopValues[root+"/"+name]; ok {
					return symbol{kind: symGlobal, name: name, file: tv.File, at: tv.At, node: tv.Decl}, true
				}
			}
		}
		return symbol{}, false
	}
	// 函数名当值交出去的那一处：检查器记在 FuncRefs 上，不在名字绑定那张表里。
	//
	// en: A function name handed over as a value: the checker records it in FuncRefs, not in
	// the name-binding table.
	for e, ref := range facts.FuncRefs {
		if n, ok := e.(*ast.Name); ok && n.Pos.Line == ln && n.Pos.Col == bc && n.Text == name {
			return symbol{kind: symFunc, name: name, key: check.CallableBase(ref)}, true
		}
	}
	if b, ok := bindAt(facts, ln, bc); ok {
		switch {
		case b.By == check.BindThis || b.By == check.BindArgs:
			return symbol{}, false // this 与 args 是语言自己名字，没有可改的声明
		case b.By == check.BindField:
			sym := symbol{kind: symField, name: name, file: path, at: b.At}
			sym.owner = fieldOwnerAt(prog, path, b.At)
			return sym, sym.owner != ""
		case b.Param != nil:
			return symbol{kind: symLocal, name: name, file: path, at: b.At}, true
		case b.Decl != nil && b.Global:
			if file, at, ok := locate(prog, b.Decl); ok {
				return symbol{kind: symGlobal, name: b.Decl.NameText, file: file, at: at, node: b.Decl}, true
			}
		case b.Decl != nil:
			return symbol{kind: symLocal, name: name, file: path, at: b.At}, true
		case b.Global:
			if nm, ok := facts.Imported[check.NameKey(path, name)]; ok {
				return importedSymbol(nm, name), true
			}
		default:
			return symbol{kind: symLocal, name: name, file: path, at: b.At}, true // catch 的名字
		}
	}
	if key := resolveTypeKey(prog, facts, path, name); key != "" {
		return symbol{kind: symType, name: name, key: key}, true
	}
	if nm, ok := facts.Imported[check.NameKey(path, name)]; ok {
		return importedSymbol(nm, name), true
	}
	return symbol{}, false
}

// importedSymbol 把引进来的那一个名字认回它原来的符号：函数、类型还是顶层的数。
//
// en: importedSymbol traces an imported name back to its original symbol: a function, a type,
// or a top-level value.
func importedSymbol(nm check.ImportedName, name string) symbol {
	switch {
	case nm.FuncNm != "":
		return symbol{kind: symFunc, name: name, key: check.CallableBase(nm.FuncNm)}
	case nm.TypeNm != "":
		return symbol{kind: symType, name: name, key: nm.TypeNm}
	default:
		original := name
		if nm.Decl != nil {
			original = nm.Decl.NameText
		}
		return symbol{kind: symGlobal, name: original, file: nm.From, at: nm.At, node: nm.Decl}
	}
}

// funcOrMethodKind 与 ownerOfKey 拆函数那一种键：带最后一个点的是方法（点前是所属类型），
// 不带的是顶层函数。包根里有斜杠没有点，所以按最后一个点拆是准的。
//
// en: funcOrMethodKind and ownerOfKey split a function key: one with a final dot is a method
// (the type comes before the dot), one without is a top-level function. Package roots contain
// slashes but no dots, so splitting at the last dot is exact.
func funcOrMethodKind(key string) int {
	if strings.LastIndexByte(key, '.') >= 0 {
		return symMethod
	}
	return symFunc
}

func ownerOfKey(key string) string {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		return check.CallableBase(key[:i])
	}
	return ""
}

// qualKeyLocal 拼一个定义在文件里的规范名：包里的带包根，工程自己的文件就是原名。
// 与检查器那一条（qualKey）同形。
//
// en: qualKeyLocal builds the canonical name of a definition in a file: package definitions
// carry the package root, the project's own files use the bare name. Same shape as the
// checker's qualKey.
func qualKeyLocal(root, name string) string {
	if root == "" {
		return name
	}
	return root + "/" + name
}

// funcSymbol 认一条函数或方法声明：owner 为空是顶层函数，否则是方法。
//
// en: funcSymbol recognises a function or method declaration: an empty owner means a top-level
// function, otherwise a method.
func funcSymbol(root, owner string, fn *ast.Func) symbol {
	if owner == "" {
		return symbol{kind: symFunc, name: fn.NameText, key: qualKeyLocal(root, fn.NameText)}
	}
	return symbol{kind: symMethod, name: fn.NameText, owner: check.CallableBase(owner)}
}

// resolveTypeKey 把一份文件里写出来的类型名认成规范名：先问花括号引进来的，再问整块挂名
// 点出来的，最后按本文件的包根拼。内建类型与容器不在 Declared 里，答空串。
//
// en: resolveTypeKey recognises a type name written in a file as a canonical name: brace
// imports first, then whole-module dot access, then the file's own package root. Built-in types
// and containers are not in Declared and answer the empty string.
func resolveTypeKey(prog *check.Program, facts *check.Facts, file, name string) string {
	if nm, ok := facts.Imported[check.NameKey(file, name)]; ok && nm.TypeNm != "" {
		return nm.TypeNm
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		if root, ok := facts.Packages[check.NameKey(file, name[:i])]; ok {
			return root + "/" + name[i+1:]
		}
		return ""
	}
	key := qualKeyLocal(prog.PkgRoots[file], name)
	if _, ok := facts.Declared[key]; ok {
		return key
	}
	return ""
}

// fieldDeclPos 找一条字段声明写在哪份文件哪一列：方法体里直接写的字段名按这个位置认。
//
// en: fieldDeclPos finds where a field declaration is written: a bare field name in a method
// body is recognised by this position.
func fieldDeclPos(prog *check.Program, owner, name string) (string, ast.Pos) {
	for _, f := range prog.Files {
		root := prog.PkgRoots[f.Path]
		for _, d := range f.Defs {
			var fields []*ast.Field
			switch n := d.(type) {
			case *ast.Object:
				fields = n.Fields
			case *ast.Interface:
				fields = n.Fields
			default:
				continue
			}
			if check.CallableBase(qualKeyLocal(root, d.Name())) != owner {
				continue
			}
			for _, fl := range fields {
				if fl.NameText == name {
					return f.Path, fl.Pos
				}
			}
		}
	}
	return "", ast.Pos{}
}

// fieldOwnerAt 反过来问：这份文件里登记在这个位置的字段属于哪一个类型。
//
// en: fieldOwnerAt asks the reverse: which type owns the field registered at this position in
// this file.
func fieldOwnerAt(prog *check.Program, path string, at ast.Pos) string {
	for _, f := range prog.Files {
		if f.Path != path {
			continue
		}
		root := prog.PkgRoots[path]
		for _, d := range f.Defs {
			var fields []*ast.Field
			switch n := d.(type) {
			case *ast.Object:
				fields = n.Fields
			case *ast.Interface:
				fields = n.Fields
			default:
				continue
			}
			for _, fl := range fields {
				if fl.Pos == at {
					return qualKeyLocal(root, d.Name())
				}
			}
		}
	}
	return ""
}

// methodFamily 找方法那一家：base 链上声明了同一个方法名的类型都算。改接口里的方法名时，
// 实现它的那些类身上的同名方法要一起改，只改一处程序就编译不过了。
//
// en: methodFamily finds a method's family: every type on a base chain declaring the same
// method name counts. Renaming a method in an interface must also rename the same-named methods
// on the classes implementing it, or the program stops compiling.
func methodFamily(prog *check.Program, facts *check.Facts, owner, name string) map[string]bool {
	type edge struct{ child, parent string }
	var edges []edge
	declares := map[string]bool{}
	for _, f := range prog.Files {
		root := prog.PkgRoots[f.Path]
		for _, d := range f.Defs {
			var base ast.Expr
			var methods []*ast.Func
			switch n := d.(type) {
			case *ast.Object:
				base, methods = n.Base, n.Methods
			case *ast.Interface:
				base, methods = n.Base, n.Methods
			default:
				continue
			}
			key := check.CallableBase(qualKeyLocal(root, d.Name()))
			for _, m := range methods {
				if m.NameText == name {
					declares[key] = true
					break
				}
			}
			if bn, ok := base.(*ast.Name); ok {
				if pk := resolveTypeKey(prog, facts, f.Path, bn.Text); pk != "" {
					edges = append(edges, edge{key, check.CallableBase(pk)})
				}
			}
		}
	}
	family := map[string]bool{owner: true}
	for changed := true; changed; {
		changed = false
		for _, e := range edges {
			if family[e.child] && declares[e.parent] && !family[e.parent] {
				family[e.parent], changed = true, true
			}
			if family[e.parent] && declares[e.child] && !family[e.child] {
				family[e.child], changed = true, true
			}
		}
	}
	return family
}

// namePosAfter 从声明节点的位置找到名字那一个记号：节点指在关键字上（let、class、method、
// shared 这些），名字是它后面第一个不是关键字的 name 记号；指在名字自己上的（循环变量）
// 第一个就命中。
//
// en: namePosAfter finds the name token from a declaration node's position: the node points at
// a keyword (let, class, method, shared), and the name is the first non-keyword name token after
// it; one pointing at the name itself (a loop variable) hits on the first token.
func namePosAfter(toks []lexer.Tok, at ast.Pos) (ast.Pos, bool) {
	for i, t := range toks {
		if t.Line < at.Line || (t.Line == at.Line && t.Col < at.Col) {
			continue
		}
		if t.Line != at.Line || t.Col != at.Col {
			return ast.Pos{}, false
		}
		for j := i; j < len(toks); j++ {
			if toks[j].Kind == lexer.Name && !lexer.Keywords[toks[j].Text] {
				return ast.Pos{Line: toks[j].Line, Col: toks[j].Col}, true
			}
			if toks[j].Kind == lexer.NL || toks[j].Kind == lexer.EOF {
				return ast.Pos{}, false
			}
		}
		return ast.Pos{}, false
	}
	return ast.Pos{}, false
}

// catchName 是 catch 那组名字里的一个：写出来那一截与它的位置。
//
// en: catchName is one of the catch group: the written name and its position.
type catchName struct {
	name string
	pos  ast.Pos
}

// catchNames 从记号里读出这一条 try 的 catch 名字。语法节点只记下名字的文字没记位置，
// 所以越过 try 体那一对花括号（里面嵌套的 try 不算），找到 catch 后面那一对圆括号。
//
// en: catchNames reads this try's catch names from the tokens. The syntax node records the names'
// text but not their positions, so this skips the try body's brace pair (a nested try inside does
// not count) and finds the parenthesis pair after catch.
func catchNames(toks []lexer.Tok, st *ast.Try) []catchName {
	depth := 0
	started := false
	for i, t := range toks {
		if t.Line < st.Pos.Line || (t.Line == st.Pos.Line && t.Col < st.Pos.Col) {
			continue
		}
		if t.Kind == lexer.EOF {
			return nil
		}
		if t.Kind == lexer.Op {
			switch t.Text {
			case "{":
				depth++
				started = true
			case "}":
				depth--
			}
			continue
		}
		if !started || depth != 0 || t.Kind != lexer.Name || t.Text != "catch" {
			continue
		}
		var out []catchName
		open := -1
		for j := i + 1; j < len(toks); j++ {
			if toks[j].Kind == lexer.Op && toks[j].Text == "(" {
				open = j
				break
			}
		}
		if open < 0 {
			return nil
		}
		for j := open + 1; j < len(toks); j++ {
			if toks[j].Kind == lexer.Op && toks[j].Text == ")" {
				break
			}
			if toks[j].Kind == lexer.Name && !lexer.Keywords[toks[j].Text] {
				out = append(out, catchName{toks[j].Text, ast.Pos{Line: toks[j].Line, Col: toks[j].Col}})
			}
		}
		return out
	}
	return nil
}

// collectSites 收集一个符号写过的每一处。每一参与的文件都走一遍语法树，比检查器留下的
// 那几份结论；声明那一处按 withDecl 决定要不要。
//
// en: collectSites gathers everywhere a symbol is written. Every file involved is walked once,
// compared against the checker's conclusions; whether the declaration itself counts is up to
// withDecl.
func collectSites(prog *check.Program, facts *check.Facts, entry, text string, sym symbol, withDecl bool) []refSite {
	w := &refWalk{
		prog: prog, facts: facts, sym: sym, withDecl: withDecl,
		sites: map[refSite]bool{},
	}
	if sym.kind == symMethod {
		w.family = methodFamily(prog, facts, sym.owner, sym.name)
	}
	for _, f := range prog.Files {
		src := text
		if f.Path != entry {
			s, ok := pkgload.New().ReadSource(f.Path)
			if !ok {
				continue
			}
			src = s
		}
		toks, err := lexer.Lex(src)
		if err != nil {
			continue
		}
		w.file, w.toks = f.Path, toks
		w.walkFile(f)
	}
	out := make([]refSite, 0, len(w.sites))
	for site := range w.sites {
		out = append(out, site)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].col < out[j].col
	})
	return out
}

// sitesToLocations 把收集到的位置排成协议里的答复。名字只含 ASCII，结束列就是起点加长度；
// 行里有中文也不影响，列按那一份文件的行文本换成 UTF-16。手上这一份用编辑器推过来的文本，
// 盘上那份可能是上一次保存的旧内容。
//
// en: sitesToLocations lays the collected positions out as the protocol's reply. Names are
// ASCII-only, so the end column is the start plus the length; Chinese elsewhere on the line does
// not matter, columns are converted to UTF-16 against that file's line text. The file in hand
// uses the text the editor pushed; the copy on disk may be the version from the last save.
func sitesToLocations(sites []refSite, entry, text string) []Location {
	if len(sites) == 0 {
		return nil
	}
	lines := map[string][]string{entry: splitLines(text)}
	out := make([]Location, 0, len(sites))
	for _, site := range sites {
		ls, ok := lines[site.file]
		if !ok {
			src, _ := pkgload.New().ReadSource(site.file)
			ls = splitLines(src)
			lines[site.file] = ls
		}
		if site.line < 1 || site.line > len(ls) {
			continue
		}
		line := ls[site.line-1]
		if site.col < 1 || site.col-1 > len(line) {
			continue
		}
		start := utf16Len(line[:site.col-1])
		out = append(out, Location{
			URI: uriFromPath(site.file),
			Range: Range{
				Start: Position{Line: site.line - 1, Character: start},
				End:   Position{Line: site.line - 1, Character: start + len(site.name)},
			},
		})
	}
	return out
}

// refWalk 是收集引用时走一遍语法树要带的那几样：找的是谁、走到哪份文件、当前方法属于谁。
//
// en: refWalk carries what a reference-collecting walk needs: which symbol is sought, which file
// the walk is in, and which type the current method belongs to.
type refWalk struct {
	prog     *check.Program
	facts    *check.Facts
	sym      symbol
	family   map[string]bool // symMethod：base 链上同名方法的那一家
	withDecl bool
	sites    map[refSite]bool
	file     string
	toks     []lexer.Tok
	owner    string
	generic  bool // 正走在带类型参数的声明体里：结论记在复制出来的节点上，按位置认回去
}

// bindOf、noteOf、callRefOf、funcRefOf 先按节点指针问结论表；走在泛型体里时指针问不到
// （检查器查的是复制出来的那份节点），再按位置与名字认一次——复制的位置与模板相同。
//
// en: bindOf, noteOf, callRefOf and funcRefOf ask the conclusion tables by node pointer first;
// inside a generic body the pointer misses (the checker examined the copied nodes), so a second
// lookup matches by position and name - a copy carries the template's positions.
func (w *refWalk) bindOf(n *ast.Name) (check.Bind, bool) {
	if b, ok := w.facts.Binds[n]; ok {
		return b, true
	}
	if w.generic {
		for k, b := range w.facts.Binds {
			if k.Pos == n.Pos && k.Text == n.Text {
				return b, true
			}
		}
	}
	return check.Bind{}, false
}

func (w *refWalk) noteOf(m *ast.Member) (check.MemberNote, bool) {
	if note, ok := w.facts.Members[m]; ok {
		return note, true
	}
	if w.generic {
		for k, note := range w.facts.Members {
			if k.Pos == m.Pos && k.NameText == m.NameText {
				return note, true
			}
		}
	}
	return check.MemberNote{}, false
}

func (w *refWalk) callRefOf(c *ast.Call) (check.CallRef, bool) {
	if ref, ok := w.facts.Calls[c]; ok {
		return ref, true
	}
	if w.generic {
		for k, ref := range w.facts.Calls {
			if k.Pos == c.Pos {
				return ref, true
			}
		}
	}
	return check.CallRef{}, false
}

func (w *refWalk) funcRefOf(n *ast.Name) (string, bool) {
	if ref, ok := w.facts.FuncRefs[n]; ok {
		return ref, true
	}
	if w.generic {
		for k, ref := range w.facts.FuncRefs {
			if kn, ok := k.(*ast.Name); ok && kn.Pos == n.Pos && kn.Text == n.Text {
				return ref, true
			}
		}
	}
	return "", false
}

func (w *refWalk) add(line, col int, name string) {
	w.sites[refSite{file: w.file, line: line, col: col, name: name}] = true
}

func (w *refWalk) addAt(p ast.Pos, name string) { w.add(p.Line, p.Col, name) }

// walkFile 走一份文件：import 引进来的名字、顶层定义与它们的体。
//
// en: walkFile walks one file: imported names, top-level definitions and their bodies.
func (w *refWalk) walkFile(f *ast.File) {
	sym := w.sym
	for _, imp := range f.Imports {
		for _, in := range imp.Names {
			local := in.Alias
			if local == "" {
				local = in.Name
			}
			nm, ok := w.facts.Imported[check.NameKey(w.file, local)]
			if !ok {
				continue
			}
			match := false
			switch sym.kind {
			case symFunc:
				match = nm.FuncNm != "" && check.CallableBase(nm.FuncNm) == sym.key
			case symType:
				match = nm.TypeNm == sym.key
			case symGlobal:
				match = nm.Decl != nil && nm.Decl == sym.node
			}
			if match {
				w.addAt(in.Pos, in.Name)
			}
		}
	}
	root := w.prog.PkgRoots[w.file]
	for _, d := range f.Defs {
		w.owner = ""
		w.generic = false
		switch n := d.(type) {
		case *ast.Func:
			if w.withDecl && sym.kind == symFunc && !n.IsMethod && qualKeyLocal(root, n.NameText) == sym.key {
				w.addAt(n.NamePos, n.NameText)
			}
			w.generic = len(n.TypeParams) > 0
			w.walkFunc(n)
		case *ast.Object:
			key := qualKeyLocal(root, n.NameText)
			w.declTypeSym(n.Pos, n.NameText, key)
			w.walkFields(key, n.Fields)
			if bn, ok := n.Base.(*ast.Name); ok {
				w.typeNameUse(bn.Pos, bn.Text)
			}
			for _, m := range n.Methods {
				w.owner = key
				w.generic = len(n.TypeParams) > 0 || len(m.TypeParams) > 0
				if w.withDecl && sym.kind == symMethod && m.NameText == sym.name && w.family[check.CallableBase(key)] {
					w.addAt(m.NamePos, m.NameText)
				}
				w.walkFunc(m)
			}
		case *ast.Interface:
			key := qualKeyLocal(root, n.NameText)
			w.declTypeSym(n.Pos, n.NameText, key)
			w.walkFields(key, n.Fields)
			if bn, ok := n.Base.(*ast.Name); ok {
				w.typeNameUse(bn.Pos, bn.Text)
			}
			for _, m := range n.Methods {
				w.owner = key
				w.generic = len(m.TypeParams) > 0
				if w.withDecl && sym.kind == symMethod && m.NameText == sym.name && w.family[check.CallableBase(key)] {
					w.addAt(m.NamePos, m.NameText)
				}
				w.walkFunc(m)
			}
		case *ast.Enum:
			key := qualKeyLocal(root, n.NameText)
			w.declTypeSym(n.Pos, n.NameText, key)
			if w.withDecl && sym.kind == symEnumMember && sym.owner == key {
				for _, m := range n.Members {
					if m.NameText == sym.name {
						w.addAt(m.Pos, m.NameText)
					}
				}
			}
			for _, m := range n.Members {
				w.walkExpr(m.Value)
			}
		case *ast.TypeAlias:
			key := qualKeyLocal(root, n.NameText)
			w.declTypeSym(n.Pos, n.NameText, key)
			w.walkType(n.Typ)
		case *ast.Decl:
			if w.withDecl && sym.kind == symGlobal && n == sym.node {
				if np, ok := namePosAfter(w.toks, n.Pos); ok {
					w.addAt(np, n.NameText)
				}
			}
			w.walkType(n.Typ)
			w.walkExpr(n.Value)
		}
	}
	// 顶层的语句流也需要走一遍：console.print 那一些调用与读写都写在这一层。
	//
	// en: The top-level statement flow also needs a walk: calls and reads/writes like
	// console.print are written at this layer.
	w.owner = ""
	w.generic = false
	w.walkStmts(f.Stmts)
}

// declTypeSym 记类型声明自己那一处（找的是这个类型、且要声明那一处时）。
//
// en: declTypeSym records a type's own declaration site (when this type is sought and the
// declaration counts).
func (w *refWalk) declTypeSym(pos ast.Pos, name, key string) {
	if w.withDecl && w.sym.kind == symType && w.sym.key == key {
		if np, ok := namePosAfter(w.toks, pos); ok {
			w.addAt(np, name)
		}
	}
}

// walkFields 走类型身上的字段：声明那一处与写出来的类型。
//
// en: walkFields walks the fields on a type: declaration sites and written types.
func (w *refWalk) walkFields(owner string, fields []*ast.Field) {
	for _, f := range fields {
		if w.withDecl && w.sym.kind == symField && w.sym.name == f.NameText && check.CallableBase(w.sym.owner) == check.CallableBase(owner) {
			w.addAt(f.Pos, f.NameText)
		}
		w.walkType(f.Typ)
	}
}

// walkFunc 走一条函数或方法：参数、返回类型、体。参数与局部量的声明按登记位置认。
//
// en: walkFunc walks one function or method: parameters, return type, body. Parameter and local
// declarations are matched by registered position.
func (w *refWalk) walkFunc(fn *ast.Func) {
	sym := w.sym
	for _, p := range fn.Params {
		if w.withDecl && sym.kind == symLocal && sym.file == w.file && sym.at == fn.Where() && sym.name == p.NameText {
			w.addAt(p.Pos, p.NameText)
		}
		w.walkType(p.Typ)
		w.walkExpr(p.Default)
	}
	w.walkType(fn.Ret)
	w.walkStmts(fn.Body)
}

// walkStmts 走一串语句。visit 给认声明那一侧用：返回 false 就不再往深处走。
//
// en: walkStmts walks a statement list. visit serves the declaration-recognising side: returning
// false stops the descent.
func walkStmts(ss []ast.Stmt, visit func(ast.Stmt) bool) {
	for _, st := range ss {
		if visit != nil && !visit(st) {
			return
		}
		switch n := st.(type) {
		case *ast.If:
			for _, b := range n.Bodies {
				walkStmts(b, visit)
			}
			walkStmts(n.Else, visit)
		case *ast.While:
			walkStmts(n.Body, visit)
		case *ast.For:
			walkStmts(n.Body, visit)
		case *ast.Switch:
			for _, c := range n.Cases {
				walkStmts(c.Body, visit)
			}
		case *ast.Try:
			walkStmts(n.Body, visit)
			walkStmts(n.Catch, visit)
			walkStmts(n.Finally, visit)
		}
	}
}

func (w *refWalk) walkStmts(ss []ast.Stmt) {
	for _, st := range ss {
		switch n := st.(type) {
		case *ast.Assign:
			w.walkExpr(n.Target)
			w.walkExpr(n.Value)
		case *ast.Incdec:
			w.walkExpr(n.What)
		case *ast.ExprStmt:
			w.walkExpr(n.E)
		case *ast.Decl:
			if w.withDecl && w.sym.kind == symLocal && w.sym.file == w.file && w.sym.at == n.Where() && w.sym.name == n.NameText {
				if np, ok := namePosAfter(w.toks, n.Pos); ok {
					w.addAt(np, n.NameText)
				}
			}
			w.walkType(n.Typ)
			w.walkExpr(n.Value)
		case *ast.If:
			for i, cond := range n.Conds {
				w.walkExpr(cond)
				w.walkStmts(n.Bodies[i])
			}
			w.walkStmts(n.Else)
		case *ast.While:
			w.walkExpr(n.Cond)
			w.walkStmts(n.Body)
		case *ast.For:
			if w.withDecl && w.sym.kind == symLocal && w.sym.file == w.file && w.sym.at == n.Var.Where() && w.sym.name == n.Var.NameText {
				if np, ok := namePosAfter(w.toks, n.Var.Pos); ok {
					w.addAt(np, n.Var.NameText)
				}
			}
			w.walkType(n.Var.Typ)
			w.walkExpr(n.From)
			w.walkExpr(n.Until)
			w.walkExpr(n.Iter)
			w.walkStmts(n.Body)
		case *ast.Switch:
			w.walkExpr(n.Head)
			for _, c := range n.Cases {
				for _, l := range c.Labels {
					w.walkExpr(l)
				}
				w.walkStmts(c.Body)
			}
		case *ast.Return:
			w.walkExpr(n.Val)
		case *ast.Try:
			if w.withDecl && w.sym.kind == symLocal && w.sym.file == w.file && w.sym.at == n.Where() {
				for _, cn := range catchNames(w.toks, n) {
					if cn.name == w.sym.name {
						w.addAt(cn.pos, cn.name)
					}
				}
			}
			w.walkStmts(n.Body)
			w.walkStmts(n.Catch)
			w.walkStmts(n.Finally)
		}
	}
}

// walkExpr 走一个表达式：名字、点号、调用与写出来的类型都在这里对上结论表。
//
// en: walkExpr walks one expression: names, dots, calls and written types are matched against
// the conclusion tables here.
func (w *refWalk) walkExpr(e ast.Expr) {
	if e == nil {
		return
	}
	sym := w.sym
	switch n := e.(type) {
	case *ast.Name:
		if b, ok := w.bindOf(n); ok {
			switch sym.kind {
			case symLocal:
				if (b.By == check.BindLocal || b.By == check.BindParam) && w.file == sym.file && b.At == sym.at && n.Text == sym.name {
					w.addAt(n.Pos, n.Text)
				}
			case symField:
				if b.By == check.BindField && w.file == sym.file && b.At == sym.at && n.Text == sym.name {
					w.addAt(n.Pos, n.Text)
				}
			case symGlobal:
				if b.Global && w.globalUseMatch(n, b) {
					w.addAt(n.Pos, n.Text)
				}
			}
		}
		if sym.kind == symFunc {
			if ref, ok := w.funcRefOf(n); ok && check.CallableBase(ref) == sym.key {
				w.addAt(n.Pos, n.Text)
			}
		}
	case *ast.Member:
		if note, ok := w.noteOf(n); ok {
			switch sym.kind {
			case symMethod:
				if note.Kind == check.MemberMethod && n.NameText == sym.name && w.family[check.CallableBase(note.OwnerName)] {
					w.addAt(n.Pos, n.NameText)
				}
			case symField:
				if note.Kind == check.MemberField && n.NameText == sym.name && check.CallableBase(note.OwnerName) == check.CallableBase(sym.owner) {
					w.addAt(n.Pos, n.NameText)
				}
			case symEnumMember:
				if note.Kind == check.MemberEnumValue && n.NameText == sym.name && note.OwnerName == sym.owner {
					w.addAt(n.Pos, n.NameText)
				}
			case symFunc:
				if note.Kind == check.MemberPkgFunc && n.NameText == sym.name {
					if root, isPkg := check.ReceiverIsPackage(w.file, note.OwnerName, w.facts); isPkg && root+"/"+n.NameText == sym.key {
						w.addAt(n.Pos, n.NameText)
					}
				}
			case symGlobal:
				if note.Kind == check.MemberPkgConst && n.NameText == sym.name {
					if root, isPkg := check.ReceiverIsPackage(w.file, note.OwnerName, w.facts); isPkg && w.prog.PkgRoots[sym.file] == root {
						w.addAt(n.Pos, n.NameText)
					}
				}
			case symType:
				if note.Kind == check.MemberEnumValue && note.OwnerName == sym.key {
					if recv, ok := n.What.(*ast.Name); ok {
						w.addAt(recv.Pos, recv.Text)
					}
				}
			}
		}
		w.walkExpr(n.What)
	case *ast.Call:
		if sym.kind == symFunc || sym.kind == symMethod {
			if ref, ok := w.callRefOf(n); ok && ref.Func != "" {
				match := false
				if sym.kind == symFunc {
					match = funcOrMethodKind(ref.Func) == symFunc && check.CallableBase(ref.Func) == sym.key
				} else {
					match = ownerOfKey(ref.Func) != "" && w.family[ownerOfKey(ref.Func)] && check.SealedName(ref.Func) == sym.name
				}
				if match {
					switch c := n.Callee.(type) {
					case *ast.Name:
						w.addAt(c.Pos, c.Text)
					case *ast.Member:
						w.addAt(c.Pos, c.NameText)
					}
				}
			}
		}
		w.walkExpr(n.Callee)
		for _, a := range n.Args {
			w.walkExpr(a)
		}
		for _, t := range n.TypeArgs {
			w.walkType(t)
		}
	case *ast.Binary:
		w.walkExpr(n.Left)
		w.walkExpr(n.Right)
	case *ast.Unary:
		w.walkExpr(n.What)
	case *ast.Convert:
		w.walkExpr(n.What)
		w.walkType(n.Typ)
	case *ast.Test:
		w.walkExpr(n.What)
		w.walkType(n.Typ)
	case *ast.Index:
		w.walkExpr(n.What)
		w.walkExpr(n.At)
	case *ast.StructLit:
		w.typeNameUse(n.Pos, n.Type)
		w.structFieldUses(n.Type, n.Fields)
		for _, t := range n.TypeArgs {
			w.walkType(t)
		}
		for _, f := range n.Fields {
			w.walkExpr(f.Value)
		}
	case *ast.New:
		if np, ok := namePosAfter(w.toks, n.Pos); ok {
			w.typeNameUse(np, n.Type)
		}
		w.structFieldUses(n.Type, n.Fields)
		for _, t := range n.TypeArgs {
			w.walkType(t)
		}
		for _, f := range n.Fields {
			w.walkExpr(f.Value)
		}
	case *ast.ListLit:
		for _, it := range n.Items {
			w.walkExpr(it)
		}
	case *ast.MapLit:
		for _, p := range n.Pairs {
			w.walkExpr(p.Key)
			w.walkExpr(p.Value)
		}
	case *ast.SetLit:
		for _, it := range n.Items {
			w.walkExpr(it)
		}
	case *ast.Throw:
		w.walkExpr(n.Code)
		w.walkExpr(n.Msg)
	case *ast.Exit:
		w.walkExpr(n.Code)
		w.walkExpr(n.Msg)
	}
}

// globalUseMatch 判一次顶层名字的使用找的是不是这一个：本程序里声明的按节点指针认；
// 引进来的数没有节点，按引进表上记的原声明文件与位置认。
//
// en: globalUseMatch decides whether a top-level name use seeks this one: declarations in this
// program match by node pointer; an imported value has no node and matches by the original
// declaration's file and position recorded in the import table.
func (w *refWalk) globalUseMatch(n *ast.Name, b check.Bind) bool {
	if b.Decl != nil {
		return b.Decl == w.sym.node
	}
	nm, ok := w.facts.Imported[check.NameKey(w.file, n.Text)]
	return ok && nm.From == w.sym.file && nm.At == w.sym.at
}

// typeNameUse 记一处写出来的类型名（struct 字面量与 new 后面那一个、base 后面那一个）。
//
// en: typeNameUse records one written type name (the one after a struct literal or new, the one
// after base).
func (w *refWalk) typeNameUse(pos ast.Pos, name string) {
	if w.sym.kind == symType && resolveTypeKey(w.prog, w.facts, w.file, name) == w.sym.key {
		w.addAt(pos, name)
	}
}

// structFieldUses 记 struct 字面量与 new 的字段初值里写出来的字段名：那几处不是点号，
// 检查器的成员表不收，按写出来的类型名认出所属再比名字。
//
// en: structFieldUses records field names written in struct-literal and new field initialisers:
// those are not dots and the checker's member table does not collect them, so the owner is
// recognised from the written type name and the name compared.
func (w *refWalk) structFieldUses(typeName string, fields []*ast.StructLitField) {
	if w.sym.kind != symField {
		return
	}
	owner := resolveTypeKey(w.prog, w.facts, w.file, typeName)
	if owner == "" || check.CallableBase(owner) != check.CallableBase(w.sym.owner) {
		return
	}
	for _, f := range fields {
		if f.NameText == w.sym.name {
			w.addAt(f.Pos, f.NameText)
		}
	}
}

// walkType 走写出来的类型：名字那一处与尖括号里的实参。
//
// en: walkType walks a written type: the name site and the arguments in angle brackets.
func (w *refWalk) walkType(t ast.Type) {
	if t == nil {
		return
	}
	switch n := t.(type) {
	case *ast.NamedType:
		w.typeNameUse(n.Pos, n.Name)
		for _, a := range n.Args {
			w.walkType(a)
		}
	case *ast.FuncType:
		for _, p := range n.Params {
			w.walkType(p.Typ)
		}
		w.walkType(n.Ret)
	}
}

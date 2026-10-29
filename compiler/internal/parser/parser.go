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

// Package parser 把记号流读成语法树。
//
// 语句到行尾结束，分号可写可不写，所以 nl 与 `;` 都算终止符。
//
// en: Package parser reads a token stream into a syntax tree.
// Statements end at the line feed and a semicolon is optional, so both nl and `;` count as a
// terminator.
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
)

type Error struct {
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("第 %d 行第 %d 列：%s", e.Line, e.Col, e.Msg) }

type parser struct {
	toks []lexer.Tok
	i    int
	file string
}

// Parse 读一个文件的记号，返回语法树。
//
// en: Parse reads the tokens of one file and returns its syntax tree.
func Parse(path string, toks []lexer.Tok) (*ast.File, error) {
	p := &parser{toks: toks, file: path}
	f := &ast.File{Path: path}
	p.skipTerminators()
	for !p.atEOF() {
		switch {
		case p.isName("import"):
			imp, err := p.parseImport()
			if err != nil {
				return nil, err
			}
			f.Imports = append(f.Imports, imp)
		case p.atDefStart():
			d, err := p.parseDef()
			if err != nil {
				return nil, err
			}
			f.Defs = append(f.Defs, d)
			// 顶层的 let／const 同时进语句流：顶层是一层作用域，
			// 按书写顺序执行，与函数体没有区别。
			if dc, ok := d.(*ast.Decl); ok {
				f.Stmts = append(f.Stmts, dc)
			}
		default:
			s, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			f.Stmts = append(f.Stmts, s)
		}
		if err := p.endOfStmt(); err != nil {
			return nil, err
		}
		p.skipTerminators()
	}
	return f, nil
}

// ---- 记号层的小工具 ----

func (p *parser) cur() lexer.Tok { return p.toks[p.i] }
func (p *parser) atEOF() bool    { return p.cur().Kind == lexer.EOF }
func (p *parser) advance() lexer.Tok {
	t := p.toks[p.i]
	if t.Kind != lexer.EOF {
		p.i++
	}
	return t
}

func (p *parser) peek(off int) lexer.Tok {
	if p.i+off >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.i+off]
}

func (p *parser) isName(w string) bool {
	t := p.cur()
	return t.Kind == lexer.Name && t.Text == w
}

func (p *parser) opIs(op string) bool {
	t := p.cur()
	return t.Kind == lexer.Op && t.Text == op
}

func (p *parser) eatOp(op string) bool {
	if p.opIs(op) {
		p.advance()
		return true
	}
	return false
}

func (p *parser) eatName(w string) bool {
	if p.isName(w) {
		p.advance()
		return true
	}
	return false
}

func (p *parser) expectOp(op string) error {
	if p.eatOp(op) {
		return nil
	}
	return p.err("想要 %q，实际看到 %s", op, p.describe(p.cur()))
}

func (p *parser) expectName() (lexer.Tok, error) {
	t := p.cur()
	if t.Kind != lexer.Name {
		return t, p.err("想要一个名字，实际看到 %s", p.describe(t))
	}
	p.advance()
	return t, nil
}

func (p *parser) err(format string, args ...any) error {
	return &Error{Line: p.cur().Line, Col: p.cur().Col, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) describe(t lexer.Tok) string {
	switch t.Kind {
	case lexer.EOF:
		return "文件末尾"
	case lexer.NL:
		return "行尾"
	default:
		return fmt.Sprintf("%s %q", t.Kind, t.Text)
	}
}

// isTerminator 识别行尾与分号两种终止符。
//
// en: isTerminator recognises the two terminators, the line feed and the semicolon.
func (p *parser) isTerminator() bool {
	return p.opIs(";") || p.cur().Kind == lexer.NL
}

func (p *parser) skipTerminators() {
	for !p.atEOF() && (p.cur().Kind == lexer.NL || p.opIs(";")) {
		p.advance()
	}
}

// endOfStmt 要求一条语句以终止符或 `}` 收尾。
//
// en: endOfStmt requires a statement to close with a terminator or with `}`.
func (p *parser) endOfStmt() error {
	if p.atEOF() || p.cur().Kind == lexer.NL || p.opIs(";") || p.opIs("}") {
		for p.eatOp(";") {
			p.skipTerminators()
			if p.atEOF() || p.opIs("}") {
				return nil
			}
		}
		p.skipTerminators()
		return nil
	}
	return p.err("这一行还没完：%s", p.describe(p.cur()))
}

// ---- 顶层 ----

func (p *parser) atDefStart() bool {
	if p.isName("export") || p.isName("import") {
		return true
	}
	for _, w := range []string{"method", "struct", "class", "interface", "enum", "type", "let", "const", "shared"} {
		if p.isName(w) {
			return true
		}
	}
	return false
}

func (p *parser) parseImport() (*ast.Import, error) {
	tok := p.advance() // import
	imp := &ast.Import{Pos: posOf(tok), PathPos: posOf(tok)}
	if !p.opIs("{") {
		// 整块导入的形式：`import a from "libs/a"`。访问其成员一律写 `a.成员`。
		nameTok, err := p.expectName()
		if err != nil {
			return nil, err
		}
		imp.Package = nameTok.Text
		if !p.eatName("from") {
			return nil, p.err("挂名后面要跟 from")
		}
	} else {
		p.advance() // {
		for !p.opIs("}") {
			nameTok, err := p.expectName()
			if err != nil {
				return nil, err
			}
			nm := ast.ImportName{Name: nameTok.Text, Pos: posOf(nameTok)}
			if p.eatName("as") {
				alias, err := p.expectName()
				if err != nil {
					return nil, err
				}
				nm.Alias = alias.Text
			}
			imp.Names = append(imp.Names, nm)
			if !p.eatOp(",") {
				break
			}
		}
		if err := p.expectOp("}"); err != nil {
			return nil, err
		}
		if !p.eatName("from") {
			return nil, p.err(`花括号后面要跟 from`)
		}
	}
	strTok := p.cur()
	if strTok.Kind != lexer.Str {
		return nil, p.err("from 后面要跟一条带引号的路径")
	}
	p.advance()
	imp.Path = strTok.Text
	imp.PathPos = posOf(strTok)
	return imp, nil
}

func (p *parser) parseDef() (ast.Def, error) {
	export := false
	if p.eatName("export") {
		export = true
	}
	switch {
	case p.isName("method"):
		return p.parseFunc(export, false)
	case p.isName("struct"), p.isName("class"):
		return p.parseObject(export)
	case p.isName("interface"):
		return p.parseInterface(export)
	case p.isName("enum"):
		return p.parseEnum(export)
	case p.isName("type"):
		return p.parseTypeAlias(export)
	case p.isName("let"), p.isName("const"), p.isName("shared"):
		return p.parseDecl(export)
	}
	return nil, p.err("export 后面要跟 method／struct／class／interface／enum／type／let／const")
}

// parseTypeAlias 处理 `type 名字 = 类型`：给一个已经存在的类型另起一个名字。
// 只能写在文件顶层，不能写在函数体内。
//
// en: parseTypeAlias parses `type Name = SomeType`: it gives an existing type
// another name. It may only be written at file top level; there is no such statement inside a
// function body.
func (p *parser) parseTypeAlias(export bool) (*ast.TypeAlias, error) {
	kw := p.advance() // type
	a := &ast.TypeAlias{Pos: posOf(kw), Export: export}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	a.NameText = nameTok.Text
	if err := p.expectOp("="); err != nil {
		return nil, err
	}
	if a.Typ, err = p.parseType(); err != nil {
		return nil, err
	}
	return a, nil
}

func (p *parser) parseFunc(export, isMethod bool) (*ast.Func, error) {
	kw := p.advance() // method
	fn := &ast.Func{Pos: posOf(kw), Export: export, IsMethod: isMethod}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	fn.NameText = nameTok.Text
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	for !p.opIs(")") {
		param, err := p.parseParam()
		if err != nil {
			return nil, err
		}
		fn.Params = append(fn.Params, param)
		if !p.eatOp(",") {
			break
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	if p.eatOp(":") {
		fn.Ret, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}
	body, err := p.parseBraceStmts()
	if err != nil {
		return nil, err
	}
	for _, s := range body {
		fn.Body = append(fn.Body, s)
	}
	return fn, nil
}

func (p *parser) parseParam() (*ast.Param, error) {
	// 参数位置上 `ref` 只接受一次：写第二次时落到 expectName，
	// 报"想要一个名字"（检查阶段再把 `ref` 本身当名字拒绝）。
	isRef := p.eatName("ref")
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	pm := &ast.Param{Pos: posOf(nameTok), NameText: nameTok.Text, Ref: isRef}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	if pm.Typ, err = p.parseType(); err != nil {
		return nil, err
	}
	if p.eatOp("=") {
		if pm.Default, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	return pm, nil
}

func (p *parser) parseObject(export bool) (*ast.Object, error) {
	kw := p.advance()
	obj := &ast.Object{Pos: posOf(kw), Export: export, Kind: kw.Text}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	obj.NameText = nameTok.Text
	if p.eatName("base") {
		b, err := p.parseType()
		if err != nil {
			return nil, err
		}
		obj.Base = typeAsExpr(b)
	}
	if err := p.expectOp("{"); err != nil {
		return nil, err
	}
	p.skipTerminators()
	for !p.opIs("}") {
		switch {
		case p.isName("method"):
			m, err := p.parseFunc(false, true)
			if err != nil {
				return nil, err
			}
			obj.Methods = append(obj.Methods, m)
		default:
			f, err := p.parseField()
			if err != nil {
				return nil, err
			}
			obj.Fields = append(obj.Fields, f)
		}
		if err := p.endOfStmt(); err != nil {
			return nil, err
		}
		p.skipTerminators()
	}
	if err := p.expectOp("}"); err != nil {
		return nil, err
	}
	return obj, nil
}

func (p *parser) parseInterface(export bool) (*ast.Interface, error) {
	kw := p.advance()
	iface := &ast.Interface{Pos: posOf(kw), Export: export, NameText: ""}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	iface.NameText = nameTok.Text
	if p.eatName("base") {
		b, err := p.parseType()
		if err != nil {
			return nil, err
		}
		iface.Base = typeAsExpr(b)
	}
	if err := p.expectOp("{"); err != nil {
		return nil, err
	}
	p.skipTerminators()
	for !p.opIs("}") {
		switch {
		case p.isName("method"):
			tok := p.cur()
			return nil, &Error{Line: tok.Line, Col: tok.Col,
				Msg: "接口里的方法声明不写 method，只写签名"}
		case p.isSignatureAhead():
			m, err := p.parseSignature()
			if err != nil {
				return nil, err
			}
			iface.Methods = append(iface.Methods, m)
		default:
			f, err := p.parseField()
			if err != nil {
				return nil, err
			}
			iface.Fields = append(iface.Fields, f)
		}
		if err := p.endOfStmt(); err != nil {
			return nil, err
		}
		p.skipTerminators()
	}
	if err := p.expectOp("}"); err != nil {
		return nil, err
	}
	return iface, nil
}

// isSignatureAhead 识别接口里的 `add(v:float):float`：名字紧跟左括号是方法声明，
// 名字后面跟冒号是字段。
//
// en: isSignatureAhead picks out `add(v:float):float` inside an interface: a name followed by
// `(` is a method declaration, a name followed by `:` is a field.
func (p *parser) isSignatureAhead() bool {
	return p.cur().Kind == lexer.Name && p.peek(1).Kind == lexer.Op && p.peek(1).Text == "("
}

// parseSignature 是接口里的方法声明：只有名字、参数、返回类型，没有体。
// 可以只写 `add(v:float):float`，前面带 method 也识别。
//
// en: parseSignature is a method declaration inside an interface: only a name, parameters and a
// return type, no body. Writing `add(v:float):float` is enough; a
// leading `method` is accepted too.
func (p *parser) parseSignature() (*ast.Func, error) {
	kw := p.cur()
	if p.isName("method") {
		p.advance()
	} else {
		kw = lexer.Tok{Kind: lexer.Name, Text: "method", Line: kw.Line, Col: kw.Col}
	}
	fn := &ast.Func{Pos: posOf(kw), IsMethod: true}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	fn.NameText = nameTok.Text
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	for !p.opIs(")") {
		pm, err := p.parseParam()
		if err != nil {
			return nil, err
		}
		if pm.Default != nil {
			return nil, &Error{Line: pm.Pos.Line, Col: pm.Pos.Col, Msg: "接口里的方法声明不带默认值"}
		}
		fn.Params = append(fn.Params, pm)
		if !p.eatOp(",") {
			break
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	if p.eatOp(":") {
		if fn.Ret, err = p.parseType(); err != nil {
			return nil, err
		}
	}
	return fn, nil
}

func (p *parser) parseField() (*ast.Field, error) {
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	f := &ast.Field{Pos: posOf(nameTok), NameText: nameTok.Text}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	if f.Typ, err = p.parseType(); err != nil {
		return nil, err
	}
	return f, nil
}

func (p *parser) parseEnum(export bool) (*ast.Enum, error) {
	kw := p.advance()
	e := &ast.Enum{Pos: posOf(kw), Export: export}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	e.NameText = nameTok.Text
	if err := p.expectOp("{"); err != nil {
		return nil, err
	}
	p.skipTerminators()
	for !p.opIs("}") {
		mTok, err := p.expectName()
		if err != nil {
			return nil, err
		}
		m := ast.EnumMember{Pos: posOf(mTok), NameText: mTok.Text}
		if p.eatOp("=") {
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			m.Value = v
		}
		e.Members = append(e.Members, m)
		if !p.eatOp(",") {
			break
		}
		p.skipTerminators()
	}
	// 最后一个成员后面可以换行再写 `}`，先跳过行尾记号。
	p.skipTerminators()
	if err := p.expectOp("}"); err != nil {
		return nil, err
	}
	return e, nil
}

func (p *parser) parseDecl(export bool) (*ast.Decl, error) {
	if p.isName("shared") { // export shared 名:类型 = 值，不写 let／const
		sh := p.advance()
		if !export {
			return nil, &Error{Line: sh.Line, Col: sh.Col, Msg: "shared 必须与 export 一起写"}
		}
		nameTok, err := p.expectName()
		if err != nil {
			return nil, err
		}
		d := &ast.Decl{Pos: posOf(sh), Export: true, Shared: true, NameText: nameTok.Text}
		if err := p.expectOp(":"); err != nil {
			return nil, err
		}
		if d.Typ, err = p.parseType(); err != nil {
			return nil, err
		}
		if err := p.expectOp("="); err != nil {
			return nil, err
		}
		if d.Value, err = p.parseExpr(); err != nil {
			return nil, err
		}
		return d, nil
	}
	kw := p.advance() // let / const
	d := &ast.Decl{Pos: posOf(kw), Export: export, IsConst: kw.Text == "const"}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	d.NameText = nameTok.Text
	if err := p.expectOp(":"); err != nil {
		return nil, &Error{Line: nameTok.Line, Col: nameTok.Col,
			Msg: fmt.Sprintf("声明 %s 必须标类型", nameTok.Text)}
	}
	if d.Typ, err = p.parseType(); err != nil {
		return nil, err
	}
	if err := p.expectOp("="); err != nil {
		return nil, err
	}
	if d.Value, err = p.parseExpr(); err != nil {
		return nil, err
	}
	return d, nil
}

// ---- 类型 ----

func (p *parser) parseType() (ast.Type, error) {
	tok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	t := &ast.NamedType{Pos: posOf(tok), Name: tok.Text}
	// 包里的类型写两个名字：`a.Pair`。这里连续收下点号后面的名字，
	// 超过一层的点交给检查阶段报"只点一层"——比在这里报"想要 ="的报错位置更准确。
	for p.opIs(".") && p.peek(1).Kind == lexer.Name {
		p.advance() // .
		inner := p.advance()
		t.Name = t.Name + "." + inner.Text
	}
	if p.opIs("<") {
		p.advance()
		for {
			arg, err := p.parseType()
			if err != nil {
				return nil, err
			}
			t.Args = append(t.Args, arg)
			if p.eatOp(",") {
				continue
			}
			break
		}
		if err := p.expectCloseAngle(); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// expectCloseAngle 消费一个 `>`。词法层把 `list<dict<string, int>>` 末尾的 `>>` 读成一个右移运算符，
// 在类型位置上按两个尖括号算，所以这里把它拆开。
//
// en: expectCloseAngle consumes one `>`. The lexer reads the trailing `>>` of
// `list<dict<string, int>>` as a single right-shift operator; in a type position it counts as
// two angle brackets, so this is where it gets split apart.
func (p *parser) expectCloseAngle() error {
	if p.opIs(">>") {
		t := p.cur()
		p.toks[p.i] = lexer.Tok{Kind: lexer.Op, Text: ">", Line: t.Line, Col: t.Col}
		rest := append([]lexer.Tok{{Kind: lexer.Op, Text: ">", Line: t.Line, Col: t.Col + 1}}, p.toks[p.i+1:]...)
		p.toks = append(p.toks[:p.i+1], rest...)
		p.advance() // 这一层消费一个 `>`，剩下的那个交给外层
		return nil
	}
	if p.opIs(">") {
		p.advance()
		return nil
	}
	return p.err("尖括号没有完成，看到 %s", p.describe(p.cur()))
}

func typeAsExpr(t ast.Type) ast.Expr {
	if nt, ok := t.(*ast.NamedType); ok {
		return &ast.Name{Pos: nt.Pos, Text: nt.Name}
	}
	return nil
}

func posOf(t lexer.Tok) ast.Pos { return ast.Pos{Line: t.Line, Col: t.Col} }

// ---- 语句 ----

func (p *parser) parseBraceStmts() ([]ast.Stmt, error) {
	if err := p.expectOp("{"); err != nil {
		return nil, err
	}
	var out []ast.Stmt
	p.skipTerminators()
	for !p.opIs("}") {
		if p.atEOF() {
			return nil, p.err("花括号没有完成")
		}
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		if err := p.endOfStmt(); err != nil {
			return nil, err
		}
		p.skipTerminators()
	}
	p.advance() // }
	return out, nil
}

func (p *parser) parseStmt() (ast.Stmt, error) {
	tok := p.cur()
	switch {
	case p.isName("let"), p.isName("const"):
		d, err := p.parseDecl(false)
		return d, err
	case p.isName("if"):
		return p.parseIf()
	case p.isName("while"):
		return p.parseWhile()
	case p.isName("for"):
		return p.parseFor()
	case p.isName("switch"):
		return p.parseSwitch()
	case p.isName("try"):
		return p.parseTry()
	case p.isName("break"):
		p.advance()
		return &ast.Break{Pos: posOf(tok)}, nil
	case p.isName("continue"):
		p.advance()
		return &ast.Continue{Pos: posOf(tok)}, nil
	case p.isName("return"):
		p.advance()
		r := &ast.Return{Pos: posOf(tok)}
		if !p.isTerminator() && !p.opIs("}") && !p.atEOF() {
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			r.Val = v
		}
		return r, nil
	case p.isName("throw"), p.isName("exit"):
		return p.parseThrowExit()
	}

	// 剩下的都是「以表达式开头」的语句：赋值、自增自减、调用
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	switch {
	case p.opIs("="):
		p.advance()
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Assign{Pos: first.Where(), Target: first, Value: v}, nil
	case p.opIs("++"), p.opIs("--"):
		op := p.advance()
		return &ast.Incdec{Pos: first.Where(), What: first, Up: op.Text == "++"}, nil
	}
	return &ast.ExprStmt{Pos: first.Where(), E: first}, nil
}

func (p *parser) parseIf() (ast.Stmt, error) {
	tok := p.advance() // if
	n := &ast.If{Pos: posOf(tok)}
	for {
		if err := p.expectOp("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		body, err := p.parseBraceStmts()
		if err != nil {
			return nil, err
		}
		n.Conds = append(n.Conds, cond)
		n.Bodies = append(n.Bodies, body)
		switch {
		case p.isName("else"):
			p.advance()
			if p.isName("if") {
				// else if：剩余部分由下一层 if 解析
				inner, err := p.parseIf()
				if err != nil {
					return nil, err
				}
				if next, ok := inner.(*ast.If); ok {
					n.Conds = append(n.Conds, next.Conds...)
					n.Bodies = append(n.Bodies, next.Bodies...)
					n.Else = next.Else
					n.HasElse = next.HasElse
				}
				return n, nil
			}
			els, err := p.parseBraceStmts()
			if err != nil {
				return nil, err
			}
			n.Else = els
			n.HasElse = true
			return n, nil
		default:
			return n, nil
		}
	}
}

func (p *parser) parseWhile() (ast.Stmt, error) {
	tok := p.advance()
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	body, err := p.parseBraceStmts()
	if err != nil {
		return nil, err
	}
	return &ast.While{Pos: posOf(tok), Cond: cond, Body: body}, nil
}

func (p *parser) parseFor() (ast.Stmt, error) {
	tok := p.advance() // for
	f := &ast.For{Pos: posOf(tok)}
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	d := &ast.Decl{Pos: posOf(nameTok), NameText: nameTok.Text}
	if err := p.expectOp(":"); err != nil {
		return nil, &Error{Line: nameTok.Line, Col: nameTok.Col, Msg: "循环变量必须标类型"}
	}
	if d.Typ, err = p.parseType(); err != nil {
		return nil, err
	}
	f.Var = d
	if !p.eatName("in") {
		return nil, p.err("for 的循环变量后面要跟 in")
	}
	start := p.cur()
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.eatName("until") {
		f.Ranged = false
		f.From = first
		f.Until, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	} else {
		f.Ranged = true
		f.Iter = first
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	_ = start
	f.Body, err = p.parseBraceStmts()
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (p *parser) parseSwitch() (ast.Stmt, error) {
	tok := p.advance() // switch
	s := &ast.Switch{Pos: posOf(tok)}
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	head, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	s.Head = head
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	if err := p.expectOp("{"); err != nil {
		return nil, err
	}
	p.skipTerminators()
	for !p.opIs("}") {
		c := &ast.Case{}
		if p.isName("default") {
			dt := p.advance()
			c.Pos = posOf(dt)
			c.Labels = nil
			s.HasDefault = true
		} else {
			for {
				label, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if c.Pos.Line == 0 {
					c.Pos = label.Where()
				}
				c.Labels = append(c.Labels, label)
				if !p.eatOp(",") {
					break
				}
			}
		}
		body, err := p.parseBraceStmts()
		if err != nil {
			return nil, err
		}
		c.Body = body
		s.Cases = append(s.Cases, c)
		p.skipTerminators()
	}
	p.advance() // }
	return s, nil
}

func (p *parser) parseTry() (ast.Stmt, error) {
	tok := p.advance() // try
	tr := &ast.Try{Pos: posOf(tok)}
	body, err := p.parseBraceStmts()
	if err != nil {
		return nil, err
	}
	tr.Body = body
	p.skipTerminators()
	if !p.isName("catch") {
		return nil, p.err("try 后面要跟 catch")
	}
	ct := p.advance()
	tr.HasCatch = true
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	codeTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(","); err != nil {
		return nil, err
	}
	msgTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	tr.CodeName = codeTok.Text
	tr.MsgName = msgTok.Text
	_ = ct
	if tr.Catch, err = p.parseBraceStmts(); err != nil {
		return nil, err
	}
	return tr, nil
}

// ---- 表达式：运算符按优先级分层解析，`is` 与 ==、!= 同档 ----

func (p *parser) parseExpr() (ast.Expr, error) { return p.parseOr() }

func (p *parser) parseOr() (ast.Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.opIs("||") {
		op := p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (ast.Expr, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.opIs("&&") {
		op := p.advance()
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseEquality() (ast.Expr, error) {
	left, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	for p.opIs("==") || p.opIs("!=") || p.isName("is") {
		op := p.advance()
		var right ast.Expr
		if op.Kind == lexer.Name { // is 后面是类型
			t, err := p.parseType()
			if err != nil {
				return nil, err
			}
			left = &ast.Test{Pos: posOf(op), What: left, Typ: t}
			continue
		}
		if right, err = p.parseCompare(); err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseCompare() (ast.Expr, error) {
	left, err := p.parseBitOr()
	if err != nil {
		return nil, err
	}
	for p.opIs("<") || p.opIs("<=") || p.opIs(">") || p.opIs(">=") {
		op := p.advance()
		right, err := p.parseBitOr()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseBitOr() (ast.Expr, error)  { return p.parseBitLeft(p.parseBitXor, "|") }
func (p *parser) parseBitXor() (ast.Expr, error) { return p.parseBitLeft(p.parseBitAnd, "^") }
func (p *parser) parseBitAnd() (ast.Expr, error) { return p.parseBitLeft(p.parseShift, "&") }

func (p *parser) parseBitLeft(next func() (ast.Expr, error), ops ...string) (ast.Expr, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for {
		matched := false
		for _, op := range ops {
			if p.opIs(op) {
				t := p.advance()
				right, err := next()
				if err != nil {
					return nil, err
				}
				left = &ast.Binary{Pos: posOf(t), Op: t.Text, Left: left, Right: right}
				matched = true
				break
			}
		}
		if !matched {
			return left, nil
		}
	}
}

func (p *parser) parseShift() (ast.Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.opIs("<<") || p.opIs(">>") {
		op := p.advance()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAdd() (ast.Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.opIs("+") || p.opIs("-") {
		op := p.advance()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseMul() (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.opIs("*") || p.opIs("/") || p.opIs("%") {
		op := p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Pos: posOf(op), Op: op.Text, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (ast.Expr, error) {
	if p.opIs("-") || p.opIs("!") || p.opIs("~") {
		op := p.advance()
		what, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.Unary{Pos: posOf(op), Op: op.Text, What: what}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (ast.Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.opIs("."):
			p.advance()
			nameTok, err := p.expectName()
			if err != nil {
				return nil, err
			}
			m := &ast.Member{Pos: posOf(nameTok), What: left, NameText: nameTok.Text}
			if p.opIs("(") {
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				left = &ast.Call{Pos: m.Pos, Callee: m, Args: args}
			} else {
				left = m
			}
		case p.opIs("["):
			op := p.advance()
			at, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp("]"); err != nil {
				return nil, err
			}
			left = &ast.Index{Pos: posOf(op), What: left, At: at}
		case p.opIs("("):
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			left = &ast.Call{Pos: left.Where(), Callee: left, Args: args}
		case p.isName("as"):
			op := p.advance()
			t, err := p.parseType()
			if err != nil {
				return nil, err
			}
			left = &ast.Convert{Pos: posOf(op), What: left, Typ: t}
		default:
			return left, nil
		}
	}
}

func (p *parser) parseArgs() ([]ast.Expr, error) {
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	var args []ast.Expr
	for !p.opIs(")") {
		a, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if !p.eatOp(",") {
			break
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	t := p.cur()
	switch t.Kind {
	case lexer.Num:
		p.advance()
		v, err := parseNumeral(t.Text)
		if err != nil {
			return nil, &Error{Line: t.Line, Col: t.Col, Msg: err.Error()}
		}
		kind := "int"
		if t.IsFloat {
			kind = "float"
		}
		return &ast.Lit{Pos: posOf(t), Kind: kind, Text: t.Text, Value: v}, nil
	case lexer.Str:
		p.advance()
		return &ast.Lit{Pos: posOf(t), Kind: "string", Text: t.Text}, nil
	case lexer.Char:
		p.advance()
		return &ast.Lit{Pos: posOf(t), Kind: "char", Text: t.Text}, nil
	case lexer.Op:
		switch t.Text {
		case "(":
			p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			return e, nil
		case "{":
			// 空的 `{}` 靠类型标注区分 dict／hashset；`{"a": 1}` 是 dict；`{1, 2}` 是 hashset
			p.advance()
			if p.eatOp("}") {
				return &ast.MapLit{Pos: posOf(t)}, nil
			}
			first, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if p.eatOp(":") {
				m := &ast.MapLit{Pos: posOf(t), Pairs: []ast.MapPair{{Pos: first.Where(), Key: first}}}
				if m.Pairs[0].Value, err = p.parseExpr(); err != nil {
					return nil, err
				}
				for p.eatOp(",") {
					if p.opIs("}") {
						break
					}
					k, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					pair := ast.MapPair{Pos: k.Where(), Key: k}
					if err := p.expectOp(":"); err != nil {
						return nil, err
					}
					if pair.Value, err = p.parseExpr(); err != nil {
						return nil, err
					}
					m.Pairs = append(m.Pairs, pair)
				}
				if err := p.expectOp("}"); err != nil {
					return nil, err
				}
				return m, nil
			}
			set := &ast.SetLit{Pos: posOf(t), Items: []ast.Expr{first}}
			for p.eatOp(",") {
				if p.opIs("}") {
					break
				}
				item, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				set.Items = append(set.Items, item)
			}
			if err := p.expectOp("}"); err != nil {
				return nil, err
			}
			return set, nil
		case "[":
			// `[1, 2]` 是 list，`[]` 是空 list
			p.advance()
			lit := &ast.ListLit{Pos: posOf(t)}
			for !p.opIs("]") {
				item, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				lit.Items = append(lit.Items, item)
				if !p.eatOp(",") {
					break
				}
			}
			if err := p.expectOp("]"); err != nil {
				return nil, err
			}
			return lit, nil
		case "-":
			p.advance()
			what, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &ast.Unary{Pos: posOf(t), Op: "-", What: what}, nil
		}
		return nil, p.err("这里不该出现 %q", t.Text)
	case lexer.Name:
		return p.parseNameStart()
	}
	return nil, p.err("这里该有的不是一个值，实际看到 %s", p.describe(t))
}

// parseNameStart 处理名字开头的表达式：普通名字、true/false/null、`new C`、`C { 字段 }`。
//
// en: parseNameStart handles the expressions that begin with a name: an ordinary name,
// true/false/null, `new C`, `C { fields }`.
func (p *parser) parseNameStart() (ast.Expr, error) {
	t := p.advance()
	switch t.Text {
	case "true", "false", "null":
		return &ast.Lit{Pos: posOf(t), Kind: t.Text}, nil
	case "new":
		nameTok, err := p.expectName()
		if err != nil {
			return nil, err
		}
		n := &ast.New{Pos: posOf(t), Type: nameTok.Text}
		if err := p.expectOp("("); err != nil {
			return nil, err
		}
		for !p.opIs(")") {
			f, err := p.parseNamedValue()
			if err != nil {
				return nil, err
			}
			n.Fields = append(n.Fields, f)
			if !p.eatOp(",") {
				break
			}
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		return n, nil
	case "throw", "exit":
		return nil, &Error{Line: t.Line, Col: t.Col,
			Msg: fmt.Sprintf("%s 是语句，只能自成一条，不能出现在取值的位置", t.Text)}
	}

	// `{` 紧跟在一个具名类型后面，就是 struct 字面量。
	// 判据：花括号里必须是 `}` 或 `名字 : 值`，否则那个 `{` 是语句块开头（switch 的分支按此区分）。
	if p.opIs("{") && p.structLitAhead() {
		lit := &ast.StructLit{Pos: posOf(t), Type: t.Text}
		p.advance()
		p.skipTerminators()
		for !p.opIs("}") {
			f, err := p.parseNamedValue()
			if err != nil {
				return nil, err
			}
			lit.Fields = append(lit.Fields, f)
			if !p.eatOp(",") {
				break
			}
			p.skipTerminators()
		}
		if err := p.expectOp("}"); err != nil {
			return nil, err
		}
		return lit, nil
	}
	return &ast.Name{Pos: posOf(t), Text: t.Text}, nil
}

// structLitAhead 检查花括号里是否为 `}` 或 `名字 : 值`——是才算 struct 字面量，
// 否则那个 `{` 是一个语句块的开头（switch 的分支按此区分）。
//
// en: structLitAhead checks whether the braces hold `}` or `name: value` - only then is it a
// struct literal, otherwise that `{` opens a statement block. This is what tells the branches
// of switch apart.
func (p *parser) structLitAhead() bool {
	if p.peek(1).Kind == lexer.Op && p.peek(1).Text == "}" {
		return true
	}
	if p.peek(1).Kind != lexer.Name {
		return false
	}
	return p.peek(2).Kind == lexer.Op && p.peek(2).Text == ":"
}

func (p *parser) parseNamedValue() (*ast.StructLitField, error) {
	nameTok, err := p.expectName()
	if err != nil {
		return nil, err
	}
	f := &ast.StructLitField{Pos: posOf(nameTok), NameText: nameTok.Text}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	if f.Value, err = p.parseExpr(); err != nil {
		return nil, err
	}
	return f, nil
}

// parseNumeral 把字面量原文算成整数：支持 `_` 分组与 `0x`。
// 小数一律保留原文，交给静态检查层按 float 处理，因为值可能超出 int64 的整数范围。
//
// en: parseNumeral turns the text of a literal into an integer, supporting `_` digit groups and
// `0x`. Decimals keep their original text and are handled as float by the static
// checker, because the value may lie outside the integer range of int64.
func parseNumeral(text string) (int64, error) {
	clean := strings.ReplaceAll(text, "_", "")
	if strings.Contains(clean, ".") {
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return 0, err
		}
		return int64(f), nil
	}
	base := 10
	if strings.HasPrefix(strings.ToLower(clean), "0x") {
		base, clean = 16, clean[2:]
	}
	v, err := strconv.ParseInt(clean, base, 64)
	if err != nil {
		return 0, fmt.Errorf("数字 %s 存不下", text)
	}
	return v, nil
}

// parseThrowExit 读 `throw (码, 话)` 与 `exit (码, 话)`。两者都是语句，
// 只有 parseStmt 在语句开头才调这里；出现在取值位置上由 parseNameStart 拒绝。
//
// en: parseThrowExit reads `throw (code, text)` and `exit (code, text)`. Both are statements,
// so only parseStmt calls this, and only at the start of a statement; appearing
// in a value position they are rejected by parseNameStart.
func (p *parser) parseThrowExit() (ast.Stmt, error) {
	t := p.advance()
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	code, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(","); err != nil {
		return nil, err
	}
	msg, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	at := posOf(t)
	if t.Text == "throw" {
		return &ast.ExprStmt{Pos: at, E: &ast.Throw{Pos: at, Code: code, Msg: msg}}, nil
	}
	return &ast.ExprStmt{Pos: at, E: &ast.Exit{Pos: at, Code: code, Msg: msg}}, nil
}

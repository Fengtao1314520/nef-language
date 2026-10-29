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

// 定义层的检查：四种自定义类型、函数与调用、定义形状的规矩。
//
// en: The definition layer of checking: the four user-defined types, functions and calls,
// and the rules about the shape of definitions.
package check

import (
	"neflang.org/nf/internal/ast"
)

func (c *Checker) checkDef(d ast.Def) {
	switch def := d.(type) {
	case *ast.Object:
		c.checkObject(def)
	case *ast.Interface:
		c.checkInterfaceDef(def)
	case *ast.Enum:
		c.checkEnumDef(def)
	case *ast.Func:
		if !def.IsMethod {
			k := c.key(def.NameText)
			fi := c.funcs[k]
			fi.ret = c.retOf(def)
			fi.params = c.paramsOf(def)
			// 摊开一次就登记结果：别的文件后来引用这个函数时不再摊第二遍——第二次摊开是在
			// **引用它的那一份文件**的上下文里进行的，包里的名字在那个上下文里指向别的定义。
			fi.laidOut = true
			c.funcs[k] = fi
			c.checkFuncBody(fi)
		}
	case *ast.Decl:
		// 顶层的 let／const 在 hoistTopDecls 里登记名字，值在语句流里求值。
	case *ast.TypeAlias:
		// 别名在 resolveTypeAliasDefs 那一趟已全部解析，这一轮没有别的要检查。
	}
}

// fieldHoldsValue 判断这个类型能不能当 `struct` 的字段。
//
// 值类型本来就各拷一份；`string` 不可变，多一个句柄不改变谁能看见什么；另一个 `struct`
// 拷起来是拷它那几个槽位（一个位置一个位置递下去，字段表不许成环，所以递得完）。
// `class`、`object`、三种容器都不行——它们在内存里就是一个句柄，`struct` 的"整份拷贝"
// 拷的正是这 8 个字节，拷完两个持有者指着同一个对象，整份拷贝的语义当场被破坏。
//
// en: fieldHoldsValue says whether a type may be a `struct` field. A value type is copied
// anyway; `string` is immutable, so one more handle changes nobody's view; another `struct`
// copies its slots (position by position, and the field table may not cycle, so the walk
// terminates). `class`, `object` and the three containers are not allowed - in memory each of
// those is just a handle, and a `struct`'s "copy everything" would copy exactly those 8 bytes,
// leaving two holders pointing at one object and breaking the whole-copy semantics on the
// spot.
func fieldHoldsValue(t Type) bool {
	switch t.Kind {
	case Bool, Byte, Unicode, Int, Float, Enum, Str, Struct:
		return true
	}
	return false
}

// hoistTopDecls 先把顶层每个 let／const 的名字与类型登记好，让「先用后写」成立：
// 一个文件里的顶层定义不分先后。值表达式与交出不在这里做——
// 顶层按书写顺序执行，与函数体相同。
//
// en: hoistTopDecls registers the name and type of every top-level let/const first, so "use
// before written" works: top-level definitions in one file are order-independent. Value
// expressions and handover are not done here - the top level runs in the order written, just
// like a function body.
func (c *Checker) hoistTopDecls(f *ast.File) {
	for _, d := range f.Defs {
		dc, ok := d.(*ast.Decl)
		if !ok {
			continue
		}
		t := c.resolveType(dc.Typ, dc.Where())
		c.checkTypeUsable(t, dc.Where())
		if dc.IsConst && !t.ByValue() {
			c.fail(dc.Where(), "const 只能配值类型，这里是 %s", t)
		}
		by := BindGlobalLet
		switch {
		case dc.Shared:
			by = BindShared
		case dc.IsConst:
			by = BindGlobalConst
		}
		c.declareKind(dc.NameText, t, dc.Where(), dc, by)
		// 顶层那些常量登记进全程序的记录：包外面 `greet.LIMIT` 取的就是这一笔。
		c.topVals[c.key(dc.NameText)] = TopValue{
			Typ: t, Export: dc.Export, Shared: dc.Shared, At: dc.Where(), File: c.file.Path, Decl: dc,
		}
		c.hoistedDecl[dc] = true
		c.moved[dc.NameText] = false
	}
}

func (c *Checker) paramsOf(m *ast.Func) []Param {
	var out []Param
	seen := map[string]bool{}
	for _, p := range m.Params {
		c.checkName(p.NameText, p.Pos)
		if seen[p.NameText] {
			c.fail(p.Pos, "参数 %s 重复", p.NameText)
		}
		seen[p.NameText] = true
		t := c.resolveType(p.Typ, p.Pos)
		c.checkTypeUsable(t, p.Pos)
		if p.Ref {
			c.checkRefParam(t, p.Pos)
		}
		if p.Default != nil {
			c.exprExpected(p.Default, t)
			c.checkDefaultShape(p.Default)
		}
		out = append(out, Param{Name: p.NameText, Typ: t, Default: p.Default, Node: p})
	}
	return out
}

// checkRefParam 规定 `ref` 能标在什么样的参数上。两条拒绝：
// ① 值类型标 ref 没有意义，`int`、`float`、`byte`、`unicode`、`struct`、`enum` 传进去本来就是整份拷贝；
// ② 系统给的句柄不允许借用（`Socket`、`Listener`、`Process`）：被调方在借来的句柄上关闭一次，
//
//	调用方那个名字就成了空句柄，之后再访问它是运行期错误 10092。句柄在类型系统里
//	单独列为一档，这里沿用同一档。
//
// en: checkRefParam pins which parameters `ref` may annotate. Two refusals: a value type
// gains nothing, since int, float, byte, unicode, struct and enum already travel as a whole copy;
// and an OS handle (Socket, Listener, Process) may not be borrowed, because closing it once
// inside the callee leaves the caller's name an empty handle, and the next access is runtime
// error 10092. Handles form their own tier in the type system; this follows that same tier.
func (c *Checker) checkRefParam(t Type, pos ast.Pos) {
	if t.ByValue() {
		c.fail(pos, "值类型的参数不许写 ref：%s 传进去本来就是整份拷贝", t)
		return
	}
	switch t.Kind {
	case Socket, Listener, Process:
		c.fail(pos, "%s 是系统给的句柄，不许写 ref：被调方关一次，调用方那个名字就成了空句柄", t)
	}
}

// checkDefaultShape 规定参数默认值能写成什么：
// 只准**字面量、顶层 const 名字，以及这两样的算术**。
//
// 收紧而不是放开的原因：允许任何表达式就得回答三个问题——它在被调方入口求值几次、
// 求出的那块内存归谁、默认值能不能引用后面的参数。语义代价远大于省下的那几行。
// 拒绝的位置在**参数声明处**：若检查放行、到生成阶段才报错（"这一版只认字面量"），
// 等于把一条语言规则说成了后端的缺陷。
//
// en: checkDefaultShape pins what a parameter default may be written as: only a LITERAL,
// a top-level const name, and arithmetic over those two. Why it was tightened rather than
// opened: allowing any expression would force answers to three questions - how many times is
// it evaluated when the callee enters, who owns the block it produces, and may a default refer
// to a later parameter. The semantic cost far outweighs the few lines saved. The refusal sits
// at the PARAMETER DECLARATION: if the checker let it through and only the generator named it
// ("this version accepts literals only"), a language rule would be stated as a backend defect.
func (c *Checker) checkDefaultShape(e ast.Expr) {
	switch x := e.(type) {
	case *ast.Lit:
		if x.Kind == "null" {
			c.fail(x.Where(), "默认值只准写字面量、const 名字和它们的算术，这里是 null")
		}
	case *ast.Name:
		if c.isConstName(x.Text) {
			return
		}
		if imp, ok := c.imported[x.Text]; ok && imp.By == BindGlobalConst {
			return
		}
		c.fail(x.Where(), "默认值只准写字面量、const 名字和它们的算术，%s 不是 const", x.Text)
	case *ast.Unary:
		if x.Op != "-" && x.Op != "+" {
			c.fail(x.Where(), "默认值里只做算术，用不到 %s", x.Op)
		}
		c.checkDefaultShape(x.What)
	case *ast.Binary:
		switch x.Op {
		case "+", "-", "*", "/", "%":
		default:
			c.fail(x.Where(), "默认值里只做算术，%s 不在其中", x.Op)
		}
		c.checkDefaultShape(x.Left)
		c.checkDefaultShape(x.Right)
	default:
		c.fail(e.Where(), "默认值只准写字面量、const 名字和它们的算术")
	}
}

// refuseStructCycle 识别这一个字段是不是把一条 struct 套回了自己：值类型的字段是整块装进来的，
// 甲装乙、乙又装甲，那一块就永远装不完自己。一个环只报一次，报在环上名字最短的那一只 struct
// 闭合它的那个字段上——与类型别名的环同一个报告说法，差别只在别名的环在解析阶段就能看见。
//
// en: refuseStructCycle recognises a field that puts a struct back inside itself: a value
// type's field is stored wholesale, so if A contains B and B contains A, the block can never be
// finished. One cycle is reported once, on the field that closes it, in the struct with the
// shortest name on the cycle - the same wording as a type-alias cycle report, the only
// difference being that the alias check sees its cycle while resolving.
func (c *Checker) refuseStructCycle(owner, through string, at ast.Pos) {
	chain := []string{owner}
	if !c.structReaches(through, owner, &chain) {
		return
	}
	shortest := chain[0]
	for _, n := range chain {
		if n < shortest {
			shortest = n
		}
	}
	if shortest != owner {
		return
	}
	c.fail(at, "struct %s 的字段绕成了环", bareName(owner))
}

// structReaches 从 from 那一只 struct 出发，顺着 struct 类型的字段往下走，走到 target 就是环。
// 遇到走不下去的类型（class、容器、文本）当场停止下探：那个位置里放的是句柄，与整块装得下无关。
// 同一条路径上的每一只 struct 至多访问一次，所以环与菱形都不会让下探无限进行。
//
// en: structReaches walks from the struct `from` down through its struct-typed fields and calls
// it a cycle if it arrives at target. When the walk arrives at a type that cannot be walked
// through (class, containers, text) it stops there: what sits in that position is a handle,
// which has nothing to do with fitting a whole block inside. Each struct on a path is visited
// at most once, so neither a cycle nor a diamond can make the walk run forever.
func (c *Checker) structReaches(from, target string, chain *[]string) bool {
	if from == target {
		*chain = append(*chain, target)
		return true
	}
	for _, n := range *chain {
		if n == from {
			return false
		}
	}
	info := c.layout(from)
	if info == nil || info.kind != Struct {
		return false
	}
	*chain = append(*chain, from)
	for _, f := range info.fields {
		if f.Typ.Kind != Struct {
			continue
		}
		if c.structReaches(f.Typ.Name, target, chain) {
			return true
		}
	}
	*chain = (*chain)[:len(*chain)-1]
	return false
}

func (c *Checker) checkObject(o *ast.Object) {
	ownKey := c.key(o.NameText)
	info := c.layout(ownKey)
	if info == nil {
		return
	}
	// 一个 class 一个名字空间。算进来的是自己的字段、自己的方法，
	// 以及接口 base 一路带下来的字段——接口声明的方法由实现者写一次，不算两个名字。
	seen := map[string]ast.Pos{}
	addName := func(name string, at ast.Pos) {
		c.checkName(name, at)
		if prev, dup := seen[name]; dup {
			c.fail(at, "%s 已经有同名成员（第 %d 行）：字段与方法共用一个名字空间", name, prev.Line)
			return
		}
		seen[name] = at
	}
	for _, f := range o.Fields {
		addName(f.NameText, f.Where())
	}
	for _, m := range o.Methods {
		addName(m.NameText, m.Where())
	}
	if bn := c.baseName(info); bn != "" {
		if b := c.layout(bn); b != nil {
			for _, f := range b.fields {
				addName(f.Name, f.Pos)
			}
		}
	}
	if o.Kind == "struct" && len(o.Methods) > 0 {
		c.fail(o.Methods[0].Where(), "struct 里不写方法")
	}
	// 字段类型：接口名不行；不能指回自己
	for _, f := range o.Fields {
		t := c.resolveType(f.Typ, f.Where())
		c.checkTypeUsable(t, f.Where())
		if t.Name == ownKey {
			c.fail(f.Where(), "字段不能指回自己这个类型")
		}
		// struct 是值类型：甲的字段是乙、乙的字段又绕回甲，那一块就永远装不完自己。
		// 直接自指由上面那条规则拒绝；经由两只以上 struct 绕出的环也在定义处拒绝，
		// 不留到需要整份拷贝时才由后端报"字段绕成了环"。
		if o.Kind == "struct" && t.Kind == Struct {
			c.refuseStructCycle(ownKey, t.Name, f.Where())
		}
		if o.Kind == "struct" && !fieldHoldsValue(t) {
			c.fail(f.Where(), "struct 的字段只能放值类型、string 或另一个 struct，这里是 %s", t)
		}
		// 与容器同一道拒绝：`Json` 没有类型字，字段释放时无法识别它，
		// 不在定义处拒绝的话，到发码阶段才报"后端还没做到这些"。
		if o.Kind == "class" && t.Kind == Json {
			c.fail(f.Where(), "%s 只能待在名字、实参与返回值里，不能当字段", t)
		}
	}
	// base 只能是接口
	if o.Base != nil {
		// 写别名也算：指向接口的那一条在别名的定义处就被拒绝，所以这里解出来的
		// 只会是 struct／class／枚举那几只，报出来的那句"不是接口"指的正是它们。
		bn, bi, ok := c.typeNameFor(baseOf(o.Base), o.Where())
		if !ok {
			c.fail(o.Where(), "base 后面的 %s 不存在", bareName(bn))
		} else if bi.kind != Iface {
			c.fail(o.Where(), "base 后面只能跟接口名，%s 不是接口", bareName(bn))
		} else {
			var own []methodSig
			for _, m := range o.Methods {
				own = append(own, c.signatureOf(m))
			}
			c.checkImpl(o.NameText, own, o.Fields, o.Where(), o.Kind == "struct", bareName(bn), c.layout(bn))
		}
	}
	// 方法体
	for _, m := range o.Methods {
		fi := funcInfo{file: c.file, fn: m, ret: c.retOf(m), params: c.paramsOf(m), method: true, owner: ownKey, key: ownKey + "." + m.NameText}
		c.checkFuncBody(fi)
	}
}

// checkImpl 核对接口方法是否全部写出、参数名是否一模一样，以及 struct 只能继承纯字段接口。
//
// en: checkImpl verifies that every interface method is implemented, that parameter names are
// identical, and that a struct may only inherit a fields-only interface.
func (c *Checker) checkImpl(name string, own []methodSig, ownFields []*ast.Field, at ast.Pos, isStruct bool, baseName string, base *typeInfo) {
	for _, want := range base.methods {
		if isStruct {
			c.fail(at, "%s 是 struct，写不了方法，所以不能继承带方法声明的接口 %s", name, baseName)
			break
		}
		var got *methodSig
		for i := range own {
			if own[i].Name == want.Name {
				got = &own[i]
				break
			}
		}
		if got == nil {
			c.fail(at, "%s 没写出接口 %s 声明的 %s", name, baseName, want.Name)
			continue
		}
		if len(got.Params) != len(want.Params) {
			c.fail(got.Pos, "%s.%s 的参数个数与接口 %s 的声明不一致", name, want.Name, baseName)
			continue
		}
		for i := range want.Params {
			if got.Params[i].Name != want.Params[i].Name {
				c.fail(got.Pos, "%s.%s 的第 %d 个参数该叫 %s",
					name, want.Name, i+1, want.Params[i].Name)
			}
			if !sameType(got.Params[i].Typ, want.Params[i].Typ) {
				c.fail(got.Pos, "%s.%s 的第 %d 个参数类型该是 %s",
					name, want.Name, i+1, want.Params[i].Typ)
			}
			// `ref` 也是签名的一部分：接口那边写了，实现这边就得写；一边写一边不写就是两种调用形状。
			if got.Params[i].takesRef() != want.Params[i].takesRef() {
				c.fail(got.Pos, "%s.%s 的第 %d 个参数 ref 写法与接口 %s 的声明不一致",
					name, want.Name, i+1, baseName)
			}
		}
		if !sameType(got.Ret, want.Ret) {
			c.fail(got.Pos, "%s.%s 的返回类型该是 %s", name, want.Name, want.Ret)
		}
	}
	// 接口带下来的字段自动拥有，体内再写一遍就是重复定义
	for _, f := range base.fields {
		for _, g := range ownFields {
			if g.NameText == f.Name {
				c.fail(g.Where(), "%s 是接口 %s 带下来的字段，自动拥有，体内不用再写一遍", g.NameText, baseName)
			}
		}
	}
}

func (c *Checker) checkInterfaceDef(i *ast.Interface) {
	ownKey := c.key(i.NameText)
	info := c.layout(ownKey)
	if info == nil {
		return
	}
	for _, f := range i.Fields {
		t := c.resolveType(f.Typ, f.Where())
		if t.Name == ownKey {
			c.fail(f.Where(), "接口字段不能指回自己")
		}
	}
	if i.Base != nil {
		bn, bi, ok := c.typeNameFor(baseOf(i.Base), i.Where())
		if !ok {
			c.fail(i.Where(), "base 后面的 %s 不存在", bareName(bn))
		} else if bi.kind != Iface {
			c.fail(i.Where(), "接口只能 base 另一个接口，%s 不是", bareName(bn))
		}
	}
	for _, m := range i.Methods {
		if len(m.Body) > 0 {
			c.fail(m.Where(), "接口里的方法只声明，不带体")
		}
	}
}

func (c *Checker) checkEnumDef(e *ast.Enum) {
	seenName := map[string]bool{}
	seenVal := map[int64]string{}
	next := int64(0)
	for _, m := range e.Members {
		c.checkName(m.NameText, m.Pos)
		if seenName[m.NameText] {
			c.fail(m.Pos, "枚举成员 %s 重复", m.NameText)
		}
		seenName[m.NameText] = true
		var v int64
		if m.Value == nil {
			v = next
		} else {
			lit, ok := m.Value.(*ast.Lit)
			if !ok || lit.Kind != "int" {
				c.fail(m.Pos, "枚举成员的值只能是整数")
				continue
			}
			v = lit.Value
		}
		if old, dup := seenVal[v]; dup {
			c.fail(m.Pos, "枚举成员 %s 与 %s 同值 %d", m.NameText, old, v)
		}
		seenVal[v] = m.NameText
		next = v + 1
		if c.facts != nil {
			c.facts.EnumVals[c.key(e.NameText)+"."+m.NameText] = v
		}
		if v > int64(1)<<62 {
			c.fail(m.Pos, "枚举值超出 int 范围")
		}
	}
}

// namedType 按名字造一个具名类型。class 与 struct 都走这条；查不到就按引用类型处理，
// 那种情况在 collect 阶段已经报过"没有这个类型"。
//
// en: namedType builds a named type from a name; both class and struct come through here. When
// nothing is found it is laid out as a reference type, and that case was already reported as
// "no such type" during collect.
func (c *Checker) namedType(name string) Type {
	if info, ok := c.types[name]; ok {
		return Type{Kind: info.kind, Name: name}
	}
	return Type{Kind: Class, Name: name}
}

func (c *Checker) checkFuncBody(fi funcInfo) {
	m := fi.fn
	prevFunc, prevRet := c.curFunc, c.retType
	c.curFunc = &fi
	c.retType = fi.ret
	c.openScope()
	if fi.method {
		// this 就是接收者那一份句柄；方法体里直接写字段名与写 this.字段名等价
		c.declareKind("this", c.namedType(fi.owner), m.Where(), nil, BindThis)
		// 接收者与标准库那些只读对象同一档处理：只读借用。
		// 能读、能就地改这个对象、能连续调用；把它交出去（return、当参数、装箱）全是编译错误。
		c.borrowed["this"] = borrowReceiver
		for _, f := range c.layout(fi.owner).fields {
			c.declareKind(f.Name, f.Typ, f.Pos, nil, BindField)
		}
	}
	for _, p := range m.Params {
		c.declareKind(p.NameText, c.resolveType(p.Typ, p.Pos), m.Where(), p, BindParam)
		if p.Ref {
			// 标了 ref 的参数与 this、遍历变量同一档：能读、能就地改，拿不到所有权。
			c.borrowed[p.NameText] = borrowParam
		}
	}
	c.openScope() // 函数体是参数之外的另一层作用域，块里的重名按这一层检测
	for _, s := range m.Body {
		c.stmt(s)
	}
	c.closeScope()
	if fi.ret.Kind != NoRet && !c.allPathsReturn(m.Body) {
		c.fail(endPos(m.Body, m.Where()), "%s 标了返回类型 %s，但有一条路没 return", m.NameText, fi.ret)
	}
	c.closeScope()
	delete(c.borrowed, "this")
	for _, p := range m.Params {
		if p.Ref {
			delete(c.borrowed, p.NameText)
		}
	}
	c.curFunc, c.retType = prevFunc, prevRet
}

// allPathsReturn 判断一个语句块是不是每条路都返回值。
// return、throw、exit 都算走到头；if 要两侧都走到头，switch 要每支都走到头，
// 而且要么有 default，要么标签已经把值列全（enum 列全成员、boolean 两个都写）。
//
// en: allPathsReturn decides whether a statement block returns on every path. return,
// throw and exit all count as reaching an end; an if needs both sides, a switch needs every
// branch, and either there is a default or the labels already enumerate every value (all
// members of an enum, both of a boolean).
func (c *Checker) allPathsReturn(body []ast.Stmt) bool {
	for _, s := range body {
		switch st := s.(type) {
		case *ast.Return:
			return true
		case *ast.ExprStmt:
			switch st.E.(type) {
			case *ast.Throw, *ast.Exit:
				return true
			}
		case *ast.If:
			if c.allBranchesReturn(st) {
				return true
			}
		case *ast.Try:
			if c.allPathsReturn(st.Body) && c.allPathsReturn(st.Catch) {
				return true
			}
		case *ast.Switch:
			if (st.HasDefault || c.covered[st]) && c.allCasesReturn(st) {
				return true
			}
		}
	}
	return false
}

// allBranchesReturn 判断一个 if／else if／else 是不是每条路都走到头。
// 没有 else 就不算——那一条路不返回任何值。
//
// en: allBranchesReturn decides whether an if/else if/else chain ends on every path. Without an
// else it does not - that path returns nothing.
func (c *Checker) allBranchesReturn(st *ast.If) bool {
	if !st.HasElse {
		return false
	}
	for _, b := range st.Bodies {
		if !c.allPathsReturn(b) {
			return false
		}
	}
	return c.allPathsReturn(st.Else)
}

func (c *Checker) allCasesReturn(st *ast.Switch) bool {
	for _, cs := range st.Cases {
		if !c.allPathsReturn(cs.Body) {
			return false
		}
	}
	return true
}

func endPos(body []ast.Stmt, def ast.Pos) ast.Pos {
	if len(body) == 0 {
		return def
	}
	return body[len(body)-1].Where()
}

func sameType(a, b Type) bool {
	if a.Kind == ErrT || b.Kind == ErrT {
		return true
	}
	if a.Kind != b.Kind || a.Name != b.Name {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if !sameType(a.Args[i], b.Args[i]) {
			return false
		}
	}
	return true
}

// labelIsConstant 识别 switch 的标签：常量、const 名字、枚举成员。
//
// en: labelIsConstant recognises switch labels: a constant, a const name, or an enum member.
func (c *Checker) labelIsConstant(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Lit:
		return x.Kind == "int" || x.Kind == "float" || x.Kind == "string" || x.Kind == "char" || x.Kind == "true" || x.Kind == "false"
	case *ast.Name:
		return c.consts[x.Text]
	case *ast.Unary:
		return x.Op == "-" && c.labelIsConstant(x.What)
	case *ast.Member:
		if n, ok := x.What.(*ast.Name); ok {
			if _, info, isType := c.typeNameFor(n.Text, x.Where()); isType && info.kind == Enum {
				return true
			}
		}
	}
	return false
}

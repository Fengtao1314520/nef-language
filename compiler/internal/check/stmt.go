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

// 语句层的检查：语句与块、错误处理、带返回类型的 return 与控制流规矩。
//
// en: The statement layer of checking: statements and blocks, error handling, return from
// functions with a declared return type, and the control-flow rules.
package check

import (
	"neflang.org/nf/internal/ast"
)

func (c *Checker) stmt(s ast.Stmt) {
	switch st := s.(type) {
	case *ast.Decl:
		c.declStmt(st)
	case *ast.Assign:
		c.assignStmt(st)
	case *ast.Incdec:
		c.incdecStmt(st)
	case *ast.ExprStmt:
		c.expr(st.E)
	case *ast.If:
		enter := c.snapshotMoved()
		out := c.snapshotMoved()
		for i, cond := range st.Conds {
			// 每条路都从"进入那一刻"的状态出发：上一支里交出掉的名字，
			// 不该让这一支的条件与本体误报（else if 就是靠这一点才成立）。
			c.moved = c.snapshotOf(enter)
			c.exprExpected(cond, typeBool)
			c.runBranch(enter, st.Bodies[i], out)
		}
		if st.HasElse {
			c.moved = c.snapshotOf(enter)
			c.runBranch(enter, st.Else, out)
		}
		c.moved = out
	case *ast.While:
		c.exprExpected(st.Cond, typeBool)
		c.loopBody(st.Body)
	case *ast.For:
		c.forStmt(st)
	case *ast.Switch:
		c.switchStmt(st)
	case *ast.Break:
		if c.loopDepth == 0 {
			c.fail(st.Where(), "break 只能写在循环里")
		}
	case *ast.Continue:
		if c.loopDepth == 0 {
			c.fail(st.Where(), "continue 只能写在循环里")
		}
	case *ast.Return:
		c.returnStmt(st)
	case *ast.Try:
		enter := c.snapshotMoved()
		out := c.snapshotMoved()
		c.runBranch(enter, st.Body, out)
		c.openScope()
		c.declare(st.CodeName, typeInt, st.Where())
		c.declare(st.MsgName, typeStr, st.Where())
		c.runBranch(enter, st.Catch, out)
		c.closeScope()
		c.moved = out
	default:
		c.fail(st.Where(), "认不出的语句")
	}
}

func (c *Checker) block(body []ast.Stmt) {
	c.openScope()
	for _, s := range body {
		c.stmt(s)
	}
	c.closeScope()
}

// branch 跑一条分支：进来时先回到进入前的状态，出去时把这条分支新交出的名字并进外层。
// 各分支都从同一份"进入前状态"出发，所以 if 的一条分支不会污染另一条；
// 而"哪条路都可能已经交出"这件事，在分支之后照旧成立。
//
// en: branch runs one branch: on entry it restores the pre-branch state, on exit it merges the
// names this branch newly handed over into the outer level. Every branch starts from the same
// pre-entry state, so one arm of an if cannot contaminate the other, while "any path may
// already have handed this over" still holds after the branch.
func (c *Checker) branch(body []ast.Stmt) {
	enter := c.snapshotMoved()
	out := c.snapshotMoved()
	c.runBranch(enter, body, out)
	c.moved = out
}

func (c *Checker) runBranch(enter map[string]bool, body []ast.Stmt, out map[string]bool) {
	c.moved = c.snapshotOf(enter)
	c.openScope()
	for _, s := range body {
		c.stmt(s)
	}
	c.closeScope()
	for k, v := range c.moved {
		if v && !enter[k] {
			out[k] = true
		}
	}
}

func (c *Checker) snapshotOf(src map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (c *Checker) snapshotMoved() map[string]bool {
	out := map[string]bool{}
	for k, v := range c.moved {
		out[k] = v
	}
	return out
}

// mergeMoved 把"这条分支新交出的名字"并进外层。before 是进分支之前的状态。
//
// en: mergeMoved folds "the names this branch newly handed over" into the outer level; before
// is the state at branch entry.
func (c *Checker) mergeMoved(before map[string]bool) {
	for k, v := range c.moved {
		if v && !before[k] {
			before[k] = true
		}
	}
	c.moved = before
}

func (c *Checker) loopBody(body []ast.Stmt) {
	c.loopDepth++
	c.branch(body)
	c.loopDepth--
}

func (c *Checker) declStmt(d *ast.Decl) {
	c.checkName(d.NameText, d.Where())
	t := c.resolveType(d.Typ, d.Where())
	c.checkTypeUsable(t, d.Where())
	if d.IsConst && !t.ByValue() {
		c.fail(d.Where(), "const 只能配值类型，这里是 %s", t)
	}
	if d.Value != nil {
		c.exprExpected(d.Value, t)
		c.takeFrom(d.Value, t)
	}
	if d.IsConst {
		c.consts[d.NameText] = true
	}
	if c.hoistedDecl[d] {
		// 顶层的名字已经在 hoistTopDecls 里登记过了，这一遍只跑值与交出
		c.moved[d.NameText] = false
		return
	}
	c.declareNode(d.NameText, t, d.Where(), d)
	c.moved[d.NameText] = false
}

func (c *Checker) assignStmt(a *ast.Assign) {
	if c.refuseMidTake(a.Target, a.Where()) {
		return
	}
	switch target := a.Target.(type) {
	case *ast.Index:
		base := c.expr(target.What)
		switch base.Kind {
		case Str:
			c.fail(a.Where(), "string 不可变，不能改其中一格")
		case List:
			c.exprExpected(target.At, typeInt)
			c.exprExpected(a.Value, base.Args[0])
			c.transferOut(base, target, a.Value)
		case Bytes:
			// `b[i] = 200` 写的就是那一个字节。值那一侧的规则与 `list<byte>` 同一位置相同——
			// 字面量越界在编译期报错，变量得先 `as byte`，不自动转换。
			// 不走 transferOut：那个位置存的永远是值类型，没有"交出去"的记录。
			c.exprExpected(target.At, typeInt)
			c.exprExpected(a.Value, typeByte)
		case Dict:
			c.exprExpected(target.At, base.Args[0])
			c.exprExpected(a.Value, base.Args[1])
			c.transferOut(base, target, a.Value)
		default:
			c.fail(a.Where(), "%s 不能用方括号改写", base)
		}
		return
	case *ast.Member:
		m, ok := c.memberOf(target)
		if !ok {
			return
		}
		if m.kind == memberField {
			c.exprExpected(a.Value, m.typ)
			c.takeFrom(a.Value, m.typ)
			return
		}
		if subj, isPkgConst := c.pkgConstRead(target); isPkgConst {
			// 包交出来的数与本文件的 const 同一条规则，只是名字前面挂着包。
			c.fail(a.Where(), "%s 是 const，声明之后不可改", subj)
			return
		}
		c.fail(a.Where(), "%s 是方法，不能被赋值", target.NameText)
		return
	case *ast.Name:
		v, ok := c.lookup(target.Text)
		if !ok {
			c.fail(target.Where(), "没有 %s 这个名字", target.Text)
			return
		}
		if v.typ.Kind == FuncT {
			c.fail(target.Where(), "%s 是函数名，不能被赋值", target.Text)
			return
		}
		if !c.usable(target) {
			return
		}
		if c.isConstName(target.Text) {
			c.fail(target.Where(), "%s 是 const，声明之后不可改", target.Text)
			return
		}
		c.exprExpected(a.Value, v.typ)
		c.takeFrom(a.Value, v.typ)
		c.moved[target.Text] = false
		return
	}
	c.fail(a.Where(), "赋值左边只能是一个名字、一格字段或一个下标")
}

// refuseMidTake 拒绝"路径中间还要再下一层"的写入与自增。
//
// 落点是最后一个位置时没有问题。要拒绝的是中间那一步：`xs[0][0] = 7`、`a[0].v = 7`、`xs[0][0]++`
// 里的 `xs[0]`、`a[0]` 都是**读走容器的一个位置**，按容器的读写规则，读完就把那个位置清空，
// 于是写落在一块已经不在容器上的副本上（实测 `xs[0][0] = 7;` 之后 `length(xs)` 还是 2、
// 再读 `xs[0][0]` 抛 10092），`++` 要把同一条路径读两遍，第二遍拿到的是空句柄（10092）。
// 判为不支持；要改先绑一个名字：`let row:list<int> = xs[0]; row[0] = 7;`。
// 值类型的那个位置不改变源头，所以 `xs[0].n = 3`、`d["甲"].n = 4` 不在这条规则之内。
//
// en: refuseMidTake refuses an assignment or increment whose path needs to descend another
// level in the middle. A destination that is a final position is fine. What is refused is the
// middle step: in `xs[0][0] = 7`, `a[0].v = 7`, `xs[0][0]++`, the `xs[0]` and `a[0]` each
// READ ONE CONTAINER POSITION, and by the container read rule the read empties that position -
// so the write lands on a copy that is no longer in the container (measured: after
// `xs[0][0] = 7;`, `length(xs)` is still 2 and reading `xs[0][0]` throws 10092), and `++` reads
// the same path twice, the second read receiving an empty handle (10092). It is judged
// unsupported; the rewrite is to bind a name first: `let row:list<int> = xs[0]; row[0] = 7;`.
// A value-typed position does not disturb its source, so `xs[0].n = 3` and `d["a"].n = 4` are
// outside this rule.
func (c *Checker) refuseMidTake(target ast.Expr, at ast.Pos) bool {
	var node ast.Expr
	switch t := target.(type) {
	case *ast.Index:
		node = t.What
	case *ast.Member:
		node = t.What
	default:
		return false
	}
	for {
		switch n := node.(type) {
		case *ast.Index:
			base := c.exprTypeOf(n.What)
			if base.Kind == List || base.Kind == Dict || base.Kind == Set {
				elem := base.Args[len(base.Args)-1]
				if !elem.ByValue() && elem.Kind != Str && elem.Kind != ErrT {
					c.fail(at, "中间那个位置读走就空了，改不到它的下一层：先给它绑一个名字")
					return true
				}
			}
			node = n.What
		case *ast.Member:
			node = n.What
		default:
			return false
		}
	}
}

// transferOut 处理写进容器的那个位置：值类型是拷贝，引用类型的值要交出去。
//
// en: transferOut handles the position written into a container: a value type is copied, a
// reference-typed value is handed over.
func (c *Checker) transferOut(base Type, idx *ast.Index, val ast.Expr) {
	elem := base.Args[len(base.Args)-1]
	if elem.ByValue() {
		return
	}
	if n, ok := val.(*ast.Name); ok {
		c.markMoved(n)
	}
}

func (c *Checker) incdecStmt(s *ast.Incdec) {
	if c.refuseMidTake(s.What, s.Where()) {
		return
	}
	t := c.expr(s.What)
	switch t.Kind {
	case Int, Float, Byte:
	default:
		c.fail(s.Where(), "++ 与 -- 只用在 int、float、byte 上，这里是 %s", t)
	}
	if n, ok := s.What.(*ast.Name); ok {
		if c.isConstName(n.Text) {
			c.fail(s.Where(), "%s 是 const，不能自增减", n.Text)
		}
	}
	if subj, isPkgConst := c.pkgConstRead(s.What); isPkgConst {
		// 包交出来的数也是 const，只是名字前面挂着包。
		c.fail(s.Where(), "%s 是 const，不能自增减", subj)
	}
}

func (c *Checker) forStmt(f *ast.For) {
	c.openScope()
	var elem Type
	if f.Ranged {
		elem = c.iterElem(f.Iter)
	} else {
		c.exprExpected(f.From, typeInt)
		c.exprExpected(f.Until, typeInt)
		elem = typeInt
	}
	// 循环变量的类型标注参与检查：写出来的类型必须就是被遍历的那一样东西的类型，
	// 不符就是编译错误，措辞与赋值、实参、下标同一句原话。标注不参与检查的话，
	// `for (x:string in xs)` 里 `xs` 是 `list<int>` 也会通过检查，跑起来印的是那一个 int。
	want := c.resolveType(f.Var.Typ, f.Var.Where())
	if !sameType(want, elem) {
		w, g := typeMismatchNames(want, elem)
		c.fail(f.Var.Where(), "这里要的是 %s，给的是 %s；类型之间不自动转换", w, g)
	}
	c.checkTypeUsable(elem, f.Var.Where())
	c.declareNode(f.Var.NameText, elem, f.Var.Where(), f.Var)
	if borrow(elem) {
		c.borrowed[f.Var.NameText] = borrowLoop
	}
	c.loopDepth++
	enter := c.snapshotMoved()
	out := c.snapshotMoved()
	// 循环体检查两遍：第一遍的结果并入第二遍，用来发现"这一圈交出了、下一圈又用"
	c.runBranch(enter, f.Body, out)
	c.moved = out
	c.runBranch(out, f.Body, out)
	c.moved = out
	c.loopDepth--
	delete(c.borrowed, f.Var.NameText)
	c.closeScope()
}

// iterElem 算出 `for (v in x)` 里 v 的类型。
//
// en: iterElem computes the type of v in `for (v in x)`.
func (c *Checker) iterElem(iter ast.Expr) Type {
	t := c.exprTypeOf(iter)
	switch t.Kind {
	case List, Set:
		return t.Args[0]
	case Dict:
		return t.Args[0] // dict 只交出键
	case Str:
		return typeUnicode
	case ErrT:
		return errType
	}
	c.fail(iter.Where(), "%s 不能遍历", t)
	return errType
}

// borrow 判遍历交出的那个位置是不是引用类型（要按只读借用处理）。
//
// en: borrow decides whether the position handed out by iteration is a reference type, in which
// case it is treated as a read-only borrow.
func borrow(t Type) bool { return !t.ByValue() && t.Kind != ErrT }

func (c *Checker) switchStmt(s *ast.Switch) {
	head := c.exprTypeOf(s.Head)
	if msg := switchable(head); msg != "" {
		c.fail(s.Where(), "%s", msg)
	}
	seen := map[string]bool{}
	enter := c.snapshotMoved()
	out := c.snapshotMoved()
	for _, cs := range s.Cases {
		for _, l := range cs.Labels {
			if !c.labelIsConstant(l) {
				c.fail(l.Where(), "switch 的标签只能写常量、const 或枚举成员")
			}
			lt := c.exprExpected(l, head)
			_ = lt
			key := labelKey(l)
			if seen[key] {
				c.fail(l.Where(), "这个标签已经出现过")
			}
			seen[key] = true
		}
		c.runBranch(enter, cs.Body, out)
	}
	c.moved = out
	if head.Kind == Enum {
		want := len(c.types[head.Name].enum.Members)
		if len(seen) < want && !s.HasDefault {
			c.fail(s.Where(), "switch 漏了 %s 的成员，又没写 default", bareName(head.Name))
		}
		if len(seen) >= want {
			c.covered[s] = true
		}
	}
	if head.Kind == Bool && allBoolLabels(s.Cases) {
		c.covered[s] = true
	}
	if head.Kind == Bool && s.HasDefault {
		for _, cs := range s.Cases {
			if cs.Labels == nil {
				continue
			}
			if allBoolLabels(s.Cases) {
				c.fail(cs.Pos, "boolean 的两个值都列全了，default 跑不到")
			}
		}
	}
}

func allBoolLabels(cases []*ast.Case) bool {
	have := map[string]bool{}
	for _, cs := range cases {
		for _, l := range cs.Labels {
			if lit, ok := l.(*ast.Lit); ok {
				have[lit.Kind] = true
			}
		}
	}
	return have["true"] && have["false"]
}

func labelKey(e ast.Expr) string {
	if lit, ok := e.(*ast.Lit); ok {
		return lit.Kind + ":" + lit.Text
	}
	if m, ok := e.(*ast.Member); ok {
		return m.NameText
	}
	if n, ok := e.(*ast.Name); ok {
		return "name:" + n.Text
	}
	return "?"
}

func (c *Checker) returnStmt(r *ast.Return) {
	if c.curFunc == nil {
		c.fail(r.Where(), "return 只能写在函数或方法里")
		return
	}
	want := c.curFunc.ret
	if want.Kind == NoRet {
		if r.Val != nil {
			c.fail(r.Where(), "%s 没声明返回类型，return 不能带值", c.curFunc.fn.NameText)
		}
		return
	}
	if r.Val == nil {
		c.fail(r.Where(), "%s 声明了返回类型 %s，return 必须带值", c.curFunc.fn.NameText, want)
		return
	}
	c.exprExpected(r.Val, want)
	// "return 交回引用类型的值同样是交出"这一条落在这里：交出去的那个名字当场不再是持有者。
	// 值类型（`int`、`float`、`boolean`、`byte`、`unicode`、`enum`、`struct`）走 takeFrom
	// 开头那一句就 return 了——那是整份拷贝，原名字照旧能用（与引用类型分支的分工一致）。
	c.takeFrom(r.Val, want)
	// 返回值去向的记录：每一条带值的 return 都给本函数的每个参数记一笔（之后要判断"是不是
	// 每一条路返回的都是这一个参数"），返回的就是参数本身时再给那一个单独记一笔。
	if c.facts == nil {
		return
	}
	for _, p := range c.curFunc.fn.Params {
		c.facts.ValueReturns[p]++
	}
	if n, ok := r.Val.(*ast.Name); ok {
		if b, ok := c.facts.Binds[n]; ok && b.Param != nil {
			c.facts.HandBack[b.Param]++
		}
	}
}

func switchable(t Type) string {
	switch t.Kind {
	case Enum, Int, Byte, Unicode, Bool, Str:
		return ""
	case ErrT:
		return ""
	}
	return "switch 只能判 enum、int、byte、unicode、boolean、string"
}

// takeFrom：把引用类型的值赋给一个名字，就是交出。
// 值类型是拷贝；标准库那些只读借用不走这里。
//
// 来源是 `class` 的一个引用类型字段时，同一笔记录记到字段那个位置上（读完写空）。
//
// en: takeFrom: assigning a reference-typed value to a name IS the handover. A value
// type is copied, and the standard library's read-only borrows do not come through here.
// When the source is a reference-typed field of a `class`, the same entry is recorded
// against the field's position (read it and the field is written empty).
func (c *Checker) takeFrom(v ast.Expr, t Type) {
	if t.ByValue() || t.Kind == ErrT || t.Kind == Null {
		return
	}
	if n, ok := v.(*ast.Name); ok {
		if c.markFieldTaken(n, t) {
			return
		}
		c.markMoved(n)
		return
	}
	c.markFieldTaken(v, t)
}

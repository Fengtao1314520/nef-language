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
		if c.finallyDepth > 0 {
			c.fail(st.Where(), "finally 那一段里不能 break：要跳出循环就写在 try 或 catch 里")
		}
		if c.loopDepth == 0 {
			c.fail(st.Where(), "break 只能写在循环里")
		}
	case *ast.Continue:
		if c.finallyDepth > 0 {
			c.fail(st.Where(), "finally 那一段里不能 continue：要换下一轮就写在 try 或 catch 里")
		}
		if c.loopDepth == 0 {
			c.fail(st.Where(), "continue 只能写在循环里")
		}
	case *ast.Return:
		if c.finallyDepth > 0 {
			c.fail(st.Where(), "finally 那一段里不能 return：要改变出口就写在 try 或 catch 里")
		}
		c.returnStmt(st)
	case *ast.Try:
		enter := c.snapshotMoved()
		out := c.snapshotMoved()
		c.runBranch(enter, st.Body, out)
		if st.HasCatch {
			c.openScope()
			c.declare(st.CodeName, typeInt, st.Where())
			c.declare(st.MsgName, typeStr, st.Where())
			c.declare(st.AtName, typeStr, st.Where())
			if st.TraceName != "" {
				// 第四个名字可以省：写了才有整条调用链的那一份文本，没写就一行也不排。
				c.declare(st.TraceName, typeStr, st.Where())
			}
			c.runBranch(enter, st.Catch, out)
			c.closeScope()
		}
		// 收尾那一段是一条独立的支路：接住块那四个名字在这一段里读不到（那四个只登记在
		// 上面那一个作用域里），这一段里也不能改变出口。四条出口每一条都要先跑完它。
		//
		// en: the finally stretch is a stretch of its own: the four catch names live only in the
		// scope above, and no exit may be changed from inside it. All four exits run it first.
		if st.HasFinally {
			c.finallyDepth++
			c.runBranch(enter, st.Finally, out)
			c.finallyDepth--
		}
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
		c.liveTop[d] = true
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
			// 包导出的数与本文件的 const 同一条规则，只是名字前面带着包名。
			c.fail(a.Where(), "%s 是 const，声明之后不可改", subj)
			return
		}
		c.fail(a.Where(), "%s 是方法，不能被赋值", target.NameText)
		return
	case *ast.Name:
		v, depth, ok := c.lookupAt(target.Text)
		if !ok {
			c.fail(target.Where(), "没有 %s 这个名字", target.Text)
			return
		}
		if v.typ.Kind == FuncT {
			c.fail(target.Where(), "%s 是函数名，不能被赋值", target.Text)
			return
		}
		if !c.usable(target, v.typ) {
			return
		}
		if c.isConstName(target.Text) {
			c.fail(target.Where(), "%s 是 const，声明之后不可改", target.Text)
			return
		}
		if c.facts != nil {
			// 赋值左边这一次使用与读它走同一份记录：编辑器的"找引用"靠它认出写入这一处。
			//
			// en: This use on the left of an assignment is recorded like a read: the editor's
			// find-references recognises the write site through it.
			decl, _ := v.node.(*ast.Decl)
			param, _ := v.node.(*ast.Param)
			c.facts.Binds[target] = Bind{By: v.by, Global: depth == 0, Decl: decl, Param: param, Typ: v.typ, At: v.pos}
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
// 要拒绝的是中间那一步还要再经过一个容器位置、而且那个位置装的是引用类型：`xs[0][0] = 7`、
// `a[0].v = 7`、`a[0].kids[0] = 7`、`xs[0][0]++`。中间那一步读出来的是只读借用，容器那个位置
// 还是持有者，写落在借来的那一份上就是改一份不属于这个名字的值；`++` 还要把同一条路径读两遍。
// 元素是值类型时没有这件事：`xs[0].n = 3`、`d["甲"].n = 4`（`list<struct Rec>` 那种容器）照改，
// 中间那一步是一次字段读的 `b.kids[0] = 7` 也照改。要改内层就分三步走：先摘出来，改完之后
// 再写回容器（`let row:list<int> = xs.remove_at(0); row[0] = 7; xs.add(row);`）。
//
// en: refuseMidTake refuses an assignment or increment whose path needs to descend another
// level in the middle, where that middle step reads one more container position holding a
// reference type: `xs[0][0] = 7`, `a[0].v = 7`, `a[0].kids[0] = 7`, `xs[0][0]++`. That middle
// read yields a read-only borrow while the container keeps holding the value, so writing
// through it changes something this name does not own; `++` reads the same path twice. A
// value-typed element raises no such question: `xs[0].n = 3` and `d["a"].n = 4` (over
// `list<struct Rec>`) still write through, as does `b.kids[0] = 7`, where the middle step is a
// field read. To change the inner one, split it into three steps - take that entry out with
// `xs.remove_at(0)`, change it, then `xs.add(row)` puts it back into the container.
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
					c.fail(at, "中间那个位置读了不拿走，改不到它的下一层：要改就先把它从容器里摘出来")
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

// transferOut 处理写进容器的那个位置：值类型是拷贝，引用类型的值要交出去，文本是共用。
// dict 的键走 putInCell 那一条：可变的那一种（class、容器、句柄）由那个位置接手它，原名当场作废；
// 文本的键是共用那一块，不作废。先前只记值那一笔、键那一本账漏下时，键的名字仍然登记着持有者，
// 作用域尽头就把桶里正在用的那块文本拆掉了——那一条改的是记账，不是把文本也算成交出。
//
// en: transferOut handles the position written into a container: a value type is copied, a
// reference-typed value is handed over, and text is shared. A dict key goes through putInCell:
// the mutable kinds (class, container, handle) ARE taken by that position and their name is
// voided on the spot, while a text key only shares the block and keeps its name. Earlier only the
// value side was booked, which left the key's name registered as a holder - the scope-end
// teardown then dismantled the text block a bucket was using; what that fix changed is the
// ledger, not whether a text counts as handed over.
func (c *Checker) transferOut(base Type, idx *ast.Index, val ast.Expr) {
	if base.Kind == Dict {
		c.putInCell(idx.At, base.Args[0])
	}
	elem := base.Args[len(base.Args)-1]
	if elem.ByValue() || elem.Kind == Str {
		return // 文本那一个位置是共用那一块：原名不作废，读来的那一份也写得进去
	}
	if c.borrowRead[val] {
		c.fail(val.Where(), "读出来的那一份是只读借用，不能放进容器那一个位置：能摘出来的位置用 remove_at、d.take 或 json.take 摘出来")
		return
	}
	if c.markFieldTaken(val, elem) {
		return // 那一个位置接手的是字段里读出来的一份，读完字段写空
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
		// 包导出的数也是 const，只是名字前面带着包名。
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
	// "return 返回引用类型的值同样是交出"这一条落在这里：交出去的那个名字当场不再是持有者。
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

// takeFrom：把一个引用类型的值赋给一个名字，就是交出。
// 值类型是拷贝；标准库那些只读借用不走这里。文本走的是共享：那一个名字不作废、源位置不抹空，
// 所以它在这一条的最前面就回去了。
//
// 来源是容器那个位置或树上那一个位置时读出来的是只读借用，接不进名字。
// 来源是 `class` 的一个可变引用类型字段时，同一笔记录记到字段那个位置上（读完写空）——
// 那一条裁定只把文本那一档挪去共用，可变数据照旧跟着位置走。
//
// en: takeFrom: assigning a reference-typed value to a name IS the handover. A value type is
// copied, and the standard library's read-only borrows do not come through here. Text is shared:
// that name stays alive and the source position keeps its handle, so text turns around at the
// very top of this rule. When the source is a container position or a spot on the tree, the read
// yields a read-only borrow and cannot be taken into a name. When the source is a mutable
// reference-typed field of a `class`, the same entry is recorded against the field's position
// (read it and the field is written empty) - The narrowed rule moved only the text tier over to
// sharing; mutable data still follows the position.
func (c *Checker) takeFrom(v ast.Expr, t Type) {
	if t.ByValue() || t.Kind == ErrT || t.Kind == Null {
		return
	}
	if t.Kind == Str {
		// 文本收到一个名字里是共用那一块：源那一个名字不作废，字段与容器的那一个位置也不抹空，
		// 从那个位置读出来的那一份因此接得进名字。两条路到作用域尽头都不归还那一块
		// （块头里带着共享的记号，见降级那侧的 shareText）。
		return
	}
	if c.borrowRead[v] {
		// 读一个位置拿到的那一份归原来的容器、那棵树或者那一个字段，收到名字里就会有两个持有者。
		c.fail(v.Where(), "读出来的那一份是只读借用，不能收到名字里、写进字段或者交给调用方：能摘出来的位置用 remove_at、d.take 或 json.take 摘出来")
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

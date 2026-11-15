// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// writes.go 给每条函数算一份"哪一个形参在函数体里被改写过"的摘要，并在调用处拿它拦下
// 一种会当场踩空的说法：
//
//	method sink(xs:list<Item>, one:Item) { xs.clear() … }
//	sink(a, a[0])        // a[0] 是借出来的一份，被调方 clear 会把它收回
//
// 判据是"借出来的那一份"与"整只容器"同时交进同一个调用，而被调方会改写容器那一个位置。
// 容器改写的那一刻会释放它里面的元素（`clear`、`remove`、`set` 覆盖、`a[i] = 值`），
// 借出去的那一份就可能当场被收回——从前这件事要到运行期 `@free_guarded` 撞上借用表
// （10097）才发现，现在在检查阶段拒掉。
//
// 摘要按**直接改写**与**转交**两步算：函数体里自己改写形参的那一步记下来；把一个形参
// 原样交给另一个函数、而那个函数在对应位置上会改写，也算（只跟同一份文件里的顶层函数，
// 方法与跨包调用交给运行期那一道护栏）。
//
// en: writes.go gives each function a summary of "which parameter its body rewrites", and uses it
// at call sites to stop one shape that steps on freed memory:
//
//	method sink(xs:list<Item>, one:Item) { xs.clear() … }
//	sink(a, a[0])        // a[0] is borrowed; the callee's clear takes it back
//
// The test is "a borrowed element and the whole container handed into one call, where the callee
// rewrites the container". Rewriting a container releases elements (clear, remove, an overwriting
// set, `a[i] = value`), so the borrowed element may be taken back on the spot - a shape that used
// to surface only at runtime when @free_guarded met the borrow table (10097); it is refused during
// checking now. The summary has two steps, direct writes and forwarding: a body rewriting a
// parameter, and a body handing a parameter to another function that rewrites that position (only
// same-file top-level functions are followed; methods and cross-package calls are left to the
// runtime guard).
package check

import (
	"neflang.org/nf/internal/ast"
)

// writeScan 是一段函数体扫一遍的结果：哪些形参被直接改写，以及途中的转交。
//
// en: writeScan is the result of walking one body: which parameters are rewritten directly, and
// the forwards met on the way.
type writeScan struct {
	params  map[string]int
	written []bool
	calls   []writeForward
	root    string // 这一份文件所在包的根，用来拼被调方的规范名
}

// writeForward 是"把某几个形参原样交给 target"的一条记录；args 里空串表示那一个位置不是形参。
//
// en: writeForward records "these parameters are handed as they are to target"; an empty entry in
// args means that position is not a parameter.
type writeForward struct {
	key  string
	args []string
}

// summarizeWrites 在全部函数的声明收齐之后、查函数体之前算这份摘要。
//
// en: summarizeWrites computes the summary after every declaration is collected and before any
// body is checked.
func (c *Checker) summarizeWrites() {
	c.writes = map[string][]bool{}
	type rec struct {
		key string
		scan *writeScan
	}
	var recs []*rec
	for key, fi := range c.funcs {
		if fi.fn == nil || len(fi.fn.Body) == 0 {
			continue
		}
		sc := &writeScan{params: map[string]int{}, written: make([]bool, len(fi.fn.Params))}
		for i, p := range fi.fn.Params {
			sc.params[p.NameText] = i
		}
		if root, ok := c.prog.packageOf(fi.file.Path); ok {
			sc.root = root
		}
		scanStmts(fi.fn.Body, sc)
		recs = append(recs, &rec{key: key, scan: sc})
	}

	// 先把直接改写那一层放进表里，转交那一层才有得读。
	// en: put the direct-write layer into the table first so the forwarding step has something to
	// read.
	for _, r := range recs {
		c.writes[r.key] = r.scan.written
	}
	// 转交那一层要走到不动为止：`a` 交给 `f`，`f` 又交给 `g`，`g` 改写它，`a` 也算被改写。
	// en: the forwarding step runs to a fixed point: a to f, f to g, and g rewrites it - then a is
	// rewritten too.
	for changed := true; changed; {
		changed = false
		for _, r := range recs {
			for _, fw := range r.scan.calls {
				callee, ok := c.writes[fw.key]
				if !ok {
					continue // 还没算过、或者是标准库／别处的函数
				}
				for i, name := range fw.args {
					if name == "" || i >= len(callee) || !callee[i] {
						continue
					}
					j := r.scan.params[name]
					if !r.scan.written[j] {
						r.scan.written[j] = true
						changed = true
					}
				}
			}
		}
	}
	for _, r := range recs {
		c.writes[r.key] = r.scan.written
		if c.facts != nil {
			// Facts 上那一份是给编辑器与测试查的；检查这一边读的是 Checker 自己的这一张，
			// `nf check` 那一趟不带 Facts 也照样拦得住。
			// en: the copy on Facts is for the editor and tests; checking reads the Checker's own
			// table, so the Facts-less nf check pass refuses the same shapes.
			c.facts.Writes[r.key] = r.scan.written
		}
	}
}

// scanStmts 扫一段语句，把直接改写与转交记进 sc。
//
// en: scanStmts walks one stretch of statements, recording direct writes and forwards into sc.
func scanStmts(list []ast.Stmt, sc *writeScan) {
	for _, s := range list {
		switch x := s.(type) {
		case *ast.Assign:
			// `p = v`、`p[i] = v`、`p.f = v` 三种都算：后两种释放的正是位置里原来那一份。
			// en: all three - `p = v`, `p[i] = v`, `p.f = v` - count; the latter two release what
			// the position held before.
			if n, ok := rootName(x.Target); ok {
				sc.mark(n)
			}
			scanExpr(x.Target, sc)
			scanExpr(x.Value, sc)
		case *ast.Incdec:
			if n, ok := rootName(x.What); ok {
				sc.mark(n)
			}
			scanExpr(x.What, sc)
		case *ast.Decl:
			scanExpr(x.Value, sc)
		case *ast.ExprStmt:
			scanExpr(x.E, sc)
		case *ast.Return:
			scanExpr(x.Val, sc)
		case *ast.If:
			for _, cond := range x.Conds {
				scanExpr(cond, sc)
			}
			for _, body := range x.Bodies {
				scanStmts(body, sc)
			}
			scanStmts(x.Else, sc)
		case *ast.While:
			scanExpr(x.Cond, sc)
			scanStmts(x.Body, sc)
		case *ast.For:
			scanExpr(x.From, sc)
			scanExpr(x.Until, sc)
			scanExpr(x.Iter, sc)
			scanStmts(x.Body, sc)
		case *ast.Switch:
			scanExpr(x.Head, sc)
			for _, cs := range x.Cases {
				for _, l := range cs.Labels {
					scanExpr(l, sc)
				}
				scanStmts(cs.Body, sc)
			}
		case *ast.Try:
			scanStmts(x.Body, sc)
			scanStmts(x.Catch, sc)
			scanStmts(x.Finally, sc)
		}
	}
}

// scanExpr 在表达式里找调用：`p.clear()` 那三条直接改写，裸名字的调用记一条转交。
//
// en: scanExpr finds calls inside expressions: the three rewriting members count directly, and a
// call by bare name records one forward.
func scanExpr(e ast.Expr, sc *writeScan) {
	switch x := e.(type) {
	case nil:
		return
	case *ast.Name, *ast.Lit:
		return
	case *ast.Call:
		if m, ok := x.Callee.(*ast.Member); ok {
			if n, ok := m.What.(*ast.Name); ok {
				switch m.NameText {
				case "clear", "remove", "set":
					sc.mark(n.Text)
				}
			}
		}
		if n, ok := x.Callee.(*ast.Name); ok {
			fw := writeForward{key: qualKey(sc.root, n.Text), args: make([]string, len(x.Args))}
			for i, a := range x.Args {
				if an, ok := a.(*ast.Name); ok {
					if _, is := sc.params[an.Text]; is {
						fw.args[i] = an.Text
					}
				}
			}
			sc.calls = append(sc.calls, fw)
		}
		scanExpr(x.Callee, sc)
		for _, a := range x.Args {
			scanExpr(a, sc)
		}
	case *ast.Member:
		scanExpr(x.What, sc)
	case *ast.Index:
		scanExpr(x.What, sc)
		scanExpr(x.At, sc)
	case *ast.Binary:
		scanExpr(x.Left, sc)
		scanExpr(x.Right, sc)
	case *ast.Unary:
		scanExpr(x.What, sc)
	case *ast.Convert:
		scanExpr(x.What, sc)
	case *ast.Test:
		scanExpr(x.What, sc)
	case *ast.New:
		for _, f := range x.Fields {
			scanExpr(f.Value, sc)
		}
	case *ast.StructLit:
		for _, f := range x.Fields {
			scanExpr(f.Value, sc)
		}
	case *ast.ListLit:
		for _, it := range x.Items {
			scanExpr(it, sc)
		}
	case *ast.SetLit:
		for _, it := range x.Items {
			scanExpr(it, sc)
		}
	case *ast.MapLit:
		for _, p := range x.Pairs {
			scanExpr(p.Key, sc)
			scanExpr(p.Value, sc)
		}
	case *ast.Throw:
		scanExpr(x.Code, sc)
		scanExpr(x.Msg, sc)
	case *ast.Exit:
		scanExpr(x.Code, sc)
		scanExpr(x.Msg, sc)
	}
}

// mark 记一个形参被直接改写；不是形参的名字（局部量、字段）不做处理。
//
// en: mark records one directly rewritten parameter; a name that is not a parameter (a local, a
// field) is ignored.
func (sc *writeScan) mark(name string) {
	if j, ok := sc.params[name]; ok {
		sc.written[j] = true
	}
}

// rootName 取一个赋值目标最里面那个名字：`p`、`p[0]`、`p.f`、`p[0].f` 都取到 `p`。
//
// en: rootName takes the innermost name of an assignment target: p from p, p[0], p.f, p[0].f.
func rootName(e ast.Expr) (string, bool) {
	for {
		switch x := e.(type) {
		case *ast.Name:
			return x.Text, true
		case *ast.Index:
			e = x.What
		case *ast.Member:
			e = x.What
		case *ast.Convert:
			e = x.What
		default:
			return "", false
		}
	}
}

// checkBorrowedArgWrite 在调用处看这一种形状：某一个实参是 `a[...]` 读出来的，
// 另一个实参是同一个名字 `a`，而被调方会改写那一个位置。
//
// en: checkBorrowedArgWrite looks at one call for this shape: an argument is read out as `a[...]`,
// another argument is the same name `a`, and the callee rewrites that position.
func (c *Checker) checkBorrowedArgWrite(call *ast.Call, callee string) {
	writes := c.writes[callee]
	if len(writes) == 0 {
		return
	}
	for i, a := range call.Args {
		borrowed, ok := borrowedFrom(a)
		if !ok {
			continue
		}
		for j, b := range call.Args {
			if j == i {
				continue
			}
			n, ok := b.(*ast.Name)
			if !ok || n.Text != borrowed || j >= len(writes) || !writes[j] {
				continue
			}
			c.fail(call.Args[i].Where(), "%s[...] 借出来的那一份与 %s 一起交了进去，而被调方会改写它：那一份可能当场被收回", borrowed, borrowed)
			return
		}
	}
}

// borrowedFrom 认 `a[...]`：从一只容器里下标读出来的那一份（`a[i][j]` 也认到最外面那个 `a`）。
//
// en: borrowedFrom recognises `a[...]`: a position read out by subscript from a container
// (`a[i][j]` also reaches the outermost a).
func borrowedFrom(e ast.Expr) (string, bool) {
	idx, ok := e.(*ast.Index)
	if !ok {
		return "", false
	}
	for {
		switch x := idx.What.(type) {
		case *ast.Name:
			return x.Text, true
		case *ast.Index:
			idx = x
		case *ast.Convert:
			// `(a as X)[0]` 少见，但读的还是 a 那一份；继续往里找。
			if inner, ok := x.What.(*ast.Index); ok {
				idx = inner
				continue
			}
			return "", false
		default:
			return "", false
		}
	}
}

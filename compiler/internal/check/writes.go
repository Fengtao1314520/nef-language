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
	"strings"

	"neflang.org/nf/internal/ast"
)

// writeScan 是一段函数体扫一遍的结果：哪些形参被直接改写，以及途中的转交。
//
// en: writeScan is the result of walking one body: which parameters are rewritten directly, and
// the forwards met on the way.
type writeScan struct {
	params  map[string]int
	written []bool
	fields  []map[string]bool // 每个形参被改写过的字段路径（如 "xs"、"xs.y"），空集只表示整体写
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
	c.writeFields = map[string][]map[string]bool{}
	type rec struct {
		key  string
		scan *writeScan
	}
	var recs []*rec
	for key, fi := range c.funcs {
		if fi.fn == nil || len(fi.fn.Body) == 0 {
			continue
		}
		sc := &writeScan{params: map[string]int{}, written: make([]bool, len(fi.fn.Params)), fields: make([]map[string]bool, len(fi.fn.Params))}
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
		c.writeFields[r.key] = r.scan.fields
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
					if j >= len(r.scan.fields) {
						continue
					}
					if r.scan.fields[j] == nil {
						r.scan.fields[j] = map[string]bool{}
					}
					for f := range calleeFields(c, fw.key, i) {
						if !r.scan.fields[j][f] {
							r.scan.fields[j][f] = true
							changed = true
						}
					}
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
		c.writeFields[r.key] = r.scan.fields
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
				_, path := fieldPath(x.Target)
				sc.markField(n, path)
			}
			scanExpr(x.Target, sc)
			scanExpr(x.Value, sc)
		case *ast.Incdec:
			if n, ok := rootName(x.What); ok {
				_, path := fieldPath(x.What)
				sc.markField(n, path)
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
			switch m.NameText {
			case "clear", "remove", "set":
				// 接收者既可能是形参本身 (`p.clear()`)，也可能是它挂在字段上的容器
				// (`p.xs.clear()`)——后者释放的正是字段里那一份，要按字段路径记。
				if n, ok := rootName(m.What); ok {
					_, path := fieldPath(m.What)
					sc.markField(n, path)
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

// markField 记一个形参的某个字段路径被改写，例如 `p.xs[0] = v` 记 "xs"、
// `p.xs.y = v` 记 "xs.y"。路径为空时就是整体改写（mark 那一档）。
// 调用处拿它区分同一个对象上的不同字段：`box.xs` 与 `box.ys` 是两处，不能混为一谈。
//
// en: markField records a rewritten field path of a parameter, e.g. "xs" for p.xs[0] = v
// and "xs.y" for p.xs.y = v. An empty path is a whole-parameter write (mark's job). The call
// site uses it to tell different fields of one object apart: box.xs and box.ys are not the
// same location.
func (sc *writeScan) markField(name, path string) {
	if path == "" {
		sc.mark(name)
		return
	}
	if j, ok := sc.params[name]; ok {
		sc.written[j] = true
		if sc.fields[j] == nil {
			sc.fields[j] = map[string]bool{}
		}
		sc.fields[j][path] = true
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

// fieldPath 把 `p.xs[0]`、`p.xs.y`、`p` 这样的表达式归到"最里面那个名字 + 字段路径"：
// 一路剥 Index 与 Convert（读位置不改变是哪只容器），把 Member 的名字按从外到内的顺序接起来。
// `p.xs[0]` 得 ("p", "xs")，`p.xs.y[0]` 得 ("p", "xs.y")，`a[0]` 得 ("a", "")。
//
// en: fieldPath reduces p.xs[0], p.xs.y and p to an innermost name plus a field path: peel Index
// and Convert (reading a position does not change which container it is), then join Member names
// from inside out. p.xs[0] yields ("p", "xs"), p.xs.y[0] yields ("p", "xs.y"), a[0] yields ("a", "").
func fieldPath(e ast.Expr) (string, string) {
	var fields []string
	for {
		switch x := e.(type) {
		case *ast.Index:
			e = x.What
		case *ast.Convert:
			e = x.What
		case *ast.Member:
			fields = append(fields, x.NameText)
			e = x.What
		case *ast.Name:
			for i, j := 0, len(fields)-1; i < j; i, j = i+1, j-1 {
				fields[i], fields[j] = fields[j], fields[i]
			}
			return x.Text, strings.Join(fields, ".")
		default:
			return "", ""
		}
	}
}

// calleeFields 读被调方第 i 个形参被写过的字段路径集合。c.writeFields 只放在 Checker 上，
// 不进 Facts 那 30 个字段——它是检查这一侧的中间结论，后端不需要看。
//
// en: calleeFields reads the rewritten field paths of the callee's i-th parameter. writeFields
// lives on the Checker only, not among the 30 Facts fields: it is a checking-side intermediate
// the backend never needs.
func calleeFields(c *Checker, key string, i int) map[string]bool {
	all := c.writeFields[key]
	if i >= len(all) || all[i] == nil {
		return nil
	}
	return all[i]
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
		// 借出来的那一份：必须是从某个位置读出来的（下标），根名与字段路径都取到。
		if _, ok := a.(*ast.Index); !ok {
			continue
		}
		// 文本是共用档：交出去时那块就标成共用，`clear`、覆盖那几步归还它是空操作，
		// 借出去的那一份不会被收回。这一档不拦（运行期的借用护栏也认这一点）。
		if c.exprTypeOf(a).Kind == Str {
			continue
		}
		borrowRoot, borrowFields := fieldPath(a)
		if borrowRoot == "" {
			continue
		}
		for j, b := range call.Args {
			if j == i || j >= len(writes) || !writes[j] {
				continue
			}
			// 整只容器（或它的某一截）交进去：根名必须同一个，字段路径要相容。
			contRoot, contFields := fieldPath(b)
			if contRoot != borrowRoot {
				continue
			}
			// 被调方在这一位改写过的字段：与实参自身的字段路径拼起来，就是实际被动到的位置。
			// 整体改写（空串）或字段改写都算；拼出来的路径必须是借出那一份路径的前缀——
			// 改写 `b.xs.y` 不会释放 `box.xs[0]`，改写 `b.xs` 或整只 `b` 才会。
			// 字段集合为空表示"整体改写"：覆盖任何字段路径。
			fs := calleeFields(c, callee, j)
			if len(fs) == 0 {
				if !pathPrefix(joinField(contFields, ""), borrowFields) {
					continue
				}
			} else {
				hit := false
				for f := range fs {
					if pathPrefix(joinField(contFields, f), borrowFields) {
						hit = true
						break
					}
				}
				if !hit {
					continue
				}
			}
			c.fail(call.Args[i].Where(), "%s[...] 借出来的那一份与 %s 一起交了进去，而被调方会改写它：那一份可能当场被收回", borrowRoot, borrowRoot)
			return
		}
	}
}

// joinField 把两段字段路径接起来，任一段是空串就取另一段。
//
// en: joinField joins two field paths; either empty segment simply contributes nothing.
func joinField(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "." + b
}

// pathPrefix 判 p 是不是 f 的一段路径前缀（按点号的段界算，不是字符串前缀）：
// "xs" 是 "xs" 与 "xs.y" 的前缀，却不是 "xsy" 的前缀。
//
// en: pathPrefix tests whether p is a segment-wise path prefix of f: "xs" prefixes "xs" and
// "xs.y" but not "xsy".
func pathPrefix(p, f string) bool {
	if p == "" {
		return true
	}
	return f == p || strings.HasPrefix(f, p+".")
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

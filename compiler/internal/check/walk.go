// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// walk.go 只干一件事：把一份文件里写出来的每一次调用按顺序交出去一遍。
//
// 要它的原因是起线程那一处：`thread.start(add_up, 101)` 里被起的那一支函数，它那一个参数收下的是
// 一份交过来的持仓，所以查它那一个函数体之前就得知道
// 它是被起的那一支。签名在 `collect` 那一趟已经登记全了，函数体排在后面，所以这里在两段中间
// 插一趟只看调用的走查。
//
// 走查覆盖 `internal/ast/clone.go` 列全的那两种节点：语句十二种、表达式十六种。认不出的节点
// 静悄悄跳过——这一趟只用来登记，漏一处的后果是那一块内存没人归还，不是生成错的机器码，
// 所以不照泛型复制那样当场炸。
//
// en: walk.go does one thing: hand out every call written in a file, in order. It exists for the
// thread boundary: in `thread.start(add_up, 101)` the function being started receives its argument
// as a holding position handed over (item 127-3.5 keeps item 123's transfer), so its body must be
// checked knowing it is a thread entry. Signatures are all registered by the collect pass and the
// bodies come after, so this scan sits between the two. It covers the twelve statement kinds and
// sixteen expression kinds that internal/ast/clone.go lists. An unrecognised node is skipped in
// silence: this pass only registers, and missing one leaves a block nobody returns rather than
// wrong machine code, so it does not stop on the spot the way the generic copy does.
package check

import "neflang.org/nf/internal/ast"

// walkFileCalls 走一份文件的声明与顶层语句，每碰到一次调用交出去一次。
//
// en: walkFileCalls walks a file's declarations and top-level statements, handing out each call.
func walkFileCalls(f *ast.File, visit func(*ast.Call)) {
	for _, d := range f.Defs {
		switch x := d.(type) {
		case *ast.Func:
			walkFuncCalls(x, visit)
		case *ast.Object:
			for _, m := range x.Methods {
				walkFuncCalls(m, visit)
			}
		}
	}
	walkStmtCalls(f.Stmts, visit)
}

// walkFuncCalls 走一支函数的参数默认值与整条语句流。
//
// en: walkFuncCalls walks one function's parameter defaults and its whole statement list.
func walkFuncCalls(fn *ast.Func, visit func(*ast.Call)) {
	for _, p := range fn.Params {
		walkExprCalls(p.Default, visit)
	}
	walkStmtCalls(fn.Body, visit)
}

func walkStmtCalls(ss []ast.Stmt, visit func(*ast.Call)) {
	for _, s := range ss {
		switch x := s.(type) {
		case *ast.Decl:
			walkExprCalls(x.Value, visit)
		case *ast.Assign:
			walkExprCalls(x.Target, visit)
			walkExprCalls(x.Value, visit)
		case *ast.Incdec:
			walkExprCalls(x.What, visit)
		case *ast.ExprStmt:
			walkExprCalls(x.E, visit)
		case *ast.If:
			for _, c := range x.Conds {
				walkExprCalls(c, visit)
			}
			for _, b := range x.Bodies {
				walkStmtCalls(b, visit)
			}
			walkStmtCalls(x.Else, visit)
		case *ast.While:
			walkExprCalls(x.Cond, visit)
			walkStmtCalls(x.Body, visit)
		case *ast.For:
			if x.Var != nil {
				walkExprCalls(x.Var.Value, visit)
			}
			walkExprCalls(x.From, visit)
			walkExprCalls(x.Until, visit)
			walkExprCalls(x.Iter, visit)
			walkStmtCalls(x.Body, visit)
		case *ast.Switch:
			walkExprCalls(x.Head, visit)
			for _, c := range x.Cases {
				for _, l := range c.Labels {
					walkExprCalls(l, visit)
				}
				walkStmtCalls(c.Body, visit)
			}
		case *ast.Return:
			walkExprCalls(x.Val, visit)
		case *ast.Try:
			walkStmtCalls(x.Body, visit)
			walkStmtCalls(x.Catch, visit)
			walkStmtCalls(x.Finally, visit)
		}
	}
}

func walkExprCalls(e ast.Expr, visit func(*ast.Call)) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *ast.Binary:
		walkExprCalls(x.Left, visit)
		walkExprCalls(x.Right, visit)
	case *ast.Unary:
		walkExprCalls(x.What, visit)
	case *ast.Convert:
		walkExprCalls(x.What, visit)
	case *ast.Test:
		walkExprCalls(x.What, visit)
	case *ast.Call:
		visit(x)
		walkExprCalls(x.Callee, visit)
		for _, a := range x.Args {
			walkExprCalls(a, visit)
		}
	case *ast.Index:
		walkExprCalls(x.What, visit)
		walkExprCalls(x.At, visit)
	case *ast.Member:
		walkExprCalls(x.What, visit)
	case *ast.StructLit:
		for _, f := range x.Fields {
			walkExprCalls(f.Value, visit)
		}
	case *ast.New:
		for _, f := range x.Fields {
			walkExprCalls(f.Value, visit)
		}
	case *ast.ListLit:
		for _, it := range x.Items {
			walkExprCalls(it, visit)
		}
	case *ast.MapLit:
		for _, p := range x.Pairs {
			walkExprCalls(p.Key, visit)
			walkExprCalls(p.Value, visit)
		}
	case *ast.SetLit:
		for _, it := range x.Items {
			walkExprCalls(it, visit)
		}
	case *ast.Throw:
		walkExprCalls(x.Code, visit)
		walkExprCalls(x.Msg, visit)
	case *ast.Exit:
		walkExprCalls(x.Code, visit)
		walkExprCalls(x.Msg, visit)
	}
}

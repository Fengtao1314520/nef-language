// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// thread.go 检查 `thread.start(被起的函数, 那一个实参)` 这一处。
//
// 这一条组函数不能走标准库那一张表的通用路：它返回的类型跟着第一个入参走——那一个函数返回
// 什么，句柄身上就写什么（`Thread<int>`、`Thread<string>`），而 `wait()` 返回的就是那一个类型。
// 通用那一张表里一个签名的返回类型是写死的，装不下这件事。
//
// 三处判据：
//
//	第一个入参要么是一个写出来的顶层函数名，要么是一个 `fn` 类型的名字（两种都收）；
//	被起的那个函数收一个参数、要有返回值；
//	交进去的那一个实参与返回的那一个类型都必须是能跨线程的那几样。
//
// 能跨线程的那几样是值类型（含文本）与 `class` 句柄。白名单的判据是"这一类的归还点按线程
// 各一份定下来没有"：容器、`bytes`、`Json`、盒子、三种系统句柄与 `Thread` 自己都要在归还时
// 按形状表往里递归、或者各带一次系统调用与一次等待，那些归还点今天都还是整个程序一份；
// `fn` 值落在同一档，只有第一个入参那个位置例外（那一次调用不产生任何归还）。
// `export shared` 的名字挡在线程体外：那是整个程序共用的一格，两条线程同时读它正是这一版
// 不打算做的共享可变。
//
// en: thread.go checks thread.start(function-to-start, argument). This group function cannot go
// through the standard-library table: its return type follows its first argument - whatever that
// function returns is what the handle says (Thread<int>, Thread<string>) and what wait() hands
// back. One signature in that table has its return type fixed, which cannot say this.
//
// Three tests: the first argument is either a written top-level function name or a name of an fn
// type (both accepted); the function being started takes one parameter and must return a value;
// and both the argument handed in and the value handed back must be one of the types that may
// cross a thread. Those are the value types (text included) and class handles. The whitelist's
// test is "has this kind's release point been settled per thread": containers, bytes, Json,
// boxes, the three system handles and Thread itself all recurse through the shape table when
// released, or carry a system call or a wait of their own, and those release points are still
// one-per-program today; an fn value lands in the same tier, with the first argument as the only
// opening - that call hands nothing over. A name declared export shared stays outside threads: it
// is one cell for the whole program, and two threads reading it is exactly the shared mutable
// state this version does not offer.

package check

import (
	"neflang.org/nf/internal/ast"
)

// crossThreadOK 说这一个类型能不能交给另一条线程。判据写在文件头那一段，句子写在三个调用点上，
// 各自带上那一个类型的名字——句子留成写死的一整句，报错清单才收得到它。
//
// en: crossThreadOK says whether this type may be handed to another thread. The test is written in
// the header above; the sentence is written out in full at the three call sites, each naming the
// type, so that the error list can collect it.
func crossThreadOK(t Type) bool {
	if t.Kind == Str || t.Kind == Class {
		return true
	}
	return t.ByValue() && t.Kind != FuncVal
}

// threadStart 检查 `thread.start(f, a)`，返回那一张句柄的类型。
//
// en: threadStart checks thread.start(f, a) and returns the handle's type.
func (c *Checker) threadStart(call *ast.Call) Type {
	if len(call.Args) != 2 {
		// 入参个数不对借的是现成那一句（`%s 收 %d 个参数，这里给了 %d 个`），不另写一句。
		c.fail(call.Where(), "%s 收 %d 个参数，这里给了 %d 个", "thread.start", 2, len(call.Args))
		return errType
	}
	sig, vt, ok := c.threadTarget(call.Args[0])
	if !ok {
		return errType
	}
	if len(sig.params) != 1 {
		// 一个参数这一档是运行期那张表的形状：交进去的那一个 64 位值与返回的那一个 64 位值。
		// 多个参数要在那里再摊一层，那一步今天没有做，写在语言里就是"能编出来、跑起来不对"。
		c.fail(call.Args[0].Where(), "%s 收 %d 个参数：起一条线程要的是它只收一个参数",
			c.threadTargetName(call.Args[0]), len(sig.params))
		return errType
	}
	if sig.ret.Kind == NoRet {
		c.fail(call.Args[0].Where(), "%s 没有返回值：句柄身上要写它返回的那一个类型",
			c.threadTargetName(call.Args[0]))
		return errType
	}
	// 实参那一侧按被起的那个函数的参数类型来配：这里没有写着类型的槽位，所以走 ErrT 那一条
	// （跟着实参走的路径），配不上就是现成的那一句"给的是 X，要的是 Y"。
	if !crossThreadOK(sig.params[0].Typ) {
		c.fail(call.Args[1].Where(), "%s 不能交给另一条线程：这一类的归还点还没按线程各一份定下来", sig.params[0].Typ)
	} else {
		c.exprExpected(call.Args[1], sig.params[0].Typ)
		// 交进去的那一个按现成的"交出"规则记账：引用类型交出去之后原来那个名字当场作废，
		// 来自字段的那一种把那个字段写空。`export shared` 的名字与只读借用的那三种
		// 由 markMoved 自己拒（现成的那三句），这一处不必另写句子。
		if !c.markFieldTaken(call.Args[1], sig.params[0].Typ) {
			if n, ok := call.Args[1].(*ast.Name); ok && takesIntoCell(sig.params[0].Typ) {
				c.markMoved(n)
			}
		}
	}
	if !crossThreadOK(sig.ret) {
		c.fail(call.Args[0].Where(), "%s 不能交给另一条线程：这一类的归还点还没按线程各一份定下来", sig.ret)
	}
	// 记这一处读的是那一个函数的入口地址：后端发的是同一个名字，
	// 走的是 `fn` 值那一条现成的路（internal/lower 的 FuncRefs 那一段）。
	// 两样都要记：后端读那一个表达式先看 Types 里有没有类型（没有就报"这一处的类型没记上"），
	// 再看 FuncRefs 里有没有那一个规范名。
	if n, isName := call.Args[0].(*ast.Name); isName {
		if fi, has := c.funcs[c.key(n.Text)]; has && c.facts != nil {
			c.facts.FuncRefs[call.Args[0]] = fi.key
		}
	}
	if c.facts != nil {
		c.facts.Types[call.Args[0]] = vt
	}
	return Type{Kind: Thread, Name: "Thread", Args: []Type{sig.ret}}
}

// threadTarget 取第一个入参那个位置里被起的函数的签名与那份函数类型本身：写出来的函数名与
// `fn` 类型的名字两种都认。认不出（别的类型、别的写法）报的是那一句"这一处要的是..."。
// 第二个返回是 `fn(参数):返回` 那一串——后端读那一个表达式时要按它拿入口地址。
//
// en: threadTarget takes the signature and the function type in the first argument: a written
// function name and a name of an fn type are both accepted. Anything else gets the sentence
// naming what this position wants. The second result is the fn(params):ret string - the backend
// reads that expression through it to obtain the entry address.
func (c *Checker) threadTarget(e ast.Expr) (funcInfo, Type, bool) {
	n, isName := e.(*ast.Name)
	if !isName {
		c.fail(e.Where(), "thread.start 的第一个入参要的是写出来的函数名或一个 fn 名字")
		return funcInfo{}, errType, false
	}
	if fi, has := c.funcs[c.key(n.Text)]; has && fi.fn != nil {
		if len(fi.fn.TypeParams) > 0 {
			// 泛型的顶层函数当被起的那个函数：这一处要的是填好的那一只实例，
			// 而"填什么"在这里没有凭据（那一个实参推不出类型参数），所以按现成那一句拒。
			c.fail(e.Where(), "%s 是泛型，先填尖括号里的类型再交出去", n.Text)
			return funcInfo{}, errType, false
		}
		params := make([]Type, 0, len(fi.params))
		refs := make([]bool, 0, len(fi.params))
		for i, p := range fi.params {
			params = append(params, p.Typ)
			refs = append(refs, i < len(fi.fn.Params) && fi.fn.Params[i].Ref)
		}
		return fi, funcTypeOf(fi.ret, params, refs), true
	}
	v, has := c.lookup(n.Text)
	if !has || v.typ.Kind != FuncVal {
		c.fail(e.Where(), "thread.start 的第一个入参要的是写出来的函数名或一个 fn 名字")
		return funcInfo{}, errType, false
	}
	// `fn` 类型那一串：Args[0] 是返回，往后可依次是参数，借用的那一个位置 Kind 是 Borrow。
	// 借用不能跨线程（那一个位置属于起它的那一条线程的帧），所以带着 ref 的函数类型当场拒。
	ret := v.typ.Args[0]
	params := make([]Param, 0, len(v.typ.Args)-1)
	for _, p := range v.typ.Args[1:] {
		if p.Kind == Borrow {
			// 带着 ref 的那一份函数类型交不进线程：那一个位置属于起它的那一条线程的帧。
			// 这里借的是同一句答复，不另写一句（"一个 fn 名字"里含不带 ref 的意思）。
			c.fail(e.Where(), "thread.start 的第一个入参要的是写出来的函数名或一个 fn 名字")
			return funcInfo{}, errType, false
		}
		params = append(params, Param{Typ: p})
	}
	return funcInfo{key: n.Text, ret: ret, params: params}, v.typ, true
}

// threadTargetName 给报错用：那一个位置写的是哪个名字。
//
// en: threadTargetName is for the error text: which name was written in that position.
func (c *Checker) threadTargetName(e ast.Expr) string {
	if n, ok := e.(*ast.Name); ok {
		return n.Text
	}
	return "这一处"
}

// checkThreadArg 是 `Thread<T>` 写在那个位置上时 T 的判据：与交进线程的那一样白名单。
//
// en: checkThreadArg is the test on T where Thread<T> is written: the same whitelist as what may
// cross a thread.
func (c *Checker) checkThreadArg(t Type, where ast.Pos) {
	if !crossThreadOK(t) {
		c.fail(where, "%s 不能交给另一条线程：这一类的归还点还没按线程各一份定下来", t)
	}
}

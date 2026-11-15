// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// narrow.go 是 `if (o is C)` 那一层类型窄化：在"这一问为真"的那一支里，
// `o` 的已知类型从 `object` 收窄成 `C`，于是 `o.field` 不必再写一次 `as C`。
//
// 窄化是一层叠在名字表上面的临时结论，按**声明节点**认人：同一段里另写的同名 `let`
// 是另一只盒子，不会被外层那一层窄化认错。出了这一支（`}`）就丢掉，
// `else` 那一支也不继承——"这一问为假"说明的是"不是 C"，不是"是别的什么"。
//
// `&&` 链从左往右生效：`o is C && o.n > 0` 里右边那半已经按 `C` 看。
// `||` 不窄化：左边为假时右边才看，那一半说不了 `o` 是什么。
//
// en: narrow.go is the type narrowing of `if (o is C)`: inside the branch where that question
// answers true, o's known type narrows from object to C, so o.field needs no second `as C`.
// Narrowing is a temporary layer over the name table and recognises the declaration NODE: a
// shadowing `let` of the same name is a different box and is never mistaken for the narrowed one.
// It is dropped at the closing brace and the else branch inherits nothing - "this question is
// false" says "not C", not "some other type". A && chain applies left to right, so the right half
// of `o is C && o.n > 0` already sees C; || narrows nothing, since the right half is only reached
// when the left is false and cannot say what o is.
package check

import (
	"neflang.org/nf/internal/ast"
)

// narrowFact 是一条窄化结论：把某一份声明在那个位置上当作 typ 看。
// was 是它原本的静态类型，两个一起比才认得出"中途另写了一个同名的别的东西"。
//
// en: narrowFact is one narrowing: that declaration is seen as typ at this position. was is its
// original static type, and comparing both is what catches "a same-named different thing was
// declared in between".
type narrowFact struct {
	typ  Type
	was  Type
	decl any
}

// pushNarrow 保存当前这一层窄化并返回一份副本：调用方在进入一支时调用它，
// 出这一支时把返回值返回给 popNarrow。复制而不是压栈，是因为这一层随时可能新增一条。
//
// en: pushNarrow saves this layer and returns a copy: the caller takes it on entering a branch
// and gives it back to popNarrow on leaving. A copy rather than a stack, because this layer may
// gain entries at any moment.
func (c *Checker) pushNarrow() map[string]narrowFact {
	saved := make(map[string]narrowFact, len(c.narrow))
	for k, v := range c.narrow {
		saved[k] = v
	}
	return saved
}

// popNarrow 丢掉这一支里新加的那些窄化。
//
// en: popNarrow drops the narrowings added inside that branch.
func (c *Checker) popNarrow(saved map[string]narrowFact) {
	c.narrow = saved
}

// noteNarrow 把一条条件里成立的窄化记进当前这一层。只认 `名字 is 类名`，
// 以及 `&&` 两边各自成立的窄化；`||`、`!` 与别的写法都不记。
//
// en: noteNarrow records the narrowings that hold in one condition into the current layer. It
// only recognises `name is ClassName` and what each side of a && establishes; ||, ! and every
// other shape record nothing.
func (c *Checker) noteNarrow(e ast.Expr) {
	switch x := e.(type) {
	case *ast.Test:
		n, ok := x.What.(*ast.Name)
		if !ok {
			return
		}
		var to Type
		have := false
		if c.facts != nil {
			to, have = c.facts.Tests[x]
		}
		if !have {
			// 只检查、不留 Facts 的那一趟（`nf check`）没有那张表：现场再解一次类型名。
			// en: the pass that only checks and keeps no Facts (nf check) has no such table, so
			// resolve the type name again here.
			to = c.resolveType(x.Typ, x.Where())
		}
		if to.Kind != Class && to.Kind != Struct {
			return
		}
		v, ok := c.lookup(n.Text)
		if !ok || v.node == nil {
			return
		}
		// 只把"装得下"的那一种收窄：本来就是同一只类型的，收窄没有意义（也不出错）。
		c.narrow[n.Text] = narrowFact{typ: to, was: v.typ, decl: v.node}
	case *ast.Binary:
		if x.Op == "&&" {
			c.noteNarrow(x.Left)
			c.noteNarrow(x.Right)
		}
	}
}

// narrowed 说这个名字在这一处能不能按窄化的那一只看：声明节点要对上，原来的类型也要对得上。
//
// en: narrowed asks whether this name may be seen as its narrowed type here: the declaration node
// and the original type both have to match.
func (c *Checker) narrowed(name string, v scopeVar) (Type, bool) {
	nf, ok := c.narrow[name]
	if !ok || v.node == nil || nf.decl != v.node || !sameType(nf.was, v.typ) {
		return Type{}, false
	}
	return nf.typ, true
}

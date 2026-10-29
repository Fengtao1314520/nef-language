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

// 表达式层的检查：类型推断、交出与借用、标准库签名。
//
// en: The expression layer of checking: type inference, handover and borrowing, and
// standard-library signatures.
package check

import (
	"fmt"

	"neflang.org/nf/internal/ast"
)

var (
	typeBool    = Type{Kind: Bool, Name: "boolean"}
	typeInt     = Type{Kind: Int, Name: "int"}
	typeFloat   = Type{Kind: Float, Name: "float"}
	typeByte    = Type{Kind: Byte, Name: "byte"}
	typeUnicode = Type{Kind: Unicode, Name: "unicode"}
	typeStr     = Type{Kind: Str, Name: "string"}
	typeObj     = Type{Kind: Obj, Name: "object"}
	typeNull    = Type{Kind: Null, Name: "null"}
	typeAuto    = Type{Kind: ErrT, Name: "任意"}
	// typeBytes 是内建引用类型 `bytes`。尖括号里不需要参数：
	// 它装的一定是字节，容量在 `bytes.new(容量)` 那一步定下来。
	typeBytes = Type{Kind: Bytes, Name: "bytes"}
	// typeSocket 与 typeListener 是那两类操作系统给的句柄。
	// 它们没有成员表可查的形状：手上只有一个句柄，形状在系统那一侧。
	typeSocket   = Type{Kind: Socket, Name: "Socket"}
	typeListener = Type{Kind: Listener, Name: "Listener"}
	// typeProcess 是操作系统给的那一类东西：手上那一个数是
	// 运行期那一张表的表项（一个子进程有两把句柄，一个名字装不下两把）。
	typeProcess = Type{Kind: Process, Name: "Process"}
)

func (c *Checker) expr(e ast.Expr) Type { return c.infer(e, typeAuto) }

// litInWant 是字面量的 byte 规则：字面量落在需要 byte 的位置上直接按 byte 处理，
// 超出 0～255 才是错。整份表达式那一层（exprExpected）与容器字面量里的每一项共用这一句，
// `let b:byte = 5` 与 `let xs:list<byte> = [5]` 的结论因此一致。
//
// en: litInWant is the byte rule for literals: a literal in a position that wants byte is read
// as a byte directly, and only a value outside 0-255 is an error. The whole-expression layer
// (exprExpected) and every item inside a container literal share this one check, so `let b:byte
// = 5` and `let xs:list<byte> = [5]` cannot reach different conclusions.
func (c *Checker) litInWant(e ast.Expr, want, got Type) Type {
	lit, ok := e.(*ast.Lit)
	if !ok || want.Kind != Byte || got.Kind != Int {
		return got
	}
	if lit.Value < 0 || lit.Value > 255 {
		c.fail(e.Where(), "byte 只能装 0～255，这里是 %d", lit.Value)
	}
	return want
}

// exprExpected 检查一个表达式的类型对不对得上期望值，返回它的实际类型。
//
// en: exprExpected checks an expression's type against the expected one and returns its actual
// type.
func (c *Checker) exprExpected(e ast.Expr, want Type) Type {
	// 空的容器字面量本身不带元素类型，靠左边的类型标注确定
	if want.Kind != ErrT && emptyContainer(e) {
		if c.facts != nil {
			// 这条路绕过了 infer，后端拿不到结论；空字面量的类型就是标注那一份
			c.facts.Types[e] = want
		}
		return want
	}
	got := c.infer(e, want)
	if want.Kind == ErrT || got.Kind == ErrT {
		return got
	}
	if want.Nullable() && got.Kind == Null {
		return got
	}
	if !sameType(got, want) {
		// 字面量落在需要 byte 的位置上直接按 byte 处理，超出 0～255 才是错
		if t := c.litInWant(e, want, got); t.Kind == Byte {
			return t
		}
		w, g := typeMismatchNames(want, got)
		c.fail(e.Where(), "这里要的是 %s，给的是 %s；类型之间不自动转换", w, g)
	}
	if want.Kind == Byte && got.Kind == Int {
		return want
	}
	return got
}

// typeMismatchNames 排出"标注与实际不符"那一句里的两个类型名：打印出来同名而分不清来源的，
// 是两只同名的类型各从一份包里出来，这一处就把各自所属的包一起带上。
// 赋值、实参、下标、`for` 的循环变量都说这一句话，措辞只这一处。
//
// en: typeMismatchNames orders the two type names inside the "annotation does not match the
// value" sentence: when the printed names are identical and indistinguishable, that is two
// same-named types from different packages, so this is where each one's package gets attached.
// Assignment, argument, subscript and the `for` loop variable all say this one sentence, so the
// wording lives in exactly one place.
func typeMismatchNames(want, got Type) (string, string) {
	w, g := want.String(), got.String()
	if w == g {
		w, g = want.Key(), got.Key()
	}
	return w, g
}

// infer 给出一个表达式的类型。want 是上层期望的那一个类型，typeAuto（"任意"）表示这一次推断没有期望类型。
// 结论同时记进 facts，后端生成中间码时不用再推一遍。
//
// en: infer gives the type of an expression. want is the type expected from above, and typeAuto
// ("anything") means this inference has no expected type. The conclusion is recorded in facts,
// so the backend does not infer again when building intermediate code.
func (c *Checker) infer(e ast.Expr, want Type) Type {
	t := c.inferRaw(e, want)
	if c.facts != nil && t.Kind != ErrT {
		c.facts.Types[e] = t
	}
	return t
}

// recordCall 记下这次调用最后落到哪儿，后端直接照它发 CALL。
//
// en: recordCall notes where this call finally lands, so the backend can emit the CALL straight
// from it.
func (c *Checker) recordCall(call *ast.Call, ref CallRef) {
	if c.facts != nil {
		c.facts.Calls[call] = ref
	}
}

func (c *Checker) inferRaw(e ast.Expr, want Type) Type {
	switch x := e.(type) {
	case *ast.Lit:
		return c.lit(x)
	case *ast.Name:
		return c.nameExpr(x)
	case *ast.Binary:
		return c.binary(x)
	case *ast.Unary:
		return c.unary(x)
	case *ast.Call:
		return c.call(x)
	case *ast.Member:
		return c.memberExpr(x, false)
	case *ast.Index:
		return c.indexExpr(x)
	case *ast.Convert:
		return c.convert(x)
	case *ast.Test:
		return c.testExpr(x)
	case *ast.StructLit:
		return c.structLit(x)
	case *ast.New:
		return c.newExpr(x)
	case *ast.ListLit:
		return c.listLit(x, want)
	case *ast.MapLit:
		return c.mapLit(x, want)
	case *ast.SetLit:
		return c.setLit(x, want)
	case *ast.Throw:
		c.throwLike(x.Pos, x.Code, x.Msg, "throw")
		return typeNull
	case *ast.Exit:
		c.throwLike(x.Pos, x.Code, x.Msg, "exit")
		return typeNull
	}
	c.fail(e.Where(), "认不出的表达式")
	return errType
}

// exprTypeOf 只问类型，不做"期望类型"的下行检查，用于先把容器认出来。
//
// en: exprTypeOf asks only for the type, with no downward check against an expected type; it is
// used to identify a container first.
func (c *Checker) exprTypeOf(e ast.Expr) Type { return c.infer(e, typeAuto) }

func (c *Checker) lit(l *ast.Lit) Type {
	switch l.Kind {
	case "int":
		return typeInt
	case "float":
		return typeFloat
	case "string":
		return typeStr
	case "char":
		return typeUnicode
	case "true", "false":
		return typeBool
	case "null":
		return typeNull
	}
	return errType
}

func (c *Checker) nameExpr(n *ast.Name) Type {
	if n.Text == "args" {
		if c.file.Path != c.prog.Entry || c.curFunc != nil {
			c.fail(n.Where(), "args 只在入口文件的顶层写得到")
		}
		t := Type{Kind: List, Name: "list", Args: []Type{typeStr}}
		if _, ok := c.lookup("args"); !ok {
			c.declareKind("args", t, n.Where(), nil, BindArgs)
		}
		if !c.usable(n) {
			return errType
		}
		if c.facts != nil {
			// args 没有对应的声明节点，后端只能靠这个标记找到那一个静态槽
			c.facts.Binds[n] = Bind{By: BindArgs, Global: true, Typ: t}
		}
		return t
	}
	v, depth, ok := c.lookupAt(n.Text)
	if !ok {
		if c.funcs[c.key(n.Text)].fn != nil || c.isKnownFunc(n.Text) {
			c.fail(n.Where(), "%s 是函数名，不能当值传来传去", n.Text)
			return errType
		}
		// 类型名不能当值。别名算类型名：`type Pair = Point` 之后写 Pair 就是写 Point。
		if t, isAlias := c.typeAliasTarget(n.Text, n.Where()); isAlias {
			if t.Kind != ErrT { // 解析失败的那一条在定义处已经报过，这里不重复
				c.fail(n.Where(), "%s 是类型名，不是值（%s）", n.Text, t.Kind)
			}
			return errType
		}
		if info, isType := c.types[c.key(n.Text)]; isType {
			c.fail(n.Where(), "%s 是类型名，不是值（%s）", n.Text, info.kind)
			return errType
		}
		if _, isEnum := c.enumMember(n.Text); isEnum {
			c.fail(n.Where(), "枚举成员要带类型名点出来：%s", n.Text)
			return errType
		}
		if _, imp := c.imported[n.Text]; imp {
			c.markUsed(n.Text)
			return Type{Kind: FuncT, Name: n.Text}
		}
		c.fail(n.Where(), "没有 %s 这个名字", n.Text)
		return errType
	}
	if !c.usable(n) {
		return errType
	}
	c.markUsed(n.Text)
	if c.facts != nil {
		decl, _ := v.node.(*ast.Decl)
		param, _ := v.node.(*ast.Param)
		c.facts.Binds[n] = Bind{By: v.by, Global: depth == 0, Decl: decl, Param: param, Typ: v.typ}
	}
	return v.typ
}

func (c *Checker) isKnownFunc(name string) bool {
	_, ok := c.funcs[c.key(name)]
	return ok
}

func (c *Checker) enumMember(name string) (string, bool) {
	for tn, ti := range c.types {
		if keyOwner(tn) != c.owner() {
			continue // 别的包里的枚举与本文件无关
		}
		if ti.kind != Enum || ti.enum == nil {
			continue
		}
		for _, m := range ti.enum.Members {
			if m.NameText == name {
				return tn, true
			}
		}
	}
	return "", false
}

// usable 检查交出分析：名字被交出去之后再用就是错。
//
// en: usable checks the handover analysis: using a name after it was handed over is an error.
func (c *Checker) usable(n *ast.Name) bool {
	if c.moved[n.Text] {
		c.fail(n.Where(), "%s 已经交出去了，这个名字不能再写", n.Text)
		return false
	}
	return true
}

func (c *Checker) markMoved(n *ast.Name) {
	if c.isShared(n.Text) {
		c.fail(n.Where(), "%s 是 export shared 的名字，只能就地用，不能交出", n.Text)
		return
	}
	if kind := c.borrowed[n.Text]; kind != "" {
		// 三处借用各留一句完整原话（错误清单按整句比对，不能拼）
		switch kind {
		case borrowReceiver:
			c.fail(n.Where(), "this 是方法接收者的只读借用，不能交出去")
		case borrowParam:
			c.fail(n.Where(), "ref 参数 %s 是只读借用，不能交出去", n.Text)
		default:
			c.fail(n.Where(), "遍历交出的 %s 是只读借用，不能交出去", n.Text)
		}
		return
	}
	if c.isConstName(n.Text) {
		c.fail(n.Where(), "%s 是 const，不能交出", n.Text)
		return
	}
	c.moved[n.Text] = true
	c.noteMoved(n)
}

// noteMoved 把"这个槽位把值交出去了"记进交给后端的记录（Facts.Moved 与 Outs）。
// Outs 记的是交出去的次数：Consumed 要求每一次都交到不带走的地方才成立。
//
// en: noteMoved records "this slot handed its value over" in the notes given to the backend
// (Facts.Moved and Outs). Outs counts how many times it was handed over: the Consumed rule
// requires EVERY one of them to go to a place that does not take it away.
func (c *Checker) noteMoved(n *ast.Name) {
	if c.facts == nil {
		return
	}
	sv, ok := c.lookup(n.Text)
	if !ok || sv.node == nil {
		return
	}
	c.facts.Moved[sv.node] = true
	c.facts.Outs[sv.node]++
}

func (c *Checker) isConstName(name string) bool {
	for _, d := range c.file.Defs {
		if dc, ok := d.(*ast.Decl); ok && dc.NameText == name && dc.IsConst {
			return true
		}
	}
	for tn := range c.consts {
		if tn == name && c.consts[name] {
			return true
		}
	}
	return false
}

// fieldPath 给出"哪一个字段"：`this.name`、`h.peer`，也包括方法体里直接写的字段名（`leaf`）。
// 报错要按写出来的样子指认字段；只说"那个字段"，对用户而言分不清是哪一个。
//
// en: fieldPath gives WHICH field: `this.name`, `h.peer`, or a field name written bare inside a
// method body (`leaf`). The error sentence has to name what was written; "that field" alone does
// not tell the reader which one.
func (c *Checker) fieldPath(v ast.Expr) string {
	if cv, ok := v.(*ast.Convert); ok {
		v = cv.What
	}
	switch x := v.(type) {
	case *ast.Member:
		if n, ok := x.What.(*ast.Name); ok {
			return n.Text + "." + x.NameText
		}
		return x.NameText
	case *ast.Name:
		return x.Text
	}
	return "这个位置"
}

// markFieldTaken 钉一条规矩：`class` 的引用类型字段被读出来交给一个新持有者，
// 这一次读就是交出，后端读完把字段那个位置写空——与容器那个位置同一本账。
//
// 只在交出的那五处调用（落到名字、当实参交出去、`return` 出去、装进容器位置、装进盒子）。
// 顺着读一次不算：`console.print(o.i.n)`、`length(o.name)`、`o.list.add(x)` 都不产生新持有者，
// 把那个位置抹空只会把值弄丢。`as` 那一侧要先剥掉：`let x:object = o.f as object` 交出的是
// `o.f`。方法体里直接写的字段名（`return leaf`）也是同一件事，那边是绑到 BindField 的
// `*ast.Name`。
//
// 值类型字段与 struct 的字段不走这一条：那两份是拷贝，没有槽位归属的问题。
//
// en: markFieldTaken pins one rule: when a `class`'s reference-typed field is read out and
// handed to a new holder, that read IS the handover, and the backend writes the field's position
// empty afterwards - the same ledger as a container position. It is called only at the five
// handover sites (into a name, as an argument, through `return`, into a container position, into
// a box). Reading through does not count: `console.print(o.i.n)`, `length(o.name)` and
// `o.list.add(x)` create no new holder, and emptying that position would just lose the value.
// The `as` side is stripped first: in `let x:object = o.f as object` what is handed over is
// `o.f`. A field name written bare in a method body (`return leaf`) is the same thing, where it
// is a `*ast.Name` bound to BindField. Value-typed fields and struct fields do not take this
// path: those two are copies, so there is no question of which holder a slot belongs to.
func (c *Checker) markFieldTaken(v ast.Expr, t Type) bool {
	if !takesIntoCell(t) {
		return false
	}
	for {
		if cv, ok := v.(*ast.Convert); ok {
			v = cv.What
			continue
		}
		break
	}
	switch x := v.(type) {
	case *ast.Member:
		if c.fieldOf[x].Kind != Class {
			return false
		}
		// 借来的那个对象（遍历给的、ref 参数给的）字段不归它支配——与容器那一侧同样拒绝；
		// 方法接收者 this 除外：它可以把自己字段里的值交出去。
		if n, ok := x.What.(*ast.Name); ok {
			switch c.borrowed[n.Text] {
			case borrowLoop:
				c.fail(x.Where(), "遍历交出的 %s 是只读借用，不能把里面的东西交出去", n.Text)
				return false
			case borrowParam:
				c.fail(x.Where(), "ref 参数 %s 是只读借用，不能把里面的东西交出去", n.Text)
				return false
			}
		}
		if c.facts != nil {
			c.facts.Taken[x] = true
		}
		return true
	case *ast.Name:
		sv, ok := c.lookup(x.Text)
		if !ok || sv.by != BindField {
			return false
		}
		if c.facts != nil {
			c.facts.Taken[x] = true
		}
		return true
	}
	return false
}

// ordered 是有确定先后次序的那五个类型。引用类型只能比句柄地址，
// 那个次序随分配顺序变化，同一对值换个写法结果就相反；enum 成员只定义了判等，
// 要按值排序先写 `D.x as int` 再比；boolean 没有定义次序。
//
// en: ordered is the five types with a defined before-and-after. Comparing reference types
// below that would only compare handle addresses, an order that depends on the allocation
// sequence and flips for the same pair of values written differently; enum members only define
// equality - to sort by value write `D.x as int` first; boolean has no defined order.
func ordered(t Type) bool {
	switch t.Kind {
	case Int, Float, Byte, Unicode, Str:
		return true
	}
	return false
}

// refuseOrder 是比大小那一族共同的答复：判等之外，只有上面那五个类型可比。
//
// en: refuseOrder is the shared answer of the ordering-comparison family: apart from equality,
// only those five types above can be compared.
func (c *Checker) refuseOrder(b *ast.Binary, l Type) {
	if b.Op == "==" || b.Op == "!=" || ordered(l) {
		return
	}
	c.fail(b.Where(), "%s 用不到 %s 上：比大小只有 int、float、byte、unicode、string 这五个类型", b.Op, l)
}

func (c *Checker) binary(b *ast.Binary) Type {
	if b.Op == "&&" || b.Op == "||" {
		c.exprExpected(b.Left, typeBool)
		c.exprExpected(b.Right, typeBool)
		return typeBool
	}
	switch b.Op {
	case "==", "!=", "<", "<=", ">", ">=":
		l := c.expr(b.Left)
		r := c.expr(b.Right)
		if l.Kind == Null || r.Kind == Null {
			other := l
			if l.Kind == Null {
				other = r
			}
			if b.Op != "==" && b.Op != "!=" {
				c.fail(b.Where(), "null 只能跟在 ==、!= 后面比，不能比大小")
			} else if other.ByValue() {
				c.fail(b.Where(), "null 只能和引用类型比，这里是 %s", other)
			}
			break
		}
		if l.Kind == ErrT || r.Kind == ErrT {
			break
		}
		if !l.ByValue() && !r.ByValue() && l.Kind != Obj && r.Kind != Obj {
			if !sameType(l, r) && b.Op == "==" || b.Op == "!=" {
				if !sameType(l, r) && !(assignable(l, r) || assignable(r, l)) {
					c.fail(b.Where(), "== 两边类型不一致：%s 与 %s", l, r)
				}
			}
			c.refuseOrder(b, l)
			break
		}
		if !sameType(l, r) {
			c.fail(b.Where(), "比较两边必须是同一类型：%s 与 %s", l, r)
		}
		if l.Kind == Struct && b.Op != "==" && b.Op != "!=" {
			// struct 只判等：它的字段摊成一整块内存，"小于"没有定义，
			// 语言也不规定先比哪个字段；判等规则是逐字段比较
			c.fail(b.Where(), "%s 之间只有 ==、!= 两条，比大小没有这回事", l)
		} else if sameType(l, r) {
			c.refuseOrder(b, l)
		}
	case "+", "-", "*", "/", "%":
		l := c.expr(b.Left)
		r := c.expr(b.Right)
		if b.Op == "+" && l.Kind == Str && r.Kind == Str {
			return typeStr
		}
		if l.Kind == ErrT || r.Kind == ErrT {
			return errType
		}
		if !sameType(l, r) {
			c.fail(b.Where(), "%s 两边必须是同一类型：%s 与 %s", b.Op, l, r)
			return errType
		}
		switch l.Kind {
		case Int, Float, Byte:
		default:
			// 走到这里只剩文本参与 `-`、`*`、`/`、`%` 这一种情况（`+` 拼文本在上面已返回）：
			// 放进去后端只会拿文本的句柄去做算术，实测过这样会让 nf.exe 自身崩溃。
			c.fail(b.Where(), "%s 用不到 %s 上", b.Op, l)
		}
		if l.Kind == Byte {
			return typeInt
		}
		return l
	case "&", "|", "^", "<<", ">>":
		l := c.expr(b.Left)
		r := c.expr(b.Right)
		if b.Op == "<<" || b.Op == ">>" {
			// 移位量只接受 byte。int 装得下九百亿亿，那个数在移位上没有意义，
			// 而硬件会静默把高位掩掉：`1 << 70` 返回 64，看着像算对了。
			// 字面量按"落在要 byte 的位置上按 byte 处理"那条规则收下，所以 `1 << 4` 照样通过。
			lit, isLit := b.Right.(*ast.Lit)
			constLit := isLit && lit.Kind == "int"
			if r.Kind != Byte && r.Kind != ErrT && !constLit {
				c.fail(b.Where(), "移位量只能写 byte 或一个 0～255 的整数，这里是 %s", r)
			}
			limit := int64(63)
			if l.Kind == Byte {
				limit = 7
			}
			if constLit && l.Kind != ErrT && (lit.Value < 0 || lit.Value > limit) {
				c.fail(b.Where(), "移位量只能在 0～%d 之间，这里写了 %d", limit, lit.Value)
			}
			if l.Kind == ErrT {
				return errType
			}
			if l.Kind != Int && l.Kind != Byte && l.Kind != ErrT {
				c.fail(b.Where(), "位运算只在 int 与 byte 上开放")
			}
			if l.Kind == Byte {
				return typeInt
			}
			return l
		}
		if !sameType(l, r) {
			c.fail(b.Where(), "%s 两边必须是同一类型：%s 与 %s", b.Op, l, r)
			return errType
		}
		if l.Kind != Int && l.Kind != Byte && l.Kind != ErrT {
			c.fail(b.Where(), "位运算只在 int 与 byte 上开放")
		}
		if l.Kind == Byte {
			return typeInt
		}
		return l
	default:
		c.fail(b.Where(), "认不出的运算符 %s", b.Op)
	}
	return typeBool
}

func assignable(from, to Type) bool {
	if to.Kind == Obj {
		return true
	}
	if from.Kind == Class && to.Kind == Obj {
		return true
	}
	return false
}

func (c *Checker) unary(u *ast.Unary) Type {
	t := c.expr(u.What)
	switch u.Op {
	case "!":
		c.exprExpected(u.What, typeBool)
		return typeBool
	case "-":
		if t.Kind != Int && t.Kind != Float && t.Kind != Byte && t.Kind != ErrT {
			c.fail(u.Where(), "一元负号只用在 int、float 上，这里是 %s", t)
		}
		if t.Kind == Byte {
			c.fail(u.Where(), "byte 是 0～255 无符号，取负得先 as int")
		}
		return t
	case "~":
		if t.Kind != Int && t.Kind != Byte && t.Kind != ErrT {
			c.fail(u.Where(), "~ 只用在 int 与 byte 上")
		}
		if t.Kind == Byte {
			return typeInt
		}
		return t
	}
	return errType
}

func (c *Checker) memberExpr(m *ast.Member, callPos bool) Type {
	t, ok := c.memberOf(m)
	if !ok {
		return errType
	}
	_ = callPos
	return t.typ
}

type memberKind int

const (
	memberField memberKind = iota
	memberMethod
	memberEnumValue
	memberStd
	// memberPkgFunc 是包里的函数：`a.make(...)`。它与直接叫名字的
	// 用户函数走同一套参数检查与归属判法，只是名字前面挂着包。
	memberPkgFunc
	// memberPkgConst 是包交出来的数：`greet.LIMIT`。它只能读：值是值类型
	// （const 只能配值类型），读出来就是一份数，不参与交出那本账。
	memberPkgConst
)

type member struct {
	takes bool
	// takesVal 是"只有最后一个实参按交出处理"那一种：`d.get(键, 默认)` 的键只读一次，
	// 默认值却是交出去的（用不上就当场析构）。整个都交出的那种用 takes。
	takesVal bool
	kind     memberKind
	typ      Type
	sig      methodSig
	fi       *funcInfo // 只有 memberPkgFunc 带：交回包外那个函数自己的那份结论
}

func (c *Checker) memberOf(m *ast.Member) (member, bool) {
	// 包的挂名走这一侧：`a.make(...)`。挂名不是值，也不是类型名。
	if n, ok := m.What.(*ast.Name); ok {
		if b, isPkg := c.packages[n.Text]; isPkg {
			return c.pkgMember(m, n.Text, b.root)
		}
	}
	// 枚举成员：`D.a` 的左边是一个类型名，不是值。写成别名也算。
	if n, ok := m.What.(*ast.Name); ok {
		dn, info, isType := c.typeNameFor(n.Text, m.Where())
		if isType && info.kind == Enum {
			for _, mem := range info.enum.Members {
				if mem.NameText == m.NameText {
					return member{kind: memberEnumValue, typ: Type{Kind: Enum, Name: dn}}, true
				}
			}
			c.fail(m.Where(), "%s 里没有成员 %s", bareName(dn), m.NameText)
			return member{}, false
		}
	}
	// 标准库的组名：console.print(...)、text.to_int(s)、math.absolute_value(n)……
	if n, ok := m.What.(*ast.Name); ok && isStdGroup(n.Text) {
		c.usedGroup[n.Text] = true
		if sig, ok := stdFunc(n.Text + "." + m.NameText); ok {
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: n.Text + "." + m.NameText},
				sig: methodSig{Name: m.NameText, Params: sig.params, Ret: sig.ret, VarArg: sig.vararg, Poly: sig.poly}}, true
		}
		c.fail(m.Where(), "%s 组里没有 %s", n.Text, m.NameText)
		return member{}, false
	}
	base := c.exprTypeOf(m.What)
	switch base.Kind {
	case Enum:
		info := c.types[base.Name]
		for _, mem := range info.enum.Members {
			if mem.NameText == m.NameText {
				return member{kind: memberEnumValue, typ: base}, true
			}
		}
		c.fail(m.Where(), "%s 里没有成员 %s", bareName(base.Name), m.NameText)
		return member{}, false
	case ErrT:
		return member{}, false
	}
	if base.Kind == Class || base.Kind == Struct {
		info := c.layout(base.Name)
		for _, f := range info.fields {
			if f.Name == m.NameText {
				c.fieldOf[m] = base
				return member{kind: memberField, typ: f.Typ}, true
			}
		}
		for _, mm := range info.methods {
			if mm.Name == m.NameText {
				return member{kind: memberMethod, typ: Type{Kind: FuncT, Name: mm.Name}, sig: mm}, true
			}
		}
		c.fail(m.Where(), "%s 里没有 %s 这个字段或方法", base.Name, m.NameText)
		return member{}, false
	}
	if base.Kind == Iface {
		c.fail(m.Where(), "%s 是接口名，不能当类型写", bareName(base.Name))
		return member{}, false
	}
	if std := stdMember(base, m.NameText); std.typ.Kind != 0 || std.typ.Kind == ErrT {
		if std.typ.Kind != 0 {
			return std, true
		}
	}
	c.fail(m.Where(), "%s 上没有 %s", base, m.NameText)
	return member{}, false
}

// containerGroup 给容器与文本的 Kind 一个能拼进落点名的组名。
// bytes 也拼在这一侧：它身上的 `slice` 与三种容器同一种写法。
//
// en: containerGroup gives the Kind of a container or a piece of text the group name that fits
// into a dotted name. bytes joins on the same side: its `slice` is spelled the same way as the
// three containers'.
func containerGroup(k Kind) string {
	switch k {
	case List:
		return "list"
	case Dict:
		return "dict"
	case Set:
		return "hashset"
	case Str:
		return "string"
	case Bytes:
		return "bytes"
	case Socket:
		return "socket"
	case Listener:
		return "listener"
	case Process:
		return "process"
	}
	return ""
}

// stdMember 给出容器与字符串上的成员。
//
// 它收的是"哪一种形状 + 一个名字"，返回零值的 member 表示"这个类型上没有这个名字"，
// 能否识别全部在这里判定——补全那一条路（complete.go）问的就是它，两处的答复不会分岔。
//
// en: stdMember gives the members of the containers and of strings. It takes "which shape +
// which name", and a zero member means "this type has no such name" - recognition is decided
// entirely here, so the completion path (complete.go), which asks exactly this question, can
// never give a different answer.
func stdMember(base Type, name string) member {
	elem := Type{Kind: ErrT}
	if len(base.Args) > 0 {
		elem = base.Args[0]
	}
	key := elem
	val := elem
	if base.Kind == Dict && len(base.Args) > 1 {
		key, val = base.Args[0], base.Args[1]
	}
	// 成员调用也要有一个能查的落点名，否则 Facts 里记到的 Std 是空串，后端不知道 call 谁。
	group := containerGroup(base.Kind) + "." + name
	switch base.Kind {
	case List, Set, Dict:
		switch name {
		case "add":
			if base.Kind == Dict {
				return member{takes: true, kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: key}, {Typ: val}}}}
			}
			return member{takes: true, kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: elem}}}}
		case "remove":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: key}}}}
		case "contains":
			if base.Kind == List {
				return member{}
			}
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: key}}, Ret: typeBool}}
		case "sort", "index_of":
			if base.Kind != List {
				return member{} // 只有 list 能排序，也只有 list 有"第几个位置"
			}
			if name == "sort" {
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}}
			}
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: elem}}, Ret: typeInt}}
		case "keys", "values":
			if base.Kind != Dict {
				return member{}
			}
			which := key
			if name == "values" {
				which = val
			}
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: Type{Kind: List, Name: "list", Args: []Type{which}}}}
		case "get":
			if base.Kind != Dict {
				return member{}
			}
			// 键只读一次，默认值交出去：用不上就当场析构，用得上就归调用方
			return member{takesVal: true, kind: memberStd, typ: Type{Kind: FuncT, Name: group},
				sig: methodSig{Params: []Param{{Typ: key}, {Typ: val}}, Ret: val}}
		case "first", "last", "clear":
			if base.Kind != List {
				return member{} // 只有 list 有"首元素、末元素、整个清空"这三件事
			}
			if name == "clear" {
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}}
			}
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: elem}}
		case "reverse", "add_all", "to_set", "slice", "insert_at", "remove_at":
			// list 那六条：只有 list 有顺序，才谈得上"第几个位置"与整体反转
			if base.Kind != List {
				return member{}
			}
			switch name {
			case "reverse":
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}}
			case "add_all":
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group},
					sig: methodSig{Params: []Param{{Typ: base}}}}
			case "to_set":
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group},
					sig: methodSig{Ret: Type{Kind: Set, Name: "hashset", Args: []Type{elem}}}}
			case "slice":
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group},
					sig: methodSig{Params: []Param{{Typ: typeInt}, {Typ: typeInt}}, Ret: base}}
			case "insert_at":
				// 位置只读一次，那一样交出去：与 `d.get(键, 默认)` 同一条分派（takesVal）
				return member{takesVal: true, kind: memberStd, typ: Type{Kind: FuncT, Name: group},
					sig: methodSig{Params: []Param{{Typ: typeInt}, {Typ: elem}}}}
			default: // remove_at
				return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group},
					sig: methodSig{Params: []Param{{Typ: typeInt}}, Ret: elem}}
			}
		case "union", "intersect", "difference":
			if base.Kind != Set {
				return member{}
			}
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: base}}, Ret: base}}
		}
	case Str:
		switch name {
		case "to_upper", "to_lower", "trim":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: typeStr}}
		case "split":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeStr}},
				Ret: Type{Kind: List, Name: "list", Args: []Type{typeStr}}}}
		case "contains", "starts_with", "ends_with":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeStr}}, Ret: typeBool}}
		case "index_of":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeStr}}, Ret: typeInt}}
		case "last_index_of":
			// 从末尾往前找（用于切出文件名、扩展名之前的那一段）
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeStr}}, Ret: typeInt}}
		case "trim_start", "trim_end":
			// 只去一侧：`trim` 两侧都去，需要保留行尾缩进时没有别的写法
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: typeStr}}
		case "repeat":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeInt}}, Ret: typeStr}}
		case "to_bytes":
			// 返回的不是 `list<byte>`，而是 `bytes` 那一块连续内存。
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group},
				sig: methodSig{Ret: typeBytes}}
		case "pad_start", "pad_end":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeInt}, {Typ: typeStr}}, Ret: typeStr}}
		case "replace":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeStr}, {Typ: typeStr}}, Ret: typeStr}}
		case "slice":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeInt}, {Typ: typeInt}}, Ret: typeStr}}
		case "to_list":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: Type{Kind: List, Name: "list", Args: []Type{typeUnicode}}}}
		}
	case Bytes:
		// `bytes` 那一档。它身上只有 `slice` 与 `to_base64` 两条成员：长度走 `length(b)` 那一个函数
		// （与三种容器同一条写法），取与放走方括号。
		// 第二个位置是**止**不是长度——与 `list.slice`、`string.slice` 同一条边界（含起不含止）；
		// 同一门语言里两个 slice 各按各的规则计算，代价比多写一步 `止 = 起 + 长` 更大。
		switch name {
		case "slice":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeInt}, {Typ: typeInt}}, Ret: typeBytes}}
		case "to_base64":
			// 与 `text.to_base64` 同一件事，只是源是字节块。返回的一律是文本：base64 写出来
			// 就是供阅读（或写进一段 JSON）的那串字符，不再是另一块字节。
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: typeStr}}
		}
	case Socket:
		// 网络那三条：accept 只有监听句柄有，send／recv_into／close 只有套接字有。
		// 交进去与收进来的都是 `bytes` 那一块：一次到手多少字节不可控，就地填才用得起。
		switch name {
		case "send":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeBytes}}, Ret: typeInt}}
		case "recv_into":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Params: []Param{{Typ: typeBytes}}, Ret: typeInt}}
		case "close":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}}
		}
	case Listener:
		switch name {
		case "accept":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: typeSocket}}
		case "close":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}}
		}
	case Process:
		// 子进程那两条：`wait()` 一直等到那个程序跑完、返回它的退出码，
		// `close()` 是不等待直接回收。名字走到头没人等它，也由语言收一次（与套接字同一条规矩）。
		switch name {
		case "wait":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}, sig: methodSig{Ret: typeInt}}
		case "close":
			return member{kind: memberStd, typ: Type{Kind: FuncT, Name: group}}
		}
	}
	return member{}
}

// memberRange 把那三条成员的范围钉在检查阶段：哪些类型能排序、keys 与 values 交得回什么，
// 都是语言规则，不能拖到发码那一步做成缺了一半的实现。
//
// en: memberRange pins these three member ranges in the checking phase: who may be sorted, and
// what keys and values hand back. Those are language rules and must not be deferred to the emit
// step, where they would silently be half-done.
func (c *Checker) memberRange(call *ast.Call, m *ast.Member, mm member) error {
	// 只看点号左边是一个值的那两种问法：console.print 这一类点号左边是组名，
	// 拿 exprTypeOf 去问它会报"没有 console 这个名字"。
	switch mm.typ.Name {
	case "list.sort":
		recv := c.exprTypeOf(m.What)
		var elem Type
		if len(recv.Args) > 0 {
			elem = recv.Args[0]
		}
		if !sortable(elem) {
			c.fail(call.Where(), "%s 排不了先后：比大小只给 int、float、byte、unicode、string", elem)
		}
	case "dict.keys", "dict.values":
		recv := c.exprTypeOf(m.What)
		var which Type
		if len(recv.Args) > 0 {
			which = recv.Args[0]
		}
		if mm.typ.Name == "dict.values" && len(recv.Args) > 1 {
			which = recv.Args[1]
		}
		if !entryCopyable(which) {
			c.fail(call.Where(), "%s 交不出一份自己的拷贝：%s 那一类东西一交就是两个持有者，只有值类型与文本交得回来", which, which)
		}
	case "hashset.union", "hashset.intersect", "hashset.difference",
		"list.slice", "list.to_set", "list.add_all":
		// 这六条都要在返回来的那一只里另开格子住下，所以是同一条范围：只交得回值类型与文本
		recv := c.exprTypeOf(m.What)
		var elem Type
		if len(recv.Args) > 0 {
			elem = recv.Args[0]
		}
		if !entryCopyable(elem) {
			c.fail(call.Where(), "%s 交不出一份自己的拷贝：%s 那一类东西一交就是两个持有者，只有值类型与文本交得回来", elem, elem)
		}
	}
	return nil
}

// sortable 只看划定"谁能比大小"的那五种类型：排序不另定一套先后。
//
// en: sortable looks only at the five types defined for "who may be compared": sorting does not
// invent a second ordering.
func sortable(t Type) bool {
	switch t.Kind {
	case Int, Float, Byte, Unicode, Str:
		return true
	}
	return false
}

// entryCopyable 是 dict.keys／dict.values 交得回的那两类：值类型与文本。
// struct 不算：整份拷贝时字段里的句柄也跟着多一份，两个持有者会析构同一块。
//
// en: entryCopyable is the two kinds dict.keys and dict.values may hand back: value types and
// text. A struct does not count: copying it whole would also duplicate the handles inside its
// fields, and then two holders would each dismantle the same block.
func entryCopyable(t Type) bool {
	if t.Kind == Str {
		return true
	}
	switch t.Kind {
	case Bool, Int, Float, Byte, Unicode, Enum:
		return true
	}
	return false
}

func (c *Checker) callExprPos(call *ast.Call) ast.Pos { return call.Where() }

func (c *Checker) call(call *ast.Call) Type {
	// 成员式调用：x.f(...)
	if m, ok := call.Callee.(*ast.Member); ok {
		return c.memberCall(call, m)
	}
	// 组函数式：text.to_int(s)、console.print(...)、length(a)、math.absolute_value(n)、throw/exit 在语句里
	name := ""
	if n, ok := call.Callee.(*ast.Name); ok {
		name = n.Text
	}
	if name == "" {
		c.fail(call.Where(), "调用左边只能是一个名字或 点号成员")
		return errType
	}
	if fi, ok := c.funcs[c.key(name)]; ok && fi.fn != nil {
		c.markUsed(name)
		// 记定义处的规范名：`import { f as g }` 与包里的名字都不该让同一个函数体拿到两个 IR 名字。
		c.recordCall(call, CallRef{Kind: CallUser, Func: fi.key})
		return c.userCall(call, c.funcSig(c.key(name), fi), name)
	}
	if t, ok := stdFunc(name); ok {
		c.recordCall(call, CallRef{Kind: CallStd, Std: name})
		return c.stdCall(call, name, t)
	}
	if _, _, isType := c.typeNameFor(name, call.Where()); isType {
		c.fail(call.Where(), "%s 是类型名，要用 new 造值", name)
		return errType
	}
	c.fail(call.Where(), "没有 %s 这个函数", name)
	return errType
}

// pkgMember 认 `a.make`、`a.Pair` 这一种点号：左边是本文件挂上的包名，右边得是那个包交出来的东西。
// 函数返回 memberPkgFunc，交出来的数返回 memberPkgConst，类型在这一侧用不了
// （要类型的位置走 resolveType 那头的双名分支）。
//
// en: pkgMember recognises the dot in `a.make` and `a.Pair`: the left side is a package alias
// bound in this file, the right side must be something that package exported. A function returns
// memberPkgFunc and an exported const returns memberPkgConst; a type is not usable on this side
// (a position that wants a type takes the two-name branch in resolveType).
func (c *Checker) pkgMember(m *ast.Member, pkgName, root string) (member, bool) {
	fi, ti, found, exported := c.pkgExported(root, m.NameText)
	if !found {
		// 不是函数也不是类型，再看是不是那一个包交出来的数。
		if tv, isVal := c.topVals[qualKey(root, m.NameText)]; isVal {
			return c.pkgValue(m, pkgName, tv)
		}
	}
	if !c.pkgSay(m.Where(), pkgName, m.NameText, found, exported) {
		return member{}, false
	}
	c.usedPkg[pkgName] = true
	if ti != nil {
		// 与直接写类型名同一条说法：造值要 new，写在要类型的位置上才算类型。
		c.fail(m.Where(), "%s 是类型名，要用 new 造值", m.NameText)
		return member{}, false
	}
	return member{kind: memberPkgFunc, typ: Type{Kind: FuncT, Name: m.NameText}, fi: fi}, true
}

// pkgValue 是 `greet.LIMIT` 这一种取法：交出来的 `const` 才读得到。`export shared` 的那一个名字
// 定成不交给包外面：它只在同一个包里几个文件共用，读出去要多出一个持有者、写出去要
// 回答"外面写进来的这一块归谁"，两条都没有答复；要交给外面，包里写一条函数。没挂 export 的那一种
// 与函数、类型同一句说法。记下声明节点，是因为静态槽按节点排，后端要找那一个槽只有这一个凭据。
//
// en: pkgValue handles the `greet.LIMIT` read: only an exported const may be read. An `export
// shared` name is never handed outside the package: it is shared only among the files of its own
// package, and reading it would add a second holder while writing it would need an answer to
// "who owns the value written in from outside" - neither exists, so a package that wants to
// expose that state returns it through a function. A name without export gets the same sentence
// functions and types get. The declaration node is recorded because static slots are laid out
// per node, and that node is the only handle the backend has on the slot.
func (c *Checker) pkgValue(m *ast.Member, pkgName string, tv TopValue) (member, bool) {
	if !tv.Export {
		c.fail(m.Where(), "%s 里的 %s 没有挂 export：包只把交出来的东西给别人用", pkgName, m.NameText)
		return member{}, false
	}
	if tv.Shared {
		c.fail(m.Where(), "%s 里的 %s 是 export shared：它只在包里面共用，包外面读不到", pkgName, m.NameText)
		return member{}, false
	}
	c.usedPkg[pkgName] = true
	if c.facts != nil {
		c.facts.PkgValues[m] = tv.Decl
	}
	return member{kind: memberPkgConst, typ: tv.Typ}, true
}

// pkgConstRead 认出"这一处 `.名字` 读的是某个包交出来的数"，交回写出来的那一段（`greet.LIMIT`）。
// 自增减那一处要拒绝的是写法本身，而 facts 在 `nf check` 那一路是空的，所以这里重问一遍名字表，
// 不靠 PkgValues 那份记录。
//
// en: pkgConstRead recognises that a `.name` reads a const some package exported, and returns
// the text as written (`greet.LIMIT`). The increment check refuses the spelling itself, and
// facts is nil on the `nf check` path, so this asks the name tables again instead of reading
// the PkgValues record.
func (c *Checker) pkgConstRead(e ast.Expr) (string, bool) {
	m, ok := e.(*ast.Member)
	if !ok {
		return "", false
	}
	n, ok := m.What.(*ast.Name)
	if !ok {
		return "", false
	}
	b, isPkg := c.packages[n.Text]
	if !isPkg {
		return "", false
	}
	tv, isVal := c.topVals[qualKey(b.root, m.NameText)]
	if !isVal || !tv.Export || tv.Shared {
		return "", false
	}
	return n.Text + "." + m.NameText, true
}

func (c *Checker) memberCall(call *ast.Call, m *ast.Member) Type {
	mm, ok := c.memberOf(m)
	if !ok {
		return errType
	}
	switch mm.kind {
	case memberPkgFunc:
		// 包外的函数与直接叫名字的同一套账：参数、交出、被调方带不带走。
		// 签名现算：检查按装载的次序逐份文件走，入口那份先跑，包那份的 funcInfo 这时还没算过。
		// 调用记的是定义处的规范名（带包根），后端发的 IR 函数名与它同一个。
		c.recordCall(call, CallRef{Kind: CallUser, Func: mm.fi.key})
		return c.userCall(call, c.funcSig(mm.fi.key, *mm.fi), m.NameText)
	case memberMethod:
		recv := c.exprTypeOf(m.What)
		c.recordCall(call, CallRef{Kind: CallUser, Func: recv.Name + "." + m.NameText, Recv: recv})
		if !omitsOnlyDefaults(call.Args, mm.sig.Params) {
			c.fail(call.Where(), "%s 收 %d 个参数，这里给了 %d 个", mm.sig.Name, len(mm.sig.Params), len(call.Args))
		}
		for i, a := range call.Args {
			if i < len(mm.sig.Params) {
				c.arg(a, mm.sig.Params[i].Typ, mm.sig.Params[i])
			}
		}
		return mm.sig.Ret
	case memberStd:
		c.recordCall(call, CallRef{Kind: CallStd, Std: mm.typ.Name})
		if !mm.sig.VarArg && len(call.Args) != len(mm.sig.Params) {
			c.fail(call.Where(), "%s 收 %d 个参数，这里给了 %d 个", m.NameText, len(mm.sig.Params), len(call.Args))
		}
		var got []Type
		for i, a := range call.Args {
			if mm.sig.VarArg {
				// console.print 那一类变长参数：`length(整只容器)` 与 `console.print(容器)`
				// 现在都走得通——形状表与打印机在降级那侧按类型现排，检查器不用替它们限制形状。
				// 裸的 null 与不返回值的那种写法在编译期拒绝：这一类落点上没有写着类型的位置，
				// 那一个位置得自己带一个值进来。
				t := c.expr(a)
				if t.Kind == Null {
					c.fail(a.Where(), bareNullMsg)
				}
				c.refuseNoValue(a, t)
				continue
			}
			if i < len(mm.sig.Params) {
				if mm.takes || (mm.takesVal && i == len(mm.sig.Params)-1) {
					c.arg(a, mm.sig.Params[i].Typ, mm.sig.Params[i])
					got = append(got, mm.sig.Params[i].Typ)
				} else {
					p := mm.sig.Params[i].Typ
					t := c.argBorrow(a, p)
					// 参数槽位上没写类型（ErrT：`number.to_string`、`length`、`math.absolute_value` 那一类）
					// 就跟变长那一侧同一件事：那一个位置得自己带形状，裸的 null 与不返回值的那一种都没有。
					if p.Kind == ErrT && t.Kind == Null {
						c.fail(a.Where(), bareNullMsg)
					}
					if p.Kind == ErrT {
						c.refuseNoValue(a, t)
					}
					// 文本进 `number.to_string` 只会白拷一份。这一条原先到发码那一步才报，
					// 把一条语言规则说成了后端少做的一块。
					if p.Kind == ErrT && t.Kind == Str && mm.typ.Name == "number.to_string" {
						c.fail(a.Where(), "number.to_string 不接 string：文本本来就是文本，接进来只会白拷一份")
					}
					got = append(got, t)
				}
			}
		}
		// 组名式调用（`text.to_int_in_base(文本, 进制)` 这一对）走的是这一条，不是 stdCall 那一条：
		// 进制那一问要在这里补上，不然它不会被执行。
		c.checkRadix(call, mm.typ.Name)
		if err := c.memberRange(call, m, mm); err != nil {
			_ = err
		}
		if mm.sig.Poly != 0 {
			return c.polyReturn(call, mm.typ.Name, mm.sig.Poly, got)
		}
		if mm.sig.Ret.Kind != 0 {
			return mm.sig.Ret
		}
		return Type{Kind: NoRet}
	case memberField:
		c.fail(call.Where(), "%s 是字段，不能加括号调用", m.NameText)
	case memberEnumValue:
		c.fail(call.Where(), "%s 是枚举成员，不能被调用", m.NameText)
	case memberPkgConst:
		c.fail(call.Where(), "%s 是包交出来的数，不能加括号调用", m.NameText)
	}
	return errType
}

// arg 检查一个实参：类型对上，引用类型的值交出去。
//
// p 是接收这一个位置的参数：用户写的函数与方法有节点，标准库组函数的签名是手排的（Node 为 nil）。
// 交出去的那一笔要记进 Handoff，后端据此问"被调方有没有带走"（见 check.noteConsumed）。
//
// en: arg checks one argument: the type must match and a reference-typed value is handed over.
// p is the parameter receiving this position: user-written functions and methods have a node,
// while a standard-library group function's signature is arranged by hand (Node is nil). The
// handover is recorded in Handoff so the backend can ask "did the callee keep it", which is what
// check.noteConsumed reads.
func (c *Checker) arg(a ast.Expr, want Type, p Param) {
	c.exprExpected(a, want)
	if p.takesRef() {
		// ref 参数只是借来读一次：那一次读多不出新持有者，所以名字不失效、字段那个位置不动，
		// `export shared` 的名字与遍历、this 借来的名字也都交得进去。
		return
	}
	if !want.ByValue() && want.Kind != ErrT && c.markFieldTaken(a, want) {
		// 字段那本账本身没错：记的是"那个槽位读完写空"，后端照它抹那一个字段。
		// 要拒绝的是这一步悄悄发生：把字段直接交给自己写的函数当实参，交完那个位置就空了，
		// 下一次读它是运行期的 10092，可在编译期一个字的错都不报（同一条用在容器那个位置上，
		// 容器那一条写明"被读走的那个位置交回 null"，字段这一侧没写死）。这里当场拒绝。
		// 正确写法是先接给一个名字再交——接名字那一步本来就是把那个位置过户出来。
		path := c.fieldPath(a)
		c.fail(a.Where(), "%s 是字段，直接交出去之后那个位置就空了：先接给一个名字，再拿那个名字交出去", path)
		return
	}
	if n, ok := a.(*ast.Name); ok && !want.ByValue() && want.Kind != ErrT {
		if c.isShared(n.Text) {
			c.fail(a.Where(), "%s 是 export shared 的名字，只能就地用，不能当参数交出去", n.Text)
			return
		}
		if kind := c.borrowed[n.Text]; kind != "" {
			if kind == borrowReceiver {
				c.fail(a.Where(), "this 是方法接收者的只读借用，不能交给参数")
				return
			}
			if kind == borrowParam {
				c.fail(a.Where(), "ref 参数 %s 是只读借用，不能交给要拿走的那个参数", n.Text)
				return
			}
			c.fail(a.Where(), "%s 是遍历交出的只读借用，不能交给参数", n.Text)
			return
		}
		c.markMoved(n)
		if p.Node != nil && c.facts != nil {
			if sv, ok := c.lookup(n.Text); ok && sv.node != nil {
				c.facts.Handoff[sv.node] = append(c.facts.Handoff[sv.node], p.Node)
			}
		}
	}
}

// argBorrow 只查类型，不动所有权：标准库那些只读借用的参数。
//
// en: argBorrow checks only the type and leaves ownership alone: the read-only borrow
// parameters of the standard library.
func (c *Checker) argBorrow(a ast.Expr, want Type) Type {
	return c.exprExpected(a, want)
}

// omitsOnlyDefaults 说这次调用少给的那几个参数是不是都带默认值。
// 少给且只少给带默认值的，才算合法；多给一律不行（交给各处的"只收 N 个参数"那条）。
//
// en: omitsOnlyDefaults says whether every parameter this call left out carries a default.
// Omitting arguments is legal only when all the omitted ones have defaults; supplying extra is
// never allowed (each site's "takes exactly N arguments" rule handles that).
func omitsOnlyDefaults(args []ast.Expr, params []Param) bool {
	if len(args) > len(params) {
		return false
	}
	for i := len(args); i < len(params); i++ {
		if params[i].Default == nil {
			return false
		}
	}
	return true
}

func (c *Checker) userCall(call *ast.Call, fi funcInfo, name string) Type {
	params := fi.params
	if len(call.Args) < len(params) {
		ok := true
		for i := len(call.Args); i < len(params); i++ {
			if fi.fn.Params[i].Default == nil {
				ok = false
			}
		}
		if !ok {
			c.fail(call.Where(), "%s 需要 %d 个参数，这里只给了 %d 个", name, len(params), len(call.Args))
		}
	}
	if len(call.Args) > len(params) {
		c.fail(call.Where(), "%s 只收 %d 个参数，这里给了 %d 个", name, len(params), len(call.Args))
	}
	for i, a := range call.Args {
		if i < len(params) {
			c.arg(a, params[i].Typ, params[i])
		}
	}
	return fi.ret
}

func (c *Checker) memberOfCallArg(a ast.Expr) bool {
	_, ok := a.(*ast.Member)
	return ok
}

// throwLike 检查 `throw (码, 消息)` 与 `exit (码, 消息)`。
//
// 码的区间：0～9999 归程序自己，10000 往上归语言自己。分成两段是为了让 `exit (0, "…")`
// 这种"留下一句话再正常退出"的写法成立——0 不属于语言那一段。
// 只判字面量：变量的值检查器拿不到，因此不对变量做这条检查。
//
// en: throwLike checks `throw (code, message)` and `exit (code, message)`. Code ranges: 0-9999
// belong to the program, 10000 and up belong to the language. The two-band split is what makes
// `exit (0, "...")` - leaving a sentence and exiting normally - possible; 0 is not in the
// language's band. Only literals are judged: the checker cannot see a variable's value, so this
// check skips variables.
func (c *Checker) throwLike(at ast.Pos, code, msg ast.Expr, what string) {
	c.exprExpected(code, typeInt)
	c.exprExpected(msg, typeStr)
	if lit, ok := code.(*ast.Lit); ok && lit.Kind == "int" {
		if lit.Value < 0 || lit.Value > 9999 {
			if what == "throw" {
				c.fail(at, "throw 的码从 0 写到 9999，%d 落在语言自己那一段（10000 往上）", lit.Value)
			} else {
				c.fail(at, "exit 的码从 0 写到 9999，%d 落在语言自己那一段（10000 往上）", lit.Value)
			}
		}
	}
}

func (c *Checker) indexExpr(ix *ast.Index) Type {
	base := c.exprTypeOf(ix.What)
	switch base.Kind {
	case List:
		c.exprExpected(ix.At, typeInt)
		return c.takeOut(base, ix)
	case Dict:
		c.exprExpected(ix.At, base.Args[0])
		return c.takeOut(base, ix)
	case Set:
		c.fail(ix.Where(), "hashset 不能用方括号按下标取，判在不在写 s.contains(v)")
		return errType
	case Str:
		c.exprExpected(ix.At, typeInt)
		return typeUnicode
	case Bytes:
		// bytes 的第 i 个槽位就是一个字节本身，越界与容器同一条（10031 号）。
		// 这一档只有读与写，没有"读走"那一本账——那个位置里放的是值类型。
		c.exprExpected(ix.At, typeInt)
		return typeByte
	case ErrT:
		return errType
	}
	c.fail(ix.Where(), "%s 不能用方括号取", base)
	return errType
}

// takeOut：引用类型的元素取出来就是接管那个位置。
//
// en: takeOut: reading a reference-typed element out of a container takes over that position.
func (c *Checker) takeOut(base Type, ix *ast.Index) Type {
	elem := base.Args[len(base.Args)-1]
	if !elem.ByValue() {
		if n, ok := ix.What.(*ast.Name); ok {
			switch c.borrowed[n.Text] {
			case borrowLoop:
				c.fail(ix.Where(), "遍历交出的 %s 是只读借用，不能再把里面的东西交给自己", n.Text)
				return errType
			case borrowParam:
				c.fail(ix.Where(), "ref 参数 %s 是只读借用，不能再把里面的东西交给自己", n.Text)
				return errType
			}
		}
		if m, ok := ix.What.(*ast.Member); ok {
			if b, ok := c.lookupOf(m); ok && c.moved[b] {
				return errType
			}
		}
	}
	return elem
}

func (c *Checker) lookupOf(m *ast.Member) (string, bool) {
	if n, ok := m.What.(*ast.Name); ok {
		return n.Text, c.moved[n.Text]
	}
	return "", false
}

func (c *Checker) convert(cv *ast.Convert) Type {
	from := c.expr(cv.What)
	to := c.resolveType(cv.Typ, cv.Where())
	if n, ok := cv.What.(*ast.Name); ok {
		if c.borrowed[n.Text] != "" {
			c.fail(cv.Where(), "只读借用不能拆盒")
		}
	}
	if to.Kind == Iface {
		c.fail(cv.Where(), "%s 是接口名，不能当类型写", bareName(to.Name))
		return errType
	}
	if to.Kind == Str && from.Kind != Str {
		c.fail(cv.Where(), "串与非串之间不走 as：变串写 number.to_string，读回数字写 text.to_int")
		return errType
	}
	if from.Kind == Str && to.Kind != Str {
		c.fail(cv.Where(), "串与非串之间不走 as：变串写 number.to_string，读回数字写 text.to_int")
		return errType
	}
	if to.Kind == Obj {
		// 空句柄进不了盒子（盒子只装 class、struct 与值类型）。这一条原先留到发码那一步
		// 才报"null 的类型字没排上"，而它是一条语言规则。
		// 声明里给 object 那一个位置放 null 不算装箱——那是可空的引用类型，照旧合法。
		if from.Kind == Null {
			c.fail(cv.Where(), "null 装不进盒子：装箱装的是那一个值，空句柄没有值可装")
			return errType
		}
		// 整只容器、Json 与 bytes 不进盒子：拆回来要认出尖括号里那一层，形状表里一个类型字不够；
		// Json 那一棵树同理。这两条原先留到发码那一步才报（"类型字没排上"），
		// 等于把一条语言规则说成后端少做的一块。bytes 是同一档：盒子里那个位置只放得下一个句柄，
		// 拆出来要靠类型字，而它比的是那一段内存，不是那一个块。
		if from.Kind == List || from.Kind == Dict || from.Kind == Set || from.Kind == Json || from.Kind == Bytes || from.Kind == Socket || from.Kind == Listener || from.Kind == Process {
			c.fail(cv.Where(), "%s 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去", from)
			return errType
		}
		if from.Kind == Obj {
			c.fail(cv.Where(), "%s 不必再装箱：它已经在盒子里，直接用它就行", from)
			return errType
		}
		// 装箱就是把那一个值交出去：来源是字段时记字段那一笔，是名字时记原名作废
		if !from.ByValue() && c.markFieldTaken(cv, to) {
			return to
		}
		if n, ok := cv.What.(*ast.Name); ok && !from.ByValue() {
			c.markMoved(n)
		}
		return to
	}
	if from.Kind == Obj {
		// 拆盒的目标得是进得了盒子的那一档：容器、Json 与 bytes 从来没装进去过，`o as list<int>`、
		// `o as Json` 拆的是一个装不进去的东西。这一条原先到发码才报
		// "%s 的类型字没排上，拆不开这个盒"，看起来像后端缺的实现，要改的其实是那句 as。
		if to.Kind == List || to.Kind == Dict || to.Kind == Set || to.Kind == Json || to.Kind == Bytes || to.Kind == Socket || to.Kind == Listener || to.Kind == Process {
			c.fail(cv.Where(), "盒子里没有 %s：它进不了盒子，也拆不出来", to)
			return errType
		}
		if c.markFieldTaken(cv, from) {
			return to
		}
		if n, ok := cv.What.(*ast.Name); ok && c.borrowed[n.Text] == "" {
			c.markMoved(n)
		}
		return to
	}
	if enumConvert(from, to) {
		return to
	}
	if !sameType(from, to) && !numericConvert(from, to) && !unicodeConvert(from, to) {
		c.fail(cv.Where(), "%s 不能 as 成 %s", from, to)
		return errType
	}
	// `as byte` 写在字面量上时在编译期拒绝：这个数此刻已经确定了，不必留到运行期。
	// 算出来的值走另一条——后端在那一刻判范围，越界当场抛（与字面量的 byte 规则同一句）。
	if to.Kind == Byte {
		if lit, ok := cv.What.(*ast.Lit); ok && lit.Kind == "int" && (lit.Value < 0 || lit.Value > 255) {
			c.fail(cv.Where(), "byte 只能装 0～255，这里是 %d", lit.Value)
			return errType
		}
	}
	return to
}

// enumConvert 是那条单向换算：enum 可以 `as int` 取出它那个数，反过来不识别。
// 只为"给错误码起个名字"提供，enum 照旧不参与算术。
//
// en: enumConvert is the one-way conversion: an enum may `as int` to get its number, never the
// reverse. It exists only to name error codes; enums still take part in no arithmetic.
func enumConvert(from, to Type) bool {
	return from.Kind == Enum && to.Kind == Int
}

func numericConvert(from, to Type) bool {
	num := func(k Kind) bool { return k == Int || k == Float || k == Byte }
	return num(from.Kind) && num(to.Kind)
}

func unicodeConvert(from, to Type) bool {
	return (from.Kind == Unicode && to.Kind == Int) || (from.Kind == Int && to.Kind == Unicode)
}

func (c *Checker) testExpr(t *ast.Test) Type {
	from := c.expr(t.What)
	to := c.resolveType(t.Typ, t.Where())
	if c.facts != nil {
		// 结论类型恒为 boolean，后端要知道的是"问的是哪个类型"，只能单独记一份
		c.facts.Tests[t] = to
	}
	if to.Kind == Iface {
		c.fail(t.Where(), "%s 是接口名，问不出来", bareName(to.Name))
	}
	// `is` 问的是"盒子里那一个值是哪一只"。容器、Json 与 bytes 进不了盒子，
	// 这一问没有东西可答——原先 `j is Json` 过了检查、到后端才报"类型字没排上"。
	if to.Kind == List || to.Kind == Dict || to.Kind == Set || to.Kind == Json || to.Kind == Bytes || to.Kind == Socket || to.Kind == Listener || to.Kind == Process {
		c.fail(t.Where(), "is 的右边只能写盒子里可能装的那个类型")
		return typeBool
	}
	if to.Kind == ErrT || from.Kind == ErrT {
		return typeBool
	}
	if from.ByValue() || from.Kind == Str {
		c.fail(t.Where(), "is 只问引用位置上的值，%s 不必问", from)
	} else if from.Kind != Obj && !sameType(from, to) && (to.Kind != Class && to.Kind != Struct) {
		c.fail(t.Where(), "is 的右边只能写盒子里可能装的那个类型")
	} else if from.Kind != Obj && to.Kind != Obj && !sameType(from, to) {
		c.fail(t.Where(), "%s 永远不会是 %s，这一问没意义", from, to)
	}
	if to.Kind == Iface {
		return typeBool
	}
	return typeBool
}

func (c *Checker) structLit(sl *ast.StructLit) Type {
	name, info, ok := c.typeNameFor(sl.Type, sl.Where())
	if !ok {
		c.fail(sl.Where(), "没有 %s 这个类型", sl.Type)
		return errType
	}
	if info.kind == Class {
		c.fail(sl.Where(), "%s 是 class，造值要写 new %s{...}", bareName(name), bareName(name))
		return errType
	}
	if info.kind == Iface {
		c.fail(sl.Where(), "%s 是接口，不能造值", bareName(name))
		return errType
	}
	if info.kind == Enum {
		c.fail(sl.Where(), "%s 是枚举，取成员写 %s.成员名", bareName(name), bareName(name))
		return errType
	}
	t := Type{Kind: Struct, Name: name}
	lay := c.layout(name)
	seen := map[string]bool{}
	for _, f := range sl.Fields {
		if seen[f.NameText] {
			c.fail(f.Pos, "字段 %s 写了两遍", f.NameText)
		}
		seen[f.NameText] = true
		var want *field
		for i := range lay.fields {
			if lay.fields[i].Name == f.NameText {
				want = &lay.fields[i]
			}
		}
		if want == nil {
			c.fail(f.Pos, "%s 里没有字段 %s", name, f.NameText)
			continue
		}
		c.exprExpected(f.Value, want.Typ)
	}
	for _, want := range lay.fields {
		if !seen[want.Name] {
			c.fail(sl.Where(), "%s 的字段 %s 没给值：字面量要把字段列全", bareName(name), want.Name)
		}
	}
	return t
}

func (c *Checker) newExpr(n *ast.New) Type {
	name, info, ok := c.typeNameFor(n.Type, n.Where())
	if !ok {
		c.fail(n.Where(), "没有 %s 这个类型", n.Type)
		return errType
	}
	if info.kind != Class {
		kind := "struct"
		if info.obj != nil {
			kind = info.obj.Kind
		}
		c.fail(n.Where(), "new 只用在 class 上，%s 是 %s", name, kind)
		return errType
	}
	lay := c.layout(name)
	seen := map[string]bool{}
	for _, f := range n.Fields {
		if seen[f.NameText] {
			c.fail(f.Pos, "字段 %s 写了两遍", f.NameText)
		}
		seen[f.NameText] = true
		var want *field
		for i := range lay.fields {
			if lay.fields[i].Name == f.NameText {
				want = &lay.fields[i]
			}
		}
		if want == nil {
			c.fail(f.Pos, "%s 里没有字段 %s", name, f.NameText)
			continue
		}
		// 字段那个位置与容器字面量那个位置同一条规则：
		// 引用类型交进去就是换持有者，原名当场作废；值类型整份拷贝，名字那头仍是持有者。
		// 顺序不能颠倒：先把初值当表达式检查完，再记交出——反过来时检查这个名字会先撞上
		// "交出去过"，报出来的就是"这个名字不能再写"那种说不清来由的话。
		c.exprExpected(f.Value, want.Typ)
		c.putInCell(f.Value, want.Typ)
	}
	if len(n.Fields) > 0 {
		for _, want := range lay.fields {
			if !seen[want.Name] {
				c.fail(n.Where(), "new %s 给了初值就得把字段给全，%s 没给", name, want.Name)
			}
		}
	}
	return Type{Kind: Class, Name: name}
}

// takesIntoCell 判容器字面量里的这个位置要不要记交出：引用类型装进去就是换持有者，
// 装进去和取出来都算交出，字面量与 `add` 共用同一份记录。
// 值类型本来就是整份拷贝，名字那头还是持有者，只记一笔"交出去过"给后端兜析构。
//
// en: takesIntoCell decides whether this position in a container literal should record a
// handover: putting a reference type in changes holders - references are handed over both going
// in and coming out, and a literal and `add` share one record. A value type is copied whole
// anyway and the name stays the holder, so only a note saying "it was handed over" is kept for
// the backend to fall back on when destructing.
func takesIntoCell(t Type) bool {
	return !t.ByValue() && t.Kind != ErrT && t.Kind != Null
}

// putInCell 是"往一个位置里放一个值"那一步的账，容器字面量与 `new` 的字段初值同一条规则：
// 引用类型按交出去算——原名当场作废，`[a, a]` 因此是错，`new C(f: b)` 之后 `b` 也不能再写；
// 值类型**不算交出**——那一个位置收下的是一份拷贝
// （`listLit`、`mapLit`、`setLit` 与 `newObject` 都走 `lower.go` 的 `cellValue`），
// 名字那头仍是自己那一块的持有者。
// 这里原先替值类型也记一笔"交出去过"，后端 `claim` 因此不登记那个名字，那一块没人析构
// （实测 `[one]` 与 `new Holder(p: one)` 各每圈留 16 字节）。
//
// en: putInCell is the ledger of "place a value into a position", shared by container literals
// and the field initialisers of `new`: a reference type counts as handed over - the original
// name is voided on the spot, which is why `[a, a]` is an error and why `b` may not be written
// again after `new C(f: b)`. A VALUE TYPE IS NOT a handover - the position receives a copy
// (listLit, mapLit, setLit and newObject all go through cellValue in lower.go), and the name on
// the other side is still the holder of its own block. This file used to record a "handed over"
// note for value types too, which made the backend's claim skip registering that name, leaving
// nobody to dismantle the block (measured: `[one]` and `new Holder(p: one)` each leaked 16
// bytes per iteration).
func (c *Checker) putInCell(v ast.Expr, t Type) {
	if c.markFieldTaken(v, t) {
		return // 装进位置的那一个来自 `class` 的字段，读完那个槽位写空
	}
	if n, ok := v.(*ast.Name); ok && takesIntoCell(t) {
		c.markMoved(n)
	}
}

// elemWants 从上头那份期望类型里取出元素（或键、值）那一个位置该是哪一种类型。
// 没有期望类型时返回 typeAuto（"任意"），底下照旧的自底向上推断走。
//
// en: elemWants takes from the expected type above what the element (or key, or value) position
// should be. With no expected type it returns typeAuto ("anything") and the usual bottom-up
// inference continues.
func elemWants(want Type, kind Kind, at int) Type {
	if want.Kind != kind || len(want.Args) <= at {
		return typeAuto
	}
	return want.Args[at]
}

func (c *Checker) listLit(l *ast.ListLit, want Type) Type {
	// 尖括号里那一个元素类型要传得下来。先前这一侧只有一个"有没有期望类型"的布尔，
	// "字面量落在要 byte 的位置上按 byte 处理"那条规则到不了元素这一层，
	// 于是 `let xs:list<byte> = [0, 255]` 被认成 list<int> 而遭拒绝。
	elemWant := elemWants(want, List, 0)
	var elem Type
	for _, it := range l.Items {
		t := c.infer(it, elemWant)
		t = c.acceptElem(it, elemWant, t)
		c.putInCell(it, t)
		if elem.Kind == 0 || elem.Kind == ErrT {
			elem = t
			continue
		}
		if t.Kind != ErrT && !sameType(t, elem) {
			c.fail(it.Where(), "一个 list 只能装一种元素：%s 与 %s 混了", elem, t)
		}
	}
	if elem.Kind == 0 {
		elem = typeInt
	}
	return Type{Kind: List, Name: "list", Args: []Type{elem}}
}

// acceptElem 是元素与键值那一层的 byte 规则：字面量落在要 byte 的位置上就按 byte 处理，
// 越界当场报，并把后端要用的那一份结论改记成实际采纳的类型。
//
// en: acceptElem is the byte rule one level down, for elements and keys/values: a literal
// landing in a position that wants byte is read as a byte, out of range is reported on the
// spot, and the conclusion the backend reads is rewritten to the type actually accepted.
func (c *Checker) acceptElem(e ast.Expr, want, got Type) Type {
	t := c.litInWant(e, want, got)
	if t.Kind == Byte && got.Kind == Int && c.facts != nil {
		c.facts.Types[e] = t
	}
	return t
}

func (c *Checker) mapLit(m *ast.MapLit, want Type) Type {
	keyWant := elemWants(want, Dict, 0)
	valWant := elemWants(want, Dict, 1)
	var k, v Type
	for _, p := range m.Pairs {
		kt := c.acceptElem(p.Key, keyWant, c.infer(p.Key, keyWant))
		vt := c.acceptElem(p.Value, valWant, c.infer(p.Value, valWant))
		c.putInCell(p.Key, kt)
		c.putInCell(p.Value, vt)
		if k.Kind == 0 || k.Kind == ErrT {
			k, v = kt, vt
		} else if !sameType(kt, k) || !sameType(vt, v) {
			c.fail(p.Pos, "一个 dict 的键与值只能各是一种类型")
		}
	}
	if k.Kind == 0 {
		k, v = typeStr, typeInt
	}
	if msg := keyReason(k); msg != "" && k.Kind != ErrT {
		c.fail(m.Where(), "%s 不能当 dict 的键：%s", k, msg)
	}
	return Type{Kind: Dict, Name: "dict", Args: []Type{k, v}}
}

func (c *Checker) setLit(s *ast.SetLit, want Type) Type {
	elemWant := elemWants(want, Set, 0)
	var e Type
	for _, it := range s.Items {
		t := c.acceptElem(it, elemWant, c.infer(it, elemWant))
		c.putInCell(it, t)
		if e.Kind == 0 || e.Kind == ErrT {
			e = t
		} else if !sameType(t, e) {
			c.fail(it.Where(), "一个 hashset 只能装一种元素")
		}
	}
	if e.Kind == 0 {
		e = typeInt
	}
	if msg := keyReason(e); msg != "" && e.Kind != ErrT {
		c.fail(s.Where(), "%s 不能当 hashset 的成员：%s", e, msg)
	}
	return Type{Kind: Set, Name: "hashset", Args: []Type{e}}
}

// ---- 标准库的签名 ----

type stdSig struct {
	params []Param
	ret    Type
	vararg bool
	// poly 非 0 表示"交回的类型跟着第一个实参走"：`math.absolute_value` 与 `math.minimum` 那几条是这种。
	// 这一类以前把 ret 写成 ErrT，而 ErrT 就是 0，stdCall 那句"ret.Kind != 0 才用它"
	// 一穿过去就成了"没有返回值"——`let x:float = math.absolute_value(-7.5)` 因此报"给的是 无返回值"。
	// 1 = 只看第一个实参；2 = 两个实参必须同型，返回那一个的类型。
	poly int
}

// stdFuncs 是挂在标准库组名上的那一些函数，键是"组名.函数名"。
// 这一张表摆在包级而不是函数里：编辑器那一侧要数得出一个组里有哪些名字
// （complete.go 的补全），查它的还是同一条 stdFunc。
//
// en: stdFuncs are the functions hanging on standard-library group names, keyed as
// "group.function". The table lives at package level rather than inside a function because the
// editor side must be able to enumerate a group's names (completion in complete.go), and the
// lookup goes through the same stdFunc.
var stdFuncs = map[string]stdSig{
	"length":      {params: []Param{{Typ: Type{Kind: ErrT}}}, ret: typeInt},
	"text.to_int": {params: []Param{{Typ: typeStr}}, ret: typeInt},
	// 文本里带小数点的数也要有一条路进得来。指数形式识别（`1.5e3`），
	// 无法识别的格式一律 10012 号，与 to_int 同一条。
	"text.to_float":    {params: []Param{{Typ: typeStr}}, ret: typeFloat},
	"text.join":        {params: []Param{{Typ: Type{Kind: List, Name: "list", Args: []Type{typeStr}}}, {Typ: typeStr}}, ret: typeStr},
	"number.to_string": {params: []Param{{Typ: Type{Kind: ErrT}}}, ret: typeStr},
	"console.print":    {vararg: true},
	// 同一套印法往标准错误那一个流里印，重定向走标准输出时提示不会被卷进文件。
	"console.print_error": {vararg: true},
	"console.read":        {ret: typeStr},
	"file.read":           {params: []Param{{Typ: typeStr}}, ret: typeStr},
	// 文件那七条：属性三条、目录两条、分块两条。位置一律是 int——
	// 位置按字节数计，而字节数没有上限记号，就用 int。
	"file.size":       {params: []Param{{Typ: typeStr}}, ret: typeInt},
	"file.modified":   {params: []Param{{Typ: typeStr}}, ret: typeInt},
	"file.readonly":   {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"file.list":       {params: []Param{{Typ: typeStr}}, ret: Type{Kind: List, Name: "list", Args: []Type{typeStr}}},
	"file.list_paths": {params: []Param{{Typ: typeStr}}, ret: Type{Kind: List, Name: "list", Args: []Type{typeStr}}},
	"file.read_from":  {params: []Param{{Typ: typeStr}, {Typ: typeInt}, {Typ: typeInt}}, ret: typeStr},
	"file.write_at":   {params: []Param{{Typ: typeStr}, {Typ: typeInt}, {Typ: typeStr}}},
	// 字节那一双：整个文件按字节进、按字节出，中间不过 UTF-8。
	// 那一条 `list<byte>` 换成 `bytes`：容器每个槽位 8 字节，读一个 5.6 MB 的文件要 98,205,840 字节，
	// 换成一块连续内存就是 1 字节 1 字节。写的那一条收的也是同一档。
	"file.read_bytes":               {params: []Param{{Typ: typeStr}}, ret: typeBytes},
	"file.write_bytes":              {params: []Param{{Typ: typeStr}, {Typ: typeBytes}}},
	"file.write":                    {params: []Param{{Typ: typeStr}, {Typ: typeStr}}},
	"file.append":                   {params: []Param{{Typ: typeStr}, {Typ: typeStr}}},
	"file.exists":                   {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"file.remove":                   {params: []Param{{Typ: typeStr}}},
	"path.join":                     {params: []Param{{Typ: typeStr}, {Typ: typeStr}}, ret: typeStr},
	"path.file_name":                {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"path.extension":                {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"path.exists":                   {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"environment.get":               {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"environment.set":               {params: []Param{{Typ: typeStr}, {Typ: typeStr}}},
	"environment.unset":             {params: []Param{{Typ: typeStr}}},
	"environment.current_directory": {ret: typeStr},
	"time.now":                      {ret: typeInt},
	"time.elapsed":                  {ret: typeFloat},
	// 把秒数拆成内建那只 `Calendar`（七个 int，按 UTC 算）
	"time.calendar":       {params: []Param{{Typ: typeInt}}, ret: typeCalendar},
	"math.absolute_value": {params: []Param{{Typ: Type{Kind: ErrT}}}, poly: 1},
	"math.floor":          {params: []Param{{Typ: typeFloat}}, ret: typeInt},
	"math.ceiling":        {params: []Param{{Typ: typeFloat}}, ret: typeInt},
	"math.round":          {params: []Param{{Typ: typeFloat}}, ret: typeInt},
	"math.minimum":        {params: []Param{{Typ: Type{Kind: ErrT}}, {Typ: Type{Kind: ErrT}}}, poly: 2},
	"math.maximum":        {params: []Param{{Typ: Type{Kind: ErrT}}, {Typ: Type{Kind: ErrT}}}, poly: 2},
	"math.power":          {params: []Param{{Typ: typeInt}, {Typ: typeInt}}, ret: typeInt},
	"math.random":         {params: []Param{{Typ: typeInt}, {Typ: typeInt}}, ret: typeInt},
	// 四条具名函数：开方一条、休眠一条、目录四条里的三条（is_directory 也在这一组）。
	"math.square_root": {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	// 数学那一块补的这几条：一个常数、两条三角、一条自然对数、一条小数随机。
	// π 做成零参数的函数，理由是这张表里组名的成员只有函数一种写法（`time.now()` 同形）。
	"math.pi":               {ret: typeFloat},
	"math.sine":             {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.cosine":           {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.natural_log":      {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.random_float":     {ret: typeFloat},
	"time.sleep":            {params: []Param{{Typ: typeInt}}},
	"file.create_directory": {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"file.is_directory":     {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"file.rename":           {params: []Param{{Typ: typeStr}, {Typ: typeStr}}},
	"file.remove_directory": {params: []Param{{Typ: typeStr}}},
	// 两条：复制一条路径（目标在就覆盖，与 `file.write` 同一条处置），
	// 以及把缺的那几层一次建齐（单层那一条只管最后一层）。
	"file.copy":                  {params: []Param{{Typ: typeStr}, {Typ: typeStr}}},
	"file.create_directories":    {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"regular_expression.match":   {params: []Param{{Typ: typeStr}, {Typ: typeStr}}, ret: typeBool},
	"regular_expression.find":    {params: []Param{{Typ: typeStr}, {Typ: typeStr}}, ret: Type{Kind: List, Name: "list", Args: []Type{typeStr}}},
	"regular_expression.replace": {params: []Param{{Typ: typeStr}, {Typ: typeStr}, {Typ: typeStr}}, ret: typeStr},
	"regular_expression.split":   {params: []Param{{Typ: typeStr}, {Typ: typeStr}}, ret: Type{Kind: List, Name: "list", Args: []Type{typeStr}}},
	// 数学那一侧补的四条：正切、反正切、以 10 为底的对数、e 的 x 次方；
	// 再加两条：正负号这一个答复、把随机数按住的开关。
	"math.tangent":     {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.arctangent":  {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.log_base_10": {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.exponential": {params: []Param{{Typ: typeFloat}}, ret: typeFloat},
	"math.sign":        {params: []Param{{Typ: Type{Kind: ErrT}}}, poly: 3},
	"math.set_seed":    {params: []Param{{Typ: typeInt}}},
	// 进制那一对：`0x` 的字面量早就识别，从文件或键盘读来的那一串还得有路进来。
	"text.to_int_in_base":      {params: []Param{{Typ: typeStr}, {Typ: typeInt}}, ret: typeInt},
	"number.to_string_in_base": {params: []Param{{Typ: typeInt}, {Typ: typeInt}}, ret: typeStr},
	// 文本与字节块互换、以及 base64 那一串。from_bytes 收的那一整片是 `bytes`。
	"text.from_bytes":  {params: []Param{{Typ: typeBytes}}, ret: typeStr},
	"text.to_base64":   {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"text.escape_json": {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"text.from_base64": {params: []Param{{Typ: typeStr}}, ret: typeStr},
	// 路径那两条：往上一截、以及是不是从根起。
	"path.directory_name": {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"path.is_absolute":    {params: []Param{{Typ: typeStr}}, ret: typeBool},
	// 目录那一侧补的两条：一层一层钻下去、连里面的东西一起删。
	"file.walk_paths":  {params: []Param{{Typ: typeStr}}, ret: Type{Kind: List, Name: "list", Args: []Type{typeStr}}},
	"file.remove_tree": {params: []Param{{Typ: typeStr}}},
	// 换当前目录（返回真／假，与 `file.create_directories` 同一答复风格）与本地比 UTC 快几分钟。
	"environment.set_current_directory": {params: []Param{{Typ: typeStr}}, ret: typeBool},
	"time.utc_offset":                   {ret: typeInt},
	// json 那一组。这一版只做读：把一段文本拆成一棵树，再从树上取。
	// 取不到一律返回 null，不新增错误码——所以 json.at／item／to_text 的返回类型
	// 是可空的引用类型；那三条返回值类型的（to_integer／to_number／to_boolean）
	// 多带一个默认值，与 `dict.get(键, 默认)` 同一种形状（值类型不能取 null）。
	"json.parse":      {params: []Param{{Typ: typeStr}}, ret: typeJson},
	"json.explain":    {params: []Param{{Typ: typeStr}}, ret: typeStr},
	"json.kind":       {params: []Param{{Typ: typeJson}}, ret: typeStr},
	"json.count":      {params: []Param{{Typ: typeJson}}, ret: typeInt},
	"json.keys":       {params: []Param{{Typ: typeJson}}, ret: typeStrList},
	"json.has":        {params: []Param{{Typ: typeJson}, {Typ: typeStr}}, ret: typeBool},
	"json.at":         {params: []Param{{Typ: typeJson}, {Typ: typeStr}}, ret: typeJson},
	"json.item":       {params: []Param{{Typ: typeJson}, {Typ: typeInt}}, ret: typeJson},
	"json.to_text":    {params: []Param{{Typ: typeJson}}, ret: typeStr},
	"json.to_integer": {params: []Param{{Typ: typeJson}, {Typ: typeInt}}, ret: typeInt},
	"json.to_number":  {params: []Param{{Typ: typeJson}, {Typ: typeFloat}}, ret: typeFloat},
	"json.to_boolean": {params: []Param{{Typ: typeJson}, {Typ: typeBool}}, ret: typeBool},
	// bytes 那一组。造出来、读回 base64、并起几块这三条挂在组名上，
	// 取与放走下标与 `length`，`slice` 与 `to_base64` 挂在值身上。
	"bytes.new":         {params: []Param{{Typ: typeInt}}, ret: typeBytes},
	"bytes.from_base64": {params: []Param{{Typ: typeStr}}, ret: typeBytes},
	"bytes.join": {params: []Param{{Typ: Type{Kind: List, Name: "list",
		Args: []Type{typeBytes}}}}, ret: typeBytes},
	// net 那一组。超时那一个毫秒数管连接与收发，不管解析：
	// `getaddrinfo` 只有四个参数，不收超时（Microsoft 的 `ws2tcpip.h`），
	// 解析那一段卡多久由系统的解析器决定——这一句要同步进语言说明与站上那一组。
	"net.connect": {params: []Param{{Typ: typeStr}, {Typ: typeInt}, {Typ: typeInt}}, ret: typeSocket},
	"net.listen":  {params: []Param{{Typ: typeInt}}, ret: typeListener},
	// http 那一组。两条都返回内建那只 `Response`：
	// 状态码、头、正文三样一次给全，TLS／重定向／证书由系统的 winhttp 做，语言不管。
	// 超时那一个毫秒数这一次管四段（解析、连接、送、收），winhttp 的 `WinHttpSetTimeouts`
	// 四段都收——与 `net.connect` 那一条"管连接与收发、不管解析"是两样，写在组那一页。
	"http.get":  {params: []Param{{Typ: typeStr}, {Typ: typeInt}}, ret: typeResponse},
	"http.post": {params: []Param{{Typ: typeStr}, {Typ: typeBytes}, {Typ: typeInt}}, ret: typeResponse},
	// process 那一组。这一档只有一条挂在组名上：那一串命令行原样交给
	// 系统的 `CreateProcessW`，语言不替谁拆参数、也不替谁加引号（带空格的路径自己写成 `"…"`）。
	// 起不成的那些原因（文件不在、不是一个可执行文件、不让访问）只占一个号，系统原号码带在消息里。
	"process.start": {params: []Param{{Typ: typeStr}}, ret: typeProcess},
}

// stdFunc 查一条挂在组名上的函数；第二个返回假说的是"没有这一条"。
//
// en: stdFunc looks up a function on a group name; a false second result means "no such
// function".
func stdFunc(name string) (stdSig, bool) {
	sig, ok := stdFuncs[name]
	return sig, ok
}

// bareNullMsg 是"裸的 null 不能当实参"这一条的报错原文。
//
// 打印机识别实参靠的是运行期形状表，而 null 自己不是一个类型、那一个位置上没有形状——
// 猜它是空句柄、空文本还是空容器都是歧义。带类型的空句柄照旧印得出 `null`（`let none:Bag = null`）。
//
// en: bareNullMsg is the error text for the rule "a bare null may not be an argument". The
// printer recognises arguments through the runtime shape table, and null is not a type - there
// is no shape for that position, so guessing between an empty handle, empty text and an empty
// container is ambiguity. A typed null handle still prints as `null` (`let none:Bag = null`).
const bareNullMsg = "裸的 null 不能当实参：它自己不是一个类型，那一个位置上没有形状。" +
	"要判空就写 x == null；要打印一个空句柄，先给它一个类型（let none:Bag = null）"

// callText 把"这一个式子叫的是谁"说成一句话：`l.sort()` 是 sort、`show()` 是 show。
// 读不出名字的写法（下标、括号里再套一层）返回"这一句"。
//
// en: callText says who this expression calls, in one phrase: `l.sort()` is sort, `show()` is
// show. Spellings with no name to read (a subscript, a parenthesised call) return "this
// expression".
func callText(e ast.Expr) string {
	cal, ok := e.(*ast.Call)
	if !ok {
		return "这一句"
	}
	switch fn := cal.Callee.(type) {
	case *ast.Name:
		return fn.Text
	case *ast.Member:
		return fn.NameText
	}
	return "这一句"
}

// refuseNoValue 拒绝"把一个不交回值的动作放在要值的位置上"（不写返回类型就是不返回值）。
//
// 带类型的参数槽位那一侧由 exprExpected 拒绝（报"这里要的是 int，给的是 无返回值"）；
// 这一处管的是没有写着类型的落点——变长参数那一个，以及 `length`、`number.to_string`
// 那种"跟着实参走"的槽位。原先这两处只拒绝裸的 null，于是 `console.print(l.sort())`
// 检查放行，到发码那一步才报"后端还没做到这些：印不出 无返回值"。
//
// en: refuseNoValue stops "an action that returns nothing, written where a value is required"
// (no return type written means it returns nothing). The typed-parameter side is handled by
// exprExpected (reporting "this wants int, got no-return-value"); this covers destinations with
// no written type - the variadic slot, and the slots that follow the argument such as `length`
// and `number.to_string`. Previously only a bare null was refused there, so
// `console.print(l.sort())` passed checking and only failed at emit with "the backend cannot do
// this yet: no-return-value cannot be printed".
func (c *Checker) refuseNoValue(a ast.Expr, t Type) {
	if t.Kind == NoRet {
		c.fail(a.Where(), "%s 不返回值，那一个位置上没有值可用", callText(a))
	}
}

// hasLength 是 `length` 收得了的那几类：三种容器与文本。
// 文本数的是码点，容器数的是里面的元素个数，别的类型没有"长度"这个概念。
//
// en: hasLength is the kinds `length` accepts: the three containers plus text. Text counts code
// points, containers count the elements inside, and no other type has a length.
func hasLength(t Type) bool {
	switch t.Kind {
	case List, Dict, Set, Str, Bytes:
		return true
	}
	return false
}

func (c *Checker) stdCall(call *ast.Call, name string, sig stdSig) Type {
	if !sig.vararg && len(call.Args) != len(sig.params) {
		c.fail(call.Where(), "%s 收 %d 个参数，这里给了 %d 个", name, len(sig.params), len(call.Args))
	}
	var got []Type
	if sig.vararg {
		// 变长那一侧（眼下只有 console.print）没有写着类型的参数槽位，每一个实参自己带形状。
		for _, a := range call.Args {
			t := c.expr(a)
			if t.Kind == Null {
				c.fail(a.Where(), bareNullMsg)
			}
			c.refuseNoValue(a, t)
			got = append(got, t)
		}
		if sig.ret.Kind != 0 {
			return sig.ret
		}
		return Type{Kind: NoRet}
	}
	for i, a := range call.Args {
		if i >= len(sig.params) {
			break
		}
		want := sig.params[i].Typ
		if want.Kind == ErrT {
			t := c.expr(a)
			// ErrT 是"这个位置没写类型，跟着实参走"（`length`、`number.to_string`、`math.absolute_value` 那几条）：
			// 裸的 null 在这儿同样落不到一个形状上，不返回值的那一种写法也是同一件事。
			if t.Kind == Null {
				c.fail(a.Where(), bareNullMsg)
			}
			c.refuseNoValue(a, t)
			// `length` 数的是三种容器、文本与字节块：这个位置跟着实参走，
			// 其余类型没有长度。这一条原先只到发码那一步才报，等于把一条语言规则
			// 说成后端少做的一块。
			if name == "length" && t.Kind != ErrT && !hasLength(t) {
				c.fail(a.Where(), "length 数不了 %s：只有 list、dict、hashset、string 与 bytes 这五样有长度", t)
			}
			got = append(got, t)
			continue
		}
		c.exprExpected(a, want)
		got = append(got, want)
	}
	c.checkRadix(call, name)
	if sig.poly != 0 {
		return c.polyReturn(call, name, sig.poly, got)
	}
	if sig.ret.Kind != 0 {
		return sig.ret
	}
	return Type{Kind: NoRet}
}

// checkRadix 是进制那一对的编译期检查：那一个位置只准写字面量，而且只能是
// 2、8、10、16 这四个数。与 `as byte` 卡在字面量上那一条是同一族做法：此刻这个数已经定了，
// 不必留到运行期，也不必为"算出来的进制"再定一条答复。
//
// en: checkRadix is the compile-time check for the radix pair: that position accepts only a
// literal, and only the four numbers 2, 8, 10, 16. Same family as the rule that holds `as byte`
// to a literal: at this moment the number is already known, so there is nothing to defer to
// runtime and no answer to invent for "a computed radix".
func (c *Checker) checkRadix(call *ast.Call, name string) {
	if name != "text.to_int_in_base" && name != "number.to_string_in_base" {
		return
	}
	if len(call.Args) < 2 {
		return // 参数个数不对那一条已经在上面报过了
	}
	lit, ok := call.Args[1].(*ast.Lit)
	if !ok || lit.Kind != "int" {
		c.fail(call.Args[1].Where(), "%s 的进制要写成一个数，不能是算出来的", name)
		return
	}
	switch lit.Value {
	case 2, 8, 10, 16:
	default:
		c.fail(call.Args[1].Where(), "%s 的进制只准 2、8、10、16，这里是 %d", name, lit.Value)
	}
}

// polyReturn 落那三条"交回的类型跟进来的值走"的组函数：
// `math.absolute_value` 只看第一个，`math.minimum`／`math.maximum` 要求两个同型。
//
// 能进来的只有 int、float、byte 三种：布尔、码点、enum 取绝对值或比大小都没有意义，
// 编译期就拒绝，不等运行期给个没人预期的数。byte 的 absolute_value 返回 int（语言规则点明了这一条），
// minimum／maximum 保持原类型——两个 byte 里挑一个，不会挑出个更大的数来。
//
// en: polyReturn implements the three group functions whose return type follows the value that
// came in: `math.absolute_value` looks only at the first argument, `math.minimum` and
// `math.maximum` require both to be the same type. Only int, float and byte get this far:
// absolute value or ordering makes no sense for boolean, code point or enum, so they are
// refused at compile time rather than producing an unexpected number at runtime. absolute_value
// of a byte returns int (the language rule says so), while minimum/maximum keep the original
// type - picking one of two bytes cannot produce a wider number.
func (c *Checker) polyReturn(call *ast.Call, name string, how int, got []Type) Type {
	if len(got) == 0 {
		c.fail(call.Where(), "%s 需要一个值", name)
		return errType
	}
	t := got[0]
	switch t.Kind {
	case Int, Float, Byte:
	default:
		c.fail(call.Where(), "%s 收 int、float 或 byte，给的是 %s", name, t)
		return errType
	}
	if how == 2 {
		if len(got) < 2 {
			c.fail(call.Where(), "%s 要两个同类型的值", name)
			return errType
		}
		if !sameType(got[1], t) {
			c.fail(call.Where(), "%s 的两个值必须同类型：一个是 %s、一个是 %s", name, t, got[1])
			return errType
		}
	}
	if how == 1 && t.Kind == Byte {
		return typeInt
	}
	if how == 3 {
		// `math.sign` 那一类：收 int、float、byte，返回的只有正、负、零三个答复
		return typeInt
	}
	return t
}

func (c *Checker) stdGroupName(call *ast.Call) (string, string, bool) {
	m, ok := call.Callee.(*ast.Member)
	if !ok {
		return "", "", false
	}
	n, ok := m.What.(*ast.Name)
	if !ok {
		return "", "", false
	}
	return n.Text, m.NameText, true
}

var _ = fmt.Sprintf

// stdGroups 是那一些标准库组名。识别组名的那一侧（isStdGroup）与编辑器补全那一侧
// （complete.go 的 GroupNames）读的都是这一张表，加一个组只改这里一行。
//
// en: stdGroups is the list of standard-library group names. Both the recognition side
// (isStdGroup) and the editor's completion side (GroupNames in complete.go) read this one
// table, so adding a group costs one line.
var stdGroups = []string{
	"console", "text", "number", "math", "time", "file", "path", "environment",
	"regular_expression", "json", "bytes", "net", "http", "process",
}

// isStdGroup 识别那些标准库组名。
//
// en: isStdGroup recognises the standard-library group names.
func isStdGroup(name string) bool {
	for _, g := range stdGroups {
		if g == name {
			return true
		}
	}
	return false
}

// emptyContainer 识别 `[]`、`{}` 这种空字面量：它自己不含元素类型，得靠左边的标注。
//
// en: emptyContainer recognises `[]` and `{}`: such a literal cannot state its element type and
// must take it from the annotation on the left.
func emptyContainer(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ListLit:
		return len(x.Items) == 0
	case *ast.MapLit:
		return len(x.Pairs) == 0
	case *ast.SetLit:
		return len(x.Items) == 0
	}
	return false
}

// funcSig 把函数的参数与返回类型摊开；跨文件拿进来的函数在这里第一次算。
//
// en: funcSig expands a function's parameters and return type; a function brought in across
// files is computed here for the first time.
func (c *Checker) funcSig(name string, fi funcInfo) funcInfo {
	if fi.laidOut || fi.fn == nil {
		return fi
	}
	fi.laidOut = true
	fi.ret = c.retOf(fi.fn)
	fi.params = c.paramsOf(fi.fn)
	c.funcs[name] = fi
	return fi
}

// markUsed 记下某个 import 进来的名字确实被用到了（没用到的 import 要报错）。
//
// en: markUsed records that an imported name was genuinely used (an unused import is an error).
func (c *Checker) markUsed(name string) {
	if _, ok := c.imported[name]; ok {
		c.usedImp[name] = true
	}
}

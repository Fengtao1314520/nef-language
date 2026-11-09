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

package ast

import "fmt"

// 这一份文件只管一件事：交出一份语法树的独立副本。
//
// 需要它的原因是泛型。一份 `class Box<Content>` 的方法体在 `Box<int>` 与 `Box<string>` 里要得到
// 两种类型结论，而检查器那些结论是按语法节点存的——同一个节点用两遍，第二份实例读到的就是
// 第一份的类型。复制一份之后节点是新的，两份结论天然分开，读侧不必改成双键。
//
// 副本上的位置一律与原文相同：报错、打印与跳转都要指回写出来的那一行。
//
// en: This file does one thing: hand out independent copies of syntax trees.
//
// Generics are why it exists. One `class Box<Content>` method body needs two sets of type
// conclusions in `Box<int>` and `Box<string>`, and the checker stores conclusions per syntax
// node - reuse one node and the second instance reads the first one's types. A copy brings fresh
// nodes, so the conclusions separate on their own and the readers need no second key dimension.
//
// Every copy keeps the original positions: errors, printing and jumping all point back at the
// line that was written.

// CloneFunc 交出一份函数的副本：参数、返回类型与整条语句流都是新节点，名字与位置照旧。
// 类型参数表原样带过去（`TypeParam` 是值不变的记号，检查那一步按名字查对照表）。
//
// en: CloneFunc copies a function: parameters, return type and the whole statement list become
// new nodes while names and positions stay. The type parameter table travels as written
// (TypeParam is an inert mark; the checker looks its names up in the mapping).
func CloneFunc(f *Func) *Func {
	if f == nil {
		return nil
	}
	out := &Func{
		Pos:        f.Pos,
		Export:     f.Export,
		NameText:   f.NameText,
		NamePos:    f.NamePos,
		TypeParams: cloneTypeParams(f.TypeParams),
		Ret:        CloneType(f.Ret),
		IsMethod:   f.IsMethod,
	}
	if f.Params != nil {
		out.Params = make([]*Param, 0, len(f.Params))
		for _, p := range f.Params {
			out.Params = append(out.Params, &Param{
				Pos:      p.Pos,
				NameText: p.NameText,
				Typ:      CloneType(p.Typ),
				Default:  cloneExpr(p.Default),
				Ref:      p.Ref,
			})
		}
	}
	out.Body = CloneStmts(f.Body)
	return out
}

// CloneObject 交出一份 `struct`／`class` 声明的副本：字段与方法的节点都是新的，
// `base` 那一个名字照旧指过去（接口不是泛型的那四类，没有按实例分开的结论）。
//
// en: CloneObject copies a struct or class declaration: fields and methods get fresh nodes,
// while the base name still points where it did (interfaces are not one of the four forms that
// take type parameters, so nothing about them splits per instance).
func CloneObject(o *Object) *Object {
	if o == nil {
		return nil
	}
	out := &Object{
		Pos:        o.Pos,
		Kind:       o.Kind,
		Export:     o.Export,
		NameText:   o.NameText,
		TypeParams: cloneTypeParams(o.TypeParams),
		Base:       o.Base,
	}
	if o.Fields != nil {
		out.Fields = make([]*Field, 0, len(o.Fields))
		for _, f := range o.Fields {
			out.Fields = append(out.Fields, &Field{Pos: f.Pos, NameText: f.NameText, Typ: CloneType(f.Typ)})
		}
	}
	if o.Methods != nil {
		out.Methods = make([]*Func, 0, len(o.Methods))
		for _, m := range o.Methods {
			out.Methods = append(out.Methods, CloneFunc(m))
		}
	}
	return out
}

// CloneStmts 交出一份语句列表的副本；nil 原样是 nil（有的地方拿 nil 与空表区分形状）。
//
// en: CloneStmts copies a statement list, and nil stays nil - some shapes tell nil apart from
// an empty list.
func CloneStmts(ss []Stmt) []Stmt {
	if ss == nil {
		return nil
	}
	out := make([]Stmt, 0, len(ss))
	for _, s := range ss {
		out = append(out, CloneStmt(s))
	}
	return out
}

// CloneStmt 交出那一条语句的副本。每一种语句都在这里列全：漏一种是编译器的错，
// 不是可以让用户看见的话，所以宁可当场炸在那里，也不把原文那一个节点悄悄还回去——
// 悄悄把原节点还回去时，两份实例会共用同一份结论，错的不是报错，是生成的机器码。
//
// en: CloneStmt copies one statement. Every statement kind is listed here. Missing one is a
// compiler bug rather than something the user should be shown, so it is better to stop on the
// spot than to hand the original node back quietly - handed back quietly, two instances share
// one set of conclusions and what comes out wrong is the machine code, not an error message.
func CloneStmt(s Stmt) Stmt {
	if s == nil {
		return nil
	}
	switch x := s.(type) {
	case *Assign:
		return &Assign{Pos: x.Pos, Target: cloneExpr(x.Target), Value: cloneExpr(x.Value)}
	case *Incdec:
		return &Incdec{Pos: x.Pos, What: cloneExpr(x.What), Up: x.Up}
	case *ExprStmt:
		return &ExprStmt{Pos: x.Pos, E: cloneExpr(x.E)}
	case *If:
		out := &If{Pos: x.Pos, HasElse: x.HasElse}
		out.Conds = cloneExprs(x.Conds)
		if x.Bodies != nil {
			out.Bodies = make([][]Stmt, 0, len(x.Bodies))
			for _, b := range x.Bodies {
				out.Bodies = append(out.Bodies, CloneStmts(b))
			}
		}
		out.Else = CloneStmts(x.Else)
		return out
	case *While:
		return &While{Pos: x.Pos, Cond: cloneExpr(x.Cond), Body: CloneStmts(x.Body)}
	case *For:
		out := &For{
			Pos:    x.Pos,
			Var:    cloneDecl(x.Var),
			Ranged: x.Ranged,
			From:   cloneExpr(x.From),
			Until:  cloneExpr(x.Until),
			Iter:   cloneExpr(x.Iter),
			Body:   CloneStmts(x.Body),
		}
		return out
	case *Switch:
		out := &Switch{Pos: x.Pos, Head: cloneExpr(x.Head), HasDefault: x.HasDefault}
		if x.Cases != nil {
			out.Cases = make([]*Case, 0, len(x.Cases))
			for _, c := range x.Cases {
				out.Cases = append(out.Cases, &Case{Pos: c.Pos, Labels: cloneExprs(c.Labels), Body: CloneStmts(c.Body)})
			}
		}
		return out
	case *Break:
		return &Break{Pos: x.Pos}
	case *Continue:
		return &Continue{Pos: x.Pos}
	case *Return:
		return &Return{Pos: x.Pos, Val: cloneExpr(x.Val)}
	case *Try:
		return &Try{
			Pos: x.Pos, Body: CloneStmts(x.Body), HasCatch: x.HasCatch,
			CodeName: x.CodeName, MsgName: x.MsgName, AtName: x.AtName, TraceName: x.TraceName,
			Catch: CloneStmts(x.Catch), HasFinally: x.HasFinally, Finally: CloneStmts(x.Finally),
		}
	case *Decl:
		return cloneDecl(x)
	}
	panic("泛型复制没有接住的语句节点：" + fmt.Sprintf("%T", s))
}

// CloneExpr 交出那一个表达式的副本；nil 原样是 nil（不带值的 `return`、没有初值的声明都靠它）。
//
// en: CloneExpr copies one expression, and nil stays nil - a valueless `return` and a
// declaration without an initializer both rely on that.
func CloneExpr(e Expr) Expr { return cloneExpr(e) }

func cloneExpr(e Expr) Expr {
	if e == nil {
		return nil
	}
	switch x := e.(type) {
	case *Lit:
		return &Lit{Pos: x.Pos, Kind: x.Kind, Text: x.Text, Value: x.Value}
	case *Name:
		return &Name{Pos: x.Pos, Text: x.Text}
	case *Binary:
		return &Binary{Pos: x.Pos, Op: x.Op, Left: cloneExpr(x.Left), Right: cloneExpr(x.Right)}
	case *Unary:
		return &Unary{Pos: x.Pos, Op: x.Op, What: cloneExpr(x.What)}
	case *Convert:
		return &Convert{Pos: x.Pos, What: cloneExpr(x.What), Typ: CloneType(x.Typ)}
	case *Test:
		return &Test{Pos: x.Pos, What: cloneExpr(x.What), Typ: CloneType(x.Typ)}
	case *Call:
		return &Call{Pos: x.Pos, Callee: cloneExpr(x.Callee), Args: cloneExprs(x.Args)}
	case *Index:
		return &Index{Pos: x.Pos, What: cloneExpr(x.What), At: cloneExpr(x.At)}
	case *Member:
		return &Member{Pos: x.Pos, What: cloneExpr(x.What), NameText: x.NameText}
	case *StructLit:
		return &StructLit{Pos: x.Pos, Type: x.Type, Fields: cloneStructFields(x.Fields)}
	case *New:
		return &New{Pos: x.Pos, Type: x.Type, TypeArgs: cloneTypes(x.TypeArgs),
			Fields: cloneStructFields(x.Fields)}
	case *ListLit:
		return &ListLit{Pos: x.Pos, Items: cloneExprs(x.Items)}
	case *MapLit:
		out := &MapLit{Pos: x.Pos}
		if x.Pairs != nil {
			out.Pairs = make([]MapPair, 0, len(x.Pairs))
			for _, p := range x.Pairs {
				out.Pairs = append(out.Pairs, MapPair{Pos: p.Pos, Key: cloneExpr(p.Key), Value: cloneExpr(p.Value)})
			}
		}
		return out
	case *SetLit:
		return &SetLit{Pos: x.Pos, Items: cloneExprs(x.Items)}
	case *Throw:
		return &Throw{Pos: x.Pos, Code: cloneExpr(x.Code), Msg: cloneExpr(x.Msg)}
	case *Exit:
		return &Exit{Pos: x.Pos, Code: cloneExpr(x.Code), Msg: cloneExpr(x.Msg)}
	}
	panic("泛型复制没有接住的表达式节点：" + fmt.Sprintf("%T", e))
}

// CloneType 交出那一段写出来的类型的副本。
//
// en: CloneType copies one type annotation as written.
func CloneType(t Type) Type {
	if t == nil {
		return nil
	}
	switch x := t.(type) {
	case *NamedType:
		return &NamedType{Pos: x.Pos, Name: x.Name, Args: cloneTypes(x.Args)}
	case *FuncType:
		out := &FuncType{Pos: x.Pos, Ret: CloneType(x.Ret)}
		if x.Params != nil {
			out.Params = make([]FuncParam, 0, len(x.Params))
			for _, p := range x.Params {
				out.Params = append(out.Params, FuncParam{Typ: CloneType(p.Typ), Ref: p.Ref})
			}
		}
		return out
	}
	panic("泛型复制没有接住的类型节点：" + fmt.Sprintf("%T", t))
}

func cloneTypes(ts []Type) []Type {
	if ts == nil {
		return nil
	}
	out := make([]Type, 0, len(ts))
	for _, t := range ts {
		out = append(out, CloneType(t))
	}
	return out
}

func cloneTypeParams(ps []*TypeParam) []*TypeParam {
	if ps == nil {
		return nil
	}
	out := make([]*TypeParam, 0, len(ps))
	for _, p := range ps {
		out = append(out, &TypeParam{Pos: p.Pos, Name: p.Name})
	}
	return out
}

func cloneExprs(es []Expr) []Expr {
	if es == nil {
		return nil
	}
	out := make([]Expr, 0, len(es))
	for _, e := range es {
		out = append(out, cloneExpr(e))
	}
	return out
}

func cloneStructFields(fs []*StructLitField) []*StructLitField {
	if fs == nil {
		return nil
	}
	out := make([]*StructLitField, 0, len(fs))
	for _, f := range fs {
		out = append(out, &StructLitField{Pos: f.Pos, NameText: f.NameText, Value: cloneExpr(f.Value)})
	}
	return out
}

func cloneDecl(d *Decl) *Decl {
	if d == nil {
		return nil
	}
	return &Decl{
		Pos: d.Pos, Export: d.Export, IsConst: d.IsConst, Shared: d.Shared,
		NameText: d.NameText, Typ: CloneType(d.Typ), Value: cloneExpr(d.Value),
	}
}

// 两处 panic 的话直接用 `%T`：炸的时候要说得出是哪一种节点没接住，类型名就是最准的那一句。
//
// en: The two panic messages use `%T` outright: when stopping, the message has to say which node
// kind went unhandled, and the type name is the truest thing to say.

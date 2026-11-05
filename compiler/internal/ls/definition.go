// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// definition.go 是"跳转定义"那一条：编辑器把光标停在一个名字上问"这个名字定义在哪儿"。
// 认名字的几条路与悬停共用同一套（调用点、点号后面的成员、名字绑到的声明、本文件里的类型名、
// 引进来的名字），差别只在悬停返回一段文字、这一条返回一个位置。
//
// en: definition.go is the "go to definition" answer: the editor rests the cursor on a name and
// asks where that name is defined. The ways of recognising a name are shared with hover (a call
// site, the member after a dot, the declaration a name binds to, a type name in this file, an
// imported name); the only difference is that hover returns text while this returns a position.
package ls

import (
	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
)

// Location 是协议里的那一个位置：哪一份文件、哪一行哪一列到哪一行哪一列。
//
// en: Location is the protocol's answer: which file, and from which line and column to which.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Definition 返回光标处那一个名字的定义位置。认不出来就返回空：
// 标准库那些动作、内建类型与组名都没有写在源码里的一行，指不过去。
//
// en: Definition returns where the name under the cursor is defined. When it cannot tell, it
// returns nothing - the standard-library actions, the built-in types and the group names are
// not written on a line of source, so there is nothing to point at.
func Definition(path, text string, at Position) []Location {
	e, ok := edgeOf(text, at)
	if !ok {
		return nil
	}
	lines := splitLines(text)
	line := lines[at.Line]
	col := byteColOf(line, at.Character)
	to := col
	for to < len(line) && isNameByte(line[to]) {
		to++
	}
	name := line[e.word:to]
	if name == "" {
		return nil
	}
	prog, facts := inspect(path, text)
	if facts == nil {
		return nil
	}
	ln, bc := at.Line+1, e.word+1
	var recvT check.Type
	if e.dot >= 0 && e.recv != "" {
		recvT = receiverType(facts, ln, e.recvCol+1, e.recv)
	}
	var node any
	if sig, n, _ := callHover(facts, recvT, ln, bc, name); sig != "" {
		node = n
	}
	if node == nil && e.dot >= 0 {
		if sig, n, _ := memberHover(path, facts, ln, e.recvCol+1, e.recv, name); sig != "" {
			node = n
		}
	}
	if node == nil {
		if b, isOk := bindAt(facts, ln, bc); isOk {
			node = b.Decl
		}
	}
	if node == nil {
		for key := range facts.Declared {
			if check.SealedName(key) == name {
				node = declNode(prog, name)
				break
			}
		}
	}
	if node == nil {
		for key, nm := range facts.Imported {
			if file, local, isOk := splitNameKey(key); isOk && local == name && file == path {
				node = nm.Decl
			}
		}
	}
	if node == nil {
		return nil
	}
	file, start, found := locate(prog, node)
	if !found {
		return nil
	}
	// 范围落在定义那一行的开头：节点带的位置是它自己那一个记号（`method`、`let`、`class`），
	// 名字在那一行的第几列要读那一行的文本才知道，而包里的定义不在编辑器打开的这份文本里。
	// 跳转要的是"到那一行去"，列从 0 起就够了。协议里的行列从 0 数，语言里那一套从 1 数。
	return []Location{{
		URI: uriFromPath(file),
		Range: Range{
			Start: Position{Line: start.Line - 1, Character: 0},
			End:   Position{Line: start.Line - 1, Character: 0},
		},
	}}
}

// locate 找那一个节点写在哪一份文件的哪一处：先在这一次编译涉及的那些文件里按指针比一遍，
// 顶层定义、身上的字段与方法、方法的参数都算数。节点自己带着位置，但位置不记它是哪一份文件，
// 所以这一步不能省——包里的定义与工程里的定义在这一趟是两份不同的文件。
//
// en: locate finds which file holds that node and where: it compares pointers through the files
// this compile touched, counting a top-level definition, the fields and methods on it and a
// method's parameters. A node carries its own position but a position does not say which file it
// came from, so this step cannot be skipped - a definition in a package and one in the project
// are two different files in this run.
func locate(prog *check.Program, node any) (string, ast.Pos, bool) {
	if prog == nil {
		return "", ast.Pos{}, false
	}
	for _, f := range prog.Files {
		for _, d := range f.Defs {
			if p, ok := matchDef(d, node); ok {
				return f.Path, p, true
			}
		}
	}
	return "", ast.Pos{}, false
}

// matchDef 比一个顶层定义：整条定义、身上的字段与方法、方法的参数与返回，都可能是悬停指着的那个节点。
// enum 的成员是值而不是指针，按名字比那一步在调用处已经落下，这里只认带指针的那几类。
//
// en: matchDef compares one top-level definition: the definition itself, the fields and methods
// on it, and a method's parameters can all be the node hover pointed at. Enum members are values
// rather than pointers, so the name-by-name step happens at the call site; here only the pointer
// kinds are compared.
func matchDef(d ast.Def, node any) (ast.Pos, bool) {
	if d == node {
		return d.Where(), true
	}
	switch x := d.(type) {
	case *ast.Func:
		for _, p := range x.Params {
			if p == node {
				return p.Where(), true
			}
		}
	case *ast.Object:
		for _, fl := range x.Fields {
			if fl == node {
				return fl.Where(), true
			}
		}
		for _, m := range x.Methods {
			if p, ok := matchDef(m, node); ok {
				return p, true
			}
		}
	case *ast.Interface:
		for _, fl := range x.Fields {
			if fl == node {
				return fl.Where(), true
			}
		}
		for _, m := range x.Methods {
			if p, ok := matchDef(m, node); ok {
				return p, true
			}
		}
	}
	return ast.Pos{}, false
}

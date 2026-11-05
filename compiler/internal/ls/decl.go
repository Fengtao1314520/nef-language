// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// decl.go 是给工具那一侧的另一个入口：只知道一个名字时，找出它写下的地方，连带它上面那一段注释。
// 编辑器悬停走的是"光标在哪一行哪一列"那一条路（complete.go），调试器手上只有名字与它在哪一份
// 文件里——变量栏、悬停取值、监视表达式都是这样。两处用的是同一套账：解析结果与检查表。
//
// en: decl.go is the other door for the tooling side: given only a name, find where it was written
// and the comment above it. The editor's hover comes in by cursor position (complete.go); the
// debug adapter holds a name and the file it lives in - the variables view, hover-while-paused and
// watch expressions all look like that. Both doors read the same books: the parsed program and the
// checker's tables.
package ls

import (
	"os"

	"neflang.org/nf/internal/ast"
)

// DocForName 返回一个名字写下的地方上面那一段注释（排好版的三段：中文、英文、示例）。
// path 是调试器给的文件名：读的是盘上那一份，也就是这一次运行的产物对应的源文件。
// 名字找不到、文件读不到、或者那一个声明上面没写注释，都返回空串。
//
// en: DocForName returns the comment written above where that name was declared, laid out as the
// three sections (Chinese, English, example). The path is the file the debug adapter names: it reads
// what is on disk, the source the running artifact was built from. A name it cannot find, a file it
// cannot read, or a declaration with nothing above it all give an empty string.
func DocForName(path, name string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	s := newChecked(path, text)
	if s.facts == nil {
		return ""
	}
	node := s.declNamed(name)
	if node == nil {
		return ""
	}
	file, at, ok := declPos(s.prog, node)
	if !ok {
		return ""
	}
	lines := fileLines(s.prog, path, text, file)
	if lines == nil || at.Line < 1 || at.Line > len(lines)+1 {
		return ""
	}
	return renderDoc(docAbove(lines, at.Line))
}

// declNamed 先查检查表上的绑定（局部量、参数、字段都记在那儿），再查顶层定义与它们身上的成员。
//
// en: declNamed asks the checker's bindings first (locals, parameters and fields are recorded
// there), then the top-level definitions and the members on them.
func (s source) declNamed(name string) any {
	for n, b := range s.facts.Binds {
		if n.Text != name {
			continue
		}
		if b.Param != nil {
			return b.Param
		}
		if b.Decl != nil {
			return b.Decl
		}
	}
	for _, f := range s.files() {
		for _, d := range f.Defs {
			if d.Name() == name {
				return d
			}
			switch x := d.(type) {
			case *ast.Object:
				if node := memberNamed(x.Fields, x.Methods, name); node != nil {
					return node
				}
			case *ast.Interface:
				if node := memberNamed(x.Fields, x.Methods, name); node != nil {
					return node
				}
			case *ast.Enum:
				for _, m := range x.Members {
					if m.NameText == name {
						return m
					}
				}
			}
		}
	}
	return nil
}

// memberNamed 在一个类型身上的字段与方法里按名字找那一个节点。
//
// en: memberNamed looks for that name among the fields and methods on one type.
func memberNamed(fields []*ast.Field, methods []*ast.Func, name string) any {
	for _, f := range fields {
		if f.NameText == name {
			return f
		}
	}
	for _, m := range methods {
		if m.NameText == name {
			return m
		}
	}
	return nil
}

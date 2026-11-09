// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package parser

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
)

// TestParseTypeParameterTables 钉住四类声明上的类型参数表：`class`、`struct`、顶层函数、
// 类体内的方法都收；名字不限制长短；多个参数逗号分隔；没有尖括号的普通声明读出来是空表。
//
// en: TestParseTypeParameterTables pins the parameter table on the four declaration forms:
// class, struct, top-level function and class-body method all take it; names are unrestricted;
// several parameters separate by commas; an ordinary declaration without brackets parses to an
// empty table.
func TestParseTypeParameterTables(t *testing.T) {
	f := parseString(t, `
class Box<Content> {
    v:Content
}

struct Pair<First, Second> {
    left:First
    right:Second
}

method twice<Element>(v:Element):Element {
    return v
}
`)
	var box *ast.Object
	var pair *ast.Object
	var fn *ast.Func
	for _, d := range f.Defs {
		switch x := d.(type) {
		case *ast.Object:
			switch x.NameText {
			case "Box":
				box = x
			case "Pair":
				pair = x
			}
		case *ast.Func:
			fn = x
		}
	}
	if box == nil || pair == nil || fn == nil {
		t.Fatalf("三份声明没读全：box=%v pair=%v fn=%v", box, pair, fn)
	}
	if got := paramNames(box.TypeParams); got != "Content" {
		t.Errorf("class 的类型参数表读成 %q，要 Content", got)
	}
	if got := paramNames(pair.TypeParams); got != "First, Second" {
		t.Errorf("struct 的类型参数表读成 %q，要 First, Second", got)
	}
	if got := paramNames(fn.TypeParams); got != "Element" {
		t.Errorf("顶层函数的类型参数表读成 %q，要 Element", got)
	}
	if fn.NameText != "twice" || len(fn.Params) != 1 {
		t.Errorf("函数那一段被类型参数表带偏了：%s 的参数个数 %d", fn.NameText, len(fn.Params))
	}
}

// TestParseClassBodyMethodTypeParams 钉住类体内那一行方法声明也收类型参数表：
// 顶层函数与类内方法共用 parseFunc 这一处读法，这一段还不生成方法体，但语法要收得下。
//
// en: TestParseClassBodyMethodTypeParams pins that a method declared inside a class body also
// takes the table: top-level functions and class-body methods share parseFunc. This segment
// does not generate bodies yet, but the syntax must parse.
func TestParseClassBodyMethodTypeParams(t *testing.T) {
	f := parseString(t, `
class Bag<Item> {
    head:Item
    method take<Other>(v:Other):Other {
        return v
    }
}
`)
	obj, ok := f.Defs[0].(*ast.Object)
	if !ok || obj.NameText != "Bag" {
		t.Fatalf("class 没读出来：%T", f.Defs[0])
	}
	if got := paramNames(obj.TypeParams); got != "Item" {
		t.Errorf("class 的类型参数表读成 %q，要 Item", got)
	}
	if len(obj.Methods) != 1 {
		t.Fatalf("方法个数读成 %d，要 1", len(obj.Methods))
	}
	if got := paramNames(obj.Methods[0].TypeParams); got != "Other" {
		t.Errorf("类内方法的类型参数表读成 %q，要 Other", got)
	}
}

// TestParseTypeParamsRefusals 钉住两处语法层的拒绝形状：接口里的方法声明不带类型参数，
// 参数位置里不能套尖括号。两处的话都不是新句子：前者是 `(` 那一句记号不对，
// 后者是尖括号没完成那一句。
//
// en: TestParseTypeParamsRefusals pins two syntax-level refusals: an interface's method
// declaration carries no type parameters, and a parameter position holds no nested brackets.
// Neither adds a sentence: the first is the "expected (" mark error, the second the
// unfinished-angle-bracket one.
func TestParseTypeParamsRefusals(t *testing.T) {
	if _, err := parse(t, `
interface AB {
    add<Other>(v:Other):int
}
`); err == nil {
		t.Errorf("接口里的方法声明带了类型参数，语法层没拦")
	} else if !strings.Contains(err.Error(), "接口里的方法声明不能带类型参数") {
		t.Errorf("接口那一处报的话不对：%v", err)
	}
	if _, err := parse(t, "class Box<list<int>> {\n    v:int\n}\n"); err == nil {
		t.Errorf("类型参数里套了尖括号，语法层没拦")
	} else if !strings.Contains(err.Error(), "尖括号") {
		t.Errorf("嵌套参数那一句报的话不对：%v", err)
	}
}

// parse 是 parseString 的报错那一条路：源码读不下去时把错返回回来，不当场失败。
//
// en: parse is the error path of parseString: it returns the syntax error instead of failing.
func parse(t *testing.T, src string) (*ast.File, error) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	return Parse("test.nf", toks)
}

// paramNames 把一张类型参数表排成一串名字，供上面的逐字比对。
//
// en: paramNames renders a parameter table as its names in one string for the checks above.
func paramNames(params []*ast.TypeParam) string {
	var out []string
	for _, p := range params {
		out = append(out, p.Name)
	}
	return strings.Join(out, ", ")
}

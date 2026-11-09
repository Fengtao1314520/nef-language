// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package ast_test

import (
	"reflect"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/parser"
)

// 泛型那一段用的那份语法树复制器（internal/ast/clone.go）在这里钉住两件事：
// 一、副本的形状与原文一致，逐句逐式都在；
// 二、副本与原文不共用任何一个节点——共用的时候不是报错难看，是两份实例读到同一份类型结论，
// 生成的是错的机器码。
//
// The syntax-tree copier used by generics (internal/ast/clone.go) is pinned here on two things:
// one, the copy has the same shape as the original, statement for statement and expression for
// expression; two, the copy shares no node with the original - a shared node is not ugly error
// text but two instances reading one set of type conclusions, which emits wrong machine code.

// cloneCoverSrc 把语句与表达式的每一种节点都写进去一遍。写法照 `samples/rpn.nf` 那几份示例：
// `switch` 的分支直接写标签，`class` 体内的方法不写返回类型就是没有返回值，赋值只有 `=`。
//
// en: cloneCoverSrc writes every statement and expression node kind once. The spelling follows
// the samples: a switch branch is its labels with a body, a class-body method with no return
// type returns nothing, and assignment is only `=`.
const cloneCoverSrc = `method helper(v:int):int {
    return v
}

struct Point {
    x:int
    y:int
}

class Node {
    next:object
}

method cover(v:int):int {
    let s:string = "甲"
    let c:fn(int):int = helper
    let p:Point = Point{x: 1, y: 2}
    let n:Node = new Node(next: null)
    s = s + "乙"
    v++
    helper(v)
    if (v < 2) {
        v = 3
    } else if (v < 4) {
        v = 5
    } else {
        v = 6
    }
    while (v < 10) {
        v = v + 1
        if (v == 7) {
            continue
        }
        if (v == 8) {
            break
        }
    }
    for (i:int in 0 until 3) {
        v = v + i
    }
    for (one:string in s.split(" ")) {
        v = length(one) + v
    }
    switch (v) {
        1, 2 {
            v = 0
        }
        default {
            v = 1
        }
    }
    let xs:list<int> = [1, 2, 3]
    let d:dict<string, int> = {"甲": 1}
    let st:hashset<int> = {1, 2}
    let sum:int = xs[0] + d[s] + v - p.x + helper(v)
    let box:object = n as object
    if (n is Node) {
        throw (7, "话")
    }
    try {
        v = -v
        exit (8, "话二")
    } catch (code, msg, at, trace) {
        v = code
    } finally {
        v = v + 1
    }
    console.print(!c)
    return v
}

method quiet():int {
    let a:int = 0
    if (a < 1) {
    }
    return
}
`

// TestCloneKeepsEveryNode 钉住副本的节点一个不少、一个不多，且与原文没有任何一个指针相同。
//
// en: TestCloneKeepsEveryNode pins that the copy has exactly the same node kinds, no more and
// no fewer, and shares not one pointer with the original.
func TestCloneKeepsEveryNode(t *testing.T) {
	f := parseClone(t, cloneCoverSrc)
	fn := findFunc(t, f, "cover")
	cp := ast.CloneFunc(fn)

	want := nodeShape(fn)
	got := nodeShape(cp)
	if got != want {
		t.Errorf("副本的形状与原文不一样：\n原文 %s\n副本 %s", want, got)
	}
	origin := map[uintptr]bool{}
	for _, p := range nodePointers(fn) {
		origin[p] = true
	}
	for _, p := range nodePointers(cp) {
		if origin[p] {
			t.Fatalf("副本与原文共用了一个节点（%d），两份实例会读到同一份结论", p)
		}
	}
	if len(nodePointers(cp)) < 120 {
		t.Errorf("副本的节点数只有 %d，源码那一份覆盖不全", len(nodePointers(cp)))
	}
}

// TestCloneNilMeansNil 钉住五处靠 nil 说"没写"的形状：不带值的 `return`、没有 else 的 `if`、
// 没有返回类型的方法、没有默认值的参数、`for` 两种形式里各自不用的那一段。
// 复制时把 nil 变成非 nil 就是凭空多写了一句。
//
// en: TestCloneNilMeansNil pins the five shapes that use nil to mean "not written": a return
// without a value, an if without else, a method with no return type, a parameter with no
// default, and the half of each for-form that that form does not use. Turning nil into non-nil
// while copying invents a sentence that was never written.
func TestCloneNilMeansNil(t *testing.T) {
	f := parseClone(t, `method quiet(v:int) {
    let a:int = 0
    if (a < 1) {
    }
    for (i:int in 0 until 2) {
        a = a + i
    }
    for (one:string in "甲,乙".split(",")) {
        a = length(one) + a
    }
    return
}
`)
	fn := findFunc(t, f, "quiet")
	cp := ast.CloneFunc(fn)
	if len(cp.Body) != len(fn.Body) {
		t.Fatalf("语句条数变了：%d 对 %d", len(cp.Body), len(fn.Body))
	}
	if cp.Ret != nil {
		t.Errorf("没有返回类型的方法被复制出一个类型：%v", cp.Ret)
	}
	if cp.Params[0].Default != nil {
		t.Errorf("没有默认值的参数被复制出一个默认值：%v", cp.Params[0].Default)
	}
	last := cp.Body[len(cp.Body)-1].(*ast.Return)
	if last.Val != nil {
		t.Errorf("不带值的 return 被复制出一个值：%v", last.Val)
	}
	ifst := cp.Body[1].(*ast.If)
	if ifst.HasElse || ifst.Else != nil {
		t.Errorf("没有 else 的 if 被复制出 else：%v %v", ifst.HasElse, ifst.Else)
	}
	rangeFor := cp.Body[2].(*ast.For)
	if !rangeFor.Ranged && rangeFor.Iter != nil {
		t.Errorf("区间那一种 for 被复制出一个容器：%v", rangeFor.Iter)
	}
	if rangeFor.From == nil || rangeFor.Until == nil {
		t.Errorf("区间那一种 for 的起点终点丢了")
	}
	iterFor := cp.Body[3].(*ast.For)
	if iterFor.Ranged && (iterFor.From != nil || iterFor.Until != nil) {
		t.Errorf("遍历那一种 for 被复制出区间：%v %v", iterFor.From, iterFor.Until)
	}
	if got, want := nodeShape(cp), nodeShape(fn); got != want {
		t.Errorf("副本形状对不上：\n%s\n对\n%s", got, want)
	}
}

// TestCloneObjectCarriesMethods 钉住整份声明的副本：字段与方法各是新节点，
// 方法的名字与位置照旧（报错要指回写它的那一行）。
//
// en: TestCloneObjectCarriesMethods pins the copy of a whole declaration: fields and methods get
// fresh nodes while method names and positions stay - an error must still point at the line that
// wrote it.
func TestCloneObjectCarriesMethods(t *testing.T) {
	f := parseClone(t, `class Box<Content> {
    v:Content

    method get():Content {
        return v
    }
}
`)
	obj, ok := f.Defs[0].(*ast.Object)
	if !ok {
		t.Fatalf("class 没读出来：%T", f.Defs[0])
	}
	cp := ast.CloneObject(obj)
	if len(cp.Methods) != 1 || cp.Methods[0].NameText != "get" {
		t.Fatalf("方法没跟着复制：%v", cp.Methods)
	}
	if cp.Methods[0].Pos.Line != obj.Methods[0].Pos.Line {
		t.Errorf("方法的位置变了：第 %d 行对第 %d 行", cp.Methods[0].Pos.Line, obj.Methods[0].Pos.Line)
	}
	if reflect.ValueOf(cp.Methods[0]).Pointer() == reflect.ValueOf(obj.Methods[0]).Pointer() {
		t.Errorf("方法与原文是同一个节点")
	}
}

// nodeShape 把一棵子树按出现顺序排成节点类型名的一串，供形状比对。
//
// en: nodeShape renders a subtree as its node type names in visit order, for the shape compare.
func nodeShape(v any) string {
	var b strings.Builder
	walkShape(reflect.ValueOf(v), &b)
	return b.String()
}

func walkShape(val reflect.Value, b *strings.Builder) {
	if !val.IsValid() {
		b.WriteString("<nil> ")
		return
	}
	switch val.Kind() {
	case reflect.Pointer, reflect.Interface:
		if val.IsNil() {
			b.WriteString("<nil> ")
			return
		}
		if val.Kind() == reflect.Pointer {
			b.WriteString(val.Type().Elem().Name() + " ")
		}
		walkShape(val.Elem(), b)
	case reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			walkShape(val.Index(i), b)
		}
	case reflect.Struct:
		if val.Type().PkgPath() != "neflang.org/nf/internal/ast" {
			return
		}
		for i := 0; i < val.NumField(); i++ {
			walkShape(val.Field(i), b)
		}
	}
}

// nodePointers 收下一棵子树里所有 AST 节点的地址：副本与原文一个都不能相同。
//
// en: nodePointers gathers the address of every AST node in a subtree: not one may match
// between the copy and the original.
func nodePointers(v any) []uintptr {
	var out []uintptr
	collectPointers(reflect.ValueOf(v), &out)
	return out
}

func collectPointers(val reflect.Value, out *[]uintptr) {
	if !val.IsValid() {
		return
	}
	switch val.Kind() {
	case reflect.Pointer:
		if val.IsNil() {
			return
		}
		if val.Type().Elem().PkgPath() == "neflang.org/nf/internal/ast" {
			*out = append(*out, val.Pointer())
		}
		collectPointers(val.Elem(), out)
	case reflect.Interface:
		if val.IsNil() {
			return
		}
		collectPointers(val.Elem(), out)
	case reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			collectPointers(val.Index(i), out)
		}
	case reflect.Struct:
		if val.Type().PkgPath() != "neflang.org/nf/internal/ast" {
			return
		}
		for i := 0; i < val.NumField(); i++ {
			collectPointers(val.Field(i), out)
		}
	}
}

func parseClone(t *testing.T, src string) *ast.File {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	return f
}

func findFunc(t *testing.T, f *ast.File, name string) *ast.Func {
	t.Helper()
	for _, d := range f.Defs {
		if fn, ok := d.(*ast.Func); ok && fn.NameText == name {
			return fn
		}
	}
	t.Fatalf("没有读到函数 %s", name)
	return nil
}

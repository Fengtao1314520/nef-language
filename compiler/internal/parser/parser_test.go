// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
)

func parseString(t *testing.T, src string) *ast.File {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := Parse("test.nf", toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	return f
}

// decls 取出顶层的 let／const（它们在语法树里算定义）。
func decls(f *ast.File) []*ast.Decl {
	var out []*ast.Decl
	for _, d := range f.Defs {
		if dc, ok := d.(*ast.Decl); ok {
			out = append(out, dc)
		}
	}
	return out
}

func TestParseInterfaceStructClass(t *testing.T) {
	f := parseString(t, `
interface AB {
    a:int
    b:float
}

struct B base AB {
    c:boolean
}

interface A base AB {
    add(v:float):float
}

class C base A {
    method add(v:float):float { return a + b + v }
}

enum D { a = 0, b, c }
`)
	if len(f.Defs) != 5 {
		t.Fatalf("定义数不对：%d", len(f.Defs))
	}
	iface, ok := f.Defs[0].(*ast.Interface)
	if !ok || len(iface.Fields) != 2 {
		t.Fatalf("接口字段没收齐：%+v", f.Defs[0])
	}
	obj := f.Defs[1].(*ast.Object)
	if obj.Kind != "struct" || obj.Base == nil || len(obj.Fields) != 1 {
		t.Fatalf("struct 形状不对：%+v", obj)
	}
	c := f.Defs[3].(*ast.Object)
	if len(c.Methods) != 1 || c.Methods[0].NameText != "add" || c.Methods[0].Ret == nil {
		t.Fatalf("class 方法不对：%+v", c.Methods)
	}
	e := f.Defs[4].(*ast.Enum)
	if len(e.Members) != 3 || e.Members[2].Value != nil {
		t.Fatalf("枚举成员不对：%+v", e.Members)
	}
}

func TestParseStatements(t *testing.T) {
	f := parseString(t, `
let items:list<string> = ["书包", "笔记本"]
const want:int = 3
for (i:int in 0 until 10) {
    items.add("雨伞")
}
for (s:string in items) {
    if (s.contains("雨")) {
        console.print("有伞")
    } else {
        console.print("没伞", length(items))
    }
}
while (want > 0) {
}
switch (items[0]) {
    "a", "b" { console.print("甲") }
    default { }
}
try {
    let n:int = items[0].to_int()
} catch (code, msg) {
    throw (code, msg)
}
exit (100, "收工")
`)
	if len(f.Stmts) != 8 {
		t.Fatalf("顶层语句数不对：%d", len(f.Stmts))
	}
	if len(f.Defs) != 2 {
		t.Fatalf("顶层声明该进 Defs：%d", len(f.Defs))
	}
	// 顶层的 let／const 两边都挂：Defs 给可见性与 import 用，Stmts 保住书写顺序。
	if _, ok := f.Stmts[0].(*ast.Decl); !ok {
		t.Fatalf("顶层第一条语句该是那个 let，实际 %T", f.Stmts[0])
	}
	for _, s := range f.Stmts {
		if s == nil {
			t.Fatal("有空语句")
		}
	}
}

func TestParseExpressionsAndTypes(t *testing.T) {
	f := parseString(t, `
let rows:list<dict<string, int>> = []
let m:int = 1 + 2 * 3
let b:boolean = m > 0 && m != 2
let s:string = "a" + m.to_string()
let u:unicode = '中'
let o:object = 42 as object
let back:int = o as int
let p:Point = Point { x: 1, y: 2 }
let c:C = new Circle(r: 2.0)
let n:int = -7 % 2
let sh:int = 1 << 3
let d:dict<string, int> = {"a": 1, "b": 2}
let hs:hashset<int> = {1, 2, 3}
`)
	ds := decls(f)
	if len(ds) != 13 {
		t.Fatalf("声明条数不对：%d", len(ds))
	}
	if _, ok := ds[0].Typ.(*ast.NamedType).Args[0].(*ast.NamedType); !ok {
		t.Fatalf("容器套容器没读出来：%+v", ds[0].Typ)
	}
	if _, ok := ds[6].Value.(*ast.Convert); !ok {
		t.Fatalf("装箱没读成 as：%T", ds[6].Value)
	}
	if _, ok := ds[8].Value.(*ast.New); !ok {
		t.Fatalf("new 没读出来：%T", ds[8].Value)
	}
	if _, ok := ds[11].Value.(*ast.MapLit); !ok {
		t.Fatalf("dict 字面量：%T", ds[11].Value)
	}
	if _, ok := ds[12].Value.(*ast.SetLit); !ok {
		t.Fatalf("hashset 字面量：%T", ds[12].Value)
	}
}

func TestPrecedenceFollowsSpec125(t *testing.T) {
	f := parseString(t, "let x:int = 1 + 2 * 3")
	bin := decls(f)[0].Value.(*ast.Binary)
	if bin.Op != "+" {
		t.Fatalf("加法该在外面，实际 %s", bin.Op)
	}
	if r, ok := bin.Right.(*ast.Binary); !ok || r.Op != "*" {
		t.Fatalf("乘法该先结合")
	}
}

func TestAsBindsTighterThanCompare(t *testing.T) {
	f := parseString(t, "let x:boolean = o as int > 3")
	bin := decls(f)[0].Value.(*ast.Binary)
	if bin.Op != ">" {
		t.Fatalf("外层该是比较，实际 %s", bin.Op)
	}
	if _, ok := bin.Left.(*ast.Convert); !ok {
		t.Fatalf("`as` 该先结合：%T", bin.Left)
	}
}

func TestIsTestNode(t *testing.T) {
	f := parseString(t, "let x:boolean = one is Circle")
	if _, ok := decls(f)[0].Value.(*ast.Test); !ok {
		t.Fatalf("is 没读成判类型节点：%T", decls(f)[0].Value)
	}
}

func TestSemicolonAccepted(t *testing.T) {
	f := parseString(t, "let a:int = 1; let b:int = 2;\n")
	if len(decls(f)) != 2 {
		t.Fatalf("分号该当终止符：%d", len(f.Defs))
	}
}

func TestMissingTypeAnnotationIsError(t *testing.T) {
	toks, err := lexer.Lex("let x = 1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse("t.nf", toks)
	if err == nil || !strings.Contains(err.Error(), "必须标类型") {
		t.Fatalf("省类型标注该报错，实际 %v", err)
	}
}

// TestParseTypeAlias 钉类型别名声明的形状：`type 名字 = 类型`，右边照 parseType 读，
// 尖括号与 `export` 都要识别。
func TestParseTypeAlias(t *testing.T) {
	f := parseString(t, "type Names = list<string>\nexport type Row2 = Row\n")
	if len(f.Defs) != 2 {
		t.Fatalf("两条定义该收下：%d", len(f.Defs))
	}
	a, ok := f.Defs[0].(*ast.TypeAlias)
	if !ok {
		t.Fatalf("第一条该是类型别名，实际 %T", f.Defs[0])
	}
	nt, ok := a.Typ.(*ast.NamedType)
	if !ok || nt.Name != "list" || len(nt.Args) != 1 {
		t.Fatalf("右边没读成 list<…>：%+v", a.Typ)
	}
	b := f.Defs[1].(*ast.TypeAlias)
	if !b.Exported() || b.NameText != "Row2" {
		t.Fatalf("export 那一头没收对：%+v", b)
	}
}

func TestEnumMembersMayEndWithNewline(t *testing.T) {
	f := parseString(t, "enum Err {\n    bad_path = 100,\n    empty_input = 101\n}\n")
	e, ok := f.Defs[0].(*ast.Enum)
	if !ok {
		t.Fatalf("顶层该是一个 enum，实际 %T", f.Defs[0])
	}
	if len(e.Members) != 2 || e.Members[1].NameText != "empty_input" {
		t.Fatalf("成员没收全：%+v", e.Members)
	}
}

func TestSamplesParse(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "samples")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("找不到 samples 目录：%v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".nf") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		toks, err := lexer.Lex(string(src))
		if err != nil {
			t.Errorf("%s 词法报错：%v", e.Name(), err)
			continue
		}
		if _, err := Parse(e.Name(), toks); err != nil {
			t.Errorf("%s 语法报错：%v", e.Name(), err)
		}
	}
}

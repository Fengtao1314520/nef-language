// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// complete_test.go 覆盖编辑器要问的几件事：成员名清单与检查器那一条 switch 不许漂移，
// 每一种形状上实际点得出什么，自己写的类、枚举、包各自返回什么。
package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	nfast "neflang.org/nf/internal/ast"
)

// TestMemberNameListMatchesTheChecker 比两份东西：complete.go 那份候选名清单，
// 与 expr.go 里 stdMember 那一条函数体上每一个 case 写出来的名字。
//
// 加一条成员而忘了登进清单，补全就少一条，而编译照旧认——这种漏在屏幕上看不出来，
// 所以拿源码比，不靠人记。读法与报错文字那张表同一个路子：收源码里的字面量。
func TestMemberNameListMatchesTheChecker(t *testing.T) {
	set := token.NewFileSet()
	f, err := parser.ParseFile(set, "expr.go", nil, 0)
	if err != nil {
		t.Fatalf("读 expr.go：%v", err)
	}
	have := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "stdMember" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING {
					have[strings.Trim(lit.Value, `"`)] = true
				}
			}
			return true
		})
	}
	if len(have) == 0 {
		t.Fatal("stdMember 里一条 case 都没读到，这条测试成了空转")
	}
	listed := map[string]bool{}
	for _, n := range stdMemberNames {
		if listed[n] {
			t.Errorf("候选名清单里 %s 写了两回", n)
		}
		listed[n] = true
	}
	for n := range have {
		if !listed[n] {
			t.Errorf("stdMember 认 %q，候选名清单里没有它：补全会少这一条", n)
		}
	}
	for n := range listed {
		if !have[n] {
			t.Errorf("候选名清单里有 %q，stdMember 却不认：补出来的是编译不过的名字", n)
		}
	}
}

// TestContainerAndTextMembers 覆盖"哪一种形状有哪些成员"：这一侧由 stdMember 判定，清单只管候选名。
func TestContainerAndTextMembers(t *testing.T) {
	str := Type{Kind: Str, Name: "string"}
	iv := Type{Kind: Int, Name: "int"}
	list := Type{Kind: List, Name: "list", Args: []Type{iv}}
	dict := Type{Kind: Dict, Name: "dict", Args: []Type{str, iv}}
	set := Type{Kind: Set, Name: "hashset", Args: []Type{iv}}

	for _, c := range []struct {
		name  string
		base  Type
		want  []string
		notWt []string
	}{
		{"list", list, []string{"add", "sort", "first", "insert_at", "remove_at", "slice", "to_set"},
			[]string{"contains", "keys", "values", "get", "union", "trim"}},
		{"dict", dict, []string{"add", "get", "keys", "values", "contains", "remove"},
			[]string{"first", "last", "sort", "union", "add_all", "insert_at"}},
		{"hashset", set, []string{"add", "contains", "union", "intersect", "difference", "remove"},
			[]string{"get", "keys", "values", "first", "index_of"}},
		{"string", str, []string{"trim", "split", "replace", "starts_with", "index_of", "to_bytes", "to_upper"},
			[]string{"add", "remove", "keys", "union", "length"}},
	} {
		got := nameList(MembersOn("x", c.base, nil))
		for _, w := range c.want {
			if !has(got, w) {
				t.Errorf("%s 身上该点得出 %s，得到 %v", c.name, w, got)
			}
		}
		for _, w := range c.notWt {
			if has(got, w) {
				t.Errorf("%s 身上点不出 %s，却补了出来：%v", c.name, w, got)
			}
		}
	}

	// 签名里带上写出来的类型：这一行是补全右侧与悬停看到的，不能只给一个名字。
	got := MembersOn("xs", list, nil)
	if s := detailOf(got, "add"); s != "add(value: int)" {
		t.Errorf("list<int>.add 的签名写成 %q，要 add(value: int)", s)
	}
	if s := detailOf(got, "first"); s != "first(): int" {
		t.Errorf("list<int>.first 的签名写成 %q，要 first(): int", s)
	}
	if s := detailOf(MembersOn("xs", set, nil), "union"); s != "union(other: hashset<int>): hashset<int>" {
		t.Errorf("hashset.union 的签名写成 %q，要 union(other: hashset<int>): hashset<int>", s)
	}
	// 没有返回值的那几条不写冒号：`print(): ` 后面跟一个空与实际不符。
	if s := detailOf(MembersOn("s", str, nil), "trim"); s != "trim(): string" {
		t.Errorf("string.trim 的签名写成 %q", s)
	}
}

// TestGroupMembers 覆盖标准库那一侧：组名后面点出的是 stdFuncs 里的键，一条不多一条不少。
func TestGroupMembers(t *testing.T) {
	got := nameList(MembersOn("console", Type{}, nil))
	for _, w := range []string{"print", "print_error", "read"} {
		if !has(got, w) {
			t.Errorf("console 组里补不出 %s：%v", w, got)
		}
	}
	if has(got, "to_int") {
		t.Errorf("text.to_int 跑到了 console 组里：%v", got)
	}
	if s := detailOf(MembersOn("console", Type{}, nil), "print"); s != "print(...values)" {
		t.Errorf("console.print 那一行写成 %q，可变参数要写成 (...values)", s)
	}
	if s := StdSignature("http.get"); s != "http.get(url: string, timeout_ms: int): Response" {
		t.Errorf("悬停 http.get 那一行写成 %q", s)
	}
	names := GroupNames()
	if !has(names, "regular_expression") || !has(names, "process") {
		t.Errorf("组名清单少了东西：%v", names)
	}
	for _, g := range names {
		if !IsGroup(g) {
			t.Errorf("GroupNames 交回 %q，IsGroup 却不认它", g)
		}
	}
}

// TestMembersOnWrittenTypes 用自己写的类与枚举走一遍真检查：字段、方法、枚举值都从
// 检查器那几张表里来，不是另写一份名单。
func TestMembersOnWrittenTypes(t *testing.T) {
	const src = "class Page {\n    url: string\n    width: int\n\n    method open(to: string): boolean {\n        url = to\n        return true\n    }\n\n    method close(): int {\n        return 0\n    }\n}\n\nenum Kind {\n    add,\n    sub,\n    mul\n}\n\nlet p: Page = new Page(url: \"甲\", width: 2)\nconsole.print(p.open(\"乙\"))\nconsole.print(p.width)\nconsole.print(Kind.mul)\n"
	prog := &Program{Files: []*nfast.File{mustFile(t, "t.nf", src)}, Entry: "t.nf"}
	errs, facts := RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("这份例子本该过检查，实际：%v", msgs(errs))
	}
	page := Type{Kind: Class, Name: "Page"}
	got := MembersOn("p", page, facts)
	for _, w := range []string{"url", "width", "open", "close"} {
		if !has(nameList(got), w) {
			t.Errorf("Page 身上点不出 %s：%v", w, nameList(got))
		}
	}
	if s := detailOf(got, "open"); s != "open(to: string): boolean" {
		t.Errorf("方法那一行写成 %q，要带写出来的参数名", s)
	}
	if s := detailOf(got, "width"); s != "width: int" {
		t.Errorf("字段那一行写成 %q", s)
	}

	kinds := MembersOn("Kind", Type{Kind: Enum, Name: "Kind"}, facts)
	if names := nameList(kinds); !has(names, "add") || !has(names, "mul") {
		t.Errorf("枚举值点不出来：%v", names)
	}
	if s := detailOf(kinds, "mul"); s != "mul = 2" {
		t.Errorf("枚举值那一行写成 %q，要带它那个数", s)
	}

	// 类型名本身也识别得出来（悬停在 Page 上要知道那是一个 class）。
	d, ok := facts.Declared["Page"]
	if !ok || d.Kind != "class" {
		t.Errorf("类型表里 Page 不对：%+v（%v）", d, ok)
	}
	if s := facts.Signature("Page.open"); s != "Page.open(to: string): boolean" {
		t.Errorf("悬停一个方法写成 %q", s)
	}
	if s := facts.Signature("没有这个"); s != "" {
		t.Errorf("认不出的键要交回空串，得到 %q", s)
	}
}

// TestPackageMembers 交出去的那一半：包里 export 的函数与类型名补得出来，没 export 的补不出来。
func TestPackageMembers(t *testing.T) {
	root := "libs/test_run"
	facts := newFacts()
	facts.Funcs[root+"/begin_run"] = &nfast.Func{NameText: "begin_run", Export: true}
	facts.Funcs[root+"/hidden_helper"] = &nfast.Func{NameText: "hidden_helper"}
	facts.Funcs[root+"/Run.open"] = &nfast.Func{NameText: "open", Export: true, IsMethod: true}
	facts.Sigs[root+"/begin_run"] = Signature{Params: []Param{{Name: "exe", Typ: Type{Kind: Str, Name: "string"}},
		{Name: "profile", Typ: Type{Kind: Str, Name: "string"}}}, Ret: Type{Kind: Class, Name: root + "/Run"}}
	facts.Sigs[root+"/hidden_helper"] = Signature{Ret: Type{Kind: Int, Name: "int"}}
	facts.Sigs[root+"/Run.open"] = Signature{Params: []Param{{Name: "to", Typ: Type{Kind: Str, Name: "string"}}},
		Ret: Type{Kind: Bool, Name: "boolean"}, Method: true, Owner: root + "/Run"}
	facts.Declared[root+"/Run"] = Declared{Kind: "class", Export: true}
	facts.Declared[root+"/Inside"] = Declared{Kind: "class"}
	// 包交出来的数：挂 export 的 const 列出来，没挂的与 shared 都不列。
	facts.TopValues[root+"/LIMIT"] = TopValue{Typ: Type{Kind: Int, Name: "int"}, Export: true,
		Decl: &nfast.Decl{NameText: "LIMIT", IsConst: true, Export: true}}
	facts.TopValues[root+"/priv"] = TopValue{Typ: Type{Kind: Int, Name: "int"},
		Decl: &nfast.Decl{NameText: "priv", IsConst: true}}
	facts.TopValues[root+"/counter"] = TopValue{Typ: Type{Kind: Int, Name: "int"}, Export: true, Shared: true,
		Decl: &nfast.Decl{NameText: "counter", Shared: true, Export: true}}

	got := PackageMembers(root, facts)
	names := nameList(got)
	for _, w := range []string{"begin_run", "Run", "LIMIT"} {
		if !has(names, w) {
			t.Errorf("包里交出的 %s 补不出来：%v", w, names)
		}
	}
	for _, w := range []string{"hidden_helper", "Inside", "open", "priv", "counter"} {
		if has(names, w) {
			t.Errorf("%s 不该出现在包的挂名后面（没 export、是别人的方法，或是还没给读的 shared）：%v", w, names)
		}
	}
	if s := detailOf(got, "begin_run"); s != "begin_run(exe: string, profile: string): Run" {
		t.Errorf("包里那条函数的一行写成 %q", s)
	}
	if s := detailOf(got, "LIMIT"); s != "LIMIT: int" {
		t.Errorf("包里那个数的一行写成 %q", s)
	}
	for _, m := range got {
		if m.Name == "LIMIT" && m.Doc == nil {
			t.Error("LIMIT 这一条没带上声明节点，悬停与补全读不到写在它上面的注释")
		}
	}

	// 挂名 → 包根那一张表按文件分开：两份文件各挂一个包时不能互相顶掉。
	facts.Packages[NameKey("a.nf", "tr")] = root
	if r, ok := ReceiverIsPackage("a.nf", "tr", facts); !ok || r != root {
		t.Errorf("a.nf 里挂的 tr 找不到包根：%q %v", r, ok)
	}
	if _, ok := ReceiverIsPackage("b.nf", "tr", facts); ok {
		t.Error("b.nf 没挂过 tr，却认下了")
	}
}

func nameList(in []Member) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		out = append(out, m.Name)
	}
	sort.Strings(out)
	return out
}

func detailOf(in []Member, name string) string {
	for _, m := range in {
		if m.Name == name {
			return m.Detail
		}
	}
	return ""
}

func has(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

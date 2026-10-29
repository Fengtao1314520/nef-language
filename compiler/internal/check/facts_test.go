// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import (
	"testing"

	"neflang.org/nf/internal/ast"
)

// 后端全靠 Facts 这一份结论。它一旦记漏或记错，问题要到 codegen 阶段才暴露，
// 所以这里把每一项都固定，改 check 时先过这一关。
func TestFactsForBackend(t *testing.T) {
	src := `class Circle {
    r:float
    method area():float { return 3.14159 * r * r }
}

struct Point {
    x:int
    y:int
}

enum Err { bad_path = 100, empty }

const limit:int = 10

method use_items(items:list<string>):int {
    items.add("雨伞")
    let up:string = "abc".to_upper()
    return length(items)
}

method call_it(n:int):int {
    return use_items(names) + n + Err.empty as int
}

let names:list<string> = ["书包"]
console.print(call_it(limit), names[0])
`
	prog := &Program{Files: []*ast.File{mustFile(t, "t.nf", src)}, Entry: "t.nf"}
	errs, f := RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("样例本身没过检查：%v", msgs(errs))
	}

	// ①容器与文本的成员调用要有落点名，不能是空串
	var addKey, upperKey string
	for _, ref := range f.Calls {
		switch ref.Std {
		case "list.add":
			addKey = ref.Std
		case "string.to_upper":
			upperKey = ref.Std
		}
	}
	if addKey == "" || upperKey == "" {
		var all []string
		for _, ref := range f.Calls {
			all = append(all, ref.Std)
		}
		t.Fatalf("成员调用的落点没记上：list.add=%q string.to_upper=%q，全部=%q", addKey, upperKey, all)
	}

	// ②方法与函数按规范名可查
	if _, ok := f.Funcs["Circle.area"]; !ok {
		t.Errorf("方法没进 Funcs：Circle.area 不在表里")
	}
	if _, ok := f.Funcs["use_items"]; !ok {
		t.Errorf("函数没进 Funcs：use_items 不在表里")
	}

	// ③字段偏移：class 的 r 是 8 字节 double，struct 的 x/y 各 8 字节
	if got := f.ObjectSize("Circle"); got != 8 {
		t.Errorf("Circle 的对象大小是 %d，要 8", got)
	}
	offs := f.FieldOffsets("Point")
	if len(offs) != 2 || offs[0] != 0 || offs[1] != 8 {
		t.Errorf("Point 的字段偏移是 %v，要 [0 8]", offs)
	}
	if got := f.Sizeof(Type{Kind: Unicode}); got != 4 {
		t.Errorf("unicode 宽度是 %d，要 4", got)
	}
	if got := f.Sizeof(Type{Kind: Bool}); got != 1 {
		t.Errorf("boolean 宽度是 %d，要 1", got)
	}
	if got := f.Sizeof(Type{Kind: List, Name: "list"}); got != 8 {
		t.Errorf("容器句柄宽度是 %d，要 8", got)
	}

	// ④枚举成员的值
	if f.EnumVals["Err.bad_path"] != 100 || f.EnumVals["Err.empty"] != 101 {
		t.Errorf("枚举值记错：%v", f.EnumVals)
	}

	// ⑦名字是哪一类分得清
	if len(f.Binds) == 0 {
		t.Fatal("一个名字都没记上")
	}
	byName := map[string]Bind{}
	for n, b := range f.Binds {
		byName[n.Text] = b
	}
	if byName["limit"].By != BindGlobalConst {
		t.Errorf("顶层 const 认成了 %d", byName["limit"].By)
	}
	if byName["names"].By != BindGlobalLet || !byName["names"].Global {
		t.Errorf("顶层 let 认成了 %d（Global=%v）", byName["names"].By, byName["names"].Global)
	}
	if byName["n"].By != BindParam {
		t.Errorf("参数认成了 %d", byName["n"].By)
	}
	if byName["up"].By != BindLocal || byName["up"].Global {
		t.Errorf("块里的 let 认成了 %d（Global=%v）", byName["up"].By, byName["up"].Global)
	}
	// 顶层声明要能顺着 Decl 节点找到初值，静态槽编号靠它排
	if byName["names"].Decl == nil {
		t.Error("顶层 let 没带上它的 *ast.Decl 节点")
	}
}

// ②规范名：`import { f as g }` 不该让同一个函数体拿到两个 IR 名字。
func TestFactsCallUsesCanonicalName(t *testing.T) {
	b := mustFile(t, "b.nf", "export method helper(n:int):int { return n }\n")
	main := mustFile(t, "main.nf", "import { helper as h } from \"./b.nf\"\nconsole.print(h(1))\n")
	errs, f := RunWithFacts(&Program{Files: []*ast.File{main, b}, Entry: "main.nf"}, true)
	if len(errs) > 0 {
		t.Fatalf("样例没过检查：%v", msgs(errs))
	}
	for call, ref := range f.Calls {
		if ref.Kind == CallUser && ref.Func != "helper" {
			t.Errorf("调用记成了 %q，要规范名 helper（%d:%d）", ref.Func, call.Pos.Line, call.Pos.Col)
		}
	}
}

// 不开 Facts 时行为必须和原来一模一样——`nf check` 走的就是这条路。
func TestRunStillWorksWithoutFacts(t *testing.T) {
	errs := Run(&Program{Files: []*ast.File{mustFile(t, "t.nf", "let x:int = \"甲\"\n")}, Entry: "t.nf"})
	if len(errs) != 1 {
		t.Fatalf("该报一处类型不对，实际 %d 处：%v", len(errs), msgs(errs))
	}
}

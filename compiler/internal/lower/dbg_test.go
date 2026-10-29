// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// dbg_test.go 钉调试器第一个位置：debug 构建里每一条语句前多一条检查点，带着它的文件与行列；
// 不开这个开关，中间码与今天一字不差（那 507 条测试走的都是不开的那条路）。
package lower_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

// dbgSrc 里三条语句：函数体两条（第 2、3 行）、顶层一条（第 6 行）。
const dbgSrc = "method twice(n: int): int {\n    let m: int = n * 2\n    return m\n}\n\nconsole.print(twice(3))\n"

func front(t *testing.T, src string, debug bool) *ir.Prog {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: "t.nf", Debug: debug}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("前端：%v", errs)
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		t.Fatalf("降级：%v", err)
	}
	return p
}

// marks 收集全部检查点，形状是 "文件:行:列"，顺带把编号查一遍：宿主那一侧拿它当索引，
// 一个号对两处就等于断点会停错地方。
func marks(t *testing.T, p *ir.Prog) []string {
	t.Helper()
	var out []string
	seen := map[int]bool{}
	for _, f := range p.Funcs {
		for _, in := range f.Body {
			d, ok := in.(*ir.Dbg)
			if !ok {
				continue
			}
			if seen[d.ID] {
				t.Fatalf("检查点编号 %d 用了两回", d.ID)
			}
			seen[d.ID] = true
			out = append(out, fmt.Sprintf("%s:%d:%d", d.File, d.Line, d.Col))
		}
	}
	sort.Strings(out)
	return out
}

func TestDebugBuildMarksEveryStatement(t *testing.T) {
	if got := marks(t, front(t, dbgSrc, false)); len(got) != 0 {
		t.Fatalf("没开 debug 也插了检查点：%v", got)
	}

	want := []string{"t.nf:2:5", "t.nf:3:5", "t.nf:6:9"}
	// 第 6 行那一条不是从第 1 列起，是第 9 列：表达式语句的位置跟着那一个调用记号，
	// parser 把 Call.Pos 定在成员名那个位置（parser.go:1117），检查器指错也指同一个位置。
	// 调试器的箭头与报错落在同一个位置，比硬掰到行首要好。
	have := marks(t, front(t, dbgSrc, true))
	if strings.Join(have, " ") != strings.Join(want, " ") {
		t.Fatalf("检查点不对\n想要：%v\n得到：%v", want, have)
	}
}

// 检查点只报位置，不算任何数：把那些 dbg 行去掉，开与不开的两份中间码要一字不差。
func TestDebugBuildAddsNothingButCheckpoints(t *testing.T) {
	plain := ir.Dump(front(t, dbgSrc, false))
	dbg := ir.Dump(front(t, dbgSrc, true))

	var kept []string
	n := 0
	for _, line := range strings.Split(dbg, "\n") {
		if strings.HasPrefix(line, "  dbg #") {
			n++
			continue
		}
		kept = append(kept, line)
	}
	if n == 0 {
		t.Fatal("一份 dbg 中间码里连一条检查点都没有，这条测试就成了空转")
	}
	if got := strings.Join(kept, "\n"); got != plain {
		t.Errorf("去掉检查点之后与不开 debug 的那一份对不上：\n%s", firstDiff(got, plain))
	}
}

// 停下来要能读出局部量：每一帧得留下一张"槽位号 → 名字与形状"的表。今天这张表只活在
// 降级那一趟的作用域里（scopes），函数查完就丢了（lower.go 的 close），后端看不见。
func TestFrameRecordsItsLocals(t *testing.T) {
	const src = "method twice(n: int): int {\n    let m: int = n * 2\n    let s: string = \"甲\"\n    let f: float = 1.5\n    return m\n}\n"

	p := front(t, src, true)
	var twice *ir.Func
	for _, f := range p.Funcs {
		if f.Name == "twice" {
			twice = f
		}
	}
	if twice == nil {
		t.Fatalf("降级结果里找不到 twice：%+v", p.Funcs)
	}
	byName := map[string]ir.Local{}
	for _, l := range twice.Locals {
		byName[l.Name] = l
	}
	for name, want := range map[string]string{"n": "int", "m": "int", "s": "handle", "f": "float"} {
		got, ok := byName[name]
		if !ok {
			t.Errorf("帧上没记下 %s：%+v", name, twice.Locals)
			continue
		}
		if got.Kind != want {
			t.Errorf("%s 的形状记成 %q，要 %q", name, got.Kind, want)
		}
		if got.Slot < 0 || got.Slot >= twice.Slots {
			t.Errorf("%s 的槽号 %d 不在这一帧的 %d 格里", name, got.Slot, twice.Slots)
		}
	}
	if s := byName["s"]; s.Type != "string" {
		t.Errorf("给人看的那一种写法该是 string，得到 %q", s.Type)
	}
}

// 顶层那几句不在任何一帧上：编辑器的"静态"那一栏要列它们，靠的是降级时留下的这张名字表。
func TestDebugBuildRecordsStaticNames(t *testing.T) {
	const src = "let k: int = 21\nconst limit: int = 8\nlet tag: string = \"甲\"\nlet xs: list<int> = [1, 2]\nmethod twice(n: int): int {\n    return n * 2\n}\n"

	p := front(t, src, true)
	byName := map[string]ir.GlobalName{}
	for _, n := range p.Names {
		if _, dup := byName[n.Name]; dup {
			t.Fatalf("%s 记了两回：%+v", n.Name, p.Names)
		}
		byName[n.Name] = n
	}
	for name, want := range map[string]string{"k": "int", "limit": "int", "tag": "handle", "xs": "handle"} {
		got, ok := byName[name]
		if !ok {
			t.Errorf("静态名字表里没有 %s：%+v", name, p.Names)
			continue
		}
		if got.Kind != want {
			t.Errorf("%s 的形状记成 %q，要 %q", name, got.Kind, want)
		}
		if got.File != "t.nf" || got.Line < 1 || got.Line > 4 {
			t.Errorf("%s 写在哪一行记成 %s:%d", name, got.File, got.Line)
		}
		// 槽号就是 codegen 那张 Global 表的下标：越界的话调试器会去读不存在的槽。
		if got.Slot < 0 || got.Slot >= p.Globals {
			t.Errorf("%s 的槽号 %d 不在 %d 个静态槽里", name, got.Slot, p.Globals)
		}
	}
	if got := byName["tag"].Type; got != "string" {
		t.Errorf("给人看的那一种写法该是 string，得到 %q", got)
	}
	if plain := front(t, src, false); len(plain.Names) != 0 {
		t.Errorf("没开 debug 也记了静态名字：%+v", plain.Names)
	}
}

func nameDup(name string) string { return name }

func firstDiff(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			return fmt.Sprintf("第 %d 行起不同：\n  开：  %q\n  不开：%q", i+1, x, y)
		}
	}
	return ""
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// dbg_test.go 钉发射这一侧的两件事：debug 构建里每一条检查点换成机器码并记下它落在哪一个位置，
// 不开 debug 时数据段与代码都不该多出一个字节（那 507 条测试量的就是今天这份形状）。
package codegen_test

import (
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

const src = "method twice(n: int): int {\n    let m: int = n * 2\n    return m\n}\n\nconsole.print(twice(3))\n"

func front(t *testing.T, debug bool) *ir.Prog {
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

func emit(t *testing.T, p *ir.Prog) (*codegen.Linker, *codegen.DataLayout, int) {
	t.Helper()
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		t.Fatalf("发射：%v\n%s", err, ir.Dump(p))
	}
	return l, d, entry
}

func TestDebugSitesAreRecorded(t *testing.T) {
	plain, plainData, _ := emit(t, front(t, false))
	if got := plain.Sites(); len(got) != 0 {
		t.Fatalf("不开 debug 也记了检查点：%+v", got)
	}

	l, d, _ := emit(t, front(t, true))
	got := l.Sites()
	if len(got) != 3 {
		t.Fatalf("该有三条检查点（函数体两条、顶层一条），得到 %d 条：%+v", len(got), got)
	}
	for _, s := range got {
		if s.File != "t.nf" {
			t.Errorf("文件记错：%+v", s)
		}
		if s.ID == 0 || s.Offset < 0 {
			t.Errorf("检查点没带上号或偏移：%+v", s)
		}
	}
	// 行号集合：第 2、3 行在 twice 那个位置里，第 6 行在顶层那个位置里。
	seen := map[int]bool{}
	for _, s := range got {
		seen[s.Line] = true
	}
	for _, line := range []int{2, 3, 6} {
		if !seen[line] {
			t.Errorf("第 %d 行没有检查点：%+v", line, got)
		}
	}
	// 数据段只多那个位置宿主指针，一个位置 8 字节；不开 debug 时一个位置也不多。
	if d.DbgHost != len(d.User)-8 {
		t.Errorf("宿主指针那一格该在数据段最尾，得到 %d，数据段长 %d", d.DbgHost, len(d.User))
	}
	if len(d.User) != len(plainData.User)+8 {
		t.Errorf("开了 debug 数据段多了 %d 字节，只该多那一格", len(d.User)-len(plainData.User))
	}
}

// 同一份程序的检查点偏移要按代码里的先后排：断点与单步都拿这张表在两个方向上查。
func TestDebugSiteOffsetsAscendWithinAFunction(t *testing.T) {
	l, _, _ := emit(t, front(t, true))
	var lastID, lastOff = 0, -1
	for _, s := range l.Sites() {
		if s.Func != "twice" {
			continue
		}
		if s.ID <= lastID || s.Offset <= lastOff {
			t.Errorf("twice 里的检查点没按先后排：%+v 在前一条 %+v/%+v 之后", s, lastID, lastOff)
		}
		lastID, lastOff = s.ID, s.Offset
	}
}

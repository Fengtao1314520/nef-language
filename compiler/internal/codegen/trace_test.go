// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// trace_test.go 钉调用链表在发码这一侧的形状：项数、项的先后、
// 第 0 个位置那个偏移真的落在被调那一条 call 之后，以及三项文本各自是谁。
//
// 表里一项 32 字节：第 0 个位置是返回地址在用户代码里的偏移，后三个是从表起点算起的字节差
// （调用点位置文本、调用者的名、被调者的名）。那几个常数在 codegen 包里不导出，
// 这一份测试按包外读者能看到的形状写死；改了布局这里要跟着改。
package codegen_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

const (
	traceRowSize   = 32 // 一项的字节数
	traceRowCode   = 0  // 返回地址那个位置
	traceRowPos    = 8  // 调用点位置文本那个位置
	traceRowCaller = 16 // 调用者的名那个位置
	traceRowCallee = 24 // 被调者的名那个位置
)

// traceSrc 里有三处调用户函数：wrap 里调 twice 一次，顶层调 wrap 与 twice 各一次。
// console.print 是内建动作，不进这张表。
const traceSrc = "method twice(n: int): int {\n    return n * 2\n}\n\nmethod wrap(n: int): int {\n    return twice(n)\n}\n\nconsole.print(wrap(3) + twice(4))\n"

func traceFront(t *testing.T, src string) *ir.Prog {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: "t.nf"}
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

// traceTextAt 读出表里第 row 项那个位置指的哪一串字：那个位置存的是从表起点算起的字节差，
// 指过去是一个文本对象（字节数 8 ＋ 码点数 8 ＋ UTF-8 字节）。
func traceTextAt(d *codegen.DataLayout, row, cell int) string {
	obj := d.TraceBase + int(int64(binary.LittleEndian.Uint64(d.User[d.TraceBase+row*traceRowSize+cell:])))
	n := int(binary.LittleEndian.Uint64(d.User[obj:]))
	return string(d.User[obj+16 : obj+16+n])
}

func traceCodeAt(d *codegen.DataLayout, row int) int {
	return int(int64(binary.LittleEndian.Uint64(d.User[d.TraceBase+row*traceRowSize+traceRowCode:])))
}

// TestTraceRowsCarryReturnAddresses 是这一步的主干：发码那一步按 BuildData 同一个顺序，
// 把每一项的第 0 个位置填成那条 call 之后的第一个字节。三处检查各钉住一个错法——
// 偏移不递增＝两边数的顺序不一样；那一条指令不是 call＝填的不是返回地址；
// 两次调 twice 的落点不一样＝填的是别处那条 call 的位置。
func TestTraceRowsCarryReturnAddresses(t *testing.T) {
	p := traceFront(t, traceSrc)
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		t.Fatalf("发射：%v\n%s", err, ir.Dump(p))
	}
	userLen := l.Asm().PC() // 运行期那一块接进来之前，用户代码的长度
	ji, err := l.Jit(d.User, entry, 0)
	if err != nil {
		t.Fatalf("装配：%v", err)
	}
	code := ji.Img.Bytes // 回填之后的完整映像：代码仍从第 0 个字节起

	if d.TraceN != 3 {
		t.Fatalf("该有三项（wrap 里一次、顶层两次），得到 %d 项", d.TraceN)
	}
	if d.TraceBase < 0 {
		t.Fatalf("表没排出位置：%d", d.TraceBase)
	}
	var last = -1
	targets := make([]int, d.TraceN)
	for row := 0; row < d.TraceN; row++ {
		off := traceCodeAt(d, row)
		if off <= last {
			t.Fatalf("第 %d 项的返回地址 %#x 不比前一项 %#x 靠后：两边的顺序不一致", row, off, last)
		}
		last = off
		start := off - 5 // 直接调用五个字节：E8 ＋ rel32
		if start < 0 || off > userLen {
			t.Fatalf("第 %d 项的返回地址 %#x 落在用户代码之外（用户代码长 %#x）", row, off, userLen)
		}
		if got := code[start]; got != 0xE8 {
			t.Fatalf("第 %d 项的返回地址前面那一条不是直接调用：偏移 %#x 处是 %#02x", row, start, got)
		}
		rel := int(int32(binary.LittleEndian.Uint32(code[start+1:])))
		targets[row] = start + 5 + rel
	}
	if targets[0] != targets[2] || targets[1] == targets[0] {
		t.Errorf("第 0 与第 2 项都调 twice，落点该一样（%#x／%#x）；第 1 项调 wrap，该不一样（%#x）", targets[0], targets[2], targets[1])
	}

	wantCaller := []string{"wrap", "t.nf", "t.nf"}
	wantCallee := []string{"twice", "wrap", "twice"}
	wantPos := []string{"t.nf 6:", "t.nf 9:", "t.nf 9:"}
	for row := 0; row < d.TraceN; row++ {
		if got := traceTextAt(d, row, traceRowCaller); got != wantCaller[row] {
			t.Errorf("第 %d 项的调用者写成 %q，该是 %q", row, got, wantCaller[row])
		}
		if got := traceTextAt(d, row, traceRowCallee); got != wantCallee[row] {
			t.Errorf("第 %d 项的被调者写成 %q，该是 %q", row, got, wantCallee[row])
		}
		if pos := traceTextAt(d, row, traceRowPos); !strings.HasPrefix(pos, wantPos[row]) {
			t.Errorf("第 %d 项的位置写成 %q，该是 %q 起头（文件 行:列）", row, pos, wantPos[row])
		}
	}
}

// TestTraceTableIsEmptyWithoutUserCalls 钉那一处代价的来由：没有一次用户函数调用的
// 程序，这张表一个字节也不占，前面那些偏移一个不动。
func TestTraceTableIsEmptyWithoutUserCalls(t *testing.T) {
	p := traceFront(t, "console.print(1 + 2)\n")
	d := codegen.BuildData(p)
	if d.TraceN != 0 || d.TraceBase != -1 {
		t.Fatalf("没有一次用户函数调用，表该是空的：项数 %d，位置 %d", d.TraceN, d.TraceBase)
	}
	l := codegen.NewLinker()
	if _, err := codegen.Emit(p, l, d); err != nil {
		t.Fatalf("发射：%v\n%s", err, ir.Dump(p))
	}
}

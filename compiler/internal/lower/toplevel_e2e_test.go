// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// toplevel_e2e_test.go 验证"顶层那一层的析构点"。
//
// 顶层的名字落在**静态槽**上，不是帧槽上：本文件的函数可能在别的文件的顶层里被调用，
// 那时候本文件的顶层早就运行完了，那个槽位还得读得到。所以这一层的尽头不是"本文件运行到最后一行"，
// 而是"整个程序运行完"——登记在 nef.main 里，排在所有 nef.top.* 的调用之后、@exit 之前。
//
// 两条测试各验证一头：中间码那一条验证"排在什么位置、谁不登记"，实际运行那一条验证"寿命足够长"。

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/exetest"
	"neflang.org/nf/internal/ir"
)

// destroyAt 列出这一个函数里每一笔 nef.destroy 释放的是哪个位置，并返回 @exit 排在第几条。
func destroyAt(fn *ir.Func) (slots []string, exit, lastTop int) {
	exit, lastTop = -1, -1
	for i, in := range fn.Body {
		c, ok := in.(*ir.Call)
		if !ok {
			continue
		}
		switch {
		case c.Fn == "nef.destroy":
			slots = append(slots, c.Args[0].String())
		case c.Fn == "@exit":
			exit = i
		case strings.HasPrefix(c.Fn, "nef.top."):
			lastTop = i
		}
	}
	return
}

// TestTopLevelDestroysAtProgramEnd 验证中间码里的登记结果。
//
// 静态槽的编号按声明序排：kept=@0、xs=@1、given=@2，而 args 那个位置排在所有文件顶层名字之后＝@3。
// 该登记的有三笔：xs（自己新造的一块）、given（交给了 take，而 take 没带走——见下）、
// args（规则把它算成入口顶层的一个 `let`）。不许进来的只剩一类：kept 是 `export shared`
// （规则明写它不参与析构）。
//
// given 这一笔是放宽的那一半：交出过不等于不归我还——**交进去的那一个参数在它
// 自己的函数里没把值留住**（没 return、没写进字段、没装进容器、也没再交出去），出了被调方就
// 再没人持有这一块，持有者还是这个位置（`Facts.Consumed`）。反过来，被调方带走了就不登记，宁可保留泄漏
// （`heap_e2e_test.go` 里"交给带走的函数"那一条验证的就是这一侧）。
//
// 释放顺序是登记顺序的反序：given、xs、args（args 最早登记，所以它最后释放）。
//
// 那一段代码单独立成 `nef.release_top`（不是发在 nef.main 的正文里）：`exit` 可以在任何深度上
// 被调用，帧链回溯不回去，顶层这一段只能做成任何位置都能调用的一站。
func TestTopLevelDestroysAtProgramEnd(t *testing.T) {
	p := progOf(t, `
class Bag {
    n:int
}
method take(b:Bag) {
}
export shared kept:list<string> = ["甲"]
let xs:list<int> = [1, 2]
let given:Bag = new Bag(n: 3)
take(given)
for (i:int in 0 until 2) {
    let perLoop:list<int> = [i]
}
console.print(length(xs), length(kept))
`)
	main := findFn(p, "nef.main")
	if main == nil {
		t.Fatal("nef.main 没排出来")
	}
	release := findFn(p, "nef.release_top")
	if release == nil {
		t.Fatal("nef.release_top 没排出来")
	}
	slots, _, _ := destroyAt(release)
	if strings.Join(slots, " ") != "@2 @1 @3" {
		t.Fatalf("程序尽头那份记录不对：%v（要 @2 @1 @3：只有 shared 的 @0 不许进来）", slots)
	}
	if _, _, _ = destroyAt(main); len(main.Body) == 0 {
		t.Fatal("nef.main 是空的")
	}
	// nef.main 里不该再有散着的析构：那一段都收进 nef.release_top 了
	_, exit, lastTop := destroyAt(main)
	for _, in := range main.Body {
		if c, ok := in.(*ir.Call); ok && c.Fn == "nef.destroy" {
			t.Fatalf("nef.main 里散着一笔析构 %v——尽头那一段应该在 nef.release_top 里", c.Args[0])
		}
	}
	call := -1
	for i, in := range main.Body {
		if c, ok := in.(*ir.Call); ok && c.Fn == "nef.release_top" {
			call = i
		}
	}
	if call < 0 {
		t.Fatal("nef.main 没叫 nef.release_top")
	}
	if exit < 0 {
		t.Fatal("nef.main 里没有 @exit")
	}
	if !(lastTop < call && call < exit) {
		t.Fatalf("nef.release_top 要排在各文件顶层都调完、@exit 之前：最后一调 %d、叫它 %d、@exit %d", lastTop, call, exit)
	}

	// 顶层的循环体里那个槽位住的是帧槽：它归本层一圈一释放，绝不能攒到程序尽头。
	// 这一条是验证过的约束——按"帧是不是顶层"分流的话，每一圈的登记都进了尽头那份记录，
	// 同一个槽位被释放无数次，实际运行当场段错误。
	tfn := findFn(p, "nef.top.t.nf")
	if tfn == nil {
		t.Fatal("nef.top.t.nf 没排出来")
	}
	loopSlots, _, _ := destroyAt(tfn)
	uniq := map[string]bool{}
	for _, s := range loopSlots {
		if !strings.HasPrefix(s, "%") {
			t.Fatalf("顶层循环体里那一号应作为帧槽在本帧还掉，实际还的是 %s", s)
		}
		uniq[s] = true
	}
	// 两笔是设计好的：一圈一释放那一笔，加帧尾帧内清理记录那一笔（跨帧抛出时这一帧可能正持有一块）。
	// 第二笔读的是被抹成 0 的那个位置，走到 nef.destroy 头上对空句柄不做处理，所以重复释放没有代价也不出错。
	if len(uniq) != 1 {
		t.Fatalf("循环体里只有一个号，本帧还的位置该只有一格，实际 %v", loopSlots)
	}
}

// TestTopLevelStaticSlotOutlivesItsFile 是实际运行的对照用例：stock.nf 的顶层造了一个 list，
// 它自己的函数由入口文件的顶层调用——那一刻 stock 的顶层早已运行完。析构点若落在
// "本文件运行完最后一行"那一侧，这里读到的就是已析构的块。两份源文件在 testdata/toplevel 里。
func TestTopLevelStaticSlotOutlivesItsFile(t *testing.T) {
	r := exetest.Run(t, buildFiles(t,
		srcFile{"main.nf", "../../testdata/toplevel/main.nf"},
		srcFile{"stock.nf", "../../testdata/toplevel/stock.nf"},
	), "toplevel.exe")
	want := "3 甲乙丙 3\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("顶层那一号没活到程序尽头：\n got %q 码 %d\nwant %q", r.Stdout, r.Code, want)
	}
}

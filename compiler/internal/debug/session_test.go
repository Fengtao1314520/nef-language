// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// session_test.go 钉"停下来还是继续走"这个位置判断：断点、单步三种走法、一行钉不住要说钉不住。
package debug_test

import (
	"testing"

	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/debug"
)

// 一份假检查点表：t.nf 第 2 行两条（函数体）、第 3 行一条、第 6 行一条；第 9 行没有语句。
var sites = []codegen.Site{
	{ID: 1, Func: "twice", Offset: 0x10, File: "t.nf", Line: 2, Col: 5},
	{ID: 2, Func: "twice", Offset: 0x40, File: "t.nf", Line: 2, Col: 20},
	{ID: 3, Func: "twice", Offset: 0x70, File: "t.nf", Line: 3, Col: 5},
	{ID: 4, Func: "nef.main", Offset: 0x90, File: "t.nf", Line: 6, Col: 9},
}

func TestBreakpointStopsOnlyOnThatLine(t *testing.T) {
	s := debug.New(sites)
	if got := s.SetBreakpoints("t.nf", []int{3}); len(got) != 1 || got[0] != 3 {
		t.Fatalf("第 3 行该钉得住，交回 %v", got)
	}
	if s.OnHit(1, 0x1000, 1) != debug.Run {
		t.Error("第 2 行没打断点，不该停")
	}
	if s.OnHit(3, 0x1000, 1) != debug.Stop {
		t.Error("第 3 行的断点没停")
	}
	if !s.Stopped() {
		t.Error("停住了就该说停住")
	}
	at, ok := s.Site()
	if !ok || at.Line != 3 || at.Func != "twice" {
		t.Errorf("停住的位置不对：%+v", at)
	}
}

// 一行上没有语句（空行、只有右花括号）就别骗编辑器说钉上了。
func TestBreakpointOnALineWithNoStatement(t *testing.T) {
	s := debug.New(sites)
	if got := s.SetBreakpoints("t.nf", []int{9}); len(got) != 0 {
		t.Errorf("第 9 行没有语句，该交回空，得到 %v", got)
	}
	for _, id := range []int{1, 2, 3, 4} {
		if s.OnHit(id, 0x1000, 1) != debug.Run {
			t.Errorf("一行钉不住却把 %d 号变成了断点", id)
		}
	}
}

// 协议里那一条 setBreakpoints 带的是"这一个文件的全部断点"，换一份文件不能把这一份的擦掉；
// 反过来，同一份文件再设一次要把上一次的清干净。
func TestBreakpointsArePerFile(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{3})
	s.SetBreakpoints("other.nf", []int{7})
	if s.OnHit(3, 0x1000, 1) != debug.Stop {
		t.Error("给别的文件设断点，把 t.nf 那一条擦掉了")
	}
	s.Continue()
	s.SetBreakpoints("t.nf", []int{6})
	if s.OnHit(3, 0x1000, 1) != debug.Run {
		t.Error("同一份文件重设，上一条断点没清掉")
	}
	if s.OnHit(4, 0x1000, 1) != debug.Stop {
		t.Error("新设的那一条没生效")
	}
}

// 一行里两条语句：钉这一行就该两条都算断点。
func TestOneLineCanCarrySeveralSites(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{2})
	if s.OnHit(1, 0x1000, 1) != debug.Stop {
		t.Error("第 2 行第一条没停")
	}
	s.Continue()
	if s.OnHit(2, 0x1000, 1) != debug.Stop {
		t.Error("第 2 行第二条没停")
	}
}

func TestStepOverSkipsDeeperFrames(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{3})
	if s.OnHit(3, 0x1000, 1) != debug.Stop {
		t.Fatal("先停在第 3 行那一格，才有 over 这一步可发")
	}
	s.Step("over")
	if s.OnHit(2, 0x1000, 2) != debug.Run {
		t.Error("走进被调函数的帧里不该停（over）")
	}
	if s.OnHit(4, 0x1000, 1) != debug.Stop {
		t.Error("回到同一层、号变了，该停（over）")
	}
}

func TestStepIntoStopsAnywhereNew(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{3})
	s.OnHit(3, 0x1000, 1)
	s.Step("in")
	if s.OnHit(2, 0x1000, 2) != debug.Stop {
		t.Error("into 该在下一格停下来，不管在第几层")
	}
}

func TestStepOutWaitsForAShallowerFrame(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{2})
	if s.OnHit(2, 0x1000, 2) != debug.Stop {
		t.Fatal("先在深一层的断点上停住，才有 out 这一步可发")
	}
	s.Step("out")
	if s.OnHit(3, 0x1000, 2) != debug.Run {
		t.Error("还在同一层里不该停（out）")
	}
	if s.OnHit(4, 0x1000, 1) != debug.Stop {
		t.Error("回到浅一层该停（out）")
	}
}

// 单步路上撞到断点也算停：over 本来不会在深一层停，断点说要停就得停。
func TestBreakpointWinsDuringAStep(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{2})
	s.OnHit(1, 0x1000, 1)
	s.SetBreakpoints("t.nf", []int{3})
	s.Step("over")
	if s.OnHit(3, 0x1000, 2) != debug.Stop {
		t.Fatal("断点该赢：over 在深一层不会停，这一条要停")
	}
	if at, _ := s.Site(); at.Line != 3 {
		t.Errorf("停的位置不对：%+v", at)
	}
}

// 停在被调函数的 return 上再按下一句：函数返回，层数浅了一个位置，这一句要停在 caller 里。
// 只认"同一层"的话这一条永远等不到，程序会一路跑完（端到端那一条测试就是这么红的）。
func TestStepOverFromAReturnStopsInCaller(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakpoints("t.nf", []int{3})
	if s.OnHit(3, 0x1000, 2) != debug.Stop {
		t.Fatal("先停在被调函数第 3 行的断点上")
	}
	s.Step("over")
	if s.OnHit(4, 0x1000, 1) != debug.Stop {
		t.Error("返回到上一层之后该停（over）")
	}
}

// 暂停：编辑器按下暂停键之后，下一条检查点就要停，理由报 pause。
// 这一条能成立是因为跑的时候每句也回宿主问一次，只是平时答"走"。
func TestPauseStopsAtTheNextStatement(t *testing.T) {
	s := debug.New(sites)
	s.Continue()
	s.Pause()
	if s.OnHit(1, 0x1000, 1) != debug.Stop {
		t.Fatal("按了暂停，下一条检查点该停")
	}
	if s.Reason() != "pause" {
		t.Errorf("停下来的理由该是 pause，得到 %q", s.Reason())
	}
	// 停过一次之后牌子要放下：再按继续就该一路跑，不该每句都停。
	s.Continue()
	if s.OnHit(2, 0x1000, 1) != debug.Run {
		t.Error("暂停的牌子没放下")
	}
}

func TestSitesByLine(t *testing.T) {
	s := debug.New(sites)
	got := s.SitesByLine("t.nf", 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("第 2 行该有 1、2 两条，得到 %v", got)
	}
	if len(s.SitesByLine("t.nf", 99)) != 0 {
		t.Error("没有的那一行不该查出东西")
	}
}

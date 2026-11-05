// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// throw_test.go 钉 breakOnThrow 那一条判断：开关关着时抛出不停，开着才停；
// 停住的理由报 exception，位置是抛出那一句自己；之后在断点上停一次，位置要换掉，
// 不能留着上一个停住原因的那一个位置。
//
// en: throw_test.go pins the breakOnThrow judgement: with the switch off a throw does not
// stop, with it on it does; the stop reports its reason as exception and its position is the
// throwing statement itself; a later breakpoint stop replaces that position rather than
// keeping the one belonging to the previous reason.

package debug_test

import (
	"testing"

	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/debug"
)

func TestThrowStopNeedsTheSwitch(t *testing.T) {
	s := debug.New(sites)
	top := codegen.Site{ID: 2, Func: "twice", Offset: 0x40, File: "t.nf", Line: 2, Col: 20}
	if s.OnThrow(top, 0x1000, 1) != debug.Run {
		t.Fatal("开关没开，抛出那一步不该停")
	}
	if s.Stopped() {
		t.Error("没停就不该说停住")
	}

	s.SetBreakOnThrow(true)
	if !s.BreakOnThrow() {
		t.Fatal("开关开着要认得出来")
	}
	if s.OnThrow(top, 0x1000, 2) != debug.Stop {
		t.Fatal("开关开着，没人接住的抛出该停")
	}
	if s.Reason() != "exception" {
		t.Errorf("停住的理由该是 exception，得到 %q", s.Reason())
	}
	if at, ok := s.Site(); !ok || at.Line != 2 || at.Func != "twice" {
		t.Errorf("停住的位置该是抛出那一句自己：%+v（查到 %v）", at, ok)
	}

	// 换一种停法：同一次会话里后来在断点上停住，位置要跟过去——那一个位置属于那一条断点。
	if got := s.SetBreakpoints("t.nf", []int{3}); len(got) != 1 {
		t.Fatalf("第 3 行该钉得住，交回 %v", got)
	}
	s.OnHit(3, 0x2000, 1)
	if at, _ := s.Site(); at.Line != 3 || at.Func != "twice" {
		t.Errorf("断点上停住后位置要换成第 3 行，得到 %+v", at)
	}
	if s.Reason() != "breakpoint" {
		t.Errorf("理由也该换成 breakpoint，得到 %q", s.Reason())
	}
}

// 抛出停过一次之后再按继续：后面每条检查点照常问，不该把"抛出"这件事留着。
// 这里钉的是 reason 与位置都被下一次 halt 换掉，以及停过抛出之后单步仍能照常走。
func TestThrowStopDoesNotStickAfterwards(t *testing.T) {
	s := debug.New(sites)
	s.SetBreakOnThrow(true)
	top := codegen.Site{ID: 2, Func: "twice", Offset: 0x40, File: "t.nf", Line: 2, Col: 20}
	if s.OnThrow(top, 0x1000, 2) != debug.Stop {
		t.Fatal("第一次抛出该停")
	}
	s.Continue()
	s.Step("over")
	// 单步之后停在下一句（号要变、层要同一层或更浅）：那一条路上没有断点，理由该是 step。
	if s.OnHit(4, 0x1000, 1) != debug.Stop {
		t.Fatal("按了下一句该停在下一条检查点上")
	}
	if at, _ := s.Site(); at.ID != 4 || at.Line != 6 {
		t.Errorf("位置该换成第 6 行那一条检查点，得到 %+v", at)
	}
	if s.Reason() != "step" {
		t.Errorf("理由该是 step，得到 %q", s.Reason())
	}
}

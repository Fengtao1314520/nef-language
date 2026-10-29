// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package debug 是调试器那一侧的账：哪几条检查点打了断点、单步要走到哪儿为止、
// 停下来的时候手里有哪一帧。它只认检查点号与地址，不碰协议——协议在 cmd/nf 那一路。
//
// en: Package debug keeps the debugger's own books: which checkpoints carry breakpoints, how
// far a step should go, and which frame is in hand when the program stops. It only knows
// checkpoint numbers and addresses and never touches the protocol - that lives on the cmd/nf
// side.
package debug

import "neflang.org/nf/internal/codegen"

// Action 是"这一条检查点命中之后干什么"。
//
// en: Action is "what to do once this checkpoint is hit".
type Action int

const (
	Run  Action = iota // 别停，往下跑
	Stop               // 停下来问编辑器
)

type stepMode int

const (
	stepNone stepMode = iota
	stepOver
	stepInto
	stepOut
)

// Session 是一份被调试程序的账。Sites 是发射那一步返回的检查点表，全程序唯一编号。
//
// en: Session is the books of one debugged program. Sites is the checkpoint table returned by
// the emit step, numbered uniquely across the whole program.
type Session struct {
	sites  []codegen.Site
	breaks map[int]bool
	step   stepMode
	// stepDepth 与 stepFrom 是发单步命令那一刻的帧深与所在检查点：
	// over 要等"同一层、号变了"，into 要等"号变了"，out 要等"层浅了"。
	stepDepth int
	stepFrom  int
	// Last 是上一次停住的那一条，编辑器与断点判定都要看它。
	Last  int
	RBP   uintptr
	Depth int
	ID    int // 当前停住的那一条检查点号
	// reason 说这一次为什么停：编辑器要在断点上停与在单步上停分两种图标。
	reason string
	// pause 是"编辑器按了暂停"那块牌子。跑的时候每句也回宿主问一次，所以立起牌子之后
	// 下一条检查点就会停；停过一次就放下，不然按继续会变成每句都停。
	pause bool
}

func New(sites []codegen.Site) *Session {
	return &Session{sites: sites, breaks: map[int]bool{}, stepFrom: -1}
}

// SetBreakpoints 把"这个文件的这几行"翻成检查点号：一行可能对上好几条（一行里写两条语句、
// 或一条语句降级出多条检查点），全打上。返回真固定了的行，编辑器据此画实心点。
// 一行钉不住（那一行没有语句，比如空行与只有右花括号）也要说清楚，别让它看着像打上了。
//
// en: SetBreakpoints translates "these lines of this file" into checkpoint numbers: one line
// may match several (two statements on one line, or one statement lowered into several
// checkpoints), and all of them get pinned. It returns the lines that were genuinely pinned so
// the editor can draw a solid dot. A line that cannot be pinned (no statement there - a blank
// line or a lone closing brace) is reported as unpinned too, so it never looks pinned.
func (s *Session) SetBreakpoints(file string, lines []int) []int {
	s.dropFile(file)
	pinned := map[int]bool{}
	for _, line := range lines {
		for _, at := range s.sites {
			if at.File == file && at.Line == line {
				s.breaks[at.ID] = true
				pinned[line] = true
			}
		}
	}
	var out []int
	for _, line := range lines {
		if pinned[line] {
			out = append(out, line)
		}
	}
	return out
}

func (s *Session) dropFile(file string) {
	for _, at := range s.sites {
		if at.File == file {
			delete(s.breaks, at.ID)
		}
	}
}

// OnHit 是每一条检查点都要问的一句：停下还是走。depth 由宿主算好交进来（要走内存）。
//
// en: OnHit is the question every checkpoint asks: stop or go. depth is computed by the host
// and passed in (it has to read memory).
func (s *Session) OnHit(id int, rbp uintptr, depth int) Action {
	if s.breaks[id] {
		s.halt(id, rbp, depth, "breakpoint", stepNone)
		return Stop
	}
	if s.pause {
		s.halt(id, rbp, depth, "pause", stepNone)
		return Stop
	}
	switch s.step {
	case stepOver:
		// 同一层**或更浅**：停在 `return` 上再按一次下一句，函数就返回到上一层去了，
		// 只认同一层的话这一句永远等不到（实测：程序一路跑完，调试器一次都没停）。
		if id != s.stepFrom && depth <= s.stepDepth {
			s.halt(id, rbp, depth, "step", stepNone)
			return Stop
		}
	case stepInto:
		if id != s.stepFrom {
			s.halt(id, rbp, depth, "step", stepNone)
			return Stop
		}
	case stepOut:
		if depth < s.stepDepth {
			s.halt(id, rbp, depth, "step", stepNone)
			return Stop
		}
	}
	return Run
}

func (s *Session) halt(id int, rbp uintptr, depth int, reason string, after stepMode) {
	s.Last, s.RBP, s.Depth, s.ID, s.reason = id, rbp, depth, id, reason
	s.step = after
	s.pause = false // 停过一次就把牌子放下，不然按继续会变成每句都停
}

// Pause 立一块牌子：下一条检查点就停，理由报 pause。跑的时候每句也回宿主问一次，
// 所以这块牌子下一句就看得见。
//
// en: Pause raises a flag: stop at the next checkpoint and report the reason as pause. Even
// while running, every statement asks the host, so the flag is seen one statement later.
func (s *Session) Pause() { s.pause = true }

// Reason 说这一次停是因为什么：breakpoint 或 step。
//
// en: Reason says what caused this stop: breakpoint or step.
func (s *Session) Reason() string {
	if s.reason == "" {
		return "step"
	}
	return s.reason
}

// Continue 是"跑起来，只在断点上停"。不动 pause：那块牌子只由"真停过一次"放下。
// 停着的时候按一下暂停再按继续，要的就是"下一句停给我看"；在这儿清掉它等于把那次暂停吞了。
//
// en: Continue means "run, stopping only at breakpoints". It deliberately leaves pause alone:
// that flag is lowered only by an actual stop. Pressing pause and then continue while stopped
// means exactly "show me the next statement", and clearing the flag here would swallow that
// pause.
func (s *Session) Continue() { s.step = stepNone }

// Step 发一道单步。mode 取 "over"、"in"、"out"，认不出的按 over 办。
//
// en: Step issues one step. mode is "over", "in" or "out"; anything unrecognised is handled as
// over.
func (s *Session) Step(mode string) {
	s.stepFrom = s.ID
	s.stepDepth = s.Depth
	switch mode {
	case "in":
		s.step = stepInto
	case "out":
		s.step = stepOut
	default:
		s.step = stepOver
	}
}

// Stopped 说此刻是不是停在用户面前。
//
// en: Stopped says whether the program is currently stopped in front of the user.
func (s *Session) Stopped() bool { return s.ID != 0 }

// Site 交出当前停住那一条的源码位置。
//
// en: Site hands out the source position of the checkpoint that is stopped.
func (s *Session) Site() (codegen.Site, bool) {
	for _, at := range s.sites {
		if at.ID == s.ID {
			return at, true
		}
	}
	return codegen.Site{}, false
}

// SitesByLine 返回落在某一行上的全部检查点号，测试与"这一行钉不钉得住"都用它。
//
// en: SitesByLine returns every checkpoint number that falls on a given line; both the tests
// and the "can this line be pinned" question use it.
func (s *Session) SitesByLine(file string, line int) []int {
	var out []int
	for _, at := range s.sites {
		if at.File == file && at.Line == line {
			out = append(out, at.ID)
		}
	}
	return out
}

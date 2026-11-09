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

// dap_windows.go 是被调试的那一个进程：编译时开 Debug，机器码摆进本进程的内存，
// 起线程之前把宿主回调的地址写进 DbgHost 那个位置。
//
// 程序每走到一条语句的开头，那四条指令就 call 回 Go 这一侧（发射那一步摊出来的：
// 号进 rcx、这一帧的 rbp 进 rdx）。这一侧判断要不要停；要停就把"停在第几行"报给父进程，
// 等它命令。父进程那头连着编辑器，见 dap.go 与 internal/dap。
//
// en: dap_windows.go is the debugged process: it compiles with Debug on, puts the machine code
// into this process's own memory, and writes the host callback's address into the DbgGuest slot
// before starting the thread. Every time the program reaches the start of a statement, those
// four instructions call back into the Go side (what the emit step produces: the number in rcx,
// this frame's rbp in rdx). The Go side decides whether to stop; if it does, it reports
// "stopped at line n" to the parent process and waits for its command. The parent is what faces
// the editor - see dap.go and internal/dap.
package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/debug"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/pe"
	"neflang.org/nf/internal/pkgload"
)

var procReadProcessMemo = kernel32.NewProc("ReadProcessMemory")

// procGetThreadID 用在宿主回调那一头：回调是由走到检查点的那一条线程直接调进来的，
// 所以在这里问一句"我是谁"就能认出不是程序那一条线程的来客。
//
// en: procGetThreadID is used by the host callback. The callback is called directly by whichever
// thread reaches the checkpoint, so asking "who am I" here is enough to recognise a caller that is
// not the program's own thread.
var procGetThreadID = kernel32.NewProc("GetCurrentThreadId")

// mainThreadID 是程序那一条线程的号码，起线程之后、放行之前写下。
//
// en: mainThreadID is the program thread's number, written after creating it and before resuming it.
var mainThreadID atomic.Uintptr

// procReader 读本进程里那块 VirtualAlloc 来的内存。走 ReadProcessMemory 是与写进去用的
// WriteProcessMemory 成对：Go 既不知道那块内存存在，也不该替我们看着它（run_windows.go 头上那句）。
//
// en: procReader reads the VirtualAlloc'd block inside this process. It goes through
// ReadProcessMemory to pair with the WriteProcessMemory used for writing: Go neither knows that
// block exists nor should be watching it for us (see the note at the top of run_windows.go).
type procReader struct{}

func (procReader) Read(addr uintptr, n int) ([]byte, bool) {
	if n <= 0 || n > 1<<20 {
		return nil, false
	}
	b := make([]byte, n)
	var got uintptr
	r, _, _ := syscall.SyscallN(procReadProcessMemo.Addr(), currentProcess, addr,
		uintptr(unsafe.Pointer(&b[0])), uintptr(n), uintptr(unsafe.Pointer(&got)))
	if r == 0 || int(got) != n {
		return nil, false
	}
	return b, true
}

// cmdDebuggee 跑一趟被调试的程序。args 形如：文件.nf --debuggee 端口号 [程序自己的参数…]。
//
// en: cmdDebuggee runs one debugged program. args looks like: file.nf --debuggee port [the
// program's own arguments ...].
func cmdDebuggee(args []string) int {
	port := ""
	throwStop := false
	var progArgs []string
	for i := 1; i < len(args); i++ {
		if args[i] == "--debuggee" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--debuggee 后面要跟一个端口号")
				return 2
			}
			port = args[i+1]
			i++
			continue
		}
		if args[i] == "--break-on-throw" {
			// 编辑器那一头 launch.json 的 breakOnThrow：开了才在没人接住的那一次抛出停。
			// 这一号是自己的，不能混进程序的参数里（那几段是给 nf 程序读的 args）。
			throwStop = true
			continue
		}
		progArgs = append(progArgs, args[i])
	}
	if port == "" {
		fmt.Fprintln(os.Stderr, "被调试的这一头要知道往哪儿连：--debuggee 端口号")
		return 2
	}
	// 路径取绝对的、并把盘符折成小写：编辑器报过来的断点是绝对路径（file:///c%3A/… 那一种，
	// 盘符本来就是小写），两边得是同一个写法才对得上。
	abs, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	entry := normPath(abs)

	prog := &check.Program{Entry: entry, Debug: true}
	seen := map[string]bool{}
	pk := pkgload.New()
	if !load(prog, entry, seen, pk, "") {
		return 1
	}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e.Error())
		}
		fmt.Fprintf(os.Stderr, "共 %d 处不对\n", len(errs))
		return 1
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	ent, err := codegen.Emit(p, l, d)
	if err != nil {
		fmt.Fprintln(os.Stderr, "后端发射阶段：", err)
		return 1
	}
	if d.DbgHost < 0 {
		fmt.Fprintln(os.Stderr, "这一份没开调试信息，停不下来")
		return 1
	}
	ji, err := l.Jit(d.User, ent, hostSpare)
	if err != nil {
		fmt.Fprintln(os.Stderr, "摆不出内存里的映像：", err)
		return 1
	}
	total := len(ji.Img.Bytes) + hostSpare
	base, _, errno := syscall.SyscallN(procVirtualAlloc.Addr(), 0, uintptr(total), memCommitReserve, pageExecuteReadWrite)
	if base == 0 {
		fmt.Fprintln(os.Stderr, "向操作系统要内存失败：", errno)
		return 1
	}
	if !poke(base, 0, ji.Img.Bytes) {
		fmt.Fprintln(os.Stderr, "把代码摆进内存失败")
		return 1
	}
	if code := fillImports(base, ji.Img.Iat); code != 0 {
		return code
	}
	cmd := commandLine(entry, progArgs)
	at := len(ji.Img.Bytes)
	if len(cmd) > hostSpare {
		fmt.Fprintln(os.Stderr, "参数太多，这一版放不下")
		return 1
	}
	if !poke(base, at, cmd) || !poke64(base, ji.CmdW, uint64(base)+uint64(at)) {
		fmt.Fprintln(os.Stderr, "把参数摆进内存失败")
		return 1
	}

	conn, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连不上调试的那一头：", err)
		return 1
	}

	sess := debug.New(l.Sites())
	sess.SetBreakOnThrow(throwStop)
	tgt := debug.NewTarget(procReader{}, base, l.Sites(), p)
	// 每一个静态槽的绝对地址：数据段从 Img.DataAt 起，槽在数据段里的偏移由 BuildData 给出。
	tgt.Statics = make([]uintptr, p.Globals)
	for slot, off := range d.Global {
		if int(slot) < len(tgt.Statics) {
			tgt.Statics[int(slot)] = base + uintptr(ji.Img.DataAt) + uintptr(off)
		}
	}
	var mu sync.Mutex  // 停住时父进程会来问栈与变量，与回调两头共用那一份账
	var wmu sync.Mutex // 一条线上两处往外写：回调报"停住了"，命令那头回话
	send := func(m debug.Message) {
		wmu.Lock()
		defer wmu.Unlock()
		debug.WriteMessage(conn, m)
	}
	resume := make(chan struct{}, 1)
	startGate := make(chan struct{})

	// 宿主回调：Windows 那套调用约定下头三个整数参数在 rcx、rdx 与 r8 里，
	// 也就是 NewCallback 返回的那个指针被 call 时，这一侧收到（检查点号, 这一帧的 rbp）。
	// 抛出那一次的问话带的是另一个号（debug.ThrowID），那一条路上第二个与第三个参数交的是
	// "抛出所在那一句之后的第一个字节"和"那一帧的帧基"：前者查现成的检查点表就是位置与函数名，
	// 后者是栈与局部量要读的那一帧。
	//
	// 头一句先比线程号：`nf dap` 只跟程序那一条线程。检查点在每一条
	// 线程的代码里都在，那一判写在产物里要给每一条线程多两次内存访问，而"要不要停"本来就是宿主
	// 定的事，所以写在回调这一头。不是程序那一条线程就直接放行：既不锁那块账，也不往线上写话，
	// 那一条线程照旧往下跑。
	//
	// en: The host callback: under the Windows calling convention the first three integer arguments
	// arrive in rcx, rdx and r8, so when the pointer returned by NewCallback is called this side
	// receives (checkpoint id, this frame's rbp). The throw query carries a different id
	// (debug.ThrowID); on that path the second and third arguments are "the first byte after the
	// statement that threw" and "that frame's frame base": the first looks up the existing
	// checkpoint table for position and function name, the second is the frame the stack and locals
	// are read from.
	//
	// The first thing it does is compare thread numbers: nf dap follows the program's own thread
	// only. The checkpoint instruction sits in every thread's code,
	// and answering "is this me" inside the product would cost every thread two more memory reads,
	// while whether to stop was the host's decision anyway - so the check lives on the callback
	// side. A caller that is not the program's thread is released immediately: it neither takes the
	// lock nor writes on the wire, and that thread keeps running.
	cb := syscall.NewCallback(func(id, arg2, arg3 uintptr) uintptr {
		if tid, _, _ := procGetThreadID.Call(); tid != mainThreadID.Load() {
			return 0
		}
		mu.Lock()
		var act debug.Action
		if int(id) == debug.ThrowID {
			top, _ := tgt.SiteAtAddr(arg2)
			act = sess.OnThrow(top, arg3, tgt.Depth(arg3))
		} else {
			act = sess.OnHit(int(id), arg2, tgt.Depth(arg2))
		}
		reason := sess.Reason()
		site, _ := sess.Site()
		mu.Unlock()
		if act == debug.Stop {
			send(debug.Message{Event: "stopped", Reason: reason, File: site.File, Line: site.Line, Col: site.Col})
			<-resume
		}
		return 0
	})
	// 那个位置在用户数据段里，而 poke 用的偏移是整份映像里的：数据段从 Img.DataAt 起。
	// 少加这一步就把回调地址写进了代码段，程序一跑就摔（与 ji.CmdW 那个位置同一个算法）。
	if !poke64(base, ji.Img.DataAt+d.DbgHost, uint64(cb)) {
		fmt.Fprintln(os.Stderr, "把回调地址写进去失败")
		return 1
	}

	// 父进程的命令一条条办：断点、看栈、看变量随时可答；start 之前不起线程，
	// continue 与 step 只把停在半路的那条线程放行。
	go func() {
		r := bufio.NewReader(conn)
		for {
			m, err := debug.ReadMessage(r)
			if err != nil {
				os.Exit(0) // 那一头断了：一起收，别留一条没人理的线程在跑
			}
			switch m.Cmd {
			case "start":
				close(startGate)
			case "breakpoints":
				mu.Lock()
				verified := sess.SetBreakpoints(normPath(m.File), m.Lines)
				mu.Unlock()
				send(debug.Message{Cmd: "breakpoints", Reply: true, Verified: verified})
			case "stack":
				mu.Lock()
				frames := stackOf(tgt, sess)
				mu.Unlock()
				send(debug.Message{Cmd: "stack", Reply: true, Frames: frames})
			case "locals":
				mu.Lock()
				vars := localsOf(tgt, sess, m.Frame)
				mu.Unlock()
				send(debug.Message{Cmd: "locals", Reply: true, Vars: vars})
			case "globals":
				// 顶层那些名字不在任何一帧上，所以不看 m.Frame；程序摆好了就读得到，
				// 还没跑到的那几句里是 0 或上一手留下的字节，解出来照实说。
				mu.Lock()
				vars := varList(tgt.Globals())
				mu.Unlock()
				send(debug.Message{Cmd: "globals", Reply: true, Vars: vars})
			case "pause":
				// 跑的时候每句都回这儿问一次，所以这块牌子下一句就看得见。
				mu.Lock()
				sess.Pause()
				mu.Unlock()
			case "continue":
				mu.Lock()
				sess.Continue()
				mu.Unlock()
				release(resume)
			case "step":
				mu.Lock()
				sess.Step(m.Mode)
				mu.Unlock()
				release(resume)
			case "evaluate":
				mu.Lock()
				value, found := evaluateOf(tgt, sess, m.Frame, m.Name)
				mu.Unlock()
				send(debug.Message{Cmd: "evaluate", Reply: true, Value: value, Found: found})
			case "memory":
				mu.Lock()
				data, ok := tgt.Memory(uintptr(m.Address), m.Count)
				mu.Unlock()
				encoded := ""
				if ok {
					encoded = base64.StdEncoding.EncodeToString(data)
				}
				send(debug.Message{Cmd: "memory", Reply: true, Data: encoded})
			case "terminate":
				os.Exit(0)
			}
		}
	}()

	<-startGate
	// 先停在入口处，不是嫌它跑得太早：回调地址已经写进产物，线程一落地就会走到第一个检查点，
	// 那时候 `mainThreadID` 要是还没写上，那一句"我是哪一条线程"会把程序自己当成别人。
	// 号码写上、再放行，那一判才有得比。
	//
	// en: The thread starts suspended, not because it runs too early but because the callback
	// address is already written into the product: the thread reaches its first checkpoint the
	// moment it lands, and if mainThreadID were still unset that "which thread am I" check would
	// treat the program as somebody else. Write the number, then release it, and the check has
	// something to compare against.
	var tid uint32
	th, _, errno := syscall.SyscallN(procCreateThread.Addr(), 0, uintptr(pe.StackReserve),
		base+uintptr(ent), 0, createSuspended, uintptr(unsafe.Pointer(&tid)))
	if th == 0 {
		fmt.Fprintln(os.Stderr, "起不来线程：", errno)
		return 1
	}
	mainThreadID.Store(uintptr(tid))
	syscall.SyscallN(procResumeThread.Addr(), uintptr(th))
	// 程序正常收尾会自己 ExitProcess，整个进程跟着没；走到下面这两行是它擅自返回了。
	syscall.SyscallN(procWaitForSingle.Addr(), uintptr(th), waitInfinite)
	send(debug.Message{Event: "exit", Code: 1})
	fmt.Fprintln(os.Stderr, "程序没走 exit 就回来了，退出码按 1 算")
	return 1
}

// release 放行停在半路的那条线程。缓冲一个位置，命令来得比回调快也不会卡住读命令的这一侧。
//
// en: release lets a thread that is stopped halfway continue. One slot of buffering keeps the
// command-reading side from stalling even when commands arrive faster than callbacks.
func release(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// normPath 把编辑器报过来的路径折成与检查器同一份写法：斜杠、盘符小写。
// Windows 上大小写不区分，编辑器给 "F:\a\nf.nf"、编译器记的是 "f:/a/nf.nf"，不折一次就永远对不上。
//
// en: normPath folds a path reported by the editor into the same spelling the checker uses:
// forward slashes, lowercase drive letter. Windows does not distinguish case, so the editor may
// send "F:\a\nf.nf" while the compiler recorded "f:/a/nf.nf" - without folding once they never
// match.
func normPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) >= 2 && p[1] == ':' {
		p = strings.ToLower(p[:1]) + p[1:]
	}
	return p
}

// stackOf 从停住那一帧往上走一遍，返回编辑器要的那一份调用栈。
// 栈顶用停住那一条检查点自己的位置：返回地址说的是"回到上一层之后接着往哪儿走"，
// 拿它查会把上一层的行号报成当前这个位置。
//
// en: stackOf walks up from the frame that stopped and returns the call stack the editor asked
// for. The top frame uses the position of the checkpoint that stopped: a return address says
// where execution continues in the CALLER, so looking it up would report the previous line as
// the current position.
func stackOf(tgt *debug.Target, sess *debug.Session) []debug.FrameInfo {
	if !sess.Stopped() {
		return nil
	}
	top, _ := sess.Site()
	var out []debug.FrameInfo
	for _, f := range tgt.Stack(sess.RBP, top) {
		out = append(out, debug.FrameInfo{ID: f.Number, Name: f.Name, File: f.File, Line: f.Line, Col: f.Col})
	}
	return out
}

// localsOf 返回第 n 帧上那些名字现在的值。
//
// en: localsOf returns the current values of the names in frame n.
func localsOf(tgt *debug.Target, sess *debug.Session, n int) []debug.VarInfo {
	if !sess.Stopped() {
		return nil
	}
	top, _ := sess.Site()
	frames := tgt.Stack(sess.RBP, top)
	if n < 0 || n >= len(frames) {
		return nil
	}
	return varList(tgt.Locals(frames[n]))
}

// varList 把解码那一路返回的位置转成线上一条话里的变量表。
//
// en: varList turns the locations the decoding path returns into a variable list inside one
// wire message.
func varList(locals []debug.Local) []debug.VarInfo {
	out := make([]debug.VarInfo, 0, len(locals))
	for _, v := range locals {
		out = append(out, debug.VarInfo{Name: v.Name, Type: v.Type, Value: v.Value, Address: uint64(v.Addr)})
	}
	return out
}

// evaluateOf 是悬停那一条：拿名字去那一帧查，帧上没有再查顶层的静态槽，两边都认不得就说认不得。
// 表达式算不了——那要在停住的那一刻跑一遍检查器与降级，这一版没有那个东西。
//
// en: evaluateOf is the hover request: look the name up in that frame, then in the top-level
// static slots if the frame has no such name, and say "unknown" if neither side knows it.
// Expressions cannot be evaluated - that would mean running the checker and the lowering at the
// moment of the stop, and this version has no such thing.
func evaluateOf(tgt *debug.Target, sess *debug.Session, n int, name string) (string, bool) {
	if !sess.Stopped() {
		return "", false
	}
	top, _ := sess.Site()
	frames := tgt.Stack(sess.RBP, top)
	if n < 0 || n >= len(frames) {
		return "", false
	}
	return tgt.Evaluate(frames[n], name)
}

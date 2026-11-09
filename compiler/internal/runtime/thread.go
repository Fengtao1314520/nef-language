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

//
// The Nef Output Exception (see the LICENSE-EXCEPTION file at the repository
// root) applies to this file: code you write and the binaries you compile with
// it are not subject to the Apache License, and you have no obligation to
// open-source them.

// thread.go 起线程、等线程，并把跑完的那一条线程留下的那块运行期状态收回来。
//
// 语言里那一个名字（`Thread<int>` 那种句柄）装的是下面这张表的表项，从 1 起，0 是空句柄，
// 理由与子进程那一档同一个：一个跑着的线程要记的不止一个数（系统给的句柄、它返回的那一个
// 64 位值、它自己那块运行期状态的起点），一个名字装不下。
//
// 表里每一格六个位置：句柄、结果、那条线程的状态块起点、被起的函数的入口地址、那一个实参、
// 空着的一格。表本身留在静态段（跨线程数的一本账），扫描与占用走一把排他锁；每一格里住的
// 东西只有那条线程自己与起它的那一方会碰。
//
// en: thread.go starts threads, waits for them, and takes back the runtime state block a
// finished thread leaves behind. The language's name (a Thread<int> handle) holds an entry
// number in the table below, counting from 1, with 0 the empty handle, for the same reason the
// child-process tier has one: a running thread needs more than one number remembered (the system
// handle, the 64-bit value it hands back, where its own runtime state block begins) and one name
// cannot carry that. Each entry has six positions: handle, result, that thread's state block,
// the entry address of the function being started, its argument, and one spare. The table is
// static - a ledger counted across threads - and scanning, taking and releasing an entry happen
// under an exclusive lock; what lives inside one entry is touched only by that thread and by
// whoever started it.

package runtime

import (
	"neflang.org/nf/internal/pe"
	"neflang.org/nf/internal/x64"
)

const (
	// threadSlots 是这张表的格数：同时挂着这么多线程就到顶了。
	// threadEntryBytes 是一格多大（六个位置，48 字节；最后一个位置现在空着）。
	threadSlots      = 128
	threadEntryBytes = 48

	// 一格里的六个位置。
	thHandle = 0  // 系统给的线程句柄，0 说明这个位置空着
	thResult = 8  // 被起的那个函数返回的那一个 64 位值
	thBlock  = 16 // 那一条线程自己的状态块起点，等过之后由等待的那一方归还
	thEntry  = 24 // 被起的那个函数的入口地址
	thArg    = 32 // 交给它的那一个实参

	// 三个号：等到的那条线程是出错结束的、表用完了、起不成。
	codeThreadErr   = 10094
	codeThreadFull  = 10095
	codeThreadStart = 10096

	// threadStackSlack 用的就是 `StackSlack` 那一个数：栈底要算成"这条线程实际能用的那一段的
	// 最低处"，与发射那一步每一帧开场比栈留的那一截必须同一条算式，所以两处共用一份号数。
	threadStackSlack = StackSlack

	memRelease = 0x00008000 // VirtualFree 归还整块时要的那个标记
	infinite   = 0xFFFFFFFF // WaitForSingleObject 的"一直等"
)

// threadData 摆那张表、三句话与那一个进门地址。
//
// threadEntry 那个位置是 @thread_begin 自己在代码段里的起点，由发射那一步在程序开局写进来
// （与默认处理那一个落点同一条做法：那个位置记的是运行期一段的地址，只有发射那一步知道自己
// 编到了哪一格）。@thread_start 把它读出来交给 CreateThread，操作系统照着它开始那条线程。
// procLock 是子进程那一张表的锁——今天有了第二条线程，扫描那一段才需要护起来。
//
// en: threadData lays out the table, the three sentences and that entry-address cell. The
// threadEntry cell holds where @thread_begin begins in the code section, written by the emit step
// at program start - the same arrangement as the default handler's landing, since only the emit
// step knows which offset each piece ends up at. @thread_start reads it and hands it to
// CreateThread, which starts the thread there. procLock guards the child-process table: a second
// thread is what makes scanning that table worth locking.
func threadData(put func(name string, bs []byte)) {
	put("threadTable", make([]byte, threadSlots*threadEntryBytes))
	put("threadEntry", make([]byte, 8))
	put("threadLock", make([]byte, 8))
	put("procLock", make([]byte, 8))
	put("sThreadErr", strObject("另一条线程出错结束了"))
	put("sThreadFull", strObject("同时跑着的线程太多了，这一张表用完了"))
	put("sThreadStart", strObject("起不来那条线程"))
}

// threadIndex 是两条等共用的开头：句柄摆在帧里第 0 格，返回 rsi＝表里那个位置的地址。
// 比表的格数还大的那一个数不是我给出去的（越界跳 bad）；0 与负数由调用方在进这一条之前先看。
//
// en: threadIndex is the shared opening of the two waiting routines: the handle sits in frame
// slot 0 and rsi comes back as the address of the table entry. A number beyond the entry count
// was never handed out by us (out of range jumps to bad); 0 and negatives are checked by the
// caller first.
func threadIndex(b *rt, bad x64.Label) {
	a := b.a
	b.get(x64.RSI, 0)
	b.get(x64.RAX, 0)
	a.CmpImm(x64.RAX, threadSlots)
	a.Jcc(x64.A, bad) // 无符号比：负的那一个数在这里就是"比格数大"
	a.AddImm(x64.RSI, -1)
	a.MovImm(x64.RDX, threadEntryBytes)
	a.Imul(x64.RSI, x64.RDX)
	b.leaCell(x64.RAX, "threadTable")
	a.Add(x64.RSI, x64.RAX)
}

// @thread_begin(rcx＝表里那个位置的地址)：操作系统开始一条线程时走进的第一段代码。
//
// 五步：备好这一条线程的状态块（R12 从此是它的起点）、把块的起点记进自己那个位置、算出这一条
// 线程自己的栈底（每个函数开场比的就是那个位置）、给这一条线程附加默认处理，然后调用被起的那个
// 函数。跑完就把返回的那一个值写进那个位置、摘掉默认处理，再叫 @thread_exit 结束这一条线程。
//
// 没人接住的那一路走的是这里附加上去的那条默认处理：打印这一条线程那一句"出错了…"与它的调用链，
// 再把那一次的错误号交给 @thread_exit 记进状态块。这里**不叫**顶层那一段归还：归还的是顶层那些
// 静态位置，属于起线程的那一层；这一条线程手里的值登记在它自己那些帧上，往外走的那一步
// （@throw_walk）已经按帧归还过了。
//
// 这一段不返回：三条出路（正常结束、没人接住的抛出、栈用完那一条）都在 @thread_exit 或
// @die_stack 里结束这条线程，所以被调方保存的那一组寄存器（rbx／rsi／rdi 与 r12～r15）
// 不必寄回来。系统要求线程起点最后返回一个数，这里一次也没有返回——那是有意的。
//
// en: @thread_begin (rcx = the address of this thread's table entry) is the first code the
// operating system runs on a new thread. Five steps: prepare this thread's state block (R12 is
// its start from then on), record where that block begins into its own entry, compute this
// thread's stack floor - the cell every frame compares against - attach this thread's default
// handler, then call the function being started. When it returns, the value handed back is
// written into that entry, the handler comes off, and @thread_exit ends this thread.
//
// An uncaught throw lands on the handler attached here: it prints this thread's error line and
// its call chain, then gives that throw's code to @thread_exit, which records it in the state
// block. It does NOT run the top-level release - that returns the top-level static positions,
// which belong to whoever started the thread; this thread's values are registered on its own
// frames, and the walk outward (@throw_walk) has already returned them frame by frame.
//
// This section never returns: all three exits (a normal end, an uncaught throw, the stack running
// out) end the thread inside @thread_exit or @die_stack, so the callee-saved registers (rbx, rsi,
// rdi and r12-r15) have nothing to give back. The system expects a thread start to return a
// number; this one never does, deliberately.
func threadBegin(b *rt) {
	const (
		bgEnt = 0 // 自己那个位置的地址
	)
	a := b.a
	b.prologue()
	b.put(bgEnt, x64.RCX)
	b.call("@state_init")
	b.get(x64.RDX, bgEnt)
	a.MovStore(x64.RDX, thBlock, x64.R12) // 块起点记进那个位置：等过之后由等的那一方归还
	// 自己那一张表的位置也留一份在这块状态里：@die_stack 那一路不经过处理器链，它要认出自己
	// 走的是哪一条线程只能问这一个数。程序那一条线程那个位置从来没人写，读到的是 0。
	b.get(x64.RCX, bgEnt)
	b.storeState(x64.RCX, StSelfEntry)
	// 这一条线程自己的栈底：这一段的栈基减去语言答应的栈，再让出每一帧共用的那一截。
	a.MovFromTeb(x64.RAX, TebStackBase)
	a.AddImm(x64.RAX, -(pe.StackReserve - threadStackSlack))
	b.storeState(x64.RAX, StStackFloor)
	// 这一条线程的默认处理：没人接住的抛出走到这里为止。
	catch := b.label()
	a.LeaLabel(x64.RCX, catch)
	b.call("@push_handler")
	b.get(x64.R11, bgEnt)
	a.MovLoad(x64.R11, x64.R11, thEntry) // 被起的那个函数从哪儿开始
	b.get(x64.RCX, bgEnt)
	a.MovLoad(x64.RCX, x64.RCX, thArg) // 交给它的那一个实参
	a.CallReg(x64.R11)
	b.get(x64.RDX, bgEnt)
	a.MovStore(x64.RDX, thResult, x64.RAX) // 返回的那一个值留在表里
	b.call("@pop_handler")                 // 正常跑完：摘掉，链上不留残留
	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, 0)
	b.call("@thread_exit") // 正常结束：错误号留 0

	// 没人接住的那一路。@throw_walk 命中这一条时已经把它从链上摘下来并归还了，
	// 码与话还在这条线程的状态块里。
	a.Mark(catch)
	b.loadState(x64.RCX, StThrowCode)
	b.loadState(x64.RDX, StThrowMsg)
	b.call("@put_errline")
	b.call("@trace_print") // 这一条线程自己的调用链
	a.MovImm(x64.RCX, 1)
	b.loadState(x64.RDX, StThrowCode)
	b.call("@thread_exit") // 错误号交给它记进状态块，等的那一方读的就是那个位置
	b.epilogue()
}

// @thread_start(rcx＝被起的函数的入口地址, rdx＝交给它的那一个实参)：返回 rax＝句柄（1 起的表项）。
//
// 占一格与起线程这一段握着一把锁：两条线程同时起线程不能各挑到同一格。CreateThread 的第二个
// 入参写 0，意思是"按产物头部那一个数给栈"——那正是语言答应的 8 MiB，每一条线程一份，与
// @thread_begin 里算栈底用的同一个数。线程号不取：这里没有要拿它去问系统别的信息的用途。
//
// 表用完了当场 10095 号；CreateThread 给不出句柄是 10096 号，带上系统原号码，与起进程那一条
// 同一种答复。两种失败都不留下半占的那个位置。
//
// en: @thread_start (rcx = entry address of the function to start, rdx = the argument handed to
// it) returns rax = the handle (a table entry from 1). Taking an entry and starting the thread
// happen under one lock: two threads starting threads at once must not pick the same entry.
// CreateThread's second parameter is 0, meaning "give it the stack the product header asks for" -
// exactly the 8 MiB the language promises, one per thread, the same number @thread_begin uses for
// its stack floor. The thread identifier is not taken: nothing here asks the system with it.
//
// A full table throws 10095 on the spot; when CreateThread hands back no handle the code is 10096
// with the system's number attached, the same answer as the child-process tier. Neither failure
// leaves a half-taken entry.
func threadStart(b *rt) {
	const (
		zFn    = 0 // 被起的那个函数的入口地址
		zArg   = 1 // 交给它的那一个实参
		zI     = 2 // 走到表里第几格
		zEntry = 3 // 那个位置的地址
		zHp    = 4 // CreateThread 返回的句柄
		zErr   = 5 // 系统号码的落点
	)
	a := b.a
	b.prologue()
	b.put(zFn, x64.RCX)
	b.put(zArg, x64.RDX)
	b.srwTake("threadLock")

	// 找第一格空着的：句柄那个位置是 0 就是没人挂着。
	a.Xor(x64.RAX, x64.RAX)
	b.put(zI, x64.RAX)
	b.leaCell(x64.RSI, "threadTable")
	b.put(zEntry, x64.RSI)
	next := b.label()
	full := b.label()
	a.Mark(next)
	b.get(x64.RAX, zI)
	a.CmpImm(x64.RAX, threadSlots)
	a.Jcc(x64.AE, full)
	b.get(x64.RDX, zEntry)
	a.MovLoad(x64.RCX, x64.RDX, thHandle)
	a.Test(x64.RCX, x64.RCX)
	taken := b.label()
	a.Jcc(x64.E, taken)
	a.AddImm(x64.RDX, threadEntryBytes)
	b.put(zEntry, x64.RDX)
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1)
	b.put(zI, x64.RAX)
	a.Jmp(next)

	a.Mark(taken)
	// 占下这个位置：先写要用的两样，句柄等 CreateThread 给。
	b.get(x64.RAX, zEntry)
	b.get(x64.RCX, zFn)
	a.MovStore(x64.RAX, thEntry, x64.RCX)
	b.get(x64.RCX, zArg)
	a.MovStore(x64.RAX, thArg, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)          // lpThreadAttributes：不给安全属性
	a.Xor(x64.RDX, x64.RDX)          // dwStackSize＝0：按头部那一个数
	b.leaCell(x64.R8, "threadEntry") // lpStartAddress：那条线程进门走的第一段代码
	a.MovLoad(x64.R8, x64.R8, 0)
	b.get(x64.R9, zEntry) // lpParameter：这个位置的位置，@thread_begin 照着它认回自己那一份
	a.MovStore(x64.RBP, arg5, x64.RCX)
	a.MovStore(x64.RBP, arg6, x64.RCX) // lpThreadId：不要
	b.impCall("CreateThread")
	a.MovStore(x64.RBP, int32(-8*(zHp+1)), x64.RAX)
	a.MovZX32(x64.RDX, x64.RBP, int32(-8*(zHp+1))) // 句柄只用得到低四字节，上面那半截抹干净
	a.Test(x64.RDX, x64.RDX)
	okHp := b.label()
	a.Jcc(x64.NE, okHp)

	// 起不成：这个位置整个抹回去，报 10096 那一句带系统号码。
	b.get(x64.RAX, zEntry)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore(x64.RAX, thEntry, x64.RCX)
	a.MovStore(x64.RAX, thArg, x64.RCX)
	b.srwGive("threadLock")
	b.impCall("GetLastError")
	a.MovStore(x64.RBP, int32(-8*(zErr+1)), x64.RAX)
	a.MovZX32(x64.RCX, x64.RBP, int32(-8*(zErr+1)))
	b.leaCell(x64.RDX, "sThreadStart")
	a.MovImm(x64.R8, codeThreadStart)
	b.call("@proc_fail") // 拼"那句（系统的号码 N）"再抛出，从不返回

	a.Mark(okHp)
	b.get(x64.RAX, zEntry)
	b.get(x64.RDX, zHp)
	a.MovStore(x64.RAX, thHandle, x64.RDX)
	b.srwGive("threadLock")
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1) // 句柄从 1 起，0 留给空句柄
	b.epilogue()

	a.Mark(full)
	b.srwGive("threadLock") // 先放开再抛出：抛出那一步要归还一块内存，那是另一条拿锁的路
	b.throwWith(codeThreadFull, "sThreadFull")
	b.epilogue()
}

// threadTail 是等过之后的共同收尾：归还那条线程的状态块、关掉句柄、把这个位置抹干净。
// 三样都握着一把锁做完，所以同一格不会有两个等待的人各拆一半。
// 每一次系统调用之后都从帧里重取那个位置的地址：rcx／rdx 与 r8～r11 都是调用方寄存器。
//
// en: threadTail is the shared tail after waiting: give back that thread's state block, close the
// handle, wipe the entry. All three run under one lock, so one entry cannot be dismantled half by
// each of two waiters. After every system call the entry's address comes back from the frame:
// rcx, rdx and r8-r11 are caller-saved.
func threadTail(b *rt, zEntry int) {
	a := b.a
	b.srwTake("threadLock")
	b.get(x64.RCX, zEntry)
	a.MovLoad(x64.RCX, x64.RCX, thBlock)
	noBlock := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, noBlock) // 那条线程还没备好块就没了：本来就没有要归还的东西
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.R8, memRelease)
	b.impCall("VirtualFree")
	a.Mark(noBlock)
	b.get(x64.RCX, zEntry)
	a.MovLoad(x64.RCX, x64.RCX, thHandle)
	b.impCall("CloseHandle")
	b.get(x64.RSI, zEntry)
	a.Xor(x64.RCX, x64.RCX)
	for off := 0; off <= thArg; off += 8 {
		a.MovStore(x64.RSI, int32(off), x64.RCX)
	}
	b.srwGive("threadLock")
}

// @thread_wait(rcx＝句柄) 与 @thread_join(rcx＝句柄)：等那一条线程跑完，返回它返回的那一个值。
//
// 两条动作一样，只有一处不同：`wait()` 读到那条线程是出错结束的（那个位置不是 0）就当场抛
// 10094 号那一句，而名字走到作用域尽头时自动等的那一次不抛——那一句"出错了…"已经由那条线程
// 自己打印过了（它自己那条默认处理），在主线程的归还代码中间再抛一次会把归还打断。
//
// 等待用的是线程那一把句柄，一直等到它跑完。等的答复不看：那个位置里的句柄是我们自己给的，
// 也只有这里会关闭它，等不到只可能是程序从别处走错了路过来（与归还一块已经关过的句柄同一种
// 处理：不看系统的答复，看的是这件事做完没有）。
//
// 顺序是钉死的：先读那一个值与那一个错误号，再走收尾那一段（归还块、关句柄、抹格子），最后才
// 抛出。抛出之前格子已经抹干净，所以接住块之后这个名字走到尽头时自动等那一次看到的是 0，
// 不做处理。
//
// en: @thread_wait (rcx = handle) and @thread_join (rcx = handle) wait for that thread to finish
// and return the value it handed back. They do the same thing with one difference: wait() throws
// code 10094 when it reads that the thread ended with an error (that cell is not 0), while the
// automatic wait at the end of a name's scope does not - that thread already printed its own
// error line in its default handler, and throwing a second time in the middle of the main
// thread's clean-up would interrupt the clean-up.
//
// The wait uses the thread handle and waits until it finishes. Its answer is not examined: the
// handle in that entry came from us and only here closes it, so a failed wait could only be the
// program reaching this from somewhere wrong - the same treatment as closing an already closed
// handle, looking at whether the work got done rather than at the system's reply.
//
// The order is fixed: read the value and the error number, run the tail (give the block back,
// close the handle, wipe the entry), and only then throw. The entry is already clean before the
// throw, so when this name later reaches the end of its scope the automatic wait sees 0 and does
// nothing.
func threadWait(b *rt) { threadWaitBoth(b, true) }

// en: @thread_join is the non-throwing twin of @thread_wait, used where a name ends its scope.
func threadJoin(b *rt) { threadWaitBoth(b, false) }

func threadWaitBoth(b *rt, throwOnErr bool) {
	const (
		wHp  = 1 // 系统给的那条线程的句柄
		wEnt = 2 // 表里那个位置的地址
		wRes = 3 // 那一条线程返回的值
		wErr = 4 // 那一条线程留下的错误号
	)
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)

	bad := b.label() // 那一个数不是我给出去的：与"这个位置已经收过了"同一条答复
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.LE, bad)
	threadIndex(b, bad)
	b.put(wEnt, x64.RSI)
	a.MovLoad(x64.RDX, x64.RSI, thHandle)
	gone := b.label() // 表里那个位置已经收过了
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, gone)
	b.put(wHp, x64.RDX)

	a.MovImm(x64.RDX, infinite)
	b.get(x64.RCX, wHp)
	b.impCall("WaitForSingleObject")

	// 那一样值与那一个错误号：错误号在那条线程自己的块里，块这时还没归还，读得到。
	b.get(x64.RAX, wEnt)
	a.MovLoad(x64.RDX, x64.RAX, thResult)
	b.put(wRes, x64.RDX)
	a.MovLoad(x64.RDX, x64.RAX, thBlock)
	a.Test(x64.RDX, x64.RDX)
	noBlock := b.label()
	gotErr := b.label()
	a.Jcc(x64.E, noBlock)
	a.MovLoad(x64.RDX, x64.RDX, StThreadError)
	b.put(wErr, x64.RDX)
	a.Jmp(gotErr)
	a.Mark(noBlock)
	a.Xor(x64.RDX, x64.RDX)
	b.put(wErr, x64.RDX)
	a.Mark(gotErr)

	threadTail(b, wEnt)
	b.get(x64.RAX, wRes)
	if throwOnErr {
		behind := b.label()
		b.get(x64.RDX, wErr)
		a.Test(x64.RDX, x64.RDX)
		a.Jcc(x64.E, behind)
		a.MovImm(x64.RCX, codeThreadErr)
		b.leaCell(x64.RDX, "sThreadErr")
		b.call("@throw")
		a.Mark(behind)
	}
	b.epilogue()

	a.Mark(gone)
	a.Mark(bad)
	if throwOnErr {
		// 空句柄与"已经等过一次"落同一句答复：对着空句柄取成员那一族。
		b.throwWith(codeNullRef, "sNullRef")
		b.epilogue()
	}
	a.Xor(x64.RAX, x64.RAX) // 自动等的那一次：那个名字不算数了就什么都不做
	b.epilogue()
}

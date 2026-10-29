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

package runtime

// proc.go 是 `process` 这一档：起一个程序、等它跑完拿退出码、不再等待就结束它的进程句柄。
// web 端自动化测试框架要的第一件地基——CDP 那一条路要求浏览器带着 `--remote-debugging-port`
// 与一只独立的 `--user-data-dir` 起来，这一档就是"语言自己把那个进程带起来、也自己收尾"。
//
// 三条例程，全问 kernel32，不碰 shell：
//
//	@proc_start(命令行文本)   → 句柄（1 起的表项），起不成当场 10111 号
//	@proc_wait(句柄)          → 退出码，等不到或问不到落 10110 号
//	@proc_close(句柄)         → 什么都不返回；没等它就结束它，句柄关掉，表里那个位置抹成 0
//
// 手上那个数不是系统进程句柄的理由：一个子进程有两把句柄（进程一把、线程一把），
// 而语言里那一个名字只装得下一个数。所以这一档把进程句柄存进 `procTable` 那一百二十八格，
// 交给用户的是表项（从 1 起，0 一律是"手上没有进程"，与空句柄检查同一个摆法）。
// 线程句柄在起成那一刻就当场关闭：这一版不管线程，等待与终止都只针对进程那一把句柄。
//
// 命令行那一串原样交给 `CreateProcessW`（第一个位置传 0，程序名就是这一串的头一段）。
// 语言不替用户拆参数、也不替用户加引号——带空格的路径要自己写成 `"…"`，
// 这一句对用户写明，与 C# 的 `ProcessStartInfo.Arguments` 同一个分工。
//
// en: proc.go is the `process` tier: start a program, wait for it to finish and take its exit
// code, or end it without waiting. The automation test framework needs this first: the CDP path
// requires the browser to start with --remote-debugging-port and a separate --user-data-dir,
// and this tier means the language starts that process itself and cleans it up itself. Three
// routines, all asking kernel32, never a shell:
//
//	@proc_start(command line text) -> handle (a table entry from 1), code 10111 on the spot if
//	it cannot start
//	@proc_wait(handle)             -> exit code, code 10110 if waiting or asking fails
//	@proc_close(handle)            -> returns nothing; terminate without waiting, close the
//	handle, zero the table entry
//
// The number in hand is not the system's process handle: a child process owns two handles
// (process and thread) while one name in the language holds only one number, so this tier keeps
// the process handle in one of the 128 procTable slots and hands the user the entry number
// (from 1; 0 always means "no process in hand", the same arrangement as the empty-handle check).
// The thread handle is closed on the spot once the process starts: this version does not touch
// threads, and waiting and terminating only address the process handle. The command line is
// passed to CreateProcessW as it is (the first argument is 0; the program name is the first
// token of the run). The language neither splits arguments nor adds quotes for the user - a
// path with spaces must be written "..." by the user, which is stated for the user and is the
// same division of work as C#'s ProcessStartInfo.Arguments.

import "neflang.org/nf/internal/x64"

// 三个号（新开的一段）：10110 是这一段的兜底，10111 是"起不来"，10112 是"表用完了"。
// win32 有上千个号码，一个个折算会把这张表撑爆——与网络那一段同一处置：
// 号只占三个，系统给的原号码一律带在消息里。
//
// en: Three codes (a newly opened band): 10110 is this band's fallback, 10111 is "it
// would not start", 10112 is "the table is full". win32 has thousands of numbers and converting
// each one would burst this table - the same treatment as the network band: three codes only,
// and the system's original number is always carried in the message.
const (
	codeProcAsk   = 10110 // 兜底：等不到那个程序、问不到退出码
	codeProcStart = 10111 // 起不来那个程序
	codeProcFull  = 10112 // 同时跑着的子进程用完了这一张表
)

const (
	procSlots           = 128 // 表里几格：同时在手上挂着这么多子进程就到顶了
	procStartupInfoSize = 104 // STARTUPINFOW 在这一档上是 104 个字节（x64）
)

// procData 摆表、那两套结构、退出码那个位置，与四句给用户看的话。
//
// STARTUPINFOW 只填头一个位置 `cb`（我给它这一套多大），其余九十八个字节留着静态区的 0：
// `dwFlags` 是 0 就是"标准流三根都不接、窗口位置与大小都归系统定"。
// PROCESS_INFORMATION 是 24 个字节（进程句柄、线程句柄、两个号码），这里摆 32 个字节够用。
//
// en: procData lays out the table, the two structures, the exit-code position, and the four
// sentences shown to the user. Only the first field of STARTUPINFOW, cb (how big this copy is),
// is filled and the remaining ninety-eight bytes stay zero in the static area: dwFlags of 0
// means "none of the three standard streams is redirected, and the window position and size are
// the system's". PROCESS_INFORMATION is 24 bytes (process handle, thread handle, two
// identifiers); 32 are laid out here, which is enough.
func procData(put func(name string, bs []byte)) {
	put("procTable", make([]byte, procSlots*8))
	put("procSI", make([]byte, 128))
	put("procPI", make([]byte, 32))
	put("procExit", make([]byte, 8)) // GetExitCodeProcess 写低四格，高四格一直是 0
	put("sProcStart", strObject("起不来那个程序"))
	put("sProcWait", strObject("等不到那个程序"))
	put("sProcExit", strObject("问不到那个程序的退出码"))
	put("sProcFull", strObject("同时跑着的子进程太多了，这一张表用完了"))
}

// @proc_fail(rcx=系统的号码（0＝这一处不是系统给的答复）, rdx=那一句, r8=号)：
// 拼出「那一句（系统的号码 N）」再 @throw，从不返回东西。
//
// 括号码那两截借网络那一段的静态文本（`sNetNum` 与 `sNetClose`）：那两句说的是「（系统的号码 」与「）」，
// 与是哪一组没有关系，再造一份同样的文字只是多摆一块内存。
// 拼串只用 @str_cat 与 @int_str，每拼一次造新的一块、不动两侧，旧的那半随手 @free——与 netFail 同一条。
//
// en: @proc_fail (rcx = system number, 0 meaning this answer did not come from the system, rdx
// = the sentence, r8 = the code): assembles "the sentence (system number N)" and calls @throw,
// never returning. The two bracket fragments borrow the network band's static text (sNetNum and
// sNetClose): those two say "(system number " and ")" and have nothing to do with which group,
// so a third copy would be one more block of memory for the same words. Assembly uses only
// @str_cat and @int_str, each join building a new block and leaving both sides alone, the older
// half @free'd immediately - the same rule as netFail.
func procFail(b *rt) {
	const (
		fNum  = 0 // 系统的号码
		fText = 1 // 拼到现在那一段
		fPart = 2 // 刚拼出来的那一块
		fCode = 3 // 号
		fDig  = 4 // 号码写成文本
	)
	a := b.a
	b.prologue()
	b.put(fNum, x64.RCX)
	b.put(fText, x64.RDX)
	b.put(fCode, x64.R8)

	// 手里那一段 ＋ piece → 手里换成新拼的那一块，旧的那一块当场归还。
	// 静态池那几条头上尺寸是 0，@free 走到就是空操作，所以"手里是写死的那一句"
	// 与"手里是刚拼出来的一块"走同一条收尾，不必分两种情况。
	cat := func(piece func()) {
		b.get(x64.RCX, fText)
		piece()
		b.call("@str_cat")
		b.put(fPart, x64.RAX)
		b.get(x64.RCX, fText)
		b.call("@free")
		b.get(x64.RAX, fPart)
		b.put(fText, x64.RAX)
	}
	from := func(name string) func() { return func() { b.leaRIP(x64.RDX, name) } }

	noNum := b.label()
	b.get(x64.RAX, fNum)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noNum)
	cat(from("sNetNum"))
	b.get(x64.RCX, fNum)
	b.call("@int_str")
	b.put(fDig, x64.RAX)
	cat(func() { b.get(x64.RDX, fDig) })
	b.get(x64.RCX, fDig) // 那一串数字是 @int_str 新造的，拼进句子里就归还
	b.call("@free")
	cat(from("sNetClose"))
	a.Mark(noNum)

	b.get(x64.RCX, fCode)
	b.get(x64.RDX, fText)
	b.call("@throw")
	b.epilogue()
}

// procFailLastError 是失败那三处共同的收尾：先把 rax 里那一个 DWORD（系统的号码）落进
// 帧里第 slot 格，再按 32 位零扩展读回来当第一个入参，叫 @proc_fail。
//
// 绕这一道的理由：kernel32 那几条返回的是 C 的 `DWORD`，只用得到低 32 位，
// 高 32 位按约定不做承诺——`@str_cat` 那头要的却是一个干净的 64 位数。
//
// en: procFailLastError is the shared tail of the three failure sites: store the DWORD (system
// number) in rax into frame slot `slot`, read it back zero-extended as 32 bits as the first
// argument, and call @proc_fail. The detour is there because those kernel32 routines return a
// C DWORD and only the low 32 bits are meaningful, with the high half explicitly unspecified,
// while @str_cat on the other end wants a clean 64-bit number.
func procFailLastError(b *rt, slot int, msgName string, code int64) {
	a := b.a
	a.MovStore(x64.RBP, int32(-8*(slot+1)), x64.RAX)
	a.MovZX32(x64.RCX, x64.RBP, int32(-8*(slot+1)))
	b.leaRIP(x64.RDX, msgName)
	a.MovImm(x64.R8, code)
	b.call("@proc_fail")
}

// procIndex 是 wait 与 close 共用的开头：句柄摆在帧里第 0 格，返回 rsi＝表里那个位置的地址。
//
// 句柄从 1 起，所以表项是"句柄 − 1"。比表的格数还大的那一个数不是我给出去的（越界跳 bad）；
// 0 与负数由调用方在进这一条之前先看，因为那两头的答复不一样。
// 算出来的地址跨不过一次系统调用（rsi 与 rdi 都是调用方寄存器），所以每叫一次系统都要
// 重新取句柄、重算一遍——不把这份地址保留在别的寄存器上，与 base64 那两条同一规矩。
//
// en: procIndex is the shared opening of wait and close: the handle sits in frame slot 0 and
// rsi comes back as the address of the table entry. Handles start at 1, so the entry is handle
// - 1. A number beyond the entry count was never handed out by us (out of range jumps to bad);
// 0 and negatives are checked by the caller before entering, because those two answer
// differently. The computed address cannot survive a system call (rsi and rdi are
// caller-saved), so every system call is followed by reloading the handle and recomputing -
// the address is never kept in another register, the same rule as the two base64 routines.
func procIndex(b *rt, bad x64.Label) {
	a := b.a
	b.get(x64.RSI, 0)
	b.get(x64.RAX, 0)
	a.CmpImm(x64.RAX, procSlots)
	a.Jcc(x64.A, bad) // 无符号比：负的那一个数在这里就是"比格数大"
	a.AddImm(x64.RSI, -1)
	a.ShlImm(x64.RSI, 3)
	b.leaRIP(x64.RAX, "procTable")
	a.Add(x64.RSI, x64.RAX)
}

// @proc_start(rcx=那一串命令行)：返回 rax=句柄（1 起的表项）。
//
// 命令行翻成宽字符摆进 `wPath`（与文件、网络那几条同一块缓冲，一次调用只用它一回），
// 这一串是可写的：`CreateProcessW` 明说它可能改这一串，静态区本来就可写，不必另拷一份。
// 空的命令行与装不下的那一种在翻那一步就看出来了（@to_wide 返回 0），当场 10111 号，不挂号码。
//
// en: @proc_start (rcx = the command line): returns rax = the handle (a table entry numbered
// from 1). The command line is translated into wide characters in wPath (the same buffer the
// file and network routines use, touched once per call), and that run is writable:
// CreateProcessW states it may modify the string, and the static area is writable, so no copy
// is made. An empty command line and one that does not fit are both spotted at the translation
// step (@to_wide returning 0) and throw 10111 on the spot with no system number attached.
func procStart(b *rt) {
	const (
		zCmd  = 0 // 那一串命令行（文本）
		zI    = 1 // 走到表里第几格
		zBase = 2 // 表在哪儿
		zErr  = 3 // GetLastError 那一个 DWORD 的落点
	)
	a := b.a
	b.prologue()
	b.put(zCmd, x64.RCX)

	badCmd := b.label() // 空的或装不下：翻不成宽字符
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, zCmd)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badCmd)

	b.leaRIP(x64.RAX, "procSI")
	a.MovImmMem(x64.RAX, 0, procStartupInfoSize) // cb＝我交给它这一套多大
	b.leaRIP(x64.RAX, "procSI")
	a.MovStore(x64.RBP, arg9, x64.RAX)
	b.leaRIP(x64.RAX, "procPI")
	a.MovStore(x64.RBP, arg10, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX) // 标准流三根不继承：子进程的话由它自己的那一头收
	a.MovStore(x64.RBP, arg6, x64.RAX) // 创建标志：一个都不设
	a.MovStore(x64.RBP, arg7, x64.RAX) // 环境块：0＝沿用这一趟的
	a.MovStore(x64.RBP, arg8, x64.RAX) // 工作目录：0＝站在哪儿起就在哪儿起
	a.Xor(x64.RCX, x64.RCX)            // 程序名不单独给：命令行头一段就是它
	b.leaRIP(x64.RDX, "wPath")         // 命令行：上面刚翻好，原样交给系统
	a.Xor(x64.R8, x64.R8)              // 线程安全属性：默认
	a.Xor(x64.R9, x64.R9)              // 进程安全属性：默认
	b.impCall("CreateProcessW")        // 十个参数：后六个在帧的那一侧（arg5～arg10）
	startFail := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, startFail)

	// 起成了：往表里找一个位置句柄摆进去。找不着就是手上挂的太多了
	b.leaRIP(x64.RAX, "procTable")
	b.put(zBase, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(zI, x64.RAX)
	top, free, full := b.label(), b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, zI)
	a.CmpImm(x64.RAX, procSlots)
	a.Jcc(x64.AE, full)
	b.get(x64.RSI, zBase)
	b.get(x64.RAX, zI)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RSI, 0)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, free)
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1)
	b.put(zI, x64.RAX)
	a.Jmp(top)

	a.Mark(free)
	b.leaRIP(x64.RAX, "procPI")
	a.MovLoad(x64.RDX, x64.RAX, 0)
	a.MovStore(x64.RSI, 0, x64.RDX) // 进程那一把把手摆进这一格
	a.MovLoad(x64.RCX, x64.RAX, 8)  // 线程那一把：这一版不等它，当场关掉
	b.impCall("CloseHandle")
	b.get(x64.RAX, zI)
	a.AddImm(x64.RAX, 1) // 句柄从 1 起：0 留给"手上没有进程"
	b.epilogue()

	a.Mark(full)
	a.MovImm(x64.RCX, 0) // 这一处不是系统给的答复，只有那一句
	b.leaRIP(x64.RDX, "sProcFull")
	a.MovImm(x64.R8, codeProcFull)
	b.call("@proc_fail")

	a.Mark(startFail)
	b.impCall("GetLastError")
	procFailLastError(b, zErr, "sProcStart", codeProcStart)

	a.Mark(badCmd)
	a.MovImm(x64.RCX, 0)
	b.leaRIP(x64.RDX, "sProcStart")
	a.MovImm(x64.R8, codeProcStart)
	b.call("@proc_fail")
	b.epilogue()
}

// @proc_wait(rcx=句柄)：返回 rax=那一个程序的退出码。
//
// 等的是进程那一把句柄，一直等到它跑完（INFINITE）：这一档没有线程，"跑完"就是进程没了。
// 等到了再问一次退出码。`WaitForSingleObject` 返回的是 DWORD，先落一帧、再按 32 位零扩展读回来，
// 比较的那个数才干净——同一类缺陷的另一头见 net.go 的 signExt。
//
// 0、负数、比表的格数大的那一个数都是 10092 号（对着空句柄取成员或调用方法那一族）；
// 表里那个位置已经是 0（收过了）是 10110 号那一句"等不到那个程序"，不挂号码。
//
// en: @proc_wait (rcx = handle): returns rax = that program's exit code. It waits on the
// PROCESS handle until it finishes (INFINITE): with no threads in this tier, "finished" means
// the process is gone. Only afterwards is the exit code asked. WaitForSingleObject returns a
// DWORD, so it is stored to the frame and read back zero-extended as 32 bits before the
// comparison is clean - the other end of the same defect is signExt in net.go. 0, negatives and
// numbers beyond the entry count all take code 10092 (the family of taking a member or
// calling a method on an empty handle); a table entry already 0 (already collected) gives the
// 10110 sentence "that program cannot be waited for", with no system number attached.
func procWait(b *rt) {
	const (
		wHp  = 1 // 进程那一把把手
		wErr = 2 // 系统号码的落点
	)
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)

	bad := b.label()  // 句柄不是我给的那一个数
	gone := b.label() // 表里那一格已经收过了
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.LE, bad)
	procIndex(b, bad)
	a.MovLoad(x64.RDX, x64.RSI, 0)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, gone)
	b.put(wHp, x64.RDX)

	b.get(x64.RCX, wHp)
	a.MovImm(x64.RDX, 0xFFFFFFFF) // INFINITE：等到了就是 0
	b.impCall("WaitForSingleObject")
	a.MovStore(x64.RBP, int32(-8*(wErr+1)), x64.RAX)
	a.MovZX32(x64.RAX, x64.RBP, int32(-8*(wErr+1)))
	notWaited := b.label()
	a.Test(x64.RAX, x64.RAX) // WAIT_OBJECT_0 就是 0
	a.Jcc(x64.NE, notWaited)

	b.get(x64.RCX, wHp)
	b.leaRIP(x64.RDX, "procExit")
	b.impCall("GetExitCodeProcess")
	a.Test(x64.RAX, x64.RAX)
	noCode := b.label()
	a.Jcc(x64.E, noCode)
	b.leaRIP(x64.RAX, "procExit")
	a.MovLoad(x64.RAX, x64.RAX, 0) // 高四格一直是静态区的 0，读八个字节就是那一个退出码
	b.epilogue()

	a.Mark(notWaited)
	b.impCall("GetLastError")
	procFailLastError(b, wErr, "sProcWait", codeProcAsk)

	a.Mark(noCode)
	b.impCall("GetLastError")
	procFailLastError(b, wErr, "sProcExit", codeProcAsk)

	a.Mark(gone)
	a.MovImm(x64.RCX, 0)
	b.leaRIP(x64.RDX, "sProcWait")
	a.MovImm(x64.R8, codeProcAsk)
	b.call("@proc_fail")

	a.Mark(bad)
	b.throwWith(codeNullRef, "sNullRef")
	b.epilogue()
}

// @proc_close(rcx=句柄)：不等它就结束它，进程句柄关掉，表里那个位置抹成 0。
//
// 0 与负数不做处理（名字作废那一处发的就是空句柄，槽里也可能存着 0）——与 @net_close 同一收尾。
// 表里那个位置已经是 0 也不做处理：同一个句柄不会被关闭第二次。
// 已经跑完的那一种再 TerminateProcess 一次没有副作用：系统只回一句"这个进程已经结束了"，
// 这一条不看它的答复，看的是句柄关没关掉。
// 每叫一次系统都要重算格子地址：rsi 是调用方寄存器，被覆盖后就重新取句柄再算。
//
// en: @proc_close (rcx = handle): end it without waiting, close the process handle, and write
// the table entry to 0. 0 and negatives do nothing (that is what is emitted where a name is
// voided, and a slot may also hold 0) - the same tail as @net_close. An entry already 0 also
// does nothing, so one handle cannot be closed twice. Calling TerminateProcess again on a
// program that already finished has no side effect: the system just says "this process has
// already ended", and this routine ignores that answer and looks only at whether the handle got
// closed. Every system call is followed by recomputing the slot address: rsi is caller-saved,
// so reload the handle and recompute.
func procClose(b *rt) {
	const zHp = 1 // 进程那一把把手
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)

	bad := b.label()
	done := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.LE, done) // 空句柄：不必再查表
	procIndex(b, bad)
	a.MovLoad(x64.RDX, x64.RSI, 0)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, done) // 这一格早收过了
	b.put(zHp, x64.RDX)

	b.get(x64.RCX, zHp)
	a.Xor(x64.RDX, x64.RDX) // 结束码 0：这一条是"不等了"，不是"它自己算错了"
	b.impCall("TerminateProcess")
	b.get(x64.RCX, zHp)
	b.impCall("CloseHandle")

	b.get(x64.RCX, 0)
	procIndex(b, bad) // 重算一次：上面两次调用覆盖了 rsi
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RSI, 0, x64.RAX)
	a.Mark(done)
	b.epilogue()

	a.Mark(bad)
	b.throwWith(codeNullRef, "sNullRef")
	b.epilogue()
}

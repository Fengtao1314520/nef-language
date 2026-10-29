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

// run_windows.go 是 `nf hello.nf 甲 乙` 那一条：编译在内存里做完就执行，
// 不落任何中间文件。
//
// 内存布局与 exe 里的完全一致：代码在前、数据在后，两段的相对差与 PE 里相同，
// 所以每一处 rip 相对位移都不用重算。不一样的只有三件事——
// 没有加载器替我们回填导入表，于是逐槽用 GetProcAddress 填真地址；
// 程序的命令行也不是本机那条（里面还带着 `nf hello.nf`），于是把本程序
// 自己的参数按 Windows 的写法拼成宽字符，写进运行期预留的那个位置 cmdW；
// 往那块内存里写入要走 WriteProcessMemory——它由操作系统掌管，不在 Go 的管辖里。
//
// 退出码不用宿主传：产物收尾本来就 call ExitProcess，那个数就是进程的退出码。
//
// en: run_windows.go is the `nf hello.nf arg1 arg2` path: compilation finishes in memory and
// the code runs right there, with no intermediate file written. The arrangement is
// exactly the one inside an exe - code first, data after, with the same distance between the
// two sections as in the PE file, so no rip-relative displacement has to be recomputed. Only
// three things differ: no loader fills the import table for us, so every slot is filled with a
// real address from GetProcAddress; the program's command line is not the machine's own (that
// one still says `nf hello.nf`), so this program's own arguments are joined into a
// wide-character command line the Windows way and written into the slot the runtime reserved,
// cmdW; and writing into that memory goes through WriteProcessMemory - the operating system
// owns it, outside Go's jurisdiction. The exit code needs no hand-over from the host: the
// product's tail already calls ExitProcess, and that number is the process's exit code.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/pe"
	"neflang.org/nf/internal/pkgload"
)

// 系统调用与几个标志位，都从本机已经加载好的 kernel32 里取，不额外加载其他模块。
//
// en: System calls and a handful of flags, all taken from the kernel32 already loaded on this
// machine - nothing else is loaded.
const (
	memCommitReserve     = 0x3000 // MEM_COMMIT | MEM_RESERVE
	pageExecuteReadWrite = 0x40   // PAGE_EXECUTE_READWRITE
	waitInfinite         = 0xFFFFFFFF
	currentProcess       = ^uintptr(0) // GetCurrentProcess 交回的那个伪句柄
	hostSpare            = 1 << 20     // 接在映像尾部之后，用来放参数串
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procVirtualAlloc     = kernel32.NewProc("VirtualAlloc")
	procLoadLibrary      = kernel32.NewProc("LoadLibraryW")
	procGetProcAddress   = kernel32.NewProc("GetProcAddress")
	procCreateThread     = kernel32.NewProc("CreateThread")
	procWaitForSingle    = kernel32.NewProc("WaitForSingleObject")
	procWriteProcessMemo = kernel32.NewProc("WriteProcessMemory")
)

// cmdRun 编译 args[0] 那个 .nf 文件并直接执行，后面的都算程序的参数。
//
// en: cmdRun compiles the .nf file named by args[0] and executes it directly; everything after
// it counts as the program's arguments.
func cmdRun(args []string) int {
	entry := filepath.ToSlash(args[0])
	prog := &check.Program{Entry: entry}
	seen := map[string]bool{}
	pk := pkgload.New()
	if !load(prog, entry, seen, pk, "") {
		return 1
	}
	l, d, ent, ok := compileChain(prog)
	if !ok {
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
	// 参数串放进映像尾部的预留区，起点地址写进 cmdW：@args 只会读到本程序的参数
	cmd := commandLine(entry, args[1:])
	if len(cmd) > hostSpare {
		fmt.Fprintln(os.Stderr, "参数太多，这一版放不下")
		return 1
	}
	at := len(ji.Img.Bytes)
	if !poke(base, at, cmd) || !poke64(base, ji.CmdW, uint64(base)+uint64(at)) {
		fmt.Fprintln(os.Stderr, "把参数摆进内存失败")
		return 1
	}

	// 第二个位置是这条线程要多少栈：写死 0 会让它去读 nf.exe 自己头里那个数（Go 给的是 2 MiB），
	// 同一份代码直跑就会比跑产物更早耗尽栈。语言规定由 `pe.StackReserve` 那一个数给出，产物头里
	// 写的也是它，两边都按这一个数起栈——"栈用完了"那条检查才能在两条路上落在同一个界限上。
	th, _, errno := syscall.SyscallN(procCreateThread.Addr(), 0, uintptr(pe.StackReserve), base+uintptr(ent), 0, 0, 0)
	if th == 0 {
		fmt.Fprintln(os.Stderr, "起不来线程：", errno)
		return 1
	}
	// 程序正常收尾会自己 ExitProcess，下面这两行走不到；走到即表示程序未经 ExitProcess 返回。
	syscall.SyscallN(procWaitForSingle.Addr(), uintptr(th), waitInfinite)
	fmt.Fprintln(os.Stderr, "程序没走 exit 就回来了，退出码按 1 算")
	return 1
}

// poke 往那块内存里写一段字节。绕开"把 uintptr 转回指针"这一步：
// 那块内存由操作系统掌管，Go 不知道它存在，也无法替我们照看它。
//
// en: poke writes a run of bytes into that memory block, avoiding the step of converting a
// uintptr back into a pointer: the operating system owns that memory, so Go neither knows it
// exists nor can watch over it for us.
func poke(base uintptr, at int, b []byte) bool {
	if len(b) == 0 {
		return true
	}
	var written uintptr
	r, _, _ := syscall.SyscallN(procWriteProcessMemo.Addr(), currentProcess, base+uintptr(at),
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&written)))
	return r != 0
}

// poke64 写一个位置 8 位：导入表与 cmdW 都是照这个格式读的。
//
// en: poke64 writes one 8-byte location - both the import table and cmdW are read in that
// format.
func poke64(base uintptr, at int, v uint64) bool {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return poke(base, at, b[:])
}

// fillImports 逐槽填上系统函数的真地址。同一个模块只问一次句柄，往后每一个位置只问函数名。
// 本机没有这个名字就是致命错误：发射出的代码确实要 call 它，宁可当场报清楚，也不能让控制流跳进空地址。
//
// en: fillImports writes the real address of each system function, slot by slot. A module is
// asked for its handle only once, and after that each symbol only asks for its name. A missing
// name on this machine is a fatal error: the emitted code really does call it, so better to say
// so plainly than let control flow jump into a null address.
func fillImports(base uintptr, iat []pe.IatSlot) int {
	// 用 LoadLibraryW 而不是 GetModuleHandleW：后者只识别已经加载进本进程的模块，
	// 而 winsock 在从未碰过网络的进程里恰好还没加载（这里用到的两个 DLL 都是系统自带的）。
	// 已经加载过的（kernel32）这一句只是把句柄再交回一次、引用计数加一，
	// 宿主不向操作系统归还——进程一结束这些引用就不存在了，这里不留未归还的计数。
	handles := map[string]uintptr{}
	for _, slot := range iat {
		h, asked := handles[slot.DLL]
		if !asked {
			h, _, _ = syscall.SyscallN(procLoadLibrary.Addr(),
				uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(slot.DLL))))
			handles[slot.DLL] = h
		}
		if h == 0 {
			fmt.Fprintf(os.Stderr, "本机找不到 %s\n", slot.DLL)
			return 1
		}
		name := append([]byte(slot.Name), 0)
		addr, _, _ := syscall.SyscallN(procGetProcAddress.Addr(), h, uintptr(unsafe.Pointer(&name[0])))
		if addr == 0 {
			fmt.Fprintf(os.Stderr, "本机 %s 里没有 %s 这个函数\n", slot.DLL, slot.Name)
			return 1
		}
		if !poke64(base, slot.At, uint64(addr)) {
			fmt.Fprintf(os.Stderr, "填 %s 的地址没填进去\n", slot.Name)
			return 1
		}
	}
	return 0
}

// commandLine 把"脚本名 + 参数"拼成一条 Windows 风格的宽字符命令行，末尾带一个 0。
// 拼法与 @args 那一条拆法成对：第一段是脚本名（@args 把它当程序名跳过），
// 每段都裹引号，段里的引号前加一个反斜杠，成串的反斜杠按 Windows 的规矩翻倍。
//
// StringToUTF16 每一段都自带一个收尾的 0，混在这里会被当成"命令行到此为止"，
// 所以每段都去掉那一个，只在整条的最后补上一个。
//
// en: commandLine joins "script name + arguments" into one Windows-style wide-character command
// line, terminated by a zero. The joining pairs with how @args splits it: the first segment is
// the script name (which @args skips as the program name), every segment is wrapped in
// quotes, a quote inside a segment is preceded by a backslash, and a run of backslashes is
// doubled per the Windows rules. StringToUTF16 appends its own terminating zero to every
// segment, which here would read as "end of command line", so each one is removed and only the
// very last is kept.
func commandLine(script string, program []string) []byte {
	var out []uint16
	push := func(s string) {
		w := syscall.StringToUTF16(s)
		out = append(out, w[:len(w)-1]...)
	}
	push(quoteArg(script))
	for _, a := range program {
		out = append(out, ' ')
		push(quoteArg(a))
	}
	out = append(out, 0)
	bs := make([]byte, len(out)*2)
	for i, u := range out {
		binary.LittleEndian.PutUint16(bs[i*2:], u)
	}
	return bs
}

// quoteArg 给一段参数加上 Windows 那套引号。不裹引号的话，带空格的参数会被切成两段。
//
// en: quoteArg wraps one argument in the Windows quoting. Without the quotes, an argument
// containing a space would be split into two.
func quoteArg(s string) string {
	var buf []rune
	var bs int
	flush := func(doubled bool) {
		n := bs
		if doubled {
			n *= 2
		}
		for i := 0; i < n; i++ {
			buf = append(buf, '\\')
		}
		bs = 0
	}
	buf = append(buf, '"')
	for _, r := range s {
		switch r {
		case '\\':
			bs++
		case '"':
			flush(true)
			buf = append(buf, '\\', '"')
		default:
			flush(false)
			buf = append(buf, r)
		}
	}
	flush(true) // 串尾的反斜杠翻倍，否则收尾的引号会被连带转义掉
	buf = append(buf, '"')
	return string(buf)
}

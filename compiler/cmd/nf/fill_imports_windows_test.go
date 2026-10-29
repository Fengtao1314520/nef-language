// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// fill_imports_windows_test.go 钉的是"在内存里直接跑"这一侧的回填：没有加载器替我们填导入表，
// 宿主得按每一个位置自带的模块名去问地址，问错了模块填进去的就是别家的函数。
// 网络那一层一次多出两个模块（winsock 与 winhttp），所以这一条先固定。
package main

import (
	"encoding/binary"
	"syscall"
	"testing"

	"neflang.org/nf/internal/pe"
)

func allocPage(t *testing.T) uintptr {
	t.Helper()
	// 要与宿主摆那份映像用的是同一条路：VirtualAlloc 不在 Go 的 syscall 包里，
	// 得直接向 kernel32 要。
	p, _, errno := syscall.SyscallN(procVirtualAlloc.Addr(), 0, uintptr(0x1000),
		uintptr(memCommitReserve), uintptr(pageExecuteReadWrite))
	if p == 0 {
		t.Fatalf("要不到一页内存：%v", errno)
	}
	free := kernel32.NewProc("VirtualFree")
	t.Cleanup(func() { syscall.SyscallN(free.Addr(), p, 0, 0x8000 /* MEM_RELEASE */) })
	return p
}

func readSlots(t *testing.T, base uintptr, n int) []uint64 {
	t.Helper()
	bs, ok := procReader{}.Read(base, 8*n)
	if !ok {
		t.Fatalf("读不回那 %d 格导入地址", n)
	}
	out := make([]uint64, n)
	for i := range out {
		out[i] = binary.LittleEndian.Uint64(bs[i*8:])
	}
	return out
}

// TestFillImportsAsksEachModuleOnce 用三个真模块各问一个函数：kernel32 与 ntdll 是每个进程里
// 已经加载好的，ws2_32 是这一趟里还没加载好的那一个——第三种才真正问出"用 LoadLibraryW 而不是
// GetModuleHandleW"这一处：后者对着没加载的模块返回 0，填进去就是一个位置空地址。
func TestFillImportsAsksEachModuleOnce(t *testing.T) {
	base := allocPage(t)
	slots := []pe.IatSlot{
		{At: 0, DLL: "kernel32.dll", Name: "WriteFile"},
		{At: 8, DLL: "ntdll.dll", Name: "RtlGetVersion"},
		{At: 16, DLL: "ws2_32.dll", Name: "WSAStartup"},
	}
	if code := fillImports(base, slots); code != 0 {
		t.Fatalf("导入表没填满，退出码 %d", code)
	}
	got := readSlots(t, base, len(slots))
	want := []uintptr{
		kernel32.NewProc("WriteFile").Addr(),
		syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Addr(),
		syscall.NewLazyDLL("ws2_32.dll").NewProc("WSAStartup").Addr(),
	}
	for i, w := range want {
		if got[i] == 0 {
			t.Fatalf("%s 那一格没填上地址", slots[i].DLL)
		}
		if got[i] != uint64(w) {
			t.Fatalf("%s 那一格填的是 %x，要的是 %x（问错了模块就会是这个数不对）",
				slots[i].DLL, got[i], w)
		}
	}
}

// TestFillImportsRefusesAnUnknownModule 钉的是失败那一侧：模块问不到要说清是哪一个，
// 不能让控制流跳进空地址。
func TestFillImportsRefusesAnUnknownModule(t *testing.T) {
	base := allocPage(t)
	slots := []pe.IatSlot{{At: 0, DLL: "nef_没有这一个模块.dll", Name: "WriteFile"}}
	if code := fillImports(base, slots); code != 1 {
		t.Fatalf("问一个不存在的模块本该回 1，实际回 %d", code)
	}
	if got := readSlots(t, base, 1); got[0] != 0 {
		t.Fatalf("没填成功就不该往那格里写东西，实际是 %x", got[0])
	}
}

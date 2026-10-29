// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package pe

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/x64"
)

const helloMsg = "nef 后端起来了\n"

// helloExe 手写一段机器码，走 kernel32 的 GetStdHandle／WriteFile／ExitProcess
// 打印一行再退出。它不碰 nef 的任何一层，只验三件事：指令编码器落得对、
// PE 头装得对、导入表读得对。这三件错了，上面所有的 lowering 都白写。
func helloExe(t *testing.T) []byte {
	t.Helper()

	a := x64.New()
	var patches []Patch
	mark := func(at, sec, off int) { patches = append(patches, Patch{At: at, Sec: sec, Off: off}) }

	a.Push(x64.RBP)
	a.Mov(x64.RBP, x64.RSP)
	a.AddImm(x64.RSP, -0x40)

	// h = GetStdHandle(-11)
	a.MovImm(x64.RCX, -11)
	mark(a.MovLoadRIP(x64.RAX), SecIAT, 0)
	a.CallReg(x64.RAX)

	// WriteFile(h, msg, len, NULL, NULL)
	a.Mov(x64.RCX, x64.RAX)
	mark(a.LeaRIP(x64.RDX), SecData, 0)
	a.MovImm(x64.R8, int64(len(helloMsg)))
	a.Xor(x64.R9, x64.R9)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, -0x20, x64.RAX)
	mark(a.MovLoadRIP(x64.RAX), SecIAT, 1)
	a.CallReg(x64.RAX)

	// ExitProcess(0)
	a.MovImm(x64.RCX, 0)
	mark(a.MovLoadRIP(x64.RAX), SecIAT, 2)
	a.CallReg(x64.RAX)
	a.Ret()

	code, err := a.Finish()
	if err != nil {
		t.Fatalf("代码没落出来：%v", err)
	}
	exe, err := Build(Layout{
		Code:  code,
		Data:  []byte(helloMsg),
		Entry: 0,
		Imports: []Import{
			{DLL: "kernel32.dll", Name: "GetStdHandle"},
			{DLL: "kernel32.dll", Name: "WriteFile"},
			{DLL: "kernel32.dll", Name: "ExitProcess"},
		},
		Patches: patches,
	})
	if err != nil {
		t.Fatalf("PE 没装起来：%v", err)
	}
	return exe
}

func TestSmokeHello(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smoke.exe")
	if err := os.WriteFile(path, helloExe(t), 0o755); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("产物跑不起来：%v，输出 %q", err, out)
	}
	if string(out) != helloMsg {
		t.Fatalf("输出不对：got %q, want %q", out, helloMsg)
	}
}

// twoDllImports 是一段"同时问两个模块"的导入表。ntdll 排在前面，是为了看真正的分段：
// 从前这张表只有一个模块，切不切段都一样。ntdll 与 kernel32 都是每台 Windows 的每个进程里
// 已经加载好的模块，所以这一条不靠 WSAStartup 那一类运行期前提。
var twoDllImports = []Import{
	{DLL: "ntdll.dll", Name: "RtlGetVersion"},
	{DLL: "kernel32.dll", Name: "ExitProcess"},
}

// twoDllCode 手写两处导入：第一个位置把 ntdll 那个函数的地址读进 rax（不 call——它要一个结构体
// 指针当参数，随便给会当场崩溃，而这一条要证的不是它能不能调），第二个位置照 kernel32 的
// ExitProcess(0) 退出。进程能起来就说明加载器把两个描述符里每一个槽位都填上了地址：
// 按名字导入是开机就解析的，找不到入口点整个进程起不来（0xC0000139），退出码到不了 0。
func twoDllCode(t *testing.T) ([]byte, []Patch) {
	t.Helper()

	a := x64.New()
	var patches []Patch
	a.Push(x64.RBP)
	a.Mov(x64.RBP, x64.RSP)
	a.AddImm(x64.RSP, -0x40)

	at := a.MovLoadRIP(x64.RAX)
	patches = append(patches, Patch{At: at, Sec: SecIAT, Off: 0})

	a.MovImm(x64.RCX, 0)
	at = a.MovLoadRIP(x64.RAX)
	patches = append(patches, Patch{At: at, Sec: SecIAT, Off: 1})
	a.CallReg(x64.RAX)
	a.Ret()

	code, err := a.Finish()
	if err != nil {
		t.Fatalf("代码没落出来：%v", err)
	}
	return code, patches
}

// TestTwoDllsMakeTwoDescriptors 这条测试覆盖的是多模块导入那一处形状：导入表从前只装得下一个
// DLL（`dllName` 写死、描述符只发一个），winsock 要进来就得先让"每一格带着自己的模块"这条路
// 真的走得通。判据是两条：加载器接受这份产物（实际运行一遍）、debug/pe 读出来是两个模块各一个函数。
func TestTwoDllsMakeTwoDescriptors(t *testing.T) {
	code, patches := twoDllCode(t)
	exe, err := Build(Layout{
		Code:    code,
		Data:    []byte("两个模块\n"),
		Entry:   0,
		Imports: twoDllImports,
		Patches: patches,
	})
	if err != nil {
		t.Fatalf("两个 DLL 的导入没装起来：%v", err)
	}

	path := filepath.Join(t.TempDir(), "two_dll.exe")
	if err := os.WriteFile(path, exe, 0o755); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("两个 DLL 的产物跑不起来：%v，输出 %q", err, out)
	}
	if len(out) != 0 {
		t.Fatalf("这一条不该印任何东西，实际 %q", out)
	}

	got := walkImports(t, exe)
	want := []struct {
		dll   string
		names []string
	}{
		{"ntdll.dll", []string{"RtlGetVersion"}},
		{"kernel32.dll", []string{"ExitProcess"}},
	}
	if len(got) != len(want) {
		t.Fatalf("导入表应该有几个模块就写几个描述符：要 %d，实际 %d", len(want), len(got))
	}
	for i, w := range want {
		if got[i].dll != w.dll {
			t.Fatalf("第 %d 个描述符指的是 %s，要的是 %s", i, got[i].dll, w.dll)
		}
		if strings.Join(got[i].names, ",") != strings.Join(w.names, ",") {
			t.Fatalf("%s 那一串函数是 %v，要的是 %v", w.dll, got[i].names, w.names)
		}
	}
	f, err := pe.NewFile(bytes.NewReader(exe))
	if err != nil {
		t.Fatalf("debug/pe 不认这份产物：%v", err)
	}
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("可选头没被认成 PE32+")
	}
	// 一个模块一个描述符，末尾再跟一个全零的收尾：两个模块就是 3×20 字节
	if d := oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]; d.Size != uint32((len(want)+1)*descSize) {
		t.Fatalf("导入表目录的长度是 %d 字节，两个模块该是 %d", d.Size, (len(want)+1)*descSize)
	}
}

// walkImports 照着加载器走的那条路读一遍导入表：数据目录 1 号指的是描述符数组，
// 一个模块一个，末尾跟一个全零的收尾；描述符里 +12 指的是模块名，+0 指的是
// "一格一个函数名"的那串 RVA（每格前面两字节是 hint）。
//
// 这一层的对错只能在字节上判定，而 Go 自带的 debug/pe 不导出导入表（它没有这个字段），
// 所以这里自己走一遍。
func walkImports(t *testing.T, exe []byte) []struct {
	dll   string
	names []string
} {
	t.Helper()
	type entry = struct {
		dll   string
		names []string
	}
	f, err := pe.NewFile(bytes.NewReader(exe))
	if err != nil {
		t.Fatalf("debug/pe 不认这份产物：%v", err)
	}
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("可选头没被认成 PE32+")
	}
	dir := oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
	if dir.VirtualAddress == 0 || dir.Size == 0 {
		t.Fatal("导入表目录没指东西")
	}
	rva := func(at uint32) int {
		for _, s := range f.Sections {
			h := s.SectionHeader
			if at >= h.VirtualAddress && at-h.VirtualAddress < h.Size {
				return int(h.Offset) + int(at-h.VirtualAddress)
			}
		}
		t.Fatalf("RVA %x 落在哪个段里没找到", at)
		return -1
	}
	z := func(at int) string {
		end := bytes.IndexByte(exe[at:], 0)
		if end < 0 {
			t.Fatalf("%d 往后读不到结尾的 0", at)
		}
		return string(exe[at : at+end])
	}

	var out []entry
	for i := 0; i*descSize < int(dir.Size); i++ {
		base := rva(dir.VirtualAddress) + i*descSize
		ipt := binary.LittleEndian.Uint32(exe[base:])
		nameAt := binary.LittleEndian.Uint32(exe[base+12:])
		if ipt == 0 && nameAt == 0 {
			break // 全零的收尾
		}
		e := entry{dll: z(rva(nameAt))}
		for k := 0; ; k++ {
			thunk := binary.LittleEndian.Uint32(exe[rva(ipt)+k*thunkSize:])
			if thunk == 0 {
				break
			}
			e.names = append(e.names, z(rva(thunk)+2)) // 两字节 hint 之后才是名字
		}
		out = append(out, e)
	}
	return out
}

// TestJitSlotsCarryTheirModule 这条测试覆盖的是"在内存里直接跑"那一条路：没有加载器替我们回填，
// 宿主（cmd/nf 的 fillImports）要靠每一个位置自带的模块名去问该问的那个 DLL。
func TestJitSlotsCarryTheirModule(t *testing.T) {
	code, patches := twoDllCode(t)
	img, err := Jit(Layout{Code: code, Data: []byte("两个模块\n"), Entry: 0, Imports: twoDllImports, Patches: patches})
	if err != nil {
		t.Fatalf("两个 DLL 的映像摆不出来：%v", err)
	}
	if len(img.Iat) != 2 {
		t.Fatalf("导入表应该有 %d 格，实际 %d", len(twoDllImports), len(img.Iat))
	}
	for i, want := range twoDllImports {
		if img.Iat[i].DLL != want.DLL || img.Iat[i].Name != want.Name {
			t.Fatalf("第 %d 格是 %s!%s，要的是 %s!%s", i, img.Iat[i].DLL, img.Iat[i].Name, want.DLL, want.Name)
		}
	}
}

// layoutWith 拼一份最小的能装出来的布局：代码就一个 ret，导入表照给的写。
func layoutWith(imports ...Import) Layout {
	return Layout{Code: []byte{0xC3}, Entry: 0, Imports: imports}
}

// TestSameDllSplitIntoTwoRunsIsRefused 这条测试覆盖的是"同一个 DLL 的名字要连着排"那一条。
// 中间夹进另一个模块的名字会切出两个指向同一个模块的描述符，加载器不会替我们合并，
// 于是第二段的函数一个也进不了产物——这种错要在生成导入表时就报出来，不能等运行期。
func TestSameDllSplitIntoTwoRunsIsRefused(t *testing.T) {
	const want = "导入表里 kernel32.dll 的名字分成两段排了：同一个 DLL 的要连着排"
	split := layoutWith(
		Import{DLL: "kernel32.dll", Name: "WriteFile"},
		Import{DLL: "ntdll.dll", Name: "NtCurrentTeb"},
		Import{DLL: "kernel32.dll", Name: "ExitProcess"},
	)
	if _, err := Build(split); err == nil || err.Error() != want {
		t.Fatalf("Build 交回的是 %v，要的是整句 %q", err, want)
	}
	// 直跑那一路同样检查这条约束：同一份代码不该能运行却产不出文件
	if _, err := Jit(split); err == nil || err.Error() != want {
		t.Fatalf("Jit 交回的是 %v，要的是整句 %q", err, want)
	}
}

// TestImportWithoutModuleIsRefused 这条测试覆盖的是另一头：漏写模块名不能被默认成 kernel32。
func TestImportWithoutModuleIsRefused(t *testing.T) {
	const want = "导入表里 WriteFile 没写它属于哪一个 DLL"
	_, err := Build(layoutWith(Import{Name: "WriteFile"}))
	if err == nil || err.Error() != want {
		t.Fatalf("Build 交回的是 %v，要的是整句 %q", err, want)
	}
}

// TestPatchOutsideTheImportTableIsRefused 这条测试覆盖的是导入表下标的越界校验：
// 名字没登记时 importSlot 返回 -1，从前 -1 会被当成"导入表前面一格"继续算下去，回填出来的地址无效。
func TestPatchOutsideTheImportTableIsRefused(t *testing.T) {
	const want = "代码要引导入表第 3 格，这张表一共只有 2 格"
	lay := layoutWith(Import{DLL: "kernel32.dll", Name: "WriteFile"}, Import{DLL: "kernel32.dll", Name: "ExitProcess"})
	lay.Code = []byte{0xC3, 0, 0, 0, 0}
	lay.Patches = []Patch{{At: 1, Sec: SecIAT, Off: 3}}
	if _, err := Build(lay); err == nil || err.Error() != want {
		t.Fatalf("Build 交回的是 %v，要的是整句 %q", err, want)
	}
	if _, err := Jit(lay); err == nil || err.Error() != want {
		t.Fatalf("Jit 交回的是 %v，要的是整句 %q", err, want)
	}
}

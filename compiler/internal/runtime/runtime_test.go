// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package runtime_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"neflang.org/nf/internal/exetest"
	"neflang.org/nf/internal/pe"
	"neflang.org/nf/internal/runtime"
	"neflang.org/nf/internal/x64"
)

// 这些测试不经过发射器与降级：手工构造一段调用运行期例程入口的代码，封成 PE 实际运行一遍。
// 运行期例程是后续所有东西的落脚点，必须先能独立自证。
type drv struct {
	a     *x64.Asm
	rt    runtime.Blob
	lbl   map[string]x64.Label
	next  x64.Label
	data  []byte
	patch []pe.Patch
}

func newDrv(t *testing.T) *drv {
	t.Helper()
	d := &drv{a: x64.New(), rt: runtime.Build(), lbl: map[string]x64.Label{}, next: 1}
	for _, p := range d.rt.Parts {
		d.lbl[p.Name] = d.next
		d.next++
	}
	d.a.Push(x64.RBP)
	d.a.Mov(x64.RBP, x64.RSP)
	d.a.AddImm(x64.RSP, -0x100)
	// 这一份驱动代码自己就是进程入口，真实那条路在这儿先要这一条线程的状态块（R12 是它的起点），
	// 不然抛出、打印与栈底那一批位置读的都是没备好的内存。
	d.call("@state_init")
	return d
}

func (d *drv) label() x64.Label {
	l := d.next
	d.next++
	return l
}

func (d *drv) call(name string) { d.a.Call(d.lbl[name]) }

// addString 按「字节数 8 + 码点数 8 + UTF-8 字节」的文本形状收一条文本。
func (d *drv) addString(s string) int {
	off := len(d.data)
	var hdr [16]byte
	binary.LittleEndian.PutUint64(hdr[0:], uint64(len(s)))
	binary.LittleEndian.PutUint64(hdr[8:], uint64(utf8.RuneCountInString(s)))
	d.data = append(d.data, hdr[:]...)
	d.data = append(d.data, s...)
	return off
}

func (d *drv) lea(off int, reg int) {
	at := d.a.LeaRIP(reg)
	d.patch = append(d.patch, pe.Patch{At: at, Sec: pe.SecData, Off: off})
}

// staticAddr 把运行期静态数据里那一个位置的地址取到 reg。测试要读写的是运行期自己那一批
// （wsadata 与 wsaOnce 那两块都在静态段，不在每一条线程的状态块里），所以地址算的是
// 驱动自己那段数据之后、再加运行期数据里的那个偏移。
func (d *drv) staticAddr(t *testing.T, name string, reg int) {
	t.Helper()
	off, ok := d.rt.DataAt[name]
	if !ok {
		t.Fatalf("静态数据里没有 %s", name)
	}
	d.lea(len(d.data)+off, reg)
}

func (d *drv) putInt(v int64) {
	d.a.MovImm(x64.RCX, v)
	d.call("@put_int")
}

func (d *drv) putStr(s string) {
	d.lea(d.addString(s), x64.RCX)
	d.call("@put_str")
}

func (d *drv) putChar(cp int64) {
	d.a.MovImm(x64.RCX, cp)
	d.call("@put_char")
}

func (d *drv) putBool(v bool) {
	n := int64(0)
	if v {
		n = 1
	}
	d.a.MovImm(x64.RCX, n)
	d.call("@put_bool")
}

func (d *drv) nl()    { d.call("@put_nl") }
func (d *drv) space() { d.call("@put_space") }

// printOutLen 打印这一条线程的行缓冲里已经攒了几个字节（状态块里那个位置，不另起一行）。
// 攒行那一条路要看得见长度才能钉住"没到换行就不写出去"。
func (d *drv) printOutLen() {
	d.a.MovLoad(x64.RCX, x64.R12, runtime.StateAt("outLen"))
	d.call("@put_int")
}

// alloc 要 @alloc：字节数与类型字进去，返回的地址在 rax。
func (d *drv) alloc(size, tag int64) {
	d.a.MovImm(x64.RCX, size)
	d.a.MovImm(x64.RDX, tag)
	d.call("@alloc")
}

// save 把 rax 存进驱动帧的第 n 个局部槽，之后 call 别的例程也不会丢失。
func (d *drv) save(n int) { d.a.MovStore(x64.RBP, int32(-8*(n+1)), x64.RAX) }

// store 往 [obj(n)+disp] 写一个 8 字节值，obj(n) 是第 n 个槽里的地址。
func (d *drv) store(n int, disp int32, v int64) {
	d.a.MovLoad(x64.RSI, x64.RBP, int32(-8*(n+1)))
	d.a.MovImm(x64.RDX, v)
	d.a.MovStore(x64.RSI, disp, x64.RDX)
}

func (d *drv) store8(n int, disp int32, v int64) {
	d.a.MovLoad(x64.RSI, x64.RBP, int32(-8*(n+1)))
	d.a.MovImm(x64.RDX, v)
	d.a.MovStore8(x64.RSI, disp, x64.RDX)
}

func (d *drv) loadAndPrint(n int, disp int32) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(n+1)))
	d.a.MovLoad(x64.RCX, x64.RAX, disp)
	d.call("@put_int")
	d.nl()
}

// load8AndPrint 按一个字节读回，验证 store8 的按字节存储。
func (d *drv) load8AndPrint(n int, disp int32) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(n+1)))
	d.a.MovZX8(x64.RCX, x64.RAX, disp)
	d.call("@put_int")
	d.nl()
}

// headerAndPrint 读对象前面那 8 字节的块头：disp 是 -8 取类型字，-4 取尺寸。
func (d *drv) headerAndPrint(n int, disp int32) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(n+1)))
	d.a.MovZX32(x64.RCX, x64.RAX, disp)
	d.call("@put_int")
	d.nl()
}

// free 释放第 n 个槽里的那一块（@free 收的是数据指针，槽里存放的正是它）。
func (d *drv) free(n int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(n+1)))
	d.call("@free")
}

// addrOf 把第 n 个槽里的地址取到 rax（紧接着用 sameAndPrint 比较）。
// 取回的地址与 `save` 存入的是同一号动作的两个方向，槽里存放的就是那块数据指针。
func (d *drv) addrOf(n int) { d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(n+1))) }

// sameAndPrint 比较"手上那个地址"（rax）与第 n 个槽里的地址是否同一个，输出 1 或 0。
//
// 地址本身每次都变（VirtualAlloc 给出的位置不固定），所以这里比较的是相等与否而不是绝对值——
// "释放复用"要验证的就是这个：释放回去的块必须能被下一次分配原样取回。
// 先 xor 清零再 cmp 最后 setcc：这个顺序错一次就会把刚要检查的标志位冲掉。
func (d *drv) sameAndPrint(n int) {
	d.a.MovLoad(x64.RDX, x64.RBP, int32(-8*(n+1)))
	d.a.Xor(x64.RCX, x64.RCX)
	d.a.Cmp(x64.RAX, x64.RDX)
	d.a.Setcc(x64.E, x64.RCX)
	d.a.MovZX8Reg(x64.RCX)
	d.call("@put_int")
	d.nl()
}

// ---- list 那四个例程的小包装：句柄一律存放在驱动帧的第 n 个局部槽里 ----

func (d *drv) listNew(n, tag int64) {
	d.a.MovImm(x64.RCX, n)
	d.a.MovImm(x64.RDX, tag)
	d.call("@list_new")
}

func (d *drv) listAdd(hn int, v int64) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovImm(x64.RDX, v)
	d.call("@list_add")
}

// listRemove 删除一个元素。r8 是"如何判定相等"的模式，与降级侧 `keyMode` 同一条规则：
// 0 比较 64 位数值，1 比较文本内容。这个参数必须显式给出——沿用上一个例程的残值，
// 走哪条路就不确定（@list_remove 早前判反方向时，这条测试也是靠 r8 的残值才碰对上的）。
func (d *drv) listRemove(hn int, v int64, mode int64) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovImm(x64.RDX, v)
	d.a.MovImm(x64.R8, mode)
	d.call("@list_remove")
}

// listAt 取那个位置的地址，存入第 on 个局部槽——读、写、交出在中间码里都从这一个地址走。
func (d *drv) listAt(hn int, idx int64, on int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovImm(x64.RDX, idx)
	d.call("@list_at")
	d.a.MovStore(x64.RBP, int32(-8*(on+1)), x64.RAX)
}

func (d *drv) cellPrint(on int) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(on+1)))
	d.a.MovLoad(x64.RCX, x64.RAX, 0)
	d.call("@put_int")
	d.nl()
}

func (d *drv) cellSet(on int, v int64) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(on+1)))
	d.a.MovImm(x64.RDX, v)
	d.a.MovStore(x64.RAX, 0, x64.RDX)
}

// listLen 读容器的第一个槽：length(a) 在中间码里就是这一个 Load，不另设例程。
func (d *drv) listLen(hn int) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovLoad(x64.RCX, x64.RAX, 0)
	d.call("@put_int")
	d.nl()
}

// finish 追加运行期那整块、翻译它内部的待回填点，然后装配成 PE。
func (d *drv) finish(t *testing.T, exitCode int64) []byte {
	t.Helper()
	d.a.MovImm(x64.RCX, exitCode)
	d.call("@exit")

	base := d.a.PC()
	for i, p := range d.rt.Parts {
		d.a.Mark(d.lbl[p.Name])
		end := len(d.rt.Code)
		if i+1 < len(d.rt.Parts) {
			end = d.rt.Parts[i+1].Off
		}
		d.a.Append(d.rt.Code[p.Off:end])
	}
	for _, p := range d.rt.Parts {
		for _, r := range p.Refs {
			patch := pe.Patch{At: base + r.At}
			switch r.Kind {
			case runtime.RefData:
				patch.Sec = pe.SecData
				patch.Off = len(d.data) + d.rt.DataAt[r.Name]
			case runtime.RefIAT:
				patch.Sec = pe.SecIAT
				patch.Off = r.Off
			default:
				t.Fatalf("未知的引用种类 %d", r.Kind)
			}
			d.patch = append(d.patch, patch)
		}
	}
	code, err := d.a.Finish()
	if err != nil {
		t.Fatalf("驱动代码没落出来：%v", err)
	}
	data := append(append([]byte{}, d.data...), d.rt.Data...)
	imports := make([]pe.Import, 0, len(d.rt.Imports))
	for _, imp := range d.rt.Imports {
		imports = append(imports, pe.Import{DLL: imp.DLL, Name: imp.Name})
	}
	exe, err := pe.Build(pe.Layout{
		Code:    code,
		Data:    data,
		Entry:   0,
		Imports: imports,
		Patches: d.patch,
	})
	if err != nil {
		t.Fatalf("PE 没装起来：%v", err)
	}
	return exe
}

// run 实际运行产物，返回标准输出与退出码。装配与运行这段是共用的（internal/exetest）。
func run(t *testing.T, exe []byte) (string, int) {
	t.Helper()
	r := exetest.Run(t, exe, "rt.exe")
	return r.Stdout, r.Code
}

func TestPutInt(t *testing.T) {
	vals := []int64{0, 1, 9, 10, 42, -7, 999, 1000, math.MaxInt64, math.MinInt64, -1, -1000000}
	d := newDrv(t)
	for _, v := range vals {
		d.putInt(v)
		d.nl()
	}
	got, _ := run(t, d.finish(t, 0))
	var want bytes.Buffer
	for _, v := range vals {
		want.WriteString(strconv.FormatInt(v, 10))
		want.WriteString("\n")
	}
	if got != want.String() {
		t.Fatalf("印出来的整数不对：\n got %q\nwant %q", got, want.String())
	}
}

func TestPutStrAndChar(t *testing.T) {
	d := newDrv(t)
	d.putStr("书包 abc")
	d.nl()
	for _, r := range []rune{'a', 0x7F, 0x80, '¢', 0x7FF, '中', 0x800, 0xFFFF, 0x10000, '𝓪'} {
		d.putChar(int64(r))
	}
	d.nl()
	d.putStr("")
	d.nl()
	got, _ := run(t, d.finish(t, 0))
	var want bytes.Buffer
	want.WriteString("书包 abc\n")
	for _, r := range []rune{'a', 0x7F, 0x80, '¢', 0x7FF, '中', 0x800, 0xFFFF, 0x10000, '𝓪'} {
		want.WriteRune(r)
	}
	want.WriteString("\n\n")
	if got != want.String() {
		t.Fatalf("文本与码点不对：\n got %q\nwant %q", got, want.String())
	}
}

func TestPutBoolSpace(t *testing.T) {
	d := newDrv(t)
	d.putBool(true)
	d.space()
	d.putBool(false)
	d.nl()
	got, _ := run(t, d.finish(t, 0))
	if got != "true false\n" {
		t.Fatalf("布尔与空格不对：%q", got)
	}
}

// @throw 走的是"未被捕获"那条路：输出行、退出码 1。
// try／catch 的帧链还没实现，所以抛出一律落到这里。
func TestThrow(t *testing.T) {
	d := newDrv(t)
	d.lea(d.addString("下标越界"), x64.RDX)
	d.a.MovImm(x64.RCX, 10031)
	d.call("@throw")
	got, code := run(t, d.finish(t, 0))
	if got != "出错了（10031）：下标越界\n" || code != 1 {
		t.Fatalf("抛出的形状不对：out=%q code=%d", got, code)
	}
}

func TestExitCode(t *testing.T) {
	d := newDrv(t)
	d.putInt(7)
	_, code := run(t, d.finish(t, 42))
	if code != 42 {
		t.Fatalf("退出码是 %d，要 42", code)
	}
}

// TestStdoutHandleCached 验证 @write 里的句柄缓存：连续写三次都要完整输出，
// 缓存没生效或者被写坏，输出就会缺失后面几段。
func TestStdoutHandleCached(t *testing.T) {
	d := newDrv(t)
	for i := int64(1); i <= 3; i++ {
		d.putInt(i)
		d.putStr("号")
		d.nl()
	}
	got, _ := run(t, d.finish(t, 0))
	if got != "1号\n2号\n3号\n" {
		t.Fatalf("连写三次不对：%q", got)
	}
}

// TestAlloc 验证分配那一套：切一块、写进去、隔着别的例程读回、块头记录的类型字与尺寸对得上。
// 三笔分别覆盖三条路径：第一笔撑开空堆（必然走 VirtualAlloc），第二笔在同一块里继续切，
// 第三笔比一整块还大——换块之后前两笔的内容必须还在。
//
// 10091 号的失败分支这里不覆盖：它要求 VirtualAlloc 真的一次都给不出来。
func TestAlloc(t *testing.T) {
	d := newDrv(t)
	d.alloc(24, 7)
	d.save(0)
	d.alloc(40, 8)
	d.save(1)
	d.alloc(0x200000, 9)
	d.save(2)

	d.store(0, 0, 111)
	d.store(0, 8, 222)
	d.store(0, 16, 333)
	d.store(1, 0, 444)
	d.store(1, 32, 555)
	d.store(2, 0, 666)
	d.store8(2, 0x1FFFFF, 200)

	d.loadAndPrint(0, 0)
	d.loadAndPrint(0, 8)
	d.loadAndPrint(0, 16)
	d.loadAndPrint(1, 0)
	d.loadAndPrint(1, 32)
	d.loadAndPrint(2, 0)
	d.load8AndPrint(2, 0x1FFFFF)
	d.headerAndPrint(0, -8) // 类型字
	d.headerAndPrint(0, -4) // 尺寸
	d.headerAndPrint(1, -4)
	d.headerAndPrint(2, -8)
	d.headerAndPrint(2, -4)

	got, code := run(t, d.finish(t, 0))
	want := "111\n222\n333\n444\n555\n666\n200\n7\n24\n40\n9\n2097152\n"
	if got != want {
		t.Fatalf("堆上读写不对：\n got %q\nwant %q\n码 %d", got, want, code)
	}
}

// TestAllocOutOfMemory 验证 10091 号的失败分支：一次请求 2^62 字节，VirtualAlloc 必然给不出，
// 当场就该按"未被捕获的异常"那个形状收场——输出行、退出码 1。
//
// 选这个数值是为了不依赖本机条件：它远大于用户地址空间上界（128 TiB），
// 所以不靠内存紧张、也不靠分页文件多大，任何一台机器上这都是一次必然失败的请求。
func TestAllocOutOfMemory(t *testing.T) {
	d := newDrv(t)
	d.alloc(int64(1)<<62, 1)
	d.save(0)
	got, code := run(t, d.finish(t, 0))
	if got != "出错了（10091）：向操作系统要不到内存\n" || code != 1 {
		t.Fatalf("要不到内存的形状不对：out=%q code=%d", got, code)
	}
}

// TestFreeAboveTopBandKeepsHandlerChain 钉这一处：比最大那一档还大的块还回堆里之后，
// 抛出那一路仍然走得通。
//
// 档号原先会排到 freeHeads 那一张之外（那张只有 73 格），而紧挨在它后面的静态格子就是
// 处理器链的头（那时候它还在静态段里）：还一块 600 KiB 的文本就把链的头改写成那一个块地址，下一次没人接住的抛出
// 照着这个头去跳，进程被操作系统终止、屏幕上一个字也不输出（2026-11-07 实测）。
// 抛出那一句本身用 10031 号，与堆无关，只看它能不能正常打成那一行、给退出码 1。
//
// en: TestFreeAboveTopBandKeepsHandlerChain pins this: after a block larger than the biggest
// band is returned to the heap, a throw still works. The band number used to advance past the
// freeHeads table, which has 73 slots, and the static cell right behind that table is the head
// of the handler chain - returning a 600 KiB text block therefore rewrote the chain head with
// that block's address, and the next uncaught throw jumped to it: the process ended with no
// output at all (measured 2026-11-07). The throw itself uses code 10031, unrelated to the heap,
// so this only asks whether that line gets printed with exit code 1.
func TestFreeAboveTopBandKeepsHandlerChain(t *testing.T) {
	d := newDrv(t)
	d.alloc(600*1024, 6)
	d.save(0)
	d.free(0)
	d.lea(d.addString("下标越界"), x64.RDX)
	d.a.MovImm(x64.RCX, 10031)
	d.call("@throw")
	got, code := run(t, d.finish(t, 0))
	if got != "出错了（10031）：下标越界\n" || code != 1 {
		t.Fatalf("还回大块的文本之后抛出不对：out=%q code=%d", got, code)
	}
}

// TestAllocAboveHeaderSizeReportsOutOfMemory 钉进门那一判：块头记尺寸的那个位置只有四个字节，
// 大过 4294967295 字节的这一笔记下来就是另一个数，往下每一步（挂链、拆两半、抹零）都按那个
// 错的数走。这样的这一笔与 VirtualAlloc 给不出走同一个出口：报 10091，不依赖本机内存多大。
//
// en: TestAllocAboveHeaderSizeReportsOutOfMemory pins the check at the door: the header's size
// field is four bytes, so a request above 4294967295 bytes is stored as some other number and
// every step below it (chaining, splitting, wiping) follows that wrong figure. Such a request
// leaves through the same exit as one VirtualAlloc cannot serve: code 10091, independent of how
// much memory the machine has.
func TestAllocAboveHeaderSizeReportsOutOfMemory(t *testing.T) {
	d := newDrv(t)
	d.alloc(4294967296, 6)
	d.save(0)
	got, code := run(t, d.finish(t, 0))
	if got != "出错了（10091）：向操作系统要不到内存\n" || code != 1 {
		t.Fatalf("记不进块头那个位置的这一笔不对：out=%q code=%d", got, code)
	}
}

// TestStateBlockPerThreadIsolation 钉这一段的核心：抛出、打印方向与栈底那一批位置现在住在
// 每一条线程自己那一块里，两块之间互不影响。
//
// 今天程序只有一条线程，所以这里用同一份驱动代码连叫两次 @state_init 当两条线程各备一份：
// 两次拿到的地址不一样（堆给的两块）；往手上那一份摆一个数，换回第一份读回来还是第一份自己的，
// 反过来第二份读到的是开局那一个 0。段 6 真起第二条线程之后，这一条钉的就是第二条线程不该
// 看见第一条线程那一份。这里直接读写块里的格子，不叫抛出那一路：那一路的测试已经有了。
//
// en: TestStateBlockPerThreadIsolation pins the core of this segment: the throw cells, the print
// direction and the stack floor now live in each thread's own block, and the two blocks do not
// reach each other. The program has one thread today, so this driver asks @state_init twice, the
// way two threads would each prepare theirs: the two addresses differ; a number written into the
// block on hand still reads back as that first block's own value when R12 goes back to it, while
// the second block reads the zero it started with. Once segment 6 starts a second thread, this is
// what that thread must not see. The test writes and reads the cells directly rather than going
// through a throw - the tests for that path already exist.
func TestStateBlockPerThreadIsolation(t *testing.T) {
	d := newDrv(t)
	d.save(0) // newDrv 开头那一次 @state_init 的返回值：第一块的起点
	d.a.MovImm(x64.RCX, 7)
	d.a.MovStore(x64.R12, runtime.StThrowCode, x64.RCX) // 往第一块里摆一个码
	d.call("@state_init")
	d.save(1)
	d.sameAndPrint(0) // 1 = 两次拿到的是同一块，那就不叫每一条线程一份
	d.a.MovLoad(x64.RCX, x64.R12, runtime.StThrowCode)
	d.call("@put_int") // 第二块是干净的：0
	d.nl()
	d.addrOf(0)
	d.a.Mov(x64.R12, x64.RAX) // 把 R12 换回第一块
	d.a.MovLoad(x64.RCX, x64.R12, runtime.StThrowCode)
	d.call("@put_int") // 第一块里那一个 7 还在
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if got != "0\n0\n7\n" || code != 0 {
		t.Fatalf("两份状态块没隔开：\n got %q 码 %d\nwant \"0\\n0\\n7\\n\"", got, code)
	}
}

// TestStateBlockLayout 钉那张清单与 st* 那一组号一格格对上，块的大小进到 8 的倍数。
// 清单是运行期与发射那一步共同的依据：号错了，抛出那一路读写的是别人的格子，
// 而错的方式取决于排在前面的格子多大，编译与跑都不响。
//
// en: TestStateBlockLayout pins the list against the st* numbers, cell by cell, and the block's
// size to a multiple of 8. The list is what both the runtime and the emit step read, so a wrong
// number makes the throw path touch someone else's cell - and how it goes wrong depends on the
// sizes ahead of it, which neither compiling nor running would notice.
func TestStateBlockLayout(t *testing.T) {
	cases := []struct {
		off  int32
		name string
	}{
		{runtime.StHTop, "hTop"},
		{runtime.StThrowCode, "throwCode"},
		{runtime.StThrowMsg, "throwMsg"},
		{runtime.StThrowAt, "throwAt"},
		{runtime.StOutErr, "outErr"},
		{runtime.StTraceRows, "traceRows"},
		{runtime.StTraceRowsN, "traceRowsN"},
		{runtime.StTraceRowsCap, "traceRowsCap"},
		{runtime.StTraceAt, "traceAt"},
		{runtime.StTraceRbp, "traceRbp"},
		{runtime.StTraceBusy, "traceBusy"},
		{runtime.StStackFloor, "stackFloor"},
		{runtime.StDefaultHandler, "defaultHandler"},
		{runtime.StReleasing, "releasing"},
		{runtime.StThreadError, "threadError"},
		{runtime.StSelfEntry, "selfEntry"},
	}
	for _, c := range cases {
		if got := runtime.StateAt(c.name); got != c.off {
			t.Errorf("%s 的号对不上：清单算出 %d，常量写着 %d", c.name, got, c.off)
		}
	}
	if runtime.StateBytes()%8 != 0 {
		t.Errorf("状态块的大小没进到 8 的倍数：%d", runtime.StateBytes())
	}
}

// TestFreeReusesBlock 验证"释放复用"那三条硬性要求：释放回去的块必须能被下一次同档分配
// 原样取回、取回时数据区是清零的（只涨不释放时这份干净是免费的，有了复用就必须自己抹）、
// 释放第二次是空操作而不是把链表弄断。
//
// 最后一条是静态形状的：块头尺寸写成 0 的那一块，释放它必须什么都不做。静态文本池里每条
// 字面量前面挂的正是这种全 0 的占位头——少这一道，将来析构一个指向字面量的名字就会去拆堆的记录表。
func TestFreeReusesBlock(t *testing.T) {
	d := newDrv(t)
	d.alloc(24, 7)
	d.save(0)
	d.store(0, 0, 111)
	d.loadAndPrint(0, 0)    // 111
	d.headerAndPrint(0, -4) // 24：块头记录的是数据尺寸
	d.free(0)

	d.alloc(24, 9)
	d.save(1)
	d.sameAndPrint(0)       // 1：同一块被下一次分配原样取回
	d.loadAndPrint(1, 0)    // 0：返回之前已清零
	d.headerAndPrint(1, -8) // 9：类型字跟着这一次走
	d.headerAndPrint(1, -4) // 24：尺寸槽不动

	// 释放三次也只挂一份：块头里的记号能识别"已在链上"
	d.free(1)
	d.free(1)
	d.free(1)
	d.alloc(24, 1)
	d.save(2)
	d.sameAndPrint(1) // 1：取回的仍是同一块
	d.alloc(24, 2)
	d.save(3)
	d.sameAndPrint(2) // 0：链空了，这一笔是新切的一块

	d.store(2, -4, 0) // 尺寸槽写 0：视其为不在堆上
	d.free(2)
	d.alloc(24, 3)
	d.save(4)
	d.sameAndPrint(2) // 0：没进链，所以取回的是别处的一块

	got, code := run(t, d.finish(t, 0))
	want := "111\n24\n1\n0\n9\n24\n1\n0\n0\n"
	if got != want {
		t.Fatalf("还自由不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFreeListGrow 验证容器扩缩那一步确实把旧数据块归还了：撑破容量搬运完成后，
// 旧块的地址必须原样出现在下一次同档的分配里。
//
// 选 minCap 那一档翻一次是最小的触发路径；五格数据必须都还在——归还旧块与搬运数据是同一段
// 代码的两端，一端出错另一端就暴露出来。
func TestFreeListGrow(t *testing.T) {
	d := newDrv(t)
	d.listNew(0, 7)
	d.save(0) // 容器对象
	d.addrOf(0)
	d.a.MovLoad(x64.RAX, x64.RAX, runtime.CellData)
	d.save(1) // 老的那一段数据块

	for _, v := range []int64{11, 22, 33, 44, 55} {
		d.listAdd(0, v) // 第五个撑破 minCap=4，翻倍搬一次
	}
	d.listLen(0)
	for i := int64(0); i < 5; i++ {
		d.listAt(0, i, 2)
		d.cellPrint(2)
	}

	d.alloc(4*8, 0) // 与老块同档的一笔
	d.save(3)
	d.sameAndPrint(1) // 1：老数据块回到链上了

	got, code := run(t, d.finish(t, 0))
	want := "5\n11\n22\n33\n44\n55\n1\n"
	if got != want {
		t.Fatalf("容器扩缩还旧块不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestList 走一遍 list 的四个例程：创建空容器、添加、按下标读写、
// 超过容量之后前面几样必须还在（翻倍搬运最容易丢东西）、删除中间一个把后面整体前移。
// 元素一律用整数：这四个例程只搬运 8 字节，不识别里面装的是什么。
func TestList(t *testing.T) {
	d := newDrv(t)
	d.listNew(0, 7)
	d.save(0)
	for _, v := range []int64{11, 22, 33} {
		d.listAdd(0, v)
	}
	d.listLen(0)
	d.listAt(0, 1, 1)
	d.cellPrint(1)
	d.cellSet(1, 99)
	d.listAt(0, 1, 1)
	d.cellPrint(1)
	for _, v := range []int64{44, 55, 66, 77} { // 第 5 次添要把容量从 4 翻到 8
		d.listAdd(0, v)
	}
	d.listLen(0)
	d.listAt(0, 0, 1)
	d.cellPrint(1)
	d.listAt(0, 6, 1)
	d.cellPrint(1)
	d.listRemove(0, 99, 0) // 装的是数值：比较那 64 位
	d.listLen(0)
	d.listAt(0, 1, 1)
	d.cellPrint(1)
	d.listAt(0, 5, 1)
	d.cellPrint(1)

	got, code := run(t, d.finish(t, 0))
	want := "3\n22\n99\n7\n11\n77\n6\n33\n77\n"
	if got != want {
		t.Fatalf("list 不对：\n got %q\nwant %q\n码 %d", got, want, code)
	}
}

// TestListIndexThrows 验证 10031 号：超出长度与负下标都算越界，都是当场抛出。
func TestListIndexThrows(t *testing.T) {
	for _, idx := range []int64{3, 99, -1} {
		d := newDrv(t)
		d.listNew(0, 7)
		d.save(0)
		for _, v := range []int64{5, 6, 7} {
			d.listAdd(0, v)
		}
		d.listAt(0, idx, 1)
		got, code := run(t, d.finish(t, 0))
		if got != "出错了（10031）：下标越界\n" || code != 1 {
			t.Fatalf("下标 %d：out=%q code=%d", idx, got, code)
		}
	}
}

// ---- dict／hashset 的小包装：表句柄存放在第 n 个局部槽里 ----

func (d *drv) mapNew(n, tag, mode int64) {
	d.a.MovImm(x64.RCX, n)
	d.a.MovImm(x64.RDX, tag)
	d.a.MovImm(x64.R8, mode)
	d.call("@map_new")
}

// mapKey 把键放进 rdx：off 不是 -1 时键是一段文本对象的地址偏移，否则就是数值本身。
func (d *drv) mapKey(key int64, off int) {
	if off >= 0 {
		d.lea(off, x64.RDX)
		return
	}
	d.a.MovImm(x64.RDX, key)
}

func (d *drv) mapPut(hn int, key int64, off int, val int64) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.mapKey(key, off)
	d.a.MovImm(x64.R8, val)
	d.call("@map_put")
}

// mapVal 查询一个键并把它的值输出：@map_at 返回的是值所在位置的地址。
func (d *drv) mapVal(hn int, key int64, off int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.mapKey(key, off)
	d.call("@map_at")
	d.a.MovLoad(x64.RCX, x64.RAX, 0)
	d.call("@put_int")
	d.nl()
}

func (d *drv) mapContains(hn int, key int64, off int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.mapKey(key, off)
	d.call("@map_contains")
	d.call("@put_bool")
	d.nl()
}

func (d *drv) mapRemove(hn int, key int64, off int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.mapKey(key, off)
	d.call("@map_remove")
}

// mapRemoved 删除一个键并把返回的值输出：返回被删那一条的值，没删成时是 0。
func (d *drv) mapRemoved(hn int, key int64, off int) {
	d.mapRemove(hn, key, off)
	d.a.Mov(x64.RCX, x64.RAX)
	d.call("@put_int")
	d.nl()
}

func (d *drv) mapLen(hn int) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovLoad(x64.RCX, x64.RAX, 0)
	d.call("@put_int")
	d.nl()
}

// TestDictIntKeys 走一遍整数键：添加、查询、修改、删除、判断存在，外加一个查不到的键。

// TestFreeListKeepsUndersized 验证的规矩：档按 2 的幂上界划分，同档里的块可能比这一笔小。
// 数据 1120 与 1920 同在上界 2048 那一档：@alloc 若只看档、不看块自己块头里记的尺寸，
// 就会把小的那一块交给要大的一笔的人——尾巴写进隔壁块，再散列搬桶时读到一个
// state=1、键是 106 的位置，@hash 把 106 当指针解，整条程序段错误。
// **本测试的数值取在高段**：低段按 8 字节精确分档，每一档只剩一种尺寸，
// "同档里出现更小的块"只在高段发生，所以必须用高段的尺寸才覆盖得到这条路径。
func TestFreeListKeepsUndersized(t *testing.T) {
	d := newDrv(t)
	d.alloc(1120, 7)
	d.save(0)
	d.free(0)

	d.alloc(1920, 9)
	d.save(1)
	d.addrOf(1)
	d.sameAndPrint(0)       // 0：小的那一块不顶替大的这一笔
	d.headerAndPrint(1, -4) // 1920：返回的这一块自身够这一笔
	d.headerAndPrint(1, -8) // 9：类型字跟着这一次走

	// 那一块小的仍完整挂在链上：再要一笔一样大的，原样取回
	d.alloc(1120, 5)
	d.save(2)
	d.addrOf(2)
	d.sameAndPrint(0) // 1

	got, code := run(t, d.finish(t, 0))
	want := "0\n1920\n9\n1\n"
	if got != want {
		t.Fatalf("偏小的块被拿去顶大的一笔：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFreeListSplitsBigBlock 验证大块切分规则：**从链上摘下来的块比这一笔大，就切两半**——
// 用掉那一半的尺寸改写成本来要的那一笔，剩下那一节挂进它自己那一档。
// 治的是"整块交出、尺寸不动"那一族问题：同一档里一大一小交替要块时，小的一笔占用掉大的那一块，
// 下一笔要大的那一块时链上只剩小的 ⇒ 挂回去、走 bump ⇒ **每多一圈多要一整块**。
//
// 数值：数据 1920（总 1928）与数据 1120（总 1128）同在上界 2048 那一档 ⇒ 余量 800，
// 切出来那一节总 800（新头 8 ＋ 数据 792），落在上界 1024 那一档；两块起点差 1128。
// **本测试的数值取在高段**：低段按 8 字节精确分档之后每一档只剩一种尺寸，
// "比这一笔大"在低段不会发生 ⇒ 切块与"切不动"两种情形都只能在用高段尺寸时覆盖到。
func TestFreeListSplitsBigBlock(t *testing.T) {
	d := newDrv(t)
	d.alloc(1920, 7)
	d.save(0)
	d.free(0) // 总 1928 那一块进链（上界 2048 那一档）

	d.alloc(1120, 9) // 同一档里小一点的分配：切一刀
	d.save(1)
	d.addrOf(1)
	d.sameAndPrint(0)       // 1：返回的仍是原块的起点（切走的是尾部那一节）
	d.headerAndPrint(1, -4) // 1120：用掉那一半的尺寸跟着这一笔改小
	d.headerAndPrint(1, -8) // 9：类型字跟着这一次走

	d.alloc(792, 5) // 剩下那一节（总 800）正好够这一笔
	d.save(2)
	d.headerAndPrint(2, -4) // 792：它的尺寸是切出来时写入的那一个
	// 两块之间的距离＝用掉那一半的总尺寸（数据 1120 ＋ 头 8）＝1128（地址每次都变，比较差值）
	d.a.MovLoad(x64.RAX, x64.RBP, -24)
	d.a.MovLoad(x64.RCX, x64.RBP, -8)
	d.a.Sub(x64.RAX, x64.RCX)
	d.a.AddImm(x64.RAX, -1128)
	d.a.Mov(x64.RCX, x64.RAX)
	d.call("@put_int")
	d.nl()

	// 切出来的那一节不许被重复释放弄断链：释放两次（第二次认得类型字里的记号，即空操作），
	// 再要一笔一样大的，拿回来的还是它。
	d.free(2)
	d.free(2)
	d.alloc(792, 6)
	d.save(3)
	d.addrOf(3)
	d.sameAndPrint(2) // 1：链没断

	// "切不动"的那一档照旧整块给出、尺寸槽不动：余量 1920−1912＝8，装不下一颗头加 8 字节。
	d.alloc(1920, 3)
	d.save(4)
	d.free(4)
	d.alloc(1912, 4) // 余量 8 < 16 ⇒ 不切
	d.save(5)
	d.addrOf(5)
	d.sameAndPrint(4)       // 1：整块交出
	d.headerAndPrint(5, -4) // 1920：没切就不改尺寸

	got, code := run(t, d.finish(t, 0))
	want := "1\n1120\n9\n792\n0\n1\n1\n1920\n"
	if got != want {
		t.Fatalf("大块没拆两半（或者拆坏了链）：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFreeListExactBins 验证低段分档规则换来的**不变量**：低段一档一种尺寸，
// 于是只差 8 字节的两大一小不再互相占用——大块切分规则对这一对切不动（余量装不下一颗头），
// 只有把档拆到 8 字节粒度才能解决（实测到的 32／64 字节／圈额外开销正是从这一对来的）。
//
// 数据 16（总 24）与数据 24（总 32）：在旧的 2 的幂分档下同在上界 32 那一档，小的一笔会占用大的那块。
func TestFreeListExactBins(t *testing.T) {
	d := newDrv(t)
	d.alloc(16, 7)
	d.save(0)
	d.alloc(24, 8)
	d.save(1)
	d.addrOf(1)
	d.sameAndPrint(0) // 0：各拿各的块
	d.free(0)
	d.free(1)

	// 各还各的档：先要大的那一笔，拿回的是大的那块，块头尺寸没有任何改动
	d.alloc(24, 9)
	d.save(2)
	d.addrOf(2)
	d.sameAndPrint(1)       // 1
	d.headerAndPrint(2, -4) // 24

	// 小的那块没被顶掉：它还在链上等同样大小的那一笔
	d.alloc(16, 5)
	d.save(3)
	d.addrOf(3)
	d.sameAndPrint(0) // 1

	got, code := run(t, d.finish(t, 0))
	want := "0\n1\n24\n1\n"
	if got != want {
		t.Fatalf("低段没按尺寸精确分档：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

func TestDictIntKeys(t *testing.T) {
	d := newDrv(t)
	d.mapNew(0, 8, 0)
	d.save(0)
	d.mapPut(0, 5, -1, 77)
	d.mapPut(0, 6, -1, 88)
	d.mapLen(0)
	d.mapVal(0, 5, -1)
	d.mapPut(0, 5, -1, 99) // 同一个键再添一次是改值，不能多出一条
	d.mapLen(0)
	d.mapVal(0, 5, -1)
	d.mapContains(0, 6, -1)
	d.mapContains(0, 7, -1)
	d.mapRemoved(0, 5, -1) // 删成的那一条把值带回来：99
	d.mapContains(0, 5, -1)
	d.mapLen(0)
	d.mapRemoved(0, 5, -1) // 同一个键再删一次：没删成就返回 0，值那个位置也早在上一条抹空了
	got, code := run(t, d.finish(t, 0))
	want := "2\n77\n2\n99\ntrue\nfalse\n99\nfalse\n1\n0\n"
	if got != want {
		t.Fatalf("整数键不对：\n got %q\nwant %q\n码 %d", got, want, code)
	}
}

// TestDictTextKeys 走文本键：两块*不同对象、同样内容*的文本必须算同一个键（判等比较内容）。
// 只比较句柄的话这一条发现不了——那正是把内容比较错当成身份比较的情形。
func TestDictTextKeys(t *testing.T) {
	one := 0
	two := 0
	d := newDrv(t)
	one = d.addString("书包")
	two = d.addString("书包") // 同样内容，另一块对象
	other := d.addString("雨伞")
	if one == two {
		t.Fatalf("测试自己没构造出两块对象：%d", one)
	}
	d.mapNew(0, 8, 1)
	d.save(0)
	d.mapPut(0, 0, one, 11)
	d.mapVal(0, 0, two) // 拿另一块查：必须命中
	d.mapContains(0, 0, other)
	d.mapPut(0, 0, two, 22) // 同一个键再写一次：改值，不多出一条
	d.mapLen(0)
	d.mapVal(0, 0, one)
	got, _ := run(t, d.finish(t, 0))
	want := "11\nfalse\n1\n22\n"
	if got != want {
		t.Fatalf("文本键不对：\n got %q\nwant %q", got, want)
	}
}

// TestDictMissingKey 查询一个不存在的键要抛出 10032 号（语言规定：读要抛，写才是新增一条）。
func TestDictMissingKey(t *testing.T) {
	d := newDrv(t)
	d.mapNew(0, 8, 0)
	d.save(0)
	d.mapPut(0, 1, -1, 5)
	d.mapVal(0, 2, -1)
	got, code := run(t, d.finish(t, 0))
	if got != "出错了（10032）：没有这个键\n" || code != 1 {
		t.Fatalf("得到 %q 码 %d", got, code)
	}
}

// TestDictGrow 添加超过四分之三容量要翻倍再散列：搬完之后一个不能少、一个不能多。
// 表起步 8 格，添加到第 7 个就触发一次搬家。
func TestDictGrow(t *testing.T) {
	d := newDrv(t)
	d.mapNew(0, 8, 0)
	d.save(0)
	for i := int64(1); i <= 7; i++ {
		d.mapPut(0, i, -1, i*10)
	}
	d.mapLen(0)
	var want strings.Builder
	want.WriteString("7\n")
	for i := int64(1); i <= 7; i++ {
		d.mapVal(0, i, -1)
		fmt.Fprintf(&want, "%d\n", i*10)
	}
	got, _ := run(t, d.finish(t, 0))
	if got != want.String() {
		t.Fatalf("搬家之后对不上：\n got %q\nwant %q", got, want.String())
	}
}

// TestImportTableIsGroupedByModule 验证那张表的三条形状规矩：每个条目都要记录它属于哪个模块、
// 同一个模块的名字要连续排列、一个条目也不许多登记。中间夹进别的模块的名字会切出两个指向同一个
// 模块的描述符，加载器只认第一个，第二段的函数一个也进不了产物。
//
// 数量门槛是有意的检查项：多一个名字就是产物多一处要解析的东西；
// 数量变化后要重新测四条负载的性能与六份示例程序的堆读数。
func TestImportTableIsGroupedByModule(t *testing.T) {
	const (
		kernel32 = "kernel32.dll"
		winsock  = "ws2_32.dll"
		winhttp  = "winhttp.dll"
		// 42 个 kernel32 的名字：文件、目录、环境、时间那一摊 31 个，加 `process` 那四个，
		// 加每一条线程那一份状态块要的两个（要一个槽号、把块登记进那个槽），
		// 再加堆锁与打印锁那两条（拿与放）、"只结束这一条线程"那一条，
		// 再加起线程那两条（CreateThread 与归还那块状态块要用的 VirtualFree）；
		// 加 winsock 那 17 个：开机与收尾两个，加 `net` 那一组十五个；
		// 再加 winhttp 那 11 个（`http` 那一组：九条走一次请求，加追加请求头与设开关各一条）。
		namesBefore   = 70
		kernel32Names = 42
		winsockNames  = 17
		winhttpNames  = 11
	)
	imps := runtime.Build().Imports
	if len(imps) != namesBefore {
		t.Fatalf("导入表里的名字是 %d 个，从前数的是 %d 个",
			len(imps), namesBefore)
	}
	seen := map[runtime.Import]bool{}
	// runs 记录每个模块被排成了几段：多于一段就是中间夹进了别的模块的名字
	runs := map[string]int{}
	for i, imp := range imps {
		if imp.DLL == "" {
			t.Fatalf("第 %d 格 %s 没写它属于哪一个 DLL", i, imp.Name)
		}
		if seen[imp] {
			t.Fatalf("%s 的 %s 登记了两次", imp.DLL, imp.Name)
		}
		seen[imp] = true
		if i == 0 || imps[i-1].DLL != imp.DLL {
			runs[imp.DLL]++
		}
	}
	if len(runs) != 3 {
		t.Fatalf("这张表该排成三段（kernel32、winsock 与 winhttp），实际 %d 段：%v", len(runs), runs)
	}
	for dll, n := range runs {
		if n != 1 {
			t.Fatalf("%s 的名字分成 %d 段排了：同一个 DLL 的要连着排", dll, n)
		}
	}
	// 段与段的顺序也要固定：kernel32 那 31 格的格号是形状表与 `nf ir` 里输出的数，
	// 新的一段只能排在后面，不能插在中间把它们往后挪。
	if imps[0].DLL != kernel32 || imps[kernel32Names].DLL != winsock {
		t.Fatalf("两段的位置变了：第 1 格是 %s，第 %d 格是 %s",
			imps[0].DLL, kernel32Names+1, imps[kernel32Names].DLL)
	}
	if got := countOf(imps, winsock); got != winsockNames {
		t.Fatalf("winsock 那一段是 %d 个名字，要的是 %d 个（开机与收尾两个 ＋ `net` 那一组十五个）", got, winsockNames)
	}
	// 第三段排在最后：前两段的格号是形状表与 `nf ir` 里打印出来的数，不能被插进来的名字挪走。
	if imps[kernel32Names+winsockNames].DLL != winhttp {
		t.Fatalf("winhttp 那一段没排在最后：第 %d 格是 %s",
			kernel32Names+winsockNames+1, imps[kernel32Names+winsockNames].DLL)
	}
	if got := countOf(imps, winhttp); got != winhttpNames {
		t.Fatalf("winhttp 那一段是 %d 个名字，要的是 %d 个（`http` 那一组八条加关句柄与拆网址）", got, winhttpNames)
	}
}

// TestAllocFreeKeepLockRegisters 钉拿锁与放锁那一步不洗寄存器。
//
// 堆锁与打印锁那两条是系统调用，按调用约定要擦掉 caller-saved 那一组（rax／rcx／rdx 与 r8～r11）。
// 运行期自己那一层把 rdx／r10／r11 当合法的过路值用：@alloc 与 @free 从头到尾不碰这三个，
// 所以叫它们的那一条例程可以把一个地址留在 r10 里穿过这一次调用。早前没有护住的时候，
// 归还两块文本之后 r10 里那一个句柄没了，进程被操作系统终止。
// 做法是这三格在拿锁与放锁前后寄到 r13／r14／r15（被调方保存的那一组），叫完系统那一条再原样读回。
func TestAllocFreeKeepLockRegisters(t *testing.T) {
	d := newDrv(t)
	d.alloc(24, 7)
	d.save(0)
	d.alloc(24, 7)
	d.save(1)

	// @free 那一路：三个哨兵摆进去，归还一块，再把三个值挪进驱动帧的局部槽。
	// 必须先原样搬走再打印：打印那一条自己就用 r10／r11 做搬运，边打印边读会被它自己擦掉。
	d.a.MovImm(x64.RDX, 1000001)
	d.a.MovImm(x64.R10, 2000002)
	d.a.MovImm(x64.R11, 3000003)
	d.free(0)
	park := func(dst int, n int) {
		d.a.Mov(x64.RAX, dst)
		d.save(n)
	}
	park(x64.RDX, 3)
	park(x64.R10, 4)
	park(x64.R11, 5)
	printSlot := func(n int) {
		d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(n+1)))
		d.call("@put_int")
		d.nl()
	}
	printSlot(3)
	printSlot(4)
	printSlot(5)

	// @alloc 那一路：只用 r10／r11 做哨兵。rdx 在那一条里是类型字的入参，按调用约定本来就是
	// 叫完作废的，钉它等于把 @alloc 的内部写法冻住。
	d.a.MovImm(x64.R10, 4000004)
	d.a.MovImm(x64.R11, 5000005)
	d.a.MovImm(x64.RCX, 24)
	d.a.MovImm(x64.RDX, 8)
	d.call("@alloc")
	d.save(2)
	park(x64.R10, 7)
	park(x64.R11, 8)
	printSlot(7)
	printSlot(8)

	got, code := run(t, d.finish(t, 0))
	want := "1000001\n2000002\n3000003\n4000004\n5000005\n"
	if got != want || code != 0 {
		t.Fatalf("拿锁与放锁洗掉了寄存器：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestWriteKeepsLineUntilNewline 钉攒行那一条：不到换行不写出去，长度一直往上加；
// 到了换行整行一次交出去，长度抹回 0。屏幕上出来的字与不攒行时一模一样。
func TestWriteKeepsLineUntilNewline(t *testing.T) {
	d := newDrv(t)
	d.putStr("ab") // 这一段还留在缓冲里，屏幕上暂时没有
	d.printOutLen()
	d.nl() // 一行到了尽头：整行一次交出去
	d.printOutLen()
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if want := "ab2\n0\n"; got != want || code != 0 {
		t.Fatalf("攒行那一路不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestWriteOversizePieceBypassesBuffer 钉比整块行缓冲还长的那一笔：它不进缓冲，自己一次写出去。
// 缓冲里先前攒着的那一截要先交出去，屏幕上先后照旧。
func TestWriteOversizePieceBypassesBuffer(t *testing.T) {
	d := newDrv(t)
	big := strings.Repeat("x", 70000) // 比 outBufCap（65536）还长
	d.putStr("ab")
	d.putStr(big)
	d.printOutLen() // 那一笔没进缓冲：长度还是 0
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if want := "ab" + big + "0\n"; got != want || code != 0 {
		t.Fatalf("比整块缓冲还长的那一笔不对：got %d 字节 码 %d，want %d 字节", len(got), code, len(want))
	}
}

// TestWriteFlushesWhenLineDoesNotFit 钉接不上的那一种：一行还没写完但缓冲已经满了，
// 先把已经攒着的那一截写出去，再接着攒后面。屏幕上出来的字与一次写完完全相同。
func TestWriteFlushesWhenLineDoesNotFit(t *testing.T) {
	d := newDrv(t)
	xs := strings.Repeat("x", 65000)
	ys := strings.Repeat("y", 30000)
	d.putStr(xs) // 装得下：65000 字节留在缓冲里
	d.printOutLen()
	d.putStr(ys) // 65000 + 5 + 30000 超过整块缓冲：前面那一截先出去
	d.printOutLen()
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if want := xs + "65000" + ys + "30000\n"; got != want || code != 0 {
		t.Fatalf("接不上那一截不对：got %d 字节 码 %d，want %d 字节", len(got), code, len(want))
	}
}

// TestWinsockStartRunsOnce 钉 winsock 那一次初始化的护栏：@winsock_start 叫第二遍不再叫系统那一条。
//
// 只看 wsaOnce 那个标志翻了没有钉不住"跳过"这一件事，所以这里读 WSADATA 的头一个字节：
// 第一次叫过之后系统在那里写下了请求版本号的低字节 0x02；把它抹成一个哨兵再叫第二遍，
// 真的又初始化一次就会被写回 0x02。段 5 与段 6 里第二条线程也要进门，这一条钉的就是那时候的形状。
func TestWinsockStartRunsOnce(t *testing.T) {
	d := newDrv(t)
	d.call("@winsock_start")
	d.staticAddr(t, "wsadata", x64.RAX)
	d.a.MovZX8(x64.RCX, x64.RAX, 0)
	d.call("@put_int") // 2：系统真的初始化过一次
	d.nl()

	d.a.MovImm(x64.RCX, 153)
	d.staticAddr(t, "wsadata", x64.RAX)
	d.a.MovStore8(x64.RAX, 0, x64.RCX) // 哨兵抹进去
	d.call("@winsock_start")
	d.staticAddr(t, "wsadata", x64.RAX)
	d.a.MovZX8(x64.RCX, x64.RAX, 0)
	d.call("@put_int") // 还是 153：那一条系统调用没有再来第二遍
	d.nl()

	d.staticAddr(t, "wsaOnce", x64.RAX)
	d.a.MovLoad(x64.RCX, x64.RAX, 0)
	d.call("@put_int") // 护栏写着 1
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if want := "2\n153\n1\n"; got != want || code != 0 {
		t.Fatalf("winsock 那一次初始化的护栏不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestThreadExitLastThreadEndsProcess 钉那一步的"最后一条"分支：还有一条活着时叫 @thread_exit，
// 数到 0 就把收尾整个交给 @exit，所以退出码就是传进去的那一个，屏幕上没有别的东西。
// 今天程序只有一条线程，这一条钉的正是那条路现在走的形状。
func TestThreadExitLastThreadEndsProcess(t *testing.T) {
	d := newDrv(t) // 开局那一次 @state_init 已经把线程条数推成 1
	d.a.MovImm(x64.RCX, 3)
	d.a.MovImm(x64.RDX, 7) // 这一条线程自己的错误号
	d.call("@thread_exit")
	d.nl() // 走不到：那一条不返回
	got, code := run(t, d.finish(t, 0))
	if got != "" || code != 3 {
		t.Fatalf("最后一条线程结束要带走整个进程并留下退出码：\n got %q 码 %d\nwant 空输出 码 3", got, code)
	}
}

// TestThreadExitFlushesWhenAnotherRemains 钉那一步的"还有别人"分支：退之前条数是 2，就不是最后一条，
// 于是交出自己那一截行缓冲之后叫 ExitThread。这里没有真的第二条线程，用同一份驱动代码连叫两次
// @state_init 当两条线程各备一份（与 TestStateBlockPerThreadIsolation 同一做法）：第二次把条数推成 2，
// @thread_exit 数到 1 就走"还有别人"那一支。
//
// 两个读数钉两件事。屏幕上只有"还在"那两个字、后面没有堆用量那一行：那一截没到换行的字先交了
// 出去，而收尾（`@heap_report` 与 `ExitProcess`）整个没有走——那一段是留给最后一条线程的。
// 退出码不钉具体数值：进程由最后一条还在的线程叫 ExitThread 结束时，那一个数是操作系统给的，
// 这里只钉它不是传进去的 9（那一个数只在"最后一条"那一支生效）。
func TestThreadExitFlushesWhenAnotherRemains(t *testing.T) {
	t.Setenv("NEF_HEAP", "1")
	d := newDrv(t)
	d.call("@state_init") // 第二条那份：线程条数成 2
	d.putStr("还在")        // 没有换行：这一截还攒在缓冲里
	d.a.MovImm(x64.RCX, 9)
	d.a.MovImm(x64.RDX, 0)
	d.call("@thread_exit")
	d.nl() // 走不到
	got, code := run(t, d.finish(t, 0))
	if got != "还在" {
		t.Fatalf("还有别人时收尾不该走 @exit，那一截字却要先交出去：\n got %q\nwant \"还在\"", got)
	}
	if code == 9 {
		t.Fatalf("传进去的退出码只在该最后一条线程时生效，这一支却用了它：码 %d", code)
	}
}

// TestReleaseBeginIsPerThread 钉释放那一道护栏按线程各一份：一条线程摆下"正在释放"之后，
// 第二条线程读到的还是开局的 0——共用一格的后果是第二条线程跳过自己那一份顶层释放。
// 三条读数分别是 0（第一份第一次）、0（第二份第一次）、1（换回第一份第二次）。
// 换回第一份用的是先存下来的那一个起点，把 R12 摆回去；这一段仍然只有一条线程，
// 所以两次备块当两条线程各备一份，与 TestStateBlockPerThreadIsolation 同一做法。
func TestReleaseBeginIsPerThread(t *testing.T) {
	d := newDrv(t)
	d.save(0) // 第一份的起点
	printBegin := func() {
		d.call("@release_begin")
		d.a.Mov(x64.RCX, x64.RAX)
		d.call("@put_int")
		d.nl()
	}
	printBegin()          // 0：第一份的第一次
	d.call("@state_init") // 第二份
	printBegin()          // 0：第二份自己那个位置还没人摆过
	d.addrOf(0)
	d.a.Mov(x64.R12, x64.RAX) // 换回第一份
	printBegin()              // 1：那一份的护栏早在第一次就摆下了
	got, code := run(t, d.finish(t, 0))
	if want := "0\n0\n1\n"; got != want || code != 0 {
		t.Fatalf("释放那一道护栏没按线程各一份：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestWaitPathsReleaseStateBlock 钉等过之后的那两条收尾路都要归还那条线程的状态块。
// 块是 252,232 字节一整块 `VirtualAlloc` 来的，不在堆的账里，所以 `NEF_HEAP` 那张每轮增长表
// 看不见它：少了那一句归还，屏幕与退出码一字不变，只有长跑下来提交量一直涨。
// 钉法取的是发出来的引用清单：`@thread_wait` 与 `@thread_join` 每一份的引用里必须有且只有一格
// 指向导入表里的 `VirtualFree`。去掉 `threadTail` 里那一句 `impCall("VirtualFree")`，两处同时红。
//
// en: TestWaitPathsReleaseStateBlock pins that both waiting tails give that thread's state block
// back. The block is one 252,232-byte VirtualAlloc, outside the heap's accounting, so the NEF_HEAP
// per-round growth table cannot see it: drop the release and the screen, the exit code and every
// heap reading stay identical while committed memory keeps climbing over a long run. The pin reads
// the emitted reference list instead - each of @thread_wait and @thread_join must reference exactly
// one import-table slot holding VirtualFree. Deleting the impCall("VirtualFree") in threadTail turns
// both red at once.
func TestWaitPathsReleaseStateBlock(t *testing.T) {
	rt := runtime.Build()
	slot := -1
	for i, imp := range rt.Imports {
		if imp.Name == "VirtualFree" {
			slot = i
		}
	}
	if slot < 0 {
		t.Fatal("导入表里没有 VirtualFree，这一条测试钉的东西不存在")
	}
	for _, name := range []string{"@thread_wait", "@thread_join"} {
		hits := 0
		found := false
		for _, p := range rt.Parts {
			if p.Name != name {
				continue
			}
			found = true
			for _, r := range p.Refs {
				if r.Kind == runtime.RefIAT && r.Off == slot {
					hits++
				}
			}
		}
		if !found {
			t.Errorf("%s 不在例程清单里", name)
			continue
		}
		if hits != 1 {
			t.Errorf("%s 归还状态块那一句出现 %d 次，要正好一次：两次就是把同一块还两遍，零次就是漏", name, hits)
		}
	}
}

func countOf(imps []runtime.Import, dll string) int {
	n := 0
	for _, imp := range imps {
		if imp.DLL == dll {
			n++
		}
	}
	return n
}

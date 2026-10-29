// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package codegen

import (
	"testing"

	"neflang.org/nf/internal/exetest"
	"neflang.org/nf/internal/x64"
)

// 帧的形状全靠这几个算式撑着，错了就是内存被写坏那种查不动的 bug。
// 先做纯算式的不变量检查，再拿实际运行的探针兜一遍——算式对不代表 CPU 认。
// 出参那一格尤其要按个数核：2026-10-26 修 后二十五 之前，帧底只留一格，
// 五个及以上实参的调用会把自己的槽区从下面盖穿（包括帧尾的清理记录）。
func TestFrameArithmetic(t *testing.T) {
	for slots := 0; slots <= 64; slots++ {
		for maxOut := 1; maxOut <= 20; maxOut++ {
			fs := frameSize(slots, maxOut)
			if fs%stackAlign != 0 {
				t.Fatalf("%d 槽 %d 格出参：帧大小 %d 不是 %d 的倍数", slots, maxOut, fs, stackAlign)
			}
			// 最深的槽必须在第五参数那个位置之上，两边不能重叠
			deepest := slotOff(slots - 1)
			a5 := outArgOff(slots, maxOut, 5)
			if slots > 0 && deepest <= a5 {
				t.Fatalf("%d 槽 %d 格出参：最深槽 %d 和第五参数 %d 重叠（帧 %d）", slots, maxOut, deepest, a5, fs)
			}
			// 最靠上的出参（按个数排到顶）也不许进槽区：第 4+maxOut 个参数离 rbp 最近
			last := outArgOff(slots, maxOut, 4+maxOut)
			if slots > 0 && last >= deepest {
				t.Fatalf("%d 槽 %d 格出参：第 %d 个参数 %d 顶进了槽区（最深槽 %d）", slots, maxOut, 4+maxOut, last, deepest)
			}
			// 最靠下的出参必须在帧内：[rsp+0x20] 起步，一格都不能跌到帧底以下
			a1 := outArgOff(slots, maxOut, 5)
			if a1 < -fs {
				t.Fatalf("%d 槽 %d 格出参：第五参数 %d 跌出帧底 %d", slots, maxOut, a1, -fs)
			}
			if a1 != -fs+shadowBytes {
				t.Fatalf("%d 槽 %d 格出参：第五参数应在 [rsp+0x%x]，算出 %d", slots, maxOut, shadowBytes, a1)
			}
			if inArgOff(5) != 0x30 {
				t.Fatalf("入参第五个应在 [rbp+0x30]，算出 %d", inArgOff(5))
			}
		}
	}
}

// runExe 装配并实际运行，返回输出与退出码。
func runExe(t *testing.T, l *Linker, userData []byte) exetest.Result {
	t.Helper()
	exe, err := l.Build(userData, 0)
	if err != nil {
		t.Fatalf("装配失败：%v", err)
	}
	return exetest.Run(t, exe, "probe.exe")
}

// 探针一：40 个槽全填上哨兵，再把 5 当第五个参数交出去。
// 槽区要是顶到了出参区，印回来的就是哨兵而不是 5。
func TestProbeSlotsVsOutgoingArgs(t *testing.T) {
	const slots = 40
	l := NewLinker()
	a := l.Asm()
	callee := l.Label()

	EntryPrologue(a, slots, 1)
	a.MovImm(x64.RAX, 0xdeadbeef)
	for i := 0; i < slots; i++ {
		a.MovStore(x64.RBP, slotOff(i), x64.RAX)
	}
	a.MovImm(x64.RAX, 5)
	a.MovStore(x64.RBP, outArgOff(slots, 1, 5), x64.RAX)
	a.MovImm(x64.RCX, 1)
	a.MovImm(x64.RDX, 2)
	a.MovImm(x64.R8, 3)
	a.MovImm(x64.R9, 4)
	a.Call(callee)
	a.Mov(x64.RCX, x64.RAX)
	l.CallRt("@put_int")
	l.CallRt("@put_nl")
	a.MovImm(x64.RCX, 0)
	l.CallRt("@exit")

	a.Mark(callee)
	Prologue(a, 1, 1)
	a.MovLoad(x64.RAX, x64.RBP, inArgOff(5))
	Epilogue(a, 1, 1)
	a.Ret()

	runExe(t, l, nil).Want(t, "5\n", 0)
}

// 探针四：把 后二十五 那一次的实际形状钉死——29 格的帧（最深槽是清理记录的一格），
// 一次带九个实参的调用。第五到第九个参数按新算式全落在帧底空地里；
// 旧算式只留一格，第九个参数正好砸穿最深那三格槽。
// 调用之前把最深格填上哨兵、九个参数填 1 到 9，调用回来读哨兵：
// 印 0xdeadbeef 才算没盖；印出别的数就是被第几个参数盖了，当场看得出来。
func TestProbeNineArgsVsRecordSlots(t *testing.T) {
	const slots = 29
	l := NewLinker()
	a := l.Asm()
	callee := l.Label()

	EntryPrologue(a, slots, 5)
	a.MovImm(x64.RAX, 0xdeadbeef)
	a.MovStore(x64.RBP, slotOff(slots-1), x64.RAX)
	a.MovImm(x64.RAX, 0xbeef)
	a.MovStore(x64.RBP, slotOff(slots-2), x64.RAX)
	for i := 0; i < 5; i++ {
		a.MovImm(x64.RAX, int64(5+i+1))
		a.MovStore(x64.RBP, outArgOff(slots, 5, 5+i), x64.RAX)
	}
	a.MovImm(x64.RCX, 1)
	a.MovImm(x64.RDX, 2)
	a.MovImm(x64.R8, 3)
	a.MovImm(x64.R9, 4)
	a.Call(callee)
	a.MovLoad(x64.RCX, x64.RBP, slotOff(slots-1))
	l.CallRt("@put_int")
	l.CallRt("@put_nl")
	a.MovImm(x64.RCX, 0)
	l.CallRt("@exit")

	a.Mark(callee)
	Prologue(a, 1, 1)
	a.Xor(x64.RAX, x64.RAX)
	Epilogue(a, 1, 1)
	a.Ret()

	runExe(t, l, nil).Want(t, itoa(0xdeadbeef)+"\n", 0)
}

// 探针二：movdqa 要求地址 16 字节对齐，不齐当场异常。
// 拿 [rbp-frameSize] 这个地址来试——它数值上就是 rsp，但走 rbp 寻址，
// 不必让编码器支持 rsp 当基址（那条要 SIB，编码器直接拒了）。
// 每个槽数都试一遍：算式对不代表 CPU 认。
func TestProbeStackAlignment(t *testing.T) {
	for _, slots := range []int{0, 1, 2, 3, 8, 40} {
		l := NewLinker()
		a := l.Asm()
		EntryPrologue(a, slots, 1)
		a.MovdqaLoad(0, x64.RBP, -frameSize(slots, 1))
		a.MovImm(x64.RCX, int64(slots))
		l.CallRt("@put_int")
		l.CallRt("@put_nl")
		a.MovImm(x64.RCX, 0)
		l.CallRt("@exit")
		got := runExe(t, l, nil)
		if got.Code != 0 {
			t.Fatalf("%d 槽：退出码 %d，输出 %q（栈没对齐？）", slots, got.Code, got.Stdout)
		}
		if got.Stdout != itoa(slots)+"\n" {
			t.Fatalf("%d 槽：输出 %q", slots, got.Stdout)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var bs []byte
	for n > 0 {
		bs = append([]byte{byte('0' + n%10)}, bs...)
		n /= 10
	}
	return string(bs)
}

// 探针三：递归 200 层，每层 3 个槽，还要在最深那一层回来之后继续用槽。
// 帧要是互相覆盖了，sum 就不是 20100。
func TestProbeDeepRecursion(t *testing.T) {
	l := NewLinker()
	a := l.Asm()
	rec := l.Label()

	EntryPrologue(a, 1, 1)
	a.MovImm(x64.RCX, 200)
	a.Call(rec)
	a.Mov(x64.RCX, x64.RAX)
	l.CallRt("@put_int")
	l.CallRt("@put_nl")
	a.MovImm(x64.RCX, 0)
	l.CallRt("@exit")

	a.Mark(rec)
	Prologue(a, 3, 1)
	a.MovStore(x64.RBP, slotOff(0), x64.RCX) // slot0 = n
	a.MovLoad(x64.RAX, x64.RBP, slotOff(0))
	zero := l.Label()
	end := l.Label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, zero)
	a.Mov(x64.RCX, x64.RAX)
	a.AddImm(x64.RCX, -1) // rcx = n-1
	a.Call(rec)
	a.MovStore(x64.RBP, slotOff(1), x64.RAX) // slot1 = 递归结果
	a.MovLoad(x64.RAX, x64.RBP, slotOff(0))
	a.MovLoad(x64.RDX, x64.RBP, slotOff(1))
	a.Add(x64.RAX, x64.RDX)
	a.Jmp(end)
	a.Mark(zero)
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(end)
	Epilogue(a, 3, 1)
	a.Ret()

	runExe(t, l, nil).Want(t, "20100\n", 0)
}

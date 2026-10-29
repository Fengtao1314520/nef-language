// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package x64

import (
	"encoding/hex"
	"testing"
)

// 每条都写死期望字节。编码器最坏的一类错是"看着能跑"：寄存器字段写错，
// 只有恰好操作 rax 的那几条对得上，其余静默错——所以这里刻意让每条都用非 rax 的寄存器。
func TestEncodings(t *testing.T) {
	cases := []struct {
		name string
		want string
		f    func(a *Asm)
	}{
		{"mov rdx,rcx", "4889ca", func(a *Asm) { a.Mov(RDX, RCX) }},
		{"mov r8,r9", "4d89c8", func(a *Asm) { a.Mov(R8, R9) }},
		{"mov rcx,-11", "48c7c1f5ffffff", func(a *Asm) { a.MovImm(RCX, -11) }},
		{"movabs rdx", "48ba8877665544332211", func(a *Asm) { a.MovImm(RDX, 0x1122334455667788) }},
		{"mov rdx,[rbp-8]", "488b55f8", func(a *Asm) { a.MovLoad(RDX, RBP, -8) }},
		{"mov r8,[rbp-16]", "4c8b45f0", func(a *Asm) { a.MovLoad(R8, RBP, -16) }},
		{"mov [rbp-8],rcx", "48894df8", func(a *Asm) { a.MovStore(RBP, -8, RCX) }},
		{"mov qword [rbp-8],0", "48c745f800000000", func(a *Asm) { a.MovImmMem(RBP, -8, 0) }},
		{"mov qword [rbp-0xE0],-1", "48c78520ffffffffffffff", func(a *Asm) { a.MovImmMem(RBP, -0xE0, -1) }},
		{"mov rdx,[rax]", "488b10", func(a *Asm) { a.MovLoad(RDX, RAX, 0) }},
		{"mov [rdi],dl", "8817", func(a *Asm) { a.MovStore8(RDI, 0, RDX) }},
		{"movzx rsi,byte[rdi]", "480fb637", func(a *Asm) { a.MovZX8(RSI, RDI, 0) }},
		{"mov rdx,[rbp-0xE0]", "48899520ffffff", func(a *Asm) { a.MovStore(RBP, -0xE0, RDX) }},
		{"add rsi,-1", "4883c6ff", func(a *Asm) { a.AddImm(RSI, -1) }},
		{"add rcx,48", "4883c130", func(a *Asm) { a.AddImm(RCX, 48) }},
		{"cmp rax,0x800", "4881f800080000", func(a *Asm) { a.CmpImm(RAX, 0x800) }},
		{"sub rdx,rdi", "4829fa", func(a *Asm) { a.Sub(RDX, RDI) }},
		{"and rdx,0x3f", "4881e23f000000", func(a *Asm) { a.AndImm(RDX, 0x3F) }},
		{"or rdx,0x80", "4881ca80000000", func(a *Asm) { a.OrImm(RDX, 0x80) }},
		{"shr rdx,12", "48c1ea0c", func(a *Asm) { a.ShrImm(RDX, 12) }},
		{"sar rax,3", "48c1f803", func(a *Asm) { a.SarImm(RAX, 3) }},
		{"div r11", "49f7f3", func(a *Asm) { a.Div1(R11) }},
		{"idiv rcx", "48f7f9", func(a *Asm) { a.Idiv(RCX) }},
		{"imul rax,r9", "490fafc1", func(a *Asm) { a.Imul(RAX, R9) }},
		{"neg rax", "48f7d8", func(a *Asm) { a.Neg(RAX) }},
		{"test rsi,rsi", "4885f6", func(a *Asm) { a.Test(RSI, RSI) }},
		{"lea rcx,[rbp-0x20]", "488d4de0", func(a *Asm) { a.LeaMem(RCX, RBP, -0x20) }},
		{"setne r8b", "410f95c0", func(a *Asm) { a.Setcc(NE, R8) }},
		{"push rbp", "55", func(a *Asm) { a.Push(RBP) }},
		{"push r12", "4154", func(a *Asm) { a.Push(R12) }},
		{"call rax", "ffd0", func(a *Asm) { a.CallReg(RAX) }},
		{"ret", "c3", func(a *Asm) { a.Ret() }},
		{"movsd xmm1,[rbp-8]", "f20f104df8", func(a *Asm) { a.MovsdLoad(1, RBP, -8) }},
		{"movsd [rbp-8],xmm2", "f20f1155f8", func(a *Asm) { a.MovsdStore(RBP, -8, 2) }},
		{"addsd xmm0,xmm3", "f20f58c3", func(a *Asm) { a.Addsd(0, 3) }},
		{"cvtsi2sd xmm0,rsi", "f2480f2ac6", func(a *Asm) { a.CvtSI2SD(0, RSI) }},
		{"cvttsd2si rax,xmm1", "f2480f2cc1", func(a *Asm) { a.CvtTSD2SI(RAX, 1) }},
		{"roundsd xmm0,xmm1,1", "660f3a0bc101", func(a *Asm) { a.Roundsd(0, 1, 1) }},
		{"ucomisd xmm0,xmm1", "660fb2c1", func(a *Asm) { a.Ucomisd(0, 1) }},
		{"movq rax,xmm1", "66480f7ec8", func(a *Asm) { a.MovsdToGPR(RAX, 1) }},
		{"movq r8,xmm3", "66490f7ed8", func(a *Asm) { a.MovsdToGPR(R8, 3) }},
		{"movq xmm2,rcx", "66480f6ed1", func(a *Asm) { a.MovsdFromGPR(2, RCX) }},
		// 栈那一检查用的两条。gs 那条的位移用真正会读的那个号：NT_TIB 里的栈基 0x08。
		{"mov rax,gs:[0x8]", "65488b042508000000", func(a *Asm) { a.MovFromTeb(RAX, 8) }},
		{"cmp rsp,[rip]", "483b2500000000", func(a *Asm) { a.CmpRspRIP() }},
	}
	for _, tc := range cases {
		a := New()
		tc.f(a)
		if got := hex.EncodeToString(a.Code()); got != tc.want {
			t.Errorf("%s：落出 %s，要 %s", tc.name, got, tc.want)
		}
	}
}

// 跳转与取地址的相对位移要在 Finish 里回填，这里验的是回填算得对不对。
// 条件码抄错的后果是"边界偏一格"，最能骗过粗看的一种错。这里把 16 个条件的
// 指令字节逐个固定住——低四位必须等于条件码本身。
func TestConditionEncodings(t *testing.T) {
	cases := []struct {
		c    Cond
		jcc  string
		setc string
	}{
		{O, "0f80", "0f90"}, {NO, "0f81", "0f91"},
		{B, "0f82", "0f92"}, {AE, "0f83", "0f93"},
		{E, "0f84", "0f94"}, {NE, "0f85", "0f95"},
		{BE, "0f86", "0f96"}, {A, "0f87", "0f97"},
		{S, "0f88", "0f98"}, {NS, "0f89", "0f99"},
		{P, "0f8a", "0f9a"}, {NP, "0f8b", "0f9b"},
		{L, "0f8c", "0f9c"}, {GE, "0f8d", "0f9d"},
		{LE, "0f8e", "0f9e"}, {G, "0f8f", "0f9f"},
	}
	for _, tc := range cases {
		a := New()
		a.Jcc(tc.c, 1)
		a.Mark(1)
		got := hex.EncodeToString(a.Code())
		if got[:4] != tc.jcc {
			t.Errorf("条件 %d：jcc 前缀落出 %s，要 %s", tc.c, got[:4], tc.jcc)
		}
		b := New()
		b.Setcc(tc.c, RCX)
		got = hex.EncodeToString(b.Code())
		if got[:4] != tc.setc {
			t.Errorf("条件 %d：setcc 前缀落出 %s，要 %s", tc.c, got[:4], tc.setc)
		}
	}
}

func TestFixups(t *testing.T) {
	a := New()
	a.Jmp(7)         // 5 字节，disp 在 1
	a.Nop()          // 占一字节，让目标不在紧邻位置
	a.Mark(7)        // 位置 6
	a.MovImm(RAX, 1) // disp 之后
	got, err := a.Finish()
	if err != nil {
		t.Fatal(err)
	}
	// 目标 6 减去下一条指令起点 5 等于 1
	if hex.EncodeToString(got[1:5]) != "01000000" {
		t.Fatalf("jmp 回填错：%s", hex.EncodeToString(got))
	}
}

func TestRipSites(t *testing.T) {
	a := New()
	at := a.LeaRIP(RCX)
	if at != 3 {
		t.Fatalf("lea rip 的回填点是 %d，要 3（rex、操作码、modrm 各一字节）", at)
	}
	a.Mark(1)
	if _, err := a.Finish(); err != nil {
		t.Fatal(err)
	}
}

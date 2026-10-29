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

// Package codegen 把中间码落成 x86-64 机器码。
//
// 这一层刻意不做寄存器分配：每个值都住在帧槽里，算绪前搬进 rax／rcx，
// 调用前该 spill 的就 spill。理由有两条——x64 编码器只支持 [base+disp] 一种
// 内存形式（没有 SIB），以及"出错就是内存被写坏"这种 bug 太难查，
// 不如一开始就少一个变量。性能等链表跑通再谈。
//
// en: Package codegen lowers intermediate code into x86-64 machine code. This layer
// deliberately does no register allocation: every value lives in a frame slot, is moved into
// rax/rcx before it is used, and is spilled before a call when required. Two reasons: the x64
// encoder supports only the [base+disp] memory form (no SIB), and a bug of the form "wrong
// memory got written" is far too hard to diagnose to be worth one more variable. Performance is
// discussed after the linked list runs.
package codegen

import "neflang.org/nf/internal/x64"

// 帧的形状。被调方进来先 push rbp 把帧基立起来，于是：
//
//	高地址
//	  [rbp+0]      调用方的 rbp
//	  [rbp-8]      槽 0
//	  ...          槽 1..slots-1，每槽 8 字节
//	  [rbp-8*slots]
//	  ...          往下是这一帧给被调方准备的第五个参数与影子空间
//	  [rsp+0x20]   第五个及以后的参数
//	  [rsp+0x1F..0] 影子空间（32 字节）
//	低地址
//
// 槽数一多就会占用到参数区，所以帧大小必须按槽数算，不能像运行期例程那样写死 0x100：
// 写死 0x100 时第 27 格正好压在第五参数那个位置上，静默毁参。
//
// en: The shape of a frame. The callee starts by pushing rbp to raise the frame base, and then:
// high address, [rbp+0] the caller's rbp, [rbp-8] slot 0, then slots 1..slots-1 at 8 bytes each
// down to [rbp-8*slots], below that this frame's fifth argument and shadow space, [rsp+0x20]
// the fifth and later arguments, [rsp+0x1F..0] the 32-byte shadow space, low address. Once there
// are enough slots the region overlaps the arguments, so the frame size
// must be computed from the slot count and cannot be hardcoded to 0x100 the way the runtime
// routines do: with a hardcoded 0x100, slot 27 lands exactly on the fifth argument's position
// and silently destroys it.
const (
	shadowBytes = 0x20 // 影子空间：32 字节
	stackAlign  = 16   // Windows x64 要求调用点 rsp 16 字节对齐

	// tebStackBase 是线程控制块里"这一段的栈基"那个位置——NT_TIB 的第二格，8 字节指针。
	tebStackBase = 0x08
	// stackSlack 是触到边界之前先让出来的一段：检查在离栈底还差这一截时就停，
	// 于是 @die_stack 那一帧、输出错误信息那一路调用、以及 WriteFile 自己用的栈都还在保留里。
	stackSlack = 0x10000
)

// outArgBytes 是这一帧要替第五个及以后的实参留出的字节数。
//
// 早先的形状是写死的 8 字节（只留得下一个参数）：一个函数里只要有一次调用带五个及以上的
// 实参，第六个起就会从帧底往上覆盖进本帧自己的槽区——帧尾的清理记录（五格）先被覆盖。
// 覆盖到哪一格取决于 frameSize 的 16 字节取整能不能多让出一格：只让出一格时被毁的是落点、
// rbp、rsp 三个槽位（正常返回用不到它们，抛出时才崩溃，是一处潜伏的故障）；恰好多让两格时
// 链头也一起被毁——链头从此指向一块文本，实测过一次：@pop_handler 把字符串句柄当链头，
// 读 [句柄+0x18] 读出 0xFFFFFFFFFFFFFFFF。
// 现在按本帧出参的最多数留槽位，各段之间不再相互覆盖。
//
// en: outArgBytes is the room this frame must keep for the fifth and later arguments of a
// call. It used to be a hardcoded 8 bytes - one slot - so any call with five or more arguments
// wrote upward from the frame bottom into the frame's own slots, and the five-slot frame-scope
// record at the tail took the damage first. Which slot got hit came down to whether the
// 16-byte rounding of frameSize gave away an extra slot: when it gave away only one, the
// damaged slots were the landing point, rbp and rsp - unused on a normal return, so they only
// faulted on a throw, a latent failure; when the rounding gave away two, the chain pointer
// itself was destroyed too, and @pop_handler then read a text handle as the chain head, taking
// 0xFFFFFFFFFFFFFFFF out of [handle+0x18]. Now the frame keeps one slot per outgoing argument,
// so no two regions overlap.
func outArgBytes(maxOut int) int {
	if maxOut < 1 {
		maxOut = 1
	}
	return 8 * maxOut
}

// frameSize 返回 slots 个槽、并为 maxOut 个第五及以后的出参留好位置的帧字节数，已是 16 的倍数。
//
// en: frameSize returns the frame bytes needed for slots slots plus maxOut outgoing argument
// slots, already a multiple of 16.
func frameSize(slots, maxOut int) int32 {
	n := 8*slots + shadowBytes + outArgBytes(maxOut)
	if n%stackAlign != 0 {
		n += stackAlign - n%stackAlign
	}
	return int32(n)
}

// slotOff 是第 i 个槽相对 rbp 的位移。
//
// en: slotOff is the displacement of slot i relative to rbp.
func slotOff(i int) int32 { return -8 * int32(i+1) }

// arg5Off 是这一帧放第五个实参的那个位置相对 rbp 的位移（等于 [rsp+0x20]）。
//
// en: arg5Off is the position in this frame holding the fifth argument, relative to rbp (it is
// [rsp+0x20]).
func arg5Off(slots, maxOut int) int32 { return -frameSize(slots, maxOut) + shadowBytes }

// outArgOff 返回这一帧作为**调用方**放第 n 个（n 从 5 起）实参的位置，相对 rbp。
//
// en: outArgOff returns the position in this frame, as the CALLER, for the n-th argument (n
// counted from 5), relative to rbp.
func outArgOff(slots, maxOut, n int) int32 {
	if n < 5 {
		panic("前四个参数走寄存器，不该来问这个")
	}
	return arg5Off(slots, maxOut) + int32(8*(n-5))
}

// inArgOff 返回这一帧作为**被调方**读第 n 个（n 从 5 起）入参的位置，相对 rbp。
//
// 调用方把第五个参数放在它自己的 [rsp+0x20]；`call` 压进返回地址、本帧再 push rbp，
// 一共往下挪了 0x10，所以那是 [rbp+0x30]——正偏移，和 outArgOff 方向相反，两者不可混用。
//
// en: inArgOff returns the position in this frame, as the CALLEE, for the n-th incoming
// argument (n counted from 5), relative to rbp. The caller places its fifth argument at its own
// [rsp+0x20]; `call` pushes the return address and this frame pushes rbp, so it moved down by
// 0x10 in total - which makes it [rbp+0x30], a POSITIVE offset, in the opposite direction from
// outArgOff, so the two must not be confused.
func inArgOff(n int) int32 {
	if n < 5 {
		panic("前四个参数走寄存器，不该来问这个")
	}
	return 0x30 + int32(8*(n-5))
}

// Prologue 是一个普通函数的开场。进来时 rsp ≡ 8 (mod 16)，push rbp 之后正好回到
// 16 字节边界，而 frameSize 是 16 的倍数，所以本帧里再 call 出去仍然对齐。
//
// en: Prologue is the opening of an ordinary function. On entry rsp is 8 (mod 16); after push
// rbp the stack is back on a 16-byte boundary, and since frameSize is a multiple of 16, any
// call made from this frame is still aligned.
func Prologue(a *x64.Asm, slots, maxOut int) {
	a.Push(x64.RBP)
	a.Mov(x64.RBP, x64.RSP)
	a.AddImm(x64.RSP, -frameSize(slots, maxOut))
}

// Epilogue 收掉 Prologue 立起来的那一帧。
//
// en: Epilogue tears down the frame that Prologue raised.
func Epilogue(a *x64.Asm, slots, maxOut int) {
	a.AddImm(x64.RSP, frameSize(slots, maxOut))
	a.Pop(x64.RBP)
}

// EntryPrologue 摆进程入口的开场。入口没有调用方，Windows 交给它时 rsp 的奇偶性
// 和"被 call 进来"不一样，差 8 字节——不在这里对齐，后面每一帧都偏 8，
// 而 kernel32 里有些函数会执行要求对齐的指令，于是出现一处难以定位的访问违例。
//
// en: EntryPrologue arranges the opening of the process entry. The entry has no caller, and
// when Windows hands it over, rsp's parity differs from the called case by 8 bytes - if it is
// not aligned here, every frame below is off by 8, and some kernel32 functions execute
// alignment-requiring instructions, so the result is an access violation that is hard to locate.
func EntryPrologue(a *x64.Asm, slots, maxOut int) {
	a.AndImm(x64.RSP, -16) // 先归到 16 字节边界，不管进来是什么形状
	a.AddImm(x64.RSP, -8)  // 摆成"刚被 call 进来"那个位置
	Prologue(a, slots, maxOut)
}

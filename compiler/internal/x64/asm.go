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

// Package x64 是 Windows x86-64 的指令编码器。
//
// 只覆盖后端真正用得上的那一组指令，不做完整汇编器：所有算绪都走"值在栈槽里、
// 计算在通用寄存器上"的窄模型，所以这里不提供"寄存器对内存"的加减法——
// 要读内存就先 mov 进寄存器。
//
// base 用 r12 需要 SIB 字节，本编码器不支持；发射器只拿 rbx／rbp 当基址。
//
// 跳转与取地址用标号（Label），由 Asm.Finish 回填相对位移。
//
// en: Package x64 is the instruction encoder for Windows x86-64. It covers only the set of
// instructions the backend actually uses and is not a complete assembler: every computation
// follows the narrow model "values live in stack slots, arithmetic happens in general-purpose
// registers", so no register-against-memory add or subtract is offered - to touch memory, move
// into a register first. Using r12 as a base requires a SIB byte, which this encoder does not
// support; the emitter only ever uses rbx/rbp as a base. Jumps and taking an address use labels
// (Label), with the relative displacement filled in by Asm.Finish.
package x64

import (
	"encoding/binary"
	"fmt"
)

// 寄存器编号，照 x64 的编码顺序。
//
// en: Register numbers, in x64 encoding order.
const (
	RAX = 0
	RCX = 1
	RDX = 2
	RBX = 3
	RSP = 4
	RBP = 5
	RSI = 6
	RDI = 7
	R8  = 8
	R9  = 9
	R10 = 10
	R11 = 11
	R12 = 12
	R13 = 13
	R14 = 14
	R15 = 15
)

// Cond 是 Jcc／Setcc 用的条件码，值就是指令的低四位。
// 这套编号没有规律可推导，抄错一位就是表面能运行、边界判定全部偏移，所以每条都标了对应的指令名。
//
// en: Cond is the condition code used by Jcc/Setcc; its value is the low four bits of the
// instruction. This numbering has no pattern to derive from, and one wrong digit gives an
// instruction that appears to run while every boundary check is off by one, so every constant
// is annotated with the instruction it belongs to.
type Cond byte

const (
	O  Cond = 0  // JO   溢出
	NO Cond = 1  // JNO  不溢出
	B  Cond = 2  // JB   无符号小于
	AE Cond = 3  // JAE  无符号大于等于
	E  Cond = 4  // JE   相等
	NE Cond = 5  // JNE  不相等
	BE Cond = 6  // JBE  无符号小于等于
	A  Cond = 7  // JA   无符号大于
	S  Cond = 8  // JS   负
	NS Cond = 9  // JNS  非负
	P  Cond = 10 // JP   奇偶位为 1（ucomisd 用它判断 NaN／无序）
	NP Cond = 11 // JNP  奇偶位为 0：两个操作数都是数，不是 NaN
	L  Cond = 12 // JL   有符号小于
	GE Cond = 13 // JGE  有符号大于等于
	LE Cond = 14 // JLE  有符号小于等于
	G  Cond = 15 // JG   有符号大于
)

// Inverse 返回条件取反。Br 带 Neg 时用，省得再发一条 jmp。
//
// en: Inverse returns the negated condition. Br uses it together with Neg, which saves emitting
// an extra jmp.
func (c Cond) Inverse() Cond {
	switch c {
	case E:
		return NE
	case NE:
		return E
	case L:
		return GE
	case GE:
		return L
	case LE:
		return G
	case G:
		return LE
	case B:
		return AE
	case AE:
		return B
	case BE:
		return A
	case A:
		return BE
	case S:
		return NS
	case NS:
		return S
	case O:
		return NO
	case NO:
		return O
	}
	panic("认不出的条件码")
}

// Label 是一个回填点。
//
// en: Label is one backfill point.
type Label int

// Asm 往 buffer 里追加机器码。
//
// en: Asm appends machine code into its buffer.
type Asm struct {
	buf     []byte
	offsets map[Label]int
	pending []fixup
}

type fixupKind int

const (
	fixRel32 fixupKind = iota // 相对下一条指令起点
)

type fixup struct {
	kind   fixupKind
	at     int
	target Label
}

func New() *Asm { return &Asm{offsets: map[Label]int{}} }

// Code 返回已经落出来的字节。
//
// en: Code returns the bytes emitted so far.
func (a *Asm) Code() []byte { return a.buf }

// PC 返回当前写入位置。
//
// en: PC returns the current write position.
func (a *Asm) PC() int { return len(a.buf) }

// OffsetOf 查一个已经落过的标号在缓冲区里的位置。
//
// en: OffsetOf looks up where an already-marked label sits in the buffer.
func (a *Asm) OffsetOf(l Label) (int, bool) {
	n, ok := a.offsets[l]
	return n, ok
}

// Mark 在标号处做记号。
//
// en: Mark places a label at the current position.
func (a *Asm) Mark(l Label) {
	if _, dup := a.offsets[l]; dup {
		panic(fmt.Sprintf("标号 %d 重复登记", l))
	}
	a.offsets[l] = len(a.buf)
}

// Finish 回填位移，返回最终字节。
//
// en: Finish fills in the displacements and returns the final bytes.
func (a *Asm) Finish() ([]byte, error) {
	out := make([]byte, len(a.buf))
	copy(out, a.buf)
	for _, f := range a.pending {
		to, ok := a.offsets[f.target]
		if !ok {
			return nil, fmt.Errorf("标号 %d 没有落点", f.target)
		}
		rel := int64(to) - int64(f.at+4)
		if rel < -0x80000000 || rel > 0x7fffffff {
			return nil, fmt.Errorf("标号 %d 离得太远，32 位位移装不下", f.target)
		}
		binary.LittleEndian.PutUint32(out[f.at:], uint32(int32(rel)))
	}
	return out, nil
}

func (a *Asm) w(v ...byte) { a.buf = append(a.buf, v...) }

// Append 把一段已经落好的字节接进来（运行期例程整块挂在用户代码后面时用）。
//
// en: Append grafts a run of already-emitted bytes onto the buffer (used when the whole runtime
// block is hung behind the user code).
func (a *Asm) Append(b []byte) { a.buf = append(a.buf, b...) }

func (a *Asm) u32(v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	a.buf = append(a.buf, t[:]...)
}

func (a *Asm) u64(v uint64) {
	var t [8]byte
	binary.LittleEndian.PutUint64(t[:], v)
	a.buf = append(a.buf, t[:]...)
}

func high(r int) byte {
	if r > 7 {
		return 1
	}
	return 0
}

// rex 拼一个 REX 前缀；w 位表示 64 位操作数。
//
// en: rex assembles one REX prefix; the w bit marks a 64-bit operand.
func (a *Asm) rex(w bool, reg, index, base int) {
	r := byte(0)
	if w {
		r |= 8
	}
	if reg > 7 {
		r |= 4
	}
	if index > 7 {
		r |= 2
	}
	if base > 7 {
		r |= 1
	}
	if r != 0 {
		a.buf = append(a.buf, 0x40|r)
	}
}

// modrmReg 落 "reg 对 rm" 的 modrm（mod=11）。
//
// en: modrmReg emits the modrm for "reg against rm" (mod=11).
func (a *Asm) modrmReg(reg, rm int) { a.buf = append(a.buf, 0xC0|byte(reg&7)<<3|byte(rm&7)) }

// modrmMem 落 reg 对 [base+disp] 的 modrm。reg 字段一定要拼进去，
// 漏了它每条"寄存器对内存"都会退化成 rax，错得极隐蔽。
//
// rsp 与 r12 当基址要额外的 SIB 字节，本编码器不做——硬发出去是一条静默错误的指令，
// 所以在这里直接 panic。发射器一律拿 rbp 当帧基，碰不到这条路。
//
// en: modrmMem emits the modrm for reg against [base+disp]. The reg field must be assembled in:
// leaving it out silently turns every register-against-memory instruction into one that uses
// rax, an extremely well-hidden error. rsp and r12 as a base need an extra SIB byte, which this
// encoder does not do - emitting one anyway would be a silently wrong instruction, so it panics
// right here. The emitter always uses rbp as the frame base and never reaches this path.
func (a *Asm) modrmMem(reg, base int, disp int32) {
	if base == RSP || base == R12 {
		panic("modrmMem：rsp／r12 当基址要 SIB 字节，这个编码器不支持")
	}
	r := byte(reg&7) << 3
	switch {
	case disp == 0 && base != RBP && base != R13:
		a.buf = append(a.buf, r|byte(base&7))
	case disp >= -128 && disp <= 127:
		a.buf = append(a.buf, 0x40|r|byte(base&7), byte(disp))
	default:
		a.buf = append(a.buf, 0x80|r|byte(base&7))
		a.u32(uint32(disp))
	}
}

// regReg 是"两个寄存器"型指令的通用落法。
//
// en: regReg is the general emission for two-register instructions.
func (a *Asm) regReg(op []byte, w bool, reg, rm int) {
	a.rex(w, reg, 0, rm)
	a.w(op...)
	a.modrmReg(reg, rm)
}

// regMem 是"寄存器 对 [base+disp]"型指令的通用落法。
//
// en: regMem is the general emission for "register against [base+disp]" instructions.
func (a *Asm) regMem(op []byte, w bool, reg, base int, disp int32) {
	a.rex(w, reg, 0, base)
	a.w(op...)
	a.modrmMem(reg, base, disp)
}

// ---- 通用寄存器 ----

// Mov 落 dst = src。
//
// en: Mov emits dst = src.
func (a *Asm) Mov(dst, src int) { a.regReg([]byte{0x89}, true, src, dst) }

// MovImm 落 dst = 立即数；32 位装得下就符号扩展，否则 movabs。
//
// en: MovImm emits dst = an immediate; a value that fits in 32 bits is sign-extended, anything
// else takes movabs.
func (a *Asm) MovImm(dst int, v int64) {
	if v >= -0x80000000 && v <= 0x7fffffff {
		a.rex(true, 0, 0, dst)
		a.w(0xC7)
		a.modrmReg(0, dst)
		a.u32(uint32(int32(v)))
		return
	}
	a.rex(true, 0, 0, dst)
	a.w(0xB8 | byte(dst&7))
	a.u64(uint64(v))
}

// MovLoad 落 dst = *[base+disp]。
//
// en: MovLoad emits dst = *[base+disp].
func (a *Asm) MovLoad(dst, base int, disp int32) { a.regMem([]byte{0x8B}, true, dst, base, disp) }

// MovStore 落 *[base+disp] = src。
//
// en: MovStore emits *[base+disp] = src.
func (a *Asm) MovStore(base int, disp int32, src int) {
	a.regMem([]byte{0x89}, true, src, base, disp)
}

// MovImmMem 落 *[base+disp] = 立即数（只走 32 位那一版，装不下的调用方自己别走这条）。
//
// 有了它，"往帧槽里写一个常量"从两条变一条：从前是 `mov rax, 常量` ＋ `mov [rbp−8k], rax`。
// 这一步在清零那一处用得最多（每一个槽登记的持有者开场清一次、归还后再清一次），省下的是一条
// uop 与八个字节的码。调用方要自己保证 v 在 int32 里——超出就别走这条，改回两条的形式。
//
// en: MovImmMem emits *[base+disp] = an immediate, using only the 32-bit form - a caller whose
// value does not fit must not take this path. With it, "write a constant into a frame slot"
// costs one instruction instead of two, which used to be `mov rax, constant` plus `mov
// [rbp-8k], rax`. The zeroing step uses it most (each slot's registered holder is cleared once
// at the start and once after it is handed back), and what is saved is one uop and eight bytes
// of code. The caller must guarantee v fits in int32 - otherwise go back to the two-instruction
// form.
func (a *Asm) MovImmMem(base int, disp int32, v int32) {
	a.rex(true, 0, 0, base)
	a.w(0xC7)
	a.modrmMem(0, base, disp)
	a.u32(uint32(v))
}

// MovZX8Reg 落 dst = 零扩展的 dst 低字节（movzx r64, r8）。
// setcc 只写最低一个字节，上面那 63 位是残留值，要靠它清掉——
// 不能用先 xor 清零：那条会覆盖 setcc 要读的标志位。
//
// en: MovZX8Reg emits dst = the zero-extended low byte of dst (movzx r64, r8). setcc writes
// only the lowest byte and leaves the other 63 bits as residue, which this clears - and it
// cannot be done by xor-zeroing first, because that xor overwrites the flags setcc is about to
// read.
func (a *Asm) MovZX8Reg(dst int) { a.regReg([]byte{0x0F, 0xB6}, true, dst, dst) }

// MovZX8 落 dst = 零扩展的 *[base+disp]，读一个字节（UTF-8 的续字节用）。
//
// en: MovZX8 emits dst = zero-extended *[base+disp], reading one byte (used for UTF-8
// continuation bytes).
func (a *Asm) MovZX8(dst, base int, disp int32) { a.regMem([]byte{0x0F, 0xB6}, true, dst, base, disp) }

// MovZX16 落 dst = 零扩展的 *[base+disp]，读两个字节（宽字符一个单位）。
// 这一条不带 REX.W：`movzx r32, word ptr` 就把 16 位零扩展到 32 位，上面还有 32 位是零。
//
// en: MovZX16 emits dst = zero-extended *[base+disp], reading two bytes (one wide-character
// unit). This one carries no REX.W: `movzx r32, word ptr` zero-extends 16 bits into 32, and the
// upper 32 bits are already zero.
func (a *Asm) MovZX16(dst, base int, disp int32) {
	a.regMem([]byte{0x0F, 0xB7}, false, dst, base, disp)
}

// MovStore32 落 *[base+disp]（四个字节）= src 的低 32 位。
//
// en: MovStore32 emits *[base+disp] (four bytes) = the low 32 bits of src.
func (a *Asm) MovStore32(base int, disp int32, src int) {
	a.regMem([]byte{0x89}, false, src, base, disp)
}

// MovZX32 落 dst = 零扩展的 *[base+disp]，读四个字节。
//
// en: MovZX32 emits dst = zero-extended *[base+disp], reading four bytes.
func (a *Asm) MovZX32(dst, base int, disp int32) { a.regMem([]byte{0x8B}, false, dst, base, disp) }

// MovLoadRIP 落 dst = *[rip+disp32]，返回那个 disp32 的位置，由链接器回填。
//
// en: MovLoadRIP emits dst = *[rip+disp32] and returns the position of that disp32 for the
// linker to fill in.
func (a *Asm) MovLoadRIP(dst int) int { return a.ripOp([]byte{0x8B}, true, dst) }

// MovFromTeb 落 dst = 本线程控制块里 off 那个位置。
//
// 只用来读栈的边界：段前缀 0x65 加 RIP 相对那套不一样，SIB 里 base 与 index 都留"没有"
// （0x25），位移写死，所以这里直接摆字节，不假装它是一种新的内存形式。
// Windows x64 的 NT_TIB 前七格是 8 字节指针：0x00 异常链、0x08 栈基（高位那一端）、
// 0x10 当下提交到的栈下限、0x18 子系统 TIB、0x20 纤维数据、0x28 用户自取指针、
// 0x30 是 TEB 自己（再往后 0x60 是 PEB——那两项是常用参照，能反过来核对这里的号）。
//
// en: MovFromTeb emits dst = the value at offset off in this thread's control block. It is only
// used to read the stack limits: the 0x65 segment prefix does not work like RIP-relative
// addressing, so with both SIB base and index left as "none" (0x25) and a hardcoded
// displacement, the bytes are laid out directly here instead of pretending this is a new memory
// form. The first seven slots of NT_TIB on Windows x64 are 8-byte pointers: 0x00 exception
// chain, 0x08 stack base (the high end), 0x10 the committed stack floor, 0x18 subsystem TIB,
// 0x20 fiber data, 0x28 a user-fetchable pointer, 0x30 the TEB itself (and 0x60 further along
// is the PEB - both commonly used reference points, so they can be used to double-check the
// numbers here).
func (a *Asm) MovFromTeb(dst int, off int32) {
	a.w(0x65)
	a.rex(true, dst, 4, 4) // 4 = "这一格没有"，所以 REX 里只有 W 与 dst 那一位
	a.w(0x8B)
	a.w(byte(dst&7)<<3 | 4) // modrm：mod=00、rm=100 → 后面跟 SIB
	a.w(0x25)               // SIB：index=100（没有）、base=101（没有）→ 位移是绝对地址
	a.u32(uint32(off))
}

// CmpRspRIP 比 rsp 与 *[rip+disp32]，返回那个 disp32 的位置，由链接器回填。
// "栈用完了"那一项检查用的就是它：一次比、一次跳，正常深度下这条跳转不会被执行。
//
// en: CmpRspRIP compares rsp with *[rip+disp32] and returns the position of that disp32 for the
// linker. This is what the "stack exhausted" check uses: one compare, one jump, and at normal
// depth the jump is not taken.
func (a *Asm) CmpRspRIP() int { return a.ripOp([]byte{0x3B}, true, RSP) }

// MovStoreRIP 落 *[rip+disp32] = src。
//
// en: MovStoreRIP emits *[rip+disp32] = src.
func (a *Asm) MovStoreRIP(src int) int { return a.ripOp([]byte{0x89}, true, src) }

// LeaMem 落 dst = &[base+disp]。
//
// en: LeaMem emits dst = &[base+disp].
func (a *Asm) LeaMem(dst, base int, disp int32) { a.regMem([]byte{0x8D}, true, dst, base, disp) }

// LeaRIP 落 dst = &[rip+disp32]。
//
// en: LeaRIP emits dst = &[rip+disp32].
func (a *Asm) LeaRIP(dst int) int { return a.ripOp([]byte{0x8D}, true, dst) }

// LeaLabel 落 dst = 同一段代码里那个标号的地址。
//
// rip 相对取自己的代码地址与取数据地址是同一件事：位移都相对"下一条指令的起点"，
// 所以回填用的是现成那套 rel32，不必新增一种重定位。
//
// en: LeaLabel emits dst = the address of a label inside the same code section. Taking the
// address of one's own code RIP-relative and taking the address of data are the same thing: the
// displacement is relative to the start of the next instruction, so backfilling uses the
// existing rel32 machinery and no new relocation type is needed.
func (a *Asm) LeaLabel(dst int, l Label) {
	a.rex(true, dst, 0, 0)
	a.w(0x8D)
	a.buf = append(a.buf, byte(dst&7)<<3|5)
	a.rel32(l)
}

func (a *Asm) ripOp(op []byte, w bool, reg int) int {
	a.rex(w, reg, 0, 0)
	a.w(op...)
	a.buf = append(a.buf, byte(reg&7)<<3|5)
	at := len(a.buf)
	a.u32(0)
	return at
}

// 二元算绪：dst = dst op src。
//
// en: The binary operations: dst = dst op src.
func (a *Asm) Add(dst, src int) { a.regReg([]byte{0x01}, true, src, dst) }
func (a *Asm) Sub(dst, src int) { a.regReg([]byte{0x29}, true, src, dst) }
func (a *Asm) And(dst, src int) { a.regReg([]byte{0x21}, true, src, dst) }
func (a *Asm) Or(dst, src int)  { a.regReg([]byte{0x09}, true, src, dst) }
func (a *Asm) Xor(dst, src int) { a.regReg([]byte{0x31}, true, src, dst) }
func (a *Asm) Cmp(dst, src int) { a.regReg([]byte{0x39}, true, src, dst) }

// Imul 落 dst *= src（有符号，低 64 位）。
//
// en: Imul emits dst *= src (signed, low 64 bits).
func (a *Asm) Imul(dst, src int) { a.regReg([]byte{0x0F, 0xAF}, true, dst, src) }

// AddImm 落 dst += 立即数。
//
// en: AddImm emits dst += an immediate.
func (a *Asm) AddImm(dst int, v int32) {
	a.rex(true, 0, 0, dst)
	if v >= -128 && v <= 127 {
		a.w(0x83)
		a.modrmReg(0, dst)
		a.buf = append(a.buf, byte(v))
		return
	}
	a.w(0x81)
	a.modrmReg(0, dst)
	a.u32(uint32(v))
}

// CmpImm 落 dst 与立即数比较。
//
// en: CmpImm emits a comparison of dst against an immediate.
func (a *Asm) CmpImm(dst int, v int32) {
	a.rex(true, 0, 0, dst)
	if v >= -128 && v <= 127 {
		a.w(0x83)
		a.modrmReg(7, dst)
		a.buf = append(a.buf, byte(v))
		return
	}
	a.w(0x81)
	a.modrmReg(7, dst)
	a.u32(uint32(v))
}

// 一元：F7 /2 = NOT、/3 = NEG、/4 = MUL(无符号)、/5 = IMUL(单操作数)、/6 = DIV、/7 = IDIV。
//
// en: The unary group: F7 /2 = NOT, /3 = NEG, /4 = MUL (unsigned), /5 = IMUL (single operand),
// /6 = DIV, /7 = IDIV.
func (a *Asm) Not(dst int)  { a.unary(2, dst) }
func (a *Asm) Neg(dst int)  { a.unary(3, dst) }
func (a *Asm) Mul1(dst int) { a.unary(4, dst) }
func (a *Asm) Div1(dst int) { a.unary(6, dst) }
func (a *Asm) Idiv(dst int) { a.unary(7, dst) }

// Test 落 flags，用来判零而不改值。
//
// en: Test sets flags, used to check for zero without changing the value.
func (a *Asm) Test(dst, src int) { a.regReg([]byte{0x85}, true, src, dst) }

// MovLoad8 与 MovStore8 走一个字节（UTF-8 的逐字节拼装用）。
//
// en: MovLoad8 and MovStore8 move one byte (used to assemble UTF-8 byte by byte).
func (a *Asm) MovLoad8(dst, base int, disp int32) { a.regMem([]byte{0x8A}, false, dst, base, disp) }
func (a *Asm) MovStore8(base int, disp int32, src int) {
	a.regMem([]byte{0x88}, false, src, base, disp)
}

func (a *Asm) unary(opReg, dst int) {
	a.rex(true, opReg, 0, dst)
	a.w(0xF7)
	a.modrmReg(opReg, dst)
}

// Cqo 把 rax 的符号扩到 rdx:rax，给有符号除法做准备。
//
// en: Cqo sign-extends rax into rdx:rax in preparation for signed division.
func (a *Asm) Cqo() { a.w(0x48, 0x99) }

// AndImm 与 OrImm 走 81 /op imm32：0x80 这类掩码当 imm8 会被符号扩展，只能走 32 位形式。
//
// en: AndImm and OrImm take 81 /op imm32: a mask that looks like it fits in imm8 would be
// sign-extended by the 0x80 forms, so only the 32-bit form is safe.
func (a *Asm) AndImm(dst int, v int32) { a.immArith(4, dst, v) }
func (a *Asm) OrImm(dst int, v int32)  { a.immArith(1, dst, v) }

func (a *Asm) immArith(opReg, dst int, v int32) {
	a.rex(true, opReg, 0, dst)
	a.w(0x81)
	a.modrmReg(opReg, dst)
	a.u32(uint32(v))
}

// 定量移位走 C1 /op imm8。
//
// en: Shifts by a constant amount take C1 /op imm8.
func (a *Asm) ShlImm(dst int, n byte) { a.shiftImm(4, dst, n) }
func (a *Asm) ShrImm(dst int, n byte) { a.shiftImm(5, dst, n) }
func (a *Asm) SarImm(dst int, n byte) { a.shiftImm(7, dst, n) }

func (a *Asm) shiftImm(opReg, dst int, n byte) {
	a.rex(true, opReg, 0, dst)
	a.w(0xC1)
	a.modrmReg(opReg, dst)
	a.buf = append(a.buf, n)
}

// Shift 走 cl：dst <<= cl、dst >>= cl（逻辑）、dst ~= cl（算术）。
//
// en: Shift takes cl: dst <<= cl, dst >>= cl (logical), dst ~= cl (arithmetic).
func (a *Asm) Shl(dst int) { a.shift(4, dst) }
func (a *Asm) Shr(dst int) { a.shift(5, dst) }
func (a *Asm) Sar(dst int) { a.shift(7, dst) }

func (a *Asm) shift(opReg, dst int) {
	a.rex(true, opReg, 0, dst)
	a.w(0xD3)
	a.modrmReg(opReg, dst)
}

// Push 与 Pop。
//
// en: Push and Pop.
func (a *Asm) Push(r int) {
	if high(r) != 0 {
		a.w(0x41)
	}
	a.w(0x50 | byte(r&7))
}

func (a *Asm) Pop(r int) {
	if high(r) != 0 {
		a.w(0x41)
	}
	a.w(0x58 | byte(r&7))
}

// ---- 控制流 ----

func (a *Asm) Jmp(l Label) { a.w(0xE9); a.rel32(l) }

func (a *Asm) Jcc(c Cond, l Label) {
	a.w(0x0F, 0x80|byte(c))
	a.rel32(l)
}

func (a *Asm) Call(l Label) { a.w(0xE8); a.rel32(l) }
func (a *Asm) Ret()         { a.w(0xC3) }

// Nop 占一字节，对齐或撑位置用。
//
// en: Nop occupies one byte, for alignment or padding.
func (a *Asm) Nop() { a.w(0x90) }

// CallReg 与 JmpReg 走 FF /2、/4，用来跳进 IAT 里那个地址。
//
// en: CallReg and JmpReg take FF /2 and /4, used to jump into the address stored in an IAT
// slot.
func (a *Asm) CallReg(r int) {
	a.rex(false, 2, 0, r)
	a.w(0xFF)
	a.modrmReg(2, r)
}

func (a *Asm) JmpReg(r int) {
	a.rex(false, 4, 0, r)
	a.w(0xFF)
	a.modrmReg(4, r)
}

func (a *Asm) rel32(l Label) {
	a.pending = append(a.pending, fixup{kind: fixRel32, at: len(a.buf), target: l})
	a.u32(0)
}

// Setcc 落 dst 的最低字节 = 条件是否成立（r8b～r15b 要 REX）。
//
// en: Setcc emits the low byte of dst = whether the condition holds (r8b through r15b need a
// REX prefix).
func (a *Asm) Setcc(c Cond, dst int) {
	a.rex(false, 0, 0, dst)
	a.w(0x0F, 0x90|byte(c))
	a.buf = append(a.buf, 0xC0|byte(dst&7))
}

// ---- SSE2：float 与装箱值都走这里 ----
//
// 字节顺序是 legacy 前缀（F2／66）→ REX → 操作码，反过来排列 CPU 不识别。
// 只有 cvtsi2sd 与 cvttsd2si 要 REX.W（另一侧是 64 位通用寄存器）；
// double 之间的那些带了就是多余甚至未定义，所以一律不给。
//
// en: SSE2 - float and boxed values all go through here. Byte order is the legacy prefix
// (F2/66), then REX, then the opcode; the other way round the CPU does not recognize it. Only
// cvtsi2sd and cvttsd2si take REX.W, because the other side is a 64-bit general-purpose
// register; for the double-to-double instructions it is redundant or even undefined, so none of
// them gets it.

// MovdqaLoad 落 xmm dst = *[base+disp] 那 16 个字节，并且**要求地址 16 字节对齐**，
// 不齐就当场异常。发射器拿它当栈对齐的探针用。
//
// en: MovdqaLoad emits xmm dst = the 16 bytes at *[base+disp] and REQUIRES the address to be
// 16-byte aligned; misaligned it faults immediately. The emitter uses it as a probe for stack
// alignment.
func (a *Asm) MovdqaLoad(dst, base int, disp int32) {
	a.w(0x66)
	a.rex(false, dst, 0, base)
	a.w(0x0F, 0x6F)
	a.modrmMem(dst, base, disp)
}

// MovsdLoad 落 xmm dst = *[base+disp] 里那个 double。
//
// en: MovsdLoad emits xmm dst = the double at *[base+disp].
func (a *Asm) MovsdLoad(dst, base int, disp int32) {
	a.w(0xF2)
	a.rex(false, dst, 0, base)
	a.w(0x0F, 0x10)
	a.modrmMem(dst, base, disp)
}

// MovsdStore 落 *[base+disp] = xmm src。
//
// en: MovsdStore emits *[base+disp] = xmm src.
func (a *Asm) MovsdStore(base int, disp int32, src int) {
	a.w(0xF2)
	a.rex(false, src, 0, base)
	a.w(0x0F, 0x11)
	a.modrmMem(src, base, disp)
}

// MovsdReg 落 xmm dst = xmm src。
//
// en: MovsdReg emits xmm dst = xmm src.
func (a *Asm) MovsdReg(dst, src int) {
	a.w(0xF2)
	a.rex(false, dst, 0, src)
	a.w(0x0F, 0x10)
	a.modrmReg(dst, src)
}

// MovsdFromGPR 把通用寄存器的 64 位搬进 xmm 低 64 位（装箱）。
//
// en: MovsdFromGPR moves the 64 bits of a general-purpose register into the low 64 bits of an
// xmm register (boxing).
func (a *Asm) MovsdFromGPR(dst, src int) {
	a.w(0x66)
	a.rex(true, dst, 0, src)
	a.w(0x0F, 0x6E)
	a.modrmReg(dst, src)
}

// MovsdToGPR 是反方向（拆盒）：r/m64 接 xmm 那 64 位。
// 这一条的位置极易记反——66 前缀是"r/m64 ← xmm"（reg 字段是来源 xmm），
// F3 前缀才是"xmm ← r/m64"（reg 字段是要写的 xmm），前缀与 reg／rm 两处任一抄反，
// 指令就变成把 rax 写进 xmm，而且没有任何报错。
//
// en: MovsdToGPR is the opposite direction (unboxing): r/m64 receives the 64 bits from xmm.
// This encoding is extremely easy to remember backwards: the 66 prefix means "r/m64 <- xmm"
// (the reg field is the source xmm), while the F3 prefix is the one that means "xmm <- r/m64"
// (the reg field is the xmm being written). Get either the prefix or the reg/rm assignment
// wrong and the instruction writes rax into an xmm register, with no error reported.
func (a *Asm) MovsdToGPR(dst, src int) {
	a.w(0x66)
	a.rex(true, src, 0, dst)
	a.w(0x0F, 0x7E)
	a.modrmReg(src, dst)
}

// 浮点四则：dst = dst op src。
//
// en: Float arithmetic: dst = dst op src.
func (a *Asm) Addsd(dst, src int) { a.sse(0x58, dst, src) }
func (a *Asm) Subsd(dst, src int) { a.sse(0x5C, dst, src) }
func (a *Asm) Mulsd(dst, src int) { a.sse(0x59, dst, src) }
func (a *Asm) Divsd(dst, src int) { a.sse(0x5E, dst, src) }

// Sqrtsd 落 dst = √src（F2 0F 51——与四则同一族，只是操作码 0x51）。
// 参数为负不算它失败：它按 IEEE 返回 NaN。
//
// en: Sqrtsd emits dst = sqrt(src) (F2 0F 51 - the same family as the four operations, only
// with opcode 0x51). A negative argument does not make it fail: it returns NaN per IEEE.
func (a *Asm) Sqrtsd(dst, src int) { a.sse(0x51, dst, src) }

// ---- x87 那几条 ----
//
// x86-64 没有一条计算正弦的 SSE 指令：超越函数只有 x87 的 fsin／fcos／fyl2x。
// 小数在内存与 FPU 栈之间各走一次拷贝，值本身不变（两边都是 IEEE 双精度），
// 参数归约与特殊值（±无穷、NaN）由硬件按 x87 的规则处理。
//
// en: The x87 instructions - x86-64 has no single SSE instruction that computes sine: the
// transcendental functions exist only as x87 fsin/fcos/fyl2x. A double is copied once between
// memory and the FPU stack and its value does not change (both sides are IEEE double
// precision); argument reduction and the special values (±infinity, NaN) are handled by the
// hardware according to x87's rules.

// FldDouble 是 DD /0：fld qword ptr [base+disp]，把内存里那一个双精度压上 FPU 栈。
//
// en: FldDouble is DD /0: fld qword ptr [base+disp], pushing the double precision value in
// memory onto the FPU stack.
func (a *Asm) FldDouble(base int, disp int32) { a.regMem([]byte{0xDD}, false, 0, base, disp) }

// FstpDouble 是 DD /3：fstp qword ptr [base+disp]，落进内存再把栈上那个位置弹掉。
//
// en: FstpDouble is DD /3: fstp qword ptr [base+disp], storing into memory and popping the FPU
// stack slot.
func (a *Asm) FstpDouble(base int, disp int32) { a.regMem([]byte{0xDD}, false, 3, base, disp) }

// 下面这几条不收内存操作数，整个编码就是两个字节。
// x87 那几条压常数（fldl2e、fldpi…）这一版不用：算自然对数要的 ln(2) 从内存那一侧
// 搬进 xmm 就行，少留一条只能靠肉眼比对来确认的编码。
//
// en: The instructions below take no memory operand, so their whole encoding is two bytes. The
// x87 constant pushes (fldl2e, fldpi ...) are not used by this version: ln(2) for the natural
// logarithm is moved into an xmm from memory instead, which leaves one fewer encoding that
// could only ever be confirmed by eye.
func (a *Asm) Fsin()  { a.w(0xD9, 0xFE) } // ST(0) = sin(ST(0))
func (a *Asm) Fcos()  { a.w(0xD9, 0xFF) } // ST(0) = cos(ST(0))
func (a *Asm) Fyl2x() { a.w(0xD9, 0xF1) } // ST(1) = ST(1) × log₂(ST(0))，然后 pop
func (a *Asm) Fpatan() {
	a.w(0xD9, 0xF3) // ST(1) = atan(ST(1)/ST(0))，然后 pop：栈顶那个位置是分母
}
func (a *Asm) F2xm1() { a.w(0xD9, 0xF0) } // ST(0) = 2^ST(0) - 1（只在 -1～1 区间内精确）
func (a *Asm) Fscale() {
	a.w(0xD9, 0xFD) // ST(0) = ST(0) × 2^trunc(ST(1))，ST(1) 留着：补 2 的整数次幂那一步
}
func (a *Asm) FAddP() { a.w(0xDE, 0xC1) } // ST(1) = ST(1) + ST(0)，然后 pop
func (a *Asm) Fld1()  { a.w(0xD9, 0xE8) } // 压一个 1.0

func (a *Asm) sse(op byte, dst, src int) {
	a.w(0xF2)
	a.rex(false, dst, 0, src)
	a.w(0x0F, op)
	a.modrmReg(dst, src)
}

// Xorpd 是 xmm 之间的按位异或：小数取负就是拿 1<<63 那个掩码异或一下。
//
// en: Xorpd is bitwise xor between xmm registers: negating a float is just xor with the 1<<63
// mask.
func (a *Asm) Xorpd(dst, src int) {
	a.w(0x66)
	a.rex(false, dst, 0, src)
	a.w(0x0F, 0x57)
	a.modrmReg(dst, src)
}

// Ucomisd 落 flags：无符号比较，NaN 走"无序"那一侧（IEEE 的无序语义）。
//
// 操作码是 66 0F B2——B4／B5 那两条在 64 位模式下根本不存在（它们是 32 位的 LES／LSS），
// 抄错一个字节就是"编译期一切正常、一跑到小数比较就当非法指令"。
//
// 这一条目前没有被使用：这台机器执行它照样报非法指令，小数的比较改走 @f_order 的整数位型那一条。
//
// en: Ucomisd sets flags: an unsigned comparison where NaN takes the "unordered" branch
// (IEEE's unordered semantics). The opcode is 66 0F B2 - B4/B5 simply do not exist in 64-bit
// mode (they are the 32-bit LES/LSS), so one wrong byte means "compiles perfectly, then faults
// with an illegal instruction the moment a float comparison runs". This instruction is
// currently unused: on this machine executing it still reports an illegal instruction, so float
// comparisons go through @f_order's integer bit-pattern route instead.
func (a *Asm) Ucomisd(l, r int) {
	a.w(0x66)
	a.rex(false, l, 0, r)
	a.w(0x0F, 0xB2)
	a.modrmReg(l, r)
}

// CvtSI2SD 落 xmm dst = double(src)，src 是通用寄存器里的有符号整数。
//
// en: CvtSI2SD emits xmm dst = double(src), where src is a signed integer in a general-purpose
// register.
func (a *Asm) CvtSI2SD(dst, src int) {
	a.w(0xF2)
	a.rex(true, dst, 0, src)
	a.w(0x0F, 0x2A)
	a.modrmReg(dst, src)
}

// CvtTSD2SI 落 dst = 向零截断的整数(src)（除法向零截断也借它）。
//
// en: CvtTSD2SI emits dst = integer(src) truncated toward zero (the truncate-toward-zero
// division borrows it too).
func (a *Asm) CvtTSD2SI(dst, src int) {
	a.w(0xF2)
	a.rex(true, dst, 0, src)
	a.w(0x0F, 0x2C)
	a.modrmReg(dst, src)
}

// Roundsd 落 dst = 按 mode 取整(src)：0 就近偶数、1 向下、2 向上、3 向零。
//
// en: Roundsd emits dst = src rounded by mode: 0 nearest/even, 1 down, 2 up, 3 toward zero.
func (a *Asm) Roundsd(dst, src int, mode byte) {
	a.w(0x66)
	a.rex(false, dst, 0, src)
	a.w(0x0F, 0x3A, 0x0B)
	a.modrmReg(dst, src)
	a.buf = append(a.buf, mode)
}

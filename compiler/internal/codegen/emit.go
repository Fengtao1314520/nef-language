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

package codegen

import (
	"fmt"
	"strings"

	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/x64"
)

// 发射器：中间码 → x86-64。
//
// 窄模型，有意不做寄存器分配：每个值都放在帧槽里，要算就搬进 rax／rcx，
// 调用时前四个实参依次进 rcx、rdx、r8、r9（浮点进 xmm0～3），第五个起进本帧的出参区。
// 搬参数用的临时寄存器是 rax，它不是参数寄存器，所以按顺序搬不会互相覆盖。
//
// en: The emitter: intermediate code to x86-64, following the calling convention.
// A narrow model that deliberately does no register allocation: every value lives in a frame
// slot and moves into rax/rcx for arithmetic; the first four arguments go in rcx, rdx, r8, r9
// (floats in xmm0-3), the fifth and beyond in the outgoing-argument area of the frame. rax is
// the scratch register for moving arguments; it is not an argument register, so placing
// arguments in order cannot overwrite each other.

// 参数寄存器，顺序即调用约定规定的顺序。
//
// en: The argument registers, in the order fixed by the calling convention.
var intArgs = []int{x64.RCX, x64.RDX, x64.R8, x64.R9}

// signBitMask 是小数取负要翻的那一位：IEEE 双精度的符号位。
// 写成 -1<<63 而不是 1<<63——后者作为常量放进 int64 会溢出报错。
//
// en: signBitMask is the bit a float negation flips: the sign bit of an IEEE double. Written as
// -1<<63 rather than 1<<63, because the latter overflows when stored as an int64 constant.
var signBitMask int64 = -1 << 63

// Emit 把整个程序落成机器码，返回入口在代码段里的偏移。
//
// en: Emit turns the whole program into machine code and returns the entry point's offset
// inside the code section.
func Emit(p *ir.Prog, l *Linker, d *DataLayout) (int, error) {
	e := &em{l: l, a: l.Asm(), d: d, fn: map[string]x64.Label{}, lab: map[key]x64.Label{}}
	for _, f := range p.Funcs {
		if strings.HasPrefix(f.Name, "@") {
			return 0, fmt.Errorf("函数名 %q 以 @ 开头，那是运行期例程的命名空间", f.Name)
		}
		e.fn[f.Name] = l.Label()
	}
	for i, f := range p.Funcs {
		if err := e.function(f, int32(i), f.Name == p.Entry); err != nil {
			return 0, fmt.Errorf("%s：%w", f.Name, err)
		}
	}
	entry, ok := e.fn[p.Entry]
	if !ok {
		return 0, fmt.Errorf("入口 %q 不存在", p.Entry)
	}
	off, ok := e.a.OffsetOf(entry)
	if !ok {
		return 0, fmt.Errorf("入口没落出位置")
	}
	return off, nil
}

// key 是一个函数内的标号：ir 的标号按函数各自编号，跨函数会重号，所以要带函数编号一起当键。
//
// en: key is a label inside one function: ir numbers labels per function, so the same number
// appears in different functions and the key has to carry the function number too.
type key struct {
	fn  int32
	lab ir.Label
}

type em struct {
	l     *Linker
	a     *x64.Asm
	d     *DataLayout
	fn    map[string]x64.Label
	lab   map[key]x64.Label
	cur   int32
	slots int // 当前函数的帧槽数，摆第五个及以后的实参要用
	// maxOut 是当前函数所有调用里"第五个及以后"实参的最大个数：帧底按它留槽位，
	// 出参才不会从下面盖进自己的槽区；早先只留一格，会盖进去。
	maxOut int
}

// maxOutOf 扫一遍函数体里的调用，数出第五个及以后的实参最多要几格。
//
// en: maxOutOf scans the function body and returns the largest number of arguments beyond the
// fourth in any single call.
func maxOutOf(f *ir.Func) int {
	m := 0
	for _, in := range f.Body {
		if c, ok := in.(*ir.Call); ok {
			if n := len(c.Args) - 4; n > m {
				m = n
			}
		}
	}
	return m
}

func (e *em) function(f *ir.Func, id int32, entry bool) error {
	e.a.Mark(e.fn[f.Name])
	e.cur = id
	e.slots = f.Slots
	e.maxOut = maxOutOf(f)
	Prologue(e.a, f.Slots, e.maxOut)
	if entry {
		// 栈底要在第一帧之前就算好：往后每一帧开场都比它一次。
		e.l.StackFloorSet(e.d.StackFloor)
	}
	e.l.StackGuard(e.d.StackFloor)
	// 实参还在 rcx／rdx／r8／r9 与 xmm0～3 里，第一步就搬进本帧的槽。
	// 静态槽与字面量地址不能当被调方的参数落点，所以这里只可能写回帧槽。
	for i := 0; i < f.Params; i++ {
		if i < 4 {
			if i < len(f.PT) && f.PT[i] == ir.TFlt {
				e.a.MovsdStore(x64.RBP, slotOff(i), i)
				continue
			}
			e.a.MovStore(x64.RBP, slotOff(i), intArgs[i])
			continue
		}
		e.a.MovLoad(x64.RAX, x64.RBP, inArgOff(i+1))
		e.a.MovStore(x64.RBP, slotOff(i), x64.RAX)
	}
	for _, in := range f.Body {
		if err := e.inst(f, in); err != nil {
			return err
		}
	}
	// 函数体走完没遇到 ret 也安全：返回一个零值，收尾照常释放这一层的值。
	Epilogue(e.a, f.Slots, e.maxOut)
	e.a.Ret()
	return nil
}

// label 拿（必要时新建）一个函数内标号在 x64 里对应的那一个。
//
// en: label takes - creating one if needed - the x64 label corresponding to a given ir label
// inside a function.
func (e *em) label(f *ir.Func, l ir.Label) x64.Label {
	k := key{e.cur, l}
	if got, ok := e.lab[k]; ok {
		return got
	}
	got := e.l.Label()
	e.lab[k] = got
	return got
}

func (e *em) inst(f *ir.Func, in ir.Inst) error {
	switch x := in.(type) {
	case *ir.Lbl:
		e.a.Mark(e.label(f, x.At))

	case *ir.Dbg:
		// 一条检查点＝四条指令：号与这一帧的 RBP 交给宿主，再照数据段里那个位置的地址 call 过去。
		// 停在语句边界上，寄存器里没有活值（临时量一律落槽），rax／rcx／rdx 与 xmm 全被宿主
		// 用掉也不要紧；那 32 字节的影子空间在帧里本来就留着（frame.go 的 shadowBytes）。
		e.l.noteSite(Site{ID: x.ID, Func: f.Name, Offset: e.a.PC(), File: x.File, Line: x.Line, Col: x.Col})
		e.a.MovImm(x64.RCX, int64(x.ID))
		e.a.Mov(x64.RDX, x64.RBP)
		e.l.LoadGlobal(x64.RAX, e.d.DbgHost)
		e.a.CallReg(x64.RAX)

	case *ir.IfOverflow:
		// 紧跟在 add／sub／imul 后面：OF 置了就跳去抛 10021 号错误。
		e.a.Jcc(x64.O, e.label(f, x.To))

	case *ir.Jump:
		e.a.Jmp(e.label(f, x.To))

	case *ir.Jz:
		if err := e.into(x64.RAX, x.V); err != nil {
			return err
		}
		e.a.Test(x64.RAX, x64.RAX)
		e.a.Jcc(x64.E, e.label(f, x.To))

	case *ir.Jnz:
		if err := e.into(x64.RAX, x.V); err != nil {
			return err
		}
		e.a.Test(x64.RAX, x64.RAX)
		e.a.Jcc(x64.NE, e.label(f, x.To))

	case *ir.Mov:
		return e.mov(x.D, x.S, x.T)

	case *ir.Neg:
		if x.T == ir.TFlt {
			// 取负就是翻符号位：与符号位掩码异或一次，省掉一条浮点减法。
			if err := e.intoX(0, x.S); err != nil {
				return err
			}
			e.a.MovImm(x64.RAX, signBitMask)
			e.a.MovsdFromGPR(1, x64.RAX)
			e.a.Xorpd(0, 1)
			return e.storeX(x.D, 0)
		}
		if err := e.into(x64.RAX, x.S); err != nil {
			return err
		}
		e.a.Neg(x64.RAX)
		return e.store(x.D, x64.RAX)

	case *ir.Not:
		if err := e.into(x64.RAX, x.S); err != nil {
			return err
		}
		e.a.Not(x64.RAX)
		return e.store(x.D, x64.RAX)

	case *ir.LNot:
		if err := e.into(x64.RAX, x.S); err != nil {
			return err
		}
		e.a.Test(x64.RAX, x64.RAX)
		return e.setFlag(x.D, x64.E)

	case *ir.Bin:
		return e.bin(x)

	case *ir.Cmp:
		return e.cmp(x)

	case *ir.Br:
		return e.br(f, x)

	case *ir.Cvt:
		return e.cvt(x)

	case *ir.Load:
		return e.load(x)

	case *ir.Store:
		return e.store_(x)

	case *ir.Addr:
		e.l.LeaGlobal(x64.RAX, e.d.Global[x.G])
		return e.store(x.D, x64.RAX)

	case *ir.Call:
		return e.call(x)

	case *ir.PushHandler:
		e.a.LeaLabel(x64.RCX, e.label(f, x.Catch))
		e.a.Call(e.l.RtLabel("@push_handler"))

	case *ir.PushAbandon:
		// 清理记录放在这一帧上：rdx 取那连续五个槽的头一个，rcx 取本函数的帧内清理记录开头。
		// 垫子与接住块都是这个函数里的标号，运行期只知道地址。
		e.a.LeaMem(x64.RDX, x64.RBP, slotOff(int(x.Rec)))
		e.a.LeaLabel(x64.RCX, e.label(f, x.Pad))
		e.a.Call(e.l.RtLabel("@push_abandon"))

	case *ir.PopHandler:
		e.a.Call(e.l.RtLabel("@pop_handler"))

	case *ir.Catch:
		// @throw 把码留在 rax、话留在 rdx，这里把它们摆进接住块的两个局部量
		return e.store2(x.D, x64.RAX, x.E, x64.RDX)

	case *ir.Ret:
		if x.Has {
			if x.T == ir.TFlt {
				if err := e.intoX(0, x.D); err != nil {
					return err
				}
			} else if err := e.into(x64.RAX, x.D); err != nil {
				return err
			}
		}
		Epilogue(e.a, f.Slots, e.maxOut)
		e.a.Ret()

	default:
		return fmt.Errorf("发射器还不认这条中间码 %T", in)
	}
	return nil
}

func (e *em) mov(d, s ir.Val, t ir.T) error {
	if t == ir.TFlt {
		if err := e.intoX(0, s); err != nil {
			return err
		}
		return e.storeX(d, 0)
	}
	// 往帧槽里写一个 32 位装得下的常量：一条 `mov qword [rbp−8k], 常量` 就够，
	// 不必先搬进 rax 再写过去（清零的那两处最常走这条：开场置一次、归还后再置一次）。
	if slot, ok := d.(ir.L); ok {
		if v, ok := s.(ir.K); ok && v >= -0x80000000 && v <= 0x7fffffff {
			e.a.MovImmMem(x64.RBP, slotOff(int(slot)), int32(v))
			return nil
		}
	}
	if err := e.into(x64.RAX, s); err != nil {
		return err
	}
	return e.store(d, x64.RAX)
}

func (e *em) bin(x *ir.Bin) error {
	if x.T == ir.TFlt {
		if err := e.intoX(0, x.A); err != nil {
			return err
		}
		if err := e.intoX(1, x.B); err != nil {
			return err
		}
		switch x.Op {
		case "+":
			e.a.Addsd(0, 1)
		case "-":
			e.a.Subsd(0, 1)
		case "*":
			e.a.Mulsd(0, 1)
		case "/":
			e.a.Divsd(0, 1)
		case "%":
			e.a.MovsdToGPR(x64.RCX, 0)
			e.a.MovsdToGPR(x64.RDX, 1)
			e.a.Call(e.l.RtLabel("@fmod"))
			e.a.MovsdFromGPR(0, x64.RAX)
		default:
			return fmt.Errorf("小数没有 %s 这个算绪", x.Op)
		}
		return e.storeX(x.D, 0)
	}
	if err := e.into(x64.RAX, x.A); err != nil {
		return err
	}
	if err := e.into(x64.RCX, x.B); err != nil {
		return err
	}
	dst := x64.RAX
	switch x.Op {
	case "+":
		e.a.Add(x64.RAX, x64.RCX)
	case "-":
		e.a.Sub(x64.RAX, x64.RCX)
	case "*":
		e.a.Imul(x64.RAX, x64.RCX)
	case "/":
		e.a.Cqo()
		e.a.Idiv(x64.RCX)
	case "%":
		e.a.Cqo()
		e.a.Idiv(x64.RCX)
		dst = x64.RDX
	case "&":
		e.a.And(x64.RAX, x64.RCX)
	case "|":
		e.a.Or(x64.RAX, x64.RCX)
	case "^":
		e.a.Xor(x64.RAX, x64.RCX)
	case "<<":
		e.a.Shl(x64.RAX)
	case ">>":
		e.a.Sar(x64.RAX)
	case ">>>":
		e.a.Shr(x64.RAX)
	default:
		return fmt.Errorf("发射器不认整数算绪 %q", x.Op)
	}
	return e.store(x.D, dst)
}

// condFor 把一个比较运算符换成 x64 的条件码（有符号）。
//
// en: condFor turns a comparison operator into an x64 condition code (signed).
func condFor(op string) (x64.Cond, error) {
	switch op {
	case "==":
		return x64.E, nil
	case "!=":
		return x64.NE, nil
	case "<":
		return x64.L, nil
	case "<=":
		return x64.LE, nil
	case ">":
		return x64.G, nil
	case ">=":
		return x64.GE, nil
	}
	return 0, fmt.Errorf("认不出的比较 %q", op)
}

func (e *em) cmp(x *ir.Cmp) error {
	if x.T == ir.TFlt {
		c, err := e.floatFlags(x.A, x.B, x.Op)
		if err != nil {
			return err
		}
		return e.store(x.D, c)
	}
	if err := e.into(x64.RAX, x.A); err != nil {
		return err
	}
	if err := e.into(x64.RCX, x.B); err != nil {
		return err
	}
	c, err := condFor(x.Op)
	if err != nil {
		return err
	}
	e.a.Cmp(x64.RAX, x64.RCX)
	return e.setFlag(x.D, c)
}

// floatFlags 按 IEEE 双精度语义算一个小数比较，结果（0 或 1）在 rdx 里。
//
// 不用 ucomisd：那一条在这台机器上一执行就报非法指令（见 x64.Ucomisd 的注释）。
// 比较交给 @f_order，它按位型返回一个 bitmask——1＝小、2＝等、4＝大、0＝无序，
// 于是六种比较全变成"查几个位、非零就算真"：
//
//	a <  b  →  查 1            a >  b  →  查 4
//	a <= b  →  查 1|2          a >= b  →  查 2|4
//	a == b  →  查 2            a != b  →  查 2 落空（NaN 那一档是 0，两句都自动成立）
//
// 无序那一边 bitmask 是 0，任何"查"都落空，正好符合"NaN 参与大小比较一律为假"的
// IEEE 语义，而 != 走"取反"那一条，NaN != 任何东西还是真。
//
// en: floatFlags computes a float comparison with IEEE double semantics, leaving the
// result (0 or 1) in rdx. ucomisd is not used: on this machine that instruction raises an
// illegal-instruction fault as soon as it executes (see the note on x64.Ucomisd). The
// comparison is delegated to @f_order, which returns a bitmask by bit pattern - 1 = less, 2 =
// equal, 4 = greater, 0 = unordered - so all six comparisons become "test a few bits, non-zero
// means true": a < b tests 1; a > b tests 4; a <= b tests 1|2; a >= b tests 2|4; a == b tests
// 2; a != b tests 2 and negates. The unordered case has bitmask 0, so every test fails, which
// is exactly the required "any ordering comparison involving NaN is false", while != takes
// the negating route so NaN != anything stays true.
func (e *em) floatFlags(a, b ir.Val, op string) (int, error) {
	if err := e.intoX(0, a); err != nil {
		return 0, err
	}
	if err := e.intoX(1, b); err != nil {
		return 0, err
	}
	e.a.MovsdToGPR(x64.RCX, 0)
	e.a.MovsdToGPR(x64.RDX, 1)
	e.a.Call(e.l.RtLabel("@f_order"))
	mask := int64(0)
	switch op {
	case "<":
		mask = 1
	case "<=":
		mask = 1 | 2
	case ">":
		mask = 4
	case ">=":
		mask = 2 | 4
	case "==":
		mask = 2
	case "!=":
		mask = 2
	}
	e.a.MovImm(x64.R10, mask)
	e.a.Test(x64.RAX, x64.R10)
	c := x64.NE
	if op == "!=" {
		c = x64.E
	}
	e.flagByte(x64.RDX, c)
	return x64.RDX, nil
}

func (e *em) br(f *ir.Func, x *ir.Br) error {
	c, err := condFor(x.Op)
	if err != nil {
		return err
	}
	if x.T == ir.TFlt {
		if _, err := e.floatFlags(x.A, x.B, x.Op); err != nil {
			return err
		}
		e.a.Test(x64.RDX, x64.RDX)
		c = x64.NE
	} else {
		if err := e.into(x64.RAX, x.A); err != nil {
			return err
		}
		if err := e.into(x64.RCX, x.B); err != nil {
			return err
		}
		e.a.Cmp(x64.RAX, x64.RCX)
	}
	if x.Neg {
		c = c.Inverse()
	}
	e.a.Jcc(c, e.label(f, x.To))
	return nil
}

// setFlag 把"标志位成立吗"变成一个干净的 0／1 放进 d。
//
// 只能在 setcc 之后用 movzx 把高位清掉。先 xor 清零再 setcc 是错的：
// 那条 xor 会把 setcc 正要读的标志位冲掉，比较结果全部出错。
//
// en: setFlag turns "is this flag set" into a clean 0/1 in d. The only correct way is setcc
// followed by movzx to clear the upper bits: xor-zeroing the register first and then setcc is
// wrong, because that xor wipes the very flags setcc is about to read, and every comparison
// result goes wrong.
func (e *em) setFlag(d ir.Val, c x64.Cond) error {
	e.a.Setcc(c, x64.RDX)
	e.a.MovZX8Reg(x64.RDX)
	return e.store(d, x64.RDX)
}

// flagByte 是 setFlag 的寄存器版：结果留在 reg 里，供后续与、或起来用。
//
// en: flagByte is the register version of setFlag: the result stays in reg so it can be ANDed
// or ORed with others afterwards.
func (e *em) flagByte(reg int, c x64.Cond) {
	e.a.Setcc(c, reg)
	e.a.MovZX8Reg(reg)
}

func (e *em) cvt(x *ir.Cvt) error {
	switch {
	case x.To == ir.TFlt:
		// 先清 xmm0：清 rax 会把刚取来的那个整数一起清掉，转换于是永远得 0.0
		e.a.Xorpd(0, 0)
		if err := e.into(x64.RAX, x.S); err != nil {
			return err
		}
		e.a.CvtSI2SD(0, x64.RAX)
		return e.storeX(x.D, 0)
	default:
		if err := e.intoX(0, x.S); err != nil {
			return err
		}
		e.a.CvtTSD2SI(x64.RAX, 0) // 向零截断
		return e.store(x.D, x64.RAX)
	}
}

func (e *em) load(x *ir.Load) error {
	if err := e.into(x64.RCX, x.Base); err != nil {
		return err
	}
	sz := x.Size
	if sz == 0 {
		sz = 8
	}
	switch sz {
	case 1:
		e.a.MovZX8(x64.RAX, x64.RCX, int32(x.Off))
	case 4:
		e.a.MovZX32(x64.RAX, x64.RCX, int32(x.Off))
	default:
		if x.T == ir.TFlt {
			e.a.MovsdLoad(0, x64.RCX, int32(x.Off))
			return e.storeX(x.D, 0)
		}
		e.a.MovLoad(x64.RAX, x64.RCX, int32(x.Off))
	}
	return e.store(x.D, x64.RAX)
}

func (e *em) store_(x *ir.Store) error {
	if err := e.into(x64.RCX, x.Base); err != nil {
		return err
	}
	if x.T == ir.TFlt && x.Size == 0 {
		if err := e.intoX(0, x.S); err != nil {
			return err
		}
		e.a.MovsdStore(x64.RCX, int32(x.Off), 0)
		return nil
	}
	if err := e.into(x64.RAX, x.S); err != nil {
		return err
	}
	switch x.Size {
	case 1:
		e.a.MovStore8(x64.RCX, int32(x.Off), x64.RAX)
	case 4:
		e.a.MovStore32(x64.RCX, int32(x.Off), x64.RAX)
	default:
		e.a.MovStore(x64.RCX, int32(x.Off), x64.RAX)
	}
	return nil
}

func (e *em) call(x *ir.Call) error {
	target, err := e.callee(x.Fn)
	if err != nil {
		return err
	}
	for i, a := range x.Args {
		t := ir.TInt
		if i < len(x.FT) {
			t = x.FT[i]
		}
		if i < 4 {
			if t == ir.TFlt {
				if err := e.intoX(i, a); err != nil {
					return err
				}
				continue
			}
			if err := e.into(intArgs[i], a); err != nil {
				return err
			}
			continue
		}
		if err := e.into(x64.RAX, a); err != nil {
			return err
		}
		e.a.MovStore(x64.RBP, outArgOff(e.slots, e.maxOut, i+1), x64.RAX)
	}
	e.a.Call(target)
	if !x.HasD {
		return nil
	}
	if x.D == nil {
		return nil
	}
	if x.RT == ir.TFlt {
		return e.storeX(x.D, 0)
	}
	return e.store(x.D, x64.RAX)
}

// floatRet 说这次调用返回的是小数还是整数／指针——由降级在 Call.RT 上标好。
//
// en: floatRet says whether this call returns a float or an integer/pointer - the lowering step
// marks it on Call.RT.
func (e *em) callee(name string) (x64.Label, error) {
	if strings.HasPrefix(name, "@") {
		return e.l.RtLabel(name), nil
	}
	if l, ok := e.fn[name]; ok {
		return l, nil
	}
	return 0, fmt.Errorf("没有 %s 这个函数", name)
}

// into 把一个值搬进通用寄存器 reg。
//
// en: into moves a value into the general-purpose register reg.
func (e *em) into(reg int, v ir.Val) error {
	switch x := v.(type) {
	case ir.K:
		e.a.MovImm(reg, int64(x))
	case ir.L:
		e.a.MovLoad(reg, x64.RBP, slotOff(int(x)))
	case ir.G:
		e.l.LoadGlobal(reg, e.d.Global[x])
	case ir.S:
		e.l.LeaData(reg, e.d.String[x])
	case ir.D: // 形状描述：交给运行期的是这条描述自己的地址
		e.l.LeaData(reg, e.d.Desc[x])
	case ir.KF:
		e.a.MovImm(reg, int64(x))
	default:
		return fmt.Errorf("认不出的值 %T", v)
	}
	return nil
}

// intoX 把一个小数搬进 xmm 寄存器。
//
// en: intoX moves a float into an xmm register.
func (e *em) intoX(reg int, v ir.Val) error {
	switch x := v.(type) {
	case ir.KF:
		e.a.MovImm(x64.RAX, int64(x))
		e.a.MovsdFromGPR(reg, x64.RAX)
	case ir.L:
		e.a.MovsdLoad(reg, x64.RBP, slotOff(int(x)))
	case ir.G:
		e.l.LoadGlobal(x64.RAX, e.d.Global[x])
		e.a.MovsdFromGPR(reg, x64.RAX)
	default:
		return fmt.Errorf("小数量的来源只能是常量、帧槽或静态槽，给的是 %T", v)
	}
	return nil
}

func (e *em) store(d ir.Val, reg int) error {
	switch x := d.(type) {
	case ir.L:
		e.a.MovStore(x64.RBP, slotOff(int(x)), reg)
	case ir.G:
		e.l.StoreGlobal(e.d.Global[x], reg)
	default:
		return fmt.Errorf("写入目标只能是帧槽或静态槽，给的是 %T", d)
	}
	return nil
}

// store2 一次把两个寄存器写进两个帧槽——只有接住块开头那一步用得上。
//
// en: store2 writes two registers into two frame slots at once - only the first step of a
// catch block needs that.
func (e *em) store2(d ir.Val, reg int, e2 ir.Val, reg2 int) error {
	if err := e.store(d, reg); err != nil {
		return err
	}
	return e.store(e2, reg2)
}

func (e *em) storeX(d ir.Val, reg int) error {
	switch x := d.(type) {
	case ir.L:
		e.a.MovsdStore(x64.RBP, slotOff(int(x)), reg)
	case ir.G:
		e.a.MovsdToGPR(x64.RAX, reg)
		e.l.StoreGlobal(e.d.Global[x], x64.RAX)
	default:
		return fmt.Errorf("小数写入目标只能是帧槽或静态槽，给的是 %T", d)
	}
	return nil
}

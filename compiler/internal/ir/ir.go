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

// Package ir 是平台无关的中间码。
//
// 形状是带类型标记的三地址小指令：值要么是常量，要么是帧槽 L，要么是静态槽 G，
// 要么是文本池里那条字面量的地址 S。每个槽 8 字节，整数家族（int、byte、
// boolean、unicode、enum、各种句柄）与小数都占一个槽，由 T 区分计算与搬运方式。
//
// 内存读写只有 Load／Store 两条，而它们的 Base 表示"把那个值当地址再解一次引用"——
// 这是 class 字段、容器元素、文本字节的唯一入口。读写静态槽本身不走这两条，只走 `Mov`
// （把 `G` 当基址等于解引用空指针；规则见 lower/print.go 头部说明）。
//
// en: Package ir is the platform-independent intermediate code.
// The shape is typed three-address micro-instructions: a value is either a constant, a
// frame slot L, a static slot G, or the address S of a literal in the text pool. Every slot is
// 8 bytes; the integer family (int, byte, boolean, unicode, enum, the various handles) and
// float each take one slot, told apart by T which says how to compute and how to move. Memory
// access has only two instructions, Load/Store, and their Base means "treat that value as an
// address and dereference once more" - the only entry point to class fields, container elements
// and text bytes. Reading and writing a static slot itself does not go through those two, only
// through `Mov` (using `G` as a base would be dereferencing a null pointer; the rule is at the
// top of lower/print.go).
package ir

import (
	"fmt"
	"strconv"
	"strings"
)

// T 是一个值在机器上的种类。
//
// en: T is the kind of a value as far as the machine is concerned.
type T uint8

const (
	TInt T = iota // 64 位整数家族：int、byte、boolean、unicode、enum，以及一切句柄
	TFlt          // 64 位 IEEE 754 双精度
)

func (t T) String() string {
	if t == TFlt {
		return "flt"
	}
	return "int"
}

// Val 是指令操作数。
//
// en: Val is an instruction operand.
type Val interface {
	val()
	String() string
}

// K 是整数常量。
//
// en: K is an integer constant.
type K int64

func (K) val() { return }

func (k K) String() string { return strconv.FormatInt(int64(k), 10) }

// KF 是小数常量，存的是位型（避免对 NaN 做比较）。
//
// en: KF is a float constant, stored as its bit pattern so that NaN never has to be compared.
type KF uint64

func (KF) val() { return }

func (f KF) String() string { return "flt:" + strconv.FormatUint(uint64(f), 16) }

// L 是帧上的第 N 个槽，落在 [rbp-8*(N+1)]。
//
// en: L is the N-th slot on the frame, which lands at [rbp-8*(N+1)].
type L int

func (L) val() { return }

func (l L) String() string { return "%" + strconv.Itoa(int(l)) }

// G 是静态池里的第 N 个槽（顶层的 let／const／shared、args）。
//
// en: G is the N-th slot of the static pool (top-level let/const/shared, and args).
type G int

func (G) val() { return }

func (g G) String() string { return "@" + strconv.Itoa(int(g)) }

// S 是文本池里第 N 条字面量的地址。
//
// en: S is the address of the N-th literal in the text pool.
type S int

func (S) val() { return }

func (s S) String() string { return "$" + strconv.Itoa(int(s)) }

// Label 是函数内的跳转目标。
//
// en: Label is a jump target inside one function.
type Label int

// Inst 是一条中间码。
//
// en: Inst is one intermediate instruction.
type Inst interface {
	inst()
	String() string
}

// Mov 是 D = S。
//
// en: Mov is D = S.
type Mov struct {
	D Val
	S Val
	T T
}

// Neg 是一元取负，整数与小数共用一条，靠 T 分。
//
// en: Neg is unary minus, shared by integers and floats and told apart by T.
type Neg struct {
	D, S Val
	T    T
}

// Not 是按位取反，LNot 是逻辑非。
//
// en: Not is bitwise complement, LNot is logical not.
type (
	Not struct {
		D, S Val
	}
	LNot struct {
		D, S Val
	}
)

// Bin 是 D = A op B。T 为 TFlt 时走 SSE2 的那一套。
//
// en: Bin is D = A op B. When T is TFlt it takes the SSE2 route.
type Bin struct {
	Op      string // + - * / % & | ^ << >> >>>
	D, A, B Val
	T       T
}

// Cmp 是 D = (A op B)，返回 0 或 1。
//
// en: Cmp is D = (A op B), returning 0 or 1.
type Cmp struct {
	Op      string // == != < <= > >=
	D, A, B Val
	T       T
}

// Br 是比较条件成立时跳转，省去一个中间布尔槽。
//
// en: Br jumps when the comparison holds, which saves one intermediate boolean slot.
type Br struct {
	Op   string
	A, B Val
	To   Label
	T    T
	Neg  bool // true 表示条件不成立时才跳
}

// Jump 与 Jz／Jnz：后两条判断一个整数是否为零。Lbl 是标号落点。
// IfOverflow 紧跟上一条 add／sub／imul：整数算术溢出时跳转（运行期错误码 10021）。
//
// en: Jump and Jz/Jnz: the latter two test whether an integer is zero. Lbl is where a label
// lands. IfOverflow follows the previous add/sub/imul: jump when the integer arithmetic
// overflows (runtime error code 10021).
type (
	Jump struct{ To Label }
	Jz   struct {
		V  Val
		To Label
	}
	Jnz struct {
		V  Val
		To Label
	}
	Lbl struct{ At Label }

	IfOverflow struct{ To Label }

	// Dbg 是一条检查点：不做任何计算，只标记"从这里起是源码第 Line 行第 Col 列那一条语句"。
	// 只在 debug 构建里出现（check.Program.Debug）。发射时把它换成一次 @dbg_trap 调用，
	// 同时记下这个位置机器码的偏移；宿主侧以 ID 作索引，断点与单步都落在这张表上。
	Dbg struct {
		ID        int
		File      string
		Line, Col int
	}
)

// Cvt 是 int↔float：I2F 精确转换，F2I 向零截断。
//
// en: Cvt is int<->float: I2F converts exactly, F2I truncates toward zero.
type Cvt struct {
	D, S Val
	To   T
}

// Load 是 D = *[Base+Off]，Store 是 *[Base+Off] = S。
//
// en: Load is D = *[Base+Off], Store is *[Base+Off] = S.
type (
	Load struct {
		D    Val
		Base Val
		Off  int
		T    T
		Size int // 读几个字节（0 表示按 T 的默认宽度）
		Sgn  bool
	}
	Store struct {
		Base Val
		Off  int
		S    Val
		T    T
		Size int
	}
)

// Addr 是 D = &静态槽 N，用来把一块静态数据当地址传给运行期。
//
// en: Addr is D = &static slot N, used to pass a block of static data to the runtime as an
// address.
type Addr struct {
	D Val
	G G
}

// Call 是一次调用：Fn 是用户函数名或 @ 开头的运行期例程名。
// Args 的类型由 FT 逐项标出（决定走通用寄存器还是 xmm），RT 是返回值的种类——
// 发射器据此决定结果从 rax 还是 xmm0 取，不做推断。
//
// en: Call is one invocation: Fn is either a user function name or a runtime routine name
// starting with @. The type of each entry in Args is marked item by item by FT (it decides
// whether the argument travels in a general-purpose register or an xmm), and RT is the kind of
// what comes back - the emitter uses it to take the result from rax or from xmm0 without
// guessing.
type Call struct {
	D    Val
	HasD bool
	Fn   string
	Args []Val
	FT   []T
	RT   T
}

// Ret 返回结果；Has 为假表示不返回值。
//
// en: Ret returns the result; a false Has means no value is returned.
type Ret struct {
	D   Val
	Has bool
	T   T
}

// PushHandler 挂一层处理器：Catch 是本函数里接住块的开头。
// 运行期按这一层恢复栈，抛出时跳转到 Catch。
//
// en: PushHandler installs one handler layer: Catch is the start of the catch block inside this
// same function. The runtime rewinds the stack according to this layer, and a throw jumps to
// Catch.
type PushHandler struct{ Catch Label }

// PushAbandon 给这一帧挂一条"异常越过本帧"标记：Pad 是本函数里先归还自己登记的资源、
// 再调用 @throw_walk 继续向外传播的代码段，Rec 是本帧为该记录预留的连续五个槽的头一个。
//
// 它与 PushHandler 挂同一条链、用同一套字段，只有两处不同：种类位置写 1，且记录住在帧上
// ——从函数入口挂到帧尾，帧销毁时随之消失，不需要向堆申请再归还。出链时也不区分：
// PopHandler 摘的永远是链头（只有链头是接住块记录时才调 @free）。
//
// en: PushAbandon attaches to this frame a marker saying "thrown past": Pad is the code in this
// function that finishes handing back the accounts it registered itself and then calls
// @throw_walk to keep going outward, and Rec is the first of the five consecutive slots this
// frame reserves for that record. It hangs on the same chain and
// uses the same fields as PushHandler, with two differences: the kind position is written 1,
// and the record lives on the frame - it is attached from the prologue until the frame ends and
// disappears with it, so no heap block has to be requested and returned. Taking it off is
// identical too: PopHandler always removes the chain head (and only calls @free when that head
// is a catch-block record).
type PushAbandon struct {
	Pad Label
	// Rec 是这五个槽位中编号最大（地址最低）的那个位置：运行期按 +0、+8、…、+32 向上走五格，
	// 正好覆盖整段。若写成头一个位置，+32 会越到这一段之上，把别的槽位当记录读；
	// 帧内清理记录读到的将是别的帧——这一结论由迁入帧槽后的一次段错误实测确认。
	Rec L
}

// PopHandler 摘掉最上面那一层：try 正常走完的那条路，以及一帧从 Ret 退出时摘自己的
// 帧内清理记录。
//
// en: PopHandler removes the topmost layer: the path where try completed normally, and the
// frame that leaves through Ret removing its own frame-scope record.
type PopHandler struct{}

// Catch 是接住块开头那一步：把 @throw 返回来的码放进 D、消息放进 E。
// 那两个值躺在 rax／rdx 里，除了这一条没有别的办法把它们落到槽上。
//
// en: Catch is the first step of a catch block: it puts the code returned by @throw into D and
// the message into E. Those two values sit in rax/rdx, and without this instruction
// there is no other way to land them in slots.
type Catch struct {
	D, E Val
}

func (*Mov) inst()         {}
func (*Neg) inst()         {}
func (*Not) inst()         {}
func (*LNot) inst()        {}
func (*Bin) inst()         {}
func (*Cmp) inst()         {}
func (*Br) inst()          {}
func (*Jump) inst()        {}
func (*Jz) inst()          {}
func (*Jnz) inst()         {}
func (*Lbl) inst()         {}
func (*IfOverflow) inst()  {}
func (*Dbg) inst()         {}
func (*Cvt) inst()         {}
func (*Load) inst()        {}
func (*Store) inst()       {}
func (*Addr) inst()        {}
func (*Call) inst()        {}
func (*Ret) inst()         {}
func (*PushHandler) inst() {}
func (*PushAbandon) inst() {}
func (*PopHandler) inst()  {}
func (*Catch) inst()       {}

func (m *Mov) String() string  { return fmt.Sprintf("%s = %s :: %s", m.D, m.S, m.T) }
func (n *Neg) String() string  { return fmt.Sprintf("%s = -%s :: %s", n.D, n.S, n.T) }
func (n *Not) String() string  { return fmt.Sprintf("%s = ~%s", n.D, n.S) }
func (l *LNot) String() string { return fmt.Sprintf("%s = !%s", l.D, l.S) }

func (b *Bin) String() string {
	return fmt.Sprintf("%s = %s %s %s :: %s", b.D, b.A, b.Op, b.B, b.T)
}

func (c *Cmp) String() string {
	return fmt.Sprintf("%s = %s %s %s :: %s", c.D, c.A, c.Op, c.B, c.T)
}

func (b *Br) String() string {
	if b.Neg {
		return fmt.Sprintf("if %s !%s %s goto L%d", b.A, b.Op, b.B, b.To)
	}
	return fmt.Sprintf("if %s %s %s goto L%d :: %s", b.A, b.Op, b.B, b.To, b.T)
}

func (j *Jump) String() string { return fmt.Sprintf("goto L%d", j.To) }
func (j *Jz) String() string   { return fmt.Sprintf("if %s == 0 goto L%d", j.V, j.To) }
func (j *Jnz) String() string  { return fmt.Sprintf("if %s != 0 goto L%d", j.V, j.To) }
func (l *Lbl) String() string  { return fmt.Sprintf("L%d:", l.At) }

func (j *IfOverflow) String() string { return fmt.Sprintf("if overflow goto L%d", j.To) }

func (d *Dbg) String() string { return fmt.Sprintf("dbg #%d %s:%d:%d", d.ID, d.File, d.Line, d.Col) }

func (c *Cvt) String() string { return fmt.Sprintf("%s = %s as %s", c.D, c.S, c.To) }

func (l *Load) String() string {
	sz := l.Size
	if sz == 0 {
		sz = 8
	}
	return fmt.Sprintf("%s = [%s+%d] :: %s.%d", l.D, l.Base, l.Off, l.T, sz)
}

func (s *Store) String() string {
	sz := s.Size
	if sz == 0 {
		sz = 8
	}
	return fmt.Sprintf("[%s+%d] = %s :: %s.%d", s.Base, s.Off, s.S, s.T, sz)
}

func (a *Addr) String() string { return fmt.Sprintf("%s = &%s", a.D, a.G) }

func (p *PushHandler) String() string { return fmt.Sprintf("push handler → L%d", p.Catch) }
func (p *PushAbandon) String() string { return fmt.Sprintf("push abandon → L%d", p.Pad) }
func (p *PopHandler) String() string  { return "pop handler" }
func (c *Catch) String() string       { return fmt.Sprintf("%s, %s = catch", c.D, c.E) }

func (c *Call) String() string {
	var ts []string
	for _, t := range c.FT {
		ts = append(ts, t.String())
	}
	dst := "_"
	if c.HasD {
		dst = c.D.String()
	}
	return fmt.Sprintf("%s = call %s(%s) :: [%s]", dst, c.Fn, join(c.Args), strings.Join(ts, ","))
}

func (r *Ret) String() string {
	if !r.Has {
		return "ret"
	}
	return fmt.Sprintf("ret %s :: %s", r.D, r.T)
}

func join(vs []Val) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = v.String()
	}
	return strings.Join(parts, ", ")
}

// Func 是一个可调用单位。Slots 是帧槽数，降级完成后固定。
// Params 与 PT 说明前几个槽是参数、分别走通用寄存器还是 xmm——
// 被调方进入后的第一步就是把它们搬进自己的槽。
//
// en: Func is one callable unit. Slots is the frame slot count, fixed once lowering finishes.
// Params and PT say which of the first slots are arguments and whether each travels in a
// general-purpose register or an xmm - the callee's first act is to move them into its own
// slots.
type Func struct {
	Name   string
	Slots  int
	Params int
	PT     []T
	Next   Label
	Body   []Inst
	// Locals 是这一帧上每一个有名字的位置，供调试器中断时读取局部量。当前这张表只存在于
	// 降级阶段的作用域里（lower.fc.scopes），函数降级结束即丢弃，后端不可见。
	Locals []Local
}

// Local 标明一个帧槽归属的名字。Kind 是解码所需的六类之一（int／float／bool／byte／rune／handle）：
// 前五类槽中装的是值本身，handle 装的是句柄（struct 也算句柄：它是值类型，
// 机器上按引用布置、赋值走 @copy_block）。Type 是源码里写出的类型写法，供阅读。
//
// en: Local says which name owns a frame slot. Kind is one of the six classes the decoder needs
// (int/float/bool/byte/rune/handle): for the first five the slot holds the value itself, for
// handle it holds a handle (a struct counts as a handle too - it is a value type, but on
// the machine it is arranged by reference and assignment goes through @copy_block). Type is the
// spelling as written, for the reader.
type Local struct {
	Slot int
	Name string
	Kind string
	Type string
}

// New 造一个函数，名字唯一。
//
// en: New creates a function with a unique name.
func New(name string) *Func { return &Func{Name: name} }

// Temp 要一个新槽。
//
// en: Temp asks for a new slot.
func (f *Func) Temp() L {
	l := L(f.Slots)
	f.Slots++
	return l
}

// Reserve 一次申请 n 个连续排列的槽，返回头一个。
// 帧内清理记录需要这个：它住在帧里，运行期按 [rbp−8×k] 一个位置一个位置地写，
// 所以这几个槽位必须是连续的（槽编号相差一个就是地址相差 8 字节）。
//
// en: Reserve asks for n slots laid out consecutively and returns the first. This is what the
// frame-scope record needs: it lives on the frame and the runtime writes it one position at a
// time as [rbp-8*k], so those slots must be contiguous (slot numbers differing by one means
// bytes differing by 8).
func (f *Func) Reserve(n int) L {
	l := L(f.Slots)
	f.Slots += n
	return l
}

// Fresh 要一个新标号。
//
// en: Fresh asks for a new label.
func (f *Func) Fresh() Label {
	l := f.Next
	f.Next++
	return l
}

// Emit 往函数体尾部追加一条。
//
// en: Emit appends one instruction to the end of the function body.
func (f *Func) Emit(in Inst) { f.Body = append(f.Body, in) }

// Prog 是一整个程序。
//
// en: Prog is the whole program.
type Prog struct {
	Funcs   []*Func
	Strings []string
	Descs   []Desc // 形状表
	Globals int
	Entry   string
	// Debug 从 check.Program 透传下来：为真才发射 ir.Dbg、记录检查点表，数据段才多出宿主指针槽位。
	// 关闭时后端看到的中间码与不带调试功能时完全一致。
	Debug bool
	// Names 是各份文件顶层那些 let／const：它们不在任何一帧上，落在静态槽里。
	// 调试器那一栏"静态"看的就是这张表，Slot 是第几个静态槽（codegen 的 DataLayout.Global 认它）。
	Names []GlobalName

	descAt map[string]D // 规范化之后的描述 → 编号，去重用
}

// GlobalName 是一个顶层名字：第几个静态槽、叫什么、什么形状、写在哪一份文件的第几行。
//
// en: GlobalName is one top-level name: which static slot, what it is called, what shape it
// has, and which line of which file wrote it.
type GlobalName struct {
	Slot int
	Name string
	Kind string
	Type string
	File string
	Line int
}

// Func 按名字找函数，没有就建一个。
//
// en: Func finds a function by name, creating one if there is none.
func (p *Prog) Func(name string) *Func {
	for _, f := range p.Funcs {
		if f.Name == name {
			return f
		}
	}
	f := New(name)
	p.Funcs = append(p.Funcs, f)
	return f
}

// Intern 把一条文本收进字面量池，返回它的编号。
//
// en: Intern takes a text into the literal pool and returns its number.
func (p *Prog) Intern(s string) S {
	for i, v := range p.Strings {
		if v == s {
			return S(i)
		}
	}
	p.Strings = append(p.Strings, s)
	return S(len(p.Strings) - 1)
}

// AddString 收一条文本并返回编号，允许重复（用于形状不同的那几条）。
//
// en: AddString takes a text and returns its number, duplicates allowed (used for the entries
// whose shapes differ).
func (p *Prog) AddString(s string) S {
	p.Strings = append(p.Strings, s)
	return S(len(p.Strings) - 1)
}

// Dump 把中间码打成文本，单测与排错都看它。
//
// en: Dump prints the intermediate code as text; both the unit tests and debugging read it.
func Dump(p *Prog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "globals=%d strings=%d entry=%s\n", p.Globals, len(p.Strings), p.Entry)
	for i, s := range p.Strings {
		fmt.Fprintf(&b, "  $%d = %q\n", i, s)
	}
	for _, f := range p.Funcs {
		fmt.Fprintf(&b, "func %s (slots=%d)\n", f.Name, f.Slots)
		for _, in := range f.Body {
			fmt.Fprintf(&b, "  %s\n", in.String())
		}
	}
	return b.String()
}

// D 是形状表里第 N 条描述的地址（console.print 与 number.to_string
// 接收整个容器／盒子时，靠它把类型信息带到运行期）。
//
// en: D is the address of the N-th description in the shape table (console.print and
// number.to_string need it when they receive a whole container or box, to carry the type
// information into the runtime).
type D int

func (D) val() { return }

func (d D) String() string { return "&" + strconv.Itoa(int(d)) }

// DescKind 是一种值在形状表里的种类。打印机（`nef.print`，降级侧用中间码写出来的一个
// 函数）识别的就是这一串数字，两边改动必须同步——建表的代码在 internal/lower/desc.go。
//
// en: DescKind is the kind of a value in the shape table. The printer (`nef.print`, a function
// written in intermediate code on the lowering side) recognises exactly these numbers, so a
// change on one side must be made on the other in the same commit - the code that builds the
// table is internal/lower/desc.go.
type DescKind int64

const (
	KindInt DescKind = iota + 1 // int、byte、enum：除 10 取余即可输出
	KindBool
	KindChar // unicode：一个码点编成 UTF-8
	KindFloat
	KindString // 文本句柄
	KindList
	KindDict
	KindSet
	KindStruct   // 一整块：字段名、偏移、字段种类都在描述里
	KindClass    // 句柄：块里就是那些字段，字段表与 struct 同一种形状
	KindBox      // object：以块头上 8 字节的类型字为准
	KindNull     // 可空位置的 null：打印 null
	KindTagTable // "类型字→形状"表本身，仅 KindBox 使用
	KindByte     // byte：与 int 同样按十进制打印，块内占一字节（窄字段按宽度读取）
	KindJson     // Json 树：打印走 nef.json.render，析构走 nef.json.free
	KindBytes    // bytes：一块连续内存，打印逐字节输出十进制，析构即释放这一块
	KindHandle   // 操作系统句柄（Socket 与 Listener）：打印输出类型名，收尾调用 closesocket
	KindProc     // 子进程（Process）：打印方式相同，收尾调用 @proc_close——未等待时直接结束该进程
)

// Field 是 struct／class 的一个字段：名字、在块里的偏移、它自己的形状。
//
// en: Field is one field of a struct/class: its name, its offset inside the block, and its own
// shape.
type Field struct {
	Name S
	Off  int64
	Type D
}

// TagRef 是"类型字 → 形状"表中的一个条目。盒子里装的到底是什么类型，运行期只能依据块头上
// 那 8 字节的类型字判断，所以整程序的具名类型都要在这张表里占一行；
// KindBox 的描述用 Elem 指向它。
//
// en: TagRef is one entry of the "type word -> shape" table. What a box actually holds is
// visible to the runtime only through the 8-byte type word at the head of the block, so
// every named type in the program gets a row in this table, and the KindBox description points
// at it through Elem.
type TagRef struct {
	Tag  int64
	Desc D
}

// Desc 是一条形状描述。指向别处的那几个槽存的是**从本条描述算起的字节差**，
// 不是绝对地址：这样数据段里就不需要任何指针型重定位，链接器只要把两个偏移相减。
// Name 指的是文本池里那条名字，Elem／Key 指向别的描述，Fields 指向这个类型的字段数组
// （每条 24 字节：名字地址、偏移、字段形状地址）。
//
// en: Desc is one shape description. The slots that point elsewhere store a BYTE DIFFERENCE
// measured from this description, not an absolute address, so the data section needs no pointer
// relocation at all - the linker only subtracts two offsets. Name points at the text pool entry
// holding the name, Elem/Key point at other descriptions, and Fields points at this type's
// field array, 24 bytes each: name address, offset, field shape address.
type Desc struct {
	Kind   DescKind
	Tag    int64 // struct／class 的类型字，打印机拿它与盒子头上那个比一比；其余为 0
	Name   S     // 类型名（struct／class 用），或空
	Elem   D     // list／hashset 的元素种类；dict 的值种类
	Key    D     // dict 的键种类
	Fields []Field
	Tags   []TagRef // 只有 KindTagTable 用
}

// NewDesc 在形状表里占一条、返回编号，内容留给调用方随后填。
//
// 采用"先占位再填充"而不是"构建完成再录入"：字段引用回自身的类型（`class A { b:B }` 与
// `class B { a:A }`）需要一张环形的描述表才能表示——按"先算子再算父"的写法会直接递归溢出。
// 先占位并登记编号，子描述绕回来时拿到的就是同一个编号，环即成型。
// key 是类型自身的写法（`list<int>`、`Point`），同一种形状在整个程序里只有一份。
//
// en: NewDesc claims a row in the shape table and returns its number, leaving the contents for
// the caller to fill in afterwards. The "claim first, fill later" order is needed because
// a type whose fields refer back to it (`class A { b:B }` with `class B { a:A }`) can only be
// represented by a cyclic description table - the "children first, then the parent" style
// recurses until it overflows. Claiming the row and registering its number means a child
// description that comes back around receives the same number, and the cycle closes. key is the
// type's own spelling (`list<int>`, `Point`), so one shape has exactly one description in the
// whole program.
func (p *Prog) NewDesc(key string) D {
	if n, ok := p.descAt[key]; ok {
		return n
	}
	n := D(len(p.Descs))
	p.Descs = append(p.Descs, Desc{})
	if p.descAt == nil {
		p.descAt = map[string]D{}
	}
	p.descAt[key] = n
	return n
}

// DescOf 查询一个已经占过的编号，没有就返回 -1（降级阶段用它判断某种形状是否已经登记过）。
//
// en: DescOf looks up a number already claimed and returns -1 when there is none; the lowering
// side uses it to ask whether this shape has already been laid out.
func (p *Prog) DescOf(key string) D {
	if n, ok := p.descAt[key]; ok {
		return n
	}
	return -1
}

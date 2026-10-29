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

	"neflang.org/nf/internal/pe"
	"neflang.org/nf/internal/runtime"
	"neflang.org/nf/internal/x64"
)

// Linker 把"用户代码 + 运行期例程 + 静态数据"装配成一个 exe。
//
// 用户代码先编，运行期那一整块接在后面；两边互相引用靠标号与两处回填：
// 用户 call 运行期例程用 x64 的标号（装配时才知道落点），
// 谁要取静态数据或导入表就用 LeaRIP／MovLoadRIP 返回的回填点，交给 pe.Build。
//
// en: Linker assembles "user code + runtime routines + static data" into one exe. User code is
// emitted first and the whole runtime block follows it; the two sides reference each other
// through labels and two kinds of patch points: a user `call` of a runtime routine uses an x64
// label (the landing point is only known at assembly), and anything that needs static data or
// the import table uses the patch point returned by LeaRIP/MovLoadRIP, which is handed to
// pe.Build.
type Linker struct {
	a       *x64.Asm
	rt      runtime.Blob
	rtLabel map[string]x64.Label
	patches []pe.Patch
	next    x64.Label
	// sites 是检查点表：只有 debug 构建会有内容，顺序就是代码里的先后。
	sites []Site
}

// Site 是一条检查点在机器码里的位置。宿主拿它把"源码第几行"与"哪一格指令"对上：
// 设断点时按（文件, 行）查出这一组号，单步时取"下一个号不同、且帧深相同"的那条。
//
// en: Site is the machine-code position of one checkpoint. The host uses it to match "which
// source line" with "which instruction slot": setting a breakpoint looks up this group of
// numbers by (file, line), and stepping takes "the next entry whose number differs, at the same
// frame depth".
type Site struct {
	ID     int // 中间码里那条 ir.Dbg 的号，全程序唯一
	Func   string
	Offset int // 这一条检查点第一条指令在代码段里的位置
	File   string
	Line   int
	Col    int
}

func (l *Linker) noteSite(s Site) { l.sites = append(l.sites, s) }

// Sites 交出那张检查点表；不开 debug 时是空的。
//
// en: Sites hands out the checkpoint table; it is empty when debug is off.
func (l *Linker) Sites() []Site { return l.sites }

// NewLinker 造一个装配器，同时为每个运行期例程分配一个标号。
//
// en: NewLinker builds an assembler and gives every runtime routine a label.
func NewLinker() *Linker {
	rt := runtime.Build()
	l := &Linker{a: x64.New(), rt: rt, rtLabel: map[string]x64.Label{}, next: 1}
	for _, p := range rt.Parts {
		l.rtLabel[p.Name] = l.take()
	}
	return l
}

// Asm 返回底层的汇编器，发射器直接往它里面写。
//
// en: Asm returns the underlying assembler; the emitter writes straight into it.
func (l *Linker) Asm() *x64.Asm { return l.a }

// Label 要一个新标号（函数入口、跳转目标都用它）。
//
// en: Label asks for a new label (used for function entries and jump targets).
func (l *Linker) Label() x64.Label { return l.take() }

func (l *Linker) take() x64.Label {
	n := l.next
	l.next++
	return n
}

// CallRt 调一个运行期例程，比如 @put_int。
//
// en: CallRt calls one runtime routine, for example @put_int.
func (l *Linker) CallRt(name string) {
	l.a.Call(l.rtLabel[name])
}

// RtLabel 取某个运行期例程的标号。
//
// en: RtLabel takes the label of a given runtime routine.
func (l *Linker) RtLabel(name string) x64.Label { return l.rtLabel[name] }

// LeaData 落 reg = &[静态数据 off]，回填留给 Build。
// off 是 userData 里的偏移；运行期自己那半截的数据由 Build 往后接，不用调用方操心。
//
// en: LeaData emits reg = &[static data off], leaving the patch to Build. off is an offset
// inside userData; the runtime's own half of the data is appended by Build and is not the
// caller's concern.
func (l *Linker) LeaData(reg, off int) {
	at := l.a.LeaRIP(reg)
	l.patches = append(l.patches, pe.Patch{At: at, Sec: pe.SecData, Off: off})
}

// LoadGlobal 落 reg = [静态数据槽 off]。
//
// en: LoadGlobal emits reg = [static data slot off].
func (l *Linker) LoadGlobal(reg, off int) {
	at := l.a.MovLoadRIP(reg)
	l.patches = append(l.patches, pe.Patch{At: at, Sec: pe.SecData, Off: off})
}

// StoreGlobal 落 [静态数据槽 off] = reg。
//
// en: StoreGlobal emits [static data slot off] = reg.
func (l *Linker) StoreGlobal(off, reg int) {
	at := l.a.MovStoreRIP(reg)
	l.patches = append(l.patches, pe.Patch{At: at, Sec: pe.SecData, Off: off})
}

// LeaGlobal 落 reg = &静态数据槽 off，用来把一份全局数据当地址交出去。
//
// en: LeaGlobal emits reg = &static data slot off, used to hand over a piece of global data as
// an address.
func (l *Linker) LeaGlobal(reg, off int) {
	l.LeaData(reg, off)
}

// StackFloorSet 摆开局那三行：这一段的栈基减去语言答应的栈，剩下那个地址就是栈底，
// 存进静态槽给每一帧比。
//
// 在开局计算而不是写死一个地址：栈住在哪儿由操作系统定，每台机器、每次运行都不同；
// 而"能用多少"是语言定的那一个数（`pe.StackReserve`），产物头里写它、直跑起线程也按它要栈，
// 于是同一份代码两条路落在同一条边界上。
//
// en: StackFloorSet places the three opening lines: the stack base of this segment minus the
// stack the language promises leaves the floor address, which is stored in a static slot for
// every frame to compare against. It is computed at start-up rather than hardcoded because
// where the stack lives is the operating system's choice and differs per machine and per run,
// while how much of it is usable is the single number the language fixes (`pe.StackReserve`):
// the product header carries it and the direct-run path requests that much stack when starting
// the thread, so the same code reaches the same boundary on both paths.
func (l *Linker) StackFloorSet(off int) {
	l.a.MovFromTeb(x64.RAX, tebStackBase)
	l.a.AddImm(x64.RAX, -(pe.StackReserve - stackSlack))
	l.patches = append(l.patches, pe.Patch{At: l.a.MovStoreRIP(x64.RAX), Sec: pe.SecData, Off: off})
}

// StackGuard 摆每一帧开场那两条：比一次栈、跳一次。正常深度下那条跳不会被执行。
//
// 检查放在 `sub rsp` 之后：那一步只是把指针挪开，不碰内存，所以触到边界之前一个字节都没写下去。
// 触到边界跳的是 @die_stack——它不返回，也不找 catch：接住一个"栈用完了"的抛出本身还要用栈。
//
// en: StackGuard places the two lines at the start of every frame: compare the stack once, jump
// once. At normal depth the jump is not taken. The check sits AFTER `sub rsp`: that
// instruction only moves a pointer and touches no memory, so not one byte has been written
// before the boundary is reached. Crossing the boundary goes to @die_stack, which neither
// returns nor looks for a catch - catching a "stack exhausted" throw would itself need stack.
func (l *Linker) StackGuard(off int) {
	l.patches = append(l.patches, pe.Patch{At: l.a.CmpRspRIP(), Sec: pe.SecData, Off: off})
	l.a.Jcc(x64.B, l.rtLabel["@die_stack"])
}

// assemble 把用户代码、运行期那一整块与静态数据拼成 pe 要的那份布局输入。
// 封 exe 与"在内存里直接跑"两条路都到这儿为止，回填规则只有一处。
//
// en: assemble joins the user code, the whole runtime block and the static data into the layout
// input pe wants. Both the exe path and the run-directly-in-memory path stop here, so the
// patching rules live in exactly one place.
func (l *Linker) assemble(userData []byte, entry int) (pe.Layout, error) {
	base := l.a.PC()
	for i, p := range l.rt.Parts {
		l.a.Mark(l.rtLabel[p.Name])
		end := len(l.rt.Code)
		if i+1 < len(l.rt.Parts) {
			end = l.rt.Parts[i+1].Off
		}
		l.a.Append(l.rt.Code[p.Off:end])
	}
	for _, p := range l.rt.Parts {
		for _, r := range p.Refs {
			patch := pe.Patch{At: base + r.At}
			switch r.Kind {
			case runtime.RefData:
				off, ok := l.rt.DataAt[r.Name]
				if !ok {
					return pe.Layout{}, fmt.Errorf("运行期数据里没有 %q 这一项", r.Name)
				}
				patch.Sec = pe.SecData
				patch.Off = len(userData) + off
			case runtime.RefIAT:
				patch.Sec = pe.SecIAT
				patch.Off = r.Off
			default:
				return pe.Layout{}, fmt.Errorf("未知的运行期引用种类 %d", r.Kind)
			}
			l.patches = append(l.patches, patch)
		}
	}
	code, err := l.a.Finish()
	if err != nil {
		return pe.Layout{}, err
	}
	data := append(append([]byte{}, userData...), l.rt.Data...)
	imports := make([]pe.Import, 0, len(l.rt.Imports))
	for _, imp := range l.rt.Imports {
		imports = append(imports, pe.Import{DLL: imp.DLL, Name: imp.Name})
	}
	return pe.Layout{
		Code:    code,
		Data:    data,
		Entry:   entry,
		Imports: imports,
		Patches: l.patches,
	}, nil
}

// Build 装配成 exe。userData 是静态槽加字符串池拼出来的那一整块，entry 是入口在代码里的偏移。
//
// en: Build assembles the exe. userData is the block built from the static slots plus the
// string pool, and entry is the entry point's offset inside the code.
func (l *Linker) Build(userData []byte, entry int) ([]byte, error) {
	lay, err := l.assemble(userData, entry)
	if err != nil {
		return nil, err
	}
	return pe.Build(lay)
}

// JitImage 摆出能直接在内存里跑的那块映像：入口在其中的偏移、
// 导入表每一格该填哪个系统函数，还有 cmdW 那个槽位的位置——宿主得把这一号程序
// 自己的命令行宽字符写进去，否则 @args 读到的是编译器的命令行。
//
// en: JitImage arranges the image that can run straight out of memory: the entry
// point's offset inside it, which system function each import slot should be filled with, and
// where the cmdW slot is - the host must write this program's own command line as wide
// characters there, otherwise @args would read the compiler's command line.
type JitImage struct {
	Img   pe.Image
	CmdW  int // cmdW 那个位置在映像里的位置
	Spare int // 数据段尾巴上空着的字节数，供宿主放自己的东西
}

// Jit 摆出那一块映像。spare 是替宿主留的额外空间（宿主自己的栈、参数串都往这儿放）。
//
// en: Jit lays out that image. spare is extra space reserved for the host, which puts its own
// stack and argument string there.
func (l *Linker) Jit(userData []byte, entry, spare int) (JitImage, error) {
	lay, err := l.assemble(userData, entry)
	if err != nil {
		return JitImage{}, err
	}
	img, err := pe.Jit(lay)
	if err != nil {
		return JitImage{}, err
	}
	cmdW, ok := l.rt.DataAt["cmdW"]
	if !ok {
		return JitImage{}, fmt.Errorf("运行期数据里没有 cmdW 这一项")
	}
	return JitImage{
		Img:   img,
		CmdW:  img.DataAt + len(userData) + cmdW,
		Spare: spare,
	}, nil
}

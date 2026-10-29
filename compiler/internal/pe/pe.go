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

// Package pe 把后端落出来的代码与数据封成一个 Windows x64 的可执行文件。
//
// 只依赖 kernel32.dll 一个导入库，不链接 C 运行时；关掉了 ASLR（DllCharacteristics 里
// 不置 DYNAMIC_BASE），于是也不用带 .reloc 段——产物自身就能运行，不需要在这台机器上装东西。
//
// en: Package pe seals the code and data emitted by the backend into a Windows x64 executable.
// It depends on one import library, kernel32.dll, and does not link a C runtime. ASLR is
// switched off (DYNAMIC_BASE is not set in DllCharacteristics), so the file carries no .reloc
// section either - the product runs on its own and needs nothing installed on this machine.
package pe

import (
	"encoding/binary"
	"fmt"
)

// 段号：RIP 相对位移要指到哪儿。
//
// en: Section numbers: where a RIP-relative displacement is meant to point.
const (
	SecCode = 0
	SecData = 1
	SecIAT  = 2 // 导入地址表里那个位置，Off 是第几个导入
)

// Patch 是一处待回填的 RIP 相对位移：At 是它在 Code 里的位置，Sec+Off 指到目标。
//
// en: Patch is one RIP-relative displacement still waiting to be filled in: At is its place
// inside Code, Sec+Off point at the target.
type Patch struct {
	At  int
	Sec int
	Off int
}

// Import 是一个位置导入：要问的函数名，与它在哪一个模块里。
//
// 从前这里只有一个名字数组、模块名写死成 kernel32；网络那一层要加 `ws2_32.dll`
// 与 `winhttp.dll`，所以模块名跟着每一个位置走。同一个 DLL 的名字要连着排——PE 的导入表
// 是一个 DLL 一个描述符，装配的时候按连续的一段一段切（见 runsOf）。
//
// en: Import is one imported symbol: the function name being asked for and the module it lives
// in. There used to be only a name array here, with the module name hardcoded to kernel32; the
// network layer added ws2_32.dll and winhttp.dll, so the module name now travels
// with every symbol. Names from the same DLL have to stay adjacent - the PE import table uses
// one descriptor per DLL, so assembly cuts the table run by run (see runsOf).
type Import struct {
	DLL  string
	Name string
}

// Layout 是链接一次的输入。
//
// en: Layout is the input of one link.
type Layout struct {
	Code    []byte
	Data    []byte
	Entry   int      // 入口在 Code 里的偏移
	Imports []Import // 每一格是"哪个模块的哪个函数"，按代码引用的先后排
	Patches []Patch
}

const (
	fileAlign = 0x200
	memAlign  = 0x1000
	imageBase = 0x140000000
	textRVA   = 0x1000

	// 一个导入描述符 20 字节，后面跟一个全零的收尾；一个位置 thunk 8 字节。
	descSize  = 20
	thunkSize = 8

	// StackReserve 是一号程序能用的调用栈，8 MiB。两处读它：产物头里写这一个数，
	// `nf 文件.nf` 起的那条线程也按这一个数要栈——于是同一份代码直跑与跑产物
	// 面对同一条边界，报的都是"调用套得太深"。
	StackReserve = 0x800000
)

// Build 返回整个 .exe 的字节。
//
// en: Build returns the bytes of the whole .exe.
func Build(l Layout) ([]byte, error) {
	if len(l.Imports) == 0 {
		return nil, fmt.Errorf("一个导入都没有，PE 装不出东西")
	}
	runs, err := runsOf(l.Imports)
	if err != nil {
		return nil, err
	}
	plan, tailSize := planImports(l.Imports, runs)
	data := make([]byte, len(l.Data)+tailSize)
	copy(data, l.Data)
	tail := data[len(l.Data):]
	writeImportTail(tail, l.Imports, plan)

	codeRVA := uint32(textRVA)
	dataRVA := uint32(align(uint64(textRVA)+uint64(len(l.Code)), memAlign))
	sizeOfImage := uint32(align(uint64(dataRVA)+uint64(len(data)), memAlign))
	iatRVA := dataRVA + uint32(len(l.Data))

	patchImports(tail, l.Imports, plan, iatRVA)

	for _, p := range l.Patches {
		var target uint32
		switch p.Sec {
		case SecCode:
			target = codeRVA + uint32(p.Off)
		case SecData:
			target = dataRVA + uint32(p.Off)
		case SecIAT:
			if p.Off < 0 || p.Off >= len(l.Imports) {
				return nil, fmt.Errorf("代码要引导入表第 %d 格，这张表一共只有 %d 格", p.Off, len(l.Imports))
			}
			target = iatRVA + uint32(plan.at[p.Off])
		default:
			return nil, fmt.Errorf("未知的段号 %d", p.Sec)
		}
		if p.At+4 > len(l.Code) {
			return nil, fmt.Errorf("回填点 %d 出了代码段", p.At+4)
		}
		rel := int64(target) - int64(codeRVA+uint32(p.At)+4)
		binary.LittleEndian.PutUint32(l.Code[p.At:], uint32(int32(rel)))
	}

	hdrSize := uint32(align(64+4+20+240+2*40, fileAlign))
	out := make([]byte, hdrSize)

	copy(out, "MZ")
	binary.LittleEndian.PutUint32(out[0x3C:], 64)

	o := 64
	copy(out[o:], "PE\x00\x00")
	o += 4
	binary.LittleEndian.PutUint16(out[o:], 0x8664)    // AMD64
	binary.LittleEndian.PutUint16(out[o+2:], 2)       // 两个段
	binary.LittleEndian.PutUint16(out[o+16:], 240)    // 可选头尺寸
	binary.LittleEndian.PutUint16(out[o+18:], 0x0022) // 可执行映像 | 32 位机
	o += 20

	oh := o
	binary.LittleEndian.PutUint16(out[oh:], 0x020B) // PE32+；0x10b 是 32 位的，装上去加载器直接不认
	binary.LittleEndian.PutUint16(out[oh+2:], 14)
	binary.LittleEndian.PutUint32(out[oh+4:], uint32(align(uint64(len(l.Code)), fileAlign)))
	binary.LittleEndian.PutUint32(out[oh+8:], uint32(align(uint64(len(data)), fileAlign)))
	binary.LittleEndian.PutUint32(out[oh+16:], codeRVA+uint32(l.Entry))
	binary.LittleEndian.PutUint32(out[oh+20:], codeRVA)
	binary.LittleEndian.PutUint64(out[oh+24:], imageBase)
	binary.LittleEndian.PutUint32(out[oh+32:], memAlign)
	binary.LittleEndian.PutUint32(out[oh+36:], fileAlign)
	binary.LittleEndian.PutUint16(out[oh+40:], 6) // 最低操作系统版本
	binary.LittleEndian.PutUint16(out[oh+48:], 6) // 最低子系统版本
	binary.LittleEndian.PutUint32(out[oh+56:], sizeOfImage)
	binary.LittleEndian.PutUint32(out[oh+60:], hdrSize)
	binary.LittleEndian.PutUint16(out[oh+68:], 3)            // 控制台子系统
	binary.LittleEndian.PutUint16(out[oh+70:], 0x0100)       // NX_COMPAT；不置 DYNAMIC_BASE，所以不用 .reloc
	binary.LittleEndian.PutUint64(out[oh+72:], StackReserve) // 调用栈：见上面那个常量的说明
	binary.LittleEndian.PutUint64(out[oh+80:], 0x1000)
	binary.LittleEndian.PutUint64(out[oh+88:], 0x100000)
	binary.LittleEndian.PutUint64(out[oh+96:], 0x1000)
	binary.LittleEndian.PutUint32(out[oh+108:], 16)
	// 数据目录 1 号＝导入表，指的是描述符数组：一个 DLL 一个，末尾再跟一个全零的收尾
	binary.LittleEndian.PutUint32(out[oh+0x70+8:], iatRVA+uint32(plan.descBase))
	binary.LittleEndian.PutUint32(out[oh+0x70+12:], uint32((len(plan.runs)+1)*descSize))

	putSection(out, oh+240, ".text", uint32(len(l.Code)), codeRVA,
		uint32(align(uint64(len(l.Code)), fileAlign)), hdrSize, 0x60000020)
	putSection(out, oh+280, ".data", uint32(len(data)), dataRVA,
		uint32(align(uint64(len(data)), fileAlign)),
		hdrSize+uint32(align(uint64(len(l.Code)), fileAlign)), 0xC0000040)

	out = append(out, l.Code...)
	out = pad(out, int(hdrSize)+int(align(uint64(len(l.Code)), fileAlign)))
	out = append(out, data...)
	return pad(out, int(hdrSize)+int(align(uint64(len(l.Code)), fileAlign))+
		int(align(uint64(len(data)), fileAlign))), nil
}

// IatSlot 是"在内存里直接跑"那块映像里导入表的一个位置：At 是它的位置，DLL 与 Name 是
// 该填进来的那个系统函数属于哪个模块、叫什么——没有加载器替我们回填，宿主挨个模块问地址。
//
// en: IatSlot is one place in the import table of an image that runs directly in memory: At is
// that place, and DLL plus Name say which module the system function to be filled in belongs to
// and what it is called. No loader fills these in for us, so the host asks each module for the
// address itself.
type IatSlot struct {
	At   int
	DLL  string
	Name string
}

// Image 是一块能直接在内存里跑的安排：Bytes 整块、Entry 是入口在其中的偏移、
// Iat 是导入表每格该填谁、DataAt 是数据段起点（宿主想往静态数据里写东西要用）。
//
// en: Image is a layout that can run straight out of memory: Bytes is the whole block, Entry
// the offset of the entry point inside it, Iat who each import slot should be filled with, and
// DataAt the start of the data section - which the host needs when it wants to write into
// static data.
type Image struct {
	Bytes  []byte
	Entry  int
	Iat    []IatSlot
	DataAt int
}

// Jit 把同一份代码与数据摆成能直接在内存里跑的映像：
// 不封 PE 头，也不写导入的名称表与描述符。
//
// 代码仍放在 0、数据仍放在 align(len(code))，与 PE 里两段的相对差一样，
// 所以每一处 rip 相对位移都照 Build 那套算法算，不必另开规则。
//
// en: Jit arranges the same code and data into an image that runs directly in memory:
// no PE header, and no import name table or descriptors. Code still starts at 0 and data still
// at align(len(code)), the same distance between the two sections as in the PE file, so every
// rip-relative displacement is computed by Build's algorithm and no second rule is needed.
func Jit(l Layout) (Image, error) {
	var out Image
	if len(l.Imports) == 0 {
		return out, fmt.Errorf("一个导入都没有，摆不出映像")
	}
	dataOff := int(align(uint64(len(l.Code)), memAlign))
	iatOff := dataOff + len(l.Data)
	// 直跑这一侧没有加载器去读描述符，所以导入表就是一串平铺的槽位；
	// "同一个 DLL 的名字要连着排"这条约束照样检查一遍，免得同一份代码能运行却产不出文件。
	if _, err := runsOf(l.Imports); err != nil {
		return out, err
	}
	img := make([]byte, align(uint64(iatOff+len(l.Imports)*8), memAlign))
	copy(img, l.Code)
	copy(img[dataOff:], l.Data)
	for _, p := range l.Patches {
		var target int
		switch p.Sec {
		case SecCode:
			target = p.Off
		case SecData:
			target = dataOff + p.Off
		case SecIAT:
			if p.Off < 0 || p.Off >= len(l.Imports) {
				return out, fmt.Errorf("代码要引导入表第 %d 格，这张表一共只有 %d 格", p.Off, len(l.Imports))
			}
			target = iatOff + p.Off*8
		default:
			return out, fmt.Errorf("未知的段号 %d", p.Sec)
		}
		if p.At+4 > len(l.Code) {
			return out, fmt.Errorf("回填点 %d 出了代码段", p.At+4)
		}
		rel := int64(target) - int64(p.At+4)
		binary.LittleEndian.PutUint32(img[p.At:], uint32(int32(rel)))
	}
	var iat []IatSlot
	for i, imp := range l.Imports {
		iat = append(iat, IatSlot{At: iatOff + i*8, DLL: imp.DLL, Name: imp.Name})
	}
	return Image{Bytes: img, Entry: l.Entry, Iat: iat, DataAt: dataOff}, nil
}

func putSection(out []byte, at int, name string, virtSize, rva, rawSize, rawPtr, chars uint32) {
	copy(out[at:at+8], name)
	binary.LittleEndian.PutUint32(out[at+8:], virtSize)
	binary.LittleEndian.PutUint32(out[at+12:], rva)
	binary.LittleEndian.PutUint32(out[at+16:], rawSize)
	binary.LittleEndian.PutUint32(out[at+20:], rawPtr)
	binary.LittleEndian.PutUint32(out[at+36:], chars)
}

// importRun 是同一个 DLL 那一段导入在尾巴上的排布。
//
// en: importRun is the tail layout of one run of imports belonging to the same DLL.
type importRun struct {
	dll        string
	first      int // 这一段第一个名字在 Imports 里的下标
	n          int // 这一段有几个名字
	iatOff     int // 这一段的导入地址表：n 格加一格收尾的 0
	iptOff     int // 这一段的名称表（OriginalFirstThunk），同样的长度
	descOff    int // 这一段那一个描述符
	dllNameOff int // 这一段那一个模块名
}

// importPlan 是导入结构在 .data 尾巴上的排布。排一遍算好偏移，写和回填都照它来，
// 免得两处各算各的、差一个字节整个 PE 就装不起来。
//
// at 与 names 按 Imports 的下标查：代码里回填的是"第几格"，而第几格落在尾巴上哪儿
// 由这里算——分成几段之后两者就不再是同一个数，所以必须过这一道映射。
//
// en: importPlan is the layout of the import structure at the tail of .data. Laying it out once
// and recording the offsets lets both writing and patching follow the same plan; otherwise two
// places compute separately, one byte apart, and the whole PE fails to load. at and names are
// indexed by the position in Imports: the code patches by "which slot", and where that slot
// lands at the tail is computed here. Once the names are split into runs, the two are no longer
// the same number, so this mapping has to be crossed.
type importPlan struct {
	runs     []importRun
	at       []int
	names    []int
	descBase int
	size     int
}

// runsOf 按"同一个 DLL 的名字要连着排"切段。另一个模块的名字夹在中间会把连续段切开，
// 同一个模块于是出现两个描述符——加载器不会替我们合并，它只读描述符里那一个模块名，
// 所以这里当场报错说清楚。
//
// en: runsOf cuts the imports so that names from the same DLL stay in one run. If a name from
// another module is interleaved and the run is cut in two, the same module gets two descriptors
// - the loader does not merge them for us, it only reads the one module name written in each
// descriptor, so the checker reports it on the spot.
func runsOf(imports []Import) ([]importRun, error) {
	var runs []importRun
	for i, imp := range imports {
		if imp.DLL == "" {
			return nil, fmt.Errorf("导入表里 %s 没写它属于哪一个 DLL", imp.Name)
		}
		if n := len(runs); n > 0 && runs[n-1].dll == imp.DLL {
			runs[n-1].n++
			continue
		}
		for _, r := range runs {
			if r.dll == imp.DLL {
				return nil, fmt.Errorf("导入表里 %s 的名字分成两段排了：同一个 DLL 的要连着排", imp.DLL)
			}
		}
		runs = append(runs, importRun{dll: imp.DLL, first: i, n: 1})
	}
	return runs, nil
}

func planImports(imports []Import, runs []importRun) (importPlan, int) {
	p := importPlan{runs: runs, at: make([]int, len(imports)), names: make([]int, len(imports))}
	cur := 0
	for k := range p.runs {
		p.runs[k].iatOff = cur
		cur += (p.runs[k].n + 1) * thunkSize
	}
	for k := range p.runs {
		p.runs[k].iptOff = cur
		cur += (p.runs[k].n + 1) * thunkSize
	}
	p.descBase = cur
	for k := range p.runs {
		p.runs[k].descOff = cur
		cur += descSize
	}
	cur += descSize // 描述符数组那一串后面的全零收尾
	for k := range p.runs {
		r := &p.runs[k]
		for j := 0; j < r.n; j++ {
			i := r.first + j
			p.at[i] = r.iatOff + j*thunkSize
			p.names[i] = cur
			cur += int(align(uint64(2+len(imports[i].Name)+1), 2))
		}
	}
	for k := range p.runs {
		p.runs[k].dllNameOff = cur
		cur += int(align(uint64(len(p.runs[k].dll)+1), 2))
	}
	p.size = cur
	return p, cur
}

func writeImportTail(b []byte, funcs []Import, p importPlan) []byte {
	// hint 那两字节留 0：按名字导入时它只是个提示
	for i, f := range funcs {
		copy(b[p.names[i]+2:], f.Name)
	}
	for _, r := range p.runs {
		copy(b[r.dllNameOff:], r.dll)
	}
	return b
}

func patchImports(tail []byte, funcs []Import, p importPlan, iatRVA uint32) {
	for _, r := range p.runs {
		for j := 0; j < r.n; j++ {
			thunk := iatRVA + uint32(p.names[r.first+j])
			binary.LittleEndian.PutUint64(tail[r.iatOff+j*thunkSize:], uint64(thunk))
			binary.LittleEndian.PutUint64(tail[r.iptOff+j*thunkSize:], uint64(thunk))
		}
		// 两段表各自都要一个位置 0 收尾：加载器读到 0 就认为这一段结束
		binary.LittleEndian.PutUint64(tail[r.iatOff+r.n*thunkSize:], 0)
		binary.LittleEndian.PutUint64(tail[r.iptOff+r.n*thunkSize:], 0)

		d := r.descOff
		binary.LittleEndian.PutUint32(tail[d:], iatRVA+uint32(r.iptOff))        // OriginalFirstThunk
		binary.LittleEndian.PutUint32(tail[d+12:], iatRVA+uint32(r.dllNameOff)) // Name
		binary.LittleEndian.PutUint32(tail[d+16:], iatRVA+uint32(r.iatOff))     // FirstThunk
	}
}

func align(v, a uint64) uint64 { return (v + a - 1) / a * a }

func pad(b []byte, n int) []byte {
	for len(b) < n {
		b = append(b, 0)
	}
	return b
}

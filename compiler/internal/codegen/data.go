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
	"encoding/binary"
	"unicode/utf8"

	"neflang.org/nf/internal/ir"
)

// 静态数据区的布局。
//
// .data 的前半截是 nef 程序自己的：先是顶层 let／const／shared 与 args 那些静态槽，
// 一条 8 字节；再是文本字面量池，每条是「字节数 8 + 码点数 8 + UTF-8 字节」；最后是形状表
// （打印整个容器与盒子时用）。运行期例程自带的那一小块由 Linker 接在后面，
// 两边互不知情。
//
// en: Layout of the static data area. The first half of .data belongs to the nef program
// itself: first the static slots for top-level let/const/shared and args, 8 bytes each; then
// the text literal pool, each entry "byte count 8 + code point count 8 + UTF-8 bytes"; finally
// the shape table, used when printing a whole container or a box. The small block the runtime
// routines carry is appended by the Linker, and the two halves know nothing of each other.

// DataLayout 记下每个静态槽、每条字面量、每条形状描述在这半截里的位置。
//
// en: DataLayout records where each static slot, each literal and each shape description lands
// in this half of the data section.
type DataLayout struct {
	User   []byte
	Global map[ir.G]int
	String map[ir.S]int
	Desc   map[ir.D]int
	// StackFloor 是"这一号程序的栈底"那个位置：入口开局算好它（Linker.StackFloorSet），
	// 之后每一帧开场比一次（Linker.StackGuard）。摆在这半截的最尾，因为它既不是
	// 程序的变量也不是字面量，形状表也不涉及它。
	StackFloor int
	// DbgHost 是宿主回调地址那个位置在这半截里的位置：只有 debug 构建才有，不开的时候是 -1，
	// 数据段一个字节也不多。发射那一步每条检查点都 `mov rax,[rip+DbgHost]; call rax`，
	// 起程序的那一侧在跑之前把 syscall.NewCallback 的地址写进这个位置（cmd/nf 的调试那一路）。
	DbgHost int
}

// BuildData 排出静态数据区。
//
// en: BuildData lays out the static data section.
func BuildData(p *ir.Prog) *DataLayout {
	d := &DataLayout{Global: map[ir.G]int{}, String: map[ir.S]int{}, Desc: map[ir.D]int{}, DbgHost: -1}
	for i := 0; i < p.Globals; i++ {
		d.Global[ir.G(i)] = len(d.User)
		d.User = append(d.User, make([]byte, 8)...)
	}
	for i, s := range p.Strings {
		var hdr [16]byte
		binary.LittleEndian.PutUint64(hdr[0:], uint64(len(s)))
		binary.LittleEndian.PutUint64(hdr[8:], uint64(utf8.RuneCountInString(s)))
		// 每条字面量前面先垫 8 字节的**占位头**（全 0），让它和堆上的文本对象同一个形状：
		// 句柄往前那 8 字节都是「类型字 4 ＋ 尺寸 4」。@free 识别的就是尺寸那个位置——
		// 是 0 就说明这块不在堆上，还它变成空操作。少了这 8 字节，"还自由"会把静态字面量
		// 当成堆块析构（`let s:string = "甲"` 之后析构那一个名字，症状是写坏堆的记录表）。
		d.User = append(d.User, make([]byte, 8)...)
		d.String[ir.S(i)] = len(d.User)
		d.User = append(d.User, hdr[:]...)
		d.User = append(d.User, s...)
		for len(d.User)%8 != 0 { // 下一条的头要 8 字节对齐
			d.User = append(d.User, 0)
		}
	}
	d.Desc = buildDescs(p, &d.User, d.String)
	// 栈底那个位置摆在这半截的最尾：它不是程序的变量、不是字面量、也不是形状，
	// 任何一帧都可以在入口算好它之前先比较（那个位置初值是 0，比一次也不会跳）。
	d.StackFloor = len(d.User)
	d.User = append(d.User, make([]byte, 8)...)
	// 宿主指针那个位置排在栈底之后：只有 debug 构建才有它，摆最后是为了不开的时候
	// 前面那一段的偏移一个都不动（形状表里的差值都按位置算）。
	if p.Debug {
		d.DbgHost = len(d.User)
		d.User = append(d.User, make([]byte, 8)...)
	}
	return d
}

// 形状表里一条描述占 9 格：种类、类型字、类型名、元素形状、键形状、字段条数、字段数组、
// 类型字表的条数、类型字表。指向别处的那几个槽存的都不是地址而是**从本条描述算起的字节差**——
// 数据段里没有指针型重定位可用（链接器只会给代码里的 RIP 相对引用打补丁），
// 而"同一个段里两个偏移相减"就是那个差。
//
// en: One description in the shape table occupies 9 slots: kind, type word, type name, element
// shape, key shape, field count, field array, type-word-table count, type-word-table. The slots
// that point elsewhere store not an address but a BYTE DIFFERENCE measured from this
// description - the data section has no pointer relocations available (the linker only patches
// RIP-relative references inside code), and "subtract two offsets in the same section" is
// exactly that difference.
const (
	DescSize   = 72
	DstKind    = 0
	DstTag     = 8
	DstName    = 16
	DstElem    = 24
	DstKey     = 32
	DstNFields = 40
	DstFields  = 48
	DstNTags   = 56
	DstTags    = 64

	FieldSize = 24 // 名字（也是从所属描述算起的差）、在块里的偏移、字段形状（同上）
	fName     = 0
	fOff      = 8
	fType     = 16

	TagSize = 16 // 类型字、那条形状的差（同样从所属描述算起）
	tTag    = 0
	tDesc   = 8
)

// buildDescs 排出形状表与它身后的字段数组，返回"第几条 → 在数据区里的偏移"。
//
// 两趟：先把每条描述与它的字段数组都占好位置（这一趟只算偏移），再回头填内容——
// 填的时候两边的位置都知道了，相互引用一律写成差值。
//
// en: buildDescs lays out the shape table and the field array behind it, and returns "which
// entry -> which offset in the data section". Two passes: first every description and its field
// array is allocated a place (this pass only computes offsets), then the contents are filled in
// - by then both sides know their positions, so cross references are written as differences.
func buildDescs(p *ir.Prog, user *[]byte, str map[ir.S]int) map[ir.D]int {
	at := map[ir.D]int{}
	for i := range p.Descs {
		at[ir.D(i)] = len(*user)
		*user = append(*user, make([]byte, DescSize)...)
	}
	fld := map[ir.D]int{}
	for i, dd := range p.Descs {
		if len(dd.Fields) == 0 {
			continue
		}
		fld[ir.D(i)] = len(*user)
		*user = append(*user, make([]byte, FieldSize*len(dd.Fields))...)
	}
	tg := map[ir.D]int{}
	for i, dd := range p.Descs {
		if len(dd.Tags) == 0 {
			continue
		}
		tg[ir.D(i)] = len(*user)
		*user = append(*user, make([]byte, TagSize*len(dd.Tags))...)
	}
	for i, dd := range p.Descs {
		self := at[ir.D(i)]
		// 差值都以"本条描述的起点"为原点：打印时只要拿到这一条的地址，加上差值就能定位到
		// 兄弟描述、类型名与字段数组，不必知道整张表的位置。
		poke := func(off int, target int) {
			binary.LittleEndian.PutUint64((*user)[self+off:], uint64(int64(target-self)))
		}
		set := func(off int, v int64) {
			binary.LittleEndian.PutUint64((*user)[self+off:], uint64(v))
		}
		set(DstKind, int64(dd.Kind))
		set(DstTag, dd.Tag)
		set(DstNFields, int64(len(dd.Fields)))
		set(DstNTags, int64(len(dd.Tags)))
		if tgt, ok := str[dd.Name]; ok {
			poke(DstName, tgt)
		}
		if tgt, ok := at[dd.Elem]; ok {
			poke(DstElem, tgt)
		}
		if tgt, ok := at[dd.Key]; ok {
			poke(DstKey, tgt)
		}
		if len(dd.Fields) > 0 {
			poke(DstFields, fld[ir.D(i)])
			for j, f := range dd.Fields {
				s := fld[ir.D(i)] + FieldSize*j
				put := func(off int, v int64) {
					binary.LittleEndian.PutUint64((*user)[s+off:], uint64(v))
				}
				put(fName, int64(str[f.Name]-self))
				put(fOff, f.Off)
				put(fType, int64(at[f.Type]-self))
			}
		}
		if len(dd.Tags) > 0 {
			poke(DstTags, tg[ir.D(i)])
			for j, g := range dd.Tags {
				s := tg[ir.D(i)] + TagSize*j
				binary.LittleEndian.PutUint64((*user)[s+tTag:], uint64(g.Tag))
				binary.LittleEndian.PutUint64((*user)[s+tDesc:], uint64(int64(at[g.Desc]-self)))
			}
		}
	}
	return at
}

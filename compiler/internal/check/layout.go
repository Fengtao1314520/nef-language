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

package check

// 类型的机器形状：各类型的字节宽度与堆上对象的形状。
//
// 检查器本身只判定类型是否正确，不计算宽度；后端发字段访问、造堆对象、摆静态槽
// 都要宽度。这一层在 check 里算一次，避免后端重复实现同一套规则时算出不一致的结果。
//
// 这些计算挂在 *Facts 上而不是包级函数：struct 的宽度要查它的字段表，而字段表属于
// 某一趟具体的检查，用包级全局会把上一趟的结果带到下一趟。
//
// en: the machine shape of types: byte widths and the shape of heap objects. The checker itself
// only decides whether a type is correct and computes no widths, while the backend needs widths
// for field access, heap allocation and static slots. This layer computes them once inside check
// so the backend does not re-derive the rules inconsistently. The calculations live on *Facts
// rather than in package-level functions: a struct's width needs its field table, and that table
// belongs to one specific checking pass; a package-level global would carry one pass's results
// into the next.

// FieldInfo 是摊平后的一个字段：名字、类型、在对象里的字节偏移。
// 顺序与字段摊平顺序一致：base 的字段在前，自己的在后。
//
// en: FieldInfo is one flattened field: name, type, byte offset inside the object. The order is
// the field flattening order - base fields first, then its own.
type FieldInfo struct {
	Name string
	Typ  Type
	Off  int64
}

// Sizeof 返回一个类型占几个字节。容器、文本、盒子这些句柄一律 8 字节——
// 指向堆上的对象，对象本身多大由运行期决定。
//
// `bytes` 也是句柄：块里是"字节数 + 正文"，多大取决于 `bytes.new` 那一步申请了多少格。
//
// `struct` 也走这一条：它是值类型，但存放形状用的是同一套句柄模型——槽位里、
// 字段里、容器里存放的都是那 8 个字节，指向堆上的一整块。值语义靠"交出时整份拷贝"
// 来保证（`@copy_block`），而不是靠把整块放进位置里：位置是 8 字节的定宽，
// 一个 struct 多大取决于它有几个字段，内联存放就要把整套 IR 的地址模型改成连续多槽位。
//
// en: Sizeof returns how many bytes a type occupies. The handles of containers, text and boxes
// are always 8 bytes: they point at an object on the heap whose size is decided at runtime.
// `bytes` is a handle too: the block holds "byte count + body", and its size depends on how many
// slots bytes.new asked for. A `struct` also goes through here: it is a value type, but its
// storage shape uses the same handle model - what a slot, a field or a container stores is those
// 8 bytes pointing at a whole block on the heap. Value semantics are guaranteed by "copy the
// whole thing on handover" (`@copy_block`), not by putting the block into the position:
// positions are fixed 8-byte widths, a struct's size depends on its field count, and inlining
// it would mean turning the whole IR address model into runs of consecutive slots.
func (f *Facts) Sizeof(t Type) int {
	switch t.Kind {
	case Bool, Byte:
		return 1
	case Unicode:
		return 4
	case Int, Float, Enum: // enum 与 int 同宽
		return 8
	case Str, Obj, Class, Iface, List, Dict, Set, Struct, Bytes:
		return 8
	}
	return 8
}

// BlockSize 是一个 struct 在堆上那一块占几个字节：把每个字段的存放宽度加起来。
// 空 struct 是 0 字节——照样要造一块（只为拿一个不重复的句柄），但拷 0 个字节。
//
// en: BlockSize is how many bytes a struct's heap block occupies: the sum of every field's
// storage width. An empty struct is 0 bytes - a block is still allocated (to get a unique
// handle) but 0 bytes are copied.
func (f *Facts) BlockSize(t Type) int64 {
	if t.Kind != Struct {
		return int64(f.Sizeof(t))
	}
	var sum int64
	for _, fi := range f.Fields[t.Name] {
		sum += int64(f.Sizeof(fi.Typ))
	}
	return sum
}

// Alignof 取自然对齐；窄字段不撑开后面的 8 字节字段。
//
// en: Alignof takes natural alignment; a narrow field does not push the 8-byte fields behind it
// further out.
func (f *Facts) Alignof(t Type) int {
	n := f.Sizeof(t)
	if n > 8 {
		return 8
	}
	if n == 0 {
		return 1
	}
	return n
}

// FieldOffsets 返回某个 struct／class 每个字段的字节偏移。
// 偏移在记录字段表时就算好了（recordFields），这里只是取出来。
//
// en: FieldOffsets returns the byte offset of every field of a struct/class. Offsets are
// computed when the field table is recorded (recordFields); this only reads them back.
func (f *Facts) FieldOffsets(name string) []int64 {
	fields := f.Fields[name]
	offs := make([]int64, len(fields))
	for i, fi := range fields {
		offs[i] = fi.Off
	}
	return offs
}

// ObjectSize 是造一个 class 对象要在堆上占多少字节（不含对象头那 8 字节）。
//
// en: ObjectSize is how many bytes on the heap one class object occupies, excluding the 8-byte
// object header.
func (f *Facts) ObjectSize(name string) int64 {
	fields := f.Fields[name]
	if len(fields) == 0 {
		return 0
	}
	last := fields[len(fields)-1]
	return last.Off + int64(f.Sizeof(last.Typ))
}

// recordFields 在 layout() 摊平一个类型之后调用：按自然对齐算好每个字段的偏移再存表。
//
// en: recordFields runs after layout() has flattened a type: it computes each field's offset
// under natural alignment and stores the table.
func (f *Facts) recordFields(name string, fields []field) {
	out := make([]FieldInfo, 0, len(fields))
	var cur int64
	for _, fi := range fields {
		al := int64(f.Alignof(fi.Typ))
		if al > 1 && cur%al != 0 {
			cur += al - cur%al
		}
		out = append(out, FieldInfo{Name: fi.Name, Typ: fi.Typ, Off: cur})
		cur += int64(f.Sizeof(fi.Typ))
	}
	f.Fields[name] = out
}

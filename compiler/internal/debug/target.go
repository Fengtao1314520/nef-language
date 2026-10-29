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

// target.go 是"从那块内存里读出人能看的东西"这个位置。
//
// 停机时宿主手里有：映像摆在哪个地址、发射那一步返回的检查点表、还有中间码本身
// （形状表 ir.Desc 与文本池 p.Strings 都在里面，所以解码不用再去解析数据段里那份字节）。
// 值本身一律从活内存里读：程序跑起来之后静态槽、帧槽、堆上的块都会变，编出来的那份字节不可信。
//
// en: target.go is the place that turns raw memory into something a person can read. When the
// program is stopped the host holds: the address the image was placed at, the checkpoint table
// from the emit step, and the intermediate code itself (the shape table ir.Desc and the text
// pool p.Strings are both in there, so decoding never has to re-parse the bytes in the data
// section). The values themselves are always read from live memory: after the program runs,
// static slots, frame slots and heap blocks all change, so the bytes as compiled cannot be
// trusted.
package debug

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

const (
	// 块头在句柄往前：四字节类型字、四字节数据字节数。往前数用减法，
	//  uintptr 是unsigned，加一个负数常量编译就过不去。
	hdrTagBack  = 8
	hdrSizeBack = 4

	textBytes = 0  // 文本对象：UTF-8 占几字节
	textRunes = 8  // 有几个码点
	textData  = 16 // 正文从这个偏移起

	jsonKind   = 0 // Json 那一块 48 字节：种类、整数、小数位型、文本句柄、键表、值表
	jsonInt    = 8
	jsonFloat  = 16
	jsonText   = 24
	jsonKeys   = 32
	jsonValues = 40

	jsonNull, jsonBool, jsonIntKind, jsonFloatKind, jsonStringKind, jsonListKind, jsonObjectKind = 0, 1, 2, 3, 4, 5, 6

	bucketUsed = 1 // 桶的三种状态：0 空、1 已用、2 已删除
	maxItems   = 20
	maxDepth   = 2
	maxFrames  = 64
)

// Reader 从被控程序的地址抄 n 个字节出来。同进程里读那块 VirtualAlloc 来的内存走
// ReadProcessMemory——与写进去用的 WriteProcessMemory 成对，不去绕 unsafe.Pointer 那一转。
//
// en: Reader copies n bytes out of the controlled program's address space. Inside the same
// process, reading that VirtualAlloc'd block goes through ReadProcessMemory - paired with the
// WriteProcessMemory used for writing - rather than converting a uintptr back into a pointer.
type Reader interface {
	Read(addr uintptr, n int) ([]byte, bool)
}

// Block 是一整块连续的内存，单测与"映像本身那一段"都用它：地址 = Base + 下标。
//
// en: Block is one contiguous stretch of memory, used by the unit tests and for the image
// itself: address = Base + index.
type Block struct {
	Base  uintptr
	Bytes []byte
}

func (b Block) Read(addr uintptr, n int) ([]byte, bool) {
	if addr < b.Base {
		return nil, false
	}
	off := int(addr - b.Base)
	if off+n > len(b.Bytes) {
		return nil, false
	}
	return b.Bytes[off : off+n], true
}

// Multi 按顺序问几份内存，取第一份能读到的：先问堆与静态数据（另一份 Reader），
// 读不到再问映像本身那一段。
//
// en: Multi asks several memory sources in order and takes the first that answers: heap and
// static data first (another Reader), then the image itself.
type Multi []Reader

func (m Multi) Read(addr uintptr, n int) ([]byte, bool) {
	for _, r := range m {
		if b, ok := r.Read(addr, n); ok {
			return b, true
		}
	}
	return nil, false
}

// Target 是这一趟调试手里有的那几样。
//
// en: Target is what this debugging session has in hand.
type Target struct {
	rd    Reader
	Base  uintptr // 映像摆在哪个地址：返回地址减它就是代码偏移
	Sites []codegen.Site
	Prog  *ir.Prog
	// Statics 是每一个静态槽在内存里的位置，下标就是槽号（codegen 的 DataLayout.Global 认它）。
	// 顶层那些 let／const 住在这一片，不在任何一帧上，"静态"那一栏与悬停都从这儿读。
	Statics []uintptr
}

func NewTarget(rd Reader, base uintptr, sites []codegen.Site, p *ir.Prog) *Target {
	return &Target{rd: rd, Base: base, Sites: sites, Prog: p}
}

func (t *Target) u64(addr uintptr) (uint64, bool) {
	b, ok := t.rd.Read(addr, 8)
	if !ok {
		return 0, false
	}
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56, true
}

func (t *Target) u32(addr uintptr) (uint32, bool) {
	b, ok := t.rd.Read(addr, 4)
	if !ok {
		return 0, false
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, true
}

// slot 返回这一帧上第 N 个槽位的位置：与发射那一侧同一条算式（codegen/frame.go 的 slotOff）。
//
// en: slot returns the position of slot N on this frame, using the same formula as the emit
// side (slotOff in codegen/frame.go).
func slotAddr(rbp uintptr, slot int) uintptr {
	return rbp - uintptr(8*(slot+1))
}

// Frame 是调用栈上的一个位置。
//
// en: Frame is one position in the call stack.
type Frame struct {
	Number int
	Name   string // 函数名，中间码里那一个
	File   string
	Line   int
	Col    int
	RBP    uintptr
}

// siteAt 找"偏移不大于 pc 的最后一条检查点"：返回地址指在 call 的下一条指令上，
// 而那个位置正是这条检查点自己，所以往前找最近的一条就对。
//
// en: siteAt finds "the last checkpoint whose offset is not greater than pc": a return address
// points at the instruction after the call, and that position belongs to this checkpoint
// itself, so looking back for the nearest one is correct.
func (t *Target) siteAt(off int) (codegen.Site, bool) {
	var best codegen.Site
	found := false
	for _, s := range t.Sites {
		if s.Offset <= off && (!found || s.Offset > best.Offset) {
			best, found = s, true
		}
	}
	return best, found
}

// Stack 沿 [rbp+0]（上一层的 rbp）与 [rbp+8]（返回地址）往上走。
//
// 第 0 帧的位置不能用返回地址查：那个位置存的是"call 回来接着往哪儿走"，属于**上一层**。
// 所以最上面那个位置得用停住的那一条检查点自己，从第二条记录起才轮到返回地址。
// top 是停住那一条的位置；没有就传零值，那个位置只按返回地址说。
//
// en: Stack walks up along [rbp+0] (the previous frame's rbp) and [rbp+8] (the return address).
// The position of frame 0 must not be looked up through the return address: that slot says
// "where to continue after the call returns", which belongs to the frame ABOVE. So the topmost
// position uses the checkpoint that stopped, and only from the second entry on do return
// addresses apply. top is the position of the checkpoint that stopped; pass a zero value when
// there is none and that position is described by its return address alone.
func (t *Target) Stack(rbp uintptr, top codegen.Site) []Frame {
	var out []Frame
	for len(out) < maxFrames && rbp != 0 {
		saved, ok1 := t.u64(rbp)
		ret, ok2 := t.u64(rbp + 8)
		if !ok1 || !ok2 {
			break
		}
		f := Frame{Number: len(out), RBP: rbp}
		if len(out) == 0 && top.Line != 0 {
			f.Name, f.File, f.Line, f.Col = top.Func, top.File, top.Line, top.Col
		} else if s, ok := t.siteAt(int(ret) - int(t.Base)); ok {
			f.Name, f.File, f.Line, f.Col = s.Func, s.File, s.Line, s.Col
		}
		out = append(out, f)
		next := uintptr(saved)
		if next == 0 || next <= rbp { // 链不再向上就是走到头了（或者读到了别的数据）
			break
		}
		rbp = next
	}
	return out
}

// Depth 是这一帧在调用栈上的层号，单步的 over 与 out 都按它判。
//
// en: Depth is this frame's number in the call stack; both over and out of stepping decide by
// it.
func (t *Target) Depth(rbp uintptr) int { return len(t.Stack(rbp, codegen.Site{})) }

// Local 是停下来时编辑器要列的一个位置。Addr 是那个位置在内存里的位置，内存视图要从它开始读。
//
// en: Local is one position the editor wants listed when the program is stopped. Addr is where
// that position lives in memory, and the memory view starts reading from it.
type Local struct {
	Name  string
	Type  string
	Value string
	Addr  uintptr
}

// Locals 列出某一帧上所有有名字的位置。这里给的是这一帧的全部，不按作用域筛：
// 还没写过的槽里是上一个使用者留下的字节，解不出来就照实说"读不出"，不装成 null。
//
// en: Locals lists every named position on a given frame. What is returned is the whole frame,
// not filtered by scope: a slot that has never been written holds bytes left by the previous
// occupant, and when it cannot be decoded the answer says "unreadable" rather than pretending
// to be null.
func (t *Target) Locals(f Frame) []Local {
	fn := t.lookup(f.Name)
	if fn == nil {
		return nil
	}
	var out []Local
	for _, l := range fn.Locals {
		addr := slotAddr(f.RBP, l.Slot)
		out = append(out, Local{Name: l.Name, Type: l.Type, Value: t.Value(addr, l.Kind, l.Type, 0), Addr: addr})
	}
	return out
}

// Globals 列出顶层那些名字：它们住在静态槽里，不属于任何一帧，所以调用栈上走到第几帧
// 看到的都是同一批。没给出地址的槽（比如没开调试那一趟编出来的程序）不列。
//
// en: Globals lists the top-level names: they live in static slots and belong to no frame, so
// whichever frame the call stack is on, the same batch is shown. Slots with no address given (a
// program compiled without debug, for instance) are left out.
func (t *Target) Globals() []Local {
	var out []Local
	for _, n := range t.prog().Names {
		addr, ok := t.static(n.Slot)
		if !ok {
			continue
		}
		out = append(out, Local{Name: n.Name, Type: n.Type, Value: t.Value(addr, n.Kind, n.Type, 0), Addr: addr})
	}
	return out
}

func (t *Target) static(slot int) (uintptr, bool) {
	if slot < 0 || slot >= len(t.Statics) || t.Statics[slot] == 0 {
		return 0, false
	}
	return t.Statics[slot], true
}

// Evaluate 按名字问一个位置的值：鼠标停在某个变量上，编辑器发一条求值请求，这里拿名字去那一帧查。
// 只认这一帧上有名字的位置——表达式算不了（那要在停住的那一刻跑一遍检查器与降级，
// 这一版没有那个东西），容器下标与字段路径也先不拆，悬停停在 `xs` 上看到的已经是整只容器。
//
// 帧上查不到再查顶层：停在文件顶层时，鼠标停的那些名字一个都不在帧上。
//
// en: Evaluate asks for the value of a position by name: the mouse hovers over a variable, the
// editor sends an evaluate request, and the name is looked up in that frame. Only positions
// named on this frame are recognised - expressions cannot be evaluated (that would mean running
// the checker and the lowering at the moment of the stop, and this version has no such thing),
// and container indices and field paths are not parsed either, so hovering on `xs` already
// shows the whole container. If the frame has no such name, the top level is tried: stopped at
// file top level, none of the names under the mouse are on the frame.
func (t *Target) Evaluate(f Frame, name string) (string, bool) {
	if fn := t.lookup(f.Name); fn != nil {
		for _, l := range fn.Locals {
			if l.Name == name {
				return t.Value(slotAddr(f.RBP, l.Slot), l.Kind, l.Type, 0), true
			}
		}
	}
	for _, n := range t.prog().Names {
		if n.Name != name {
			continue
		}
		if addr, ok := t.static(n.Slot); ok {
			return t.Value(addr, n.Kind, n.Type, 0), true
		}
	}
	return "", false
}

// Memory 从某个地址抄 n 个字节出来，给编辑器的内存视图用。读不出来就返回失败。
// 一次最多 4 KiB：再多也不是给人看的，而且越界的读会落到被控内存区域之外。
//
// en: Memory copies n bytes starting at an address for the editor's memory view, and says so
// when it cannot read. One call is capped at 4 KiB: more than that is not readable by a person
// anyway, and an out-of-range read would wander past the controlled region.
func (t *Target) Memory(addr uintptr, n int) ([]byte, bool) {
	if n <= 0 || n > 4096 {
		return nil, false
	}
	return t.rd.Read(addr, n)
}

// lookup 按名字找那个位置函数。不能用 ir.Prog.Func：它找不到函数时会顺手建一个，
// 调试这一侧只是想看张表，不该把被调试程序的中间码改了。
//
// en: lookup finds the position function by name. ir.Prog.Func cannot be used: it creates a
// function when it finds none, and the debugging side only wants to read a table - it must not
// modify the debugged program's intermediate code.
func (t *Target) lookup(name string) *ir.Func {
	for _, fn := range t.prog().Funcs {
		if fn.Name == name {
			return fn
		}
	}
	return nil
}

func (t *Target) prog() *ir.Prog {
	if t.Prog == nil {
		return &ir.Prog{}
	}
	return t.Prog
}

// Value 读出一个位置。kind 是降级那一路记下的六类之一，text 是写出来的类型标注。
//
// en: Value reads one position. kind is one of the six classes recorded by the lowering path,
// text is the type annotation as written.
func (t *Target) Value(addr uintptr, kind, text string, depth int) string {
	raw, ok := t.u64(addr)
	if !ok {
		return "读不出"
	}
	switch kind {
	case "float":
		return trimFloat(math.Float64frombits(raw))
	case "bool":
		if raw == 0 {
			return "false"
		}
		return "true"
	case "byte":
		return fmt.Sprintf("%d", raw)
	case "rune":
		if r := rune(raw); r > 0 && r < 0x110000 {
			return fmt.Sprintf("'%c'", r)
		}
		return fmt.Sprintf("%d", raw)
	case "int":
		return fmt.Sprintf("%d", int64(raw))
	}
	return t.handle(raw, text, depth)
}

// handle 解一个句柄。先照写出来的类型走（那一条最准：静态字面量的块头里类型字是 0，
// 只有写出来的标注能看出它是一段文本），写出来的类型判断不了时再回头查块头。
//
// en: handle decodes one handle. It first follows the type as written, which is the most
// accurate: a static literal's block header carries type word 0, and only the written
// annotation says it is a piece of text. When the written type gives no answer, the block
// header is consulted.
func (t *Target) handle(h uint64, text string, depth int) string {
	if h == 0 {
		return "null"
	}
	addr := uintptr(h)
	switch {
	case text == "string":
		return t.text(addr, depth)
	case strings.HasPrefix(text, "list<"):
		return t.list(addr, inner(text), depth)
	case strings.HasPrefix(text, "dict<"):
		k, v := kvInner(text)
		return t.dict(addr, k, v, depth)
	case strings.HasPrefix(text, "hashset<"):
		return t.set(addr, inner(text), depth)
	case text == "Json":
		return t.json(addr, depth)
	case text == "object":
		if d := t.byTag(addr); d != nil {
			return t.byDesc(h, d, depth)
		}
		return fmt.Sprintf("#%#x（装的是什么读不出）", h)
	default:
		if d := t.byName(text); d != nil {
			return t.fields(h, d, depth)
		}
		if d := t.byTag(addr); d != nil {
			return t.byDesc(h, d, depth)
		}
	}
	// 到这里说明这个位置既不符合写出来的类型、块头也给不出类型：多半是没写过的槽。
	if size, ok := t.u32(addr - hdrSizeBack); ok && size > 0 && size < 1<<28 {
		return fmt.Sprintf("#%#x（%s）", h, text)
	}
	return fmt.Sprintf("%d（不像句柄，按整数看）", int64(h))
}

func (t *Target) byDesc(h uint64, d *ir.Desc, depth int) string {
	switch d.Kind {
	case ir.KindString:
		return t.text(uintptr(h), depth)
	case ir.KindList:
		return t.list(uintptr(h), t.nameOf(d.Elem), depth)
	case ir.KindDict:
		return t.dict(uintptr(h), t.nameOf(d.Key), t.nameOf(d.Elem), depth)
	case ir.KindSet:
		return t.set(uintptr(h), t.nameOf(d.Elem), depth)
	case ir.KindStruct, ir.KindClass:
		return t.fields(h, d, depth)
	}
	return fmt.Sprintf("#%#x", h)
}

func (t *Target) text(addr uintptr, depth int) string {
	n, ok := t.u64(addr + textBytes)
	if !ok || n > 1<<20 {
		return "读不出（不像一段文本）"
	}
	b, ok := t.rd.Read(addr+textData, int(n))
	if !ok {
		return "读不出（不像一段文本）"
	}
	if len(b) > 120 {
		return fmt.Sprintf("%q…（共 %d 字节）", b[:120], n)
	}
	return fmt.Sprintf("%q", b)
}

func (t *Target) list(addr uintptr, elem string, depth int) string {
	n, ok := t.u64(addr + runtime.CellLen)
	data, ok2 := t.u64(addr + runtime.CellData)
	if !ok || !ok2 {
		return "读不出（不像一个容器）"
	}
	parts := []string{}
	for i := uint64(0); i < n && i < maxItems; i++ {
		parts = append(parts, t.item(addrOf(data)+uintptr(8*i), elem, depth))
	}
	if n > maxItems {
		parts = append(parts, fmt.Sprintf("… 共 %d 样", n))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (t *Target) set(addr uintptr, elem string, depth int) string {
	return t.walkBuckets(addr, elem, "object", depth, func(k, v string) string { return k })
}

func (t *Target) dict(addr uintptr, key, val string, depth int) string {
	return t.walkBuckets(addr, key, val, depth, func(k, v string) string { return k + ": " + v })
}

// walkBuckets 按桶表走一遍 dict／hashset：桶里三格（状态、键、值），线性探测。
// 表有多长要从 CellCap 数，已删除的那个位置（状态 2）跳过。
//
// en: walkBuckets walks the bucket table of a dict/hashset: three slots per bucket (state, key,
// value), linear probing. The table's length comes from CellCap, and a removed position (state
// 2) is skipped.
func (t *Target) walkBuckets(addr uintptr, key, val string, depth int, join func(k, v string) string) string {
	n, ok := t.u64(addr + runtime.CellLen)
	room, ok2 := t.u64(addr + runtime.CellCap)
	data, ok3 := t.u64(addr + runtime.CellData)
	if !ok || !ok2 || !ok3 {
		return "读不出（不像一张表）"
	}
	parts := []string{}
	base := addrOf(data)
	for i := uint64(0); i < room && uint64(len(parts)) < n && i < maxItems*4; i++ {
		b := base + uintptr(i*runtime.BucketPad)
		st, ok := t.u64(b)
		if !ok || st != bucketUsed {
			continue
		}
		k := t.item(b+runtime.BucketKey, key, depth)
		v := t.item(b+runtime.BucketVal, val, depth)
		parts = append(parts, join(k, v))
	}
	if uint64(len(parts)) < n {
		parts = append(parts, fmt.Sprintf("… 共 %d 样", n))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// item 读容器里的一个位置。窄元素在容器里也占满 8 字节，所以这里按同一套走。
//
// en: item reads one element of a container. A narrow element still occupies a full 8 bytes
// inside a container, so this follows the same layout.
func (t *Target) item(addr uintptr, kindText string, depth int) string {
	if depth >= maxDepth {
		return "…"
	}
	kind := "handle"
	switch kindText {
	case "int":
		kind = "int"
	case "float":
		kind = "float"
	case "boolean":
		kind = "bool"
	case "byte":
		kind = "byte"
	case "unicode":
		kind = "rune"
	}
	return t.Value(addr, kind, kindText, depth+1)
}

func (t *Target) fields(h uint64, d *ir.Desc, depth int) string {
	name := t.str(d.Name)
	if depth >= maxDepth {
		return name + " {…}"
	}
	parts := []string{}
	for _, f := range d.Fields {
		ft := t.nameOf(f.Type)
		kind := "handle"
		switch ft {
		case "int":
			kind = "int"
		case "float":
			kind = "float"
		case "boolean":
			kind = "bool"
		case "byte":
			kind = "byte"
		case "unicode":
			kind = "rune"
		}
		// 字段在块里的位置：句柄往前那 8 字节是头，正文从 +0 起，
		// 而 Field.Off 记的就是正文里的偏移（check/layout.go 那一处算的）。
		v := t.Value(uintptr(h)+uintptr(f.Off), kind, ft, depth+1)
		parts = append(parts, fmt.Sprintf("%s: %s", t.str(f.Name), v))
	}
	return name + " {" + strings.Join(parts, ", ") + "}"
}

func (t *Target) json(addr uintptr, depth int) string {
	k, ok := t.u64(addr + jsonKind)
	if !ok {
		return "读不出（不像一个 Json）"
	}
	switch k {
	case jsonNull:
		return "null"
	case jsonBool:
		v, _ := t.u64(addr + jsonInt)
		if v == 0 {
			return "false"
		}
		return "true"
	case jsonIntKind:
		v, _ := t.u64(addr + jsonInt)
		return fmt.Sprintf("%d", int64(v))
	case jsonFloatKind:
		v, _ := t.u64(addr + jsonFloat)
		return trimFloat(math.Float64frombits(v))
	case jsonStringKind:
		h, _ := t.u64(addr + jsonText)
		if h == 0 {
			return `""`
		}
		return t.text(uintptr(h), depth)
	case jsonListKind, jsonObjectKind:
		h, _ := t.u64(addr + jsonKeys)
		if k == jsonListKind {
			return t.list(uintptr(h), "Json", depth)
		}
		v, _ := t.u64(addr + jsonValues)
		return t.dict2(uintptr(h), uintptr(v), depth)
	}
	return fmt.Sprintf("Json（种类 %d 认不出）", k)
}

// dict2 是 Json 那一棵树里键表与值表分开摆的那一种：两个容器对象，一个位置对一个位置。
//
// en: dict2 is the layout used by the Json tree, where the key table and the value table are
// two separate containers, position against position.
func (t *Target) dict2(keys, vals uintptr, depth int) string {
	n, ok := t.u64(keys + runtime.CellLen)
	if !ok {
		return "读不出（不像一个对象）"
	}
	parts := []string{}
	for i := uint64(0); i < n && i < maxItems; i++ {
		kp, _ := t.u64(keys + uintptr(runtime.CellData) + uintptr(8*i))
		vp, _ := t.u64(vals + uintptr(runtime.CellData) + uintptr(8*i))
		parts = append(parts, t.text(uintptr(kp), depth)+" = "+t.json(uintptr(vp), depth+1))
	}
	if n > maxItems {
		parts = append(parts, fmt.Sprintf("… 共 %d 对", n))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// byTag 照块头那一个类型字找形状；静态字面量的头里是 0，找不到。
//
// en: byTag finds a shape from the single type word in the block header; a static literal's
// header holds 0, so nothing is found there.
func (t *Target) byTag(addr uintptr) *ir.Desc {
	tag, ok := t.u32(addr - hdrTagBack)
	if !ok || tag == 0 {
		return nil
	}
	for i := range t.prog().Descs {
		if t.prog().Descs[i].Tag == int64(tag) && t.prog().Descs[i].Kind != ir.KindBox {
			return &t.prog().Descs[i]
		}
	}
	return nil
}

// byName 照写出来的类型名找形状。名字带包根的那种（"greet.Pair"）在这里就是形状表里那一个名字。
//
// en: byName finds a shape by the type name as written. A name carrying a package root
// ("greet.Pair") is exactly the name stored in the shape table.
func (t *Target) byName(name string) *ir.Desc {
	if name == "" || name == "?" || strings.ContainsAny(name, "<, ") {
		return nil
	}
	for i := range t.prog().Descs {
		d := &t.prog().Descs[i]
		if (d.Kind == ir.KindStruct || d.Kind == ir.KindClass) && t.str(d.Name) == name {
			return d
		}
	}
	return nil
}

func (t *Target) nameOf(d ir.D) string {
	for i := range t.prog().Descs {
		if ir.D(i) == d {
			if n := t.str(t.prog().Descs[i].Name); n != "" {
				switch t.prog().Descs[i].Kind {
				case ir.KindList:
					return "list<" + t.nameOf(t.prog().Descs[i].Elem) + ">"
				case ir.KindSet:
					return "hashset<" + t.nameOf(t.prog().Descs[i].Elem) + ">"
				case ir.KindDict:
					return "dict<" + t.nameOf(t.prog().Descs[i].Key) + ", " + t.nameOf(t.prog().Descs[i].Elem) + ">"
				}
				return n
			}
			switch t.prog().Descs[i].Kind {
			case ir.KindInt:
				return "int"
			case ir.KindFloat:
				return "float"
			case ir.KindBool:
				return "boolean"
			case ir.KindByte:
				return "byte"
			case ir.KindChar:
				return "unicode"
			case ir.KindString:
				return "string"
			}
			return "?"
		}
	}
	return "?"
}

func (t *Target) str(s ir.S) string {
	list := t.prog().Strings
	if int(s) < len(list) {
		return list[s]
	}
	return ""
}

func addrOf(v uint64) uintptr { return uintptr(v) }

// inner 与 kvInner 从写出来的标注里取出尖括号里那几段：list<int> → int，
// dict<string, int> → 两段。嵌套的按最后一个逗号切，够用且不再多写一份解析器。
//
// en: inner and kvInner take the segments inside the angle brackets out of the written
// annotation: list<int> gives int, dict<string, int> gives two. Nesting is cut at the last
// comma - enough, and it avoids writing a second parser.
func inner(text string) string {
	i := strings.IndexByte(text, '<')
	j := strings.LastIndexByte(text, '>')
	if i < 0 || j <= i {
		return "?"
	}
	return strings.TrimSpace(text[i+1 : j])
}

func kvInner(text string) (string, string) {
	body := inner(text)
	if i := strings.LastIndexByte(body, ','); i >= 0 {
		return strings.TrimSpace(body[:i]), strings.TrimSpace(body[i+1:])
	}
	return body, "?"
}

// trimFloat 是小数值的显示文本：走 Go 的最短往返。text.to_float 用的是另一条算法，
// 极端值上相差最后一个 ulp——调试面板要的是"这个槽位大概是多少"，不在那个精度上作判断。
//
// en: trimFloat is the display text of a float value: it takes Go's shortest roundtrip.
// text.to_float uses a different algorithm, and the two differ by one ulp at the extremes -
// the debug panel shows "roughly what value this slot holds", which is not judged at that
// precision.
func trimFloat(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%.1f", v)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

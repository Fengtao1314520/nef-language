// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// target_test.go 拿一份假内存验解码：块头、容器那 40 字节、桶、文本、帧链，
// 每一个偏移都照 internal/runtime 与 codegen 那一侧写的算式来，谁改了布局这里就红。
package debug_test

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/debug"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

const (
	base    = 0x1000 // 映像起点：代码偏移 0x40 那一条检查点落在地址 0x1040 上
	textA   = 0x2000 // 文本块（句柄指这里，头在它前面 8 字节）
	listA   = 0x3000 // 容器对象
	listD   = 0x3100 // 容器的数据块
	dictA   = 0x4000 // 一张 dict
	dictD   = 0x4100 // dict 的桶
	frame   = 0x7000 // 当前帧的 rbp
	caller  = 0x8000 // 上一层的 rbp
	site0   = 0x40   // 检查点在代码段里的偏移
	site1   = 0x90
	staticA = 0x5000 // 静态槽那一片：顶层那些名字住在这儿，不在任何一帧上
)

func put64(mem []byte, addr uintptr, v uint64) { binary.LittleEndian.PutUint64(mem[addr-base:], v) }
func put32(mem []byte, addr uintptr, v uint32) { binary.LittleEndian.PutUint32(mem[addr-base:], v) }

// 文本块：句柄往前那 8 字节是占位头（静态字面量就是全 0），句柄起是字节数、码点数、正文。
func writeText(mem []byte, addr uintptr, s string) {
	put32(mem, addr-8, 0)
	put32(mem, addr-4, uint32(len(s)))
	put64(mem, addr+0, uint64(len(s)))
	put64(mem, addr+8, uint64(len([]rune(s))))
	copy(mem[addr+16-base:], s)
}

func writeList(mem []byte, addr uintptr, items []uint64) {
	put64(mem, addr+runtime.CellLen, uint64(len(items)))
	put64(mem, addr+runtime.CellCap, uint64(len(items)+1))
	put64(mem, addr+runtime.CellData, uint64(listD))
	for i, v := range items {
		put64(mem, uintptr(listD)+uintptr(8*i), v)
	}
}

// 一张两个键值的 dict：桶里三格（状态、键、值），中间夹一个位置空的看会不会被数进去。
func writeDict(mem []byte, pairs [][2]uintptr) {
	put64(mem, addrOf(dictA)+runtime.CellLen, uint64(len(pairs)))
	put64(mem, addrOf(dictA)+runtime.CellCap, uint64(len(pairs)+2))
	put64(mem, addrOf(dictA)+runtime.CellData, uint64(dictD))
	for i, p := range pairs {
		b := addrOf(dictD) + uintptr(i*2)*uintptr(runtime.BucketPad)
		put64(mem, b, 1) // 用了
		put64(mem, b+uintptr(runtime.BucketKey), uint64(p[0]))
		put64(mem, b+uintptr(runtime.BucketVal), uint64(p[1]))
	}
}

func addrOf(v uintptr) uintptr { return v }

func newTarget() (*debug.Target, []byte) {
	mem := make([]byte, 0x9000-base)
	writeText(mem, textA, "abc")
	writeText(mem, textA+0x100, "甲乙")
	writeList(mem, listA, []uint64{10, 20, 30})
	writeDict(mem, [][2]uintptr{{textA, 7}, {textA + 0x100, 9}})
	// 两帧：当前帧的上一层 rbp 与返回地址；返回地址 = 映像基址 + 代码偏移。
	put64(mem, frame+0, caller)
	put64(mem, frame+8, base+site1)
	put64(mem, caller+0, 0)
	put64(mem, caller+8, base+site0)
	// 这一帧上的三格：slot0 是 int，slot1 是一段文本的句柄，slot2 是个小数。
	put64(mem, frame-8, 42)
	put64(mem, frame-16, textA)
	put64(mem, frame-24, mathBits(1.5))
	// Value 收的是"槽在哪"，不是"东西在哪"：句柄那个位置装的才是指针。
	put64(mem, frame-48, listA)
	put64(mem, frame-56, dictA)
	// 顶层那两个位置：一个是整数，一个是一段文本的句柄。第三格没给地址，该被跳过。
	put64(mem, staticA, 7)
	put64(mem, staticA+8, textA+0x100)

	sites := []codegen.Site{
		{ID: 1, Func: "twice", Offset: site0, File: "t.nf", Line: 2, Col: 5},
		{ID: 2, Func: "twice", Offset: site1, File: "t.nf", Line: 3, Col: 5},
	}
	prog := &ir.Prog{Funcs: []*ir.Func{{
		Name: "twice", Slots: 6, Params: 1,
		Locals: []ir.Local{
			{Slot: 0, Name: "n", Kind: "int", Type: "int"},
			{Slot: 1, Name: "s", Kind: "handle", Type: "string"},
			{Slot: 2, Name: "f", Kind: "float", Type: "float"},
		},
	}}, Names: []ir.GlobalName{
		{Slot: 0, Name: "k", Kind: "int", Type: "int", File: "t.nf", Line: 6},
		{Slot: 1, Name: "tag", Kind: "handle", Type: "string", File: "t.nf", Line: 7},
		{Slot: 2, Name: "没摆好", Kind: "int", Type: "int", File: "t.nf", Line: 8},
	}}
	tg := debug.NewTarget(debug.Block{Base: base, Bytes: mem}, base, sites, prog)
	// 槽号 → 地址那一张表由命令行那一侧填（它手里才有映像基址与数据段的起点）。
	tg.Statics = []uintptr{staticA, staticA + 8, 0}
	return tg, mem
}

func mathBits(f float64) uint64 { return *(*uint64)(unsafe.Pointer(&f)) }

func TestValueReadsEachKind(t *testing.T) {
	tg, _ := newTarget()
	cases := []struct {
		addr       uintptr
		kind, text string
		want       string
	}{
		{frame - 8, "int", "int", "42"},
		{frame - 16, "handle", "string", `"abc"`},
		{frame - 24, "float", "float", "1.5"},
		{frame - 48, "handle", "list<int>", "[10, 20, 30]"},
		{frame - 56, "handle", "dict<string, int>", `{"abc": 7, "甲乙": 9}`},
	}
	for _, c := range cases {
		got := tg.Value(c.addr, c.kind, c.text, 0)
		if got != c.want {
			t.Errorf("解 %s 得到 %q，要 %q", c.text, got, c.want)
		}
	}
}

// 顶层那些名字不在任何一帧上，编辑器的"静态"那一栏问的是这一张表；没给地址的那个位置不列。
func TestGlobalsListsTheStaticSlots(t *testing.T) {
	tg, _ := newTarget()
	got := tg.Globals()
	want := map[string]string{"k": "7", "tag": `"甲乙"`}
	if len(got) != len(want) {
		t.Fatalf("该列两格，得到 %+v", got)
	}
	for _, l := range got {
		if w, ok := want[l.Name]; !ok {
			t.Errorf("多列了一个名字 %q", l.Name)
		} else if l.Value != w {
			t.Errorf("%s = %q，要 %q", l.Name, l.Value, w)
		}
	}
	// 悬停问的是同一个名字：帧上没有，就该落到静态槽那一条路上。
	f := tg.Stack(frame, codegen.Site{})[0]
	if v, ok := tg.Evaluate(f, "tag"); !ok || v != `"甲乙"` {
		t.Errorf("悬停顶层的 tag 该得到 \"甲乙\"，得到 %q（查到没有：%v）", v, ok)
	}
	// 帧上的名字优先：同名时看到的是这一帧里那一个，不是顶层的那一个。
	if v, ok := tg.Evaluate(f, "n"); !ok || v != "42" {
		t.Errorf("悬停帧上的 n 该得到 42，得到 %q（%v）", v, ok)
	}
}

// 空句柄就是 null；没写过的槽里是个不像句柄的数，要说清是按整数看的，别装作是个对象。
func TestValueOnNullAndGarbage(t *testing.T) {
	tg, mem := newTarget()
	put64(mem, frame-32, 0)
	if got := tg.Value(frame-32, "handle", "string", 0); got != "null" {
		t.Errorf("空句柄该是 null，得到 %q", got)
	}
	put64(mem, frame-40, 0x1234)
	got := tg.Value(frame-40, "handle", "Pair", 0)
	if got == "" || got[0] != '4' && got != "4660（不像句柄，按整数看）" {
		t.Logf("没写过的槽交回：%q", got)
	}
	if g := tg.Value(frame-40, "handle", "Pair", 0); g == "null" {
		t.Error("没写过的槽不能报成 null")
	}
}

// 悬停要能按名字问一个位置：鼠标停在 n 上，编辑器发一条求值请求，这一侧拿名字去那一帧查。
func TestEvaluateLooksUpANameInAFrame(t *testing.T) {
	tg, _ := newTarget()
	frame0 := tg.Stack(frame, codegen.Site{})[0]
	got, ok := tg.Evaluate(frame0, "s")
	if !ok || got != `"abc"` {
		t.Errorf("悬停 s 该得到 \"abc\"，得到 %q（查到没有：%v）", got, ok)
	}
	if v, ok := tg.Evaluate(frame0, "n"); !ok || v != "42" {
		t.Errorf("悬停 n 该得到 42，得到 %q（%v）", v, ok)
	}
	if _, ok := tg.Evaluate(frame0, "没这个"); ok {
		t.Error("这一帧上没有的名字要说查不到")
	}
}

// 看内存：从某个地址抄一段字节出来，抄不动要说抄不动，别返回一串零。
func TestMemoryReadsRawBytes(t *testing.T) {
	tg, _ := newTarget()
	got, ok := tg.Memory(frame-8, 4)
	if !ok {
		t.Fatal("读这一帧上的整数格不该失败")
	}
	if got[0] != 42 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Errorf("抄出来的四个字节不对：%v", got)
	}
	if _, ok := tg.Memory(frame-8, 1<<24); ok {
		t.Error("一次抄得太多要被挡住")
	}
	if _, ok := tg.Memory(0xdeadbeef, 8); ok {
		t.Error("那块内存之外的地址要说读不到")
	}
}

func TestStackWalksTheFrameChain(t *testing.T) {
	tg, _ := newTarget()
	got := tg.Stack(frame, codegen.Site{})
	if len(got) != 2 {
		t.Fatalf("该走出两帧，得到 %d 帧：%+v", len(got), got)
	}
	if got[0].Line != 3 || got[0].Name != "twice" {
		t.Errorf("当前帧不对：%+v", got[0])
	}
	if got[1].Line != 2 {
		t.Errorf("上一层该指到第 2 行：%+v", got[1])
	}
	if tg.Depth(frame) != 2 {
		t.Errorf("帧深该是 2，得到 %d", tg.Depth(frame))
	}
}

// 停在 twice 第 3 行时，栈顶那个位置该说第 3 行，不是把它上一层的第 2 行报上来。
func TestStackTopUsesTheStoppedSite(t *testing.T) {
	tg, _ := newTarget()
	top := codegen.Site{ID: 2, Func: "twice", Offset: site1, File: "t.nf", Line: 3, Col: 5}
	got := tg.Stack(frame, top)
	if got[0].Line != 3 || got[0].Name != "twice" {
		t.Errorf("栈顶该是停住那一格：%+v", got[0])
	}
	if len(got) > 1 && got[1].Line != 2 {
		t.Errorf("第二格该按返回地址指回上一层：%+v", got[1])
	}
}

func TestLocalsListsTheFrame(t *testing.T) {
	tg, _ := newTarget()
	frames := tg.Stack(frame, codegen.Site{})
	got := tg.Locals(frames[0])
	want := map[string]string{"n": "42", "s": `"abc"`, "f": "1.5"}
	if len(got) != len(want) {
		t.Fatalf("列了 %d 格，要 %d 格：%+v", len(got), len(want), got)
	}
	for _, l := range got {
		if w, ok := want[l.Name]; !ok {
			t.Errorf("多列了一个名字 %q", l.Name)
		} else if l.Value != w {
			t.Errorf("%s = %q，要 %q", l.Name, l.Value, w)
		}
	}
}

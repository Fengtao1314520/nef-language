// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// generic_test.go 钉泛型实例在调试变量视图里读得出：形状表里那一行的名字带着类型实参
// （`Cell<int>`），堆上那一块既能按写出来的类型名找到，也能在标注只写 `object` 时按块头
// 那一个类型字找到；静态字面量那一块头里是 0，只能按写出来的那一串名字找。
//
// en: generic_test.go pins that a generic instance reads in the variable view: the shape table row
// carries the arguments in its name (Cell<int>), a heap block is found either by that written name
// or - when the annotation only says object - by the type word in its header, while a static
// literal's header holds 0, so only the written name identifies it.
package debug_test

import (
	"testing"

	"neflang.org/nf/internal/debug"
	"neflang.org/nf/internal/ir"
)

const (
	classA    = uintptr(0x6000) // 堆上那一只 Cell<int>
	staticLit = uintptr(0x6200) // 静态字面量那一只：块头全 0
	slotHeap  = uintptr(0x6400) // 槽：装 classA
	slotPlain = uintptr(0x6408) // 槽：装 staticLit
)

func newInstTarget() *debug.Target {
	mem := make([]byte, 0x9000-base)
	put32(mem, classA-8, 77) // 块头：类型字
	put64(mem, classA, 5)    // 字段 v
	put32(mem, staticLit-8, 0)
	put32(mem, staticLit-4, 0)
	put64(mem, staticLit, 6)
	put64(mem, slotHeap, uint64(classA))
	put64(mem, slotPlain, uint64(staticLit))
	prog := &ir.Prog{
		Strings: []string{"Cell<int>", "v", ""},
		Descs: []ir.Desc{
			{Kind: ir.KindInt, Name: 2},
			{Kind: ir.KindClass, Tag: 77, Name: 0,
				Fields: []ir.Field{{Name: 1, Off: 0, Type: 0}}},
		},
	}
	return debug.NewTarget(debug.Block{Base: base, Bytes: mem}, base, nil, prog)
}

func TestValueReadsAGenericInstance(t *testing.T) {
	tg := newInstTarget()
	cases := []struct {
		where string
		slot  uintptr
		text  string
		want  string
	}{
		{"堆上那一只按写出来的类型名", slotHeap, "Cell<int>", "Cell<int> {v: 5}"},
		{"堆上那一只标注只说 object", slotHeap, "object", "Cell<int> {v: 5}"},
		{"静态字面量那一只", slotPlain, "Cell<int>", "Cell<int> {v: 6}"},
	}
	for _, c := range cases {
		if got := tg.Value(c.slot, "handle", c.text, 0); got != c.want {
			t.Errorf("%s解出来是 %q，要 %q", c.where, got, c.want)
		}
	}
}

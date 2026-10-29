// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// stock_e2e_test.go 覆盖 samples/stock.nf 的那条路径：`struct` 值存进 `dict` 的位置。
//
// 此前五份示例程序没有一份同时用到这两样，"容器位置里是一整块 struct"这条运行期路径也就从未
// 被实际执行过。这里两条测试各验证一件事：
//   - **读出来是整份拷贝**：修改读出的 `struct`，位置里的内容不变（值类型语义）；
//   - **覆盖就是换值**：同一个键再写一次，条目数不增加，值被替换。
package lower_test

import (
	"os"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// 三条测试的期望值都先手写、再与实际运行核对：改动测试后重跑一遍，不把实得值直接抄成期望。

func TestSampleStock(t *testing.T) {
	const path = "../../../samples/stock.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, string(raw)), "stock.exe")
	want := "几种 3 一共 18 最多的一格 12\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("stock.nf跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

// TestStructInDictCell 读出一个位置并修改它，位置里的内容不受影响；再写同一个键是换值、不增加条目。
func TestStructInDictCell(t *testing.T) {
	got, code := run(t, `
struct P {
    name:string
    qty:int
}
let d:dict<string, P> = {}
let k1:string = "甲".slice(0, 1)
d.add(k1, P{name: "甲", qty: 3})
let a:P = d["甲"]
a.qty = 9
console.print(a.qty, d["甲"].qty, length(d))
let k2:string = "甲".slice(0, 1)
d[k2] = P{name: "甲", qty: 5}
console.print(d["甲"].qty, length(d))
`)
	if got != "9 3 1\n5 1\n" || code != 0 {
		t.Fatalf("struct 摆在位置里读出来不该是同一块：得到 %q 码 %d，要 9 3 1 / 5 1 码 0", got, code)
	}
}

// TestStructCellByComputedKey 键是计算所得文本、值是整块 struct：
// 用另一块内容相同的文本去查，必须命中同一个条目（文本判等比内容）。
func TestStructCellByComputedKey(t *testing.T) {
	got, code := run(t, `
struct P {
    name:string
    qty:int
}
let d:dict<string, P> = {}
let key:string = "abc".slice(0, 3)
d.add(key, P{name: "甲", qty: 7})
let other:string = "abc".to_upper().to_lower()
console.print(length(d), d[other].qty, d[other].name)
`)
	if got != "1 7 甲\n" || code != 0 {
		t.Fatalf("算出来的键查不回那个位置：得到 %q 码 %d，要 1 7 甲 码 0", got, code)
	}
}

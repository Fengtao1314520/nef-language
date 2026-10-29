// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// `d["甲"].n = 4` 这类写法此前到不了运行期：`placeOf` 取容器尖括号里的第一个类型当"槽位里存的类型"，
// 对 dict 来说那一个是键类型。读回来的是键类型，降级去它的字段表里找 `n`，报的是
// "string 里没有字段 n：前端交出来的字段表对不上"——前端交出的字段表没有错，是后端读错了槽位的类型。
// 同一条写在 list 上一直正常（`xs[0].n = 4`），因为 list 尖括号里的类型就是元素类型。

func TestDictCellFieldWrite(t *testing.T) {
	got, code := run(t, `struct Rec {
    n:int
}
let d:dict<string, Rec> = {"甲": Rec { n: 1 }}
d["甲"].n = 4
console.print(d["甲"].n)
`)
	if want := "4\n"; got != want || code != 0 {
		t.Fatalf("字典那一格里再点字段并赋值：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 与名字进容器的规则对照着看：那个位置收下的是拷贝，写它不影响名字那一份
	got, code = run(t, `struct Rec {
    n:int
}
let one:Rec = Rec { n: 1 }
let d:dict<string, Rec> = {"甲": one}
d["甲"].n = 5
console.print(one.n, d["甲"].n)
`)
	if want := "1 5\n"; got != want || code != 0 {
		t.Fatalf("字典那一格与源各自独立这条：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 小数与文本在同一个位置里也能写对：键是文本、值是数
	got, code = run(t, `let d:dict<string, int> = {"甲": 1}
d["甲"] = 7
console.print(d["甲"])
`)
	if want := "7\n"; got != want || code != 0 {
		t.Fatalf("`d[键] = 值` 那条照旧要能走：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 每圈写一次同一个位置，不泄漏内存
	const prog = `struct Rec {
    n:int
}
let d:dict<string, Rec> = {"甲": Rec { n: 1 }}
let total:int = 0
for (i:int in 0 until @N@) {
    d["甲"].n = i
    total = total + d["甲"].n
}
console.print(total)
`
	if _, code := run(t, strings.ReplaceAll(prog, "@N@", "8")); code != 0 {
		t.Fatalf("循环里逐圈写那一格跑坏了（码 %d）", code)
	}
	if per := heapSlope(t, prog); per > 2 {
		t.Fatalf("逐圈写字典那一格每圈留 %g 字节", per)
	}
}

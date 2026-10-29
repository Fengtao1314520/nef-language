// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// listremove_e2e_test.go 覆盖 `list`.remove(值)` 的两条相等判据。
//
// 运行期"怎么算相等"的模式与 @key_eq 同一条规则：**0 比那 64 位，1 比文本内容**。
// 此前 @list_remove 头上判反了方向，两种元素类型各出一种错误：数字 list 按文本比较
//（把 1、2、3 当指针读，有时当场段错误，有时读错位置也不报错），文本 list 只按句柄判断
//（`xs.remove(当场算出的同一个串)` 删不掉，只有相同字面量才命中）。
// 这条测试源于实战样例一轮的实际运行——此前只有 hashset／dict 的 remove 有测试，`list` 一条没有。

package lower_test

import "testing"

func TestListRemoveByValue(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"删中间那个数", `
let xs:list<int> = [1, 2, 3]
xs.remove(2)
console.print(length(xs), xs[0], xs[1])
`, "2 1 3\n"},
		{"删最后那个数", `
let xs:list<int> = [1, 2, 3]
xs.remove(3)
console.print(length(xs), xs[1])
`, "2 2\n"},
		{"删第一个数", `
let xs:list<int> = [1, 2, 3]
xs.remove(1)
console.print(length(xs), xs[0])
`, "2 2\n"},
		{"按内容删文本：算出来的那一个串", `
let xs:list<string> = ["甲乙", "丙丁"]
let key:string = "丙" + "丁"
xs.remove(key)
console.print(length(xs), xs[0])
`, "1 甲乙\n"},
		{"按内容删文本：字面量那一个串", `
let xs:list<string> = ["甲乙", "丙丁"]
xs.remove("甲乙")
console.print(length(xs), xs[0])
`, "1 丙丁\n"},
		{"没有那一个值就原样过去", `
let xs:list<int> = [1, 2, 3]
xs.remove(9)
console.print(length(xs))
`, "3\n"},
	}
	for _, one := range cases {
		got, code := run(t, one.src)
		if got != one.want || code != 0 {
			t.Errorf("%s：\n got %q 码 %d\nwant %q", one.name, got, code, one.want)
		}
	}
}

// TestListRemoveFreesTheRemovedOne 覆盖一处已修复的泄漏：`xs.remove(值)` 删掉的那个元素
// 被后面的元素盖住，语言里再没有名字指着它，当时无人析构——两万圈每圈留一块（实测峰值 800,224 字节，
// 同一段代码不删是 264）。现在 @list_remove 返回那个句柄，降级按元素形状当场析构。
//
// 第二条用例覆盖挪动完成后的尾部位置：那个位置与新的最后一个位置指向同一块，不抹零就有两个持有者，
// 这只 list 析构时会把同一块析构两次。删到只剩一个位置、再删掉最后一个位置，是同时遇到这两件事的最短写法。
func TestListRemoveFreesTheRemovedOne(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲", "乙", "丙"]
xs.remove("甲")
console.print(length(xs), xs)
xs.remove("丙")
console.print(length(xs), xs)
xs.remove("乙")
console.print(length(xs), xs)
xs.add("新的")
console.print(length(xs), xs)

let ns:list<int> = [1, 2, 3]
ns.remove(2)
ns.remove(9)
console.print(ns)
`)
	want := "2 [\"乙\", \"丙\"]\n1 [\"乙\"]\n0 []\n1 [\"新的\"]\n[1, 3]\n"
	if got != want || code != 0 {
		t.Fatalf("删掉之后剩下的与再往里添不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// heapAt 会把 NEF_HEAP 设进这一条测试的子进程，跑完再比对别的输出就会多出一行，所以放最后
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let xs:list<string> = ["甲乙" + "丙丁", "尾"]
    xs.remove("甲乙丙丁")
}
`); used > 4000 {
		t.Fatalf("两万圈各删一次，峰值 %d 字节：被删那一份大概还没人拆", used)
	}
}

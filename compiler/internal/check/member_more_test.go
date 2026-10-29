// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// 覆盖两条范围问题：list 新添的六个动作能装什么、进制那一对的进制怎么写。
// 两句都是整句比对（`expectErr` 认 `Error.Msg` 相等），改一个字就红。

func TestListMoreRefuseHandleElements(t *testing.T) {
	unfit := "C 交不出一份自己的拷贝：C 那一类东西一交就是两个持有者，只有值类型与文本交得回来"
	for _, src := range []string{
		"class C { i: int }\nlet a:list<C> = []\nlet b:list<C> = []\na.add_all(b)\n",
		"class C { i: int }\nlet a:list<C> = []\nconsole.print(a.to_set())\n",
		"class C { i: int }\nlet a:list<C> = []\nconsole.print(a.slice(0, 1))\n",
	} {
		expectErr(t, src, unfit)
	}
	// 值类型与文本走得通；插入与移除不走这一条（它们不属于"交出一份拷贝"那一族）
	expectOK(t, `
let a:list<string> = ["甲"]
let b:list<string> = ["乙"]
a.add_all(b)
a.insert_at(0, "丙")
console.print(length(a), a.remove_at(0), length(a.to_set()), length(a.slice(1, 2)))
a.reverse()
console.print(a)
let ns:list<int> = [3, 1]
ns.add_all([2])
console.print(ns, length(ns.to_set()))
`)
}

func TestRadixLiteralOnly(t *testing.T) {
	expectErr(t, `
let r:int = 16
console.print(text.to_int_in_base("ff", r))
`, "text.to_int_in_base 的进制要写成一个数，不能是算出来的")
	expectErr(t, `console.print(number.to_string_in_base(255, 12))`,
		"number.to_string_in_base 的进制只准 2、8、10、16，这里是 12")
	expectErr(t, `console.print(text.to_int_in_base("ff", 10 + 6))`,
		"text.to_int_in_base 的进制要写成一个数，不能是算出来的")
	expectOK(t, `
console.print(text.to_int_in_base("ff", 16), text.to_int_in_base("1010", 2), text.to_int_in_base("777", 8), text.to_int_in_base("42", 10))
console.print(number.to_string_in_base(255, 16), number.to_string_in_base(10, 8), number.to_string_in_base(0, 2))
`)
}

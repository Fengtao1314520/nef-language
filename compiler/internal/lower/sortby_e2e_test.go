// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// sortby_e2e_test.go 覆盖 `list.sort_by(比较器)`：比较器是一份函数值，
// 排序就地完成、稳定，元素可以是任意类型（这次用 class 字段、整数倒序与小数三档）。
//
// en: sortby_e2e_test.go covers list.sort_by(comparator): the comparator is a function value,
// the sort happens in place and stays stable, and elements may be of any type (a class field,
// integers in reverse and floats are the three cases here).

package lower_test

import "testing"

func TestSortByClassField(t *testing.T) {
	got, code := run(t, `
class Player {
    name:string
    age:int
}

method by_age(a:Player, b:Player):int {
    return a.age - b.age
}

let xs:list<Player> = [new Player(name: "甲", age: 30), new Player(name: "乙", age: 20), new Player(name: "丙", age: 25)]
xs.sort_by(by_age)
for (p:Player in xs) {
    console.print(p.name, p.age)
}
`)
	want := "乙 20\n丙 25\n甲 30\n"
	if got != want || code != 0 {
		t.Fatalf("按 class 字段排序不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

func TestSortByReversedComparator(t *testing.T) {
	got, code := run(t, `
method later_first(a:int, b:int):int {
    return b - a
}

method later_first_text(a:string, b:string):int {
    return length(b) - length(a)
}

let xs:list<int> = [3, 1, 2]
xs.sort_by(later_first)
console.print(xs)

let ws:list<string> = ["甲", "乙乙", "丙丙丙"]
ws.sort_by(later_first_text)
console.print(ws)
`)
	want := "[3, 2, 1]\n[\"丙丙丙\", \"乙乙\", \"甲\"]\n"
	if got != want || code != 0 {
		t.Fatalf("比较器反向时顺序不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestSortByFloatElements 覆盖小数元素：比较器的两个参数走 xmm0／xmm1，
// 与直接调用同一套摆法，不能把位型当整数交进 rcx／rdx。
func TestSortByFloatElements(t *testing.T) {
	got, code := run(t, `
method by_value(a:float, b:float):int {
    if (a < b) {
        return -1
    }
    if (a > b) {
        return 1
    }
    return 0
}

let xs:list<float> = [2.5, 1.25, 3.75]
xs.sort_by(by_value)
console.print(xs)
`)
	want := "[1.25, 2.5, 3.75]\n"
	if got != want || code != 0 {
		t.Fatalf("小数按比较器排序不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

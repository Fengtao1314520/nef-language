// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// narrow_e2e_test.go 实际跑一遍 `if (o is C)` 那一支里的字段读取：窄化之后
// `o.field` 直接落到 C 的字段偏移上，不必再写 `as C`；`&&` 右半边同样生效。
//
// en: narrow_e2e_test.go runs field reads inside `if (o is C)`: after narrowing, o.field lands
// directly on C's field offset with no second `as C`, and the right half of && behaves the same.

package lower_test

import "testing"

func TestIsNarrowingBranchReadsFields(t *testing.T) {
	got, code := run(t, `
class Cat {
    lives:int
    name:string
}

struct Pair {
    left:int
    right:int
}

let a:object = new Cat(lives: 9, name: "甲") as object
if (a is Cat) {
    console.print(a.lives, a.name)
}
if (a is Cat && a.lives > 5) {
    console.print("many")
} else {
    console.print("few")
}

let b:object = Pair { left: 3, right: 4 } as object
if (b is Pair) {
    console.print(b.left + b.right)
}
`)
	want := "9 甲\nmany\n7\n"
	if got != want || code != 0 {
		t.Fatalf("窄化之后读字段不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestIsNarrowingBranchReturns 覆盖分支里直接 return：窄化只在那一支里，返回值照常算出来。
func TestIsNarrowingBranchReturns(t *testing.T) {
	got, code := run(t, `
class Cat {
    lives:int
}

method lives_of(o:object):int {
    if (o is Cat) {
        return o.lives
    }
    return 0
}

console.print(lives_of(new Cat(lives: 7) as object))
console.print(lives_of(5 as object))
`)
	want := "7\n0\n"
	if got != want || code != 0 {
		t.Fatalf("分支里 return 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

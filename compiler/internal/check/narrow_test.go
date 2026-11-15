// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// narrow_test.go 钉住 `if (o is C)` 那一层类型窄化：那一支里 `o` 按 C 看，
// 出了那一支、`else` 里、`||` 右边都不看它；`&&` 右边继承左边认出来的那一只。
//
// en: narrow_test.go pins the type narrowing of `if (o is C)`: inside the branch o is seen as C;
// outside it, in the else branch and on the right of || it is not; the right half of && inherits
// what the left half established.
package check

import "testing"

const narrowHead = `class Cat {
    lives:int
    name:string
}

class Dog {
    bones:int
}
`

func TestIsNarrowingInBranch(t *testing.T) {
	expectOK(t, narrowHead+`
let o:object = new Cat(lives: 9, name: "甲") as object
if (o is Cat) {
    console.print(o.lives, o.name)
}
`)
	// && 链：右半边看见左边认出来的那一只
	expectOK(t, narrowHead+`
let o:object = new Cat(lives: 9, name: "甲") as object
if (o is Cat && o.lives > 0) {
    console.print(o.name)
}
`)
	// 收窄后的类型能传给要猫的函数
	expectOK(t, narrowHead+`
method pet(c:Cat):int {
    return c.lives
}
let o:object = new Cat(lives: 9, name: "甲") as object
if (o is Cat) {
    console.print(pet(o))
}
`)
}

func TestIsNarrowingDoesNotEscape(t *testing.T) {
	// 出了那一支就不再看成 Cat
	expectErr(t, narrowHead+`
let o:object = new Cat(lives: 9, name: "甲") as object
if (o is Cat) {
    console.print(o.lives)
}
console.print(o.lives)
`, "object 上没有 lives")
	// else 那一支不继承
	expectErr(t, narrowHead+`
let o:object = new Cat(lives: 9, name: "甲") as object
if (o is Cat) {
    console.print(o.lives)
} else {
    console.print(o.lives)
}
`, "object 上没有 lives")
	// || 右边不窄化
	expectErr(t, narrowHead+`
let o:object = new Cat(lives: 9, name: "甲") as object
if (o is Dog || o.lives > 0) {
    console.print("ok")
}
`, "object 上没有 lives")
}

// TestIsNarrowingStruct 钉住 `struct` 那一侧：盒子装着结构值时同样收窄，字段读得到。
func TestIsNarrowingStruct(t *testing.T) {
	expectOK(t, `struct Pair {
    left:int
    right:int
}
let o:object = Pair { left: 1, right: 2 } as object
if (o is Pair) {
    console.print(o.left + o.right)
}
`)
}

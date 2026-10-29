// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// 三处先前只有后端才报出来的写法，提前到检查阶段拒绝。
//
// 共同的毛病是同一件事：检查放行、`nf build` 或直接运行才报一句"后端还没做到这些：…"，
// 等于把一条语言规则说成后端少做了一块。"不交回值的动作放在要值的位置"已经按这个方向
// 改过一次，这三处是同一方向的剩下几处。

// TestLengthRefusesOtherTypes `length` 数的是三种容器、文本与字节块的长度，
// 别的类型没有"多长"这一件事。先前这四样都过了检查，到发码才报"length 收不了 int"。
func TestLengthRefusesOtherTypes(t *testing.T) {
	for _, one := range []struct{ src, want string }{
		{`console.print(length(5))`, "length 数不了 int：只有 list、dict、hashset、string 与 bytes 这五样有长度"},
		{`console.print(length(true))`, "length 数不了 boolean：只有 list、dict、hashset、string 与 bytes 这五样有长度"},
		{`console.print(length(1.5))`, "length 数不了 float：只有 list、dict、hashset、string 与 bytes 这五样有长度"},
		{`console.print(length('甲'))`, "length 数不了 unicode：只有 list、dict、hashset、string 与 bytes 这五样有长度"},
		{`class Bag {
    v:int
}
let b:Bag = new Bag(v: 1)
console.print(length(b))`, "length 数不了 Bag：只有 list、dict、hashset、string 与 bytes 这五样有长度"},
	} {
		expectErr(t, one.src, one.want)
	}
	for _, src := range []string{
		`console.print(length("甲乙"), length([1, 2]), length({"甲": 1}), length({1}))`,
		`console.print(length(args))`,
	} {
		expectOK(t, src)
	}
}

// TestNullCannotBeBoxed 盒子只装 `class`、`struct` 与值类型。
// `null as object` 先前过了检查，到发码才报"null 的类型字没排上"。
// 声明里给 object 那一个位置放 null 是另一件事——那是可空的引用类型，照旧合法。
func TestNullCannotBeBoxed(t *testing.T) {
	expectErr(t, `console.print(null as object)`, "null 装不进盒子：装箱装的是那一个值，空句柄没有值可装")
	expectErr(t, `let p:object = null as object
console.print(p)`, "null 装不进盒子：装箱装的是那一个值，空句柄没有值可装")
	expectOK(t, `let p:object = null
console.print(p)`)
}

// TestStructFieldCycleRefused struct 是值类型：甲的字段是乙、乙的字段又绕回甲，
// 那一块就永远装不完自己。直接自指由"字段不能指回自己这个类型"管，绕出去的两只以上先前没有
// 检查覆盖——过了检查，直到要整份拷贝时后端才报"字段绕成了环，这一版不发这种码"。
func TestStructFieldCycleRefused(t *testing.T) {
	expectErr(t, `struct A {
    b:B
}
struct B {
    a:A
}
console.print(1)`, "struct A 的字段绕成了环")
	// 三只绕一圈也算，一只环只报一次，报在环上名字最短的那一只合上它的字段上
	expectErr(t, `struct P {
    q:Q
}
struct Q {
    r:R
}
struct R {
    p:P
}
console.print(1)`, "struct P 的字段绕成了环")
	// 字段写在别名上，环要按换到底的那个名字认
	expectErr(t, `struct S1 {
    m:Mid
}
struct S2 {
    s:S1
}
type Mid = S2
console.print(1)`, "struct S1 的字段绕成了环")
	for _, src := range []string{
		// 值类型一层层套下去、不绕回来，照旧写得过
		`struct Inner {
    n:int
}
struct Outer {
    i:Inner
    s:string
}
console.print(1)`,
		// class 那一侧不受这一条约束：字段里放的是句柄，两只互相指着也照样装得完
		`class A9 {
    b:B9
}
class B9 {
    a:A9
}
console.print(1)`,
	} {
		expectOK(t, src)
	}
	// struct 字段放 class 本来就被另一条规则拒绝，这一条确认报的不是环
	expectErr(t, `class Leaf {
    v:int
}
struct Boxed {
    l:Leaf
}
console.print(1)`, "struct 的字段只能放值类型、string 或另一个 struct，这里是 Leaf")
}

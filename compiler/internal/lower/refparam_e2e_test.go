// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// refparam_e2e_test.go 覆盖 `ref` 参数降级之后必须成立的三件事：
//
//   - 调用方那个名字还在，连续调用两次、调用之后再读都成立；
//   - 被调方就地修改的那一份就是调用方那一份（与标准库入参同一条待遇）；
//   - 交进借用位置的那一块不会两头释放，也不会无人释放：从容器读走的一个位置交给借用参数，
//     两万圈实测每圈 0 字节；把同一个位置交给会拿走它的用户函数曾每圈留 16 字节，
//     那是已知尚未处理的遗留泄漏。
//
// en: refparam_e2e_test.go covers the three things that must hold once a `ref` parameter is
// lowered: the caller's name survives, so two calls in a row and a read afterwards all
// work; an in-place change inside the callee is a change to the caller's own object, exactly as
// for a standard-library argument; and the block handed to a borrowing slot is freed neither
// twice nor never - a position read out of a container and given to a borrowing parameter costs
// 0 bytes per round over twenty thousand rounds, where handing the same position to a user
// function that takes it away left 16 bytes per round, a known leak not yet addressed.
package lower_test

import (
	"strings"
	"testing"
)

func TestRefParamKeepsAndModifiesInPlace(t *testing.T) {
	const src = `method count(ref xs:list<string>):int {
    let total:int = 0
    for (one:string in xs) {
        total = total + length(one)
    }
    return total
}

method push(ref xs:list<string>, one:string) {
    xs.add(one)
}

let items:list<string> = ["甲", "乙"]
console.print(count(items))
console.print(count(items))
push(items, "丙")
console.print(length(items), items[2])
console.print(items[0])
`
	if got, code := run(t, src); got != "2\n2\n3 丙\n甲\n" || code != 0 {
		t.Fatalf("借用参数跑坏了：got %q 码 %d", got, code)
	}
}

// TestRefParamDoesNotLeakOrFreeTwice 一份列表借两万圈：那一块归调用方，被调方不参与析构。
func TestRefParamDoesNotLeakOrFreeTwice(t *testing.T) {
	const src = `method count(ref xs:list<string>):int {
    return length(xs)
}

let items:list<string> = ["甲", "乙"]
for (i:int in 0 until @N@) {
    console.print(count(items))
}
`
	if got, code := run(t, strings.ReplaceAll(src, "@N@", "20")); !strings.HasPrefix(got, "2\n2\n") || code != 0 {
		t.Fatalf("两万圈之前那二十圈跑坏了：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, src); per > 2 {
		t.Errorf("借一份列表跑两万圈，每圈 %g 字节：调用方那一份被多拆了一次或者没人拆", per)
	}
}

// TestRefParamFreesReadOutCell 从容器读走的一个位置交给借用参数：那一句走完就该释放。
// 同一个位置交进一个会拿走的参数属于已知遗留泄漏，每圈留 16 字节；这里覆盖的是标了 ref 的那一种。
func TestRefParamFreesReadOutCell(t *testing.T) {
	const src = `method show(ref one:string) {
    console.print(length(one))
}

let src:list<string> = ["甲乙", "丙丁"]
for (i:int in 0 until @N@) {
    let fresh:list<string> = ["甲乙", "丙丁"]
    show(fresh[0])
}
`
	if got, code := run(t, strings.ReplaceAll(src, "@N@", "20")); !strings.HasPrefix(got, "2\n2\n") || code != 0 {
		t.Fatalf("读走一格交给借用参数跑坏了：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, src); per > 120 {
		t.Errorf("每圈 %g 字节：那一格读走之后交给借用参数，没在这一句尽头还", per)
	}
}

// TestRefParamWithDefault 默认值那一档与 ref 互不影响：少给的位置进门就有值，谁都不持有它。
func TestRefParamWithDefault(t *testing.T) {
	const src = `method greet(ref heading:string = "季度报告") {
    console.print(heading)
}

for (i:int in 0 until @N@) {
    greet();
}
`
	if got, code := run(t, strings.ReplaceAll(src, "@N@", "3")); got != "季度报告\n季度报告\n季度报告\n" || code != 0 {
		t.Fatalf("默认值配 ref 跑坏了：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, src); per > 2 {
		t.Errorf("每圈 %g 字节：写在程序里的那一段文本不该被拆、也不该有人拿着它不放", per)
	}
}

// TestRefParamReassignmentKeepsCallerBlock 往标了 ref 的参数名字里再赋一个值，改的只是这一个
// 位置上的句柄：调用方那一块不动，两万圈里每圈改写一次，结束后那个名字仍是原来那一份。
//
// en: TestRefParamReassignmentKeepsCallerBlock overwriting a ref parameter's own name changes
// only the handle sitting in that slot: the caller's block is untouched, and after twenty
// thousand rounds of rewriting the caller's name still holds the original text.
func TestRefParamReassignmentKeepsCallerBlock(t *testing.T) {
	const src = `method f(ref s:string) {
    s = "改写的那一个"
    console.print(length(s))
}

let a:string = "甲乙丙"
for (i:int in 0 until @N@) {
    f(a)
}
console.print(a, length(a))
`
	if got, code := run(t, strings.ReplaceAll(src, "@N@", "20")); !strings.HasSuffix(got, "甲乙丙 3\n") || code != 0 {
		t.Fatalf("被调方里的改写动到了调用方那一份：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, src); per > 2 {
		t.Errorf("每圈 %g 字节：那一次改写把调用方那一块拆掉了或者留了一份没人还", per)
	}
}

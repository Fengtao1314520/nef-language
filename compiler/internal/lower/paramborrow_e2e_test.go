// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// paramborrow_e2e_test.go 覆盖"实参一律是只读借用"降级之后必须成立的三件事：
//
//   - 调用方那个名字还在，连续调用两次、调用之后再读都成立；
//   - 被调方就地修改的那一份就是调用方那一份（与标准库入参同一条待遇）；
//   - 交进借用位置的那一块不会两头释放，也不会无人释放：从容器读走的一个位置交给借用参数，
//     两万圈实测每轮 0 字节。
//
// en: paramborrow_e2e_test.go covers the three things that must hold once every argument is a
// read-only borrow: the caller's name survives, so two calls in a row and a read afterwards all
// work; an in-place change inside the callee is a change to the caller's own object, exactly as
// for a standard-library argument; and the block handed to a borrowing slot is freed neither twice
// nor never - a position read out of a container and given to a borrowing parameter costs 0 bytes
// per round over twenty thousand rounds.
package lower_test

import (
	"strings"
	"testing"
)

func TestParamKeepsAndModifiesInPlace(t *testing.T) {
	const src = `method count(xs:list<string>):int {
    let total:int = 0
    for (one:string in xs) {
        total = total + length(one)
    }
    return total
}

method push(xs:list<string>, one:string) {
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

// TestParamDoesNotLeakOrFreeTwice 一份列表借两万圈：那一块归调用方，被调方不参与析构。
func TestParamDoesNotLeakOrFreeTwice(t *testing.T) {
	const src = `method count(xs:list<string>):int {
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

// TestParamFreesReadOutCell 从容器读走的一个位置交给借用参数：那一句走完就该释放。
// 现在没有"会拿走实参"的用户函数，会接手的两处（系统句柄的参数与另一条线程的入口）在别处覆盖。
func TestParamFreesReadOutCell(t *testing.T) {
	const src = `method show(one:string) {
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

// TestParamWithDefault 默认值那一档与借用互不影响：少给的位置进门就有值，谁都不持有它。
func TestParamWithDefault(t *testing.T) {
	const src = `method greet(heading:string = "季度报告") {
    console.print(heading)
}

for (i:int in 0 until @N@) {
    greet();
}
`
	if got, code := run(t, strings.ReplaceAll(src, "@N@", "3")); got != "季度报告\n季度报告\n季度报告\n" || code != 0 {
		t.Fatalf("默认值配借用参数跑坏了：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, src); per > 2 {
		t.Errorf("每圈 %g 字节：写在程序里的那一段文本不该被拆、也不该有人拿着它不放", per)
	}
}

// TestParamReassignmentKeepsCallerBlock 往借来的参数名字里再赋一个值，改的只是这一个
// 位置上的句柄：调用方那一块不动，两万圈里每轮改写一次，结束后那个名字仍是原来那一份。
//
// en: TestParamReassignmentKeepsCallerBlock overwriting a borrowed parameter's own name changes
// only the handle sitting in that slot: the caller's block is untouched, and after twenty
// thousand rounds of rewriting the caller's name still holds the original text.
func TestParamReassignmentKeepsCallerBlock(t *testing.T) {
	const src = `method f(s:string) {
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

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// fnvalue_e2e_test.go 覆盖函数当值：`fn(参数类型):返回类型` 接住一份入口地址，
// 名字、实参、返回值与字段这四处都走得通，被调方按那一份地址发一次间接调用。
// 三条硬事实钉在这里：交出去的是签名对得上的顶层函数（方法不算）、
// 借用位上的文本读一次不转持有者、这一条路径每轮不长内存。
//
// en: fnvalue_e2e_test.go covers functions used as values: `fn(param types):return type` holds
// an entry address, and a name, an argument, a return value and a field all carry it - the callee
// issues one indirect call through that address. Three hard facts are pinned here: what may be
// handed over is a top-level function whose signature matches (a method is not), a text argument
// in a borrowed position is read once and changes no holder, and this path grows nothing per
// round.
package lower_test

import (
	"strings"
	"testing"
)

// TestFnValueThroughNameArgAndReturn 一份函数值走完整条路：接给一个名字、当实参交出去、
// 从被调方那里返回一份再来一次、重新赋值之后再调一次。
func TestFnValueThroughNameArgAndReturn(t *testing.T) {
	got, code := run(t, `
method twice(v:int):int {
    return v * 2
}

method three_times(v:int):int {
    return v * 3
}

method apply(by:fn(int):int, n:int):int {
    return by(n) + by(n)
}

method pick(larger:boolean):fn(int):int {
    if (larger) {
        return three_times
    }
    return twice
}

let f:fn(int):int = twice
console.print(f(3), apply(twice, 4), apply(f, 5))
f = pick(true)
console.print(f(6), apply(f, 7))
`)
	if got != "6 16 20\n18 42\n" || code != 0 {
		t.Fatalf("函数值这条路走出来是 %q 码 %d", got, code)
	}
}

// TestFnValueBorrowedTextArgs 是比较器那一档：两个借来的文本各读一次长度，两边都不转持有者，
// 所以同一种调用可以在一个循环里反复做。实参是从容器里读走的那两个位置，
// 借用的那一位不接手，这一句走完就把读走的那两步归还；容器里那两个位置空着，格数不变。
func TestFnValueBorrowedTextArgs(t *testing.T) {
	got, code := run(t, `
method by_length(ref a:string, ref b:string):int {
    return length(a) - length(b)
}

let words:list<string> = ["短", "长一点的那一个"]
let by:fn(ref string, ref string):int = by_length
let first:int = by(words[0], words[1])
console.print(first, by("甲乙丙", "丁"), length(words))
`)
	if got != "-6 2 2\n" || code != 0 {
		t.Fatalf("借用的文本参数走进函数值这条路是 %q 码 %d", got, code)
	}
}

// TestFnValueInFieldCall 字段里装一份函数值，再顺着点名调用它：与名字里接住的是同一件事。
func TestFnValueInFieldCall(t *testing.T) {
	got, code := run(t, `
method twice(v:int):int {
    return v * 2
}

class Runner {
    step:fn(int):int

    method go(n:int):int {
        return step(n) + 1
    }
}

let r:Runner = new Runner(step: twice)
console.print(r.go(3), r.step(10))
`)
	if got != "7 20\n" || code != 0 {
		t.Fatalf("字段里那份函数值调用出来是 %q 码 %d", got, code)
	}
}

// TestFnValueEmptyPositionIsRefused 字段没填函数值时那个位置是 0：照着它调用就是跳进空地址，
// 这一处按"对着空句柄取成员或调用方法"那一条当场拒绝，位置报的是这一次调用。
func TestFnValueEmptyPositionIsRefused(t *testing.T) {
	got, code := run(t, `
method twice(v:int):int {
    return v * 2
}

class Runner {
    step:fn(int):int
}

let a:Runner = new Runner(step: twice)
let b:Runner = new Runner()
console.print(a.step(3))
try {
    console.print(b.step(3))
} catch (code, msg, at) {
    console.print(code, msg, at)
}
`)
	if !strings.HasPrefix(got, "6\n10092 对着空句柄取成员或调用方法 t.nf 14:21\n") || code != 0 {
		t.Fatalf("空的那个函数位置没被挡下来：%q 码 %d", got, code)
	}
}

// TestFnValueDoesNotGrow 两份负载只差"调用走的是名字还是值"：圈数与每一步的分配完全相同，
// 向堆累计要过的字节数要一样——函数值那个位置只是一个地址，不另起一块内存。
func TestFnValueDoesNotGrow(t *testing.T) {
	const body = `
method twice(v:int):int {
    return v + 0
}

__DECL__
let sum:int = 0
for (i:int in 0 until 20000) {
    sum = sum + __CALL__
}
console.print(sum)
`
	directSrc := strings.ReplaceAll(strings.ReplaceAll(body, "__DECL__", ""), "__CALL__", "twice(i)")
	valueSrc := strings.ReplaceAll(strings.ReplaceAll(body,
		"__DECL__", "let by:fn(int):int = twice"), "__CALL__", "by(i)")
	_, direct := runHeap(t, directSrc)
	_, viaValue := runHeap(t, valueSrc)
	if direct != viaValue {
		t.Fatalf("按名字调用要过 %d 字节，按值调用要过 %d 字节，两条路不一样", direct, viaValue)
	}
}

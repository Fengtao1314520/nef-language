// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 这一族规则管：自己写的函数与方法返回的那一块，调用方能不能释放。
//
// 可释放的条件写在 lower.go 的 handsBackFresh 里：该函数的每一条 return 返回的，要么当场新造，
// 要么从自己那个位置作废出来（名字交出、容器一个位置被读走、class 字段按"读字段算交出"抹空）。
// 这五段覆盖条件的两头：证明得到的每圈留 0，证不出来的照旧保留且不出错。
// 每一段都把实际运行放在量堆之前：heapSlope 会设 NEF_HEAP，之后的 run() 输出会多一行 `heap …`。

// TestReturnOfFreshValueIsFreedByCaller 返回值是当场新造的那一块：没人接的语句到句尽头就析构。
// 修复前两万圈：返回文本 640,136 字节（每圈 32）、返回 list 1,760,144（每圈 88）。
func TestReturnOfFreshValueIsFreedByCaller(t *testing.T) {
	const text = `method make():string {
    return "甲" + "乙"
}
for (i:int in 0 until @N@) {
    make();
}
console.print(length(make()))
`
	const list = `method mk():list<int> {
    return [1, 2]
}
for (i:int in 0 until @N@) {
    mk();
}
console.print(length(mk()))
`
	if got, code := run(t, strings.ReplaceAll(text, "@N@", "20")); got != "2\n" || code != 0 {
		t.Fatalf("交回文本那一种跑坏了：got %q 码 %d", got, code)
	}
	if got, code := run(t, strings.ReplaceAll(list, "@N@", "20")); got != "2\n" || code != 0 {
		t.Fatalf("交回 list 那一种跑坏了：got %q 码 %d", got, code)
	}
	if per := heapSlope(t, text); per > 2 {
		t.Fatalf("被调方新造的文本每圈还留 %g 字节", per)
	}
	if per := heapSlope(t, list); per > 2 {
		t.Fatalf("被调方新造的 list 每圈还留 %g 字节", per)
	}
}

// TestReturnOfHandedOverNameIsFreedByCaller 返回的是自己那个位置作废出来的那一份：参数、局部名字、
// class 的字段、容器里读走的那个位置——四种来路各一段，接住的那一侧读得到、也能正常释放。
func TestReturnOfHandedOverNameIsFreedByCaller(t *testing.T) {
	got, code := run(t, `class Holder {
    peer:list<int>

    method take():list<int> {
        return this.peer
    }
}
method fromParam(x:string):string {
    return x
}
method fromLocal():list<int> {
    let a:list<int> = [7, 8]
    return a
}
method fromCell(xs:list<string>):string {
    return xs[0]
}
let one:Holder = new Holder(peer: [1, 2])
let kept:list<int> = one.take()
console.print(length(kept), kept[0])
console.print(fromParam("参"))
let second:list<int> = fromLocal()
console.print(length(second), second[1])
let box:list<string> = ["格"]
console.print(fromCell(box))
`)
	want := "2 1\n参\n2 8\n格\n"
	if got != want || code != 0 {
		t.Fatalf("四种来路交回的值读不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestReturnOfStaticLiteralIsHarmless 返回值是静态写死在程序里的文本：调用方接住之后照常读取，
// 覆盖与圈尾那两次释放落在静态占位头上（尺寸位置是 0 → 空操作），无法析构它，也不出错。
func TestReturnOfStaticLiteralIsHarmless(t *testing.T) {
	const tmpl = `method keep():string {
    return "共"
}
for (i:int in 0 until @N@) {
    let s:string = keep()
    console.print(length(s))
}
`
	if got, code := run(t, strings.ReplaceAll(tmpl, "@N@", "20")); got != strings.Repeat("1\n", 20) || code != 0 {
		t.Fatalf("交回静态字面量那一种读错了：码 %d", code)
	}
	if per := heapSlope(t, tmpl); per > 2 {
		t.Fatalf("交回静态字面量那一种每圈留 %g 字节（那一格本不该有主人）", per)
	}
}

// TestStructFieldReturnStaysUnproven 从 struct 字段读出的那一种不满足证明条件：struct 是值类型，
// 那一次读取不动源头（同一个字段读两次都还在），那块文本的持有者仍是原来的那个块。
// 这一段验证"没有被误登记"：连续调用二十次每次都读到同一份，调用方没有提前把它析构掉。
// （这一段每圈仍留 16 字节——那是值类型实参在调用点拷出来的那一块，属于另一类，见每圈增长表。）
func TestStructFieldReturnStaysUnproven(t *testing.T) {
	const tmpl = `struct P {
    t:string
}
method peek(p:P):string {
    return p.t
}
let q:P = P { t: "甲" + "乙" }
for (i:int in 0 until 20) {
    console.print(length(peek(q)))
}
console.print(length(q.t))
`
	if got, code := run(t, tmpl); got != strings.Repeat("2\n", 21) || code != 0 {
		t.Fatalf("struct 字段那一种该每圈读到 2、最后那一次也读到 2：码 %d", code)
	}
}

// TestStructArgumentCopyIsFreedByCaller：值类型实参在调用点整份拷出来的那一块，
// 登记为调用方一侧的持有者。两件事一起验证——每圈不多留（每圈增长表第 35 行核同一个数），
// 而且被调方改的是它那一份副本，调用方的 `q` 保持不变（传参对值类型就是给它一份副本）。
func TestStructArgumentCopyIsFreedByCaller(t *testing.T) {
	const tmpl = `struct P {
    n:int
}
method bump(p:P):int {
    p.n = p.n + 1
    return p.n
}
let q:P = P { n: 1 }
for (i:int in 0 until @N@) {
    console.print(bump(q), q.n)
}
`
	if got, code := run(t, strings.ReplaceAll(tmpl, "@N@", "20")); got != strings.Repeat("2 1\n", 20) || code != 0 {
		t.Fatalf("副本改了不该动原那一个：码 %d", code)
	}
	if per := heapSlope(t, tmpl); per > 2 {
		t.Fatalf("值类型实参那一块每圈还留 %g 字节", per)
	}
}

// TestRecursiveReturnStaysUnproven 环一律按"证不出来"处理：函数自己调用自己的那一种，本版不收这个位置。
func TestRecursiveReturnStaysUnproven(t *testing.T) {
	const tmpl = `method down(n:int):list<int> {
    if (n > 0) {
        return down(n - 1)
    }
    return [1, 2, 3]
}
for (i:int in 0 until @N@) {
    console.print(length(down(4)))
}
`
	if got, code := run(t, strings.ReplaceAll(tmpl, "@N@", "20")); got != strings.Repeat("3\n", 20) || code != 0 {
		t.Fatalf("递归那一种读错了：码 %d", code)
	}
	if per := heapSlope(t, tmpl); per < 28 {
		t.Fatalf("递归交回的那一种今天按证不出来处理（每圈 %g 字节）", per)
	}
}

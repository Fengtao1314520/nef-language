// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// cellfree_e2e_test.go 覆盖"容器位置覆盖"实现后保证"不析构错持有者"的几处场景。
//
// 覆盖容器的一个位置会析构换下来的旧值，于是三件事必须成立：
//   - 读一个位置不拿走值，那个位置之后还是原来的那一份，覆盖它才谈得上"换下来"；
//   - 正在遍历这个 list 时覆盖它，会命中 10033 号（借用的就是旧的这一块），不会拿着已释放的内存走完一圈；
//   - 覆盖 class 的字段照旧不析构——读字段仍然算交出，那一块可能正被另一个持有者拿着。
package lower_test

import (
	"strings"
	"testing"
)

// TestTakeThenOverwrite 先把那一个位置里的一份复制出来（读一个位置不拿走，要留在手上就自己拿一份），
// 再覆盖它：覆盖析构的是换下来的旧值，手里那一份不受影响；之后 list 散掉时也不许再析构第二次。
func TestTakeThenOverwrite(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲", "乙"]
let a:string = xs[0] + ""
xs[0] = "丙丙".slice(0, 2)
console.print(a, xs[0], length(xs))
`)
	if got != "甲 丙丙 2\n" || code != 0 {
		t.Fatalf("覆盖一个位置不动手里那一份复制：得到 %q 码 %d", got, code)
	}
}

// TestOverwriteWhileIterating 遍历交出的是只读借用，借的就是那个位置里旧的这一块；
// 覆盖它必须让这一圈命中 10033 号，而不是拿着已析构的对象继续走完。
func TestOverwriteWhileIterating(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲", "乙"]
for (s:string in xs) {
    xs[0] = "丙".slice(0, 1)
}
console.print("走完")
`)
	if code == 0 {
		t.Fatalf("遍历list途中覆盖那个位置该被拦下，却跑完了：%q", got)
	}
	if code != 1 || got != "出错了（10033）：遍历途中改了容器（在 t.nf 4:7）\n" {
		t.Fatalf("遍历中覆盖应当是 10033 号那一句，得到 %q 码 %d", got, code)
	}
}

// TestDictValueCycleStress 把"添、覆盖、读、删"混在一圈里跑八千圈：数要对、不能崩溃，
// 也不能把还活着的东西析构掉（析构错了对象，这一圈就会读到 0 或抛出空句柄）。
func TestDictValueCycleStress(t *testing.T) {
	got, code := run(t, `
class Row {
    n:int
    kids:list<int>
}
let d:dict<string, Row> = {}
let first:string = "头".slice(0, 1)
d.add(first, new Row(n: 1, kids: [1, 2]))
let total:int = 0
for (i:int in 0 until 8000) {
    let k:string = "头".slice(0, 1)
    d.add(k, new Row(n: i, kids: [i]))       // 命中：没收下的那个键当场拆，旧值也拆
    total = total + d["头"].n + length(d["头"].kids)   // 读两遍同一个位置，第二遍还在
    let k2:string = "头".slice(0, 1)
    d.add(k2, new Row(n: 1, kids: [1, 2, 3]))
    total = total + length(d["头"].kids)
    let k3:string = "尾".slice(0, 1)
    d.add(k3, new Row(n: 3, kids: []))
    d.remove("尾")
}
console.print(total, length(d), d["头"].n)
`)
	// 手算这一圈的数：第一次 add 之后那个位置是 Row(n=i, kids 一个位置) → 加 i+1；
	// 第二次 add 换成 kids 三个位置 → 加 3。每轮 i+4，8000 圈 = 8000×7999/2 + 32000 = **32,028,000**。
	// 表里最后剩 1 条（"尾"添完就删），"头"那个位置是 k2 那一次添的 n=1。
	if got != "32028000 1 1\n" || code != 0 {
		t.Fatalf("混合八千圈跑不对：得到 %q 码 %d", got, code)
	}
}

// TestTakeIntoBorrow 从容器里读出来的那一份交给只读借用的标准库组函数：调用结束谁都不析构它，
// 那个位置之后还留着值。核对"读两遍值一样、位置不为空句柄"。
func TestTakeIntoBorrow(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲乙丙".slice(0, 3)]
console.print(length(xs[0]), xs[0] == null)
`)
	if got != "3 false\n" || code != 0 {
		t.Fatalf("读出来交给标准库那一路不对：得到 %q 码 %d", got, code)
	}
}

// TestTakeBorrowTwice 同一个位置读两次：读一个位置不拿走，第二趟读到的还是那一份。
// 这一条若第二趟读到空句柄，就是把借来的那一份当成要归还的中间值析构掉了。
func TestTakeBorrowTwice(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲"]
console.print(xs[0])
console.print(xs[0] == null)
let d:dict<string, int> = {"k": 1}
console.print(d["k"] + 1, d.contains("k"))
`)
	want := "甲\nfalse\n2 true\n"
	if got != want || code != 0 {
		t.Fatalf("读两遍不空：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTakeToNameThenBorrow 三条路混在一起：摘出来交给有名字的变量（由声明那个位置负责释放）、
// 读一个位置交给只读借用的用户函数、
// 写进字段那一个位置（交接手，源名字交出之后就作废）。
// 三千圈跑下来数对、不崩溃，说明这三套规则互不干扰。
func TestTakeToNameThenBorrow(t *testing.T) {
	got, code := run(t, `
class Node {
    n:int
}
class Hold {
    item:Node
}
method peek(nd:Node):int {
    return nd.n
}
let total:int = 0
for (i:int in 0 until 3000) {
    let ns:list<Node> = [new Node(n: i), new Node(n: i + 1)]
    let a:Node = ns.remove_at(0)
    let h:Hold = new Hold(item: a)
    total = total + peek(ns[0]) + h.item.n
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("混合三千圈退出码 %d（要 0）：\n%s", code, got)
	}
	// 每轮 (i+1) + i = 2i+1，三千圈 = 2×4,498,500 + 3,000 = 9,000,000
	if got != "9000000\n" {
		t.Fatalf("混合三千圈的总数不对：得到 %q", got)
	}
}

// TestHandBackRegistersReceiver 覆盖 return 那一侧的释放规则：被调方把从容器里摘出来的那一份
// return 出来，递进去的那一块出了被调方只剩接住它的那个位置持有 ⇒ 那个位置登记成持有者、块尾释放。
// 三千圈混两条路（读走的与当场新造的），释放早了就会读到已析构的块、总数就不对。
//
// 旧规则里"被调方把参数原样 return 出来"那一条已经走不通了：实参是只读借用，
// `return 那一个参数` 在编译期就拒，所以这里换用容器那一个位置摘出来的那一份。
func TestHandBackRegistersReceiver(t *testing.T) {
	got, code := run(t, `
class Note {
    n:int
}
method back(cs:list<Note>):Note {
    return cs.remove_at(0)
}
method peek(cs:list<Note>):int {
    return length(cs)
}
let total:int = 0
for (i:int in 0 until 3000) {
    let cs:list<Note> = [new Note(n: i), new Note(n: 7)]
    let got:Note = back(cs)
    total = total + got.n + peek(cs)
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("转手回来三千圈退出码 %d（要 0）：\n%s", code, got)
	}
	// 每轮 i + 1（摘走一个之后容器里还剩一个）：4,498,500 + 3,000
	if got != "4501500\n" {
		t.Fatalf("转手回来三千圈的总数不对：得到 %q", got)
	}
}

// TestHandBackTwoPathsNotRegistered 覆盖保守的一面：函数有两条带值的 return，只有一条把
// 容器里那一块交出去，另一条当场新造 ⇒ 两条路递出来的块不是同一处来历，接住的那个位置
// 只能按"return 出去的那一份归调用方"这一条通用规则释放，一笔画多了就是错误析构。
func TestHandBackTwoPathsNotRegistered(t *testing.T) {
	got, code := run(t, `
class Note {
    n:int
}
method pick(cs:list<Note>, fresh:boolean):Note {
    if (fresh) {
        return new Note(n: 7)
    }
    return cs.remove_at(0)
}
let total:int = 0
for (i:int in 0 until 300) {
    let cs:list<Note> = [new Note(n: i)]
    let got:Note = pick(cs, i == 0)
    total = total + got.n
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("两条 return 路三百圈退出码 %d（要 0）：\n%s", code, got)
	}
	// 头一圈走的是 fresh 那一支拿到 7，其余 299 圈拿到 i
	if got != "44857\n" {
		t.Fatalf("两条 return 路的总数不对：得到 %q", got)
	}
}

// TestHandBackPremiseHeldByFrontEnd 验证这一族的前提由前端守住：实参是只读借用，
// 被调方既不能把参数 `return` 给调用方，也不能把它交进容器或字段那一个位置——
// "带回来了"与"另有持有者"两种情况不会同时出现。
func TestHandBackPremiseHeldByFrontEnd(t *testing.T) {
	if err := compileErr(`
class Note {
    n:int
}
method stash(n:Note):Note {
    return n
}
console.print(1)
`); err == nil || !strings.Contains(err.Error(), "参数 n 是只读借用") {
		t.Fatalf("把只读借用的参数 return 出去该被拦：err=%v", err)
	}
	if err := compileErr(`
class Note {
    n:int
}
method stash(cs:list<Note>, n:Note) {
    cs.add(n)
}
console.print(1)
`); err == nil || !strings.Contains(err.Error(), "参数 n 是只读借用") {
		t.Fatalf("把只读借用的参数交进容器那一个位置该被拦：err=%v", err)
	}
}

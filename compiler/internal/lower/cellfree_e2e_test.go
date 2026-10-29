// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// cellfree_e2e_test.go 覆盖"容器位置覆盖"实现后保证"不析构错持有者"的几处场景。
//
// 覆盖容器的一个位置会析构换下来的旧值，于是三件事必须成立：
//   - 那个位置被**读走**过（旧值是空句柄）就不析构任何内容，也不许析构第二次；
//   - 正在遍历这个 list 时覆盖它，会命中 10033 号（借用的就是旧的这一块），不会拿着已释放的内存走完一圈；
//   - 覆盖 class 的字段照旧不析构——读字段不算交出，那一块可能正被另一个持有者拿着。
package lower_test

import (
	"strings"
	"testing"
)

// TestTakeThenOverwrite 先把那个位置读走（之后那个位置是空句柄），再覆盖它：旧值没有可析构的内容，
// 手里那一份照旧能用；之后 list 散掉时也不许再析构第二次。
func TestTakeThenOverwrite(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲", "乙"]
let a:string = xs[0]
xs[0] = "丙丙".slice(0, 2)
console.print(a, xs[0], length(xs))
`)
	if got != "甲 丙丙 2\n" || code != 0 {
		t.Fatalf("读走之后覆盖同一个槽不对：得到 %q 码 %d", got, code)
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
	if code != 1 || got != "出错了（10033）：遍历途中改了容器\n" {
		t.Fatalf("遍历中覆盖应当是 10033 号那一句，得到 %q 码 %d", got, code)
	}
}

// TestFieldOverwriteKeepsOld 字段一侧维持现状：读出来不算交出，覆盖时不析构旧的，
// 所以旧的那一块还能安全地使用（这里"漏一块"是语言明文规定的行为，不是错误）。
func TestFieldOverwriteKeepsOld(t *testing.T) {
	got, code := run(t, `
class Node {
    v:int
}
class Box {
    inner:Node
}
let b:Box = new Box(inner: new Node(v: 1))
let y:Node = b.inner
b.inner = new Node(v: 2)
y.v = 7
console.print(y.v, b.inner.v)
`)
	if got != "7 2\n" || code != 0 {
		t.Fatalf("覆盖字段不许拆别人手里那一块：得到 %q 码 %d", got, code)
	}
}

// TestDictValueCycleStress 把"添、覆盖、读走、删"混在一圈里跑八千圈：数要对、不能崩溃，
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
    let back:Row = d["头"]                    // 读走：那个位置之后是空句柄
    total = total + back.n + length(back.kids)
    let back2:Row = back                      // 值类型另起一块？不是——class 是句柄，这里是交出
    let k2:string = "头".slice(0, 1)
    d.add(k2, new Row(n: 1, kids: [1, 2, 3]))
    total = total + length(back2.kids)
    let k3:string = "尾".slice(0, 1)
    d.add(k3, new Row(n: 3, kids: []))
    d.remove("尾")
}
console.print(total, length(d), d["头"].n)
`)
	// 手算这一圈的数：`back` 是第一次 add 那一行（n=i、kids 一个位置）→ 加 i+1；
	// `let back2:Row = back` 是句柄交出（同一块，不是新行）→ 再加它的 kids 一个位置 = 1；
	// 每圈 i+2，8000 圈 = 8000×7999/2 + 16000 = **32,012,000**。
	// 表里最后剩 1 条（"尾"添完就删），"头"那个位置是 k2 那一次添的 n=1。
	if got != "32012000 1 1\n" || code != 0 {
		t.Fatalf("混合八千圈跑不对：得到 %q 码 %d", got, code)
	}
}

// TestTakeIntoBorrow 覆盖这一条规则：从容器里**读走**的那个句柄交给只读借用的标准库组函数
// （这次调用把参数交出就当场释放）。安全依据有两条：读走那一步已经把源位置抹成 `null`
// （没有第二个出处），而标准库不收留值。这里核对的是"释放之后那个位置还是 `null`、值也打印正确"。
func TestTakeIntoBorrow(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲乙丙".slice(0, 3)]
console.print(length(xs[0]), xs[0] == null)
`)
	if got != "3 true\n" || code != 0 {
		t.Fatalf("读走之后交给标准库那一路不对：得到 %q 码 %d", got, code)
	}
}

// TestTakeBorrowTwice 同一个槽位读两次：第一趟读走（并当场释放），第二趟看到的就是空句柄。
// 两个临时槽位一起登记时第二个是 `null`——释放它必须是空操作（第二次 @free 识别空句柄、不做处理）。
// 这一条若把值打印两遍，或第二趟访问了悬空句柄，就是双重释放／写入已释放内存的形态。
func TestTakeBorrowTwice(t *testing.T) {
	got, code := run(t, `
let xs:list<string> = ["甲"]
console.print(xs[0])
console.print(xs[0] == null)
let d:dict<string, int> = {"k": 1}
console.print(d["k"] + 1, d.contains("k"))
`)
	want := "甲\ntrue\n2 true\n"
	if got != want || code != 0 {
		t.Fatalf("两次读走没按'读走就是交出'走：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTakeToNameThenBorrow 三条路混在一起：读走之后交给有名字的变量（由声明那个位置负责释放）、交给
// 不带走的用户函数（这一版不登记，宁可保留少量泄漏）、交给会带走的用户函数（被调方拿走，这里谁都不许释放）。
// 三千圈跑下来数对、不崩溃，说明这三套规则互不干扰——该规则只在"交给只读借用的标准库"一侧生效，
// 其余两条一律不碰。
func TestTakeToNameThenBorrow(t *testing.T) {
	got, code := run(t, `
class Node {
    n:int
}
method hold(nd:Node):Node {
    return nd
}
method peek(nd:Node):int {
    return nd.n
}
let total:int = 0
for (i:int in 0 until 3000) {
    let ns:list<Node> = [new Node(n: i), new Node(n: i + 1), new Node(n: i + 2)]
    let a:Node = ns[0]
    total = total + peek(ns[1]) + a.n + hold(ns[2]).n
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("混合三千圈退出码 %d（要 0）：\n%s", code, got)
	}
	if got != "13504500\n" {
		t.Fatalf("混合三千圈的总数不对：得到 %q", got)
	}
}

// TestHandBackRegistersReceiver 覆盖 return 转手位置的释放规则：被调方把参数原样 return 出来，
// 递进去的那一块出了被调方只剩接住它的那个位置持有 ⇒ 那个位置登记成持有者、块尾释放。
// 三千圈混两条路（读走的与当场新造的），释放早了就会读到已析构的块、总数就不对。
func TestHandBackRegistersReceiver(t *testing.T) {
	got, code := run(t, `
class Note {
    n:int
}
method back(n:Note):Note {
    return n
}
let total:int = 0
for (i:int in 0 until 3000) {
    let cs:list<Note> = [new Note(n: i)]
    let got:Note = back(cs[0])
    total = total + got.n
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("转手回来三千圈退出码 %d（要 0）：\n%s", code, got)
	}
	if got != "4498500\n" {
		t.Fatalf("转手回来三千圈的总数不对：得到 %q", got)
	}
}

// TestHandBackTwoPathsNotRegistered 覆盖保守的一面：函数有两条带值的 return，只有一条返回
// 这一个参数 ⇒ 无法确定接住的那个位置拿到的是哪一块 ⇒ 一笔都不许多释放（不释放是泄漏，错误析构是事故）。
func TestHandBackTwoPathsNotRegistered(t *testing.T) {
	got, code := run(t, `
class Note {
    n:int
}
method pick(n:Note, other:Note):Note {
    if (n.n > 0) {
        return n
    }
    return other
}
let total:int = 0
for (i:int in 0 until 300) {
    let cs:list<Note> = [new Note(n: i), new Note(n: 7)]
    let a:Note = cs[0]
    let b:Note = cs[1]
    let got:Note = pick(a, b)
    total = total + got.n
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("两条 return 路三千圈退出码 %d（要 0）：\n%s", code, got)
	}
	// 头一圈走的是 else 那一支拿到 7，其余 299 圈拿到 i
	if got != "44857\n" {
		t.Fatalf("两条 return 路的总数不对：得到 %q", got)
	}
}

// TestHandBackPremiseHeldByFrontEnd 验证这一族的前提由前端守住：参数交进容器、交给别的函数
// 那一步之后就作废了，再 `return` 它是编译错误——"交回来了"与"另有持有者"两种情况不会同时出现。
func TestHandBackPremiseHeldByFrontEnd(t *testing.T) {
	if err := compileErr(`
class Note {
    n:int
}
method stash(n:Note):Note {
    let bag:list<Note> = [n]
    return n
}
console.print(1)
`); err == nil || !strings.Contains(err.Error(), "已经交出去了") {
		t.Fatalf("参数交进容器之后再 return 它该被拦：err=%v", err)
	}
}

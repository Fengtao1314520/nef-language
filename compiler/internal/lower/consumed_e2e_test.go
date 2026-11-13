// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// consumed_e2e_test.go 覆盖交出账里"接住它的那一侧留没留下"这半边的规则。
//
// 此前"把值交出过"一律不登记，于是每一圈整块泄漏（实测 88 字节／圈）。
// 现行判据分两层：实参定为只读借用，自己写的函数与方法的参数不再从调用方带走那一份，
// 所以名字不失效、源位置照旧由调用方那一帧释放；会接手的位置只剩五处——接给一个名字、
// 写进 `class` 的字段、写进容器的一个位置、装箱、`return` 给接住它的那个位置，
// 这五处交出去之后源位置一笔都不许多释放。
// 两个持有者释放同一块，等于错误析构对方持有的内存，因此登记宁可保守。
//
// 两头都要覆盖：释放之后程序结果仍正确（泄漏那一侧由 heap_e2e_test.go 的每轮增长表度量）；
// 而**不许双重释放**那一侧只能靠实际运行验证——析构了别人持有的那块，症状是几十圈之后数值错误或者当场段错误。

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ir"
)

// dropCallee 只读一下长度，不带走。
const dropCallee = `
method use(xs:list<int>):int {
    return length(xs)
}
`

// destroyedSlots 返回这一个函数里每一笔 nef.destroy 释放的位置集合（静态槽写成 @N）。
func destroyedSlots(fn *ir.Func) map[string]bool {
	out := map[string]bool{}
	for _, in := range fn.Body {
		if c, ok := in.(*ir.Call); ok && c.Fn == "nef.destroy" {
			out[c.Args[0].String()] = true
		}
	}
	return out
}

// TestConsumedArgFreedByCaller 是最小用例：顶层把一个静态槽交给不带走的函数，这笔释放应出现在
// 程序尽头那份记录上（交出去过 ≠ 不归我释放）。释放的位置在 `nef.release_top`——顶层的名字住静态槽。
// 静态槽的编号：acc=@0，入口顶层那个 args=@1（规则把它算成入口顶层的一个 `let`）。
func TestConsumedArgFreedByCaller(t *testing.T) {
	src := dropCallee + `
let acc:list<int> = [1, 2, 3]
console.print(use(acc))
`
	got, code := run(t, src)
	if want := "3\n"; got != want || code != 0 {
		t.Fatalf("交进不带走的函数之后连数都不对了：\n got %q 码 %d\nwant %q", got, code, want)
	}
	slots := destroyedSlots(findFn(progOf(t, src), releaseName))
	if !slots["@0"] {
		t.Fatalf("acc（@0）该由程序尽头还掉，尽头那份记录是 %v", keys(slots))
	}
}

// TestAnonymousArgFreedAtCallSite 覆盖**没名字**的那一半：`use([1, 2, 3])`、
// `count("甲" + "乙")`、`hold(new Bag(n: i))`——式子当场新造的一块，除调用点这个位置寄存器外
// 没有第二处持有，被调方又不带走，于是调用这一层登记它、块尾释放（这一类此前整块泄漏）。
func TestAnonymousArgFreedAtCallSite(t *testing.T) {
	src := `
class Bag {
    n:int
}
method use(xs:list<int>):int {
    return length(xs)
}
method count(s:string):int {
    return length(s)
}
method hold(b:Bag) {
}
let total:int = 0
for (i:int in 0 until 2000) {
    total = total + use([i, i, i]) + count("甲" + "乙")
    hold(new Bag(n: i))
}
console.print(total)
`
	// 一圈 3 + 2 = 5
	out, used := runHeap(t, src)
	if strings.TrimSpace(out) != "10000" {
		t.Fatalf("没名字的新造一块跑两千圈数不对：%q", out)
	}
	if used > 1<<20 {
		t.Fatalf("交出去的三处都该有持有者，两千圈不该涨出最初那一块：%d 字节", used)
	}
	// 中间码那一侧：调用点上那个位置是本层唯一的一笔持有者（同一条路径可能执行三回——
	// 溢出退出清理代码、块尾、帧内清理记录，每回释放完都把那个位置抹成 0，所以数的是位置不是笔数）。
	top := findFn(progOf(t, `
method use(xs:list<int>):int {
    return length(xs)
}
let total:int = 0
total = total + use([1, 2])
console.print(total)
`), "nef.top.t.nf")
	if got := keys(destroyedSlots(top)); len(got) != 1 || got[0][0] != '%' {
		t.Fatalf("调用点那个位置该是本层唯一的持有者，实际还的是 %v", got)
	}
}

// TestAnonymousArgKeptFreedAtReceiver 覆盖转手规则的另一面：式子当场新造的那一块被容器接走，
// 再从容器里摘出来交给一个名字 ⇒ 新造它的那一处不许当场释放（释放过早），
// 而 `got` 那个位置登记成持有者、块尾释放。
//
// 两头都要固定：输出照旧正确（当场释放就会读到已析构的块），块尾那一笔也必须在。
// 旧规则里那条"被调方把参数原样 return 出来"已经走不通（实参是只读借用），
// 而系统句柄与 `thread.start` 那两处接手在别的用例里覆盖。
func TestAnonymousArgKeptFreedAtReceiver(t *testing.T) {
	src := `
let total:int = 0
for (i:int in 0 until 500) {
    let bag:list<list<int>> = []
    bag.add([i, i])
    let got:list<int> = bag.remove_at(0)
    total = total + length(got) + got[0]
}
console.print(total)
`
	// 一圈 2 + i：500×2 + (0+1+…+499) = 1000 + 124750
	got, code := run(t, src)
	if want := "125750\n"; got != want || code != 0 {
		t.Fatalf("被调方带走的写法被多还了一笔：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if n := destroysIn(findFn(progOf(t, src), "nef.top.t.nf")); n == 0 {
		t.Fatalf("转手回来那个位置该在块尾还掉，实际一笔没有")
	}
}

// keys 把 map 的键摊平成切片，让报错更短。
func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// destroysIn 数这一个函数里有几笔 nef.destroy。
func destroysIn(fn *ir.Func) int {
	n := 0
	for _, in := range fn.Body {
		if c, ok := in.(*ir.Call); ok && c.Fn == "nef.destroy" {
			n++
		}
	}
	return n
}

// TestKeptArgNotFreedByCaller 反向验证：值交出去之后源位置那一笔都不许多还——
// 写进容器的那个位置、写进 class 的字段、交进另一条线程的入口、装进盒子，四处都是接手的位置，
// 那块内存从此另有出处（或者无人认领，那是泄漏不是错误）。
// 每一段都写明了哪些位置**该**释放：它们自己也是持有者，一释放其位置里那一块随之析构，
// 所以交进去的那一号绝不能再释放第二次。
//
// 旧规则里"实参交进用户函数、被调方把它留住"那几条已经不成立：实参是只读借用，
// 交不出去；剩下会接手的两处调用是系统句柄的参数与 `thread.start` 的第二个入参，这里取后者。
func TestKeptArgNotFreedByCaller(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// denied 是这一段的源位置（不许出现在尽头那份记录上）
		denied string
		// extra 记的是这一段里**该**释放的那些编号，写出来以便静态槽编号漂移时能被发现
		extra []string
		want  string
	}{
		{"写进容器那一个位置", `
let acc:list<int> = [9]
let outer:list<list<int>> = []
outer.add(acc)
console.print(length(outer[0]))
`, "@0", []string{"@1"}, "1\n"},
		{"写进 class 字段那一个位置", `
class Box {
    held:list<int>
}
let box:Box = new Box(held: [])
let acc:list<int> = [3, 4, 5]
box.held = acc
console.print(length(box.held))
`, "@1", []string{"@0"}, "3\n"},
		{"交进另一条线程的入口", `
class Row {
    kids:list<int>
}
method work(row:Row):int {
    return length(row.kids)
}
let r:Row = new Row(kids: [1, 2])
let t:Thread<int> = thread.start(work, r)
console.print(t.wait())
`, "@0", nil, "2\n"},
		{"装进盒子", `
class Bag {
    n:int
}
let acc:Bag = new Bag(n: 1)
let o:object = acc as object
console.print((o as Bag).n)
`, "@0", nil, "1\n"},
	}
	for _, one := range cases {
		got, code := run(t, one.src)
		if strings.TrimSpace(got) != strings.TrimSpace(one.want) || code != 0 {
			t.Errorf("%s：跑出来 %q 码 %d，要 %q", one.name, got, code, one.want)
		}
		slots := destroyedSlots(findFn(progOf(t, one.src), releaseName))
		if slots[one.denied] {
			t.Errorf("%s：那一块已经交出去了，源位置 %s 一笔都不许还，实际那份记录是 %v", one.name, one.denied, keys(slots))
		}
		for _, s := range one.extra {
			if !slots[s] {
				t.Errorf("%s：位置 %s 本该在这一本记录里，实际是 %v", one.name, s, keys(slots))
			}
		}
	}
}

// TestConsumedMixedStress 是"跑几千圈不崩溃"的压力用例：三类位置混在一圈里——
// 交给只读借用的函数（本帧释放）、写进字段那一个位置（源名字交出之后作废）、
// 写进容器的一个位置再摘出来交给一个名字（由接住它的那个位置释放）、
// 就地修改它（修改不算带走，仍由本帧释放）。
// 数值正确、空闲链未被破坏，这条测试才算通过。
func TestConsumedMixedStress(t *testing.T) {
	src := `
class Box {
    held:list<int>
}
method use(xs:list<int>):int {
    return length(xs)
}
method grow(xs:list<int>) {
    xs.add(9)
}
method boom(xs:list<int>) {
    throw (141, "交进去的时候抛")
}
let total:int = 0
for (i:int in 0 until 3000) {
    let a:list<int> = [i]
    let b:list<int> = [i, i]
    let c:list<int> = [i, i, i]
    let e:list<int> = [i, i, i, i]
    let f:list<int> = [i, i, i, i, i]
    total = total + use(a)
    total = total + length(f)
    grow(f)
    let box:Box = new Box(held: [])
    box.held = b
    let bag:list<list<int>> = []
    bag.add(c)
    let got:list<int> = bag.remove_at(0)
    total = total + length(got) + length(box.held)
    try {
        boom(e)
    } catch (code, msg, at) {
        total = total + code
    }
}
console.print(total)
`
	// 一圈的账：use(a)=1、length(f)=5、length(got)=3、length(box.held)=2、code=141 ⇒ 一圈 152
	if got, code := run(t, src); got != "456000\n" || code != 0 {
		t.Fatalf("混着跑三千圈不对：\n got %q 码 %d\nwant 456000\n（一圈 152＝1+5+3+2+141）", got, code)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"fmt"
	"os"
	"strings"

	"neflang.org/nf/internal/ir"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// thread_e2e_test.go 覆盖第二条线程真的跑起来之后那几件事：值两头走得通、没接住的抛出只结束
// 那一条、`wait()` 读到错误结束时抛的那一句能被人接住、等过两次是 10092 号、名字走到作用域尽头
// 时自动等那一次不抛，以及段 4 攒下的那三条并发（同一只容器、同一块堆、同一行屏幕）。
//
// 这一份文件里的每一条都是真起线程的：段 2 到段 5 那一批测试钉的是"只有一条线程时不许变"，
// 这一段钉的是"有了第二条线程之后要变的那一些"。

// TestThreadHandbackInt 是最短的一条路：起一条线程算一个数，等它跑完把那个数取回来。
func TestThreadHandbackInt(t *testing.T) {
	got, code := run(t, `
method double(n:int):int {
    return n * 2
}
let t:Thread<int> = thread.start(double, 21)
console.print(t.wait())
`)
	if got != "42\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 \"42\\n\" 码 0", got, code)
	}
}

// TestThreadHandbackString 钉返回类型跟着句柄身上那一个尖括号参数走：返回的是一段文本，
// 不是整数。中间码与运行期那一条路上那个值都只是 8 个字节的一个数，类型只在检查那一层成立，
// 所以这一条钉的是"接住的那一个名字能用成文本"。
func TestThreadHandbackString(t *testing.T) {
	got, code := run(t, `
method shout(s:string):string {
    return s + "！"
}
let t:Thread<string> = thread.start(shout, "甲")
let answer:string = t.wait()
console.print(answer + answer)
`)
	if got != "甲！甲！\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 \"甲！甲！\\n\" 码 0", got, code)
	}
}

// TestThreadNested 钉一条线程里再起一条：被起的那个函数自己也是一条普通函数，它的帧挂在
// 那一条线程自己的栈上；两条线程各自等各自的那一个，各自归还自己那一份状态块。
func TestThreadNested(t *testing.T) {
	got, code := run(t, `
method inner(n:int):int {
    return n + 1
}
method outer(n:int):int {
    let t:Thread<int> = thread.start(inner, n)
    return t.wait()
}
let a:Thread<int> = thread.start(outer, 40)
console.print(a.wait())
`)
	if got != "41\n" || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 \"41\\n\" 码 0", got, code)
	}
}

// TestThreadClassHandedOver 钉交进去的是一个 `class` 句柄：那一个对象归那一条线程，
// 它在自己那一条线程的作用域尽头归还，主线程不再碰它。堆只有一份且加锁，所以两条线程
// 各自拿与放不会互相踩；这里数的是总申请量，跑歪了那一条会多要或少要一块。
func TestThreadClassHandedOver(t *testing.T) {
	out, code, total := runHeapAny(t, `
class Count {
    n:int
}
method build(box:Count):int {
    box.n = box.n + 1
    return box.n
}
let c:Count = new Count(n: 41)
let t:Thread<int> = thread.start(build, c)
console.print(t.wait())
`)
	if code != 0 || !strings.HasPrefix(out, "42\n") {
		t.Fatalf("得到 %q 码 %d，要开头 \"42\\n\" 码 0", out, code)
	}
	// 那个对象只申请一次：两条线程都从同一张空闲链表上拿，重复归还才会让读数变。
	if total < 42 || total > 500 {
		t.Fatalf("堆用量 %d 不在单份归还的形状里", total)
	}
}

// TestThreadUncaughtEndsOnlyThatThread 钉第 1.7.4 节第六句：那一条线程里没人接住的抛出只结束
// 那一条。屏幕上要有那一条线程自己那一句"出错了…"，主线程在那之后照旧跑完，退出码仍然是 0——
// 换成整个进程结束就看不到后面那一行。
func TestThreadUncaughtEndsOnlyThatThread(t *testing.T) {
	got, code := run(t, `
method crash(n:int):int {
    throw (7, "这一条不走了")
}
let t:Thread<int> = thread.start(crash, 1)
console.print("主线程还在")
try {
    console.print(t.wait())
} catch (code, msg, at) {
    console.print("接住了", code)
}
`)
	if code != 0 {
		t.Fatalf("未接住的抛出只结束那一条线程，主线程的退出码仍是 0，得到 %d\n%s", code, got)
	}
	if !strings.Contains(got, "这一条不走了") {
		t.Fatalf("那一条线程自己那一句没打出来：%q", got)
	}
	if !strings.Contains(got, "主线程还在") || !strings.Contains(got, "接住了 10094") {
		t.Fatalf("主线程没接住那一条线程留下的错误号：%q", got)
	}
}

// TestThreadWaitTwiceThrows 钉等过一次之后那个名字就是空句柄：再等一次是 10092 号那一句，
// 与对着空句柄取成员那一族同一答复。
func TestThreadWaitTwiceThrows(t *testing.T) {
	got, code := run(t, `
method work(n:int):int {
    return n
}
let t:Thread<int> = thread.start(work, 1)
console.print(t.wait())
try {
    console.print(t.wait())
} catch (code, msg, at) {
    console.print("第二次是", code)
}
`)
	if code != 0 || !strings.Contains(got, "第二次是 10092") {
		t.Fatalf("等过两次要报 10092：%q 码 %d", got, code)
	}
}

// TestThreadScopeEndAutoWait 钉名字走到作用域尽头那一次自动等。这里不能写成顶层：顶层那些名字
// 走到的是 `nef.release_top` 那一段，它归还的是程序级的那批槽，一条线程的句柄不在里面；
// 写在一个函数里，那一条 `let` 走到的是这一帧的归还记录，`nef.destroy` 的 `ir.KindThread` 那一案
// 才是发射出来的那一次自动等。
//
// 让它出错结束：屏幕上要有那一条线程自己那一句，主线程在那之后照旧跑完，退出码仍然是 0，
// 而且屏幕上不出现 10094 那一句——那一次自动等不抛，抛在归还代码中间会把后面的归还打断。
func TestThreadScopeEndAutoWait(t *testing.T) {
	got, code := run(t, `
method crash(n:int):int {
    throw (9, "自己结束")
}
method leave() {
    let t:Thread<int> = thread.start(crash, 1)
    console.print("不写 wait")
}
leave()
console.print("尽头之后")
`)
	if code != 0 {
		t.Fatalf("作用域尽头的自动等不该改退出码：%q 码 %d", got, code)
	}
	if !strings.Contains(got, "自己结束") || !strings.Contains(got, "尽头之后") {
		t.Fatalf("那一条线程没被自动等完：%q", got)
	}
	if strings.Contains(got, "10094") {
		t.Fatalf("作用域尽头那一次自动等不该抛 10094：%q", got)
	}
}

// TestThreadConcurrentSameListPush 是段 4 攒下的第一条：两条线程各拿一只清单，同时往同一块堆上
// 申请与归还。堆锁与 `lock xadd` 那一次落的正是这里——去掉堆锁，空闲链表会在两条线程交错时断掉，
// 后果要么是进程直接停住（退出码 3221225477），要么是多算或少算一块。
func TestThreadConcurrentSameListPush(t *testing.T) {
	got, code := run(t, `
method fill(n:int):int {
    let xs:list<int> = []
    for (i:int in 0 until n) {
        xs.add(i)
    }
    return length(xs)
}
let a:Thread<int> = thread.start(fill, 200)
let b:Thread<int> = thread.start(fill, 300)
console.print(a.wait() + b.wait())
`)
	if code != 0 || got != "500\n" {
		t.Fatalf("两条线程各自推两百与三百项，要 \"500\\n\" 码 0，得到 %q 码 %d", got, code)
	}
}

// TestThreadConcurrentVersionCounter 钉那只"改过几次"的计数：两条线程各自改各自的清单时，
// 那一次原子加一不能合成一次。这一段能观察到的形状是"两边各数到自己的那一份"，
// 与普通加一在两条线程同时命中同一格时才会丢的那一次，这里用两只清单各改一百次把它跑出来。
func TestThreadConcurrentVersionCounter(t *testing.T) {
	got, code := run(t, `
method churn(n:int):int {
    let xs:list<int> = []
    for (i:int in 0 until n) {
        xs.add(i)
        xs[0] = i
    }
    return length(xs) + xs[0]
}
let a:Thread<int> = thread.start(churn, 100)
let b:Thread<int> = thread.start(churn, 50)
console.print(a.wait() + b.wait())
`)
	if code != 0 || got != "298\n" {
		t.Fatalf("两条线程各推到一百与五十，要 \"298\\n\" 码 0，得到 %q 码 %d", got, code)
	}
}

// TestThreadPrintLinesNotSplit 钉第 1.7.2 节第十一处那一句的落地：两条线程同时打印时，一行不会被
// 拆成两截。屏幕上是四条"甲…乙"这样的成对文本，交错只会发生在行与行之间，不会在一行中间。
func TestThreadPrintLinesNotSplit(t *testing.T) {
	got, code := run(t, `
method say(n:int):int {
    for (i:int in 0 until 50) {
        console.print("线程", n, "的第", i, "行")
    }
    return n
}
let a:Thread<int> = thread.start(say, 1)
let b:Thread<int> = thread.start(say, 2)
console.print(a.wait(), b.wait())
`)
	if code != 0 {
		t.Fatalf("两条线程各打五十行要正常结束，码 %d", code)
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\r\n"), "\n") {
		if !strings.HasPrefix(line, "线程") {
			continue // 最后那一行是两条线程各自的结果，形状是 "1 2"
		}
		if strings.Count(line, "线程") != 1 {
			t.Fatalf("有一行被拆开了：%q", line)
		}
	}
	if !strings.Contains(got, "1 2\n") {
		t.Fatalf("最后那一行没交出两条线程各自的结果：%q", got)
	}
}

// TestThreadTableFullThrows 钉那张表的一百二十八格用完时那一句 10095 号能被人接住。
// 格子被占住到等它的人才来收为止，所以一百二十八条还没等过的句柄就把表填满，第一百二十九条
// 当场报——那一条线程根本没起，接住之后照旧能往下走。
//
// 一百二十八条句柄写成一百二十八行 `let`：`Thread<int>` 进不了容器，这是语言里唯一写得出的形状。
func TestThreadTableFullThrows(t *testing.T) {
	var src strings.Builder
	src.WriteString(`
method busy(n:int):int {
    let sum:int = 0
    for (i:int in 0 until n) {
        sum = sum + i
    }
    return sum
}
method fill() {
`)
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&src, "    let a%d:Thread<int> = thread.start(busy, 40)\n", i)
	}
	src.WriteString(`    try {
        let extra:Thread<int> = thread.start(busy, 40)
        console.print("第一百二十九条起成了")
    } catch (code, msg, at) {
        console.print("接住了", code)
    }
}
fill()
console.print("都等完了")
`)
	got, code := run(t, src.String())
	if code != 0 {
		t.Fatalf("接住 10095 之后要正常结束：%q 码 %d", got, code)
	}
	if !strings.Contains(got, "接住了 10095") || strings.Contains(got, "起成了") {
		t.Fatalf("第一百二十九条要当场报 10095：%q", got)
	}
	if !strings.Contains(got, "都等完了") {
		t.Fatalf("一百二十八条句柄走到作用域尽头没等完：%q", got)
	}
}

// TestTwoThreadsHeapAlone 是段 4 攒下的第二条：两条线程同时在那一份堆上拿与放。
// 各拿五十项的清单，一份在起线程之前用、一份在之后用——三次的读数必须一样，
// 因为堆只有一张空闲链表，两条线程交错时没有锁就会把它断开。断开之后的形状是进程停住
// （退出码 3221225477）或多算一块，所以这一条同时钉读码与钉总数。
//
// en: TestTwoThreadsHeapAlone is the second of the three deferred from segment 4: two threads
// allocating and releasing on the one heap at the same time. Three lists of fifty items each -
// one before the threads, two inside them - must report the same numbers, because there is one
// free list and interleaving without a lock breaks it. A break looks like a stalled process
// (exit code 3221225477) or one block counted twice, so this pins both the code and the totals.
func TestTwoThreadsHeapAlone(t *testing.T) {
	out, code, _ := runHeapAny(t, `
method build(n:int):int {
    let xs:list<int> = []
    for (i:int in 0 until n) {
        xs.add(i * 2)
    }
    let sum:int = 0
    for (v:int in xs) {
        sum = sum + v
    }
    return sum
}
let before:int = build(50)
let a:Thread<int> = thread.start(build, 50)
let b:Thread<int> = thread.start(build, 50)
console.print(before, a.wait(), b.wait())
`)
	if code != 0 {
		t.Fatalf("两条线程同时在堆上拿放要正常结束（堆锁没护住时是 3221225477）：%q 码 %d", out, code)
	}
	if !strings.HasPrefix(out, "2450 2450 2450\n") {
		t.Fatalf("三份各五十项的读数要相同：%q", out)
	}
}

// TestTwoThreadsContainerTouch 钉那一只"改过几次"的计数在两条线程各自跑时不互相顶掉。
// 两只清单各推五十项、各自读回第一项：两条线程各自的清单各数各的，跑完两边的读数都是 49。
//
// 这一条钉不到原子加一本身——两条线程碰的是两个对象，同一格从来没被两边同时命中过。钉得住的是
// 另一件事：那一次推进不因为另一条线程也在推进而丢掉自己那一份。
//
// en: TestTwoThreadsContainerTouch pins that one thread's bump does not cost the other its own.
// It cannot pin the atomic add itself: the two lists are two objects, so no cell is ever hit by
// both at once. What it does pin is that a bump is not lost while another thread bumps too.
func TestTwoThreadsContainerTouch(t *testing.T) {
	got, code := run(t, `
method churn(n:int):int {
    let xs:list<int> = []
    for (i:int in 0 until n) {
        xs.add(i)
    }
    let last:int = 0
    for (i:int in 0 until n) {
        xs[0] = i
        last = xs[0]
    }
    return last
}
let a:Thread<int> = thread.start(churn, 50)
let b:Thread<int> = thread.start(churn, 50)
console.print(a.wait(), b.wait())
`)
	if code != 0 || got != "49 49\n" {
		t.Fatalf("两条线程各推到四十九，要 \"49 49\n\" 码 0，得到 %q 码 %d", got, code)
	}
}

// TestSampleThread 把整份 `samples/thread.nf` 编成真实产物跑一遍。这一份示例的输出每回相同：
// 屏幕上那三行只由一条线程写，而主线程等到它跑完才往下走，所以那三行一定排在前面。
func TestSampleThread(t *testing.T) {
	const path = "../../../samples/thread.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, string(raw)), "thread.exe")
	want := "线程写的第 1 行\n线程写的第 2 行\n线程写的第 3 行\n" +
		"5050 甲号 42\n3 11\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("thread.nf 跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

// TestThreadReleaseRoutesThroughDestroy 钉那一次自动等走的是形状表那一路。结构上看得出来的两件事：
// 用户函数自己的收尾里叫的是 `nef.destroy`，`@thread_join` 只出现在 `nef.destroy` 那一段里。
//
// 另一条写法是把 `@thread_join` 直接发在块尾（`closerOf` 那一张表就是干这件事的）。那样发出来的
// 是一次丢弃返回值的单入参调用，而等一条线程跑完还要按它返回的那一个值的形状归还它手里那块内存，
// 那一张形状表只在 `nef.destroy` 里查得到——所以那一档故意不进 `closerOf`。
func TestThreadReleaseRoutesThroughDestroy(t *testing.T) {
	const src = `
method w(n:int):string {
    return "甲"
}
method leave():int {
    let t:Thread<string> = thread.start(w, 1)
    return 0
}
console.print(leave())
`
	p := progOf(t, src)
	leave := findFn(p, "leave")
	if leave == nil {
		t.Fatal("leave 没排出来")
	}
	if join := idxOf(leave, func(in ir.Inst) bool { return isCall(in, "@thread_join") }); join >= 0 {
		t.Fatalf("块尾直接发了 @thread_join（第 %d 条）：那一步按返回值形状归还的那一半就没了", join)
	}
	if d := idxOf(leave, func(in ir.Inst) bool { return isCall(in, "nef.destroy") }); d < 0 {
		t.Fatal("块尾没有 nef.destroy：线程句柄的收集点应在形状表那一条路上")
	}
	destroy := findFn(p, "nef.destroy")
	if destroy == nil {
		t.Fatal("nef.destroy 没排出来")
	}
	if idxOf(destroy, func(in ir.Inst) bool { return isCall(in, "@thread_join") }) < 0 {
		t.Fatal("nef.destroy 里没有 @thread_join：ir.KindThread 那一案没排进去")
	}
}

// TestThreadIdsAreTableSlots 钉句柄身上那一个数就是线程表的表项：两条同时挂着的是 1 与 2，
// 等过之后那两个位置抹干净，第三条扫到的是同一格，于是又是 1。
// `thread.id` 不发任何例程、也不改动那个位置，所以这一处钉的是"交出的就是那一个数"，
// 换成人看的另一种编号（序号、系统号码），这一条就失败。
//
// en: TestThreadIdsAreTableSlots pins that the number inside a thread handle is the slot in the
// thread table: two live threads are 1 and 2, both waitings wipe their entries, and the third start
// scans the same first free slot, so it reads 1 again. thread.id emits no routine and leaves the
// entry alone, which is why this pins "the number handed back is that slot" - swap it for any other
// human-facing id (an ordinal, the system's number) and this turns red.
func TestThreadIdsAreTableSlots(t *testing.T) {
	got, code := run(t, `
method work(n:int):int {
    return n
}
let a:Thread<int> = thread.start(work, 1)
let b:Thread<int> = thread.start(work, 2)
console.print(a.id(), b.id())
console.print(a.wait(), b.wait())
let c:Thread<int> = thread.start(work, 3)
console.print(c.id())
console.print(c.wait())
`)
	want := "1 2\n1 2\n1\n3\n"
	if code != 0 || got != want {
		t.Fatalf("句柄上的数要就是表项编号：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestWorkerStackGuardPrintsOneLine 钉那一段每帧开场的栈检查在第二条线程上同样生效：
// 二十万层递归在这一条线程里撞出 10093 那一句，只结束这一条，主线程等到的是 10094。
// 这一段之前只在主线程上测过（`stack_e2e_test.go` 那两条），而算栈底那一步每一条线程各算一次
// （`@thread_begin` 里从 `tebStackBase` 减出来的），漏了那一步的话这一条线程会一路走到操作系统
// 终止进程，屏幕上那一句和退出码 0 都没有。
//
// en: TestWorkerStackGuardPrintsOneLine pins that the per-frame stack check works on a second
// thread too: two hundred thousand recursive levels trip code 10093 inside that thread, only that
// thread ends, and the main thread's wait reads 10094. Before this, the guard was only tested on the
// main thread (the two cases in stack_e2e_test.go), while the stack floor is computed per thread (the
// subtraction from tebStackBase inside @thread_begin). Skip that step and this thread walks straight
// into the operating system terminating the process: neither the printed line nor exit code 0 appears.
func TestWorkerStackGuardPrintsOneLine(t *testing.T) {
	got, code := run(t, `
method walk(n:int, seen:int, depth:int):int {
    if (n == 0) {
        return seen + depth;
    }
    return walk(n - 1, seen + 1, depth + 1);
}
method dive(n:int):int {
    return walk(n, 0, 0);
}
let t:Thread<int> = thread.start(dive, 200000)
try {
    console.print(t.wait())
} catch (c, msg, at) {
    console.print("接住了", c)
}
console.print("主线程还在")
`)
	if code != 0 {
		t.Fatalf("那一条线程用完栈只结束那一条，主线程的退出码仍是 0，得到 %d\n%s", code, got)
	}
	if !strings.Contains(got, stackLine) {
		t.Fatalf("第二条线程上那段每帧开场的栈检查没生效，屏幕上少的是那一句 10093：%q", got)
	}
	if !strings.Contains(got, "接住了 10094") || !strings.Contains(got, "主线程还在") {
		t.Fatalf("主线程没等到那一条线程的错误结束：%q", got)
	}
}

// TestThreadGenericInstanceArg 钉泛型的实例交进另一条线程：白名单那一判问的是归还点定没定，
// `class` 句柄那一档按改写后的那句理由是开着的，实例走的正是同一档，不是另开的一条口子。
// 两头都要看得见的证据：交进去的那一只在另一条线程里读得出字段，等回来的那一只在自己这一头
// 按实例自己的形状释放（少了形状表那个位置就是 3221225477）。
//
// en: TestThreadGenericInstanceArg pins handing a generic instance to another thread. The whitelist
// asks whether a type's release point is per thread yet; the class-handle row is open under ruling
// 123's new reason, and an instance falls in exactly that row rather than through a separate door.
// Both ends need visible evidence: the handed-over box reads its field on the other thread, and the
// one that comes back is released along its own instance's shape entry (without that slot the run
// dies with 3221225477).
func TestThreadGenericInstanceArg(t *testing.T) {
	got, code := run(t, `
class Box<Item> {
    v:Item
}

method unwrap(b:Box<int>):int {
    return b.v
}

let g:Box<int> = new Box<int>(v: 41)
let t:Thread<int> = thread.start(unwrap, g)
console.print(t.wait())
`)
	if code != 0 || got != "41\n" {
		t.Fatalf("泛型实例交进另一条线程：\n got %q 码 %d\nwant \"41\n\" 码 0", got, code)
	}
}

// TestThreadGenericStructArg 钉泛型 `struct` 当实参：值类型那一档交出去的是拷的一份，
// 所以原来那一个名字在这之后照旧读得出来。这一处与上一条合起来才是白名单那两句的两种形状。
//
// en: TestThreadGenericStructArg pins a generic struct as the argument: the value-type row hands over
// a copy, so the original name still reads afterwards. Together with the case above this is what the
// two whitelist rows actually look like.
func TestThreadGenericStructArg(t *testing.T) {
	got, code := run(t, `
struct Pnt<N> {
    x:N
}

method sum(p:Pnt<int>):int {
    return p.x + 1
}

let a:Pnt<int> = Pnt<int> { x: 8 }
let t:Thread<int> = thread.start(sum, a)
console.print(t.wait(), a.x)
`)
	if code != 0 || got != "9 8\n" {
		t.Fatalf("泛型 struct 交进去要的是拷一份、原来那个名字照旧读得出来：\n got %q 码 %d\nwant \"9 8\n\"", got, code)
	}
}

// TestThreadReturnIsGenericInstance 钉 `Thread<T>` 的尖括号里写的就是一只实例：被起的那个函数
// 返回 `Box<int>`，句柄身上要写的还是那一条规则（跟着返回类型走），等回来的那一只按实例的形状
// 打印与释放。这一处不新裁规则，钉的是返回那一路与实参那一路不是两条分开的代码。
//
// en: TestThreadReturnIsGenericInstance pins an instance in Thread<T>: the started function returns
// Box<int>, so the handle still follows the return type, and what comes back prints and releases along
// that instance's shape. Nothing new is ruled here; what is pinned is that the return path and the
// argument path are not two separate implementations.
func TestThreadReturnIsGenericInstance(t *testing.T) {
	got, code := run(t, `
class Box<Item> {
    v:Item
}

method make(n:int):Box<int> {
    return new Box<int>(v: n)
}

let t:Thread<Box<int>> = thread.start(make, 7)
let g:Box<int> = t.wait()
console.print(g, g.v)
`)
	if code != 0 || got != "Box<int> { v: 7 } 7\n" {
		t.Fatalf("尖括号里写一只实例的那一路：\n got %q 码 %d\nwant \"Box<int> { v: 7 } 7\n\"", got, code)
	}
}

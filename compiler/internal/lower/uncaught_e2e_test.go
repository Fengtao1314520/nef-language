// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// uncaught_e2e_test.go 覆盖一条规则：**无人接住的抛出同样要走顶层那份释放记录**。
//
// nef.main 开场挂一条**兜底处理器**：抛出走完中间各帧的帧内清理记录后仍无人接住时命中这一段——
// 打印提示、释放顶层的块、以退出码 1 结束，与"正常走完"和 `exit` 的释放行为对齐；
// 若没有这条处理器，`@throw_walk` 命中空链时会直接"打印提示 → @exit(1)"，跳过 `nef.release_top`。
//
// 两条测试各覆盖一侧：这条规则**从外部观察不到差别**（`NEF_HEAP` 报的是向堆累计申请的
// 总字节数，块归还空闲链不会减少它，三种收尾的堆用量因此完全相同）。结构测试验证"调用有没有、
// 排在哪、护栏在不在"；端到端测试验证"这条路径不破坏内存"——进入的是 nef.main 的帧，
// 要释放的块按静态槽那份记录处理，栈布局错误会释放别人持有的内存。

package lower_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/ir"
)

// runHeapAny 与 runHeap 相同，但不要求退出码为 0——`exit` 与无人接住的抛出两条路径都要核对退出码与堆用量。
func runHeapAny(t *testing.T, src string) (string, int, int64) {
	t.Helper()
	t.Setenv("NEF_HEAP", "1")
	out, code := run(t, src)
	re := regexp.MustCompile(`(?m)^heap (\d+)(?: \d+)?\r?\n$`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("没看到堆用量那一行：%q", out)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatalf("堆用量那一行读不成数：%q", out)
	}
	return strings.Replace(out, m[0], "", 1), code, n
}

// idxOf 返回这一个函数里第一条命中条件的指令下标，找不到返回 -1。
func idxOf(fn *ir.Func, hit func(ir.Inst) bool) int {
	for i, in := range fn.Body {
		if hit(in) {
			return i
		}
	}
	return -1
}

func isCall(in ir.Inst, name string) bool {
	c, ok := in.(*ir.Call)
	return ok && c.Fn == name
}

func isTopCall(in ir.Inst) bool {
	c, ok := in.(*ir.Call)
	return ok && strings.HasPrefix(c.Fn, "nef.top.")
}

// countOf 数一种指令在这一个函数里出现几次。
func countOf(fn *ir.Func, hit func(ir.Inst) bool) int {
	n := 0
	for _, in := range fn.Body {
		if hit(in) {
			n++
		}
	}
	return n
}

// 共同的顶层场景：一个 `class`、一个 list、一段文本都登记在静态槽上，抛出点在三层调用之下。
const uncaughtSrc = `
class Bag {
    n:int
    kids:list<int>
}
method deep3(b:Bag):int {
    let tmp:list<string> = ["甲", "乙"]
    throw (120, "不干了")
    return length(tmp)
}
method deep2(b:Bag):int {
    let inner:Bag = new Bag(n: b.n, kids: [b.n])
    return deep3(inner)
}
method deep1(b:Bag):int {
    return deep2(b)
}
let keep:Bag = new Bag(n: 7, kids: [1, 2, 3])
let names:list<string> = ["甲乙丙".slice(0, 3)]
console.print("跑到这儿")
console.print(deep1(keep))
`

// TestUncaughtThrowRunsTopLevelAccount 验证中间码里的调用排列（这类结构性质在端到端运行中观察不到）。
func TestUncaughtThrowRunsTopLevelAccount(t *testing.T) {
	p := progOf(t, uncaughtSrc)
	main := findFn(p, "nef.main")
	if main == nil {
		t.Fatal("nef.main 没排出来")
	}
	release := findFn(p, "nef.release_top")
	if release == nil {
		t.Fatal("nef.release_top 没排出来")
	}

	push := idxOf(main, func(in ir.Inst) bool { _, ok := in.(*ir.PushHandler); return ok })
	pop := idxOf(main, func(in ir.Inst) bool { _, ok := in.(*ir.PopHandler); return ok })
	lastTop := idxOfLast(main, isTopCall)
	exit0 := idxOf(main, func(in ir.Inst) bool { return isCall(in, "@exit") })
	errline := idxOf(main, func(in ir.Inst) bool { return isCall(in, "@put_errline") })
	if push < 0 || pop < 0 || errline < 0 {
		t.Fatalf("兜底那一段缺指令：挂 %d 摘 %d 印 %d", push, pop, errline)
	}
	// 挂载必须排在任何用户代码之前，否则抛出无法命中它。
	if push > firstTop(main) {
		t.Fatalf("兜底那条要排在所有 nef.top.* 之前：挂 %d、头一调 %d", push, firstTop(main))
	}
	// 正常走完的路径先摘除兜底记录再做顶层释放——不摘除会留下一条指向程序尽头的孤立记录。
	if !(lastTop < pop && pop < exit0) {
		t.Fatalf("正常走完那一条要摘掉兜底那条再叫 @exit：末调 %d、摘 %d、@exit %d", lastTop, pop, exit0)
	}
	// 兜底那一段的顺序：Catch → 打印提示 → 检查护栏 → 释放该释放的块（若不在释放中）→ 退出码 1。
	if !(exit0 < errline) {
		t.Fatalf("印那一句的 @put_errline 要在 @exit(0) 之后那一段里：%d 与 %d", exit0, errline)
	}
	if idxOf(main, func(in ir.Inst) bool { _, ok := in.(*ir.Catch); return ok }) != errline-1 {
		t.Fatal("兜底那一段的开头不是 Catch（错误码与消息要先落进两个槽位，后面要调用）")
	}
	// 护栏：release_top 进门时设立"正在释放"标记，兜底那一段在调用它之前读的是**同一个槽**。
	flag, ok := release.Body[0].(*ir.Mov)
	if !ok || flag.S != ir.K(1) {
		t.Fatalf("nef.release_top 开头不是\"立一块正在还的标记\"：%v", release.Body[0])
	}
	g, isG := flag.D.(ir.G)
	if !isG {
		t.Fatalf("那块标记不在静态槽上：%v", flag.D)
	}
	read := -1
	for i := errline + 1; i < len(main.Body); i++ {
		if m, ok := main.Body[i].(*ir.Mov); ok && m.S == g {
			read = i
			break
		}
	}
	if read < 0 {
		t.Fatalf("兜底那一段没读那块标记 %v——护栏不在", g)
	}
	// 护栏之后的那一步：不在释放中就先释放，然后退出码 1。这一段是这条规则的全部要点，必须固定。
	rel2 := -1
	for i := read + 1; i < len(main.Body); i++ {
		if isCall(main.Body[i], "nef.release_top") {
			rel2 = i
			break
		}
	}
	if rel2 < 0 {
		t.Fatal("兜底那一段没叫 nef.release_top——没人接的抛出又绕过顶层那份记录了")
	}
	if idxOfLast(main, func(in ir.Inst) bool { return isCall(in, "@exit") }) <= rel2 {
		t.Fatal("兜底那一段的 @exit(1) 要排在该还的账之后")
	}
	// 调用 release_top 的那条排在读标记之后（读标记正是为了决定调用与否），且全函数只挂一条处理器记录：
	// 再次进入这一段时处理器链上已无记录，配合链空时"打印后即返回"，递归最多一层。
	if countOf(main, func(in ir.Inst) bool { _, ok := in.(*ir.PushHandler); return ok }) != 1 {
		t.Fatal("nef.main 挂了不止一颗处理器——release_top 里再抛出就可能成环")
	}
	if countOf(main, func(in ir.Inst) bool { return isCall(in, "@put_errline") }) != 1 {
		t.Fatal("@put_errline 在 nef.main 里该只被兜底那一段叫一次")
	}
}

func firstTop(fn *ir.Func) int {
	for i, in := range fn.Body {
		if isTopCall(in) {
			return i
		}
	}
	return -1
}

func idxOfLast(fn *ir.Func, hit func(ir.Inst) bool) int {
	at := -1
	for i, in := range fn.Body {
		if hit(in) {
			at = i
		}
	}
	return at
}

// TestThreeEndingsAligned 是三种收尾的现场对照：正常走完、`exit`、无人接住的抛出。
//
// 三条收尾的堆内存占用**完全一致是设计结果**（`NEF_HEAP` 读的是向堆累计申请的总字节数，归还空闲链不减少它），
// 所以这里核对"打印了什么、退出码是多少"，另加一条：抛出那条路径不许把顶层的块释放第二次。
func TestThreeEndingsAligned(t *testing.T) {
	normal := `
class Bag {
    n:int
}
let keep:Bag = new Bag(n: 3)
console.print("跑到尽头", keep.n)
`
	withExit := `
class Bag {
    n:int
}
let keep:Bag = new Bag(n: 3)
console.print("先印一行", keep.n)
exit (100, "收工")
`
	uncaught := `
class Bag {
    n:int
}
let keep:Bag = new Bag(n: 3)
console.print("跑到尽头", keep.n)
throw (120, "不干了")
`
	for i, one := range []struct {
		src  string
		body string
		code int
	}{
		{normal, "跑到尽头 3\n", 0},
		{withExit, "先印一行 3\n收工\n", 100},
		{uncaught, "跑到尽头 3\n出错了（120）：不干了\n", 1},
		{uncaught, "跑到尽头 3\n出错了（120）：不干了\n", 1}, // 同一场景运行两遍，第二遍验证重复执行结果一致
	} {
		body, code, heap := runHeapAny(t, one.src)
		if body != one.body || code != one.code {
			t.Fatalf("第 %d 条收尾跑出来不一样：\n got %q 码 %d\nwant %q 码 %d", i+1, body, code, one.body, one.code)
		}
		if heap <= 0 {
			t.Fatalf("第 %d 条收尾没量到堆：%d", i+1, heap)
		}
		if i == 2 && heap != 152 {
			t.Fatalf("三条收尾共同的堆内存占用变了（先前 152）：%d——有人在这条新路上多要了块", heap)
		}
	}
}

// TestUncaughtThrowFromDepth 用实际运行覆盖"抛出发生在三帧之下、顶层还登记着几块"的场景：
// 兜底那一段进入的是 nef.main 的帧，释放的是静态槽那份记录登记的块——栈摆错就会析构别人持有的内存。
func TestUncaughtThrowFromDepth(t *testing.T) {
	body, code, _ := runHeapAny(t, uncaughtSrc)
	want := "跑到这儿\n出错了（120）：不干了\n"
	if body != want || code != 1 {
		t.Fatalf("三帧之下的抛出没走到兜底那一段：\n got %q 码 %d\nwant %q 码 1", body, code, want)
	}
}

// TestUncaughtRethrowPrintsOnce 验证"接住块里再抛也只打印一次提示"：再抛命中兜底那一段之前，
// 该条记录已被 @throw_walk 摘掉——第二次抛出走的是链空那一步，不会成环。
func TestUncaughtRethrowPrintsOnce(t *testing.T) {
	body, code, _ := runHeapAny(t, `
let xs:list<int> = [1]
try {
    throw (121, "头一趟")
} catch (code, msg) {
    console.print("接住了", code, length(xs))
    throw (122, "再抛一趟")
}
console.print("这行不该印")
`)
	want := "接住了 121 1\n出错了（122）：再抛一趟\n"
	if body != want || code != 1 {
		t.Fatalf("接住块里再抛没收好：\n got %q 码 %d\nwant %q 码 1", body, code, want)
	}
	if strings.Count(body, "出错了") != 1 {
		t.Fatalf("那一句印了不止一次，像是绕回来了：%q", body)
	}
}

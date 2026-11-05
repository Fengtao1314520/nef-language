// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// trace_e2e_test.go 钉无人接住的抛出打印的那一条调用链：一帧一行、每行"    位置 函数名"，
// 层数不设上限，有人接住时不打，抛出在用户代码最外面那一层时也不打。
// 最后一条跑的是 `samples/trace.nf` 这一份示例程序。
//
// 表里装了什么读不回来（产物里那张表没有对外入口），所以这几条钉的都是屏幕上的结果；
// 项与项的顺序、第 0 个位置那个返回地址偏移由 `internal/codegen` 的 trace_test.go 钉。
package lower_test

import (
	"os"
	"strings"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// TestUncaughtThrowPrintsChain 是三帧之下抛出没人接住的形状：最里那一行是抛出点所在的那一个
// 函数，往外的每一行是"这个函数跑到哪一次调用了"，最外面那一行是文件的顶层。
func TestUncaughtThrowPrintsChain(t *testing.T) {
	got, code := run(t, `
method deep(n:int) {
    throw (135, "没人接")
}
method middle(n:int) {
    deep(n)
}
middle(2)
`)
	want := "出错了（135）：没人接（在 t.nf 3:5）\n" +
		"    t.nf 3:5 deep\n    t.nf 6:5 middle\n    t.nf 8:1 t.nf\n"
	if got != want || code != 1 {
		t.Fatalf("链的形状不对：\n got %q 码 %d\nwant %q 码 1", got, code, want)
	}
}

// TestCaughtThrowPrintsNoChain 钉那一条"链只在没人接住的那一次给"：有人接住的抛出照旧走，
// 屏幕上不多打一行。
func TestCaughtThrowPrintsNoChain(t *testing.T) {
	got, code := run(t, `
method deep(n:int) {
    throw (135, "有人接")
}
method middle(n:int):int {
    deep(n)
    return 0
}
let v:int = 0
try {
    v = middle(1)
} catch (code, msg, at) {
    console.print("接住了", code, msg)
}
console.print(v)
`)
	if code != 0 {
		t.Fatalf("接住之后退出码该是 0：%d（屏幕：%q）", code, got)
	}
	if strings.Contains(got, "    ") {
		t.Fatalf("有人接住的抛出不该打链：%q", got)
	}
	if want := "接住了 135 有人接\n0\n"; got != want {
		t.Fatalf("接住那一路的屏幕不对：\n got %q\nwant %q", got, want)
	}
}

// TestThrowInTopLevelPrintsNoChain 钉表里查不到那一项时一行也不打：文件的顶层由运行期直接调用，
// 没有哪一项说得出它是被谁调进来的，那一行的位置本来就在"出错了…"那一句里带着。
func TestThrowInTopLevelPrintsNoChain(t *testing.T) {
	got, code := run(t, `
let n:int = 1
throw (136, "就在顶层")
`)
	want := "出错了（136）：就在顶层（在 t.nf 3:1）\n"
	if got != want || code != 1 {
		t.Fatalf("顶层抛出该只有一行：\n got %q 码 %d\nwant %q 码 1", got, code, want)
	}
}

// TestChainIsNotCapped 用 70 层递归钉"全打、不设层数上限"，同时钉捕获那串位置的扩块：
// 头一块装 64 项，第 65 项起换一块大的、已抄的搬过去、旧的当场还掉。
// 行数比帧数多一：最外面还有一行是调用最深那一次的顶层。
func TestChainIsNotCapped(t *testing.T) {
	got, code := run(t, `
method down(n:int) {
    if (n <= 0) {
        throw (150, "到底了")
    }
    down(n - 1)
}
down(70)
`)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if code != 1 {
		t.Fatalf("退出码该是 1：%d（屏幕开头：%q）", code, lines[0])
	}
	if len(lines) != 1+72 {
		t.Fatalf("该是 1 行提示加 72 行链（71 层 down 与顶层）：%d 行", len(lines))
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "    ") {
			t.Fatalf("链上有一行没缩进：%q", line)
		}
	}
	if want := "    t.nf 4:9 down"; lines[1] != want {
		t.Fatalf("最里那一行不对：\n got %q\nwant %q", lines[1], want)
	}
	if last := lines[len(lines)-1]; !strings.HasSuffix(last, " t.nf") {
		t.Fatalf("最外那一行该是文件的顶层：%q", last)
	}
}

// TestRuntimeThrowCarriesChain 钉运行期自己抛出的那几条也带链：下标越界（10031 号）发生在
// 两帧之下时，链上说得清是从哪几层传进来的；位置还是那一步自己的位置。
func TestRuntimeThrowCarriesChain(t *testing.T) {
	got, code := run(t, `
method pick(xs:list<int>):int {
    return xs[9]
}
method wrapper():int {
    return pick([1, 2, 3])
}
console.print(wrapper())
`)
	if code != 1 {
		t.Fatalf("越界该以 1 收场：%d（屏幕：%q）", code, got)
	}
	if !strings.HasPrefix(got, "出错了（10031）：下标越界（在 t.nf 3:14）\n") {
		t.Fatalf("第一行不对：%q", got)
	}
	if !strings.Contains(got, "    t.nf 3:14 pick\n") || !strings.Contains(got, "    t.nf 6:12 wrapper\n") {
		t.Fatalf("链里缺了那两层：%q", got)
	}
}

// TestCatchTraceNameHoldsTheChain 钉接住块的第四个名字：写了 `trace` 就有整条链的一份文本，
// 内容与屏幕上那一段一样（一行一层，前面四个空格）。前三个名字照旧，写三个不写第四个也对。
// TestSampleTracePrintsChain 把 `samples/trace.nf` 编成真实产物跑一遍：先是接住那一段
// （`trace` 那一份文本自己打出来），最后那一次抛出没人接住，屏幕上"出错了…"那一句下面
// 跟着三行链。这份示例按 `1` 结束，那是它要演示的那条结束方式。
func TestSampleTracePrintsChain(t *testing.T) {
	const path = "../../../samples/trace.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, string(raw)), "trace.exe")
	want := "接住了: 10031 下标越界\n" +
		"在哪一步: trace.nf 15:17\n" +
		"哪几层传进来的:\n" +
		"    trace.nf 15:17 read_word\n" +
		"    trace.nf 19:12 first_of\n" +
		"    trace.nf 27:11 trace.nf\n" +
		"\n" +
		"接住之后照旧往下跑: 5\n" +
		"下面这一次没人接住\n" +
		"出错了（10031）：下标越界（在 trace.nf 15:17）\n" +
		"    trace.nf 15:17 read_word\n" +
		"    trace.nf 19:12 first_of\n" +
		"    trace.nf 41:15 trace.nf\n"
	if r.Stdout != want || r.Code != 1 {
		t.Fatalf("trace.nf 跑出来是 %q 码 %d，要\n%q码 1", r.Stdout, r.Code, want)
	}
}

func TestCatchTraceNameHoldsTheChain(t *testing.T) {
	got, code := run(t, `
method deep(n:int) {
    throw (135, "有人接")
}
method middle(n:int):int {
    deep(n)
    return 0
}
let v:int = 0
try {
    v = middle(1)
} catch (code, msg, at, trace) {
    console.print(code, msg, at)
    console.print(trace)
}
console.print(v)
`)
	want := "135 有人接 t.nf 3:5\n" +
		"    t.nf 3:5 deep\n    t.nf 6:5 middle\n    t.nf 11:9 t.nf\n\n" + "0\n"
	if got != want || code != 0 {
		t.Fatalf("第四个名字不对：\n got %q 码 %d\nwant %q 码 0", got, code, want)
	}
}

// TestCatchThreeNamesStillWork 钉"第四个名字可以省"：只写三个的接住块一字不变，
// 也不在接住块开头多发那一条 @trace_text（屏幕上没有链，堆里也不多一块）。
func TestCatchThreeNamesStillWork(t *testing.T) {
	got, code := run(t, `
method deep(n:int) {
    throw (135, "有人接")
}
let v:int = 0
try {
    deep(1)
    v = 9
} catch (code, msg, at) {
    console.print("接住了", code, at)
}
console.print(v)
`)
	want := "接住了 135 t.nf 3:5\n0\n"
	if got != want || code != 0 {
		t.Fatalf("三个名字的接住块变了：\n got %q 码 %d\nwant %q 码 0", got, code, want)
	}
}

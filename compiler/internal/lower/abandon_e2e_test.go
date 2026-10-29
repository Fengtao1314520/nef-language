// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// abandon_e2e_test.go 覆盖抛出跨过的中间帧的释放逻辑。
//
// 抛出要从写 throw 的那一帧往外走到接住块为止：本帧登记的句柄在 @throw 之前释放；
// 若缺少跨帧处理，中间那几帧登记的句柄无人释放，接住块自身 try 体里登记的同样无人释放。
// 泄漏以整块为单位，表现为 heap_e2e_test.go 里"跨两帧抛"那一条的每圈堆用量持续增长。
//
// 机制是把帧内清理记录挂上处理器链：登记过句柄的帧在开场挂一条（@push_abandon），
// @throw_walk 走到时跳进那一帧尾部的清理代码，释放完成后调用 @throw_walk 继续向外走。
// 两侧都要固定：不漏释放，也**不多释放**——接住块还要读的那些名字必须保持有效。

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ir"
)

// crossSrc 是三帧：top 接住、middle 是中间帧（登记了一个句柄）、deep 写 throw。
// 三帧都持有对象；接住块读的 held 声明在 try 之外——它一个都不许被释放。
const crossSrc = `
method deep(n:int) {
    throw (130, "从最里头抛")
}

method middle(n:int):int {
    let mid:list<int> = [n, n]
    deep(n)
    return length(mid)
}

method top(n:int):int {
    let held:list<int> = [1, 2, 3]
    try {
        let inner:list<int> = [n]
        let walked:int = middle(n)
        return walked + length(inner) + length(held)
    } catch (code, msg) {
        console.print(code, msg, length(held))
        return 0 - code
    }
}

console.print(top(7))
`

// TestCrossFrameThrowReleasesMiddleFrames 是端到端用例：中间帧与接住块所在层的登记都已释放，
// 而 try 之外声明的句柄在接住块里仍可读——释放它会销毁仍在使用的对象。
func TestCrossFrameThrowReleasesMiddleFrames(t *testing.T) {
	got, code := run(t, crossSrc)
	if want := "130 从最里头抛 3\n-130\n"; got != want || code != 0 {
		t.Fatalf("跨帧抛出这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestCrossFrameThrowPadsChainBalance 检查处理器链的释放配对：接住块里再向外抛，两层各释放各的；
// 接住块读得到外层 try 体里登记的句柄（它已被内层抛出走过一次，槽位抹成 0 才是正确状态）。
func TestCrossFrameThrowPadsChainBalance(t *testing.T) {
	got, code := run(t, `
method boom(n:int) {
    let xs:list<int> = [n]
    throw (134, "跨帧嵌套")
}
method outer(n:int):int {
    let a:list<int> = [1]
    try {
        let b:list<int> = [2, 3]
        try {
            let c:list<int> = [3]
            boom(n)
        } catch (c1, m1) {
            console.print(c1, m1, length(b))
            throw (c1, m1)
        }
    } catch (c2, m2) {
        console.print(c2, m2, length(a))
        return length(a)
    }
    return 0
}
console.print(outer(9))
`)
	if want := "134 跨帧嵌套 2\n134 跨帧嵌套 1\n1\n"; got != want || code != 0 {
		t.Fatalf("接住块里再往外抛这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestCaughtThrowKeepsChainForLaterOne 固定这一点：接住块走完、帧从 Ret 退出后，处理器链必须干净。
// 标记未摘除时，后来一次无人接住的抛出会先撞上指向死帧的旧标记——跳进去既不报错、
// 也不打印，以退出码 0 结束。接住块那条路径曾出现过同一问题，本用例覆盖帧内清理记录这条路径。
func TestCaughtThrowKeepsChainForLaterOne(t *testing.T) {
	got, code := run(t, `
method deep(n:int) {
    throw (131, "中间帧抛")
}
method middle(n:int):int {
    let mid:list<int> = [n]
    deep(n)
    return length(mid)
}
method caught():int {
    try {
        return middle(1)
    } catch (c, m) {
        return 0 - c
    }
}
let x:int = caught()
console.print(x)
throw (132, "这一趟没人接")
`)
	if want := "-131\n出错了（132）：这一趟没人接\n"; got != want || code != 1 {
		t.Fatalf("接住之后再抛不对：\n got %q 码 %d\nwant %q 码 1", got, code, want)
	}
}

// TestUncaughtThrowStillReleasesFrames 覆盖该机制带来的附加效果：无人接住的路径同样从链头逐帧
// 走过，中间各帧的释放在打印"出错了"之前全部完成。程序随即退出、内存由操作系统回收，
// 但语义规则"每一层被摆掉的容器都析构"不取决于是否存在接住块。
func TestUncaughtThrowStillReleasesFrames(t *testing.T) {
	got, code := run(t, `
class Row {
    kids:list<int>
}
method deep(n:int) {
    throw (135, "没人接")
}
method middle(n:int) {
    let r:Row = new Row(kids: [n, n, n])
    deep(n)
}
middle(2)
`)
	if want := "出错了（135）：没人接\n"; got != want || code != 1 {
		t.Fatalf("没人接那一条不对：\n got %q 码 %d\nwant %q 码 1", got, code, want)
	}
}

// TestThrowIntoUntakenBranch 固定开场清零的登记槽位：未走到的分支里也可能登记了句柄，
// 帧内清理记录释放"本帧登记过的全部"——该槽位若残留上次调用的旧值，释放会拿无效值当句柄。
func TestThrowIntoUntakenBranch(t *testing.T) {
	got, code := run(t, `
method risky(n:int):int {
    if (n > 1000) {
        let far:list<int> = [n]
        return length(far)
    }
    throw (136, "没走到那一支")
    return 0
}
let total:int = 0
for (i:int in 0 until 5) {
    try {
        total = total + risky(i)
    } catch (c, m) {
        total = total + c
    }
}
console.print(total)
`)
	if want := "680\n"; got != want || code != 0 {
		t.Fatalf("没走到的那一支：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 中间码那一侧：开场先把登记槽位抹成 0，挂帧内清理记录紧随其后
	fn := findFn(progOf(t, `
method risky(n:int):int {
    let far:list<int> = [n]
    throw (136, "没走到那一支")
    return 0
}
console.print(risky(1))
`), "risky")
	if len(fn.Body) < 2 {
		t.Fatal("risky 的帧是空的")
	}
	mv, ok := fn.Body[0].(*ir.Mov)
	if !ok || mv.S != ir.K(0) {
		t.Fatalf("开场第一条该是把登记槽位抹成 0，实际 %s", fn.Body[0])
	}
	if _, ok := fn.Body[1].(*ir.PushAbandon); !ok {
		t.Fatalf("抹完零就该挂帧内清理记录，实际 %s", fn.Body[1])
	}
}

// findPad 返回帧尾那段帧内清理记录在 f.Body 中的起始下标（按标记记录的 Pad 标签查找对应 Lbl）。
func findPad(f *ir.Func) (int, ir.Label) {
	var pad ir.Label
	found := false
	for _, in := range f.Body {
		if pa, ok := in.(*ir.PushAbandon); ok {
			pad, found = pa.Pad, true
			break
		}
	}
	if !found {
		return -1, 0
	}
	for i, in := range f.Body {
		if l, ok := in.(*ir.Lbl); ok && l.At == pad {
			return i, pad
		}
	}
	return -1, pad
}

// TestAbandonPadShape 固定中间码那一段的形状：帧尾一段清理代码（按反序释放完毕后再调用 @throw_walk），
// 每一处 Ret 之前都摘一次标记。接住块路径的 PopHandler 由 tryStmt 自己发射，不在本用例范围内。
func TestAbandonPadShape(t *testing.T) {
	p := progOf(t, crossSrc)
	for _, name := range []string{"middle", "top"} {
		f := findFn(p, name)
		if f == nil {
			t.Fatalf("%s 没排出来", name)
		}
		at, _ := findPad(f)
		if at < 0 {
			t.Fatalf("%s 没挂帧内清理记录（登记过东西的帧都要挂）", name)
		}
		last := f.Body[len(f.Body)-1]
		c, ok := last.(*ir.Call)
		if !ok || c.Fn != "@throw_walk" {
			t.Fatalf("%s 清理段的收尾不是叫 @throw_walk：%s", name, last)
		}
		n := 0
		for _, in := range f.Body[at+1:] {
			if x, ok := in.(*ir.Call); ok && x.Fn == "nef.destroy" {
				n++
			}
		}
		if n == 0 {
			t.Fatalf("%s 清理段里一笔都没还", name)
		}
	}
	// 未登记过句柄的帧不发射标记：deep 只有一个 throw，帧内没有登记任何块
	if f := findFn(p, "deep"); f != nil {
		if at, _ := findPad(f); at >= 0 {
			t.Fatalf("deep 没登记过任何东西，不该挂帧内清理记录")
		}
	}
	// 每一处 Ret 之前那条标记：middle 与 top 各有一处 return
	for _, name := range []string{"middle", "top"} {
		f := findFn(p, name)
		for i, in := range f.Body {
			if _, isRet := in.(*ir.Ret); !isRet {
				continue
			}
			if i == 0 {
				t.Fatalf("%s 第一帧就是 Ret？", name)
			}
			// 清理段排在帧的最后，Ret 之前那一条必须是摘标记（PopHandler）
			if _, ok := f.Body[i-1].(*ir.PopHandler); !ok {
				t.Fatalf("%s 第 %d 条是 Ret，前一条不是摘标记：%s", name, i, f.Body[i-1])
			}
		}
	}
}

// TestAbandonRecordLivesInFrame 固定那条记录**分配在帧槽上**（而不是堆上）：
// 挂标记的指令之前不发生任何堆分配，且交给标记的 Rec 是五个连续槽位中编号最大的一个——
// 运行期从它向上遍历五格（+0…+32），若交出第一个槽位，+32 会越出这段帧槽，
// 帧内清理记录读到的就是其他用途的槽位。
func TestAbandonRecordLivesInFrame(t *testing.T) {
	p := progOf(t, crossSrc)
	f := findFn(p, "middle")
	if f == nil {
		t.Fatal("middle 没排出来")
	}
	if len(f.Body) < 2 {
		t.Fatal("middle 的帧是空的")
	}
	pa, ok := f.Body[1].(*ir.PushAbandon)
	if !ok {
		t.Fatalf("抹完零第二条该是挂标记，实际 %s", f.Body[1])
	}
	if int(pa.Rec)+1 != f.Slots {
		t.Fatalf("那五格该排在帧的最后（Rec=%d，帧槽 %d 格）", pa.Rec, f.Slots)
	}
	for _, in := range f.Body[:2] {
		if c, ok := in.(*ir.Call); ok && c.Fn == "@alloc" {
			t.Fatalf("挂标记之前不该向堆要东西：%s", in)
		}
	}
}

// TestAbandonPadReleasesEveryFrameOnce 用堆内存占用量验证同一件事：三帧各登记一个句柄，抛出跨过
// 中间那一帧——释放干净后，两万圈跑完堆用量应停留在最初水平。
func TestAbandonPadReleasesEveryFrameOnce(t *testing.T) {
	const src = `
method deep(n:int) {
    throw (130, "从最里头抛")
}
method middle(n:int):int {
    let mid:list<int> = [n, n]
    let s:string = "甲" + "乙"
    deep(n)
    return length(mid)
}
method top(n:int):int {
    let held:list<int> = [1, 2, 3]
    try {
        let inner:dict<string, int> = {"甲": n}
        let walked:int = middle(n)
        return walked + length(inner) + length(held)
    } catch (code, msg) {
        return 0 - code + length(held)
    }
}
let total:int = 0
for (i:int in 0 until 20000) {
    total = total + top(i)
}
console.print(total)
`
	out, used := runHeap(t, src)
	// 每一圈：0 - 130 + length(held) = -127，两万圈
	if strings.TrimSpace(strings.Split(out, "\n")[0]) != "-2540000" {
		t.Fatalf("数不对：%q", out)
	}
	if used > 1<<20 {
		t.Fatalf("每圈抛一次、跨两帧接住，堆用量不该超过最初那一块：%d 字节", used)
	}
}

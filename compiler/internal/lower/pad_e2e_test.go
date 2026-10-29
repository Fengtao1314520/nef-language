// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// pad_e2e_test.go 覆盖早退路径上的**退出清理代码**：抛出异常与正常走完的释放行为一致，已建好的值照常释放。
//
// 两头都要固定：一头是"别漏"——漏掉就是每圈整块增长，那一侧由 heap_e2e_test.go 的每圈增长表覆盖；
// 另一头是**别提前释放**——接住块、调用方手里那几个名字还要读，提前释放等于析构仍在使用的内存。
// 下面几条测试覆盖的是第二头。

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ir"
)

// padBefore 统计"紧贴在第一个匹配 predicate 的指令之前"那一段退出清理代码的笔数：
// 从该处往回走，只认"nef.destroy ＋ 紧跟的抹零"成对出现，遇到其他指令即停。
// 只统计紧邻那一段，是为了不把同一函数其他位置的清理（读字段前的判空也有一段）计入。
func padBefore(fn *ir.Func, predicate func(in ir.Inst) bool) int {
	at := -1
	for i, in := range fn.Body {
		if predicate(in) {
			at = i
			break
		}
	}
	if at < 0 {
		return -1
	}
	n := 0
	for i := at - 1; i >= 0; i-- {
		if mv, ok := fn.Body[i].(*ir.Mov); ok && mv.S == ir.K(0) {
			continue
		}
		if c, ok := fn.Body[i].(*ir.Call); ok && c.Fn == "nef.destroy" {
			n++
			continue
		}
		break
	}
	return n
}

func isThrow(in ir.Inst) bool {
	c, ok := in.(*ir.Call)
	return ok && c.Fn == "@throw"
}

func isDie(in ir.Inst) bool {
	c, ok := in.(*ir.Call)
	return ok && c.Fn == "@die"
}

func isReleaseTop(in ir.Inst) bool {
	c, ok := in.(*ir.Call)
	return ok && c.Fn == releaseName
}

func isRet(in ir.Inst) bool {
	_, ok := in.(*ir.Ret)
	return ok
}

const releaseName = "nef.release_top"

// TestThrowPadStopsAtTryBody 验证抛出只释放到"接住它的 try 体"这一层：
// try 体外面的 `keep` 接住块接着要读，一笔都不许释放。
func TestThrowPadStopsAtTryBody(t *testing.T) {
	p := progOf(t, `
method f():int {
    let keep:list<int> = [1, 2, 3]
    try {
        let inner:list<int> = [9]
        throw (120, "里头抛")
    } catch (code, msg) {
        console.print(code, msg, length(keep))
    }
    return length(keep)
}
console.print(f())
`)
	got, code := run(t, `
method f():int {
    let keep:list<int> = [1, 2, 3]
    try {
        let inner:list<int> = [9]
        throw (120, "里头抛")
    } catch (code, msg) {
        console.print(code, msg, length(keep))
    }
    return length(keep)
}
console.print(f())
`)
	if want := "120 里头抛 3\n3\n"; got != want || code != 0 {
		t.Fatalf("接住块读不到 try 外面那一个了：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 中间码一侧：抛出之前只应释放一笔（inner），把整帧都释放的话就是两笔
	fn := findFn(p, "f")
	if n := padBefore(fn, isThrow); n != 1 {
		t.Fatalf("throw 之前的退出清理代码该只还 try 体那一层（一笔），实际 %d 笔", n)
	}
}

// TestThrowPadReleasesThrowingFrame 验证抛出把**本帧**登记的全部释放：
// 被废弃的那一帧无人再能读取，留着就是每圈整块增长（每圈增长那条测试核对同一件事）。
// 接住块里再次使用本帧外面的名字，也在这条测试里一起核对。
func TestThrowPadReleasesThrowingFrame(t *testing.T) {
	got, code := run(t, `
method boom(n:int) {
    let xs:list<int> = [n]
    let d:dict<string, int> = {"甲": n}
    throw (121, "炸了")
}
let held:list<int> = [7]
let total:int = 0
for (i:int in 0 until 5) {
    try {
        boom(i)
    } catch (code, msg) {
        total = total + code + length(held)
    }
}
console.print(total, held[0])
`)
	// 五次各加 121 与 length(held)=1，再加 held[0]=7：接住块读的仍是本帧外面那一个
	if want := "610 7\n"; got != want || code != 0 {
		t.Fatalf("抛出这一路与接住块不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestReturnHandleKeepsSource 验证 `return` 交出字段里的句柄时**不许**释放源头那一块：
// 接住返回值的一侧正持有那块内存，提前释放就是错误析构使用中的对象。
// 漏掉这条约束就会每圈 104 字节（heap_e2e_test.go 里那一条故意标着"该漏"的对照）。
func TestReturnHandleKeepsSource(t *testing.T) {
	src := `
class Holder {
    peer:list<int>
}
method hand(n:int):list<int> {
    let h:Holder = new Holder(peer: [n])
    return h.peer
}
let got:list<int> = hand(5)
console.print(length(got), got[0])
`
	got, code := run(t, src)
	if want := "1 5\n"; got != want || code != 0 {
		t.Fatalf("交出去的句柄被退出清理代码拆掉了：\n got %q 码 %d\nwant %q", got, code, want)
	}
	fn := findFn(progOf(t, src), "hand")
	if n := padBefore(fn, isRet); n != 0 {
		t.Fatalf("return 交的是读出来的句柄，Ret 之前一笔都不许还，实际 %d 笔", n)
	}
}

// TestExitReleasesFrameAndTop 验证 `exit`：整帧清理，并且顶层那一段释放也被调用。
func TestExitReleasesFrameAndTop(t *testing.T) {
	src := `
class Bag {
    n:int
}
let top:Bag = new Bag(n: 1)
method leave() {
    let xs:list<int> = [1, 2]
    exit (100, "走人")
}
leave()
console.print("不该到这里")
`
	got, code := run(t, src)
	if code != 100 {
		t.Fatalf("exit 的退出码是 %d，要 100：%q", code, got)
	}
	if strings.Contains(got, "不该到这里") {
		t.Fatalf("exit 之后还跑到了下一行：%q", got)
	}
	fn := findFn(progOf(t, src), "leave")
	if n := padBefore(fn, isReleaseTop); n != 1 {
		t.Fatalf("exit 之前该把本帧那一笔还掉，实际 %d 笔", n)
	}
	// 顺序也得固定：先还本帧、再叫顶层那一段、最后才 @die
	rk, dk := -1, -1
	for i, in := range fn.Body {
		if isReleaseTop(in) && rk < 0 {
			rk = i
		}
		if isDie(in) {
			dk = i
			break
		}
	}
	if !(rk >= 0 && dk > rk) {
		t.Fatalf("exit 那一处的次序不是「本帧→顶层→@die」：nef.release_top 在第 %d 条，@die 在第 %d 条", rk, dk)
	}
}

// TestEarlyExitOutOfTryPopsHandler 验证跳出 try 体时处理器记录被弹出。
//
// `try { return 1 }` 这条路不经过接住块，也就走不到 PopHandler——链头上会遗留一条指向
// **已经废弃的帧**的记录。漏掉的不只是那 32 字节：之后任何无人接住的抛出都会先命中这条记录，
// 跳进那个死帧的接住块，程序不报错、不打印、以退出码 0 结束。修复与退出清理代码同一类做法：
// 早退路径先把该摘的记录摘掉，从内到外。
func TestEarlyExitOutOfTryPopsHandler(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"return 跳出去", `
method leaveByReturn():int {
    try {
        return 1
    } catch (c, m) {
        return 2
    }
}
let x:int = leaveByReturn()
console.print(x)
throw (120, "该由程序终止接手")
`, "1\n出错了（120）：该由程序终止接手\n"},
		{"break 跳出去", `
method leaveByBreak():int {
    for (i:int in 0 until 1) {
        try {
            break
        } catch (c, m) {
            return 2
        }
    }
    return 5
}
let x:int = leaveByBreak()
console.print(x)
throw (121, "也一样")
`, "5\n出错了（121）：也一样\n"},
	}
	for _, one := range cases {
		got, code := run(t, one.src)
		if got != one.want || code != 1 {
			t.Errorf("%s：得到 %q 码 %d，要 %q 码 1", one.name, got, code, one.want)
		}
	}
}

// TestNestedTryEarlyExit 反向验证：摘记录不能把**应当接住本次抛出的那一条**也摘掉。
// 里层 try 抛、里层接住；外层那两颗留给后面真正的抛出；里层接住块走完属于正常路径，
// PopHandler 由 tryStmt 自己发出。
func TestNestedTryEarlyExit(t *testing.T) {
	got, code := run(t, `
method nested():int {
    try {
        try {
            throw (120, "里层抛")
        } catch (c1, m1) {
            console.print(c1, m1)
        }
        return 1
    } catch (c2, m2) {
        return 2
    }
}
console.print(nested())
`)
	if want := "120 里层抛\n1\n"; got != want || code != 0 {
		t.Fatalf("嵌套 try 的早退路不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

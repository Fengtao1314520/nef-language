// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// handpath_e2e_test.go 钉住"一条 try 体有好几条出口"这件事：每条出口各欠一条 PopHandler，
// 一条路上摘过不等于另一条路上也摘过。
//
// 缺陷的样子（issue #10）：`try` 体里第一处 `return` 摘了处理器，账上却记成"这一层已经摘了"，
// 于是同一层 try 的其余出口——第二处 `return`、体正常走完——都不再发 `PopHandler`。链上因此
// 留下一条指向刚返回那一帧的记录，帧尾那一次摘链又把这条残留当成自己的清理记录摘掉并归还，
// 这一帧真正的清理记录反而留在链上。下一个来摘链的人读 `[作废帧+24]` 取"上一条"，那就是野指针：
// Windows 上表现为 `0xC0000005`，没有任何 Nef 层报错，程序跑到一半就没了。
//
// 为什么用数指令来钉：链是在运行期连起来的，那个位置什么时候被读到，取决于那一刻栈上
// 留着什么。同一个形状跑一遍往往看不出来（清理段会把用过的槽位抹成 0，第二次走到是空操作），
// 所以行为测试只能钉"形状走通"，真正确定的观测点在发出来的指令上。

package lower_test

import (
	"strings"
	"testing"
)

// countInFunc 数一个函数体里某条指令发了几次（`what` 取 "pop handler" 这类写法；
// `push handler → L0` 这种带落点的按前缀算）。
func countInFunc(t *testing.T, dump, fn, what string) int {
	t.Helper()
	body, ok := funcBody(dump, fn)
	if !ok {
		t.Fatalf("转储里找不到函数 %s", fn)
	}
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), what) {
			n++
		}
	}
	return n
}

// funcBody 切出 `func 名字 (slots=…)` 到下一个 `func ` 之间的那一段。
func funcBody(dump, fn string) (string, bool) {
	lines := strings.Split(dump, "\n")
	head := "func " + fn + " ("
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, head) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "func ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), true
}

// TestTryBodyOwesOnePopPerExit 数 try 体三条出口各欠的那一条 pop。
//
// 这一个方法有四处 `ret`：体里两处 `return`、接住块里一处 `return`、以及 try 走完之后那一处。
// 四处各欠一条帧尾的 pop（这一帧挂了一条帧内清理记录）。此外 try 体的三条出口——
// 两处 `return` 和"体正常走完"——各再欠一条：
//
//	4（帧尾） + 3（体那三条出口） = 7
//
// 接住块那一处 `return` 不再欠：抛出走到接住块时，运行期走路那一步已经把这一层摘了。
//
// 修之前这里只有 5 条：第二处 `return` 与"走完"这两条路一条 pop 都没有，链上留一条指向
// 已作废帧的记录（也就是 issue #10 崩溃的来路）。
//
// en: One pop is owed per exit of a try body. This method has four `ret` sites: two `return`s in
// the body, one in the catch block, and one after the try; each owes the frame-tail pop (this
// frame hangs one frame-scope cleanup record). The body's three exits - two `return`s and
// falling off the end - owe one more each: 4 + 3 = 7. The catch block's `return` owes none,
// because the runtime's walk already unlinked that layer. Before the fix there were only five.
func TestTryBodyOwesOnePopPerExit(t *testing.T) {
	src := `
class R {
    hits:int

    method pick(n:int):boolean {
        try {
            if (n > 0) {
                return true
            }
            return false
        } catch (code, msg, op) {
            return false
        }
    }
}
`
	dump := dumpIR(t, src)
	if got, want := countInFunc(t, dump, "R.pick", "pop handler"), 7; got != want {
		t.Fatalf("R.pick 里 pop handler 发了 %d 条，要 %d 条：\n%s", got, want, mustBody(t, dump, "R.pick"))
	}
	if got, want := countInFunc(t, dump, "R.pick", "push handler"), 1; got != want {
		t.Fatalf("R.pick 里 push handler 发了 %d 条，要 %d 条", got, want)
	}
	if got, want := countInFunc(t, dump, "R.pick", "push abandon"), 1; got != want {
		t.Fatalf("R.pick 里 push abandon 发了 %d 条，要 %d 条", got, want)
	}
}

// TestTryBodyPopsSurviveLaterBranches 钉"分支编完要把账恢复回去"这一条：
// 第一条分支里的 `return` 摘过一层，不影响同层 try 的其余出口，也不影响后面分支里那处 `return`。
//
// 三段各一处 `return`，加上接住块那一处、try 之后那一处、以及帧尾那一处：六条帧尾 pop，
// 加 try 体那四条出口（三处 `return` 与走完）各一条 = 10。
//
// en: Pins the restore-after-each-branch rule: a `return` in the first branch does not cancel the
// other exits of the same try, nor the later branch's `return`. Three returns plus the frame's
// tail ret give six frame pops, and the try body's four exits give four more: 10.
func TestTryBodyPopsSurviveLaterBranches(t *testing.T) {
	src := `
class R {
    kind:int

    method pick(n:int):boolean {
        try {
            if (kind == 0) {
                return n > 0
            }
            if (kind == 1) {
                return n > 1
            }
            if (kind == 2) {
                return n > 2
            }
        } catch (code, msg, op) {
            return false
        }
        return true
    }
}
`
	dump := dumpIR(t, src)
	if got, want := countInFunc(t, dump, "R.pick", "pop handler"), 10; got != want {
		t.Fatalf("R.pick 里 pop handler 发了 %d 条，要 %d 条：\n%s", got, want, mustBody(t, dump, "R.pick"))
	}
}

// TestTryReturnsInLoopKeepRunning 是形状那一侧的行为钉子：class 里的方法、try 体里几处 `return`、
// 在循环里反复调用，之后的一次抛出还能被正常接住。
//
// 这条在修之前也是绿的——坏链什么时候被读到取决于栈上留着什么，所以它守不住这个缺陷；
// 守住的是上面那两条数指令的。留着它是因为它跑的是用户真正写的形状。
//
// en: The behaviour-side pin: a method on a class, several `return`s inside a try body, called in a
// loop, with a later throw still caught properly. This one passes before the fix too - when the
// broken chain gets read depends on what the stack holds - so the instruction-count tests above
// are the real guard. It stays because it runs the shape people actually write.
func TestTryReturnsInLoopKeepRunning(t *testing.T) {
	got, code := run(t, `
struct P {
    a:boolean
    b:int
    c:boolean
    d:string
}

class Runner {
    hits:int

    method pick(n:int):boolean {
        let p:P = P { a: true, b: n, c: false, d: "v" + number.to_string(n) }
        try {
            if (n % 2 == 0) {
                hits = hits + 1
                return true
            }
            if (n % 3 == 0) {
                return false
            }
            hits = hits + 1
            return p.b > 0
        } catch (code, msg, op) {
            return false
        }
    }

    method run(total:int) {
        for (i:int from 0 until total) {
            this.pick(i)
        }
    }
}

let r:Runner = new Runner(hits: 0)
r.run(300)
let caught:int = 0
try {
    throw (9001, "故意的")
} catch (code, msg, op) {
    caught = code
}
console.print("hits", r.hits, "caught", caught)
`)
	want := "hits 250 caught 9001\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q 码 0", got, code, want)
	}
}

func mustBody(t *testing.T, dump, fn string) string {
	t.Helper()
	body, ok := funcBody(dump, fn)
	if !ok {
		return "（找不到 " + fn + "）"
	}
	return body
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// finally_e2e_test.go 钉 `finally` 那一段：四条出口每一条都要先跑完它，而且只跑一次。
//
// 四条出口是走完、`return`、抛出（有人接与没人接两种）、跳出循环（`break` 与 `continue`）。
// 抛出那一路还分三种：本层接住、本层没写 catch、接住块里又抛出一条新的。

package lower_test

import (
	"testing"
)

func TestFinallyOnEveryExit(t *testing.T) {
	got, code := run(t, `
method clean_return(n:int):int {
    try {
        console.print("体里", n)
        return n * 2
    } finally {
        console.print("收尾", n)
    }
}

console.print(clean_return(3))

try {
    console.print("走完")
} finally {
    console.print("收尾一")
}

try {
    console.print("抛出一条")
    throw (120, "故意的")
} catch (c, m, at) {
    console.print("接住:", c, m)
} finally {
    console.print("收尾二")
}

try {
    try {
        console.print("里面接住再抛出")
        throw (121, "里面那条")
    } catch (c2, m2, at2) {
        console.print("里面接住:", c2, m2)
        throw (122, "外面那条")
    } finally {
        console.print("收尾三")
    }
} catch (c3, m3, at3) {
    console.print("外面接住:", c3, m3)
}

console.print("都跑完了")
`)
	want := "体里 3\n收尾 3\n6\n走完\n收尾一\n抛出一条\n接住: 120 故意的\n收尾二\n" +
		"里面接住再抛出\n里面接住: 121 里面那条\n收尾三\n外面接住: 122 外面那条\n都跑完了\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q 码 0", got, code, want)
	}
}

func TestFinallyRunsOncePerLoopExit(t *testing.T) {
	got, code := run(t, `
let hits:int = 0
for (i:int in 0 until 4) {
    try {
        if (i == 2) {
            break
        }
        if (i == 3) {
            continue
        }
        console.print("跑", i)
    } finally {
        hits = hits + 1
        console.print("收尾", i)
    }
}
console.print("一共", hits)
`)
	// 第 0、1 轮走完，第 2 轮 break 之前跑一次，第 3 轮进不到（break 已经出圈）
	want := "跑 0\n收尾 0\n跑 1\n收尾 1\n收尾 2\n一共 3\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

func TestFinallyWithoutCatchKeepsPropagating(t *testing.T) {
	got, code := run(t, `
try {
    throw (120, "没人接")
} catch (c, m, at) {
    console.print("接住:", c, m)
} finally {
    console.print("接住这一段也要收尾")
}

try {
    console.print("这一层没有 catch")
    throw (121, "往外走的那条")
} finally {
    console.print("没 catch 也要收尾")
}
`)
	// 第一个 try 的抛出有人接住：接住那一段跑完，收尾跟着跑；第二个没有 catch，收尾跑完把那一条交出去
	want := "接住: 120 没人接\n接住这一段也要收尾\n这一层没有 catch\n没 catch 也要收尾\n出错了（121）：往外走的那条（在 t.nf 12:5）\n"
	if got != want || code != 1 {
		t.Fatalf("得到 %q 码 %d，要 %q 码 1", got, code, want)
	}
}

func TestFinallyNestedOrder(t *testing.T) {
	got, code := run(t, `
try {
    try {
        console.print("最里")
        throw (120, "从最里抛出")
    } finally {
        console.print("里层收尾")
    }
} catch (c, m, at) {
    console.print("外层接住:", c, m)
} finally {
    console.print("外层收尾")
}
`)
	want := "最里\n里层收尾\n外层接住: 120 从最里抛出\n外层收尾\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// 收尾那一段里的值要按同一条规矩归还：一圈一圈跑下来不多留一块。
func TestFinallyKeepsHeapFlat(t *testing.T) {
	const tmpl = `
let total:int = 0
for (i:int in 0 until @N@) {
    try {
        let acc:list<int> = [i]
        total = total + acc[0]
    } finally {
        let tag:list<string> = ["甲"]
        total = total + length(tag)
    }
}
console.print(total)
`
	per := heapSlope(t, tmpl)
	if per > 2 {
		t.Fatalf("每圈多要 %v 字节，收尾那一段把值漏下了\n%s", per, tmpl)
	}
}

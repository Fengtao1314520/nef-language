// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// throwpos_e2e_test.go 覆盖抛出带的源码位置：接住块的第三个名字读到的就是这一串，
// 没人接住时那句"出错了（码）：话"末尾跟着「（在 文件 行:列）」。
// 钉住的三条：位置指到抛出那一步而不是接住那一步、语言自己抛的那几处与手写 throw 走同一格式、
// 接住之后程序照旧往下走。栈用完那一条（10093 号）不带位置，钉在 `cmd/nf`；
// 跨文件的那一条也钉在 `cmd/nf`（那一份用真文件与真包）。
//
// en: throwpos_e2e_test.go covers the source position a throw carries: the catch block's third
// name reads exactly this text, and a throw nobody catches prints 「（在 file line:column）」 after
// the "出错了（码）：话" line. Three things are pinned: the position names the throwing step rather
// than the handler; the places the language raises and a hand-written throw share one format; and
// the program still runs on after a catch. The stack-exhausted line (10093) carries no position
// and is pinned under cmd/nf, as is the cross-file case, which needs real files and a real
// package.
package lower_test

import (
	"strings"
	"testing"
)

// TestCaughtPositionNamesTheThrowingStep 里抛出在第三行，接住在第五、六行；
// 接住块读到的位置必须是第三行那一步。
func TestCaughtPositionNamesTheThrowingStep(t *testing.T) {
	got, code := run(t, `
let d:dict<string, int> = {"甲": 1}
try {
    console.print(d["乙"])
} catch (code, msg, at) {
    console.print(code, msg, at)
}
console.print("接住了还往下走")
`)
	want := "10032 没有这个键 t.nf 4:20\n接住了还往下走\n"
	if got != want || code != 0 {
		t.Fatalf("接住块读到的位置不对：got %q 码 %d，要 %q", got, code, want)
	}
}

// TestHandWrittenThrowPosition 手写的那一次抛出与语言自己抛的那六处走同一格式：
// 接住时第三个名字是位置，没人接时那一句末尾带上「（在 …）」。
func TestHandWrittenThrowPosition(t *testing.T) {
	got, code := run(t, `
method deep():int {
    throw (231, "自己抛的")
}

try {
    console.print(deep())
} catch (code, msg, at) {
    console.print(code, msg, at)
}
`)
	want := "231 自己抛的 t.nf 3:5\n"
	if got != want || code != 0 {
		t.Fatalf("手写抛出的位置不对：got %q 码 %d，要 %q", got, code, want)
	}

	uncaught, code := run(t, `
let xs:list<int> = [1]
console.print(length(xs))
xs[9] = 2
`)
	if code != 1 || !strings.HasSuffix(uncaught, "出错了（10031）：下标越界（在 t.nf 4:3）\n") {
		t.Fatalf("没人接住那一句末尾没带上位置：%q 码 %d", uncaught, code)
	}
}

// TestPositionTravelsAcrossFiles 在 `cmd/nf` 里钉（那一份用真文件与真包，位置要指到包那一份文件里去）。

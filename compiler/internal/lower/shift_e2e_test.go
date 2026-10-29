// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestShiftAmountCheckedAtRun 固定编译期之外的运行期一半：移位量是算出来的，宽度只有跑起来才知道，
// 超出左式能移的位数就抛 10021 号。修复之前这条路径没有任何检查，CPU 把移位量的高位掩掉，
// `1 << 70` 返回 64、`1 >> (-1)` 返回最小值，表面上都像正确结果。
func TestShiftAmountCheckedAtRun(t *testing.T) {
	got, code := run(t, `
let s:byte = 3
let n:int = 1024
console.print(n << s, n >> s)
let m:byte = 63
console.print(1 << m, -16 >> m)
`)
	want := "8192 128\n-9223372036854775808 -1\n"
	if got != want || code != 0 {
		t.Fatalf("过得去的移位量跑不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	cases := []struct {
		name string
		src  string
	}{
		{"左式 int，量 64", "let s:byte = 64\nconsole.print(1 << s)\n"},
		{"左式 int，量 200", "let s:byte = 200\nconsole.print(1 >> s)\n"},
		{"左式 byte，量 8", "let b:byte = 3\nlet s:byte = 8\nconsole.print(b << s)\n"},
		{"量是算出来的", "let m:int = 40 + 30\nlet s:byte = m as byte\nconsole.print(1 << s)\n"},
	}
	for _, c := range cases {
		out, oc := run(t, c.src)
		if oc != 1 || !strings.Contains(out, "出错了（10021）：整数运算溢出") {
			t.Fatalf("%s：该抛 10021 号，得到 %q 码 %d", c.name, out, oc)
		}
	}
}

// TestShiftAmountMustBeByte 覆盖编译期那一半：`int` 的移位量通不过类型检查，字面量例外。
func TestShiftAmountMustBeByte(t *testing.T) {
	if err := compileErr("let s:int = 70\nconsole.print(1 << s)\n"); err == nil ||
		!strings.Contains(err.Error(), "移位量只能写 byte") {
		t.Fatalf("int 的移位量该被拦：err=%v", err)
	}
	got, code := run(t, "let s:byte = 4\nconsole.print(1 << s, 1 << 4)\n")
	if code != 0 || strings.TrimSpace(got) != "16 16" {
		t.Fatalf("byte 的名字与字面量都该照过：got %q 码 %d", got, code)
	}
}

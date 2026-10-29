// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// rpn_e2e_test.go 把 samples/rpn.nf 编成真实产物运行一遍。
//
// 这份样例最早暴露了两处此前未被测试覆盖的缺陷：
// `list`.remove(值)` 的相等模式在运行期判反了方向（测试在 listremove_e2e_test.go），
// 以及 dict 长出第二块桶数组、键为计算所得文本时段错误（根因在 @alloc 的摘链，
// 已修复，测试在 dictgrow_e2e_test.go；samples/wordfreq.nf 是同一条路径的样例）。

package lower_test

import (
	"os"
	"testing"

	"neflang.org/nf/internal/exetest"
)

func TestSampleRpn(t *testing.T) {
	const path = "../../../samples/rpn.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, string(raw)), "rpn.exe")
	want := "9\n4\n算不动: 10022 整数除以 0\n算不动: 121 式子没算完，栈里还剩 3\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("rpn.nf跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

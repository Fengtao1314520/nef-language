// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// stack_e2e_test.go 覆盖"调用嵌套过深"的行为：栈用完的退出码与对应提示。
//
// 这条检查的作用：它在每帧开头加了一段不到十个字节的比较，正常程序感知不到差别；
// 只有把栈真正用完才能看到它是否生效。生效时的行为是"打印一条提示再退出码 1"，
// 而不是让进程被操作系统崩溃终止——那正是加这道检查之前唯一会发生的事。
//
// 两份源文件放在 `testdata/stack/`，与 `cmd/nf` 共用：同一份源码在直跑与产物两条路径上
// 必须输出同一条提示，分开编写就无法保证一致（两条路径的并排比较在 cmd/nf 的测试里）。
package lower_test

import (
	"os"
	"path/filepath"
	"testing"
)

const stackLine = "出错了（10093）：调用套得太深，栈用完了\n"

func stackFixture(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "stack", name))
	if err != nil {
		t.Fatalf("读不到 %s：%v", name, err)
	}
	return string(src)
}

// TestStackGuardPrintsOneLine 实际运行产物：栈用完之后打印的就是那条提示，退出码 1。
func TestStackGuardPrintsOneLine(t *testing.T) {
	got, code := run(t, stackFixture(t, "deep.nf"))
	if got != stackLine || code != 1 {
		t.Fatalf("栈用完没按预期收尾：\n got %q 码 %d\nwant %q 码 1", got, code, stackLine)
	}
}

// TestStackGuardKeepsLegalDepth 验证"未到上限就不报错"：5000 层递归正常算完、正常退出码 0。
func TestStackGuardKeepsLegalDepth(t *testing.T) {
	got, code := run(t, stackFixture(t, "legal.nf"))
	if got != "10000\n" || code != 0 {
		t.Fatalf("5 千层递归被拦了：得到 %q 码 %d，要 \"10000\\n\" 码 0", got, code)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"testing"
)

// TestUnsetEnvironmentVariable 覆盖这条规则的两头：删成之后 `get` 抛 10061 号，
// 删一个本来就不存在的什么都不做、也不抛（与三个 remove 找不到目标时的形状一致）。
func TestUnsetEnvironmentVariable(t *testing.T) {
	got, code := run(t, `
environment.set("NF_UNSET_DEMO", "1")
console.print(environment.get("NF_UNSET_DEMO"))
environment.unset("NF_UNSET_DEMO")
try {
    console.print(environment.get("NF_UNSET_DEMO"))
} catch (c, m) {
    console.print(c)
}
environment.unset("NF_UNSET_DEMO")
console.print("删第二次也没事")
`)
	want := "1\n10061\n删第二次也没事\n"
	if got != want || code != 0 {
		t.Fatalf("删环境变量不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestUnsetBadNameThrows 名字坏到系统调用不接受（`=` 是环境变量名的分隔符）时，走的仍是
// environment 这一组共同的兜底 10060 号——同一组函数对坏名字的处理结果必须一致。
func TestUnsetBadNameThrows(t *testing.T) {
	got, code := run(t, `
try {
    environment.unset("NF=BAD")
    console.print("没抛")
} catch (c, m) {
    console.print(c, m)
}
`)
	want := "10060 环境这一处没办成\n"
	if got != want || code != 0 {
		t.Fatalf("坏名字该抛 10060：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestUnsetBorrowsItsName 名字按只读借用算：`unset` 不收留传进去的那一段文本，
// 所以每一圈拿同一个名字去删，多跑一圈不多要一个字节。
func TestUnsetBorrowsItsName(t *testing.T) {
	per := heapSlope(t, `
for (i:int in 0 until @N@) {
    environment.unset("NF_UNSET0")
}
console.print("跑完了")
`)
	if per > 2 {
		t.Fatalf("删环境变量每圈还留 %v 字节，参数那一头不该收留名字", per)
	}
}

// TestUnsetKeepsTheMidExpressionLeak 名字是当场拼出来的那一种（`"NF_UNSET" + number.to_string(...)`），
// 每圈留 32 字节。这一处泄漏不在 `unset` 的责任范围：同一段式子换成 `console.print(...)` 一字不差也是 32
// （两万圈各 640,176 字节）。它属于"式子中间那一步读走的值"那一族，要先定下"式子里各步读走的
// 分别归谁、按什么顺序析构"。这里固定量级：那一族收掉的时候这条测试会失败，届时同步更新。
func TestUnsetKeepsTheMidExpressionLeak(t *testing.T) {
	per := heapSlope(t, `
for (i:int in 0 until @N@) {
    environment.unset("NF_UNSET" + number.to_string(i % 2))
}
console.print("跑完了")
`)
	if per < 28 {
		t.Fatalf("式子中间那一步这一族改成能拆了（每圈 %v 字节），要把这条测试与对应文档一起更新", per)
	}
}

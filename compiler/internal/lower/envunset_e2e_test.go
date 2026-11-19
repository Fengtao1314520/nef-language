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
} catch (c, m, at) {
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
// environment 这一组共同的默认错误码 10060 号——同一组函数对坏名字的处理结果必须一致。
func TestUnsetBadNameThrows(t *testing.T) {
	got, code := run(t, `
try {
    environment.unset("NF=BAD")
    console.print("没抛")
} catch (c, m, at) {
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
for (i:int from 0 until @N@) {
    environment.unset("NF_UNSET0")
}
console.print("跑完了")
`)
	if per > 2 {
		t.Fatalf("删环境变量每圈还留 %v 字节，参数那一头不该收留名字", per)
	}
}

// TestUnsetFreesTheAssembledName 名字是当场拼出来的那一种（`"NF_UNSET" + number.to_string(...)`）：
// 运算符两个操作数只读一次、谁都不收留，所以右边那一段"这一轮才拼出来的"到这一层尽头就没有人
// 认得了，当场归还。每轮不多要一个字节。
//
// 这一条原先固定的是"每轮 32 字节"——运算符那两个操作数落在"式子中间那一步"那一族之外
// （登记只在标准库那几条只读借用的参数窗口上做），拼出来的那一段没人还。收掉之后改为要求 0；
// 同一族剩下的部分（`xs[0][0]` 那一种读走）仍由 midread／heap 那两组钉子管着。
//
// en: The name is assembled on the spot (`"NF_UNSET" + number.to_string(...)`). A binary operator
// reads both operands once and keeps neither, so the freshly assembled right-hand side is unknown
// to anyone at the end of that layer and is returned there - no extra byte per round. This test
// used to pin 32 bytes a round, the state before fresh operands were registered at the operator's
// own layer.
func TestUnsetFreesTheAssembledName(t *testing.T) {
	per := heapSlope(t, `
for (i:int from 0 until @N@) {
    environment.unset("NF_UNSET" + number.to_string(i % 2))
}
console.print("跑完了")
`)
	if per > 2 {
		t.Fatalf("拼出来的名字每轮还留 %v 字节，运算符那一层该把它还掉", per)
	}
}

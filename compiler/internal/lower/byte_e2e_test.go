// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestByteMathIsInt 固定"byte 参与算术结果是 int"。
// 修改之前两个 byte 相加的类型仍标为 byte，于是 400 能安静地落在一个 byte 位置上；
// 现在这一算交出的是 int，越出 0～255 的数照样能打印，但落不回 byte 的位置。
func TestByteMathIsInt(t *testing.T) {
	got, code := run(t, `
let a:byte = 200
let b:byte = 3
console.print(a + b, a - b, a * b, a / b, a % b)
console.print(a << 1, a >> 1, a & b, a | b, a ^ b, ~a)
let big:int = a + a
console.print(big)
`)
	want := "203 197 600 66 2\n400 100 0 203 203 -201\n400\n"
	if got != want || code != 0 {
		t.Fatalf("byte 算术不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 结果落不回 byte 的位置：这一处必须报类型，不能悄悄收下
	if err := compileErr("let a:byte = 200\nlet c:byte = a + a\nconsole.print(c)\n"); err == nil ||
		!strings.Contains(err.Error(), "这里要的是 byte，给的是 int") {
		t.Fatalf("byte 算术的结果直接落进 byte 该被拦：err=%v", err)
	}

	// 溢出检查跟着结果类型走：此前两个 byte 相除不带任何检查，除数为 0 直接触发 CPU 硬件异常
	zero, zc := run(t, "let a:byte = 5\nlet b:byte = 0\nconsole.print(a / b)\n")
	if zc != 1 || !strings.Contains(zero, "出错了（10022）：整数除以 0") {
		t.Fatalf("byte 除以 0 该报 10022 号：got %q 码 %d", zero, zc)
	}
}

// TestByteGuardOnWrite 固定"往 byte 位置写入运行时才知道的数"的两道检查：
// `as byte` 与 byte 的 `++`／`--`。越界当场抛 10013 号，不越界正常执行。
// 字面量走编译期检查（300 as byte 在检查阶段就报），不在这一条里。
func TestByteGuardOnWrite(t *testing.T) {
	got, code := run(t, `
let m:int = 200
let ok:byte = m as byte
console.print(ok)
let w:byte = 100
w = (w + w) as byte
console.print(w)
let h:byte = 254
h++
console.print(h)
let z:byte = 1
z--
console.print(z)
`)
	want := "200\n200\n255\n0\n"
	if got != want || code != 0 {
		t.Fatalf("过得去的几个数不该抛：\n got %q 码 %d\nwant %q", got, code, want)
	}

	cases := []struct {
		name string
		src  string
	}{
		{"as byte 上界外", "let m:int = 256\nlet b:byte = m as byte\nconsole.print(b)\n"},
		{"as byte 负数", "let m:int = 0 - 5\nlet b:byte = m as byte\nconsole.print(b)\n"},
		{"小数换算越界", "let f:float = 300.5\nlet b:byte = f as byte\nconsole.print(b)\n"},
		{"255 再加一", "let k:byte = 255\nk++\nconsole.print(k)\n"},
		{"0 再减一", "let k:byte = 0\nk--\nconsole.print(k)\n"},
	}
	for _, c := range cases {
		out, oc := run(t, c.src)
		if oc != 1 || !strings.Contains(out, "出错了（10013）：超出了 byte 的值域") {
			t.Fatalf("%s：该抛 10013 号，得到 %q 码 %d", c.name, out, oc)
		}
	}

	// 接得住：抛出来的 10013 号能用 catch 接，接住块里报的是那一个号
	kept, kc := run(t, `
let m:int = 300
try {
    let b:byte = m as byte
    console.print(b)
} catch (code, msg) {
    console.print(code, msg)
}
`)
	if kc != 0 || strings.TrimSpace(kept) != "10013 超出了 byte 的值域" {
		t.Fatalf("10013 号该接得住：got %q 码 %d", kept, kc)
	}
}

// TestByteLiteralStillRefused 覆盖编译期那一半：字面量的值此刻就确定，不必留到运行期验证。
func TestByteLiteralStillRefused(t *testing.T) {
	if err := compileErr("console.print(300 as byte)\n"); err == nil ||
		!strings.Contains(err.Error(), "byte 只能装 0～255，这里是 300") {
		t.Fatalf("300 as byte 该在编译期就报：err=%v", err)
	}
	got, code := run(t, "let b:byte = 200 as byte\nconsole.print(b)\n")
	if code != 0 || strings.TrimSpace(got) != "200" {
		t.Fatalf("200 as byte 该照过：got %q 码 %d", got, code)
	}
}

// TestByteFieldHoldsItsRange 说明 guardByte 那句"byte 位置里的值总是合法"的依据：
// 字段按 1 字节存储，读出来做零扩展；struct 与 class 里的 byte 字段都只会在 0～255 之内。
func TestByteFieldHoldsItsRange(t *testing.T) {
	got, code := run(t, `
struct Box {
    n:byte
}
class Bin {
    n:byte
}
let b:Box = Box { n: 200 }
let c:Bin = new Bin(n: 255)
console.print(b.n, c.n, b.n + b.n)
`)
	want := "200 255 400\n"
	if got != want || code != 0 {
		t.Fatalf("byte 字段读出来不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

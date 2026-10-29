// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestIncdecAndDefaults 覆盖两项特性：`i++`／`i--` 与函数参数的默认值。
//
// `++` 的语义就是 `i = i + 1`，所以溢出抛 10021 号、`byte` 到顶同样抛出；
// 默认值补在**调用点**（被调方一进入参数槽就必须有值），省去的实参按字面量计算后填入。
func TestIncdecAndDefaults(t *testing.T) {
	got, code := run(t, `let i:int = 0
i++
i++
i--
console.print(i)

let k:byte = 254
k++
console.print(k)

let f:float = 1.5
f++
f--
console.print(f)

method line(a:int, d:int = 7):int {
    return a * 10 + d
}

console.print(line(1), line(1, 2))

class Counter {
    n:int

    method step(by:int = 3):int {
        n = n + by
        return n
    }
}

let c:Counter = new Counter()
console.print(c.step(), c.step(10), c.n)
`)
	want := "1\n255\n1.5\n17 12\n3 13 13\n"
	if got != want || code != 0 {
		t.Fatalf("++／-- 与默认值不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 溢出：`i++` 与 `i = i + 1` 同一条规矩，抛 10021 号
	over, overCode := run(t, `let top:int = 9223372036854775807
top++
console.print(top)
`)
	if overCode != 1 || !strings.Contains(over, "出错了（10021）") {
		t.Fatalf("int 到顶再加一该抛 10021 号：got %q 码 %d", over, overCode)
	}
}

// TestDefaultsRefuseWhatTheyCannotDo 固定两处编译期拒绝的行为。
func TestDefaultsRefuseWhatTheyCannotDo(t *testing.T) {
	// const 的名字不能自增减（检查器规则）
	if err := compileErr("const c:int = 1\nc++\nconsole.print(c)\n"); err == nil ||
		!strings.Contains(err.Error(), "不能自增减") {
		t.Fatalf("const 自增减该被拦：err=%v", err)
	}
	// 少给了没有默认值的参数
	if err := compileErr("method f(a:int, b:int):int { return a + b }\nconsole.print(f(1))\n"); err == nil ||
		!strings.Contains(err.Error(), "需要 2 个参数") {
		t.Fatalf("少给实参该被拦：err=%v", err)
	}
	// 默认值可以写成什么：字面量、const 名字、以及这两者的算术允许，
	// 其余在**参数声明处**拒绝——此前是检查放行、到生成代码时才点名，把一条语言规则说成了实现缺失。
	if err := compileErr("let floorValue:int = 3\nmethod f(a:int, b:int = floorValue):int { return a + b }\nconsole.print(f(1))\n"); err == nil ||
		!strings.Contains(err.Error(), "不是 const") {
		t.Fatalf("非 const 的默认值该在声明处被拦：err=%v", err)
	}
	if err := compileErr("method g():int { return 3 }\nmethod f(a:int, b:int = g()):int { return a + b }\nconsole.print(f(1))\n"); err == nil ||
		!strings.Contains(err.Error(), "默认值只准写字面量、const 名字和它们的算术") {
		t.Fatalf("函数调用当默认值该被拦：err=%v", err)
	}
	got, code := run(t, `
const STEP:int = 2
method page(n:int = 10, wide:int = STEP * 4):int { return n + wide }
console.print(page(), page(1), page(1, 1))
`)
	if got != "18 9 2\n" || code != 0 {
		t.Fatalf("const 名字与常量算术的默认值跑不对：got %q 码 %d", got, code)
	}
}

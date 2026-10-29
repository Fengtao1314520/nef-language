// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

// 包交出来的数（`export const`）在包外面按点号读得到。
//
// 先前只做了两样：`greet.make()` 是函数（check 侧 memberPkgFunc）、`greet.Pair` 是类型名
// （resolveType 那一侧的双名分支），`greet.LIMIT` 这一种取一个数没有那条路，报的是
// "这个包里没有对外交出 LIMIT"——名字明明挂着 export。
//
// 值类型这一侧没有归属问题：`const` 只能配值类型（检查在 def.go 的 hoistTopDecls），
// 读出来就是一份数，不存在两个人拿着同一块。`export shared` 那一半定成不交给包外面，
// 要交给外面就写一条函数。

import "testing"

const pkgConstSrc = `export const LIMIT:int = 4096
export const RATIO:float = 1.5
export const ready:boolean = true
export const code_assert_failed:int = 206
const priv:int = 1
export shared counter:int = 0

export method twice(n:int):int {
    return n * 2
}
`

// 正写的几种：直接读、参与算术、当实参、赋给一个名字。
func TestPackageConstIsReadable(t *testing.T) {
	errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

let n:int = greet.LIMIT
console.print(n + 1, greet.RATIO, greet.ready, greet.LIMIT * 2)
console.print(greet.twice(greet.LIMIT))
method later() {
    throw (greet.code_assert_failed, "标题不对")
}
later()
`)
	if len(errs) > 0 {
		t.Fatalf("包交出来的数该读得到，实际报错：%v", msgs(errs))
	}
}

// 读出来的类型跟着走：把 int 那个数当文本用要拒绝。
func TestPackageConstKeepsItsType(t *testing.T) {
	errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

let s:string = greet.LIMIT
console.print(s)
`)
	expectMsg(t, errs, "这里要的是 string，给的是 int；类型之间不自动转换")
}

func TestPackageConstRefused(t *testing.T) {
	t.Run("没挂 export 的数", func(t *testing.T) {
		errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

console.print(greet.priv)
`)
		expectMsg(t, errs, "greet 里的 priv 没有挂 export：包只把交出来的东西给别人用")
	})

	t.Run("当函数叫", func(t *testing.T) {
		errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

console.print(greet.LIMIT())
`)
		expectMsg(t, errs, "LIMIT 是包交出来的数，不能加括号调用")
	})

	t.Run("shared 还读不到", func(t *testing.T) {
		errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

console.print(greet.counter)
`)
		expectMsg(t, errs, "greet 里的 counter 是 export shared：它只在包里面共用，包外面读不到")
	})

	t.Run("往它里面写", func(t *testing.T) {
		errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

greet.LIMIT = 3
console.print(greet.LIMIT)
`)
		expectMsg(t, errs, "greet.LIMIT 是 const，声明之后不可改")
	})

	t.Run("自增减", func(t *testing.T) {
		errs := pkgRun(t, pkgConstSrc, `import greet from "libs/greet"

greet.LIMIT++
console.print(greet.LIMIT)
`)
		expectMsg(t, errs, "greet.LIMIT 是 const，不能自增减")
	})
}

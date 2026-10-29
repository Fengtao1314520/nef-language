// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// default_test.go 钉参数默认值那一段在**哪一份文件的作用域里**算（默认值只准写字面量、
// 顶层 `const` 名字与这两样的算术）。
//
// 这条测试覆盖的是实现曾漏掉的一档：此前这一条只在本文件内好用——写库的人把
// `毫秒:int = expect_ms`（本文件的一个 `const`）交给别的文件调用，检查放行，到降级报一句
// "expect_ms 是 args 或还没排到静态槽的名字"。那是实现漏了一档，不是这种写法不该允许。
package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultUsesCalleeFileConst(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "big.nf", `export const BIG:int = 40`)
	write(t, root, "kit.nf", `import { BIG } from "./big.nf"

const STEP:int = 2

export method page(n:int = 10, wide:int = STEP * 4):int {
    return n + wide
}

export method half(x:int = BIG):int {
    return x / 2
}

export class Wheel {
    rim:int

    method turn(by:int = STEP):int {
        rim = rim + by
        return rim
    }
}

export method new_wheel():Wheel {
    return new Wheel(rim: 0)
}
`)
	write(t, root, "main.nf", `import { page, half, new_wheel } from "./kit.nf"

// 这一侧站着一个同名的顶层 let 与同名 const：都不许顶掉被调方那份文件里的 STEP／BIG
const STEP:int = 1000
let wide:int = 7

console.print(page())
console.print(page(1))
console.print(half())
let w:Wheel = new_wheel()
console.print(w.turn(), w.turn())

// 同一批调用再走一遍，这一回站在自己写的函数里。2026-10-22 实测漏的就是这一处：顶层那一段
// 是最后才降的，那时被调方那份文件的顶层表已经登记好，所以顶层那句过；函数体里的调用排在
// 被调方登记之前，报一句"STEP 是 args 或还没排到静态槽的名字"。
method inside() {
    console.print(page(2))
    console.print(half())
    let second:Wheel = new_wheel()
    console.print(second.turn())
}

inside()
`)
	got := runEntry(t, exe, filepath.Join(root, "main.nf"))
	// 18 = 10 + 2*4；9 = 1 + 2*4；20 = 40/2；turn 两次各加 STEP(=2) → 2 4
	// 函数体里那三行：10 = 2 + 2*4；20 = 40/2；2 = 0 + STEP
	if got != "18\n9\n20\n2 4\n10\n20\n2\n" {
		t.Fatalf("默认值该读被调方那份文件里的 const，实际输出 %q，要 \"18 9 20 2 4 10 20 2\" 七行", got)
	}
}

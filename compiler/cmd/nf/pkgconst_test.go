// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// pkgconst_test.go 钉这条行为：包交出来的 `const` 在包外面按点号读得到，读出来的值两条路
// （内存里直跑与生成产物）跑出来是同一串字。
//
// 这一条查的是"检查认下来之后，后端能不能真读到那一个静态槽"：静态槽按声明节点排，
// 按执行次序，包那一份文件的顶层名字在入口之前排好，所以入口里读它拿的是同一个槽。
// 检查那一侧的规矩（没挂 export、shared、当函数叫、往里写、自增减）在
// `internal/check/packageconst_test.go` 里逐句钉着，这里不重复。
package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const pkgConstFacade = `export const LIMIT:int = 4096
export const ratio:float = 0.5
export const ready:boolean = true
const priv:int = 1

export method twice(n:int):int {
    return n * 2
}
`

func TestReadsPackageConst(t *testing.T) {
	nf := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "greet": "1.0" } }`)
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/greet/greet.nf", pkgConstFacade)
	write(t, root, "main.nf", `import greet from "libs/greet"

let n:int = greet.LIMIT
console.print(n + 4, greet.twice(greet.LIMIT), greet.ratio, greet.ready)
`)

	jit, code := runNf(t, nf, filepath.Join(root, "main.nf"))
	if code != 0 {
		t.Fatalf("读包交出来的数该跑得通（%d）：%s", code, jit)
	}
	if want := "4100 8192 0.5 true\n"; jit != want {
		t.Fatalf("内存里直跑得到 %q，要 %q", jit, want)
	}

	out := filepath.Join(t.TempDir(), "main.exe")
	if _, code := runNf(t, nf, "build", filepath.Join(root, "main.nf"), "-o", out); code != 0 {
		t.Fatalf("nf build 的退出码是 %d", code)
	}
	b, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("跑产物失败：%v", err)
	}
	if string(b) != jit {
		t.Fatalf("产物给 %q，内存里直跑给 %q，两条路不一样", string(b), jit)
	}
}

// 顶层那段"先用后写"的规矩在这儿也成立：包的 const 排在门面文件的哪一行都不影响读得到，
// 静态槽先排好、值后算。这一条测的是"包自己顶层跑了之后值才在槽里"。
func TestPackageConstValueIsItsOwnTopLevelRun(t *testing.T) {
	nf := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "counter": "1.0" } }`)
	write(t, root, "libs/counter/nf.json", `{"name": "counter", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/counter/counter.nf", `const seed:int = 20
export const step:int = seed * 2
`)
	write(t, root, "main.nf", `import counter from "libs/counter"

console.print(counter.step)
`)
	out, code := runNf(t, nf, filepath.Join(root, "main.nf"))
	if code != 0 || strings.TrimSpace(out) != "40" {
		t.Fatalf("包自己的顶层算法没跑对（%d）：%s", code, out)
	}
}

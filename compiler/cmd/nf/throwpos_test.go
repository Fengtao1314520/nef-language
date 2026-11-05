// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// throwpos_test.go 钉跨文件那一条：抛出写在包里那一份文件中，接住发生在工程这一份文件里，
// 接住块第三个名字读到的位置必须是那一份包里的文件，而不是接住那一句所在的位置。
// 这一条只能用真文件与真包：位置是在编译那一步按"正在降的这份文件"排出来的，
// 单文件的降级测试里两份文件合成一份，测不出这件事。
//
// en: throwpos_test.go pins the cross-file case: the throw is written in a package's file and
// caught in the project's file, so the catch block's third name must read that package file's
// position, not the position of the catching sentence. Only real files and a real package can
// show this: the position is laid out during lowering, per the file being lowered, and a
// single-file lowering test merges the two files into one and cannot tell them apart.
package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const posPackFacade = `export method boom():int {
    let xs:list<int> = []
    return xs[3]
}
`

func TestPositionNamesTheThrowingFile(t *testing.T) {
	nf := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "boom": "1.0" } }`)
	write(t, root, "libs/boom/nf.json", `{"name": "boom", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/boom/boom.nf", posPackFacade)
	write(t, root, "main.nf", `import boom from "libs/boom"

try {
    console.print(boom.boom())
} catch (code, msg, at) {
    console.print(code, msg)
    console.print(at)
}
console.print("接住了还往下走")
`)

	jit, code := runNf(t, nf, filepath.Join(root, "main.nf"))
	if code != 0 {
		t.Fatalf("接住包里抛出的那一次没走完（%d）：%s", code, jit)
	}
	if !strings.Contains(jit, "10031 下标越界") {
		t.Fatalf("那句抛出没进来：%q", jit)
	}
	if !strings.Contains(jit, "boom.nf 3:14") {
		t.Fatalf("位置没指到包里那一份文件：%q", jit)
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

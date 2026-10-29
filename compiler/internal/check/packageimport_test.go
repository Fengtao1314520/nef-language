// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

// 整块挂名的 import 与 `包名.东西` 那一种取法。
//
// 这里不读盘：包根与那句引入的对应关系由装载那一侧填进 Program 的两张表，
// 检查这一层只认这两张表（`pkgload` 那边另有一条实际运行测试覆盖读盘的那一路）。

import (
	"testing"

	"neflang.org/nf/internal/ast"
)

const pkgMain = "C:/p/main.nf"
const pkgFacade = "C:/p/libs/greet.nfpack/greet.nf"
const pkgRoot = "C:/p/libs/greet.nfpack"

// pkgRun 拿一份门面源文件与一份入口源文件拼一个两文件的程序：
// 门面上写着它属于 libs/greet.nfpack 这个包，入口那一句 `import greet from "libs/greet"` 指到它。
func pkgRun(t *testing.T, facadeSrc, mainSrc string) []Error {
	t.Helper()
	greet := mustFile(t, pkgFacade, facadeSrc)
	main := mustFile(t, pkgMain, mainSrc)
	prog := &Program{
		Files:    []*ast.File{greet, main},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{},
		PkgRoots: map[string]string{pkgFacade: pkgRoot},
	}
	prog.PkgRefs[PkgRefKey(pkgMain, "libs/greet")] = pkgRoot
	return Run(prog)
}

// 包里交出来的函数与类型，外面用点号取，两条都走得通。
const pkgGreetSrc = `export struct Pair {
    left:int
}

method hidden():int {
    return 7
}

export method make():Pair {
    return Pair { left: 1 }
}

export method twice(n:int):int {
    return n * 2
}
`

func TestPackageImportCallAndType(t *testing.T) {
	errs := pkgRun(t, pkgGreetSrc, `import greet from "libs/greet"

let p:greet.Pair = greet.make()
console.print(p.left, greet.twice(3))
`)
	if len(errs) > 0 {
		t.Fatalf("包里的函数与类型都该取到，实际报错：%v", msgs(errs))
	}
}

// 拒绝的那六处，各覆盖一句完整文案。
func TestPackageImportRefused(t *testing.T) {
	t.Run("没挂 export", func(t *testing.T) {
		errs := pkgRun(t, pkgGreetSrc, `import greet from "libs/greet"

console.print(greet.hidden())
`)
		expectMsg(t, errs, "greet 里的 hidden 没有挂 export：包只把交出来的东西给别人用")
	})

	t.Run("包里没这个东西", func(t *testing.T) {
		errs := pkgRun(t, pkgGreetSrc, `import greet from "libs/greet"

console.print(greet.nothere())
`)
		expectMsg(t, errs, "greet 这个包里没有对外交出 nothere")
	})

	t.Run("函数名当类型写", func(t *testing.T) {
		errs := pkgRun(t, pkgGreetSrc, `import greet from "libs/greet"

let p:greet.twice = 1
console.print(p)
`)
		expectMsg(t, errs, "greet 里的 twice 是一个函数，不能当类型写")
	})

	t.Run("引进来却没用", func(t *testing.T) {
		errs := pkgRun(t, pkgGreetSrc, `import greet from "libs/greet"

console.print(1)
`)
		expectMsg(t, errs, "import 了 greet 这个包却没用到")
	})

	t.Run("同一个挂名挂两次", func(t *testing.T) {
		greet := mustFile(t, pkgFacade, pkgGreetSrc)
		main := mustFile(t, pkgMain, `import greet from "libs/greet"
import greet from "libs/greet"

console.print(greet.twice(1))
`)
		prog := &Program{
			Files:    []*ast.File{greet, main},
			Entry:    pkgMain,
			PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/greet"): pkgRoot},
			PkgRoots: map[string]string{pkgFacade: pkgRoot},
		}
		expectMsg(t, Run(prog), "greet 这个名字已经挂过一个包了：一个挂名只能指一个包")
	})

	t.Run("与本文件的定义撞", func(t *testing.T) {
		greet := mustFile(t, pkgFacade, pkgGreetSrc)
		main := mustFile(t, pkgMain, `import greet from "libs/greet"

method greet():int {
    return 1
}
console.print(greet())
`)
		prog := &Program{
			Files:    []*ast.File{greet, main},
			Entry:    pkgMain,
			PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/greet"): pkgRoot},
			PkgRoots: map[string]string{pkgFacade: pkgRoot},
		}
		expectMsg(t, Run(prog), "本文件里已经有 greet 这个名字，包的挂名不能与它撞：给这句 import 换个挂名")
	})

	t.Run("挂名占了标准库的组名", func(t *testing.T) {
		greet := mustFile(t, "C:/p/libs/time.nfpack/time.nf", pkgGreetSrc)
		main := mustFile(t, pkgMain, `import time from "libs/time"

console.print(time.twice(1))
`)
		prog := &Program{
			Files:    []*ast.File{greet, main},
			Entry:    pkgMain,
			PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/time"): "C:/p/libs/time.nfpack"},
			PkgRoots: map[string]string{"C:/p/libs/time.nfpack/time.nf": "C:/p/libs/time.nfpack"},
		}
		expectMsg(t, Run(prog), "time 是标准库的组名，包的挂名不能用它")
	})

	t.Run("装载那一头没解析出来", func(t *testing.T) {
		main := mustFile(t, pkgMain, `import greet from "libs/greet"

console.print(greet.twice(1))
`)
		prog := &Program{Files: []*ast.File{main}, Entry: pkgMain}
		expectMsg(t, Run(prog), `找不到 import 的包 "libs/greet"`)
	})

	t.Run("包里的名字点两层", func(t *testing.T) {
		errs := pkgRun(t, pkgGreetSrc, `import greet from "libs/greet"

let p:greet.Pair.left = 1
console.print(p)
`)
		expectMsg(t, errs, "包里的名字只点一层：greet.Pair.left 这样写认不出")
	})
}

func expectMsg(t *testing.T, errs []Error, want string) {
	t.Helper()
	for _, e := range errs {
		if e.Msg == want {
			return
		}
	}
	t.Fatalf("没报出整句 %q；实际报错：%v", want, msgs(errs))
}

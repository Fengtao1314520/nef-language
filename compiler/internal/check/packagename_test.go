// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// packagename_test.go 覆盖名字表带包限定之后的两件事：两份同名的东西各归各的包；
// 门面不能把依赖里的类型交出去。
//
// 与 packageimport_test.go 同一做法：不读盘，包根与那句引入的对应关系由装载那一侧填进
// Program 的三张表（PkgRefs、PkgRoots、Facades），检查这一层只认这三张表。

package check

import (
	"testing"

	"neflang.org/nf/internal/ast"
)

const aaFacade = "C:/p/libs/aa.nfpack/aa.nf"
const aaRoot = "C:/p/libs/aa.nfpack"
const bbFacade = "C:/p/libs/bb.nfpack/bb.nf"
const bbRoot = "C:/p/libs/bb.nfpack"

// aaSrc 与 bbSrc 各有一只 `Pair`，字段不一样。名字表按裸名字存的时候，第二只会被
// "Pair 已经定义过了"拒绝；带包限定以后两只各归各的包根。
const aaSrc = `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 1 }
}
`

const bbSrc = `export struct Pair {
    name:string
}

export method make():Pair {
    return Pair { name: "hi" }
}
`

// twoPkg 拼一个"入口 + 两份包"的程序。
func twoPkg(t *testing.T, mainSrc string) *Program {
	t.Helper()
	aa := mustFile(t, aaFacade, aaSrc)
	bb := mustFile(t, bbFacade, bbSrc)
	main := mustFile(t, pkgMain, mainSrc)
	return &Program{
		Files: []*ast.File{main, aa, bb},
		Entry: pkgMain,
		PkgRefs: map[string]string{
			PkgRefKey(pkgMain, "libs/aa"): aaRoot,
			PkgRefKey(pkgMain, "libs/bb"): bbRoot,
		},
		PkgRoots: map[string]string{aaFacade: aaRoot, bbFacade: bbRoot},
	}
}

// 两个包各有一只 Pair，一起用不该撞。
func TestTwoPackagesSameTypeName(t *testing.T) {
	errs := Run(twoPkg(t, `import aa from "libs/aa"
import bb from "libs/bb"

let x:aa.Pair = aa.make()
let y:bb.Pair = bb.make()
console.print(x.left, y.name)
`))
	if len(errs) > 0 {
		t.Fatalf("两只同名的 Pair 该各归各的包，实际报错：%v", msgs(errs))
	}
}

// 各归各的包根就是两个类型：拿 aa.Pair 接 bb.make() 返回的那一只要拒绝，
// 而且这一句要把各自的包根带出来——只看原名分不出是哪一个。
func TestSameTypeNameFromTwoPackagesNotInterchangeable(t *testing.T) {
	want := "这里要的是 " + aaRoot + "/Pair，给的是 " + bbRoot + "/Pair；类型之间不自动转换"
	expectMsg(t, Run(twoPkg(t, `import aa from "libs/aa"
import bb from "libs/bb"

let x:aa.Pair = bb.make()
console.print(x.left)
`)), want)
}

// 同一个包的两份版本并在一处：两份 `c` 各有各的包根（分别由两份包各自引入），互不影响。
func TestTwoVersionsOfOnePackage(t *testing.T) {
	c13 := mustFile(t, "C:/p/libs/big1.nfpack/pkg/c/1.3.0/c.nf", `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 13 }
}
`)
	c20 := mustFile(t, "C:/p/libs/big2.nfpack/pkg/c/2.0.0/c.nf", `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 20 }
}
`)
	big1 := mustFile(t, "C:/p/libs/big1.nfpack/big1.nf", `import c from "libs/c"

export method value():int {
    let p:c.Pair = c.make()
    return p.left
}
`)
	big2 := mustFile(t, "C:/p/libs/big2.nfpack/big2.nf", `import c from "libs/c"

export method value():int {
    let p:c.Pair = c.make()
    return p.left
}
`)
	main := mustFile(t, pkgMain, `import big1 from "libs/big1"
import big2 from "libs/big2"

console.print(big1.value(), big2.value())
`)
	const r13 = "C:/p/libs/big1.nfpack/pkg/c/1.3.0"
	const r20 = "C:/p/libs/big2.nfpack/pkg/c/2.0.0"
	prog := &Program{
		Files: []*ast.File{main, big1, big2, c13, c20},
		Entry: pkgMain,
		PkgRefs: map[string]string{
			PkgRefKey(pkgMain, "libs/big1"): "C:/p/libs/big1.nfpack",
			PkgRefKey(pkgMain, "libs/big2"): "C:/p/libs/big2.nfpack",
			PkgRefKey(big1.Path, "libs/c"):  r13,
			PkgRefKey(big2.Path, "libs/c"):  r20,
		},
		PkgRoots: map[string]string{
			"C:/p/libs/big1.nfpack/big1.nf": "C:/p/libs/big1.nfpack",
			"C:/p/libs/big2.nfpack/big2.nf": "C:/p/libs/big2.nfpack",
			c13.Path:                        r13,
			c20.Path:                        r20,
		},
	}
	if errs := Run(prog); len(errs) > 0 {
		t.Fatalf("同一个包的两份版本该能并存，实际报错：%v", msgs(errs))
	}
}

// 花括号那一种 import 不允许指向包内部：那是给工程里的文件互相引用的写法。
func TestBraceImportIntoPackageRefused(t *testing.T) {
	aa := mustFile(t, aaFacade, aaSrc)
	bb := mustFile(t, bbFacade, bbSrc)
	main := mustFile(t, pkgMain, `import { Pair } from "./libs/aa.nfpack/aa.nf"

let x:Pair = 1
console.print(x)
`)
	prog := &Program{
		Files:    []*ast.File{main, aa, bb},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/aa"): aaRoot},
		PkgRoots: map[string]string{aaFacade: aaRoot, bbFacade: bbRoot},
	}
	expectMsg(t, Run(prog), `./libs/aa.nfpack/aa.nf 在包里面：引包要写 import 包名 from "…"，花括号那一套是给工程里的文件用的`)
}

// 包里的文件也不能反过来引工程里的东西：同一份包换一个工程还得照样编。
func TestPackageImportingProjectFileRefused(t *testing.T) {
	helper := mustFile(t, "C:/p/libs/util.nf", `export method far():int { return 1 }`)
	aa := mustFile(t, aaFacade, `import { far } from "../util.nf"

export method value():int {
    return far()
}
`)
	main := mustFile(t, pkgMain, `import aa from "libs/aa"

console.print(aa.value())
`)
	prog := &Program{
		Files:    []*ast.File{main, helper, aa},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/aa"): aaRoot},
		PkgRoots: map[string]string{aaFacade: aaRoot},
	}
	expectMsg(t, Run(prog), `包里的文件只能引同一个包里的文件：../util.nf 不在这个包里面`)
}

// 同包内的两份文件互相用花括号引，照旧认：这一条只管跨出包那一步。
func TestSiblingFileInSamePackageOkay(t *testing.T) {
	helper := mustFile(t, aaRoot+"/inner.nf", `export method twice(n:int):int { return n * 2 }`)
	aa := mustFile(t, aaFacade, `import { twice } from "./inner.nf"

export method value():int {
    return twice(3)
}
`)
	main := mustFile(t, pkgMain, `import aa from "libs/aa"

console.print(aa.value())
`)
	prog := &Program{
		Files:    []*ast.File{main, aa, helper},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/aa"): aaRoot},
		PkgRoots: map[string]string{aaFacade: aaRoot, helper.Path: aaRoot},
	}
	if errs := Run(prog); len(errs) > 0 {
		t.Fatalf("同包内花括号引该认，实际报错：%v", msgs(errs))
	}
}

// depProgram 拼一份"门面 + 它引入的依赖"的程序；onFacade 标记这一份是否被当作门面引入。
func depProgram(t *testing.T, facadeSrc string, onFacade bool) *Program {
	t.Helper()
	const layer = "C:/p/libs/aa.nfpack/pkg/c/1.0"
	dep := mustFile(t, layer+"/c.nf", `export struct Pair {
    left:int
}
`)
	facade := mustFile(t, aaFacade, facadeSrc)
	main := mustFile(t, pkgMain, `import aa from "libs/aa"

console.print(1)
`)
	prog := &Program{
		Files:    []*ast.File{main, facade, dep},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/aa"): aaRoot, PkgRefKey(facade.Path, "libs/c"): layer},
		PkgRoots: map[string]string{aaFacade: aaRoot, dep.Path: layer},
	}
	if onFacade {
		prog.Facades = map[string]bool{aaFacade: true}
	}
	return prog
}

const leakyFacadeSrc = `import c from "libs/c"

export method value(x:c.Pair):int {
    return x.left
}
`

// 门面交出去的类型得是这个包自己定义的。
func TestFacadeMayNotLeakDependencyType(t *testing.T) {
	expectMsg(t, Run(depProgram(t, leakyFacadeSrc, true)),
		"Pair 是别的包里的类型：门面只能导出这个包自己定义的类型")
}

// 同一份源文件不被当门面用时不拒绝：包里的其余文件可以使用依赖。
func TestLeakRuleOnlyAppliesToFacade(t *testing.T) {
	errs := Run(depProgram(t, leakyFacadeSrc, false))
	for _, e := range errs {
		if e.Msg == "Pair 是别的包里的类型：门面只能导出这个包自己定义的类型" {
			t.Fatalf("这一条只管门面：%v", msgs(errs))
		}
	}
}

// 门面把自己这一包定义的类型交出去不算泄漏。
func TestFacadeOwnTypeOkay(t *testing.T) {
	src := `import c from "libs/c"

struct Inner {
    n:int
}

export struct Wrap {
    inner:Inner
}

export method value(w:Wrap):int {
    return w.inner.n
}
`
	errs := Run(depProgram(t, src, true))
	for _, e := range errs {
		if e.Msg == "Inner 是别的包里的类型：门面只能导出这个包自己定义的类型" ||
			e.Msg == "Wrap 是别的包里的类型：门面只能导出这个包自己定义的类型" {
			t.Fatalf("自己包里的类型不该被拦：%v", msgs(errs))
		}
	}
}

// 尖括号里的也算：`list<c.Pair>` 交出去的仍是别的包里的类型。
func TestFacadeLeakInsideContainer(t *testing.T) {
	src := `import c from "libs/c"

export method value(xs:list<c.Pair>):int {
    return 0
}
`
	expectMsg(t, Run(depProgram(t, src, true)),
		"Pair 是别的包里的类型：门面只能导出这个包自己定义的类型")
}

// 包里 `export type Row = Pair` 那条别名，外面写 `a.Row` 也认：造值与字段表都跟着
// 解到底那一只类型走（别名的键也按包分开存）。
func TestPackageExportedAlias(t *testing.T) {
	facade := mustFile(t, aaFacade, `export struct Pair {
    left:int
}

export type Row = Pair

export method make():Row {
    return Pair { left: 3 }
}
`)
	main := mustFile(t, pkgMain, `import aa from "libs/aa"

let r:aa.Row = aa.make()
console.print(r.left)
`)
	prog := &Program{
		Files:    []*ast.File{main, facade},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/aa"): aaRoot},
		PkgRoots: map[string]string{aaFacade: aaRoot},
	}
	if errs := Run(prog); len(errs) > 0 {
		t.Fatalf("包里交出来的别名该当类型用，实际报错：%v", msgs(errs))
	}
}

// 工程里的名字对包是看不见的：包内那份文件写一个只有工程里才有的类型，报"没有这个类型"。
func TestPackageCannotSeeProjectNames(t *testing.T) {
	helper := mustFile(t, "C:/p/util.nf", `export struct Point {
    x:int
}
`)
	aa := mustFile(t, aaFacade, `export method make():Point {
    return Point { x: 1 }
}
`)
	main := mustFile(t, pkgMain, `import { Point } from "./util.nf"
import aa from "libs/aa"

let p:Point = Point { x: 2 }
console.print(p.x)
`)
	prog := &Program{
		Files:    []*ast.File{main, helper, aa},
		Entry:    pkgMain,
		PkgRefs:  map[string]string{PkgRefKey(pkgMain, "libs/aa"): aaRoot},
		PkgRoots: map[string]string{aaFacade: aaRoot},
	}
	expectMsg(t, Run(prog), "没有 Point 这个类型")
}

// 别名引来的类型换个名字也该是同一只：字段表与形状只认定义处那一个规范名。
func TestAliasedImportIsTheSameType(t *testing.T) {
	helper := mustFile(t, "C:/p/util.nf", `export struct Point {
    x:int
}

export method make():Point {
    return Point { x: 1 }
}
`)
	main := mustFile(t, pkgMain, `import { Point as P, make } from "./util.nf"

let a:P = make()
console.print(a.x)
`)
	prog := &Program{Files: []*ast.File{main, helper}, Entry: pkgMain}
	if errs := Run(prog); len(errs) > 0 {
		t.Fatalf("改名引来的类型该是同一只，实际报错：%v", msgs(errs))
	}
}

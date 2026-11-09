// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// generic_test.go 钉泛型跨包那一条：包里定义带类型参数的声明，工程里用另一种类型实例化。
// 起真子进程走的是装载包的完整路径（`nf.json` 的依赖、包根进名字表的键），
// 与 `pack_test.go` 那几条同一套做法。
//
// 写法上有一条现成的限制要避开：`new` 后面只准写一个名字，`new 包.类型(...)` 这台编译器
// 从来不接受（`greet.Pair` 那一种类型要拿值，得走包导出的函数）。
// 所以这里的实例都是从包导出的函数那一步生成的，工程这边用类型标注把它们接住。
//
// en: generic_test.go pins cross-package generics: a package defines declarations with type
// parameters and the project instantiates them with other types. These run real subprocesses
// through the whole package-loading path (the nf.json dependency, the package root inside name
// keys), the same way pack_test.go does.
//
// One existing limitation shapes the spelling: `new` accepts a single name only, and
// `new pkg.Type(...)` has never been accepted by this compiler (a value of a type like
// greet.Pair arrives through a function the package exports). So the instances here come from
// exported functions, and the project receives them with type annotations.
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGenericPackageInstantiatesOnUsingSide 钉住"实例在用的那一侧现生成"这一句：
// 包里那份容器存的还是源文件，它自己只把 `Counter<int>` 用了一次；
// 工程里要的是 `Counter<string>` 与 `Counter<float>`，那两只是在工程的这一次编译里生成的。
//
// en: TestGenericPackageInstantiatesOnUsingSide pins "instances are built on the using side":
// the container keeps source files, and the package itself uses Counter<int> once; the project
// wants Counter<string> and Counter<float>, and those two come
// into being during the project's own compile.
func TestGenericPackageInstantiatesOnUsingSide(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "counters": "1.0" } }`)
	write(t, root, "libs/counters/nf.json", `{"name": "counters", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/counters/counters.nf", `export class Counter<Item> {
    v:Item

    method get():Item {
        return v
    }

    method plus(k:Item):Item {
        v = k
        return v
    }
}

export method build<T>(v:T):Counter<T> {
    return new Counter<T>(v: v)
}

export method sample():int {
    let c:Counter<int> = build(1)
    return c.get()
}
`)
	write(t, root, "main.nf", `import counters from "libs/counters"

let a:counters.Counter<string> = counters.build("甲")
let b:counters.Counter<float> = counters.build(1.5)
console.print(counters.sample(), a.get(), b.plus(2.5), b.get())
console.print(counters.build(7), counters.build("乙"))
`)
	out, code := nfRun(t, exe, filepath.Join(root, "main.nf"))
	if code != 0 {
		t.Fatalf("工程里实例化包里的泛型该跑通（%d）：%s", code, out)
	}
	want := "1 甲 2.5 2.5\nCounter<int> { v: 7 } Counter<string> { v: \"乙\" }\n"
	if out != want {
		t.Errorf("得到 %q，要 %q", out, want)
	}
}

// TestTwoPackagesWithSameGenericName 钉住包根进实例的键：两个包各有一只 `class Cell<T>`，
// 字段不一样，工程里两只各用一次。键合在一起时打印与析构会按对方的形状走——
// 这与工程里那只 `Pair` 与包里那只同名 `Pair` 必须分开是同一条道理，带上实参那一段也一样。
//
// en: TestTwoPackagesWithSameGenericName pins the package root inside an instance's key: two
// packages each define a class Cell<T> with different fields, and the project uses both. Merge
// the keys and printing and destruction follow the other one's shape - the same rule that keeps
// a project's `Pair` apart from a package's same-named Pair, brackets and all.
func TestTwoPackagesWithSameGenericName(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "left": "1.0", "right": "1.0" } }`)
	write(t, root, "libs/left/nf.json", `{"name": "left", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/left/left.nf", `export class Cell<T> {
    a:T
}

export method makeCell<T>(v:T):Cell<T> {
    return new Cell<T>(a: v)
}
`)
	write(t, root, "libs/right/nf.json", `{"name": "right", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/right/right.nf", `export class Cell<T> {
    a:T
    b:int
}

export method makeCell<T>(v:T):Cell<T> {
    return new Cell<T>(a: v, b: 7)
}
`)
	write(t, root, "main.nf", `import left from "libs/left"
import right from "libs/right"

let x:left.Cell<int> = left.makeCell(1)
let y:right.Cell<string> = right.makeCell("甲")
console.print(x, y)
`)
	out, code := nfRun(t, exe, filepath.Join(root, "main.nf"))
	if code != 0 {
		t.Fatalf("两只同名不同包的泛型该跑通（%d）：%s", code, out)
	}
	if !strings.Contains(out, "Cell<int> { a: 1 }") || !strings.Contains(out, `Cell<string> { a: "甲", b: 7 }`) {
		t.Errorf("两只实例的打印该各自按自己的形状：got %q", out)
	}
}

// TestGenericPackageRefusesProjectNames 钉住"包看不见工程"这一条不被实例化绕过：
// 包里的泛型字段写一个只在工程里存在的类型名，报错报在包那份文件里，
// 由工程这边写出那一只实例把它逼出来。
//
// en: TestGenericPackageRefusesProjectNames pins that a package still cannot see the project
// once instantiation is involved: a generic field in the package naming a type only the project
// defines reports inside the package's file, drawn out by the project building that instance.
func TestGenericPackageRefusesProjectNames(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "inner": "1.0" } }`)
	write(t, root, "libs/inner/nf.json", `{"name": "inner", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/inner/inner.nf", `export class Holder<T> {
    v:T
    w:ProjectOnly
}

export method makeHolder<T>(v:T):Holder<T> {
    return new Holder<T>(v: v, w: null)
}
`)
	write(t, root, "main.nf", `import inner from "libs/inner"

struct ProjectOnly {
    n:int
}

method probe(h:inner.Holder<int>) {
    console.print(1)
}

probe(inner.makeHolder(1))
`)
	out, code := nfRun(t, exe, "check", filepath.Join(root, "main.nf"))
	if code == 0 || !strings.Contains(out, "没有 ProjectOnly 这个类型") {
		t.Errorf("包里的字段写工程里的类型名该被拦住（%d）：%s", code, out)
	}
	if !strings.Contains(out, "inner.nf") {
		t.Errorf("报错该归到定义它的那份包文件：%s", out)
	}
}

// TestTwoPackagesSameInstanceName 钉的是 `TestTwoPackagesWithSameGenericName` 没走过的那种用法：
// 两只包各有一只 `class Cell<T>`，字段不一样，工程里两边填的是同一个实参。
// 那一条测试填的是 `Cell<int>` 与 `Cell<string>`，两只实例的键天然不同；这里两只都叫 `Cell<int>`，
// 给人看的那一串一模一样，只有带包根的内部键分得开。合成一格的话打印与析构会按对方的形状走，
// 屏幕上少的是 `b: 7`，重的那一只释放时会按三只字段的形状去拆两只字段的块。
//
// 这一条也回答了开工前那句没裁完的问题：那里写着"实例名必须唯一，名字重复要在检查期拒掉，
// 并且新增一句报错原话"。实测下来不需要那一句——内部键带包根，两只同名的实例本来就不是
// 同一个位置，所以没有任何一处会重复；这一条钉的就是"本来就不是同一个位置"这一件事。
// 报错清单因此一句没多。
//
// en: TestTwoPackagesSameInstanceName covers the one case TestTwoPackagesWithSameGenericName does
// not: two packages each declare class Cell<T> with different fields and the project instantiates
// both with the same argument. There the keys differ by themselves; here both read Cell<int>, so only
// the package root inside the internal key tells them apart. Merge them and printing loses "b: 7"
// while the heavier release walks a three-field shape over a two-field block. This also settles the
// question left open before work started, which asked for a new rejection sentence on duplicate
// instance names: measured, none is needed - the internal key carries the package root, so
// two same-named instances were never the same slot and nothing collides. The error list grows by
// nothing.
func TestTwoPackagesSameInstanceName(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "left": "1.0", "right": "1.0" } }`)
	write(t, root, "libs/left/nf.json", `{"name": "left", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/left/left.nf", `export class Cell<T> {
    a:T
}

export method makeCell<T>(v:T):Cell<T> {
    return new Cell<T>(a: v)
}
`)
	write(t, root, "libs/right/nf.json", `{"name": "right", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/right/right.nf", `export class Cell<T> {
    a:T
    b:int
}

export method makeCell<T>(v:T):Cell<T> {
    return new Cell<T>(a: v, b: 7)
}
`)
	write(t, root, "main.nf", `import left from "libs/left"
import right from "libs/right"

let x:left.Cell<int> = left.makeCell(1)
let y:right.Cell<int> = right.makeCell(2)
console.print(x, y)
`)
	out, code := nfRun(t, exe, filepath.Join(root, "main.nf"))
	if code != 0 {
		t.Fatalf("两只同名同实参的实例该各自一格（%d）：%s", code, out)
	}
	want := "Cell<int> { a: 1 } Cell<int> { a: 2, b: 7 }\n"
	if out != want {
		t.Fatalf("两只实例的键合成一格了：\n got %q\nwant %q", out, want)
	}
}

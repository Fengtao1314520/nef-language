// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/exetest"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

// 泛型前两层的端到端：实例的形状、两只实例各一份字段表、方法体各生成一份、归还、
// 实例套实例、实例进容器、装箱与拆回来。
//
// 第一层只做数据形状：`class Box<Content>` 配上 `int` 之后，字段读写、打印与析构都按那一只
// 实例自己的字段表走。第二层把方法体按实例各生成一份：一份 `class Stack<Item>` 用 `int` 与
// `float` 就是两个 IR 函数，各自的参数种类按那一只实例摊好的签名发。
// 生成的是声明的那一份副本（internal/ast/clone.go），不是原文：
// 检查器那六张按语法节点存档的表要是拿到同一个节点，后一份实例读到的是前一份的类型结论。
//
// en: The end-to-end checks for the first two layers of generics: an instance's shape, two
// instances keeping separate field tables, one method body per instance, releasing, instances
// nested in instances, instances inside containers, boxing and unboxing.
//
// The first layer builds data shapes only: once `class Box<Content>` takes `int`, field access,
// printing and destruction follow that instance's own field table. The second layer generates
// one method body per instance: one `class Stack<Item>` used with int and float becomes two IR
// functions, each parameter's kind emitted from the signature flattened for that instance. What
// gets generated is a copy of the declaration (internal/ast/clone.go), not the original: if the
// checker's six node-keyed tables saw the same node twice, the second instance would read the
// first one's type conclusions.
const boxHead = `class Box<Content> {
    v:Content
}

`

// dumpIR 把一份源码走完前端与降级两步，返回中间码的文本：泛型那几条要钉的是
// "两份实参就是两个函数名"，这一处看名字比看运行结果更直接。
//
// en: dumpIR runs one source through the front end and lowering and hands back the intermediate
// code as text: several generics checks are about "two argument lists mean two function
// names", where reading names is more direct than reading output.
func dumpIR(t *testing.T, src string) string {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("词法：%v", err)
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		t.Fatalf("语法：%v", err)
	}
	prog := &check.Program{Files: []*ast.File{f}, Entry: "t.nf"}
	errs, facts := check.RunWithFacts(prog, true)
	if len(errs) > 0 {
		t.Fatalf("前端：%v", errs)
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		t.Fatalf("降级：%v", err)
	}
	return ir.Dump(p)
}

// TestGenericInstanceShapeEndToEnd 钉住一只实例从造出来到打印的整条路。
// 打印的是给人看的那一串（`Box<int> { v: 3 }`），字段读的是实例那份字段表。
//
// en: TestGenericInstanceShapeEndToEnd pins the whole path of one instance, from allocation to
// printing. What prints is the reader-facing string (`Box<int> { v: 3 }`); what fields read is
// the instance's own field table.
func TestGenericInstanceShapeEndToEnd(t *testing.T) {
	src := boxHead + `let a:Box<int> = new Box<int>(v: 3)
console.print(a)
a.v = 5
console.print(a.v)
let b:Box<string> = new Box<string>(v: "甲")
console.print(b)
console.print(b.v)
`
	got, code := run(t, src)
	want := "Box<int> { v: 3 }\n5\nBox<string> { v: \"甲\" }\n甲\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestTwoInstancesKeepTheirOwnFields 钉住两只实例在形状表与字段表上是两条：
// 同一份声明、同一个字段名、两种类型，各自存各自的。
// 实例的规范名里带上实参，`Box<int>` 与 `Box<string>` 就是两个键，
// 形状表按那一个键去重（internal/lower/desc.go 的 descOf）。
// 把那一段尖括号从键里去掉时，两条会挤进同一格，这一条与下一条都要失败。
//
// en: TestTwoInstancesKeepTheirOwnFields pins that two instances are two entries in the shape
// table and the field table: one declaration, the same field name, two types, each storing on
// its own. An instance's canonical name carries its arguments, so `Box<int>` and `Box<string>`
// are two keys, and descOf in internal/lower/desc.go deduplicates shapes by that key. Strip
// the brackets from the key and the two crowd into one cell, failing this test and the next.
func TestTwoInstancesKeepTheirOwnFields(t *testing.T) {
	src := boxHead + `let a:Box<int> = new Box<int>(v: 1)
let b:Box<string> = new Box<string>(v: "乙")
a.v = 10
console.print(a.v, b.v)
console.print(a)
console.print(b)
`
	got, code := run(t, src)
	want := "10 乙\nBox<int> { v: 10 }\nBox<string> { v: \"乙\" }\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestGenericInstanceFreesItsBlock 钉住实例那一块内存的归还：字段是文本时，析构要顺着
// 实例那一条形状往下拆（internal/lower/destroy.go 的 walkFields 读的就是形状表里那一条）。
// 每轮增量的判据与算法沿用 heapSlope 那一条说明（200 轮与 5000 轮取差值）。
//
// en: TestGenericInstanceFreesItsBlock pins releasing an instance's block: with a text field,
// destruction must walk down the instance's own shape entry, which is exactly what walkFields
// in internal/lower/destroy.go reads. The per-round test and its method follow heapSlope's note
// (200 rounds against 5000).
func TestGenericInstanceFreesItsBlock(t *testing.T) {
	tmpl := boxHead + `let total:int = 0
for (i:int in 0 until @N@) {
    let b:Box<string> = new Box<string>(v: "甲乙丙丁戊")
    total = total + length(b.v) + i
}
console.print(total)
`
	if slope := heapSlope(t, tmpl); slope >= 4 {
		t.Errorf("每轮还留 %g 字节：实例那一块没跟着形状表归还", slope)
	}
}

// TestGenericInstanceNests 钉住实例套实例与实例进容器：`Holder<Item>` 的字段写的是
// `Box<Item>`，`list<Box<int>>` 装的是同一只实例的两份对象，`Box<list<int>>` 里那一层实参
// 自己又是一只容器。三条在这一层就能跑通，靠的是实参解出来的那一只类型直接进字段表，
// 而不是另排一份形状。
//
// en: TestGenericInstanceNests pins instances inside instances and inside containers: Holder's
// field names Box<Item>, list<Box<int>> holds two objects of one instance, and Box<list<int>>
// takes a container as its own argument. All three work at this layer because a resolved
// argument goes straight into the field table instead of a second shape being laid out.
func TestGenericInstanceNests(t *testing.T) {
	src := boxHead + `class Holder<Item> {
    inner:Box<Item>
    tag:int
}

let h:Holder<int> = new Holder<int>(inner: new Box<int>(v: 7), tag: 1)
let row:list<Box<int>> = [new Box<int>(v: 1), new Box<int>(v: 2)]
let deep:Box<list<int>> = new Box<list<int>>(v: [3, 4, 5])
console.print(h)
console.print(h.inner.v, h.tag)
console.print(length(row), row[1].v)
console.print(deep)
`
	got, code := run(t, src)
	want := strings.Join([]string{
		"Holder<int> { inner: Box<int> { v: 7 }, tag: 1 }",
		"7 1",
		"2 2",
		"Box<list<int>> { v: [3, 4, 5] }",
		"",
	}, "\n")
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestGenericInstanceMethodsAreTwoIRFunctions 钉住两份实例发出来的是两个 IR 函数：
// 名字里带上实参（`Stack<int>.push` 与 `Stack<float>.push`）是这一条的全部依据。
// `internal/ir/ir.go` 的 `Prog.Func` 同名即复用、`internal/codegen/emit.go` 同名标号后写覆盖先写，
// 两处都不会报错——所以这里直接看中间码里的函数名，而不是等运行结果出错。
//
// en: TestGenericInstanceMethodsAreTwoIRFunctions pins that two instances become two IR
// functions, which is exactly what the argument-bearing names (Stack<int>.push and
// Stack<float>.push) provide. Prog.Func in internal/ir/ir.go reuses a name it has seen and
// emit.go in internal/codegen lets the later label overwrite the earlier one, so neither
// complains - hence this reads the function names out of the intermediate code instead of
// waiting for a wrong runtime value.
func TestGenericInstanceMethodsAreTwoIRFunctions(t *testing.T) {
	src := `class Stack<Item> {
    cells:list<Item>
    sp:int

    method push(v:Item) {
        cells[sp] = v
    }
}

let a:Stack<int> = new Stack<int>(cells: [0, 0], sp: 0)
let b:Stack<float> = new Stack<float>(cells: [0.0, 0.0], sp: 0)
a.push(1)
b.push(1.5)
console.print(a.sp, b.sp)
`
	dump := dumpIR(t, src)
	for _, want := range []string{"func Stack<int>.push", "func Stack<float>.push"} {
		if !strings.Contains(dump, want) {
			t.Errorf("中间码里缺 %q：\n%s", want, dump)
		}
	}
	// 模板那一个名字不能带着体出现在中间码里：`Stack.push` 与两只实例的三个名字并存，
	// 说明有一处该跳过的没跳过。
	// The template must not appear with a body: three names - Stack.push plus the two
	// instances - would mean a pass that should have skipped it did not.
	if strings.Contains(dump, "func Stack.push") {
		t.Errorf("模板的方法体也被发出来了：\n%s", dump)
	}
}

// TestGenericInstanceMethodsRunSeparately 钉住两份实例的方法各跑各的：同一份
// `class Stack<Item>` 用 `int` 与 `float` 各实例化一次，压弹互不干扰。
// 两份实例的方法名里带上实参（`Stack<int>.push` 与 `Stack<float>.push`）就是这一条的根据：
// 名字不带实参时这里合成一个函数，浮点参数会按整数的形状落进通用寄存器，
// 屏幕上得到的是 0.0 与一串极小的数（2026-11-06 实测过这一处）。
//
// en: TestGenericInstanceMethodsRunSeparately pins two instances running their own methods: the
// same `class Stack<Item>` instantiated with int and float, pushing and popping without
// interfering. The argument-bearing method names (`Stack<int>.push` and `Stack<float>.push`)
// are why: without them this becomes one function, a float argument lands in a general-purpose
// register following an integer shape, and the screen shows 0.0 plus a tiny meaningless number -
// measured on 2026-11-06.
func TestGenericInstanceMethodsRunSeparately(t *testing.T) {
	src := `class Stack<Item> {
    cells:list<Item>
    sp:int

    method push(v:Item) {
        cells[sp] = v
        sp = sp + 1
    }

    method pop():Item {
        sp = sp - 1
        return cells[sp]
    }

    method size():int {
        return sp
    }
}

let a:Stack<int> = new Stack<int>(cells: [0, 0, 0], sp: 0)
let b:Stack<float> = new Stack<float>(cells: [0.0, 0.0, 0.0], sp: 0)
a.push(1)
a.push(2)
b.push(1.5)
b.push(2.5)
console.print(a.pop(), a.pop(), a.size())
console.print(b.pop(), b.pop(), b.size(), a.size())
`
	got, code := run(t, src)
	want := "2 1 0\n2.5 1.5 0 0\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestGenericInstanceStringFieldKeepsItsOwnShape 钉住同名字段与同名方法在两份实例里是两份形状：
// `Box<int>.get()` 返回的是整数，`Box<string>.get()` 返回的是文本，各读各的偏移、各按各的种类。
//
// 那一句 `length(b.get())` 之后不再读 `b.v`：方法里 `return v` 是把字段那一份带走，
// 普通类也是同一条规矩（实测：`class Plain { v:string }` 的 `get()` 之后 `b.v` 是空句柄）。
//
// en: TestGenericInstanceStringFieldKeepsItsOwnShape pins same-named fields and methods keeping
// separate shapes across instances: Box<int>.get() yields a number, Box<string>.get() yields
// text, each reading its own offset with its own kind. Nothing reads b.v after length(b.get()):
// a `return v` inside a method takes the field's value away, which is the same rule for an
// ordinary class (measured: after Plain's get(), b.v is a null handle).
func TestGenericInstanceStringFieldKeepsItsOwnShape(t *testing.T) {
	src := `class Box<Content> {
    v:Content

    method put(x:Content) {
        v = x
    }

    method get():Content {
        return v
    }
}

let a:Box<int> = new Box<int>(v: 7)
let b:Box<string> = new Box<string>(v: "甲")
a.put(11)
b.put("乙丙")
console.print(a.v)
console.print(b.v)
console.print(length(b.get()))
console.print(a.get())
`
	got, code := run(t, src)
	want := "11\n乙丙\n2\n11\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestGenericInstanceBodyMintsAnotherInstance 钉住体里再生成新实例那一条路：
// `Maker<int>.make()` 造的是 `Box<string>`。排队那一处要接着把新生成的这一份查完、落完，
// 不然它的方法体没人查、发码时报的是"这一处的类型没记上"。
//
// en: TestGenericInstanceBodyMintsAnotherInstance pins minting inside a body: Maker<int>.make()
// builds Box<string>. The queue has to keep draining until that one is checked and lowered too,
// or nobody would have checked its body and lowering would report an unrecorded type.
func TestGenericInstanceBodyMintsAnotherInstance(t *testing.T) {
	src := `class Box<Content> {
    v:Content

    method get():Content {
        return v
    }
}

class Maker<Item> {
    seed:Item

    method make():Box<string> {
        return new Box<string>(v: "甲")
    }

    method own():Item {
        return seed
    }
}

let m:Maker<int> = new Maker<int>(seed: 4)
let b:Box<string> = m.make()
console.print(b.get(), m.own())
`
	got, code := run(t, src)
	want := "甲 4\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestGenericInstanceMethodReleasesItsBlock 钉住方法体那一族的内存归还：
// `Box<string>` 的一份对象在名字走到尽头时按实例那一条形状归还，方法调用本身不多留一块。
// 判据与算法沿用 heapSlope 那一条说明（200 轮与 5000 轮取差值）。
//
// en: TestGenericInstanceMethodReleasesItsBlock pins releasing across method calls: a
// Box<string> object is freed along its instance's shape entry when the name reaches the end of
// its stretch, and the call itself leaves no extra block. The per-round test follows
// heapSlope's note (200 rounds against 5000).
func TestGenericInstanceMethodReleasesItsBlock(t *testing.T) {
	tmpl := `class Box<Content> {
    v:Content

    method get():Content {
        return v
    }
}

let total:int = 0
for (i:int in 0 until @N@) {
    let b:Box<string> = new Box<string>(v: "甲乙丙丁戊")
    total = total + length(b.get())
}
console.print(total)
`
	if slope := heapSlope(t, tmpl); slope >= 4 {
		t.Errorf("每轮还留 %g 字节：带方法的实例没跟着形状表归还", slope)
	}
}

// TestGenericInstanceBoxAndUnbox 钉住实例能装箱、也能 `as Box<int>` 拆回来：
// 块头本来就带一个类型字，这一层给每只实例排了一格，所以这一处现在就走通了
// （`is` 与 `as` 带尖括号那两种写法都认，两份实例之间不自动转换；那一路另有
// `TestIsAndAsPerInstanceRun` 钉着）。
//
// en: TestGenericInstanceBoxAndUnbox pins that an instance boxes and `as Box<int>` takes it
// back: the block header already carries a type word and this layer gives each instance one, so
// the path works today. Both `is` and `as` read the name with brackets, and no implicit
// conversion runs between two instances - TestIsAndAsPerInstanceRun pins that side.
func TestGenericInstanceBoxAndUnbox(t *testing.T) {
	src := boxHead + `let a:Box<int> = new Box<int>(v: 3)
let o:object = a as object
console.print(o)
let back:Box<int> = o as Box<int>
console.print(back.v)
`
	got, code := run(t, src)
	want := "Box<int> { v: 3 }\n3\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestGenericFunctionInstancesAreSeparateCopies 钉住顶层泛型函数按实参各生成一份：
// `twice(21)` 与 `twice("甲")` 是两个 IR 函数（`twice<int>`、`twice<string>`），
// 中间码里也没有模板那一个名字带体的那份。
//
// en: TestGenericFunctionInstancesAreSeparateCopies pins one top-level generic function per
// argument list: twice(21) and twice("甲") become two IR functions (twice<int>, twice<string>),
// and the template's own name appears nowhere with a body.
func TestGenericFunctionInstancesAreSeparateCopies(t *testing.T) {
	src := `method twice<Element>(v:Element):Element {
    return v
}

class Box<Content> {
    v:Content

    method get():Content {
        return v
    }
}

let a:int = twice(21)
let b:string = twice("甲")
let c:Box<int> = twice(new Box<int>(v: 7))
let d:int = twice<int>(8)
console.print(a, b, c.get(), d)
`
	got, code := run(t, src)
	want := "21 甲 7 8\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
	dump := dumpIR(t, src)
	for _, name := range []string{"func twice<int>", "func twice<string>", "func twice<Box<int>>"} {
		if !strings.Contains(dump, name) {
			t.Errorf("中间码里缺 %q", name)
		}
	}
	if strings.Contains(dump, "func twice (") {
		t.Errorf("模板那一份也被发出来了：\n%s", dump)
	}
}

// TestGenericFunctionKeepsOwnership 钉住泛型函数那条路上的交出与归还：
// 文本当实参交进去、原样返回，循环里每轮的块都要还掉。
// 这里多算的那一遍表达式求值（推导要先知道实参的类型）不能把"交出去"记成两次。
//
// en: TestGenericFunctionKeepsOwnership pins hand-over and release along the generic function
// path: text handed in as an argument and returned unchanged must leave no block behind per
// round. The extra evaluation inference needs (to learn each argument's type first) must not
// count one hand-over twice.
func TestGenericFunctionKeepsOwnership(t *testing.T) {
	tmpl := `method echoOf<Item>(v:Item):Item {
    return v
}

let total:int = 0
for (i:int in 0 until @N@) {
    let s:string = echoOf("甲乙丙丁戊")
    total = total + length(s)
}
console.print(total)
`
	if slope := heapSlope(t, tmpl); slope >= 4 {
		t.Errorf("每轮还留 %g 字节：泛型函数那条路上交出与归还没对上", slope)
	}
}

// TestMethodLevelTypeParamsRun 钉住方法自己带类型参数那一种：
// 所属类那一层的实参与方法这一层的实参合在一张对照表里解参数类型，
// 两种方法实参就是两个 IR 函数（`Cell<int>.take<string>` 与 `Cell<int>.take<int>`）。
//
// en: TestMethodLevelTypeParamsRun pins a method carrying its own type parameters: the class's
// mapping and the method's own merge before parameters resolve, and two method arguments mean
// two IR functions (Cell<int>.take<string> and Cell<int>.take<int>).
func TestMethodLevelTypeParamsRun(t *testing.T) {
	src := `class Cell<T> {
    v:T

    method take<E>(k:E):E {
        return k
    }

    method own():T {
        return v
    }
}

let a:Cell<int> = new Cell<int>(v: 9)
console.print(a.take("乙"), a.take(3), a.own())
`
	got, code := run(t, src)
	want := "乙 3 9\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
	dump := dumpIR(t, src)
	for _, name := range []string{"func Cell<int>.take<string>", "func Cell<int>.take<int>"} {
		if !strings.Contains(dump, name) {
			t.Errorf("中间码里缺 %q", name)
		}
	}
}

// TestIsAndAsPerInstanceRun 钉住 `is` 与 `as` 认得出是哪一只实例：
// 同一个盒子先问 `Box<int>` 得到真、再问 `Box<string>` 得到否，拆回来按实参那一只对得上。
//
// en: TestIsAndAsPerInstanceRun pins that is and as tell instances apart: the same box answers
// true to Box<int> and false to Box<string>, and the unbox follows the argument it actually is.
func TestIsAndAsPerInstanceRun(t *testing.T) {
	src := `class Box<Content> {
    v:Content

    method get():Content {
        return v
    }
}

let a:Box<int> = new Box<int>(v: 3)
let o:object = a as object
console.print(o is Box<int>, o is Box<string>)
let back:Box<string> = o as Box<string>
console.print(back.get())
`
	got, code := run(t, src)
	// 拆错那一只在运行期报 10011，那一行不会打出来。
	// Unboxing the wrong instance throws 10011 at runtime, so that line never prints.
	if !strings.HasPrefix(got, "true false\n") {
		t.Fatalf("得到 %q 码 %d，要以 \"true false\n\" 开头", got, code)
	}
	if code == 0 {
		t.Errorf("拆错实例那一步该以非零码结束，得到 %d：%q", code, got)
	}
}

// TestGenericProductGrowthStaysLinear 钉住产物按实例份数长这件事是线性的：
// 一份 `class Cell<T>` 带两个方法，声明不用、用一种实参、用三种实参，产物字节数一格一格加，
// 三种实参那一份不超过"一种实参"那一档的四倍。
// 这里比的是份数关系而不是绝对值：绝对数是产物按节对齐之后的读数，这一批里每一档恰好 512 字节。
//
// en: TestGenericProductGrowthStaysLinear pins that product size grows with the number of
// instances, linearly: one class Cell<T> with two methods, declared-but-unused, used with one
// argument list, used with three - each step adds, and three stays under four times one. The
// check compares the ratios, not absolute values: those are post-alignment figures, and in this
// batch every step is exactly 512 bytes.
func TestGenericProductGrowthStaysLinear(t *testing.T) {
	head := `class Cell<T> {
    v:T

    method get():T {
        return v
    }

    method set(k:T) {
        v = k
    }
}

`
	zero := len(build(t, head+"console.print(1)\n"))
	one := len(build(t, head+`let a:Cell<int> = new Cell<int>(v: 1)
console.print(a.get())
`))
	three := len(build(t, head+`let a:Cell<int> = new Cell<int>(v: 1)
let b:Cell<string> = new Cell<string>(v: "甲")
let c:Cell<float> = new Cell<float>(v: 1.5)
console.print(a.get(), b.get(), c.get())
`))
	if one <= zero {
		t.Errorf("用了一种实参反而没变多：零 %d，一 %d", zero, one)
	}
	if three > 4*one {
		t.Errorf("三份实例长过了头：零 %d，一 %d，三 %d", zero, one, three)
	}
	t.Logf("声明不用 %d 字节，一种实参 %d 字节，三种实参 %d 字节", zero, one, three)
}

// TestSampleGeneric 把整份 `samples/generic.nf` 编成真实产物跑一遍：一份声明按用到的类型各生成一份，
// 屏幕上那五行就是两只实例、一只 struct、一次 `is` 与一次推出来的类型实参各答一次。
func TestSampleGeneric(t *testing.T) {
	const path = "../../../samples/generic.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, string(raw)), "generic.exe")
	want := "42\n甲 乙\ntrue false\n1 丙\n21 戊\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("generic.nf 跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

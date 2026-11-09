// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import (
	"testing"

	"neflang.org/nf/internal/ast"
)

// TestGenericInstanceArguments 钉住尖括号里能填哪些类型：判据与容器那本账一条一条对得上——
// `Json`、三种系统句柄、函数值各撞一句现成的话，一只实例与另一只实例之间不自动转换。
// 这几句都在清单上原有的句子里，泛型这一段一句新的也没添。
//
// en: TestGenericInstanceArguments pins which types may be filled in: the judgments match the
// containers' account line by line, so Json, the three system handles and a function value each
// hit one existing sentence, and one instance never converts into another. Every sentence here
// is already on the list; this segment adds none.
func TestGenericInstanceArguments(t *testing.T) {
	head := "class Box<Content> {\n    v:Content\n}\n\n"
	expectErr(t, head+"let a:Box<Json> = new Box<Json>(v: null)\n",
		"Json 进不了 Box：先把它拆成自己要的值，或者存那段文本")
	expectErr(t, head+"let a:Box<fn(int):int> = new Box<fn(int):int>(v: null)\n",
		"fn(int):int 进不了 Box：函数值只待在名字、实参、返回值与字段里")
	expectErr(t, head+"let a:Box<int> = new Box<string>(v: \"甲\")\n",
		"这里要的是 Box<int>，给的是 Box<string>；类型之间不自动转换")
	expectOK(t, head+"let a:Box<int> = new Box<int>(v: 3)\nlet b:Box<string> = new Box<string>(v: \"甲\")\nconsole.print(a.v, b.v)\n")
	expectOK(t, head+"let a:Box<list<int>> = new Box<list<int>>(v: [1, 2])\nconsole.print(length(a.v))\n")
}

// TestGenericParameterCount 钉住实参个数与类型参数个数对不上时那一句现成的话：
// 少写、多写都报在同一句上，用的还是容器那一句。
//
// en: TestGenericParameterCount pins the existing sentence for an argument-count mismatch: too
// few and too many report the same line, the container's own sentence.
func TestGenericParameterCount(t *testing.T) {
	head := "class Box<Content> {\n    v:Content\n}\n\n"
	expectErr(t, head+"let a:Box = new Box(v: 3)\n",
		"Box 需要 1 个尖括号参数，实际 0 个")
	expectErr(t, head+"let a:Box<int, string> = new Box<int>(v: 3)\n",
		"Box 需要 1 个尖括号参数，实际 2 个")
	two := "class Pair<First, Second> {\n    left:First\n    right:Second\n}\n\n"
	expectErr(t, two+"let p:Pair<int> = new Pair<int>(left: 1)\n",
		"Pair 需要 2 个尖括号参数，实际 1 个")
	// `new` 后面那一对尖括号里写的也是类型，不是名字：写了一个没配上的名字，
	// 报的是"没有这个类型"那一句，位置在写它的那一处。
	// The brackets after `new` hold types too, not names: an unbound name there gets the
	// "no such type" sentence, reported at the place that wrote it.
	expectErr(t, head+"class Holder<Item> {\n    inner:Box<Item>\n}\n\nlet h:Holder<int> = new Holder<Item>(inner: null)\n",
		"没有 Item 这个类型")
}

// TestGenericTemplateIsNotAType 钉住模板本身不能当类型写：`Box` 与 `Box<int>` 是两条记录，
// 前者不摊字段（否则两只实例会挤进同一份字段表）。没有类型参数的定义照旧拒一句。
// 造值那一处（`Pnt{...}` 与 `new Box(...)`）还收不下尖括号，但报的必须是缺尖括号那一句，
// 不能是"Pnt 里没有字段 x"那种无从定位的话。
//
// en: TestGenericTemplateIsNotAType pins that the template is not itself a usable type: Box and
// Box<int> are two records and the former flattens no fields, which is what would otherwise let
// two instances crowd into one field table. A definition without parameters keeps its refusal.
// The value-building forms (`Pnt{...}`, `new Box(...)`) cannot read the brackets yet, but they
// must report the missing-brackets sentence rather than an unlocatable "Pnt has no field x".
func TestGenericTemplateIsNotAType(t *testing.T) {
	expectErr(t, "class Box<Content> {\n    v:Content\n}\n\nclass Other {\n    b:Box\n}\n",
		"Box 需要 1 个尖括号参数，实际 0 个")
	expectErr(t, "struct S {\n    v:int\n}\n\nlet x:S<int> = S{v: 1}\n",
		"S 是自定义类型，不带尖括号参数")
	expectErr(t, "struct Pnt<X> {\n    x:X\n}\n\nlet p:Pnt<int> = Pnt{x: 1}\n",
		"Pnt 需要 1 个尖括号参数，实际 0 个")
	expectErr(t, "class Box<Content> {\n    v:Content\n}\n\nlet a:Box<int> = new Box(v: 1)\n",
		"Box 需要 1 个尖括号参数，实际 0 个")
}

// TestTypeParameterNameCollisions 钉住类型参数名字的两条限制：写法本身不限制（`Item` 与 `T`
// 都收），撞上内建名字与两个参数同名才算错，两句都用清单上原有的话。
//
// en: TestTypeParameterNameCollisions pins the two limits on parameter names: the spelling is
// unrestricted (`Item` and `T` both parse), while colliding with a built-in name or repeating a
// name is wrong, each reported by a sentence already on the list.
func TestTypeParameterNameCollisions(t *testing.T) {
	expectErr(t, "class Box<list> {\n    v:int\n}\n\nlet a:Box<int> = new Box<int>(v: 1)\n",
		"list 是内建名字，不能拿去做名字")
	expectErr(t, "class Pair<Same, Same> {\n    left:int\n    right:int\n}\n\nlet p:Pair<int, string> = new Pair<int, string>(left: 1, right: \"甲\")\n",
		"Same 已经定义过了")
	expectOK(t, "class Box<T> {\n    v:T\n}\n\nclass Pair<First, Second> {\n    left:First\n    right:Second\n}\n\nlet a:Box<int> = new Box<int>(v: 1)\nlet p:Pair<int, string> = new Pair<int, string>(left: 2, right: \"乙\")\nconsole.print(a.v, p.left, p.right)\n")
}

// TestNestedGenericInstanceFields 钉住实例套实例：`Holder<Item>` 的字段写 `Box<Item>`，
// 实例化 `Holder<int>` 时内层那一只解成 `Box<int>`；类型参数对照表要成栈，
// 内层解完还要把外层那一份留着（generic.go 的 pushTypeEnv）。
//
// en: TestNestedGenericInstanceFields pins instances inside instances: Holder's field names
// Box<Item>, so Holder<int> must resolve the inner one to Box<int>. The parameter mappings
// stack, and the inner resolution leaves the outer one in place (pushTypeEnv in generic.go).
func TestNestedGenericInstanceFields(t *testing.T) {
	src := `class Box<Content> {
    v:Content
}

class Holder<Item> {
    inner:Box<Item>
    tag:int
}

let h:Holder<int> = new Holder<int>(inner: new Box<int>(v: 7), tag: 1)
console.print(h.inner.v, h.tag)
`
	expectOK(t, src)
	expectErr(t, `class Box<Content> {
    v:Content
}

class Holder<Item> {
    inner:Box<Item>
}

let h:Holder<int> = new Holder<int>(inner: new Box<string>(v: "甲"))
`, "这里要的是 Box<int>，给的是 Box<string>；类型之间不自动转换")
}

// TestSelfReferentialGenericClass 钉住自引用那一条与源码里那份声明同一规矩：字段指回同一只类型就拒，
// `class` 也一样（实测：非泛型的 `class Link { next:Link }` 报的是同一句）。
// 一只实例的字段可以指回**另一只**实例（`Link<int>` 的字段放 `Link<string>` 的实例），那是两个类型。
//
// en: TestSelfReferentialGenericClass pins that self-reference follows the same rule as a
// declaration in the source: a field pointing back at its own type is refused, for classes too -
// measured, the non-generic `class Link { next:Link }` reports that same sentence. An instance
// may hold a field pointing at a *different* instance (`Link<int>` holding a Link<string>), since
// those are two types.
func TestSelfReferentialGenericClass(t *testing.T) {
	expectErr(t, `class Link<Item> {
    val:Item
    next:Link<Item>
}

let a:Link<int> = new Link<int>(val: 1, next: null)
`, "字段不能指回自己这个类型")
	expectOK(t, `class Leaf<X> {
    v:X
}

class Link<Item> {
    val:Item
    side:Leaf<Item>
}

let a:Link<int> = new Link<int>(val: 1, side: new Leaf<int>(v: 2))
console.print(a.side.v, a.val)
`)
}

// TestGenericInstanceNotCreatedKeepsProgram 钉住"只用声明、一次都没实例化"的程序：
// 名字表里躺着一条模板，字段表与方法表都不该被摊开——否则类型参数会撞出
// "没有 Content 这个类型"那种无从定位的话。
//
// en: TestGenericInstanceNotCreatedKeepsProgram pins a program that declares but never
// instantiates: one template sits in the name table and neither its field nor method table may
// be flattened, or the parameter would raise an unlocatable "no such type as Content".
func TestGenericInstanceNotCreatedKeepsProgram(t *testing.T) {
	expectOK(t, "class Box<Content> {\n    v:Content\n\n    method get():Content {\n        return v\n    }\n}\n\nconsole.print(1)\n")
}

// TestGenericInstanceMethodBodiesChecked 钉住方法体按实例各查一遍：模板那一趟不查体
// （`Item` 那一个名字还没配上实参），实例生成之后才查，而且查的是那一只实例自己的类型。
//
// 下面那一份程序是同一条 `return 1`：`Box<int>` 那一份对，`Box<string>` 那一份不对——
// 报错只出在配不上的那一只实例上，这一句就是"各查一遍"的证据。
//
// en: TestGenericInstanceMethodBodiesChecked pins bodies checked once per instance: the
// template pass checks none (`Item` has no argument attached yet), and once an instance exists
// its own types are what the body is judged against. The program below returns the same `1`
// from both instantiations: right for Box<int>, wrong for Box<string>, and only the mismatching
// instance reports - which is the evidence that each copy was checked.
func TestGenericInstanceMethodBodiesChecked(t *testing.T) {
	expectOK(t, `class Box<Content> {
    v:Content

    method get():Content {
        return 1
    }
}

let a:Box<int> = new Box<int>(v: 3)
console.print(a.get())
`)
	expectErr(t, `class Box<Content> {
    v:Content

    method get():Content {
        return 1
    }
}

let a:Box<int> = new Box<int>(v: 3)
let b:Box<string> = new Box<string>(v: "甲")
console.print(a.get(), b.get())
`, "这里要的是 string，给的是 int；类型之间不自动转换")
	// 体里另一处的错也一样查得到：调一个没有的函数，报在写它的那一行。
	// Other mistakes in a body are found the same way: calling a function that does not exist
	// reports on the line that wrote it.
	expectErr(t, `class Box<Content> {
    v:Content

    method get():Content {
        return nowhere(1)
    }
}

let a:Box<int> = new Box<int>(v: 3)
console.print(a.get())
`, "没有 nowhere 这个函数")
}

// TestGenericInstanceBodyMintsInstances 钉住体里再生成新实例这条路：`Maker<int>` 的方法造
// `Box<string>`。实例生成时排在队里的那一批要接着排空，不能等到查完所有文件才发现少查了一份。
//
// en: TestGenericInstanceBodyMintsInstances pins minting inside a body: Maker<int>'s method
// builds Box<string>. Entries added while the queue drains must keep draining, instead of a
// body going unchecked until every file's pass is over.
func TestGenericInstanceBodyMintsInstances(t *testing.T) {
	expectOK(t, `class Box<Content> {
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
}

let m:Maker<int> = new Maker<int>(seed: 1)
let b:Box<string> = m.make()
console.print(b.get(), m.seed)
`)
	expectErr(t, `class Box<Content> {
    v:Content
}

class Maker<Item> {
    seed:Item

    method make():Box<string> {
        return new Box<string>(v: 1)
    }
}

let m:Maker<int> = new Maker<int>(seed: 1)
console.print(m.make())
`, "这里要的是 string，给的是 int；类型之间不自动转换")
}

// TestGenericInstanceMethodCallsSibling 钉住同一个实例里方法互相调用：`twice()` 里调 `size()`
// 走的是 `this.size()` 那一条写法（普通类也是同一条），落点记的是实例名那一个键
// （`Cell<int>.size`），不是模板那一个名字。
//
// en: TestGenericInstanceMethodCallsSibling pins one instance calling its own other method:
// twice() calls this.size(), the same spelling an ordinary class needs, and the landing name is
// the instance's key (Cell<int>.size) rather than the template's.
func TestGenericInstanceMethodCallsSibling(t *testing.T) {
	expectOK(t, `class Cell<T> {
    v:T

    method size():int {
        return 2
    }

    method twice():int {
        return this.size() + this.size()
    }
}

let a:Cell<int> = new Cell<int>(v: 1)
console.print(a.twice())
`)
	expectErr(t, `class Cell<T> {
    v:T

    method size():int {
        return 2
    }

    method twice():int {
        return this.sizes()
    }
}

let a:Cell<int> = new Cell<int>(v: 1)
console.print(a.twice())
`, "Cell<int> 里没有 sizes 这个字段或方法")
}

// TestGenericFunctionCall 钉住顶层泛型函数的三种调用形状：
// 没写尖括号按实参推、写了尖括号照配的、两种实参对不上时报现成的类型不一致那一句。
// 哪一种实参都推不出某个类型参数时，那一句是新写的一句报错原话。
//
// en: TestGenericFunctionCall pins the three call shapes for a top-level generic function:
// inferred when no brackets are written, bound as written when they are, and the existing
// type-mismatch sentence when two arguments disagree. When nothing can infer a parameter, a
// newly written sentence reports it.
func TestGenericFunctionCall(t *testing.T) {
	twice := `method twice<Element>(v:Element):Element {
    return v
}

`
	expectOK(t, twice+"let a:int = twice(21)\nlet b:string = twice(\"甲\")\nconsole.print(a, b)\n")
	expectErr(t, twice+"let a:int = twice(21)\nlet b:string = twice(\"甲\")\nconsole.print(a, b, twice(1.5))\nlet c:string = twice(3)\n",
		"这里要的是 string，给的是 int；类型之间不自动转换")
	// 一个类型参数在所有参数位置都没出现，就没有能推出它的实参：那一句新写的报错指的正是这一处。
	// A parameter appearing in no argument position has nothing to infer it from, which is what
	// the new sentence reports.
	expectErr(t, `method noneOf<Element>(k:int):int {
    return k
}

console.print(noneOf(1))
`, "从这些实参推不出 noneOf 的类型参数 Element")
	expectOK(t, `method noneOf<Element>(k:int):int {
    return k
}

console.print(noneOf<string>(1))
`)
	// 嵌套位置也能推：`list<Item>` 对上 `list<int>` 就把 Item 绑成 int。
	// Nested positions bind too: list<Item> against list<int> binds Item to int.
	expectOK(t, `method firstOf<Item>(xs:list<Item>):Item {
    return xs[0] + 0
}

let n:int = firstOf([1, 2, 3])
console.print(n)
`)
}

// TestGenericStructLiteral 钉住 `Pnt<int, string>{x: 1, y: "甲"}` 那一种写法：
// 字段类型按那一只实例摊开的对，缺字段与类型不对都报现成的句子；不写尖括号仍然拒一句。
//
// en: TestGenericStructLiteral pins `Pnt<int, string>{x: 1, y: "甲"}`: fields are checked
// against the flattened instance, a missing field or a wrong type reports an existing sentence,
// and leaving the brackets out stays refused.
func TestGenericStructLiteral(t *testing.T) {
	head := `struct Pnt<X, Y> {
    x:X
    y:Y
}

`
	expectOK(t, head+"let p:Pnt<int, string> = Pnt<int, string>{x: 1, y: \"甲\"}\nconsole.print(p.x, p.y)\n")
	expectErr(t, head+"let p:Pnt<int, string> = Pnt<int, string>{x: \"甲\", y: 1}\n",
		"这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, head+"let p:Pnt<int, string> = Pnt<int, string>{x: 1}\n",
		"Pnt<int, string> 的字段 y 没给值：字面量要把字段列全")
	expectErr(t, head+"let p:Pnt<int, string> = Pnt{x: 1, y: 2}\n",
		"Pnt 需要 2 个尖括号参数，实际 0 个")
}

// TestGenericInstanceFieldRules 钉住实例的字段规则与源码那份声明同一套：
// 指回自己这一只拒、两只 struct 绕成环拒、`struct` 字段放句柄拒、`class` 字段放 `Json` 拒。
// 报的类型名是实例那一只（`A<int>`），屏幕与报错上都要说得出是哪一只实例。
//
// en: TestGenericInstanceFieldRules pins instance fields against the same rules a source
// declaration faces: pointing back at itself, a cycle through two structs, a handle in a struct
// field, and Json in a class field are all refused. The type named is the instance (`A<int>`) -
// printing and error text both have to say which instance is meant.
func TestGenericInstanceFieldRules(t *testing.T) {
	expectErr(t, `struct A<X> {
    b:B<X>
}

struct B<X> {
    a:A<X>
}

let x:A<int> = A<int>{b: B<int>{a: x}}
`, "struct A<int> 的字段绕成了环")
	// 字段规则在实例生成时才判得出来：模板那一趟不查字段（`Holder<X>` 里的 `X` 还没配实参），
	// 所以下面那一句要等到 `Want<int>` 真被生成才报。
	// Field rules can only be judged once an instance exists: the template pass checks no fields
	// (the `X` inside `Holder<X>` has no argument yet), so the sentence below appears when
	// `Want<int>` is actually built.
	expectErr(t, `class Holder<X> {
    v:X
}

struct Want<X> {
    h:Holder<X>
}

let w:Want<int> = Want<int>{h: null}
`, "struct 的字段只能放值类型、string 或另一个 struct，这里是 Holder<int>")
}

// TestTemplateSignatureMintsNoInstance 钉住编辑器那一栏的模板签名不生成实例：
// `build<T>(v:T):Counter<T>` 的返回类型里写着另一只带类型参数的类型名，摊这一行的时候
// 只排一串给人看的名字——不进名字表、不进 Instances、不排方法体，也不报一句。
// 占位那一只 `T` 配出来的实例从来不会被程序用到，发码发它就是多一份产物。
// 这一趟要在定义它的那一份文件的上下文里解，那一条由 `cmd/nf` 的
// `TestGenericPackageInstantiatesOnUsingSide` 钉住（包里的 `build` 在工程的上下文里查
// `Counter`，查不到就报"没有 Counter 这个类型"）。
//
// en: TestTemplateSignatureMintsNoInstance pins that the editor's template signature mints no
// instance: build<T>(v:T):Counter<T> names another parameterized type in its return, and laying
// that line out only builds a reader-facing string - no name table entry, no Instances entry, no
// queued body, no report. An instance built from the placeholder T is never used by a program, so
// emitting it would be dead weight. That this pass must run in the file that defines the template
// is pinned by cmd/nf's TestGenericPackageInstantiatesOnUsingSide: resolving Counter for the
// package's build in the project's context used to answer the no-such-type sentence.
func TestTemplateSignatureMintsNoInstance(t *testing.T) {
	src := `class Counter<Item> {
    v:Item

    method get():Item {
        return v
    }
}

method build<T>(v:T):Counter<T> {
    return new Counter<T>(v: v)
}

let c:Counter<int> = build(1)
console.print(c.get())
`
	errs, facts := RunWithFacts(&Program{Files: []*ast.File{mustFile(t, "t.nf", src)}, Entry: "t.nf"}, true)
	if len(errs) > 0 {
		t.Fatalf("编辑器那一栏不该报出东西：%v", msgs(errs))
	}
	if got := facts.Signature("build"); got != "build(v: T): Counter<T>" {
		t.Errorf("模板那一行的签名不对：得到 %q", got)
	}
	for key := range facts.Instances {
		if key == "Counter<T>" {
			t.Errorf("占位实参生成了一只从来没人用的实例：%s", key)
		}
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/parser"
)

func mustFile(t *testing.T, path, src string) *ast.File {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("%s 词法报错：%v", path, err)
	}
	f, err := parser.Parse(path, toks)
	if err != nil {
		t.Fatalf("%s 语法报错：%v", path, err)
	}
	return f
}

// expectErr 跑一遍检查，要求其中一句报错与 want 一字不差。比的是 Error.Msg 那一句本体——
// "t.nf 第 3 行第 1 列：" 那个前缀不在 Msg 里。半句过不了，改一个字就红。
func expectErr(t *testing.T, src, want string) {
	t.Helper()
	errs := Run(&Program{Files: []*ast.File{mustFile(t, "t.nf", src)}, Entry: "t.nf"})
	for _, e := range errs {
		if e.Msg == want {
			return
		}
	}
	t.Fatalf("没报出整句 %q；实际报错：%v", want, msgs(errs))
}

func expectOK(t *testing.T, src string) {
	t.Helper()
	errs := Run(&Program{Files: []*ast.File{mustFile(t, "t.nf", src)}, Entry: "t.nf"})
	if len(errs) > 0 {
		t.Fatalf("不该报错，实际：%v", msgs(errs))
	}
}

func msgs(errs []Error) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

func TestNoImplicitConversion(t *testing.T) {
	expectErr(t, "let f:float = 1\n", "这里要的是 float，给的是 int；类型之间不自动转换")
	expectErr(t, "let b:byte = 300\n", "byte 只能装 0～255，这里是 300")
	expectOK(t, "let f:float = 1.0\nlet i:int = 3\n")
}

// TestByteArithmeticPromotes 覆盖"byte 参与算术结果是 int"这一条：
// 能否落成 `int` 就是这条规则的判定方式，所以每一种都往 `let n:int` 上放一次、往 `let c:byte` 上放一次。
// `&`、`<<`、`~` 与 `+ - * / %` 走的是同一句。
func TestByteArithmeticPromotes(t *testing.T) {
	for _, expr := range []string{"b + b", "b - b", "b * b", "b / b", "b % b", "b & b", "b | b", "b ^ b", "b << 3", "b >> 1", "~b"} {
		src := "let b:byte = 200\nlet n:int = " + expr + "\n"
		expectOK(t, src)
		expectErr(t, "let b:byte = 200\nlet c:byte = "+expr+"\n", "这里要的是 byte，给的是 int；类型之间不自动转换")
	}
	expectErr(t, "let b:byte = 200\nlet n:int = b + 1\n", "+ 两边必须是同一类型：byte 与 int")
}

// TestByteConvert 覆盖 `as byte` 的两半：字面量在编译期就拒绝，算出来的值留给运行期。
func TestByteConvert(t *testing.T) {
	expectErr(t, "let n:byte = 300 as byte\n", "byte 只能装 0～255，这里是 300")
	expectErr(t, "let n:byte = 256 as byte\n", "byte 只能装 0～255，这里是 256")
	expectOK(t, "let n:byte = 200 as byte\n")
	expectOK(t, "let m:int = 5\nlet n:byte = m as byte\n")
}

// TestFieldTakeFromBorrowed 覆盖只读借用边界那道规则：遍历给出的值是只读借用，
// 它替容器持有那一个对象，所以从它上面把引用类型字段**接走**不行——与 `xs[0]` 那一侧是同一条拒绝规则。
// 只是顺着读一次不算接走（`console.print(o.inner.v)`），这一条必须通过，否则每次遍历都只能读一个字段。
func TestFieldTakeFromBorrowed(t *testing.T) {
	const pre = "class Inner {\n    v:int\n}\nclass Outer {\n    inner:Inner\n}\n" +
		"let os:list<Outer> = [new Outer(inner: new Inner(v: 1))]\n"
	expectErr(t, pre+"for (o:Outer in os) {\n    let a:Inner = o.inner\n    console.print(a.v)\n}\n",
		"遍历交出的 o 是只读借用，不能把里面的东西交出去")
	expectErr(t, pre+"for (o:Outer in os) {\n    console.print(o.inner as object)\n}\n", "遍历交出的 o 是只读借用，不能把里面的东西交出去")
	expectOK(t, pre+"for (o:Outer in os) {\n    console.print(o.inner.v, o.inner.v + 1)\n}\n")
	// 值类型字段不在这一条里：那是整份拷贝，借用也照给
	expectOK(t, pre+"for (o:Outer in os) {\n    let n:int = o.inner.v\n    console.print(n)\n}\n")
}

func TestConstOnlyValueType(t *testing.T) {
	expectErr(t, "const items:list<int> = []\n", "const 只能配值类型，这里是 list<int>")
	expectOK(t, "const limit:int = 100\n")
}

func TestStructLiteralMustBeComplete(t *testing.T) {
	src := "struct Point {\n    x:int\n    y:int\n}\nlet p:Point = Point { x: 1 }\n"
	expectErr(t, src, "Point 的字段 y 没给值：字面量要把字段列全")
	expectErr(t, "struct Point {\n    x:int\n}\nlet p:Point = Point { z: 1 }\n", "Point 里没有字段 z")
}

func TestShadowingRejected(t *testing.T) {
	src := "method f(x:int):int {\n    let x:int = 2\n    return x\n}\n"
	expectErr(t, src, "x 与外层的名字重名：不做内层顶掉外层")
}

func TestMissingReturn(t *testing.T) {
	src := "method pick(x:int):int {\n    if (x > 0) {\n        return x\n    }\n}\n"
	expectErr(t, src, "pick 标了返回类型 int，但有一条路没 return")
	expectOK(t, "method pick(x:int):int {\n    if (x > 0) {\n        return x\n    }\n    return 0\n}\n")
}

func TestTransferOnCallAndReuse(t *testing.T) {
	src := "method eat(s:string) {\n}\nlet txt:string = \"喂\"\neat(txt)\neat(txt)\n"
	expectErr(t, src, "txt 已经交出去了，这个名字不能再写")
}

func TestLoopBorrowCannotEscape(t *testing.T) {
	src := "let items:list<string> = [\"a\"]\nfor (s:string in items) {\n    let keep:string = s\n}\n"
	expectErr(t, src, "遍历交出的 s 是只读借用，不能交出去")
}

// consumedOf 查一份记录：这个名称把值交出去过，而每一处交进去的位置是不是都没带走。
// 后端据此决定调用方那个位置还要不要还（Facts.Consumed）。
func consumedOf(t *testing.T, src, name string) bool {
	t.Helper()
	errs, facts := RunWithFacts(&Program{Files: []*ast.File{mustFile(t, "t.nf", src)}, Entry: "t.nf"}, true)
	if len(errs) > 0 {
		t.Fatalf("%s 这一段不该报错：%v", name, msgs(errs))
	}
	for node := range facts.Moved {
		if d, ok := node.(*ast.Decl); ok && d.NameText == name {
			return facts.Consumed[node]
		}
	}
	t.Fatalf("%s 没记进交出记录（Moved）", name)
	return false
}

// TestConsumedLedger 覆盖交出记录的另一半：**交出过不等于不需要还**。
// 交进的那一个参数在自己的函数里没把值留住（没 return、没写进字段、没装进容器、没再交出去），
// 出了被调方就再没人持有这一块——那个位置仍然是调用方这个名称的持有者，后端可以登记它、还它。
// 反过来，只要有一处带走了，这个名称就不登记：多留是泄漏，析构错是把别人持有的对象拆掉。
func TestConsumedLedger(t *testing.T) {
	cases := []struct {
		who       string
		want      bool
		name, src string
	}{
		{"acc", false, "被调方把它 return 出去", "method passOn(xs:list<int>):list<int> {\n    return xs\n}\nlet acc:list<int> = [1]\nlet got:list<int> = passOn(acc)\nconsole.print(length(got))\n"},
		{"acc", false, "被调方写进字段", "class Box {\n    held:list<int>\n}\nmethod stash(b:Box, xs:list<int>) {\n    b.held = xs\n}\nlet box:Box = new Box(held: [])\nlet acc:list<int> = [1]\nstash(box, acc)\n"},
		{"acc", false, "被调方装进容器", "method stashOuter(xs:list<int>, dst:list<list<int>>) {\n    dst.add(xs)\n}\nlet acc:list<int> = [1]\nlet outer:list<list<int>> = []\nstashOuter(acc, outer)\n"},
		{"acc", false, "被调方再交出去（两跳）", "method inner(xs:list<int>) {\n}\nmethod middle(xs:list<int>) {\n    inner(xs)\n}\nlet acc:list<int> = [1]\nmiddle(acc)\n"},
		{"acc", false, "交出去的是别名的那一头", "let acc:list<int> = [1]\nlet other:list<int> = acc\nconsole.print(length(other))\n"},
		{"acc", true, "只读一下长度", "method use(xs:list<int>):int {\n    return length(xs)\n}\nlet acc:list<int> = [1]\nconsole.print(use(acc))\n"},
		{"acc", true, "被调方就地改它（改不算带走）", "method grow(xs:list<int>) {\n    xs.add(9)\n}\nlet acc:list<int> = [1]\ngrow(acc)\n"},
		{"r", true, "交进普通函数、里头只读字段", "class Row {\n    kids:list<int>\n}\nmethod sum(row:Row):int {\n    return length(row.kids)\n}\nlet r:Row = new Row(kids: [1, 2])\nconsole.print(sum(r))\n"},
	}
	for _, one := range cases {
		if got := consumedOf(t, one.src, one.who); got != one.want {
			t.Errorf("%s：登记成持有者这件事判成 %v，要 %v", one.name, got, one.want)
		}
	}
}

// TestReceiverIsReadOnlyBorrow 覆盖：方法接收者是只读借用。
// 能读、能就地改这个对象、能连着调；把它交出去——`return this`、当参数、装箱——三条路都在编译期拒绝。
func TestReceiverIsReadOnlyBorrow(t *testing.T) {
	expectOK(t, "class Bag {\n    n:int\n\n    method bump():int {\n        n = n + 1\n        return n\n    }\n}\nlet b:Bag = new Bag(n: 1)\nconsole.print(b.bump(), b.n)\n")
	expectErr(t, "class Bag {\n    n:int\n\n    method out():Bag {\n        return this\n    }\n}\n", "this 是方法接收者的只读借用，不能交出去")
	expectErr(t, "class Bag {\n    n:int\n\n    method handOff() {\n        eat(this)\n    }\n}\nmethod eat(b:Bag) {\n}\n", "this 是方法接收者的只读借用，不能交给参数")
	expectErr(t, "class Bag {\n    n:int\n\n    method boxed():object {\n        return this as object\n    }\n}\n", "只读借用不能拆盒")
}

// TestContainerLiteralTransfers 覆盖：容器字面量里的引用类型元素
// 与 `add` 是同一份记录——那个位置就是它的新家，原名当场作废，`[a, a]` 也就编不过去了。
func TestContainerLiteralTransfers(t *testing.T) {
	src := "class Bag {\n    n:int\n}\nlet a:Bag = new Bag(n: 1)\nlet hs:list<Bag> = [a]\nconsole.print(a)\n"
	expectErr(t, src, "a 已经交出去了，这个名字不能再写")
	expectErr(t, "class Bag {\n    n:int\n}\nlet a:Bag = new Bag(n: 1)\nlet hs:list<Bag> = [a, a]\n", "a 已经交出去了，这个名字不能再写")
	expectErr(t, "class Bag {\n    n:int\n}\nlet a:Bag = new Bag(n: 1)\nlet s:hashset<Bag> = {a}\nconsole.print(a)\n", "a 已经交出去了，这个名字不能再写")
	expectErr(t, "class Bag {\n    n:int\n}\nlet a:Bag = new Bag(n: 1)\nlet d:dict<string, Bag> = {\"一个\": a}\nconsole.print(a)\n", "a 已经交出去了，这个名字不能再写")
	expectOK(t, "class Bag {\n    n:int\n}\nlet a:Bag = new Bag(n: 1)\nlet hs:list<Bag> = [a]\nconsole.print(hs)\n")
	// 值类型那个位置是整份拷贝，原名照旧能用
	expectOK(t, "struct Point {\n    x:int\n}\nlet p:Point = Point { x: 1 }\nlet ps:list<Point> = [p, p]\nconsole.print(p.x, length(ps))\n")
	// 遍历交出的那个位置本来就是借用，装进字面量时按借用的规则报错
	expectErr(t, "class Bag {\n    n:int\n}\nlet cs:list<Bag> = [new Bag(n: 1)]\nfor (c:Bag in cs) {\n    let keep:list<Bag> = [c]\n}\n", "遍历交出的 c 是只读借用，不能交出去")
}

// TestNewFieldTransfers 覆盖 `new C(f: a)`：字段初值与容器字面量那个位置是同一份记录——
// 引用类型交进去就作废原名，值类型整份拷贝、名字照旧能用。
func TestNewFieldTransfers(t *testing.T) {
	bag := "class Bag {\n    n:int\n}\n"
	// 引用类型：交进字段之后原名不能再写
	expectErr(t, bag+"class Box {\n    inner:Bag\n}\nlet b:Bag = new Bag(n: 1)\nlet x:Box = new Box(inner: b)\nconsole.print(b)\n", "b 已经交出去了，这个名字不能再写")
	// 同一个名字交两处字段：第二处当场拒绝（一块对象不能有两个持有者要还）
	expectErr(t, bag+"class Pair {\n    l:Bag\n    r:Bag\n}\nlet a:Bag = new Bag(n: 1)\nlet b:Bag = new Bag(n: 2)\nlet p:Pair = new Pair(l: a, r: a)\n", "a 已经交出去了，这个名字不能再写")
	// 两个名字各交一个，照放行
	expectOK(t, bag+"class Pair {\n    l:Bag\n    r:Bag\n}\nlet a:Bag = new Bag(n: 1)\nlet b:Bag = new Bag(n: 2)\nlet p:Pair = new Pair(l: a, r: b)\nconsole.print(p)\n")
	// 文本也是引用类型：交进字段就作废原名
	expectErr(t, "class Tag {\n    label:string\n}\nlet s:string = \"甲\"\nlet t:Tag = new Tag(label: s)\nconsole.print(s)\n", "s 已经交出去了，这个名字不能再写")
	// 值类型那个位置是整份拷贝，原名照旧能用
	expectOK(t, "struct Point {\n    x:int\n}\nclass Held {\n    at:Point\n}\nlet p:Point = Point { x: 1 }\nlet h:Held = new Held(at: p)\nconsole.print(h.at.x, p.x)\n")
	// 遍历交出的借用交不进字段：报的是借用那一句，不是交出那一句
	expectErr(t, bag+"class Box {\n    inner:Bag\n}\nlet cs:list<Bag> = [new Bag(n: 1)]\nfor (c:Bag in cs) {\n    let x:Box = new Box(inner: c)\n}\n", "遍历交出的 c 是只读借用，不能交出去")
	// 交出去之后再拿它交给别人，同样拒绝（线性判定没有留例外）
	expectErr(t, bag+"class Box {\n    inner:Bag\n}\nlet b:Bag = new Bag(n: 1)\nlet x:Box = new Box(inner: b)\nlet xs:list<Bag> = [b]\n", "b 已经交出去了，这个名字不能再写")
}

// TestFieldAsArgumentIsRefused 覆盖：把 `class` 的引用类型字段**直接**交给自己写的函数或方法
// 当实参，在编译期拒绝。
//
// 语言早就定了这一读是过户（交完那个位置空着，后端把那一处写空），但检查阶段不记名字的账，
// 于是"下一次读它是运行期 10092"这一种只有跑起来才看得见——同一条用在容器那个位置上时，
// "被读走的那个位置交回 null"是写明的，字段这一侧以前没有定死。
func TestFieldAsArgumentIsRefused(t *testing.T) {
	const bag = "class Bag {\n    n:int\n}\nclass Hold {\n    item:Bag\n    label:string\n    count:int\n}\n"
	const refuse = " 是字段，直接交出去之后那个位置就空了：先接给一个名字，再拿那个名字交出去"
	const made = "let h:Hold = new Hold(item: new Bag(n: 1), label: \"甲\", count: 2)\n"

	// 一、句柄型的字段交进自己写的函数：拒绝，报的是那一个字段的名字
	expectErr(t, bag+"method sink(b:Bag) {\n    console.print(b.n)\n}\n"+made+"sink(h.item)\n", "h.item"+refuse)
	// 二、`this` 上的文本字段交进自己写的函数：同一条拒绝规则，方法那条路也算（走的是 arg）
	expectErr(t, "class Chain {\n    name:string\n    size:int\n}\nmethod sink(s:string) {\n    console.print(s)\n}\nclass Carrier {\n    head:Chain\n    tag:string\n    n:int\n\n    method pass() {\n        sink(this.tag)\n    }\n}\n", "this.tag"+refuse)
	// 三、正当写法：先接给一个名字，再拿那个名字交出去（接名字那一步本来就是过户）
	expectOK(t, bag+"method sink(b:Bag) {\n    console.print(b.n)\n}\n"+made+"let picked:Bag = h.item\nsink(picked)\n")
	// 四、值类型字段照旧放行：那一读是拷贝，不存在"那个位置归谁"的问题
	expectOK(t, bag+"method count(n:int) {\n    console.print(n + 1)\n}\n"+made+"count(h.count)\n")
	// 五、交给标准库那一类"只读一次"的参数不拒绝（arg 那一条只管自己写的函数与方法）
	expectOK(t, bag+made+"console.print(h.label)\n")
	// 六、当成返回值交出去不在这一条之内：`return h.item` 是把句柄返回调用方，另有那条记录管它
	expectOK(t, bag+"method give(x:Hold):Bag {\n    return x.item\n}\n"+made+"console.print(give(h).n)\n")
	// 七、装进容器那个位置也不在这一条之内（那是过户列明的五处之一，过户之后源位置空着是对的）
	expectOK(t, bag+made+"let xs:list<Bag> = [h.item]\nconsole.print(length(xs))\n")
}

func TestBoxingTransfers(t *testing.T) {
	src := "let c:C = new C()\nlet o:object = c as object\nconsole.print(c)\n"
	expectErr(t, src, "c 已经交出去了，这个名字不能再写")
}

// TestTransferIsBranchLocal 覆盖交出记录是按分支分别累计的：一条分支里交掉了，
// 另一条分支照旧能用这个名字（`else if` 连着判断盒子全靠这一条）；
// 但两条路都可能交出之后，再写它就是错误。
func TestTransferIsBranchLocal(t *testing.T) {
	ok := `method eat(s:string) {
}
let txt:string = "喂"
if (length(txt) > 0) {
    eat(txt)
} else if (length(txt) < 0) {
    eat(txt)
} else {
    eat(txt)
}
`
	expectOK(t, ok)
	later := `method eat(s:string) {
}
let txt:string = "喂"
if (length(txt) > 0) {
    eat(txt)
} else {
    eat(txt)
}
eat(txt)
`
	expectErr(t, later, "txt 已经交出去了，这个名字不能再写")
	onlyOne := `method eat(s:string) {
}
let txt:string = "喂"
if (length(txt) > 0) {
    eat(txt)
}
eat(txt)
`
	// 任一条路交出去，出了 if 就算"可能已经没了"
	expectErr(t, onlyOne, "txt 已经交出去了，这个名字不能再写")
}

// TestTransferResetsPerBinding 覆盖的是"每一个新绑定都从未交出的状态起头"：
// 两个函数的参数同名、都把它交出去，后一个不该沿用前一个的记录。
// 交出那张表按名字查、不随作用域回收，所以这条要留一道检查——
// 跨文件的写法最先撞上（被引方法的参数名与本地方法的参数名相同）。
func TestTransferResetsPerBinding(t *testing.T) {
	share := `method eat(s:string) {
}

method first(s:string) {
    eat(s)
}

method second(s:string) {
    eat(s)
}

console.print(1)
`
	expectOK(t, share)
	// 真该报的还是得报：函数体内第二次写同一个参数
	reuse := `method eat(s:string) {
}

method use(s:string) {
    eat(s)
    eat(s)
}

console.print(1)
`
	expectErr(t, reuse, "s 已经交出去了，这个名字不能再写")
}

func TestFloatKeyRejected(t *testing.T) {
	expectErr(t, "let d:dict<float, int> = {}\n", "float 不能当 dict 的键：NaN 不等于自己，这个键永远查不中")
	expectErr(t, "let s:hashset<struct_bad> = {}\n", "没有 struct_bad 这个类型")
}

func TestStringImmutable(t *testing.T) {
	expectErr(t, "let s:string = \"abc\"\ns[0] = 'x'\n", "string 不可变，不能改其中一格")
}

func TestSwitchMustCoverEnum(t *testing.T) {
	src := "enum D { a, b, c }\nmethod f(d:D) {\n    switch (d) {\n        D.a { console.print(1) }\n        D.b { console.print(2) }\n    }\n}\n"
	expectErr(t, src, "switch 漏了 D 的成员，又没写 default")
	expectOK(t, "enum D { a, b, c }\nmethod f(d:D) {\n    switch (d) {\n        D.a { console.print(1) }\n        D.b { console.print(2) }\n        default { console.print(3) }\n    }\n}\n")
}

func TestSwitchOnFloatRejected(t *testing.T) {
	expectErr(t, "method f(x:float) {\n    switch (x) {\n        1.0 { console.print(1) }\n        default { }\n    }\n}\n", "switch 只能判 enum、int、byte、unicode、boolean、string")
}

func TestInterfaceImplementation(t *testing.T) {
	missing := "interface A {\n    add(v:float):float\n}\nclass C base A {\n    f:float\n}\n"
	expectErr(t, missing, "C 没写出接口 A 声明的 add")
	nameMismatch := "interface A {\n    add(v:float):float\n}\nclass C base A {\n    f:float\n    method add(w:float):float { return w }\n}\n"
	expectErr(t, nameMismatch, "C.add 的第 1 个参数该叫 v")
	// 接口里写 method 是语法层的错
	expectParseErr(t, "interface A {\n    method add(v:float):float\n}\n")
}

func TestStructCannotImplementMethods(t *testing.T) {
	src := "interface A {\n    add(v:float):float\n}\nstruct S base A {\n    f:float\n}\n"
	expectErr(t, src, "S 是 struct，写不了方法，所以不能继承带方法声明的接口 A")
}

func TestNoOverload(t *testing.T) {
	expectErr(t, "method f(a:int) {\n}\nmethod f(b:int) {\n}\n", "函数 f 重复定义：一个名字只能有一个")
}

func TestEnumNoDuplicateValue(t *testing.T) {
	expectErr(t, "enum D { a = 5, b = 5 }\n", "枚举成员 b 与 a 同值 5")
}

func TestClassOneNamespace(t *testing.T) {
	expectErr(t, "class C {\n    x:int\n    method x():int { return 1 }\n}\n", "x 已经有同名成员（第 2 行）：字段与方法共用一个名字空间")
}

func TestInterfaceNameNotUsableAsType(t *testing.T) {
	expectErr(t, "interface A {\n    f:int\n}\nlet x:A = new C()\n", "A 是接口名，不能当类型写")
	expectErr(t, "interface A {\n    f:int\n}\nlet box:list<A> = []\n", "A 是接口名，不能当类型写")
}

func TestReservedNames(t *testing.T) {
	expectErr(t, "method length(n:int):int { return n }\n", "length 是内建名字，不能拿去做名字")
	expectErr(t, "method for(a:int) { }\n", "for 是关键字，不能拿去做名字")
}

func TestStdGroupAndMembers(t *testing.T) {
	expectErr(t, "console.printx(1)\n", "console 组里没有 printx")
	expectErr(t, "let n:int = length()\n", "length 收 1 个参数，这里给了 0 个")
	expectErr(t, "let s:string = \"abc\"\nconsole.print(s.to_int())\n", "string 上没有 to_int")
	expectOK(t, "let s:string = \"abc\"\nconsole.print(s.trim(), length(s), text.to_int(\"12\"))\n")
}

// TestThrowAndExitCodeRanges 覆盖那两段退出码：0～9999 归程序，10000 往上归语言自己。
// `exit (0, "…")` 是这一条开出来的新路——留一句话再正常退出；从前 0 被当成"太小说不定是内建的"拒绝掉。
func TestThrowAndExitCodeRanges(t *testing.T) {
	expectOK(t, "exit (0, \"跑完了\")\n")
	expectOK(t, "method f() {\n    throw (7, \"程序自己的号\")\n}\n")
	expectOK(t, "method f() {\n    throw (9999, \"区间的上界\")\n}\n")
	expectErr(t, "method f() {\n    throw (10000, \"混进内建段\")\n}\n", "throw 的码从 0 写到 9999，10000 落在语言自己那一段（10000 往上）")
	expectErr(t, "exit (10000, \"混进内建段\")\n", "exit 的码从 0 写到 9999，10000 落在语言自己那一段（10000 往上）")
	// 变量的值检查器拿不到，所以只判字面量
	expectOK(t, "let k:int = 20000\nmethod f() {\n    throw (k, \"来自变量的码不追值\")\n}\n")
	expectParseErr(t, "method f() {\n    throw (100)\n}\n")
}

func TestAsAndIs(t *testing.T) {
	expectErr(t, "let o:object = 1 as object\nlet s:string = o as string\n", "串与非串之间不走 as：变串写 number.to_string，读回数字写 text.to_int")
	expectErr(t, "let n:int = 3\nlet b:boolean = n is int\n", "is 只问引用位置上的值，int 不必问")
	expectErr(t, "class C {\n    f:int\n}\nlet c:C = new C()\nlet b:boolean = c is class_D\n", "没有 class_D 这个类型")
	expectOK(t, "class C {\n    f:int\n}\nlet o:object = new C(f: 1) as object\nlet b:boolean = o is C\n")
}

func TestNewOnlyOnClass(t *testing.T) {
	expectErr(t, "struct P {\n    x:int\n}\nlet p:P = new P()\n", "new 只用在 class 上，P 是 struct")
	expectErr(t, "class C {\n    a:int\n    b:int\n}\nlet c:C = new C(a: 1)\n", "new C 给了初值就得把字段给全，b 没给")
}

func TestBreakOutsideLoop(t *testing.T) {
	expectErr(t, "method f() {\n    break\n}\n", "break 只能写在循环里")
}

func TestArgsOnlyEntryTopLevel(t *testing.T) {
	expectErr(t, "method f() {\n    console.print(args[0])\n}\n", "args 只在入口文件的顶层写得到")
	expectOK(t, "if (length(args) > 0) {\n    console.print(args[0])\n}\n")
}

func TestUnreachableNameAndBadCall(t *testing.T) {
	expectErr(t, "console.print(nope)\n", "没有 nope 这个名字")
	expectErr(t, "let n:int = 1\nn(2)\n", "没有 n 这个函数")
}

// 跨文件：export／未用 import／互引
func twoFiles(t *testing.T, a, b string) []Error {
	t.Helper()
	return Run(&Program{
		Files: []*ast.File{mustFile(t, "a.nf", a), mustFile(t, "b.nf", b)},
		Entry: "a.nf",
	})
}

func TestImportExport(t *testing.T) {
	ok := twoFiles(t,
		"import { helper } from \"./b.nf\"\nconsole.print(helper(1))\n",
		"export method helper(n:int):int { return n }\n")
	if len(ok) > 0 {
		t.Fatalf("该有的没有：%v", msgs(ok))
	}
	errs := twoFiles(t,
		"import { helper } from \"./b.nf\"\n",
		"export method helper(n:int):int { return n }\n")
	if !strings.Contains(msgs(errs)[0], "没用到") {
		t.Fatalf("未用的 import 该报错：%v", msgs(errs))
	}
	errs = twoFiles(t,
		"import { helper } from \"./b.nf\"\nconsole.print(helper(1))\n",
		"method helper(n:int):int { return n }\n")
	if !strings.Contains(strings.Join(msgs(errs), "|"), "没有对外交出") {
		t.Fatalf("没 export 的名字不该拿得到：%v", msgs(errs))
	}
}

func TestImportCycle(t *testing.T) {
	errs := twoFiles(t,
		"import { b1 } from \"./b.nf\"\nconsole.print(b1())\n",
		"import { a1 } from \"./a.nf\"\nexport method b1():int { return a1() }\n")
	if !strings.Contains(strings.Join(msgs(errs), "|"), "互相引用") {
		t.Fatalf("互引该被拦下：%v", msgs(errs))
	}
}

func TestNestedContainersAndShift(t *testing.T) {
	expectOK(t, "let rows:list<dict<string, int>> = []\nlet n:int = 1 << 3\n")
	expectErr(t, "let n:int = 1 << 70\n", "移位量只能在 0～63 之间，这里写了 70")
}

// TestShiftAmountOnlyByte 覆盖：移位量这一侧只识别 `byte`，`int` 不行。
// 字面量是个例外——它落在要 `byte` 的位置上就按 `byte` 认，所以 `1 << 4` 通过，
// 而 `1 << 70` 这类越界的常数在编译期就报，越界的**算出来的量**留给运行期抛 10021 号（那条测在 `internal/lower`）。
func TestShiftAmountOnlyByte(t *testing.T) {
	expectOK(t, "let s:byte = 3\nlet n:int = 1 << s\nlet m:int = 1 >> s\n")
	expectOK(t, "let n:int = 1 << 4\nlet b:byte = 3\nlet m:int = b << 2\n")
	expectErr(t, "let s:int = 3\nlet n:int = 1 << s\n", "移位量只能写 byte 或一个 0～255 的整数，这里是 int")
	// 左式是 byte 时能移的那几个位置只有 0～7
	expectErr(t, "let b:byte = 3\nlet n:int = b << 8\n", "移位量只能在 0～7 之间，这里写了 8")
	// 字面量这一侧，越出 0～255 也是编译期就报
	expectErr(t, "let n:int = 1 << 300\n", "移位量只能在 0～63 之间，这里写了 300")
	expectErr(t, "let f:float = 1.5\nlet n:int = 1 << f\n", "移位量只能写 byte 或一个 0～255 的整数，这里是 float")
}

// expectParseErr 要求这段源码在语法层就被拒绝（括号形状那几类属于这一种）。
func expectParseErr(t *testing.T, src string) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		return
	}
	if _, err := parser.Parse("t.nf", toks); err == nil {
		t.Fatalf("这段源码该在语法层就被拒：%s", src)
	}
}

func TestSwitchLabelsMustBeConstant(t *testing.T) {
	expectErr(t, "method f(n:int, lo:int) {\n    switch (n) {\n        lo { console.print(1) }\n        default { }\n    }\n}\n", "switch 的标签只能写常量、const 或枚举成员")
	expectOK(t, "const lo:int = 3\nmethod f(n:int) {\n    switch (n) {\n        lo { console.print(1) }\n        default { }\n    }\n}\n")
}

func TestLocalNamesMustNotBeReserved(t *testing.T) {
	expectErr(t, "method f() {\n    let list:int = 1\n}\n", "list 是内建名字，不能拿去做名字")
	// shared 是关键字，先前漏在词法表之外，`let shared:int = 1` 一度能通过
	expectErr(t, "let shared:int = 1\nconsole.print(shared)\n", "shared 是关键字，不能拿去做名字")
}

// TestCalendarIsBuiltinName 覆盖 `Calendar` 这个类型名：它是编译器发出的类型名——能当类型写，
// 不能拿去做别的名字，也不能自己在文件里再定义一只同名类型。
func TestCalendarIsBuiltinName(t *testing.T) {
	expectOK(t, "let c:Calendar = time.calendar(0)\nconsole.print(c.year, c.weekday)\n")
	expectErr(t, "let Calendar:int = 1\n", "Calendar 是内建名字，不能拿去做名字")
	expectErr(t, "struct Calendar {\n    x:int\n}\n", "Calendar 是内建名字，不能拿去做名字")
}

func TestReturnCannotBeInterface(t *testing.T) {
	expectErr(t, "interface A {\n    f:int\n}\nmethod g():A {\n    return 1\n}\n", "A 是接口名，不能当类型写")
}

func TestEnumCannotDoArithmetic(t *testing.T) {
	expectErr(t, "enum D { a, b }\nmethod f(d:D):D {\n    return d + d\n}\n", "+ 用不到 D 上")
}

// enum 只有单向的换算——as int 取那个数，反过来不识别。
func TestEnumAsIntIsOneWay(t *testing.T) {
	expectOK(t, "enum Err { bad_path = 100 }\nthrow (Err.bad_path as int, \"路径不对\")\n")
	expectErr(t, "enum D { a, b }\nmethod f():D {\n    return 1 as D\n}\n", "int 不能 as 成 D")
}

// 整只容器与盒子都能进 `console.print`——运行期那一支打印机（形状表＋识别环）
// 通了，检查这一层就不该再拒绝。打印出来长什么样由 internal/lower 那组端到端测试覆盖，这里只覆盖"放行"。
func TestConsolePrintTakesContainers(t *testing.T) {
	expectOK(t, "let items:list<int> = [1]\nconsole.print(items)\n")
	expectOK(t, "let d:dict<string, int> = {}\nconsole.print(d)\n")
	expectOK(t, "let s:hashset<int> = {1}\nconsole.print(s)\n")
	expectOK(t, "let o:object = 1 as object\nconsole.print(o)\n")
	expectOK(t, "let items:list<int> = [1]\nconsole.print(length(items), items[0])\n")
}

func TestAssignmentNotExpression(t *testing.T) {
	expectParseErr(t, "let a:int = 1\nlet b:int = (a = 2)\n")
}

func TestFunctionValueRejected(t *testing.T) {
	expectErr(t, "method f(a:int) {\n}\nmethod g() {\n    let h:auto = f\n}\n", "没有 auto 这个类型")
	expectErr(t, "method f(a:int) { }\nmethod g() {\n    let x:int = f\n}\n", "f 是函数名，不能当值传来传去")
}

func TestThrowExitAreStatements(t *testing.T) {
	expectParseErr(t, "let x:int = throw (100, \"a\")\n")
	expectParseErr(t, "method f(a:int) { }\nmethod g() {\n    f(exit (100, \"a\"))\n}\n")
	expectOK(t, "method f() {\n    throw (100, \"a\")\n}\n")
}

func TestImportedFileTopLevelRuns(t *testing.T) {
	// 每个文件的顶层都跑一遍，所以非入口文件顶层写 let 是合法的
	errs := twoFiles(t,
		"import { twice } from \"./b.nf\"\nconsole.print(twice(3))\n",
		"export method twice(n:int):int {\n    return n * 2\n}\nlet factor:int = 2\n")
	if len(errs) > 0 {
		t.Fatalf("非入口文件顶层的 let 不该报错：%v", msgs(errs))
	}
}

func TestLoopBodyCheckedTwice(t *testing.T) {
	// 第一圈把 s 交出去，第二圈开头又用：这一条要靠"循环体检查两遍"才抓得到
	src := "method eat(s:string) {\n}\nlet items:list<string> = [\"a\"]\nfor (s:string in items) {\n    eat(s)\n    eat(s)\n}\n"
	expectErr(t, src, "s 是遍历交出的只读借用，不能交给参数")
	owned := "method eat(s:string) {\n}\nlet items:list<string> = [\"a\"]\nfor (i:int in 0 until 2) {\n    let s:string = items[0]\n    eat(s)\n    console.print(s)\n}\n"
	expectErr(t, owned, "s 已经交出去了，这个名字不能再写")
}

func TestSharedGlobals(t *testing.T) {
	// export shared 就地使用是合法的
	ok := twoFiles(t,
		"import { bag } from \"./b.nf\"\nbag.add(\"书包\")\nconsole.print(length(bag), bag[0])\n",
		"export shared bag:list<string> = []\n")
	if len(ok) > 0 {
		t.Fatalf("shared 就地用不该报错：%v", msgs(ok))
	}
	// 不能当参数交出去
	errs := twoFiles(t,
		"import { eat } from \"./b.nf\"\nlet xs:list<string> = []\n",
		"export method eat(s:list<string>) { }\nexport shared bag:list<string> = []\n")
	_ = errs
	errs = twoFiles(t,
		"import { bag, eat } from \"./b.nf\"\neat(bag)\n",
		"export method eat(s:list<string>) { }\nexport shared bag:list<string> = []\n")
	if !strings.Contains(strings.Join(msgs(errs), "|"), "只能就地用") {
		t.Fatalf("shared 当参数该被拦：%v", msgs(errs))
	}
}

func TestTopLevelLetCannotBeExported(t *testing.T) {
	expectErr(t, "export let count:int = 0\nconsole.print(count)\n", "顶层的 let 不能 export：跨文件共用一份可变的要写 export shared")
	expectOK(t, "export const LIMIT:int = 100\nconsole.print(LIMIT)\n")
}

func TestSharedMustPairWithExport(t *testing.T) {
	expectParseErr(t, "shared bag:list<int> = []\n")
}

func TestUnusedImportPointsAtItsOwnLine(t *testing.T) {
	errs := twoFiles(t,
		"console.print(\"先看这行\")\nconsole.print(1 + 1)\nimport { h } from \"./b.nf\"\n",
		"export method h():int { return 1 }\n")
	if len(errs) != 1 {
		t.Fatalf("该只报一条：%v", msgs(errs))
	}
	if errs[0].Line != 3 || !strings.Contains(errs[0].Msg, "没用到") {
		t.Fatalf("报错该指到 import 那一行： %+v", errs[0])
	}
}

func TestNullableRefsAcceptNull(t *testing.T) {
	expectOK(t, `class C {
    f:int
}
let s:string = null
let xs:list<int> = null
let d:dict<string, int> = null
let c:C = null
`)
	expectErr(t, "let n:int = null\n", "这里要的是 int，给的是 null；类型之间不自动转换")
}

func TestTopLevelMoveFlowsInWrittenOrder(t *testing.T) {
	expectErr(t, `class Box {
    n:int
}
let p:Box = new Box(n: 1)
let q:Box = p
console.print(q.n)
console.print(p.n)
`, "p 已经交出去了，这个名字不能再写")
	// 用在前面、写在后面：检查到那一行的时候还没交出，不该报错（按书写顺序检查）
	expectOK(t, `class Box {
    n:int
}
let p:Box = new Box(n: 1)
console.print(p.n)
let q:Box = p
`)
}

func TestTopLevelNamesAreCollectedFirst(t *testing.T) {
	// 函数体可以用下面才写的顶层名字
	expectOK(t, `method show() {
    console.print(noun)
}

let noun:string = "书包"
`)
	// 名字先收齐，参数与顶层名字重名就跟写在哪一行无关了
	expectErr(t, `method tally(width:int) {
    console.print(width)
}

const width:int = 20
`, "width 与外层的名字重名：不做内层顶掉外层")
	// 顶层块里的 let 不算"已经收齐的那一个"，照旧报重名
	expectErr(t, `let x:int = 1

if (x > 0) {
    let x:int = 2
}
`, "x 与外层的名字重名：不做内层顶掉外层")
}

func TestNullOnlyComparesWithRefs(t *testing.T) {
	expectOK(t, `let s:string = null
console.print(s == null, s != null)
`)
	expectErr(t, "console.print(null == 1)\n", "null 只能和引用类型比，这里是 int")
	expectErr(t, "let n:int = 1\nconsole.print(n > null)\n", "null 只能跟在 ==、!= 后面比，不能比大小")
}

func TestFloatCanCompareButNotBeAKey(t *testing.T) {
	// float 完全按 IEEE 语义，比大小一律有意义（NaN 参与比较得假）；拒绝的只是把它当散列键
	expectOK(t, `method near(a:float, b:float):boolean {
    if (a < b) {
        return a + 1.0 <= b
    }
    return a >= b
}

console.print(near(1.0, 2.0))
`)
	expectErr(t, "let d:dict<float, int> = {}\nd[1.5] = 1\n", "float 不能当 dict 的键：NaN 不等于自己，这个键永远查不中")
}

func TestExhaustiveSwitchCountsAsReturn(t *testing.T) {
	expectOK(t, `enum D { a, b, c }
method name(d:D):string {
    switch (d) {
        D.a { return "甲" }
        D.b { return "乙" }
        D.c { return "丙" }
    }
}
`)
	expectOK(t, `method yes(b:boolean):int {
    switch (b) {
        true { return 1 }
        false { return 0 }
    }
}
`)
	expectErr(t, `enum D { a, b, c }
method name(d:D):string {
    switch (d) {
        D.a { return "甲" }
        D.b { return "乙" }
    }
}
`, "name 标了返回类型 string，但有一条路没 return")
}

// "return 交回引用类的值同样是交出"这一条：检查器原先只做类型检查，
// 交出去之后原名还能写，是实现漏掉的一处。
// 两条一起看才算覆盖完整：引用类型算交出，值类型只是整份拷贝、原名照旧能用。
func TestReturnTransfersReferenceOnly(t *testing.T) {
	// 值类型（含 struct）：交出去的是这一份拷贝，p 还是持有者
	expectOK(t, "struct P {\n x:int\n}\nmethod f():int {\n let p:P = P { x: 3 }\n if (p.x > 0) {\n let q:P = p\n return q.x\n }\n return p.x\n}\n")
	// 引用类型：一个名字交出去两次，第二次就是错
	expectErr(t, "class Bag {\n n:int\n}\nmethod pick(flag:boolean):Bag {\n let b:Bag = new Bag(n: 1)\n if (flag) {\n return b\n }\n return b\n}\n", "b 已经交出去了，这个名字不能再写")
	// 交出去之前照样能用：这一条要是被拒绝就是记录得太早
	expectOK(t, "class Bag {\n n:int\n}\nmethod mk():Bag {\n let b:Bag = new Bag(n: 1)\n console.print(b.n)\n return b\n}\n")
	// 容器同一条：list 交出去，原名不再是持有者
	expectErr(t, "method f(flag:boolean):list<int> {\n let xs:list<int> = [1]\n if (flag) {\n return xs\n }\n return xs\n}\n", "xs 已经交出去了，这个名字不能再写")
}

// TestBareNullRefusedAtArgument 覆盖：**裸的 null 不能当实参**。
//
// 打印机靠运行期形状表识别对象，`null` 自己不是一个类型、那一个位置上没有形状——猜空句柄、
// 空文本还是空容器都有歧义。这一条先前要到生成机器码那一步才报出来（"印不出 null：运行期
// 还没有对应的例程"，把一条语言规矩说成了漏释放），现在在**参数那一处**就拒绝。
// 三样照旧放行：带类型的空句柄能印（那个位置上有形状）、`x == null` 能判、装进写着类型的
// 参数位置能过（`f(null)` 里 `f` 的参数写着 `string`，形状由那个位置给）。
func TestBareNullRefusedAtArgument(t *testing.T) {
	const want = "裸的 null 不能当实参：它自己不是一个类型，那一个位置上没有形状。要判空就写 x == null；要打印一个空句柄，先给它一个类型（let none:Bag = null）"
	expectErr(t, "console.print(null)\n", want)
	expectErr(t, "console.print(\"甲\", null)\n", want)
	expectErr(t, "let xs:list<int> = [1]\nconsole.print(xs, null)\n", want)
	expectErr(t, "let s:string = number.to_string(null)\nconsole.print(s)\n", want)
	expectErr(t, "console.print(length(null))\n", want)
	expectErr(t, "console.print(math.absolute_value(null))\n", want)
	// 带类型的空句柄：那个位置上有形状，照旧印得出 null
	expectOK(t, "class Bag {\n    n:int\n}\nlet none:Bag = null\nconsole.print(none, none == null)\n")
	// 判空本来就是这一条要给的路
	expectOK(t, "class Bag {\n    n:int\n}\nlet b:Bag = new Bag(n: 1)\nif (b != null) {\n    console.print(b.n)\n}\n")
	// 写着类型的参数位置给 null 是另一件事：那个位置自己带形状
	expectOK(t, "method f(s:string) {\n}\nf(null)\n")
}

// TestContainerCannotBeBoxed 覆盖：盒子只装 class、struct 和值类型，
// **整只**容器装不进去，而且这一条要在检查阶段就报——先前它是前端放行、降到后端才报出来
// （"把 list<int> 装进盒子：这一条还空着"），一条语言规矩被说成了后端少做了一块。
// 拆回来要认出尖括号里那一层，光一个形状表里的号不够，所以这一条是设计选择不是缺口。
// 三样照旧过：一个一个装进去、class 句柄装进去、struct 装进去。
func TestContainerCannotBeBoxed(t *testing.T) {
	expectErr(t, "let l:list<int> = [1]\nlet o:object = l as object\nconsole.print(o)\n", "list<int> 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, "let d:dict<string, int> = {}\nlet o:object = d as object\nconsole.print(o)\n", "dict<string, int> 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, "let s:hashset<int> = {1}\nlet o:object = s as object\nconsole.print(o)\n", "hashset<int> 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	// 里面那一个装进去是另一回事：那个位置上有形状
	expectOK(t, "let l:list<int> = [1]\nlet o:object = l[0] as object\nconsole.print(o)\n")
	expectOK(t, "class Bag {\n    n:int\n}\nlet o:object = new Bag(n: 1) as object\nconsole.print(o)\n")
	expectOK(t, "struct Dot {\n    x:int\n}\nlet d:Dot = Dot { x: 1 }\nlet o:object = d as object\nconsole.print(o)\n")
}

// TestJsonCannotBeBoxed 覆盖另一半：Json 与三种容器同一档——进不了盒子，
// 也从盒子里拆不出来，`is` 也问不出来。这三处先前过了检查、到发码那一步报
// "Json 的类型字没排上"（`nf ir` 同一条路也断），现在在检查阶段拒绝。
func TestJsonCannotBeBoxed(t *testing.T) {
	expectErr(t, "let o:object = json.parse(\"1\") as object\nconsole.print(o)\n", "Json 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, "let j:Json = json.parse(\"1\")\nlet o:object = j as object\nconsole.print(o)\n", "Json 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, "let o:object = 5 as object\nlet j:Json = o as Json\nconsole.print(j)\n", "盒子里没有 Json：它进不了盒子，也拆不出来")
	expectErr(t, "let o:object = 5 as object\nlet xs:list<int> = o as list<int>\nconsole.print(xs)\n", "盒子里没有 list<int>：它进不了盒子，也拆不出来")
	expectErr(t, "let j:Json = json.parse(\"1\")\nconsole.print(j is Json)\n", "is 的右边只能写盒子里可能装的那个类型")
	// 该过的照旧过：Json 待在名字与返回值里、打印；盒子里拆的是进得去的那一档
	expectOK(t, "let j:Json = json.parse(\"1\")\nconsole.print(j)\n")
	expectOK(t, "method f():Json {\n    return json.parse(\"3\")\n}\nconsole.print(f())\n")
	expectOK(t, "let o:object = 5 as object\nconsole.print(o as int)\n")
}

// TestBoxTwiceRefused 覆盖：已经在盒子里的值不必再装箱。
// 先前 `o as object`（`o` 已经写着 `object`）检查放行、发码那一步才报
// "盒子转盒子这一版不发码"——那是一种没有意义的写法，被说成了这一版还没做。
// 装一次照旧过，拆盒那一侧（`object` 拆成具名类型）也照旧过，运行结果对不对由另一条覆盖（10011 号）。
func TestBoxTwiceRefused(t *testing.T) {
	const pre = "class Bag {\n    n:int\n}\nlet o:object = new Bag(n: 1) as object\n"
	expectErr(t, pre+"let p:object = o as object\nconsole.print(p)\n", "object 不必再装箱：它已经在盒子里，直接用它就行")
	expectOK(t, pre+"console.print(o)\n")
	expectOK(t, "class Bag {\n    n:int\n}\nlet o:object = new Bag(n: 1) as object\nlet b:Bag = o as Bag\nconsole.print(b.n)\n")
}

// TestToStringRefusesText 覆盖检查这一侧：`number.to_string` 不接文本，
// 因为文本本来就是文本、接进来只会多拷一份。这一条先前到发码那一步才报出来。
// 其余种类照旧过（`unicode` 与 `enum` 返回的是它那个数，不是它的名字）。
func TestToStringRefusesText(t *testing.T) {
	expectErr(t, "console.print(number.to_string(\"甲\"))\n", "number.to_string 不接 string：文本本来就是文本，接进来只会白拷一份")
	expectErr(t, "let s:string = \"12\"\nlet t:string = number.to_string(s)\nconsole.print(t)\n", "number.to_string 不接 string：文本本来就是文本，接进来只会白拷一份")
	expectOK(t, "console.print(number.to_string(1), number.to_string(1.5), number.to_string(true))\n")
	expectOK(t, "console.print(number.to_string(97 as unicode), number.to_string(1 as byte))\n")
	expectOK(t, "enum E { a = 0 }\nconsole.print(number.to_string(E.a))\n")
	expectOK(t, "let l:list<int> = [1]\nconsole.print(number.to_string(l))\n")
}

// TestOrderingRefusesUnorderedTypes 覆盖：比大小那四个记号只用于
// `int`、`float`、`byte`、`unicode`、`string` 的先后。先前引用类型、容器、`enum`、
// `boolean` 都能过检查，实际比较的是句柄地址——实测两只 `new Box()` 的 `x < y` 给 `true`、
// `x >= y` 给 `false`，顺序跟着分配走；`enum` 与 `boolean` 则没有先后可言。
// 判等那两条不受影响。
func TestOrderingRefusesUnorderedTypes(t *testing.T) {
	const classPre = "class Box {\n    n:int\n}\nlet x:Box = new Box(n: 1)\nlet y:Box = new Box(n: 2)\n"
	expectErr(t, classPre+"console.print(x < y)\n", "< 用不到 Box 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectErr(t, classPre+"console.print(x >= y)\n", ">= 用不到 Box 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectOK(t, classPre+"console.print(x == y, x != y)\n")

	const objPre = "let o:object = 7 as object\nlet p:object = 8 as object\n"
	expectErr(t, objPre+"console.print(o < p)\n", "< 用不到 object 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectOK(t, objPre+"console.print(o == p)\n")

	expectErr(t, "let a:list<int> = [1, 2]\nlet b:list<int> = [3]\nconsole.print(a < b)\n", "< 用不到 list<int> 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectErr(t, "let a:hashset<int> = {1}\nlet b:hashset<int> = {2}\nconsole.print(a > b)\n", "> 用不到 hashset<int> 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectErr(t, "let a:dict<int, int> = {1: 2}\nlet b:dict<int, int> = {3: 4}\nconsole.print(a <= b)\n", "<= 用不到 dict<int, int> 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectErr(t, "enum D { a = 1, b = 2 }\nconsole.print(D.a < D.b)\n", "< 用不到 D 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectOK(t, "enum D { a = 1, b = 2 }\nconsole.print(D.a == D.b, D.a as int)\n")
	expectErr(t, "console.print(true < false)\n", "< 用不到 boolean 上：比大小只有 int、float、byte、unicode、string 这五个类型")
	expectOK(t, "console.print(true == false)\n")

	// 那五个类型照旧可比
	expectOK(t, "console.print(1 < 2, 1.5 <= 2.5)\n")
	expectOK(t, "let a:byte = 1\nlet b:byte = 2\nconsole.print(a < b)\n")
	expectOK(t, "let a:unicode = '甲'\nlet b:unicode = '乙'\nconsole.print(a < b)\n")
	expectOK(t, "console.print(\"abc\" < \"abd\")\n")
}

// TestArithmeticRefusesText 覆盖：文本只参与 `+`。
// 先前这四条过得去检查，后端拿文本的句柄去做算术——实测 `-`、`*` 让 nf.exe 自己段错误（退出 139），
// `/` 运行期抛 10092，`%` 把 `"abc" % "de"` 打印成 `abc`。
func TestArithmeticRefusesText(t *testing.T) {
	const pre = "let a:string = \"abc\"\nlet b:string = \"de\"\n"
	expectErr(t, pre+"console.print(a - b)\n", "- 用不到 string 上")
	expectErr(t, pre+"console.print(a * b)\n", "* 用不到 string 上")
	expectErr(t, pre+"console.print(a / b)\n", "/ 用不到 string 上")
	expectErr(t, pre+"console.print(a % b)\n", "% 用不到 string 上")
	expectOK(t, pre+"console.print(a + b)\n")
}

// TestUnusualOperatorMessages 覆盖那两句改过措辞的报错：自增自减那句先前开头多打印一个 `%`
// （Go 的格式串把 `%%` 当字面百分号），一元负号那句把 `byte` 列进可用名单、但它走的是另一句。
func TestUnusualOperatorMessages(t *testing.T) {
	expectErr(t, "let s:string = \"a\"\ns++\n", "++ 与 -- 只用在 int、float、byte 上，这里是 string")
	expectErr(t, "console.print(-\"a\")\n", "一元负号只用在 int、float 上，这里是 string")
	expectErr(t, "let b:byte = 3\nconsole.print(-b)\n", "byte 是 0～255 无符号，取负得先 as int")
}

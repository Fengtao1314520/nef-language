// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// 规则：往容器的一个位置里写入一个当场创建的 `struct`，那块内存直接驻留在该位置，不再拷贝第二次。
//
// 若进容器时再拷一次，就会出现两块同时存在、只有一块有持有者的情形：`Rec { n: i }` 先造一块，
// 进容器那一步又拷一块，原始那块从此无人析构（实测往 list 里每圈放一个泄漏 16 字节，三个位置每圈 48）。
// 判据与声明、整份赋值同一条（`lower.go` 的 `fresh`）。
//
// 名字（`[one]`）与 class 的 struct 字段（`[g.p]`）保持现状：那两处只读、不带走源，名字之后还能
// 再读，去掉拷贝就会造成一个值两个持有者。它们量出的每圈 16 字节另有原因，见 TestContainerCopySweep。

const recDecl = `struct Rec {
    n:int
}
`

func TestStructLiteralIntoContainerIsNotCopied(t *testing.T) {
	cases := []struct {
		name  string
		body  string // 循环体，两种圈数下量每圈增长
		out   string // 这一段跑一次的逐字输出
		check string // 完整程序：验证读回来的值
	}{
		{"list 字面量",
			`    let xs:list<Rec> = [Rec { n: i }]
    total = total + xs[0].n
`, "2 7 8\n",
			recDecl + `let xs:list<Rec> = [Rec { n: 7 }, Rec { n: 8 }]
console.print(length(xs), xs[0].n, xs[1].n)
`},
		{"list.add",
			`    let xs:list<Rec> = [Rec { n: 0 }]
    xs.add(Rec { n: i })
    total = total + xs[1].n
`, "2 1 2\n",
			recDecl + `let xs:list<Rec> = [Rec { n: 1 }]
xs.add(Rec { n: 2 })
console.print(length(xs), xs[0].n, xs[1].n)
`},
		{"list.insert_at",
			`    let xs:list<Rec> = [Rec { n: 9 }]
    xs.insert_at(0, Rec { n: i })
    total = total + xs[0].n
`, "2 4 9\n",
			recDecl + `let xs:list<Rec> = [Rec { n: 9 }]
xs.insert_at(0, Rec { n: 4 })
console.print(length(xs), xs[0].n, xs[1].n)
`},
		{"dict.add",
			`    let d:dict<string, Rec> = {"丙": Rec { n: 0 }}
    d.add("甲", Rec { n: i })
    total = total + d["甲"].n
`, "2 1 2\n",
			recDecl + `let d:dict<string, Rec> = {"甲": Rec { n: 1 }}
d.add("乙", Rec { n: 2 })
console.print(length(d), d["甲"].n, d["乙"].n)
`},
	}
	progs := make([]string, len(cases))
	for i, one := range cases {
		progs[i] = recDecl + `let total:int = 0
for (i:int in 0 until @N@) {
` + one.body + `}
console.print(total)
`
		// 四段先各运行一遍再量堆：heapSlope 设了 NEF_HEAP，之后的 run() 输出会多一行 `heap …`
		got, code := run(t, one.check)
		if got != one.out || code != 0 {
			t.Errorf("%s：值读错了\n got %q 码 %d\nwant %q", one.name, got, code, one.out)
		}
		if _, code := run(t, strings.ReplaceAll(progs[i], "@N@", "8")); code != 0 {
			t.Fatalf("%s：循环那段跑坏了（码 %d）", one.name, code)
		}
	}
	for i, one := range cases {
		if per := heapSlope(t, progs[i]); per > 2 {
			t.Errorf("%s：进容器时多拷的那一块每圈还留 %g 字节", one.name, per)
		}
	}
}

// TestDictAddOverwriteFreesTheOldBlock 键已在桶里：值槽位换成新块、旧块当场析构。
// 若新块拷贝后才进槽位，原始块无人管理，每圈留 16 字节；旧块本身能正常析构。
func TestDictAddOverwriteFreesTheOldBlock(t *testing.T) {
	const prog = recDecl + `let d:dict<string, Rec> = {"甲": Rec { n: 0 }}
let total:int = 0
for (i:int in 0 until @N@) {
    d.add("甲", Rec { n: i })
    total = total + d["甲"].n
}
console.print(total)
`
	got, code := run(t, strings.ReplaceAll(prog, "@N@", "6"))
	if want := "15\n"; got != want || code != 0 {
		t.Fatalf("跑坏了：got %q 码 %d，want %q", got, code, want)
	}
	if per := heapSlope(t, prog); per > 2 {
		t.Fatalf("同一个键换值，每圈还留 %g 字节", per)
	}
}

// TestNameAndFieldIntoContainerKeepTheirCopy 固定现行规则下仍拷贝的两种：名字与 class 的 struct 字段
// 进容器仍另起一份拷贝，源那一侧读得到、改得动，容器里那个位置改了也不碰源——各覆盖一条用例。
func TestNameAndFieldIntoContainerKeepTheirCopy(t *testing.T) {
	got, code := run(t, recDecl+`class Holder {
    p:Rec
}
let one:Rec = Rec { n: 1 }
let g:Holder = new Holder(p: Rec { n: 9 })
let xs:list<Rec> = [one, g.p]
one.n = 2
xs[0].n = 3
xs[1].n = 5
console.print(one.n, g.p.n, xs[0].n, xs[1].n)
`)
	const want = "2 9 3 5\n"
	if got != want || code != 0 {
		t.Fatalf("两种照旧拷一份的写法：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestNameIntoContainerLiteralKeepsItsOwnBlock：值类型名字进容器字面量的位置不算把名字
// 交出去。那个位置收下的是一份拷贝（`list` 路径一直拷贝，`dict` 路径现行规则同样拷贝），
// 名字一侧仍是自己那块的持有者。此前 `claim` 因该位置记过一笔"交出过"而不登记名字，
// 名字那块每圈留 16 字节无人析构。
func TestNameIntoContainerLiteralKeepsItsOwnBlock(t *testing.T) {
	const list = recDecl + `let total:int = 0
for (i:int in 0 until @N@) {
    let one:Rec = Rec { n: i }
    let xs:list<Rec> = [one]
    total = total + one.n + xs[0].n
}
console.print(total)
`
	const dict = recDecl + `let total:int = 0
for (i:int in 0 until @N@) {
    let one:Rec = Rec { n: i }
    let d:dict<string, Rec> = {"甲": one}
    total = total + one.n + d["甲"].n
}
console.print(total)
`
	// 两条各运行一遍：先验证语义——名字后来改了，容器里那两份都不跟着变（此前 dict 那一份与源是同一块，会跟着变）
	got, code := run(t, recDecl+`let one:Rec = Rec { n: 1 }
let xs:list<Rec> = [one]
let d:dict<string, Rec> = {"甲": one}
one.n = 2
console.print(one.n, xs[0].n, d["甲"].n)
`)
	if want := "2 1 1\n"; got != want || code != 0 {
		t.Fatalf("容器里那两份与源不同一块这条跑坏了：\n got %q 码 %d\nwant %q", got, code, want)
	}
	for _, one := range []struct{ name, tmpl string }{{"list 字面量", list}, {"dict 字面量", dict}} {
		if _, code := run(t, strings.ReplaceAll(one.tmpl, "@N@", "8")); code != 0 {
			t.Fatalf("%s：那段跑坏了（码 %d）", one.name, code)
		}
		if per := heapSlope(t, one.tmpl); per > 2 {
			t.Errorf("%s：名字自己那一块每圈还留 %g 字节", one.name, per)
		}
	}
}

// TestStructFieldInitIntoNewIsNotCopiedTwice：`new C(f: 值)` 的字段初值走与容器字面量
// 同一个判据——当场创建的那块直接驻进该字段；是名字就拷一份，名字仍是自己那块的持有者。
// 此前两种写法各每圈留 16 字节：字面量写法丢的是被丢弃的原始块，名字写法丢的是名字自己那块。
func TestStructFieldInitIntoNewIsNotCopiedTwice(t *testing.T) {
	const lit = recDecl + `class Holder {
    p:Rec
}
let total:int = 0
for (i:int in 0 until @N@) {
    let g:Holder = new Holder(p: Rec { n: i })
    total = total + g.p.n
}
console.print(total)
`
	const name = recDecl + `class Holder {
    p:Rec
}
let total:int = 0
for (i:int in 0 until @N@) {
    let one:Rec = Rec { n: i }
    let g:Holder = new Holder(p: one)
    total = total + g.p.n + one.n
}
console.print(total)
`
	// 字段里那份与源是两块：改源不动字段（值类型保持值语义）
	got, code := run(t, recDecl+`class Holder {
    p:Rec
}
let one:Rec = Rec { n: 1 }
let g:Holder = new Holder(p: one)
one.n = 2
console.print(one.n, g.p.n)
`)
	if want := "2 1\n"; got != want || code != 0 {
		t.Fatalf("字段那一份与源各自独立这条：\n got %q 码 %d\nwant %q", got, code, want)
	}
	for _, one := range []struct{ name, tmpl string }{{"初值是字面量", lit}, {"初值是名字", name}} {
		if _, code := run(t, strings.ReplaceAll(one.tmpl, "@N@", "8")); code != 0 {
			t.Fatalf("%s：那段跑坏了（码 %d）", one.name, code)
		}
		if per := heapSlope(t, one.tmpl); per > 2 {
			t.Errorf("%s：每圈还留 %g 字节", one.name, per)
		}
	}
}

// TestContainerCopySweep 把"进容器的一格"几种来源各测量一遍，输出实测数值。
//
// 连保持现状的几类一起覆盖的原因：这一族此前被记成一笔"每放一个留一块"，实测发现
// 三种来源漏的是三处不同的东西——字面量写法丢的是多余拷贝的那一块；名字写法丢的是
// **名字自己那一块**（那个位置收下的是拷贝，源却因容器字面量记过一笔"交出过"而不登记成
// 持有者，已按名字规则修正；`xs.add(one)` 那条路径从不记这一笔，所以一直是 0）；字段写法不在
// 容器这一侧（单独一句 `new Holder(p: Rec { n: i })` 就每圈 16，对象析构能把字段那块正常释放：
// 同一处换成圈外那个名字是 0——那是 `new` 的字段初值多拷的一块）。
// 还有一处是容器字面量与 `new` 里的中间读走：`listLit`／`mapLit`／`setLit`／`newObject` 开头那句
// `noBorrow` 曾把中间读走的释放规则挡在外面，改为开窗口后正常释放。`setLit` 那一处量不出来——
// 能进 `hashset` 的那几样（数字、文本、`class`、盒子）读走都不另起一块，本表没有能固定它的形状，
// 只有代码与另外三处改在同一条规则上。
// 表里最后一行是同一形状的另一处：`argsOf` 开头也曾是那句 `noBorrow`，所以 `wear(xs[0])` 那种
// "读走的一格交给用户函数"每圈留 16 字节；这一处按同一规则纳入、改成开窗口之后归 0。
func TestContainerCopySweep(t *testing.T) {
	shapes := []struct {
		name string
		body string
		want float64 // 这一种当前每圈应留的字节数：0＝已释放，16＝留一整块
	}{
		{"字面量进 list", `    let xs:list<Rec> = [Rec { n: i }]
    total = total + xs[0].n
`, 0},
		{"字面量进 list（三个位置）", `    let xs:list<Rec> = [Rec { n: i }, Rec { n: i }, Rec { n: i }]
    total = total + xs[2].n
`, 0},
		{"字面量交 list.add", `    let xs:list<Rec> = [Rec { n: 0 }]
    xs.add(Rec { n: i })
    total = total + xs[1].n
`, 0},
		{"字面量交 list.insert_at", `    let xs:list<Rec> = [Rec { n: 0 }]
    xs.insert_at(0, Rec { n: i })
    total = total + xs[0].n
`, 0},
		{"字面量交 dict.add", `    let d:dict<string, Rec> = {"丙": Rec { n: 0 }}
    d.add("甲", Rec { n: i })
    total = total + d["甲"].n
`, 0},
		{"名字进 list（名字仍是自己那一块的持有者）", `    let one:Rec = Rec { n: i }
    let xs:list<Rec> = [one]
    total = total + length(xs)
`, 0},
		{"同一个名字进两个位置（两份拷贝、名字照旧登记）", `    let one:Rec = Rec { n: i }
    let xs:list<Rec> = [one, one]
    total = total + length(xs)
`, 0},
		{"名字交给 list.add（那一路不记交出）", `    let one:Rec = Rec { n: i }
    let xs:list<Rec> = [Rec { n: 0 }]
    xs.add(one)
    total = total + length(xs)
`, 0},
		{"名字交给另一个名字（声明的拷贝）", `    let one:Rec = Rec { n: i }
    let q:Rec = one
    total = total + q.n
`, 0},
		{"名字交给用户函数的参数", `    let one:Rec = Rec { n: i }
    total = total + wear(one)
`, 0},
		{"class 的 struct 字段进 list（现行规则下两处都不留）", `    let g:Holder = new Holder(p: Rec { n: i })
    let xs:list<Rec> = [g.p]
    total = total + length(xs)
`, 0},
		{"只 new 一个带 struct 字段的对象", `    let g:Holder = new Holder(p: Rec { n: i })
    total = total + g.p.n
`, 0},
		{"new 的字段初值是名字", `    let one:Rec = Rec { n: i }
    let g:Holder = new Holder(p: one)
    total = total + g.p.n
`, 0},
		{"new 的字段初值是容器里读走的一格", `    let src:list<Rec> = [Rec { n: i }]
    let g:Holder = new Holder(p: src[0])
    total = total + g.p.n
`, 0},
		{"对象的 struct 字段来自圈外那个名字", `    let g:Holder = new Holder(p: held)
    total = total + g.p.n
`, 0},
		{"容器里读走的一格进 list", `    let src:list<Rec> = [Rec { n: i }]
    let xs:list<Rec> = [src[0]]
    total = total + xs[0].n
`, 0},
		{"容器里读走的一格进 dict 的值位", `    let src:list<Rec> = [Rec { n: i }]
    let d:dict<string, Rec> = {"甲": src[0]}
    total = total + d["甲"].n
`, 0},
		{"容器里那一格读走，不再进容器", `    let src:list<Rec> = [Rec { n: i }]
    total = total + src[0].n
`, 0},
		{"容器里读走的一格交给用户函数的参数", `    let src:list<Rec> = [Rec { n: i }]
    total = total + wear(src[0])
`, 0},
	}
	const prog = recDecl + `class Holder {
    p:Rec
}
method wear(v:Rec):int {
    return v.n
}
let held:Rec = Rec { n: 0 }
let total:int = 0
for (i:int in 0 until @N@) {
@BODY@}
console.print(total)
`
	for _, s := range shapes {
		per := heapSlope(t, strings.ReplaceAll(prog, "@BODY@", s.body))
		t.Logf("%-32s 每圈 %6.1f 字节", s.name, per)
		if s.want == 0 && per > 2 {
			t.Errorf("%s：这一种该还掉，每圈还留 %g 字节", s.name, per)
		}
		if s.want > 0 && per < s.want/2 {
			t.Errorf("%s：这一种本轮没动，量到每圈 %g 字节，与钉住的 %g 差一半以上", s.name, per, s.want)
		}
	}
}

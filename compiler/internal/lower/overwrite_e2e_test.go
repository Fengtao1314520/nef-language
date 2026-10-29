// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestOverwrittenNameFreesTheOldBlock 验证"覆盖一个名字就析构它换下的旧值"（容器位置的规则应用到名字一侧）。
//
// 度量的是每多一圈多要几个字节（不用绝对值的原因见 heapSlope 处的说明）。这四行都应是 0；
// 修复前在同一台机器上的实测：struct 每圈 48、class 16、list 88（两万圈分别 960,160／320,152／1,760,232），
// dict 那条没有单独量过，与 list 遵循同一条规则。
func TestOverwrittenNameFreesTheOldBlock(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
	}{
		{"struct 名字被覆盖", `struct Row {
    a:int
    b:float
}
let r:Row = Row { a: 0, b: 0.0 }
for (i:int in 0 until @N@) {
    r = Row { a: i, b: 1.5 }
}
console.print(r.a)
`},
		{"class 名字被覆盖", `class C {
    n:int
}
let c:C = new C()
for (i:int in 0 until @N@) {
    c = new C()
}
console.print(c.n)
`},
		{"list 名字被覆盖", `let xs:list<int> = []
for (i:int in 0 until @N@) {
    xs = [1, 2]
}
console.print(length(xs))
`},
		{"dict 名字被覆盖", `let d:dict<string, int> = {}
for (i:int in 0 until @N@) {
    d = {"甲": i}
}
console.print(d["甲"])
`},
	}
	for _, tc := range cases {
		const few, many = 200, 5000
		a := heapAt(t, strings.ReplaceAll(tc.tmpl, "@N@", "200"))
		b := heapAt(t, strings.ReplaceAll(tc.tmpl, "@N@", "5000"))
		if slope := float64(b-a) / float64(many-few); slope >= 4 {
			t.Errorf("%s：每圈还留 %g 字节，覆盖的时候没把换下来的那一份拆掉（两万圈量到过 48～88）", tc.name, slope)
		}
	}
}

// TestStaticLiteralNameIsAHolder 验证：初值是程序里写死的那段文本（`let s:string = "甲"`）的名字
// 也算持有者。能析构的是换下来的那一份，第一次换下的正是那段字面量——它的占位头全为 0，
// `@free` 能识别它不在堆上，释放它是空操作，所以登记它不会误析构别人持有的内存。
//
// 三段各检查一件事：读出的值仍是最新一份；覆盖只发生在 `if` 里的那条路径也不报错（编译器无法
// 静态确定该位置此刻装的是哪一份）；每圈赋新文本不再留块（此前每圈 32，两万圈 640,136）。
// 量堆的两步放在实际运行之后：heapAt 会设 NEF_HEAP，之后的 run() 输出会多一行 `heap …`。
func TestStaticLiteralNameIsAHolder(t *testing.T) {
	const tmpl = `let s:string = "甲"
for (i:int in 0 until @N@) {
    s = "丙" + "丁"
}
console.print(length(s))
`
	got, code := run(t, strings.ReplaceAll(tmpl, "@N@", "20"))
	if got != "2\n" || code != 0 {
		t.Fatalf("覆盖之后的长度不对：\n got %q 码 %d\nwant \"2\\n\"（长度数的是码点）", got, code)
	}
	// 那段字面量无法析构、堆上那块要在圈尾释放：两条路径在同一段代码里各跑一千多圈，数值必须正确且不得报错
	kept, keptCode := run(t, `
for (i:int in 0 until 2000) {
    let s:string = "甲"
    if (i > 900) {
        s = "丙" + "丁"
    }
    console.print(length(s))
}
`)
	want := strings.Repeat("1\n", 901) + strings.Repeat("2\n", 1099)
	if kept != want || keptCode != 0 {
		t.Fatalf("条件里覆盖、圈尾再还一次，这一段跑坏了：码 %d，got 行数 %d 长度 %d，want 行数 %d 长度 %d",
			keptCode, strings.Count(kept, "\n"), len(kept), strings.Count(want, "\n"), len(want))
	}
	a := heapAt(t, strings.ReplaceAll(tmpl, "@N@", "200"))
	b := heapAt(t, strings.ReplaceAll(tmpl, "@N@", "5000"))
	if per := float64(b-a) / float64(5000-200); per > 2 {
		t.Fatalf("每圈还留 %g 字节：这个名字没登记成持有者", per)
	}
}

// TestOverwriteKeepsCopiesAndBorrows 验证"只析构这个名字自己那一块"的边界：
// 拷出去的那一份、容器里的那一份、只读借用的那一个、shared 的那一份，都不应被顺带析构。
func TestOverwriteKeepsCopiesAndBorrows(t *testing.T) {
	got, code := run(t, `struct Inner {
    n:int
}

struct Row {
    v:Inner
    tag:int
}

class Box {
    n:int
    method get():int { return n }
}

let a:Row = Row { v: Inner { n: 1 }, tag: 2 }
let b:Row = a
a = Row { v: Inner { n: 9 }, tag: 3 }
console.print(a.v.n, a.tag, b.v.n, b.tag)

let words:list<string> = ["甲", "乙"]
for (w:string in words) {
    w = "丙"
}
console.print(words[0], words[1], length(words))

let self:Row = Row { v: Inner { n: 4 }, tag: 0 }
self = self
console.print(self.v.n)

let one:Box = new Box()
one.n = 7
let two:Box = new Box()
two.n = 8
console.print(one.get(), two.get())
one = two
console.print(one.get())
`)
	want := "9 3 1 2\n甲 乙 2\n4\n7 8\n8\n"
	if got != want || code != 0 {
		t.Fatalf("覆盖的时候拆错了地方：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestOverwriteInMethodParameter 验证方法里覆盖参数那个名字：析构的是本帧该位置原来的值，
// 调用方持有的那一份不受影响（参数传进来是交出，但 struct 一侧交出的是拷贝）。
func TestOverwriteInMethodParameter(t *testing.T) {
	got, code := run(t, `struct Row {
    a:int
}

method mk(n:int):Row {
    return Row { a: n }
}

method bump(r:Row):Row {
    r = mk(r.a + 1)
    return r
}

let one:Row = mk(5)
let two:Row = bump(one)
console.print(one.a, two.a)
let three:Row = bump(bump(two))
console.print(three.a)
`)
	want := "5 6\n8\n"
	if got != want || code != 0 {
		t.Fatalf("参数那一格覆盖不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestSharedOverwrite 验证 `shared` 一侧：它不参与析构登记，覆盖它不析构旧值，
// 别的文件里正持有的那一份不受影响。
func TestSharedOverwrite(t *testing.T) {
	got, code := run(t, `export shared log:list<string> = []

method push(s:string) {
    log = [s]
}

push("甲")
push("乙")
console.print(length(log), log[0])
`)
	want := "1 乙\n"
	if got != want || code != 0 {
		t.Fatalf("shared 那一个名字的覆盖不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

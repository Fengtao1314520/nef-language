// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/parser"
)

// prog 把一段源码降到中间码（与 run 那条链同一条路，只是停在中间码这一层）。
func progOf(t *testing.T, src string) *ir.Prog {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.Parse("t.nf", toks)
	if err != nil {
		t.Fatal(err)
	}
	p := &check.Program{Files: []*ast.File{f}, Entry: "t.nf"}
	errs, facts := check.RunWithFacts(p, true)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip, err := lower.Run(p, facts)
	if err != nil {
		t.Fatal(err)
	}
	return ip
}

// findFn 按名字找那一个函数（顶层那一层的函数名是 "nef.top.<文件>"，方法是 "类名.方法名"）。
func findFn(p *ir.Prog, name string) *ir.Func {
	for _, f := range p.Funcs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// lastRun 取函数体里**最后**一段连续的"nef.destroy ＋ 抹零"——块尾 close() 发射的那一段。
// 早退路径（return／throw／exit／break）上各有一份同样形状，叫退出清理代码：发射在跳走之前，
// 两条路径各执行一次，不会把同一块释放两遍（释放完那个位置就抹成 0，另一条路径看见的是空句柄）。
func lastRun(fn *ir.Func) []string {
	var cur, last []string
	for _, in := range fn.Body {
		c, ok := in.(*ir.Call)
		if ok && c.Fn == "nef.destroy" {
			cur = append(cur, c.Args[0].String())
			continue
		}
		if mv, ok := in.(*ir.Mov); ok && mv.S == ir.K(0) {
			continue // 跟着那一笔析构的抹零，不算断开
		}
		if len(cur) > 0 {
			last = cur
			cur = nil
		}
	}
	if len(cur) > 0 {
		last = cur
	}
	return last
}

// padsBefore 检查每一个"跳走"（@throw／@die／Ret）之前有没有一段退出清理代码。
func padsBefore(t *testing.T, fn *ir.Func) {
	t.Helper()
	for i, in := range fn.Body {
		c, ok := in.(*ir.Call)
		if !ok || (c.Fn != "@throw" && c.Fn != "@die") {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if pc, ok := fn.Body[j].(*ir.Call); ok && pc.Fn == "nef.destroy" {
				return // 就近有一笔析构，算有清理段
			}
			if _, isMov := fn.Body[j].(*ir.Mov); !isMov {
				if _, isLbl := fn.Body[j].(*ir.Lbl); !isLbl {
					break
				}
			}
		}
		t.Fatalf("第 %d 条 %s 之前没有退出清理代码", i, c.Fn)
	}
}

// TestDestroyEmittedAtScopeEnd 验证"谁登记、谁不登记"的规则（实现在 lower 的 claim 与
// reclaimable 两处）：
//
//   - `new`、容器字面量、struct 字面量、标准库组函数返回的整块——登记，块尾按声明反序释放；
//   - 从别的名字交出来的、从字段与容器里读出来的、自定义函数返回来的——不登记，
//     因为那几处此刻还有另一个出处（检查器只在"名字直接交出去"这一种情形把源记成交出），
//     两个持有者释放同一块，比遗留一块无人认领的内存严重得多。
//
// 这条测试检查中间码而非运行表现：运行结果正确不代表发射正确，反之亦然。
func TestDestroyEmittedAtScopeEnd(t *testing.T) {
	p := progOf(t, `
struct Pair {
    a:int
    b:int
}
class Bag {
    n:int
}
method mk():Bag {
    return new Bag(n: 1)
}
method f():int {
    let a:Bag = new Bag(n: 1)
    let xs:list<int> = [1, 2]
    let parts:list<string> = "甲,乙".split(",")
    let p:Pair = Pair { a: 1, b: 2 }
    let b2:Bag = a
    let q:Bag = mk()
    return length(xs) + length(parts) + p.a + b2.n + q.n
}
console.print(f())
`)
	fn := findFn(p, "f")
	if fn == nil {
		t.Fatal("函数 f 没排出来")
	}
	var all []string
	for _, in := range fn.Body {
		c, ok := in.(*ir.Call)
		if !ok || c.Fn != "nef.destroy" {
			continue
		}
		all = append(all, c.Args[0].String())
	}
	slots := lastRun(fn)
	// 登记的是四笔：xs（容器字面量）、parts（标准库返回的）、p（struct 字面量），
	// 加上 q——`mk()` 是自定义函数，它的每一条 return 返回的都是当场新造的一块，
	// 除这个位置外没有别的持有者（判据在 lower.go 的 handsBackFresh）。
	// a 把值交给了 b2 → 那份记录上记它"交出过"，于是它不再是持有者；b2 的初值是一个名字，
	// 那一块还有 a 这个出处，所以不登记。
	if len(slots) != 4 {
		t.Fatalf("块尾该四笔析构，实际 %d 笔：%v", len(slots), slots)
	}
	// 本函数以 return 结束，路径上还有几处内建检查（读字段要判空引用），每一处跳走之前都要有
	// 一段退出清理代码——所以总笔数一定多于块尾那四笔。具体几段清理代码、各释放哪个槽位，由
	// throw／return／break 那三组专门的测试覆盖，这里只验证"块尾那一段的形状没被清理段破坏"。
	if len(all) <= len(slots) {
		t.Fatalf("早退路上的退出清理代码一笔都没有：%v", all)
	}
	padsBefore(t, fn)
	// 声明的反序：p 最后登记（帧上的槽号最大），所以它第一个被释放
	num := func(s string) int { n, _ := strconv.Atoi(strings.TrimPrefix(s, "%")); return n }
	if !(num(slots[0]) > num(slots[1]) && num(slots[1]) > num(slots[2])) {
		t.Fatalf("析构该按声明的反序走：%v", slots)
	}
	// 每一笔之后紧跟一句"把那个位置抹成 0"：第二遍走到这里（循环的第二圈、或者退出清理代码与块尾两条路径）
	// 只能看见空句柄，不会把同一块释放两次
	var cleared int
	for i, in := range fn.Body {
		c, ok := in.(*ir.Call)
		if !ok || c.Fn != "nef.destroy" {
			continue
		}
		if mv, ok := fn.Body[i+1].(*ir.Mov); ok && mv.S == ir.K(0) && mv.D == c.Args[0] {
			cleared++
		}
	}
	if cleared != len(all) {
		t.Fatalf("还完要抹源位置，%d 笔里只抹了 %d 笔", len(all), cleared)
	}
}

// TestDestroyBlockOrder 验证"析构顺序为声明的反序"：块里两个登记，先声明的后释放。
func TestDestroyBlockOrder(t *testing.T) {
	p := progOf(t, `
class Bag {
    n:int
}
method f():int {
    let a:list<int> = [1]
    let b:list<int> = [2]
    return length(a) + length(b)
}
console.print(f())
`)
	fn := findFn(p, "f")
	if fn == nil {
		t.Fatal("函数 f 没排出来")
	}
	slots := lastRun(fn)
	if len(slots) != 2 {
		t.Fatalf("块尾该两笔析构：%v", slots)
	}
	// 后声明的 b 先释放：两笔里第一笔的槽号必须比第二笔大（帧上的槽按自上而下分配）
	if slots[0] <= slots[1] {
		t.Fatalf("析构该按声明的反序走：%v", slots)
	}
}

// TestStringOwnerFreesDirectly 验证文本一类的释放方式：登记成持有者的串走 `@free`，不走 `nef.destroy`。
//
// 分成两条路径的原因：拆块路径按形状表向内遍历，而"从容器位置与字段里读出来的串"按规则不算交出
// （那个位置还是持有者），顺着形状表去拆串就会释放别人持有的内存。串自身内部没有别的持有者，
// 所以直接持有者一侧一次 @free 就够；位置里、字段里的串这一版不释放（泄漏，但安全）。
func TestStringOwnerFreesDirectly(t *testing.T) {
	p := progOf(t, `
method f():int {
    let s:string = "甲" + "乙"
    let xs:list<string> = ["丙", "丁"]
    return length(s) + length(xs)
}
console.print(f())
`)
	fn := findFn(p, "f")
	var freed, destroyed []string
	for _, in := range fn.Body {
		c, ok := in.(*ir.Call)
		if !ok {
			continue
		}
		switch c.Fn {
		case "@free":
			freed = append(freed, c.Args[0].String())
		case "nef.destroy":
			destroyed = append(destroyed, c.Args[0].String())
		}
	}
	if len(freed) == 0 {
		t.Fatalf("串的持有者该走 @free，实际一笔都没有（nef.destroy：%v）", destroyed)
	}
	for _, f := range freed {
		for _, d := range destroyed {
			if f == d {
				t.Fatalf("同一个槽既走 @free 又走 nef.destroy：%s", f)
			}
		}
	}
	got, code := run(t, `
method f():int {
    let s:string = "甲" + "乙"
    let xs:list<string> = ["丙", "丁"]
    return length(s) + length(xs)
}
console.print(f())
`)
	if want := "4\n"; got != want || code != 0 {
		t.Fatalf("跑出来不对：%q 码 %d", got, code)
	}
}

// TestTextOwnersAcrossShapesRun 是端到端的对照：直接持有者、装进字面量的、位置里的、字段里的串
// 在同一段循环里跑三千遍，数值必须正确、不能崩溃——双重释放这种错误在这种圈数下最容易暴露（空闲链表
// 一旦被破坏，下一圈取回的块就属于别人）。
func TestTextOwnersAcrossShapesRun(t *testing.T) {
	struct_ := `
struct Card {
    title:string
}
class Note {
    body:string
    tags:list<string>
}
`
	got, code := run(t, struct_+`
let n:int = 0
for (i:int in 0 until 3000) {
    let head:string = "头" + "尾"
    let c:Card = Card { title: head }
    let note:Note = new Note(body: "正文", tags: ["甲", "乙"])
    let tail:string = c.title + "！"
    n = n + length(head) + length(c.title) + length(note.tags) + length(note.body) + length(tail)
}
console.print(n)
`)
	// 每圈 2 + 2 + 2 + 2 + 3 = 11
	if want := "33000\n"; got != want || code != 0 {
		t.Fatalf("混着装的文本跑三千圈不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestDestroyStressRun 用端到端运行验证结果：一万圈，每圈造一批新对象——list、
// 字典、class、struct、嵌套 list——块尾应把它们全部释放，而下一圈新分配的块必须
// 是清零的（`new C()` 的字段默认值是零；未启用空闲链复用时新分配的内存本来就是清零的，
// 启用复用之后由 @alloc 负责抹零）。任何一处提前释放、释放错、或者漏了清零，这里的数字都会漂移。
func TestDestroyStressRun(t *testing.T) {
	got, code := run(t, `
struct Pair {
    a:int
    b:int
}
class Row {
    n:int
    kids:list<int>
}

let total:int = 0
let rows:list<int> = []
for (i:int in 0 until 10000) {
    let acc:list<int> = [i]
    let d:dict<string, int> = {"甲": i}
    let p:Pair = Pair { a: i, b: 0 - i }
    let r:Row = new Row(n: i, kids: [i, i])
    acc.add(p.a + p.b + d["甲"] + r.n)
    total = total + acc[0]
    rows.add(r.kids[1])
    if (i > 3) {
        let inner:list<int> = [7]
        total = total + inner[0]
    }
}
console.print(total, length(rows), rows[0], rows[9999])
`)
	// 每一圈 total 加的是 acc[0]，也就是 i：0+1+…+9999 = 49995000。
	// i>3 的那些圈（共 9996 个）再各加一笔 inner[0] = 7 → 49995000 + 69972。
	want := "50064972 10000 0 9999\n"
	if got != want || code != 0 {
		t.Fatalf("一万圈造与还不对了：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// paramborrow_test.go 覆盖这一条：自己写的函数与方法的每一个参数都是只读借用，
// `ref` 那一个记号取消（全语言剩下的所有权记号是零个）。
//
// 规则与先前那三处（标准库入参、遍历变量、方法接收者）相同：借的一方拿不到所有权，
// 也就毁不掉原来那一处。能读、能就地改、能连着调用；`return` 出去、写进字段、装进容器、
// 装箱、交进一个要拿走的位置，那几处是编译错误。
// 两档不在这一条里：文本走共用（那一个位置多一个读者，文本不可变，不多出写入者），
// 系统给的句柄那一个位置交接出（否则被调方到帧尽头关一次、调用方手里那一个名字也跟着关一次）。
//
// en: paramborrow_test.go covers item 127-3.1: every parameter of a user-written function or
// method is a read-only borrow, and the ref marker is gone (the language keeps zero ownership
// markers). The rule matches the three places that already borrowed - a standard library
// argument, a loop variable, a method receiver. A borrower gets no ownership, so it cannot
// destroy the original place: it may read, modify in place and call on, while returning it,
// storing it into a field, putting it into a container, boxing it, and handing it to a position
// that takes are compile errors. Two tiers sit outside this rule: text is shared (one more
// reader of an immutable block adds no writer), and a system handle's position transfers
// (otherwise the callee closes once at the end of its frame and the caller's name closes once
// too).
package check

import "testing"

func TestParamKeepsTheCallerName(t *testing.T) {
	expectOK(t, "method q(s:string) { console.print(s) }\nlet a:string = \"甲\"\nq(a)\nq(a)\nconsole.print(a)\n")
	expectOK(t, "method q(xs:list<int>) { console.print(length(xs)) }\nlet a:list<int> = [1, 2]\nq(a)\nq(a)\nconsole.print(length(a))\n")
	expectOK(t, "class C { n:int }\nmethod q(c:C) { console.print(c.n) }\nlet o:C = new C(n: 1)\nq(o)\nq(o)\nconsole.print(o.n)\n")
	expectOK(t, "method q(b:bytes) { console.print(length(b)) }\nlet buf:bytes = bytes.new(8)\nq(buf)\nq(buf)\nconsole.print(length(buf))\n")
	// 系统给的句柄走的是交出那一条：调用方那一个名字第二次写就报"已经交出去了"
	expectErr(t, "method q(l:Listener) { }\nlet s:Listener = net.listen(9301, 5000)\nq(s)\nq(s)\n",
		"s 已经交出去了，这个名字不能再写")
}

func TestParamRefusesToTakeAway(t *testing.T) {
	// 文本那几处是共用：`return` 出去、写进字段、装进容器都只多一个读它的位置，
	// 那一块从这一句起谁都不归还，所以文本交得出去。装箱那一处不算——
	// 盒子里那一个位置要靠类型字认回来，文本进不了盒子（那是拆箱认类型字那一条，与归属无关）。
	expectOK(t, "method q(s:string):string { return s }\n")
	expectOK(t, "class W { t:string }\nmethod q(s:string) { let w:W = new W(t: s) }\n")
	expectOK(t, "method q(xs:list<string>, one:string) { xs.add(one) }\n")
	// 文本进不了盒子：那一条与归属无关（拆箱要靠类型字认回来），报的是装箱那一句
	expectErr(t, "method q(s:string) { let box:object = s }\n", "这里要的是 object，给的是 string；类型之间不自动转换")
	// 可变的那一种照旧拒绝，各有一句报错文案
	expectErr(t, "class C { }\nmethod q(c:C) { let o:object = c as object }\n",
		"读出来的那一份是只读借用，不能收到名字里、写进字段或者交给调用方：能摘出来的位置用 remove_at、d.take 或 j.take 摘出来")
	expectOK(t, "class C { n:int }\nmethod q(c:C):boolean { return (c as object) is C }\n")
	// 借来的那一个对象句柄本身交不出去：那一份是调用方手里的
	expectErr(t, "class Bag {\n    n:int\n}\nmethod q(c:Bag) { let out:Bag = c }\n", "参数 c 是只读借用，不能交出去")
	// 借来的那一个对象手里的字段也不由它支配
	expectErr(t, "class Box {\n    item:list<int>\n}\nmethod q(b:Box) { let out:list<int> = b.item }\n",
		"参数 b 是只读借用，不能把里面的东西交出去")
	// 借来的容器里读一个位置：元素是文本时那一个位置不改动、也不多出可归还的持有者，所以接得住；
	// 元素是可变的那一种时照旧拒（借来的那一个容器本身仍读得到）。
	expectOK(t, "method q(xs:list<string>) { let out:string = xs[0]\n    console.print(out, xs[0]) }\n")
	expectErr(t, "class Bag {\n    n:int\n}\nmethod q(xs:list<Bag>) { let out:Bag = xs[0] }\n", "读出来的那一份是只读借用，不能收到名字里、写进字段或者交给调用方：能摘出来的位置用 remove_at、d.take 或 j.take 摘出来")
	// 借来的那一份交不进容器那一个位置：那一个位置要自己拿着一份
	expectErr(t, "method q(xs:list<int>) { let bag:list<list<int>> = [xs] }\n",
		"参数 xs 是只读借用，不能交出去")
}

func TestParamAcceptsEveryBorrowSource(t *testing.T) {
	// 借来的值交给另一个借的一方：不产生新持有者，所以合法
	expectOK(t, "method b(s:string) { console.print(s) }\nmethod a(s:string) { b(s) }\na(\"甲\")\n")
	expectOK(t, "method b(s:string) { console.print(s) }\nlet xs:list<string> = [\"甲\"]\nfor (one:string in xs) { b(one) }\n")
	expectOK(t, "class P { n:string\n    method shout(other: P) { console.print(other.n) }\n}\nlet p:P = new P(n: \"甲\")\np.shout(p)\nconsole.print(p.n)\n")
	// `export shared` 的那一个名字交不进参数：那一份是跨文件共用的一处可变状态
	expectErr(t, "export shared bag:list<string> = []\nmethod q(b:list<string>) { console.print(length(b)) }\nq(bag)\n",
		"bag 是 export shared 的名字，只能就地用，不能当参数交出去")
	// 字段读进借用的位置：那一个位置不变（只读借用只管收方要不要拿走那一份）
	expectOK(t, "class H { t:string }\nmethod q(s:string) { console.print(s) }\nlet h:H = new H(t: \"甲\")\nq(h.t)\nconsole.print(h.t)\n")
	// 就地改是正当用法：改的是调用方那一份（与标准库入参同一条待遇）
	expectOK(t, "method q(xs:list<string>) { xs.add(\"乙\") }\nlet items:list<string> = [\"甲\"]\nq(items)\nconsole.print(length(items))\n")
	// 带默认值那一档不受影响：默认值是这一刻才造出来的那一份
	expectOK(t, "method q(s:string = \"甲\") { console.print(s) }\nq()\n")
	// 值类型的参数与引用类型的参数混着写：那一份是整份拷贝，借与交出是同一件事
	expectOK(t, "method q(head:string, tail:int) { console.print(head, tail) }\nlet a:string = \"甲\"\nq(a, 1)\nconsole.print(a)\n")
}

// TestInterfaceParamShape 钉的是接口那一边不再有 `ref`：实参一律改成只读借用之后两侧写的形状一样，
// 所以先前那一条"一个写一个不写就拒绝"跟着作废，只剩类型与参数个数还得对上。
//
// en: TestInterfaceParamShape pins that the interface side no longer spells ref. After item
// 127-3.1 both sides write the same shape, so the old refusal for a one-sided marker is void and
// only the types and the parameter count still have to line up.
func TestInterfaceParamShape(t *testing.T) {
	expectOK(t, "interface A {\n    draw(s:string)\n}\nclass C base A {\n    f:float\n    method draw(s:string) { console.print(s) }\n}\n")
	expectErr(t, "interface A {\n    draw(s:string)\n}\nclass C base A {\n    f:float\n    method draw(n:int) { console.print(n) }\n}\n",
		"C.draw 的第 1 个参数该叫 s")
}

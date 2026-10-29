// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// refparam_test.go 覆盖 `ref` 这一档只读借用参数。
//
// 语言先前只把借用开在三处：标准库的入参、遍历变量、方法接收者。自己写的函数收引用类型
// 一律交出，调用方那个名字当场失效。`ref` 开的是第四处：函数只想读一读、不留那份值。
// 规则与前三个相同——借的一方拿不到所有权，也就毁不掉原来那一处；能读、能就地改、
// 能连着调，`return`、写进字段、装进容器、装箱、交进一个要拿走的参数，五处都是编译错误。
//
// en: refparam_test.go covers the read-only borrow parameter written `ref`. The
// language used to open borrowing in three places only: a standard-library argument, a loop
// variable, and a method receiver. A function the user writes took every reference-typed
// argument by handover, so the caller's name died on the spot. `ref` opens a fourth place: a
// function that only wants to read, and to keep nothing. The rule is the same one the first
// three follow - a borrower gets no ownership, so it cannot destroy the original place: it may
// read, modify in place and call on, while `return`, storing into a field, putting into a
// container, boxing, and handing to a parameter that takes are all compile errors.
package check

import "testing"

func TestRefParamKeepsTheCallerName(t *testing.T) {
	expectOK(t, "method q(ref s:string) { console.print(s) }\nlet a:string = \"甲\"\nq(a)\nq(a)\nconsole.print(a)\n")
	// 不标 ref 的那个位置照旧交出：加了 ref 也不能松动这一条
	expectErr(t, "method q(s:string) { console.print(s) }\nlet a:string = \"甲\"\nq(a)\nq(a)\n", "a 已经交出去了，这个名字不能再写")
	// 值类型标 ref 没有意义：传进去本来就是整份拷贝
	expectErr(t, "method q(ref n:int) { console.print(n) }\n", "值类型的参数不许写 ref：int 传进去本来就是整份拷贝")
	expectErr(t, "struct P { x:int }\nmethod q(ref p:P) { }\n", "值类型的参数不许写 ref：P 传进去本来就是整份拷贝")
	// 系统给的句柄不允许作为只读借用传入（Socket／Listener／Process 那一档）
	expectErr(t, "method q(ref s:Socket) { }\n", "Socket 是系统给的句柄，不许写 ref：被调方关一次，调用方那个名字就成了空句柄")
	expectErr(t, "method q(l:Listener, ref p:Process) { }\n", "Process 是系统给的句柄，不许写 ref：被调方关一次，调用方那个名字就成了空句柄")
	// ref 是关键字，不能拿去做名字
	expectErr(t, "let ref:int = 1\n", "ref 是关键字，不能拿去做名字")
}

func TestRefParamRefusesToTakeAway(t *testing.T) {
	// 五处交出在 ref 参数上全部拒绝，各有一句报错文案
	expectErr(t, "method q(ref s:string):string { return s }\n", "ref 参数 s 是只读借用，不能交出去")
	expectErr(t, "class W { t:string }\nmethod q(ref s:string) { let w:W = new W(t: s) }\n", "ref 参数 s 是只读借用，不能交出去")
	expectErr(t, "method q(ref xs:list<string>, ref one:string) { xs.add(one) }\n", "ref 参数 one 是只读借用，不能交给要拿走的那个参数")
	expectErr(t, "method q(ref s:string) { let box:object = s }\n", "ref 参数 s 是只读借用，不能交出去")
	expectErr(t, "class C { }\nmethod q(ref c:C) { let o:object = c as object }\n", "只读借用不能拆盒")
	expectErr(t, "class C { v:string }\nmethod q(ref c:C) { let out:string = c.v }\n", "ref 参数 c 是只读借用，不能把里面的东西交出去")
	// 从借来的容器里读走一个位置会把那个位置置空，所以拒绝（与遍历变量是同一条道理）；
	// 顺着借用的对象读它的容器、再读走一个位置，这一条语言本来就没拒绝（this 那一侧同一形状）。
	expectErr(t, "method q(ref xs:list<string>) { let out:string = xs[0] }\n", "ref 参数 xs 是只读借用，不能再把里面的东西交给自己")
	// 借来的交不进要拿走的：这一句与上面 `xs.add` 是同一条道理
	expectErr(t, "method sink(s:string) { }\nmethod q(ref s:string) { sink(s) }\n", "ref 参数 s 是只读借用，不能交给要拿走的那个参数")
}

func TestRefParamAcceptsEveryBorrowSource(t *testing.T) {
	// 借来的值交给另一个借的一方：不产生新持有者，所以合法
	expectOK(t, "method b(ref s:string) { console.print(s) }\nmethod a(ref s:string) { b(s) }\na(\"甲\")\n")
	expectOK(t, "method b(ref s:string) { console.print(s) }\nlet xs:list<string> = [\"甲\"]\nfor (one:string in xs) { b(one) }\n")
	expectOK(t, "class P { n:string\n    method shout(ref other: P) { console.print(other.n) }\n}\nlet p:P = new P(n: \"甲\")\np.shout(p)\nconsole.print(p.n)\n")
	// export shared 也只能就地用，交进借用这一处不算交出
	expectOK(t, "export shared bag:list<string> = []\nmethod q(ref b:list<string>) { console.print(length(b)) }\nq(bag)\nq(bag)\n")
	// 字段读进借用的位置：那个位置不变（只读借用只管收方要拿走的那一半）
	expectOK(t, "class H { t:string }\nmethod q(ref s:string) { console.print(s) }\nlet h:H = new H(t: \"甲\")\nq(h.t)\nconsole.print(h.t)\n")
	// 就地改是正当用法：改的是调用方那一份（与标准库入参同一条待遇）
	expectOK(t, "method q(ref xs:list<string>) { xs.add(\"乙\") }\nlet items:list<string> = [\"甲\"]\nq(items)\nconsole.print(length(items))\n")
	// 带默认值那一档不受影响：默认值是这一刻才造出来的那一份
	expectOK(t, "method q(ref s:string = \"甲\") { console.print(s) }\nq()\n")
	// 不标 ref 的参数与标了 ref 的参数混着写
	expectOK(t, "method q(ref head:string, tail:string) { console.print(head, tail) }\nlet a:string = \"甲\"\nlet b:string = \"乙\"\nq(a, b)\nconsole.print(a)\n")
}

func TestRefParamMatchesInterface(t *testing.T) {
	expectOK(t, "interface A {\n    draw(ref s:string)\n}\nclass C base A {\n    f:float\n    method draw(ref s:string) { console.print(s) }\n}\n")
	// 两边一个写一个不写：那是两种调用形状，直接拒绝（与接口一致性的同一条做法）
	expectErr(t, "interface A {\n    draw(s:string)\n}\nclass C base A {\n    f:float\n    method draw(ref s:string) { console.print(s) }\n}\n",
		"C.draw 的第 1 个参数 ref 写法与接口 A 的声明不一致")
	expectErr(t, "interface A {\n    draw(ref s:string)\n}\nclass C base A {\n    f:float\n    method draw(s:string) { console.print(s) }\n}\n",
		"C.draw 的第 1 个参数 ref 写法与接口 A 的声明不一致")
}

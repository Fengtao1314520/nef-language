// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// thread_test.go 覆盖 `thread` 这一组的检查面：那一条组函数的三个位置各要什么、
// 句柄那一族的拒绝规则、以及哪一些类型能跨过线程、哪一些不能。
//
// 这里只管"检查阶段就说"的那几条；实际运行那一条端到端在 `internal/lower/thread_e2e_test.go`。

func TestThreadStartShapes(t *testing.T) {
	// 写出来的函数名与 `fn` 名字两种都收：第一个位置要的是那一个函数的入口地址。
	expectOK(t, `method w(n:int):int { return n }
let a:Thread<int> = thread.start(w, 1)
let f:fn(int):int = w
let b:Thread<int> = thread.start(f, 2)
console.print(a.wait(), b.wait())
`)
	// 句柄身上那一个尖括号参数跟着被起的那个函数的返回类型走。
	expectOK(t, `method w(s:string):string { return s }
let a:Thread<string> = thread.start(w, "甲")
console.print(a.wait())
`)
	// 尖括号里填错了要按现成那一句：那是类型不符，不是"线程写错了"。
	expectErr(t, `method w(n:int):int { return n }
let a:Thread<string> = thread.start(w, 1)
`, "这里要的是 Thread<string>，给的是 Thread<int>；类型之间不自动转换")
	// 入参个数不对借的是现成那一句"收几个参数"。
	expectErr(t, `method w(n:int):int { return n }
let a:Thread<int> = thread.start(w)`,
		"thread.start 收 2 个参数，这里给了 1 个")
	// 被起的那个函数只许收一个参数：运行期那张表每一格只有一个位置留给实参。
	expectErr(t, `method w(a:int, b:int):int { return a }
let t:Thread<int> = thread.start(w, 1)`,
		"w 收 2 个参数：起一条线程要的是它只收一个参数")
	// 没有返回值也不行：句柄身上那一个尖括号参数没有出处。
	expectErr(t, `method w(n:int) { console.print(n) }
let t:Thread<int> = thread.start(w, 1)`,
		"w 没有返回值：句柄身上要写它返回的那一个类型")
	// 第一个位置不是函数（是个整数、是个容器）时说的是那一句"要的是函数名或 fn 名字"。
	expectErr(t, `let n:int = 1
let t:Thread<int> = thread.start(n, 1)`,
		"thread.start 的第一个入参要的是写出来的函数名或一个 fn 名字")
	// 泛型的顶层函数不能直接交出去：那一只实例要填什么，这一处没有凭据。
	expectErr(t, `method twice<Element>(v:Element):Element { return v }
let t:Thread<int> = thread.start(twice, 1)`,
		"twice 是泛型，先填尖括号里的类型再交出去")
	// 类型名那一侧：Thread 只带一个尖括号参数，写法与容器同一条判据。
	expectErr(t, `let t:Thread = null`, "Thread 需要 1 个尖括号参数，实际 0 个")
	expectErr(t, `let t:Thread<int, int> = null`, "Thread 需要 1 个尖括号参数，实际 2 个")
}

// TestThreadCrossTypes 覆盖能跨过线程的那一样白名单与不能的那一排。
// 判据是"这一类的归还点按线程各一份定下来没有"：值类型与文本不参与递归归还，
// `class` 句柄归谁由第 1.7.4 节第三句裁了，剩下的都还没有答复。
func TestThreadCrossTypes(t *testing.T) {
	// 值类型（含枚举与 byte 那一族）与文本、class 句柄都能走两头。
	for _, one := range []struct{ decl, arg, ret string }{
		{"method w(v:int):int { return v }", "0", "int"},
		{"method w(v:boolean):boolean { return v }", "true", "boolean"},
		{"method w(v:float):float { return v }", "1.5", "float"},
		{"method w(v:byte):byte { return v }", "2 as byte", "byte"},
		{"method w(v:unicode):unicode { return v }", "'x'", "unicode"},
		{"method w(v:string):string { return v }", `"甲"`, "string"},
		{"class Box { n:int }\nmethod w(v:Box):Box { return v }", "new Box(n: 1)", "Box"},
		{"enum Color { red }\nmethod w(v:Color):Color { return v }", "Color.red", "Color"},
	} {
		expectOK(t, one.decl+`
let t:Thread<`+one.ret+`> = thread.start(w, `+one.arg+`)
`)
	}
	// 容器、bytes、盒子、函数值各拒一句（同一句原话，换的是类型名）。
	expectErr(t, `method w(xs:list<int>):int { return length(xs) }
let t:Thread<int> = thread.start(w, [1, 2])`,
		"list<int> 不能交给另一条线程：这一类的归还点还没按线程各一份定下来")
	expectErr(t, `method w(b:bytes):int { return length(b) }
let t:Thread<int> = thread.start(w, bytes.new(4))`,
		"bytes 不能交给另一条线程：这一类的归还点还没按线程各一份定下来")
	expectErr(t, `method w(o:object):int { return 1 }
let t:Thread<int> = thread.start(w, 7 as object)`,
		"object 不能交给另一条线程：这一类的归还点还没按线程各一份定下来")
	// class 句柄身上的字段跟着那一个对象一起过去：归还点是那一个对象，第 1.7.4 节第三句已裁。
	expectOK(t, `class Node { kids:list<int> }
method w(arg:Node):int { return length(arg.kids) }
let t:Thread<int> = thread.start(w, new Node(kids: [1, 2]))`)
	// 返回值那一侧同一条判据：报的位置是被起的那个函数那一个名字。
	expectErr(t, `method w(n:int):list<int> { return [n] }
let t:Thread<int> = thread.start(w, 1)`,
		"list<int> 不能交给另一条线程：这一类的归还点还没按线程各一份定下来")
	// 函数值作为普通实参仍然拒（第一个入参那个位置是唯一开着的口子）。
	expectErr(t, `method q(n:int):int { return n }
method w(f:fn(int):int):int { return 1 }
let g:fn(int):int = q
let t:Thread<int> = thread.start(w, g)`,
		"fn(int):int 不能交给另一条线程：这一类的归还点还没按线程各一份定下来")
	// 线程句柄那一族与另三类系统句柄同档：进不了容器、进不了盒子、当不了键。
	expectErr(t, `let xs:list<Thread<int>> = []`,
		"Thread<int> 进不了 list：它是操作系统给的句柄，只待在名字、实参与返回值里")
	expectErr(t, `method w(n:int):int { return n }
let t:Thread<int> = thread.start(w, 1)
let o:object = t as object`,
		"Thread<int> 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, `let d:dict<Thread<int>, int> = {}`,
		"Thread<int> 不能当 dict 的键：这个类型不能当键")
	// 系统给的句柄当参数走的是交出那一条：那一个位置只有一个能关它的持有者。
	expectOK(t, `method w(arg:Thread<int>):int { return 1 }`)
}

// TestThreadArgIsHandedOver 覆盖交进去的那一个实参按现成的"交出"规则处理：
// 原来那个名字当场作废，`export shared` 的那一个名字与只读借用的那三种仍然拒。
func TestThreadArgIsHandedOver(t *testing.T) {
	// 交进去的那一个 class 句柄走现成的交出规则：那个名字当场不再持有，往后写它就是那句。
	expectErr(t, `class Box { n:int }
method w(arg:Box):int { return arg.n }
let b:Box = new Box(n: 1)
let t:Thread<int> = thread.start(w, b)
console.print(b.n)`,
		"b 已经交出去了，这个名字不能再写")
	expectErr(t, `class Box { n:int }
method w(arg:Box):int { return arg.n }
let b:Box = new Box(n: 1)
let t:Thread<int> = thread.start(w, b)
let c:Box = b`,
		"b 已经交出去了，这个名字不能再写")
	// 交出去之前用完再交是合法的：那一个名字在交出之后不再被读。
	expectOK(t, `class Box { n:int }
method w(arg:Box):int { return arg.n }
let b:Box = new Box(n: 1)
console.print(b.n)
let t:Thread<int> = thread.start(w, b)
console.print(t.wait())
`)
	// `export shared` 的名字挡在线程体外：那是整个程序共用的一格。
	expectErr(t, `class Box { n:int }
export shared box:Box = new Box(n: 1)
method w(arg:Box):int { return arg.n }
let t:Thread<int> = thread.start(w, box)`,
		"box 是 export shared 的名字，只能就地用，不能交出")
	// 遍历得到的那一份是只读借用，交不进线程。
	expectErr(t, `class Box { n:int }
method w(arg:Box):int { return arg.n }
let xs:list<Box> = [new Box(n: 1)]
for (one:Box in xs) {
    let t:Thread<int> = thread.start(w, one)
    console.print(t.wait())
}`,
		"遍历交出的 one 是只读借用，不能交出去")
	// 值类型不是交出：原来那个名字照旧能用（与递给普通函数同一条）。
	expectOK(t, `method w(v:int):int { return v }
let n:int = 5
let t:Thread<int> = thread.start(w, n)
console.print(n + t.wait())
`)
}

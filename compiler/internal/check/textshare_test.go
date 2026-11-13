// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// textshare_test.go 钉文本走共享这一档之后的四处放行。
//
// 文本不可变，所以"多一个句柄"不会多出第二个写入者；归属那一问只剩"谁归还"，而共享过的文本块
// 到进程结束都不归还，因此赋值、实参、读一个位置、写进字段与容器这四处都不再作废原来的名字，
// 也不需要 `+ ""` 那种现成的拷贝写法。值类型那一族照旧整份拷贝，`class`、容器、`bytes`、`Json`
// 与四种系统句柄照旧交出——它们可变，多一个持有者就要两个写入者或者两次归还。
//
// en: textshare_test.go pins the four places a text may now be used twice. A text is immutable,
// so an extra handle adds no second writer; the only ownership question left is who releases the
// block, and a block that was shared is never returned - it lives until the process ends. So
// assignment, an argument, reading a position and storing into a field or container no longer
// void the original name, and the `+ ""` copy idiom is not needed. Value types keep copying as
// before; `class`, containers, `bytes`, `Json` and the four system handles keep moving, because
// they are mutable and a second holder would mean either two writers or two releases.
package check

import "testing"

func TestTextAssignmentKeepsBothNames(t *testing.T) {
	expectOK(t, "let t:string = \"甲\" + \"乙\"\nlet u:string = t\nconsole.print(t, u)\n")
	expectOK(t, "method q(s:string) { console.print(s) }\nlet a:string = \"甲\"\nq(a)\nq(a)\nconsole.print(a)\n")
	// 不需要 ref 也不需要 + ""：文本这一档本来就是共享
	expectOK(t, "method q(s:string) { console.print(s) }\nlet a:string = \"甲\"\nq(a)\n")
}

func TestTextFromAPositionKeepsTheSource(t *testing.T) {
	expectOK(t, "let xs:list<string> = [\"甲\", \"乙\"]\nlet p:string = xs[0]\nconsole.print(p, xs[0], length(xs))\n")
	expectOK(t, "class C { name:string }\nlet o:C = new C(name: \"甲\")\nlet n:string = o.name\nconsole.print(n, o.name)\n")
	expectOK(t, "let d:dict<string, string> = {\"甲\": \"乙\"}\nlet v:string = d[\"甲\"]\nconsole.print(v, d[\"甲\"])\n")
}

func TestTextStoredIntoPositions(t *testing.T) {
	expectOK(t, "class Box { name:string }\nlet txt:string = \"甲\"\nlet b:Box = new Box(name: txt)\nconsole.print(b.name, txt)\n")
	expectOK(t, "let bag:list<string> = []\nlet txt:string = \"甲\"\nbag.add(txt)\nbag.add(txt)\nconsole.print(length(bag), txt)\n")
}

func TestMutableTypesStillMove(t *testing.T) {
	// 交出的那五处写进位置仍然作废名字：接给一个名字、写进字段、装进容器那一个位置、装箱、return。
	// 参数那一处不在此列，所以这三段用容器那一个位置写。
	expectErr(t, "let a:list<int> = [1, 2]\nlet keep:list<int> = a\nconsole.print(length(keep), length(a))\n",
		"a 已经交出去了，这个名字不能再写")
	expectErr(t, "class C { }\nlet o:C = new C()\nlet bag:list<C> = [o]\nconsole.print(o)\n",
		"o 已经交出去了，这个名字不能再写")
	expectErr(t, "let buf:bytes = bytes.new(8)\nlet keep:bytes = buf\nconsole.print(length(keep), length(buf))\n",
		"buf 已经交出去了，这个名字不能再写")
	// 文本仍然进不了盒子：那一条与归属无关，是拆箱要认类型字
	expectErr(t, "let o:object = \"甲\" as object\n", "串与非串之间不走 as：变串写 number.to_string，读回数字写 text.to_int")
}

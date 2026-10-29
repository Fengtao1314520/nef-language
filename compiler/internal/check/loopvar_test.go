// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// loopvar_test.go 覆盖 `for` 循环变量的类型标注真的算数：写出来的那一个类型必须就是
// 被遍历对象自己的类型，不符就是编译错误。标注先前会被丢掉：
// `for (x:string in xs)` 里 `xs` 是 `list<int>` 也过了检查，跑起来照印 1、22。
//
// 报错复用"类型之间不自动转换"那一句，与赋值、实参、下标同一条规则；不另起一句新文案。

func TestLoopVarTypeMustMatch(t *testing.T) {
	// 四种能遍历的东西，标注对了照旧通过。
	expectOK(t, "let xs:list<int> = []\nfor (x:int in xs) {\n    console.print(x)\n}\n")
	expectOK(t, "let d:dict<string, int> = {}\nfor (k:string in d) {\n    console.print(k)\n}\n")
	expectOK(t, "let s:string = \"甲乙\"\nfor (ch:unicode in s) {\n    console.print(ch)\n}\n")
	expectOK(t, "let bs:list<byte> = []\nfor (b:byte in bs) {\n    console.print(b)\n}\n")
	expectOK(t, "class P {\n    n:int\n}\nlet ps:list<P> = []\nfor (p:P in ps) {\n    console.print(p.n)\n}\n")
	// 计数循环给出的是 int，标注也得是 int。
	expectOK(t, "for (i:int in 0 until 3) {\n    console.print(i)\n}\n")

	// 标注不符：四种各覆盖一句。
	expectErr(t, "let xs:list<int> = []\nfor (x:string in xs) {\n    console.print(x)\n}\n",
		"这里要的是 string，给的是 int；类型之间不自动转换")
	expectErr(t, "let d:dict<string, int> = {}\nfor (v:int in d) {\n    console.print(v)\n}\n",
		"这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, "let s:string = \"甲乙\"\nfor (ch:int in s) {\n    console.print(ch)\n}\n",
		"这里要的是 int，给的是 unicode；类型之间不自动转换")
	expectErr(t, "class P {\n    n:int\n}\nclass Q {\n    m:int\n}\nlet ps:list<P> = []\nfor (q:Q in ps) {\n    console.print(q.m)\n}\n",
		"这里要的是 Q，给的是 P；类型之间不自动转换")
	// 盒子不是"什么都能装"的入口：容器里那一样不是 object，标注写 object 就是不符。
	expectErr(t, "let xs:list<int> = []\nfor (x:object in xs) {\n    console.print(x)\n}\n",
		"这里要的是 object，给的是 int；类型之间不自动转换")
	// 计数循环用同一条规则。
	expectErr(t, "for (i:string in 0 until 3) {\n    console.print(i)\n}\n",
		"这里要的是 string，给的是 int；类型之间不自动转换")
}

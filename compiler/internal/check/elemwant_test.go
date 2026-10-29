// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// elemwant_test.go 覆盖容器字面量的元素类型传递：容器字面量里每一项该是哪一种类型，
// 要从左边的标注里尖括号那一个传下来。传下来之前，"字面量落在要 byte 的位置上
// 按 byte 认"这一条只覆盖整个表达式那一层，覆盖不到元素，于是 `let xs:list<byte> = [0, 255]`
// 报的是"这里要的是 list<byte>，给的是 list<int>"。
package check

import "testing"

func TestByteLiteralInContainerLiteral(t *testing.T) {
	// 三种容器都能把元素类型传下去
	expectOK(t, "let xs:list<byte> = [0, 255]\nconsole.print(xs)\n")
	expectOK(t, "let d:dict<string, byte> = {\"a\": 7}\nconsole.print(d)\n")
	expectOK(t, "let s:hashset<byte> = {1, 2}\nconsole.print(s)\n")
	// 键那一侧也传得到：dict<byte, int> 的键位置
	expectOK(t, "let d:dict<byte, int> = {200: 1}\nconsole.print(d)\n")
	// 嵌套一层照样传得下去
	expectOK(t, "let xss:list<list<byte>> = [[0, 255]]\nconsole.print(xss)\n")
	// 位置不是 byte 时，字面量照旧按它自己的类型认
	expectOK(t, "let xs:list<int> = [1, 300]\nconsole.print(xs)\n")

	// 越界当场报，报的还是 byte 范围那一句，行列指向那一个数
	expectErr(t, "let xs:list<byte> = [300]\n", "byte 只能装 0～255，这里是 300")
	expectErr(t, "let d:dict<string, byte> = {\"a\": 256}\n", "byte 只能装 0～255，这里是 256")
	// 混了还是那一句，元素类型是认完的那一种
	expectErr(t, "let xs:list<byte> = [1, \"a\"]\n", "一个 list 只能装一种元素：byte 与 string 混了")
	// 非字面量的数不能靠这一条通过：算出来的 int 落不进 byte 的位置。
	// 报错在容器那一层（list<byte> 对 list<int>），不在元素那一层——这一处不能悄悄收下。
	expectErr(t, "let n:int = 300\nlet xs:list<byte> = [n]\n", "这里要的是 list<byte>，给的是 list<int>；类型之间不自动转换")
	// 标注写的是 list<int>，就不能把 byte 的认定套上来
	expectErr(t, "let xs:list<int> = [1, 2.5]\n", "一个 list 只能装一种元素：int 与 float 混了")
}

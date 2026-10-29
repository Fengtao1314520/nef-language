// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// TestRefuseNoValueArg 覆盖"不交回值的动作不能放在要值的位置上"这一条。
//
// 带类型的参数位置早就拒绝了（"这里要的是 int，给的是 无返回值"）；这几处走的是
// 没有写着类型的落点——`console.print` 的变长参数与 `length` 那种跟着实参走的类型——
// 先前检查放行、到发码那一步才报"后端还没做到这些：印不出 无返回值"。
func TestRefuseNoValueArg(t *testing.T) {
	expectErr(t, "method show() { console.print(\"甲\") }\nconsole.print(show())\n", "show 不返回值，那一个位置上没有值可用")
	expectErr(t, "let l:list<int> = [1, 2]\nconsole.print(l.sort())\n", "sort 不返回值，那一个位置上没有值可用")
	expectErr(t, "let l:list<int> = [1, 2]\nconsole.print(1, l.add(3), 2)\n", "add 不返回值，那一个位置上没有值可用")
	expectErr(t, "method show() { console.print(\"甲\") }\nconsole.print(length(show()))\n", "show 不返回值，那一个位置上没有值可用")
	// 写着类型的那一个位置由 exprExpected 拒绝，用的是既有那一句（这一条不改它）
	expectErr(t, "method show() { console.print(\"甲\") }\nlet s:string = \"甲\"\nconsole.print(s.starts_with(show()))\n", "这里要的是 string，给的是 无返回值；类型之间不自动转换")
	expectErr(t, "method show() { console.print(\"甲\") }\nconsole.print_error(show())\n", "show 不返回值，那一个位置上没有值可用")
	expectErr(t, "console.print(environment.unset(\"NF_UNSET_DEMO\"))\n", "unset 不返回值，那一个位置上没有值可用")
	// 单独成一句是正当用法（这一句没人接住值，句尽头就析构），不能顺手拒绝掉
	expectOK(t, "let l:list<int> = [1, 2]\nl.sort()\nconsole.print(l[0], length(l))\n")
	// 返回值的那些照旧走得通：`length` 的类型跟着实参走，实参有形状就行
	expectOK(t, "let l:list<int> = [1, 2]\nl.add(3)\nconsole.print(length(l), l[2])\n")
}

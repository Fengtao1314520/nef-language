// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// forwarddecl_test.go 钉顶层那一种先读后写。
//
// 名字在 hoistTopDecls 那一趟就登记好，所以先读后写的句子过得了类型检查，运行读到的是还没算出
// 初值的那个静态槽：引用类型是空句柄，值类型是零值，屏幕上不报一个字。现在这一种在编译期拒绝，
// 报错带上初值所在那一行。函数体里不查：函数在顶层跑完之后才被调用。
//
// en: forwarddecl_test.go pins the read-before-initialised case at the top level. Names are
// registered by hoistTopDecls, so such a line passes type checking and at run time reads a static
// slot whose value was never computed: a null handle for a reference type, zero for a value type,
// with no message on screen. The compiler now refuses it and names the line that holds the
// initialiser. A function body is not checked - a function runs after the top level.
package check

import "testing"

func TestForwardTopLevelLetRefused(t *testing.T) {
	expectErr(t, "console.print(ahead)\nlet ahead:string = \"后面才声明\"\n",
		"ahead 在本文件第 2 行才有初值，这一句先读了它：把这一句挪到那一句后面")
	// 中间隔着别的语句也一样拒，判据是初值那一句在后
	expectErr(t, "let n:int = 1\nconsole.print(n, later)\nlet later:list<int> = [1, 2]\n",
		"later 在本文件第 3 行才有初值，这一句先读了它：把这一句挪到那一句后面")
	// 值类型同样：读到的是零值，不是"没初始化"就没事
	expectErr(t, "console.print(count)\nlet count:int = 7\n",
		"count 在本文件第 2 行才有初值，这一句先读了它：把这一句挪到那一句后面")
}

func TestTopLevelOrderOKStillPasses(t *testing.T) {
	expectOK(t, "let first:string = \"甲\"\nconsole.print(first)\n")
	// 函数体里读后面声明的顶层名字：函数是顶层跑完之后才调用的，合法
	expectOK(t, "method show():string { return later }\nlet later:string = \"后面声明\"\nconsole.print(show())\n")
	// 名字先用后声明的是函数与类型，那一条不分先后，照旧合法
	expectOK(t, "console.print(deep())\nmethod deep():int { return 3 }\n")
}

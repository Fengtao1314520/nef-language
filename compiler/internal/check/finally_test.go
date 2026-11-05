// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// TestFinallyRefusals 钉 `finally` 那一段的四条拒绝与两条正当写法。
//
// 那一段只负责收尾：在里面改变出口（`return`、`break`、`continue`）会把正在往外传的那一条顶掉，
// 三处各一句拒绝；接住块那四个名字登记在接住那一段的作用域里，这一段读不到，用的是既有那一句
// "没有 %s 这个名字"，不新增话。
//
// en: TestFinallyRefusals pins the four refusals and two legal forms around a `finally` stretch.
// That stretch only does cleanup: changing the exit from inside it (return, break, continue) would
// discard whichever exit was already on its way out, so each gets its own sentence. The four catch
// names are registered in the catch scope, so this stretch cannot read them - the existing "no such
// name" sentence covers that, and nothing new was added.
func TestFinallyRefusals(t *testing.T) {
	expectErr(t, "method pick():int {\n    try {\n        return 1\n    } finally {\n        return 2\n    }\n}\n",
		"finally 那一段里不能 return：要改变出口就写在 try 或 catch 里")
	expectErr(t, "for (i:int in 0 until 3) {\n    try {\n        console.print(i)\n    } finally {\n        break\n    }\n}\n",
		"finally 那一段里不能 break：要跳出循环就写在 try 或 catch 里")
	expectErr(t, "for (i:int in 0 until 3) {\n    try {\n        console.print(i)\n    } finally {\n        continue\n    }\n}\n",
		"finally 那一段里不能 continue：要换下一轮就写在 try 或 catch 里")
	expectErr(t, "try {\n    throw (120, \"甲\")\n} catch (code, msg, at) {\n    console.print(code)\n} finally {\n    console.print(msg)\n}\n",
		"没有 msg 这个名字")
	// 第四个名字（整条调用链的那一份文本）读不到，用的还是同一句
	expectErr(t, "try {\n    throw (120, \"甲\")\n} catch (code, msg, at, trace) {\n    console.print(trace)\n} finally {\n    console.print(trace)\n}\n",
		"没有 trace 这个名字")

	// 走完那一条路与接住那一条路都合法；没写 catch 的那一层，try 体里 return 就算走到头
	expectOK(t, "try {\n    console.print(\"甲\")\n} finally {\n    console.print(\"乙\")\n}\n")
	expectOK(t, "method pick():int {\n    try {\n        return 1\n    } finally {\n        console.print(\"收尾\")\n    }\n}\n")
	expectOK(t, "method pick():int {\n    try {\n        throw (120, \"甲\")\n    } catch (code, msg, at) {\n        return code\n    } finally {\n        console.print(\"收尾\")\n    }\n}\n")
	// 收尾里调用一条会抛出的动作，仍然只是普通语句：这一段不拒绝调用
	expectOK(t, "method risky() {\n    let s:string = null\n    console.print(length(s))\n}\n\ntry {\n    console.print(\"甲\")\n} finally {\n    risky()\n}\n")
	// 第四个名字写了就在这个作用域里可读，省掉也照样合法
	expectOK(t, "try {\n    throw (120, \"甲\")\n} catch (code, msg, at, trace) {\n    console.print(trace)\n}\n")
}

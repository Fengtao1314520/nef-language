// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// process_test.go 覆盖 `process` 这一档在检查阶段的规则：
// 两个名字是保留的、`process` 组里只有 `start` 那一条、类型上只有 `wait` 与 `close` 两条、
// 那一类句柄进不了容器也进不了盒子，也只待在名字、实参与返回值里。
//
// 运行期那一侧（实际运行出来的退出码、三个号、收掉进程）在 `internal/lower/proc_e2e_test.go`。

// TestProcessNamesAreReserved 覆盖 `process` 与 `Process` 都已进内建名字表。
func TestProcessNamesAreReserved(t *testing.T) {
	expectErr(t, "let process:int = 1\n", "process 是内建名字，不能拿去做名字")
	expectErr(t, "let Process:int = 1\n", "Process 是内建名字，不能拿去做名字")
	expectErr(t, "method process(n:int):int { return n }\n", "process 是内建名字，不能拿去做名字")
	expectErr(t, "struct Process {\n    n:int\n}\n", "Process 是内建名字，不能拿去做名字")
	expectOK(t, "let p:Process = process.start(\"cmd.exe /c exit 0\")\nconsole.print(p.wait())\n")
}

// TestProcessCallShapes 覆盖那三条的形状：`start` 收一串文本、返回一个 Process，
// 组名式只认 `start`；`wait` 返回整数、`close` 不返回任何值，两条都不收参数。
func TestProcessCallShapes(t *testing.T) {
	expectErr(t, "let p:Process = process.start(4)\n", "这里要的是 string，给的是 int；类型之间不自动转换")
	expectErr(t, "let p:Process = process.start()\n", "start 收 1 个参数，这里给了 0 个")
	expectErr(t, "let p:Process = process.make(\"x\")\n", "process 组里没有 make")
	expectErr(t, "let p:Process = process.start(\"x\")\nlet n:int = process.wait(p)\n", "process 组里没有 wait")
	expectOK(t, "let line:string = \"cmd.exe /c exit 0\"\nlet p:Process = process.start(line)\n")
	expectOK(t, "let p:Process = process.start(\"x\")\nlet n:int = p.wait()\np.close()\n")
	expectErr(t, "let p:Process = process.start(\"x\")\nlet s:string = p.wait()\n",
		"这里要的是 string，给的是 int；类型之间不自动转换")
	expectErr(t, "let p:Process = process.start(\"x\")\np.wait(1)\n", "wait 收 0 个参数，这里给了 1 个")
	expectErr(t, "let p:Process = process.start(\"x\")\np.close(1)\n", "close 收 0 个参数，这里给了 1 个")
	expectErr(t, "let p:Process = process.start(\"x\")\np.stop()\n", "Process 上没有 stop")
	expectErr(t, "let t:Process<int> = process.start(\"x\")\n", "Process 不带尖括号参数")
}

// TestProcessGoesNowhereButAName 覆盖那一档与套接字同一族拒绝规则：进不了容器、进不了盒子、
// 当不了键。能写的地方只有名字、实参与返回值。
func TestProcessGoesNowhereButAName(t *testing.T) {
	expectErr(t, "let xs:list<Process> = []\n",
		"Process 进不了 list：它是操作系统给的句柄，只待在名字、实参与返回值里")
	expectErr(t, "let d:dict<Process, int> = {}\n",
		"Process 进不了 dict：它是操作系统给的句柄，只待在名字、实参与返回值里")
	expectErr(t, "let p:Process = process.start(\"x\")\nlet o:object = p as object\n",
		"Process 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, "let p:Process = process.start(\"x\")\nconsole.print(p is Process)\n",
		"is 的右边只能写盒子里可能装的那个类型")
	// 字段与方法的参数、返回值都收（与套接字同一条拒绝规则）
	expectOK(t, "class Holder {\n    child:Process\n}\nlet h:Holder = new Holder(child: process.start(\"x\"))\n")
	expectOK(t, "method take(p:Process):int { return p.wait() }\n")
}

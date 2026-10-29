// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// proc_e2e_test.go 覆盖进程成员的三种行为：启动程序、等待结束取退出码、不等待直接释放。
//
// 三条路径各验证一件事：
// 一、`wait()` 返回子程序自身的退出码，而 `console.print` 打印的是类型名，不是那个数值；
// 二、`close()`、名字到作用域尽头、字段那块被析构，三条路径都真的会终止进程——
// 这一点只能用"它本来会在一秒后写出一个文件，释放后三秒过去，文件不存在"来验证，没有第二种手段
// （同类做法见 net_e2e_test.go 的 TestListenerScopeEndFreesPort：能否重新占用那个端口
// 才是"那次关闭生效"的证据）；
// 三、启动失败、进程表用满、空句柄访问成员，三种答复各是哪个错误码。
//
// 子进程一律用系统自带的 `cmd.exe`：这一组不依赖任何第三方程序。

// slowLine 构造那串命令行与文件路径：先等约一秒，再把 "late" 写入 out 文件。
// 两端带双引号是这一组的约定：语言不自动拆参数，也不自动加引号。
func slowLine(out string) string {
	return `let line:string = "cmd.exe /c \"ping -n 2 127.0.0.1 >nul & echo late > ` +
		filepath.ToSlash(out) + `\""
`
}

func TestProcessWaitExitCode(t *testing.T) {
	got, code := run(t, `
let p:Process = process.start("cmd.exe /c exit 7")
console.print(p)
console.print(p.wait())
let q:Process = process.start("cmd.exe /c exit 0")
console.print(q.wait())
let none:Process = null
console.print(none)
`)
	want := "Process\n7\n0\nnull\n"
	if got != want || code != 0 {
		t.Fatalf("退出码那一头不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestProcessQuoteRule 验证"带空格的路径要自己加双引号"确实可行：
// 程序名一段加引号，后面的参数照旧用空格分隔。
func TestProcessQuoteRule(t *testing.T) {
	got, code := run(t, `
let root:string = environment.get("SystemRoot")
let line:string = "\"" + root + "\\System32\\cmd.exe\" /c exit 9"
let p:Process = process.start(line)
console.print(p.wait())
`)
	if got != "9\n" || code != 0 {
		t.Fatalf("加了引号的那一条起不成：got %q 码 %d", got, code)
	}
}

// TestProcessCloseEndsTheChild 验证提前回收确实终止了进程：子进程本来会在约一秒后
// 写出那个文件，当场 close()，再等三秒，文件不存在。
//
// 不用退出码验证的原因：close() 之后那个名字已作废，再 wait() 抛 10092 号，拿不到退出码。
func TestProcessCloseEndsTheChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "close.txt")
	got, code := run(t, slowLine(marker)+`
let p:Process = process.start(line)
p.close()
time.sleep(3000)
console.print(file.exists("`+filepath.ToSlash(marker)+`"))
`)
	if got != "false\n" || code != 0 {
		t.Fatalf("收了之后那个文件还是落了（或跑不通）：got %q 码 %d", got, code)
	}
}

// TestProcessScopeEndEndsTheChild 验证名字走到作用域尽头这一条：方法里启动一个、不等待就返回，
// 语言的自动释放同样要真的终止进程。这一条覆盖降级在圈尾与帧尾发出的那次 @proc_close。
func TestProcessScopeEndEndsTheChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "scope.txt")
	got, code := run(t, slowLine(marker)+`
method fire(l:string) {
    let p:Process = process.start(l)
}
fire(line)
time.sleep(3000)
console.print(file.exists("`+filepath.ToSlash(marker)+`"))
`)
	if got != "false\n" || code != 0 {
		t.Fatalf("作用域尽头那一次没收掉进程（或跑不通）：got %q 码 %d", got, code)
	}
}

// TestProcessFieldEndsTheChild 验证 Process 进得了 `class` 的字段，而析构那块时调用的是
// `@proc_close`，不是 `@net_close`（形状表里的类型编号正是为区分这一件事）。
func TestProcessFieldEndsTheChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "field.txt")
	got, code := run(t, slowLine(marker)+`
class Holder {
    child:Process
}
method hold(l:string) {
    let h:Holder = new Holder(child: process.start(l))
}
hold(line)
time.sleep(3000)
console.print(file.exists("`+filepath.ToSlash(marker)+`"))
`)
	if got != "false\n" || code != 0 {
		t.Fatalf("字段那一头没把进程收掉：got %q 码 %d", got, code)
	}
}

// TestProcessRefused 验证三种失败路径：起不来（带系统错误号 2＝文件不存在；空命令行没有系统号）、
// 进程表用满、空句柄访问成员。
func TestProcessRefused(t *testing.T) {
	cases := []struct {
		src, want string
	}{
		{`let p:Process = process.start("没有这个程序.exe")`, "10111）：起不来那个程序（系统的号码 2）"},
		{`let p:Process = process.start("")`, "10111）：起不来那个程序"},
		{`let none:Process = null
console.print(none.wait())`, "10092）：对着空句柄取成员或调用方法"},
		{`let p:Process = process.start("cmd.exe /c exit 3")
p.close()
console.print(p.wait())`, "10092）：对着空句柄取成员或调用方法"},
	}
	for _, c := range cases {
		got, code := run(t, c.src+"\n")
		if code != 1 || !strings.Contains(got, c.want) {
			t.Fatalf("%s 得到 %q 码 %d，要含 %q", c.src, got, code, c.want)
		}
	}

	// 进程表用满：一百四十层递归每层都持有一个未释放的子进程（进程表容量为一百二十八）
	got, code := run(t, `
method hold(n:int) {
    let p:Process = process.start("cmd.exe /c exit 0")
    if (n > 0) {
        hold(n - 1)
    }
}
hold(140)
console.print("没撞")
`)
	if code != 1 || !strings.Contains(got, "10112）：同时跑着的子进程太多了，这一张表用完了") {
		t.Fatalf("表满那一头得到 %q 码 %d，要 10112 那一句", got, code)
	}
}

// TestProcessNothingOnHeap 验证这一组不在堆上分配：手上的值是进程表项，不是内存块。
// 两百圈各起、各等、各释放，累计应停留在第一块附近。
func TestProcessNothingOnHeap(t *testing.T) {
	if used := heapAt(t, `
let total:int = 0
for (i:int in 0 until 200) {
    let p:Process = process.start("cmd.exe /c exit 0")
    total = total + p.wait()
}
console.print(total)
`); used > 4000 {
		t.Errorf("两百圈各起各等：累计 %d 字节：这一档不该在堆上要东西", used)
	}
}

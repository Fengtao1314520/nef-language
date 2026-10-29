// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// main_test.go 把命令行这一侧也固定：`nf 文件.nf` 是"内存里编译完直接执行"，
// 它跑出来的东西必须与同一条链封成的 exe 一字不差。
//
// 这里只能真起子进程：产物收尾是 ExitProcess，在测试进程里跑会把测试一起带走。
package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/exetest"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/pkgload"
)

// stackLine 是遇到那条栈线时该看见的一行。那句话由运行期印；
// internal/lower 的栈测试钉的是同一句，两处各写一遍，改运行期那句话时两条测试都会失败。
const stackLine = "出错了（10093）：调用套得太深，栈用完了\n"

// nfExe 现编一个 nf 出来放在临时目录，测完随临时目录一起删除。
func nfExe(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "nf.exe")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/nf")
	cmd.Dir = moduleDir(t)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编 nf 失败：%v\n%s", err, b)
	}
	return out
}

// repoRoot 是 Go 工程再往上一级：那三份示例程序都在那里的 samples/ 下面。
func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(moduleDir(t))
}

// moduleDir 是 Go 工程那一层（里面有 go.mod），编 nf 要在那儿跑：
// 测试自己的工作目录就在 cmd/nf 里，往上两级就是它。
func moduleDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("问不出工作目录：%v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

func runNf(t *testing.T, exe string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("跑不起来 nf：%v（%s）", err, errOut.String())
		}
		code = ee.ExitCode()
	}
	if errOut.Len() > 0 {
		t.Logf("nf 的标准错误：%q", errOut.String())
	}
	return out.String(), code
}

// TestRunInMemoryMatchesExe 同一个程序两条路：内存里直跑与产物自己跑，输出要一样。
func TestRunInMemoryMatchesExe(t *testing.T) {
	nf := nfExe(t)
	root := repoRoot(t)
	src := filepath.Join(root, "samples", "shopping.nf")
	jit, code := runNf(t, nf, src)
	if jit != "共 3 件，字符数合计: 7\n算完了\n" || code != 0 {
		t.Fatalf("内存里直跑得到 %q 码 %d", jit, code)
	}
	out := filepath.Join(t.TempDir(), "shopping.exe")
	if _, code := runNf(t, nf, "build", src, "-o", out); code != 0 {
		t.Fatalf("nf build 的退出码是 %d", code)
	}
	cmd := exec.Command(out)
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("跑产物失败：%v", err)
	}
	if string(b) != jit {
		t.Fatalf("产物给 %q，内存里直跑给 %q，两条路不一样", string(b), jit)
	}
}

// TestRunPassesArgs 参数要真的进到 `args` 里，脚本名自己不算一个；
// 一个都不给时 linecount.nf 走 exit (100, …)，那个数得原样变成进程的退出码。
func TestRunPassesArgs(t *testing.T) {
	nf := nfExe(t)
	root := repoRoot(t)
	src := filepath.Join(root, "samples", "linecount.nf")
	p := filepath.Join(t.TempDir(), "喂进去.txt")
	if err := os.WriteFile(p, []byte("甲\n乙丙\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := runNf(t, nf, src, p)
	if code != 0 {
		t.Fatalf("linecount.nf带参数跑，退出码 %d，输出 %q", code, got)
	}
	for _, w := range []string{"长 1 的行有 1 行", "长 2 的行有 1 行"} {
		if !strings.Contains(got, w) {
			t.Fatalf("输出 %q 里缺了 %q", got, w)
		}
	}
	none, code := runNf(t, nf, src)
	if code != 100 || !strings.Contains(none, "要给我一个文件路径") {
		t.Fatalf("不给参数时得到 %q 码 %d，要那句提醒与码 100", none, code)
	}
}

// TestRunProjectByPath 钉"从工程目录外面指着里面的文件跑"这一条：`nf 某目录/main.nf` 里
// main.nf 写着 `import ... from "./stock.nf"`，装载、检查、降级排执行顺序三处算出来的路径
// 必须是同一个名字。若各行其道——filepath.Join 会把 "./" 抹掉，降级只在整条串的头里削
// "./"——从工程目录外面跑就会报"后端无法识别文件 某目录/./stock.nf"，cd 进目录里跑却是好的。
// 现在三处共用 check.ResolveImportPath 那一份规则。
func TestRunProjectByPath(t *testing.T) {
	nf := nfExe(t)
	src := filepath.Join(repoRoot(t), "compiler", "testdata", "toplevel", "main.nf")
	want := "3 甲乙丙 3\n"
	jit, code := runNf(t, nf, src)
	if jit != want || code != 0 {
		t.Fatalf("按路径直跑这份两文件工程：%q 码 %d，要 %q", jit, code, want)
	}
	out := filepath.Join(t.TempDir(), "toplevel.exe")
	if _, code := runNf(t, nf, "build", src, "-o", out); code != 0 {
		t.Fatalf("nf build 的退出码是 %d", code)
	}
	b, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("跑产物失败：%v", err)
	}
	if string(b) != want {
		t.Fatalf("产物给 %q，内存里直跑给 %q", string(b), jit)
	}
}

// TestRunReportsBadProgram 前端没过时不执行：错误要说清是哪一条，退出码 1。
func TestRunReportsBadProgram(t *testing.T) {
	nf := nfExe(t)
	p := filepath.Join(t.TempDir(), "坏程序.nf")
	if err := os.WriteFile(p, []byte("let x:int = \"不是数\"\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	_, code := runNf(t, nf, p)
	if code != 1 {
		t.Fatalf("坏程序的退出码是 %d，要 1", code)
	}
}

// TestVersionRunsQuietly 固定 `nf version`：脚本里拿它探"装了没有"，所以不读文件、
// 不写 stderr、退出码恒 0，输出的那一行就是"nf <版本号>"。
func TestVersionRunsQuietly(t *testing.T) {
	nf := nfExe(t)
	out, code := runNf(t, nf, "version")
	if code != 0 {
		t.Fatalf("nf version 退出码 %d，要 0", code)
	}
	line := strings.TrimRight(out, "\r\n")
	if !strings.HasPrefix(line, "nf ") {
		t.Fatalf("`nf version` 该印「nf <版本号>」，得到 %q", out)
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("版本号就该一行，得到 %q", out)
	}
}

// TestIRSubcommandPrintsDump 钉 `nf ir` 这条命令行入口：`nf ir 文件.nf` 印降级出来的中间码，
// 与单测里 `ir.Dump` 那一份一字不差，不落任何文件；前端不过就是退出码 1，不给文件是 2。
func TestIRSubcommandPrintsDump(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "两个数.nf")
	if err := os.WriteFile(src, []byte("let n:int = 2\nconsole.print(n + 1)\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := runNf(t, nf, "ir", src)
	if code != 0 {
		t.Fatalf("`nf ir` 退出码 %d，要 0", code)
	}
	if want := dumpInProcess(t, src); got != want {
		t.Fatalf("`nf ir` 印的不是 `ir.Dump` 那一份：\n got %d 字节\nwant %d 字节", len(got), len(want))
	}
	if !strings.Contains(got, "func nef.main (slots=") || !strings.HasPrefix(got, "globals=") {
		t.Fatalf("印出来的不像中间码：\n%s", got)
	}
	// 一条读取命令，不该在原地留下产物
	if b, err := os.ReadDir(dir); err == nil {
		for _, one := range b {
			if strings.HasSuffix(one.Name(), ".exe") {
				t.Fatalf("`nf ir` 落了一份产物 %s", one.Name())
			}
		}
	}
	if _, code := runNf(t, nf, "ir"); code != 2 {
		t.Fatalf("`nf ir` 不给文件该是码 2，得到 %d", code)
	}
	bad := filepath.Join(dir, "坏程序.nf")
	if err := os.WriteFile(bad, []byte("let x:int = \"不是数\"\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	if _, code := runNf(t, nf, "ir", bad); code != 1 {
		t.Fatalf("前端没过时 `nf ir` 该是码 1，得到 %d", code)
	}
}

// dumpInProcess 在测试进程里走同一条链（装载 → 检查 → 降级），返回 `ir.Dump` 那一份文本，
// 用来核对命令行印的是同一台机器算出来的东西，不是另写的一份打印实现。
func dumpInProcess(t *testing.T, path string) string {
	t.Helper()
	entry := filepath.ToSlash(path)
	prog := &check.Program{Entry: entry}
	seen := map[string]bool{}
	if !load(prog, entry, seen, pkgload.New(), "") {
		t.Fatalf("装载 %s 没过", entry)
	}
	p, ok := compileIR(prog)
	if !ok {
		t.Fatalf("降级 %s 没过", entry)
	}
	return ir.Dump(p)
}

// TestTokensSubcommandPrintsStream 钉 `nf tokens 文件.nf` 这条命令行：站点的语法高亮检查
// （site/tools-site/check-site.mjs）把它的输出当唯一参照。
// 一行一个 `类型:文字`；行尾记号打印为 `nl:\n`；eof 不印；不给文件是码 2；词法失败（字符串没闭合）
// 是码 1，并且标准错误里要说清第几行第几列。
//
// en: TestTokensSubcommandPrintsStream pins `nf tokens file.nf` — the reference the site's
// highlight check reads. One "kind:text" per line, end-of-line tokens printed as `nl:\n`,
// no eof line, exit 2 without a file, exit 1 with a line/column message on stderr when lexing fails.
func TestTokensSubcommandPrintsStream(t *testing.T) {
	nf := nfExe(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "两个数.nf")
	if err := os.WriteFile(src, []byte("let n:int = 2\nconsole.print(n + 1)\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	want := "name:let\nname:n\nop::\nname:int\nop:=\nnum:2\nnl:\\n\n" +
		"name:console\nop:.\nname:print\nop:(\nname:n\nop:+\nnum:1\nop:)\nnl:\\n\n"
	got, code := runNf(t, nf, "tokens", src)
	if code != 0 {
		t.Fatalf("`nf tokens` 退出码 %d，要 0", code)
	}
	if got = strings.ReplaceAll(got, "\r\n", "\n"); got != want {
		t.Fatalf("`nf tokens` 给\n%s\n要\n%s", got, want)
	}
	if _, code := runNf(t, nf, "tokens"); code != 2 {
		t.Fatalf("`nf tokens` 不给文件该是码 2，得到 %d", code)
	}
	bad := filepath.Join(dir, "坏文件.nf")
	if err := os.WriteFile(bad, []byte("let x = \"\n"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	cmd := exec.Command(nf, "tokens", bad)
	errOut := &strings.Builder{}
	cmd.Stderr = errOut
	if err := cmd.Run(); err == nil {
		t.Fatalf("词法不过的 `nf tokens` 居然退了 0")
	} else if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("词法不过时 `nf tokens` 该是码 1，得到 %v", err)
	}
	if s := errOut.String(); !strings.Contains(s, "第 1 行第 10 列") {
		t.Fatalf("词法错误该说清行列，标准错误得到 %q", s)
	}
}

// TestDeepRecursionSameInBothModes 钉"栈用完了"那一句在直跑与产物两条路上一字不差。
// 那条线按"这一段的栈基减去语言规定的 8 MiB"算，两边起栈的都读 `pe.StackReserve`
// 那一个数：产物头里写它，直跑起线程也按它要栈。若不对齐，症状正是
// 同一份程序一条路打印报错、另一条路被操作系统直接终止。
// 那两份程序与 internal/lower 的栈测试共用 testdata/stack/。
func TestDeepRecursionSameInBothModes(t *testing.T) {
	nf := nfExe(t)
	src := filepath.Join(moduleDir(t), "testdata", "stack", "deep.nf")
	ran, code := runNf(t, nf, src)
	if ran != stackLine || code != 1 {
		t.Fatalf("直跑收尾不对：\n got %q 码 %d\nwant %q 码 1", ran, code, stackLine)
	}
	built := filepath.Join(t.TempDir(), "deep.exe")
	if _, code := runNf(t, nf, "build", src, "-o", built); code != 0 {
		t.Fatalf("编产物没成功，退出码 %d", code)
	}
	r := exetest.RunPath(t, built)
	if r.Stdout != ran || r.Code != code {
		t.Fatalf("两条路收尾不一样：直跑 %q 码 %d；产物 %q 码 %d",
			ran, code, r.Stdout, r.Code)
	}
}

// TestLSPSubcommandSpeaksProtocol 钉 `nf lsp` 那条链路：标准输出的第一个字节就是分帧头。
// 启动时多打印任何一行（版本号、提示）都会让这一轮协议会话失效，而这种情况只有真起
// 一个进程才看得见。exit 之后进程码要为 0，编辑器一侧才不报"服务器意外退出"。
func TestLSPSubcommandSpeaksProtocol(t *testing.T) {
	nf := nfExe(t)
	cmd := exec.Command(nf, "lsp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("接不上标准输入：%v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("接不上标准输出：%v", err)
	}
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("起不来 nf lsp：%v", err)
	}

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`
	quit := `{"jsonrpc":"2.0","method":"exit"}`
	if _, err := io.WriteString(stdin, lspFrame(req)+lspFrame(quit)); err != nil {
		t.Fatalf("喂不进报文：%v", err)
	}
	stdin.Close()

	out, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("读不回报文：%v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("nf lsp 这一趟没干净收尾：%v（标准错误 %q）", err, errOut.String())
	}
	if !bytes.HasPrefix(out, []byte("Content-Length: ")) {
		t.Fatalf("标准输出的头一个字节不是分帧头，前 40 字节是 %q", out[:min(40, len(out))])
	}
	if !bytes.Contains(out, []byte(`"textDocumentSync"`)) {
		t.Errorf("initialize 的答复里没有 textDocumentSync：%q", out)
	}
	if errOut.Len() > 0 {
		t.Logf("nf lsp 的标准错误：%q", errOut.String())
	}
}

func lspFrame(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

// 两份文件各自把不同的东西引成同一个本地名字，互不覆盖：名字表的键收到
// "这份文件 + 这个名字"上——`limit` 在 main 里指向 a.nf 那一份、在 mid 里指向 b.nf 那一份，
// 若按全局名字登记，两处就会读到同一个静态槽。
func TestSameLocalNameInTwoFiles(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "a.nf", `export const limit:int = 7`)
	write(t, root, "b.nf", `export const limit:int = 9`)
	write(t, root, "mid.nf", `import { limit } from "./b.nf"

export method show():int {
    return limit
}
`)
	write(t, root, "main.nf", `import { limit } from "./a.nf"
import { show } from "./mid.nf"

console.print(limit, show())
`)
	if got := runEntry(t, exe, filepath.Join(root, "main.nf")); got != "7 9\n" {
		t.Fatalf("两份文件各自的 limit 该读到自己那一份，实际输出：%q", got)
	}
}

// 在工程目录里运行（入口只给一个光文件名 `main.nf`）时，花括号伸进包里那一句要说准。
// 包根那一条路径是从 "." 拼出来的（`./libs/greet/greet.nf`），那句 import 自己算出来的却是
// `libs/greet/greet.nf`——只差一个 "./" 就成了两个不同的键：用户拿到"找不到 import 的文件"
// 的报错，而那个文件就在盘上，用户却会去检查自己的路径写法。
func TestBraceIntoPackageSaysSoFromARelativeEntry(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/greet/greet.nf", `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 1 }
}
`)
	write(t, root, "main.nf", `import greet from "libs/greet"
import { Pair } from "./libs/greet/greet.nf"

console.print(greet.make())
`)
	out, code := nfRunIn(t, root, exe, "check", "main.nf")
	if code != 1 || !strings.Contains(out, "在包里面：引包要写 import 包名 from") {
		t.Errorf("这一句该说花括号伸不进包（%d）：%s", code, out)
	}
}

// 花括号那一种的 `from` 路径必须以 ./ 或 ../ 开头。若放行到读盘，报回来的是
// "libs/greet：读不到这个文件"——那是一条目录，不是文件，指不到根上：要改的是那个花括号。
func TestBraceImportPathMustStartWithDot(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/greet/greet.nf", `export method twice(n:int):int { return n * 2 }`)
	write(t, root, "main.nf", `import { twice } from "libs/greet"

console.print(twice(3))
`)
	out, code := nfRun(t, exe, "check", filepath.Join(root, "main.nf"))
	if code != 1 || !strings.Contains(out, `花括号那一种引的是工程里的文件，路径要 ./ 或 ../ 开头`) {
		t.Errorf("该说路径要 ./ 或 ../ 开头（%d）：%s", code, out)
	}
}

// 两个包做兄弟：`libs/greet` 引 `libs/shout`，查找路径要一直走到运行命令时所在的目录。
// 入口只给一个光文件名（`nf main.nf`）时，包根是从 "." 起算的，往上走的最后一级就是那一个目录；
// 少这一级报的是"这一句引的 libs/shout 无法识别"，而那一份包就在 main.nf 旁边。
func TestSiblingPackageFoundFromARelativeEntry(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "libs/shout/nf.json", `{"name": "shout", "version": "2.0", "deps": {}}`)
	write(t, root, "libs/shout/shout.nf", `export method shoutOnce(s:string):string { return s + "!" }`)
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {"shout": "2.0"}}`)
	write(t, root, "libs/greet/greet.nf", `import shout from "libs/shout"

export method greet(name:string):string { return shout.shoutOnce(name) }
`)
	write(t, root, "main.nf", `import greet from "libs/greet"

console.print(greet.greet("甲"))
`)
	out, code := nfRunIn(t, root, exe, "main.nf")
	if code != 0 || !strings.Contains(out, "甲!") {
		t.Errorf("站在工程目录里跑该印「甲!」（%d）：%s", code, out)
	}
}

// 包里的文件只能引同一个包里的文件。装载一侧要把包引到的每一份文件都登记成"这个包的东西"，
// 伸到包外面那一片不能跟着登记——否则真从命令行跑出"检查通过：3 个文件"，
// 交出去的容器到使用方那儿编不过（包外面那一片没进容器）。
func TestPackageFileReachingOutOfItsOwnDir(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "util.nf", `export method far():int { return 1 }`)
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/greet/greet.nf", `import { far } from "../../util.nf"

export method value():int {
    return far()
}
`)
	write(t, root, "main.nf", `import greet from "libs/greet"

console.print(greet.value())
`)
	out, code := nfRunIn(t, root, exe, "check", "main.nf")
	if code != 1 || !strings.Contains(out, "包里的文件只能引同一个包里的文件：../../util.nf 不在这个包里面") {
		t.Errorf("这一句该拦下来（%d）：%s", code, out)
	}
}

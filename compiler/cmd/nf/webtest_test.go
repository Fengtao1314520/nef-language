// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// webtest_test.go 固定 `webtest/libs/web_test` 这个位置：底下到浏览器那一层（起进程、连上、
// 把帧收发对、一条命令一个号），上面到页面与元素那一层（五种定位、点与填、断言、等待）。
//
// 七份自测用 .nf 写成，放在仓库根的 webtest/selftest 下面：
// frame_check.nf     不起浏览器：帧的编码与解码，拿定值比字节
// socket_check.nf    不起浏览器：自己listen、自己 connect，验送完为止、两段拼一条、ping 回 pong、答复夹在通知后面
// page_check.nf      不起浏览器：拿 selftest/data 那四份真答复原文验取字段与截图落成 png
// error_check.nf     不起浏览器：程序不在（10111）与等不到端口文件（200）
// live_check.nf      真起一个 headless 浏览器：两条命令、开一页、读标题、截一张图
// element_check.nf   真起一个 headless 浏览器：开 webtest/fixtures 那两份本地页面，定位、动作、断言、等待、关掉一页那三十九项
// site_check.nf      真起一个 headless 浏览器开真站：https://example.com 那八项（吃网络，红就红）
//
// 还有一份不在 selftest 下面：webtest/cases/login.nf 是"脚本运行那一层"给人看的那一份用例，
// 钉在 TestWebTestRunCheck 里（六行结果：过、过、过、跳、挂、重试后过，两份报告与一张截图）。
//
// 真起浏览器那三条用这台机器上装着的 Chrome 或 Edge（找不到就红，不跳过）。
// 只能真起子进程（与这个位置里别的测试同一条理由）：产物收尾是 ExitProcess。
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// webtestDir 是仓库根那个位置 webtest。
func webtestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "webtest")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("找不到 %s：%v", dir, err)
	}
	return dir
}

// runSelftest 跑一份自测，要它返回退出码 0 且最后一行是"全对"。
// 不比对整份输出：那几份里"过  …"那一串会随检查项增加而变，红了看的是最后那一行与尾巴。
func runSelftest(t *testing.T, nf string, script string, args ...string) string {
	t.Helper()
	argv := append([]string{filepath.Join(webtestDir(t), "selftest", script)}, args...)
	out, code := runNf(t, nf, argv...)
	if code != 0 {
		t.Fatalf("%s 没跑过：退出码 %d，输出 %q", script, code, out)
	}
	if !strings.HasSuffix(out, "全对\n") {
		t.Fatalf("%s 的最后一行不是\"全对\"：%q", script, out)
	}
	if strings.Contains(out, "\n不对") || strings.HasPrefix(out, "不对") {
		t.Fatalf("%s 的输出里有失败的行：%q", script, out)
	}
	return out
}

// passedCount 数"过  "那一串，少一项就是有一条检查没被执行到。
func passedCount(out string) int {
	return strings.Count(out, "过  ")
}

// TestWebTestFrameCheck 钉帧的编码与解码：定值两头都要一样，直跑与产物也要一样。
// 定值的出处写在 frame_check.nf 头上那一段（RFC 6455 第 5 节那三个长度档与遮码的例子，
// 加上从本机 Edge 155 真收下来的答复帧的形状）。
func TestWebTestFrameCheck(t *testing.T) {
	nf := nfExe(t)
	out := runSelftest(t, nf, "frame_check.nf")
	if got := passedCount(out); got != 24 {
		t.Fatalf("帧自测跑了 %d 项，要 24 项", got)
	}
	src := filepath.Join(webtestDir(t), "selftest", "frame_check.nf")
	exe := filepath.Join(t.TempDir(), "frame_check.exe")
	if _, code := runNf(t, nf, "build", src, "-o", exe); code != 0 {
		t.Fatalf("nf build 的退出码是 %d", code)
	}
	built, err := exec.Command(exe).Output()
	if err != nil {
		t.Fatalf("跑产物失败：%v", err)
	}
	if string(built) != out {
		t.Fatalf("产物与内存里直跑不一样：产物 %q，直跑 %q", tail(string(built)), tail(out))
	}
}

// TestWebTestSocketCheck 不起浏览器，用一对回环套接字验四件：两万个字节送出去对面
// 收回来一模一样、一条消息被切成两段时拼得回来、对面发 ping 这一侧回 pong、
// 答复前面先来一条不带 id 的通知时也认得回自己那一条。
func TestWebTestSocketCheck(t *testing.T) {
	nf := nfExe(t)
	out := runSelftest(t, nf, "socket_check.nf")
	if got := passedCount(out); got != 8 {
		t.Fatalf("收发那一组跑了 %d 项，要 8 项", got)
	}
}

// TestWebTestPageCheck 拿 selftest/data 那四份真答复原文（从本机 Chrome 155
// 取的）验取字段与截图落成 png：不浏览器也能固定"sessionId 在 result 那一格里"这件事。
func TestWebTestPageCheck(t *testing.T) {
	nf := nfExe(t)
	dataDir := filepath.Join(webtestDir(t), "selftest", "data")
	shot := filepath.Join(t.TempDir(), "shot.png")
	out := runSelftest(t, nf, "page_check.nf", dataDir, shot)
	if got := passedCount(out); got != 10 {
		t.Fatalf("取字段与截图那一组跑了 %d 项，要 10 项", got)
	}
}

// TestWebTestErrorCheck 钉两种起不来的情形：那个程序根本不在（语言那一侧 10111）、
// 程序在但不是浏览器、永远不写那个端口文件（库这一侧 200）。
// 后一种还顺带走过"失败那一趟把起的进程收掉"那一条（漏的时候实测留下 9 个进程，修完留 0 个）。
func TestWebTestErrorCheck(t *testing.T) {
	nf := nfExe(t)
	profile := filepath.Join(t.TempDir(), "profiles")
	out := runSelftest(t, nf, "error_check.nf", profile)
	if got := passedCount(out); got != 2 {
		t.Fatalf("起不来的两种情形跑了 %d 项，要 2 项", got)
	}
}

// browserPath 找这台机器上那两个默认装上去的位置之一。
// 找不到就红，不做跳过：这个位置的测试没跑过就等于没验过，安静地跳过最容易被当成过了。
func browserPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		`C:/Program Files/Google/Chrome/Application/chrome.exe`,
		`C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe`,
	}
	for _, one := range candidates {
		if _, err := os.Stat(one); err == nil {
			return one
		}
	}
	t.Fatalf("这台机器上找不到 Chrome 也找不到 Edge，那两条默认路径是 %q", candidates)
	return ""
}

// fileURL 把仓库里那一份页面写成浏览器认的那种网址。反斜杠要换成正斜杠：
// file:///C:/... 那一种写法 Chrome 与 Edge 都收，带反斜杠的收不下来。
func fileURL(t *testing.T, name string) string {
	t.Helper()
	return "file:///" + filepath.ToSlash(filepath.Join(webtestDir(t), "fixtures", name))
}

// TestWebTestLiveCheck 真起一个浏览器走一遍：起进程、等它写下调试端口、连上去、
// 不带 Origin 握手、两条浏览器层的命令按号认答复、开一页挂上去、读得出标题、
// 截一张图落成 png、收尾把自己起的那个进程带走。
func TestWebTestLiveCheck(t *testing.T) {
	browser := browserPath(t)
	nf := nfExe(t)
	profile := filepath.Join(t.TempDir(), "profile")
	out, code := runNf(t, nf, filepath.Join(webtestDir(t), "selftest", "live_check.nf"), browser, profile, fileURL(t, "form.html"))
	if code != 0 {
		t.Fatalf("真起浏览器那一条没跑过：退出码 %d，输出 %q", code, out)
	}
	if !strings.Contains(out, "过  起浏览器、连上、两条命令、开一页、截一张图，全收干净\n") {
		t.Fatalf("没看见收干净那一行：%q", out)
	}
}

// TestWebTestElementCheck 真起一个浏览器把页面与元素那一面走一遍：三种定位与复数那一份、
// 点两下与悬停与滚、填与读、属性与方框、看得见与不能用、五种断言与两种等不到的错误号、
// 两页同时在手里、截图、重新加载、等一条通知、关掉一页，三十九项。
// 这一条也是"库交出的成员每一个都真被调用过"那一份清单。
func TestWebTestElementCheck(t *testing.T) {
	browser := browserPath(t)
	nf := nfExe(t)
	profile := filepath.Join(t.TempDir(), "profile")
	out := runSelftest(t, nf, "element_check.nf", browser, profile, filepath.ToSlash(filepath.Join(webtestDir(t), "fixtures")))
	if got := passedCount(out); got != 39 {
		t.Fatalf("页面与元素那一组跑了 %d 项，要 39 项，输出尾巴 %q", got, tail(out))
	}
}

// TestWebTestExamples 真起一个浏览器把 webtest/examples 那两份跑一遍。
// 那两份是"用这门语言自己写的代码驱动浏览器"给人看的样子，跑不动的例子等于没例子。
func TestWebTestExamples(t *testing.T) {
	browser := browserPath(t)
	nf := nfExe(t)
	fixtures := filepath.ToSlash(filepath.Join(webtestDir(t), "fixtures"))
	for _, name := range []string{"form_login.nf", "list_rows.nf"} {
		profile := filepath.Join(t.TempDir(), "profile")
		out, code := runNf(t, nf, filepath.Join(webtestDir(t), "examples", name), browser, profile, fixtures)
		if code != 0 {
			t.Fatalf("%s 没跑过：退出码 %d，输出 %q", name, code, tail(out))
		}
		if !strings.HasSuffix(out, "例子跑完\n") {
			t.Fatalf("%s 的最后一行不是\"例子跑完\"：%q", name, tail(out))
		}
	}
}

// TestWebTestSiteCheck 开一个真站（https://example.com）走一遍：读标题与整页文字、
// 按 CSS 数链接、读那一个链接的 href、点它跳到另一家、在跳到的那一页上截一张图。
//
// 这一条吃网络，红就红，不做跳过。它同时是"鼠标动作要先把元素滚进视口"
// 那一条改动的凭据：无头浏览器的视口只有 800×600，那一页的链接在上边 657 像素那一行，
// 不先滚一下的话鼠标事件送过去什么也不发生（本地那两份页面都在一屏之内，测不出来）。
func TestWebTestSiteCheck(t *testing.T) {
	browser := browserPath(t)
	nf := nfExe(t)
	profile := filepath.Join(t.TempDir(), "profile")
	out := runSelftest(t, nf, "site_check.nf", browser, profile)
	if got := passedCount(out); got != 8 {
		t.Fatalf("真网站那一条跑了 %d 项，要 8 项，输出尾巴 %q", got, tail(out))
	}
}

// TestWebTestRunCheck 真起一个浏览器把 webtest/cases/login.nf 那一组跑一遍（测试脚本运行那一层）。
// 五条用例走出六行结果：过、过、过、跳、挂、重试之后过；两份报告都落得下来，
// 挂的那一条带着截图与整页文字。这一条该红的时候是退 1（那一条故意等 20 毫秒必然挂），不是崩溃。
//
// 后半截再跑一次挑跑：命令行里给一个标题，报告里就只剩那一行，退 0。
func TestWebTestRunCheck(t *testing.T) {
	browser := browserPath(t)
	nf := nfExe(t)
	fixtures := filepath.ToSlash(filepath.Join(webtestDir(t), "fixtures"))
	script := filepath.Join(webtestDir(t), "cases", "login.nf")
	profile := filepath.Join(t.TempDir(), "profile")
	outDir := filepath.Join(t.TempDir(), "out")

	out, code := runNf(t, nf, script, browser, profile, outDir, fixtures)
	if code != 1 {
		t.Fatalf("那一组该退 1（有一条故意挂）：实际退出码 %d，输出 %q", code, tail(out))
	}
	for _, want := range []string{"表单登录", "  -- 空账号也点", "  x 等那块后出来的", "→ 6 条：过 4，挂 1，跳 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("终端那份里没有 %q，输出尾巴 %q", want, tail(out))
		}
	}
	record, err := os.ReadFile(filepath.Join(outDir, "report.jsonl"))
	if err != nil {
		t.Fatalf("读不到 report.jsonl：%v", err)
	}
	lines := strings.Split(strings.TrimRight(string(record), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("report.jsonl 是 %d 行，要 6 行：%q", len(lines), record)
	}
	states := []string{"pass", "pass", "pass", "skip", "fail", "pass"}
	for i, one := range states {
		if !strings.Contains(lines[i], "\"state\":\""+one+"\"") {
			t.Fatalf("第 %d 行的状态是 %q，要 %q", i+1, one, lines[i])
		}
	}
	if !strings.Contains(lines[4], "\"code\":205") || !strings.Contains(lines[4], "case-5.png") {
		t.Fatalf("挂的那一行要带上错误号与那一张图：%q", lines[4])
	}
	shot, err := os.Stat(filepath.Join(outDir, "case-5.png"))
	if err != nil || shot.Size() < 1000 {
		t.Fatalf("那一张截图不在或太小：%v，尺寸 %+v", err, shot)
	}
	if _, err := os.Stat(filepath.Join(outDir, "case-5.txt")); err != nil {
		t.Fatalf("挂的那一条没留下整页文字：%v", err)
	}
	text, err := os.ReadFile(filepath.Join(outDir, "report.txt"))
	if err != nil || !strings.Contains(string(text), "表单登录") {
		t.Fatalf("report.txt 不对：%q，err %v", text, err)
	}

	// 挑跑那一条：只给它一个标题，报告里就只剩那一行，全过退 0。
	pickDir := filepath.Join(t.TempDir(), "out")
	pickProfile := filepath.Join(t.TempDir(), "profile")
	picked, pickCode := runNf(t, nf, script, browser, pickProfile, pickDir, fixtures, "错密码进不去")
	if pickCode != 0 {
		t.Fatalf("挑跑那一条该退 0：实际 %d，输出 %q", pickCode, tail(picked))
	}
	oneRecord, err := os.ReadFile(filepath.Join(pickDir, "report.jsonl"))
	if err != nil {
		t.Fatalf("挑跑那一份读不到 report.jsonl：%v", err)
	}
	oneLines := strings.Split(strings.TrimRight(string(oneRecord), "\n"), "\n")
	if len(oneLines) != 1 || !strings.Contains(oneLines[0], "错密码进不去") {
		t.Fatalf("挑跑那一份该只有一行：给 %q", oneRecord)
	}
}

// TestWebTestRunAllCheck 钉的是再上面那一层：webtest/run_all.nf 扫 cases 目录、一份起一个
// nf.exe 跑完、把每份 report.jsonl 读回来拼成 all.jsonl 与 all.txt。这一层本身也是这门语言写的
// （file.list、process.start／wait、file.read ＋ json.parse、exit），所以它同时是"用这门语言
// 跑一批用例"给人看的那一份。
//
// 第一趟全跑：两份九条，退出码 1（login.nf 里那一条是故意等不到的），总的报告十行（九条加一行总结），
// 每份自己那个位置的报告与那张截图都在。第二趟给第五个参数挑跑：只剩一份三条，退 0。
func TestWebTestRunAllCheck(t *testing.T) {
	browser := browserPath(t)
	nf := nfExe(t)
	root := repoRoot(t)
	cases := filepath.ToSlash(filepath.Join(root, "webtest", "cases"))
	outDir := filepath.ToSlash(filepath.Join(t.TempDir(), "all"))

	out, code := runNf(t, nf, filepath.Join(root, "webtest", "run_all.nf"), browser, cases, outDir, filepath.ToSlash(nf))
	if code != 1 {
		t.Fatalf("全跑那一趟该退 1：实际 %d，输出 %q", code, tail(out))
	}
	if !strings.Contains(out, "→ 2 份，9 条：过 7，挂 1，跳 1") {
		t.Fatalf("没看见合起来那一句总结：%q", tail(out))
	}
	merged, err := os.ReadFile(filepath.Join(outDir, "all.jsonl"))
	if err != nil {
		t.Fatalf("读不到 all.jsonl：%v", err)
	}
	mergedLines := strings.Split(strings.TrimRight(string(merged), "\n"), "\n")
	if len(mergedLines) != 10 {
		t.Fatalf("all.jsonl 是 %d 行，要 10 行（九条加一行总结）", len(mergedLines))
	}
	for _, want := range []string{`"file":"login"`, `"file":"list_page"`, `"state":"skip"`, `"state":"fail"`} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("all.jsonl 里没有 %s：%q", want, merged)
		}
	}
	if !strings.Contains(mergedLines[9], "过 7，挂 1，跳 1") {
		t.Fatalf("最后一行不是那一句总结：%q", mergedLines[9])
	}
	if _, err := os.Stat(filepath.Join(outDir, "all.txt")); err != nil {
		t.Fatalf("没有 all.txt：%v", err)
	}
	for _, one := range []string{"login", "list_page"} {
		for _, name := range []string{"report.jsonl", "report.txt"} {
			if _, err := os.Stat(filepath.Join(outDir, one, name)); err != nil {
				t.Fatalf("%s/%s 不在：%v", one, name, err)
			}
		}
	}
	shot, err := os.Stat(filepath.Join(outDir, "login", "case-5.png"))
	if err != nil || shot.Size() < 1000 {
		t.Fatalf("挂的那一条的截图不对：%v，尺寸 %+v", err, shot)
	}

	// 挑跑那一份：第五个参数是文件名（不带 .nf），逗号分隔。
	pickedDir := filepath.ToSlash(filepath.Join(t.TempDir(), "picked"))
	picked, pickCode := runNf(t, nf, filepath.Join(root, "webtest", "run_all.nf"), browser, cases, pickedDir, filepath.ToSlash(nf), "list_page")
	if pickCode != 0 {
		t.Fatalf("挑跑那一趟该退 0：实际 %d，输出 %q", pickCode, tail(picked))
	}
	oneMerged, err := os.ReadFile(filepath.Join(pickedDir, "all.jsonl"))
	if err != nil {
		t.Fatalf("挑跑那一趟读不到 all.jsonl：%v", err)
	}
	oneLines := strings.Split(strings.TrimRight(string(oneMerged), "\n"), "\n")
	if len(oneLines) != 4 {
		t.Fatalf("挑跑那一趟的 all.jsonl 是 %d 行，要 4 行（三条加一行总结）：%q", len(oneLines), oneMerged)
	}
	if strings.Contains(string(oneMerged), `"file":"login"`) {
		t.Fatalf("挑跑了 list_page 却把 login 也跑了：%q", oneMerged)
	}
	if _, err := os.Stat(filepath.Join(pickedDir, "login")); !os.IsNotExist(err) {
		t.Fatalf("没挑中的那一份不该有输出目录，实际在：%v", err)
	}
}

// TestWebTestFromContainer 钉"给别人用"那一条路：`nf pack` 把这份包打成一份容器，
// 另一个工程放进自己的 libs/ 里 import 得到、跑得对。
// 门面漏了东西、包内文件没吞进容器，都会在这一条红掉。
func TestWebTestFromContainer(t *testing.T) {
	nf := nfExe(t)
	proj := t.TempDir()
	libs := filepath.Join(proj, "libs")
	if err := os.MkdirAll(libs, 0o755); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	packed := filepath.Join(libs, "web_test.nfpack")
	out, code := runNf(t, nf, "pack", filepath.Join(webtestDir(t), "libs", "web_test", "web_test.nf"), "-o", packed)
	if code != 0 {
		t.Fatalf("nf pack 没成：退出码 %d，输出 %q", code, out)
	}
	main := "import wt from \"libs/web_test\"\n" +
		"let key:bytes = bytes.new(4)\n" +
		"key[0] = 0x37\nkey[1] = 0xFA\nkey[2] = 0x21\nkey[3] = 0x58\n" +
		"console.print(wt.build_frame(\"Hi\", key))\n" +
		"console.print(wt.reply_field(\"{\\\"id\\\":1,\\\"result\\\":{\\\"targetId\\\":\\\"ABC\\\"}}\", \"targetId\"))\n" +
		"console.print(wt.first_chars(\"abcdef\", 3))\n"
	mainPath := filepath.Join(proj, "main.nf")
	if err := os.WriteFile(mainPath, []byte(main), 0o644); err != nil {
		t.Fatalf("写主文件失败：%v", err)
	}
	// 遮码钥匙是 37 FA 21 58 时，"Hi" 那一帧按 RFC 6455 的算法是 81 82 + 钥匙 + 7F 93
	ran, ranCode := runNf(t, nf, mainPath)
	if ranCode != 0 {
		t.Fatalf("用容器里那份包没跑过：退出码 %d，输出 %q", ranCode, ran)
	}
	want := "[129, 130, 55, 250, 33, 88, 127, 147]\nABC\nabc\n"
	if ran != want {
		t.Fatalf("容器里那几个成员交回的不是这个：给 %q，要 %q", ran, want)
	}
}

// tail 取最后 200 个字节，报错的时候只带尾巴：整份输出前头全是"过  …"那一串，
// 出了错要看的是最后那几行。
func tail(s string) string {
	if len(s) <= 200 {
		return s
	}
	return "…" + s[len(s)-200:]
}

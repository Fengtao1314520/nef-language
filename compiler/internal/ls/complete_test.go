// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// complete_test.go 钉补全与悬停问到的那几件事：点号后面列得出具体的成员，
// 鼠标停在名字上认得出签名，中文混在一行里时列还数得准。
//
// 这一组样例故意用"写到一半"的源码：`p.` 后面什么都没有，正是用户输入到一半的状态。
package ls

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/check"
)

// sampleSrc 里三样都用得上：一个枚举、一个带字段与方法的类、一只容器。
const sampleSrc = "enum Kind {\n    add,\n    sub,\n    mul\n}\n\nclass Page {\n    url: string\n    width: int\n\n    method open(to: string): boolean {\n        url = to\n        return true\n    }\n\n    method close(): int {\n        return 0\n    }\n}\n\nlet p: Page = new Page(url: \"甲\", width: 2)\nlet xs: list<int> = [1, 2]\n"

// cursorAfter 停在那一个锚点写完之后：补全问的就是"接着这一点往下打"。
// 列按 UTF-16 单元数算——编辑器给的就是这个数，一行里有过中文时它与字节数不一样。
func cursorAfter(text, anchor string) Position {
	i := strings.LastIndex(text, anchor)
	if i < 0 {
		panic("样例里没有那一段：" + anchor)
	}
	nl := strings.LastIndexByte(text[:i], '\n')
	return Position{Line: strings.Count(text[:i], "\n"), Character: utf16Len(text[nl+1 : i+len(anchor)])}
}

// cursorOn 停在那一个名字的**整词**第一次不在别处出现的起始字符上：悬停问的是
// "鼠标这一点上是什么"，按整词找才不会停到 `return` 里那个 n 上。
func cursorOn(text, token string) Position {
	i := -1
	for j := strings.LastIndex(text, token); j >= 0; j = strings.LastIndex(text[:j], token) {
		after := j + len(token)
		if j > 0 && isNameByte(text[j-1]) {
			continue
		}
		if after < len(text) && isNameByte(text[after]) {
			continue
		}
		i = j
		break
	}
	if i < 0 {
		panic("样例里没有那一个独立的名字：" + token)
	}
	nl := strings.LastIndexByte(text[:i], '\n')
	return Position{Line: strings.Count(text[:i], "\n"), Character: utf16Len(text[nl+1 : i])}
}

func TestCompletionAfterADotListsMembers(t *testing.T) {
	text := sampleSrc + "p.\n"
	got := labels(Completion("/p/sample.nf", text, cursorAfter(text, "p.")))
	for _, w := range []string{"url", "width", "open", "close"} {
		if !hasLabel(got, w) {
			t.Errorf("p. 后面补不出 %s：%v", w, got)
		}
	}
	for _, w := range []string{"add", "trim", "print"} {
		if hasLabel(got, w) {
			t.Errorf("Page 身上没有 %s，却补了出来：%v", w, got)
		}
	}
}

func TestCompletionByShapeGroupEnumAndPackage(t *testing.T) {
	text := sampleSrc + "xs.\n"
	got := labels(Completion("/p/sample.nf", text, cursorAfter(text, "xs.")))
	if !hasLabel(got, "add") || !hasLabel(got, "insert_at") {
		t.Errorf("list<int> 后面补不出容器那几条：%v", got)
	}
	if hasLabel(got, "union") || hasLabel(got, "keys") {
		t.Errorf("union 与 keys 不在 list 身上：%v", got)
	}

	text = sampleSrc + "Kind.\n"
	got = labels(Completion("/p/sample.nf", text, cursorAfter(text, "Kind.")))
	if strings.Join(got, " ") != "add mul sub" {
		t.Errorf("Kind. 后面该是那三个枚举值，得到 %v", got)
	}

	text = "console.\n"
	got = labels(Completion("/p/sample.nf", text, cursorAfter(text, "console.")))
	for _, w := range []string{"print", "print_error", "read"} {
		if !hasLabel(got, w) {
			t.Errorf("console 组里补不出 %s：%v", w, got)
		}
	}

	// 包里挂名那一种：`import tr from "libs/test_run"` 之后 tr. 补得出包自己交出的东西，
	// run. 补得出那一只类身上的方法——实际的测试用例里最常写的就是这两行。
	dir := t.TempDir()
	writeIn(t, dir, filepath.Join("libs", "test_run", "nf.json"), `{"name":"test_run","version":"1.0.0"}`)
	writeIn(t, dir, filepath.Join("libs", "test_run", "test_run.nf"),
		"export class Run {\n    name: string\n\n    method step(title: string): int {\n        return 0\n    }\n}\n\nexport method begin_run(exe: string, profile: string, out_dir: string): Run {\n    return new Run(name: \"甲\")\n}\n\nmethod hidden(): int {\n    return 0\n}\n")
	entry := filepath.ToSlash(filepath.Join(dir, "case.nf"))

	text = "import tr from \"libs/test_run\"\n\nlet run: tr.Run = tr.begin_run(\"a\", \"b\", \"c\")\nrun.\n"
	got = labels(Completion(entry, text, cursorAfter(text, "run.")))
	if !hasLabel(got, "step") || !hasLabel(got, "name") {
		t.Errorf("包里的类身上补不出成员：%v", got)
	}

	text = "import tr from \"libs/test_run\"\n\ntr.\n"
	got = labels(Completion(entry, text, cursorAfter(text, "tr.")))
	if !hasLabel(got, "begin_run") || !hasLabel(got, "Run") {
		t.Errorf("tr. 后面该补出包里交出的函数与类型名：%v", got)
	}
	if hasLabel(got, "hidden") {
		t.Errorf("没 export 的 hidden 不该出现在包里那一段：%v", got)
	}
}

// 包交出来的数：`greet.` 后面列得出它，补全的说明栏与悬停读得到写在它上面那段注释。
func TestPackageConstInEditor(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, filepath.Join("libs", "greet", "nf.json"), `{"name":"greet","version":"1.0.0"}`)
	writeIn(t, dir, filepath.Join("libs", "greet", "greet.nf"),
		"// 一次读多长的上限。\n"+
			"// en: The largest run one read takes.\n"+
			"// 例：\n"+
			"// let n: int = greet.LIMIT\n"+
			"export const LIMIT: int = 4096\n\n"+
			"const priv: int = 1\n\n"+
			"export method twice(n: int): int {\n    return n * 2\n}\n")
	entry := filepath.ToSlash(filepath.Join(dir, "case.nf"))

	text := "import greet from \"libs/greet\"\n\ngreet.\n"
	items := Completion(entry, text, cursorAfter(text, "greet."))
	got := labels(items)
	if !hasLabel(got, "LIMIT") {
		t.Fatalf("greet. 后面补不出包里交出的数：%v", got)
	}
	if hasLabel(got, "priv") {
		t.Errorf("没挂 export 的 priv 不该出现在那一段里：%v", got)
	}
	if d := detailOf(items, "LIMIT"); d != "LIMIT: int" {
		t.Errorf("那一条右边那一栏写成 %q，要 LIMIT: int", d)
	}
	doc := documentationOf(items, "LIMIT")
	if !strings.Contains(doc, "The largest run one read takes.") || !strings.Contains(doc, "```nf\nlet n: int = greet.LIMIT\n```") {
		t.Errorf("补全的说明栏没带上英文段或示例：%q", doc)
	}

	read := "import greet from \"libs/greet\"\n\nlet n: int = greet.LIMIT + 1\nconsole.print(n)\n"
	h := Hover(entry, read, cursorOn(read, "LIMIT"))
	if h == nil {
		t.Fatal("悬停在 greet.LIMIT 上什么都没有")
	}
	want := "greet.LIMIT: int\n--\n一次读多长的上限。\n\nThe largest run one read takes.\n\n" +
		"```nf\nlet n: int = greet.LIMIT\n```"
	if got := hoverText(h); got != want {
		t.Errorf("悬停那一段不对\n得到：%q\n要  ：%q", got, want)
	}
}

// 补全右边那一栏与悬停看到的是同一行人写得懂的话：容器那条要带元素类型，
// 自己写的方法要带参数名。
func TestCompletionCarriesSignatures(t *testing.T) {
	text := sampleSrc + "xs.\n"
	items := Completion("/p/sample.nf", text, cursorAfter(text, "xs."))
	if d := detailOf(items, "add"); d != "add(value: int)" {
		t.Errorf("list<int>.add 那一行写成 %q，要 add(value: int)", d)
	}
	if d := detailOf(items, "first"); d != "first(): int" {
		t.Errorf("list<int>.first 那一行写成 %q", d)
	}

	text = sampleSrc + "p.\n"
	items = Completion("/p/sample.nf", text, cursorAfter(text, "p."))
	if d := detailOf(items, "open"); d != "open(to: string): boolean" {
		t.Errorf("自己写的方法那一行写成 %q，参数名要带上", d)
	}
	if d := detailOf(items, "width"); d != "width: int" {
		t.Errorf("字段那一行写成 %q", d)
	}
}

// 行首打一个还没写完的名字：这一份文件里的名字、引进来的名字、类型名、组名与关键字都算候选。
func TestCompletionOfAPlainName(t *testing.T) {
	text := sampleSrc + "\nmet\nmethod main(): int {\n    return 0\n}\n"
	got := labels(Completion("/p/sample.nf", text, cursorAfter(text, "met")))
	for _, w := range []string{"method", "xs", "p", "Page", "Kind", "console"} {
		if !hasLabel(got, w) {
			t.Errorf("%s 补不出来：%v", w, got)
		}
	}

	// 引进来的那一些名字，右边那一栏写的是它自己：函数写签名、类型写声明那一句、
	// 常量写一行声明；不写"function X"，也不把出处的绝对路径塞进这一栏。
	dir := t.TempDir()
	writeIn(t, dir, "util.nf", "export const limit: int = 8\nexport method twice(n:int):int { return n * 2 }\nexport struct Pair { left:int }\nexport method read_aloud(ref s:string) { console.print(s) }\n")
	entry := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	text = "import { limit, twice, read_aloud, Pair } from \"./util.nf\"\n\nmet\nmethod main(): int {\n    return twice(limit)\n}\n"
	items := Completion(entry, text, cursorAfter(text, "met"))
	for _, c := range []struct{ label, want string }{
		{"limit", "const limit: int"},
		{"twice", "twice(n: int): int"},
		{"read_aloud", "read_aloud(ref s: string)"},
		{"Pair", "struct Pair"},
	} {
		if d := detailOf(items, c.label); d != c.want {
			t.Errorf("%s 那一栏应是 %q，得到 %q", c.label, c.want, d)
		}
	}
	// 组名那一栏要说清它是标准库的组（它也在内建名字那张表里，晚一步登就只剩"内建名字"）。
	if d := detailOf(items, "console"); d != "标准库组" {
		t.Errorf("组名那一栏要说它是标准库的组，得到 %q", d)
	}

	// 包的挂名（`import tr from "libs/greet"` 里那个 tr）：既不是函数也没有签名，
	// 右边那一栏写它指到哪一个包。
	pkgDir := t.TempDir()
	writeIn(t, pkgDir, filepath.Join("libs", "greet", "nf.json"), `{"name":"greet","version":"1.0.0"}`)
	writeIn(t, pkgDir, filepath.Join("libs", "greet", "greet.nf"), "export method hello(): int {\n    return 1\n}\n")
	pkgEntry := filepath.ToSlash(filepath.Join(pkgDir, "main.nf"))
	pkgText := "import tr from \"libs/greet\"\n\ntr\n"
	pkgItems := Completion(pkgEntry, pkgText, cursorAfter(pkgText, "tr"))
	if d := detailOf(pkgItems, "tr"); d != "包 libs/greet" {
		t.Errorf("包的挂名那一栏要说它指到哪一个包，得到 %q", d)
	}
}

func TestHoverOnNamesAndMembers(t *testing.T) {
	for _, c := range []struct {
		text, token, want string
		std               bool
	}{
		{sampleSrc + "p.close()\n", "close", "Page.close(): int", false},
		{sampleSrc + "p.width\n", "width", "p.width: int", false},
		{sampleSrc + "console.print(p.url)\n", "print", "console.print(...values)", true},
		{sampleSrc + "xs.add(3)\n", "add", "list.add(value: int)", true},
		{sampleSrc + "\nmethod twice(n: int): int {\n    return n * 2\n}\n\nlet m: int = twice(3)\n", "twice", "twice(n: int): int", false},
	} {
		h := Hover("/p/sample.nf", c.text, cursorOn(c.text, c.token))
		if h == nil {
			t.Errorf("悬停在 %s 上什么都没有", c.token)
			continue
		}
		sig, body, _ := strings.Cut(hoverText(h), "\n--\n")
		if sig != c.want {
			t.Errorf("悬停在 %s 上得到 %q，要 %q", c.token, sig, c.want)
		}
		if !c.std {
			continue
		}
		// 内建的那一条没有声明可以挂注释，说明与示例在 check 那张表里：三段都要出现，
		// 顺序是中文、英文、一个 nf 围栏里的例子。
		secs := strings.Split(body, "\n\n")
		if len(secs) != 3 || !hasHan(secs[0]) || hasHan(secs[1]) || !strings.HasPrefix(secs[2], "```nf\n") {
			t.Errorf("%s 的说明不是中文、英文、示例三段：%q", c.token, body)
		}
	}
}

// 变量自己的那一行：顶层 let、方法里直接写的字段、参数，三种说法各不一样。
func TestHoverOnBindings(t *testing.T) {
	for _, c := range []struct{ text, token, want string }{
		{"method twice(n: int): int {\n    return n\n}\n", "n", "n: int"},
		{"class Box {\n    n: int\n\n    method show(): int {\n        return n\n    }\n}\n", "n", "n: int"},
		{"class Box {\n    n: int\n}\n\nlet p: Box = new Box(n: 1)\nconsole.print(p)\n", "p", "let p: Box"},
		{"method twice(n: int): int {\n    return n * 2\n}\n\nlet m: int = twice(3)\n", "twice", "twice(n: int): int"},
		{"class Box {\n    n: int\n}\n\nlet p: Box = new Box(n: 1)\nconsole.print(p)\n", "Box", "class Box\n\nnew Box(n: int)"},
	} {
		h := Hover("/p/sample.nf", c.text, cursorOn(c.text, c.token))
		if h == nil {
			t.Errorf("悬停在 %q 那一段上什么都没有", c.token)
			continue
		}
		if got := hoverText(h); got != c.want {
			t.Errorf("悬停在 %q 上得到 %q，要 %q", c.token, got, c.want)
		}
	}
}

// 一行里有过中文时，编辑器给的列是 UTF-16 单元数，插占位名与找名字都要按字节走：
// 差一个数就补到别的名字上，或者干脆补不出东西。
func TestCompletionAndHoverPastWideText(t *testing.T) {
	text := sampleSrc + "console.print(\"甲乙\", p.o)\n"
	items := Completion("/p/sample.nf", text, cursorAfter(text, "p.o"))
	if !hasLabel(labels(items), "open") {
		t.Errorf("同一行前面有过中文，p.o 后面就补不出 open：%v", labels(items))
	}

	hoverLine := sampleSrc + "console.print(\"甲乙丙\", p.width)\n"
	h := Hover("/p/sample.nf", hoverLine, cursorOn(hoverLine, "width"))
	if h == nil {
		t.Fatal("同一行前面有过中文，悬停 p.width 什么都没显示")
	}
	if got := hoverText(h); got != "p.width: int" {
		t.Errorf("悬停得到 %q，要 p.width: int", got)
	}
}

// 检查器那几张表在这一趟里到底填没填：填漏了的话补全只会静悄悄少东西，
// 所以这里直接问那几张表，而不是只看屏幕上有没有。
func TestFactsCarryWhatTheEditorNeeds(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, filepath.Join("libs", "greet", "nf.json"), `{"name":"greet","version":"1.0.0"}`)
	writeIn(t, dir, filepath.Join("libs", "greet", "greet.nf"), "export method twice(n: int): int {\n    return n * 2\n}\n")
	writeIn(t, dir, "util.nf", "export const one: int = 1\n")
	entry := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	text := "import greet from \"libs/greet\"\nimport { one } from \"./util.nf\"\n\nmethod main(): int {\n    return greet.twice(one)\n}\n"

	_, facts := inspect(entry, text)
	if facts == nil {
		t.Fatal("inspect 没交回结论表")
	}
	if root, ok := check.ReceiverIsPackage(entry, "greet", facts); !ok || !strings.HasSuffix(root, "greet") {
		t.Errorf("包的挂名没记下来：%q %v", root, ok)
	}
	if _, ok := facts.Imported[check.NameKey(entry, "one")]; !ok {
		t.Errorf("花括号引进来的名字不在结论表里：%+v", keysOf(facts.Imported))
	}
	// 包里的函数按规范名记，那一个规范名带着包的**绝对路径**（装载那一侧算出来的），
	// 测试里不去拼那个目录，只按尾巴找——找得到就行，键的写法不是这条要钉的东西。
	key := ""
	for k := range facts.Sigs {
		if strings.HasSuffix(k, "libs/greet/twice") {
			key = k
		}
	}
	if key == "" {
		t.Fatalf("包里的函数没进签名表：%v", sigKeys(facts))
	}
	if s := facts.Signature(key); s != "twice(n: int): int" {
		t.Errorf("包里的函数签名写成 %q", s)
	}
	if s := facts.Signature("main"); s != "main(): int" {
		t.Errorf("自己写的函数签名写成 %q", s)
	}
}

// 编辑器发一条补全，服务器要按协议回话：这一条钉的是那条对话本身
// （capabilities 里有没有声明、答复形状对不对），不是里面算出来的名字。
func TestServeAnswersCompletion(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	const text = "let xs: list<int> = [1, 2]\\nxs.\\n"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+`","languageId":"nf","version":1,"text":"`+text+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":2,"method":"textDocument/completion","params":{"textDocument":{"uri":"`+uri+`"},"position":{"line":1,"character":3}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	init := replyOf(t, msgs, "1")
	var res initResult
	if err := json.Unmarshal(init.Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	caps := res.Capabilities
	if caps.CompletionProvider == nil {
		t.Fatal("initialize 没答出 completionProvider：编辑器就不会来问补全")
	}
	if len(caps.CompletionProvider.TriggerCharacters) != 1 || caps.CompletionProvider.TriggerCharacters[0] != "." {
		t.Errorf("补全的触发键该只有点号，得到 %v", caps.CompletionProvider.TriggerCharacters)
	}
	if !caps.HoverProvider {
		t.Error("initialize 没答应 hoverProvider")
	}

	var items []CompletionItem
	if err := json.Unmarshal(replyOf(t, msgs, "2").Result, &items); err != nil {
		t.Fatalf("补全的答复形状不对：%v", err)
	}
	if !hasLabel(labels(items), "add") {
		t.Errorf("xs. 的补全里没有 add：%v", labels(items))
	}
}

// 悬停那一条单独测：它走的是**没有写到一半**的那一份文本。光标那一句语法过不去时
// 检查器根本没跑，这时候什么都不显示（宁可不显示，也不显示一句从上一轮错认来的话）。
func TestServeAnswersHover(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	const text = "let xs: list<int> = [1, 2]\\nconsole.print(xs)\\n"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+`","languageId":"nf","version":1,"text":"`+text+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":3,"method":"textDocument/hover","params":{"textDocument":{"uri":"`+uri+`"},"position":{"line":1,"character":15}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var hover HoverResult
	if err := json.Unmarshal(replyOf(t, msgs, "3").Result, &hover); err != nil {
		t.Fatalf("悬停的答复形状不对：%v", err)
	}
	if !strings.Contains(hover.Contents.Value, "list<int>") {
		t.Errorf("悬停在 xs 上得到 %q，要带它的类型", hover.Contents.Value)
	}
	if hover.Contents.Kind != "markdown" {
		t.Errorf("悬停那一段该按 Markdown 交回，得到 %q", hover.Contents.Kind)
	}
}

// replyOf 按 id 找那一条答复：答复没有 method，只能按号对。
func replyOf(t *testing.T, msgs []got, id string) got {
	t.Helper()
	for _, m := range msgs {
		if string(m.ID) == id {
			return m
		}
	}
	t.Fatalf("id %s 的答复没交回来：%+v", id, msgs)
	return got{}
}

// hoverText 把悬停那一段 Markdown 拆开：第一格代码块里是签名，后面是注释。
// 断言比的是这两段的内容，不是围栏的写法。
func hoverText(h *HoverResult) string {
	v := h.Contents.Value
	v = strings.TrimPrefix(v, "```nf\n")
	if i := strings.Index(v, "\n```"); i >= 0 {
		sig := v[:i]
		body := strings.TrimSpace(v[i+len("\n```"):])
		if body == "" {
			return sig
		}
		return sig + "\n--\n" + body
	}
	return strings.TrimSuffix(v, "\n```")
}

// 写在声明上面那段注释要跟着签名一起出现：人给函数写文档注释，为的就是调用点上看得见。
// 注释在语言里没有语义（词法层整个丢掉），所以这一头是从文本上按行列把它读回来的。
func TestHoverCarriesTheDocComment(t *testing.T) {
	const src = "/**\n * Adds two integers together.\n *\n * @param x The first integer.\n * @param y The second integer.\n * @return The sum of x and y.\n */\nmethod add(x: int, y: int): int {\n    return x + y\n}\n\nlet c: int = add(1, 2)\n"
	h := Hover("/p/doc.nf", src, cursorOn(src, "add(1"))
	if h == nil {
		t.Fatal("悬停在 add 上什么都没有")
	}
	got := hoverText(h)
	want := "add(x: int, y: int): int\n--\nAdds two integers together.\n\n@param x The first integer.\n@param y The second integer.\n@return The sum of x and y."
	if got != want {
		t.Errorf("悬停带出的注释不对\n得到：%q\n要  ：%q", got, want)
	}

	// 行注释那一种也认；隔了一个空行的那一段不算这一个声明的。
	const lines = "// 上一句留给别的声明，下面这一段才是这个方法的。\n\nmethod take(n: int): int {\n    return n\n}\n\nlet q: int = take(1)\n"
	h = Hover("/p/doc.nf", lines, cursorOn(lines, "take(1"))
	if h == nil {
		t.Fatal("悬停在 take 上什么都没有")
	}
	if got := hoverText(h); strings.Contains(got, "--") {
		t.Errorf("空行隔开的注释不该跟过来：%q", got)
	}

	const rightAbove = "// 贴着写的行注释。\nmethod keep(n: int): int {\n    return n\n}\n\nlet r: int = keep(1)\n"
	h = Hover("/p/doc.nf", rightAbove, cursorOn(rightAbove, "keep(1"))
	if h == nil {
		t.Fatal("悬停在 keep 上什么都没有")
	}
	if got := hoverText(h); got != "keep(n: int): int\n--\n贴着写的行注释。" {
		t.Errorf("行注释没跟着进来：%q", got)
	}

	// 补全列表右侧那一栏也带同一段注释。
	partial := "method use(): int {\n    ke\n    return 0\n}\n"
	items := Completion("/p/doc.nf", rightAbove+partial, cursorAfter(rightAbove+partial, "ke"))
	if d := documentationOf(items, "keep"); d != "贴着写的行注释。" {
		t.Errorf("补全里 keep 那条没带注释，得到 %q", d)
	}
}

// 双语与示例的两个前缀：`en: ` 那一句是英文，`例：` 那一段单独包成代码围栏，两个前缀都不上屏。
func TestHoverSplitsBilingualDocAndExample(t *testing.T) {
	const src = "// 记一步：过的不打印，挂的才把走过的列出来。\n" +
		"// en: Records a step; a passed case prints nothing, a failed one lists the trail.\n" +
		"// 例：\n" +
		"// run.step(\"点开登录\")\n" +
		"method step(label: string) {\n    return\n}\n\nmethod use() {\n    step(\"甲\")\n}\n"
	h := Hover("/p/doc.nf", src, cursorOn(src, "step(\"甲\""))
	if h == nil {
		t.Fatal("悬停在 step 上什么都没有")
	}
	want := "step(label: string)\n--\n记一步：过的不打印，挂的才把走过的列出来。\n\n" +
		"Records a step; a passed case prints nothing, a failed one lists the trail.\n\n" +
		"```nf\nrun.step(\"点开登录\")\n```"
	if got := hoverText(h); got != want {
		t.Errorf("双语与示例的排版不对\n得到：%q\n要  ：%q", got, want)
	}

	// 块注释那一种同样分得开；补全的说明栏用同一段排版。
	const block = "/**\n * 数出文本里的码点数。\n * en: Counts the code points in a text.\n * 例：\n * length(\"甲乙\")\n */\nmethod count(t: string): int {\n    return 0\n}\n\nlet n: int = count(\"甲\")\n"
	h = Hover("/p/doc.nf", block, cursorOn(block, "count(\"甲\""))
	if h == nil {
		t.Fatal("悬停在 count 上什么都没有")
	}
	if got := hoverText(h); !strings.Contains(got, "Counts the code points") || strings.Contains(got, "en:") {
		t.Errorf("块注释里的英文段没分开或前缀没吃掉：%q", got)
	}
	text := src + "method again() {\n    st\n}\n"
	items := Completion("/p/doc.nf", text, cursorAfter(text, "st"))
	d := documentationOf(items, "step")
	if !strings.Contains(d, "Records a step") || !strings.Contains(d, "```nf\nrun.step(\"点开登录\")\n```") || strings.Contains(d, "en:") {
		t.Errorf("补全的说明栏缺英文段、缺示例围栏或留着前缀：%q", d)
	}
}

// 标准库的组名不是一个声明，凑不出一行签名：悬停那一栏列出它有什么动作，不写"console：标准库组"那种说法。
func TestHoverOnAStdGroupListsItsActions(t *testing.T) {
	const src = "console.read()\n"
	h := Hover("/p/doc.nf", src, cursorOn(src, "console"))
	if h == nil {
		t.Fatal("悬停在 console 上什么都没有")
	}
	got := hoverText(h)
	if !strings.HasPrefix(got, "console\n--\n") {
		t.Fatalf("第一行该只有组名，得到 %q", got)
	}
	if strings.Contains(got, "：标准库组") {
		t.Errorf("答复还是一句说明：%q", got)
	}
	for _, want := range []string{"console.read(): string", "console.print(...values)"} {
		if !strings.Contains(got, want) {
			t.Errorf("那一段里少了 %s：%q", want, got)
		}
	}
}

func labels(items []CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func detailOf(items []CompletionItem, name string) string {
	for _, it := range items {
		if it.Label == name {
			return it.Detail
		}
	}
	return ""
}

func documentationOf(items []CompletionItem, name string) string {
	for _, it := range items {
		if it.Label == name {
			return it.Documentation
		}
	}
	return ""
}

// hasHan 问这一段里有没有汉字：屏幕上那三段靠这个分中文与英文那两段。
func hasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func hasLabel(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

func keysOf(m map[string]check.ImportedName) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sigKeys(facts *check.Facts) []string {
	var out []string
	for k := range facts.Sigs {
		out = append(out, k)
	}
	return out
}

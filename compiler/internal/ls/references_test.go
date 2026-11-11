// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// references_test.go 钉"找引用"这一条：局部量、参数、函数、方法、字段、类型、枚举成员、
// catch 的名字各找得齐，泛型的模板与实例归并成同一个符号，接口的方法带上 base 链那一家，
// 跨文件引进来的名字认得回原声明，标准库的名字安静返回空。
//
// en: references_test.go pins find-references: locals, parameters, functions, methods, fields,
// types, enum members and catch names are all found in full; a generic's template and instances
// merge into one symbol; an interface method carries its base-chain family; an imported name
// traces back to its original declaration; standard-library names answer nothing.
package ls

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/lexer"
)

// atToken 把光标停在第 nth 次（0 起）写出来的那一个名字上：按词法记号找，
// 注释与字符串里同名的那一截不算。
//
// en: atToken rests the cursor on the nth (0-based) written occurrence of a name: found by lexed
// token, so a same-named stretch inside a comment or string does not count.
func atToken(t *testing.T, text, name string, nth int) Position {
	t.Helper()
	toks, err := lexer.Lex(text)
	if err != nil {
		t.Fatalf("样例词法过不去：%v", err)
	}
	seen := 0
	for _, tok := range toks {
		if tok.Kind != lexer.Name || tok.Text != name {
			continue
		}
		if seen < nth {
			seen++
			continue
		}
		line := lineOf(text, tok.Line-1)
		return Position{Line: tok.Line - 1, Character: utf16Len(line[:tok.Col-1])}
	}
	t.Fatalf("样例里没有第 %d 个 %s", nth+1, name)
	return Position{}
}

// sitesOf 把答复收成"第几行第几列"的清单，行从 0 起、列按 UTF-16，与协议同形。
//
// en: sitesOf collects the reply into a list of line/column pairs, line 0-based and column in
// UTF-16, the same shape as the protocol.
func sitesOf(locs []Location) []string {
	var out []string
	for _, l := range locs {
		out = append(out, strings.TrimPrefix(l.URI, "file://")+":"+strconv.Itoa(l.Range.Start.Line)+":"+strconv.Itoa(l.Range.Start.Character))
	}
	return out
}

func TestReferencesLocal(t *testing.T) {
	const text = `method run() {
    let sum:int = 0
    sum = sum + 1
    console.print(sum)
}
`
	locs := References("p.nf", text, atToken(t, text, "sum", 1), true)
	if len(locs) != 4 {
		t.Fatalf("sum 连声明带使用该有 4 处：%v", sitesOf(locs))
	}
	if locs[0].Range.Start.Line != 1 || locs[0].Range.Start.Character != 8 {
		t.Fatalf("第一处该是声明那一行第 9 列：%v", locs[0])
	}
	locs = References("p.nf", text, atToken(t, text, "sum", 0), false)
	if len(locs) != 3 {
		t.Fatalf("不算声明该有 3 处使用：%v", sitesOf(locs))
	}
	// 光标停在声明那一行自己的名字上也认得。
	if locs := References("p.nf", text, atToken(t, text, "sum", 0), true); len(locs) != 4 {
		t.Fatalf("停在声明上也该找全 4 处：%v", sitesOf(locs))
	}
}

func TestReferencesParamAndCatch(t *testing.T) {
	const text = `method twice(n:int):int {
    return n * 2
}

method guard() {
    try {
        throw (7, "甲")
    } catch (code, msg, at) {
        console.print(code, msg)
    }
    try {
        throw (8, "乙")
    } catch (code, msg, at) {
        console.print(code)
    }
}
`
	if locs := References("p.nf", text, atToken(t, text, "n", 0), true); len(locs) != 2 {
		t.Fatalf("参数 n 连声明该有 2 处：%v", sitesOf(locs))
	}
	// 两条 try 的 catch 名字相同也不是同一个符号：第一条的 code 只有声明加那一处打印。
	locs := References("p.nf", text, atToken(t, text, "code", 0), true)
	if len(locs) != 2 || locs[0].Range.Start.Line != 7 {
		t.Fatalf("第一条 catch 的 code 该有 2 处且声明在第 8 行：%v", sitesOf(locs))
	}
	locs = References("p.nf", text, atToken(t, text, "code", 2), true)
	if len(locs) != 2 || locs[0].Range.Start.Line != 12 {
		t.Fatalf("第二条 catch 的 code 该有 2 处且声明在第 13 行：%v", sitesOf(locs))
	}
}

func TestReferencesFunction(t *testing.T) {
	const text = `method twice(n:int):int {
    return n * 2
}

let f:fn(int):int = twice
console.print(twice(3), f(4))
`
	locs := References("p.nf", text, atToken(t, text, "twice", 1), true)
	if len(locs) != 3 {
		t.Fatalf("twice 该有声明、当值交出、调用三处：%v", sitesOf(locs))
	}
}

func TestReferencesMethodFieldAndType(t *testing.T) {
	const text = `class Score {
    points:int

    method bump():int {
        points = points + 1
        return points
    }
}

let s:Score = new Score(points: 3)
console.print(s.points, s.bump())
let box:object = s
let back:Score = box as Score
if (back is Score) {
    console.print(back.points)
}
`
	if locs := References("p.nf", text, atToken(t, text, "bump", 0), true); len(locs) != 2 {
		t.Fatalf("bump 该有声明与调用两处：%v", sitesOf(locs))
	}
	// points：声明、方法体里三次直接写、new 的初值、两处点出来，共 7 处。
	if locs := References("p.nf", text, atToken(t, text, "points", 1), true); len(locs) != 7 {
		t.Fatalf("points 该有 7 处：%v", sitesOf(locs))
	}
	// Score：声明、两处标注、new、as、is，共 6 处。
	if locs := References("p.nf", text, atToken(t, text, "Score", 1), true); len(locs) != 6 {
		t.Fatalf("Score 该有 6 处：%v", sitesOf(locs))
	}
}

func TestReferencesStructLiteralField(t *testing.T) {
	const text = `struct Point {
    x:int
    y:int
}

let p:Point = Point { x: 1, y: 2 }
console.print(p.x)
`
	if locs := References("p.nf", text, atToken(t, text, "x", 0), true); len(locs) != 3 {
		t.Fatalf("x 该有声明、字面量、点出来三处：%v", sitesOf(locs))
	}
}

func TestReferencesEnumMember(t *testing.T) {
	const text = `enum Kind {
    add = 0,
    sub
}

let k:Kind = Kind.add
console.print(Kind.sub)
`
	if locs := References("p.nf", text, atToken(t, text, "add", 0), true); len(locs) != 2 {
		t.Fatalf("add 该有声明与点出来两处：%v", sitesOf(locs))
	}
	// 枚举自己：声明、标注、两处点出来的左边，共 4 处。
	if locs := References("p.nf", text, atToken(t, text, "Kind", 1), true); len(locs) != 4 {
		t.Fatalf("Kind 该有 4 处：%v", sitesOf(locs))
	}
}

func TestReferencesGenericMergesInstances(t *testing.T) {
	const text = `method twice<Element>(v:Element):Element {
    return v
}

class Cell<T> {
    v:T

    method get():T {
        let out:T = v
        return out
    }
}

let a:int = twice(1)
let b:string = twice("甲")
let c:Cell<int> = new Cell<int>(v: 3)
let d:Cell<string> = new Cell<string>(v: "乙")
console.print(c.get(), d.get())
`
	if locs := References("p.nf", text, atToken(t, text, "twice", 0), true); len(locs) != 3 {
		t.Fatalf("twice 的模板与两种实例该归并成 3 处：%v", sitesOf(locs))
	}
	if locs := References("p.nf", text, atToken(t, text, "get", 0), true); len(locs) != 3 {
		t.Fatalf("get 的模板与两种实例该归并成 3 处：%v", sitesOf(locs))
	}
	// 模板体里的局部量：两份实例的节点是复制出来的，使用处按位置认回同一处声明。
	if locs := References("p.nf", text, atToken(t, text, "out", 0), true); len(locs) != 2 {
		t.Fatalf("泛型体里的 out 该有声明与使用两处：%v", sitesOf(locs))
	}
	// 类型自己：声明、两处标注、两处 new，共 5 处。
	if locs := References("p.nf", text, atToken(t, text, "Cell", 0), true); len(locs) != 5 {
		t.Fatalf("Cell 该有 5 处：%v", sitesOf(locs))
	}
}

func TestReferencesInterfaceFamily(t *testing.T) {
	const text = `interface Shape {
    perim():float
}

class Circle base Shape {
    r:float

    method perim():float {
        return 6.28 * r
    }
}

class Square base Shape {
    side:float

    method perim():float {
        return side * 4.0
    }
}

let c:Circle = new Circle(r: 1.0)
let s:Square = new Square(side: 2.0)
console.print(c.perim(), s.perim())
`
	// 接口里那一个 perim：接口声明、两个类里的实现、两处调用，共 5 处。
	if locs := References("p.nf", text, atToken(t, text, "perim", 0), true); len(locs) != 5 {
		t.Fatalf("接口的 perim 该带上实现那一家共 5 处：%v", sitesOf(locs))
	}
	// 从其中一个实现问，答复相同。
	if locs := References("p.nf", text, atToken(t, text, "perim", 1), true); len(locs) != 5 {
		t.Fatalf("从实现问 perim 也该有 5 处：%v", sitesOf(locs))
	}
}

func TestReferencesImportedCrossFile(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "a.nf", "export method twice(n:int):int {\n    return n * 2\n}\n\nexport const limit:int = 9\n")
	main := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	text := "import { twice, limit } from \"./a.nf\"\n\nconsole.print(twice(limit))\n"
	write(t, main, text)
	locs := References(main, text, atToken(t, text, "twice", 1), true)
	if len(locs) != 3 {
		t.Fatalf("twice 该有原声明、import、调用三处：%v", sitesOf(locs))
	}
	if !strings.HasSuffix(locs[0].URI, "/a.nf") {
		t.Fatalf("第一处该落在 a.nf：%v", locs[0])
	}
	locs = References(main, text, atToken(t, text, "limit", 1), true)
	if len(locs) != 3 {
		t.Fatalf("limit 该有原声明、import、使用三处：%v", sitesOf(locs))
	}
}

func TestReferencesPackageMember(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, filepath.Join("libs", "greet", "nf.json"), `{"name":"greet","version":"1.0.0"}`)
	writeIn(t, dir, filepath.Join("libs", "greet", "greet.nf"), "export method hello():int {\n    return 1\n}\n\nexport const limit:int = 9\n")
	main := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	text := "import gt from \"libs/greet\"\n\nconsole.print(gt.hello(), gt.limit)\n"
	write(t, main, text)
	if locs := References(main, text, atToken(t, text, "hello", 0), true); len(locs) != 2 {
		t.Fatalf("包导出的 hello 该有声明与点出来两处：%v", sitesOf(locs))
	}
	if locs := References(main, text, atToken(t, text, "limit", 0), true); len(locs) != 2 {
		t.Fatalf("包导出的 limit 该有声明与点出来两处：%v", sitesOf(locs))
	}
}

func TestReferencesStdlibIsSilent(t *testing.T) {
	const text = "console.print(1)\n"
	if locs := References("p.nf", text, atToken(t, text, "print", 0), true); len(locs) != 0 {
		t.Fatalf("标准库的动作没有写在源码里的声明，不该找出几处：%v", sitesOf(locs))
	}
}

func TestReferencesUTF16Column(t *testing.T) {
	// 名字前面摆一段中文：字节列与 UTF-16 列差着几个，答复要按协议给 UTF-16。
	const src = "let n:int = 7\nconsole.print(\"一段中文\", n)\n"
	locs := References("p.nf", src, atToken(t, src, "n", 0), true)
	if len(locs) != 2 {
		t.Fatalf("n 该有 2 处：%v", sitesOf(locs))
	}
	want := utf16Len("console.print(\"一段中文\", ")
	if locs[1].Range.Start.Character != want || locs[1].Range.End.Character != want+1 {
		t.Fatalf("中文后面那一处该从 UTF-16 第 %d 列起：%+v", want, locs[1])
	}
}

func TestServeAnswersReferencesRenameFormatting(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	const text = "let total:int = 4\\nconsole.print(total)\\n"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+`","languageId":"nf","version":1,"text":"`+text+`"}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/references","params":{"textDocument":{"uri":"`+uri+`"},"position":{"line":1,"character":15},"context":{"includeDeclaration":true}}}`),
		frame(`{"jsonrpc":"2.0","id":5,"method":"textDocument/prepareRename","params":{"textDocument":{"uri":"`+uri+`"},"position":{"line":0,"character":5}}}`),
		frame(`{"jsonrpc":"2.0","id":6,"method":"textDocument/rename","params":{"textDocument":{"uri":"`+uri+`"},"position":{"line":0,"character":5},"newName":"sum"}}`),
		frame(`{"jsonrpc":"2.0","id":7,"method":"textDocument/formatting","params":{"textDocument":{"uri":"`+uri+`"},"options":{"tabSize":4,"insertSpaces":true}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var res initResult
	if err := json.Unmarshal(replyOf(t, msgs, "1").Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	if !res.Capabilities.ReferencesProvider || res.Capabilities.RenameProvider == nil || !res.Capabilities.DocumentFormattingProvider {
		t.Fatalf("initialize 没把找引用、重命名、格式化三样本事报全：%+v", res.Capabilities)
	}
	var locs []Location
	if err := json.Unmarshal(replyOf(t, msgs, "4").Result, &locs); err != nil {
		t.Fatalf("找引用的答复形状不对：%v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("total 连声明该有 2 处：%v", locs)
	}
	var rng Range
	if err := json.Unmarshal(replyOf(t, msgs, "5").Result, &rng); err != nil {
		t.Fatalf("prepareRename 的答复形状不对：%v", err)
	}
	if rng.Start.Character != 4 || rng.End.Character != 9 {
		t.Fatalf("total 的范围该是第 5 到第 9 列：%+v", rng)
	}
	var edit WorkspaceEdit
	if err := json.Unmarshal(replyOf(t, msgs, "6").Result, &edit); err != nil {
		t.Fatalf("重命名的答复形状不对：%v", err)
	}
	if len(edit.Changes[uri]) != 2 {
		t.Fatalf("重命名该交出两条改动：%+v", edit)
	}
	var edits []TextEdit
	if err := json.Unmarshal(replyOf(t, msgs, "7").Result, &edits); err != nil {
		t.Fatalf("格式化的答复形状不对：%v", err)
	}
}

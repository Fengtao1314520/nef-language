// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// formatting_test.go 钉"格式化文档"这一条：缩进按花括号深度重排，记号间的空白按规则收齐，
// 字符串与注释的原文不动，空行并成一行，文件最后只留一个换行。拿不准的（尖括号前后、
// 冒号后有没有空格）照旧。词法过不去与排完没变化都返回空。
//
// en: formatting_test.go pins format-document: indentation follows brace depth, whitespace
// between tokens is tidied by the rules, strings and comments keep their original text, blank
// runs collapse to one, and the file keeps a single final newline. What it cannot judge (around
// angle brackets, the space after a colon) stays. A text that fails lexing or needs no change
// answers nothing.
package ls

import "testing"

func formatOnce(t *testing.T, text string) string {
	t.Helper()
	edits := Formatting("p.nf", text, FormattingOptions{TabSize: 4, InsertSpaces: true})
	if len(edits) == 0 {
		return text
	}
	if len(edits) != 1 {
		t.Fatalf("格式化该交一条整份改动：%d 条", len(edits))
	}
	return edits[0].NewText
}

func TestFormattingIndentAndSpacing(t *testing.T) {
	const text = "method run(n:int):int {\nlet sum:int=0\nif(n>0) {\nsum = sum+n\n}\nreturn sum\n}\n"
	const want = "method run(n:int):int {\n    let sum:int = 0\n    if (n>0) {\n        sum = sum + n\n    }\n    return sum\n}\n"
	if got := formatOnce(t, text); got != want {
		t.Fatalf("排完不对：\n got %q\nwant %q", got, want)
	}
}

func TestFormattingKeepsStringsAndComments(t *testing.T) {
	const text = "//  头上这一段照旧\nmethod run() {\nlet s:string = \"里面  的空格不动\\n\"\nconsole.print(s) //  行尾这一截也照旧\n}\n"
	const want = "//  头上这一段照旧\nmethod run() {\n    let s:string = \"里面  的空格不动\\n\"\n    console.print(s) //  行尾这一截也照旧\n}\n"
	if got := formatOnce(t, text); got != want {
		t.Fatalf("字符串与注释该一字节不动：\n got %q\nwant %q", got, want)
	}
}

func TestFormattingBlankLinesAndTail(t *testing.T) {
	const text = "let a:int = 1\n\n\n\nlet b:int = 2   \nconsole.print(a, b)\n\n\n"
	const want = "let a:int = 1\n\nlet b:int = 2\nconsole.print(a, b)\n"
	if got := formatOnce(t, text); got != want {
		t.Fatalf("空行与收尾不对：\n got %q\nwant %q", got, want)
	}
}

func TestFormattingUnaryAndCalls(t *testing.T) {
	const text = "method f(n:int):int {\nreturn -n\n}\nlet a:int = f(-3)\nlet b:boolean = !true\nlet c:int = 1 + -2\nconsole.print(a,b,c)\n"
	const want = "method f(n:int):int {\n    return -n\n}\nlet a:int = f(-3)\nlet b:boolean = !true\nlet c:int = 1 + -2\nconsole.print(a, b, c)\n"
	if got := formatOnce(t, text); got != want {
		t.Fatalf("一元与调用不对：\n got %q\nwant %q", got, want)
	}
}

func TestFormattingKeepsAnglesAndColons(t *testing.T) {
	const text = "let xs:list<int> = [1,2]\nlet p:Point = Point { x: 1,y: 2 }\nlet q:Point = Point {x:1, y:2}\nlet ok:boolean = 1<2\n"
	const want = "let xs:list<int> = [1, 2]\nlet p:Point = Point { x: 1, y: 2 }\nlet q:Point = Point { x:1, y:2 }\nlet ok:boolean = 1<2\n"
	if got := formatOnce(t, text); got != want {
		t.Fatalf("尖括号与冒号该照旧：\n got %q\nwant %q", got, want)
	}
}

func TestFormattingNoChangeAndBroken(t *testing.T) {
	const text = "let n:int = 7\nconsole.print(n)\n"
	if edits := Formatting("p.nf", text, FormattingOptions{TabSize: 4, InsertSpaces: true}); edits != nil {
		t.Fatalf("排完没变化该返回空：%v", edits)
	}
	if edits := Formatting("p.nf", "let s:string = \"没写完\n", FormattingOptions{}); edits != nil {
		t.Fatalf("词法过不去该返回空：%v", edits)
	}
}

func TestFormattingCRLFAndTabs(t *testing.T) {
	const text = "method run() {\r\nlet n:int=0\r\n}\r\n"
	const want = "method run() {\r\n    let n:int = 0\r\n}\r\n"
	if got := formatOnce(t, text); got != want {
		t.Fatalf("CRLF 该照旧：\n got %q\nwant %q", got, want)
	}
	edits := Formatting("p.nf", "method run() {\nlet n:int=0\n}\n", FormattingOptions{TabSize: 4, InsertSpaces: false})
	if len(edits) != 1 || edits[0].NewText != "method run() {\n\tlet n:int = 0\n}\n" {
		t.Fatalf("制表符那一路不对：%v", edits)
	}
}

func TestFormattingIdempotent(t *testing.T) {
	const text = "method run(n:int):int {\nlet sum:int=0\nfor(i:int in 0 until n) {\nsum = sum+i\n}\nreturn sum\n}\n"
	once := formatOnce(t, text)
	if edits := Formatting("p.nf", once, FormattingOptions{TabSize: 4, InsertSpaces: true}); edits != nil {
		t.Fatalf("排过一遍再排该没有改动：%v", edits)
	}
}

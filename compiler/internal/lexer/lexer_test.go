// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lexer

import (
	"strings"
	"testing"
)

func kinds(toks []Tok) string {
	var b strings.Builder
	for _, tk := range toks {
		if tk.Kind == EOF {
			b.WriteString("eof")
			continue
		}
		b.WriteString(tk.Kind.String())
		b.WriteByte(':')
		b.WriteString(tk.Text)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

func TestLexBasics(t *testing.T) {
	got := kinds(mustLex(t, "let n:int = 3 + 4\n"))
	want := "name:let name:n op:: name:int op:= num:3 op:+ num:4 nl:\n eof"
	if got != want {
		t.Fatalf("记号流不对\n得到 %s\n期望 %s", got, want)
	}
}

func TestLiterals(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"1_000", "num:1_000 eof"},
		{"0xFF", "num:0xFF eof"},
		{"2.5", "num:2.5 eof"},
		{`"a\tb"`, "str:a\tb eof"},
		{`'中'`, "char:中 eof"},
		{`"\u4e2d"`, "str:中 eof"},
		{"true false null", "name:true name:false name:null eof"},
	} {
		toks := mustLex(t, tc.src)
		if g := kinds(toks); g != tc.want {
			t.Errorf("%s → 得到 %s，期望 %s", tc.src, g, tc.want)
		}
	}
}

func TestNumberFollowedByBareName(t *testing.T) {
	// `1e9` 不认：数字后面直接跟名字，当场报错
	if _, err := Lex("let x:int = 1e9"); err == nil || !strings.Contains(err.Error(), "数字后面直接跟了名字") {
		t.Fatalf("1e9 该报错，实际 %v", err)
	}
}

// 这一版没有八进制：0 开头后面还跟着多位数字的写法当场报错，
// 不许把它按十进制悄悄收下。
func TestLeadingZeroNumberRejected(t *testing.T) {
	for _, src := range []string{"let n:int = 0644", "let n:int = 00", "let n:int = 0_644"} {
		if _, err := Lex(src); err == nil || !strings.Contains(err.Error(), "0 开头的写法不认") {
			t.Fatalf("%s 该报错，实际 %v", src, err)
		}
	}
	for _, src := range []string{"let n:int = 0", "let x:float = 0.5", "let n:int = 1_000", "let n:int = 0xFF"} {
		if _, err := Lex(src); err != nil {
			t.Fatalf("%s 不该报错，实际 %v", src, err)
		}
	}
}

func TestFloatFlag(t *testing.T) {
	toks := mustLex(t, "3 3.5")
	if toks[0].IsFloat || !toks[1].IsFloat {
		t.Fatalf("3 不该是小数、3.5 该是： %+v %+v", toks[0], toks[1])
	}
}

func TestDotIsMemberAccess(t *testing.T) {
	toks := mustLex(t, "a.b")
	if kinds(toks) != "name:a op:. name:b eof" {
		t.Fatalf("点号该单独成记号：%s", kinds(toks))
	}
}

func TestTrailingDotNotNumber(t *testing.T) {
	// `1.` 这种写法不在语法里出现，但别把后面的成员名吞进数字
	toks := mustLex(t, "1.")
	if kinds(toks) != "num:1 op:. eof" {
		t.Fatalf("得到 %s", kinds(toks))
	}
}

func TestComments(t *testing.T) {
	toks := mustLex(t, "// 一行\n1 /* 一块\n还在一块 */ 2")
	if kinds(toks) != "nl:\n num:1 num:2 eof" {
		t.Fatalf("注释没吃掉：%s", kinds(toks))
	}
	if _, err := Lex("/* 没完成"); err == nil || !strings.Contains(err.Error(), "块注释没有完成") {
		t.Fatalf("未完成的块注释该报错，实际 %v", err)
	}
}

func TestBadEscape(t *testing.T) {
	if _, err := Lex(`"a\zb"`); err == nil || !strings.Contains(err.Error(), "不认识的转义") {
		t.Fatalf("非法转义该报错，实际 %v", err)
	}
}

func TestNonASCIIIdent(t *testing.T) {
	if _, err := Lex("变量 = 1"); err == nil || !strings.Contains(err.Error(), "非 ASCII") {
		t.Fatalf("非 ASCII 标识符该报错，实际 %v", err)
	}
	if _, err := Lex("ab变量"); err == nil || !strings.Contains(err.Error(), "非 ASCII") {
		t.Fatalf("标识符后头夹汉字也该报错，实际 %v", err)
	}
}

func TestUnterminatedString(t *testing.T) {
	if _, err := Lex("let s:string = \"没收尾\n"); err == nil {
		t.Fatal("未完成的字符串该报错")
	}
}

func TestKeywordsAndReservedAreListed(t *testing.T) {
	for _, w := range []string{"let", "const", "method", "struct", "class", "interface", "enum", "type", "import", "export",
		"shared", "ref", "base", "this", "new", "return", "as", "is", "throw", "try", "catch", "if", "else", "while", "for",
		"in", "until", "switch", "default", "break", "continue", "true", "false", "null"} {
		if !Keywords[w] {
			t.Errorf("关键字表漏了 %s", w)
		}
	}
	if len(Keywords) != 34 {
		t.Errorf("关键字一共该是 34 个，实际 %d", len(Keywords))
	}
	for _, w := range []string{"boolean", "int", "float", "byte", "unicode", "string", "object",
		"list", "dict", "hashset", "Calendar", "length", "exit", "console", "text", "number", "math", "time",
		"file", "path", "environment", "regular_expression", "json", "Json", "bytes", "Socket", "Listener",
		"Response", "net", "http", "args", "Process", "process"} {
		if !Reserved[w] {
			t.Errorf("保留名单漏了 %s", w)
		}
	}
	if len(Reserved) != 33 {
		t.Errorf("内建名字一共该是 33 个，实际 %d", len(Reserved))
	}
}

func TestCharMustBeOneCodepoint(t *testing.T) {
	if _, err := Lex("let u:unicode = 'ab'"); err == nil {
		t.Fatal("单引号里两个字符该报错")
	}
	// 代理区的码点在合法 UTF-8 源码里写不出来，只能由 `int as unicode` 产生，那边由静态检查拒绝
	if _, err := Lex("let u:unicode = ''"); err == nil {
		t.Fatal("单引号里空的也该报错")
	}
}

func mustLex(t *testing.T, src string) []Tok {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("词法不该报错：%v", err)
	}
	return toks
}

// 识别一个标点要拿"源码剩下那一段"逐个运算符比一轮。比之前若先把那一段拷成字符串，
// 代价就成了记号数 × 文件长度：一份 3000 行、12 万字符的程序，光词法 2.9 秒，
// 换成不拷贝的比法是 7.7 毫秒。分配次数每次跑都一样，
// 所以拿它当判据，不拿毫秒数当判据。
func TestLexAllocatesPerTokenNotPerFileLength(t *testing.T) {
	src := strings.Repeat("method m17():int { return 7 }\n", 200)
	toks := mustLex(t, src)
	allocs := testing.AllocsPerRun(10, func() { _, _ = Lex(src) })
	per := allocs / float64(len(toks))
	t.Logf("%d 个记号，%d 次分配，平均每个记号 %.2f 次", len(toks), int(allocs), per)
	if per > 6 {
		t.Errorf("每个记号摊到 %.2f 次分配：比运算符时把源码尾巴拷出来了", per)
	}
}

func BenchmarkLexLarge(b *testing.B) {
	src := strings.Repeat("method m17():int { return 7 }\n", 3000)
	for i := 0; i < b.N; i++ {
		if _, err := Lex(src); err != nil {
			b.Fatal(err)
		}
	}
}

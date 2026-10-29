// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/rtre"
)

// nfLit 把一个原始字符串转成一段 Nef 文本字面量。
//
// 文本字面量认识的转义只有五个（\n \t \" \\ \uXXXX），所以式子里的反斜杠在源文件里
// 要写两遍——`"\\d+"` 表示式子 `\d+`。这个 helper 就是替测试把这件事做对。
func nfLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestRegexRun 是四条成员各运行一遍真实产物。期望串来自实际运行结果。
//
// 挑的用例覆盖三条规则：`.` 匹配一个码点（汉字算一个）、`|` 先写的分支优先、
// 能匹配空串的式子不逐码点拆开占位。
func TestRegexRun(t *testing.T) {
	got, code := run(t, `let s:string = "let total:int = 42"
console.print(regular_expression.match(s, "\\d+"), "|", regular_expression.match(s, "^[a-z]+"), "|", regular_expression.match(s, "\\w+:\\w+"))
console.print(regular_expression.replace(s, "\\s+", "_"))
for (x:string in regular_expression.find(s, "[a-z]+")) {
    console.print(x)
}
for (x:string in regular_expression.split(s, ":")) {
    console.print(x)
}
console.print(regular_expression.match("甲乙", "^.乙$"), "|", regular_expression.match("甲乙丙", "^.乙$"), "|", regular_expression.replace("aaa", "a?", "-"))
`)
	want := "true | true | true\n" +
		"let_total:int_=_42\n" +
		"let\ntotal\nint\n" +
		"let total\nint = 42\n" +
		"true | false | ---\n"
	if got != want || code != 0 {
		t.Fatalf("regular_expression 那四条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestRegexMatchesReference 用 internal/rtre 的参考实现作对照表：同一批式子、同一批文本，
// 产物跑出的 match／find／replace／split 必须与它逐字一致。
//
// 这一组是这个匹配器的主要测试——手写期望串只能固定几个点，逐项比对能覆盖
// 优先级、贪婪、空匹配、字符组、锚、码点这些全部行为。
func TestRegexMatchesReference(t *testing.T) {
	pats := []string{
		`\d+`, `\w+`, `\s`, `\S+`, `[a-z]+`, `[^a-z]`, `a|b`, `ab|a`, `(a|b)+`, `(甲|乙)`,
		`a*b`, `ab?`, `a.c`, `.*`, `^let`, `total$`, `[0-9a-zA-Z_]+`, `[-a]`, `[]]`, `[a-]`,
		`\d\d\d\d-\d\d-\d\d`, `(\w)(\w)`, `\w+@[a-z]+`, `x*`, `let\s+\w+`,
	}
	texts := []string{
		`let total:int = 42`, `甲,乙,丙`, `a1b2c3`, `2026-10-01`, `中文 混着 english 一起`,
		`one two three`, `aaaab`, `...---...`, `X`, ``, `a@b`, `abab`,
	}
	// 一次跑一批：单个产物进程处理整行断言，省去几十次 build 的开销
	var src strings.Builder
	for _, pat := range pats {
		for _, text := range texts {
			re, err := rtre.Compile(pat)
			if err != nil {
				t.Fatalf("%q 该是能编的：%v", pat, err)
			}
			src.WriteString("console.print(regular_expression.match(" + nfLit(text) + ", " + nfLit(pat) + "))\n")
			for _, frag := range re.FindAll(text) {
				src.WriteString("console.print(" + nfLit(frag) + ")\n")
			}
			src.WriteString("console.print(regular_expression.replace(" + nfLit(text) + ", " + nfLit(pat) + ", \"#\"))\n")
			for _, seg := range re.Split(text) {
				src.WriteString("console.print(" + nfLit(seg) + ")\n")
			}
		}
	}
	got, code := run(t, src.String())
	if code != 0 {
		t.Fatalf("跑出来的码是 %d，输出：\n%s", code, got)
	}
	// 期望串按同一顺序现拼一遍：每段对应一条 print，产物逐行输出
	var exp strings.Builder
	for _, pat := range pats {
		re, err := rtre.Compile(pat)
		if err != nil {
			t.Fatalf("%q：%v", pat, err)
		}
		for _, text := range texts {
			exp.WriteString(boolLine(re.Match(text)))
			for _, frag := range re.FindAll(text) {
				exp.WriteString(frag + "\n")
			}
			exp.WriteString(re.ReplaceAll(text, "#") + "\n")
			for _, seg := range re.Split(text) {
				exp.WriteString(seg + "\n")
			}
		}
	}
	if got != exp.String() {
		t.Fatalf("与那份参考实现对不上：\n got %q\nwant %q\n（前 %d 行一样）", got, exp.String(), commonPrefixLines(got, exp.String()))
	}
}

func boolLine(b bool) string {
	if b {
		return "true\n"
	}
	return "false\n"
}

// commonPrefixLines 返回两侧从头起相同的行数：逐项比对失败时先看这个数，比读两段长字符串容易。
func commonPrefixLines(a, b string) int {
	al, bl := strings.SplitAfter(a, "\n"), strings.SplitAfter(b, "\n")
	n := 0
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			break
		}
		n++
	}
	return n
}

// TestRegexNamesBadPatterns 固定 10071 号：不支持的写法要当场点名，不许悄悄当成字面字符吞掉。
func TestRegexNamesBadPatterns(t *testing.T) {
	for _, one := range []struct{ pat, msg string }{
		{`a{2,3}`, "{m,n}"},
		{`a*?`, "量词"},
		{`(?i)ab`, "(?...)"},
		{`()`, "(?...)"},
		{`\1`, "反向引用"},
		{`\b`, "词边界"},
		{`(a`, "括号没配上"},
		{`a)`, "括号没配上"},
		{`[a`, "收尾的 ]"},
		{`[z-a]`, "范围反了"},
		{`[\D]`, "\\D \\W \\S"},
		{`\`, "反斜杠收尾"},
		{`*a`, "量词前面"},
		{``, "是空的"},
	} {
		got, code := run(t, "console.print(regular_expression.match(\"甲1\", "+nfLit(one.pat)+"))\n")
		if code != 1 || !strings.Contains(got, "出错了（10071）") {
			t.Errorf("%q 该报 10071 号：got %q 码 %d", one.pat, got, code)
			continue
		}
		if !strings.Contains(got, one.msg) {
			t.Errorf("%q 该点名到 %q：got %q", one.pat, one.msg, got)
		}
	}
}

// TestRegexEmptyPatternIsRuntime 验证：正则式子是运行期的值，写错的式子无法在编译期拒绝。
// 接住它的是 catch，拿到的错误码是 10071。
func TestRegexCatchSees71(t *testing.T) {
	got, code := run(t, `let pat:string = "a{2}"
try {
    console.print(regular_expression.match("aa", pat))
} catch (code, msg) {
    console.print(code, "|", length(msg) > 0)
}
`)
	want := "10071 | true\n"
	if got != want || code != 0 {
		t.Fatalf("接住的该是 10071 号：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestRegexAnchorsAndCodepoints 验证"一切按码点走"的规则：`.` 与 `[^x]` 匹配的都是一个码点，
// 汉字不会被拆成三个字节各算一次；`\w` 只匹配 ASCII，汉字不在其内。
func TestRegexAnchorsAndCodepoints(t *testing.T) {
	got, code := run(t, `console.print(regular_expression.replace("甲乙", "[^甲]", "#"))
console.print(regular_expression.match("甲", "."), "|", regular_expression.match("甲乙", "^..$"), "|", regular_expression.match("甲", "^...$"))
console.print(regular_expression.match("中文", "\\w+"), "|", length(regular_expression.find("中文 a", "\\w+")))
`)
	want := "甲#\ntrue | true | false\nfalse | 1\n"
	if got != want || code != 0 {
		t.Fatalf("码点那一组不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

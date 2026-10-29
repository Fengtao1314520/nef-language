// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package rtre

import (
	"regexp"
	"strings"
	"testing"
)

// TestSubsetMatchesGoRegexp 拿 Go 的标准库当对照表：最小可用子集里的写法，两边结果必须一致
// （Go 的 regexp 也是"最左、按书写顺序优先"，与这台 VM 的优先级同一条）。
//
// 比的是 Match 与取出的片段两件事。Go 的 `.` 默认不匹配换行，本实现匹配，所以含 `.` 的文本
// 里不放换行——这是设计上的差异，不是实现错误。
func TestSubsetMatchesGoRegexp(t *testing.T) {
	texts := []string{
		`let total:int = 0`, `  甲,乙,丙  `, `a1b2c3`, `没有数字的一行`, `X`, ``,
		`one two three four`, `2026-10-01`, `中文 混着 english 一起`, `...---...`, `aaaab`,
	}
	pats := []string{
		`\w+`, `\d+`, `\s`, `[abc]+`, `[^abc]`, `a|b`, `(a|b)+`, `ab?`, `a*b`, `a+b`,
		`^a`, `a$`, `let\s+\w+`, `\d\d\d\d-\d\d-\d\d`, `.*`, `x`, `\w\W\s\S\d\D`,
		`[0-9a-zA-Z_]+`, `(甲|乙)`, `[-a]`, `[]]`, `[a-]`, `\s+`, `(\w)(\w)`, `a.c`,
	}
	for _, pat := range pats {
		mine, err := Compile(pat)
		if err != nil {
			t.Fatalf("%q 该是能编的：%v", pat, err)
		}
		theirs, terr := regexp.Compile(pat)
		if terr != nil {
			t.Fatalf("Go 那边居然编不过 %q：%v", pat, terr)
		}
		for _, text := range texts {
			if got, want := mine.Match(text), theirs.MatchString(text); got != want {
				t.Errorf("%q 在 %q 上：我 %v，Go %v", pat, text, got, want)
			}
			g, w := mine.FindAll(text), theirs.FindAllString(text, -1)
			if strings.Join(g, "|") != strings.Join(w, "|") {
				t.Errorf("%q 在 %q 上取的片段不一样：我 %q，Go %q", pat, text, g, w)
			}
		}
	}
}

// TestReplaceAndSplit 同样与 Go 逐值比对：替换用字面替身（本实现不支持 $1），所选式子都保证
// 只能匹配非空——空匹配规则由本语言另行规定，单独在下一组测试覆盖。
func TestReplaceAndSplit(t *testing.T) {
	cases := []struct{ pat, rep, text string }{
		{`total`, `count`, `let total:int = total`},
		{`\s+`, `_`, `one  two\tthree`},
		{`,`, `、`, `甲,乙,,丙,`},
		{`\d`, `#`, `a1b22c333`},
		{`[,;]`, `|`, `甲,乙;丙`},
	}
	for _, one := range cases {
		mine, err := Compile(one.pat)
		if err != nil {
			t.Fatalf("%q：%v", one.pat, err)
		}
		theirs := regexp.MustCompile(one.pat)
		if got, want := mine.ReplaceAll(one.text, one.rep), theirs.ReplaceAllLiteralString(one.text, one.rep); got != want {
			t.Errorf("%q 替身 %q 在 %q 上：我 %q，Go %q", one.pat, one.rep, one.text, got, want)
		}
		gs, ws := mine.Split(one.text), theirs.Split(one.text, -1)
		if strings.Join(gs, "/") != strings.Join(ws, "/") {
			t.Errorf("%q 在 %q 上切得不一样：我 %q，Go %q", one.pat, one.text, gs, ws)
		}
	}
}

// TestEmptyMatchIsSkipped 覆盖本实现与 Go 有意不同的一处（"空匹配不切、不重复占位"那条规则）：
// 能匹配空的式子不逐码点拆分，而是跳过空匹配、往前挪一个位置。
func TestEmptyMatchIsSkipped(t *testing.T) {
	star, err := Compile(`x*`)
	if err != nil {
		t.Fatal(err)
	}
	if got := star.FindAll("abxc"); strings.Join(got, "|") != "x" {
		t.Errorf(`x* 在 "abxc" 上只该取到那个 x，got %q`, got)
	}
	if got := star.Split("abxc"); strings.Join(got, "|") != "ab|c" {
		t.Errorf(`x* 切 "abxc"（ab／x／c 三段）该得 ab｜c，got %q`, got)
	}
	if got := star.ReplaceAll("abxc", "-"); got != "ab-c" {
		t.Errorf(`x* 替身该只动那个 x，got %q`, got)
	}
	// 只能匹配空的式子：find 返回空 list、replace 不改动任何字符、split 返回整串
	never, err := Compile(`x?y?`)
	if err != nil {
		t.Fatal(err)
	}
	if got := never.FindAll("甲乙"); len(got) != 0 {
		t.Errorf("全是空匹配时 find 该交回空list，got %q", got)
	}
	if got := never.ReplaceAll("甲乙", "#"); got != "甲乙" {
		t.Errorf("全是空匹配时不该改动一个字，got %q", got)
	}
	if got := never.Split("甲乙"); strings.Join(got, "|") != "甲乙" {
		t.Errorf("全是空匹配时切一段就该是原串，got %q", got)
	}
	// Match 的判定仍然成立：空匹配也是"匹配得上"
	if !never.Match("甲乙") {
		t.Error(`x?y? 该能匹配上（空匹配也算）`)
	}
}

// TestDotAndCodepoints 固定"一切按码点走"：`.` 吃一个码点（三个字节也算一个），也吃换行；
// `\w` 只覆盖 ASCII，中文不属于它，但汉字写进字符组时按一个码点计。
func TestDotAndCodepoints(t *testing.T) {
	dot, err := Compile(`^.$`)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"甲", "a", "\n"} {
		if !dot.Match(s) {
			t.Errorf("`.` 该吃下 %q 这一个码点", s)
		}
	}
	if dot.Match("甲乙") {
		t.Errorf("`.` 是一个码点，不该把两个吞成一段")
	}
	w, err := Compile(`^\w+$`)
	if err != nil {
		t.Fatal(err)
	}
	if w.Match("中文") {
		t.Errorf(`\w 只认 ASCII，中文落不进去`)
	}
	cn, err := Compile(`^[中]+$`)
	if err != nil {
		t.Fatal(err)
	}
	if !cn.Match("中中中") {
		t.Errorf("字符组里写汉字该按码点算")
	}
}

// TestUnsupportedRefuses 固定"写出来就是 10071 号"那一组：不支持的写法要当场拒绝并报错，
// 不能静默按字面字符处理——那比报错更糟。
func TestUnsupportedRefuses(t *testing.T) {
	badPats := []string{
		`a{2,3}`, `a{2,}`, `a{`, `a*?`, `a+?`, `(?i)ab`, `(?:ab)`, `(?<n>a)`, `\1`,
		`\b`, `\B`, `()`, `a)`, `(a`, `[a`, `[]`, `[z-a]`, `a**`, ``, `\`, `[a-\`,
	}
	for _, pat := range badPats {
		_, err := Compile(pat)
		if err == nil {
			t.Errorf("%q 该被拦下，却编过了", pat)
			continue
		}
		if _, ok := err.(*ErrBad); !ok {
			t.Errorf("%q 该交回 *ErrBad（10071 号），got %T：%v", pat, err, err)
		}
	}
	// 合法但容易误判的几条：末尾的 `-` 按字面处理、开头的 `]` 按字面处理、`\-` 按字面处理
	for _, pat := range []string{`[a-z-]`, `[]a]`, `[-]`, `[\]]`, `\]`, `\\`, `[^\]]`} {
		if _, err := Compile(pat); err != nil {
			t.Errorf("%q 该编得过：%v", pat, err)
		}
	}
}

// TestAnchors 固定 `^` 与 `$` 的两条规则：只匹配文本开头与文本末尾，没有多行标志。
func TestAnchors(t *testing.T) {
	both, err := Compile(`^ab$`)
	if err != nil {
		t.Fatal(err)
	}
	if !both.Match("ab") {
		t.Error(`^ab$ 该匹配得上 "ab"`)
	}
	if both.Match("xab") || both.Match("abx") {
		t.Error(`^ab$ 不该在中间匹配上`)
	}
	if both.Match("ab\ncd") {
		t.Error("没有多行标志：$ 只认文本末尾")
	}
	multi, err := Compile(`^\d+$`)
	if err != nil {
		t.Fatal(err)
	}
	if multi.Match("1\n2") {
		t.Error("^ 与 $ 都不认行边界：多行文本里的 1 与 2 不该被当成两个匹配")
	}
}

// TestGreedyAndOrder 固定优先级：贪婪、`|` 先写的分支优先、从左往右不重叠。
func TestGreedyAndOrder(t *testing.T) {
	greedy, err := Compile(`a*`)
	if err != nil {
		t.Fatal(err)
	}
	if got := greedy.FindAll("aaabaa"); strings.Join(got, "|") != "aaa||aa" && strings.Join(got, "|") != "aaa|aa" {
		// 空的那一次按我们的规则被跳过，所以只剩两段非空的
		t.Errorf("`a*` 该取到 aaa 与 aa，got %q", greedy.FindAll("aaabaa"))
	}
	alt, err := Compile(`ab|a`)
	if err != nil {
		t.Fatal(err)
	}
	if got := alt.FindAll("ab"); strings.Join(got, "|") != "ab" {
		t.Errorf("`ab|a` 该取先写的那支 ab，got %q", got)
	}
	group, err := Compile(`(\w)(\w)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := group.FindAll("abcd"); strings.Join(got, "|") != "ab|cd" {
		t.Errorf("不重叠、从左往右：got %q", got)
	}
}

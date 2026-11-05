// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// repair_test.go 钉这一补只管括号：该补的补齐、配好的不动、字符串没收尾的那种不碰，
// 补出来的那几颗排在原文之后，原来每一个记号的行列因此不变。
//
// en: repair_test.go pins what the repair may touch: missing closers get appended, balanced text
// is left alone, and an unterminated string is not repaired. The added tokens come after the
// original text, so every existing token keeps its line and column.
package ls

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/lexer"
)

func TestCloseOpenBracketsAppendsOnlyWhatIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"配好的不动", "let a:int = 1\n", "let a:int = 1\n"},
		{"少一个右花括号", "class Note {\n", "class Note {\n}\n"},
		{"括号按相反的顺序补", "let xs:list<int> = [f(1\n", "let xs:list<int> = [f(1\n)\n]\n"},
		{"字符串没收尾不碰", "let a:string = \"甲\n", "let a:string = \"甲\n"},
	} {
		if got := closeOpenBrackets(tc.text); got != tc.want {
			t.Errorf("%s：得到 %q，要 %q", tc.name, got, tc.want)
		}
	}
}

func TestRepairKeepsEarlierTokensInPlace(t *testing.T) {
	const text = "method twice(n:int):int {\n    return twice(\n"
	fixed := closeOpenBrackets(text)
	before := tokensOf(text)
	after := tokensOf(fixed)
	if len(after) <= len(before) {
		t.Fatalf("补完之后记号该更多：原来 %d 个，现在 %d 个", len(before), len(after))
	}
	for i := range before {
		if before[i].Line != after[i].Line || before[i].Col != after[i].Col || before[i].Text != after[i].Text {
			t.Fatalf("第 %d 个记号被挪动了：%+v 对 %+v", i+1, before[i], after[i])
		}
	}
	if !strings.HasSuffix(fixed, ")\n}\n") {
		t.Errorf("补的内容不对：%q", fixed)
	}
}

// tokensOf 取一份文本的记号（词法过不去也拿到已经切出来的那一段）。
//
// en: tokensOf lexes one text, keeping whatever came out when the lexer stopped early.
func tokensOf(text string) []lexer.Tok {
	toks, _ := lexer.Lex(text)
	out := make([]lexer.Tok, 0, len(toks))
	for _, tk := range toks {
		if tk.Kind == lexer.EOF {
			continue
		}
		out = append(out, tk)
	}
	return out
}

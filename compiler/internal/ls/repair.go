// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// repair.go 处理"写到一半"的那一份文本：括号没闭合时，语法分析过不去，大纲、折叠、语义着色
// 与参数提示就都没有结果——而那正是打字打到一半的常态。
//
// 做法只有一步：拿词法层的记号数一遍没配上的左括号，在末尾按相反的顺序补上对应的右括号，
// 再走一遍前端。补出来的那几颗排在原文之后，原来每一个记号的行列都不变，所以四处的位置仍然对得上。
// 这只是为了让编辑器有东西可显示：编译器与 `nf check` 照旧按原文报错，不会因为这一补就放过谁。
//
// en: repair.go handles half-typed text: with a parenthesis or brace still open, the parser stops,
// and then the outline, folding, semantic colours and parameter hints have nothing to show - which
// is the normal state while typing. The step is narrow: count the unmatched open brackets using the
// lexer's tokens and append the matching closers at the end, in reverse order, then run the front
// end again. The appended tokens sit after the original text, so every existing token keeps its
// line and column and all four answers stay aligned with what is on screen. This only gives the
// editor something to show; the compiler and `nf check` still read the real text and still report
// the missing bracket.
package ls

import (
	"strings"

	"neflang.org/nf/internal/lexer"
)

// closeOpenBrackets 补上没配对的右括号；已经配好就原样返回。
//
// en: closeOpenBrackets appends the unmatched closing brackets; text that balances comes back
// unchanged.
func closeOpenBrackets(text string) string {
	toks, err := lexer.Lex(text)
	if err != nil {
		// 词法本身过不去（字符串没有收尾）：那时补括号帮不上忙，原样交给前端报错。
		return text
	}
	var open []string
	for _, t := range toks {
		if t.Kind != lexer.Op {
			continue
		}
		switch t.Text {
		case "(", "{", "[":
			open = append(open, t.Text)
		case ")", "}", "]":
			if n := len(open); n > 0 && pairs(open[n-1], t.Text) {
				open = open[:n-1]
			}
		}
	}
	if len(open) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	for i := len(open) - 1; i >= 0; i-- {
		b.WriteString(closerOf(open[i]))
		b.WriteByte('\n')
	}
	return b.String()
}

func pairs(open, closeWith string) bool {
	return closerOf(open) == closeWith
}

func closerOf(open string) string {
	switch open {
	case "(":
		return ")"
	case "{":
		return "}"
	case "[":
		return "]"
	}
	return ""
}

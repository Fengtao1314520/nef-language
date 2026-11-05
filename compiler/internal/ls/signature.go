// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// signature.go 是参数提示那一条：写到 `console.print(` 或者 `twice(` 的括号里，编辑器问
// "这一格要填什么"，答复是一行签名加上"光标现在在第几格"。
//
// 签名与悬停、补全都是同一张表算出来的：`Facts.Sigs`（自己写的函数与方法）、
// `StdSignature`／`StdParameters`（标准库的组与内建动作）、`MemberSignature`／`MemberParameters`
// （具体容器上的成员）。这一份不重抄一份参数表，改一处三处跟着变。
// 括号里填到一半（左括号刚打上、实参还没写）也算：那一份文本先按 repair.go 补全再问检查器。
// 认不出被叫的是谁才答"没有提示"——这时候屏幕上不该多出一行猜的话。
//
// en: signature.go is parameter help: inside `console.print(` or `twice(` the editor asks what this
// slot takes, and gets one signature line plus which slot the cursor is on. The signatures come
// from the same tables as hover and completion - Facts.Sigs for the functions and methods the
// program declares, StdSignature/StdParameters for the standard-library groups and built-ins,
// MemberSignature/MemberParameters for a member on a particular container. Nothing is copied here,
// so one change moves all three. Half-typed arguments count too: the text is completed per
// repair.go before the checker is asked. Only when the callee cannot be worked out is the answer
// "no help" - a guessed line on screen is worse than none.
package ls

import (
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
)

// ParameterInformation 是签名里的一格：显示出来的那一段文字。
//
// en: ParameterInformation is one slot in a signature: the text shown for it.
type ParameterInformation struct {
	Label string `json:"label"`
}

// SignatureInformation 是一行签名：整行文字与它每一格的文字。
//
// en: SignatureInformation is one signature line: the whole line plus each slot.
type SignatureInformation struct {
	Label      string                 `json:"label"`
	Parameters []ParameterInformation `json:"parameters,omitempty"`
}

// SignatureHelp 是这一条的答复：可选的签名列表、当前那一条的序号、当前那一个参数的序号。
//
// en: SignatureHelp is the reply: the candidate signatures, which one is active, and which
// parameter the cursor sits on.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

// SignatureHelpInfo 返回光标所在那一次调用的参数提示。认不出来返回空。
//
// en: SignatureHelpInfo answers the parameter help for the call the cursor is inside; nil when the
// callee cannot be worked out.
func SignatureHelpInfo(path, text string, at Position) *SignatureHelp {
	src := newChecked(path, text)
	if src.facts == nil {
		return nil
	}
	open, commas, ok := src.enclosingCall(at)
	if !ok {
		return nil
	}
	label, params := src.calleeSignature(open)
	if label == "" {
		return nil
	}
	sig := SignatureInformation{Label: label}
	for _, p := range params {
		sig.Parameters = append(sig.Parameters, ParameterInformation{Label: p})
	}
	active := commas
	if len(params) == 0 {
		active = 0
	} else if active > len(params)-1 {
		active = len(params) - 1
	}
	return &SignatureHelp{Signatures: []SignatureInformation{sig}, ActiveParameter: active}
}

// enclosingCall 从光标往回走，找包住它的那一个左括号，顺带数括号里出现过几个平层的逗号。
// 返回的是那一个左括号在记号表里的下标。
//
// en: enclosingCall walks back from the cursor to the left parenthesis around it, counting the
// same-level commas on the way. It returns that parenthesis's index in the token list.
func (s source) enclosingCall(at Position) (int, int, bool) {
	depth, commas := 0, 0
	for i := len(s.toks) - 1; i >= 0; i-- {
		t := s.toks[i]
		if t.Kind == lexer.EOF {
			continue
		}
		if !s.tokenBefore(t, at) {
			continue
		}
		if t.Kind != lexer.Op {
			continue
		}
		switch t.Text {
		case ")", "]", "}":
			depth++
		case "(":
			if depth == 0 {
				return i, commas, true
			}
			depth--
		case ",":
			if depth == 0 {
				commas++
			}
		}
	}
	return 0, 0, false
}

// tokenBefore 说这一个记号是否排在光标之前（或正压在光标下）。协议里的列按 UTF-16 计，
// 词法层的列按字符计，两边在这一行上换算一次。
//
// en: tokenBefore says whether a token lies at or before the cursor. The protocol counts columns in
// UTF-16 units and the lexer in characters, so this converts once on that line.
func (s source) tokenBefore(t lexer.Tok, at Position) bool {
	if t.Line-1 != at.Line {
		return t.Line-1 < at.Line
	}
	return s.u16Col(t.Line, t.Col) <= at.Character
}

// calleeSignature 认左括号前面那一个名字，返回它的一行签名与每一格的文字。
// 三种写法都在这一条里：`twice(`、`console.print(`、`xs.add(`（后面那一种是挂在具体容器上的成员）。
//
// en: calleeSignature reads the name in front of that parenthesis and returns its signature line
// plus each slot. All three spellings land here: `twice(`, `console.print(`, and `xs.add(` - the
// last being a member on a particular container.
func (s source) calleeSignature(open int) (string, []string) {
	if open <= 0 || open >= len(s.toks) {
		return "", nil
	}
	name := s.toks[open-1]
	if name.Kind != lexer.Name {
		return "", nil
	}
	// `组名.动作(` 与 `值.方法(`：点号前面那一个名字决定走哪一条。
	if open >= 3 && s.toks[open-2].Kind == lexer.Op && s.toks[open-2].Text == "." {
		recv := s.toks[open-3]
		if recv.Kind != lexer.Name {
			return "", nil
		}
		if check.IsGroup(recv.Text) {
			dot := recv.Text + "." + name.Text
			return check.StdSignature(dot), check.StdParameters(dot)
		}
		t := receiverType(s.facts, recv.Line, recv.Col, recv.Text)
		if t.Kind == check.ErrT {
			return "", nil
		}
		if label := check.MemberSignature(t, name.Text); label != "" {
			return label, check.MemberParameters(t, name.Text)
		}
		// 挂在具体类型上的方法（`p.open(`）：签名表里的键是"类型名.方法名"，包里的带包根。
		key := s.methodKey(t, name.Text)
		if key == "" {
			return "", nil
		}
		return s.facts.Signature(key), s.facts.SignatureParams(key)
	}
	key := sigKeyFrom(s.facts, name.Text, s.path)
	if key == "" {
		return "", nil
	}
	return s.facts.Signature(key), s.facts.SignatureParams(key)
}

// methodKey 按接收者的类型名与方法名找签名表里的那一条：类型名带着包根时两种写法都试一遍。
//
// en: methodKey looks a method up by its receiver's type name and the method name, trying both
// spellings when the type carries a package root.
func (s source) methodKey(t check.Type, name string) string {
	for _, owner := range []string{t.Name, check.BareName(t.Name)} {
		if owner == "" {
			continue
		}
		key := owner + "." + name
		if s.facts.Signature(key) != "" {
			return key
		}
	}
	return ""
}

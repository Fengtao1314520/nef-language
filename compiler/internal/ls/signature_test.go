// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// signature_test.go 钉参数提示那一条：自己写的函数与方法、标准库的组动作、具体容器上的成员
// 三种写法各答什么，光标在第几格，认不出被叫的是谁时答"没有提示"，
// 以及 initialize 有没有把触发键报给编辑器。
//
// en: signature_test.go pins parameter help: what a declared function or method, a standard-library
// action and a member on a particular container each answer, which slot the cursor sits in, that an
// unrecognised callee answers "no help", and that initialize sends the trigger characters.
package ls

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSignatureHelpForOwnFunction(t *testing.T) {
	const text = `method twice(n:int):int {
    return n * 2
}
console.print(twice(1))
`
	// 光标在 `twice(` 的左括号后面一点：`console.print(twice(1))` 里那一个实参的位置。
	help := SignatureHelpInfo("p.nf", text, Position{Line: 3, Character: 20})
	if help == nil {
		t.Fatal("twice( 里没有给出参数提示")
	}
	if len(help.Signatures) != 1 {
		t.Fatalf("该只有一行签名：%+v", help.Signatures)
	}
	if got := help.Signatures[0].Label; got != "twice(n: int): int" {
		t.Errorf("签名那行不对：得到 %q", got)
	}
	if got := help.Signatures[0].Parameters; len(got) != 1 || got[0].Label != "n: int" {
		t.Errorf("每一格没列出来：%+v", got)
	}
	if help.ActiveParameter != 0 {
		t.Errorf("光标在第一格，activeParameter 该是 0，得到 %d", help.ActiveParameter)
	}
}

func TestSignatureHelpCountsCommasAtThisLevel(t *testing.T) {
	const text = `method between(a:int, b:int):int {
    return a + b
}
let k:int = between(1, 2)
`
	help := SignatureHelpInfo("p.nf", text, Position{Line: 3, Character: 23})
	if help == nil {
		t.Fatal("between( 里没有给出参数提示")
	}
	if help.ActiveParameter != 1 {
		t.Errorf("逗号后面该指向第二格，得到 %d（签名 %+v）", help.ActiveParameter, help.Signatures[0])
	}
	if got := help.Signatures[0].Parameters; len(got) != 2 || got[1].Label != "b: int" {
		t.Errorf("两格没列出来：%+v", got)
	}
	// 括号里再套一次调用：里层的逗号不算在这一条上。
	inner := SignatureHelpInfo("p.nf", text, Position{Line: 3, Character: 20})
	if inner == nil {
		t.Fatal("光标在左括号右边一点也该有提示")
	}
	if inner.ActiveParameter != 0 {
		t.Errorf("第一个实参之前该是第 0 格，得到 %d", inner.ActiveParameter)
	}
}

func TestSignatureHelpForStdGroupAndContainerMember(t *testing.T) {
	const printText = `console.print("甲", "乙")
`
	help := SignatureHelpInfo("p.nf", printText, Position{Line: 0, Character: 18})
	if help == nil {
		t.Fatal("console.print( 里没有给出参数提示")
	}
	if !strings.HasPrefix(help.Signatures[0].Label, "console.print(") {
		t.Errorf("组动作的签名头不对：%q", help.Signatures[0].Label)
	}
	if got := help.Signatures[0].Parameters; len(got) != 1 || !strings.HasPrefix(got[0].Label, "...") {
		t.Errorf("变长的那种该只列一格（`...values`）：%+v", got)
	}
	if help.ActiveParameter != 0 {
		t.Errorf("一格也只有一个可指的位置，得到 %d", help.ActiveParameter)
	}

	const memberText = `let xs:list<int> = [1]
xs.add(2)
`
	help = SignatureHelpInfo("p.nf", memberText, Position{Line: 1, Character: 7})
	if help == nil {
		t.Fatal("xs.add( 里没有给出参数提示")
	}
	if !strings.Contains(help.Signatures[0].Label, "add(") {
		t.Errorf("具体容器上的成员签名不对：%q", help.Signatures[0].Label)
	}
	if got := help.Signatures[0].Parameters; len(got) == 0 || got[0].Label == "" {
		t.Errorf("这一格没列出来：%+v", got)
	}
}

func TestSignatureHelpStaysQuietWhenItCannotTell(t *testing.T) {
	// 认不出的名字：宁可不显示，也不猜一行。
	if help := SignatureHelpInfo("p.nf", "nope(1)\n", Position{Line: 0, Character: 5}); help != nil {
		t.Errorf("认不出的调用不该有提示：%+v", help)
	}
	// 光标不在括号里。
	if help := SignatureHelpInfo("p.nf", "let a:int = 1\n", Position{Line: 0, Character: 10}); help != nil {
		t.Errorf("不在调用里不该有提示：%+v", help)
	}
	// 左括号还没打上。
	if help := SignatureHelpInfo("p.nf", "twice\n", Position{Line: 0, Character: 5}); help != nil {
		t.Errorf("括号没打开不该有提示：%+v", help)
	}
}

func TestServeAnswersSignatureHelp(t *testing.T) {
	const uri = "file:///p/nf/sample.nf"
	msgs := exchange(t,
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"`+uri+
			`","languageId":"nf","version":1,"text":"method twice(n:int):int {\n    return n * 2\n}\nconsole.print(twice(\n"}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"`+uri+
			`"},"position":{"line":3,"character":20}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`))

	var res initResult
	if err := json.Unmarshal(replyOf(t, msgs, "1").Result, &res); err != nil {
		t.Fatalf("initialize 的答复形状不对：%v", err)
	}
	sh := res.Capabilities.SignatureHelpProvider
	if sh == nil || len(sh.TriggerCharacters) == 0 {
		t.Fatalf("initialize 没答应 signatureHelpProvider 或没给触发键：%+v", sh)
	}
	var help SignatureHelp
	if err := json.Unmarshal(replyOf(t, msgs, "4").Result, &help); err != nil {
		t.Fatalf("参数提示的答复形状不对：%v", err)
	}
	if len(help.Signatures) != 1 || help.Signatures[0].Label != "twice(n: int): int" {
		t.Fatalf("答复里没有那一行签名：%+v", help.Signatures)
	}
}

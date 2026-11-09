// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// generic_test.go 钉泛型在编辑器这一侧看到什么：点号后面列出的是字段与方法的名字
// （不是 `take<string>` 那种带键的名字），参数提示答的是这一次调用真正落到的那一只实例，
// 光标停在模板的名字上时答的是带类型参数的那一份签名。
//
// en: generic_test.go pins what the editor sees for generics: after a dot the list holds field
// and method names (not keyed names like take<string>), parameter help answers the instance this
// call actually lands on, and hovering the template's own name shows the signature with its type
// parameters.
package ls

import (
	"strings"
	"testing"
)

const genericSrc = `class Cell<T> {
    v:T

    method get():T {
        return v
    }

    method take<E>(k:E):E {
        return k
    }
}

method twice<Element>(v:Element):Element {
    return v
}

let c:Cell<int> = new Cell<int>(v: 1)
let n:int = twice(1)
let s:string = twice("甲")
`

// TestCompletionAfterDotOnAnInstance 钉住点号后面补出来的是名字本身：
// `c.` 要给出 v、get、take 三条，`take` 只能出现一次（两种实参的键不能各列一条）。
//
// en: TestCompletionAfterDotOnAnInstance pins that the dot offers names: c. must list v, get and
// take, with take once - two keyed instances must not each add a line.
func TestCompletionAfterDotOnAnInstance(t *testing.T) {
	text := genericSrc + "c.\n"
	got := labels(Completion("/p/sample.nf", text, cursorAfter(text, "c.")))
	for _, w := range []string{"v", "get", "take"} {
		if !hasLabel(got, w) {
			t.Errorf("c. 后面补不出 %s：%v", w, got)
		}
	}
	for _, line := range got {
		if strings.Contains(line, "<") {
			t.Errorf("补全里出现了带尖括号的键：%v", got)
		}
	}
	if strings.Count(strings.Join(got, "\n"), "\ntake") > 1 {
		t.Errorf("take 列了不止一条：%v", got)
	}
}

// TestSignatureHelpNamesTheInstance 钉住参数提示答的是这一次调用真正落到的那一只实例：
// `twice(1)` 是 `twice(v: int): int`，`twice("甲")` 是 `twice(v: string): string`。
//
// en: TestSignatureHelpNamesTheInstance pins that help shows the instance each call reaches:
// twice(1) reads twice(v: int): int, twice("甲") reads twice(v: string): string.
func TestSignatureHelpNamesTheInstance(t *testing.T) {
	two := `method twice<Element>(v:Element):Element {
    return v
}

let n:int = twice(1)
let s:string = twice("甲")
`
	first := SignatureHelpInfo("p.nf", two, cursorAfter(two, "twice(1"))
	if first == nil || len(first.Signatures) != 1 {
		t.Fatalf("twice(1 里没给出参数提示：%+v", first)
	}
	if got := first.Signatures[0].Label; got != "twice(v: int): int" {
		t.Errorf("第一份实例的签名不对：得到 %q", got)
	}
	second := SignatureHelpInfo("p.nf", two, cursorAfter(two, `twice("甲"`))
	if second == nil || len(second.Signatures) != 1 {
		t.Fatalf(`twice("甲" 里没给出参数提示：%+v`, second)
	}
	if got := second.Signatures[0].Label; got != "twice(v: string): string" {
		t.Errorf("第二份实例的签名不对：得到 %q", got)
	}
}

// TestSignatureHelpForMethodWithOwnParameters 钉住方法自己带一层类型参数时的提示：
// `c.take("乙")` 答的是 `Cell<int>.take(k: string): string`，
// 括号里再写一格时也是同一只实例（键带着两层实参，展示只给人看的那一串）。
//
// en: TestSignatureHelpForMethodWithOwnParameters pins help for a method carrying its own
// parameters: c.take("乙") answers Cell<int>.take(k: string): string, and the same instance
// shows while typing the next slot - the key holds both argument layers, the display only the
// reader-facing one.
func TestSignatureHelpForMethodWithOwnParameters(t *testing.T) {
	text := genericSrc + `let k:string = c.take("乙")
`
	help := SignatureHelpInfo("p.nf", text, cursorAfter(text, `c.take("乙"`))
	if help == nil || len(help.Signatures) != 1 {
		t.Fatalf("c.take( 里没给出参数提示：%+v", help)
	}
	if got := help.Signatures[0].Label; got != "Cell<int>.take(k: string): string" {
		t.Errorf("方法级类型参数的签名不对：得到 %q", got)
	}
}

// TestGenericNamesInEditor 钉住名字那一栏与悬停各答什么：名字栏里 `twice` 只列一条，
// 写的是模板的签名（`twice(v: Element): Element`，类型参数的名字留着）；
// 悬停在 `twice(1)` 上答的是这一次调用落到的那一只实例（`twice(v: int): int`），
// 换成 `twice("甲")` 就换成 `twice(v: string): string`。
//
// en: TestGenericNamesInEditor pins what the name column and hover each answer: the column lists
// twice once, with the template's signature (twice(v: Element): Element - the parameter's own name
// kept), while hover on twice(1) answers the instance that call reaches (twice(v: int): int) and
// twice("甲") answers twice(v: string): string.
func TestGenericNamesInEditor(t *testing.T) {
	text := genericSrc + "twic\n"
	got := Completion("/p/sample.nf", text, cursorAfter(text, "twic"))
	var hits []string
	for _, it := range got {
		if it.Label == "twice" {
			hits = append(hits, it.Detail)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("twice 在名字栏里列了 %d 条：%v", len(hits), hits)
	}
	if hits[0] != "twice(v: Element): Element" {
		t.Errorf("名字栏里的模板签名不对：得到 %q", hits[0])
	}
	first := Hover("/p/sample.nf", genericSrc, cursorOn(genericSrc, "twice(1)"))
	if first == nil {
		t.Fatal("悬停在 twice(1) 上没有内容")
	}
	if !strings.Contains(first.Contents.Value, "twice(v: int): int") {
		t.Errorf("第一次调用的悬停没落到那一只实例：%q", first.Contents.Value)
	}
	second := Hover("/p/sample.nf", genericSrc, cursorOn(genericSrc, `twice("甲")`))
	if second == nil {
		t.Fatal(`悬停在 twice("甲") 上没有内容`)
	}
	if !strings.Contains(second.Contents.Value, "twice(v: string): string") {
		t.Errorf(`第二次调用的悬停没落到那一只实例：%q`, second.Contents.Value)
	}
}

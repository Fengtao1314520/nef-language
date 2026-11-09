// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// defhover_test.go 钉光标停在定义那一行的名字上时悬停答什么：顶层函数、类体内的方法、
// 方法自己带一层类型参数的、接口里的方法声明四种都要答得出签名，写在声明上面那段注释跟着上屏。
// 带类型参数的函数答的是模板那一份（类型参数写自己的名字），不是文件里某一次调用落到的实例。
//
// en: defhover_test.go pins what hover answers when the cursor rests on the name in a definition
// line: top-level functions, methods inside a class body, methods carrying their own type
// parameters, and method declarations inside an interface all answer with a signature, and the
// comment written above the declaration reaches the screen. A function with type parameters
// answers with the template (its parameters keep their own names), not the instance some call in
// the same file happens to reach.
package ls

import (
	"strings"
	"testing"
)

const defSrc = `// 把传进来的那一个数原样返回。
// en: Returns the number it was given.
method plain(a:int):int {
    return a
}

// 传进来什么就返回什么，类型由调用处定。
// en: Returns what it was given; the type is settled at the call site.
method twice<Element>(v:Element):Element {
    return v
}

class Cell<T> {
    v:T

    // 读出存着的那一个值。
    // en: Reads the stored value.
    method get():T {
        return v
    }

    method take<E>(k:E):E {
        return k
    }
}

interface Shape {
    perim():float
}

let c:Cell<int> = new Cell<int>(v: 1)
let n:int = twice(1)
let s:string = twice("甲")
let k:string = c.take("乙")
`

// hoverDef 问定义那一行：锚点写到名字写完为止，光标正好压在名字后面那一个字符之前。
//
// en: hoverDef asks about a definition line: the anchor runs to the end of the name, so the
// cursor sits right after it.
func hoverDef(t *testing.T, anchor string) *HoverResult {
	t.Helper()
	h := Hover("/p/def.nf", defSrc, cursorAfter(defSrc, anchor))
	if h == nil {
		t.Fatalf("定义那一行上问 %q 没有答复", anchor)
	}
	return h
}

func TestHoverOnAFunctionDefinition(t *testing.T) {
	h := hoverDef(t, "method plain")
	for _, w := range []string{"plain(a: int): int", "把传进来的那一个数原样返回。", "Returns the number it was given."} {
		if !strings.Contains(h.Contents.Value, w) {
			t.Errorf("悬停里没有 %q：%q", w, h.Contents.Value)
		}
	}
}

func TestHoverOnAGenericFunctionDefinition(t *testing.T) {
	h := hoverDef(t, "method twice")
	if !strings.Contains(h.Contents.Value, "twice(v: Element): Element") {
		t.Errorf("模板的定义行没答带类型参数的那一份：%q", h.Contents.Value)
	}
	for _, w := range []string{"twice(v: int): int", "twice(v: string): string"} {
		if strings.Contains(h.Contents.Value, w) {
			t.Errorf("定义行答成了某一只实例 %q：%q", w, h.Contents.Value)
		}
	}
	if !strings.Contains(h.Contents.Value, "传进来什么就返回什么，类型由调用处定。") {
		t.Errorf("声明上面那段注释没上屏：%q", h.Contents.Value)
	}
}

func TestHoverOnAMethodDefinition(t *testing.T) {
	h := hoverDef(t, "method get")
	if !strings.Contains(h.Contents.Value, "Cell.get(): T") {
		t.Errorf("模板里的方法没答类型参数自己那一份：%q", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "读出存着的那一个值。") {
		t.Errorf("方法上面那段注释没上屏：%q", h.Contents.Value)
	}
}

func TestHoverOnAMethodWithOwnTypeParameters(t *testing.T) {
	h := hoverDef(t, "method take")
	if !strings.Contains(h.Contents.Value, "Cell.take(k: E): E") {
		t.Errorf("方法自己带类型参数的定义行没答模板那一份：%q", h.Contents.Value)
	}
}

func TestHoverOnAnInterfaceMethodDeclaration(t *testing.T) {
	h := hoverDef(t, "    perim")
	if !strings.Contains(h.Contents.Value, "Shape.perim(): float") {
		t.Errorf("接口里的方法声明没答出签名：%q", h.Contents.Value)
	}
}

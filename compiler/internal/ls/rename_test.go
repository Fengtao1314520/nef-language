// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// rename_test.go 钉"重命名"这一条：改完的文本逐字对得上，关键字与内建名字当场拒，
// 撞名与改完编译不过由改后校验拦住，接口的方法带上实现那一家一起改，
// 跨文件与包容器各按各的规矩。
//
// en: rename_test.go pins rename: the renamed text matches word for word, keywords and built-in
// names are refused on the spot, clashes and other compile breakage are caught by the
// after-check, an interface method changes together with its implementing family, and
// cross-file renames and package containers follow their own rules.
package ls

import (
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/pkgfmt"
)

// applyEdits 把一份文件上的改动落下去，核对改完的整份文本。
//
// en: applyEdits applies one file's edits and checks the whole resulting text.
func applyEdits(t *testing.T, text string, edits []TextEdit, want string) {
	t.Helper()
	// 从后往前落，前面的行列不受影响。
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		lines := splitLines(text)
		start := lineOffset(text, e.Range.Start.Line) + byteColOf(lines[e.Range.Start.Line], e.Range.Start.Character)
		end := lineOffset(text, e.Range.End.Line) + byteColOf(lines[e.Range.End.Line], e.Range.End.Character)
		text = text[:start] + e.NewText + text[end:]
	}
	if text != want {
		t.Fatalf("改完的文本不对：\n got %q\nwant %q", text, want)
	}
}

func TestRenameLocal(t *testing.T) {
	const text = `method run() {
    let sum:int = 0
    sum = sum + 1
    console.print(sum)
}
`
	edit, msg := Rename("p.nf", text, atToken(t, text, "sum", 0), "total")
	if msg != "" {
		t.Fatalf("局部量改名被拒：%s", msg)
	}
	applyEdits(t, text, edit.Changes[uriFromPath("p.nf")], `method run() {
    let total:int = 0
    total = total + 1
    console.print(total)
}
`)
}

func TestRenameRejectsInvalidNames(t *testing.T) {
	const text = "let n:int = 7\nconsole.print(n)\n"
	for _, name := range []string{"let", "int", "console", "1x", "甲", ""} {
		if _, msg := Rename("p.nf", text, atToken(t, text, "n", 0), name); msg == "" {
			t.Fatalf("改成 %q 该被拒", name)
		}
	}
	// 标准库的名字没有声明可改。
	if _, msg := Rename("p.nf", text, atToken(t, text, "print", 0), "show"); msg == "" {
		t.Fatal("标准库的动作该改不了")
	}
	// 同名不算错，交空改动。
	edit, msg := Rename("p.nf", text, atToken(t, text, "n", 0), "n")
	if msg != "" || len(edit.Changes) != 0 {
		t.Fatalf("同名该交空改动：%v %s", edit, msg)
	}
}

func TestRenameRefusesWhenResultBreaks(t *testing.T) {
	const text = `method run() {
    let a:int = 1
    let b:int = 2
    console.print(a, b)
}
`
	// 同一层里已经有 b：改完编译不过，一处也不交。
	if _, msg := Rename("p.nf", text, atToken(t, text, "a", 0), "b"); msg == "" {
		t.Fatal("改成同层已有的名字该被改后校验拦住")
	}
}

func TestRenameInterfaceFamily(t *testing.T) {
	const text = `interface Shape {
    perim():float
}

class Circle base Shape {
    r:float

    method perim():float {
        return 6.28 * r
    }
}

class Square base Shape {
    side:float

    method perim():float {
        return side * 4.0
    }
}

let c:Circle = new Circle(r: 1.0)
let s:Square = new Square(side: 2.0)
console.print(c.perim(), s.perim())
`
	edit, msg := Rename("p.nf", text, atToken(t, text, "perim", 0), "boundary")
	if msg != "" {
		t.Fatalf("接口方法改名被拒：%s", msg)
	}
	applyEdits(t, text, edit.Changes[uriFromPath("p.nf")], strings.ReplaceAll(text, "perim", "boundary"))
}

func TestRenameCrossFile(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.ToSlash(filepath.Join(dir, "a.nf"))
	aText := "export method twice(n:int):int {\n    return n * 2\n}\n"
	write(t, aPath, aText)
	main := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	text := "import { twice } from \"./a.nf\"\n\nconsole.print(twice(3))\n"
	write(t, main, text)
	edit, msg := Rename(main, text, atToken(t, text, "twice", 1), "double")
	if msg != "" {
		t.Fatalf("跨文件改名被拒：%s", msg)
	}
	if len(edit.Changes) != 2 {
		t.Fatalf("该改两份文件：%v", edit.Changes)
	}
	applyEdits(t, text, edit.Changes[uriFromPath(main)], strings.ReplaceAll(text, "twice", "double"))
	applyEdits(t, aText, edit.Changes[uriFromPath(aPath)], strings.ReplaceAll(aText, "twice", "double"))
}

func TestRenameContainerRefused(t *testing.T) {
	dir := t.TempDir()
	container := filepath.Join(dir, "libs", "greet.nfpack")
	if err := pkgfmt.Pack(container, pkgfmt.Meta{Name: "greet", Version: "1.0.0"}, []pkgfmt.File{
		{Rel: "greet.nf", Text: "export method hello():int {\n    return 1\n}\n"},
	}); err != nil {
		t.Fatalf("打容器失败：%v", err)
	}
	main := filepath.ToSlash(filepath.Join(dir, "main.nf"))
	text := "import gt from \"libs/greet\"\n\nconsole.print(gt.hello())\n"
	write(t, main, text)
	if _, msg := Rename(main, text, atToken(t, text, "hello", 0), "hi"); msg == "" {
		t.Fatal("包容器里的名字该改不了")
	}
}

func TestPrepareRename(t *testing.T) {
	const text = "let total:int = 4\nconsole.print(total)\n"
	rng := PrepareRename("p.nf", text, atToken(t, text, "total", 0))
	if rng == nil || rng.Start.Character != 4 || rng.End.Character != 9 {
		t.Fatalf("total 的范围该是第 5 到第 9 列：%+v", rng)
	}
	if rng := PrepareRename("p.nf", text, atToken(t, text, "print", 0)); rng != nil {
		t.Fatalf("标准库的动作该答不能改：%+v", rng)
	}
}
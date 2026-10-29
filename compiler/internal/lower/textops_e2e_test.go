// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestTextActionsRun 覆盖这一批接入的文本动作：大小写、去首尾空白、查找、替换、按码点取子串、
// 拆成码点 list、把 list 拼接成串。期望串全部来自本机实际运行，不是手工推导。
func TestTextActionsRun(t *testing.T) {
	got, code := run(t, `let mixed:string = "Hello, 世界 Abc"
console.print(mixed.to_upper())
console.print(mixed.to_lower())
console.print("  甲,乙,丙  ".trim() + "|")
console.print(" \t\n x \n\t ".trim() + "|")
console.print("|" + "      ".trim() + "|")
let s:string = "中文abc"
console.print(s.contains("乙"), s.contains("中"), s.contains("中文abc"), s.contains("中文abcd"))
console.print(s.slice(1, 3), "|", s.slice(0, 0), "|", s.slice(2, 5), "|", s.slice(0, length(s)))
let chars:list<unicode> = s.to_list()
console.print(length(chars), chars[0], chars[1], chars[4])
console.print(s.replace("乙", "X"), "|", "aaa".replace("aa", "Z"), "|", s.replace("中", "一"))
console.print(length(s.replace("中", "一一")), s.replace("中", "一一"))
let swapped:string = s.replace("", "X")
console.print(swapped, length(swapped))
let items:list<string> = ["甲", "乙", "丙"]
console.print(text.join(items, "、"))
console.print(text.join(["只有一样"], "-"))
let none:list<string> = []
let joined:string = text.join(none, ",")
console.print("[" + joined + "]", length(joined))
console.print(text.join(["", "", ""], "+"))
console.print(text.join("a,b,c".split(","), "+"))
`)
	want := "HELLO, 世界 ABC\nhello, 世界 abc\n甲,乙,丙|\nx|\n||\n" +
		"false true true false\n文a |  | abc | 中文abc\n" +
		"5 中 文 c\n中文abc | Za | 一文abc\n6 一一文abc\n" +
		"中文abc 5\n甲、乙、丙\n只有一样\n[] 0\n++\na+b+c\n"
	if got != want || code != 0 {
		t.Fatalf("文本动作不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTextActionsThrow 固定越界与空句柄那两种运行期错误，错误码与别的检查同一套。
func TestTextActionsThrow(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
		code int
	}{
		{"slice 反着写", "let s:string = \"中文abc\"\nconsole.print(s.slice(3, 1))\n", "出错了（10031）", 1},
		{"slice 止越界", "let s:string = \"中文abc\"\nconsole.print(s.slice(0, 99))\n", "出错了（10031）", 1},
		{"slice 起是负的", "let s:string = \"中文abc\"\nconsole.print(s.slice(0 - 1, 2))\n", "出错了（10031）", 1},
		{"点空文本的东西", "let z:string = null\nconsole.print(z.trim())\n", "出错了（10092）", 1},
		{"join 里有空句柄", "let z:string = null\nlet xs:list<string> = []\nxs.add(z)\nconsole.print(text.join(xs, \",\"))\n", "出错了（10092）", 1},
	}
	for _, one := range cases {
		got, c := run(t, one.src)
		if c != one.code || !strings.Contains(got, one.want) {
			t.Fatalf("%s：got %q 码 %d，要含 %q 码 %d", one.name, got, c, one.want, one.code)
		}
	}
}

// TestJoinRefusesWhatItCannotDo 固定 text.join 的入参类型：它读的是文本句柄，
// 收一个 list<int> 会在运行期把整数当地址用，所以这一条在编译期拒绝。
func TestJoinRefusesWhatItCannotDo(t *testing.T) {
	if err := compileErr("let ns:list<int> = [1, 2]\nconsole.print(text.join(ns, \",\"))\n"); err == nil ||
		!strings.Contains(err.Error(), "list<string>") {
		t.Fatalf("join 收非文本list该在编译期拦下：err=%v", err)
	}
	if err := compileErr("console.print(text.join(\"甲\", \",\"))\n"); err == nil ||
		!strings.Contains(err.Error(), "list<string>") {
		t.Fatalf("join 收整条文本该在编译期拦下：err=%v", err)
	}
}

// TestTextActionsBorrow 验这些动作确实只读借用：走完原来那个名字还在，还能接着用。
// 标准库那九组是唯一不接管的一批；这条规则一旦不成立，站上的例子全都得改。
func TestTextActionsBorrow(t *testing.T) {
	got, code := run(t, `let s:string = "  abc  "
let t1:string = s.trim()
let t2:string = s.to_upper()
console.print(s, t1, t2, length(s))
let items:list<string> = ["甲", "乙"]
console.print(text.join(items, "-"), length(items), items[0])
`)
	want := "  abc   abc   ABC   7\n甲-乙 2 甲\n"
	if got != want || code != 0 {
		t.Fatalf("借用不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 借出去之后原名还能用；反过来"交给一个名字"仍然要拦
	if err := compileErr("let s:string = \"甲\"\nlet t:string = s\nconsole.print(s.trim())\n"); err == nil ||
		!strings.Contains(err.Error(), "已经交出去") {
		t.Fatalf("交出后的原名再点东西该被拦：err=%v", err)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileops_e2e_test.go 覆盖七条文件相关成员：
// 属性三条（大小／修改时间／只读）、目录两条（名字／全路径）、分块两条（读一段／覆盖一段）。
// 错误码划分：属性拿不到是 10045，目录类是 10043；分块读取的位置按字节计、结尾不拆分码点。

// TestFileAttrs 三条属性一起测量：字节数、秒为单位的尺度、只读标志。
// 修改时间不比对具体数值（随时间变化），只验证它"不晚于当前时间"。
func TestFileAttrs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(p, []byte("甲乙丙"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let p:string = "`+pp+`"
console.print(file.size(p))
console.print(file.modified(p) <= time.now())
console.print(file.readonly(p))
`)
	if got != "9\ntrue\nfalse\n" || code != 0 {
		t.Fatalf("三条属性不对：\n got %q 码 %d\nwant %q", got, code, "9\ntrue\nfalse\n")
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatalf("改只读失败：%v", err)
	}
	got, code = run(t, `console.print(file.readonly("`+pp+`"))`)
	if got != "true\n" || code != 0 {
		t.Fatalf("只读标志没认出来：got %q 码 %d", got, code)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatalf("改回可写失败：%v", err)
	}
}

// TestFileAttrCodes 验证两个新错误码各自独立：路径不存在是 10045，目标是目录是 10043。
// 设计取"两个号各管一件事"，不把目录类并入"读不到"那个号。
func TestFileAttrCodes(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.ToSlash(filepath.Join(dir, "没有这个文件.md"))
	asDir := filepath.ToSlash(dir)
	cases := []struct{ src, want string }{
		{`console.print(file.size("` + gone + `"))`, "10045）：拿不到这个文件的属性"},
		{`console.print(file.size("` + asDir + `"))`, "10043）：那是一个目录，不是一个文件"},
		{`console.print(file.list("` + gone + `"))`, "10043）：打不开这个目录"},
	}
	for _, c := range cases {
		got, code := run(t, c.src+"\n")
		if code != 1 || !strings.Contains(got, c.want) {
			t.Fatalf("%s 得到 %q 码 %d，要含 %q", c.src, got, code, c.want)
		}
	}
}

// TestFileListNamesAndPaths 覆盖这一对成员：同一个目录，一条返回名字、一条返回全路径。
// 点条目（`.` 与 `..`）不得出现在名单里——这个目录只放一个文件，长度就是最直接的证据。
func TestFileListNamesAndPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	d := filepath.ToSlash(dir)
	got, code := run(t, `
let d:string = "`+d+`"
let names:list<string> = file.list(d)
let paths:list<string> = file.list_paths(d)
console.print(length(names), names[0])
console.print(length(paths))
console.print(paths[0] == path.join(d, "a.txt"))
`)
	want := "1 a.txt\n1\ntrue\n"
	if got != want || code != 0 {
		t.Fatalf("目录那两条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFileReadFromChunks 验证分块读取的两条规则：位置按字节计，结尾不拆分半个码点。
// "甲乙丙"每个字符三个字节，所以从第 1 字节起取两字节正好落在两个续字节上——返回空串。
func TestFileReadFromChunks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "three.txt")
	if err := os.WriteFile(p, []byte("甲乙丙"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let p:string = "`+pp+`"
console.print(file.read_from(p, 0, 3) == "甲")
console.print(length(file.read_from(p, 1, 2)))
console.print(file.read_from(p, 3, 3) == "乙")
console.print(length(file.read_from(p, 0, 4)))
console.print(length(file.read_from(p, 999, 10)))
console.print(length(file.read_from(p, 0, 0)))
`)
	want := "true\n0\ntrue\n1\n0\n0\n"
	if got != want || code != 0 {
		t.Fatalf("分块读不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFileWriteAtMiddle 验证"覆盖中间那一段"：六字节的文件从第 3 字节起覆盖两个字节，
// 前后内容都保留，文件不被截短；打开一个不存在的文件去写，是 10042 号那一句。
func TestFileWriteAtMiddle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "abcdef.txt")
	if err := os.WriteFile(p, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let p:string = "`+pp+`"
file.write_at(p, 3, "XY")
console.print(file.read(p), file.size(p))
`)
	if got != "abcXYf 6\n" || code != 0 {
		t.Fatalf("盖中间那一段不对：\n got %q 码 %d\nwant %q", got, code, "abcXYf 6\n")
	}
	got, code = run(t, `file.write_at("`+
		filepath.ToSlash(filepath.Join(dir, "没有这个文件.txt"))+`", 0, "z")`)
	if code != 1 || !strings.Contains(got, "10042）：写不进去这个文件") {
		t.Fatalf("文件不在该报 10042：%q 码 %d", got, code)
	}
}

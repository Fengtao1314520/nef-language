// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dirs_more_e2e_test.go 覆盖两条目录与复制相关的成员：
// file.copy 整份复制（目标存在则覆盖，与 file.write 处理一致），
// file.create_directories 把缺失的几层一次建齐（只用 kernel32 的 CreateDirectoryW 一层一层建）。
// 两条都在临时目录里真实创建和删除。

// TestFileCopyOverwrites 复制一份、再复制一份覆盖；错误码分两头：源不存在是 10041，目标是目录是 10042。
func TestFileCopyOverwrites(t *testing.T) {
	dir := t.TempDir()
	d := filepath.ToSlash(dir)
	src := d + "/源.txt"
	dst := d + "/标.txt"
	got, code := run(t, `
let src:string = "`+src+`"
let dst:string = "`+dst+`"
file.write(src, "一")
file.copy(src, dst)
console.print(file.read(dst), file.size(dst))
file.write(src, "二二三")
file.copy(src, dst)
console.print(file.read(dst), file.size(dst))
console.print(file.exists(src), file.exists(dst))
`)
	want := "一 3\n二二三 9\ntrue true\n"
	if got != want || code != 0 {
		t.Fatalf("复制那一条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	got, code = run(t, `file.copy("`+d+`/没有这个文件.txt", "`+dst+`")`)
	if code != 1 || !strings.Contains(got, "10041）：读不到这个文件") {
		t.Fatalf("源不在得到 %q 码 %d，要 10041 那一句", got, code)
	}
	// 目标是一个目录：CopyFileW 不接受这种输入，报"写不进去"
	got, code = run(t, `file.copy("`+src+`", "`+d+`")`)
	if code != 1 || !strings.Contains(got, "10042）：写不进去这个文件") {
		t.Fatalf("目标是目录得到 %q 码 %d，要 10042 那一句", got, code)
	}
	// 复制按字节进行：二进制内容复制后不得改变
	bin := d + "/二进制.bin"
	if err := os.WriteFile(bin, []byte{0xFF, 0xFE, 0x00, 0x41}, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code = run(t, `
let a:string = "`+bin+`"
let b:string = "`+d+`/副本.bin"
file.copy(a, b)
let left:bytes = file.read_bytes(a)
let right:bytes = file.read_bytes(b)
console.print(length(left), length(right))
let bad:int = 0
for (i:int in 0 until length(left)) {
    if (left[i] != right[i]) {
        bad = bad + 1
    }
}
console.print(bad)
`)
	if got != "4 4\n0\n" || code != 0 {
		t.Fatalf("按字节复制变了样：got %q 码 %d", got, code)
	}
}

// TestFileCreateDirectories 一次建齐三层、已经齐了再调用一次、上一段是文件时返回 false。
// 与单层版本的分别是判定语义：那一条判定"有没有新建成一层"（已存在算 false），
// 这一条判定"这条路径上的目录齐了没有"（本来就齐的算 true）。
func TestFileCreateDirectories(t *testing.T) {
	dir := t.TempDir()
	d := filepath.ToSlash(dir)
	deep := d + "/甲/乙/丙"
	got, code := run(t, `
let deep:string = "`+deep+`"
console.print(file.create_directories(deep))
console.print(file.is_directory(deep))
console.print(file.create_directories(deep))
let p:string = path.join(deep, "写这里.txt")
file.write(p, "内容")
console.print(file.read(p))
console.print(file.create_directories(p + "/下面"))
console.print(file.create_directories("`+d+`/丁"))
`)
	want := "true\ntrue\ntrue\n内容\nfalse\ntrue\n"
	if got != want || code != 0 {
		t.Fatalf("一次建齐那几条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if _, err := os.Stat(filepath.FromSlash(deep)); err != nil {
		t.Fatalf("程序说建齐了，磁盘上却没有：%v", err)
	}

	// 只传最后一层名字时，它建的是一层（与单层版本一致：已经存在算 true，因为判定的是"齐没齐"）
	one := filepath.ToSlash(filepath.Join(dir, "单独一层"))
	got, code = run(t, `
let one:string = "`+one+`"
console.print(file.create_directories(one), file.create_directories(one))
console.print(file.is_directory(one))
`)
	if got != "true true\ntrue\n" || code != 0 {
		t.Fatalf("单层那一条走多层这条路得到 %q 码 %d", got, code)
	}

	// 路径长到无法转换成宽字符，抛 10051 那一句，与其余路径转换问题同一个号（不是"建不出来"）
	long := strings.Repeat("很长", 4000)
	got, code = run(t, `console.print(file.create_directories("`+long+`"))`)
	if code != 1 || !strings.Contains(got, "10051）：") {
		t.Fatalf("长得离谱的路径得到 %q 码 %d，要 10051 那一句", got, code)
	}
}

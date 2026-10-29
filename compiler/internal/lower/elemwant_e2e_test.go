// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// elemwant_e2e_test.go 覆盖字节元素期望类型落到后端的另一半：检查器把容器每一项认成 byte 之后，
// 槽位里存放、读回来、打印出的都应是同一个数——与 `file.read_bytes` 已跑通的路径结果完全一致。
func TestByteLiteralCells(t *testing.T) {
	got, code := run(t, `
let xs:list<byte> = [0, 255, 128]
console.print(length(xs), xs[0], xs[1], xs[2])
xs[1] = 7
console.print(xs[1])
let d:dict<string, byte> = {"a": 7, "b": 200}
console.print(d["a"], d["b"])
let s:hashset<byte> = {1, 2, 2}
console.print(s.contains(2), s.contains(3), length(s))
`)
	want := "3 0 255 128\n7\n7 200\ntrue false 2\n"
	if got != want || code != 0 {
		t.Fatalf("byte 字面量落进容器不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestByteLiteralSameAsReadBytes 把同一串字节走两条路：一条是 `list<byte>` 字面量（每槽 8 字节），
// 一条是 `file.read_bytes` 返回的那块 `bytes`（每槽 1 字节）。两条读回来的数必须
// 完全一致——不一致就是元素层识别的类型与运行期槽位对不上，或者窄槽位的读取方式读宽了。
func TestByteLiteralSameAsReadBytes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "three.bin")
	if err := os.WriteFile(p, []byte{0xFF, 0x00, 0x41}, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let from_file:bytes = file.read_bytes("`+pp+`")
let from_lit:list<byte> = [255, 0, 65]
console.print(from_file[0], from_file[1], from_file[2])
console.print(from_lit[0], from_lit[1], from_lit[2])
`)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("两条路都要跑得起来：got %q 码 %d", got, code)
	}
	if lines[0] != lines[1] {
		t.Fatalf("同一串字节两条路印得不一样：文件那条 %q，字面量那条 %q", lines[0], lines[1])
	}
	if lines[0] != "255 0 65" {
		t.Fatalf("字节数不对：%q", lines[0])
	}
}

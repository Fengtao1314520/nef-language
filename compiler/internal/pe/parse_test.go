// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package pe

import (
	"bytes"
	"debug/pe"
	"testing"
)

// TestParseWithDebugPe 用 Go 自带的 debug/pe 读一遍产物。
// 它是个真实现，头装错了它先不答应，比"跑不起来"三个字有用得多。
//
// 曾经栽过的坑：PE32+ 的 magic 是 0x20b，写成 0x10b（那是 32 位的）时
// 加载器只丢一句"不是有效的 Win32 应用程序"，debug/pe 则会报数据目录数目对不上。
func TestParseWithDebugPe(t *testing.T) {
	f, err := pe.NewFile(bytes.NewReader(helloExe(t)))
	if err != nil {
		t.Fatalf("debug/pe 不认这个产物：%v", err)
	}
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("可选头没被认成 PE32+")
	}
	if oh.Magic != 0x20b {
		t.Fatalf("magic 是 %x，要 0x20b", oh.Magic)
	}
	if len(f.Sections) != 2 {
		t.Fatalf("应该只有 .text 与 .data 两个段，实际 %d", len(f.Sections))
	}
	if d := oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]; d.VirtualAddress == 0 || d.Size != 40 {
		t.Fatalf("导入表目录没指对：%x:%x", d.VirtualAddress, d.Size)
	}
}

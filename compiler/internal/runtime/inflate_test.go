// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package runtime_test

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"neflang.org/nf/internal/x64"
)

// inflate_test.go 直接驱动 @http_inflate：手工构造一段调用它的代码，封成 PE 实际运行，
// 把解出的字节与 Go 标准库的解压结果逐字节比对。覆盖原样存放块、固定表、动态表、
// zlib 包裹与 raw deflate、匹配回拷、多块、各压缩档位。
//
// en: inflate_test.go drives @http_inflate directly: it hand-builds a snippet that calls the
// routine, packs it into a PE and actually runs it, then compares the unpacked bytes against
// Go's standard-library decompression byte for byte. It covers stored blocks, fixed tables,
// dynamic tables, the zlib wrapper and raw deflate, match copy-back, multiple blocks and the
// various compression levels.

// inflateProbe 喂一段压缩数据，期望解出 want。驱动把结果块逐字节打印成整数，
// 这里解析回来与 want 比对；返回 0（解不开）或内容不符都判失败。
//
// en: inflateProbe feeds one compressed payload, expecting want to come out. The driver prints
// the result block byte by byte as integers; this parses them back and compares against want.
// Returning 0 (cannot unpack) or any content mismatch is a failure.
func inflateProbe(t *testing.T, name string, compressed, want []byte) {
	d := newDrv(t)
	d.a.MovImm(x64.RCX, int64(len(compressed)))
	d.call("@bytes_new")
	d.save(0)
	for i, c := range compressed {
		d.a.MovLoad(x64.RSI, x64.RBP, -8)
		d.a.MovImm(x64.RDX, int64(c))
		d.a.MovStore8(x64.RSI, int32(8+i), x64.RDX)
	}
	d.a.MovLoad(x64.RCX, x64.RBP, -8)
	d.call("@http_inflate")
	d.save(1)
	d.a.MovLoad(x64.RAX, x64.RBP, -16)
	zero := d.label()
	end := d.label()
	done := d.label()
	d.a.Test(x64.RAX, x64.RAX)
	d.a.Jcc(x64.E, zero)
	d.a.MovLoad(x64.RCX, x64.RAX, 0)
	d.a.Mov(x64.RAX, x64.RCX)
	d.save(2)
	d.call("@put_int")
	d.nl()
	d.a.MovImm(x64.RAX, 0)
	d.save(3)
	loop := d.label()
	d.a.Mark(loop)
	d.a.MovLoad(x64.RAX, x64.RBP, -32)
	d.a.MovLoad(x64.RCX, x64.RBP, -24)
	d.a.Cmp(x64.RAX, x64.RCX)
	d.a.Jcc(x64.AE, end)
	d.a.MovLoad(x64.RSI, x64.RBP, -16)
	d.a.MovLoad(x64.RAX, x64.RBP, -32)
	d.a.Add(x64.RSI, x64.RAX)
	d.a.MovZX8(x64.RCX, x64.RSI, 8)
	d.call("@put_int")
	d.space()
	d.a.MovLoad(x64.RAX, x64.RBP, -32)
	d.a.AddImm(x64.RAX, 1)
	d.save(3)
	d.a.Jmp(loop)
	d.a.Mark(end)
	d.nl()
	d.a.Jmp(done)
	d.a.Mark(zero)
	d.a.MovImm(x64.RCX, -1)
	d.call("@put_int")
	d.nl()
	d.a.Mark(done)
	got, code := run(t, d.finish(t, 0))
	if code != 0 {
		t.Fatalf("[%s] 退出码 %d，输出 %q", name, code, got)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if lines[0] == "-1" {
		t.Fatalf("[%s] 解不开（返回 0）", name)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
	var bs []byte
	if n > 0 {
		for _, f := range strings.Fields(lines[1]) {
			v, _ := strconv.Atoi(f)
			bs = append(bs, byte(v))
		}
	}
	if !bytes.Equal(bs, want) {
		t.Fatalf("[%s] 解出的内容与 Go 不一致：得 %d 字节，要 %d 字节", name, len(bs), len(want))
	}
}

func rawDeflate(t *testing.T, s string) []byte {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.Bytes()
}

func zlibWrap(t *testing.T, s string) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.Bytes()
}

func TestInflateStored(t *testing.T) {
	inflateProbe(t, "stored", []byte{0x01, 0x03, 0x00, 0xFC, 0xFF, 0x41, 0x42, 0x43}, []byte("ABC"))
}

func TestInflateFixedEmpty(t *testing.T) {
	inflateProbe(t, "fixed-empty", []byte{0x03, 0x00}, []byte(""))
}

func TestInflateFixedA(t *testing.T) {
	inflateProbe(t, "fixed-A", []byte{0x73, 0x04, 0x00}, []byte("A"))
}

func TestInflateRaw(t *testing.T) {
	s := "hello hello hello world"
	inflateProbe(t, "raw", rawDeflate(t, s), []byte(s))
}

func TestInflateZlib(t *testing.T) {
	s := "deflate 的正文"
	inflateProbe(t, "zlib", zlibWrap(t, s), []byte(s))
}

func TestInflateRepeat(t *testing.T) {
	s := strings.Repeat("ab", 5000)
	inflateProbe(t, "repeat", rawDeflate(t, s), []byte(s))
}

func TestInflateMed(t *testing.T) {
	s := ""
	for i := 0; i < 40; i++ {
		s += fmt.Sprintf("line %d abcdefghij\n", i)
	}
	inflateProbe(t, "med", rawDeflate(t, s), []byte(s))
}

func TestInflateBig(t *testing.T) {
	s := ""
	for i := 0; i < 2000; i++ {
		s += fmt.Sprintf("line %d abcdefghij\n", i)
	}
	inflateProbe(t, "big", rawDeflate(t, s), []byte(s))
}

func TestInflateBigZlib(t *testing.T) {
	s := ""
	for i := 0; i < 2000; i++ {
		s += fmt.Sprintf("第 %d 行 中文内容甲乙丙丁戊\n", i)
	}
	inflateProbe(t, "big-zlib", zlibWrap(t, s), []byte(s))
}

func TestInflateLevels(t *testing.T) {
	s := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 300)
	for _, lvl := range []int{flate.BestSpeed, flate.BestCompression, flate.HuffmanOnly, flate.NoCompression} {
		var buf bytes.Buffer
		w, err := flate.NewWriter(&buf, lvl)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(s))
		_ = w.Close()
		inflateProbe(t, fmt.Sprintf("level%d", lvl), buf.Bytes(), []byte(s))
	}
}

func TestInflateMultiBlock(t *testing.T) {
	// 足够大且多样，逼出多个 deflate 块。
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "row %d: %x %x\n", i, i*2654435761, i*40503)
	}
	s := sb.String()
	inflateProbe(t, "multiblock", rawDeflate(t, s), []byte(s))
}

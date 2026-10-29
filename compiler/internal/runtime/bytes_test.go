// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package runtime_test

import (
	"testing"

	"neflang.org/nf/internal/runtime"
	"neflang.org/nf/internal/x64"
)

// bytes_test.go 是 `bytes` 那一档三条例程的驱动测试：
// @bytes_new、@bytes_at、@bytes_slice，再加换过面的那两条 @str_to_bytes 与 @bytes_str。
//
// 这里不经过降级与发射器：手工构造一段调用例程的入口，封成 PE 实际运行一遍——与文件那头的同一套走法。

// bytesNew 是 @bytes_new(rcx=字节数) 的小包装：返回的句柄存进第 n 个局部槽。
func (d *drv) bytesNew(n int64, on int) {
	d.a.MovImm(x64.RCX, n)
	d.call("@bytes_new")
	d.save(on)
}

// bytesLen 读那一档的头一个位置：`length(b)` 在中间码里就是这一个 Load，不另设例程。
func (d *drv) bytesLen(hn int) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovLoad(x64.RCX, x64.RAX, runtime.BytesLen)
	d.call("@put_int")
	d.nl()
}

// bytesAt 取那个位置的地址（@bytes_at），存进第 on 个槽——读与写都从这一个地址走。
func (d *drv) bytesAt(hn int, idx int64, on int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovImm(x64.RDX, idx)
	d.call("@bytes_at")
	d.a.MovStore(x64.RBP, int32(-8*(on+1)), x64.RAX)
}

// byteSet 往那个地址写一个字节；byteCell 读回一个字节（必须是 MovZX8：整份搬八个字节
// 会把后面那七个格一起读进来，那一档的格宽就是一个字节）。
func (d *drv) byteSet(on int, v int64) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(on+1)))
	d.a.MovImm(x64.RDX, v)
	d.a.MovStore8(x64.RAX, 0, x64.RDX)
}

func (d *drv) byteCell(on int) {
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(on+1)))
	d.a.MovZX8(x64.RCX, x64.RAX, 0)
	d.call("@put_int")
	d.nl()
}

// bytesSlice 是 @bytes_slice(rcx=句柄, rdx=起, r8=止)：返回的新句柄存进第 on 个槽。
func (d *drv) bytesSlice(hn int, from, to int64, on int) {
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(hn+1)))
	d.a.MovImm(x64.RDX, from)
	d.a.MovImm(x64.R8, to)
	d.call("@bytes_slice")
	d.a.MovStore(x64.RBP, int32(-8*(on+1)), x64.RAX)
}

// TestBytesNewAtSlice 钉那三条的基本形状：长度那个位置、按格读写、slice 含起不含止。
// 尺寸取 3 是为了把"@alloc 把数据区凑成 8 的倍数"那一笔也带上——凑出来的余量不许算进 length。
func TestBytesNewAtSlice(t *testing.T) {
	d := newDrv(t)
	d.bytesNew(3, 0)
	d.bytesLen(0) // 3
	d.bytesAt(0, 0, 1)
	d.byteSet(1, 255)
	d.bytesAt(0, 1, 1)
	d.byteSet(1, 0)
	d.bytesAt(0, 2, 1)
	d.byteSet(1, 65)
	for i := int64(0); i < 3; i++ {
		d.bytesAt(0, i, 1)
		d.byteCell(1)
	}
	d.bytesSlice(0, 1, 3, 2) // 含起不含止：两格
	d.bytesLen(2)
	d.bytesAt(2, 0, 1)
	d.byteCell(1)
	d.bytesAt(2, 1, 1)
	d.byteCell(1)
	d.bytesSlice(0, 2, 2, 2) // 起等于止：空的
	d.bytesLen(2)
	got, code := run(t, d.finish(t, 0))
	want := "3\n255\n0\n65\n2\n0\n65\n0\n"
	if got != want || code != 0 {
		t.Fatalf("那三条例程不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestBytesNewZeroAndNegative 钉两个边角：0 个字节造得出来（长度那个位置写 0），
// 负的那一个当场抛容器段的兜底 10030 号——绕回的大数会一路走到"要不到内存"那一句，
// 那句话说的是这台机器给不出这么多，不是"你写错了一个符号"。
func TestBytesNewZeroAndNegative(t *testing.T) {
	d := newDrv(t)
	d.bytesNew(0, 0)
	d.bytesLen(0)
	got, code := run(t, d.finish(t, 0))
	if got != "0\n" || code != 0 {
		t.Fatalf("空的块：got %q 码 %d，要 \"0\\n\" 码 0", got, code)
	}

	d2 := newDrv(t)
	d2.bytesNew(-1, 0)
	got, code = run(t, d2.finish(t, 0))
	if got != "出错了（10030）：bytes.new 要的字节数不能是负数\n" || code != 1 {
		t.Fatalf("负的那一头：got %q 码 %d", got, code)
	}
}

// TestBytesAtThrows 钉越界那一侧（10031 号，与容器同一条）：尽头那个位置与负下标都算越界。
func TestBytesAtThrows(t *testing.T) {
	for _, idx := range []int64{3, 99, -1} {
		d := newDrv(t)
		d.bytesNew(3, 0)
		d.bytesAt(0, idx, 1)
		got, code := run(t, d.finish(t, 0))
		if got != "出错了（10031）：下标越界\n" || code != 1 {
			t.Fatalf("下标 %d：out=%q code=%d", idx, got, code)
		}
	}
}

// TestBytesSliceThrows 钉 slice 的三条越界各抛 10031 号，边界判定与 @list_slice、@str_slice 同一套。
func TestBytesSliceThrows(t *testing.T) {
	cases := []struct{ from, to int64 }{{0, 4}, {-1, 2}, {2, 1}}
	for _, c := range cases {
		d := newDrv(t)
		d.bytesNew(3, 0)
		d.bytesSlice(0, c.from, c.to, 1)
		got, code := run(t, d.finish(t, 0))
		if got != "出错了（10031）：下标越界\n" || code != 1 {
			t.Fatalf("slice(%d, %d)：out=%q code=%d", c.from, c.to, got, code)
		}
	}
}

// TestBytesTextRoundTrip 钉换面之后那两条文本例程：@str_to_bytes 不再收类型字，
// 返回的是这一块；@bytes_str 反过来把这一块的正文当文本收，两趟过去内容一字不动。
func TestBytesTextRoundTrip(t *testing.T) {
	d := newDrv(t)
	src := d.addString("甲a")
	d.lea(src, x64.RCX)
	d.call("@str_to_bytes")
	d.save(0)
	d.bytesLen(0) // 4 个字节：甲是三个，a 是一个
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(0+1)))
	d.call("@bytes_str")
	d.a.Mov(x64.RCX, x64.RAX)
	d.call("@put_str")
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if got != "4\n甲a\n" || code != 0 {
		t.Fatalf("文本与那一档互换：got %q 码 %d", got, code)
	}

	// 空文本那一侧：正文一个位置都没有，两趟过去还是空文本
	d2 := newDrv(t)
	empty := d2.addString("")
	d2.lea(empty, x64.RCX)
	d2.call("@str_to_bytes")
	d2.save(0)
	d2.bytesLen(0)
	d2.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(0+1)))
	d2.call("@bytes_str")
	d2.a.Mov(x64.RCX, x64.RAX)
	d2.call("@put_str")
	d2.nl()
	got, code = run(t, d2.finish(t, 0))
	if got != "0\n\n" || code != 0 {
		t.Fatalf("空的那一头：got %q 码 %d", got, code)
	}
}

// TestBytesToStrRejectsBadUTF8 钉 10044 那一条：验字节那一步走在造文本之前，
// 所以抛出去的时候一块都没造，不留没人认领的东西（从前那一版要先还掉刚填好的那一块）。
func TestBytesToStrRejectsBadUTF8(t *testing.T) {
	d := newDrv(t)
	d.bytesNew(1, 0)
	d.bytesAt(0, 0, 1)
	d.byteSet(1, 255)
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(0+1)))
	d.call("@bytes_str")
	got, code := run(t, d.finish(t, 0))
	if got != "出错了（10044）：文件不是合法的 UTF-8\n" || code != 1 {
		t.Fatalf("非法字节那一头：got %q 码 %d", got, code)
	}
}

// TestBytesHeapTagIsItsOwn 钉那一档头上带的是自己的类型字（tagBytes=10）：
// 块还回空闲链时要挂进链；头上的号与其他块相同不会立刻出问题，但 `is`、打印机与析构都靠它区分类型。
func TestBytesHeapTagIsItsOwn(t *testing.T) {
	d := newDrv(t)
	d.bytesNew(16, 0)
	d.a.MovLoad(x64.RAX, x64.RBP, int32(-8*(0+1)))
	d.a.MovZX32(x64.RCX, x64.RAX, -8) // 块头低四个字节：类型字
	d.call("@put_int")
	d.nl()
	got, code := run(t, d.finish(t, 0))
	if got != "10\n" || code != 0 {
		t.Fatalf("那一档的类型字：got %q 码 %d，要 10（lower 那头 tagBytes 同一个号）", got, code)
	}
}

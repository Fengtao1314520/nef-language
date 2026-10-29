// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// heap_stress_test.go 钉的是"每一块只管自己那段"这条最底线的账。
//
// 这条测试覆盖的是：dict 长出第二块桶数组曾引发一次崩溃，读三段汇编读不出来——
// `@alloc` 从空闲链表取块时只看档、不看块自己头里记的尺寸，而档是按 2 的
// 幂上界分的，同一档里挂着比这一笔小的块（数据 128 与数据 192 同在上界 256 那一档），于是
// 128 那一块被交给要 192 的桶数组，尾巴 64 字节写在隔壁块上。**任何一段输出都没变**，
// 一直到再散列去读那个被越界写过的位置才段错误。
//
// 所以这一条测试不问"印出来的对不对"，只问一件事：**每一块自己的字节有没有被动过**。
// 做法是随机尺寸（专挑同一档里凑得出大小一对的那些）反复要块、整块写入只属于自己的标记、
// 随机还掉一批、再要、再从头验一遍所有还在手上的块。
//
// 随机是定死的种子：同一份文件在任何机器上生成同一串指令，失败就能复现。
//
// 反向验证过：把 `internal/runtime/runtime.go` 的 `alloc` 里"偏小就挂回档头"那一判拆掉
// （`a.Jcc(x64.AE, fit)` 改成 `a.Jmp(fit)`），这一条立即失败，报出对不上的字数。

package runtime_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"neflang.org/nf/internal/x64"
)

// stressSizes 里每一对**同档**的大小都是精心挑的：还进链的那一块会正好卡在下一笔下面。
//
// 低段（总尺寸 ≤512）是**一档一种尺寸**，里头不存在"同档两大一小"，那种形状只能在高段造
// ——所以成对的那几组全在 512 以上
// （600／1000 同在上界 1024，1100／1900 同在 2048，2100／3000 与 3500／4000 同在 4096）。
// 低段那些数照旧留着，测的是精确档本身（16 与 24 各归各档，互不占用）与切块的清零规则。
var stressSizes = []int64{8, 16, 24, 60, 100, 128, 192, 264, 384, 500, 600, 1000, 1100, 1900, 2100, 3000, 3500, 4000}

// 帧里的第 30 格数"对不上的字"，第 31 格数"验过的字"（24 格给在手的块，留几格余量）。
const (
	slotLive  = 24 // 在手的块最多占 0..23 这二十四格
	slotCheck = 30
	slotBad   = 31
)

func TestHeapStressOverwrite(t *testing.T) {
	rnd := rand.New(rand.NewSource(20261003))
	d := newDrv(t)
	// 两个计数器先清零（写在帧槽里，之后每块都靠编号取回自己的地址）
	d.a.MovImm(x64.RAX, 0)
	d.a.MovStore(x64.RBP, int32(-8*(slotCheck+1)), x64.RAX)
	d.a.MovStore(x64.RBP, int32(-8*(slotBad+1)), x64.RAX)

	type hold struct {
		id    int // 全局编号，标记按它算，两块不可能撞出同一份内容
		words int // 这一块验几个字
	}
	var inHand = make([]*hold, slotLive) // 下标就是帧槽号，nil 表示那个位置空着
	blocks := 0

	pattern := func(h *hold, j int) int64 {
		return int64(uint64(h.id)*0x10000 + uint64(j))
	}

	// fill 往这一块写满只属于它的标记：每格是"块号 × 65536 ＋ 第几格"。
	fill := func(slot int, h *hold) {
		d.a.MovLoad(x64.RSI, x64.RBP, int32(-8*(slot+1)))
		for j := 0; j < h.words; j++ {
			d.a.MovImm(x64.RAX, pattern(h, j))
			d.a.MovStore(x64.RSI, int32(8*j), x64.RAX)
		}
	}

	// check 把这一块再读一遍，对不上就往 slotBad 那个位置加一笔。
	check := func(slot int, h *hold) {
		d.a.MovLoad(x64.RSI, x64.RBP, int32(-8*(slot+1)))
		for j := 0; j < h.words; j++ {
			d.a.MovLoad(x64.RAX, x64.RSI, int32(8*j))
			d.a.MovImm(x64.RDX, pattern(h, j))
			d.a.Cmp(x64.RAX, x64.RDX)
			good := d.label()
			d.a.Jcc(x64.E, good)
			d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(slotBad+1)))
			d.a.AddImm(x64.RCX, 1)
			d.a.MovStore(x64.RBP, int32(-8*(slotBad+1)), x64.RCX)
			d.a.Mark(good)
			d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(slotCheck+1)))
			d.a.AddImm(x64.RCX, 1)
			d.a.MovStore(x64.RBP, int32(-8*(slotCheck+1)), x64.RCX)
		}
	}

	// grab 要一块新的、填上标记；放不进空位就先用着同一个槽（旧的已经还掉了）
	grab := func() {
		size := stressSizes[rnd.Intn(len(stressSizes))]
		h := &hold{id: blocks, words: int(size / 8)}
		if h.words == 0 {
			h.words = 1
		}
		blocks++
		slot := -1
		for i := 0; i < slotLive; i++ {
			if inHand[i] == nil {
				slot = i
				break
			}
		}
		if slot < 0 {
			t.Fatalf("帧槽不够用了：%d 块在手", slotLive)
		}
		d.alloc(size, int64(0x5000+blocks))
		d.save(slot)
		inHand[slot] = h
		fill(slot, h)
	}

	const waves = 5
	checked := 0
	for w := 0; w < waves; w++ {
		// 一轮：先补满在手的那几个槽
		for free := 0; free < slotLive; free++ {
			if inHand[free] == nil {
				grab()
			}
		}
		// 再从头验一遍所有在手的（这一块写到那一块，越界的写会在这里露出来）
		for i, h := range inHand {
			if h != nil {
				check(i, h)
				checked += h.words
			}
		}
		// 随机还掉三四成，把块放回空闲链表，下一轮才有"同档偏小"的料
		for i, h := range inHand {
			if h != nil && rnd.Intn(100) < 35 {
				d.free(i)
				inHand[i] = nil
				_ = h
			}
		}
	}

	// 收尾再全验一遍，然后把"验过的字数"与"对不上的笔数"打印出来
	for i, h := range inHand {
		if h != nil {
			check(i, h)
			checked += h.words
		}
	}
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(slotCheck+1)))
	d.call("@put_int")
	d.space()
	d.a.MovLoad(x64.RCX, x64.RBP, int32(-8*(slotBad+1)))
	d.call("@put_int")
	d.nl()

	got, code := run(t, d.finish(t, 0))
	want := fmt.Sprintf("%d 0\n", checked)
	if got != want || code != 0 {
		t.Fatalf("堆上的块互相覆盖了：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 这一趟验了多少字要留个可见的实测数；改动测试之后这个数要重新跑一遍确认
	t.Logf("这一趟验了 %d 个字，对不上 0（块 %d 笔）", checked, blocks)
	// 自己也要证明这条测试不是空跑的：验过的字数得够多
	if checked < 3000 {
		t.Fatalf("这一趟只验了 %d 个字，量不够", checked)
	}
	if !strings.HasSuffix(got, " 0\n") {
		t.Fatalf("对不上的笔数不是 0：%q", got)
	}
}

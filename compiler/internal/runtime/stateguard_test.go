// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// 这一份是包内测试：要查的那张清单与静态那一份数据两头都没交出去。
//
// en: This file tests from inside the package: the cell list and the static data are both
// unexported.

package runtime

import "testing"

// TestStateCellsAreNotStatic 钉两头不重叠：清单里排着的名字，静态数据段里不能再有同一个名字，
// 而且每一格的大小是正数。
//
// 读写一格的三条助手按名字决定走哪一块：在清单里就走这一条线程的状态块，不在就走静态段。
// 同一个名字两头都排着的时候，代码读到块里、发射器回填的却是静态那一个位置，两处都能编过、
// 也都能跑出个结果，只有第二条线程上来才露。反过来一个名字两头都没有，回填那一步当场报出它
// （internal/pe），所以那一种错藏不住。
//
// en: TestStateCellsAreNotStatic pins that the two halves do not overlap: a name in the list
// must not also be a static cell, and every size must be positive.
//
// The three helpers pick a route by name: in the list means this thread's block, otherwise the
// static segment. With a name in both, the code reads the block while the emit step backfills
// the static position - both compile, both print something, and only a second thread shows the
// split. A name in neither cannot hide: the backfill step names it on the spot (internal/pe).
func TestStateCellsAreNotStatic(t *testing.T) {
	_, at := dataBytes()
	for _, c := range stateCells {
		if _, dup := at[c.name]; dup {
			t.Errorf("%s 两头都有：静态那一个位置还在，块里 %d 又排了一份", c.name, stateBlock.at[c.name])
		}
		if c.size <= 0 {
			t.Errorf("%s 的大小不是正数：%d", c.name, c.size)
		}
	}
}

// TestStateBlockBytes 钉这一块加起来的字节数。每一条线程一进门就要这一块，所以它的大小是
// "起一条线程要多花多少"的一部分；排进清单的每一格都在这里算数。这个数变了就是有人往每一条
// 线程的份里加了东西，要连同讲内存布局的那一份文档一起改。
//
// 2026-11-08 段 5 加三格（默认处理的落点、释放的重入护栏、这一条线程自己的错误号），
// 252,200 变 252,224；同日再加一格 `selfEntry`（栈用完那一路要认自己走的是哪一条线程），
// 252,224 变 252,232。2026-11-10 HTTP 那一组加两个（应答正文的字节上限、本层自己那四个号要抛
// 的那一句）：上限由打开请求那一步写下、由读正文那一步读，那一句要先落格是因为关句柄踩过寄存器。
// 2026-11-12 HTTP 再加一格 `httpDeflate`（应答是不是 deflate 压缩）：读正文那一步开头查
// Content-Encoding 写下、收尾据此决定要不要自己解，252,248 变 252,256。
//
// en: TestStateBlockBytes pins how large the block adds up to. Every thread asks for this block
// as it starts, so its size is part of what a thread costs, and every cell in the list counts
// here. A changed number means someone added to each thread's share, and the document about the
// memory layout has to move with it. Segment 5 added three cells - the default handler's landing
// point, the release re-entry mark and this thread's own error number - taking 252,200 to 252,224;
// the same day added selfEntry, the cell the exhausted-stack path reads to tell which thread it is
// on, taking 252,224 to 252,232. On 2026-11-10 the HTTP group added two more, taking 252,232 to
// 252,248: the byte cap on the response body, written where the request is opened and read where
// the body is read, and the sentence this layer raises itself, which needs a cell because closing
// handles clobbers the registers. On 2026-11-12 HTTP added httpDeflate, whether the response is
// deflate-compressed, written where the body read starts by querying Content-Encoding and read at
// the end to decide whether to unpack it itself, taking 252,248 to 252,256.
func TestStateBlockBytes(t *testing.T) {
	const want = 252256
	if got := StateBytes(); got != want {
		t.Errorf("每一条线程的状态块现在是 %d 字节，先前钉的是 %d", got, want)
	}
}

// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//
// The Nef Output Exception (see the LICENSE-EXCEPTION file at the repository
// root) applies to this file: code you write and the binaries you compile with
// it are not subject to the Apache License, and you have no obligation to
// open-source them.

// inflate.go 实现 @http_inflate：把 deflate 压缩的应答正文解开（RFC 1951）。
//
// 为什么要有这一条：winhttp 的自动解压只认不带包裹的 raw deflate，而网上多数服务器
// 给的是 zlib 包裹的那一种（RFC 1950，两个字节的头、四个字节的校验尾）。开着 deflate
// 那一位去读，读到一半系统就报 0x80004004。所以 http.go 那一侧只开 gzip 一位，
// deflate 应答原样收下来，由这一条自己解——两种包裹都认：开头两个字节符合 zlib
// 的头就跳过它，不符合就当 raw deflate 从头解。gzip 不经过这里：系统已经解开了。
//
// 解法是标准的三段：块头（最后一块标志加两比特类型）、 Huffman 表（固定表直接填，
// 动态表先解码长再建表）、符号流（字面量直接输出，长度加距离那一组往回拷）。
// 码表用"按长度计数"的那一种建法与读法：count[长度] 记该长度有几个码，symbol 按
// 长度排好，读的时候一位一位攒，攒到落在哪个长度的区间里就取对应的符号。
// 回拷的窗口就是输出缓冲本身：距离最远 32768，输出一直留着，往回找即可。
//
// 入参 rcx＝压缩的 bytes 块，返回 rax＝解开的新 bytes 块；解不开返回 0，
// 由调用方（@http_body）关句柄再抛 10110。本条自己分配的工作块与输出缓冲
// 在返回前（无论成败）都释放；入参那一块归调用方，这里只读。
//
// 状态放在一块堆上的工作区里（帧槽只有三个：入参块、工作区、结果块），子程序
// （取比特、读符号、建表、扩容、输出一个字节）之间靠工作区传值。取比特那一条
// 不碰 rsi／rdi，读符号那一条因此能把两张表的地址一直拿在这两个寄存器里。
// 失败那一路先把 rsp 拨回帧底——子程序是 call 进去的，栈上还留着返回地址，
// 不拨回去 epilogue 那一条 ret 就取错了位置。
//
// en: inflate.go implements @http_inflate: it unpacks a deflate-compressed response body
// (RFC 1951). Why this routine exists: winhttp's automatic decompression only recognises raw
// deflate without a wrapper, while most servers on the web send the zlib-wrapped kind (RFC
// 1950, a two-byte header and a four-byte checksum tail). Reading with the deflate flag on
// aborts mid-stream with 0x80004004. So http.go enables only the gzip flag, deflate responses
// arrive untouched, and this routine unpacks them - recognising both wrappers: if the first
// two bytes form a valid zlib header they are skipped, otherwise the stream is read as raw
// deflate from the start. gzip never comes through here: the system has already unpacked it.
// The method is the standard three parts: block header (last-block flag plus a two-bit type),
// Huffman tables (the fixed ones are filled in directly, the dynamic ones decode code lengths
// first and then build), and the symbol stream (literals go straight out, a length plus
// distance pair copies backwards). The tables use the count-by-length construction and
// reading: count[length] records how many codes have that length, symbol is sorted by length,
// and reading accumulates one bit at a time until the value falls in a length's range, then
// takes the matching symbol. The copy-back window is the output buffer itself: distances reach
// at most 32768 and the output is kept whole, so looking back suffices. Argument rcx = the
// compressed bytes block, return rax = a new unpacked bytes block; on failure it returns 0 and
// the caller (@http_body) closes the handles and throws 10110. The workspace and output buffer
// this routine allocates are freed before returning, on success and failure alike; the input
// block belongs to the caller and is only read. State lives in one heap workspace (only three
// frame slots: input block, workspace, result block); the subroutines (take bits, read symbol,
// build table, grow, emit one byte) pass values through the workspace. The bits subroutine
// leaves rsi/rdi alone, so the symbol reader can keep both table addresses in those two
// registers throughout. The failure path first resets rsp to the frame floor - subroutines
// were entered with call and their return addresses still sit on the stack, and without the
// reset the epilogue's ret would take the wrong one.
package runtime

import "neflang.org/nf/internal/x64"

// inflate 工作区里各格的偏移。前段是输入输出与比特缓冲，中段是四张码表
// （字面量／距离／码长各一套 count+symbol，外加动态块那一条长度数组与建表用的
// offs），后段是子程序之间传值的临时格。全部八字节一格，符号与计数也按八字节
// 存——多占几 KB 换来每一处读写都是同一条 MovLoad／MovStore。
//
// en: Offsets of the cells in the inflate workspace. The front holds input, output and the
// bit buffer; the middle holds the four code tables (count+symbol each for literal, distance
// and code-length, plus the dynamic block's length array and the offs used while building);
// the tail holds the scratch cells the subroutines pass values through. Every cell is eight
// bytes - symbols and counts too - a few KB extra buys every access being the same
// MovLoad/MovStore.
const (
	wIn     = 0    // 压缩字节的起点（入参块 + BytesBody）
	wInLen  = 8    // 压缩字节数
	wInPos  = 16   // 下一个要读的字节
	wBitBuf = 24   // 比特缓冲（先读进来的字节放高位）
	wBitCnt = 32   // 缓冲里现有的比特数（任何一次取用之后小于 8）
	wOut    = 40   // 输出缓冲
	wOutCap = 48   // 它的容量
	wOutPos = 56   // 已输出的字节数
	wLitCnt = 64   // 字面量表 count[16]
	wLitSym = 192  // 字面量表 symbol[288]
	wDstCnt = 2496 // 距离表 count[16]
	wDstSym = 2624 // 距离表 symbol[30]
	wLens   = 2864 // 动态块的码长数组 [318]（288 字面量 + 30 距离）
	wClCnt  = 5408 // 码长表 count[16]
	wClSym  = 5536 // 码长表 symbol[19]
	wOffs   = 5688 // 建表用的 offs[17]
	wCode   = 5824 // 读符号：攒起来的码
	wFirst  = 5832 // 读符号：该长度区间的第一个码
	wIndex  = 5840 // 读符号：symbol 里的游标
	wLen    = 5848 // 读符号：当前长度
	wLensP  = 5856 // 建表入参：码长数组
	wN      = 5864 // 建表入参：符号个数
	wCntP   = 5872 // 建表入参：count
	wSymP   = 5880 // 建表入参：symbol
	wLeft   = 5904 // 建表：完整性核对剩下的额度
	wI      = 5912 // 通用游标
	wTmp    = 5928 // 临时格
	wTmp2   = 5936
	wTmp3   = 5944
	wLast   = 5952 // 块头：最后一块标志
	wType   = 5960 // 块头：类型
	wHlit   = 5968 // 动态块：字面量符号个数
	wHdist  = 5976 // 动态块：距离符号个数
	wHclen  = 5984 // 动态块：码长符号个数
	wTotal  = 5992 // 动态块：hlit + hdist
	wSym    = 6000 // 当前解出的符号
	wMLen   = 6008 // 匹配的长度（stored 块借它放 LEN）
	wDist   = 6016 // 匹配的距离
	wRep    = 6024 // 码长重复次数
	wPrev   = 6032 // 重复 16 那一种要抄的前一个码长
	wBitsN  = 6040 // 取比特入参：要几个比特
	wSize   = 6048 // 工作区总大小
)

// inflate 的三张静态表加一条顺序表：长度码的基数与额外比特数、距离码的基数与
// 额外比特数、动态块码长的读取顺序。数值是 RFC 1951 第 3.2.5 节写死的那几张。
//
// en: inflate's three static tables plus one order table: the length codes' bases and extra
// bit counts, the distance codes' bases and extra bit counts, and the reading order of the
// dynamic block's code lengths. The numbers are the ones fixed in RFC 1951 section 3.2.5.
var (
	infLenBase  = []uint16{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258}
	infLenExtra = []byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
	infDstBase  = []uint16{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}
	infDstExtra = []byte{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
	infClOrder  = []byte{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}
)

// inflateData 把上面那五段写进数据段：基数两字节一格（小端），额外比特数与
// 顺序表一字节一格。
//
// en: inflateData writes the five arrays above into the data section: bases two bytes per
// cell (little-endian), extra bit counts and the order table one byte per cell.
func inflateData(put func(name string, bs []byte)) {
	u16 := func(vs []uint16) []byte {
		bs := make([]byte, 0, 2*len(vs))
		for _, v := range vs {
			bs = append(bs, byte(v), byte(v>>8))
		}
		return bs
	}
	put("infLenBase", u16(infLenBase))
	put("infLenExtra", infLenExtra)
	put("infDistBase", u16(infDstBase))
	put("infDistExtra", infDstExtra)
	put("infClOrder", infClOrder)
}

// httpInflate 发射 @http_inflate。整体形状：三个帧槽（入参块、工作区、结果块），
// 其余状态全在工作区；五个子程序各是一段标号，call 进去 ret 出来，失败一律跳到
// lFail 那一条共同收尾。
//
// en: httpInflate emits @http_inflate. Overall shape: three frame slots (input block,
// workspace, result block), all other state in the workspace; five subroutines are each a
// labelled block, entered with call and left with ret, and every failure jumps to the one
// shared tail at lFail.
func httpInflate(b *rt) {
	const (
		fIn  = 0 // 入参：压缩的 bytes 块
		fWs  = 1 // 工作区
		fOut = 2 // 结果块
		fRbx = 3 // 调用方原有的 rbx：本条借 rbx 常驻工作区指针，收尾时归还
	)
	a := b.a

	// 工作区指针常驻 rbx：wl／ws 直接以它作基址，不再经 rax 取地址，
	// 因此读／写工作区不会踩掉 rax 里正拿着的值。rbx 是被调方保存的那一组，
	// @alloc／@free／@bytes_new 与系统调用都不动它；进门先把调用方原有的 rbx
	// 落进帧槽，两条收尾（成功与失败）各自归还。
	wl := func(reg int, off int32) { a.MovLoad(reg, x64.RBX, off) }
	ws := func(off int32, reg int) { a.MovStore(x64.RBX, off, reg) }
	wl2 := func(r1 int, o1 int32, r2 int, o2 int32) {
		wl(r1, o1)
		wl(r2, o2)
	}
	wzero := func(off int32) {
		a.Xor(x64.RAX, x64.RAX)
		ws(off, x64.RAX)
	}

	lBits := b.label()      // rcx＝要几个比特 → rax＝值；不碰 rsi／rdi
	lDecode := b.label()    // rcx＝count、rdx＝symbol → rax＝符号
	lConstruct := b.label() // rcx＝码长、rdx＝个数、r8＝count、r9＝symbol
	lReserve := b.label()   // rcx＝需要的容量：不够就翻倍搬一次
	lOutByte := b.label()   // rcx＝一个字节：追加到输出
	lFail := b.label()

	b.prologue()
	b.put(fRbx, x64.RBX)
	b.put(fIn, x64.RCX)
	a.MovImm(x64.RCX, wSize)
	a.Xor(x64.RDX, x64.RDX)
	b.call("@alloc")
	b.put(fWs, x64.RAX)
	a.Mov(x64.RBX, x64.RAX) // 工作区指针常驻 rbx
	wzero(wOut)             // 失败收尾按它是不是 0 决定要不要释放

	// 输入：块头之后那一段字节。
	b.get(x64.RAX, fIn)
	a.LeaMem(x64.RDX, x64.RAX, BytesBody)
	ws(wIn, x64.RDX)
	b.get(x64.RAX, fIn)
	a.MovLoad(x64.RAX, x64.RAX, BytesLen)
	ws(wInLen, x64.RAX)
	wzero(wInPos)
	wzero(wBitBuf)
	wzero(wBitCnt)
	wzero(wOutPos)

	// zlib 包裹的识别：第一个字节低四位是 8（deflate 方法），两个字节合起来的
	// 十六位数能被 31 整除（RFC 1950 的核对式）。中了就跳过那两个字节；
	// FLG 的第 5 位若还带着字典标志，再跳四个字节。不符合就当 raw deflate。
	hdrDone := b.label()
	posSet := b.label()
	wl(x64.RAX, wInLen)
	a.CmpImm(x64.RAX, 2)
	a.Jcc(x64.B, hdrDone)
	wl(x64.RDI, wIn)
	a.MovZX8(x64.RCX, x64.RDI, 0)
	a.MovZX8(x64.RDX, x64.RDI, 1)
	a.Mov(x64.RAX, x64.RCX)
	a.AndImm(x64.RAX, 15)
	a.CmpImm(x64.RAX, 8)
	a.Jcc(x64.NE, hdrDone)
	a.ShlImm(x64.RCX, 8)
	a.Or(x64.RCX, x64.RDX)
	a.Mov(x64.RAX, x64.RCX)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.R8, 31)
	a.Div1(x64.R8)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, hdrDone)
	wl(x64.RDI, wIn)
	a.MovZX8(x64.RDX, x64.RDI, 1)
	a.MovImm(x64.RAX, 2)
	a.AndImm(x64.RDX, 0x20)
	a.Jcc(x64.E, posSet)
	a.MovImm(x64.RAX, 6)
	a.Mark(posSet)
	ws(wInPos, x64.RAX)
	a.Mark(hdrDone)

	// 输出缓冲先申请 8192 字节，不够再翻倍（与 @http_body 同一条路）。
	a.MovImm(x64.RCX, httpFirst)
	a.Xor(x64.RDX, x64.RDX)
	b.call("@alloc")
	ws(wOut, x64.RAX)
	a.MovImm(x64.RAX, httpFirst)
	ws(wOutCap, x64.RAX)

	blockTop := b.label()
	stored := b.label()
	fixed := b.label()
	dynamic := b.label()
	blockEnd := b.label()

	// 块头：一个比特"是不是最后一块"，两个比特类型。
	a.Mark(blockTop)
	a.MovImm(x64.RCX, 1)
	a.Call(lBits)
	ws(wLast, x64.RAX)
	a.MovImm(x64.RCX, 2)
	a.Call(lBits)
	ws(wType, x64.RAX)
	wl(x64.RAX, wType)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, stored)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.E, fixed)
	a.CmpImm(x64.RAX, 2)
	a.Jcc(x64.E, dynamic)
	a.Jmp(lFail) // 类型 3 没有定义

	// 类型 0：原样存放。比特缓冲清零即对齐到字节边界（取比特那一条保证
	// 任何一次取用之后缓冲里不足八个比特，清掉的就是当前字节剩下的那几位），
	// 随后 LEN 与 NLEN 两个十六位数互为反码，再逐字节收下 LEN 个。
	storedLoop := b.label()
	a.Mark(stored)
	wzero(wBitBuf)
	wzero(wBitCnt)
	a.MovImm(x64.RCX, 16)
	a.Call(lBits)
	ws(wMLen, x64.RAX)
	a.MovImm(x64.RCX, 16)
	a.Call(lBits)
	ws(wTmp, x64.RAX)
	wl2(x64.RAX, wMLen, x64.RCX, wTmp)
	a.Xor(x64.RAX, x64.RCX)
	a.MovImm(x64.RCX, 0xFFFF)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.NE, lFail)
	a.Mark(storedLoop)
	wl(x64.RAX, wMLen)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, blockEnd)
	a.MovImm(x64.RCX, 8)
	a.Call(lBits)
	a.Mov(x64.RCX, x64.RAX)
	a.Call(lOutByte)
	wl(x64.RAX, wMLen)
	a.AddImm(x64.RAX, -1)
	ws(wMLen, x64.RAX)
	a.Jmp(storedLoop)

	// 类型 1：固定表。码长是写死的四段，填进长度数组再按动态表同一条路建表。
	fill := func(from, to, val int) {
		top := b.label()
		b.get(x64.RDI, fWs)
		a.AddImm(x64.RDI, wLens+int32(from*8))
		a.MovImm(x64.RCX, int64(to-from))
		a.MovImm(x64.RAX, int64(val))
		a.Mark(top)
		a.MovStore(x64.RDI, 0, x64.RAX)
		a.AddImm(x64.RDI, 8)
		a.AddImm(x64.RCX, -1)
		a.Test(x64.RCX, x64.RCX)
		a.Jcc(x64.NE, top)
	}
	constructBoth := b.label()
	constructDist := b.label()
	a.Mark(fixed)
	fill(0, 144, 8)
	fill(144, 256, 9)
	fill(256, 280, 7)
	fill(280, 288, 8)
	fill(288, 318, 5)
	// 固定表的符号个数是写死的 288 与 30：落进动态块那两格，
	// 好与动态块共用下面那一段建表（字面量与距离两张都建）。
	a.MovImm(x64.RAX, 288)
	ws(wHlit, x64.RAX)
	a.MovImm(x64.RAX, 30)
	ws(wHdist, x64.RAX)
	a.Jmp(constructBoth)

	// 类型 2：动态表。先读三个个数，再按顺序表读码长的码长，建码长表，
	// 用它解出全部码长（16／17／18 是三种重复），最后建字面量与距离两张表。
	clTop, clDone := b.label(), b.label()
	lenTop, lenDone := b.label(), b.label()
	lenLiteral, len16, len17, lenFill, lenNext := b.label(), b.label(), b.label(), b.label(), b.label()
	fillLoop := b.label()
	a.Mark(dynamic)
	a.MovImm(x64.RCX, 5)
	a.Call(lBits)
	a.AddImm(x64.RAX, 257)
	ws(wHlit, x64.RAX)
	a.CmpImm(x64.RAX, 286)
	a.Jcc(x64.A, lFail)
	a.MovImm(x64.RCX, 5)
	a.Call(lBits)
	a.AddImm(x64.RAX, 1)
	ws(wHdist, x64.RAX)
	a.CmpImm(x64.RAX, 30)
	a.Jcc(x64.A, lFail)
	a.MovImm(x64.RCX, 4)
	a.Call(lBits)
	a.AddImm(x64.RAX, 4)
	ws(wHclen, x64.RAX)
	// 长度数组清零（318 格）。
	b.get(x64.RDI, fWs)
	a.AddImm(x64.RDI, wLens)
	a.MovImm(x64.RCX, 318)
	a.Xor(x64.RAX, x64.RAX)
	zeroLens := b.label()
	a.Mark(zeroLens)
	a.MovStore(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RDI, 8)
	a.AddImm(x64.RCX, -1)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NE, zeroLens)
	wzero(wI)
	a.Mark(clTop)
	wl2(x64.RAX, wI, x64.RCX, wHclen)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, clDone)
	a.MovImm(x64.RCX, 3)
	a.Call(lBits)
	ws(wTmp, x64.RAX)
	b.leaCell(x64.RDI, "infClOrder")
	wl(x64.RAX, wI)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX8(x64.RDX, x64.RDI, 0)
	b.get(x64.RDI, fWs)
	a.Mov(x64.RAX, x64.RDX)
	a.ShlImm(x64.RAX, 3)
	a.AddImm(x64.RAX, wLens)
	a.Add(x64.RDI, x64.RAX)
	wl(x64.RAX, wTmp)
	a.MovStore(x64.RDI, 0, x64.RAX)
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	a.Jmp(clTop)
	a.Mark(clDone)
	b.get(x64.RCX, fWs)
	a.AddImm(x64.RCX, wLens)
	a.MovImm(x64.RDX, 19)
	b.get(x64.R8, fWs)
	a.AddImm(x64.R8, wClCnt)
	b.get(x64.R9, fWs)
	a.AddImm(x64.R9, wClSym)
	a.Call(lConstruct)
	wl2(x64.RAX, wHlit, x64.RCX, wHdist)
	a.Add(x64.RAX, x64.RCX)
	ws(wTotal, x64.RAX)
	wzero(wI)
	a.Mark(lenTop)
	wl2(x64.RAX, wI, x64.RCX, wTotal)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, lenDone)
	b.get(x64.RCX, fWs)
	a.AddImm(x64.RCX, wClCnt)
	b.get(x64.RDX, fWs)
	a.AddImm(x64.RDX, wClSym)
	a.Call(lDecode)
	ws(wSym, x64.RAX)
	a.CmpImm(x64.RAX, 16)
	a.Jcc(x64.B, lenLiteral)
	a.Jcc(x64.E, len16)
	a.CmpImm(x64.RAX, 17)
	a.Jcc(x64.E, len17)
	// 18：零重复 11～138 次。
	a.MovImm(x64.RCX, 7)
	a.Call(lBits)
	a.AddImm(x64.RAX, 11)
	ws(wRep, x64.RAX)
	wzero(wPrev)
	a.Jmp(lenFill)
	a.Mark(len17)
	a.MovImm(x64.RCX, 3)
	a.Call(lBits)
	a.AddImm(x64.RAX, 3)
	ws(wRep, x64.RAX)
	wzero(wPrev)
	a.Jmp(lenFill)
	a.Mark(len16)
	wl(x64.RAX, wI)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lFail) // 第一个就是重复 16：没有前一个可抄
	b.get(x64.RDI, fWs)
	a.AddImm(x64.RDI, wLens)
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, -1)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RAX, x64.RDI, 0)
	ws(wPrev, x64.RAX)
	a.MovImm(x64.RCX, 2)
	a.Call(lBits)
	a.AddImm(x64.RAX, 3)
	ws(wRep, x64.RAX)
	a.Mark(lenFill)
	wl2(x64.RAX, wI, x64.RCX, wRep)
	a.Add(x64.RAX, x64.RCX)
	wl(x64.RDX, wTotal)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.A, lFail) // 重复越过了总数
	a.Mark(fillLoop)
	wl(x64.RAX, wRep)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lenNext)
	b.get(x64.RDI, fWs)
	a.AddImm(x64.RDI, wLens)
	wl(x64.RAX, wI)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	wl(x64.RAX, wPrev)
	a.MovStore(x64.RDI, 0, x64.RAX)
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	wl(x64.RAX, wRep)
	a.AddImm(x64.RAX, -1)
	ws(wRep, x64.RAX)
	a.Jmp(fillLoop)
	a.Mark(lenLiteral)
	b.get(x64.RDI, fWs)
	a.AddImm(x64.RDI, wLens)
	wl(x64.RAX, wI)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	wl(x64.RAX, wSym)
	a.MovStore(x64.RDI, 0, x64.RAX)
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	a.Mark(lenNext)
	a.Jmp(lenTop)
	a.Mark(lenDone)
	// 结束符（256）必须有码，否则这一块永远收不了尾。
	b.get(x64.RDI, fWs)
	a.MovLoad(x64.RAX, x64.RDI, wLens+256*8)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lFail)
	a.Mark(constructBoth)
	b.get(x64.RCX, fWs)
	a.AddImm(x64.RCX, wLens)
	wl(x64.RDX, wHlit)
	b.get(x64.R8, fWs)
	a.AddImm(x64.R8, wLitCnt)
	b.get(x64.R9, fWs)
	a.AddImm(x64.R9, wLitSym)
	a.Call(lConstruct)
	a.Mark(constructDist)
	b.get(x64.RCX, fWs)
	a.AddImm(x64.RCX, wLens)
	wl(x64.RAX, wHlit)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RCX, x64.RAX)
	wl(x64.RDX, wHdist)
	b.get(x64.R8, fWs)
	a.AddImm(x64.R8, wDstCnt)
	b.get(x64.R9, fWs)
	a.AddImm(x64.R9, wDstSym)
	a.Call(lConstruct)

	// 符号流：小于 256 是字面量，256 是块尾，257～285 是长度码——查基数加
	// 额外比特得长度，再读一个距离码同样算出距离，往回拷那一段。
	codes := b.label()
	codeLit, codeNext := b.label(), b.label()
	copyLoop, matchDone := b.label(), b.label()
	a.Mark(codes)
	b.get(x64.RCX, fWs)
	a.AddImm(x64.RCX, wLitCnt)
	b.get(x64.RDX, fWs)
	a.AddImm(x64.RDX, wLitSym)
	a.Call(lDecode)
	ws(wSym, x64.RAX)
	a.CmpImm(x64.RAX, 256)
	a.Jcc(x64.B, codeLit)
	a.Jcc(x64.E, blockEnd)
	a.AddImm(x64.RAX, -257)
	a.CmpImm(x64.RAX, 29)
	a.Jcc(x64.AE, lFail)
	ws(wTmp, x64.RAX)
	b.leaCell(x64.RDI, "infLenExtra")
	wl(x64.RAX, wTmp)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX8(x64.RCX, x64.RDI, 0)
	ws(wTmp2, x64.RCX)
	b.leaCell(x64.RDI, "infLenBase")
	wl(x64.RAX, wTmp)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX16(x64.RDX, x64.RDI, 0)
	ws(wMLen, x64.RDX)
	wl(x64.RCX, wTmp2)
	a.Call(lBits)
	wl(x64.RCX, wMLen)
	a.Add(x64.RAX, x64.RCX)
	ws(wMLen, x64.RAX)
	b.get(x64.RCX, fWs)
	a.AddImm(x64.RCX, wDstCnt)
	b.get(x64.RDX, fWs)
	a.AddImm(x64.RDX, wDstSym)
	a.Call(lDecode)
	a.CmpImm(x64.RAX, 30)
	a.Jcc(x64.AE, lFail)
	ws(wTmp, x64.RAX)
	b.leaCell(x64.RDI, "infDistExtra")
	wl(x64.RAX, wTmp)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX8(x64.RCX, x64.RDI, 0)
	ws(wTmp2, x64.RCX)
	b.leaCell(x64.RDI, "infDistBase")
	wl(x64.RAX, wTmp)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX16(x64.RDX, x64.RDI, 0)
	ws(wDist, x64.RDX)
	wl(x64.RCX, wTmp2)
	a.Call(lBits)
	wl(x64.RCX, wDist)
	a.Add(x64.RAX, x64.RCX)
	ws(wDist, x64.RAX)
	wl2(x64.RAX, wDist, x64.RCX, wOutPos)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.A, lFail) // 距离比已输出的还远：没有可抄的
	wl2(x64.RCX, wOutPos, x64.RAX, wMLen)
	a.Add(x64.RCX, x64.RAX)
	a.Call(lReserve)
	wl(x64.RSI, wOut)
	wl(x64.RAX, wOutPos)
	a.Add(x64.RSI, x64.RAX)
	wl(x64.RAX, wDist)
	a.Sub(x64.RSI, x64.RAX)
	wl(x64.RDI, wOut)
	wl(x64.RAX, wOutPos)
	a.Add(x64.RDI, x64.RAX)
	wl(x64.RCX, wMLen)
	a.Mark(copyLoop)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, matchDone)
	a.MovLoad8(x64.RAX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RCX, -1)
	a.Jmp(copyLoop)
	a.Mark(matchDone)
	// 回拷的那一段也计入已输出：游标加上匹配长度。
	wl(x64.RAX, wOutPos)
	wl(x64.RCX, wMLen)
	a.Add(x64.RAX, x64.RCX)
	ws(wOutPos, x64.RAX)
	a.Jmp(codeNext)
	a.Mark(codeLit)
	wl(x64.RCX, wSym)
	a.Call(lOutByte)
	a.Mark(codeNext)
	a.Jmp(codes)

	// 块尾：不是最后一块就接着读下一块；是最后一块就把输出装进 bytes 块。
	a.Mark(blockEnd)
	wl(x64.RAX, wLast)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, blockTop)
	wl(x64.RCX, wOutPos)
	b.call("@bytes_new")
	b.put(fOut, x64.RAX)
	a.Mov(x64.RDI, x64.RAX)
	a.AddImm(x64.RDI, BytesBody)
	wl(x64.RSI, wOut)
	wl(x64.RDX, wOutPos)
	copyBytes(b)
	wl(x64.RCX, wOut)
	b.call("@free")
	b.get(x64.RCX, fWs)
	b.call("@free")
	b.get(x64.RAX, fOut)
	b.get(x64.RBX, fRbx) // 归还调用方的 rbx
	b.epilogue()

	// ---- 子程序 ----

	// 取比特：rcx＝个数（0～16），rax＝值。缓冲里不够就一个字节一个字节补，
	// 补到够为止；输入读完还要补即失败。取用之后缓冲里不足八个比特——
	// stored 块对齐字节边界靠的就是这一条。不碰 rsi／rdi。
	bitsNeed, bitsHave, bitsZero := b.label(), b.label(), b.label()
	a.Mark(lBits)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, bitsZero)
	ws(wBitsN, x64.RCX)
	a.Mark(bitsNeed)
	wl(x64.RDX, wBitCnt)
	wl(x64.RCX, wBitsN)
	a.Cmp(x64.RDX, x64.RCX)
	a.Jcc(x64.AE, bitsHave)
	wl(x64.RAX, wInPos)
	a.Mov(x64.R8, x64.RAX)
	wl(x64.RCX, wInLen)
	a.Cmp(x64.R8, x64.RCX)
	a.Jcc(x64.AE, lFail)
	wl(x64.R9, wIn)
	a.Add(x64.R9, x64.R8)
	a.MovZX8(x64.R10, x64.R9, 0)
	wl(x64.R11, wBitBuf)
	a.Mov(x64.RCX, x64.RDX)
	a.Shl(x64.R10)
	a.Or(x64.R11, x64.R10)
	ws(wBitBuf, x64.R11)
	a.AddImm(x64.RDX, 8)
	ws(wBitCnt, x64.RDX)
	a.Mov(x64.RAX, x64.R8)
	a.AddImm(x64.RAX, 1)
	ws(wInPos, x64.RAX)
	a.Jmp(bitsNeed)
	a.Mark(bitsHave)
	wl(x64.R8, wBitBuf)
	wl(x64.RCX, wBitsN)
	a.MovImm(x64.R9, 1)
	a.Shl(x64.R9)
	a.AddImm(x64.R9, -1)
	a.Mov(x64.R10, x64.R8)
	a.And(x64.R10, x64.R9)
	a.Shr(x64.R8)
	ws(wBitBuf, x64.R8)
	wl(x64.RDX, wBitCnt)
	wl(x64.RCX, wBitsN)
	a.Sub(x64.RDX, x64.RCX)
	ws(wBitCnt, x64.RDX)
	a.Mov(x64.RAX, x64.R10)
	a.Ret()
	a.Mark(bitsZero)
	a.Xor(x64.RAX, x64.RAX)
	a.Ret()

	// 读符号：rcx＝count、rdx＝symbol。一位一位攒码，攒到 code-count < first
	// 就落在当前长度的区间里，取 symbol[index + code - first]。十五位都落不進
	// 任何区间即失败。两张表的地址放 rsi／rdi——取比特不碰它们。
	decLoop, decHit := b.label(), b.label()
	a.Mark(lDecode)
	a.Mov(x64.RSI, x64.RCX)
	a.Mov(x64.RDI, x64.RDX)
	wzero(wCode)
	wzero(wFirst)
	wzero(wIndex)
	a.MovImm(x64.RAX, 1)
	ws(wLen, x64.RAX)
	a.Mark(decLoop)
	wl(x64.RAX, wLen)
	a.CmpImm(x64.RAX, 15)
	a.Jcc(x64.A, lFail)
	a.MovImm(x64.RCX, 1)
	a.Call(lBits)
	wl(x64.RCX, wCode)
	a.Or(x64.RCX, x64.RAX)
	ws(wCode, x64.RCX)
	wl(x64.RAX, wLen)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RAX, x64.RSI)
	a.MovLoad(x64.RDX, x64.RAX, 0) // count[长度]
	wl(x64.RCX, wCode)
	a.Sub(x64.RCX, x64.RDX)
	wl(x64.R8, wFirst)
	a.Cmp(x64.RCX, x64.R8)
	a.Jcc(x64.L, decHit)
	wl(x64.RAX, wIndex)
	a.Add(x64.RAX, x64.RDX)
	ws(wIndex, x64.RAX)
	wl(x64.RAX, wFirst)
	a.Add(x64.RAX, x64.RDX)
	a.ShlImm(x64.RAX, 1)
	ws(wFirst, x64.RAX)
	wl(x64.RAX, wCode)
	a.ShlImm(x64.RAX, 1)
	ws(wCode, x64.RAX)
	wl(x64.RAX, wLen)
	a.AddImm(x64.RAX, 1)
	ws(wLen, x64.RAX)
	a.Jmp(decLoop)
	a.Mark(decHit)
	wl(x64.RCX, wCode)
	wl(x64.R8, wFirst)
	a.Sub(x64.RCX, x64.R8)
	wl(x64.RAX, wIndex)
	a.Add(x64.RAX, x64.RCX)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RAX, x64.RDI)
	a.MovLoad(x64.RAX, x64.RAX, 0)
	a.Ret()

	// 建表：rcx＝码长数组、rdx＝符号个数、r8＝count、r9＝symbol。
	// 先数每个长度有几个码；全是零即没有这张表（距离表允许）；额度核对
	// （left 每长一位翻倍再减去该长度的个数）出负数即超订，失败。剩下正额度
	// 是"不完整"的表，不拒绝（固定距离表天生如此）；随后按长度排好 symbol。
	zeroCnt, countLoop, countDone, countNext := b.label(), b.label(), b.label(), b.label()
	leftLoop, leftDone, offsInit := b.label(), b.label(), b.label()
	offsLoop, offsDone := b.label(), b.label()
	symLoop, symNext, constructRet := b.label(), b.label(), b.label()
	a.Mark(lConstruct)
	ws(wLensP, x64.RCX)
	ws(wN, x64.RDX)
	ws(wCntP, x64.R8)
	ws(wSymP, x64.R9)
	wl(x64.RDI, wCntP)
	a.Xor(x64.RAX, x64.RAX)
	a.MovImm(x64.RCX, 16)
	a.Mark(zeroCnt)
	a.MovStore(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RDI, 8)
	a.AddImm(x64.RCX, -1)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NE, zeroCnt)
	wzero(wI)
	a.Mark(countLoop)
	wl2(x64.RAX, wI, x64.RCX, wN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, countDone)
	wl(x64.RDI, wLensP)
	wl(x64.RAX, wI)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RDI, 0) // 这一个符号的码长
	wl(x64.RDI, wCntP)
	a.Mov(x64.RAX, x64.RDX)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RAX, x64.RDI, 0)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, 0, x64.RAX)
	a.Mark(countNext)
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	a.Jmp(countLoop)
	a.Mark(countDone)
	wl(x64.RDI, wCntP)
	a.MovLoad(x64.RAX, x64.RDI, 0)
	wl(x64.RCX, wN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.E, constructRet) // 全零：没有这张表
	a.MovImm(x64.RAX, 1)
	ws(wLeft, x64.RAX)
	a.MovImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	a.Mark(leftLoop)
	wl(x64.RAX, wI)
	a.CmpImm(x64.RAX, 15)
	a.Jcc(x64.A, leftDone)
	wl(x64.RAX, wLeft)
	a.ShlImm(x64.RAX, 1)
	ws(wLeft, x64.RAX)
	wl(x64.RDI, wCntP)
	wl(x64.RAX, wI)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RDI, 0)
	wl(x64.RAX, wLeft)
	a.Sub(x64.RAX, x64.RDX)
	ws(wLeft, x64.RAX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, lFail) // 超订
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	a.Jmp(leftLoop)
	a.Mark(leftDone)
	// left>0 是"不完整"的码表：固定距离表（30 个 5 比特码）天生如此，RFC 1951 允许。
	// 不完整不拒绝——真读到没用上的那一段码，读符号那一条十五位落不进区间自会失败。
	// 这里只拦"超订"（left<0，已在上面那圈里拦下）。
	a.Mark(offsInit)
	b.get(x64.RDI, fWs)
	a.AddImm(x64.RDI, wOffs)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RDI, 8, x64.RAX) // offs[1] = 0
	a.MovImm(x64.RCX, 1)
	a.Mark(offsLoop)
	a.CmpImm(x64.RCX, 14)
	a.Jcc(x64.A, offsDone)
	a.Mov(x64.RAX, x64.RCX)
	a.ShlImm(x64.RAX, 3)
	a.Mov(x64.R8, x64.RDI)
	a.Add(x64.R8, x64.RAX)
	a.MovLoad(x64.R9, x64.R8, 0) // offs[len]
	a.Mov(x64.R10, x64.RCX)
	a.ShlImm(x64.R10, 3)
	wl(x64.R11, wCntP)
	a.Add(x64.R11, x64.R10)
	a.MovLoad(x64.R10, x64.R11, 0) // count[len]
	a.Add(x64.R9, x64.R10)
	a.Mov(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, 1)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RAX, x64.RDI)
	a.MovStore(x64.RAX, 0, x64.R9) // offs[len+1]
	a.AddImm(x64.RCX, 1)
	a.Jmp(offsLoop)
	a.Mark(offsDone)
	wzero(wI)
	a.Mark(symLoop)
	wl2(x64.RAX, wI, x64.RCX, wN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, constructRet)
	wl(x64.RDI, wLensP)
	wl(x64.RAX, wI)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RDI, 0)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, symNext)
	b.get(x64.RDI, fWs)
	a.AddImm(x64.RDI, wOffs)
	a.Mov(x64.RAX, x64.RDX)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.R8, x64.RDI, 0) // offs[码长]
	a.Mov(x64.R9, x64.R8)
	a.AddImm(x64.R9, 1)
	a.MovStore(x64.RDI, 0, x64.R9)
	wl(x64.RDI, wSymP)
	a.Mov(x64.RAX, x64.R8)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	wl(x64.RAX, wI)
	a.MovStore(x64.RDI, 0, x64.RAX)
	a.Mark(symNext)
	wl(x64.RAX, wI)
	a.AddImm(x64.RAX, 1)
	ws(wI, x64.RAX)
	a.Jmp(symLoop)
	a.Mark(constructRet)
	a.Ret()

	// 扩容：rcx＝需要的总容量。够就直接回；不够取"翻倍"与"需要值"的较大者，
	// 申请新缓冲、搬走已输出的字节、释放旧的。
	reserveCap, reserveRet := b.label(), b.label()
	a.Mark(lReserve)
	wl(x64.RAX, wOutCap)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, reserveRet)
	ws(wTmp3, x64.RCX)
	a.ShlImm(x64.RAX, 1)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, reserveCap)
	wl(x64.RAX, wTmp3)
	a.Mark(reserveCap)
	ws(wOutCap, x64.RAX)
	a.Mov(x64.RCX, x64.RAX)
	a.Xor(x64.RDX, x64.RDX)
	b.call("@alloc")
	ws(wTmp2, x64.RAX)
	wl(x64.RSI, wOut)
	wl(x64.RDI, wTmp2)
	wl(x64.RDX, wOutPos)
	copyBytes(b)
	wl(x64.RCX, wOut)
	b.call("@free")
	wl(x64.RAX, wTmp2)
	ws(wOut, x64.RAX)
	a.Mark(reserveRet)
	a.Ret()

	// 输出一个字节：rcx＝值。先保证装得下，再写入、游标加一。
	a.Mark(lOutByte)
	ws(wTmp, x64.RCX)
	wl(x64.RCX, wOutPos)
	a.AddImm(x64.RCX, 1)
	a.Call(lReserve)
	wl(x64.RDI, wOut)
	wl(x64.RAX, wOutPos)
	a.Add(x64.RDI, x64.RAX)
	wl(x64.RCX, wTmp)
	a.MovStore8(x64.RDI, 0, x64.RCX)
	wl(x64.RAX, wOutPos)
	a.AddImm(x64.RAX, 1)
	ws(wOutPos, x64.RAX)
	a.Ret()

	// 共同失败收尾：子程序是 call 进去的，栈上留着返回地址，先把 rsp 拨回
	// 帧底；输出缓冲若已申请就释放，工作区释放，返回 0。
	failNoOut := b.label()
	a.Mark(lFail)
	a.Mov(x64.RSP, x64.RBP)
	a.AddImm(x64.RSP, -frameSize)
	wl(x64.RAX, wOut)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, failNoOut)
	a.Mov(x64.RCX, x64.RAX)
	b.call("@free")
	a.Mark(failNoOut)
	b.get(x64.RCX, fWs)
	b.call("@free")
	a.Xor(x64.RAX, x64.RAX)
	b.get(x64.RBX, fRbx) // 归还调用方的 rbx
	b.epilogue()
}

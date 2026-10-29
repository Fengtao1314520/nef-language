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

// file.go 是访问操作系统的那几个例程：把路径翻成本机写法、整个文件读进来（按文本、按字节）、等一行输入。
//
// 产物只依赖 kernel32，所以宽字符翻译用 MultiByteToWideChar／WideCharToMultiByte
// 这一对——它们也在 kernel32 里；`CreateFileW` 收 UTF-16，而 nef 的文本一律 UTF-8，
// 中间一定要翻一次。第五个及以后的实参落回本帧的 [rbp-0xE0] 往下一个位置一个位置，与 @write 同一张表。
//
// en: file.go holds the routines that touch the operating system: translate a path into the
// local spelling, read a whole file (as text, as bytes), and wait for one line of input. The
// product depends only on kernel32, so wide-character translation uses the
// MultiByteToWideChar/WideCharToMultiByte pair - also in kernel32; `CreateFileW` takes UTF-16
// while nef text is always UTF-8, so something must convert in between. The fifth and
// later arguments fall back into this frame at [rbp-0xE0] and onwards, one position at a time,
// the same table as @write.
package runtime

import "neflang.org/nf/internal/x64"

// 系统调用要的那些常量与标志，一律写死在指令里。
//
// en: The constants and flags the system calls need, all written directly into the
// instructions.
const (
	cpUTF8        = 65001
	genericRead   = int64(0x80000000)
	shareAll      = 7 // 读、写、删都允许共享：读取文件时不应锁定其他使用者
	openExisting  = 3
	attrNormal    = 0x80
	stdInHandle   = -10
	invalidHandle = -1
	wideCap       = 4096 // 缓冲装得下 4096 个宽字符
	lineCap       = 65536

	codeNoFile  = 10041
	codeNotUTF8 = 10044
	codeBadPath = 10051
)

// 文本与文件这一支的静态缓冲。行缓冲前面留了 16 字节的占位头，
// 好让 @read_line 收完整行之后直接叫 @str_sub 造对象，不必再写一遍复制。
//
// en: The static buffers of the text-and-file branch. The line buffer is fronted by a
// placeholder 16-byte header so that @read_line, once a line is complete, can call @str_sub to
// build the
// object directly instead of writing the copy again.
func fileData(put func(name string, bs []byte)) {
	put("wPath", make([]byte, wideCap*2+2))
	put("rdBuf", make([]byte, strHead+lineCap))
	put("hstdin", make([]byte, 8))
	put("aWide", make([]byte, wideCap*2+2))
	put("aBuf", make([]byte, strHead+lineCap))
}

// @to_wide(rcx=文本对象, rdx=目标缓冲)：UTF-8 翻成宽字符，末尾补一个 0，
// 返回 rax=几个宽字符；0 表示翻不成或者装不下。
//
// en: @to_wide (rcx = text object, rdx = target buffer): converts UTF-8 into wide characters,
// appends a zero, and returns rax = how many wide characters; 0 means it could not be converted
// or did not fit.
func toWide(b *rt) {
	const (
		xDst = 0
	)
	a := b.a
	b.prologue()
	b.put(xDst, x64.RDX)
	a.MovLoad(x64.R9, x64.RCX, 0) // 字节数
	a.LeaMem(x64.R8, x64.RCX, strHead)
	a.MovImm(x64.RCX, cpUTF8)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.RAX, xDst)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, wideCap)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	b.impCall("MultiByteToWideChar")
	// 末尾那个 0：宽字符两个字节，一个一个写，避免越过缓冲上界
	b.get(x64.RDI, xDst)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RDI, x64.RAX)
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore8(x64.RDI, 0, x64.RDX)
	a.MovStore8(x64.RDI, 1, x64.RDX)
	b.epilogue()
}

// @count_runes(rcx=字节地址, rdx=几个字节)：返回 rax=几个码点。
//
// 只有一个规则：不是续字节（`(b & 0xC0) != 0x80`）的就是一个码点的开头。
//
// en: @count_runes (rcx = byte address, rdx = byte count): returns rax = how many code points.
// One rule only: anything that is not a continuation byte (`(b & 0xC0) != 0x80`) starts a code
// point.
func countRunes(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.RSI, x64.RCX)
	a.Mov(x64.R9, x64.RDX)
	a.MovImm(x64.R8, leadMask)
	a.MovImm(x64.R10, contByte)
	a.Xor(x64.R11, x64.R11)
	top := b.label()
	done := b.label()
	skip := b.label()
	a.Mark(top)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, done)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.And(x64.RAX, x64.R8)
	a.Cmp(x64.RAX, x64.R10)
	a.Jcc(x64.E, skip)
	a.AddImm(x64.R11, 1)
	a.Mark(skip)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.R9, -1)
	a.Jmp(top)
	a.Mark(done)
	a.Mov(x64.RAX, x64.R11)
	b.epilogue()
}

// @utf8_ok(rcx=字节地址, rdx=几个字节)：返回 rax=1 是合法的 UTF-8，0 不是。
//
// `file.read` 遇到不合法的字节要抛 10044 号，所以这里按 Unicode 的规则查得较严：
// 不允许 0xC0／0xC1 那种过长两字节、不允许代理区（三字节里 0xED 后面只能跟 0x80～0x9F）、
// 不允许超过 U+10FFFF（四字节里 0xF4 后面只能跟 0x80～0x8F），也不允许尾部缺续字节。
//
// en: @utf8_ok (rcx = byte address, rdx = byte count): returns rax = 1 for valid UTF-8, 0 for
// not. file.read must throw code 10044 on an invalid byte, so this checks by the
// Unicode rules rather than loosely: no overlong two-byte forms (0xC0/0xC1), no surrogate range
// (in three bytes, an 0xED may only be followed by 0x80-0x9F), nothing above U+10FFFF (in four
// bytes, 0xF4 may only be followed by 0x80-0x8F), and no missing continuation bytes at the end.
func utf8Ok(b *rt) {
	a := b.a
	b.prologue()
	a.Mov(x64.RSI, x64.RCX) // 游标
	a.Mov(x64.R10, x64.RDX) // 还剩几字节
	a.MovImm(x64.R11, 1)    // 一路都合法就是 1
	a.MovImm(x64.R8, leadMask)
	a.MovImm(x64.R9, contByte)
	top := b.label()
	done := b.label()
	bad := b.label()
	ascii := b.label()
	two := b.label()
	three := b.label()
	four := b.label()
	n1 := b.label()
	n2 := b.label()
	n3 := b.label()
	up4 := b.label()
	a.Mark(top)
	a.Test(x64.R10, x64.R10)
	a.Jcc(x64.E, done)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, 0x80)
	a.Jcc(x64.B, ascii)
	a.Mov(x64.RDI, x64.RAX) // 首字节留着，下面算下界要用
	a.CmpImm(x64.RAX, 0xC2)
	a.Jcc(x64.B, bad) // 续字节开头，或者 0xC0／0xC1 那种过长编码
	a.CmpImm(x64.RAX, 0xE0)
	a.Jcc(x64.B, two)
	a.CmpImm(x64.RAX, 0xF0)
	a.Jcc(x64.B, three)
	a.CmpImm(x64.RAX, 0xF5)
	a.Jcc(x64.B, four)
	a.Jmp(bad)

	// 一个 ASCII 字节：往前挪一个位置
	a.Mark(ascii)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.R10, -1)
	a.Jmp(top)

	// 两字节：第二个必须是续字节
	a.Mark(two)
	a.CmpImm(x64.R10, 2)
	a.Jcc(x64.B, bad)
	a.MovZX8(x64.RAX, x64.RSI, 1)
	a.And(x64.RAX, x64.R8)
	a.Cmp(x64.RAX, x64.R9)
	a.Jcc(x64.NE, bad)
	a.AddImm(x64.RSI, 2)
	a.AddImm(x64.R10, -2)
	a.Jmp(top)

	// 三字节：第一个续字节的上下界看首字节（0xE0 与 0xED 各偏一头），第二个按常规
	a.Mark(three)
	a.CmpImm(x64.R10, 3)
	a.Jcc(x64.B, bad)
	a.MovImm(x64.RAX, 0x80)
	a.MovImm(x64.RDX, 0xBF) // 常规上界；0xED／0xF4 那一支再往下压
	a.CmpImm(x64.RDI, 0xE0)
	a.Jcc(x64.NE, n1)
	a.MovImm(x64.RAX, 0xA0)
	a.Mark(n1)
	a.CmpImm(x64.RDI, 0xED)
	a.Jcc(x64.NE, n2)
	a.MovImm(x64.RDX, 0x9F)
	a.Mark(n2)
	a.MovZX8(x64.RCX, x64.RSI, 1)
	a.Cmp(x64.RCX, x64.RAX)
	a.Jcc(x64.B, bad)
	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.A, bad)
	a.MovZX8(x64.RCX, x64.RSI, 2)
	a.And(x64.RCX, x64.R8)
	a.Cmp(x64.RCX, x64.R9)
	a.Jcc(x64.NE, bad)
	a.AddImm(x64.RSI, 3)
	a.AddImm(x64.R10, -3)
	a.Jmp(top)

	// 四字节：0xF0 的下界抬高、0xF4 的上界压低，后面两个按常规
	a.Mark(four)
	a.CmpImm(x64.R10, 4)
	a.Jcc(x64.B, bad)
	a.MovImm(x64.RAX, 0x80)
	a.MovImm(x64.RDX, 0xBF) // 常规上界；0xED／0xF4 那一支再往下压
	a.CmpImm(x64.RDI, 0xF0)
	a.Jcc(x64.NE, n3)
	a.MovImm(x64.RAX, 0x90)
	a.Mark(n3)
	a.CmpImm(x64.RDI, 0xF4)
	a.Jcc(x64.NE, up4)
	a.MovImm(x64.RDX, 0x8F)
	a.Mark(up4)
	a.MovZX8(x64.RCX, x64.RSI, 1)
	a.Cmp(x64.RCX, x64.RAX)
	a.Jcc(x64.B, bad)
	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.A, bad)
	a.MovZX8(x64.RCX, x64.RSI, 2)
	a.And(x64.RCX, x64.R8)
	a.Cmp(x64.RCX, x64.R9)
	a.Jcc(x64.NE, bad)
	a.MovZX8(x64.RCX, x64.RSI, 3)
	a.And(x64.RCX, x64.R8)
	a.Cmp(x64.RCX, x64.R9)
	a.Jcc(x64.NE, bad)
	a.AddImm(x64.RSI, 4)
	a.AddImm(x64.R10, -4)
	a.Jmp(top)

	a.Mark(bad)
	a.Xor(x64.R11, x64.R11)
	a.Mark(done)
	a.Mov(x64.RAX, x64.R11)
	b.epilogue()
}

// @file_read(rcx=路径文本)：整个文件读进来，返回 rax=内容的文本对象。
//
// 打不开、读不全都是 10041 号；读完了再验一遍 UTF-8，不是合法编码是 10044 号。
// 一次读满：文件多大就分配多大，读不满就当场报错，不留半截内容给程序。
//
// en: @file_read (rcx = path text): reads the whole file and returns rax = its content as a
// text object. Not opening and reading short are both code 10041; after reading, UTF-8 is
// validated and an illegal encoding is code 10044. One read takes everything:
// allocate the file's size, and if the read comes up short report it there and then rather than
// handing the program a truncated body.
func fileRead(b *rt) {
	const (
		xHandle = 0
		xSize   = 1
		xGot    = 2
		xObj    = 3
	)
	a := b.a
	b.prologue()
	b.put(4, x64.RCX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, 4)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)

	// CreateFileW(路径, 只读, 全共享, 无安全属性, 只开现有的, 普通属性, 无模板)
	b.leaRIP(x64.RCX, "wPath")
	a.MovImm(x64.RDX, genericRead)
	a.MovImm(x64.R8, shareAll)
	a.Xor(x64.R9, x64.R9)
	a.MovImm(x64.RAX, openExisting)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, attrNormal)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	b.impCall("CreateFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	noFile := b.label()
	a.Jcc(x64.E, noFile)
	b.put(xHandle, x64.RAX)

	// GetFileSizeEx(句柄, &哪里放)：返回的布尔不管，失败时那个位置是 -1
	b.get(x64.RCX, xHandle)
	a.LeaMem(x64.RDX, x64.RBP, -8*(xSize+1))
	b.impCall("GetFileSizeEx")
	b.get(x64.RAX, xSize)
	a.MovImm(x64.R8, -1)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, noFile)

	// 对象＝16 字节的头＋整块内容，标记是文本
	a.AddImm(x64.RAX, strHead)
	a.Mov(x64.RCX, x64.RAX)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(xObj, x64.RAX)

	// ReadFile(句柄, 内容起点, 要读几个, &读到了几个, 不重叠)：一次到底。
	// "读到了几个"是个 DWORD，只能按四个字节读回来——多读的那半截是栈上的旧值，
	// 产物自己跑时那半截正好是零，宿主在内存里直跑时是宿主用过的残留数据。
	b.get(x64.RCX, xHandle)
	b.get(x64.RDX, xObj)
	a.AddImm(x64.RDX, strHead)
	b.get(x64.R8, xSize)
	a.LeaMem(x64.R9, x64.RBP, -8*(xGot+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCall("ReadFile")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noFile)
	a.MovZX32(x64.RAX, x64.RBP, -8*(xGot+1))
	b.get(x64.RDX, xSize)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, noFile)

	b.get(x64.RCX, xHandle)
	b.impCall("CloseHandle")

	// 验编码
	b.get(x64.RCX, xObj)
	a.AddImm(x64.RCX, strHead)
	b.get(x64.RDX, xSize)
	b.call("@utf8_ok")
	a.Test(x64.RAX, x64.RAX)
	notUTF := b.label()
	a.Jcc(x64.E, notUTF)

	// 开头那个 BOM（U+FEFF）丢掉不要：整截往前挪三字节，长度跟着减。
	// 只有真的以 EF BB BF 开头才动手，三字节里少一个都不算。
	b.get(x64.RDX, xSize)
	a.CmpImm(x64.RDX, 3)
	noBOM := b.label()
	a.Jcc(x64.B, noBOM)
	b.get(x64.RSI, xObj)
	a.AddImm(x64.RSI, strHead)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, 0xEF)
	a.Jcc(x64.NE, noBOM)
	a.MovZX8(x64.RAX, x64.RSI, 1)
	a.CmpImm(x64.RAX, 0xBB)
	a.Jcc(x64.NE, noBOM)
	a.MovZX8(x64.RAX, x64.RSI, 2)
	a.CmpImm(x64.RAX, 0xBF)
	a.Jcc(x64.NE, noBOM)
	a.Mov(x64.R10, x64.RSI) // 读的位置＝起点＋3
	a.AddImm(x64.R10, 3)
	a.Mov(x64.R11, x64.RDX)
	a.AddImm(x64.R11, -3) // 还剩几字节要挪
	bom := b.label()
	bomd := b.label()
	a.Mark(bom)
	a.Test(x64.R11, x64.R11)
	a.Jcc(x64.E, bomd)
	a.MovZX8(x64.RAX, x64.R10, 0)
	a.MovStore8(x64.RSI, 0, x64.RAX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.R10, 1)
	a.AddImm(x64.R11, -1)
	a.Jmp(bom)
	a.Mark(bomd)
	b.get(x64.RAX, xSize)
	a.AddImm(x64.RAX, -3)
	b.put(xSize, x64.RAX)
	a.Mark(noBOM)

	// 头两个槽：字节数与码点数
	b.get(x64.RDI, xObj)
	b.get(x64.RAX, xSize)
	a.MovStore(x64.RDI, 0, x64.RAX)
	b.get(x64.RCX, xObj)
	a.AddImm(x64.RCX, strHead)
	b.get(x64.RDX, xSize)
	b.call("@count_runes")
	b.get(x64.RDI, xObj)
	a.MovStore(x64.RDI, 8, x64.RAX)
	b.get(x64.RAX, xObj)
	b.epilogue()

	a.Mark(noFile)
	b.throwWith(codeNoFile, "sNoFile")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
	a.Mark(notUTF)
	b.throwWith(codeNotUTF8, "sNotUTF8")
}

// @file_read_bytes(rcx=路径文本)：整个文件按字节读进来，返回 rax=`bytes`。
//
// 与 @file_read 的分别是最后那一步：不验编码、不丢开头的 BOM、不数码点，字节是什么就返回什么，
// 所以读二进制（读产物自己也算）不会命中 10044 号；打不开、读不全仍是 10041 号那一句。
// 空文件返回空的块，不另开号。
//
// 这一条例程此前返回的是 `list<byte>`：先按字节读进数据块，再从尾巴往回把每一个位置
// 撑成 8 字节（容器那个位置一律 8 字节）。换成 `bytes` 之后正文就是一个位置一字节连着摆，
// ReadFile 直接读进块里那一段，一次拷贝都不必做——读 5,607,424 字节的文件，
// 累计从 98,205,840 降到 5,607,456 这一档。
//
// 读不满那一条现在先把这一块归还给堆再抛：那块是这一条自己造的，抛出去之后没有持有者。
//
// en: @file_read_bytes (rcx = path text): reads the whole file as bytes and returns rax =
// `bytes`. The only difference from @file_read is the last step: no encoding check, no BOM
// stripped, no code point count - the bytes are returned as they are, so reading binary
// (including the product itself) cannot hit code 10044, while not opening and reading short
// still give the 10041 sentence. An empty file returns an empty block; no separate code.
// This routine previously returned `list<byte>`: bytes were read
// into a data block first, then each position was widened to 8 bytes from the end backwards
// (a container slot is always 8 bytes). With `bytes`, the body is one byte per position laid
// out contiguously and ReadFile reads straight into that part of the block with no copy at
// all - reading the 5,607,424-byte file went from 98,205,840 accumulated bytes down to the
// 5,607,456 band. The short-read path now returns this
// block to the heap before throwing: the block was made by this routine, and after the throw
// nobody holds it.
func fileReadBytes(b *rt) {
	const (
		xPath   = 0 // 路径那一段文本
		xHandle = 1
		xSize   = 2 // 文件有几个字节
		xGot    = 3 // ReadFile 交回的"读到了几个"
		xObj    = 4 // 造好的 bytes
	)
	var (
		badPath = b.label()
		noFile  = b.label()
		noRead  = b.label()
	)
	a := b.a
	b.prologue()
	b.put(xPath, x64.RCX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, xPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

	// CreateFileW(路径, 只读, 全共享, 无安全属性, 只开现有的, 普通属性, 无模板)
	b.leaRIP(x64.RCX, "wPath")
	a.MovImm(x64.RDX, genericRead)
	a.MovImm(x64.R8, shareAll)
	a.Xor(x64.R9, x64.R9)
	a.MovImm(x64.RAX, openExisting)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, attrNormal)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	b.impCall("CreateFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, noFile)
	b.put(xHandle, x64.RAX)

	b.get(x64.RCX, xHandle)
	a.LeaMem(x64.RDX, x64.RBP, -8*(xSize+1))
	b.impCall("GetFileSizeEx")
	b.get(x64.RAX, xSize)
	a.MovImm(x64.R8, -1)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, noFile)

	// 一块正好装得下这个文件的字节：@bytes_new 把"字节数"那个位置摆好，正文留在它后面。
	b.get(x64.RCX, xSize)
	b.call("@bytes_new")
	b.put(xObj, x64.RAX)

	b.get(x64.RAX, xSize)
	a.Test(x64.RAX, x64.RAX)
	noBytes := b.label()
	a.Jcc(x64.E, noBytes)

	// ReadFile(句柄, 正文起点, 要读几个, &读到了几个, 不重叠)：一次到底，读不满就报错。
	b.get(x64.RCX, xHandle)
	b.get(x64.RDX, xObj)
	a.AddImm(x64.RDX, BytesBody)
	b.get(x64.R8, xSize)
	a.LeaMem(x64.R9, x64.RBP, -8*(xGot+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCall("ReadFile")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noRead)
	a.MovZX32(x64.RAX, x64.RBP, -8*(xGot+1))
	b.get(x64.RDX, xSize)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, noRead)

	a.Mark(noBytes)
	b.get(x64.RCX, xHandle)
	b.impCall("CloseHandle")
	b.get(x64.RAX, xObj)
	b.epilogue()

	a.Mark(noRead)
	b.get(x64.RCX, xObj)
	b.call("@free") // 这一块是自己造的，抛出去之后没人再拿着它
	a.Mark(noFile)
	b.throwWith(codeNoFile, "sNoFile")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @read_line()：等一行，返回 rax=文本对象（不含行尾）。
//
// 一个字节一个字节地要：ReadFile 在控制台上本来就是一行一行进来的，
// 事先不知道有多长，只有这样才能在遇到换行那一刻停止。
// 后面再没有可读的了、且一个字节都没接到，就返回空文本。
// 行超过 lineCap 就在此停止——多出来的留在输入里，下一次接着读。
//
// en: @read_line(): waits for one line and returns rax = a text object (without the line
// ending). Bytes are requested one at a time: ReadFile on a console already delivers line by
// line, the length is unknown beforehand, and only this way can it stop at the moment a newline
// arrives. When there is nothing further to read and not one byte was received, it returns
// empty text. A line longer than lineCap stops there - the remainder stays in the input
// and the next call continues reading it.
func readLine(b *rt) {
	const (
		xN   = 0
		xGot = 1
	)
	a := b.a
	b.prologue()
	a.MovImm(x64.RAX, 0)
	b.put(xN, x64.RAX)

	// 标准输入句柄拿一次就缓存
	b.loadRIP(x64.RAX, "hstdin")
	have := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, have)
	a.MovImm(x64.RCX, stdInHandle)
	b.impCall("GetStdHandle")
	b.storeRIP(x64.RAX, "hstdin")
	a.Mark(have)

	top := b.label()
	out := b.label()
	nl := b.label()
	done := b.label()
	a.Mark(top)
	b.get(x64.RAX, xN)
	a.CmpImm(x64.RAX, lineCap)
	a.Jcc(x64.AE, out)
	b.leaRIP(x64.RCX, "hstdin")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	b.leaRIP(x64.RDX, "rdBuf")
	a.AddImm(x64.RDX, strHead)
	b.get(x64.R8, xN)
	a.Add(x64.RDX, x64.R8)
	a.MovImm(x64.R8, 1)
	a.LeaMem(x64.R9, x64.RBP, -8*(xGot+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCall("ReadFile")
	a.MovZX32(x64.RAX, x64.RBP, -8*(xGot+1)) // 读到了几个：DWORD，另一半是栈上的旧值
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, out) // 没读到一个字节：后面没有可读的了
	b.get(x64.RAX, xN)
	a.AddImm(x64.RAX, 1)
	b.put(xN, x64.RAX)
	// 遇到换行就停止，换行本身不进结果
	b.leaRIP(x64.RAX, "rdBuf")
	a.AddImm(x64.RAX, strHead)
	b.get(x64.RDX, xN)
	a.AddImm(x64.RDX, -1)
	a.Add(x64.RAX, x64.RDX)
	a.MovZX8(x64.RCX, x64.RAX, 0)
	a.CmpImm(x64.RCX, '\n')
	a.Jcc(x64.E, nl)
	a.Jmp(top)

	a.Mark(nl) // 去掉换行；它前面若是回车，一并去掉
	b.get(x64.RAX, xN)
	a.AddImm(x64.RAX, -1)
	b.put(xN, x64.RAX)
	b.leaRIP(x64.RAX, "rdBuf")
	a.AddImm(x64.RAX, strHead)
	b.get(x64.RDX, xN)
	a.AddImm(x64.RDX, -1)
	a.CmpImm(x64.RDX, 0)
	a.Jcc(x64.B, done)
	a.Add(x64.RAX, x64.RDX)
	a.MovZX8(x64.RCX, x64.RAX, 0)
	a.CmpImm(x64.RCX, '\r')
	a.Jcc(x64.NE, done)
	b.get(x64.RAX, xN)
	a.AddImm(x64.RAX, -1)
	b.put(xN, x64.RAX)
	a.Jmp(done)

	a.Mark(out)
	a.Jmp(done)
	a.Mark(done)
	b.leaRIP(x64.RCX, "rdBuf")
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, xN)
	b.call("@str_sub")
	b.epilogue()
}

// @args()：返回 rax=list<string>，脚本名自己不算一个。
//
// 拆法按 Windows 的那一套：两个反斜杠对出一个反斜杠，后面紧跟的引号仍然只管开关引号；
// 奇数个反斜杠跟一个引号对出一个**字面**引号，那个引号就不算开关。
// 空格只在引号外面才是分界。
// 先按这套规则把宽字符整理进 aWide，再整串交 WideCharToMultiByte 翻成 UTF-8——
// 反斜杠与引号的处理在宽字符上做，翻译交给系统，两件事分开处理。
//
// en: @args(): returns rax = list<string>, the script's own name not counted. Splitting
// follows the Windows rules: two backslashes yield one backslash and a quote right after still
// only opens or closes quoting; an odd run of backslashes followed by a quote yields a LITERAL
// quote and that quote does not toggle. A space separates only outside quotes. The wide
// characters are cleaned into aWide by those rules first, then the whole run is handed to
// WideCharToMultiByte for UTF-8 - backslash and quote handling happens on wide characters and
// translation is left to the system, so the two do not tangle.
func args(b *rt) {
	const (
		xOut  = 0 // list句柄
		xP    = 1 // 主游标：上一个 token 收在哪
		xQ    = 2 // 本 token 的游标
		xWL   = 3 // 整理出几个宽字符
		xQuo  = 4 // 在引号里吗
		xN    = 5 // 连着几个反斜杠
		xK    = 6 // 还要添几个
		xSeen = 7 // 已经见过第一个 token 没有：那一个是脚本名自己，不进 args
	)
	a := b.a
	b.prologue()
	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, tagString)
	b.call("@list_new")
	b.put(xOut, x64.RAX)
	// 在内存里直接跑的时候宿主已经把参数摆成宽字符放在 cmdW 那个位置里：
	// 那才是"这个程序的命令行"，机器上真正的命令行带着 `run 文件.nf`，不能用。
	b.loadRIP(x64.RAX, "cmdW")
	a.Test(x64.RAX, x64.RAX)
	fromHost := b.label()
	a.Jcc(x64.NE, fromHost)
	b.impCall("GetCommandLineW")
	a.Mark(fromHost)
	b.put(xP, x64.RAX)
	a.MovImm(x64.RAX, 0)
	b.put(xSeen, x64.RAX)

	// putWide 把 R8 里那个宽字符添到 aWide 尾巴上，只碰 rax／rdi。
	putWide := func() {
		b.get(x64.RAX, xWL)
		a.ShlImm(x64.RAX, 1)
		b.leaRIP(x64.RDI, "aWide")
		a.Add(x64.RDI, x64.RAX)
		a.MovStore8(x64.RDI, 0, x64.R8)
		a.Mov(x64.RAX, x64.R8)
		a.ShrImm(x64.RAX, 8)
		a.MovStore8(x64.RDI, 1, x64.RAX)
		b.get(x64.RAX, xWL)
		a.AddImm(x64.RAX, 1)
		b.put(xWL, x64.RAX)
	}

	loop := b.label()
	spc := b.label()
	quit := b.label()
	body := b.label()
	start := b.label()
	notSpace := b.label()
	notQuote := b.label()
	bsCnt := b.label()
	bsPeek := b.label()
	bsPlain := b.label()
	plainLoop := b.label()
	plainDone := b.label()
	halfLoop := b.label()
	halfDone := b.label()
	evenRun := b.label()
	lit := b.label()
	tail := b.label()

	a.Mark(loop)
	b.get(x64.RSI, xP)
	a.Mark(spc)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, quit)
	a.CmpImm(x64.RAX, ' ')
	a.Jcc(x64.NE, start)
	a.AddImm(x64.RSI, 2)
	a.Jmp(spc)

	// 一个 token 从这儿起：整理缓冲清零，引号状态关掉
	a.Mark(start)
	b.put(xQ, x64.RSI)
	a.MovImm(x64.RAX, 0)
	b.put(xWL, x64.RAX)
	b.put(xQuo, x64.RAX)

	a.Mark(body)
	b.get(x64.RSI, xQ)
	a.MovZX16(x64.R8, x64.RSI, 0)
	a.Test(x64.R8, x64.R8)
	a.Jcc(x64.E, tail)
	a.CmpImm(x64.R8, ' ')
	a.Jcc(x64.NE, notSpace)
	b.get(x64.RAX, xQuo)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, tail) // 引号外面的空格就是分界
	a.Jmp(lit)

	a.Mark(notSpace)
	a.CmpImm(x64.R8, '"')
	a.Jcc(x64.NE, notQuote)
	b.get(x64.RAX, xQuo)
	a.MovImm(x64.R9, 1)
	a.Xor(x64.RAX, x64.R9)
	b.put(xQuo, x64.RAX)
	b.get(x64.RSI, xQ)
	a.AddImm(x64.RSI, 2)
	b.put(xQ, x64.RSI)
	a.Jmp(body)

	a.Mark(notQuote)
	a.CmpImm(x64.R8, '\\')
	a.Jcc(x64.NE, lit)
	// 数一连串反斜杠，顺便把后面那个字符留在 RAX
	a.MovImm(x64.RAX, 0)
	b.put(xN, x64.RAX)
	b.get(x64.RDI, xQ)
	a.Mark(bsCnt)
	a.MovZX16(x64.RAX, x64.RDI, 0)
	a.CmpImm(x64.RAX, '\\')
	a.Jcc(x64.NE, bsPeek)
	b.get(x64.RCX, xN)
	a.AddImm(x64.RCX, 1)
	b.put(xN, x64.RCX)
	a.AddImm(x64.RDI, 2)
	a.Jmp(bsCnt)

	a.Mark(bsPeek)
	a.CmpImm(x64.RAX, '"')
	a.Jcc(x64.NE, bsPlain)
	// 后面真是引号：对出一半反斜杠
	b.get(x64.RAX, xN)
	a.MovImm(x64.R9, 2)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R9)
	b.put(xK, x64.RAX)
	a.MovImm(x64.R8, '\\')
	a.Mark(halfLoop)
	b.get(x64.RAX, xK)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, halfDone)
	putWide()
	b.get(x64.RAX, xK)
	a.AddImm(x64.RAX, -1)
	b.put(xK, x64.RAX)
	a.Jmp(halfLoop)
	a.Mark(halfDone)
	// 奇数：那个引号是字面引号，吃掉它；偶数：留给下一轮去开关引号
	b.get(x64.RAX, xN)
	a.AndImm(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, evenRun)
	a.MovImm(x64.R8, '"')
	putWide()
	b.get(x64.RSI, xQ)
	b.get(x64.RAX, xN)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RSI, x64.RAX)
	a.AddImm(x64.RSI, 2)
	b.put(xQ, x64.RSI)
	a.Jmp(body)
	a.Mark(evenRun)
	b.get(x64.RSI, xQ)
	b.get(x64.RAX, xN)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RSI, x64.RAX)
	b.put(xQ, x64.RSI)
	a.Jmp(body)

	// 后面不是引号：这一串反斜杠全是字面的
	a.Mark(bsPlain)
	b.get(x64.RAX, xN)
	b.put(xK, x64.RAX)
	a.MovImm(x64.R8, '\\')
	a.Mark(plainLoop)
	b.get(x64.RAX, xK)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, plainDone)
	putWide()
	b.get(x64.RAX, xK)
	a.AddImm(x64.RAX, -1)
	b.put(xK, x64.RAX)
	a.Jmp(plainLoop)
	a.Mark(plainDone)
	b.get(x64.RSI, xQ)
	b.get(x64.RAX, xN)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RSI, x64.RAX)
	b.put(xQ, x64.RSI)
	a.Jmp(body)

	a.Mark(lit)
	putWide()
	b.get(x64.RSI, xQ)
	a.AddImm(x64.RSI, 2)
	b.put(xQ, x64.RSI)
	a.Jmp(body)

	// token 收完：整串翻成 UTF-8，造文本、添进list，主游标推到分界那个位置
	a.Mark(tail)
	b.get(x64.RSI, xQ)
	b.put(xP, x64.RSI)
	skip := b.label()
	b.get(x64.RAX, xSeen)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, skip) // 第一个 token 是脚本名自己，不收进list
	a.MovImm(x64.RCX, cpUTF8)
	a.Xor(x64.RDX, x64.RDX)
	b.leaRIP(x64.R8, "aWide")
	b.get(x64.R9, xWL)
	b.leaRIP(x64.RAX, "aBuf")
	a.AddImm(x64.RAX, strHead)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, lineCap)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	a.MovStore(x64.RBP, arg8, x64.RAX)
	b.impCall("WideCharToMultiByte")
	b.leaRIP(x64.RCX, "aBuf")
	a.Xor(x64.RDX, x64.RDX)
	a.Mov(x64.R8, x64.RAX)
	b.call("@str_sub")
	b.get(x64.RCX, xOut)
	a.Mov(x64.RDX, x64.RAX)
	b.call("@list_add")
	a.Mark(skip)
	a.MovImm(x64.RAX, 1)
	b.put(xSeen, x64.RAX)
	a.Jmp(loop)

	a.Mark(quit)
	b.get(x64.RAX, xOut)
	b.epilogue()
}

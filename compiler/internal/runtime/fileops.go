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

// fileops.go 是 M2b 那七条文件能力的运行期那一段：属性三条、目录两条、分块两条。
//
// 前提：产物只向 kernel32 要东西，这里叫到的每个名字都得先写进 runtime.go 的 imports。
// 两条规则：位置一律按**字节**数，但读回来的那一段
// 末尾不允许切半一个码点；"属性拿不到"与"那是目录"分占 10045 与 10043。
//
// en: fileops.go is the runtime half of M2b's seven file capabilities: three attribute members,
// two directory members, two ranged reads. The premise: the product asks only
// kernel32, so every name called here must first be written into runtime.go's imports. Two
// rules: positions are always counted in BYTES, but a slice read must not cut a code point in
// half at its end; "attribute
// unavailable" and "that is a directory" take 10045 and 10043 separately.
package runtime

import "neflang.org/nf/internal/x64"

const (
	// FindFirstFileW 摆回的那块 WIN32_FIND_DATAW 里的偏移（整块 592 字节）。
	ffdAttr    = 0  // 属性（四个字节）
	ffdWriteLo = 20 // 最后写入时间：FILETIME 是 8 个字节，低的那四个在前
	ffdWriteHi = 24
	ffdSizeHi  = 28 // 大小：高四个字节在前、低四个字节在后
	ffdSizeLo  = 32
	ffdName    = 44 // 名字，260 个宽字符

	attrDir      = 0x10
	attrReadOnly = 1

	// FILETIME 从 1601-01-01 起算，`time.now()` 从 1970-01-01 起算：
	// 中间差着一百一十四年的 100 纳秒，除回来就是同一时间基准上的秒。
	epochShift = int64(116444736000000000)
	hundredNS  = int64(10000000)

	moveBegin = 0 // SetFilePointerEx 的"从头数"

	// 一次翻译搬得出去的字节上界：路径长到装不下就算本机用不了（10051 号那一句）。
	cvCap = 16384

	codeNoAttrs = 10045 // 属性拿不到：那个路径不在
	codeIsDir   = 10043 // 目录打不开，或拿目录去问文件的属性
)

// fileOpsData 摆这一段的静态缓冲：查找回来的那一块、枚举用的模式串、属性三条的落点、
// 一次翻译的临时文本，还有查到的那一个句柄。
//
// en: fileOpsData lays out this section's static buffers: the block found by a search, the
// pattern string for enumeration, the landing slots of the three attributes, the temporary text
// of one translation, and the handle that was looked up.
func fileOpsData(put func(name string, bs []byte)) {
	put("findBuf", make([]byte, 592))
	put("enumPat", make([]byte, (wideCap+272)*2))
	put("findH", make([]byte, 8))
	put("gSize", make([]byte, 8))
	put("gTime", make([]byte, 8))
	put("gRo", make([]byte, 8))
	put("cvBuf", make([]byte, strHead+cvCap))
}

// @file_attrs(rcx=路径文本)：把大小、最后写入时间（秒）、只读标志摆进那三个静态槽。
//
// 三条属性都走这一条，所以"不在"与"那是目录"两条答复只写一遍：找不到是 10045 号，
// 是目录是 10043 号，路径本身摆不下是 10051 号。那三个槽是一次性的：
// 叫完这条马上取，中间再叫一次就换一份——三个入口各自都是"叫完就取"。
//
// en: @file_attrs (rcx = path text): places size, last write time (seconds) and the read-only
// flag into the three static slots. All three attribute members go through this one, so the
// "not there" and "that is a directory" answers are written once: missing is code 10045, a
// directory is 10043, and a path too long to fit is 10051. The three slots are single
// use: take them immediately after this call, because calling again replaces them - all three
// entry points are "call then take".
func fileAttrs(b *rt) {
	const xPath = 0
	var (
		badPath = b.label()
		noAttrs = b.label()
		isDir   = b.label()
	)
	a := b.a
	b.prologue()
	b.put(xPath, x64.RCX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, xPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "wPath")
	b.leaRIP(x64.RDX, "findBuf")
	b.impCall("FindFirstFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, noAttrs)
	b.storeRIP(x64.RAX, "findH")

	b.leaRIP(x64.RSI, "findBuf")
	a.MovZX32(x64.RAX, x64.RSI, ffdAttr)
	a.AndImm(x64.RAX, attrDir)
	a.Jcc(x64.NE, isDir)

	a.MovZX32(x64.RDX, x64.RSI, ffdSizeHi)
	a.MovZX32(x64.RAX, x64.RSI, ffdSizeLo)
	a.ShlImm(x64.RDX, 32)
	a.Or(x64.RAX, x64.RDX)
	b.storeRIP(x64.RAX, "gSize")

	// 时间那一步用有符号除：1970 年之前的文件真的存在，商要往负的那一侧走。
	// 除数不能放在 RDX——`CQO` 正是拿 RDX 当被除数高位的，除数挪到 RCX 去。
	a.MovZX32(x64.RDX, x64.RSI, ffdWriteHi)
	a.MovZX32(x64.RAX, x64.RSI, ffdWriteLo)
	a.ShlImm(x64.RDX, 32)
	a.Or(x64.RAX, x64.RDX)
	a.MovImm(x64.RDX, epochShift)
	a.Sub(x64.RAX, x64.RDX)
	a.MovImm(x64.RCX, hundredNS)
	a.Cqo()
	a.Idiv(x64.RCX)
	b.storeRIP(x64.RAX, "gTime")

	b.leaRIP(x64.RSI, "findBuf")
	a.MovZX32(x64.RAX, x64.RSI, ffdAttr)
	a.AndImm(x64.RAX, attrReadOnly)
	b.storeRIP(x64.RAX, "gRo")

	b.loadRIP(x64.RCX, "findH")
	b.impCall("FindClose")
	b.epilogue()

	a.Mark(noAttrs)
	b.throwWith(codeNoAttrs, "sNoAttrs")
	a.Mark(isDir)
	b.loadRIP(x64.RCX, "findH")
	b.impCall("FindClose")
	b.throwWith(codeIsDir, "sIsDir")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// attrOnce 是属性那三条的共同形状：把路径交给 @file_attrs，再从那三个槽里取自己要的那一个。
//
// en: attrOnce is the shared shape of the three attribute members: hand the path to
// @file_attrs, then take the one slot each wants out of the three.
func attrOnce(b *rt, slot string) {
	b.prologue()
	b.put(0, x64.RCX)
	b.get(x64.RCX, 0)
	b.call("@file_attrs")
	b.loadRIP(x64.RAX, slot)
	b.epilogue()
}

// @file_size(rcx=路径文本)：返回 rax=那个文件的字节数。
//
// en: @file_size (rcx = path text): returns rax = that file's byte count.
func fileSize(b *rt) { attrOnce(b, "gSize") }

// @file_modified(rcx=路径文本)：返回 rax=最后写入的时间，从 1970-01-01 起算的秒（与 time.now() 同一基准）。
//
// en: @file_modified (rcx = path text): returns rax = the last write time in seconds since
// 1970-01-01 (the same ruler as time.now()).
func fileModified(b *rt) { attrOnce(b, "gTime") }

// @file_readonly(rcx=路径文本)：返回 rax=1 是只读的，0 不是。
//
// en: @file_readonly (rcx = path text): returns rax = 1 when read-only, 0 when not.
func fileReadonly(b *rt) { attrOnce(b, "gRo") }

// @file_list(rcx=目录文本, rdx=1 要全路径、0 只要名字)：返回 rax=list<string>。
//
// 点目录（`.` 与 `..`）不进名单，顺序按系统返回来的那个。目录打不开是 10043 号。
// 模式串是"目录＋分隔符＋ `*`"，目录自己尾巴上已经有分隔符就不再补一个（与 path.join 那句
// "缺一个才补一个"同一规则）。要全路径时把名字抄到那一个分隔符后面、盖掉 `*`，整串一起翻。
//
// en: @file_list (rcx = directory text, rdx = 1 full paths / 0 names only): returns rax =
// list<string>. The dot entries (`.` and `..`) are left out, and the order is whatever the
// system returns. A directory that will not open is code 10043. The pattern is
// "directory + separator + `*`", and no separator is added when the directory already ends with
// one (the same "add one only if missing" rule as path.join). When full paths are wanted, the
// name is copied after that separator over the `*` and the whole string is translated together.
func fileList(b *rt) {
	const (
		xDir   = 0 // 目录那一段文本
		xFlag  = 1 // 要全路径吗
		xOut   = 2 // 生成的 list
		xDLen  = 3 // 目录翻成宽字符后有几个
		xSep   = 4 // 名字从第几个宽字符起（`*` 摆在同一格上）
		xSrc   = 5 // 这一条要翻的那串宽字符在哪
		xBytes = 6 // 翻出来几个字节
	)
	var (
		badPath   = b.label()
		backCheck = b.label()
		slashEnd  = b.label()
		keepSep   = b.label()
		putSep    = b.label()
		gotSep    = b.label()
		noDir     = b.label()
		entry     = b.label()
		body      = b.label()
		full      = b.label()
		cp        = b.label()
		cpd       = b.label()
		translate = b.label()
		nxt       = b.label()
	)
	a := b.a
	b.prologue()
	b.put(xDir, x64.RCX)
	b.put(xFlag, x64.RDX)

	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, tagString)
	b.call("@list_new")
	b.put(xOut, x64.RAX)

	b.leaRIP(x64.RDX, "enumPat")
	b.get(x64.RCX, xDir)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)
	// @to_wide 返回的是**字节**数（末尾那两个 0 就是按字节挪的位置），这里要的是宽字符个数：折一半。
	a.ShrImm(x64.RAX, 1)
	b.put(xDLen, x64.RAX)

	// RDI 停在目录那一段之后的一个位置。尾巴上已经是 `\` 或 `/`：那个位置就是分隔符，
	// 名字接在 RDI 当下这个位置上；两头都不是：先在这个位置补一个 `\`，RDI 往后挪一个位置再放名字。
	// xSep 记的是"名字从第几个宽字符起"，模式串里那个 `*` 也摆在同一个位置上。
	b.leaRIP(x64.RDI, "enumPat")
	b.get(x64.RAX, xDLen)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX16(x64.RCX, x64.RDI, -2)
	a.CmpImm(x64.RCX, '\\')
	a.Jcc(x64.NE, backCheck)
	b.get(x64.RAX, xDLen)
	a.Jmp(keepSep)
	a.Mark(backCheck)
	a.CmpImm(x64.RCX, '/')
	a.Jcc(x64.E, slashEnd)
	// 补那一个分隔符：`\` ＋ 一个 0，RDI 往后挪一个位置，名字从挪过之后的那个位置起。
	a.MovImm(x64.RCX, '\\')
	a.MovStore8(x64.RDI, 0, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore8(x64.RDI, 1, x64.RCX)
	a.AddImm(x64.RDI, 2)
	b.get(x64.RAX, xDLen)
	a.AddImm(x64.RAX, 1)
	a.Jmp(putSep)
	a.Mark(slashEnd)
	b.get(x64.RAX, xDLen)
	a.Mark(keepSep)
	b.put(xSep, x64.RAX)
	a.Jmp(gotSep)
	a.Mark(putSep)
	b.put(xSep, x64.RAX)
	a.Mark(gotSep)

	// 模式串的最后一个位置摆一个 `*`：第一次查找按它枚举，全路径那一支后面用名字把它盖掉。
	// `*` 后面紧跟的那个 0 也要写：这块缓冲区是被上一轮的长名字用过的，
	// 不写终止符就成了"目录＋`*`＋上一头那个名字的尾巴"，枚举出来的字符串是否正确就成了偶然。
	a.MovImm(x64.RCX, '*')
	a.MovStore8(x64.RDI, 0, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore8(x64.RDI, 1, x64.RCX)
	a.MovStore8(x64.RDI, 2, x64.RCX)
	a.MovStore8(x64.RDI, 3, x64.RCX)

	b.leaRIP(x64.RCX, "enumPat")
	b.leaRIP(x64.RDX, "findBuf")
	b.impCall("FindFirstFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, noDir)
	b.storeRIP(x64.RAX, "findH")

	a.Mark(entry)
	b.leaRIP(x64.RSI, "findBuf")
	a.AddImm(x64.RSI, ffdName)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, '.')
	a.Jcc(x64.NE, body)
	a.MovZX16(x64.RAX, x64.RSI, 2)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt) // 就是 "."
	a.CmpImm(x64.RAX, '.')
	a.Jcc(x64.NE, body)
	a.MovZX16(x64.RAX, x64.RSI, 4)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt) // 就是 ".."
	a.Mark(body)

	// 只要名字就直接翻 findBuf 里那一串；要全路径就先把名字抄到 enumPat 的分隔符后面。
	// flag 是 0 才走名字那一侧，所以"不相等才跳"——Test 立的正是"等于零"那个标志。
	b.get(x64.RAX, xFlag)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, full)
	b.leaRIP(x64.RAX, "findBuf")
	a.AddImm(x64.RAX, ffdName)
	b.put(xSrc, x64.RAX)
	a.Jmp(translate)
	a.Mark(full)

	// 把名字抄到分隔符后面（盖掉那个 `*`），末尾补一个 0：整串就是"目录＋名字"。
	b.get(x64.RAX, xSep)
	a.ShlImm(x64.RAX, 1)
	b.leaRIP(x64.RDI, "enumPat")
	a.Add(x64.RDI, x64.RAX)
	b.leaRIP(x64.RSI, "findBuf")
	a.AddImm(x64.RSI, ffdName)
	a.Mark(cp)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, cpd)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.Mov(x64.RCX, x64.RAX)
	a.ShrImm(x64.RCX, 8)
	a.MovStore8(x64.RDI, 1, x64.RCX)
	a.AddImm(x64.RDI, 2)
	a.AddImm(x64.RSI, 2)
	a.Jmp(cp)
	a.Mark(cpd)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.MovStore8(x64.RDI, 1, x64.RAX)
	b.leaRIP(x64.RAX, "enumPat")
	b.put(xSrc, x64.RAX)

	// 宽字符翻成 UTF-8 进 cvBuf：给 -1 就是"到第一个 0 为止"，返回的字节数带着那个 0，减掉。
	// 翻不出来（长到装不下）就跳过这一条，不算整个目录打不开。
	a.Mark(translate)
	a.MovImm(x64.RCX, cpUTF8)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, xSrc)
	a.MovImm(x64.R9, -1)
	b.leaRIP(x64.RAX, "cvBuf")
	a.AddImm(x64.RAX, strHead)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, cvCap)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	a.MovStore(x64.RBP, arg8, x64.RAX)
	b.impCall("WideCharToMultiByte")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt)
	a.AddImm(x64.RAX, -1)
	b.put(xBytes, x64.RAX)

	b.leaRIP(x64.RCX, "cvBuf")
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, xBytes)
	b.call("@str_sub")
	b.get(x64.RCX, xOut)
	a.Mov(x64.RDX, x64.RAX)
	b.call("@list_add")

	a.Mark(nxt)
	b.loadRIP(x64.RCX, "findH")
	b.leaRIP(x64.RDX, "findBuf")
	b.impCall("FindNextFileW")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, entry)

	b.loadRIP(x64.RCX, "findH")
	b.impCall("FindClose")
	b.get(x64.RAX, xOut)
	b.epilogue()

	a.Mark(noDir)
	b.throwWith(codeIsDir, "sNoDir")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_read_from(rcx=路径文本, rdx=从第几个字节起, r8=要几个字节)：返回 rax=那一段文本。
//
// 位置按字节数，可返回来那一段的末尾不允许切半一个码点——从尾巴上最多退三个字节位置，
// 遇到一个码点的首字节就按它该带的续字节数比一次，不够就从这个首字节起一并退掉。
// 越过文件末尾就是空文本，不另开号；要 0 个、或者给的是负数也是空文本，连文件都不开。
//
// en: @file_read_from (rcx = path text, rdx = byte offset, r8 = byte count): returns rax = that
// slice as text. Positions are counted in bytes, but the end of what comes back may
// not cut a code point in half - step back at most three byte positions from the tail, and
// when a lead byte is hit compare against the continuation count it demands, backing off past
// that position too if they are missing. Past the end of the file is empty text with no new
// code; asking for 0 bytes, or passing a negative, is empty text too and does not even open
// the file.
func fileReadFrom(b *rt) {
	const (
		xPath   = 0
		xStart  = 1
		xCount  = 2
		xObj    = 3 // 读进来的那一块，比最后交出去的多一些（交出去的那一份是精确尺寸）
		xGot    = 4 // 已经读到几个字节
		xKeep   = 5 // 退过之后的长度；ReadFile 的"读到了几个"也落在这一格
		xHandle = 6
		xRes    = 7 // 现造的那一份交回给调用方，得留住——@free 要用 RAX
	)
	var (
		none     = b.label()
		badPath  = b.label()
		noFile   = b.label()
		top      = b.label()
		readDone = b.label()
		scan     = b.label()
		lead     = b.label()
		need1    = b.label()
		need2    = b.label()
		need3    = b.label()
		need4    = b.label()
		check    = b.label()
		trim     = b.label()
		fin      = b.label()
		empty    = b.label()
		pick     = b.label()
	)
	a := b.a
	b.prologue()
	b.put(xPath, x64.RCX)
	b.put(xStart, x64.RDX)
	b.put(xCount, x64.R8)

	b.get(x64.RAX, xCount)
	a.CmpImm(x64.RAX, 1)
	a.Jcc(x64.L, none)

	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, xPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

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
	b.get(x64.RDX, xStart)
	a.Xor(x64.R8, x64.R8)
	a.MovImm(x64.R9, moveBegin)
	b.impCall("SetFilePointerEx")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noFile)

	b.get(x64.RCX, xCount)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(xObj, x64.RAX)
	a.MovImm(x64.RAX, 0)
	b.put(xGot, x64.RAX)

	// 一轮一轮地读：ReadFile 只报告"这一次给了几个"，对文件一般一次到底，但不保证。
	a.Mark(top)
	b.get(x64.RCX, xHandle)
	b.get(x64.RDX, xObj)
	a.AddImm(x64.RDX, strHead)
	b.get(x64.RAX, xGot)
	a.Add(x64.RDX, x64.RAX)
	b.get(x64.R8, xCount)
	a.Sub(x64.R8, x64.RAX)
	a.Jcc(x64.E, readDone) // 已经读满
	a.LeaMem(x64.R9, x64.RBP, -8*(xKeep+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCall("ReadFile")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, readDone)
	a.MovZX32(x64.RAX, x64.RBP, -8*(xKeep+1))
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, readDone) // 一个也没读到：已到文件末尾
	b.get(x64.RDX, xGot)
	a.Add(x64.RDX, x64.RAX)
	b.put(xGot, x64.RDX)
	a.Jmp(top)
	a.Mark(readDone)

	b.get(x64.RCX, xHandle)
	b.impCall("CloseHandle")

	// 从尾巴上退：R10 是段尾（固定不动），RDI 是"正在看的那一格之后"的游标（一个位置一个位置往回找首字节），
	// RSI 是内容起点，R11 是最多看四格。找到首字节后按它该带的格数与"它到段尾实际有几格"比一次：
	// 不够就把从这个首字节起的那一截全退掉，够就整段留下。
	b.get(x64.RSI, xObj)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDI, xGot)
	a.Add(x64.RDI, x64.RSI)
	a.Mov(x64.R10, x64.RDI)
	a.MovImm(x64.R11, 4)
	a.Mark(scan)
	a.Cmp(x64.RDI, x64.RSI)
	a.Jcc(x64.BE, empty)
	a.MovZX8(x64.RAX, x64.RDI, -1)
	a.Mov(x64.RCX, x64.RAX)
	a.AndImm(x64.RCX, 0xC0)
	a.CmpImm(x64.RCX, 0x80)
	a.Jcc(x64.NE, lead)
	a.AddImm(x64.RDI, -1) // 续字节：往前再看一格
	a.AddImm(x64.R11, -1)
	a.Jcc(x64.NE, scan)
	a.Jmp(fin)

	// 首字节：它自己往后要几格才算一个完整的码点；四字节之外的写法按"够用"放过。
	a.Mark(lead)
	a.CmpImm(x64.RAX, 0x80)
	a.Jcc(x64.B, need1)
	a.Mov(x64.RCX, x64.RAX)
	a.AndImm(x64.RCX, 0xE0)
	a.CmpImm(x64.RCX, 0xC0)
	a.Jcc(x64.E, need2)
	a.Mov(x64.RCX, x64.RAX)
	a.AndImm(x64.RCX, 0xF0)
	a.CmpImm(x64.RCX, 0xE0)
	a.Jcc(x64.E, need3)
	a.Mov(x64.RCX, x64.RAX)
	a.AndImm(x64.RCX, 0xF8)
	a.CmpImm(x64.RCX, 0xF0)
	a.Jcc(x64.E, need4)
	a.Jmp(need1)
	a.Mark(need4)
	a.MovImm(x64.RCX, 4)
	a.Jmp(check)
	a.Mark(need3)
	a.MovImm(x64.RCX, 3)
	a.Jmp(check)
	a.Mark(need2)
	a.MovImm(x64.RCX, 2)
	a.Jmp(check)
	a.Mark(need1)
	a.MovImm(x64.RCX, 1)
	a.Mark(check)
	// 实际有幾格：这个首字节（在 RDI-1）到段尾 R10 之间的距离。
	a.Mov(x64.RAX, x64.R10)
	a.Sub(x64.RAX, x64.RDI)
	a.AddImm(x64.RAX, 1)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.B, trim) // 凑不满一个码点：从这个首字节起全退掉
	a.Jmp(fin)
	a.Mark(trim)
	a.Mov(x64.R10, x64.RDI) // 段尾收到这个首字节之前
	a.AddImm(x64.R10, -1)
	a.Mark(fin)
	a.Mov(x64.RAX, x64.R10)
	a.Sub(x64.RAX, x64.RSI)
	b.put(xKeep, x64.RAX)
	a.Jmp(pick)
	a.Mark(empty)
	a.MovImm(x64.RAX, 0)
	b.put(xKeep, x64.RAX)

	// 交出去的那一份按退过的长度现造（精确尺寸）。造完先把它留住再还原始那一块——
	// `@free` 要用 RAX 算尺寸，返回给调用方的那一份不能被它盖掉。
	a.Mark(pick)
	b.get(x64.RCX, xObj)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, xKeep)
	b.call("@str_sub")
	b.put(xRes, x64.RAX)
	b.get(x64.RCX, xObj)
	b.call("@free")
	b.get(x64.RAX, xRes)
	b.epilogue()

	a.Mark(none)
	b.leaRIP(x64.RCX, "cvBuf")
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.call("@str_sub")
	b.epilogue()

	a.Mark(noFile)
	b.throwWith(codeNoFile, "sNoFile")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_write_at(rcx=路径文本, rdx=从第几个字节起, r8=要写出去的那一段)：返回 nothing。
//
// 同一规则的另一头：只开现有的文件，从头数到那一个字节，把这一段盖上去——后面的内容不动，
// 也不把文件截短。开不了、位置摆不下、写不满都是 10042 号那一句（与 file.write 同一号）。
//
// en: @file_write_at (rcx = path text, rdx = byte offset, r8 = the slice to write): returns
// nothing. The other side of the same rule: only an existing file is opened, counted to that
// byte from the start, and this slice is laid over it - the content behind is untouched and
// the file is not truncated. Not opening, an offset too far to store, and a short write all
// give the 10042 sentence (the same code as file.write).
func fileWriteAt(b *rt) {
	const (
		xPath   = 0
		xStart  = 1
		xText   = 2
		xGot    = 3 // WriteFile 的"写出去几个"
		xHandle = 4
	)
	var (
		badPath = b.label()
		noFile  = b.label()
	)
	a := b.a
	b.prologue()
	b.put(xPath, x64.RCX)
	b.put(xStart, x64.RDX)
	b.put(xText, x64.R8)

	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, xPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "wPath")
	a.MovImm(x64.RDX, genericWrite)
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
	b.get(x64.RDX, xStart)
	a.Xor(x64.R8, x64.R8)
	a.MovImm(x64.R9, moveBegin)
	b.impCall("SetFilePointerEx")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noFile)

	b.get(x64.RCX, xHandle)
	b.get(x64.RDX, xText)
	a.AddImm(x64.RDX, strHead)
	b.get(x64.R8, xText)
	a.MovLoad(x64.R8, x64.R8, 0) // 文本的头一格是字节数
	a.LeaMem(x64.R9, x64.RBP, -8*(xGot+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCall("WriteFile")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noFile)
	a.MovZX32(x64.RAX, x64.RBP, -8*(xGot+1))
	b.get(x64.RDX, xText)
	a.MovLoad(x64.RDX, x64.RDX, 0)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, noFile)

	b.get(x64.RCX, xHandle)
	b.impCall("CloseHandle")
	b.epilogue()

	a.Mark(noFile)
	b.throwWith(codeWriteFail, "sNoWrite")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// 目录那四条要的号。建不成与改不了各开一号，删空目录这一问回到 10041 号那一档，
// 只是句子换成目录——与 file.remove 那一条"号数保持不变、措辞跟着现场走"同一个做法。
//
// en: The codes the four directory members need. Failing to create and failing to
// rename each get a new code, while removing a non-empty directory returns to the 10041 band
// with only the sentence changed to talk about a directory - the same approach as file.remove,
// "the code stays fixed, the wording follows the situation".
const (
	codeMkDir  = 10046 // 建不出这一层目录
	codeRename = 10047 // 这个名字改不了
	// ERROR_ALREADY_EXISTS：CreateDirectoryW 说"不行"之后要问一句是不是已经有了
	errAlreadyExists = 183
)

// @file_mkdir(rcx=路径文本)：返回 rax=1 是这一趟新建出来的，0 是本来就在。
//
// 只建这一层：上头那一层没有就抛 10046 号，不推测"是不是想连上头一起建"。
// 已经有一个同名的文件在那儿也算冲突，同样抛 10046 号——这一条不覆盖已有的名字。
//
// en: @file_mkdir (rcx = path text): returns rax = 1 when this call created it, 0 when it was
// already there. Only one level is created: a missing parent throws code 10046 rather
// than guessing "maybe you meant to build the parents too". An existing file of the same name
// also counts as a clash and throws 10046 - this routine does not overwrite an existing name.
func fileMkdir(b *rt) {
	const kPath = 0
	a := b.a
	b.prologue()
	b.put(kPath, x64.RCX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, kPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	cant := b.label()
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "wPath")
	a.Xor(x64.RDX, x64.RDX) // 安全属性给空：这一版没有"给目录带权限"那一问
	b.impCall("CreateDirectoryW")
	a.Test(x64.RAX, x64.RAX)
	made := b.label()
	a.Jcc(x64.NE, made)
	b.impCall("GetLastError")
	a.CmpImm(x64.RAX, errAlreadyExists)
	there := b.label()
	a.Jcc(x64.E, there)
	a.Jmp(cant)
	a.Mark(made)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
	a.Mark(there)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()

	a.Mark(cant)
	b.throwWith(codeMkDir, "sMkDir")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_is_dir(rcx=路径文本)：返回 rax=1 是目录、0 不是（那条路径根本不在也是 0，不抛）。
//
// 走一次 FindFirstFileW 就够：属性那个位置里 bit 4 立着就是目录。查完马上 FindClose，
// 不留句柄——这一条是只回答是或否的查询，与 file.exists 同一个形状。
//
// en: @file_is_dir (rcx = path text): returns rax = 1 for a directory, 0 otherwise (a path that
// does not exist is also 0, and nothing is thrown). One FindFirstFileW is enough: bit 4
// set in the attribute field means a directory. FindClose follows immediately and leaves no
// handle - this is a yes-or-no query, the same shape as file.exists.
func fileIsDir(b *rt) {
	const jPath = 0
	a := b.a
	b.prologue()
	b.put(jPath, x64.RCX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, jPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "wPath")
	b.leaRIP(x64.RDX, "findBuf")
	b.impCall("FindFirstFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	notThere := b.label()
	a.Jcc(x64.E, notThere)
	b.storeRIP(x64.RAX, "findH")

	b.leaRIP(x64.RSI, "findBuf")
	a.MovZX32(x64.RAX, x64.RSI, ffdAttr)
	a.AndImm(x64.RAX, attrDir)
	b.put(1, x64.RAX)
	b.loadRIP(x64.RCX, "findH")
	b.impCall("FindClose")
	b.get(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	yes := b.label()
	a.Jcc(x64.NE, yes)
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(yes)
	b.epilogue()

	a.Mark(notThere)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_rename(rcx=旧路径, rdx=新路径)：改名或挪到同一台机器上的别处。
//
// 两条路径都先翻成本机写法：旧的进 wPath、新的进 aWide，MoveFileW 就收这两个静态缓冲。
// 改不成抛 10047 号：新名字已经存在、旧路径不在、跨卷挪不动，都是这一号。
//
// en: @file_rename (rcx = old path, rdx = new path): rename, or move elsewhere on the same
// machine. Both paths are translated to the local spelling first: the old into wPath,
// the new into aWide, and MoveFileW takes those two static buffers. Failure throws code 10047:
// the new name is taken, the old path is missing, or the move crosses a volume - all of them
// this one code.
func fileRename(b *rt) {
	const (
		oOld = 0
		oNew = 1
	)
	a := b.a
	b.prologue()
	b.put(oOld, x64.RCX)
	b.put(oNew, x64.RDX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, oOld)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)
	b.leaRIP(x64.RDX, "aWide")
	b.get(x64.RCX, oNew)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "wPath")
	b.leaRIP(x64.RDX, "aWide")
	b.impCall("MoveFileW")
	a.Test(x64.RAX, x64.RAX)
	cant := b.label()
	a.Jcc(x64.E, cant)
	b.epilogue()

	a.Mark(cant)
	b.throwWith(codeRename, "sNoRename")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_rmdir(rcx=路径文本)：删掉一个空目录。里头还有东西就抛 10041 号那一句
// "删不掉这个目录"——这一版不往下钻，也就没有"连带着把里面的都删掉"那种事。
//
// en: @file_rmdir (rcx = path text): deletes an empty directory. Anything left inside
// throws the sentence "cannot delete this directory", code 10041 - this version does not
// descend, so there is no "and everything below it".
func fileRemoveDir(b *rt) {
	const rPath = 0
	a := b.a
	b.prologue()
	b.put(rPath, x64.RCX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, rPath)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)
	b.leaRIP(x64.RCX, "wPath")
	b.impCall("RemoveDirectoryW")
	a.Test(x64.RAX, x64.RAX)
	cant := b.label()
	a.Jcc(x64.E, cant)
	b.epilogue()

	a.Mark(cant)
	b.throwWith(codeNoFile, "sNoRmDir")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_copy(rcx=从哪儿, rdx=到哪儿)：整份抄一遍。
//
// 目标在那儿就覆盖掉，与 `file.write` 同一个处置；不抛"已经存在"这一问。
// 答复分两头：源那一条路径开不了是 10041，目标写不进去是 10042，两条路径本身
// 递不出去（转不成宽字符）是 10051，跟其余几条一样。
//
// 失败之后再查一次属性的理由：`CopyFileW` 只报"没成"，不说卡在哪一头，
// 而这句报错要告诉用户的正是"是读不到还是写不进"。
//
// en: @file_copy (rcx = source, rdx = destination): copy the whole file. An
// existing destination is overwritten, the same treatment as `file.write`; no "already exists"
// question is thrown. The answer splits in two: the source path that cannot be opened is 10041,
// the destination that cannot be written is 10042, and a path that cannot be translated to wide
// characters is 10051, like the other members. The attributes are consulted again after a
// failure because CopyFileW only reports failure, not which end broke, and the message must
// say exactly whether it was unreadable or unwritable.
func fileCopy(b *rt) {
	const (
		cFrom = 0
		cTo   = 1
	)
	a := b.a
	b.prologue()
	b.put(cFrom, x64.RCX)
	b.put(cTo, x64.RDX)
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, cFrom)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)
	b.leaRIP(x64.RDX, "aWide")
	b.get(x64.RCX, cTo)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "wPath")
	b.leaRIP(x64.RDX, "aWide")
	a.Xor(x64.R8, x64.R8) // bFailIfExists＝0：目标在就盖掉
	b.impCall("CopyFileW")
	a.Test(x64.RAX, x64.RAX)
	cant := b.label()
	noSrc := b.label()
	a.Jcc(x64.E, cant)
	b.epilogue()

	a.Mark(cant)
	// 源路径读不到就报"读不到"，否则算"写不进"。
	// 这里先按 32 位收一次再比：GetFileAttributesW 返回的是 DWORD，
	// INVALID_HANDLE_VALUE 在那一侧是 0xFFFFFFFF，直接拿 64 位的 -1 比就对不上。
	b.leaRIP(x64.RCX, "wPath")
	b.impCall("GetFileAttributesW")
	a.MovStore32(x64.RBP, -24, x64.RAX)
	a.MovZX32(x64.RAX, x64.RBP, -24)
	a.MovImm(x64.RCX, 0xFFFFFFFF)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.E, noSrc)
	b.throwWith(codeWriteFail, "sNoWrite")
	a.Mark(noSrc)
	b.throwWith(codeNoFile, "sNoFile")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_mkdirs(rcx=路径文本)：一层一层往下建，返回 rax=1 这条路径现在是目录、0 建不出来。
//
// 与单层那一条的区别在回答的问题：那一条问"我有没有新建成一层"（已经有了算假），
// 这一条问"这条路径上的目录齐了没有"（本来就齐的算真）。
//
// 只在 kernel32 里做：shell32 那个一次建齐的例程要连进别的组件，而产物只向 kernel32
// 要东西。做法是把宽字符路径按分隔符一段一段截出来建，遇到"已经有了"就当过去，
// 遇到别的错（上一截是个文件、权限不够）就返回 0，不抛——这一条与 `file.create_directory`
// 同一形状：返回真／假，让写文件那一步去报它自己的号。
//
// 连续的分隔符与开头那一个都跳过：`a//b` 里那一段空的、`/tmp` 最前面那一截，都不是要建的一层。
//
// en: @file_mkdirs (rcx = path text): create level by level downward, returning rax = 1 when
// this path is now a directory, 0 when it could not be built. The difference
// from the single-level routine is the question it answers: that one asks "did I just create a
// level" (already present counts as false), this one asks "are the directories along this path
// in place now" (already complete counts as true). It is done inside kernel32 only: shell32's
// build-everything routine would mean linking another component, and the product asks kernel32
// only. The method is to cut the wide-character path at separators one segment at a
// time and create each, treating "already exists" as passed and returning 0 - not throwing - on
// any other error (a segment that is a file, insufficient permission): this member has the same
// shape as file.create_directory, returning true/false and letting the write step report its
// own code. Runs of separators and a leading one are skipped: the empty segment in `a//b` and
// the one before `/tmp` are not levels to create.
func fileMkdirs(b *rt) {
	const (
		mI    = 0 // 走到整条路径的第几个字节
		mPrev = 1 // 上一格是不是分隔符（开头先记成"是"，最前面那一个就不建空段）
		mCur  = 2 // 抄这一段时的游标
	)
	a := b.a
	b.prologue()
	b.put(mI, x64.RCX) // 路径文本先留在这一格，转宽字符要用它
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, mI)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)

	loop := b.label()
	sep := b.label()
	notSep := b.label()
	buildSeg := b.label()
	copyTop := b.label()
	copyDone := b.label()
	segOk := b.label()
	advance := b.label()
	tail := b.label()
	yes := b.label()
	noWay := b.label()

	a.Xor(x64.RAX, x64.RAX)
	b.put(mI, x64.RAX)
	a.MovImm(x64.RAX, 1)
	b.put(mPrev, x64.RAX)

	a.Mark(loop)
	b.leaRIP(x64.RSI, "wPath")
	b.get(x64.RAX, mI)
	a.Add(x64.RSI, x64.RAX)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, tail) // 整条走完了：最后那一截（就是全路径）单独建一次
	a.CmpImm(x64.RAX, '\\')
	a.Jcc(x64.E, sep)
	a.CmpImm(x64.RAX, '/')
	a.Jcc(x64.NE, notSep)
	a.Mark(sep)
	b.get(x64.RAX, mPrev)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, advance) // 连着两个分隔符、或者开头那一个：前面没有要建的一层
	// 上一个位置是冒号就是盘符那一截（"C:"），它不是一个要建的目录
	b.get(x64.RAX, mI)
	a.AddImm(x64.RAX, -2)
	b.leaRIP(x64.RSI, "wPath")
	a.Add(x64.RSI, x64.RAX)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, ':')
	a.Jcc(x64.E, advance)
	a.Jmp(buildSeg)

	a.Mark(notSep)
	a.Xor(x64.RAX, x64.RAX)
	b.put(mPrev, x64.RAX)
	a.Jmp(advance)

	// 把 [0, mI) 这一段抄进 aWide，尾巴补一个结束符，然后建它
	a.Mark(buildSeg)
	a.Xor(x64.RAX, x64.RAX)
	b.put(mCur, x64.RAX)
	a.Mark(copyTop)
	b.get(x64.RDX, mCur)
	b.get(x64.RAX, mI)
	a.Cmp(x64.RDX, x64.RAX)
	a.Jcc(x64.AE, copyDone)
	b.leaRIP(x64.RSI, "wPath")
	a.Add(x64.RSI, x64.RDX)
	a.MovZX16(x64.R9, x64.RSI, 0)
	b.leaRIP(x64.RDI, "aWide")
	a.Add(x64.RDI, x64.RDX)
	a.MovStore32(x64.RDI, 0, x64.R9) // 低两格是这一格字符，高两格顺手成了结束符（下一轮要么盖掉、要么正是尾巴）
	b.get(x64.RDX, mCur)
	a.AddImm(x64.RDX, 2)
	b.put(mCur, x64.RDX)
	a.Jmp(copyTop)

	a.Mark(copyDone)
	b.leaRIP(x64.RDI, "aWide")
	b.get(x64.RAX, mI)
	a.Add(x64.RDI, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore32(x64.RDI, 0, x64.RAX)
	b.leaRIP(x64.RCX, "aWide")
	a.Xor(x64.RDX, x64.RDX)
	b.impCall("CreateDirectoryW")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, segOk)
	b.impCall("GetLastError")
	a.CmpImm(x64.RAX, errAlreadyExists)
	a.Jcc(x64.NE, noWay) // 已经有了算过去；别的原因（上一截是文件、权限不够）就是建不出来
	a.Mark(segOk)
	a.MovImm(x64.RAX, 1)
	b.put(mPrev, x64.RAX)

	a.Mark(advance)
	b.get(x64.RAX, mI)
	a.AddImm(x64.RAX, 2)
	b.put(mI, x64.RAX)
	a.Jmp(loop)

	a.Mark(tail)
	b.leaRIP(x64.RCX, "wPath")
	a.Xor(x64.RDX, x64.RDX)
	b.impCall("CreateDirectoryW")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, yes)
	b.impCall("GetLastError")
	a.CmpImm(x64.RAX, errAlreadyExists)
	a.Jcc(x64.NE, noWay)
	a.Mark(yes)
	a.MovImm(x64.RAX, 1)
	b.epilogue()

	a.Mark(noWay)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()

	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_walk(rcx=根)：返回 rax=整棵目录树的全路径，装在一个 list 里。
//
// 一层一层往下钻，钻到的每一条都记进那个 list，目录本身也记——调用方要的是"这条路下面有什么"，
// 把目录挑出去反而得自己再问一遍是不是目录。list 里每一个位置都是一份新拷的文本，list 是它唯一的持有者。
//
// 答复与 file.list_paths 同两条：这一个目录打不开是 10043 号那一句，路长到摆不进宽字符是 10051 号那一句。
// 往下钻到第 64 层就停：链接点与挂载点能造出一条永远钻不完的环，这一条把深度卡死，
// 少交几层也不让程序一直阻塞。
//
// 栈是帧里那个位置 wTop 牵着的一条链表，一个节点 24 字节：[下一个位置][那一条路的文本][第几层]。
// 压进去的文本都是拷出来的那一份，弹出来用完就 @free，与 list 里那个位置互不相干。
//
// en: @file_walk (rcx = root): returns rax = the full paths of a whole directory tree in one
// list. It descends level by level, recording every path reached,
// directories included - the caller wants "what is under this path", and excluding directories
// would force it to ask all over again which entries are directories. Every element in the list
// is a fresh copy of the text, and the list is its only owner. The answers match
// file.list_paths' two: a directory that will not open is the 10043 sentence, a path too long
// for the wide buffer is the 10051 sentence. Descent stops at level 64: symbolic links and
// mount points can build a tree that never ends, so capping the depth trades a few missing
// levels for not blocking the program. The stack is a linked list held by the wTop slot on
// the frame, 24 bytes per node:
// [next][this path's text][which level]. The text pushed is always a copy, freed after popping,
// and unrelated to the element stored in the list.
func fileWalk(b *rt) {
	const (
		wRoot = 0  // 传进来的那一个句柄，不动它：先拷一份压进栈
		wOut  = 1  // 生成的那个 list
		wTop  = 2  // 栈顶，0 是空
		wText = 3  // 这一圈在钻的那一条目录（自己的一份）
		wDep  = 4  // 它在哪一层
		wNode = 5  // 节点与临时块
		wDLen = 6  // 目录翻成宽字符后有几个
		wSep  = 7  // 名字从第几个宽字符起
		wSrc  = 8  // 这一条要翻的那串宽字符在哪
		wByte = 9  // 翻出来几个字节
		wAttr = 10 // 这一条的属性里"是目录"那一位
		wOne  = 11 // 刚翻出来的那一条文本
		wTmp  = 12
	)
	var (
		badPath   = b.label()
		noDir     = b.label()
		loop      = b.label()
		done      = b.label()
		entry     = b.label()
		body      = b.label()
		cp        = b.label()
		cpd       = b.label()
		translate = b.label()
		nxt       = b.label()
		backCheck = b.label()
		slashEnd  = b.label()
		keepSep   = b.label()
		putSep    = b.label()
		gotSep    = b.label()
		deep      = b.label()
	)
	a := b.a
	b.prologue()
	b.put(wRoot, x64.RCX)

	a.MovImm(x64.RCX, 0)
	a.MovImm(x64.RDX, tagString)
	b.call("@list_new")
	b.put(wOut, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(wTop, x64.RAX)

	// 把 wPut 里那一份文本连同层数挂上栈：交进来的那一个句柄归调用方，压栈的一律是拷出来的
	push := func(textSlot int, fromDep bool, dep int64) {
		a.MovImm(x64.RCX, 24)
		a.MovImm(x64.RDX, 0)
		b.call("@alloc")
		b.put(wNode, x64.RAX)
		b.get(x64.RDI, wNode)
		b.get(x64.RAX, wTop)
		a.MovStore(x64.RDI, 0, x64.RAX)
		b.get(x64.RAX, textSlot)
		a.MovStore(x64.RDI, 8, x64.RAX)
		if fromDep {
			b.get(x64.RAX, wDep)
			a.AddImm(x64.RAX, 1)
		} else {
			a.MovImm(x64.RAX, dep)
		}
		a.MovStore(x64.RDI, 16, x64.RAX)
		b.get(x64.RAX, wNode)
		b.put(wTop, x64.RAX)
	}

	// 根：拷一份压进栈，层数 0
	b.get(x64.RCX, wRoot)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wRoot)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	b.put(wOne, x64.RAX)
	push(wOne, false, 0)

	a.Mark(loop)
	b.get(x64.RAX, wTop)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, done)
	// 取栈顶：三格都落到帧里，节点本身归还给堆
	b.get(x64.RSI, wTop)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.put(wTop, x64.RAX)
	a.MovLoad(x64.RAX, x64.RSI, 8)
	b.put(wText, x64.RAX)
	a.MovLoad(x64.RAX, x64.RSI, 16)
	b.put(wDep, x64.RAX)
	a.Mov(x64.RCX, x64.RSI)
	b.call("@free") // 这个 24 字节的节点用完就归还

	b.leaRIP(x64.RDX, "enumPat")
	b.get(x64.RCX, wText)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)
	a.ShrImm(x64.RAX, 1) // @to_wide 交回的是字节数，这里要的是宽字符个数
	b.put(wDLen, x64.RAX)

	// 尾巴上的分隔符：已经有了就把名字接在那个位置之后，没有就先补一个（与 @file_list 同一段）
	b.leaRIP(x64.RDI, "enumPat")
	b.get(x64.RAX, wDLen)
	a.ShlImm(x64.RAX, 1)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX16(x64.RCX, x64.RDI, -2)
	a.CmpImm(x64.RCX, '\\')
	a.Jcc(x64.NE, backCheck)
	b.get(x64.RAX, wDLen)
	a.Jmp(keepSep)
	a.Mark(backCheck)
	a.CmpImm(x64.RCX, '/')
	a.Jcc(x64.E, slashEnd)
	a.MovImm(x64.RCX, '\\')
	a.MovStore8(x64.RDI, 0, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore8(x64.RDI, 1, x64.RCX)
	a.AddImm(x64.RDI, 2)
	b.get(x64.RAX, wDLen)
	a.AddImm(x64.RAX, 1)
	a.Jmp(putSep)
	a.Mark(slashEnd)
	b.get(x64.RAX, wDLen)
	a.Mark(keepSep)
	b.put(wSep, x64.RAX)
	a.Jmp(gotSep)
	a.Mark(putSep)
	b.put(wSep, x64.RAX)
	a.Mark(gotSep)

	a.MovImm(x64.RCX, '*')
	a.MovStore8(x64.RDI, 0, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore8(x64.RDI, 1, x64.RCX)
	a.MovStore8(x64.RDI, 2, x64.RCX) // 同上：`*` 后面补一个 0，别让上一轮的名字尾巴跟着
	a.MovStore8(x64.RDI, 3, x64.RCX)

	b.leaRIP(x64.RCX, "enumPat")
	b.leaRIP(x64.RDX, "findBuf")
	b.impCall("FindFirstFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, noDir)
	b.storeRIP(x64.RAX, "findH")

	a.Mark(entry)
	b.leaRIP(x64.RSI, "findBuf")
	a.AddImm(x64.RSI, ffdName)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, '.')
	a.Jcc(x64.NE, body)
	a.MovZX16(x64.RAX, x64.RSI, 2)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt) // 就是 "."
	a.CmpImm(x64.RAX, '.')
	a.Jcc(x64.NE, body)
	a.MovZX16(x64.RAX, x64.RSI, 4)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt) // 就是 ".."
	a.Mark(body)

	// 属性里"是目录"那一位先记下来：它决定这一条要不要再钻一层
	b.leaRIP(x64.RAX, "findBuf")
	a.MovZX32(x64.RCX, x64.RAX, ffdAttr)
	a.AndImm(x64.RCX, attrDir)
	b.put(wAttr, x64.RCX)

	// 名字抄到分隔符后面（盖掉那个 `*`），整串就是"目录＋名字"
	b.get(x64.RAX, wSep)
	a.ShlImm(x64.RAX, 1)
	b.leaRIP(x64.RDI, "enumPat")
	a.Add(x64.RDI, x64.RAX)
	b.leaRIP(x64.RSI, "findBuf")
	a.AddImm(x64.RSI, ffdName)
	a.Mark(cp)
	a.MovZX16(x64.RAX, x64.RSI, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, cpd)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.Mov(x64.RCX, x64.RAX)
	a.ShrImm(x64.RCX, 8)
	a.MovStore8(x64.RDI, 1, x64.RCX)
	a.AddImm(x64.RDI, 2)
	a.AddImm(x64.RSI, 2)
	a.Jmp(cp)
	a.Mark(cpd)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.MovStore8(x64.RDI, 1, x64.RAX)
	b.leaRIP(x64.RAX, "enumPat")
	b.put(wSrc, x64.RAX)

	// 宽字符翻成 UTF-8 进 cvBuf；长到翻不出去的那一条跳过，这个目录照旧钻完
	a.Mark(translate)
	a.MovImm(x64.RCX, cpUTF8)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wSrc)
	a.MovImm(x64.R9, -1)
	b.leaRIP(x64.RAX, "cvBuf")
	a.AddImm(x64.RAX, strHead)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, cvCap)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	a.MovStore(x64.RBP, arg8, x64.RAX)
	b.impCall("WideCharToMultiByte")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt)
	a.AddImm(x64.RAX, -1)
	b.put(wByte, x64.RAX)

	b.leaRIP(x64.RCX, "cvBuf")
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wByte)
	b.call("@str_sub")
	b.put(wOne, x64.RAX)
	b.get(x64.RCX, wOut)
	b.get(x64.RDX, wOne)
	b.call("@list_add") // 那个 list 是这一份文本的唯一持有者

	b.get(x64.RAX, wAttr)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nxt) // 不是目录：这一条到这儿就完
	b.get(x64.RAX, wDep)
	a.CmpImm(x64.RAX, 64) // 到这一层就不再往下钻
	a.Jcc(x64.B, deep)
	a.Jmp(nxt) // 到层数上限：这一支不钻了
	a.Mark(deep)
	b.get(x64.RCX, wOne)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wOne)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub") // 再拷一份压栈：list 里那个位置归 list
	b.put(wTmp, x64.RAX)
	push(wTmp, true, 0)

	a.Mark(nxt)
	b.loadRIP(x64.RCX, "findH")
	b.leaRIP(x64.RDX, "findBuf")
	b.impCall("FindNextFileW")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, entry)

	b.loadRIP(x64.RCX, "findH")
	b.impCall("FindClose")
	b.get(x64.RCX, wText)
	b.call("@free") // 这个目录的文本是本例程自己的拷贝
	a.Jmp(loop)

	a.Mark(done)
	b.get(x64.RAX, wOut)
	b.epilogue()

	a.Mark(noDir)
	b.throwWith(codeIsDir, "sNoDir")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// itemAt 把 list 里第几个位置上的那一个句柄取进帧槽。@file_remove_tree 三处都要这么读一次，
// 抽在 Go 这一侧发成一小段，与 copyRun、widen 那几条同一类做法（不另开运行期例程）。
//
// en: itemAt takes the handle at index n of a list into a frame slot. @file_remove_tree needs
// that read in three places, so it is factored to the Go side and emitted as a short run - the
// same kind of arrangement as copyRun and widen (no separate runtime routine).
func itemAt(b *rt, listSlot, iSlot, outSlot int) {
	a := b.a
	b.get(x64.RSI, listSlot)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	b.get(x64.RDX, iSlot)
	a.ShlImm(x64.RDX, 3)
	a.Add(x64.RSI, x64.RDX)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.put(outSlot, x64.RAX)
}

// @file_remove_tree(rcx=根)：把这个目录连里面的东西一起删掉。
//
// 顺序是先删文件、再按发现的反序删目录：@file_walk 返回的那个 list 里父目录排在子目录前面，
// 倒着走一遍就是子目录先删，轮到父目录时它里面已经空了。根自己不在那份 list 里，
// 所以在两遍走完之后单独删它——那一条答复与 file.remove_directory 同一条。
// 删不成用的是现成那两条句子：文件删不掉是"删不掉这个文件"、目录删不掉是"删不掉这个目录"，
// 都是 10041 号，与 file.remove、file.remove_directory 同两条，不开新号。
// 根不在是 @file_walk 那一侧报的 10043 号那一句，与 file.list_paths 同一条答复。
//
// en: @file_remove_tree (rcx = root): delete this directory together with everything inside.
// The order is files first, then directories in the reverse of discovery:
// @file_walk's list puts a parent before its children, so walking it backwards dismantles
// children first and the parent is empty by the time it is reached. The root itself is not in
// that list, so it is deleted once the two passes finish - with the same answer as
// file.remove_directory. Failures reuse the two existing sentences: an undeletable file is
// "cannot delete this file", an undeletable directory is "cannot delete this directory", both
// code 10041, the same two as file.remove and file.remove_directory, with no new code. A
// missing root is the 10043 sentence reported by the @file_walk side, the same answer as
// file.list_paths.
func fileRemoveTree(b *rt) {
	const (
		rRoot = 0
		rList = 1
		rLen  = 2
		rI    = 3
		rOne  = 4
	)
	var (
		top1, done1, dirWay, back1, top2, done2, fileWay, freeTop, freeDone = b.label(), b.label(), b.label(), b.label(), b.label(), b.label(), b.label(), b.label(), b.label()
	)
	a := b.a
	b.prologue()
	b.put(rRoot, x64.RCX)

	b.get(x64.RCX, rRoot)
	b.call("@file_walk")
	b.put(rList, x64.RAX)
	b.get(x64.RAX, rList)
	a.MovLoad(x64.RAX, x64.RAX, cLen)
	b.put(rLen, x64.RAX)

	// 第一遍顺着走：是目录的先放着，其余交给 @file_remove
	a.Xor(x64.RAX, x64.RAX)
	b.put(rI, x64.RAX)
	a.Mark(top1)
	b.get(x64.RAX, rI)
	b.get(x64.RDX, rLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done1)
	itemAt(b, rList, rI, rOne)
	b.get(x64.RCX, rOne)
	b.call("@file_is_dir")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, dirWay)
	b.get(x64.RCX, rOne)
	b.call("@file_remove")
	a.Jmp(back1)
	a.Mark(dirWay)
	a.Mark(back1)
	b.get(x64.RAX, rI)
	a.AddImm(x64.RAX, 1)
	b.put(rI, x64.RAX)
	a.Jmp(top1)
	a.Mark(done1)

	// 第二遍倒着走：子目录先删完，父目录后面删，轮到父目录时里面已经空了
	b.get(x64.RAX, rLen)
	a.AddImm(x64.RAX, -1)
	b.put(rI, x64.RAX)
	a.Mark(top2)
	b.get(x64.RAX, rI)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.L, done2)
	itemAt(b, rList, rI, rOne)
	b.get(x64.RCX, rOne)
	b.call("@file_is_dir")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, fileWay) // 文件在第一遍已经删过了
	b.get(x64.RCX, rOne)
	b.call("@file_rmdir")
	a.Mark(fileWay)
	b.get(x64.RAX, rI)
	a.AddImm(x64.RAX, -1)
	b.put(rI, x64.RAX)
	a.Jmp(top2)
	a.Mark(done2)

	// 根自己不在那一份 list 里（@file_walk 只交它下面的东西），子目录都删完后单独删它
	b.get(x64.RCX, rRoot)
	b.call("@file_rmdir")

	// 那个 list 与里面每一个位置都是这一条自己造的：先归还每个位置的文本，再归还数据块与容器对象
	a.Xor(x64.RAX, x64.RAX)
	b.put(rI, x64.RAX)
	a.Mark(freeTop)
	b.get(x64.RAX, rI)
	b.get(x64.RDX, rLen)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, freeDone)
	itemAt(b, rList, rI, rOne)
	b.get(x64.RCX, rOne)
	b.call("@free")
	b.get(x64.RAX, rI)
	a.AddImm(x64.RAX, 1)
	b.put(rI, x64.RAX)
	a.Jmp(freeTop)
	a.Mark(freeDone)
	b.get(x64.RAX, rList)
	a.MovLoad(x64.RCX, x64.RAX, cData)
	b.call("@free")
	b.get(x64.RCX, rList)
	b.call("@free")
	b.epilogue()
}

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

// write.go 是往外写的那几条成员：file.write、file.append、file.write_bytes、file.remove。
//
// `file.exists` 不在这里——它回答的就是"是否存在"，与 `path.exists` 是同一个问题、同一个答复，
// 降级那头发的是同一条 @path_exists，不另写一遍机器码。
//
// 这一组都要先把路径翻成宽字符：翻不出去（空串、超过本机支持的长度）是 10051 号，与 `file.read`
// 同一条规则；路径递得出去而系统拒绝，才是这一组自己的号——写不进 42、删不掉 41。
//
// 追加不开第二条 Seek：`CreateFileW` 的访问权直接给 `FILE_APPEND_DATA`，Windows 就把每次
// 写都放到文件尾巴上（别的写者同时写也不会插进别人已写的中间）。少一次调用就少一处失败点。
//
// en: write.go is the outward-writing members: file.write, file.append,
// file.write_bytes, file.remove. `file.exists` is not here - it answers the same question
// ("is it there") and gives the same result as `path.exists`, so lowering emits the very same
// @path_exists rather than writing machine code twice. Every member of this group translates
// its path to wide characters first: a path that cannot be translated (empty, or longer than
// this machine accepts) is code 10051, the same rule as `file.read`; only a path that goes
// through and the system refuses takes this group's own code - unwritable 42, undeletable 41.
// Appending opens no second Seek: CreateFileW's access right is given
// FILE_APPEND_DATA directly, and Windows puts every write at the file's end (so even a
// concurrent writer cannot splice into the middle of what somebody else already wrote). One
// fewer call is one fewer failure point.
package runtime

import "neflang.org/nf/internal/x64"

// 写这一路要的访问权与处置方式。
//
// en: The access right and disposition the writing path needs.
const (
	genericWrite   = int64(0x40000000) // 读写都要
	fileAppendData = int64(0x0004)     // 只允许在文件末尾追加
	createAlways   = int64(2)          // 文件已存在则清空重写，不存在则创建
	openAlways     = int64(4)          // 文件已存在则打开，不存在则创建
	codeWriteFail  = 10042
)

// fileWriteAppend 是 @file_write 与 @file_append 共用的主体，差别只在访问权与处置方式。
//
// 一次写完：内容的字节数就在文本对象自己那个位置里，返回的"写到了几个"是 DWORD，
// 先清高位再与要写的比一次——少了就是 10042 号，不留下半截文件却不报错。
// 空内容照走：WriteFile 收 0 个字节是成功的，返回的 0 与要求的 0 相等，不动那一条判断。
//
// en: fileWriteAppend is the body shared by @file_write and @file_append, differing only in
// access right and disposition. One write takes everything: the content's byte count is in the
// text object's own slot, and the returned "how many were written" is a DWORD whose high half
// is cleared before comparing with what was asked - fewer means code 10042, leaving no
// half-written file without an error. Empty content still goes through: WriteFile succeeding
// with 0 bytes returns 0 against a request of 0, so the comparison is unchanged.
func fileWriteAppend(b *rt, access, disposition int64) {
	const (
		wPath = 0
		wText = 1
		wH    = 2
		wGot  = 3
	)
	a := b.a
	b.prologue()
	b.put(wText, x64.RDX)
	nameToWide(b, wPath) // 顺带把路径对象摆进第 wPath 个槽位
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "aWide")
	a.MovImm(x64.RDX, access)
	a.MovImm(x64.R8, shareAll)
	a.Xor(x64.R9, x64.R9)
	a.MovImm(x64.RAX, disposition)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, attrNormal)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	b.impCall("CreateFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	fail := b.label()
	a.Jcc(x64.E, fail)
	b.put(wH, x64.RAX)

	b.get(x64.RCX, wH)
	b.get(x64.RDX, wText)
	a.AddImm(x64.RDX, strHead) // 正文就在两个槽长度后面
	b.get(x64.RAX, wText)
	a.MovLoad(x64.R8, x64.RAX, StrBytes)
	a.LeaMem(x64.R9, x64.RBP, -8*(wGot+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX) // lpOverlapped 给 NULL：这一路是同步句柄
	b.impCall("WriteFile")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, fail)
	a.MovZX32(x64.RAX, x64.RBP, -8*(wGot+1))
	b.get(x64.RDX, wText)
	a.MovLoad(x64.RDX, x64.RDX, StrBytes)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, fail)

	b.get(x64.RCX, wH)
	b.impCall("CloseHandle")
	b.epilogue()

	a.Mark(fail)
	b.throwWith(codeWriteFail, "sNoWrite")
	b.epilogue()
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
	b.epilogue()
}

// @file_write(rcx=路径, rdx=内容)：整个文件写出，原来有的覆盖掉。写不进抛 10042 号。
//
// en: @file_write (rcx = path, rdx = content): write the whole file, replacing what was there.
// Unwritable throws code 10042.
func fileWrite(b *rt) { fileWriteAppend(b, genericWrite, createAlways) }

// @file_append(rcx=路径, rdx=内容)：尾部续写，文件不存在就直接建。失败同样是 10042 号。
//
// en: @file_append (rcx = path, rdx = content): continue writing at the end, creating the file
// when it is missing. Failure is likewise code 10042.
func fileAppend(b *rt) { fileWriteAppend(b, fileAppendData, openAlways) }

// @file_write_bytes(rcx=路径文本, rdx=bytes)：整块按字节写出去，原来有的覆盖掉。
//
// 与 @file_write 同一套规矩：处置方式是 createAlways（有就清空、没有就建），一次 WriteFile
// 写到底，写不满就是 10042 号那一句，不留下半截文件却不报错；空的块也照走这一段（写 0 个字节）。
//
// 这一例程早期收的是 `list<byte>`：每个位置占满 8 字节，所以要先要一块 N 字节的
// 临时缓冲，把每个位置的低八位攒成连着的一截，写完再还。换成 `bytes` 之后正文本来就是连着的一截，
// WriteFile 直接读那一段，一块临时缓冲都不必造。
//
// en: @file_write_bytes (rcx = path text, rdx = bytes): write the whole block as bytes,
// replacing what was there. The same rules as @file_write: disposition createAlways (empty an
// existing one, create a missing one), one WriteFile to the end, and a short write is the 10042
// sentence rather than leaving a half file looking successful; an empty block still runs this
// path (writing 0 bytes). This routine originally took `list<byte>`: every slot filled
// 8 bytes, so an N-byte temporary buffer had to be allocated, the low eight bits of each slot
// gathered into a contiguous run, and the buffer returned after writing. With `bytes` the body
// already is a contiguous run, so WriteFile reads that part directly and not one temporary
// buffer is built.
func fileWriteBytes(b *rt) {
	const (
		wPath = 0 // 路径那一段文本
		wData = 1 // 内容的 bytes 对象
		wH    = 2
		wGot  = 3 // WriteFile 返回的"写出去几个"
		wN    = 4 // 有几个字节
	)
	var (
		badPath = b.label()
		fail    = b.label()
	)
	a := b.a
	b.prologue()
	b.put(wData, x64.RDX)
	nameToWide(b, wPath) // 顺带把路径对象摆进第 wPath 个槽位
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPath)

	b.leaRIP(x64.RCX, "aWide")
	a.MovImm(x64.RDX, genericWrite)
	a.MovImm(x64.R8, shareAll)
	a.Xor(x64.R9, x64.R9)
	a.MovImm(x64.RAX, createAlways)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovImm(x64.RAX, attrNormal)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	b.impCall("CreateFileW")
	a.MovImm(x64.R8, invalidHandle)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, fail)
	b.put(wH, x64.RAX)

	b.get(x64.RCX, wData)
	a.MovLoad(x64.RAX, x64.RCX, BytesLen)
	b.put(wN, x64.RAX)           // 有几个字节
	a.Mov(x64.RDX, x64.RCX)      // 正文起点＝句柄后面的第一个字节
	a.AddImm(x64.RDX, BytesBody) // 一个字节一个位置，连着摆，WriteFile 要的正是这一段
	b.get(x64.RCX, wH)
	b.get(x64.R8, wN)
	a.LeaMem(x64.R9, x64.RBP, -8*(wGot+1))
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX) // lpOverlapped 给 NULL：这一路是同步句柄
	b.impCall("WriteFile")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, fail)
	a.MovZX32(x64.RAX, x64.RBP, -8*(wGot+1))
	b.get(x64.RDX, wN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, fail)

	b.get(x64.RCX, wH)
	b.impCall("CloseHandle")
	b.epilogue()

	a.Mark(fail)
	b.throwWith(codeWriteFail, "sNoWrite")
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
}

// @file_remove(rcx=路径)：删掉。删不掉抛 10041 号（"读不到"与"删不掉"共用一号）。
//
// 目录也走这一条：`DeleteFileW` 对目录一定失败，于是返回 10041 号——这一版没有目录遍历，
// 也没有"删掉一个空目录"这一问，指向目录就是这个结果，不另开一号。
//
// en: @file_remove (rcx = path): delete it, throwing code 10041 when it cannot be deleted
// ("cannot read" and "cannot delete" share one code). Directories take this path
// too: `DeleteFileW` always fails on a directory, so the result is 10041 - this version has no
// directory traversal and no "delete an empty directory" question, so pointing at a directory
// gets exactly this outcome and no new code.
func fileRemove(b *rt) {
	a := b.a
	b.prologue()
	gone := b.label()
	nameToWide(b, 0)
	a.Test(x64.RAX, x64.RAX)
	badPath := b.label()
	a.Jcc(x64.E, badPath)
	b.leaRIP(x64.RCX, "aWide")
	b.impCall("DeleteFileW")
	zeroExtend32(b, 1) // BOOL 是 DWORD 宽度，只取低半部分有效
	b.get(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, gone)
	b.throwWith(codeNoFile, "sNoRemove")
	b.epilogue()
	a.Mark(gone)
	b.epilogue()
	a.Mark(badPath)
	b.throwWith(codeBadPath, "sBadPath")
	b.epilogue()
}

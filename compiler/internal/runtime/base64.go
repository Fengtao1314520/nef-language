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

package runtime

// base64.go 是补的那一对：一串字节按 RFC 4648 那张标准表写成文本，以及反过来读回来。
// 两张表摆在静态区（"b64Table" 是 64 个字符，"b64Value" 是 128 格反查表，不是表里字符那个位置摆 255），
// 运行期只查表，不自己拼字母表，也不问操作系统有没有 base64 那一套 API。
//
// en: base64.go is the pair added for text and bytes: write a run of bytes as text with the
// standard RFC 4648 table, and read it back the other way. The two tables sit in the static
// area (b64Table is 64 characters, b64Value is a 128-slot reverse table, 255 where a character
// is not in the table); the runtime only looks the tables up - it never assembles an alphabet
// or asks the operating system for a base64 API.

import (
	"neflang.org/nf/internal/x64"
)

// codeNotB64 是"那一串字根本不是 base64"那一条：长度不成组、= 摆在中间、
// 或遇到一个不在表里的字符，都报这一句。解出来是字节、但那串字节不是合法 UTF-8 的是 10044 号，
// 与 text.from_bytes、file.read 同一条答复——两件事不一样，不并成一个号。
//
// en: codeNotB64 is the sentence "that run is not base64 at all": a length that does
// not make a group, an = sitting in the middle, or a character outside the table all report it.
// Bytes that decode fine but are not valid UTF-8 take code 10044, the same answer as
// text.from_bytes and file.read - two different facts are not merged into one number.
const codeNotB64 = 10014

// base64Data 摆那两张表：64 个字符的正表，与 128 格的反查表（不是表里字符那个位置摆 255，
// 高位那一截（≥0x80）也一律 255——那不是 base64，也不是 ASCII）。
//
// en: base64Data lays out the two tables: the forward table of 64 characters and the reverse
// table of 128 slots (255 where the character is not in the table, and the whole upper half >=
// 0x80 also 255 - that is neither base64 nor ASCII).
func base64Data(put func(name string, bs []byte)) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	table := []byte(alphabet)
	value := make([]byte, 128)
	for i := range value {
		value[i] = 255
	}
	for i, c := range table {
		value[c] = byte(i)
	}
	put("b64Table", table)
	put("b64Value", value)
}

// @b64_encode、@bytes_b64_encode(rcx=源)：返回 rax=这一串字节的 base64 写法（永远是文本）。
//
// 三个字节一组合成 24 位，切成四段各查一次表；最后一组凑不满三字节，缺的那几格摆 =。
// 输出长度是 (((n+2)/3)*4)，永远是 4 的整倍数，这一个数不用再去看余数。
//
// 源是什么档由两个偏移说：`b64EncodeFrom` 拿"字节数在哪一格、正文从哪儿起"，
// 文本给 (`StrBytes`, `strHead`)，`bytes` 给 (`BytesLen`, `BytesBody`)。写出去的块两档同一形状，
// 因为 base64 的结果本来就是给读者看的那串字符，`bytes` 编码出来的也是文本，不是另一段字节。
//
// en: @b64_encode, @bytes_b64_encode (rcx = source): returns rax = the base64 spelling of these
// bytes (always text). Three bytes are packed into 24 bits and cut into four segments, each
// looked up once; the final group of fewer than three bytes pads the missing slots with =. The
// output length is (((n+2)/3)*4), always an exact multiple of four, so that number needs no
// remainder inspection. Which flavour the source is gets said by two offsets: b64EncodeFrom
// takes "which slot holds the byte count and where the body starts", text giving (StrBytes,
// strHead) and bytes giving (BytesLen, BytesBody). The block written out has the same shape for
// both flavours, because a base64 result is by definition the characters a reader sees -
// encoding bytes produces text, not another run of bytes.
func b64EncodeFrom(b *rt, lenOff, bodyOff int) {
	const (
		eSrc = 0
		eN   = 1 // 几个字节
		eOut = 2 // 新文本
		eIn  = 3 // 读到第几个字节
		eOp  = 4 // 写到第几个字符
		eAcc = 5 // 这一组合并的 24 位
		eGot = 6 // 这一组凑齐了几个字节
		eSiz = 7 // 整块几字节
		eTab = 8 // 那张表在哪儿
	)
	a := b.a
	b.prologue()
	b.put(eSrc, x64.RCX)
	b.get(x64.RAX, eSrc)
	a.MovLoad(x64.RAX, x64.RAX, int32(lenOff))
	b.put(eN, x64.RAX)

	b.get(x64.RAX, eN)
	a.AddImm(x64.RAX, 2)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 3)
	a.Div1(x64.RCX) // 无符号除：rdx 先摆 0，商落 rax
	a.MovImm(x64.RCX, 4)
	a.Imul(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, strHead)
	b.put(eSiz, x64.RAX)
	b.get(x64.RCX, eSiz)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(eOut, x64.RAX)
	b.get(x64.RDI, eOut)
	b.get(x64.RAX, eSiz)
	a.AddImm(x64.RAX, -strHead)
	a.MovStore(x64.RDI, 0, x64.RAX) // 头里的字节数
	a.MovStore(x64.RDI, 8, x64.RAX) // 码点数：出来的全是 ASCII，一字节一个码点
	b.leaRIP(x64.RAX, "b64Table")   // 要的是这张表的地址，不是它头八格里存的那个数
	b.put(eTab, x64.RAX)
	b.get(x64.RDI, eOut)
	a.AddImm(x64.RDI, strHead)

	a.Xor(x64.RAX, x64.RAX)
	b.put(eIn, x64.RAX)
	b.put(eOp, x64.RAX)

	// 这一组的第 k 个字节：还在范围内就并进 acc，不在就跳过（尾巴那一组凑不满是常事）。
	// 跳过的那个位置由闭包自己造：三个字节各要一个落点，共用一个标号就成了重复登记。
	takeByte := func(k int, shift byte) {
		skip := b.label()
		b.get(x64.RAX, eIn)
		a.AddImm(x64.RAX, int32(k))
		b.get(x64.RCX, eN)
		a.Cmp(x64.RAX, x64.RCX)
		a.Jcc(x64.AE, skip)
		b.get(x64.RSI, eSrc)
		a.AddImm(x64.RSI, int32(bodyOff))
		a.Add(x64.RSI, x64.RAX) // rax 已经是 eIn+k
		a.MovZX8(x64.R9, x64.RSI, 0)
		if shift > 0 {
			a.ShlImm(x64.R9, shift)
		}
		b.get(x64.RCX, eAcc)
		a.Add(x64.RCX, x64.R9)
		b.put(eAcc, x64.RCX)
		b.get(x64.RAX, eGot)
		a.AddImm(x64.RAX, 1)
		b.put(eGot, x64.RAX)
		a.Mark(skip)
	}

	top, done := b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, eIn)
	b.get(x64.RDX, eN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)

	a.Xor(x64.RAX, x64.RAX)
	b.put(eAcc, x64.RAX)
	b.put(eGot, x64.RAX)
	takeByte(0, 16)
	takeByte(1, 8)
	takeByte(2, 0)

	// 第三、四格先摆上 =，凑齐了再覆成表里的字符：省掉"哪一格该摆什么"的三层分支
	b.get(x64.RDI, eOut)
	a.AddImm(x64.RDI, strHead)
	b.get(x64.RAX, eOp)
	a.Add(x64.RDI, x64.RAX)
	a.MovImm(x64.R9, '=')
	a.MovStore8(x64.RDI, 2, x64.R9)
	a.MovImm(x64.R9, '=')
	a.MovStore8(x64.RDI, 3, x64.R9)

	b.get(x64.R8, eAcc)
	charAt := func(shift byte, pos int32) {
		a.Mov(x64.RAX, x64.R8)
		if shift > 0 {
			a.ShrImm(x64.RAX, shift)
		}
		a.AndImm(x64.RAX, 63)
		b.get(x64.RSI, eTab) // 每查一次从槽里重取一次表址：不把这份地址留在别的寄存器上
		a.Add(x64.RSI, x64.RAX)
		a.MovZX8(x64.R9, x64.RSI, 0)
		a.MovStore8(x64.RDI, pos, x64.R9)
	}
	charAt(18, 0)
	charAt(12, 1)
	b.get(x64.RAX, eGot)
	a.CmpImm(x64.RAX, 2)
	low2 := b.label()
	a.Jcc(x64.L, low2)
	charAt(6, 2)
	a.Mark(low2)
	b.get(x64.RAX, eGot)
	a.CmpImm(x64.RAX, 3)
	low3 := b.label()
	a.Jcc(x64.L, low3)
	charAt(0, 3)
	a.Mark(low3)

	b.get(x64.RAX, eIn)
	a.AddImm(x64.RAX, 3)
	b.put(eIn, x64.RAX)
	b.get(x64.RAX, eOp)
	a.AddImm(x64.RAX, 4)
	b.put(eOp, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, eOut)
	b.epilogue()
}

// @b64_encode(rcx=文本)：源是文本那一档（`text.to_base64`）。
//
// en: @b64_encode (rcx = text): the source is the text flavour (`text.to_base64`).
func b64Encode(b *rt) { b64EncodeFrom(b, StrBytes, strHead) }

// @bytes_b64_encode(rcx=bytes 句柄)：同一件事，源换成 `bytes` 那一档（`bytes.to_base64`）。
//
// en: @bytes_b64_encode (rcx = bytes handle): the same thing with the source being the `bytes`
// flavour (`bytes.to_base64`).
func b64BytesEncode(b *rt) { b64EncodeFrom(b, BytesLen, BytesBody) }

// @b64_decode、@bytes_b64_decode(rcx=文本)：返回 rax=解出来的那一串字节。
//
// 长度是 4 的整倍数；= 只准摆在整串的最后两格里，摆过之后再出现正文的字符也是摆在中间——
// 这两样都是 10014 号那一句。
// 别的字符一律查反表，表里那个位置是 255 就是根本不像 base64，同样 10014 号。
//
// 解出来的字节再验一遍 UTF-8：它得先是一门语言的文本，不是合法编码是 10044 号，
// 与 text.from_bytes、file.read 遇到不合法字节是同一条答复。这一验只在写成文本那一档做：
// `bytes.from_base64` 返回的就是字节本身，二进制没有"合不合法"这一问，也不数码点。
//
// en: @b64_decode, @bytes_b64_decode (rcx = text): returns rax = the decoded bytes. The length
// must be a multiple of four; = may appear only in the last two slots of the whole run, and a
// body character after one is also "= in the middle" - both are the sentence at code 10014.
// Every other character goes through the reverse table, and 255 in that slot means it does not
// look like base64 at all, also 10014. The decoded bytes are then validated as UTF-8: to be
// written as text it must first be text in this language, and an illegal encoding is code 10044
// - the same answer text.from_bytes and file.read give for a bad byte. That check happens only
// on the text flavour: `bytes.from_base64` returns the bytes themselves, and binary has no
// question of "valid encoding" and no code point count.
func b64DecodeTo(b *rt, asString bool) {
	const (
		dSrc = 0
		dN   = 1 // 几个字符
		dOut = 2 // 新块
		dI   = 3 // 读到第几个字符
		dAcc = 5 // 这一组合并的 24 位
		dPad = 6 // 尾巴上几格是 =
		dTab = 7 // 反查表在哪儿
		dSiz = 8 // 整块几字节
		dLen = 9 // 真正的字节数
	)
	// 写出去的那一块是哪一档：句柄往上数几字节起正文、字节数摆在哪一个位置、块头上盖哪个类型字，
	// 还有要不要验 UTF-8，四件事由 `asString` 一起定下来。
	dstHead, dstLenField, dstTag := bytesHead, BytesLen, tagBytes
	if asString {
		dstHead, dstLenField, dstTag = strHead, StrBytes, tagString
	}
	a := b.a
	b.prologue()
	b.put(dSrc, x64.RCX)
	b.get(x64.RAX, dSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(dN, x64.RAX)

	bad, okLen := b.label(), b.label()
	b.get(x64.RAX, dN)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, okLen) // 空的文本是合法的空结果
	b.get(x64.RAX, dN)
	a.AndImm(x64.RAX, 3)
	a.Jcc(x64.NE, bad) // 长度不是 4 的整倍数
	a.Mark(okLen)

	b.get(x64.RAX, dN)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 4)
	a.Div1(x64.RCX)
	a.MovImm(x64.RCX, 3)
	a.Imul(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, int32(dstHead))
	b.put(dSiz, x64.RAX)
	b.get(x64.RCX, dSiz)
	a.MovImm(x64.RDX, int64(dstTag))
	b.call("@alloc")
	b.put(dOut, x64.RAX)
	b.leaRIP(x64.RAX, "b64Value") // 同上：反查表也是取地址
	b.put(dTab, x64.RAX)

	a.Xor(x64.RAX, x64.RAX)
	b.put(dI, x64.RAX)
	b.put(dPad, x64.RAX)

	top, done := b.label(), b.label()
	a.Mark(top)
	b.get(x64.RAX, dI)
	b.get(x64.RDX, dN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)

	a.Xor(x64.RAX, x64.RAX)
	b.put(dAcc, x64.RAX)
	for k := 0; k < 4; k++ {
		// 每一个位置一对自己的标号：四格共用一个就是重复登记
		isChar, accum := b.label(), b.label()
		b.get(x64.RSI, dSrc)
		a.AddImm(x64.RSI, strHead)
		b.get(x64.RAX, dI)
		a.AddImm(x64.RAX, int32(k))
		a.Add(x64.RSI, x64.RAX) // rsi＝字符地址，rax＝它的位置
		a.MovZX8(x64.R9, x64.RSI, 0)
		a.CmpImm(x64.R9, '=')
		a.Jcc(x64.NE, isChar)
		// 是 =：整串只剩下两格还容得下它
		b.get(x64.RCX, dN)
		a.AddImm(x64.RCX, -2)
		a.Cmp(x64.RAX, x64.RCX)
		a.Jcc(x64.B, bad)
		b.get(x64.RAX, dPad)
		a.AddImm(x64.RAX, 1)
		b.put(dPad, x64.RAX)
		a.MovImm(x64.R9, 0) // 那六位按零并入，格数照旧
		a.Jmp(accum)
		a.Mark(isChar)
		// = 只准出现在尾巴上：它前头不能有它，它后头也不能再有正文的字符
		b.get(x64.RCX, dPad)
		a.Test(x64.RCX, x64.RCX)
		a.Jcc(x64.NE, bad)
		b.get(x64.RSI, dTab)
		a.Add(x64.RSI, x64.R9)
		a.MovZX8(x64.R9, x64.RSI, 0)
		a.CmpImm(x64.R9, 255)
		a.Jcc(x64.E, bad) // 不是表里的字符
		a.Mark(accum)
		b.get(x64.RAX, dAcc)
		if k > 0 {
			a.ShlImm(x64.RAX, 6)
		}
		a.Or(x64.RAX, x64.R9)
		b.put(dAcc, x64.RAX)
	}

	// 24 位拆三个字节写出去；尾巴那几格 = 不在这三格里少写，最后一起从头里的数目扣
	b.get(x64.R8, dAcc)
	b.get(x64.RDI, dOut)
	a.AddImm(x64.RDI, int32(dstHead))
	b.get(x64.RAX, dI)
	a.ShrImm(x64.RAX, 2) // 第几组＝字符数整除 4，每组的落点是它乘 3
	a.MovImm(x64.RCX, 3)
	a.Imul(x64.RAX, x64.RCX)
	a.Add(x64.RDI, x64.RAX)
	a.Mov(x64.R9, x64.R8)
	a.ShrImm(x64.R9, 16)
	a.MovStore8(x64.RDI, 0, x64.R9)
	a.Mov(x64.R9, x64.R8)
	a.ShrImm(x64.R9, 8)
	a.MovStore8(x64.RDI, 1, x64.R9)
	a.Mov(x64.R9, x64.R8)
	a.MovStore8(x64.RDI, 2, x64.R9)

	b.get(x64.RAX, dI)
	a.AddImm(x64.RAX, 4)
	b.put(dI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, dN)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 4)
	a.Div1(x64.RCX)
	a.MovImm(x64.RCX, 3)
	a.Imul(x64.RAX, x64.RCX)
	b.get(x64.RDX, dPad)
	a.Sub(x64.RAX, x64.RDX)
	b.put(dLen, x64.RAX)
	b.get(x64.RDI, dOut)
	b.get(x64.RAX, dLen)
	a.MovStore(x64.RDI, int32(dstLenField), x64.RAX)

	if asString {
		bad8 := b.label()
		b.get(x64.RCX, dOut)
		a.AddImm(x64.RCX, strHead)
		b.get(x64.RDX, dLen)
		b.call("@utf8_ok")
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, bad8)

		// 数码点：不是续字节的就是一个码点，与 @str_sub、@bytes_str 那一段同一件事
		b.get(x64.RSI, dOut)
		a.AddImm(x64.RSI, strHead)
		b.get(x64.R11, dLen)
		a.MovImm(x64.R8, leadMask)
		a.MovImm(x64.R9, contByte)
		a.Xor(x64.R10, x64.R10)
		a.Xor(x64.RAX, x64.RAX)
		cnt, skip, cdone := b.label(), b.label(), b.label()
		a.Mark(cnt)
		a.Test(x64.R11, x64.R11)
		a.Jcc(x64.E, cdone)
		a.MovZX8(x64.RAX, x64.RSI, 0)
		a.And(x64.RAX, x64.R8)
		a.Cmp(x64.RAX, x64.R9)
		a.Jcc(x64.E, skip)
		a.AddImm(x64.R10, 1)
		a.Mark(skip)
		a.AddImm(x64.RSI, 1)
		a.AddImm(x64.R11, -1)
		a.Jmp(cnt)
		a.Mark(cdone)
		b.get(x64.RDI, dOut)
		a.MovStore(x64.RDI, 8, x64.R10)
		b.get(x64.RAX, dOut)
		b.epilogue()

		a.Mark(bad8)
		b.get(x64.RCX, dOut)
		b.call("@free")
		b.throwWith(codeNotUTF8, "sNotUTF8")
		b.epilogue()
	} else {
		b.get(x64.RAX, dOut)
		b.epilogue()
	}

	a.Mark(bad)
	b.get(x64.RCX, dOut)
	b.call("@free")
	b.throwWith(codeNotB64, "sNotB64")
	b.epilogue()
}

// @b64_decode(rcx=文本)：解出来的字节验过 UTF-8，写成文本那一档（`text.from_base64`）。
//
// en: @b64_decode (rcx = text): the decoded bytes are validated as UTF-8 and written as text
// (`text.from_base64`).
func b64Decode(b *rt) { b64DecodeTo(b, true) }

// @bytes_b64_decode(rcx=文本)：同一串字节写成 `bytes` 那一档（`bytes.from_base64`）。
// 二进制不讲合法编码，所以那一道 UTF-8 检查与码点那个位置都不做。
//
// en: @bytes_b64_decode (rcx = text): the same bytes written as the `bytes` flavour
// (`bytes.from_base64`). Binary has no validity requirement, so both the UTF-8 check and the
// code point count are skipped.
func b64BytesDecode(b *rt) { b64DecodeTo(b, false) }

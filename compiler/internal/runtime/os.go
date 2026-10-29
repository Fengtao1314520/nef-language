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

// os.go 是 time、path、environment 这三组：问时钟、摆路径、读环境变量。
//
// 与 file.go 同一套前提：产物只依赖 kernel32，第五个及以后的实参落回本帧
// [rsp+0x20] 也就是 [rbp-0xE0] 那个位置往 rbp 方向一个位置一个位置。
//
// 文本进来只有一条路：@to_wide 翻成宽字符摆进静态缓冲（`aWide`／`wPath`，各 4096 个宽字符）。
// 文本出去也只有一条：先 WideCharToMultiByte 问一遍要几个字节，再翻进 aBuf 那块
// 「占位头 16 ＋ 正文」，最后交 @str_sub 就地造对象——与 @args 那一条完全同一个写法。
// 码点数由 @str_sub 自己数，不在这里另数一遍（规则只有一处）。
//
// 系统调用返回的是 **DWORD**（四个字节），rax 上面那半截是未定义的数据：凡是要拿来比大小、
// 当长度、判成败的，一律先 zeroExtend32 洗一遍。@read_line 就出过这回事：按八个字节读了
// `ReadFile` 的"读到了几个"，产物自己跑对、宿主在内存里直跑就错——这里统一零扩展，不再出这类错。
//
// 分隔符读写都认 `\` 与 `/`，补的时候补本机习惯的那个 `\`。两个都是 ASCII，而 UTF-8 的
// 续字节一律 ≥ 0x80，所以在字节上扫不会扫进一个多字节字符中间（与 @str_case 同一条理由）。
//
// en: os.go is the three groups time, path and environment: ask the clock,
// arrange a path, read the environment. Same premise as file.go: the product depends only on
// kernel32, and the fifth and later arguments fall back to this frame's [rsp+0x20],
// i.e. [rbp-0xE0], one slot toward rbp at a time. Text comes in through one door only: @to_wide
// translates into wide characters in a static buffer (aWide/wPath, 4096 wide characters each).
// Text goes out through one door too: ask WideCharToMultiByte how many bytes, translate into
// aBuf's "placeholder header 16 + body", and hand it to @str_sub to build the object in place -
// exactly how @args does it. The code point count is made by @str_sub itself rather than
// counted again here (one rule in one place). System calls return a DWORD (four bytes) and the
// upper half of rax is unspecified leftover data: anything used for comparison, as a length, or
// as a success test is washed through zeroExtend32 first. @read_line once read `ReadFile`'s
// "how many" as eight bytes - the product worked and the host running the same image in memory
// failed; uniform zero-extension prevents this class of error. Both separators are read and
// written (\ and /)
// and the one this machine prefers, \, is added when missing. Both are ASCII and every UTF-8
// continuation byte is >= 0x80, so scanning bytes cannot land inside a multi-byte character -
// the same reason as @str_case.
package runtime

import "neflang.org/nf/internal/x64"

// 这三组要的那些常数。
//
// en: The constants those three groups need.
const (
	unixEpoch  = 116444736000000000 // 1601 到 1970 之间隔了多少个 100 纳秒
	hundredNap = 10000000           // 一秒里几个 100 纳秒
	sepA       = '\\'               // 补的时候补它
	sepB       = '/'                // 读的时候两个都认
	dotByte    = '.'
	attrMiss   = int64(0xFFFFFFFF) // GetFileAttributesW 交回这个就是"不在"
	codeNoEnv  = 10061             // 没有这个环境变量
	codeEnvAsk = 10060             // 环境那一组的兜底号
	// wideToText 与 env／current_directory 那几条预留的位置：调用方的槽一律排在 0～9，
	// 这两个槽固定给它们用，不与调用方的槽重叠
	wBytes = 22
	wSrc   = 23
)

// osData 摆这一组的静态数据：高精度计数器那两个槽、那一条反斜杠、两句运行期要说的话。
//
// en: osData lays out this group's static data: the two high-resolution counter slots, that one
// backslash, and the two runtime sentences.
func osData(put func(name string, bs []byte)) {
	put("perfStart", make([]byte, 8))
	put("perfFreq", make([]byte, 8))
	put("sSep", []byte{'\\'})
	put("sNoEnv", strObject("没有这个环境变量"))
	put("sEnvAsk", strObject("环境这一处没办成"))
}

// @time_now()：返回 rax=从 1970-01-01 零点（UTC）到此刻的秒数。
//
// Windows 返回的是"从 1601 年起算的 100 纳秒位置数"，减掉中间那 369 年再除一千万。
// 只到整数秒；语言里没有日期类型，算"跨了几天"要自己除。
//
// en: @time_now(): returns rax = seconds from 1970-01-01 00:00 UTC to now. Windows
// reports 100-nanosecond units since 1601, so the 369 years in between are subtracted and the
// rest divided by ten million. Whole seconds only; there is no date type, and
// "how many days passed" is left to the caller's own division.
func timeNow(b *rt) {
	a := b.a
	b.prologue()
	a.LeaMem(x64.RCX, x64.RBP, -8)
	b.impCall("GetSystemTimeAsFileTime")
	b.get(x64.RAX, 0)
	a.MovImm(x64.RCX, unixEpoch)
	a.Sub(x64.RAX, x64.RCX)
	// 这一段一定非负（现在晚于 1970），所以走无符号除法，不碰 idiv 的符号那一套
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, hundredNap)
	a.Div1(x64.RCX)
	b.epilogue()
}

// @time_parts(rcx=秒, rdx=那一块 `Calendar`)：把从 1970-01-01 零点（UTC）起算的秒数拆成
// 年、月、日、时、分、秒、星期，一个一个写进那块。什么都不返回。
//
// 秒数可以为负（1970 年以前）。日数与星期按"向下取整"算：`-1` 秒是 1969-12-31 的 23:59:59，
// 不是 1970-01-01 的 -1 秒。x86 的除法向零截断、余数跟着被除数的符号，所以那两处各补一次。
//
// 七个偏移写死在下面：0 年、8 月、16 日、24 时、32 分、40 秒、48 星期（0 = 星期一），
// 与 `check.calendarFields` 那张表一一对应——改字段顺序要连着改这里，反过来也是。
//
// 日子那一套是 Howard Hinnant 的 civil_from_days：先把位置数挪到"3 月 1 日起算"那一套，
// 闰日就落在这一段的最后一个位置，剩下的只有除 146097、36524、1460、365、153、5 这六道。
//
// en: @time_parts (rcx = seconds, rdx = that `Calendar` block): split seconds since 1970-01-01
// 00:00 UTC into year, month, day, hour, minute, second and weekday, writing each into the
// block. Returns nothing. Seconds may be negative (before 1970). Day number
// and weekday use FLOOR division: `-1` second is 1969-12-31 23:59:59, not 1970-01-01 minus one
// second. x86 division truncates toward zero and the remainder follows the dividend's sign, so
// those two places each correct once. The seven offsets are hardcoded below: 0 year, 8 month,
// 16 day, 24 hour, 32 minute, 40 second, 48 weekday (0 = Monday), matching check.calendarFields
// one for one - changing the field order means changing this too, and the other way round. The
// calendar arithmetic is Howard Hinnant's civil_from_days: the day count is shifted to a year
// starting March 1 so the leap day falls at the end of that run, leaving only divisions by
// 146097, 36524, 1460, 365, 153 and 5.
func timeParts(b *rt) {
	a := b.a
	b.prologue()
	// 槽位一路排开：0 秒、1 块、2 日数、3 当天第几秒、4 z、5 era、6 doe、7 yoe、
	// 8 y、9 doy、10 mp、11 月、12 累加器、13 term、14/15 除法腾出来的手
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.get(x64.RDI, 1) // 整趟只用 RDI 拿块地址：除法只动 RAX 与 RDX

	// 日数 = 秒数往下取整除 86400；余数负的说明商退过了一个位置，补一次
	b.get(x64.RAX, 0)
	a.Cqo()
	a.MovImm(x64.RCX, 86400)
	a.Idiv(x64.RCX)
	b.put(2, x64.RAX)
	a.Test(x64.RDX, x64.RDX)
	posDay := b.label()
	a.Jcc(x64.NS, posDay)
	b.get(x64.RAX, 2)
	a.AddImm(x64.RAX, -1)
	b.put(2, x64.RAX)
	a.Mark(posDay)
	// 当天第几秒 = 秒数 - 日数*86400，落在 0～86399
	b.get(x64.RAX, 2)
	a.MovImm(x64.RCX, 86400)
	a.Imul(x64.RAX, x64.RCX)
	b.put(14, x64.RAX)
	b.get(x64.RAX, 0)
	b.get(x64.RCX, 14)
	a.Sub(x64.RAX, x64.RCX)
	b.put(3, x64.RAX)

	// 时、分、秒
	b.get(x64.RAX, 3)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 3600)
	a.Div1(x64.RCX)
	a.MovStore(x64.RDI, 24, x64.RAX)
	b.put(14, x64.RDX)
	b.get(x64.RAX, 14)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 60)
	a.Div1(x64.RCX)
	a.MovStore(x64.RDI, 32, x64.RAX)
	a.MovStore(x64.RDI, 40, x64.RDX)

	// 星期：1970-01-01 是星期四，所以日数加三再对 7 取余；余数是负的就补上一个位置
	b.get(x64.RAX, 2)
	a.AddImm(x64.RAX, 3)
	a.Cqo()
	a.MovImm(x64.RCX, 7)
	a.Idiv(x64.RCX)
	a.Test(x64.RDX, x64.RDX)
	posWeek := b.label()
	a.Jcc(x64.NS, posWeek)
	a.AddImm(x64.RDX, 7)
	a.Mark(posWeek)
	a.MovStore(x64.RDI, 48, x64.RDX)

	// z = 日数 + 719468（挪到 0000-03-01 起算）
	b.get(x64.RAX, 2)
	a.AddImm(x64.RAX, 719468)
	b.put(4, x64.RAX)
	// era = z / 146097，负的那一侧同样先退一个位置
	b.get(x64.RAX, 4)
	a.Test(x64.RAX, x64.RAX)
	posEra := b.label()
	a.Jcc(x64.NS, posEra)
	a.AddImm(x64.RAX, -146096)
	a.Mark(posEra)
	a.Cqo()
	a.MovImm(x64.RCX, 146097)
	a.Idiv(x64.RCX)
	b.put(5, x64.RAX)
	// doe = z - era*146097，0～146096
	b.get(x64.RAX, 5)
	a.MovImm(x64.RCX, 146097)
	a.Imul(x64.RAX, x64.RCX)
	b.put(14, x64.RAX)
	b.get(x64.RAX, 4)
	b.get(x64.RCX, 14)
	a.Sub(x64.RAX, x64.RCX)
	b.put(6, x64.RAX)
	// yoe = (doe - doe/1460 + doe/36524 - doe/146096) / 365
	b.get(x64.RAX, 6)
	b.put(12, x64.RAX)
	b.get(x64.RAX, 6)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 1460)
	a.Div1(x64.RCX)
	b.get(x64.RCX, 12)
	a.Sub(x64.RCX, x64.RAX)
	b.put(12, x64.RCX)
	b.get(x64.RAX, 6)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 36524)
	a.Div1(x64.RCX)
	b.get(x64.RCX, 12)
	a.Add(x64.RCX, x64.RAX)
	b.put(12, x64.RCX)
	b.get(x64.RAX, 6)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 146096)
	a.Div1(x64.RCX)
	b.get(x64.RCX, 12)
	a.Sub(x64.RCX, x64.RAX)
	b.put(12, x64.RCX)
	b.get(x64.RAX, 12)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 365)
	a.Div1(x64.RCX)
	b.put(7, x64.RAX)
	// y = yoe + era*400（1 月与 2 月要再加一年，那一步在最后）
	b.get(x64.RAX, 5)
	a.MovImm(x64.RCX, 400)
	a.Imul(x64.RAX, x64.RCX)
	b.get(x64.RCX, 7)
	a.Add(x64.RAX, x64.RCX)
	b.put(8, x64.RAX)
	// doy = doe - (365*yoe + yoe/4 - yoe/100)
	b.get(x64.RAX, 7)
	a.MovImm(x64.RCX, 365)
	a.Imul(x64.RAX, x64.RCX)
	b.put(13, x64.RAX)
	b.get(x64.RAX, 7)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 4)
	a.Div1(x64.RCX)
	b.get(x64.RCX, 13)
	a.Add(x64.RCX, x64.RAX)
	b.put(13, x64.RCX)
	b.get(x64.RAX, 7)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 100)
	a.Div1(x64.RCX)
	b.get(x64.RCX, 13)
	a.Sub(x64.RCX, x64.RAX)
	b.put(13, x64.RCX)
	b.get(x64.RAX, 6)
	b.get(x64.RCX, 13)
	a.Sub(x64.RAX, x64.RCX)
	b.put(9, x64.RAX)
	// mp = (5*doy + 2) / 153，这一段里 3 月是第 0 个月
	b.get(x64.RAX, 9)
	a.MovImm(x64.RCX, 5)
	a.Imul(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, 2)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 153)
	a.Div1(x64.RCX)
	b.put(10, x64.RAX)
	// 日 = doy - (153*mp + 2)/5 + 1
	b.get(x64.RAX, 10)
	a.MovImm(x64.RCX, 153)
	a.Imul(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, 2)
	a.Xor(x64.RDX, x64.RDX)
	a.MovImm(x64.RCX, 5)
	a.Div1(x64.RCX)
	b.put(14, x64.RAX)
	b.get(x64.RAX, 9)
	b.get(x64.RCX, 14)
	a.Sub(x64.RAX, x64.RCX)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, 16, x64.RAX)
	// 月 = mp < 10 ? mp + 3 : mp - 9
	b.get(x64.RAX, 10)
	a.CmpImm(x64.RAX, 10)
	lateMonth := b.label()
	doneMonth := b.label()
	a.Jcc(x64.GE, lateMonth)
	a.AddImm(x64.RAX, 3)
	a.Jmp(doneMonth)
	a.Mark(lateMonth)
	a.AddImm(x64.RAX, -9)
	a.Mark(doneMonth)
	b.put(11, x64.RAX)
	a.MovStore(x64.RDI, 8, x64.RAX)
	// 年 = y + (月 <= 2 ? 1 : 0)
	b.get(x64.RAX, 11)
	a.CmpImm(x64.RAX, 2)
	sameYear := b.label()
	gotYear := b.label()
	a.Jcc(x64.G, sameYear)
	b.get(x64.RAX, 8)
	a.AddImm(x64.RAX, 1)
	a.Jmp(gotYear)
	a.Mark(sameYear)
	b.get(x64.RAX, 8)
	a.Mark(gotYear)
	a.MovStore(x64.RDI, 0, x64.RAX)
	b.epilogue()
}

// @time_elapsed()：返回 xmm0=本程序从启动到此刻跑了多少秒（含小数）。
//
// en: @time_elapsed(): returns xmm0 = seconds this program has been running since start-up,
// including the fraction.
func timeElapsed(b *rt) {
	a := b.a
	b.prologue()
	b.loadRIP(x64.RAX, "perfFreq")
	a.Test(x64.RAX, x64.RAX)
	got := b.label()
	a.Jcc(x64.NE, got)
	a.LeaMem(x64.RCX, x64.RBP, -8)
	b.impCall("QueryPerformanceFrequency")
	b.get(x64.RAX, 0)
	b.storeRIP(x64.RAX, "perfFreq")
	a.LeaMem(x64.RCX, x64.RBP, -16)
	b.impCall("QueryPerformanceCounter")
	b.get(x64.RAX, 1)
	b.storeRIP(x64.RAX, "perfStart")
	a.Mark(got)

	a.LeaMem(x64.RCX, x64.RBP, -16)
	b.impCall("QueryPerformanceCounter")
	b.get(x64.RDX, 1) // rdx=此刻的 tick 数
	b.loadRIP(x64.RAX, "perfStart")
	a.Sub(x64.RDX, x64.RAX)
	b.loadRIP(x64.RAX, "perfFreq")
	a.CvtSI2SD(0, x64.RDX)
	a.CvtSI2SD(1, x64.RAX)
	a.Divsd(0, 1)
	b.epilogue()
}

// ---- 公共的小函数 ----

// zeroExtend32 把"系统调用刚交回的那个 DWORD"洗成干净的 64 位，并摆进第 slot 格。
//
// 先按四个字节落帧、再零扩展读回来——寄存器之间没有现成的"三十二位往六十四位清零"这一条，
// 而上面那半截是未定义的数据，不洗就不能拿去比大小、当长度。
//
// 末了那一次**整格写回**不能省：这个位置之后还会被 `b.get` 按八个字节读走（当长度、当格数），
// 只落四个字节的话上面那半截还是原来留在那儿的东西——@env_get 第一版就是这么触发了 10091 号：
// 问到的格数是对的，乘二之后却变成了一个天文数字的分配请求。
//
// en: zeroExtend32 washes "the DWORD a system call just returned" into a clean 64-bit value and
// places it in slot `slot`. Store four bytes to the frame first, then zero-extend on the read
// back: there is no ready-made 32-to-64 widening between registers, and the upper half is
// unspecified leftover data that cannot be compared or used as a length unwashed. The final
// WHOLE-SLOT write must not be skipped: this position is later read as eight bytes by b.get (as
// a length, as a slot count), and storing four bytes leaves the upper half as whatever was
// there - @env_get's first version hit code 10091 exactly that way: the byte count asked was
// correct, but doubled it became an astronomical allocation request.
func zeroExtend32(b *rt, slot int) {
	off := int32(-8 * (slot + 1))
	b.a.MovStore32(x64.RBP, off, x64.RAX)
	b.a.MovZX32(x64.RAX, x64.RBP, off)
	b.a.MovStore(x64.RBP, off, x64.RAX)
}

// nameToWide 把 rcx 那个文本翻进静态的 aWide，返回 rax=几个宽字符（0 是翻不成或太长）。
//
// 文本对象在翻之前先存进第 srcSlot 格留住：@to_wide 与它调的那些例程都会动寄存器。
//
// en: nameToWide translates the text in rcx into the static aWide and returns rax = how many
// wide characters (0 meaning it could not be translated or is too long). The text object is
// parked in slot srcSlot before translation, because @to_wide and the routines it calls all
// move registers.
func nameToWide(b *rt, srcSlot int) {
	b.put(srcSlot, x64.RCX)
	b.leaRIP(x64.RDX, "aWide")
	b.get(x64.RCX, srcSlot)
	b.call("@to_wide")
}

// widen 把第 srcSlot 格那个文本翻进名为 bufName 的静态宽字符缓冲，返回的宽字符数在 rax。
//
// 与 nameToWide 的分别是**空串那个位置**：空串翻出来是"只装一个结尾 0"、宽字符数 0，
// 这是合法输入——`environment.set("K", "")` 就是把一个变量设成空，`environment.get("")`
// 也只是"没有这个变量"那一问，应由 API 报 10061 号，不该在翻译这一层就断。
// 只有"本身有内容却一个都没翻出来"才是坏，那是太长、传不出去，跳到 bad（由调用方 Mark）。
//
// en: widen translates the text in slot srcSlot into the static wide buffer named bufName,
// returning the wide character count in rax. The difference from nameToWide is the EMPTY STRING
// position: translating one yields "just a terminating 0" with a count of 0, which is a legal
// input - `environment.set("K", "")` sets a variable to empty, and `environment.get("")` is
// simply the "no such variable" question, which should be answered by the API with code 10061
// rather than cut off in the translation layer. Only "it had content yet nothing came out" is a
// failure, meaning too long to pass, and jumps to bad (a Mark supplied by the caller).
func widen(b *rt, srcSlot, lenSlot int, bufName string, bad x64.Label) {
	a := b.a
	b.get(x64.RAX, srcSlot)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(lenSlot, x64.RAX)
	b.get(x64.RCX, srcSlot)
	b.leaRIP(x64.RDX, bufName)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	ok := b.label()
	a.Jcc(x64.NE, ok)
	b.get(x64.RAX, lenSlot)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, bad)
	a.Mark(ok)
}

// wideToText 把第 wideSlot 格那块宽字符（第 countSlot 格那么多个，不含结尾那个 0）翻成
// UTF-8，造出一个文本对象留在 rax。两次 WideCharToMultiByte：先问要几个字节，
// 再翻进 aBuf 的正文那一侧，最后 @str_sub 就地造对象。
//
// 问出来是 0（那一段本来就是空的）也照同一条走：@str_sub 收零长度返回空文本，不必另开一支。
// 落点是静态的 aBuf，装得下 lineCap 个字节——环境变量与路径都远在这个数以下；真的超过了
// 返回的就是截断的那一段，这一条语言规则尚未定。
// 本函数只用第 wBytes／wSrc 两个槽，调用方的槽排在 0～9，不会重叠。
//
// en: wideToText translates the wide characters in slot wideSlot (countSlot of them, excluding
// the terminating zero) into UTF-8 and leaves a text object in rax. WideCharToMultiByte is
// called twice: first to ask how many bytes, then into aBuf's body side, and finally @str_sub
// builds the object in place. An answer of 0 (the run was empty) takes the same path: @str_sub
// with zero length returns empty text, so no extra branch is needed. The landing spot is the
// static aBuf, which holds lineCap bytes - environment variables and paths stay far below that;
// if it is truly exceeded the truncated run comes back, and the language rule for that case
// is not settled. This function uses only slots wBytes/wSrc while the
// caller's are numbered 0-9, so they cannot collide.
func wideToText(b *rt, wideSlot, countSlot int) {
	a := b.a
	// 第一次：只问长度（lpMultiByteStr 传 NULL、cbMultiByte 传 0）
	a.MovImm(x64.RCX, cpUTF8)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wideSlot)
	b.get(x64.R9, countSlot)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	a.MovStore(x64.RBP, arg8, x64.RAX)
	b.impCall("WideCharToMultiByte")
	zeroExtend32(b, wBytes)

	// 第二次：真翻
	a.MovImm(x64.RCX, cpUTF8)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wideSlot)
	b.get(x64.R9, countSlot)
	b.leaRIP(x64.RAX, "aBuf")
	a.AddImm(x64.RAX, strHead)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.get(x64.RAX, wBytes)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	a.MovStore(x64.RBP, arg8, x64.RAX)
	b.impCall("WideCharToMultiByte")
	zeroExtend32(b, wBytes)

	b.leaRIP(x64.RCX, "aBuf")
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, wBytes)
	b.call("@str_sub")
}

// segmentBounds 在第 srcSlot 格那个文本上摆出"最后一段"的字节边界：
// 第 hiSlot 格 = 最后一个不是分隔符的字节下标（整条都是分隔符时是 -1），
// 第 startSlot 格 = 那一段的起点。两个都按字节算，不是码点。
//
// 尾巴上一路是分隔符就一路退掉：`docs\` 的最后一段是 `docs`，不是那个空的东西。
// 起点那一趟是正着走的：遇到分隔符就把起点挪到它右边一个位置，一路走到段的尾巴为止，
// 留下的就是"最后一个分隔符之后"。
//
// en: segmentBounds computes the byte bounds of the LAST segment of the text in slot srcSlot:
// hiSlot = the index of the final byte that is not a separator (-1 if the whole string is
// separators), startSlot = where that segment begins. Both are bytes, not code points. A run of
// separators at the tail is backed over one by one: the last segment of `docs\` is `docs`, not
// the empty thing after it. The start pass walks forward: each separator moves the start to the
// position right after it, ending at the segment's tail, which leaves "after the last
// separator".
func segmentBounds(b *rt, srcSlot, nSlot, hiSlot, startSlot, curSlot int) {
	a := b.a
	// 尾巴：hi 从 n-1 起，是分隔符就往前退
	b.get(x64.RAX, nSlot)
	a.AddImm(x64.RAX, -1)
	b.put(hiSlot, x64.RAX)
	tTest := b.label()
	tHit := b.label()
	tDone := b.label()
	a.Mark(tTest)
	b.get(x64.RAX, hiSlot)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, tDone) // 退过界：整条都是分隔符
	b.get(x64.RSI, srcSlot)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, hiSlot)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, sepA)
	a.Jcc(x64.E, tHit)
	a.CmpImm(x64.RAX, sepB)
	a.Jcc(x64.NE, tDone) // 两个都不是：这个位置就是段的尾巴
	a.Mark(tHit)
	b.get(x64.RAX, hiSlot)
	a.AddImm(x64.RAX, -1)
	b.put(hiSlot, x64.RAX)
	a.Jmp(tTest)
	a.Mark(tDone)

	// 起点：0 起头，正着往右走，每遇到一个分隔符就把起点挪到它右边一个位置。
	// 走到尾巴为止（`cur >= hi` 就停），所以段的第一个字符永远算在段里。
	a.MovImm(x64.RAX, 0)
	b.put(startSlot, x64.RAX)
	b.put(curSlot, x64.RAX)
	sTest := b.label()
	sHit := b.label()
	sStep := b.label()
	sDone := b.label()
	a.Mark(sTest)
	b.get(x64.RAX, curSlot)
	b.get(x64.RDX, hiSlot)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.GE, sDone)
	b.get(x64.RSI, srcSlot)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, curSlot)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, sepA)
	a.Jcc(x64.E, sHit)
	a.CmpImm(x64.RAX, sepB)
	a.Jcc(x64.NE, sStep)
	a.Mark(sHit)
	b.get(x64.RAX, curSlot)
	a.AddImm(x64.RAX, 1)
	b.put(startSlot, x64.RAX)
	a.Mark(sStep)
	b.get(x64.RAX, curSlot)
	a.AddImm(x64.RAX, 1)
	b.put(curSlot, x64.RAX)
	a.Jmp(sTest)
	a.Mark(sDone)
}

// ---- path ----

// @path_join(rcx=甲, rdx=乙)：返回 rax=拼好的路径，斜杠按本机习惯补。
//
// 甲的尾巴已经是分隔符就不再补第二个；乙是空的也不补。哪一头是空的就返回另一头的一份拷贝
// （不是那一个句柄——返回同一个句柄就成了两个持有者指着同一块）。
// 不做规范化：不去 `..`、不展开 `~`，规则只要求补斜杠这一件事。
//
// en: @path_join (rcx = a, rdx = b): returns rax = the joined path with the separator added in
// this machine's style. No second separator when a already ends with one, and none when
// b is empty. If either side is empty, a COPY of the other is returned (not the same handle -
// returning one handle to two names would give it two holders). Nothing is normalised: no
// resolving `..`, no expanding `~`; only the separator is required.
func pathJoin(b *rt) {
	const (
		jA    = 0
		jB    = 1
		jNA   = 2 // 甲几字节
		jNB   = 3
		jSep  = 4 // 要不要补一个分隔符
		jAll  = 5
		jRuns = 6
		jObj  = 7
	)
	a := b.a
	b.prologue()
	b.put(jA, x64.RCX)
	b.put(jB, x64.RDX)
	b.get(x64.RAX, jA)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(jNA, x64.RAX)
	b.get(x64.RAX, jB)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(jNB, x64.RAX)

	// 甲是空的：乙原样返回一份（乙也可能空，那正好是空文本）
	b.get(x64.RAX, jNA)
	a.Test(x64.RAX, x64.RAX)
	nonEmpty := b.label()
	a.Jcc(x64.NE, nonEmpty)
	b.get(x64.RCX, jB)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, jNB)
	b.call("@str_sub")
	b.epilogue()
	a.Mark(nonEmpty)

	// 要不要补：乙空着不补，甲的尾巴已经是分隔符不补，乙的开头已经是分隔符也不补——
	// "斜杠按本机习惯补"说的是缺一个才补一个，不是一律加一个。
	a.MovImm(x64.R8, 0)
	b.put(jSep, x64.R8)
	b.get(x64.RAX, jNB)
	a.Test(x64.RAX, x64.RAX)
	skip := b.label()
	a.Jcc(x64.E, skip)
	b.get(x64.RSI, jA)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, jNA)
	a.AddImm(x64.RDX, -1)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, sepA)
	a.Jcc(x64.E, skip)
	a.CmpImm(x64.RAX, sepB)
	a.Jcc(x64.E, skip)
	b.get(x64.RSI, jB)
	a.AddImm(x64.RSI, strHead)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, sepA)
	a.Jcc(x64.E, skip)
	a.CmpImm(x64.RAX, sepB)
	a.Jcc(x64.E, skip)
	a.MovImm(x64.R8, 1)
	b.put(jSep, x64.R8)
	a.Mark(skip)

	b.get(x64.RAX, jNA)
	b.get(x64.RDX, jNB)
	a.Add(x64.RAX, x64.RDX)
	b.get(x64.RDX, jSep)
	a.Add(x64.RAX, x64.RDX)
	b.put(jAll, x64.RAX)
	b.get(x64.RAX, jA)
	a.MovLoad(x64.RAX, x64.RAX, StrCount)
	b.get(x64.RDX, jB)
	a.MovLoad(x64.RDX, x64.RDX, StrCount)
	a.Add(x64.RAX, x64.RDX)
	b.get(x64.RDX, jSep)
	a.Add(x64.RAX, x64.RDX)
	b.put(jRuns, x64.RAX)

	b.get(x64.RCX, jAll)
	a.AddImm(x64.RCX, strHead)
	a.MovImm(x64.RDX, tagString)
	b.call("@alloc")
	b.put(jObj, x64.RAX)
	b.get(x64.RDI, jObj)
	b.get(x64.RAX, jAll)
	a.MovStore(x64.RDI, StrBytes, x64.RAX)
	b.get(x64.RAX, jRuns)
	a.MovStore(x64.RDI, StrCount, x64.RAX)
	a.AddImm(x64.RDI, strHead)

	b.get(x64.RSI, jA)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RCX, jNA)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.get(x64.RAX, jSep)
	a.Test(x64.RAX, x64.RAX)
	noSep := b.label()
	a.Jcc(x64.E, noSep)
	b.leaRIP(x64.RSI, "sSep")
	a.MovImm(x64.RCX, 1)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	a.Mark(noSep)
	b.get(x64.RSI, jB)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RCX, jNB)
	copyRun(b, x64.RDI, x64.RSI, x64.RCX)
	b.get(x64.RAX, jObj)
	b.epilogue()
}

// @path_file_name(rcx=路径)：返回 rax=最后那一段，含扩展名。
//
// 尾巴上的分隔符先退掉，所以 `docs\` 返回 `docs` 而不是空串；整条都是分隔符（或本来
// 就是空串）就返回空文本——那个长度算出来正好是 0，不必另开一支。
//
// en: @path_file_name (rcx = path): returns rax = the last segment, extension included.
// Trailing separators are backed off first, so `docs\` gives `docs` rather than an empty
// string; a path that is only separators (or empty to begin with) returns empty text - the
// computed length is exactly 0 there, so no separate branch is needed.
func pathFileName(b *rt) {
	const (
		fSrc = 0
		fN   = 1
		fHi  = 2
		fSt  = 3
		fCur = 4 // segmentBounds 找起点用的游标
	)
	a := b.a
	b.prologue()
	b.put(fSrc, x64.RCX)
	b.get(x64.RAX, fSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(fN, x64.RAX)
	segmentBounds(b, fSrc, fN, fHi, fSt, fCur)

	b.get(x64.RCX, fSrc)
	b.get(x64.RDX, fSt)
	b.get(x64.R8, fHi)
	b.get(x64.RAX, fSt)
	a.Sub(x64.R8, x64.RAX)
	a.AddImm(x64.R8, 1)
	b.call("@str_sub")
	b.epilogue()
}

// @path_extension(rcx=路径)：返回 rax=点后面那段；没有点返回空文本。
//
// 点必须在最后那一段的**中间**才算：`.nf` 这种以点开头的名字没有扩展名，那点属于名字本身。
//
// en: @path_extension (rcx = path): returns rax = the run after the dot, or empty text when
// there is none. The dot must lie INSIDE the last segment to count: a name that starts
// with a dot, `.nf`, has no extension - that dot belongs to the name.
func pathExtension(b *rt) {
	const (
		eSrc = 0
		eN   = 1
		eHi  = 2
		eSt  = 3
		eDot = 4 // 点在哪个位置；-1 是没有
		eCur = 5 // 游标：segmentBounds 用完，下面倒着找点接着用
	)
	a := b.a
	b.prologue()
	b.put(eSrc, x64.RCX)
	b.get(x64.RAX, eSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(eN, x64.RAX)
	segmentBounds(b, eSrc, eN, eHi, eSt, eCur)

	// 从段尾往段首倒着找第一个点；走到段的第一个字符为止（`cur > start`），
	// 所以 `.nf` 那种以点开头的名字找到的那点不算。
	a.MovImm(x64.RAX, -1)
	b.put(eDot, x64.RAX)
	b.get(x64.RAX, eHi)
	b.put(eCur, x64.RAX)
	test := b.label()
	hit := b.label()
	step := b.label()
	done := b.label()
	a.Mark(test)
	b.get(x64.RAX, eCur)
	b.get(x64.RDX, eSt)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.LE, done)
	b.get(x64.RSI, eSrc)
	a.AddImm(x64.RSI, strHead)
	b.get(x64.RDX, eCur)
	a.Add(x64.RSI, x64.RDX)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.CmpImm(x64.RAX, dotByte)
	a.Jcc(x64.E, hit)
	a.Mark(step)
	b.get(x64.RAX, eCur)
	a.AddImm(x64.RAX, -1)
	b.put(eCur, x64.RAX)
	a.Jmp(test)
	a.Mark(hit)
	b.get(x64.RAX, eCur)
	b.put(eDot, x64.RAX)

	a.Mark(done)
	// 没有点返回空文本；有点则起 = dot+1、长 = hi - dot（hi 一定在点后面，长度至少 1）
	b.get(x64.RAX, eDot)
	a.Test(x64.RAX, x64.RAX)
	none := b.label()
	a.Jcc(x64.S, none)
	b.get(x64.RCX, eSrc)
	b.get(x64.RDX, eDot)
	a.AddImm(x64.RDX, 1)
	b.get(x64.R8, eHi)
	b.get(x64.RAX, eDot)
	a.Sub(x64.R8, x64.RAX)
	b.call("@str_sub")
	b.epilogue()

	a.Mark(none)
	b.get(x64.RCX, eSrc)
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.call("@str_sub")
	b.epilogue()
}

// @path_exists(rcx=路径)：返回 rax=1 在／0 不在（这一条只问一句、不抛）。
//
// "不抛"管的是**在不在**这一问：路径摆得下、系统答"没有"，就返回 0，不抛 10041 号那一套。
// 但一条本机根本递不出去的路径（太长、翻不成宽字符）不是"不在"，是"写得不对"——
// 那与 `file.read` 的同一情形是同一个 10051 号，两边同一条规则（空串走的就是这一支）。
//
// en: @path_exists (rcx = path): returns rax = 1 present / 0 absent - this one "asks a
// question, throws nothing". The no-throw rule covers the PRESENT/ABSENT question: as long
// as the path fits and the system says "no", 0 is returned rather than the 10041 family. But a
// path this machine cannot pass at all (too long, not translatable to wide characters) is not
// "absent" - it is "written wrongly", and takes the same code 10051 as the same failure in
// file.read, one rule on both sides (an empty string comes here too).
func pathExists(b *rt) {
	a := b.a
	b.prologue()
	nameToWide(b, 0)
	a.Test(x64.RAX, x64.RAX)
	bad := b.label()
	a.Jcc(x64.E, bad)
	b.leaRIP(x64.RCX, "aWide")
	b.impCall("GetFileAttributesW")
	zeroExtend32(b, 1)
	b.get(x64.RAX, 1)
	a.MovImm(x64.RDX, attrMiss)
	a.Cmp(x64.RAX, x64.RDX)
	nope := b.label()
	a.Jcc(x64.E, nope)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
	a.Mark(nope)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
	a.Mark(bad)
	b.throwWith(codeBadPath, "sBadPath")
	b.epilogue()
}

// ---- environment ----

// @env_get(rcx=名字)：返回 rax=那个环境变量的值。
//
// 没有这个变量抛 10061 号——规则写明"不交回空串也不交回空句柄"，所以这条路一定有答复。
// 两次 GetEnvironmentVariableW：第一次传空缓冲问要几个宽字符（返回的数含结尾那个 0），
// 按那个数 @alloc 一块，第二次真读；第二次的返回值不含那个 0，就是翻成 UTF-8 的格数。
// 名字翻不成（太长）是 10060 号：那不是"没有这个变量"。
//
// en: @env_get (rcx = name): returns rax = that environment variable's value. A missing
// variable throws code 10061 - the rule states plainly that neither an empty string nor a null
// handle is handed back, so this path always answers. GetEnvironmentVariableW is called twice:
// the first
// with an empty buffer to ask how many wide characters (the returned count includes the
// terminating zero), one block is @alloc'd at that size, and the second read is real; the
// second call's return excludes that zero and is exactly the count to translate into UTF-8. A
// name that cannot be translated (too long) takes the 10060 side: that is not "no such
// variable".
func envGet(b *rt) {
	const (
		gName = 0
		gLen  = 4 // 名字本身几字节，只为"空名字合法、太长才算坏"那一条留着
		gNeed = 1 // 要几个宽字符（含结尾那个 0）
		gBuf  = 2 // 堆上那块宽字符
		gGot  = 3 // 真读到了几个（不含 0，可能是 0：那就是被设成了空）
	)
	a := b.a
	b.prologue()
	b.put(gName, x64.RCX)
	badName := b.label()
	widen(b, gName, gLen, "aWide", badName)

	b.leaRIP(x64.RCX, "aWide")
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.impCall("GetEnvironmentVariableW")
	zeroExtend32(b, gNeed)
	b.get(x64.RAX, gNeed)
	a.Test(x64.RAX, x64.RAX)
	missing := b.label()
	a.Jcc(x64.E, missing)

	b.get(x64.RCX, gNeed)
	a.ShlImm(x64.RCX, 1) // 两个字节一个宽字符
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.put(gBuf, x64.RAX)

	b.leaRIP(x64.RCX, "aWide")
	b.get(x64.RDX, gBuf)
	b.get(x64.R8, gNeed)
	b.impCall("GetEnvironmentVariableW")
	zeroExtend32(b, gGot)
	// 这一次返回 0 不再当"没有这个变量"：第一次已经问过尺寸了，0 格就是被设成了空串。
	// 规则要的就是"没设过"与"设成了空"分得开——把这个位置再报 10061 号就把两者重新混在一起了。
	wideToText(b, gBuf, gGot)
	b.epilogue()

	a.Mark(missing)
	b.throwWith(codeNoEnv, "sNoEnv")
	b.epilogue()
	a.Mark(badName)
	b.throwWith(codeEnvAsk, "sEnvAsk")
	b.epilogue()
}

// @env_set(rcx=名字, rdx=值)：设一个环境变量，只对本程序有效。
//
// 名字与值分别翻进 aWide 与 wPath 两块静态缓冲。值本身是空句柄，不在这一层检查——
// 降级在取用之前先按 10092 号拒绝，这一条只管操作系统那一侧。
// 不成就是 10060 号：环境这一组的兜底，没有为"设不进"另开一号。
// SetEnvironmentVariableW 返回的是 BOOL（DWORD），不洗高位就没法判成败。
//
// en: @env_set (rcx = name, rdx = value): set an environment variable, valid for this program
// only. Name and value are translated into the two static buffers aWide and wPath
// respectively. A null value handle is not checked here - lowering rejects it with code 10092
// before use, and this routine only deals with the operating-system side. Any failure is
// code 10060, this group's fallback: no separate code was opened for "could
// not set". SetEnvironmentVariableW returns a BOOL (DWORD), and without washing the upper half
// success cannot be judged.
func envSet(b *rt) {
	const (
		sName = 0
		sVal  = 1
		sOK   = 2
		sNLen = 3 // 名字几字节
		sVLen = 4 // 值几字节
	)
	a := b.a
	b.prologue()
	done := b.label()
	b.put(sVal, x64.RDX) // 名字那一翻会动 rdx，先把值留住
	b.put(sName, x64.RCX)
	bad := b.label()
	widen(b, sName, sNLen, "aWide", bad)
	// 值可以是空串（"把它设成空"是一件事），太长才算坏。对象已经在第 sVal 格里了，
	// 中间那一次 widen 动过 rcx，所以这里只交槽号、不再交寄存器。
	widen(b, sVal, sVLen, "wPath", bad)

	b.leaRIP(x64.RCX, "aWide")
	b.leaRIP(x64.RDX, "wPath")
	b.impCall("SetEnvironmentVariableW")
	zeroExtend32(b, sOK)
	b.get(x64.RAX, sOK)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, done)
	b.throwWith(codeEnvAsk, "sEnvAsk")
	a.Mark(done)
	b.epilogue()
	a.Mark(bad)
	b.throwWith(codeEnvAsk, "sEnvAsk")
	b.epilogue()
}

// @env_unset(rcx=名字)：删掉一个环境变量，只对本程序有效。
//
// 名字翻不进宽字符就是 10060 号，与 @env_set 同一条兜底。值那一侧给的是空句柄：
// Windows 那一句把"值为空句柄"就当成"把这个变量删掉"。
//
// 系统调用说没成时再问一次 GetLastError：203（ERROR_ENVVAR_NOT_FOUND）就是那个变量本来
// 就不在，这一句按"不做处理"收场，不算错——三个 remove 成员找不到那一样时也是不做处理。
// 剩下的失败原因（名字里带 `=` 那一类）照 10060 抛：同一组里两处对坏名字的答复不该相反。
//
// en: @env_unset (rcx = name): delete an environment variable, valid for this program only.
// A name that will not fit into wide characters is code 10060, the same fallback as
// @env_set. The value side passes an empty handle: Windows reads "value is an empty handle" as
// "delete this variable". When the system call says failure, GetLastError is asked once more:
// 203 (ERROR_ENVVAR_NOT_FOUND) means the variable was never there and this ends as "nothing
// happened", not an error - the three remove members also do nothing when the thing is missing.
// Any other failure reason (a name containing `=`, for instance) still throws 10060: two places
// in the same group must not answer a bad name in opposite ways.
func envUnset(b *rt) {
	const (
		sName = 0
		sNLen = 1 // 名字几字节
		sOK   = 2
	)
	const errNoEnvVar = 203 // ERROR_ENVVAR_NOT_FOUND
	a := b.a
	b.prologue()
	b.put(sName, x64.RCX)
	bad := b.label()
	widen(b, sName, sNLen, "aWide", bad)
	b.leaRIP(x64.RCX, "aWide")
	a.Xor(x64.RDX, x64.RDX)
	b.impCall("SetEnvironmentVariableW")
	zeroExtend32(b, sOK)
	gone := b.label()
	b.get(x64.RAX, sOK)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, gone)
	b.impCall("GetLastError")
	zeroExtend32(b, sOK)
	b.get(x64.RAX, sOK)
	a.CmpImm(x64.RAX, errNoEnvVar)
	notThere := b.label()
	a.Jcc(x64.E, notThere)
	b.throwWith(codeEnvAsk, "sEnvAsk")
	b.epilogue()
	a.Mark(notThere)
	a.Mark(gone)
	b.epilogue()
	a.Mark(bad)
	b.throwWith(codeEnvAsk, "sEnvAsk")
	b.epilogue()
}

// @path_dir(rcx=路径)：返回 rax=上一截（父目录那个位置），没有上一截返回空文本。
//
// 起点用的是 @path_file_name 那同一段 segmentBounds：它把尾巴上的分隔符先退掉，再返回最后一段
// 从哪个字节起。上一截就是那一个位置之前的全部字节，所以 a/b/ 返回 a、/x 返回 /、x 返回空文本——
// 三条都是同一个算法的落点，不另判形状。分隔符按字节认（/ 与 \ 都是 ASCII），
// 与 @path_join 那一条同一认法（文本在内存里就是 UTF-8）。
//
// en: @path_dir (rcx = path): returns rax = the part above (the parent-directory position), or
// empty text when there is none. The start point reuses the same
// segmentBounds as @path_file_name: it backs off trailing separators first, then returns the
// byte where the last segment begins. The upper part is therefore every byte before that
// position, which makes a/b/ give a, /x give /, and x give empty text - all three are landings
// of one algorithm, with no per-shape special case. Separators are recognised byte by byte (/
// and \ are both ASCII), the same reading as @path_join (text in memory IS UTF-8).
func pathDir(b *rt) {
	const (
		dSrc = 0
		dN   = 1
		dHi  = 2
		dSt  = 3
		dCur = 4
	)
	a := b.a
	b.prologue()
	b.put(dSrc, x64.RCX)
	b.get(x64.RAX, dSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(dN, x64.RAX)
	segmentBounds(b, dSrc, dN, dHi, dSt, dCur)

	b.get(x64.RCX, dSrc)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, dSt)
	b.call("@str_sub") // 起 0、长＝最后一段的起点：那之前的每一字节都是上一截
	b.epilogue()
}

// @path_abs(rcx=路径)：返回 rax=1 这条路是从根起的、0 不是。
//
// 三种写法算从根起，一条是 Windows 的盘符（`C:` 那一侧），两条是分隔符打头（`/a/b`、`\\server\share`）。
// 这一条是新定的规则，没有现成的业界惯例可跟，三种写法各有一条测试钉住。
// 判的都是字节：盘符的冒号、字母、分隔符全在 ASCII 里，UTF-8 的 lead 字节都 ≥0x80，冒充不了它们。
//
// en: @path_abs (rcx = path): returns rax = 1 when the path starts at a root, 0 otherwise.
// Three spellings count as rooted: one is a Windows drive letter (`C:`),
// two start with a separator (`/a/b`, `\\server\share`). There was no convention to follow -
// this is a newly set rule with one test per case. All
// comparisons are on bytes: the drive colon, the letter and the separators are all ASCII, and a
// UTF-8 lead byte is >= 0x80, so it cannot imitate them.
func pathAbs(b *rt) {
	const (
		pSrc = 0
		pN   = 1
	)
	a := b.a
	b.prologue()
	b.put(pSrc, x64.RCX)
	b.get(x64.RAX, pSrc)
	a.MovLoad(x64.RAX, x64.RAX, StrBytes)
	b.put(pN, x64.RAX)

	yes, no := b.label(), b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, no) // 空文本不是一条从根起的路
	b.get(x64.RSI, pSrc)
	a.AddImm(x64.RSI, strHead)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	// 第 0 个字节是分隔符：/x 与 \x
	a.CmpImm(x64.RAX, '/')
	a.Jcc(x64.E, yes)
	a.CmpImm(x64.RAX, '\\')
	a.Jcc(x64.E, yes)
	// 第 1 个字节是冒号、第 0 个是字母：C: 那一侧（大小写都算）
	b.get(x64.RAX, pN)
	a.CmpImm(x64.RAX, 2)
	a.Jcc(x64.B, no) // 不足两个字节，摆不下"字母＋冒号"
	a.MovZX8(x64.RAX, x64.RSI, 1)
	a.CmpImm(x64.RAX, ':')
	a.Jcc(x64.NE, no)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.OrImm(x64.RAX, 0x20) // 只有真是字母时这一并才落到 a～z 那一段
	a.AddImm(x64.RAX, -'a')
	a.CmpImm(x64.RAX, 25)
	a.Jcc(x64.BE, yes)
	a.Mark(no)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
	a.Mark(yes)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
}

// @env_chdir(rcx=路径)：换当前目录，返回 rax=1 换成了、0 没换成。
//
// 答复风格与 file.create_directories 同一条：问的是"这一件事办成了没有"，路不存在、
// 那是一个文件都不是目录，都算 0，不抛——操作系统那一条错误码在这里没有意义，
// 因为调用方下一句自己会去问那条路在不在（file.exists）。
// SetCurrentDirectoryW 返回的是 BOOL（DWORD），不洗高位就没法判成败，与 env_set 同一条。
//
// en: @env_chdir (rcx = path): change the current directory, returning rax = 1 changed / 0 not
// changed. The answer style matches file.create_directories: the question is
// "did this get done", and a nonexistent path or a file rather than a directory both answer 0
// without throwing - the OS error code would be meaningless here because the caller's next
// sentence asks whether the path exists (file.exists). SetCurrentDirectoryW returns a BOOL
// (DWORD), so the upper half must be washed before judging success, the same as env_set.
func envChdir(b *rt) {
	const (
		hName = 0
		hLen  = 1 // 路径几字节，只为"太长才算坏"那一条留着
		hOK   = 2
	)
	a := b.a
	b.prologue()
	b.put(hName, x64.RCX)
	bad := b.label()
	widen(b, hName, hLen, "aWide", bad)

	b.leaRIP(x64.RCX, "aWide")
	b.impCall("SetCurrentDirectoryW")
	zeroExtend32(b, hOK)
	b.get(x64.RAX, hOK)
	a.Test(x64.RAX, x64.RAX)
	done := b.label()
	a.Jcc(x64.NE, done)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
	a.Mark(done)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
	// 路径太长，翻不成宽字符：这一条与 file 那一组同一条答复（10051 号那一句），
	// 不是"换不成"——摆不进去操作系统的那一侧的确一条路都没问。
	a.Mark(bad)
	b.throwWith(codeBadPath, "sBadPath")
	b.epilogue()
}

// @tz_offset()：返回 rax=本地时间比 UTC 快几分钟。
//
// GetTimeZoneInformation 那个位置的 Bias 记的是"本地＝UTC 减几分钟"，所以取个负号返回去，
// 东八区是 480。答 daylight 那一档时再加上 DaylightBias，夏令时就跟着进去了；
// unknown（这台机器没设时区）只剩 Bias 可用，也照负号返回。
// 结构体那一侧要 172 字节，帧里的格子摆不下，向堆要一块临时区，问完就归还——
// 与 str_pad 那几块翻倍的中间文本是同一条做法（块还回链上，下一圈直接拿得回来）。
// 操作系统答 FAILED 时返回 0：没有时区信息就当没有偏移，不抛。
//
// en: @tz_offset(): returns rax = how many minutes local time is ahead of UTC.
// GetTimeZoneInformation's Bias field records "local = UTC minus N minutes", so the sign
// is flipped on the way out and UTC+8 answers 480. When the daylight rung is answered,
// DaylightBias is added too so daylight saving comes along; unknown (this machine has no time
// zone set) leaves only Bias, still returned with the sign flipped. The structure needs 172
// bytes, more than frame slots can hold, so a temporary block is taken from the heap and
// returned after the question - the same approach as the doubling intermediate text in str_pad
// (the block goes back on the free chain and the next round takes it straight back). If
// the operating system answers FAILED, 0 is returned: no time zone
// information means no offset, and nothing is thrown.
func tzOffset(b *rt) {
	const (
		tBuf  = 0 // 那块临时区（TIME_ZONE_INFORMATION 本身 172 字节，凑到 8 的倍数）
		tKind = 1 // 操作系统答的那一档
	)
	a := b.a
	b.prologue()
	a.MovImm(x64.RCX, 184)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.put(tBuf, x64.RAX)

	b.get(x64.RCX, tBuf)
	b.impCall("GetTimeZoneInformation")
	zeroExtend32(b, tKind) // 交回的是 DWORD：洗成无符号才好认 FAILED 那一档

	// Bias 在结构体第 0 格，是有符号 32 位：取出后把符号扩到 64 位（左移 32、算术右移 32）
	b.get(x64.RAX, tBuf)
	a.MovZX32(x64.RDX, x64.RAX, 0)
	a.ShlImm(x64.RDX, 32)
	a.SarImm(x64.RDX, 32)

	// 答 daylight 那一档时再加上第 168 格的 DaylightBias；standard 与 unknown 只剩 Bias
	b.get(x64.RAX, tKind)
	a.CmpImm(x64.RAX, 2)
	notDay := b.label()
	a.Jcc(x64.NE, notDay)
	b.get(x64.RCX, tBuf)
	a.MovZX32(x64.R8, x64.RCX, 168)
	a.ShlImm(x64.R8, 32)
	a.SarImm(x64.R8, 32)
	a.Add(x64.RDX, x64.R8)
	a.Mark(notDay)

	b.get(x64.RCX, tBuf)
	b.call("@free") // 问完了，这一格里没有要留的东西

	// FAILED（0xFFFFFFFF）那一档：没有时区信息就当没有偏移，返回 0，不抛
	b.get(x64.RAX, tKind)
	a.MovImm(x64.RCX, int64(0xFFFFFFFF))
	a.Cmp(x64.RAX, x64.RCX)
	failed := b.label()
	a.Jcc(x64.E, failed)
	a.Mov(x64.RAX, x64.RDX)
	a.Neg(x64.RAX) // 本地＝UTC 减 Bias 分钟，取负交回去就是"本地快几分钟"
	b.epilogue()

	a.Mark(failed)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
}

// @env_cwd()：返回 rax=当前在哪个目录里跑。
//
// 与 env_get 同一条两段式：先传空缓冲问要几个宽字符（含结尾那个 0），再按那个数开一块真读。
//
// en: @env_cwd(): returns rax = the directory this program is running in. The same
// two-step as env_get: first ask with an empty buffer how many wide characters are needed
// (including the terminating zero), then allocate that much and read for real.
func envCwd(b *rt) {
	const (
		cNeed = 0
		cBuf  = 1
		cGot  = 2
	)
	a := b.a
	b.prologue()
	a.Xor(x64.RCX, x64.RCX)
	a.Xor(x64.RDX, x64.RDX)
	b.impCall("GetCurrentDirectoryW")
	zeroExtend32(b, cNeed)
	b.get(x64.RAX, cNeed)
	a.Test(x64.RAX, x64.RAX)
	miss := b.label()
	a.Jcc(x64.E, miss)

	b.get(x64.RCX, cNeed)
	a.ShlImm(x64.RCX, 1)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.put(cBuf, x64.RAX)

	b.get(x64.RCX, cNeed)
	b.get(x64.RDX, cBuf)
	b.impCall("GetCurrentDirectoryW")
	zeroExtend32(b, cGot)

	wideToText(b, cBuf, cGot)
	b.epilogue()

	a.Mark(miss)
	b.throwWith(codeEnvAsk, "sEnvAsk")
	b.epilogue()
}

// @sleep(rcx=几毫秒)：让出当前这段时间。
//
// 负数先清成 0 再交给 Sleep：那个参数是四个字节无符号的，负的一传过去就成了四十多亿毫秒。
// 0 也照样传——那一句"等 0 毫秒"本来就是"让出这一次时间片"，这是系统定义的行为。
//
// en: @sleep (rcx = milliseconds): yield the current time slice's worth of wait. A negative is
// zeroed before handing to Sleep: that parameter is a four-byte unsigned number and a negative
// passed over becomes four billion milliseconds. 0 is still passed - "wait 0 ms" is precisely
// "yield this time slice", and that is the system's definition.
func sleepMs(b *rt) {
	a := b.a
	b.prologue()
	a.Test(x64.RCX, x64.RCX)
	negative := b.label()
	a.Jcc(x64.S, negative)
	b.impCall("Sleep")
	b.epilogue()
	a.Mark(negative)
	a.Xor(x64.RCX, x64.RCX)
	b.impCall("Sleep")
	b.epilogue()
}

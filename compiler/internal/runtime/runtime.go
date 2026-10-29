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

// Package runtime 是 nef 自带的一组运行期例程。
//
// 它们用同一个指令编码器手工构建，整块挂在用户代码后面：往屏幕写字、把整数变成十进制、
// 码点编成 UTF-8、退出。产物只依赖 kernel32，不链接 C 运行时，也不需要本机装什么。
//
// 每个例程共用同一张帧表：入口 push rbp 之后 rsp = rbp-0x100。低 0x20 字节是给被调方
// 的影子空间，第五个参数落在 [rbp-0xE0]，局部量从 [rbp-8] 往下数。只用调用方寄存器
// （rax、rcx、rdx、rsi、rdi、r8～r11），所以例程之间不必互相保存。
//
// 例程之间的调用有方向：@write 排最前，其余按各自依赖排在后面。@alloc 要抛 10091 号，
// 只能排在 @throw 之后。
//
// en: Package runtime is the small set of routines nef carries itself. They
// are hand-built with the same instruction encoder and hung as one block behind the user code:
// write to the screen, turn an integer into decimal, encode a code point as UTF-8, exit. The
// product depends only on kernel32, links no C runtime and needs nothing installed on this
// machine.
// Every routine shares one frame table: after the entry's push rbp, rsp = rbp-0x100. The low
// 0x20 bytes are the callee's shadow space, the fifth argument lands at [rbp-0xE0], and locals
// count down from [rbp-8]. Only caller-saved registers are used (rax, rcx, rdx, rsi, rdi,
// r8-r11), so routines need not preserve anything for each other. Calls between routines have
// an order: @write sorts first and the rest follow their dependencies. @alloc throws code
// 10091, so it can only be placed after @throw.
package runtime

import (
	"encoding/binary"
	"unicode/utf8"

	"neflang.org/nf/internal/x64"
)

// 内建错误的码。这一段放的是几条没归到某一组文件里的号，加上"兜底"那两个整十号；
// 各组自己的号（41、51、60、61、71…）跟那条例程放在一起。
// 一律 10000 往上：0～9999 留给程序自己写，两段隔开，程序写出的号不会被误认成内建号。
//
// en: The built-in error codes. This block holds the few codes that belong to no group
// file, plus the two round-number fallbacks; each group's own codes (41, 51, 60, 61, 71...) sit
// next to the routine that uses them. All of them are 10000 and up: 0-9999 are left for the
// program to write, and separating the bands keeps anyone from thinking they hit a
// built-in code.
const (
	codeOverflow   = 10021 // 整数运算溢出
	codeEmptyRange = 10020 // math.random 的范围是空的
	codeCell       = 10030 // 容器与字符串那一段的兜底：眼下只有 bytes.new 收的那一个数用着它
	codeIndex      = 10031 // 下标越界：容器与文本同一条
	codeNoKey      = 10032 // dict 没有这个键
	codeNoRoom     = 10091 // 向操作系统要不到内存
	codeStack      = 10093 // 调用套得太深，栈用完了
)

// 帧里的位置。
//
// en: Positions inside the frame.
const (
	frameSize = 0x100
	arg5      = -0xE0 // 第五个参数落点＝[rsp+0x20]；往后的每一个槽往 rbp 那一侧靠，与 codegen 同一张表
	arg6      = -0xD8
	arg7      = -0xD0
	arg8      = -0xC8
	// 第九、第十个参数往后排：`CreateProcessW` 有十个入参，前四个走寄存器，
	// 第五到第八已经是 arg5～arg8，起进程那条例程还要两个参数槽位。这一档的帧只用得起
	// 前二十个槽位的局部量，与这两个槽位（第 23、22 个）不会重叠。
	arg9    = -0xC0
	arg10   = -0xB8
	digits  = -0x28 // 数字缓冲区的上界，24 字节到 [rbp-0x40)
	utf8Buf = -0x20 // 最多四个字节

	// 容器对象里那四个字段（句柄指向对象头之后的第一个位置）。
	// 每格 8 字节，位置里存的就是中间码那个槽的值——窄元素（boolean、byte、unicode）
	// 在容器里也占满 8 字节，为的是容器只有一种搬法。
	cLen   = 0  // 有几样东西
	cCap   = 8  // 数据块能装几样
	cData  = 16 // 数据块在堆上的起点
	cVer   = 24 // 改过几次：遍历途中一旦发现它变了就抛 10033 号
	cMode  = 32 // 键怎么算相等与散列：0 比那 64 位，1 按文本内容
	cObjSz = 40 // 容器对象本身占几字节（不含那 8 字节的对象头）
	minCap = 4  // 空容器先按这个容量要数据块
)

// 下面这份偏移是给降级与发射器看的同一张表：容器对象的五个字段、桶里那三个槽位、
// 文本对象的前两个槽位。运行期例程与中间码都照它摆，任何一方不得自行另算一遍。
//
// en: The offsets below are one table read by both lowering and the emitter: the
// container object's five fields, the three slots in a bucket, and the first two slots of a
// text object. Both the runtime routines and the intermediate code lay out by it, and neither
// side may count them again.
const (
	CellLen   = cLen  // 容器里有几样东西
	CellCap   = cCap  // 数据块能装几样
	CellData  = cData // 数据块在堆上的起点
	CellVer   = cVer  // 改过几次：10033 号的判断靠它
	CellMode  = cMode // 键怎么算散列与相等，造表那一刻写进来
	CellSize  = cObjSz
	BucketPad = stride // 一格桶占几字节
	BucketKey = bKey   // 键在桶里第几格：遍历 dict 交的就是这个位置
	BucketVal = bVal   // 值在桶里第几格：`d[k]` 与 `d[k]=v` 都落在这个位置
	StrBytes  = 0      // 文本：UTF-8 占几字节
	StrCount  = 8      // 文本：有几个码点，length(文本) 读这个位置
)

// RefKind 是一处 RIP 相对引用最后指到哪儿。
//
// en: RefKind says where a RIP-relative reference finally points.
type RefKind int

const (
	RefData RefKind = iota // 运行期自己的静态数据，Name 查 DataAt
	RefIAT                 // 导入表里的第 Off 格
)

// Ref 是代码里一个待回填的位置，At 是它在整块 Code 里的偏移。
//
// en: Ref is one position in the code awaiting backfill; At is its offset within the whole Code
// block.
type Ref struct {
	At   int
	Kind RefKind
	Name string
	Off  int
}

// Part 是一个例程在整块 Code 里占的那一段。Parts 按顺序把 Code 铺满，不重叠。
//
// en: Part is the span one routine occupies within the whole Code block. Parts tile Code in
// order without overlap.
type Part struct {
	Name string
	Off  int
	Refs []Ref
}

// Import 是一个位置导入：函数名，加上它在哪个 DLL 里。
//
// 产物只向 kernel32.dll 要东西、不链接 C 运行库；只有一个 DLL 的时候光记名字就够，
// 网络那一层还要问 `ws2_32.dll` 与 `winhttp.dll`，所以每个名字得带上自己的模块。
//
// en: Import is one imported symbol: the function name plus the DLL it lives in. The product
// asks only kernel32.dll and links no C runtime; with one DLL, recording names was enough.
// The network layer also asks ws2_32.dll and winhttp.dll, so every name must carry its own
// module.
type Import struct {
	DLL  string
	Name string
}

// kernel32、ws2_32 与 winhttp 是这张表现在的三个模块。从前只有一个导入库，
// 网络这一层改成"每个名字带着自己那一个"：winsock 在 `ws2_32.dll`、
// HTTP 客户端在 `winhttp.dll`，两个都是每台 Windows 都自带的系统 DLL，也不引 C 运行时。
//
// en: kernel32, ws2_32 and winhttp are the three modules in the table today. Formerly there
// was a single import library; the network layer gives every name its own: winsock lives in
// ws2_32.dll and the HTTP client in winhttp.dll, both system DLLs present on every Windows,
// and still no C runtime.
const (
	kernel32 = "kernel32.dll"
	ws2      = "ws2_32.dll"
	// winhttp 是第三个模块（网络那一层的下半）：HTTP 客户端整个交给它，
	// TLS、重定向与证书由系统做。仍是每台 Windows 都自带的系统 DLL，也不引 C 运行时，
	// "拷走就能跑"不受影响——同一条理由在接进 ws2_32 时已经说过一遍。
	winhttp = "winhttp.dll"
)

// importsOf 把一串名字并成同一个模块的导入，写 imports 那张表用的。
//
// en: importsOf merges a run of names into one module's imports, used to write the imports
// table.
func importsOf(dll string, names ...string) []Import {
	out := make([]Import, 0, len(names))
	for _, n := range names {
		out = append(out, Import{DLL: dll, Name: n})
	}
	return out
}

// Blob 是一次 Build 的全部产出。
//
// en: Blob is everything one Build produces.
type Blob struct {
	Code    []byte
	Parts   []Part
	Data    []byte
	DataAt  map[string]int
	Imports []Import
}

// Build 落全部例程。@write 排在第一个：其余几个都要 call 它。
//
// en: Build emits all routines. @write goes first: every other one calls it.
func Build() Blob {
	b := newRt()
	b.routine("@write", write)
	// 打印的去向（0 标准输出、1 标准错误）：@write 读它，打印点在写每一段之前先摆好
	b.routine("@out_err", outErr)
	b.routine("@put_int", putInt)
	b.routine("@put_str", putStr)
	b.routine("@put_char", putChar)
	b.routine("@put_bool", putBool)
	b.routine("@put_space", putSpace)
	b.routine("@put_nl", putNL)
	// @exit 要叫它，所以它排在 @exit 前面；它自己要的 @write／@put_int／@put_nl 都在更前面。
	b.routine("@heap_report", heapReport)
	b.routine("@exit", exitProc)
	// winsock 的开机那一步：`nef.main` 的第一条指令叫它
	b.routine("@winsock_start", winsockStart)
	// @free 排在 @throw 之前：抛出那一条路要释放摘下来的处理器记录（@throw_walk call @free）。
	b.routine("@free", free)
	// 那一句"出错了（码）：话"：链走到空那一步与 nef.main 顶上那条兜底处理器两条路共用。
	b.routine("@put_errline", putErrline)
	// 栈用完了那一条：每一帧开场比一次栈，掉到下限之下就跳进这里。
	b.routine("@die_stack", dieStack)
	// @throw 只做"把码与话存进静态那两个槽、往下走"，走路的是 @throw_walk：
	// 帧内清理记录还完自己那一帧的释放要叫回 @throw_walk 接着往外走，所以它必须先编好。
	b.routine("@throw_walk", throwWalk)
	b.routine("@throw", throw)
	b.routine("@die", die)
	// @alloc 要抛 10091 号，所以它排在 @throw 后面。
	b.routine("@alloc", alloc)
	// struct 的整份拷贝：值类型每次交出都另起一块，标记照抄源的那一个
	b.routine("@copy_block", copyBlock)
	// `bytes` 那一档：三条都只要 @alloc 与 @throw，slice 要先有 bytes_new。
	// 文本与文件那四条换到这一档上来，所以排在那几条之前（@str_to_bytes、@bytes_str、
	// @file_read_bytes 都要叫 @bytes_new）。
	b.routine("@bytes_new", bytesNew)
	b.routine("@bytes_at", bytesAt)
	b.routine("@bytes_slice", bytesSlice)
	// 清单里那几块连着摆成一块新的，要先有 bytes_new。
	b.routine("@bytes_join", bytesJoin)
	// 处理器链：@throw_walk 识别它，接住块进出各挂一次，登记过东西的帧开场再挂一条帧内清理记录
	b.routine("@push_handler", pushHandler)
	b.routine("@push_abandon", pushAbandon)
	b.routine("@pop_handler", popHandler)
	b.routine("@str_cat", strCat)
	b.routine("@list_new", listNew)
	b.routine("@list_add", listAdd)
	b.routine("@list_at", listAt)
	b.routine("@list_touch", listTouch)
	b.routine("@str_eq", strEq)
	b.routine("@list_remove", listRemove)
	// 文本这一支：先能比字，再能切一截，最后才谈得上按分隔切
	b.routine("@str_cmp", strCmp)
	b.routine("@str_sub", strSub)
	b.routine("@str_split", strSplit)
	b.routine("@str_to_int", strToInt)
	// 带小数点的那一种数从文本进来：识别不出的写法与上面同一条号
	b.routine("@str_to_float", strToFloat)
	// 文本上的下标与遍历：先能解一个码点，再能跳到下一个位置
	b.routine("@str_at", strAt)
	b.routine("@str_dec", strDec)
	b.routine("@str_adj", strAdj)
	// 访问操作系统的那几条：翻译与校验排前面，@file_read 与 @read_line 在后头用它们
	b.routine("@count_runes", countRunes)
	b.routine("@to_wide", toWide)
	b.routine("@utf8_ok", utf8Ok)
	b.routine("@file_read", fileRead)
	// M2b 那七条：属性三条共用 @file_attrs，目录两条共用 @file_list（差一个标志格），
	// 分块两条一条读一段、一条盖一段。顺序都在 @to_wide／@str_sub／@list_add／@free 之后。
	b.routine("@file_attrs", fileAttrs)
	b.routine("@file_size", fileSize)
	b.routine("@file_modified", fileModified)
	b.routine("@file_readonly", fileReadonly)
	b.routine("@file_list", fileList)
	b.routine("@file_read_from", fileReadFrom)
	b.routine("@file_write_at", fileWriteAt)
	// 目录那两条：读的那一条要 @list_new，写的那一条要 @alloc／@free，
	// 两条都在它们之后、也都在 @to_wide 之后。
	b.routine("@file_mkdir", fileMkdir)
	b.routine("@file_is_dir", fileIsDir)
	b.routine("@file_rename", fileRename)
	b.routine("@file_rmdir", fileRemoveDir)
	// 另两条：整份抄一遍，与把缺的那几层一次建齐
	b.routine("@file_copy", fileCopy)
	b.routine("@file_mkdirs", fileMkdirs)
	b.routine("@file_read_bytes", fileReadBytes)
	b.routine("@file_write_bytes", fileWriteBytes)
	b.routine("@read_line", readLine)
	b.routine("@args", args)
	// dict 与 hashset 共用一张表，排在一起的顺序就是它们互相调用的顺序
	b.routine("@hash", hashKey)
	b.routine("@key_eq", keyEq)
	b.routine("@map_find", mapFind)
	b.routine("@map_new", mapNew)
	b.routine("@map_grow", mapGrow)
	b.routine("@map_put", mapPut)
	b.routine("@map_at", mapAt)
	b.routine("@map_contains", mapContains)
	b.routine("@map_remove", mapRemove)
	b.routine("@map_next", mapNext)
	// 小数这一支：先是拆解与比大小的几个基础操作，最后才是 @put_float
	b.routine("@f_mul", fMul)
	b.routine("@f_hi", fHi)
	b.routine("@f_expand", fExpand)
	b.routine("@f_round", fRound)
	b.routine("@f_digit", fDigit)
	b.routine("@f_cmp", fCmp)
	b.routine("@f_layout", fLayout)
	b.routine("@put_float", func(x *rt) { putFloat(x, false) })
	// 取模排在最后：它若要打印或借用别的例程，前面那些都已经编完了
	b.routine("@f_order", fOrder)
	b.routine("@fmod", fMod)
	// 把值变成文本（number.to_string）：@str_wrap 是共同的收尾，所以它排在四个 @*_str 前面
	b.routine("@str_wrap", strWrap)
	b.routine("@int_str", intStr)
	b.routine("@char_str", charStr)
	b.routine("@bool_str", boolStr)
	b.routine("@float_str", func(x *rt) { putFloat(x, true) })
	// 挂在文本与 list 上的动作：查找是基础，其余几条都靠它或靠 @str_sub 造结果
	b.routine("@str_index", strIndex)
	b.routine("@str_contains", strContains)
	b.routine("@str_case", strCase)
	b.routine("@str_trim", strTrim)
	b.routine("@str_rune_off", strRuneOff)
	b.routine("@str_slice", strSlice)
	b.routine("@str_replace", strReplace)
	b.routine("@str_to_list", strToList)
	b.routine("@list_join", listJoin)
	// 容器与文本三条：比较那一侧沿用 @str_eq／@str_cmp／@f_order 已有的三支判断。
	b.routine("@str_index_of", strIndexOf)
	b.routine("@str_edges", strEdges)
	// 两头补位：凑那一段填充用翻倍，收尾按码点裁齐
	b.routine("@str_pad", strPad)
	b.routine("@list_index_of", listIndexOf)
	b.routine("@list_sort", listSort)
	b.routine("@dict_entries", dictEntries)
	// 这三条：一头一尾与整只清空走的是 @list_at 同一条判断，集合三条造新表走 @map_*
	b.routine("@list_pop_last", listPopLast)
	b.routine("@set_op", setOp)
	// math 组：整数那四条自己算，小数那几条借 @f_order 的方向掩码
	b.routine("@i_abs", iAbs)
	b.routine("@f_abs", fAbs)
	b.routine("@i_min", iMin)
	b.routine("@i_max", iMax)
	b.routine("@f_min", fMin)
	b.routine("@f_max", fMax)
	b.routine("@i_pow", iPow)
	// 小数取整返回整数那三条叫 @math_*：@f_round 那个名字已经被"收成 p 位小数"占着，
	// 重名的话 routine 会把标号覆盖，先前那条 call 就跳到别人身上去了。
	b.routine("@math_floor", intFloor)
	b.routine("@math_ceil", intCeil)
	b.routine("@math_round", intRound)
	b.routine("@f_sqrt", fSqrt)
	// 后补的数学那五条：一个常数、两条三角、一条自然对数、一条小数随机。
	// 后四条走 x87（这台机器上没有用 SSE 指令算正弦的做法），见 `x64` 里那几条编码。
	b.routine("@f_pi", fPi)
	b.routine("@f_sine", fSine)
	b.routine("@f_cosine", fCosine)
	b.routine("@f_natural_log", fNaturalLog)
	b.routine("@f_random_float", fRandomFloat)
	// 两条时钟：只向系统取值，不碰文本，所以排在最末尾也不依赖谁
	b.routine("@time_now", timeNow)
	b.routine("@time_elapsed", timeElapsed)
	b.routine("@time_parts", timeParts) // 把秒数拆成内建 Calendar 那七个整数
	b.routine("@sleep", sleepMs)
	// path 与 environment 两组：都靠 aWide／wPath／aBuf 那几块静态缓冲，
	// 彼此之间不嵌套调用，所以谁先谁后都行；唯一的顺序要求是它们都在 @to_wide、
	// @str_sub、@alloc、@throw 之后（这几条已经被上面的注册排在前面了）
	b.routine("@path_join", pathJoin)
	b.routine("@path_file_name", pathFileName)
	b.routine("@path_extension", pathExtension)
	b.routine("@path_exists", pathExists)
	b.routine("@env_get", envGet)
	b.routine("@env_set", envSet)
	b.routine("@env_unset", envUnset)
	b.routine("@env_cwd", envCwd)
	// 往外写的三条（file.exists 复用上面那条 @path_exists，降级那头直接指过去）
	b.routine("@file_write", fileWrite)
	b.routine("@file_append", fileAppend)
	b.routine("@file_remove", fileRemove)
	// math 最后一条：随机数。它只用 kernel32 的计数器，不碰文本也不碰堆
	b.routine("@i_random", iRandom)
	// regular_expression 那一组。匹配机只用自己那三块静态缓冲（指令块、线程表、编译期那一摞），
	// 不碰操作系统。顺序：编译要 @rx_ensure，@rx_scan 要 @rx_at，四条入口要前面全部
	b.routine("@rx_ensure", rxEnsure)
	b.routine("@rx_compile", rxCompile)
	b.routine("@rx_at", rxAt)
	b.routine("@rx_scan", rxScan)
	b.routine("@regex_match", regexMatch)
	b.routine("@regex_find", regexFind)
	b.routine("@regex_replace", regexReplace)
	b.routine("@regex_split", regexSplit)
	// 挂在 list 上的六条。顺序要紧：add_all 与 slice 都要先有 @list_add、
	// to_set 要先有 @map_new 与 @map_put、slice 与 add_all 拷文本要先有 @str_sub。
	b.routine("@list_reverse", listReverse)
	b.routine("@list_insert", listInsert)
	b.routine("@list_shift_out", listShiftOut)
	b.routine("@list_add_all", listAddAll)
	b.routine("@list_slice", listSlice)
	b.routine("@list_to_set", listToSet)
	// 挂在文本、数字、路径、目录与数学上的其余各条。
	// 顺序要紧：@bytes_str 与 @b64_decode 要先有 @utf8_ok 与 @str_sub，
	// @num_str_base 要先有 @str_wrap，@file_remove_tree 要先有 @file_walk。
	b.routine("@str_rindex", strRIndex)
	b.routine("@str_trim_head", strTrimHead)
	b.routine("@str_trim_tail", strTrimTail)
	// `text.escape_json`：只叫 @alloc，所以跟着这一批文本动作排。它是 web 端
	// 自动化测试框架的前置：每一条 CDP 命令都是一段手拼的 JSON。
	b.routine("@str_json_escape", strJsonEscape)
	b.routine("@str_repeat", strRepeat)
	b.routine("@str_to_bytes", strToBytes)
	b.routine("@bytes_str", bytesToStr)
	b.routine("@b64_encode", b64Encode)
	b.routine("@b64_decode", b64Decode)
	// 同一对动作换到 `bytes` 那一档（web 端自动化测试框架的前置：TCP 上收来的帧是字节，
	// 要写成 base64 才进得了 JSON；反过来 `bytes.from_base64` 返回的二进制不验 UTF-8）。
	b.routine("@bytes_b64_encode", b64BytesEncode)
	b.routine("@bytes_b64_decode", b64BytesDecode)
	b.routine("@str_int_base", strToIntBase)
	b.routine("@num_str_base", numToStrBase)
	b.routine("@i_sign", iSign)
	b.routine("@f_sign", fSign)
	b.routine("@set_seed", setSeed)
	b.routine("@f_tangent", fTangent)
	b.routine("@f_arctangent", fArctangent)
	b.routine("@f_log_base_10", fLogBase10)
	b.routine("@f_exponential", fExponential)
	b.routine("@path_dir", pathDir)
	b.routine("@path_abs", pathAbs)
	b.routine("@env_chdir", envChdir)
	b.routine("@tz_offset", tzOffset)
	b.routine("@file_walk", fileWalk)
	b.routine("@file_remove_tree", fileRemoveTree)
	// `net` 那一组。全部排在最后：@net_fail 要叫 @str_cat／@int_str／@throw，
	// 其余每一条又要叫 @net_fail，而注册顺序就是依赖顺序（拿不到标号的表现是"标号 0 没有落点"）。
	b.routine("@net_fail", netFail)
	b.routine("@net_connect", netConnect)
	b.routine("@net_listen", netListen)
	b.routine("@net_accept", netAccept)
	b.routine("@net_send", netSend)
	b.routine("@net_recv_into", netRecvInto)
	b.routine("@net_close", netClose)
	// `http` 那一组：一次请求由这五条排成一条直路（`httpAction` 那头）。
	// 顺序要紧：@http_headers 要 @list_new／@str_sub／@free，@http_body 要 @bytes_new／@alloc，
	// 五条的失败那一路都要 @net_fail，所以全排在 net 那一组之后。
	b.routine("@http_begin", httpBegin)
	b.routine("@http_status", httpStatus)
	b.routine("@http_headers", httpHeaders)
	b.routine("@http_body", httpBody)
	b.routine("@http_close", httpClose)
	// `process` 那一档：起一个程序、等它跑完拿退出码、不再等待就结束它。
	// 顺序要紧：三条都要 @to_wide／@str_cat／@int_str／@free／@throw，@proc_start 还要 CloseHandle
	// 那一个导入名字——失败那一路都要 @proc_fail，所以四条连续排在一起。
	b.routine("@proc_fail", procFail)
	b.routine("@proc_start", procStart)
	b.routine("@proc_wait", procWait)
	b.routine("@proc_close", procClose)
	code, refs := b.finish()

	data, at := dataBytes()
	return Blob{
		Code:    code,
		Parts:   partsOf(b.order, refs),
		Data:    data,
		DataAt:  at,
		Imports: imports,
	}
}

// imports 的顺序就是 SecIAT 里的序号，例程里按名字查。同一个 DLL 的名字要连着写：
// 装 PE 时按一段一段切描述符（internal/pe 的 runsOf），中间夹进别家的名字装不出来。
//
// en: The order of imports is exactly the numbering in SecIAT, and routines look theirs up by
// name. Names from the same DLL must be written adjacently: PE assembly cuts the descriptors
// run by run (runsOf in internal/pe), and interleaving another module's name cannot be
// assembled.
var imports = groupImports(
	importsOf(kernel32,
		"GetStdHandle", "WriteFile", "ExitProcess", "VirtualAlloc",
		"CreateFileW", "ReadFile", "GetFileSizeEx", "CloseHandle",
		"MultiByteToWideChar", "WideCharToMultiByte", "GetCommandLineW",
		"GetSystemTimeAsFileTime", "QueryPerformanceCounter", "QueryPerformanceFrequency",
		"GetEnvironmentVariableW", "SetEnvironmentVariableW", "GetCurrentDirectoryW", "GetFileAttributesW",
		"DeleteFileW",
		// M2b 那七条要的四个：列目录三个、分块定位一个。
		"FindFirstFileW", "FindNextFileW", "FindClose", "SetFilePointerEx",
		// 目录那九条要的：目录三条、问一次失败原因、等一会儿。
		"CreateDirectoryW", "RemoveDirectoryW", "MoveFileW", "GetLastError", "Sleep",
		// 另两条：整份抄一遍走 CopyFileW，一层一层建还是用上面的 CreateDirectoryW。
		"CopyFileW", "SetCurrentDirectoryW", "GetTimeZoneInformation",
		// `process` 那一档：起一个程序、等它跑完、问退出码、不再等待就结束它。
		// 关句柄那一条（CloseHandle）文件与目录那一路早就进来了，这里只补四个。
		"CreateProcessW", "WaitForSingleObject", "GetExitCodeProcess", "TerminateProcess",
	),
	// 网络这一层：winsock 开机与收尾，加上 `net` 那一组要问的十五条，共十七个名字。
	// 同一个模块的名字必须连着排（internal/pe 的 runsOf 按段切描述符），所以这一串一口气列全。
	importsOf(ws2, "WSAStartup", "WSACleanup",
		"socket", "closesocket", "connect", "bind", "listen", "accept", "send", "recv",
		"setsockopt", "getsockopt", "ioctlsocket", "select", "GetAddrInfoW", "FreeAddrInfoW",
		"WSAGetLastError"),
	// `http` 那一组：会话、连接、请求三步各拿一只句柄，送出去、收答复、问状态码与头、
	// 读正文、摆超时、关句柄——九个名字。网址那三段由这一层自己扫（见 internal/runtime/http.go
	// 的 @http_begin），不走 WinHttpCrackUrl：那一条要的是 URL_COMPONENTS 的字节形状。
	importsOf(winhttp, "WinHttpOpen", "WinHttpConnect", "WinHttpOpenRequest", "WinHttpSendRequest",
		"WinHttpReceiveResponse", "WinHttpReadData", "WinHttpQueryHeaders", "WinHttpSetTimeouts",
		"WinHttpCloseHandle"))

// groupImports 把按模块分好的几串导入并成那一张表，并的顺序就是模块排在一起的顺序。
// 三个模块再往 `append` 里塞就得套两层，这一条把那一层省掉。
//
// en: groupImports merges the per-module name runs into that one table, and the merge order is
// the order the modules stand in. Three modules fed into append would need two nesting levels;
// this routine removes one.
func groupImports(gs ...[]Import) []Import {
	var out []Import
	for _, g := range gs {
		out = append(out, g...)
	}
	return out
}

// VirtualAlloc 的四个参数与几种标志位。
//
// en: VirtualAlloc's four parameters and the relevant flags.
const (
	allocCommitReserve = 0x3000 // MEM_COMMIT | MEM_RESERVE
	pageReadWrite      = 0x04   // PAGE_READWRITE
	minChunk           = 0x100000
	page               = 0x1000
)

// 空闲链表的形状。
//
// 档：第 k 档装总尺寸（含 8 字节的头）落在 (8<<k, 8<<(k+1)] 的那些块，最大那一档封顶
// 在 8<<16 = 512 KiB。比它更大的块不进链——释放后不再复用，理由是不为超大块
// 另设第二套拆分与合并机制。
//
// 链上挂的是**数据指针**而不是块的起点，所以下一个位置的指针写进数据区的前 8 字节，
// 头上那两个槽（类型字与尺寸）保持完整：从链上取回来时还知道这块有多大、要清零几个字节。
// 因此数据区至少要有 8 字节可写——@alloc 把不足 8 的请求先垫到 8（见 alloc 里那一句）。
//
// 两个直接返回、不做处理的入口是设计的一部分而不是遗漏：头里的尺寸是 0 说明这块不在堆上
// （静态文本池里每条字面量前面那 8 字节占位头正是全 0，见 internal/codegen/data.go），
// 头里的类型字是 FreeSentinel 说明它已经挂在链上（重复释放变成空操作，而不是把链弄断）。
//
// en: The free-list shape. Bands: band k holds blocks whose total size (the 8-byte
// header included) falls in (8<<k, 8<<(k+1)], and the largest band caps at 8<<16 = 512 KiB.
// Anything bigger never enters the chain - releasing one is treated as losing it, because a
// second splitting-and-coalescing scheme for enormous blocks is not wanted. What hangs on the
// chain is the DATA POINTER rather than the block start, so the next pointer is written into
// the first 8 bytes of the data area and the header's two slots (type word and size) stay
// intact: a block taken back off the chain still says how big it is and how many bytes to zero.
// That means the data area needs at least 8 writable bytes, which is why @alloc rounds a
// request below 8 up to 8 (the line inside alloc). The two do-nothing entry points are part of
// the design, not omissions: size 0 in the header means this block is not on the heap (the
// 8-byte placeholder headers in front of each text-pool literal are exactly all zero - see
// internal/codegen/data.go), and a header type word of FreeSentinel means it already lies on
// the chain, so returning it twice is a no-op instead of breaking the chain.
const (
	// 档分两段：**低段一档一种尺寸**——总尺寸 16～512 每 8 字节
	// 一档（63 档），往上才是 1024／2048／…／512 KiB 那 10 档。低段精确分档的理由：一档装 (u/2, u]
	// 时同档里必然有两种尺寸，"余量 ≥16 才切"那一刀对**只差 8 字节的一对**（文本键那块
	// 总 32、两字段的 class 那块总 24）切不动——小的一笔把大的那一块占用掉，下一笔要大的那块
	// 时链上只剩小的，于是每圈多 bump 一块（实测 32／64 字节／圈，块全还进了链、水线照涨）。
	freeExactClasses = 63                    // 总尺寸 16～512：一档一种尺寸
	freeClasses      = freeExactClasses + 10 // 再加 1024／2048／…／512 KiB 那 10 档
	freeExactMax     = 512                   // 低段的顶：超过它走翻倍那一段
	freeMinData      = 8
	freeMaxTotal     = 512 << 10
	// freeSplitMin 是"大块切一刀"的门槛：余量至少要塞下一个新的块头（8）加一个挂链的地方（freeMinData）。
	// 切不动就整块给出去——浪费封顶 16 字节，与档的大小无关（见 alloc 里那一段）。
	freeSplitMin = 16
	// FreeSentinel 写进已经挂上空闲链表那块头的类型字那个位置：重复释放时能被识别，变成空操作。
	FreeSentinel = 0x68636565
	// DestroyingTag 是"这一块正在被析构"：析构之前先把它写进同一个槽，于是绕回来的那一趟
	// （同一块被两处指着、或者字段里指回自己）能被直接识别、跳过——环与菱形引用
	// 都只析构一次，不必另外记一张"走到哪析构到哪"的表。写完这一块不会再被读，所以盖掉类型字没有代价。
	DestroyingTag = 0x64657374
)

// utf16z 把一个纯 ASCII 的名字摆成宽字符（结尾带那个 0）。
//
// 只有编译期就定死的名字才这么办——省掉运行期一次 WideCharToMultiByte 的反向调用。
//
// en: utf16z lays a pure-ASCII name out as wide characters with the terminating zero. Only
// names fixed at compile time do this, which saves a reverse WideCharToMultiByte call at
// runtime.
func utf16z(s string) []byte {
	out := make([]byte, 0, 2*(len(s)+1))
	for i := 0; i < len(s); i++ {
		out = append(out, s[i], 0)
	}
	return append(out, 0, 0)
}

// importSlot 按"哪一个模块的哪一个名字"查它在导入表里的那个位置。查不到回 -1：
// 那不是用户写得出来的东西（名字全在 Go 这边写死），回填那一侧会当场报出槽位编号（internal/pe）。
//
// en: importSlot looks up the position in the import table of "which name in which module". -1
// when not found: none of these is something a user can write (all names are fixed on the Go
// side), and the backfill side names the slot number on the spot (internal/pe).
func importSlot(dll, name string) int {
	for i, imp := range imports {
		if imp.DLL == dll && imp.Name == name {
			return i
		}
	}
	return -1
}

func dataBytes() ([]byte, map[string]int) {
	at := map[string]int{}
	var out []byte
	put := func(name string, bs []byte) {
		at[name] = len(out)
		out = append(out, bs...)
	}
	put("hstdout", make([]byte, 8))
	// 标准错误那一个句柄与"这一段字往哪儿写"（0 标准输出、1 标准错误），
	// console.print_error 用的就是这两个槽位；@write 每写一段都先读后者。
	put("hstderr", make([]byte, 8))
	put("outErr", make([]byte, 8))
	put("heapCur", make([]byte, 8))
	put("heapEnd", make([]byte, 8))
	// heapFrom 是第一块 arena 的起点。@heap_report 报的是 `heapCur - heapFrom`：
	// 只涨不还的时候它就是"总共要过多少"，接上复用之后它是"峰值要过多少"（块还回链上
	// 并不还给操作系统）。两条都够测试用——要比的正是这个量。
	put("heapFrom", make([]byte, 8))
	// 空闲链表的 73 个档头（`freeClasses`）。分两段：总尺寸（含那 8 字节的头）≤512 的
	// 按 8 字节一档，共 63 档，**一档里只有一种尺寸**；往上 1024…512 KiB 按 2 的幂十档，
	// 第 k 档装落在 (8<<k, 8<<(k+1)] 的那些块。低段之所以精确分档，是因为幂档里"两种尺寸只差
	// 8 字节"的一对会互相占用（余量又装不下一个新的块头，切不动）。
	put("freeHeads", make([]byte, 8*freeClasses))
	put("hTop", make([]byte, 8)) // 处理器链的头，只有 @throw_walk／@push_handler／@pop_handler 访问它
	// 抛出那一路的码与话。帧内清理记录一帧一帧往外走，每一帧都要 call 出去、句柄里的寄存器冲干净，
	// 只有静态这两个槽跨得过这一串调用（接住块开头读的还是 rax／rdx，@throw_walk 实现前才摆进去）。
	put("throwCode", make([]byte, 8))
	put("throwMsg", make([]byte, 8))
	// cmdW 是给"在内存里直接跑"留的一个位置：宿主把这一号程序的参数摆成
	// 宽字符写在这里，@args 看见它有内容就不去问 GetCommandLineW——不然拿到的是
	// 编译器的命令行，里面带着 `run 文件.nf` 这两段。
	put("cmdW", make([]byte, 8))
	put("sTrue", []byte("true"))
	put("sFalse", []byte("false"))
	put("sSpace", []byte(" "))
	put("sNL", []byte("\n"))
	put("sErrHead", []byte("出错了（"))
	put("sErrMid", []byte("）："))
	// 一条只有测试用得上的自检：设了环境变量 NEF_HEAP 的程序在退出前打印一行 "heap <字节数>"，
	// 报的是向堆要过的总量（见 @heap_report）。语言面没有这个函数，也不看程序的输出——
	// 它只在环境变量设了的时候才打印，没设的时候 @heap_report 不做任何输出。
	put("envHeap", utf16z("NEF_HEAP"))
	put("sHeapHead", []byte("heap "))
	// sOOM 走的是另一套形状：@throw 收的消息是「字节数 8 + 码点数 8 + UTF-8」
	// 的字符串对象，不是裸字节，所以这一条得自己带上那 16 字节的头。
	// 上面那几条都是直接 call @write、长度写死在指令里，才容得下裸字节。
	put("sOOM", strObject("向操作系统要不到内存"))
	// 栈用完了那一句（10093 号）：只有每一帧开场那一次比较会走到这儿。
	put("sStack", strObject("调用套得太深，栈用完了"))
	// @list_at 自己就要抛 10031 号，所以这句也得是字符串对象——写运行期例程的人不必
	// 先学会降级那一层怎么备消息。
	put("sIndex", strObject("下标越界"))
	// bytes.new 收的那一个数是负的（10030 号，容器那一段的兜底）
	put("sBytesSize", strObject("bytes.new 要的字节数不能是负数"))
	put("sNoKey", strObject("没有这个键"))
	put("sNotNum", strObject("文本读不成数字"))
	put("sRange", strObject("超出了 int 的值域"))
	put("sNullRef", strObject("对着空句柄取成员或调用方法"))
	put("sNoFile", strObject("读不到这个文件"))
	// 10042 号（写不进）与 10041 号的"删不掉"那一支各有各的话，号数沿用既有编号，措辞按现场写
	put("sNoWrite", strObject("写不进去这个文件"))
	put("sNoRemove", strObject("删不掉这个文件"))
	put("sNotUTF8", strObject("文件不是合法的 UTF-8"))
	// M2b 那七条要说的话：10045 号只有一句，10043 号有两句——问属性时遇到目录、与打开目录失败。
	put("sNoAttrs", strObject("拿不到这个文件的属性"))
	put("sIsDir", strObject("那是一个目录，不是一个文件"))
	put("sNoDir", strObject("打不开这个目录"))
	put("sBadPath", strObject("这条路径本机用不了"))
	// 目录那四条：建不出与改不了各有各的号，删空目录回到 10041 号、只换句子。
	put("sMkDir", strObject("建不出这一层目录"))
	put("sNoRename", strObject("这个名字改不了"))
	put("sNoRmDir", strObject("删不掉这个目录"))
	// math.power：指数为负抛 10012 号（号数是定死的），这句话是这一条路自己的
	put("sNegExp", strObject("指数不能是负的"))
	// 运行期自己也要抛 10021 号（math.absolute_value 与 math.power 的溢出），这句话与降级那张表同一个措辞
	put("sOverflow", strObject("整数运算溢出"))
	// math.random：状态一个位置（0 意味着还没取过种子）＋空范围那一句（10020 号）
	put("rndSeed", make([]byte, 8))
	put("sEmptyRange", strObject("随机数的范围是空的：取不到止，起 == 止 也算空"))
	// fHalf 是 0.5 那 8 个字节：@f_round 靠"加上它再向下取整"实现 .5 向正方向进位
	put("fHalf", []byte{0, 0, 0, 0, 0, 0, 0xE0, 0x3F})
	// base64：那两张表与那一句"文本读不成 base64"（10014 号）。
	put("sNotB64", strObject("文本读不成 base64"))
	base64Data(put)
	fileData(put)
	fileOpsData(put)
	osData(put)
	floatData(put)
	regexData(put)
	winsockData(put)
	netData(put)
	httpData(put)
	procData(put)
	return out, at
}

// strObject 按文本对象的形状摆一条字符串：字节数、码点数，再是 UTF-8 字节。
//
// en: strObject lays out a string in the text-object shape: byte count, code point count,
// then the UTF-8 bytes.
func strObject(s string) []byte {
	var hdr [16]byte
	binary.LittleEndian.PutUint64(hdr[0:], uint64(len(s)))
	binary.LittleEndian.PutUint64(hdr[8:], uint64(utf8.RuneCountInString(s)))
	return append(hdr[:], s...)
}

func partsOf(order []namedStart, refs []Ref) []Part {
	parts := make([]Part, len(order))
	for i, o := range order {
		parts[i] = Part{Name: o.name, Off: o.off}
	}
	for _, r := range refs {
		i := 0
		for j, o := range order {
			if o.off <= r.At {
				i = j
			}
		}
		parts[i].Refs = append(parts[i].Refs, r)
	}
	return parts
}

// ---- 汇编器的小包装 ----

type rt struct {
	a     *x64.Asm
	refs  []Ref
	order []namedStart
	next  x64.Label
	lbl   map[string]x64.Label
}

type namedStart struct {
	name string
	off  int
}

func newRt() *rt { return &rt{a: x64.New(), next: 1, lbl: map[string]x64.Label{}} }

// routine 先给这个例程占一个标号并落在起点上，再往下编，这样后面的例程能直接 call 它。
//
// en: routine first claims a label for this routine and marks it at the start, then assembles,
// so the routines that come later can call it directly.
func (b *rt) routine(name string, build func(*rt)) {
	l := b.next
	b.next++
	b.lbl[name] = l
	b.a.Mark(l)
	b.order = append(b.order, namedStart{name: name, off: b.a.PC()})
	build(b)
}

// call 调另一个例程，只能是已经编完的那几个（@write 排在最前）。
//
// en: call invokes another routine, and only one already finished (@write comes first).
func (b *rt) call(name string) { b.a.Call(b.lbl[name]) }

// label 要一个函数内的临时标号，和例程标号共用一个计数器，不会冲突。
//
// en: label asks for a temporary label inside one function; it shares the counter with routine
// labels, so they cannot collide.
func (b *rt) label() x64.Label {
	l := b.next
	b.next++
	return l
}

func (b *rt) finish() ([]byte, []Ref) {
	code, err := b.a.Finish()
	if err != nil {
		panic("运行期例程没落出来：" + err.Error())
	}
	return code, b.refs
}

// 下面三个都会把待回填点记进 b.refs，At 是整块 Code 里的偏移。

func (b *rt) loadRIP(reg int, name string) {
	at := b.a.MovLoadRIP(reg)
	b.refs = append(b.refs, Ref{At: at, Kind: RefData, Name: name})
}

func (b *rt) storeRIP(reg int, name string) {
	at := b.a.MovStoreRIP(reg)
	b.refs = append(b.refs, Ref{At: at, Kind: RefData, Name: name})
}

func (b *rt) leaRIP(reg int, name string) {
	at := b.a.LeaRIP(reg)
	b.refs = append(b.refs, Ref{At: at, Kind: RefData, Name: name})
}

// put／get 是本帧第 n 个局部量（8 字节一个位置）。例程里跨一次 call 要靠它把值留住——
// 我们只用调用方寄存器，call 一过，寄存器就当已经作废。
//
// en: put/get are the n-th local of this frame (8 bytes each). A routine keeps a value across a
// call through them: only caller-saved registers are used, so after a call the registers are
// assumed dead.
func (b *rt) put(n, reg int) { b.a.MovStore(x64.RBP, int32(-8*(n+1)), reg) }
func (b *rt) get(reg, n int) { b.a.MovLoad(reg, x64.RBP, int32(-8*(n+1))) }

// throwWith 发一次当场抛出：码与消息对象交给 @throw。
//
// en: throwWith throws on the spot: the code and the message object are handed to @throw.
func (b *rt) throwWith(code int64, msgName string) {
	b.a.MovImm(x64.RCX, code)
	b.leaRIP(x64.RDX, msgName)
	b.call("@throw")
}

// impCall 是"从导入表里取 kernel32 那个地址再 call"：调系统函数就是一个普通 call。
//
// en: impCall is "take kernel32's address from the import table and call it"; calling a
// system function is just an ordinary call.
func (b *rt) impCall(name string) { b.impCallFrom(kernel32, name) }

// impCallFrom 同一条路，但要指定问哪一个模块——winsock 那两个 DLL 进产物走的就是这一条。
//
// en: impCallFrom is the same path but names the module to ask - which is how winsock's two
// DLLs get into the product.
func (b *rt) impCallFrom(dll, name string) {
	at := b.a.MovLoadRIP(x64.RAX)
	b.refs = append(b.refs, Ref{At: at, Kind: RefIAT, Name: name, Off: importSlot(dll, name)})
	b.a.CallReg(x64.RAX)
}

func (b *rt) prologue() {
	b.a.Push(x64.RBP)
	b.a.Mov(x64.RBP, x64.RSP)
	b.a.AddImm(x64.RSP, -frameSize)
}

func (b *rt) epilogue() {
	b.a.AddImm(x64.RSP, frameSize)
	b.a.Pop(x64.RBP)
	b.a.Ret()
}

// ---- 例程 ----

// @write(rcx=字节地址, rdx=个数)：按 outErr 那个位置决定写到标准输出还是标准错误，句柄拿一次就缓存。
//
// lpNumberOfBytesWritten 与 lpOverlapped 都给 NULL：标准流这一路是同步句柄，
// 允许这么写——@write 只管把 UTF-8 字节交出去，不做代码页翻译。
//
// 两个句柄分开缓存：去向是每写一段之前摆好的（@out_err），所以这一条里不换方向，
// 也就不用每段都去问一次系统。
//
// en: @write (rcx = byte address, rdx = count): writes to stdout or stderr as the outErr
// position decides, and a handle is fetched once and then cached. lpNumberOfBytesWritten and
// lpOverlapped are both NULL: the standard streams here are synchronous handles, which permits
// that; @write only hands the UTF-8 bytes out, with no code-page translation. The
// two handles are cached separately because the destination is set before every write
// (@out_err): this routine never switches direction, so it need not ask the system per piece.
func write(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	a.MovStore(x64.RBP, -16, x64.RDX)
	errWay := b.label()
	joined := b.label()
	b.loadRIP(x64.RAX, "outErr")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, errWay)

	b.loadRIP(x64.RAX, "hstdout")
	have := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, have)
	a.MovImm(x64.RCX, -11) // STD_OUTPUT_HANDLE
	b.impCall("GetStdHandle")
	b.storeRIP(x64.RAX, "hstdout")
	a.Mark(have)
	b.loadRIP(x64.RAX, "hstdout")
	a.Jmp(joined)

	a.Mark(errWay)
	b.loadRIP(x64.RAX, "hstderr")
	haveErr := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, haveErr)
	a.MovImm(x64.RCX, -12) // STD_ERROR_HANDLE
	b.impCall("GetStdHandle")
	b.storeRIP(x64.RAX, "hstderr")
	a.Mark(haveErr)
	b.loadRIP(x64.RAX, "hstderr")

	a.Mark(joined)
	a.Mov(x64.RCX, x64.RAX)
	a.MovLoad(x64.RDX, x64.RBP, -8)
	a.MovLoad(x64.R8, x64.RBP, -16)
	a.Xor(x64.R9, x64.R9)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCall("WriteFile")
	b.epilogue()
}

// @out_err(rcx=0 或 1)：把"接下来这一段字往哪个流里写"摆进数据段那个位置。
//
// 打印那一路每写一段之前都摆一次（`console.print` 摆 0、`console.print_error` 摆 1），
// 所以实参里再套一个打印也不会把外面这一句的去向带走，中途抛出也不会留下没人复位的方向。
//
// en: @out_err (rcx = 0 or 1): puts "which stream the next piece goes to" into a data-section
// position. The printing path sets it before every piece (`console.print` sets 0,
// `console.print_error` sets 1), so a print nested inside an argument cannot carry the outer
// sentence's destination away, and a throw halfway leaves no direction unre-set.
func outErr(b *rt) {
	b.prologue()
	b.storeRIP(x64.RCX, "outErr")
	b.epilogue()
}

// @put_int(rcx=int64)：十进制写进 stdout。
//
// en: @put_int (rcx = int64): writes decimal to stdout.
func putInt(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	intChars(b)
	b.call("@write")
	b.epilogue()
}

// intChars 把 [rbp-8] 里那个 int64 写成十进制：除 10 取余、从帧里的缓冲区尾巴往前写，
// 负数在最前面补一个减号。写完把 rcx=起点、rdx=字节数 摆好——@put_int 拿去写 stdout，
// @int_str 拿去包成文本，两条路上的字因此一定相同。
//
// 最小值取反还是它自己，所以这里把值当无符号的位数往下除，不碰有符号除法那条边界。
//
// en: intChars writes the int64 at [rbp-8] as decimal: divide by ten collecting remainders,
// write backwards from the end of a frame buffer, and prefix a minus for negatives. It ends
// with rcx = start and rdx = byte count set up - @put_int writes them to stdout and @int_str
// wraps them into text, so the characters on the two paths are guaranteed identical. Negating
// the minimum value yields itself, so the value is divided as an unsigned digit count here,
// never touching the signed-division boundary.
func intChars(b *rt) {
	a := b.a
	a.Xor(x64.R9, x64.R9)
	a.MovLoad(x64.RAX, x64.RBP, -8)
	pos := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NS, pos)
	a.MovImm(x64.R9, 1)
	a.Neg(x64.RAX)
	a.Mark(pos)

	a.LeaMem(x64.RDI, x64.RBP, digits)
	a.MovImm(x64.R11, 10)
	loop := b.label()
	a.Mark(loop)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.R11)
	a.AddImm(x64.RDX, '0')
	a.AddImm(x64.RDI, -1)
	a.MovStore8(x64.RDI, 0, x64.RDX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, loop)

	neg := b.label()
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, neg)
	a.AddImm(x64.RDI, -1)
	a.MovImm(x64.RAX, '-')
	a.MovStore8(x64.RDI, 0, x64.RAX)
	a.Mark(neg)

	a.Mov(x64.RCX, x64.RDI)
	a.LeaMem(x64.RDX, x64.RBP, digits)
	a.Sub(x64.RDX, x64.RDI)
}

// @put_str(rcx=文本对象)：形状是「字节数 8 + 码点数 8 + UTF-8 字节」。
//
// 空句柄不在这里检查：@throw 自己也要用这一条印消息，两边互相 call 就成环了。
// 判空由降级在那一次取用之前发（10092 号本来就是取用那一刻的检查）。
//
// en: @put_str (rcx = text object): the shape is "byte count 8 + code point count 8 + UTF-8
// bytes". An empty handle is not checked here: @throw prints its message through this
// very routine, so the two would call each other in a circle. The null test is emitted by
// lowering before that use (code 10092 is exactly the check at the moment of
// dereferencing).
func putStr(b *rt) {
	a := b.a
	b.prologue()
	a.MovLoad(x64.RDX, x64.RCX, 0)
	a.AddImm(x64.RCX, 16)
	b.call("@write")
	b.epilogue()
}

// @put_char(rcx=码点)：现场编一段 UTF-8，按一到四字节的门槛分四档。
//
// en: @put_char (rcx = code point): encodes a UTF-8 run on the spot, splitting into four bands
// at the one- to four-byte thresholds.
func putChar(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	a.MovLoad(x64.RAX, x64.RBP, -8)
	l1, l2, l3 := b.label(), b.label(), b.label()
	a.CmpImm(x64.RAX, 0x80)
	a.Jcc(x64.B, l1)
	a.CmpImm(x64.RAX, 0x800)
	a.Jcc(x64.B, l2)
	a.CmpImm(x64.RAX, 0x10000)
	a.Jcc(x64.B, l3)
	b.utf8(4)
	emit := b.label()
	a.Jmp(emit)
	a.Mark(l1)
	b.utf8(1)
	a.Jmp(emit)
	a.Mark(l2)
	b.utf8(2)
	a.Jmp(emit)
	a.Mark(l3)
	b.utf8(3)
	a.Mark(emit)
	b.call("@write")
	b.epilogue()
}

// utf8 把 [rbp-8] 里那个码点编成 n 个字节写进缓冲区，并把 rcx／rdx 摆成 @write 的入参。
//
// 首字节的前缀是 n 个 1 后面跟一个 0，也就是 0xC0／0xE0／0xF0 那三个；一位的形式没有前缀。
//
// en: utf8 encodes the code point at [rbp-8] into n bytes in the buffer and sets rcx/rdx as
// @write's arguments. The lead byte's prefix is n ones followed by a zero, i.e. 0xC0/0xE0/0xF0;
// the one-byte form has no prefix.
func (b *rt) utf8(n int) {
	a := b.a
	lead := []int64{0x00, 0xC0, 0xE0, 0xF0}[n-1]
	for j := 0; j < n; j++ {
		shift := uint64(6 * (n - 1 - j))
		a.MovLoad(x64.RDX, x64.RBP, -8)
		if shift > 0 {
			a.ShrImm(x64.RDX, byte(shift))
		}
		if j > 0 {
			a.AndImm(x64.RDX, 0x3F)
			a.OrImm(x64.RDX, 0x80)
		} else if lead != 0 {
			a.OrImm(x64.RDX, int32(lead))
		}
		a.MovStore8(x64.RBP, int32(utf8Buf+j), x64.RDX)
	}
	a.LeaMem(x64.RCX, x64.RBP, utf8Buf)
	a.MovImm(x64.RDX, int64(n))
}

// @put_bool(rcx=0|1)：返回 true 或 false 那两个字面。
//
// en: @put_bool (rcx = 0|1): writes the two literals true or false.
func putBool(b *rt) {
	a := b.a
	b.prologue()
	isFalse := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, isFalse)
	b.leaRIP(x64.RCX, "sTrue")
	a.MovImm(x64.RDX, 4)
	done := b.label()
	a.Jmp(done)
	a.Mark(isFalse)
	b.leaRIP(x64.RCX, "sFalse")
	a.MovImm(x64.RDX, 5)
	a.Mark(done)
	b.call("@write")
	b.epilogue()
}

func putSpace(b *rt) {
	a := b.a
	b.prologue()
	b.leaRIP(x64.RCX, "sSpace")
	a.MovImm(x64.RDX, 1)
	b.call("@write")
	b.epilogue()
}

func putNL(b *rt) {
	a := b.a
	b.prologue()
	b.leaRIP(x64.RCX, "sNL")
	a.MovImm(x64.RDX, 1)
	b.call("@write")
	b.epilogue()
}

// @exit(rcx=退出码)：直接交给 ExitProcess。已经建好的值由调用方在语义层释放完才走到这儿。
// @heap_report()：设了环境变量 NEF_HEAP 才打印一行 "heap <字节数>\n"，否则不打印。
//
// 报的是 `heapCur - heapFrom`：向堆要过的总量。块还回空闲链表并不还给操作系统，所以这个数
// 在接上复用之后读的是"峰值要过多少"——测试要比的正是这个：同样一万圈，每圈造完就还的那条
// 应该停在最初那一块附近，把造出来的东西一路留着的那条会一路撑出好几块新的。
//
// 问"这个环境变量设过没有"用不着把值取回来：传一个空缓冲、长度 0，返回的就是"要几个宽字符"
// （设过是正数，没设是 0）。名字在编译期就摆成宽字符（见 envHeap 那一句）。
//
// en: @exit (rcx = exit code) hands it straight to ExitProcess: values already built have been
// released by the caller at the semantic level before arriving here. @heap_report():
// only when the NEF_HEAP environment variable is set does it print one line "heap <bytes>\n";
// otherwise it does nothing. What is reported is heapCur - heapFrom: the total ever asked from
// the heap. Returning a block to the free list does not return it to the operating system, so
// after reuse is wired up this figure reads "what the peak needed", which is exactly what the
// tests compare: over the same ten thousand rounds, the loop that returns each block
// immediately should stay near the first block, while the one that keeps everything grows
// several new blocks. Asking "is this variable set" needs no value: pass an empty buffer of
// length 0 and the return is "how many wide characters it would need" (positive if set, 0 if
// not). The name is laid out as wide characters at compile time (see envHeap).
func heapReport(b *rt) {
	a := b.a
	b.prologue()
	b.leaRIP(x64.RCX, "envHeap")
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.impCall("GetEnvironmentVariableW")
	off := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, off)
	b.leaRIP(x64.RCX, "sHeapHead")
	a.MovImm(x64.RDX, int64(len("heap ")))
	b.call("@write")
	b.loadRIP(x64.RCX, "heapCur")
	b.loadRIP(x64.RDX, "heapFrom")
	a.Sub(x64.RCX, x64.RDX)
	b.call("@put_int")
	// 第二个数：**此刻还挂在空闲链表上的字节数**（走完 73 个档头，把链上每一块的数据尺寸加起来）。
	// 加这个数的理由：第一个数读的是"向堆要过的累计总量"，块还进链并不减它——于是一档拆细
	// 这类改动必然让它涨（不再拿 40 那一块去顶 48 的请求），可它省的恰恰是"活着占用的那一节"
	// （＝累计 − 链上）。之前那次减少浪费的改动，正是落在这个分辨不出来的值上。
	b.call("@put_space")
	a.MovImm(x64.RAX, 0)
	a.MovStore(x64.RBP, -8, x64.RAX)  // 第几档
	a.MovStore(x64.RBP, -16, x64.RAX) // 链上累计
	// 循环状态一律放槽里：中间要 call @put_int 那一路，寄存器活不过第一站
	aX := b.label()
	aNext := b.label()
	iDone := b.label()
	aXDone := b.label()
	a.Mark(aX)
	a.MovLoad(x64.RCX, x64.RBP, -8)
	a.CmpImm(x64.RCX, freeClasses)
	a.Jcc(x64.AE, iDone)
	b.leaRIP(x64.RDI, "freeHeads")
	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RAX, x64.RDI, 0)
	a.MovStore(x64.RBP, -24, x64.RAX) // 这一档的链，从档头起走
	a.Mark(aNext)
	a.MovLoad(x64.RAX, x64.RBP, -24)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, aXDone)
	a.MovZX32(x64.RDX, x64.RAX, -4) // 这一块的数据尺寸
	a.MovLoad(x64.RCX, x64.RBP, -16)
	a.Add(x64.RCX, x64.RDX)
	a.MovStore(x64.RBP, -16, x64.RCX)
	a.MovLoad(x64.RAX, x64.RAX, 0) // 下一格（写在数据区头 8 字节）
	a.MovStore(x64.RBP, -24, x64.RAX)
	a.Jmp(aNext)
	a.Mark(aXDone)
	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RBP, -8, x64.RAX)
	a.Jmp(aX)
	a.Mark(iDone)
	a.MovLoad(x64.RCX, x64.RBP, -16)
	b.call("@put_int")
	b.call("@put_nl")
	a.Mark(off)
	b.epilogue()
}

// @exit(rcx=退出码)：返回操作系统。
//
// 先走一遍 @heap_report：没设 NEF_HEAP 的话那一句就是"跳过去"，正常的退出路径不多印任何东西。
//
// en: @exit (rcx = exit code): returns to the operating system. @heap_report runs first:
// without NEF_HEAP that step is just "skip", so a normal exit prints nothing extra.
func exitProc(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	b.call("@heap_report")
	// winsock 的收尾。三条退出路（走完、`exit`、没人接的抛出）都过这一个出口，
	// 所以这一次叫完就再没有"已经初始化却没人收"的状态留着。
	a.MovImm(x64.RCX, 0)
	b.impCallFrom(ws2, "WSACleanup")
	a.MovLoad(x64.RCX, x64.RBP, -8)
	b.impCall("ExitProcess")
	b.epilogue()
}

// @throw(rcx=码, rdx=消息对象)：把这两样存进静态那两个槽，然后交给 @throw_walk 往外走。
//
// 先存再走的理由：往下是一帧一帧地走，每一帧的垫子都 call 出去，寄存器活不过第一站。
//
// en: @throw (rcx = code, rdx = message object): stores both in the two static slots and hands
// them to @throw_walk to unwind. Store before walking because the walk goes frame by frame
// and each frame's padding calls out, so registers do not survive the first stop.
func throw(b *rt) {
	b.prologue()
	b.storeRIP(x64.RCX, "throwCode")
	b.storeRIP(x64.RDX, "throwMsg")
	b.call("@throw_walk")
	b.epilogue()
}

// @put_errline(rcx=码, rdx=消息对象)：印那句"出错了（码）：话"，末尾一个换行。
//
// 两条路共用它：@throw_walk 走到空链那一步（兜底那条也没人接的最后一次），以及 nef.main 顶上
// 那条处理器接住之后——没人接的抛出也要走顶层那份记录。
//
// en: @put_errline (rcx = code, rdx = message object): prints the "error (code): message"
// sentence with a trailing newline. Two paths share it: @throw_walk reaching the empty chain
// (the last attempt of the fallback with nobody to catch) and the top-level handler installed
// above nef.main catching it (an uncaught throw still has to run the top level's ledger).
func putErrline(b *rt) {
	a := b.a
	b.prologue()
	// 语言自己那一句一律走标准输出：手上那个位置可能是上一条 `console.print_error` 摆下的 1，
	// 不摆回去就变成"提示在错误流、报错也在错误流"，而那一句报错本来就在标准输出。
	a.Xor(x64.RAX, x64.RAX)
	b.storeRIP(x64.RAX, "outErr")
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.leaRIP(x64.RCX, "sErrHead")
	a.MovImm(x64.RDX, int64(len("出错了（")))
	b.call("@write")
	b.get(x64.RCX, 0)
	b.call("@put_int")
	b.leaRIP(x64.RCX, "sErrMid")
	a.MovImm(x64.RDX, int64(len("）：")))
	b.call("@write")
	b.get(x64.RCX, 1)
	b.call("@put_str")
	b.call("@put_nl")
	b.epilogue()
}

// @throw_walk()：沿处理器链从里往外走。
//
// 遇到"接住块"就把栈摆回那一层、码与话摆进 rax／rdx 交进去；遇到"帧内清理记录"就跳进那一帧，
// 由它把自己登记的账还完再叫回这一条（那是降级发的一段普通代码，不在这里）；
// 链走到空还没人接，按兜底处理：打印一行、退出码 1。
//
// 一条记录用完当场 @free：正常走完那一路由 @pop_handler 归还，被抛出接住由这里归还。
// 此前只摘链不归还，接住一次泄漏三十二字节。
//
// en: @throw_walk(): walks the handler chain from inside out. Reaching a catch block
// rewinds the stack to that layer, loads code and message into rax/rdx and enters it; reaching
// a frame-scope record jumps into that frame, whose own code (emitted by lowering, not here)
// finishes returning its registrations and calls back into this routine; reaching an empty
// chain with nobody having caught takes the fallback path: print one line, exit code 1. Each
// record is @free'd as soon as it is used: the normal path returns it via @pop_handler, a
// throw reaching it returns it here. Previously the chain was unlinked without returning,
// leaking thirty-two bytes per catch.
func throwWalk(b *rt) {
	a := b.a
	b.prologue()
	b.loadRIP(x64.RAX, "hTop")
	a.Test(x64.RAX, x64.RAX)
	none := b.label()
	a.Jcc(x64.E, none)
	a.MovStore(x64.RBP, -8, x64.RAX) // 这一条，末尾还它
	a.MovLoad(x64.R10, x64.RAX, hndNext)
	b.storeRIP(x64.R10, "hTop")
	// 落点、两个栈指针、种类先各自落进本帧的槽：下面 call @free 会动一堆寄存器，
	// 值留在手里就是留了一个会被冲掉的东西（这一条是接住块那一路实测到的）。
	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.MovLoad(x64.R11, x64.RAX, hndTarget)
	a.MovStore(x64.RBP, -16, x64.R11)
	a.MovLoad(x64.R11, x64.RAX, hndRbp)
	a.MovStore(x64.RBP, -24, x64.R11)
	a.MovLoad(x64.R11, x64.RAX, hndRsp)
	a.MovStore(x64.RBP, -32, x64.R11)
	a.MovLoad(x64.R11, x64.RAX, hndKind)
	a.MovStore(x64.RBP, -40, x64.R11)
	// 只有堆上那一条才 @free：帧内清理记录那条住在被摆掉的这一帧里，跟着帧一起散——
	// 拿帧上的地址当块堆还回去，就是把空闲链表弄断。
	a.MovLoad(x64.RCX, x64.RBP, -40)
	a.Test(x64.RCX, x64.RCX)
	noFree := b.label()
	a.Jcc(x64.NE, noFree)
	a.MovLoad(x64.RCX, x64.RBP, -8)
	b.call("@free")
	a.Mark(noFree)
	// 摆回那一层的栈之前先把四样取进寄存器：换掉 rbp 之后再读 [rbp-16] 读的是别人的帧。
	a.MovLoad(x64.R8, x64.RBP, -16)  // 落点
	a.MovLoad(x64.R9, x64.RBP, -24)  // 那一层的 rbp
	a.MovLoad(x64.R10, x64.RBP, -32) // 那一层的 rsp
	a.MovLoad(x64.R11, x64.RBP, -40) // 种类
	a.Mov(x64.RSP, x64.R10)
	a.Mov(x64.RBP, x64.R9)
	leave := b.label()
	a.Test(x64.R11, x64.R11)
	a.Jcc(x64.NE, leave)
	// 接住块：码与话放进 rax／rdx，接住块开头把它们存进两个局部量的槽。
	b.loadRIP(x64.RAX, "throwCode")
	b.loadRIP(x64.RDX, "throwMsg")
	a.JmpReg(x64.R8)
	// 帧内清理记录：这一帧不是接住块，还完它自己那一笔会 call 回 @throw_walk，这里什么都不用留。
	a.Mark(leave)
	a.JmpReg(x64.R8)

	a.Mark(none)
	b.loadRIP(x64.RCX, "throwCode")
	b.loadRIP(x64.RDX, "throwMsg")
	b.call("@put_errline")
	// 到这儿就是**连兜底那条都没有**：nef.main 顶上挂的那一条命中时已经把它自己摘了，
	// 所以还顶层那份记录的途中再抛一次，走的是这一步——印完就走，不会再绕回那一段（不成环）。
	a.MovImm(x64.RCX, 1)
	b.call("@exit")
	b.epilogue()
}

// 处理器链上那一条记录：五个格、四十字节。@push_handler 摆的那一条在**堆**上（摘下来要释放），
// @push_abandon 摆的那一条住在**挂它的那一帧**里（帧散了它跟着散，谁也不许 @free 它）。
// 同一条链、同一套字段，只有种类那个位置不同——摘它的 @pop_handler 看这个位置决定要不要归还。
//
// en: A record on the handler chain: five slots, forty bytes. The one @push_handler places is
// on the HEAP (unlinking must release it); the one @push_abandon places lives INSIDE THE FRAME
// that attached it (the frame's end dissolves it, and nobody may @free it). Same chain, same
// fields, only the kind position differs - and @pop_handler reads that position to decide
// whether to return it.
const (
	hndTarget = 0  // 落点：接住块的开头，或这一段帧内清理记录的开头
	hndRbp    = 8  // 挂上来那一帧的 rbp
	hndRsp    = 16 // 同一帧的 rsp
	hndNext   = 24 // 链上上一条
	hndKind   = 32 // 0 = 接住块（堆上那条），1 = 帧内清理记录（帧上那条）
	hndSize   = 40
)

const (
	hndCatch = 0 // 接住块：抛出走到这儿就停，交货
	hndLeave = 1 // 帧内清理记录：这一帧是被抛过去的，还完自己那一笔要继续往外走
)

// @push_handler(rcx=接住块的地址)：把这一层处理器挂到链上。
//
// 帧的两个指针在这里当场抄下来——抛出时要靠它们把栈摆回接住那一层的样子。
// 用完要归还：正常走完由 @pop_handler 归还，被抛出接住由 @throw_walk 归还。
//
// en: @push_handler (rcx = catch block address): hang this handler layer on the chain.
// The two frame pointers are copied here on the spot, because a throw needs them to rewind the
// stack to the catching layer's shape. It is returned when done: @pop_handler on the
// normal path, @throw_walk when a throw is caught.
func pushHandler(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	a.MovImm(x64.RCX, hndSize)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	a.Mov(x64.RDX, x64.RAX) // 这一条在堆上，摘下来时要归还它
	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.MovStore(x64.RDX, hndTarget, x64.RAX)
	a.MovLoad(x64.RAX, x64.RBP, 0) // 调用方的 rbp：push 进来的那一个
	a.MovStore(x64.RDX, hndRbp, x64.RAX)
	a.LeaMem(x64.RAX, x64.RBP, 16) // 调用方的 rsp：call 压进去的返回地址就在这个位置
	a.MovStore(x64.RDX, hndRsp, x64.RAX)
	b.loadRIP(x64.RAX, "hTop")
	a.MovStore(x64.RDX, hndNext, x64.RAX)
	a.MovImm(x64.RAX, hndCatch)
	a.MovStore(x64.RDX, hndKind, x64.RAX)
	b.storeRIP(x64.RDX, "hTop")
	b.epilogue()
}

// @push_abandon(rcx=帧内清理记录的地址, rdx=本帧给这颗记录留的连续五格)：挂上"这一帧被抛过去了"那条标记。
//
// 抛出从被叫的那一帧里走出来时，中间那几帧没人释放——它们在降级那份记录上登记过，却已经没有
// 机会走自己的退出清理代码。挂上这一条，@throw_walk 走到时就跳进那一帧把该释放的释放完再往外走。
//
// 与 @push_handler 两处不同：种类那个位置写 1，而且记录住在**调用方自己的帧**上——这一条从开场挂到
// 帧尾，帧散了它跟着散，所以既不要 @alloc 也不要 @free（登记过东西的函数每次调用少一次堆上的
// 一取一还），也不会因为要不到内存而抛 10091 号。那五格由降级那头 `ir.Func.Reserve(5)` 留出来。
//
// en: @push_abandon (rcx = frame-scope record address, rdx = the five consecutive slots this
// frame reserved for that record): attach the "this frame is being thrown past" marker. When a
// throw leaves the frame that was called, the frames in between release nothing themselves -
// they are registered in lowering's ledger but never get to run their own cleanup. With this
// marker attached, @throw_walk jumps into that frame, releases what it registered, and
// continues outward. Two differences from @push_handler: the kind position is written 1, and
// the record lives on THE CALLER'S OWN FRAME - attached from the prologue until the frame ends
// and dissolved with it, so neither @alloc nor @free is needed (a function with registrations
// saves one heap get-and-return per call) and code 10091 cannot be thrown for want of memory.
// Those five slots are reserved by `ir.Func.Reserve(5)` on the lowering side.
func pushAbandon(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)  // 帧内清理记录的开头
	a.MovStore(x64.RBP, -16, x64.RDX) // 那一条记录（在调用方的帧里）
	a.MovLoad(x64.RDX, x64.RBP, -16)
	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.MovStore(x64.RDX, hndTarget, x64.RAX)
	a.MovLoad(x64.RAX, x64.RBP, 0) // 调用方的 rbp
	a.MovStore(x64.RDX, hndRbp, x64.RAX)
	a.LeaMem(x64.RAX, x64.RBP, 16) // 调用方的 rsp
	a.MovStore(x64.RDX, hndRsp, x64.RAX)
	b.loadRIP(x64.RAX, "hTop")
	a.MovStore(x64.RDX, hndNext, x64.RAX)
	a.MovImm(x64.RAX, hndLeave)
	a.MovStore(x64.RDX, hndKind, x64.RAX)
	b.storeRIP(x64.RDX, "hTop")
	b.epilogue()
}

// @pop_handler()：把链头摘掉并归还给堆——try 正常走完那一条路，以及一帧从 Ret 走出去时
// 摘自己那条帧内清理记录。
//
// 此前只摘链不归还，接住一次泄漏四十字节；现在两头都收（抛出那一条路由 @throw_walk 归还，
// 走完这一条由这里归还）。**只有堆上那一条才 @free**：帧内清理记录那条住在被摘它的这一帧里，
// 跟着帧一起散，拿它当块堆归还就是把空闲链表弄断。
//
// en: @pop_handler(): unlink the chain head and return it to the heap - the path where try
// completed normally, and a frame leaving through Ret removing its own frame-scope record.
// Previously the chain was unlinked without returning, leaking forty bytes per catch;
// now both ends collect (the throw path returns it in @throw_walk, this path here). ONLY THE
// HEAP RECORD IS @free'd: the frame-scope record lives inside the very frame being unlinked and
// dissolves with it, so returning it as a heap block would break the free list.
func popHandler(b *rt) {
	a := b.a
	b.prologue()
	b.loadRIP(x64.RAX, "hTop")
	done := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, done) // 链是空的：没有 try 挂过处理器，直接返回
	a.MovStore(x64.RBP, -8, x64.RAX)
	a.MovLoad(x64.RAX, x64.RAX, hndNext)
	b.storeRIP(x64.RAX, "hTop")
	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.MovLoad(x64.R10, x64.RAX, hndKind)
	a.Test(x64.R10, x64.R10)
	freeIt := b.label()
	a.Jcc(x64.E, freeIt) // 帧内清理记录那条：在帧上，不归还
	a.Jmp(done)
	a.Mark(freeIt)
	a.MovLoad(x64.RCX, x64.RBP, -8)
	b.call("@free")
	a.Mark(done)
	b.epilogue()
}

// @die(rcx=退出码, rdx=消息对象)：`exit (码, 话)` 走这条。
// 话先原样打印到屏幕上，返回操作系统的就是括号里那个数——和 @throw 不同，
// @throw 是"没人接的异常"，退出码固定 1。
//
// en: @die (rcx = exit code, rdx = message object): this is `exit (code, text)`. The
// text is printed to the screen as written and the number in parentheses is what returns to the
// operating system - unlike @throw, which is an uncaught exception and always exits with code 1.
func die(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	a.MovStore(x64.RBP, -16, x64.RDX)
	a.MovLoad(x64.RCX, x64.RBP, -16)
	b.call("@put_str")
	b.call("@put_nl")
	a.MovLoad(x64.RCX, x64.RBP, -8)
	b.call("@exit")
	b.epilogue()
}

// @die_stack()：调用套得太深，栈用完了（10093 号）。印那句报错，然后按 1 退。
//
// 这一条不走 @throw_walk，也不跑顶层那本账：归还一块内存本身就要用栈，而栈正是用完的那个东西。
// 每一帧开场那一次比较就是它唯一的入口（`internal/codegen/link.go` 的 StackGuard），
// 下限留了 64 KiB 余量，够这一路把话印完。
//
// en: @die_stack(): the calls went too deep and the stack is exhausted (code 10093). Print
// the error sentence and exit with 1. This path does not use @throw_walk and does not run the top
// level's ledger: releasing a memory block itself needs stack, and stack is precisely what ran
// out. The comparison at the start of every frame is its only entry point (StackGuard in
// internal/codegen/link.go), and the floor keeps 64 KiB of headroom - enough to print the
// sentence.
func dieStack(b *rt) {
	a := b.a
	b.prologue()
	a.MovImm(x64.RCX, codeStack)
	b.leaRIP(x64.RDX, "sStack")
	b.call("@put_errline")
	a.MovImm(x64.RCX, 1)
	b.call("@exit")
	b.epilogue()
}

// @str_cat(rcx=文本甲, rdx=文本乙)：返回 rax=新的一块「字节数 8 + 码点数 8 + UTF-8」。
//
// 拼接的结果是"用完就没了的对象"，走同一套分配。两段的长度先各读一遍：
// 码点数相加就是结果的码点数（UTF-8 拼起来不会改变码点数），不必再数一遍。
// 空句柄不在这里检查——那是"对着空句柄取成员或调用方法"，由降级在那一次取用之前发检查。
//
// en: @str_cat (rcx = text a, rdx = text b): returns rax = a new block of "byte count 8 + code
// point count 8 + UTF-8". A concatenation is a used-and-done object, allocated the same way.
// Both lengths are read first: adding the code point counts gives the result's count
// (joining UTF-8 cannot change code point counts), so no recount. An empty handle is not
// checked here - that would be dereferencing to take a member or call a method, which lowering
// checks before the use.
func strCat(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)

	// 结果的字节数与码点数
	b.get(x64.RAX, 0)
	a.MovLoad(x64.R8, x64.RAX, StrBytes)
	b.get(x64.RAX, 1)
	a.MovLoad(x64.R9, x64.RAX, StrBytes)
	a.Add(x64.R8, x64.R9) // 总字节数
	b.put(2, x64.R8)
	b.get(x64.RAX, 0)
	a.MovLoad(x64.R8, x64.RAX, StrCount)
	b.get(x64.RAX, 1)
	a.MovLoad(x64.R9, x64.RAX, StrCount)
	a.Add(x64.R8, x64.R9) // 总码点数
	b.put(3, x64.R8)

	b.get(x64.RCX, 2)
	a.AddImm(x64.RCX, 16)
	a.MovImm(x64.RDX, 6) // 文本 own 自己的类型字
	b.call("@alloc")
	b.put(4, x64.RAX) // 新对象

	b.get(x64.RSI, 4)
	b.get(x64.RAX, 2)
	a.MovStore(x64.RSI, StrBytes, x64.RAX)
	b.get(x64.RAX, 3)
	a.MovStore(x64.RSI, StrCount, x64.RAX)
	a.AddImm(x64.RSI, 16) // 抄写的落点

	// 抄两段：逐字节。 lengths 都在对象自己那格里，抄完一段再拿下一段。
	for _, which := range []int{0, 1} {
		b.get(x64.RDI, which)
		a.MovLoad(x64.RCX, x64.RDI, StrBytes)
		a.AddImm(x64.RDI, 16)
		one := b.label()
		skip := b.label()
		a.Mark(one)
		a.Test(x64.RCX, x64.RCX)
		a.Jcc(x64.E, skip)
		a.MovZX8(x64.RAX, x64.RDI, 0)
		a.MovStore8(x64.RSI, 0, x64.RAX)
		a.AddImm(x64.RDI, 1)
		a.AddImm(x64.RSI, 1)
		a.AddImm(x64.RCX, -1)
		a.Jmp(one)
		a.Mark(skip)
	}

	b.get(x64.RAX, 4) // 交回新对象的首格：字节数那个位置的位置
	b.epilogue()
}

// @list_new(rcx=元素个数, rdx=类型字)：返回 rax=新的 list 句柄。
//
// 形状是「有几样 8 ＋ 能装几样 8 ＋ 数据块地址 8 ＋ 改过几次 8」，位置在数据块上，
// 每格一律 8 字节（窄元素在容器里也占满一个位置，这样容器只有一种搬法）。
// 类型字由调用方给：编号的规则只有一处（降级那边），运行期不自己另排一套。
// 数据块那一笔的头里类型字填 0——它不是任何对象，只是一段裸缓冲。
//
// en: @list_new (rcx = element count, rdx = type word): returns rax = a new list handle. The
// shape is "how many 8 + capacity 8 + data block address 8 + mutation count 8" laid out in the
// data block, every slot 8 bytes (a narrow element still fills a slot, so a container has one
// and only one way to move). The type word comes from the caller: the numbering rule exists in
// one place only (the lowering side) and the runtime invents no second one. The data block's
// own header carries type word 0 - it is not an object, just a raw buffer.
func listNew(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX) // 个数
	b.put(1, x64.RDX) // 类型字

	// 容量先取"个数与 minCap 的大者"：空的 list 也要有个能写进去的落点。
	b.get(x64.RAX, 0)
	a.CmpImm(x64.RAX, minCap)
	big := b.label()
	a.Jcc(x64.AE, big)
	a.MovImm(x64.RAX, minCap)
	a.Mark(big)
	b.put(2, x64.RAX) // 容量

	a.MovImm(x64.R8, 8)
	a.Imul(x64.RAX, x64.R8)
	a.Mov(x64.RCX, x64.RAX)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.put(3, x64.RAX) // 数据块

	a.MovImm(x64.RCX, cObjSz)
	b.get(x64.RDX, 1)
	b.call("@alloc")
	b.put(4, x64.RAX) // 容器对象

	b.get(x64.RDI, 4)
	b.get(x64.RAX, 0)
	a.MovStore(x64.RDI, cLen, x64.RAX)
	b.get(x64.RAX, 2)
	a.MovStore(x64.RDI, cCap, x64.RAX)
	b.get(x64.RAX, 3)
	a.MovStore(x64.RDI, cData, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RDI, cVer, x64.RAX)
	a.Mov(x64.RAX, x64.RDI)
	b.epilogue()
}

// @list_add(rcx=句柄, rdx=那一样)：往尾部添一个位置，容量不够就先翻倍搬一次。
//
// 翻倍搬完把老数据块归还给堆（@free），添成功要把"改过几次"推一个位置，让正在遍历的那圈
// 下一轮就抛 10033 号。
//
// en: @list_add (rcx = handle, rdx = the element): append one slot at the tail, doubling and
// moving first when capacity runs out. After the move the old data block is returned to the
// heap (@free), and a successful append advances the mutation counter so an iteration in
// progress meets code 10033 on its next round.
func listAdd(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)

	have := b.label()
	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.MovLoad(x64.R8, x64.RDI, cCap)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.B, have)

	// 满了：新容量翻倍、要一块新的、把老的整段搬过去、换掉句柄里的两处。
	// 老的那一段换完就没有持有者了（正在遍历的那一圈靠"改过几次"下一轮抛 10033 号），
	// 所以交完尾就归还给堆——此前这里是明知该还而没有还的浪费。
	b.get(x64.RAX, 0)
	a.MovLoad(x64.RAX, x64.RAX, cData)
	b.put(4, x64.RAX) // 老数据块，最后要释放它
	b.get(x64.RAX, 0)
	a.MovLoad(x64.RAX, x64.RAX, cCap)
	a.Add(x64.RAX, x64.RAX)
	b.put(2, x64.RAX)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RAX, x64.R8)
	a.Mov(x64.RCX, x64.RAX)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.put(3, x64.RAX)

	b.get(x64.R11, 0)
	a.MovLoad(x64.R9, x64.R11, cLen) // 还有几格要搬
	a.MovLoad(x64.RSI, x64.R11, cData)
	b.get(x64.RDI, 3) // 新块
	step := b.label()
	done := b.label()
	a.Mark(step)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, done)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.MovStore(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.RDI, 8)
	a.AddImm(x64.R9, -1)
	a.Jmp(step)
	a.Mark(done)

	b.get(x64.RDI, 0)
	b.get(x64.RAX, 2)
	a.MovStore(x64.RDI, cCap, x64.RAX)
	b.get(x64.RAX, 3)
	a.MovStore(x64.RDI, cData, x64.RAX)
	b.get(x64.RCX, 4)
	b.call("@free") // 老数据块归还给堆：下一次添到这一档就能直接取回来

	a.Mark(have)
	// 尾格 = 数据块 + 已有个数*8，写进去，个数与次数各推一个位置。
	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RAX, x64.R8)
	a.MovLoad(x64.RSI, x64.RDI, cData)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RDX, 1)
	a.MovStore(x64.RSI, 0, x64.RDX)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, cLen, x64.RAX)
	a.MovLoad(x64.RAX, x64.RDI, cVer)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, cVer, x64.RAX)
	b.epilogue()
}

// @list_at(rcx=句柄, rdx=下标)：返回 rax=那个位置的地址，越界当场抛 10031 号。
//
// 只交地址不交值，是因为读、写、交出三件事都要先过同一道边界检查：
// 降级拿这个地址发 Load／Store，检查就不必抄三遍。负下标与超出长度同一个号码。
//
// en: @list_at (rcx = handle, rdx = index): returns rax = the address of that position and
// throws code 10031 on the spot for out of range. Only an address is handed out, never
// a value, because reading, writing and taking out all three must pass the same bounds check:
// lowering emits Load/Store from that address, so the check is not written three times. A
// negative index and one past the length take the same code.
func listAt(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)

	b.get(x64.RAX, 1)
	bad := b.label()
	ok := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R8, x64.RDI, cLen)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.B, ok)
	a.Mark(bad)
	b.throwWith(codeIndex, "sIndex")
	a.Mark(ok)
	a.MovImm(x64.R9, 8)
	a.Imul(x64.RAX, x64.R9)
	a.MovLoad(x64.RSI, x64.RDI, cData)
	a.Add(x64.RSI, x64.RAX)
	a.Mov(x64.RAX, x64.RSI)
	b.epilogue()
}

// @list_touch(rcx=句柄)：把"改过几次"推一个位置，返回 nothing。
//
// 往已经有东西的一个位置里写（`xs[i] = v`）现在要把换下来的那一块析构掉，而正在遍历
// 这个 list 的那一圈手上拿着的是**只读借用**——它借的就是那个位置里旧的那一块。先前 list 只有
// `@list_add` 推这个位置（增删才算改过），覆盖不算；析构旧值之后，覆盖就成了会让借用者
// 读到已析构内存的那种改动，所以照同一条规则推一个位置：正在遍历的那一圈下一轮抛 10033 号，
// 不给它拿着已析构的旧值继续走的机会。
//
// en: @list_touch (rcx = handle): advance the mutation counter and return nothing. Writing into
// a position that already holds something now dismantles the value being replaced, and an
// iteration over this list holds a READ-ONLY BORROW - borrowed precisely from
// the old block in that position. Previously only @list_add advanced this counter (only add and
// remove counted as mutation); with the old value being dismantled, an overwrite became the
// kind of mutation that lets a live borrow read dismantled memory, so the counter advances here
// too: the loop in progress meets code 10033 on its next round instead of continuing after its
// borrow was dismantled.
func listTouch(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cVer)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, cVer, x64.RAX)
	b.epilogue()
}

// @str_eq(rcx=文本甲, rdx=文本乙)：返回 rax=1 或 0，按**内容**比（文本判等看内容）。
//
// 相等的规矩：同一个句柄（两个都是空句柄也算同一个）相等；一边空一边不空就不等。
// 之后再按内容比——先比长度那个位置，不等就直接返回，这是最常命中的分支。
//
// en: @str_eq (rcx = text a, rdx = text b): returns rax = 1 or 0, compared by CONTENT -
// string equality looks at content.
func strEq(b *rt) {
	a := b.a
	b.prologue()
	yes := b.label()
	no := b.label()
	differ := b.label()
	top := b.label()

	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.NE, differ)
	// 同一个句柄，两边都是 null 也算同一个
	a.MovImm(x64.RAX, 1)
	b.epilogue()

	a.Mark(differ)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, no)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, no)

	a.MovLoad(x64.R9, x64.RCX, StrBytes)
	a.MovLoad(x64.R8, x64.RDX, StrBytes)
	a.Cmp(x64.R9, x64.R8)
	a.Jcc(x64.NE, no)

	a.LeaMem(x64.RSI, x64.RCX, 16)
	a.LeaMem(x64.RDI, x64.RDX, 16)
	a.Mark(top)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, yes)
	a.MovZX8(x64.RAX, x64.RSI, 0)
	a.MovZX8(x64.RDX, x64.RDI, 0)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.NE, no)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.R9, -1)
	a.Jmp(top)

	a.Mark(yes)
	a.MovImm(x64.RAX, 1)
	b.epilogue()
	a.Mark(no)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
}

// @list_remove(rcx=句柄, rdx=那一样, r8=怎么算相等)：删掉第一个相等的位置，后面整体往前挪一个位置。
// 找不到就原样回来，不抛——语言规则只说"删一个元素"，没规定不存在时算什么。
//
// r8 是降级那边按元素类型定的：0 比那 64 位本身（整数家族、枚举、句柄按身份），
// 1 走 @str_eq 按内容比文本。float 不能作为键，不会走到这一条。
//
// 返回的是被删掉那一句柄（没删成就是 0）：那个位置马上要被后面那个位置盖掉，盖掉之后语言里
// 再没有名字指着它，而运行期手上没有形状表，无法析构，所以交给降级按元素的形状析构。
// 挪完尾巴那个槽位还留着最后一个元素的第二个句柄——长度一退它就不在名单里，但它与还在名单里
// 的那一处指着同一块，析构它就是析构活着的那一个，所以只抹成 0（一个值同一时刻只有一个持有者）。
//
// en: @list_remove (rcx = handle, rdx = the element, r8 = how equality is decided): delete the
// first position that compares equal and shift everything after it one slot left. Not finding
// it returns unchanged and throws nothing - the language rule only says "remove one element"
// and never defines what an absent one means. r8 is chosen by lowering from the element type: 0
// compares the 64 bits themselves (integer family, enums, handles by identity), 1 goes through
// @str_eq comparing text by content. A float cannot be a key, so it never reaches this. It
// returns the handle of the entry that was removed (0 if nothing was removed): that position is
// about to be overwritten by the one behind it, after which no name in the language points at
// it, and the runtime has no shape table to dismantle it, so lowering is handed it and
// dismantles by element shape. After the shift, the tail still holds a second handle to the
// last element: decrementing the length puts it off the books, but it points at the same block
// as an entry still on the books, and dismantling it would dismantle the live one - so it is
// only zeroed (a value has exactly one holder at any moment).
func listRemove(b *rt) {
	const kRemoved = 5 // 被删那一句柄，留着到末尾交回
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.put(2, x64.R8)

	b.get(x64.RDI, 0)
	a.MovLoad(x64.R9, x64.RDI, cLen) // 还要看几格
	a.MovLoad(x64.RSI, x64.RDI, cData)
	find := b.label()
	found := b.label()
	notFound := b.label()
	numWay := b.label()
	step := b.label()
	a.Mark(find)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, notFound)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.get(x64.R8, 2)
	a.Test(x64.R8, x64.R8)
	// 模式的规则与 @key_eq 同一条：0 比那 64 位，1 比文本内容（降级那头的 keyMode／list.remove
	// 都是这么发的）。此前这一条把方向判反了——比文本的 list 走了数值那支（只有句柄相同才命中），
	// 比数的 list 走了 @str_eq（拿着整数当指针去读，有时候崩溃、有时候读到别的字节）。
	a.Jcc(x64.E, numWay)
	// 按内容比文本：游标与剩余数要跨过一次 call，先放进局部槽
	b.put(3, x64.RSI)
	b.put(4, x64.R9)
	a.Mov(x64.RCX, x64.RAX)
	b.get(x64.RDX, 1)
	b.call("@str_eq")
	b.get(x64.RSI, 3)
	b.get(x64.R9, 4)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, found)
	a.Jmp(step)

	a.Mark(numWay)
	b.get(x64.RDX, 1)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.E, found)

	a.Mark(step)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.R9, -1)
	a.Jmp(find)

	a.Mark(found)
	// 命中那个位置里的句柄先存一份，末尾返回去给降级析构
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.put(kRemoved, x64.RAX)
	// 命中在 RSI，它后面还有 R9-1 格，每格往前一个位置。
	a.AddImm(x64.R9, -1)
	a.Mov(x64.RDI, x64.RSI)
	a.AddImm(x64.RSI, 8)
	shrink := b.label()
	nada := b.label()
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, nada)
	a.Mark(shrink)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	a.MovStore(x64.RDI, 0, x64.RAX)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.RDI, 8)
	a.AddImm(x64.R9, -1)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.NE, shrink)
	a.Mark(nada)

	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.AddImm(x64.RAX, -1)
	// 退长度之前先把尾巴那一处抹成 0：那个位置与新的最后一个位置指着同一块
	b.get(x64.RSI, 0)
	a.MovLoad(x64.RSI, x64.RSI, cData)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore(x64.RSI, 0, x64.RCX)

	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.AddImm(x64.RAX, -1)
	a.MovStore(x64.RDI, cLen, x64.RAX)
	a.MovLoad(x64.RAX, x64.RDI, cVer)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, cVer, x64.RAX)
	b.get(x64.RAX, kRemoved)
	b.epilogue()

	a.Mark(notFound)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
}

// @alloc(rcx=字节数, rdx=类型字)：返回 rax=那一块的地址（头在地址前面）。
//
// 形状：每笔分配前面挂 8 字节的头，低四个字节是类型标识，高四个字节是这块的尺寸。
// 尺寸先往上凑到 8 的倍数，好让下一个对象也是 8 字节对齐的；头里记的是凑过之后的数。
// 再垫一道"数据至少 8 字节"：归还自由要把链上下一个位置的指针写进数据区头 8 字节（见 freeHeads），
// 数据区比这还小的块就无处可挂。
//
// 两条来路：先看空闲链表里那一档有没有现成的块，有就摘下来用；没有才从 [heapCur, heapEnd)
// 往外切，切不出这一笔就再要一块新的（一次至少 1 MiB，按页向上取整；要不到就是 10091 号）。
// 换块时旧块剩下的尾巴仍然浪费掉，上界是一整块——那是"没归还过的块"的浪费，比此前只涨不还得多。
//
// 返回的内存一律是干净的：新页本来就是全 0，从链上取回来的那一块在交出之前先清零数据区。
// `new C()` 的默认值靠的就是这一条：只涨不还时零区域是免费得到的，接上复用后必须自己清零。
//
// en: @alloc (rcx = byte count, rdx = type word): returns rax = the address of the block (its
// header sits before the address). Every allocation carries an 8-byte
// header, its low four bytes the type tag and its high four the size. The size is first rounded
// up to a multiple of 8 so the next object is also 8-byte aligned, and the header records the
// rounded figure. A second floor is applied - the data area is never smaller than 8 bytes -
// because returning a block to the chain writes the next pointer into the first 8 bytes of the
// data area (see freeHeads), and a smaller area has nowhere to hang it. Two sources are tried:
// a ready block in that band of the free list is unlinked and used, otherwise memory is carved
// outward from [heapCur, heapEnd), and if this request does not fit, a new region is asked for
// (at least 1 MiB per request, rounded up to a page; refusing is code 10091). When a
// region is replaced, whatever remains of the old one is wasted - bounded by one whole region -
// which is waste among blocks never returned, far less than the earlier grow-only-never-return
// behaviour. Returned memory is always clean: a new page is already all zero, and a block taken
// off the chain has its data area zeroed before it is handed over. `new C()`'s default values
// rely on exactly this: it came free when memory only grew, but with reuse wired up the zeroing
// has to happen explicitly.
func alloc(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	a.MovStore(x64.RBP, -16, x64.RDX)

	a.MovLoad(x64.RAX, x64.RBP, -8)
	a.AddImm(x64.RAX, 7)
	a.AndImm(x64.RAX, -8)
	a.CmpImm(x64.RAX, freeMinData)
	wide := b.label()
	a.Jcc(x64.AE, wide)
	a.MovImm(x64.RAX, freeMinData) // 不足 8 的那一笔垫到 8，好让自由链有地方挂下一格
	a.Mark(wide)
	a.MovStore(x64.RBP, -8, x64.RAX)
	a.AddImm(x64.RAX, 8) // 数据之外还要那 8 字节的头
	a.MovStore(x64.RBP, -24, x64.RAX)

	// 先问链上那一档。最大那一档装不下的（比 512 KiB 还大）直接走 bump 那条路。
	a.MovLoad(x64.RAX, x64.RBP, -24)
	a.CmpImm(x64.RAX, freeMaxTotal)
	room := b.label()
	a.Jcc(x64.A, room) // 无符号大于：这一笔太大，不去碰链
	b.heapClass()      // r9 = 第几档，r8 = 那一档的上界
	a.ShlImm(x64.R9, 3)
	b.leaRIP(x64.RDI, "freeHeads")
	a.Add(x64.RDI, x64.R9)
	a.MovLoad(x64.RDX, x64.RDI, 0)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, room) // 那一档还空着：去切新的
	// 摘下来这个位置，链头换成它的下一个位置
	a.MovLoad(x64.RAX, x64.RDX, 0)
	a.MovStore(x64.RDI, 0, x64.RAX)
	// 清零：这一块自己的数据尺寸还在头里（比这一笔要的大的话，多出来的也一并清零）
	a.MovZX32(x64.RCX, x64.RDX, -4)
	// 档是按 2 的幂上界分的，同一档里存在比这一笔小的块（例：数据 128 与 192 同在一档）：
	// 那样一块挂得回链上、却装不下这一笔。小的那块连整条链原样挂回去留给小的那一笔，这一笔改走 bump。
	fit := b.label()
	a.MovLoad(x64.RSI, x64.RBP, -8)
	a.Cmp(x64.RCX, x64.RSI)
	a.Jcc(x64.AE, fit)
	a.MovStore(x64.RDI, 0, x64.RDX)
	a.Jmp(room)
	a.Mark(fit)
	// 大块拆两半：链上摘下来这一块比这一笔大的话，过去是整块交出、
	// 头里的尺寸不动——于是同一档里一大一小交替要块时，小的一笔把大的那一块占用掉，下一笔要
	// 大的那一块时链上只剩小的，挂回去走 bump，**每多一圈多要一整块**（实测 32 字节／圈，
	// 块一块都没漏还，水线却按圈涨）。现在余量装得下"一颗头 8 ＋ 数据 8"就切一刀，剩下那一节
	// 挂进它自己那一档；切不动就照旧整块交出（浪费 ≤ 16 字节）。
	// 与上面那一判是同一条规则：**小的不交、大的切开**。
	split := b.label()
	skip := b.label()
	a.Mov(x64.RAX, x64.RCX)
	a.Sub(x64.RAX, x64.RSI) // 余量＝剩下那一节的总尺寸（含它自己的头）
	a.CmpImm(x64.RAX, freeSplitMin)
	a.Jcc(x64.B, skip)
	a.MovStore(x64.RBP, -32, x64.RAX)
	a.MovStore(x64.RBP, -40, x64.RSI)
	a.Mov(x64.RCX, x64.RSI) // 清零只抹用掉那一块：剩下那一节的头正躺在数据区里，抹不得
	a.Jmp(split)
	a.Mark(skip)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, -32, x64.RAX) // 0＝这一刀不切，尾巴那一段跟着跳过
	a.Mark(split)
	a.Add(x64.RCX, x64.RDX)
	a.Mov(x64.RSI, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	zero := b.label()
	zdone := b.label()
	a.Mark(zero)
	a.Cmp(x64.RSI, x64.RCX)
	a.Jcc(x64.AE, zdone)
	a.MovStore(x64.RSI, 0, x64.R8)
	a.AddImm(x64.RSI, 8)
	a.Jmp(zero)
	a.Mark(zdone)
	// 切过一刀就把两半各自那头写齐：用掉那一块的尺寸改成这一笔要的，剩下那一节按它自己的尺寸
	// 挂进它自己那一档。槽 -32 里是 0 就是没切，这一段整个跳过。
	back := b.label()
	a.MovLoad(x64.RDI, x64.RBP, -32)
	a.Test(x64.RDI, x64.RDI)
	a.Jcc(x64.E, back)
	a.MovLoad(x64.RSI, x64.RBP, -40)   // 这一笔要的数据尺寸
	a.MovStore32(x64.RDX, -4, x64.RSI) // 用掉那一块：头里的尺寸改成它自己那份
	a.Add(x64.RSI, x64.RDX)
	a.AddImm(x64.RSI, 8) // 剩下那一节的数据起点（它的头在 RSI-8）
	a.Mov(x64.RCX, x64.RDI)
	a.AddImm(x64.RCX, -8) // 它的数据尺寸＝总尺寸减一颗头
	a.MovStore32(x64.RSI, -4, x64.RCX)
	a.MovImm(x64.RCX, FreeSentinel)
	a.MovStore32(x64.RSI, -8, x64.RCX) // 类型字＝"在链上"，与 @free 同一写法：重复归还是空操作
	a.Mov(x64.RAX, x64.RDI)            // heapClass 收的是总尺寸
	b.heapClass()                      // r9 = 第几档（只动 r8／r9，其余寄存器留着）
	a.ShlImm(x64.R9, 3)
	b.leaRIP(x64.RDI, "freeHeads")
	a.Add(x64.RDI, x64.R9)
	a.MovLoad(x64.RCX, x64.RDI, 0)
	a.MovStore(x64.RSI, 0, x64.RCX) // 下一格写进它的数据区头 8 字节
	a.MovStore(x64.RDI, 0, x64.RSI)
	a.Mark(back)
	a.MovLoad(x64.RCX, x64.RBP, -16)
	a.MovStore32(x64.RDX, -8, x64.RCX) // 用掉那一块的类型字（切过一刀的，尺寸那格已经对过了）
	a.Mov(x64.RAX, x64.RDX)
	b.epilogue()

	a.Mark(room)
	b.loadRIP(x64.RSI, "heapCur")
	b.loadRIP(x64.RDX, "heapEnd")
	a.MovLoad(x64.RAX, x64.RBP, -24)
	a.Add(x64.RAX, x64.RSI)
	a.Cmp(x64.RAX, x64.RDX)
	bump := b.label()
	a.Jcc(x64.BE, bump)

	// 现有的块不够：这一轮要多少，1 MiB 与这一笔取大者，再按页往上凑。
	a.MovImm(x64.RAX, minChunk)
	a.MovLoad(x64.RDX, x64.RBP, -24)
	a.AddImm(x64.RDX, page-1)
	a.AndImm(x64.RDX, -page)
	a.Cmp(x64.RAX, x64.RDX)
	enough := b.label()
	a.Jcc(x64.AE, enough) // 1 MiB 已经不小于这一笔，就用它
	a.Mov(x64.RAX, x64.RDX)
	a.Mark(enough)
	a.MovStore(x64.RBP, -32, x64.RAX)

	a.Xor(x64.RCX, x64.RCX)
	a.MovLoad(x64.RDX, x64.RBP, -32)
	a.MovImm(x64.R8, allocCommitReserve)
	a.MovImm(x64.R9, pageReadWrite)
	b.impCall("VirtualAlloc")
	ok := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, ok)
	a.MovImm(x64.RCX, codeNoRoom)
	b.leaRIP(x64.RDX, "sOOM")
	b.call("@throw") // 没人接的那条路：印一行、退出码 1。帧链做好之前抛出都到这儿
	a.Mark(ok)

	// 新的一块：起点交给 rsi，终点记进 heapEnd。heapCur 由下面那段公共尾巴推。
	a.Mov(x64.RSI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RBP, -32)
	a.Add(x64.RDX, x64.RSI)
	b.storeRIP(x64.RDX, "heapEnd")
	// 第一块的起点单独记一笔：@heap_report 报的总量要从它算（见 heapFrom 那一句）
	b.loadRIP(x64.RDX, "heapFrom")
	a.Test(x64.RDX, x64.RDX)
	known := b.label()
	a.Jcc(x64.NE, known)
	a.Mov(x64.RDX, x64.RSI)
	b.storeRIP(x64.RDX, "heapFrom")
	a.Mark(known)
	a.Jmp(bump)

	a.Mark(bump)
	// rsi 是这一笔的起点：写好头，把堆指针推过"头 + 数据"，返回头后面那个地址。
	a.MovLoad(x64.RCX, x64.RBP, -16)
	a.MovStore32(x64.RSI, 0, x64.RCX)
	a.MovLoad(x64.RCX, x64.RBP, -8)
	a.MovStore32(x64.RSI, 4, x64.RCX)
	a.MovLoad(x64.RDX, x64.RBP, -24)
	a.Add(x64.RDX, x64.RSI)
	b.storeRIP(x64.RDX, "heapCur")
	a.LeaMem(x64.RAX, x64.RSI, 8)
	b.epilogue()
}

// heapClass 把 rax 里那个"总尺寸（含 8 字节头）"折成第几档：返回 r9 = 档号，
// r8 = 那一档的上界（调用者拿它再比一次就知道这一笔是不是太大、不该进链）。
//
// 十二轮移位而不是 lzcnt／bsr：那两条的操作码与前缀正是这台机器上容易出编码错误的
// 一类（如 ucomisd），而这里每次分配最多多跑十几条指令，换一处不会错。
//
// en: heapClass folds the "total size including the 8-byte header" in rax into a band number:
// r9 = the band, r8 = that band's ceiling, which the caller compares once more to learn whether
// this allocation is too large for the chain at all. Twelve shift rounds instead of lzcnt/bsr:
// those two instructions' opcode and prefix are the class most likely to carry an encoding
// error on this machine (like ucomisd), and at most a dozen extra instructions per allocation
// buys a place that cannot be wrong.
func (b *rt) heapClass() {
	a := b.a
	power := b.label()
	done := b.label()
	a.CmpImm(x64.RAX, freeExactMax)
	a.Jcc(x64.A, power)
	a.Mov(x64.R8, x64.RAX) // 低段：一档一种尺寸，上界就是它自己
	a.Mov(x64.R9, x64.RAX)
	a.ShrImm(x64.R9, 3)
	a.AddImm(x64.R9, -2) // 总尺寸 16 → 第 0 档
	a.Jmp(done)
	a.Mark(power)
	a.Xor(x64.R9, x64.R9)
	a.MovImm(x64.R8, 1024)
	a.AddImm(x64.R9, freeExactClasses)
	top := b.label()
	a.Mark(top)
	a.Cmp(x64.R8, x64.RAX)
	a.Jcc(x64.AE, done) // 上界已经不小于这一笔，就停在这一档
	a.AddImm(x64.R9, 1)
	a.ShlImm(x64.R8, 1)
	a.CmpImm(x64.R9, freeClasses-1)
	a.Jcc(x64.BE, top)
	a.Mark(done)
}

// @free(rcx=那一块的地址)：还回堆里，返回 nothing。
//
// 三种情况不做处理，都是设计而不是遗漏：句柄是空的；头里的尺寸是 0（那说明这块不在堆上——
// 静态文本池里每条字面量前面挂的正是全 0 的占位头，见 internal/codegen/data.go）；
// 头里的类型字已经是那个记号（说明它早就挂在链上，重复归还是空操作，而不是把链弄断）。
// 比最大那一档还大的块也不进链：当它已经没了。
//
// 归还的块**不清零**——清零是取用那一侧的事（@alloc 从链上摘下来之后抹）。
// 理由是要释放的块可能很大，而"没人看的内存写零"是纯浪费；两头各管一段，读代码时也分得清。
//
// en: @free (rcx = the block's address): return it to the heap, returns nothing. Three cases do
// nothing, all by design rather than omission: the handle is empty; the header's size is 0,
// meaning the block is not on the heap (the all-zero placeholder headers in front of each
// static text-pool literal, see internal/codegen/data.go); or the header's type word already is
// that sentinel, meaning it is already on the chain, so returning it twice is a no-op instead
// of breaking the chain. A block larger than the biggest band is likewise not chained - it is
// treated as gone. A returned block is NOT zeroed: zeroing is the acquiring side's job (@alloc
// wipes it after unlinking). The reason is that the block being released may be large, and
// writing zeroes into memory nobody will read is pure waste, while splitting the two duties
// also makes the code readable.
func free(b *rt) {
	a := b.a
	b.prologue()
	a.MovStore(x64.RBP, -8, x64.RCX)
	done := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, done)              // 空句柄
	a.MovZX32(x64.RAX, x64.RCX, -4) // 头里的数据尺寸
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, done)
	a.MovZX32(x64.R8, x64.RCX, -8) // 头里的类型字
	a.MovImm(x64.R9, FreeSentinel)
	a.Cmp(x64.R8, x64.R9)
	a.Jcc(x64.E, done)
	a.AddImm(x64.RAX, 8) // 总尺寸
	b.heapClass()
	a.Cmp(x64.R8, x64.RAX)
	a.Jcc(x64.B, done) // 太大那一档：不进链
	a.ShlImm(x64.R9, 3)
	b.leaRIP(x64.RDI, "freeHeads")
	a.Add(x64.RDI, x64.R9)
	a.MovLoad(x64.RSI, x64.RDI, 0)
	a.MovStore(x64.RCX, 0, x64.RSI) // 下一格写进数据区头 8 字节，头上那两个槽留着
	a.MovStore(x64.RDI, 0, x64.RCX)
	a.MovImm(x64.R8, FreeSentinel)
	a.MovStore32(x64.RCX, -8, x64.R8)
	a.Mark(done)
	b.epilogue()
}

// @copy_block(rcx=源的那一块, rdx=要拷几个字节)：返回 rax=新的一块。
//
// `struct` 是值类型，可它在内存里是一整块（句柄模型），所以"整份拷贝"必须
// 另起一块而不是照抄句柄——抄了句柄就有两个持有者拿着同一块，改这个字段那个字段跟着变，
// 值类型唯一的理由就没了。标记从源那块的头上读（那 8 字节头：低四字节是类型标识），
// 新块因此仍然是同一个 struct 的类型，`is`、`as` 照旧识别。
//
// 一次一个字节就够：一个 struct 至多几十字节，拷贝的次数才是数量级，这里不值得省指令。
//
// en: @copy_block (rcx = the source block, rdx = bytes to copy): returns rax = a new block. A
// `struct` is a value type but in memory is a whole block (the handle model), so "copy
// it wholesale" must allocate rather than duplicate the handle - two holders of one block would
// mean writing this field changes that one, and the only reason a value type exists would be
// gone. The tag is read off the source's header (the 8-byte header: low four bytes are the
// type tag), so the new block is still the same struct's type and `is`/`as` keep recognising
// it. One byte at a time is enough here: a struct is at most a few dozen bytes while the number
// of copies is what scales, so this is not where to save instructions.
func copyBlock(b *rt) {
	const (
		xSrc  = 0
		xSize = 1
		xNew  = 2
		xI    = 3
	)
	a := b.a
	b.prologue()
	b.put(xSrc, x64.RCX)
	b.put(xSize, x64.RDX)

	a.MovZX32(x64.RDX, x64.RCX, -8) // 源那块的类型标识
	b.put(4, x64.RDX)
	b.get(x64.RCX, xSize)
	b.get(x64.RDX, 4)
	b.call("@alloc")
	b.put(xNew, x64.RAX)

	a.MovImm(x64.RAX, 0)
	b.put(xI, x64.RAX)
	top := b.label()
	done := b.label()
	a.Mark(top)
	b.get(x64.RAX, xI)
	b.get(x64.RDX, xSize)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, done)
	b.get(x64.RSI, xSrc)
	a.Add(x64.RSI, x64.RAX)
	b.get(x64.RDI, xNew)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad8(x64.RDX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RDX)
	b.get(x64.RAX, xI)
	a.AddImm(x64.RAX, 1)
	b.put(xI, x64.RAX)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, xNew)
	b.epilogue()
}

// copySlot 发一段"抄一格并把游标往前推"的机器码：把第 src 块的第 srcIdx 格抄到第 dst 块的
// 第 dstIdx 格，随后把 adv 里点名的那几个槽各加一（@list_sort 的三处抄法都是这一段）。
// 只用调用方寄存器，槽位上的游标跨不过一次 call，所以调用前后都要从槽里重取。
//
// en: copySlot emits "copy one slot and advance the cursors": slot srcIdx of block src into
// slot dstIdx of block dst, then increment each slot named by adv (all three copying
// sites inside @list_sort use this). Only caller-saved registers are used and slot cursors do
// not survive a call, so they are reloaded from their slots before and after each call.
func (b *rt) copySlot(src, srcIdx, dst, dstIdx int, adv ...int) {
	a := b.a
	b.get(x64.RSI, src)
	b.get(x64.RAX, srcIdx)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RSI, 0)
	b.get(x64.RDI, dst)
	b.get(x64.RAX, dstIdx)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovStore(x64.RDI, 0, x64.RDX)
	for _, slot := range adv {
		b.get(x64.RAX, slot)
		a.AddImm(x64.RAX, 1)
		b.put(slot, x64.RAX)
	}
}

// @list_index_of(rcx=句柄, rdx=那一样, r8=怎么算相等)：返回 rax=头一个相等的位置是第几格，
// 一个位置都没有返回 -1。
//
// 怎么算相等与 @list_remove 同一条：0 比那 64 位本身（整数家族、枚举、句柄按身份），
// 1 走 @str_eq 按内容比文本。降级那头按元素类型定这一个数，两条用的是同一支判断。
//
// en: @list_index_of (rcx = handle, rdx = the element, r8 = how equality is decided): returns
// rax = the index of the first equal position, -1 when there is none. Equality follows
// @list_remove's rule: 0 compares the 64 bits themselves (integer family, enums, handles by
// identity), 1 goes through @str_eq comparing text by content. Lowering picks that
// number from the element type, and both routines use the same comparison.
func listIndexOf(b *rt) {
	const (
		qList = 0 // 句柄
		qVal  = 1 // 要找的那一样
		qMode = 2 // 怎么算相等
		qCur  = 3 // 游标（跨过 @str_eq 要留住）
		qLeft = 4 // 还剩几格
		qBeg  = 5 // 数据块起点，命中时用它算第几格
	)
	a := b.a
	b.prologue()
	b.put(qList, x64.RCX)
	b.put(qVal, x64.RDX)
	b.put(qMode, x64.R8)

	b.get(x64.RDI, qList)
	a.MovLoad(x64.R9, x64.RDI, cLen)
	a.MovLoad(x64.RSI, x64.RDI, cData)
	b.put(qBeg, x64.RSI)

	find := b.label()
	found := b.label()
	gone := b.label()
	numWay := b.label()
	step := b.label()
	a.Mark(find)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, gone)
	a.MovLoad(x64.RAX, x64.RSI, 0)
	b.get(x64.R8, qMode)
	a.Test(x64.R8, x64.R8)
	a.Jcc(x64.E, numWay)
	// 按内容比文本：游标与剩余数先放进局部槽，call 一过寄存器就作废了
	b.put(qCur, x64.RSI)
	b.put(qLeft, x64.R9)
	a.Mov(x64.RCX, x64.RAX)
	b.get(x64.RDX, qVal)
	b.call("@str_eq")
	b.get(x64.RSI, qCur)
	b.get(x64.R9, qLeft)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, found)
	a.Jmp(step)

	a.Mark(numWay)
	b.get(x64.RDX, qVal)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.E, found)

	a.Mark(step)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.R9, -1)
	a.Jmp(find)

	a.Mark(found)
	b.get(x64.RAX, qBeg)
	a.Sub(x64.RSI, x64.RAX) // 差几个字节
	a.ShrImm(x64.RSI, 3)    // 除以每格 8 字节
	a.Mov(x64.RAX, x64.RSI)
	b.epilogue()

	a.Mark(gone)
	a.MovImm(x64.RAX, -1)
	b.epilogue()
}

// @list_sort(rcx=句柄, rdx=怎么比)：就地排。
//
// 怎么比三档，由降级那头按元素类型定：0 按那 64 位（int、byte、unicode，有符号比），
// 1 按文本内容（@str_cmp），2 按小数（@f_order 的位型比较，NaN 那一档无序、留在左边）。
// 这三档正是语言划定"谁能比大小"的三种走法，排序不另定一套相等与先后。
//
// 用的是自下而上的归并：宽 1、2、4……一轮一轮并，每轮把并好的那一段抄回数据块。
// 另外要一块 N 格的数据区（要不到就是 10091 号，@alloc 自己抛），不递归，因此不吃栈。
// 排完把"改过几次"推一个位置：正在遍历这一只容器的那一圈下一轮抛 10033 号，
// 不给它拿着旧顺序继续走的机会。
//
// en: @list_sort (rcx = handle, rdx = how to compare): sort in place.
// Comparison has three settings, chosen by lowering from the element type: 0 by the 64 bits
// (int, byte, unicode, signed compare), 1 by text content (@str_cmp), 2 by float (the @f_order
// bit-pattern compare, where NaN is unordered and stays on the left). These three are exactly
// the three routes the language drew for "who may be ordered", so sorting invents no second
// notion of equality or precedence. Bottom-up merge sort: runs of width 1, 2, 4... merged round
// by round, each round copying the merged span back into the data block. One extra N-slot area
// is needed (if it cannot be had, @alloc throws 10091); it does not recurse, so it eats no
// stack. When sorted, the mutation counter advances so an iteration over this container meets
// code 10033 on its next round rather than continuing on the old order.
func listSort(b *rt) {
	const (
		sList  = 0  // 句柄
		sMode  = 1  // 怎么比
		sN     = 2  // 几格
		sBuf   = 3  // 那一块 N 格的临时数据区
		sWidth = 4  // 正在并的段有多宽
		sI     = 5  // 这一轮的起点
		sL     = 6  // 左段游标
		sR     = 7  // 右段游标
		sLEnd  = 8  // 左段尽头
		sREnd  = 9  // 这一段合并的尽头
		sData  = 10 // 数据块起点
		sK     = 11 // 往临时区写的位置，抄回去时也用
	)
	a := b.a
	b.prologue()
	b.put(sList, x64.RCX)
	b.put(sMode, x64.RDX)
	b.get(x64.RAX, sList)
	a.MovLoad(x64.RAX, x64.RAX, cLen)
	b.put(sN, x64.RAX)
	a.CmpImm(x64.RAX, 2)
	touch := b.label()
	a.Jcc(x64.B, touch) // 一格以下的list没什么可排
	b.get(x64.RAX, sList)
	a.MovLoad(x64.RAX, x64.RAX, cData)
	b.put(sData, x64.RAX)

	// 临时区：N 格，每格 8 字节；它不是任何对象，头里的类型字填 0
	b.get(x64.RCX, sN)
	a.MovImm(x64.R8, 8)
	a.Imul(x64.RCX, x64.R8)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.put(sBuf, x64.RAX)

	a.MovImm(x64.RAX, 1)
	b.put(sWidth, x64.RAX)

	outer := b.label()
	outerDone := b.label()
	run := b.label()
	runDone := b.label()
	merge := b.label()
	takeLeft := b.label()
	takeRight := b.label()
	copyLeft := b.label()
	copyRight := b.label()
	runNext := b.label()
	cpTop := b.label()
	cpDone := b.label()
	numCmp := b.label()
	fltCmp := b.label()

	a.Mark(outer)
	b.get(x64.RAX, sWidth)
	b.get(x64.RDX, sN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, outerDone)
	a.Xor(x64.RAX, x64.RAX)
	b.put(sI, x64.RAX)

	// 一轮里的一段：左段 [i, min(i+width, n))，右段 [r, min(r+width, n))
	a.Mark(run)
	b.get(x64.RAX, sI)
	b.get(x64.RDX, sN)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, runDone)
	b.get(x64.RAX, sI)
	b.put(sL, x64.RAX)
	b.get(x64.RAX, sI)
	b.get(x64.RDX, sWidth)
	a.Add(x64.RAX, x64.RDX)
	b.get(x64.RDX, sN)
	a.Cmp(x64.RAX, x64.RDX)
	lessR := b.label()
	a.Jcc(x64.B, lessR)
	b.get(x64.RAX, sN)
	a.Mark(lessR)
	b.put(sR, x64.RAX)
	b.put(sLEnd, x64.RAX)
	b.get(x64.RDX, sWidth)
	a.Add(x64.RAX, x64.RDX)
	b.get(x64.RDX, sN)
	a.Cmp(x64.RAX, x64.RDX)
	lessE := b.label()
	a.Jcc(x64.B, lessE)
	b.get(x64.RAX, sN)
	a.Mark(lessE)
	b.put(sREnd, x64.RAX)
	b.get(x64.RAX, sI)
	b.put(sK, x64.RAX)

	// 两段都还有东西就比较，谁小抄谁；一样大抄左边那一个（这一版定的是稳的排法）
	a.Mark(merge)
	b.get(x64.RAX, sL)
	b.get(x64.RDX, sLEnd)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, copyRight)
	b.get(x64.RAX, sR)
	b.get(x64.RDX, sREnd)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, copyLeft)
	// RCX = 右段当前那个位置，RDX = 左段当前那个位置：比较时"右 < 左"才往右拿
	b.get(x64.RSI, sData)
	b.get(x64.RAX, sR)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RSI, x64.RAX)
	a.MovLoad(x64.RCX, x64.RSI, 0)
	b.get(x64.RDI, sData)
	b.get(x64.RAX, sL)
	a.ShlImm(x64.RAX, 3)
	a.Add(x64.RDI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RDI, 0)
	b.get(x64.R8, sMode)
	a.Test(x64.R8, x64.R8)
	a.Jcc(x64.E, numCmp)
	a.CmpImm(x64.R8, 1)
	a.Jcc(x64.NE, fltCmp)
	b.call("@str_cmp") // 甲=右、乙=左：交回 -1 就是右段那一个在前
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, takeRight)
	a.Jmp(takeLeft)

	a.Mark(numCmp)
	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.S, takeRight) // 有符号：右 < 左
	a.Jmp(takeLeft)

	a.Mark(fltCmp)
	b.call("@f_order") // 掩码 1=小、2=等、4=大、0=无序（NaN 留在左边）
	a.AndImm(x64.RAX, 1)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, takeRight)
	a.Jmp(takeLeft)

	// 往临时区抄一个：从数据块的 S 格抄到 K 格，再把 S 往前推一个位置
	a.Mark(takeLeft)
	b.copySlot(sData, sL, sBuf, sK, sL, sK)
	a.Jmp(merge)
	a.Mark(takeRight)
	b.copySlot(sData, sR, sBuf, sK, sR, sK)
	a.Jmp(merge)

	a.Mark(copyLeft)
	b.copySlot(sData, sL, sBuf, sK, sL, sK)
	b.get(x64.RAX, sL)
	b.get(x64.RDX, sLEnd)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.B, copyLeft)
	a.Jmp(runNext)
	a.Mark(copyRight)
	b.copySlot(sData, sR, sBuf, sK, sR, sK)
	b.get(x64.RAX, sR)
	b.get(x64.RDX, sREnd)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.B, copyRight)

	// 这一段并完了：把临时区的 [i, rend) 抄回数据块，下一段从 rend 起
	a.Mark(runNext)
	b.get(x64.RAX, sI)
	b.put(sK, x64.RAX)
	a.Mark(cpTop)
	b.get(x64.RAX, sK)
	b.get(x64.RDX, sREnd)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, cpDone)
	b.copySlot(sBuf, sK, sData, sK, sK)
	a.Jmp(cpTop)
	a.Mark(cpDone)
	b.get(x64.RAX, sREnd)
	b.put(sI, x64.RAX)
	a.Jmp(run)

	a.Mark(runDone)
	b.get(x64.RAX, sWidth)
	a.Add(x64.RAX, x64.RAX)
	b.put(sWidth, x64.RAX)
	a.Jmp(outer)

	a.Mark(outerDone)
	b.get(x64.RCX, sBuf)
	b.call("@free")
	a.Mark(touch)
	b.get(x64.RCX, sList)
	b.call("@list_touch")
	b.epilogue()
}

// @dict_entries(rcx=表, rdx=要哪一头, r8=怎么交, r9=类型字)：返回 rax=新的 list。
//
// rdx：0 交键、1 交值。r8：0 把那个位置的 64 位原样搬过去（整数家族、枚举），
// 1 是文本，走 @str_sub 拷一份出来——不拷就有两个位置指着同一块，谁都能析构一次。
// 能在检查阶段走到这一条的类型只有这两类，其余的（class、容器、盒子、struct）在检查阶段就拒了。
// 顺序不保证，与遍历同一个说法。
//
// en: @dict_entries (rcx = table, rdx = which end, r8 = how to hand over, r9 = type word):
// returns rax = a new list. rdx: 0 keys, 1 values. r8: 0 moves the position's 64
// bits as they are (integer family, enums), 1 means text and copies one out through @str_sub -
// without copying, two positions would point at one block and either could dismantle it.
// Only those two kinds can reach this member after checking; everything else (class,
// containers, boxes, struct) was refused at check time. Order is unspecified, the same
// statement as iteration.
func dictEntries(b *rt) {
	const (
		eTab  = 0 // 那只 dict
		eSide = 1 // 0 键、1 值
		eWay  = 2 // 0 原样、1 拷文本
		eTag  = 3 // 新 list 的类型字
		eOut  = 4 // 生成的 list
		eCur  = 5 // 当前桶的地址（下一趟从它往后一格起找）
		eOne  = 6 // 这一趟取到的那一个
	)
	a := b.a
	b.prologue()
	b.put(eTab, x64.RCX)
	b.put(eSide, x64.RDX)
	b.put(eWay, x64.R8)
	b.put(eTag, x64.R9)

	a.Xor(x64.RCX, x64.RCX)
	b.get(x64.RDX, eTag)
	b.call("@list_new")
	b.put(eOut, x64.RAX)

	b.get(x64.RDI, eTab)
	a.MovLoad(x64.RSI, x64.RDI, cData)
	b.put(eCur, x64.RSI)

	top := b.label()
	keyWay := b.label()
	gotOne := b.label()
	textWay := b.label()
	next := b.label()
	done := b.label()

	a.Mark(top)
	b.get(x64.RCX, eTab)
	b.get(x64.RDX, eCur)
	b.call("@map_next")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, done)
	b.put(eCur, x64.RAX)
	a.Mov(x64.RSI, x64.RAX)
	b.get(x64.RAX, eSide)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, keyWay)
	a.MovLoad(x64.RAX, x64.RSI, bVal)
	a.Jmp(gotOne)
	a.Mark(keyWay)
	a.MovLoad(x64.RAX, x64.RSI, bKey)
	a.Mark(gotOne)
	b.put(eOne, x64.RAX)

	b.get(x64.RDX, eWay)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, textWay)
	b.get(x64.RCX, eOut)
	b.get(x64.RDX, eOne)
	b.call("@list_add")
	a.Jmp(next)

	a.Mark(textWay)
	// 拷一份：@str_sub(那一块, 起 0, 它自己的字节数)
	b.get(x64.RCX, eOne)
	a.Xor(x64.RDX, x64.RDX)
	b.get(x64.R8, eOne)
	a.MovLoad(x64.R8, x64.R8, StrBytes)
	b.call("@str_sub")
	b.get(x64.RCX, eOut)
	a.Mov(x64.RDX, x64.RAX)
	b.call("@list_add")

	a.Mark(next)
	b.get(x64.RSI, eCur)
	a.AddImm(x64.RSI, stride)
	b.put(eCur, x64.RSI)
	a.Jmp(top)

	a.Mark(done)
	b.get(x64.RAX, eOut)
	b.epilogue()
}

// @list_pop_last(rcx=list)：返回 rax=最后那个位置里的值，那个位置写空、数目退一、次数推一。
//
// 空的时侯返回 0，并且不推次数——`xs.clear()` 就是一圈一圈取到空为止，
// 而"遍历途中改了容器"那一问只看次数变没变，空表上没改过就不该报。
// 析构那个位置的事留给降级的理由：形状表在编译器手里，运行期这一侧只看得到那 64 位。
//
// en: @list_pop_last (rcx = list): returns rax = the value in the last position, which is then
// emptied, the count drops and the version advances. When the list is empty it
// returns 0 and does NOT advance the version: `xs.clear()` is simply popping until empty, and
// the "did the container change during iteration" question looks only at the version counter -
// nothing changed on an empty list, so nothing should be reported. The dismantling is left to
// lowering because the shape table is in the compiler's hands and the runtime
// side only sees those 64 bits.
func listPopLast(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)

	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	nada := b.label()
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, nada)
	a.AddImm(x64.RAX, -1)
	a.MovStore(x64.RDI, cLen, x64.RAX)
	a.MovLoad(x64.RSI, x64.RDI, cData)
	a.ShlImm(x64.RAX, 3) // 第几字节：每格 8 字节
	a.Add(x64.RSI, x64.RAX)
	a.MovLoad(x64.RDX, x64.RSI, 0) // 交回的那一个值
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RSI, 0, x64.RAX) // 那一格从此空着
	a.MovLoad(x64.RAX, x64.RDI, cVer)
	a.AddImm(x64.RAX, 1)
	a.MovStore(x64.RDI, cVer, x64.RAX)
	a.Mov(x64.RAX, x64.RDX)
	b.epilogue()

	a.Mark(nada)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
}

// @set_op(rcx=甲, rdx=乙, r8=0 并／1 交／2 差, r9=文本元素拷不拷, 第五格=新表的类型字)：
// 返回 rax=一只新表，两边都不动。
//
// 返回新造的一块，理由与 `dict.keys` 同一条：把乙那个位置的句柄直接搬进新表，就有两个位置
// 认领同一块，谁都能析构它一次。文本元素拷一份（r9 那一面旗），别的种类都是值类型——
// 检查阶段按同一条范围拒绝了句柄型（能走到这儿的只有值类型与文本）。
//
// 顺序不保证：走的是桶数组的下标顺序，与 `for (k in d)` 同一顺序。
// 并集走两圈、交与差只走甲那一圈（乙只用来问"这个键在不在"）。
//
// en: @set_op (rcx = a, rdx = b, r8 = 0 union / 1 intersection / 2 difference, r9 = whether
// text elements are copied, fifth slot = the new table's type word): returns rax = a new table
// with both inputs untouched. A fresh block is returned for the same reason
// as `dict.keys`: moving b's handle straight into the new table would have two positions
// claiming one block, either able to dismantle it. Text elements are copied according to r9's
// flag; every other kind is a value type - handle-typed elements were refused at check time
// under the same scope, so only value types and text reach here. Order is unspecified:
// the walk follows bucket-array indices, the same order as `for (k in d)`. Union runs both
// sides, intersection and difference run only a (b is used just to ask "is this key there").
func setOp(b *rt) {
	const (
		sNew   = 0 // 生成的新表
		sA     = 1 // 甲
		sB     = 2 // 乙
		sWhich = 3 // 0 并、1 交、2 差
		sWay   = 4 // 0 原样、1 拷文本
		sMode  = 5 // 键怎么算散列与相等，从甲表里取
		sTag   = 6 // 新表的类型字
		sCur   = 7 // 当前桶
		sKey   = 8 // 这一格的键
		sHit   = 9 // 问乙表的结果
	)
	a := b.a
	b.prologue()
	b.put(sA, x64.RCX)
	b.put(sB, x64.RDX)
	b.put(sWhich, x64.R8)
	b.put(sWay, x64.R9)
	a.MovLoad(x64.RAX, x64.RBP, arg5)
	b.put(sTag, x64.RAX)

	b.get(x64.RDI, sA)
	a.MovLoad(x64.RAX, x64.RDI, cMode)
	b.put(sMode, x64.RAX)

	a.Xor(x64.RCX, x64.RCX)
	b.get(x64.RDX, sTag)
	b.get(x64.R8, sMode)
	b.call("@map_new")
	b.put(sNew, x64.RAX)

	// 走一遍 tab 那个位置的桶数组：该收的就收进新表，走完落到 after
	sweep := func(tab int, after x64.Label) {
		top := b.label()
		ask := b.label()
		take := b.label()
		raw := b.label()

		b.get(x64.RAX, tab)
		a.MovLoad(x64.RSI, x64.RAX, cData)
		b.put(sCur, x64.RSI)

		a.Mark(top)
		b.get(x64.RCX, tab)
		b.get(x64.RDX, sCur)
		b.call("@map_next")
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, after)
		a.Mov(x64.RSI, x64.RAX) // 这一格的桶
		a.AddImm(x64.RAX, stride)
		b.put(sCur, x64.RAX) // 下一趟从它后面那一格起
		a.MovLoad(x64.RAX, x64.RSI, bKey)
		b.put(sKey, x64.RAX)

		b.get(x64.RAX, sWhich)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, take) // 并集：这一格一定要收
		b.get(x64.RCX, sB)
		b.get(x64.RDX, sKey)
		b.call("@map_contains")
		b.put(sHit, x64.RAX)
		b.get(x64.RAX, sWhich)
		a.CmpImm(x64.RAX, 1)
		a.Jcc(x64.NE, ask)
		b.get(x64.RAX, sHit)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, top) // 交集而乙没有：看下一格
		a.Jmp(take)
		a.Mark(ask)
		b.get(x64.RAX, sHit)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.NE, top) // 差集而乙有：看下一格

		a.Mark(take)
		// 新表里已经有这个位置就算了：省一次白拷
		b.get(x64.RCX, sNew)
		b.get(x64.RDX, sKey)
		b.call("@map_contains")
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.NE, top)
		b.get(x64.RAX, sWay)
		a.Test(x64.RAX, x64.RAX)
		a.Jcc(x64.E, raw)
		b.get(x64.RCX, sKey)
		a.Xor(x64.RDX, x64.RDX)
		b.get(x64.R8, sKey)
		a.MovLoad(x64.R8, x64.R8, StrBytes)
		b.call("@str_sub")
		b.put(sKey, x64.RAX)
		b.get(x64.RCX, sNew)
		b.get(x64.RDX, sKey)
		a.Xor(x64.R8, x64.R8)
		b.call("@map_put")
		a.Jmp(top)
		a.Mark(raw)
		b.get(x64.RCX, sNew)
		b.get(x64.RDX, sKey)
		a.Xor(x64.R8, x64.R8)
		b.call("@map_put")
		a.Jmp(top)
	}

	afterA := b.label()
	afterB := b.label()
	out := b.label()

	sweep(sA, afterA)
	a.Mark(afterA)
	b.get(x64.RAX, sWhich)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, out) // 交与差只走甲这一圈就够了
	sweep(sB, afterB)
	a.Mark(afterB)

	a.Mark(out)
	b.get(x64.RAX, sNew)
	b.epilogue()
}

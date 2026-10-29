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

// http.go 实现 `http` 组的运行期入口：HTTP 客户端。
//
// 这一层不构造请求行、不做分帧计数，全部交给系统的 `winhttp.dll`：TLS、重定向、证书
// 这三件事自行实现要排到 `1.0` 之后，而它们恰恰是"拷走就能跑"的脚本真正要用的。
// winhttp 是同步接口，与没有线程的设定一致；`WinHttpSetTimeouts` 四个阶段
// （解析、连接、发送、接收）都接收同一个毫秒数，比 `net.connect` 多管一段——
// 那一侧 `getaddrinfo` 不接受超时，这一侧接受。
//
// **一次请求拆成五条例程**，由降级侧排成固定顺序（`internal/lower/lower.go` 的 `httpAction`）：
//
//	@http_begin(网址, 要发送的块或 0, 超时毫秒)   拆网址、打开会话、设置超时、连接、打开请求、发送、接收响应
//	@http_status(那块 Response)              把状态码写入第一个槽
//	@http_headers(那块 Response)             把整块原始头拆成 list<string> 写入第二个槽
//	@http_body(那块 Response)                一圈读取一段，构造 bytes 块写入第三个槽
//	@http_close()                              关闭那三只句柄
//
// 拆分的理由：一次请求要调用系统九步，中间还要拆头部、循环接收正文——全部塞进一条例程，
// 帧上的槽要二十几个，寄存器每一次 call 之后都要重新装载，错一处就覆写别人的内存。
// 拆开之后每一段只使用自己的那几个槽；句柄与网址保存在静态槽里（没有线程，
// 同一时刻只有一件事在运行，与 wPath／aBuf 同一个道理）。拆成五段同时把归属理清：
// 三样值都由运行期直接写入那一块，降级侧没有中间值要登记，句子结束时析构的是那一块，
// 按形状表递归。
//
// 错误答复沿用 `net` 那六个号（10100～10105）：winhttp 的号码由 `GetLastError`
// 取得，折算表与 winsock 那二十行合并在 `netCodeOf` 那一张表里，消息一律带上系统原号码。
//
// 寄存器纪律与 net.go 相同：每一次 call 之后都视为 rax／rcx／rdx／r8～r11 已被踩过，
// 要保留的值一律写入帧上的槽。
//
// en: http.go implements the runtime entries of the `http` group: an HTTP client. This layer
// neither builds a request line nor counts framing: all of it is handed to the system's
// `winhttp.dll`. TLS, redirects and certificates would take until after 1.0 to do oneself, and
// those three are exactly what a copy-and-run script needs. winhttp is synchronous, which fits
// the no-threads design; WinHttpSetTimeouts takes the same millisecond figure for all four
// phases (resolve, connect, send, receive), one phase more than net.connect's - that side's
// getaddrinfo accepts no timeout, this one does. ONE REQUEST IS SPLIT INTO FIVE ROUTINES,
// arranged in a fixed order by the lowering side (httpAction in internal/lower/lower.go):
// @http_begin(url, block to send or 0, timeout ms) cracks the URL, opens the session, sets
// timeouts, connects, opens the request, sends and receives the response; @http_status(that
// Response) writes the status code into the first slot; @http_headers(that Response) splits the
// raw header block into a list<string> in the second slot; @http_body(that Response) reads a
// chunk per round, builds a bytes block and writes it into the third slot; @http_close() closes
// the three handles. Why this split: one request calls the system nine times and additionally
// splits headers and loops the body - stuffed into one routine that needs twenty-plus frame
// slots, every register reloaded after each call, and one mistake overwrites somebody else's
// memory. Split, each part lives only in its own few slots; the handles and URL live in static
// slots (no threads, so exactly one thing runs at a time - the same reasoning as wPath/aBuf).
// Five parts also puts ownership straight: all three values are written directly into the block
// by the runtime, so the lowering side registers no intermediate, and at the end of the
// sentence the block itself is what the shape-table recursion dismantles. Error answers reuse
// net's six codes (10100-10105): winhttp's number comes from GetLastError and its conversion
// table joins winsock's twenty rows in netCodeOf, with the message always carrying the
// system's original number. Register discipline is net.go's rule: after every call assume
// rax/rcx/rdx/r8-r11 were clobbered and keep anything worth having in frame slots.
package runtime

import "neflang.org/nf/internal/x64"

// winhttp 侧的常数按 Microsoft 公开的值写死，不引用头文件。
//
// en: winhttp's constants are written as Microsoft's published values; no header file is
// imported.
const (
	portHTTP  = 80  // 网址省略端口时 http 使用的默认端口
	portHTTPS = 443 // 同上，https 的默认端口

	accessDefaultProxy = 0          // 沿用本机的 WinHTTP 代理设置，不自行配置
	flagSecure         = 0x00800000 // WinHttpOpenRequest：该请求走 TLS
	crByte             = 13         // 头部区的行分隔符是 CR 紧跟 LF

	queryStatusCode     = 0x13       // WINHTTP_QUERY_STATUS_CODE
	queryFlagNumber     = 0x20000000 // 交回的是数值，不是 "200" 三个字符
	queryRawHeadersCRLF = 0x16       // 整块原始头，行与行之间是 CRLF

	// winhttp 的错误号码（winhttp.h 里 ERROR_WINHTTP_*）。前面三个本组代码自己会产生；
	// 后面几个只出现在 `net.go` 的折算表里——它们是系统可能返回的号码。
	// 未折算的那些落 10100 兜底：不新增号码，但系统原号码一律带在消息里。
	errHttpTimeout       = 12002 // 等待响应超时
	errHttpInternal      = 12004 // winhttp 内部错误
	errHttpInvalidU      = 12005 // 网址无法解析（拆不出主机、端口非法、超出缓冲都归这一条）
	errHttpNoName        = 12007 // 主机名解析失败
	errHttpCannotConnect = 12029 // 无法建立连接
	errHttpBadResponse   = 12030 // 服务器响应格式非法
	errHttpConnError     = 12031 // 该连接已断开
	errHttpSecure        = 12175 // TLS 与证书协商失败

	httpHostCap = 256  // 主机名缓冲：宽字符个数（含结尾那个 0）
	httpPathCap = 2048 // 路径缓冲：同上（查询串就在路径末尾，一并收下）
	httpChunk   = 4096 // 一次 WinHttpReadData 最多读取这么多字节
	httpFirst   = 8192 // 正文累加缓冲的初始档位
	// 头部区转成 UTF-8 后写入静态的 aBuf（正文侧是 lineCap=65536 个字节）。
	// 一个码点最多三个字节，所以最多转换 65536/3 个宽字符——超过该长度的头部只转换前面这一截。
	httpHdrChars = 21845
)

// httpData 定义本组的静态数据。本组不需要新的报错句：状态码、头部、正文都是数据，
// 失败路径沿用 `net` 那六句，所以报错文案表没有任何新增。
//
// en: httpData lays out this group's static data. The group needs no new sentences: status
// code, headers and body are data, and the failure path reuses net's six sentences, so the
// table of error wording gained not one line.
func httpData(put func(name string, bs []byte)) {
	// 三只句柄与网址文本：一次请求之内由 @http_begin 写入，后面几段都使用它们；
	// @http_close 走完就抹回 0。没有线程，所以同一时刻只有一件事在运行。
	put("httpSess", make([]byte, 8))
	put("httpConn", make([]byte, 8))
	put("httpReq", make([]byte, 8))
	put("httpU", make([]byte, 8)) // 网址文本：10103 那一句把网址拼进"解析不到 …"里
	put("httpHost", make([]byte, 2*httpHostCap))
	put("httpPath", make([]byte, 2*httpPathCap))
	// 三个落点：httpDw 是"这块缓冲多大"（WinHttpQueryHeaders 进出都用它），
	// httpGot 是"本次读取的字节数"，httpStatus 是查询返回的状态码。
	put("httpDw", make([]byte, 8))
	put("httpGot", make([]byte, 8))
	put("httpStatus", make([]byte, 8))
	put("httpErr", make([]byte, 8))                        // 失败路径的号码：关句柄会踩 rax，先写入这一格
	put("httpAgent", []byte{'n', 0, 'e', 0, 'f', 0, 0, 0}) // 随请求发送的 User-Agent
	put("httpGet", []byte{'G', 0, 'E', 0, 'T', 0, 0, 0})
	put("httpPost", []byte{'P', 0, 'O', 0, 'S', 0, 'T', 0, 0, 0})
	put("httpChunkBuf", make([]byte, httpChunk))
}

// copyBytes 发射一段拷贝：rsi＝源、rdi＝目的、rdx＝字节数。只用 rax 作游标、rcx 作临时。
//
// 八个字节一批，不足八的尾巴按一字节一批。只按八个走会多写最多七个字节，而那七个位置是
// 下一块的块头——尾部处理不是收尾修饰，是本例程可用的前提条件。
//
// en: copyBytes emits a copy: rsi = source, rdi = destination, rdx = byte count. Only rax is
// used as the cursor and rcx as scratch. Eight bytes per batch, then the leftover under eight
// one byte at a time. Running eights first would write up to seven bytes too many, and those
// seven positions are the next block's header - the tail handling is not polish, it is the
// precondition for this routine being usable at all.
func copyBytes(b *rt) {
	a := b.a
	w8, w8done, w1, w1done := b.label(), b.label(), b.label(), b.label()
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(w8)
	a.Mov(x64.RCX, x64.RAX)
	a.AddImm(x64.RCX, 8)
	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.A, w8done)
	a.MovLoad(x64.RCX, x64.RSI, 0)
	a.MovStore(x64.RDI, 0, x64.RCX)
	a.AddImm(x64.RSI, 8)
	a.AddImm(x64.RDI, 8)
	a.AddImm(x64.RAX, 8)
	a.Jmp(w8)
	a.Mark(w8done)
	a.Mark(w1)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, w1done)
	a.MovLoad8(x64.RCX, x64.RSI, 0)
	a.MovStore8(x64.RDI, 0, x64.RCX)
	a.AddImm(x64.RSI, 1)
	a.AddImm(x64.RDI, 1)
	a.AddImm(x64.RAX, 1)
	a.Jmp(w1)
	a.Mark(w1done)
}

// httpFail 是本段共同的收尾：先关闭仍打开的句柄，再按 rax 里的号码抛出。
//
// 三只句柄各判一次 0：到达这一处的位置不同，有的还没打开任何一只，有的已经打开到第三只。
// 关完之后把该槽抹回 0——静态槽里留着已关闭的句柄，下一次请求会对其二次关闭。
//
// 号码必须先写入静态槽再去关句柄：WinHttpCloseHandle 返回 BOOL，会踩坏 rax——
// 直接把 rax 交给 @net_fail，抛出的号码是 1，看起来像系统报了"功能不对"，
// 而真正的 12002／12007 在关句柄那一步就已丢失（TestHttpCodes 与 TestHttpTimeout 固定这一行为）。
// 这里不碰任何帧槽：它发在调用方的帧里，写别人的槽就是覆写数据。
//
// en: httpFail is the shared tail of this section: close whatever handles are still open, then
// throw the number sitting in rax. Each of the three is tested against 0 separately, because
// the place that reached this code differs - some paths opened none, others had opened the
// third. After closing, the slot is written back to 0: a static slot holding an already-closed
// handle would make the next request close it twice. The number must be stored in its static
// slot BEFORE closing handles: WinHttpCloseHandle returns a BOOL and clobbers rax - handing rax
// to @net_fail directly threw "1", which reads as if the system said "wrong function", while
// the real 12002/12007 had already been lost in the close step (pinned by TestHttpCodes and
// TestHttpTimeout). Not one frame slot is touched here: this runs in the caller's frame, and
// writing another's slot overwrites live data.
func httpFail(b *rt) {
	a := b.a
	b.leaRIP(x64.RDX, "httpErr")
	a.MovStore(x64.RDX, 0, x64.RAX)
	httpCloseHandles(b)
	b.leaRIP(x64.RDX, "httpU")
	a.MovLoad(x64.RDX, x64.RDX, 0) // 网址文本：只有 10103 那一句会把它拼进话里
	b.leaRIP(x64.RCX, "httpErr")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	b.call("@net_fail")
}

// httpCloseHandles 发射三段"每只先判 0、非 0 才关闭"。返回值一概不检查：
// 无论关闭成败都走同一条收尾。
//
// en: httpCloseHandles emits the three "test each handle against 0, close only a real one"
// blocks. The reply is ignored: whether closing succeeds takes the same tail path either way.
func httpCloseHandles(b *rt) {
	a := b.a
	for _, name := range []string{"httpReq", "httpConn", "httpSess"} {
		skip := b.label()
		b.leaRIP(x64.RCX, name)
		a.MovLoad(x64.RCX, x64.RCX, 0)
		a.Test(x64.RCX, x64.RCX)
		a.Jcc(x64.E, skip)
		b.impCallFrom(winhttp, "WinHttpCloseHandle")
		b.leaRIP(x64.RDX, name)
		a.Xor(x64.RCX, x64.RCX)
		a.MovStore(x64.RDX, 0, x64.RCX)
		a.Mark(skip)
	}
}

// @http_begin(rcx=网址文本, rdx=要发送的字节块（GET 传 0）, r8=超时毫秒（0＝不设）)：
// 一次请求走到"响应已接收"为止，返回 nothing。三只句柄留在静态槽里供后面几段使用。
//
// 本例程是本组最长的一条，但没有一圈循环：识别家族、拆三段、打开会话、设置超时、连接、
// 打开请求、发送、接收响应，八步直线走完。两圈循环在 @http_headers 与 @http_body 里，
// 各带自己的那几个槽。
//
// en: @http_begin (rcx = url text, rdx = the byte block to send (0 for GET), r8 = timeout ms, 0
// meaning unset): runs one request up to "response received" and returns nothing. The three
// handles stay in the three static slots for the later stages. This is the longest routine of
// the group but has no loop at all: recognise the scheme, crack three segments, open the
// session, set timeouts, connect, open the request, send, receive - eight steps in a straight
// line. The two loops live in @http_headers and @http_body, each with its own few slots.
func httpBegin(b *rt) {
	const (
		sURL   = 0 // 网址文本
		sSend  = 1 // 要发送的字节块（GET＝0）
		sMS    = 2 // 超时毫秒
		sFlags = 3 // TLS 标志（0 或 flagSecure）
		sPort  = 4 // 连接的目标端口
		sSkip  = 5 // 前缀宽字符个数（"http://" 是 7，"https://" 是 8）
		sHost  = 6 // 主机段的起点
		sEnd   = 7 // 主机段的终点（遇到冒号、斜杠或结尾时停下的位置）
		sPathP = 8 // 路径段的起点（0＝网址未写路径，补一个 '/'）
	)
	a := b.a
	b.prologue()
	b.put(sURL, x64.RCX)
	b.put(sSend, x64.RDX)
	b.put(sMS, x64.R8)
	b.leaRIP(x64.RDX, "httpU")
	a.MovStore(x64.RDX, 0, x64.RCX) // 失败那一句要把网址拼进"解析不到 …"里
	// 先清理上一次可能遗留的：上一次若以抛出离开就没走到 @http_close，
	// 这三格里还留着已无人需要的句柄。各判一次 0，无遗留时即空转。
	httpCloseHandles(b)

	badURL := b.label()   // 网址本身即不合法：为空、拆不出主机、端口非法、超出缓冲
	lastFail := b.label() // 来自系统的失败：号码取自 GetLastError
	isHTTP := b.label()   // 第五个宽字符不是 's'：即 "http:" 分支
	isSet := b.label()    // 家族与前缀识别完成
	scan := b.label()     // 扫描主机段
	atColon := b.label()
	atSlash := b.label()
	atEnd := b.label()
	dig, digStop, digBad := b.label(), b.label(), b.label() // 端口的那串数字
	cut := b.label()
	portSet := b.label()
	hCut := b.label()                                   // 主机段拷贝完成，下面写入结尾那个 0
	pTop, pEnd, pGot := b.label(), b.label(), b.label() // 统计路径段长度
	pathDone := b.label()
	sessFail := b.label()
	timeout := b.label()
	connect := b.label()
	verbSet := b.label()
	sendIt := b.label()

	// 1) 网址转成宽字符。为空与超出 wPath 容量的都算"这个地址不行"，无需调用系统。
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, sURL)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badURL)

	// 2) 家族与前缀：识别 wPath 开头的宽字符——只接受 "http:" 与 "https:"，
	//    其余一律拒绝（ftp／gopher 本版没有实现）。
	//    大小写都识别：or 上 0x20 只会设置字母位，不影响宽字符的高字节与冒号。
	b.leaRIP(x64.RAX, "wPath")
	a.MovZX32(x64.RCX, x64.RAX, 0) // 前两个宽字符当一个数读：'h' 0 't' 0
	a.OrImm(x64.RCX, 0x00200020)
	a.CmpImm(x64.RCX, 0x00740068)
	a.Jcc(x64.NE, badURL)
	a.MovZX32(x64.RCX, x64.RAX, 4) // 第三、第四个：'t' 0 'p' 0
	a.OrImm(x64.RCX, 0x00200020)
	a.CmpImm(x64.RCX, 0x00700074)
	a.Jcc(x64.NE, badURL)
	a.MovZX16(x64.RDX, x64.RAX, 8) // 第五个：冒号即 http，'s' 后跟冒号即 https
	a.OrImm(x64.RDX, 0x20)
	a.CmpImm(x64.RDX, 0x73)
	a.Jcc(x64.NE, isHTTP)
	a.MovZX16(x64.RCX, x64.RAX, 10)
	a.CmpImm(x64.RCX, 0x3A)
	a.Jcc(x64.NE, badURL)
	a.MovImm(x64.RCX, flagSecure)
	b.put(sFlags, x64.RCX)
	a.MovImm(x64.RDX, 8) // "https://" 八个宽字符：主机从该位置起
	b.put(sSkip, x64.RDX)
	a.Jmp(isSet)
	a.Mark(isHTTP)
	a.CmpImm(x64.RDX, 0x3A) // 不是 's' 则必须是冒号
	a.Jcc(x64.NE, badURL)
	a.Xor(x64.RCX, x64.RCX)
	b.put(sFlags, x64.RCX)
	a.MovImm(x64.RDX, 7) // "http://" 七个宽字符：主机从该位置起
	b.put(sSkip, x64.RDX)
	a.Mark(isSet)

	// 3) 拆解网址：主机、端口、路径三段，扫描的就是上面那串宽字符。
	//    不调用 WinHttpCrackUrl：它要求 URL_COMPONENTS 整个结构体的字节布局，
	//    按 C 头文件自行布置一遍，布置错一处就是往别人的槽里写数据。这里只认自己算出的偏移。
	//    识别的写法只有 scheme://主机[:端口][/路径[?查询串]]——百分号转义、用户名密码本版不支持。
	b.leaRIP(x64.RAX, "wPath")
	b.get(x64.RSI, sSkip)
	a.ShlImm(x64.RSI, 1) // 一个宽字符两个字节
	a.Add(x64.RSI, x64.RAX)
	b.put(sHost, x64.RSI) // 主机段从这里起
	a.Mark(scan)
	a.MovZX16(x64.RCX, x64.RSI, 0)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, atEnd) // 结尾：没有端口，也没有路径
	a.CmpImm(x64.RCX, 0x3A)
	a.Jcc(x64.E, atColon)
	a.CmpImm(x64.RCX, 0x2F) // '/'
	a.Jcc(x64.E, atSlash)
	a.AddImm(x64.RSI, 2)
	a.Jmp(scan)
	a.Mark(atSlash)
	b.put(sEnd, x64.RSI) // 主机到该斜杠为止；路径也从该斜杠起（斜杠本身计入路径）
	b.put(sPathP, x64.RSI)
	a.Xor(x64.RAX, x64.RAX)
	b.put(sPort, x64.RAX)
	a.Jmp(cut)
	a.Mark(atEnd)
	b.put(sEnd, x64.RSI)
	a.Xor(x64.RAX, x64.RAX)
	b.put(sPort, x64.RAX)
	b.put(sPathP, x64.RAX) // 0＝网址未写路径，下面补一个 '/'
	a.Jmp(cut)
	a.Mark(atColon)
	b.put(sEnd, x64.RSI) // 主机到冒号为止
	a.AddImm(x64.RSI, 2)
	a.Mov(x64.RDI, x64.RSI) // 数字起点：没有任何一位即网址非法
	a.Xor(x64.RAX, x64.RAX) // 累加得到的端口号
	a.MovImm(x64.R8, 10)
	a.Mark(dig)
	a.MovZX16(x64.RCX, x64.RSI, 0)
	a.AddImm(x64.RCX, -0x30) // '0'：减去它才得到数值
	a.CmpImm(x64.RCX, 9)
	a.Jcc(x64.A, digStop) // 不是数字（结尾那个 0 也算）：该段到头
	a.Imul(x64.RAX, x64.R8)
	a.Add(x64.RAX, x64.RCX)
	a.AddImm(x64.RSI, 2)
	a.Jmp(dig)
	a.Mark(digStop)
	a.Cmp(x64.RSI, x64.RDI)
	a.Jcc(x64.E, digBad) // 冒号后没有任何数字
	a.CmpImm(x64.RAX, 65535)
	a.Jcc(x64.A, digBad)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, digBad) // 端口 0 不是可连接的号码
	b.put(sPort, x64.RAX)
	a.MovZX16(x64.RCX, x64.RSI, 0)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, cut) // 端口后直接是结尾：路径为空，下面补 '/'
	a.CmpImm(x64.RCX, 0x2F)
	a.Jcc(x64.NE, digBad) // 端口后既不是斜杠也不是结尾
	b.put(sPathP, x64.RSI)
	a.Jmp(cut)
	a.Mark(digBad)
	a.Jmp(badURL)

	a.Mark(cut)
	// 端口省略时按家族补默认值
	b.get(x64.RAX, sPort)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, portSet)
	b.get(x64.RAX, sFlags)
	a.Test(x64.RAX, x64.RAX)
	a.MovImm(x64.RAX, portHTTP)
	a.Jcc(x64.E, portSet)
	a.MovImm(x64.RAX, portHTTPS)
	a.Mark(portSet)
	b.put(sPort, x64.RAX)

	// 主机段搬进 httpHost：长度先算好，为空或装不下都报那一句。
	// 长度要算两次（搬之前一次、写入结尾 0 再一次）：copyBytes 把 rdx 与 rcx 都踩过了。
	b.get(x64.RAX, sEnd)
	b.get(x64.RCX, sHost)
	a.Mov(x64.RDX, x64.RAX)
	a.Sub(x64.RDX, x64.RCX)
	a.CmpImm(x64.RDX, 2)
	a.Jcc(x64.B, badURL) // 主机为空
	a.CmpImm(x64.RDX, 2*(httpHostCap-1))
	a.Jcc(x64.A, badURL)
	b.get(x64.RSI, sHost)
	b.leaRIP(x64.RDI, "httpHost")
	copyBytes(b)
	a.Jmp(hCut)
	a.Mark(hCut)
	b.get(x64.RAX, sEnd)
	b.get(x64.RCX, sHost)
	a.Sub(x64.RAX, x64.RCX) // 已搬入的字节数
	b.leaRIP(x64.RCX, "httpHost")
	a.Add(x64.RAX, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore8(x64.RAX, 0, x64.RCX) // 结尾 0：宽字符两个字节，逐字节抹
	a.MovStore8(x64.RAX, 1, x64.RCX)

	// 路径段搬进 httpPath；网址未写路径就补一个 '/'（有的服务器不接受空的对象名）。
	b.get(x64.RSI, sPathP)
	a.Test(x64.RSI, x64.RSI)
	a.Jcc(x64.NE, pTop)
	b.leaRIP(x64.RAX, "httpPath")
	a.MovImm(x64.RCX, 0x2F) // '/'
	a.MovStore8(x64.RAX, 0, x64.RCX)
	a.Xor(x64.RCX, x64.RCX)
	a.MovStore8(x64.RAX, 1, x64.RCX)
	a.Jmp(pathDone)
	a.Mark(pTop)
	a.Mov(x64.RCX, x64.RSI) // 一路走到结尾那个 0
	a.Mark(pEnd)
	a.MovZX16(x64.RAX, x64.RCX, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, pGot)
	a.AddImm(x64.RCX, 2)
	a.Jmp(pEnd)
	a.Mark(pGot)
	a.Mov(x64.RDX, x64.RCX) // 连同结尾那个 0 一起搬过去
	a.AddImm(x64.RDX, 2)
	a.Sub(x64.RDX, x64.RSI)
	a.CmpImm(x64.RDX, 2*httpPathCap)
	a.Jcc(x64.A, badURL)
	b.leaRIP(x64.RDI, "httpPath")
	copyBytes(b)
	a.Mark(pathDone)

	// 4) 打开会话。User-Agent 写 `nef`；代理沿用本机设置；同步模式（不设置异步标志）。
	b.leaRIP(x64.RCX, "httpAgent")
	a.MovImm(x64.RDX, accessDefaultProxy)
	a.Xor(x64.R8, x64.R8)
	a.Xor(x64.R9, x64.R9)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCallFrom(winhttp, "WinHttpOpen")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, sessFail)
	b.leaRIP(x64.RDX, "httpSess")
	a.MovStore(x64.RDX, 0, x64.RAX)
	a.Jmp(timeout)
	a.Mark(sessFail)
	a.MovImm(x64.RAX, errHttpInternal) // 会话打开失败：这是 winhttp 自身返回的号码
	httpFail(b)

	// 5) 超时：四个阶段（解析、连接、发送、接收）各设同一个毫秒数。该值为 0 就不调用——
	//    保留系统默认，与 `net.connect(…, 0)` 同一条"不设"。调用失败也不处理：
	//    该值只表示愿意等待的时长，设置不成功仍能完成一次请求。
	a.Mark(timeout)
	b.get(x64.RAX, sMS)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, connect)
	b.leaRIP(x64.RCX, "httpSess")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	a.Mov(x64.RDX, x64.RAX)
	a.Mov(x64.R8, x64.RAX)
	a.Mov(x64.R9, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCallFrom(winhttp, "WinHttpSetTimeouts")

	// 6) 连接目标：主机名缓冲区是上面填好的宽字符，自带结尾那个 0。
	a.Mark(connect)
	b.leaRIP(x64.RCX, "httpSess")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	b.leaRIP(x64.RDX, "httpHost")
	b.get(x64.R8, sPort)
	a.Xor(x64.R9, x64.R9)
	b.impCallFrom(winhttp, "WinHttpConnect")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lastFail)
	b.leaRIP(x64.RDX, "httpConn")
	a.MovStore(x64.RDX, 0, x64.RAX)

	// 7) 打开请求。传入字节块即 POST，否则 GET。
	//    版本、来源、接受类型三个参数都传 0：版本由系统选择（1.1），重定向与证书全部由 winhttp 处理。
	b.leaRIP(x64.RCX, "httpConn")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	b.leaRIP(x64.RDX, "httpGet")
	b.get(x64.RAX, sSend)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, verbSet)
	b.leaRIP(x64.RDX, "httpPost")
	a.Mark(verbSet)
	b.leaRIP(x64.R8, "httpPath")
	a.Xor(x64.R9, x64.R9)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	b.get(x64.RAX, sFlags)
	a.MovStore(x64.RBP, arg7, x64.RAX)
	b.impCallFrom(winhttp, "WinHttpOpenRequest")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lastFail)
	b.leaRIP(x64.RDX, "httpReq")
	a.MovStore(x64.RDX, 0, x64.RAX)

	// 8) 发送并等待响应。不追加任何请求头：`Host` 与 `Content-Length` 由系统补齐，
	//    发送的只有正文块（GET 时相应参数全部传 0）。
	b.get(x64.RAX, sSend)
	a.Mov(x64.R9, x64.RAX) // lpOptional＝块的正文；为 0 即什么都不发送
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8) // 附加的请求头：无
	a.Mov(x64.RAX, x64.RDX)
	a.MovStore(x64.RBP, arg5, x64.RAX) // 附加头部的字节数：0
	a.MovStore(x64.RBP, arg6, x64.RAX) // 发送总长度：传 0 由系统按实际长度计算
	a.MovStore(x64.RBP, arg7, x64.RAX) // dwContext：本版不向用户提供回调
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, sendIt)
	a.AddImm(x64.R9, BytesBody) // 数据位于块头之后
	b.get(x64.RAX, sSend)
	a.MovLoad(x64.RAX, x64.RAX, BytesLen)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	a.Mark(sendIt)
	b.leaRIP(x64.RCX, "httpReq")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	b.impCallFrom(winhttp, "WinHttpSendRequest")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lastFail)
	b.leaRIP(x64.RCX, "httpReq")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	a.Xor(x64.RDX, x64.RDX)
	b.impCallFrom(winhttp, "WinHttpReceiveResponse")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, lastFail)
	b.epilogue()

	a.Mark(badURL)
	a.MovImm(x64.RAX, errHttpInvalidU) // 此处未经系统调用，号码由本层给定（折算到 10104 那一句）
	httpFail(b)

	a.Mark(lastFail)
	b.impCall("GetLastError")
	httpFail(b)
}

// @http_status(rcx=那块 `Response`)：把状态码写入第一个槽（偏移 0），返回 nothing。
//
// `WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER` 直接返回数值，无需逐字符解析
// "200" 三个字符。缓冲区是静态槽，进入之前先把"大小"写为 4、把该槽清零（返回的是
// 四个字节，块头里剩下的四个字节必须为 0 才读得正确）。
//
// en: @http_status (rcx = that `Response` block): writes the status code into the first slot
// (offset 0) and returns nothing. WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER returns
// the number directly, so "200" never has to be decoded character by character. The buffer is a
// static slot: before entering, its size is set to 4 and the slot zeroed (four bytes come back,
// and the remaining four header bytes must read 0 for it to be right).
func httpStatus(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX) // 那块 Response
	b.leaRIP(x64.RAX, "httpDw")
	a.MovImm(x64.RDX, 4)
	a.MovStore(x64.RAX, 0, x64.RDX)
	b.leaRIP(x64.RAX, "httpStatus")
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RAX, 0, x64.RDX)
	b.leaRIP(x64.RCX, "httpReq")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	a.MovImm(x64.RDX, queryStatusCode|queryFlagNumber)
	a.Xor(x64.R8, x64.R8) // 查询的不是具名头部
	b.leaRIP(x64.R9, "httpStatus")
	b.leaRIP(x64.RAX, "httpDw")
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg6, x64.RAX) // WINHTTP_NO_HEADER_INDEX
	b.impCallFrom(winhttp, "WinHttpQueryHeaders")
	a.Test(x64.RAX, x64.RAX)
	asked := b.label()
	a.Jcc(x64.NE, asked)
	b.impCall("GetLastError")
	httpFail(b)
	a.Mark(asked)
	b.leaRIP(x64.RDX, "httpStatus")
	a.MovLoad(x64.RDX, x64.RDX, 0)
	b.get(x64.RAX, 0)
	a.MovStore(x64.RAX, 0, x64.RDX) // status 槽：字段表的第一格（internal/check/check.go 的 responseFields）
	b.epilogue()
}

// @http_headers(rcx=那块 `Response`)：查询整块原始头、转成 UTF-8、按 CRLF 拆成
// 一个 `list<string>`，写入第二个槽（偏移 8），返回 nothing。
//
// 第一行是状态行（`HTTP/1.1 200 OK` 那句），不收进列表：它表达的内容 `status` 槽已经给出。
// 其余每行原样作为一条 `"名字: 值"`，进一步拆分由用户自己调用 `split`——本层只保证
// "一行一条"。连续两个换行、以及末尾的空行都不收。
//
// 换行查找的是字节 13（CR）：头部区是 ASCII 范围，而 UTF-8 的续字节全都大于 0x80，
// 多字节字符内不会出现 13 或 10 的字节值，所以逐字节扫描不会切错
// （与大小写只识别 ASCII 同一条理由）。
//
// en: @http_headers (rcx = that `Response` block): asks for the whole raw header block,
// translates it to UTF-8, splits it on CRLF into a `list<string>` and writes it into the second
// slot (offset 8), returning nothing. The first line is the status line (the `HTTP/1.1 200 OK`
// sentence) and is not put in the list - the status slot already says what it says. Every
// remaining line goes in verbatim as one `"name: value"`, and splitting further is the program
// author's own `split` (this layer stops at "one entry per line"). A double line break and a
// trailing empty line are both skipped. The line break searched for is the byte 13 (CR): the
// header block is ASCII territory and every UTF-8 continuation byte is above 0x80, so no
// multi-byte character contains a 13 or 10 byte value and a byte-wise scan cannot cut in the
// wrong place - the same reasoning as case folding recognising ASCII only.
func httpHeaders(b *rt) {
	const (
		hOut  = 0 // 那块 Response
		hSz   = 1 // 整块原始头的字节数（含结尾那个 0）
		hRaw  = 2 // 堆上的宽字符缓冲
		hText = 3 // 转换出的 UTF-8 文本
		hList = 4 // 拆分得到的列表
		hN    = 5 // 文本的字节数
		hI    = 6 // 扫描位置
		hJ    = 7 // 当前行的起点
		hWide = 8 // wideToText 的入参：宽字符串的地址
		hChar = 9 // 以及：宽字符个数
	)
	a := b.a
	b.prologue()
	b.put(hOut, x64.RCX)
	// 文本槽先置 0：走"没有任何头部"分支时它从未被写入，
	// 下面读取字节数那一步依赖该槽为 0 才知道不必读取文本头。
	a.Xor(x64.RCX, x64.RCX)
	b.put(hText, x64.RCX)
	done := b.label()
	mkList := b.label() // "没有任何头部"分支落到这里：创建列表并写入段数
	noBuf := b.label()
	haveRaw := b.label() // 第二趟查询成功：尺寸以其写回的数值为准

	// 1) 第一趟查询尺寸：缓冲区传 NULL，系统把"需要几个字节"写进 httpDw。
	//    没有任何头部的答复返回"成功"，尺寸槽保持 0，下面按该分支处理。
	b.leaRIP(x64.RAX, "httpDw")
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RAX, 0, x64.RDX)
	b.leaRIP(x64.RCX, "httpReq")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	a.MovImm(x64.RDX, queryRawHeadersCRLF)
	a.Xor(x64.R8, x64.R8)
	a.Xor(x64.R9, x64.R9)
	b.leaRIP(x64.RAX, "httpDw")
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	b.impCallFrom(winhttp, "WinHttpQueryHeaders")
	b.leaRIP(x64.RAX, "httpDw")
	a.MovLoad(x64.RAX, x64.RAX, 0)
	b.put(hSz, x64.RAX)
	a.CmpImm(x64.RAX, 2) // 连结尾那个 0 都装不下＝没有任何头部
	a.Jcc(x64.B, mkList)

	// 2) 申请对应大小的缓冲，第二趟才真正写入。查询失败就先释放该缓冲再走共同收尾。
	a.Mov(x64.RCX, x64.RAX)
	a.Xor(x64.RDX, x64.RDX)
	b.call("@alloc")
	b.put(hRaw, x64.RAX)
	b.leaRIP(x64.RCX, "httpReq")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	a.MovImm(x64.RDX, queryRawHeadersCRLF)
	a.Xor(x64.R8, x64.R8)
	b.get(x64.R9, hRaw)
	b.leaRIP(x64.RAX, "httpDw")
	a.MovStore(x64.RBP, arg5, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	a.MovStore(x64.RBP, arg6, x64.RAX)
	b.impCallFrom(winhttp, "WinHttpQueryHeaders")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, noBuf)         // 失败：释放该缓冲，按 GetLastError 抛出
	b.leaRIP(x64.RAX, "httpDw") // 成功：该槽现在是实际写入的字节数（含结尾那个 0）
	a.MovLoad(x64.RAX, x64.RAX, 0)
	b.put(hSz, x64.RAX)
	a.Jmp(haveRaw)
	a.Mark(noBuf)
	b.get(x64.RCX, hRaw) // 该缓冲是本趟分配的，抛出前释放
	b.call("@free")
	b.impCall("GetLastError")
	httpFail(b)
	a.Mark(haveRaw)

	// 3) 转成 UTF-8：宽字符个数为"字节数折半再减去结尾那一个"。
	//    超过 httpHdrChars 只转换前面这一截（该上限的推导写在常数处）。
	b.get(x64.RAX, hSz)
	a.ShrImm(x64.RAX, 1)
	a.AddImm(x64.RAX, -1)
	haveChars := b.label()
	a.CmpImm(x64.RAX, httpHdrChars)
	a.Jcc(x64.BE, haveChars)
	a.MovImm(x64.RAX, httpHdrChars)
	a.Mark(haveChars)
	b.put(hChar, x64.RAX)
	b.get(x64.RAX, hRaw)
	b.put(hWide, x64.RAX)
	wideToText(b, hWide, hChar) // 返回 rax=新文本（落点是静态的 aBuf，生成后即成为对象）
	b.put(hText, x64.RAX)
	b.get(x64.RCX, hRaw) // 宽字符缓冲用完即释放：文本已另建一块
	b.call("@free")

	// 4) 列表先创建出来，再定段数。元素类型字与 file.list_dir、regular_expression.split 同一条规则。
	a.Mark(mkList)
	a.Xor(x64.RCX, x64.RCX)
	a.MovImm(x64.RDX, tagString)
	b.call("@list_new")
	b.put(hList, x64.RAX)
	b.get(x64.RAX, hText)
	a.Test(x64.RAX, x64.RAX)
	zeroN := b.label()
	haveN := b.label()
	a.Jcc(x64.E, zeroN)
	a.MovLoad(x64.RAX, x64.RAX, 0) // 文本的第一格就是它的字节数
	a.Jmp(haveN)
	a.Mark(zeroN)
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(haveN)
	b.put(hN, x64.RAX)

	// addLine 收入 [hJ, hI) 区间（非空才收）：切出来是一行，加入列表后归列表持有。
	addLine := func() {
		skip := b.label()
		b.get(x64.RAX, hI)
		b.get(x64.RDX, hJ)
		a.Cmp(x64.RAX, x64.RDX)
		a.Jcc(x64.BE, skip)
		b.get(x64.RCX, hText)
		b.get(x64.RDX, hJ)
		b.get(x64.R8, hI)
		a.Sub(x64.R8, x64.RDX)
		b.call("@str_sub")
		a.Mov(x64.RDX, x64.RAX)
		b.get(x64.RCX, hList)
		b.call("@list_add")
		a.Mark(skip)
	}

	// 5) 先跳过第一行（状态行）：扫到第一个 CR，行首移到其后两个字节。
	//    两个循环使用同一个字节判断，各设一次标号。
	a.Xor(x64.RAX, x64.RAX)
	b.put(hI, x64.RAX)
	sTop, sGo := b.label(), b.label()
	a.Mark(sTop)
	b.get(x64.RAX, hI)
	b.get(x64.RCX, hN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, done) // 连状态行都没有：整段为空
	b.get(x64.RDI, hText)
	a.AddImm(x64.RDI, strHead)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX8(x64.RCX, x64.RDI, 0) // 读一个字节需要零扩展：MovLoad8 只写低八位，高位留着的是其他值
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, done) // 遇到结尾那个 0：本圈之后没有下一行
	a.CmpImm(x64.RCX, crByte)
	a.Jcc(x64.E, sGo)
	a.AddImm(x64.RAX, 1)
	b.put(hI, x64.RAX)
	a.Jmp(sTop)
	a.Mark(sGo)
	a.AddImm(x64.RAX, 2) // 跳过 CR 与 LF
	b.put(hI, x64.RAX)
	b.put(hJ, x64.RAX)

	// 6) 逐行拆分：[hJ, hI) 是一行，遇到 CR 就收入这一段，行首移到 CR 后两个字节。
	xTop, xScan, xLast, xHit := b.label(), b.label(), b.label(), b.label()
	a.Mark(xTop)
	b.get(x64.RAX, hJ)
	b.put(hI, x64.RAX)
	a.Mark(xScan)
	b.get(x64.RAX, hI)
	b.get(x64.RCX, hN)
	a.Cmp(x64.RAX, x64.RCX)
	a.Jcc(x64.AE, xLast)
	b.get(x64.RDI, hText)
	a.AddImm(x64.RDI, strHead)
	a.Add(x64.RDI, x64.RAX)
	a.MovZX8(x64.RCX, x64.RDI, 0)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, xLast) // 遇到结尾那个 0：这一段就是最后一行（不带 CR 结尾的写法）
	a.CmpImm(x64.RCX, crByte)
	a.Jcc(x64.E, xHit)
	a.AddImm(x64.RAX, 1)
	b.put(hI, x64.RAX)
	a.Jmp(xScan)
	// 到达串尾：最后一行没有 CR 结尾（有的服务器不补末尾换行），按同一条规则收入
	a.Mark(xLast)
	addLine()
	a.Jmp(done)
	a.Mark(xHit)
	addLine()
	b.get(x64.RAX, hI) // 行首移到该 CR 后两个字节（addLine 踩过了寄存器，重新取）
	a.AddImm(x64.RAX, 2)
	b.put(hJ, x64.RAX)
	a.Jmp(xTop)

	// 7) 列表写入第二个槽。列表中每条都是 @str_sub 新建的块，源串由本层分配、由本层释放——
	//    与 text.split 同一套归属规则：切分完成后原串释放，切出的各条归列表持有。
	a.Mark(done)
	b.get(x64.RCX, hText)
	b.call("@free")
	b.get(x64.RAX, hList)
	b.get(x64.RCX, hOut)
	a.MovStore(x64.RCX, 8, x64.RAX) // headers 槽
	b.epilogue()
}

// @http_body(rcx=那块 `Response`)：一圈读取一段，读取完成后构造 bytes 块写入第三个槽（偏移 16），
// 返回 nothing。
//
// 不预设总长：`WinHttpReadData` 每次返回"本次读取的字节数"，返回 0 即该流已读完。
// 分块传输、无 Content-Length 的响应、以及正文大于一块缓冲这几种情形都走同一条路径。
// 累加缓冲不足时容量翻倍搬运一次——搬运的只是"已接收的字节"，因此不会越写入相邻块的块头。
//
// en: @http_body (rcx = that `Response` block): reads one chunk per round, builds a bytes block
// when done and writes it into the third slot (offset 16), returning nothing. Total length is
// never guessed: WinHttpReadData returns "how many bytes this round" and 0 means the stream is
// finished. Chunked transfer, a response without Content-Length, and a body larger than one
// buffer all take this same path. When the accumulating buffer is too small it doubles and
// moves once - what moves is only "the bytes received so far", so it never runs into a
// neighbouring header.
func httpBody(b *rt) {
	const (
		bOut   = 0 // 那块 Response
		bBuf   = 1 // 累加缓冲（堆上）
		bCap   = 2 // 它的容量
		bTotal = 3 // 已接收字节数
		bNew   = 4 // 翻倍时的新缓冲
		bBody  = 5 // 返回的 bytes 块
		bGot   = 6 // 本次读取的字节数
	)
	a := b.a
	b.prologue()
	b.put(bOut, x64.RCX)
	rTop, rDone, grow, fitted, readFail := b.label(), b.label(), b.label(), b.label(), b.label()

	// 初始档位先申请 8192 字节：绝大多数响应一次即可容纳，不必预先按正文大小估算。
	a.MovImm(x64.RCX, httpFirst)
	a.Xor(x64.RDX, x64.RDX)
	b.call("@alloc")
	b.put(bBuf, x64.RAX)
	a.MovImm(x64.RAX, httpFirst)
	b.put(bCap, x64.RAX)
	a.Xor(x64.RAX, x64.RAX)
	b.put(bTotal, x64.RAX)

	a.Mark(rTop)
	// 每趟最多读取 httpChunk 字节，落入静态缓冲：这样"容量是否足够"与"搬运"各自计算，
	// 也避免直接读入一块本身还要参与翻倍的缓冲——翻倍一搬运，已读到的内容就丢失了。
	b.leaRIP(x64.RCX, "httpReq")
	a.MovLoad(x64.RCX, x64.RCX, 0)
	b.leaRIP(x64.RDX, "httpChunkBuf")
	a.MovImm(x64.R8, httpChunk)
	b.leaRIP(x64.R9, "httpGot")
	b.impCallFrom(winhttp, "WinHttpReadData")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, readFail)
	b.leaRIP(x64.RAX, "httpGot")
	a.MovLoad(x64.RAX, x64.RAX, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, rDone) // 0 字节＝该流已读完
	b.put(bGot, x64.RAX)
	// 容量判断：已接收加上本次的，超过容量时要先更换缓冲
	b.get(x64.RDX, bTotal)
	a.Add(x64.RDX, x64.RAX)
	b.get(x64.RCX, bCap)
	a.Cmp(x64.RDX, x64.RCX)
	a.Jcc(x64.BE, fitted)
	// 新缓冲取"容量翻倍"与"本次累计值再加一趟余量"中的较大者——
	// 单趟就超过 8192 字节的响应，翻倍容量装不下，须按后者确定。
	a.ShlImm(x64.RCX, 1)
	a.Mov(x64.R8, x64.RCX)
	a.Mov(x64.RAX, x64.RDX)
	a.AddImm(x64.RAX, httpChunk)
	a.Cmp(x64.R8, x64.RAX)
	a.Jcc(x64.AE, grow) // 翻倍容量已足够：不必按后者抬高
	a.Mov(x64.R8, x64.RAX)
	a.Mark(grow)
	b.put(bCap, x64.R8) // 新容量先写入槽：下面 @alloc 与 @free 都会踩 r8
	a.Mov(x64.RCX, x64.R8)
	a.Xor(x64.RDX, x64.RDX)
	b.call("@alloc")
	b.put(bNew, x64.RAX)
	b.get(x64.RDI, bNew) // 已接收的字节搬运到新缓冲
	b.get(x64.RSI, bBuf)
	b.get(x64.RDX, bTotal)
	copyBytes(b)
	b.get(x64.RCX, bBuf)
	b.call("@free")
	b.get(x64.RAX, bNew)
	b.put(bBuf, x64.RAX)
	a.Mark(fitted)
	// 把本次收到的字节追加到累加缓冲末尾
	b.get(x64.RDI, bBuf)
	b.get(x64.RAX, bTotal)
	a.Add(x64.RDI, x64.RAX)
	b.leaRIP(x64.RSI, "httpChunkBuf")
	b.get(x64.RDX, bGot)
	copyBytes(b)
	b.get(x64.RAX, bTotal) // 已接收字节数：加上本次
	b.get(x64.RDX, bGot)
	a.Add(x64.RAX, x64.RDX)
	b.put(bTotal, x64.RAX)
	a.Jmp(rTop)

	a.Mark(rDone)
	// 构造 bytes 块，把累加缓冲的内容整体搬入，然后释放缓冲。
	b.get(x64.RCX, bTotal)
	b.call("@bytes_new")
	b.put(bBody, x64.RAX)
	b.get(x64.RDI, bBody)
	a.AddImm(x64.RDI, BytesBody)
	b.get(x64.RSI, bBuf)
	b.get(x64.RDX, bTotal)
	copyBytes(b)
	b.get(x64.RCX, bBuf)
	b.call("@free")
	b.get(x64.RAX, bBody)
	b.get(x64.RCX, bOut)
	a.MovStore(x64.RCX, 16, x64.RAX) // body 槽
	b.epilogue()

	a.Mark(readFail)
	b.get(x64.RCX, bBuf) // 缓冲由本例程分配：抛出前释放
	b.call("@free")
	b.impCall("GetLastError")
	httpFail(b)
}

// @http_close()：依次关闭本次请求的三只句柄，返回 nothing。
//
// 这三只不属于语言层面的句柄（`Socket`／`Listener` 才在名字作废处关闭）：
// 它们是 winhttp 内部的会话、连接与请求，一次 `http.get` 结束即回收，用户不持有其名字。
// 网址槽随之抹 0：下一次请求会写入自己的新文本。
//
// en: @http_close(): closes the three handles of this request one by one and returns nothing.
// These three are not the language's handle class (only Socket/Listener are closed where the
// name is voided): they are winhttp's session, connection and request, to be collected when
// one http.get finishes, and the user never holds a name for them. The url slot is zeroed along
// with them - the next request will put its own text there.
func httpClose(b *rt) {
	a := b.a
	b.prologue()
	httpCloseHandles(b)
	b.leaRIP(x64.RCX, "httpU")
	a.Xor(x64.RDX, x64.RDX)
	a.MovStore(x64.RCX, 0, x64.RDX)
	b.epilogue()
}

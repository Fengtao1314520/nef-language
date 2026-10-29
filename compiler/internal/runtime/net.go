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

// net.go 实现 `net` 组：TCP 客户端与服务器。
//
// 三个事实决定这一层的形状：
//
//   - **一切都要经由操作系统**。winsock 位于 `ws2_32.dll`，初始化步骤（`@winsock_start`）
//     已发在 `nef.main` 的第一条指令上，所以这里每一条例程都能直接使用套接字。
//   - **没有线程**。`recv` 只能原地等待，所以超时是这一层的必需项：连接用"非阻塞 ＋ select"，
//     收与发用 `SO_RCVTIMEO`／`SO_SNDTIMEO`。`accept` 不提供超时——该成员的定义就是
//     "等待一个连接进来"，"等多久就走"需要另行定义一条规则。
//   - **句柄是一类新资源**。它不是堆上的一块，所以不进空闲链表、不进容器、不装箱：
//     名字到达作用域尽头时由 `@net_close` 调用一次 `closesocket`（资源归属账目中多出的一类）。
//
// 错误答复只占六个号（10100～10105），消息里一律带上系统给的原号码：winsock 有
// 一千多个码，逐条赋号会超出文档与站上速查表的容量，一个不给又只能让用户猜测具体原因。
//
// 这一层共用同一条寄存器纪律：每一次 call 之后都视为 rax／rcx／rdx／r8～r11 已被踩过，
// 要保留的值一律写入帧上的槽（`b.put`／`b.get`），使用时再取回——与 file.go 开头的理由相同。
// 静态缓冲（netHints／netRes／netFd／…）只被本组使用，用户无法命名：没有线程，
// 同一时刻只有一件事在运行，与 `wPath`／`aWide` 同一个道理。
//
// en: net.go implements the `net` group: TCP client and server. Three facts decide this
// layer's shape. It can only go through the operating system: winsock lives in ws2_32.dll and
// the start-up step (@winsock_start) is already emitted as the first instruction of nef.main,
// so every routine here may use a socket directly. There are no threads: recv can only wait in
// place, so a timeout is a required part of this layer - connecting uses non-blocking plus
// select, receiving and sending use SO_RCVTIMEO/SO_SNDTIMEO. accept gets no timeout: that
// member is by definition "wait for a connection to arrive", and "wait a while then leave"
// would need its own rule. A handle is a new class of resource: it is not a heap block, so it
// enters neither the free list nor a container nor a box; when the name's scope ends,
// @net_close calls closesocket once (one more class in the resource-ownership ledger). Error
// answers take only six codes (10100-10105), with the system's own number always included in
// the message: winsock has over a thousand codes, giving each one a code would overflow the
// documentation and the site's lookup table, and giving none would leave the user guessing.
// The layer shares one register discipline: after every call assume rax/rcx/rdx/r8-r11 were
// clobbered, keep anything needed in frame slots (b.put/b.get) and reload it when used - the
// same reason stated at the top of file.go. The static buffers (netHints/netRes/netFd/...)
// are used only by this group and no user can name them: with no threads exactly one thing
// runs at a time, the same reasoning as wPath/aWide.
package runtime

import "neflang.org/nf/internal/x64"

// winsock 侧的常数按 Microsoft 公开的值写死，不引用头文件：产物不依赖本机安装了什么。
//
// en: winsock's constants are hardcoded at Microsoft's published values; no header file is
// imported, so nothing depends on what is installed on this machine.
const (
	afInet      = 2 // 自行布置 sockaddr_in 时使用（listen 只绑定 IPv4 这一族）
	afUnspec    = 0 // 给 getaddrinfo 的提示：家族不限，v4 与 v6 都列出，逐个尝试
	sockStream  = 1
	ipProtoTCP  = 6
	sockAnyType = 0 // 提示里协议填 0 表示"由 socket 类型决定"，本版填 TCP，不使用它

	invalidSocket = int64(-1) // socket()／accept() 返回 INVALID_SOCKET 即失败
	solSocket     = 0xffff
	soError       = 0x1004 // getsockopt 查询"该连接是否已建立"
	soRcvTimeo    = 0x1006 // 接收方向的等待时长（一个 DWORD，毫秒）
	soSndTimeo    = 0x1007 // 发送方向的等待时长

	fionBio = int64(0x4004667e) // ioctlsocket：设置／撤销非阻塞

	wsaWouldblock   = int64(10035) // 非阻塞 connect 的"仍在连接"状态，不是错误
	wsaTimedOut     = int64(10060)
	wsaRefused      = int64(10061)
	wsaReset        = int64(10054)
	wsaAbort        = int64(10053)
	wsaNotConn      = int64(10057)
	wsaNotSock      = int64(10038)
	wsaHostDown     = int64(10064)
	wsaHostUnreach  = int64(10065)
	wsaNetDown      = int64(10066)
	wsaNetUnreach   = int64(10051)
	wsaAddrInUse    = int64(10048)
	wsaAddrNotAvail = int64(10049)
	wsaInvalidArg   = int64(10022)
	wsaNotInit      = int64(10093)
	wsaProtoType    = int64(10046)
	wsaAccess       = int64(10013)
	wsaNoHost       = int64(11001)
	wsaNoHost2      = int64(11004)
	wsaBadFlags     = int64(11010)

	addrInfoSize  = 48 // Win64 的 ADDRINFOW：四个 int ＋ size_t ＋ 三个指针
	aiFamilyOff   = 4
	aiTypeOff     = 8
	aiProtoOff    = 12
	aiAddrLenOff  = 16
	aiAddrOff     = 32
	aiNextOff     = 40
	sockAddrSize  = int64(16)         // sockaddr_in
	listenBacklog = int64(0x7fffffff) // SOMAXCONN：队列长度由系统决定
)

// netData 定义本组的静态数据与那几句报错文案。
//
// en: netData lays out this group's static data and its error sentences.
func netData(put func(name string, bs []byte)) {
	put("netHints", make([]byte, addrInfoSize)) // getaddrinfo 的提示：只有前 16 个字节会被写
	put("netRes", make([]byte, 8))              // 结果链表的首项
	put("netOne", make([]byte, 8))              // ioctlsocket 的那个 u_long（0 或 1）
	put("netMs", make([]byte, 8))               // setsockopt 的那个 DWORD（毫秒）
	put("netLen4", make([]byte, 8))             // getsockopt 的"数值大小"：始终写 4
	put("netOpt", make([]byte, 8))              // getsockopt 返回的整数
	put("netFd", make([]byte, 16))              // fd_set：一个计数 ＋ 一个句柄
	put("netTv", make([]byte, 8))               // timeval：秒 ＋ 微秒
	put("netAddr", make([]byte, sockAddrSize))  // 自行布置的 sockaddr_in（listen 用）
	put("sNetRefuse", strObject("连不上这个地址"))
	put("sNetTimeout", strObject("等回应等超时了"))
	put("sNetParse", strObject("解析不到 "))
	put("sNetParseTail", strObject(" 这个地址"))
	put("sNetAddr", strObject("这个地址或端口不行"))
	put("sNetDead", strObject("这一条连接不能用了"))
	put("sNetAsk", strObject("网络这一处没办成"))
	put("sNetNum", strObject("（系统的号码 "))
	put("sNetClose", strObject("）"))
}

// netCodeOf 是错误号码映射表：入参 rcx＝系统号码，返回 rax＝10100 段里的号码、
// rdx＝对应句子的地址、r8＝1 表示该条要把名字拼进句子中间（只有解析失败那一条需要）。
//
// 这张表与站上 tools/ 的码表是同一份，改一处要同步改另一处。
// 两套系统各占一段：winsock 的十九行是 `net` 组会遇到的（号码取自 WSAGetLastError），
// winhttp 的八行是 `http` 组会遇到的（号码取自 GetLastError）；两段的号码不重叠，
// 一张表即可走完。都未命中时落到 10100 兜底：不新增号码，但系统原号码照原样带进消息。
//
// en: netCodeOf is the error-number table: rcx in = the system number, rax out = the code in
// the 10100 band, rdx = the address of the message sentence, r8 = 1 meaning this one splices
// "that name" into the middle of the sentence (only the unresolvable-address case does). This
// table and the site's tools/ code table are the same document, so a change here must be made
// in the other one as well. The two systems each get a run of rows: winsock's nineteen are
// what the net group meets (numbers asked of WSAGetLastError), winhttp's eight are what the
// http group meets (asked of GetLastError); the two bands' codes do not overlap, so one table
// walks both. Nothing matching falls back to 10100: no new code is invented, but the system's
// number is carried in the message.
func netCodeOf(b *rt) {
	a := b.a
	type row struct {
		n    int64
		code int64
		msg  string
		tail int64
	}
	rows := []row{
		// winsock（ws2_32.dll）那一段。
		{wsaRefused, 10101, "sNetRefuse", 0},
		{wsaHostUnreach, 10101, "sNetRefuse", 0},
		{wsaNetUnreach, 10101, "sNetRefuse", 0},
		{wsaHostDown, 10101, "sNetRefuse", 0},
		{wsaNetDown, 10101, "sNetRefuse", 0},
		{wsaTimedOut, 10102, "sNetTimeout", 0},
		{wsaNoHost, 10103, "sNetParse", 1},
		{wsaNoHost2, 10103, "sNetParse", 1},
		{wsaBadFlags, 10103, "sNetParse", 1},
		{wsaAddrInUse, 10104, "sNetAddr", 0},
		{wsaAddrNotAvail, 10104, "sNetAddr", 0},
		{wsaInvalidArg, 10104, "sNetAddr", 0},
		{wsaAccess, 10104, "sNetAddr", 0},
		{wsaProtoType, 10104, "sNetAddr", 0},
		{wsaNotInit, 10104, "sNetAddr", 0},
		{wsaReset, 10105, "sNetDead", 0},
		{wsaAbort, 10105, "sNetDead", 0},
		{wsaNotConn, 10105, "sNetDead", 0},
		{wsaNotSock, 10105, "sNetDead", 0},
		// winhttp（winhttp.dll）段：12000 起的那一串，这里折算八行。
		// 未折算的（家族不支持 12006、内存不足 12001 那一类）落 10100 兜底，
		// 消息里照样带系统原号码——本组自身只产生 12005 与 GetLastError 返回的号码。
		{errHttpTimeout, 10102, "sNetTimeout", 0},
		{errHttpNoName, 10103, "sNetParse", 1},
		{errHttpInvalidU, 10104, "sNetAddr", 0},
		{errHttpCannotConnect, 10101, "sNetRefuse", 0},
		{errHttpSecure, 10101, "sNetRefuse", 0},
		{errHttpBadResponse, 10105, "sNetDead", 0},
		{errHttpConnError, 10105, "sNetDead", 0},
		{errHttpInternal, 10100, "sNetAsk", 0},
	}
	hits := make([]x64.Label, len(rows))
	for i, r := range rows {
		hits[i] = b.label()
		a.CmpImm(x64.RCX, int32(r.n))
		a.Jcc(x64.E, hits[i])
	}
	// 一路比下来都没中：兜底那一句
	a.MovImm(x64.RAX, 10100)
	b.leaRIP(x64.RDX, "sNetAsk")
	a.MovImm(x64.R8, 0)
	end := b.label()
	a.Jmp(end)
	for i, r := range rows {
		a.Mark(hits[i])
		a.MovImm(x64.RAX, r.code)
		b.leaRIP(x64.RDX, r.msg)
		a.MovImm(x64.R8, r.tail)
		a.Jmp(end)
	}
	a.Mark(end)
}

// netThrow 是各处失败共同的收尾：rcx＝系统号码、rdx＝要拼进句子的文本（无则 0），
// 调用 @net_fail 后不返回任何值。写成一行是因为该收尾在 connect 路径上会出现多次。
//
// en: netThrow is the shared tail of every failure: rcx = the system number, rdx = the text to
// splice into the sentence (0 for none); it calls @net_fail and nothing comes back. It is one
// line because this code is reached several times along the connect path.
func netThrow(b *rt) {
	b.call("@net_fail")
}

// @net_fail(rcx=系统号码（0＝没有系统号码，只有那一句）, rdx=要拼进句子的文本，没有就传 0)：
// 拼出那句话、@throw，从不返回。
//
// 消息的形状是「那一句 ＋（系统的号码 N）」，10103 那一条是「解析不到 那个名字 这个地址（系统的号码 N）」。
// 拼串只用 @str_cat 与 @int_str：每拼一次构造新的一块、不改动两侧，所以旧的左半随即 @free。
// 静态池那几条块头上尺寸是 0，@free 走到即空操作，因此"左半是写死的那一句"与
// "左半是刚拼出来的一块"走同一条收尾，不必分两种情况。
//
// en: @net_fail (rcx = system number, 0 meaning "no number, just the sentence", rdx = text to
// splice in, else 0): assembles the sentence, calls @throw, and never returns. The message
// shape is "the sentence + (system number N)", and 10103 reads "cannot resolve that name as an
// address (system number N)". Assembly uses only @str_cat and @int_str: each join builds a new
// block and leaves both sides alone, so the older left half is @free'd immediately. Entries in
// the static pool carry size 0 in their header, making @free a no-op there, so "the left half
// is a hardcoded sentence" and "the left half is a freshly joined block" take the same tail
// with no special case.
func netFail(b *rt) {
	const (
		fN    = 0 // 系统号码
		fMid  = 1 // 拼在句中的文本（可为空）；拼接号码的几步借用它存放"号码写成的文本"
		fCode = 2 // 段号
		fTail = 3 // 是否拼入那个文本
		fText = 4 // 已拼接的部分
		fPart = 5 // 本次新拼出的块
	)
	a := b.a
	b.prologue()
	b.put(fN, x64.RCX)
	b.put(fMid, x64.RDX)

	b.get(x64.RCX, fN)
	netCodeOf(b) // rax=段号, rdx=那一句, r8=要不要夹
	b.put(fCode, x64.RAX)
	b.put(fTail, x64.R8)
	b.put(fText, x64.RDX)

	// 已拼部分 ＋ piece → 换成新拼的那一块，旧的当场释放。
	// 静态池那几条（「解析不到 」、两个括号）块头上尺寸是 0，@free 走到即空操作，
	// 所以"手里是写死的那一句"与"手里是刚拼出来的一块"走同一条收尾。
	cat := func(piece func()) {
		b.get(x64.RCX, fText)
		piece()
		b.call("@str_cat")
		b.put(fPart, x64.RAX)
		b.get(x64.RCX, fText)
		b.call("@free")
		b.get(x64.RAX, fPart)
		b.put(fText, x64.RAX)
	}
	from := func(name string) func() { return func() { b.leaRIP(x64.RDX, name) } }

	// 「解析不到 <名字> 这个地址」：只有该条把名字拼在句子中间
	skipParse := b.label()
	b.get(x64.RAX, fTail)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, skipParse)
	b.get(x64.RAX, fMid)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, skipParse) // 名字为空：句子按原样使用，不拼接空文本
	cat(func() { b.get(x64.RDX, fMid) })
	cat(from("sNetParseTail"))
	a.Mark(skipParse)

	// 「（系统的号码 N）」：号码为 0 就不追加任何内容（0＝该处不是系统返回的号码）
	noNum := b.label()
	b.get(x64.RCX, fN)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, noNum)
	cat(from("sNetNum"))
	b.get(x64.RCX, fN)
	b.call("@int_str")
	b.put(fMid, x64.RAX)
	cat(func() { b.get(x64.RDX, fMid) })
	b.get(x64.RCX, fMid) // 数字串由 @int_str 新建，拼进句子后即释放
	b.call("@free")
	cat(from("sNetClose"))
	a.Mark(noNum)

	b.get(x64.RCX, fCode)
	b.get(x64.RDX, fText)
	b.call("@throw")
	b.epilogue()
}

// portInto 把帧槽 `slot` 里的端口号写入 `dst` 所指地址的第 2、3 个字节——
// `sockaddr_in` 与 `sockaddr_in6` 的端口都在那个位置（v6 的头两字节是家族与长度，端口紧随其后）。
//
// 网络字节顺序是**大端**：高字节位于低地址。本机是小端，所以不能把该数值原样写入，
// 须先拆成两个字节按逆序摆放（等价于一次 htons）。
//
// en: portInto places the port number from frame slot `slot` into bytes 2 and 3 of the address
// at `dst` - both sockaddr_in and sockaddr_in6 keep the port there (in v6 the first two bytes
// are family and length, with the port right after). Network byte order is BIG ENDIAN: the high
// byte at the low address. This machine is little endian, so the number cannot be written as
// is; it is split into two bytes placed in reverse - equivalent to one htons.
func portInto(b *rt, dst int, slot int) {
	a := b.a
	b.get(x64.RAX, slot)
	a.Mov(x64.RDX, x64.RAX)
	a.ShrImm(x64.RDX, 8)
	a.MovStore8(dst, 2, x64.RDX)
	a.MovStore8(dst, 3, x64.RAX)
}

// signExt 把 rax 低 32 位的 int 扩展成 64 位带符号的数。
//
// winsock 返回 int 的那几条（send／recv／select／bind／…）只用得到低 32 位，高 32 位按约定为 0：
// −1 不扩展就成了 4,294,967,295，而 `test` 检查的是第 63 位——"失败"会被读成"正常"，
// 打印出来的结果是 `4294967295`，错误号一个也不报。左移 32 再算术右移 32，
// 就是把符号位扩展到高 32 位。
//
// en: signExt spreads the low 32-bit int in rax into a signed 64-bit value. The winsock
// routines that return int (send/recv/select/bind/...) only use the low 32 bits and leave the
// high half zero by convention: an unspread -1 becomes 4,294,967,295, and since `test` looks at
// bit 63, "something went wrong" is read as "nothing wrong", the printed answer is 4294967295
// and no error code is reported. Shifting left 32 and arithmetically right 32 moves the sign
// bit up.
func signExt(a *x64.Asm) {
	a.ShlImm(x64.RAX, 32)
	a.SarImm(x64.RAX, 32)
}

// @net_connect(rcx=地址文本（名字与 IP 的写法都接受）, rdx=端口, r8=超时毫秒（0＝不设）)：
// 返回 rax=套接字句柄。
//
// `getaddrinfo` 只有四个参数、不接受超时，所以那个毫秒数只覆盖连接与收发，
// 解析耗时由系统的解析器决定——这一点要写进面向用户的文档，避免误以为超时管三个阶段。
//
// 结果表逐条目尝试：每个条目按其自身家族创建套接字、写入端口、连接一次；失败就关闭
// 该套接字、记录系统号码、移到表的下一个条目。整张表走完才报错：本版不会只试第一个
// 条目就报"连不上"。
//
// 端口写入地址的第 2、3 个字节（v4 与 v6 位置相同），按网络字节序逆序写入：见 `portInto`。
//
// en: @net_connect (rcx = address text, accepting both a name and an IP, rdx = port, r8 =
// timeout ms, 0 meaning unset): returns rax = the socket handle. getaddrinfo takes only four
// parameters and accepts no timeout, so that millisecond figure covers connecting and
// sending/receiving only - how long resolution takes is up to the system's resolver. This has
// to be in the user-facing documentation so nobody assumes one timeout governs three phases.
// The returned address table is tried entry by entry: each entry builds a socket of its own
// family, sets the port, and tries once; on failure that socket is closed, the system number
// recorded, and the next entry tried. The sentence is only said after the whole table ran, so
// this version never tries one entry and reports "cannot connect". The port goes into bytes 2
// and 3 of the address (v4 and v6 alike) in network order, reversed - see portInto.
func netConnect(b *rt) {
	const (
		cHost = 0
		cPort = 1
		cMs   = 2
		cCur  = 3 // 当前走到结果表的哪一个条目
		cS    = 4 // 本候选的句柄
		cAddr = 5
		cLen  = 6
		cLast = 7 // 一路试下来最后记录的系统号码
		cErr  = 8
	)
	a := b.a
	b.prologue()
	b.put(cHost, x64.RCX)
	b.put(cPort, x64.RDX)
	b.put(cMs, x64.R8)

	badPort := b.label()  // 端口不在 1～65535，或地址文本为空／超出缓冲
	noParse := b.label()  // 该名字解析失败
	top := b.label()      // 表的下一个条目
	advance := b.label()  // 本条目失败：关闭句柄、移到下一个条目
	none := b.label()     // 整张表走完
	good := b.label()     // 连接成功
	blocking := b.label() // 未设超时：阻塞连接一次
	would := b.label()    // 非阻塞分支：仍在连接
	gotReady := b.label() // select 报告可写
	timedOut := b.label() // select 报告超时
	selErr := b.label()   // select 调用失败

	// 端口先在本层校验，无需调用系统
	b.get(x64.RAX, cPort)
	a.CmpImm(x64.RAX, 0)
	a.Jcc(x64.LE, badPort)
	a.CmpImm(x64.RAX, 65535)
	a.Jcc(x64.A, badPort)

	// 地址文本转成宽字符：为空与超出容量都算"这个地址不行"（尚未调用解析器）
	b.leaRIP(x64.RDX, "wPath")
	b.get(x64.RCX, cHost)
	b.call("@to_wide")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, badPort)

	// 提示：家族不限、流式、TCP。那 48 个字节里本组只填头 16 个（四个 int），
	// 后面 addrlen／canonname／addr／next 一律留 0——静态数据初始就是 0，系统只读不写。
	// 两次各写 8 个字节，窗口不重叠，免得后一笔覆盖前一笔刚写入的那个 int。
	b.leaRIP(x64.RDI, "netHints")
	a.MovImmMem(x64.RDI, 0, 0) // ai_flags 与 ai_family：都不限（AF_UNSPEC 就是 0）
	a.MovImm(x64.RAX, int64(ipProtoTCP)<<32|sockStream)
	a.MovStore(x64.RDI, 8, x64.RAX) // ai_socktype 与 ai_protocol
	b.leaRIP(x64.RCX, "wPath")
	a.Xor(x64.RDX, x64.RDX)
	b.leaRIP(x64.R8, "netHints")
	b.leaRIP(x64.R9, "netRes")
	b.impCallFrom(ws2, "GetAddrInfoW")
	// `getaddrinfo` 把结果直接放在返回值里（不走 WSAGetLastError）：0 才是成功。
	// 那一句「解析不到 <名字> 这个地址」只在 11001／11004／11010 三个号码上说，
	// 其余号码落 10100 兜底，系统原号码一律带进消息。
	a.Mov(x64.RCX, x64.RAX)
	b.put(cLast, x64.RCX)
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NE, noParse)

	a.MovImm(x64.RAX, 0)
	b.put(cLast, x64.RAX)
	b.leaRIP(x64.RAX, "netRes")
	a.MovLoad(x64.RAX, x64.RAX, 0)
	b.put(cCur, x64.RAX)

	a.Mark(top)
	b.get(x64.RAX, cCur)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, none)

	// 在该条目创建套接字之前 cS 必须为 0：`advance` 步骤无条件先关闭，
	// 关闭 0 是空操作（见 @net_close 开头的说明），不会把上一个条目的句柄关闭第二次。
	a.Xor(x64.RCX, x64.RCX)
	b.put(cS, x64.RCX)

	// socket(该条目自身的家族, 该条目自身的类型, 该条目自身的协议)
	b.get(x64.RSI, cCur)
	a.MovZX32(x64.RCX, x64.RSI, aiFamilyOff)
	a.MovZX32(x64.RDX, x64.RSI, aiTypeOff)
	a.MovZX32(x64.R8, x64.RSI, aiProtoOff)
	b.impCallFrom(ws2, "socket")
	a.MovImm(x64.R8, invalidSocket)
	a.Cmp(x64.RAX, x64.R8)
	haveSock := b.label()
	a.Jcc(x64.NE, haveSock)
	b.impCallFrom(ws2, "WSAGetLastError") // 创建失败：记录号码，本条目作废
	b.put(cLast, x64.RAX)
	a.Jmp(advance)
	a.Mark(haveSock)
	b.put(cS, x64.RAX)

	// 本条目要连接的目标：地址、长度，并写入端口
	b.get(x64.RAX, cCur)
	a.MovLoad(x64.RAX, x64.RAX, aiAddrOff)
	b.put(cAddr, x64.RAX)
	b.get(x64.RAX, cCur)
	a.MovZX32(x64.RDX, x64.RAX, aiAddrLenOff)
	b.put(cLen, x64.RDX)
	b.get(x64.RSI, cAddr)
	portInto(b, x64.RSI, cPort)

	b.get(x64.RAX, cMs)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, blocking)

	// 设置了超时：先置非阻塞，连接一次，未连上再判断是否仍在连接
	a.MovImm(x64.RAX, 1)
	b.leaRIP(x64.RDX, "netOne")
	a.MovStore(x64.RDX, 0, x64.RAX)
	b.get(x64.RCX, cS)
	a.MovImm(x64.RDX, fionBio)
	b.leaRIP(x64.R8, "netOne")
	b.impCallFrom(ws2, "ioctlsocket")
	b.get(x64.RCX, cS)
	b.get(x64.RDX, cAddr)
	b.get(x64.R8, cLen)
	b.impCallFrom(ws2, "connect")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, good)
	b.impCallFrom(ws2, "WSAGetLastError")
	a.Mov(x64.RCX, x64.RAX)
	b.put(cLast, x64.RCX) // 先记录当前返回的号码：若之后走到"整张表走完"，依据的就是它
	a.MovImm(x64.RDX, wsaWouldblock)
	a.Cmp(x64.RCX, x64.RDX)
	a.Jcc(x64.NE, advance) // 不是"仍在连接"：本条目作废，移到下一个条目

	// 仍在连接：select 只监视"该句柄是否可写"，时限就是那个毫秒数
	a.Mark(would)
	b.get(x64.RAX, cMs)
	a.MovImm(x64.RCX, 1000)
	a.Xor(x64.RDX, x64.RDX)
	a.Div1(x64.RCX) // rax＝秒，rdx＝剩余的毫秒
	b.leaRIP(x64.RDI, "netTv")
	a.MovStore32(x64.RDI, 0, x64.RAX)
	a.MovImm(x64.RCX, 1000)
	a.Imul(x64.RDX, x64.RCX)
	a.MovStore32(x64.RDI, 4, x64.RDX)
	b.leaRIP(x64.RDI, "netFd")
	a.MovImm(x64.RAX, 1)
	a.MovStore32(x64.RDI, 0, x64.RAX)
	b.get(x64.RAX, cS)
	a.MovStore(x64.RDI, 8, x64.RAX)
	a.Xor(x64.RCX, x64.RCX)
	a.Xor(x64.RDX, x64.RDX)
	b.leaRIP(x64.R8, "netFd")
	a.Xor(x64.R9, x64.R9)
	b.leaRIP(x64.RAX, "netTv")
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCallFrom(ws2, "select")
	signExt(a) // int 扩展成 64 位后才能区分"可写"／"超时"／"select 调用失败"
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.G, gotReady)
	a.Jcc(x64.E, timedOut) // 超时
	a.Jmp(selErr)          // 负数：select 调用失败

	// 超时：这就是超时（10102 那一句），不必再尝试其他家族
	a.Mark(timedOut)
	b.get(x64.RCX, cS)
	b.impCallFrom(ws2, "closesocket")
	a.MovImm(x64.RCX, wsaTimedOut)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)

	a.Mark(gotReady)
	// 可写不等于连接成功：SO_ERROR 中的值才是该连接的结果
	b.leaRIP(x64.RAX, "netLen4")
	a.MovImm(x64.RDX, 4)
	a.MovStore(x64.RAX, 0, x64.RDX)
	a.Xor(x64.RDX, x64.RDX)
	b.leaRIP(x64.RAX, "netOpt")
	a.MovStore(x64.RAX, 0, x64.RDX)
	b.get(x64.RCX, cS)
	a.MovImm(x64.RDX, solSocket)
	a.MovImm(x64.R8, soError)
	b.leaRIP(x64.R9, "netOpt")
	b.leaRIP(x64.RAX, "netLen4")
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCallFrom(ws2, "getsockopt")
	a.Test(x64.RAX, x64.RAX)
	asked := b.label()
	a.Jcc(x64.E, asked)
	b.impCallFrom(ws2, "WSAGetLastError") // 查询本身失败：该号码就是本条目的结果
	b.put(cLast, x64.RAX)
	a.Jmp(advance)
	a.Mark(asked)
	// 查询成功：返回的整数（0＝已连接）才是该连接的实际结果
	b.leaRIP(x64.RAX, "netOpt")
	a.MovLoad(x64.RAX, x64.RAX, 0)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, good)
	b.put(cLast, x64.RAX) // 非零：它说明连接失败的原因，记录后移到下一个条目
	a.Jmp(advance)

	// 未设超时：阻塞连接一次，成功即走收尾
	a.Mark(blocking)
	b.get(x64.RCX, cS)
	b.get(x64.RDX, cAddr)
	b.get(x64.R8, cLen)
	b.impCallFrom(ws2, "connect")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, good)
	b.impCallFrom(ws2, "WSAGetLastError")
	b.put(cLast, x64.RAX)
	a.Jmp(advance)

	// 连接成功：恢复阻塞模式（select 分支曾将其关闭），再设置收发两个超时
	a.Mark(good)
	b.get(x64.RAX, cMs)
	a.Test(x64.RAX, x64.RAX)
	noTimeo := b.label()
	a.Jcc(x64.E, noTimeo)
	a.Xor(x64.RDX, x64.RDX)
	b.leaRIP(x64.RAX, "netOne")
	a.MovStore(x64.RAX, 0, x64.RDX)
	b.get(x64.RCX, cS)
	a.MovImm(x64.RDX, fionBio)
	b.leaRIP(x64.R8, "netOne")
	b.impCallFrom(ws2, "ioctlsocket")
	b.leaRIP(x64.RAX, "netMs")
	b.get(x64.RDX, cMs)
	a.MovStore(x64.RAX, 0, x64.RDX)
	b.get(x64.RCX, cS)
	a.MovImm(x64.RDX, solSocket)
	a.MovImm(x64.R8, soRcvTimeo)
	b.leaRIP(x64.R9, "netMs")
	a.MovImm(x64.RAX, 4)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCallFrom(ws2, "setsockopt")
	b.get(x64.RCX, cS)
	a.MovImm(x64.RDX, solSocket)
	a.MovImm(x64.R8, soSndTimeo)
	b.leaRIP(x64.R9, "netMs")
	a.MovImm(x64.RAX, 4)
	a.MovStore(x64.RBP, arg5, x64.RAX)
	b.impCallFrom(ws2, "setsockopt")
	a.Mark(noTimeo)
	b.leaRIP(x64.RAX, "netRes")
	a.MovLoad(x64.RCX, x64.RAX, 0)
	b.impCallFrom(ws2, "FreeAddrInfoW")
	b.get(x64.RAX, cS)
	b.epilogue()

	// 本候选失败：号码已记入 cLast（每个失败点各自写入），
	// 这里只关闭句柄、把游标前进一个条目、回到表顶——closesocket 调用之后再问 WSAGetLastError 就得不到原因了。
	a.Mark(advance)
	b.get(x64.RAX, cCur)
	a.MovLoad(x64.RAX, x64.RAX, aiNextOff)
	b.put(cCur, x64.RAX)
	b.get(x64.RAX, cS)
	a.Mov(x64.RCX, x64.RAX)
	b.impCallFrom(ws2, "closesocket")
	a.Xor(x64.RCX, x64.RCX)
	b.put(cS, x64.RCX)
	a.Jmp(top)

	// 表走完：以最后一个记录的号码为准（一个条目都没试过时为 0，走兜底句）
	a.Mark(none)
	b.leaRIP(x64.RAX, "netRes")
	a.MovLoad(x64.RCX, x64.RAX, 0)
	b.impCallFrom(ws2, "FreeAddrInfoW")
	b.get(x64.RCX, cLast)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)

	// select 失败：其号码即结果
	a.Mark(selErr)
	b.impCallFrom(ws2, "WSAGetLastError")
	b.put(cErr, x64.RAX)
	b.get(x64.RCX, cS)
	b.impCallFrom(ws2, "closesocket")
	b.get(x64.RCX, cErr)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)

	a.Mark(badPort)
	a.MovImm(x64.RCX, wsaInvalidArg)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)

	// 解析步骤的结果就是那个号码（上面已存入 cLast）；把名字拼进句子的处理交给 netFail
	a.Mark(noParse)
	b.get(x64.RCX, cLast)
	b.get(x64.RDX, cHost)
	netThrow(b)
}

// @net_listen(rcx=端口)：返回 rax=监听句柄。
//
// 绑定 0.0.0.0（INADDR_ANY）：一种形态即可。backlog 取 SOMAXCONN，队列长度由系统决定。
// 不设置 SO_REUSEADDR：端口被占用时必须能报出来（系统号码 10048 折算到 10104）。
//
// en: @net_listen (rcx = port): returns rax = the listener handle. It binds to 0.0.0.0
// (INADDR_ANY) - one shape is enough. The backlog is SOMAXCONN, so the system decides how many
// may queue. SO_REUSEADDR is not set: a port already taken must be reportable (that system
// error 10048 folds into 10104).
func netListen(b *rt) {
	const (
		lPort = 0
		lS    = 1
		lErr  = 2 // 失败步骤的号码：关闭句柄前先记录，closesocket 会覆盖"上一个错误"
	)
	a := b.a
	b.prologue()
	b.put(lPort, x64.RCX)

	badPort := b.label()
	bad := b.label()
	bound := b.label()
	ready := b.label()
	a.Xor(x64.RCX, x64.RCX)
	b.put(lS, x64.RCX) // 句柄尚未创建时跳到 bad 也不会误关其他资源（关闭 0 是空操作）

	b.get(x64.RAX, lPort)
	a.CmpImm(x64.RAX, 0)
	a.Jcc(x64.LE, badPort)
	a.CmpImm(x64.RAX, 65535)
	a.Jcc(x64.A, badPort)

	a.MovImm(x64.RCX, afInet)
	a.MovImm(x64.RDX, sockStream)
	a.MovImm(x64.R8, ipProtoTCP)
	b.impCallFrom(ws2, "socket")
	a.MovImm(x64.R8, invalidSocket)
	a.Cmp(x64.RAX, x64.R8)
	have := b.label()
	a.Jcc(x64.NE, have)
	b.impCallFrom(ws2, "WSAGetLastError")
	b.put(lErr, x64.RAX)
	a.Jmp(bad)
	a.Mark(have)
	b.put(lS, x64.RAX)

	// sockaddr_in 的 16 字节：家族 2、端口由 portInto 写入（网络字节序）、地址 0＝接收本机所有网卡的连接
	b.leaRIP(x64.RDI, "netAddr")
	a.MovImmMem(x64.RDI, 0, afInet) // 低两字节是家族，2～7 一并清 0（端口与地址的前一段）
	a.MovImmMem(x64.RDI, 8, 0)      // 地址的剩余四字节与两个填充字节
	portInto(b, x64.RDI, lPort)

	b.get(x64.RCX, lS)
	b.leaRIP(x64.RDX, "netAddr")
	a.MovImm(x64.R8, sockAddrSize)
	b.impCallFrom(ws2, "bind")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, bound)
	b.impCallFrom(ws2, "WSAGetLastError") // 端口被占用还是本机禁止绑定，由号码区分
	b.put(lErr, x64.RAX)
	a.Jmp(bad)
	a.Mark(bound)
	b.get(x64.RCX, lS)
	a.MovImm(x64.RDX, listenBacklog)
	b.impCallFrom(ws2, "listen")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.E, ready)
	b.impCallFrom(ws2, "WSAGetLastError")
	b.put(lErr, x64.RAX)
	a.Jmp(bad)
	a.Mark(ready)
	b.get(x64.RAX, lS)
	b.epilogue()

	// 关闭本例程创建的句柄，然后把失败步骤记录的号码抛出
	a.Mark(bad)
	b.get(x64.RCX, lS)
	b.impCallFrom(ws2, "closesocket")
	b.get(x64.RCX, lErr)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)

	// 端口不在 1～65535：此处未经系统调用，号码由本层给定（WSAEINVAL 那个号码）
	a.Mark(badPort)
	a.MovImm(x64.RCX, wsaInvalidArg)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)
}

// @net_accept(rcx=监听句柄)：返回 rax=新进入那一条连接的套接字。
//
// 不带超时：该成员的定义就是"等待一个连接"。不返回对端地址：语言里还没有
// "一个地址"这一档，查询是谁连进来是 `1.0` 之后再谈的一条。
//
// en: @net_accept (rcx = listener): returns rax = the socket of the connection that just
// arrived. No timeout: this member means "wait for a connection". The peer's address is not
// returned: the language has no "an address" tier yet, and asking who connected is a topic for
// after 1.0.
func netAccept(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	bad := b.label()
	b.get(x64.RCX, 0)
	a.Xor(x64.RDX, x64.RDX)
	a.Xor(x64.R8, x64.R8)
	b.impCallFrom(ws2, "accept")
	a.MovImm(x64.R8, invalidSocket)
	a.Cmp(x64.RAX, x64.R8)
	a.Jcc(x64.E, bad)
	b.epilogue()
	a.Mark(bad)
	b.impCallFrom(ws2, "WSAGetLastError")
	a.Mov(x64.RCX, x64.RAX)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)
}

// netIO 是 send 与 recv 共用的部分：句柄、字节块、要搬运的个数，第四个参数是标志（传 0）。
//
// 返回值小于 0 表示该连接出了问题，走 @net_fail；recv 返回 0 是"对端已关闭"，
// 那是结果不是错误（要区分"已关闭"与"暂无数据"需要另行定义一条规则，本版不定义）。
//
// en: netIO is the shared body of send and recv: the handle, the byte block, how many to move,
// and a fourth parameter for flags (passed 0). A return below 0 means this connection broke and
// takes @net_fail; recv returning 0 means "the peer closed", which is an answer rather than an
// error (separating "closed" from "nothing yet" would need its own rule, and this version does
// not set one).
func netIO(b *rt, fn string) {
	const (
		hS    = 0
		hB    = 1
		hN    = 2
		hBack = 3
	)
	a := b.a
	b.prologue()
	b.put(hS, x64.RCX)
	b.put(hB, x64.RDX)
	bad := b.label()
	b.get(x64.RAX, hB)
	a.MovLoad(x64.RAX, x64.RAX, BytesLen) // 块有多大就搬运多少个
	b.put(hN, x64.RAX)
	b.get(x64.RCX, hS)
	b.get(x64.RDX, hB)
	a.AddImm(x64.RDX, BytesBody)
	b.get(x64.R8, hN)
	a.Xor(x64.R9, x64.R9)
	b.impCallFrom(ws2, fn)
	signExt(a) // send／recv 返回 int：那个 −1 不扩展就成了"搬运了 42 亿个字节"
	b.put(hBack, x64.RAX)
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.S, bad)
	b.get(x64.RAX, hBack)
	b.epilogue()
	a.Mark(bad)
	b.impCallFrom(ws2, "WSAGetLastError")
	a.Mov(x64.RCX, x64.RAX)
	a.Xor(x64.RDX, x64.RDX)
	netThrow(b)
}

// @net_send(rcx=套接字, rdx=bytes)：返回 rax=写出去的字节数。
//
// en: @net_send (rcx = socket, rdx = bytes): returns rax = how many bytes were written.
func netSend(b *rt) { netIO(b, "send") }

// @net_recv_into(rcx=套接字, rdx=bytes)：把收到的字节就地填入该块，返回 rax=个数。
//
// 就地填充是该档存在的理由之一：单次接收的字节数不可控，
// 每收一片都要重建一个八倍大的容器就没法用。
//
// en: @net_recv_into (rcx = socket, rdx = bytes): fills what arrives in place into that block
// and returns rax = how many. Filling in place is one of the reasons that tier exists: how
// much arrives per read is not controllable, and rebuilding a container eight times the size
// for every chunk would be unusable.
func netRecvInto(b *rt) { netIO(b, "recv") }

// @net_close(rcx=句柄)：调用一次 closesocket。
//
// 0 表示"手上没有句柄"（名字作废处发出的就是这一号，槽里可能存着 0），不做处理。
// 提前关闭那条路（`s.close()`）由降级侧在发出这一号之后把槽抹成 0，所以同一句柄不会被关两次。
//
// en: @net_close (rcx = handle): calls closesocket once. 0 means "no handle in hand" (that is
// what is emitted where a name is voided, and the slot may hold 0) and does nothing. The
// early-close path (`s.close()`) has lowering zero the slot after emitting this code, so one
// handle can never be closed twice.
func netClose(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	done := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, done)
	b.impCallFrom(ws2, "closesocket")
	a.Mark(done)
	b.epilogue()
}

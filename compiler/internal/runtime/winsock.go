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

// winsock.go 负责 winsock 层的初始化与收尾；网络层实际引入的第二个 DLL 就是它。
//
// 系统侧要求：winsock 的任何成员在使用之前，进程须先调用一次 WSAStartup，
// 结束时对应调用一次 WSACleanup。产物没有"启动时先执行一步"的位置，所以 WSAStartup 发在
// `nef.main` 的第一条指令上（internal/lower/lower.go 那一侧），WSACleanup 发在 `@exit` 里——
// 三条退出路径（正常走完、`exit`、未被捕获的抛出）都经过这一个出口。
//
// en: winsock.go covers the start-up and clean-up of the winsock layer, the second DLL the
// network layer brings in. The system requires one WSAStartup call before any winsock member
// is used, with a matching WSACleanup at the end. The produced binary has no start-up slot,
// so WSAStartup is emitted as the first instruction of `nef.main` (on the
// internal/lower/lower.go side) and WSACleanup inside `@exit`; all three exit paths (normal
// completion, `exit`, and an uncaught throw) leave through that single exit.

package runtime

import (
	"neflang.org/nf/internal/x64"
)

const (
	// wantVer 是请求的 winsock 版本：2.2。该版本号在 Windows 95 起即已提供，
	// 返回的 WSADATA 各版本形状一致，所以只把它当作"初始化完成"的记号。
	wantVer = 0x0202
	// wsaDataSize 是 WSADATA 结构体的大小（400 字节）。其中的内容本版一概不读取：
	// 只需要确认初始化已完成；版本号由本层写死，不取系统返回值。
	wsaDataSize = 400
)

// winsockData 定义那块 400 字节的 WSADATA。它与 hstdout 等槽位于同一段静态数据，
// 用户无法命名，只有 @winsock_start 向其写入。
//
// en: winsockData lays out the 400-byte WSADATA block. It shares the static area with slots
// such as hstdout, no user can name it, and only @winsock_start writes into it.
func winsockData(put func(name string, bs []byte)) {
	put("wsadata", make([]byte, wsaDataSize))
}

// @winsock_start()：初始化步骤。不检查返回值——该调用在所有 Windows 上都会成功；
// 即便失败，后续每条网络成员会自行失败并报 10100 段的号码。
//
// en: @winsock_start(): the start-up step. Its return value is ignored - this call succeeds on
// every Windows, and were it to fail, each network member would fail on its own and report a
// code from the 10100 band.
func winsockStart(b *rt) {
	a := b.a
	b.prologue()
	a.MovImm(x64.RCX, wantVer)
	b.leaRIP(x64.RDX, "wsadata")
	b.impCallFrom(ws2, "WSAStartup")
	b.epilogue()
}

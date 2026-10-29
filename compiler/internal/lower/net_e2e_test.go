// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"bufio"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"neflang.org/nf/internal/exetest"
)

// net_e2e_test.go 覆盖 `net` 这一组成员：TCP 的客户端与服务器。
//
// 端口一律现场取得（`freePort`：让系统选一个未占用的，读出后立即归还系统），固定端口会与其他机器上的进程冲突。
// 对端要么是同一个 nf 程序自己（自己 listen、自己 connect，回环的 backlog 收得下连接，
// 没有线程也走得通），要么是本测试进程里用 Go 启动的回声服务——所有用例都不出本机。
// 访问外网的测试一条都没有：那类需要当日数据落盘并设离线开关才可靠，这里要验证的只是这一层自身是否连通。

// freePort 取得一个空闲的本机端口：开一次监听、读出端口号、立即关闭。
//
// 从关闭到 nf 真正绑定之间，别的进程可能抢占这个端口——这是这套实现唯一已知的时序缺口，
// 表现是对应的测试报 10104（地址或端口不可用），重跑可以恢复。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("拿一个空端口都拿不到：%v", err)
	}
	n := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("还端口没成：%v", err)
	}
	return n
}

// echoOnce 起一台只服务一个连接的回声服务器：收一段、原样送回去、关掉。
// 返回的是它 listen 的那个端口，和一个收尾用的函数。
func echoOnce(t *testing.T) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("回声服务起不来：%v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4096)
		for {
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := c.Read(buf)
			if n > 0 {
				if _, werr := c.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil || n == 0 {
				return
			}
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() {
		_ = l.Close()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
		}
	}
}

// echoServe 启动一台持续服务的回声服务器：每来一条连接就把收到的字节原样送回，
// 直到调用者执行 stop 关闭。`echoOnce` 只服务一条连接；每圈增长表的用例要跑几千圈，用的是这一台。
func echoServe(t *testing.T) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("回声服务起不来：%v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := l.Accept()
			if err != nil {
				return // stop 已关闭监听句柄：这个错误就是这台服务器的收尾信号
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
					n, err := c.Read(buf)
					if n > 0 {
						if _, werr := c.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil || n == 0 {
						return
					}
				}
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() {
		_ = l.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// TestNetSelfServe 一个程序里自己 listen、自己 connect、accept，两个方向各送一趟。
//
// 这条测试覆盖这一层的完整路径：句柄能创建、能连上、能收发、slice 出的字节片段照样能发送；
// 同时"名字作废处自动关闭"没有提前关掉仍在使用的句柄（监听句柄与两个套接字句柄在同一个块里，各有各的生命期）。
func TestNetSelfServe(t *testing.T) {
	p := freePort(t)
	got, code := run(t, `
let port:int = `+strconv.Itoa(p)+`
let srv:Listener = net.listen(port)
let cli:Socket = net.connect("127.0.0.1", port, 3000)
let acc:Socket = srv.accept()
let msg:bytes = "ping".to_bytes()
console.print(cli.send(msg))
let got:bytes = bytes.new(16)
let n:int = acc.recv_into(got)
console.print(n, text.from_bytes(got.slice(0, n)))
console.print(acc.send(got.slice(0, n)))
let back:bytes = bytes.new(16)
console.print(cli.recv_into(back))
console.print(text.from_bytes(back.slice(0, 4)))
console.print(cli, srv)
`)
	want := "4\n4 ping\n4\n4\nping\nSocket Listener\n"
	if got != want || code != 0 {
		t.Fatalf("自己跟自己走不通：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestNetToGoServer 覆盖 nf 作客户端、Go 作服务端：文本、二进制、以及"发送字节数"这个返回值。
func TestNetToGoServer(t *testing.T) {
	port, stop := echoOnce(t)
	defer stop()
	got, code := run(t, `
let cli:Socket = net.connect("127.0.0.1", `+strconv.Itoa(port)+`, 3000)
let msg:bytes = "甲a".to_bytes()
console.print(cli.send(msg))
let back:bytes = bytes.new(8)
let n:int = cli.recv_into(back)
console.print(n, text.from_bytes(back.slice(0, n)))
cli.close()
`)
	want := "4\n4 甲a\n"
	if got != want || code != 0 {
		t.Fatalf("客户端这一头不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestNetServeGoClient 覆盖反向：nf 是服务器，Go 一侧连进来。
//
// 启动的是真实产物（`nf build` 输出的那一份），因为它要在等待 accept 时被另一个进程连接。
// 端口经命令行参数传入：程序自己打印一行 ready，测试读到该行才发起连接——避免连得过早。
func TestNetServeGoClient(t *testing.T) {
	port := freePort(t)
	src := `
let port:int = text.to_int(args[0])
let srv:Listener = net.listen(port)
console.print("ready")
let c:Socket = srv.accept()
let buf:bytes = bytes.new(32)
let n:int = c.recv_into(buf)
c.send(buf.slice(0, n))
console.print(n)
`
	path := exetest.Write(t, build(t, src), "nf-server.exe")
	cmd := exec.Command(path, strconv.Itoa(port))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("接不上标准输出：%v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("服务器起不来：%v", err)
	}
	lines := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	// 任何失败都先杀掉子进程再报错，避免服务器进程遗留在测试进程之外。
	fail := func(f string, args ...any) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf(f, args...)
	}
	var ready string
	select {
	case ready = <-lines:
	case <-time.After(15 * time.Second):
		fail("等不到服务器那一句 ready")
	}
	if strings.TrimSpace(ready) != "ready" {
		fail("第一行要的是 ready，得到 %q", ready)
	}
	c, cerr := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if cerr != nil {
		fail("连不上自己起的服务器：%v", cerr)
	}
	if _, werr := c.Write([]byte("hi there")); werr != nil {
		_ = c.Close()
		fail("送不出去：%v", werr)
	}
	echo := make([]byte, 16)
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	n, rerr := c.Read(echo)
	_ = c.Close()
	if rerr != nil || n == 0 {
		fail("回声没读回来：%v（%q）", rerr, echo[:n])
	}
	if string(echo[:n]) != "hi there" {
		fail("回声不对：%q", string(echo[:n]))
	}
	var count string
	select {
	case count = <-lines:
	case <-time.After(15 * time.Second):
		fail("服务器没报它收到几个字节")
	}
	if count != "8" {
		fail("服务器自己报的字节数不对：%q", count)
	}
	if err := cmd.Wait(); err != nil {
		fail("服务器收尾不干净：%v", err)
	}
}

// TestNetCodes 验证 10100 段四个错误码各在哪条路径上抛出。
// 系统原始错误号一律带在消息里，所以这里比对"错误号＋句式"，不断言系统那一部分的内容。
func TestNetCodes(t *testing.T) {
	dead := freePort(t) // 无监听者的端口：连接被拒绝
	cases := []struct {
		name, src, want string
	}{
		{"端口没人听", `let s:Socket = net.connect("127.0.0.1", ` + strconv.Itoa(dead) + `, 2000)`,
			"出错了（10101）：连不上这个地址"},
		{"名字解析不到", `let s:Socket = net.connect("nef-没有这个域名.invalid", 80, 2000)`,
			"出错了（10103）：解析不到 nef-没有这个域名.invalid 这个地址"},
		{"端口是 0", `let l:Listener = net.listen(0)`, "出错了（10104）：这个地址或端口不行"},
		{"端口越界", `let s:Socket = net.connect("127.0.0.1", 70000, 2000)`, "出错了（10104）：这个地址或端口不行"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, code := run(t, c.src+"\n")
			if code != 1 || !strings.Contains(got, c.want) {
				t.Fatalf("得到 %q 码 %d，要含 %q", got, code, c.want)
			}
			if !strings.Contains(got, "（系统的号码 ") {
				t.Fatalf("消息里要带上系统给的原号码：%q", got)
			}
		})
	}
}

// TestNetRecvTimeout 验证超时覆盖发送与接收（`getaddrinfo` 不支持超时，管不了解析阶段）：
// 服务端接受连接但不发送任何数据，`recv_into` 应在 `net.connect` 给定的毫秒数处抛出 10102。
func TestNetRecvTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起不来：%v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	hang := make(chan struct{})
	go func() {
		defer close(hang)
		c, err := l.Accept()
		if err != nil {
			return
		}
		// 不读不写，等对端自己超时；八秒后关闭，避免测试进程遗留一个永不返回的连接
		time.Sleep(8 * time.Second)
		_ = c.Close()
	}()
	got, code := run(t, `
let s:Socket = net.connect("127.0.0.1", `+strconv.Itoa(port)+`, 400)
let buf:bytes = bytes.new(8)
console.print(s.recv_into(buf))
`)
	if code != 1 || !strings.Contains(got, "出错了（10102）：等回应等超时了") {
		t.Fatalf("超时那一条不对：\n got %q 码 %d", got, code)
	}
	<-hang
}

// TestListenerScopeEndFreesPort 验证"名字到作用域尽头自动关闭"确实发出：在被调函数里创建监听句柄，
// 函数一返回同一个端口就应被释放——外面第二个句柄能绑定，说明那里已经执行过 `closesocket`。
//
// 选监听句柄而不是套接字，是因为它判定明确：端口没关就绑不上（10104，系统错误号 10048），
// 不需要等进程退出，也不依赖时序。`TestNetConnectLoopReleases` 只覆盖"一千圈不崩溃"——
// 即使删掉 `releaseOne`（`internal/lower/lower.go`）里的那次 `@net_close` 调用，它仍然通过，
// 所以为那次关闭提供凭据的是这一条。
func TestListenerScopeEndFreesPort(t *testing.T) {
	p := freePort(t)
	got, code := run(t, `
let port:int = `+strconv.Itoa(p)+`
method hold(p:int) {
    let first:Listener = net.listen(p)
}
hold(port)
let second:Listener = net.listen(port)
console.print("又绑得上")
`)
	want := "又绑得上\n"
	if got != want || code != 0 {
		t.Fatalf("里层那只没在名字作废处关掉：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestNetClosedHandleIsVoid 验证"关闭后再用即作废"：`close()` 执行完把那个位置抹成 0，
// 那个名字再访问成员就是 10092 号，而不是第二次调用系统关闭，也不是拿旧句柄去接收。
func TestNetClosedHandleIsVoid(t *testing.T) {
	port, stop := echoOnce(t)
	defer stop()
	got, code := run(t, `
let s:Socket = net.connect("127.0.0.1", `+strconv.Itoa(port)+`, 3000)
s.close()
let buf:bytes = bytes.new(4)
console.print(s.recv_into(buf))
`)
	if code != 1 || !strings.Contains(got, "出错了（10092）：对着空句柄取成员或调用方法") {
		t.Fatalf("关过再用要按空句柄那一条说：%q 码 %d", got, code)
	}
}

// TestNetConnectLoopReleases 验证"反复创建句柄"这条路径可用：一千圈各连接一次、
// 每圈到名字尽头自动关闭，全程不崩溃。
//
// 它无法证明"那次关闭确实发出"——即使删掉 `releaseOne`（`internal/lower/lower.go`）里的
// `@net_close` 调用，这一千圈仍然通过，因为漏掉的是对端一侧的临时端口，系统并不阻止第二次连接。
// 那次关闭由 `TestListenerScopeEndFreesPort` 单独提供凭据。
//
// 圈数不能继续增加：在本机上"对端先关闭"的这种连接方式会把临时端口占用一段时间，
// 几千圈会命中 10048（地址或端口不可用）——那是系统的端口表限制，不是内存归属问题。
// 每圈多出多少字节的问题由 `TestHeapSlopeHandles` 覆盖：五千圈各连接一次，量的正是字节数。
func TestNetConnectLoopReleases(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起不来：%v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			c, aerr := l.Accept()
			if aerr != nil {
				return
			}
			_ = c.Close() // 一来就关：对面每一圈都是"连上、马上关"
		}
	}()
	got, code := run(t, `
let total:int = 0
for (i:int in 0 until 1000) {
    let s:Socket = net.connect("127.0.0.1", `+strconv.Itoa(port)+`, 3000)
    total = total + 1
}
console.print(total)
`)
	close(stop)
	if got != "1000\n" || code != 0 {
		t.Fatalf("四千圈各连一次不对：\n got %q 码 %d", got, code)
	}
}

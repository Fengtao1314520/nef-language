// 回显服务器：在本机监听端口，把收到的字节原样发回，对方关闭连接后退出。
//
// 运行方式：先启动它，再用一个客户端连过去（另一个终端窗口，或 samples 之外自己写的客户端）：
//
//	nf samples/echo.nf 9000
//
// 端口来自命令行参数（`args`）；写死端口会和同机上占用该端口的程序冲突。
// 没有线程，一次只服务一个连接：接受一个连接、回传若干次、关闭连接，程序随后退出。
// 要持续服务，就把 `accept` 到收尾那一段放进一个 `while` 循环。
// en: An echo server on the local machine: it listens on a port, sends back the bytes it receives, and exits when the peer closes. Start it, then connect a client from another terminal window.

let port:int = text.to_int(args[0])
let door:Listener = net.listen(port)
console.print("在等连接:", port)

let one:Socket = door.accept()
console.print("连进来一个")

let box:bytes = bytes.new(256)
let n:int = one.recv_into(box)
while (n > 0) {
    one.send(box.slice(0, n))
    n = one.recv_into(box)
}

one.close()
console.print("对方关了，收摊")

// 不启动浏览器，自己 listen、自己 connect 一对套接字，验这三件：
// 一大块字节送出去、对面收回来一模一样；一条消息被切成两段送过来时能拼接回来；
// 对面发 ping（操作码 9）时回一条 pong（操作码 10）。
//
// 遮码的四个字节由 math.set_seed 固定，所以同一份程序跑两次，送出去的字节完全一致。
//
// 跑法（在仓库根）：./nf.exe webtest/selftest/socket_check.nf

import wt from "libs/web_test"

// 对面那一只套接字：读到的字节先攒起来，需要多少取多少，取完把已用掉的前段切掉。
class Peer {
    sock:Socket
    kept:bytes

    method pull() {
        let block:bytes = bytes.new(8192)
        let n:int = this.sock.recv_into(block)
        if (n <= 0) {
            throw (202, "对面先关了这条连接")
        }
        let old:bytes = this.kept
        this.kept = bytes.join([old, block.slice(0, n)])
    }

    // 收到够 size 个字节为止，返回整 size 个字节（多收的部分留在缓冲区里）
    method read_up_to(size:int):bytes {
        while (length(this.kept) < size) {
            this.pull()
        }
        return this.kept.slice(0, size)
    }

    method drop(size:int) {
        let all:bytes = this.kept
        this.kept = all.slice(size, length(all))
    }

    method send_raw(body:bytes) {
        this.sock.send(body)
    }
}

class Door {
    listener:Listener
    port:int
}

// 端口从 45000 起，被占用（10104）就加一再试，最多一百个。
// 语言里没有"绑任意端口再问得到几号"那条，所以端口号由调用方记着。
method open_door():Door {
    let out:Door = new Door()
    let p:int = 45000
    let done:boolean = false
    while (!done) {
        try {
            out.listener = net.listen(p)
            done = true
        } catch (code, msg) {
            if (code != 10104) {
                throw (code, msg)
            }
            p = p + 1
            if (p > 45100) {
                exit (1, "从 45000 起一百个端口都被占着")
            }
        }
    }
    out.port = p
    return out
}

method from_numbers(xs:list<int>):bytes {
    let out:bytes = bytes.new(length(xs))
    for (i:int in 0 until length(xs)) {
        out[i] = xs[i] as byte
    }
    return out
}

method check_int(title:string, got:int, want:int):boolean {
    if (got != want) {
        console.print("不对  " + title, "给", got, "要", want)
        return false
    }
    console.print("过  " + title)
    return true
}

method check_true(title:string, got:boolean):boolean {
    if (!got) {
        console.print("不对  " + title, "要真，给的是假")
        return false
    }
    console.print("过  " + title)
    return true
}

let bad:int = 0
math.set_seed(20261021)

let door:Door = open_door()
let client:Socket = net.connect("127.0.0.1", door.port, 5000)
let peer_sock:Socket = door.listener.accept()
let peer:Peer = new Peer(sock: peer_sock, kept: bytes.new(0))
let b:wt.Browser = wt.wrap_socket(client)
console.print("自己连自己，端口", door.port)

// 一、两万个字节压成一帧送出去，对面收回来再拆开，正文还是那一段
let body_text:string = "x".repeat(20000)
let frame:bytes = wt.build_frame(body_text, from_numbers([1, 2, 3, 4]))
let frame_size:int = length(frame)
b.send_all_bytes(frame)
let got_back:bytes = peer.read_up_to(frame_size)
let ok1:boolean = check_int("一、对面收到的字节数", length(got_back), frame_size)
if (!ok1) {
    bad = bad + 1
}
let one:wt.Frame = wt.read_frame(got_back)
let ok2:boolean = check_true("一、拆得开", one.ready)
if (!ok2) {
    bad = bad + 1
}
let two_text:string = "x".repeat(20000)
let ok3:boolean = check_int("一、正文的字符数", length(text.from_bytes(one.payload)), length(two_text))
if (!ok3) {
    bad = bad + 1
}
peer.drop(frame_size)

// 二、对面把一条消息切成两段送过来：头一段 fin 没置（0x01），第二段是接续且 fin 置着（0x80）
let frag_head:bytes = from_numbers([0x01, 0x02, 65, 66])
let frag_tail:bytes = from_numbers([0x80, 0x02, 67, 68])
peer.send_raw(bytes.join([frag_head, frag_tail]))
let msg_a:string = b.next_message()
let ok4:boolean = check_true("二、两段拼成一条", msg_a == "ABCD")
if (!ok4) {
    bad = bad + 1
}

// 三、对面发一条空正文的 ping（0x89 0x00），这一侧要回 pong；后面紧跟一条完整消息，
// 让读的那一句不必阻塞等待
let ping:bytes = from_numbers([0x89, 0x00, 0x81, 0x02, 65, 66])
peer.send_raw(ping)
let msg_b:string = b.next_message()
let ok5:boolean = check_true("三、ping 之后照常收到一条消息", msg_b == "AB")
if (!ok5) {
    bad = bad + 1
}
let pong:bytes = peer.read_up_to(6)
let ok6:boolean = check_int("三、pong 的第一个字节是 0x8A", pong[0] as int, 0x8A)
if (!ok6) {
    bad = bad + 1
}
let ok7:boolean = check_int("三、pong 的长度那一格是 0x80（遮码位加上零个字节）", pong[1] as int, 0x80)
if (!ok7) {
    bad = bad + 1
}

// 四、答复前面先来一条不带 id 的通知：认答复那一步要跳过它，不能因为取不到 id 就抛 10092
// （真浏览器上确实会发生：Target.attachToTarget 之后立刻来一条通知）。
let key_a:bytes = from_numbers([9, 8, 7, 6])
let key_b:bytes = from_numbers([9, 8, 7, 6])
let notice:bytes = wt.build_frame("{\"method\":\"Target.targetCreated\",\"params\":{}}", key_a)
let answer:bytes = wt.build_frame("{\"id\":1,\"result\":{\"ok\":true}}", key_b)
peer.send_raw(bytes.join([notice, answer]))
let got_reply:string = b.call("Nop", "{}")
let ok8:boolean = check_true("四、跳过头上那条通知，认回了自己的答复", got_reply == "{\"id\":1,\"result\":{\"ok\":true}}")
if (!ok8) {
    bad = bad + 1
}

b.close()
console.print("—— 自测跑完，不对", bad, "处")
if (bad > 0) {
    exit (1, "收发那一段有 " + number.to_string(bad) + " 处不对")
}
console.print("全对")

// WebSocket 帧的编码与解码（RFC 6455 第 5 节）。
//
// 这一份只负责一帧的形状，不负责 CDP 消息的内容，也不负责它属于哪一条命令。
// 分成两个文件的原因是出错表现不同：帧拼错了，在真浏览器上表现为
// "这一条连接不能用了"（10105），离协议文本越远越难定位。
//
// 两条写法约束（实测）：
// ① 一个表达式不能跨行，`[` 或 `,` 后面换行就是编译错误。
// ② take_wire 收进来的那块缓冲标的是 `ref`：只读不交出，调用方那块缓冲仍在它自己
//    手上。所以没凑成一帧时返回的也是抄的一份（不是原块）：代价是一次拷贝，
//    换来的是这块缓冲只有一个持有者，不会同一块在两处析构。

// 一次拆帧的结果。ready 为假表示这一块还凑不成一帧，此时 payload 为空，
// rest 返回已收到部分的拷贝。
// en: The result of splitting one frame off the front of a block: a false ready means the block is not a whole frame yet, and then payload is empty while rest is a copy of what was received.
export class Wire {
    ready:boolean
    fin:boolean
    opcode:int
    payload:bytes
    rest:bytes
}

// 把一段文本编码成一帧送出去：FIN 加上 text 操作码（1），长度按三档编码（小于 126 占一个
// 字节、小于 65536 跟两个字节、更大跟八个字节，都是大端序），再跟上四个字节长的掩码密钥
// 与遮码后的正文。客户端发出的帧一律要遮码。
// key 必须正好四个字节：比对结果要写成固定值，就得自带一串固定的密钥。
// en: Packs one text frame: FIN plus opcode 1, the length in its three widths (one byte below 126, two below 65536, eight beyond that, all big-endian), then the four-byte masking key and the masked body. Everything a client sends is masked, and the key has to be exactly four bytes because the comparison values are written out as fixed text.
export method encode_text(ref payload:bytes, ref key:bytes):bytes {
    return encode_frame(1, payload, key)
}

// 同上，只是操作码由参数给出：pong 需要 10。
// 这一条只编码 FIN 置位的一帧——我们发出的命令都装得下整条消息，不分段。
// en: The same packing with the opcode given from outside, which is what pong needs (10). Only frames with FIN set are packed here: every command we send fits one message, so nothing is continued.
export method encode_frame(opcode:int, ref payload:bytes, ref key:bytes):bytes {
    if (length(key) != 4) {
        throw (203, "遮码的钥匙要四个字节")
    }
    let n:int = length(payload)
    let head:int = 2
    if (n >= 126) {
        if (n < 65536) {
            head = 4
        } else {
            head = 10
        }
    }
    let out:bytes = bytes.new(head + 4 + n)
    out[0] = (0x80 | opcode) as byte
    if (head == 2) {
        out[1] = (0x80 + n) as byte
    } else if (head == 4) {
        out[1] = 0xFE as byte
        out[2] = ((n >> 8) & 255) as byte
        out[3] = (n & 255) as byte
    } else {
        out[1] = 0xFF as byte
        for (i:int in 0 until 8) {
            let shift:byte = ((7 - i) * 8) as byte
            out[2 + i] = ((n >> shift) & 255) as byte
        }
    }
    for (i:int in 0 until 4) {
        out[head + i] = key[i]
    }
    for (i:int in 0 until n) {
        let one:int = payload[i] as int
        let m:int = key[i & 3] as int
        out[head + 4 + i] = (one ^ m) as byte
    }
    return out
}

// 从整块字节的头部拆出第一帧。进来的这块由这一条函数处置（调用者交出的那个名字作废），
// 返回的 Wire.rest 是剩下未用的部分：拆成一帧时是切掉头与正文后的那一段，
// 没拆成时是整块抄的一份。
// 浏览器发来的帧不带遮码；带遮码也识别，把四个掩码字节读掉即可。
// en: Splits the first frame off a block. The block handed in is this function's to dispose of (the caller's name is spent on the way in), and Wire.rest is what is left over: a fresh copy when a frame came off, a copy of what was received when it did not.
export method take_wire(ref buf:bytes):Wire {
    let out:Wire = new Wire()
    out.ready = false
    out.payload = bytes.new(0)
    let n:int = length(buf)
    out.rest = buf.slice(0, n)
    if (n >= 2) {
        let b0:int = buf[0] as int
        let b1:int = buf[1] as int
        let masked:boolean = (b1 & 0x80) != 0
        let len:int = b1 & 0x7f
        let at:int = 2
        let enough:boolean = true
        if (len == 126) {
            if (n < 4) {
                enough = false
            } else {
                len = ((buf[2] as int) << 8) | (buf[3] as int)
                at = 4
            }
        } else if (len == 127) {
            if (n < 10) {
                enough = false
            } else {
                len = 0
                for (i:int in 0 until 8) {
                    len = (len << 8) | (buf[2 + i] as int)
                }
                at = 10
            }
        }
        let key_at:int = at
        if (masked) {
            at = at + 4
        }
        if (enough && n >= at + len) {
            let body:bytes = bytes.new(len)
            for (i:int in 0 until len) {
                let v:int = buf[at + i] as int
                if (masked) {
                    v = v ^ (buf[key_at + (i & 3)] as int)
                }
                body[i] = v as byte
            }
            out.fin = (b0 & 0x80) != 0
            out.opcode = b0 & 0x0f
            out.payload = body
            out.rest = buf.slice(at + len, n)
            out.ready = true
        }
    }
    return out
}

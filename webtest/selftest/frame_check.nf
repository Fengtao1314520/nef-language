// web_test 这一层里帧的那一段自测：不起浏览器、不联网，只比字节。
//
// 跑法（在仓库根）：./nf.exe webtest/selftest/frame_check.nf
// 全对就印"全对"，退出码 0；有一处不对就把那几处打印出来，退出码 1。
//
// 期望值的两样来源：
// ① RFC 6455 第 5 节的三个长度档与遮码例子。"Hello" 那一帧的十一个字节是照那份规范手算的：
//    'H'=0x48^0x37=0x7F、'e'=0x65^0xFA=0x9F、'l'=0x6C^0x21=0x4D、'l'=0x6C^0x58=0x34、'o'=0x6F^0x37=0x58。
// ② 从本机 Edge 155 与 Chrome 155 实际收到的答复帧：不遮码、正文是一段 JSON、
//    长度落在一个字节那一档（第四节那一帧就是它的形状）。

import wt from "libs/web_test"

method from_numbers(xs:list<int>):bytes {
    let out:bytes = bytes.new(length(xs))
    for (i:int in 0 until length(xs)) {
        out[i] = xs[i] as byte
    }
    return out
}

method check_bytes(name:string, got:bytes, want:bytes):boolean {
    if (length(got) != length(want)) {
        console.print("不对  " + name, "长度", length(got), "要", length(want))
        console.print("  给", got)
        console.print("  要", want)
        return false
    }
    for (i:int in 0 until length(want)) {
        if (got[i] != want[i]) {
            console.print("不对  " + name, "第", i, "格", got[i], "要", want[i])
            return false
        }
    }
    console.print("过  " + name)
    return true
}

method check_int(name:string, got:int, want:int):boolean {
    if (got != want) {
        console.print("不对  " + name, "给", got, "要", want)
        return false
    }
    console.print("过  " + name)
    return true
}

method check_true(name:string, got:boolean):boolean {
    if (!got) {
        console.print("不对  " + name, "要真，给的是假")
        return false
    }
    console.print("过  " + name)
    return true
}

method check_text(name:string, got:string, want:string):boolean {
    if (got != want) {
        console.print("不对  " + name, "给", got, "要", want)
        return false
    }
    console.print("过  " + name)
    return true
}

let bad:int = 0

// 一、遮码那一帧：长度小的一档，钥匙用 RFC 例子里那四个字节
let key1:bytes = from_numbers([0x37, 0xFA, 0x21, 0x58])
let want1:bytes = from_numbers([0x81, 0x85, 0x37, 0xFA, 0x21, 0x58, 0x7F, 0x9F, 0x4D, 0x34, 0x58])
let got1:bytes = wt.build_frame("Hello", key1)
let ok1:boolean = check_bytes("一、Hello 遮码一帧", got1, want1)
if (!ok1) {
    bad = bad + 1
}

// 二、130 字节要走两个字节那一档的长度；钥匙全 0，遮与不遮一样
let zeros:bytes = from_numbers([0, 0, 0, 0])
let mid:bytes = wt.build_frame("A".repeat(130), zeros)
let ok2:boolean = check_int("二、总长 2+2+4+130", length(mid), 138)
if (!ok2) {
    bad = bad + 1
}
let ok3:boolean = check_int("二、第一个字节 0x81", mid[0] as int, 0x81)
if (!ok3) {
    bad = bad + 1
}
let ok4:boolean = check_int("二、第二个字节 0xFE", mid[1] as int, 0xFE)
if (!ok4) {
    bad = bad + 1
}
let wide2:int = ((mid[2] as int) << 8) | (mid[3] as int)
let ok5:boolean = check_int("二、后面两格读出 130", wide2, 130)
if (!ok5) {
    bad = bad + 1
}
let mid_body:bytes = mid.slice(8, 138)
let mid_want:bytes = "A".repeat(130).to_bytes()
let ok6:boolean = check_bytes("二、正文一格不差", mid_body, mid_want)
if (!ok6) {
    bad = bad + 1
}

// 三、七万字节要走八个字节那一档
let big_zeros:bytes = from_numbers([0, 0, 0, 0])
let big:bytes = wt.build_frame("B".repeat(70000), big_zeros)
let ok7:boolean = check_int("三、总长 2+8+4+70000", length(big), 70014)
if (!ok7) {
    bad = bad + 1
}
let ok8:boolean = check_int("三、第二个字节 0xFF", big[1] as int, 0xFF)
if (!ok8) {
    bad = bad + 1
}
let wide:int = 0
for (i:int in 0 until 8) {
    wide = (wide << 8) | (big[2 + i] as int)
}
let ok9:boolean = check_int("三、八格读出 70000", wide, 70000)
if (!ok9) {
    bad = bad + 1
}

// 四、拆一帧不遮码的（浏览器发给我们的就是这个样子）
let buf4:bytes = from_numbers([0x81, 0x05, 72, 105, 33, 63, 33])
let one:wt.Frame = wt.read_frame(buf4)
let ok10:boolean = check_true("四、拆开了", one.ready)
if (!ok10) {
    bad = bad + 1
}
let ok11:boolean = check_int("四、操作码是 text", one.opcode, 1)
if (!ok11) {
    bad = bad + 1
}
let ok12:boolean = check_true("四、FIN 置着", one.fin)
if (!ok12) {
    bad = bad + 1
}
let left4:int = length(one.rest)
let ok13:boolean = check_int("四、七个字节全吃掉", left4, 0)
if (!ok13) {
    bad = bad + 1
}
let body4:string = text.from_bytes(one.payload)
let ok14:boolean = check_text("四、正文", body4, "Hi!?!")
if (!ok14) {
    bad = bad + 1
}

// 五、拆一帧遮码的：拿第一节那十一个字节反过来读
let buf5:bytes = from_numbers([0x81, 0x85, 0x37, 0xFA, 0x21, 0x58, 0x7F, 0x9F, 0x4D, 0x34, 0x58])
let two:wt.Frame = wt.read_frame(buf5)
let ok15:boolean = check_true("五、拆开了", two.ready)
if (!ok15) {
    bad = bad + 1
}
let body5:string = text.from_bytes(two.payload)
let ok16:boolean = check_text("五、遮掉的正文还原", body5, "Hello")
if (!ok16) {
    bad = bad + 1
}

// 六、还不够一帧：剩下的那一些抄一份留着
let buf6:bytes = from_numbers([0x81, 0x05, 72])
let part:wt.Frame = wt.read_frame(buf6)
let ok17:boolean = check_true("六、说还没成一帧", part.ready == false)
if (!ok17) {
    bad = bad + 1
}
let left6:int = length(part.rest)
let ok18:boolean = check_int("六、三格都留着", left6, 3)
if (!ok18) {
    bad = bad + 1
}

// 七、两块连在一起：头一块吃掉之后，剩下的正好是第二块
let a7:bytes = from_numbers([0x81, 0x02, 65, 66])
let b7:bytes = from_numbers([0x81, 0x05, 72, 105, 33, 63, 33])
let joined:bytes = bytes.join([a7, b7])
let first:wt.Frame = wt.read_frame(joined)
let body7:string = text.from_bytes(first.payload)
let ok19:boolean = check_text("七、头一块的正文", body7, "AB")
if (!ok19) {
    bad = bad + 1
}
let tail7:bytes = first.rest
let left7:int = length(tail7)
let ok20:boolean = check_int("七、剩下七格", left7, 7)
if (!ok20) {
    bad = bad + 1
}
let second:wt.Frame = wt.read_frame(tail7)
let body7b:string = text.from_bytes(second.payload)
let ok21:boolean = check_text("七、第二块的正文", body7b, "Hi!?!")
if (!ok21) {
    bad = bad + 1
}

// 八、拼出去的大帧再拆回来：130 那一档两头都走一遍
let key8:bytes = from_numbers([1, 2, 3, 4])
let frame8:bytes = wt.build_frame("A".repeat(130), key8)
let round:wt.Frame = wt.read_frame(frame8)
let ok22:boolean = check_true("八、拆开了", round.ready)
if (!ok22) {
    bad = bad + 1
}
let size8:int = length(round.payload)
let ok23:boolean = check_int("八、正文 130 格", size8, 130)
if (!ok23) {
    bad = bad + 1
}
let body8:string = text.from_bytes(round.payload)
let want8:string = "A".repeat(130)
let ok24:boolean = check_text("八、正文还是那一段", body8, want8)
if (!ok24) {
    bad = bad + 1
}

console.print("—— 自测跑完，不对", bad, "处")
if (bad > 0) {
    exit (1, "帧的编码与解码有 " + number.to_string(bad) + " 处不对")
}
console.print("全对")

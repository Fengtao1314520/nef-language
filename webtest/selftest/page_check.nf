// 用存下来的四份真答复验两件事：从答复里取字段取得对，截图那一段能解码成 png 并落成一个文件。
// 这四份文本是从本机 Chrome 155 取下来的原文，一个字节未改。
//
// 跑法（两个参数：那份数据在哪一层、png 写到哪一个文件）：
//
//	./nf.exe webtest/selftest/page_check.nf webtest/selftest/data /临时/shot.png

import wt from "libs/web_test"

method check_text(name:string, got:string, want:string):boolean {
    if (got != want) {
        console.print("不对  " + name, "给", got, "要", want)
        return false
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

let bad:int = 0
let dir:string = args[0]
let shot:string = args[1]

// 一、二、三：那三条命令的答复里，要的那个位置都在 result 下面
let created:string = file.read(dir + "/create_target.txt")
let target:string = wt.reply_field(created, "targetId")
let ok1:boolean = check_text("一、targetId", target, "A98F5B5320D5EE79B04BA25EADF0A9C1")
if (!ok1) {
    bad = bad + 1
}

let attached:string = file.read(dir + "/attach.txt")
let session:string = wt.reply_field(attached, "sessionId")
let ok2:boolean = check_text("二、sessionId", session, "CA08F73990771EF91D9E1ECD110C9A18")
if (!ok2) {
    bad = bad + 1
}

let nav:string = file.read(dir + "/navigate.txt")
let frame:string = wt.reply_field(nav, "frameId")
let ok3:boolean = check_text("三、frameId", frame, "A98F5B5320D5EE79B04BA25EADF0A9C1")
if (!ok3) {
    bad = bad + 1
}

// 四：没有的那个位置返回空句柄，不抛新错误。
// 那一份 navigate.txt 在这里再读一遍：nav 那个名字上一条已经交出去了，位置当场空了。
let none:string = wt.reply_field(file.read(dir + "/navigate.txt"), "没有这一格")
let ok4:boolean = check_true("四、取不到的那一格是空句柄", none == null)
if (!ok4) {
    bad = bad + 1
}

// 五、六：那一串 base64 拆成 png 的字节，再落成一个文件读回来
let png_answer:string = file.read(dir + "/screenshot.txt")
let png:bytes = wt.png_from_reply(png_answer)
let ok5:boolean = check_int("五、png 的字节数", length(png), 1005)
if (!ok5) {
    bad = bad + 1
}
let ok6:boolean = check_int("五、第一个字节是 137", png[0] as int, 137)
if (!ok6) {
    bad = bad + 1
}
let ok7:boolean = check_int("五、第四个字节是 71（png 的头四格）", png[3] as int, 71)
if (!ok7) {
    bad = bad + 1
}
file.write_bytes(shot, png)
let back:bytes = file.read_bytes(shot)
let ok8:boolean = check_int("六、读回来还是 1005 个字节", length(back), 1005)
if (!ok8) {
    bad = bad + 1
}
let ok9:boolean = check_int("六、最后一格也对得上", back[1004] as int, png[1004] as int)
if (!ok9) {
    bad = bad + 1
}

// 七：答复里没有 data 那一段，报的是 204
let code:int = -1
try {
    let junk:bytes = wt.png_from_reply("{\"id\":9,\"result\":{}}")
    console.print("不该走到这一句", length(junk))
} catch (c, m) {
    code = c
}
let ok10:boolean = check_int("七、答复里没 data 那一段是 204", code, 204)
if (!ok10) {
    bad = bad + 1
}

file.remove(shot)
console.print("—— 自测跑完，不对", bad, "处")
if (bad > 0) {
    exit (1, "取字段与截图那一段有 " + number.to_string(bad) + " 处不对")
}
console.print("全对")

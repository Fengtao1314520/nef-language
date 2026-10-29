// web_test：拿 Nef 自己写的代码驱动一个浏览器。
//
// 包内三份：frame.nf 负责一帧的形状，wire.nf 负责字符串相关的处理（握手文本、从答复里
// 取字段、把定位拼成一段浏览器可执行的 JS），这一份是门面，交出 Browser、Page、Element 三个类型。
// 三个类写在一起是因为编译器不许包内文件互相引用（实测报
// "libs/web_test/cyc.nf 与 ./widget.nf 互相引用"）。
//
// 那条连接由下面这个包内的状态对象持有，三个类型的成员都通过它发命令。
// 这样 Page 与 Element 才能同时存在、元素还能放进 list：语言里一个值只有一个持有者、
// `this` 又交不出去（实测报"this 是方法接收者的只读借用，不能交出去"），
// 对象之间互相引用是写不出来的。限制随之写在 README 里：一个进程连一只浏览器。

import { encode_text, encode_frame, take_wire, Wire } from "./frame.nf"
import { build_handshake, json_string_of, first_chars_of, reply_field_of, png_from_reply_of, find_expression, count_expression, read_text_script, write_text_script, box_script, center_script, visible_script, enabled_script, exists_script, scroll_script, attribute_script, title_script, url_script, ready_script, body_text_script } from "./wire.nf"

// 库自己的错误号。语言的号占 10000 往上，0～9999 留给库与用户程序。
// en: The library's own error codes. The language takes 10000 and up, leaving 0-9999 to libraries and user scripts.

// 浏览器起来了，等不到它写下那个端口。
// en: The browser started but never wrote its port file in time.
// 例：
// try { wt.start("msedge.exe", "profile") } catch (bad_code, bad_msg) { console.print(bad_code == wt.code_port_timeout, bad_msg) }
export const code_port_timeout:int = 200

// 握手回来的不是 101。
// en: The handshake came back with something other than 101.
// 例：
// try { wt.start("msedge.exe", "profile") } catch (bad_code, bad_msg) { console.print(bad_code == wt.code_handshake_failed, bad_msg) }
export const code_handshake_failed:int = 201

// 连接断了，或对方先关了。
// en: The connection broke, or the other end closed first.
// 例：
// throw (wt.code_socket_closed, "连不上那一页")
export const code_socket_closed:int = 202

// 帧本身不成样子（frame.nf 那一句同一个号）。
// en: The frame itself is malformed (frame.nf reports the same number).
// 例：
// throw (wt.code_bad_frame, "读出一条不像样的帧")
export const code_bad_frame:int = 203

// 答复不是一段 JSON，或缺要的那个位置。
// en: The reply is not a piece of JSON, or lacks the slot being asked for.
// 例：
// throw (wt.code_reply_not_json, wt.first_chars(answer, 60))
export const code_reply_not_json:int = 204

// 等到时限还没等到那个元素。
// en: The element was still missing when the waiting limit ran out.
// 例：
// throw (wt.code_element_missing, "等不到登录按钮")
export const code_element_missing:int = 205

// 断言不通过。
// en: An assertion did not hold.
// 例：
// throw (wt.code_assert_failed, "标题不对")
export const code_assert_failed:int = 206

// 页面没到位。
// en: The page never reached the state being waited for.
// 例：
// throw (wt.code_page_not_ready, "等完那一段页面还没到位")
export const code_page_not_ready:int = 207

// 浏览器对那一条命令回了 error 那个位置。
// en: The browser answered that command with something in the error slot.
// 例：
// throw (wt.code_browser_error, "Page.navigate 被拒了")
export const code_browser_error:int = 208

// 等到时限没等到浏览器主动发来的那一条通知。
// en: The notification the browser was expected to send never came within the waiting limit.
// 例：
// throw (wt.code_event_timeout, "等不到 Page.loadEventFired")
export const code_event_timeout:int = 209

// 一帧的形状，包内那一份的名字原样交出去。
// en: The shape of one frame, handed out under the name it carries inside the package.
// 例：
// let got: wt.Frame = wt.read_frame(block)
export type Frame = Wire

const recv_size:int = 8192
const wait_step_ms:int = 100
const expect_ms:int = 3000
const port_ms:int = 8000
const io_ms:int = 5000
const kind_css:int = 1
const kind_xpath:int = 2
const kind_text:int = 3

// 包内的这一份状态。字段而不是文件级的名字，是因为一个名字交出去之后就不能再写
// （实测"a 已经交出去了，这个名字不能再写"），而攒字节这件事每收一片都要把原来那一块
// 交进 bytes.join 一次；字段的位置可以这么来回写。
class Link {
    sock:Socket
    proc:Process
    pending:bytes
    held:bytes
    next_id:int
    port:int
    ws_path:string
    sessions:list<string>
    events:dict<string,string>
}

let link:Link = new Link()

// —— 一条命令进、一条答复出 ——

method send_all(ref body:bytes) {
    let sent:int = 0
    let n:int = length(body)
    while (sent < n) {
        let part:int = link.sock.send(body.slice(sent, n))
        if (part <= 0) {
            throw (code_socket_closed, "送不出去：这一条连接不能用了")
        }
        sent = sent + part
    }
}

method send_text_line(ref body:string) {
    let key:bytes = random_key()
    send_all(encode_text(body.to_bytes(), key))
}

method random_key():bytes {
    let k:bytes = bytes.new(4)
    for (i:int in 0 until 4) {
        k[i] = math.random(0, 256) as byte
    }
    return k
}

method pull_into() {
    let block:bytes = bytes.new(recv_size)
    let got:int = link.sock.recv_into(block)
    if (got <= 0) {
        throw (code_socket_closed, "浏览器那边关了这条连接")
    }
    link.pending = bytes.join([link.pending, block.slice(0, got)])
}

method reply_pong(ref body:bytes) {
    let key:bytes = random_key()
    send_all(encode_frame(10, body, key))
}

// 读出一条消息的正文：一帧不够就再收一片，一条消息被切成几段就按 FIN 位接起来，
// 收到 ping 就照原样回一条 pong。
method read_message():string {
    link.held = bytes.new(0)
    while (true) {
        let buf:bytes = link.pending
        let one:Frame = take_wire(buf)
        link.pending = one.rest
        if (one.ready) {
            if (one.opcode == 8) {
                throw (code_socket_closed, "浏览器把这一条连接关了")
            }
            if (one.opcode == 9) {
                // 字段位置的值直接当实参交出去会走过户，交出后位置就空了，所以先接给一个名字
                let beat:bytes = one.payload
                reply_pong(beat)
            } else if (one.opcode == 0 || one.opcode == 1 || one.opcode == 2) {
                link.held = bytes.join([link.held, one.payload])
                if (one.fin) {
                    return text.from_bytes(link.held)
                }
            }
        } else {
            pull_into()
        }
    }
    return ""
}

// 送一条命令，读到 id 对得上的那一条答复为止。没有 id 的消息是浏览器主动发的通知，
// 存进 link.events 等上层来取。
// 必须先判 `id` 位置是否存在：json.to_integer 拿到空句柄会抛 10092。
method ask(page_index:int, ref method_name:string, ref params:string):string {
    link.next_id = link.next_id + 1
    let id:int = link.next_id
    let body:string = "{\"id\":" + number.to_string(id) + ",\"method\":" + json_string_of(method_name)
    if (page_index >= 0) {
        body = body + ",\"sessionId\":" + json_string_of(session_of(page_index))
    }
    body = body + ",\"params\":" + params + "}"
    send_text_line(body)
    while (true) {
        let one:string = read_message()
        let doc:Json = json.parse(one)
        if (doc == null) {
            throw (code_reply_not_json, "浏览器交回来的不是一段 JSON：" + first_chars_of(one, 60))
        }
        let id_slot:Json = json.at(doc, "id")
        if (id_slot == null) {
            store_event(one)
        } else {
            if (json.to_integer(id_slot, 0) == id) {
                // 答复带 error 位置就是浏览器拒绝了这条命令，把原文带出去。不报这一句，
                // "命令送出但未生效"这一类问题就无从定位。
                if (json.at(doc, "error") != null) {
                    throw (code_browser_error, "浏览器拒了这条命令（" + method_name + "）：" + first_chars_of(one, 160))
                }
                return one
            }
        }
    }
    return ""
}

// —— 浏览器主动发来的那一条（通知）——
//
// 存的是"哪一个页发的、哪一件事"两部分拼成的键，值是那条原文。不用 list 逐条比对：
// 容器装引用类型时进出都是交出，位置读一次就归读走（实测 `xs[0]` 第二次读就是 10092），
// 扫一遍等于清空整条队列。查询走只读借用，`contains` 与 `get` 不动其他槽位。
// 同一页同一事件连续到来只留最后一条：这里等的是"发生过"，等不到报 209。

method store_event(body:string) {
    let doc:Json = json.parse(body)
    if (doc == null) {
        return
    }
    let which:Json = json.at(doc, "method")
    if (which == null) {
        return
    }
    let name:string = json.to_text(which)
    if (name == null) {
        return
    }
    // 不带 sessionId 的是浏览器层的通知，键的开头用 "*" 占位：空句柄拼进 `+` 会触发运行期 10092。
    let here:Json = json.at(doc, "sessionId")
    let session:string = "*"
    if (here != null) {
        let got:string = json.to_text(here)
        if (got != null) {
            session = got
        }
    }
    link.events[session + "|" + name] = body
}

// 从表里找那一条，找到就把它摘走（同一件事第二次等，不会拿上一回那条充数）。
// 先查这一页自己的那一条，再查浏览器层的那一条（键开头是 "*" 的那些）。
method take_event(ref name:string, ref session:string):string {
    let mine:string = session + "|" + name
    if (link.events.contains(mine)) {
        let first:string = link.events.get(mine, "")
        link.events.remove(mine)
        return first
    }
    let broad:string = "*|" + name
    if (link.events.contains(broad)) {
        let second:string = link.events.get(broad, "")
        link.events.remove(broad)
        return second
    }
    return null
}

// 等一条通知。recv 到达连接设定的时限会报 10102，这一种在这里不算错：
// 它表示这段时间浏览器没有数据到达，继续查队列、继续等；其余错误号照常抛出。
method wait_event_in(page_index:int, ref name:string, timeout_ms:int):string {
    let session:string = "*"
    if (page_index >= 0) {
        session = session_of(page_index)
    }
    let begun:float = time.elapsed()
    let limit:float = timeout_ms as float
    while (true) {
        let hit:string = take_event(name, session)
        if (hit != null) {
            return hit
        }
        if ((time.elapsed() - begun) * 1000.0 >= limit) {
            throw (code_event_timeout, "等不到那条通知：" + first_chars_of(name, 60) + " 等了 " + number.to_string(timeout_ms) + " 毫秒")
        }
        try {
            let one:string = read_message()
            store_event(one)
        } catch (code, msg) {
            if (code != 10102) {
                throw (code, msg)
            }
        }
    }
    return null
}

method session_of(page_index:int):string {
    if (page_index < 0 || page_index >= length(link.sessions)) {
        throw (code_reply_not_json, "没有第 " + number.to_string(page_index) + " 页")
    }
    // 从 list 里取那一样是交出语义，取过之后位置就空了。
    // 这个位置后面还要再取（同一页的第二条命令），所以取出来先接住，再把一份拷贝写回去。
    let got:string = link.sessions[page_index]
    link.sessions[page_index] = got + ""
    return got
}

// 送一段 JS 到那一页执行，返回它算出来的那段文本。算不出东西返回空句柄。
// 库里所有读与写都从这一条走，所以那些 JS 一律把结果写成文本：
// 数与真假经 json.to_text 会得到空句柄。
method run_js_in(page_index:int, ref expression:string):string {
    let answer:string = ask(page_index, "Runtime.evaluate", "{\"expression\":" + json_string_of(expression) + ",\"returnByValue\":true}")
    let doc:Json = json.parse(answer)
    if (doc == null) {
        throw (code_reply_not_json, "这段 JS 的答复不是 JSON：" + first_chars_of(answer, 60))
    }
    if (json.at(doc, "error") != null) {
        throw (code_browser_error, "这段 JS 被浏览器拒了：" + first_chars_of(answer, 120))
    }
    let result:Json = json.at(doc, "result")
    if (result == null) {
        return null
    }
    // 页面里那段 JS 自己抛出的错走 exceptionDetails，不走顶层的 error（两者不同：
    // 前者是命令未被识别，后者是命令已识别、在页面上执行时出错）。
    if (json.at(result, "exceptionDetails") != null) {
        throw (code_browser_error, "这段 JS 在页面上出了错：" + first_chars_of(expression + " || ", 120) + first_chars_of(answer, 200))
    }
    let inner:Json = json.at(result, "result")
    if (inner == null) {
        return null
    }
    let slot:Json = json.at(inner, "value")
    if (slot == null) {
        return null
    }
    return json.to_text(slot)
}

method count_in(page_index:int, kind:int, ref selector:string):int {
    let got:string = run_js_in(page_index, count_expression(kind, selector))
    if (got == null) {
        return 0
    }
    return text.to_int(got)
}

method present(page_index:int, kind:int, ref selector:string, position:int):boolean {
    return run_js_in(page_index, exists_script(find_expression(kind, selector, position))) == "1"
}

method describe_kind(kind:int):string {
    if (kind == kind_xpath) {
        return "XPath "
    }
    if (kind == kind_text) {
        return "可见文字 "
    }
    return "CSS "
}

// 元素中心在页面上的位置："横,纵"两个整数用逗号隔开的一段文本；找不到元素返回空句柄。
method center_of(page_index:int, kind:int, ref selector:string, position:int):string {
    return run_js_in(page_index, center_script(find_expression(kind, selector, position)))
}

// 把指针挪到页面上那一个点。悬停要走这一条；要先挪开再挪过去的原因写在 hover 那一句里。
method mouse_move(page_index:int, x:int, y:int) {
    let params:string = "{\"type\":\"mouseMoved\",\"x\":" + number.to_string(x) + ",\"y\":" + number.to_string(y) + ",\"modifiers\":0}"
    ask(page_index, "Input.dispatchMouseEvent", params)
}

// 在那个点上按下、抬起（clicks 是第几次点击，双击时传 2）。
method mouse_at(page_index:int, ref event_type:string, x:int, y:int, clicks:int) {
    let params:string = "{\"type\":" + json_string_of(event_type) + ",\"x\":" + number.to_string(x) + ",\"y\":" + number.to_string(y)
    params = params + ",\"button\":\"left\",\"clickCount\":" + number.to_string(clicks) + "}"
    ask(page_index, "Input.dispatchMouseEvent", params)
}

// 元素上的一次或两次点击：中心算出来，按下抬起各一次（双击两对）。
method click_element(page_index:int, kind:int, ref selector:string, position:int, clicks:int) {
    let where:string = center_of(page_index, kind, selector, position)
    if (where == null) {
        throw (code_element_missing, "点不动：按" + describe_kind(kind) + "「" + selector + "」找不到元素")
    }
    let parts:list<string> = where.split(",")
    let x:int = text.to_int(parts[0])
    let y:int = text.to_int(parts[1])
    mouse_at(page_index, "mousePressed", x, y, 1)
    mouse_at(page_index, "mouseReleased", x, y, 1)
    if (clicks > 1) {
        mouse_at(page_index, "mousePressed", x, y, 2)
        mouse_at(page_index, "mouseReleased", x, y, 2)
    }
}

method key_event(page_index:int, ref event_type:string, ref key:string, ref code:string, virtual:int) {
    let params:string = "{\"type\":" + json_string_of(event_type) + ",\"key\":" + json_string_of(key) + ",\"code\":" + json_string_of(code)
    params = params + ",\"windowsVirtualKeyCode\":" + number.to_string(virtual) + ",\"nativeVirtualKeyCode\":" + number.to_string(virtual) + ",\"modifiers\":0}"
    ask(page_index, "Input.dispatchKeyEvent", params)
}

// 按下再抬起。识别 Enter、Tab、Escape、Backspace 与单个字母、单个数字（不区分大小写）；
// 其余功能键需逐个扩展，每加一个补一条检查。
// key 位置与 code 位置不同：key 写页面上可见的键名（"Enter"、"a"），
// code 写键盘上的物理键（"Enter"、"KeyA"、"Digit1"）——页面里 e.key 读的是前者。
method key_by_name(page_index:int, ref name:string) {
    let lowered:string = name.to_lower()
    let label:string = ""
    let code:string = ""
    let virtual:int = 0
    if (lowered == "enter") {
        label = "Enter"
        code = "Enter"
        virtual = 13
    } else if (lowered == "tab") {
        label = "Tab"
        code = "Tab"
        virtual = 9
    } else if (lowered == "escape") {
        label = "Escape"
        code = "Escape"
        virtual = 27
    } else if (lowered == "backspace") {
        label = "Backspace"
        code = "Backspace"
        virtual = 8
    } else if (length(lowered) == 1) {
        let one:int = lowered[0] as int
        if (one >= 97 && one <= 122) {
            // 赋给 label 就是把 lowered 整个交出，下面还要用 lowered 两回，
            // 所以这里给一份拷贝。
            label = lowered + ""
            code = "Key" + lowered.to_upper()
            virtual = one - 32
        } else if (one >= 48 && one <= 57) {
            label = lowered + ""
            code = "Digit" + lowered
            virtual = one
        }
    }
    if (code == "") {
        throw (code_reply_not_json, "这一个键名还不认：" + lowered)
    }
    key_event(page_index, "keyDown", label, code, virtual)
    key_event(page_index, "keyUp", label, code, virtual)
}

method wait_present(page_index:int, kind:int, ref selector:string, timeout_ms:int):boolean {
    let waited:int = 0
    while (true) {
        if (present(page_index, kind, selector, -1)) {
            return true
        }
        if (waited >= timeout_ms) {
            return false
        }
        time.sleep(wait_step_ms)
        waited = waited + wait_step_ms
    }
    return false
}

// 一句断言到底：反复读那一段文本，等到对上或到时限。
method expect_reading(page_index:int, ref script:string, ref want:string, ref what:string, timeout_ms:int) {
    let waited:int = 0
    while (true) {
        let now:string = run_js_in(page_index, script)
        if (now == want) {
            return
        }
        if (waited >= timeout_ms) {
            throw (code_assert_failed, "断言不通过：" + what + "是「" + first_chars_of(now + "|", 60) + "」，要的是「" + first_chars_of(want + "|", 60) + "」")
        }
        time.sleep(wait_step_ms)
        waited = waited + wait_step_ms
    }
}

// —— 三个交出去的类型 ——

// 一只开着的浏览器。它只是"浏览器那一层"的句柄：连接在包的状态里，所以 page.get_browser()
// 返回来的那一只与 start 那一只是同一个东西，两只可以同时在手里。
// en: One open browser. It is only the handle to the browser layer: the connection lives in the package's state, so the one page.get_browser() gives back and the one start returned are the same browser and both may be held at once.
// 例：
// let held: wt.Browser = wt.current_browser()
export class Browser {
    // 本机给这个浏览器的那一个端口号。
    // en: The local port this browser's DevTools endpoint is listening on.
    // 例：
    // console.print(browser.port())
    method port():int {
        return link.port
    }

    // 这一趟连的是哪台浏览器（Chrome 或 Edge 的名字与版本号）。
    // en: Which browser this run is connected to: the name and version, straight off the DevTools endpoint.
    // 例：
    // console.print(browser.product())
    method product():string {
        return reply_field_of(ask(-1, "Browser.getVersion", "{}"), "product")
    }

    // 库未封装的命令走这一条：方法名与参数原样送过去，返回答复文本。
    // en: The escape hatch for commands the library does not wrap: the method name and the parameters are sent verbatim, and the reply text comes back.
    // 例：
    // let history: string = browser.call("Page.getNavigationHistory", "{}")
    method call(ref method_name:string, ref params:string):string {
        return ask(-1, method_name, params)
    }

    // 读出一条消息的正文。答复之外还有浏览器主动发的通知，这一条把两者都返回来，
    // 给这一层的自测与上一层的逃生用。
    // en: Reads the payload of one message. Besides replies, the browser also sends notifications on its own; this hands back both, and the layer above uses it for its self-tests.
    // 例：
    // console.print(browser.next_message())
    method next_message():string {
        return read_message()
    }

    // 一块一块送，直到送完。
    // en: Sends the whole block, piece by piece until it is all out.
    // 例：
    // browser.send_all_bytes(bytes.new(64))
    method send_all_bytes(ref body:bytes) {
        send_all(body)
    }

    // 现在开着的那些页面的 targetId。那一串在答复的 result 下面一层（与 sessionId 同一层）。
    // Target.getTargets 返回的是所有目标（还有 service worker 那一类），所以按 type=="page" 挑。
    // en: The targetIds of the pages that are open right now. Targets of other kinds (service workers and the like) are filtered out by type.
    // 例：
    // console.print(length(browser.list_pages()))
    method list_pages():list<string> {
        let out:list<string> = []
        let doc:Json = json.parse(ask(-1, "Target.getTargets", "{}"))
        let result:Json = null
        if (doc != null) {
            result = json.at(doc, "result")
        }
        if (result != null) {
            let items:Json = json.at(result, "targetInfos")
            if (items != null) {
                for (i:int in 0 until json.count(items)) {
                    let one_info:Json = json.item(items, i)
                    if (one_info != null) {
                        let kind_slot:Json = json.at(one_info, "type")
                        let id_slot:Json = json.at(one_info, "targetId")
                        if (kind_slot != null && id_slot != null && json.to_text(kind_slot) == "page") {
                            let id:string = json.to_text(id_slot)
                            if (id != null) {
                                out.add(id)
                            }
                        }
                    }
                }
            }
        }
        return out
    }

    // 开一页并挂上去，等它报成 complete（等不到报 207）。
    // en: Opens a tab, attaches to it, and waits until the document reports complete; 207 if that never happens.
    // 例：
    // let opened: Page = browser.open_page("https://example.com/")
    method open_page(ref url:string, wait_ms:int = expect_ms):Page {
        let created:string = ask(-1, "Target.createTarget", "{\"url\":" + json_string_of(url) + "}")
        let target:string = reply_field_of(created, "targetId")
        if (target == null) {
            throw (code_reply_not_json, "开一页那一步的答复里没有 targetId 那一格")
        }
        let made:Page = this.attach_page(target)
        made.settle_on(url, wait_ms)
        return made
    }

    // 挂到一个已经开着的页面上（页号从 list_pages 那边来）。
    // en: Attaches to a tab that is already open; the targetId comes from list_pages.
    // 例：
    // let opened: Page = browser.attach_page(browser.list_pages()[0])
    method attach_page(ref target_id:string):Page {
        let attached:string = ask(-1, "Target.attachToTarget", "{\"targetId\":" + json_string_of(target_id) + ",\"flatten\":true}")
        let session:string = reply_field_of(attached, "sessionId")
        if (session == null) {
            throw (code_reply_not_json, "挂上去那一步的答复里没有 sessionId 那一格")
        }
        link.sessions.add(session)
        let made:int = length(link.sessions) - 1
        // 开启这一页的事件流：通知（`Page.loadEventFired` 那一族）要先调用这一条，
        // 浏览器才往这条连接上发。页号要先进 sessions，带页号的命令才找得到这一页
        // （session_of 按位置取），所以这一句排在 new Page 之前。
        ask(made, "Page.enable", "{}")
        return new Page(index: made)
    }

    // 收掉：断开连接、结束那个浏览器进程。漏写这一句也不会留下多余窗口：那两个句柄在包
    // 的状态里，程序结束时的收尾会把它们析构。
    // en: Tears the connection down and ends the browser process. Leaving this out still does not leave windows around: the handles are registered, and the end-of-program accounting releases them.
    // 例：
    // browser.close()
    method close() {
        if (link.sock != null) {
            link.sock.close()
            link.sock = null
        }
        if (link.proc != null) {
            link.proc.close()
            link.proc = null
        }
        link.pending = null
        link.held = null
        link.sessions = []
        link.events = {}
    }
}

// 一页。里面只有一个页号。
// en: One page. All it holds is the page number.
// 例：
// let opened: wt.Page = browser.open_page("https://example.com/")
export class Page {
    index:int

    // 取回这一页所属的那只浏览器把手。
    // en: Gives back the browser handle this page belongs to.
    // 例：
    // let back: Browser = page.get_browser()
    method get_browser():Browser {
        return new Browser()
    }

    // 这一页在协议里的那个 sessionId。
    // en: The sessionId this page is addressed by on the wire.
    // 例：
    // console.print(page.session_id())
    method session_id():string {
        return session_of(this.index)
    }

    // —— 定位：找不着返回空句柄，等到了算 ——

    // 按 CSS 选一个元素；找不着返回空句柄。
    // en: Finds one element by CSS selector; a null handle when there is none.
    // 例：
    // let one: Element = page.find_element_by_css("h1")
    method find_element_by_css(ref selector:string):Element {
        return this.find_one(kind_css, selector)
    }

    // 按 XPath 选一个元素；找不着返回空句柄。
    // en: Finds one element by XPath; a null handle when there is none.
    // 例：
    // let one: Element = page.find_element_by_xpath("//h1")
    method find_element_by_xpath(ref xpath:string):Element {
        return this.find_one(kind_xpath, xpath)
    }

    // 按可见文字选一个元素；找不着返回空句柄。
    // en: Finds one element by its visible text; a null handle when there is none.
    // 例：
    // let one: Element = page.find_element_by_text("提交")
    method find_element_by_text(ref needle:string):Element {
        return this.find_one(kind_text, needle)
    }

    // 按 CSS 选出全部匹配的元素。
    // en: Finds every element the CSS selector matches.
    // 例：
    // console.print(length(page.find_elements_by_css("li")))
    method find_elements_by_css(ref selector:string):list<Element> {
        return this.find_many(kind_css, selector)
    }

    // 按 XPath 选出全部匹配的元素。
    // en: Finds every element the XPath matches.
    // 例：
    // console.print(length(page.find_elements_by_xpath("//li")))
    method find_elements_by_xpath(ref xpath:string):list<Element> {
        return this.find_many(kind_xpath, xpath)
    }

    // 按可见文字选出全部匹配的元素。
    // en: Finds every element whose visible text contains what you are looking for.
    // 例：
    // console.print(length(page.find_elements_by_text("提交")))
    method find_elements_by_text(ref needle:string):list<Element> {
        return this.find_many(kind_text, needle)
    }

    // 按 CSS 数有几个元素，只数不动。
    // en: Counts the elements a CSS selector matches, touching none.
    // 例：
    // console.print(page.count_css("li"))
    method count_css(ref selector:string):int {
        return count_in(this.index, kind_css, selector)
    }

    // 按 XPath 数有几个元素，只数不动。
    // en: Counts the elements an XPath matches, touching none.
    // 例：
    // console.print(page.count_xpath("//li"))
    method count_xpath(ref xpath:string):int {
        return count_in(this.index, kind_xpath, xpath)
    }

    // 按可见文字数有几个元素，只数不动。
    // en: Counts the elements whose visible text matches, touching none.
    // 例：
    // console.print(page.count_text("提交"))
    method count_text(ref needle:string):int {
        return count_in(this.index, kind_text, needle)
    }

    // 等到那个元素出现为止；等不到报 205。
    // en: Waits until that element shows up; 205 when it never does.
    // 例：
    // let one: Element = page.wait_for_element_by_css("#result", 8000)
    method wait_for_element_by_css(ref selector:string, timeout_ms:int = expect_ms):Element {
        return this.wait_for_one(kind_css, selector, timeout_ms)
    }

    // 等到那个元素出现为止（XPath）；等不到报 205。
    // en: Waits until that element shows up, matched by XPath; 205 when it never does.
    // 例：
    // let one: Element = page.wait_for_element_by_xpath("//div[@id='result']", 8000)
    method wait_for_element_by_xpath(ref xpath:string, timeout_ms:int = expect_ms):Element {
        return this.wait_for_one(kind_xpath, xpath, timeout_ms)
    }

    // 等到那个元素出现为止（可见文字）；等不到报 205。
    // en: Waits until that element shows up, matched by visible text; 205 when it never does.
    // 例：
    // let one: Element = page.wait_for_element_by_text("登录成功", 8000)
    method wait_for_element_by_text(ref needle:string, timeout_ms:int = expect_ms):Element {
        return this.wait_for_one(kind_text, needle, timeout_ms)
    }

    // —— 带定位的快捷：就是"找一下再做"，与元素上那一份同一实现 ——

    // 按 CSS 找到那个元素并点它；找不到当场报错。
    // en: Finds the element by CSS and clicks it; an error when it is not there.
    // 例：
    // page.click_css("#submit")
    method click_css(ref selector:string) {
        click_element(this.index, kind_css, selector, -1, 1)
    }

    // 按 XPath 找到那个元素并点它；找不到当场报错。
    // en: Finds the element by XPath and clicks it; an error when it is not there.
    // 例：
    // page.click_xpath("//button")
    method click_xpath(ref xpath:string) {
        click_element(this.index, kind_xpath, xpath, -1, 1)
    }

    // 按可见文字找到那个元素并点它；找不到当场报错。
    // en: Finds the element by visible text and clicks it; an error when it is not there.
    // 例：
    // page.click_text("提交")
    method click_text(ref needle:string) {
        click_element(this.index, kind_text, needle, -1, 1)
    }

    // 按 CSS 找到那个输入框并填进一段文字（走页面里的事件赋值那一条路）。
    // en: Finds the field by CSS and types into it, going through the page's own setter so framework state updates.
    // 例：
    // page.write_text_css("#name", "阿明")
    method write_text_css(ref selector:string, ref value:string) {
        run_js_in(this.index, write_text_script(find_expression(kind_css, selector, -1), value))
    }

    // 按 XPath 找到那个输入框并填进一段文字。
    // en: Finds the field by XPath and types into it the same way.
    // 例：
    // page.write_text_xpath("//input[@id='name']", "阿明")
    method write_text_xpath(ref xpath:string, ref value:string) {
        run_js_in(this.index, write_text_script(find_expression(kind_xpath, xpath, -1), value))
    }

    // 按可见文字找到那个输入框并填进一段文字。
    // en: Finds the field by visible text and types into it the same way.
    // 例：
    // page.write_text_text("用户名", "阿明")
    method write_text_text(ref needle:string, ref value:string) {
        run_js_in(this.index, write_text_script(find_expression(kind_text, needle, -1), value))
    }

    // 按 CSS 读出那个元素上的文字。
    // en: Reads the text of the element found by CSS.
    // 例：
    // console.print(page.read_text_css("h1"))
    method read_text_css(ref selector:string):string {
        return run_js_in(this.index, read_text_script(find_expression(kind_css, selector, -1)))
    }

    // 按 XPath 读出那个元素上的文字。
    // en: Reads the text of the element found by XPath.
    // 例：
    // console.print(page.read_text_xpath("//h1"))
    method read_text_xpath(ref xpath:string):string {
        return run_js_in(this.index, read_text_script(find_expression(kind_xpath, xpath, -1)))
    }

    // 按可见文字读出那个元素上的文字。
    // en: Reads the text of the element found by visible text.
    // 例：
    // console.print(page.read_text_text("标题"))
    method read_text_text(ref needle:string):string {
        return run_js_in(this.index, read_text_script(find_expression(kind_text, needle, -1)))
    }

    // 断言：按 CSS 找到的那个元素上的文字要等于给定值；到时限还不算过就报 206。
    // en: Asserts the text of the element found by CSS equals what is wanted; 206 once the timeout runs out.
    // 例：
    // page.expect_text_css("h1", "Example Domain", 4000)
    method expect_text_css(ref selector:string, ref want:string, timeout_ms:int = expect_ms) {
        expect_reading(this.index, read_text_script(find_expression(kind_css, selector, -1)), want, "按CSS「" + selector + "」上的字", timeout_ms)
    }

    // 断言：按 XPath 找到的那个元素上的文字要等于给定值；到时限还不算过就报 206。
    // en: Asserts the text of the element found by XPath equals what is wanted; 206 once the timeout runs out.
    // 例：
    // page.expect_text_xpath("//h1", "Example Domain", 4000)
    method expect_text_xpath(ref xpath:string, ref want:string, timeout_ms:int = expect_ms) {
        expect_reading(this.index, read_text_script(find_expression(kind_xpath, xpath, -1)), want, "按XPath「" + xpath + "」上的字", timeout_ms)
    }

    // 断言：按可见文字找到的那个元素上的文字要等于给定值；到时限还不算过就报 206。
    // en: Asserts the text of the element found by visible text equals what is wanted; 206 once the timeout runs out.
    // 例：
    // page.expect_text_text("标题", "Example Domain", 4000)
    method expect_text_text(ref needle:string, ref want:string, timeout_ms:int = expect_ms) {
        expect_reading(this.index, read_text_script(find_expression(kind_text, needle, -1)), want, "按可见文字「" + needle + "」上的字", timeout_ms)
    }

    // —— 页面本身 ——

    // 让这一页去那个网址；不等页面加载完成，要等就接一句 wait_until_ready。
    // en: Sends the page to that URL and returns; nothing is waited for, so follow it with wait_until_ready when the document has to be there.
    // 例：
    // page.navigate("https://example.com/")
    method navigate(ref url:string) {
        ask(this.index, "Page.navigate", "{\"url\":" + json_string_of(url) + "}")
    }

    // 重新加载这一页；同样不等加载完成。
    // en: Reloads the page, again without waiting for the document.
    // 例：
    // page.reload()
    method reload() {
        ask(this.index, "Page.reload", "{}")
    }

    // 关掉这一页那个标签页。页号不回收：后面那一页的号还是原来那一个，
    // 所以关完之后这一只 Page 不能再读任何东西（读它就是 208，浏览器说"这一页不在了"）。
    // en: Closes this page's tab. Page numbers are not recycled, so the handle of a later page keeps working while this one stops reading anything (208).
    // 例：
    // page.close()
    method close() {
        ask(this.index, "Page.close", "{}")
    }

    // 等浏览器主动发来的通知（名字原样写，如 `Page.loadEventFired`），返回那条原文。
    // 带 sessionId 的通知只算这一页的；等不到报 209。
    // en: Waits for one notification the browser sends on its own, named exactly as on the wire, and gives back its text; notifications carrying this page's sessionId count, others do not. 209 when none arrives.
    // 例：
    // console.print(page.wait_event("Page.loadEventFired", 6000))
    method wait_event(ref name:string, timeout_ms:int = expect_ms):string {
        return wait_event_in(this.index, name, timeout_ms)
    }

    // 把这一页截成一张 png 写到那个路径上。
    // en: Captures the page as a png and writes it to that path.
    // 例：
    // page.screenshot("out/scene.png")
    method screenshot(ref file_path:string) {
        let answer:string = ask(this.index, "Page.captureScreenshot", "{\"format\":\"png\"}")
        file.write_bytes(file_path, png_from_reply_of(answer))
    }

    // 读页面标题。
    // en: Reads the document title.
    // 例：
    // console.print(page.read_title())
    method read_title():string {
        return run_js_in(this.index, title_script())
    }

    // 读页面现在的网址。
    // en: Reads the URL the page is on now.
    // 例：
    // console.print(page.read_url())
    method read_url():string {
        return run_js_in(this.index, url_script())
    }

    // 读整页可见文字（body 的 innerText）。
    // en: Reads the visible text of the whole page, which is body.innerText.
    // 例：
    // console.print(length(page.read_body_text()))
    method read_body_text():string {
        return run_js_in(this.index, body_text_script())
    }

    // 按一个键：名字写 Enter、Tab、Escape、Backspace 与单个字母这一类。
    // en: Presses one key: Enter, Tab, Escape, Backspace, a single letter and the like.
    // 例：
    // page.press_key("Enter")
    method press_key(ref key_name:string) {
        key_by_name(this.index, key_name)
    }

    // 在页面坐标上按下再抬起，算一次点击。元素不在的时候才用这一条。
    // en: Presses and releases at a page coordinate, counting as one click. Reach for this only when there is no element to click.
    // 例：
    // page.click_at(120, 60)
    method click_at(x:int, y:int) {
        mouse_at(this.index, "mousePressed", x, y, 1)
        mouse_at(this.index, "mouseReleased", x, y, 1)
    }

    // 导航之后等的一等：这一页报成 complete 才算到位。等不到报 207。
    // en: The wait after a navigation: the document has to report complete, or 207.
    // 例：
    // page.wait_until_ready(8000)
    method wait_until_ready(timeout_ms:int = expect_ms) {
        let waited:int = 0
        while (true) {
            if (run_js_in(this.index, ready_script()) == "complete") {
                return
            }
            if (waited >= timeout_ms) {
                throw (code_page_not_ready, "页面没到位：等 " + number.to_string(timeout_ms) + " 毫秒，readyState 还不是 complete")
            }
            time.sleep(wait_step_ms)
            waited = waited + wait_step_ms
        }
    }

    // 开一页之后等的这一步，比上面那一条多看一件事：那份文档得是"已经开始加载"的那一份。
    // Target.createTarget 带着网址时先附上的是那张 about:blank，它的 readyState 一开始就是
    // complete（实测：那一刻读标题读到的是空白页的，返回空）。
    // 要的就是 about:blank 那一张时，这一条与上面那一条一致。
    // en: The wait after opening a page. It looks at one thing more than wait_until_ready: the document has to be the one that started moving, not the blank page still attached when createTarget carries a URL.
    // 例：
    // page.settle_on("https://example.com/", 8000)
    method settle_on(ref want:string, timeout_ms:int) {
        let waited:int = 0
        while (true) {
            let state:string = run_js_in(this.index, ready_script())
            let here:string = run_js_in(this.index, url_script())
            let blank:boolean = here != null && here == "about:blank" && want != "about:blank"
            if (state == "complete" && !blank) {
                return
            }
            if (waited >= timeout_ms) {
                throw (code_page_not_ready, "页面没到位：等 " + number.to_string(timeout_ms) + " 毫秒，那一份文档是「" + first_chars_of(here + "|", 60) + "」，状态「" + first_chars_of(state + "|", 30) + "」")
            }
            time.sleep(wait_step_ms)
            waited = waited + wait_step_ms
        }
    }

    // 断言：页面标题要等于给定值；到时限还不算过就报 206。
    // en: Asserts the document title equals what is wanted; 206 once the timeout runs out.
    // 例：
    // page.expect_title("Example Domain", 4000)
    method expect_title(ref want:string, timeout_ms:int = expect_ms) {
        expect_reading(this.index, title_script(), want, "页面标题", timeout_ms)
    }

    // 断言：页面网址要等于给定值；到时限还不算过就报 206。
    // en: Asserts the page URL equals what is wanted; 206 once the timeout runs out.
    // 例：
    // page.expect_url("https://example.com/", 4000)
    method expect_url(ref want:string, timeout_ms:int = expect_ms) {
        expect_reading(this.index, url_script(), want, "页面网址", timeout_ms)
    }

    // 网址里含那一段就算过（跳转之后带查询串的那种，整串相等做不到）。
    // en: The URL only has to contain that part, which is what a redirect with a query string needs.
    // 例：
    // page.expect_url_contains("iana.org", 6000)
    method expect_url_contains(ref part:string, timeout_ms:int = expect_ms) {
        let waited:int = 0
        while (true) {
            if ((this.read_url()).contains(part)) {
                return
            }
            if (waited >= timeout_ms) {
                throw (code_assert_failed, "断言不通过：页面网址是「" + first_chars_of(this.read_url() + "|", 60) + "」，要含的是「" + part + "」")
            }
            time.sleep(wait_step_ms)
            waited = waited + wait_step_ms
        }
    }

    // 三种定位方式共用的一步：按那一种取法数一下，有就交回第一个位置的把手，没有就交回空句柄。
    // en: The step the three finders share: ask the page how many match, then give back a handle for the first one, or a null handle.
    // 例：
    // let one: Element = page.find_one(1, "h1")
    method find_one(kind:int, ref selector:string):Element {
        if (present(this.index, kind, selector, -1)) {
            return new Element(page_index: this.index, kind: kind, selector: selector + "", position: -1)
        }
        return null
    }

    // 三种定位方式共用的一步：按那一种取法数几个，然后把每一个位置的把手装进列表。
    // en: The step the three plural finders share: count the matches, then put a handle for each position into a list.
    // 例：
    // console.print(length(page.find_many(1, "li")))
    method find_many(kind:int, ref selector:string):list<Element> {
        let out:list<Element> = []
        let total:int = count_in(this.index, kind, selector)
        for (i:int in 0 until total) {
            out.add(new Element(page_index: this.index, kind: kind, selector: selector + "", position: i))
        }
        return out
    }

    // 等到那一个元素出现为止；每等一步问一次，到时限报 205。
    // en: The step the three waiters share: ask once per step until the element shows up, and throw 205 at the deadline.
    // 例：
    // let one: Element = page.wait_for_one(1, "h1", 6000)
    method wait_for_one(kind:int, ref selector:string, timeout_ms:int):Element {
        if (!wait_present(this.index, kind, selector, timeout_ms)) {
            throw (code_element_missing, "等不到元素：按" + describe_kind(kind) + "「" + selector + "」等了 " + number.to_string(timeout_ms) + " 毫秒")
        }
        return new Element(page_index: this.index, kind: kind, selector: selector + "", position: -1)
    }
}

// 页面上的一个元素。它记着"哪一页、按哪种定位、找什么、第几个"，每次使用都重新定位：
// 页面一直在变，沿用上一次的节点会读到已经不存在的东西。
// en: One element on a page. It remembers which page, which kind of locator, what to look for and which occurrence, and finds it again every time it is used - a page keeps changing, and holding on to last round's node would read something that no longer exists.
// 例：
// let box: wt.Element = page.find_element_by_css("input[name=user]")
export class Element {
    page_index:int
    kind:int
    selector:string
    position:int

    // 点这个元素：先把它滚到视口中间，再按它的边框中心点。
    // en: Clicks the element: it is scrolled to the middle of the viewport first, then pressed at the centre of its box.
    // 例：
    // element.click()
    method click() {
        click_element(this.page_index, this.kind, this.selector, this.position, 1)
    }

    // 双击这个元素。
    // en: Double-clicks the element.
    // 例：
    // element.double_click()
    method double_click() {
        click_element(this.page_index, this.kind, this.selector, this.position, 2)
    }

    // 把鼠标移到这个元素上，不按键。
    // en: Moves the mouse onto the element without pressing a button.
    // 例：
    // element.hover()
    method hover() {
        // 指针先移到左上角再挪过去：mouseenter 只在从别处进入的那一刻发出。
        // 实测：先在 #box 上双击、再把指针移到紧邻其下的 #off，页面收不到 mouseenter；
        // 中间补一次移到左上角，就收得到。
        let sel:string = this.selector + ""
        let where:string = center_of(this.page_index, this.kind, sel, this.position)
        if (where == null) {
            throw (code_element_missing, "悬停不了：按" + describe_kind(this.kind) + "「" + sel + "」找不到元素")
        }
        let parts:list<string> = where.split(",")
        mouse_move(this.page_index, 1, 1)
        mouse_move(this.page_index, text.to_int(parts[0]), text.to_int(parts[1]))
    }

    // 把这个元素滚到视口中间。
    // en: Scrolls the element to the middle of the viewport.
    // 例：
    // element.scroll_into_view()
    method scroll_into_view() {
        run_js_in(this.page_index, scroll_script(this.expression()))
    }

    // 在这个元素上填一段文字（走它自己的 setter，页面框架的状态会跟着更新）。
    // en: Types into this element through its own setter, so framework state follows the new value.
    // 例：
    // element.write_text("阿明")
    method write_text(ref value:string) {
        run_js_in(this.page_index, write_text_script(this.expression(), value))
    }

    // 读这个元素上的文字。
    // en: Reads the text on this element.
    // 例：
    // console.print(element.read_text())
    method read_text():string {
        return run_js_in(this.page_index, read_text_script(this.expression()))
    }

    // 读这个元素的一个属性；没有那个属性时返回空文本。
    // en: Reads one attribute of the element; an empty text when it does not have one.
    // 例：
    // console.print(element.read_attribute("href"))
    method read_attribute(ref name:string):string {
        return run_js_in(this.page_index, attribute_script(this.expression(), name))
    }

    // 这个元素在页面上看不看得见。
    // en: Whether the element is visible on the page.
    // 例：
    // console.print(element.is_visible())
    method is_visible():boolean {
        return run_js_in(this.page_index, visible_script(this.expression())) == "true"
    }

    // 这个元素能不能用（没被 disabled）。
    // en: Whether the element is usable, that is, not disabled.
    // 例：
    // console.print(element.is_enabled())
    method is_enabled():boolean {
        return run_js_in(this.page_index, enabled_script(this.expression())) == "true"
    }

    // 它在页面上的方框：返回左、上、宽、高四个数；找不着返回空的。
    // en: Reads the element's box as one text: "x,y,w,h", in page coordinates.
    // 例：
    // console.print(element.read_box())
    method read_box():list<float> {
        let out:list<float> = []
        let box:string = run_js_in(this.page_index, box_script(this.expression()))
        if (box != null) {
            let parts:list<string> = box.split(",")
            for (i:int in 0 until length(parts)) {
                out.add(text.to_float(parts[i]))
            }
        }
        return out
    }

    // 断言：这个元素上的文字要等于给定值；到时限还不算过就报 206。
    // en: Asserts the element's text equals what is wanted; 206 once the timeout runs out.
    // 例：
    // element.expect_text("提交", 4000)
    method expect_text(ref want:string, timeout_ms:int = expect_ms) {
        expect_reading(this.page_index, read_text_script(this.expression()), want, "按" + describe_kind(this.kind) + "「" + this.selector + "」上的字", timeout_ms)
    }

    // 断言：这个元素要看得见；到时限还不算过就报 206。
    // en: Asserts the element is visible; 206 once the timeout runs out.
    // 例：
    // element.expect_visible(4000)
    method expect_visible(timeout_ms:int = expect_ms) {
        expect_reading(this.page_index, visible_script(this.expression()), "true", "按" + describe_kind(this.kind) + "「" + this.selector + "」是否看得见", timeout_ms)
    }

    // 断言：这个元素要从页面上消失；到时限还在就报 206。
    // en: Asserts the element disappears; 206 if it is still there at the deadline.
    // 例：
    // element.expect_gone(4000)
    method expect_gone(timeout_ms:int = expect_ms) {
        let waited:int = 0
        while (true) {
            if (!present(this.page_index, this.kind, this.selector, this.position)) {
                return
            }
            if (waited >= timeout_ms) {
                throw (code_assert_failed, "断言不通过：按" + describe_kind(this.kind) + "「" + this.selector + "」等 " + number.to_string(timeout_ms) + " 毫秒还在页上")
            }
            time.sleep(wait_step_ms)
            waited = waited + wait_step_ms
        }
    }

    // 把这一个元素的位置写成页面里那段 JS 的取法，元素身上那几条与页面上带定位的快捷共用它。
    // en: Writes this element's position as the selector expression used inside the page's script. The members on the element and the shortcuts on the page both end up here.
    // 例：
    // console.print(element.expression())
    method expression():string {
        return find_expression(this.kind, this.selector, this.position)
    }
}

// —— 门面交出的那几个自由函数 ——

// 起一个 headless 浏览器、连上、握完手。port_wait 是等那个端口文件多久，io_wait 是
// 连上与收发每一次等多久——两个数都由调用的人给。
// en: Starts a headless browser, connects and completes the handshake. port_wait is how long to wait for that port file and io_wait how long each connect, send and receive may take - both numbers come from the caller.
// 例：
// let launched: wt.Browser = wt.start("msedge.exe", "profile")
export method start(ref exe:string, ref profile:string, port_wait:int = port_ms, io_wait:int = io_ms):Browser {
    file.create_directories(profile)
    let mark:string = profile + "/DevToolsActivePort"
    // 上一趟留下的端口文件要先删掉：等的是"这一趟浏览器写下哪个端口"，
    // 拿旧的端口去连，等到的是系统错误号 10061（连不上），报的还不是端口这一件事。
    if (file.exists(mark)) {
        file.remove(mark)
    }
    let line:string = "\"" + exe + "\" --headless=new --no-first-run --no-default-browser-check"
    line = line + " --remote-debugging-port=0 --user-data-dir=\"" + profile + "\" about:blank"
    link.proc = process.start(line)
    link.pending = bytes.new(0)
    link.held = bytes.new(0)
    link.sessions = []
    link.events = {}
    // 这一段任何一处不成都要收掉那个进程再往外报：包内状态不会自动收尾
    // （实测：不这样做时，握手被拒后浏览器与它的 9 个子进程都会残留）。
    try {
        wait_port(mark, port_wait)
        link.sock = net.connect("127.0.0.1", link.port, io_wait)
        handshake()
    } catch (code, msg) {
        if (link.sock != null) {
            link.sock.close()
            link.sock = null
        }
        if (link.proc != null) {
            link.proc.close()
            link.proc = null
        }
        throw (code, msg)
    }
    return new Browser()
}

// 上面那一层要浏览器句柄时从这一条取，不必一直持有 `start` 返回的那一只：`Browser` 里
// 一个字段也没有，连接与进程都握在包内那一份状态里。这一条也正因为
// "门面只能导出这个包自己定义的类型"才必要——上面那一层的字段里不能放 `wt.Browser`。
// en: The layer above takes its browser handle from this member instead of holding on to the one `start` returned: `Browser` has no fields at all, and the connection and the process are both held in the package's state. This member exists precisely because a facade may only export types the package defines itself, so the layer above cannot keep a `wt.Browser` in a field.
// 例：
// let held: wt.Browser = wt.current_browser()
export method current_browser():Browser {
    return new Browser()
}

// 按页号取回一只 Page。`Page` 里只有那一个页号，所以同一页取几次都行，两只同时持有
// 也不违反"一个值只有一个持有者"（各是各的句柄，指向浏览器里同一个标签页）。
// 给上面那一层用的：它自己掌握页的开与关，脚本那边拿到的是一只新句柄。
// en: Takes a Page out by its page number. A `Page` holds nothing but that number, so the same page may be taken any number of times and two handles held together do not break "one value, one holder" (each is its own handle pointing at the same browser tab). This is for the layer above: it keeps opening and closing pages itself, while the script is handed a fresh handle.
// 例：
// let taken: wt.Page = wt.page_at(0)
export method page_at(index:int):Page {
    return new Page(index: index)
}

// 把一只已经连上的套接字当成连接用：自测里自己 listen、自己 connect，不开浏览器。
// en: Treats an already connected socket as the connection: the package's self-test listens and connects by itself, without starting a browser.
// 例：
// let mine: wt.Browser = wt.wrap_socket(net.connect("127.0.0.1", 9222, 1000))
export method wrap_socket(sock:Socket):Browser {
    link.sock = sock
    link.pending = bytes.new(0)
    link.held = bytes.new(0)
    link.sessions = []
    link.events = {}
    return new Browser()
}

// 等浏览器把真端口写进那个文件：第一行是端口、第二行是 WebSocket 路径
// （实测：两个浏览器都在启动进程后 500 毫秒内写下这个文件）。
method wait_port(ref mark:string, port_wait:int) {
    let waited:int = 0
    while (!file.exists(mark)) {
        time.sleep(20)
        waited = waited + 20
        if (waited > port_wait) {
            throw (code_port_timeout, "等不到浏览器写下那个端口：" + mark)
        }
    }
    let lines:list<string> = file.read(mark).split("\n")
    if (length(lines) < 2) {
        throw (code_port_timeout, "那个端口文件不像话：" + mark)
    }
    link.port = text.to_int(lines[0].trim())
    link.ws_path = lines[1].trim()
}

// 握手发出去之后读到第一个空行为止；其后可能已经跟上一帧，剩下的字节交给读帧那一步。
// 不带 Origin：Chrome 从 111 起校验来源，带上反而被拒绝（实测：带 Origin 回 403）。
// 握手段是裸的 HTTP 文本，不能当成一帧送（升级之前还没有帧）：直接写字节。
method handshake() {
    // 路径从 link 里读出来先接给一个名字：字段直接交给别的函数会走过户，那个位置当场就空了
    let route:string = link.ws_path
    send_all(build_handshake(link.port, route).to_bytes())
    let at:int = find_header_end()
    while (at < 0) {
        pull_into()
        at = find_header_end()
    }
    let head_buf:bytes = link.pending
    let head_bytes:bytes = head_buf.slice(0, at)
    link.pending = head_buf.slice(at, length(head_buf))
    let head:string = text.from_bytes(head_bytes)
    if (!head.starts_with("HTTP/1.1 101")) {
        throw (code_handshake_failed, "浏览器不应这次握手：" + first_chars_of(head, 60))
    }
}

method find_header_end():int {
    let buf:bytes = link.pending
    let n:int = length(buf)
    let at:int = -1
    for (i:int in 0 until n - 3) {
        let head_ok:boolean = (buf[i] as int) == 13 && (buf[i + 1] as int) == 10
        let tail_ok:boolean = (buf[i + 2] as int) == 13 && (buf[i + 3] as int) == 10
        if (head_ok && tail_ok) {
            at = i + 4
            break
        }
    }
    // 上面那句 `let buf:bytes = link.pending` 是把字段里的值交出去，
    // 位置当场空了，所以读完必须写回去——不论后面是否还用到，交出就是交出。
    link.pending = buf
    return at
}

// 下面这几个把 frame.nf 与 wire.nf 里的东西原样交出去，给自测与上一层用。

// 把一段正文按这一把掩码编成一整条 WebSocket 帧（掩码与正文都在参数里）。
// en: Encodes one body into a whole WebSocket frame with this mask key; the key and the body both come in as parameters.
// 例：
// let packed: bytes = wt.build_frame("甲", block)
export method build_frame(ref body:string, ref key:bytes):bytes {
    return encode_text(body.to_bytes(), key)
}

// 从这一串字节里读出最前面那一条帧；不够一条就交回空句柄，读掉的那一段从缓冲里去掉。
// en: Reads the frontmost frame out of this run of bytes, or a null handle when it is not a whole frame yet; the part consumed leaves the buffer.
// 例：
// let got: wt.Frame = wt.read_frame(block)
export method read_frame(ref buf:bytes):Frame {
    return take_wire(buf)
}

// 握手那一段文本，交给浏览器之外的裸套接字送出去（不带 Origin，Chrome 从 111 起查来源）。
// en: The handshake text, sent over the bare socket rather than through the browser: it carries no Origin, since Chrome checks the source from version 111 on.
// 例：
// let head:string = wt.handshake_text(9222, "/devtools/page/1")
export method handshake_text(port:int, ref path_value:string):string {
    return build_handshake(port, path_value)
}

// 从一条答复里取 result 下面那一个位置，取不到交回空句柄。
// en: Takes one slot under result out of a reply, or a null handle when there is none.
// 例：
// let field:string = wt.reply_field(answer, "id")
export method reply_field(ref answer:string, ref key:string):string {
    return reply_field_of(answer, key)
}

// 把答复里那一段 base64 的截图解成 PNG 字节，交回一整块。
// en: Decodes the base64 screenshot carried in a reply into a whole PNG block.
// 例：
// let picture: bytes = wt.png_from_reply(answer)
export method png_from_reply(ref answer:string):bytes {
    return png_from_reply_of(answer)
}

// 取头 count 个字符，不足那么多就整个返回——报错的那一句要贴出原文，而原文常常本来短。
// en: Takes the first count characters, or the whole text when it is already shorter: an error message quotes what it saw, and what it saw is often short.
// 例：
// let brief:string = wt.first_chars(answer, 60)
export method first_chars(ref value:string, count:int):string {
    return first_chars_of(value, count)
}

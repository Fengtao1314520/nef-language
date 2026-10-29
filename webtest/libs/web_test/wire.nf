// 只处理字符串：握手文本、从答复里取字段、把一段文本截短、把定位与动作拼成一段浏览器
// 可执行的 JS。
//
// 这一份不引用门面里的任何一个类：包内文件不许互相引用（实测编译器报
// "libs/web_test/cyc.nf 与 ./widget.nf 互相引用"），所以这里只有字符串进、字符串出。
//
// 写这一份时守住两条：一个表达式不能跨行；下面的参数只读不改、也不留下那份值，
// 所以一律标 `ref`——调用方把名字交进来，交完那个名字仍归调用方使用。

// 握手那一段文本。不带 Origin，也不带别的：Chrome 从 111 起校验来源，带上反而被拒绝
// （实测：带 Origin 回 403 Forbidden，不带回 101）。
// en: The handshake text, with no Origin and nothing else: Chrome checks the source from version 111 on, so sending one is what gets the request refused (measured: with Origin 403, without it 101).
export method build_handshake(port:int, ref ws_path:string):string {
    let crlf:string = "\u000D\u000A"
    let req:string = "GET " + ws_path + " HTTP/1.1" + crlf
    req = req + "Host: 127.0.0.1:" + number.to_string(port) + crlf
    req = req + "Upgrade: websocket" + crlf
    req = req + "Connection: Upgrade" + crlf
    req = req + "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" + crlf
    req = req + "Sec-WebSocket-Version: 13" + crlf + crlf
    return req
}

// 写成一段 JSON 字符串：两头带引号，中间交给 text.escape_json。JS 里那一段也用这一种
// （JSON 的字符串写法是 JS 的子集）。
// en: Writes a text as a JSON string: quotes on both ends and text.escape_json in between. The JS we evaluate uses the same form, since JSON string syntax is a subset of JS.
export method json_string_of(ref value:string):string {
    return "\"" + text.escape_json(value) + "\""
}

method js_string(ref value:string):string {
    return json_string_of(value)
}

// 取头 count 个字符，不足那么多就整个返回。语言的 slice 拒绝止点大于长度
// （越界是 10031 号），而报错那句要带上的答复常常本来就是短的。
// en: Takes the first count characters, or the whole text when it is shorter. The language's slice refuses an end past the length (10031), while the reply quoted in an error message is often short to begin with.
export method first_chars_of(ref value:string, count:int):string {
    let n:int = length(value)
    if (count < n) {
        n = count
    }
    return value.slice(0, n)
}

// 从一条答复里取 result 下面的字段，取不到返回空句柄。sessionId、targetId、截图那一段
// base64 都在这一层下面（实测：读到消息顶层的位置会得到空，后面每条命令都被当成
// 浏览器层的，答复是 -32601 'Page.navigate' wasn't found）。
// en: Reads one slot under result in a reply, or a null handle. sessionId, targetId and the screenshot's base64 all live at that level; reading the slot at the top of the message instead gives nothing and every command is then treated as browser-level.
export method reply_field_of(ref answer:string, ref key:string):string {
    let doc:Json = json.parse(answer)
    if (doc == null) {
        throw (204, "答复不是一段 JSON：" + first_chars_of(answer, 60))
    }
    let result:Json = json.at(doc, "result")
    if (result == null) {
        return null
    }
    let slot:Json = json.at(result, key)
    if (slot == null) {
        return null
    }
    // 取到的是数或真假时 json.to_text 也返回空句柄；库里那一串
    // sessionId、targetId、base64 全是文本，所以这一条够。
    return json.to_text(slot)
}

// 把一条截图答复里那串 base64 解码成 png 的字节。解不开是语言的 10014 号，
// 答复里没有那个字段是 204 号。
// en: Turns the base64 inside a screenshot reply into png bytes. Base64 that will not decode is the language's 10014; a reply without that slot is 204.
export method png_from_reply_of(ref answer:string):bytes {
    let data:string = reply_field_of(answer, "data")
    if (data == null) {
        throw (204, "答复里没有 data 那一段：" + first_chars_of(answer, 80))
    }
    return bytes.from_base64(data)
}

// 三种定位的号。库内部用它分头拼 JS，脚本里看不到这三个数（成员名各写各的）。
const kind_css_local:int = 1
const kind_xpath:int = 2
const kind_text:int = 3

// XPath 的表达式：归一化空白、位置从 0 起（负数取第一个命中）。
method xpath_expression(ref xpath:string, position:int):string {
    let head:string = "document.evaluate(" + js_string(xpath) + ", document, null, "
    if (position < 0) {
        return "(" + head + "XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue)"
    }
    return "(" + head + "XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null).snapshotItem(" + number.to_string(position) + "))"
}

// 按可见文字找：先归一化首尾与中间多余的空白，再看是否包含那段文字。
// 后面的 not(.//*) 只留最里层的那一个：整张页面从 html 到 body 都"包含"那段文字，
// 不留最里层就会定位到 <html> 上（与 Playwright 的 text= 判据相同：按子串匹配，不要求整段相等）。
method text_expression(ref needle:string, position:int):string {
    let quoted:string = js_string(needle)
    let innermost:string = "//*[contains(normalize-space(.)," + quoted + ")][not(.//*[contains(normalize-space(.)," + quoted + ")])]"
    if (position >= 0) {
        return xpath_expression(innermost, position)
    }
    return xpath_expression(innermost + "[1]", -1)
}

// 一个元素在 JS 里的写法。kind 用上面那三个号；position 负数取第一个命中，
// 非负数是复数结果里的第几个（find_elements_by_css 返回的那些就是这么定位自己的）。
// en: How one element is written inside the JS. The kind is one of the three numbers above; a negative position means the first hit and a non-negative one means the n-th of a plural result, which is how the elements returned by find_elements_by_css locate themselves.
export method find_expression(kind:int, ref target:string, position:int):string {
    if (kind == kind_xpath) {
        return xpath_expression(target, position)
    }
    if (kind == kind_text) {
        return text_expression(target, position)
    }
    let quoted:string = js_string(target)
    if (position < 0) {
        return "(document.querySelector(" + quoted + "))"
    }
    return "(document.querySelectorAll(" + quoted + ")[" + number.to_string(position) + "])"
}

// 多少个元素：三种定位各一条。返回的是文本，因为 json.to_text 只答文本，
// 数与真假那两种它返回空句柄。
// en: How many elements there are, one expression per locating kind. The answer comes back as text because json.to_text only answers for text; a number or a boolean would give it a null handle.
export method count_expression(kind:int, ref target:string):string {
    let quoted:string = js_string(target)
    if (kind == kind_css_local) {
        return "String(document.querySelectorAll(" + quoted + ").length)"
    }
    if (kind == kind_xpath) {
        return "String(document.evaluate(" + quoted + ", document, null, XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null).snapshotLength)"
    }
    let walked:string = "//*[contains(normalize-space(.)," + quoted + ")][not(.//*[contains(normalize-space(.)," + quoted + ")])]"
    return "String(document.evaluate(" + js_string(walked) + ", document, null, XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null).snapshotLength)"
}

// 把一段动作接在一个元素后面：元素先找出来，动作里统一用 el 指它（找不到就是 undefined，
// 库那边返回空句柄）。
// en: Appends an action onto an element lookup: the element is found first and the action body refers to it as el, so a missing element reads as undefined and the library turns that into a null handle.
export method page_script(ref find:string, ref body:string):string {
    return "(function(el){" + body + "})(" + find + ")"
}

// 读那段文字：可输入的三种元素（input、textarea、select）返回填入的值，别的元素返回页面里的文字。
// 这里判的是标签名，不是"有没有 value 字段"：`<li>` 也有 value（HTMLLIElement 上那是个数字，
// 未设置时是 0），用 `el.value===undefined` 判会读成 "0"（实测）。
// en: The script that reads an element's text: the three input-capable tags answer with their value, any other element with its text. The test is on the tag name, not on whether a value field exists, because <li> also carries a value (a number that reads back as "0").
export method read_text_script(ref find:string):string {
    return page_script(find, "if(!el){return undefined}var t=el.tagName;if(t===\"INPUT\"||t===\"TEXTAREA\"||t===\"SELECT\"){return String(el.value)}return String(el.textContent)")
}

// 写字：可输入的三种元素先清空、赋值，再派发 input 与 change 事件，页面绑定的监听收得到；
// 别的元素改的是其中的文字。
// en: The script that types into an element through its own setter and then dispatches input and change, so a page built on framework state updates as well.
export method write_text_script(ref find:string, ref value:string):string {
    return page_script(find, "if(!el){return undefined}var v=" + js_string(value) + ";var t=el.tagName;if(t!==\"INPUT\"&&t!==\"TEXTAREA\"&&t!==\"SELECT\"){el.textContent=v;return v}el.value=\"\";el.value=v;el.dispatchEvent(new Event(\"input\",{bubbles:true}));el.dispatchEvent(new Event(\"change\",{bubbles:true}));return String(el.value)")
}

// 元素在页面上的方框，写成"左,上,宽,高"四个数用逗号隔开的一段文本。
// en: The script that measures an element's border box and answers with one text, "x,y,w,h", in page coordinates.
export method box_script(ref find:string):string {
    return page_script(find, "if(!el){return undefined}var r=el.getBoundingClientRect();return [r.left,r.top,r.width,r.height].join(\",\")")
}

// 元素中心在页面上的位置，写成"横,纵"两个整数用逗号隔开的一段文本。
// 取整在 JS 里做：鼠标位置本来就以像素为单位，语言那边不必再处理小数。
// 先滚到中间再量：无头浏览器的视口只有 800×600（实测——example.com
// 那一页的链接在上边 657 像素那一行，落在视口之外，鼠标事件送过去不生效）。
// 只有鼠标相关的几条调用走这一句，所以"先滚动"只发生在点击、双击、悬停之前；读方框那一条不动页面。
// en: The script that scrolls an element to the middle of the viewport and answers with the coordinates of its box centre, measured after that scroll.
export method center_script(ref find:string):string {
    return page_script(find, "if(!el){return undefined}el.scrollIntoView({block:'center',inline:'center'});var r=el.getBoundingClientRect();return String(Math.round(r.left+r.width/2))+\",\"+String(Math.round(r.top+r.height/2))")
}

// 在不在页面上（不管看不看得见）：返回 "1" 或 "0"。
// en: The script that asks whether an element is present.
export method exists_script(ref find:string):string {
    return page_script(find, "return el?\"1\":\"0\"")
}

// 看不看得见：有画出来的方框就算看得见。返回 "true"/"false"。
// en: The script that asks whether an element is visible: it has to have a box and must not be hidden, collapsed or transparent.
export method visible_script(ref find:string):string {
    return page_script(find, "if(!el){return \"false\"}return String(el.getClientRects().length>0)")
}

// 能不能用：带 disabled 的那一种返回 "false"。
// en: The script that asks whether an element can be used, counting both the element's own disabled attribute and a disabled fieldset.
export method enabled_script(ref find:string):string {
    return page_script(find, "if(!el){return \"false\"}return String(!el.disabled)")
}

// 滚到看得见的位置。
// en: The script that scrolls one element to the middle of the viewport.
export method scroll_script(ref find:string):string {
    return page_script(find, "if(!el){return undefined}el.scrollIntoView();return undefined")
}

// 读一个属性。元素没有那个属性时页面里返回 undefined，答复里就没有 `value` 字段，
// 库这一侧返回空句柄（脚本里要先判 `!= null` 再拿去比，拿空句柄去比是运行期 10092）。
// en: The script that reads one attribute of an element; an attribute the element does not have reads back as an empty text.
export method attribute_script(ref find:string, ref name:string):string {
    return page_script(find, "if(!el){return undefined}var v=el.getAttribute(" + js_string(name) + ");return v===null?undefined:String(v)")
}

// 页面标题、网址、整页文字。
// en: The script that reads the document title.
export method title_script():string {
    return "String(document.title)"
}

// en: The script that reads the URL the document is on.
export method url_script():string {
    return "String(location.href)"
}

// 这一页自己报到哪一步了："loading"／"interactive"／"complete"。
// en: The script that reads readyState, which is what every wait in this library polls.
export method ready_script():string {
    return "String(document.readyState)"
}

// en: The script that reads the visible text of the whole page.
export method body_text_script():string {
    return "String(document.body?document.body.innerText:\"\")"
}

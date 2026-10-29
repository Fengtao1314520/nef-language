// web_test 上面那一层（定位、动作、断言、等待）的实际运行检查：起一个浏览器、开两页本地页面，
// 把库交出的成员几乎每一个都走一遍（`page_at` 与 `current_browser` 是给上面那一层用的，
// 由 `TestWebTestRunCheck` 那一趟覆盖）。
//
// 跑法（在仓库根，三个参数：浏览器路径、一个空的个人资料目录、fixtures 目录）：
//
//	./nf.exe webtest/selftest/element_check.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" /临时/profile <仓库目录>/webtest/fixtures
//
// 那两份页面写在 webtest/fixtures/ 里，是这一项检查需要的形状：表单那一页有登录、有一条半秒后自己
// 消失的提示、有一块 400 毫秒后才出来的东西；列表那一页有三行、两个链接、一个看不见的块、
// 一个不能用的按钮、一个 120×40 的方框、一块远在两千像素之下的东西。
//
// 每一条检查打印一行"过  …"，全过最后印"全对"、退出码 0；有一处不过把对不上的那一处打印出来，
// 退出码 1。

import wt from "libs/web_test"

method check_text(name:string, got:string, want:string):boolean {
    // 库里读出来的那段文本可能是空句柄（没有的那个属性、找不到的那个元素），而文本比较要读内容：
    // 拿空句柄去比就是运行期的 10092。所以先判空句柄。
    if (got == null) {
        console.print("不对  " + name, "读到的是空句柄，要的是「" + want + "」")
        return false
    }
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
let exe:string = args[0]
let profile:string = args[1]
let fixdir:string = args[2]

let browser:wt.Browser = wt.start(exe, profile, 8000, 5000)
let shot:string = profile + "/element.png"

// —— 表单那一页 ——
let form:wt.Page = browser.open_page("file:///" + fixdir + "/form.html")

let ok1:boolean = check_text("一、read_title 读出表单页的标题", form.read_title(), "登录")
if (!ok1) {
    bad = bad + 1
}
let ok2:boolean = check_true("二、read_url 里带着那一份页面的名字", form.read_url().contains("form.html"))
if (!ok2) {
    bad = bad + 1
}
let ok3:boolean = check_true("三、read_body_text 里有页面上的字", form.read_body_text().contains("登录"))
if (!ok3) {
    bad = bad + 1
}

// 四～六：定位返回一个元素，元素自己写字、自己读字
let name_el:wt.Element = form.find_element_by_css("#name")
let ok4:boolean = check_true("四、find_element_by_css 交回一个元素", name_el != null)
if (!ok4) {
    bad = bad + 1
}
name_el.write_text("nef")
let ok5:boolean = check_text("五、元素上 write_text 之后 read_text 读得回", name_el.read_text(), "nef")
if (!ok5) {
    bad = bad + 1
}
let ok6:boolean = check_text("六、Page 上那份同名快捷读到的与元素一样", form.read_text_css("#name"), "nef")
if (!ok6) {
    bad = bad + 1
}

// 七：找不着就是空句柄，不报错
let ok7:boolean = check_true("七、找不到的那一个交回空句柄", form.find_element_by_css("#没有这一个") == null)
if (!ok7) {
    bad = bad + 1
}

// 八～十：密码框走 Page 上那份快捷，点登录走元素，断言走元素上带等待的那一条
form.write_text_css("#pass", "1234")
form.find_element_by_css("#submit").click()
let tip_el:wt.Element = form.find_element_by_css("#tip")
let ok8:boolean = check_text("八、点元素真的触发了页面里的监听", tip_el.read_text(), "欢迎, nef")
if (!ok8) {
    bad = bad + 1
}
tip_el.expect_text("欢迎, nef")
let ok9:boolean = check_true("九、expect_text 读到对的字就过（它自带一段短等待）", true)
if (!ok9) {
    bad = bad + 1
}

// 十：属性读得出来（那一份页面里 #tip 带着 data-mark="tip"）
let ok10:boolean = check_text("十、read_attribute 读出 data-mark", tip_el.read_attribute("data-mark"), "tip")
if (!ok10) {
    bad = bad + 1
}

// 十一：等一条自己消失的提示消失（那条在 500 毫秒后自己把自己摘掉）
let banner_el:wt.Element = form.find_element_by_css(".banner")
if (banner_el != null) {
    banner_el.expect_gone(3000)
}
let ok11:boolean = check_true("十一、expect_gone 等到那条提示自己消失", form.find_element_by_css(".banner") == null)
if (!ok11) {
    bad = bad + 1
}

// 十二：断言对不上的号
let code_206:int = -1
try {
    form.expect_text_css("#name", "不是这两个字", 300)
} catch (c1, m1) {
    code_206 = c1
}
let ok12:boolean = check_int("十二、断言不通过报 206", code_206, 206)
if (!ok12) {
    bad = bad + 1
}

// 十三：等一个永远不来的元素，等的时限到了报 205
let code_205:int = -1
try {
    form.wait_for_element_by_css("#没有这一个", 400)
} catch (c2, m2) {
    code_205 = c2
}
let ok13:boolean = check_int("十三、等不到元素报 205", code_205, 205)
if (!ok13) {
    bad = bad + 1
}

// 十四～十五：400 毫秒后才出来的那一块，等到它、再验它看得见
let late_el:wt.Element = form.wait_for_element_by_css("#arrived", 3000)
let ok14:boolean = check_text("十四、wait_for_element_by_css 等到后出来的那一块", late_el.read_text(), "后出来的那块")
if (!ok14) {
    bad = bad + 1
}
late_el.expect_visible(1000)
let ok15:boolean = check_true("十五、expect_visible 验它看得见", true)
if (!ok15) {
    bad = bad + 1
}

// —— 列表那一页 ——
let rows_page:wt.Page = browser.open_page("file:///" + fixdir + "/list.html")

let ok16:boolean = check_int("十六、count_css 数得出三行", rows_page.count_css(".row"), 3)
if (!ok16) {
    bad = bad + 1
}
let ok17:boolean = check_int("十七、count_xpath 数得出两个链接", rows_page.count_xpath("//a"), 2)
if (!ok17) {
    bad = bad + 1
}
let ok18:boolean = check_int("十八、count_text 数得出那一行", rows_page.count_text("第二行"), 1)
if (!ok18) {
    bad = bad + 1
}

// 十九～二十一：复数定位返回一个元素列表。两条语言规则在这一段里都会走到：
// 按位置取那一样是过户（容器装 class 时 `let x:C = cs[0]` 之后 `cs[0]` 就是 null），
// 所以取出的那一个接在名字上继续使用；逐个遍历走 for（遍历是只读借用，走完列表还是满的）。
let rows:list<wt.Element> = rows_page.find_elements_by_css(".row")
let seen:int = 0
for (el:wt.Element in rows) {
    if (el != null) {
        seen = seen + 1
    }
}
let ok19:boolean = check_int("十九、find_elements_by_css 交回三个元素，for 走完列表还是满的", seen + length(rows), 6)
if (!ok19) {
    bad = bad + 1
}
let second:wt.Element = rows[1]
let ok20:boolean = check_text("二十、列表里第二个元素读得出自己的字", second.read_text(), "第二行")
if (!ok20) {
    bad = bad + 1
}
second.click()
let ok21:boolean = check_text("二十一、点列表里那一个，页面记下的是第二行", rows_page.read_text_css("#tip"), "点到第二行")
if (!ok21) {
    bad = bad + 1
}

// 二十二～二十三：按可见文字定位、按 XPath 取属性
let ok22:boolean = check_text("二十二、按可见文字定位到的那一个读出来就是那段字", rows_page.find_element_by_text("第三行").read_text(), "第三行")
if (!ok22) {
    bad = bad + 1
}
let ok23:boolean = check_text("二十三、XPath 取第二个链接的 href", rows_page.find_element_by_xpath("//a[2]").read_attribute("href"), "/b")
if (!ok23) {
    bad = bad + 1
}

// 二十四：双击
rows_page.find_element_by_css("#box").double_click()
let ok24:boolean = check_text("二十四、double_click 触发页面的 dblclick", rows_page.read_text_css("#tip"), "双击过了")
if (!ok24) {
    bad = bad + 1
}

// 二十五～二十六：看得见与不能用
let ok25:boolean = check_true("二十五、is_visible：画出来的看得见、display:none 的看不见", rows_page.find_element_by_css("#box").is_visible() && !rows_page.find_element_by_css("#hidden").is_visible())
if (!ok25) {
    bad = bad + 1
}
let ok26:boolean = check_true("二十六、is_enabled：disabled 的那个不能用、输入框能用", !rows_page.find_element_by_css("#dis").is_enabled() && rows_page.find_element_by_css("#search").is_enabled())
if (!ok26) {
    bad = bad + 1
}

// 二十七：方框那四个数（页面里写死 120×40）
let box:list<float> = rows_page.find_element_by_css("#box").read_box()
let ok27:boolean = check_true("二十七、read_box 报得出宽 120、高 40", length(box) == 4 && box[2] >= 119.0 && box[2] <= 121.0 && box[3] >= 39.0 && box[3] <= 41.0)
if (!ok27) {
    bad = bad + 1
}

// 二十八：悬停（页面里 mouseenter 才改那个 class）
let off_el:wt.Element = rows_page.find_element_by_css("#off")
off_el.hover()
let ok28:boolean = check_text("二十八、hover 真的让页面收到 mouseenter", off_el.read_attribute("class"), "on")
if (!ok28) {
    bad = bad + 1
}

// 二十九：按一个键（先点那个输入框，让它拿到键盘焦点）
rows_page.find_element_by_css("#search").click()
rows_page.press_key("Enter")
let ok29:boolean = check_text("二十九、press_key 落到聚焦的那个输入框", rows_page.read_text_css("#keylog"), "键:Enter")
if (!ok29) {
    bad = bad + 1
}

// 三十：按坐标点（坐标从那个块的方框算出来）
let zone_box:list<float> = rows_page.find_element_by_css("#zone").read_box()
rows_page.click_at((zone_box[0] + zone_box[2] / 2.0) as int, (zone_box[1] + zone_box[3] / 2.0) as int)
let ok30:boolean = check_text("三十、click_at 用坐标点到那一个块", rows_page.read_text_css("#tip"), "点过坐标")
if (!ok30) {
    bad = bad + 1
}

// 三十一：滚到看得见的位置（那一块在两千像素之下）。这一条排在所有点击与按键之后：
// 滚上来之后上面那些块都出了视口，按坐标那一种动作就打不到了（实测）。
let tail_el:wt.Element = rows_page.find_element_by_css("#tail")
let before_y:float = tail_el.read_box()[1]
tail_el.scroll_into_view()
let after_y:float = tail_el.read_box()[1]
let ok31:boolean = check_true("三十一、scroll_into_view 把最下面那块滚上来", after_y < before_y)
if (!ok31) {
    bad = bad + 1
}

// 三十二：网址与标题的断言
rows_page.expect_url_contains("list.html")
rows_page.expect_title("列表")
let ok32:boolean = check_true("三十二、expect_url_contains 与 expect_title 都过", true)
if (!ok32) {
    bad = bad + 1
}

// 三十三：两只句柄、同一条连接
let ok33:boolean = check_int("三十三、page.get_browser() 那一只与 start 那一只同一条连接", rows_page.get_browser().port(), browser.port())
if (!ok33) {
    bad = bad + 1
}

// 三十四：两页都还挂着
let ok34:boolean = check_true("三十四、list_pages 数得出两页以上", length(browser.list_pages()) >= 2)
if (!ok34) {
    bad = bad + 1
}

// 三十五：截图
rows_page.screenshot(shot)
let head:bytes = file.read_bytes(shot)
let ok35:boolean = check_int("三十五、screenshot 落成 png", head[0] as int, 137)
if (!ok35) {
    bad = bad + 1
}

// 三十六：重新加载之后那一份页面还在（页号没变、连接还是那一条）
rows_page.reload()
rows_page.wait_until_ready(3000)
let ok36:boolean = check_text("三十六、reload 之后标题还在", rows_page.read_title(), "列表")
if (!ok36) {
    bad = bad + 1
}

// 三十七：等一条浏览器主动发来的通知。挂上一页时库里替这一页开了 Page 域，
// 上面那次 reload 一定发过 Page.loadEventFired——那些不带 id 的帧在认答复那一步被跳过，
// 存进包内的表里等上层来取。
let beat:string = rows_page.wait_event("Page.loadEventFired", 3000)
let ok37:boolean = check_true("三十七、wait_event 等得到那条通知", beat != null && beat.contains("Page.loadEventFired"))
if (!ok37) {
    bad = bad + 1
}

// 三十八、三十九：关掉一个标签页。开一页多一个、关一页少一个，数的是浏览器那头的目标，
// 不是包里的页号（页号不回收，这条不回收的规矩靠的就是这一处）。
let before:int = length(browser.list_pages())
let extra:wt.Page = browser.open_page("file:///" + fixdir + "/list.html")
let ok38:boolean = check_int("三十八、开一页之后多一个标签页", length(browser.list_pages()), before + 1)
if (!ok38) {
    bad = bad + 1
}
extra.close()
time.sleep(300)
let ok39:boolean = check_int("三十九、close 之后回到原来那一个数", length(browser.list_pages()), before)
if (!ok39) {
    bad = bad + 1
}

browser.close()
console.print("—— 自测跑完，不对", bad, "处")
if (bad > 0) {
    exit (1, "定位、动作、断言那一面有 " + number.to_string(bad) + " 处不对")
}
console.print("全对")

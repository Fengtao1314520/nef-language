// 真网站检查项：起一个真浏览器，开一个真站，读得出来、点得出去、截得下来。
//
// 跑法（在仓库根，两个参数：浏览器路径、一个空目录作个人资料目录）：
//
//	./nf.exe webtest/selftest/site_check.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" /临时/profile
//
// 这一项与 `element_check.nf` 的分别是：那一项开的是 `webtest/fixtures` 里那两份本地页面，
// 页面形状由我们自己定；这一项开的是第三方站点，形状不由我们定。所以这里只验四件稳定的：
// 网址打开了、标题与整页文字读得出来、页面里有链接、点那个链接能跳到另一家。
// 不验具体文字——example.com 给不同机器回不同语言的页面（实测：有的机器回的是西班牙文
// 版本，页面里没有 `<h1>`，按 `h1` 找就返回空句柄）。
//
// 连不上网、或者那一家不开门：这一项**算失败**，并说明"没网或那一家没开门，这一项没验到"。
// 不静默跳过——跳过的检查等于没检查。

import wt from "libs/web_test"

method check_true(name:string, got:boolean):boolean {
    if (!got) {
        console.print("不对  " + name, "要真，给的是假")
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

let bad:int = 0
let exe:string = args[0]
let profile:string = args[1]
let shot:string = profile + "/site.png"

let browser:wt.Browser = wt.start(exe, profile, 8000, 15000)
console.print("这一趟连的是:", browser.product())

// 一、开一页。开不动（没网、DNS 不通、对方不应）就是抛，这一句直接把话说清再退。
let page:wt.Page = null
try {
    page = browser.open_page("https://example.com/", 15000)
} catch (code, msg) {
    console.print("没网或那一家没开门，这一条没验到：", msg)
    browser.close()
    exit (1, "开 https://example.com/ 没成：" + msg)
}
let here:string = page.read_url()
if (!here.contains("example.com")) {
    console.print("开的网址不对：", here)
    browser.close()
    exit (1, "没网或那一家没开门，这一条没验到（网址是 " + here + "）")
}
let ok1:boolean = check_true("一、那一家开起来了，网址对得上", true)
if (!ok1) {
    bad = bad + 1
}

// 二、标题读得出来（标题里那一段字各家都一样，稳）
let title:string = page.read_title()
let ok2:boolean = check_true("二、读得出页面标题", title != null && title.contains("Example Domain"))
if (!ok2) {
    bad = bad + 1
}

// 三、整页文字读得出来（不验内容，只验读到了东西）
let body:string = page.read_body_text()
let ok3:boolean = check_true("三、读得出整页文字", body != null && length(body) > 20)
if (!ok3) {
    bad = bad + 1
}

// 四～五：页面里有链接，那一个链接的 href 读得出来
let ok4:boolean = check_true("四、按 CSS 数得出页面里的链接", page.count_css("a") >= 1)
if (!ok4) {
    bad = bad + 1
}
let link:wt.Element = page.find_element_by_css("a")
let href:string = ""
if (link != null) {
    href = link.read_attribute("href")
}
let ok5:boolean = check_true("五、那个链接的 href 读得出来", href.contains("iana.org"))
if (!ok5) {
    bad = bad + 1
}

// 六、点它一下，跳到另一家去（这一步验的是真跳转：换了一家站，还在同一条连接上）。
// 用的是库里带等待的断言：点下去之后旧文档的 readyState 还是 complete，
// 不等就直接读网址，读到的仍是原来那一家。
let jumped:boolean = true
if (link == null) {
    jumped = false
} else {
    link.click()
    try {
        page.expect_url_contains("iana.org", 15000)
    } catch (c1, m1) {
        jumped = false
        console.print("    停在：", page.read_url())
    }
}
let ok6:boolean = check_true("六、点那个链接真的跳出去了", jumped)
if (!ok6) {
    bad = bad + 1
}
console.print("    到了：", page.read_url())

// 六乙、等这一页自己报成 complete。第三方脚本多的站可能迟迟不到（那是站点侧的行为）；
// 等不到只记录一句，不把它当成截图那一步的失败。
if (page.read_url().contains("iana.org")) {
    try {
        page.wait_until_ready(8000)
    } catch (c2, m2) {
        console.print("    那一家没报成 complete，只说一句：", m2)
    }
}

// 七、在跳到的那一家上截一张图，png 落得了文件
page.screenshot(shot)
let head:bytes = file.read_bytes(shot)
let ok7:boolean = check_int("七、截图落成 png（头一个字节是 137）", head[0] as int, 137)
if (!ok7) {
    bad = bad + 1
}

// 八、那一张图不是空的：跳到的那一家内容比 example.com 多，字节数应该上千
let ok8:boolean = check_true("八、截出来的 png 有内容", length(head) > 1000)
if (!ok8) {
    bad = bad + 1
}

browser.close()
console.print("—— 自测跑完，不对", bad, "处")
if (bad > 0) {
    exit (1, "真网站那一条有 " + number.to_string(bad) + " 处不对")
}
console.print("全对")

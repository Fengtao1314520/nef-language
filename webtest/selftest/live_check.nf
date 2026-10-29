// web_test 这一层的实际运行检查：起一个浏览器、连上、发两条浏览器层的命令，
// 再开一页挂上去、读得出标题、截一张图落成文件。
//
// 跑法（在仓库根，浏览器路径、一只空的个人资料目录、那一页的网址从命令行进来）：
//
//	./nf.exe webtest/selftest/live_check.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" /临时/profile file:///<仓库目录>/webtest/fixtures/form.html
//
// 清理干净就返回退出码 0。这一项检查会真起浏览器，所以它不跟 socket_check.nf、page_check.nf 同一组：
// 那一组不起浏览器、不联网。
//
// 答复里字段的形状（sessionId 在 result 位置里、一条消息切成几段）的原文已经取过一次，
// 存在 selftest/data 那四份文本里，不起浏览器的检查用那份数据做。
//
// 这一份里那几个名字（exe、profile、start_url）要连着使用几回：库侧收参数的位置都标了
// `ref`，传进去的是只读借用，交完名字仍在调用方手上。

import wt from "libs/web_test"

let exe:string = args[0]
let profile:string = args[1]
let start_url:string = args[2]
let shot:string = profile + "/live.png"
console.print("起这个浏览器:", exe)

let browser:wt.Browser = wt.start(exe, profile, 8000, 5000)
console.print("调试端口:", browser.port())

// 一、浏览器层的一条命令：答复里 result 下面那个位置取得到
let first:string = browser.call("Browser.getVersion", "{}")
let product:string = wt.reply_field(first, "product")
if (product == null) {
    console.print("第一条答复里没有 product:", wt.first_chars(first, 120))
    exit (1, "Browser.getVersion 交不回 product")
}
console.print(" Browser.getVersion 交回:", product)

// 二、命令的号往后走了，答复按号认得对
let second:string = browser.call("Target.getTargets", "{}")
if (!second.contains("\"id\":2")) {
    console.print("第二条答复认不出自己的号:", wt.first_chars(second, 120))
    exit (1, "第二条答复的号对不上")
}
if (!second.contains("targetInfos")) {
    console.print("第二条答复里没有 targetInfos:", wt.first_chars(second, 120))
    exit (1, "Target.getTargets 没交回那一份清单")
}
console.print("过  两条命令都按号认出来了")

// 三、开一页并挂上去：返回的是 Page，页号在库里记着，命令都还走这一条连接
let page:wt.Page = browser.open_page(start_url)
let session:string = page.session_id()
if (length(session) != 32) {
    console.print("sessionId 看着不对:", session)
    exit (1, "开一页那一步取不到 sessionId")
}
console.print("过  开一页挂上去，sessionId 是 32 个字符")

// 四、页面层的命令走到了：标题是从那一页里读出来的
let title:string = page.read_title()
if (title != "登录") {
    console.print("标题读成:", wt.first_chars(title + "|", 60))
    exit (1, "Page 那一层的 Runtime.evaluate 没读到标题")
}
console.print("过  页面层的命令走到了（读得出那一页的标题）")

// 五、截一张图落成文件。
// 导航的答复只说"开始导航"，立刻截图会报 {"error":{"code":-32000,"message":"Not attached to
// an active page"}}（实测），所以 open_page 里面已经等到这一页报成 complete 才返回来。
page.screenshot(shot)
let head:bytes = file.read_bytes(shot)
if (head[0] as int != 137) {
    console.print("png 的头一格不对:", head[0])
    exit (1, "截图落出来的不是 png")
}
console.print("过  截图落成 png，", length(head), "个字节")

browser.close()
console.print("过  起浏览器、连上、两条命令、开一页、截一张图，全收干净")

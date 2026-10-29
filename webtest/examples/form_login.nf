// web_test 的第一个例子：用 Nef 写的一段程序启动一个浏览器，填一张表单、
// 点一次提交、读取页面提示、截一张图。
//
// 跑法（在仓库根，三个参数：浏览器路径、一个空目录作个人资料目录、本地页面所在目录）：
//
//	./nf.exe webtest/examples/form_login.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" /临时/profile <仓库目录>/webtest/fixtures
//
// 这一份用的写法是"定位交回一个元素，动作都在那个元素上做"；Page 上那一份同名快捷
// （read_text_css、click_css 那一族）在最后也用了一次，两种是同一件事的两种写法。

import wt from "libs/web_test"

let exe:string = args[0]
let profile:string = args[1]
let fixdir:string = args[2]
let shot:string = profile + "/login.png"

// 启动一个 headless 浏览器、连上、开一页。等到那一页真正开始加载并报告 complete 才返回。
let browser:wt.Browser = wt.start(exe, profile, 8000, 5000)
console.print("连上的浏览器：", browser.product())

let page:wt.Page = browser.open_page("file:///" + fixdir + "/form.html")
console.print("打开的这一页：", page.read_title(), "（", page.read_url(), "）")

// 定位返回的是元素：写进去的字、读出来的字都在它身上
let name_box:wt.Element = page.find_element_by_css("#name")
let pass_box:wt.Element = page.find_element_by_css("#pass")
name_box.write_text("nef")
pass_box.write_text("1234")
console.print("填进去的是：", name_box.read_text(), "／", pass_box.read_text())

// 点它一下。库里算的是那个元素在页面上的中心，按下与抬起各一次，页面绑定的监听收得到。
page.find_element_by_css("#submit").click()

// expect_ 系列方法自带一段短等待：反复读取，到时限仍不相等就报 206。
let tip:wt.Element = page.find_element_by_css("#tip")
tip.expect_text("欢迎, nef")
console.print("页面上的提示：", tip.read_text(), "（属性 data-mark = ", tip.read_attribute("data-mark"), "）")

// 那条提示半秒后自动消失：找得到元素就等它消失，已经不存在就直接通过。
let banner:wt.Element = page.find_element_by_css(".banner")
if (banner != null) {
    banner.expect_gone(3000)
}
console.print("那条提示已经不在页上了")

// Page 上那份同名快捷：不留句柄、一步做完的时候用它
page.expect_text_css("#heading", "登录")
console.print("Page 上那份快捷也读到了标题")

// 截一张图，png 落成文件
page.screenshot(shot)
console.print("截图落在：", shot, "，", length(file.read_bytes(shot)), "个字节")

// 收：断开连接、把自己起的那个浏览器进程带走
browser.close()
console.print("例子跑完")

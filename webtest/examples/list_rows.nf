// web_test 的第二个例子：用元素列表走完一表格的行，再单独持有一个元素来用。
//
// 跑法（在仓库根，三个参数同第一个例子）：
//
//	./nf.exe webtest/examples/list_rows.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" /临时/profile <仓库目录>/webtest/fixtures
//
// 这一份要说明的是元素列表那件事：
// ① 遍历走 for，元素是只读借用，走完列表还是满的；
// ② 按位置取那一个是过户——取出来之后那一个位置就空了，所以要接着用的那一个
//    先接在一个名字上，后面都用这个名字。

import wt from "libs/web_test"

let exe:string = args[0]
let profile:string = args[1]
let fixdir:string = args[2]

let browser:wt.Browser = wt.start(exe, profile, 8000, 5000)
let page:wt.Page = browser.open_page("file:///" + fixdir + "/list.html")
console.print("这一页：", page.read_title(), "，", page.count_css(".row"), " 行，", page.count_xpath("//a"), " 个链接")

// ① 整份列表走一遍：读每一行的字、点每一行一下
let rows:list<wt.Element> = page.find_elements_by_css(".row")
for (row:wt.Element in rows) {
    if (row != null) {
        let line:string = row.read_text()
        console.print("  一行「", line, "」在页面上的上边是 ", row.read_box()[1])
        row.click()
        console.print("    点它之后提示：", page.read_text_css("#tip"))
    }
}
console.print("走完，列表里还是 ", length(rows), " 个元素")

// ② 要留着后面单独使用的那一个：按位置取出，那一个位置就此交给这个名字
let middle:wt.Element = rows[1]
console.print("握着的那一行读出来是「", middle.read_text(), "」")

// 三种定位方式各调用一次
console.print("按可见文字找那一行：", page.find_element_by_text("第三行").read_text())
console.print("按 XPath 找第二个链接：", page.find_element_by_xpath("//a[2]").read_attribute("href"))

// 悬停与双击：页面里 mouseenter 才改那个 class、dblclick 才写那句提示
page.find_element_by_css("#off").hover()
console.print("悬停之后那个块的 class：", page.find_element_by_css("#off").read_attribute("class"))
let box:wt.Element = page.find_element_by_css("#box")
box.double_click()
console.print("双击之后页面上的提示：", page.read_text_css("#tip"))

// 看得见、能不能用、能不能填字
console.print("display:none 的那一块看得见么：", page.find_element_by_css("#hidden").is_visible())
console.print("disabled 的那个按钮能用么：", page.find_element_by_css("#dis").is_enabled())
let search:wt.Element = page.find_element_by_css("#search")
search.write_text("搜点啥")
search.click()
page.press_key("Enter")
console.print("填进去又按了一下回车，页面记下的是「", search.read_text(), "」与「", page.read_text_css("#keylog"), "」")

// 滚到看得见的位置：那一块本来在两千像素之下
let tail:wt.Element = page.find_element_by_css("#tail")
let before_y:float = tail.read_box()[1]
tail.scroll_into_view()
console.print("滚之前那一块的上边 ", before_y, "，滚之后 ", tail.read_box()[1])

// 一个浏览器可以开好几页；页与页各拿各的定位与动作
let form:wt.Page = browser.open_page("file:///" + fixdir + "/form.html")
console.print("第二页开好了：", form.read_title(), "；这个浏览器现在一共有 ", length(browser.list_pages()), " 个目标")
console.print("从第二页也能回到同一只浏览器：", page.get_browser().port() == form.get_browser().port())

browser.close()
console.print("例子跑完")

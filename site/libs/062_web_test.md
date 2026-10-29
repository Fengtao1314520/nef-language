# 浏览器驱动库 web_test

`web_test` 是用 Nef 自己写成的一份外部库，不随语言发布。

它解决的问题是：用这门语言写的程序驱动一个真实浏览器。程序启动浏览器，在调试端口上建立连接，收发 Chrome DevTools Protocol（CDP）的协议帧，打开页面、点击元素、输入文本、读取页面内容并核对结果。

浏览器可以用无头模式启动，运行期间不出现窗口。这个库在 Chrome 与 Edge 上验证过，其他浏览器没有试过。

## 安装方式

这个库要放进工程的 `libs/` 目录，有两种做法。
第一种是直接放源码目录；第二种是先在仓库里执行下面这条命令，把源码打包成一份容器，再把 `web_test.nfpack` 放进工程的 `libs/`。

```text
nf pack webtest/libs/web_test/web_test.nf -o 你的工程/libs/web_test.nfpack
```

放好之后，代码里写 `import wt from "libs/web_test"`，两种做法的写法相同。
`libs/test_run` 在它的 `nf.json` 里写明依赖 `web_test`，两个包要一起放进工程。

这个包导出三个类型：`Browser` 对应一个已经启动的浏览器，`Page` 对应一个页面，`Element` 对应页面上的一个元素。
下面这段用法是节选，完整可运行示例在仓库 `webtest/examples/` 里。

```nef
import wt from "libs/web_test"

let browser:wt.Browser = wt.start("C:/Program Files/Google/Chrome/Application/chrome.exe", "一处空的目录")
let page:wt.Page = browser.open_page("https://example.com")
page.find_element_by_css("#search").write_text("nef")
page.find_element_by_css("#go").click()
page.expect_text_css("#tip", "找到 3 条")
page.screenshot("一处目录/shot.png")
browser.close()
```

下面三张表里，抛出的错误码那一列只列这个成员自己会报的错误号。
202（连接中断）、203（帧格式不对）、204（答复不是一段 JSON 或者缺少要取的字段）、208（浏览器对命令回复了 error，或者页面执行脚本时自己报错）会出现在任何一次命令往返里。

## `Browser`：一个已经启动的浏览器

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `port()` | `int` | 不抛 | 本次连接使用的调试端口号 |
| `product()` | `string` | 不抛 | 连上的是哪一款浏览器（形如 `Chrome/155.0.8059.85`、`Edg/155.0.4283.62`） |
| `open_page(url, wait_ms = 3000)` | `Page` | 207 | 打开一个页面并把它附加到这次会话上。要等到这一页确实开始加载、并且报告的状态成为 `complete` 才返回 |
| `attach_page(targetId)` | `Page` | 不抛 | 附加到一个已经打开的页面上（页号来自 `list_pages()`） |
| `list_pages()` | `list<string>` | 不抛 | 当前打开的每个页面的 `targetId`（只列出 `type` 为 `page` 的项） |
| `call(method_name, params)` | `string` | 不抛 | 需要发出本库没有覆盖的 DevTools 命令时使用它，返回那条答复的文本 |
| `next_message()` | `string` | 不抛 | 读出一条消息的正文（应答与浏览器主动发来的通知都从它读出） |
| `send_all_bytes(body)` | 没有 | 不抛 | 把这段字节分块送出，直到全部发送完成 |
| `close()` | 没有 | 不抛 | 断开连接，并结束由自己启动的那个浏览器进程。忘记调用也不会留下多个浏览器窗口：连接句柄与进程句柄保存在包的状态里，程序结束时会执行本库登记的清理动作，把它们关闭 |

## `Page`：一个页面

`Page` 上放着定位、动作、断言与等待。
三种定位方式（CSS、XPath、可见文字）各有一套成员，其中复数形式的一组返回元素列表。
查找不到元素时返回空句柄；`wait_for_` 开头的三个成员要等到超时才报 205。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `find_element_by_css` / `_by_xpath` / `_by_text` | `Element` | 不抛 | 查找一个元素；找不到时返回空句柄 |
| `find_elements_by_css` / `_by_xpath` / `_by_text` | `list<Element>` | 不抛 | 返回匹配到的元素列表 |
| `count_css` / `count_xpath` / `count_text` | `int` | 不抛 | 匹配到的元素有多少个 |
| `wait_for_element_by_css` / `_by_xpath` / `_by_text`（等多久 = 3000） | `Element` | 205 | 等到那个元素出现；到时限仍未出现时报 205 |
| `click_css` / `click_xpath` / `click_text`、`write_text_*`、`read_text_*`、`expect_text_*` | `read_text_*` 返回 `string`，其余无返回 | `expect_text_*` 报 206，其余无 | 对应元素上的同名成员，一步完成，只是不返回 `Element` |
| `navigate(url)`、`reload()`、`wait_until_ready(timeout_ms = 3000)` | 没有 | `wait_until_ready` 超时报 207 | 页面自身的这几件事：导航、重新加载、等到页面进入可用状态 |
| `read_title()`、`read_url()`、`read_body_text()` | `string` | 不抛 | 分别读出标题、网址与整页文字 |
| `expect_title` / `expect_url`（整串相等）、`expect_url_contains`（包含那一段就算通过） | 没有 | 206 | 断言，各自带一段短暂的等待；断言不成立时报 206 |
| `press_key(key_name)`、`click_at(x, y)` | 没有 | 不抛 | 键盘输入与坐标点击。键名接受 Enter、Tab、Escape、Backspace，也接受单个字母与单个数字 |
| `screenshot(file_path)` | 没有 | 不抛 | 把当前这一页以 png 格式写入给定的路径 |
| `close()` | 没有 | 关闭后再读取这个 `Page` 报 208 | 关闭这一页所在的标签页。页号不回收，关闭后这个 `Page` 失效，再读取它报 208（浏览器回复的是“这一页不在了”） |
| `wait_event(name, timeout_ms = 3000)` | `string` | 209 | 等待一条浏览器主动发来的通知（形如 `Page.loadEventFired`），返回那条通知的原文。附加到页面时，库会为这一页打开 `Page` 域，通知先存放在包内部的表里，读走一条就少一条 |
| `session_id()`、`get_browser()` | `string`、`Browser` | 不抛 | 这一页挂在哪一条会话上、这一页属于哪一个浏览器 |

## `Element`：页面上的一个元素

`Element` 是页面上的一个元素。
它记录自己属于哪一页、用哪种定位方式、查找什么内容、是第几个，每次使用都重新查找一遍。
页面内容一直在变化，保留上一次的节点会读到已经不存在的元素。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `click()`、`double_click()`、`hover()` | 没有 | 不抛 | 按下与抬起各一次（双击是两对）。这三个成员都先把元素滚动到视口中间，再测量坐标：无头浏览器的视口只有 800×600，页面下方的元素不先滚动，鼠标事件就落在视口之外，什么也不会发生。悬停还要求先把鼠标移到左上角一次，页面才收得到 `mouseenter` |
| `scroll_into_view()` | 没有 | 不抛 | 把元素滚动到可以看见的位置 |
| `write_text(value)`、`read_text()` | `read_text()` 返回 `string` | 不抛 | 能输入文字的三种元素（`input`、`textarea`、`select`）按值读写，其余元素按里面的文字读写；写入之后派发 `input` 与 `change` |
| `read_attribute(name)` | `string` | 不抛 | 读取一个属性；该属性不存在时返回空句柄 |
| `is_visible()`、`is_enabled()` | `boolean` | 不抛 | 有没有被绘制出来；有没有设置 `disabled` |
| `read_box()` | `list<float>` | 不抛 | 元素在页面上的方框：左、上、宽、高四个数；找不到时返回空列表 |
| `expect_text(want, timeout_ms = 3000)`、`expect_visible(timeout_ms = 3000)`、`expect_gone(timeout_ms = 3000)` | 没有 | 206 | 断言，断言不成立时报 206 |

## 自由函数

除了三个类型，这个包还导出一组自由函数。

- `start(exe, profile, port_wait = 8000, io_wait = 5000)`：启动浏览器进程，连接它的调试端口，返回一个 `Browser`。
- `wrap_socket(sock)`：把一个已经建立的套接字包装成 `Browser`，供本库的自测自己监听、自己连接时使用。
- `page_at(index)`：按页号取得一个 `Page` 的句柄，`libs/test_run` 就靠它把页面交给脚本。
- `current_browser()`：需要浏览器句柄时当场取一个，不必一直持有 `start` 返回的那个浏览器句柄。
- 再往后，包把底层能力也开放出来：`build_frame` 与 `read_frame` 负责一帧协议文本的编码与解码；`handshake_text`、`reply_field`、`png_from_reply`、`first_chars` 负责握手文本的拼接、从答复里取出一个字段的值、把截图答复解码成字节、按长度截取一段文本的开头。
- 包同时导出本库的十个错误号（200 到 209），脚本里可以直接用名字判断捕获到的是哪一种错误。

## 本库定义的错误号

语言自己占用的错误号从 10000 往上，写程序的人与库使用 0 到 9999 这一段。
这个包从 200 开始分配，一个错误号对应一种错误：

| 错误号 | 什么时候抛出 |
| --- | --- |
| 200 | 浏览器已经启动，但等不到它写出端口文件 |
| 201 | 握手返回的状态码不是 101 |
| 202 | 连接中断，或者对方先关闭了连接 |
| 203 | 帧本身的格式就不对 |
| 204 | 答复不是一段 JSON，或者缺少要取的那个字段 |
| 205 | 到时限仍然没有等到那个元素 |
| 206 | 断言不通过 |
| 207 | 页面没有进入可用状态（`readyState` 到时限还不是 `complete`） |
| 208 | 浏览器对该命令回复了 `error`，或者页面执行那段脚本时自己抛出了错误 |
| 209 | 到时限仍然没有等到浏览器主动发来的通知 |

## 限制：一个进程只能连一个浏览器

一个进程只能连一个浏览器。

连接句柄与进程句柄存放在包的顶层状态里（`web_test.nf` 顶层的 `Link`），三个类型的成员都通过它发出命令。
原因在语言这一侧：`this` 是方法接收者的只读借用，不能交出，编译器原文是“this 是方法接收者的只读借用，不能交出去”。
所以 `Page` 的字段里存不下指向 `Browser` 的引用，`Element` 的字段里也存不下指向 `Page` 的引用。
包之间的规则也加强了这一点：包的门面只能导出这个包自己定义的类型，别的包定义的类型放不进字段，`Browser` 的句柄无法存进 `libs/test_run` 的字段。
因此脚本运行层只能按页号调用 `page_at` 取回页面句柄。

结果是：同一次运行里启动第二个浏览器，会覆盖第一个浏览器的连接。

## 参见

- [用例运行层 test_run](/libs/063_test_run)：建立在 web_test 上面的用例框架
- [TCP 套接字](/stdlib/057_net)：这个库的 WebSocket 握手与协议帧收发，建立在 `net.connect` 返回的裸套接字之上
- [HTTP 客户端](/stdlib/058_http)：与浏览器无关的取网页写法
- [多个文件](/reference/042_modules)：`libs/` 目录与 `.nfpack` 容器的规则

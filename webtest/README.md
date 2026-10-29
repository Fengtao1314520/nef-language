# webtest

这份说明回答三个问题：**怎么用 Nef 自己写的代码控制一个浏览器**、**一份用例脚本按什么结构写**、**一次怎么运行一批用例**。
语言本身的规则不在这里回答，它们写在站上的参考页（`site/reference/`）里。
给网站读者看的那两页在 [`../site/libs/062_web_test.md`](../site/libs/062_web_test.md) 与 [`../site/libs/063_test_run.md`](../site/libs/063_test_run.md)，内容比这一份精简，成员表相同。

## 里面有什么

| 位置 | 内容 |
| --- | --- |
| `libs/web_test/` | 浏览器驱动包，四个文件：`nf.json` 写包名与版本，`frame.nf` 负责一帧的格式，`wire.nf` 负责与文本有关的工作（握手那一段、从答复里取出一个位置上的值、把定位条件拼接成页面能够执行的 JS），`web_test.nf` 是门面文件，导出 `Browser`、`Page`、`Element` 三个类型 |
| `libs/test_run/` | 脚本运行层，一份文件 `test_run.nf` 加一份 `nf.json`。它建立在 `web_test` 之上，负责用例组、用例、步骤与报告，只对外提供 `Run` 这一个类型 |
| `selftest/` | 七份自测。四份不启动浏览器：`frame_check.nf`（24 项定值）、`socket_check.nf`（8 项，用一对回环套接字）、`page_check.nf`（10 项，使用 `data/` 那四份真实答复原文）、`error_check.nf`（2 项，浏览器启动失败的两种情形）。三份实际启动浏览器：`live_check.nf`（底层的往返）、`element_check.nf`（页面与元素部分，39 项，使用 `fixtures` 那两份本地页面）、`site_check.nf`（8 项，打开真实站点 https://example.com，点击它的链接跳转到 iana.org 之后再截图） |
| `selftest/data/` | 2026-10-21 从本机 Chrome 155 记录下来的四条答复原文，一个字节都没有改动。不启动浏览器的那几份自测使用这些数据 |
| `fixtures/` | 两份本地页面，供实际启动浏览器的那几份自测与示例使用：`form.html`（一张登录表单、一条半秒后自动消失的提示、一块 400 毫秒后才出现的区域）、`list.html`（三行列表项、两个链接、一块设置了 `display:none` 的区域、一个 `disabled` 元素、一个 120×40 的方框、一块位于两千像素之下的区域） |
| `examples/` | 两份用这门语言编写的示例：`form_login.nf` 演示填写表单、提交表单、查看提示与截图；`list_rows.nf` 演示元素列表与单个元素的操作 |
| `cases/` | 两份按脚本结构编写的真实用例：`login.nf` 包含“表单登录”这一组的五条用例（结果里有通过、有失败、有跳过，还有一次由脚本自己编写的重试），`list_page.nf` 包含“列表页”这一组的三条用例 |
| `run_all.nf` | 批量运行的入口：遍历 `cases/` 目录，为每份用例启动一个 `nf.exe` 进程并等它运行结束，再把每份 `report.jsonl` 读回来合并成 `all.jsonl` 与 `all.txt`。这个入口同样用这门语言写成，只使用语言已经具备的能力 |

## 安装方式与用法

这个包是 Nef 的第一个外部库，语言本身不带它。写程序的人要把它放进自己工程的 `libs/` 目录：
可以直接放源码目录，也可以在本仓库里执行 `nf pack webtest/libs/web_test/web_test.nf -o 你的工程/libs/web_test.nfpack` 打出一份容器再放进去。
放进去之后的写法仍然相同，是 `import 名字 from "libs/web_test"`。`libs/test_run` 的 `nf.json` 里
写明它依赖 `web_test`，所以两份都要放进工程。

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

## 包提供的三个类型

`Browser`：一个已经启动的浏览器。

| 成员 | 做什么 |
| --- | --- |
| `port()` | 本次连接使用的调试端口号 |
| `product()` | 连上的是哪一款浏览器（形如 `Chrome/155.0.8059.85`、`Edg/155.0.4283.62`） |
| `open_page(网址, 等多久 = 3000)` | 打开一个页面并把它挂到这次会话上，返回一个 `Page`。要等到这一页确实开始加载、并且报告的状态成为 `complete` 才返回 |
| `attach_page(targetId)` | 挂到一个已经打开的页面上（页号来自 `list_pages()`） |
| `list_pages()` | 当前打开的每个页面的 `targetId`（只列出 `type` 为 `page` 的项） |
| `call(方法名, 参数)` | 需要发出本库没有覆盖的 DevTools 命令时使用它，返回那条答复的文本 |
| `next_message()` | 读出一条消息的正文（应答与浏览器主动发来的通知都从它读出） |
| `send_all_bytes(字节块)` | 把这段字节分块送出，直到全部发送完成 |
| `close()` | 断开连接，并结束由自己启动的那个进程。忘记调用也不会留下多个浏览器窗口：那两个句柄保存在包的状态里，程序结束时会执行本库登记的清理动作，把它们关闭 |

`Page`：一个页面。定位、动作、断言与等待都在这里；三种定位方式（CSS、XPath、可见文字）各有一套成员，
其中复数形式的那一套返回元素列表。查找不到元素时返回空句柄，`wait_for_` 开头的那三条要等到时限才报 205。

| 成员 | 做什么 |
| --- | --- |
| `find_element_by_css` / `_by_xpath` / `_by_text` | 查找一个元素，返回 `Element`；找不到时返回空句柄 |
| `find_elements_by_css` / `_by_xpath` / `_by_text` | 返回 `list<Element>` |
| `count_css` / `count_xpath` / `count_text` | 匹配到的元素有多少个 |
| `wait_for_element_by_css` / `_by_xpath` / `_by_text`（等多久 = 3000） | 等到那个元素出现；到时限仍未出现时报 205 |
| `click_css` / `click_xpath` / `click_text`、`write_text_*`、`read_text_*`、`expect_text_*` | 对应元素上的同名成员，只是不返回 `Element`，一步完成 |
| `navigate(网址)`、`reload()`、`wait_until_ready(等多久 = 3000)` | 页面自身的这几件事 |
| `read_title()`、`read_url()`、`read_body_text()` | 分别读出标题、网址与整页文字 |
| `expect_title` / `expect_url`（整串相等）、`expect_url_contains`（包含那一段就算通过） | 断言，各自带一段短暂的等待；断言不成立时报 206 |
| `press_key(key_name)`、`click_at(x, y)` | 键盘输入与坐标点击。键名接受 Enter、Tab、Escape、Backspace，也接受单个字母与单个数字 |
| `screenshot(路径)` | 把当前这一页以 png 格式写入给定的路径 |
| `close()` | 关闭这一页所在的标签页。页号不回收，关闭后这个 `Page` 之后失效，再读取它报 208，浏览器回复的是“这一页不在了”） |
| `wait_event(方法名, 等多久 = 3000)` | 等待一条浏览器主动发来的通知（形如 `Page.loadEventFired`），返回那条通知的原文；到时限仍未等到时报 209。挂上一页时，库会为这一页打开 `Page` 域，通知先存放在包内部的那张表里，取走一条就少一条 |
| `session_id()`、`get_browser()` | 这一页挂在哪一条会话上、这一页属于哪一个浏览器 |

`Element`：页面上的一个元素。它记录自己属于哪一页、用哪种方式定位、查找什么内容、是第几个，
每次使用都重新查找一遍：页面内容一直在变化，保留上一次的节点会读到已经不存在的元素。

| 成员 | 做什么 |
| --- | --- |
| `click()`、`double_click()`、`hover()` | 按下与抬起各一次（双击是两对）。这三条都先把那一个元素滚动到视口中间，再测量坐标：无头浏览器的视口只有 800×600，页面下方的元素不先滚动，鼠标事件就落在视口之外，什么也不会发生（2026-10-22 在真实站点上遇到的就是这一条）。悬停还要求先把鼠标移到左上角一次，页面才收得到 `mouseenter` |
| `scroll_into_view()` | 把元素滚动到可以看见的位置 |
| `write_text(文本)`、`read_text()` | 能输入文字的三种元素（`input`、`textarea`、`select`）按值读写，其余元素按里面的文字读写；写入之后派发 `input` 与 `change` |
| `read_attribute(属性名)` | 读取一个属性；该属性不存在时返回空句柄 |
| `is_visible()`、`is_enabled()` | 有没有被绘制出来；有没有设置 `disabled` |
| `read_box()` | 元素在页面上的方框：左、上、宽、高四个数；找不到时返回空列表 |
| `expect_text(文本, 等多久 = 3000)`、`expect_visible(等多久 = 3000)`、`expect_gone(等多久 = 3000)` | 断言，断言不成立时报 206 |

自由函数：`start(浏览器程序的路径, 个人资料目录, 等端口多久 = 8000, 收发等多久 = 5000)`、
`wrap_socket(套接字)`（供自测自己 listen、自己 connect 时使用）、`page_at(页号)`（按页号取得一个 `Page` 的句柄，
脚本运行层就靠它把页面交给脚本）、`current_browser()`（需要浏览器句柄时当场取一个，不必一直持有 `start` 返回的那个）、
`build_frame`、`read_frame`、`handshake_text`、`reply_field`、`png_from_reply`、`first_chars`，另外还包括本库的错误号。

`Run`：脚本运行层（`libs/test_run`）导出的唯一类型。脚本运行层只有四个概念：用例组、用例、步骤与两种钩子。
一份用例文件包含一个用例组，组里的每条用例写成这个文件里的一个函数，由一个整数序号通过 `switch` 分派；
两种钩子（`before_each`、`after_each`）同样是脚本里的函数，在循环里固定调用；函数不能当作值传递，
框架因此收不了回调。重试写在脚本里，框架不处理“第几遍”这件事。逐条规矩写在下面「编写脚本运行层要知道的九条」那一节。

```nef
import wt from "libs/web_test"
import tr from "libs/test_run"

let run:tr.Run = tr.begin_run(exe, profile, "一处空的目录")
let page:wt.Page = null

method group_login() {
    let home:int = run.group_begin("表单登录", start_url)           // 组里只登一次录
    for (i:int in 0 until length(titles)) {
        let title:string = titles[i]                              // 那个位置读一次就空了，先接住
        if (!run.picked(title)) {
            continue
        }
        page = wt.page_at(run.case_begin(title))                   // 每条一个新标签页
        try {
            run.step("填上用户名")
            page.find_element_by_css("#name").write_text("nef")
            run.step("点登录")
            page.expect_text_css("#tip", "欢迎, nef")
            run.case_pass()
        } catch (code, msg) {
            run.case_fail(code, msg)                               // 截图与整页文字在这一句里拿
        }
        run.case_end()                                            // 关掉这一个标签页
    }
    run.group_end()
}

run.pick(picks)                                                   // 空清单＝全跑
group_login()
run.finish()                                                      // 有挂就退 1
```

| 成员 | 做什么 |
| --- | --- |
| `tr.begin_run(浏览器程序的路径, 个人资料目录, 输出目录)` | 启动浏览器，把输出目录清空后重新建好，返回一个 `Run` |
| `run.pick(标题列表)` / `run.picked(标题)` | 挑选要运行哪几条：列表为空就全部运行；`picked` 返回一个 `boolean`，脚本里的那句 `if` 使用它 |
| `run.group_begin(组名, 起始网址)` / `run.group_end()` | 一个用例组只执行一次的开场与结束处理。开场返回一个页号（组里准备前置条件时使用的那个页面），结束时把它关掉 |
| `run.case_begin(标题)` / `run.case_pass()` / `run.case_fail(错误号, 说明)` / `run.case_end()` | 一条用例的过程。`case_begin` 返回新标签页的页号，`case_end` 打印那一行结果，把两份报告各追加一条，并关掉那一页 |
| `run.skip(标题, 原因)` | 在报告里留下一行“跳过”，让人看得出这条用例存在过、而这一次没有验证到它 |
| `run.step(名字)` | 记录一个步骤，同时记下它距离这条用例开头的毫秒数。通过的用例不打印，失败的用例才列出走过的步骤 |
| `run.finish()` | 打印总结那一行，写出终端显示的文本，并关闭浏览器；有用例失败时以 1 退出 |

一条用例的结果以第一个 `case_fail` 为准，后来的 `case_pass` 不改变它，第二个 `case_fail` 只在那一行的下面追加一句。

## 运行一批

一份文件对应一个用例组，运行它就是 `nf 用例文件.nf`；一次运行一组文件用 `webtest/run_all.nf`，它同样用这门语言写成。

```text
./nf.exe webtest/run_all.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <仓库目录>/webtest/cases <空的输出目录>
```

后面还可以跟两个可以省略的参数：`nf.exe` 自己的路径（默认就叫 `nf.exe`，也就是当前目录下的可执行文件），以及只运行哪几份用例的文件名（不带 `.nf`，用逗号分隔，如 `login,list_page`）。两个目录都要给出绝对路径：每份用例脚本里的那句 `file:///` 网址是取 cases 目录的上一级拼接出来的，浏览器不认识相对路径，所以这个入口在开头就检查是不是绝对路径，不合格时报一句“要给绝对路径”。

每份用例分到一个属于自己的 profile 目录（`输出目录/profiles/<文件名>`），两份用例不会争用同一个浏览器目录。每份用例各自目录里的 `report.jsonl`、`report.txt` 与截图由 `test_run` 写出；它只负责合并，不改动内容：把每份报告的每一行原样接过来，在开头插入一个 `"file":"文件名"` 字段，最后一行是总结。

```text
→ 2 份，9 条：过 7，挂 1，跳 1
  x login / 等那块后出来的：等不到元素：按CSS 「#arrived」等了 20 毫秒
报告 那一个目录/all.jsonl
```

退出码 0 表示全部通过，1 表示有用例失败，2 表示无法启动（目录不存在，或者某一份用例没有产生报告）。

生成的文件都在同一个输出目录里：`report.jsonl` 一条用例占一行（写给机器读取，脚本运行层汇总时读的就是它）；
`report.txt` 是终端上显示的文本；失败的用例另有 `case-<第几条>.png` 与 `.txt`。每次运行先清空再写入。

## 本库定义的错误号

语言自己占用的错误号从 10000 往上，写程序的人与库使用 0～9999 这一段。这个包从 200 开始分配，一个号对应一种错误：

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
| 208 | 浏览器对该命令回复了 `error`，或者页面执行那段 JS 时自己抛出了错误 |
| 209 | 到时限仍然没有等到浏览器主动发来的通知 |

## 限制

**一个进程只能连一个浏览器。** 连接与进程句柄存放在包的顶层状态里（`web_test.nf` 顶层的那一个
`Link`），三个类型的成员都通过它发出命令。原因在语言这一侧：`this` 不能交出去（实测报
“this 是方法接收者的只读借用，不能交出去”），所以 `Page` 里存不下一个指向 `Browser` 的引用，
元素里也存不下指向页面的引用。结果是同一次运行里启动第二个浏览器，会覆盖第一个浏览器的连接。

## 如何运行

在仓库根：

```text
./nf.exe webtest/selftest/frame_check.nf
./nf.exe webtest/selftest/socket_check.nf
./nf.exe webtest/selftest/page_check.nf webtest/selftest/data <临时目录>/shot.png
./nf.exe webtest/selftest/error_check.nf <临时目录>
./nf.exe webtest/selftest/live_check.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录> file:///<页面地址>
./nf.exe webtest/selftest/element_check.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录> <页面所在目录>
./nf.exe webtest/selftest/site_check.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录>
./nf.exe webtest/examples/form_login.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录> <页面所在目录>
./nf.exe webtest/examples/list_rows.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录> <页面所在目录>
./nf.exe webtest/cases/login.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录> <空的输出目录> <页面所在目录> 要挑的那一条标题
./nf.exe webtest/cases/list_page.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <空的临时目录> <空的输出目录> <页面所在目录>
./nf.exe webtest/run_all.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <仓库目录>/webtest/cases <空的输出目录>
```

每份自测在全部通过时打印“全对”并返回退出码 0；只要有一处不对，就把不对的几处打印出来并返回 1；
示例运行结束打印“例子跑完”；`cases/login.nf` 那一组里有一条是故意等不到的，所以它返回 1 符合预期，
最后一个参数不给就运行全部用例。`site_check.nf` 依赖网络：没有网络，或者那个站点没有响应，就算失败，
并打印一句“没网或那一家没开门，这一条没验到”，而不是静悄悄跳过。
实际启动浏览器的这几份自测在结束前都会关闭自己启动的那个进程，跑完之后不留仍在运行的 headless 进程。

这些内容在 Go 侧另有十一条测试（`compiler/cmd/nf/webtest_test.go`），跟着 `go test ./...` 一起运行。

## 编写脚本运行层要知道的九条

这九条来自编译器的报错，以及程序能运行但结果不对这两类实际情况：

1. 一个式子不能跨行书写：`[` 或 `,` 之后换行就是编译错误。
2. 名字作为实参传给自己写的函数之后就失效，与后面那个分支有没有执行到无关；参数前面标 `ref` 的
   位置是只读借用，名字交进去还归调用方用。这一层的参数只要读不改，
   所以库里都标了 `ref`；要把值留下来的那两处（写字段、进容器）在库里给一份拷贝（`x + ""`）。
3. 从字段与容器的一个位置里取走那个值，同样是转移所有权：`let buf:bytes = link.pending` 之后 `link.pending` 就空了，
   用完要写回去。从 `list` 里按位置取元素也是同样的规则。所以 `rows[1]` 用过一次之后那一个位置就是空句柄，
   还要继续使用就先把它接在一个名字上；`for` 遍历交过来的是只读借用，走完一遍容器还是满的。
   把字段本身整个交给一个**要拿走**的函数（`sink(this.selector)`）会在编译时被拒绝，编译器同时给出两种
   正当写法（先把值接到名字上再交，或者交一份拷贝 `this.selector + ""`）。
   收方标了 `ref` 就不一样：那一次读不多出新持有者，字段不置空，所以 `note(this.label)` 这种写法直接合法。
4. 默认值可以写字面量，也可以写**被调方所在文件**顶层的 `const`，以及这两者的算术
   （`毫秒:int = expect_ms`、`wide:int = STEP * 4`）。这里认的是被调方文件的名字表：调用方
   即使有一个同名的顶层名字也不会替换掉它。写成调用方自己的顶层名字不认，运行时会报一句
   “… 是 args 或还没排到静态槽的名字”。
5. `s.slice(start, end)` 的“止”不允许大于长度（越界是 10031 号），所以截取一段文本之前要先判断长度，包里的
   `first_chars` 就是为这件事准备的。
6. `json.at` 取不到那个位置时返回空句柄，而 `json.to_integer`、`json.to_text` 拿到空句柄要抛 10092；
   浏览器主动发来的通知里没有 `id` 这个位置，所以在确认一条答复之前，要先判断这个位置在不在。同理，从库里读出来的那段文本
   可能是空句柄（对应的那个属性不存在），拿它去和一段文本比较就是 10092，要先判断 `!= null`；
   `console.print` 打印空句柄时输出的是“null”这四个字符，不要把它当成一段文本。
7. 页面里执行 JS 的结果一律写成文本返回：`json.to_text` 只返回文本，数字与真假这两种值它返回空句柄。
   判断一个元素是不是“能输入文字的那一种”要看标签名，不能看 `el.value` 在不在；`<li>` 也有 `value`，
   是一个数字，没有写就是 0。
8. **容器里的一个位置读一次就空了，写成 `xs[i] + ""` 同样如此**：容器装的是引用类型时，读和写都是交出，不分哪一种。执行
   `console.print(xs[0] + "")` 之后再读一次 `xs[0]`，报
   “对着空句柄取成员或调用方法”（10092）。名字那种情况不一样：名字给一份拷贝就行，名字本身读不空，
   容器里的位置读一次就空。所以循环里按位置取标题只取一次，取下来接在一个名字上。
   `for (一样 in 容器)` 交过来的是只读借用，遍历完容器还是满的，可以反复读取。
9. **包的门面只能导出这个包自己定义的类型**（编译器原文“Page 是别的包里的类型：门面只交出
   这个包自己定义的东西”）。所以 `test_run` 的字段里放不进 `wt.Browser`，成员函数也不能返回
   `wt.Page`：`case_begin` 返回的是页号，脚本那一边写 `wt.page_at(页号)` 来取句柄。
   另外，顶层 `let` 可以在自己写的函数里重新赋值，把句柄放在顶层就是这样用的。

## 为什么只走 CDP

这个库只使用 CDP（Chrome DevTools Protocol），第一版只在 Chrome 与 Edge 上测试，Firefox 排在以后。
选这条协议是因为一条 WebSocket 连接就能覆盖启动、定位、输入、截图与执行 JS，不需要在系统里再装别的东西。库分两层：`web_test` 只管与浏览器通讯和页面操作，`test_run` 只管用例的组织和报告；两层的边界就是上面那些类型、成员与错误号。

## 约定

- 包里的名字一律用英文完整拼写，不缩写（`absolute_value` 而不是 `abs`，与语言内置成员用的是同一条规则）。
- 库自己抛出的错误号取 0～9999 这一段，脚本运行层使用 200 到 209。
- 这个目录里运行时生成的产物（截图、报告）不放进版本控制。

## 许可

`web_test` 与 `test_run` 这两个包是 Nef 项目的组成部分，源码按仓库根 `LICENSE` 的 Apache-2.0 授权。

`LICENSE-EXCEPTION` 那份输出例外说的是编译产物：用 `nf` 编出来的程序归使用者，不要求开源，也不要求在程序里复现这份声明。这两个包不是那一档——它们是被人以源码 `import` 进自己工程的，那份源码会留在别人的仓库里，仍然是 Apache-2.0 材料。要尽的是 Apache-2.0 第 4 条那几项常规义务：给拿到这份源码的人一份许可证文本，改过的文件注明改动。

`nf pack` 打出来的 `.nfpack` 容器里只有 `nf.json` 与包内的源文件（`internal/pkgfmt/pkgfmt.go` 开头记的就是这个形状），不带许可文本。往外交付一个容器时，把 `LICENSE`（有依赖就再加 `NOTICE`）跟着一起给出去。

这一份 README 与 `.nf` 文件里的注释按 `nef-licensing/LICENSE-DOCS.md` 的 CC BY 4.0 授权。

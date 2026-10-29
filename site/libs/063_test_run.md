# 用例运行层 test_run

`test_run` 是 `web_test` 上面的一层，负责把浏览器操作组织成用例：用例组、用例、步骤，以及打印出来的结果。它只对外提供一个类型 `Run`。

`test_run` 用 Nef 自己写成，只有一个文件。它不负责决定运行哪几份用例文件，也不判断“第几遍”：一份用例文件就是一次程序运行，重试写在脚本的循环里。

函数不能当作值传递，所以框架收不到回调。`before_each`、`after_each` 与分派用例的那个函数都写在同一份文件里，在循环里固定调用。

## 安装方式

`test_run` 依赖 `web_test`，两份都要放进工程的 `libs/` 目录。放法与依赖关系写在[浏览器驱动库 web_test](/libs/062_web_test) 的“安装方式”一节。

## 用例文件的写法

一份文件装一个用例组。组名写在 `group_begin` 的第一个参数里，每条用例是一段 `switch` 分支，标题放在一个 `list<string>` 里按位置对应。

```nef
import wt from "libs/web_test"
import tr from "libs/test_run"

let titles:list<string> = ["填对了就进得去", "错密码进不去"]
let run:tr.Run = tr.begin_run(exe, profile, out_dir)
let page:wt.Page = null

method before_each(which:int) {
    run.step("确认这一页已经打开")
    page.expect_title("登录")
}

method after_each(which:int) {
    run.step("清掉那句提示")
    page.find_element_by_css("#tip").write_text("")
}

method one_case(which:int) {
    switch (which) {
        0 {
            run.step("填上用户名")
            page.find_element_by_css("#name").write_text("nef")
            run.step("点登录")
            page.find_element_by_css("#submit").click()
            page.expect_text_css("#tip", "欢迎, nef")
        }
        1 {
            run.step("填上不对的密码")
            page.find_element_by_css("#pass").write_text("wrong")
            page.expect_text_css("#tip", "用户名或密码不对")
        }
    }
}

method group_login() {
    let home:int = run.group_begin("表单登录", start_url)
    for (i:int in 0 until length(titles)) {
        let title:string = titles[i]
        if (!run.picked(title)) {
            continue
        }
        page = wt.page_at(run.case_begin(title))
        try {
            before_each(i)
            one_case(i)
            run.case_pass()
        } catch (code, msg) {
            run.case_fail(code, msg)
        }
        after_each(i)
        run.case_end()
    }
    run.group_end()
}

run.pick([])
group_login()
run.finish()
```

上面这段是骨架，`exe`、`profile`、`out_dir`、`start_url` 四个名字由脚本从 `args` 里读出来再赋给顶层的 `let`。仓库里 `webtest/cases/login.nf` 是一份填好内容的完整用例文件。

三处顺序要留意：

`run.pick(...)` 在所有分组函数之前调用，它只登记要运行哪些标题；`picked(title)` 在循环里逐条判断。

一条用例的第一个 `case_fail` 决定结果，后来的 `case_pass` 不改变它。第二个 `case_fail` 只在失败结果下面追加一句。

`case_end()` 负责打印这条用例的结果行、写两份报告、关掉这一条自己的标签页。少调用一次，这一条就停在“没有报结果”的状态，`case_end` 会把它记成失败。

## 成员

| 写法 | 返回 | 作用 |
| --- | --- | --- |
| `tr.begin_run(exe, profile, dir)` | `Run` | 启动浏览器，把输出目录清空后重新创建，返回一个 `Run` |
| `run.pick(names)` | 没有 | 登记要运行的用例标题。列表为空表示全部运行 |
| `run.picked(title)` | `boolean` | 这一条要不要运行，脚本里的那句 `if` 使用它 |
| `run.group_begin(name, url)` | `int` | 一个用例组执行一次的开场：打开起始页面，返回它的页号 |
| `run.group_end()` | 没有 | 关闭 `group_begin` 打开的那一页 |
| `run.case_begin(title)` | `int` | 开始一条用例：新开一个标签页，返回页号；脚本用 `wt.page_at(index)` 取句柄 |
| `run.step(label)` | 没有 | 记录一个步骤，同时记下它距离这条用例开头多少毫秒 |
| `run.case_pass()` | 没有 | 标记这一条通过 |
| `run.case_fail(bad_code, bad_msg)` | 没有 | 标记这一条失败，并当场取回截图与整页文字 |
| `run.case_end()` | 没有 | 结束这一条：打印结果、写两份报告、关闭标签页 |
| `run.skip(title, reason)` | 没有 | 在报告里留下一行“跳过”，说明这条用例存在过、这一次没有验证 |
| `run.finish()` | 没有 | 打印总结，写出终端的报告文本，关闭浏览器；有失败时以 1 退出 |

`case_begin` 返回页号而不是 `Page`，是因为包的门面只能导出这个包自己定义的类型：`test_run` 的字段与成员都不能出现 `web_test` 里的类型。脚本用 `wt.page_at(index)` 把页号换成句柄。

## 步骤与失败时留下的记录

`run.step(label)` 只在失败的用例里打印。通过的用例只有一行结果，不列出步骤。

`case_fail` 会当场向浏览器取两项数据：一张 png 截图、整页的可见文字。两份文件写在输出目录里，名字带着这是第几条：`case-3.png` 与 `case-3.txt`。取这两项数据的动作在框架里，脚本不需要写。

失败那一行在终端上展开成步骤列表与错误说明：

```text
  x 等那块后出来的  25 毫秒
      - 每条用例开头先确认这一页已经打开
      - 等那块 400 毫秒后才出来的方块
      ! 等不到元素：按CSS 「#arrived」等了 20 毫秒（205）
      图 /临时/out/case-4.png  页面 /临时/out/case-4.txt
```

## 报告

每次运行先把输出目录清空再写。三类文件落在这个目录里：

| 文件 | 内容 |
| --- | --- |
| `report.jsonl` | 一条用例占一行，给机器读 |
| `report.txt` | 终端上看到的那份文本 |
| `case-<序号>.png`、`case-<序号>.txt` | 只有失败的用例才有，是失败时的截图与整页文字 |

`report.jsonl` 每行九个字段：`group`、`case`、`state`、`ms`、`code`、`msg`、`shot`、`text`、`steps`。

`state` 是 `pass`、`fail`、`skip` 三个词之一；`ms` 是这条用例用了多少毫秒；`code` 与 `msg` 只有失败时才不是 0 和空文本；`shot` 与 `text` 是截图与整页文字这两个文件的路径；`steps` 是走过的步骤，用分号隔开。

跳过的那一行 `ms` 与 `code` 都是 0，`msg` 放跳过的原因。

终端上每一行前面是一个标记：`ok` 表示通过，`x` 表示失败，`--` 表示跳过。最后一行给出总数：

```text
→ 5 条：过 3，挂 1，跳 1
报告 /临时/out/report.jsonl
```

## 一次运行一组

一份文件对应一个用例组，运行它就是 `nf 用例文件.nf`。一批用例文件用 `webtest/run_all.nf` 遍历目录，逐份起一个 `nf.exe` 进程运行完，再把每份 `report.jsonl` 读回来合并。

```text
./nf.exe webtest/run_all.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <仓库目录>/webtest/cases <一处空的目录>
```

后面还可以跟两个可省略的参数：`nf.exe` 自己的路径，以及只运行哪几份用例的文件名（不带 `.nf`，用逗号分隔）。两个目录都要写绝对路径：用例脚本里的 `file:///` 网址由 cases 目录的上一级拼出来，浏览器不认相对路径。

每份用例分到一个自己的 profile 目录（`输出目录/profiles/<文件名>`），两份用例用不到同一个浏览器目录。

合并后的报告是 `all.jsonl` 与 `all.txt`：每行原样接过来，在开头插入一个 `"file"` 字段说明它来自哪一份用例，最后一行是总结。这个入口只做合并，不改动内容。

退出码 0 表示全部通过，1 表示有用例失败，2 表示没能运行起来（目录不存在，或者某一份用例没有产生报告）。

## 未提供的能力

- 框架不判断“第几遍”，重试写在脚本的循环里。
- 没有并发：一条用例运行完才运行下一条，因为这门语言没有线程。
- 不接收回调函数：函数不能当作值传递，钩子在循环里固定调用。
- 报告只有终端文本与 `report.jsonl` 两种，没有 html 页面，也不向任何服务上报结果。

## 参见

- [浏览器驱动库 web_test](/libs/062_web_test)：用例运行层建立在它上面
- [多个文件](/reference/042_modules)：`libs/` 目录与 `.nfpack` 的规则
- [错误处理](/learn/031_errors)：`try` 与 `catch` 的写法

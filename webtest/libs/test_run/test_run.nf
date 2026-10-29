// test_run：脚本运行这一层，坐在 web_test 上面。
//
// 脚本里认四个概念：组、条、步骤、两种钩子。一份用例文件对应一个组，组里那几条用例各写成
// 同文件里的一个函数，靠一个整数序号 switch 分派（函数不能当值，所以清单写不成
// "标题 → 函数"那张表）。重试写在脚本里，这一层没有"第几遍"这个概念。
//
// 三条语言事实决定了下面的写法：
// ① 参数只读、不把值留下的标 `ref`；要把值留下来的那两处——写字段与进容器——
//    给一份拷贝（`x + ""`）。这样脚本那边把名字交进来，交完那个名字还在自己手上；
// ② 顶层 `let` 可以在自己写的函数里重新赋值，脚本那边因此能把 `page` 放在顶层、每条用例覆盖一次；
// ③ 函数的参数名不许与顶层名字重名。
// 另外表达式不能跨行（`[` 与 `,` 后面换行就是编译错误），所以下面有几行写得比一行宽。
//
// 另一条是编译出来的：包的门面只准交出这个包自己定义的东西（编译器原话
// "Page 是别的包里的类型：门面只能导出这个包自己定义的类型"）。所以 `case_begin` 与
// `group_begin` 返回的是**页号**，脚本那边写 `wt.page_at(那一个号)` 取回句柄；
// 这一层的字段里也不放 `wt.Browser`，要浏览器句柄就当场叫 `wt.current_browser()`。
//
// 产物（都写进 begin_run 给的那个目录，每次跑先清再写）：
//   report.jsonl   一条用例一行，供机器读取
//   report.txt     终端那一份，供人阅读
//   case-3.png     失败那条用例的截图（3 是用例序号）
//   case-3.txt     同一条用例当时的整页文字

import wt from "libs/web_test"

const state_open:int = 0
const state_pass:int = 1
const state_fail:int = 2
const state_skip:int = 3

// JSON 里引号内的内容：转义交给标准库，两头加引号由自己完成。
// 空句柄写成空字符串那一个值，不写 null：读报告的一侧不必防两种空。
method quoted(ref value:string):string {
    if (value == null) {
        return "\"\""
    }
    return "\"" + text.escape_json(value) + "\""
}

method one_line(ref group_name:string, ref title:string, ref state_name:string, ms:int, code:int, ref msg:string, ref shot:string, ref body:string, ref steps_text:string):string {
    let head:string = "{\"group\":" + quoted(group_name)
    head = head + ",\"case\":" + quoted(title) + ",\"state\":\"" + state_name + "\""
    head = head + ",\"ms\":" + number.to_string(ms) + ",\"code\":" + number.to_string(code)
    head = head + ",\"msg\":" + quoted(msg) + ",\"shot\":" + quoted(shot)
    head = head + ",\"text\":" + quoted(body) + ",\"steps\":" + quoted(steps_text) + "}"
    return head
}

method state_name_of(state:int):string {
    if (state == state_pass) {
        return "pass"
    }
    if (state == state_skip) {
        return "skip"
    }
    return "fail"
}

method mark_of(ref state_name:string):string {
    if (state_name == "pass") {
        return "ok"
    }
    if (state_name == "skip") {
        return "--"
    }
    return "x"
}

// 跑一批用例的句柄：记录这一趟的输出目录、当前哪一组、当前哪一条的进度，
// 步骤、现场、报告都由它写入。脚本那边只调用它的成员，不访问里面的字段。
// en: The handle that runs a batch of cases: it keeps this run's output directory, which group is current and how far the current case has got, and writes the steps, the scene and the report. The script only calls its members and never touches those fields.
// 例：
// let started: tr.Run = tr.begin_run("nf.exe", "profile", "out")
export class Run {
    dir:string
    group:string
    group_url:string
    picks:list<string>
    steps:list<string>
    extra:list<string>
    lines:list<string>
    case_name:string
    case_index:int
    home_index:int
    begun:float
    state:int
    code:int
    message:string
    shot:string
    body_file:string
    order:int
    passed:int
    failed:int
    skipped:int

    // —— 挑选 ——

    // 空清单＝全部跑。名单由脚本那边从命令行解析出标题后交进这里。
    // en: An empty list means everything runs. The script picks those titles out of its arguments and hands them over here.
    // 例：
    // run.pick(["登录成功"])
    method pick(names:list<string>) {
        this.picks = names
    }

    // 判断这个标题在不在挑选名单里；名单为空时全部算在内。
    // en: Whether this title is on the picked list; with an empty list everyone is in.
    // 例：
    // console.print(run.picked("标题对了"))
    method picked(ref title:string):boolean {
        if (length(this.picks) == 0) {
            return true
        }
        for (one:string in this.picks) {
            if (one == title) {
                return true
            }
        }
        return false
    }

    // —— 组 ——

    // 一组只执行一次的开场：记下组名与起始网址，开一只标签页给组内建立前置用（登录那一类），
    // 返回那一页的**页号**（句柄由脚本那边 `wt.page_at(页号)` 取）。起始网址为空串时开 about:blank。
    // en: The once-per-group opening: it records the group name and the start URL and opens one tab to build the group's preconditions (logging in and the like). The answer is that page's number, and the script turns it back into a handle with wt.page_at. An empty start URL opens about:blank.
    // 例：
    // let home: int = run.group_begin("登录这一组", "https://example.com/")
    method group_begin(ref name:string, ref url:string):int {
        this.group = name + ""
        this.group_url = url + ""
        let start:string = url + ""
        if (start == "") {
            start = "about:blank"
        }
        console.print("")
        console.print(name)
        let made:wt.Page = wt.current_browser().open_page(start)
        this.home_index = made.index
        return made.index
    }

    // 收尾：关掉组里那只标签页。关不上不算错——组内的前置已经完成它的任务。
    // en: The group's ending: closes the tab the group opened. Failing to close it is not an error, since the precondition has already done its job.
    // 例：
    // run.group_end()
    method group_end() {
        if (this.home_index < 0) {
            return
        }
        let gone:wt.Page = wt.page_at(this.home_index)
        this.home_index = -1
        try {
            gone.close()
        } catch (bad_code, bad_msg) {
            console.print("  组那一只页签关不掉：" + bad_msg)
        }
    }

    // —— 条 ——

    // 一条用例的开头：在同一只浏览器里另开一只标签页（带着组内的登录态），返回它的页号。
    // 页号记在这里，所以失败时的截图与整页文字不需要脚本参与。
    // en: The start of one case: opens another tab in the same browser, so it carries the login the group established, and returns that page's number. The number is kept here, so the screenshot and the page text of a failed case need no help from the script.
    // 例：
    // let index: int = run.case_begin("标题对了")
    method case_begin(ref title:string):int {
        this.order = this.order + 1
        this.case_name = title + ""
        this.state = state_open
        this.code = 0
        this.message = ""
        this.shot = ""
        this.body_file = ""
        this.steps = []
        this.extra = []
        this.begun = time.elapsed()
        let start:string = this.group_url + ""
        if (start == "") {
            start = "about:blank"
        }
        let made:wt.Page = wt.current_browser().open_page(start)
        this.case_index = made.index
        return made.index
    }

    // 脚本里要记录哪一步就调用一次；通过的用例不打印步骤，失败的用例才列出已完成的步骤。
    // en: Write one of these for each step you want recorded; a passing case prints nothing, while a failed one lists the steps it got through.
    // 例：
    // run.step("点开登录")
    method step(ref label:string) {
        let ms:int = ((time.elapsed() - this.begun) * 1000.0) as int
        this.steps.add(label + "  " + number.to_string(ms) + " 毫秒")
    }

    // 标记这条用例通过；case_end 打印那一行并续写进报告。
    // en: Marks this case passed; case_end prints the line and appends it to the reports.
    // 例：
    // run.case_pass()
    method case_pass() {
        if (this.state == state_open) {
            this.state = state_pass
        }
    }

    // 结果以第一个 case_fail 为准；后来的调用（包括收尾里的失败）只追加一行，不改错误号。
    // en: The first case_fail decides the result, so a later assertion failure cannot overwrite the message from the try/catch that already recorded what happened.
    // 例：
    // run.case_fail(205, "没等到那个元素")
    method case_fail(bad_code:int, ref bad_msg:string) {
        if (this.state == state_skip) {
            return
        }
        if (this.state != state_fail) {
            this.state = state_fail
            this.code = bad_code
            this.message = bad_msg + ""
            this.take_scene()
        } else {
            this.extra.add(bad_msg + "")
        }
    }

    // 失败现场：一张截图、一份整页文字。抓取失败不另报错——报告里对应字段写空，
    // 终端那句说明没拿到的是哪一样（连不上浏览器这类失败本来就没有页面可截）。
    // en: The scene of a failure: one screenshot and the page's whole text. Failing to capture them is not reported separately, because the report says what the capture threw.
    // 例：
    // run.take_scene()
    method take_scene() {
        let stem:string = this.dir + "/case-" + number.to_string(this.order)
        let page:wt.Page = wt.page_at(this.case_index)
        try {
            page.screenshot(stem + ".png")
            this.shot = stem + ".png"
        } catch (bad_code, bad_msg) {
            this.shot = ""
            this.extra.add("截图没拿到：" + bad_msg)
        }
        try {
            let body:string = page.read_body_text()
            if (body == null) {
                body = ""
            }
            file.write(stem + ".txt", body)
            this.body_file = stem + ".txt"
        } catch (bad_code2, bad_msg2) {
            this.body_file = ""
            this.extra.add("整页文字没拿到：" + bad_msg2)
        }
    }

    // 一条用例的结尾：打印那一行、续进两份报告、关掉这一条自己的标签页。
    // en: The end of one case: prints that line, appends the two reports, and closes the tab this case opened.
    // 例：
    // run.case_end()
    method case_end() {
        let ms:int = ((time.elapsed() - this.begun) * 1000.0) as int
        if (this.state == state_open) {
            this.state = state_fail
            this.message = "这一条没报结果就走到了 case_end"
        }
        let title:string = this.case_name + ""
        let steps_text:string = text.join(this.steps, "; ")
        if (this.state == state_fail) {
            this.failed = this.failed + 1
            console.print("  x " + title + "  " + number.to_string(ms) + " 毫秒")
            for (one:string in this.steps) {
                console.print("      - " + one)
            }
            console.print("      ! " + this.message + "（" + number.to_string(this.code) + "）")
            for (two:string in this.extra) {
                console.print("      ! " + two)
            }
            if (this.shot != "") {
                console.print("      图 " + this.shot + "  页面 " + this.body_file)
            }
        } else {
            this.passed = this.passed + 1
            console.print("  ok " + title + "  " + number.to_string(ms) + " 毫秒")
        }
        this.write_line(ms, title, state_name_of(this.state), this.message, this.shot, this.body_file, steps_text)
        this.close_case_page()
    }

    // 跳过：报告里留一行，说明这条用例存在、本次未验证。跑不跑由脚本侧决定，
    // 这个方法只负责写下那一行。
    // en: Skips it: the report keeps one line so a reader can see the case exists and was not checked in this run.
    // 例：
    // run.skip("等那块后出来的", "这一条手工验")
    method skip(ref title:string, ref reason:string) {
        this.order = this.order + 1
        this.skipped = this.skipped + 1
        console.print("  -- " + title + "  跳过：" + reason)
        this.write_line(0, title, "skip", reason, "", "", "")
    }

    // 一份结果同时进两处：jsonl 一行、终端文本一行。
    // en: One result goes to two places at once: a line of jsonl for machines and a line of terminal text for people.
    // 例：
    // run.write_line(12, "标题对了", "pass", "", "", "", "")
    method write_line(ms:int, ref title:string, ref state_name:string, ref msg:string, ref shot_path:string, ref body_path:string, ref steps_text:string) {
        let group_name:string = this.group + ""
        let record:string = one_line(group_name, title, state_name, ms, this.code, msg, shot_path, body_path, steps_text)
        file.append(this.dir + "/report.jsonl", record + "\n")
        this.lines.add("[" + mark_of(state_name) + "] " + group_name + " / " + title + "  " + msg + "  " + shot_path)
    }

    // 关掉这一条自己开的标签页；关闭失败不另报错，收尾那一步会再处理一次。
    // en: Closes the tab this case opened. A failed close is not reported here; the ending looks once more.
    // 例：
    // run.close_case_page()
    method close_case_page() {
        if (this.case_index < 0) {
            return
        }
        let gone:wt.Page = wt.page_at(this.case_index)
        this.case_index = -1
        try {
            gone.close()
        } catch (bad_code, bad_msg) {
            this.extra.add("这一页关不掉：" + bad_msg)
        }
    }

    // 收场：打印总结那句、写终端文本报告、关闭浏览器；有失败用例时退出码为 1。
    // en: The ending: prints the summary, writes the terminal report and closes the browser. A failed case still leaves the exit code at 1, so a batch can tell.
    // 例：
    // run.finish()
    method finish() {
        let total:int = this.passed + this.failed + this.skipped
        let tally:string = "→ " + number.to_string(total) + " 条：过 " + number.to_string(this.passed) + "，挂 " + number.to_string(this.failed) + "，跳 " + number.to_string(this.skipped)
        console.print(tally)
        console.print("报告 " + this.dir + "/report.jsonl")
        this.lines.add(tally)
        file.write(this.dir + "/report.txt", text.join(this.lines, "\n") + "\n")
        wt.current_browser().close()
        if (this.failed > 0) {
            exit (1, "有 " + number.to_string(this.failed) + " 条用例没过")
        }
    }
}

// 起一趟：清并建那个输出目录、起浏览器。三个入参与脚本那边从命令行读到的那三个一一对上。
// en: Starts one run: the output directory is cleared and recreated and the browser is launched. The three arguments line up one for one with the three the script read off the command line.
// 例：
// let opening: tr.Run = tr.begin_run("nf.exe", "profile", "out")
export method begin_run(ref exe:string, ref profile:string, ref dir:string):Run {
    let place:string = dir + ""
    if (file.exists(place)) {
        file.remove_tree(place)
    }
    file.create_directories(place)
    let made:Run = new Run()
    made.dir = place
    // 起浏览器。返回来的那一只不存：`Browser` 里一个字段也没有，连接与进程握在 web_test
    // 包内那一份状态里，之后要句柄就当场叫 wt.current_browser()（这一层的字段里放不了
    // 别的包的类型）。
    wt.start(exe, profile)
    made.group = ""
    made.group_url = ""
    made.picks = []
    made.steps = []
    made.extra = []
    made.lines = []
    made.case_name = ""
    made.case_index = -1
    made.home_index = -1
    made.begun = time.elapsed()
    made.state = state_open
    made.code = 0
    made.message = ""
    made.shot = ""
    made.body_file = ""
    made.order = 0
    made.passed = 0
    made.failed = 0
    made.skipped = 0
    return made
}

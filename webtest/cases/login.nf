// 一份用例文件装一个组：这一份是"表单登录"那一组，五条用例。
//
// 跑法（仓库根；fixdir 是 webtest/fixtures 那一只目录的绝对路径，正斜杠写法）：
//
//	./nf.exe webtest/cases/login.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" /临时/profile /临时/out <仓库目录>/webtest/fixtures
//
// 第五个参数是要挑选的那一条的标题，不给就跑全部。
// 结果写入 /临时/out：终端一行一条，同一批内容写进 report.jsonl，失败的用例当场带截图与整页文字。
//
// 这一份里四条写法要说明：
// ① `run` 与 `page` 放在顶层，下面的函数直接访问它们：一份用例文件只执行一趟，顶层变量每条用例
//    覆盖一次，比把句柄逐个传进函数短（要传也可以：`ref` 是只读借用，借进去那个名字不作废）；
// ② 顶层 `let` 在自己写的函数里可以重新赋值，所以 `page = wt.page_at(...)` 这一句合法，
//    一条用例覆盖一次；
// ③ `list` 里那个位置只要读一次就是读走（装引用类型时进出都是交出；实测同一位置第二次读
//    当场 10092，`xs[0]` 同样会把那个位置读空），
//    所以每条标题只从 `titles[i]` 取一次，取下来接在 `title` 上，后面全用那个名字；
// ④ 重试写在脚本里："等那块后出来的"那一条跑两遍，第一遍故意只等
//    20 毫秒、必然挂在 205，第二遍等 3000 毫秒就过——报告里因此有同一句标题的两行。

import wt from "libs/web_test"
import tr from "libs/test_run"

method before_each(which:int) {
    run.step("每条用例开头先确认这一页开门")
    page.expect_title("登录")
}

method after_each(which:int) {
    run.step("把那句提示清掉")
    page.find_element_by_css("#tip").write_text("")
}

method one_case(which:int) {
    switch (which) {
        0 {
            run.step("填上用户名")
            page.find_element_by_css("#name").write_text("nef")
            run.step("填上密码")
            page.find_element_by_css("#pass").write_text("1234")
            run.step("点登录")
            page.find_element_by_css("#submit").click()
            run.step("看到欢迎那一句")
            page.expect_text_css("#tip", "欢迎, nef")
        }
        1 {
            run.step("填上用户名与不对的密码")
            page.find_element_by_css("#name").write_text("nef")
            page.find_element_by_css("#pass").write_text("不对的那一个")
            run.step("点登录")
            page.find_element_by_css("#submit").click()
            run.step("看到那一句拒绝")
            page.expect_text_css("#tip", "用户名或密码不对")
        }
        2 {
            run.step("等那半秒后自己消失的提示")
            page.find_element_by_css(".banner").expect_gone(3000)
        }
        4 {
            run.step("等那块 400 毫秒后才出来的方块")
            if (tries == 1) {
                page.wait_for_element_by_css("#arrived", 20)
            } else {
                let arrived:wt.Element = page.wait_for_element_by_css("#arrived", 3000)
                arrived.expect_text("后出来的那块", 1000)
            }
        }
    }
}

method group_login() {
    let home_index:int = run.group_begin("表单登录", start_url)
    wt.page_at(home_index).expect_title("登录")
    for (i:int in 0 until length(titles)) {
        let title:string = titles[i]
        if (!run.picked(title)) {
            continue
        }
        if (i == 3) {
            run.skip(title, "这一条要先把那句空账号的提示文案定了再开")
            continue
        }
        let passes:int = 1
        if (i == 4) {
            passes = 2
        }
        for (pass:int in 0 until passes) {
            tries = pass + 1
            page = wt.page_at(run.case_begin(title))
            try {
                before_each(i)
                one_case(i)
                run.case_pass()
            } catch (bad_code, bad_msg) {
                run.case_fail(bad_code, bad_msg)
            }
            try {
                after_each(i)
            } catch (bad_code2, bad_msg2) {
                run.case_fail(bad_code2, bad_msg2)
            }
            run.case_end()
        }
    }
    run.group_end()
}

let titles:list<string> = ["填对了就进得去", "错密码进不去", "那半秒后的提示自己消失", "空账号也点", "等那块后出来的"]
let picks:list<string> = []
let exe:string = args[0]
let profile:string = args[1]
let out_dir:string = args[2]
let fixdir:string = args[3]
let start_url:string = "file:///" + fixdir + "/form.html"
let page:wt.Page = null
let tries:int = 0
let run:tr.Run = tr.begin_run(exe, profile, out_dir)

if (length(args) > 4) {
    picks.add(args[4])
}
run.pick(picks)
group_login()
run.finish()

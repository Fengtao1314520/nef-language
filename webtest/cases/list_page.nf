// 一份用例文件装一个组：这一份是"列表页"那一组，三条用例，全过。
//
// 跑法（仓库根）与 login.nf 同一套四个参数：浏览器路径、个人资料目录、输出目录、fixtures 目录。
// 这一份存在的目的，是给 webtest/run_all.nf 提供第二份可执行的用例：一批只有一份文件时，
// "扫目录、逐份起进程、合并多份报告"这几条路径不会被走到。
//
// 三条都只做读取与点击，不故意失败：失败的样子在 login.nf 里已经有示范。

import wt from "libs/web_test"
import tr from "libs/test_run"

method before_each(which:int) {
    run.step("每条用例开头先确认这一页开门")
    page.expect_title("列表")
}

method one_case(which:int) {
    switch (which) {
        0 {
            run.step("数那三行")
            let rows:int = page.count_css(".row")
            if (rows != 3) {
                throw (206, "行数不对：读到 " + number.to_string(rows) + "，要是 3")
            }
        }
        1 {
            run.step("点第一行")
            page.find_element_by_css(".row").click()
            run.step("看到那一句点到第几行")
            page.expect_text_css("#tip", "点到第一行")
        }
        2 {
            run.step("读搜索框里本来那一段字")
            page.expect_text_css("#search", "默认文字")
        }
    }
}

method group_list() {
    let index:int = run.group_begin("列表页", start_url)
    wt.page_at(index).expect_title("列表")
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
        } catch (bad_code, bad_msg) {
            run.case_fail(bad_code, bad_msg)
        }
        run.case_end()
    }
    run.group_end()
}

let titles:list<string> = ["三行都在", "点第一行那句提示跟着变", "搜索框里那段默认文字"]
let picks:list<string> = []
let exe:string = args[0]
let profile:string = args[1]
let out_dir:string = args[2]
let fixdir:string = args[3]
let start_url:string = "file:///" + fixdir + "/list.html"
let page:wt.Page = null
let run:tr.Run = tr.begin_run(exe, profile, out_dir)

if (length(args) > 4) {
    picks.add(args[4])
}
run.pick(picks)
group_list()
run.finish()

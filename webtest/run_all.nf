// webtest/run_all.nf —— 一批用例跑完，汇总一份总报告。这一层本身也是用这门语言写的：
// 扫目录用 file.list，起进程用 process.start／wait，读每份报告用 file.read ＋ json.parse，
// 没有为这一层新添的语言能力。
//
// 跑法（在仓库根；cases 目录要给绝对路径，那份 fixtures 网址要用到它）：
//
//	./nf.exe webtest/run_all.nf "C:/Program Files/Google/Chrome/Application/chrome.exe" <仓库目录>/webtest/cases <一处空的输出目录>
//
// 第四个参数是 nf.exe 自己的路径（默认就叫 `nf.exe`，也就是用当前目录下那一份），
// 第五个是只跑哪几份的文件名（不带 .nf，逗号分隔，如 login,list_page）。
// cases 目录尾巴上不要带分隔符：fixtures 目录是拿它的上一截拼出来的。
//
// 产出文件：
//   输出目录/<文件名>/report.jsonl 与 report.txt 与那几张图   每份用例自己那一套，由 test_run 写
//   输出目录/all.jsonl                                        一条用例一行，多带一个 "file" 字段，最后一行是总结
//   输出目录/all.txt                                          同一份内容，给人看的摆法
// 每份用例分到独立的 profile（输出目录/profiles/<文件名>），避免两个用例进程争用同一个浏览器目录。
//
// 退出码：0 全过；1 有用例失败；2 无法完成（cases 目录不在、或某一份没有成功执行）。

let browser:string = args[0]
let cases_dir:string = args[1]
let out_root:string = args[2]
let nf_path:string = "nf.exe"
let want:string = ""
let fixdir:string = path.directory_name(cases_dir) + "fixtures"

let total:int = 0
let passed:int = 0
let failed:int = 0
let skipped:int = 0
let broken:int = 0
let all_rows:list<string> = []
let fail_notes:list<string> = []
let ran:list<string> = []

if (length(args) > 3) {
    nf_path = args[3]
}
if (length(args) > 4) {
    want = args[4]
}

method in_want(ref stem_name:string):boolean {
    if (want == "") {
        return true
    }
    let parts:list<string> = want.split(",")
    for (i:int in 0 until length(parts)) {
        let one:string = parts[i]
        if (one == stem_name) {
            return true
        }
    }
    return false
}

method text_of(slot:Json):string {
    if (slot == null) {
        return ""
    }
    let got:string = json.to_text(slot)
    if (got == null) {
        return ""
    }
    return got
}

// 一份用例跑完，把它那个位置 report.jsonl 读回来：数进总的账，行拼上文件名写进 all.jsonl。
// 拼那一行的时候不重新生成 JSON——只在开头插一个 "file" 字段，后面原样带着，
// 避免二次转义改动原文里的路径与中文。
method collect(ref target_dir:string, ref stem_name:string) {
    let report:string = target_dir + "/report.jsonl"
    if (!file.exists(report)) {
        console.print("  这一份没有报告：" + report)
        broken = broken + 1
        return
    }
    let rows:list<string> = file.read(report).split("\n")
    for (i:int in 0 until length(rows)) {
        let row:string = rows[i]
        if (length(row) < 2) {
            continue
        }
        let doc:Json = json.parse(row)
        if (doc == null) {
            console.print("  报告里有一行不是 JSON，跳过去")
            continue
        }
        let kind:string = text_of(json.at(doc, "state"))
        total = total + 1
        if (kind == "pass") {
            passed = passed + 1
        }
        if (kind == "skip") {
            skipped = skipped + 1
        }
        if (kind == "fail") {
            failed = failed + 1
            let title:string = text_of(json.at(doc, "case"))
            let why:string = text_of(json.at(doc, "msg"))
            let note:string = stem_name + " / " + title + "：" + why
            fail_notes.add(note)
        }
        let cut:string = row.slice(1, length(row))
        let quoted:string = text.escape_json(stem_name)
        let merged:string = "{\"file\":\"" + quoted + "\"," + cut
        all_rows.add(merged)
    }
    ran.add(stem_name + "")
}

if (!file.exists(cases_dir)) {
    exit (2, "那个目录不在：" + cases_dir)
}
// fixtures 目录是拿 cases 目录的上一截拼出来的，而用例脚本要把它写成 file:/// 那种网址，
// 那一种网址不接受相对路径（实测：给 webtest/cases 这种写法，页面标题读回来是
// `file:///webtest/fixtures/list.html`，浏览器打开的不是那一页）。所以在这里要求绝对路径。
if (!path.is_absolute(cases_dir)) {
    exit (2, "cases 目录要给绝对路径（浏览器那头的 file:/// 网址要用到它）：" + cases_dir)
}
file.create_directories(out_root)

let names:list<string> = file.list(cases_dir)
for (i:int in 0 until length(names)) {
    let name:string = names[i]
    if (!name.ends_with(".nf")) {
        continue
    }
    let stem:string = name.slice(0, length(name) - 3)
    if (!in_want(stem)) {
        continue
    }
    let one_out:string = out_root + "/" + stem
    let one_prof:string = out_root + "/profiles/" + stem
    let case_path:string = cases_dir + "/" + name
    let cmd:string = nf_path + " \"" + case_path + "\" \"" + browser + "\" \"" + one_prof + "\" \"" + one_out + "\" \"" + fixdir + "\""
    console.print("跑 " + stem)
    let one:Process = process.start(cmd)
    let code:int = one.wait()
    if (code != 0 && code != 1) {
        console.print("  这一份没跑成：退出码 " + number.to_string(code))
        broken = broken + 1
    }
    collect(one_out, stem)
}

let tally:string = "→ " + number.to_string(length(ran)) + " 份，" + number.to_string(total) + " 条：过 " + number.to_string(passed) + "，挂 " + number.to_string(failed) + "，跳 " + number.to_string(skipped)
console.print("")
console.print(tally)
for (note:string in fail_notes) {
    console.print("  x " + note)
}
all_rows.add(tally)
file.write(out_root + "/all.jsonl", text.join(all_rows, "\n") + "\n")
file.write(out_root + "/all.txt", text.join(all_rows, "\n") + "\n")
console.print("报告 " + out_root + "/all.jsonl")

if (broken > 0) {
    exit (2, "有 " + number.to_string(broken) + " 份没跑成")
}
if (failed > 0) {
    exit (1, "有 " + number.to_string(failed) + " 条用例没过")
}

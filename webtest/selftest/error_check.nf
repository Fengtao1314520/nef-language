// 两种起不来的情形，各钉一句：那个程序根本不存在（语言侧是 10111），
// 程序存在、但它不是浏览器、永远不写那个端口文件（库侧是 200）。
// 后一种还会一并验证"失败那一趟不留下还在跑的进程"：
// 收进程那一步在 start 里面，测完可以用任务管理器或 Get-Process 数一眼进程数。
//
// 跑法（一个参数：当作个人资料目录的路径，用完这一份程序自己删）：
//
//	./nf.exe webtest/selftest/error_check.nf /临时/目录

import wt from "libs/web_test"

method check_int(title:string, got:int, want:int):boolean {
    if (got != want) {
        console.print("不对  " + title, "给", got, "要", want)
        return false
    }
    console.print("过  " + title)
    return true
}

let bad:int = 0
let profile:string = args[0]

// 一、那个程序不在
let code_one:int = -1
try {
    let nowhere:wt.Browser = wt.start("C:/Windows/没有这个程序.exe", profile + "/one", 800, 800)
    nowhere.close()
} catch (c, m) {
    code_one = c
    console.print("  第一句说的是：", m)
}
let ok1:boolean = check_int("一、程序不在是 10111", code_one, 10111)
if (!ok1) {
    bad = bad + 1
}

// 二、程序在，但不是浏览器：它不会写那个端口文件，等到 limit 就报 200
let code_two:int = -1
try {
    let notabrowser:wt.Browser = wt.start("C:/Windows/System32/cmd.exe", profile + "/two", 800, 800)
    notabrowser.close()
} catch (c2, m2) {
    code_two = c2
    console.print("  第二句说的是：", m2)
}
let ok2:boolean = check_int("二、等不到端口文件是 200", code_two, 200)
if (!ok2) {
    bad = bad + 1
}

file.remove_directory(profile + "/one")
file.remove_directory(profile + "/two")
file.remove_directory(profile)
console.print("—— 自测跑完，不对", bad, "处")
if (bad > 0) {
    exit (1, "起不来那两种情形有 " + number.to_string(bad) + " 处不对")
}
console.print("全对")

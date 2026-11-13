// 示例程序八：三层调用里抛出，屏幕上打出整条调用链
// 用到：throw、`catch` 的第四个名字 `trace`（整条链的一份文本）、没人接住时自动打出的那一段链、
//      运行期自己抛出的那几条（下标越界 10031）也带同一条链、exit 之外的第四条结束方式（退出码 1）
// en: The eighth sample: a throw three levels down, with the whole call chain printed. Covers
// `throw`, the fourth `catch` name `trace` (the chain as one text), the chain the runtime prints
// when nobody catches the throw, that the errors raised by the runtime itself (index out of
// range, code 10031) carry the same chain, and the exit code 1 of that fourth way of ending.
//
// 运行方式：`nf samples/trace.nf`。程序最后那一次抛出没人接住，所以它按 `1` 结束——
// 屏幕上最后那几行缩进的就是调用链，一行一层。
// en: Run it with `nf samples/trace.nf`. The last throw is not caught, so the program ends with
// exit code 1; those indented lines at the end of the screen are the call chain, one per frame.

method read_word(words:list<string>, at:int):string {
    return words[at]              // 文本读出来是共用那一块，交出去不作废源位置；越界是 10031，由运行期当场抛出
}

method first_of(words:list<string>):string {
    return read_word(words, 9)    // 这一层只往下传
}

let shelf:list<string> = ["书包", "笔记本", "雨伞"]

// 第一段：接住它，并把整条链读出来打一遍（第四个名字 `trace`）。
let got:string = "没接住东西"
try {
    got = first_of(shelf)
} catch (code, msg, at, trace) {
    console.print("接住了:", code, msg)
    console.print("在哪一步:", at)
    console.print("哪几层传进来的:")
    console.print(trace)
}
console.print("接住之后照旧往下跑:", length(got))

// 第二段：不接住，让它一路走到最外面。屏幕上那一句"出错了…"下面就是同一条链。
// 这一句之后的内容不会执行，程序按退出码 `1` 结束。
// en: the second part leaves it uncaught: the chain follows the "error" line on the screen, and
// nothing after this statement runs - the program ends with exit code 1.
console.print("下面这一次没人接住")
console.print(first_of(shelf))

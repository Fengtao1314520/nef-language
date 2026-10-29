// 顶层那一号落在**静态槽**上：本文件的函数由别的文件的顶层调用时，本文件的顶层早就跑完了，
// 这一号还得读得到（§13.5、决定记录后九④）。所以它的析构点在程序尽头，不在这里。
let names:list<string> = ["甲", "乙", "丙"]

export method count():int {
    return length(names)
}

export method line():string {
    let out:string = ""
    for (s:string in names) {
        out = out + s
    }
    return out
}

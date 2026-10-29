// 示例程序二：读一个文件，统计每个长度上有多少行
// 用到：入口处的 args、file.read、split 的成员式写法、dict 的 contains／add、遍历时只交出键、try/catch、退出码从 100 往上
//
// `file`、`path`、`text`、`number`、`time` 是标准库组名，属于保留字，不能用作变量名。
// en: Reads a file and counts how many lines fall on each length. Covers args at the entry point, file.read, the member form of split, dictionary contains and add, iterating over keys only, try/catch and exit codes from 100 up. Note that file, path, text, number and time are standard-library group names and reserved, so they cannot be used as variable names here.

method tally(ref target:string, width:int) {
    let body:string = file.read(target)
    let lines:list<string> = body.split("\n")
    let by_length:dict<int, int> = {}

    for (line:string in lines) {
        let n:int = length(line)
        if (by_length.contains(n)) {
            by_length[n] = by_length[n] + 1
        } else {
            by_length.add(n, 1)
        }
    }

    for (size:int in by_length) {              // 只交出键，值按键自己取
        if (size <= width) {
            console.print("长", size, "的行有", by_length[size], "行")
        }
    }
}

if (length(args) == 0) {
    exit (100, "要给我一个文件路径")
}

const print_up_to:int = 20

try {
    let first:string = args[0]                 // 过户出 args 的第一个值
    tally(first, print_up_to)
} catch (code, msg) {
    console.print("读不了:", code, msg)         // 41 读不到、44 不是 UTF-8 都在这一支处理
}

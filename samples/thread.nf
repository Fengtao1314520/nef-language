// 示例程序九：五件事分给五条线程各算一件，再等它们各自把结果返回回来
// 用到：thread.start 的三种实参（值类型、文本、class 句柄）、Thread<T> 的 T 跟着被起的那个函数走、wait() 取回返回值、交出去的那个名字当场作废、被起的那个函数自己往屏幕写、一条线程里再起一条
// en: Five jobs split across five threads, each handing its result back. Covers thread.start with three kinds of argument (a value type, text, a class handle), Thread<T> where T follows the started function, wait() taking the returned value back, the old name of a given-away handle stopping work, a started function writing to the screen, and one thread starting another.

class Score {
    name:string
    points:int
}

// add_up 只收一个整数：值类型过去的是拷贝，外面的名字不受影响。
method add_up(limit:int):int {
    let sum:int = 0
    for (i:int in 1 until limit) {
        sum = sum + i
    }
    return sum
}

// named 收的是一段文本：交过去的是一段文本，返回的还是文本，所以句柄身上写 Thread<string>。
method named(part:string):string {
    return "甲" + part
}

// double 收的是一个 class 句柄：那一个对象交进去之后，外面的名字不再持有它。
// 对象里的字段归那一条线程改，改完返回一个数——这一版没有锁，两条线程之间只有一次交出与一次等回。
method double(box:Score):int {
    box.points = box.points * 2
    return box.points
}

// report 自己往屏幕写几行，写完返回它写了几行。这一段排在主线程那两行之前，
// 因为主线程等到它跑完才往下走；这里只让一条线程写屏幕，为的是这一份示例的输出每回相同。
method report(lines:int):int {
    let n:int = 0
    for (i:int in 1 until lines) {
        console.print("线程写的第", i, "行")
        n = n + 1
    }
    return n
}

// nested 是一条线程里再起一条：等的是它自己起的那一条，返回来的是两层算完的数。
method nested(n:int):int {
    let inner:Thread<int> = thread.start(add_up, n)
    return inner.wait() + 1
}

let total:Thread<int> = thread.start(add_up, 101)
let sign:Thread<string> = thread.start(named, "号")

let s:Score = new Score(name: "丙", points: 21)
let score:Thread<int> = thread.start(double, s)

let written:Thread<int> = thread.start(report, 4)
let deep:Thread<int> = thread.start(nested, 5)

// 三样返回值的类型各不相同：一个整数、一段文本、一个整数。
console.print(total.wait(), sign.wait(), score.wait())
console.print(written.wait(), deep.wait())

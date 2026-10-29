// 示例程序四：逆波兰表达式求值，非法表达式走统一的错误路径
// 用到：class 的字段与方法、方法里直接写字段名、接收者是只读借用但能就地修改、
//      switch 匹配文本且分支不贯穿、list 的下标读写（删元素按值删，
//      所以这里的栈用 sp 指示位置，不依赖 remove）、除 0 当场抛 10022、
//      text.to_int 与 length
// en: Evaluates reverse Polish notation and reports an invalid expression through the language error path. Covers class fields and methods, methods that name fields directly, a read-only borrow receiver that still mutates in place, switch over text without fall-through, list indexing and removal by value (the stack tracks position with sp instead of remove), division by zero as 10022 thrown on the spot, and text.to_int with length.

class Stack {
    cells:list<int>
    sp:int
    method push(v:int) {
        cells[sp] = v
        sp = sp + 1
    }
    method pop():int {
        if (sp == 0) {
            throw (120, "栈空了，取不到数")
        }
        sp = sp - 1
        return cells[sp]
    }
    method size():int {
        return sp
    }
}

class Calc {
    method run(ref expr:string):int {
        let s:Stack = new Stack(cells: [0, 0, 0, 0, 0, 0, 0, 0], sp: 0)
        let tokens:list<string> = expr.split(" ")
        for (t:string in tokens) {
            if (t == "") {
                continue
            }
            switch (t) {
                "+" { s.push(s.pop() + s.pop()) }
                "-" {
                    let b:int = s.pop()
                    let a:int = s.pop()
                    s.push(a - b)
                }
                "*" { s.push(s.pop() * s.pop()) }
                "/" {
                    let b:int = s.pop()
                    let a:int = s.pop()
                    s.push(a / b)
                }
                default { s.push(text.to_int(t)) }
            }
        }
        let rest:int = s.size()
        if (rest != 1) {
            throw (121, "式子没算完，栈里还剩 " + number.to_string(rest))
        }
        return s.pop()
    }
}

let c:Calc = new Calc()

console.print(c.run("1 2 + 3 *"))
console.print(c.run("10 4 - 2 -"))

try {
    console.print(c.run("8 0 /"))
} catch (code, msg) {
    console.print("算不动:", code, msg)
}

try {
    console.print(c.run("1 2 3"))
} catch (code, msg) {
    console.print("算不动:", code, msg)
}

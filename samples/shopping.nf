// 示例程序一：一个清单，把每样东西的字数加起来
// 用到：顶层是一层作用域、length 是函数、遍历交出的是只读借用、`ref` 参数只借不交出
// en: One list of items with the character count of each added up. Covers the top level being one scope, length as a function, what iterating hands over (a read-only borrow), and a ref parameter that borrows without taking.

let items:list<string> = ["书包", "笔记本", "雨伞"]
const want:int = 3                       // const 配值类型，合法

method count_chars(ref xs:list<string>):int {
    let total:int = 0
    for (s:string in xs) {              // s 是这个位置的只读借用，出作用域即失效
        total = total + length(s)
    }
    return total
}

const n:int = length(items)             // 内建函数按只读借用接收，items 仍然可用
console.print("共", n, "件，字符数合计:", count_chars(items))

// count_chars 的参数标的是 `ref`：清单借进去读一遍，返回后 items 仍然可用。
// 这里也可以写 console.print(items[0])，只是那个位置的值会被读走，此后它是 null。
console.print("算完了")

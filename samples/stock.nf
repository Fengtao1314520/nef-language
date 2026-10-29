// 示例程序六：一张库存表，每个键的值是一个 struct
// 用到：struct 的字段与字面量（字段必须列全）、dict 的按键读写、取之前先用 contains 查一次、
//      遍历交出的是只读借用、split、slice、text.to_int、struct 在内存里
//      是一整块：读出来是整份拷贝，写进去后那个位置归容器
// en: A stock table whose values are structs. Covers struct fields and literals with every field listed, dictionary reads and writes with a contains check before access, iteration handing over a read-only borrow, split, slice and text.to_int, and that a struct in memory is one block: reading copies it, writing hands that position to the container.

struct Stock {
    name:string
    qty:int
}

let rows:list<string> = "苹果:3 梨:7 桃:1 苹果:2 梨:5".split(" ")
let by:dict<string, Stock> = {}

for (r:string in rows) {
    let cells:list<string> = r.split(":")
    let what:string = cells[0]
    let num:int = text.to_int(cells[1])
    let key:string = what.slice(0, length(what))
    if (by.contains(key)) {
        by[key] = Stock{name: what, qty: by[key].qty + num}
    } else {
        by.add(key, Stock{name: what, qty: num})
    }
}

let total:int = 0
let most:int = 0
for (k:string in by) {
    let s:Stock = by[k]
    total = total + s.qty
    if (s.qty > most) {
        most = s.qty
    }
}
console.print("几种", length(by), "一共", total, "最多的一格", most)

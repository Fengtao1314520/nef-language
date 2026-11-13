// 示例程序五：一段文本统计词频，按第一次出现的顺序输出
// 用到：split、length、dict 的读写与 contains、清单的遍历交出的是只读借用，
//      按下标读那个位置交出的也是只读借用（读了不拿走，那个位置之后还在），
//      文本交给一个名字或写进容器的一个位置都是共用同一块，所以 `counts.add` 与 `order.add`
//      连着收同一个 key，key 不作废；可变的数据写进容器那个位置才叫接手，交出去的名字当场失效，
//      缺键是 10032，所以添加新词之前先用 contains 查一次
// en: Counts word frequencies in a text and reports them in order of first appearance. Covers split and length, dictionary reads and writes with contains, that iterating and indexing both hand over a read-only borrow (the position keeps its value), that text going into a name or a container position shares the same block - so counts.add and order.add can both take the same key, which stays usable - while a mutable value written into a container position is handed over and the old name stops working, and that a missing key is 10032, which is why new words are checked with contains first.

let body:string = "the quick brown fox jumps over the lazy dog the fox and the hound"
let words:list<string> = body.split(" ")

let counts:dict<string, int> = {}
let order:list<string> = []
for (w:string in words) {
    let key:string = w
    if (counts.contains(key)) {
        counts[key] = counts[key] + 1
    } else {
        counts.add(key, 1)
        order.add(key)
    }
}

let best:int = 0
for (w:string in order) {
    if (counts[w] > best) {
        best = counts[w]
    }
}
console.print("词数", length(words), "不同词", length(order), "最多的一共", best)
for (w:string in order) {
    if (counts[w] == best) {
        console.print("冠军", w, counts[w])
    }
    console.print(w, counts[w])
}

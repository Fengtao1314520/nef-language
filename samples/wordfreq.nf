// 示例程序五：一段文本统计词频，按第一次出现的顺序输出
// 用到：split、slice、length、dict 的读写与 contains、清单的遍历交出的是只读借用，
//      按下标读那个位置交出的也是只读借用（读了不拿走，那个位置之后还在），
//      借来的那一份要交给别人就先拷一份，所以 `counts.add` 与 `order.add` 交出去的都是 `slice` 出来的新文本，
//      把值交进容器的那个位置就是接手：容器接手，原来的名字当场失效，
//      缺键是 10032，所以添加新词之前先用 contains 查一次
// en: Counts word frequencies in a text and reports them in order of first appearance. Covers split, slice and length, dictionary reads and writes with contains, that iterating and indexing both hand over a read-only borrow (the position keeps its value), so a borrowed text is copied with slice before it goes into a container, and that a missing key is 10032, which is why new words are checked with contains first.

let body:string = "the quick brown fox jumps over the lazy dog the fox and the hound"
let words:list<string> = body.split(" ")

let counts:dict<string, int> = {}
let order:list<string> = []
for (w:string in words) {
    let key:string = w.slice(0, length(w))
    if (counts.contains(key)) {
        counts[key] = counts[key] + 1
    } else {
        let listed:string = w.slice(0, length(w))
        counts.add(key, 1)
        order.add(listed)
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

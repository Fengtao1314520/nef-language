// 示例程序五：一段文本统计词频，按第一次出现的顺序输出
// 用到：split、slice、length、dict 的读写与 contains、清单的遍历交出的是只读借用，
//      按下标读那个位置则是把值带走——所以下面统计时一律用遍历，不重复按下标读、
//      把值交进容器的那个位置就是过户：容器接手，原来的名字当场失效，
//      因此遍历交出的那个只读借用得先拷一份，才能交出、
//      缺键是 10032，所以添加新词之前先用 contains 查一次
// en: Counts word frequencies in a text and reports them in order of first appearance. Covers split, slice and length, dictionary reads and writes with contains, the difference between iterating (a read-only borrow) and indexing (taking the value out, which is why the statistics below only iterate), and that a missing key is 10032, which is why new words are checked with contains first.

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

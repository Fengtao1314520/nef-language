// 示例程序十：一份声明，按用到的类型各生成一份
// 用到：泛型 class、泛型 struct、泛型顶层函数、泛型 class 里自带一层类型参数的方法、尖括号里推得出的类型可以省、两只实例是两只类型（`is` 分得开）
// en: The tenth sample: one declaration, one instance per type it is used with. Covers a generic class, a generic struct, a generic top-level function, a method inside a generic class carrying its own type parameter, brackets left out where the argument types say what to fill, and two instances being two types that `is` tells apart.

// Bucket 是一只泛型 class：里面那一样东西是什么类型，由用它的名字写出来。
class Bucket<Item> {
    v:Item

    method peek():Item {
        return v
    }

    // 方法自己再带一层类型参数：尖括号里那两层各管各的，名字重了才算错。
    method again<Extra>(echo:Extra):Extra {
        return echo
    }
}

// Edge 是一只泛型 struct：两个字段各填一种类型。
struct Edge<From, To> {
    from:From
    to:To
}

// second 是泛型的顶层函数：两个实参同一种类型，填什么从这里就推得出来。
method second<Element>(first:Element, next:Element):Element {
    return next
}

let numbers:Bucket<int> = new Bucket<int>(v: 41)
let words:Bucket<string> = new Bucket<string>(v: "甲")

console.print(numbers.peek() + 1)
console.print(words.peek(), words.again("乙"))

// 两只实例是两只类型：装进盒子之后用 `is` 问，各认各的那一只。
// 名字上写着什么就问不出新东西（`numbers is Bucket<int>` 编译期就知道答案，那一问会被拦下），
// 所以这里先把那一只对象装进盒子，再按两种实例各问一遍。
let box:object = numbers as object
console.print(box is Bucket<int>, box is Bucket<string>)

let e:Edge<int, string> = Edge<int, string>{from: 1, to: "丙"}
console.print(e.from, e.to)

// 尖括号不用写的那一种与写了的那一种同一条用法。
console.print(second(2, 21), second<string>("丁", "戊"))

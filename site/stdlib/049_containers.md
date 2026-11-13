# 列表·字典·集合

`list`、`dict`、`hashset` 三种容器的成员动作不在这十五组里，也不用 `import`：它们是写在容器类型上的成员。

## 容器上的动作

| 作用 | `list` | `dict` | `hashset` |
| --- | --- | --- | --- |
| 添加一个元素 | `a.add(9)` | `d.add("k", 1)` | `s.add(9)` |
| 删除一个元素 | `a.remove(9)` | `d.remove("k")` | `s.remove(9)` |
| 统计元素个数 | `length(a)` | `length(d)` | `length(s)` |
| 是否存在 | 没有 | `d.contains("k")` | `s.contains(9)` |
| 排先后 | `xs.sort()`，就地排序 | 没有 | 没有 |
| 查找位置 | `xs.index_of(9)`，找不到时返回 `-1` | 没有 | 没有 |
| 取全部键 | 没有 | `d.keys()`，返回 `list<K>` | 没有 |
| 取全部值 | 没有 | `d.values()`，返回 `list<V>` | 没有 |
| 取第一个位置与最后一个位置 | `xs.first()`、`xs.last()` | 没有 | 没有 |
| 全部清空 | `xs.clear()` | 没有 | 没有 |
| 读取一个键，不存在时返回默认值 | 没有 | `d.get(key, fallback)` | 没有 |
| 并、交、差 | 没有 | 没有 | `s.union(t)`、`s.intersect(t)`、`s.difference(t)`，各返回一个新的集合 |
| 整体倒序 | `xs.reverse()`，就地颠倒顺序 | 没有 | 没有 |
| 整个追加到末尾 | `a.add_all(b)`，`b` 不发生变化 | 没有 | 没有 |
| 去重 | `xs.to_set()`，返回 `hashset<T>` | 没有 | 它本身已经完成去重 |
| 取出一段 | `xs.slice(start, end)`，含起点不含终点 | 没有 | 没有 |
| 插入到某个位置 | `xs.insert_at(index, value)` | 没有 | 没有 |
| 从某个位置移除 | `xs.remove_at(index)`，返回被移除的元素 | 没有 | 没有 |
| 把一个键对应的那一份值摘出来 | 没有 | `d.take(key)`，返回之后桶里那个位置读回来是 `null` | 没有 |

`sort`、`index_of`、`get` 这三个成员都不抛出错误：`sort` 只是把已有的元素重新排列，不改变长度，也不改变元素内容；`index_of` 找不到时返回 `-1`；`get` 的键不存在时返回调用传入的默认值，不抛 10032。

`clear`、`reverse`、`add_all` 与并、交、差三个集合运算也不抛错误：它们操作的都是容器自己已经持有的内容。

需要位置参数的三个成员按下标检查：`insert_at` 的位置为负数或者大于长度时抛 10031 号；位置等于长度是合法的，表示追加到末尾。

`remove_at` 与 `slice` 的越界规则和方括号、`s.slice` 相同：下标为负、`起 > 止`、终点超出长度都抛 10031 号。在空的列表上调用 `remove_at` 也是这一个错误码。

三个 `remove` 找不到给定元素时也不做任何事，长度不变；被删除的元素当场释放：它原来所在的位置是它唯一的持有者。`dict.add` 命中已有的键时是覆盖，替换下来的旧值同样当场释放。需要留意的是下面三种抛出错误的情形：

| 位置 | 抛 | 消息 |
| --- | --- | --- |
| `a[i]` 读写越界，空表上取 `first()`、`last()`，以及 `insert_at`、`remove_at`、`slice` 的位置不对 | 10031 | `下标越界` |
| `d[k]` 读一个没有的键 | 10032 | `没有这个键` |
| 遍历途中对同一个容器增删、排序 | 10033 | `遍历途中改了容器` |

`d[k]` 写一个不存在的键是新增一项，不抛错误；要判断是否存在就写 `d.contains(k)`。这三句报错原文都能在[错误信息索引](/tools/060_errors)里按消息搜索到。

```nf
let items:list<string> = ["书包", "笔记本"]
items.add("雨伞")
items.remove("笔记本")

let seen:hashset<int> = {1, 2}
seen.add(2)

let ages:dict<string, int> = {}
ages.add("阿明", 31)

console.print(length(items), items[0], length(seen), ages.contains("阿明"))
```

```text
2 书包 2 true
```

```nf
let xs:list<int> = [3, 1, 2]
xs.reverse()
console.print(xs)
let head:list<int> = [0]
head.add_all(xs)
console.print(head)
xs.insert_at(0, 9)
console.print(xs, xs.remove_at(0))
let uniq:list<string> = ["甲", "乙", "甲"]
console.print(length(uniq.to_set()))
console.print(xs.slice(1, 3))
```

```text
[2, 1, 3]
[0, 2, 1, 3]
[9, 2, 1, 3] 9
2
[1, 3]
```

上面一行先输出还带着 `9` 的 `xs`，再输出 `9`。这条语句执行完，`xs` 只剩 `[2, 1, 3]`：`remove_at` 返回被移除的元素，后面的元素依次前移。

`length` 是函数，不写成 `items.length`：点号只用于读取字段和调用方法。

`sort` 与 `index_of` 只在 `list` 上提供：`xs.sort()` 就地排好，之后读取 `xs` 看到的就是排完的顺序；`xs.index_of(value)` 返回元素所在的位置，找不到时是 `-1`。

`list` 没有 `contains`，要询问是否存在可以写 `xs.index_of(v) >= 0`，或者直接改用 `hashset`。

```nf
let xs:list<int> = [5, 3, 9, 1]
xs.sort()

let ws:list<string> = ["水", "风", "秋"]
ws.sort()

console.print(xs, ws[0], xs.index_of(9), xs.index_of(7))
```

```text
[1, 3, 5, 9] 水 3 -1
```

`sort` 对文本按 UTF-8 的字节序排列，所以“水”排在“秋”与“风”前面。要按别的规则排序，得自己写比较与挑选的逻辑（当前这一版只按元素自身的先后顺序排）。

取头尾、清空、带默认值地读取键，以及并、交、差三个集合运算放在一起看：

```nf
let xs:list<int> = [3, 5, 9]
console.print(xs.first(), xs.last())
xs.clear()
console.print(length(xs))

let scores:dict<string, int> = {}
scores.add("语文", 92)
console.print(scores.get("语文", 0), scores.get("数学", 0))

let a:hashset<int> = {1, 2, 3}
let b:hashset<int> = {3, 4}
console.print(length(a.union(b)), length(a.intersect(b)), length(a.difference(b)))
console.print(a.union(b).contains(4), a.intersect(b).contains(1))
```

```text
3 9
0
92 0
4 1 2
true false
```

`first()`、`last()` 读取的就是 `xs[0]`、`xs[length(xs) - 1]` 这两个位置，读取规则也相同：读完那个位置还是原样，同一位置可以读第二遍。

要留住那一个值：文本直接接给一个名字就行，可变的那一种用 `remove_at` 摘出来（见[值与存放](/reference/038_memory)）。

`clear()` 是就地清空，每一个位置都归还给堆。

`d.get(key, fallback)` 两个位置都只读一次：命中读的是表里那个位置，没有命中读的是调用处递进来的默认值，读回来的那一份是借来的，接不进名字；键与默认值两边的原名在这条调用之后都还能用。并、交、差三个运算各返回一个新的集合，进去的两个集合不发生变化。

`add`、`remove`、`contains` 只作为容器的成员存在，不占用全局名字，自己的 `class` 里可以照原样写 `method add(...)`。

键不必是固定的字面量。下面的例子统计每个词出现的次数。

遍历交出的是只读借用，文本接给一个名字是共用同一块，直接拿它当键就行。

```nf
let words:list<string> = "秋 风 秋 水 秋".split(" ")
let n:dict<string, int> = {}
for (w:string in words) {
    let key:string = w
    if (n.contains(key)) {
        n[key] = n[key] + 1
    } else {
        n.add(key, 1)
    }
}

console.print(length(n), n["秋"], n["风"], n["水"])
```

```text
3 3 1 1
```

## 归属

| 位置 | 语义 |
| --- | --- |
| 被操作的那个容器（`items.add(...)` 和 `length(items)` 里的 `items`） | 只读借用，调用之后持有者不变 |
| 传进去的值（`box.add(c)` 里的 `c`） | 值类型拷贝，可变的那一种交出，文本共用同一块 |
| 读一个位置（`box[0]`、`xs.first()`、`d.get(键, 默认)`） | 不拿走：读到的那一份归容器持有，同一位置可以读第二遍 |
| 把值从容器里拿走（`box.remove_at(0)`、`json.take(doc, 键)`） | 交出，那个位置之后是 `null`；`remove_at` 还会让后面的位置依次前移 |

读不拿走，拿走要明写。

从容器里拿走一个值，走的是 `xs.remove_at(0)` 与 `json.take(doc, 键)` 这两条。删除那三条（`xs.remove(值)`、`d.remove(键)`、`s.remove(值)`）只删，不当作值交出来，写了接住它的名字会被编译器拦下。

要留住读到的一份：文本直接接给一个名字，两处拿着同一块；可变的那一种要用 `xs.remove_at(位置)` 把那一个摘出来（删除那几条不交出值）。

存放值类型的容器（`list<int>`、`hashset<byte>`）进出都是拷贝，每个位置始终有值。

## dict 的方括号

| 写法 | 结果 |
| --- | --- |
| `d["有的键"]` | 返回值 |
| `d["没有的键"]` | 抛出 10032 号，能 `catch` |
| `d["没有的键"] = 1` | 新增一项 |
| `d["已有的键"] = 1` | 修改值 |

读取会抛出错误，写入会新增一项，所以计数要先用 `contains` 判断，再分支处理。

## 遍历

`for (v in c)`：`list` 交出元素，`hashset` 交出成员，`dict` 只交出键。交出的都是只读借用：可以读取，也可以调用方法，但不能把它们交出去。

元素是文本时不算交出：接给一个名字是共用同一块，容器那个位置不动。`dict` 与 `hashset` 不保证顺序；遍历途中对被遍历的容器做增删会抛出 10033 号。细节在[容器](/reference/037_containers)。

```nf
let scores:dict<string, int> = {"甲": 90}
scores.add("乙", 71)

for (k:string in scores) {
    console.print(k, scores[k])
}
```

## 整体打印

整个容器直接交给 `console.print` 就可以：`list` 用方括号，`dict` 与 `hashset` 用花括号，每个元素按它自己的类型输出，嵌套的容器接着展开。文本在容器里要带一对引号（否则空文本显示不出任何内容），没有指向任何对象的元素输出 `null`。

```nf
struct Point {
    x:int
    y:int
}

let pts:list<Point> = [Point { x: 1, y: 2 }]
let nested:list<list<int>> = [[1, 2], [3]]
let ages:dict<string, int> = {"阿明": 31}

console.print(pts)
console.print(nested, ages)
```

```text
[Point { x: 1, y: 2 }]
[[1, 2], [3]] {"阿明": 31}
```

`dict` 与 `hashset` 的输出顺序与放进去的先后没有关系：哈希表不记录插入顺序。

上面两行里 `list` 的元素按下标排列，就是放进去的顺序；`{"阿明": 31}` 是 `dict`，键的顺序同样不作保证，只是只放了一个元素，看不出来。

格式的全部细节，以及 `object` 如何拆开输出，写在[屏幕输出与输入](/stdlib/045_console)的形状表里。

## 这一行过不过

`items.add(items[0])` 这一行能不能通过检查？要想清楚 `items` 是只读借用的对象，而 `items[0]` 交出的那个值归属于谁。

## 参见

- [值的所有权](/learn/026_ownership)：交出与只读借用两条规则
- [容器](/reference/037_containers)：三种容器的类型写法与越界规则
- [字节块](/stdlib/047_bytes)：同样按位置读写，但每个位置只占一个字节的类型

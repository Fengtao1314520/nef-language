# 容器类型

要存一组值，Nef 提供三种容器：`list`（有序列表）、`dict`（键值映射）、`hashset`（哈希集合）。

三者都是引用类型，长度都能变化。`list` 与 `dict` 用方括号读写，`hashset` 不支持方括号，只对外提供 `add`、`remove`、`contains` 三个方法，要判断成员是否存在就调用 `contains`。

这一课介绍三种容器各自的常用操作，并说明最容易混淆的一点：方括号在 `dict` 上的读取行为与写入行为不同。

## `list`：有序列表

`list` 按插入顺序保存元素，允许重复，下标从 `0` 开始。元素类型写在尖括号中：

```nf
let items:list<string> = ["书包", "笔记本"]
items.add("雨伞")

console.print(items[0])       // 书包
console.print(length(items))  // 3
```

前两行创建一个含两个元素的列表，随后追加一个元素；`items[0]` 读取第一个元素，`length(items)` 返回元素个数。

元素个数由函数 `length(items)` 给出，不写成 `items.length`。写成 `items.length` 是编译错误，提示为 `list<string> 上没有 length`。

点号在这门语言里只表示一件事：访问字段或者调用方法。长度不属于任何一个元素，因此使用函数形式。

## `dict`：键值映射

`dict` 保存键到值的映射：

```nf
let ages:dict<string, int> = {"阿明": 31}
ages["小云"] = 27

console.print(ages["阿明"], ages.contains("小云"))   // 31 true
```

方括号在 `dict` 上有两种不同的行为，这一点最容易混淆：**读取**一个不存在的键会抛出错误，**写入**一个不存在的键会新增一条记录，键已存在时改的是原有的值。

因此计数需要先判断再分支：

```nf
let by_length:dict<int, int> = {}

for (word:string in ["天", "地", "东西", "南北"]) {
    let n:int = length(word)
    if (by_length.contains(n)) {
        by_length[n] = by_length[n] + 1
    } else {
        by_length.add(n, 1)
    }
}

console.print(by_length[1], by_length[2])   // 2 2
```

这段代码比 `dict[n] = dict[n] + 1` 多出三行。

多写这三行，是为了键名拼错时不会悄悄多出一条记录。如果读取不存在的键也自动创建条目，拼错的键名会被写入字典，直到查询不到时才暴露，而且已经查不出错误记录是哪一行写入的。

`dict[k]` 在键不存在时抛出 10032 号错误；新增必须使用 `add`；判断是否存在使用 `contains`。

如果想把这段逻辑放进一个函数：

```nf
method bump(d:dict<int, int>, k:int) {
    if (d.contains(k)) {
        d[k] = d[k] + 1
    } else {
        d.add(k, 1)
    }
}
```

`bump` 接收 `dict`，而 `dict` 是引用类型：传入就是交出所有权，调用方的名称之后无法继续使用（规则见[值的所有权](/learn/026_ownership)）。

这类辅助函数要么写在调用处所在的同一个文件中，要么就在调用处保留这三行分支。

## `hashset`：哈希集合

`hashset` 只记录成员是否出现过，不保存值，也不保证顺序：

```nf
let seen:hashset<int> = {1, 2}
seen.add(2)

console.print(length(seen), seen.contains(7))   // 2 false
```

`add(2)` 添加的是一个已经存在的成员，长度仍为 2。

`list` 没有 `contains` 方法。需要在列表中查找某个值时自行遍历；查找频繁时改用 `hashset`。

这不是疏忽。列表要保住顺序，判断某个值是否存在就得逐个比较，最坏情况要比较到最后一个元素。如果这个判断也叫 `contains`，调用处就看不出开销随长度增长。`hashset` 与 `dict` 的 `contains` 先计算一次散列再定位，比较的只是对应位置上的键。

## 空容器的初始化

空的 `{}` 是 `dict` 还是 `hashset`，由左侧的类型标注决定。声明本来就要求写类型，因此不另设区分记号：

```nf
let d:dict<string, int> = {}
let s:hashset<int> = {}
```

## 越界访问与缺失键

下标越界、查询不存在的键都会在运行时抛出错误（10031 号、10032 号）。抛出的形式与手写 `throw` 完全相同，可以被 `catch` 捕获。

它们不会返回 `0`，也不会返回 `null`。

```nf
let ages:dict<string, int> = {"阿明": 31}

if (ages.contains("老赵")) {
    console.print(ages["老赵"])
} else {
    console.print("没这个人")
}
```

```text
没这个人
```

## 遍历容器

`for (v in c)` 三种容器都支持，但每轮交出的内容不同。

`list` 交出元素，`hashset` 交出成员，`dict` **只交出键**：需要值时在循环内用 `d[k]` 读取。文本交出码点。

```nf
let scores:dict<string, int> = {"甲": 90}
scores.add("乙", 71)

for (k:string in scores) {
    console.print(k, scores[k])
}
```

交出的值是只读借用，不能转交出去，本轮结束就失效。

`dict` 与 `hashset` 不保证遍历顺序。需要固定顺序时，另外用一个 `list` 保存键。

> 注意：遍历过程中对被遍历的容器执行 `add` 或 `remove`，会当场抛出 10033 号错误。需要边遍历边修改时，先把键复制到一个 `list`，遍历结束后再按这些键修改。

## 可作为键的类型

可以作为 `dict` 的键和 `hashset` 的成员的有：`boolean`、`int`、`byte`、`unicode`、`string`、`enum`，以及 `class` 和 `object`。

后两者按对象身份判断是否相同。

`float` 不能作为键：`NaN` 不等于自身，作为键将永远查询不到。

`struct` 与所有容器类型同样不能作为键：比较内容需要逐字段进行，而字段还能被修改，散列值无法保持稳定。

用不支持的类型作为键是编译错误，不会等到运行时才发现查询永远无法命中。

## 容器元素的所有权语义

容器存放引用类型（`string`、`class`、`object`、其他容器）时，取出和放入都是**交出**：

```nf
let names:list<string> = ["第一", "第二"]
let first:string = names[0]

console.print(first)
console.print(names[0] == null)
```

```text
第一
true
```

这个位置上的值已经归属 `first`，再次读取得到 `null`。需要放回去就写 `names[0] = first`，这又是一次交出。

> 注意：这里的取出不是拷贝。容器最常见的用途是集中存放若干 `class` 对象，这些对象往往较大，还持有自己名下的其他值：如果每次下标访问都拷贝一份，拷贝的层数和开销都难以估计。读走一份就交出一个持有者，需要理解的就是“读走”和“变空”两点。

存放值类型（`int`、`float`、`struct`）时，进出都是拷贝，每个位置始终有值，不存在上述问题。

## 嵌套容器

```nf
let rows:list<dict<string, int>> = [{"甲": 1}, {"乙": 2}]

console.print(length(rows), rows[1]["乙"])   // 2 2
```

在类型位置上连写的 `>>` 读作两个尖括号的闭合，中间不需要空格（在表达式中 `>>` 表示右移运算符）。

同一个位置需要存放不同类型的值时，使用 `list<object>`，取出后再拆箱，见[自定义类型](/learn/029_types)。

## 练习

用 `dict` 统计诗句中每个字出现的次数：`["床前明月光", "疑是地上光"]`，把两行拼接后逐字遍历。

动手之前先确认两点：`length("床前明月光")` 为什么是 5 而不是 15；“光”出现两次时，应该使用 `add` 还是赋值。

## 本节要点

`list` 保存顺序、`dict` 按键存取、`hashset` 记录是否出现；读取不存在的键会抛出错误，而不是返回空值；引用类型的元素取出后原位置变为 `null`。

下一节把若干字段组合成一个自定义类型。

# 判断与循环

给一段程序加两个功能：金额超过 100 的记录单独输出一句“大额”，以及把所有条目从头到尾累加出总数。

前者是条件分支，后者是循环。这一课写下 `if`、`for`、`while` 和 `switch`，同时说明条件位置为什么只接受布尔值。

## 条件表达式必须是 boolean

在其他语言里可能写过 `if (list)`、`if (0)`、`if (name)`，然后需要回忆那门语言的判定规则：空字符串算不算假？空容器呢？

这里没有这套“真值语义”需要记忆：`if`、`while` 的括号内必须是一个 `boolean`。数字、文本和容器都不行，写进去是编译错误。需要判断就在当场比较出一个布尔值：

```nf
let count:int = 0

if (count != 0) {
    console.print("有货")
} else {
    console.print("卖完了")
}
```

```text
卖完了
```

`count` 是 `int`，它本身不是条件；`count != 0` 才是条件，结果是 `boolean`。

`&&`、`||` 只能用于 `boolean`，两者都是短路运算：`&&` 左侧为假就不计算右侧，`||` 左侧为真就不计算右侧。`!` 取反。

没有三元运算符。`n > 0 ? "正" : "负"` 不被接受，需要按条件取值就写完整的 `if` 与 `else`，或者把判断放进一个函数。

## 多分支条件

需要分出三个区间时，用 `else if` 依次续写：

```nf
let n:int = 9

if (n > 9) {
    console.print("大了")
} else if (n == 9) {
    console.print("正好")
} else {
    console.print("还差")
}
```

```text
正好
```

`else if` 可以连续写多条，最后的 `else` 可写可不写。

## 计数循环

计数循环只有 `until` 一种形式，右端不包含：

```nf
let total:int = 0

for (i:int in 0 until 5) {
    total = total + i
}

console.print(total)          // 10
```

`0 until 5` 覆盖 `0、1、2、3、4`，共五次。需要把 `5` 也包含进去就写 `0 until 6`。

没有包含右端的形式，也没有 `..` 记号：“上界到底取不取”在这里只有一个答案。

循环变量 `i` 同样需要标注类型，它只在循环体内有效，在循环之外使用 `i` 是错误。计数循环交出的是 `int`，所以这个位置标注 `int`；标注其他类型是编译错误：标注的类型必须与本轮交出的值同类型，类型之间不自动转换。

## 遍历容器

容器和文本都可以直接遍历：

```nf
let names:list<string> = ["阿明", "小云", "老张三点"]
let short:int = 0

for (name:string in names) {
    if (length(name) <= 3) {
        short = short + 1
    }
}

console.print(short, length(names))   // 2 3
```

`length(name)` 计算的是码点个数，一个汉字算一个。三个名字里 `阿明` 与 `小云` 都是两个字，`老张三点` 是四个字，因此满足条件的有两个。

循环变量标注的 `string` 必须与 `names` 中的元素类型一致：`list<T>` 交出 `T`，`dict<K,V>` 只交出键 `K`，文本交出 `unicode`。标注成其他类型是编译错误，错误信息仍是“类型之间不自动转换”。

遍历交出的 `name` 是**只读借用**：可以读取它，也可以调用它的方法，但不能转交给其他名称或函数，本轮结束就失效。循环结束后，`names` 中的元素一个也不少。

至于遍历时为什么不交出所有权，[值的所有权](/learn/026_ownership)中“循环体的两轮检查”一节已经说明：第一轮把它转交出去，第二轮这个名称就没有可以指向的值。

## while、break、continue

重复次数未知时使用 `while`：

```nf
let n:int = 0
let kept:int = 0

while (n < 6) {
    n++
    if (n == 3) {
        continue
    }
    if (n == 5) {
        break
    }
    kept = kept + 1
}

console.print(n, kept)        // 5 3
```

`n` 加到 3 的那一轮由 `continue` 直接回到条件判断处，`kept` 没有累加；加到 5 的那一轮由 `break` 跳出整个循环，因此 `n` 停在 5，`kept` 累加到 3。

`continue` 回到条件判断处，`break` 跳出当前循环。

两者都只对最内层循环生效，也没有带标签的跳出。需要从两层循环中退出时，把外层放入函数，用 `return` 返回。

## switch 表达式

`switch` 把一个值与若干标签依次比较，命中哪个分支就执行哪个分支。分支执行完之后不会接着执行下一个分支，因此不需要跳出语句：

```nf
let score:int = 87

switch (score / 10) {
    9, 10 { console.print("甲") }
    8 { console.print("乙") }
    default { console.print("还要练") }
}
```

```text
乙
```

`87 / 10` 是整数除法，向零截断得到 `8`，因此执行的是 `8` 所在的分支。

可以作为判定对象的类型有六种：`enum`、`int`、`byte`、`unicode`、`boolean`、`string`。

标签必须是编译期可确定的值：数字、带负号的数、`enum` 成员、`true`、`false`、`const` 的值。

变量不支持，`null` 也不支持（能判定的六种类型都取不到它），区间（`0` 到 `10`）同样不支持，需要判断区间时改用 `if`。

一个分支可以并列多个标签，前面的 `9, 10` 就是这种形式。

## 枚举的完整匹配

以 `enum` 作为判定对象时，成员必须列全；漏掉成员又没有 `default` 是编译错误：

```nf
enum Door { open, closed, broken }

method describe(d:Door):string {
    switch (d) {
        Door.open { return "开着" }
        Door.closed { return "关着" }
        Door.broken { return "坏了" }
    }
}

console.print(describe(Door.closed))    // 关着
```

三个成员都已列出，因此不写 `default` 也能通过检查；每条路径都返回了值，结尾不需要再补 `return`。

这条规则什么时候起作用？在给 `Door` 增加第四个成员之后，所有漏掉该成员的 `switch` 都会被编译器一并报错。`enum` 本身在[第 9 课：接口与枚举](/learn/030_contracts)详细说明。

## 块与作用域

`{ }` 构成一个作用域：块内声明的名称在块外失效。块内不允许与外层已有的名称重名，也不提供“内层覆盖外层”的行为：

```nf
let x:int = 1

if (x > 0) {
    let y:int = 2
    console.print(x + y)      // 3
}
```

> 注意：外层已有 `x` 时，块内再写 `let x:int = 2` 是编译错误。不存在名称遮蔽，也就不会出现“修改了内层的 `x`，外层的值毫无变化”这类意图与结果不一致的代码。长函数中的中间变量因此需要各自有意义的名称。

## 练习

把上文 `while` 一节里的那段示例改写成 `for (i:int in 1 until 7)`，`continue` 与 `break` 的条件保持不变。

先推算最后输出的两个数值是否改变，再修改代码。

改完再思考一个问题：如果把 `break` 的条件从 `n == 5` 改成 `n == 7`，`kept` 最终是多少？

## 本节要点

条件位置只接受 `boolean`；计数循环只有 `0 until 上界`（右端不包含）；`break`、`continue` 只作用于最内层循环，跨层退出改用函数与 `return`。

下一节介绍容器：有序列表、键值映射与哈希集合，以及“取出一个元素，对应位置就变空”的行为。

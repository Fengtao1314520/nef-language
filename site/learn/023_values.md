# 变量与类型标注

下面这段程序要保存三种数据：一段文本、一个计数值、一个不再改变的上限。

给它们命名时，每个名称都要在声明它的那一行写上类型。

这一课先写 `let` 和 `const` 两种声明，再看省略类型标注之后会发生什么。

## 变量与常量

```nf
let name:string = "雨伞"
let count:int = 0
const limit:int = 3

count = count + 1
console.print(name, count, limit)
```

```text
雨伞 1 3
```

`let` 声明的名字可以再次赋值，`const` 声明的名字不可以。第四行把 `count` 加 1，因此输出是 `1`；`limit` 的值没有改变。

再写一句 `limit = 4`，编译器会当场拒绝，不需要等到运行时才发现。

## 类型标注的必要性

类型写在名称后面，用冒号分隔，例如 `let count:int = 0`。省略类型标注就是编译错误，编译器的原文是 `声明 count 必须标类型`。

编译器不从右侧的值推断类型。一开始会觉得每个声明都写上类型太重复；读到别人写的函数时，每个参数的类型就写在同一行上，不用跳到定义处查找。变量、参数、返回值和字段这四个位置遵守同一条规则。

## `const` 的限制

`const` 只能搭配值类型。

```nf-bad
const items:list<string> = ["书包"]   // 报：const 只能配值类型，这里是 list<string>
```

`list` 属于引用类型，`const` 管不住它：名字不能改，指着的对象里面的内容照旧能改；再把它传出去一次（参数没写 `ref` 时就是交出），这个名字当场失效，`const` 反而名不副实。需要常量就写 `const limit:int = 100`，需要容器就用 `let`。

## 赋值与自增

`x = v` 是赋值：值类型整份拷贝，引用类型交出所有权（完整规则见[值的所有权](/learn/026_ownership)）。`x++`、`x--` 只能用作独立语句，仅对 `int`、`float`、`byte` 有效。

```nf
let total:int = 0
total = total + 1
total++
console.print(total)     // 2
```

没有复合赋值运算符：`+=`、`-=`、`*=` 都不存在，要写成完整的 `total = total + 1`。

## 默认值

不存在内容未定义的值。`new` 创建的对象，字段直接取得自己类型的默认值：

| 类型 | 默认值 |
| --- | --- |
| `int`、`float`、`byte`、`unicode` | `0`、`0.0`、`0`、码点 0 |
| `boolean` | `false` |
| `string`、`list`、`dict`、`hashset`、`object` | `null` |

这张表针对对象字段。`let` 声明仍然必须给出初始值，需要暂时为空时，就显式写 `let s:string = null`。

## `null` 检查

`null` 表示一个引用没有指向任何值。数值类型永远不为空：`0` 是一个正常的数字，不是“没有”。

```nf
let maybe:string = null

if (maybe != null) {
    console.print("拿到了:", maybe)
} else {
    console.print("什么也没拿到")
}
```

```text
什么也没拿到
```

上面这个判空检查是给读代码的人看的，运行期避开 10092 号错误同样依靠它。编译器不跟踪空值状态。

`let m:string = null` 之后直接读取 `length(m)`，检查阶段会通过，运行到读取它的那一步才抛出 10092 号错误（`对着空句柄取成员或调用方法`）。先判空、再把 `null` 赋回去、然后继续读取，检查阶段同样会通过。

> 注意：判空是运行期避开 10092 号错误的唯一手段，不是编译期保证。编译器只看类型，不看某一个时刻句柄中是否还存着值。

## 无隐式类型转换

赋值和传参都不做隐式转换，类型不匹配就是错误，形式上再接近也不行：

```nf-bad
let total:float = 3       // 报：这里要的是 float，给的是 int；类型之间不自动转换
let ok:boolean = 1        // 报：这里要的是 boolean，给的是 int；类型之间不自动转换
```

需要转换就显式写出来：`3 as float`、`3.9 as int`。类型转换的规则在[数字与小数](/learn/024_numbers)里详细说明。

## 条件表达式必须为 boolean

换成别的语言，我们很可能写过 `if (items)`，意思是容器里有元素就算真。这里的条件位置只接受 `boolean`：

```nf
let items:list<string> = ["书包"]

if (length(items) > 0) {
    console.print("有东西")
}
```

```text
有东西
```

`length(items) > 0` 的结果是 `boolean`，这才是条件需要的值。编译器会拒绝 `if (items)` 这种写法，提示是“这里要的是 boolean”。

> 注意：Nef 没有真值语义，也就是不存在“某个值自动算作真”的规则。`0`、空字符串、空容器都不等于 `false`，它们只是普通的值。

## 练习

把本节开头那段程序里的 `count = count + 1` 移动到 `const limit:int = 3` 之前，运行 `nf check`，观察这条命令报出的内容。

然后把 `limit` 的 `const` 改成 `let`，判断程序能否通过检查，输出的第三个值是否改变。

## 本节要点

类型标注写在名称后面，四个位置都不能省略；`let` 可以再次赋值，`const` 不行，而 `const` 只与值类型搭配。

下一节处理数值：整数除法与取余的符号规则，以及运算超出范围时会发生什么。

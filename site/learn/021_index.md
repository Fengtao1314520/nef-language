# 开始之前

**Nef** 是一门静态类型的系统编程语言。

它的核心规则是：一个值在同一时刻只有一个持有者。这条规则写在类型系统里，所以程序不需要垃圾回收，也不会出现悬空指针。程序运行速度快，内存占用也小。

它适合写系统工具、网络服务、命令行程序，也适合写需要长期维护、多人协作的项目。

## 名字怎么读

**Nef** 读作 /nɛf/，近似“内夫”。

## 长什么样

下面是一段完整的 Nef 程序：

```nf
// 第一个程序
console.print("你好，nef")
console.print("1 + 1 =", 1 + 1)
```

把这段代码保存成 `hello.nf`，在终端运行：

```text
nf hello.nf
```

输出：

```text
你好，nef
1 + 1 = 2
```

`console.print` 把内容输出到屏幕上，多个参数之间自动用空格分隔，表达式先求值再输出。

还没有安装 `nf` 也没关系：[第 1 课：应用与入口](/learn/022_start)会先给出工具的安装步骤，再运行这三行。改完代码想先检查一遍时，使用 `nf check hello.nf`：这条命令只做检查，把发现的问题按文件、行号、列号逐行报出。

## 几个会反复出现的词

**类型标注**、**归属**、**字段**、**装箱**。

> 第一次读到不必记住，后面每次出现时都会再解释一遍。

**类型标注**见[第 2 课：变量与类型标注](/learn/023_values)，**归属**（一个值只有一个持有者）见[第 5 课：值的所有权](/learn/026_ownership)，**字段**与**装箱**都在[第 8 课：自定义类型](/learn/029_types)：装箱这个词在第 5 课先出现一次，到第 8 课才完整说明。

想一次读完它们的定义，见[术语表](/reference/043_glossary)。

## 随时能查的地方

- **[语法速查](/reference/034_syntax)** — 快速查看某个写法的形式
- **[语法参考](/reference/033_index)** — 某个规则的完整说明
- **[错误信息索引](/tools/060_errors)** — 查询编译器报错信息的含义
- **[标准库](/stdlib/044_index)** — 内置类型和函数的说明

## 七份完整程序

`samples/` 目录里有七份能直接运行的程序。每一份只用这门语言本身和标准库，学完一课就能挑一份跑。

| 程序 | 讲什么 | 怎么跑 |
| --- | --- | --- |
| `shopping.nf` | 一份购物列表，把每一项的字数加起来：顶层就是一层作用域、参数前的 `ref`、遍历交出的只读借用 | `nf samples/shopping.nf` |
| `linecount.nf` | 读一个文件，统计每个长度上有几行：命令行参数、`file.read`、`dict`、`try` 与 `catch` | `nf samples/linecount.nf 文件名` |
| `shapes.nf` | 形状与周长：`interface`、装箱与拆盒、`is` 判类型 | `nf samples/shapes.nf` |
| `rpn.nf` | 逆波兰式子求值：`class` 的字段与方法、`switch` 的分支不贯穿、除 0 抛出的错误被 `catch` 接住 | `nf samples/rpn.nf` |
| `wordfreq.nf` | 一段文本数词频，按第一次出现的顺序报出来：`split`、`slice`、`dict` 的按键读写 | `nf samples/wordfreq.nf` |
| `stock.nf` | 一张库存表，每一项存一个 `struct`：字面量要把字段列全，读出来是整份拷贝 | `nf samples/stock.nf` |
| `echo.nf` | 回显服务器：`net.listen`、`accept`、`recv_into`、`send`。没有线程，所以一次只服务一个连接 | `nf samples/echo.nf 9000`，再从另一个窗口连过去 |

最短的一份是 `shopping.nf`，去掉注释之后是这样：

```nf
let items:list<string> = ["书包", "笔记本", "雨伞"]
const want:int = 3

method count_chars(ref xs:list<string>):int {
    let total:int = 0
    for (s:string in xs) {
        total = total + length(s)
    }
    return total
}

const n:int = length(items)
console.print("共", n, "件，字符数合计:", count_chars(items))
console.print("算完了")
```

```text
共 3 件，字符数合计: 7
算完了
```

`count_chars` 的参数标了 `ref`，那份列表借进去看一遍，调用之后还在手上；`length(items)` 是标准库的函数，被操作的对象本来就按只读借用处理。这两条都在[第 5 课：值的所有权](/learn/026_ownership)里逐条说明。

## 接下来去哪

- **[第 1 课：应用与入口](/learn/022_start)** — 完成环境安装，运行上面的代码
- **[语法速查](/reference/034_syntax)** — 不按顺序阅读，直接查某个写法
- **[错误信息索引](/tools/060_errors)** — 编译器已经报错时直接查阅

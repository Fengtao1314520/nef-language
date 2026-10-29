# 多文件组织

一个文件里的代码长到不容易阅读时，就会想拆开。但拆分的理由只有一个：**别的地方需要使用同一段代码**。只因为“看起来太长了”而拆分，结果是修改一个缺陷时改了一处、漏掉另一处。

这一课把三个文件组成一个真实的小项目，然后说明 `import`、`export`、`shared` 各自的规则。

## 文件与命名空间

有些语言把文件或者目录当作命名空间：先给一组代码起一个名字，再按名字整块引入。

Nef 不是这样。一个 `.nf` 文件不是一个命名空间，它是一段程序；要使用另一个文件中的内容，就在自己文件的开头**按名称引入**，而被引入的文件必须先把这个名字 `export` 出来。给整个文件的名称加统一前缀，只在 `libs/` 的包中使用；工程内的文件之间没有这种形式。

好处很直接：任何一行用到的名称，都能在本文件开头的引入语句里查到来源。

## 示例：多文件项目

三个文件放在同一个目录中。`registry.nf` 保存一份日志，任何文件都能向这份日志添加一条记录：

```nf
// registry.nf
export shared log:list<string> = []

export method note(entry:string) {
    log.add(entry)
}

export method size():int {
    return length(log)
}
```

`louder.nf` 使用 `note`，并对外导出另一个名称：

```nf
// louder.nf
import { note } from "./registry.nf"

export method shout(entry:string) {
    note(entry)
}
```

`main.nf` 是在命令行上指定的入口文件：

```nf
// main.nf
import { note, size } from "./registry.nf"
import { shout } from "./louder.nf"

note("开始")
shout("中间")
note("结束")
console.print("一共", size(), "条")
```

```text
一共 3 条
```

三条记录进入同一份 `log`：`note` 直接记录两条，`shout` 把收到的文本交给 `note` 记录。

运行不需要先编译：

```text
nf main.nf
```

`nf build main.nf -o tally.exe` 产出的可执行文件运行后输出同一行。

## import 语法

| 事项 | 规矩 |
| --- | --- |
| 形式 | `import { 名字, 名字 } from "./相对路径.nf"`，没有 `import * as`，也没有只写一句 `import "./x.nf"`；引入 `libs/` 中的包是另一种写法，见[多个文件](/reference/042_modules)的“包”一节 |
| 路径 | 结尾要有 `.nf`，分隔符只接受 `/`（Windows 上亦然）。按本文件所在目录解析的前提是**以 `./` 或 `../` 开头**；不带前缀时按运行命令时所在的目录解析，不应这样写 |
| 以谁为基准 | 以 `./` 或 `../` 开头时，按**书写该语句的文件**所在目录解析，与执行命令时所在的目录无关 |
| 大小写 | 编译器不核对：当前 Windows 上写 `./UTIL.nf` 也能引入 `util.nf`，换到大小写敏感的系统上就是另一个文件。解析的规则见[多个文件](/reference/042_modules) |
| 改名 | `import { helper as h } from "./util.nf"`，之后本文件内只使用 `h` |
| 未 export 的名称 | 无法引入：`./util.nf 里没有对外交出 hidden` |
| 引入但未使用 | 花括号里列出的名称在本文件中一个都没有使用，这个名称的引入就是编译错误 |
| 文件不存在 | 编译报错，指出是哪条路径无法读取 |

`import` 属于**语句之外**的结构，只能出现在文件顶部，不能放进函数体内。

```nf-bad
// util.nf
method hidden(i:int):int { return i }

export method helper(i:int):int { return i }
```

```nf-bad
// main.nf
import { hidden } from "./util.nf"      // 报：./util.nf 里没有对外交出 hidden

console.print(hidden(1))
```

## 文件执行顺序

每个文件的顶层都会执行一次，规则有两条：被引入的文件先执行，入口文件最后执行。同一个文件被两条路径引入，也只执行一次。

```nf
// dep.nf
console.print("dep 的顶层先跑")

export const limit:int = 3
```

```nf
// main.nf
import { limit } from "./dep.nf"

console.print("入口的顶层后跑", limit)
```

```text
dep 的顶层先跑
入口的顶层后跑 3
```

因此“每个文件先把自己的准备工作做完”是自然发生的结果：`registry.nf` 顶层的 `export shared log:list<string> = []` 在 `main.nf` 的第一行之前就执行过了，`main.nf` 里没有任何一行代码需要担心日志尚未创建。

## `export` 与 `shared` 修饰符

没有 `export` 的名称只在本文件内可见，加上 `export` 才是对外导出。

`export shared` 只有一种用途：把一份**可变的**值跨文件共用。它必须与 `export` 一起出现，单独的 `shared` 不成立。

| 声明形式 | 含义 | 谁能使用 |
| --- | --- | --- |
| `export const limit:int = 3` | 一个不再改变的值 | 其他文件引入后各自持有一份拷贝 |
| `export shared log:list<string> = []` | 一份可变的容器，全程序只有这一个 | 其他文件引入后修改的是同一份 |
| `export method note(...)` | 一个函数 | 任何文件都可引入 |

`shared` 名称有两条附加规则，都在编译期检查：

**只能就地使用。** 把这个名称交给一个要拿走它的参数（参数没写 `ref`），或者赋给别的名称，都是编译错误，提示是 `bag 是 export shared 的名字，只能就地用，不能当参数交出去`。因为交出等于承诺“这份可变容器只有一个持有者”，而 `shared` 存在的意义正是让多处代码共用它。

**只有 `list`、`dict`、`hashset` 三种容器可以标注 `shared`。** 值类型本来就各持一份，文本不可变，都不需要这条规则。

## 循环依赖

两个文件互相引用时，编译器会拒绝，要求把公共部分移到第三个文件：

```nf-bad
// one.nf
import { two } from "./two.nf"    // 报：one.nf 与 ./two.nf 互相引用

export method one():int { return two() }
```

```nf-bad
// two.nf
import { one } from "./one.nf"    // 报：two.nf 与 ./one.nf 互相引用

export method two():int { return one() }
```

```nf-bad
// main.nf
import { one } from "./one.nf"

console.print(one())
```

两边各报一次，因为两种顺序都成立：谁先执行、谁后执行，编译器无法给出答案。把 `one`、`two` 都要使用的代码放入 `base.nf`，两个文件各自引入，循环依赖就解开了。

## 文件拆分策略

一段代码是否值得单独成文件，看两点。

**是否还有第二处使用。** 只有一处在使用，就留在原地。为了“以后可能会用到”提前拆分，结果是修改一个函数时要同时打开两个文件。

**它是否需要知道调用方是谁。** 一个计算税额的函数不需要知道调用方是命令行还是网页；一个会读取调用方全局状态的函数只能与该调用方绑定，无法拆开。

另外，`console.print`、`length`、`file.read` 这些标准库的十四个组名**不需要 import**，在任何文件中都能直接使用，它们也不参与“引入未使用”这条检查。

## 练习

把上面三个文件写成一份真实程序，然后给 `main.nf` 加一句 `import { size } from "./louder.nf"`。

`louder.nf` 中没有 `size`，先写下预期的报错内容，再运行核对。

再进一步：给 `registry.nf` 增加 `export method clear()`，清空 `log` 中的内容，并在 `main.nf` 的 `console.print` 之前调用它一次。`size()` 返回多少？

## 本节要点

文件不是命名空间：`export` 决定哪些名称可被引入，`import` 只能按名称引入，路径以引入语句所在文件的目录为基准，被引入文件的顶层先执行；两个文件互相引用会在编译期被拒绝。

第 11 课到此结束。后续查阅入口分别是[语法速查](/reference/034_syntax)、[标准库总览](/stdlib/044_index)与[错误信息索引](/tools/060_errors)。

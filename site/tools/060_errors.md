---
title: 错误信息索引
---

# 错误信息索引

程序里的错误分两种：编译不通过的，和运行期抛出的。前者程序根本不会开始运行，后者可以用 `try` / `catch` 捕获。

这一页按编译器输出的原文排序。看到一条报错，取其中辨识度最高的几个字搜索即可；下面“编译期错误速查”的每一行，都能在后面的主题小节里找到对应的错误写法。

## 报错消息的构成

一条报错占一行，格式固定：

```text
hello.nf 第 7 行第 10 列：bag 是 export shared 的名字，只能就地用，不能当参数交出去
```

依次是文件名、行号、列号，最后是一句说明。

报错出现在哪个阶段，决定这条消息是不是唯一的报错。词法与语法没有通过时只有最前面的一条，修正一条才看见下一条；静态检查阶段一次列出全部，一句一行，最后追加一行 `共 N 处不对`。两条流的分工与先后顺序写在[命令行工具](/tools/059_cli)。

## 编译期错误速查

下表里的名字（`x`、`one`、`Shape`、`Bag.add`）会替换为程序里实际使用的名字。

| 编译器原文 | 常见原因 | 修正方式 |
| --- | --- | --- |
| `声明 x 必须标类型` | 省了类型标注 | 写成 `let x:int = 1` |
| `这里要的是 float，给的是 int；类型之间不自动转换` | 跨类型赋值 | 显式转换：`1 as float` |
| `这里要的是 object，给的是 Note；类型之间不自动转换` | 忘了装箱 | `box.add(n as object)` |
| `null 装不进盒子：装箱装的是那一个值，空句柄没有值可装` | 写 `null as object` | 需要空的盒子时直接声明：`let p:object = null` |
| `list<int> 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去` | 整个容器、`Json` 或 `bytes` 装箱（`Json 装不进盒子：…`、`bytes 装不进盒子：…` 只是类型名不同） | 一个一个装进去；要放一段文本或一棵 JSON 树，先包进一个 `class` |
| `盒子里没有 list<int>：它进不了盒子，也拆不出来` | 从盒子里往回拆容器、`Json` 或 `bytes`（`as dict<string, int>`、`as Json`、`as bytes` 也报这一句） | 盒子里只能拆 `class`、`struct` 与值类型 |
| `is 的右边只能写盒子里可能装的那个类型` | 写 `x is list<int>`、`j is Json`、`b is bytes` 来询问 | 容器、`Json` 与 `bytes` 不会进入盒子，无法这样询问；只能询问 `class` 与 `struct` |
| `% 两边必须是同一类型：float 与 int` | 混合类型运算 | 两侧写成同一类型 |
| `length 数不了 int：只有 list、dict、hashset、string 与 bytes 这五样有长度` | 对别的类型使用 `length` | 只用于 `list`、`dict`、`hashset`、`string`、`bytes` 这五种类型；其他类型的长度需要自行统计 |
| `byte 只能装 0～255，这里是 300` | 字面量超出 `byte` 的取值范围，`let b:byte = 300` 与 `300 as byte` 都会报这一句 | 改用 `int`，或把值改到 0 到 255 之内 |
| `数字后面直接跟了名字（1e 认不出）` | 写了科学计数法 | 写全：`1_000_000_000` |
| `0644 这种 0 开头的写法不认：没有八进制，要写十进制就照实写` | 写了八进制字面量 | 写十进制，或用 `0x` |
| `移位量只能在 0～63 之间，这里写了 70` | 移位量写成了越界的常量 | `int` 用 0 到 63，`byte` 用 0 到 7；算出来的移位量无法在编译期判断，运行时抛出错误号 10021 |
| `移位量只能写 byte 或一个 0～255 的整数，这里是 int` | 移位量用了 `int` 的名字 | 换成 `byte` 的名字，或者直接写常量 |
| `text.to_int_in_base 的进制要写成一个数，不能是算出来的` | 进制参数给了变量或算式 | 直接写 `2`、`8`、`10`、`16` 中的一个数（`number.to_string_in_base` 规则相同） |
| `number.to_string_in_base 的进制只准 2、8、10、16，这里是 12` | 进制写了这四个之外的数 | 换成 2、8、10、16，可用的进制只有这四个 |
| `string 不可变，不能改其中一格` | 想原地改字符 | 拼接出新文本 |
| `中间那个位置读走就空了，改不到它的下一层：先给它绑一个名字` | 连续下标往下写或自增：`xs[0][0] = 7`、`a[0].v = 7`、`xs[0][0]++` | 先把中间的位置赋给一个名字，再改这个名字：`let row:list<int> = xs[0]` 之后 `row[0] = 7` |
| `这一层里已经有 x` | 同层重名 | 换个名字 |
| `x 与外层的名字重名：不做内层顶掉外层` | 块内遮蔽外层名称 | 改用一个含义明确的新名字 |
| `x 已经有同名成员（第 2 行）：字段与方法共用一个名字空间` | 字段与方法重名 | 二者改一个 |
| `没有 s 这个名字` | 用了循环变量的作用域之外 | 在循环内使用，或另存一份 |
| `import 了 h 却没用到` | 引入了没用上的名字 | 删除这条引入 |
| `./util.nf 里没有对外交出 hidden` | 对方没写 `export` | 请对方 `export`，或本文件自己实现 |
| `./libs/greet/greet.nf 在包里面：引包要写 import 包名 from "…"，花括号那一套是给工程里的文件用的` | 用花括号去引 `libs/` 里的包 | 改走整块引入：`import greet from "libs/greet"`，再写 `greet.Pair` |
| `包里的文件只能引同一个包里的文件：../../util.nf 不在这个包里面` | 包里的文件引入了工程里的文件 | 把这个文件移进包里；要用的数据由工程一侧作为参数传入 |
| `Pair 是别的包里的类型：门面只能导出这个包自己定义的类型` | 门面把依赖里的类型写进了参数、返回值、字段或别名 | 换成这个包自己定义的类型 |
| `greet 里的 priv 没有挂 export：包只把交出来的东西给别人用` | `greet.priv` 取的是包里没写 `export` 的那个 `const` | 请包作者补上 `export`；包没导出的数在包外面用不了 |
| `greet 里的 counter 是 export shared：它只在包里面共用，包外面读不到` | `greet.counter` 取的是一个 `export shared` 的名字 | 读不到，也写不进去：`shared` 是同一个包里几个文件共用的一个变量。要给外面用，由包里写一个函数把内容取出来 |
| `LIMIT 是包交出来的数，不能加括号调用` | 把包交出来的 `const` 当函数调用：`greet.LIMIT()` | 去掉括号，它就是一个数：`console.print(greet.LIMIT)` |
| `greet.LIMIT 是 const，声明之后不可改` | 往包交出来的数里写：`greet.LIMIT = 3` | 只能读。要一份外面能改的值，得由包里导出一个函数来改它自己的 `let` |
| `greet.LIMIT 是 const，不能自增减` | `greet.LIMIT++`、`greet.LIMIT--` | 同上：这一句改的仍然是一个 `const` |
| `引自己工程里的文件要写 import { 名字 } from "./util.nf"，不带花括号那一种是引包` | 不带花括号去引入本地文件 | 工程里的文件用花括号列出名字 |
| `libs/greet：花括号那一种引的是工程里的文件，路径要 ./ 或 ../ 开头；引包写 import 包名 from "libs/包名"` | 花括号后面写了不带前缀的裸路径 | 引入本地文件时补上 `./`；引入包时改用整块引入的写法 |
| `one 已经交出去了，这个名字不能再写` | 值已交出又用原名 | 在使用前留住一份，或重新读取 |
| `h.item 是字段，直接交出去之后那个位置就空了：先接给一个名字，再拿那个名字交出去` | 把 `class` 的引用类型字段直接当实参交给其他函数或方法（`sink(this.name)` 也报这一句） | 先 `let picked:Bag = h.item` 再交出 `picked`；文本可以交出拷贝：`sink(h.label + "")` |
| `s 是遍历交出的只读借用，不能交给参数` | 把遍历交出的值转交出去 | 改用下标 `items[i]` 取出之后再传 |
| `遍历交出的 o 是只读借用，不能把里面的东西交出去` | 把借来的对象里的引用类型字段读出来带走 | 逐层读取可以（`o.inner.v`）；要带走这个值，先用下标取出对应的对象 |
| `只读借用不能拆盒` | 对借来的值做 `as` | 先 `let one:object = box[i]` 再 `as` |
| `this 是方法接收者的只读借用，不能交出去` | `return this` | 在方法里 `new` 一个新对象返回 |
| `this 是方法接收者的只读借用，不能交给参数` | 把 `this` 当实参传出 | 交出需要的字段即可 |
| `bag 是 export shared 的名字，只能就地用，不能当参数交出去` | 转交了共享名称 | 就地读写，或写一个函数去操作它 |
| `ref 参数 s 是只读借用，不能交出去` | 把标了 `ref` 的参数 `return`、赋给别的名称、或者装箱 | 那个位置只是借来看；要自己留一份就当场造，或者在调用处把原名称交出来 |
| `ref 参数 s 是只读借用，不能交给要拿走的那个参数` | 把借来的值交给一个没写 `ref` 的参数 | 被调方的参数也标 `ref`，或者在调用处先接成一个名称 |
| `ref 参数 s 是只读借用，不能把里面的东西交出去` | 把借来的对象里的引用类型字段读出来带走 | 逐层读取可以（`o.inner.v`）；要带走这一格，先让调用方把对象交出来 |
| `ref 参数 s 是只读借用，不能再把里面的东西交给自己` | 从借来的容器里读走一个位置，接到自己这边的名称上 | 读一格用一格可以；要长期持有，就在调用方那里用下标取 |
| `值类型的参数不许写 ref：int 传进去本来就是整份拷贝` | 给 `int`、`float`、`byte`、`unicode`、`struct`、`enum` 的参数标了 `ref` | 去掉 `ref`：值类型传进去就是整份拷贝 |
| `Socket 是系统给的句柄，不许写 ref：被调方关一次，调用方那个名字就成了空句柄` | 给 `Socket`、`Listener`、`Process` 的参数标了 `ref` | 这三类照旧交出，或者把要用它的那几句写在调用处 |
| `C.draw 的第 1 个参数 ref 写法与接口 A 的声明不一致` | 接口声明与实现两边，`ref` 一边写一边没写 | 两边写一样；这一条与参数名必须和声明一致的规矩是同一条 |
| `g 标了返回类型 int，但有一条路没 return` | 存在未返回值的路径 | 补齐分支，或加结尾 `return` |
| `裸的 null 不能当实参` | `console.print(null)` | 先声明一个带类型的空句柄再传 |
| `null 只能和引用类型比，这里是 int` | 拿 `null` 比数值 | 与引用类型比较 |
| `P 的字段 y 没给值：字面量要把字段列全` | `struct` 字面量漏字段 | 把字段写全 |
| `new Counter 给了初值就得把字段给全，m 没给` | `new` 括号里漏字段 | 要么全给，要么整个括号留空 |
| `struct 的字段只能放值类型、string 或另一个 struct，这里是 Note` | `struct` 存了引用类型 | 这段数据改用 `class` |
| `struct A 的字段绕成了环` | 两个 `struct` 的字段互相指：`A` 里有 `B`，`B` 里又有 `A` | 拆开其中一处；`class` 的字段互相指是合法的，字段里存的是句柄 |
| `Circle 没写出接口 Shape 声明的 area` | 接口方法没实现 | 补上实现 |
| `Bag.add 的第 1 个参数该叫 v` | 实现里改了参数名 | 参数名照声明写 |
| `接口里的方法声明不写 method，只写签名` | 接口里写了 `method` | 去掉 `method` |
| `P 是 struct，写不了方法，所以不能继承带方法声明的接口 Shape` | `struct` 继承带方法的接口 | 换成 `class` |
| `枚举成员 b 与 a 同值 5` | 两个成员同一个数 | 改成不同的数值 |
| `switch 漏了 D 的成员，又没写 default` | 枚举没列全 | 补分支，或写 `default` |
| `Shape 是接口名，不能当类型写` | 用接口名声明变量或参数 | 用 `class` 或 `struct`，要混装就用 `object` |
| `int 不能 as 成 D` | 想把整数转成枚举 | 反过来：成员 `as int` 取出数值 |
| `float 不能当 dict 的键：NaN 不等于自己，这个键永远查不中` | 浮点当键 | 改用 `int`、`string` 等 |
| `throw 的码从 0 写到 9999，20000 落在语言自己那一段（10000 往上）` | 使用了属于语言的错误码区段 | 自己的码写在 `0` 到 `9999` |
| `throw 是语句，只能自成一条，不能出现在取值的位置` | 把 `throw` 当表达式 | 写成独立的一条语句 |
| `想要 "]"，实际看到 行尾` | 括号或引号没闭合 | 补上对应的闭合记号 |

## 类型与标注

```nf-bad
let f:float = 1    // 报：这里要的是 float，给的是 int；类型之间不自动转换
```

```nf-bad
let x = 1    // 报：声明 x 必须标类型
```

```nf-bad
console.print(7.5 % 2)    // 报：% 两边必须是同一类型：float 与 int
```

```nf-bad
console.print(length(5))    // 报：length 数不了 int：只有 list、dict、hashset、string 与 bytes 这五样有长度
```

```nf-bad
let b:byte = 300    // 报：byte 只能装 0～255，这里是 300
```

```nf-bad
let x:int = 1e9    // 报：数字后面直接跟了名字（1e 认不出）
```

```nf-bad
let n:int = 0644    // 报：0644 这种 0 开头的写法不认：没有八进制，要写十进制就照实写
```

```nf-bad
console.print(null as object)    // 报：null 装不进盒子：装箱装的是那一个值，空句柄没有值可装
```

需要空的盒子时不必装箱：`let p:object = null` 是合法声明，这个类型本身允许空值。

盒子只装 `class`、`struct` 与值类型：整个容器与 `Json` 进不去，也就从盒子里拆不出来。这两种情况都在检查阶段报出来，不会留到运行期抛出 10011。

```nf-bad
let j:Json = json.parse("1")
let o:object = j as object    // 报：Json 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去
console.print(o)
```

```nf-bad
let o:object = 5 as object
let xs:list<int> = o as list<int>    // 报：盒子里没有 list<int>：它进不了盒子，也拆不出来
console.print(xs)
```

```nf-bad
let j:Json = json.parse("1")
console.print(j is Json)    // 报：is 的右边只能写盒子里可能装的那个类型
```

`as` 的方向也会检查：`enum` 只能取出数值，不能从整数转回枚举。`100 as Err` 报 `int 不能 as 成 Err`。

## 名字与作用域

```nf-bad
let x:int = 1
let x:int = 2    // 报：这一层里已经有 x
```

外层已有 `x` 时，块内再声明一个 `x` 同样不允许，报的是 `x 与外层的名字重名：不做内层顶掉外层`。

同一个 `class` 里字段和方法共用一个名字空间：

```nf-bad
class C {
    x:int

    method x(v:int):int { return v }    // 报：x 已经有同名成员（第 2 行）：字段与方法共用一个名字空间
}
```

`import` 进来的名字必须真的用上：

```nf-bad
// util.nf
export method h(i:int):int { return i }
```

```nf-bad
// main.nf
import { h } from "./util.nf"    // 报：import 了 h 却没用到

console.print(1)
```

名字在来源文件里没有 `export` 时引入不了，报的是 `./util.nf 里没有对外交出 hidden`。

## 包

`libs/` 里的包只能用整块引入的写法：`import greet from "libs/greet"`，取其中的成员写 `greet.Pair`。花括号写法用于工程里的文件之间。两种写法各适用于一种场景，写错就会报下面这几句。

在包外面用花括号引入包里的文件时：

```json libs/greet/nf.json
{
  "name": "greet",
  "version": "1.0"
}
```

```nf-bad
// libs/greet/greet.nf
export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 1 }
}
```

```nf-bad
import greet from "libs/greet"
import { Pair } from "./libs/greet/greet.nf"    // 报：./libs/greet/greet.nf 在包里面：引包要写 import 包名 from "…"，花括号那一套是给工程里的文件用的

let p:greet.Pair = greet.make()
console.print(p.left)
```

反过来，包里的文件也不能引入工程里的文件：同一个包换到另一个工程，还要能照常编译。

```json libs/greet/nf.json
{
  "name": "greet",
  "version": "1.0"
}
```

```nf-bad
// util.nf
export method far():int { return 1 }
```

```nf-bad
// libs/greet/greet.nf
import { far } from "../../util.nf"

export method value():int {
    return far()
}    // 报：包里的文件只能引同一个包里的文件：../../util.nf 不在这个包里面
```

```nf-bad
import greet from "libs/greet"

console.print(greet.value())
```

门面导出的类型只能由这个包自己定义。下面这个包把依赖里的 `Pair` 直接写进自己的返回值，使用它的人拿到返回值却说不出类型名：

```json libs/cart/nf.json
{
  "name": "cart",
  "version": "1.0",
  "deps": { "item": "2.0" }
}
```

```nf-bad
// libs/cart/cart.nf
import item from "libs/item"

export method one():item.Pair {    // 报：Pair 是别的包里的类型：门面只能导出这个包自己定义的类型
    return item.make()
}
```

```json libs/item/nf.json
{
  "name": "item",
  "version": "2.0"
}
```

```nf-bad
// libs/item/item.nf
export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 1 }
}
```

```nf-bad
import cart from "libs/cart"

console.print(cart.one().left)
```

`libs/item` 这个包放在 `libs/cart` 旁边，打包时会被合并进 `cart.nfpack` 里的 `pkg/item/2.0/` 目录。使用者看不到依赖的包名，因此说不出 `one()` 返回的是什么类型。

改法是门面自己包一层：把 `Pair` 的两个字段换成 `cart` 里定义的 `Row` 再返回。

下面两句把两种引入写法用反了：第一句不带花括号，引入的却是工程里的文件；第二句带花括号，引入的却是包。

```nf-bad
import util from "./util.nf"    // 报：引自己工程里的文件要写 import { 名字 } from "./util.nf"，不带花括号那一种是引包

console.print(1)
```

```nf-bad
import { twice } from "libs/greet"    // 报：libs/greet：花括号那一种引的是工程里的文件，路径要 ./ 或 ../ 开头；引包写 import 包名 from "libs/包名"

console.print(1)
```

打包时同样按这条规则判断：`nf pack` 只把整块引入的写法当作依赖收下。包里用花括号指向 `libs/` 的写法当场报回 `包引自己的依赖要写 import 包名 from "libs/包名"，这一句是花括号那一种，只引工程里的文件`。

这样的容器放进使用者的工程之后编译不过，所以这个错误报在打包这一步。

## 归属与借用

```nf-bad
method take(s:string) {
}

let items:list<string> = ["甲"]
let one:string = items[0]
take(one)
console.print(length(one))    // 报：one 已经交出去了，这个名字不能再写
```

字段遵循同一条规则，检查发生在交出值的时刻：把 `class` 的引用类型字段直接当实参交给自己写的函数，交出之后源位置就空了，下一次读取得到空句柄，所以编译器在交出这一步就报出来。

`return o.item`、`[o.item]`、`new Box(inner: o.item)` 这些写法不受这条规则限制：交出之后源位置变空，正是这些写法想要的结果。

```nf-bad
class Bag {
    n:int
}
class Hold {
    item:Bag
}
method sink(b:Bag) {
    console.print(b.n)
}
let h:Hold = new Hold(item: new Bag(n: 1))
sink(h.item)    // 报：h.item 是字段，直接交出去之后那个位置就空了：先接给一个名字，再拿那个名字交出去
```

要交出就先赋给一个名字：`let picked:Bag = h.item`，再 `sink(picked)`。存放文本的字段也可以直接交出拷贝：`h.label + ""` 产生的是一段新文本，原字段的值不受影响。

遍历交出的值是只读借用，不能转交出去：

```nf-bad
method take(s:string) {
}

let items:list<string> = ["甲"]
for (s:string in items) {
    take(s)    // 报：s 是遍历交出的只读借用，不能交给参数
}
```

标了 `ref` 的参数同样拿不到所有权，交给一个要拿走它的参数就是编译错误：

```nf-bad
method sink(s:string) {
}

method relay(ref s:string) {
    sink(s)    // 报：ref 参数 s 是只读借用，不能交给要拿走的那个参数
}

relay("甲")
```

值类型的参数不许写 `ref`：

```nf-bad
method total(ref n:int) {
    console.print(n)
}

// 报：值类型的参数不许写 ref：int 传进去本来就是整份拷贝
```

只读借用也不能拆箱：`for (o:object in box)` 里写 `o as Note` 报 `只读借用不能拆盒`。需要拆箱时先用下标取出元素，`let one:object = box[i]`，再对 `one` 用 `as`。

从借来的对象里把引用类型字段读走，同样是转交只读借用（见[值与存放](/reference/038_memory)的“从字段里读出来的值”）：

```nf-bad
class Inner {
    v:int
}
class Outer {
    inner:Inner
}
let items:list<Outer> = [new Outer(inner: new Inner(v: 1))]
for (o:Outer in items) {
    let a:Inner = o.inner    // 报：遍历交出的 o 是只读借用，不能把里面的东西交出去
    console.print(a.v)
}
```

逐层读取不需要交出：同一个循环里写 `console.print(o.inner.v)` 能通过检查，也照常输出 `1`。

方法的接收者同样是只读借用：

```nf-bad
class Bag {
    n:int

    method out():Bag {
        return this    // 报：this 是方法接收者的只读借用，不能交出去
    }
}
```

`this as object` 报的是 `只读借用不能拆盒`，把 `this` 当参数交出去报 `this 是方法接收者的只读借用，不能交给参数`。要交出一个对象，就用 `new` 新建一个再返回。

`export shared` 的名称只能就地用，交给一个要拿走它的参数（参数没写 `ref`）会报 `bag 是 export shared 的名字，只能就地用，不能当参数交出去`。

## 函数与返回值

`g` 标了返回类型，就必须每条路都返回值：

```nf-bad
method g():int {
    console.print(1)
}    // 报：g 标了返回类型 int，但有一条路没 return
```

实参位置只写一个 `null` 也不行：`null` 是值不是类型，这个位置上没有类型可言：

```nf-bad
console.print(null)    // 报：裸的 null 不能当实参：它自己不是一个类型，那一个位置上没有形状。要判空就写 x == null；要打印一个空句柄，先给它一个类型（let none:Bag = null）
```

要判空就写 `if (x == null)`；要输出一个空句柄，先给它类型（`let none:Bag = null`，再 `console.print(none)`）。

## 定义与接口

`struct` 的字面量要把字段列全：

```nf-bad
struct P {
    x:int
    y:int
}

let p:P = P { x: 1 }    // 报：P 的字段 y 没给值：字面量要把字段列全
```

`class` 用括号给初值时规则相同，报的是 `new Counter 给了初值就得把字段给全，m 没给`。

`struct` 的字段只允许值类型、`string` 与另一个 `struct`：

```nf-bad
class Note {
    body:string
}

struct Holder {
    n:Note
}    // 报：struct 的字段只能放值类型、string 或另一个 struct，这里是 Note
```

句柄型的值放进字段后，拷贝对象时只复制这个句柄，两个名称就共用同一个对象。

接口声明的每个方法都必须实现，参数名还要完全相同：

```nf-bad
interface Shape {
    area():float
}

class Circle base Shape {
    r:float

    method name():string { return "圆" }
}    // 报：Circle 没写出接口 Shape 声明的 area
```

接口里写 `add(v:int):int`，实现写成 `add(w:int):int` 报 `Bag.add 的第 1 个参数该叫 v`。

接口自己的方法声明不写 `method`：

```nf-bad
interface Adder {
    method add(v:int):int    // 报：接口里的方法声明不写 method，只写签名
}
```

`struct` 不能继承带方法声明的接口，报的是 `P 是 struct，写不了方法，所以不能继承带方法声明的接口 Shape`。

```nf-bad
struct A {
    b:B
}

struct B {
    a:A
}

console.print(1)    // 报：struct A 的字段绕成了环
```

环报在完成环形引用的那个字段上，一个环只报一次。报出的类型名是环上最短的名字，所以 `A` 与 `B` 互相引用时报的是 `struct A 的字段绕成了环`。

`enum` 成员不许同值：`enum D { a = 5, b = 5 }` 报 `枚举成员 b 与 a 同值 5`。

## 语句与表达式

```nf-bad
enum D { a, b, c }

method f(d:D) {
    switch (d) {
        D.a { console.print(1) }
        D.b { console.print(2) }
    }
}    // 报：switch 漏了 D 的成员，又没写 default
```

```nf-bad
throw (20000, "x")    // 报：throw 的码从 0 写到 9999，20000 落在语言自己那一段（10000 往上）
```

```nf-bad
let x:int = throw (100, "a")    // 报：throw 是语句，只能自成一条，不能出现在取值的位置
```

`exit` 的码遵守同一条规则：`exit (10000, "不对")` 报 `exit 的码从 0 写到 9999，10000 落在语言自己那一段（10000 往上）`。这个错误同样发生在编译期，程序不会开始运行。`exit (0, "跑完了")` 是允许的写法：打印一句话，再正常结束。

```nf-bad
console.print(1 << 70)    // 报：移位量只能在 0～63 之间，这里写了 70
```

```nf-bad
let s:int = 8
console.print(1 << s)    // 报：移位量只能写 byte 或一个 0～255 的整数，这里是 int
```

```nf-bad
let d:dict<float, int> = {}    // 报：float 不能当 dict 的键：NaN 不等于自己，这个键永远查不中
```

```nf-bad
if (null == 1) {
    console.print(1)
}    // 报：null 只能和引用类型比，这里是 int
```

```nf-bad
enum D { a, b }
method pick():D {
    return 1 as D
}    // 报：int 不能 as 成 D
```

```nf-bad
let s:string = "abc"
s[0] = 'X'    // 报：string 不可变，不能改其中一格
```

```nf-bad
let xs:list<list<int>> = [[1, 2]]
xs[0][0] = 7    // 报：中间那个位置读走就空了，改不到它的下一层：先给它绑一个名字
```

接口名称不能出现在需要类型的地方（变量、参数、返回值、容器元素）：报的是 `Shape 是接口名，不能当类型写`。

装箱必须写出来：`box.add(n)` 里 `n` 是 `Note`，报 `这里要的是 object，给的是 Note；类型之间不自动转换`，要写 `box.add(n as object)`。

少一个右括号属于词法错误：`let a:list<string> = ["x"` 报 `想要 "]"，实际看到 行尾`。改法是补上对应的闭合记号。

## 运行期抛出的错误

抛出的形式永远是两个值：一个整数码，一条消息。语言内置的错误码也是这个形状，可以 `catch`。

```nf
method load(name:string):string {
    if (name == "") {
        throw (100, "路径不能是空的")
    }
    return file.read(name)
}

try {
    let content:string = load("")
    console.print(length(content))
} catch (code, msg) {
    console.print("接住了:", code, msg)
}
```

```text
接住了: 100 路径不能是空的
```

### 内置错误码

`10000` 往上属于语言，程序自己使用的码写在 `0` 到 `9999`。两段分开之后，读代码的人不必再猜某个数是程序自定义的还是语言规定的。每组占一个百位段（`10010`、`20`…），该段末尾为整百的错误号是该组的默认码。

| 错误号 | 触发情况 | 能否 `catch` |
| --- | --- | --- |
| 10010 | 转换类的默认码 | 能 |
| 10011 | `as` 拆箱，盒中装的不是所请求的类型 | 能 |
| 10012 | 文本解析不成数字；`math.power` 给出负指数 | 能 |
| 10013 | 换算出来的值超出目标类型的取值范围（`as byte`、`byte` 的 `++` 与 `--`；`text.to_int`、`text.to_int_in_base` 超出 `int`） | 能 |
| 10014 | 文本读不成 base64：长度不是 4 的整倍数、`=` 出现在中间、出现字母表以外的字符。`text.from_base64` 与 `bytes.from_base64` 两个函数的三项检查相同，报错也是同一句 | 能 |
| 10020 | 数值类的默认码；`math.random` 的空范围 | 能 |
| 10021 | 整数运算的结果超出 `int` 范围；算出来的移位量超出左式能移的范围 | 能 |
| 10022 | 整数除以 `0` | 能 |
| 10030 | 容器、文本与字节块的默认码；`bytes.new` 传入了负的字节数（`bytes.new 要的字节数不能是负数`） | 能 |
| 10031 | 下标越界（容器、文本与字节块同样处理） | 能 |
| 10032 | `dict` 查询不存在的键 | 能 |
| 10033 | 遍历容器的同时对它增删 | 能 |
| 10040 | 文件类的默认码 | 能 |
| 10041 | 文件读不到、删不掉；`file.remove_tree` 内层与外层删不掉时也是同一个错误号，消息分文件与目录两句 | 能 |
| 10042 | 文件写不进去 | 能 |
| 10043 | 目录打不开，或拿目录去问文件的属性 | 能 |
| 10044 | 不是合法的 UTF-8：文件内容、传入 `text.from_bytes` 的字节块、`text.from_base64` 解码得到的字节，三种来源共用同一个错误号。`bytes.from_base64` 没有这一项检查：它接受的就是字节本身 | 能 |
| 10045 | 拿不到文件的属性（路径不存在） | 能 |
| 10046 | 建不出这个目录（上一级目录不存在，或者创建被拒绝） | 能 |
| 10047 | 重命名或移动失败（旧名字不存在、新名字被占用、跨卷无法移动） | 能 |
| 10050 | 路径类的默认码 | 能 |
| 10051 | 路径本身不合法 | 能 |
| 10060 | 环境变量类的默认码 | 能 |
| 10061 | 要读取的环境变量不存在 | 能 |
| 10070 | 正则类的默认码 | 能 |
| 10071 | 正则式子写错 | 能 |
| 10080 | 时间类的默认码 | 能 |
| 10090 | 其余情况的默认码 | 能 |
| 10091 | 向操作系统申请内存失败 | 能 |
| 10092 | 用 `null` 句柄访问成员；已关闭的套接字、已回收的进程句柄再拿来访问成员，也是同一个错误号 | 能 |
| 10093 | 调用套得太深，栈用完了 | 不能：打印一行后程序结束 |
| 10100 | 网络类的默认码：折算表里没有对应的系统号码（`internal/runtime/net.go` 的 `netCodeOf`） | 能 |
| 10101 | 连不上这个地址：连接被拒绝、没有路由、主机或网络不可达、TLS 与证书校验失败 | 能 |
| 10102 | 等待超时：`net.connect` 的毫秒数用于连接与收发，`http.get` 的毫秒数覆盖四个阶段 | 能 |
| 10103 | 无法解析这个名字：消息里带上地址部分（`net`）或整个网址（`http`） | 能 |
| 10104 | 地址或端口不合法：端口越界、端口已被占用、`bind` 失败、网址里解析不出主机 | 能 |
| 10105 | 这条连接已不可用：对方关闭了连接，或者发送、接收时连接已断 | 能 |
| 10110 | 进程类的默认码：等不到子进程结束、读取不到它的退出码 | 能 |
| 10111 | 程序启动失败：路径不存在、目标不是可执行的文件、没有访问权限，也包括命令行是空的或者超过内部缓冲区的长度 | 能 |
| 10112 | 同时运行的子进程太多，表里一百二十八个位置全部占满（还有结束后没有回收句柄的子进程） | 能 |

10100 段六行的消息一律带上系统返回的原始号码，形如 `出错了（10101）：连不上这个地址（系统的号码 10061）`。

winsock 与 winhttp 各有上千个错误号，逐个分配编号会让这张表过长；一个系统号码都不带，读者又只能靠猜测定位原因。折算表与段位表的内容相同，改一处要同步改另一处。

10110 与 10111 这两行的消息同样带系统号码（`出错了（10111）：起不来那个程序（系统的号码 2）`）。10112 的消息由语言自行统计得出，系统没有给号码。

表里只有 10093 无法被捕获。栈耗尽之后，释放已经建好的值和跳回 `catch` 都还要使用栈，而耗尽的正是栈本身。

所以这个错误不做清理，也不交给 `catch`：屏幕上打印 `出错了（10093）：调用套得太深，栈用完了`，返回操作系统的数是 `1`。

栈的大小是 8 MiB。编译产物与 `nf 文件.nf` 直接运行申请同样大小的栈，同一段代码无论走哪条路径，都在同样的深度耗尽。

一个递归程序实测：递归 5 千层能正常执行完并打印结果，递归 20 万层会在执行过程中耗尽栈。

`throw` 与 `try` / `catch` 的规则在[错误处理](/learn/031_errors)，各组的错误码归属在[标准库](/stdlib/044_index)各页里逐个标注。

错误号 10021 只覆盖算出来的加减乘除。移位量越界只在写成常量时报编译错误；算出来的移位量按机器码的规则处理，不抛出 10021，这条规则写在[数字与小数](/learn/024_numbers)。

### 未捕获时

错误始终无人捕获时，程序终止，屏幕上只有一行，返回操作系统的数是 `1`：

```text
出错了（10031）：下标越界
```

括号内是错误号，冒号后是消息文本。消息里没有文件名、行号与调用链：这些信息要求产物额外携带一张行号对照表，与直接生成机器码这一目标相冲突。需要定位时，自行 `catch` 后把位置写进消息。

这条消息走标准输出还是标准错误、脚本里如何把它与编译器的诊断分开接收，写在[命令行工具](/tools/059_cli)。

## 编译期错误无法捕获

上面“编译期错误速查”里的每一条都发生在编译阶段：程序不会开始运行，也就不存在可以捕获它的位置。能被 `catch` 的只有运行期抛出的错误。

# 值的所有权

写一个函数，只为数一下 `list` 里有几项。调用这个函数，下一行继续使用这份容器时，编译器拒绝：`items 已经交出去了，这个名字不能再写`。

只是一次读取，为什么值就不属于本地了？这一课说明这条规则；后面的字段、装箱、传参都建立在这条规则之上。

## 一个值只有一个持有者

如果同一个值有两个持有者，会出现两类错误：一个持有者先释放，另一个随后读写已经失效的对象（悬垂引用）；或者同一块内存被释放两次（双重释放）。两者都发生在运行期，排查代价高。

这门语言不带垃圾回收器，所以归属必须在编译期确定清楚。规则只有一条：**任何一个值，同一时刻只有一个持有者**（下文称“持有者”）。持有者负责在作用域结束时释放该值，运行期不需要跟踪还有多少引用。

其余内容都是这条规则的推论。

## 核心概念：交出、借用、装箱

**交出**：把值的归属交给另一个名称，原名称立即失效。转移之后仍然只有一个持有者，不存在两个名称各持一份的情况。

**只读借用**：读取或者使用该值，不接管所有权。操作结束后持有者不变。

**装箱**：把一个值放入 `object`，以便和不同类型的值放在一起；需要时再取回。

## 值类型与引用类型

值类型包括 `boolean`、`int`、`float`、`byte`、`unicode`、`struct`、`enum`。赋值时整份拷贝，两份互不影响。

引用类型包括 `class`、`string`、`object` 和三种容器。赋值即交出，原名称失效。

```nf
let txt:string = "喂"
let other:string = txt

console.print(other)          // 喂
```

执行到第二行，`txt` 已不再是持有者。

此时再写 `console.print(txt)`，编译器的原文是 `txt 已经交出去了，这个名字不能再写`。

```nf-bad
let txt:string = "喂"
let other:string = txt

console.print(txt)             // 报：txt 已经交出去了，这个名字不能再写
```

一个值只有一个持有者，但**能书写它的名称**不一定只有一个：后面讲到的四种只读借用就是这种情况。读取字段不算只读借用：`class` 的字段装着引用类型时，读出来就是交出，之后读那个字段得到的是 `null`（见下文关于字段交出的说明）。

## 函数参数也是交出

自定义函数接收引用类型参数时，交出去的是值本身：

```nf-bad
method shout(t:string) {
    console.print(t)
}                 // t 到这里失效；文本没被它留住，还它的是调用方那一层

let word:string = "喂"
shout(word)
console.print(length(word))    // 报：word 已经交出去了，这个名字不能再写
```

`shout` 接收的是文本本身，`word` 从这次调用起不再是持有者。

标准库的十四个内建组（语言自带的成组函数）情况不同。`console.print(word)`、`length(word)`、`word.split(" ")` 执行完毕后，`word` 仍然可用，因为它们只是只读借用。

界线只有一条：**传入的值按交出处理（自定义函数与 `class` 方法都是；参数前写了 `ref` 的那一处例外），被操作的对象按只读借用处理（标准库函数、方法的接收者）**。

如果所有函数都接管参数，容器将无法使用：每调用一次就转移一次所有权，向 `list` 添加一个元素就要把整份容器交出去。

## 只想读一读，参数前写 `ref`

上面那一处 `shout` 只是把文本打印出来，没有把它留住。这样的参数前面写 `ref`，调用方的名称就不再失效：

```nf
method shout(ref t:string) {
    console.print(t)         // 喂
}

let word:string = "喂"
shout(word)
console.print(length(word))    // 1
```

`ref` 标在参数前面，那一个位置只是借来使用：可以读它、调用它的方法、就地修改它（`words.add("雨伞")` 改的就是调用方那一份）。`return` 它、把它写进字段、装进容器、装箱、交进一个没写 `ref` 的参数，这五处都是编译错误，报错原文里带着出问题的那个参数的名字。

值类型的参数不许写 `ref`：那些传进去本来就是整份拷贝。`Socket`、`Listener`、`Process` 这三类句柄也不许写：在借来的句柄上关一次，调用方的名称就成了空句柄。

## 存进容器也是交出

```nf-bad
class Bag {
    n:int
}

let a:Bag = new Bag(n: 1)
let cs:list<Bag> = [a]
console.print(a)      // 报：a 已经交出去了，这个名字不能再写
```

字面量中的 `[a]` 与 `cs.add(a)` 遵循同一条规则：容器里的这个位置成为该值的新持有者。因此 `[a, a]` 也是编译错误：一个值不能有两个持有者，一个位置只保存一份值。

存入值类型本来就是整份拷贝，`[p, p]` 可以照常书写，`p` 仍然可用。

`new` 括号中的字段初值同样按交出处理：

```nf-bad
class Bag {
    n:int
}
class Box {
    inner:Bag
}

let b:Bag = new Bag(n: 1)
let x:Box = new Box(inner: b)
console.print(b)      // 报：b 已经交出去了，这个名字不能再写
```

`new Box(inner: b)` 之后 `b` 失效，`x.inner` 成为这个值的新持有者。

字段也遵循同一条规则：`class` 的字段装着引用类型时，读出来就是交出，之后读这个字段得到的是 `null`。所以不存在“让同一个对象被两处持有”的写法：`let a:Inner = o.i` 之后 `o.i` 已经为空，再读一次得到的是 `null`。

只是继续读取成员不算交出：`console.print(o.i.v)`、`o.list.add(x)` 这样的读取不产生新持有者，字段仍然持有它的值。完整的规则见[值与存放](/reference/038_memory)。

## 条件分支的合并规则

编译器不判断程序实际走哪条分支，它只看是否存在一条路径把值交出去：

```nf-bad
method shout(t:string) {
    console.print(t)
}

method pick(one:string, other:string, take:int) {
    if (take == 1) {
        shout(one)
    } else {
        shout(other)
    }
    console.print(one)      // 报：one 已经交出去了，这个名字不能再写
}
```

离开 `if` 时，两条分支的状态要合并：只要任意一条分支里 `one` 已不再是持有者，合并之后就不能再使用它。

即使实际执行走的是 `else` 分支，合并之后的结论仍然相同。

最初会觉得这条规则保守：实际执行走的是 `else`，却判定 `one` 已经交出。但编译器一旦放行，就等于承诺“此处 `one` 一定有值”，它无法为没有走过的路径作出保证。

需要在分支里继续使用一个值时，就在进入分支之前先把这个值取出来：

```nf
method shout(t:string) {
    console.print(t)
}

method pick(items:list<string>, take:int) {
    let one:string = items[take]        // 从容器交出出来，持有者是 one
    shout(one)                          // one 交出去，此后不能再写它
    // 容器一直在本地手上，只是少了一个位置上的值
    console.print(length(items))
}
```

## 循环体的两轮检查

```nf-bad
method shout(t:string) {
    console.print(t)
}

method shout_all(items:list<string>) {
    for (s:string in items) {
        shout(s)         // 报：s 是遍历交出的只读借用，不能交给参数
    }
    console.print(s)     // 报：没有 s 这个名字
}
```

上面这段代码同时涉及两条规则。

遍历给出的 `s` 是只读借用：可以读取它，也可以调用它的方法，但不能转交出去，出了这一轮就失效。

循环体要检查两遍。只检查一遍时，“第一轮把 `s` 转交出去、第二轮开头这个名称已经为空”要到第二轮才能发现；检查两遍，就能在编译期报出来。

需要逐个取出元素时，使用下标：

```nf
method shout(t:string) {
    console.print(t)
}

method shout_all(items:list<string>) {
    for (i:int in 0 until length(items)) {
        let one:string = items[i]     // 交出出来，一个位置一份
        shout(one)
    }
}
```

这个循环结束后，`items` 中每个位置都是 `null`，长度不变，容器本身始终属于本地。

## 接收者与 `this`

调用 `c.bump()` 时，左侧的 `c` 是被操作的对象：方法在整个调用期间借用它，结束时归还，调用之后 `c` 照常可用。

方法体内的 `this` 是同一个句柄，规则相同：可以读取字段，可以就地修改这个对象，可以连续调用方法，但不能转交出去。

```nf-bad
class Bag {
    n:int

    method bump():int {
        n = n + 1          // 就地修改自身：允许
        return n
    }

    method copyOut():Bag {
        return this        // 报：this 是方法接收者的只读借用，不能交出去
    }
}
```

`return this`、把 `this` 作为参数传出、`this as object`，这三条路径都会在编译期报错：一旦放行，`c` 与接收它的名称就同时成为持有者。需要返回一个对象时，另外 `new` 一个。

## 释放时机

一个作用域结束时，该作用域内声明的值按声明的**反序**释放，后声明的先释放。函数体、块、文件顶层都遵循同一套规则。

顶层只有一处不同：顶层声明的值要到**整个程序运行结束**才释放。

被 `import` 的文件先执行完，但它顶层声明的容器之后还要供本文件的函数使用，因为本文件的函数可能在其他文件的顶层中被调用。因此各文件的顶层依次执行完毕，再从入口文件的最后一行声明向前释放。

```nf
method build():list<string> {
    let parts:list<string> = ["甲", "乙"]
    return parts            // parts 随函数结束失效，容器交给接收它的名称
}

let all:list<string> = build()
console.print(length(all))  // 2
```

抛出异常与正常执行完毕采用同一套清理流程：`throw` 让当前函数立即结束，该函数作用域内的值仍然按反序释放。因此语言不需要 `finally`。

`exit (code, message)` 也不会跳过释放，它只终止尚未执行的代码。

唯一例外是 `export shared` 标注的名称，它存活到进程结束。

这套规则带来两点好处，也带来一点限制。

好处是：任何地方都不需要书写“用完记得关闭”，也就不存在“忘记关闭”这类错误；每个值的释放点就写在它所属的作用域里，读代码时不必推测。

限制是：同一个值不能同时交给两个名称使用。需要把一份容器传给三个函数时，三种写法各自解决不同的问题：

| 需求 | 写法 | 调用结束后 |
| --- | --- | --- |
| 只读取，不修改 | 参数前写 `ref`（`method total(ref xs:list<string>):int`），或者把读取写在调用处：标准库那十四个内建组的被操作对象本来就是只读借用 | 传进去再传回时，持有者换成另一个名称，容器本身不会丢失 |
| 每个函数都要一份独立副本 | 拷贝之后再传出 | 各自修改，互不影响 |
| 多个文件确实共用一份可变值 | `export shared`，写在顶层 | 任何一处都不独占它，因此它不能交给会拿走值的参数；标了 `ref` 的参数位置可以接收它 |

第三种写法留给确实需要跨文件共用的场合。标注 `shared` 等于在代码中声明“这份数据有多处在修改，由我负责协调”，因此不需要共享时就不要标注。

## 装箱与拆箱

```nf
class Note {
    body:string
}

let n:Note = new Note(body: "便条")
let box:list<object> = []

box.add(n as object)        // n 交出去了
console.print(length(box))  // 1
```

装箱同样是一次交出：`n` 交出后不再是持有者。

值类型装箱（`42 as object`）本身就是一次拷贝，不涉及交出。

拆箱 `as` 会接管其中的值：拆完之后这个装箱值已经为空，原来的位置再读取得到 `null`。完整说明见[自定义类型](/learn/029_types)。

## 跨文件共享：`export shared`

顶层的 `let` 属于本文件，不能导出。

确实需要在多个文件之间共用一份可变容器时，写 `export shared`。这个标注相当于在代码中声明：这份数据有多处使用，由我负责。

共用只在这几个文件之间成立。把这些文件打包给别人使用时，包外面读不到 `shared` 的名字，也写不进去；要把这份状态提供出去，就在包里写一个函数，由函数交出内容。

```nf
// registry.nf
export shared log:list<string> = []

export method note(entry:string) {
    log.add(entry)
}
```

`export` 与 `shared` 必须成对书写，且只能出现在顶层。

带有这个标注的名称存活到进程结束，不参与作用域释放。

**不能交出**：`eat(log)`、`let x = log`、`return log` 都是编译错误，只能就地读写。

其中每个元素仍按普通规则处理：`let s:string = log[0]` 合法，该位置此后为 `null`。

日常代码很少需要这种共享标注。确实需要使用时，这个关键字会提醒后来读代码的人：这里存在共享状态。

## 练习

把“条件分支的合并规则”一节里的例子改成：在 `if` 之前先写 `let one:string = items[0]`，两条分支都调用 `shout(one)`。

先判断这样改能否通过检查，再动手修改。判断依据只有一条：两条路径都会把同一个名称交出去。

再比较三种写法：`shout(word)`、`console.print(word)`，以及 `shout` 的参数标上 `ref` 之后再写 `shout(word)`。三处只差几个字符，前两种规则相反，第三种与第二种相同：要看的是那一个位置会不会成为持有者。

## 本节要点

赋值、传参、存入容器和装箱都是交出，交出之后原名称失效；参数前写 `ref` 的那一处不算交出。标准库函数、方法接收者、`for` 给出的循环变量和 `ref` 参数是只读借用，借用结束后持有者不变。释放点写在作用域里，抛出异常与正常执行完毕采用同样的清理流程。

下一节讲条件分支与循环。

# 函数与调用

`method` 声明一个函数：写在文件顶层就是函数，写在 `class` 体内就是方法，两者用的是同一个关键字。

## 声明

```nf
method line(x:int, y:int = 0):int {
    return x + y
}

method show(m:string) {
    console.print(m)
}

console.print(line(3), line(3, 4))
```

```text
3 7
```

`console.print` 把一次调用的几个实参用空格连接后输出在同一行，末尾输出一个换行；需要分成两行就调用两次。

| 事项 | 规则 |
| --- | --- |
| 参数 | 每个参数都要标注类型，不能省略 |
| 返回类型 | 不写返回类型表示没有返回值，语言里没有 `void` 一类的记号。这样的调用也就不能出现在需要一个值的位置上：`console.print(show())`、`console.print(l.sort())` 都是编译错误。单独写成一句 `l.sort()` 是合法用法，返回值在这条语句结束时释放 |
| 默认值 | 写在类型后面：`y:int = 0`。调用时省略的参数取默认值，必须从右向左连续省略，不能跳过中间一个参数。上面的示例里 `line(3)` 输出 `3`、`line(3, 4)` 输出 `7`。默认值只允许写**字面量、顶层 `const` 名称，以及这两者的算术组合**（`wide:int = STEP * 4`）；函数调用、`console.read()`、非 `const` 的名称都在**参数声明处**被拒绝，报错原文是 `默认值只准写字面量、const 名字和它们的算术` |
| 重载 | 不支持。同一个作用域里一个名称只能有一个函数，`class` 中的方法遵循同一条规则 |
| 实参中的 `null` | 是否接受，取决于接收实参的位置有没有标注类型。自定义函数和标准库绝大多数参数都写着类型，因此 `f(null)` 合法。没写类型的参数只有几处：变长的 `console.print`，以及 `length`、`number.to_string`、`math.absolute_value`、`math.minimum`、`math.maximum`。这些位置上单独的 `null` 无法传入，因为它本身不是一个类型，这样的位置也就无法核对形状。需要在这种位置传入一个空引用时，先给它标注类型（`let none:Bag = null`，再 `console.print(none)`） |
| 名称 | 方法名在一个 `class` 内唯一，字段与方法共用同一个命名空间 |

```nf-bad
method show(m:string) {
    console.print(m)
}

console.print(show("甲"))   // 报：show 不返回值，那一个位置上没有值可用
```

## 传参

| 参数的类型 | 传参方式 |
| --- | --- |
| 值类型 | 整份拷贝。函数里怎么修改都不影响调用方，修改也写不回调用方的变量 |
| 引用类型（`class`、容器、`Json`、`bytes`、`object`） | 只读借用，参数前面不写任何记号。调用方的名称不失效；被调方可以读取、调用它的方法、就地修改、顺着装箱拆箱，但不能把它 `return`、写进字段、装进容器的一个位置、交进一个会接手的位置，也不能把装出来的那一份收进一个名字。 |
| `string` | 共用。那一个位置与调用方手里的是同一块，两边都不作废、都不释放。 |
| 四种系统句柄（`Socket`、`Listener`、`Process`、`Thread`）与 `thread.start` 的第二个入参 | 交出。名称交给参数之后调用方那个名称当场失效；句柄由接住它的那一侧在作用域尽头关闭，交进另一条线程的那一块由那条线程跑到尽头归还 |

```nf
struct Point {
    x:int
    y:int
}

method grow(p:Point) {
    p.x = p.x + 1        // 改的是拷贝
}

let orig:Point = Point { x: 1, y: 2 }
grow(orig)
console.print(orig.x)    // 1
```

只想读一读、不把值留下，是默认写法：参数本来就是只读借用，什么记号都不用写。

```nf
method word_count(words:list<string>):int {
    let total:int = 0
    for (word:string in words) {
        total = total + length(word)
    }
    return total
}

let items:list<string> = ["书包", "笔记本"]
console.print(word_count(items))       // 5
console.print(length(items))         // 2
```


标准库的十五个内建组按同一条处理：它们只读借用第一个参数，不接管。方法左侧的接收者同样按只读借用算。

只读借用的位置一共四处：标准库的入参、方法接收者、遍历给出的名称、自己写的函数与方法的参数。共同点是一条：借的一方拿不到所有权，所以毁不掉原来那个位置。这条界线在[值与存放](/reference/038_memory)里说明。

## 返回

- 标了返回类型的函数，函数体每条路径都必须 `return`，少一条就是编译错误，编译器不会补默认值。
- `return` 返回引用类型的值同样是交出，函数里新建的对象就这样交给接收返回值的名称。
- 没有返回值时，可以写一条不带值的 `return` 提前返回，也可以直接执行到右花括号。
- 没有返回类型的调用不是一个值，所以不能放在需要值的位置上。`console.print(show())`、`console.print(length(xs.slice(0, 1).sort()))` 这类写法在检查阶段就会被拒绝，报的是被调用的函数名加一句“不返回值”；标注了类型的参数位置报的是 `这里要的是 int，给的是 无返回值；类型之间不自动转换`。
- 一个函数只返回一个值。要一次返回几个数就 `return` 一个 `struct`，调用方按字段名取值。

```nf-bad
method pick(x:int):int {
    if (x > 0) {
        return x
    }
}                     // 报：pick 标了返回类型 int，但有一条路没 return
```

交出在控制流里有两条推论：分支里任何一条路径把值交出，离开分支后这个值就视为已经不属于本地；循环体检查两遍。

## 名字的先后

一个文件里的顶层定义不分先后。

编译器先收集全部顶层名称，再检查调用。函数可以调用后面才声明的函数，也可以使用后面才声明的顶层变量。

值的情况不同：值按书写顺序计算。顶层的 `let` 与 `const` 要在程序执行到声明所在的那一行时才求值、才交出，因为文件顶层就是一层作用域，和函数体没有两样。

## 函数当值

函数可以当值交出去：`fn(参数类型):返回类型` 写的就是这种值的类型，那一个位置里存的是这个函数的入口。调用它写成 `cmp(a, b)`，与按名字调用走同一套。

```nf
method by_length(a:string, b:string):int {
    return length(a) - length(b)
}

method gap(first:string, second:string, by:fn(string, string):int):int {
    return by(first, second)
}

let cmp:fn(string, string):int = by_length
console.print(cmp("甲乙丙", "丁"), gap("甲", "乙丙丁", by_length))
```

```text
2 -2
```

范围只到这一档，几条限制各有理由：

| 限制 | 原因 |
| --- | --- |
| 只有自己写的顶层函数能当值交出去，方法不行 | 方法带着接收者，一份函数类型说不清接收者归谁 |
| 没有匿名函数，函数值也不带走外面的名称 | 一旦允许捕获，"捕获的变量归谁所有、什么时候释放"要把单一持有者那套规则重新讨论一遍 |
| 签名要对得上：参数个数、每个参数的类型、返回类型三项都要一样 | 这是类型标注本身应有的要求，与别处的赋值同一条 |
| 参数不限制，返回值只能是 `boolean`、`int`、`float`、`byte`、`unicode`、`enum` 这六种不参与释放的值类型，或者不返回 | 参数一律是只读借用，接住的是哪一支函数与归属无关；返回值不同——一份函数类型说不出接住的是哪一支函数，"被调方有没有留下这一块"就证不了。两个持有者释放同一块内存，比多留一块没人管严重得多 |
| 函数值进不了三种容器，装不进 `object` 盒子，`console.print` 打不出它，也当不了 `dict` 的键 | 这几处都要靠运行期认类型，而函数值没有可认的类型标记 |
| 同一个函数的两份值相等，不同函数不相等 | 比的是入口地址；除此之外没有别的运算 |

按一个值分派一组动作，除了交函数值，也可以照旧写一个 `switch`：

```nf
enum Kind { add, sub, mul }

method apply(k:Kind, a:int, b:int):int {
    switch (k) {
        Kind.add { return a + b }
        Kind.sub { return a - b }
        Kind.mul { return a * b }
    }
}

console.print(apply(Kind.mul, 3, 4))   // 12
```

## 未提供的写法

下面这些写法都不提供：构造函数（`init` 一类）、`throws` 标注、函数重载、可变参数的自定义函数（只有 `console.print` 是变长的）。

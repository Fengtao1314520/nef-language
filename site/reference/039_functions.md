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
| 引用类型，前面写 `ref` | 只读借用。调用方的名称不失效；被调方可以读取、调用它的方法、就地修改，但不能把它 `return`、写进字段、装进容器，也不能装箱。值类型不写 `ref`：那些传进去本来就是拷贝。 |
| 引用类型 | 交出。对象交给参数之后，调用方原来的名称当场失效；被调方没有把它保留下来时，就在这个被调函数的作用域结束时释放 |

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

只想读一读、不把值留下，就在参数前面写 `ref`：

```nf
method word_count(ref words:list<string>):int {
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


标准库的十四个内建组不适用这条规则：它们只读借用第一个参数，不接管。方法左侧的接收者同样按只读借用算。

能借来看的位置一共四处：标准库的入参、方法接收者、遍历给出的名称、标了 `ref` 的参数。共同点是一条：借的一方拿不到所有权，所以毁不掉原来那个位置。这条界线在[值与存放](/reference/038_memory)里说明。

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

## 函数不是值

没有函数类型，没有匿名函数，函数不能存进字段、不能当参数传递、不能从函数返回。只能按名字调用（`helper(3)`）或者用点号调用（`c.add(1.0)`）。

函数不能当值传递，是因为归属无法确定：函数一旦能在调用之间传来传去，它捕获的外部变量归谁、什么时候释放都没有定论，整套单一持有者的规则都要重新讨论。

要按一个值分派一组动作，就写一个 `switch`：

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

# 自定义类型

一笔支出有日期、金额和备注，这三个值总是一起传递、一起修改。如果拆成三个变量，每次调用都要传三个参数，改动一处逻辑就要改三行，因此可以把它们组合成一个类型。

Nef 提供两种组合方式：`struct` 定义值类型，`class` 定义引用类型。这一课先掌握两种定义方式，第 5 课说明过的所有权差异会在本课末尾再次出现。

## `struct` 与 `class` 的区别

`struct` 是值类型：宽度在编译期确定，赋值时整份拷贝，判等逐字段比较。

在存放方式上，它和引用类型使用同一套布局：名称中保存的是 8 字节的句柄，字段数据在堆上。整份拷贝复制的是堆上的整块数据，所以两份互不影响。

`class` 是引用类型：值存放在堆上，名称持有的是指向它的句柄，赋值与传参都会交出，判等比较的是是否为同一个对象。

`interface` 与 `enum` 也写在文件顶层，前者约束方法集合，后者给一组整数命名。四种定义都不能嵌套在 `struct` 或 `class` 体内。

顶层还可以用 `type Names = list<string>` 给一个已有的类型另起一个名字。它不是一种新类型，用它标注类型与直接写 `list<string>` 完全等价，细节见[定义](/reference/040_definitions)。

## struct

`struct` 里只声明字段，不能声明方法：

```nf
struct Point {
    x:int
    y:int
}

let a:Point = Point { x: 1, y: 2 }
let b:Point = a

b.x = 9
console.print(a.x, b.x)       // 1 9
```

`b` 是 `a` 的一份拷贝，修改 `b.x` 不影响 `a`。判等 `==` 逐字段比较，全部字段相等才算相等。

拷贝和逐字段判等都以“宽度在编译期确定”为前提。

### 字段的限制

`struct` 的字段只能放值类型、`string` 或另一个 `struct`。

`class` 名称、`object`、`list`、`dict`、`hashset` 出现在字段位置上会被编译期拒绝，提示是 `struct 的字段只能放值类型、string 或另一个 struct，这里是 Note`。

因为这些类型在内存中的值就是一个句柄。`struct` 的整份拷贝复制的正是这 8 个字节，拷贝完成后两个名称指向同一个对象，修改其中一个会影响另一个，得到的不是两个独立的值。需要组合若干对象时，改用 `class`。

`string` 是唯一的例外：它不可变，多一个句柄不会改变任何可见的行为。

构造值使用字面量，**字段必须列全**：

```nf-bad
struct Point {
    x:int
    y:int
}

let p:Point = Point { x: 1 }    // 报：Point 的字段 y 没给值：字面量要把字段列全
```

字段顺序可以打乱，名称不匹配或者缺少任意一个都是编译错误。需要全零就自行给出 `Point { x: 0, y: 0 }`。

允许没有任何字段，没有字段的 `struct` 占 0 字节。

## class

字段与方法都可以省略，只声明字段、不声明方法同样合法：

```nf
class Counter {
    n:int

    method bump():int {
        n = n + 1
        return n
    }
}

let c:Counter = new Counter()
console.print(c.bump(), c.bump())   // 1 2
```

`new Counter()` 在堆上创建对象，所有字段取对应类型的默认值（`0`、`0.0`、`false`、`null`），不存在内容未定义的字段。

也可以在括号里按名称给出初值：`new Counter(n: 10)`。一旦使用这种形式就必须把字段全部给出，漏掉字段是编译错误，不会自动补上默认值。

没有构造函数，不存在 `init` 一类的形式。

方法体内访问自身的字段时直接使用字段名，`this` 可以省略，两种形式等价。

方法名在一个 `class` 内必须唯一，不按参数类型或个数重载。字段名与方法名共用同一个命名空间：`x:int` 与 `method x()` 并存是编译错误，编译器不会根据后面是否带括号来区分两者。

`class` 是引用类型，`==` 比较的是对象身份，不看字段内容。

## 赋值与传参的所有权语义

这是两者真正分开的地方。`class` 的赋值和传参都会交出，原名称随即失效：

```nf-bad
class Box {
    n:int
}

let p:Box = new Box(n: 1)
let q:Box = p
console.print(p.n)    // 报：p 已经交出去了，这个名字不能再写
```

`struct` 相反，`let b:Point = a` 得到的是第二份拷贝，两个名称都能继续使用。

选择哪一种，只问一个问题：这个值传出去之后，原名称还要不要继续使用。需要继续使用就用 `struct`；不需要继续使用（或者本来就应该只由一处持有）就用 `class`。

> 注意：字段没有公私之分，定义之后就能读取和修改，也没有 `public`、`private` 这类修饰符。跨文件的可见性由 `export` 决定，见[多个文件](/reference/042_modules)。

字段也不能引用自己所在的类型：`struct Node { next:Node }` 是编译错误，因为这样的定义无法确定宽度。需要把多个值连接起来时，使用容器。

## object：混合存放

`struct` 与 `class` 的名称不能作为同一种容器元素类型放在一起，`interface` 的名称更不能当作类型使用。需要在同一个位置存放不同类型的值时，就把它们装箱成 `object`：

```nf
struct Size {
    w:int
    h:int
}

class Note {
    body:string
}

let box:list<object> = []

let s:Size = Size { w: 3, h: 4 }
let n:Note = new Note(body: "便条")

box.add(s as object)
box.add(n as object)

console.print(length(box))    // 2
```

`x as object` 是**装箱**。值类型装箱需要一次堆分配，所以这一步必须由代码自己给出，没有自动装箱。

装箱同时是一次所有权转移，按引用类型的规则处理：`box.add(n as object)` 之后 `n` 不再是持有者，再访问 `n.body` 是编译错误。

值类型装入的本来就是一份拷贝，因此 `s` 仍然可用，访问 `s.w` 不受影响。

取出要用 `as`，没有其他方式。若装箱值中装的不是所请求的类型，运行时会抛出 10011 号错误（`拆开的类型对不上`），可以被 `catch`。没有“拆不开就返回 `null`”的版本：`null` 不会停在取出这一行，它会继续向下传递，等到下一次解引用才暴露，之后已经查不出是哪一次取出出错。

抛出错误则当场带上错误码和消息，在 `catch` 里就能决定怎么处理。

```nf
struct Size {
    w:int
    h:int
}

let s:Size = Size { w: 3, h: 4 }
let o:object = s as object

let back:Size = o as Size
console.print(back.w * back.h)   // 12
```

## 类型检查：`is` 操作符

`x is T` 只判断类型，不取出值，结果是 `boolean`：

```nf
class Note {
    body:string
}

class Marker {
    tag:int
}

let box:list<object> = []
box.add(new Note(body: "甲") as object)
box.add(new Marker(tag: 7) as object)

for (i:int in 0 until length(box)) {
    let one:object = box[i]
    if (one is Note) {
        let n:Note = one as Note
        console.print(n.body)
    } else {
        console.print("不是便条")
    }
}
```

```text
甲
不是便条
```

`is` 不会自动收窄类型，判断通过后仍需写出 `as` 来取值。

从容器中取出对象永远是两步：先用 `let one:object = box[i]` 把这个位置上的值转移出来，再用 `as` 拆箱。

> 注意：两种“更短”的形式会被编译器拒绝。`box[i] as Note` 不合法：下标取出的值还没有归属，`as` 无法处理它。`for (o:object in box)` 交出的 `o` 是只读借用，也不能 `as`：借来的引用没有所有权，无法拆开其他容器中的内容。

`is` 的右侧需要是具名类型（`class`、`struct`、基础类型），不能是 `interface` 的名称。

## 练习

把 `Point` 改成 `class Point`，保留字段 `x`、`y`，并添加一个 `method move(dx:int, dy:int)`。

保留 `let b:Point = a` 之后打印 `a.x` 的语句，观察编译器的报错内容。想清楚为什么换成另一个关键字之后，这两条语句就不能同时存在。

再进一步：把若干对象装入 `list<object>`，用 `is` 区分哪些是 `Point`、哪些是 `Counter`，各输出一项内容。

## 本节要点

`struct` 是值类型：整份拷贝、逐字段判等、字段只能是值类型、`string`、`struct`。

`class` 是引用类型：赋值与传参转移所有权、判等比较对象身份、字段可以是任意类型。

需要在同一处存放不同类型的值时用 `object`，取出时使用 `as`，判断类型时使用 `is`。

下一节把“必须实现哪些方法”交给编译器核对：`interface` 与 `enum`。

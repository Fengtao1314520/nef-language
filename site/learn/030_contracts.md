# 接口与枚举

写第 8 课的 `class` 时可能已经注意到：字段和方法都要逐个声明。这一课处理两件相关的事：一类类型**必须**提供哪些方法（`interface`），以及一组固定的整数如何命名（`enum`）。

两者的共同点是减少读代码时的猜测。方法是否遗漏由编译器核对；散落在各处的错误码 `100`、`101` 可以改成有名字的成员。

## interface

如果“每一类都必须实现这两个方法”只写在一句说明里，编译器不参与检查：说明会过时，遗漏也发现不了。

把这份约定写成 `interface`，用 `base` 关联就表示承诺实现，缺少一个方法就在编译期报错。

```nf
interface Shape {
    area():float
    name():string
}

class Circle base Shape {
    r:float

    method area():float { return 3.14159 * r * r }
    method name():string { return "圆" }
}

let c:Circle = new Circle(r: 2.0)
console.print(c.name(), c.area())
```

```text
圆 12.56636
```

前两行是方法声明：`area():float`、`name():string`，只有签名没有实现。`Circle base Shape` 把自己绑定到这份声明上，两个方法都必须写出实现。

接口中的方法声明不带 `method`，只保留签名。

加上 `method` 是编译错误：这个关键字只出现在带函数体的定义前面。

`base` 后面只能跟一个 `interface`。

`class` 不从其他 `class` 继承，`struct` 不从其他 `struct` 继承。`interface` 自身可以再 `base` 另一个 `interface`，把父接口的字段与方法声明一并继承过来。

接口声明的每个方法都必须给出实现，漏一个就是编译错误；参数名、参数类型、参数个数、返回类型和参数前的 `ref` 这五项都要与声明完全一致。

声明是 `add(v:float):float`，实现写成 `add(count:float):float` 同样是错误，编译器的原文是 `C.add 的第 1 个参数该叫 v`。

参数名同样统一，读接口就能确定调用的形式，不必进入实现确认。

## 接口中的字段

接口里也可以声明字段，继承下来的字段自动拥有，实现者体内不再重写：

```nf
interface AB {
    a:int
    b:float
}

class C base AB {
    method show():int { return a + 1 }
}

let one:C = new C(a: 1, b: 2.5)
console.print(one.show())     // 2
```

`a` 与 `b` 由接口带入，`C` 的体内没有重写，`new C(a: 1, b: 2.5)` 仍然需要给出这两个字段。

`struct` 只能继承不含方法声明的接口。

原因是 `struct` 体内不能声明方法；接口一旦声明方法，`struct` 就没有实现它的位置。

## 接口名称不能当类型用

把实现同一接口的对象放进同一个列表 `list<Shape>` 是一个很自然的想法。编译器的原文是 `接口名，不能当类型`：

变量、参数、返回值、容器元素的位置上只能声明 `struct`、`class` 与基础类型的名称，声明接口名称是编译错误。

```nf-bad
interface Shape {
    area():float
}

method total(s:Shape):float {    // 报：Shape 是接口名，不能当类型写
    return s.area()
}
```

`interface` 只在定义处起作用：编译器用它核对实现者的方法与参数名是否完整。

运行时不会用接口名反查“这个对象实现过哪个接口”，代价是不能声明 `list<Shape>`，好处是每个堆上的对象不必额外携带一块类型标识。

需要把实现同一接口的对象放在一起时，改用 `object`：装箱存入，用 `is` 判断，用 `as` 取出，见[自定义类型](/learn/029_types)。

## enum

```nf
enum Door { open, closed, broken }

console.print(Door.open == Door.closed)   // false
```

成员值只允许整数。

省略值时取上一个成员加一，默认从 `0` 开始，因此上面三个是 `0、1、2`。

存储宽度与 `int` 相同（8 字节），但只支持取成员、判等、作为 `switch` 的标签、作为 `dict` 的键。

`Door.open + 1` 是错误，枚举不参与算术：一个错误码加一没有意义。

从枚举到整数的转换只允许单向：`as int` 取出它的数值，反过来 `int as Door` 不被接受。

```nf
enum Err { bad_path = 100, empty = 101 }

let first:int = Err.bad_path as int

console.print(first, Err.empty as int)   // 100 101
```

最典型的用途正是错误码：`throw (Err.bad_path as int, "路径不能是空的")`。`throw` 的码位要求 `int`，不加 `as int` 就无法抛出。

成员必须通过类型名访问。成员名不会进入外层作用域，因此两个不同的 `enum` 可以有同名成员。

> 注意：同一个 `enum` 中两个成员不允许取同一个整数。`enum D { a = 5, b = 5 }` 是编译错误：一个数值对应两个名称时，`switch` 的标签与 `D.a == D.b` 都会产生两种解释。

## 枚举与 switch

枚举配 `switch` 的完整匹配规则在[第 6 课：判断与循环](/learn/027_flow)已经写过一遍：成员必须列全，漏掉又没有 `default` 是编译错误。

这条规则发挥作用的时候是给 `enum` 添加新成员之后：所有漏掉该成员的 `switch` 会被一并报错，不必把整个项目搜索一遍。

## 练习

给 `Shape` 增加一个方法 `perim():float`，让 `Circle` 实现它，再故意让一个实现者缺少这个方法，观察编译器给出的错误内容。

补上 `method perim():float { return 6.28318 * r }`，确认缺少方法的编译错误随之消失。

再练习一次：先让第 6 课 `Door` 的 `switch` 通过检查，然后给 `Door` 添加第四个成员，其他代码不动，直接运行 `nf check`。先估计这次检查会给出几处错误。

## 本节要点

`interface` 让编译器在定义处核对“方法是否齐全、参数名是否一致”，但接口名称不能用作类型；`enum` 为整数命名，不参与算术，只在需要 `int` 的位置用 `as int` 取出数值。

下一节处理失败情况：`throw` 抛出、`try`、`catch` 捕获，以及语言内置错误为什么同样可以捕获。

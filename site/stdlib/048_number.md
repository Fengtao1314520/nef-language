# 数值与数学

这一页是两组函数：`number` 负责把数值转换成文本，`math` 负责数学运算。

`number.to_string(value)` 能接收除 `string` 之外的所有类型：文本本来就是文本，传进来只会多复制一份。

基础类型按各自的格式转换，`struct`、`class`、三种容器与 `object` 用的格式规则与 `console.print` 相同，输出结果与打印到屏幕上的内容一字不差。

小数按最短往返格式输出，而且必带小数点。要把数值直接输出给人看，用 `console.print`。

## number

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `number.to_string_in_base(value, base)` | `string` | 不抛 | 按进制把数值写成文本：`number.to_string_in_base(255, 16)` 得到 `"ff"`。进制参数的要求与 `text.to_int_in_base` 相同：必须写成字面量，只能是 `2`、`8`、`10`、`16`。字母是小写；负数把 `-` 放在最前面，其余部分按绝对值书写；`0` 就是 `"0"` |
| `number.to_string(value)` | `string` | 不抛 | 基础类型按各自的格式输出：`int`、`byte`、`enum` 输出十进制，`boolean` 输出 `true` 或 `false`，`unicode` 输出对应的字符，`float` 输出最短往返且必带小数点的写法。`struct` 与 `class` 返回 `类型名 { 字段: 值, … }`，字段按登记的顺序排列，嵌套一层就接着按同样的格式展开，文本字段带引号，未指向任何对象的字段返回 `null`。三种容器与 `object` 返回的形状与 `console.print` 的输出完全相同（见[屏幕输出与输入](/stdlib/045_console)那一页的形状表）。`string` 本身不需要这个函数：传进来只会多复制一份 |

```nf
let n:int = 42
let x:float = 3.0

console.print(number.to_string(n) + "，" + number.to_string(x))   // 42，3.0
```

小数自带一个小数点：`float` 的输出格式保证按原样读回时还是同一个数。

`struct` 与 `class` 返回的形状就是书写字面量时用的格式：

```nf
struct Point {
    x:int
    y:float
    tag:string
}

let p:Point = Point { x: 3, y: 1.5, tag: "起点" }
console.print(number.to_string(p))
```

```text
Point { x: 3, y: 1.5, tag: "起点" }
```

嵌套一层就接着按同样的格式展开，`class` 的字段按登记的顺序排列（`new` 出来还没有被写入过的字段，句柄为空时输出 `null`）。
字段里放着整个容器或者一个盒子，也照样展开：`console.print` 使用的那套格式规则由两个函数共用，所以两边的输出内容一字不差。
数据中出现指回自身的循环引用时，被重复引用的对象输出 `...`，不会无限拼接下去。

```nf
struct Point {
    x:int
    y:int
}

let pts:list<Point> = [Point { x: 1, y: 2 }, Point { x: 3, y: 4 }]
let ages:dict<string, int> = {"阿明": 31}
let one:object = 42 as object

console.print(number.to_string(pts))
console.print(number.to_string(ages))
console.print(number.to_string(one))
console.print(number.to_string(3.0) + " 与 " + number.to_string(true))
```

```text
[Point { x: 1, y: 2 }, Point { x: 3, y: 4 }]
{"阿明": 31}
42
3.0 与 true
```

这个函数存在的理由是要把结果当文本使用：`to_string` 返回的是一段真正的文本，可以比较、可以切分、可以继续拼接。只想把结果给人看一眼，就直接用 `console.print`。

反方向的操作（把文本读取成数值）写在文本处理那一组：整数用 `text.to_int(s)`，带小数点的用 `text.to_float(s)`。这两个函数属于解析，会抛 10012 号，所以不放在 `to_string` 这一组里。

## math

这一组一共二十个函数，名字都是完整单词。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `math.absolute_value(number)` | 与传入时同一类型 | 10021 | 传入 `int` 返回 `int`，传入 `float` 返回 `float`；`byte` 返回 `int`。对 `int` 的最小值取绝对值会溢出，抛 10021 号（`整数运算溢出`） |
| `math.floor(x)` | `int` | 10013 | 向下取整，返回不超过输入的最大整数 |
| `math.ceiling(x)` | `int` | 10013 | 向上取整，返回不小于输入的最小整数 |
| `math.round(x)` | `int` | 10013 | 就近取整，`.5` 向数轴正方向进位 |
| `math.minimum(a, b)`、`math.maximum(a, b)` | 与传入时同一类型 | 不抛 | 两个值必须是同一类型，不做隐式转换 |
| `math.power(base, exponent)` | `int` | 10012、10021 | 两个参数都是 `int`。指数为负抛 10012 号（`指数不能是负的`），结果超出 `int` 抛 10021 号 |
| `math.square_root(x)` | `float` | 不抛 | 平方根。输入为负数时返回 `NaN`，不抛出错误；`0.0` 返回 `0.0` |
| `math.pi()` | `float` | 不抛 | 圆周率 `3.141592653589793`。这个函数不带参数，写法与 `time.now()` 相同：写在组名下面的成员只有函数这一种形式 |
| `math.sine(x)`、`math.cosine(x)` | `float` | 不抛 | 正弦与余弦，参数是弧度不是角度。要用角度就先算 `角度 * math.pi() / 180.0`。`±Inf`、`NaN` 返回 `NaN` |
| `math.natural_log(x)` | `float` | 不抛 | 自然对数。`0.0` 返回 `-Inf`、负数返回 `NaN`。结果与最精确的值在末几位上有差距：先调用机器的 `log₂`，再乘一次 `ln(2)`，两步各做一次舍入 |
| `math.tangent(x)` | `float` | 不抛 | 正切，参数是弧度。在余弦为 `0` 的位置（π/2 这类弧度）返回正负无穷；`±Inf`、`NaN` 返回 `NaN`。这几种结果都是除法本身给出的 |
| `math.arctangent(x)` | `float` | 不抛 | 反正切，返回的是弧度，取值在 `-π/2` 到 `π/2` 之间。正负无穷返回正负 π/2 |
| `math.log_base_10(x)` | `float` | 不抛 | 以 10 为底的对数。`0.0` 返回 `-Inf`、负数返回 `NaN`。结果与最精确的值在末几位上有差距：换底要多走一步，那一步同样要做一次舍入，`natural_log` 的差距也是这么来的 |
| `math.exponential(x)` | `float` | 不抛 | 求 e 的指定次方。大于 `709.782712893384` 返回正无穷、小于 `-745.1332191019411` 返回 `0.0`、`NaN` 返回 `NaN`，都不抛出错误：超出 `float` 能容纳的最大值与低于最小值这两种情况，IEEE 本来就规定了这样的结果 |
| `math.sign(number)` | `int` | 不抛 | 判断正、负还是零，返回 `1`、`-1`、`0`。`int` 与 `float` 都接受，返回的一律是 `int`；`-0.0` 按零处理。`NaN` 也返回 `0`，因为它既不是正也不是负。要判断 `NaN` 就写 `x != x` |
| `math.set_seed(seed)` | 没有 | 不抛 | 设置随机数种子（见下文“随机数”一节） |
| `math.random_float()` | `float` | 不抛 | `0` 到 `1` 之间的小数，包含 `0`、不包含 `1`。与 `math.random` 共用同一个随机数种子 |
| `math.random(low, high)` | `int` | 10020 | 包含起点、不包含终点，规则与 `until` 相同。没有调用 `math.set_seed` 时，种子取自系统里的高精度计数器，每次运行的结果都不同；调用过之后，同一个程序两次运行会得到同一串结果。范围为空（`起 ≥ 止`，两者相等也算空）时抛 10020 号，可以 `catch` |

`math.floor`、`math.ceiling`、`math.round` 返回的是 `int`，所以无法放进 `int` 的数会抛 10013 号：大到两百多倍的小数、`±Inf`、`NaN` 都会执行到这条路径。

`-9223372036854775808.0` 正好是 `int` 的下界，`math.floor` 返回它本身：它是唯一一个还落在下界之内的 2 的六十三次方，取正号就不行了。

```nf
console.print(math.absolute_value(-7), math.absolute_value(-7.5))
console.print(math.floor(2.9), math.ceiling(2.1), math.round(2.5))
console.print(math.minimum(3, 7), math.maximum(3.5, 1.25))
console.print(math.power(2, 10))
console.print(math.pi(), math.sine(0.0), math.cosine(0.0), math.natural_log(1.0))
console.print(math.sine(3.141592653589793 / 2.0) > 0.99, math.natural_log(math.pi()) > 1.14)
console.print(math.random_float() >= 0.0, math.random_float() < 1.0)
```

```text
7 7.5
2 3 3
3 3.5
1024
3.141592653589793 0.0 1.0 0.0
true true
true true
```

## 随机数

`math.random(low, high)` 包含 `起`、不包含 `止`。没有设置种子时，它取自系统里的高精度计数器，每次运行得到的序列都不同。

需要一串固定不变的数（写测试、要求别人能复现同一个结果）时，就先调用 `math.set_seed(seed)`，往后每一次 `math.random`、`math.random_float` 都接着这个种子往下生成。

传入 `0` 时会自动换成一个常数：种子为 `0` 时生成多少次都还是同一个数，那就成了假的随机。

```nf
let dice:int = math.random(1, 7)
console.print(dice >= 1, dice < 7)
console.print(math.random(9, 10))
```

```text
true true
9
```

范围为空时当场抛出 10020 号，可以 `catch`。`起 == 止` 也算空：既然不包含止，那个唯一的值也取不到。

设置种子之后连续调用两次，第二次返回的数与第一次相同：

```nf
math.set_seed(20261011)
console.print(math.random(1, 7), math.random(1, 7))
math.set_seed(20261011)
console.print(math.random(1, 7), math.random(1, 7))
```

```text
3 3
3 3
```

四个小数运算函数与取正负号的函数放在一起是这样：

```nf
console.print(math.sign(-12), math.sign(0.0 - 2.5), math.sign(7))
console.print(math.floor(math.log_base_10(1000.0)), math.arctangent(1.0) * 4.0 > 3.14)
console.print(math.exponential(0.0), math.tangent(0.0))
```

```text
-1 -1 1
3 true
1.0 0.0
```

## 三个容易出错的地方

`math.minimum(3, 3.5)` 是编译错误：两个参数的类型不一致，而这里不做隐式转换。要么都写成整数，要么都写成小数。

`math.power` 只有整数版本，指数为负或者结果超出 `int` 都会抛出错误（分别是 10012 号和 10021 号），不会给出一个近似值。

`math.floor` 返回的是 `int`，不是取整之后的小数。要把一个小数向零截断，写 `x as int`：`3.9 as int` 得到 `3`，`-3.9 as int` 得到 `-3`；而 `math.floor(-3.9)` 得到 `-4`。

## `.5` 往哪边进

一律向数轴正方向进位，等价于 `floor(x + 0.5)`。

所以 `math.round(2.5)` 得到 `3`，`math.round(-2.5)` 得到 `-2`，`math.round(-2.6)` 得到 `-3`。负数一侧的规则不是“远离零”，不能凭直觉推断。

## 哪一步先报错

`console.print(math.absolute_value(-9223372036854775807 - 1))` 会发生什么？是输出一个数值，还是中途抛出错误？

先推测结果，再把它包进 `try`、`catch` 运行一遍，把错误码输出出来，看是哪一个号。

## 参见

- [文本处理](/stdlib/046_text)：`text.to_int`、`text.to_float` 与 `text.to_int_in_base`，反方向的解析
- [字节块](/stdlib/047_bytes)：`bytes.new(size)`，一段连续内存
- [错误信息索引](/tools/060_errors)：10012、10021 两句报错的原文

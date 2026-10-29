# 文本处理

`text` 这一组的写法分两种：作用于字符串自身的成员用点号写法，涉及类型转换的用组函数写法。

二十五个成员里，十八个属于字符串类型，七个写在组名上。

属于字符串的十八个成员包括 `to_upper`、`to_lower`、`trim`、`split`、`contains`、`index_of`、`starts_with`、`ends_with`、`replace`、`slice`、`to_list`。

另有在两端补齐宽度的 `pad_start`、`pad_end`。

还有五个同样属于字符串：从末尾向前查找的 `last_index_of`，只去除一端空白的 `trim_start`、`trim_end`，把自身重复若干遍的 `repeat`，以及返回字节块的 `to_bytes`。

写在组名上的七个是 `text.join`、`text.to_int`、把文本解析成小数的 `text.to_float`、按进制解析整数的 `text.to_int_in_base`。

另外三个处理的是字节：文本与字节块互相转换的 `text.from_bytes`，以及 base64 的一对 `text.to_base64`、`text.from_base64`。

再加上语言自带的 `+` 拼接、六种比较运算符、`length`、按下标取一个码点的 `s[i]`、按码点遍历的 `for (ch:unicode in s)`，就是这一页的全部写法。

这一组有两处限制：大小写转换与空白处理只针对 ASCII 字符，`text.join` 接收的必须是 `list<string>`。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `s.to_upper()`、`s.to_lower()` | `string` | 不抛 | 把字母转成大写或小写，只处理 ASCII 字符 |
| `s.trim()` | `string` | 不抛 | 去掉文本两端的空白 |
| `s.split(separator)` | `list<string>` | 不抛 | 按分隔符把文本切分成列表 |
| `text.join(parts, separator)` | `string` | 不抛 | `split` 的反向操作：用分隔符把列表元素拼接成文本；接收的必须是 `list<string>`，这个要求编译期就会检查 |
| `s.contains(needle)` | `boolean` | 不抛 | 判断文本里是否包含要找的内容 |
| `s.index_of(needle)` | `int` | 不抛 | 返回要找的内容第一次出现时的码点位置；一处都没有时返回 `-1`；要查找的是空文本时，算在第 0 个码点 |
| `s.starts_with(prefix)`、`s.ends_with(suffix)` | `boolean` | 不抛 | 判断文本是否以指定内容开头或结尾；指定内容为空时，两端都算符合 |
| `s.pad_start(width, padding)`、`s.pad_end(width, padding)` | `string` | 不抛 | 在一端补齐到指定宽度。宽度按码点计数，与 `length` 的计数标准一致；原有文本已经达到指定宽度时按原样返回一份新文本；填充内容不足时重复取用，最后一次只截取填满宽度所需的长度 |
| `s.replace(from, to)` | `string` | 不抛 | 替换查找到的内容，生成一段新文本 |
| `s.slice(start, end)` | `string` | 10031 | 截取一段文本，含起点、不含终点；下标为负、`start > end`、`end` 超出长度这三种情况都抛 10031 号（`下标越界`） |
| `s.to_list()` | `list<unicode>` | 不抛 | 把文本拆成码点列表 |
| `text.to_int(text)` | `int` | 10012 | 把文本解析成整数；无法解析时抛 10012 号（`文本读不成数字`） |
| `s.last_index_of(needle)` | `int` | 不抛 | 从末尾向前查找，返回要找的内容最后一次出现时的码点位置；一处都没有时返回 `-1`。要查找的是空文本时返回 `length(s)`，空文本算在末尾，与 `index_of` 返回 `0`（空文本算在开头）互相对应 |
| `s.trim_start()`、`s.trim_end()` | `string` | 不抛 | 只去除一端的空白。哪些字符算空白与 `trim` 完全相同，都是空格、`\t`、`\n`、`\v`、`\f`、`\r` 这六个字节。整串都是空白就返回空文本 |
| `s.repeat(count)` | `string` | 不抛 | 把自身重复 `count` 遍。`count` 为 `0` 或负数时返回空文本，不抛出错误 |
| `s.to_bytes()` | `bytes` | 不抛 | 返回这段文本的 UTF-8 字节组成的字节块（见[字节块](/stdlib/047_bytes)）；不带 BOM。空文本返回空的字节块 |
| `text.from_bytes(data)` | `string` | 10044 | `to_bytes` 的反向操作。传入的字节不是合法的 UTF-8 时抛 10044 号（`文件不是合法的 UTF-8`）；`file.read` 读到非法字节时抛出的也是这一个错误码 |
| `text.to_base64(text)` | `string` | 不抛 | 先取文本的 UTF-8 字节，再每三个字节输出四个字符，不足三个的末尾用 `=` 补齐 |
| `text.from_base64(encoded)` | `string` | 10014、10044 | 解码结果是文本。长度不是 4 的整数倍、`=` 出现在中间、含有字母表以外的字符，这三种情况都抛 10014 号（`文本读不成 base64`）；解出来的字节不是合法的 UTF-8 时抛 10044 号 |
| `text.escape_json(text)` | `string` | 不抛 | 把文本写成 JSON 里引号中间的内容：`"` 与 `\` 前面各加一个 `\`，`\b`、`\t`、`\n`、`\f`、`\r` 五个写成两个字符的短写法，其余小于 `0x20` 的控制字符写成 `\u00xx`（数字用小写），别的一律原样保留。非 ASCII 不转成 `\u`，`"甲"` 本身就是一段合法的 JSON。两端不加引号：这个函数转换的是内容本身 |
| `text.to_int_in_base(text, base)` | `int` | 10012、10013 | 按进制把文本解析成整数：`ff` 以 16 为进制是 `255`。进制只能写成 `2`、`8`、`10`、`16` 四个数之一，而且必须是字面量，编译期就会检查这个要求；除 `0` 到 `9` 外，`a` 到 `z` 与 `A` 到 `Z` 按 10 到 35 识别；不允许出现空白、`0x` 前缀和小数点 |
| `text.to_float(text)` | `float` | 10012 | 把文本解析成小数：可选的正负号、中间一个小数点（`.5` 是 `0.5`、`5.` 是 `5.0`）、末尾可以带 `e` 或 `E` 加符号的指数。前后不跳过空白，要去空白先 `trim`；有效数字最多取 17 位，超过 `float` 范围的返回正负无穷，不抛出错误 |
| `length(value)` | `int` | 不抛 | 返回文本的码点个数 |

按下标取一个码点的 `s[i]` 用的是语言自带的记号，越界时同样抛 10031 号；改写其中一个码点，例如 `s[0] = 'X'`，是编译错误，因为文本不可变。全部错误码的报错原文见[错误信息索引](/tools/060_errors)。

```nf
let line:string = "  甲,乙,丙  "
let clean:string = line.trim()
let parts:list<string> = clean.split(",")

console.print(length(parts), text.join(parts, "|"))
console.print(clean.contains("乙"), parts[0].to_upper())
```

```text
3 甲|乙|丙
true 甲
```

对读进来的一行数字求和，步骤相同：先用 `trim` 去掉两端空白，用 `split` 切分，再用 `text.to_int` 把每一项解析成数值，最后用 `text.join` 拼接回去。

```nf
let line:string = "  12 30 7  "
let parts:list<string> = line.trim().split(" ")
let sum:int = 0
for (p:string in parts) {
    sum = sum + text.to_int(p)
}

console.print(text.join(parts, "+"), sum)
```

```text
12+30+7 49
```

分隔符连续出现两次，中间就切出一段空文本：`"a,,b"` 切出三段，其中一段是空文本。

空文本无法解析成数值，`text.to_int` 在这里抛 10012 号。上面这个例子的分隔符都是单个空格，`trim` 又把两端的空白清除干净，才没有遇到这种情况。

读取小数时步骤相同，只是把整数解析换成 `text.to_float`：

```nf
let line:string = "  1.5 20 0.25  "
let parts:list<string> = line.trim().split(" ")
let sum:float = 0.0
for (p:string in parts) {
    sum = sum + text.to_float(p)
}

console.print(sum)
console.print("7".pad_start(3, "0"), "|", "月".pad_end(3, "·"), "|")
```

```text
21.75
007 | 月·· |
```

`pad_start`、`pad_end` 按码点计数，所以上面的输出把“月”补齐到三个码点，后面跟着两个填充用的点号。

## 文本与字节、base64 与进制

文本在内存里就是 UTF-8 字节。`s.to_bytes()` 把这段文本的字节原样放进一块 `bytes` 返回，`text.from_bytes` 再把这块字节转换回文本。

转换回来时要先校验一次：字节序列必须是合法的 UTF-8，否则抛 10044 号。`file.read` 读到非法字节时抛的是同一个错误码。

`text.to_base64` 与 `text.from_base64` 处理的也是这批字节：编码时按文本的 UTF-8 字节编码，解码后的字节还要再校验一次是否合法。

解码时如果长度不是 4 的整数倍、`=` 出现在中间、含有字母表以外的字符，抛 10014 号（`文本读不成 base64`）。

```nf
let s:string = "甲a"
let bs:bytes = s.to_bytes()
console.print(length(bs), bs[0], text.from_bytes(bs) == s)
console.print(text.to_base64(s), text.from_base64("YWJj"))
console.print(text.to_int_in_base("ff", 16), text.to_int_in_base("1010", 2))
```

```text
4 231 true
55SyYQ== abc
255 10
```

`text.to_int_in_base(text, base)` 与 `number.to_string_in_base(value, base)` 是一对反向操作，写出方向的函数在[数值与数学](/stdlib/048_number)。

进制参数必须写成字面量，而且只能是 `2`、`8`、`10`、`16` 四个之一。写成计算出来的结果或者其他数值，编译期就会报错，报错原文是“进制要写成一个数，不能是算出来的”。

`text.escape_json(text)` 处理另一类转换：把文本写成 JSON 里引号中间的内容。

转换的是 `"` 与 `\`，以及五个有短写法的控制字符（`\b`、`\t`、`\n`、`\f`、`\r`），其余小于 `0x20` 的字符写成 `\u00xx`。

其余字符一律原样保留，汉字不转成 `\u`：`"甲"` 本身就是一段合法的 JSON。两端不加引号：这个函数转换的是内容本身，引号由编写者自己加上。

```nf
console.print(text.escape_json("他说\"甲\"\t完了"))
console.print(text.escape_json("反斜杠 \\ 摆在这儿"))
```

```text
他说\"甲\"\t完了
反斜杠 \\ 摆在这儿
```

## 每次都生成新文本

文本不可变：上面每个成员返回的都是一段新文本，原有文本没有改动。

想修改一个字符只能拼接出新的文本，`s[0] = 'X'` 是编译错误。拼接长文本时这一规则有代价：循环里的一句 `s = s + x` 每一轮都要新建一段，文本越长开销越大。

批量拼接用 `text.join`：先把片段存入 `list`，最后一次性合并。

## 按码点算

`length(s)`、`s[i]`、`s.slice(start, end)` 计数的单位都是码点，不是字节：`"中文abc"` 的长度是 5，`s[0]` 是 `'中'`。

内存里按 UTF-8 字节存放，文本开头记录着码点个数，所以 `length` 只要读出这个数，`s[i]` 要按字节走到对应码点的位置。如果要在文本中间反复随机取字符，先转换成码点的列表：

```nf
let chars:list<unicode> = "中文".to_list()

console.print(length(chars), chars[1])   // 2 文
```

`s.slice(start, end)` 的越界也包括起点与终点写反这一种：`s.slice(3, 1)` 同样抛 10031 号，不会静默地返回一个空串。终点可以正好等于码点个数，这时截取结果就到文本结尾。

## 大小写与空白只处理 ASCII

`to_upper`、`to_lower` 只处理 `a` 到 `z` 与 `A` 到 `Z` 这 52 个字节，其余一律原样输出；`trim` 去除的是空格、`\t`、`\n`、`\v`、`\f`、`\r` 这六个字节。

中文本来就没有大小写，这条限制对它没有影响。

带重音的拉丁字母就不一定了：`"é".to_upper()` 返回的还是 `"é"`。混排时只有 ASCII 那 52 个字节会被处理：`"Ünïcode".to_upper()` 得到 `"ÜNïCODE"`，`Ü` 与 `ï` 原样保留。

要按 Unicode 的大小写表处理，就得自己在程序里带上这张大小写对照表，这一组不提供。

UTF-8 的续字节都大于 `0x80`，不会与 ASCII 字母的字节值相同，所以逐字节扫描不会把一个多字节字符改坏。这是 UTF-8 编码方式带来的一个附带好处。

## 解析不成数字时抛出错误

`text.to_int` 是解析，不是类型转换，所以不走 `as`。无法解析成数字时抛 10012 号，可以 `catch`：

```nf
try {
    console.print(text.to_int("30"))
} catch (code, msg) {
    console.print("这不是数字:", code)
}

try {
    console.print(text.to_int("三十"))
} catch (code, msg) {
    console.print("这不是数字:", code)
}
```

```text
30
这不是数字: 10012
```

整数解析只有这一种写法。小数的解析没有对应的函数，要自己按 `.` 拆开计算。标准库没有规定读到 `"3.14"` 应该得到什么，这件事的规则由程序自己定。

## 切分与拼接

`s.split(separator)` 返回一个列表，`text.join(parts, separator)` 把它重新拼接成一个文本。分隔符可以是多个字符。

`text.join` 接收的必须是 `list<string>`。`list<int>` 过不了类型检查：`这里要的是 list<string>，给的是 list<int>；类型之间不自动转换`。

要把数值拼接进去，先逐个用 `number.to_string` 转换，再放入列表。

`s.split` 用点号写法，因为被切分的 `s` 就是被操作的对象；`text.join` 用组函数写法，因为它把一个列表转换成了文本，发生了类型转换。

切分后的列表里可能含有空串。文本以分隔符结尾时，最后会多出一个空串；连续两个分隔符之间也是一个空串。`split` 一律保留这些空串，不做删除。

## 换行与空白

按行处理文件内容时，行尾可能带一个回车符。`trim()` 会把两端的空白连同回车符一起去掉，通常这就够了。

要精确判断回车符本身，五个转义记号里没有 `\r`，只能写成 `"\u000D"`。这种写法长一些，换来的是转义表小到能背下来。

## 练习

把 `"a,b,,c"` 用 `split(",")` 切开之后，列表里有几个元素？要想清楚其中的空串各自对应原文的哪一段。

## 参见

- [字节块](/stdlib/047_bytes)：`bytes` 类型的创建、按下标读写、截取、Base64 编码与拼接

# 正则表达式

`regular_expression` 这一组共有四个函数，按模式在文本里查找内容，四个函数各自的作用写在下面的成员表里。

表达式能接受的记号全部列在下文的表格里。

表格外的记号一律不接受，写出来会在运行期抛 10071 号。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `regular_expression.match(text, pattern)` | `boolean` | 10071 | `pattern` 能否在 `text` 里匹配成功 |
| `regular_expression.find(text, pattern)` | `list<string>` | 10071 | 匹配到的全部片段，按出现的先后顺序排列 |
| `regular_expression.replace(text, pattern, replacement)` | `string` | 10071 | 把 `text` 里每一处非空的匹配都换成 `replacement`，返回新的文本 |
| `regular_expression.split(text, pattern)` | `list<string>` | 10071 | 按表达式切分 |

四个函数都在**运行期**抛 10071 号：表达式写错时，错误消息里带有出错的位置，例如 `regular_expression 的字符组没有收尾的 ]`。

表达式可以来自变量与字符串拼接，`nf check` 看不到它的内容，所以这个错误无法在编译期发现，只能用 `catch` 捕获。

```nf
let code:string = "let total:int = 0"

console.print(regular_expression.match(code, "let\\s+\\w+"))
console.print(regular_expression.replace(code, "total", "count"))
console.print(regular_expression.split(code, " ")[0])
```

```text
true
let count:int = 0
let
```

表达式就是一个普通字符串，所以正则里的反斜杠要按字符串的规则书写：`\s` 在源码里要写成 `"\\s"`。表达式写错时抛 10071 号，可以 `catch`。

`regular_expression.find` 返回一个 `list<string>`。整个列表可以直接输出；要取第几个匹配到的片段，就按下标读取：

```nf
let code:string = "let total:int = 0"

let found:list<string> = regular_expression.find(code, "\\w+")
console.print(found)
console.print(length(found), found[0], found[3])
```

```text
["let", "total", "int", "0"]
4 let 0
```

## 表达式里能写什么

| 类别 | 写法 |
| --- | --- |
| 一个字符本身 | 任何字符都只代表它自己。`\` 后面跟的不是数字类、字母类、空白类的转义时，表示紧跟在它后面的那个字符本身：`\\`、`\.`、`\*`、`\[` |
| 三类字符 | `\d` 数字、`\w` 字母数字下划线、`\s` 空白；大写的 `\D` `\W` `\S` 分别是各自取反的结果 |
| 字符组 | `[abc]`、`[a-z]`、`[^a-z]`。`-` 写在开头或末尾时就表示它自己；要让 `]` 表示字符本身，就把它放在第一位 |
| 任意一个字符 | `.`，换行符也算一个字符 |
| 重复 | `*` 零个或多个、`+` 一个或多个、`?` 零个或一个，三者都是贪婪匹配，即尽量多匹配 |
| 分组与选择 | `( ... )` 只用来划分范围，不产生捕获组；`a\|b` 返回先匹配成功的分支 |
| 开头与末尾 | `^` 表示文本开头、`$` 表示文本末尾。没有多行模式，一段文本就是一个整体 |

不支持的写法有以下几种：`{2,5}` 次数范围、`*?` 非贪婪匹配、`\1` 反向引用、`(?...)` 的任何形态（命名捕获组、忽略大小写、先行断言）、`\b` 词边界。表达式本身写错时抛 10071 号，可以 `catch`。

`.` 与字符组匹配的都是**一个字符**，汉字算一个字符，不会被拆成三个字节。`\d` `\w` `\s` 只识别 ASCII 字符，汉字不属于这三个类：需要匹配汉字时就把汉字直接写进字符组，例如 `[中甲]`。

## 空匹配的处理

`"x?"` 这类表达式在 `"abc"` 里每一次都匹配不到字符，匹配到的内容是一段空文本。

`regular_expression.find`、`regular_expression.replace`、`regular_expression.split` 三个函数遇到这种空结果时一律跳过：查找的位置向前移动一个字符再继续，不会在每两个字符之间都插入一段空文本：

```nf
console.print(regular_expression.replace("abc", "x?", "-"))
console.print(length(regular_expression.find("abc", "x?")))
console.print(regular_expression.match("abc", "x?"))
for (seg:string in regular_expression.split("abc", "b|c")) {
    console.print("[" + seg + "]")
}
```

```text
abc
0
true
[a]
[]
[]
```

`regular_expression.match` 不受这条规则限制：空结果也算匹配成功，所以第三行输出 `true`。

最后三行是 `"b|c"` 把 `"abc"` 切出的三段：第一段是 `a`，`b` 与 `c` 之间的一段是空文本，`c` 之后的一段也是空文本。`regular_expression.split` 会丢弃分隔符本身，切出的三段全部保留。

## 只有组函数写法

四个函数接收文本，返回其他类型的值，跨出了文本类型，所以一律写成组函数写法。

没有 `string.match(...)` 这种点号写法。

看两个例子：`regular_expression.match("123", "^\\d+$")` 返回 `true`；把 `"123"` 换成 `"123abc"`，返回 `false`，因为 `$` 要求匹配一直延续到文本末尾才算成功。

## 参见

- [文本处理](/stdlib/046_text)：不带模式的切分、替换与查找
- [错误信息索引](/tools/060_errors)：10071 号的报错原文
- [命令行工具](/tools/059_cli)：`nf check` 检查到哪一步为止

# 解析 JSON

配置文件与接口返回的内容，常常是一段 JSON。

这一组 `json` 只负责读取：`json.parse` 把一段文本解析成一棵树，`Json` 指向这棵树的根；`json.explain` 说明解析失败出现在哪一行哪一列；其余十个函数负责判断种类与取值。没有提供把值写成 JSON 的函数。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `json.parse(text)` | `Json` | 不抛 | 解析成功时返回树根，解析失败时返回 `null`。想知道失败原因，紧接着用同一段文本调用 `json.explain` |
| `json.explain(text)` | `string` | 不抛 | 返回一句说明，带行与列，例如 `第 1 行第 15 列：这里该是一个值`。文本本身没有问题时返回空文本 |
| `json.kind(doc)` | `string` | 10092 | 判断一个值是哪一类：`null`、`boolean`、`number`、`string`、`array`、`object` 六个词之一 |
| `json.count(doc)` | `int` | 10092 | 数组有多少个元素、对象有多少个成员；其他种类返回 `0` |
| `json.keys(doc)` | `list<string>` | 10092 | 返回对象的全部键，顺序依照原文。不是对象时返回空列表。返回的是拷贝，树里的键还在原来的位置 |
| `json.has(doc, key)` | `boolean` | 10092 | 判断对象里有没有这个键。只询问，不读走对应位置的值 |
| `json.at(doc, key)` | `Json` | 10092 | 取出对象里该键对应的值。取出即读走：取过一次之后，那个位置就是 `null` |
| `json.item(doc, index)` | `Json` | 10092 | 数组里指定位置的元素。同样是读走。位置为负、或者不小于个数时，返回 `null` |
| `json.to_text(doc)` | `string` | 10092 | 返回文本类型的值的内容，返回的是一份拷贝。其他种类返回 `null` |
| `json.to_integer(doc, fallback)` | `int` | 10092 | 只有整数形态的数能取到值（`3`、`-12`），其他情况返回默认值 |
| `json.to_number(doc, fallback)` | `float` | 10092 | 两种形态的数都能取到值，`3` 也算作 `3.0`，其他情况返回默认值 |
| `json.to_boolean(doc, fallback)` | `boolean` | 10092 | 只有 `true` 与 `false` 能取到值，其他情况返回默认值 |

表里错误码那一列的 10092 说的是同一件事：对着 `null` 取成员。

`json.parse` 解析失败时不抛出错误，它返回 `null`；如果接着拿这个 `null` 去调用 `json.kind`，才会抛出运行期错误。要先判断有没有解析成功，写 `if (doc == null)` 就可以。

## 逐层取值

`json.at` 与 `json.item` 返回的还是一个 `Json`，要继续向下取值就再调用一次。

要把树里的值变成能直接使用的 `string`、`int`、`float`、`boolean`，用表末四条函数。

```nf
let doc:Json = json.parse("{\"name\": \"nef\", \"port\": 8080, \"tags\": [\"语言\", \"编译器\"]}")
console.print(json.kind(doc), json.count(doc))
console.print(json.to_text(json.at(doc, "name")))
console.print(json.to_integer(json.at(doc, "port"), 0))
let tags:Json = json.at(doc, "tags")
console.print(json.count(tags), json.to_text(json.item(tags, 0)))
```

```text
object 3
nef
8080
2 语言
```

取 `port` 时给的默认值是 `0`。文本里没有这个键、或者对应位置放的是 `true`，`json.to_integer` 就取不到值，此时总得返回一个结果，而 `int` 不能是 `null`。

所以 `json.to_integer`、`json.to_number`、`json.to_boolean` 这三条各带一个默认值，写法与[列表·字典·集合](/stdlib/049_containers)里 `dict.get(key, fallback)` 的规则相同。

`json.to_text` 返回的是 `string`，本来就可以是 `null`，所以它不带默认值。

## 解析失败时报告行与列

`json.explain` 不依赖前面有没有解析过，它会拿同一段文本自己重新解析一遍。

这个函数用来让读者看清错误位置，不在性能敏感的路径上使用。

```nf
let bad:string = "{\"a\": 1, \"b\": }"
let tree:Json = json.parse(bad)
console.print(tree == null)
console.print(json.explain(bad))
console.print(json.explain("[1, 2]"))
```

```text
true
第 1 行第 15 列：这里该是一个值

```

最后一次的输出是空的：这段文本没有问题。

行与列都从 1 开始计数，列按字符（码点）计数，所以一个汉字不占三个位置。说明文字写的是解析时看到的具体问题，一共十七种，例如 `文本少了收尾的那一个引号`、`键的后面该有一个冒号`、`值的尾巴上多出来了东西`。

## 键的顺序、重复的键与整棵树的输出

对象的键不按字母顺序排列，顺序依照原文保留。同一个键写两次时，后一个值覆盖前一个，位置仍然是第一次出现的位置。

直接输出一个 `Json` 时用的是 JSON 的写法，紧凑、不带多余空格。

```nf
let dup:Json = json.parse("{\"乙\": 1, \"甲\": 2, \"乙\": 3}")
console.print(json.keys(dup))
console.print(dup)
console.print(json.has(dup, "甲"), json.has(dup, "丙"))
```

```text
["乙", "甲"]
{"乙":3,"甲":2}
true false
```

## 这棵树由谁持有

`Json` 是引用类型，遵循[值的所有权](/learn/026_ownership)的规则：交给一个名字、放进一个位置都会发生交出。

`json.at` 与 `json.item` 是**读走**那个位置上的值，取过之后那个位置变成 `null`，再取一次拿到的也是 `null`。整棵树由指向根的那个名字持有，它离开作用域时，连最里面的文本与键也一起释放。

```nf
let one:Json = json.parse("{ \"a\": 1 }")
let a:Json = json.at(one, "a")
console.print(json.to_integer(a, -1))
console.print(one)
```

```text
1
{"a":null}
```

最后输出的一行并不是出错了：`"a"` 位置上的值已经转由 `a` 持有，树里的这个位置已经空了，所以输出是 `null`。

要再查看一次整棵树，先不要读走，用 `json.has` 判断那个键是否存在。

`Json` 只能出现在名字、实参与返回值里：放进 `list<Json>` 或 `dict<string, Json>`、作为 `class` 的字段，编译期就会拒绝，报 `Json 进不了 list：先把它拆成自己要的值，或者存那段文本`。

要保存一棵树就存 JSON 原文，使用的时候再调用一次 `json.parse`；或者当场把它转换成自己要的值：`int`、`float`、`string`、`boolean`，以及它们的容器。

## 接受哪些写法

只接受严格的 JSON。注释（`//` 与 `/* */`）、数组或对象末尾多出的逗号、以单引号开头的文本、`NaN` 与 `Infinity` 都不接受。

数值分两种形态：整数形态（`3`、`-12`）与小数形态（`3.0`、`1e9`）。整数形态放不进 `int` 时转为小数形态；写法本身不符合规则的数字（`1.`、`1e`、`01`）算作这段文本解析失败。

文本里的 `\uXXXX` 转义接受，代理区的一对码点（`"\uD83D\uDE00"`）合并成一个码点。配不成对的半个代理码点算作这段文本解析失败；这一点比 JavaScript 严格，JavaScript 会把它替换成一个替换字符再继续解析。

嵌套很深时不另外设置深度上限，与[出错的时候](/reference/041_errors)那一页写的栈用完遵循同一条规则：栈用完时报 10093 号，而不是“最多四十层”这种由本语言自己规定的数值。

## 参见

- [读写文件](/stdlib/050_file)：`file.read` 读取一段文本，`json.parse` 接着解析
- [列表·字典·集合](/stdlib/049_containers)：`json.keys` 返回的就是 `list<string>`
- [值的所有权](/learn/026_ownership)：交出与读走这两件事的完整规则
- [错误信息索引](/tools/060_errors)：10092 与 10093 两句报错的原文

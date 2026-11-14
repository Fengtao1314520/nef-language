# 读写 JSON

配置文件与接口返回的内容，常常是一段 JSON。

挂在组名 `json` 上的只有五条：`json.parse` 把一段文本解析成一棵树，`json.explain` 说明解析失败出现在哪一行哪一列。

另外三条起新树：`json.new_object` 与 `json.new_array` 各起一棵空的，`json.from_value` 把程序里的一个值变成一棵树。

其余读写都写在 `Json` 这个类型身上，与 `list`、`dict` 的成员同一性质：取一个位置写成 `tree["键"]` 与 `tree[位置]`，数元素写成 `length(tree)`。

渲染回文本写成 `tree.to_json()`，往里放写成 `tree.set(key, value)` 与 `tree.add(value)`。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `json.parse(text)` | `Json` | 不抛 | 解析成功时返回树根，解析失败时返回 `null`。想知道失败原因，紧接着用同一段文本调用 `json.explain` |
| `json.explain(text)` | `string` | 不抛 | 返回一句说明，带行与列，例如 `第 1 行第 15 列：这里该是一个值`。文本本身没有问题时返回空文本 |
| `j.kind()` | `string` | 10092 | 判断一个值是哪一类：`null`、`boolean`、`number`、`string`、`array`、`object` 六个词之一 |
| `length(value)` | `int` | 不抛 | 数组有多少个元素、对象有多少个成员；其他种类返回 `0` |
| `j.keys()` | `list<string>` | 10092 | 返回对象的全部键，顺序依照原文。不是对象时返回空列表。返回的是拷贝，树里的键还在原来的位置 |
| `j.contains(key)` | `boolean` | 10092 | 判断对象里有没有这个键。只询问，不改动那个位置 |
| `tree[key]` | `Json` | 不抛 | 读对象里该键对应的值。读了不拿走：那一个位置之后还留着，同一把键可以读第二遍 |
| `j.take(key)` | `Json` | 10092 | 把对象里该键对应的值从树上摘下来交给调用方：那一个位置之后是 `null`，键还留着。要接给一个名字就写这一条 |
| `tree[index]` | `Json` | 不抛 | 读数组里指定位置的元素。读了不拿走：那一个位置之后还留着。位置为负、或者不小于个数时，返回 `null` |
| `j.take_at(index)` | `Json` | 10092 | 把数组里那个位置的元素从树上摘下来交给调用方：那一个位置之后是 `null`，个数不变。要接给一个名字就写这一条 |
| `j.to_text()` | `string` | 10092 | 返回文本类型的值的内容，返回的是一份拷贝。其他种类返回 `null` |
| `j.to_integer(fallback)` | `int` | 10092 | 只有整数形态的数能取到值（`3`、`-12`），其他情况返回默认值 |
| `j.to_number(fallback)` | `float` | 10092 | 两种形态的数都能取到值，`3` 也算作 `3.0`，其他情况返回默认值 |
| `j.to_boolean(fallback)` | `boolean` | 10092 | 只有 `true` 与 `false` 能取到值，其他情况返回默认值 |
| `j.to_json()` | `string` | 10092 | 把这棵树连同下面的整一支渲染成一段 JSON 文本，与 `console.print` 打印一个 `Json` 给出的那一字不差。被读走的位置写成 `null` |
| `json.new_object()` | `Json` | 不抛 | 造一棵空的树，形状是对象：还没有成员，`set(key, value)` 与 `tree[key] = value` 往里放 |
| `json.new_array()` | `Json` | 不抛 | 造一棵空的树，形状是数组：`add(value)` 往尾巴上放，`insert_at(index, value)` 往中间放 |
| `json.from_value(value)` | `Json` | 不抛 | 把程序里的一个值变成一棵树：int、float、boolean、string、byte、unicode、enum 各成一个节点，list 成数组，dict 与 struct、class 成对象，一层套一层照形状表走。这一条只读那个值，不动它的归属，返回的是一棵新树。字节块、句柄与 hashset 写不成 JSON，在检查阶段就说出是哪一层写不成 |
| `j.set(key, value)` | 不返回值 | 10092、10114 | 往对象里收下这一对：已有这把键就覆盖那一个位置、把换下来的那棵子树拆掉，没有就添一条。键与值各拷一份或交一份进来，所以调用方手里的那两个名字不受影响；交进来的是另一棵树时，那个名字当场作废 |
| `j.add(value)` | 不返回值 | 10092、10114 | 往数组的尾巴上放一个值。打成对象的那一棵当场抛 10114 号 |
| `j.insert_at(index, value)` | 不返回值 | 10092、10114 | 在数组的那个位置前面放进去一个值，位置等于长度就是往尾巴上放。位置越界当场抛 10031 号，与 `xs.insert_at` 同一条边界 |
| `j.remove(key)` | `boolean` | 10092、10114 | 把那把键连同它的值一起拿走，键在返回真、不在返回假。拿走的值当场拆掉 |
| `j.remove_at(index)` | `Json` | 10092、10114 | 把数组里那个位置的元素摘下来交给调用方，后面的依次前移。摘下来的那一棵由接住它的那个名字持有；位置越界当场抛 10031 号 |
| `j.clear()` | 不返回值 | 10092、10114 | 把这棵树下面的全部成员拆干净，对象与数组本身留着接着用。清空之后长度是 0，先前借出去的那一块会被查到（10097 号） |
| `tree[key] = value` | — | 10114 | 往对象的一个位置里写，与 `j.set(key, value)` 是同一条 |
| `tree[index] = value` | — | 10031、10114 | 往数组的一个位置里写：那一个位置接手，换下来的那棵子树当场拆掉 |

表里错误码那一列的 10092 说的是同一件事：对着 `null` 取成员。

取下标的两条不抛：树里没有那个键、位置越界都返回 `null`，这跟 `list` 越界就抛是两件事。

`json.parse` 解析失败时不抛出错误，它返回 `null`。

如果接着拿这个 `null` 去调用 `tree.kind()`，才会抛出运行期错误。要先判断有没有解析成功，写 `if (doc == null)` 就可以。

## 逐层取值

`tree["键"]` 与 `tree[位置]` 读回来的是一个借来的 `Json`：当场判类型、当场取下一层都可以。

要把它接给一个名字，就得写 `tree.take(key)` 或 `tree.take_at(index)`：那是从树上摘下来，交给接住它的那个名字。

要把树里的值变成能直接使用的 `string`、`int`、`float`、`boolean`，用表末四条成员。

```nf
let doc:Json = json.parse("{\"name\": \"nef\", \"port\": 8080, \"tags\": [\"语言\", \"编译器\"]}")
console.print(doc.kind(), length(doc))
console.print(doc["name"].to_text())
console.print(doc["port"].to_integer(0))
let tags:Json = doc.take("tags")
console.print(length(tags), tags[0].to_text())
```

```text
object 3
nef
8080
2 语言
```

取 `port` 时给的默认值是 `0`。文本里没有这个键、或者对应位置放的是 `true`，`to_integer(0)` 就取不到值，此时总得返回一个结果，而 `int` 不能是 `null`。

所以 `to_integer`、`to_number`、`to_boolean` 这三条各带一个默认值，写法与[列表·字典·集合](/stdlib/049_containers)里 `d.get(key, fallback)` 的规则相同。

`to_text()` 返回的是 `string`，本来就可以是 `null`，所以它不带默认值。

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

直接输出一个 `Json` 时用的是 JSON 的写法，紧凑、不带多余空格；要把那一段文本接住自己用，写 `tree.to_json()`。

```nf
let dup:Json = json.parse("{\"乙\": 1, \"甲\": 2, \"乙\": 3}")
console.print(dup.keys())
console.print(dup)
console.print(dup.contains("甲"), dup.contains("丙"))
console.print(dup.to_json())
```

```text
["乙", "甲"]
{"乙":3,"甲":2}
true false
{"乙":3,"甲":2}
```

## 这棵树由谁持有

`Json` 是引用类型，遵循[值的所有权](/learn/026_ownership)的规则：交给一个名字、放进一个位置都会发生交出。

`tree["键"]` 与 `tree[位置]` 读了不拿走：那一个位置之后还是原样，同一把键可以读第二遍。

要把那一节的值接给一个名字，写 `tree.take(key)` 或 `tree.take_at(index)`：那是从树上摘下来，交给接住它的那个名字，树上那个位置之后是 `null`。整棵树由指向根的那个名字持有，它离开作用域时，连最里面的文本与键也一起释放。

```nf
let one:Json = json.parse("{ \"a\": 1 }")
console.print(one["a"].to_integer(-1))
console.print(one)
let a:Json = one.take("a")
console.print(a.to_integer(-1))
console.print(one)
```

```text
1
{"a":1}
1
{"a":null}
```

前两行是读：树没有被改动。后两行是摘：`"a"` 位置上的值转由 `a` 持有，树上那个位置空了，所以最后一行输出 `null`。

要把整棵树再看一遍，用 `tree[key]` 与 `tree.contains(key)`，这两条都不改动树。

`Json` 只能出现在名字、实参与返回值里：放进 `list<Json>` 或 `dict<string, Json>`、作为 `class` 的字段，编译期就会拒绝，报 `Json 进不了 list：先把它拆成自己要的值，或者存那段文本`。

要保存一棵树就存 JSON 原文，使用的时候再调用一次 `json.parse`；或者当场把它转换成自己要的值：`int`、`float`、`string`、`boolean`，以及它们的容器。

## 往树里写

`json.new_object()` 与 `json.new_array()` 各起一棵空的树。

放东西用 `set(key, value)`、`add(value)`、`insert_at(index, value)`。

要那一段文本的时候用 `to_json()`。

```nf
let body:Json = json.new_object()
body.set("name", "nef")
body.set("tags", ["语言", "编译器"])
body.set("count", 2)
console.print(body.to_json())
```

```text
{"name":"nef","tags":["语言","编译器"],"count":2}
```

值的这一档收 `int`、`float`、`boolean`、`string`，也收 `list`、`dict`、`struct`、`class`，还收另一棵 `Json`。

交进去的是另一棵树时，这棵树由树上那一个位置持有，原来那个名字当场不能再写。

键与文本各拷一份进来，所以调用方手里的那两个名字照旧能用。

```nf
let body:Json = json.new_object()
body.set("name", "nef")
let tags:Json = json.from_value(["语言", "编译器"])
body.set("tags", tags)
console.print(body.to_json())
```

```text
{"name":"nef","tags":["语言","编译器"]}
```

同一把键写第二次是覆盖那一个位置，位置不挪；换下来的那棵子树当场拆解。

数组的三个动作按位置算：`insert_at(index, value)` 在那个位置前面放进去，`remove_at(index)` 把那个位置摘下来交给接住它的那个名字。

`clear()` 拆干净下面的全部成员，对象与数组自己留着。

```nf
let row:Json = json.new_array()
row.add(1)
row.add(3)
row.insert_at(1, 2)
console.print(row.to_json(), length(row))
let last:Json = row.remove_at(2)
console.print(row.to_json(), last.to_integer(0))
```

```text
[1,2,3] 3
[1,2] 3
```

数组的动作打在对象上、对象的动作打在数组上，当场抛 10114 号，报的是 `这一棵 Json 不是那一种形状`。

位置越界回到容器的 10031 号。

`tree[key] = value` 与 `tree[index] = value` 是 `set` 和数组那一档的另一种写法。

规则的来处与[列表·字典·集合](/stdlib/049_containers)里 `d[key] = value`、`xs[index] = value` 那两行同一条。

## 程序里的值写成 JSON

`json.from_value(value)` 按运行期形状表把一个值变成一棵树：`list` 成数组，`dict`、`struct` 与 `class` 成对象，一层套一层照字段表与元素类型走。

它只读那个值，返回的是一棵新树，所以之后改那只字典、那个列表都不影响这棵树。

```nf
struct Stock {
    name:string
    qty:int
}
let rows:list<Stock> = [Stock{name: "苹果", qty: 3}, Stock{name: "梨", qty: 7}]
console.print(json.from_value(rows).to_json())
```

```text
[{"name":"苹果","qty":3},{"name":"梨","qty":7}]
```

字典的键在 JSON 里是一串字符：`dict<int, string>` 的 `7` 写成 `"7"`，与打印那只字典时给出的是同一串。

写不进 JSON 的类型在编译期就拒绝，并报出坏在哪一层。

`bytes` 是一段连续内存，先写 `b.to_base64()` 存成文本；`hashset` 没有先后，要先自己排成一个 `list`；另一棵树本来就是树，不必再变一次。

`http.post` 与 `http.request` 的正文那一个位置除了 `bytes` 那一块，还收一棵 `Json`，发出去的是渲染好的那一段文本。

```nf
let body:Json = json.new_object()
body.set("name", "nef")
body.set("tags", ["语言", "编译器"])
let r:Response = http.post("http://127.0.0.1:8080/api", body, 3000)
console.print(r.status)
```

## 接受哪些写法

只接受严格的 JSON。注释（`//` 与 `/* */`）、数组或对象末尾多出的逗号、以单引号开头的文本、`NaN` 与 `Infinity` 都不接受。

数值分两种形态：整数形态（`3`、`-12`）与小数形态（`3.0`、`1e9`）。整数形态放不进 `int` 时转为小数形态；写法本身不符合规则的数字（`1.`、`1e`、`01`）算作这段文本解析失败。

文本里的 `\uXXXX` 转义接受，代理区的一对码点（`"\uD83D\uDE00"`）合并成一个码点。配不成对的半个代理码点算作这段文本解析失败；这一点比 JavaScript 严格，JavaScript 会把它替换成一个替换字符再继续解析。

嵌套很深时不另外设置深度上限，与[出错的时候](/reference/041_errors)那一页写的栈用完遵循同一条规则：栈用完时报 10093 号，而不是“最多四十层”这种由本语言自己规定的数值。

## 参见

- [读写文件](/stdlib/050_file)：`file.read` 读取一段文本，`json.parse` 接着解析
- [列表·字典·集合](/stdlib/049_containers)：`tree.keys()` 返回的就是 `list<string>`
- [值的所有权](/learn/026_ownership)：交出与读走这两件事的完整规则
- [错误信息索引](/tools/060_errors)：10092 与 10093 两句报错的原文

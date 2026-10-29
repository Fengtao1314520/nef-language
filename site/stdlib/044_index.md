# 标准库总览

标准库分成十四组，在任何文件里都能直接调用：不需要 `import`，也无法被 `export`。

写成 `组名.动作` 的一共 95 个，另外还有一个全局的 `length`。

三个容器的成员（`add`、`remove`、`sort`、`insert_at` 等），以及文本的十八个点号成员（`split`、`index_of`、`trim_start`、`to_bytes` 等），都不在这 95 个名字里，各页的成员表会逐条列出。

字节块的 `slice` 与 `to_base64`、套接字的 `send`、`recv_into`、`close`、监听的 `accept` 与 `close`、进程的 `wait` 与 `close` 同样不在其中。

| 组 | 作用 | 详细页 |
| --- | --- | --- |
| `console` | 向屏幕输出文本、读取一行输入 | [屏幕输出与输入](/stdlib/045_console) |
| `text` | 文本处理：切分、截取、比较、查找，以及字节与 base64 转换 | [文本处理](/stdlib/046_text) |
| `bytes` | 一段连续内存的字节块：创建、按位置读写、截取片段、base64 与拼接 | [字节块](/stdlib/047_bytes) |
| `number` | 数值转换成文本、按进制写出 | [数值与数学](/stdlib/048_number) |
| `math` | 绝对值、取整、幂、三角与指数、随机数 | [数值与数学](/stdlib/048_number) |
| `file` | 整个文件的读取、写入、追加、判断是否存在与删除，包含遍历子目录 | [读写文件](/stdlib/050_file) |
| `path` | 拼接路径、取文件名与上一级目录、判断是否存在、判断是否为绝对路径 | [路径](/stdlib/051_path) |
| `environment` | 环境变量（读取、设置、删除）、读取当前目录、切换当前目录 | [环境变量与当前目录](/stdlib/052_environment) |
| `process` | 启动一个程序：等它运行结束并取得退出码，不再等待时关闭对应的句柄 | [进程](/stdlib/053_process) |
| `time` | 当前时刻、程序运行时长、本机时区比 UTC 快几分钟 | [时间](/stdlib/054_time) |
| `regular_expression` | 正则匹配、查找、替换与切分 | [正则表达式](/stdlib/055_regular_expression) |
| `json` | 解析一段 JSON：拆成树、取值、说明错误出现在哪一行 | [解析 JSON](/stdlib/056_json) |
| `net` | TCP：建立连接、打开端口等待接入、收发字节、关闭 | [TCP 套接字](/stdlib/057_net) |
| `http` | 获取一个网页：GET、POST，返回状态码、响应头与正文 | [HTTP 客户端](/stdlib/058_http) |

一组一页，每页列出该组的成员表、示例与容易出错的写法。

`number` 与 `math` 同在一页：两组成员的返回类型只有 `float`，放在一起查阅更方便。

标准库之外还有两个用 Nef 自己写成的库，要放进工程的 `libs/` 目录才能用：[浏览器驱动库 web_test](/libs/062_web_test)、[用例运行层 test_run](/libs/063_test_run)。

三个容器的动作不属于这些组，写在[列表·字典·集合](/stdlib/049_containers)。

## 点号还是组函数

判断标准只有一条：第一个参数是否就是被操作的对象。

`s.split(",")` 里的 `s` 就是被切分的对象，用点号写法。`text.to_int(s)` 接收一个文本、返回一个 `int`，完成了从文本到数值的转换，用组函数写法。

`items.add(9)`、`d.remove("k")`、`file.read(p)`、`math.absolute_value(n)` 都符合这一标准。

| 情形 | 写法 |
| --- | --- |
| 操作对象本身 | `items.add(9)`、`d.remove("k")`、`s.split(",")`、`s.trim()`、`s.slice(0, 3)` |
| 转换成另一种类型 | `text.to_int(s)`、`number.to_string(42)` |
| 查询整个容器的长度 | `length(items)` |
| 与外部世界交互 | `file.read(p)`、`path.join(a, b)`、`environment.get("PATH")`、`time.now()`、`console.print(...)` |
| 计算并返回一个值 | `math.absolute_value(n)`、`math.power(2, 10)`、`regular_expression.match(text, pattern)`、`json.parse(text)` |

## 归属分两种情况

**被操作的对象一律按只读借用处理。** `items.add(9)`、`length(items)`、`console.print(txt)` 调用之后，`items`、`txt` 的持有者没有变化。

**传入的值按普通规则处理。** 值类型拷贝，引用类型交出。

所以 `box.add(c)` 之后 `c` 不再是持有者，再写 `c.field` 是编译错误。

这条分界按“操作完成后调用者能不能继续使用该变量”来划分。如果标准库每操作一次就接管一次，往 `list` 里添加一个元素就得把整个容器交出去，添加完之后调用者就不再是持有者，这个容器也就无法继续使用了。

反过来说，放进容器的值确实归容器所有：调用者和容器都还持有这个名字时，两个名字都能释放同一块内存，这就是双重释放。

## 命名

组名都是完整单词：`console`、`text`、`number`、`environment`、`regular_expression`、`json`。

这十四个组名都是保留字，不能用作变量名、字段名、函数名、类型名。连同类型名与 `length`、`exit`、`args` 在内的完整名单写在[语法速查](/reference/034_syntax)。

## 错误如何表现

标准库统一抛出内建错误码，这些错误都能用 `catch` 捕获。

文件与路径：10041 读取失败、10042 写入失败、10044 内容不是合法的 UTF-8、10051 路径不合法。

文本解析：10012 无法解析成数字、10014 无法解析成 base64。正则表达式：10071 表达式书写有误。环境变量：10061 变量不存在。容器与引用：10092 在 `null` 上取成员。

所有错误码与对应的报错原文见[错误信息索引](/tools/060_errors)。

有两个例外：`file.exists` 和 `path.exists` 只返回真或假，不抛出错误。想做到“存在才读取”，就直接写 `if (path.exists(p))`（`file.exists` 与另一个函数是同一种判断，都只返回真假），不需要包 `try`。

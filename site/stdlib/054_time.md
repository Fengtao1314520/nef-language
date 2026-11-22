# 时间

`time` 这一组共有六个函数，负责这几件事：取当前的时刻，取程序从启动到现在运行了多少秒，让程序暂停执行指定的时长，把一个给定的秒数拆成年、月、日等字段，按模式把秒数排成 UTC 文本，以及取本机时区与 UTC 相差的分钟数。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `time.now()` | `int` | 不抛 | 从 1970-01-01 零点（UTC）起算的秒数 |
| `time.elapsed()` | `float` | 不抛 | 本程序从启动到此刻运行了多少秒，包含小数 |
| `time.sleep(milliseconds)` | 没有 | 不抛 | 等待指定的时长后再继续执行。负数按 `0` 计算，`0` 表示让出这一次时间片 |
| `time.calendar(seconds)` | `Calendar` | 不抛 | 把从 1970-01-01 零点（UTC）起算的秒拆成七个整数：`year`、`month`、`day`、`hour`、`minute`、`second`、`weekday`（`0` = 星期一）。按 UTC 拆分，不看本机时区；秒数可以为负 |
| `time.format_utc(seconds, pattern)` | `string` | 不抛 | 把秒数按 UTC 排成文本。模式串是写死的一段文本，认六个记号：`yyyy` 四位年、`MM` 两位月、`dd` 两位日、`HH` 两位时、`mm` 两位分、`ss` 两位秒；其余字节照抄，两位不足补前导 0，负数或超过四位的年份照实写 |
| `time.utc_offset()` | `int` | 不抛 | 本机所在时区比 UTC 快几分钟：东八区是 `480`，西五区是 `-300`。带夏令时的时区返回的是此刻的数值，所以同一个程序在夏天与冬天返回的数值可以不同。读取不到时区信息时返回 `0`，不抛出错误 |

```nf
let start:float = time.elapsed()
let total:int = 0

for (i:int from 0 until 100) {
    total = total + i
}

console.print(total)
console.print(time.elapsed() >= start)
```

```text
4950
true
```

`time.now()` 精确到整数秒；要计算两个时刻之间相差几天，得拿 `time.now()` 的返回值自己相除。

要把秒数拆成年月日，调用 `time.calendar(seconds)`：

```nf
let c:Calendar = time.calendar(1762867925)
console.print(c.year, c.month, c.day, c.hour, c.minute, c.second)
console.print(c.weekday)
```

```text
2025 11 11 13 32 5
1
```

这个函数按 UTC 拆分，与本机时区无关。

要得到本机时区下的年、月、日、时、分，先把 `time.utc_offset()` 返回的分钟数换算成秒，加进 `time.now()` 的结果。

换算之后调用一次拆分：`time.calendar(time.now() + time.utc_offset() * 60)`。

要测量一段代码运行了多久，调用两次 `time.elapsed()`，把两次返回的秒数相减。返回的秒数包含小数，不会像 `time.now()` 相减那样丢掉整秒以下的精度。

要写出 `2025-11-11 13:32:05` 这样的 UTC 日期时间形式，直接用 `time.format_utc`，模式串里把记号写好就行：

```nf
console.print(time.format_utc(1762867925, "yyyy-MM-dd HH:mm:ss"))
```

```text
2025-11-11 13:32:05
```

模式串要写死的一段文本——记号在编译期展开成排版调用，不能用运行时算出来的变量。`time` 这一组只按 UTC 排，要按本机时区输出，先把 `time.utc_offset()` 的分钟数折成秒加进去再传给 `format_utc`。

`time.utc_offset()` 只给出一个分钟数：不返回时区名字，不把日期换算成某一时区的年、月、日、时、分，也不读取系统里的时区数据库。

要按日历计算某一时区的时间，前提是先有一份时区表；`time` 这一组没有提供时区表。

## 参见

- [文本处理](/stdlib/046_text)：`pad_start` 与 `+`，把七个字段拼成日期写法
- [读写文件](/stdlib/050_file)：`file.modified` 与 `time.now()` 用同一个基准，两个数值可以直接比较
- [数值与数学](/stdlib/048_number)：`number.to_string`，把秒数写成文本

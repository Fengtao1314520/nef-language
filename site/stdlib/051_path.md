# 路径

这一组叫 `path`，六个函数处理的是路径的写法：拼接、取最后一段、取扩展名、取上一级目录、判断是否为绝对路径、判断文件或目录是否存在。

只有 `path.exists` 访问系统。

另外五个函数只看这条路径怎么写，不检查它指向的内容是否在磁盘上。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `path.join(dir, name)` | `string` | 不抛 | 拼接一条路径，分隔符按本机习惯补充；它只处理字符串，不访问系统 |
| `path.file_name(path)` | `string` | 不抛 | 取最后一段，包含扩展名；整条都是分隔符时返回 `""` |
| `path.extension(path)` | `string` | 不抛 | 取最后一个点之后的部分，没有点时返回 `""` |
| `path.directory_name(path)` | `string` | 不抛 | 取上一级目录，连同分隔符：`C:/work/demo.nf` 得到 `C:/work/`，反斜杠写法 `a\b\c.nf` 得到 `a\b\`。整条路径里没有分隔符时返回空文本。末尾本来就带分隔符时（`C:/work/`）返回 `C:/`：返回的是这个目录所在的上一级 |
| `path.is_absolute(path)` | `boolean` | 不抛 | 判断是否为绝对路径。第一个字符是斜杠（两种斜杠都算）时一定是；前两个字符是一个字母加一个冒号时也是，`F:/a`、`F:`、`F:a` 都算：`F:a` 这种写法在本机就是“在这个盘上从当前目录起”。只查看写法，不查看盘是否存在：盘是否存在要问 `path.exists` 与 `file.exists` |
| `path.exists(path)` | `boolean` | 10051 | 判断文件或目录是否存在，只询问不抛出错误；空串这类无法提交给系统的路径抛 51 |

```nf
let full:string = path.join("docs", "notes.md")

console.print(path.file_name(full), path.extension(full))   // notes.md md
```

`path.join` 一次只接受两个参数，三个以上的路径段要连着调用多次。四个参数的写法在编译期就被拒绝，报的是 `join 收 2 个参数，这里给了 4 个`。

```nf
let a:string = path.join("docs", "notes.md")
let b:string = path.join(a, "报告.txt")
let full:string = path.join("C:/work", "demo.nf")

console.print(a, path.file_name(b), path.extension(b))
console.print(path.directory_name(b), path.directory_name(full))
console.print(path.is_absolute(full), path.is_absolute(a))
```

```text
docs\notes.md 报告.txt txt
docs\notes.md\ C:/work\
true false
```

输出的分隔符是反斜杠：`path.join` 按本机习惯补分隔符，Windows 上返回的就是反斜杠写法。`path.is_absolute` 只查看写法，`C:/work` 这种带盘符与冒号的算绝对路径，`docs\notes.md` 没有盘符也没有开头的斜杠，算相对路径。

## 分隔符怎么补

`path.join` 遵循“缺一个才补一个”，不是每次都加一个。

`path.join("docs\\", "notes.md")` 与 `path.join("docs", "\\notes.md")` 都返回 `docs\notes.md`，不会出现两个连续的分隔符。

`path.join` 遇到空文本时，直接返回另一端文本的拷贝。

`path.file_name` 会先跳过末尾的分隔符，所以 `path.file_name("docs\\")` 返回 `docs`，不是空串。整条路径都是分隔符时才返回空串。

`path.extension` 只看最后一段里的点，而且这个点要在最后一段的中间：`n.tar.gz` 返回 `gz`。

`path.extension(".nf")` 返回 `""`：以点开头的名字里，第一个点属于名字本身，不算扩展名。

## 两处容易出错

`path.join` 按本机习惯补分隔符，所以返回的文本里可能是 `\`。带反斜杠的路径文本不要写进 `import`：`import` 那里的路径只接受 `/`，而 `\` 在字符串里是转义记号。

`path.exists` 只询问不抛出错误：路径能够提交给系统、而系统回答没有这个文件或目录，就返回 `false`。

无法提交给系统的路径（空串、本机放不下的长度）算“写得不对”，不算“不存在”，抛 10051 号；`file.read` 遇到同样的路径问题时抛的是同一个错误码。

路径本身写得不对（比如写进了一个本机不允许的字符）抛 10051 号。

## 参见

- [读写文件](/stdlib/050_file)：`file.exists` 与 `path.exists` 的询问内容和返回值完全相同，`file.read` 对无效路径抛同一个 10051 号
- [环境变量与当前目录](/stdlib/052_environment)：`environment.current_directory()` 返回的也是本机习惯的写法
- [错误信息索引](/tools/060_errors)：10051 号那句报错的原文

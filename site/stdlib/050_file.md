# 读写文件

这一组叫 `file`，负责与磁盘打交道：读取整个文件与写出整个文件、询问文件的属性、列出目录里的名字、按字节读写、创建目录、重命名、复制、遍历目录树。

`file` 组的二十二个函数是这一页的全部内容。

这些函数的第一个参数是一个名字或者一条路径，不是被操作的对象，所以全部用组函数写法。

路径的写法、环境变量与当前目录、启动另一个程序这三组分别写在[路径](/stdlib/051_path)、[环境变量与当前目录](/stdlib/052_environment)与[进程](/stdlib/053_process)。

`file.read` 按 UTF-8 解析文本，并且丢弃开头的 BOM；写出时一律不带 BOM，写出的字节就是所写文本的 UTF-8，一个不多。要读取的是字节本身时，用“字节读写”一节的两个函数。

## file

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `file.read(path)` | `string` | 10041、10044、10051 | 读取整个文件；读取失败、路径无法提交给系统分别是 10041（`读不到这个文件`）、10051（`这条路径本机用不了`），内容不是合法 UTF-8 时是 10044 |
| `file.write(path, text)` | 没有 | 10042、10051 | 写出整个文件，原有的内容被覆盖；写入失败抛 10042 号 |
| `file.append(path, text)` | 没有 | 10042、10051 | 在尾部追加，文件不存在时直接创建；失败同样是 10042 号 |
| `file.exists(path)` | `boolean` | 10051 | 只询问不抛出错误；路径本身无法提交给系统（空串、本机放不下的长度）时仍然抛 51，报的是“写得不对”，不是“不存在” |
| `file.remove(path)` | 没有 | 10041、10051 | 删除文件；删除失败抛 10041 号 |
| `file.size(path)` | `int` | 10043、10045、10051 | 有多少个字节。路径不存在时是 10045，传入目录时是 10043 |
| `file.modified(path)` | `int` | 10043、10045、10051 | 最后一次写入的时间，按从 1970-01-01 起算的秒计，与 `time.now()` 用同一个基准 |
| `file.readonly(path)` | `boolean` | 10043、10045、10051 | 系统里的那个只读标记，询问的就是标记本身 |
| `file.list(dir)` | `list<string>` | 10043、10051 | 列出该目录里的名字，不含 `.` 与 `..`，顺序不保证 |
| `file.list_paths(dir)` | `list<string>` | 10043、10051 | 同样是列出目录里的名字，只是每个名字前面带上目录，取到之后可以直接打开 |
| `file.read_from(path, at, count)` | `string` | 10041、10051 | 从第 `at` 个字节开始读取，最多读取 `count` 个字节；位置越过末尾时返回空文本 |
| `file.write_at(path, at, text)` | 没有 | 10042、10051 | 从第 `at` 个字节开始覆盖写入，后面的内容不变；文件不存在时不创建 |
| `file.read_bytes(path)` | `bytes` | 10041、10051 | 按字节读取整个文件，不校验编码、不丢弃 BOM；传入目录时也是 10041 |
| `file.write_bytes(path, data)` | 没有 | 10042、10051 | 把整块 `bytes` 按字节写出，原有内容被覆盖、文件不存在时新建 |
| `file.create_directory(path)` | `boolean` | 10046、10051 | 创建指定的目录。真=这一次新建出来了，假=已经存在（同名的路径即使指向一个文件，也算存在）。上一级目录不存在时抛 10046 号，不会连续向上级创建 |
| `file.create_directories(path)` | `boolean` | 10051 | 把缺失的各级目录一次创建完整。真=这条路径上的目录现在齐全了（本来就齐全的也算真），假=创建不出来（中间有一级同名的是文件、或者不允许写入）。不抛出错误 |
| `file.is_directory(path)` | `boolean` | 10051 | 判断是否为目录；路径根本不存在时也返回假，只询问不抛出错误 |
| `file.rename(source, target)` | 没有 | 10047、10051 | 重命名，或者在同一个卷里移动位置；新名字已被占用、旧名字不存在都是 10047 号 |
| `file.remove_directory(path)` | 没有 | 10041、10051 | 删除一个空目录；里面有内容就抛 10041 号，报的是 `删不掉这个目录` |
| `file.copy(source, target)` | 没有 | 10041、10042、10051 | 整个复制一遍，目标已存在就被覆盖。源文件无法打开是 10041，目标写入失败（包括目标本身是一个目录）是 10042 |
| `file.walk_paths(root)` | `list<string>` | 10043、10051 | 列出这个目录之下的每一项，目录自身也算一条，每一条都是取到之后可以直接打开的路径。`.` 与 `..` 不在内，根目录自身也不在（它已经在参数里了），先后顺序按系统返回的次序。向下遍历到第 64 层就停止：链接点与挂载点可以构造出一条遍历不完的循环，宁可限制深度，也不让程序一直遍历下去 |
| `file.remove_tree(root)` | 没有 | 10041、10043、10051 | 把里面的内容一并删除。先删除文件，再逆着发现顺序删除目录，处理到上级目录时它里面已经空了，最后删除根目录自身。里面的内容删除失败就抛出错误：文件与目录都抛 10041 号，与 `file.remove`、`file.remove_directory` 的报错相同；根不存在、或者根不是一个目录时抛 10043 号 |

一共二十二个函数。

前五个是整个文件的读取、写出、追加、询问是否存在与删除；中间七个询问属性、列出目录中的名字、按位置读写文件中间的一段；接着两个按字节处理，专门用于不是文本的内容。

后面六个操作目录与复制：创建一个目录、一次性创建缺失的各级目录、判断是否为目录、重命名、删除空目录、完整复制一次；最后两个遍历子目录：一次性返回整棵树的路径、把整棵树删除。

写出的字节就是所写文本的 UTF-8，一个字节不多，一个字节不少。

写入之前不创建目录：上一级目录不存在，`file.write` 就抛 10042 号。

要先把缺失的各级目录创建出来，调用 `file.create_directories`，它一次创建到底；`file.create_directory` 只创建最后一级。

```nf
let dir:string = "报告/一月"
console.print(file.create_directories(dir))

let deep:string = dir + "/汇总.txt"
file.write(deep, "一月汇总")
console.print(file.is_directory(dir))

let copy:string = dir + "/副本.txt"
file.copy(deep, copy)
console.print(file.read(copy), file.exists(deep))
```

```text
true
true
一月汇总 true
```

`create_directories` 返回真表示“这条路径上的目录齐全了”，本来就齐全的也算真。

`create_directory` 返回真表示“这一次新建出了一个目录”，已经存在时算假。这两个函数确认的不是同一件事，所以两个返回值的含义要分开看。

`file.exists` 与 `path.exists` 的询问内容与返回值完全相同：能提交给系统的路径只返回真或假；空路径无法提交给系统，才抛 10051 号。读取之前想确认一下，用哪一个都可以。

```nf
let name:string = "笔记.txt"

if (file.exists(name)) {
    let body:string = file.read(name)
    console.print("已经有过，长度:", length(body))
} else {
    file.write(name, "第一行\n")
    console.print("写了")
}

file.append(name, "第二行\n")
```

`file.write` 是覆盖而不是追加，`file.append` 才在文件末尾接着写。

追加不需要自己先定位到文件末尾：打开时申请的就是“只能向末尾写入”的访问权限，写入位置由系统决定。多个写入者同时追加，内容也不会插到别人已经写好的数据中间。

`file.remove` 只删除文件。传入一个目录时，操作系统的删除调用会失败，于是抛 10041 号；它也不提供“删除一个空目录”的用法。

## 属性、目录、分块

询问文件大小、修改时间与只读标记的三个函数都只针对一个文件。传入一个目录时抛 10043 号；路径不存在时抛 10045 号。

```nf
let p:string = "分块.txt"

file.write(p, "一二三四五六七八九十")
console.print(file.size(p))                       // 30
console.print(file.readonly(p))                   // false
console.print(file.modified(p) <= time.now())     // true
```

`file.modified` 与 `time.now()` 用的是同一个基准上的秒数，所以两个数值可以直接比较。要按天计算就自己相除：一天 86400 秒。

列出目录的两个函数是同一次查找的两种结果：`file.list` 返回名字，`file.list_paths` 返回带上目录的完整路径。只查看这个目录本身，不进入子目录；`.` 与 `..` 不在返回列表里，顺序也不保证。

```nf
for (n:string in file.list(environment.current_directory())) {
    console.print(n)
}
```

分块读写的位置按**字节**计数，不按码点：`file.read_from(p, 0, 9)` 读取的是前九个字节。末尾凑不满一个码点的几个字节不计入返回值，所以返回的永远是完整的码点序列，长度可能比请求的少一两个字节。

```nf
let p:string = "分块.txt"

file.write(p, "一二三四五六七八九十")
let head:string = file.read_from(p, 0, 9)
console.print(head, length(head))                 // 一二三 3
file.write_at(p, 0, "甲")
console.print(file.read_from(p, 0, 6))            // 甲二
console.print(file.size(p))                       // 30
```

`file.write_at` 只覆盖中间的一段：从第 0 个字节写入三个字节，原来第四个字节往后的内容还在，文件也不会被截短。要整个替换用 `file.write`，要在末尾追加用 `file.append`。

分块读取是为了逐段处理大文件：`file.read` 会把整个文件读入内存。

## 字节读写

`file.read` 返回的是文本，所以它要先校验一遍 UTF-8：以 `FF FE` 开头的字节会让它抛 10044 号。

要读取的是图片、构建产物这类不是文本的内容，就用 `file.read_bytes`：每个位置一个字节，不校验编码，也不丢弃开头的 BOM。

```nf
let p:string = "字节.bin"

let buf:bytes = bytes.new(4)

for (i:int in 0 until 4) {
    buf[i] = i as byte
}
file.write_bytes(p, buf)
let back:bytes = file.read_bytes(p)
console.print(length(back), back[0], back[3])      // 4 0 3
```

这一块字节属于 `bytes` 类型，用 `bytes.new(size)` 创建，成员表与越界规则的说明写在[字节块](/stdlib/047_bytes)一页。向某个位置写入 `i as byte` 要显式转换：`i` 是一个整数，类型之间不自动转换。

`file.write_bytes` 与 `file.write` 的处理方式相同：文件已存在就清空重写，不存在就新建，未能写满时抛 10042 号。

要处理的字节较多时，先算一下开销：每个位置一个字节，读取多大的文件就占用多大的内存，不像容器那样每个位置占 8 字节。要逐段处理大文件，`file.read_from` 更省内存。

## 编码与换行

| 事项 | 规则 |
| --- | --- |
| 读取 | 按 UTF-8 解析，开头的 BOM（`U+FEFF`）丢弃 |
| 不是合法 UTF-8 | 抛 10044 号 |
| 写出 | 一律写成不带 BOM 的 UTF-8 |
| 行尾 | 文件里的 `\r\n` 原样保留，不转换成 `\n`。按行处理时要自己调用 `split("\n")`，再去掉行尾的回车符 |
| 按字节的两个函数 | `file.read_bytes`、`file.write_bytes` 不参与这套规则：不校验 UTF-8、不丢弃 BOM、不修改行尾，字节原样读入、原样写出 |

```nf
method lines_of(body:string):list<string> {
    let out:list<string> = []

    for (one:string in body.split("\n")) {
        out.add(one.trim())
    }
    return out
}

console.print(length(lines_of("甲\u000D\n乙")))   // 2
```

`trim()` 会把行尾那个回车符当作空白去掉，所以大多数情况下不需要单独处理它。文本以换行符结尾时，`split("\n")` 会在最后多返回一个空串。

## 未提供的写法

`file.list` 与 `file.list_paths` 只查看指定的目录本身。

目录内各级的路径列表由 `file.walk_paths` 一次性返回，删除整棵树用 `file.remove_tree`：这两个函数遍历到第 64 层就停止，再深的内容不返回，也不报错。

没有权限位：这门语言不处理文件权限，也没有八进制模式的规定。`file.readonly` 询问的是 Windows 的只读标记。

## 练习

把[应用与入口](/learn/022_start)那课的 `hello.nf` 改成：读取命令行给出的路径，输出它的文件名、扩展名和文件里的行数；路径不存在时输出一句提示，不要把错误抛出去。

## 参见

- [路径](/stdlib/051_path)：`path` 组六个函数处理路径的写法
- [环境变量与当前目录](/stdlib/052_environment)：`environment` 组五个函数读取环境变量与当前目录
- [进程](/stdlib/053_process)：`process` 组启动另一个程序、等待它的退出码

# 环境变量与当前目录

这一组叫 `environment`，五个函数管理当前程序自己的环境变量与当前目录。

三个函数读取、设置、删除环境变量，另外两个函数读取与切换当前目录。

`environment.set` 与 `environment.unset` 的改动只对本程序有效：启动这个程序的命令行窗口里的环境变量不受影响。

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `environment.get(name)` | `string` | 10061 | 读取一个环境变量。不存在这个变量时抛 10061 号（`没有这个环境变量`），不返回 `""`，也不返回 `null` |
| `environment.set(name, value)` | 没有 | 不抛 | 设置一个环境变量，只对本程序有效 |
| `environment.unset(name)` | 没有 | 10060 | 删除一个环境变量，也只对本程序有效。变量本来就不存在时不做任何事、不抛出错误；只有名字非法、系统调用不接受时（比如带 `=`）才抛 10060 号 |
| `environment.current_directory()` | `string` | 不抛 | 返回当前在哪个目录下运行。返回的是本机习惯的写法（Windows 上是反斜杠） |
| `environment.set_current_directory(path)` | `boolean` | 10051 | 切换到指定目录。真=切换成功，假=没有切换成功（路径不存在，或者这个路径不是目录），不抛出错误：这个函数常用于试探性地切换，要判断结果就查看返回的 `boolean`。`C:/Temp/a` 与 `C:\\Temp\\a` 是同一个目录，两种分隔符都接受 |

## 没有设置过与设置成空值是两件事

变量不存在时抛出错误，为的是把“没有设置过”与“设置成空值”分开：设置成空值再读取回来是空文本，不是 10061 号。

```nf
environment.set("DEMO", "")
console.print(environment.get("DEMO") + "|", length(environment.get("DEMO")))   // | 0
```

要删除这个变量就调用 `environment.unset(name)`。删除之后再读取，抛 10061 号；删除一个本来就不存在的变量，不做任何事。

```nf
environment.set("DEMO", "1")
environment.unset("DEMO")
try {
    console.print(environment.get("DEMO"))
} catch (code, msg) {
    console.print(code)      // 10061
}
environment.unset("DEMO")
console.print("还在跑")          // 还在跑
```

要准备一个备用值就自己封装一个函数。

```nf
method or_default(key:string, fallback:string):string {
    try {
        return environment.get(key)
    } catch (code, msg) {
        return fallback
    }
}

console.print(or_default("一定没有的变量名", "用默认值"))   // 用默认值
```

## 当前目录

`environment.current_directory()` 返回程序此刻在哪个目录下运行，返回的文本按本机习惯书写：Windows 上是反斜杠。

`environment.set_current_directory(path)` 用返回值报告结果：真表示切换成功，假表示没有切换成功（路径不存在，或者这个路径不是目录）。

`set_current_directory` 不抛出错误，切换成功与切换失败只能靠这个 `boolean` 区分：这个函数常用于先试一次切换，再根据返回值决定下一步做什么。`C:/Temp/a` 与 `C:\\Temp\\a` 指向同一个目录，两种分隔符系统调用都接受。

## 两处容易出错

`environment.get` 在变量不存在时抛 10061 号（`没有这个环境变量`），既不返回 `""`，也不返回 `null`。

要一个备用值，就自己用 `try` 与 `catch` 包住这次读取，上面示例里的 `or_default` 用的就是这种写法。

`environment.unset` 删除一个本来就不存在的变量时不做任何事，也不抛出错误；只有名字非法、系统调用无法接受时（比如名字里带 `=`）才抛 10060 号。

## 参见

- [路径](/stdlib/051_path)：`path.join` 拼出的分隔符与 `environment.current_directory()` 的返回值都是本机写法
- [读写文件](/stdlib/050_file)：`file.list(environment.current_directory())` 列出当前目录里的名字
- [进程](/stdlib/053_process)：`process.start` 启动的子进程跟随调用时的环境变量块
- [错误信息索引](/tools/060_errors)：10060 与 10061 两句报错的原文

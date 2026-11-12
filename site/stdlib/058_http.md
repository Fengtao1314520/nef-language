# HTTP 客户端

`http` 是 HTTP 客户端。`http.request` 发送一个请求，`http.get` 与 `http.post` 是它的两个封装。三次调用都等到整个响应接收完成，再返回一个 `Response`。

这一组直接调用 Windows 自带的系统组件 winhttp，不依赖任何额外组件。

产物拷贝到另一台 Windows 机器上仍然可以直接运行，与其他功能没有区别。

要自己定义一套协议，用 `net` 组（[TCP 套接字](/stdlib/057_net)）。

## 成员

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `http.request(method, url, headers, body, timeout_ms, ignore_cert_error = false, max_body_bytes = 0)` | `Response` | 10100～10109、10113 | 发送一个请求，等待整个响应接收完成，返回状态码、头与正文 |
| `http.get(url, timeout_ms, headers = [])` | `Response` | 10100～10105、10109、10113 | 发送一个 GET 请求，等待整个响应接收完成；`headers` 省掉就是不追加任何头 |
| `http.post(url, body, timeout_ms, headers = [])` | `Response` | 10100～10105、10109、10113 | 发送一个 POST 请求，传入的字节块就是发送出去的正文；`headers` 省掉就是不追加任何头 |

`http.get` 与 `http.post` 等于把方法写成 `GET` 或 `POST`、把后两个开关留默认的 `http.request`，写出来的程序与展开之后的产物没有区别。要发认证头，把那一串 `"名字: 值"` 写进最后的 `headers` 就行。

```nf
let page:Response = http.get("http://example.com/", 5000)
if (page.status == 200) {
    console.print(length(page.body), text.from_bytes(page.body.slice(0, 40)))
}
```

## 方法

`method` 认七个词：`GET`、`POST`、`PUT`、`DELETE`、`PATCH`、`HEAD`、`OPTIONS`。大小写都收，发出去一律写成大写。这七个之外的词当场抛 10108 号，消息里带着写进去的那一个。

```nf
let a:Response = http.request("get", "http://example.com/", [], null, 5000)
let b:Response = http.request("DELETE", "http://example.com/item/7", [], null, 5000)
console.print(a.status, b.status)
```

要发的是 `HEAD`，拿到的是状态码与响应头，正文为空——先看对方打算给多大、再决定收不收，靠的就是这一条。

## 请求头

`headers` 是一个 `list<string>`，一行一个头，写法与 `Response.headers` 返回的相同：`"名字: 值"`。传空列表或者 `null` 就是不追加任何头。

库按第一个冒号把一行拆成名字与值，去掉名字两端的空白与值左端的空白，再发出去。三种写法算这一行不合法，都抛 10109 号，消息里说是第几行：整行没有冒号、冒号前面什么都没有、行里出现回车或换行。最后那一种挡的是拼出第二行请求头这种写法，所以只要值可能来自外部，这一道就值得留着。

一行最长 4096 个字符，超过同样报 10109。名字与值都只写 ASCII：实测 Windows 10 上，值里的非 ASCII 字符会被系统按本机代码页转换，对方收到的字节与写进去的 UTF-8 并不相同。

```nf
let hs:list<string> = [
    "Authorization: Bearer abc123",
    "Accept: application/json",
    "X-Test-Round: 7"
]
let body:bytes = "{\"name\":\"dev-7\"}".to_bytes()
let r:Response = http.request("POST", "http://127.0.0.1:8080/api", hs, body, 5000)
console.print(r.status, length(r.headers))
```

`Content-Length` 与 `Host` 不用自己写，系统会补。

## 应答的大小

`max_body_bytes` 是这次请求愿意收下的正文字节数。超过就抛 10106 号，消息里带上限与实际收到的字节数，整个请求作废，拿不到 `Response`——这一组不交半个正文出来，因为半个 JSON 与对方发来的坏数据在屏幕上长得一样。

写 `0` 或者省掉就是不限制，对方给多少收多少。写负数抛 10107 号。

```nf
let r:Response = http.request("GET", "http://127.0.0.1:8080/big", [], null, 60000, false, 1048576)
console.print(length(r.body))
```

压缩的应答会解开之后再交给 `body`，`headers` 里那一行 `Content-Encoding` 照原样留着。gzip 由系统的 winhttp 解开，deflate 由库自己解开，zlib 包裹的与不带包裹的 raw deflate 两种都认。

deflate 不能交给系统：系统只认不带包裹的那一种，而网上多数服务器给的是 zlib 包裹的。

系统还会自己写上 `Accept-Encoding: gzip`，所以这一项不用自己管。gzip 的自动解压要 Windows 8.1 以上，旧系统上库把这一项跳过；deflate 由库自己解，不受这个版本限制。

`max_body_bytes` 按解开之后的大小算：压缩时没超、解开后超了，同样抛 10106。deflate 正文解不开（`Content-Encoding` 说是 deflate，内容却不是合法的 deflate 流）抛 10113。

## 证书

https 的加密、证书与重定向都由系统的 winhttp 处理。证书校验不通过就抛 10101 号，消息里带上系统给出的原始错误码。

`ignore_cert_error` 写 `true` 时，五类证书问题都不再拦：签发者不认识、证书名字与访问的地址不符、证书过期或还没生效、用途不对、签名算法太弱。默认是 `false`，也就是照旧校验。

测试设备的自签证书可以写这一个开关。它是逐次请求的开关：这一条写了 `true`，下一条不写仍要校验。它只免掉证书这一项，超时、应答大小那两条照旧管着。

```nf
let r:Response = http.request("GET", "https://192.168.1.10/api/status", [], null, 5000, true)
console.print(r.status)
```

## 重定向与凭据

请求头会跟着重定向一起送到下一跳，而下一跳可能是另一台主机。带着令牌出去的时候，这就是把令牌交给了一台没有约定过的机器。

所以规则定成一条：`headers` 里出现了 `Authorization`、`Cookie`、`Proxy-Authorization` 三者之一，这一次请求就不自动跟随重定向，`30x` 原样返回，`Response.headers` 里能看到 `Location`。

要不要跟过去、带不带着那个头跟过去，由写程序的人决定。没有这三个头的请求照旧一路跟到底。

```nf
let hs:list<string> = ["Authorization: Bearer abc123"]
let first:Response = http.request("GET", "http://127.0.0.1:8080/start", hs, null, 5000)
console.print(first.status)
for (line:string in first.headers) {
    if (line.starts_with("Location:")) {
        console.print(line)
    }
}
```

## Cookie

库不保存 Cookie。同一台设备上连着发几个请求时，从上一个应答的头里挑出 `Set-Cookie`，拼成一条 `Cookie` 头带过去。

```nf
let jar:list<string> = []

method collect(r:Response) {
    for (line:string in r.headers) {
        if (line.starts_with("Set-Cookie:")) {
            let cut:int = line.index_of(";")
            let last:int = length(line)
            if (cut < 0) {
                jar.add(line.slice(11, last).trim())
            } else {
                jar.add(line.slice(11, cut).trim())
            }
        }
    }
}
```

`method collect` 那段也可以写进一个包里，让每个工程都免掉这一遍。

## 超时

一次调用就在调用方那一条线程里停在原地等待，所以三个成员都带一个毫秒数。

解析网址、建立连接、发送请求、接收响应四个阶段共用同一个超时毫秒数。

域名解析的耗时也计入这个超时；`net.connect` 的超时不覆盖域名解析阶段，`http` 这一组没有这项例外（见 [TCP 套接字](/stdlib/057_net)）。

要一边等应答、一边按固定间隔发心跳，就起一条线程专门发心跳（[线程](/stdlib/067_thread)）。这一组的临时数据按线程各一份，两条线程各发各的请求，互不干扰。

```nf
method beat(url:string):int {
    let n:int = 0
    while (n < 20) {
        let r:Response = http.request("POST", url, [], null, 3000)
        console.print(r.status)
        time.sleep(5000)
        n = n + 1
    }
    return n
}

let t:Thread<int> = thread.start(beat, "http://127.0.0.1:8080/heartbeat")
let data:Response = http.request("GET", "http://127.0.0.1:8080/export", [], null, 60000)
let sent:int = t.wait()
console.print(data.status, sent)
```

心跳要发多少次由那一个函数自己数：这一版没有能让另一条线程递出“停下来”这个动作的写法。

## 网址

网址只接受 `http://` 与 `https://` 两种开头，其余一概拒绝。带查询串时按平时的写法即可：

```nf
let q:Response = http.get("http://example.com/search?word=甲", 5000)
console.print(q.status)
```

## Response

`Response` 是一个 `struct`，包含三个字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `status` | `int` | 200、404 这样的数值。四百多与五百多的状态码都不算出错，要自己判断 |
| `headers` | `list<string>` | 每一行形如 `"名字: 值"`。要取出值，需要自己按冒号拆分 |
| `body` | `bytes` | 完整的正文，一段字节数据。是不是文本、是什么编码，由接收的一方决定 |

`body` 字段的类型是 `bytes`，它的成员与错误码写在[字节块](/stdlib/047_bytes)一页。

## 出错的时候

网络这一类失败共用十一个错误码（[错误信息索引](/tools/060_errors)），`net` 组与 `http` 组用的是同一张表。

系统那边的错误码有一千多个，逐个分配会把文档写得过长；一个都不分配又只能让读者猜测。折中的办法是一类失败用一个错误码，消息里带上系统的原始错误码：

```text
出错了（10101）：连不上这个地址（系统的号码 10061）
出错了（10103）：解析不到 http://nef-没有这个域名.invalid/ 这个地址（系统的号码 12007）
```

`10100` 到 `10105` 是 `net` 与 `http` 共用的六条，`10106` 到 `10109` 与 `10113` 是 `http.request` 自己给的五条：

| 错误码 | 说的是 |
| --- | --- |
| 10100 | 这一类的默认码：系统的原始错误码没有折进对照表时用它，消息里照样带上系统的原始号码 |
| 10101 | 连不上这个地址：连接被拒绝、没有路由、主机或网络不可达、TLS 与证书校验失败都归入这个错误码 |
| 10102 | 等待超时：四个阶段中任何一个阶段的耗时超过传入的超时毫秒数 |
| 10103 | 无法解析这个名字：网址里的域名没有解析成功，消息里带上整个网址 |
| 10104 | 地址或端口不合法：网址的端口越界，或者解析不出网址的主机 |
| 10105 | 这条连接已不可用：对方关闭了连接，或者发送、接收时连接已断 |
| 10106 | 应答正文超过 `max_body_bytes`，消息里带上限与实际收到的字节数 |
| 10107 | `max_body_bytes` 是负数 |
| 10108 | `method` 不在那七个词里，消息里带上写进去的那一个 |
| 10109 | `headers` 里有一行不是 `名字: 值`，消息里带上是第几行 |
| 10113 | 应答的压缩正文解不开：`Content-Encoding` 说是 `deflate`，正文却不是合法的 deflate 流 |

后五个号是这一组自己给的，不来自系统。

十一个错误码都能用 `catch` 捕获（[错误处理](/learn/031_errors)）：

```nf
try {
    let page:Response = http.request("GET", "http://127.0.0.1:9/", [], null, 1000)
    console.print(page.status)
} catch (code, msg, at, trace) {
    console.print("没取到:", code)
}
```

## 未提供的能力

不支持分块接收：完整的响应体在 `Response.body` 里，`max_body_bytes` 只挡大小，不能先看头再决定收不收正文。要这一档，先用 `HEAD` 问一次。

不读对方指定的根证书。要信任一台自签证书的设备，要么把那张证书装进系统的受信任根证书存储（`certutil -addstore`，需要管理员权限，一次装好之后这一组不用改就认），要么在下一次请求里写 `ignore_cert_error`。

没有异步与定时动作：`http.request` 就在调用它的那一条线程里等。并行靠线程（[线程](/stdlib/067_thread)）。

## 参见

- [TCP 套接字](/stdlib/057_net)：对方使用的不是 HTTP 协议时用它自己定义协议
- [字节块](/stdlib/047_bytes)：`http.request` 的正文与 `Response.body` 的类型
- [文本处理](/stdlib/046_text)：`text.from_bytes` 把响应体转换成文本
- [解析 JSON](/stdlib/056_json)：把应答正文拆成值
- [线程](/stdlib/067_thread)：一边等应答、一边做别的
- [错误信息索引](/tools/060_errors)：10100 段错误码与对应的报错原文

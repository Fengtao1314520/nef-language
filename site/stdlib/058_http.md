# HTTP 客户端

`http` 是 HTTP 客户端，用来获取一个网页。`http.get` 与 `http.post` 各发送一个请求，等到整个响应接收完成再返回一个 `Response`。

这一组直接调用 Windows 自带的系统组件 winhttp，不依赖任何额外组件。

产物拷贝到另一台 Windows 机器上仍然可以直接运行，与其他功能没有区别。

要自己定义一套协议，用 `net` 组（[TCP 套接字](/stdlib/057_net)）。

## 超时

这门语言没有线程，一次调用就会停在原地等待。

`http.get` 与 `http.post` 都接收一个超时毫秒数：等待超过这个数值就报错，而不是永远停在那里。

解析网址、建立连接、发送请求、接收响应四个阶段共用同一个超时毫秒数。

域名解析的耗时也计入这个超时；`net.connect` 的超时不覆盖域名解析阶段，`http` 这一组没有这项例外（见 [TCP 套接字](/stdlib/057_net)）。

## 请求与响应

| 写法 | 返回 | 抛出的错误码 | 说明 |
| --- | --- | --- | --- |
| `http.get(url, timeout_ms)` | `Response` | 10101、10102、10103、10104、10105 | 发送一个 GET 请求，等待整个响应接收完成 |
| `http.post(url, body, timeout_ms)` | `Response` | 同上 | 发送一个 POST 请求，传入的字节块就是发送出去的正文 |

`Response` 是一个 `struct`，包含三个字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `status` | `int` | 200、404 这样的数值。四百多与五百多的状态码都不算出错，要自己判断 |
| `headers` | `list<string>` | 每一行形如 `"名字: 值"`。要取出值，需要自己按冒号拆分 |
| `body` | `bytes` | 完整的正文，一段字节数据。是不是文本、是什么编码，由接收的一方决定 |

`body` 字段的类型是 `bytes`，它的成员与错误码写在[字节块](/stdlib/047_bytes)一页。

```nf
let page:Response = http.get("http://example.com/", 5000)
if (page.status == 200) {
    console.print(length(page.body), text.from_bytes(page.body.slice(0, 40)))
}
```

网址只接受 `http://` 与 `https://` 两种开头，其余一概拒绝。带查询串时按平时的写法即可：

```nf
let q:Response = http.get("http://example.com/search?word=甲", 5000)
console.print(q.status)
```

`https` 的加密、证书、重定向都由系统的 winhttp 处理：重定向一路跟随到底，证书校验不通过就报 10101，消息里带上系统给出的原始错误码。

## 出错的时候

网络这一类失败共用六个错误码（[错误信息索引](/tools/060_errors)），`net` 组与 `http` 组用的是同一张表。

系统那边的错误码有一千多个，逐个分配会把文档写得过长；一个都不分配又只能让读者猜测。折中的办法是一类失败用一个错误码，消息里带上系统的原始错误码：

```text
出错了（10101）：连不上这个地址（系统的号码 10061）
出错了（10103）：解析不到 http://nef-没有这个域名.invalid/ 这个地址（系统的号码 12007）
```

`http.get` 与 `http.post` 能抛出六个错误码，其中三个的触发条件如下：

- 10102 等待超时：四个阶段中任何一个阶段的耗时超过传入的超时毫秒数。
- 10103 无法解析这个名字：网址里的域名没有解析成功，消息里带上整个网址。
- 10104 地址或端口不合法：网址的端口越界，或者解析不出网址的主机。

另外三个：

- 10100 是这一类的默认码：系统的原始错误码没有折进对照表时用它，消息里照样带上系统的原始号码（`internal/runtime/net.go` 的 `netCodeOf`）。
- 10101 表示连不上这个地址：连接被拒绝、没有路由、主机或网络不可达、TLS 与证书校验失败都归入这个错误码。
- 10105 表示这条连接已不可用：对方关闭了连接，或者发送、接收时连接已断。

六个错误码的完整对照见[错误信息索引](/tools/060_errors)。

六个错误码都能用 `catch` 捕获（[错误处理](/learn/031_errors)）：

```nf
try {
    let page:Response = http.get("http://127.0.0.1:9/", 1000)
    console.print(page.status)
} catch (code, msg) {
    console.print("没取到:", code)
}
```

## 未提供的能力

一次请求拿到整个响应。

不支持分块接收：完整的响应体在 `Response.body` 里。

请求体同样一次性传入：`http.post` 把字节块一次发送出去，不分块发送。

不能自定义请求头：GET 与 POST 都按固定的写法发出请求。要逐行处理响应体，需要在 `Response.body` 的基础上自己编写。

## 参见

- [TCP 套接字](/stdlib/057_net)：对方使用的不是 HTTP 协议时用它自己定义协议
- [字节块](/stdlib/047_bytes)：`http.post` 的请求体与 `Response.body` 的类型
- [文本处理](/stdlib/046_text)：`text.from_bytes` 把响应体转换成文本
- [错误信息索引](/tools/060_errors)：10100 段六个错误码与对应的报错原文

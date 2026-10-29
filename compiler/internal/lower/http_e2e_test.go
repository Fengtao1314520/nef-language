// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// http_e2e_test.go 覆盖 `http` 这一组成员：HTTP 客户端走 winhttp。
//
// 对端固定是本测试进程里用 Go 启动的 HTTP 服务器（`httptest`），请求不出本机：
// 访问外网需要当日数据落盘并设置离线开关才可靠，而这一层要验证的是它自身是否连通、
// 状态码／头／正文三项各从哪里来、以及那三种句柄是否都关闭干净。
// TLS 路径（https）不在本测试范围：本机服务器需要证书，证书相关事项在 1.0 之后再一并处理。

// urlOf 返回 httptest 服务器的地址，可直接写进 Nef 字符串字面量。
func urlOf(srv *httptest.Server) string { return srv.URL }

// TestHttpGet 验证三个返回项：状态码是数值、正文是一整块字节、头是一行一条。
//
// 头各行按原样返回（"名字: 值"），测试只检查"这一行存在"，不比较行的顺序，也不比较 Date 的值——
// 顺序与日期由服务器决定，把它们写死就变成依赖环境状态的比较。
func TestHttpGet(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.RequestURI(), r.Method
		w.Header().Set("X-Nef-Test", "nef")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong 乙"))
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.get("`+urlOf(srv)+`/hello", 5000)
console.print(r.status, text.from_bytes(r.body), length(r.body))
console.print(r.headers.index_of("X-Nef-Test: nef") >= 0, length(r.headers) >= 2)
console.print(r)
`)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if code != 0 || len(lines) != 3 {
		t.Fatalf("GET 这一趟不对：\n got %q 码 %d\n要三行", got, code)
	}
	if lines[0] != "200 pong 乙 8" || lines[1] != "true true" {
		t.Fatalf("前两行不对：%q", got)
	}
	if !strings.Contains(lines[2], "Response") || !strings.Contains(lines[2], "200") {
		t.Fatalf("第三行该按内建 struct 的印法说自己是 Response、带着状态码：%q", lines[2])
	}
	if gotPath != "/hello" || gotMethod != "GET" {
		t.Fatalf("服务器看到的这一趟不对：%s %s", gotMethod, gotPath)
	}
}

// TestHttpPostEcho 验证发送出去的字节块：正文按原样进入服务器，方法是 POST。
// 服务器把收到的字节原样返回，于是"发送量"与"接收量"两头对得上。
func TestHttpPostEcho(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	got, code := run(t, `
let msg:bytes = "甲乙丙".to_bytes()
let r:Response = http.post("`+urlOf(srv)+`/echo", msg, 5000)
console.print(r.status, length(msg), length(r.body), text.from_bytes(r.body))
console.print(r.body[0], r.body[8])
`)
	// 「甲」的 UTF-8 首字节是 231，「丙」的末字节是 153：两端各取一个字节，
	// 送出去与答回来的若不一致会当场对不上。
	want := "200 9 9 甲乙丙\n231 153\n"
	if got != want || code != 0 {
		t.Fatalf("POST 这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestHttpBigBody 验证正文的翻倍缓冲：一次接收 4096 字节，答复比初始容量大（8192），
// 中间扩容次数不作假设——用两端字节与总长核对，漏搬或重复搬都会当场对不上。
func TestHttpBigBody(t *testing.T) {
	const n = 200000
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = byte(i % 251)
		}
		buf[0] = 7
		buf[n-1] = 211
		_, _ = w.Write(buf)
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.get("`+urlOf(srv)+`/big", 10000)
console.print(r.status, length(r.body), r.body[0], r.body[`+strconv.Itoa(n-1)+`])
console.print(r.body[4096], r.body[8192])
`)
	// 那四个数值按 `i % 251` 计算：4096 − 251×16 = 80，8192 − 251×32 = 160，
	// 而第 0 个与最后一个字节被特意改成 7 与 211。
	// 跨缓冲边界搬错、少搬或重复搬，落在的数值就不一样。
	want := "200 200000 7 211\n80 160\n"
	if got != want || code != 0 {
		t.Fatalf("大正文这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestHttpTwoInARow 验证三种句柄的生命周期：连续两次请求各自打开、各自关闭。
// 若上一次未关闭干净就再次发起，第二次会用到已关闭的句柄——这种情况由 @http_begin
// 开头一段先关闭清理上一次遗留的句柄；本测试验证两次请求都成功。
func TestHttpTwoInARow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 把解码后的路径与查询串分别答回来：两条都在，才能确认"问号后面那一段送到了"
		w.Write([]byte(r.URL.Path))
		if r.URL.RawQuery != "" {
			w.Write([]byte("?" + r.URL.RawQuery))
		}
	}))
	defer srv.Close()
	got, code := run(t, `
let a:Response = http.get("`+urlOf(srv)+`/一", 5000)
let b:Response = http.get("`+urlOf(srv)+`/二?x=1", 5000)
console.print(a.status, text.from_bytes(a.body))
console.print(b.status, text.from_bytes(b.body), length(b.headers) >= 1)
`)
	want := "200 /一\n200 /二?x=1 true\n"
	if got != want || code != 0 {
		t.Fatalf("连着两次不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestHttpCodes 验证 winhttp 错误码如何映射进 10100 段：
// 三个用例各命中一个号，消息里都带系统原始错误号。
func TestHttpCodes(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"家族不认", `let r:Response = http.get("ftp://nef.invalid/x", 2000)`,
			"出错了（10104）：这个地址或端口不行"},
		{"网址是空的", `let r:Response = http.get("", 2000)`,
			"出错了（10104）：这个地址或端口不行"},
		{"主机名解析不到", `let r:Response = http.get("http://nef-没有这个域名.invalid/", 5000)`,
			"出错了（10103）：解析不到 http://nef-没有这个域名.invalid/ 这个地址"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, code := run(t, c.src+"\n")
			if code != 1 || !strings.Contains(got, c.want) {
				t.Fatalf("得到 %q 码 %d，要含 %q", got, code, c.want)
			}
			if !strings.Contains(got, "（系统的号码 ") {
				t.Fatalf("消息里要带上系统给的原号码：%q", got)
			}
		})
	}
}

// TestHttpTimeout 验证超时覆盖四个阶段（`net.connect` 那一侧只覆盖连接与收发，因为
// `getaddrinfo` 不支持超时；这一侧 `WinHttpSetTimeouts` 解析／连接／发送／接收四个都收）：
// 服务端接受连接但不作任何响应，应在给定的毫秒数处抛出 10102。
func TestHttpTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起不来：%v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, aerr := l.Accept()
		if aerr != nil {
			return
		}
		// 既不读取也不响应，八秒后关闭：这一条测试里对端只会发起一次连接
		time.Sleep(8 * time.Second)
		_ = c.Close()
	}()
	got, code := run(t, `
let r:Response = http.get("http://127.0.0.1:`+strconv.Itoa(port)+`/", 500)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10102）：等回应等超时了") {
		t.Fatalf("超时那一条不对：\n got %q 码 %d", got, code)
	}
	// 该连接由上面的八秒定时器关闭；测试不等待它，收尾交给 defer 关闭监听句柄。
}

// TestHttpSlope 验证 `Response` 及其两个成员的归属：每一圈发一次请求、接住结果，
// 到语句尽头应析构 `body` 的字节块，`headers` 的列表与容器位置一并释放。
// 每圈多出的字节应为 0——与 `TestHeapSlopeHandles` 相同的度量方法（200 圈与 5000 圈两次测量）。
//
// 对端是本机的 `httptest` 服务器：这条验证的是内存账目，不是网络本身。
func TestHttpSlope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Nef-One", "1")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	tmpl := `
let total:int = 0
for (i:int in 0 until @N@) {
    let r:Response = http.get("@URL@", 5000)
    total = total + r.status + length(r.body) + length(r.headers)
}
console.print(total)
`
	src := strings.ReplaceAll(tmpl, "@URL@", urlOf(srv))
	per := heapSlope(t, src)
	t.Logf("每一圈发一次请求                        每圈 %8.1f 字节", per)
	if per >= 2 {
		t.Fatalf("句尽头该拆的那些每圈漏了 %.1f 字节", per)
	}
}

// TestHttpNullUrlIsGuarded 验证静态检查与运行期两头：网址那个位置是引用类型，
// 传 null 发请求抛 10092 号，而不是把空指针交给系统调用。
func TestHttpNullUrlIsGuarded(t *testing.T) {
	got, code := run(t, `
let u:string = null
let r:Response = http.get(u, 1000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10092）：对着空句柄取成员或调用方法") {
		t.Fatalf("空网址要先挡下来：%q 码 %d", got, code)
	}
}

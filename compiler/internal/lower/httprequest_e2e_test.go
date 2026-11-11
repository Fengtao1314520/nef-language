// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// httprequest_e2e_test.go 覆盖底层一条 `http.request` 与它那两个封装。
//
// 对端仍是本测试进程里用 Go 起的服务器（`httptest`），请求不出本机。TLS 这一回进来了：
// 最后一条要验证"忽略证书错误"那个开关，自签证书由 `httptest.NewTLSServer` 当场生成。

// TestHttpRequestHeadersAndVerb 验证自定义头与方法是真发出去的：服务器那一侧记下方法与两个头，
// 方法写小写也按大写发出。五个位置的写法（后两个省掉）与七个位置的写法走同一条路。
func TestHttpRequestHeadersAndVerb(t *testing.T) {
	var gotMethod, gotTrace, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/thing" { // 只记第一趟：第二趟是另一个方法，别把它盖上去
			gotMethod = r.Method
			gotTrace = r.Header.Get("X-Nef-Trace")
			gotAuth = r.Header.Get("Authorization")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	got, code := run(t, `
let hs:list<string> = ["X-Nef-Trace: 42", "Authorization: Bearer abc123"]
let r:Response = http.request("delete", "`+urlOf(srv)+`/thing", hs, null, 5000)
console.print(r.status, text.from_bytes(r.body))
let s:Response = http.request("GET", "`+urlOf(srv)+`/again", hs, null, 5000, false, 0)
console.print(s.status, length(hs))
`)
	want := "200 ok\n200 2\n"
	if got != want || code != 0 {
		t.Fatalf("这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if gotMethod != "DELETE" || gotTrace != "42" || gotAuth != "Bearer abc123" {
		t.Fatalf("服务器看到的不对：%s trace=%q auth=%q", gotMethod, gotTrace, gotAuth)
	}
}

// TestHttpRequestSevenVerbs 七个词都认，大小写不敏感。
func TestHttpRequestSevenVerbs(t *testing.T) {
	for _, verb := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		t.Run(verb, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Method
				_, _ = w.Write([]byte("v"))
			}))
			defer srv.Close()
			body := "null"
			if verb == "POST" || verb == "PUT" || verb == "PATCH" {
				body = `"甲".to_bytes()`
			}
			gotOut, code := run(t, `
let r:Response = http.request("`+strings.ToLower(verb)+`", "`+urlOf(srv)+`/", [], `+body+`, 5000)
console.print(r.status)
`)
			if code != 0 || !strings.Contains(gotOut, "200") {
				t.Fatalf("%s 这一趟没成：%q 码 %d", verb, gotOut, code)
			}
			if got != verb {
				t.Fatalf("写的是小写，服务器该看到 %s，实际 %q", verb, got)
			}
		})
	}
}

// TestHttpRequestUnknownVerb 钉住 10108：不认得的方法当场抛，消息里带着写进去的那个词。
func TestHttpRequestUnknownVerb(t *testing.T) {
	got, code := run(t, `
let r:Response = http.request("GETT", "http://127.0.0.1:9/", [], null, 5000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10108）：不认得 GETT 这个方法") {
		t.Fatalf("方法不认得那一句不对：%q 码 %d", got, code)
	}
}

// TestHttpRequestHeaderLineChecked 没有冒号、名字为空、含换行三种各报 10109，
// 消息里说是第几行；合法那一种两侧空白去掉之后发出去。
func TestHttpRequestHeaderLineChecked(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"没有冒号", `["Accept: application/json", "没有冒号的一行"]`,
			"出错了（10109）：第 2 行请求头要写成 名字: 值"},
		{"名字为空", `[": 值"]`,
			"出错了（10109）：第 1 行请求头要写成 名字: 值"},
		{"名字全是空白", `["   : 值"]`,
			"出错了（10109）：第 1 行请求头要写成 名字: 值"},
		{"含换行", `["X: 1\nX-Extra: 2"]`,
			"出错了（10109）：第 1 行请求头要写成 名字: 值"},
		{"含回车", `["X: 1\u000DX-Extra: 2"]`,
			"出错了（10109）：第 1 行请求头要写成 名字: 值"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, code := run(t, `
let hs:list<string> = `+c.src+`
let r:Response = http.request("GET", "http://127.0.0.1:9/", hs, null, 5000)
console.print(r.status)
`)
			if code != 1 || !strings.Contains(got, c.want) {
				t.Fatalf("得到 %q 码 %d，要含 %q", got, code, c.want)
			}
		})
	}
}

// TestHttpRequestHeaderTrimmed 验证空白去掉之后服务器收到的是哪一种写法。
func TestHttpRequestHeaderTrimmed(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Nef-Trace")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	gotOut, code := run(t, `
let hs:list<string> = ["  X-Nef-Trace :  42  "]
let r:Response = http.request("GET", "`+urlOf(srv)+`/", hs, null, 5000)
console.print(r.status)
`)
	if code != 0 || !strings.Contains(gotOut, "200") || got != "42" {
		t.Fatalf("空白没去掉：%q 码 %d，服务器收到 %q", gotOut, code, got)
	}
}

// TestHttpRequestNullHeaderElem 验证列表里那一条是空句柄时先挡下来，不把空指针交给系统。
func TestHttpRequestNullHeaderElem(t *testing.T) {
	got, code := run(t, `
let hs:list<string> = []
hs.add(null)
let r:Response = http.request("GET", "http://127.0.0.1:9/", hs, null, 5000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10092）：对着空句柄取成员或调用方法") {
		t.Fatalf("空头那一条要先挡下来：%q 码 %d", got, code)
	}
}

// TestHttpRequestMaxBody 超过上限抛 10106 并带上两个数，
// 上限写 0 就是全部收下，写负数抛 10107。
func TestHttpRequestMaxBody(t *testing.T) {
	const n = 20000
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = byte(i%251 + 1)
		}
		_, _ = w.Write(buf)
	}))
	defer srv.Close()
	u := urlOf(srv)

	got, code := run(t, `
let r:Response = http.request("GET", "`+u+`/", [], null, 10000, false, 1000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10106）：应答超过给出的上限（上限 1000，已收 ") {
		t.Fatalf("超上限那一句不对：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", [], null, 10000)
console.print(r.status, length(r.body))
`)
	if code != 0 || got != "200 "+strconv.Itoa(n)+"\n" {
		t.Fatalf("上限省掉就是全部收下：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", [], null, 10000, false, 0)
console.print(length(r.body))
`)
	if code != 0 || got != strconv.Itoa(n)+"\n" {
		t.Fatalf("上限写 0 也是全部收下：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", [], null, 10000, false, -1)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10107）：给出的应答上限不能是负数") {
		t.Fatalf("负上限那一句不对：%q 码 %d", got, code)
	}

	// 正好等于上限不算超
	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", [], null, 10000, false, `+strconv.Itoa(n)+`)
console.print(length(r.body))
`)
	if code != 0 || got != strconv.Itoa(n)+"\n" {
		t.Fatalf("正好等于上限该收下：%q 码 %d", got, code)
	}
}

// TestHttpRequestGzip 开了系统自动解压之后，压缩应答返回的是解开的内容。
// 服务器无条件压缩，所以这一条不依赖调用方写没写 Accept-Encoding。
func TestHttpRequestGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("压缩过的正文 甲"))
		_ = gz.Close()
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.request("GET", "`+urlOf(srv)+`/", [], null, 5000)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || !strings.Contains(got, "200 压缩过的正文 甲") {
		t.Fatalf("压缩应答没解开：%q 码 %d", got, code)
	}
}

// TestHttpRequestRedirectCredentials 给了凭据头就不自动跟随，30x 原样返回；
// 没给那三个头的请求照旧跟随到底。
func TestHttpRequestRedirectCredentials(t *testing.T) {
	var hits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("到了"))
	}))
	defer target.Close()
	from := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, urlOf(target)+"/final", http.StatusFound)
	}))
	defer from.Close()

	got, code := run(t, `
let hs:list<string> = ["Authorization: Bearer 甲"]
let r:Response = http.request("GET", "`+urlOf(from)+`/start", hs, null, 5000)
console.print(r.status)
`)
	if code != 0 || got != "302\n" {
		t.Fatalf("带凭据头该拿到 302：%q 码 %d", got, code)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("带凭据头不该跟到另一台：对面被打了 %d 次", atomic.LoadInt32(&hits))
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+urlOf(from)+`/start", ["X-Nef: 1"], null, 5000)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || got != "200 到了\n" {
		t.Fatalf("不带凭据头该跟随到底：%q 码 %d", got, code)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("不带凭据头该跟过去一次：实际 %d 次", atomic.LoadInt32(&hits))
	}
}

// TestHttpRequestIgnoreCert 自签证书默认拒（10101），第六个位置写 true 就放行。
func TestHttpRequestIgnoreCert(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("加密的"))
	}))
	defer srv.Close()

	got, code := run(t, `
let r:Response = http.request("GET", "`+srv.URL+`/", [], null, 8000, true)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || got != "200 加密的\n" {
		t.Fatalf("忽略证书错误那一次没成：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+srv.URL+`/", [], null, 8000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10101）：连不上这个地址") {
		t.Fatalf("默认该拒证书：%q 码 %d", got, code)
	}
}

// TestHttpRequestPostBody 验证底层那一条发正文与封装 `http.post` 是同一段码：
// 服务器把收到的字节原样答回来，两头对得上。
func TestHttpRequestPostBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	got, code := run(t, `
let msg:bytes = "甲乙丙".to_bytes()
let r:Response = http.request("POST", "`+urlOf(srv)+`/echo", ["Content-Type: application/octet-stream"], msg, 5000)
let s:Response = http.post("`+urlOf(srv)+`/echo", msg, 5000)
console.print(r.status, text.from_bytes(r.body), length(msg))
console.print(s.status, text.from_bytes(s.body))
`)
	want := "200 甲乙丙 9\n200 甲乙丙\n"
	if got != want || code != 0 {
		t.Fatalf("底层与封装该答同一段字节：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestHttpRequestUrlPortWithoutPath 钉住一处查出来的缺陷：网址写了端口但没写路径时，
// 拆网址那一段走的是 atColon → digStop → cut，路径起点那个位置从来没被赋值，读的是栈上残留。
// 残留恰好是 0 时补一个 '/'，非 0 时就拿着一个野指针去扫宽字符。循环里连着发两次最容易撞上。
func TestHttpRequestUrlPortWithoutPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("路径是 " + r.URL.Path))
	}))
	defer srv.Close()
	got, code := run(t, `
let total:int = 0
for (i:int in 0 until 3) {
    let r:Response = http.get("`+urlOf(srv)+`", 5000)
    total = total + r.status
    console.print(text.from_bytes(r.body))
}
console.print(total)
`)
	want := "路径是 /\n路径是 /\n路径是 /\n600\n"
	if got != want || code != 0 {
		t.Fatalf("带端口不带路径该补一个斜杠：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestHttpArgTempsReclaimedAtSentenceEnd 钉住"交进组函数的中间值"的归属：正文、头列表、网址
// 这三个位置运行期只读一次，读完了 caller 这一侧就是唯一的持有者，到这一句尽头就该析构。
//
// 这一处从前是漏的：`http.post(url, "甲".to_bytes(), ms)` 每轮留下一个 24 字节的块（HEAD 实测
// 同一形状每轮 24.0 字节）。规则复用 `dict.add` 的键与 `d.get` 的默认值那一条
// （`argOwnedByCaller` 判持有者、`releaseIfAny` 发析构），不是为 HTTP 另立一条。
func TestHttpArgTempsReclaimedAtSentenceEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	cases := []struct{ name, src string }{
		{"封装的正文是当场造的", `
for (i:int in 0 until @N@) {
    let r:Response = http.post("@URL@", "甲".to_bytes(), 5000)
    total = total + r.status
}`},
		{"底层的正文与头都是当场造的", `
for (i:int in 0 until @N@) {
    let r:Response = http.request("POST", "@URL@", ["X-Nef: 1"], "甲".to_bytes(), 5000)
    total = total + r.status
}`},
		{"网址是当场拼出来的", `
for (i:int in 0 until @N@) {
    let r:Response = http.get("@URL@" + "/", 5000)
    total = total + r.status
}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "let total:int = 0\n" + strings.ReplaceAll(c.src, "@URL@", urlOf(srv)) + "\nconsole.print(total)\n"
			per := heapSlope(t, src)
			t.Logf("%-26s 每轮 %8.1f 字节", c.name, per)
			if per >= 2 {
				t.Fatalf("交进组函数的中间值每轮漏了 %.1f 字节", per)
			}
		})
	}
}

// TestHttpRequestSlope 验证归属：每一圈带着头发一次请求，句尽头该拆的那些一并拆掉。
//
// 正文写成一个名字交进去，不写成 `"甲".to_bytes()` 这种式子中间值：后者每轮漏 24 字节，
// 是 `http.post` 在本条之前就有的那一处（HEAD 实测同一形状每轮 24.0 字节），
// 归中间值那一本账，不在这一条测试里改；这一条只管头那一族临时块与 `Response` 的两个字段。
func TestHttpRequestSlope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // 正文要读干净：不读的话服务器会提前关掉这一条连接
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	tmpl := `
let msg:bytes = "甲".to_bytes()
let total:int = 0
for (i:int in 0 until @N@) {
    let hs:list<string> = ["X-Nef: 1", "Accept: text/plain"]
    let r:Response = http.request("POST", "@URL@", hs, msg, 5000)
    total = total + r.status + length(r.body) + length(r.headers)
}
console.print(total)
`
	src := strings.ReplaceAll(tmpl, "@URL@", urlOf(srv))
	per := heapSlope(t, src)
	t.Logf("每一圈带着头发一次请求                      每圈 %8.1f 字节", per)
	if per >= 2 {
		t.Fatalf("句尽头该拆的那些每轮漏了 %.1f 字节", per)
	}
}

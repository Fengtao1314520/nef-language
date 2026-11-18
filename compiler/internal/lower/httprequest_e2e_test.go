// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
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
let r:Response = http.request("delete", "`+urlOf(srv)+`/thing", null, hs, 5000)
console.print(r.status, text.from_bytes(r.body))
let s:Response = http.request("GET", "`+urlOf(srv)+`/again", null, hs, 5000, false, 0)
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

// TestHttpWrappersCarryHeaders 钉住封装那两条新加的末尾 headers：get 与 post 都能带认证头发出去，
// 省掉那一个位置就是不带任何头（旧的两/三参写法照旧）。
func TestHttpWrappersCarryHeaders(t *testing.T) {
	var gotAuth, gotTrace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Nef") != "" {
			gotAuth = r.Header.Get("Authorization")
			gotTrace = r.Header.Get("X-Nef")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	got, code := run(t, `
let hs:list<string> = ["Authorization: Bearer tok9", "X-Nef: 7"]
let a:Response = http.get("`+urlOf(srv)+`/", [], 5000)
console.print(a.status)
let b:Response = http.get("`+urlOf(srv)+`/", hs, 5000)
console.print(b.status, length(hs))
let c:Response = http.post("`+urlOf(srv)+`/", "x".to_bytes(), hs, 5000)
console.print(c.status)
`)
	if code != 0 {
		t.Fatalf("这一趟没成：%q 码 %d", got, code)
	}
	if !strings.Contains(got, "200\n200 2\n200") {
		t.Fatalf("输出不对：%q", got)
	}
	if gotAuth != "Bearer tok9" || gotTrace != "7" {
		t.Fatalf("封装没把头发出去：auth=%q trace=%q", gotAuth, gotTrace)
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
let r:Response = http.request("`+strings.ToLower(verb)+`", "`+urlOf(srv)+`/", `+body+`, [], 5000)
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
let r:Response = http.request("GETT", "http://127.0.0.1:9/", null, [], 5000)
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
let r:Response = http.request("GET", "http://127.0.0.1:9/", null, hs, 5000)
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
let r:Response = http.request("GET", "`+urlOf(srv)+`/", null, hs, 5000)
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
let r:Response = http.request("GET", "http://127.0.0.1:9/", null, hs, 5000)
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
let r:Response = http.request("GET", "`+u+`/", null, [], 10000, false, 1000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10106）：应答超过给出的上限（上限 1000，已收 ") {
		t.Fatalf("超上限那一句不对：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", null, [], 10000)
console.print(r.status, length(r.body))
`)
	if code != 0 || got != "200 "+strconv.Itoa(n)+"\n" {
		t.Fatalf("上限省掉就是全部收下：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", null, [], 10000, false, 0)
console.print(length(r.body))
`)
	if code != 0 || got != strconv.Itoa(n)+"\n" {
		t.Fatalf("上限写 0 也是全部收下：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", null, [], 10000, false, -1)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10107）：给出的应答上限不能是负数") {
		t.Fatalf("负上限那一句不对：%q 码 %d", got, code)
	}

	// 正好等于上限不算超
	got, code = run(t, `
let r:Response = http.request("GET", "`+u+`/", null, [], 10000, false, `+strconv.Itoa(n)+`)
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
let r:Response = http.request("GET", "`+urlOf(srv)+`/", null, [], 5000)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || !strings.Contains(got, "200 压缩过的正文 甲") {
		t.Fatalf("压缩应答没解开：%q 码 %d", got, code)
	}
}

// TestHttpRequestDeflateZlib 服务器给 zlib 包裹的 deflate（网上多数服务器的那一种）。
// winhttp 只认 raw deflate，这一种由 @http_inflate 自己解。
func TestHttpRequestDeflateZlib(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		_, _ = zw.Write([]byte("zlib 包裹的正文 乙"))
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.get("`+urlOf(srv)+`/", [], 5000)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || !strings.Contains(got, "200 zlib 包裹的正文 乙") {
		t.Fatalf("zlib 包裹的 deflate 没解开：%q 码 %d", got, code)
	}
}

// TestHttpRequestDeflateRaw 不带包裹的 raw deflate 也认。
func TestHttpRequestDeflateRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		var buf bytes.Buffer
		fw, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		_, _ = fw.Write([]byte("raw deflate 的正文 丙"))
		_ = fw.Close()
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.get("`+urlOf(srv)+`/", [], 5000)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || !strings.Contains(got, "200 raw deflate 的正文 丙") {
		t.Fatalf("raw deflate 没解开：%q 码 %d", got, code)
	}
}

// TestHttpRequestDeflateBig 大正文（动态表加匹配回拷）经 http.get 解开后逐字对得上。
func TestHttpRequestDeflateBig(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("第 " + strconv.Itoa(i) + " 行 甲乙丙丁戊己庚辛壬癸\n")
	}
	want := sb.String()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		_, _ = zw.Write([]byte(want))
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.get("`+urlOf(srv)+`/", [], 8000)
console.print(r.status, length(r.body))
`)
	if code != 0 || !strings.Contains(got, "200 "+strconv.Itoa(len(want))) {
		t.Fatalf("大 deflate 正文长度不对：%q 码 %d（要 %d 字节）", got, code, len(want))
	}
}

// TestHttpRequestDeflateOverLimit 压缩时没超上限、解开后超了，也按超上限抛 10106。
func TestHttpRequestDeflateOverLimit(t *testing.T) {
	body := strings.Repeat("甲", 5000) // 解开后 15000 字节
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		_, _ = zw.Write([]byte(body))
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.request("GET", "`+urlOf(srv)+`/", null, [], 5000, false, 100)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10106）：应答超过给出的上限") {
		t.Fatalf("解开后超上限该抛 10106：%q 码 %d", got, code)
	}
}

// TestHttpRequestDeflateCorrupt Content-Encoding 说是 deflate，正文却是乱字节，抛 10113。
func TestHttpRequestDeflateCorrupt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		_, _ = w.Write([]byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA})
	}))
	defer srv.Close()
	got, code := run(t, `
let r:Response = http.get("`+urlOf(srv)+`/", [], 5000)
console.print(r.status)
`)
	if code != 1 || !strings.Contains(got, "出错了（10113）：应答的压缩正文解不开") {
		t.Fatalf("解不开该抛 10113：%q 码 %d", got, code)
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
let r:Response = http.request("GET", "`+urlOf(from)+`/start", null, hs, 5000)
console.print(r.status)
`)
	if code != 0 || got != "302\n" {
		t.Fatalf("带凭据头该拿到 302：%q 码 %d", got, code)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("带凭据头不该跟到另一台：对面被打了 %d 次", atomic.LoadInt32(&hits))
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+urlOf(from)+`/start", null, ["X-Nef: 1"], 5000)
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
let r:Response = http.request("GET", "`+srv.URL+`/", null, [], 8000, true)
console.print(r.status, text.from_bytes(r.body))
`)
	if code != 0 || got != "200 加密的\n" {
		t.Fatalf("忽略证书错误那一次没成：%q 码 %d", got, code)
	}

	got, code = run(t, `
let r:Response = http.request("GET", "`+srv.URL+`/", null, [], 8000)
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
let msg:Bytes = "甲乙丙".to_bytes()
let r:Response = http.request("POST", "`+urlOf(srv)+`/echo", msg, ["Content-Type: application/octet-stream"], 5000)
let s:Response = http.post("`+urlOf(srv)+`/echo", msg, [], 5000)
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
for (i:int from 0 until 3) {
    let r:Response = http.get("`+urlOf(srv)+`", [], 5000)
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
for (i:int from 0 until @N@) {
    let r:Response = http.post("@URL@", "甲".to_bytes(), [], 5000)
    total = total + r.status
}`},
		{"底层的正文与头都是当场造的", `
for (i:int from 0 until @N@) {
    let r:Response = http.request("POST", "@URL@", "甲".to_bytes(), ["X-Nef: 1"], 5000)
    total = total + r.status
}`},
		{"网址是当场拼出来的", `
for (i:int from 0 until @N@) {
    let r:Response = http.get("@URL@" + "/", [], 5000)
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
let msg:Bytes = "甲".to_bytes()
let total:int = 0
for (i:int from 0 until @N@) {
    let hs:list<string> = ["X-Nef: 1", "Accept: text/plain"]
    let r:Response = http.request("POST", "@URL@", msg, hs, 5000)
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

// TestHttpPaddedStructBodyAcrossProfiles 把报告 #9 的走法搬进本机：两个 profile 顺序跑，
// 每个先登录一次、再发一批带 n 的 GET 与心跳 POST，正文由 `json.from_value` 从 struct 造，
// 回来的 JSON 解析、读字段、`take` 取值。两种 struct 都带窄字段（`{boolean, int, boolean,
// string}` 与九字段的心跳 DTO），字段宽度之和比按对齐排出来的布局小——块按和造就冲出块尾，
// 压进下一块的头（见 `internal/check/layout.go` 里 `BlockSize` 的注释），症状是进程在
// 某个调用边界上以 `0xC0000005`／`0xC0000409` 直接退出，一句报错也没有。
//
// 这一段为什么值得单独一条：`TestStructNarrowPadKeepsItsBlock` 只钉了打印、拷贝与
// `json.from_value`，没把 struct 送到网络那一侧；而报告里崩的是"HTTP ＋ JSON ＋ 收尾"
// 叠在一起的那一条路，块在堆上的邻居换成了响应正文与树节点，冲出块尾压坏的东西不同，
// 露出来的时机也不同（要过好几轮才炸）。
func TestHttpPaddedStructBodyAcrossProfiles(t *testing.T) {
	var beats int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hb" {
			atomic.AddInt32(&beats, 1)
			_, _ = w.Write([]byte(`{"code":0,"ok":true}`))
			return
		}
		n := r.URL.Query().Get("n")
		if n == "" {
			n = "0"
		}
		_, _ = w.Write([]byte(`{"code":0,"seq":` + n + `,"owner":"sim"}`))
	}))
	defer srv.Close()

	src := `
struct Login {
    instance:int
    sessionUuid:string
    id:string
    password:string
    isMTLogin:boolean
    loginTime:string
}

struct Heartbeat {
    ip:string
    sessionUuid:string
    userInstance:int
    timeStamp:string
    pageVisible:boolean
    count:int
    isFallback:boolean
    tabOwnerId:string
    tabOwnerGeneration:string
}

method body_of<Value>(value:Value):string {
    return json.from_value(value).to_json()
}

method run_one(name:string, n_cases:int):int {
    let fails:int = 0
    let lg:Login = Login {
        instance: 1,
        sessionUuid: name,
        id: "002",
        password: "1234",
        isMTLogin: false,
        loginTime: "1794886000"
    }
    if (length(body_of(lg)) == 0) {
        fails = fails + 1
    }
    for (i:int from 0 until n_cases) {
        let r:Response = http.get("@URL@/status?n=" + number.to_string(i), [], 5000)
        if (r.status != 200) {
            fails = fails + 1
        }
        let doc:Json = json.parse(text.from_bytes(r.body))
        if (doc == null) {
            fails = fails + 1
        } else {
            if (doc["seq"].to_int(-1) != i) {
                fails = fails + 1
            }
            let owner:Json = doc.take("owner")
            if (owner.to_text() != "sim") {
                fails = fails + 1
            }
        }
        if (i % 3 == 0) {
            let hb:Heartbeat = Heartbeat {
                ip: "127.0.0.1",
                sessionUuid: name,
                userInstance: 1,
                timeStamp: "1795000000000",
                pageVisible: true,
                count: i,
                isFallback: false,
                tabOwnerId: "owner",
                tabOwnerGeneration: "1"
            }
            let hb_body:Json = json.from_value(hb)
            let hr:Response = http.post("@URL@/hb", hb_body, [], 5000)
            if (hr.status != 200) {
                fails = fails + 1
            }
            let back:Json = json.parse(text.from_bytes(hr.body))
            if (back == null) {
                fails = fails + 1
            } else {
                if (back["ok"].to_boolean(false) == false) {
                    fails = fails + 1
                }
            }
        }
    }
    return fails
}

let first:int = run_one("primary", 4)
console.print("profile 1:", first)
let second:int = run_one("secondary", 2)
console.print("profile 2:", second)
console.print("done")
`
	got, code := run(t, strings.ReplaceAll(src, "@URL@", urlOf(srv)))
	want := "profile 1: 0\nprofile 2: 0\ndone\n"
	if got != want || code != 0 {
		t.Fatalf("两个 profile 走完这一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if n := atomic.LoadInt32(&beats); n != 3 {
		t.Fatalf("心跳 POST 收到 %d 次，要 3 次（第一个 profile 两次、第二个一次）", n)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// httprequest_test.go 覆盖新增的底层一条 `http.request` 的检查面：
// 七个位置的类型、后两个位置可省、封装那两条照旧。
//
// 实际发出去那一趟在 `internal/lower/httprequest_e2e_test.go`。

func TestHttpRequestSignature(t *testing.T) {
	expectOK(t, `let hs:list<string> = ["X-Nef: 甲"]
let b:Bytes = "甲".to_bytes()
let r:Response = http.request("POST", "http://127.0.0.1/", b, hs, 5000)
let s:Response = http.request("GET", "http://127.0.0.1/", null, hs, 5000, true, 1024)
let u:Response = http.request("GET", "http://127.0.0.1/")
console.print(r.status, s.status, u.status, length(b), length(hs))
`)
	// 正文、头、超时与后两个开关都带默认值，但省只能从末尾连着省。
	expectOK(t, `let r:Response = http.request("GET", "http://x/")
let s:Response = http.request("GET", "http://x/", null, [])
let t:Response = http.request("GET", "http://x/", null, [], 5000)
console.print(r.status, s.status, t.status)
`)
	expectErr(t, `let r:Response = http.request("GET")`,
		"request 收 7 个参数，这里给了 1 个")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", null, [], 5000, false, 0, true)`,
		"request 收 7 个参数，这里给了 8 个")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", null, [], 5000, 1024)`,
		"这里要的是 boolean，给的是 int；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request(1, "http://x/")`,
		"这里要的是 string，给的是 int；类型之间不自动转换")
	expectErr(t, `let d:dict<string, string> = {}
let r:Response = http.request("GET", "http://x/", null, d)`,
		"这里要的是 list<string>，给的是 dict<string, string>；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", null, [1])`,
		"这里要的是 list<string>，给的是 list<int>；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", "甲")`,
		"这里要的是 Bytes，给的是 string；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", null, [], "5000")`,
		"这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", null, [], 5000, false, "1024")`,
		"这里要的是 int，给的是 string；类型之间不自动转换")
	// 头与正文都只是被读一遍：调用之后原来的名字照旧能用
	expectOK(t, `let hs:list<string> = ["X: 1"]
let b:Bytes = bytes.allocate(4)
let r:Response = http.request("POST", "http://x/", b, hs, 5000)
console.print(length(hs), length(b), r.status)
`)
}

// TestHttpWrappersKeepTheirShape 钉住封装那两条：底层那一条的参数换了顺序之后，
// get 仍是"网址、头、超时"三个位置，post 仍是"网址、正文、头、超时"四个位置，
// 后两个都带默认值；底下换成 `http.request` 不改变调用方写出来的样子。
func TestHttpWrappersKeepTheirShape(t *testing.T) {
	expectOK(t, `let r:Response = http.get("http://x/")
let s:Response = http.post("http://x/", "甲".to_bytes())
console.print(r.status, s.status)
`)
	// 头那一个位置能写：空表与带几条头都收
	expectOK(t, `let hs:list<string> = ["X-T: 1"]
let r:Response = http.get("http://x/", hs, 5000)
let s:Response = http.post("http://x/", "甲".to_bytes(), hs, 5000)
console.print(r.status, s.status, length(hs))
`)
	expectErr(t, `let r:Response = http.get("http://x/", [], 5000, 5000)`,
		"get 收 3 个参数，这里给了 4 个")
	expectErr(t, `let r:Response = http.post("http://x/", "甲")`,
		"这里要的是 Bytes，给的是 string；类型之间不自动转换")
}

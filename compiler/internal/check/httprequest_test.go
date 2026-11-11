// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// httprequest_test.go 覆盖新增的底层一条 `http.request` 的检查面：
// 七个位置的类型、后两个位置可省、封装那两条照旧。
//
// 实际发出去那一趟在 `internal/lower/httprequest_e2e_test.go`。

func TestHttpRequestSignature(t *testing.T) {
	expectOK(t, `let hs:list<string> = ["X-Nef: 甲"]
let b:bytes = "甲".to_bytes()
let r:Response = http.request("POST", "http://127.0.0.1/", hs, b, 5000)
let s:Response = http.request("GET", "http://127.0.0.1/", hs, null, 5000, true, 1024)
let u:Response = http.request("GET", "http://127.0.0.1/", [], null, 5000)
console.print(r.status, s.status, u.status, length(b), length(hs))
`)
	// 前五个位置一个都不能省
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [], null)`,
		"request 收 7 个参数，这里给了 4 个")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [], null, 5000, false, 0, true)`,
		"request 收 7 个参数，这里给了 8 个")
	// 省只能从末尾省：跳过第六个直接写第七个不接受
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [], null, 5000, 1024)`,
		"这里要的是 boolean，给的是 int；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request(1, "http://x/", [], null, 5000)`,
		"这里要的是 string，给的是 int；类型之间不自动转换")
	expectErr(t, `let d:dict<string, string> = {}
let r:Response = http.request("GET", "http://x/", d, null, 5000)`,
		"这里要的是 list<string>，给的是 dict<string, string>；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [1], null, 5000)`,
		"这里要的是 list<string>，给的是 list<int>；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [], "甲", 5000)`,
		"这里要的是 bytes，给的是 string；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [], null, "5000")`,
		"这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, `let r:Response = http.request("GET", "http://x/", [], null, 5000, false, "1024")`,
		"这里要的是 int，给的是 string；类型之间不自动转换")
	// 头与正文都只是被读一遍：调用之后原来的名字照旧能用
	expectOK(t, `let hs:list<string> = ["X: 1"]
let b:bytes = bytes.new(4)
let r:Response = http.request("POST", "http://x/", hs, b, 5000)
console.print(length(hs), length(b), r.status)
`)
}

// TestHttpWrappersKeepTheirShape 钉住封装那两条：底层那一条加进来之后它们仍然是两个与三个位置，
// 底下换成 `http.request` 不改变调用方写出来的样子。
func TestHttpWrappersKeepTheirShape(t *testing.T) {
	expectOK(t, `let r:Response = http.get("http://x/", 5000)
let s:Response = http.post("http://x/", "甲".to_bytes(), 5000)
console.print(r.status, s.status)
`)
	expectErr(t, `let r:Response = http.get("http://x/", [], 5000)`,
		"get 收 2 个参数，这里给了 3 个")
	expectErr(t, `let r:Response = http.post("http://x/", "甲", 5000)`,
		"这里要的是 bytes，给的是 string；类型之间不自动转换")
}

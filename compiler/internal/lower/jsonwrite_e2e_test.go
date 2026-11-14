// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// jsonwrite_e2e_test.go 按真实产物验收 json 的构造与写入那一族：空树、往树上放一个位置、
// 把程序里的一个值变成一棵树，以及这一族在检查阶段就该拒绝的几种写法。
//
// 四条各钉一件事：
//   - 写进去再渲染出来，与手打的那一段 JSON 文本一字不差；
//   - 原生容器变成树只读源，不动它的归属，套两万圈一块内存不留；
//   - 形状用错（数组的动作打在对象上）当场抛 10114 号；
//   - 写不进 JSON 的那几种类型（bytes、hashset、另一棵树）在编译期就说清是哪一层。
//
// en: jsonwrite_e2e_test.go accepts the construction and writing side of json against real
// products: empty trees, putting one position on a tree, turning a program value into a tree, and
// the spellings the checking phase must refuse. Four things each get a nail: what goes in renders
// byte for byte like the hand-written text; making a native container into a tree only reads it,
// leaves its owner alone and keeps no memory over twenty thousand rounds; using the wrong shape
// throws code 10114 on the spot; and the types that cannot be written as JSON - bytes, hashset,
// another tree - name their layer at compile time.
package lower_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJSONWriteRoundTrip 钉构造与写入：写进去什么就渲染出什么。
func TestJSONWriteRoundTrip(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"对象按写的顺序出，覆盖那一个位置不变", `
let body:Json = json.new_object()
body.set("name", "nef")
body.set("count", 2)
body.set("name", "NEF")
console.print(body.to_json())
`, "{\"name\":\"NEF\",\"count\":2}\n"},
		{"数组的三条：尾巴上放、中间插、按下标盖", `
let row:Json = json.new_array()
row.add(2)
row.add(3)
row.insert_at(0, 1)
row[1] = 9
console.print(row.to_json())
`, "[1,9,3]\n"},
		{"下标赋值两条都认：文本走对象，整数走数组", `
let body:Json = json.new_object()
body["a"] = [1, 2]
console.print(body.to_json())
`, "{\"a\":[1,2]}\n"},
		{"摘出来那两条：remove 答真与假，remove_at 交出那一棵", `
let body:Json = json.parse("{ \"a\": 1, \"b\": 2 }")
console.print(body.remove("a"), body.remove("丁"))
let row:Json = json.parse("[7, 8]")
let last:Json = row.remove_at(0)
console.print(last.to_integer(0), row.to_json())
`, "true false\n7 [8]\n"},
		{"清空之后还在，长度是 0", `
let body:Json = json.parse("{ \"a\": [1, 2] }")
body.clear()
console.print(body.to_json(), length(body))
`, "{} 0\n"},
		{"嵌套着写：对象里放数组，数组里放对象", `
let row:Json = json.new_array()
let one:Json = json.new_object()
one.set("k", true)
row.add(one)
console.print(row.to_json())
`, "[{\"k\":true}]\n"},
	}
	for _, c := range cases {
		got, code := run(t, c.src)
		if code != 0 {
			t.Errorf("%s：码 %d，要 0。输出 %q", c.name, code, got)
			continue
		}
		if got != c.want {
			t.Errorf("%s：\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestJSONFromValue 钉"程序里的一个值变成一棵树"：原生容器逐层走形状表。
func TestJSONFromValue(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"五种标量各成一个节点", `
console.print(json.from_value(7).to_json())
console.print(json.from_value(1.5).to_json())
console.print(json.from_value(true).to_json())
console.print(json.from_value("甲").to_json())
console.print(json.from_value(7 as byte).to_json())
`, "7\n1.5\ntrue\n\"甲\"\n7\n"},
		{"list 成数组，dict 成对象，嵌套照形状走", `
let scores:list<int> = [90, 92]
let deep:dict<string, list<int>> = {"a": [1, 2]}
console.print(json.from_value(scores).to_json())
console.print(json.from_value(deep).to_json())
`, "[90,92]\n{\"a\":[1,2]}\n"},
		{"struct 与 class 成对象，字段名是那把键", `
struct Point {
    x:int
    y:int
}
class Node {
    name:string
    kids:list<int>
}
let p:Point = Point{x: 1, y: 2}
let n:Node = new Node(name: "n", kids: [3, 4])
console.print(json.from_value(p).to_json())
console.print(json.from_value(n).to_json())
`, "{\"x\":1,\"y\":2}\n{\"name\":\"n\",\"kids\":[3,4]}\n"},
		{"整数键与布尔键各成一串文本：字典的键在 JSON 里是一串字符", `
let byNum:dict<int, string> = {7: "甲"}
console.print(json.from_value(byNum).to_json())
`, "{\"7\":\"甲\"}\n"},
		{"变成树只读源：那个名字之后还能用，也能改", `
let scores:list<int> = [1, 2]
let tree:Json = json.from_value(scores)
scores.add(3)
console.print(tree.to_json(), json.from_value(scores).to_json())
`, "[1,2] [1,2,3]\n"},
		{"写成 JSON 再读回来：往返同一条", `
let deep:dict<string, list<int>> = {"a": [1, 2], "b": []}
let printed:string = json.from_value(deep).to_json()
let back:Json = json.parse(printed)
console.print(back["a"].kind(), length(back["a"]), back["b"].kind())
`, "array 2 array\n"},
	}
	for _, c := range cases {
		got, code := run(t, c.src)
		if code != 0 {
			t.Errorf("%s：码 %d，要 0。输出 %q", c.name, code, got)
			continue
		}
		if got != c.want {
			t.Errorf("%s：\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestJSONWriteOwnership 钉三件归属：交进树的那个名字作废、覆盖与删除换下来的那棵当场拆掉、
// 两万圈一块内存不留。
func TestJSONWriteOwnership(t *testing.T) {
	if err := compileErr(`
let inner:Json = json.new_array()
inner.add(1)
let outer:Json = json.new_object()
outer.set("list", inner)
console.print(inner.to_json())
`); err == nil || !strings.Contains(err.Error(), "inner 已经交出去了") {
		t.Errorf("交进树的那一个名字应当当场作废，实际得到：%v", err)
	}

	// 每轮造一棵新的、拆一棵旧的：覆盖与摘出来换下来的那一棵都要拆干净。
	cases := []struct {
		name, tmpl string
	}{
		{"写进树里再渲染", `
let total:int = 0
for (i:int in 0 until @N@) {
    let body:Json = json.new_object()
    body.set("list", [1, 2, 3])
    body.set("name", "甲")
    body.set("name", "乙")
    let out:string = body.to_json()
    total = total + 1
}
`},
		{"拿掉与清空", `
let total:int = 0
for (i:int in 0 until @N@) {
    let body:Json = json.new_object()
    body.set("a", [1, 2, 3])
    body.set("b", {"c": "丁"})
    let one:Json = body.take("a")
    body.remove("b")
    body.clear()
    total = total + length(one)
}
`},
		{"原生容器变成树", `
let total:int = 0
let deep:dict<string, list<int>> = {"a": [1, 2, 3], "b": [4]}
for (i:int in 0 until @N@) {
    let out:string = json.from_value(deep).to_json()
    total = total + 1
}
`},
	}
	for _, c := range cases {
		if slope := heapSlope(t, c.tmpl); slope != 0 {
			t.Errorf("%s：每轮留 %v 字节，那一路有东西没拆", c.name, slope)
		}
	}
}

// TestJSONShapeMismatch 钉形状用错那一档：10114 号，以及下标越界仍回到容器那一条 10031 号。
func TestJSONShapeMismatch(t *testing.T) {
	cases := []struct {
		src, want string
	}{
		{`let row:Json = json.new_array()
row.set("k", 1)`, "10114）：这一棵 Json 不是那一种形状"},
		{`let body:Json = json.new_object()
body.add(1)`, "10114）：这一棵 Json 不是那一种形状"},
		{`let body:Json = json.new_object()
body.remove_at(0)`, "10114）：这一棵 Json 不是那一种形状"},
		{`let row:Json = json.new_array()
row.insert_at(5, 1)`, "10031）：下标越界"},
	}
	for _, c := range cases {
		got, code := run(t, c.src+"\n")
		if code != 1 || !strings.Contains(got, c.want) {
			t.Fatalf("%s 得到 %q 码 %d，要含 %q", c.src, got, code, c.want)
		}
	}
}

// TestHttpPostJsonBody 钉正文那一个位置多收的那一种：写一棵树进去，发出去的是渲染好的
// 那一段文本；树上那个位置一个字节都不动，两次发的还是同一段。
func TestHttpPostJsonBody(t *testing.T) {
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
let row:Json = json.new_object()
row.set("name", "nef")
row.set("tags", ["语言", "编译器"])
let r:Response = http.post("`+urlOf(srv)+`/echo", row, 5000)
let second:Response = http.request("POST", "`+urlOf(srv)+`/echo", [], row, 5000)
console.print(text.from_bytes(r.body))
console.print(text.from_bytes(second.body))
console.print(row.to_json())
`)
	want := "{\"name\":\"nef\",\"tags\":[\"语言\",\"编译器\"]}\n" +
		"{\"name\":\"nef\",\"tags\":[\"语言\",\"编译器\"]}\n" +
		"{\"name\":\"nef\",\"tags\":[\"语言\",\"编译器\"]}\n"
	if code != 0 {
		t.Fatalf("POST 一棵树这一趟退出码 %d：\n%s", code, got)
	}
	if got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

// TestHttpJsonBodySlope 钉那一趟的内存：正文由树渲染出来的是两块中转（文本与字节块），
// 发完就在这一句里还掉，两万圈一块不留。
func TestHttpJsonBodySlope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	tmpl := `
let row:Json = json.new_object()
row.set("name", "nef")
let total:int = 0
for (i:int in 0 until @N@) {
    let r:Response = http.post("` + urlOf(srv) + `/echo", row, 5000)
    total = total + length(r.body)
}
console.print(total)
`
	if slope := heapSlope(t, tmpl); slope != 0 {
		t.Errorf("每轮留 %v 字节，正文那两块没还掉", slope)
	}
}

// TestJSONWriteRefused 钉检查阶段就拒绝的几种写法：不是到了运行期才交一个 null。
func TestJSONWriteRefused(t *testing.T) {
	cases := []struct{ src, want string }{
		{`let b:bytes = bytes.new(2)
console.print(json.from_value(b).to_json())`, "bytes 写不成 JSON"},
		{`let s:hashset<int> = {1, 2}
console.print(json.from_value(s).to_json())`, "hashset<int> 写不成 JSON"},
		{`let j:Json = json.parse("[1]")
console.print(json.from_value(j).to_json())`, "Json 本来就是一棵树"},
		{`let row:list<bytes> = [bytes.new(2)]
console.print(json.from_value(row).to_json())`, "写不成 JSON"},
		{`let row:Json = json.new_array()
row.add(bytes.new(2))`, "bytes 写不成 JSON"},
	}
	for _, c := range cases {
		err := compileErr(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s 该拒，要含 %q，实际得到：%v", c.src, c.want, err)
		}
	}
}

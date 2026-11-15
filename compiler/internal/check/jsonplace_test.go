// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// jsonplace_test.go 覆盖 `Json` 能进容器、泛型实例与 `class` 字段这一条。
//
// 早前两处都拒绝：容器那个位置的形状与析构要靠运行期类型字，而 `Json` 头上没有那一个号；
// `class` 的字段放 `Json` 则是到发码才报"后端还没做到这些"。现在形状表里 `KindJson`
// 那一条固定存在（打印 nef.json.render、析构 nef.json.free），容器的类型字只认容器自己的
// 种类，与元素无关，所以这三处都放开了；键仍然按身份散列（与 class 同一条）。
//
// en: jsonplace_test.go covers Json entering containers, generic instances and class fields.
// Two places used to refuse it: a container position's shape and dismantling relied on a runtime
// type word that a Json node did not carry, and a class field holding Json only failed later, at
// emit time, with "the backend does not do this yet". The shape table now always has its KindJson
// row (printing nef.json.render, dismantling nef.json.free), and a container's type word names
// the container itself rather than its element, so all three places accept it; keys still hash by
// identity, the same rule as a class.
package check

import "testing"

func TestJsonPlacesAccepted(t *testing.T) {
	expectOK(t, "let xs:list<Json> = [json.parse(\"[]\")]\n")
	expectOK(t, "let d:dict<string, Json> = {\"甲\": json.parse(\"[]\")}\n")
	expectOK(t, "let s:hashset<Json> = {json.parse(\"[]\")}\n")
	expectOK(t, "class Holder {\n    j:Json\n}\n")
	expectOK(t, "class Box<T> {\n    v:T\n}\nlet b:Box<Json> = new Box<Json>(v: json.parse(\"1\"))\n")
}

// TestJsonAsKeyCompiles 复核树能当键写出来（比较按身份，运行期那一侧的覆盖在 e2e 里）。
func TestJsonAsKeyCompiles(t *testing.T) {
	expectOK(t, "let d:dict<Json, int> = {}\nlet k:Json = json.parse(\"1\")\nd.set(k, 1)\n")
	expectOK(t, "let s:hashset<Json> = {}\ns.add(json.parse(\"[1]\"))\n")
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// jsoncell_e2e_test.go 覆盖 `Json` 进容器、进 `class` 字段三件事：放得进、取得出、
// 覆盖与析构时不漏。树的形状表条目是 `KindJson`：打印走 nef.json.render，
// 析构走 nef.json.free（逐节点递归，文本节点、键表、值表一并归还）。
//
// en: jsoncell_e2e_test.go covers Json entering containers and class fields: it goes in, comes
// out, and neither overwriting nor dismantling leaks. The tree's shape row is KindJson: printing
// goes through nef.json.render and dismantling through nef.json.free, which walks every node
// and returns the text, the key table and the value table too.

package lower_test

import "testing"

func TestJsonInContainers(t *testing.T) {
	got, code := run(t, `
let xs:list<Json> = [json.parse("{\"a\":1}"), json.parse("[1,2]")]
console.print(length(xs), xs[0].kind(), xs[1].kind())
console.print(xs)

let d:dict<string, Json> = {"甲": json.parse("true")}
console.print(d["甲"].kind(), d["甲"].to_boolean(false))

let s:hashset<Json> = {}
s.add(json.parse("null"))
console.print(length(s))
`)
	want := "2 object array\n[{\"a\":1}, [1,2]]\nboolean true\n1\n"
	if got != want || code != 0 {
		t.Fatalf("Json 进容器不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

func TestJsonInClassField(t *testing.T) {
	got, code := run(t, `
class Holder {
    j:Json
}

let h:Holder = new Holder(j: json.parse("{\"a\":7}"))
console.print(h.j.kind(), h.j["a"].to_int(0))
h.j = json.parse("[3]")
console.print(h.j.kind(), length(h.j))
`)
	want := "object 7\narray 1\n"
	if got != want || code != 0 {
		t.Fatalf("Json 进 class 字段不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestJsonInContainersFrees 覆盖覆盖与析构：每一圈建一棵树放进容器、覆盖字段、
// 再让容器与对象一起走，峰值不随圈数增长。
func TestJsonInContainersFrees(t *testing.T) {
	if used := heapAt(t, `
class HolderJ {
    j:Json
}
for (i:int from 0 until 20000) {
    let xs:list<Json> = [json.parse("{\"a\":\"甲乙丙\"}")]
    xs[0] = json.parse("[1,2,3]")
    let d:dict<string, Json> = {"甲": json.parse("null")}
    d.set("甲", json.parse("true"))
    let h:HolderJ = new HolderJ(j: json.parse("{\"b\":\"丁\"}"))
    h.j = json.parse("[\"戊\"]")
}
`); used > 6000 {
		t.Fatalf("两万圈各换一次，峰值 %d 字节：旧的树大概还没人拆", used)
	}
}

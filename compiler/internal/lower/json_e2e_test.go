// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// json_e2e_test.go 是按真实产物验收的一份测试：把 json 这一组成员编成产物运行一遍。
//
// 三项检查各有来路：
//
//   - TestJSONCorpusThroughProgram 把 rtjson 的对照表写进一份文件，产物按行读入、
//     只使用 json 的十二条成员拼出一行答复，与参考实现的结果逐字比对。
//     实现与对照表任何一边有错，这一条都会红。
//   - TestJSONHeapSlope 验证归属规则：解析一棵树且无人接住时，走完这一句不留任何内存。
//   - TestJSONChecksRefuse 验证检查阶段就拒绝的几种写法（内建名字、盒子、length）。
package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neflang.org/nf/internal/rtjson"
)

// jsonSigProg 是一份只使用 json 十二条成员的 Nef 程序：把一棵树写成一行签名。
//
// 写它的用意是"只经公开的十二条取用"：签名函数访问不到节点内部，只能用 json.kind、
// json.count、json.keys、json.at、json.item、json.to_*——所以签名对得上，
// 就说明这十二条成员确实能完整描述这棵树。@PATH@ 由测试替换成一份写好的样例文件路径。
const jsonSigProg = `method signature(x:Json):string {
    let k:string = json.kind(x)
    if (k == "null") {
        return "null"
    }
    if (k == "boolean") {
        if (json.to_boolean(x, false)) {
            return "true"
        }
        return "false"
    }
    if (k == "number") {
        return "n" + number.to_string(json.to_integer(x, 0)) + "/" + number.to_string(json.to_number(x, 0.0))
    }
    if (k == "string") {
        return "s:" + json.to_text(x)
    }
    let n:int = json.count(x)
    let out:string = ""
    if (k == "array") {
        out = "a["
        for (i:int in 0 until n) {
            if (i > 0) {
                out = out + ","
            }
            out = out + signature(json.item(x, i))
        }
        return out + "]"
    }
    let keys:list<string> = json.keys(x)
    out = "o{"
    for (i:int in 0 until n) {
        if (i > 0) {
            out = out + ","
        }
        let key:string = keys[i]
        out = out + key + ":" + signature(json.at(x, key))
    }
    return out + "}"
}

let body:string = file.read("@PATH@")
let lines:list<string> = body.split("\n")
for (i:int in 0 until length(lines)) {
    let line:string = lines[i]
    let tree:Json = json.parse(line)
    if (tree == null) {
        console.print("坏了：" + json.explain(line))
    } else {
        console.print(signature(tree))
    }
}
`

func TestJSONCorpusThroughProgram(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "样例.jsonl")
	if err := os.WriteFile(src, []byte(rtjson.Text()), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	prog := strings.ReplaceAll(jsonSigProg, "@PATH@", filepath.ToSlash(src))
	got, code := run(t, prog)
	if code != 0 {
		t.Fatalf("码 %d，要 0。输出：\n%s", code, got)
	}
	want := rtjson.Answers()
	if strings.TrimSuffix(got, "\n") != want {
		g, w := strings.Split(strings.TrimSuffix(got, "\n"), "\n"),
			strings.Split(strings.TrimSuffix(want, "\n"), "\n")
		if len(g) != len(w) {
			t.Fatalf("行数对不上：产物 %d 行，答案卷 %d 行", len(g), len(w))
		}
		list := rtjson.Cases
		for i := range w {
			if g[i] != w[i] {
				t.Errorf("第 %d 条 %q：产物 %q，答案卷 %q", i, clip(list[i].In), clip(g[i]), clip(w[i]))
			}
		}
		t.FailNow()
	}
}

// clip 是给报错用的截断片段：几百层的签名整条写进测试输出会无法阅读。
func clip(s string) string {
	if len(s) > 70 {
		return s[:70] + "…"
	}
	return s
}

// TestJSONAccessors 验证十二条成员各自的输出，另加两项行为：打印整棵树、读走之后那个位置变空。
func TestJSONAccessors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"印一整棵树走原文的写法，count 数成员", `
let j:Json = json.parse("{ \"a\": [1, 2], \"b\": \"甲\" }")
console.print(j)
console.print(json.count(j))
`, "{\"a\":[1,2],\"b\":\"甲\"}\n2\n"},
		{"取走那一格就空了", `
let j:Json = json.parse("{ \"a\": 1 }")
let one:Json = json.at(j, "a")
console.print(json.kind(one), json.to_integer(one, -1))
console.print(json.at(j, "a"))
console.print(j)
`, "number 1\nnull\n{\"a\":null}\n"},
		{"没毛病那一句是空的，坏了那句说清行列", `
console.print(json.explain("[1, 2]"))
console.print(json.explain("[1,"))
`, "\n第 1 行第 4 列：这一片到这儿就断了，后头还差东西\n"},
		{"数组按下标取，越界给 null", `
let j:Json = json.parse("[10, 20, 30]")
console.print(json.count(j))
let mid:Json = json.item(j, 1)
console.print(json.kind(mid), json.to_integer(mid, 0))
console.print(json.item(j, 9) == null)
console.print(j)
`, "3\nnumber 20\ntrue\n[10,null,30]\n"},
		{"has、keys 与那三条带默认值的取法", `
let j:Json = json.parse("{ \"甲\": true, \"乙\": 2.5, \"丙\": \"喂\" }")
console.print(json.has(j, "甲"), json.has(j, "丁"))
let keys:list<string> = json.keys(j)
console.print(length(keys), keys[0], keys[2])
let v:Json = json.at(j, "乙")
console.print(json.to_number(v, 0.0), json.to_integer(v, -1))
console.print(json.to_text(json.at(j, "丙")))
console.print(json.to_text(j), json.to_boolean(json.at(j, "甲"), false))
`, "true false\n3 甲 丙\n2.5 -1\n喂\nnull true\n"},
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

// TestJSONHeapSlope 验证归属规则：解析一棵树且无人接住时，走完这一句一块内存也不留。
func TestJSONHeapSlope(t *testing.T) {
	cases := []struct {
		name, tmpl string
		want       float64
	}{
		{"拆一片没人接", `
let total:int = 0
for (i:int in 0 until @N@) {
    json.parse("{ \"甲\": [1, 2, 3], \"乙\": \"丙\", \"丁\": true }")
    total = total + 1
}
console.print(total)
`, 0},
		{"接住它的那个名字走完就还", `
let total:int = 0
for (i:int in 0 until @N@) {
    let j:Json = json.parse("[1, \"甲\", { \"k\": 2 }]")
    total = total + json.count(j)
}
console.print(total)
`, 0},
		{"一层层取出来也照还", `
let total:int = 0
for (i:int in 0 until @N@) {
    let j:Json = json.parse("{ \"a\": { \"b\": [1, 2] } }")
    let one:Json = json.at(j, "a")
    let two:Json = json.at(one, "b")
    total = total + json.count(two)
}
console.print(total)
`, 0},
		{"拆坏了的那一片也不留东西", `
let total:int = 0
for (i:int in 0 until @N@) {
    json.parse("{ \"a\": [1, 2, x")
    total = total + 1
}
console.print(total)
`, 0},
	}
	for _, c := range cases {
		if got := heapSlope(t, c.tmpl); got != c.want {
			t.Errorf("%s：每圈 %v 字节，要 %v", c.name, got, c.want)
		}
	}
}

// TestJSONParseDeep 验证"深度嵌套不另设上限"：五百层嵌套数组能解析，最深一层能数出来。
func TestJSONParseDeep(t *testing.T) {
	got, code := run(t, `
let deep:string = ""
for (i:int in 0 until 500) {
    deep = deep + "["
}
for (i:int in 0 until 500) {
    deep = deep + "]"
}
let j:Json = json.parse(deep)
console.print(json.kind(j), json.count(j))
let inmost:Json = j
while (json.count(inmost) == 1) {
    inmost = json.item(inmost, 0)
}
console.print(json.kind(inmost), json.count(inmost))
`)
	// 五百层每层只包含一个子层，所以最外层是 array 1，剥到尽头是空数组 array 0。
	if code != 0 || got != "array 1\narray 0\n" {
		t.Fatalf("五百层套娃读不动：码 %d，输出 %q", code, got)
	}
}

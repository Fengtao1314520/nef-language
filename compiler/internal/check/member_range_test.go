// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// member_range_test.go 覆盖三条的范围：谁能排序、keys 与 values 交得回什么。
// 两句都是整句比对（`expectErr` 认 `Error.Msg` 相等），改一个字就红。

func TestSortRefusesUnorderedElements(t *testing.T) {
	cases := []struct{ src, want string }{
		{"let b:list<boolean> = [true, false]\nb.sort()\n", "boolean 排不了先后：比大小只给 int、float、byte、unicode、string"},
		{"class C { i: int }\nlet cs:list<C> = []\ncs.sort()\n", "C 排不了先后：比大小只给 int、float、byte、unicode、string"},
		{"struct P { x: int }\nlet ps:list<P> = []\nps.sort()\n", "P 排不了先后：比大小只给 int、float、byte、unicode、string"},
		{"let xs:list<int> = [3, 1]\nxs.sort()\nlet bs:list<byte> = [3 as byte, 1 as byte]\nbs.sort()\nlet fs:list<float> = [3.0, 1.0]\nfs.sort()\nlet us:list<unicode> = []\nus.sort()\nlet ss:list<string> = []\nss.sort()\n", ""},
	}
	for _, c := range cases {
		if c.want == "" {
			expectOK(t, c.src)
			continue
		}
		expectErr(t, c.src, c.want)
	}
}

func TestDictEntriesRefuseHandles(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class C { i: int }\nlet d:dict<string, C> = {}\nconsole.print(d.values())\n", "C 交不出一份自己的拷贝：C 那一类东西一交就是两个持有者，只有值类型与文本交得回来"},
		{"class C { i: int }\nlet d:dict<C, int> = {}\nconsole.print(d.keys())\n", "C 交不出一份自己的拷贝：C 那一类东西一交就是两个持有者，只有值类型与文本交得回来"},
		{"struct P { x: int }\nlet d:dict<string, P> = {}\nconsole.print(d.values())\n", "P 交不出一份自己的拷贝：P 那一类东西一交就是两个持有者，只有值类型与文本交得回来"},
		{"let s:hashset<int> = {1}\nconsole.print(s.keys())\n", "hashset<int> 上没有 keys"},
		{"let l:list<int> = [1]\nconsole.print(l.keys())\n", "list<int> 上没有 keys"},
	}
	for _, c := range cases {
		expectErr(t, c.src, c.want)
	}
}

func TestDictEntriesAcceptValueTypesAndText(t *testing.T) {
	expectOK(t, `
let d:dict<string, int> = {"a": 1, "b": 2}
let ks:list<string> = d.keys()
let vs:list<int> = d.values()
ks.sort()
vs.sort()
let e:dict<int, string> = {1: "甲"}
console.print(length(e.keys()), length(e.values()))
console.print(e.keys().index_of(1), e.values().index_of("甲"))
`)
}

// 下一段覆盖：谁能取头尾、集合三条交得回什么、
// dict.get 那两个位置各按什么规则算（键只看一眼，默认值交出去）。

func TestSetOpsRefuseHandles(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class C { i: int }\nlet s:hashset<C> = {}\nlet t:hashset<C> = {}\nlet u:hashset<C> = s.union(t)\n", "C 交不出一份自己的拷贝：C 那一类东西一交就是两个持有者，只有值类型与文本交得回来"},
		{"struct P { x: int }\nlet s:hashset<P> = {}\nlet t:hashset<P> = {}\nconsole.print(s.intersect(t))\n", "P 不能当 hashset 的键：逐字段比、字段还能改，散列值守不住"},
		{"let l:list<int> = [1, 2]\nlet k:list<int> = [2]\nconsole.print(l.union(k))\n", "list<int> 上没有 union"},
	}
	for _, c := range cases {
		expectErr(t, c.src, c.want)
	}
	expectOK(t, `
let s:hashset<string> = {"甲"}
let t:hashset<int> = {1, 2}
let u:hashset<string> = s.difference(s)
let v:hashset<int> = t.intersect(t)
console.print(length(u), length(v))
`)
}

func TestHeadTailClearOnlyOnList(t *testing.T) {
	cases := []struct{ src, want string }{
		{"let s:hashset<int> = {1}\nconsole.print(s.first())\n", "hashset<int> 上没有 first"},
		{"let d:dict<string, int> = {}\nconsole.print(d.last())\n", "dict<string, int> 上没有 last"},
		{"let s:string = \"甲\"\ns.clear()\n", "string 上没有 clear"},
		{"let s:hashset<int> = {1}\ns.clear()\n", "hashset<int> 上没有 clear"},
	}
	for _, c := range cases {
		expectErr(t, c.src, c.want)
	}
	expectOK(t, `
let xs:list<string> = ["甲", "乙"]
console.print(xs.first(), xs.last())
xs.clear()
console.print(length(xs), xs.first())
`)
}

// TestDictGetOwnsItsArguments 键那个位置是只读借用（原名还能用），默认值那个位置是交出
// （用上了归调用方，没用上就地析构，两种情况下原名都不能再用）。
func TestDictGetOwnsItsArguments(t *testing.T) {
	expectErr(t, `
let d:dict<string, list<int>> = {}
let miss:list<int> = [9]
let got:list<int> = d.get("无", miss)
console.print(length(miss))
`, "miss 已经交出去了，这个名字不能再写")
	expectErr(t, `
let d:dict<string, int> = {}
console.print(d.get("无", null))
`, "这里要的是 int，给的是 null；类型之间不自动转换")
	expectOK(t, `
let d:dict<string, int> = {}
let s:string = "无"
console.print(d.get(s, 1), s)
`)
	expectErr(t, `
let d:dict<string, int> = {}
console.print(d.get("无"))
`, "get 收 2 个参数，这里给了 1 个")
	expectErr(t, `
let xs:list<int> = [1]
console.print(xs.get(0, 0))
`, "list<int> 上没有 get")
}

func TestPadAndFloatArities(t *testing.T) {
	expectErr(t, `console.print("甲".pad_start(3))`, "pad_start 收 2 个参数，这里给了 1 个")
	expectErr(t, `console.print("甲".pad_end("x", "x"))`, "这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, `console.print(text.to_float())`, "to_float 收 1 个参数，这里给了 0 个")
	expectErr(t, `console.print(math.pi(1))`, "pi 收 0 个参数，这里给了 1 个")
	expectOK(t, `
console.print("甲".pad_start(3, "○"), "甲".pad_end(3, "○"))
console.print(text.to_float("1.5"), math.pi(), math.sine(0.0), math.cosine(0.0), math.natural_log(1.0), math.random_float() >= 0.0)
`)
}

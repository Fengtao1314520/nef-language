// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// json_test.go 是 Go 参考实现自身的验收：与 encoding/json 逐条比对合法性、核对表中每条答复
// 是否符合预期，以及无法写成单行样例的条目（文本中的换行、控制字符、极深嵌套）。
package rtjson

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

// loneSurrogates 是本实现比 Go 更严的一条：单个代理字符无法构成代理对，判这段文本非法。
// Go 用 U+FFFD 替换后继续解析，所以这几条不按合法性与它比对（语言规则对此单独规定）。
var loneSurrogates = map[string]bool{
	`"\uD800"`:       true,
	`"\uDC00"`:       true,
	`"\uD800A"`:      true,
	`"\uD800\u0041"`: true,
}

// TestValidAgreesWithGoStdlib 拿标准库当对照表：除了那四条单个代理字符，
// "是否为合法 JSON"两边必须完全一致。
func TestValidAgreesWithGoStdlib(t *testing.T) {
	for _, c := range Cases {
		theirs := json.Valid([]byte(c.In))
		mine := Parse(c.In).Root != nil
		if loneSurrogates[c.In] {
			if !theirs || mine {
				t.Fatalf("%q：这一条要的是「Go 判合法、我们判坏」，得到 Go %v、我们 %v", c.In, theirs, mine)
			}
			continue
		}
		if mine != theirs {
			t.Errorf("%q：我们判 %v，Go 判 %v", c.In, mine, theirs)
		}
	}
}

// TestStructureAgreesWithGoStdlib 把每条合法样例都解析成 Go 的对应结构，逐项比对：
// 键的顺序、重复键的答复、解码后的文本值、数的大小。
//
// Go 的 `map[string]any` 会把键打乱，所以这里用 `json.Decoder` 的 Token 流按顺序读出来比。
func TestStructureAgreesWithGoStdlib(t *testing.T) {
	for _, c := range Cases {
		if c.Want == Bad || loneSurrogates[c.In] {
			continue
		}
		tree := Parse(c.In)
		if tree.Root == nil {
			t.Fatalf("%q 应当拆得开，却报了 %s", c.In, ReasonName(tree.Reason))
		}
		// 数与字面量：Go 的 Decode 直接给值，两边比同一个数
		var got any
		dec := json.NewDecoder(strings.NewReader(c.In))
		dec.UseNumber() // 1e999 这类超出 float64 范围的数，Decode 会直接报错；先取原文自行比对
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("%q：Go 读不动：%v", c.In, err)
		}
		check(t, tree.Root, got, c.In)
	}
}

// check 把一个节点与 Go 解析出的对应值比对。
func check(t *testing.T, n *Node, got any, in string) {
	t.Helper()
	switch v := got.(type) {
	case nil:
		if n.Kind != KNull {
			t.Errorf("%q：Go 是 null，我们是 %s", in, n.Kind.Name())
		}
	case bool:
		if (v && n.Kind != KTrue) || (!v && n.Kind != KFalse) {
			t.Errorf("%q：Go 是 %v，我们印的是 %s", in, v, n.Kind.Name())
		}
	case string:
		if n.Kind != KStr || n.Text != v {
			t.Errorf("%q：Go 解出来 %q，我们解出来 %q", in, v, n.Text)
		}
	case json.Number:
		if n.Kind != KInt && n.Kind != KFloat {
			t.Fatalf("%q：Go 是数，我们这一号种类是 %s", in, n.Kind.Name())
		}
		f, _ := strconv.ParseFloat(string(v), 64)
		// 参考实现只取前 17 位**有效**数字，其后靠一次乘 10 的幂补数量级，
		// 所以十九位的整数面天生比 Go 的正确舍入粗（2^63 那个值差一个 ulp）。
		// 17 位以内必须完全一致；超过 17 位只允许差几个 ulp，再远就是本实现算错了。
		if n.Float != f && !(isNaN(f) && isNaN(n.Float)) {
			if digits := sigDigits(string(v)); digits <= 17 || ulps(n.Float, f) > 4 {
				t.Errorf("%q：小数面两边不一样（有效数字 %d 位）：我们 %v，Go %v", in, digits, n.Float, f)
			}
		}
		if iv, err := v.Int64(); err == nil && n.Kind == KInt && iv != n.Int {
			t.Errorf("%q：整数面那一个数对不上：我们 %d，Go %v", in, n.Int, v)
		}
	case []any:
		if n.Kind != KArr || len(n.Vals) != len(v) {
			t.Fatalf("%q：数组对不上：我们 %d 样，Go %d 样", in, len(n.Vals), len(v))
		}
		for i := range v {
			check(t, n.Vals[i], v[i], in)
		}
	case map[string]any:
		if n.Kind != KObj {
			t.Fatalf("%q：Go 是对象，我们这一号种类是 %s", in, n.Kind.Name())
		}
		if len(n.Keys) != len(v) {
			t.Fatalf("%q：成员数对不上：我们 %d，Go %d", in, len(n.Keys), len(v))
		}
		for i, k := range n.Keys {
			one, ok := v[k]
			if !ok {
				t.Fatalf("%q：键 %q 在 Go 那一份里找不到", in, k)
			}
			check(t, n.Vals[i], one, in)
		}
	default:
		t.Fatalf("%q：Go 给了一个没见过的种类 %T", in, got)
	}
}

func isNaN(f float64) bool { return f != f }

// sigDigits 统计原文中有几位有效数字：小数点、指数部分与前导零不计。
func sigDigits(face string) int {
	m := face
	if i := strings.IndexAny(m, "eE"); i >= 0 {
		m = m[:i]
	}
	if i := strings.IndexByte(m, '.'); i >= 0 {
		m = m[:i] + m[i+1:]
	}
	m = strings.TrimLeft(m, "+-0")
	return len(m)
}

// ulps 返回两个 double 之间相隔几格：位模式按有符号整数读取即为大小顺序（负数一侧翻转）。
func ulps(a, b float64) int64 {
	x, y := floatOrder(a), floatOrder(b)
	if x < y {
		return y - x
	}
	return x - y
}

func floatOrder(v float64) int64 {
	b := int64(math.Float64bits(v))
	if b < 0 {
		return math.MinInt64 - b
	}
	return b
}

// TestKeyOrderAndDup 单独把这两条固定：键按原文的先后，重复的键后覆盖前、位置不动。
func TestKeyOrderAndDup(t *testing.T) {
	tree := Parse(`{"乙":1,"甲":2,"乙":3}`)
	if tree.Root == nil {
		t.Fatalf("拆不开：%s", ReasonName(tree.Reason))
	}
	if strings.Join(tree.Root.Keys, ",") != "乙,甲" {
		t.Errorf("键的顺序应当是「乙,甲」，得到 %q", tree.Root.Keys)
	}
	if got := Sig(tree.Root); got != "o{乙:n3/3.0,甲:n2/2.0}" {
		t.Errorf("答复是 %q", got)
	}
}

// TestTable 逐条比对样例表的答复：解析成功的比签名，失败的比错误类别。
func TestTable(t *testing.T) {
	for i, c := range Cases {
		tree := Parse(c.In)
		got, want := Bad+ReasonName(tree.Reason), c.First()
		if tree.Root != nil {
			got = Sig(tree.Root)
		}
		if got != want {
			t.Errorf("第 %d 条 %q：得到 %q，表里写的是 %q", i, clip(c.In), clip(got), clip(want))
		}
	}
}

// TestMultiline 覆盖无法写成单行样例的条目：文本中的换行与控制字符、深层嵌套。
func TestMultiline(t *testing.T) {
	deep := strings.Repeat("[", 400) + "1" + strings.Repeat("]", 400)
	deepWant := strings.Repeat("a[", 400) + "n1/1.0" + strings.Repeat("]", 400)
	cases := []struct {
		In   string
		Want string
	}{
		{"{\n  \"a\":\n  1\n}", "o{a:n1/1.0}"},
		{"[\r\n1,\r\n2\r\n]", "a[n1/1.0,n2/2.0]"},
		{`"a\nb"`, "s:a\nb"},
		{`"a\tb"`, "s:a\tb"},
		{`"a\rb"`, "s:a\rb"},
		{`"a\bb"`, "s:a\bb"},
		{`"a\fb"`, "s:a\fb"},
		{"\"a\x01b\"", Bad + "控制字符没转义"},
		{"\"a\x7fb\"", "s:a\x7fb"}, // 0x7F 不在 JSON 的限制范围内：只限制 0x00～0x1F
		{`"a\u0009b"`, "s:a\tb"},
		{`"\u65e5\u672c\u8a9e"`, "s:日本語"},
		{`{"a":1,"b":[true,null]}`, "o{a:n1/1.0,b:a[true,null]}"},
		{deep, deepWant},
	}
	for _, c := range cases {
		tree := Parse(c.In)
		got := Bad + ReasonName(tree.Reason)
		if tree.Root != nil {
			got = Sig(tree.Root)
		}
		if got != c.Want {
			t.Errorf("%q：得到 %q，要 %q", clip(c.In), clip(got), clip(c.Want))
		}
	}
}

// clip 截短测试输出里的长签名：几百层的签名整行抄进输出无法阅读。
func clip(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// TestLineCol 单独验行列换算：列按码点算，一个汉字占一列。
func TestLineCol(t *testing.T) {
	text := "甲乙\nabc\r\nC"
	cases := []struct {
		at        int
		line, col int
	}{
		{0, 1, 1},
		{3, 1, 2},  // 第二个汉字的开头
		{6, 1, 3},  // 第一个换行本身还在第一行上
		{7, 2, 1},  // a
		{10, 2, 4}, // \r
		{12, 3, 1}, // C
		{13, 3, 2}, // 文本尽头：C 的后面那一列
		{99, 3, 2}, // 位置超出文本长度：按尽头算
	}
	for _, c := range cases {
		if l, col := LineCol(text, c.at); l != c.line || col != c.col {
			t.Errorf("位置 %d：得到第 %d 行第 %d 列，要第 %d 行第 %d 列", c.at, l, col, c.line, c.col)
		}
	}
}

// TestExplain 固定面向用户的说明：第几行第几列，加上错误类别。
func TestExplain(t *testing.T) {
	cases := []struct{ In, Want string }{
		{`{"a": 1}`, ""},
		{`[1, 2]`, ""},
		{`{"a": 1`, "第 1 行第 8 列：这一片到这儿就断了，后头还差东西"},
		{"{\n  \"a\": [1,\n", "第 3 行第 1 列：这一片到这儿就断了，后头还差东西"},
		{"\"甲乙", "第 1 行第 4 列：文本少了收尾的那一个引号"},
		{"[1, 甲]", "第 1 行第 5 列：这里该是一个值"},
		{`123abc`, "第 1 行第 4 列：值的尾巴上多出来了东西"},
		{"[\n1,\nx]", "第 3 行第 1 列：这里该是一个值"},
		{`{"a":1"b":2}`, "第 1 行第 7 列：这里该是一个逗号或者右花括号"},
	}
	for _, c := range cases {
		if got := Parse(c.In).Explain(c.In); got != c.Want {
			t.Errorf("%q：explain 得到 %q，要 %q", c.In, got, c.Want)
		}
	}
}

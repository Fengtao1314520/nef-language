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

// corpus.go 是那张双向比对的表：一份表，三处用。
//
// `rtjson` 的测试用它校验 Go 这一版；`internal/lower` 的实际运行测试把它写成一个文件，
// 让产物逐行读出签名，与这张表逐字比对；`json.explain` 的说法也在这儿固定。
//
// 表中每一行都不含换行——写出的文件按行切分就是这些样例。无法写成单行样例的内容
// （文本中的换行、控制字符）另放在 json_test.go 的小表中。
//
// en: corpus.go is the table used by the two-way comparison: one table, three uses. The rtjson
// tests use it to check the Go version; the actual-run test in internal/lower writes it out as
// a file and has the product read one line per case and print a signature, compared word for
// word against this table; and the wording of json.explain is fixed here too. Every row in the
// table contains no newline - the file written out is exactly these entries, split by line.
// Things that cannot be expressed as a one-line sample, such as newlines and control characters
// inside text, are kept in a separate small table in json_test.go.
package rtjson

import "strings"

// Case 是一条样例：In 是待解析的文本，Want 是期望的答复。
//
// Want 有两种写法：`a[...]`、`o{...}` 形式的一行签名（完全展开），或者 `坏了：<错误类别>`。
//
// en: Case is one sample: in is the text to read, want is the answer it should produce. want
// has two spellings: a one-line signature such as `a[...]` or `o{...}` (fully expanded), or
// `坏了:<which kind of error>`.
type Case struct {
	In   string
	Want string
	Why  Reason // want 是"坏了"的时候填对应的原因编号
}

// Bad 是错误答复的前缀。
//
// en: Bad is the answer prefix for a failed case.
const Bad = "坏了："

// Cases 是样例表。其顺序就是双向比对的顺序，两份测试都按它执行。
//
// en: Cases is that table. Its order is the order of the two-way comparison, and both tests
// follow it.
var Cases = []Case{
	// 三个整词字面量
	{`null`, `null`, RNone},
	{`true`, `true`, RNone},
	{`false`, `false`, RNone},
	{`  true  `, `true`, RNone},
	{"\ttrue", `true`, RNone},

	// 数：整数面
	{`0`, `n0/0.0`, RNone},
	{`-0`, `n0/0.0`, RNone},
	{`3`, `n3/3.0`, RNone},
	{`-12`, `n-12/-12.0`, RNone},
	{`2026`, `n2026/2026.0`, RNone},

	// 数：小数面与指数
	{`3.5`, `n0/3.5`, RNone},
	{`-0.5`, `n0/-0.5`, RNone},
	{`0.1`, `n0/0.1`, RNone},
	{`1.5e-3`, `n0/0.0015`, RNone},
	{`1e3`, `n0/1000.0`, RNone},
	{`1E+3`, `n0/1000.0`, RNone},
	{`0.0`, `n0/0.0`, RNone},

	// 数：int64 的容纳边界与装不进的几条（退化为小数面）
	{`9223372036854775807`, `n9223372036854775807/9223372036854776000.0`, RNone},
	{`-9223372036854775808`, `n-9223372036854775808/-9223372036854776000.0`, RNone},
	{`9223372036854775808`, `n0/9223372036854778000.0`, RNone},
	{`-9223372036854775809`, `n0/-9223372036854778000.0`, RNone},
	{`1234567890123456789012`, `n0/1234567890123456800000.0`, RNone},
	{`1e999`, `n0/Inf`, RNone},
	{`-1e999`, `n0/-Inf`, RNone},

	// 数的写法不合法的几条
	{`01`, Bad, RNumber},
	{`00`, Bad, RNumber},
	{`+1`, Bad, RValue},
	{`.5`, Bad, RValue},
	{`1.`, Bad, RNumber},
	{`1e`, Bad, RNumber},
	{`1e+`, Bad, RNumber},
	{`-`, Bad, RTruncated},
	{`123abc`, Bad, RJunk},

	// 文本：直写的与九种转义
	{`""`, `s:`, RNone},
	{`"甲"`, `s:甲`, RNone},
	{`"甲乙丙"`, `s:甲乙丙`, RNone},
	{"\"a\\\"b\"", `s:a"b`, RNone},
	{"\"a\\\\b\"", `s:a\b`, RNone},
	{"\"a\\/b\"", `s:a/b`, RNone},
	{`"\u0041"`, `s:A`, RNone},
	{`"\u00e9"`, `s:é`, RNone},
	{`"\u004A\u006B"`, `s:Jk`, RNone},
	{`"\uD83D\uDE00"`, `s:😀`, RNone},
	{`"\uFFFF"`, "s:\uFFFF", RNone},

	// 文本：非法转义
	{`"abc`, Bad, RStrClose},
	{`"\x41"`, Bad, REScape},
	{`"\u12"`, Bad, RHex},
	{`"\uZZZZ"`, Bad, RHex},
	{`"\u00"`, Bad, RHex},
	{`"\uD800"`, Bad, RSurrogate},
	{`"\uDC00"`, Bad, RSurrogate},
	{`"\uD800A"`, Bad, RSurrogate},
	{`"\uD800\u0041"`, Bad, RSurrogate},
	{`"\`, Bad, RTruncated},

	// 数组
	{`[]`, `a[]`, RNone},
	{`[ ]`, `a[]`, RNone},
	{`[1,2,3]`, `a[n1/1.0,n2/2.0,n3/3.0]`, RNone},
	{`[null,true,false]`, `a[null,true,false]`, RNone},
	{`[[1],[2]]`, `a[a[n1/1.0],a[n2/2.0]]`, RNone},
	{`[[[[[1]]]]]`, `a[a[a[a[a[n1/1.0]]]]]`, RNone},
	{`["甲","乙"]`, `a[s:甲,s:乙]`, RNone},
	{`[1,]`, Bad, RValue},
	{`[1 2]`, Bad, RArrClose},
	{`[1`, Bad, RTruncated},
	{`[`, Bad, RTruncated},
	{`]`, Bad, RValue},
	{`[1,2] extra`, Bad, RJunk},

	// 对象：顺序、重复键、空对象
	{`{}`, `o{}`, RNone},
	{`{ }`, `o{}`, RNone},
	{`{"a":1}`, `o{a:n1/1.0}`, RNone},
	{`{"a" : 1 }`, `o{a:n1/1.0}`, RNone},
	{`{"":1}`, `o{:n1/1.0}`, RNone},
	{`{"b":1,"a":2}`, `o{b:n1/1.0,a:n2/2.0}`, RNone},
	{`{"a":1,"a":2}`, `o{a:n2/2.0}`, RNone},
	{`{"a":1,"b":2,"a":3}`, `o{a:n3/3.0,b:n2/2.0}`, RNone},
	{`{"甲":"乙"}`, `o{甲:s:乙}`, RNone},
	{`{"k":{}}`, `o{k:o{}}`, RNone},
	{`{"a":[1,{"b":null}]}`, `o{a:a[n1/1.0,o{b:null}]}`, RNone},
	{`{"a":{"b":{"c":{"d":1}}}}`, `o{a:o{b:o{c:o{d:n1/1.0}}}}`, RNone},
	{`{a:1}`, Bad, RKeyQuote},
	{`{"a"1}`, Bad, RColon},
	{`{"a":}`, Bad, RValue},
	{`{"a":1,}`, Bad, RKeyQuote},
	{`{"a":1`, Bad, RTruncated},
	{`{`, Bad, RTruncated},
	{`{,}`, Bad, RKeyQuote},
	{`{"a":1"b":2}`, Bad, RObjClose},

	// 一份完整的配置样例：组合上述各类写法
	{
		`{"name":"nef","version":1,"tags":["语言","编译器"],` +
			`"server":{"host":"127.0.0.1","port":8080,"tls":false,"limit":null},"ratio":0.85}`,
		`o{name:s:nef,version:n1/1.0,tags:a[s:语言,s:编译器],` +
			`server:o{host:s:127.0.0.1,port:n8080/8080.0,tls:false,limit:null},ratio:n0/0.85}`,
		RNone,
	},

	// 空文本与只含空白的文本
	{``, Bad, RTruncated},
	{`   `, Bad, RTruncated},

	// 大小写不符与不合法的单词字面量
	{`nul`, Bad, RNullLit},
	{`Null`, Bad, RValue},
	{`TRUE`, Bad, RValue},
	{`tru`, Bad, RTrueLit},
	{`fals`, Bad, RFalseLit},
	{`NaN`, Bad, RValue},
	{`Infinity`, Bad, RValue},
	{`infinity`, Bad, RValue},
}

// First 返回第 i 条样例期望的那一行答复。
//
// en: First returns the answer line case i should produce.
func (c Case) First() string {
	if c.Want == Bad {
		return Bad + ReasonName(c.Why)
	}
	return c.Want
}

// Text 把整张表的样例写成一个文件的内容：一行一条，顺序不变，末尾不留空行
// （`split` 按换行切分，多一个换行会多出一条空样例）。
//
// en: Text writes every sample of the whole table as the content of one file: one per line, in
// the same order, with no trailing blank line (`split` cuts on newlines, so one extra newline
// would add an empty sample).
func Text() string {
	list := make([]string, len(Cases))
	for i, c := range Cases {
		list[i] = c.In
	}
	return strings.Join(list, "\n")
}

// Wants 返回整张表期望的答复，同样一行一条、末尾不留空行。
//
// 答复取 First 的形式（错误样例只到错误类别为止）：供参考实现自己的测试比对。
//
// en: Wants returns the expected answers for the whole table, again one per line with no
// trailing blank line. The answers are the First form (a failed case goes only as far as "which
// kind of error"): this is what the reference implementation's own test compares against.
func Wants() string {
	list := make([]string, len(Cases))
	for i, c := range Cases {
		list[i] = c.First()
	}
	return strings.Join(list, "\n")
}

// Answers 与 Wants 用同一张表，只是错误样例带上"第几行第几列"与面向用户的说明——
// 实际运行的那份测试比对的是这一种（产物只通过 json 的十二个成员输出，给不出编号）。
//
// en: Answers uses the same table as Wants, except that a failed case carries "line n, column
// m" plus the user-facing message. The actual-run test compares against this form, because the
// product only speaks through the twelve json members and cannot name a reason code.
func Answers() string {
	list := make([]string, len(Cases))
	for i, c := range Cases {
		list[i] = Answer(c.In)
	}
	return strings.Join(list, "\n")
}

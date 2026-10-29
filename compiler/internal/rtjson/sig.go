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

// sig.go 把一棵树写成一行，只用于双向比对。
//
// Nef 侧的签名函数（`internal/lower/json_e2e_test.go` 里那段实际运行的程序）用 json 的十二个
// 成员拼出同样的一行。两边逐字一致，才能确认机器码那一版真的把文本解析成了这一棵树。
//
// en: sig.go writes a whole tree as one line, purely for the two-way comparison. The nef-side
// signature function (the program that runs for real in internal/lower/json_e2e_test.go) builds
// the same line out of the twelve json members. Only when the two agree character for character
// can we say the machine-code version really parsed the text into this tree.
package rtjson

import (
	"strconv"
	"strings"

	"neflang.org/nf/internal/rtfloat"
)

// Sig 返回那一行写法：
//
//	null true false
//	n3/3.0                  一个数：整数面的值与小数面的值，未使用的一面写 0
//	s:甲                    一段文本，解过码的
//	a[1,2]  o{x:1,y:2}      数组与对象，对象的键按原文的先后
//
// en: Sig returns the one-line spelling: null and true and false stand alone; n3/3.0 is one
// number, integer-face value then float-face value, with 0 written where a face is unused; s:甲
// is a piece of text, already decoded; a[1,2] and o{x:1,y:2} are an array and an object, the
// object's keys in the order written.
func Sig(n *Node) string {
	if n == nil {
		return "坏"
	}
	switch n.Kind {
	case KNull:
		return "null"
	case KTrue:
		return "true"
	case KFalse:
		return "false"
	case KInt:
		return "n" + strconv.FormatInt(n.Int, 10) + "/" + rtfloat.Shortest(n.Float)
	case KFloat:
		return "n0/" + rtfloat.Shortest(n.Float)
	case KStr:
		return "s:" + n.Text
	case KArr:
		var b strings.Builder
		b.WriteString("a[")
		for i, v := range n.Vals {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(Sig(v))
		}
		return b.String() + "]"
	case KObj:
		var b strings.Builder
		b.WriteString("o{")
		for i, k := range n.Keys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(k)
			b.WriteString(":")
			b.WriteString(Sig(n.Vals[i]))
		}
		return b.String() + "}"
	}
	return "坏"
}

// Answer 是一条样例期望的那一行答复：解析成功给签名，失败给"坏了："加上面向用户的说明。
//
// 实际运行的那份测试（`internal/lower/json_e2e_test.go`）逐行比对的就是这一行——它把样例表
// 写成一个文件，产物按行读入，每行给一个答复。
//
// en: Answer is the answer line a sample should produce: a signature when it parses, "坏了:" plus
// the user-facing message when it does not. The actual-run test (internal/lower/json_e2e_test.go)
// compares line against exactly this line - it writes that table into a file and has the
// product read it in, one answer per line.
func Answer(text string) string {
	t := Parse(text)
	if t.Root != nil {
		return Sig(t.Root)
	}
	return Bad + t.Explain(text)
}

// ReasonName 是错误类别的名字，只给测试看：表里写 RValue 比写一串中文更易读。
//
// en: ReasonName is the name of one error kind, for the tests only: writing RValue in the table
// is easier to read than a run of Chinese.
func ReasonName(r Reason) string {
	names := [...]string{
		"没毛病", "断了", "该是一个值", "对象少逗号或花括号", "该是引号起头的键",
		"少冒号", "数组少逗号或方括号", "少收尾的引号", "控制字符没转义",
		"转义用不了", "u 后面不是四个十六进制", "半个代理字符", "数的写法不合规矩",
		"null 拼错", "true 拼错", "false 拼错", "尾巴上多出来东西",
	}
	if r <= RNone || int(r) >= len(names) {
		return "没这一号"
	}
	return names[r]
}

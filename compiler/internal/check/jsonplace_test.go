// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// jsonplace_test.go 覆盖 `Json` 进不了容器与 `class` 字段这一条补充规则。
//
// 两条都是实测确认的：`list<Json>` 先前过了检查也过了发码，打印出来是 `[null, null]`
// （容器那个位置的形状与析构都要靠运行期类型字，而 `Json` 头上没有那一个号），
// `class` 的字段放 `Json` 则是到发码才报"后端还没做到这些"。静默给出错误结果比报错更坏，
// 所以两处都提前到检查阶段拒绝，与其余提前拒绝的写法是同一种做法。
package check

import "testing"

func TestJsonPlacesRefused(t *testing.T) {
	expectErr(t, "let xs:list<Json> = [json.parse(\"[]\")]\n", "Json 进不了 list：先把它拆成自己要的值，或者存那段文本")
	expectErr(t, "let d:dict<string, Json> = {\"甲\": json.parse(\"[]\")}\n", "Json 进不了 dict：先把它拆成自己要的值，或者存那段文本")
	expectErr(t, "let s:hashset<Json> = {json.parse(\"[]\")}\n", "Json 进不了 hashset：先把它拆成自己要的值，或者存那段文本")
	expectErr(t, "class Holder {\n    j:Json\n}\n", "Json 只能待在名字、实参与返回值里，不能当字段")
	// 当键也走上面那一条（`dict<Json, int>` 里的 `Json` 先撞上"进不了 dict"）：
	// `keyReason` 那张表只覆盖值类型与容器，`Json` 在这一侧就被拒绝了。
	expectErr(t, "let d:dict<Json, int> = {json.parse(\"[]\"): 1}\n", "Json 进不了 dict：先把它拆成自己要的值，或者存那段文本")
}

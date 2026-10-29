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

// params.go 保存内建动作的参数名，用于向人展示签名。
//
// 语言里内建的参数没有名字，这门语言也没有具名实参：实参一律按位置交出。检查、生成代码
// 与归属判定都不读这一张表，它只服务编辑器的签名栏与文档里的那一行写法。
// 名字与 stdFuncs、stdMember 两张表的对应关系由 params_test.go 校验：每一条动作的每一个参数
// 都要在这里有一个名字，多一个或少一个都判失败——补全与悬停显示的签名由这一张表拼接。
//
// 命名沿用主流语言：小写下划线，取自各条动作的说明；容器里的元素类型说不清具体是哪一样时
// 写 T、K、V，不写省略号；变长参数写成 (...values) 那种形状。
//
// en: params.go holds the human-readable parameter names of the built-in actions. Built-in
// parameters have no names in the language itself and the language has no named arguments:
// arguments go purely by position. Checking, code generation and the ownership analysis never
// read this table; it exists only for the editor's signature column and the notation line in
// the documentation. Its alignment with the stdFuncs and stdMember tables is pinned by
// params_test.go: every parameter of every action must have a name here, one missing or one
// extra fails the test - the signatures shown by completion and hover are assembled from this
// table. The naming follows mainstream languages: lowercase with underscores, taken from each
// action's description; when a container's element type cannot be named concretely, write T, K
// or V rather than an ellipsis; a variadic parameter is written (...values).
package check

import "strings"

// stdParamNames 是每一条内建动作的参数名，键是落点名：组函数是"组名.函数名"，
// 容器与文本的成员是"形状.名字"（`list.add`、`string.slice`）。
//
// en: stdParamNames is the parameter names of every built-in action, keyed by dotted name: a
// group function is "group.function", a member of a container or of text is "shape.name"
// (`list.add`, `string.slice`).
var stdParamNames = map[string][]string{
	// 屏幕输出与输入
	"console.print":       {"values"},
	"console.print_error": {"values"},
	"console.read":        {},

	// 文本与数字
	"text.to_int":              {"text"},
	"text.to_int_in_base":      {"text", "base"},
	"text.to_float":            {"text"},
	"text.join":                {"parts", "separator"},
	"text.to_base64":           {"text"},
	"text.from_base64":         {"encoded"},
	"text.escape_json":         {"text"},
	"text.from_bytes":          {"data"},
	"number.to_string":         {"value"},
	"number.to_string_in_base": {"value", "base"},
	"length":                   {"value"},

	// 数学
	"math.absolute_value": {"number"},
	"math.arctangent":     {"x"},
	"math.ceiling":        {"x"},
	"math.cosine":         {"x"},
	"math.exponential":    {"x"},
	"math.floor":          {"x"},
	"math.log_base_10":    {"x"},
	"math.maximum":        {"a", "b"},
	"math.minimum":        {"a", "b"},
	"math.natural_log":    {"x"},
	"math.pi":             {},
	"math.power":          {"base", "exponent"},
	"math.random":         {"low", "high"},
	"math.random_float":   {},
	"math.round":          {"x"},
	"math.set_seed":       {"seed"},
	"math.sign":           {"number"},
	"math.sine":           {"x"},
	"math.square_root":    {"x"},
	"math.tangent":        {"x"},

	// 时间与日历
	"time.calendar":   {"seconds"},
	"time.elapsed":    {},
	"time.now":        {},
	"time.sleep":      {"milliseconds"},
	"time.utc_offset": {},

	// 文件
	"file.append":             {"path", "text"},
	"file.copy":               {"source", "target"},
	"file.create_directories": {"path"},
	"file.create_directory":   {"path"},
	"file.exists":             {"path"},
	"file.is_directory":       {"path"},
	"file.list":               {"dir"},
	"file.list_paths":         {"dir"},
	"file.modified":           {"path"},
	"file.read":               {"path"},
	"file.read_bytes":         {"path"},
	"file.read_from":          {"path", "at", "count"},
	"file.readonly":           {"path"},
	"file.remove":             {"path"},
	"file.remove_directory":   {"path"},
	"file.remove_tree":        {"root"},
	"file.rename":             {"source", "target"},
	"file.size":               {"path"},
	"file.walk_paths":         {"root"},
	"file.write":              {"path", "text"},
	"file.write_at":           {"path", "at", "text"},
	"file.write_bytes":        {"path", "data"},

	// 路径
	"path.directory_name": {"path"},
	"path.exists":         {"path"},
	"path.extension":      {"path"},
	"path.file_name":      {"path"},
	"path.is_absolute":    {"path"},
	"path.join":           {"dir", "name"},

	// 环境变量与本进程
	"environment.current_directory":     {},
	"environment.get":                   {"name"},
	"environment.set":                   {"name", "value"},
	"environment.set_current_directory": {"path"},
	"environment.unset":                 {"name"},

	// 正则
	"regular_expression.find":    {"text", "pattern"},
	"regular_expression.match":   {"text", "pattern"},
	"regular_expression.replace": {"text", "pattern", "replacement"},
	"regular_expression.split":   {"text", "pattern"},

	// JSON
	"json.at":         {"doc", "key"},
	"json.count":      {"doc"},
	"json.explain":    {"text"},
	"json.has":        {"doc", "key"},
	"json.item":       {"doc", "index"},
	"json.keys":       {"doc"},
	"json.kind":       {"doc"},
	"json.parse":      {"text"},
	"json.to_boolean": {"doc", "fallback"},
	"json.to_integer": {"doc", "fallback"},
	"json.to_number":  {"doc", "fallback"},
	"json.to_text":    {"doc"},

	// 字节块
	"bytes.from_base64": {"encoded"},
	"bytes.join":        {"parts"},
	"bytes.new":         {"size"},

	// 网络
	"net.connect": {"host", "port", "timeout_ms"},
	"net.listen":  {"port"},
	"http.get":    {"url", "timeout_ms"},
	"http.post":   {"url", "body", "timeout_ms"},

	// 起进程那一档
	"process.start": {"command_line"},

	// 挂在容器、文本与字节块上的成员
	"list.add":       {"value"},
	"list.add_all":   {"other"},
	"list.clear":     {},
	"list.first":     {},
	"list.index_of":  {"value"},
	"list.insert_at": {"index", "value"},
	"list.last":      {},
	"list.remove":    {"value"},
	"list.remove_at": {"index"},
	"list.reverse":   {},
	"list.slice":     {"start", "end"},
	"list.sort":      {},
	"list.to_set":    {},

	"dict.add":      {"key", "value"},
	"dict.contains": {"key"},
	"dict.get":      {"key", "fallback"},
	"dict.keys":     {},
	"dict.remove":   {"key"},
	"dict.values":   {},

	"hashset.add":        {"value"},
	"hashset.contains":   {"value"},
	"hashset.difference": {"other"},
	"hashset.intersect":  {"other"},
	"hashset.remove":     {"value"},
	"hashset.union":      {"other"},

	"string.contains":      {"needle"},
	"string.ends_with":     {"suffix"},
	"string.index_of":      {"needle"},
	"string.last_index_of": {"needle"},
	"string.pad_end":       {"width", "padding"},
	"string.pad_start":     {"width", "padding"},
	"string.repeat":        {"count"},
	"string.replace":       {"from", "to"},
	"string.slice":         {"start", "end"},
	"string.split":         {"separator"},
	"string.starts_with":   {"prefix"},
	"string.to_bytes":      {},
	"string.to_list":       {},
	"string.to_lower":      {},
	"string.to_upper":      {},
	"string.trim":          {},
	"string.trim_end":      {},
	"string.trim_start":    {},

	"bytes.slice":     {"start", "end"},
	"bytes.to_base64": {},

	"socket.send":      {"data"},
	"socket.recv_into": {"buffer"},
	"socket.close":     {},

	"listener.accept": {},
	"listener.close":  {},

	"process.wait":  {},
	"process.close": {},
}

// stdParamName 返回一条动作第 i 个参数的展示名；这张表没有登记的返回空串，
// 签名随之退回只写类型。
//
// en: stdParamName returns the readable name of the i-th parameter of one action; anything not
// registered in this table comes back empty and the signature falls back to writing types only.
func stdParamName(dotName string, i int) string {
	names, ok := stdParamNames[dotName]
	if !ok || i >= len(names) {
		return ""
	}
	return names[i]
}

// placeholderType 是元素类型此刻无法确定时的写法：`list.add(value: T)`、
// `dict.get(key: K, fallback: V): V`。T／K／V 是主流语言文档的通用记号，
// 相比省略号，它能直接表明该位置随接收者自身的类型确定。
//
// en: placeholderType is the spelling used when the element type cannot be named at this
// moment: `list.add(value: T)`, `dict.get(key: K, fallback: V): V`. T/K/V are the conventional
// notation in mainstream language documentation, and better than an ellipsis because they say
// directly that this slot follows the receiver's own type.
func placeholderType(group string, i int) Type {
	name := "T"
	switch {
	case group == "dict" && i == 1:
		name = "V"
	case group == "dict":
		name = "K"
	}
	return Type{Kind: Struct, Name: name}
}

// sigText 把一行签名写成 `名字(甲: 整型, 乙: 小数): 布尔` 那种样子——但用的是编程语言的写法：
// 参数名与类型用冒号隔开，多个参数用逗号，变长参数前面那三个点是 `(...values)`。
//
// 名字有两个来源：自己写的函数与方法参数本来就带名字，内建那一条到 params.go
// 那一张表里问。类型说不清的（`length` 收三种容器、`math.minimum` 跟着实参走）只写名字：
// 写 `any` 或 `object` 与实际不符，nf 里没有那种参数类型。
// 返回值是 NoRet 或说不清的，不写后面那一段：`print(...values): ` 留一个空冒号同样与实际不符。
//
// en: sigText writes a signature line in the form `name(a: int, b: float): bool` - using
// programming-language notation: parameter name and type separated by a colon, parameters by
// commas, and a variadic tail written as `(...values)`. Names come from two places: user
// functions and methods carry their parameter names already, built-ins ask the table in
// params.go. Where the type cannot be stated (`length` taking three container kinds,
// `math.minimum` following its argument) only the name is written - putting an `any` or
// `object` there would not match the language, since nf has no such parameter type. A return
// that is NoRet or unstated drops the trailing part entirely: `print(...values): ` with an
// empty colon would not match the language either.
func SigText(head, key string, params []Param, ret Type, vararg bool) string {
	var b strings.Builder
	b.WriteString(head)
	b.WriteByte('(')
	switch {
	case vararg:
		b.WriteString("...")
		if len(params) > 0 && params[0].Name != "" {
			b.WriteString(params[0].Name)
		} else {
			b.WriteString(stdParamName(key, 0))
		}
	default:
		parts := make([]string, 0, len(params))
		for i, p := range params {
			name := p.Name
			if name == "" {
				name = stdParamName(key, i)
			}
			text := ""
			if p.Typ.Kind != ErrT {
				text = p.Typ.String()
			}
			// 只读借用在签名里照写：编辑器显示的那一行要与源码一致。
			head := ""
			if p.takesRef() {
				head = "ref "
			}
			switch {
			case name != "" && text != "":
				parts = append(parts, head+name+": "+text)
			case name != "":
				parts = append(parts, head+name)
			default:
				parts = append(parts, head+text)
			}
		}
		b.WriteString(strings.Join(parts, ", "))
	}
	b.WriteByte(')')
	if ret.Kind != NoRet && ret.Kind != ErrT {
		b.WriteString(": " + ret.String())
	}
	return b.String()
}

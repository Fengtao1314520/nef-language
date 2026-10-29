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

// complete.go 回答编辑器要问的几件事：点号后面能补出什么、光标停在一个名字上时那一行签名是什么。
//
// 这里不另立一份成员名单。某一种类型有哪些成员，答复全部来自检查器自己用的那几张表：
// 容器、文本、字节块、套接字、监听句柄与进程句柄问 `stdMember`（它认不出时返回零值那一个），
// 标准库的组问 `stdFuncs`，自己写的类、结构体与枚举问 `Facts` 那几张表，包问 `Facts.Sigs`
// 与 `Facts.Funcs` 里带包根的那一些键。所以补出来的名字与编译接受的名字出自同一份依据。
//
// 唯一的例外是 `stdMemberNames`：它只是候选名的清单，某一种类型上到底有没有这个名字
// 仍由 `stdMember` 判定。清单与 `stdMember` 里那些 case 的对应关系由 `complete_test.go`
// 读源码逐项比对——新增一条成员却漏登清单，那条测试立即判失败。
//
// en: complete.go answers the questions the editor asks: what may follow a dot, and what the
// signature line says when the cursor rests on a name. This file keeps no separate member list.
// Which members a given type has is answered entirely by the tables the checker itself uses:
// containers, text, byte blocks, sockets, listeners and process handles ask stdMember (which
// returns the zero member when it does not know the name), standard-library groups ask stdFuncs,
// hand-written classes, structs and enums ask the Facts tables, and packages ask the keys of
// Facts.Sigs and Facts.Funcs that carry a package root. So a completed name rests on the same
// ledger the compiler uses. The only exception is stdMemberNames: it is a list of candidate
// names only, and whether a given type actually has one is still decided by stdMember. Keeping
// that list in step with stdMember's cases is checked by complete_test.go, which reads the
// source and compares item by item - add a member and omit it from the list, and that test goes
// red immediately.
package check

import (
	"sort"
	"strconv"
	"strings"
)

// Member 是补全里的一条。
//
// en: Member is one completion entry.
type Member struct {
	Name   string
	Detail string // 补全列表右侧与悬停显示的一行，例：`trim(): string`、`count: int`
	// Field 为真表示这一条是字段，不是函数或方法；类型名（要 new 才造得出值）也算在这一档里。
	Field bool
	// Doc 是"这一条的说明写在哪个节点上面"——包交出来的数走这一个（函数与方法那一半由
	// `Facts.Funcs` 交回同一个节点）。字段、枚举成员与内建那一些没有这个位置，留空。
	Doc any
}

// stdMemberNames 是容器、文本、字节块、套接字、监听句柄、进程句柄这几类值上可能出现的全部成员名。
// 每一种实际有哪些成员不写在这里，由 stdMember 逐条问出来（`contains` 在 list 上就没有）。
//
// en: stdMemberNames is every member name that could appear on the containers, text, byte
// blocks, sockets, listeners and process handles. Which ones each of them really has is not
// written here - stdMember answers that one at a time (`contains` does not exist on a list).
var stdMemberNames = []string{
	"accept", "add", "add_all", "clear", "close", "contains", "difference", "ends_with",
	"first", "get", "index_of", "insert_at", "intersect", "keys", "last", "last_index_of",
	"pad_end", "pad_start", "recv_into", "remove", "remove_at", "repeat", "replace", "reverse",
	"send", "slice", "sort", "split", "starts_with", "to_base64", "to_bytes", "to_list",
	"to_lower", "to_set", "to_upper", "trim", "trim_end", "trim_start", "union", "values",
	"wait",
}

// GroupNames 返回标准库的组名，编辑器在行首打 `con` 也能补出 `console`。
//
// en: GroupNames returns the standard-library group names, so typing `con` at the start of a
// line also completes to `console`.
func GroupNames() []string {
	out := make([]string, len(stdGroups))
	copy(out, stdGroups)
	sort.Strings(out)
	return out
}

// IsGroup 判断这个名字是不是标准库的组名：组名不是一个值，它后面点出来的那一批成员
// 与容器的成员走的是两条路径。
//
// en: IsGroup tells whether this name is a standard-library group name: a group name is not a
// value, and the members dotted off it take a different road from the members of a container.
func IsGroup(name string) bool { return isStdGroup(name) }

// MembersOn 返回"点号左边那一个对象上可以补出哪些名字"。
//
// receiver 是点号左边写出来的那一个名字（可能是组名，也可能是一个变量或一个包的挂名），
// t 是检查器给点号左边那一段算出来的类型（算不出来传零值）。
// 组名与包那两种不需要类型，剩下的都按 t 那一种形状问。
//
// en: MembersOn returns the names that can be dotted off whatever is left of this dot. receiver
// is the name written to the left of the dot (possibly a group name, a variable, or a package
// alias) and t is the type the checker computed for that expression (pass the zero value when
// it could not be computed). The group and package cases need no type; everything else is
// answered from the shape of t.
func MembersOn(receiver string, t Type, f *Facts) []Member {
	if isStdGroup(receiver) {
		return groupFunctions(receiver)
	}
	switch t.Kind {
	case List, Dict, Set, Str, Bytes, Socket, Listener, Process:
		// 这一类的形状自己就能确定，不需要 Facts：逐条问 stdMember 认不认这个名字。
		out := make([]Member, 0, len(stdMemberNames))
		for _, name := range stdMemberNames {
			mm := stdMember(t, name)
			if mm.typ.Kind != FuncT {
				continue // 这一类没有这个成员
			}
			out = append(out, Member{Name: name, Detail: SigText(name, containerGroup(t.Kind)+"."+name, mm.sig.Params, mm.sig.Ret, false)})
		}
		return sorted(out)
	case Struct, Class, Iface, Enum:
		if f == nil {
			return nil // 自定义类型要到那一趟检查的表里取，没跑过检查时取不到
		}
		if t.Kind == Enum {
			return enumValues(t, f)
		}
		return typeMembers(t, f)
	}
	return nil
}

// ReceiverIsPackage 认点号左边那一个名字是不是本文件挂过的包名；是就返回那个包的根。
//
// en: ReceiverIsPackage recognises whether the name left of the dot is a package alias bound in
// this file, and returns that package's root when it is.
func ReceiverIsPackage(file, receiver string, f *Facts) (string, bool) {
	if f == nil {
		return "", false
	}
	root, ok := f.Packages[NameKey(file, receiver)]
	return root, ok
}

// PackageMembers 返回一个包能点出来的名字：它自己 `export` 出去的函数，与它 `export` 出去的
// 类型名（类型写在 `wt.` 后面只出现在 `let p: wt.Page` 那一种地方，造值要 `new`）。
//
// en: PackageMembers returns the names a package can be dotted into: the functions it exported
// and the type names it exported (a type written after `wt.` only makes sense in a `let p:
// wt.Page` position; making a value takes `new`).
func PackageMembers(root string, f *Facts) []Member {
	if f == nil || root == "" {
		return nil
	}
	prefix := root + pkgSep
	var out []Member
	for key, sg := range f.Sigs {
		if !strings.HasPrefix(key, prefix) || sg.Method {
			continue
		}
		name := key[len(prefix):]
		if strings.Contains(name, ".") || strings.Contains(name, pkgSep) {
			continue // 包里类型的方法键是"包根/类型名.方法名"，不是包自己交出的函数
		}
		fn := f.Funcs[key]
		if fn == nil || !fn.Exported() {
			continue
		}
		out = append(out, Member{Name: name, Detail: SigText(name, name, sg.Params, sg.Ret, false)})
	}
	for key, d := range f.Declared {
		if !strings.HasPrefix(key, prefix) || !d.Export {
			continue
		}
		name := key[len(prefix):]
		if strings.Contains(name, ".") || strings.Contains(name, pkgSep) {
			continue
		}
		if fn := f.Funcs[key]; fn != nil {
			continue // 同一个名字既是函数又是类型：那由检查器负责拒绝，这里不重复处理
		}
		out = append(out, Member{Name: name, Detail: Declaration(name, d), Field: true})
	}
	// 包交出来的数：写法照"名字: 类型"那一行，例如 `LIMIT: int`。
	// `export shared` 那一半在包外读不到，因此不列出，避免补全之后仍然报错。
	for key, tv := range f.TopValues {
		if !strings.HasPrefix(key, prefix) || !tv.Export || tv.Shared {
			continue
		}
		name := key[len(prefix):]
		if strings.Contains(name, ".") || strings.Contains(name, pkgSep) {
			continue
		}
		if fn := f.Funcs[key]; fn != nil {
			continue
		}
		out = append(out, Member{Name: name, Detail: name + ": " + tv.Typ.String(), Field: true, Doc: tv.Decl})
	}
	return sorted(out)
}

// typeMembers 返回一个类、结构体或接口的字段与方法。字段表是摊平过的那一份（含继承来的），
// 方法按规范名配前缀查找：`libs/web_test/Page.goto` 这种键在包里的类型上照样对得上。
//
// en: typeMembers returns the fields and methods of a class, struct or interface. The field
// table is the flattened one (including inherited fields), and methods are looked up by
// canonical name with a prefix, so a key like `libs/web_test/Page.goto` still matches a type
// inside a package.
func typeMembers(t Type, f *Facts) []Member {
	var out []Member
	for _, fl := range f.Fields[t.Name] {
		out = append(out, Member{Name: fl.Name, Detail: fl.Name + ": " + fl.Typ.String(), Field: true})
	}
	prefix := t.Name + "."
	for key, sg := range f.Sigs {
		if !strings.HasPrefix(key, prefix) || !sg.Method {
			continue
		}
		name := key[len(prefix):]
		out = append(out, Member{Name: name, Detail: SigText(name, name, sg.Params, sg.Ret, false)})
	}
	return sorted(out)
}

func enumValues(t Type, f *Facts) []Member {
	prefix := t.Name + "."
	var out []Member
	for key, v := range f.EnumVals {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := key[len(prefix):]
		out = append(out, Member{Name: name, Detail: name + " = " + strconv.FormatInt(v, 10), Field: true})
	}
	return sorted(out)
}

// groupFunctions 返回一个标准库组里的函数名，键是"组名.函数名"的写法。
//
// en: groupFunctions returns the function names inside one standard-library group, keyed as
// "group.function".
func groupFunctions(group string) []Member {
	prefix := group + "."
	var out []Member
	for key, sg := range stdFuncs {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := key[len(prefix):]
		out = append(out, Member{Name: name, Detail: SigText(name, key, sg.params, sg.ret, sg.vararg)})
	}
	return sorted(out)
}

// StdSignature 返回一条内建动作的一行签名（悬停用：`Facts.Calls` 里记的 Std 就是
// "console.print"、"list.add" 那一种落点名）。认不出返回空串。
//
// en: StdSignature returns the one-line signature of a built-in action, for hover: the Std
// recorded in Facts.Calls is exactly a dotted name like "console.print" or "list.add". It
// returns an empty string when it does not recognise the name.
func StdSignature(dotName string) string {
	sg, ok := stdFuncs[dotName]
	if ok {
		return SigText(dotName, dotName, sg.params, sg.ret, sg.vararg)
	}
	// 容器、文本那一侧的成员不在 stdFuncs 里：落点名是 "list.add" 这种"形状.名字"。
	// 悬停这一刻不知道元素类型（它跟着那一只容器自己走），写成 T／K／V：那是主流文档里
	// "该位置随接收者类型确定"的通用记号，比省略元素类型或猜一个 int 更准确。
	i := strings.IndexByte(dotName, '.')
	if i < 0 {
		return ""
	}
	name := dotName[i+1:]
	group := dotName[:i]
	k, ok := kindOfGroup(group)
	if !ok {
		return ""
	}
	t := Type{Kind: k, Name: group}
	switch k {
	case List, Set:
		t.Args = []Type{placeholderType(group, 0)}
	case Dict:
		t.Args = []Type{placeholderType(group, 0), placeholderType(group, 1)}
	}
	if mm := stdMember(t, name); mm.typ.Kind == FuncT {
		return SigText(dotName, dotName, mm.sig.Params, mm.sig.Ret, false)
	}
	return ""
}

// kindOfGroup 把形状名折回 Kind：落点名是"形状.名字"，
// 悬停按它找到那一条成员，不需要逐个试八种形状（`add` 在 list 与 dict 上是两条不同的动作）。
//
// en: kindOfGroup folds a shape name back into a Kind: a dotted name is "shape.name", and hover
// uses this to find that one member instead of trying eight shapes in turn (`add` on a list and
// `add` on a dict are two different actions).
func kindOfGroup(group string) (Kind, bool) {
	for _, k := range []Kind{List, Dict, Set, Str, Bytes, Socket, Listener, Process} {
		if containerGroup(k) == group {
			return k, true
		}
	}
	return ErrT, false
}

// MemberSignature 返回"这一只具体容器上那一条成员"的一行签名：`list.add(value: int)`。
// 悬停拿的是 `Facts.Calls` 里记的接收者类型，元素类型是真实的那一个，不用写成 T。
//
// en: MemberSignature returns the one-line signature of that member on a particular container:
// `list.add(value: int)`. Hover takes the receiver type recorded in Facts.Calls, so the element
// type is the real one and never has to be written as T.
func MemberSignature(recv Type, name string) string {
	group := containerGroup(recv.Kind)
	if group == "" {
		return ""
	}
	mm := stdMember(recv, name)
	if mm.typ.Kind != FuncT {
		return ""
	}
	key := group + "." + name
	return SigText(key, key, mm.sig.Params, mm.sig.Ret, false)
}

// Signature 返回一个函数或方法的一行说明；键是规范名（方法是"类型名.方法名"，包里的带包根）。
// 认不出返回空串，编辑器随即不显示。
//
// en: Signature returns the one-line description of a function or method; the key is the
// canonical name (a method is "type.method", and inside a package it carries the root). It
// returns an empty string when the key is unknown, and the editor shows nothing.
func (f *Facts) Signature(key string) string {
	if f == nil {
		return ""
	}
	sg, ok := f.Sigs[key]
	if !ok {
		return ""
	}
	head := SealedName(key)
	if sg.Method {
		head = bareName(sg.Owner) + "." + head
		return SigText(head, head, sg.Params, sg.Ret, false)
	}
	return SigText(head, head, sg.Params, sg.Ret, false)
}

// SealedName 返回一个规范名最后一截用于展示的名字：包里的成员带着包根
// （`C:/…/libs/greet/twice`），方法是"类型名.方法名"，两种分隔符都要截断。
// 补全列表的标签与这里的一行签名都用它；两边不一致时，屏幕上会出现语言里无法书写的名字。
//
// en: SealedName returns the human-readable last segment of a canonical name: things from a
// package carry the root (`C:/.../libs/greet/twice`) and a method is "type.method", so both
// separators get cut. The completion label and the signature line here both use it - if the two
// diverged, the screen would show a name that cannot be written in the language.
func SealedName(key string) string {
	if i := strings.LastIndexAny(key, "/."); i >= 0 {
		return key[i+1:]
	}
	return key
}

// Declaration 是类型名在补全列表里的短形式：`class Page`、`enum Kind`、`type Row`。
// 照主流语言的写法只说明"它是哪一种声明"，不另加一句说明。
//
// en: Declaration is the short form of a type name in the completion column: `class Page`,
// `enum Kind`, `type Row`. Following mainstream languages it says only which kind of
// declaration it is, with no extra sentence.
func Declaration(name string, d Declared) string {
	switch d.Kind {
	case "class", "struct", "interface", "enum":
		return d.Kind + " " + name
	}
	return "type " + name
}

// DeclaredHover 返回悬停停在类型名上时的那两行：它是哪一种声明，以及造值那一句长什么样。
// 第二行用的是字段自己的名字与类型（`new Page(url: string, width: int)`），
// 而不是"造值要 new XXX"那种说明——屏幕上那一行要能照着写出来。
//
// en: DeclaredHover returns the two lines shown when hover rests on a type name: which kind of
// declaration it is, and what the value-construction line looks like. The second line uses the
// fields' own names and types (`new Page(url: string, width: int)`) rather than "to construct,
// use new XXX" - the line on screen should be something the reader can copy.
func (f *Facts) DeclaredHover(key string, d Declared) string {
	name := SealedName(key)
	head := Declaration(name, d)
	switch d.Kind {
	case "class", "struct":
	default:
		return head
	}
	parts := make([]string, 0, len(f.Fields[key]))
	for _, fl := range f.Fields[key] {
		parts = append(parts, fl.Name+": "+fl.Typ.String())
	}
	return head + "\n\nnew " + name + "(" + strings.Join(parts, ", ") + ")"
}

func sorted(in []Member) []Member {
	out := make([]Member, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

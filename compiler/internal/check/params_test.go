// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// params_test.go 校验 params.go 那一张参数名表与两张真表（stdFuncs、stdMember）保持一致：
// 每一条内建动作的每一个参数都要有一个用于展示的名字，反过来表里也不许留着已经不存在的条目。
//
// 两边不一致的后果是签名显示不完整：少名字时只剩类型，多出来的名字对应不上任何一个参数。
package check

import (
	"sort"
	"strings"
	"testing"
)

func TestEveryBuiltinParamHasADisplayName(t *testing.T) {
	// 组函数那一张表。
	for key, sg := range stdFuncs {
		names, ok := stdParamNames[key]
		if !ok {
			t.Errorf("%s 在 params.go 里没有参数名", key)
			continue
		}
		want := len(sg.params)
		if sg.vararg {
			want = 1 // 变长参数只写一个 (...values)，不管它收几个实参
		}
		if len(names) != want {
			t.Errorf("%s 有 %d 个参数，params.go 给了 %d 个名字：%v", key, len(sg.params), len(names), names)
		}
	}

	// 容器、文本、字节块、套接字、监听句柄、进程句柄上的成员。
	for _, k := range []Kind{List, Dict, Set, Str, Bytes, Socket, Listener, Process} {
		group := containerGroup(k)
		shape := Type{Kind: k, Name: group}
		switch k {
		case List, Set:
			shape.Args = []Type{placeholderType(group, 0)}
		case Dict:
			shape.Args = []Type{placeholderType(group, 0), placeholderType(group, 1)}
		}
		for _, name := range stdMemberNames {
			mm := stdMember(shape, name)
			if mm.typ.Kind != FuncT {
				continue
			}
			key := group + "." + name
			names, ok := stdParamNames[key]
			if !ok {
				t.Errorf("%s 在 params.go 里没有参数名", key)
				continue
			}
			if len(names) != len(mm.sig.Params) {
				t.Errorf("%s 有 %d 个参数，params.go 给了 %d 个名字：%v", key, len(mm.sig.Params), len(names), names)
			}
		}
	}

	// 反方向：表里不许留着真表里已经不存在的条目。
	for key := range stdParamNames {
		if _, ok := stdFuncs[key]; ok {
			continue
		}
		i := strings.IndexByte(key, '.')
		if i < 0 || !memberExists(key[:i], key[i+1:]) {
			t.Errorf("params.go 里 %s 这一条对不上任何一条内建动作", key)
		}
	}
}

// memberExists 判断"形状.名字"这一条在 stdMember 那儿能否识别。
func memberExists(group, name string) bool {
	for _, k := range []Kind{List, Dict, Set, Str, Bytes, Socket, Listener, Process} {
		if containerGroup(k) != group {
			continue
		}
		shape := Type{Kind: k, Name: group}
		switch k {
		case List, Set:
			shape.Args = []Type{placeholderType(group, 0)}
		case Dict:
			shape.Args = []Type{placeholderType(group, 0), placeholderType(group, 1)}
		}
		return stdMember(shape, name).typ.Kind == FuncT
	}
	return false
}

// TestSignatureShape 覆盖写出来的那一行长什么样：编程语言的签名，不是散文。
func TestSignatureShape(t *testing.T) {
	for _, c := range []struct{ dotName, want string }{
		{"console.print", "console.print(...values)"},
		{"console.read", "console.read(): string"},
		{"length", "length(value): int"},
		{"text.to_int", "text.to_int(text: string): int"},
		{"file.read_from", "file.read_from(path: string, at: int, count: int): string"},
		{"http.post", "http.post(url: string, body: bytes, timeout_ms: int): Response"},
		{"math.minimum", "math.minimum(a, b)"},
		{"list.add", "list.add(value: T)"},
		{"list.slice", "list.slice(start: int, end: int): list<T>"},
		{"dict.get", "dict.get(key: K, fallback: V): V"},
		{"dict.add", "dict.add(key: K, value: V)"},
		{"string.replace", "string.replace(from: string, to: string): string"},
		{"socket.send", "socket.send(data: bytes): int"},
		{"process.wait", "process.wait(): int"},
	} {
		if got := StdSignature(c.dotName); got != c.want {
			t.Errorf("%s 那一行写成 %q，要 %q", c.dotName, got, c.want)
		}
	}

	// 具体类型在补全里要落成真类型，不是 T。
	list := Type{Kind: List, Name: "list", Args: []Type{{Kind: Int, Name: "int"}}}
	if got := detailOfMap(MembersOn("xs", list, nil), "add"); got != "add(value: int)" {
		t.Errorf("list<int>.add 写成 %q", got)
	}

	// 散文式占位不许回来：这一行里出现"东西"或光秃秃的省略号就是退回旧写法。
	for _, key := range allStdKeys() {
		s := StdSignature(key)
		if strings.Contains(s, "东西") {
			t.Errorf("%s 那一行还写着散文：%q", key, s)
		}
	}
}

func allStdKeys() []string {
	var out []string
	for key := range stdFuncs {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func detailOfMap(in []Member, name string) string {
	for _, m := range in {
		if m.Name == name {
			return m.Detail
		}
	}
	return ""
}

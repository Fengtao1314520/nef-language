// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/parser"
)

// 每条示例都得编译通过（只要求通过检查，不实际运行：网络、进程、文件那几条运行要搭沙盒）。
// 语法报错在这里也算失败，所以不复用 mustFile——那一条是 t.Fatalf，一份示例坏了会让整轮测试中断。
func TestEveryStdExampleCompiles(t *testing.T) {
	for _, k := range StdDocKeys() {
		src := StdExample(k)
		toks, err := lexer.Lex(src)
		if err != nil {
			t.Errorf("%s 的例子词法不过：%v", k, err)
			continue
		}
		f, err := parser.Parse("e.nf", toks)
		if err != nil {
			t.Errorf("%s 的例子语法不过：%v", k, err)
			continue
		}
		if errs := Run(&Program{Files: []*ast.File{f}, Entry: "e.nf"}); len(errs) > 0 {
			t.Errorf("%s 的例子编译不过：%s\n原文：%s", k, strings.Join(msgs(errs), "；"), src)
		}
	}
}

// 每一条内建动作都要有中文一句、英文一句、用法示例一段；表里也不许留着实际已经不存在的条目。
// 这张表与 params.go 那张参数名表用同一批落点名，所以两边的键必须一字不差地一样。
func TestEveryBuiltinHasBilingualDocAndExample(t *testing.T) {
	for k, d := range stdDocs {
		if d.Zh == "" || d.En == "" || d.Eg == "" {
			t.Errorf("%s 三样不齐：中文 %q、英文 %q、示例 %q", k, d.Zh, d.En, d.Eg)
		}
		if _, ok := stdParamNames[k]; !ok {
			t.Errorf("%s 在 stdDocs 里有，params.go 那张表里没有", k)
		}
	}
	for k := range stdParamNames {
		if _, ok := stdDocs[k]; !ok {
			t.Errorf("%s 有参数名却没有说明，屏幕上会出现只有签名那一条", k)
		}
	}

	// 组名那一段：十四组每组的中文与英文都要有，且组名都在 stdGroups 里。
	if len(groupDocs) != len(stdGroups) {
		t.Errorf("组说明 %d 组，stdGroups %d 组", len(groupDocs), len(stdGroups))
	}
	for _, g := range stdGroups {
		d, ok := groupDocs[g]
		if !ok || d.Zh == "" || d.En == "" {
			t.Errorf("组 %s 缺说明", g)
		}
	}
}

// 落点名要能在两张真表上找得到：说明表里不许造一条语言里不存在的动作。
func TestDocKeysResolveToRealActions(t *testing.T) {
	for k := range stdDocs {
		if _, ok := stdFuncs[k]; ok {
			continue
		}
		if !resolvesAsMember(k) {
			t.Errorf("%s 既不是 stdFuncs 里的落点名，也不是任何一个形状身上的成员", k)
		}
	}
}

// resolvesAsMember 识别 "list.add"、"string.slice" 这一种"形状.名字"：按那张表逐个查一遍。
func resolvesAsMember(dotName string) bool {
	for i := 0; i < len(dotName); i++ {
		if dotName[i] != '.' {
			continue
		}
		head, name := dotName[:i], dotName[i+1:]
		for _, k := range []Kind{List, Dict, Set, Str, Bytes, Socket, Listener, Process} {
			shape := Type{Kind: k, Name: head}
			switch k {
			case List, Set:
				shape.Args = []Type{placeholderType(head, 0)}
			case Dict:
				shape.Args = []Type{placeholderType(head, 0), placeholderType(head, 1)}
			}
			if head != containerGroup(k) {
				continue
			}
			if mm := stdMember(shape, name); mm.typ.Kind == FuncT {
				return true
			}
		}
		return false
	}
	return false
}

// 说明原文的写法必须与 ls 识别的那两个前缀一致：中文一段、`en: ` 一段、`例：` 一段。
func TestStdDocRawShapeMatchesTheEditorPrefixes(t *testing.T) {
	got := StdDocRaw("console.print")
	parts := strings.Split(got, "\n\n")
	if len(parts) != 3 {
		t.Fatalf("说明原文该是三段，得到 %d 段：%q", len(parts), got)
	}
	if !strings.HasSuffix(parts[0], "。") || strings.Contains(parts[0], "en:") {
		t.Errorf("第一段该是中文那一句：%q", parts[0])
	}
	if !strings.HasPrefix(parts[1], "en: ") {
		t.Errorf("第二段该以 en: 起头：%q", parts[1])
	}
	if !strings.HasPrefix(parts[2], "例：\n") {
		t.Errorf("第三段该以 例： 起头，下面一行就是示例：%q", parts[2])
	}
	if StdDocRaw("没有这一条") != "" {
		t.Error("认不出的落点名要交回空串")
	}
	// 按形状查那一条也一样能落到：`xs.add(3)` 的接收者是 list 形状。
	if StdDocFor("", Type{Kind: List, Name: "list"}, "add") == "" {
		t.Error("list 形状上的 add 没找到说明")
	}
	if StdDocFor("console", Type{}, "print") == "" {
		t.Error("组名上的 console.print 没找到说明")
	}
	if StdDocFor("", Type{Kind: Str, Name: "string"}, "to_upper") == "" {
		t.Error("文本身上的 to_upper 没找到说明")
	}
}

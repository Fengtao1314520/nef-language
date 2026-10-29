// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestPathGroupRun 运行 path 四条成员中不涉及磁盘的部分：补分隔符的三条规则、最后一段、
// 点后面那段。期望串来自实际运行结果：
// 甲的末尾已是分隔符就不再加第二个，乙的开头已是分隔符也不加；末尾连续是分隔符时
// `path.file_name` 回退到最后一个非分隔符字符；段首的点不算扩展名（`.nf` 返回空串）。
func TestPathGroupRun(t *testing.T) {
	got, code := run(t, `console.print(path.join("docs", "SPEC.md"))
console.print(path.join("docs\\", "SPEC.md"), "|", path.join("docs", "\\SPEC.md"))
console.print(path.join("", "只有一头"), "|", path.join("另一头", ""))
console.print(path.file_name("a/b/c.nf"), "|", path.file_name("尾缀分隔符\\"))
console.print(path.file_name("整条都是\\") + "|")
console.print(path.extension("n.tar.gz"), "|", path.extension("没有点") + "|", path.extension(".nf"))
console.print(path.extension("a/b.txt\\"), "|", path.extension("尾点.") + "|")
`)
	want := "docs\\SPEC.md\n" +
		"docs\\SPEC.md | docs\\SPEC.md\n" +
		"只有一头 | 另一头\n" +
		"c.nf | 尾缀分隔符\n" +
		"整条都是|\n" +
		"gz | | \n" +
		"txt | |\n"
	if got != want || code != 0 {
		t.Fatalf("path 那几条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestEnvironmentRoundTrip 自己设置一个变量再读取：不依赖机器上已有的环境变量，
// 因此期望串在任何 Windows 上都相同。
//
// 空值那个位置是这条测试的关键：语言规则要求"没设过"与"设成空"可区分，
// 所以设成空再读取必须得到空文本，而不是 10061 号。
func TestEnvironmentRoundTrip(t *testing.T) {
	got, code := run(t, `environment.set("NEF_DEMO_KEY", "甲乙丙")
console.print(environment.get("NEF_DEMO_KEY"))
environment.set("NEF_DEMO_EMPTY", "")
let e:string = environment.get("NEF_DEMO_EMPTY")
console.print("[" + e + "]", length(e))
let dir:string = environment.current_directory()
console.print(length(dir) > 0, path.file_name(dir) != "")
console.print(path.join(dir, "x.nf").contains("x.nf"), path.extension(path.join(dir, "a.nf")) == "nf")
console.print(path.exists("."), path.exists("./一定没有这个东西.nf"))
`)
	want := "甲乙丙\n[] 0\ntrue true\ntrue true\ntrue false\n"
	if got != want || code != 0 {
		t.Fatalf("environment 那三条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestOsGroupThrows 固定这一组的三处报错：变量不存在是 10061、路径无法传递是 10051、
// 空句柄当路径或当变量名都是 10092（与引用类型同一条判空规则）。10061 号还应能被 `catch` 接住。
func TestOsGroupThrows(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"没有这个变量", "console.print(environment.get(\"一定没有的变量名\"))\n", "出错了（10061）：没有这个环境变量"},
		{"空名字也是没有", "console.print(environment.get(\"\"))\n", "出错了（10061）：没有这个环境变量"},
		{"空路径递不出去", "console.print(path.exists(\"\"))\n", "出错了（10051）：这条路径本机用不了"},
		{"名字是空句柄", "let z:string = null\nconsole.print(environment.get(z))\n", "出错了（10092）"},
		{"路径是空句柄", "let z:string = null\nconsole.print(path.file_name(z))\n", "出错了（10092）"},
		{"拼接的乙是空句柄", "let z:string = null\nconsole.print(path.join(\"a\", z))\n", "出错了（10092）"},
		{"设值的值句柄是空", "let z:string = null\nenvironment.set(\"k\", z)\n", "出错了（10092）"},
	}
	for _, one := range cases {
		got, c := run(t, one.src)
		if c != 1 || !strings.Contains(got, one.want) {
			t.Fatalf("%s：got %q 码 %d，要含 %q 码 1", one.name, got, c, one.want)
		}
	}
	// 10061 号走"内建错误与手写 throw 形状统一"的路径，包一层 try/catch 即可给出兜底值
	got, code := run(t, `method or_default(key:string, fallback:string):string {
    try {
        return environment.get(key)
    } catch (code, msg) {
        return fallback
    }
}

console.print(or_default("一定没有的变量名", "算了"))
`)
	if got != "算了\n" || code != 0 {
		t.Fatalf("10061 号该能被接住：got %q 码 %d", got, code)
	}
}

// TestOsGroupsBorrow 验证这一组的入参都是只读借用：调用结束原名字仍在、仍可继续使用，
// 返回的也是一份新文本，而不是把借来的句柄传出去。
func TestOsGroupsBorrow(t *testing.T) {
	got, code := run(t, `let dir:string = environment.current_directory()
let one:string = path.file_name(dir)
console.print(length(dir) > 0, one != "")
console.print(dir == dir, path.join(dir, one) != dir)
let full:string = path.join("docs", "SPEC.md")
console.print(full, path.file_name(full), path.extension(full))
`)
	if code != 0 || !strings.HasPrefix(got, "true true\ntrue true\n") {
		t.Fatalf("借用不对：got %q 码 %d", got, code)
	}
	if !strings.Contains(got, "docs\\SPEC.md SPEC.md md") {
		t.Fatalf("三段交回的串不对：got %q", got)
	}
	// 借用之后原名字仍可使用；反过来"把文本交给一个名字"仍然被拒绝
	if err := compileErr("let p:string = path.join(\"a\", \"b\")\nlet q:string = p\nconsole.print(p)\n"); err == nil ||
		!strings.Contains(err.Error(), "已经交出去") {
		t.Fatalf("path 交回的文本交出后该拦下原名：err=%v", err)
	}
}

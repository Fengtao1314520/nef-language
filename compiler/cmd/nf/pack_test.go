// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// pack_test.go 钉 `nf pack` 交出来的那份容器。
//
// 起真子进程：这一条命令写的每一句话都在标准错误上，退出码也是命令行一侧的。
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"neflang.org/nf/internal/pkgfmt"
)

// write 在 temp 目录下按斜杠路径造一份文件。
func write(t *testing.T, root, rel, text string) string {
	t.Helper()
	full := filepath.FromSlash(filepath.Join(root, rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

// nfRun 跑一条命令，返回它写出来的全部文字与退出码。
func nfRun(t *testing.T, exe string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("起 nf 失败：%v", err)
	}
	return buf.String(), code
}

// 一个包：门面引包内另一份文件，再引一个依赖；依赖自己还带一份包内文件。
func TestPackBuildsContainerFromSourceDirs(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{
  "name": "greet",
  "version": "1.0",
  "deps": { "shout": "2.0" }
}`+"\n")
	write(t, root, "greet.nf", `import shout from "libs/shout"
import { bang } from "./helper.nf"

export method greet(name:string):string {
    return shout.shoutOnce(name) + bang()
}
`)
	write(t, root, "helper.nf", `export method bang():string { return "!" }`)
	write(t, root, "libs/shout/nf.json", `{"name": "shout", "version": "2.0", "deps": {}}`)
	write(t, root, "libs/shout/shout.nf", `import { mark } from "./mark.nf"

export method shoutOnce(s:string):string { return s + mark() }
`)
	write(t, root, "libs/shout/mark.nf", `export method mark():string { return ">" }`)

	out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
	if code != 0 {
		t.Fatalf("打包该成，退出码 %d：%s", code, out)
	}
	container := filepath.Join(root, "greet.nfpack")
	meta, files, err := pkgfmt.Open(container)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "greet" || meta.Version != "1.0" || meta.Deps["shout"] != "2.0" {
		t.Errorf("容器里那份清单不对：%+v", meta)
	}
	var rels []string
	for _, f := range files {
		rels = append(rels, f.Rel)
	}
	sort.Strings(rels)
	want := []string{
		"greet.nf",
		"helper.nf",
		"pkg/shout/2.0/mark.nf",
		"pkg/shout/2.0/nf.json",
		"pkg/shout/2.0/shout.nf",
	}
	if strings.Join(rels, " ") != strings.Join(want, " ") {
		t.Errorf("条目不对：\n got %v\nwant %v", rels, want)
	}
	text, ok := pkgfmt.Find(files, "pkg/shout/2.0/shout.nf")
	if !ok || !strings.Contains(text, "mark()") {
		t.Errorf("依赖那份源文件没并进来：%q", text)
	}
}

// 依赖换成已经打好的容器，走的必须是同一条路，条目一字不差。
func TestPackSwallowsContainerDependency(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	dep := filepath.Join(root, "libs", "shout")
	if err := os.MkdirAll(dep, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "libs/shout/nf.json", `{"name": "shout", "version": "2.0", "deps": {}}`)
	write(t, root, "libs/shout/shout.nf", `export method shoutOnce(s:string):string { return s + ">" }`)

	// 先把依赖打成容器，容器就放在 greet 旁边那一层（`libs/shout.nfpack`），
	// 然后把依赖的源文件目录删掉：外层只能从容器里取。
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(dep, "shout.nf"), "-o", filepath.Join(root, "libs", "shout"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("先打依赖该成：%s", out)
	}
	if err := os.RemoveAll(dep); err != nil {
		t.Fatal(err)
	}

	write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {"shout": "2.0"}}`)
	write(t, root, "greet.nf", `import shout from "libs/shout"

export method greet(name:string):string { return shout.shoutOnce(name) }
`)
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(root, "greet.nf")); code != 0 {
		t.Fatalf("打外层该成：%s", out)
	}
	_, files, err := pkgfmt.Open(filepath.Join(root, "greet.nfpack"))
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, f := range files {
		rels = append(rels, f.Rel)
	}
	sort.Strings(rels)
	// 吞进来的那一层带着它自己那份 nf.json：它写的 deps 才是"c 要哪一版"的答案，
	// 少这一条，再往下引一层就解析不出那个版本（TestNestedDependencyChainRuns 钉的是同一件事）。
	if strings.Join(rels, " ") != "greet.nf pkg/shout/2.0/nf.json pkg/shout/2.0/shout.nf" {
		t.Errorf("从容器里吞进来的条目不对：%v", rels)
	}
}

// 打成容器之后，工程只写一句 `import greet from "libs/greet"` 就能跑到产物：
// 这一条钉的是从容器读盘那一路，门面里的 ./ 引入、点号取函数、点号取类型都在这一条里走到。
func TestPackThenRunFromContainer(t *testing.T) {
	exe := nfExe(t)
	packDir := t.TempDir()
	write(t, packDir, "nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, packDir, "greet.nf", `import { mark } from "./mark.nf"

export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 1 }
}

export method twice(n:int):int {
    return n * 2
}

export method shout(s:string):string {
    return s + mark()
}
`)
	write(t, packDir, "mark.nf", `export method mark():string { return "!" }`)

	proj := t.TempDir()
	write(t, proj, "nf.json", `{"deps": { "greet": "1.0" } }`)
	write(t, proj, "main.nf", `import greet from "libs/greet"

let p:greet.Pair = greet.make()
console.print(p.left, greet.twice(21), greet.shout("hi"))
`)

	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(packDir, "greet.nf"),
		"-o", filepath.Join(proj, "libs", "greet"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("打包该成：%s", out)
	}

	cmd := exec.Command(exe, filepath.Join(proj, "main.nf"))
	var buf strings.Builder
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("直接跑没过：%v\n%s", err, buf.String())
	}
	if got := buf.String(); got != "1 42 hi!\n" {
		t.Errorf("输出不对：got %q", got)
	}

	// 版本要对得上：清单要的那一版与容器里那一份不一样就拒绝。
	write(t, proj, "nf.json", `{"deps": { "greet": "9.9" } }`)
	out, code := nfRun(t, exe, "check", filepath.Join(proj, "main.nf"))
	if code != 1 || !strings.Contains(out, "要 greet 9.9，") || !strings.Contains(out, "里那一份是 1.0") {
		t.Errorf("版本对不上该拦住（%d）：%s", code, out)
	}
}

// 源文件目录那一条路：不打包也能引，包作者改一行不用重打。
func TestRunFromPackageSourceDir(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "greet": "1.0" } }`)
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/greet/greet.nf", `export method twice(n:int):int { return n * 2 }
`)
	write(t, root, "main.nf", `import greet from "libs/greet"

console.print(greet.twice(4))
`)
	out, code := nfRun(t, exe, filepath.Join(root, "main.nf"))
	if code != 0 || !strings.Contains(out, "8") {
		t.Fatalf("从源文件目录引包该跑通（%d）：%s", code, out)
	}
}

// 同一份包被大小写不同的两种写法各引一次，只能算一份：一个文件被多条路径引到时只算一次，
// 同一个包的顶层也只跑一次。不区分大小写的系统上这两条路径是同一个目录，
// 而包的身份是一个路径字符串（名字表按"包根 + 原名"区分）——不先规范化写法，
// 那份包的顶层会跑两遍，它的每一个类型也成两个。
func TestPackageReachedByTwoSpellingsLoadsOnce(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("这一条要的是不区分大小写的文件系统；现在只有 Windows 这一头（pkgload.caseInsensitiveFS）")
	}
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "greet": "1.0" } }`)
	write(t, root, "libs/greet/nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/greet/greet.nf", `export struct Pair {
    n:int
}

export method make():Pair {
    return Pair { n: 7 }
}

console.print("包的顶层跑了一次")
`)
	// 另一份工程文件按另一种写法引同一份包。
	write(t, root, "helper.nf", `import Greet from "libs/Greet"

export method viaOtherSpell():int {
    let p:Greet.Pair = Greet.make()
    return p.n
}
`)
	write(t, root, "main.nf", `import greet from "libs/greet"
import { viaOtherSpell } from "./helper.nf"

let p:greet.Pair = greet.make()
console.print(p.n, viaOtherSpell())
`)
	got := runEntry(t, exe, filepath.Join(root, "main.nf"))
	if n := strings.Count(got, "包的顶层跑了一次"); n != 1 {
		t.Fatalf("那份包的顶层跑了 %d 遍（要 1 遍），实际输出：%q", n, got)
	}
	if got != "包的顶层跑了一次\n7 7\n" {
		t.Fatalf("两种写法该读同一份包的同一个值，实际输出：%q", got)
	}
	if out, code := nfRun(t, exe, "check", filepath.Join(root, "main.nf")); code != 0 || !strings.Contains(out, "3 个文件") {
		t.Fatalf("装载该只算 3 份文件（main、helper、greet），实际（%d）：%s", code, out)
	}
}

func nfRunCodeOut(t *testing.T, exe string, args ...string) (int, string) {
	out, code := nfRun(t, exe, args...)
	return code, out
}

// 依赖自己也有依赖（a → b → c）时，那一层一层的 pkg/ 要一路读得通。
//
// 这里钉的是两处容易断的地方：打包若只把依赖的源文件收进来、不收它自己那份 nf.json，
// b 的 deps 里"要 c 哪一版"到了容器里就没了；读取一侧只按路径结尾识别"容器"，
// b 所在的那一层（`a.nfpack/pkg/b/1.0`）走的就是在盘上遍历工程 `libs/` 的那条路。
// 两层都缺的时候，报错说的是"既没有 libs/c.nfpack 这份容器"，指不到根上。
func TestNestedDependencyChainRuns(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()

	// 最里层那一个包 c。
	write(t, root, "c/nf.json", `{"name": "c", "version": "1.0", "deps": {}}`)
	write(t, root, "c/c.nf", `export struct Node {
    n:int
}

export method make():Node {
    return Node { n: 5 }
}

export method twice(k:int):int {
    return k * 2
}
`)
	// 中间层 b：门面把自己的类型交出去，c 的类型留在包内。
	write(t, root, "b/nf.json", `{"name": "b", "version": "1.0", "deps": { "c": "1.0" } }`)
	write(t, root, "b/libs/c/nf.json", `{"name": "c", "version": "1.0", "deps": {}}`)
	write(t, root, "b/libs/c/c.nf", `export struct Node {
    n:int
}

export method make():Node {
    return Node { n: 5 }
}

export method twice(k:int):int {
    return k * 2
}
`)
	write(t, root, "b/b.nf", `import c from "libs/c"

export struct Count {
    value:int
}

export method build():Count {
    let n:c.Node = c.make()
    return Count { value: n.n + 1 }
}

export method shout(k:int):int {
    return c.twice(k) + 1
}
`)
	// 最外层 a：引 b，用得到的是 b 自己的类型。
	write(t, root, "a/nf.json", `{"name": "a", "version": "1.0", "deps": { "b": "1.0" } }`)
	write(t, root, "a/libs/b/nf.json", `{"name": "b", "version": "1.0", "deps": { "c": "1.0" } }`)
	write(t, root, "a/libs/b/libs/c/nf.json", `{"name": "c", "version": "1.0", "deps": {}}`)
	write(t, root, "a/libs/b/libs/c/c.nf", `export struct Node {
    n:int
}

export method make():Node {
    return Node { n: 5 }
}

export method twice(k:int):int {
    return k * 2
}
`)
	write(t, root, "a/libs/b/b.nf", `import c from "libs/c"

export struct Count {
    value:int
}

export method build():Count {
    let n:c.Node = c.make()
    return Count { value: n.n + 1 }
}

export method shout(k:int):int {
    return c.twice(k) + 1
}
`)
	write(t, root, "a/a.nf", `import b from "libs/b"

export method total():int {
    let w:b.Count = b.build()
    return w.value + b.shout(1)
}
`)
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(root, "a/a.nf"),
		"-o", filepath.Join(root, "proj/libs", "a"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("打 a 该成：%s", out)
	}

	// 条目里要看得见中间层自己那份清单——没有它，就无从得知 b 要 c 的哪一版。
	_, files, err := pkgfmt.Open(filepath.ToSlash(filepath.Join(root, "proj", "libs", "a"+pkgfmt.Suffix)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pkg/b/1.0/nf.json", "pkg/b/1.0/pkg/c/1.0/c.nf"} {
		if _, ok := pkgfmt.Find(files, want); !ok {
			t.Errorf("容器里缺 %s 这一条", want)
		}
	}

	write(t, root, "proj/nf.json", `{"deps": { "a": "1.0" } }`)
	write(t, root, "proj/main.nf", `import a from "libs/a"

console.print(a.total())
`)
	// 5 + 1 = 6，2×1 + 1 = 3，合起来 9。
	if got := runEntry(t, exe, filepath.Join(root, "proj/main.nf")); got != "9\n" {
		t.Fatalf("三层依赖跑出的数不对（要 9）：%q", got)
	}
}

// 不带花括号那一种是引包；写成 `./` 起头的本地路径时，那句"既没有 libs/x.nf.nfpack 这份容器"
// 指不到根上——要改的是那个花括号。
func TestPackageImportOfLocalFileRefused(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "util.nf", `export method twice(n:int):int { return n * 2 }`)
	write(t, root, "main.nf", `import util from "./util.nf"

console.print(util.twice(3))
`)
	out, code := nfRun(t, exe, "check", filepath.Join(root, "main.nf"))
	if code != 1 || !strings.Contains(out, `引自己工程里的文件要写 import { 名字 } from "./util.nf"`) {
		t.Errorf("这一句该指到花括号上（%d）：%s", code, out)
	}
}

// nfRunIn 与 nfRun 同一条，只是先把工作目录切到 dir 再跑：入口只给一个光文件名，
// 钉的就是"在工程目录里运行 nf"这一种最常见的用法。
func nfRunIn(t *testing.T, dir, exe string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	var buf strings.Builder
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("起 nf 失败：%v", err)
	}
	return buf.String(), code
}

// 一个包引自己的依赖只许写整块挂名那一种（`import shout from "libs/shout"`）：花括号那一种
// 的路径必须以 ./ 或 ../ 开头，光秃秃的 "libs/shout" 到检查一侧就是错的。打包这一步不收这种
// 写法：否则交出的容器在使用方编译失败，报错落不到写它的人当场。
func TestPackRefusesBraceDependency(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {"shout": "2.0"}}`)
	write(t, root, "greet.nf", `import { shoutOnce } from "libs/shout"

export method greet(name:string):string { return shoutOnce(name) }
`)
	out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
	if code != 1 || !strings.Contains(out, `包引自己的依赖要写 import 包名 from "libs/包名"`) {
		t.Errorf("依赖要走整块挂名那一种（%d）：%s", code, out)
	}
}

// run 直接跑一份入口，返回它写出来的全部文字（跑不成就算测试失败）。
func runEntry(t *testing.T, exe, entry string) string {
	t.Helper()
	cmd := exec.Command(exe, entry)
	var buf strings.Builder
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("跑 %s 没过：%v\n%s", entry, err, buf.String())
	}
	return buf.String()
}

// 两份包各有一只同名类型，一起用时不该冲突——编得过还要跑得对，
// 形状表与类型字必须各归各的（挤在一起的话，打印出来的是别人的字段或者干脆把内存拆错）。
func TestTwoPackagesWithSameTypeName(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()
	write(t, root, "nf.json", `{"deps": { "aa": "1.0", "bb": "1.0" } }`)
	write(t, root, "libs/aa/nf.json", `{"name": "aa", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/aa/aa.nf", `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 1 }
}
`)
	write(t, root, "libs/bb/nf.json", `{"name": "bb", "version": "1.0", "deps": {}}`)
	write(t, root, "libs/bb/bb.nf", `export struct Pair {
    name:string
}

export method make():Pair {
    return Pair { name: "hi" }
}
`)
	write(t, root, "main.nf", `import aa from "libs/aa"
import bb from "libs/bb"

let x:aa.Pair = aa.make()
let y:bb.Pair = bb.make()
console.print(x.left, y.name, x, y)
`)
	got := runEntry(t, exe, filepath.Join(root, "main.nf"))
	if strings.Count(got, "Pair") < 2 || !strings.Contains(got, "1 hi") {
		t.Fatalf("两只 Pair 各印各的字段，实际输出：%q", got)
	}
	if !strings.Contains(got, "left") || !strings.Contains(got, "name") {
		t.Fatalf("形状表串了（一只该有 left、另一只该有 name），实际输出：%q", got)
	}
}

// 同一个包的两份版本并在一处：各自吞在两份容器里，各有各的包根，跑出来的数各是各的。
func TestTwoVersionsOfOnePackageRun(t *testing.T) {
	exe := nfExe(t)
	root := t.TempDir()

	// 先把 c 的两份版本各打成容器，放在两份外层包旁边。
	write(t, root, "c13/nf.json", `{"name": "c", "version": "1.3.0", "deps": {}}`)
	write(t, root, "c13/c.nf", `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 13 }
}
`)
	write(t, root, "c20/nf.json", `{"name": "c", "version": "2.0.0", "deps": {}}`)
	write(t, root, "c20/c.nf", `export struct Pair {
    left:int
}

export method make():Pair {
    return Pair { left: 20 }
}
`)
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(root, "c13/c.nf"),
		"-o", filepath.Join(root, "bigA/libs", "c"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("先打 c 1.3.0 该成：%s", out)
	}
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(root, "c20/c.nf"),
		"-o", filepath.Join(root, "bigB/libs", "c"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("先打 c 2.0.0 该成：%s", out)
	}

	bigFacade := `import c from "libs/c"

export method value():int {
    let p:c.Pair = c.make()
    return p.left
}
`
	write(t, root, "bigA/nf.json", `{"name": "bigA", "version": "1.0", "deps": { "c": "1.3.0" } }`)
	write(t, root, "bigA/bigA.nf", bigFacade)
	write(t, root, "bigB/nf.json", `{"name": "bigB", "version": "1.0", "deps": { "c": "2.0.0" } }`)
	write(t, root, "bigB/bigB.nf", bigFacade)
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(root, "bigA/bigA.nf"),
		"-o", filepath.Join(root, "proj/libs", "bigA"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("打 bigA 该成：%s", out)
	}
	if code, out := nfRunCodeOut(t, exe, "pack", filepath.Join(root, "bigB/bigB.nf"),
		"-o", filepath.Join(root, "proj/libs", "bigB"+pkgfmt.Suffix)); code != 0 {
		t.Fatalf("打 bigB 该成：%s", out)
	}

	write(t, root, "proj/nf.json", `{"deps": { "bigA": "1.0", "bigB": "1.0" } }`)
	write(t, root, "proj/main.nf", `import bigA from "libs/bigA"
import bigB from "libs/bigB"

console.print(bigA.value(), bigB.value())
`)
	if got := runEntry(t, exe, filepath.Join(root, "proj/main.nf")); got != "13 20\n" {
		t.Fatalf("同一个包的两份版本各算各的，实际输出：%q", got)
	}
}

// 打包的拒绝路径：每一个子测试钉一句报错原文与一个退出码。
func TestPackRefusals(t *testing.T) {
	exe := nfExe(t)

	t.Run("没有清单", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "greet.nf", `export method greet():string { return "hi" }`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
		if code != 1 || !strings.Contains(out, "读不到这份清单") {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})

	t.Run("清单里的名字跟门面不一样", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "nf.json", `{"name": "other", "version": "1.0", "deps": {}}`)
		write(t, root, "greet.nf", `export method greet():string { return "hi" }`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
		if code != 1 || !strings.Contains(out, "nf.json 里写的名字是 other，门面文件却是 greet.nf：这两处得是同一个名字") {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})

	t.Run("引了依赖却没在清单里写哪一版", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
		write(t, root, "greet.nf", `import shout from "libs/shout"

export method greet():string { return shout.shoutOnce("hi") }
`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
		if code != 1 || !strings.Contains(out, `这一句引了 "libs/shout"，nf.json 的 deps 里却没写 shout 哪一版`) {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})

	t.Run("清单写的版本跟依赖那一份不一样", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {"shout": "9.9"}}`)
		write(t, root, "greet.nf", `import shout from "libs/shout"

export method greet():string { return shout.shoutOnce("hi") }
`)
		write(t, root, "libs/shout/nf.json", `{"name": "shout", "version": "2.0", "deps": {}}`)
		write(t, root, "libs/shout/shout.nf", `export method shoutOnce(s:string):string { return s }`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
		if code != 1 || !strings.Contains(out, "要 shout 9.9，") || !strings.Contains(out, "里那一份是 2.0") {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})

	t.Run("相对引入跨出包目录", func(t *testing.T) {
		// 包目录放在测试的临时目录里面一层：`../outside` 仍落在该测试的临时目录里，随它一起删除。
		root := filepath.Join(t.TempDir(), "pkg")
		write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
		write(t, root, "greet.nf", `import { far } from "../outside/util.nf"

export method greet():string { return far() }
`)
		write(t, root, "../outside/util.nf", `export method far():string { return "f" }`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
		if code != 1 || !strings.Contains(out, "那是这个包目录外面的东西：包只能带自己的文件") {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})

	t.Run("没有门面文件", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.nf"))
		if code != 1 || !strings.Contains(out, "这个包没有门面文件 greet.nf") {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})

	t.Run("门面不是 .nf", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "nf.json", `{"name": "greet", "version": "1.0", "deps": {}}`)
		out, code := nfRun(t, exe, "pack", filepath.Join(root, "greet.txt"))
		if code != 2 || !strings.Contains(out, "门面文件的名字得是 <包名>.nf") {
			t.Errorf("原话或退出码不对（%d）：%s", code, out)
		}
	})
}

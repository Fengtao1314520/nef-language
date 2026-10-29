// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree_more_e2e_test.go 覆盖遍历目录树的两个成员：
// file.walk_paths 返回整棵树的所有全路径（目录本身也计入一条），
// file.remove_tree 递归删除目录及其内容。两个成员都在临时目录里真实创建和删除。
// 同时固定枚举使用的模式串：`*` 后面必须紧跟一个 0，
// 否则上一轮残留的名字尾巴会延续，第二次枚举会变成"按那个尾巴筛选"。

// buildTree 构造下面这棵目录树：
//
//	根/甲.txt
//	根/乙/丙.txt
//	根/乙/丁/戊.txt
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "乙", "丁"), 0o755); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	for name, body := range map[string]string{
		"甲.txt":                          "一",
		filepath.Join("乙", "丙.txt"):      "二二",
		filepath.Join("乙", "丁", "戊.txt"): "三三三",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("备料失败：%v", err)
		}
	}
	return filepath.ToSlash(root)
}

func TestFileWalkPathsTree(t *testing.T) {
	d := buildTree(t)
	// 根路径末尾带分隔符传入：返回的分隔符跟随它，期望串里不混用两种斜杠
	got, code := run(t, `
let root:string = "`+d+`/"
let xs:list<string> = file.walk_paths(root)
xs.sort()
console.print(length(xs))
for (i:int in 0 until length(xs)) {
    console.print(xs[i])
}
`)
	// 根路径末尾带分隔符时，第一层就使用该分隔符；更深层的名字本身不再带分隔符，
	// 补的是本机的 `\`——这两点遵循 file.list_paths 早已定下的写法。
	want := "5\n" +
		d + "/乙\n" +
		d + "/乙\\丁\n" +
		d + "/乙\\丁\\戊.txt\n" +
		d + "/乙\\丙.txt\n" +
		d + "/甲.txt\n"
	if got != want || code != 0 {
		t.Fatalf("整棵树的全路径不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 根不存在与根是文件：两条都报 10043，与 file.list_paths 的答复一致
	for _, probe := range []string{d + "/没有这一层", d + "/甲.txt"} {
		got, code = run(t, `console.print(length(file.walk_paths("`+probe+`")))`)
		if code != 1 || !strings.Contains(got, "10043）：打不开这个目录") {
			t.Fatalf("%q 得到 %q 码 %d，要 10043 那一句", probe, got, code)
		}
	}
	// 空目录：只含 . 与 ..，返回的是空列表
	empty := filepath.ToSlash(t.TempDir())
	got, code = run(t, `console.print(length(file.walk_paths("`+empty+`")))`)
	if got != "0\n" || code != 0 {
		t.Fatalf("空目录得到 %q 码 %d，要 0", got, code)
	}
}

// TestFileWalkDepthCap 验证深度上限：七十层的链条若一次钻到底不会终止，第 64 层是刚设定的上限。
// 触顶后返回已枚举的层级——既不挂住，也不抛出。
func TestFileWalkDepthCap(t *testing.T) {
	root := t.TempDir()
	p := root
	for i := 0; i < 70; i++ {
		p = filepath.Join(p, "a")
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	d := filepath.ToSlash(root)
	got, code := run(t, `
let root:string = "`+d+`/"
let xs:list<string> = file.walk_paths(root)
console.print(length(xs))
console.print(file.is_directory("`+d+`/a/a"))
`)
	// 第 64 层的条目仍会被记入名单，只是不再继续递归，所以结果有 65 条而不是 70 条
	if got != "65\ntrue\n" || code != 0 {
		t.Fatalf("七十层的链得到 %q 码 %d，要 65 条与上一层还在", got, code)
	}
}

func TestFileRemoveTree(t *testing.T) {
	d := buildTree(t)
	got, code := run(t, `
let root:string = "`+d+`/"
file.remove_tree(root + "乙")
console.print(file.exists(root + "乙"), file.exists(root + "甲.txt"))
let left:list<string> = file.walk_paths(root)
left.sort()
console.print(length(left), left[0])
file.remove_tree(root)
console.print(file.exists(root))
`)
	want := "false true\n1 " + d + "/甲.txt\nfalse\n"
	if got != want || code != 0 {
		t.Fatalf("连里面一起删不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if _, err := os.Stat(filepath.FromSlash(d)); !os.IsNotExist(err) {
		t.Fatalf("程序说根删掉了，磁盘上还在：%v", err)
	}

	// 根不存在：10043，与 file.walk_paths 一致（它内部调用的就是同一条例程）
	got, code = run(t, `file.remove_tree("`+d+`/没有这一层")`)
	if code != 1 || !strings.Contains(got, "10043）：打不开这个目录") {
		t.Fatalf("根不在得到 %q 码 %d，要 10043 那一句", got, code)
	}
	// 根是一个文件：同样报 10043，它不是可遍历的目录
	fileSide := filepath.ToSlash(t.TempDir())
	if err := os.WriteFile(filepath.Join(fileSide, "乙.txt"), []byte("一"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code = run(t, `file.remove_tree("`+fileSide+`/乙.txt")`)
	if code != 1 || !strings.Contains(got, "10043）：打不开这个目录") {
		t.Fatalf("根是文件得到 %q 码 %d，要 10043 那一句", got, code)
	}
}

// TestEnumerationPatternNotSticky 固定模式串的结尾：`*` 后面的那个 0 必须写。
// 对同一目录连续查询三次全路径，每次应返回同样的两条；查询过全路径形式之后
// 再查询只含名字的形式，也仍是两条。v0.2.0 版本这里行为错误：第二次起只返回按残留尾巴筛选出的一条。
func TestEnumerationPatternNotSticky(t *testing.T) {
	d := buildTree(t)
	got, code := run(t, `
let root:string = "`+d+`"
for (round:int in 0 until 3) {
    let p:list<string> = file.list_paths(root)
    p.sort()
    console.print(round, length(p))
}
let names:list<string> = file.list(root)
names.sort()
console.print(length(names))
let again:list<string> = file.list_paths(root + "/乙")
again.sort()
console.print(length(again))
`)
	want := "0 2\n1 2\n2 2\n2\n2\n"
	if got != want || code != 0 {
		t.Fatalf("连着问三次全路径不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 遍历完一棵树后再查询另一目录：模式串是同一块缓冲区，遍历期间写入过名字，这里仍应全量返回
	other := filepath.ToSlash(t.TempDir())
	if err := os.WriteFile(filepath.Join(other, "丙.txt"), []byte("一"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code = run(t, `
let deep:string = "`+d+`/"
let xs:list<string> = file.walk_paths(deep)
console.print(length(xs))
console.print(length(file.list_paths("`+other+`")))
console.print(length(file.walk_paths("`+other+`")))
`)
	if got != "5\n1\n1\n" || code != 0 {
		t.Fatalf("钻完再问得到 %q 码 %d，要 5／1／1", got, code)
	}
}

// TestWalkHeap 两万圈各遍历一棵三个条目的树：list、其中每一份文本、栈上的节点都应完整释放。
func TestWalkHeap(t *testing.T) {
	d := buildTree(t)
	if used := heapAt(t, `
let root:string = "`+d+`/"
for (i:int in 0 until 20000) {
    let xs:list<string> = file.walk_paths(root)
}
`); used > 4000 {
		t.Fatalf("两万圈各钻一次，峰值 %d 字节：那只 list、里面的文本或栈上的节点大概没拆", used)
	}
}

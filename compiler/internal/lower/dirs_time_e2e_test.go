// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dirs_time_e2e_test.go 覆盖剩余四条目录动作与 time.sleep：
// file.create_directory、file.is_directory、file.rename、file.remove_directory、time.sleep。
// 错误码规则：建不出与改不了各新增一号（10046、10047），删空目录复用 10041 号、只更换句子。

// TestFileCreateDirectory 建一层目录：新建返回 true，再调用一次返回 false（已存在，不算错误）；
// 目标路径上有同名文件也返回 false（Windows 返回"已存在"那个错误码）；上一层不存在才抛 10046。
func TestFileCreateDirectory(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.ToSlash(filepath.Join(dir, "没有的那一层", "里面"))
	newDir := filepath.ToSlash(filepath.Join(dir, "新建的这一层"))
	clash := filepath.ToSlash(filepath.Join(dir, "文件.txt"))
	got, code := run(t, `
let d:string = "`+newDir+`"
console.print(file.create_directory(d), file.is_directory(d))
console.print(file.create_directory(d))
`)
	want := "true true\nfalse\n"
	if got != want || code != 0 {
		t.Fatalf("建目录那两条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 与同名文件冲突：Windows 返回的是"已存在"那个错误码，所以返回 false，不抛出。
	if err := os.WriteFile(filepath.Join(dir, "文件.txt"), []byte("甲"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code = run(t, `console.print(file.create_directory("`+clash+`"), file.is_directory("`+clash+`"))`)
	if got != "false false\n" || code != 0 {
		t.Fatalf("撞上同名文件该交回 false：%q 码 %d", got, code)
	}
	got, code = run(t, `console.print(file.create_directory("`+gone+`"))`)
	if code != 1 || !strings.Contains(got, "10046）：建不出这一层目录") {
		t.Fatalf("上头没有这一层该抛 10046：%q 码 %d", got, code)
	}
}

// TestFileIsDirectory 三种情形各一次：目录真、文件假、根本不存在也假（只返回判定，不抛出）。
// 空路径属于"参数不合法"，抛 10051 号那一句，与 file.exists 同一个号。
func TestFileIsDirectory(t *testing.T) {
	dir := t.TempDir()
	aFile := filepath.Join(dir, "一个文件.txt")
	if err := os.WriteFile(aFile, []byte("甲"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := run(t, `
let d:string = "`+filepath.ToSlash(dir)+`"
console.print(file.is_directory(d), file.is_directory("`+filepath.ToSlash(aFile)+`"))
console.print(file.is_directory("`+filepath.ToSlash(filepath.Join(dir, "根本没有这一项"))+`"))
`)
	if got != "true false\nfalse\n" || code != 0 {
		t.Fatalf("is_directory 三种情形不对：\n got %q 码 %d", got, code)
	}
	got, code = run(t, `console.print(file.is_directory(""))`)
	if code != 1 || !strings.Contains(got, "10051）：这条路径本机用不了") {
		t.Fatalf("空路径该抛 10051：%q 码 %d", got, code)
	}
}

// TestFileRenameMovesFile 验证改名：旧名字消失、新名字存在，内容随之转移；
// 给一个根本不存在的名字改名抛 10047 号那一句。
func TestFileRenameMovesFile(t *testing.T) {
	dir := t.TempDir()
	oldName := filepath.ToSlash(filepath.Join(dir, "旧的.txt"))
	newName := filepath.ToSlash(filepath.Join(dir, "新的.txt"))
	if err := os.WriteFile(filepath.Join(dir, "旧的.txt"), []byte("三段"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	got, code := run(t, `
let o:string = "`+oldName+`"
let n:string = "`+newName+`"
file.rename(o, n)
console.print(file.exists(o), file.exists(n), file.read(n))
`)
	want := "false true 三段\n"
	if got != want || code != 0 {
		t.Fatalf("改名不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	got, code = run(t, `file.rename("`+filepath.ToSlash(filepath.Join(dir, "没有这个.nf"))+`", "`+newName+`")`)
	if code != 1 || !strings.Contains(got, "10047）：这个名字改不了") {
		t.Fatalf("改一个不在的名字该抛 10047：%q 码 %d", got, code)
	}
}

// TestFileRemoveDirectory 空目录可删除；目录里有内容就抛 10041 号那句"删不掉这个目录"，
// 这一版不递归进入子目录，也没有"连带删除其中内容"的行为。
func TestFileRemoveDirectory(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "里面有东西")
	if err := os.Mkdir(full, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(full, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	empty := filepath.ToSlash(filepath.Join(dir, "空的一层"))
	got, code := run(t, `
let d:string = "`+empty+`"
console.print(file.create_directory(d), file.is_directory(d))
file.remove_directory(d)
console.print(file.is_directory(d))
`)
	want := "true true\nfalse\n"
	if got != want || code != 0 {
		t.Fatalf("空目录那一趟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	got, code = run(t, `file.remove_directory("`+filepath.ToSlash(full)+`")`)
	if code != 1 || !strings.Contains(got, "10041）：删不掉这个目录") {
		t.Fatalf("里面有东西该抛 10041 那一句：%q 码 %d", got, code)
	}
}

// TestTimeSleep 验证等待真实生效（墙上时间不少于所等毫秒数）；负数参数不会被当成"等四十亿毫秒"：
// 负的那一侧按 0 处理，程序照常继续执行。
func TestTimeSleep(t *testing.T) {
	began := time.Now()
	got, code := run(t, `
time.sleep(400)
console.print("等完了")
`)
	spent := time.Since(began)
	if got != "等完了\n" || code != 0 {
		t.Fatalf("等一会儿不对：got %q 码 %d", got, code)
	}
	if spent < 300*time.Millisecond {
		t.Fatalf("只过了 %v，那 400 毫秒没等", spent)
	}
	got, code = run(t, `
time.sleep(0)
time.sleep(0 - 1)
console.print("负数没卡住")
`)
	if got != "负数没卡住\n" || code != 0 {
		t.Fatalf("0 与负的那一条不对：%q 码 %d", got, code)
	}
}

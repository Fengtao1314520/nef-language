// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestFileWriteRoundTrip 是往外写四条成员的一次实际运行：写、读回、追加、再读回、覆盖、删掉。
// 文件位于 %TMP% 之下，测试结束前删除，不留残余。
//
// 这三处行为容易被"看着对"掩盖，这条测试逐一覆盖：`file.write` 是覆盖不是追加（覆盖后只读到
// 新内容那一行）、`file.append` 往文件末尾续接（文件不存在时先把它建出来）、
// `length` 数的是码点不是字节（追加完那份是 8 个码点，不是 16 个字节）。
// 最后一段验证"行尾不翻译"：写入的 `\r\n` 读回来仍是两个字符。
func TestFileWriteRoundTrip(t *testing.T) {
	got, code := run(t, `let where:string = path.join(environment.get("TMP"), "nef_io_demo.txt")
file.write(where, "第一行\n")
console.print(file.exists(where), path.exists(where))
console.print(file.read(where))
file.append(where, "第二行\n")
console.print(file.read(where))
console.print(length(file.read(where)))
file.write(where, "覆盖了\n")
console.print(file.read(where))
file.remove(where)
console.print(file.exists(where))

let fresh:string = path.join(environment.get("TMP"), "nef_io_fresh.txt")
file.append(fresh, "头一遍就建\n")
console.print(file.read(fresh))
file.remove(fresh)
console.print(file.exists(fresh))

let crlf:string = path.join(environment.get("TMP"), "nef_io_crlf.txt")
file.write(crlf, "甲\u000D\n乙\n")
console.print(length(file.read(crlf)))
file.remove(crlf)
`)
	want := "true true\n第一行\n\n第一行\n第二行\n\n8\n覆盖了\n\nfalse\n头一遍就建\n\nfalse\n5\n"
	if got != want || code != 0 {
		t.Fatalf("往外写那四条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFileWriteThrows 固定错误码：写不进 10042、删不掉 10041、
// 路径无法传递 10051、空句柄 10092——与 `file.read` 的 10041 号分工一致，每一个都固定不变。
func TestFileWriteThrows(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"底下没有那一层目录", `file.write(path.join(environment.get("TMP"), "nef_io_none/sub.txt"), "一行")` + "\n", "出错了（10042）：写不进去这个文件"},
		{"往目录上追加", `file.append(environment.current_directory(), "一行")` + "\n", "出错了（10042）：写不进去这个文件"},
		{"删一个没有的", `file.remove(path.join(environment.get("TMP"), "nef_io_gone.txt"))` + "\n", "出错了（10041）：删不掉这个文件"},
		{"删一个目录", "file.remove(environment.current_directory())\n", "出错了（10041）：删不掉这个文件"},
		{"空路径写不出去", `file.write("", "一行")` + "\n", "出错了（10051）：这条路径本机用不了"},
		{"空路径当一问", `console.print(file.exists(""))` + "\n", "出错了（10051）：这条路径本机用不了"},
		{"内容是空句柄", "let z:string = null\nfile.write(\"a.txt\", z)\n", "出错了（10092）"},
		{"路径是空句柄", "let z:string = null\nfile.remove(z)\n", "出错了（10092）"},
	}
	for _, one := range cases {
		got, c := run(t, one.src)
		if c != 1 || !strings.Contains(got, one.want) {
			t.Fatalf("%s：got %q 码 %d，要含 %q 码 1", one.name, got, c, one.want)
		}
	}
}

// TestFileWriteBorrow 验证写出去的内容与路径都是只读借用：调用之后原来两个名字仍在，
// 还能继续拿去做追加——写文件不应收走交进去的那段文本。
func TestFileWriteBorrow(t *testing.T) {
	got, code := run(t, `let where:string = path.join(environment.get("TMP"), "nef_io_borrow.txt")
let body:string = "借一下\n"
file.write(where, body)
file.append(where, body)
console.print(file.read(where), length(body), length(where) > 0)
file.remove(where)
`)
	want := "借一下\n借一下\n 4 true\n" // 读回来的那份自带两个换行，`body` 是四个字（三汉字＋一个换行）
	if got != want || code != 0 {
		t.Fatalf("借用不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

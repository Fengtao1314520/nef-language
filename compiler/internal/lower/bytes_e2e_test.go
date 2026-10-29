// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bytes_e2e_test.go 覆盖两件事：
//
// 一、`file.read_bytes` 与 `file.write_bytes` 这一对在类型更换之后仍正常工作——
// 返回与收下的不再是 `list<byte>`，而是 `bytes` 那块连续内存。
// 错误码一个没改：读不到仍是 10041，写不进仍是 10042，路径转换不了仍是 10051。
//
// 二、`bytes` 类型本身：`bytes.new(容量)`、`length`、下标读写、`slice(start, end)`、
// 打印形状、空句柄、越界，以及"块由谁释放"的规则。

// TestFileReadBytes 使用二进制数据：`FF FE` 打头那段不是合法 UTF-8，`file.read` 在它上面抛 10044，
// 这一条照样返回那十个字节。0 与 0x80 原样保留、`\r\n` 不作修改、甲仍是三个字节，
// 长度就是字节数（`file.size` 与它相等正是这一点）。
func TestFileReadBytes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "raw.bin")
	// 十个字节：FF FE 打头（不是合法 UTF-8）、`\r\n`、甲（E7 AC AC）、0、80
	raw := []byte{0xFF, 0xFE, 0x41, 0x0D, 0x0A, 0xE7, 0xAC, 0xAC, 0x00, 0x80}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let buf:bytes = file.read_bytes("`+pp+`")
console.print(length(buf))
console.print(buf[0], buf[1], buf[2])
console.print(buf[3], buf[4])
console.print(buf[5], buf[6], buf[7])
console.print(buf[8], buf[9])
console.print(file.size("`+pp+`") == length(buf))
`)
	want := "10\n255 254 65\n13 10\n231 172 172\n0 128\ntrue\n"
	if got != want || code != 0 {
		t.Fatalf("得到 %q 码 %d，要 %q", got, code, want)
	}
}

// TestFileReadBytesNoUTF8Check 验证"字节这条路不做编码校验"：同一段字节，文本那条路抛 10044，字节这条路交出长度。
func TestFileReadBytesNoUTF8Check(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "坏字节.bin")
	if err := os.WriteFile(p, []byte{0xFF, 0xFE, 0x41}, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let p:string = "`+pp+`"
console.print(length(file.read_bytes(p)))
`)
	if got != "3\n" || code != 0 {
		t.Fatalf("字节读该照样交回三个字节：got %q 码 %d", got, code)
	}
	if text, textCode := run(t, `console.print(file.read("`+pp+`"))`); textCode != 1 ||
		!strings.Contains(text, "10044）：文件不是合法的 UTF-8") {
		t.Fatalf("同一段字节走文本那一条要撞 10044：%q 码 %d", text, textCode)
	}
}

// TestFileBytesRoundTrip 把全 256 个字节值写出去再读回来：一个不多、一个不少、顺序不变，
// 总和 0+1+…+255 = 32640 是最直接的证据。往那个槽位里写要写成 `i as byte`——
// 语言规矩是算术两侧必须同类型，`byte` 自己不作运算的两侧。
func TestFileBytesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "all.bin"))
	got, code := run(t, `
let p:string = "`+p+`"
let buf:bytes = bytes.new(256)
for (i:int in 0 until 256) {
    buf[i] = i as byte
}
file.write_bytes(p, buf)
let back:bytes = file.read_bytes(p)
let sum:int = 0
for (i:int in 0 until length(back)) {
    sum = sum + (back[i] as int)
}
console.print(length(buf), length(back))
console.print(sum)
console.print(back[0], back[255])
`)
	want := "256 256\n32640\n0 255\n"
	if got != want || code != 0 {
		t.Fatalf("往返不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "all.bin")); err != nil || len(raw) != 256 {
		t.Fatalf("文件里应该是 256 个字节，得到 %d 个（%v）", len(raw), err)
	}
}

// TestFileBytesEmpty 验证两端都容纳"一个字节都没有"：空文件读回空的块，空的块写出是 0 字节的文件。
func TestFileBytesEmpty(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "空.bin")
	if err := os.WriteFile(in, nil, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	out := filepath.ToSlash(filepath.Join(dir, "也空.bin"))
	got, code := run(t, `
console.print(length(file.read_bytes("`+filepath.ToSlash(in)+`")))
let none:bytes = bytes.new(0)
file.write_bytes("`+out+`", none)
console.print(file.size("`+out+`"), length(file.read_bytes("`+out+`")))
`)
	want := "0\n0 0\n"
	if got != want || code != 0 {
		t.Fatalf("空的那两头不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFileBytesOverwritesWhole 验证"整个文件写出"那一侧：原来 9 个字节，写出 4 个字节文件就变成 4 个字节，
// 与 file.write 行为一致（不是从第 0 格起覆盖、后面保留）。
func TestFileBytesOverwritesWhole(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(p, []byte("甲乙丙"), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	pp := filepath.ToSlash(p)
	got, code := run(t, `
let p:string = "`+pp+`"
let src:bytes = bytes.new(4)
src[0] = 1
src[1] = 2
src[2] = 3
src[3] = 4
file.write_bytes(p, src)
let back:bytes = file.read_bytes(p)
console.print(length(back), file.size(p))
console.print(back[0], back[1], back[2], back[3])
`)
	want := "4 4\n1 2 3 4\n"
	if got != want || code != 0 {
		t.Fatalf("覆盖那一段不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFileWriteBytesKeepsBlock 验证交出一侧的行为：一块 bytes 交两次都还能正常使用——标准库收参数是只读借用，
// 不留值也不析构原持有者。写完立刻读回来比对一次，确认那三个槽位的值没有变化。
func TestFileWriteBytesKeepsBlock(t *testing.T) {
	dir := t.TempDir()
	first := filepath.ToSlash(filepath.Join(dir, "甲.bin"))
	second := filepath.ToSlash(filepath.Join(dir, "乙.bin"))
	got, code := run(t, `
let buf:bytes = bytes.new(3)
buf[0] = 10
buf[1] = 20
buf[2] = 30
file.write_bytes("`+first+`", buf)
file.write_bytes("`+second+`", buf)
console.print(length(buf), buf[0], buf[2])
let back:bytes = file.read_bytes("`+second+`")
console.print(back[0], back[1], back[2])
`)
	want := "3 10 30\n10 20 30\n"
	if got != want || code != 0 {
		t.Fatalf("借用那一头不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestFileReadBytesSizes 验证"文件有多大读回来就多长"：1、4、5、9 各来一次。
// 这一条旧版本对应的是容器容量那一档（`minCap` 与翻倍搬）——换成 `bytes` 之后没有容量这个槽位，
// 剩下的边界是 `@alloc` 把数据区凑成 8 的倍数那一步：凑出来的余量不许算进 length，
// 也不许把读进去的字节挤走（4 与 5 各跨一个凑整边界，9 是跨过 8 的那一个）。
func TestFileReadBytesSizes(t *testing.T) {
	dir := t.TempDir()
	sizes := []int{1, 4, 5, 9}
	for _, n := range sizes {
		raw := make([]byte, n)
		for i := range raw {
			raw[i] = byte(200 - i) // 第一个是 200，往后每个槽位减一
		}
		if err := os.WriteFile(filepath.Join(dir, "s"+strconv.Itoa(n)+".bin"), raw, 0o644); err != nil {
			t.Fatalf("备料失败：%v", err)
		}
	}
	var src strings.Builder
	for _, n := range sizes {
		p := filepath.ToSlash(filepath.Join(dir, "s"+strconv.Itoa(n)+".bin"))
		fmt.Fprintf(&src, "let b%d:bytes = file.read_bytes(%q)\n", n, p)
		fmt.Fprintf(&src, "console.print(length(b%d), b%d[0], b%d[%d])\n", n, n, n, n-1)
	}
	var want []string
	for _, n := range sizes {
		want = append(want, fmt.Sprintf("%d %d %d", n, 200, 200-(n-1)))
	}
	got, code := run(t, src.String())
	if got != strings.Join(want, "\n")+"\n" || code != 0 {
		t.Fatalf("那四个尺寸不对：\n got %q 码 %d\nwant %q", got, code, strings.Join(want, "\n")+"\n")
	}
}

// TestFileBytesCodes 验证这一双复用的两个错误码：文件不存在、传入的是目录，读那头都是 10041；
// 往不存在的目录里写、把目录当文件写，写那头都是 10042（与 file.read／file.write 同一句报错）。
// 文件本身不存在不算错——写那一侧是 createAlways，新建一份（TestFileBytesRoundTrip 就是这么运行的）。
func TestFileBytesCodes(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.ToSlash(filepath.Join(dir, "没有这个文件.bin"))
	in := filepath.Join(dir, "子目录")
	if err := os.Mkdir(in, 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	deep := filepath.ToSlash(filepath.Join(dir, "没有这一层", "落不下去.bin"))
	cases := []struct {
		src, want string
	}{
		{`console.print(length(file.read_bytes("` + gone + `")))`, "10041）：读不到这个文件"},
		{`console.print(length(file.read_bytes("` + filepath.ToSlash(in) + `")))`, "10041）：读不到这个文件"},
		{`file.write_bytes("` + deep + `", bytes.new(2))`, "10042）：写不进去这个文件"},
		{`file.write_bytes("` + filepath.ToSlash(in) + `", bytes.new(2))`, "10042）：写不进去这个文件"},
	}
	for _, c := range cases {
		got, code := run(t, c.src+"\n")
		if code != 1 || !strings.Contains(got, c.want) {
			t.Fatalf("%s 得到 %q 码 %d，要含 %q", c.src, got, code, c.want)
		}
	}
}

// TestBytesShape 验证这一型看得见的四件事：造出来多长、下标读写、slice 含起不含止、打印出来的形状。
// 打印格式是 `[65, 255, 0]`——与旧版 `list<byte>` 的输出逐字相同（类型更换改的是内存布局，不是那行输出）。
func TestBytesShape(t *testing.T) {
	got, code := run(t, `
let b:bytes = bytes.new(4)
console.print(length(b), b)
b[0] = 65
b[1] = 255
b[2] = 0
b[3] = 7
console.print(b)
console.print(b[1])
let mid:bytes = b.slice(1, 3)
console.print(length(mid), mid)
console.print(b.slice(2, 2), b.slice(0, 4))
let same:list<byte> = [65, 255, 0, 7]
console.print(b == b, length(same))
`)
	want := "4 [0, 0, 0, 0]\n[65, 255, 0, 7]\n255\n2 [255, 0]\n[] [65, 255, 0, 7]\ntrue 4\n"
	if got != want || code != 0 {
		t.Fatalf("那一档的形状不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestBytesSamePrintAsList 验证"两条路印出来逐字相同"：同一串字节，一条走 `bytes.new` 加下标写，
// 一条走 `list<byte>` 的字面量。这一条是元素级期望类型那组用例在单字节槽位上的重跑——
// 一个 1 字节的槽位与一个 8 字节的槽位读回来必须是同一个数。
func TestBytesSamePrintAsList(t *testing.T) {
	got, code := run(t, `
let b:bytes = bytes.new(3)
b[0] = 255
b[1] = 0
b[2] = 65
let xs:list<byte> = [255, 0, 65]
console.print(b)
console.print(xs)
`)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("两条路都要跑得起来：got %q 码 %d", got, code)
	}
	if lines[0] != "[255, 0, 65]" || lines[1] != lines[0] {
		t.Fatalf("同一串字节两条路印得不一样：bytes 那条 %q，list 那条 %q", lines[0], lines[1])
	}
}

// TestBytesIndexOutOfBounds 验证下标一侧的三种越界各抛 10031 号（容器与文本同一条规则，bytes 也适用）：
// 读到最后一个位置之后、读到负数的位置、往越界的位置写。
func TestBytesIndexOutOfBounds(t *testing.T) {
	cases := []string{
		`let b:bytes = bytes.new(3)
console.print(b[3])`,
		`let b:bytes = bytes.new(3)
console.print(b[-1])`,
		`let b:bytes = bytes.new(3)
b[3] = 1`,
	}
	for _, src := range cases {
		got, code := run(t, src+"\n")
		if code != 1 || !strings.Contains(got, "10031）：下标越界") {
			t.Fatalf("%s 得到 %q 码 %d，要 10031 那一句", src, got, code)
		}
	}
}

// TestBytesSliceOutOfBounds 验证 slice 的边界与 list／string 遵循同一条规则（含起不含止）：
// 止越界、负的下标、起大于止都算越界，各抛 10031 号。
func TestBytesSliceOutOfBounds(t *testing.T) {
	cases := []string{
		`let b:bytes = bytes.new(3)
console.print(b.slice(0, 4))`,
		`let b:bytes = bytes.new(3)
console.print(b.slice(-1, 2))`,
		`let b:bytes = bytes.new(3)
console.print(b.slice(2, 1))`,
	}
	for _, src := range cases {
		got, code := run(t, src+"\n")
		if code != 1 || !strings.Contains(got, "10031）：下标越界") {
			t.Fatalf("%s 得到 %q 码 %d，要 10031 那一句", src, got, code)
		}
	}
}

// TestBytesNegativeSizeIsRefused 验证 `bytes.new` 收负数的处理：绕回成大数会一路走到"要不到内存"
// （10091 号），那句报错说的是这台机器给不出这么多，不是"符号写错了"。
// 错误码取容器那一段的兜底 10030 号。
func TestBytesNegativeSizeIsRefused(t *testing.T) {
	got, code := run(t, `let b:bytes = bytes.new(0 - 4)
console.print(length(b))`)
	if code != 1 || !strings.Contains(got, "10030）：bytes.new 要的字节数不能是负数") {
		t.Fatalf("负的那一头得到 %q 码 %d，要 10030 那一句", got, code)
	}
}

// TestBytesNullHandle 验证拿着空句柄访问这一型：三条路各抛 10092 号，与容器行为一致。
func TestBytesNullHandle(t *testing.T) {
	cases := []string{
		`let none:bytes = null
console.print(length(none))`,
		`let none:bytes = null
console.print(none[0])`,
		`let none:bytes = null
console.print(none.slice(0, 1))`,
	}
	for _, src := range cases {
		got, code := run(t, src+"\n")
		if code != 1 || !strings.Contains(got, "10092）：对着空句柄取成员或调用方法") {
			t.Fatalf("%s 得到 %q 码 %d，要 10092 那一句", src, got, code)
		}
	}
	if out, code := run(t, "let none:bytes = null\nconsole.print(none)\n"); code != 0 || out != "null\n" {
		t.Fatalf("空的句柄印出来要的是 null：%q 码 %d", out, code)
	}
}

// TestBytesReleases 验证"块由谁释放"的规则：
// 每一圈造一块没人接住的、每一圈造一块放进容器再整只析构掉的、每一圈读一个小文件进来的，
// 两万圈以后累计都应停留在第一块附近——多出来的都是泄漏。
func TestBytesReleases(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "六十四.bin"))
	if err := os.WriteFile(p, make([]byte, 64), 0o644); err != nil {
		t.Fatalf("备料失败：%v", err)
	}
	cases := []struct {
		name, src string
	}{
		{"每一圈造一块，句柄到名字作废那一处就散", `
for (i:int in 0 until 20000) {
    let b:bytes = bytes.new(24)
    b[0] = 200
}`},
		{"每一圈造一块放进容器，容器自己散", `
for (i:int in 0 until 20000) {
    let xs:list<bytes> = [bytes.new(24)]
    let one:bytes = xs[0]
}`},
		{"每一圈读一个小文件进来", `
let p:string = "` + p + `"
let total:int = 0
for (i:int in 0 until 20000) {
    let b:bytes = file.read_bytes(p)
    total = total + length(b)
}
console.print(total)`},
		{"每一圈把一块交给标准库（只读借用，不还别人的）", `
let p:string = "` + p + `"
for (i:int in 0 until 20000) {
    let b:bytes = bytes.new(8)
    file.write_bytes(p, b)
}`},
		{"slice 造出来的那一片归接住它的名字", `
let head:bytes = bytes.new(64)
let total:int = 0
for (i:int in 0 until 20000) {
    let cut:bytes = head.slice(0, 8)
    total = total + length(cut)
}
console.print(total)`},
	}
	for _, c := range cases {
		_, code := run(t, c.src+"\nconsole.print(1)\n")
		if code != 0 {
			t.Fatalf("%s 跑不起来，码 %d", c.name, code)
		}
		if used := heapAt(t, c.src); used > 4000 {
			t.Errorf("%s：两万圈累计 %d 字节，那几块大概没拆", c.name, used)
		}
	}
}

// TestBytesKeptGrows 是上一条的对照：一路把造出来的块存进顶层那个 list，累计必须真的增长。
// 没有这一条，上一条"累计很小"可能只是根本没造出东西来。
func TestBytesKeptGrows(t *testing.T) {
	used := heapAt(t, `
let keep:list<bytes> = []
for (i:int in 0 until 20000) {
    keep.add(bytes.new(24))
}
console.print(length(keep))
`)
	if used < 400000 {
		t.Fatalf("两万圈各留一块 24 字节的，累计只 %d 字节：这一条要的就是真的涨", used)
	}
}

// TestBytesTextRoundTrip 验证文本与 bytes 互换的三条：空文本、非 ASCII、以及"块写进文件再读回来仍是同一串"。
// 类型更换之前这四个成员返回的是 `list<byte>`，网站上的例子与这里的期望串都是同一串十进制数。
func TestBytesTextRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "文本.bin"))
	got, code := run(t, `
let s:string = "甲a"
let bs:bytes = s.to_bytes()
console.print(length(bs), bs[0], bs[1], bs[2], bs[3])
console.print(text.from_bytes(bs) == s)
console.print(length("".to_bytes()), "".to_bytes())
file.write_bytes("`+p+`", bs)
let back:bytes = file.read_bytes("`+p+`")
console.print(back == bs)
console.print(text.from_bytes(back))
`)
	want := "4 231 148 178 97\ntrue\n0 []\nfalse\n甲a\n"
	if got != want || code != 0 {
		t.Fatalf("文本与那一档互换不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestBytesBase64 验证 bytes 与 base64 文本这一双的互转。期望串一律由 Go 的 `encoding/base64`
// 算出来再比对——不必手写 RFC 4648 那张表的顺序，错一个位置就红一条。
//
// 覆盖三处：一、`bytes` 里存的是二进制，`/w==` 解出的那个 0xFF 不是合法 UTF-8，
// 这一条照样返回那一个字节（`text.from_base64` 在同串字节上抛 10044 号，两条路的分界就在这里）；
// 二、文本那一侧与字节那一侧对同一段字节给出同一个结果；三、解码失败仍是 10014 号那句，不另设新码。
func TestBytesBase64(t *testing.T) {
	cases := [][]byte{
		{},
		{0},
		{255},
		{255, 0, 128},
		{0xFF, 0xFE, 0x41, 0x0D, 0x0A, 0xE7, 0xAC, 0xAC, 0x00, 0x80},
		[]byte("甲a"),
	}
	for _, raw := range cases {
		enc := base64.StdEncoding.EncodeToString(raw)
		var src strings.Builder
		fmt.Fprintf(&src, "let b:bytes = bytes.new(%d)\n", len(raw))
		for i, v := range raw {
			fmt.Fprintf(&src, "b[%d] = %d\n", i, v)
		}
		src.WriteString("console.print(b.to_base64())\n")
		fmt.Fprintf(&src, `let back:bytes = bytes.from_base64("%s")`+"\n", enc)
		src.WriteString("console.print(length(back), back)\n")
		got, code := run(t, src.String())
		var want strings.Builder
		fmt.Fprintf(&want, "%s\n%d %s\n", enc, len(raw), listOf(raw))
		if got != want.String() || code != 0 {
			t.Fatalf("%v 那一串不对：\n got %q 码 %d\nwant %q", raw, got, code, want.String())
		}
	}

	// 同一段字节，文本那一侧与字节那一侧给出同一个 base64 写法
	got, code := run(t, `
let s:string = "甲a"
console.print(text.to_base64(s), s.to_bytes().to_base64())
console.print(text.to_base64("a"), text.to_base64("ab"), text.to_base64("abc"), text.to_base64(""))
`)
	want := "55SyYQ== 55SyYQ==\nYQ== YWI= YWJj \n"
	if got != want || code != 0 {
		t.Fatalf("两条路的答复对不上：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 0xFF 那一串在文本那一侧抛 10044 号，在这一侧是三个字节里最普通的一个数据字节
	got, code = run(t, `let b:bytes = bytes.from_base64("/w==")
console.print(length(b), b[0])`)
	if got != "1 255\n" || code != 0 {
		t.Fatalf("非 UTF-8 的那一串要交回一个字节：got %q 码 %d", got, code)
	}

	for _, s := range []string{"YQ", "a=", "Y Q==", "YQ=A", "===="} {
		got, code = run(t, `console.print(bytes.from_base64("`+s+`"))`)
		if code != 1 || !strings.Contains(got, "10014）：文本读不成 base64") {
			t.Fatalf("%q 得到 %q 码 %d，要 10014 那一句", s, got, code)
		}
	}

	// 释放账目：两万圈各写一段 base64、各读回一块字节，接在名字上的那两块都应在语句尽头释放。
	// 名字要写在圈里——`console.print(f(), g())` 那种无名中转值眼下是"释放后同一槽位无法取回"，
	// 每圈都算进累计（`s.trim()` 同样摆法，量出来一样是每圈 32 字节），拿它当参照会误报
	if used := heapAt(t, `
let b:bytes = "甲a".to_bytes()
let t:string = b.to_base64()
for (i:int in 0 until 20000) {
    let s:string = b.to_base64()
    let back:bytes = bytes.from_base64(t)
    console.print(length(s), length(back))
}`); used > 4000 {
		t.Errorf("两万圈各写各读：累计 %d 字节，那两块接住的大概没拆", used)
	}
}

// TestBytesJoin 验证并块规则：几块连续摆成一块新的，顺序照清单、中间不插内容。
// 这一条是 web 端自动化测试框架的地基——TCP 上一段一段收来的 WebSocket 帧要连续拼接，
// 而 `bytes` 的长度在 `bytes.new` 那一步定下之后不再增减，"两块并成一块"需要专门的构造操作。
func TestBytesJoin(t *testing.T) {
	got, code := run(t, `
let a:bytes = "甲".to_bytes()
let b:bytes = "a".to_bytes()
console.print(bytes.join([a, b]))
console.print(bytes.join([]))
let z1:bytes = bytes.new(0)
let t1:bytes = "ab".to_bytes()
console.print(bytes.join([z1, t1]))
let t2:bytes = "ab".to_bytes()
let z2:bytes = bytes.new(0)
console.print(bytes.join([t2, z2]))
let z3:bytes = bytes.new(0)
let z4:bytes = bytes.new(0)
console.print(bytes.join([z3, z4]))
let head:bytes = bytes.new(2)
head[0] = 255
let m1:bytes = "甲".to_bytes()
console.print(bytes.join([head, m1]))
let xs:list<bytes> = [bytes.new(2), "甲".to_bytes()]
let all:bytes = bytes.join(xs)
console.print(length(xs), length(all), xs[0], all)
`)
	want := "[231, 148, 178, 97]\n[]\n[97, 98]\n[97, 98]\n[]\n[255, 0, 231, 148, 178]\n2 5 [0, 0] [0, 0, 231, 148, 178]\n"
	if got != want || code != 0 {
		t.Fatalf("并块不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 清单里那个位置是空句柄：与 `text.join` 同一条规则，10092 号
	got, code = run(t, `let none:bytes = null
let xs:list<bytes> = [none]
console.print(bytes.join(xs))`)
	if code != 1 || !strings.Contains(got, "10092）：对着空句柄取成员或调用方法") {
		t.Fatalf("空句柄那一格得到 %q 码 %d，要 10092 那一句", got, code)
	}

	// 二百五十块并起来：长度是各块之和，头一个位置与末一个位置各落在头一块与末一块上
	var many strings.Builder
	many.WriteString("let parts:list<bytes> = []\n")
	many.WriteString("for (i:int in 0 until 250) {\n    let one:bytes = bytes.new(3)\n    one[0] = 7\n    one[2] = i as byte\n    parts.add(one)\n}\n")
	many.WriteString("let all:bytes = bytes.join(parts)\nconsole.print(length(parts), length(all), all[0], all[747], all[749])\n")
	got, code = run(t, many.String())
	if got != "250 750 7 7 249\n" || code != 0 {
		t.Fatalf("二百五十块并一块：got %q 码 %d", got, code)
	}

	// 释放账目：清单留着不动，并出来的那一块没人接住就应在语句尽头释放。
	// 接结果的名字要写在圈里——无名中转值眼下是"释放后同一槽位无法取回"，每圈都算进累计
	if used := heapAt(t, `
let parts:list<bytes> = ["甲".to_bytes(), "a".to_bytes()]
for (i:int in 0 until 20000) {
    let all:bytes = bytes.join(parts)
    console.print(length(all))
}`); used > 4000 {
		t.Errorf("两万圈各并一块：累计 %d 字节，并出来的那一块大概没拆", used)
	}
}

// listOf 把一串字节写成 `console.print` 的那个样子：`[255, 0, 128]`，空的写 `[]`。
func listOf(raw []byte) string {
	parts := make([]string, len(raw))
	for i, v := range raw {
		parts[i] = strconv.Itoa(int(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// TestBytesInClassField 验证 bytes 进得了 `class` 的字段：写进去是交出、覆盖字段把换下来的那一块析构，
// 与容器适用同一套规则。struct 一侧不收引用类型，不归这一条管。
func TestBytesInClassField(t *testing.T) {
	got, code := run(t, `
class Holder {
    data:bytes
}
let h:Holder = new Holder(data: bytes.new(2))
h.data[0] = 7
console.print(length(h.data), h.data[0])
h.data = bytes.new(3)
console.print(h.data)
`)
	want := "2 7\n[0, 0, 0]\n"
	if got != want || code != 0 {
		t.Fatalf("字段那一头不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	if used := heapAt(t, `
class Holder {
    data:bytes
}
let total:int = 0
for (i:int in 0 until 20000) {
    let h:Holder = new Holder(data: bytes.new(16))
    h.data = bytes.new(8)
    total = total + length(h.data)
}
console.print(total)`); used > 4000 {
		t.Errorf("两万圈各换一次字段：累计 %d 字节，换下来的那一块大概没拆", used)
	}
}

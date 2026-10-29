// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// text_more_e2e_test.go 覆盖文本、数字、路径、环境与数学这几组里的各个成员：
// 从后往前找、单侧去空白、重复、文本与字节互换、base64、带进制的读与写、
// 正负号、固定随机种子、四条小数运算、取上级目录、是否绝对路径、换当前目录、UTC 偏移分钟数。
// 每条都是实际运行。list 的六条在 listops_e2e_test.go，目录的两条在 tree_more_e2e_test.go。

func TestTextFindTrimRepeat(t *testing.T) {
	got, code := run(t, `
let p:string = "C:/work/demo.nf"
console.print(p.index_of("."), p.last_index_of("."))
console.print(p.last_index_of("/"), p.last_index_of("nf"))
console.print(p.last_index_of("没有的东西"))
let line:string = "  甲,乙  "
console.print("|"+line.trim_start()+"|", "|"+line.trim_end()+"|")
console.print("|"+line.trim_start().trim_end()+"|")
let blank:string = "   "
console.print("|"+blank.trim_start()+"|", "|"+blank.trim_end()+"|", length(blank.trim_start()))
let one:string = "甲"
console.print(one.repeat(3), "|", one.repeat(0), "|", one.repeat(-2), "|")
console.print(length(("ab,").repeat(4)))
let hi:string = "你好"
console.print(hi.repeat(2), length(hi.repeat(2)))
`)
	want := "12 12\n7 13\n-1\n|甲,乙  | |  甲,乙|\n|甲,乙|\n|| || 0\n甲甲甲 |  |  |\n12\n你好你好 4\n"
	if got != want || code != 0 {
		t.Fatalf("往前找、去一头、重复几遍不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTextFindTrimHeap 度量这几条成员造出的中间块：两万圈各造一次，峰值不增长才说明
// repeat 翻倍过程的中间产物与 trim 返回的结果都被析构了。
func TestTextFindTrimHeap(t *testing.T) {
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let a:string = ("甲乙" + "丙丁").repeat(4)
    let b:string = ("  " + a + "  ").trim_start()
    let c:string = b.trim_end()
}
`); used > 4000 {
		t.Fatalf("两万圈各造三段，峰值 %d 字节：那几份中间块大概没拆", used)
	}
}

func TestTextBytesAndBase64(t *testing.T) {
	got, code := run(t, `
let s:string = "甲a"
let bs:bytes = s.to_bytes()
console.print(length(bs), bs[0], bs[1], bs[2], bs[3])
let back:string = text.from_bytes(bs)
console.print(back, length(back), back == s)
console.print(length("".to_bytes()), length(text.from_bytes("".to_bytes())))
console.print(text.to_base64("甲a"), text.from_base64(text.to_base64("甲a")))
console.print(text.to_base64("a"), text.to_base64("ab"), text.to_base64("abc"), text.to_base64(""))
let one:list<byte> = []
one.add(255)
console.print(length(one), one[0])
`)
	want := "4 231 148 178 97\n甲a 2 true\n0 0\n55SyYQ== 甲a\nYQ== YWI= YWJj \n1 255\n"
	if got != want || code != 0 {
		t.Fatalf("字节表与 base64 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	for _, s := range []string{"a", "ab", "====", "a=", "Y Q==", "YQ", "YQ=A"} {
		got, code = run(t, `console.print(text.from_base64("`+s+`"))`)
		if code != 1 || !strings.Contains(got, "10014）：文本读不成 base64") {
			t.Fatalf("%q 得到 %q 码 %d，要 10014 那一句", s, got, code)
		}
	}
	// 解码结果不是合法 UTF-8 的那一串：报 10044 号那句，与 text.from_bytes 一致
	got, code = run(t, `console.print(text.from_base64("/w=="))`)
	if code != 1 || !strings.Contains(got, "10044）：文件不是合法的 UTF-8") {
		t.Fatalf("非法 UTF-8 那一头得到 %q 码 %d，要 10044 那一句", got, code)
	}
}

func TestTextEscapeJSON(t *testing.T) {
	// 这一成员为 web 端自动化测试框架补充：每条 CDP 命令都是一段手工拼接的 JSON，
	// `Runtime.evaluate` 的 `expression` 位置放的是 JS 源码，漏转义一个引号或反斜杠就会发出坏请求。
	// 只转义 ASCII 范围：非 ASCII 原样保留（JSON 允许字符串内含 UTF-8），控制字符用短写法或 \u00XX。
	got, code := run(t, `
let a:string = "甲\"乙"
console.print(text.escape_json(a))
let b:string = "线\t尾\n"
console.print(text.escape_json(b))
let c:string = "反斜杠 \\ 与引号 \""
console.print(text.escape_json(c))
console.print(text.escape_json(""), length(text.escape_json("甲")))
let d:string = "控制\u0001字符"
console.print(text.escape_json(d), length(text.escape_json(d)), length(d))
let e:string = "尾\u001A"
console.print(text.escape_json(e), length(text.escape_json(e)), length(e))
console.print(text.escape_json("甲") == "甲")
`)
	want := "甲\\\"乙\n线\\t尾\\n\n反斜杠 \\\\ 与引号 \\\"\n 1\n控制\\u0001字符 10 5\n尾\\u001a 7 2\ntrue\n"
	if got != want || code != 0 {
		t.Fatalf("JSON 转义不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 只读不写：传入的原文还在，返回的是新造的一块（两万圈不留无人认领的内存）。
	if _, used := runHeap(t, `
let s:string = "甲\"乙"
let n:int = 0
while (n < 20000) {
    let e:string = text.escape_json(s)
    n = n + 1
}
console.print(length(s))
`); used > 4000 {
		t.Fatalf("两万圈各造一条转义，峰值 %d 字节：那几块新文本大概没拆", used)
	}
}

func TestTextFromBytesRefused(t *testing.T) {
	// 单个字节 0xFF 不是合法的 UTF-8：报 10044 号那句，与 file.read 一致
	got, code := run(t, `
let bs:bytes = bytes.new(1)
bs[0] = 255
let s:string = text.from_bytes(bs)
console.print(s)
`)
	if code != 1 || !strings.Contains(got, "10044）：文件不是合法的 UTF-8") {
		t.Fatalf("非法字节表得到 %q 码 %d，要 10044 那一句", got, code)
	}
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let bs:bytes = ("甲" + "乙").to_bytes()
    let t:string = text.from_bytes(bs)
}
`); used > 4000 {
		t.Fatalf("两万圈各走一趟字节表，峰值 %d 字节：那两块大概没拆", used)
	}
}

func TestRadix(t *testing.T) {
	got, code := run(t, `
console.print(text.to_int_in_base("ff", 16), text.to_int_in_base("FF", 16), text.to_int_in_base("-1a", 16))
console.print(text.to_int_in_base("1010", 2), text.to_int_in_base("777", 8), text.to_int_in_base("42", 10))
console.print(text.to_int_in_base("0", 16), text.to_int_in_base("-0", 2))
console.print(number.to_string_in_base(255, 16), number.to_string_in_base(-26, 16))
console.print(number.to_string_in_base(0, 2), number.to_string_in_base(10, 8), number.to_string_in_base(9223372036854775807, 16))
console.print(number.to_string_in_base(-9223372036854775807 - 1, 2))
console.print(text.to_int_in_base(number.to_string_in_base(255, 16), 16))
// 21 个八位 7 恰好是 2^63-1，正好卡在 int 的上界；负号那一侧到 2^63 为止
console.print(text.to_int_in_base("777777777777777777777", 8), text.to_int_in_base("-8000000000000000", 16))
`)
	want := "255 255 -26\n10 511 42\n0 0\nff -1a\n0 12 7fffffffffffffff\n-1000000000000000000000000000000000000000000000000000000000000000\n255\n9223372036854775807 -9223372036854775808\n"
	if got != want || code != 0 {
		t.Fatalf("进制那一对不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	for _, s := range []string{"g", "1.", " 1", "1 ", "-", "0x10", "10,0"} {
		got, code = run(t, `console.print(text.to_int_in_base("`+s+`", 16))`)
		if code != 1 || !strings.Contains(got, "10012）：文本读不成数字") {
			t.Fatalf("%q 得到 %q 码 %d，要 10012 那一句", s, got, code)
		}
	}
	for _, pair := range [][2]string{{"fffffffffffffffff", "16"}, {"7777777777777777777777", "8"}, {"9223372036854775808", "10"}} {
		got, code = run(t, `console.print(text.to_int_in_base("`+pair[0]+`", `+pair[1]+`))`)
		if code != 1 || !strings.Contains(got, "10013）：超出了 int 的值域") {
			t.Fatalf("%s 得到 %q 码 %d，要 10013 那一句", pair[0], got, code)
		}
	}
}

// TestMathSignSeed 验证正负号的三档返回值与固定种子的可复现性：输入 NaN 时返回 0 是语言定下的行为。
func TestMathSignSeed(t *testing.T) {
	got, code := run(t, `
console.print(math.sign(-12), math.sign(0), math.sign(7))
console.print(math.sign(-2.5), math.sign(0.0), math.sign(3.0))
let n:float = 0.0 - 1.0
let d:float = 0.0 - 1.0
console.print(math.sign(n / d - 1.0))
math.set_seed(20261011)
let a:int = math.random(0, 1000)
let b:float = math.random_float()
math.set_seed(20261011)
console.print(a == math.random(0, 1000), b == math.random_float())
math.set_seed(0)
console.print(math.random(0, 10) >= 0)
`)
	want := "-1 0 1\n-1 0 1\n0\ntrue true\ntrue\n"
	if got != want || code != 0 {
		t.Fatalf("正负号与种子不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestMathMoreCurves 覆盖四条小数运算：在已知点位上固定结果，换底与 e^x 按区间核对——
// 与 natural_log 那条的情况一致（每一步各舍入一次，末位相差几个 ulp）。
func TestMathMoreCurves(t *testing.T) {
	got, code := run(t, `
console.print(math.tangent(0.0), math.arctangent(0.0), math.log_base_10(1.0), math.exponential(0.0))
console.print(math.tangent(1.0) > 1.55, math.tangent(1.0) < 1.56)
console.print(math.arctangent(1.0) > 0.78, math.arctangent(1.0) < 0.79)
console.print(math.log_base_10(1000.0) > 2.999999999, math.log_base_10(1000.0) < 3.000000001)
console.print(math.log_base_10(0.001) > -3.000000001, math.log_base_10(0.001) < -2.999999999)
console.print(math.exponential(1.0) > 2.718281828, math.exponential(1.0) < 2.718281829)
console.print(math.exponential(-1.0) > 0.3678794411, math.exponential(-1.0) < 0.3678794412)
console.print(math.exponential(700.0) > 100000000000000000000.0)
console.print(math.exponential(-800.0) == 0.0)
console.print(math.exponential(800.0) > 1.0)
console.print(math.tangent(math.pi()), math.arctangent(1.0) * 4.0 > 3.14)
let big:float = 1000.0
let tiny:float = 0.0 - 1000.0
console.print(math.exponential(big) > 1.0, math.exponential(tiny) == 0.0)
console.print(math.exponential(math.natural_log(5.0)) > 4.9999999, math.exponential(math.natural_log(5.0)) < 5.0000001)
// 连续调用二十次：fscale 压在寄存器栈上的 k 值必须当场取走，否则 x87 栈八次就满，后续全部算成 NaN
let acc:float = 0.0
for (i:int in 0 until 20) {
    acc = acc + math.exponential(0.1)
}
console.print(acc > 22.0, acc < 22.2)
`)
	want := "0.0 0.0 0.0 1.0\ntrue true\ntrue true\ntrue true\ntrue true\ntrue true\ntrue true\ntrue\ntrue\ntrue\n-0.00000000000000012246063538223773 true\ntrue true\ntrue true\ntrue true\n"
	if got != want || code != 0 {
		t.Fatalf("四条小数运算不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

func TestPathMore(t *testing.T) {
	got, code := run(t, `
console.print(path.directory_name("C:/work/demo.nf"))
console.print(path.directory_name("C:/work/"))
console.print(path.directory_name("/etc/passwd"), path.directory_name("demo.nf"), path.directory_name(""))
console.print(path.directory_name("a\\b\\c.nf"))
console.print(path.is_absolute("F:/a"), path.is_absolute("/a"), path.is_absolute("\\\\srv\\share"))
console.print(path.is_absolute("a/b"), path.is_absolute(""), path.is_absolute("F/a"))
`)
	want := "C:/work/\nC:/\n/etc/  \na\\b\\\ntrue true true\nfalse false false\n"
	if got != want || code != 0 {
		t.Fatalf("取上一截与是不是从根起不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestEnvironmentAndOffset 验证切换当前目录：成功与失败各有一种答复（失败返回 false，不抛出）；
// UTC 偏移返回一个整数，其值随机器与时区变化，所以这里只验证"是整数且可调用"。
// current_directory 返回的是本机书写形式（反斜杠），因此比较的是它返回的那一串，
// 而不是传入的正斜杠写法。
func TestEnvironmentAndOffset(t *testing.T) {
	dir := t.TempDir()
	slash := filepath.ToSlash(dir)
	nf := strings.ReplaceAll(slash, "/", `\\`) // nf 的文本里 \\ 才是一个反斜杠
	got, code := run(t, `
let here:string = environment.current_directory()
console.print(environment.set_current_directory("`+slash+`"))
let now:string = environment.current_directory()
console.print(now == "`+nf+`")
console.print(environment.set_current_directory(now))
console.print(environment.current_directory() == here)
console.print(environment.set_current_directory(here))
console.print(environment.current_directory() == here)
console.print(environment.set_current_directory("`+slash+`/没有这一层"))
console.print(time.utc_offset() > -1440, time.utc_offset() < 1440)
`)
	want := "true\ntrue\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue true\n"
	if got != want || code != 0 {
		t.Fatalf("换当前目录与那几分钟不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// stdlib_more_e2e_test.go 覆盖标准库中除目录相关之外的成员：
// text.to_float、文本两头补位、dict.get、list 的头尾与清空、hashset 的三条集合运算、
// 五条数学成员、以及 console.print_error。每条都是实际运行：编译、启动子进程、比对打印出的文本。范围一侧（哪些类型能 union、返回什么）
// 由 internal/check 的 member_range_test.go 验证，这一份不覆盖。

// TestStrToFloat 覆盖可接受的形式：小数点两侧、指数两侧、符号、前导零，以及有效数字超过 17 位的那种。
// 大数打印就是二十几位，所以那两个极端值直接打印出来固定（布尔比对结果说明不了位数）。
func TestStrToFloat(t *testing.T) {
	got, code := run(t, `
console.print(text.to_float("1.5"), text.to_float("-0.25"), text.to_float(".5"), text.to_float("5."))
console.print(text.to_float("1.5e3"), text.to_float("-2E-1"), text.to_float("0"), text.to_float("0.1"))
console.print(text.to_float("1.5") + text.to_float("2.25"))
console.print(text.to_float("-1.5") < text.to_float("0.0"))
console.print(text.to_float("007"), text.to_float("-0"), text.to_float("0.00001234"))
console.print(text.to_float("1234567890123456789012"))
console.print(text.to_float("0.000000000000000000001"))
`)
	want := "1.5 -0.25 0.5 5.0\n" +
		"1500.0 -0.2 0.0 0.1\n" +
		"3.75\n" +
		"true\n" +
		"7.0 -0.0 0.00001234\n" +
		"1234567890123456800000.0\n" +
		"0.000000000000000000001\n"
	if got != want || code != 0 {
		t.Fatalf("小数读进来不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStrToFloatRefused 覆盖不可识别的形式，一律抛 10012 号，与 text.to_int 同一条规则：
// 前后含空白、"1e" 这种不完整的指数、多个小数点、空文本、只有符号都被拒绝。
func TestStrToFloatRefused(t *testing.T) {
	for _, s := range []string{"abc", "1e", " 1.5", "1.5 ", "1..2", "", "-", "1.5x", "1.5e"} {
		got, code := run(t, `console.print(text.to_float("`+s+`"))`)
		if code != 1 || !strings.Contains(got, "10012）：文本读不成数字") {
			t.Fatalf("%q 得到 %q 码 %d，要 10012 那一句", s, got, code)
		}
	}
}

// TestStringPad 覆盖两头补位：宽度按码点数计（与 length 同一单位）；已经够长时原样返回一份新文本；
// 填充比剩余宽度长时只补到齐数；填充是空文本时也返回一份新文本。
func TestStringPad(t *testing.T) {
	got, code := run(t, `
console.print("ab".pad_start(5, "-"), "|", "ab".pad_end(4, "0"), "|")
console.print("abc".pad_start(2, "x"), "|", "ab".pad_end(6, ""), "|")
console.print("ab".pad_start(4, "cd"), "|", "ab".pad_end(5, "cd"), "|")
let s:string = "甲乙".pad_end(5, "○")
console.print(s, "|", length(s))
console.print("".pad_start(3, "z"), "|")
`)
	want := "---ab | ab00 |\n" +
		"abc | ab |\n" +
		"cdab | abcdc |\n" +
		"甲乙○○○ | 5\n" +
		"zzz |\n"
	if got != want || code != 0 {
		t.Fatalf("两头补位不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestStringPadKeepsHeap 验证补位结果是当场新建的（扩容过程的中间产物也是）；两万圈不增长，说明无人引用的都已释放。
func TestStringPadKeepsHeap(t *testing.T) {
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let s:string = ("甲乙" + "丙丁").pad_start(40, "○")
}
`); used > 2000 {
		t.Fatalf("两万圈各补出一段，峰值 %d 字节：那几份翻倍用的文本大概没拆", used)
	}
}

// TestDictGet 键存在时与 d[键] 是同一种读法（读走）；键不存在时返回传入的默认值。
// 默认值是交出去的：没命中键的那一份归调用方，命中键时没用上的那一份当场析构。
func TestDictGet(t *testing.T) {
	got, code := run(t, `
let d:dict<string, int> = {}
d.add("甲", 7)
console.print(d.get("甲", -1))
console.print(d.get("乙", -1), length(d))
let w:dict<string, string> = {}
w.add("语言", "Nef")
console.print(w.get("语言", "没有"))
console.print(length(w))
`)
	want := "7\n" +
		"-1 1\n" +
		"Nef\n" +
		"1\n"
	if got != want || code != 0 {
		t.Fatalf("dict.get 不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestDictGetWithListValue 值类型是容器时同样适用：命中时读走那一只，未命中时接住返回的默认值。
func TestDictGetWithListValue(t *testing.T) {
	got, code := run(t, `
let d:dict<string, list<int>> = {}
d.add("有", [1, 2])
let back:list<int> = d.get("有", [7, 8, 9])
console.print(length(back), back[0])
console.print(d.contains("有"))
let miss:list<int> = d.get("无", [4])
console.print(length(miss), miss[0])
`)
	want := "2 1\ntrue\n1 4\n"
	if got != want || code != 0 {
		t.Fatalf("dict.get 取值那一头不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
	// 一个 list 由容器对象 40 字节加数据块 40 字节构成：每圈漏掉无人接住的默认值，峰值就是 1.6 MB。
	// 度量的是峰值；析构之后可以复用，所以这一条核对"没有涨上去"。
	if used := heapAt(t, `
let d:dict<string, list<int>> = {}
d.add("有", [1, 2])
for (i:int in 0 until 20000) {
    let back:list<int> = d.get("有", [3, 4, 5])
}
`); used > 2000 {
		t.Fatalf("两万圈每次撞着，峰值 %d 字节：那份没人接的默认值大概没拆", used)
	}
}

// TestListFirstLastClear 覆盖 first／last 与 clear。空容器取 first／last 走下标边界检查（10031）。
func TestListFirstLastClear(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [3, 5, 9]
console.print(xs.first(), xs.last(), length(xs))
xs.clear()
console.print(length(xs))
let ys:list<string> = ["甲", "乙"]
console.print(ys.first(), ys.last())
ys.clear()
console.print(length(ys))
`)
	want := "3 9 3\n0\n甲 乙\n0\n"
	if got != want || code != 0 {
		t.Fatalf("一头一尾与清空不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	for _, which := range []string{"first", "last"} {
		got, code := run(t, `
let xs:list<int> = []
console.print(xs.`+which+`())
`)
		if code != 1 || !strings.Contains(got, "10031）：下标越界") {
			t.Fatalf("空表取 %s 得到 %q 码 %d，要 10031 那一句", which, got, code)
		}
	}
}

// TestListClearBumpsVersion 清空是就地修改：正在遍历这个容器的循环，下一轮抛 10033 号。
func TestListClearBumpsVersion(t *testing.T) {
	got, code := run(t, `
let xs:list<int> = [1, 2, 3]
for (v:int in xs) {
    xs.clear()
}
`)
	if code != 1 || !strings.Contains(got, "10033）：遍历途中改了容器") {
		t.Fatalf("遍历途中清空得到 %q 码 %d，要 10033 那一句", got, code)
	}
}

// TestListClearFreesStrings 验证 clear 析构每个位置里的文本、没有残留：两万圈各放入一份当场创建的文本再清空，
// 峰值与"一个容器＋一份文本"同一档；每圈漏析构一个位置就会涨到几兆。
func TestListClearFreesStrings(t *testing.T) {
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let xs:list<string> = []
    xs.add("甲" + "乙")
    xs.clear()
}
`); used > 2000 {
		t.Fatalf("每圈装一份再清空，峰值 %d 字节：那一格里的文本大概没拆", used)
	}
}

// TestHashSetOps 三条集合运算各返回一个新的 set，两侧都不改动；顺序不保证，所以只核对长度与成员是否存在。
func TestHashSetOps(t *testing.T) {
	got, code := run(t, `
let a:hashset<int> = {1, 2, 3}
let b:hashset<int> = {3, 4}
let u:hashset<int> = a.union(b)
let i:hashset<int> = a.intersect(b)
let d:hashset<int> = a.difference(b)
console.print(length(u), length(i), length(d))
console.print(u.contains(1), u.contains(4), i.contains(3), i.contains(2))
console.print(d.contains(1), d.contains(3), length(a), length(b))
let s:hashset<string> = {}
let t:hashset<string> = {}
s.add("甲")
t.add("乙")
let w:hashset<string> = s.union(t)
console.print(length(w), w.contains("甲"), w.contains("乙"))
console.print(length(s.intersect(t)), length(s.difference(t)))
console.print(length(a.union(a)))
`)
	want := "4 1 2\n" +
		"true true true false\n" +
		"true false 3 2\n" +
		"2 true true\n" +
		"0 1\n" +
		"3\n"
	if got != want || code != 0 {
		t.Fatalf("集合三条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestHashSetTextElements 覆盖文本元素：新 set 里每个位置都是独立拷贝，两侧析构后那个元素仍可查。
// 两万圈自己并自己峰值不增长，才能说明"拷进新 set 的那一份"与"旧 set 里那一格"都没有漏。
func TestHashSetTextElements(t *testing.T) {
	got, code := run(t, `
let s:hashset<string> = {}
s.add("甲" + "乙")
s.add("丙" + "丁")
let u:hashset<string> = s.union(s)
console.print(length(u), u.contains("甲乙"), u.contains("丙丁"))
`)
	if code != 0 || got != "2 true true\n" {
		t.Fatalf("文本元素并自己得到 %q 码 %d", got, code)
	}
	if used := heapAt(t, `
for (i:int in 0 until 20000) {
    let s:hashset<string> = {}
    s.add("甲" + "乙")
    let u:hashset<string> = s.union(s)
}
`); used > 2000 {
		t.Fatalf("两万圈自己并自己，峰值 %d 字节：拷进新表的那一份或旧表那一圈没拆", used)
	}
}

// TestMathExtras 覆盖五条数学成员：特殊值不抛出、π 是固定值、正弦余弦落在已知点位、平方和为 1。
// ln 的末位与最高精度值相差几个 ulp（x87 用 log₂ 再乘 ln(2) 计算，两步各有一次舍入），
// 所以这里按区间核对，不断言最后两位。
func TestMathExtras(t *testing.T) {
	got, code := run(t, `
console.print(math.sine(0.0), math.cosine(0.0), math.natural_log(1.0))
console.print(math.pi())
console.print(math.sine(1.0) > 0.84, math.sine(1.0) < 0.85)
console.print(math.cosine(1.0) > 0.54, math.cosine(1.0) < 0.55)
console.print(math.natural_log(2.0) > 0.69, math.natural_log(2.0) < 0.7)
console.print(math.natural_log(0.0), math.natural_log(-1.0))
let half:float = 3.141592653589793 / 2.0
console.print(math.sine(half) > 0.99, math.sine(half) <= 1.0)
let s:float = math.sine(1.0)
let cc:float = math.cosine(1.0)
console.print(s * s + cc * cc > 0.9999999, s * s + cc * cc < 1.0000001)
`)
	want := "0.0 1.0 0.0\n" +
		"3.141592653589793\n" +
		"true true\n" +
		"true true\n" +
		"true true\n" +
		"-Inf NaN\n" +
		"true true\n" +
		"true true\n"
	if got != want || code != 0 {
		t.Fatalf("数学那五条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestMathRandomFloat 验证返回的数落在 [0,1) 内，且两万圈里至少出现过两个不同的数。
// out 统计越界次数；same 在遇到第二个不同的数时清成 0（初始按"全都相同"记）。
func TestMathRandomFloat(t *testing.T) {
	got, code := run(t, `
let out:int = 0
let first:float = math.random_float()
let same:int = 1
for (i:int in 0 until 2000) {
    let r:float = math.random_float()
    if (r < 0.0) {
        out = out + 1
    }
    if (r >= 1.0) {
        out = out + 1
    }
    if (r != first) {
        same = 0
    }
}
console.print(out, same)
`)
	if got != "0 0\n" || code != 0 {
		t.Fatalf("随机小数不对：got %q 码 %d，要 \"0 0\\n\"（没有越界、且见过不同的数）", got, code)
	}
}

// TestPrintError 验证 console.print_error 写入标准错误流、console.print 仍写标准输出流：重定向标准输出时，
// 错误输出不会混进文件。两个流分别核对，小数、容器等值也走这同一条打印路径。
func TestPrintError(t *testing.T) {
	r := exetest.Run(t, build(t, `
console.print_error("这句走标准错误")
console.print("这句走标准输出")
console.print_error(1, 2.5, "甲")
console.print(["甲", "乙"])
console.print_error(["丙"])
`), "t.exe")
	wantOut := "这句走标准输出\n[\"甲\", \"乙\"]\n"
	wantErr := "这句走标准错误\n1 2.5 甲\n[\"丙\"]\n"
	if r.Stdout != wantOut {
		t.Fatalf("标准输出多了或少了字：\n got %q\nwant %q", r.Stdout, wantOut)
	}
	if r.Stderr != wantErr {
		t.Fatalf("标准错误不对：\n got %q\nwant %q", r.Stderr, wantErr)
	}
	if r.Code != 0 {
		t.Fatalf("退出码是 %d，要 0", r.Code)
	}
}

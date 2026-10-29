// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// docs.go 是内建动作在编辑器里出现的那三样东西：中文一句、英文一句、用法示例一段。
//
// 为什么要这张表：`console.print`、`list.add` 这些动作在语言里没有写它们的那一行——它们活在
// `stdFuncs` 与 `stdMember` 里，悬停按声明位置回原文读注释那一条路对它们不成立。
// 屏幕上要有说明与例子，就得有个地方放着；放的地方仍然是一张 Go 表，与 `params.go` 那张
// 参数名表同一个键（落点名），同一条双向测试比齐。
//
// 三样内容各自的规矩：
//   - Zh 与站上 `site/stdlib/` 那十四页表格里的"说明"同一句，两边不一致由测试当场指出；
//   - En 是它的对应说法，不是逐字硬译，术语与站上英文记号一致；
//   - Eg 是能编译过去的 nf 写法，逐条交给 `nf check`（`docs_test.go` 的 `TestEveryStdExampleCompiles`）。
//     例子只要求编译过，不要求真跑：网络、进程、文件那几条真跑要搭沙盒。
//
// en: docs.go holds the three things a built-in action shows in the editor: one Chinese
// sentence, one English sentence, one usage example. Why this table exists: actions such as
// `console.print` and `list.add` have no line in the language where they are written - they
// live in stdFuncs and stdMember - so the path "read the comment above the declaration by
// position" cannot work for them. If the screen is to show an explanation and an
// example, they have to be stored somewhere, and that somewhere is still a Go table, keyed by
// the same dotted name as the parameter-name table in params.go, kept in step by the same
// two-way tests. The three fields each have their own rule: Zh is the same sentence as the
// "description" column of the fourteen pages under site/stdlib/, and a test names any
// disagreement between them; En is its counterpart rather than a word-for-word translation,
// with terminology matching the English notation on the site; Eg is nf that actually passes
// checking, each one handed to `nf check` (TestEveryStdExampleCompiles in docs_test.go).
// An example only has to compile, not run: the network, process and file actions would
// need a sandbox to execute.
package check

import "strings"

// StdDoc 是一条内建动作的三样说明。
//
// en: StdDoc is the three-part documentation of one built-in action.
type StdDoc struct {
	Zh string
	En string
	Eg string
}

var stdDocs = map[string]StdDoc{
	// —— console：屏幕输出与输入
	"console.print": {
		Zh: "可以传任意多个值，值之间输出一个空格，末尾输出换行。它是变长参数，那个参数上没有声明类型，所以单个不带类型的 null 无法传入，在编译期就会报错，不是运行期错误。",
		En: "Prints any number of values to standard output, separated by a space and followed by a newline. It is variadic and that parameter carries no declared type, so a bare null with no type cannot be passed: the compiler refuses it, it is not a runtime error.",
		Eg: `console.print("共 ", length("甲乙"), " 个码点")`,
	},
	"console.print_error": {
		Zh: "输出格式与打印到标准输出时完全相同，只是写入标准错误。把标准输出重定向到文件时，提示与进度不会混进文件里。",
		En: "Same format as console.print, but written to standard error.",
		Eg: `console.print_error("这一行只是给人看")`,
	},
	"console.read": {
		Zh: "等待一行输入，返回的内容不含末尾的换行符；没有可读内容时，返回 \"\"。一行最多读取 65536 字节，达到上限就停止，多出来的部分留在输入里，下一次调用继续读取。",
		En: "Waits for one line of input without the trailing newline; returns an empty text when nothing is left to read.",
		Eg: "let name: string = console.read()\nconsole.print(\"你好，\", name)",
	},

	// —— text：文本
	"text.to_int": {
		Zh: "把文本解析成整数；无法解析时抛 10012 号（文本读不成数字）。",
		En: "Parses an integer; throws 10012 when the text does not parse.",
		Eg: `let n: int = text.to_int("42")`,
	},
	"text.to_int_in_base": {
		Zh: "按进制把文本解析成整数：ff 以 16 为进制是 255。进制只能写成 2、8、10、16 四个数之一，而且必须是字面量，这一点在编译期就要求；除 0 到 9 外，a 到 z 与 A 到 Z 按 10 到 35 识别；不允许出现空白、0x 前缀和小数点。",
		En: "Reads an integer in the given base: ff in base 16 is 255. The base must be a literal 2, 8, 10 or 16.",
		Eg: `let v: int = text.to_int_in_base("ff", 16)`,
	},
	"text.to_float": {
		Zh: "把文本解析成小数：可选的正负号、中间一个小数点（.5 是 0.5、5. 是 5.0）、末尾可以带 e 或 E 加符号的指数。前后不跳过空白，要去空白先 trim；有效数字最多取 17 位，超过 float 范围的返回正负无穷，不抛出错误。",
		En: "Parses a float: optional sign, one decimal point, and an optional e/E exponent with a sign.",
		Eg: `let f: float = text.to_float("1.5e3")`,
	},
	"text.join": {
		Zh: "split 的反向操作：用分隔符把列表元素拼接成文本；接收的必须是 list<string>，这个要求编译期就会检查。",
		En: "The inverse of split: joins a list into a text. The argument must be a list<string>, checked at compile time.",
		Eg: `let line: string = text.join(["甲", "乙", "丙"], ", ")`,
	},
	"text.to_base64": {
		Zh: "先取文本的 UTF-8 字节，再每三个字节输出四个字符，不足三个的末尾用 = 补齐。",
		En: "Base64-encodes the text's UTF-8 bytes: four characters per three bytes, padded with = at the end.",
		Eg: `let code: string = text.to_base64("甲乙")`,
	},
	"text.from_base64": {
		Zh: "解码结果是文本。长度不是 4 的整数倍、等号出现在中间、含有字母表以外的字符，这三种情况都抛 10014 号。",
		En: "Decodes back to a text. A length that is not a multiple of four, an = in the middle, or a character outside the alphabet each throw 10014.",
		Eg: `let back: string = text.from_base64("5Li55Li6")`,
	},
	"text.escape_json": {
		Zh: "把文本写成 JSON 里引号中间的内容：\" 与 \\ 前面各加一个 \\，\\b、\\t、\\n、\\f、\\r 五个写成两个字符的短写法，其余小于 0x20 的控制字符写成 \\u00xx（数字用小写），别的一律原样保留。非 ASCII 不转成 \\u，\"甲\" 本身就是一段合法的 JSON。两端不加引号：它转换的是内容本身。",
		En: "Writes a text as the content between quotes in JSON: quote and backslash each get a backslash in front, \\b, \\t, \\n, \\f and \\r take their two-character short form, other control characters below 0x20 become \\u00xx with lowercase digits, and everything else stays as it is. Non-ASCII is not turned into \\u: \"甲\" is already valid JSON on its own. Neither end gets quotes: this converts the content itself.",
		Eg: `let quoted: string = text.escape_json("他说\"甲\"")`,
	},
	"text.from_bytes": {
		Zh: "to_bytes 的反向操作。传入的字节不是合法的 UTF-8 时抛 10044 号（文件不是合法的 UTF-8），与 file.read 遇到非法字节时是同一条错误。",
		En: "The inverse of to_bytes. Bytes that are not valid UTF-8 throw 10044.",
		Eg: "let b: bytes = bytes.new(3)\nlet s: string = text.from_bytes(b)",
	},

	// —— number 与 length：数值输出与长度
	"number.to_string": {
		Zh: "基础类型按各自的格式输出：int、byte、enum 输出十进制，boolean 输出 true 或 false，unicode 输出对应的字符，float 输出最短往返且必带小数点的写法。struct 与 class 返回 类型名 { 字段: 值, … }，字段按登记的顺序排列，嵌套一层就接着拼接进去，文本字段带引号，未指向任何对象的字段返回 null。三种容器与 object 返回的形状与 console.print 的输出完全相同。string 本身不用它：传进来只会多复制一份。",
		En: "Formats a basic value by its own rules: int, byte and enum as decimal, boolean as true or false, a code point as that character, a float in its shortest round-trip form and always with a decimal point. struct and class give Type { field: value, … } with the fields in the order they were declared, one level of nesting splices straight in, text fields keep their quotes and a field pointing at nothing gives null. The three containers and object come out exactly as console.print prints them. A string needs no help from it: passing one in only makes another copy.",
		Eg: `let tag: string = number.to_string(42)`,
	},
	"number.to_string_in_base": {
		Zh: "按进制把数值写成文本：number.to_string_in_base(255, 16) 得到 \"ff\"。进制参数与 text.to_int_in_base 是同一条要求：必须写成字面量，只能是 2、8、10、16。字母是小写；负数把 - 放在最前面，其余部分按绝对值书写；0 就是 \"0\"。",
		En: "Writes a number in the given base: number.to_string_in_base(255, 16) yields ff. The base follows the same rule as text.to_int_in_base.",
		Eg: `let hex: string = number.to_string_in_base(255, 16)`,
	},
	"length": {
		Zh: "有多少个字节。与三种容器、文本用同一个写法，不是点号成员。",
		En: "Length: code points for a text, elements for a container, bytes for a byte block.",
		Eg: `let n: int = length("甲乙")`,
	},

	// —— math：数学
	"math.absolute_value": {
		Zh: "传入 int 返回 int，传入 float 返回 float；byte 返回 int。对 int 的最小值取绝对值会溢出，抛 10021 号（整数运算溢出）。",
		En: "Returns int for an int and float for a float; taking the absolute value of the smallest int overflows and throws 10021.",
		Eg: `let d: int = math.absolute_value(-7)`,
	},
	"math.arctangent": {
		Zh: "反正切，返回的是弧度，取值在 -π/2 到 π/2 之间。正负无穷返回正负 π/2。",
		En: "Arctangent, returned in radians, between -pi/2 and pi/2.",
		Eg: `let a: float = math.arctangent(1.0)`,
	},
	"math.ceiling": {
		Zh: "向上取整，返回不小于输入的最小整数。",
		En: "Rounds up to the next integer and returns an int.",
		Eg: `let up: int = math.ceiling(1.2)`,
	},
	"math.cosine": {
		Zh: "余弦，参数是弧度不是角度。",
		En: "Cosine; the argument is radians, not degrees.",
		Eg: `let c: float = math.cosine(0.0)`,
	},
	"math.exponential": {
		Zh: "求 e 的指定次方。大于 709.782712893384 返回正无穷、小于 -745.1332191019411 返回 0.0、NaN 返回 NaN，都不抛出错误：超出 float 能容纳的最大值与低于最小值这两种情况，本来就由 IEEE 给出答复。",
		En: "e raised to the argument. Above 709.782712893384 it returns +Inf, below -745.1332191019411 it returns 0.0.",
		Eg: `let g: float = math.exponential(1.0)`,
	},
	"math.floor": {
		Zh: "向下取整，返回不超过输入的最大整数。",
		En: "Rounds down to the previous integer and returns an int.",
		Eg: `let dn: int = math.floor(1.8)`,
	},
	"math.log_base_10": {
		Zh: "以 10 为底的对数。0.0 返回负无穷、负数返回 NaN。",
		En: "Base-10 logarithm. 0.0 returns -Inf, a negative argument returns NaN.",
		Eg: `let t: float = math.log_base_10(100.0)`,
	},
	"math.maximum": {
		Zh: "两个值必须是同一类型，不做隐式转换；返回较大的那一个。",
		En: "Both arguments must have the same type, with no implicit conversion; returns the larger one.",
		Eg: `let big: int = math.maximum(3, 7)`,
	},
	"math.minimum": {
		Zh: "两个值必须是同一类型，不做隐式转换；返回较小的那一个。",
		En: "Both arguments must have the same type, with no implicit conversion; returns the smaller one.",
		Eg: `let small: int = math.minimum(3, 7)`,
	},
	"math.natural_log": {
		Zh: "自然对数。0.0 返回 -Inf、负数返回 NaN。结果与最精确的值在末几位上有差距：先调用机器的 log₂，再乘一次 ln(2)，两步各做一次舍入。",
		En: "Natural logarithm. 0.0 returns -Inf, a negative argument returns NaN.",
		Eg: `let l: float = math.natural_log(2.718281828459045)`,
	},
	"math.pi": {
		Zh: "圆周率 3.141592653589793。它是不带参数的函数，与 time.now() 一样：写在组名下面的成员只有函数这一种形式。",
		En: "Pi, 3.141592653589793. It is a function with no arguments: members under a group name only come in function form.",
		Eg: `let half: float = math.pi() / 2.0`,
	},
	"math.power": {
		Zh: "两个参数都是 int。指数为负抛 10012 号（指数不能是负的），结果超出 int 抛 10021 号。",
		En: "Both arguments are int. A negative exponent throws 10012 and a result too large for int throws 10021.",
		Eg: `let v: int = math.power(2, 10)`,
	},
	"math.random": {
		Zh: "包含起点、不包含终点，规则与 until 相同。范围为空（start ≥ end，两者相等也算空）时抛 10020 号，可以 catch。没有调用 math.set_seed 时，种子取自系统里的高精度计数器，每次运行的结果都不同；调用过之后，同一个程序两次运行会得到同一串结果。",
		En: "A random integer from low up to but not including high. Without math.set_seed the seed comes from the system's high-resolution clock.",
		Eg: `let pick: int = math.random(0, 6)`,
	},
	"math.random_float": {
		Zh: "0 到 1 之间的小数，包含 0、不包含 1。随机数种子与 math.random 共用同一个。",
		En: "A float in [0, 1). It shares one seed with math.random.",
		Eg: `let r: float = math.random_float()`,
	},
	"math.round": {
		Zh: "就近取整，.5 向数轴正方向进位。",
		En: "Rounds to the nearest integer; .5 carries toward the positive end of the axis. The result is an int.",
		Eg: `let n: int = math.round(2.5)`,
	},
	"math.set_seed": {
		Zh: "设置随机数种子。",
		En: "Sets the random seed; after it, two runs of the same program produce the same sequence.",
		Eg: "math.set_seed(2026)\nlet pick: int = math.random(0, 6)",
	},
	"math.sign": {
		Zh: "判断正、负还是零，返回 1、-1、0。int 与 float 都接受，返回的一律是 int；-0.0 按零处理。NaN 也返回 0：它既不是正也不是负。要判断 NaN 就写 x != x。",
		En: "Positive, negative or zero: 1, -1, 0. Accepts int and float and always returns int; -0.0 counts as zero. NaN returns 0 as well, since it is neither positive nor negative: to test for NaN write x != x.",
		Eg: `let s: int = math.sign(-3)`,
	},
	"math.sine": {
		Zh: "正弦，参数是弧度不是角度。要用角度就先算 角度 * math.pi() / 180.0。",
		En: "Sine; the argument is radians. For degrees compute degrees * math.pi() / 180.0 first.",
		Eg: `let v: float = math.sine(1.5707963267948966)`,
	},
	"math.square_root": {
		Zh: "平方根。输入为负数时返回 NaN，不抛出错误；0.0 返回 0.0。",
		En: "Square root. A negative input returns NaN instead of throwing.",
		Eg: `let root: float = math.square_root(9.0)`,
	},
	"math.tangent": {
		Zh: "正切，参数是弧度。在余弦为 0 的位置（π/2 这类弧度）返回正负无穷；±Inf、NaN 返回 NaN：这些都是除法本身给出的结果。",
		En: "Tangent in radians. Where the cosine is 0 it returns +/-Inf.",
		Eg: `let t: float = math.tangent(0.0)`,
	},

	// —— time：时间
	"time.calendar": {
		Zh: "把从 1970-01-01 零点（UTC）起算的秒拆成七个整数：year、month、day、hour、minute、second、weekday（0 = 星期一）。按 UTC 拆分，不看本机时区；秒数可以为负。",
		En: "Splits seconds since the 1970-01-01 UTC epoch into seven integers: year, month, day, hour, minute, second, weekday.",
		Eg: `let stamp: Calendar = time.calendar(time.now())`,
	},
	"time.elapsed": {
		Zh: "本程序从启动到此刻运行了多少秒，包含小数。",
		En: "How many seconds this program has been running, with the fraction.",
		Eg: `let took: float = time.elapsed()`,
	},
	"time.now": {
		Zh: "从 1970-01-01 零点（UTC）起算的秒数。",
		En: "Seconds since the 1970-01-01 UTC epoch.",
		Eg: `let at: int = time.now()`,
	},
	"time.sleep": {
		Zh: "等待指定的时长后再继续执行。负数按 0 计算，0 表示让出这一次时间片。",
		En: "Waits the given duration before continuing. A negative value counts as 0, and 0 yields the current time slice.",
		Eg: `time.sleep(20)`,
	},
	"time.utc_offset": {
		Zh: "本机所在时区比 UTC 快几分钟：东八区是 480，西五区是 -300。带夏令时的时区返回的是此刻的数值，所以同一个程序在夏天与冬天返回的数值可以不同。读取不到时区信息时返回 0，不抛出错误。",
		En: "How many minutes the local timezone is ahead of UTC: 480 for UTC+8, -300 for UTC-5. A timezone that observes summer time reports the offset in effect right now, so the same program can answer differently in summer and winter. When the timezone cannot be read it returns 0 and throws nothing.",
		Eg: `let offset: int = time.utc_offset()`,
	},

	// —— file：读写文件
	"file.append": {
		Zh: "在尾部追加，文件不存在时直接创建；失败同样是 10042 号。",
		En: "Appends at the end, creating the file when it does not exist; a failure is 10042 as well.",
		Eg: `file.append("note.txt", "第二行\n")`,
	},
	"file.copy": {
		Zh: "整个复制一遍，目标已存在就被覆盖。源文件无法打开是 10041，目标写入失败（包括目标本身是一个目录）是 10042。",
		En: "Copies the whole file, overwriting an existing target. An unreadable source is 10041, a failed write is 10042.",
		Eg: `file.copy("a.txt", "b.txt")`,
	},
	"file.create_directories": {
		Zh: "把缺失的各级目录一次创建完整。真=这条路径上的目录现在齐全了（本来就齐全的也算真），假=创建不出来（中间有一级同名的是文件、或者不允许写入）。不抛出错误。",
		En: "Creates every missing level at once. True means the path's directories now exist, false means they could not be created; it never throws.",
		Eg: `let made: boolean = file.create_directories("out/report/2026")`,
	},
	"file.create_directory": {
		Zh: "创建指定的目录。真=这一次新建出来了，假=已经存在（同名的路径即使指向一个文件，也算存在）。上一级目录不存在时抛 10046 号，不会连续向上级创建。",
		En: "Creates one directory. True when it was created now, false when it already existed. A missing parent throws 10046.",
		Eg: `let one: boolean = file.create_directory("out")`,
	},
	"file.exists": {
		Zh: "只询问不抛出错误；路径本身无法提交给系统（空串、本机放不下的长度）时仍然抛 51，报的是“写得不对”，不是“不存在”。",
		En: "Asks without throwing; a path the system cannot take still throws 51 — malformed, not missing.",
		Eg: `let here: boolean = file.exists("a.txt")`,
	},
	"file.is_directory": {
		Zh: "判断是否为目录；路径根本不存在时也返回假，只询问不抛出错误。",
		En: "Tells whether the path is a directory; a path that does not exist returns false, and it never throws.",
		Eg: `let isDir: boolean = file.is_directory("out")`,
	},
	"file.list": {
		Zh: "列出该目录里的名字，不含 . 与 ..，顺序不保证。",
		En: "Lists the names inside a directory, without . and .., in no guaranteed order.",
		Eg: `let names: list<string> = file.list(".")`,
	},
	"file.list_paths": {
		Zh: "同样是列出目录里的名字，只是每个名字前面带上目录，取到之后可以直接打开。",
		En: "Same listing, but each name carries the directory in front, so a result can be opened directly.",
		Eg: `let paths: list<string> = file.list_paths(".")`,
	},
	"file.modified": {
		Zh: "最后一次写入的时间，按从 1970-01-01 起算的秒计，与 time.now() 用同一个基准。",
		En: "The last write time, counted in seconds from 1970-01-01, the same basis as time.now().",
		Eg: `let when: int = file.modified("a.txt")`,
	},
	"file.read": {
		Zh: "读取整个文件；读取失败、路径无法提交给系统分别是 10041（读不到这个文件）、10051（这条路径本机用不了），内容不是合法 UTF-8 时是 10044。",
		En: "Reads a whole file: 10041 when the read fails, 10051 when the path cannot be given to the system, 10044 when the content is not valid UTF-8.",
		Eg: `let body: string = file.read("a.txt")`,
	},
	"file.read_bytes": {
		Zh: "按字节读取整个文件，不校验编码、不丢弃 BOM；传入目录时也是 10041。",
		En: "Reads a whole file as bytes, with no encoding check and no BOM stripped; a directory is 10041 too.",
		Eg: `let raw: bytes = file.read_bytes("a.bin")`,
	},
	"file.read_from": {
		Zh: "从第 at 个字节开始读取，最多读取 count 个字节；位置越过末尾时返回空文本。",
		En: "Reads from byte at, at most count bytes; a position past the end returns an empty text.",
		Eg: `let head: string = file.read_from("a.txt", 0, 16)`,
	},
	"file.readonly": {
		Zh: "系统里的那个只读标记，询问的就是标记本身。",
		En: "The operating system's read-only flag, asked for as the flag itself.",
		Eg: `let ro: boolean = file.readonly("a.txt")`,
	},
	"file.remove": {
		Zh: "删除文件；删除失败抛 10041 号。",
		En: "Deletes a file; a failed delete throws 10041.",
		Eg: `file.remove("a.txt")`,
	},
	"file.remove_directory": {
		Zh: "删除一个空目录；里面有内容就抛 10041 号，报的是 删不掉这个目录。",
		En: "Deletes an empty directory; if anything is inside it throws 10041 saying the directory could not be removed.",
		Eg: `file.remove_directory("out")`,
	},
	"file.remove_tree": {
		Zh: "把里面的内容一并删除。先删除文件，再逆着发现顺序删除目录，处理到上级目录时它里面已经空了，最后删除根目录自身。里面的内容删除失败就抛出错误：文件与目录都抛 10041 号，与 file.remove、file.remove_directory 的报错相同；根不存在、或者根不是一个目录时抛 10043 号。",
		En: "Deletes the contents along with the tree: files first, then directories against the order they were found, so each parent is already empty when its turn comes, and the root itself last. A failed delete inside throws: files and directories both give 10041, the same sentences file.remove and file.remove_directory use, while a root that does not exist or is not a directory gives 10043.",
		Eg: `file.remove_tree("out")`,
	},
	"file.rename": {
		Zh: "重命名，或者在同一个卷里移动位置；新名字已被占用、旧名字不存在都是 10047 号。",
		En: "Renames, or moves within the same volume; an occupied target or a missing source is 10047.",
		Eg: `file.rename("old.txt", "new.txt")`,
	},
	"file.size": {
		Zh: "有多少个字节。路径不存在时是 10045，传入目录时是 10043。",
		En: "How many bytes the file holds. A missing path is 10045, a directory is 10043.",
		Eg: `let bytes_of_a: int = file.size("a.txt")`,
	},
	"file.walk_paths": {
		Zh: "列出这个目录之下的每一项，目录自身也算一条，每一条都是取到之后可以直接打开的路径。. 与 .. 不在内，根目录自身也不在（它已经在参数里了），先后顺序按系统返回的次序。向下遍历到第 64 层就停止：链接点与挂载点可以构造出一条遍历不完的循环，宁可限制深度，也不让程序一直遍历下去。",
		En: "Lists every entry under a directory, the directory itself counted once, each one a path that can be opened as returned. . and .. are left out, and so is the root a second time (it is already the argument); the order follows what the system hands back. The walk stops 64 levels down: link points and mount points can describe a walk that never ends, and a depth limit is preferred over a program that walks forever.",
		Eg: `let every: list<string> = file.walk_paths(".")`,
	},
	"file.write": {
		Zh: "写出整个文件，原有的内容被覆盖；写入失败抛 10042 号。",
		En: "Writes a whole file, replacing whatever was there; a failed write throws 10042.",
		Eg: `file.write("a.txt", "第一行\n")`,
	},
	"file.write_at": {
		Zh: "从第 at 个字节开始覆盖写入，后面的内容不变；文件不存在时不创建。",
		En: "Overwrites starting at byte at, leaving the rest untouched; it does not create a missing file.",
		Eg: `file.write_at("a.txt", 0, "甲")`,
	},
	"file.write_bytes": {
		Zh: "把整块 bytes 按字节写出，原有内容被覆盖、文件不存在时新建。",
		En: "Writes a whole bytes block, replacing existing content and creating the file when it is missing.",
		Eg: `file.write_bytes("a.bin", bytes.new(4))`,
	},

	// —— path：路径
	"path.directory_name": {
		Zh: "取上一级目录，连同分隔符：C:/work/demo.nf 得到 C:/work/，反斜杠写法 a\\b\\c.nf 得到 a\\b\\。整条路径里没有分隔符时返回空文本。末尾本来就带分隔符时（C:/work/）返回 C:/：返回的是这个目录所在的上一级。",
		En: "Takes the parent directory including its separator; a path with no separator yields an empty text.",
		Eg: `let parent: string = path.directory_name("C:/work/demo.nf")`,
	},
	"path.exists": {
		Zh: "判断文件或目录是否存在，只询问不抛出错误；空串这类无法提交给系统的路径抛 51。",
		En: "Tells whether a file or directory exists without throwing; a path the system cannot take, such as an empty string, throws 51.",
		Eg: `let there: boolean = path.exists("C:/work")`,
	},
	"path.extension": {
		Zh: "取最后一个点之后的部分，没有点时返回 \"\"。",
		En: "Takes what follows the last dot, or an empty text when there is none.",
		Eg: `let ext: string = path.extension("demo.nf")`,
	},
	"path.file_name": {
		Zh: "取最后一段，包含扩展名；整条都是分隔符时返回 \"\"。",
		En: "Takes the last segment, extension included; a path made only of separators yields an empty text.",
		Eg: `let leaf: string = path.file_name("C:/work/demo.nf")`,
	},
	"path.is_absolute": {
		Zh: "判断是否为绝对路径。第一个字符是斜杠（两种斜杠都算）时一定是；前两个字符是一个字母加一个冒号时也是，F:/a、F:、F:a 都算：F:a 这种写法在本机就是“在这个盘上从当前目录起”。只查看写法，不查看盘是否存在：盘是否存在要问 path.exists 与 file.exists。",
		En: "Tells whether a path is absolute. A leading slash of either kind always is; so is a letter followed by a colon in the first two characters, where F:/a, F: and F:a all count and F:a on this machine means from the current directory of that drive. Only the spelling is looked at, never whether the drive exists: that is what path.exists and file.exists are for.",
		Eg: `let absolute: boolean = path.is_absolute("F:/a")`,
	},
	"path.join": {
		Zh: "拼接一条路径，分隔符按本机习惯补充；它只处理字符串，不访问系统。",
		En: "Joins a path, adding separators as this machine writes them; it only touches strings, not the system.",
		Eg: `let full: string = path.join("C:/work", "demo.nf")`,
	},

	// —— environment：环境变量与当前目录
	"environment.current_directory": {
		Zh: "返回当前在哪个目录下运行。返回的是本机习惯的写法（Windows 上是反斜杠）。",
		En: "Returns the directory the program runs in, written the way this machine does it.",
		Eg: `let here: string = environment.current_directory()`,
	},
	"environment.get": {
		Zh: "读取一个环境变量。不存在这个变量时抛 10061 号（没有这个环境变量），不返回 \"\"，也不返回 null。",
		En: "Reads an environment variable. A variable that does not exist throws 10061 rather than giving an empty text or null.",
		Eg: `let windir: string = environment.get("SystemRoot")`,
	},
	"environment.set": {
		Zh: "设置一个环境变量，只对本程序有效。",
		En: "Sets an environment variable, for this program only.",
		Eg: `environment.set("NEF_DEMO", "甲")`,
	},
	"environment.set_current_directory": {
		Zh: "切换到指定目录。真=切换成功，假=没有切换成功（路径不存在，或者它不是一个目录），不抛出错误：这个函数常用于试探性地切换，要判断结果就查看返回的 boolean。C:/Temp/a 与 C:\\\\Temp\\\\a 是同一个目录，两种分隔符都接受。",
		En: "Changes the current directory. True when it changed, false when it did not; it never throws.",
		Eg: `let moved: boolean = environment.set_current_directory("F:/")`,
	},
	"environment.unset": {
		Zh: "删除一个环境变量，也只对本程序有效。变量本来就不存在时不做任何事、不抛出错误；名字非法到系统调用不接受（比如带 =）才抛 10060 号。",
		En: "Removes an environment variable, again for this program only; removing one that is not there does nothing and throws nothing.",
		Eg: `environment.unset("NEF_DEMO")`,
	},

	// —— regular_expression：正则表达式
	"regular_expression.find": {
		Zh: "匹配到的全部片段，按出现的先后顺序排列。",
		En: "Every match, in the order they appear.",
		Eg: `let hits: list<string> = regular_expression.find("甲1乙2", "[0-9]")`,
	},
	"regular_expression.match": {
		Zh: "pattern 能否在 text 里匹配成功。",
		En: "Whether the pattern matches anywhere in the text.",
		Eg: `let ok: boolean = regular_expression.match("甲1乙", "[0-9]")`,
	},
	"regular_expression.replace": {
		Zh: "把 text 里每一处非空的匹配都换成 replacement，返回新的文本。",
		En: "Replaces every match with the given text and returns a new text.",
		Eg: `let out: string = regular_expression.replace("甲1乙2", "[0-9]", "_")`,
	},
	"regular_expression.split": {
		Zh: "按表达式切分。",
		En: "Splits the text wherever the pattern matches.",
		Eg: `let parts: list<string> = regular_expression.split("甲,乙;丙", "[,;]")`,
	},

	// —— json：解析 JSON
	"json.at": {
		Zh: "取出对象里该键对应的值。取出即读走：取过一次之后，那个位置就是 null。",
		En: "Takes the value under a key in an object. It is a read-out: after one take, that position holds null.",
		Eg: `let tree: Json = json.parse("{\"a\": 1}")` + "\n" + `let one: Json = json.at(tree, "a")`,
	},
	"json.count": {
		Zh: "数组有多少个元素、对象有多少个成员；其他种类返回 0。",
		En: "How many members a node has: elements for an array, keys for an object, 0 for anything else.",
		Eg: `let tree: Json = json.parse("[1, 2]")` + "\n" + `let n: int = json.count(tree)`,
	},
	"json.explain": {
		Zh: "返回一句说明，带行与列，例如 第 1 行第 15 列：这里该是一个值。文本本身没有问题时返回空文本。",
		En: "One sentence with the line and column, such as line 1 column 15: a value belongs here. An empty text when the text is fine.",
		Eg: `let why: string = json.explain("{")`,
	},
	"json.has": {
		Zh: "判断对象里有没有这个键。只询问，不改动那个位置。",
		En: "Whether an object has that key.",
		Eg: `let tree: Json = json.parse("{\"a\": 1}")` + "\n" + `let seen: boolean = json.has(tree, "a")`,
	},
	"json.item": {
		Zh: "数组里指定位置的元素。同样是读走。位置为负、或者不小于个数时，返回 null。",
		En: "Takes the value at a position in an array, also a read-out. A negative index, or one at or past the count, gives null.",
		Eg: `let tree: Json = json.parse("[1, 2]")` + "\n" + `let second: Json = json.item(tree, 1)`,
	},
	"json.keys": {
		Zh: "返回对象的全部键，顺序依照原文。不是对象时返回空列表。返回的是拷贝，树里的键还在原来的位置。",
		En: "Returns every key of an object, in the order they appear in the text; a node that is not an object returns an empty list. The result is a copy: the keys stay where they are in the tree.",
		Eg: `let tree: Json = json.parse("{\"a\": 1}")` + "\n" + `let names: list<string> = json.keys(tree)`,
	},
	"json.kind": {
		Zh: "判断一个值是哪一类：null、boolean、number、string、array、object 六个词之一。",
		En: "Returns which kind this value is, one of the six words null, boolean, number, string, array, object.",
		Eg: `let tree: Json = json.parse("[]")` + "\n" + `let which: string = json.kind(tree)`,
	},
	"json.parse": {
		Zh: "解析成功时返回树根，解析失败时返回 null。想知道为什么读取失败，紧接着用同一段文本调用 json.explain。",
		En: "Returns the tree root when the text parses and null when it does not. To learn why, call json.explain with the same text right after.",
		Eg: `let tree: Json = json.parse("{\"a\": 1}")`,
	},
	"json.to_boolean": {
		Zh: "只有 true 与 false 能取到值，其他情况返回默认值。",
		En: "Reads the node as a boolean, falling back to the second argument when it is not a boolean.",
		Eg: `let tree: Json = json.parse("true")` + "\n" + `let flag: boolean = json.to_boolean(tree, false)`,
	},
	"json.to_integer": {
		Zh: "只有整数形态的数能取到值（3、-12），其他情况返回默认值。",
		En: "Reads the node as an integer, falling back to the second argument when it is not an integer.",
		Eg: `let tree: Json = json.parse("7")` + "\n" + `let n: int = json.to_integer(tree, 0)`,
	},
	"json.to_number": {
		Zh: "两种形态的数都能取到值，3 也算作 3.0，其他情况返回默认值。",
		En: "Reads the node as a float, falling back to the second argument when it is not a float.",
		Eg: `let tree: Json = json.parse("7")` + "\n" + `let f: float = json.to_number(tree, 0.0)`,
	},
	"json.to_text": {
		Zh: "返回文本类型的值的内容，返回的是一份拷贝。其他种类返回 null。",
		En: "Reads the node as a text; a node that is not text gives an empty text.",
		Eg: `let tree: Json = json.parse("\"甲\"")` + "\n" + `let s: string = json.to_text(tree)`,
	},

	// —— bytes：字节块
	"bytes.from_base64": {
		Zh: "b.to_base64() 的反向操作。外形上的三条检查与文本一侧完全一致，解出来的字节原样留着，不校验编码。",
		En: "The inverse of b.to_base64(). The three shape checks match the text side exactly; the decoded bytes are kept as they are, with no encoding check.",
		Eg: `let raw: bytes = bytes.from_base64("5Li55Li6")`,
	},
	"bytes.join": {
		Zh: "把列表里的多块字节按顺序拼接成一块新的，中间不插入任何内容。长度确定之后不再增减，而网络上一段一段接收的数据需要连续存放，这个函数就是“两块合并成一块”的入口。列表中任一元素为空句柄时抛 10092 号，与 text.join 是同一句错误；空列表返回空的字节块。要插入分隔符，就把分隔符自己也做成一块放进列表。",
		En: "Concatenates the blocks in a list into one new block, inserting nothing between them. The length is settled once, and data received off the network piece by piece has to be kept contiguous: this is how two blocks become one. An element that is a null handle throws 10092, the same sentence text.join uses, and an empty list gives an empty block. To insert a separator, make the separator a block of its own and put it in the list.",
		Eg: `let whole: bytes = bytes.join([bytes.new(2), bytes.new(2)])`,
	},
	"bytes.new": {
		Zh: "创建一块指定长度的字节块，内容先全部置零。请求负数长度时抛 10030 号（bytes.new 要的字节数不能是负数）。",
		En: "Creates a block of the given length, zero-filled. A negative length throws 10030.",
		Eg: `let buf: bytes = bytes.new(64)`,
	},
	"bytes.slice": {
		Zh: "截取一段并另外创建一块字节块，含起点、不含终点。边界与 list.slice、string.slice 用同一套判断：起点为负、end 超出长度、start > end 都抛 10031 号；start == end 返回空的字节块。",
		En: "Takes [start, end) into a new block, with the same boundary rules as list.slice and string.slice.",
		Eg: `let head: bytes = bytes.new(8).slice(0, 4)`,
	},
	"bytes.to_base64": {
		Zh: "把这一块字节按 RFC 4648 的标准表写成 Base64 编码文本。",
		En: "Writes the block as Base64 text using RFC 4648's standard alphabet.",
		Eg: `let code: string = bytes.new(3).to_base64()`,
	},

	// —— net 与 http：网络
	"net.connect": {
		Zh: "建立连接。地址参数写域名或写 IP 都可以。10101 连不上、10102 超时、10103 解析不到这个名字、10104 地址或端口不合法。",
		En: "Opens a connection. The host is dotted-decimal text; the port and the timeout in milliseconds are integers.",
		Eg: `let sock: Socket = net.connect("127.0.0.1", 8080, 1000)`,
	},
	"net.listen": {
		Zh: "在本机监听端口，等待连接进来。端口已被占用时也是 10104。",
		En: "Listens on a local port for incoming connections; a port already in use throws 10104.",
		Eg: `let gate: Listener = net.listen(8080)`,
	},
	"http.get": {
		Zh: "发送一个 GET 请求，等待整个响应接收完成。",
		En: "Sends a GET request and returns its status, headers and body.",
		Eg: `let page: Response = http.get("https://example.com/", 5000)`,
	},
	"http.post": {
		Zh: "发送一个 POST 请求，传入的字节块就是发送出去的正文。",
		En: "Sends a POST request with the given bytes as the body, unchanged.",
		Eg: `let page: Response = http.post("https://example.com/", bytes.new(2), 5000)`,
	},

	// —— process：进程
	"process.start": {
		Zh: "启动一个程序。这条命令行原样交给系统：语言不负责拆分参数，也不负责添加引号，所以路径里带空格时自己写成 \"C:/Program Files/…/chrome.exe\"，参数之间用一个空格隔开。命令行是空字符串、长度超过内部缓冲区、被系统拒绝启动，都是 10111 号（起不来那个程序），消息里带上系统给出的原始号码。",
		En: "Starts a program. The command line is handed to the system as written: the language neither splits arguments nor adds quotes.",
		Eg: `let child: Process = process.start("cmd /c ver")`,
	},
	"process.wait": {
		Zh: "一直等到子进程运行结束，返回子进程的退出码。语言不代替使用者判断“0 才算成功”，要判断就自己写 if (p.wait() != 0)。等待失败、或者读取不到退出码时是 10110 号（等不到那个程序、问不到那个程序的退出码），也带上系统号码。这个名字已经失效时是 10092 号。",
		En: "Waits until the child exits and returns its own exit code. The language does not decide for you that 0 means success: write if (p.wait() != 0) to make that judgement. A wait that fails, or an exit code that cannot be read, is 10110 (cannot wait for that program, cannot read that program's exit code) and carries the system's number too. A name that is no longer a handle gives 10092.",
		Eg: `let child: Process = process.start("cmd /c ver")` + "\n" + `let code: int = child.wait()`,
	},
	"process.close": {
		Zh: "不再等待：子进程还在运行就把它结束，关闭句柄，这个名字当场变成空句柄。对已经运行结束的进程再调用一次也没有影响：系统只返回一句“这个进程已经结束了”，该函数不检查系统的返回值。",
		En: "Stops waiting: a running child is terminated, the handle is closed, and the name becomes a null handle right there.",
		Eg: `let child: Process = process.start("cmd /c ver")` + "\n" + `child.close()`,
	},

	// —— list：列表
	"list.add": {
		Zh: "在末尾添加一个元素。",
		En: "Appends one element at the end.",
		Eg: "let xs: list<int> = [1, 2]\nxs.add(3)",
	},
	"list.add_all": {
		Zh: "把另一份列表整个追加到末尾，来源那一份不发生变化。",
		En: "Appends another list at the end; the source list is unchanged.",
		Eg: "let xs: list<int> = [1, 2]\nxs.add_all([3, 4])",
	},
	"list.clear": {
		Zh: "全部清空，带出去的元素在语句尽头释放。",
		En: "Empties the list; the elements taken out are destroyed at the end of the statement.",
		Eg: "let xs: list<int> = [1, 2]\nxs.clear()",
	},
	"list.first": {
		Zh: "取第一个位置的元素；空列表上抛 10031 号。",
		En: "Takes the first element; on an empty list it throws 10031.",
		Eg: "let xs: list<int> = [1, 2]\nlet head: int = xs.first()",
	},
	"list.index_of": {
		Zh: "查找位置，返回第一个相等元素的位置；找不到时返回 -1。",
		En: "Searches by value and returns the position of the first equal element, or -1 when there is none.",
		Eg: "let xs: list<int> = [1, 2]\nlet at: int = xs.index_of(2)",
	},
	"list.insert_at": {
		Zh: "插入到某个位置，那个位置与之后的元素一起向后挪。",
		En: "Inserts at a position, shifting that element and everything after it one place back.",
		Eg: "let xs: list<int> = [1, 2]\nxs.insert_at(0, 9)",
	},
	"list.last": {
		Zh: "取最后一个位置的元素；空列表上抛 10031 号。",
		En: "Takes the last element; on an empty list it throws 10031.",
		Eg: "let xs: list<int> = [1, 2]\nlet tail: int = xs.last()",
	},
	"list.remove": {
		Zh: "删除一个元素。删除第一个与它相等的元素。",
		En: "Removes the first element equal to it.",
		Eg: "let xs: list<int> = [1, 2]\nxs.remove(2)",
	},
	"list.remove_at": {
		Zh: "从某个位置移除，返回被移除的元素。",
		En: "Removes the element at a position and returns it.",
		Eg: "let xs: list<int> = [1, 2]\nlet taken: int = xs.remove_at(0)",
	},
	"list.reverse": {
		Zh: "整体倒序，就地颠倒顺序。",
		En: "Reverses the list in place.",
		Eg: "let xs: list<int> = [1, 2]\nxs.reverse()",
	},
	"list.slice": {
		Zh: "取出一段，含起点不含终点，另外创建一份新列表。",
		En: "Takes [start, end) into a new list.",
		Eg: "let xs: list<int> = [1, 2, 3]\nlet part: list<int> = xs.slice(1, 3)",
	},
	"list.sort": {
		Zh: "排先后。就地排序，按元素自己的先后关系比较。",
		En: "Sorts in place using the elements' own ordering.",
		Eg: "let xs: list<int> = [2, 1]\nxs.sort()",
	},
	"list.to_set": {
		Zh: "去重，返回一份新的 hashset。",
		En: "Deduplicates and returns a new hashset.",
		Eg: "let xs: list<int> = [1, 1]\nlet only: hashset<int> = xs.to_set()",
	},

	// —— dict：字典
	"dict.add": {
		Zh: "添加一个元素。添加一个键与它的值；键已经存在时是编译错误，要改值就写 d[键] = 值。",
		En: "Adds a key with its value; an existing key is a compile error — write d[key] = value to change it.",
		Eg: "let d: dict<string, int> = {}\nd.add(\"甲\", 1)",
	},
	"dict.contains": {
		Zh: "是否存在。问有没有那一个键，只询问不抛出错误。",
		En: "Asks whether a key is present, without throwing.",
		Eg: "let d: dict<string, int> = {}\nlet has: boolean = d.contains(\"甲\")",
	},
	"dict.get": {
		Zh: "读取一个键，不存在时返回默认值。读取一个键的内容，不存在时返回默认值；默认值即使没用上也会被交出。",
		En: "Reads a key, returning the fallback when it is absent; the fallback is handed over even when it is not used.",
		Eg: "let d: dict<string, int> = {}\nlet n: int = d.get(\"甲\", 0)",
	},
	"dict.keys": {
		Zh: "取全部键，返回一份新的 list<K>。",
		En: "Returns a new list<K> of every key.",
		Eg: "let d: dict<string, int> = {}\nlet names: list<string> = d.keys()",
	},
	"dict.remove": {
		Zh: "删除一个元素。删除一个键；找不到这个键时不抛出错误，容器原样保留。删除成功的那条记录当场析构。",
		En: "Removes a key; a key that is not there throws nothing and leaves the container as it was. The record removed is destroyed on the spot.",
		Eg: "let d: dict<string, int> = {}\nd.add(\"甲\", 1)\nd.remove(\"甲\")",
	},
	"dict.values": {
		Zh: "取全部值，返回一份新的 list<V>。",
		En: "Returns a new list<V> of every value.",
		Eg: "let d: dict<string, int> = {}\nlet all: list<int> = d.values()",
	},

	// —— hashset：集合
	"hashset.add": {
		Zh: "添加一个元素；已经在里面的，那个位置不变。",
		En: "Adds an element; one already in the set leaves that spot alone.",
		Eg: "let s: hashset<int> = {}\ns.add(1)",
	},
	"hashset.contains": {
		Zh: "是否存在。问集合里有没有那一个元素。",
		En: "Asks whether the set holds that element.",
		Eg: "let s: hashset<int> = {}\nlet has: boolean = s.contains(1)",
	},
	"hashset.difference": {
		Zh: "差集：在这个集合里而不在另一个里的元素，返回一个新的集合。",
		En: "Difference: the elements in this set but not the other, as a new set.",
		Eg: "let a: hashset<int> = {}\nlet b: hashset<int> = {}\nlet rest: hashset<int> = a.difference(b)",
	},
	"hashset.intersect": {
		Zh: "交集：两个集合都有的元素，返回一个新的集合。",
		En: "Intersection: the elements both sets have, as a new set.",
		Eg: "let a: hashset<int> = {}\nlet b: hashset<int> = {}\nlet both: hashset<int> = a.intersect(b)",
	},
	"hashset.remove": {
		Zh: "删除一个元素。移除一个元素；不在里面的时候不抛出错误，集合原样保留。",
		En: "Removes an element; one that is not there throws nothing and leaves the set alone.",
		Eg: "let s: hashset<int> = {}\ns.add(1)\ns.remove(1)",
	},
	"hashset.union": {
		Zh: "并集：两个集合的元素合在一起，返回一个新的集合。",
		En: "Union: both sets' elements together, as a new set.",
		Eg: "let a: hashset<int> = {}\nlet b: hashset<int> = {}\nlet all: hashset<int> = a.union(b)",
	},

	// —— string：文本身上的成员
	"string.contains": {
		Zh: "判断文本里是否包含要找的内容。",
		En: "Whether the text contains what you are looking for.",
		Eg: `let hit: boolean = "甲乙".contains("乙")`,
	},
	"string.ends_with": {
		Zh: "判断文本是否以指定内容结尾；指定内容为空时算符合。",
		En: "Whether the text ends with the given content; an empty argument counts as a match.",
		Eg: `let ok: boolean = "甲乙".ends_with("乙")`,
	},
	"string.index_of": {
		Zh: "返回要找的内容第一次出现时的码点位置；一处都没有时返回 -1；要查找的是空文本时，算在第 0 个码点。",
		En: "The code point of the first match, or -1 when there is none; an empty needle is taken at position 0.",
		Eg: `let at: int = "甲乙丙".index_of("乙")`,
	},
	"string.last_index_of": {
		Zh: "从末尾向前查找，返回要找的内容最后一次出现时的码点位置；一处都没有时返回 -1。要查找的是空文本时，返回 length(s)，与 index_of 返回 0 正好对应。",
		En: "Searches from the end backwards and returns the first match it finds that way, or -1.",
		Eg: `let at: int = "甲乙甲".last_index_of("甲")`,
	},
	"string.pad_end": {
		Zh: "在末尾补齐到指定宽度。宽度按码点计数，与 length 的计数标准一致。",
		En: "Pads at the end up to the given width, counted in code points exactly like length.",
		Eg: `let cell: string = "7".pad_end(3, "0")`,
	},
	"string.pad_start": {
		Zh: "在开头补齐到指定宽度。宽度按码点计数，与 length 的计数标准一致。",
		En: "Pads at the start up to the given width, counted in code points exactly like length.",
		Eg: `let cell: string = "7".pad_start(3, "0")`,
	},
	"string.repeat": {
		Zh: "把自身重复 count 遍。次数为 0 或负数时返回空文本，不抛出错误。",
		En: "Repeats the text that many times; 0 or a negative count returns an empty text instead of throwing.",
		Eg: `let line: string = "-".repeat(3)`,
	},
	"string.replace": {
		Zh: "替换查找到的内容，生成一段新文本。",
		En: "Replaces and returns a new text.",
		Eg: `let out: string = "甲乙".replace("乙", "丙")`,
	},
	"string.slice": {
		Zh: "截取一段文本，含起点、不含终点；下标为负、start > end、end 超出长度这三种情况都抛 10031 号（下标越界）。",
		En: "Takes [start, end); a negative index, start above end, or end past the length all throw 10031.",
		Eg: `let part: string = "甲乙丙".slice(1, 3)`,
	},
	"string.split": {
		Zh: "按分隔符把文本切分成列表。",
		En: "Splits on the separator.",
		Eg: `let parts: list<string> = "甲,乙".split(",")`,
	},
	"string.starts_with": {
		Zh: "判断文本是否以指定内容开头；指定内容为空时算符合。",
		En: "Whether the text starts with the given content; an empty argument counts as a match.",
		Eg: `let ok: boolean = "甲乙".starts_with("甲")`,
	},
	"string.to_bytes": {
		Zh: "返回这段文本的 UTF-8 字节组成的字节块；不带 BOM。空文本返回空的字节块。",
		En: "Returns the text's UTF-8 bytes as a block, without a BOM; an empty text gives an empty block.",
		Eg: `let raw: bytes = "甲乙".to_bytes()`,
	},
	"string.to_list": {
		Zh: "把文本拆成码点列表。",
		En: "Splits the text into a list of code points.",
		Eg: `let chars: list<unicode> = "甲乙".to_list()`,
	},
	"string.to_lower": {
		Zh: "转换小写，只处理 ASCII 字符。",
		En: "Lowercases the text, handling ASCII characters only.",
		Eg: `let low: string = "ABC".to_lower()`,
	},
	"string.to_upper": {
		Zh: "转换大写，只处理 ASCII 字符。",
		En: "Uppercases the text, handling ASCII characters only.",
		Eg: `let up: string = "abc".to_upper()`,
	},
	"string.trim": {
		Zh: "去掉文本两端的空白。",
		En: "Strips whitespace at both ends.",
		Eg: `let clean: string = "  甲  ".trim()`,
	},
	"string.trim_end": {
		Zh: "只去末尾的空白，空白与 trim 同一条。",
		En: "Strips trailing whitespace only, with the same definition of whitespace as trim.",
		Eg: `let clean: string = "甲  ".trim_end()`,
	},
	"string.trim_start": {
		Zh: "只去开头的空白，空白与 trim 同一条。",
		En: "Strips leading whitespace only, with the same definition of whitespace as trim.",
		Eg: `let clean: string = "  甲".trim_start()`,
	},

	// —— socket 与 listener：套接字与监听
	"socket.send": {
		Zh: "把字节块发送出去，返回已发送的字节数。",
		En: "Sends the block and returns how many bytes went out.",
		Eg: `let sock: Socket = net.connect("127.0.0.1", 8080, 1000)` + "\n" + `let sent: int = sock.send(bytes.new(4))`,
	},
	"socket.recv_into": {
		Zh: "把接收到的字节填入传入的字节块，返回填入的字节数；返回 0 表示对方已关闭连接。等待超时是 10102。",
		En: "Fills the given block with what arrives and returns the byte count; 0 once the peer has closed.",
		Eg: `let sock: Socket = net.connect("127.0.0.1", 8080, 1000)` + "\n" + `let buf: bytes = bytes.new(16)` + "\n" + `let got: int = sock.recv_into(buf)`,
	},
	"socket.close": {
		Zh: "提前关闭。不主动调用，名字执行到尽头时也会关。",
		En: "Closes early; the handle is closed anyway when the name reaches the end of its scope.",
		Eg: `let sock: Socket = net.connect("127.0.0.1", 8080, 1000)` + "\n" + `sock.close()`,
	},
	"listener.accept": {
		Zh: "等待并接受下一个连接，返回对应的套接字。",
		En: "Waits for the next connection and returns it.",
		Eg: `let gate: Listener = net.listen(8080)` + "\n" + `let peer: Socket = gate.accept()`,
	},
	"listener.close": {
		Zh: "不再监听：关掉监听句柄，那个端口立刻可以重新绑定。",
		En: "Stops listening: the handle closes and the port can be bound again immediately.",
		Eg: `let gate: Listener = net.listen(8080)` + "\n" + `gate.close()`,
	},
}

// raw 把三样东西排成"注释里怎么写"的那一段：中文一句、`en: ` 起头的英文一句、`例：` 起头的示例。
// 语言服务器认这两个前缀，屏幕上分成三段显示，两个前缀都不上屏。
//
// en: raw lays the three parts out as they are written in a comment: the Chinese sentence, an
// English sentence starting with `en: `, an example starting with `例：`. The language server
// recognises those two prefixes, the screen shows three sections, and neither prefix
// is displayed.
func (d StdDoc) raw() string {
	parts := []string{d.Zh}
	if d.En != "" {
		parts = append(parts, "en: "+d.En)
	}
	if d.Eg != "" {
		parts = append(parts, "例：\n"+d.Eg)
	}
	return strings.Join(parts, "\n\n")
}

// StdDocRaw 交回一条内建动作的说明原文，按落点名找（`console.print`、`list.add`）。
// 认不出返回空串——屏幕上就只留签名那一行，不凑话。
//
// en: StdDocRaw returns the documentation text of one built-in action, looked up by dotted name
// (`console.print`, `list.add`). An unknown name returns an empty string and the screen keeps
// only the signature line rather than manufacturing prose.
func StdDocRaw(dotName string) string {
	d, ok := stdDocs[dotName]
	if !ok {
		return ""
	}
	return d.raw()
}

// groupDoc 是一个标准库组自己那两句话：悬停停在组名上时用（组名没有声明那一行，凑不出一行签名）。
//
// en: groupDoc is the two sentences about a standard-library group itself, used when hover
// rests on a group name (a group name has no declaration line, so no signature can be
// assembled).
type groupDoc struct{ Zh, En string }

var groupDocs = map[string]groupDoc{
	"console":            {"向屏幕输出内容、从键盘读取一行。", "Writes to the screen and reads one line from the keyboard."},
	"text":               {"文本的创建、查找、切分、拼接与转换。", "Creating, searching, splitting, joining and converting text."},
	"bytes":              {"一整块字节：创建、切片、Base64 与拼接。", "A whole block of bytes: creation, slicing, Base64 and joining."},
	"number":             {"把数值写成文本，按进制写也一样。", "Writing numbers as text, in any of the four bases."},
	"math":               {"取整、幂、对数、三角函数与随机数。", "Rounding, powers, logarithms, trigonometry and random numbers."},
	"file":               {"读写整个文件、追加、列目录、复制、移动与删除。", "Reading and writing whole files, appending, listing, copying, moving and deleting."},
	"path":               {"只处理路径那一段文本，不访问系统。", "Manipulating path text only, without touching the system."},
	"environment":        {"环境变量与当前目录。", "Environment variables and the current directory."},
	"process":            {"启动一个程序、等它结束、关掉这个把手。", "Starting a program, waiting for it and releasing the handle."},
	"time":               {"当前时间、运行了多久、等待、拆成日历与时区差。", "Current time, elapsed time, sleeping, calendar breakdown and the UTC offset."},
	"regular_expression": {"匹配、查找、替换与切分。", "Matching, finding, replacing and splitting."},
	"json":               {"解析 JSON、读那棵树、取各类型的值。", "Parsing JSON and reading that tree."},
	"net":                {"TCP 客户端与服务器。", "The TCP client and server."},
	"http":               {"HTTP 客户端，走系统的 winhttp。", "The HTTP client, on the system's winhttp."},
}

// GroupDoc 交回一个标准库组自己那两句话。
//
// en: GroupDoc returns the two sentences about one standard-library group.
func GroupDoc(group string) (zh, en string) {
	d, ok := groupDocs[group]
	if !ok {
		return "", ""
	}
	return d.Zh, d.En
}

// StdDocKeys 是这张表的全部键，测试拿它与 `stdFuncs`、`stdMember` 两张真表双向比齐。
//
// en: StdDocKeys is every key of this table; the tests compare it both ways against the real
// tables stdFuncs and stdMember.
func StdDocKeys() []string {
	out := make([]string, 0, len(stdDocs))
	for k := range stdDocs {
		out = append(out, k)
	}
	return out
}

// StdExample 只要那一段用法示例，给"每条示例都得编译过去"的那条测试用。
//
// en: StdExample returns only the usage example, for the test that requires every example to
// compile.
func StdExample(dotName string) string {
	return stdDocs[dotName].Eg
}

// StdDocFor 按"那一样东西 + 那一条成员"找说明原文。三种落点名的写法都试一遍：
// 形状身上的（list 的 add、string 的 slice）、组名身上的（console 的 print）、
// 光一个名字的（length）。自己写的类与包里的东西不在这张表里，交回空串。
//
// en: StdDocFor finds the documentation text by "that thing + that member", trying all three
// dotted-name forms: on a shape (list's add, string's slice), on a group name (console's
// print), and a bare name (length). Hand-written classes and package contents are not in this
// table and return an empty string.
func StdDocFor(recvName string, recv Type, name string) string {
	if g := containerGroup(recv.Kind); g != "" {
		if d, ok := stdDocs[g+"."+name]; ok {
			return d.raw()
		}
	}
	if d, ok := stdDocs[recvName+"."+name]; ok {
		return d.raw()
	}
	if d, ok := stdDocs[name]; ok {
		return d.raw()
	}
	return ""
}

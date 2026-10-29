# nf

nf 语言在 Visual Studio Code 里的编辑器支持。装好之后打开 `.nf` 文件就有语法高亮、编译期错误提示、补全与悬停；按 F5 可以调试。

分析与报错都出自 `nf` 编译器：扩展把它作为语言服务器（`nf lsp`）启动，把它的调试适配器（`nf dap`）作为 DAP 服务端启动，自己不实现第二套规则。

## 功能与用法

| 功能 | 怎么触发 | 屏幕上是什么 |
| --- | --- | --- |
| 语法高亮 | 打开 `.nf` 文件 | 关键字、控制流、运算符、数字（十进制与十六进制）、字符串与转义、注释、类型名、函数名分别着色 |
| 错误提示 | 文本每次改动之后重新算，不必保存 | 出错的字符下面画波浪线，鼠标停上去显示 `nf check` 的那句原文与行列位置 |
| 补全 | 输入 `.`，或在名字位置按 Ctrl+Space | 这个值的成员、标准库组里的函数、包里 `export` 出去的名字、本文件与引进来的名字、类型名与关键字；右边那一栏是这一条的签名 |
| 悬停 | 鼠标停在一个名字上 | 第一行是签名或类型，第二行起是写在声明上面的注释：中文一段、英文一段、一段标着 nf 的示例 |
| 调试 | 打开一份 `.nf` 按 F5 | 断点、单步、调用栈、变量、悬停查看变量的值、内存视图、程序的输出与退出码 |

## 补全

下面几行是从 `webtest/cases/login.nf` 与 `samples/demo.nf` 里实测的答复：左边是当时打的字与补出来的那个名字，右边那一栏是它的签名（不是动作的那几类就写它自己是什么）。

```text
run. 里的 step         step(label: string)
page. 里的 click_at    click_at(x: int, y: int)
wt. 里的 page_at       page_at(index: int): Page
wt. 里的 code_assert_failed  code_assert_failed: int
console. 里的 print    print(...values)
行首的 page            let page: Page
行首的 case_begin      Run.case_begin(title: string): int
行首的 wt              包 libs/web_test
行首的 console         标准库组
行首的 while           关键字
```

`run.`、`page.`、`wt.`、`console.` 那四段候选各有 34、45、49、3 条，行首那一段 250 条。`wt.` 那四十九里有十行是包交出来的数（`code_assert_failed: int` 那种写法），它们只能读，不能赋值，也不能加括号调用。

`run.`、`page.`、`wt.` 后面还什么都没有时照样能列出候选：句子写到一半时，扩展在光标处插一个占位名字再走一遍检查，好让点号左边那一段先算出类型；占位名字只为这一次问答存在，不会出现在编辑器里，也不写入文件。

参数名一律是英文小写下划线；只读借用的参数在签名里写成 `ref 名字: 类型`，与声明那一侧一样；元素类型跟着接收者走，写不出真类型时写 `T`／`K`／`V`；变长参数写成 `(...values)`；没有返回值的动作不写冒号那一段。没写 `export` 的函数不会出现在包名后面那一段候选里。

## 悬停

停在方法或函数名上，第一行是签名，下面那几句是写在这个声明上面的注释——中文一段、英文一段、一段标着 `nf` 的示例（下面这一段是从 `webtest/cases/login.nf` 第 25 行实际问出来的答复）：

```text
Run.step(label: string)

脚本里想记哪一步就写一句；过的那一条不打印，挂的那一条才把走过的列出来。

Write one of these for each step you want recorded; a passing case prints nothing, while a failed one lists the steps it got through.

run.step("点开登录")
```

注释在语言里没有语义：词法阶段整段丢掉，编译器看不见它。这一段是扩展按声明所在的行回到原文读出来显示的，写在别的文件里的注释同样读得到。紧贴声明上面那一段才算这条声明的，隔了一个空行就不算；写在注释里的 `en: ` 与 `例：` 两个记号只用来分段，屏幕上不出现。`@param`、`@return` 这类标签语言没有规定意思，屏幕上保持原样。声明上面没写注释，就只给签名那一行。停在包交出来的那一个数上也是这三段，第一行换成它的名字与类型（实测：`greet.LIMIT: int`，下面是写在 `export const LIMIT:int = 4096` 上面那中文、英文与示例）。

标准库的那一百四十八个动作没有声明所在的行可以回原文读，它们的中文、英文与示例放在编译器里的一张表上（`compiler/internal/check/docs.go`），屏幕上与库的注释是同一个形状。停在 `number.to_string(rows)` 的动作名上：

```text
number.to_string(value): string

基础类型按各自的格式输出：int、byte、enum 输出十进制，boolean 输出 true／false，unicode 输出对应的字符，float 输出最短往返且必带小数点的写法。……

Formats a basic value by its own rules: int, byte and enum as decimal, boolean as true or false, a code point as that character, a float in its shortest round-trip form and always with a decimal point. …

let tag: string = number.to_string(42)
```

停在类型名上给的是类型本身，再给一句实际的造值写法（`class Box` 与 `new Box(n: int)`，字段名与类型取自声明）。停在标准库的组名上（比如 `console`）给的是组名，后面列出这个组里那些动作的签名。

## 调试

- 断点：点行号左侧。只能停在语句开头那一个位置（一条语句一个检查点），所以表达式中间停不下来。
- 单步：F10 执行下一句，F11 进入函数，Shift+F11 跳出当前函数。
- 变量：每一帧给两个作用域。局部是这一帧上有名字的位置；静态是每份文件顶层的 `let`／`const`，它们不存放在任何一帧里，停在文件顶层时只有这一栏有内容。
- 能查看的值：整数、小数、布尔、字节、码点、文本、`list`、`dict`、`hashset`（一次最多展开 20 项、向内两层）、`class` 与 `struct` 的字段、`null`。没有写过的槽位会如实说明读不出来。
- 内存视图：沿着变量所在格的地址看一段字节，一次最多 4 KiB，只能查看不能修改。
- 程序写到屏幕上的内容实时回到调试控制台，运行结束后报告退出码。

配置项是 `program`（要调试的那份 `.nf`，默认当前文件）与 `args`（交给程序的参数，程序里的 `args` 看得见）。没有 `launch.json` 直接按 F5 时使用当前打开的文件。

## 设置

| 项 | 默认 | 说明 |
| --- | --- | --- |
| `nf.path` | 空 | 留空就用扩展自带的那一份 `nf.exe`（Windows x64）；填一个完整路径就用本机那一份，改完编译器重载窗口立即生效 |

扩展自带一份 `nf.exe`（Windows x64），装完就能用，不必先自行安装编译器。它注入的版本号与插件自己的号相同：插件 0.4.1 带的这一份，`nf version` 打印 `nf v0.4.1`。要试自己改过的编译器，把 `nf.path` 指向本机那一份，重载窗口就生效，不必重新打包扩展。

## 语言配置

`.nf` 的注释符号（`//` 与 `/* */`）、括号与引号的自动配对、缩进规则由扩展声明，编辑器自带的注释开关、括号高亮与自动缩进跟着生效。

## 已知限制

- 没有跳转到定义、格式化与重命名。
- 补全不处理点号左边是下标或括号的那种中段（`xs[0].`、`(x).`）。
- 调试不能在表达式中间单步，没有条件断点，无法计算表达式（要看一个中间结果，先用 `let` 存一个名字），内存只读。
- 需要 Visual Studio Code 1.90 或更新；扩展自带的那一份编译器只能运行在 Windows x64。

## 安装与构建

这一份扩展当前只从本仓库打包安装，没有发布到 Marketplace：

```text
cd compiler
go build -o ../nf.exe ./cmd/nf
cd ../vscode
npm install
npm run check
npm run package
code --install-extension nef-0.4.1.vsix
```

装完重载一次窗口（Ctrl+Shift+P → Reload Window）。`npm run check` 做两件事：把语法文件里那两张名字表与 `nf tokens` 逐个比对，再用 VS Code 使用的 TextMate 引擎把探针与 `samples/` 里的示例程序分词一遍。

## 参见

- nf 语言本身：仓库根的 `README.md`，以及站上 `site/learn/`、`site/reference/` 那两组页面
- 命令行工具与 `nf lsp`、`nf dap`：站上 `site/tools/059_cli.md` 与 `site/tools/061_editors.md`
- 许可：Apache License 2.0，另附一份针对程序输出的例外条款；许可证全文、例外条款与第三方组件署名见仓库根的 `LICENSE`、`LICENSE-EXCEPTION` 与 `NOTICE`，打包时这三份会随扩展包一起分发（`tools/make-license.mjs`）

# Nef

![版本](https://img.shields.io/badge/version-v0.4.1-blue)
[![代码许可](https://img.shields.io/badge/license-Apache--2.0%20%E4%B8%8E%20Nef%20Output%20Exception-green)](LICENSE)
[![文档许可](https://img.shields.io/badge/docs-CC%20BY%204.0-yellow)](nef-licensing/LICENSE-DOCS.md)
[![平台](https://img.shields.io/badge/platform-Windows%20x64-lightgrey)](#依赖)
[![构建于 Qoder](https://img.shields.io/badge/built%20with-Qoder-6c5ce7)](#致谢)

English version: [README.md](README.md).

Nef 是一门静态类型语言：类型标注必须写出来，没有垃圾回收。一块内存在任一时刻只有一个持有者：把值交给函数、或者放进容器，持有就转移过去；编译器据此在编译期安排好每一块内存的释放时机，在最后一个持有者的作用域尽头，按声明的反序释放。编译器用 Go 写成，直接生成 x86-64 机器码，产物是一个独立的 `.exe`。

编译出的程序只向操作系统要几项能力（标准输出、内存、进程退出，以及套接字与 HTTP 用的那几个系统库）。不需要运行时，不依赖任何已安装的组件，也不链接 C 运行时。

## 概览

- [特点](#特点)
- [依赖](#依赖)
- [构建](#构建)
- [第一个程序](#第一个程序)
- [命令行](#命令行)
- [测试](#测试)
- [目录](#目录)
- [参与贡献](#参与贡献)
- [许可](#许可)
- [致谢](#致谢)

## 特点

- 赋值与传参都会把持有交出去，参数前写 `ref` 的那一处只是借来看一眼；释放顺序是声明的反序，编译期定死。
- 同一套检查与机器码走两条路：`nf 文件.nf` 在内存里编译完直接运行，`nf build` 生成独立的可执行文件。
- 标准库十四组，覆盖文本、容器、文件、时间、数学、JSON、正则、网络（TCP 与 HTTP）与子进程。
- 包以源文件分发（`.nfpack` 容器），使用它的项目自己现场编译。
- VS Code 插件：语法高亮、编译期错误提示、补全、悬停说明与 F5 调试，背后是 `nf lsp` 与 `nf dap`。
- `webtest/`：用 Nef 自己写的浏览器自动化测试库，也是这门语言的第一份外部库。

## 依赖

- Windows x64。这是编译器与产物唯一支持的平台：生成的 PE 只引用 `kernel32`、`ws2_32` 与 `winhttp`，目前没有跨平台层。
- 编译编译器需要 Go 1.27 或更新（这套代码在 `go version go1.27.1 windows/amd64` 上测试）。
- 网站与 VS Code 插件需要 Node.js 与 npm。

## 构建

```text
cd compiler
go build -trimpath -o ../nf.exe ./cmd/nf
```

`nf.exe` 生成在仓库根，运行示例用的就是这一份。`./nf.exe version` 打印它的版本号；构建时没注入版本号的打印 `nf dev`。要一份带版本号的产物，在上面那条命令里加上 `-ldflags "-X main.version=vX.Y.Z"`。每个发布版本的 `nf.exe` 与 VS Code 扩展包都挂在该版本的 GitHub Releases 页面上。版本号三位数各自什么时候变、`0.x` 阶段承诺什么，写在站上的[版本与兼容](site/about/066_versions.md)；每一版改了什么记在 [CHANGELOG.md](CHANGELOG.md)。

直接运行一个示例，或者把它编成可执行文件：

```text
./nf.exe samples/shapes.nf
./nf.exe build samples/stock.nf -o stock.exe
./stock.exe
```

## 第一个程序

```nf
let name:string = "Nef"
let xs:list<int> = [1, 2, 3]
let total:int = 0
for (x:int in xs) {
    total = total + x
}
console.print("hello, " + name + "; sum = " + number.to_string(total))
```

存成 `hello.nf`，运行 `./nf.exe hello.nf`。类型标注是声明的一部分；`total = total + x` 只是把一个 `int` 重新赋值，而 `let one:string = xs[0]` 会把列表里那一个位置交出来：读走之后，那个位置就空了。

## 命令行

| 命令 | 作用 |
| --- | --- |
| `nf 文件.nf [参数…]` | 在内存里编译完直接执行，不落中间文件 |
| `nf build 文件.nf [-o 产物.exe]` | 生成独立的可执行文件 |
| `nf pack 门面.nf [-o 包.nfpack]` | 把一个包的源文件装进 `.nfpack` 容器，给别的项目用 |
| `nf check 文件.nf` | 词法、语法、静态检查依次走完；报错带文件名、行号与列号 |
| `nf tokens 文件.nf` | 打印记号流（每行一个 `类型:文字`），站点的语法高亮拿它当对照基准 |
| `nf ir 文件.nf` | 打印降级后的中间码，不生成机器码，也不写文件 |
| `nf lsp` | 语言服务器：从标准输入读编辑器的协议消息，检查规则与 `nf check` 同一条 |
| `nf dap` | 调试适配器：断点、单步、调用栈与变量，VS Code 里按 F5 用的就是它 |
| `nf version` | 打印编译器版本后退出，退出码恒为 0，方便脚本判断装没装 |

## 测试

```text
cd compiler
go test ./...
```

测试查语言规则、查降级后的中间码，也实际运行程序：`samples/` 里每一份示例都有一条端到端测试，其中几份会把产物编出来、作为子进程启动、再逐行比对输出。编译器对用户说的每一句报错原文都收在 `compiler/testdata/报错文字.txt` 里，由测试从源码抄出来逐句比对；改了句子而没重新生成这份列表，测试就失败。

`compiler/` 里每个 `.go` 文件开头一行都是许可声明，形状取自 `nef-licensing/templates/FILE_HEADER.txt`。

注释只写这段代码做什么、为什么这么做、什么情况下不成立，不引用规范章节号、决定编号或公开目录里没有的文档名。

网站文档另有自动检查（`site/` 里跑 `npm run verify`）：每一段 `nf` 示例都必须编译得过；标成错误写法的每一段都必须被拦下，而且报错文本与编译器输出整行相等；写着输出的每一段都实际运行一遍再比。

## 目录

| 位置 | 内容 |
| --- | --- |
| `compiler/` | 整套工具链的 Go 源码（一个模块，零第三方依赖） |
| `samples/` | 七份示例程序，每一份同时是一条端到端测试的用例 |
| `webtest/` | 用 Nef 写的浏览器自动化测试库与脚本运行层 |
| `site/` | 文档网站（VitePress）与它的七项检查 |
| `vscode/` | VS Code 插件 |
| `.github/workflows/` | 构建并发布文档网站的 GitHub Pages 工作流 |
| `nef-licensing/` | 其余许可文本：CLA、商标政策、贡献流程、文档许可、Apache 原文、文件头与例外模板 |
| `CHANGELOG.md` | 每一版改了什么 |

## 参与贡献

先读 [nef-licensing/CONTRIBUTING.md](nef-licensing/CONTRIBUTING.md) 和旁边的贡献者许可协议。在这份仓库里改动要守两条规矩：

- 编译器对用户说的那句话是接口的一部分：测试和文档里记录的原话都来自实际运行，不凭记忆写。
- 自动检查失败时，不许为了让检查通过而改动期望值；只有语言规则本身定了变化，才能改期望，而且要先把这条规则写进网站上的规范：改的是语言，不是测试。

## 许可

- 代码（编译器、工具链、运行期、标准库、插件）：Apache License 2.0，另附一份 **Nef Output Exception**——`nf` 生成的机器码不受任何传染性条款约束。许可证全文在
  [LICENSE](LICENSE)，例外条款在 [LICENSE-EXCEPTION](LICENSE-EXCEPTION)，第三方组件署名在
  [NOTICE](NOTICE)，这三份一起随 VS Code 扩展包分发。
- 文档、规范与网站文字：[CC BY 4.0](site/LICENSE)。`site/tools-site/` 与 `site/.vitepress/` 里的脚本是代码，跟着上面那一档走。
- 语言名称 `Nef` 受[商标政策](nef-licensing/TRADEMARK_POLICY.md)约束。
- `samples/` 那七份示例程序，以及这份 README 和站上正文里出现的代码，不受任何许可证约束：拿去写自己的程序，用什么条款都行，不需要署名。这句话的出处是
  [NOTICE](NOTICE) 那份许可摘要的最后一条。

## 致谢

这套编译器、标准库、网站、VS Code 插件与文档都在 [Qoder](https://qoder.com)（智能体式编程 IDE）里开发完成；语言设计、架构与每一处合并的取舍由作者自己定。

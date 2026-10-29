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

// nf 的命令行入口。
//
// check 走前端：词法、语法、静态检查。build 走完整条链，出独立可执行文件。
// ir 走到中间码那一步就停，把降级出来的三地址码打到标准输出，不落文件。
// pack 把一个包的源文件装进一份 .nfpack 容器，不产可执行。
// `nf 文件.nf 甲 乙` 在内存里编译完直接执行，不落中间文件（run_windows.go）。
// lsp 起语言服务器：标准输入接收编辑器送来的报文，标准输出回波浪线（internal/ls）。
// dap 起调试器：不带文件时是与编辑器通信的一侧，带文件与 --debuggee 时是被调试的子进程（dap_windows.go）。
//
// en: The command-line entry point of nf. check runs the front end: lexer,
// parser, static checker. build runs the whole chain and emits a standalone executable. ir
// stops at the intermediate code and prints the three-address code produced by lowering to
// standard output, writing no file. pack puts a package's source files into one .nfpack
// container and produces no executable. `nf file.nf arg1
// arg2` compiles entirely in memory and executes straight away, leaving no intermediate file
// (run_windows.go). lsp starts the language server: it takes the editor's messages on standard
// input and answers with squiggles on standard output (internal/ls). dap starts the debugger:
// with no file it is the side that talks to the editor, with a file and --debuggee it is the
// debugged child process (dap_windows.go).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/codegen"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/lower"
	"neflang.org/nf/internal/ls"
	"neflang.org/nf/internal/parser"
	"neflang.org/nf/internal/pkgload"
)

// version 是编译器自己的版本号。仓库里没有发布流水线来管这个数字，所以默认写 dev，
// 发出去的那一份用 -ldflags "-X main.version=v0.4.0" 注入。
// 没有它的话，"这份 nf.exe 是哪一版编的"只能从文件修改时间推断。
//
// en: version is the compiler's own version number. The repository has no release pipeline to
// manage this figure, so it defaults to dev and the shipped copy injects it with -ldflags "-X
// main.version=v0.4.0".
// Without it, "which build is this nf.exe" could only be inferred from the file's timestamp.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法：nf check 文件.nf　｜　nf build 文件.nf [-o 产物名]　｜　nf pack 门面.nf [-o 容器名]　｜　nf 文件.nf　｜　nf tokens 文件.nf　｜　nf ir 文件.nf　｜　nf lsp　｜　nf dap　｜　nf version")
		return 2
	}
	switch args[0] {
	case "tokens":
		return cmdTokens(args[1:])
	case "ir":
		return cmdIR(args[1:])
	case "pack":
		return cmdPack(args[1:])
	case "lsp":
		return cmdLSP()
	case "dap":
		return cmdDAP(args[1:])
	case "version":
		return cmdVersion()
	case "check":
		return cmdCheck(args[1:])
	case "build":
		return cmdBuild(args[1:])
	default:
		if strings.HasSuffix(args[0], ".nf") {
			return cmdRun(args)
		}
		fmt.Fprintf(os.Stderr, "认不出的命令 %q\n", args[0])
		return 2
	}
}

func cmdCheck(args []string) int {
	args = dropFlag(args, "-o")
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "要给一个 .nf 文件")
		return 2
	}
	entry := filepath.ToSlash(args[0])
	prog := &check.Program{Entry: entry}
	seen := map[string]bool{}
	pk := pkgload.New()
	if !load(prog, entry, seen, pk, "") {
		return 1
	}
	errs := check.Run(prog)
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e.Error())
	}
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "共 %d 处不对\n", len(errs))
		return 1
	}
	fmt.Printf("检查通过：%d 个文件\n", len(prog.Files))
	return 0
}

// compileIR 走完前端到中间码那一段：检查 → 降级。`nf ir` 到这里就停，
// compileChain 在它后面接发射。任何一步不过都把错误信息原样打出来。
//
// en: compileIR runs front end through intermediate code: check then lower. `nf ir` stops here
// and compileChain appends the emit step after it. Whatever step fails, its message is printed
// verbatim.
func compileIR(prog *check.Program) (*ir.Prog, bool) {
	errs, facts := check.RunWithFacts(prog, true)
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e.Error())
	}
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "共 %d 处不对\n", len(errs))
		return nil, false
	}
	p, err := lower.Run(prog, facts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, false
	}
	return p, true
}

// compileChain 走完前端到机器码那一段：检查 → 中间码 → 发射。
// 返回装配器、数据布局与入口在代码里的偏移——出 exe 与"在内存里直接跑"两条路
// 都在它后面各接自己那一步。任何一步不过都把错误信息原样打出来。
//
// en: compileChain runs front end through machine code: check, intermediate code, emit. It
// returns the assembler, the data layout and the entry offset inside the code, because the two
// paths - producing an exe and running straight out of memory - each append their own last
// step after it. Whatever step fails, its message is printed verbatim.
func compileChain(prog *check.Program) (*codegen.Linker, *codegen.DataLayout, int, bool) {
	p, ok := compileIR(prog)
	if !ok {
		return nil, nil, 0, false
	}
	l := codegen.NewLinker()
	d := codegen.BuildData(p)
	entry, err := codegen.Emit(p, l, d)
	if err != nil {
		fmt.Fprintln(os.Stderr, "后端发射阶段：", err)
		return nil, nil, 0, false
	}
	return l, d, entry, true
}

// compileAll 走完前端再到产物：检查 → 中间码 → 机器码 → PE 字节。
//
// en: compileAll runs front end all the way to the product: check, intermediate code, machine
// code, PE bytes.
func compileAll(prog *check.Program) ([]byte, bool) {
	l, d, entry, ok := compileChain(prog)
	if !ok {
		return nil, false
	}
	exe, err := l.Build(d.User, entry)
	if err != nil {
		fmt.Fprintln(os.Stderr, "装配阶段：", err)
		return nil, false
	}
	return exe, true
}

// cmdBuild 出一个独立可执行文件：默认与源文件同名同目录、加本机后缀，
// -o 指定产物名字。产物自己就能跑，这台机器上不需要 nf，也不需要任何运行时。
//
// en: cmdBuild produces one standalone executable: by default next to the source file
// with the same name plus the local suffix, and -o names the product explicitly. The product
// runs by itself - this machine needs neither nf nor any runtime.
func cmdBuild(args []string) int {
	out := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "-o 后面要跟一个产物名字")
				return 2
			}
			out = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "要给一个 .nf 文件")
		return 2
	}
	entry := filepath.ToSlash(rest[0])
	prog := &check.Program{Entry: entry}
	seen := map[string]bool{}
	pk := pkgload.New()
	if !load(prog, entry, seen, pk, "") {
		return 1
	}
	exe, ok := compileAll(prog)
	if !ok {
		return 1
	}
	if out == "" {
		out = strings.TrimSuffix(entry, ".nf") + exeSuffix()
	}
	if err := os.WriteFile(out, exe, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "%s：写不出来：%v\n", out, err)
		return 1
	}
	fmt.Println(out)
	return 0
}

// exeSuffix 是本机可执行文件的后缀。当前只出 Windows x64。
//
// en: exeSuffix is this machine's executable file suffix. Only Windows x64 is produced today.
func exeSuffix() string { return ".exe" }

// dropFlag 把 -o 与它的值一起去掉，让 check 也能吃 build 的参数形状。
//
// en: dropFlag removes -o together with its value so that check can also accept build's
// argument shape.
func dropFlag(args []string, flag string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// load 读一个文件并把它的 import 递归拉进来，任何一步不成都把错误信息打在标准错误上。
// pk 登记这一次装载的状态（开过的容器、读过的 nf.json），root 是这一份文件所属的包根：
// 工程自己的文件传空，包里的文件传它那个包根，包自己引的 `./` 文件同属这个包。
//
// en: load reads one file and pulls in its imports recursively; any step that fails prints its
// message on standard error. pk records this pass's loading state (containers already opened,
// nf.json files already read) and root is the package root this file belongs to: the project's
// own files pass empty, a file inside a package passes that package's root, and a `./` file
// imported by a package belongs to the same package.
func load(p *check.Program, path string, seen map[string]bool, pk *pkgload.Loader, root string) bool {
	if p.PkgRefs == nil {
		p.PkgRefs = map[string]string{}
	}
	if p.PkgRoots == nil {
		p.PkgRoots = map[string]string{}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false
	}
	// 去重的键要先按磁盘上的真实写法规范化一次：`./A.nf` 与 `./a.nf` 在不区分大小写的
	// 系统上是同一份文件，登记两份就把同一个定义登记了两遍；一个文件被多条路径引到时
	// 只算一次。File.Path 仍用写出来的那一种，报错上的路径不跟着变长。
	key := filepath.ToSlash(pkgload.RealCase(abs))
	if seen[key] {
		return true
	}
	seen[key] = true
	src, ok := pk.ReadSource(key)
	if !ok {
		// 不把系统的错误对象原样打印出去：那里面带着英文原文和整条绝对路径，
		// 需要说的是"哪一句 import 找不到文件"。
		fmt.Fprintf(os.Stderr, "%s：读不到这个文件\n", path)
		return false
	}
	toks, lerr := lexer.Lex(src)
	if lerr != nil {
		var le *lexer.Error
		if errors.As(lerr, &le) {
			fmt.Fprintf(os.Stderr, "%s 第 %d 行第 %d 列：%s\n", path, le.Line, le.Col, le.Msg)
		} else {
			fmt.Fprintf(os.Stderr, "%s：%v\n", path, lerr)
		}
		return false
	}
	f, perr := parser.Parse(filepath.ToSlash(path), toks)
	if perr != nil {
		var pe *parser.Error
		if errors.As(perr, &pe) {
			fmt.Fprintf(os.Stderr, "%s 第 %d 行第 %d 列：%s\n", path, pe.Line, pe.Col, pe.Msg)
		} else {
			fmt.Fprintf(os.Stderr, "%s：%v\n", path, perr)
		}
		return false
	}
	given := filepath.ToSlash(path)
	if root != "" {
		p.PkgRoots[given] = root
	}
	p.Files = append(p.Files, f)
	for _, imp := range f.Imports {
		if imp.Package != "" {
			// 整块挂名那一句：包根与门面文件由 pk 解析，门面照常递归进来。
			newRoot, facade, rerr := pk.Resolve(p, given, imp.Path, root)
			if rerr != nil {
				fmt.Fprintln(os.Stderr, rerr)
				return false
			}
			if p.Facades == nil {
				p.Facades = map[string]bool{}
			}
			// 门面就是这一份：包对外的那一个文件。"门面不能交出依赖里的类型"这条检查
			// 只针对它，包里的其余文件不受这一条限制。
			p.Facades[filepath.ToSlash(facade)] = true
			if !load(p, facade, seen, pk, newRoot) {
				return false
			}
			continue
		}
		// 花括号那一种：路径必须以 ./ 或 ../ 开头，不是这种写法的句子不去盘上查找，
		// 由检查一侧给出报错。
		if !check.IsLocalImportPath(imp.Path) {
			continue
		}
		// 与检查器、降级同一份路径规则（check.ResolveImportPath）：命令行给的是带目录的路径时，
		// 这里各算各的就会出现"检查识别、后端不识别"。
		target := check.ResolveImportPath(given, imp.Path)
		// 包里的文件引包内另一片，那一片属于这一个包；伸到包外面去的不能顺手登记成这个包的东西
		// ——否则"包里的文件只能引同一个包里的文件"这条检查就报不出来了，而那一份容器到使用方
		// 那儿根本编不过（外面那一片没跟着进容器）。
		next := root
		if root != "" && !strings.HasPrefix(target, root+"/") {
			next = ""
		}
		if !load(p, target, seen, pk, next) {
			return false
		}
	}
	return true
}

// cmdIR 把降级出来的中间码打到标准输出，一行一条指令。
//
// 走的是 `nf build` 的前半段：检查 → 降级，到中间码就停，不发机器码也不落任何文件。
// 形状与单测里 `ir.Dump` 打出来的那份一字不差。
//
// en: cmdIR prints the lowered intermediate code to standard output, one instruction per line.
// It walks the first half of `nf build`: check then
// lower, stopping at intermediate code with no machine code and no file written. The output is
// identical, character for character, to what ir.Dump prints in the unit tests.
func cmdIR(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "要给一个 .nf 文件")
		return 2
	}
	entry := filepath.ToSlash(args[0])
	prog := &check.Program{Entry: entry}
	seen := map[string]bool{}
	pk := pkgload.New()
	if !load(prog, entry, seen, pk, "") {
		return 1
	}
	p, ok := compileIR(prog)
	if !ok {
		return 1
	}
	fmt.Print(ir.Dump(p))
	return 0
}

// cmdLSP 起语言服务器：标准输入接收编辑器送来的报文，标准输出回答复与波浪线。
//
// 这一条不读命令行给的文件名，读的是编辑器里那一份当前文本；报错的原文与行列与
// `nf check` 同一份，只是换成编辑器能呈现的形状（internal/ls）。
// 除了协议那几条报文，标准输出上不该有第二个字节。
//
// en: cmdLSP starts the language server: it takes the editor's messages on standard input and
// answers with responses and squiggles on standard output. This path does not read a
// file name from the command line but the current text inside the editor; the error wording and
// its line and column are the same ones `nf check` produces, only in a shape the editor can
// draw (internal/ls). Apart from the protocol's own messages, standard output must not carry a
// second byte.
func cmdLSP() int {
	if err := ls.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "语言服务器：", err)
		return 1
	}
	return 0
}

// cmdVersion 返回编译器自己是哪一版。不读任何输入，退出码恒 0——脚本用它检测编译器是否已安装。
//
// en: cmdVersion reports which version the compiler itself is. It reads nothing and always
// exits 0 - scripts use it to check whether the compiler is installed.
func cmdVersion() int {
	fmt.Printf("nf %s\n", version)
	return 0
}

// cmdTokens 把文件切成记号流打出来，一行一个：类型:文字。
//
// 站点的代码高亮以它为参照——高亮器与编译器必须识别同一套记号。
//
// en: cmdTokens cuts a file into its token stream and prints one token per line as kind:text.
// The site's syntax highlighter uses it as the reference: highlighter and compiler must
// recognise the same set of tokens.
func cmdTokens(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "要给一个 .nf 文件")
		return 2
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	toks, lerr := lexer.Lex(string(src))
	if lerr != nil {
		var le *lexer.Error
		if errors.As(lerr, &le) {
			fmt.Fprintf(os.Stderr, "第 %d 行第 %d 列：%s\n", le.Line, le.Col, le.Msg)
		} else {
			fmt.Fprintf(os.Stderr, "%v\n", lerr)
		}
		return 1
	}
	for _, t := range toks {
		text := t.Text
		if t.Kind == lexer.EOF {
			break
		}
		if t.Kind == lexer.NL {
			text = `\n`
		}
		fmt.Printf("%s:%s\n", t.Kind, text)
	}
	return 0
}

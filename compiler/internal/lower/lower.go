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

// Package lower 把语法树连同检查出来的结论落成中间码（编译流水线的中间一层）。
//
// 类型、名字指向哪儿、调用落到哪个函数，全都从 check.Facts 里取——这一层不重新
// 推断任何东西。碰到还没支持的构造就当场说清是哪一条：宁可编译不了，
// 也不能悄悄发一段少了检查的机器码。
//
// en: Package lower turns the syntax tree plus the checker's conclusions into intermediate code
// (the middle layer of the compilation pipeline). Types, where a name points, which function a
// call lands on are all taken from check.Facts - this layer re-infers nothing. When it meets a
// construct that is not supported yet it names that construct on the spot: better not to
// compile than to quietly emit machine code that skipped a check.
package lower

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

// minInt64 用来识别"最小值除以 -1"这一条——它在 CPU 上是硬件异常，必须提前拦截。
//
// en: minInt64 exists to recognise "the most negative value divided by -1", which is a hardware
// exception on the CPU and must be intercepted beforehand.
const minInt64 = int64(-9223372036854775807 - 1)

// 入口函数名。用户函数按名字全程序唯一，这个名字占不掉（关键字与保留名规则）。
//
// en: The entry function's name. A user function name is unique program-wide by its name, and
// keyword and reserved-name rules mean this name cannot be taken.
const entryFn = "nef.main"

// releaseTopFn 是"释放顶层全部持有"那一段的函数名。用户函数名不允许含点，不会与之冲突。
// 单独成一个函数是为了让 exit 也能调用它：exit 可以在任何深度上被调用，帧链无法回退，
// 顶层这一段只能做成任何位置都能调用的独立函数（规则"已经建好的值照常释放"）。
//
// en: releaseTopFn is the name of the "release everything the top level holds" section. User
// function names contain no dot, so it cannot collide. It is its own function so that
// exit can call it too: exit may be called at any depth, where the frame chain cannot be walked
// back, so the top-level section has to be a standalone function callable from anywhere (values
// already built are released as usual).
const releaseTopFn = "nef.release_top"

// recordSlots 是帧内清理记录要在帧上占的格数：落点、这一帧的 rbp、rsp、链上上一条、种类。
// 与运行期 `runtime.hnd*` 那五个偏移一一对应（一个位置 8 字节）。
//
// en: recordSlots is how many frame slots a frame-scope record occupies: destination, this
// frame's rbp, rsp, the previous entry on the chain, and kind. It corresponds one to one with
// the five runtime offsets hnd* (8 bytes each).
const recordSlots = 5

// 内建错误的码。10000 往上归语言自己，0～9999 留给程序：
// 两段挨着取值会让人误以为程序错误码与内建号重合，隔到一万以上就分得开。
//
// en: The built-in error codes. 10000 and above belong to the language, 0-9999 to the
// program: writing the two bands adjacently would make people think they hit a
// built-in code, and starting at ten thousand keeps them apart.
const (
	codeUnbox    = 10011 // 拆盒时类型不匹配
	codeRange    = 10013 // 换算出来的值超出目标类型（目前只有 as byte 与 byte 的 ++／-- 会触发）
	codeOverflow = 10021
	codeDivZero  = 10022
	codeIndex    = 10031 // 下标越界：容器与文本共用一条
	codeKey      = 10032 // dict 没有这个键
	codeMutate   = 10033 // 遍历途中改了容器
	codeNull     = 10092 // 对着空句柄取成员或调用方法
)

// 每条抛出都要一句消息。同一句话在文本池里只留一份。
//
// en: Every throw needs a message. One identical message is kept only once in the text pool.
var builtinMsg = map[int]string{
	codeUnbox:    "拆开的类型对不上",
	codeRange:    "超出了 byte 的值域",
	codeOverflow: "整数运算溢出",
	codeDivZero:  "整数除以 0",
	codeIndex:    "下标越界",
	codeKey:      "没有这个键",
	codeMutate:   "遍历途中改了容器",
	codeNull:     "对着空句柄取成员或调用方法",
}

// 类型字：堆对象那 8 字节头里记的编号，`is` 与 `as` 就比它。
// 基础类型与三种容器占死前几格，具名类型（class／struct／enum）从 firstNamedTag 起
// 按名字排——排序而不是按书写顺序，是为了同一套类型在任何编译顺序下都拿到同一个号。
// 容器也要号：不然一只 list 的头会跟某个 class 撞上。
//
// en: Type words: the number recorded in the 8-byte header of a heap object, which `is` and
// `as` compare. The basic types and the three containers occupy the first
// slots for good, and named types (class/struct/enum) start at firstNamedTag ordered by name -
// sorted rather than in writing order so that the same set of types receives the same numbers
// whatever the compile order. Containers need numbers too: otherwise a list's header would
// collide with some class.
const (
	tagBool = iota + 1
	tagByte
	tagUnicode
	tagInt
	tagFloat
	tagStr
	tagList
	tagDict
	tagSet
	tagBytes // 字节块这一档：块头上记的是它，与 `internal/runtime/bytes.go` 的 tagBytes 同一个编号
	firstNamedTag
)

type L struct {
	p     *ir.Prog
	f     *check.Facts
	files []*ast.File
	entry string
	// pkgFacades 是那句包引入实际 load 的门面文件路径（那句引入 → 路径，由装载那一侧算好）。
	// 排执行顺序用它：照 `imp.Path` 最后那一段拼出的 `<包名>.nf` 是源码作者侧的写法，
	// 与磁盘上的实际文件名可能不同。
	pkgFacades map[string]string
	// pkgRoots 说每一份文件属于哪一个包（文件路径 → 包根）。名字表、字段表、类型字与 IR 里
	// 那个函数名，包里的都带这个前缀——与检查那头用的是同一个键。
	pkgRoots map[string]string
	glob     map[*ast.Decl]ir.G
	gcount   int
	// funcFile 与 funcGlobals 记每一个用户函数与方法是**在哪一份文件里写的**、那一份文件顶层
	// 那张表长什么样。补参数的默认值要用：默认值那一段是在被调方那份文件里写的，
	// 它读的 `const` 是被调方那一个，不是调用方同名的那一个。
	funcFile    map[string]string
	funcGlobals map[string]map[string]ir.G
	args        ir.G // 入口文件顶层的 args：一个静态槽，启动时由 @args 填
	msgID       map[string]ir.S
	tags        map[string]int64
	// objKind 记每个具名类型是 struct 还是 class（形状表要按这两种分别处理）。
	// tagTab 是"类型字→形状"那一张表的编号，没打印过盒子就一直是 -1。
	objKind map[string]check.Kind
	tagTab  ir.D
	// topOwns 是各文件顶层登记的、由 nef.main 统一归还的那些静态槽，按执行顺序排列（被引的文件先），
	// 在所有文件的顶层都跑完、@exit 之前按反序归还。顶层跑完时就还不行的原因是：
	// 本文件的函数可能在别的文件的顶层里被调用，那时那个槽仍需可读。
	topOwns []own
	// freshBack 与 checkingBack 是"函数返回的引用类型值除接住它的一侧外没有其他持有者"这条判定的
	// 备忘录。checkingBack 用来断环——自调用与互调用成环时，
	// 环上的函数一律按"证不出来"处理。
	freshBack    map[string]bool
	checkingBack map[string]bool
	// json 是 nef.json 那十几支共用的七个静态槽。第一次降到 json 的成员才占用，
	// 没用到的程序不额外占用位置。
	json *jstate
	// debug 说这一趟是给调试器编的（check.Program.Debug）：每一条语句前多一条检查点。
	// dbgID 是全程序共用的编号——宿主拿它当"这一段机器码对应源码哪一句"的键，跨函数也不能重号。
	debug bool
	dbgID int
}

// Run 返回整个程序的中间码。
//
// en: Run returns the intermediate code of the whole program.
func Run(prog *check.Program, f *check.Facts) (*ir.Prog, error) {
	l := &L{p: &ir.Prog{Entry: entryFn, Debug: prog.Debug}, f: f, files: prog.Files, entry: prog.Entry,
		pkgFacades: prog.PkgFacades,
		pkgRoots:   prog.PkgRoots,
		glob:       map[*ast.Decl]ir.G{}, msgID: map[string]ir.S{},
		funcFile: map[string]string{}, funcGlobals: map[string]map[string]ir.G{},
		objKind: map[string]check.Kind{}, tagTab: -1,
		freshBack: map[string]bool{}, checkingBack: map[string]bool{}, debug: prog.Debug}
	l.buildTags()
	order, err := l.execOrder()
	if err != nil {
		return nil, err
	}
	// 静态槽按执行顺序排：被引的文件先跑，入口最后。
	for _, file := range order {
		for _, s := range file.Stmts {
			if d, ok := s.(*ast.Decl); ok {
				l.glob[d] = ir.G(l.gcount)
				l.gcount++
			}
		}
	}
	// args 占一个静态槽，排在所有文件顶层名字之后：它在文件里写不出来，是运行期预先摆好的。
	l.args = ir.G(l.gcount)
	l.gcount++
	// 打印机是编译器自己写的那个函数，它要在第一行用户代码被降出去之前就登记好——
	// 不然 call 找不到它。身份表那两个静态槽也由它申请。
	buildPrint(l)
	// 析构器与打印机共用那张形状表与那一套中间码工具箱（destroy.go）：块头上多了一个
	// "正在被析构"的标记，环与同一块被两处指着都只析构一次。调用它的有两处：每一层的块尾（`fc.close`）
	// 与程序尽头释放顶层持有的那一段。
	buildDestroy(l)
	// json 那一组与打印机／析构器同一性质：它们是编译器发的函数，形状表里那条 KindJson 要
	// 在 nef.print 与 nef.destroy 登记好之前就存在于"按名字找标号"的那一张表里，所以在这里
	// 一次登记齐，而不是等第一处 json.* 降级时才登记（那样析构器里就没有那一条分支了）。
	// 代价是每一个程序都带着这一组函数——与运行期那一整块静态数据同一个做法。
	ensureJSON(l)
	// 顶层那一段先占下这一个函数：里面要释放哪些槽位，得等所有文件的顶层都降完才数得清（l.topOwns）。
	// 先占是因为 `exit` 的退出清理代码要在任何深度上调用到它，而那些代码比这一段先降——
	// 发射器按名字找标号，调用时它还没登记就找不到了。
	l.p.Func(releaseTopFn)

	var errs []string
	// 调用点补默认值要按"那一段写在哪一份文件里、那一份文件的顶层表是哪一张"来算。
	// 这两样必须先在全部文件上登记齐，再落函数体：函数体按文件顺序降级，入口文件排在前，
	// 其中**自己写的函数**调用后面那一份文件里的方法时，若等落函数体那一趟才记，被调方的条目
	// 还没写上，报出来的是"某个名字还没排到静态槽"（`webtest/cases/login.nf` 的 `before_each`
	// 调 `page.expect_title("登录")` 即此类调用）；顶层代码最后降级，直接调用不受影响。
	for _, file := range l.files {
		g := l.fileGlobals(file)
		for _, d := range file.Defs {
			switch x := d.(type) {
			case *ast.Func:
				l.funcFile[l.symKey(file, x.NameText)] = file.Path
				l.funcGlobals[l.symKey(file, x.NameText)] = g
			case *ast.Object:
				owner := l.symKey(file, x.NameText)
				for _, m := range x.Methods {
					l.funcFile[owner+"."+m.NameText] = file.Path
					l.funcGlobals[owner+"."+m.NameText] = g
				}
			}
		}
	}
	for _, file := range l.files {
		g := l.fileGlobals(file)
		for _, d := range file.Defs {
			switch x := d.(type) {
			case *ast.Func:
				if err := l.function(file, "", x, g); err != nil {
					errs = append(errs, err.Error())
				}
			case *ast.Object:
				// class 的方法体也在这儿落：接口里的方法声明没有体，轮不到它
				for _, m := range x.Methods {
					if err := l.function(file, l.symKey(file, x.NameText), m, g); err != nil {
						errs = append(errs, err.Error())
					}
				}
			}
		}
	}
	// 每个文件的顶层各成一个函数，nef.main 按执行顺序挨个调
	main := l.p.Func(entryFn)
	c := &fc{l: l, name: entryFn, f: main}
	c.open()
	// winsock 的初始化，排在所有文件的顶层之前。
	// 放在这里而不是每个用得到它的成员里，是因为这条路只有一条：一次调用、一次收尾（在 `@exit` 里）。
	main.Emit(&ir.Call{Fn: "@winsock_start", FT: []ir.T{}})
	// 兜底处理：一路没人接住的抛出走到 @throw_walk 时若链已为空，会只打印消息就直接退 1，
	// 把顶层的释放整个跳过。挂上这一个处理点后，抛出走完中间那些帧内清理记录、仍没人接住时
	// 到达的是这里：打印消息、释放顶层持有的全部值、再退 1——与"正常走完"和 `exit` 三条收尾路径一致。
	// 正常走完时在调用 release_top 之前先把它摘掉，链上不留残留条目。
	uncaught := main.Fresh()
	main.Emit(&ir.PushHandler{Catch: uncaught})
	for _, file := range order {
		top := "nef.top." + file.Path
		tf := l.p.Func(top)
		tc := &fc{l: l, f: tf, name: top, glob: l.fileGlobals(file), filePath: file.Path}
		tc.open()
		if file.Path == l.entry {
			// args 是入口顶层预先摆好的那个位置，启动时由运行期解析命令行填入
			h := tf.Temp()
			tf.Emit(&ir.Call{D: h, HasD: true, Fn: "@args", FT: []ir.T{}, RT: ir.TInt})
			tf.Emit(&ir.Mov{D: l.args, S: h, T: ir.TInt})
			// args 是入口顶层的一个 `let:list<string>`，所以按同一份记录在程序尽头归还。
			// 登记排在入口那些声明之前 ⇒ 倒着还的时候它落在最后，与"先造的后还"一致。
			argsDesc, err := l.descOf(check.Type{Kind: check.List, Name: "list",
				Args: []check.Type{{Kind: check.Str, Name: "string"}}})
			if err == nil {
				l.topOwns = append(l.topOwns, own{slot: l.args, desc: argsDesc})
			}
		}
		for _, s := range file.Stmts {
			if err := tc.stmt(s); err != nil {
				errs = append(errs, fmt.Sprintf("%s：%v", file.Path, err))
			}
		}
		tc.endFrame()
		main.Emit(&ir.Call{Fn: top, FT: []ir.T{}})
	}
	// 顶层那一层的尽头在这里：所有文件的顶层都跑完了，这一层的值按登记的**反序**归还。
	// 登记是按执行顺序走的（被引的文件先、入口最后），所以还的时候正过来：
	// 入口文件的声明先还、args 那个位置最后——"先造的后还"这一条与函数帧上完全一样。
	// 早一步还（每个文件的顶层函数自己收尾时还）是不行的：这些槽是静态槽，
	// 本文件的函数可能在别的文件的顶层里被调用，那时它们仍需可读。
	//
	// 这一段单独成一个函数的原因是 `exit` 也要调用它（"已经建好的值照常释放"）：
	// exit 可以在任何深度上被调用，帧链无法回退，所以顶层这一段必须做成任何位置都能调用的独立函数。
	release := l.p.Func(releaseTopFn)
	// 这一个函数一进门就设置一个"正在释放"的标记（静态槽，用户拿不到名字）：兜底那一段要根据它
	// 决定是否再调用一遍 release_top——这是防止递归进入的护栏。
	releasing := l.hidden()
	release.Emit(&ir.Mov{D: releasing, S: ir.K(1), T: ir.TInt})
	(&fc{l: l, f: release, name: releaseTopFn}).destroyAll(l.topOwns)
	release.Emit(&ir.Ret{})
	// 正常走完时先摘掉兜底处理点，再释放顶层持有的值（处理点是堆上的一块，不摘就留在链上）。
	main.Emit(&ir.PopHandler{})
	main.Emit(&ir.Call{Fn: releaseTopFn, FT: []ir.T{}})
	c.close()
	main.Emit(&ir.Call{Fn: "@exit", Args: []ir.Val{ir.K(0)}, FT: []ir.T{ir.TInt}})
	main.Emit(&ir.Ret{})
	// 一路没人接住的抛出走到这里。错误码与消息先落进两个槽——下面要 call 出去，
	// rax／rdx 里的值一过 call 就没了（catch 块那一步实测的结论，规则同样适用于此处）。
	// @throw_walk 命中这一个处理点时已经把它从链上摘除并 @free 了，所以再次抛出通常到达的是空链、
	// 直接由 @throw_walk 打印后就退出。**还存在一条能绕回这里的路径**：`exit` 调完 release_top
	// 再退出时，兜底处理点还挂在链上，若释放途中抛出一次就会绕回这一段——所以这里先检查
	// "正在释放"标记，标记在就只打印、只退出，绝不再调用第二遍 release_top。
	main.Emit(&ir.Lbl{At: uncaught})
	errCode := main.Temp()
	errMsg := main.Temp()
	main.Emit(&ir.Catch{D: errCode, E: errMsg})
	main.Emit(&ir.Call{Fn: "@put_errline", Args: []ir.Val{errCode, errMsg},
		FT: []ir.T{ir.TInt, ir.TInt}})
	busy := main.Temp()
	releaseNow := main.Fresh()
	skipRelease := main.Fresh()
	main.Emit(&ir.Mov{D: busy, S: releasing, T: ir.TInt})
	main.Emit(&ir.Jz{V: busy, To: releaseNow}) // 标记未设置＝这一趟是第一次收尾，去释放顶层持有的值
	main.Emit(&ir.Jump{To: skipRelease})       // 标记已设置＝释放途中又抛了一次，只打印、只退出，不再调用第二遍
	main.Emit(&ir.Lbl{At: releaseNow})
	main.Emit(&ir.Call{Fn: releaseTopFn, FT: []ir.T{}})
	main.Emit(&ir.Lbl{At: skipRelease})
	main.Emit(&ir.Call{Fn: "@exit", Args: []ir.Val{ir.K(1)}, FT: []ir.T{ir.TInt}})
	main.Emit(&ir.Ret{})

	// 形状表里"类型字→形状"那一张要等全程序降完才填得出来（哪些类型被装过盒子，
	// 到这一步才收得齐），所以放在最后一步。
	if err := l.fillTagTable(); err != nil {
		return nil, err
	}

	l.p.Globals = l.gcount // 连 hidden 那两个槽一起算：静态槽到此才收得齐

	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, fmt.Errorf("后端还没做到这些：\n  %s", strings.Join(dedupe(errs), "\n  "))
	}
	return l.p, nil
}

// symKey 把一份文件里写的名字换成它在整程序里的规范名：包里的带包根，工程的文件就是原名。
// 检查那头交出来的每一张表（字段表、函数表、类型字）都按这一个键排，两边必须一模一样，
// 不然两份同名不同版的包并在一处就会共用同一份形状、同一个函数。
//
// en: symKey turns a name written in a file into its canonical name across the program: inside
// a package it carries the package root, in a project file it is the original name. Every table
// the checker hands out (field table, function table, type words) is keyed the same way and the
// two sides must match exactly, otherwise two versions of one same-named package placed
// together would share one shape and one function.
func (l *L) symKey(file *ast.File, name string) string {
	return check.QualKey(l.pkgRoots[file.Path], name)
}

// buildTags 给每个具名类型（class／struct／enum）排一个号。
//
// 号只用来识别盒子（`as` 与 `is` 比它），不参与任何静态决定，
// 所以排出来的顺序只要同一套程序里前后一致就行；按名字排最简单，也不受文件顺序影响。
//
// en: buildTags assigns a number to each named type (class/struct/enum). The number is only
// used to recognise a box (what `as` and `is` compare) and takes part in no static decision,
// so the order only has to be consistent within one program; ordering by name is cheapest and
// is unaffected by file order.
func (l *L) buildTags() {
	var names []string
	seen := map[string]bool{}
	for _, file := range l.files {
		for _, d := range file.Defs {
			var n string
			switch x := d.(type) {
			case *ast.Object:
				n = l.symKey(file, x.NameText)
				l.objKind[n] = check.Struct
				if x.Kind == "class" {
					l.objKind[n] = check.Class
				}
			case *ast.Enum:
				n = l.symKey(file, x.NameText)
				l.objKind[n] = check.Enum
			}
			if n != "" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	// 内建的那两个类型 `Calendar` 与 `Response`：源码里写不出来，号与形状照样要登记——
	// 盒子识别它们、打印机与析构器按形状表处理它们，都靠这两行把名字排进同一张表。
	if !seen["Calendar"] {
		seen["Calendar"] = true
		names = append(names, "Calendar")
		l.objKind["Calendar"] = check.Struct
	}
	if !seen["Response"] {
		seen["Response"] = true
		names = append(names, "Response")
		l.objKind["Response"] = check.Struct
	}
	sort.Strings(names)
	l.tags = map[string]int64{}
	for i, n := range names {
		l.tags[n] = firstNamedTag + int64(i)
	}
}

// tagOf 返回一个类型在盒子上的编号。
//
// en: tagOf returns a type's number as stored in a box.
func (l *L) tagOf(t check.Type) (int64, bool) {
	switch t.Kind {
	case check.Bool:
		return tagBool, true
	case check.Byte:
		return tagByte, true
	case check.Unicode:
		return tagUnicode, true
	case check.Int:
		return tagInt, true
	case check.Float:
		return tagFloat, true
	case check.Str:
		return tagStr, true
	}
	n, ok := l.tags[t.Name]
	return n, ok
}

// fieldOff 查一个字段在对象里第几个字节。字段表是前端摊平好交出来的。
//
// en: fieldOff looks up which byte of an object a field is at. The field table is flattened and
// handed over by the front end.
func (l *L) fieldOff(typeName, field string) (int64, bool) {
	for _, fi := range l.f.Fields[typeName] {
		if fi.Name == field {
			return fi.Off, true
		}
	}
	return 0, false
}

// narrow 是窄字段的读法：boolean 与 byte 占一个字节、unicode 四个，
// 槽里躺的却是 8 字节，所以碰它们要按宽度取，不能整份搬。
//
// en: narrow is how a narrow field is read: boolean and byte are one byte, unicode four, while
// the slot holds 8 bytes - so touching them must take the width, never move the whole slot.
func (l *L) narrow(t check.Type) int {
	switch l.f.Sizeof(t) {
	case 1, 4:
		return l.f.Sizeof(t)
	}
	return 0
}

// mapLit 落 `{"a": 1, "b": 2}`：每一对先按书写顺序算出来，再造表、再一对一对摆进去。
// 摆进去走的是 @map_put，跟程序里 `d[k]=v` 同一条路——包括途中该不该扩容。
//
// en: mapLit lowers `{"a": 1, "b": 2}`: every pair is computed in written order first, then the
// table is built and the pairs are placed one by one. Placement goes through @map_put, the same
// path as `d[k]=v` in a program - including whether the table should grow along the way.
func (c *fc) mapLit(x *ast.MapLit, t check.Type) (ir.Val, check.Type, error) {
	outer, from := c.openWindow() // 同 listLit：进表的那几份挑出去，中间读走的当场归还
	type pair struct {
		k, v   ir.Val
		kt, vt check.Type
	}
	pairs := make([]pair, 0, len(x.Pairs))
	for _, p := range x.Pairs {
		k, kt, err := c.expr(p.Key)
		if err != nil {
			return nil, t, err
		}
		v, vt, err := c.expr(p.Value)
		if err != nil {
			return nil, t, err
		}
		// 值那个位置与 `list` 字面量同一本账：来路是当场造好的那一块就直接放入，
		// 是一个名字或一个字段就另起一份——`@map_put` 把句柄原样收进桶里，不拷就是两个持有者。
		v, err = c.cellValue(v, vt, p.Value)
		if err != nil {
			return nil, t, err
		}
		pairs = append(pairs, pair{k: k, v: v, kt: kt, vt: vt})
	}
	d, err := c.newMap(int64(len(pairs)), t, tagDict)
	if err != nil {
		return nil, t, err
	}
	keeps := make([]ir.Val, 0, len(pairs)*2)
	for _, p := range pairs {
		c.f.Emit(&ir.Call{Fn: "@map_put",
			Args: []ir.Val{d, c.bitsOf(p.k, p.kt), c.bitsOf(p.v, p.vt)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt}})
		keeps = append(keeps, p.k, p.v) // 键与值都归桶里的槽位，其余的读走步骤这一趟就归还
	}
	c.closeWindow(outer, from, keeps...)
	return d, t, nil
}

// setLit 落 `{1, 2, 3}`：与 dict 同一张表，值那个位置空着不用。
// 空的是 `{}` 还是 `{1}` 由左边的类型标注分清，到这儿已经确定是 hashset。
//
// en: setLit lowers `{1, 2, 3}`: the same table as dict, with the value position left unused.
// Whether `{}` is an empty map or `{1}` is decided by the type annotation on the left, so
// by here it is certainly a hashset.
func (c *fc) setLit(x *ast.SetLit, t check.Type) (ir.Val, check.Type, error) {
	outer, from := c.openWindow() // 同 listLit：成员进了位置，其余的读走步骤当场归还
	vals := make([]ir.Val, 0, len(x.Items))
	ets := make([]check.Type, 0, len(x.Items))
	for _, it := range x.Items {
		v, vt, err := c.expr(it)
		if err != nil {
			return nil, t, err
		}
		vals = append(vals, v)
		ets = append(ets, vt)
	}
	d, err := c.newMap(int64(len(vals)), t, tagSet)
	if err != nil {
		return nil, t, err
	}
	for i, v := range vals {
		c.f.Emit(&ir.Call{Fn: "@map_put",
			Args: []ir.Val{d, c.bitsOf(v, ets[i]), ir.K(0)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt}})
	}
	c.closeWindow(outer, from, vals...) // 成员的槽位是持有者，中间读走的几步这一趟归还
	return d, t, nil
}

// newMap 造一张 dict／hashset 的表：桶数按元素个数预排，键的比较方式在这里定下来。
// 定下来之后就写在表自己身上，之后每次探测、每次搬迁都按它执行（表上的 cMode 字段）。
//
// en: newMap builds a dict/hashset table: the bucket count is pre-arranged from the element
// count and the way keys compare is fixed here. Once fixed it is written on the table itself,
// and every later probe and every later move follows it - the cMode field on the table.
func (c *fc) newMap(n int64, t check.Type, tag int64) (ir.Val, error) {
	mode := c.keyMode(t)
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@map_new",
		Args: []ir.Val{ir.K(n), ir.K(tag), ir.K(mode)},
		FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
	return d, nil
}

// cellOf 返回"容器第 i 个槽位在内存里的地址"：例程顺手拦截越界（10031 号）与缺键（10032 号）。
// 返回的是地址而不是值，读、写、读走三件事才共用得到同一道检查。
//
// bytes 的槽位是一个字节：返回的还是地址，读法与写法照旧共用这一道检查，
// 只是取与放要按一字节的宽度走（见 cellWidth）。
//
// en: cellOf returns "where in memory container slot i is": the routine also blocks
// out-of-range (code 10031) and missing key (code 10032). It returns an address rather than a
// value so that reading, writing and reading-out all share one check. A bytes position is one
// byte: still an address, still the shared check, but the fetch and the
// store go at one-byte width (see cellWidth).
func (c *fc) cellOf(base ir.Val, bt check.Type, at ir.Val) (ir.Val, error) {
	switch bt.Kind {
	case check.List:
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@list_at",
			Args: []ir.Val{base, at}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, nil
	case check.Bytes:
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@bytes_at",
			Args: []ir.Val{base, at}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, nil
	case check.Dict:
		// 散列与相等的算法在建表那一刻就写进表里了，之后不再需要持有类型信息
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@map_at",
			Args: []ir.Val{base, at}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, nil
	}
	return nil, fmt.Errorf("%s 上的下标还没接到后端", bt)
}

// keyMode 说这张表的键怎么算散列与相等：文本按内容比，其余一律比那 64 位——
// 句柄就是身份，整数家族本来就是数。小数与 struct 当不了键，轮不到这儿。
//
// en: keyMode says how this table hashes and compares keys: text by content, everything
// else by the full 64 bits - a handle IS the identity and the integer family is a number
// anyway. Floats and structs cannot be keys, so they never reach here.
func (c *fc) keyMode(bt check.Type) int64 {
	if len(bt.Args) > 0 && bt.Args[0].Kind == check.Str {
		return 1
	}
	return 0
}

// indexRead 落 `a[i]`。装的是引用类型时这一读就是**读走**：那个位置之后返回 null，
// 所以读完顺手把那个位置写空。装值类型时是普通拷贝，不碰那个位置。
//
// en: indexRead lowers `a[i]`. When the position holds a reference type this read IS a
// read-out: the position returns null afterwards, so the position is written empty right
// after. A value type is an ordinary copy and the position is untouched.
func (c *fc) indexRead(x *ast.Index, t check.Type) (ir.Val, check.Type, error) {
	base, bt, err := c.expr(x.What)
	if err != nil {
		return nil, t, err
	}
	at, _, err := c.expr(x.At)
	if err != nil {
		return nil, t, err
	}
	c.guardNotNull(base)
	if bt.Kind == check.Str {
		// 文本的第 i 个码点没有现成地址：UTF-8 变长，只能从字节 0 逐码点定位。
		// 返回的是一个码点的**值**（`unicode`），不是内存位置，所以走不到下面那条读写共用一路。
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@str_at",
			Args: []ir.Val{base, at}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	}
	cell, err := c.cellOf(base, bt, at)
	if err != nil {
		return nil, t, err
	}
	return c.readCell(cell, bt, t)
}

// cellWidth 说那个槽位里存几个字节。三种容器一律 8 字节（窄元素进容器也占满一个槽位，
// 为的是容器只有一种搬运方式）；`bytes` 的槽位就是一个字节——增加这一档宽度所节省的正是这 8 倍。
//
// 返回 0 是"按类型的默认宽度"，与中间码那两条 Load/Store 的约定同一个（`internal/ir/ir.go` 的 Size）。
//
// en: cellWidth says how many bytes lie in that position. The three containers are always 8
// bytes (a narrow element still occupies a full slot, so a container has exactly one way
// to move things); a `bytes` position is one byte - this width exists precisely
// to save that factor of eight. Returning 0 means "use the type's default width", the same
// convention as Size on the two Load/Store instructions in internal/ir/ir.go.
func cellWidth(bt check.Type) int {
	if bt.Kind == check.Bytes {
		return 1
	}
	return 0
}

// readCell 是"从容器位置里取值"的那一半：拿到位置之后怎么读、struct 要另起一份、
// 句柄读完把那个位置抹空。`a[i]`、`xs.first()`、`xs.last()`、`d.get(键, 默认)`
// 走的是同一条，读法不许有四份。
//
// en: readCell is the "take a value out of a container position" half: how to read once the
// position is found, a struct needs its own copy, and a handle leaves the position emptied.
// `a[i]`, `xs.first()`, `xs.last()`, `d.get(key, default)` all go through this one path -
// the read must not exist in four copies.
func (c *fc) readCell(cell ir.Val, bt check.Type, t check.Type) (ir.Val, check.Type, error) {
	d := c.f.Temp()
	c.f.Emit(&ir.Load{D: d, Base: cell, Off: 0, T: c.rt(t), Size: cellWidth(bt)})
	if t.Kind == check.Struct {
		// "一个位置一份"对 struct 来说是一整块：位置里的句柄仍需保留，
		// 读走的必须是另一个拷贝，不然读两次就得到两个持有者指向同一块
		cp, err := c.copyVal(d, t)
		c.noteBorrowed(cp, t)
		return cp, t, err
	}
	if !t.ByValue() {
		c.f.Emit(&ir.Store{Base: cell, Off: 0, S: ir.K(0), T: ir.TInt})
	}
	c.noteBorrowed(d, t)
	return d, t, nil
}

// noteBorrowed 登记"从容器位置读走、落在没名字的临时槽上"的那一块：只在某一层的窗口开着时
// 登记（`borrow > 0`），登记给哪一层就由那一层还——一次只读借用的标准库调用、
// 一个运算式、一句话都是一层窗口。
//
// 这条之所以安全：读走那一步已经把源位置抹成 `null`，这个位置上再没有第二个出处；
// 而接住它的那一侧要么把它挑出去自己持有（`closeWindow` 的 keep），要么根本不收留值——
// 标准库只读借用，运算式与语句到尽头就把值丢了。
// 会收留值的那几条路（容器 add、写进位置、进字段、交给用户函数的参数、装箱）在下去之前
// 把 borrow 清成 0，所以不会重复登记同一块。
//
// en: noteBorrowed registers the block "read out of a container position and landed in an
// unnamed temporary slot", but only while some layer's window is open (`borrow > 0`), and the
// layer it is registered to is the layer that returns it: a single read-only-borrow
// standard-library call, one expression, one statement -
// each is a layer of window. This is safe because the read-out step already zeroed the
// source position to `null`, so that position has no second source; and the receiving side
// either picks the value out and keeps it (keep in closeWindow) or keeps nothing at all -
// standard-library calls borrow read-only, and an expression or statement discards
// the value at its end. The paths that DO keep a value (container add, writing into a position,
// into a field, into a user function's parameter, boxing) clear borrow to 0 before descending,
// so they can never meet the same block.
func (c *fc) noteBorrowed(v ir.Val, t check.Type) {
	if c.borrow == 0 || !reclaimable(t) {
		return
	}
	if _, isSlot := v.(ir.L); !isSlot {
		return // 没落在槽位里的（直接在寄存器上）本版本不处理
	}
	d, err := c.l.descOf(t)
	if err != nil {
		return
	}
	c.temps = append(c.temps, own{slot: v, desc: d, direct: t.Kind == check.Str, closer: closerOf(t)})
}

// freeTemps 把 n 之后登记的那些临时槽当场还掉（反序，与块尾那一条同一规则）。
//
// en: freeTemps returns the temporary slots registered after n on the spot, in reverse order -
// the same rule as the block tail.
func (c *fc) freeTemps(n int) {
	for i := len(c.temps) - 1; i >= n; i-- {
		c.releaseOne(c.temps[i])
	}
	c.temps = c.temps[:n]
}

// openWindow 与 closeWindow 是一对：把"这一句里中间那几步读走的"登记下来，
// 语句走到尽头按读走的反序归还。复用标准库调用参数那一侧的窗口机制，只是开在语句这一侧而不是
// 一次调用的参数那一侧。
//
// 中间那几步要单独登记的原因是：只管最外层值的规则只看得到这一句最外面那一个值，而 `xs[0][0]`、`cs[0].n`
// 这类写法中间那一步读走的也落在一个没有名字的中转上——源位置那一步已经抹成 `null`，
// 外面那一个值又用不上它，于是一次循环遗留一块。实测两万圈每圈 88 字节。
//
// keep 是这一句要留下来的那几个位置（进名字的那一份、返回调用方的那一份、进容器那个位置的那一份）：
// 它们只是从这本账里挑出去，持有者是接住它们的那一侧。
//
// en: openWindow and closeWindow are a pair: they record "what the middle steps of
// this sentence read out", and at the end of the sentence return them in the reverse order of
// read-out. It reuses the standard-library argument window, only opened at the sentence side
// instead of at one call's arguments. The middle steps need their own entry because
// an outermost-value rule only sees the
// outermost value of this sentence, while in spellings like `xs[0][0]` and `cs[0].n` the middle
// step's read-out also lands in an unnamed temporary - the source position was already zeroed
// to `null` and the outer value does not use it, so each iteration kept one block. Measured: 88
// bytes per iteration over twenty thousand rounds. keep is the positions this sentence intends
// to retain (the one going into a name, the one returning to the caller, the one going into a
// container position): they are simply picked out of this ledger, because their holder is the
// receiving side.
func (c *fc) openWindow() (int, int) {
	outer, from := c.borrow, len(c.temps)
	c.borrow++
	return outer, from
}

func (c *fc) closeWindow(outer, from int, keep ...ir.Val) {
	c.borrow = outer
	for i := len(c.temps) - 1; i >= from; i-- {
		if keptSlot(c.temps[i].slot, keep) {
			continue
		}
		c.releaseOne(c.temps[i])
	}
	c.temps = c.temps[:from]
}

// keptSlot 判断这一个槽位是不是这一句要保留的那一些。每个中转槽都是新申请的，编号不会复用，
// 所以按编号比较就够了。
//
// en: keptSlot recognises whether this position is one of those the sentence keeps. Every
// temporary slot is requested fresh and numbers are never reissued, so comparing numbers is
// enough.
func keptSlot(slot ir.Val, keep []ir.Val) bool {
	for _, v := range keep {
		if v == slot {
			return true
		}
	}
	return false
}

// structLit 落 `P { x: 1, y: 2.0 }`：造一块、按字段各自的偏移摆进去。
//
// 字段必须列全是前端的检查规则，所以这里不补默认值；没写到的位置也不存在。
// 值的形状是"堆上一整块 ＋ 手里一个句柄"，返回的就是那个句柄。
//
// en: structLit lowers `P { x: 1, y: 2.0 }`: allocate a block and place each field at its own
// offset. Listing every field is the front end's rule, so no default is filled in here
// and positions not written do not exist. The value's shape is "a whole block on the heap plus
// a handle in hand", and what is returned is that handle.
func (c *fc) structLit(x *ast.StructLit, t check.Type) (ir.Val, check.Type, error) {
	defer c.noBorrow()() // 字段初值已进槽位，持有者是字段
	tag, ok := c.l.tagOf(t)
	if !ok {
		return nil, t, fmt.Errorf("%s 的类型字没排上", t)
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@alloc",
		Args: []ir.Val{ir.K(c.l.f.BlockSize(t)), ir.K(tag)},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	for _, f := range x.Fields {
		v, vt, err := c.expr(f.Value)
		if err != nil {
			return nil, t, err
		}
		off, err := c.fieldOffset(t.Name, f.NameText)
		if err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Store{Base: d, Off: int(off), S: v, T: c.rt(vt), Size: c.l.narrow(vt)})
	}
	return d, t, nil
}

// copyVal 是值类型每一次交出要做的"整份拷贝"：struct 另起一块，其余类型槽里本来就是一份完整的值。
//
// 调用它的地方都是"源名字之后仍然可用"的场合（`let q:P = p`、`p = q`、传参、`return`、
// 往对象字段与容器里摆）——引用类型走的是另一条路，那边检查器已经把源的名字作废，直接搬句柄。
//
// 字段里嵌着 `struct` 的位置要连着多拷一层：整块复制只会把里面的句柄原样照抄，
// 两个槽就指向同一份了。递归层数不会无限——字段表不允许成环（检查器的一条规则管直接自指，
// 环在这里再拒绝一次），所以递归能结束。
//
// en: copyVal is the wholesale copy every value-type handover performs: a struct gets a new
// block, and for the other types the slot already holds a complete value. Callers are all the
// places where the source name must remain usable afterwards (`let q:P = p`, `p = q`, passing
// an argument, `return`, placing into an object field or a container) - reference types take
// another path, where the checker already voided the source name and the handle is moved
// directly. A field that embeds a `struct` must be copied one level deeper: copying the block
// wholesale would duplicate the inner handle verbatim and two slots would point at one thing.
// The nesting cannot run forever - the field table may not cycle (the checker stops direct
// self-reference, and a cycle is refused again here), so the descent terminates.
func (c *fc) copyVal(v ir.Val, t check.Type) (ir.Val, error) {
	return c.copyValAt(v, t, nil)
}

func (c *fc) copyValAt(v ir.Val, t check.Type, onPath []string) (ir.Val, error) {
	if t.Kind != check.Struct {
		return v, nil
	}
	for _, seen := range onPath {
		if seen == t.Name {
			return nil, fmt.Errorf("%s 的字段绕成了环，整份拷贝递不到头：这一版不发这种码", check.BareName(t.Name))
		}
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@copy_block",
		Args: []ir.Val{v, ir.K(c.l.f.BlockSize(t))},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	path := append(append([]string{}, onPath...), t.Name)
	for _, fi := range c.l.f.Fields[t.Name] {
		if fi.Typ.Kind != check.Struct {
			continue
		}
		inner := c.f.Temp()
		c.f.Emit(&ir.Load{D: inner, Base: v, Off: int(fi.Off), T: ir.TInt, Size: c.l.narrow(fi.Typ)})
		cp, err := c.copyValAt(inner, fi.Typ, path)
		if err != nil {
			return nil, err
		}
		c.f.Emit(&ir.Store{Base: d, Off: int(fi.Off), S: cp, T: ir.TInt, Size: c.l.narrow(fi.Typ)})
	}
	return d, nil
}

// cellValue 是"往容器的一个槽位里放一个值类型"那一步：来路是当场造出来的那一块就不再拷第二次。
//
// 判据与声明、整份赋值同用一条（`fresh`）：struct 与容器的字面量、`new`、标准库返回的
// 那一块、拼接生成的文本，以及过得了 handsBackFresh 那道证明的自定义函数与方法。除这个槽位外没有别的
// 持有者拿着那一块，多拷一份就是把原来那一块丢在堆上——往 list 里放一个 `Rec { n: i }`，每个位置遗留 16 字节。
//
// 名字（`[one]`）与 class 里的 struct 字段（`[g.p]`）照旧拷贝：那两处读取不改动源头，名字之后仍可
// 再读，"一格一份"就得另起一块。
//
// en: cellValue is the step "put a value type into a container slot": when the block was just
// created on the spot it is not copied a second time. The test is shared with declarations and
// whole-value assignments (`fresh`): struct and container literals, `new`, a block
// returned by the standard library, assembled text, and a custom function or method that passes
// the handsBackFresh proof. Nothing but this position holds that block, so a second copy would
// abandon the original on the heap - putting `Rec { n: i }` into a list leaked 16 bytes per
// slot. A name (`[one]`) and a struct inside a class field (`[g.p]`) are still copied: those
// two reads do not disturb the source, the name can be read again afterwards, and "one block
// per slot" requires a fresh one.
func (c *fc) cellValue(v ir.Val, t check.Type, src ast.Expr) (ir.Val, error) {
	if t.Kind == check.Struct && c.fresh(src, t) {
		return v, nil
	}
	return c.copyVal(v, t)
}

// placeOf 是"要被修改的那一处在哪里"：与求值同一条路，只多一件事——容器里那一整块
// 返回的是**槽位里存的那份**，不另起拷贝。赋值若拿着拷贝去修改，写入的内容当场丢失，
// 还不报错（读回来是槽位里那句柄指向的那一块）。
//
// `x.f = v` 的被赋值对象 x 本身可以是一个下标（`sizes[0].h = 77`）或一个字段（`bn.size.h = 99`）：
// 字段那一路本来就不拷贝（fieldRead 返回句柄），要特殊处理的只有下标这一路。
//
// en: placeOf is "where is the thing about to be modified": the same path as evaluation with
// one difference - for a whole block inside a container it returns WHAT THE POSITION STORES,
// without making a copy. If an assignment modified a copy, the write would be lost immediately
// and nothing would report an error (reading back would show the block the position's handle
// points at). In `x.f = v`, x itself may be a subscript (`sizes[0].h = 77`) or a field
// (`bn.size.h = 99`): the field path never copied in the first place (fieldRead returns a
// handle), so only the subscript path needs special handling.
func (c *fc) placeOf(e ast.Expr) (ir.Val, check.Type, error) {
	ix, ok := e.(*ast.Index)
	if !ok {
		return c.expr(e)
	}
	base, bt, err := c.expr(ix.What)
	if err != nil {
		return nil, bt, err
	}
	at, _, err := c.expr(ix.At)
	if err != nil {
		return nil, bt, err
	}
	c.guardNotNull(base)
	cell, err := c.cellOf(base, bt, at)
	if err != nil {
		return nil, bt, err
	}
	// 槽位里装的是什么，以前端为这一个下标表达式算出的类型为准：dict 尖括号里第一位是键、
	// 第二位才是值，写死取 Args[0] 会把键当成槽位里的值——`d["甲"].n = 4` 因此报
	// "string 里没有字段 n"。读那一路本来就是照这个类型走的（indexRead 返回的就是它）。
	elem, ok := c.l.f.Types[ix]
	if !ok {
		// 前端没有记录这一个下标表达式的类型（走到这里说明它没有被检查过）：只能取尖括号里第一位，
		// 那一个是 list／hashset 的元素类型、dict 的键类型。
		if len(bt.Args) > 0 {
			elem = bt.Args[0]
		}
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Load{D: d, Base: cell, Off: 0, T: c.rt(elem)})
	if elem.Kind != check.Struct {
		// 其余类型走的就是原来那一条读法：引用类型顺手把那个位置写空（一个位置一份）
		if !elem.ByValue() {
			c.f.Emit(&ir.Store{Base: cell, Off: 0, S: ir.K(0), T: ir.TInt})
		}
		return d, elem, nil
	}
	return d, elem, nil
}

// listLit 落 `[a, b, c]`：先把每一项按书写顺序算出来，再造容器、再摆进去。
// 中间某一项抛出时不该留下一只建了一半的 list 在外头走。
//
// en: listLit lowers `[a, b, c]`: every item is computed in written order first, then the
// container is built and the items placed. If some item throws halfway, a half-built list must
// not be left outside.
func (c *fc) listLit(x *ast.ListLit, t check.Type) (ir.Val, check.Type, error) {
	// 这一句里的每一步都登记（与语句那一侧的窗口同一套机制），进位置的那几份按位置挑出去；
	// 此前这一段整体不登记，`[src[0]]` 那种"从另一只容器读走的槽位"因此每圈遗留一块无人归还。
	outer, from := c.openWindow()
	vals := make([]ir.Val, len(x.Items))
	for i, it := range x.Items {
		v, vt, err := c.expr(it)
		if err != nil {
			return nil, t, err
		}
		// 每一项都要有自己的一份：位置里存的是句柄，直接放进原句柄就有两个槽指同一块。
		// 来路是这一项才造出来的那一块（`[Rec { n: i }]`）就不另起。
		v, err = c.cellValue(v, vt, it)
		if err != nil {
			return nil, t, err
		}
		vals[i] = v
	}
	elem := check.Type{}
	if len(t.Args) > 0 {
		elem = t.Args[0]
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@list_new",
		Args: []ir.Val{ir.K(int64(len(vals))), ir.K(tagList)},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	for i, v := range vals {
		cell, err := c.cellOf(d, t, ir.K(int64(i)))
		if err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Store{Base: cell, Off: 0, S: v, T: c.rt(elem)})
	}
	c.closeWindow(outer, from, vals...) // 进位置的那几份留给对应槽位，中间那几步读走的当场归还
	return d, t, nil
}

// containerAction 识别标准库里挂在容器与文本上的那几个动作。落点在 check 那边拼成
// "list.add"／"length" 这样的串，容器动作不填 Recv，所以接收者要从点号左边反查。
//
// en: containerAction recognises the standard-library actions hanging on containers and text.
// The checker spells the destination as a string like "list.add" or "length", and a container
// action leaves Recv empty, so the receiver must be looked up from the left side of the dot.
func containerAction(name string) bool {
	switch name {
	case "length", "list.add", "list.remove", "list.sort", "list.index_of",
		"list.first", "list.last", "list.clear",
		"list.reverse", "list.add_all", "list.to_set", "list.slice", "list.insert_at", "list.remove_at",
		"dict.add", "dict.remove", "dict.contains", "dict.keys", "dict.values", "dict.get",
		"hashset.add", "hashset.remove", "hashset.contains",
		"hashset.union", "hashset.intersect", "hashset.difference",
		"bytes.slice", "bytes.to_base64",
		"socket.send", "socket.recv_into", "socket.close", "listener.accept", "listener.close",
		"process.wait", "process.close":
		return true
	}
	return false
}

// stdBorrowsArgs 说这一条标准库动作是不是只"读"参数（标准库组函数收参数按只读借用、
// 不收留值）。容器那几条一律返回"不是"：`add` 会把值留在槽位里（那一本账由那个位置
// 接管），remove／contains 走的是同一条 `stdContainer` 分派，分开要多看一层——宁可少归还，不多归还。
//
// en: stdBorrowsArgs says whether this standard-library action only READS its arguments
// (standard-library group functions take parameters as read-only borrows and keep no
// value). The container members always answer "no": `add` keeps the value in a position (that
// position takes over the ownership ledger), and remove/contains travel the
// same stdContainer dispatch, so telling them apart would need another level of analysis -
// better to return one block too few than one too many.
func stdBorrowsArgs(name string) bool {
	switch name {
	case "list.add", "list.remove", "dict.add", "dict.remove", "dict.contains",
		"hashset.add", "hashset.remove", "hashset.contains",
		"list.sort", "list.index_of", "dict.keys", "dict.values", "dict.get",
		"list.first", "list.last", "list.clear",
		"list.reverse", "list.add_all", "list.to_set", "list.slice", "list.insert_at", "list.remove_at",
		"hashset.union", "hashset.intersect", "hashset.difference",
		"bytes.slice":
		return false
	}
	return true
}

// noBorrow 在"值会被谁收留"的那一段代码里关闭中转登记，返回一个还原用的闭包：
// 交给用户函数的参数（可能被 return／写进字段／装进容器）、写进容器槽位、字面量的槽位、
// `new` 的字段初值、装箱——那些地方另有持有者，重复登记就会错误析构别人的块。
//
// en: noBorrow turns off temporary registration inside the code that asks "who will keep this
// value", and returns a closure that restores it: arguments to a user function (which may
// return it, store it in a field, or put it in a container), writing into a container position,
// a literal's position, a `new` field initialiser, boxing - those places have another holder,
// and registering twice would wrongly free somebody else's block.
func (c *fc) noBorrow() func() {
	saved := c.borrow
	c.borrow = 0
	return func() { c.borrow = saved }
}

// stdContainer 落标准库作用在容器与文本上的那几个动作。接收者挂在哪一类上，
// 从 Facts.Types[成员表达式的左边] 反查——check 那一侧给容器动作不填 Recv。
//
// en: stdContainer lowers the standard-library actions applied to containers and text. Which
// class the receiver belongs to is looked up from Facts.Types[the left side of the member
// expression], because the checker leaves Recv empty for container actions.
func (c *fc) stdContainer(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	if ref.Std == "length" {
		v, vt, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(v)
		off := runtime.CellLen
		switch vt.Kind {
		case check.List, check.Dict, check.Set:
		case check.Str:
			// 文本的 length 数的是码点，不是字节
			off = runtime.StrCount
		case check.Bytes:
			// 字节块的"多长"就是它有几个字节：那个位置与容器的 CellLen 同为偏移 0
			off = runtime.BytesLen
		default:
			return nil, t, fmt.Errorf("length 收不了 %s", vt)
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Load{D: d, Base: v, Off: off, T: ir.TInt})
		return d, t, nil
	}

	m, ok := x.Callee.(*ast.Member)
	if !ok {
		return nil, t, fmt.Errorf("标准库的 %s 该写成点名式，前端与后端对不上号", ref.Std)
	}
	recv, rt, err := c.expr(m.What)
	if err != nil {
		return nil, t, err
	}
	c.guardNotNull(recv)
	switch ref.Std {
	case "dict.add", "dict.remove", "dict.contains",
		"hashset.add", "hashset.remove", "hashset.contains":
		return c.mapAction(x, ref, t)
	}
	switch ref.Std {
	case "list.add":
		// 外面那一层的窗口管的是"这一句里没人接住的那些块"，而交进来的这一个要放进
		// 容器的槽位——持有者是那个位置，所以它从这本账里挑出去，中间那几步读走的照常归还。
		addOuter, addFrom := c.openWindow()
		v, vt, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		v, err = c.cellValue(v, vt, x.Args[0]) // 进容器的一份要独立，理由同list字面量
		if err != nil {
			return nil, t, err
		}
		c.closeWindow(addOuter, addFrom, v)
		c.f.Emit(&ir.Call{Fn: "@list_add", Args: []ir.Val{recv, c.bitsOf(v, vt)},
			FT: []ir.T{ir.TInt, ir.TInt}})
		return nil, t, nil
	case "list.sort":
		// 怎么比分三档：0 按那 64 位、1 按文本内容、2 按小数
		mode := int64(0)
		if len(rt.Args) > 0 {
			switch rt.Args[0].Kind {
			case check.Str:
				mode = 1
			case check.Float:
				mode = 2
			}
		}
		c.f.Emit(&ir.Call{Fn: "@list_sort", Args: []ir.Val{recv, ir.K(mode)},
			FT: []ir.T{ir.TInt, ir.TInt}})
		return nil, t, nil
	case "list.index_of":
		v, vt, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		same := int64(0)
		if len(rt.Args) > 0 && rt.Args[0].Kind == check.Str {
			same = 1 // 与 list.remove 同一条规则：文本按内容比，其余比那 64 位
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@list_index_of",
			Args: []ir.Val{recv, c.bitsOf(v, vt), ir.K(same)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "dict.keys", "dict.values":
		which := check.Type{}
		side := int64(0)
		if ref.Std == "dict.values" {
			side = 1
			if len(rt.Args) > 1 {
				which = rt.Args[1]
			}
		} else if len(rt.Args) > 0 {
			which = rt.Args[0]
		}
		way := int64(0)
		if which.Kind == check.Str {
			way = 1 // 文本要拷贝一份：两个位置指向同一块就会出现两个可析构的持有者
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@dict_entries",
			Args: []ir.Val{recv, ir.K(side), ir.K(way), ir.K(tagList)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "list.first", "list.last":
		// 头一个位置与尾一个位置走的就是 `a[i]` 那一条路：空表上 `last` 的下标是 -1，
		// 越界那一道检查顺带就报了它，不另设错误码。
		at := c.f.Temp()
		if ref.Std == "list.first" {
			c.f.Emit(&ir.Mov{D: at, S: ir.K(0), T: ir.TInt})
		} else {
			c.f.Emit(&ir.Load{D: at, Base: recv, Off: runtime.CellLen, T: ir.TInt})
			c.f.Emit(&ir.Bin{Op: "-", D: at, A: at, B: ir.K(1), T: ir.TInt})
		}
		cell, err := c.cellOf(recv, rt, at)
		if err != nil {
			return nil, t, err
		}
		return c.readCell(cell, rt, t)
	case "list.clear":
		// 析构每一个槽位的工作归降级侧：形状表在编译器手里，运行期那一侧只比较那 64 位。
		var elem check.Type
		if len(rt.Args) > 0 {
			elem = rt.Args[0]
		}
		if !reclaimable(elem) {
			// 值类型槽位没有下属：清零长度、递增版本计数即可，不需要逐位置的循环
			c.f.Emit(&ir.Store{Base: recv, Off: runtime.CellLen, S: ir.K(0), T: ir.TInt})
			ver := c.f.Temp()
			c.f.Emit(&ir.Load{D: ver, Base: recv, Off: runtime.CellVer, T: ir.TInt})
			c.f.Emit(&ir.Bin{Op: "+", D: ver, A: ver, B: ir.K(1), T: ir.TInt})
			c.f.Emit(&ir.Store{Base: recv, Off: runtime.CellVer, S: ver, T: ir.TInt})
			return nil, t, nil
		}
		loop, done := c.f.Fresh(), c.f.Fresh()
		n := c.f.Temp()
		c.f.Emit(&ir.Lbl{At: loop})
		c.f.Emit(&ir.Load{D: n, Base: recv, Off: runtime.CellLen, T: ir.TInt})
		c.f.Emit(&ir.Jz{V: n, To: done})
		v := c.f.Temp()
		c.f.Emit(&ir.Call{D: v, HasD: true, Fn: "@list_pop_last", Args: []ir.Val{recv},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		if err := c.releaseVal(v, elem); err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Jump{To: loop})
		c.f.Emit(&ir.Lbl{At: done})
		return nil, t, nil
	case "dict.get":
		// 键只读一眼（只读借用），默认值是交出去的：命中就当场析构它，没命中就归调用方。
		// 命中那一侧读的是 `d[键]` 同一条读法（读走），两种读法不许多于一份。
		// 这一条整个不开中转登记：返回的那一个是"键命中则是读走的那一块、未命中则是默认的这一块"，
		// 两条路各自的槽位都不是结果那一个槽，窗口无法判断该保留哪一笔。
		defer c.noBorrow()()
		kv, kt, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(kv)
		key := c.bitsOf(kv, kt)
		dv, dt, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		hit := c.f.Temp()
		c.f.Emit(&ir.Call{D: hit, HasD: true, Fn: "@map_contains", Args: []ir.Val{recv, key},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		absent, done := c.f.Fresh(), c.f.Fresh()
		d := c.f.Temp()
		c.f.Emit(&ir.Jz{V: hit, To: absent})
		cell, err := c.cellOf(recv, rt, key)
		if err != nil {
			return nil, t, err
		}
		got, _, err := c.readCell(cell, rt, t)
		if err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Mov{D: d, S: got, T: c.rt(t)})
		if err := c.releaseIfAny(dv, dt); err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Jump{To: done})
		c.f.Emit(&ir.Lbl{At: absent})
		c.f.Emit(&ir.Mov{D: d, S: dv, T: c.rt(t)})
		c.f.Emit(&ir.Lbl{At: done})
		return d, t, nil
	case "list.reverse":
		c.f.Emit(&ir.Call{Fn: "@list_reverse", Args: []ir.Val{recv}, FT: []ir.T{ir.TInt}})
		return nil, t, nil
	case "list.add_all":
		other, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(other)
		way := int64(0)
		if len(rt.Args) > 0 && rt.Args[0].Kind == check.Str {
			way = 1 // 文本的每个位置都拷一份进来，理由与 dict.keys 那条相同
		}
		c.f.Emit(&ir.Call{Fn: "@list_add_all", Args: []ir.Val{recv, other, ir.K(way)},
			FT: []ir.T{ir.TInt, ir.TInt, ir.TInt}})
		return nil, t, nil
	case "list.to_set":
		way, d := int64(0), c.f.Temp()
		if len(rt.Args) > 0 && rt.Args[0].Kind == check.Str {
			way = 1
		}
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@list_to_set",
			Args: []ir.Val{recv, ir.K(way), ir.K(c.keyMode(rt)), ir.K(tagSet)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "list.slice":
		lo, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		hi, _, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		way, d := int64(0), c.f.Temp()
		if len(rt.Args) > 0 && rt.Args[0].Kind == check.Str {
			way = 1
		}
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@list_slice",
			Args: []ir.Val{recv, lo, hi, ir.K(way), ir.K(tagList)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "bytes.slice":
		// `bytes` 的切片：含起不含止，与 list／string 的 slice 同一条边界（越界 10031 号）。
		// 返回的是新造的一块，所以不必带类型字：这一档的形状只有一种。
		lo, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		hi, _, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@bytes_slice",
			Args: []ir.Val{recv, lo, hi}, FT: []ir.T{ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "bytes.to_base64":
		// 源是字节块，写的出去的是文本：那一条例程自己按 `bytes` 的偏移读长度与正文（base64.go）。
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@bytes_b64_encode", Args: []ir.Val{recv},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "listener.accept":
		// 没有线程，一次只服务一个连接：这一条就是"等下一个连接进来"。
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@net_accept", Args: []ir.Val{recv},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "socket.send", "socket.recv_into":
		fn := "@net_send"
		if ref.Std == "socket.recv_into" {
			fn = "@net_recv_into"
		}
		blk, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(blk)
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: []ir.Val{recv, blk},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "socket.close", "listener.close", "process.close":
		// 提前关闭那一条：先调用它自身那一条收尾例程，再把这个位置抹成 0——那个名字当场作废，
		// 走到作用域尽头时看到的是 0，不做处理。
		// 落在字段上那一种（`h.sock.close()`）抹不到字段本身：句柄不是堆上的一块，
		// 多关一次系统只返回"这不是套接字"的错误，不会破坏其他对象；本版按这一行为处理。
		c.f.Emit(&ir.Call{Fn: closerOf(rt), Args: []ir.Val{recv}, FT: []ir.T{ir.TInt}})
		c.f.Emit(&ir.Mov{D: recv, S: ir.K(0), T: ir.TInt})
		return nil, t, nil
	case "process.wait":
		// 一直等到那个程序运行结束，返回它的退出码。等待用的是进程侧的那一枚句柄，
		// 名字里存的那一个数是运行期那一张表的表项——两枚句柄装不进一个名字，见 proc.go 文件头。
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@proc_wait", Args: []ir.Val{recv},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "list.insert_at":
		at, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		// 与 `list.add` 同一条：交进来的那一个要放进那个位置，中间那几步读走的照常归还。
		insOuter, insFrom := c.openWindow()
		v, vt, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		v, err = c.cellValue(v, vt, x.Args[1]) // 与 list.add 同一条：进容器的那一份要独立
		if err != nil {
			return nil, t, err
		}
		c.closeWindow(insOuter, insFrom, v)
		c.f.Emit(&ir.Call{Fn: "@list_insert",
			Args: []ir.Val{recv, at, c.bitsOf(v, vt)}, FT: []ir.T{ir.TInt, ir.TInt, ir.TInt}})
		return nil, t, nil
	case "list.remove_at":
		// 先把那个位置读走（与 a[i]、first、last 同一条读法），再把后面每一个位置前移、计数减一。
		// 读的时候那个位置已经写空，前移的一趟正好把它覆盖，不会留下无人认领的位置。
		at, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		cell, err := c.cellOf(recv, rt, at)
		if err != nil {
			return nil, t, err
		}
		got, _, err := c.readCell(cell, rt, t)
		if err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Call{Fn: "@list_shift_out", Args: []ir.Val{recv, at},
			FT: []ir.T{ir.TInt, ir.TInt}})
		return got, t, nil
	case "hashset.union", "hashset.intersect", "hashset.difference":
		other, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(other)
		// 0 并、1 交、2 差；文本元素要拷一份进新表，理由与 dict.keys 同一条
		way := int64(0)
		if len(rt.Args) > 0 && rt.Args[0].Kind == check.Str {
			way = 1
		}
		which := int64(0)
		switch ref.Std {
		case "hashset.intersect":
			which = 1
		case "hashset.difference":
			which = 2
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@set_op",
			Args: []ir.Val{recv, other, ir.K(which), ir.K(way), ir.K(tagSet)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "list.remove":
		v, vt, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		// 怎么算相等看元素类型：文本按内容比，别的就比那 64 位
		mode := int64(0)
		if len(rt.Args) > 0 && rt.Args[0].Kind == check.Str {
			mode = 1
		}
		gone := c.f.Temp()
		c.f.Emit(&ir.Call{D: gone, HasD: true, Fn: "@list_remove",
			Args: []ir.Val{recv, c.bitsOf(v, vt), ir.K(mode)},
			FT:   []ir.T{ir.TInt, ir.TInt, ir.TInt}, RT: ir.TInt})
		// 例程把被删掉的那个句柄返回来：那个位置已被后面的位置覆盖，语言里再没有名字指着它，
		// 按元素的形状当场析构（与覆盖一个位置、`add` 命中已有键同一本账）。没删成返回 0，不做处理。
		if len(rt.Args) > 0 {
			if err := c.releaseIfAny(gone, rt.Args[0]); err != nil {
				return nil, t, err
			}
		}
		return nil, t, nil
	}
	_ = rt
	return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
}

// mapAction 落挂在 dict／hashset 上的 add／remove／contains。三种容器共用一张表，
// 所以三个组名发的例程也一样——差别只在 hashset 的值槽位写 0、contains 要读取返回值。
// mapStore 落 dict 的 `d[k] = v` 与 `d.add(k, v)`／`s.add(k)`：
// 命中就是替换那个位置——被换下的旧值要析构，**没被收下的那一个键也要析构**（键已在表里，交进来的
// 那一块从此无人认领）；没命中才走 @map_put 插入，那时键与值都归那个位置。
//
// 先问一次在不在的原因是：旧值存在表里，降级这一侧拿不到它；@map_put 里只有"键怎么比"那个位置
// 信息（cMode），没有值的形状。要在运行期析构句柄类型的值，得先把形状表交进容器对象——那是另一步。
//
// en: mapAction lowers add/remove/contains on dict and hashset. The three containers share one
// table, so the routines emitted under the three group names are the same too; the only
// differences are that a hashset writes 0 in the value position and contains must read the
// reply. mapStore lowers dict's `d[k] = v` and `d.add(k, v)` / `s.add(k)`:
// a hit replaces that position - the value being replaced is
// dismantled, and THE KEY THAT WAS NOT ACCEPTED IS DISMANTLED TOO (the key is already in the
// table, so the block handed in is now claimed by nobody); only a miss goes to @map_put to
// insert, where both key and value become the position's.
func (c *fc) mapStore(recv, key, val ir.Val, hasVal bool, vt check.Type, keySrc ast.Expr, kt check.Type) error {
	hit := c.f.Temp()
	c.f.Emit(&ir.Call{D: hit, HasD: true, Fn: "@map_contains", Args: []ir.Val{recv, key},
		FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	miss, done := c.f.Fresh(), c.f.Fresh()
	c.f.Emit(&ir.Jz{V: hit, To: miss})
	if hasVal {
		vp := c.f.Temp()
		c.f.Emit(&ir.Call{D: vp, HasD: true, Fn: "@map_at", Args: []ir.Val{recv, key},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		old := c.f.Temp()
		c.f.Emit(&ir.Load{D: old, Base: vp, Off: 0, T: ir.TInt})
		if err := c.releaseIfAny(old, vt); err != nil {
			return err
		}
		c.f.Emit(&ir.Store{Base: vp, Off: 0, S: val, T: c.rt(vt)})
	}
	if c.argOwnedByCaller(keySrc, kt) {
		if err := c.releaseIfAny(key, kt); err != nil {
			return err
		}
	}
	c.f.Emit(&ir.Jump{To: done})
	c.f.Emit(&ir.Lbl{At: miss})
	v := val
	if !hasVal {
		v = ir.K(0) // 集合的成员只看键：值那个位置空着
	}
	c.f.Emit(&ir.Call{Fn: "@map_put", Args: []ir.Val{recv, key, v},
		FT: []ir.T{ir.TInt, ir.TInt, ir.TInt}})
	c.f.Emit(&ir.Lbl{At: done})
	return nil
}

func (c *fc) mapAction(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	m := x.Callee.(*ast.Member)
	recv, _, err := c.expr(m.What)
	if err != nil {
		return nil, t, err
	}
	c.guardNotNull(recv)
	// 键与值都算这一句里的几步。交进来的那一些归桶里那个位置（`@map_put` 收下键与值，
	// 都不另拷），所以从这本账里挑出去；只被读取一次的（remove／contains 的键、add 命中时
	// 没收下的那个键）用完即归还。
	mapOuter, mapFrom := c.openWindow()
	k, kt, err := c.expr(x.Args[0])
	if err != nil {
		return nil, t, err
	}
	key := c.bitsOf(k, kt)
	switch ref.Std {
	case "dict.add":
		v, vt, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		v, err = c.cellValue(v, vt, x.Args[1]) // 值那个位置要独立一份，句柄模型下的值类型不能共享
		if err != nil {
			return nil, t, err
		}
		c.closeWindow(mapOuter, mapFrom, k, v)
		if err := c.mapStore(recv, key, c.bitsOf(v, vt), true, vt, x.Args[0], kt); err != nil {
			return nil, t, err
		}
		return nil, t, nil
	case "hashset.add":
		// 值那个位置空着：成员在不在只看键
		c.closeWindow(mapOuter, mapFrom, k)
		if err := c.mapStore(recv, key, ir.K(0), false, check.Type{}, x.Args[0], kt); err != nil {
			return nil, t, err
		}
		return nil, t, nil
	case "dict.remove", "hashset.remove":
		// 删成的那一条：值槽位里那一块从此没有其他持有者，@map_remove 把它返回来，按值的形状析构。
		// 集合没有值槽位（`s.add(x)` 往那个位置写的是 0），值类型的槽位没有下属——两种都不必发这一趟。
		val := check.Type{}
		if bt, ok := c.l.f.Types[m.What]; ok && len(bt.Args) > 1 {
			val = bt.Args[1]
		}
		if !reclaimable(val) {
			c.f.Emit(&ir.Call{Fn: "@map_remove", Args: []ir.Val{recv, key},
				FT: []ir.T{ir.TInt, ir.TInt}})
			c.closeWindow(mapOuter, mapFrom) // 键只是拿来定位桶：例程走完它就不再被使用
			return nil, t, nil
		}
		got := c.f.Temp()
		c.f.Emit(&ir.Call{D: got, HasD: true, Fn: "@map_remove", Args: []ir.Val{recv, key},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		c.closeWindow(mapOuter, mapFrom)
		if err := c.releaseIfAny(got, val); err != nil {
			return nil, t, err
		}
		return nil, t, nil
	case "dict.contains", "hashset.contains":
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@map_contains", Args: []ir.Val{recv, key},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		c.closeWindow(mapOuter, mapFrom) // 同 remove：键比较完毕后就再无其他持有者
		return d, t, nil
	}
	return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
}

// bitsOf 把一个小数的位型当 8 字节整数交出去：要进容器槽位、要交给只识别通用寄存器的例程，
// 都走这一步。槽里存放的就是那 64 位，搬运方式与整数完全相同。
//
// en: bitsOf hands a float's bit pattern out as an 8-byte integer: needed when it enters a
// container position or a routine that only understands general-purpose registers. Those 64
// bits are exactly what lies in the slot, and it moves like an integer.
func (c *fc) bitsOf(v ir.Val, t check.Type) ir.Val {
	if c.rt(t) != ir.TFlt {
		return v
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Mov{D: d, S: v, T: ir.TFlt})
	return d
}

// forString 按码点遍历文本（`for (c:unicode in s)` 逐个交出码点）。
//
// 游标是**字节偏移**：每圈先比较偏移与字节数，未走完就解出当前位置、再跳到下一个位置。
// 文本不可变，所以没有"遍历途中改容器"这条规则要检查——版本号机制只覆盖三种容器。
//
// en: forString iterates text by code point (`for (c:unicode in s)` yields code points one at a
// time). The cursor is a BYTE OFFSET: each round compares the offset with the byte count first,
// then decodes this position and jumps to the next when there is more to read. Text is
// immutable, so there is no "the container changed during iteration" rule to enforce - the
// version counter only covers the three containers.
func (c *fc) forString(x *ast.For, iter ir.Val) error {
	c.open()
	blen := c.f.Temp()
	c.f.Emit(&ir.Load{D: blen, Base: iter, Off: 0, T: ir.TInt})
	off := c.f.Temp()
	c.f.Emit(&ir.Mov{D: off, S: ir.K(0), T: ir.TInt})

	top := c.f.Fresh()
	next := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Lbl{At: top})
	c.f.Emit(&ir.Br{Op: ">=", A: off, B: blen, To: end, T: ir.TInt})

	cp := c.f.Temp()
	c.f.Emit(&ir.Call{D: cp, HasD: true, Fn: "@str_dec",
		Args: []ir.Val{iter, off}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	c.bind(x.Var.NameText, cp, dbgOf(check.Type{Kind: check.Unicode, Name: "unicode"}))

	c.brk = append(c.brk, exitSite{to: end, mark: len(c.marks) - 1})
	c.cont = append(c.cont, exitSite{to: next, mark: len(c.marks) - 1})
	for _, s := range x.Body {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.brk = c.brk[:len(c.brk)-1]
	c.cont = c.cont[:len(c.cont)-1]

	// `next` 这个标签要发在析构**之前**：`continue` 跳的就是它，跳过来之后照样走完这一圈的
	// 块尾，圈里登记的局部量一圈都不留。反过来摆在析构之前，就成了每圈漏一整块——
	// 这条由 heap_e2e_test.go 那组按每圈增量测得（continue 那一圈实测 88 字节）。
	// `break` 跳的是 `end`，它跳过析构：那一圈的块留到进程结束，属泄漏而非错误。
	c.f.Emit(&ir.Lbl{At: next})
	c.close()
	c.f.Emit(&ir.Call{D: off, HasD: true, Fn: "@str_adj",
		Args: []ir.Val{iter, off}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	c.f.Emit(&ir.Jump{To: top})
	c.f.Emit(&ir.Lbl{At: end})
	return nil
}

// forList 是 `for (v:T in list)`：交出的是这个位置的**只读借用**，不动那个位置，
// 所以遍历完容器还是满的。中途添删要抛 10033 号——
// 版本号在进圈那一刻抄一份，每圈圈头比一次，比不上一律先抛再看边界。
//
// en: forList is `for (v:T in list)`: what is yielded is a READ-ONLY BORROW of that position,
// the position is untouched, so after the loop the container is still full. Adding or
// removing mid-loop throws code 10033 - the version number is copied when entering the
// loop and compared at the top of every round.
func (c *fc) forList(x *ast.For, iter ir.Val, it check.Type) error {
	c.open()
	n := c.f.Temp()
	c.f.Emit(&ir.Load{D: n, Base: iter, Off: runtime.CellLen, T: ir.TInt})
	ver := c.f.Temp()
	c.f.Emit(&ir.Load{D: ver, Base: iter, Off: runtime.CellVer, T: ir.TInt})
	i := c.f.Temp()
	c.f.Emit(&ir.Mov{D: i, S: ir.K(0), T: ir.TInt})

	top := c.f.Fresh()
	next := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Lbl{At: top})
	cur := c.f.Temp()
	c.f.Emit(&ir.Load{D: cur, Base: iter, Off: runtime.CellVer, T: ir.TInt})
	c.throwIf("!=", cur, ver, codeMutate)
	c.f.Emit(&ir.Br{Op: ">=", A: i, B: n, To: end, T: ir.TInt})

	cell, err := c.cellOf(iter, it, i)
	if err != nil {
		return err
	}
	v := c.f.Temp()
	elem := check.Type{}
	if len(it.Args) > 0 {
		elem = it.Args[0]
	}
	c.f.Emit(&ir.Load{D: v, Base: cell, Off: 0, T: c.rt(elem)})
	c.bind(x.Var.NameText, v, dbgOf(elem))

	c.brk = append(c.brk, exitSite{to: end, mark: len(c.marks) - 1})
	c.cont = append(c.cont, exitSite{to: next, mark: len(c.marks) - 1})
	for _, s := range x.Body {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.brk = c.brk[:len(c.brk)-1]
	c.cont = c.cont[:len(c.cont)-1]
	c.f.Emit(&ir.Lbl{At: next})
	c.close() // 同 forString：`next` 先发，continue 跳过来也走完这一圈的块尾

	c.f.Emit(&ir.Bin{Op: "+", D: i, A: i, B: ir.K(1), T: ir.TInt})
	c.f.Emit(&ir.Jump{To: top})
	c.f.Emit(&ir.Lbl{At: end})
	return nil
}

// forMap 是 `for (k:T in 表)`：dict 交出的是**键**，hashset 交出的是成员。
// 走法是扫描一遍桶数组，跳过空槽与墓碑槽；顺序不保证。
// 途中添删同样抛 10033 号——版本号在进圈那一刻抄一份，每圈圈头比一次。
//
// en: forMap is `for (k:T in table)`: a dict yields the KEY, a hashset yields its
// member. It scans the bucket array and skips empty and tombstone slots; order is not
// guaranteed. Mid-loop modification again throws code 10033, comparing the copied
// version number at the top of every round.
func (c *fc) forMap(x *ast.For, iter ir.Val, it check.Type) error {
	elem := check.Type{}
	if len(it.Args) > 0 {
		elem = it.Args[0]
	}
	c.open()
	ver := c.f.Temp()
	c.f.Emit(&ir.Load{D: ver, Base: iter, Off: runtime.CellVer, T: ir.TInt})
	cur := c.f.Temp()
	c.f.Emit(&ir.Load{D: cur, Base: iter, Off: runtime.CellData, T: ir.TInt})

	top := c.f.Fresh()
	next := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Lbl{At: top})
	cVer := c.f.Temp()
	c.f.Emit(&ir.Load{D: cVer, Base: iter, Off: runtime.CellVer, T: ir.TInt})
	c.throwIf("!=", cVer, ver, codeMutate)

	bk := c.f.Temp()
	c.f.Emit(&ir.Call{D: bk, HasD: true, Fn: "@map_next",
		Args: []ir.Val{iter, cur}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	c.f.Emit(&ir.Jz{V: bk, To: end})
	// 下一轮从这一桶往后一个位置开始走。先算好这一步，body 里的 continue 落点才正确。
	c.f.Emit(&ir.Bin{Op: "+", D: cur, A: bk, B: ir.K(runtime.BucketPad), T: ir.TInt})

	v := c.f.Temp()
	c.f.Emit(&ir.Load{D: v, Base: bk, Off: runtime.BucketKey, T: c.rt(elem)})
	c.bind(x.Var.NameText, v, dbgOf(elem))

	c.brk = append(c.brk, exitSite{to: end, mark: len(c.marks) - 1})
	c.cont = append(c.cont, exitSite{to: next, mark: len(c.marks) - 1})
	for _, s := range x.Body {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.brk = c.brk[:len(c.brk)-1]
	c.cont = c.cont[:len(c.cont)-1]
	c.f.Emit(&ir.Lbl{At: next})
	c.close() // 同 forString：`next` 先发，continue 跳过来也走完这一圈的块尾

	c.f.Emit(&ir.Jump{To: top})
	c.f.Emit(&ir.Lbl{At: end})
	return nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// execOrder 返回文件该跑的先后：被 import 的先跑，入口最后。
// 后序 DFS 正好是这个顺序，而互相引用在检查期就被拒绝，图一定是无环的。
//
// en: execOrder returns the order files should run in: imported files first, the entry last.
// A post-order DFS is exactly that order, and mutual import is already rejected during
// checking, so the graph is necessarily acyclic.
func (l *L) execOrder() ([]*ast.File, error) {
	byPath := map[string]*ast.File{}
	for _, f := range l.files {
		byPath[f.Path] = f
	}
	var out []*ast.File
	state := map[string]int{} // 0 未访问 1 在路径上 2 已完成
	var walk func(path string) error
	walk = func(path string) error {
		f, ok := byPath[path]
		if !ok {
			return fmt.Errorf("后端认不出文件 %s", path)
		}
		switch state[path] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("%s 的引用绕成了环，检查器本该拦住它", path)
		}
		state[path] = 1
		for _, imp := range f.Imports {
			if imp.Package != "" {
				// 整包挂名那一种：跑在这一份文件前面的，是那个包的门面文件。
				// 门面路径由装载侧记录（`Program.PkgFacades`）——装载的就是那一份；
				// 按导入写法拼出的名字在大小写不敏感的系统上可能指向不存在的文件。
				facade, ok := l.pkgFacades[check.PkgRefKey(path, imp.Path)]
				if !ok {
					return fmt.Errorf("后端认不出包 %s", imp.Path)
				}
				if err := walk(facade); err != nil {
					return err
				}
				continue
			}
			if err := walk(check.ResolveImportPath(path, imp.Path)); err != nil {
				return err
			}
		}
		state[path] = 2
		out = append(out, f)
		return nil
	}
	if err := walk(l.entry); err != nil {
		return nil, err
	}
	return out, nil
}

// fileGlobals 返回这个文件顶层那些名字各自的静态槽。
//
// 顶层就是一层作用域，所以函数体里读到的顶层名字要从这儿查。
// 跨文件带进来的 const／shared 还没排静态槽，遇到时会在解析那一步当场报出。
//
// en: fileGlobals returns the static slot of each name at this file's top level. Top level is
// one scope layer, so a top-level name read from inside a function body is looked up
// here. A const/shared brought in across files that has not been allocated a static slot is
// named on the spot at the resolution step.
func (l *L) fileGlobals(file *ast.File) map[string]ir.G {
	g := map[string]ir.G{}
	for _, s := range file.Stmts {
		if d, ok := s.(*ast.Decl); ok {
			g[d.NameText] = l.glob[d]
		}
	}
	return g
}

// function 落一个用户函数或一个方法。方法的名字是 "类型名.方法名"，接收者占第 0 个槽：
// 它在 rcx 里进来，跟参数走同一套摆法，所以把它也算进 Params。
//
// en: function lowers one user function or one method. A method's name is "type.method" and the
// receiver occupies slot 0: it arrives in rcx and is arranged exactly like a parameter,
// so it is counted into Params as well.
func (l *L) function(file *ast.File, owner string, fn *ast.Func, globals map[string]ir.G) error {
	name := l.symKey(file, fn.NameText)
	if fn.IsMethod {
		// owner 已经是该类型的规范名（包内的带包根）
		name = owner + "." + fn.NameText
	}
	f := l.p.Func(name)
	// 记下这一份函数写在哪个文件、那一份文件的顶层表：调用点补默认值时要用。
	l.funcFile[name] = file.Path
	l.funcGlobals[name] = globals
	c := &fc{l: l, f: f, name: name, owner: owner, glob: globals, filePath: file.Path}
	c.open()
	if fn.IsMethod {
		// this 就是那一份句柄；方法体里直接写字段名与写 this.字段名等价
		rv := f.Temp()
		c.bind("this", rv, dbgOf(check.Type{Kind: c.l.objKind[owner], Name: owner}))
		f.Params++
		f.PT = append(f.PT, ir.TInt)
	}
	for _, p := range fn.Params {
		v := f.Temp()
		c.bind(p.NameText, v, dbgOfAst(p.Typ))
		f.Params++
		f.PT = append(f.PT, c.rtOf(p.Typ))
		// 参数有没有默认值在被调方一侧不产生影响：缺失的那个实参由调用点补上（见 argsOf）
	}
	for _, s := range fn.Body {
		if err := c.stmt(s); err != nil {
			return fmt.Errorf("%s：%w", check.BareName(name), err)
		}
	}
	c.endFrame()
	return nil
}

// ---- 表达式与语句 ----

type fc struct {
	l      *L
	f      *ir.Func
	name   string
	owner  string // 方法所属的类名；普通函数是空
	scopes []map[string]ir.Val
	glob   map[string]ir.G // 本文件顶层那些名字的静态槽
	// filePath 是正在降的这一份文件：跨文件带进来的顶层 const／shared 按（这份文件, 本地名字）
	// 记，两份文件各自引来的同名东西不会互相顶掉。
	filePath string
	brk      []exitSite
	cont     []exitSite
	// tries 记当前打开着的每一层 try 的体：抛出去要释放到哪一层为止（leaveToHandler）。
	tries []int
	// owns 是本帧负责归还的那些位置，按声明顺序排；marks 记每一层作用域从哪个下标开始。
	// 语言规则"局部值随作用域结束自动析构，顺序为声明的反序"，落到机器上就是 close 里倒序释放这一段。
	owns  []own
	marks []int
	// held 是本帧登记过的**全部**（close 把 owns 截回外层，这一份不截）：帧尾那段清理代码按它
	// 归还，try 的接住块也按它记自己那一段的下标。两处要的都是"这一帧登记的全部"，
	// 而不是"当前还开着的那一层登记的"。
	held []own
	// borrow 大于 0 表示"此刻正在往一个只读借用的标准库动作交参数"（标准库不收留值）。
	// 这时候从容器里**读走**的那一个句柄落在一个没有名字的临时槽上：除这个槽位外无人持有它，
	// 所以登记进 temps，等这次调用的参数都交完就当场归还。
	// 会收留值的那几条（容器 add、写字面量的槽位、进字段、给用户的参数）一律把 borrow 清零，
	// 因为它们那一侧已经有持有者在等着了。
	borrow int
	temps  []own
}

// own 是一笔登记：哪个位置、按哪条形状归还、这一笔登记的名字。
// 名字供退出清理代码使用：`return h.peer` 交出去的句柄存放在 h 那一块里，本次归还路径不许还 h。
//
// direct 说这个位置是"照 `@free` 归还的一块"而不是"照形状表析构的一块"——文本就是这一类：
// 串内部没有别的持有者，但从容器位置与字段里**读出来**的那一个句柄，那个位置仍是持有者
// （读不算交出），所以 `nef.destroy` 那一路不得递归进入串内部（见 destroy.go 文件头那条）。
//
// en: own is one registration: which position, by which shape it is returned, and what this
// entry is called. The name is for the exit-cleanup code: a handle handed over by `return
// h.peer` lives inside h's own block, so this h must not return it. direct says the position is
// "a block returned by @free" rather than "a block dismantled per the shape table" - text is
// exactly that kind: the string holds nothing itself, but a handle READ OUT of a container
// position or a field leaves that position as the holder (a read is not a handover), so
// the nef.destroy path must not descend into a string (see the rule at the top of destroy.go).
type own struct {
	slot   ir.Val
	desc   ir.D
	name   string
	direct bool
	// closer 对应"这一块不在堆上，收尾要调用一次 closesocket"那一种：操作系统给的句柄。
	// 该字段存收尾例程的名字：套接字与监听是 @net_close，子进程这一类是 @proc_close。
	// 不是句柄时该字段为空字符串。
	closer string
}

// exitSite 是一条早退路的落点：跳去哪一个标签，以及离开时要把登记还到哪一层为止
// （那个数是 fc.marks 里的下标，跳出循环要把循环体那一层的登记先还掉）。
//
// en: exitSite is the destination of one early exit: which label to jump to and, on leaving,
// down to which registration layer to return (the number is an index into fc.marks - leaving a
// loop must first return the loop body's own layer).
type exitSite struct {
	to   ir.Label
	mark int
}

// unwind 发一段**退出清理代码**：把这一帧上从 mi 那一层登记进来的条目按声明反序归还，记录本身不动。
//
// 记录不动的原因是：正常走完那条路的块尾 `close()` 还要照原样再发一遍——两条路各走一次，
// 而每一笔归还完都把那个位置抹成 0，走另一条路时看到的是空句柄（nef.destroy 那一路不做处理）。
//
// en: unwind emits EXIT CLEANUP CODE: it returns, in reverse declaration order, every entry
// registered from layer mi on this frame, leaving the record itself alone. Why the ledger is
// not touched: the normal path's block tail close() will emit the same thing again - the two
// paths each run once, and every return zeroes that position, so whoever takes the other path
// sees an empty handle and nef.destroy does nothing.
func (c *fc) unwind(mi int, skip map[string]bool) {
	lo := c.marks[mi]
	for i := len(c.owns) - 1; i >= lo; i-- {
		if skip[c.owns[i].name] {
			continue
		}
		c.releaseOne(c.owns[i])
	}
}

// leaveFrame 是"这一帧到此为止"那一条早退路（return、exit、没人接的 throw）：
// 从最里层一路还到帧底。
//
// en: leaveFrame is the "this frame ends here" early exit (return, exit, an exception nobody
// catches): returning from the innermost layer all the way down to the frame base.
func (c *fc) leaveFrame(skip map[string]bool) { c.unwind(0, skip) }

// leaveToHandler 是 `throw` 那一条：抛出去释放到的只到"接住它的那一个 try 的体"为止。
// 本帧里没有包住它的 try，那就是整帧都要释放；有的话，try 体外面的那些名字接住块接着还要读，
// 不许归还（规则说的"本层"指的是抛出所在的这一层，接住块执行时外层仍然存活，那一层就没有结束）。
//
// en: leaveToHandler is the `throw` path: what a throw discards goes only as far as "the body
// of the try that will catch it". If this frame holds no enclosing try, the whole frame must be
// discarded; if it does, the names declared outside the try body are still read by the catch
// block and must not be returned (the rule says "this layer" - while the catch block runs the
// outer layer is still alive, and that layer has not ended).
func (c *fc) leaveToHandler() {
	if len(c.tries) > 0 {
		c.unwind(c.tries[len(c.tries)-1], nil)
		return
	}
	c.leaveFrame(nil)
}

// endFrame 收一帧：正常那一路的块尾（close）发完、Ret 发完，再给两条早退路收尾。
//
// 这一帧登记过东西的话（跨帧的那一半）：
//
//   - 开场先把每一个槽抹成 0，再挂一条帧内清理记录。抹零是给标记读的：清理段释放的是"本帧登记过的
//     全部"，而没走到的那些分支里，槽里是上一次调用留下的残留数据——不清零就是拿无效值当句柄去归还。
//   - 每一处 Ret 之前摘掉自己那条标记。正常走出去的帧不许在链上留一条指向死帧的记录，
//     否则下一次抛出会跳进已经作废的帧里（接住块的处理点也出现过同一问题）。
//   - 帧的最后（Ret 之后，正常路径不可达）放一段垫子：把本帧登记过的按反序归还完，再调用 @throw_walk 继续向外传播。
//
// 没登记过东西的帧三条都不发：链上不多一个节点、开场不多两个槽，不碰堆的程序一次调用也不多申请一块。
//
// en: endFrame closes a frame: after the normal path's block tail (close) and the Ret
// instruction, the two early-exit paths get their cleanup too. If this frame registered
// anything (the cross-frame half of the rule): the prologue first zeroes every slot and then
// attaches a frame-scope record. The zeroing exists for the marker to be readable - the cleanup
// section releases "everything this frame registered", and in branches that never ran, those
// positions hold garbage left by the previous call; without zeroing, garbage would be handed to
// @free as a handle.
func (c *fc) endFrame() {
	c.close()
	c.f.Emit(&ir.Ret{})
	if len(c.held) == 0 {
		return
	}
	pad := c.f.Fresh()
	old := c.f.Body
	body := make([]ir.Inst, 0, len(old)+2*len(c.held)+4)
	for _, o := range c.held {
		body = append(body, &ir.Mov{D: o.slot, S: ir.K(0), T: ir.TInt})
	}
	// 那条记录住在本帧：五个槽位连续排列（落点、rbp、rsp、上一条、种类），运行期从最低地址的槽位
	// 向上走五个，所以交出去的是这一段里编号最大的那个位置（交出第一个位置的话 +32 会越过本段边界）。
	rec := c.f.Reserve(recordSlots)
	body = append(body, &ir.PushAbandon{Pad: pad, Rec: rec + ir.L(recordSlots-1)})
	for _, in := range old {
		if _, stop := in.(*ir.Ret); stop {
			body = append(body, &ir.PopHandler{})
		}
		body = append(body, in)
	}
	c.f.Body = body
	c.f.Emit(&ir.Lbl{At: pad})
	c.destroyAll(c.held)
	c.f.Emit(&ir.Call{Fn: "@throw_walk", FT: []ir.T{}})
}

func (c *fc) open() {
	c.scopes = append(c.scopes, map[string]ir.Val{})
	c.marks = append(c.marks, len(c.owns))
}

// bind 登记一个帧上的名字：往后写它读它都走这个位置（scopes），调试器停下来读局部量也看得见它
// （Func.Locals）。顶层那些名字落在静态槽上、不在任何一帧里，因此不进这张表。
//
// en: bind registers a name on the frame: every later read and write of it goes through this
// position (scopes), and a debugger stopped at a breakpoint sees it too (Func.Locals).
// Top-level names live in static slots and in no frame, so they are not in this table.
func (c *fc) bind(name string, v ir.Val, s dbgShape) {
	c.scopes[len(c.scopes)-1][name] = v
	l, ok := v.(ir.L)
	if !ok {
		return
	}
	c.f.Locals = append(c.f.Locals, ir.Local{Slot: int(l), Name: name, Kind: s.kind, Type: s.text})
}

// dbgShape 是一个帧上局部量的形状：解码所需的类别，与面向读者的写法。
//
// en: dbgShape is the shape of a frame-local for decoding: the class the decoder needs plus the
// spelling a person reads.
type dbgShape struct{ kind, text string }

// dbgOf 把一个解好的类型压成解码要的那六类之一。struct 归 handle：语言说它是值类型，
// 机器上按引用摆、赋值走 @copy_block，那个位置里是个句柄。
//
// en: dbgOf compresses a decoded type into one of the six decoding classes. A struct counts as
// handle: the language calls it a value type, but on the machine it is arranged by reference,
// assignment goes through @copy_block, and what lies in that position is a handle.
func dbgOf(t check.Type) dbgShape {
	switch t.Kind {
	case check.Int, check.Enum:
		return dbgShape{"int", t.String()}
	case check.Float:
		return dbgShape{"float", t.String()}
	case check.Bool:
		return dbgShape{"bool", t.String()}
	case check.Byte:
		return dbgShape{"byte", t.String()}
	case check.Unicode:
		return dbgShape{"rune", t.String()}
	}
	return dbgShape{"handle", t.String()}
}

// dbgOfAst 只能看到写出来的类型标注，用在参数位置——检查器把解好的类型留在自己那一侧，
// 没有交给后端。五种内建值类型能识别出来，其余（具名 struct／class／容器）按 handle 记；
// 类型别名（`type 甲 = int`）与枚举名识别不出来，也按 handle 记。
// 识别错误不影响正确性：调试器读那个位置时先验证句柄特征（是否落在堆范围内、块头是否合法），
// 不符合就按整数原样打印并附加一句说明，不会去析构一块无效内存。
//
// en: dbgOfAst has only the written annotation to look at, and is used for the parameter
// positions - the checker keeps resolved types on its own side and does not hand them to the
// backend. The five built-in value types are recognised; everything else (a named
// struct/class/container) is recorded as handle, and so are type aliases (`type A = int`)
// and enum names. A wrong guess is not fatal: when the debugger reads that position it first
// sanity-checks the handle (inside the heap range, header plausible), and if it fails it prints
// the raw integer with a note instead of dismantling fake memory.
func dbgOfAst(ty ast.Type) dbgShape {
	nt, ok := ty.(*ast.NamedType)
	if !ok {
		return dbgShape{"handle", "?"}
	}
	switch nt.Name {
	case "int":
		return dbgShape{"int", nt.Name}
	case "float":
		return dbgShape{"float", nt.Name}
	case "boolean":
		return dbgShape{"bool", nt.Name}
	case "byte":
		return dbgShape{"byte", nt.Name}
	case "unicode":
		return dbgShape{"rune", nt.Name}
	}
	return dbgShape{"handle", dbgTypeName(ty)}
}

// dbgTypeName 把写出来的类型标注还原成面向读者的写法（list<int>、dict<string, int>）。
//
// en: dbgTypeName restores a written type annotation into the spelling a person reads
// (list<int>, dict<string, int>).
func dbgTypeName(ty ast.Type) string {
	nt, ok := ty.(*ast.NamedType)
	if !ok {
		return "?"
	}
	if len(nt.Args) == 0 {
		return nt.Name
	}
	parts := make([]string, len(nt.Args))
	for i, a := range nt.Args {
		parts[i] = dbgTypeName(a)
	}
	return nt.Name + "<" + strings.Join(parts, ", ") + ">"
}

// recordGlobal 记下顶层那个名字落在第几个静态槽。帧上的名字由 bind 登进 Func.Locals，
// 顶层的不在任何一帧里，调试器列"静态"那一栏只能靠这张表（ir.Prog.Names）。
//
// en: recordGlobal notes which static slot a top-level name landed in. Names on a frame go into
// Func.Locals via bind, top level sits in no frame, and the debugger's "static" column can only
// come from this table (ir.Prog.Names).
func (c *fc) recordGlobal(slot ir.G, x *ast.Decl, t check.Type) {
	k := dbgOf(t)
	c.l.p.Names = append(c.l.p.Names, ir.GlobalName{
		Slot: int(slot), Name: x.NameText, Kind: k.kind, Type: k.text,
		File: c.filePath, Line: x.Where().Line,
	})
}

func (c *fc) close() {
	c.destroyAll(c.owns[c.marks[len(c.marks)-1]:])
	c.owns = c.owns[:c.marks[len(c.marks)-1]]
	c.marks = c.marks[:len(c.marks)-1]
	c.scopes = c.scopes[:len(c.scopes)-1]
}

// isHandle 说这一类值是操作系统给的那一类句柄（`Socket`、`Listener` 与 `Process`）：
// 它不是堆上的一块，所以收尾不走 `@free`、也不走 `nef.destroy`，
// 叫的是 `closerOf` 返回的那一条例程。
//
// en: isHandle says this class of value is an operating-system handle (`Socket`, `Listener`,
// `Process`): it is not a block on the heap,
// so cleanup goes neither through @free nor nef.destroy but through the routine named by
// closerOf.
func isHandle(t check.Type) bool {
	return t.Kind == check.Socket || t.Kind == check.Listener || t.Kind == check.Process
}

// closerOf 返回那一类句柄到作用域尽头要调用的例程名（不是句柄就返回空字符串）。
//
// 两套句柄的收尾动作不同：套接字与监听都是 `closesocket`，而子进程这一类要的是
// "不等候就结束该进程，再把两枚句柄都关闭"，由 @proc_close 完成。
//
// en: closerOf returns the routine to call for that class of handle when the scope ends (an
// empty string when it is not a handle). The two families end differently: sockets and
// listeners both take `closesocket`, while the child-process tier needs "kill it without
// waiting, then close both handles", which is @proc_close.
func closerOf(t check.Type) string {
	switch t.Kind {
	case check.Socket, check.Listener:
		return "@net_close"
	case check.Process:
		return "@proc_close"
	}
	return ""
}

// releaseOne 归还一笔登记：文本这一类直接走 @free（串内部没有别的东西要析构，形状表那一路也不得
// 递归进入串内部——见 destroy.go 文件头那条），句柄这一类调用一次 @net_close，其余都交给 nef.destroy 按形状表递归。
// 归还后一律抹成 0：同一个槽第二次被走到只能看见空句柄，而两条路径对空句柄都是"不做处理"。
//
// en: releaseOne returns one entry: the text class is returned straight through @free (a string
// holds nothing else, and the shape-table path must not descend into it - see the rule at the
// top of destroy.go), the handle class calls @net_close once, and everything else goes to
// nef.destroy to recurse by the shape table. After returning, the position is zeroed: a slot
// reached a second time can only show an empty handle, and an empty handle means "do nothing"
// on both sides.
func (c *fc) releaseOne(o own) {
	switch {
	case o.closer != "":
		c.f.Emit(&ir.Call{Fn: o.closer, Args: []ir.Val{o.slot}, FT: []ir.T{ir.TInt}})
	case o.direct:
		c.f.Emit(&ir.Call{Fn: "@free", Args: []ir.Val{o.slot}, FT: []ir.T{ir.TInt}})
	default:
		c.f.Emit(&ir.Call{Fn: destroyFn, Args: []ir.Val{o.slot, ir.D(o.desc)},
			FT: []ir.T{ir.TInt, ir.TInt}})
	}
	c.f.Emit(&ir.Mov{D: o.slot, S: ir.K(0), T: ir.TInt})
}

// destroyAll 按登记的**反序**还一批：后声明的先还。还完把那个位置抹成 0——同一个槽第二次被走到
// （循环、接住块之后又走完函数）就只能看见一个空句柄。
//
// en: destroyAll returns a batch in REVERSE registration order - the last declared is returned
// first - and zeroes each position afterwards, so a slot reached a second time (a loop, or the
// function completing after a catch block) can only be an empty handle.
func (c *fc) destroyAll(list []own) {
	for i := len(list) - 1; i >= 0; i-- {
		c.releaseOne(list[i])
	}
}

// releaseVal 析构"原来存放在某个槽位里"的那一块（覆盖一个位置、删一个成员都算）。
// 文本的槽位直接 @free（串没有下属），其余按形状表递归；值类型不会走到这里。
//
// en: releaseVal dismantles the block that used to live in one slot (overwriting a position or
// removing a member both count). A text position goes straight to @free (a string has no
// subordinates); everything else recurses by the shape table, and a value type never
// reaches here.
func (c *fc) releaseVal(v ir.Val, t check.Type) error {
	switch {
	case isHandle(t):
		// 句柄那一类不归形状表管：字段覆盖与被覆盖下来的那一个，叫一次它自己那一条收尾
		// （套接字与监听是 @net_close，子进程是 @proc_close）。
		c.f.Emit(&ir.Call{Fn: closerOf(t), Args: []ir.Val{v}, FT: []ir.T{ir.TInt}})
		return nil
	case t.Kind == check.Str:
		c.f.Emit(&ir.Call{Fn: "@free", Args: []ir.Val{v}, FT: []ir.T{ir.TInt}})
		return nil
	}
	d, err := c.l.descOf(t)
	if err != nil {
		return err
	}
	c.f.Emit(&ir.Call{Fn: destroyFn, Args: []ir.Val{v, ir.D(d)}, FT: []ir.T{ir.TInt, ir.TInt}})
	return nil
}

// releaseIfAny 发一段"那个位置有东西就析构"：空句柄（从未存入过、或者已被读走）不做处理。
//
// en: releaseIfAny emits "if that position holds something, dismantle it": an empty handle
// (never stored into, or already read out) does nothing.
func (c *fc) releaseIfAny(old ir.Val, t check.Type) error {
	if !reclaimable(t) {
		return nil
	}
	skip := c.f.Fresh()
	c.f.Emit(&ir.Jz{V: old, To: skip})
	if err := c.releaseVal(old, t); err != nil {
		return err
	}
	c.f.Emit(&ir.Lbl{At: skip})
	return nil
}

// freeFieldOld 是覆盖字段那一步的析构旧值，与容器槽位析构旧值同一本账。
//
// 容器槽位可以直接析构：读出来就是读走，那个位置之后返回空句柄，除了这一处没有别的持有者。
// 字段读出来不算交出，所以这里要多比较一次句柄——`b.inner = b.inner` 与被调方
// 顺着同一个位置写回自己这两种，相等的那个就是即将写入的那一块，析构它就是析构正要写入的东西。
// 比较句柄不必比较内容：语言里一个值同一时刻只有一个持有者，两个句柄相等就是同一块。
//
// en: freeFieldOld is the dismantle-the-old-value step when a field is overwritten, on the
// same ledger as a container position. A container
// position is dismantled without asking: reading it out is a read-out, the position then
// returns an empty handle, and nobody else holds it. Reading a field out is not a handover
// , so one extra handle comparison is needed here: in `b.inner = b.inner`, and when a
// callee writes the same position back into itself, an equal handle IS the block about to be
// stored - dismantling it would dismantle the thing being written. Comparing handles need not
// compare contents: a value has exactly one holder at any moment, so two equal handles are the
// same block.
func (c *fc) freeFieldOld(base ir.Val, off int64, v ir.Val, vt check.Type) error {
	if !reclaimable(vt) {
		return nil
	}
	old := c.f.Temp()
	c.f.Emit(&ir.Load{D: old, Base: base, Off: int(off), T: ir.TInt})
	same := c.f.Fresh()
	c.f.Emit(&ir.Br{Op: "==", A: old, B: v, To: same, T: ir.TInt})
	if err := c.releaseIfAny(old, vt); err != nil {
		return err
	}
	c.f.Emit(&ir.Lbl{At: same})
	return nil
}

// freeNameOld 是"覆盖一个名字就析构被换下来的那一份"（槽位就是它那一块唯一的持有者）。
//
// 容器的一个位置与 class 的字段早就按这一条析构了，名字这一侧此前遗漏：两万圈实测 `r = Row { ... }`
// 遗留 960,160 字节、`s = "甲" + "乙"` 遗留 640,136、`c = new C()` 遗留 320,152、`xs = [1, 2]`
// 遗留 1,760,232。补上这一条只增加两道判断：
//
//	① 只有登记在册的持有者才析构。遍历得到的那一个是只读借用，`shared` 那一侧不参与这本账，
//	   两处都不在册——对它们析构就是拆别人持有的块。
//	② 先比较一次句柄。`r = r` 这种自己覆盖自己的，要析构的正是即将写入的那一块，
//	   相等就不析构（字段那一侧同一道比较：比句柄不比内容，一个值同一时刻只有一个持有者）。
//
// en: freeNameOld is "overwriting a name dismantles the value it replaced" (a position is
// the sole holder of its block). Container positions and class fields already did this; names
// were the gap: measured over twenty thousand rounds, `r = Row { ... }` kept 960,160 bytes, `s
// = "a" + "b"` kept 640,136, `c = new C()` kept 320,152 and `xs = [1, 2]` kept 1,760,232. The
// fix adds only two tests: 1) only a holder registered in the ledger is dismantled - the one
// yielded by iteration is a read-only borrow and `shared` takes no part in this ledger,
// and neither is registered, so touching them would dismantle property held elsewhere;
// 2) compare handles first - in `r = r` the block being dismantled is the one about to move in,
// so equal means do not dismantle (the field side makes the same comparison: compare
// handles not contents, a value has one holder at a time).
func (c *fc) freeNameOld(slot ir.Val, name string, v ir.Val, vt check.Type) error {
	if !reclaimable(vt) || !c.ownsName(slot, name) {
		return nil
	}
	old := c.f.Temp()
	c.f.Emit(&ir.Mov{D: old, S: slot, T: ir.TInt})
	same := c.f.Fresh()
	c.f.Emit(&ir.Br{Op: "==", A: old, B: v, To: same, T: ir.TInt})
	if err := c.releaseIfAny(old, vt); err != nil {
		return err
	}
	c.f.Emit(&ir.Lbl{At: same})
	return nil
}

// ownsName 判断"这个名字是不是那个槽位的持有者"：本帧登记的（take 那一路）与顶层登记的（takeTop）。
// takeArg 记的是调用点上那些没有名字的槽位，name 是空的，按名字查询不会把它们算进来。
//
// en: ownsName asks "is this name the holder of that slot": registered on this frame (the take
// path) or registered at top level (takeTop). takeArg records unnamed positions at a call site,
// whose name is empty, so a name lookup will never count them.
func (c *fc) ownsName(slot ir.Val, name string) bool {
	if name == "" {
		return false
	}
	for _, o := range c.owns {
		if o.name == name && o.slot == slot {
			return true
		}
	}
	for _, o := range c.l.topOwns {
		if o.name == name && o.slot == slot {
			return true
		}
	}
	return false
}

// argOwnedByCaller 说调用点上交出去的那一个表达式，caller 这一侧是不是最后一份：
// 有名字的按交出记录判（交出去过、又没被证明"接住它的那一侧带走了"⇒ 那个位置就是它的新家）；
// 没名字的按"当场新造的一块"判。**从字段里读出来的句柄不在内**——那个位置仍是持有者，
// 析构它就是析构别人持有的块。
//
// en: argOwnedByCaller says whether the expression handed over at a call site is the last copy
// on the caller's side: named ones are judged by the handover record (handed over once and not
// proven "the receiving end kept it" means that position is its new home); unnamed ones by "a
// block created on the spot". A handle READ OUT OF A FIELD is excluded - that position remains
// the holder, so dismantling it would dismantle property held elsewhere.
func (c *fc) argOwnedByCaller(src ast.Expr, t check.Type) bool {
	if !reclaimable(t) {
		return false
	}
	if n, ok := src.(*ast.Name); ok {
		b, ok := c.l.f.Binds[n]
		if !ok || b.Decl == nil {
			return false
		}
		return c.l.f.Moved[b.Decl] && !c.l.f.Consumed[b.Decl]
	}
	return c.fresh(src, t) || c.takenFromCell(src, t)
}

// takenFromCell 说这一处读的是**容器槽位里的引用类型**：那是"读走"——读的那一刻那个位置
// 返回空句柄，除了这个读出来的值，再无第二个地方持有它。所以它可以登记成持有者
// （读走之后归接住它的那一侧）。
//
// 与"从字段里读出来"是两件事：读字段不算交出，那个位置仍是持有者，登记它就是重复归还。
//
// en: takenFromCell says this read is a REFERENCE TYPE INSIDE A CONTAINER POSITION: that
// is a read-out - at the moment of reading the position returns an empty handle, and apart from
// this value nowhere else holds it. So it may be registered as the holder
// (after a read-out it belongs to whoever receives it). This is a different matter
// from reading out of a field: reading a field is not a handover, that position is
// still the holder, and registering it would return the same block twice.
func (c *fc) takenFromCell(src ast.Expr, t check.Type) bool {
	return takenFromCellOf(c.l, src, t)
}

func takenFromCellOf(l *L, src ast.Expr, t check.Type) bool {
	x, ok := src.(*ast.Index)
	if !ok || !reclaimable(t) {
		return false
	}
	bt, ok := l.f.Types[x.What]
	if !ok {
		return false
	}
	switch bt.Kind {
	case check.List, check.Dict, check.Set:
		return true
	}
	return false
}

// handsBackFresh 说这一个函数或方法返回的引用类型值，除接住它的那一侧外没有其他持有者。
// 有了这一条，调用点就能把它当"当场新造的一块"记账：没人接的那一句到句尽头就析构，
// 接住的那个名字登记成持有者，交进容器一个位置的是那个位置的主人。
//
// 判的是它**每一条**带值的 return，那一项只识别这四种来路：
//   - 当场新造（`new C(...)`、容器与 struct 字面量、拼接生成的文本、标准库返回的那一块）；
//   - 名字交出（`return` 一个局部量、参数、顶层名字）——交出记录把源头那个位置作废了；
//   - 从容器的一个位置里读走（读的那一刻那个位置空了）；
//   - 语言规则认定的那一种读法（`return o.f`：读 class 的引用类型字段算交出，源头那个位置当场抹空）。
//
// 不识别的是从 `struct` 字段里读出来的那一种：struct 是值类型，读那一下不改动源头（实测同一个字段
// 连续读两次仍返回同一块），那块文本的持有者仍是原来的块——调用方析构它就是析构别人持有的一块。
// 无法识别的语句种类、以及环（自调用、两个函数互相调用）一律按"证不出来"处理：那种函数照旧
// 保留它返回的那一块，宁可多保留一处，也不多析构一次。
//
// en: handsBackFresh proves that the reference-typed value this function or method returns is
// held by nobody except the side that receives it. With that, a call site may treat
// it as "a block created on the spot": a result nobody takes is dismantled at the end of the
// sentence, the name that receives it is registered as holder, and one
// handed into a container position becomes that position's property. Every value-returning
// `return` in the body is judged, and the item accepts only four sources: created on the spot
// (`new C(...)`, a container or struct literal, assembled text, a block returned by the
// standard library); a name handed over (`return` a local, a parameter, a top-level name - the
// handover record voided the source position); read out of a container position (the
// position is empty the moment it is read); and the reading the language rule recognises
// (`return o.f` - reading a class's reference-typed field counts as a handover and empties the
// source). What
// is NOT accepted: reading out of a STRUCT field - a struct is a value type, that read does not
// disturb the source (measured: calling it twice still returns the same thing), and the block
// holding that text is still the original one, so the caller dismantling it would dismantle
// somebody else's property. Any statement kind it cannot recognise, and any cycle (self-call,
// two functions calling each other), is treated as "not provable": such functions keep their
// returned block - leaving one block unclaimed is better than dismantling one too often.
func (l *L) handsBackFresh(name string) bool {
	if v, ok := l.freshBack[name]; ok {
		return v
	}
	if l.checkingBack[name] {
		return false
	}
	l.checkingBack[name] = true
	l.freshBack[name] = false // 先把下限摆在这里，递归里读到的就是这一条
	info, ok := l.f.Funcs[name]
	if ok && info != nil {
		l.freshBack[name] = returnsProvablyNew(l, info.Body)
	}
	delete(l.checkingBack, name)
	return l.freshBack[name]
}

// returnsProvablyNew 走完一段语句里的每一条 return。漏一种语句种类就是漏一处证明，所以
// default 那一支按"证不出来"收。
//
// en: returnsProvablyNew walks every return inside a statement. Missing a statement kind means
// missing a proof, so the default branch concludes "not provable".
func returnsProvablyNew(l *L, body []ast.Stmt) bool {
	for _, s := range body {
		switch x := s.(type) {
		case *ast.Return:
			if x.Val == nil {
				continue
			}
			t, ok := l.f.Types[x.Val]
			if !ok || !reclaimable(t) {
				continue // 交回的是值类型与无法识别的类型：与这本账无关
			}
			if !l.handsBackOne(x.Val, t) {
				return false
			}
		case *ast.If:
			for _, b := range x.Bodies {
				if !returnsProvablyNew(l, b) {
					return false
				}
			}
			if x.HasElse && !returnsProvablyNew(l, x.Else) {
				return false
			}
		case *ast.While:
			if !returnsProvablyNew(l, x.Body) {
				return false
			}
		case *ast.For:
			if !returnsProvablyNew(l, x.Body) {
				return false
			}
		case *ast.Switch:
			for _, c := range x.Cases {
				if !returnsProvablyNew(l, c.Body) {
					return false
				}
			}
		case *ast.Try:
			if !returnsProvablyNew(l, x.Body) {
				return false
			}
			if x.HasCatch && !returnsProvablyNew(l, x.Catch) {
				return false
			}
		case *ast.Decl, *ast.Assign, *ast.ExprStmt, *ast.Incdec, *ast.Break, *ast.Continue:
			continue
		default:
			return false
		}
	}
	return true
}

// handsBackOne 判一条 return 返回的那一项。
//
// en: handsBackOne judges the item one `return` hands back.
func (l *L) handsBackOne(e ast.Expr, t check.Type) bool {
	root := unwrappedConvert(e)
	if l.f.Taken[root] {
		return true // 语言规则认定的读法：被读的槽位当场抹空，源头不再持有
	}
	switch x := root.(type) {
	case *ast.New, *ast.ListLit, *ast.MapLit, *ast.SetLit, *ast.StructLit:
		return true
	case *ast.Binary:
		return x.Op == "+" && t.Kind == check.Str
	case *ast.Index:
		return takenFromCellOf(l, root, t)
	case *ast.Name:
		b, ok := l.f.Binds[x]
		if !ok || b.Decl == nil {
			return false
		}
		if b.Decl != nil && b.Decl.Shared {
			return false // export shared 的槽位不参与这本账
		}
		return l.f.Moved[b.Decl]
	case *ast.Call:
		ref, ok := l.f.Calls[x]
		if !ok {
			return false
		}
		if ref.Kind == check.CallStd {
			return freshValue(root, true, t)
		}
		return ref.Kind == check.CallUser && reclaimable(t) && l.handsBackFresh(ref.Func)
	}
	return false
}

// fresh 是 freshValue 那一条判定在调用方这一侧的版本：除了写着"当场新造"的那几种，
// 自己写的函数与方法返回的可回收值也算，前提是那一条函数过得了 handsBackFresh 那一道证明。
//
// en: fresh is the caller-side version of the freshValue test: besides the spellings literally
// marked "created on the spot", a value-returning custom function or method also counts,
// provided that function passes the handsBackFresh proof.
func (c *fc) fresh(e ast.Expr, t check.Type) bool {
	cal, ok := e.(*ast.Call)
	if !ok {
		return freshValue(e, false, t)
	}
	ref, ok := c.l.f.Calls[cal]
	if !ok {
		return false
	}
	if ref.Kind == check.CallStd {
		return freshValue(e, true, t)
	}
	return reclaimable(t) && c.l.handsBackFresh(ref.Func)
}

// handedBack 说这一句调用返回来的，就是调用者自己传进去的那一块：被调方每一条带值的 return
// 返回的都是同一个参数，而那个参数没有别的外流（`Facts.Back` 那份记录）。
// 传进去的那一个实参还得是"这一块第一次被某个名字持有"——从容器位置读走的（读走已把源位置抹成
// `null`）、或当场新造的（字面量、`new`、拼接生成的文本）。两条一起成立，接结果的那个位置
// 就是唯一的持有者；缺一条就是两个名字指向同一块。
//
// en: handedBack says the value this call returns IS the very block the caller passed in: every
// value-returning `return` in the callee returns that same parameter, and that parameter flows
// nowhere else (the Facts.Back record). The argument passed in must also be
// "the first time anybody holds this block" - read out of a container position (the read-out already
// emptied the source to `null`), or created on the spot (a literal, `new`, assembled text).
// Both conditions together make the position receiving the result the sole holder; miss either
// and two names point at one block.
func (c *fc) handedBack(e ast.Expr) bool {
	cal, ok := e.(*ast.Call)
	if !ok {
		return false
	}
	ref, ok := c.l.f.Calls[cal]
	if !ok || ref.Kind != check.CallUser {
		return false
	}
	fn, ok := c.l.f.Funcs[ref.Func]
	if !ok {
		return false
	}
	for i, arg := range cal.Args {
		if i >= len(fn.Params) {
			break
		}
		if !c.l.f.Back[fn.Params[i]] {
			continue
		}
		at := c.l.f.Types[arg]
		if c.takenFromCell(arg, at) || c.fresh(arg, at) {
			return true
		}
		// 名字交进来的那一步也算：交出去之后那个名字就作废了，这一趟出了被调方
		// 只有接结果的那个位置还持有它。只登记局部量与顶层名字，参数逐级上转的那条链暂不处理。
		if n, ok := arg.(*ast.Name); ok {
			if b, ok := c.l.f.Binds[n]; ok && b.Decl != nil && b.Param == nil && c.l.f.Moved[b.Decl] {
				return true
			}
		}
	}
	return false
}

// claim 判"这一笔归不归还"，归还的话按哪一条形状还。不归的几种：`export shared` 那一个名字
// （规则明确它不参与析构，活到进程结束）；不是可还的种类；这一笔把值交出去过、
// 而接住它的那一侧可能仍持有（Facts.Moved 那份记录）；初值既不是自己新造的那一块、也不是从容器位置读走的。
//
// "交出去过"这一条放宽了一半：**只交给过用户函数的参数、而那些参数在它们自己的
// 函数里没有保留该值**（没 return、没写进字段、没装进容器、也没再交出去）的话，这一趟出了被调方
// 就再没有人持有那一块，调用方的帧仍作为持有者（`Facts.Consumed`，判定在检查器最后一步算）。
// 除此之外仍是一份**保守**账：交出去过、或交进过容器与字段的名字都不登记，因为那几处确实还有
// 另一个出处——两个持有者归还同一块，比一块内存无人认领严重得多。
//
// en: claim decides "does this entry get returned" and, if so, by which shape. Not returned: an
// `export shared` name (the rule says plainly it takes no part in destruction and lives until the
// process ends); a kind that is not reclaimable; a name that handed its value over while the
// receiving side may still hold it (the Facts.Moved record); or an initialiser that is neither
// a block it created nor one read out of a container position. The "handed over once" clause
// is loosened halfway: if the value went only into a user function's parameters
// and those parameters did not keep it (no return, no field write, no container store, no
// further handover), then once this call returns nobody holds the block and the caller's frame
// is still its holder (Facts.Consumed, computed in the checker's final step). Everything else
// stays a CONSERVATIVE ledger: a name that handed over, or went into a container or field, is
// not registered, because those places genuinely have another source - two owners returning one
// block is far worse than one block nobody claims.
func (c *fc) claim(slot ir.Val, x *ast.Decl, t check.Type, copied bool) (own, bool) {
	if x.Shared || !reclaimable(t) || (c.l.f.Moved[x] && !c.l.f.Consumed[x]) {
		return own{}, false
	}
	// copied 是"这一块就是这一刻为这个名字拷贝出来的"（struct 的赋值拷贝）：那一判考察的是
	// 表达式的来路，而拷贝之后的块与表达式已经无关，除这个名字外没有其他持有者
	if !copied && !c.fresh(x.Value, t) && !c.takenFromCell(x.Value, t) && !c.handedBack(x.Value) &&
		!staticText(x.Value, t) {
		return own{}, false
	}
	d, err := c.l.descOf(t)
	if err != nil {
		return own{}, false // 排不出形状的还不了：宁可多留一块，也不发一段会跳到无效地址的码
	}
	return own{slot: slot, desc: d, name: x.NameText, direct: t.Kind == check.Str, closer: closerOf(t)}, true
}

// staticText 说这一条声明的初值是写死在程序里的那一段文本（`let s:string = "甲"`）。
//
// 那一份存在数据段里，前面垫了一个全 0 的占位头，`@free` 能识别出它不在堆上、归还它是空操作
// （见 `internal/codegen/data.go` 文件头那一段）。所以把这样的名字登记成持有者是安全的：
// 覆盖时要析构的是换下来的那一份，第一次析构的对象是那段字面量——@free 对它不做处理；往后每次析构的都是堆上的块。
// 不登记就没有这一步：往这个名字里赋新造出来的文本，每一圈都遗留一块无人处理
// （登记之后实测每圈 32 → 0）。
//
// en: staticText says this declaration's initialiser is text baked into the program (`let
// s:string = "a"`). That copy lies in the data section behind an all-zero placeholder header,
// so @free recognises it as not-on-the-heap and its return is a no-op (the note at the top of
// internal/codegen/data.go). Registering such a name as holder is therefore safe:
// overwriting dismantles the replaced copy, the first dismantle gets the literal and
// does nothing, and every later one gets a heap block. Without registration this step disappears:
// assigning freshly built text into that name would leave one unclaimed block per iteration
// (after registration measured 32 bytes per iteration down to 0).
func staticText(e ast.Expr, t check.Type) bool {
	lit, ok := e.(*ast.Lit)
	return ok && lit.Kind == "string" && t.Kind == check.Str
}

// take 登记"这个位置归本帧还"：块尾（循环就是每一圈的圈尾）倒着还。
//
// en: take registers "this position is returned by this frame": at the block tail (for a loop,
// at the end of every iteration), in reverse order.
func (c *fc) take(slot ir.Val, x *ast.Decl, t check.Type, copied bool) {
	if o, ok := c.claim(slot, x, t, copied); ok {
		c.owns = append(c.owns, o)
		c.held = append(c.held, o)
	}
}

// takeTop 登记**顶层静态槽**里的那些条目。分开的理由：这些条目位于静态槽中，本文件的函数可能在
// 别的文件的顶层里被调用，那时它们仍需可读——所以不能在本文件的顶层跑完时就归还，
// 要等整个程序跑完。
//
// 这条路按"落进静态槽"判定，不按"在顶层那一帧里"：顶层的循环体、`if` 块里
// 声明的那些条目用的是帧槽，一圈一还，照旧归 take。
//
// en: takeTop registers an entry that lands in a TOP-LEVEL STATIC SLOT. They are kept apart
// because such an entry lives in a static slot and a function of this file may be called from
// another file's top level, where it must still be readable - so it cannot be returned when
// this file's top level finishes, only when the whole program does. Note this follows "landed in a static slot", not
// "inside the top-level frame": entries declared inside a top-level loop body or `if` block
// live in frame slots, are returned each iteration, and still go through take.
func (c *fc) takeTop(slot ir.Val, x *ast.Decl, t check.Type, copied bool) {
	if o, ok := c.claim(slot, x, t, copied); ok {
		c.l.topOwns = append(c.l.topOwns, o)
	}
}

// reclaimable 说这一个类型的值是不是"一块有持有者的堆内存"。
//
// 文本在列：造出来的串都是 @alloc 出来的一块、头上带 tagString，
// 静态字面量那一版靠全 0 的占位头让 @free 直接停止释放。
// 盒子不在这一列：`p as object` 交出去的是 struct 自己那一块，而 struct 是值类型、原名字依旧可用——
// 两个名字持有同一块，这时归还两次就是析构别人持有的块。
//
// en: reclaimable says whether values of this type are "a heap block with a holder". Text is
// included: a built string is an @alloc'd block carrying tagString in
// its header, and the static literal version makes @free stop immediately via its all-zero
// placeholder header. A box is NOT in this list: `p as object` hands over the struct's
// own block, and since a struct is a value type whose original name remains usable, two
// names would hold one block - returning it twice would dismantle somebody else's property.
func reclaimable(t check.Type) bool {
	switch t.Kind {
	case check.Class, check.Struct, check.List, check.Dict, check.Set, check.Str, check.Json, check.Bytes, check.Socket, check.Listener, check.Process:
		return true
	}
	return false
}

// freshValue 说这一个表达式造出来的东西是不是"从这个槽位第一次被某个名字持有"。
//
// 只有这一种初值才登记成持有者：从字段里读出来的、从容器里取出来的、别的名字交出过来的，
// 那几处目前都还有另一个出处（检查器只在"名字直接交出去"那一条上把源记成交出，
// 而 `g.peer`、`xs[0]`、方法把 own 字段 return 出去这几条不会），登记了就是两个持有者归还同一块。
// 自己写的函数与方法的 return 也不在列内，同一条理由。标准库组函数每一条返回的都是
// 新造的一块，所以接受它。文本拼接同理：`a + b` 返回的是 @str_cat 现造的那一块，
// 两边原来的串各自仍在它们的持有者手里，只有这个位置是新家。
//
// en: freshValue says whether what this expression produces is "held by somebody for the first
// time, right here". Only such initialisers are registered as holders: read out of a field,
// taken out of a container, handed over from another name - each of those still has another
// source (the checker records the source as handed over only for a direct name handover;
// `g.peer`, `xs[0]` and a method returning its own field do not), so registering would give two
// holders for one block. A custom function or method's return is excluded for the same reason.
// Standard-library group functions always return a freshly created block, so they are accepted.
// Text concatenation likewise: `a + b` returns the block @str_cat just made, while the two
// original strings stay with their own holders - only this position is a new home.
func freshValue(e ast.Expr, ref bool, t check.Type) bool {
	switch x := e.(type) {
	case *ast.New, *ast.ListLit, *ast.MapLit, *ast.SetLit, *ast.StructLit:
		return true
	case *ast.Call:
		return ref // 标准库的调用：交回的是新造的一块
	case *ast.Binary:
		return x.Op == "+" && t.Kind == check.Str
	}
	return false
}

// resolve 解析一个名字落到哪里：先看本地作用域（参数、块里的 let、循环变量），
// 再看本文件顶层的静态槽（顶层就是一层作用域）。
// 跨文件带进来的名字与 args 还没排静态槽，当场报出错误，不做静默处理。
//
// en: resolve finds where a name lands: first the local scopes (parameters, block lets, loop
// variables), then this file's top-level static slots (top level is one scope). A name
// brought in across files, and args, have not been allocated static slots yet and are named on
// the spot rather than glossed over.
func (c *fc) resolve(x *ast.Name) (ir.Val, error) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if v, ok := c.scopes[i][x.Text]; ok {
			return v, nil
		}
	}
	if g, ok := c.glob[x.Text]; ok {
		return g, nil
	}
	// args 不是文件里写出来的声明，检查器把它标成 BindArgs，后端照着那个位置静态槽走。
	if c.l.f.Binds[x].By == check.BindArgs {
		return c.l.args, nil
	}
	// 跨文件带进来的顶层 const／shared：静态槽是按"声明它的那个节点"排的，
	// 检查器交出来的 ImportedName 上带着那个节点，顺着它就能找到同一个槽。
	// 键里带上正在降的这一份文件：两份文件各自把不同的东西引成同一个本地名字时，
	// 谁也不许顶掉谁。
	if imp, ok := c.l.f.Imported[check.NameKey(c.filePath, x.Text)]; ok && imp.Decl != nil {
		if g, ok := c.l.glob[imp.Decl]; ok {
			return g, nil
		}
	}
	return nil, fmt.Errorf("%s 是 args 或还没排到静态槽的名字", x.Text)
}

func (c *fc) find(name string) (ir.Val, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if v, ok := c.scopes[i][name]; ok {
			return v, true
		}
	}
	return nil, false
}

// rt 把一个 nef 类型归到中间码的两种种类上。
//
// en: rt maps a nef type onto the two categories of intermediate code.
func (c *fc) rt(t check.Type) ir.T {
	if t.Kind == check.Float {
		return ir.TFlt
	}
	return ir.TInt
}

func (c *fc) expr(e ast.Expr) (ir.Val, check.Type, error) {
	t, ok := c.l.f.Types[e]
	if !ok {
		return nil, t, fmt.Errorf("%s 这一处的类型没记上，前端与后端对不上号", describe(e))
	}
	switch x := e.(type) {
	case *ast.Lit:
		return c.lit(x, t)
	case *ast.Name:
		// 局部槽（参数、this、块里的 let）先查，查不到才识别方法自己的字段，最后才是本文件顶层
		if v, ok := c.find(x.Text); ok {
			return v, t, nil
		}
		if off, ok := c.isField(x.Text); ok {
			return c.selfField(x, off, t)
		}
		v, err := c.resolve(x)
		if err != nil {
			return nil, t, err
		}
		return v, t, nil
	case *ast.Binary:
		return c.binary(x, t)
	case *ast.Unary:
		return c.unary(x, t)
	case *ast.Convert:
		return c.convert(x, t)
	case *ast.Test:
		return c.test(x, t)
	case *ast.New:
		return c.newObject(x, t)
	case *ast.Index:
		return c.indexRead(x, t)
	case *ast.StructLit:
		return c.structLit(x, t)
	case *ast.ListLit:
		return c.listLit(x, t)
	case *ast.MapLit:
		return c.mapLit(x, t)
	case *ast.SetLit:
		return c.setLit(x, t)
	case *ast.Call:
		return c.call(x, t)
	case *ast.Throw:
		return nil, t, c.throwLike(x.Code, x.Msg, "@throw")
	case *ast.Exit:
		// exit 的码是给操作系统的，不套"出错了（…）"那个格式
		return nil, t, c.throwLike(x.Code, x.Msg, "@die")
	case *ast.Member:
		if d, ok := c.l.f.PkgValues[x]; ok {
			// 包交出来的数：静态槽按节点排，检查那一步把声明节点记在 PkgValues 上，
			// 这里拿它取回同一个槽。值类型，读出来就是一份数，不进交出那本账。
			g, laid := c.l.glob[d]
			if !laid {
				return nil, t, fmt.Errorf("包交出来的 %s 还没排到静态槽", x.NameText)
			}
			return g, t, nil
		}
		if t.Kind == check.Enum {
			// 识别的是解到底的类型名，不是写出来的名字：`type Choice = D` 之后
			// `Choice.b` 与 `D.b` 是同一个成员。
			b, ok := c.l.f.EnumVals[t.Name+"."+x.NameText]
			if !ok {
				return nil, t, fmt.Errorf("枚举成员 %s.%s 的值没记上", describe(x.What), x.NameText)
			}
			return ir.K(b), t, nil
		}
		return c.fieldRead(x, t)
	}
	return nil, t, fmt.Errorf("还不支持在这里用 %s", describe(e))
}

func (c *fc) lit(x *ast.Lit, t check.Type) (ir.Val, check.Type, error) {
	switch x.Kind {
	case "int":
		return ir.K(x.Value), t, nil
	case "byte":
		return ir.K(x.Value), t, nil
	case "float":
		f, err := strconv.ParseFloat(strings.ReplaceAll(x.Text, "_", ""), 64)
		if err != nil {
			return nil, t, fmt.Errorf("小数 %s 读不成数：%v", x.Text, err)
		}
		return ir.KF(math.Float64bits(f)), t, nil
	case "char":
		rs := []rune(x.Text)
		if len(rs) != 1 {
			return nil, t, fmt.Errorf("码点字面量 %s 不是一个字符", x.Text)
		}
		return ir.K(int64(rs[0])), t, nil
	case "true":
		return ir.K(1), t, nil
	case "false":
		return ir.K(0), t, nil
	case "null":
		// null 就是零值的句柄：引用类型用它表示当前没有指向任何对象
		return ir.K(0), t, nil
	case "string":
		return c.l.msg(x.Text), t, nil
	}
	return nil, t, fmt.Errorf("认不出的字面量 %s", x.Kind)
}

// structEq 落 `a == b`（struct）：逐字段比较，任何一个字段不等就立即结束。
//
// 字段类型只会是值类型或文本（会共享句柄的那几种在建值那一步就被拒绝）：
// 值类型比较那几字节就够；文本要逐字比较，看 @str_cmp 返回的是不是 0；小数走 @f_order，
// 相等那一档正好是 bitmask 里的 2——NaN 与任何数都不相等，这条 IEEE 的规则顺带就满足了。
//
// en: structEq lowers `a == b` for a struct: field by field, stopping at the first inequality.
// A field's type can only be a value type or text (the kinds that share a handle are refused
// when the value is built): a value type needs only its bytes compared, text compares
// characters through @str_cmp's zero, and floats go through @f_order where the equal rung is
// exactly bitmask 2 - so NaN unequal to everything, an IEEE rule, comes along for free.
func (c *fc) structEq(a, b ir.Val, t check.Type) (ir.Val, error) {
	d := c.f.Temp()
	fail := c.f.Fresh()
	done := c.f.Fresh()
	for _, fi := range c.l.f.Fields[t.Name] {
		off := int(fi.Off)
		ft := fi.Typ
		va := c.f.Temp()
		vb := c.f.Temp()
		c.f.Emit(&ir.Load{D: va, Base: a, Off: off, T: c.rt(ft), Size: c.l.narrow(ft)})
		c.f.Emit(&ir.Load{D: vb, Base: b, Off: off, T: c.rt(ft), Size: c.l.narrow(ft)})
		switch ft.Kind {
		case check.Str:
			r := c.f.Temp()
			c.f.Emit(&ir.Call{D: r, HasD: true, Fn: "@str_cmp", Args: []ir.Val{va, vb},
				FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
			c.f.Emit(&ir.Br{Op: "!=", A: r, B: ir.K(0), To: fail, T: ir.TInt})
		case check.Float:
			r := c.f.Temp()
			c.f.Emit(&ir.Call{D: r, HasD: true, Fn: "@f_order", Args: []ir.Val{va, vb},
				FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
			c.f.Emit(&ir.Br{Op: "!=", A: r, B: ir.K(2), To: fail, T: ir.TInt})
		default:
			c.f.Emit(&ir.Br{Op: "!=", A: va, B: vb, To: fail, T: ir.TInt})
		}
	}
	c.f.Emit(&ir.Mov{D: d, S: ir.K(1), T: ir.TInt})
	c.f.Emit(&ir.Jump{To: done})
	c.f.Emit(&ir.Lbl{At: fail})
	c.f.Emit(&ir.Mov{D: d, S: ir.K(0), T: ir.TInt})
	c.f.Emit(&ir.Lbl{At: done})
	return d, nil
}

// freeTempsTo 归还 [from, through) 那一段，把 `through` 之后的留在记录里。
//
// 使用这一步的是二元运算：操作数那几个槽算完就该归还，而这一步自己新造的那一块（拼接生成的文本）
// 登记在外面那一层的窗口里，必须等外层用完再归还——一起归还就等于析构仍在活动持有者手里的块。
//
// en: freeTempsTo returns the registrations in [from, through) and keeps those after `through`
// in the record. The binary operators use it: the operand slots should be returned as soon as
// the computation is done, while the block this step itself created (assembled text) is
// registered in the window one level out and must wait for that level to finish - returning
// both together would dismantle something a live owner holds.
func (c *fc) freeTempsTo(from, through int) {
	for i := through - 1; i >= from; i-- {
		c.releaseOne(c.temps[i])
	}
	c.temps = append(c.temps[:from], c.temps[through:]...)
}

func (c *fc) binary(x *ast.Binary, t check.Type) (ir.Val, check.Type, error) {
	// && 与 || 要短路，单独走
	if x.Op == "&&" || x.Op == "||" {
		return c.logical(x, t)
	}
	// 两个操作数在这一步里的去向只有两种：比较只读它们的字，拼接用它们拼出**新的一块**。
	// 两种都不把原处交给别人 ⇒ 这一段按只读借用记录，路上读走的那些临时槽算完当场归还
	//（读走那一步已把源位置抹成 `null`，这个位置上再没有第二个出处）。
	// 拼接生成的那一段登记在**外面那一层**的窗口里（`length(a + b)`、`console.print(a + b)`），
	// 这里不登记——登记就等于析构刚交出去的值。
	outer, from := c.borrow, len(c.temps)
	through := from
	c.borrow++
	defer func() {
		c.borrow = outer
		c.freeTempsTo(from, through)
	}()
	a, at, err := c.expr(x.Left)
	if err != nil {
		return nil, t, err
	}
	b, bt, err := c.expr(x.Right)
	if err != nil {
		return nil, t, err
	}
	through = len(c.temps)
	// 操作数都已取得：往后按**外面那一层**的规则登记，这一步自己造的那一块才不会在这里被归还。
	c.borrow = outer
	switch x.Op {
	case "==", "!=", "<", "<=", ">", ">=":
		if at.Kind == check.Struct || bt.Kind == check.Struct {
			// struct 在内存里是一整块，只比较那 8 字节句柄会得到看似合理实则错误的结果：
			// 两份内容完全相同的拷贝，判等反而是 false。所以这一条逐字段比较（判等规则就是这样定的）。
			if x.Op != "==" && x.Op != "!=" {
				return nil, t, fmt.Errorf("%s 之间只有 ==、!= 两条，比大小没有这回事", at)
			}
			eq, err := c.structEq(a, b, at)
			if err != nil {
				return nil, t, err
			}
			if x.Op == "==" {
				return eq, t, nil
			}
			out := c.f.Temp()
			c.f.Emit(&ir.Cmp{Op: "==", D: out, A: eq, B: ir.K(0), T: ir.TInt})
			return out, t, nil
		}
		d := c.f.Temp()
		if at.Kind == check.Str && bt.Kind == check.Str {
			// 文本比较大小与判等一律按内容逐字比（UTF-8 的字节序就是码点序）；
			// 与 null 判等是另一条规则（显式判空），比较的是句柄，不走这里。
			c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@str_cmp", Args: []ir.Val{a, b},
				FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
			out := c.f.Temp()
			c.f.Emit(&ir.Cmp{Op: x.Op, D: out, A: d, B: ir.K(0), T: ir.TInt})
			return out, t, nil
		}
		c.f.Emit(&ir.Cmp{Op: x.Op, D: d, A: a, B: b, T: c.rt(at)})
		return d, t, nil
	}
	if at.Kind == check.Str && x.Op == "+" {
		// 拼接要先读两段的长度，那就是"取它的内容"：空文本当场抛 10092 号
		c.guardNotNull(a)
		c.guardNotNull(b)
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@str_cat", Args: []ir.Val{a, b},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		// 这一段新文本正躺在外面那一层的窗口里时（`length(a + b)` 这种没名字的），登记给那一层还
		c.noteBorrowed(d, t)
		return d, t, nil
	}
	// 移位量是运行时计算的那一半：宽度要到运行期才知道，超出左操作数可移的位数就抛 10021 号。
	// 写成字面量的在检查阶段已经判定（int 0～63、byte 0～7），这里不重复。
	// 下界无需判定：移位量只能写 byte，而 byte 槽位里的值始终在 0～255 之内。
	if x.Op == "<<" || x.Op == ">>" {
		if _, isLit := x.Right.(*ast.Lit); !isLit {
			limit := int64(63)
			if at.Kind == check.Byte {
				limit = 7
			}
			c.throwIf(">", b, ir.K(limit), codeOverflow)
		}
	}
	// 算绪按**这一步的结果类型**发检查：两个 byte 相加的结果是 int，除零与溢出那两道
	// 检查就得跟着按 int 带上（byte 单独算时原本一道都没有）。
	return c.arith(x.Op, a, b, t, x.Where())
}

// arith 发一条算绪，并把该带的检查带上。
//
// en: arith emits one arithmetic instruction together with the required checks.
func (c *fc) arith(op string, a, b ir.Val, t check.Type, at ast.Pos) (ir.Val, check.Type, error) {
	d := c.f.Temp()
	switch op {
	case "+", "-", "*":
		c.f.Emit(&ir.Bin{Op: op, D: d, A: a, B: b, T: c.rt(t)})
		if t.Kind == check.Int {
			c.throwIfOverflow(codeOverflow)
		}
		return d, t, nil
	case "/", "%":
		if t.Kind == check.Int {
			c.throwIfZero(b)
			c.throwIfMinDiv(b, a)
		}
		c.f.Emit(&ir.Bin{Op: op, D: d, A: a, B: b, T: c.rt(t)})
		return d, t, nil
	case "&", "|", "^", "<<", ">>":
		c.f.Emit(&ir.Bin{Op: op, D: d, A: a, B: b, T: ir.TInt})
		return d, t, nil
	}
	return nil, t, fmt.Errorf("还不支持算绪 %q", op)
}

// returnKeep 说这一句 return 交出去的值可能正位于哪几个已登记的槽位之下——那些位置这一趟不许归还。
//
// 只在两件事同时成立时才要保留：返回去的是**句柄**（`class`、三种容器、盒子；文本不在其中，
// `nef.destroy` 这一版本不归还文本），而它是从别的对象底下**读出来**的（字段、容器位置、
// 或者自定义函数返回的那一个）。值类型交出去的是另一份拷贝；`return <名字>` 那一个值
// 本来就被检查器记成"交出去过"、从未登记过——两种都不用保留。
//
// 识别不出源头的那一类（函数返回的句柄）就整帧保留：多留几块是泄漏，
// 析构接收方手里那一块是错误释放别人的内存（同一取舍）。
//
// en: returnKeep says which registered positions the value returned by this `return` may lie
// underneath - those positions must not be returned on this pass. Keeping is needed only when
// both hold: what returns is a HANDLE (class, the three containers, a box; text is not among
// them because this version of nef.destroy does not return text), and it was READ OUT of
// something else (a field, a container position, or a value returned by a custom function). A
// value type hands over a separate copy, and `return <name>` was already recorded by the
// checker as "handed over" and never registered - neither needs keeping. For a source that
// cannot be recognised (a handle coming back from a function call), the whole frame is kept:
// keeping a few extra blocks is a leak, while dismantling the block the receiver holds is
// taking somebody else's property (the same trade-off).
func (c *fc) returnKeep(v ast.Expr, t check.Type) map[string]bool {
	if v == nil || !flowsHandle(t) {
		return nil
	}
	// `return o.f` 交出去的正好是字段里那个位置，读完那个位置就空了，
	// 源头不再持有它 ⇒ 这一帧不必为它保留（此前这一条造成的每圈 104 字节遗留随之归零）。
	if e := unwrappedConvert(v); c.l.f.Taken[e] {
		return nil
	}
	switch v.(type) {
	case *ast.Name, *ast.Member, *ast.Index, *ast.Convert:
		root := chainRoot(v)
		if root == "" {
			return nil
		}
		return map[string]bool{root: true}
	}
	keep := map[string]bool{}
	for _, o := range c.owns {
		keep[o.name] = true
	}
	return keep
}

// unwrappedConvert 把连着写的 `as` 剥到底，返回最里面那一个表达式：
// 交出记录（Facts.Taken）挂在真正被读走的那一个位置上，不挂在外层的换类型上。
//
// en: unwrappedConvert strips back-to-back `as` conversions down to the innermost expression:
// the handover record (Facts.Taken) hangs on the position that was actually read out, not on
// the outer conversion.
func unwrappedConvert(v ast.Expr) ast.Expr {
	out := v
	for {
		cv, ok := out.(*ast.Convert)
		if !ok {
			return out
		}
		out = cv.What
	}
}

// flowsHandle 说这一个类型的值是不是"由别人持有的堆内存"——返回它的时候不许析构它的源头。
// 文本不在这一列：串虽然也是句柄，但析构那一路**不进入串内部**（releaseOne 与 destroy.go 文件头
// 那条规则），所以归还源头那一块不会把交出去的串析构掉，`return h.name` 不必保留 h。
//
// en: flowsHandle says whether values of this type are "a heap block in somebody else's hands"
// - returning one must not dismantle its source. Text is not on this list: a string is a handle
// too, but the dismantling path NEVER DESCENDS INTO A STRING (the rule at the top of releaseOne
// and destroy.go), so returning the source block cannot destroy the string handed over, and
// `return h.name` does not need to keep h.
func flowsHandle(t check.Type) bool {
	switch t.Kind {
	case check.Class, check.List, check.Dict, check.Set, check.Obj, check.Bytes, check.Socket, check.Listener, check.Process:
		return true
	}
	return false
}

// chainRoot 顺着点号与方括号回溯到最里面那一个名字：`h.items[0]` 回溯到 h，无法回溯时返回空。
//
// en: chainRoot follows dots and subscripts back to the innermost name: `h.items[0]` leads back
// to h; if it cannot, the result is empty.
func chainRoot(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Name:
			return x.Text
		case *ast.Member:
			e = x.What
		case *ast.Index:
			e = x.What
		case *ast.Convert:
			e = x.What
		default:
			return ""
		}
	}
}

// popsTo 补上跳出 try 体时应释放的那一条 PopHandler。
//
// 规则是"接住块走完就把这一条摘掉"，正常走完那一路 close() 后面跟着 PopHandler；
// 但 `return`、`break`、`continue` 都能从 try 体中间跳走，那条路不经过 PopHandler——
// 链头就残留一条指向**已经作废的帧**的记录。后果不是内存泄漏：后来任何一处无人接住的抛出
// 会先命中这一条记录，跳进那个作废帧的接住块，程序对用户而言静默地以退出码 0 结束（退出清理代码实测观察到的）。
// 所以凡是跳出若干层 try 体的早退路，先按从里到外的顺序把它们全部摘除。
//
// en: popsTo adds the PopHandler instruction owed when control leaves a try body. The rule says
// "once the catch block is done, remove this entry", and the normal path emits PopHandler after
// close() - but `return`, `break` and `continue` can jump out from the middle of a try body,
// and that route passes no PopHandler, leaving a record on the chain head that points at a
// frame ALREADY DEAD. The consequence is not leaked memory: any later uncaught throw meets this
// entry first and jumps into that dead frame's catch block, and the process exits quietly with
// code 0 in front of the user (observed while testing the exit cleanup code). So every early
// exit that leaves several try-body layers first unwinds them inside out.
func (c *fc) popsTo(mi int) {
	for i := len(c.tries) - 1; i >= 0; i-- {
		if c.tries[i] > mi {
			c.f.Emit(&ir.PopHandler{})
		}
	}
}

// throw 返回一段"调用 @throw"的指令：@throw 打印完就不再返回。
//
// 前面先垫一段退出清理代码：把这一趟被释放的那些层归还干净（规则："本层的局部值按声明的反序析构，
// 这一条对正常走完和抛出去两种情况完全一样"）。包住这一处的最近一个 try 属于**本帧**时，
// 就只归还到它那一层为止——接住块接着还要读 try 体外面的那些名字。
//
// en: throw returns a "call @throw" sequence: it prints and leaves, and must not come back.
// Exit cleanup is emitted in front of it: every layer being unwound returns its
// registrations (the rule: "this layer's locals are destructed in reverse declaration order,
// identically for completing normally and for throwing"). When the nearest enclosing try is in
// THIS frame, unwinding stops at that layer, because the catch block still has to read the
// names declared outside the try body.
func (c *fc) throw(code int) {
	msg, ok := builtinMsg[code]
	if !ok {
		msg = "内建错误"
	}
	c.leaveToHandler()
	c.f.Emit(&ir.Call{Fn: "@throw", Args: []ir.Val{ir.K(int64(code)), c.l.msg(msg)}, FT: []ir.T{ir.TInt, ir.TInt}})
}

// throwIfOverflow 紧跟在上一条 add／sub／imul 之后：结果回绕溢出就跳转去抛出。
//
// en: throwIfOverflow follows the previous add/sub/imul: if the arithmetic wrapped, jump to the
// throw.
func (c *fc) throwIfOverflow(code int) {
	bad := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.IfOverflow{To: bad})
	c.f.Emit(&ir.Jump{To: end})
	c.f.Emit(&ir.Lbl{At: bad})
	c.throw(code)
	c.f.Emit(&ir.Lbl{At: end})
}

func (c *fc) throwIfZero(b ir.Val) {
	c.throwIf("==", b, ir.K(0), codeDivZero)
}

// throwIfMinDiv 挡最小值除以 -1：那条在 CPU 上是硬件异常，不能让它发生。
//
// en: throwIfMinDiv blocks the minimum value divided by -1: that is a hardware exception on the
// CPU and must never happen.
func (c *fc) throwIfMinDiv(b, a ir.Val) {
	skip := c.f.Fresh()
	bad := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Br{Op: "!=", A: b, B: ir.K(-1), To: skip, T: ir.TInt})
	c.f.Emit(&ir.Br{Op: "!=", A: a, B: ir.K(minInt64), To: skip, T: ir.TInt})
	c.f.Emit(&ir.Jump{To: bad})
	c.f.Emit(&ir.Lbl{At: skip})
	c.f.Emit(&ir.Jump{To: end})
	c.f.Emit(&ir.Lbl{At: bad})
	c.throw(codeOverflow)
	c.f.Emit(&ir.Lbl{At: end})
}

func (c *fc) logical(x *ast.Binary, t check.Type) (ir.Val, check.Type, error) {
	a, _, err := c.expr(x.Left)
	if err != nil {
		return nil, t, err
	}
	d := c.f.Temp()
	short := c.f.Fresh()
	done := c.f.Fresh()
	if x.Op == "&&" {
		c.f.Emit(&ir.Jz{V: a, To: short})
	} else {
		c.f.Emit(&ir.Jnz{V: a, To: short})
	}
	b, _, err := c.expr(x.Right)
	if err != nil {
		return nil, t, err
	}
	c.f.Emit(&ir.Mov{D: d, S: b, T: ir.TInt})
	c.f.Emit(&ir.Jump{To: done})
	c.f.Emit(&ir.Lbl{At: short})
	c.f.Emit(&ir.Mov{D: d, S: ir.K(boolOf(x.Op)), T: ir.TInt})
	c.f.Emit(&ir.Lbl{At: done})
	return d, t, nil
}

func boolOf(op string) int64 {
	if op == "&&" {
		return 0
	}
	return 1
}

func (c *fc) unary(x *ast.Unary, t check.Type) (ir.Val, check.Type, error) {
	v, vt, err := c.expr(x.What)
	if err != nil {
		return nil, t, err
	}
	d := c.f.Temp()
	switch x.Op {
	case "-":
		c.f.Emit(&ir.Neg{D: d, S: v, T: c.rt(vt)})
		return d, t, nil
	case "!":
		c.f.Emit(&ir.LNot{D: d, S: v})
		return d, t, nil
	case "~":
		c.f.Emit(&ir.Not{D: d, S: v})
		return d, t, nil
	}
	return nil, t, fmt.Errorf("还不支持一元 %q", x.Op)
}

// throwIf 发一段"条件成立就抛、否则接着走"。每处检查都就地放一块，不去共用一个出口——
// try／catch 实现之后，每个抛出点要能报出自己是谁，共用出口会把这件事糊掉。
//
// en: throwIf emits "throw if the condition holds, otherwise continue". Each check gets its own
// block instead of sharing one exit: once try/catch exists, every throw site must be able to
// say who it is, and a shared exit would blur that.
func (c *fc) throwIf(op string, a, b ir.Val, code int) {
	bad := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Br{Op: op, A: a, B: b, To: bad, T: ir.TInt})
	c.f.Emit(&ir.Jump{To: end})
	c.f.Emit(&ir.Lbl{At: bad})
	c.throw(code)
	c.f.Emit(&ir.Lbl{At: end})
}

// guardByte 挡"一个算出来的数落进 byte 的位置"：超出 0～255 当场抛 10013 号。
// 字面量在检查阶段就卡住了（byte 只能装 0～255，这里是 300），这一道管的是此刻还不知道的那一半。
//
// en: guardByte blocks "a computed number landing in a byte position": outside 0-255 it throws
// code 10013 on the spot. Literals were already stopped at check time (a byte holds only 0-255,
// and 300 was refused there); this handles the half that is only known at runtime.
func (c *fc) guardByte(v ir.Val) {
	c.throwIf("<", v, ir.K(0), codeRange)
	c.throwIf(">", v, ir.K(255), codeRange)
}

// incdec 落 `i++`、`i--`：读出来、加一或减一、写回去，三步全用现成的那两条路。
//
// 溢出照 `i = i + 1` 同一条规矩抛 10021 号——`int` 的上界加一都在这儿拒绝，不留"绕回去"这种静默结果。
// `byte` 那一侧不一样：加一减一先按 int 算，写回 byte 位置时越界抛 10013 号，
// 255 加一不给 256、0 减一不给 −1（256 塞进 byte 槽里就是一句假话：类型写着 byte，值却是 256）。
// 检查器已经保证目标能改（不是 `const`）。
//
// en: incdec lowers `i++` and `i--`: read it out, add or subtract one, write it back - all
// three steps reuse the two existing paths. Overflow throws code 10021 under the same rule as
// `i = i + 1`: even the upper bound of `int` is stopped here, leaving no silent
// wrap-around. The `byte` side differs: plus and minus are computed as int first, and writing
// back out of range throws 10013 - 255 plus one does not give 256 and 0 minus one does not give
// -1 (storing 256 in a byte slot would be a lie: the type says byte, the value says 256). The
// checker already guaranteed the target is writable (not `const`).
func (c *fc) incdec(x *ast.Incdec) error {
	v, vt, err := c.expr(x.What)
	if err != nil {
		return err
	}
	op := "+"
	if !x.Up {
		op = "-"
	}
	// 那个"一"要跟着类型走：小数那一侧中间码认的是位型，塞个整数常量进去发射器就断了
	var one ir.Val = ir.K(1)
	if vt.Kind == check.Float {
		one = ir.KF(math.Float64bits(1))
	}
	at := vt
	if vt.Kind == check.Byte {
		at = check.Type{Kind: check.Int}
	}
	nv, _, err := c.arith(op, v, one, at, x.Where())
	if err != nil {
		return err
	}
	if vt.Kind == check.Byte {
		c.guardByte(nv)
	}
	return c.storeTo(x.What, nv, vt)
}

// guardNotNull 是"没判空就取成员"那条检查：当场抛 10092 号，检查常开没有开关。
//
// en: guardNotNull is the "dereferencing without a null test" check: it throws code 10092 on
// the spot, and this check is always on with no switch.
func (c *fc) guardNotNull(v ir.Val) {
	c.throwIf("==", v, ir.K(0), codeNull)
}

// fieldRead 落 `x.f`：先挡空引用，再按字段偏移从对象上取那一份。
//
// 语言规则：`class` 里装引用类型的那个位置被**接走**的时候，这一读就是交出，读完把那个位置
// 写空——与容器那个位置（`indexRead`）同一本账。改之前同一个字段读两次会拿到两个都能写的名字。
// 哪些算"接走"由检查器记在 `Facts.Taken` 上（落到名字、当实参交出去、`return` 出去、装进位置、装箱）；
// 顺着看一眼不算：`console.print(o.i.n)`、`o.list.add(x)` 那一读没有新持有者，抹空只会把值弄丢。
//
// en: fieldRead lowers `x.f`: first refuse a null reference, then take that field off the object
// by its offset. By the language rule, when a reference-typed position inside a `class` is
// TAKEN AWAY, this read is the handover and the position is written empty afterwards - the same
// ledger as a container position (indexRead). Before this change, reading the same field
// twice produced two names that could both write it. Which readings count as taking away is
// recorded by the checker in Facts.Taken (into a name, as an argument, through `return`, into a
// position, into a box). A look-through does not: `console.print(o.i.n)` and `o.list.add(x)`
// create no new holder, and emptying there would only lose the value.
func (c *fc) fieldRead(x *ast.Member, t check.Type) (ir.Val, check.Type, error) {
	base, bt, err := c.expr(x.What)
	if err != nil {
		return nil, t, err
	}
	off, err := c.fieldOffset(bt.Name, x.NameText)
	if err != nil {
		return nil, t, err
	}
	c.guardNotNull(base)
	d := c.f.Temp()
	c.f.Emit(&ir.Load{D: d, Base: base, Off: int(off), T: c.rt(t), Size: c.l.narrow(t)})
	if c.l.f.Taken[x] {
		// 只有真被接走的那一次才抹空，也才允许当场还（只读借用那一条登记靠"源位置已经空了"才安全）
		c.f.Emit(&ir.Store{Base: base, Off: int(off), S: ir.K(0), T: ir.TInt})
		c.noteBorrowed(d, t)
	}
	return d, t, nil
}

// isField 认"方法体里直接写的那个字段名"。struct 没有方法，所以能走到这儿的一定是 class。
//
// en: isField recognises "the field name written bare inside a method body". A struct has no
// methods, so anything reaching here belongs to a class.
func (c *fc) isField(name string) (int64, bool) {
	if c.owner == "" {
		return 0, false
	}
	return c.l.fieldOff(c.owner, name)
}

// selfField 落方法体里直接写的字段名——它就是 this.那个字段，不必再挡一次空：
// 调用那一侧已经挡过，进得来就说明句柄是好的。
//
// en: selfField lowers a field name written bare in a method body - it is this.thatField, and
// needs no second null guard: the call side already guarded it, and reaching here proves the
// handle is good.
func (c *fc) selfField(x *ast.Name, off int64, t check.Type) (ir.Val, check.Type, error) {
	recv, ok := c.find("this")
	if !ok {
		return nil, t, fmt.Errorf("%s 是字段，可这个方法体里没有 this：接收者没落到第 0 槽", x.Text)
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Load{D: d, Base: recv, Off: int(off), T: c.rt(t), Size: c.l.narrow(t)})
	if c.l.f.Taken[x] {
		c.f.Emit(&ir.Store{Base: recv, Off: int(off), S: ir.K(0), T: ir.TInt})
		c.noteBorrowed(d, t)
	}
	return d, t, nil
}

// fieldOffset 查字段在第几个字节，查不到就是前端与后端对不上号。
//
// en: fieldOffset looks up which byte a field is at; failing to find it means the front end and
// the backend disagree.
func (c *fc) fieldOffset(typeName, field string) (int64, error) {
	off, ok := c.l.fieldOff(typeName, field)
	if !ok {
		return 0, fmt.Errorf("%s 里没有字段 %s：前端交出来的字段表对不上", typeName, field)
	}
	return off, nil
}

// newObject 落 `new C()` 与 `new C(f: 值, …)`：要一块堆、把给定的字段摆进去，
// 没给的那些留零值——@alloc 返回来的本来就是没被人碰过的新页，零值白拿。
//
// en: newObject lowers `new C()` and `new C(f: value, ...)`: allocate on the heap, place the
// given fields, and leave the rest at zero - a page @alloc returns has never been
// touched, so the zeroes come free.
func (c *fc) newObject(x *ast.New, t check.Type) (ir.Val, check.Type, error) {
	outer, from := c.openWindow() // 同 listLit：进字段的那几份挑出去，中间读走的当场归还
	tag, ok := c.l.tagOf(t)
	if !ok {
		return nil, t, fmt.Errorf("%s 的类型字没排上", t)
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@alloc",
		Args: []ir.Val{ir.K(c.l.f.ObjectSize(t.Name)), ir.K(tag)},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	keeps := make([]ir.Val, 0, len(x.Fields))
	for _, f := range x.Fields {
		v, vt, err := c.expr(f.Value)
		if err != nil {
			return nil, t, err
		}
		// 字段那个位置与容器字面量同一本账：来路是当场造好的那一块就直接住进去，
		// 是名字或别的类型的一个位置就另起一份——不然对象与手里那个名字指着同一块，值类型就不是值类型了
		v, err = c.cellValue(v, vt, f.Value)
		if err != nil {
			return nil, t, err
		}
		off, err := c.fieldOffset(t.Name, f.NameText)
		if err != nil {
			return nil, t, err
		}
		c.f.Emit(&ir.Store{Base: d, Off: int(off), S: v, T: c.rt(vt), Size: c.l.narrow(vt)})
		keeps = append(keeps, v) // 那个字段是这一份的持有者
	}
	c.closeWindow(outer, from, keeps...)
	return d, t, nil
}

// test 落 `x is T`：只看不动手。空盒子问不出类型，答"不是"就完事，
// 不去读 [null-8] 那块不存在的头。
//
// en: test lowers `x is T`: only look, never touch. An empty box cannot answer a type,
// so it is simply "no" - reading the header at [null-8] would be reading a header that does not
// exist.
func (c *fc) test(x *ast.Test, t check.Type) (ir.Val, check.Type, error) {
	v, _, err := c.expr(x.What)
	if err != nil {
		return nil, t, err
	}
	want, ok := c.l.f.Tests[x]
	if !ok {
		return nil, t, fmt.Errorf("%s 问的是哪个类型没记上，前端与后端对不上号", describe(x))
	}
	tag, ok := c.l.tagOf(want)
	if !ok {
		return nil, t, fmt.Errorf("%s 的类型字没排上", want)
	}
	d := c.f.Temp()
	yes := c.f.Fresh()
	done := c.f.Fresh()
	c.f.Emit(&ir.Br{Op: "==", A: v, B: ir.K(0), To: done, T: ir.TInt})
	got := c.f.Temp()
	c.f.Emit(&ir.Load{D: got, Base: v, Off: -8, T: ir.TInt, Size: 4})
	c.f.Emit(&ir.Br{Op: "==", A: got, B: ir.K(tag), To: yes, T: ir.TInt})
	c.f.Emit(&ir.Mov{D: d, S: ir.K(0), T: ir.TInt})
	c.f.Emit(&ir.Jump{To: done})
	c.f.Emit(&ir.Lbl{At: yes})
	c.f.Emit(&ir.Mov{D: d, S: ir.K(1), T: ir.TInt})
	c.f.Emit(&ir.Lbl{At: done})
	return d, t, nil
}

// convert 里盒子那一侧单独走：装箱与拆盒都是交出，只是谁在堆上多一块的区别。
//
// en: In convert, the boxing side takes its own path: boxing and unboxing are both handovers,
// the only difference being who gets one more block on the heap.
func (c *fc) convert(x *ast.Convert, t check.Type) (ir.Val, check.Type, error) {
	v, vt, err := c.expr(x.What)
	if err != nil {
		return nil, t, err
	}
	if t.Kind == check.Obj && vt.Kind != check.Obj {
		return c.box(v, vt)
	}
	if vt.Kind == check.Obj && t.Kind != check.Obj {
		return c.unbox(v, t)
	}
	if t.Kind == check.Obj || vt.Kind == check.Obj {
		// 装箱与拆盒上面两支已经走完，两头都是盒子那一例前端就拦了（不必再装箱），
		// 走到这里就是前端与后端对不上号。
		return nil, t, fmt.Errorf("%s 转 %s：前端拦下的写法走到了后端", vt, t)
	}
	d := c.f.Temp()
	switch {
	case t.Kind == check.Float:
		c.f.Emit(&ir.Cvt{D: d, S: v, To: ir.TFlt})
	case vt.Kind == check.Float:
		c.f.Emit(&ir.Cvt{D: d, S: v, To: ir.TInt})
	default:
		// int↔byte、int↔unicode 这些只是换个看法，槽里那 64 位不用动
		c.f.Emit(&ir.Mov{D: d, S: v, T: ir.TInt})
	}
	// 落到 byte 的这一侧要当场验范围：槽里那 64 位不动，不等于它还在 0～255 里。
	// 本来就是 byte 的不必再问（byte 位置里的值一向过得去，过不了的那几处已经抛在别处）。
	if t.Kind == check.Byte && vt.Kind != check.Byte {
		c.guardByte(d)
	}
	return d, t, nil
}

// box 装箱。class 的句柄本身就指向一块带类型字的堆对象，直接交出去就成；
// 别的类型（值类型、string）在槽里只是一个数或一个不带标记的句柄，得另起一块盒子装进去。
//
// en: box is boxing. A class's handle already points at a heap object carrying a type
// word, so handing it over directly works. Other types (value types, string) are just a number
// or an untagged handle in the slot, so they need a separate box block around them.
func (c *fc) box(v ir.Val, vt check.Type) (ir.Val, check.Type, error) {
	if vt.Kind == check.Obj {
		return nil, check.Type{}, fmt.Errorf("%s 已经是盒子，不必再装", vt)
	}
	switch vt.Kind {
	case check.List, check.Dict, check.Set:
		// 整只容器不进盒子是前端那条规矩（拆回来要认出尖括号里那一层，一个号不够），
		// 发码这一步走到这里就是前端与后端对不上号。
		return nil, check.Type{}, fmt.Errorf("把 %s 装进盒子：前端拦下的写法走到了后端", vt)
	}
	tag, ok := c.l.tagOf(vt)
	if !ok {
		return nil, check.Type{}, fmt.Errorf("%s 的类型字没排上", vt)
	}
	obj := check.Type{Kind: check.Obj}
	if vt.Kind == check.Class {
		return v, obj, nil
	}
	if vt.Kind == check.Struct {
		// struct 那一块的头里已经写着它自己的类型字（造它的时候写的 @alloc），
		// 装进盒子就是把同一份句柄交出去，`is`／`as` 照标记认，不必另造一层
		return v, obj, nil
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@alloc",
		Args: []ir.Val{ir.K(int64(c.l.f.Sizeof(vt))), ir.K(tag)},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	c.f.Emit(&ir.Store{Base: d, Off: 0, S: v, T: c.rt(vt), Size: c.l.narrow(vt)})
	return d, obj, nil
}

// unbox 拆盒：先认标记，对不上就抛 10011 号；认过了才按目标类型取那个位置。
// 盒子本身是 @alloc 出来的，用完还回堆里要等"还自由"接上，眼下先让它留在堆上。
//
// en: unbox is unboxing: check the tag first and throw code 10011 when it does not
// match; only after that is the position read as the target type. The box itself came from
// @alloc, and returning it to the heap waits for the freedom work to be wired up - for now it
// stays on the heap.
func (c *fc) unbox(v ir.Val, t check.Type) (ir.Val, check.Type, error) {
	tag, ok := c.l.tagOf(t)
	if !ok {
		return nil, t, fmt.Errorf("%s 的类型字没排上，拆不开这个盒", t)
	}
	c.guardNotNull(v)
	got := c.f.Temp()
	c.f.Emit(&ir.Load{D: got, Base: v, Off: -8, T: ir.TInt, Size: 4})
	c.throwIf("!=", got, ir.K(tag), codeUnbox)
	if t.Kind == check.Class || t.Kind == check.Struct {
		// 盒子里装的就是这个句柄：class 是对象自己，struct 是那一整块
		return v, t, nil
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Load{D: d, Base: v, Off: 0, T: c.rt(t), Size: c.l.narrow(t)})
	return d, t, nil
}

func (c *fc) call(x *ast.Call, t check.Type) (ir.Val, check.Type, error) {
	ref, ok := c.l.f.Calls[x]
	if !ok {
		return nil, t, fmt.Errorf("这次调用的落点没记上")
	}
	// 只读借用那一族（标准库组函数）：参数交完就不归它们管了，所以这一段里
	// "从容器位置读走、落在没名字临时槽上"的那一块由这一侧当场还。
	// 容器那几条（add／remove／contains）不在内：add 会把值留在位置里，另有持有者。
	if ref.Kind == check.CallStd && stdBorrowsArgs(ref.Std) {
		base := len(c.temps)
		c.borrow++
		defer func() {
			c.borrow--
			c.freeTemps(base)
		}()
	}
	if ref.Kind == check.CallStd && ref.Std == "console.print" {
		return nil, t, c.print(x.Args, false)
	}
	if ref.Kind == check.CallStd && ref.Std == "console.print_error" {
		return nil, t, c.print(x.Args, true)
	}
	if ref.Kind == check.CallStd && ref.Std == "console.read" {
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@read_line", FT: []ir.T{}, RT: ir.TInt})
		return d, t, nil
	}
	if ref.Kind == check.CallStd && ref.Std == "file.read" {
		p, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(p)
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@file_read", Args: []ir.Val{p},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	}
	if ref.Kind == check.CallStd && containerAction(ref.Std) {
		return c.stdContainer(x, ref, t)
	}
	if ref.Kind == check.CallStd && ref.Std == "text.to_int" {
		v, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(v)
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@str_to_int", Args: []ir.Val{v},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	}
	if ref.Kind == check.CallStd && ref.Std == "text.to_float" {
		// 返回的是那 64 位的位型，跟 @f_sqrt 一样的走法：槽里存的就是这个数
		v, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(v)
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@str_to_float", Args: []ir.Val{v},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	}
	if ref.Kind == check.CallStd && ref.Std == "string.split" {
		m, ok := x.Callee.(*ast.Member)
		if !ok {
			return nil, t, fmt.Errorf("text.split 该写成点名式，前端与后端对不上号")
		}
		recv, _, err := c.expr(m.What)
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(recv)
		sep, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(sep)
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@str_split", Args: []ir.Val{recv, sep},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d, t, nil
	}
	if ref.Kind == check.CallStd && ref.Std == "number.to_string" {
		return c.numToString(x, t)
	}
	if ref.Kind == check.CallStd && strings.HasPrefix(ref.Std, "json.") {
		return c.jsonCall(x, ref, t)
	}
	if ref.Kind == check.CallStd {
		return c.stdAction(x, ref, t)
	}
	args, fts, err := c.recv(x, ref)
	if err != nil {
		return nil, t, err
	}
	rest, restT, err := c.argsOf(x.Args, ref.Func)
	if err != nil {
		return nil, t, err
	}
	args = append(args, rest...)
	fts = append(fts, restT...)
	d := c.f.Temp()
	var dv ir.Val = d
	has := t.Kind != check.NoRet
	if !has {
		dv = nil
	}
	c.f.Emit(&ir.Call{D: dv, HasD: has, Fn: ref.Func, Args: args, FT: fts, RT: c.rt(t)})
	if has {
		return d, t, nil
	}
	return nil, t, nil
}

// recv 是方法调用的那一份接收者：排在第一个参数位上，走 rcx。
// 普通函数返回空表，接收者那个位置由被调方的第 0 槽认。
//
// en: recv is the receiver of a method call: it sits in the first argument slot and travels in
// rcx. An ordinary function returns an empty list, and the receiver position is
// recognised by the callee's slot 0.
func (c *fc) recv(x *ast.Call, ref check.CallRef) ([]ir.Val, []ir.T, error) {
	m, ok := x.Callee.(*ast.Member)
	if !ok || ref.Kind != check.CallUser {
		return nil, nil, nil
	}
	if ref.Recv.Kind == check.ErrT {
		// 点号左边是包的挂名（`greet.twice(2)`）：那不是接收者，
		// 这一号调用与被调方第 0 槽认普通函数参数的写法一样。
		return nil, nil, nil
	}
	v, _, err := c.expr(m.What)
	if err != nil {
		return nil, nil, err
	}
	c.guardNotNull(v)
	return []ir.Val{v}, []ir.T{ir.TInt}, nil
}

func (c *fc) args(list []ast.Expr) ([]ir.Val, []ir.T, error) {
	vals := make([]ir.Val, 0, len(list))
	kinds := make([]ir.T, 0, len(list))
	for _, a := range list {
		v, t, err := c.expr(a)
		if err != nil {
			return nil, nil, err
		}
		if v == nil {
			return nil, nil, fmt.Errorf("这里需要一个值，但 %s 不返回任何东西", describe(a))
		}
		// 传参对值类型就是"给它一份副本"：被调方怎么改都不碰调用方这一个
		v, err = c.copyVal(v, t)
		if err != nil {
			return nil, nil, err
		}
		vals = append(vals, v)
		kinds = append(kinds, c.rt(t))
	}
	return vals, kinds, nil
}

// argsOf 是调用点那一份实参表：写出来的照旧，**没写出来的从被调方的默认值补上**。
//
// 补在哪儿很重要：默认值是在调用这一侧算的，不是被调方进门时兜底——被调方一进门
// 参数槽就得有值，而它不知道来的是实参还是替身。字面量当默认值最省事也最安全：
// 它不读任何东西，因而不存在"这句在调用方这边算，读到的却是调用方的同名变量"那种事。
// 想要 `x:int = 别的变量` 这种默认值，要先定下"在哪个作用域里算"，这一版不接受这种写法。
//
// en: argsOf is the argument list at a call site: what was written stays as written, and WHAT
// WAS NOT IS FILLED FROM THE CALLEE'S DEFAULTS. Where the filling happens matters: defaults are
// computed on the call side, not patched up by the callee on entry - the callee's parameter
// slots must already hold a value on entry and it cannot tell an argument from a stand-in. A
// literal default is the cheapest and safest: it reads nothing, so there is no case of "this
// expression is evaluated on the caller's side but reads the caller's variable of the same
// name". A default like `x:int = someOtherVariable` would first need a decision on "in which
// scope it is evaluated"; this version does not accept that form.
func (c *fc) argsOf(list []ast.Expr, fnName string) ([]ir.Val, []ir.T, error) {
	// 开一层窗口，最后把交出去的每一个槽挑出去（那一块的归属由 `takeArg` 与交出那本账
	// 判，被调方带走了的话这里当场还就是拆仍在活动的持有者手里的），剩下没挑出去的——`wear(src[0])` 里
	// 那个 `src[0]` 读走的一个位置——在这一段走完按反序还。先前整段关在这本账外面，那一块没人登记。
	outer, from := c.openWindow()
	vals, kinds, err := c.args(list)
	if err != nil {
		return nil, nil, err
	}
	fn, ok := c.l.f.Funcs[fnName]
	if !ok {
		c.closeWindow(outer, from, vals...)
		return vals, kinds, nil
	}
	for i := len(vals); i < len(fn.Params); i++ {
		p := fn.Params[i]
		if p.Default == nil {
			return nil, nil, fmt.Errorf("%s 的第 %d 个参数 %s 没有默认值，调用时得给", fnName, i+1, p.NameText)
		}
		// 默认值能写成什么由检查器在声明那一处固定：字面量、const 名字与这两样的算术。
		// 少给的那几个参数都在被调方一进门就有值，所以这里就地降那一段——算术里不含函数调用，不会重走一遍调用图。
		//
		// 那一段是在**被调方那份文件里**写的，所以降它时要临时换成那一侧的作用域：本文件的顶层表
		// 与那份文件的路径（引进来的 const 按（文件, 名字）记，路径错了就找不到那个位置），
		// 同时把调用方这一侧的局部量挡在门外——不然调用方一个叫 `STEP` 的局部量会顶掉被调方那个 `const STEP`。
		backScopes, backGlob, backPath := c.scopes, c.glob, c.filePath
		c.scopes = []map[string]ir.Val{{}}
		c.glob, c.filePath = c.l.funcGlobals[fnName], c.l.funcFile[fnName]
		v, vt, err := c.expr(p.Default)
		c.scopes, c.glob, c.filePath = backScopes, backGlob, backPath
		if err != nil {
			return nil, nil, err
		}
		v, err = c.copyVal(v, vt)
		if err != nil {
			return nil, nil, err
		}
		vals = append(vals, v)
		kinds = append(kinds, c.rt(vt))
	}
	// 交出去的每一个槽，被调方不带走的话就归这一层还。有名字的那一类在声明
	// 那一侧已经登记过了（`claim` 看 Facts.Consumed），这一趟管的是**没名字**的：
	// `use([1, 2])`、`use("甲,乙".split(","))`、`keep(box, new Bag(n: 1))`——式子当场新造的一块，
	// 除了这个位置没有第二个地方拿着它，出了被调方就再没人认得。
	for i := range vals {
		if i >= len(fn.Params) {
			break
		}
		if c.l.f.Moved[fn.Params[i]] {
			continue // 带走：那一块被 return／写进字段／装进容器／又交出去了，另有出处
		}
		src := fn.Params[i].Default
		if i < len(list) {
			src = list[i]
		}
		c.takeArg(vals[i], src)
	}
	c.closeWindow(outer, from, vals...) // 交出去的那些槽归上面那本账，中间读走的这几步到这里还完
	return vals, kinds, nil
}

// takeArg 给调用点上这个位置登记一笔持有者。登记不上的几种一律放过（多留一块是漏，不是错）：
// 落点不是帧槽（值直接进寄存器那种）、式子不是当场新造的一块、类型本来不参与析构。
//
// 值类型实参是同一条道理的另一副样子：`args` 那一侧为它整份拷了一块（传参对值类型就是"给它一份副本"），
// 那一块是这一刻为这一个位置造的——写得多有名字都一样，
// `count(q)` 里 `q` 还在，可 `q` 不是这一份。被调方带走了的话上面那条 `Moved` 已经跳过。
// 拷出来的这一块只拆它自己：里面的文本字段是共用那一份，`nef.destroy` 走到文本就不做处理。
//
// en: takeArg registers a holder for this position at a call site. The cases that cannot be
// registered are all let through (one extra block is a leak, not an error): the destination is
// not a frame slot (the value goes straight into a register), the expression is not a block
// created on the spot, or the type takes no part in destruction. A value-typed argument is the
// same reasoning in another shape: the args side copied a whole block for it (
// passing a value type means giving it a copy), and that block was made at this moment for
// this position; it looks the same whether a name is attached or not - inside `count(q)`, `q`
// still exists but `q` is not this copy. If the callee took it away, the Moved rule above
// already skipped it. The copy is dismantled only as itself: its text fields are the shared
// ones, and nef.destroy does not process text.
func (c *fc) takeArg(slot ir.Val, src ast.Expr) {
	if _, isSlot := slot.(ir.L); !isSlot || src == nil {
		return
	}
	t, ok := c.l.f.Types[src]
	if !ok || !reclaimable(t) {
		return
	}
	if !c.fresh(src, t) && !c.takenFromCell(src, t) && t.Kind != check.Struct {
		return
	}
	d, err := c.l.descOf(t)
	if err != nil {
		return
	}
	o := own{slot: slot, desc: d, direct: t.Kind == check.Str, closer: closerOf(t)}
	c.owns = append(c.owns, o)
	c.held = append(c.held, o)
}

// print 把 console.print 摊开：每个值按类型挑例程，值之间一个空格，末尾一个换行。
//
// toErr 是真的那一条 `console.print_error`：它印进标准错误那一个流。
// 去向不是一次性摆在开头，而是每写一段之前都摆一次——实参里再套一个打印，
// 里面那一句会把方向改成自己的，回来时外面这一句还剩几段没写。
//
// en: print expands console.print: each value picks its routine by type, values separated by
// one space, a newline at the end. toErr is the real console.print_error: it writes into the
// standard-error stream. The destination is not set once at the start but re-set
// before every piece written - if an argument contains another print, that inner call switches
// the destination to its own, and the outer sentence still has pieces left to write when it
// returns.
func (c *fc) print(list []ast.Expr, toErr bool) error {
	flag := int64(0)
	if toErr {
		flag = 1
	}
	sink := func(fn string, args []ir.Val, ft []ir.T) {
		c.f.Emit(&ir.Call{Fn: "@out_err", Args: []ir.Val{ir.K(flag)}, FT: []ir.T{ir.TInt}})
		c.f.Emit(&ir.Call{Fn: fn, Args: args, FT: ft})
	}
	for i, a := range list {
		if i > 0 {
			sink("@put_space", nil, nil)
		}
		v, t, err := c.expr(a)
		if err != nil {
			return err
		}
		if printable(t) {
			// 容器、盒子、struct、class 走打印机：它们要看的形状在运行期才说得清。
			txt, err := c.printVal(v, t)
			if err != nil {
				return err
			}
			sink("@put_str", []ir.Val{txt}, []ir.T{ir.TInt})
			// 打印机返回的那一整段是这一步现造的：字已经写完在屏幕上，这一块没别人拿着
			c.f.Emit(&ir.Call{Fn: "@free", Args: []ir.Val{txt}, FT: []ir.T{ir.TInt}})
			continue
		}
		routine, k := printRoutine(t)
		if routine == "" {
			return fmt.Errorf("无法打印 %s：运行期还没有对应的例程", t)
		}
		if t.Kind == check.Str {
			// 打印一个空文本不拒绝：那个位置给 `null`，与 class、容器同一种形状（空句柄印 null 的既定答复）。
			// @put_str 读的是块里那两个槽，所以空句柄不能直接交给它——在这儿换成像样的三个字。
			txt := c.f.Temp()
			isEmpty, done := c.f.Fresh(), c.f.Fresh()
			c.f.Emit(&ir.Jz{V: v, To: isEmpty})
			c.f.Emit(&ir.Mov{D: txt, S: v, T: ir.TInt})
			c.f.Emit(&ir.Jump{To: done})
			c.f.Emit(&ir.Lbl{At: isEmpty})
			c.f.Emit(&ir.Mov{D: txt, S: c.l.msg(pNull), T: ir.TInt})
			c.f.Emit(&ir.Lbl{At: done})
			sink("@put_str", []ir.Val{txt}, []ir.T{ir.TInt})
			continue
		}
		arg := v
		if k == "bits" {
			// 小数量按位型交给例程：槽里躺的就是那 64 位，按整数搬过去，不碰 xmm
			tmp := c.f.Temp()
			c.f.Emit(&ir.Mov{D: tmp, S: v, T: ir.TInt})
			arg = tmp
		}
		sink(routine, []ir.Val{arg}, []ir.T{ir.TInt})
	}
	sink("@put_nl", nil, nil)
	return nil
}

func printRoutine(t check.Type) (string, string) {
	switch t.Kind {
	case check.Int, check.Byte, check.Bool, check.Enum:
		if t.Kind == check.Bool {
			return "@put_bool", ""
		}
		return "@put_int", ""
	case check.Unicode:
		return "@put_char", ""
	case check.Str:
		return "@put_str", ""
	case check.Float:
		return "@put_float", "bits"
	}
	return "", ""
}

// numToString 落 `number.to_string(value)`：基础类型走那四条把值变成文本的例程，
// `struct`／`class`／容器／盒子交给打印机（print.go）。
//
// 与 console.print 用的是同一批算法（intChars、putFloat，以及同一条 cat 的路），
// 所以"印出来的"与"转成文本的"是同一串字——小数照样最短往返、照样必带小数点，
// list与盒子两边印的形状也一模一样。
//
// 小数量按位型交出去：槽里存的就是那 64 位，按整数搬过去，不碰 xmm。
// 文本自己不接（接进来只会白拷一份）。
//
// en: numToString lowers `number.to_string(value)`: the basic types take the four
// value-to-text routines, while struct/class/container/box go to the printer (print.go). It
// uses the same algorithms as console.print (intChars, putFloat, and the same cat path), so
// "what gets printed" and "what gets converted to text" are the same characters - a float still
// takes shortest roundtrip and still always carries a decimal point, and a list or box prints
// the same shape on both sides. The float travels as its bit pattern: those 64 bits are what
// lie in the slot, moved as an integer, never touching xmm. Text does not accept it (
// passing a string in would only copy it uselessly).
func (c *fc) numToString(x *ast.Call, t check.Type) (ir.Val, check.Type, error) {
	v, vt, err := c.expr(x.Args[0])
	if err != nil {
		return nil, t, err
	}
	if printable(vt) {
		txt, err := c.printVal(v, vt)
		if err != nil {
			return nil, t, err
		}
		return txt, t, nil
	}
	fn := ""
	switch vt.Kind {
	case check.Int, check.Byte, check.Enum:
		fn = "@int_str"
	case check.Bool:
		fn = "@bool_str"
	case check.Unicode:
		fn = "@char_str"
	case check.Float:
		tmp := c.f.Temp()
		c.f.Emit(&ir.Mov{D: tmp, S: v, T: ir.TInt})
		v = tmp
		fn = "@float_str"
	default:
		// 两种话都归前端管：文本进不来是语言规则（`number.to_string` 不接 string，检查就报），
		// 剩下的种类上面几支已经收全——走到这里就是前端与后端对不上号
		switch vt.Kind {
		case check.Str:
			return nil, t, fmt.Errorf("number.to_string 接文本：前端拦下的写法走到了后端")
		default:
			return nil, t, fmt.Errorf("number.to_string 接 %s：前端没拦的写法走到了后端", vt)
		}
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: []ir.Val{v}, FT: []ir.T{ir.TInt}, RT: ir.TInt})
	return d, t, nil
}

// printable 说这一类值要交给打印机（print.go 那个用中间码写的 nef.print）：
// 它们得在运行期看形状——list里有什么、盒子里装的是哪个类型，编译期都不知道。
// `struct` 虽然全是静态字段，也一并走打印机：这样"印到屏幕上的"与"转成文本的"、
// 容器里的与外面套着的，全都出自同一支代码，不会两处各长一个样。
//
// en: printable says this class of value goes to the printer (nef.print, written in
// intermediate code in print.go): it needs its shape at runtime - what a list holds and which
// type a box carries are both unknown at compile time. A `struct`, though entirely static
// fields, also goes to the printer, so that "printed to screen" and "converted to text", and
// the one inside a container and the one outside it, all come from one piece of code and cannot
// grow two shapes.
func printable(t check.Type) bool {
	switch t.Kind {
	case check.List, check.Dict, check.Set, check.Obj, check.Struct, check.Class, check.Json, check.Bytes, check.Socket, check.Listener, check.Process:
		return true
	}
	return false
}

// printVal 返回那一个值打印成的一段文本（新造的一份，动不了原来那块）。
//
// 小数量先取位型：槽里躺的就是那 64 位，按整数搬过去，打印机里那一支 @float_str 自己搬回去。
//
// en: printVal returns the text a value prints to (a freshly made copy; the original block is
// untouched). A float's bits are taken first: those 64 bits are what lie in the slot, moved as
// an integer, and the printer's own @float_str moves them back.
func (c *fc) printVal(v ir.Val, t check.Type) (ir.Val, error) {
	d, err := c.l.descOf(t)
	if err != nil {
		return nil, err
	}
	arg := v
	if t.Kind == check.Float {
		arg = c.bitsOf(v, t)
	}
	out := c.f.Temp()
	c.f.Emit(&ir.Call{D: out, HasD: true, Fn: printFn, Args: []ir.Val{arg, ir.D(d)},
		FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	return out, nil
}

// mathAction 落数学组那七条：absolute_value、floor、ceiling、round、minimum、maximum、power。
//
// 小数这一支一律先取位型再交给运行期（那条 movq 与别的调用同一搬法），要拿回一个小数量
// 再搬回去——运行期那几条不碰 `ucomisd`（这台机器一执行就当非法指令），minimum／maximum 的
// 方向问的是 @f_order 那个整数位型掩码。
//
// `math.random` 也在这一张表里（种子取系统里的高精度计数器，不另开"设种子"那个开关；
// 空范围报数值组的兜底 10020 号），两个入参都是整数，所以走 call2 这一步。
//
// en: mathAction lowers the seven math members: absolute_value, floor, ceiling, round, minimum,
// maximum, power. The float side always takes the bit pattern before handing to the runtime
// (that movq moves like any other call) and takes a float back afterwards - the runtime
// routines never touch `ucomisd` (this machine faults it as an illegal instruction), and
// minimum/maximum ask @f_order's integer bit-pattern mask for direction. `math.random` is in
// this table too (the seed comes from the system's high-resolution counter, with no
// separate "set seed" switch; an empty range reports the numeric group's fallback code 10020);
// both parameters are integers, so it goes through call2.
func (c *fc) mathAction(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	// bits 把一个小数量返回它的 64 位；back 是反方向。
	bits := func(v ir.Val) ir.Val {
		d := c.f.Temp()
		c.f.Emit(&ir.Mov{D: d, S: v, T: ir.TInt})
		return d
	}
	back := func(v ir.Val) ir.Val {
		d := c.f.Temp()
		c.f.Emit(&ir.Mov{D: d, S: v, T: ir.TFlt})
		return d
	}
	call1 := func(fn string, v ir.Val) ir.Val {
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: []ir.Val{v}, FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d
	}
	call2 := func(fn string, a, b ir.Val) ir.Val {
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: []ir.Val{a, b},
			FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		return d
	}
	// 那两条不收参数的（π 与小数随机）要在动实参之前答：x.Args[0] 在这种落点上没有东西。
	switch ref.Std {
	case "math.pi", "math.random_float":
		fn := "@f_pi"
		if ref.Std == "math.random_float" {
			fn = "@f_random_float"
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, RT: ir.TInt})
		return back(d), t, nil
	}
	v0, t0, err := c.expr(x.Args[0])
	if err != nil {
		return nil, t, err
	}
	switch ref.Std {
	case "math.absolute_value":
		if t0.Kind == check.Float {
			return back(call1("@f_abs", bits(v0))), t, nil
		}
		return call1("@i_abs", v0), t, nil
	case "math.floor":
		return call1("@math_floor", bits(v0)), t, nil
	case "math.ceiling":
		return call1("@math_ceil", bits(v0)), t, nil
	case "math.round":
		return call1("@math_round", bits(v0)), t, nil
	case "math.minimum", "math.maximum":
		v1, _, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		fn := "@i_min"
		if ref.Std == "math.maximum" {
			fn = "@i_max"
		}
		if t0.Kind == check.Float {
			fn = "@f_min"
			if ref.Std == "math.maximum" {
				fn = "@f_max"
			}
			return back(call2(fn, bits(v0), bits(v1))), t, nil
		}
		return call2(fn, v0, v1), t, nil
	case "math.power":
		v1, _, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		return call2("@i_pow", v0, v1), t, nil
	case "math.square_root":
		return back(call1("@f_sqrt", bits(v0))), t, nil
	case "math.sine":
		return back(call1("@f_sine", bits(v0))), t, nil
	case "math.cosine":
		return back(call1("@f_cosine", bits(v0))), t, nil
	case "math.natural_log":
		return back(call1("@f_natural_log", bits(v0))), t, nil
	case "math.tangent":
		return back(call1("@f_tangent", bits(v0))), t, nil
	case "math.arctangent":
		return back(call1("@f_arctangent", bits(v0))), t, nil
	case "math.log_base_10":
		return back(call1("@f_log_base_10", bits(v0))), t, nil
	case "math.exponential":
		return back(call1("@f_exponential", bits(v0))), t, nil
	case "math.sign":
		// 三档答复只有整数那一个形状，所以小数这一侧返回的也是整数
		if t0.Kind == check.Float {
			return call1("@f_sign", bits(v0)), t, nil
		}
		return call1("@i_sign", v0), t, nil
	case "math.set_seed":
		// 只改运行期那个位置状态，不返回东西
		c.f.Emit(&ir.Call{Fn: "@set_seed", Args: []ir.Val{v0}, FT: []ir.T{ir.TInt}})
		return nil, t, nil
	case "math.random":
		v1, _, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		return call2("@i_random", v0, v1), t, nil
	}
	return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
}

// osAction 落 path 组、environment 组、file 组往外写与目录那几条、time.sleep 与 regular_expression 那一组。
//
// 它们全是**点名式**：参数都在实参表里，点号左边的 `path`／`environment`／`file`／`regular_expression` 是组名不是值
// （与 math 那几条同一个道理，所以也必须在动实参之前先从名字表里认出来）。
// 入参一律是文本，取用之前先查一次 10092 号——`environment.get(null)` 与 `null.length()`
// 是同一种错，不该到系统调用或匹配机里头去撞。
// `environment.set`／`file.write`／`file.append`／`file.remove` 没有返回值，那几条不发目的格。
// `file.exists` 问的就是"在不在"，与 `path.exists` 同一个答复，所以发的是同一条 @path_exists。
//
// en: osAction lowers path, environment, the write-and-directory members of
// file, time.sleep and the regular_expression group. They are all NAMED-FUNCTION
// style: every parameter is in the argument list and the
// `path`/`environment`/`file`/`regular_expression` left of the dot is a group name, not a value
// (same reasoning as the math members, so they too must be recognised from the name table
// before any argument is touched). Parameters are always text and get a 10092 guard before use
// - `environment.get(null)` and `null.length()` are the same mistake and should not have to
// meet it inside a system call or the matcher. environment.set, file.write, file.append and
// file.remove return nothing, so those emit no destination slot. file.exists asks simply "is it
// there" and answers like path.exists, so it emits the same @path_exists.
func (c *fc) osAction(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	fn := map[string]string{
		"path.join":                         "@path_join",
		"path.file_name":                    "@path_file_name",
		"path.extension":                    "@path_extension",
		"path.exists":                       "@path_exists",
		"file.exists":                       "@path_exists",
		"environment.get":                   "@env_get",
		"environment.set":                   "@env_set",
		"environment.unset":                 "@env_unset",
		"environment.current_directory":     "@env_cwd",
		"file.write":                        "@file_write",
		"file.append":                       "@file_append",
		"file.remove":                       "@file_remove",
		"file.size":                         "@file_size",
		"file.modified":                     "@file_modified",
		"file.readonly":                     "@file_readonly",
		"file.list":                         "@file_list",
		"file.list_paths":                   "@file_list",
		"file.read_from":                    "@file_read_from",
		"file.write_at":                     "@file_write_at",
		"file.read_bytes":                   "@file_read_bytes",
		"file.write_bytes":                  "@file_write_bytes",
		"file.create_directory":             "@file_mkdir",
		"file.is_directory":                 "@file_is_dir",
		"file.rename":                       "@file_rename",
		"file.remove_directory":             "@file_rmdir",
		"file.copy":                         "@file_copy",
		"file.create_directories":           "@file_mkdirs",
		"file.walk_paths":                   "@file_walk",
		"file.remove_tree":                  "@file_remove_tree",
		"path.directory_name":               "@path_dir",
		"path.is_absolute":                  "@path_abs",
		"environment.set_current_directory": "@env_chdir",
		"time.utc_offset":                   "@tz_offset",
		"time.sleep":                        "@sleep",
		"regular_expression.match":          "@regex_match",
		"regular_expression.find":           "@regex_find",
		"regular_expression.replace":        "@regex_replace",
		"regular_expression.split":          "@regex_split",
	}[ref.Std]
	if fn == "" {
		return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
	}
	// 目录那两条是同一条例程的两个答复：要名字还是要全路径，靠尾巴上那一个标志格分。
	// 从前字节读那一条也靠尾巴上一个位置——造 `list<byte>` 要的类型字只有降级这一侧排得出号；
	// 换成 `bytes` 之后形状只有一种，那个位置不必再递。
	var mark ir.Val
	switch ref.Std {
	case "file.list":
		mark = ir.K(0)
	case "file.list_paths":
		mark = ir.K(1)
	}
	var args []ir.Val
	for _, e := range x.Args {
		v, _, err := c.expr(e)
		if err != nil {
			return nil, t, err
		}
		if v == nil {
			return nil, t, fmt.Errorf("这里需要一个值，但 %s 不返回任何东西", describe(e))
		}
		// 挡空只挡引用位置上的值：`file.read_from(p, 0, 4096)` 里那个 0 是位置不是空句柄。
		if et, ok := c.l.f.Types[e]; ok && !et.ByValue() {
			c.guardNotNull(v)
		}
		args = append(args, v)
	}
	if mark != nil {
		args = append(args, mark)
	}
	fts := make([]ir.T, len(args))
	for i := range fts {
		fts[i] = ir.TInt
	}
	if t.Kind == check.NoRet {
		c.f.Emit(&ir.Call{Fn: fn, Args: args, FT: fts})
		return nil, t, nil
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: args, FT: fts, RT: ir.TInt})
	return d, t, nil
}

// netAction 落挂在组名上的那三条返回句柄的：`net.connect(地址, 端口, 超时毫秒)`、
// `net.listen(端口)` 与 `process.start(那一串命令行)`。
// 三条返回的都是操作系统给的那一类句柄，不是堆上的一块，所以登记持有者那一步走的是
// `claim`（`closer` 那个位置），到作用域尽头发的是它自己那一条收尾。
//
// 实参一律按只读借用算：地址那一段文本与那一串命令行都由运行期翻成宽字符读一次。
//
// en: netAction lowers the three group functions that return a handle: `net.connect(address,
// port, timeout_ms)`, `net.listen(port)` and `process.start(command line)`.
// All three return an operating-system handle rather than a heap block, so holder registration
// goes through `claim` (the closer field) and at the end of scope each emits its own teardown.
// Arguments are all read-only borrows: the address text
// and the command line are each converted to wide characters once by the runtime.
func (c *fc) netAction(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	fn := "@net_connect"
	switch ref.Std {
	case "net.listen":
		fn = "@net_listen"
	case "process.start":
		fn = "@proc_start"
	}
	var args []ir.Val
	for _, e := range x.Args {
		v, vt, err := c.expr(e)
		if err != nil {
			return nil, t, err
		}
		if v == nil {
			return nil, t, fmt.Errorf("这里需要一个值，但 %s 不返回任何东西", describe(e))
		}
		if !vt.ByValue() {
			c.guardNotNull(v)
		}
		args = append(args, v)
	}
	fts := make([]ir.T, len(args))
	for i := range fts {
		fts[i] = ir.TInt
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: args, FT: fts, RT: ir.TInt})
	return d, t, nil
}

// httpAction 落那两条：`http.get(网址, 超时毫秒)` 与 `http.post(网址, 字节块, 超时毫秒)`。
//
// 一次请求由运行期那五条排成一条直路（那张表写在 `internal/runtime/http.go` 头上）。顺序是有
// 讲究的：`@http_begin` 排第一，问系统那七步全在这一条里——它不成的话那块 `Response` 还没造，
// 抛出去时手上没有登记着的块。成了才 `@alloc` 那一块（尺寸与类型字只降级这头知道，同
// `time.calendar` 那一手），后面三条各自把问到的那个位置写进它：列表与字节块都在运行期那一条里
// 另起一块、直接落进字段，这一侧因此没有中间值要登记，到句尽头该拆的是那一块，按
// 形状表递归（`headers` 与 `body` 两格归它）。
//
// GET 那一条第二个位置交 0：运行期按"没有正文"办，动词也就成 GET。
// 实参一律按只读借用算：网址与正文系统都只读一次。
//
// en: httpAction lowers the two members: `http.get(url, timeout_ms)` and `http.post(url,
// bytes, timeout_ms)`. One request is laid out as a straight line by five runtime routines
// (that table is at the top of internal/runtime/http.go) and the order is deliberate:
// @http_begin goes first and asks the system all seven steps inside it - if it fails, the
// `Response` block has not been created yet, so nothing registered is in hand when the throw
// leaves. Only after success does @alloc take the block (only lowering knows its size and type
// word, the same trick as time.calendar), and the following three each write what they learned
// into their own field. The header list and the body are allocated inside those runtime
// routines and land directly in the fields, so this side has no intermediate value to register;
// at the end of the sentence the block itself is what gets dismantled, by recursion over
// the shape table (the headers and body two slots belong to it). The GET
// form passes 0 in the second position: the runtime treats it as "no body" and the verb becomes
// GET. Arguments are all read-only borrows: the URL and the body are each read once by
// the system.
func (c *fc) httpAction(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	url, ut, err := c.expr(x.Args[0])
	if err != nil {
		return nil, t, err
	}
	if !ut.ByValue() {
		c.guardNotNull(url)
	}
	var send ir.Val = ir.K(0)
	msAt := 1
	if ref.Std == "http.post" {
		v, vt, err := c.expr(x.Args[1])
		if err != nil {
			return nil, t, err
		}
		if !vt.ByValue() {
			c.guardNotNull(v)
		}
		send = v
		msAt = 2
	}
	ms, _, err := c.expr(x.Args[msAt])
	if err != nil {
		return nil, t, err
	}
	tag, ok := c.l.tagOf(t)
	if !ok {
		return nil, t, fmt.Errorf("%s 的类型字没排上", t)
	}
	c.f.Emit(&ir.Call{Fn: "@http_begin", Args: []ir.Val{url, send, ms},
		FT: []ir.T{ir.TInt, ir.TInt, ir.TInt}})
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@alloc",
		Args: []ir.Val{ir.K(c.l.f.BlockSize(t)), ir.K(tag)},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	for _, fn := range []string{"@http_status", "@http_headers", "@http_body"} {
		c.f.Emit(&ir.Call{Fn: fn, Args: []ir.Val{d}, FT: []ir.T{ir.TInt}})
	}
	c.f.Emit(&ir.Call{Fn: "@http_close"})
	return d, t, nil
}

// textFunc 落挂在组名上的那七条转换：带进制的读与写、字节表那一双、base64 那一双、
// JSON 转义那一条，与把 base64 读成 `bytes` 的那一条。
//
// 参数一律按只读借用算（标准库不收留值），返回来的那一份是新造的——
// 读的那几条从传进去的文本或字节表里各造一个新对象，写的那几条造一段新文本，
// 传进来的那一个句柄一个位置都不动。
//
// en: textFunc lowers the seven conversions hanging on the group name: reading and writing with
// a radix, the byte-table pair, the base64 pair, the JSON escaping one, and the one that reads
// base64 into `bytes`. Parameters are all read-only borrows (the standard library keeps
// no value) and what comes back is freshly made - the reading members create a new object from
// the text or byte table passed in, the writing members create new text, and the handle passed
// in is not touched in any position.
func (c *fc) textFunc(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	fn := map[string]string{
		"text.to_int_in_base":      "@str_int_base",
		"number.to_string_in_base": "@num_str_base",
		"text.from_bytes":          "@bytes_str",
		"text.to_base64":           "@b64_encode",
		"text.from_base64":         "@b64_decode",
		"text.escape_json":         "@str_json_escape",
		"bytes.from_base64":        "@bytes_b64_decode",
	}[ref.Std]
	if fn == "" {
		return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
	}
	var args []ir.Val
	for _, e := range x.Args {
		v, vt, err := c.expr(e)
		if err != nil {
			return nil, t, err
		}
		if v == nil {
			return nil, t, fmt.Errorf("这里需要一个值，但 %s 不返回任何东西", describe(e))
		}
		if !vt.ByValue() {
			c.guardNotNull(v)
		}
		args = append(args, v)
	}
	fts := make([]ir.T, len(args))
	for i := range fts {
		fts[i] = ir.TInt
	}
	d := c.f.Temp()
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: args, FT: fts, RT: ir.TInt})
	return d, t, nil
}

// stdAction 降挂在文本与list上的那批动作：大小写、去两头、找、换掉、按码点切一段、
// 拆成码点list、把list跨成串。无法识别的组名（`math.*`、`path.*`、`file` 的写、`regular_expression.*`）
// 在入口处按名拒绝并报"这一版还没做到它"，不发半截产物——先看表再动实参，是因为
// `math.absolute_value(-7)` 那一种点号左边是组名不是值，拿它当接收者降一次，报错就缺了主语。
//
// 成员式（`s.trim()`）的接收者在点号左边，实参表里不含它；点名式（`text.join(list, 分隔)`）
// 两个都在实参表里。两边都是**只读借用**，所以这里不 copyVal：返回的是新造的一块，
// 原来那一块动不了，也不需要为了"怕被改"而先拷一份。
// 借来的文本可能是空的，取用之前一律先做一次 10092 号空句柄检查；`s.slice(1, 3)` 那两个下标是整数，
// 不在这条规则内，所以哪些位置是文本按位置记一张表，而不是在循环里推测类型。
//
// en: stdAction lowers the actions hanging on text and list: case, trimming, searching,
// replacing, slicing by code point, splitting into a code point list, joining a list into text.
// An unrecognised group name (`math.*`, `path.*`, file's writes, `regular_expression.*`) is
// rejected by name at the entry with "this version cannot do it yet" and no half-finished
// product is emitted - the table is consulted before the arguments are touched because in
// `math.absolute_value(-7)` the left of the dot is a group name, not a value, and lowering it
// as a receiver would produce an error message with no subject. Member style (`s.trim()`)
// keeps its receiver left of the dot and out of the argument list; named style
// (`text.join(list, sep)`) keeps both in the list. Both are READ-ONLY BORROWS, so copyVal is
// not called here: what returns is a freshly made block, the original is untouched, and there
// is nothing to protect by copying first. A borrowed text may be empty, so every one of them
// gets a 10092 empty-handle guard before use; the two indices in `s.slice(1, 3)` are integers
// and outside that rule, so which positions are text is recorded in a table by position rather
// than inferred from types inside the loop.
func (c *fc) stdAction(x *ast.Call, ref check.CallRef, t check.Type) (ir.Val, check.Type, error) {
	switch ref.Std {
	case "math.absolute_value", "math.floor", "math.ceiling", "math.round", "math.minimum", "math.maximum", "math.power", "math.square_root", "math.random",
		"math.pi", "math.sine", "math.cosine", "math.natural_log", "math.random_float",
		"math.tangent", "math.arctangent", "math.log_base_10", "math.exponential", "math.sign", "math.set_seed":
		return c.mathAction(x, ref, t)
	case "text.to_int_in_base", "number.to_string_in_base", "text.from_bytes", "text.to_base64", "text.from_base64", "text.escape_json", "bytes.from_base64":
		return c.textFunc(x, ref, t)
	case "time.now", "time.elapsed":
		// 这两条不收参数，返回的一个位置整数、一个位置小数（小数量返回 xmm0，发射器按 RT 识别）
		d := c.f.Temp()
		rt := ir.TInt
		fn := "@time_now"
		if ref.Std == "time.elapsed" {
			rt, fn = ir.TFlt, "@time_elapsed"
		}
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, RT: rt})
		return d, t, nil
	case "time.calendar":
		// 块在这儿造（尺寸与类型字都从内建那张字段表来），七个整数由 @time_parts 按
		// 0、8、…、48 写进去。返回的是新造的一块，所以接住它的名字登记成持有者，
		// 没人接的（`time.calendar(t);` 单独一句）到句尽头拆掉
		sec, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		tag, ok := c.l.tagOf(t)
		if !ok {
			return nil, t, fmt.Errorf("%s 的类型字没排上", t)
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@alloc",
			Args: []ir.Val{ir.K(c.l.f.BlockSize(t)), ir.K(tag)},
			FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
		c.f.Emit(&ir.Call{Fn: "@time_parts", Args: []ir.Val{sec, d}, FT: []ir.T{ir.TInt, ir.TInt}})
		return d, t, nil
	case "net.connect", "net.listen", "process.start":
		// 三条组函数 `net.connect`、`net.listen` 与 `process.start`：入参都是整数与文本，返回的都是操作系统给的那一类句柄。
		// 端口越界与解析不到都由运行期报（10104／10103），起不成那个程序也是（10111），
		// 检查这一侧看不到值。
		return c.netAction(x, ref, t)
	case "http.get", "http.post":
		// 两条组函数 `http.get` 与 `http.post`：一次请求由运行期那五步排成一条直路，返回的是内建那只 `Response`。
		return c.httpAction(x, ref, t)
	case "bytes.new":
		// `bytes.new(容量)` 那一档：造一块连着摆的字节，长度定下之后不再增减。
		// 负的字节数由 @bytes_new 当场抛 10030 号（容器那一段的兜底检查）——那一个数
		// 常来自变量与 `file.size`，检查这一侧看不到，无法在编译期拒绝。
		n, _, err := c.expr(x.Args[0])
		if err != nil {
			return nil, t, err
		}
		d := c.f.Temp()
		c.f.Emit(&ir.Call{D: d, HasD: true, Fn: "@bytes_new", Args: []ir.Val{n},
			FT: []ir.T{ir.TInt}, RT: ir.TInt})
		return d, t, nil
	case "path.join", "path.file_name", "path.extension", "path.exists",
		"environment.get", "environment.set", "environment.unset", "environment.current_directory",
		"file.write", "file.append", "file.exists", "file.remove",
		"file.size", "file.modified", "file.readonly", "file.list", "file.list_paths",
		"file.read_from", "file.write_at",
		"file.read_bytes", "file.write_bytes",
		"file.create_directory", "file.is_directory", "file.rename", "file.remove_directory",
		"file.copy", "file.create_directories",
		"file.walk_paths", "file.remove_tree",
		"path.directory_name", "path.is_absolute",
		"environment.set_current_directory", "time.utc_offset",
		"time.sleep",
		"regular_expression.match", "regular_expression.find", "regular_expression.replace", "regular_expression.split":
		return c.osAction(x, ref, t)
	}
	fn, extra := "", []ir.Val(nil)
	switch ref.Std {
	case "string.to_upper":
		fn, extra = "@str_case", []ir.Val{ir.K(0)}
	case "string.to_lower":
		fn, extra = "@str_case", []ir.Val{ir.K(1)}
	case "string.trim":
		fn = "@str_trim"
	case "string.contains":
		fn = "@str_contains"
	case "string.index_of":
		fn = "@str_index_of"
	case "string.last_index_of":
		fn = "@str_rindex"
	case "string.trim_start":
		fn = "@str_trim_head"
	case "string.trim_end":
		fn = "@str_trim_tail"
	case "string.repeat":
		fn = "@str_repeat"
	case "string.to_bytes":
		// 返回的是 `bytes` 那一块，不再需要一个容器的类型字
		fn = "@str_to_bytes"
	case "string.starts_with":
		fn, extra = "@str_edges", []ir.Val{ir.K(0)}
	case "string.ends_with":
		fn, extra = "@str_edges", []ir.Val{ir.K(1)}
	case "string.pad_start":
		fn, extra = "@str_pad", []ir.Val{ir.K(0)}
	case "string.pad_end":
		fn, extra = "@str_pad", []ir.Val{ir.K(1)}
	case "string.replace":
		fn = "@str_replace"
	case "string.slice":
		fn = "@str_slice"
	case "string.to_list":
		fn, extra = "@str_to_list", []ir.Val{ir.K(tagList)}
	case "text.join":
		fn = "@list_join"
	case "bytes.join":
		// 清单里那几块连着摆成一块新的。收下的只有清单那一个句柄，
		// 块里的内容一律抄走不留（与 `text.join` 同一套处理），所以清单与它的内容照旧归清单自己。
		fn = "@bytes_join"
	default:
		return nil, t, fmt.Errorf("标准库的 %s 还没接到后端", ref.Std)
	}

	textArgs := map[string][]bool{
		"string.contains":      {true},
		"string.index_of":      {true},
		"string.last_index_of": {true},
		"string.repeat":        {false}, // 那个位置是次数，不是文本
		"string.starts_with":   {true},
		"string.ends_with":     {true},
		"string.pad_start":     {false, true}, // 宽度是码点数，填充才是文本
		"string.pad_end":       {false, true},
		"string.replace":       {true, true},
		"string.slice":         {false, false},
		"text.join":            {false, true}, // list那个位置另做判断，分隔是文本
		"bytes.join":           {false},       // 只有清单那一个句柄
	}[ref.Std]

	// val 降一个实参：文本要先挡空句柄，整数不挡
	val := func(e ast.Expr, isText bool) (ir.Val, error) {
		v, _, err := c.expr(e)
		if err != nil {
			return nil, err
		}
		if v == nil {
			return nil, fmt.Errorf("这里需要一个值，但 %s 不返回任何东西", describe(e))
		}
		if isText {
			c.guardNotNull(v)
		}
		return v, nil
	}
	var args []ir.Val
	if ref.Std == "text.join" || ref.Std == "bytes.join" {
		lst, err := val(x.Args[0], false)
		if err != nil {
			return nil, t, err
		}
		c.guardNotNull(lst)
		args = []ir.Val{lst}
		if ref.Std == "text.join" {
			sep, err := val(x.Args[1], textArgs[1])
			if err != nil {
				return nil, t, err
			}
			args = append(args, sep)
		}
	} else {
		m, ok := x.Callee.(*ast.Member)
		if !ok {
			return nil, t, fmt.Errorf("标准库的 %s 该写成点名式，前端与后端对不上号", ref.Std)
		}
		recv, err := val(m.What, true) // 点号左边那一截一定是文本
		if err != nil {
			return nil, t, err
		}
		args = append(args, recv)
		for i, e := range x.Args {
			v, err := val(e, i < len(textArgs) && textArgs[i])
			if err != nil {
				return nil, t, err
			}
			args = append(args, v)
		}
	}
	args = append(args, extra...)
	d := c.f.Temp()
	fts := make([]ir.T, len(args))
	for i := range fts {
		fts[i] = ir.TInt
	}
	c.f.Emit(&ir.Call{D: d, HasD: true, Fn: fn, Args: args, FT: fts, RT: ir.TInt})
	return d, t, nil
}

// ---- 语句 ----

func (c *fc) stmt(s ast.Stmt) error {
	// 检查点排在每一条语句的最前面：这一处是全部语句唯一的入口（函数体、顶层、循环体、
	// 分支、try 的体都从这儿走），所以只这一处插就够了。它不能往后挪一步——
	// ir.IfOverflow 说的是"上一条 add／sub／imul 翻掉了没有"，中间插一条东西就接错地方。
	if c.l.debug {
		at := s.Where()
		c.l.dbgID++
		c.f.Emit(&ir.Dbg{ID: c.l.dbgID, File: c.filePath, Line: at.Line, Col: at.Col})
	}
	switch x := s.(type) {
	case *ast.Decl:
		outer, from := c.openWindow()
		v, vt, err := c.expr(x.Value)
		if err != nil {
			return err
		}
		// struct 是值类型：`let q:P = p` 之后 p 还能用，所以必须另起一块，不能照抄句柄。
		// 初值本来就是"这一句才造出来的那一块"（`Row { ... }` 字面量、`time.calendar(...)`
		// 这类标准库返回的 struct、从容器位置读走时已经拷过一份的那一块）就不用再拷：
		// 除了这个名字没别人拿着，多拷一份反而把原来那一块丢在堆上（实测每圈留 24 与 64 字节）。
		// 判据与 claim 那一条同一个，不用 `argOwnedByCaller`——它把"交出去过的名字"也算成最后一份，
		// 而值类型的名字不管交出过没交出过，读了都还得拷一份。
		copied := false
		if vt.Kind == check.Struct {
			if !c.fresh(x.Value, vt) && !c.takenFromCell(x.Value, vt) {
				v, err = c.copyVal(v, vt)
				copied = true
				if err != nil {
					return err
				}
			}
		}
		// 初值那一句里中间读走的几步在这一趟还掉；最外面那一个（拷过之后的那一个）
		// 留给这个名字，它是新的持有者。
		c.closeWindow(outer, from, v)
		if g, ok := c.l.glob[x]; ok {
			c.f.Emit(&ir.Mov{D: g, S: v, T: c.rtOf(x.Typ)})
			c.takeTop(g, x, vt, copied) // 静态槽那个位置留给程序尽头归还（原因见 takeTop 的说明）
			if c.l.debug {
				c.recordGlobal(g, x, vt)
			}
			return nil
		}
		d := c.f.Temp()
		c.bind(x.NameText, d, dbgOf(vt))
		c.f.Emit(&ir.Mov{D: d, S: v, T: c.rtOf(x.Typ)})
		c.take(d, x, vt, copied)
		return nil

	case *ast.Assign:
		return c.assign(x)

	case *ast.Incdec:
		return c.incdec(x)

	case *ast.ExprStmt:
		outer, from := c.openWindow()
		v, t, err := c.expr(x.E)
		if err != nil {
			return err
		}
		// 这一句里中间那几步读走的，落点都没有名字，上面那一趟按读走的反序还掉。
		// 最外面那一个值要是也从那个位置里读走的，这一趟就顺手把它还了，并且把那个槽抹成 0
		// ——所以下面那句尽头析构走到时看见的是空句柄，不会拆第二遍。
		c.closeWindow(outer, from)
		// 这一句没人接住那个值，到了句尽头就析构。走得到的只有四种——从容器位置读走的那一份、
		// 标准库现造的那一份、拼出来的一串、当场新造的一块；判断复用 `argOwnedByCaller`（与容器
		// `add` 的键、`d.get` 的默认值同一套处理）。光写一个名字的 `s;` 不在内：读名字没有新造出
		// 持有者，那一个名字的主人早在声明那一处登记过了。
		if _, named := x.E.(*ast.Name); !named && c.argOwnedByCaller(x.E, t) {
			return c.releaseIfAny(v, t)
		}
		return nil

	case *ast.If:
		return c.ifStmt(x)

	case *ast.While:
		return c.while(x)

	case *ast.For:
		return c.forStmt(x)

	case *ast.Return:
		if x.Val == nil {
			c.popsTo(0)
			c.leaveFrame(nil)
			c.f.Emit(&ir.Ret{})
			return nil
		}
		outer, from := c.openWindow()
		v, t, err := c.expr(x.Val)
		if err != nil {
			return err
		}
		// `return this.s` 交出去的是对象自己那一块，必须拷；`return p` 那一种其实可以直接交出，
		// 但区分两种情况要全程追踪这个名字的来源，所以先一律拷：多拷不会错，省下来才是要小心出错的地方
		v, err = c.copyVal(v, t)
		if err != nil {
			return err
		}
		// `return xs[0][0]` 的那个 `xs[0]` 是本帧中间读走的一步，出这一帧之前还掉；
		// 返回的那一个位置（拷过之后的那一个）留给调用方。
		c.closeWindow(outer, from, v)
		// 退出清理代码：这一帧到此为止，登记过的都还掉——**除了那一个可能正是交出去的值自己的位置**
		c.popsTo(0)
		c.leaveFrame(c.returnKeep(x.Val, t))
		c.f.Emit(&ir.Ret{D: v, Has: true, T: c.rt(t)})
		return nil

	case *ast.Break:
		if len(c.brk) == 0 {
			return fmt.Errorf("break 不在循环里，检查器本该拦住")
		}
		// 跳出循环要把这一圈块里登记过的还掉：`end` 标签在块尾析构之外，不垫就等于每圈漏一块。
		// 记录本身不动（正常走完那一路的 close() 还要自己发一遍），两条路各走一次不会重还。
		site := c.brk[len(c.brk)-1]
		c.popsTo(site.mark)
		c.unwind(site.mark, nil)
		c.f.Emit(&ir.Jump{To: site.to})
		return nil

	case *ast.Continue:
		if len(c.cont) == 0 {
			return fmt.Errorf("continue 不在循环里，检查器本该拦住")
		}
		// continue 的落点就在本圈圈尾之前，那一圈的析构照样走完——只欠 try 体那几颗处理器记录。
		c.popsTo(c.cont[len(c.cont)-1].mark)
		c.f.Emit(&ir.Jump{To: c.cont[len(c.cont)-1].to})
		return nil

	case *ast.Try:
		return c.tryStmt(x)

	case *ast.Switch:
		return c.switchStmt(x)
	}
	return fmt.Errorf("还不支持这条语句 %T", s)
}

// tryStmt 降 `try { 体 } catch (code, msg) { 接住块 }`。
//
// 进来先挂一层处理器，接住块的开头当参数交给运行期；体正常走完就把这一层摘掉，
// 中间任何一处抛（手写的 throw，或 31／32／33／92／11／21／22／91 那些内建错）
// 都由运行期把栈摆回这一层的样子，直接跳进接住块。
// 码与话躺在 rax／rdx 里，接住块开头那一步把它们落进两个局部量的槽。
//
// en: tryStmt lowers `try { body } catch (code, msg) { handler }`. It first
// installs one handler layer, giving the runtime the start of the catch block as the parameter;
// when the body completes normally that layer is removed, and a throw anywhere inside - a
// hand-written throw or the built-in errors 31/32/33/92/11/21/22/91 - has the runtime rewind
// the stack to this layer and jump straight into the catch block. The code and message lie in
// rax/rdx, and the catch block's first step lands them in the two locals' slots.
func (c *fc) tryStmt(x *ast.Try) error {
	catch := c.f.Fresh()
	after := c.f.Fresh()
	c.f.Emit(&ir.PushHandler{Catch: catch})
	// 本帧在这层 try 体里登记的每一笔都从下标 from 起记在 c.held 上。抛出从被叫的那一帧里
	// 走出来时没人释放它们——那一趟不经过本帧的任何退出清理代码，接住块直接就开始跑了。
	from := len(c.held)
	c.open()
	// 记下这一层：抛出去最多还到这里为止——try 体外面的那些名字，接住块接着还要读。
	c.tries = append(c.tries, len(c.marks)-1)
	for _, s := range x.Body {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.tries = c.tries[:len(c.tries)-1]
	inBody := c.held[from:]
	c.close()
	c.f.Emit(&ir.PopHandler{})
	c.f.Emit(&ir.Jump{To: after})

	c.f.Emit(&ir.Lbl{At: catch})
	c.open()
	code := c.f.Temp()
	msg := c.f.Temp()
	// 码与话要先落进两个局部量：下面那段垫子还是要 call 出去的，rax／rdx 里那两个一过 call 就没了。
	c.f.Emit(&ir.Catch{D: code, E: msg})
	// 接住块开头补还本帧这一层 try 体里登记的那些。同一帧抛出去那一路已经由
	// leaveToHandler 还过，抹了零的位置第二次走到就是空操作，所以这一段重复不花钱也不出错。
	c.destroyAll(inBody)
	top := c.scopes[len(c.scopes)-1]
	top[x.CodeName] = code
	top[x.MsgName] = msg
	for _, s := range x.Catch {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.close()
	c.f.Emit(&ir.Lbl{At: after})
	return nil
}

// switchStmt 降 `switch (判定) { 标签 { } default { } }`。
//
// 发成一条一条的顺序比较：先把所有分支的标签试一遍，谁匹配就跳去谁的体，
// 一个都没匹配上就走 default（没写 default 就走尾巴）。判定值只算一次。
//
// 文本判定按内容比，别的都比那 64 位。贯穿没有：每个体走完直接跳出，
// 语言规则在"改了枚举没人提醒"与"提醒"之间选了前者。
//
// en: switchStmt lowers `switch (value) { labels { } default { } }`. It emits
// sequential comparisons: try every branch's label, jump to the body of whichever matches, and
// with no match take default (or the tail when no default was written). The scrutinee is
// computed only once. Text compares by content, everything else by the full 64 bits.
// There is no fallthrough: every body jumps out when done - the language rule accepts that
// "adding an enum member will not remind you".
func (c *fc) switchStmt(x *ast.Switch) error {
	// 判定与各个标签都是"比完就没人要"的那几块，登记在这一层；等所有分支的体都发完、
	// 走到收尾那个位置之后一起还——比之前就还会把正在比的这一块拆掉。
	swOuter, swFrom := c.openWindow()
	head, ht, err := c.expr(x.Head)
	if err != nil {
		return err
	}
	isStr := ht.Kind == check.Str
	end := c.f.Fresh()
	def := c.f.Fresh()
	// 第一遍只发比较：每个分支的落点标号先记下来，第二发体时要拿同一批标号，
	// 现要会要出新的号，跳过来的那些就成了死号。
	bodies := make([]ir.Label, len(x.Cases))
	for i, cs := range x.Cases {
		if len(cs.Labels) == 0 {
			continue // default 的体排在下面，这里没有标签要试
		}
		body := c.f.Fresh()
		bodies[i] = body
		for _, lab := range cs.Labels {
			lv, lt, err := c.expr(lab)
			if err != nil {
				return err
			}
			if isStr {
				hit := c.f.Temp()
				c.f.Emit(&ir.Call{D: hit, HasD: true, Fn: "@str_eq",
					Args: []ir.Val{head, lv}, FT: []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
				c.f.Emit(&ir.Jnz{V: hit, To: body})
				continue
			}
			c.f.Emit(&ir.Br{Op: "==", A: head, B: lv, To: body, T: c.rt(lt)})
		}
	}
	// 没写 default 的 switch（枚举列全了就不必写）也要有个去处：直接收尾。
	// 这一步不能省，否则那条 jmp 指向一个没人落过的标号，装配当场就报"标号没有落点"。
	swPending := len(c.temps)
	c.borrow = swOuter // 分支的体里各句各开各的窗口，不再挂在这一层
	if !x.HasDefault {
		def = end
	}
	c.f.Emit(&ir.Jump{To: def})
	for i, cs := range x.Cases {
		// default 的落点必须摆在它自己那段体*之前*：摆到后面去就成了"跳过体直接收尾"
		body := def
		if len(cs.Labels) > 0 {
			body = bodies[i]
		}
		c.f.Emit(&ir.Lbl{At: body})
		c.open()
		// 这里不压 brk：break 只属于循环（与检查器同一条语言规则），写在 switch 里
		// 也是跳出外面那圈，不是跳出 switch。
		for _, s := range cs.Body {
			if err := c.stmt(s); err != nil {
				return err
			}
		}
		c.close()
		c.f.Emit(&ir.Jump{To: end})
	}
	c.f.Emit(&ir.Lbl{At: end})
	// 走到这里判定与标签那些块都比完了：按比的反序还掉
	c.freeTempsTo(swFrom, swPending)
	return nil
}

func (c *fc) rtOf(ty ast.Type) ir.T {
	nt, ok := ty.(*ast.NamedType)
	if ok && nt.Name == "float" {
		return ir.TFlt
	}
	return ir.TInt
}

func (c *fc) assign(x *ast.Assign) error {
	outer, from := c.openWindow()
	v, vt, err := c.expr(x.Value)
	if err != nil {
		return err
	}
	// 整份赋值同理：左边那一个名字赋完之后右边还得能用，所以给过去的必须是一块新的。
	// 右边本来就是这一刻才造出来的那一块（`r = Row { ... }`、`xs = [1, 2]`）就不要再拷一次——
	// 拷出来的归这个名字，原来那一块从此没人析构。判据与声明那一侧完全相同。
	if vt.Kind == check.Struct {
		if !c.fresh(x.Value, vt) && !c.takenFromCell(x.Value, vt) {
			v, err = c.copyVal(v, vt)
			if err != nil {
				return err
			}
		}
	} else {
		v, err = c.copyVal(v, vt)
		if err != nil {
			return err
		}
	}
	// 等号右边那一句里中间读走的几步在这一趟还掉，最外面那一个交给左边那个位置。
	c.closeWindow(outer, from, v)
	return c.storeTo(x.Target, v, vt)
}

// storeTo 把算好的一个值写回某处：局部槽、方法体里直接写的字段名、本文件顶层的名字、
// 对象的一个字段、容器的一个位置。`i++` 那一条也走这里——它就是读出来、加一、写回去，
// 两条路共用同一套目标形状，免得其中一条漏了越界或窄字段的处理。
//
// 左边那一条路径上读走的那几步也是这一句的中间步骤：写回走完就没人拿着，按反序还掉。
// 这条语言规则定下之后，走得到的只剩两类——值类型那个位置（`cs[0].n = 1` 的那个 `cs[0]`）与字段
// （`b.kids[0] = 7` 的那个 `b.kids`），它们读出来是地址与句柄，本来就不另起一块。
// 路径里真会多出一块的是 `d[键] = 值` 的那一把键：它住进桶里那个位置，持有者是那个位置，从这本账里挑出去。
//
// en: storeTo writes a computed value back somewhere: a local slot, a field name written bare
// in a method body, a name at this file's top level, an object field, a container position.
// `i++` comes through here too - it is read, add one, write back - and the two paths share one
// destination shape so that neither forgets the range check or the narrow-field handling. The
// read-outs along the left-hand path are intermediate steps of this same sentence: once the
// write-back is done nobody holds them and they are returned in reverse order. Since that
// language rule was settled only two kinds remain reachable - a VALUE-TYPED position (the
// `cs[0]` in `cs[0].n = 1`) and a field (the `b.kids` in `b.kids[0] = 7`), whose reads give an address
// and a handle and never allocate. What genuinely allocates on that path is the KEY in
// `d[key] = value`: it moves into a bucket position whose holder is that position, so it is
// picked out of this ledger.
func (c *fc) storeTo(target ast.Expr, v ir.Val, vt check.Type) error {
	defer c.noBorrow()() // 赋值：名字与位置接过去就是新的持有者，这一头不许当场还
	outer, from := c.openWindow()
	keep, err := c.storeToPath(target, v, vt)
	c.closeWindow(outer, from, keep...)
	return err
}

func (c *fc) storeToPath(target ast.Expr, v ir.Val, vt check.Type) ([]ir.Val, error) {
	switch t := target.(type) {
	case *ast.Name:
		if slot, ok := c.find(t.Text); ok {
			if err := c.freeNameOld(slot, t.Text, v, vt); err != nil {
				return nil, err
			}
			c.f.Emit(&ir.Mov{D: slot, S: v, T: c.rt(vt)})
			return nil, nil
		}
		if off, ok := c.isField(t.Text); ok {
			recv, ok := c.find("this")
			if !ok {
				return nil, fmt.Errorf("%s 是字段，可这个方法体里没有 this：接收者没落到第 0 槽", t.Text)
			}
			if err := c.freeFieldOld(recv, off, v, vt); err != nil {
				return nil, err
			}
			c.f.Emit(&ir.Store{Base: recv, Off: int(off), S: v, T: c.rt(vt), Size: c.l.narrow(vt)})
			return nil, nil
		}
		d, err := c.resolve(t)
		if err != nil {
			return nil, err
		}
		// 顶层那一个名字与本文件那一层作用域里的名字同一本账：登记在册才拆（freeNameOld 那里说的是什么情形）
		if err := c.freeNameOld(d, t.Text, v, vt); err != nil {
			return nil, err
		}
		c.f.Emit(&ir.Mov{D: d, S: v, T: c.rt(vt)})
		return nil, nil
	case *ast.Member:
		base, bt, err := c.placeOf(t.What)
		if err != nil {
			return nil, err
		}
		off, err := c.fieldOffset(bt.Name, t.NameText)
		if err != nil {
			return nil, err
		}
		c.guardNotNull(base)
		if err := c.freeFieldOld(base, off, v, vt); err != nil {
			return nil, err
		}
		c.f.Emit(&ir.Store{Base: base, Off: int(off), S: v, T: c.rt(vt), Size: c.l.narrow(vt)})
		return nil, nil
	case *ast.Index:
		// 往容器的一个位置里写：list 的越界由 @list_at 当场抛（10031 号），与读用的是同一条检查
		base, bt, err := c.expr(t.What)
		if err != nil {
			return nil, err
		}
		at, _, err := c.expr(t.At)
		if err != nil {
			return nil, err
		}
		c.guardNotNull(base)
		if bt.Kind == check.Dict {
			// `d[k] = v` 与 `d.add(k, v)` 是同一条语言规则：没有这个键就是添一条，
			// 所以这里不能走 @map_at——那一条找不到键要抛 10032 号。
			// 键的类型不含小数，所以它不必先转位型。
			if err := c.mapStore(base, at, c.bitsOf(v, vt), true, vt, t.At, bt.Args[0]); err != nil {
				return nil, err
			}
			return []ir.Val{at}, nil
		}
		cell, err := c.cellOf(base, bt, at)
		if err != nil {
			return nil, err
		}
		if reclaimable(vt) {
			// 覆盖就是换持有者：被换下来的那一块当场析构掉。list的位置读出来是
			// **读走**（那个位置之后返回空句柄），所以除了这个位置没人拿着它，拆它安全。
			// 顺带给这个位置记一次"改过次数"：正在遍历这条list的那一圈借的就是旧的那一块。
			old := c.f.Temp()
			c.f.Emit(&ir.Load{D: old, Base: cell, Off: 0, T: ir.TInt})
			if err := c.releaseIfAny(old, vt); err != nil {
				return nil, err
			}
			c.f.Emit(&ir.Call{Fn: "@list_touch", Args: []ir.Val{base}, FT: []ir.T{ir.TInt}})
		}
		c.f.Emit(&ir.Store{Base: cell, Off: 0, S: v, T: c.rt(vt), Size: cellWidth(bt)})
		return nil, nil
	}
	return nil, fmt.Errorf("还不支持往 %s 上赋值", describe(target))
}

// ownerType 是方法所属那个类的类型；普通函数上是空名字，查字段表自然查不到东西。
//
// en: ownerType is the type of the class a method belongs to; an ordinary function has an empty
// name here, so looking up a field table naturally finds nothing.
func (c *fc) ownerType() check.Type {
	return check.Type{Kind: check.Class, Name: c.owner}
}

func (c *fc) ifStmt(x *ast.If) error {
	ends := make([]ir.Label, len(x.Conds))
	for i, cond := range x.Conds {
		// 条件那一个布尔值没人接住，中间读走的那几步也算完就当还
		ifOuter, ifFrom := c.openWindow()
		v, _, err := c.expr(cond)
		if err != nil {
			return err
		}
		c.closeWindow(ifOuter, ifFrom)
		next := c.f.Fresh()
		c.f.Emit(&ir.Jz{V: v, To: next})
		c.open()
		for _, s := range x.Bodies[i] {
			if err := c.stmt(s); err != nil {
				return err
			}
		}
		c.close()
		end := c.f.Fresh()
		ends[i] = end
		c.f.Emit(&ir.Jump{To: end})
		c.f.Emit(&ir.Lbl{At: next})
	}
	if x.HasElse {
		c.open()
		for _, s := range x.Else {
			if err := c.stmt(s); err != nil {
				return err
			}
		}
		c.close()
	}
	for _, e := range ends {
		c.f.Emit(&ir.Lbl{At: e})
	}
	return nil
}

func (c *fc) while(x *ast.While) error {
	top := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Lbl{At: top})
	wOuter, wFrom := c.openWindow()
	v, _, err := c.expr(x.Cond)
	if err != nil {
		return err
	}
	// 条件这一句没有接住值的人：中间读走的那几步算完就当还，比较本身只看得见最外面那一个
	c.closeWindow(wOuter, wFrom)
	c.f.Emit(&ir.Jz{V: v, To: end})
	c.open()
	// continue 跳的不是条件那一侧，而是本圈的块尾：先走完这一圈登记的析构，再回到条件。
	next := c.f.Fresh()
	c.brk = append(c.brk, exitSite{to: end, mark: len(c.marks) - 1})
	c.cont = append(c.cont, exitSite{to: next, mark: len(c.marks) - 1})
	for _, s := range x.Body {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.brk = c.brk[:len(c.brk)-1]
	c.cont = c.cont[:len(c.cont)-1]
	c.f.Emit(&ir.Lbl{At: next})
	c.close()
	c.f.Emit(&ir.Jump{To: top})
	c.f.Emit(&ir.Lbl{At: end})
	return nil
}

// forStmt 只接计数循环：`for (i:int in a until b)`。
// 遍历容器要等容器实现，文本遍历要等下标实现。
//
// en: forStmt accepts only the counting loop `for (i:int in a until b)`. Iterating a container
// waits for the container implementation, and iterating text waits for subscripting.
func (c *fc) forStmt(x *ast.For) error {
	if x.Ranged {
		// 遍历的那一条要活到循环走完，所以这一段登记先压着：窗口一开就关掉 `borrow`，
		// 循环体里每一句各开各的窗口，走到 `end` 之后（break 也落在同一个位置后面）再一起还。
		outer, from := c.openWindow()
		iter, it, err := c.expr(x.Iter)
		if err != nil {
			return err
		}
		pending := len(c.temps)
		c.borrow = outer
		c.guardNotNull(iter)
		var runErr error
		switch it.Kind {
		case check.List:
			runErr = c.forList(x, iter, it)
		case check.Dict, check.Set:
			runErr = c.forMap(x, iter, it)
		case check.Str:
			runErr = c.forString(x, iter)
		default:
			runErr = fmt.Errorf("遍历 %s 还没接进后端", it)
		}
		c.freeTempsTo(from, pending)
		return runErr
	}
	fromOuter, fromFrom := c.openWindow()
	from, _, err := c.expr(x.From)
	if err != nil {
		return err
	}
	until, _, err := c.expr(x.Until)
	if err != nil {
		return err
	}
	c.closeWindow(fromOuter, fromFrom)
	c.open()
	i := c.f.Temp()
	c.bind(x.Var.NameText, i, dbgOf(check.Type{Kind: check.Int, Name: "int"}))
	c.f.Emit(&ir.Mov{D: i, S: from, T: ir.TInt})
	top := c.f.Fresh()
	end := c.f.Fresh()
	c.f.Emit(&ir.Lbl{At: top})
	c.f.Emit(&ir.Br{Op: ">=", A: i, B: until, To: end, T: ir.TInt})
	c.brk = append(c.brk, exitSite{to: end, mark: len(c.marks) - 1})
	next := c.f.Fresh()
	c.cont = append(c.cont, exitSite{to: next, mark: len(c.marks) - 1})
	for _, s := range x.Body {
		if err := c.stmt(s); err != nil {
			return err
		}
	}
	c.brk = c.brk[:len(c.brk)-1]
	c.cont = c.cont[:len(c.cont)-1]
	c.f.Emit(&ir.Lbl{At: next})
	c.close() // 圈里登记的局部量每圈还一次：`next` 先发，continue 跳过来也走得到
	c.f.Emit(&ir.Bin{Op: "+", D: i, A: i, B: ir.K(1), T: ir.TInt})
	c.f.Emit(&ir.Jump{To: top})
	c.f.Emit(&ir.Lbl{At: end})
	return nil
}

// throwLike 是 throw 与 exit 共同的形状：一个码、一句话，交给一个不返回的例程。
// 两者能不能被接住要等处理器帧链才分得开，但退出码的语义现在就不同，所以例程分开。
//
// 退出清理代码分两种：`throw` 有可能被本帧的某一个 try 接住，只还到那一层；`exit` 不给任何人接的机会，
// 整帧都释放掉，所以连本文件顶层那一层也要释放——语言规则规定"已经建好的值照常释放"。
//
// en: throwLike is the shared shape of throw and exit: one code, one message, handed to a
// routine that never returns. Whether the two can be caught only becomes distinguishable once
// handler frames exist, but the exit-code semantics differ right now, so the routines are
// separate. Exit cleanup comes in two flavours: a `throw` may be caught by a try in this frame,
// so it unwinds only to that layer; `exit` gives nobody the chance and tears down the entire
// frame, including this file's top-level layer - the language rule states that "values
// already built are released as usual".
func (c *fc) throwLike(codeExpr, msgExpr ast.Expr, routine string) error {
	outer, from := c.openWindow()
	code, _, err := c.expr(codeExpr)
	if err != nil {
		return err
	}
	msg, _, err := c.expr(msgExpr)
	if err != nil {
		return err
	}
	// 码与话这两样要交给接住的那一侧（另一个帧里还要读），所以留下；
	// 把它们算出来途中读走的那几步没有别人拿，现在就还。
	c.closeWindow(outer, from, code, msg)
	if routine == "@die" {
		c.popsTo(0)
		c.leaveFrame(nil)
		c.f.Emit(&ir.Call{Fn: releaseTopFn, FT: []ir.T{}})
	} else {
		c.leaveToHandler()
	}
	c.f.Emit(&ir.Call{Fn: routine, Args: []ir.Val{code, msg}, FT: []ir.T{ir.TInt, ir.TInt}})
	return nil
}

// ---- 小工具 ----

func (l *L) msg(s string) ir.S {
	if id, ok := l.msgID[s]; ok {
		return id
	}
	id := l.p.AddString(s)
	l.msgID[s] = id
	return id
}

// hidden 在静态池里占一个位置给后端自己用（打印机那两个槽：身份表的地址、走到第几层）。
// 与顶层 `let` 同一张表，但用户拿不到名字，也就不会被谁的程序读到。
//
// en: hidden claims one static pool slot for the backend's own use (the printer's two slots:
// the identity table's address and how deep it has walked). It shares the table with top-level
// `let` but no user can name it, so no program can read it.
func (l *L) hidden() ir.G {
	g := ir.G(l.gcount)
	l.gcount++
	return g
}

func describe(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Name:
		return x.Text
	case *ast.Member:
		return describe(x.What) + "." + x.NameText
	case *ast.Binary:
		return "(" + x.Op + ")"
	case *ast.Call:
		return "调用"
	case *ast.Convert:
		return describe(x.What) + " as …"
	case *ast.Test:
		return describe(x.What) + " is …"
	case *ast.Index:
		return describe(x.What) + "[…]"
	case *ast.New:
		return "new " + x.Type
	case *ast.StructLit:
		return x.Type + " {…}"
	case *ast.ListLit:
		return "list 字面量"
	case *ast.MapLit:
		return "dict 字面量"
	case *ast.SetLit:
		return "hashset 字面量"
	}
	return fmt.Sprintf("%T", e)
}

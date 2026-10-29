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

// Package check 做静态检查：类型、名字、交出与控制流。
//
// 产出是一串带行列的报错；编译器在生成任何机器码之前先跑完它。
//
// en: Package check performs the static checking of types, names, handover and control flow.
// What it produces is a list of errors carrying line and column, and the compiler runs it to
// completion before generating any machine code.
package check

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/lexer"
)

// Error 是一条静态检查报错。
//
// en: Error is one static-check error.
type Error struct {
	File string
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s 第 %d 行第 %d 列：%s", e.File, e.Line, e.Col, e.Msg)
}

// Kind 是内部用的类型分类。
//
// en: Kind is the internal classification of types.
type Kind int

const (
	ErrT Kind = iota
	Bool
	Int
	Float
	Byte
	Unicode
	Str
	Obj
	Null
	Struct
	Class
	Iface
	Enum
	List
	Dict
	Set
	NoRet
	FuncT // 函数名出现在值位置上——这一版不支持
	// Alias 只出现在 ImportedName 上，表示"引来的这一个名字是一条类型别名"。
	// 别名解出来的一定是上面某一个真类型，所以类型式子里不会带着 Alias 走到后端。
	Alias
	// Json 是内建的引用类型：形状住在编译器发的那几支函数里，
	// 文件里写不出它，也不给字段、不给方法——只能经 json 那一组读。
	Json
	// Bytes 是内建的引用类型：一段连续内存的字节块，1 字节就是 1 字节。
	// 它取代的是 `list<byte>` 那一档方案（`text.to_bytes`／`from_bytes`、`file.read_bytes`／`write_bytes`），
	// 因为那个方案每个槽位 8 字节：实测读 5,607,424 字节的文件要 98,205,840 字节。
	Bytes
	// Socket 与 Listener 是操作系统给的句柄。它们不是堆上的一块：
	// 名字走到头不是把块还进空闲链表，而是调用一次 `closesocket`。不进容器、不装箱、当不了键。
	Socket
	Listener
	// Process 也是操作系统提供的那一类对象：一个子进程有两把句柄，
	// 语言里那一个名字装不下两把，所以手上那个数是运行期那一张表的表项。
	// 名字走到头调用一次 `@proc_close`：没等它结束就把进程终止、句柄关闭。规则与上面两类相同。
	Process
)

func (k Kind) String() string {
	switch k {
	case Bool:
		return "boolean"
	case Int:
		return "int"
	case Float:
		return "float"
	case Byte:
		return "byte"
	case Unicode:
		return "unicode"
	case Str:
		return "string"
	case Obj:
		return "object"
	case Null:
		return "null"
	case List:
		return "list"
	case Dict:
		return "dict"
	case Set:
		return "hashset"
	case Json:
		return "Json"
	case Bytes:
		return "bytes"
	case Socket:
		return "Socket"
	case Listener:
		return "Listener"
	case Process:
		return "Process"
	case NoRet:
		return "无返回值"
	case ErrT:
		return "错误"
	default:
		return "引用类型"
	}
}

// Type 是一个解析过的类型。
//
// en: Type is a resolved type.
type Type struct {
	Kind Kind
	Name string
	Args []Type
}

func (t Type) String() string {
	switch t.Kind {
	case List:
		return "list<" + t.Args[0].String() + ">"
	case Dict:
		return "dict<" + t.Args[0].String() + ", " + t.Args[1].String() + ">"
	case Set:
		return "hashset<" + t.Args[0].String() + ">"
	case Struct, Class, Iface, Enum:
		return bareName(t.Name)
	default:
		if t.Name != "" {
			return bareName(t.Name)
		}
		return t.Kind.String()
	}
}

// Key 是同一个类型在整程序里的**身份**：包里的类型带着它的包根，工程里的类型就是原名。
// String() 输出给读者看，Key() 供机器比较——形状表按它去重，少了这一层，工程里那只 `Pair`
// 与包里的 `Pair` 会挤进同一条形状、同一个类型字。
//
// en: Key is a type's IDENTITY across the whole program: a type from a package carries its
// package root, a type in the project is just its original name. String() is for the reader,
// Key() is for the machine - the shape table deduplicates by it. Without that layer, the
// project's `Pair` and a package's `Pair` would squeeze into one shape and one type word.
func (t Type) Key() string {
	switch t.Kind {
	case List:
		return "list<" + t.Args[0].Key() + ">"
	case Dict:
		return "dict<" + t.Args[0].Key() + ", " + t.Args[1].Key() + ">"
	case Set:
		return "hashset<" + t.Args[0].Key() + ">"
	default:
		if t.Name != "" {
			return t.Name
		}
		return t.Kind.String()
	}
}

// 包限定名用的分隔符。`/` 是路径里的字节，词法上永远进不了一个名字（名字只许字母数字下划线），
// 所以限定名与用户写得出的名字撞不上；报错文本里出现的全是原名——一条报错已经带了文件名与行列，
// 足以说明是哪一个文件的定义。
//
// en: The separator used in a package-qualified name. `/` is a byte that appears in paths and
// can never enter a name lexically (names allow letters, digits and underscore only), so a
// qualified name cannot collide with a name a user can write. Error messages contain only the
// original name - one error already carries the file name, line and column, which identifies
// which file's definition it is.
const pkgSep = "/"

// qualKey 把包根与原名拼成名字表里的键；不在包里（root 为空）就是原名本身，
// 这样工程里那些文件的键与限定名一个字节都没变，原有读取方无需改动。
//
// en: qualKey joins a package root and an original name into a name-table key. Outside a
// package (empty root) it is the original name itself, so the keys of a project's own files do
// not change by one byte and every existing reader keeps working.
func qualKey(root, name string) string {
	if root == "" {
		return name
	}
	return root + pkgSep + name
}

// bareName 交出限定的那一个名字本身。
//
// en: bareName returns the qualified name's own name.
func bareName(key string) string {
	if i := strings.LastIndex(key, pkgSep); i >= 0 {
		return key[i+len(pkgSep):]
	}
	return key
}

// keyOwner 交出这个键属于哪一个包根；工程里的名字返回空。
//
// en: keyOwner returns which package root this key belongs to; a name from the project returns
// empty.
func keyOwner(key string) string {
	i := strings.LastIndex(key, pkgSep)
	if i < 0 {
		return ""
	}
	return key[:i]
}

// QualKey 与 bareName 同一份规矩，交给后端用：降级那一路按名字找函数、找字段表、排类型字，
// 用的键必须与这里一模一样。
//
// en: QualKey and bareName follow the same rule and are handed to the backend: the lowering path
// looks up functions, field tables and type words by name, and the keys it uses must be exactly
// the ones chosen here.
func QualKey(root, name string) string { return qualKey(root, name) }

// BareName 返回限定名里那一个原名，只给用户可见的文字用：报错信息、打印出来的类型名。
//
// en: BareName returns the original name inside a qualified name, for text visible to the
// user: error messages and printed type names.
func BareName(key string) string { return bareName(key) }

// ByValue 判是不是值类型。
//
// en: ByValue decides whether this is one of the value types.
func (t Type) ByValue() bool {
	switch t.Kind {
	case Bool, Int, Float, Byte, Unicode, Struct, Enum:
		return true
	}
	return false
}

// Nullable 判这个类型能不能装 `null`（只有引用类型可取 null）。
// Json 也在列内：json.parse 整段文本解析失败时返回 null。
// bytes 也在列内：它是引用类型，块在堆上，名字没有指向任何对象时就是 null。
// Socket 与 Listener 也在：连接尚未建立、或者 `close()` 之后，那个名字就是空句柄。
// Process 也在：那个进程尚未启动、或者 `close()` 之后，那个名字同样是空句柄。
//
// en: Nullable decides whether this type can hold `null` (only reference types may be null).
// Json is on the list: json.parse returns null when the whole text does not parse. bytes is on
// it: it is a reference type whose block lives on the heap, so a name pointing at nothing is
// null. Socket and Listener are: before a connection completes, or after `close()`, the name is
// an empty handle. Process is: before the program starts, or after `close()`, the name is
// likewise an empty handle.
func (t Type) Nullable() bool {
	switch t.Kind {
	case Str, Obj, Class, Iface, List, Dict, Set, Json, Bytes, Socket, Listener, Process:
		return true
	}
	return false
}

var errType = Type{Kind: ErrT, Name: "错误"}

// Program 是一趟检查的输入。
//
// en: Program is the input of one check pass.
type Program struct {
	Files []*ast.File
	Entry string
	// PkgRefs 把"某一份文件里那一句整块挂名的 import"接到它解析到的包根。
	// 键是 PkgRefKey(文件路径, 引入原文)。读文件那一侧填它，检查这一侧只读。
	PkgRefs map[string]string
	// PkgRoots 说每一份文件属于哪一个包：文件路径 → 包根。不在任何包里的文件不进这张表。
	// 由装载那一路顺着递归填写（包里的文件引的 `./` 文件同属这个包），不依据路径的形状判断。
	PkgRoots map[string]string
	// Facades 记的是每一个包对外的那一份门面文件（路径 → true），也是装载那一路填的。
	// "门面不能交出依赖里的类型"这一条只查这一份文件：包里的其余文件不受这一条限制。
	Facades map[string]bool
	// PkgFacades 把"某一份文件里那一句整块挂名的 import"接到它实际 load 的那个门面文件路径
	// （键同 PkgRefs）。后端排执行顺序用这一张，不依据用户写的大小写自行拼一个 `<包名>.nf`：
	// 拼出来的名字与盘上真正的文件可能不同，那样就找不到文件（路径只解析一次的规则）。
	PkgFacades map[string]string
	// Debug 说这一趟是给调试器编的：降级因此给每一条语句前多留一条检查点（ir.Dbg），
	// 发射那一步同时记下它落在机器码哪一个位置。不开的时候中间码与产物行为一字不变，
	// 所以这个开关放在 Program 上而不是 lower.Run 的参数上——那条签名后面还跟着几百条测试。
	Debug bool
}

// SetPkgRef 记下一句包引入解析到的包根与门面文件：两张表同一条键，一起填。
// 由装载那一侧（`internal/pkgload`）调，检查与后端只读。
//
// en: SetPkgRef records the package root and facade file one package import resolved to: both
// tables share one key and are filled together. It is called by the loading side
// (internal/pkgload); the checker and backend only read.
func (p *Program) SetPkgRef(fromFile, ref, root, facade string) {
	if p.PkgRefs == nil {
		p.PkgRefs = map[string]string{}
	}
	if p.PkgFacades == nil {
		p.PkgFacades = map[string]string{}
	}
	k := PkgRefKey(fromFile, ref)
	p.PkgRefs[k] = root
	p.PkgFacades[k] = facade
}

// PkgFacade 交出那一句包引入实际 load 的门面文件路径。
//
// en: PkgFacade returns the facade path actually loaded for that package import.
func (p *Program) PkgFacade(fromFile, ref string) (string, bool) {
	if p == nil || p.PkgFacades == nil {
		return "", false
	}
	path, ok := p.PkgFacades[PkgRefKey(fromFile, ref)]
	return path, ok
}

// PkgRefKey 是 PkgRefs 的键。用一个不可能出现在路径里的分隔符，免得拼出别人已有的键。
//
// en: PkgRefKey is the key of PkgRefs. It uses a separator that cannot appear in a path, so it
// can never spell out a key somebody already has.
func PkgRefKey(file, ref string) string { return file + "\x00" + ref }

// packageOf 交出这份文件属于哪一个包的根；工程的普通文件返回 ok 为假。
//
// en: packageOf returns the root of the package this file belongs to; an ordinary project file
// returns a false ok.
func (p *Program) packageOf(file string) (string, bool) {
	if p == nil || p.PkgRoots == nil {
		return "", false
	}
	root, ok := p.PkgRoots[file]
	return root, ok
}

// Facts 是后端复用的一份语义结论（前端到中间码那一步）：每个表达式算出什么类型、
// 每个名字指向哪儿、每次调用落到哪个函数。记这一份是为了不用把检查器再写一遍。
//
// en: Facts is the set of semantic conclusions the backend reuses (the front end to
// intermediate code step): what type every expression has, what every name refers to, which
// function every call lands on. Recording it exists so the checker does not have to be written
// twice.
type Facts struct {
	Types    map[ast.Expr]Type
	Binds    map[*ast.Name]Bind
	Calls    map[*ast.Call]CallRef
	EnumVals map[string]int64     // "D.a" → 它的值
	Tests    map[*ast.Test]Type   // `x is T` 里那个 T——结果本身恒为 boolean，被判断的类型要另记
	Funcs    map[string]*ast.Func // 规范名 → 函数体；方法是 "类型名.方法名"
	// Sigs 是每一个函数与方法算好的签名（键与 Funcs 同一种写法：方法是"类型名.方法名"，
	// 包里的带包根）。编辑器那一侧的补全与悬停使用这一张表；标准库那些不在这里，
	// 它们在 stdFuncs 与 stdMember 那两处。
	Sigs map[string]Signature
	// Declared 是这一趟里登记的每一个类型名（含 `type 甲 = 乙` 那种别名）：规范名 → 种类与
	// 有没有 export。编辑器补一个包后面那一段、悬停停在一个类型名上使用这一张表。
	Declared map[string]Declared
	Imported map[string]ImportedName // NameKey(引的那份文件, 本地名字) → 那一个名字
	// Packages 是本文件里挂过的包名 → 那一个包的根（`import wt from "libs/web_test"` 里那一段
	// "libs/web_test"）。键与 Imported 同一种写法，两份文件把不同的包挂成同一个名字也不互相遮蔽。
	// 编辑器补 `wt.` 后面那一段时使用这一张表。
	Packages map[string]string
	Fields   map[string][]FieldInfo // 类型名 → 摊平后的字段表（含偏移）
	// TopValues 是每一份文件顶层那些 `const` 与 `export shared`：键与 Sigs 同一种写法
	// （包里的带包根，工程自己的文件就是原名）。包外面按点号取一个交出来的数读的是这张表，
	// 编辑器列一个包能点出什么时也查它。
	TopValues map[string]TopValue
	// PkgValues 记的是"这一次 `.名字` 读的是哪个包交出来的数"，值就是声明它的那个节点——
	// 静态槽按节点排，后端拿这个才找得到槽位（与 Imported 上带的 Decl 同一条路）。
	PkgValues map[ast.Expr]*ast.Decl
	// Moved 说"这一个局部量的值曾经交出去过"。键是声明它的那个语法节点
	// （`*ast.Decl` 或 `*ast.Param`）。
	//
	// 后端靠它决定要不要给这个槽位发析构：交出去之后它不再是那一块的持有者，两个持有者析构同一块
	// 比多留一块无人认领的内存严重得多。这是一份**保守**记录——分支里有一条路交出去、
	// 循环第二圈交出去，都算"交出去过"，这一个槽位这一趟就不还了。
	// 装进容器字面量、装进 `add`、写进 `new` 的字段初值那类"名字还能接着用但值已转移给别的持有者"的
	// 写法只有引用类型才记在这里（`putInCell` 走 markMoved）；值类型不记——那个位置收下的是拷贝，
	// 源那一块的持有者没变。
	Moved map[any]bool
	// Outs 记的是"这一个槽位把值交出去过几次"（markMoved 那一笔）。
	// 一个名字最多作废一次，但分支与循环里可以出现好几处交出，所以这个位置数的是次数。
	Outs map[any]int
	// Handoff 记的是"哪几次是当**用户函数**的实参交出去的"，值就是接收那一个位置的参数节点。
	// 装进容器、写进字段、`return` 出去那些都不在这里——那几处确实另有持有者。
	Handoff map[any][]any
	// Consumed 是留给后端的一句话：这一个槽位把值交出去过，但交进去的每一处都被证明不留
	// （接收的那个参数在它的函数里没被 return、没写进字段、没装进容器、也没再交出去）。
	// 这时候那一块内存出了被调方就不再被任何一方持有，调用方的帧照样能当它的持有者。
	// 判定放在所有函数体都查完之后，见 noteConsumed。
	Consumed map[any]bool
	// HandBack 记"被调方把这一个参数原样 `return` 给调用者"记了几次；ValueReturns 记它所在
	// 那个函数一共有几条带值的 return（每一条都给这一个函数的全部参数各记一笔）。
	// 两数相等才算每一条路返回的都是这一个参数。
	HandBack     map[any]int
	ValueReturns map[any]int
	// Back 是留给后端的一句话：这一个参数除了被返回，没有别的路把它交出去，而函数每一条带值的
	// return 返回的都是它 ⇒ 调用者拿结果的那个位置就是那块内存唯一的持有者。
	// 判不上就不登记——漏登记会让一块内存无人认领，错登记会让两个持有者析构同一块。
	Back map[any]bool
	// Taken 记的是：读出来的**这一个字段**被带走了，读完要把字段那个位置写空。
	// 键就是那次读的语法节点（`*ast.Member`，或方法体里直接写的字段名 `*ast.Name`）。
	// 只有交出才会记上——名字、实参、`return`、容器位置、盒子这五种持有者发生变更的地方；
	// 只是顺着读一次（`o.i.n`、`o.list.add(x)`、`length(o.name)`）不记，那次读取没有新的持有者。
	Taken map[ast.Expr]bool
}

// BindKind 说一个名字是哪一类名字——后端要据此决定它在帧上、在静态池里，还是根本不发内存。
//
// en: BindKind says which sort of name this is - the backend decides from it whether the name
// lives on the frame, in the static pool, or gets no memory at all.
type BindKind uint8

const (
	BindLocal       BindKind = iota // 块里的 let、循环变量、catch 的 code／msg
	BindParam                       // 函数或方法的参数
	BindThis                        // 方法体里的 this：就是接收者那份句柄
	BindField                       // 方法体里直接写自己的字段（走 this）
	BindGlobalLet                   // 顶层 let
	BindGlobalConst                 // 顶层 const
	BindShared                      // export shared
	BindArgs                        // 入口文件顶层的 args
)

// Bind 说一个名字写出来时指的是哪儿。
//
// en: Bind says what a name refers to when written.
type Bind struct {
	By     BindKind
	Global bool // true：静态槽（顶层 let／const／shared、args）；false：本帧的槽
	Decl   *ast.Decl
	Param  *ast.Param
	Typ    Type
}

// CallKind 是一次调用的落点类别。
//
// en: CallKind is the category of a call site's destination.
type CallKind uint8

const (
	CallUser CallKind = iota // 自己写的函数或方法
	CallStd                  // 标准库的组函数
)

// CallRef 是一次调用最后算出来的目标。
//
// en: CallRef is the target a call finally resolved to.
type CallRef struct {
	Kind CallKind
	Func string // CallUser：函数名（全程序唯一）
	Std  string // CallStd：形如 "console.print"
	Recv Type   // 方法是挂在哪个类型上的
}

// Signature 是一个函数或方法算好的签名，交给编辑器做补全与悬停。
// 参数带着写出来的名字（用户函数才有，标准库那些是手工排列的，没有名字），Ret 是返回类型，
// 没返回值的是 NoRet。
//
// en: Signature is the computed signature of one function or method, handed to the editor for
// completion and hover. Parameters carry the names as written (only user functions have them;
// the standard library's are arranged by hand and unnamed), Ret is the return type, and NoRet
// when nothing comes back.
type Signature struct {
	Params []Param
	Ret    Type
	Method bool
	// Owner 只有方法有：它挂在哪一个类型上（规范名）。补全一个类型的成员时按它找。
	Owner string
}

// Declared 是一个类型名的种类与它有没有 export。Kind 写的是语言里那一个关键字：
// class、struct、interface、enum，别名是 type。
//
// en: Declared is the kind of a type name and whether it was exported. Kind records the keyword
// as written in the language: class, struct, interface, enum, and type for an alias.
type Declared struct {
	Kind   string
	Export bool
}

// TopValue 是一份文件顶层那一个 `const` 或 `export shared`：它的类型、有没有挂 export、
// 是哪一种（shared 那一半定成不给包外面读）、写在哪一份文件第几行，
// 以及声明它的那个节点——后端找静态槽要按这个节点。
//
// en: TopValue is one top-level const or export shared of a file: its type, whether it was
// exported, which of the two it is (a shared name is not readable outside the package), where
// and in which file it was written, and the node that declares it - the backend locates the
// static slot through that node.
type TopValue struct {
	Typ    Type
	Export bool
	Shared bool
	At     ast.Pos
	File   string
	Decl   *ast.Decl
}

func newFacts() *Facts {
	return &Facts{
		Types:        map[ast.Expr]Type{},
		Binds:        map[*ast.Name]Bind{},
		Calls:        map[*ast.Call]CallRef{},
		EnumVals:     map[string]int64{},
		Tests:        map[*ast.Test]Type{},
		Funcs:        map[string]*ast.Func{},
		Sigs:         map[string]Signature{},
		Declared:     map[string]Declared{},
		Imported:     map[string]ImportedName{},
		Packages:     map[string]string{},
		Fields:       map[string][]FieldInfo{},
		TopValues:    map[string]TopValue{},
		PkgValues:    map[ast.Expr]*ast.Decl{},
		Moved:        map[any]bool{},
		Outs:         map[any]int{},
		Handoff:      map[any][]any{},
		Consumed:     map[any]bool{},
		HandBack:     map[any]int{},
		ValueReturns: map[any]int{},
		Back:         map[any]bool{},
		Taken:        map[ast.Expr]bool{},
	}
}

// Run 检查整个程序，返回全部报错。
//
// en: Run checks the whole program and returns every error.
func Run(p *Program) []Error {
	errs, _ := RunWithFacts(p, false)
	return errs
}

// RunWithFacts 同 Run；wantFacts 为真时顺带把后端要的那份结论记下来。
//
// en: RunWithFacts is Run; when wantFacts is set it also records the conclusions the backend
// needs.
func RunWithFacts(p *Program, wantFacts bool) ([]Error, *Facts) {
	var facts *Facts
	if wantFacts {
		facts = newFacts()
	}
	c := &Checker{prog: p, types: map[string]*typeInfo{}, typeAlias: map[string]*typeAliasInfo{}, funcs: map[string]funcInfo{},
		vars: map[string]Type{}, moved: map[string]bool{}, consts: map[string]bool{}, borrowed: map[string]string{}, shared: map[string]bool{},
		imported: map[string]ImportedName{}, usedImp: map[string]bool{}, usedGroup: map[string]bool{},
		covered: map[*ast.Switch]bool{}, fieldOf: map[ast.Expr]Type{}, facts: facts,
		topVals: map[string]TopValue{}}
	if facts != nil {
		// 顶层那些常量只记一份：facts 在的时候就记到 facts.TopValues 上，
		// 编辑器与包外面取数读的都是它，不留两份可能不一致的副本。
		c.topVals = facts.TopValues
	}
	c.seedBuiltins()
	// 查的顺序是"被引的在前"：一份文件的签名与字段表第一次摊开时，使用的是**它自己那份名字表**
	// （包里的带包根）。顺序排错了，摊开发生在引用它的那一份文件里，报出来的就是
	// "没有 Pair 这个类型"这种无法定位根源的话，行列还会归到别的文件上去。
	order := checkOrder(p)
	for _, f := range order {
		c.file = f
		c.collect(f)
	}
	for _, f := range order {
		c.file = f
		c.hoistedDecl = map[*ast.Decl]bool{}
		c.checkImports(f)
		// 本文件的类型别名先解一遍：环、右边写错，都要报在定义它的那一份文件里，
		// 而且它右边用上的 import 名字要记在这一份文件的账上。
		c.resolveTypeAliasDefs(f)
		c.openScope() // 顶层就是一层作用域
		// 引来的名字必须先登记，再查本文件的定义：函数体里用一个 `export const`
		// 是最常见的跨文件写法，晚一步登记，函数体里就查不到那个名字，
		// 报出来的是"这里要的是 int，给的是 k"这种无法定位根源的话。
		for _, nm := range c.importedList() {
			if c.facts != nil {
				c.facts.Imported[NameKey(c.file.Path, nm.Name)] = nm
			}
			if nm.Val.Kind != 0 {
				by := nm.By
				if by == BindGlobalLet {
					by = BindGlobalConst // 导入进来的可变态只有 const 与 shared，let 导不出来
				}
				c.declareKind(nm.Name, nm.Val, nm.At, nil, by)
				continue
			}
			if nm.Kind != FuncT {
				continue // 类型名（struct/class/interface/enum）由 collect 那一趟登记
			}
			c.declare(nm.Name, Type{Kind: FuncT, Name: nm.Name}, nm.At)
		}
		c.hoistTopDecls(f)
		for _, d := range f.Defs {
			c.checkDef(d)
		}
		for _, s := range f.Stmts {
			c.stmt(s)
		}
		// 门面那一条排在查完本文件之后：它的签名这时都已经可以完整解析。
		if c.prog.Facades[f.Path] {
			c.checkFacadeLeaks(f)
		}
		c.closeScope()
		for _, nm := range c.importedList() {
			if !c.usedImp[nm.Name] {
				c.fail(nm.At, "import 了 %s 却没用到", nm.Name)
			}
		}
		for nm, b := range c.packages {
			if !c.usedPkg[nm] {
				c.fail(b.at, "import 了 %s 这个包却没用到", nm)
			}
		}
	}
	c.noteConsumed()
	// 编辑器那一张签名表排在最后一趟汇总：走到这里，顶层函数在 checkDef 那一趟摊过了、
	// 方法在对象那一趟摊过了、跨文件引进来的在 funcSig 里摊过了，三份来源都进了 c.funcs，
	// 这里只按规范名汇总一份，不另算第二遍。
	if c.facts != nil {
		for k, fi := range c.funcs {
			if fi.fn == nil {
				continue
			}
			c.facts.Sigs[k] = Signature{Params: fi.params, Ret: fi.ret, Method: fi.method, Owner: fi.owner}
		}
		for k, ti := range c.types {
			d := Declared{Kind: "type", Export: ti.obj != nil && ti.obj.Export}
			switch {
			case ti.obj != nil && ti.obj.Kind == "class":
				d.Kind = "class"
			case ti.obj != nil:
				d.Kind = "struct"
			case ti.iface != nil:
				d.Kind, d.Export = "interface", ti.iface.Export
			case ti.enum != nil:
				d.Kind, d.Export = "enum", ti.enum.Export
			}
			c.facts.Declared[k] = d
			// 方法摊在类型那一张表上（layout 那一趟算的，含从 base 继承来的那几条），
			// 顶层函数才有 c.funcs 那一条路：这里按"类型名.方法名"汇总一份，键的写法与
			// pkgExported 查找用的一致。
			for _, ms := range ti.methods {
				c.facts.Sigs[k+"."+ms.Name] = Signature{Params: ms.Params, Ret: ms.Ret, Method: true, Owner: k}
			}
		}
		for k, al := range c.typeAlias {
			if al.def == nil {
				continue
			}
			c.facts.Declared[k] = Declared{Kind: "type", Export: al.def.Export}
		}
	}
	sortErrors(c.errs)
	return dedup(c.errs), c.facts
}

// noteConsumed 在所有文件都查完之后补全"交出去但没人带走"的记录（Facts.Consumed）。
//
// 等到最后的原因：判的是**接收那一个参数**在它自己的函数里有没有把值留住，而那个函数体
// 可能在本文件之后才查（跨文件调用、互相调用都算），早一步问只会得到"还没记上"。
// 递归与互调不必特别处理——被调方的参数一旦被 return／写进字段／再交出去，Moved 就是终局，
// 循环调用里谁也不会改变谁的结论。
//
// 判的是三个条件一起成立：这一个槽位作废过、作废的每一处都是当用户函数的实参交出去的
// （交进容器、写进字段那一类另有持有者，不算），而那些接收位置全都没有留住它。
// 少任何一条都不登记——漏登记会让一块内存无人认领，错登记会让两个持有者析构同一块。
//
// en: noteConsumed settles the record of "handed over but nobody took it away"
// (Facts.Consumed) after every file has been checked. It waits until the end because the
// question is whether the RECEIVING parameter kept the value inside its own function, and that
// function body may be checked after this file (cross-file calls and mutual calls both count),
// so asking earlier can only answer "not recorded yet". Recursion and mutual recursion need no
// special case - once the callee's parameter is returned, written into a field, or handed on
// again, Moved is final, and in a cycle neither call can change the other's conclusion. Three
// conditions must all hold to register: this slot was voided, every place it was voided was as
// an argument to a user function (into a container or into a field is different - those have a
// holder), and every receiving position let it go. Registering on less than that is wrong
// either way: a miss leaves a block nobody owns, a false registration lets two holders destroy
// one block.
func (c *Checker) noteConsumed() {
	if c.facts == nil {
		return
	}
	for node := range c.facts.Moved {
		hs := c.facts.Handoff[node]
		if len(hs) == 0 || len(hs) != c.facts.Outs[node] {
			continue
		}
		drops := true
		for _, h := range hs {
			if c.facts.Moved[h] {
				drops = false
				break
			}
		}
		if drops {
			c.facts.Consumed[node] = true
		}
	}
	// 被调方把参数原样返回调用者，且这个参数没有别的外流——三条一起成立才登记。
	// 少任何一条都算不准：漏登记会让一块内存无人认领，错登记会让两个持有者析构同一块。
	for node, n := range c.facts.HandBack {
		if n > 0 && c.facts.ValueReturns[node] == n && c.facts.Outs[node] == n {
			c.facts.Back[node] = true
		}
	}
}

type funcInfo struct {
	file    *ast.File
	fn      *ast.Func
	ret     Type
	params  []Param
	method  bool
	owner   string
	laidOut bool
	// key 是这一个函数在名字表里的**规范名**（包里的带包根）。`import { f as g }` 之后
	// 写 g 查到的还是这一条，调用记的、后端发的那个 IR 函数名都取这里——不然同一个函数体会
	// 拿到两个名字。
	key string
}

// Param 是函数或方法的一个参数。
//
// en: Param is one parameter of a function or method.
type Param struct {
	Name string
	Typ  Type
	// Default 是写在这个参数位置上的默认值，nil 表示没有。
	// 有了它，调用方才能省掉这一个实参——省下来的那一个由降级在调用点补上。
	Default ast.Expr
	// Node 是源码里那一个参数节点，只有用户写的函数与方法有（标准库那些签名是手工排列的）。
	// 后端据此判断"交进这一个位置的值，被调方有没有带走"（Facts.Consumed）。
	Node *ast.Param
}

// takesRef 说这一个位置是不是 `ref` 收的只读借用。标准库那些签名 Node 为 nil，
// 它们按只读借用规则处理，不在这里判断。
//
// en: takesRef says whether this position is a read-only borrow taken with `ref`. A
// standard-library signature has no Node: those follow the read-only borrow rule and are not
// checked here.
func (p Param) takesRef() bool { return p.Node != nil && p.Node.Ref }

type field struct {
	Name string
	Typ  Type
	Pos  ast.Pos
}

type methodSig struct {
	VarArg bool
	Name   string
	Params []Param
	Ret    Type
	Pos    ast.Pos
	// Poly 是从标准库那张表带过来的："交回的类型跟着第一个实参走"那一类
	// （math.absolute_value／math.minimum／math.maximum）。组名式调用走的是 methodSig 这条路，
	// 不记录这个字段就会丢失。
	Poly int
}

type typeInfo struct {
	file    *ast.File
	name    string
	kind    Kind
	obj     *ast.Object
	iface   *ast.Interface
	enum    *ast.Enum
	laidOut bool
	fields  []field
	methods []methodSig
}

// calendarFields 是内建 `struct Calendar` 的字段表。
//
// 它是编译器发的两种形状之一（另一种是 `Response`）：文件里写不出它，`time.calendar(seconds)`
// 返回的就是这一只。七个字段一律 int，按 UTC 计算；要得到本地时间，
// 把 `time.utc_offset() * 60` 加进秒数再调用一次。
//
// 两处必须同步修改：运行期 `@time_parts` 按这张表的顺序把七个整数写到偏移 0、8、…、48，
// 调整顺序或添加字段都要同步改那条例程；`weekday` 是 0=星期一，1970-01-01（星期四）算出来是 3。
//
// en: calendarFields is the field table of the built-in `struct Calendar`. It is one of the two
// shapes the compiler emits itself (the other is `Response`): it cannot be written in a file,
// and `time.calendar(seconds)` returns exactly this. Seven fields, all int, computed in UTC;
// for local time add `time.utc_offset() * 60` seconds and call it again. Two places must
// change together: the runtime routine @time_parts writes seven integers at offsets 0, 8, ...
// 48 in this table's order, so reordering or adding a field means editing that routine; and
// `weekday` is 0 = Monday, which makes 1970-01-01 (a Thursday) come out as 3.
var calendarFields = []string{"year", "month", "day", "hour", "minute", "second", "weekday"}

// typeCalendar 是那一只在类型式子里的样子。
//
// en: typeCalendar is that struct's appearance inside a type expression.
var typeCalendar = Type{Kind: Struct, Name: "Calendar"}

// typeResponse 是内建 `struct Response` 在类型式子里的样子。
// 三样：状态码、头（每一行一条 `"名字: 值"`，由调用方自己 `split`）、正文（一段字节，不按文本校验）。
//
// en: typeResponse is the built-in `struct Response` inside a type expression: three things,
// the status code, the headers (one `"name: value"` per line, split by the caller) and the
// body (a run of bytes, not validated as text).
var typeResponse = Type{Kind: Struct, Name: "Response"}

// typeJson 是内建引用类型 `Json` 在类型式子里的样子。
// 它没有名字可查的字段表：形状住在编译器发的 nef.json.* 那几支里，
// 检查这一侧只知道"这是一棵树、是引用类型、可以取 null"。
//
// en: typeJson is the built-in reference type `Json` inside a type expression. It has no field
// table addressable by name: its shapes live in the nef.json.* functions emitted by the
// compiler, and the checking side only knows "this is a tree, it is a reference type, it can be
// null".
var typeJson = Type{Kind: Json, Name: "Json"}

// typeStrList 是 `list<string>`：json.keys 与 regular_expression 那几条返回的就是它。
//
// en: typeStrList is `list<string>`: what json.keys and the regular_expression members return.
var typeStrList = Type{Kind: List, Name: "list", Args: []Type{typeStr}}

// seedBuiltins 登记编译器自己发的类型，跑在 collect 之前：
// 用户在同一份文件里再写 `struct Calendar` 会被"Calendar 是内建名字"那一条拒绝
// （`Calendar` 在 lexer 的保留名表里），走到这里也不会触发 dup 检查。
//
// en: seedBuiltins registers the types the compiler emits itself, and runs before collect: a
// user writing `struct Calendar` in the same file is refused because "Calendar is a built-in
// name" (`Calendar` is in the lexer's reserved table), and even reaching here it would not trip
// the duplicate check.
func (c *Checker) seedBuiltins() {
	info := &typeInfo{name: "Calendar", kind: Struct, laidOut: true}
	for _, n := range calendarFields {
		info.fields = append(info.fields, field{Name: n, Typ: typeInt})
	}
	c.types["Calendar"] = info
	if c.facts != nil {
		c.facts.recordFields("Calendar", info.fields)
	}
	// 内建那只 `struct Response`：同一做法——名字与形状都是编译器发的，
	// 文件里写不出 `struct Response`（那一个名字在 lexer 的保留名表里）。
	// 三个字段的顺序就是运行期那块内存的偏移 0、8、16，也是 `@http_request` 填充的顺序，
	// 改一处必须同步改另一处。
	resp := &typeInfo{name: "Response", kind: Struct, laidOut: true}
	resp.fields = []field{{Name: "status", Typ: typeInt}, {Name: "headers", Typ: typeStrList}, {Name: "body", Typ: typeBytes}}
	c.types["Response"] = resp
	if c.facts != nil {
		c.facts.recordFields("Response", resp.fields)
	}
}

// 只读借用的三种来路。规矩同一条，报错的话各留一句。
//
// en: The three sources of a read-only borrow. One rule, one sentence each.
const (
	borrowReceiver = "接收者"
	borrowLoop     = "遍历"
	borrowParam    = "参数"
)

// Checker 装着一趟检查的全部上下文。
//
// en: Checker holds the entire context of one check pass.
type Checker struct {
	prog      *Program
	file      *ast.File
	errs      []Error
	types     map[string]*typeInfo
	typeAlias map[string]*typeAliasInfo
	funcs     map[string]funcInfo
	vars      map[string]Type
	moved     map[string]bool
	consts    map[string]bool
	// topVals 是这一趟里每一份文件顶层那些 `const` 与 `export shared`：键与 funcs、types
	// 同一种写法（包里的带包根）。包外面按点号取一个交出来的数读的就是它。
	topVals map[string]TopValue
	// hoistedDecl 记的是本文件顶层那些 let／const 的语法节点：名字与类型先收齐
	// （先用后写那一条），值表达式和交出留到语句流跑到那一行再算。
	// 按节点记：块里另写的同名 let 不算它，仍按重名规则拒绝。
	hoistedDecl map[*ast.Decl]bool
	shared      map[string]bool
	// borrowed 记"这一个名字是只读借用"，值写来路：接收者、遍历、ref 参数。
	// 三处借用的规则相同（拿不到所有权，也就不能析构原来的对象），报错文案按来路区分。
	//
	// en: borrowed marks "this name is a read-only borrow" and keeps where it came from: the
	// receiver, a loop variable, or a ref parameter. The three follow the same rule (a borrower
	// gets no ownership, so it cannot destroy the original place); each source has its own
	// error wording.
	borrowed  map[string]string
	scopes    []scope
	loopDepth int
	curFunc   *funcInfo
	retType   Type
	imported  map[string]ImportedName
	// packages 是本文件里挂着的包名：`import a from "libs/a"` 的 a → 那个包的根。
	// 访问包内成员一律写 `a.成员名`。usedPkg 记它有没有被用上，
	// 与 imported 的记录在同一时机报出"引进了却没用"。
	packages  map[string]pkgBinding
	usedPkg   map[string]bool
	usedGroup map[string]bool
	usedImp   map[string]bool
	// covered 记下哪些 switch 的标签已经把值列全（enum 列满成员、boolean 两个都写），
	// 没有 default 也覆盖了所有路径。
	covered map[*ast.Switch]bool
	// fieldOf 记"这一次 `.名字` 读的是哪个类型的字段"——判断"被带走的那一个是不是
	// class 的引用类型字段"要用它，而这一条在 `nf check`（不带 facts 那一趟）也要报得出来，
	// 所以不能只记在 facts.Types 里。
	fieldOf map[ast.Expr]Type
	// facts 非空时同时记下后端要复用的那一份结论（前端到中间码那一步）。
	facts *Facts
}

// owner 交出正在查的这一份文件所属的包根；工程里的普通文件返回空串。
//
// en: owner returns the package root the file being checked belongs to; an ordinary project
// file returns the empty string.
func (c *Checker) owner() string {
	if root, ok := c.prog.packageOf(c.file.Path); ok {
		return root
	}
	return ""
}

// key 把写出来的名字换成本文件里的规范名：包里的文件一律带包根，工程的文件就是原名。
// 登记与查找都走这一条，所以**包看不见工程**：包里的名字与工程里的名字是两个键，
// 包引不到工程定义的名字（本来也不应引——同一个包换一个工程必须仍能编译）。
// 反向也成立：两个包各有一只 `Pair`、同一个包的两份版本并排，都落在各自的包根下，互不遮蔽。
//
// en: key turns a written name into the canonical name for this file: a file inside a package
// always prefixes the package root, a project file uses the original name. Both registering and
// looking up go through here, which is why a PACKAGE CANNOT SEE THE PROJECT: a package name and
// a project name are two keys, so a package cannot import something the project defines - and
// it should not, since the same package must compile unchanged in a different project. The
// converse holds too: two packages each with a `Pair`, or two versions of one package side by
// side, each land under their own package root and neither shadows the other.
func (c *Checker) key(name string) string { return qualKey(c.owner(), name) }

// NameKey 是 Facts.Imported 的键：同一份程序里两份文件可以各自把不同的东西引成同一个
// 本地名字（`import { limit } from "./a.nf"` 与 `import { limit } from "libs/b"`），
// 按名字存就会后一份覆盖前一份，后端拿着别人的静态槽。
//
// en: NameKey is the key of Facts.Imported: within one program two files may each import a
// different thing under the same local name (`import { limit } from "./a.nf"` and `import {
// limit } from "libs/b"`), and storing by name alone would let the second overwrite the first,
// leaving the backend holding somebody else's static slot.
func NameKey(file, name string) string { return file + "\x00" + name }

// ImportedName 是从别的文件拿进来的一个名字。
//
// en: ImportedName is a name taken in from another file.
type ImportedName struct {
	Kind   Kind // Class / Struct / Iface / Enum / FuncT
	TypeNm string
	FuncNm string
	From   string
	Name   string
	Val    Type // 顶层 const／shared 的值类型，别的导入项为空
	By     BindKind
	Decl   *ast.Decl // 指向声明它的那个节点：静态槽是按节点排的，后端拿这个才找得到槽位
	At     ast.Pos
}

// bindOfDecl 把一条顶层声明归入后端识别的那一类。
//
// en: bindOfDecl classifies one top-level declaration into the category the backend recognises.
func bindOfDecl(d *ast.Decl) BindKind {
	switch {
	case d.Shared:
		return BindShared
	case d.IsConst:
		return BindGlobalConst
	}
	return BindGlobalLet
}

func (c *Checker) importedList() []ImportedName {
	var out []ImportedName
	for _, nm := range c.imported {
		out = append(out, nm)
	}
	return out
}

func sortErrors(es []Error) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0; j-- {
			a, b := es[j], es[j-1]
			if a.File == b.File {
				if a.Line < b.Line || (a.Line == b.Line && a.Col < b.Col) {
					es[j], es[j-1] = es[j-1], es[j]
					continue
				}
			} else if a.File < b.File {
				es[j], es[j-1] = es[j-1], es[j]
			}
			break
		}
	}
}

func (c *Checker) fail(at ast.Pos, format string, args ...any) {
	c.errs = append(c.errs, Error{File: c.file.Path, Line: at.Line, Col: at.Col, Msg: fmt.Sprintf(format, args...)})
}

// ---- 名字能不能用 ----

func badName(name string, at ast.Pos) string {
	if lexer.Keywords[name] {
		return fmt.Sprintf("%s 是关键字，不能拿去做名字", name)
	}
	if lexer.Reserved[name] {
		return fmt.Sprintf("%s 是内建名字，不能拿去做名字", name)
	}
	if name == "" {
		return "名字不能是空的"
	}
	if !(name[0] == '_' || name[0] == '$' || isASCIILetter(name[0])) {
		return fmt.Sprintf("%s 不是合法名字", name)
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if !(ch == '_' || ch == '$' || isASCIILetter(ch) || (ch >= '0' && ch <= '9')) {
			return fmt.Sprintf("%s 不是合法名字", name)
		}
	}
	_ = at
	return ""
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func (c *Checker) checkName(name string, at ast.Pos) {
	if msg := badName(name, at); msg != "" {
		c.fail(at, "%s", msg)
	}
}

// ---- 第一趟：登记所有顶层名字 ----

// checkOrder 交出"被引的在前"的那一个文件顺序。
//
// 需要它的原因：一份文件的函数签名与字段表是**第一次用到时**才摊开的，摊开的时候按的是
// "正在查的这一份文件属于哪个包"。入口排在前面，包里那些签名就在入口的
// 上下文里摊开——同一个名字在这两个上下文里指向两个不同的东西，报出来的是"没有 Pair 这个类型"，
// 行列还会归到错误的文件。被引的排在前头，摊开就发生在它自己的文件里，两处都正确。
//
// 整块挂名那一句把**那一份包的所有文件**都算成前头；包里的文件之间的先后，
// 由它们自己那些 `./` 引入决定（也在同一张表里）。
// 引用成环在 checkImports 里已经是编译错误，这里的 onPath 只用于保证遍历终止。
//
// en: checkOrder returns the file order with "imported before importer". It is needed because a
// file's function signatures and field tables are only expanded when FIRST used, and the
// expansion uses "which package does the file being checked belong to" as its context. If the
// entry file is ordered first, a package's signatures get expanded in the entry's context - the
// same name points at two different things in those two contexts, and the report says "no such
// type as Pair" with line and column attributed to the wrong file. Put the imported file first
// and the expansion happens inside its own file, which is right on both ends. A whole-module
// import counts EVERY file of that package as coming first; the order among a package's own
// files is decided by their own `./` imports (also in this table). An import cycle is already a
// compile error by the time checkImports is done, so the onPath flag here only guarantees the
// walk terminates.
func checkOrder(p *Program) []*ast.File {
	byPath := map[string]*ast.File{}
	for _, f := range p.Files {
		byPath[f.Path] = f
	}
	depsOf := func(f *ast.File) []*ast.File {
		var out []*ast.File
		for _, imp := range f.Imports {
			if imp.Package != "" {
				root, ok := p.PkgRefs[PkgRefKey(f.Path, imp.Path)]
				if !ok {
					continue // 找不到包那句 import 自己会报错
				}
				for _, g := range p.Files {
					if r, isPkg := p.packageOf(g.Path); isPkg && r == root {
						out = append(out, g)
					}
				}
				continue
			}
			if g := byPath[ResolveImportPath(f.Path, imp.Path)]; g != nil && g != f {
				out = append(out, g)
			}
		}
		return out
	}
	seen := map[string]bool{}
	var out []*ast.File
	var walk func(f *ast.File, onPath map[string]bool)
	walk = func(f *ast.File, onPath map[string]bool) {
		if seen[f.Path] || onPath[f.Path] {
			return
		}
		onPath[f.Path] = true
		for _, g := range depsOf(f) {
			walk(g, onPath)
		}
		delete(onPath, f.Path)
		seen[f.Path] = true
		out = append(out, f)
	}
	for _, f := range p.Files {
		walk(f, map[string]bool{})
	}
	return out
}

func (c *Checker) collect(f *ast.File) {
	for _, d := range f.Defs {
		switch def := d.(type) {
		case *ast.Object:
			c.checkName(def.NameText, def.Where())
			k := c.key(def.NameText)
			if _, dup := c.types[k]; dup || c.typeAlias[k] != nil {
				c.fail(def.Where(), "%s 已经定义过了", def.NameText)
				continue
			}
			kind := Struct
			if def.Kind == "class" {
				kind = Class
			}
			c.types[k] = &typeInfo{file: f, name: k, kind: kind, obj: def}
		case *ast.Interface:
			c.checkName(def.NameText, def.Where())
			k := c.key(def.NameText)
			if _, dup := c.types[k]; dup || c.typeAlias[k] != nil {
				c.fail(def.Where(), "%s 已经定义过了", def.NameText)
				continue
			}
			c.types[k] = &typeInfo{file: f, name: k, kind: Iface, iface: def}
		case *ast.Enum:
			c.checkName(def.NameText, def.Where())
			k := c.key(def.NameText)
			if _, dup := c.types[k]; dup || c.typeAlias[k] != nil {
				c.fail(def.Where(), "%s 已经定义过了", def.NameText)
				continue
			}
			c.types[k] = &typeInfo{file: f, name: k, kind: Enum, enum: def}
		case *ast.TypeAlias:
			c.checkName(def.NameText, def.Where())
			k := c.key(def.NameText)
			if _, dup := c.types[k]; dup || c.typeAlias[k] != nil {
				c.fail(def.Where(), "%s 已经定义过了", def.NameText)
				continue
			}
			// 右边那一个先不解：这一趟只登记名字，解要等所有名字都登记完（`type A = B` 写在
			// `struct B` 上面也必须能解析）。
			c.typeAlias[k] = &typeAliasInfo{file: f, def: def}
		case *ast.Func:
			if def.IsMethod {
				continue
			}
			c.checkName(def.NameText, def.Where())
			k := c.key(def.NameText)
			if _, dup := c.funcs[k]; dup {
				c.fail(def.Where(), "函数 %s 重复定义：一个名字只能有一个", def.NameText)
				continue
			}
			c.funcs[k] = funcInfo{file: f, fn: def, key: k}
			if c.facts != nil {
				c.facts.Funcs[k] = def
			}
		case *ast.Decl:
			c.checkName(def.NameText, def.Where())
			if def.IsConst {
				c.consts[def.NameText] = true
			}
			if def.Shared {
				c.markShared(def.NameText)
			} else if def.Export && !def.IsConst {
				c.fail(def.Where(), "顶层的 let 不能 export：跨文件共用一份可变的要写 export shared")
			}
		}
	}
}

// ---- import ----

func (c *Checker) checkImports(f *ast.File) {
	c.imported = map[string]ImportedName{}
	c.usedImp = map[string]bool{}
	c.packages = map[string]pkgBinding{}
	c.usedPkg = map[string]bool{}
	byPath := map[string]*ast.File{}
	for _, g := range c.prog.Files {
		byPath[g.Path] = g
	}
	for _, imp := range f.Imports {
		if imp.Package != "" {
			c.bindPackage(f, imp)
			continue
		}
		// 花括号那一种的 `from` 必须以 ./ 或 ../ 开头。写成一条纯路径的多半是想引依赖却用了
		// 花括号（引包只走整块挂名那一条），装载那一侧拿它当文件去读盘，报回来的是
		// "这条目录读不到这个文件"——无法定位根源，所以这一句在这里明确拒绝。
		if !IsLocalImportPath(imp.Path) {
			c.fail(imp.PathPos, "%s：花括号那一种引的是工程里的文件，路径要 ./ 或 ../ 开头；引包写 import 包名 from \"libs/包名\"", imp.Path)
			continue
		}
		target := ResolveImportPath(f.Path, imp.Path)
		g, ok := byPath[target]
		if !ok {
			c.fail(imp.PathPos, "找不到 import 的文件 %q", imp.Path)
			continue
		}
		// 花括号那一种 import 只在"一份包内的文件引同包的另一份文件"与"工程的文件引工程的文件"
		// 两种情况下成立。跨到包那儿去就得走整块挂名那一条：挂名带的是包根，名字表按包根分家，
		// 花括号这一侧只会把对方的定义按本地名字放进本文件的名字表里。
		gRoot, gInPkg := c.prog.packageOf(g.Path)
		fRoot := c.owner()
		if gInPkg && fRoot == "" {
			c.fail(imp.PathPos, "%s 在包里面：引包要写 import 包名 from \"…\"，花括号那一套是给工程里的文件用的", imp.Path)
			continue
		}
		if fRoot != "" && (!gInPkg || gRoot != fRoot) {
			c.fail(imp.PathPos, "包里的文件只能引同一个包里的文件：%s 不在这个包里面", imp.Path)
			continue
		}
		if importsCycle(c.prog, target, f.Path, map[string]bool{}) {
			c.fail(imp.PathPos, "%s 与 %s 互相引用", f.Path, imp.Path)
		}
		for _, nm := range imp.Names {
			local := nm.Alias
			if local == "" {
				local = nm.Name
			}
			// 这个包里的名字在名字表里的规范名：登记到本文件的键上，值与类型都按这一个走，
			// 换了名也不会让同一个定义拿到两个身份。
			canon := c.key(nm.Name)
			found := false
			for _, d := range g.Defs {
				if d.Name() != nm.Name || !d.Exported() {
					continue
				}
				found = true
				switch def := d.(type) {
				case *ast.Func:
					c.imported[local] = ImportedName{Kind: FuncT, FuncNm: nm.Name, From: target, Name: local, At: nm.Where()}
					// 签名已经在**定义它的那一份文件**里摊过一遍，就不要再摊第二遍：第二遍是在引用它这一份
					// 文件的上下文里进行的，参数默认值那一段读的名字指向别的定义——默认值允许写
					// 本文件的顶层 const，在调用方这一侧摊就报"没有 BIG 这个名字"（实测过）。
					// 本地名字与规范名不同（`import { f as g }`）时把摊好的那一份原样接过来。
					k := c.key(local)
					if old, had := c.funcs[k]; had && old.fn == def && old.laidOut {
						break
					}
					if src, had := c.funcs[canon]; had && src.fn == def && src.laidOut {
						src.key = canon
						c.funcs[k] = src
						break
					}
					c.funcs[k] = funcInfo{file: g, fn: def, key: canon}
				case *ast.Object:
					kind := Struct
					if def.Kind == "class" {
						kind = Class
					}
					if _, dup := c.types[c.key(local)]; !dup {
						c.types[c.key(local)] = &typeInfo{file: g, name: canon, kind: kind, obj: def}
					}
					c.imported[local] = ImportedName{Kind: kind, TypeNm: nm.Name, From: target, Name: local, At: nm.Where()}
				case *ast.Interface:
					if _, dup := c.types[c.key(local)]; !dup {
						c.types[c.key(local)] = &typeInfo{file: g, name: canon, kind: Iface, iface: def}
					}
					c.imported[local] = ImportedName{Kind: Iface, TypeNm: nm.Name, From: target, Name: local, At: nm.Where()}
				case *ast.Enum:
					if _, dup := c.types[c.key(local)]; !dup {
						c.types[c.key(local)] = &typeInfo{file: g, name: canon, kind: Enum, enum: def}
					}
					c.imported[local] = ImportedName{Kind: Enum, TypeNm: nm.Name, From: target, Name: local, At: nm.Where()}
				case *ast.TypeAlias:
					if _, dup := c.types[c.key(local)]; !dup {
						if _, isAlias := c.typeAlias[c.key(local)]; !isAlias {
							c.typeAlias[c.key(local)] = &typeAliasInfo{file: g, def: def}
						}
					}
					c.imported[local] = ImportedName{Kind: Alias, TypeNm: nm.Name, From: target, Name: local, At: nm.Where()}
				case *ast.Decl:
					found = true
					if def.IsConst && !def.Shared {
						c.consts[local] = true
					}
					if def.Shared {
						c.markShared(local)
					}
					c.imported[local] = ImportedName{Kind: FuncT, Name: local, From: target, At: nm.Where(), Val: c.resolveType(def.Typ, def.Where()),
						By: bindOfDecl(def), Decl: def}
				}
			}
			if !found {
				c.fail(nm.Pos, "%s 里没有对外交出 %s", imp.Path, nm.Name)
			}
		}
	}
}

// pkgBinding 是一个挂名绑到的包：包根路径加那句 import 的位置（报"引进了却没用"要说得出行号）。
//
// en: pkgBinding is the package one alias is bound to: the package root path plus the position
// of that import line, because "imported but never used" has to name a line number.
type pkgBinding struct {
	root string
	at   ast.Pos
}

// bindPackage 处理整块挂名那一种 import（`import a from "libs/a"`）。
// 挂名在本文件里得独占一个名字：挂过两次、撞上标准库的组名、撞上本文件的定义，都当场拒绝。
// 挂名由用户指定，所以发生冲突时只需改这一侧，不动本文件已有的名字。
//
// en: bindPackage handles the whole-module form of import (`import a from "libs/a"`). The alias
// must own a name of its own in this file: aliased twice, clashing with a standard-library group
// name, or clashing with a definition in this file are all refused on the spot. The author picks
// the alias, so a clash is fixed on this side without touching any name already written in the
// file.
func (c *Checker) bindPackage(f *ast.File, imp *ast.Import) {
	name := imp.Package
	root, ok := c.prog.PkgRefs[PkgRefKey(f.Path, imp.Path)]
	if !ok {
		c.fail(imp.PathPos, "找不到 import 的包 %q", imp.Path)
		return
	}
	if isStdGroup(name) {
		c.fail(imp.Pos, "%s 是标准库的组名，包的挂名不能用它", name)
		return
	}
	if _, dup := c.packages[name]; dup {
		c.fail(imp.Pos, "%s 这个名字已经挂过一个包了：一个挂名只能指一个包", name)
		return
	}
	if _, dup := c.imported[name]; dup {
		c.fail(imp.Pos, "本文件里已经有 %s 这个名字，包的挂名不能与它撞：给这句 import 换个挂名", name)
		return
	}
	if fi, ok := c.funcs[c.key(name)]; ok && fi.file == f {
		c.fail(imp.Pos, "本文件里已经有 %s 这个名字，包的挂名不能与它撞：给这句 import 换个挂名", name)
		return
	}
	if ti, ok := c.types[c.key(name)]; ok && ti.file == f {
		c.fail(imp.Pos, "本文件里已经有 %s 这个名字，包的挂名不能与它撞：给这句 import 换个挂名", name)
		return
	}
	c.packages[name] = pkgBinding{root: root, at: imp.Pos}
	// 编辑器补 `wt.` 后面那一段时，先要知道这一个挂名指的是哪一个包。
	if c.facts != nil {
		c.facts.Packages[NameKey(f.Path, name)] = root
	}
}

// pkgExported 在一个包里找一个名字。found 说包里有没有这一条，exported 说它挂没挂 export。
// 名字表按包根分家，所以这里直接拿"包根 + 原名"去查：同一个包的两份版本
// 各有各的包根，两个包各有一只同名类型也各归各的，互不遮蔽。
//
// en: pkgExported looks for a name inside a package. found says whether the package has it,
// exported says whether it was exported. Because the name table is split by package root, this
// looks up "package root + original name" directly: two versions of one package have different
// roots, and two packages with a same-named type each keep their own, so neither shadows the
// other.
func (c *Checker) pkgExported(root, name string) (fi *funcInfo, ti *typeInfo, found, exported bool) {
	if f, ok := c.funcs[qualKey(root, name)]; ok && f.fn != nil {
		fc := f
		return &fc, nil, true, f.fn.Exported()
	}
	if info, ok := c.types[qualKey(root, name)]; ok {
		var exp bool
		switch {
		case info.obj != nil:
			exp = info.obj.Export
		case info.iface != nil:
			exp = info.iface.Export
		case info.enum != nil:
			exp = info.enum.Export
		}
		return nil, info, true, exp
	}
	return nil, nil, false, false
}

// pkgAlias 判断包里那一个名字是不是一条交出来的类型别名（`export type Row = Point`）。
// 别名与真类型在 `a.名字` 这一处是两条路径：解出来的是包自己的规范名，按它处理即可。
//
// en: pkgAlias checks whether a name inside a package is an exported type alias (`export type
// Row = Point`). An alias and a real type take two different paths at `a.Name`; here the
// resolved one is the package's own canonical name, so following it is enough.
func (c *Checker) pkgAlias(root, name string) (Type, bool) {
	a, ok := c.typeAlias[qualKey(root, name)]
	if !ok || a.file == nil || !a.def.Export {
		return Type{}, false
	}
	return c.resolveTypeAlias(a, a.def.Where()), true
}

// pkgSay 把"包里没有这个东西"与"有，但没交出来"这两句话分开发出去。
//
// en: pkgSay sends out two different sentences: "the package has no such thing" and "it has it
// but did not export it".
func (c *Checker) pkgSay(at ast.Pos, pkgName, name string, found, exported bool) bool {
	if !found {
		c.fail(at, "%s 这个包里没有对外交出 %s", pkgName, name)
		return false
	}
	if !exported {
		c.fail(at, "%s 里的 %s 没有挂 export：包只把交出来的东西给别人用", pkgName, name)
		return false
	}
	return true
}

// pkgType 识别类型位置上的双名 `a.Pair`：左边必须是本文件挂上的包名，右边得是那个包交出来的类型。
// 返回的 Type 带的是那个包的规范名——工程里那只同名的 `Pair` 与它不是同一个类型，
// 形状表、类型字、字段表都按这一个名字分家。
//
// en: pkgType recognises the two-part name `a.Pair` in a type position: the left side must be a
// package alias bound in this file, the right side must be a type that package exported. The
// Type returned carries that package's canonical name - the project's same-named `Pair` is not
// the same type, and the shape table, type word and field table are all keyed on the canonical
// one.
func (c *Checker) pkgType(nt *ast.NamedType, dot int) Type {
	pkgName, typeName := nt.Name[:dot], nt.Name[dot+1:]
	if strings.Contains(typeName, ".") {
		c.fail(nt.Where(), "包里的名字只点一层：%s 这样写认不出", nt.Name)
		return errType
	}
	b, ok := c.packages[pkgName]
	if !ok {
		c.fail(nt.Where(), "%s 没有挂过包：要写包里的东西，先 import %s from ...", pkgName, pkgName)
		return errType
	}
	if t, isAlias := c.pkgAlias(b.root, typeName); isAlias {
		c.usedPkg[pkgName] = true
		if len(nt.Args) > 0 {
			c.fail(nt.Where(), "%s 是类型别名，不带尖括号参数", nt.Name)
		}
		return t
	}
	_, ti, found, exported := c.pkgExported(b.root, typeName)
	if !c.pkgSay(nt.Where(), pkgName, typeName, found, exported) {
		return errType
	}
	if ti == nil {
		c.fail(nt.Where(), "%s 里的 %s 是一个函数，不能当类型写", pkgName, typeName)
		return errType
	}
	c.usedPkg[pkgName] = true
	if len(nt.Args) > 0 {
		c.fail(nt.Where(), "%s 是自定义类型，不带尖括号参数", nt.Name)
	}
	return Type{Kind: ti.kind, Name: ti.name}
}

// checkFacadeLeaks 规定：门面交出去的那些东西，类型必须是这个包自己定义的。
//
// 拒绝的原因：包的依赖装在容器里面（`libs/大.nfpack/…`），用这个包的人看不见那个名字。
// 门面的签名上要是写了依赖里的类型，外部拿到的是一个无法命名的类型——不能给它起变量名、
// 不能比类型、连报错都说不清是谁的。当场拒绝，让写包的人把它换成自己定义的类型。
// 包里的其余文件不受这一条限制：它们本来就是这一包的内部事情。
//
// en: checkFacadeLeaks enforces one rule: whatever a facade exports must have types defined
// by this very package. Why it has to be refused: a package's dependencies are installed inside
// the container (`libs/big.nfpack/...`), and the person using this package cannot see those
// names. If a facade signature mentions a dependency's type, the outside gets a type it cannot
// name - no variable declaration for it, no type comparison, and even the error message cannot
// say whose it is. Refusing it on the spot tells the package author to swap in a type of their
// own definition. The package's other files are not under this rule: they are already internal
// to the package.
func (c *Checker) checkFacadeLeaks(f *ast.File) {
	root := c.prog.PkgRoots[f.Path]
	if root == "" {
		return
	}
	one := func(t ast.Type) {
		if t == nil {
			return
		}
		c.refuseForeign(c.resolveType(t, t.Where()), root, t.Where())
	}
	list := func(params []*ast.Param, ret ast.Type) {
		for _, p := range params {
			one(p.Typ)
		}
		one(ret)
	}
	for _, d := range f.Defs {
		if !d.Exported() {
			continue
		}
		switch def := d.(type) {
		case *ast.Func:
			if def.IsMethod {
				continue
			}
			list(def.Params, def.Ret)
		case *ast.Object:
			for _, fl := range def.Fields {
				one(fl.Typ)
			}
			for _, m := range def.Methods {
				list(m.Params, m.Ret)
			}
		case *ast.Interface:
			for _, fl := range def.Fields {
				one(fl.Typ)
			}
			for _, m := range def.Methods {
				list(m.Params, m.Ret)
			}
		case *ast.TypeAlias:
			one(def.Typ)
		case *ast.Decl:
			one(def.Typ)
		}
	}
}

// refuseForeign 判断这一个类型里是不是包含其他包定义的类型（容器里的依赖、或者工程自己的）。
// 尖括号里的也算：`list<依赖.Thing>` 交出去同样无法处理。
//
// en: refuseForeign checks whether this type contains a type defined by another package (a
// dependency inside a container, or the project itself). What sits inside angle brackets counts
// too: exporting `list<dep.Thing>` has the same problem.
func (c *Checker) refuseForeign(t Type, root string, at ast.Pos) {
	switch t.Kind {
	case List, Dict, Set:
		for _, a := range t.Args {
			c.refuseForeign(a, root, at)
		}
	case Struct, Class, Iface, Enum:
		if o := keyOwner(t.Name); o != "" && o != root {
			c.fail(at, "%s 是别的包里的类型：门面只能导出这个包自己定义的类型", bareName(t.Name))
		}
	}
}

// ResolveImportPath 把一句 `import ... from "./x.nf"` 拼成与 File.Path 同一种写法
// （斜杠、不带 "./"）。**这是全项目唯一的一份规则**：检查器登记文件、查引用环、降级排执行
// 顺序、命令行装载，四处都要按同一条规则计算，否则同一份程序在两处会被识别成两个名字。
// 若降级那头另写一份（只在整条串的开头削 "./"），`nf 某目录/main.nf` 这种
// 从别处指着工程跑的写法就会当场报"后端认不出文件 某目录/./x.nf"，
// 而在工程目录里跑却正常。
//
// en: ResolveImportPath turns a line of `import ... from "./x.nf"` into the same spelling as
// File.Path (slashes, no "./"). THIS IS THE ONE RULE IN THE WHOLE PROJECT for that: the
// checker registering files, the import-cycle search, the lowering that orders execution, and
// the command-line loader all four must compute it the same way, otherwise the same program is
// recognised as two files on two of those paths. If the lowering side kept its own copy that
// only stripped "./" from the very beginning of the string, `nf somedir/main.nf`, a project run
// from elsewhere, would fail with "the backend does not know the file somedir/./x.nf" while
// running inside the project directory worked.
func ResolveImportPath(fromFile, rel string) string {
	dir := fromFile
	if i := strings.LastIndex(fromFile, "/"); i >= 0 {
		dir = fromFile[:i+1]
	} else {
		dir = ""
	}
	if strings.HasPrefix(rel, "./") {
		return cleanImportPath(dir + strings.TrimPrefix(rel, "./"))
	}
	if strings.HasPrefix(rel, "../") {
		rest := strings.TrimPrefix(rel, "../")
		if i := strings.LastIndex(dir[:max(0, len(dir)-1)], "/"); i >= 0 {
			return cleanImportPath(dir[:i+1] + rest)
		}
		return cleanImportPath(rest)
	}
	return cleanImportPath(rel)
}

// cleanImportPath 把拼出来的那一条折成唯一的一种写法：入口写成 `./main.nf` 时，dir 带着
// 一个 "./"，拼出来的就是 `./libs/greet/greet.nf`；而包根那边（internal/pkgload）从 "." 起
// 算，算出来的是同一条路的另一种写法。两处不一样，同一个文件在名字表、包根表、门面表里就成
// 了三个身份，报出来的还是"找不到 import 的文件"——那个文件实际在磁盘上。
// 只统一写法，不处理大小写：大小写由 pkgload.RealCase 判断。
//
// en: cleanImportPath folds the assembled path into a single spelling: when the entry is
// written `./main.nf`, dir still carries a "./" and the assembled result is
// `./libs/greet/greet.nf`, while the package-root side (internal/pkgload) starts from "." and
// produces the same route spelled differently. Two spellings give one file three identities
// across the name table, the package-root table and the facade table, and the message is still
// "cannot find the imported file" - even though the file is plainly on disk. Only the spelling
// is folded, never letter case: that is handled by pkgload.RealCase.
func cleanImportPath(p string) string {
	if !strings.Contains(p, "/") {
		return p
	}
	return path.Clean(p)
}

// IsLocalImportPath 说这一句 `from` 后面的路径是不是局部引入要求的那种：必须 ./ 或 ../ 开头。
// 装载那一侧（命令行、编辑器、打包）拿它决定"这一句不去盘上找"，由检查那一侧给出完整的报错说明。
//
// en: IsLocalImportPath says whether the path after this `from` is the kind a local import
// requires: it must start with ./ or ../. The loading side (command line, editor, packer) uses
// it to decide "this line is not looked for on disk", and the checking side spells out the
// consequence.
func IsLocalImportPath(rel string) bool {
	return strings.HasPrefix(rel, "./") || strings.HasPrefix(rel, "../")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func importsCycle(p *Program, from, target string, seen map[string]bool) bool {
	if from == target {
		return true
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	for _, f := range p.Files {
		if f.Path != from {
			continue
		}
		for _, imp := range f.Imports {
			if importsCycle(p, ResolveImportPath(from, imp.Path), target, seen) {
				return true
			}
		}
	}
	return false
}

// ---- 类型的字段与方法摊平 ----

func (c *Checker) layout(name string) *typeInfo {
	info, ok := c.types[name]
	if !ok {
		return nil
	}
	if info.laidOut {
		return info
	}
	info.laidOut = true
	var fields []field
	var methods []methodSig
	if base := c.baseName(info); base != "" {
		if b := c.layout(base); b != nil {
			fields = append(fields, b.fields...)
			methods = append(methods, b.methods...)
		}
	}
	switch {
	case info.obj != nil:
		for _, f := range info.obj.Fields {
			fields = append(fields, field{Name: f.NameText, Typ: c.resolveType(f.Typ, f.Where()), Pos: f.Where()})
		}
		for _, m := range info.obj.Methods {
			methods = append(methods, c.signatureOf(m))
		}
	case info.iface != nil:
		for _, f := range info.iface.Fields {
			fields = append(fields, field{Name: f.NameText, Typ: c.resolveType(f.Typ, f.Where()), Pos: f.Where()})
		}
		for _, m := range info.iface.Methods {
			methods = append(methods, c.signatureOf(m))
		}
	}
	info.fields, info.methods = fields, methods
	if c.facts != nil {
		c.facts.recordFields(info.name, fields)
		for _, m := range info.objMethods() {
			c.facts.Funcs[info.name+"."+m.NameText] = m
		}
	}
	return info
}

// objMethods 只取这个类型自己写的方法（不含从 base 摊上来的），用来给后端建 "类型.方法" 表。
//
// en: objMethods takes only the methods this type wrote itself (not the ones hoisted from
// base), which the backend uses to build the "type.method" table.
func (t *typeInfo) objMethods() []*ast.Func {
	if t.obj != nil {
		return t.obj.Methods
	}
	return nil
}

func (c *Checker) baseName(info *typeInfo) string {
	switch {
	case info.obj != nil && info.obj.Base != nil:
		return baseOf(info.obj.Base)
	case info.iface != nil && info.iface.Base != nil:
		return baseOf(info.iface.Base)
	}
	return ""
}

func baseOf(e ast.Expr) string {
	if n, ok := e.(*ast.Name); ok {
		return n.Text
	}
	return ""
}

func (c *Checker) signatureOf(m *ast.Func) methodSig {
	sig := methodSig{Name: m.NameText, Ret: c.retOf(m), Pos: m.Where()}
	for _, p := range m.Params {
		sig.Params = append(sig.Params, Param{Name: p.NameText, Typ: c.resolveType(p.Typ, p.Pos),
			Default: p.Default, Node: p})
	}
	return sig
}

func (c *Checker) retOf(m *ast.Func) Type {
	if m.Ret == nil {
		return Type{Kind: NoRet, Name: "无返回值"}
	}
	t := c.resolveType(m.Ret, m.Where())
	c.checkTypeUsable(t, m.Where())
	return t
}

// typeAliasInfo 是 `type 名字 = 类型` 登记下来的那一条。
//
// typ 是解到底的那一个类型：解一次存下来，之后每一处用到这个名字都拿同一份。
// 只解一遍有两个好处——环只报一次，而且别名右边那一串名字里若有错，报的位置是定义它的
// 那一份文件，不是碰巧第一个用到它的文件。
//
// en: typeAliasInfo is the entry registered by `type Name = SomeType`. typ is
// the fully resolved type: resolved once and stored, and every later use of the name takes the
// same copy. Resolving only once buys two things - a cycle is reported once, and if there is an
// error inside the alias's right-hand name list, the report is attributed to the file that
// defined it rather than to whichever file happened to use it first.
type typeAliasInfo struct {
	file   *ast.File
	def    *ast.TypeAlias
	typ    Type
	active bool // 正在这条别名的解算路上：绕回自己就是环
	done   bool
}

// resolveTypeAlias 把一条别名解到底。解不动时返回 errType，而错已经在解的那一步报过了，
// 调用方别再补一句"没有这个类型"。
//
// en: resolveTypeAlias resolves one alias to the end. When it cannot, it returns errType, and
// the error was already reported during resolution - the caller must not add "no such type" on
// top.
func (c *Checker) resolveTypeAlias(a *typeAliasInfo, where ast.Pos) Type {
	if a.done {
		return a.typ
	}
	if a.active {
		c.fail(where, "类型别名 %s 绕回了自己", a.def.NameText)
		return errType
	}
	// 跨文件用到别人的别名时（B 先查、A 的别名还没解过），右边那一串名字要按定义它的那一份
	// 文件报：不然 A 里的一处写错会被算到 B 的账上。这一趟不记"import 用上了"——那份记录
	// 要等 A 自己那一轮重新解析才准确（resolveTypeAliasDefs 那里）。
	prevFile, prevImported := c.file, c.imported
	if a.file != c.file {
		c.imported = map[string]ImportedName{}
	}
	c.file = a.file
	a.active = true
	t := c.resolveType(a.def.Typ, a.def.Where())
	a.active = false
	c.file, c.imported = prevFile, prevImported
	// 接口本来就当不了类型，别名指到它身上也一样：在定义处报告，
	// 不等哪一个用到这个名字时才报一句指向别处的话。
	c.checkTypeUsable(t, a.def.Where())
	a.done, a.typ = true, t
	return t
}

// resolveTypeAliasDefs 在检查一份文件之前，先把这一份里的别名各解析一遍。
// 两件事要保证：环与"右边写了一个没有的类型"即使没人用到也要报出来；报错归属在定义它的那份文件。
// 重新解析不看上一次的缓存——别名右边用上的 import 名字要记在这一份文件名下，
// 晚一步的"import 了却没用到"检查才会计入它们。同一处内容相同的报错重复出现会被 dedup 并掉，
// 不多出一句。
//
// en: resolveTypeAliasDefs resolves every alias in a file before that file is checked. Two
// things must hold: a cycle and "the right side names a type that does not exist" are reported
// even when nothing uses the alias, and the report belongs to the file that defines it.
// Re-running ignores the previous cache, because the import names an alias's right side uses
// must be recorded under this file's name - otherwise the later "imported but unused" check
// would not know to count them. A duplicated identical message is merged by dedup, so no extra
// line appears.
func (c *Checker) resolveTypeAliasDefs(f *ast.File) {
	for _, d := range f.Defs {
		a, ok := d.(*ast.TypeAlias)
		if !ok {
			continue
		}
		info := c.typeAlias[c.key(a.NameText)]
		if info == nil || info.def != a {
			continue // 这个名字与别处重名，登记的不是本文件这一条，错已经报过了
		}
		info.done = false
		c.resolveTypeAlias(info, a.Where())
	}
}

// typeAliasTarget 判断写出来的这个名字是不是一只类型别名；是就返回它解到底的那一个类型。
// `Row { x: 1 }`、`new Box()`、`D.a` 那几处按名字查类型的地方都先过这一道。
//
// en: typeAliasTarget checks whether a written name is a type alias and, if so, returns the
// type it resolves to. Every place that looks a type up by name - `Row { x: 1 }`, `new Box()`,
// `D.a` - passes through here first.
func (c *Checker) typeAliasTarget(name string, where ast.Pos) (Type, bool) {
	a, ok := c.typeAlias[c.key(name)]
	if !ok {
		return Type{}, false
	}
	return c.resolveTypeAlias(a, where), true
}

// typeNameFor 是 `Row { x: 1 }`、`new Box()`、`D.a`、"这是类型名不是值"那几处按名字查类型的
// 公共头一步：写出来的是别名就跟着解到底——`type Pair = Point` 之后
// 写 Pair 就是写 Point，造值、取枚举成员都算。解不动时 ok 为假：那条别名的错在解的时候
// 已经报过了，这里不必再补。
//
// 返回的第一个值是**规范名**（包里的带包根），不是写出来的那一个：造值、取枚举成员、
// 查字段表都按这一个走，换了名或引了别名都不会让同一只类型拿到两个身份。
//
// en: typeNameFor is the shared first step for `Row { x: 1 }`, `new Box()`, `D.a` and "this is
// a type name, not a value": if what was written is an alias, follow it to the end - after
// `type Pair = Point`, writing Pair means Point, for making values and for
// taking enum members alike. A false ok means the alias's own error was already reported while
// resolving, so nothing is added here. The first returned value is the CANONICAL name (a
// package's carries its root), not the one written: making a value, taking an enum member and
// looking up a field table all use it, so renaming or aliasing can never give one type two
// identities.
func (c *Checker) typeNameFor(name string, where ast.Pos) (string, *typeInfo, bool) {
	if t, isAlias := c.typeAliasTarget(name, where); isAlias {
		if t.Kind == ErrT {
			return "", nil, false
		}
		info, ok := c.types[t.Name]
		return t.Name, info, ok
	}
	k := c.key(name)
	info, ok := c.types[k]
	if ok {
		return info.name, info, true
	}
	return k, nil, false
}

func (c *Checker) resolveType(t ast.Type, where ast.Pos) Type {
	if t == nil {
		return errType
	}
	nt, ok := t.(*ast.NamedType)
	if !ok {
		c.fail(where, "这里需要一个类型")
		return errType
	}
	// 类型位置上的双名：`a.Pair`。左边必须是本文件挂上的包名。
	if i := strings.Index(nt.Name, "."); i >= 0 {
		return c.pkgType(nt, i)
	}
	if bt, ok := baseType(nt.Name); ok {
		if len(nt.Args) > 0 {
			c.fail(nt.Where(), "%s 不带尖括号参数", bt)
		}
		return bt
	}
	switch nt.Name {
	case "list", "dict", "hashset":
		want := 1
		kind := List
		switch nt.Name {
		case "dict":
			want, kind = 2, Dict
		case "hashset":
			kind = Set
		}
		if len(nt.Args) != want {
			c.fail(nt.Where(), "%s 需要 %d 个尖括号参数，实际 %d 个", nt.Name, want, len(nt.Args))
			return errType
		}
		var args []Type
		for _, a := range nt.Args {
			args = append(args, c.resolveType(a, nt.Where()))
		}
		for _, a := range args {
			c.checkTypeUsable(a, nt.Where())
			// `Json` 头上没有运行期的类型字（`lower.go` 的 `tagOf`），容器那个位置的形状与析构
			// 都要靠它：不作处理的话，打印出来会是 `[null, null]` 这种静默的错误结果。
			if a.Kind == Json {
				c.fail(nt.Where(), "%s 进不了 %s：先把它拆成自己要的值，或者存那段文本", a, nt.Name)
			}
			// Socket 与 Listener 同理不进容器：容器那个位置要的是"能被容器完整持有并一路析构"的
			// 一块内存，而句柄的释放方式是调用一次关闭；两只名字指着同一个句柄会导致它被关闭两次。
			// Process 也在这一条上：它的释放方式同样是调用一次关闭，而且它并不是一块内存。
			if a.Kind == Socket || a.Kind == Listener || a.Kind == Process {
				c.fail(nt.Where(), "%s 进不了 %s：它是操作系统给的句柄，只待在名字、实参与返回值里", a, nt.Name)
			}
		}
		if kind == Dict || kind == Set {
			for _, a := range args[:1] {
				if msg := keyReason(a); msg != "" {
					c.fail(nt.Where(), "%s 不能当 %s 的键：%s", a, nt.Name, msg)
				}
			}
		}
		return Type{Kind: kind, Name: nt.Name, Args: args}
	}
	if a, ok := c.typeAlias[c.key(nt.Name)]; ok {
		if len(nt.Args) > 0 {
			c.fail(nt.Where(), "%s 是类型别名，不带尖括号参数", nt.Name)
		}
		c.markUsed(nt.Name) // 引来的别名算用上了；要在换文件之前记，`import 了却没用到` 查的是本文件那份名单
		return c.resolveTypeAlias(a, nt.Where())
	}
	c.markUsed(nt.Name)
	info, ok := c.types[c.key(nt.Name)]
	if !ok {
		c.fail(nt.Where(), "没有 %s 这个类型", nt.Name)
		return errType
	}
	if len(nt.Args) > 0 {
		c.fail(nt.Where(), "%s 是自定义类型，不带尖括号参数", nt.Name)
	}
	// 返回登记时那个**规范名**而不是写出来的名字：`import { Pair as P }` 之后写 P，
	// 指的还是同一只类型，字段表与形状表只以一个键索引。
	return Type{Kind: info.kind, Name: info.name}
}

func baseType(name string) (Type, bool) {
	for _, t := range []Type{
		{Kind: Bool, Name: "boolean"}, {Kind: Int, Name: "int"}, {Kind: Float, Name: "float"},
		{Kind: Byte, Name: "byte"}, {Kind: Unicode, Name: "unicode"}, {Kind: Str, Name: "string"},
		{Kind: Obj, Name: "object"},
		// Json 是内建的引用类型：与 Calendar 同一做法——编译器发的名字，
		// 文件里写不出（`Json` 在 lexer 的保留名表里），只能经 json 那一组读。
		{Kind: Json, Name: "Json"},
		// bytes 也是内建的引用类型：名字同样在 lexer 的保留名表里，
		// 与 Json 的区别是它可以创建——`bytes.new(容量)`，也能写进容器与字段。
		{Kind: Bytes, Name: "bytes"},
		// 操作系统那两类句柄：创建它们的是 `net.connect`／`net.listen`，
		// 文件里写不出这两只类型名（都在保留名表里）。
		{Kind: Socket, Name: "Socket"},
		{Kind: Listener, Name: "Listener"},
		// 子进程这一档：同一做法——创建它的是 `process.start`，类型名在保留名表里。
		{Kind: Process, Name: "Process"},
	} {
		if t.Name == name {
			return t, true
		}
	}
	return Type{}, false
}

func (c *Checker) checkTypeUsable(t Type, where ast.Pos) {
	if t.Kind == Iface {
		c.fail(where, "%s 是接口名，不能当类型写", bareName(t.Name))
	}
}

func keyReason(t Type) string {
	switch t.Kind {
	case Float:
		return "NaN 不等于自己，这个键永远查不中"
	case Struct, List, Dict, Set:
		return "逐字段比、字段还能改，散列值守不住"
	case Bool, Int, Byte, Unicode, Str, Enum, Class, Obj:
		return ""
	}
	return "这个类型不能当键"
}

// ---- 作用域 ----

type scopeVar struct {
	typ Type
	pos ast.Pos
	// node 是声明它的那个语法节点（*ast.Decl 或 *ast.Param），后端要靠它找槽位。
	node any
	// by 说它是哪一类名字；后端据此决定落帧上、静态池里，还是根本不发内存。
	by BindKind
}

// 作用域用一棵栈表示：每进一个 { } 压一层。
//
// en: Scope is a stack: entering each { } pushes a layer.
type scope struct {
	names map[string]scopeVar
}

func (c *Checker) openScope()  { c.scopes = append(c.scopes, scope{names: map[string]scopeVar{}}) }
func (c *Checker) closeScope() { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *Checker) lookup(name string) (scopeVar, bool) {
	v, _, ok := c.lookupAt(name)
	return v, ok
}

// lookupAt 顺带返回它在第几层作用域：第 0 层就是文件顶层，也就是静态槽。
//
// en: lookupAt also returns which scope layer it was found on: layer 0 is the file top level,
// i.e. a static slot.
func (c *Checker) lookupAt(name string) (scopeVar, int, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if v, ok := c.scopes[i].names[name]; ok {
			return v, i, true
		}
	}
	return scopeVar{}, -1, false
}

func (c *Checker) declare(name string, t Type, at ast.Pos) {
	c.declareKind(name, t, at, nil, BindLocal)
}

func (c *Checker) declareNode(name string, t Type, at ast.Pos, node any) {
	by := BindLocal
	if _, isParam := node.(*ast.Param); isParam {
		by = BindParam
	}
	c.declareKind(name, t, at, node, by)
}

// declareKind 是唯一的落点：by 由调用方给，因为"同一个 *ast.Decl 节点"既可能是顶层
// 静态槽也可能是块里的局部槽，光看节点分不出来。
//
// en: declareKind is the single place this is recorded. The caller supplies by, because "the
// same *ast.Decl node" can be either a top-level static slot or a block-local slot, and the
// node alone cannot tell them apart.
func (c *Checker) declareKind(name string, t Type, at ast.Pos, node any, by BindKind) {
	if len(c.scopes) == 0 {
		return
	}
	top := &c.scopes[len(c.scopes)-1]
	if _, dup := top.names[name]; dup {
		c.fail(at, "这一层里已经有 %s", name)
		return
	}
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if _, outer := c.scopes[i].names[name]; outer {
			c.fail(at, "%s 与外层的名字重名：不做内层顶掉外层", name)
			return
		}
	}
	// 每登记一个新名字，交出那份记录都要从"没有交出去"开始。
	// moved 是按名字查的扁平表，不随作用域关闭回收：上一个函数把它的参数 entry
	// 交了出去，下一个函数的参数也叫 entry 时就会读到上一个函数的记录——
	// 跨文件的例子最先出现（reg.nf 的 note(entry) 的记录被 main.nf 的 shout(entry) 沿用）。
	top.names[name] = scopeVar{typ: t, pos: at, node: node, by: by}
	c.moved[name] = false
}

var _ = math.Abs
var _ = strconv.FormatInt
var _ = strings.HasPrefix

// dedup 抹掉重复报错：循环体检查两遍会让同一条问题报两次。
//
// en: dedup erases repeated reports: a loop body is checked twice, which would otherwise
// report the same problem twice.
func dedup(es []Error) []Error {
	seen := map[string]bool{}
	var out []Error
	for _, e := range es {
		k := e.Error()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// sharedNames 记下 `export shared` 名字：它们不参与交出，也不随作用域析构。
//
// en: sharedNames records the `export shared` names: they take no part in handover and
// are not destroyed when the scope ends.
func (c *Checker) markShared(name string)    { c.shared[name] = true }
func (c *Checker) isShared(name string) bool { return c.shared[name] }

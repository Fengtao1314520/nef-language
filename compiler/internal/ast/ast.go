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

// Package ast 定义 nef 语法树的节点形状。
//
// en: Package ast defines the node shapes of nef's syntax tree.
package ast

// Pos 记一个节点的行与列，报错时要能指回源码。
//
// en: Pos records a node's line and column so that an error can point back into the source.
type Pos struct {
	Line int
	Col  int
}

// File 是一个 .nf 文件。
//
// en: File is one .nf file.
type File struct {
	Path    string
	Imports []*Import
	Defs    []Def  // 顶层的类型、函数、常量声明
	Stmts   []Stmt // 顶层语句流，按书写顺序执行；顶层的 let／const 也在这条语句流里
}

// Import 是 `import { a, b as c } from "./x.nf"`。
//
// en: Import is `import { a, b as c } from "./x.nf"`.
type Import struct {
	Pos     Pos
	Names   []ImportName
	Package string // 整块导入的写法：`import a from "libs/a"` 里的 a。为空表示花括号列举的形式
	Path    string
	PathPos Pos
}

type ImportName struct {
	Name  string
	Alias string // 空表示没改名
	Pos   Pos
}

// Def 是顶层定义的共同接口。
//
// en: Def is the shared interface of top-level definitions.
type Def interface {
	defNode()
	Where() Pos
	Exported() bool
	Name() string
}

// Func 是 `method 名(参数):返回 { 体 }`；写在顶层是函数，写在 class 体内是方法。
//
// en: Func is `method Name(parameters):return { body }`; written at top level it is a function,
// written inside a class body it is a method.
type Func struct {
	Pos      Pos
	Export   bool
	NameText string
	Params   []*Param
	Ret      Type // nil 表示没有返回值
	Body     []Stmt
	IsMethod bool // 在 class 体内
}

type Param struct {
	Pos      Pos
	NameText string
	Typ      Type
	Default  Expr // nil 表示没默认值
	Ref      bool // 参数前写了 ref：只读借用，调用方的实参名字不失效
}

func (f *Func) defNode()       {}
func (f *Func) Where() Pos     { return f.Pos }
func (f *Func) Exported() bool { return f.Export }
func (f *Func) Name() string   { return f.NameText }

// Struct 与 Class 共用一个节点，由 Kind 区分。
//
// en: Struct and Class share one node, told apart by Kind.
type Object struct {
	Pos      Pos
	Kind     string // "struct" 或 "class"
	Export   bool
	NameText string
	Base     Expr // base 后面的接口名，nil 表示没有
	Fields   []*Field
	Methods  []*Func // struct 一律为空
}

type Field struct {
	Pos      Pos
	NameText string
	Typ      Type
}

func (o *Object) defNode()       {}
func (o *Object) Where() Pos     { return o.Pos }
func (o *Object) Exported() bool { return o.Export }
func (o *Object) Name() string   { return o.NameText }

// Interface 是 `interface 名 base 别的接口 { 字段; 方法声明 }`。
//
// en: Interface is `interface Name base OtherInterface { fields; method declarations }`.
type Interface struct {
	Pos      Pos
	Export   bool
	NameText string
	Base     Expr
	Fields   []*Field
	Methods  []*Func // 只有签名，没有体
}

func (i *Interface) defNode()       {}
func (i *Interface) Where() Pos     { return i.Pos }
func (i *Interface) Exported() bool { return i.Export }
func (i *Interface) Name() string   { return i.NameText }

// Enum 是 `enum D { a = 0, b, c }`。
//
// en: Enum is `enum D { a = 0, b, c }`.
type Enum struct {
	Pos      Pos
	Export   bool
	NameText string
	Members  []EnumMember
}

type EnumMember struct {
	Pos      Pos
	NameText string
	Value    Expr // nil 表示没写值，由前一个往上加
}

func (e *Enum) defNode()       {}
func (e *Enum) Where() Pos     { return e.Pos }
func (e *Enum) Exported() bool { return e.Export }
func (e *Enum) Name() string   { return e.NameText }

// TypeAlias 是 `type 名字 = 类型`：给一个已经存在的类型另起一个名字。
//
// 它不是一种新类型。检查器解析类型遇到这个名字时，直接使用右侧的类型——
// 类型表、形状表、判等、交出与装箱都不感知这个名字的存在。
//
// en: TypeAlias is `type Name = SomeType`: it gives an existing type another
// name. It is not a new type. When the checker resolves a type and reaches this name, it simply
// uses the type on the right - the type table, the shape table, equality, handover and boxing
// are never told that this name appeared.
type TypeAlias struct {
	Pos      Pos
	Export   bool
	NameText string
	Typ      Type
}

func (a *TypeAlias) defNode()       {}
func (a *TypeAlias) Where() Pos     { return a.Pos }
func (a *TypeAlias) Exported() bool { return a.Export }
func (a *TypeAlias) Name() string   { return a.NameText }

// Decl 是 `let x:T = v`、`const x:T = v`，或 `export shared x:T = v`。
//
// en: Decl is `let x:T = v`, `const x:T = v`, or `export shared x:T = v`.
type Decl struct {
	Pos      Pos
	Export   bool
	IsConst  bool
	Shared   bool
	NameText string
	Typ      Type
	Value    Expr
}

func (d *Decl) defNode()       {}
func (d *Decl) Where() Pos     { return d.Pos }
func (d *Decl) Exported() bool { return d.Export }
func (d *Decl) Name() string   { return d.NameText }

// Type 是写出来的类型标注。
//
// en: Type is a type annotation as written in the source.
type Type interface {
	typeNode()
	Where() Pos
}

// NamedType 是 `int`、`Circle`、`list<string>` 这一类带名字（可能带尖括号参数）的类型。
//
// en: NamedType is a type that carries a name, possibly with angle-bracket arguments: `int`,
// `Circle`, `list<string>`.
type NamedType struct {
	Pos  Pos
	Name string
	Args []Type
}

func (t *NamedType) typeNode()  {}
func (t *NamedType) Where() Pos { return t.Pos }

// Stmt 是一条语句。StmtPtr 只是让函数体可以用 []*StmtPtr 装指针，省得 AST 里到处写指针。
//
// en: Stmt is one statement. StmtPtr exists only so a function body can be a []*StmtPtr, which
// saves writing pointers everywhere else in the AST.
type Stmt interface {
	stmtNode()
	Where() Pos
}

// Assign 是 `x = v`、`a[0] = v`、`p.x = v`。
//
// en: Assign is `x = v`, `a[0] = v`, `p.x = v`.
type Assign struct {
	Pos    Pos
	Target Expr
	Value  Expr
}

func (s *Assign) stmtNode()  {}
func (s *Assign) Where() Pos { return s.Pos }

// Incdec 是 `x++`、`x--`，只能单独成一条语句。
//
// en: Incdec is `x++` or `x--`, and may only stand as a statement of its own.
type Incdec struct {
	Pos  Pos
	What Expr
	Up   bool
}

func (s *Incdec) stmtNode()  {}
func (s *Incdec) Where() Pos { return s.Pos }

// ExprStmt 是一条由调用、throw、exit 组成的语句。
//
// en: ExprStmt is a statement made of a call, a throw or an exit.
type ExprStmt struct {
	Pos Pos
	E   Expr
}

func (s *ExprStmt) stmtNode()  {}
func (s *ExprStmt) Where() Pos { return s.Pos }

// If 带一串 else if 与可选的 else。
//
// en: If carries a chain of else if branches and an optional else.
type If struct {
	Pos     Pos
	Conds   []Expr
	Bodies  [][]Stmt
	Else    []Stmt
	HasElse bool
}

func (s *If) stmtNode()  {}
func (s *If) Where() Pos { return s.Pos }

type While struct {
	Pos  Pos
	Cond Expr
	Body []Stmt
}

func (s *While) stmtNode()  {}
func (s *While) Where() Pos { return s.Pos }

// For 有两种：`for (i:int in 0 until 10)` 遍历整数区间，`for (v:T in 容器)` 遍历容器。
//
// en: For has two forms: `for (i:int in 0 until 10)` iterates an integer range,
// `for (v:T in container)` iterates a container.
type For struct {
	Pos    Pos
	Var    *Decl // 循环变量的声明形状：类型必写，没有初值
	Ranged bool  // true 表示遍历容器
	From   Expr  // 区间遍历：起点
	Until  Expr  // 区间遍历：终点（不含）
	Iter   Expr  // 容器遍历：被遍历的容器或字符串
	Body   []Stmt
}

func (s *For) stmtNode()  {}
func (s *For) Where() Pos { return s.Pos }

// Switch 的一个分支：几个标签共用一个体。
//
// en: One branch of Switch: several labels share one body.
type Case struct {
	Pos    Pos
	Labels []Expr // nil 表示 default
	Body   []Stmt
}

type Switch struct {
	Pos        Pos
	Head       Expr
	Cases      []*Case
	HasDefault bool
}

func (s *Switch) stmtNode()  {}
func (s *Switch) Where() Pos { return s.Pos }

type Break struct{ Pos Pos }

func (s *Break) stmtNode()  {}
func (s *Break) Where() Pos { return s.Pos }

type Continue struct{ Pos Pos }

func (s *Continue) stmtNode()  {}
func (s *Continue) Where() Pos { return s.Pos }

type Return struct {
	Pos Pos
	Val Expr // nil 表示不带值
}

func (s *Return) stmtNode()  {}
func (s *Return) Where() Pos { return s.Pos }

// Try 是 `try { } catch (code, msg) { }`，没有 finally。
//
// en: Try is `try { } catch (code, msg) { }`, with no finally.
type Try struct {
	Pos      Pos
	Body     []Stmt
	HasCatch bool
	CodeName string
	MsgName  string
	Catch    []Stmt
}

func (s *Try) stmtNode()  {}
func (s *Try) Where() Pos { return s.Pos }

// Expr 是表达式。
//
// en: Expr is an expression.
type Expr interface {
	exprNode()
	Where() Pos
}

type Lit struct {
	Pos   Pos
	Kind  string // "int"、"float"、"string"、"char"、"true"、"false"、"null"
	Text  string // 原始写法（数字含 `_` 与 `0x`）或解码后的文本
	Value int64  // 数字字面量的值
}

func (e *Lit) exprNode()  {}
func (e *Lit) Where() Pos { return e.Pos }

type Name struct {
	Pos  Pos
	Text string
}

func (e *Name) exprNode()  {}
func (e *Name) Where() Pos { return e.Pos }

type Binary struct {
	Pos   Pos
	Op    string
	Left  Expr
	Right Expr
}

func (e *Binary) exprNode()  {}
func (e *Binary) Where() Pos { return e.Pos }

type Unary struct {
	Pos  Pos
	Op   string // "-", "!", "~"
	What Expr
}

func (e *Unary) exprNode()  {}
func (e *Unary) Where() Pos { return e.Pos }

// Convert 是 `x as T`，Test 是 `x is T`。
//
// en: Convert is `x as T`, Test is `x is T`.
type Convert struct {
	Pos  Pos
	What Expr
	Typ  Type
}

func (e *Convert) exprNode()  {}
func (e *Convert) Where() Pos { return e.Pos }

type Test struct {
	Pos  Pos
	What Expr
	Typ  Type
}

func (e *Test) exprNode()  {}
func (e *Test) Where() Pos { return e.Pos }

type Call struct {
	Pos    Pos
	Callee Expr // Name 或 Field
	Args   []Expr
}

func (e *Call) exprNode()  {}
func (e *Call) Where() Pos { return e.Pos }

type Index struct {
	Pos  Pos
	What Expr
	At   Expr
}

func (e *Index) exprNode()  {}
func (e *Index) Where() Pos { return e.Pos }

type Member struct {
	Pos      Pos
	What     Expr
	NameText string
}

func (e *Member) exprNode()  {}
func (e *Member) Where() Pos { return e.Pos }

// StructLit 是 `Point { x: 1, y: 2 }`。
//
// en: StructLit is `Point { x: 1, y: 2 }`.
type StructLit struct {
	Pos    Pos
	Type   string
	Fields []*StructLitField
}

type StructLitField struct {
	Pos      Pos
	NameText string
	Value    Expr
}

func (e *StructLit) exprNode()  {}
func (e *StructLit) Where() Pos { return e.Pos }

// New 是 `new C()` 或 `new C(r: 2.0)`。
//
// en: New is `new C()` or `new C(r: 2.0)`.
type New struct {
	Pos    Pos
	Type   string
	Fields []*StructLitField // 空表示全零
}

func (e *New) exprNode()  {}
func (e *New) Where() Pos { return e.Pos }

// ListLit 是 `[1, 2, 3]`；MapLit 是 `{"a": 1}`；SetLit 是 `{1, 2}`。
//
// en: ListLit is `[1, 2, 3]`; MapLit is `{"a": 1}`; SetLit is `{1, 2}`.
type ListLit struct {
	Pos   Pos
	Items []Expr
}

func (e *ListLit) exprNode()  {}
func (e *ListLit) Where() Pos { return e.Pos }

type MapLit struct {
	Pos   Pos
	Pairs []MapPair
}

type MapPair struct {
	Pos   Pos
	Key   Expr
	Value Expr
}

func (e *MapLit) exprNode()  {}
func (e *MapLit) Where() Pos { return e.Pos }

type SetLit struct {
	Pos   Pos
	Items []Expr
}

func (e *SetLit) exprNode()  {}
func (e *SetLit) Where() Pos { return e.Pos }

// Throw 是 `throw (码, 消息)`，Exit 是 `exit (码, 消息)`。
//
// en: Throw is `throw (code, message)`, Exit is `exit (code, message)`.
type Throw struct {
	Pos  Pos
	Code Expr
	Msg  Expr
}

func (e *Throw) exprNode()  {}
func (e *Throw) Where() Pos { return e.Pos }

type Exit struct {
	Pos  Pos
	Code Expr
	Msg  Expr
}

func (e *Exit) exprNode()  {}
func (e *Exit) Where() Pos { return e.Pos }

// Decl 既能当定义（顶层）也能当语句（函数体内），所以两套方法都实现。
//
// en: Decl serves both as a definition (top level) and as a statement (inside a function body),
// so it implements both sets of methods.
func (d *Decl) stmtNode() {}

// 给带位置的子节点补上 Where()，报错时要能指回源码。
//
// en: Adds Where() to the child nodes that carry a position, so an error can point back into
// the source.
func (f *Field) Where() Pos          { return f.Pos }
func (p *Param) Where() Pos          { return p.Pos }
func (f *StructLitField) Where() Pos { return f.Pos }
func (m *EnumMember) Where() Pos     { return m.Pos }
func (m *MapPair) Where() Pos        { return m.Pos }
func (n *ImportName) Where() Pos     { return n.Pos }

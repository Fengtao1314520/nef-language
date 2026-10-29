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

// desc.go 建立**运行期形状表**：`console.print` 与 `number.to_string` 收到整个容器或盒子时
// 依赖的就是它。
//
// 形状需要带到运行期：`list<int>` 里实际存放的类型、盒子里装的是哪一个类型，编译期无法确定；
// 在编译期按 struct 逐段拼接字段覆盖不了这两类。所以每个可能被打印的类型都登记一条描述，
// 打印机（`nef.print`，即 print.go 里用中间码写出的那个函数）照描述执行。
//
// 描述之间的相互引用一律存"从本条描述算起的字节差"（排布方式与不使用指针的理由见
// internal/codegen/data.go 文件头）：打印机只持有当前这一条描述的地址，加上差值即可到达兄弟
// 描述、类型名、字段数组与类型字表，不必知道整张表在数据段中的位置。
//
// en: desc.go builds the RUNTIME SHAPE TABLE: console.print and number.to_string rely on it
// when they receive a whole container or box. The shape has to reach runtime: what is actually
// inside a `list<int>`, or which type a box holds, is unknown at compile time, and assembling
// the fields per struct at compile time cannot cover those two cases. So every type that might
// be printed gets one description, and the printer (`nef.print`, the function written in
// intermediate code inside print.go) walks by it. Cross references between descriptions always
// store a byte difference measured from the description itself (how they are arranged and why
// no pointer is used is at the top of internal/codegen/data.go): the printer only ever holds
// the address of the current description, and adding a difference reaches the sibling
// description, the type name, the field array and the type-word table without knowing where in
// the data section the whole table sits.
package lower

import (
	"fmt"
	"sort"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/ir"
)

// maxPrintDepth 是打印机递归深度的上限：在环检测之外另加一道深度限制。
// 环由身份链识别，这一道防的是另一种情况：合法但极深的真实数据把输出撑到不可用。
// 两种情况触发时给出同一个记号——读者不需要区分来源，多一种记号只是多一处要解释的规则。
//
// en: maxPrintDepth is the limit on how far the printer walks in: on top of cycle detection
// there is a depth limit. Cycles are recognised by the identity chain;
// this guard is for the other case - legitimate but extremely deep real data flooding the
// output. Both hits produce the same marker: a reader does not care which one triggered it, and
// a second spelling would only add one more thing to explain.
const maxPrintDepth = 32

// printEllipsis 是触发上限时写在该位置的文本。三个 ASCII 点：产物不必先构造 UTF-8，
// 也与 Python／Go 的 `...` 一致。
//
// en: printEllipsis is exactly the text written at that position: three ASCII dots, so the
// product needs no UTF-8 assembled first, and it matches the `...` of Python and Go.
const printEllipsis = "..."

// descOf 返回一个类型的形状描述编号。同一种形状整个程序只占一条描述（`list<int>` 出现一百次，
// 描述还是那一条）。
//
// 先登记再算子：`class A { b:B }` 与 `class B { a:A }` 这种互相引用的类型要有环形的描述才
// 表示得出来——先为自己这条占下编号，递归回到同一类型时拿到的就是它，环在此成型。
// 也因此，原先"两个类型互相引用即在编译期拒绝"的拦截已取消：现在可以打印。
//
// en: descOf returns a type's shape description number. One shape occupies one description in
// the whole program (`list<int>` appearing a hundred times still has that one description).
// Register first, then compute the children: types that point back at each other, `class A {
// b:B }` with `class B { a:A }`, can only be represented by a cyclic description - claiming its
// own number first means the recursion coming back down receives it, and the cycle forms here.
// That is also why the compile-time refusal for "two types pointing back at each other" could
// be removed: it prints now.
func (l *L) descOf(t check.Type) (ir.D, error) {
	// 键用的是带包根的 Key，不是面向读者的 String：工程里的 `Pair`
	// 与包里的 `Pair` 形状不同，共用同一条描述就会按对方的形状去打印、去归还。
	key := t.Key()
	if d := l.p.DescOf(key); d >= 0 {
		return d, nil
	}
	n := l.p.NewDesc(key)
	// 不能把 &l.p.Descs[n] 存成一个指针再慢慢填：往下递归算子描述时切片会扩容换底，
	// 该指针悬在旧数组上，写进去不生效——`list<int>` 的元素形状会指回它自己，运行期按无效
	// 内存遍历。要写就按下标现取。
	d := func() *ir.Desc { return &l.p.Descs[n] }
	switch t.Kind {
	case check.Int, check.Byte, check.Enum:
		// byte 与 enum 都按十进制打印：enum 返回它的数值（单向换算规则），byte 也是 0～255 的数，
		// 只是它在块里占一个字节——种类分开登记，打印机取窄字段时依赖的就是这一点。
		d().Kind = ir.KindInt
		if t.Kind == check.Byte {
			d().Kind = ir.KindByte
		}
	case check.Bool:
		d().Kind = ir.KindBool
	case check.Unicode:
		d().Kind = ir.KindChar
	case check.Float:
		d().Kind = ir.KindFloat
	case check.Str:
		d().Kind = ir.KindString
	case check.List, check.Set:
		if len(t.Args) == 0 {
			return 0, fmt.Errorf("%s 说不清里面装的是什么，无法打印", t)
		}
		if t.Kind == check.List {
			d().Kind = ir.KindList
		} else {
			d().Kind = ir.KindSet
		}
		e, err := l.descOf(t.Args[0])
		if err != nil {
			return 0, err
		}
		d().Elem = e
	case check.Dict:
		if len(t.Args) < 2 {
			return 0, fmt.Errorf("%s 说不清键与值的形状，无法打印", t)
		}
		k, err := l.descOf(t.Args[0])
		if err != nil {
			return 0, err
		}
		v, err := l.descOf(t.Args[1])
		if err != nil {
			return 0, err
		}
		d().Kind, d().Key, d().Elem = ir.KindDict, k, v
	case check.Struct, check.Class:
		tag, ok := l.tagOf(t)
		if !ok {
			return 0, fmt.Errorf("%s 没有类型字，无法打印", t)
		}
		if t.Kind == check.Struct {
			d().Kind = ir.KindStruct
		} else {
			d().Kind = ir.KindClass
		}
		// 打印出来的是原名：包根只用于编译器区分同名类型，不出现在打印输出里。
		d().Tag, d().Name = tag, l.msg(check.BareName(t.Name))
		for _, fi := range l.f.Fields[t.Name] {
			fd, err := l.descOf(fi.Typ)
			if err != nil {
				return 0, err
			}
			d().Fields = append(d().Fields, ir.Field{Name: l.msg(fi.Name), Off: fi.Off, Type: fd})
		}
	case check.Json:
		// 内建的那棵树：形状描述只有一条，内部结构完全由运行期逐位置的种类编号确定。
		// 打印机与析构器各自走 nef.json.render／nef.json.free，所以这条描述不必带字段表或子描述。
		d().Kind = ir.KindJson
	case check.Bytes:
		// 字节块：形状描述同样只有一条——正文固定为一个字节一个位置，
		// 没有"里面装的是哪一种"需要带到运行期。打印与析构分别走 printer／destroy 中各自新增的分支。
		d().Kind = ir.KindBytes
	case check.Socket, check.Listener:
		// 操作系统给的句柄：形状这一侧只需要登记"这是一个句柄"，
		// 打印出来就是它自己的名字。名字的位置复用 struct／class 的那条描述（打印机已能读取），
		// 所以不必为这两类新增描述形式。
		d().Kind = ir.KindHandle
		name := "Socket"
		if t.Kind == check.Listener {
			name = "Listener"
		}
		d().Name = l.msg(name)
	case check.Process:
		// 子进程：打印方式与上面两类完全相同，收尾动作不同——
		// 套接字是关闭一次，这里是"不等候就结束该进程，再关闭句柄"，所以单独一个种类号，
		// 析构器的那张表因此不必在运行期再判断"这是哪一类句柄"。
		d().Kind = ir.KindProc
		d().Name = l.msg("Process")
	case check.Obj:
		// 盒子里是哪一个类型，只有运行期看块头上那 8 字节知道，所以这条描述指向
		// "类型字→形状"那一张表（整程序一张，Run 收尾时填）。
		d().Kind = ir.KindBox
		tt, err := l.tagTable()
		if err != nil {
			return 0, err
		}
		d().Elem = tt
	default:
		return 0, fmt.Errorf("%s 没有打印方式", t)
	}
	return n, nil
}

// tagTable 占下那张"类型字→形状"的表，返回它的编号（第一次要用的时候才占）。
//
// en: tagTable claims the "type word -> shape" table and returns its number (claimed only when
// first needed).
func (l *L) tagTable() (ir.D, error) {
	if l.tagTab >= 0 {
		return l.tagTab, nil
	}
	l.tagTab = l.p.NewDesc("object 的类型字表")
	l.p.Descs[l.tagTab].Kind = ir.KindTagTable
	return l.tagTab, nil
}

// fillTagTable 在全部代码降完之后回头填那张表：凡是能装进盒子的类型都要在表里排一行——
// 五个可装箱的基础值类型加程序里每一个 `struct`／`class`／`enum`。
//
// 顺序按名字排：描述在数据段里的位置因此与文件顺序无关，同一份程序两次编译产物一字不差。
//
// en: fillTagTable goes back to fill that table once all code has been lowered: every type that
// can be put into a box needs a row - the five boxable basic value types plus
// every struct, class and enum in the program. Ordered by name, which makes a description's
// position in the data section independent of file order, so compiling the same program twice
// produces byte-identical output.
func (l *L) fillTagTable() error {
	if l.tagTab < 0 {
		return nil // 本程序没有打印过盒子，无需填表
	}
	prims := []check.Type{
		{Kind: check.Int}, {Kind: check.Float}, {Kind: check.Bool}, {Kind: check.Byte}, {Kind: check.Unicode},
	}
	names := make([]string, 0, len(l.tags))
	for n := range l.tags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t, ok := l.namedType(n)
		if !ok {
			continue // 这个名字没有登记形状（只有 `object` 自身会到达这里）
		}
		if err := l.addTag(t); err != nil {
			return err
		}
	}
	for _, t := range prims {
		if err := l.addTag(t); err != nil {
			return err
		}
	}
	return nil
}

func (l *L) addTag(t check.Type) error {
	tag, ok := l.tagOf(t)
	if !ok {
		return nil
	}
	d, err := l.descOf(t)
	if err != nil {
		return err
	}
	l.p.Descs[l.tagTab].Tags = append(l.p.Descs[l.tagTab].Tags, ir.TagRef{Tag: tag, Desc: d})
	return nil
}

// namedType 按名字找回一个具名类型（struct 还是 class，由降级侧从 AST 记录）。
//
// en: namedType finds a named type back by name (struct or class - copied off the AST by the
// lowering side).
func (l *L) namedType(name string) (check.Type, bool) {
	kind, ok := l.objKind[name]
	if !ok {
		return check.Type{}, false
	}
	return check.Type{Kind: kind, Name: name}, true
}

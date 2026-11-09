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

package check

import (
	"strings"

	"neflang.org/nf/internal/ast"
)

// 这一份文件管泛型的第一层：把一份带类型参数的声明与尖括号里填进来的类型合成一只具体的实例。
//
// 实例的身份用一个键说清：`Box<int>` 与 `Box<string>` 是两个键，字段表、形状表与类型字各占一条。
// 键里带上包根（`libs/a/Box<int>`），给人看的那一串把包根去掉、每一层实参也按给人看的写法排。
// 这与那只同名的 `Pair` 用的是同一条分工：机器比的是键，屏幕与报错上看的是给人看的那一串。
//
// 实例什么时候生成：用到的那一侧现生成。检查这一步走到一个带类型参数的类型名时，先把实参解出来，
// 再看这只实例在不在名字表里；不在就登记一只、把字段摊一遍。方法体按实例各生成一份（段 2 那一半），
// 所以登记下来的实例带形状也带方法。
//
// en: This file holds the first layer of generics: it turns a declaration with type parameters
// and the types written in its angle brackets into one concrete instance.
//
// An instance's identity is one key: `Box<int>` and `Box<string>` are two keys, each with its own
// field table, shape entry and type word. The key carries the package root (`libs/a/Box<int>`);
// the reader-facing string drops the root and renders every argument the way readers see it. That
// is the same split that keeps a project's `Pair` apart from a package's: the machine compares
// keys, the screen and the error text show the display string.
//
// When is an instance built? On the using side, when it is needed. Reaching a type name that has
// type parameters, the checker resolves the arguments first, then looks for this instance in the
// name table; a missing one is registered and its fields flattened. No method body exists per
// instance yet, so an instance here carries a shape and nothing else.

// instanceKey 把模板的规范名与实参类型拼成实例的键：`Box` 加 `int` 得到 `Box<int>`，
// 包里的模板得到 `libs/a/Box<int>`。实参那一侧用 Key()，两只同名不同类型的实例才分得开。
//
// en: instanceKey joins a template's canonical name with its argument types: `Box` plus `int`
// gives `Box<int>`, a template from a package gives `libs/a/Box<int>`. The arguments go through
// Key(), which is what keeps two same-named instances of different shapes apart.
func instanceKey(tmplName string, args []Type) string {
	var b strings.Builder
	b.WriteString(tmplName)
	b.WriteByte('<')
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.Key())
	}
	b.WriteByte('>')
	return b.String()
}

// instanceDisplay 是给人看的那一串：去掉包根，每一层实参也按给人看的写法排。
// 参数列表为空时返回原串——没有类型参数的类型走的还是 bareName 那一条。
//
// en: instanceDisplay is the reader-facing string: the package root is dropped and every
// argument renders the way readers see it. With no arguments it returns the input unchanged,
// so a type without type parameters still goes through bareName.
func instanceDisplay(key string, args []Type) string {
	if len(args) == 0 {
		return bareName(key)
	}
	base := key
	if i := strings.Index(base, "<"); i >= 0 {
		base = base[:i]
	}
	var b strings.Builder
	b.WriteString(bareName(base))
	b.WriteByte('<')
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.String())
	}
	b.WriteByte('>')
	return b.String()
}

// pushTypeEnv 把一份类型参数对照表压到栈顶：`Item` 对应 `int` 这样一条，只在摊这一只实例的
// 字段时有效。栈是因为实例可以套实例——摊 `Holder<list<Item>>` 内层那一只时，外面这一层的
// 对照表还得留着，不然内层再遇到 `Item` 就解不出来。
//
// en: pushTypeEnv puts one parameter mapping, such as `Item` standing for `int`, on top of the
// stack: it holds while this instance's fields are flattened. The structure is a stack because
// instances nest - flattening the inner one of `Holder<list<Item>>` must keep the outer mapping
// alive or a later `Item` cannot be resolved.
func (c *Checker) pushTypeEnv(env map[string]Type) {
	c.typeEnv = append(c.typeEnv, env)
}

// popTypeEnv 返回栈顶那一份对照表并把它揭掉。
//
// en: popTypeEnv returns the mapping on top of the stack and removes it.
func (c *Checker) popTypeEnv() map[string]Type {
	top := c.typeEnv[len(c.typeEnv)-1]
	c.typeEnv = c.typeEnv[:len(c.typeEnv)-1]
	return top
}

// lookupTypeParam 查现在这一层里那个名字是不是类型参数，是的话返回它此刻对应的那一只类型。
// 栈从顶往下找：最内层那一份对照表说话算数。
//
// en: lookupTypeParam asks whether this name is a type parameter in the current stretch and,
// if so, returns the type it stands for now. The stack is searched from the top: the innermost
// mapping wins.
func (c *Checker) lookupTypeParam(name string) (Type, bool) {
	for i := len(c.typeEnv) - 1; i >= 0; i-- {
		if t, ok := c.typeEnv[i][name]; ok {
			return t, true
		}
	}
	return Type{}, false
}

// checkTypeArguments 判定尖括号里填进来的每一只类型能不能用。判据与内建容器那一条完全相同，
// 所以三句拒绝用的还是现成的话：`Json`、操作系统那三种句柄、函数值进不了一个实例的位置，
// 理由与进不了容器那一条是同一个（那个位置的归还与打印要靠形状表与类型字，这几样都没有）。
//
// containerName 是给人看的模板名（`Box`），报出来的就是“`Json` 进不了 `Box`：……”这一串。
//
// en: checkTypeArguments decides whether every type written in the angle brackets can be used.
// The test is the built-in containers' own, so the three refusals stay the existing sentences:
// Json, the three operating-system handles and a function value do not enter an instance's
// position, for the same reason they do not enter a container - that position's release and
// printing depend on the shape table and the type word, and those types have neither.
//
// containerName is the template's reader-facing name (`Box`), so the report reads "Json cannot
// go into Box: ...".
func (c *Checker) checkTypeArguments(args []Type, containerName string, where ast.Pos) {
	for _, a := range args {
		c.checkTypeUsable(a, where)
		// `Json` 头上没有运行期的类型字（`lower.go` 的 `tagOf`），那个位置的形状与析构
		// 都要靠它：不作处理的话，打印出来会是 `[null, null]` 这种静默的错误结果。
		if a.Kind == Json {
			c.fail(where, "%s 进不了 %s：先把它拆成自己要的值，或者存那段文本", a, containerName)
		}
		// Socket 与 Listener 同理：那个位置要的是能被完整持有并一路析构的一块内存，
		// 而句柄的释放方式是调用一次关闭；两只名字指着同一个句柄会导致它被关闭两次。
		// Process 也在这一条上：它同样是调用一次关闭，而且它并不是一块内存。
		if IsSystemHandle(a.Kind) {
			c.fail(where, "%s 进不了 %s：它是操作系统给的句柄，只待在名字、实参与返回值里", a, containerName)
		}
		// 函数值那个位置装的是机器代码的入口地址：它没有运行期的类型字，也打印不出来，
		// 所以与 Json 走的是同一档处理——先拒在检查这一层，不留到发码那一步。
		if a.Kind == FuncVal {
			c.fail(where, "%s 进不了 %s：函数值只待在名字、实参、返回值与字段里", a, containerName)
		}
	}
}

// instantiate 登记或取出一只实例。走到这里时模板已经登记在名字表里，`nt` 是写出来的那一个
// 类型名（带尖括号的那些实参）。
//
// 三条顺序固定的动作：实参个数对不对 → 实参解出来、能不能填 → 这只实例登记过没有。
// 没登记过的那一只才摊字段：摊的时候要站在定义模板的那一份文件的上下文里（字段里写的别的类型名
// 是那份文件的名字），并且把类型参数对照表压上栈顶，让 `Content` 解成实参那一只类型。
// 这一段只摊字段，不摊方法：方法体里的语句要按实例各生成一份才谈得上检查。
//
// en: instantiate registers or fetches one instance. The template is already in the name table;
// nt is the type name as written, with its arguments in the angle brackets.
//
// Three steps in a fixed order: does the argument count match, do the arguments resolve and may
// they be filled, has this instance been registered. Only an unregistered instance gets its
// fields flattened - and flattening must happen in the context of the file that defined the
// template (a field naming another type means that file's type) with the parameter mapping on
// top of the stack, so `Content` resolves to the argument's own type. This layer flattens
// fields only, not methods: a method body's statements have to exist per instance before they
// can be checked at all.
func (c *Checker) instantiate(tmpl *typeInfo, nt *ast.NamedType, where ast.Pos) Type {
	want := len(tmpl.typeParams)
	if c.displayPass {
		// 编辑器那一趟签名表：按写出来的实参拼一串给人看的名字就完事，不登记实例、不摊字段、
		// 不排队方法体，也不报错——那一只 `Counter<T>` 从来不会被程序用到，发码发它就是多余的产物。
		// The editor's signature pass: render the written arguments into a reader-facing name and
		// stop - no instance registered, no fields flattened, no body queued, no report. That
		// `Counter<T>` is never used by a program, so emitting code for it would be dead weight.
		args := make([]Type, 0, want)
		for _, a := range nt.Args {
			args = append(args, c.resolveType(a, where))
		}
		return Type{Kind: tmpl.kind, Name: instanceKey(tmpl.name, args), Args: args}
	}
	if len(nt.Args) != want {
		c.fail(where, "%s 需要 %d 个尖括号参数，实际 %d 个", nt.Name, want, len(nt.Args))
		return errType
	}
	args := make([]Type, 0, want)
	for _, a := range nt.Args {
		args = append(args, c.resolveType(a, where))
	}
	c.checkTypeArguments(args, bareName(tmpl.name), where)
	key := instanceKey(tmpl.name, args)
	if made, ok := c.types[key]; ok {
		return Type{Kind: made.kind, Name: made.name, Args: args}
	}
	// 声明要复制一份：字段与方法的节点都换成新的。两份实例共用同一个方法节点时，
	// 检查器那六张按语法节点存档的表会让后一份读到前一份的类型结论，
	// 生成的就是错的机器码，不是难看一句报错。
	// The declaration gets copied so fields and methods arrive as fresh nodes. With two
	// instances sharing one method node, the checker's six node-keyed tables make the second
	// instance read the first one's conclusions, and what comes out is wrong machine code, not
	// an ugly message.
	obj := ast.CloneObject(tmpl.obj)
	obj.TypeParams = nil
	info := &typeInfo{
		file: tmpl.file,
		name: key,
		kind: tmpl.kind,
		obj:  obj,
	}
	c.types[key] = info
	env := map[string]Type{}
	for i, tp := range tmpl.typeParams {
		env[tp.Name] = args[i]
	}
	info.env = env
	// 方法自己还带类型参数的那一条不在这里生成：它的实参要看那一次调用给的是什么，
	// 由调用处现算（instantiateCallable）。这一份实例自己能落的那些方法才进 Instances 与队列。
	// A method carrying its own type parameters is not generated here: its arguments depend on
	// what that one call passes, so the call site builds it (instantiateCallable). Only the
	// methods this instance can lower on its own enter Instances and the queue.
	var own []*ast.Func
	for _, m := range obj.Methods {
		if len(m.TypeParams) > 0 {
			continue
		}
		own = append(own, m)
	}
	if c.facts != nil {
		c.facts.Instances[key] = Instance{Kind: tmpl.kind, File: tmpl.file, Methods: own}
	}
	// 与解类型别名那一段同一条动作：换到定义它的那一份文件的上下文里解，解完原样换回来；
	// 这一趟不把内层用上的 import 名字记到引用方的账上。
	prevFile, prevImported := c.file, c.imported
	if tmpl.file != c.file {
		c.imported = map[string]ImportedName{}
	}
	c.file = tmpl.file
	c.pushTypeEnv(env)
	c.layout(key)
	c.popTypeEnv()
	c.file, c.imported = prevFile, prevImported
	// 方法体不当场查：这一笔是在别人的函数体里生成的，作用域栈与当前函数都还是那一层的。
	// 排队等所有文件的语句流查完再统一查（checkPendingBodies）。
	// Bodies do not get checked on the spot: this instance appeared inside someone else's
	// function, whose scope stack and current function are still the active ones. They wait
	// for the queue to drain once every file's statements have been checked
	// (checkPendingBodies).
	for _, m := range own {
		c.pending = append(c.pending, pendingBody{file: tmpl.file, owner: key, fn: m, env: env, method: true})
	}
	return Type{Kind: info.kind, Name: info.name, Args: args}
}

// genericTemplate 判这份记录是不是一个带类型参数的声明本身（不是某一只实例）。
// 模板只有一份名字表里的记录，它自己不摊字段——字段里 `Content` 那一个名字
// 要配上实参才认得出是什么。
//
// en: genericTemplate says whether this record is the declaration itself, not an instance.
// A template holds one entry in the name table and never flattens its own fields: a field
// written `Content` only names a type once an argument is attached to it.
func genericTemplate(info *typeInfo) bool {
	return info != nil && len(info.typeParams) > 0
}

// drainPending 查完排在队里的实例方法体。
//
// onlyPath 不是空串时只查那一份文件里定义的那一批模板的实例——Run 查完一份文件的语句流就调一次，
// 这样"import 了却没用到"那一句要等到实例的方法体也算上之后才报。空串是把剩下的全部查完。
// 一批里查完又生出新的实例，就再来一批，直到这一队空为止。实例的份数由程序里写出的实参种类
// 决定，是有限的；那一个轮数上限只防排队去重写错时把编译卡死，那种情况是编译器的错，
// 不是用户写得出来的话。
//
// en: drainPending checks the instance method bodies sitting in the queue.
//
// When onlyPath is not empty it drains only the instances whose template is defined in that
// file - Run calls it right after a file's statements, so "imported but unused" is judged only
// after those bodies count as uses. The empty string drains everything still queued. A batch
// that mints further instances is followed by another batch until the queue empties. The number
// of instances is bounded by the argument types a program writes; the round limit only stops a
// build from hanging when the queue's deduplication is broken, which is a compiler fault, not a
// sentence a user can produce.
func (c *Checker) drainPending(onlyPath string) {
	rounds := 0
	for len(c.pending) > 0 {
		batch := c.pending
		c.pending = nil
		groups := map[string][]pendingBody{}
		var order []string
		var rest []pendingBody
		for _, b := range batch {
			if onlyPath != "" && b.file.Path != onlyPath {
				rest = append(rest, b)
				continue
			}
			if _, ok := groups[b.file.Path]; !ok {
				order = append(order, b.file.Path)
			}
			groups[b.file.Path] = append(groups[b.file.Path], b)
		}
		for _, path := range order {
			c.checkPendingGroup(groups[path])
		}
		c.pending = append(rest, c.pending...)
		if len(order) == 0 {
			break
		}
		rounds++
		if rounds > 1000 {
			panic("实例方法体那一队排不完：同一只实例的方法被重复排队了")
		}
	}
}

// checkPendingGroup 在定义这批实例的那一份文件的上下文里，把这一批方法体逐个查一遍。
// 换文件要换的那几样（引进来的名字、包名、用上了没有、顶层那些声明）在这里重摆，
// 查完原样换回去——这一批之后还要接着查别份文件那一批。
//
// 顶层那一层作用域用 bindFileTop 搭，与普通文件那一趟同一份规矩；类型参数对照表按每一笔
// 各自压上栈顶，因为同一份文件里可以有几只不同的实例（`Box<int>` 与 `Box<string>`）。
//
// en: checkPendingGroup runs one batch of instance bodies in the context of the file that
// defines their templates. The state a file switch carries (imported names, package bindings,
// which were used, the top-level declarations) is re-seated here and restored afterwards,
// because the next batch may belong to another file.
//
// The top-level scope comes from bindFileTop, the same rules an ordinary file pass uses; each
// entry pushes its own parameter mapping, since one file can hold several instances
// (`Box<int>` and `Box<string>`) whose bodies differ in what `Content` means.
func (c *Checker) checkPendingGroup(group []pendingBody) {
	f := group[0].file
	// 正在查的那一份文件自己那一层顶层作用域还开着（Run 里查完一份文件的语句流就排一次队），
	// 这一批直接用它——再搭一层就成了内层顶掉外层，顶层那些 let 会被当成重名。
	if f == c.file {
		for _, b := range group {
			c.checkOnePendingBody(b)
		}
		return
	}
	prevFile, prevImported, prevPackages := c.file, c.imported, c.packages
	prevUsedImp, prevUsedPkg, prevHoisted := c.usedImp, c.usedPkg, c.hoistedDecl
	c.file = f
	c.hoistedDecl = map[*ast.Decl]bool{}
	c.checkImports(f)
	c.bindFileTop(f)
	for _, b := range group {
		c.checkOnePendingBody(b)
	}
	c.closeScope()
	c.file, c.imported, c.packages = prevFile, prevImported, prevPackages
	c.usedImp, c.usedPkg, c.hoistedDecl = prevUsedImp, prevUsedPkg, prevHoisted
}

// checkOnePendingBody 查一笔：压上那一只实例的类型参数对照表，按查普通方法体那一条路走一遍。
//
// en: checkOnePendingBody checks one entry: it pushes that instance's parameter mapping and runs
// the ordinary method-body path over it.
func (c *Checker) checkOnePendingBody(b pendingBody) {
	c.pushTypeEnv(b.env)
	fi := funcInfo{
		file:   b.file,
		fn:     b.fn,
		ret:    c.retOf(b.fn),
		params: c.paramsOf(b.fn),
		method: b.method,
		owner:  b.owner,
		key:    b.owner + "." + b.fn.NameText,
	}
	if !b.method {
		fi.owner, fi.key = "", b.owner
	}
	c.checkFuncBody(fi)
	c.popTypeEnv()
}

// instantiateCallable 生成或取出一只可调用实例：带类型参数的顶层函数、带自己类型参数的方法，
// 以及泛型类里那些方法在实例之外再带一层类型参数的那一种，都走这一处。
//
// baseKey 是模板的键：顶层函数是它的规范名（`twice`），方法是"所属类实例名.方法名"
// （`Cell<int>.take`）。实参的来源有两条：调用处写出来的尖括号，或者按实参类型推出来的一张对照表。
// 副本的名字要带上实参那一段，理由与实例的规范名同一条——`internal/ir/ir.go` 的 `Prog.Func`
// 同名即复用、`internal/codegen/emit.go` 同名标号后写覆盖先写，两处都不报错。
//
// en: instantiateCallable builds or fetches one callable instance: a top-level function with
// type parameters, a method with its own type parameters, and a class-body method carrying a
// second layer of parameters on top of its class's.
//
// baseKey is the template's key: the canonical name for a function (`twice`), or
// instance-of-class dot method for a method (`Cell<int>.take`). Arguments come from either the
// brackets written at the call or a mapping inferred from the argument types. The copy's name
// grows the arguments, for the same reason an instance's canonical name does - Prog.Func in
// internal/ir/ir.go reuses a name it has seen and emit.go in internal/codegen lets the later
// label overwrite the earlier one, and neither complains.
func (c *Checker) instantiateCallable(tmplFile *ast.File, baseKey string, tmplFn *ast.Func, outer map[string]Type, ownerKey string, explicit []ast.Type, argTypes []Type, where ast.Pos) (funcInfo, bool) {
	names := map[string]bool{}
	for _, tp := range tmplFn.TypeParams {
		names[tp.Name] = true
	}
	env := map[string]Type{}
	for k, v := range outer {
		env[k] = v
	}
	var args []Type
	if explicit != nil {
		if len(explicit) != len(tmplFn.TypeParams) {
			c.fail(where, "%s 需要 %d 个尖括号参数，实际 %d 个", tmplFn.NameText, len(tmplFn.TypeParams), len(explicit))
			return funcInfo{}, false
		}
		for _, a := range explicit {
			args = append(args, c.resolveType(a, where))
		}
		for i, tp := range tmplFn.TypeParams {
			env[tp.Name] = args[i]
		}
	} else {
		for i, p := range tmplFn.Params {
			if i >= len(argTypes) {
				break
			}
			c.bindOneTypeParam(p.Typ, argTypes[i], names, env)
		}
		for _, tp := range tmplFn.TypeParams {
			t, ok := env[tp.Name]
			if !ok {
				c.fail(where, "从这些实参推不出 %s 的类型参数 %s", tmplFn.NameText, tp.Name)
				return funcInfo{}, false
			}
			args = append(args, t)
		}
	}
	key := baseKey + callableSuffix(args)
	if made, ok := c.callables[key]; ok {
		return made, true
	}
	clone := ast.CloneFunc(tmplFn)
	clone.TypeParams = nil
	clone.NameText = tmplFn.NameText + callableSuffix(args)
	prevFile, prevImported := c.file, c.imported
	if tmplFile != c.file {
		c.imported = map[string]ImportedName{}
	}
	c.file = tmplFile
	c.pushTypeEnv(env)
	fi := funcInfo{
		file:    tmplFile,
		fn:      clone,
		ret:     c.retOf(clone),
		params:  c.paramsOf(clone),
		method:  ownerKey != "",
		owner:   ownerKey,
		key:     key,
		laidOut: true,
	}
	c.popTypeEnv()
	c.file, c.imported = prevFile, prevImported
	c.callables[key] = fi
	if c.facts != nil {
		c.facts.Funcs[key] = clone
		c.facts.Sigs[key] = Signature{Params: fi.params, Ret: fi.ret, Method: fi.method, Owner: ownerKey}
		c.facts.InstFuncs[key] = InstFunc{File: tmplFile, Fn: clone, Owner: ownerKey}
	}
	// 体不当场查：与实例的方法体同一队（owner 顶层函数记的是实例自己的键，方法是所属类实例名）。
	// The body is not checked on the spot - it joins the same queue as instance method bodies
	// (a function's entry carries the instance's own key in owner, a method's carries the class
	// instance).
	c.pending = append(c.pending, pendingBody{file: tmplFile, owner: key, fn: clone, env: env, method: ownerKey != ""})
	if ownerKey != "" {
		c.pending[len(c.pending)-1].owner = ownerKey
	}
	return fi, true
}

// callableSuffix 把实参排成那一段带尖括号的串：`<int, string>`。一个参数也没有时返回空串。
// 这里用 Key()（机器比的那一串），因为这一个后缀要同时当名字表里的键与 IR 的函数名用。
//
// en: callableSuffix renders the arguments as `<int, string>` and returns the empty string when
// there are none. It uses Key() - the machine-facing string - because this suffix serves both
// as a name-table key and as the IR function name.
func callableSuffix(args []Type) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('<')
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.Key())
	}
	b.WriteByte('>')
	return b.String()
}

// paramPlaceholders 给编辑器排一份占位的对照表：`Element` 解出来还是"Element"那一个名字。
// 这一份只进 `Facts.Sigs` 那一张给编辑器读的表，别的读的地方看到的还是没摊开的模板那一笔——
// 占位那一只类型没有形状、没有类型字，进不了后端，也不该进。
//
// en: paramPlaceholders builds a display-only mapping so `Element` resolves to the name
// "Element". This feeds only Facts.Sigs, the table the editor reads; everywhere else still sees
// the unflattened template. The placeholder has no shape and no type word, so it cannot reach
// the backend and must not.
func paramPlaceholders(tps []*ast.TypeParam) map[string]Type {
	out := map[string]Type{}
	for _, tp := range tps {
		out[tp.Name] = Type{Kind: Struct, Name: tp.Name}
	}
	return out
}

// displaySignature 摊一份模板自己的签名，只给编辑器那一栏读。三样都要在这一趟结束时收回去：
// 站在定义它的那一份文件的上下文里解（包里定义的名字在工程的上下文里指向别处，`Counter<T>`
// 那种返回类型在别的文件里根本解不出来），带尖括号的类型不生成实例（displayPass 那一条），
// 而且这一趟一句报错都不留——类型参数没配上实参时说不了对错。
// 占位按传进来的那一张表：模板里的方法要把类那一层与它自己那一层合在一起才解得全。
//
// en: displaySignature flattens the template's own signature, for the editor's column only.
// Three things get put back when it ends: the types resolve in the context of the file that
// defines them (a name defined in a package points elsewhere in the project's context, so a
// return type like Counter<T> cannot be resolved from another file), an argument list mints no
// instance (that displayPass branch), and no report survives this pass - with no argument
// attached, a type parameter says nothing right or wrong. Placeholders come from the table passed
// in: a method inside a template needs the class's layer merged with its own to resolve them all.
func (c *Checker) displaySignature(fi funcInfo, tps []*ast.TypeParam) Signature {
	prevFile, prevImported := c.file, c.imported
	prevErrs, prevDisplay := len(c.errs), c.displayPass
	if fi.file != c.file {
		c.imported = map[string]ImportedName{}
	}
	c.file = fi.file
	c.displayPass = true
	c.pushTypeEnv(paramPlaceholders(tps))
	sg := Signature{Params: c.paramsOf(fi.fn), Ret: c.retOf(fi.fn), Method: fi.method, Owner: fi.owner}
	c.popTypeEnv()
	c.file, c.imported = prevFile, prevImported
	c.displayPass = prevDisplay
	c.errs = c.errs[:prevErrs]
	return sg
}

// bindOneTypeParam 从"写出来的参数类型"与"实参求出来的类型"这一对里收类型参数的对应关系。
// 写出来的是这一个可调用自己的类型参数名字时，第一次见到就绑上；已经绑过的留给后面那一步去比
// （两处实参不一致时报的是现成的类型不一致那一句，不另写一句）。
// 写出来的是一段带尖括号的嵌套（`list<Item>`、`Box<Item, int>`）时，位置对位置往下递；
// 底下那一个名字要与实参那一只的基名对得上才递得下去，对不上就这里什么都不绑。
//
// en: bindOneTypeParam collects parameter-to-argument correspondences from one pair: the type as
// written in the parameter and the type the argument evaluated to. When the written name is one
// of this callable's own parameters, the first sighting binds it; a later one is left to the
// check below (two arguments disagreeing report the existing type-mismatch sentence rather than
// a new one). A written nested form such as `list<Item>` or `Box<Item, int>` descends position by
// position, but only when its base name matches the argument's own; otherwise nothing binds.
func (c *Checker) bindOneTypeParam(written ast.Type, actual Type, names map[string]bool, env map[string]Type) {
	nt, ok := written.(*ast.NamedType)
	if !ok {
		return
	}
	if names[nt.Name] {
		if _, had := env[nt.Name]; !had {
			env[nt.Name] = actual
		}
		return
	}
	if len(nt.Args) == 0 || len(actual.Args) != len(nt.Args) {
		return
	}
	if CallableBase(actual.Name) != nt.Name {
		return
	}
	for i, a := range nt.Args {
		c.bindOneTypeParam(a, actual.Args[i], names, env)
	}
}

// CallableBase 交出一个规范名去掉尾部尖括号那一段之后的基名：`libs/a/Box<int>` → `libs/a/Box`。
// 方法那一种键（`Cell<int>.get`）需要先按最后一个点拆开再对类型那一半调它：
// 这一条只认第一个尖括号，整串进来会把方法名那一段丢掉。
//
// en: CallableBase strips the trailing bracket section from a canonical name:
// `libs/a/Box<int>` becomes `libs/a/Box`. A method key (`Cell<int>.get`) must be split at the
// last dot first and only the type half passed here: this strips at the first angle bracket,
// so the whole string would lose the method name.
func CallableBase(key string) string {
	if i := strings.Index(key, "<"); i >= 0 {
		return key[:i]
	}
	return key
}

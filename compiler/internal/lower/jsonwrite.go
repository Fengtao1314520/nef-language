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

// jsonwrite.go 是 json 那一族的**构造与写入**：空对象与空数组、把程序里的值变成一棵树、
// 往树上放一个位置与拿走一个位置。读取与渲染在 jsonops.go，递归下降在 jsonparse.go。
//
// 程序里的值变成树靠的是运行期形状表：`list<int>` 那个位置里装的到底是什么，编译期说得出，
// 但把它逐格放进树上要在跑起来的时候做，与打印机（`nef.print`）是同一套走法、同一张表。
// 因此 `nef.json.of` 的分支表照着 print.go 排，只是每个分支交的是一个节点而不是一段文本。
//
// en: jsonwrite.go is the construction and writing side of the json family: empty objects and
// arrays, turning a program's value into a tree, putting one position on the tree and taking one
// off. The read-outs and rendering live in jsonops.go, the recursive descent in jsonparse.go. A
// program value becomes a tree through the runtime shape table: what a `list<int>` actually
// holds is known at compile time, but placing it slot by slot into a tree happens at run time,
// so this is the same walk and the same table the printer (`nef.print`) uses. nef.json.of
// therefore mirrors print.go's branch table, except that every branch hands back a node instead
// of a text.
package lower

import (
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

const (
	fnJSONOf        = "nef.json.of"         // (值, 形状描述) → 一个节点
	fnJSONOfInt     = "nef.json.of_int"     // (整数) → 数节点，两个面各存一份
	fnJSONOfFloat   = "nef.json.of_float"   // (64 位位型) → 数节点
	fnJSONOfBool    = "nef.json.of_bool"    // (0／1) → boolean 节点
	fnJSONOfText    = "nef.json.of_text"    // (文本) → 文本节点，正文自己拷一份
	fnJSONKeyText   = "nef.json.key_text"   // (键值, 形状描述) → 对象那把键的文本
	fnJSONNewObject = "nef.json.new_object" // () → 空对象
	fnJSONNewArray  = "nef.json.new_array"  // () → 空数组
	fnJSONSet       = "nef.json.set"        // (对象, 键, 节点) → 没东西
	fnJSONPush      = "nef.json.push"       // (数组, 节点) → 没东西
	fnJSONInsertAt  = "nef.json.insert_at"  // (数组, 位置, 节点) → 没东西
	fnJSONReplaceAt = "nef.json.replace_at" // (数组, 位置, 节点) → 没东西，换下来的拆掉
	fnJSONDelKey    = "nef.json.remove_key" // (对象, 键) → 真／假
	fnJSONDelAt     = "nef.json.remove_at"  // (数组, 位置) → 摘下来的那一个节点
	fnJSONClearNode = "nef.json.clear_node" // (节点) → 没东西，树里的下属全拆干净
)

// jsonFnsWrite 是这一族新加的十五支，跟着读取那一族一起排（见 json.go 的 jsonFns）。
//
// en: jsonFnsWrite is the fifteen new entries of this group, laid out together with the
// read-outs (see jsonFns in json.go).
var jsonFnsWrite = []string{
	fnJSONOf, fnJSONOfInt, fnJSONOfFloat, fnJSONOfBool, fnJSONOfText, fnJSONKeyText,
	fnJSONNewObject, fnJSONNewArray, fnJSONSet, fnJSONPush, fnJSONInsertAt, fnJSONReplaceAt,
	fnJSONDelKey, fnJSONDelAt, fnJSONClearNode,
}

// maxJSONDepth 是 `nef.json.of` 走原生对象时的层数上限，与打印机同一条：环由身份链识别，
// 这一道防的是合法但极深的数据。两种情况都交一个 null 节点——那里没有可写成 JSON 的内容，
// 与打印那一侧的 `...` 是同一个答复，只是这一边交的是节点，不能是一段文本。
//
// en: maxJSONDepth limits how far nef.json.of walks a native value, the printer's rule again:
// cycles come from the identity chain and this guard is for legitimate but extremely deep data.
// Both cases hand back a null node - there is nothing there to write as JSON - which is the
// printer's `...` answer, except that this side returns a node and cannot return a text.
const maxJSONDepth = maxPrintDepth

// buildJSONWrite 排出写入这一族的十五支（重复调用不做处理）。
//
// en: buildJSONWrite lays out the fifteen write-side functions (calling it again does nothing).
func buildJSONWrite(l *L) {
	for _, name := range jsonFnsWrite {
		l.p.Func(name)
	}
	jsonBuildOfInt(l)
	jsonBuildOfFloat(l)
	jsonBuildOfBool(l)
	jsonBuildOfText(l)
	jsonBuildKeyText(l)
	jsonBuildNewObject(l)
	jsonBuildNewArray(l)
	jsonBuildSet(l)
	jsonBuildPush(l)
	jsonBuildInsertAt(l)
	jsonBuildReplaceAt(l)
	jsonBuildDelKey(l)
	jsonBuildDelAt(l)
	jsonBuildClearNode(l)
	jsonBuildOf(l)
}

// guardNode 是"这一路上已经走过这一块"那一步：与打印机的 guarded 同一张走法，
// 只是撞回来或者深过上限时交的是 null 节点而不是三个点。
//
// en: guardNode is the "this block is already on the path" step - the printer's guarded walk
// with a different answer: a null node instead of three dots when the identity repeats or the
// limit is reached.
func guardNode(j *jb, ident ir.Val, body func() ir.Val) ir.Val {
	out := j.t()
	yes, hit, done, top := j.fresh(), j.fresh(), j.fresh(), j.fresh()

	tr := j.t()
	got, after := j.fresh(), j.fresh()
	j.mov(tr, j.g(j.s.trail))
	j.br("!=", tr, j.zero(), got)
	fresh := j.t()
	j.emit(&ir.Call{D: fresh, HasD: true, Fn: "@alloc",
		Args: []ir.Val{j.num(int64(maxJSONDepth) * 8), j.num(0)},
		FT:   []ir.T{ir.TInt, ir.TInt}, RT: ir.TInt})
	j.setG(j.s.trail, fresh)
	j.mov(tr, fresh)
	j.jmp(after)
	j.at(got)
	j.mov(tr, j.g(j.s.trail))
	j.at(after)

	depth := j.g(j.s.depth)
	j.br(">=", depth, j.num(int64(maxJSONDepth)), hit)
	i := j.t()
	j.mov(i, j.zero())
	j.at(top)
	j.br(">=", i, depth, yes)
	j.br("==", j.ld(j.idx(tr, i, 8), 0), ident, hit)
	j.addOne(i)
	j.jmp(top)

	j.at(yes)
	j.st(j.idx(tr, depth, 8), 0, ident)
	j.setG(j.s.depth, j.add(depth, j.num(1)))
	inner := body()
	j.setG(j.s.depth, j.minus(j.g(j.s.depth), j.num(1)))
	j.mov(out, inner)
	j.jmp(done)

	j.at(hit)
	j.mov(out, j.call(fnJSONNew, j.num(jsonNull)))
	j.at(done)
	return out
}

// shapeFail 是"这一棵 Json 不是那一种形状"那一步的收尾：叫运行期那一支抛 10114。
// 抛由运行期那一侧做，与借用表那两条同一档：发射出来的这几支不自己走帧清理。
//
// en: shapeFail ends the "this Json is not that shape" step by calling the runtime routine that
// throws 10114. The throw lives on the runtime side, like the two borrow-table codes: the
// emitted functions do not walk the frame cleanup themselves.
func shapeFail(j *jb) {
	j.void("@json_shape_fail")
}

// notKind 发一段"种类不是这一个就抛"。
//
// en: notKind emits "throw unless the kind is this one".
func (j *jb) notKind(node ir.Val, want int64) {
	okay := j.fresh()
	j.br("==", j.ld(node, jsonKind), j.num(want), okay)
	shapeFail(j)
	j.at(okay)
}

// nef.json.of_int(整数)：一个数节点，整数面与小数面各存一份。
//
// 小数面顺手存的原因是 `to_number`：两种数都读小数面那一个位置，先前只有解析那一路（`nef.json.number`）
// 会填它。程序里造出来的数节点要是只填整数面，`j.to_integer()` 答得对、`j.to_number()` 答 0。
//
// en: nef.json.of_int(int) makes one number node holding both faces. The float face is filled
// because to_number reads it: only the parse path filled it before, so a built node carrying the
// integer face alone would answer to_integer correctly and to_number with 0.
func jsonBuildOfInt(l *L) {
	j, ok := openJSON(l, fnJSONOfInt, 1)
	if !ok {
		return
	}
	v := param(0)
	node := j.call(fnJSONNew, j.num(jsonIntKind))
	j.st(node, jsonInt, v)
	dbl := j.t()
	j.emit(&ir.Cvt{D: dbl, S: v, To: ir.TFlt})
	j.st(node, jsonFloat, dbl)
	j.ret(node)
}

// nef.json.of_float(位型)：一个小数节点，槽里躺的就是那 64 位。
//
// en: nef.json.of_float(pattern) makes a float node; the slot already holds those 64 bits.
func jsonBuildOfFloat(l *L) {
	j, ok := openJSON(l, fnJSONOfFloat, 1)
	if !ok {
		return
	}
	node := j.call(fnJSONNew, j.num(jsonFloatKind))
	j.st(node, jsonFloat, param(0))
	j.ret(node)
}

// nef.json.of_bool(0／1)：true 与 false 两种节点之一。
//
// en: nef.json.of_bool(0/1) makes one of the two boolean nodes.
func jsonBuildOfBool(l *L) {
	j, ok := openJSON(l, fnJSONOfBool, 1)
	if !ok {
		return
	}
	isTrue := j.fresh()
	j.br("==", param(0), j.zero(), isTrue)
	j.ret(j.call(fnJSONNew, j.num(jsonTrue)))
	j.at(isTrue)
	j.ret(j.call(fnJSONNew, j.num(jsonFalse)))
}

// nef.json.of_text(文本)：文本节点，正文另拷一份。
//
// 必须拷：拆解一棵树时，那个位置上的文本由这一个节点归还（`nef.json.free`），
// 交进来的那一段文本的持有者还在调用方手里。
//
// en: nef.json.of_text(text) makes a text node whose body is a copy. The copy is required:
// dismantling a tree returns that position's text from this node (nef.json.free) while the
// incoming text's holder is still on the caller's side.
func jsonBuildOfText(l *L) {
	j, ok := openJSON(l, fnJSONOfText, 1)
	if !ok {
		return
	}
	src := param(0)
	node := j.call(fnJSONNew, j.num(jsonTextKind))
	cp := j.call("@str_sub", src, j.zero(), j.ld(src, runtime.StrBytes))
	j.st(node, jsonText, cp)
	j.ret(node)
}

// nef.json.key_text(键值, 形状描述)：对象那一把键的文本，返回的是新的一份。
//
// 文本键原样拷一份；别的种类先按打印机的写法变成给人看的那一串（`1`、`true`），
// 与 `d[1]`、`d[true]` 这类字典写进 JSON 时是同一条答复。
//
// en: nef.json.key_text(value, description) returns the object key for that value as a fresh
// text. A text key is copied as it is; anything else first goes through the printer (1, true),
// which is the same answer a `d[1]` or `d[true]` dict gives when it is written as JSON.
func jsonBuildKeyText(l *L) {
	j, ok := openJSON(l, fnJSONKeyText, 2)
	if !ok {
		return
	}
	v, desc := param(0), param(1)
	isText := j.fresh()
	j.br("==", j.ld(desc, pdKind), j.num(int64(ir.KindString)), isText)
	j.ret(j.call(printFn, v, desc))
	j.at(isText)
	j.ret(j.call("@str_sub", v, j.zero(), j.ld(v, runtime.StrBytes)))
}

// nef.json.new_object()：一个空对象（两张表都是空的）。
//
// en: nef.json.new_object() makes an empty object (both tables empty).
func jsonBuildNewObject(l *L) {
	j, ok := openJSON(l, fnJSONNewObject, 0)
	if !ok {
		return
	}
	node := j.call(fnJSONNew, j.num(jsonObjectKind))
	j.st(node, jsonKeys, j.call("@list_new", j.zero(), j.num(tagStr)))
	j.st(node, jsonVals, j.call("@list_new", j.zero(), j.zero()))
	j.ret(node)
}

// nef.json.new_array()：一个空数组。值表的摆法与解析那一路相同。
//
// en: nef.json.new_array() makes an empty array, the value table arranged as the parse path
// arranges it.
func jsonBuildNewArray(l *L) {
	j, ok := openJSON(l, fnJSONNewArray, 0)
	if !ok {
		return
	}
	node := j.call(fnJSONNew, j.num(jsonArrayKind))
	j.st(node, jsonVals, j.call("@list_new", j.zero(), j.num(tagList)))
	j.ret(node)
}

// nef.json.set(对象, 键, 节点)：收下这一对，键自己拷一份。
//
// 已有的键就覆盖那一个位置、把换下来的子树拆掉；没有就在两张表各添一个位置（位置不变那一条
// 与解析那一路同一条规则，都走 nef.json.pair）。
//
// en: nef.json.set(object, key, node) stores the pair, copying the key first. An existing key
// replaces that position and dismantles the subtree it swaps out; a new one appends to both
// tables, with the "position does not move" rule shared with the parse path through
// nef.json.pair.
func jsonBuildSet(l *L) {
	j, ok := openJSON(l, fnJSONSet, 3)
	if !ok {
		return
	}
	node, key, child := param(0), param(1), param(2)
	j.notKind(node, jsonObjectKind)
	cp := j.call("@str_sub", key, j.zero(), j.ld(key, runtime.StrBytes))
	j.void(fnJSONPair, node, cp, child)
	j.ret0()
}

// nef.json.push(数组, 节点)：往尾巴上放一个位置。
//
// en: nef.json.push(array, node) appends one position.
func jsonBuildPush(l *L) {
	j, ok := openJSON(l, fnJSONPush, 2)
	if !ok {
		return
	}
	node, child := param(0), param(1)
	j.notKind(node, jsonArrayKind)
	j.void("@list_add", j.ld(node, jsonVals), child)
	j.ret0()
}

// nef.json.insert_at(数组, 位置, 节点)：在那个位置前面放进去，越界当场报下标那一档。
//
// en: nef.json.insert_at(array, position, node) puts the node before that position; an index
// outside the range is the container's own code on the spot.
func jsonBuildInsertAt(l *L) {
	j, ok := openJSON(l, fnJSONInsertAt, 3)
	if !ok {
		return
	}
	node, at, child := param(0), param(1), param(2)
	j.notKind(node, jsonArrayKind)
	j.void("@list_insert", j.ld(node, jsonVals), at, child)
	j.ret0()
}

// nef.json.replace_at(数组, 位置, 节点)：盖掉那一个位置，换下来的那棵子树当场拆掉。
//
// 边界由 @list_at 报（与 `xs[i] = 值` 同一条）；那一个位置上的旧值由这一个节点欠的账收回，
// 与覆盖一个容器位置的旧值是同一种处理。
//
// en: nef.json.replace_at(array, position, node) overwrites that position and dismantles the
// subtree it swaps out. The boundary comes from @list_at (the same rule as xs[i] = value), and
// the old value is reclaimed here, like the old value of an overwritten container slot.
func jsonBuildReplaceAt(l *L) {
	j, ok := openJSON(l, fnJSONReplaceAt, 3)
	if !ok {
		return
	}
	node, at, child := param(0), param(1), param(2)
	j.notKind(node, jsonArrayKind)
	cell := j.call("@list_at", j.ld(node, jsonVals), at)
	old := j.ld(cell, 0)
	j.st(cell, 0, child)
	j.void(fnJSONFree, old)
	j.ret0()
}

// nef.json.remove_key(对象, 键)：那把键连同值一起拿走，键不在返回假。
//
// 两张表同一次前移，位置因此对齐；换下来的键文本归还、子树拆掉，与覆盖一个位置是同一本账。
//
// en: nef.json.remove_key(object, key) takes the key and its value off the tree, answering false
// when the key is not there. Both tables shift once so the positions stay lined up; the key text
// is returned and the subtree dismantled, the same ledger as overwriting a position.
func jsonBuildDelKey(l *L) {
	j, ok := openJSON(l, fnJSONDelKey, 2)
	if !ok {
		return
	}
	nope := j.fresh()
	node, key := param(0), param(1)
	j.notKind(node, jsonObjectKind)
	keys := j.ld(node, jsonKeys)
	vals := j.ld(node, jsonVals)
	i := j.call("@list_index_of", keys, key, j.num(1))
	j.br("<", i, j.zero(), nope)
	j.void("@free_guarded", j.ld(j.call("@list_at", keys, i), 0))
	j.void(fnJSONFree, j.ld(j.call("@list_at", vals, i), 0))
	j.void("@list_shift_out", keys, i)
	j.void("@list_shift_out", vals, i)
	j.ret(j.num(1))
	j.at(nope)
	j.ret(j.zero())
}

// nef.json.remove_at(数组, 位置)：那个位置的元素被摘下来交给调用方，后面的依次前移。
//
// 交出去的节点唯一的持有者就是接住它的那个名字（与 `take_at` 同一本账）；摘出来之后
// 树上那个位置不再欠任何东西。
//
// en: nef.json.remove_at(array, position) takes the element off and hands it to the caller while
// the rest shift forward. The handed-over node's only holder is the receiving name (the same
// ledger as take_at); the tree owes nothing for that position afterwards.
func jsonBuildDelAt(l *L) {
	j, ok := openJSON(l, fnJSONDelAt, 2)
	if !ok {
		return
	}
	node, at := param(0), param(1)
	j.notKind(node, jsonArrayKind)
	vals := j.ld(node, jsonVals)
	cell := j.call("@list_at", vals, at)
	one := j.ld(cell, 0)
	j.void("@list_shift_out", vals, at)
	j.ret(one)
}

// nef.json.clear_node(节点)：整棵树里的下属全拆干净，数组与对象自己留着。
//
// 从尾巴上一格一格取（@list_pop_last）：数目与次数由那一条改，这一支只管拆掉交出来的那一个节点。
// 对象的键文本也是这一棵树独占的，归还走带借用检查的那一条。
//
// en: nef.json.clear_node(node) dismantles everything below the node and keeps the array or
// object itself. Slots come off the tail one at a time (@list_pop_last), which maintains the
// count and the version; this function only dismantles what it takes out. The key texts belong
// to this tree too, so they go back through the borrow-checked path.
func jsonBuildClearNode(l *L) {
	j, ok := openJSON(l, fnJSONClearNode, 1)
	if !ok {
		return
	}
	node := param(0)
	isArr, isObj, done := j.fresh(), j.fresh(), j.fresh()
	kind := j.ld(node, jsonKind)
	j.br("==", kind, j.num(jsonArrayKind), isArr)
	j.br("==", kind, j.num(jsonObjectKind), isObj)
	shapeFail(j)
	j.at(isObj)
	j.dropAll(j.ld(node, jsonKeys), false)
	j.at(isArr)
	j.dropAll(j.ld(node, jsonVals), true)
	j.at(done)
	j.ret0()
}

// dropAll 把一张表从尾巴起逐个拿走：sub 为真时那一个位置是一棵子树（拆掉），
// 为假时是一串键文本（归还）。
//
// en: dropAll takes one table off from the tail: with sub true each slot is a subtree to
// dismantle, with sub false a key text to return.
func (j *jb) dropAll(list ir.Val, sub bool) {
	top, end := j.fresh(), j.fresh()
	j.at(top)
	j.br("==", j.ld(list, runtime.CellLen), j.zero(), end)
	one := j.call("@list_pop_last", list)
	if sub {
		j.void(fnJSONFree, one)
	} else {
		j.void("@free_guarded", one)
	}
	j.jmp(top)
	j.at(end)
}

// nef.json.of(值, 形状描述)：按形状描述把程序里的一个值变成一棵树。
//
// 分支表照着 nef.print 排：那一张表说的就是"这一块里装的是什么、下属在哪一格"。
// 三处和打印不一样：
//
//   - 交的是节点，不是文本；种类对不上的那一种（字节块、句柄、集合）在这一层交 null 节点，
//     检查阶段已经把那些类型拦下来了，走不到这里。
//   - 只读源，不动源的账：整趟走下来程序里那块内存该归谁还照旧，树上是一份新的。
//     这与"读一个位置不交出"同一条，也是 `json.from_value(表)` 之后那个名字还能用的理由。
//   - 环与层数按节点身份记（guardNode），撞回来给 null 节点。
//
// en: nef.json.of(value, description) turns one program value into a tree, walking by the shape
// description. The branch table follows nef.print, which describes exactly what a block holds
// and where its children are. Three things differ: what comes back is a node, not a text; a kind
// that does not fit (byte blocks, handles, hashsets) answers a null node, though the checking
// phase already refuses those types so nothing reaches here; the walk only reads the source and
// leaves its ownership alone, which is the same rule as reading a position and why the name
// still works after json.from_value; and cycles and depth are counted per block identity
// (guardNode), answering a null node.
func jsonBuildOf(l *L) {
	j, ok := openJSON(l, fnJSONOf, 2)
	if !ok {
		return
	}
	j.trail = l.json.trail
	j.depth = l.json.depth
	v, desc := param(0), param(1)
	child := func(off int) ir.Val { return j.bin("+", desc, j.ld(desc, off)) }

	cases := []struct {
		k  ir.DescKind
		fn func() ir.Val
	}{
		{ir.KindInt, func() ir.Val { return j.call(fnJSONOfInt, v) }},
		{ir.KindByte, func() ir.Val { return j.call(fnJSONOfInt, v) }},
		{ir.KindBool, func() ir.Val { return j.call(fnJSONOfBool, v) }},
		{ir.KindChar, func() ir.Val { return j.charNode(v) }},
		{ir.KindFloat, func() ir.Val { return j.call(fnJSONOfFloat, v) }},
		{ir.KindString, func() ir.Val { return j.notNull(v, func() ir.Val { return j.call(fnJSONOfText, v) }) }},
		{ir.KindList, func() ir.Val { return j.listNode(v, child(pdElem)) }},
		{ir.KindDict, func() ir.Val { return j.dictNode(v, child(pdKey), child(pdElem)) }},
		{ir.KindStruct, func() ir.Val { return j.shapeNode(v, desc) }},
		{ir.KindClass, func() ir.Val { return j.shapeNode(v, desc) }},
		{ir.KindBox, func() ir.Val { return j.boxNode(v, child(pdElem)) }},
		{ir.KindNull, func() ir.Val { return j.call(fnJSONNew, j.num(jsonNull)) }},
	}
	body := make([]ir.Label, len(cases))
	kind := j.ld(desc, pdKind)
	for i, c := range cases {
		body[i] = j.fresh()
		j.br("==", kind, j.num(int64(c.k)), body[i])
	}
	last := j.fresh()
	j.jmp(last)
	for i := range cases {
		j.at(body[i])
		j.ret(cases[i].fn())
	}
	j.at(last)
	j.ret(j.call(fnJSONNew, j.num(jsonNull)))
}

// notNull 与打印那一条同一步，只是"是空的"这一侧交的是 null 节点。
//
// en: notNull is the printer's step with a null node on the empty side.
func (j *jb) notNull(v ir.Val, body func() ir.Val) ir.Val {
	out := j.t()
	yes, done := j.fresh(), j.fresh()
	j.br("!=", v, j.zero(), yes)
	j.mov(out, j.call(fnJSONNew, j.num(jsonNull)))
	j.jmp(done)
	j.at(yes)
	j.mov(out, body())
	j.at(done)
	return out
}

// charNode 把一个码点变成文本节点：先按打印机那条编成 UTF-8，再把那一段拷进节点。
//
// en: charNode makes a code point a text node: encoded as UTF-8 the printer encodes it, then the
// body is copied into the node.
func (j *jb) charNode(v ir.Val) ir.Val {
	txt := j.call("@char_str", v)
	node := j.call(fnJSONOfText, txt)
	j.free(txt)
	return node
}

// listNode 走一遍 list 的位置，每个元素交一个节点放进新数组。
//
// en: listNode walks a list's positions, making one node per element in a new array.
func (j *jb) listNode(v, ed ir.Val) ir.Val {
	return j.notNull(v, func() ir.Val {
		return guardNode(j, v, func() ir.Val {
			out := j.call(fnJSONNewArray)
			n := j.ld(v, runtime.CellLen)
			data := j.ld(v, runtime.CellData)
			i := j.t()
			j.mov(i, j.zero())
			top, end := j.fresh(), j.fresh()
			j.at(top)
			j.br(">=", i, n, end)
			one := j.call(fnJSONOf, j.ld(j.idx(data, i, 8), 0), ed)
			j.void("@list_add", j.ld(out, jsonVals), one)
			j.addOne(i)
			j.jmp(top)
			j.at(end)
			return out
		})
	})
}

// dictNode 走一遍哈希表的桶：键变成那把键的文本，值变成节点，一对放进新对象。
//
// 桶的顺序不保证（与打印字典同一条），JSON 对象的成员因此按跑起来那一次的顺序写出。
//
// en: dictNode walks the buckets: each key becomes a key text and each value a node, and the
// pair goes into a new object. Bucket order is not guaranteed (as with printing a dict), so a
// JSON object's members come out in the order that run happened to use.
func (j *jb) dictNode(v, kd, vd ir.Val) ir.Val {
	return j.notNull(v, func() ir.Val {
		return guardNode(j, v, func() ir.Val {
			out := j.call(fnJSONNewObject)
			cur := j.ld(v, runtime.CellData)
			top, end := j.fresh(), j.fresh()
			j.at(top)
			bk := j.call("@map_next", v, cur)
			j.br("==", bk, j.zero(), end)
			j.mov(cur, j.add(bk, j.num(runtime.BucketPad)))
			key := j.call(fnJSONKeyText, j.ld(bk, runtime.BucketKey), kd)
			one := j.call(fnJSONOf, j.ld(bk, runtime.BucketVal), vd)
			j.void(fnJSONPair, out, key, one)
			j.jmp(top)
			j.at(end)
			return out
		})
	})
}

// shapeNode 走一遍 struct 或 class 的字段表：字段名是那把键，字段的值变成节点。
//
// class 与 struct 走同一张字段表、同一种读法（`fieldVal` 按字段的宽度取）。这一趟只读源，
// 所以"字段里那个可变对象"不会被摘走，程序里那一块该归谁还照旧。
//
// en: shapeNode walks a struct's or class's field table: the field name is the key and the
// field's value becomes a node. Both use one field table and one read (fieldVal takes a field at
// its width). The walk only reads, so a mutable object in a field is not taken away and that
// block's owner is unchanged.
func (j *jb) shapeNode(v, desc ir.Val) ir.Val {
	return j.notNull(v, func() ir.Val {
		return guardNode(j, v, func() ir.Val {
			out := j.call(fnJSONNewObject)
			n := j.ld(desc, pdNField)
			arr := j.bin("+", desc, j.ld(desc, pdFields))
			i := j.t()
			j.mov(i, j.zero())
			top, end := j.fresh(), j.fresh()
			j.at(top)
			j.br(">=", i, n, end)
			e := j.idx(arr, i, pFieldSz)
			fdesc := j.bin("+", desc, j.ld(e, pfType))
			one := j.call(fnJSONOf, j.fieldVal(v, j.ld(e, pfOff), fdesc), fdesc)
			// 字段名那一条来自文本池（形状表里存的就是它的地址），头上那格尺寸是 0，
			// 归还走到就返回——所以直接把它交进键表，两份树共用同一条也不会重复收回。
			name := j.bin("+", desc, j.ld(e, pfName))
			j.void(fnJSONPair, out, name, one)
			j.addOne(i)
			j.jmp(top)
			j.at(end)
			return out
		})
	})
}

// boxNode 走盒子里那一个值：先去"类型字→形状"那张表里找到里面的形状，再按它走。
//
// 表里没有那一个类型字时交 null 节点（只可能是内存被写坏了，与打印那一侧的省略号同一档）。
//
// en: boxNode walks the value inside a box: first find its shape in the "type word -> shape"
// table, then walk by it. A missing row answers a null node (only corrupted memory can do that,
// the same bracket as the printer's ellipsis).
func (j *jb) boxNode(v, tab ir.Val) ir.Val {
	return j.notNull(v, func() ir.Val {
		d := j.boxDesc(v, tab)
		return j.notNull(d, func() ir.Val {
			inner := j.t()
			direct, done := j.fresh(), j.fresh()
			k := j.ld(d, pdKind)
			j.br("==", k, j.num(int64(ir.KindStruct)), direct)
			j.br("==", k, j.num(int64(ir.KindClass)), direct)
			j.mov(inner, j.ld(v, 0))
			j.jmp(done)
			j.at(direct)
			j.mov(inner, v)
			j.at(done)
			return j.call(fnJSONOf, inner, d)
		})
	})
}

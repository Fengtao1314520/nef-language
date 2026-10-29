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

// jsonops.go 是 nef.json 函数组的其余部分：拆解一棵树、渲染一棵树，与十一条 json 取值成员。
// 工具箱在 json.go，递归下降在 jsonparse.go。
//
// en: jsonops.go is the rest of the nef.json function set: dismantling a tree, rendering a
// tree, and the eleven json read-outs. The toolbox is in json.go and the recursive descent in
// jsonparse.go.
package lower

import (
	"neflang.org/nf/internal/runtime"
)

// 三支只在本组内部使用的帮手，与那十二条成员分开登记（jsonFns 登记的是十二条成员，
// 这三条是内部构造：释放两张表、渲染数组或对象、把一个数字写成十六进制字符）。
//
// en: Three helpers only this group uses, listed apart from the twelve members (jsonFns
// registers the twelve entries; these three are internal construction: dismantling the two
// tables, printing an array or object, writing one digit as a hexadecimal literal).
const (
	fnJSONFreeKeys   = "nef.json.free_keys"   // (键表) → 逐格还文本，再还数组与容器
	fnJSONFreeVals   = "nef.json.free_vals"   // (值表) → 逐格往里拆，再还数组与容器
	fnJSONRenderList = "nef.json.render_list" // (节点, 0 数组／1 对象) → 那一段
	fnJSONHexChar    = "nef.json.hex_char"    // (0～15) → 对应的十六进制字符
)

// nef.json.free(节点)：从内到外拆解整棵树，不遗留任何一块。
//
// 与 nef.destroy 路径同一套护栏：进入一块之前先把其头部的类型字写成"正在被拆"，
// 看到该标记或"已在空闲链上"就不做处理（见 destroy.go）。两个名字持有同一棵树的写法
// 走不到这里——读走那个位置会把它抹成 0——保留这道护栏是为了在按保守方式处理归属的
// 场景下兜底。
//
// 文本节点里的文本、键表里的每条键都由这一个节点独占（由 @str_sub 当场创建），
// 所以直接 @free；这与"字段里的文本这一版不释放"那条规则不冲突：那里同一个串被两处
// 持有，这里没有第二个出处。
//
// en: nef.json.free(node): dismantle from the inside out, leaving no block behind. The same
// guards as the nef.destroy path: before entering a block its header type word is rewritten to
// "being dismantled", and seeing that mark or "already on the free list" skips processing (see
// destroy.go). The spelling where two names hold the same tree cannot reach here - reading out
// a position zeroes it - but the guard stays as a fallback for the conservative-ownership
// case. The text inside a text node and every key in the key table are owned solely by this
// node (created on the spot by @str_sub), so they are freed with @free directly; that does not
// conflict with the "text in a field is not returned" rule: there the same string is held in
// two places, here there is no second source.
func jsonBuildFree(l *L) {
	j, ok := openJSON(l, fnJSONFree, 1)
	if !ok {
		return
	}
	node := param(0)
	done := j.fresh()
	j.br("==", node, j.zero(), done)
	tag := j.ldSz(node, -8, 4)
	j.br("==", tag, j.num(runtime.FreeSentinel), done)
	j.br("==", tag, j.num(runtime.DestroyingTag), done)
	j.st32(node, -8, j.num(runtime.DestroyingTag))
	j.void("@free", j.ld(node, jsonText)) // 非文本节点该字段是 0，@free 立即返回
	j.void(fnJSONFreeKeys, j.ld(node, jsonKeys))
	j.void(fnJSONFreeVals, j.ld(node, jsonVals))
	j.void("@free", node)
	j.at(done)
	j.ret0()
}

// jsonBuildFreeKeys 释放一张键表：每条键是这个对象独占的文本，逐格释放，
// 再释放数组数据段与容器对象（与 nef.destroy 处理 list 的收尾步骤相同）。
//
// 空句柄不做处理：数组节点没有键表。
//
// en: jsonBuildFreeKeys dismantles a key table: every key is text owned by this object alone,
// released slot by slot, then the array run and the container object are released - the same
// tail as the list branch of nef.destroy. An empty handle does nothing: an array node has no
// key table.
func jsonBuildFreeKeys(l *L) {
	j, ok := openJSON(l, fnJSONFreeKeys, 1)
	if !ok {
		return
	}
	list := param(0)
	// 两个返回路径分开：空句柄那条要跳过"释放数组数据段与容器对象"，而不是落到这两步之前。
	done, gone := j.fresh(), j.fresh()
	j.br("==", list, j.zero(), gone)
	n := j.ld(list, runtime.CellLen)
	data := j.ld(list, runtime.CellData)
	i := j.t()
	j.mov(i, j.zero())
	top := j.fresh()
	j.at(top)
	j.br(">=", i, n, done)
	j.void("@free", j.ld(j.idx(data, i, 8), 0))
	j.addOne(i)
	j.jmp(top)
	j.at(done)
	j.void("@free", data)
	j.void("@free", list)
	j.at(gone)
	j.ret0()
}

// jsonBuildFreeVals 释放一张值表：每个位置是一棵子树，逐格向内拆解，再释放数组数据段与容器对象。
//
// en: jsonBuildFreeVals dismantles a value table: every position is a subtree, dismantled
// inward slot by slot, then the array run and the container object are released.
func jsonBuildFreeVals(l *L) {
	j, ok := openJSON(l, fnJSONFreeVals, 1)
	if !ok {
		return
	}
	list := param(0)
	done, gone, top, empty := j.fresh(), j.fresh(), j.fresh(), j.fresh()
	j.br("==", list, j.zero(), gone)
	n := j.ld(list, runtime.CellLen)
	data := j.ld(list, runtime.CellData)
	i := j.t()
	j.mov(i, j.zero())
	j.at(top)
	j.br(">=", i, n, done)
	v := j.ld(j.idx(data, i, 8), 0)
	j.br("==", v, j.zero(), empty) // 已被读走的槽位：那里没有需要释放的对象
	j.void(fnJSONFree, v)
	j.at(empty)
	j.addOne(i)
	j.jmp(top)
	j.at(done)
	j.void("@free", data)
	j.void("@free", list)
	j.at(gone)
	j.ret0()
}

// nef.json.hex_char(0～15)：把该数字写成十六进制字符，小写。
//
// en: nef.json.hex_char(0-15): writes that digit as a lowercase hexadecimal character.
func jsonBuildHexChar(l *L) {
	j, ok := openJSON(l, fnJSONHexChar, 1)
	if !ok {
		return
	}
	v := param(0)
	letter := j.fresh()
	j.br("<", v, j.num(10), letter)
	j.ret(j.add(v, j.num('a'-10)))
	j.at(letter)
	j.ret(j.add(v, j.num('0')))
}

// nef.json.quote(文本)：返回带引号、该转义字符都已转义的那段文本（新分配的一块）。
//
// 只转必须转的：引号与反斜杠各占两个字节，0x00～0x1F 这三十一个码点写成 `\u00XX`。
// 非 ASCII 字节原样复制——它们本来就是合法的 UTF-8（文件读取路径已校验过），
// 而 JSON 允许字符串里直接放任何码点。
//
// 同样是两趟：第一趟统计要写出的字节数，第二趟填充。空句柄走不到这里
// （对空句柄取成员在使用前就会报 10092 号）。
//
// en: nef.json.quote(text): returns the quoted run with everything that must be escaped escaped
// (a freshly allocated block). Only what has to be escaped is: the quote and the backslash
// become two bytes each, and the 31 code points 0x00-0x1F are written as `\u00XX`. Non-ASCII
// bytes are copied as they are - they are already well-formed UTF-8 (validated by the
// file-reading path) and JSON allows any code point inside a string. Also two passes: the first
// counts how many bytes will be written, the second fills them. An empty handle never reaches
// here (dereferencing an empty handle is reported with code 10092 before use).
func jsonBuildQuote(l *L) {
	j, ok := openJSON(l, fnJSONQuote, 1)
	if !ok {
		return
	}
	src := param(0)
	n := j.ld(src, runtime.StrBytes)
	body := j.add(src, j.num(jsonTextBody))
	// 第一趟：统计要写出多少个字节
	out := j.t()
	j.mov(out, j.num(2)) // 两端的一对引号各占一字节
	i := j.t()
	j.mov(i, j.zero())
	top, end, two, six, cont := j.fresh(), j.fresh(), j.fresh(), j.fresh(), j.fresh()
	j.at(top)
	j.br(">=", i, n, end)
	c := j.byteAt(j.add(body, i))
	j.br("==", c, j.num('"'), two)
	j.br("==", c, j.num('\\'), two)
	j.br("<", c, j.num(0x20), six)
	j.mov(out, j.add(out, j.num(1)))
	j.jmp(cont)
	j.at(two)
	j.mov(out, j.add(out, j.num(2)))
	j.jmp(cont)
	j.at(six)
	j.mov(out, j.add(out, j.num(6)))
	j.at(cont)
	j.addOne(i)
	j.jmp(top)
	j.at(end)
	// 第二趟：写进新分配的对象。块布局为「字节数 8 ＋ 码点数 8 ＋ 正文」
	blk := j.call("@alloc", j.add(out, j.num(jsonTextBody)), j.num(tagStr))
	j.st(blk, runtime.StrBytes, out)
	w := j.add(blk, j.num(jsonTextBody))
	j.stByte(w, j.num('"'))
	j.mov(w, j.add(w, j.num(1)))
	j.mov(i, j.zero())
	fill, filled, escTwo, escSix, step := j.fresh(), j.fresh(), j.fresh(), j.fresh(), j.fresh()
	j.at(fill)
	j.br(">=", i, n, filled)
	c = j.byteAt(j.add(body, i))
	j.br("==", c, j.num('"'), escTwo)
	j.br("==", c, j.num('\\'), escTwo)
	j.br("<", c, j.num(0x20), escSix)
	j.stByte(w, c)
	j.mov(w, j.add(w, j.num(1)))
	j.jmp(step)
	j.at(escTwo)
	j.stByte(w, j.num('\\'))
	j.stByte(j.add(w, j.num(1)), c)
	j.mov(w, j.add(w, j.num(2)))
	j.jmp(step)
	j.at(escSix)
	// 那六个字节：`\u00` 加上高四位与低四位对应的两个十六进制字符
	j.stByte(w, j.num('\\'))
	j.stByte(j.add(w, j.num(1)), j.num('u'))
	j.stByte(j.add(w, j.num(2)), j.num('0'))
	j.stByte(j.add(w, j.num(3)), j.num('0'))
	hi := j.bin(">>", c, j.num(4))
	j.stByte(j.add(w, j.num(4)), j.call(fnJSONHexChar, hi))
	lo := j.bin("&", c, j.num(0xF))
	j.stByte(j.add(w, j.num(5)), j.call(fnJSONHexChar, lo))
	j.mov(w, j.add(w, j.num(6)))
	j.at(step)
	j.addOne(i)
	j.jmp(fill)
	j.at(filled)
	j.stByte(w, j.num('"'))
	runes := j.call("@count_runes", j.add(blk, j.num(jsonTextBody)), out)
	j.st(blk, runtime.StrCount, runes)
	j.ret(blk)
}

// nef.json.render(节点)：把一棵树渲染成一段 JSON 文本（新分配的一块）。
//
// `console.print` 打印一个 Json 走的就是这一支（"形状要到运行期才能确定"的那一族类型）。
// 已被读走的位置是空句柄，渲染时输出 null——与容器一侧同一套规则。
// 拼接方式与打印路径一致：逐段 @str_cat，旧的那一段用完即释放，所以这一支返回的是新的一块。
//
// en: nef.json.render(node): writes a whole tree as one JSON text (a freshly allocated block).
// This is the function `console.print` uses to print a Json (the family of "shapes only
// knowable at runtime"). A position that was read out is an empty handle and prints null - the
// same rule as the container side. Assembly follows the printing path:
// @str_cat piece by piece, each older piece released as soon as it is used up, so what this
// function returns is a new block.
func jsonBuildRender(l *L) {
	j, ok := openJSON(l, fnJSONRender, 1)
	if !ok {
		return
	}
	var (
		null    = j.fresh()
		isInt   = j.fresh()
		isFloat = j.fresh()
		isText  = j.fresh()
		isArr   = j.fresh()
		isObj   = j.fresh()
		theEnd  = j.fresh()
	)
	node := param(0)
	j.br("==", node, j.zero(), null)
	kind := j.ld(node, jsonKind)
	for _, one := range []struct {
		k    int64
		word string
	}{
		{jsonNull, "null"}, {jsonTrue, "true"}, {jsonFalse, "false"},
	} {
		no := j.fresh()
		j.br("!=", kind, j.num(one.k), no)
		j.ret(j.freshText(j.str(one.word)))
		j.at(no)
	}
	j.br("==", kind, j.num(jsonIntKind), isInt)
	j.br("==", kind, j.num(jsonFloatKind), isFloat)
	j.br("==", kind, j.num(jsonTextKind), isText)
	j.br("==", kind, j.num(jsonArrayKind), isArr)
	j.br("==", kind, j.num(jsonObjectKind), isObj)
	j.jmp(theEnd) // 无法识别的种类：形状表由编译器生成，走不到这里
	j.at(isArr)
	j.ret(j.call(fnJSONRenderList, node, j.zero()))
	j.at(isObj)
	j.ret(j.call(fnJSONRenderList, node, j.num(1)))
	j.at(isInt)
	j.ret(j.call("@int_str", j.ld(node, jsonInt)))
	j.at(isFloat)
	j.ret(j.call("@float_str", j.ld(node, jsonFloat)))
	j.at(isText)
	j.ret(j.call(fnJSONQuote, j.ld(node, jsonText)))
	j.at(null)
	j.ret(j.freshText(j.str("null"))) // 空句柄渲染为 null
	j.at(theEnd)
	j.ret(j.freshText(j.str("null")))
}

// nef.json.render_list(节点, 0 数组／1 对象)：印外面那一层。
//
// 对象那一侧才去读键表：数组节点的键表那个位置是空句柄，一进门就读它等于拿 0 加偏移。
//
// en: nef.json.render_list(node, 0 array / 1 object): prints the outer layer. Only the object
// side reads the key table: on an array node that position is an empty handle, and reading it
// on entry would mean adding an offset to 0.
func jsonBuildRenderList(l *L) {
	j, ok := openJSON(l, fnJSONRenderList, 2)
	if !ok {
		return
	}
	var (
		top      = j.fresh()
		end      = j.fresh()
		notFirst = j.fresh()
		noKey    = j.fresh()
		isArr    = j.fresh()
		tailArr  = j.fresh()
	)
	node, wantObj := param(0), param(1)
	vals := j.ld(node, jsonVals)
	n := j.ld(vals, runtime.CellLen)
	data := j.ld(vals, runtime.CellData)
	head := j.t()
	j.mov(head, j.freshText(j.str("[")))
	j.br("==", wantObj, j.zero(), isArr)
	j.mov(head, j.freshText(j.str("{")))
	j.at(isArr)
	acc := j.t()
	j.mov(acc, head)
	i := j.t()
	j.mov(i, j.zero())
	j.at(top)
	j.br(">=", i, n, end)
	j.br("==", i, j.zero(), notFirst)
	j.mov(acc, j.catFree(acc, j.str(","))) // 从第二个成员起先拼接一个逗号
	j.at(notFirst)
	j.br("==", wantObj, j.zero(), noKey)
	// 对象：把 `"键":` 拼接在该位置的值前面
	keys := j.ld(node, jsonKeys)
	keyTxt := j.ld(j.idx(j.ld(keys, runtime.CellData), i, 8), 0)
	piece := j.call(fnJSONQuote, keyTxt)
	j.mov(acc, j.catFree(acc, piece))
	j.free(piece)
	j.mov(acc, j.catFree(acc, j.str(":")))
	j.at(noKey)
	child := j.ld(j.idx(data, i, 8), 0)
	piece = j.call(fnJSONRender, child)
	j.mov(acc, j.catFree(acc, piece))
	j.free(piece)
	j.addOne(i)
	j.jmp(top)
	j.at(end)
	tail := j.t()
	j.mov(tail, j.freshText(j.str("]")))
	j.br("==", wantObj, j.zero(), tailArr)
	j.mov(tail, j.freshText(j.str("}")))
	j.at(tailArr)
	j.mov(acc, j.catFree(acc, tail))
	j.free(tail)
	j.ret(acc)
}

// ---- 十二条成员里的十一条取值 ----

// nef.json.kind(节点)：返回六个词之一（新分配的一块，接收它的名字登记成持有者）。
//
// en: nef.json.kind(node): returns one of the six words (a freshly allocated block, registered
// against the name that receives it).
func jsonBuildKind(l *L) {
	j, ok := openJSON(l, fnJSONKind, 1)
	if !ok {
		return
	}
	kind := j.ld(param(0), jsonKind)
	for k, word := range jsonKinds {
		no := j.fresh()
		j.br("!=", kind, j.num(int64(k)), no)
		j.ret(j.freshText(j.str(word)))
		j.at(no)
	}
	j.ret(j.freshText(j.str("null"))) // 种类编号由编译器生成，不会走到这条分支
}

// nef.json.count(节点)：数组的元素个数、对象的成员个数；其他种类返回 0（那里没有可数的内容）。
//
// en: nef.json.count(node): how many items an array has or how many members an object has; any
// other kind returns 0 (there is nothing there to count).
func jsonBuildCount(l *L) {
	j, ok := openJSON(l, fnJSONCount, 1)
	if !ok {
		return
	}
	yes, nope := j.fresh(), j.fresh()
	node := param(0)
	kind := j.ld(node, jsonKind)
	j.br("==", kind, j.num(jsonArrayKind), yes)
	j.br("!=", kind, j.num(jsonObjectKind), nope)
	j.at(yes)
	j.ret(j.ld(j.ld(node, jsonVals), runtime.CellLen))
	j.at(nope)
	j.ret(j.zero())
}

// nef.json.keys(节点)：对象的全部键，返回一份新的 list<string>，顺序与原文一致。
//
// 交出的是**拷贝**：树里的键仍在原位置，持有者还是那个位置（与 @dict_entries 拷贝文本键
// 是同一条做法）。不是对象就返回空表。
//
// en: nef.json.keys(node): an object's keys as a new list<string>, in the order written. What
// is handed over is a COPY: the key inside the tree stays where it is and that position remains
// its holder (the same approach as @dict_entries copying text keys). A non-object returns an
// empty table.
func jsonBuildKeys(l *L) {
	j, ok := openJSON(l, fnJSONKeys, 1)
	if !ok {
		return
	}
	done, top := j.fresh(), j.fresh()
	node := param(0)
	out := j.call("@list_new", j.zero(), j.num(tagStr))
	j.br("!=", j.ld(node, jsonKind), j.num(jsonObjectKind), done)
	keys := j.ld(node, jsonKeys)
	n := j.ld(keys, runtime.CellLen)
	data := j.ld(keys, runtime.CellData)
	i := j.t()
	j.mov(i, j.zero())
	j.at(top)
	j.br(">=", i, n, done)
	one := j.ld(j.idx(data, i, 8), 0)
	fresh := j.call("@str_sub", one, j.zero(), j.ld(one, runtime.StrBytes))
	j.void("@list_add", out, fresh)
	j.addOne(i)
	j.jmp(top)
	j.at(done)
	j.ret(out)
}

// nef.json.has(节点, 键)：对象里是否存在这把键。其他种类返回假。只读，不改动那个位置。
//
// en: nef.json.has(node, key): whether an object has this key; any other kind is false.
// Read-only, the position is untouched.
func jsonBuildHas(l *L) {
	j, ok := openJSON(l, fnJSONHas, 2)
	if !ok {
		return
	}
	nope := j.fresh()
	node, key := param(0), param(1)
	j.br("!=", j.ld(node, jsonKind), j.num(jsonObjectKind), nope)
	i := j.call("@list_index_of", j.ld(node, jsonKeys), key, j.num(1))
	j.br("<", i, j.num(0), nope)
	j.ret(j.num(1))
	j.at(nope)
	j.ret(j.zero())
}

// nef.json.at(节点, 键)：取对象里那把键对应的值——**读走**那个位置。
//
// 不是对象、或者没有这把键，返回空句柄；取过一次之后那个位置就是 null，
// 与 `xs[0]`、`o.f` 是同一套读走规则。
//
// en: nef.json.at(node, key): take the value for a key in an object - a READ-OUT of that
// position. Not an object, or no such key, returns an empty handle; after one read
// that position is null, following the same read-out rule as `xs[0]` and `o.f`.
func jsonBuildAt(l *L) {
	j, ok := openJSON(l, fnJSONAt, 2)
	if !ok {
		return
	}
	nope := j.fresh()
	node, key := param(0), param(1)
	j.br("!=", j.ld(node, jsonKind), j.num(jsonObjectKind), nope)
	i := j.call("@list_index_of", j.ld(node, jsonKeys), key, j.num(1))
	j.br("<", i, j.num(0), nope)
	cell := j.idx(j.ld(j.ld(node, jsonVals), runtime.CellData), i, 8)
	v := j.ld(cell, 0)
	j.st(cell, 0, j.zero())
	j.ret(v)
	j.at(nope)
	j.ret(j.zero())
}

// nef.json.item(节点, 位置)：取数组里那个位置的值——同样是读走。
//
// 位置为负、或者不小于长度，返回空句柄：这一组取不到一律返回 null，不新增错误码。
//
// en: nef.json.item(node, index): take the value at an array index - also a read-out. A
// negative index, or one not less than the length, returns an empty handle: this group answers
// every failure with null and adds no error code.
func jsonBuildItem(l *L) {
	j, ok := openJSON(l, fnJSONItem, 2)
	if !ok {
		return
	}
	nope := j.fresh()
	node, at := param(0), param(1)
	j.br("!=", j.ld(node, jsonKind), j.num(jsonArrayKind), nope)
	vals := j.ld(node, jsonVals)
	j.br("<", at, j.zero(), nope)
	j.br(">=", at, j.ld(vals, runtime.CellLen), nope)
	cell := j.idx(j.ld(vals, runtime.CellData), at, 8)
	v := j.ld(cell, 0)
	j.st(cell, 0, j.zero())
	j.ret(v)
	j.at(nope)
	j.ret(j.zero())
}

// nef.json.to_text(节点)：文本节点那一段值的一份拷贝；别的种类返回空句柄。
//
// en: nef.json.to_text(node): a copy of the text held by a text node; any other kind returns an
// empty handle.
func jsonBuildToText(l *L) {
	j, ok := openJSON(l, fnJSONToText, 1)
	if !ok {
		return
	}
	nope := j.fresh()
	node := param(0)
	j.br("!=", j.ld(node, jsonKind), j.num(jsonTextKind), nope)
	one := j.ld(node, jsonText)
	j.ret(j.call("@str_sub", one, j.zero(), j.ld(one, runtime.StrBytes)))
	j.at(nope)
	j.ret(j.zero())
}

// nef.json.to_int(节点, 默认)：只有整数面的数返回真值，其余返回默认值。
//
// 值类型不能是 null，所以 to_integer／to_number／to_bool 这三条按
// `dict.get(键, 默认)` 的形状多带一个默认值；to_text 返回的是 string，本身可为 null。
//
// en: nef.json.to_int(node, default): only the integer face of a number returns its value;
// everything else takes the default. A value type cannot be null, which is why
// to_integer/to_number/to_bool carry a default like `dict.get(key, default)`; to_text returns a
// string, which may already be null.
func jsonBuildToInt(l *L) {
	j, ok := openJSON(l, fnJSONToInt, 2)
	if !ok {
		return
	}
	nope := j.fresh()
	node := param(0)
	j.br("!=", j.ld(node, jsonKind), j.num(jsonIntKind), nope)
	j.ret(j.ld(node, jsonInt))
	j.at(nope)
	j.ret(param(1))
}

// nef.json.to_float(节点, 默认位型)：两种数都返回真值（整数面也存了对应的位型），其余返回默认值。
//
// en: nef.json.to_float(node, default pattern): both number faces return their value (the
// integer face also stores its double pattern); everything else takes the default.
func jsonBuildToFloat(l *L) {
	j, ok := openJSON(l, fnJSONToFloat, 2)
	if !ok {
		return
	}
	yes, nope := j.fresh(), j.fresh()
	node := param(0)
	kind := j.ld(node, jsonKind)
	j.br("==", kind, j.num(jsonIntKind), yes)
	j.br("!=", kind, j.num(jsonFloatKind), nope)
	j.at(yes)
	j.ret(j.ld(node, jsonFloat))
	j.at(nope)
	j.ret(param(1))
}

// nef.json.to_bool(节点, 默认)：true 与 false 两种节点返回真值，其余返回默认值。
//
// en: nef.json.to_bool(node, default): the true and false nodes return their value; everything
// else takes the default.
func jsonBuildToBool(l *L) {
	j, ok := openJSON(l, fnJSONToBool, 2)
	if !ok {
		return
	}
	yes, nope := j.fresh(), j.fresh()
	kind := j.ld(param(0), jsonKind)
	j.br("==", kind, j.num(jsonTrue), yes)
	j.br("!=", kind, j.num(jsonFalse), nope)
	j.ret(j.zero()) // false 种类的节点
	j.at(yes)
	j.ret(j.num(1))
	j.at(nope)
	j.ret(param(1))
}

// nef.json.explain(文本)：返回一句面向用户的说明，例如"第 3 行第 12 列：这里该是一个值"。没有错误时返回空文本。
//
// 做法是重新解析一遍：解析成功就拆掉那棵树、返回空文本；解析失败就按 jErr／jErrPos 生成说明。
// 解析两遍的开销可以接受——这一条是给用户定位错误用的，不在热路径上；因此它**不需要记住上一次解析**，
// 也就没有"必须先 parse 再 explain"这种用法上的顺序要求。
//
// en: nef.json.explain(text): the plain-language sentence - "line 3, column 12: expected a
// value here". Clean text returns empty. It works by parsing again: if it parses, the tree is
// dismantled and empty is returned; if not, jErr/jErrPos are read out and reported. Parsing
// twice is an acceptable cost - this member exists to show the user where the error is and is
// not on a hot path. The upside is that it never has to remember a previous parse, so there is
// no usage rule like "parse before explain".
func jsonBuildExplain(l *L) {
	j, ok := openJSON(l, fnJSONExplain, 1)
	if !ok {
		return
	}
	var (
		isBad = j.fresh()
		top   = j.fresh()
		end   = j.fresh()
		isNL  = j.fresh()
		isCP  = j.fresh()
		cont  = j.fresh()
		done  = j.fresh()
	)
	root := j.call(fnJSONParse, param(0))
	j.br("==", root, j.zero(), isBad)
	j.void(fnJSONFree, root)
	j.ret(j.freshText(j.str(""))) // 无错误：返回空文本
	j.at(isBad)
	// 行与列都从 1 开始计数；列按码点计——UTF-8 续字节落在 0x80～0xBF，跳过它们即按码点计数
	buf := j.g(j.s.buf)
	at := j.g(j.s.errAt)
	line := j.t()
	col := j.t()
	i := j.t()
	j.mov(line, j.num(1))
	j.mov(col, j.num(1))
	j.mov(i, j.zero())
	j.at(top)
	j.br(">=", i, at, end)
	c := j.byteAt(j.add(buf, i))
	j.br("==", c, j.num('\n'), isNL)
	// 列计数的是码点：单字节（<0x80）与多字节序列的首字节（>=0xC0）各算一个码点，
	// 落在 0x80～0xBF 的是续字节，不单独计数。
	j.br("<", c, j.num(0x80), isCP)
	j.br(">=", c, j.num(0xC0), isCP)
	j.jmp(cont)
	j.at(isCP)
	j.addOne(col)
	j.jmp(cont)
	j.at(isNL)
	j.addOne(line)
	j.mov(col, j.num(1))
	j.at(cont)
	j.addOne(i)
	j.jmp(top)
	j.at(end)
	// 拼那一句：第 <行> 行第 <列> 列：<那一句正文>
	//
	// acc 固定使用同一个位置：每一段都要在同一链上继续拼接，所以每拼一次就把结果搬回这个
	// 位置。在 Go 侧换用另一个临时寄存器记录是错的——拼接链会在中途断开，
	// 后续指令读到的是从未写入的槽位。
	acc := j.t()
	j.mov(acc, j.str(""))
	one := j.t()
	j.mov(acc, j.catFree(acc, j.str("第 ")))
	j.mov(one, j.call("@int_str", line))
	j.mov(acc, j.catFree(acc, one))
	j.free(one)
	j.mov(acc, j.catFree(acc, j.str(" 行第 ")))
	j.mov(one, j.call("@int_str", col))
	j.mov(acc, j.catFree(acc, one))
	j.free(one)
	j.mov(acc, j.catFree(acc, j.str(" 列：")))
	code := j.g(j.s.err)
	for k := 1; k < len(jsonReasons); k++ {
		no := j.fresh()
		j.br("!=", code, j.num(int64(k)), no)
		piece := j.freshText(j.str(jsonReasons[k]))
		j.mov(acc, j.catFree(acc, piece))
		j.free(piece)
		j.jmp(done)
		j.at(no)
	}
	j.at(done)
	j.ret(acc)
}

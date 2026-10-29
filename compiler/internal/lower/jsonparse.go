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

// jsonparse.go 是 nef.json 的递归下降部分：文本、数、对象、数组与总入口。
// 工具箱、静态槽位与三支内部例程在 json.go。
//
// en: jsonparse.go holds the recursive descent of nef.json: text, number, object, array and
// the entry point. The toolbox, the static slots and the three internal helpers are in json.go.
package lower

import (
	"neflang.org/nf/internal/ir"
	"neflang.org/nf/internal/runtime"
)

// nef.json.textof()：读取游标位置那段带引号的文本，返回解码后的新文本；读取失败返回空句柄。
//
// 两趟。第一趟只做校验：找到收尾引号，记录是否出现过转义；第二趟只在有转义的路径上走，
// 把直写片段与逐个解码出的字符拼接起来。全程没有转义时一次 @str_sub 调用即完成——
// 绝大多数真实数据走这一条路径。键与值都用这一步：对象一侧需要的正是解码后的文本。
//
// en: nef.json.textof(): reads the quoted text under the cursor and returns a newly decoded
// string, or an empty handle on failure. Two passes: the first only validates, finds the
// closing quote and records whether an escape appeared; the second runs only on the escaped
// path, assembling the literal runs and each decoded character. Without an escape, one
// @str_sub call is enough - most real data takes that path. Keys and values both use this
// step: the object side wants exactly the decoded text.
func jsonBuildText(l *L) {
	j, ok := openJSON(l, fnJSONText, 0)
	if !ok {
		return
	}
	j.wsIn()
	quote := j.fresh()
	j.br("==", j.char(0), j.num('"'), quote)
	j.errIn(jsonQuote)
	j.ret(j.zero())
	j.at(quote)
	j.adv(1)
	start := j.t()
	j.mov(start, j.pos())
	esc := j.t()
	j.mov(esc, j.zero())

	// ---- 第一趟：校验合法性 ----
	top, cut, ctrl, plain, escBad, done, second := j.fresh(), j.fresh(), j.fresh(),
		j.fresh(), j.fresh(), j.fresh(), j.fresh()
	j.at(top)
	c := j.char(0)
	j.br("==", c, j.num(-1), cut)
	j.br("==", c, j.num('"'), done)
	j.br("<", c, j.num(0x20), ctrl)
	j.br("!=", c, j.num('\\'), plain)
	cp := j.call(fnJSONUnesc)
	j.br("==", cp, j.num(-1), escBad) // 读取失败：错误已由 nef.json.unesc 记录
	j.mov(esc, j.num(1))
	j.jmp(top)
	j.at(escBad)
	j.ret(j.zero())
	j.at(cut)
	j.errIn(jsonQuote) // 文本读完仍未遇到收尾引号：报错为"缺少收尾引号"，而不是"被截断"
	j.ret(j.zero())
	j.at(ctrl)
	j.errIn(jsonControl)
	j.ret(j.zero())
	j.at(plain)
	j.adv(1)
	j.jmp(top)

	// ---- 收尾：无转义，直接截出结果 ----
	j.at(done)
	q := j.t()
	j.mov(q, j.pos())
	j.br("!=", esc, j.zero(), second)
	j.adv(1) // 移过收尾引号，与第二趟路径在同一位置返回
	j.ret(j.call("@str_sub", j.g(j.s.src), start, j.minus(q, start)))

	// ---- 第二趟：有转义，逐段拼接 ----
	j.at(second)
	acc := j.t()
	j.mov(acc, j.str("")) // 以空文本起始；每拼接一次，释放此前那一段
	run := j.t()
	j.mov(run, start)
	j.setPos(start)
	t2, e2, step2, notBack, gap := j.fresh(), j.fresh(), j.fresh(), j.fresh(), j.fresh()
	j.at(t2)
	c2 := j.char(0)
	j.br("==", c2, j.num('"'), e2)
	j.br("!=", c2, j.num('\\'), notBack)
	// 反斜杠起始：先把 run 到当前位置的直写片段截出来
	j.br("<=", j.pos(), run, gap)
	piece := j.call("@str_sub", j.g(j.s.src), run, j.minus(j.pos(), run))
	j.mov(acc, j.catFree(acc, piece))
	j.free(piece)
	j.at(gap)
	// 第一趟已校验过整个转义序列，这一趟不会失败
	cp2 := j.call(fnJSONUnesc)
	piece2 := j.call("@char_str", cp2)
	j.mov(acc, j.catFree(acc, piece2))
	j.free(piece2)
	j.mov(run, j.pos())
	j.jmp(t2)
	j.at(notBack)
	j.adv(1)
	j.jmp(t2)
	j.at(e2)
	j.br("<=", j.pos(), run, step2)
	piece3 := j.call("@str_sub", j.g(j.s.src), run, j.minus(j.pos(), run))
	j.mov(acc, j.catFree(acc, piece3))
	j.free(piece3)
	j.at(step2)
	j.adv(1) // 挪过收尾那个引号
	j.ret(acc)
}

// nef.json.string()：解析一个文本节点（种类 jsonTextKind，解码后的值存进节点的文本字段）。
//
// en: nef.json.string(): parses a text node (kind jsonTextKind, the decoded value stored in the
// node's position).
func jsonBuildString(l *L) {
	j, ok := openJSON(l, fnJSONString, 0)
	if !ok {
		return
	}
	txt := j.call(fnJSONText)
	failed := j.fresh()
	j.br("==", txt, j.zero(), failed)
	n := j.call(fnJSONNew, j.num(jsonTextKind))
	j.st(n, jsonText, txt)
	j.ret(n)
	j.at(failed)
	j.ret(j.zero())
}

// nef.json.number()：解析一个数，返回节点或空句柄。
//
// 分两段，各自只有一种循环——可读性优先于少写几行：
//
//   - 第一段只校验写法：可选负号、整数部分（以 0 开头只能是一个 0）、可选小数部分、
//     可选指数部分。任一部分缺少数字即"这个数的写法不合法"，文本读完即"被截断"。
//     这一段同时记录"是否出现小数部分或指数"。
//   - 第二段只在整数面路径上走：把开头几位数字累加成数值，累出 int64 范围就记录"装不下"。
//     上界比较与 @str_to_int 同一套：先比上界的十分之一，恰好等于该值时最后一位按符号判定
//     （正号限到 7，负号限到 8，因此最小的负数合法）。这一段不再校验数字：第一段
//     已验证过，起点到游标之间全部是数字。
//
// 收尾两条路径：整数面且未越界是 jsonIntKind，整数字段存该数值，浮点字段同时存它的
// double 位型（json.to_number 因此两条路径都能应答）；出现小数部分或指数、或整数面装不进
// int64 的，截出原文交给 @str_to_float，种类 jsonFloatKind——超出 double 范围返回 ±无穷，
// 这本来就是 @str_to_float 的既定行为。临时截出的文本用完即释放。
//
// en: nef.json.number(): parses a number and returns a node or an empty handle. It is split in
// two stages, each with only one kind of loop, because readability matters more than saving
// a few lines. The first stage only validates the syntax: an optional minus, the integer part
// (a leading 0 may be only a single 0), an optional fraction, an optional exponent; a stage
// missing digits means "this number is spelled wrong" and running out of text means
// "truncated"; this stage also records whether a fraction or exponent appeared. The second
// stage runs only on the integer-face path: it accumulates the leading digits into a value and
// records "does not fit" if it overflows int64. The bound test follows @str_to_int's rule:
// compare against one tenth of the bound first, and when sitting exactly on that line check the
// last digit against the sign (up to 7 positive, up to 8 negative, so the most negative int64
// is legal). This stage never re-tests digits: the first stage validated them, and walking from
// the start to the cursor is all digits. Two endings: integer face and in range is
// jsonIntKind, storing the value in the integer field and its double bit pattern in the float
// field (so json.to_number answers on both paths); a fraction or exponent, or an integer that
// does not fit int64, slices the original text out and hands it to @str_to_float with kind
// jsonFloatKind - larger than a double returns ±infinity, which is already @str_to_float's
// defined behavior. The temporarily sliced text is freed as soon as it is used.
func jsonBuildNumber(l *L) {
	j, ok := openJSON(l, fnJSONNumber, 0)
	if !ok {
		return
	}
	const intLim = 922337203685477580 // 上界的十分之一，与 @str_to_int 用的是同一个数
	var (
		numFirst   = j.fresh()
		numNotZero = j.fresh()
		numLoop    = j.fresh()
		numFrac    = j.fresh()
		numFracDot = j.fresh()
		numFracEnd = j.fresh()
		numExp     = j.fresh()
		expGo      = j.fresh()
		expDigits  = j.fresh()
		expSign    = j.fresh()
		expLoop    = j.fresh()
		expEnd     = j.fresh()
		numShape   = j.fresh()
		numInt     = j.fresh()
		accLoop    = j.fresh()
		accMul     = j.fresh()
		accLast    = j.fresh()
		accRun     = j.fresh()
		accOver    = j.fresh()
		accEnd     = j.fresh()
		numFloat   = j.fresh()
		numKeep    = j.fresh()
		numNeg     = j.fresh()
		numBad     = j.fresh()
		numCut     = j.fresh()
	)
	start := j.t()
	j.mov(start, j.pos())
	neg := j.t()
	j.mov(neg, j.zero())
	frac := j.t()
	j.mov(frac, j.zero())
	any := j.t()
	j.mov(any, j.zero())
	// 可选的负号。首个字节读进"当前数字"寄存器：两条路径（有负号、无负号）都从同一寄存器取值，
	// 避免跳到 numFirst 后读到只在负号路径上写入的槽位。
	digit := j.t()
	c := j.char(0)
	j.mov(digit, c)
	j.br("==", c, j.num(-1), numCut)
	j.br("!=", c, j.num('-'), numFirst)
	j.mov(neg, j.num(1))
	j.adv(1)
	c = j.char(0)
	j.mov(digit, c)
	j.br("==", c, j.num(-1), numCut)
	j.at(numFirst)
	// 整数部分：以 0 开头只能是一个 0
	j.br("!=", digit, j.num('0'), numNotZero)
	j.adv(1)
	j.mov(any, j.num(1))
	after := j.char(0)
	j.br("<", after, j.num('0'), numFrac)
	j.br(">", after, j.num('9'), numFrac)
	j.jmp(numBad) // "01" 这类写法
	j.at(numNotZero)
	j.br("<", digit, j.num('1'), numBad)
	j.br(">", digit, j.num('9'), numBad)
	j.mov(any, j.num(1))
	j.adv(1)
	j.at(numLoop)
	digit = j.char(0)
	j.br("<", digit, j.num('0'), numFrac)
	j.br(">", digit, j.num('9'), numFrac)
	j.adv(1)
	j.jmp(numLoop)

	// 小数部分：出现小数点就必须跟至少一位数字
	j.at(numFrac)
	c = j.char(0)
	j.br("!=", c, j.num('.'), numExp)
	j.mov(frac, j.num(1))
	j.adv(1)
	j.mov(any, j.zero())
	j.at(numFracDot)
	c = j.char(0)
	j.br("<", c, j.num('0'), numFracEnd)
	j.br(">", c, j.num('9'), numFracEnd)
	j.mov(any, j.num(1))
	j.adv(1)
	j.jmp(numFracDot)
	j.at(numFracEnd)
	j.br("!=", any, j.zero(), numExp)
	j.jmp(numBad)

	// 指数部分：e 与 E 均识别，符号可选，后面必须有数字
	j.at(numExp)
	c = j.char(0)
	j.br("==", c, j.num('e'), expGo)
	j.br("!=", c, j.num('E'), numShape)
	j.at(expGo)
	j.mov(frac, j.num(1))
	j.adv(1)
	c = j.char(0)
	j.br("==", c, j.num('+'), expSign)
	// 既非加号也非减号（数字、非法字符、文本读完都归此类）：不消费该字节，留给下面的数字循环
	j.br("!=", c, j.num('-'), expDigits)
	j.at(expSign)
	j.adv(1)
	j.at(expDigits)
	j.mov(any, j.zero())
	j.at(expLoop)
	c = j.char(0)
	j.br("<", c, j.num('0'), expEnd)
	j.br(">", c, j.num('9'), expEnd)
	j.mov(any, j.num(1))
	j.adv(1)
	j.jmp(expLoop)
	j.at(expEnd)
	j.br("!=", any, j.zero(), numShape)
	j.jmp(numBad) // e 后面没有数字

	// 第二段：只有整数面才把这几位数字累加成值
	j.at(numShape)
	j.br("!=", frac, j.zero(), numFloat)
	acc := j.t()
	j.mov(acc, j.zero())
	over := j.t()
	j.mov(over, j.zero())
	p := j.t()
	j.mov(p, start)
	j.br("==", neg, j.zero(), numInt)
	j.mov(p, j.add(p, j.num(1))) // 负号所在位置不算数字
	j.at(numInt)
	lim := j.t()
	j.at(accLoop)
	j.br(">=", p, j.pos(), accEnd)
	digit = j.byteAt(j.add(j.g(j.s.buf), p))
	j.mov(digit, j.minus(digit, j.num('0'))) // 该位的数值（第一段已校验）
	j.br("!=", over, j.zero(), accRun)
	j.br(">", acc, j.num(intLim), accOver)            // 大于上界的十分之一：乘完必然越界
	j.br("!=", acc, j.num(intLim), accMul)            // 小于它：最后一位无需按符号比较
	j.br("!=", p, j.minus(j.pos(), j.num(1)), accMul) // 不是最后一位：无需比较
	j.mov(lim, j.num(7))
	j.br("==", neg, j.zero(), accLast)
	j.mov(lim, j.num(8)) // 负号一侧的幅度可达 2^63
	j.at(accLast)
	j.br(">", digit, lim, accOver)
	j.at(accMul)
	j.mov(acc, j.add(j.bin("*", acc, j.num(10)), digit))
	j.at(accRun)
	j.mov(p, j.add(p, j.num(1)))
	j.jmp(accLoop)
	j.at(accOver)
	j.mov(over, j.num(1))
	j.mov(p, j.add(p, j.num(1)))
	j.jmp(accLoop)
	j.at(accEnd)
	j.br("==", over, j.zero(), numKeep)

	// 小数面（或装不进 int64 的整数面）：截出原文交给 @str_to_float
	j.at(numFloat)
	txt := j.call("@str_sub", j.g(j.s.src), start, j.minus(j.pos(), start))
	bits := j.call("@str_to_float", txt)
	j.void("@free", txt)
	n := j.call(fnJSONNew, j.num(jsonFloatKind))
	j.st(n, jsonFloat, bits)
	j.ret(n)

	// 整数面：一个节点，整数与浮点两个字段各存一份
	j.at(numKeep)
	j.br("==", neg, j.zero(), numNeg)
	j.emit(&ir.Neg{D: acc, S: acc, T: ir.TInt})
	j.at(numNeg)
	node := j.call(fnJSONNew, j.num(jsonIntKind))
	j.st(node, jsonInt, acc)
	dbl := j.t()
	j.emit(&ir.Cvt{D: dbl, S: acc, To: ir.TFlt})
	j.st(node, jsonFloat, dbl)
	j.ret(node)

	j.at(numBad)
	j.errIn(jsonNumber)
	j.ret(j.zero())
	j.at(numCut)
	j.errIn(jsonCut)
	j.ret(j.zero())
}

// nef.json.value()：解析一个值——递归下降的总入口。
//
// 先跳过空白，再按首个字节分派。每个函数序言里的栈深度比较由发射器统一插入，
// 所以"嵌套过深"这条检查不必在这一组里再实现一遍。
//
// en: nef.json.value(): parses one value - the entry point of the descent. Whitespace first,
// then dispatch on the leading byte. The stack comparison in the prologue is placed by the
// emitter for every function, so "nested too deep" needs no second implementation in this
// group.
func jsonBuildValue(l *L) {
	j, ok := openJSON(l, fnJSONValue, 0)
	if !ok {
		return
	}
	var (
		cut    = j.fresh()
		notObj = j.fresh()
		notArr = j.fresh()
		notStr = j.fresh()
		notNum = j.fresh()
		notDig = j.fresh()
	)
	j.wsIn()
	c := j.char(0)
	j.br("==", c, j.num(-1), cut)
	j.br("!=", c, j.num('{'), notObj)
	j.ret(j.call(fnJSONObject))
	j.at(notObj)
	j.br("!=", c, j.num('['), notArr)
	j.ret(j.call(fnJSONArray))
	j.at(notArr)
	j.br("!=", c, j.num('"'), notStr)
	j.ret(j.call(fnJSONString))
	j.at(notStr)
	j.br("!=", c, j.num('-'), notNum)
	j.ret(j.call(fnJSONNumber))
	j.at(notNum)
	for _, one := range []struct {
		c    int64
		word string
		kind int64
		why  int64
	}{
		{'t', "true", jsonTrue, jsonTrueLit},
		{'f', "false", jsonFalse, jsonFalseLit},
		{'n', "null", jsonNull, jsonNullLit},
	} {
		no := j.fresh()
		j.br("!=", c, j.num(one.c), no)
		j.ret(j.call(fnJSONLit, j.str(one.word), j.num(one.kind), j.num(one.why)))
		j.at(no)
	}
	// 以 0～9 开头同样解析为数；其余首字符在这里都不匹配
	j.br("<", c, j.num('0'), notDig)
	j.br(">", c, j.num('9'), notDig)
	j.ret(j.call(fnJSONNumber))
	j.at(notDig)
	j.errIn(jsonValue)
	j.ret(j.zero())
	j.at(cut)
	j.errIn(jsonCut)
	j.ret(j.zero())
}

// nef.json.object()：解析一个 `{...}`。
//
// 节点与两张表在入口处立即建立：中途失败时 nef.json.free 凭这一个节点就能把已接受的部分
// 全部拆解；只有"尚未进表的键文本"要单独释放——此刻它的持有者还是当前解析流程。
// 同一个键第二次出现由 nef.json.pair 处理：后值覆盖前值、位置不变。
//
// en: nef.json.object(): parses a `{...}`. The node and its two tables are built on entry, so a
// failure halfway lets nef.json.free dismantle everything already accepted; only the key text
// that has not entered the table yet must be released separately - at that moment this pass
// still holds it. A key appearing a second time is handled by nef.json.pair: the later value
// overwrites, the position does not move.
func jsonBuildObject(l *L) {
	j, ok := openJSON(l, fnJSONObject, 0)
	if !ok {
		return
	}
	var (
		objTop   = j.fresh()
		objNoKey = j.fresh()
		objColon = j.fresh()
		objChild = j.fresh()
		objNext  = j.fresh()
		objComma = j.fresh()
		objClose = j.fresh()
		objCut   = j.fresh()
		objFree  = j.fresh()
		objClean = j.fresh()
	)
	node := j.call(fnJSONNew, j.num(jsonObjectKind))
	keys := j.call("@list_new", j.zero(), j.num(tagStr)) // 键表是 list<string>
	j.st(node, jsonKeys, keys)
	vals := j.call("@list_new", j.zero(), j.zero()) // 值表无需记录元素类型：节点本身就是那张表
	j.st(node, jsonVals, vals)
	j.adv(1) // 移过左花括号：进入本函数时游标正指向它
	j.wsIn()
	j.br("!=", j.char(0), j.num('}'), objTop)
	j.adv(1)
	j.ret(node) // 空的对象

	j.at(objTop)
	j.wsIn() // 逗号后可以有空格：本段起点游标不一定正指向键的引号
	j.br("==", j.char(0), j.num(-1), objCut)
	j.br("!=", j.char(0), j.num('"'), objNoKey)
	key := j.call(fnJSONText) // 键是一段解码后的文本，与值用同一读取路径
	j.br("==", key, j.zero(), objClean)
	j.wsIn()
	j.br("==", j.char(0), j.num(':'), objColon)
	j.errIn(jsonColon)
	j.jmp(objFree)
	j.at(objColon)
	j.adv(1)
	child := j.call(fnJSONValue)
	j.br("!=", child, j.zero(), objChild)
	j.jmp(objFree) // 值读取失败：键尚未进表，先释放它
	j.at(objChild)
	j.void(fnJSONPair, node, key, child)
	j.wsIn()
	j.at(objNext)
	c := j.char(0)
	j.br("==", c, j.num(-1), objCut)
	j.br("!=", c, j.num(','), objComma)
	j.adv(1)
	j.jmp(objTop)
	j.at(objComma)
	j.br("==", c, j.num('}'), objClose)
	j.errIn(jsonObjEnd)
	j.jmp(objClean)
	j.at(objClose)
	j.adv(1)
	j.ret(node)

	j.at(objNoKey)
	j.errIn(jsonKey)
	j.jmp(objClean)
	j.at(objCut)
	j.errIn(jsonCut)
	j.jmp(objClean)
	j.at(objFree)
	j.void("@free", key)
	j.at(objClean)
	j.void(fnJSONFree, node)
	j.ret(j.zero())
}

// nef.json.array()：解析一个 `[...]`。
//
// en: nef.json.array(): parses a `[...]`.
func jsonBuildArray(l *L) {
	j, ok := openJSON(l, fnJSONArray, 0)
	if !ok {
		return
	}
	var (
		arrTop   = j.fresh()
		arrGot   = j.fresh()
		arrComma = j.fresh()
		arrClose = j.fresh()
		arrCut   = j.fresh()
		arrClean = j.fresh()
	)
	node := j.call(fnJSONNew, j.num(jsonArrayKind))
	vals := j.call("@list_new", j.zero(), j.num(tagList)) // 值表存放的是节点句柄
	j.st(node, jsonVals, vals)
	j.adv(1) // 移过左方括号：进入本函数时游标正指向它
	j.wsIn()
	j.br("!=", j.char(0), j.num(']'), arrTop)
	j.adv(1)
	j.ret(node) // 空的数组
	j.at(arrTop)
	child := j.call(fnJSONValue)
	j.br("!=", child, j.zero(), arrGot)
	j.jmp(arrClean) // 读取失败：错误已由发现问题的层记录
	j.at(arrGot)
	j.void("@list_add", vals, child)
	j.wsIn()
	c := j.char(0)
	j.br("==", c, j.num(-1), arrCut)
	j.br("!=", c, j.num(','), arrComma)
	j.adv(1)
	j.jmp(arrTop)
	j.at(arrComma)
	j.br("==", c, j.num(']'), arrClose)
	j.errIn(jsonArrEnd)
	j.jmp(arrClean)
	j.at(arrClose)
	j.adv(1)
	j.ret(node)
	j.at(arrCut)
	j.errIn(jsonCut)
	j.jmp(arrClean)
	j.at(arrClean)
	j.void(fnJSONFree, node)
	j.ret(j.zero())
}

// nef.json.pair(对象, 键, 值)：把一对键值写进对象。
//
// 键表里已有这个键，就替换值表的对应位置、销毁旧子树，并释放多余的那条键文本；
// 没有就在两张表各追加一个位置（同一个键第二次出现：后值覆盖前值、位置不变）。
// 键按文本内容比较（字符串判等逐字符比较），与 dict 一样走 @list_index_of 的模式 1。
//
// en: nef.json.pair(object, key, value): stores a pair into an object. If the key table already
// knows this key, the value table's position is replaced, the old subtree is dismantled and the
// extra key text is released; if not, one position is appended to each table (a key written
// twice keeps its original position and takes the later value). Keys compare by text content
// (string equality compares characters), using @list_index_of mode 1 like dict.
func jsonBuildPair(l *L) {
	j, ok := openJSON(l, fnJSONPair, 3)
	if !ok {
		return
	}
	node, key, child := param(0), param(1), param(2)
	keys := j.ld(node, jsonKeys)
	vals := j.ld(node, jsonVals)
	i := j.call("@list_index_of", keys, key, j.num(1))
	fresh := j.fresh()
	j.br("==", i, j.num(-1), fresh)
	// 键已存在：替换值、销毁旧子树、释放多余的那条键文本
	cell := j.idx(j.ld(vals, runtime.CellData), i, 8)
	old := j.ld(cell, 0)
	j.st(cell, 0, child)
	j.void(fnJSONFree, old)
	j.void("@free", key)
	j.ret(j.zero())
	j.at(fresh)
	j.void("@list_add", keys, key)
	j.void("@list_add", vals, child)
	j.ret(j.zero())
}

// nef.json.parse(一段文本)：解析整篇输入，返回树根或空句柄。
//
// 先设置七个静态槽位，解析一个值，再检查尾部是否残留内容。值之后还有字符属于
// "尾部多余数据"：先拆解已建好的树，再返回空句柄。
//
// en: nef.json.parse(text): parses the whole input and returns the tree root or an empty
// handle. It sets up the seven static slots, parses one value, then checks whether anything is
// left over. Text after the value is "trailing junk": the tree already built is dismantled
// first and an empty handle is returned.
func jsonBuildParse(l *L) {
	j, ok := openJSON(l, fnJSONParse, 1)
	if !ok {
		return
	}
	src := param(0)
	j.setG(j.s.src, src)
	j.setG(j.s.buf, j.add(src, j.num(jsonTextBody)))
	j.setG(j.s.len, j.ld(src, runtime.StrBytes))
	j.setG(j.s.pos, j.zero())
	j.setG(j.s.err, j.zero())
	j.setG(j.s.errAt, j.zero())
	var (
		bad  = j.fresh()
		tail = j.fresh()
	)
	root := j.call(fnJSONValue)
	j.br("==", root, j.zero(), bad)
	j.wsIn()
	j.br("==", j.pos(), j.textLen(), tail)
	// 尾部多余数据：这棵树无人接收，就地拆解
	j.void(fnJSONFree, root)
	j.errIn(jsonJunk)
	j.at(bad)
	j.ret(j.zero())
	j.at(tail)
	j.ret(root)
}

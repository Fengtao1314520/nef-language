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

// Package rtre 是 nef 那一组 regular_expression 的参考实现（实现的是"最小可用"子集）。
//
// 与 @put_float 同一条路线：先在 Go 这边写一份可逐值比对的实现，把语义一次固定住
// （哪些记号支持、空匹配怎么处理、字符组按什么算），再往机器码移植。
// 这一份不进产物——产物只依赖 kernel32，运行时没有 Go；它是**答案**，不是实现。
//
// 两条贯穿全局的规则：
//
//  1. **一切按码点走**。式子里的字面字符（哪怕占三个字节）是一个记号，`.` 是一个码点，
//     字符组是一堆码点范围。VM 的一步就是一个码点，于是没有"半个字符"这种状态，
//     机器码那一版也照同一个模型（一个码点一次比较），不用先拆字节。
//  2. **形状是 Thompson 的 NFA ＋ 一条按优先级排的线程表**（Pike 那台 VM 去掉捕获）：
//     不递归回溯，式子多少状态就最多多少条线程，线性时间。贪婪由 split 出边的顺序给出：
//     先取"再吃一个"，失败才取"跳过"；`|` 先取左边——与 Perl／Go 的"最左、按书写顺序优先"同一条。
//
// en: Package rtre is the reference implementation of nef's regular_expression group (the
// "minimum usable" subset). Same route as @put_float: first
// write an implementation on the Go side that can be compared value by value, fixing the
// semantics once (which tokens are supported, how an empty match is handled, what a character
// class counts over), then port it to machine code. This copy never reaches the product - the
// product only depends on kernel32 and has no Go at runtime; it is the ANSWER key, not the
// implementation. Two rules run through the whole package. 1. Everything moves by code point. A
// literal character in the pattern, even three bytes wide, is one token, `.` is one code point,
// a class is a set of code point ranges. One "step" of the VM is one code point, so there is no
// such state as half a character, and the machine-code version follows the same model (one code
// point per comparison) without splitting bytes first. 2. The shape is Thompson's NFA plus a
// priority-ordered thread list (Pike's VM with captures removed): no recursive backtracking, at
// most as many threads as the pattern has states, linear time. Greediness comes free from the
// order of split's out edges - take "eat one more" first, and only if that fails "skip"; `|`
// tries the left branch first, which is the same leftmost, written-order-wins rule Perl and Go
// use.
package rtre

import (
	"fmt"
	"unicode/utf8"
)

// ErrBad 是式子写错：对用户报 10071 号错误。
//
// en: ErrBad means the pattern is written wrong: it is reported to the user as code 10071.
type ErrBad struct{ Msg string }

func (e *ErrBad) Error() string { return e.Msg }

func bad(format string, a ...any) error { return &ErrBad{Msg: fmt.Sprintf(format, a...)} }

// rule 是字符组里的一段闭区间（码点）。范围数量都是个位数，线性扫描比建位图更合适。
//
// en: rule is one closed interval (by code point) inside a character class. Range counts stay
// in single digits, so a linear scan fits better than building a bitmap.
type rule struct{ lo, hi rune }

// class 是一组码点范围，neg 表示取反（`[^...]`、`\D` 那一大写的一支）。
//
// en: class is a set of code point ranges; neg means the complement (`[^...]` and the uppercase
// forms such as `\D`).
type class struct {
	rules []rule
	neg   bool
}

func (cl *class) has(r rune) bool {
	for _, u := range cl.rules {
		if r >= u.lo && r <= u.hi {
			return !cl.neg
		}
	}
	if cl.neg {
		return true
	}
	return false
}

func (cl *class) add(lo, hi rune) { cl.rules = append(cl.rules, rule{lo, hi}) }

const (
	iRune  = iota + 1 // 一个码点，等于 arg
	iClass            // 一个码点，落在 cl 里
	iAny              // 任意一个码点
	iSplit            // out1 优先于 out2
	iJmp
	iBol // ^ ——只匹配文本开头，没有多行标志
	iEol // $ ——只匹配文本末尾
	iMatch
)

// inst 是一条指令：一个码点宽的匹配 ＋ 一到两条出边。
//
// en: inst is one instruction: a match one code point wide plus one or two out edges.
type inst struct {
	op         byte
	arg        rune
	out1, out2 int
	cl         *class
}

type program struct {
	prog  []inst
	start int // 入口指令下标：发指令的顺序不等于走的顺序，`a|b` 的 split 就是最后发出来的
}

// emit 落一条指令，返回它的下标。
//
// out1／out2 一律由调用方设好：-1 表示"这一头还留着洞"，其余值表示已经接上。
// 这里不清零：发 split 的各调用点都带着出边进来，清零会破坏式子结构。
//
// en: emit writes one instruction and returns its index. out1/out2 are always set by the
// caller: -1 means "this end is still a hole", anything else means it is connected. Nothing is
// zeroed here - every place that emits a split comes in with its out edges already set, and
// clearing them would corrupt the pattern.
func (p *program) emit(in inst) int {
	p.prog = append(p.prog, in)
	return len(p.prog) - 1
}

// hole 发一条一个码点宽的匹配指令，两头的出边都留着（它就是一个片段的尾巴）。
//
// en: hole emits one instruction that matches a single code point and leaves both out edges
// open - it is the tail of a fragment.
func (c *compiler) hole(in inst) frag {
	in.out1, in.out2 = -1, -1
	return one(c.p.emit(in))
}

// patch 把一个片段留下的"出口"接到 to 上。出口的约定与 Pike 那份一样：
// split 的出口是 out2（"跳过"那一条），其余指令的出口是 out1（"往下走"那一条）。
//
// en: patch connects the "exit" a fragment leaves behind to to. The exit convention is Pike's:
// a split exits through out2 (the "skip" edge), every other instruction exits through out1 (the
// "go on" edge).
func (p *program) patch(holes []int, to int) {
	for _, h := range holes {
		if p.prog[h].op == iSplit {
			p.prog[h].out2 = to
		} else {
			p.prog[h].out1 = to
		}
	}
}

type frag struct {
	start int
	holes []int
}

func one(at int) frag { return frag{start: at, holes: []int{at}} }

// compiler 是递归下降：alt = concat ('|' concat)*，concat = piece*，piece = atom 量词?
//
// en: compiler is a recursive descent: alt = concat ('|' concat)*, concat = piece*, piece =
// atom quantifier?.
type compiler struct {
	src []byte
	i   int
	p   *program
}

func (c *compiler) eof() bool { return c.i >= len(c.src) }

func (c *compiler) r() (rune, int) {
	r, n := utf8.DecodeRune(c.src[c.i:])
	if r == utf8.RuneError && n <= 1 {
		return -1, 0 // 断掉的字节：交给调用方判错
	}
	return r, n
}

// Compile 把式子编成指令串。
//
// en: Compile turns a pattern into a run of instructions.
func Compile(pat string) (*Regexp, error) {
	if pat == "" {
		return nil, bad("式子是空的")
	}
	c := &compiler{src: []byte(pat), p: &program{}}
	f, err := c.alt()
	if err != nil {
		return nil, err
	}
	if !c.eof() {
		return nil, bad("式子里多出来的 %q（括号没配上？）", string(c.src[c.i]))
	}
	end := c.p.emit(inst{op: iMatch, out1: -1, out2: -1})
	c.p.patch(f.holes, end)
	c.p.start = f.start
	return &Regexp{pat: pat, p: c.p}, nil
}

func (c *compiler) alt() (frag, error) {
	left, err := c.concat()
	if err != nil {
		return frag{}, err
	}
	for !c.eof() && c.runeAt() == '|' {
		c.i++
		right, err := c.concat()
		if err != nil {
			return frag{}, err
		}
		sp := c.p.emit(inst{op: iSplit, out1: left.start, out2: right.start})
		left = frag{start: sp, holes: append(append([]int{}, left.holes...), right.holes...)}
	}
	return left, nil
}

func (c *compiler) runeAt() rune {
	r, _ := c.r()
	return r
}

func (c *compiler) concat() (frag, error) {
	var head *frag
	for !c.eof() {
		r := c.runeAt()
		if r == '|' || r == ')' {
			break
		}
		f, err := c.piece()
		if err != nil {
			return frag{}, err
		}
		if head == nil {
			cp := f
			head = &cp
			continue
		}
		c.p.patch(head.holes, f.start)
		head.holes = f.holes
	}
	if head == nil {
		// `a|` 那种空的一支：一条不消费任何输入的 jmp。允许它，但配量词就是死循环，
		// 量词那一步会当场拒绝（见 piece）。
		j := c.p.emit(inst{op: iJmp, out1: -1, out2: -1})
		return one(j), nil
	}
	return *head, nil
}

func (c *compiler) piece() (frag, error) {
	f, err := c.atom()
	if err != nil {
		return frag{}, err
	}
	if c.eof() {
		return f, nil
	}
	switch c.runeAt() {
	case '{':
		return frag{}, bad("{m,n} 这一版不支持（最小可用子集）")
	case '*':
		c.i++
		sp := c.p.emit(inst{op: iSplit, out1: f.start, out2: -1})
		c.p.patch(f.holes, sp)
		return one(sp), nil
	case '+':
		c.i++
		sp := c.p.emit(inst{op: iSplit, out1: f.start, out2: -1})
		c.p.patch(f.holes, sp)
		return frag{start: f.start, holes: []int{sp}}, nil
	case '?':
		c.i++
		sp := c.p.emit(inst{op: iSplit, out1: f.start, out2: -1})
		return frag{start: sp, holes: append([]int{sp}, f.holes...)}, nil
	}
	return f, nil
}

func (c *compiler) atom() (frag, error) {
	r, n := c.r()
	if r < 0 {
		return frag{}, bad("式子里有断掉的 UTF-8 字节")
	}
	c.i += n
	switch r {
	case '(':
		if !c.eof() && c.runeAt() == '?' {
			return frag{}, bad("(?...) 这一版不支持：命名组、标志、先行断言都没有")
		}
		if !c.eof() && c.runeAt() == ')' {
			return frag{}, bad("空的 () 没有意义：那种东西配上量词就没有尽头")
		}
		f, err := c.alt()
		if err != nil {
			return frag{}, err
		}
		if c.eof() || c.runeAt() != ')' {
			return frag{}, bad("括号没配上")
		}
		c.i++
		return f, nil
	case '[':
		return c.class()
	case '.':
		return c.hole(inst{op: iAny}), nil
	case '^':
		return c.hole(inst{op: iBol}), nil
	case '$':
		return c.hole(inst{op: iEol}), nil
	case '\\':
		return c.escape()
	case ')':
		return frag{}, bad(") 没有配对的 (")
	case '|':
		return frag{}, bad("| 前面没有东西")
	case '*', '+', '?':
		return frag{}, bad("%q 前面没有可以量的东西", string(r))
	default:
		return c.hole(inst{op: iRune, arg: r}), nil
	}
}

// escape 识别那六个预定义类与大写的取反，其余一律按字面处理（`\\`、`\.`、`\*`、`\[`、`\日`…）。
//
// en: escape recognises the six classes and their uppercase negations; everything else is taken
// literally (`\\`, `\.`, `\*`, `\[`, `\日` ...).
func (c *compiler) escape() (frag, error) {
	r, n := c.r()
	if r < 0 || n == 0 {
		return frag{}, bad("式子以一个反斜杠收尾")
	}
	c.i += n
	switch r {
	case 'd', 'D', 'w', 'W', 's', 'S':
		return c.hole(inst{op: iClass, cl: predef(r)}), nil
	case 'b', 'B':
		return frag{}, bad(`\%s 这类词边界这一版不支持`, string(r))
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return frag{}, bad("反向引用（\\%s）这一版不支持", string(r))
	default:
		return c.hole(inst{op: iRune, arg: r}), nil
	}
}

// class 处理 `[...]`。`-` 位于首尾时按字面处理；`]` 要作字符须放在第一位；组内 `\` 识别那六个预定义类。
//
// en: class handles `[...]`. A `-` at the start or end is literal; to use `]` as a character put
// it first; inside a class `\` recognises the same six classes.
func (c *compiler) class() (frag, error) {
	cl := &class{}
	if !c.eof() && c.runeAt() == '^' {
		c.i++
		cl.neg = true
	}
	first := true
	for {
		if c.eof() {
			return frag{}, bad("字符组没有收尾的 ]")
		}
		r, n := c.r()
		if r < 0 {
			return frag{}, bad("字符组里有断掉的 UTF-8 字节")
		}
		c.i += n
		if r == ']' && !first {
			break
		}
		first = false
		if r == '\\' {
			e, en := c.r()
			if en == 0 {
				return frag{}, bad("字符组以一个反斜杠收尾")
			}
			c.i += en
			switch e {
			case 'd', 'w', 's':
				for _, u := range predef(e).rules {
					cl.add(u.lo, u.hi)
				}
				continue
			case 'D', 'W', 'S':
				return frag{}, bad("取反的预定义类（\\%s）放进字符组里没有一致的说法，写在组外头可以用", string(e))
			case 'b':
				r = '\b'
			default:
				r = e
			}
		}
		lo, hi := r, r
		if !c.eof() && c.runeAt() == '-' {
			// 尾巴上的 `-` 后面就是 `]`：那是它自己，不是范围
			save := c.i
			c.i++
			if c.eof() {
				c.i = save
			} else if c.runeAt() == ']' {
				c.i = save
			} else {
				h, hn := c.r()
				if h < 0 {
					return frag{}, bad("字符组的范围没有上界")
				}
				c.i += hn
				if h == '\\' {
					h2, hn2 := c.r()
					if hn2 == 0 {
						return frag{}, bad("字符组的范围以一个反斜杠收尾")
					}
					c.i += hn2
					h = h2
				}
				if h < lo {
					return frag{}, bad("字符组的范围反了：%q 在 %q 前面", string(h), string(lo))
				}
				hi = h
			}
		}
		cl.add(lo, hi)
	}
	return c.hole(inst{op: iClass, cl: cl}), nil
}

// predef 是那六个预定义类。`\d` `\w` `\s` 只覆盖 ASCII 范围——非 ASCII 码点不属于任何
// 预定义类，要匹配它就用字面字符或 `.`。
//
// en: predef is the six classes. `\d` `\w` `\s` cover only the ASCII range - a non-ASCII code
// point belongs to no predefined class, so match it by literal or with `.`.
func predef(kind rune) *class {
	cl := &class{}
	one := func(lo, hi rune) { cl.add(lo, hi) }
	switch kind | 0x20 {
	case 'd':
		one('0', '9')
	case 'w':
		one('0', '9')
		one('a', 'z')
		one('A', 'Z')
		one('_', '_')
	case 's':
		for _, b := range []rune{' ', '\t', '\n', '\v', '\f', '\r'} {
			one(b, b)
		}
	}
	if kind >= 'A' && kind <= 'Z' {
		cl.neg = true
	}
	return cl
}

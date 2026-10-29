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

// run.go 描述这台 VM 的运行方式：一条按优先级排的线程表，一个位置（一个码点）一个位置往前推。
//
// **位置一律是字节偏移**，"走一格"是走一个码点（按首字节解出它占几字节）。这么做是为了
// 让这一份与机器码那一版一字不差地对得上：机器码里文本就是一块 UTF-8 字节，先把它拆成
// 码点数组再匹配，等于多要两份与文本同长的表，而匹配本身只要"这个位置是哪个码点、占几字节"。
// 语义没变——每个记号还是吃一个码点，`.` 还是一个字符，切出来的区间必然落在码点边界上。
//
// en: run.go is how this VM runs: a priority-ordered thread list, advancing one position (one
// code point) at a time. Positions are always byte offsets, and "one step" means one code point
// (decode its width from the lead byte). Doing it this way keeps this copy and the machine-code
// copy identical down to the last detail: in machine code the text is a block of UTF-8 bytes,
// and splitting it into a code point array first would demand two extra tables as long as the
// text, while matching itself only ever needs "which code point is at this position and how
// many bytes does it take". The semantics are unchanged - every token still consumes one code
// point, `.` is still one character, and any slice cut this way necessarily lands on a code
// point boundary.
package rtre

import "unicode/utf8"

// Regexp 是一份编好的式子，可以反复用，跑的时候不再改它。
//
// en: Regexp is a compiled pattern: it can be reused, and running never modifies it.
type Regexp struct {
	pat string
	p   *program
}

// width 返回 s 里 pos 处那个码点占几字节。断掉的字节按一个字节计——这一层不验编码，
// 读文件那条路径已经验过，式子与文本里的字节都是干净的。
//
// en: width returns how many bytes the code point at pos inside s occupies. A broken byte
// counts as one - this layer does not validate encoding; the file-reading path already did,
// so the bytes in patterns and text are clean.
func width(s string, pos int) int {
	_, n := utf8.DecodeRuneInString(s[pos:])
	if n == 0 {
		return 1
	}
	return n
}

// nextCP 从 pos 往前挪一个码点。
//
// en: nextCP moves pos forward by one code point.
func nextCP(s string, pos int) int {
	if pos >= len(s) {
		return len(s)
	}
	return pos + width(s, pos)
}

// matchAt 从第 start 个字节起试，返回"匹配到第几个字节"（可以是 start：那就是空的）。
//
// 线程表每一个槽换一次。add 那一趟走完 split／jmp／锚这些不消费输入的边，同一个槽只进一次
// （seen 标记）。到达 iMatch 时要做两件事：把这个位置的落点记下来，并且**封住这一趟的后半**
// ——排在它后面的线程优先级都比它低，不可能再推翻这个结果；已经进表的线程都比它优先，让它们
// 继续走，走到哪个位置再到达 iMatch，就以那一次为准（那是更优先的一支）。这就是贪婪与
// "`|` 先写的分支优先"的全部机制，不需要回溯。
//
// en: matchAt tries from byte start and returns "how many bytes the match reached", which may
// be start - that is an empty match. The thread list is swapped one slot at a time. The add
// pass walks the edges that consume nothing (split/jmp/anchors), and a slot enters only once
// (the seen mark). Reaching iMatch does two things: it records where this position landed, and
// it CLOSES the second half of this pass - the threads behind it all have lower priority and
// cannot overturn it, while threads already in the table are more preferred, so let them keep
// going; if one of them hits iMatch at some later position, that attempt is the one that counts
// (it is the more preferred branch). That is the entire mechanism behind greediness and "the
// branch written first wins", with no backtracking.
func (p *program) matchAt(s string, start int) (int, bool) {
	prog := p.prog
	n := len(s)
	// mark 是"这个位置已经进过没有"，每一个槽换一次代号（epoch）：一条线程在同一个位置上只进一次。
	// 不复位数组、只涨代号，与机器码那一版同一个办法。
	mark := make([]int, len(prog))
	epoch := 0
	sealed := false
	var end, found = 0, false

	var add func(pc int, pos int, list *[]int)
	add = func(pc int, pos int, list *[]int) {
		if sealed || pc < 0 || mark[pc] == epoch {
			return
		}
		mark[pc] = epoch
		switch in := prog[pc]; in.op {
		case iSplit:
			add(in.out1, pos, list)
			add(in.out2, pos, list)
		case iJmp:
			add(in.out1, pos, list)
		case iBol:
			if pos == 0 {
				add(in.out1, pos, list)
			}
		case iEol:
			if pos == n {
				add(in.out1, pos, list)
			}
		case iMatch:
			end, found = pos, true
			sealed = true
		default:
			*list = append(*list, pc)
		}
	}

	cur, next := make([]int, 0, len(prog)), make([]int, 0, len(prog))
	epoch++
	add(p.start, start, &cur)
	for pos := start; len(cur) > 0; {
		if pos >= n {
			break // 文本已到尽头：表里剩下的线程没有输入可消费，不会再有结果
		}
		r, _ := utf8.DecodeRuneInString(s[pos:])
		w := width(s, pos)
		next = next[:0]
		epoch++
		sealed = false
		for _, pc := range cur {
			in := prog[pc]
			ok := false
			switch in.op {
			case iAny:
				ok = true
			case iRune:
				ok = r == in.arg
			case iClass:
				ok = in.cl.has(r)
			}
			if ok {
				add(in.out1, pos+w, &next)
			}
		}
		pos += w
		cur, next = next, cur
	}
	return end, found
}

// Match 检查式子能否在 s 的某处匹配（不要求整串）。空匹配也算数。
//
// 末尾那个位置也要试：`$` 与只能匹配空的式子就落在那儿。
//
// en: Match checks whether the pattern matches somewhere inside s (the whole string is not
// required). An empty match counts. The position past the end is tried too: that is
// where `$` and a pattern that can only match empty land.
func (r *Regexp) Match(s string) bool {
	for start := 0; ; {
		if _, ok := r.p.matchAt(s, start); ok {
			return true
		}
		if start >= len(s) {
			return false
		}
		start = nextCP(s, start)
	}
}

// scan 返回从第 from 个字节起第一个匹配的位置。modeNonEmpty 为真时空的那一次一律跳过——
// "空匹配不切、不重复占位"这条语言规则就在这里落实。
//
// en: scan returns the position of the first match at or after byte from. When modeNonEmpty is
// set, empty matches are skipped - this is where the rule "an empty match does not cut and
// does not occupy a place" gets enforced.
func (r *Regexp) scan(s string, from int, modeNonEmpty bool) (int, int, bool) {
	for start := from; ; {
		if !modeNonEmpty || start < len(s) {
			if end, ok := r.p.matchAt(s, start); ok && (!modeNonEmpty || end > start) {
				return start, end, true
			}
		}
		if start >= len(s) {
			return -1, -1, false
		}
		start = nextCP(s, start)
	}
}

// FindAll 返回所有互不重叠的非空匹配，按出现先后排。
//
// en: FindAll returns every non-overlapping non-empty match, in order of appearance.
func (r *Regexp) FindAll(s string) []string {
	var out []string
	for from := 0; ; {
		a, b, ok := r.scan(s, from, true)
		if !ok {
			return out
		}
		out = append(out, s[a:b])
		from = b
	}
}

// ReplaceAll 把每个非空匹配换成替身。替身里没有 `$1` 这种引用（不做捕获）。
//
// en: ReplaceAll swaps every non-empty match for the replacement. The replacement has no `$1`
// style references: captures are not implemented.
func (r *Regexp) ReplaceAll(s, rep string) string {
	var out []byte
	from, cut := 0, 0
	for {
		a, b, ok := r.scan(s, from, true)
		if !ok {
			out = append(out, s[cut:]...)
			return string(out)
		}
		out = append(out, s[cut:a]...)
		out = append(out, rep...)
		from, cut = b, b
	}
}

// Split 按式子切开：切点自己丢掉，紧挨着的两段该是空串就留空串（与 s.split 同一条）。
//
// en: Split cuts on the pattern: the cut point itself is dropped, and two adjacent pieces that
// should be empty stay empty - the same rule as s.split.
func (r *Regexp) Split(s string) []string {
	var out []string
	from, cut := 0, 0
	for {
		a, b, ok := r.scan(s, from, true)
		if !ok {
			out = append(out, s[cut:])
			return out
		}
		out = append(out, s[cut:a])
		from, cut = b, b
	}
}

// FindStringIndex 返回第一个非空匹配的字节区间（没有就 [-1,-1]）。给逐一比对与调用方看细节用。
//
// en: FindStringIndex returns the byte range of the first non-empty match, or [-1,-1] when
// there is none. Used by the value-by-value comparison and by callers who want the detail.
func (r *Regexp) FindStringIndex(s string) [2]int {
	if a, b, ok := r.scan(s, 0, true); ok {
		return [2]int{a, b}
	}
	return [2]int{-1, -1}
}

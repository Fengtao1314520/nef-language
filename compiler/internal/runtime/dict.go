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

//
// The Nef Output Exception (see the LICENSE-EXCEPTION file at the repository
// root) applies to this file: code you write and the binaries you compile with
// it are not subject to the Apache License, and you have no obligation to
// open-source them.

// dict 与 hashset 共用的那一张表（块头＋容器对象那五个字段）。
//
// 桶数组每个位置 24 字节：状态、键、值。`hashset` 用不上值那个字段，但两种容器走同一条路——
// 宁可空着八个字节，也不维护两份容易走偏的探测代码。
// 冲突向右挪一个位置、走到末尾折回开头（线性探测）；删除不真正清除，把状态写成墓碑，
// 否则一条键的探测链会被后半截断掉。
//
// 键怎么算散列、怎么算相等，记在容器对象自己身上（cMode）：再散列那一刻手上没有类型
// 信息可用，所以这个值必须在建表时就写进去。
//
// en: The table shared by dict and hashset (the header plus the container object's five
// fields). Each bucket is 24 bytes: state, key, value. A `hashset` has no use for the value
// field, but both containers take one path - better eight unused bytes than two copies of
// probing code that can drift apart. On collision move one slot right, wrapping at the end
// (linear probing); a delete does not really clear the slot but writes a tombstone in the
// state, otherwise the second half of a key's probe chain would be cut. How a key hashes and
// how it compares equal is recorded on the container object itself (cMode): at rehash time
// there is no type information in hand, so that value must be written when the table is built.
package runtime

import "neflang.org/nf/internal/x64"

// 桶里的状态与格内偏移。
//
// en: Bucket states and the offsets inside a slot.
const (
	stateEmpty = 0 // 从未存过键值：探测链到此结束
	stateUsed  = 1 // 正存着键值
	stateTomb  = 2 // 存过、已删除：不能当链尾，但可以用来插入

	stride = 24
	bState = 0
	bKey   = 8
	bVal   = 16

	// 桶数始终是 2 的幂，取余就是清高位。最少 8 个桶，装过四分之三就翻倍。
	minBuckets = 8
	growNum    = 3
	growDen    = 4
)

// 散列用的那几个乘数是 64 位的位型，超出 int64 的正数范围，而 MovImm 只收 int64，
// 所以绕一次变量：常量到不了这一步，必须经一个变量把位型搬过去。
//
// en: The hash multipliers are 64-bit patterns above the positive int64 range, and MovImm only
// accepts int64, so they detour through a variable: the constant cannot reach this step and
// must be carried by a variable holding the pattern.
var (
	fnvBase  = bits(0xcbf29ce484222325) // FNV-1a 的初始值
	fnvPrime = bits(0x100000001b3)      // FNV-1a 的素数
	mix1     = bits(0xff51afd7ed558ccd)
	mix2     = bits(0xc4ceb9fe1a85ec53) // splitmix64 收尾那两个乘数
	nullKey  = bits(0x5A5A5A5A)
)

func bits(v uint64) int64 { return int64(v) }

// @hash(rcx=键, rdx=模式)：返回 rax=散列值。
//
// 模式 0 给整数家族、枚举与"按身份判等"的那些句柄：用 splitmix64 的收尾三步把一个
// 通常很小的数分散开——不分散的话 0、1、2、3 这四个键会落在相邻的桶里。
// 模式 1 走文本：逐字节 FNV-1a。空句柄也是合法的键，给它一个定值，不去读 [null]。
//
// en: @hash (rcx = key, rdx = mode): returns rax = the hash. Mode 0 serves the integer family,
// enums and the identity-compared handles: the final three steps of splitmix64 spread a number
// that is usually very small - without spreading, the four keys 0, 1, 2, 3 would land in
// adjacent buckets. Mode 1 is for text: byte by byte FNV-1a. An empty handle is also a legal
// key and gets a fixed value rather than reading [null].
func hashKey(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)

	num := b.label()
	out := b.label()
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, num) // 模式 0 才走数字那一路

	// 文本：先取定值，再逐字节乘素数
	a.MovImm(x64.RAX, fnvBase)
	b.put(2, x64.RAX)
	b.get(x64.RSI, 0)
	null := b.label()
	a.Test(x64.RSI, x64.RSI)
	a.Jcc(x64.E, null)
	a.MovLoad(x64.R9, x64.RSI, StrBytes)
	a.LeaMem(x64.RSI, x64.RSI, 16)
	b.put(3, x64.RSI)
	a.MovImm(x64.R8, fnvPrime)
	b.put(4, x64.R8)
	loop := b.label()
	done := b.label()
	a.Mark(loop)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, done)
	b.get(x64.RSI, 3)
	a.MovZX8(x64.RDX, x64.RSI, 0)
	b.get(x64.RAX, 2)
	a.Xor(x64.RAX, x64.RDX)
	b.get(x64.R8, 4)
	a.Imul(x64.RAX, x64.R8)
	b.put(2, x64.RAX)
	b.get(x64.RSI, 3)
	a.AddImm(x64.RSI, 1)
	b.put(3, x64.RSI)
	a.AddImm(x64.R9, -1)
	a.Jmp(loop)
	a.Mark(done)
	b.get(x64.RAX, 2)
	a.Jmp(out)
	a.Mark(null)
	a.MovImm(x64.RAX, 0x5A5A5A5A)
	a.Jmp(out)

	// 整数家族与句柄：分散三步
	a.Mark(num)
	a.Mov(x64.RAX, x64.RCX)
	a.Mov(x64.RSI, x64.RAX)
	a.ShrImm(x64.RSI, 33)
	a.Xor(x64.RAX, x64.RSI)
	a.MovImm(x64.R8, mix1)
	a.Imul(x64.RAX, x64.R8)
	a.Mov(x64.RSI, x64.RAX)
	a.ShrImm(x64.RSI, 33)
	a.Xor(x64.RAX, x64.RSI)
	a.MovImm(x64.R8, mix2)
	a.Imul(x64.RAX, x64.R8)
	a.Mov(x64.RSI, x64.RAX)
	a.ShrImm(x64.RSI, 33)
	a.Xor(x64.RAX, x64.RSI)

	a.Mark(out)
	b.epilogue()
}

// @key_eq(rcx=键甲, rdx=键乙, r8=模式)：返回 rax=1 或 0。
// 文本按内容比（文本判等看码点序列），其余比那 64 位本身——句柄就是身份。
//
// en: @key_eq (rcx = key a, rdx = key b, r8 = mode): returns rax = 1 or 0. Text compares by
// content (text equality looks at the code point sequence), everything else compares the 64
// bits themselves - a handle is the identity.
func keyEq(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.put(2, x64.R8)

	num := b.label()
	a.Test(x64.R8, x64.R8)
	a.Jcc(x64.E, num) // 同上：0 比数，1 比文本内容
	b.get(x64.RCX, 0)
	b.get(x64.RDX, 1)
	b.call("@str_eq")
	b.epilogue()

	a.Mark(num)
	b.get(x64.RAX, 0)
	b.get(x64.RDX, 1)
	a.Cmp(x64.RAX, x64.RDX)
	a.Setcc(x64.E, x64.RAX)
	a.MovZX8Reg(x64.RAX)
	b.epilogue()
}

// @map_find(rcx=表, rdx=键)：返回 rax=桶地址、rcx=1 命中／0 未命中。
// 未命中时 rax 落在可插入的那个位置：探测路径上第一个空格；路径上只有墓碑就取第一个
// 墓碑；一个都没有（整轮全是存活键）才退回起点。
//
// 一轮最多检查 cap 个位置。墓碑较多时表里可能一个空格都没有，不设上限就是无法结束的循环；
// 装过四分之三就先扩容，正常路径不会走到那种局面。
//
// en: @map_find (rcx = table, rdx = key): returns rax = the bucket address, rcx = 1 on a hit /
// 0 on a miss. On a miss rax lands on the position where the key can be inserted: the first
// empty slot on the probe path; if the path holds only tombstones, the first tombstone; only
// when there is none at all (the whole loop holds live keys) does it fall back to the start. One
// pass checks at most cap slots: with many tombstones the table can hold no empty slot at
// all, and without a bound the loop never ends; load factor above three quarters grows
// first, so the normal path never reaches that state.
func mapFind(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX) // 表
	b.put(1, x64.RDX) // 要找的键

	// 起始桶 = 散列值 mod cap
	b.get(x64.RDI, 0)
	b.get(x64.RCX, 1)
	a.MovLoad(x64.RDX, x64.RDI, cMode)
	b.call("@hash")
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R8, x64.RDI, cCap)
	a.AddImm(x64.R8, -1)
	a.And(x64.RAX, x64.R8)
	a.MovImm(x64.R8, stride)
	a.Imul(x64.RAX, x64.R8)
	a.MovLoad(x64.RSI, x64.RDI, cData) // 桶数组开头
	b.put(7, x64.RSI)
	a.Add(x64.RSI, x64.RAX)
	b.put(2, x64.RSI) // 游标
	b.put(3, x64.RSI) // 起始格：整轮都没有可让出的位置时的兜底
	// 终点 = 开头 + cap*stride
	a.MovLoad(x64.R9, x64.RDI, cCap)
	a.MovImm(x64.R8, stride)
	a.Imul(x64.R9, x64.R8)
	b.get(x64.RSI, 7)
	a.Add(x64.RSI, x64.R9)
	b.put(4, x64.RSI)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R11, x64.RDI, cCap)
	b.put(5, x64.R11) // 还能检查几个位置
	a.MovImm(x64.RAX, 0)
	b.put(6, x64.RAX) // 路径上第一个可让出的位置，0 表示还没有

	top := b.label()
	keep := b.label()
	next := b.label()
	nowrap := b.label()
	dead := b.label()
	none := b.label()
	out := b.label()
	hit := b.label()

	a.Mark(top)
	b.get(x64.RSI, 2)
	a.MovLoad(x64.RAX, x64.RSI, bState)

	// 空格与墓碑都算可让出的位置，只记录遇到的第一个
	b.get(x64.RDX, 6)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, keep)
	a.CmpImm(x64.RAX, stateUsed)
	a.Jcc(x64.E, keep)
	b.put(6, x64.RSI)
	a.Mark(keep)

	a.CmpImm(x64.RAX, stateEmpty)
	a.Jcc(x64.E, dead) // 遇到空格：探测链到此结束，键确定不在表里
	a.CmpImm(x64.RAX, stateUsed)
	a.Jcc(x64.NE, next) // 墓碑：跳过这个位置继续找
	b.get(x64.RCX, 2)
	a.MovLoad(x64.RCX, x64.RCX, bKey)
	b.get(x64.RDX, 1)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R8, x64.RDI, cMode)
	b.call("@key_eq")
	a.Test(x64.RAX, x64.RAX)
	a.Jcc(x64.NE, hit)

	a.Mark(next)
	b.get(x64.RSI, 2)
	a.AddImm(x64.RSI, stride)
	b.get(x64.RDX, 4)
	a.Cmp(x64.RSI, x64.RDX)
	a.Jcc(x64.B, nowrap)
	b.get(x64.RSI, 7) // 到达终点：折回桶数组开头
	a.Mark(nowrap)
	b.put(2, x64.RSI)
	b.get(x64.R11, 5)
	a.AddImm(x64.R11, -1)
	b.put(5, x64.R11)
	a.Jcc(x64.NE, top)

	// 一整轮检查完都没有命中
	b.get(x64.RDX, 6)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.NE, out)
	b.get(x64.RDX, 3)
	a.Jmp(out)

	a.Mark(dead)
	b.get(x64.RDX, 6)
	a.Test(x64.RDX, x64.RDX)
	a.Jcc(x64.E, none)
	b.get(x64.RDX, 2) // 记过墓碑就填墓碑，不然填这个空格
	a.Jmp(out)
	a.Mark(none)
	b.get(x64.RDX, 2)

	a.Mark(out)
	a.Mov(x64.RAX, x64.RDX)
	a.Xor(x64.RCX, x64.RCX)
	b.epilogue()

	a.Mark(hit)
	b.get(x64.RAX, 2)
	a.MovImm(x64.RCX, 1)
	b.epilogue()
}

// @map_new(rcx=元素个数, rdx=类型字, r9=模式)：返回 rax=新的 dict／hashset 句柄。
//
// 桶数取"能装下两倍元素数"的那个 2 的幂，最少 8 个桶：留一倍余量，探测链一开始就不会太长。
// 元素本身由调用方逐个位置用 @map_put 放入，所以返回时表是空的。
//
// en: @map_new (rcx = element count, rdx = type word, r9 = mode): returns rax = a new
// dict/hashset handle. The bucket count is the power of two that "holds twice the elements", at
// least 8 slots: doubling leaves headroom so probe chains do not start out long. The elements
// themselves are put in by the caller one position at a time with @map_put, so the table is
// empty when this returns.
func mapNew(b *rt) {
	a := b.a
	b.prologue()
	// 三个参数按 Microsoft x64 落在 rcx／rdx／r8：第三个不是 r9
	b.put(0, x64.RCX) // 个数
	b.put(1, x64.RDX) // 类型字
	b.put(2, x64.R8)  // 模式

	a.MovImm(x64.RAX, minBuckets)
	b.put(3, x64.RAX) // cap
	b.get(x64.RAX, 0)
	a.Add(x64.RAX, x64.RAX) // 要装下两倍
	b.put(4, x64.RAX)
	grow := b.label()
	big := b.label()
	a.Mark(grow)
	b.get(x64.RAX, 3)
	b.get(x64.RDX, 4)
	a.Cmp(x64.RAX, x64.RDX)
	a.Jcc(x64.AE, big)
	b.get(x64.RAX, 3)
	a.Add(x64.RAX, x64.RAX)
	b.put(3, x64.RAX)
	a.Jmp(grow)
	a.Mark(big)

	// 桶数组
	b.get(x64.RAX, 3)
	a.MovImm(x64.R8, stride)
	a.Imul(x64.RAX, x64.R8)
	a.Mov(x64.RCX, x64.RAX)
	a.MovImm(x64.RDX, 0) // 一段裸缓冲，不是任何对象
	b.call("@alloc")
	b.put(5, x64.RAX)

	// 容器对象
	a.MovImm(x64.RCX, cObjSz)
	b.get(x64.RDX, 1)
	b.call("@alloc")
	b.put(6, x64.RAX)
	b.get(x64.RDI, 6)
	a.MovImm(x64.RAX, 0)
	a.MovStore(x64.RDI, cLen, x64.RAX)
	b.get(x64.RAX, 3)
	a.MovStore(x64.RDI, cCap, x64.RAX)
	b.get(x64.RAX, 5)
	a.MovStore(x64.RDI, cData, x64.RAX)
	a.MovImm(x64.RAX, 0)
	a.MovStore(x64.RDI, cVer, x64.RAX)
	b.get(x64.RAX, 2)
	a.MovStore(x64.RDI, cMode, x64.RAX)
	a.Mov(x64.RAX, x64.RDI)
	b.epilogue()
}

// @map_grow(rcx=表)：桶数翻倍，把活着的键值整批重摆一次。
//
// 版本号不变——重摆不是语言层面的增删（增删由版本号那条规则管），
// 正在遍历的那一轮持有的是旧桶数组的地址，所以重摆只能由增删触发，而增删已经先抛过 10033 号。
//
// en: @map_grow (rcx = table): doubles the bucket count and re-places every live key and value
// in bulk. The version counter does not move - re-placing is not a language-level add or
// remove (add/remove is what the version counter guards), and a loop in progress holds the
// old bucket array's address, so growth may only be triggered by an add or remove, and those
// already threw 10033 first.
func mapGrow(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)

	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cData)
	b.put(1, x64.RAX) // 老数组（下面当游标用）
	b.put(4, x64.RAX) // 老数组的起点：搬完要释放它
	a.MovLoad(x64.RAX, x64.RDI, cCap)
	b.put(2, x64.RAX) // 老桶数
	a.Add(x64.RAX, x64.RAX)
	b.put(3, x64.RAX) // 新桶数

	b.get(x64.RAX, 3)
	a.MovImm(x64.R8, stride)
	a.Imul(x64.RAX, x64.R8)
	a.Mov(x64.RCX, x64.RAX)
	a.MovImm(x64.RDX, 0)
	b.call("@alloc")
	b.get(x64.RDI, 0)
	b.get(x64.R8, 3)
	a.MovStore(x64.RDI, cCap, x64.R8) // 表已指向新数组，下面按新表放置
	a.MovStore(x64.RDI, cData, x64.RAX)
	a.MovImm(x64.R8, 0)
	a.MovStore(x64.RDI, cLen, x64.R8)

	walk := b.label()
	moved := b.label()
	skip := b.label()

	// 逐桶搬：旧数组走一遍，存活的键值插入新表
	a.Mark(walk)
	b.get(x64.R9, 2)
	a.Test(x64.R9, x64.R9)
	a.Jcc(x64.E, moved)
	b.get(x64.RSI, 1)
	a.MovLoad(x64.RAX, x64.RSI, bState)
	a.CmpImm(x64.RAX, stateUsed)
	a.Jcc(x64.NE, skip)
	// 在新表里找一个位置放入（新表容量翻倍，一定能找到空格）
	b.get(x64.RCX, 0)
	b.get(x64.RSI, 1)
	a.MovLoad(x64.RDX, x64.RSI, bKey)
	b.call("@map_find") // rax = 桶
	a.MovImm(x64.R8, stateUsed)
	a.MovStore(x64.RAX, bState, x64.R8)
	b.get(x64.RSI, 1)
	a.MovLoad(x64.R8, x64.RSI, bKey)
	a.MovStore(x64.RAX, bKey, x64.R8)
	a.MovLoad(x64.R8, x64.RSI, bVal)
	a.MovStore(x64.RAX, bVal, x64.R8)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R8, x64.RDI, cLen)
	a.AddImm(x64.R8, 1)
	a.MovStore(x64.RDI, cLen, x64.R8)
	a.Mark(skip)
	b.get(x64.RSI, 1)
	a.AddImm(x64.RSI, stride)
	b.put(1, x64.RSI)
	b.get(x64.R9, 2)
	a.AddImm(x64.R9, -1)
	b.put(2, x64.R9)
	a.Jmp(walk)

	a.Mark(moved)
	// 旧表此时已无任何引用：正在遍历的那一轮会在下一次检查版本号时抛 10033 号，
	// 而 @map_next 手上拿的是这一次的游标，不跨再散列。释放它。
	b.get(x64.RCX, 4)
	b.call("@free")
	b.epilogue()
}

// @map_put(rcx=表, rdx=键, r8=值)：有这一个键就改值，没有就添一条。
//
// 修改值不计入"改过几次"——键没变、桶数没变，遍历仍然安全（需要拦的是增删）。
// 插入一条之前先看装载因子，超过四分之三就翻倍重摆，否则探测链会越拖越长。
//
// en: @map_put (rcx = table, rdx = key, r8 = value): replace the value if this key exists,
// otherwise append an entry. Replacing does not count as a mutation - the key did not move and
// the slot count did not, so iteration stays safe (what must be guarded is adding and removing).
// Before appending, the load factor is checked and above three quarters the table doubles and
// re-places, otherwise probe chains stretch.
func mapPut(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.put(2, x64.R8)

	b.get(x64.RDI, 0)
	a.MovLoad(x64.RAX, x64.RDI, cLen)
	a.AddImm(x64.RAX, 1)
	a.MovImm(x64.R8, growDen)
	a.Imul(x64.RAX, x64.R8)
	a.MovLoad(x64.R9, x64.RDI, cCap)
	a.MovImm(x64.R10, growNum)
	a.Imul(x64.R9, x64.R10)
	a.Cmp(x64.RAX, x64.R9)
	full := b.label()
	same := b.label()
	place := b.label()
	a.Jcc(x64.BE, full)
	b.get(x64.RCX, 0)
	b.call("@map_grow")

	a.Mark(full)
	b.get(x64.RCX, 0)
	b.get(x64.RDX, 1)
	b.call("@map_find") // rax=桶，rcx=1 命中
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NE, same)
	// 插一条
	a.MovImm(x64.R9, stateUsed)
	a.MovStore(x64.RAX, bState, x64.R9)
	b.get(x64.R9, 1)
	a.MovStore(x64.RAX, bKey, x64.R9)
	b.get(x64.R9, 2)
	a.MovStore(x64.RAX, bVal, x64.R9)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R9, x64.RDI, cLen)
	a.AddImm(x64.R9, 1)
	a.MovStore(x64.RDI, cLen, x64.R9)
	a.MovLoad(x64.R9, x64.RDI, cVer)
	a.AddImm(x64.R9, 1)
	a.MovStore(x64.RDI, cVer, x64.R9)
	a.Jmp(place)

	a.Mark(same)
	b.get(x64.R9, 2)
	a.MovStore(x64.RAX, bVal, x64.R9)
	a.Mark(place)
	b.epilogue()
}

// @map_at(rcx=表, rdx=键)：返回 rax=值那个位置的地址。没有这个键就抛 10032 号。
//
// 返回地址而不是值，与 @list_at 同一个道理：读、改、读走三件事共用同一道查找。
//
// en: @map_at (rcx = table, rdx = key): returns rax = the address of the value slot; a missing
// key throws code 10032. An address rather than a value, for the same reason as
// @list_at: reading, modifying and reading out share one lookup.
func mapAt(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.get(x64.RCX, 0)
	b.get(x64.RDX, 1)
	b.call("@map_find")
	has := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.NE, has)
	b.throwWith(codeNoKey, "sNoKey")
	a.Mark(has)
	a.AddImm(x64.RAX, bVal)
	b.epilogue()
}

// @map_contains(rcx=表, rdx=键)：返回 rax=1 或 0。有键才算在，值是不是零不看。
//
// en: @map_contains (rcx = table, rdx = key): returns rax = 1 or 0. Presence means the key is
// there; whether the value is zero is not looked at.
func mapContains(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.get(x64.RCX, 0)
	b.get(x64.RDX, 1)
	b.call("@map_find")
	a.Test(x64.RCX, x64.RCX)
	a.Setcc(x64.NE, x64.RAX)
	a.MovZX8Reg(x64.RAX)
	b.epilogue()
}

// @map_remove(rcx=表, rdx=键)：命中就把状态写成墓碑、数目减一、次数加一；未命中就原样返回。
//
// 不真正清除是为了探测链：把后面的连续段前移会断开其他键的链，留一个墓碑最省事。
// 代价是墓碑多了表会偏大——将来实现释放时可以顺带做一次压缩。
//
// 返回的是被删那一条的值（未删成、以及没有值的集合类型，一律返回 0）：删成之后那一条已不在表里，
// 语言里没有名字指向它的值，而运行期手上没有形状表，无法析构，所以交给降级侧按值的形状析构。
// 返回之前先把值那个位置写成 0——留着就成了墓碑与降级侧的临时槽共同持有同一块。
//
// en: @map_remove (rcx = table, rdx = key): on a hit write a tombstone in the state, decrement
// the count, increment the version; on a miss return as is. Not really clearing protects the
// probe chain - shifting the run behind it forward would cut other keys' chains, so a tombstone
// is the cheapest option. The price is that many tombstones make the table larger than it
// really is; when the freeing work lands, a compaction can ride along. It returns the value
// of the removed entry (0 when nothing was removed, and for the container that has no values at
// all): after a successful removal that entry is out of the table, the language has no name
// pointing at its value, and the runtime has no shape table to destruct it with, so lowering is
// handed the value and destructs it by shape. The value slot is zeroed before returning -
// leaving it would mean the tombstone and lowering's temporary slot both hold the same block.
func mapRemove(b *rt) {
	const kValue = 4 // 被删那一条的值，保留到末尾返回
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)
	b.get(x64.RCX, 0)
	b.get(x64.RDX, 1)
	b.call("@map_find")
	gone := b.label()
	a.Test(x64.RCX, x64.RCX)
	a.Jcc(x64.E, gone)
	// 文本键：删除之后就再也没有和它比较的地方，这一块当场释放（"位置里的值归那个位置"）。
	// 只释放文本——其余种类比的是那 64 位，句柄型的那一块要靠形状表析构，而形状表在运行期这头没有。
	b.put(2, x64.RAX) // 那个位置
	a.MovLoad(x64.RDX, x64.RAX, bKey)
	b.put(3, x64.RDX) // 键的句柄
	a.MovLoad(x64.RDX, x64.RAX, bVal)
	b.put(kValue, x64.RDX) // 值的句柄：末尾返回，值那个位置随后写成 0
	b.get(x64.RAX, 0)      // 表：@map_find 把 rdi 用掉了，得从槽里再取一次
	a.MovLoad(x64.R9, x64.RAX, cMode)
	a.Test(x64.R9, x64.R9)
	noFree := b.label()
	a.Jcc(x64.E, noFree)
	b.get(x64.RCX, 3)
	b.call("@free")
	a.Mark(noFree)
	b.get(x64.RSI, 2)
	a.MovImm(x64.R9, stateTomb)
	a.MovStore(x64.RSI, bState, x64.R9) // 该位置（此时 rax 里是表地址，不是桶地址）
	a.Xor(x64.R9, x64.R9)
	a.MovStore(x64.RSI, bVal, x64.R9)
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R9, x64.RDI, cLen)
	a.AddImm(x64.R9, -1)
	a.MovStore(x64.RDI, cLen, x64.R9)
	a.MovLoad(x64.R9, x64.RDI, cVer)
	a.AddImm(x64.R9, 1)
	a.MovStore(x64.RDI, cVer, x64.R9)
	b.get(x64.RAX, kValue)
	b.epilogue()
	a.Mark(gone)
	a.Xor(x64.RAX, x64.RAX)
	b.epilogue()
}

// @map_next(rcx=表, rdx=起始桶)：返回 rax=下一个有内容的桶，遍历结束返回 0。
//
// 遍历要的是"下一个有内容的桶"，中间的空格与墓碑一律跳过；顺序不作保证。
// 终点由表自己算，不靠调用方记住，以免重摆之后仍拿着旧终点越界。
//
// en: @map_next (rcx = table, rdx = starting bucket): returns rax = the next bucket that holds
// something, 0 when the walk is done. Iteration wants "the next non-empty bucket", skipping
// empty and tombstone slots; order is not guaranteed. The end is computed from the table itself
// rather than remembered by the caller, so a stale end cannot run past a re-placed table.
func mapNext(b *rt) {
	a := b.a
	b.prologue()
	b.put(0, x64.RCX)
	b.put(1, x64.RDX)

	// 终点 = 开头 + 桶数*每桶字节数，由表自己算：重摆之后旧终点就作废了
	b.get(x64.RDI, 0)
	a.MovLoad(x64.R9, x64.RDI, cData)
	a.MovLoad(x64.R10, x64.RDI, cCap)
	a.MovImm(x64.R8, stride)
	a.Imul(x64.R10, x64.R8)
	a.Add(x64.R10, x64.R9)
	b.get(x64.RSI, 1)

	top := b.label()
	got := b.label()
	none := b.label()
	done := b.label()
	a.Mark(top)
	a.Cmp(x64.RSI, x64.R10)
	a.Jcc(x64.AE, none)
	a.MovLoad(x64.RAX, x64.RSI, bState)
	a.CmpImm(x64.RAX, stateUsed)
	a.Jcc(x64.E, got) // 命中存活的位置：返回它
	a.AddImm(x64.RSI, stride)
	a.Jmp(top)

	a.Mark(got)
	a.Mov(x64.RAX, x64.RSI)
	a.Jmp(done)
	a.Mark(none)
	a.Xor(x64.RAX, x64.RAX)
	a.Mark(done)
	b.epilogue()
}

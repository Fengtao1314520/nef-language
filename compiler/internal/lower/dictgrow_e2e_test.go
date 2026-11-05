// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// dictgrow_e2e_test.go 覆盖一类已修复的崩溃，形状是"堆上新计算的文本键＋dict 扩容"。
//
// 根因不在 dict：@alloc 从空闲链表取块时只看档位不看块自身的尺寸，而档位按 2 的幂上界划分——
// 于是把数据 128 字节的那一块交给了要 192 字节的这一笔（桶数组正是 8 槽 × 24），尾巴 64 字节
// 写进了隔壁块。再散列搬桶时读到那一块里 state=1、键为 106 的位置，@hash 把 106 当指针解引用 → 段错误。
// 静态字面量当键走不到这条路：那种块的头是全 0，@free 与 @alloc 都不碰它。

package lower_test

import (
	"os"
	"testing"

	"neflang.org/nf/internal/exetest"
)

// TestDictGrowHeapTextKeys 就是那份最小复现：20 个互不相同、一次都不重复的键，
// 每个都当场计算（先前"20 个键正常"的对照用静态字面量跑，不算有效证据）。
func TestDictGrowHeapTextKeys(t *testing.T) {
	got, code := run(t, `
let words:list<string> = "a b c d e f g h i j k l m n o p q r s t".split(" ")
let by:dict<string, int> = {}
for (w:string in words) {
    let k:string = w.slice(0, length(w))
    by.add(k, 1)
}
console.print(length(by))
`)
	if got != "20\n" || code != 0 {
		t.Fatalf("长出第二块桶数组得到 %q 码 %d，要 20 码 0", got, code)
	}
}

// TestDictGrowCounts 覆盖命中重复的那一支：同一个键命中之后只改值，键那个位置不换。
// total 的含义是"每个词按它出现的次数再数一遍"，即各键次数的平方和（3²×6 + 2²）。
func TestDictGrowCounts(t *testing.T) {
	got, code := run(t, `
let words:list<string> = "a b c d e f g a b c d e f g a b c d e f".split(" ")
let by:dict<string, int> = {}
for (w:string in words) {
    let k:string = w.slice(0, length(w))
    if (by.contains(k)) {
        by[k] = by[k] + 1
    } else {
        by.add(k, 1)
    }
}
let total:int = 0
for (w:string in words) {
    total = total + by[w]
}
console.print(length(by), total)
`)
	if got != "7 58\n" || code != 0 {
		t.Fatalf("数词频得到 %q 码 %d，要 7 58 码 0", got, code)
	}
}

// TestDictGrowManyTimes 一路增长到 256 个槽位：中间经过六次再散列，每一个槽都要能查回来。
func TestDictGrowManyTimes(t *testing.T) {
	got, code := run(t, `
let by:dict<string, int> = {}
let i:int = 0
while (i < 200) {
    let k:string = "k" + number.to_string(i)
    by.add(k, i)
    i = i + 1
}
let sum:int = 0
let j:int = 0
while (j < 200) {
    sum = sum + by["k" + number.to_string(j)]
    j = j + 1
}
console.print(length(by), sum, by.contains("k199"), by.contains("k200"))
`)
	if got != "200 19900 true false\n" || code != 0 {
		t.Fatalf("长到 256 格之后对不上：得到 %q 码 %d", got, code)
	}
}

// TestHashsetGrowHeapText 覆盖同一条路径在 hashset 上的形状（三种容器共用同一张表）。
func TestHashsetGrowHeapText(t *testing.T) {
	got, code := run(t, `
let words:list<string> = "红 橙 黄 绿 青 蓝 紫 红 橙 黄 绿 青 蓝 紫 白 黑".split(" ")
let seen:hashset<string> = {}
for (w:string in words) {
    let k:string = w.slice(0, length(w))
    seen.add(k)
}
console.print(length(seen), seen.contains("紫"), seen.contains("灰"))
`)
	if got != "9 true false\n" || code != 0 {
		t.Fatalf("hashset 搬家后对不上：得到 %q 码 %d", got, code)
	}
}

// TestDictIndexStoreKeyOwnership 钉住 `d[键] = 值` 写新键那一条：键是当场算出来的文本时，
// 交出去的是那一个位置，名字当场作废。先前只记值那一笔，名字仍然登记着持有者，
// 作用域尽头把桶里正在用的那块文本拆掉——同一个键写两轮就多出一条记录，按内容查永远查不中。
// en: TestDictIndexStoreKeyOwnership pins `d[key] = value` when the key is text computed on the
// spot: the position takes it and the name is voided there and then. Only the value side used to
// be recorded, so the name stayed a registered holder and its scope-end teardown dismantled the
// block a bucket was using - two rounds over the same keys grew an extra record each time and a
// lookup by content could never find it.
func TestDictIndexStoreKeyOwnership(t *testing.T) {
	got, code := run(t, `
let by:dict<string, int> = {}
for (i:int in 0 until 5) {
    let k:string = "key" + number.to_string(i)
    by[k] = 1
}
for (i:int in 0 until 5) {
    let k:string = "key" + number.to_string(i)
    by[k] = 2
}
let names:list<string> = by.keys()
names.sort()
for (n:string in names) {
    console.print(n, by[n])
}
console.print(length(by), by.contains("key0"), by.get("key4", -1))
`)
	if got != "key0 2\nkey1 2\nkey2 2\nkey3 2\nkey4 2\n5 true 2\n" || code != 0 {
		t.Fatalf("索引赋值写新键之后键坏了：得到 %q 码 %d", got, code)
	}
}

// TestSampleWordFreq 把整份 wordfreq.nf 样例编成真实产物运行一遍，期望输出按实际运行结果写死。
func TestSampleWordFreq(t *testing.T) {
	const path = "../../../samples/wordfreq.nf"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	r := exetest.Run(t, buildAt(t, path, string(raw)), "wordfreq.exe")
	want := "词数 14 不同词 10 最多的一共 4\n" +
		"冠军 the 4\nthe 4\nquick 1\nbrown 1\nfox 2\njumps 1\nover 1\nlazy 1\ndog 1\nand 1\nhound 1\n"
	if r.Stdout != want || r.Code != 0 {
		t.Fatalf("wordfreq.nf跑出来是 %q 码 %d，要 %q 码 0", r.Stdout, r.Code, want)
	}
}

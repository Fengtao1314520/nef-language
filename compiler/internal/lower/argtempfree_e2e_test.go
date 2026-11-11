// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strconv"
	"strings"
	"testing"
)

// 实参位置与接收者位置上"当场造出来的那一块"归谁。
//
// 这一族写法的共同形状：一条标准库动作的参数（或接收者）本身又是一条标准库动作，
// 里面那一条返回的是这一刻才要出来的一块，外面那一条只读它一次、不收留它。
// 语言里没有名字接住这一块，于是每一圈遗留一块。
//
// 释放判据与 `noteBorrowed` 那一段同一条：只在**只读借用那一条动作自己的那一层窗口**里登记，
// 登记给哪一层就由那一层在动作走完时归还。语句那一层的窗口不收这一族——
// `xs.add(s.trim())` 交出去的那一块住进容器的槽位，持有者是那个位置，
// 在语句尽头归还它就是把容器还在用的那一块拆掉。
//
// en: Who owns the block created on the spot in an argument or a receiver position. Every shape here
// is one standard-library action whose argument (or receiver) is another standard-library action: the
// inner one returns a block asked for at this moment, the outer one reads it once and keeps nothing,
// and no name holds it - so each round leaves one block behind. The release rule is the one
// noteBorrowed already states: register only inside the window of the read-only-borrow action itself,
// and that layer returns the block when the action is done. The sentence-level window does not take
// this family - the block handed to `xs.add(s.trim())` lives in a container position whose holder is
// that position, so returning it at the end of the sentence would dismantle a block the container is
// still using.
func TestHeapSlopeArgTempFree(t *testing.T) {
	cases := []struct {
		name   string
		reused bool
		tmpl   string
	}{
		{"文本动作的实参位置现造一段文本", true, `
let s:string = "  甲乙丙  "
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(s.trim())
}
console.print(total)
`},
		{"接收者位置现造的一段字节（链式两步）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length("甲乙".to_bytes().to_base64())
}
console.print(total)
`},
		{"拼接动作的实参位置现造一只清单", true, `
let parts:list<string> = ["甲", "乙", "丙"]
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(text.join(parts, ","))
}
console.print(total)
`},
		{"字节清单动作现造的那一块", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(bytes.join(["甲".to_bytes(), "乙".to_bytes()]))
}
console.print(total)
`},
		{"解析出的树交进计数那一步", true, `
let doc:string = "[1, 2, 3]"
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + json.count(json.parse(doc))
}
console.print(total)
`},
		{"自己写的函数造出来的一块交进只读借用那一步", true, `
method build(n:int):list<int> {
    return [n, n]
}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(build(i))
}
console.print(total)
`},
		// 下面七条是那七条"从接收者搬位置"的动作：搬过来的只有值类型那种（不欠析构），
		// 文本那一种运行期各拷一份，所以新表还回 heap 不会碰到接收者的东西。
		// 每一圈的量与接收者无关，都在这里各钉一条，改坏了当场红。
		{"切片切出来的一小段交进只读借用", true, `
let xs:list<string> = ["甲乙", "丙丁", "戊己"]
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(xs.slice(0, 2))
}
console.print(total)
`},
		{"转成集合那一步的新表", true, `
let xs:list<string> = ["甲乙", "丙丁"]
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(xs.to_set())
}
console.print(total)
`},
		{"问出的键那一串", true, `
let d:dict<string, int> = {"甲": 1, "乙": 2}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(d.keys())
}
console.print(total)
`},
		{"问出的值那一串", true, `
let d:dict<string, string> = {"甲": "甲乙", "乙": "丙丁"}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(d.values())
}
console.print(total)
`},
		{"并集那一只新表", true, `
let a:hashset<string> = {"甲", "乙"}
let b:hashset<string> = {"丙"}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(a.union(b))
}
console.print(total)
`},
		{"交集那一只新表", true, `
let a:hashset<string> = {"甲", "乙"}
let b:hashset<string> = {"乙", "丙"}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(a.intersect(b))
}
console.print(total)
`},
		{"差集那一只新表", true, `
let a:hashset<string> = {"甲", "乙"}
let b:hashset<string> = {"乙", "丙"}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length(a.difference(b))
}
console.print(total)
`},
		// 下面三条是对照：那几块的持有者都不是这一句，改了释放判据也不许跟着变。
		// 名字接住的那一份归名字（声明那一处登记）；读容器槽位得到的是那一个位置的一份只读借用；
		// 拼出来的那一段是最外层的值，本来就有语句尽头那一条管着。
		{"名字接住现造的那一段（对照：该由名字还）", true, `
let s:string = "  甲乙丙  "
let total:int = 0
for (i:int in 0 until @N@) {
    let t:string = s.trim()
    total = total + length(t)
}
console.print(total)
`},
		{"读容器槽位那一份交进实参（对照：本来就不交出）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<string> = ["甲乙".slice(0, 2)]
    total = total + length(xs[0])
}
console.print(total)
`},
		{"拼出来的一段是最外层的值（对照：早就是 0）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + length("甲" + "乙")
}
console.print(total)
`},
	}
	for _, one := range cases {
		per := heapSlope(t, one.tmpl)
		t.Logf("%-30s 每圈 %8.1f 字节", one.name, per)
		if one.reused && per >= 2 {
			t.Errorf("%s：该还掉的每圈漏了 %.1f 字节", one.name, per)
		}
		if !one.reused && per <= 2 {
			t.Errorf("%s：这一类本来是故意不还的，每圈只漏 %.1f 字节？", one.name, per)
		}
	}
}

// TestArgTempFreeKeepsContainerValues 把"不许多还"钉成结果而不是字节数：每轮往清单里交两段
// 当场造出来的文本，交完再逐格读回来。若释放判据错把住进槽位的那一块拆掉，这里读回来的就是
// 空句柄或者乱码；字节数那几条反而看不出来。
//
// en: TestArgTempFreeKeepsContainerValues pins "never return one block too many" as an outcome rather
// than a byte count: each round hands two freshly built texts into a list and every position is read
// back afterwards. If the release rule dismantled the block living in the position, the read-back
// shows an empty handle or garbage, which the byte-count rows would not reveal.
func TestArgTempFreeKeepsContainerValues(t *testing.T) {
	out, code := run(t, `
let xs:list<string> = []
for (i:int in 0 until 300) {
    let seed:string = "abcdefghij"
    xs.add(seed.slice(0, 3))
    xs.add(seed.to_bytes().to_base64())
}
let total:int = 0
for (i:int in 0 until length(xs)) {
    total = total + length(xs[i])
}
console.print(total, length(xs))
`)
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, out)
	}
	// 每轮两段：3 个码点 ＋ 10 字节编成 base64 的 16 个码点，300 轮合计 5700
	if strings.TrimSpace(out) != "5700 600" {
		t.Fatalf("读回来的总长与格数 %q，要 \"5700 600\"", out)
	}
}

// TestArgTempFreeAcrossRounds 是同一件事在字典那一侧：每轮读回上一轮存进去的那一份，
// 内容必须还是当初交进去的那一段。圈数足够多，错误释放会在中途露出来。
//
// en: TestArgTempFreeAcrossRounds is the same guarantee on the dictionary side: each round reads back
// what the previous round stored, and it must still be the text that was handed in. Enough rounds
// make a wrong release show up partway through.
func TestArgTempFreeAcrossRounds(t *testing.T) {
	out, code := run(t, `
let d:dict<string, string> = {}
let wrong:int = 0
for (i:int in 0 until 2000) {
    let key:string = ("k" + number.to_string(i)).trim()
    d.add(key, "v" + number.to_string(i))
    if (i > 0) {
        let prev:string = ("k" + number.to_string(i - 1)).trim()
        if (length(d[prev]) != length(prev)) {
            wrong = wrong + 1
        }
    }
}
console.print(wrong, length(d))
`)
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, out)
	}
	if strings.TrimSpace(out) != "0 2000" {
		t.Fatalf("读回来的长度对不上：%q", out)
	}
}

// TestArgTempFreePrintsRight 把最常用的一条路钉住：打印的实参位置现造一块，印出来的内容与
// 圈数都要对。打印是只读借用里用得最多的一条，判据改错最先在这里露出来。
//
// en: TestArgTempFreePrintsRight pins the most travelled path: a block built on the spot in
// console.print's argument position. The printed text and the round count must both be right -
// printing is the most used read-only borrow, so a wrong rule shows there first.
func TestArgTempFreePrintsRight(t *testing.T) {
	out, code := run(t, `
let total:int = 0
for (i:int in 0 until 50) {
    console.print("甲乙丙丁".trim().slice(0, 2))
    total = total + 1
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, out)
	}
	want := strings.Repeat("甲乙\n", 50) + "50\n"
	if out != want {
		t.Fatalf("印出来的不对：\n得到 %q\n要   %q", out, want)
	}
}

// 最后一条把现造的那一块交给两个会收留它的地方（容器的槽位与对象的一个字段），交完再从那个
// 位置读回来用。数对说明归还那一刀没有落在收留方的块上。
//
// en: The last row hands a freshly built block to the two places that keep it - a container position
// and an object field - then reads it back and uses it. A right count means the release never landed
// on the keeping side's block.
func TestArgTempFreeIntoKeepingPlaces(t *testing.T) {
	const rounds = 400
	out, code := run(t, `
class Holder {
    s:string
}
let keep:list<string> = []
let held:list<Holder> = []
for (i:int in 0 until `+strconv.Itoa(rounds)+`) {
    let seed:string = "  甲乙丙  "
    keep.add(seed.trim())
    held.add(new Holder(s: seed.trim()))
}
let total:int = 0
for (i:int in 0 until length(keep)) {
    total = total + length(keep[i]) + length(held[i].s)
}
console.print(total)
`)
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, out)
	}
	// 每轮两份「甲乙丙」：清单里那一份归那个槽位，字段里那一份归那个字段，各 3 个码点
	if got := strings.TrimSpace(out); got != strconv.Itoa(rounds*6) {
		t.Fatalf("读回来的总长 %q，要 %d", got, rounds*6)
	}
}

// TestDerivedContainersKeepTheirSource 钉住那七条"从接收者搬位置"的动作：新表在只读借用那一步
// 尽头还给 heap 之后，接收者那些位置必须还是原样。搬过来的只有值类型那一种（本来就不欠析构），
// 文本那一种运行期各拷一份，所以两头指向的从来不是同一块。写坏任何一头，这里读回来的就不是那一段。
//
// en: TestDerivedContainersKeepTheirSource pins the seven actions that move positions out of the
// receiver: after the new table goes back to the heap at the end of a read-only borrow, every
// position on the receiving side must still be what it was. What can move is either a value type
// (which owes no destruction) or text, and the runtime copies text element by element, so the two
// sides never point at one block. Break either half and the read-back below stops matching.
func TestDerivedContainersKeepTheirSource(t *testing.T) {
	out, code := run(t, `
let xs:list<string> = ["甲乙", "丙丁", "戊己"]
let d:dict<string, string> = {"甲": "甲乙丙", "乙": "丁戊己"}
let s:hashset<string> = {"甲乙", "丙丁"}
let total:int = 0
for (i:int in 0 until 2000) {
    total = total + length(xs.slice(0, 2)) + length(xs.to_set())
    total = total + length(d.keys()) + length(d.values())
    total = total + length(s.union({"丁戊"})) + length(s.intersect({"甲乙"})) + length(s.difference({"甲乙"}))
}
let back:int = length(xs[0]) + length(xs[1]) + length(xs[2]) + length(d["甲"]) + length(d["乙"])
console.print(total, back, length(xs), length(d), length(s), xs[0], d["乙"])
`)
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, out)
	}
	// 每轮七条各返回 2、3、2、2、3、1、1 ⇒ 14；接收者那三样读回来一共 12 个码点
	want := "28000 12 3 2 2 甲乙 丁戊己"
	if got := strings.TrimSpace(out); got != want {
		t.Fatalf("读回来的不对：\n得到 %q\n要   %q", got, want)
	}
}

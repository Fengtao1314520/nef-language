// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 堆用量的自检（运行期例程 @heap_report，仅在环境变量 NEF_HEAP 设置时输出）。
//
// 这组测试覆盖的是释放策略的核心判断："少还是安全的漏，两个持有者指向同一块时
// 释放别人那一份就是错误"。这条判断只能推理、无法直接度量；而"实际释放了多少"
// 每次改动之后都要比对。于是用两段相同的负载各跑一遍：一段每圈在块尾释放，
// 一段把每圈造出的对象一路保留。前者应停留在第一块附近，后者会扩出多块新内存。

// heapLine 从输出末尾取出 "heap <累计要过的字节> <链上现有的字节>" 那一行，并把这一行从输出里移除。
// 取的是第一个数（累计）——每圈增长表要回答"每多一圈多要几字节"，第二个数用于"值不值"的取舍判断。
func heapLine(t *testing.T, out string) (string, int64) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^heap (\d+)(?: \d+)?\r?\n$`)
	m := re.FindStringSubmatchIndex(out)
	if m == nil {
		t.Fatalf("没看到堆用量那一行：%q", out)
	}
	n, err := strconv.ParseInt(out[m[2]:m[3]], 10, 64)
	if err != nil {
		t.Fatalf("堆用量那一行读不成数：%q", out[m[0]:])
	}
	return strings.TrimSuffix(strings.Replace(out, out[m[0]:m[1]], "", 1), "\n"), n
}

// runHeap 设置 NEF_HEAP 运行一段源码，返回程序自身的输出与向堆累计申请的总字节数。
func runHeap(t *testing.T, src string) (string, int64) {
	t.Helper()
	t.Setenv("NEF_HEAP", "1")
	out, code := run(t, src)
	if code != 0 {
		t.Fatalf("退出码 %d，要 0：\n%s", code, out)
	}
	body, used := heapLine(t, out)
	return body, used
}

// 两段负载只差在"造出的对象是否保留"：算法、圈数、每一步的分配个数完全相同。
const heapWork = `
class Row {
    n:int
    kids:list<int>
}

let total:int = 0
@DECLS@
for (i:int in 0 until 20000) {
    let acc:list<int> = [i]
    let d:dict<string, int> = {"甲": i}
    let r:Row = new Row(n: i, kids: [i, i])
    acc.add(d["甲"] + r.kids[0])
    total = total + acc[0]
@KEEPS@
}
console.print(total)
`

// heapSrc 把两段代码填进上面的模板：保留版本两个占位都填，释放版本两个都留空。
func heapSrc(decls, keeps string) string {
	s := strings.ReplaceAll(heapWork, "@DECLS@", decls)
	return strings.ReplaceAll(s, "@KEEPS@", keeps)
}

// TestHeapFreedIsReused 验证"块还回空闲链之后，下一圈能重新分配到它"：
// 每圈造一个 list、一个 dict、一个 class（连同它的 kids 那一段），在块尾按声明反序释放。
// 二十万次分配若无人释放会扩出好几 MB；正常释放则应停留在第一块之内。
func TestHeapFreedIsReused(t *testing.T) {
	out, used := runHeap(t, heapSrc("", ""))
	// 圈数 20000，每圈 total 加的是 i：0+1+…+19999
	if strings.TrimSpace(out) != "199990000" {
		t.Fatalf("数不对：%q", out)
	}
	if used > 1<<20 {
		t.Fatalf("每圈造完就还，堆用量不该超过最初那一块：%d 字节", used)
	}
}

// TestHeapKeptGrows 用同一段负载把对象全部保留：三个 keep 外层 list 是顶层名字，落在静态槽上，
// 作用域到整个程序跑完才结束——二十万圈里它们一直被持有，所以内存占用持续增长、
// 扩出多块新内存，到程序结束才一次释放（堆用量记录的是峰值，释放不会让它回落）。
// 两条一起看才是"每圈释放"与"一路持有"的对照，单看任何一条都不足以说明问题。
func TestHeapKeptGrows(t *testing.T) {
	decls := `let keepA:list<list<int>> = []
let keepD:list<dict<string, int>> = []
let keepR:list<Row> = []`
	keeps := "    keepA.add(acc)\n    keepD.add(d)\n    keepR.add(r)"
	out, used := runHeap(t, heapSrc(decls, keeps))
	if strings.TrimSpace(out) != "199990000" {
		t.Fatalf("数不对：%q", out)
	}
	if used < 4<<20 {
		t.Fatalf("一路留着不该只有 %d 字节——这一条是给上一条做对照的", used)
	}
}

// TestHeapQuietWithoutFlag 验证这条自检不改动正常输出：未设置环境变量时不额外打印任何内容。
// 网站上每段示例的输出都按实际运行核对，多出一行就会全部对不上。
func TestHeapQuietWithoutFlag(t *testing.T) {
	t.Setenv("NEF_HEAP", "")
	os.Unsetenv("NEF_HEAP")
	out, code := run(t, "console.print(1)\n")
	if code != 0 || out != "1\n" {
		t.Fatalf("没设 NEF_HEAP 还多印了东西：%q 码 %d", out, code)
	}
}

// 按种类度量：每一类各跑两种圈数，算出"每多一圈多要几个字节"。
//
// 度量每圈增量而不是绝对值的原因：`heapFrom` 落在哪儿、第一块多大都由本机决定，绝对值换台
// 机器就会漂移；而"每圈漏几个字节"只与程序本身有关——正常释放是 0，未释放是一整块的大小。
// 两个阈值（释放 < 40 字节／未释放 > 28 字节）之间留出的是同类差额的最小长度：一个空容器
// 连头带对象最小也有 40 字节，一段两个汉字的新文本 32 字节，所以两档之间不会混淆。
func heapAt(t *testing.T, src string) int64 {
	t.Helper()
	_, used := runHeap(t, src)
	return used
}

// heapSlope 返回每多一圈多要的字节数。圈数取 200 与 5000：两端都远离单圈的字节量级，
// 一两个块大小的噪声不会改变增量的正负。
func heapSlope(t *testing.T, tmpl string) float64 {
	t.Helper()
	const few, many = 200, 5000
	a := heapAt(t, strings.ReplaceAll(tmpl, "@N@", strconv.Itoa(few)))
	b := heapAt(t, strings.ReplaceAll(tmpl, "@N@", strconv.Itoa(many)))
	return float64(b-a) / float64(many-few)
}

func TestHeapSlopeByKind(t *testing.T) {
	cases := []struct {
		name   string
		reused bool
		tmpl   string
	}{
		{"list：块尾释放", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let acc:list<int> = [i]
    total = total + acc[0]
}
console.print(total)
`},
		{"class 连着它那条 list", true, `
class Row {
    n:int
    kids:list<int>
}
let total:int = 0
for (i:int in 0 until @N@) {
    let r:Row = new Row(n: i, kids: [i, i])
    total = total + r.kids[1]
}
console.print(total)
`},
		{"字典与集合（桶数组那一块）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, int> = {"甲": i}
    let s:hashset<int> = {i}
    total = total + d["甲"] + length(s)
}
console.print(total)
`},
		{"continue 走的那一圈", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let acc:list<int> = [i]
    if (i >= 0) {
        total = total + acc[0]
        continue
    }
    total = total + 1
}
console.print(total)
`},
		// 下面几类是设计上**故意不释放**的：登记条件不满足，宁可留下少量泄漏。
		// 每一笔都对应一条规则，任何把它们改小的实现改动都会在这里暴露。
		// 交进一个"不带走"的参数（只读一下长度）：出了被调方无人持有，调用方登记为持有者。
		{"交给不带走的函数", true, `
method use(xs:list<int>):int {
    return length(xs)
}
let total:int = 0
for (i:int in 0 until @N@) {
    let acc:list<int> = [i]
    total = total + use(acc)
}
console.print(total)
`},
		// 同一段负载只差一处：被调方把值 `return` 出去。释放判据是"这个参数除了被交回没有
		// 别的外流，而每一条带值的 return 交回的都是它"（`Facts.Back`）：递进去的那个名字
		// 交出之后就作废 ⇒ 接住结果的那个位置是唯一的持有者。
		// 识别不出这种写法的照旧不释放：`return h.peer`（从字段读出来的）那一行仍在下面单独覆盖。
		{"交给带走的函数（被调方把它 return 出去）", true, `
method passOn(xs:list<int>):list<int> {
    return xs
}
let total:int = 0
for (i:int in 0 until @N@) {
    let acc:list<int> = [i]
    let got:list<int> = passOn(acc)
    total = total + length(got)
}
console.print(total)
`},
		{"拼出来的文本（文本也按同一规则释放）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let s:string = "甲" + "乙"
    total = total + length(s)
}
console.print(total)
`},
		{"split 交回的 list 里那三段新文本（位置里的串归位置）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let parts:list<string> = "甲,乙,丙".split(",")
    total = total + length(parts)
}
console.print(total)
`},
		// 覆盖容器的那个位置：被换下来的旧值与没被收下的键都当场析构。
		// 三条各覆盖一种：dict 的 class 值、list 里的文本、add 命中已有键时交进来的那个键。
		// 每一圈都只覆盖、不再去读那个位置——读一次就是"读走"，读走的临时值无人认领是
		// 另一处独立的覆盖点（下面那条单独验证），混在这里就测不出覆盖这一步了。
		{"覆盖 dict 的那个位置（换下来的 class 值）", true, `
class Row {
    n:int
}
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, Row> = {}
    let k1:string = "甲".slice(0, 1)
    d.add(k1, new Row(n: i))
    let k2:string = "甲".slice(0, 1)
    d[k2] = new Row(n: i + 1)
    total = total + length(d)
}
console.print(total)
`},
		{"覆盖 list 的那个位置（换下来的文本）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<string> = ["甲"]
    xs[0] = "乙乙".slice(0, 2)
    total = total + length(xs)
}
console.print(total)
`},
		{"add 命中已有键（没收下的那个键）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, int> = {"甲": 1}
    let k:string = "甲".slice(0, 1)
    d.add(k, 2)
    total = total + length(d)
}
console.print(total)
`},
		// 散列表扩容时，位置里的文本键与文本值都要跟着搬：这一条只添加一次、不覆盖、不读回，
		// 专门验证"容器是该位置里那块内存唯一的持有者"。
		{"装进去的文本键与文本值（再散列时一起走）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, string> = {}
    let k:string = "甲".slice(0, 1)
    let v:string = "旧旧".slice(0, 2)
    d.add(k, v)
    total = total + length(d)
}
console.print(total)
`},
		// 这两个形状此前只有文字描述、没有可复跑的源码，这里把源码固定进测试：
		// 以后复测直接跑这一条，不必重新构造。
		// list 每圈增长到 20 格（`@list_add` 一路翻倍、搬完把旧的几块释放）⇒ 应当释放。
		{"list 每圈长到 20 个位置再整条释放", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<int> = []
    for (j:int in 0 until 20) {
        xs.add(j)
    }
    total = total + length(xs)
}
console.print(total)
`},
		// 计算得到的三段文本存进**顶层** dict ⇒ 键仍被持有，堆水位按圈增长是预期行为。
		// 这一行验证反方向：不要把"仍被持有"当成"泄漏"去多余释放（容器位置会析构，
		// 但这本 dict 到程序结束才散，中间每一圈的键都还在它手里）。
		{"算出来的三段文本存进顶层 dict（键还活着，该涨）", false, `
let d:dict<string, int> = {}
for (i:int in 0 until @N@) {
    let seed:string = number.to_string(i) + "abcdefghi"
    d.add(seed.slice(0, 3), i)
    d.add(seed.slice(3, 6), i)
    d.add(seed.slice(6, 9), i)
}
console.print(length(d))
`},
		// 一处**早就存在**的泄漏，根因查到底后确认：**不是"位置里那块没析构"，而是空闲链表
		// 整块交出不拆两半**。
		// 一个文本键的块是 32 字节（头 8 ＋ 字节数 8 ＋ 码点数 8 ＋ UTF-8），`Row2` 的块是 24（头 8 ＋ 两个 int），
		// 两块同在上界 32 那一档。拆表时先释放键再释放值 ⇒ 链头是那块小的；下一圈先要 32 的那一笔，
		// 链头只有 24 ⇒ 按空闲链表的既有规矩原样挂回去、这一笔改走 bump ⇒ **每圈多切一块 32**。
		// 八条对照把这条固定住：两块一样大（三个 int 的 class＝32）0／键换成八个字节（还是 32）32／
		// 键换成四个码点（40，另一档）0／值换成一个 int 的 class（16，另一档）0／值换成 int 0／
		// `hashset<string>` 只一块 0／`list<Row2>` 只一块 0。一块都没漏还，只是水位每圈涨一档。
		// **"大块拆两半"的方案治不到这个位置**：余量只有 24−16＝8 字节，装不下一个新的块头加
		// 8 字节的挂链处（`freeSplitMin`），无法拆分 ⇒ 照旧整块给出去（`runtime` 那条测试里"无法拆分"
		// 那一半验证的就是这个）。收掉它的做法是**低段按 8 字节精确分档**：一档一种尺寸
		// ⇒ 24 与 32 分属两档，互不占用。改分档前后这一行的实测：幂档 32 字节／圈、精确档 0
		// （把 `heapClass` 换回单段翻倍，这一行立刻回到 32——已跑给测试看过，不必再反向验证）。
		{"同一档里差 8 字节的两大一小交替要块（低段精确分档）", true, `
class Row2 {
    n:int
    m:int
}
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, Row2> = {}
    let k:string = "甲".slice(0, 1)
    d.add(k, new Row2(n: i, m: i))
    total = total + length(d)
}
console.print(total)
`},
		// 与上一条同族（同一档里两种尺寸差 8 字节 ⇒ 无法拆分）：换下来的旧值确实被析构了
		// （"覆盖 dict 的那个位置（换下来的 class 值）"那条量着 0），这里每格再要一次"大小交替"，
		// 所以是 2 × 32 而不是 1 × 32。
		// 对照：同样两个槽位、把 `kids` 换成八个元素（那块进入另一档）⇒ 每圈 32，不是 64。
		// 同样由低段精确分档收掉：改分档前后这一行 64 → 0。
		{"覆盖 dict 的那个位置（换下来的 class 连着 list，同属低段精确分档那一族）", true, `
class Row {
    n:int
    kids:list<int>
}
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, Row> = {}
    let k1:string = "甲".slice(0, 1)
    d.add(k1, new Row(n: i, kids: [i, i]))
    let k2:string = "甲".slice(0, 1)
    d[k2] = new Row(n: i + 1, kids: [i])
    total = total + length(d)
}
console.print(total)
`},
		// 一处历史遗留：`xs[0]`、`d[k]` 这种读就是读走，取出的那个句柄原先落在**没名字的临时槽**
		// 上——`claim` 只管有名字的、`takeArg` 只管交给用户函数的，两头都不沾，量着 32 字节／圈。
		// 这个位置已收进释放规则：交给**只读借用**（标准库组函数）的那些当场释放。安全依据有两条：
		// 源位置在读走那一刻被抹成 `null`（没有第二个出处），而标准库不收留值。
		// 仍漏的另一半（进运算式的那些）也已收掉：交完当场释放，见下面那条。
		{"按下标读走之后交给标准库的那个临时值", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<string> = ["甲乙".slice(0, 2)]
    total = total + length(xs[0])
}
console.print(total)
`},
		// 字段这一侧与容器那个位置适用同一套规则：覆盖字段就把换下来的那一个析构。
		// 早前"读字段不算交出，所以字段那头不许析构"是旧规则；现行规则下
		// 读字段只在"多出一个新持有者"的那几处算交出，而覆盖字段不产生新持有者——
		// 被换下来的那一个除这个位置外无人持有，析构它是安全的。
		{"覆盖 class 的字段（旧的那一块当场拆掉）", true, `
class Row {
    n:int
}
class Box {
    inner:Row
}
let total:int = 0
for (i:int in 0 until @N@) {
    let b:Box = new Box(inner: new Row(n: i))
    b.inner = new Row(n: i + 1)
    total = total + b.inner.n
}
console.print(total)
`},
		// 早退路径上的退出清理代码。三条早退路各一条用例：return、throw、break。
		// 没有清理代码时这三条每条都会整块泄漏——最初量得 return 一侧为每圈 40 字节。
		{"函数以 return 收场（帧里登记过的靠退出清理代码还）", true, `
method make(n:int):int {
    let xs:list<int> = [n]
    xs.add(n)
    return length(xs)
}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + make(i)
}
console.print(total)
`},
		{"每圈抛一次、由外面接住", true, `
method risky(n:int) {
    let xs:list<int> = [n]
    throw (120, "每圈都抛")
}
let total:int = 0
for (i:int in 0 until @N@) {
    try {
        risky(i)
    } catch (code, msg) {
        total = total + 1
    }
}
console.print(total)
`},
		{"内圈每圈 break 出去", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    for (j:int in 0 until 2) {
        let inner:list<int> = [j]
        break
    }
    total = total + i
}
console.print(total)
`},
		// 跨帧抛出：中间那一帧与接住块自己 try 体里那一笔都没走过本帧的退出清理代码。
		// 补上帧内清理记录之前这一条实测每圈 176 字节（中间那一帧与 try 体里各一个 list），补完是 0。
		{"跨两帧抛（中间那一帧＋接住块那一层）", true, `
method deep(n:int) {
    throw (130, "跨帧抛")
}
method middle(n:int):int {
    let mid:list<int> = [n]
    deep(n)
    return length(mid)
}
let total:int = 0
for (i:int in 0 until @N@) {
    try {
        let held:list<int> = [i]
        total = total + middle(i) + length(held)
    } catch (code, msg) {
        total = total + code
    }
}
console.print(total)
`},
		// 没有名字的新造块直接当实参：调用点上这个位置登记持有者。
		{"直接交一只新造的list（不带走）", true, `
method use(xs:list<int>):int {
    return length(xs)
}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + use([i, i])
}
console.print(total)
`},
		{"直接交一段新拼的文本（不带走）", true, `
method count(s:string):int {
    return length(s)
}
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + count("甲" + "乙")
}
console.print(total)
`},
		// 同一段负载把 list 换成被调方 return 出去的：递进去的那一块出了被调方后只剩结果这个位置持有。
		{"直接交一只新造的list（被调方带走）", true, `
method passOn(xs:list<int>):list<int> {
    return xs
}
let total:int = 0
for (i:int in 0 until @N@) {
    let got:list<int> = passOn([i, i])
    total = total + length(got)
}
console.print(total)
`},
		// 被调方**自己新造**一个值 `return` 给调用方时，调用方原本不敢释放（被调方也许还把
		// 同一块写在别处），所以每一圈那 88 字节无人认领。上面那条 `passOn` 返回的是递进去的
		// 那一块，`Facts.Back` 能证明只有结果这一个出处，所以是 0。这一行覆盖的写法需要为
		// 每条函数证明"返回的那一块没有别的持有者"才敢收窄；该证明规则实现后这一族量回 0。
		{"被调方新造一个值 return 给调用方（证明返回块无其他持有者后调用方可释放）", true, `
method build(n:int):list<int> {
    return [n]
}
let total:int = 0
for (i:int in 0 until @N@) {
    let got:list<int> = build(i)
    total = total + length(got)
}
console.print(total)
`},
		// 旧规则下这一条是 104 字节／圈：`return h.peer` 只把字段里那个句柄复制一份给调用方，
		// `h` 那一侧还持有同一块，所以整帧不许释放，多出的是 Holder 自身那 16 字节。
		// 现行规则下，`h.peer` 读出去就是把那个位置交走，读完那个位置为空，
		// Holder 照常在帧尾释放 ⇒ 104 → 88。
		// 剩下的 88 来自"被调方自己新造一个值 return 给调用方时调用方不敢释放"；
		// 该场景已被证明安全：这条函数的每一条 return 返回的都是当场新造的或从自己那个位置
		// 作废出来的那一份 ⇒ 88 → 0（上面两行）。
		{"return 把字段里的句柄交出去（源头已空；交回的那一块归调用方）", true, `
class Holder {
    peer:list<int>
}
method hand(n:int):list<int> {
    let h:Holder = new Holder(peer: [n])
    return h.peer
}
let total:int = 0
for (i:int in 0 until @N@) {
    let got:list<int> = hand(i)
    total = total + got[0]
}
console.print(total)
`},
		// ---- 先度量这三族的每圈字节，不改实现 ----
		// 读走的那一个句柄进了运算式：`xs[0] + "丙丁"` 两个操作数读完即无其他持有者，
		// 拼出来的那一段交给外层那一步（`length(...)`）用完再释放。该规则实现后量回 0。
		{"读走的文本进加法", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<string> = ["甲乙".slice(0, 2)]
    total = total + length(xs[0] + "丙丁")
}
console.print(total)
`},
		// 读走的那一个句柄进盒子：`class` 装箱交出的是同一份句柄，盒子就是新持有者。
		// 曾量得 248 字节／圈——大头不在归属规则，而是**打印机每打印一次复合值就泄漏一整段
		// 攒出来的文本**（一路 `@str_cat` 把前一段丢了）。改为每拼一次释放一次（print.go 的
		// catFree）之后，这一条回到 0。
		{"读走的 class 进盒子", true, `
class Note {
    n:int
}
let total:int = 0
for (i:int in 0 until @N@) {
    let cs:list<Note> = [new Note(n: i)]
    console.print(cs[0] as object)
    total = total + length(cs)
}
console.print(total)
`},
		// 同一份负载把装箱去掉，只打印字段：用于分离 248 字节里由装箱那一步贡献的部分。
		{"对照：读走的 class 只读字段就印", true, `
class Note {
    n:int
}
let total:int = 0
for (i:int in 0 until @N@) {
    let cs:list<Note> = [new Note(n: i)]
    console.print(cs[0].n)
    total = total + length(cs)
}
console.print(total)
`},
		// 同一份负载一次都不读走，整个容器直接打印。
		{"对照：整只 list 直接印，不读走", true, `
class Note {
    n:int
}
let total:int = 0
for (i:int in 0 until @N@) {
    let cs:list<Note> = [new Note(n: i)]
    console.print(cs)
    total = total + length(cs)
}
console.print(total)
`},
		// 读走的那一个句柄交给用户函数，被调方只读不带走：**实测为 0**，这一条覆盖"该释放"。
		{"读走的 class 交给不带走的函数", true, `
class Note {
    n:int
}
method onlyRead(n:Note):int {
    return n.n
}
let total:int = 0
for (i:int in 0 until @N@) {
    let cs:list<Note> = [new Note(n: i)]
    total = total + onlyRead(cs[0])
}
console.print(total)
`},
		// 同一份值经被调方 `return` 转手回来，由接住它的那一个 `let` 位置持有；实现后回到 0。
		{"读走的 class 转手回来", true, `
class Note {
    n:int
}
method passOn(n:Note):Note {
    return n
}
let total:int = 0
for (i:int in 0 until @N@) {
    let cs:list<Note> = [new Note(n: i)]
    let got:Note = passOn(cs[0])
    total = total + got.n
}
console.print(total)
`},
		// `class` 里那条文本：析构那一步不往串里走。规则把"读 class 的引用类型字段"
		// 改成算交出之后，字段就是那块文本唯一的持有者、这一族本可以正常析构；这条语言规则
		// 已裁定维持原样，所以这一行验证"故意不释放"仍然生效：数值一旦归零就说明规则被改动。
		{"class 里的文本字段（设计上不释放）", false, `
class Box {
    v:string
}
let total:int = 0
for (i:int in 0 until @N@) {
    let b:Box = new Box(v: "甲乙" + "丙丁")
    total = total + i
}
console.print(total)
`},
		// 度量交出一族时新查出来的一行：把 struct 交给一条函数，调用点要整份拷贝一块
		// （传参对值类型就是"给它一份副本"），那一块是这一刻为这一个位置造的，
		// 除了被调方那个位置无人持有 ⇒ 调用方登记成持有者。收掉之前两万圈 320,184 字节（每圈 16），
		// `release/v0.2.0/nf.exe` 同一段是 320,200——这一族早就存在，不是新引入的。
		{"值类型实参在调用点拷出来的那一块", true, `
struct P {
    t:string
}
method count(p:P):int {
    return length(p.t)
}
let q:P = P { t: "甲" + "乙" }
let total:int = 0
for (i:int in 0 until @N@) {
    total = total + count(q)
}
console.print(total)
`},
		// 一个式子中间那几步读走的值，落在没名字的中转位置上，此前的规则只看得到最外面那一个值。
		// 下面每一行收掉之前都是每圈 88 或 16 字节（一个 list 连头带那一串位置、或一个 class 对象），
		// 九种写法各覆盖一处落点：这一句无人接、声明的初值、赋值两侧、return、遍历所用的那一条、
		// 计数边界、读出来作方法接收者、字典位置的下标访问。
		{"这一句没人接住、中间那一步读走的那一条（xs[0][0]）", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    xs[0][0];
    total = total + 1
}
console.print(total)
`},
		{"声明的初值里中间那一步读走的那一条", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    let t:int = xs[0][0];
    total = total + t
}
console.print(total)
`},
		{"等号右边中间那一步读走的那一条", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    let k:int = 0
    k = xs[0][0];
    total = total + k
}
console.print(total)
`},
		{"等号左边先绑一个名字再写那一格", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[1, 1], [1, 1]]
    let row:list<int> = xs[0]
    row[0] = 7;
    total = total + row[0]
}
console.print(total)
`},
		{"return 那一句里中间那一步读走的那一条", true, `
method head(xs:list<list<int>>):int {
    return xs[0][0];
}
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    total = total + head(xs)
}
console.print(total)
`},
		{"遍历用的那一条是读走的", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[i, i], [i, i]]
    for (v:int in xs[0]) {
        total = total + v
    }
}
console.print(total)
`},
		{"计数循环的边界里中间那一步读走的那一条", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<list<int>> = [[2, 2], [2, 2]]
    for (j:int in 0 until xs[0][0]) {
        total = total + j
    }
}
console.print(total)
`},
		{"读出来当方法接收者的那一个对象", true, `
class Node {
    v:int
    method ping() {
        console.print(v)
    }
}
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<Node> = [new Node(v: 7), new Node(v: 8)]
    xs[0].ping()
    total = total + 1
}
console.print(total)
`},
		{"字典里那一格再点一下", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let d:dict<string, list<int>> = {"甲": [1, 2], "乙": [3, 4]}
    d["甲"][0];
    total = total + 1
}
console.print(total)
`},
		// `bytes` 那一档的四行：一块连续存放的字节，每格宽一个字节。
		// 这四行各覆盖一处"谁持有这一块"：名字作废、容器的那个位置、字段被换下、slice 造出的那一片。
		{"每一圈造一块 bytes，名字到尽头就散", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let b:bytes = bytes.new(24)
    total = total + length(b)
}
console.print(total)
`},
		{"每一圈把一块 bytes 放进容器那一格", true, `
let total:int = 0
for (i:int in 0 until @N@) {
    let xs:list<bytes> = [bytes.new(24)]
    total = total + length(xs)
}
console.print(total)
`},
		{"覆盖 class 字段里那一块 bytes（旧的那一块当场拆）", true, `
class Holder {
    data:bytes
}
let total:int = 0
for (i:int in 0 until @N@) {
    let h:Holder = new Holder(data: bytes.new(16))
    h.data = bytes.new(8)
    total = total + length(h.data)
}
console.print(total)
`},
		{"每一圈从一块 bytes 里 slice 出那一片（顶层那一块留着，slice 的每一片散）", true, `
let head:bytes = bytes.new(64)
let total:int = 0
for (i:int in 0 until @N@) {
    let cut:bytes = head.slice(0, 8)
    total = total + length(cut)
}
console.print(total)
`},
	}
	for _, one := range cases {
		per := heapSlope(t, one.tmpl)
		t.Logf("%-34s 每圈 %8.1f 字节", one.name, per)
		if one.reused && per >= 2 {
			t.Errorf("%s：该还掉的每圈漏了 %.1f 字节", one.name, per)
		}
		if !one.reused && per <= 2 {
			t.Errorf("%s：这一类本来是故意不还的，每圈只漏 %.1f 字节？", one.name, per)
		}
	}
}

// TestHeapSlopeHandles 覆盖新增的一类资源：句柄不是堆上的一块内存，名字到作用域尽头调用一次
// `closesocket`，所以每圈多出来的字节应为 0；漏关的那种写法暴露的是"连不上"，不是字节数。
//
// 单独成一条而不并入上面那张表：那几条的源码只有一个 @N@ 要填，这几条还需要一个本机监听端——
// 端口由测试进程现场取得，填入的是第二个位置 @P@。
func TestHeapSlopeHandles(t *testing.T) {
	echo, stopEcho := echoServe(t)
	defer stopEcho()
	bind := freePort(t) // listen 那条用例绑定的端口：现场取得，测试进程释放后立即归还系统
	rows := []struct {
		name string
		port int
		tmpl string
	}{
		{"每一圈连一次、送一段、再收回来", echo, `
let total:int = 0
for (i:int in 0 until @N@) {
    let s:Socket = net.connect("127.0.0.1", @P@, 3000)
    let msg:bytes = "ping".to_bytes()
    s.send(msg)
    let back:bytes = bytes.new(8)
    total = total + s.recv_into(back)
}
console.print(total)
`},
		{"每一圈连一次（不设超时那一条路）", echo, `
let total:int = 0
for (i:int in 0 until @N@) {
    let s:Socket = net.connect("127.0.0.1", @P@, 0)
    total = total + 1
}
console.print(total)
`},
		{"每一圈 listen 一次", bind, `
let total:int = 0
for (i:int in 0 until @N@) {
    let l:Listener = net.listen(@P@)
    total = total + 1
}
console.print(total)
`},
	}
	for _, one := range rows {
		tmpl := strings.ReplaceAll(one.tmpl, "@P@", strconv.Itoa(one.port))
		per := heapSlope(t, tmpl)
		t.Logf("%-34s 每圈 %8.1f 字节", one.name, per)
		if per >= 2 {
			t.Errorf("%s：句柄那一档该还掉的每圈漏了 %.1f 字节", one.name, per)
		}
	}
}

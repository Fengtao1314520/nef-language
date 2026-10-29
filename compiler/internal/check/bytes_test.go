// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// bytes_test.go 覆盖 `bytes` 这一档在检查阶段的规则：名字是保留的、能写在哪些地方、
// 下标两侧各收什么、进不了盒子、当不了键、`bytes` 组里只有 `new` 与 `from_base64` 两条、
// 类型上只有 `slice` 与 `to_base64` 两条成员，以及不支持 `for ... in` 遍历。
//
// 运行期那一侧（实际运行出来的数与越界那几句）在 `internal/lower/bytes_e2e_test.go`。

// TestBytesNameIsReserved 覆盖 `bytes` 已进内建名字表这一条：拿它当名字要拒绝，
// 拿它当类型名、组名、容器元素、字段都合法。
func TestBytesNameIsReserved(t *testing.T) {
	expectErr(t, "let bytes:int = 1\n", "bytes 是内建名字，不能拿去做名字")
	expectErr(t, "method bytes(n:int):int { return n }\n", "bytes 是内建名字，不能拿去做名字")
	expectErr(t, "struct bytes {\n    n:int\n}\n", "bytes 是内建名字，不能拿去做名字")
	expectOK(t, "let b:bytes = bytes.new(4)\nclass Holder {\n    data:bytes\n}\n")
	expectOK(t, "let xs:list<bytes> = []\nlet d:dict<string, bytes> = {}\n")
}

// TestBytesCallShapes 覆盖 `bytes.new(容量)` 这一条的形状：收一个整数、返回一块 bytes；
// 组名式调用只认 `new`，`slice` 挂在类型上而不是组上。
func TestBytesCallShapes(t *testing.T) {
	expectOK(t, "let b:bytes = bytes.new(4)\nconsole.print(length(b))\n")
	expectOK(t, "let n:int = 8\nlet b:bytes = bytes.new(n)\n")
	expectErr(t, "let b:bytes = bytes.new(\"四\")\n", "这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, "let b:bytes = bytes.new()\n", "new 收 1 个参数，这里给了 0 个")
	expectErr(t, "let b:bytes = bytes.new(4, 5)\n", "new 收 1 个参数，这里给了 2 个")
	expectErr(t, "let b:bytes = bytes.make(4)\n", "bytes 组里没有 make")
	expectErr(t, "let b:bytes = bytes.new(4)\nlet c:bytes = bytes.slice(b, 0, 2)\n", "bytes 组里没有 slice")
	expectErr(t, "let t:bytes<int> = bytes.new(4)\n", "bytes 不带尖括号参数")
	// base64 这一对：写成 base64 挂在类型上（与 `to_bytes` 同一条路），读回来挂在组上
	// （与 `text.from_base64` 同一条路）。返回与收下的类型不对，走的是同一句"类型之间不自动转换"。
	expectOK(t, "let b:bytes = bytes.new(4)\nlet s:string = b.to_base64()\nconsole.print(s)\n")
	expectOK(t, "let b:bytes = bytes.from_base64(\"YQ==\")\nconsole.print(length(b), b.to_base64())\n")
	expectErr(t, "let b:bytes = bytes.new(4)\nlet n:int = b.to_base64()\n",
		"这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, "let b:bytes = bytes.new(4)\nlet s:string = b.to_base64(1)\n", "to_base64 收 0 个参数，这里给了 1 个")
	expectErr(t, "let b:bytes = bytes.from_base64(4)\n", "这里要的是 string，给的是 int；类型之间不自动转换")
	expectErr(t, "let b:bytes = bytes.from_base64()\n", "from_base64 收 1 个参数，这里给了 0 个")
	expectErr(t, "let b:bytes = bytes.to_base64(bytes.new(4))\n", "bytes 组里没有 to_base64")
	// 合并字节块：收一只 `list<bytes>`，返回一块新的。清单那个位置不是"能装下就行"——
	// 元素档数不对就是另一档，与 `text.join` 收 `list<string>` 是同一条拒绝规则。
	expectOK(t, "let xs:list<bytes> = []\nlet b:bytes = bytes.join(xs)\nconsole.print(length(b))\n")
	expectOK(t, "let b:bytes = bytes.join([bytes.new(0), \"甲\".to_bytes()])\n")
	expectErr(t, "let b:bytes = bytes.join(4)\n", "这里要的是 list<bytes>，给的是 int；类型之间不自动转换")
	expectErr(t, "let xs:list<int> = []\nlet b:bytes = bytes.join(xs)\n",
		"这里要的是 list<bytes>，给的是 list<int>；类型之间不自动转换")
	expectErr(t, "console.print(bytes.join([\"甲\"]))\n", "这里要的是 list<bytes>，给的是 list<string>；类型之间不自动转换")
	expectErr(t, "let b:bytes = bytes.join()\n", "join 收 1 个参数，这里给了 0 个")
	expectErr(t, "let b:bytes = bytes.new(4)\nlet c:bytes = b.join()\n", "bytes 上没有 join")
}

// TestBytesCellTypes 覆盖下标两侧：位置是 int、那个位置上的值是 byte。
// 写进去的字面量越界与 `list<byte>` 是同一句，写一个整数变量也是同一句（不自动转换）。
func TestBytesCellTypes(t *testing.T) {
	expectOK(t, "let b:bytes = bytes.new(4)\nb[0] = 255\nconsole.print(b[0], length(b))\n")
	expectErr(t, "let b:bytes = bytes.new(4)\nb[0] = 300\n", "byte 只能装 0～255，这里是 300")
	expectErr(t, "let b:bytes = bytes.new(4)\nlet n:int = 7\nb[0] = n\n",
		"这里要的是 byte，给的是 int；类型之间不自动转换")
	expectErr(t, "let b:bytes = bytes.new(4)\nb[0] = '甲'\n",
		"这里要的是 byte，给的是 unicode；类型之间不自动转换")
	expectErr(t, "let b:bytes = bytes.new(4)\nlet s:string = b[\"甲\"]\n",
		"这里要的是 int，给的是 string；类型之间不自动转换")
	// 取出来是一个 byte：byte 自己不参与算术两边，要加先 as int
	expectOK(t, "let b:bytes = bytes.new(4)\nconsole.print(b[0] as int + 1)\n")
	expectErr(t, "let b:bytes = bytes.new(4)\nconsole.print(b[0] + 1)\n",
		"+ 两边必须是同一类型：byte 与 int")
}

// TestBytesCannotBeBoxed 覆盖这一档进不了盒子，与容器、`Json` 是同一条拒绝规则：
// 装进去、拆回来、`is` 一问，三处都在检查阶段拒绝，不留到发码那一步。
func TestBytesCannotBeBoxed(t *testing.T) {
	expectErr(t, "let b:bytes = bytes.new(4)\nlet o:object = b as object\n",
		"bytes 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	expectErr(t, "let o:object = 1 as object\nlet b:bytes = o as bytes\n",
		"盒子里没有 bytes：它进不了盒子，也拆不出来")
	expectErr(t, "let b:bytes = bytes.new(4)\nconsole.print(b is bytes)\n",
		"is 的右边只能写盒子里可能装的那个类型")
	// 正当写法照旧过：盒子里装一个 int、bytes 单独判空
	expectOK(t, "let o:object = 1 as object\nlet b:bytes = bytes.new(4)\nconsole.print(o is int, b == null)\n")
}

// TestBytesMembersAndLength 覆盖类型上只有 `slice` 这一条成员、长度走 `length(b)` 这一个函数。
func TestBytesMembersAndLength(t *testing.T) {
	expectOK(t, "let b:bytes = bytes.new(4)\nconsole.print(length(b), b.slice(1, 3))\n")
	expectErr(t, "let b:bytes = bytes.new(4)\nconsole.print(b.length())\n", "bytes 上没有 length")
	expectErr(t, "let b:bytes = bytes.new(4)\nconsole.print(b.add(1))\n", "bytes 上没有 add")
	expectErr(t, "let b:bytes = bytes.new(4)\nconsole.print(b.slice(1))\n", "slice 收 2 个参数，这里给了 1 个")
	expectErr(t, "let b:bytes = bytes.new(4)\nconsole.print(b.slice(\"甲\", 2))\n",
		"这里要的是 int，给的是 string；类型之间不自动转换")
}

// TestBytesCannotBeAKey 覆盖它当不了 dict 的键与 hashset 的成员（两处说的都是"键"，
// hashset 那一种没有值那一半）：这一档落进 keyReason 最后那一条，与 `Json` 是同一句答复。
func TestBytesCannotBeAKey(t *testing.T) {
	expectErr(t, "let b:bytes = bytes.new(4)\nlet d:dict<bytes, int> = {}\n",
		"bytes 不能当 dict 的键：这个类型不能当键")
	expectErr(t, "let s:hashset<bytes> = {}\n",
		"bytes 不能当 hashset 的键：这个类型不能当键")
}

// TestBytesCannotBeIterated 覆盖 `bytes` 不支持 `for ... in` 遍历：`for (x:byte in b)` 在检查阶段
// 拒绝（`stmt.go` 里那句报错），要一路走过每一个位置就拿下标的计数循环，那一条照旧通过。
func TestBytesCannotBeIterated(t *testing.T) {
	expectErr(t, "let b:bytes = bytes.new(4)\nfor (x:byte in b) {\n    console.print(x)\n}\n",
		"bytes 不能遍历")
	expectErr(t, "let b:bytes = bytes.new(4)\nfor (x:int in b) {\n    console.print(x)\n}\n",
		"bytes 不能遍历")
	expectOK(t, "let b:bytes = bytes.new(4)\nfor (i:int in 0 until length(b)) {\n    console.print(b[i])\n}\n")
}

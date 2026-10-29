// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import "testing"

// net_test.go 覆盖 `net` 这一组的检查面：
// 两条组函数的签名、两类句柄的归属拒绝规则、还有空句柄在哪些位置算合法。
//
// 这里只管"检查阶段就说"的那几条；实际运行那一条端到端在 `internal/lower/net_e2e_test.go`。

func TestNetFuncSignatures(t *testing.T) {
	expectOK(t, `let s:Socket = net.connect("127.0.0.1", 80, 3000)
let l:Listener = net.listen(8080)
let a:Socket = l.accept()
let m:bytes = "hi".to_bytes()
console.print(s.send(m), s.recv_into(bytes.new(8)))
a.close()
s.close()
l.close()
`)
	// 超时那一个位置不能省：没有线程，连接必须有到点的时候
	expectErr(t, `let s:Socket = net.connect("127.0.0.1", 80)`,
		"connect 收 3 个参数，这里给了 2 个")
	// 端口与超时都是整数，不能拿文本混进去
	expectErr(t, `let s:Socket = net.connect("127.0.0.1", "80", 3000)`,
		"这里要的是 int，给的是 string；类型之间不自动转换")
	expectErr(t, `let s:Socket = net.listen("8080")`,
		"这里要的是 int，给的是 string；类型之间不自动转换")
	// 两类句柄各是各的，不能互相顶替
	expectErr(t, `let l:Listener = net.connect("127.0.0.1", 80, 3000)`,
		"这里要的是 Listener，给的是 Socket；类型之间不自动转换")
	// accept 与 send／recv_into／close 只挂在各自那一类句柄上
	expectErr(t, `let l:Listener = net.listen(8080)
console.print(l.recv_into(bytes.new(4)))`,
		"Listener 上没有 recv_into")
}

// TestHandleCannotGoIntoContainers 覆盖这一族拒绝规则：句柄的归宿是"关一次"，
// 容器那个位置要的是"能被拿着再被析构的一块内存"。
func TestHandleCannotGoIntoContainers(t *testing.T) {
	expectErr(t, `let xs:list<Socket> = []`,
		"Socket 进不了 list：它是操作系统给的句柄，只待在名字、实参与返回值里")
	expectErr(t, `let d:dict<string, Listener> = {}`,
		"Listener 进不了 dict：它是操作系统给的句柄，只待在名字、实参与返回值里")
	expectErr(t, `let hs:hashset<Socket> = {}`,
		"Socket 进不了 hashset：它是操作系统给的句柄，只待在名字、实参与返回值里")
	// 装箱同一族：盒子里没有句柄这一档
	expectErr(t, `let s:Socket = net.connect("127.0.0.1", 1, 0)
let o:object = s as object`,
		"Socket 装不进盒子：盒子只装 class、struct 和值类型，要混装就一个一个装进去")
	// 长度只有那五种类型有：句柄没有"多长"这一问
	expectErr(t, `let s:Socket = net.connect("127.0.0.1", 1, 0)
console.print(length(s))`,
		"length 数不了 Socket：只有 list、dict、hashset、string 与 bytes 这五样有长度")
}

// TestHandleNullable 覆盖那两类句柄与引用类型同一档：`null` 写得出来，也走得进 `if`。
// 空句柄再去点成员是运行期的 10092 号，检查阶段只管类型形状。
func TestHandleNullable(t *testing.T) {
	expectOK(t, `let s:Socket = null
if (s == null) {
    console.print("还没连")
}
s = net.connect("127.0.0.1", 1, 0)
`)
	expectOK(t, `method pick(l:Listener):Listener {
    return l
}
`)
}

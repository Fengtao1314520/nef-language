// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// heaplock_e2e_test.go 钉堆锁那一条路走完了要放开。
//
// 那一颗锁是不重入的：同一条线程拿着它再去拿就停在那里。@alloc 要不到内存的那一支因此必须
// 先放开再走 @throw_walk——走路途中 @pop_handler 归还处理器记录要叫 @free，那一条也要拿锁。
// 漏开放开的话，这一份程序不会报错，只会停住：接住块里再也分配不成，第二次的大请求也到不了
// 抛出那一步。所以两条都用"接住之后还能继续分配"来钉，停住由 go test 的超时报出来。

package lower_test

import (
	"testing"
)

func TestCaughtOutOfMemoryKeepsHeapUsable(t *testing.T) {
	got, code := run(t, `
let big:int = 4294967296
try {
    let b:bytes = bytes.new(big)
    console.print("这么大的请求本该抛出来")
} catch (c, m, at) {
    console.print(c, m)
}
let xs:list<int> = [1, 2, 3]
console.print(length(xs), xs[0])
`)
	want := "10091 向操作系统要不到内存\n3 1\n"
	if got != want || code != 0 {
		t.Fatalf("接住 10091 之后堆该照常用：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// 第二次同样太大的请求要再抛一次：锁若还握在手里，这一次连抛出都到不了。
func TestOutOfMemoryTwiceThrowsTwice(t *testing.T) {
	got, code := run(t, `
let big:int = 4294967296
let hits:list<int> = []
for (i:int in 0 until 2) {
    try {
        let b:bytes = bytes.new(big)
        console.print(b)
    } catch (c, m, at) {
        hits.add(c)
    }
}
console.print(hits[0], hits[1], length(hits))
`)
	if want := "10091 10091 2\n"; got != want || code != 0 {
		t.Fatalf("两次太大的请求要各抛一次：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

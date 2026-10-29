// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import "testing"

// TestTypeAliasRun 验证类型别名在后端这一侧：别名写的程序照常运行得出结果——
// 类型字、形状表、`new`、枚举成员、容器成员调用、`as`／`is` 全按解析到的最终名字走。
func TestTypeAliasRun(t *testing.T) {
	got, code := run(t, `type Row2 = Row
type Box2 = Box
type Choice = D
type Names = list<string>
type Scores = dict<string, int>
type Cal = Calendar

struct Row {
    x:int
}

class Box {
    n:int
    method get():int { return n }
}

enum D { a, b }

let r:Row2 = Row2 { x: 3 }
let b:Box2 = new Box2(n: r.x)
let d:Choice = Choice.b
let xs:Names = ["甲"]
xs.add("乙")
let m:Scores = {"甲": 1}
let c:Cal = time.calendar(951782400)
let box:object = Row2 { x: 7 } as object
console.print(r.x, b.get(), d, xs[1], m["甲"], c.year, length(xs))
console.print(box is Row2, box is Row)
`)
	want := "3 3 1 乙 1 2000 2\ntrue true\n"
	if got != want || code != 0 {
		t.Fatalf("别名写的程序跑偏了：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTypeAliasHeapSameAsFullName 是"别名不是一种新类型"的实测：同一份负载，
// 一份写全名、一份写别名，向堆累计申请的总字节数必须完全一致。
// 对不上说明别名在后端留下了第二种痕迹——多一块、多一个类型字、多一条形状表都会让这个数变化。
func TestTypeAliasHeapSameAsFullName(t *testing.T) {
	full := `struct Row {
    x:int
}

method make():Row {
    let xs:list<string> = ["甲"]
    xs.add("乙")
    return Row { x: length(xs) }
}

let total:int = 0
for (i:int in 0 until 2000) {
    let r:Row = make()
    total = total + r.x
}
console.print(total)
`
	short := `type Row2 = Row
type Names = list<string>

struct Row {
    x:int
}

method make():Row2 {
    let xs:Names = ["甲"]
    xs.add("乙")
    return Row2 { x: length(xs) }
}

let total:int = 0
for (i:int in 0 until 2000) {
    let r:Row2 = make()
    total = total + r.x
}
console.print(total)
`
	wantOut, wantHeap := runHeap(t, full)
	gotOut, gotHeap := runHeap(t, short)
	if gotOut != wantOut {
		t.Fatalf("两份程序印出来的该一样：\n got %q\nwant %q", gotOut, wantOut)
	}
	if gotHeap != wantHeap {
		t.Fatalf("别名不该在堆上留痕迹：全名 %d 字节，别名 %d 字节", wantHeap, gotHeap)
	}
}

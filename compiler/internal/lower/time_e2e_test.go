// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import "testing"

// TestTimeGroupRun 运行两条时钟成员。
//
// `time.now()` 只检查一个区间（1700000000 到 2000000000 之间）——它的值每次调用都变化，
// 写死期望值的测试很快就会失败；区间的理由写在断言旁边，范围到期时更换这两个边界值即可。
// `time.elapsed()` 验证时间真实推进：起始点之后取得的第二个读数不会小于起始点。
func TestTimeGroupRun(t *testing.T) {
	got, code := run(t, `let start:float = time.elapsed()
let total:int = 0

for (i:int in 0 until 100) {
    total = total + i
}

console.print(total)
console.print(time.elapsed() >= start)
let now:int = time.now()
console.print(now > 1700000000, now < 2000000000)
console.print(time.elapsed() - start >= 0.0)
`)
	want := "4950\ntrue\ntrue true\ntrue\n"
	if got != want || code != 0 {
		t.Fatalf("time 那两条不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTimeCalendar 验证 `time.calendar(seconds)`：按 UTC 把秒数拆成内建 `Calendar` 的各个字段。
//
// 期望串由另一套算法（通用的 UTC 拆解）算出，不是按本例程自身推导——
// 两边对不上说明例程里的除法或减法有错。负秒数、1900 与 2100（百年不闰）、
// 2000（四百年闰）特意留在用例里：向下取整与闰日最容易在这几处暴露错误。
// `weekday` 以 0 表示星期一；1970-01-01 是星期四，所以返回 3。
func TestTimeCalendar(t *testing.T) {
	got, code := run(t, `
let a:Calendar = time.calendar(0)
console.print(a.year, a.month, a.day, a.hour, a.minute, a.second, a.weekday)
let b:Calendar = time.calendar(-1)
console.print(b.year, b.month, b.day, b.hour, b.minute, b.second, b.weekday)
let c:Calendar = time.calendar(951782400)
console.print(c.year, c.month, c.day, c.weekday)
let d:Calendar = time.calendar(-2208988800)
console.print(d.year, d.month, d.day, d.weekday)
let e:Calendar = time.calendar(4104144000)
console.print(e.year, e.month, e.day, e.hour, e.weekday)
let f:Calendar = time.calendar(-62135596800)
console.print(f.year, f.month, f.day, f.weekday)
console.print(time.calendar(1762867925))
`)
	want := "1970 1 1 0 0 0 3\n" +
		"1969 12 31 23 59 59 2\n" +
		"2000 2 29 1\n" +
		"1900 1 1 0\n" +
		"2100 1 20 16 2\n" +
		"1 1 1 0\n" +
		"Calendar { year: 2025, month: 11, day: 11, hour: 13, minute: 32, second: 5, weekday: 1 }\n"
	if got != want || code != 0 {
		t.Fatalf("拆出来的年月日时分秒与星期不对：\n got %q 码 %d\nwant %q", got, code, want)
	}
}

// TestTimeCalendarFreesIt 验证两块内存的释放：接住它的那个名字到作用域尽头释放；单独写成一句也要释放
// （后者是"无人接住的值到语句尽头析构"的规则）。
//
// 修复前接住那一侧每圈留 64 字节——降级为声明做值类型拷贝时，把例程刚造好的那一块又拷贝了一份，
// 原来那一块从此无人析构；改成"初值本来就是当场新造的那一块就不再拷贝"之后归零。
// 两万圈实测：修复前 1,280,256，现在 200；丢弃那一条一直是 200。
func TestTimeCalendarFreesIt(t *testing.T) {
	for _, one := range []struct{ name, src string }{
		{"名字接住", `
for (i:int in 0 until 20000) {
    let c:Calendar = time.calendar(1762867925)
}
`},
		{"一句丢掉", `
for (i:int in 0 until 20000) {
    time.calendar(1762867925);
}
`},
	} {
		if used := heapAt(t, one.src); used > 4000 {
			t.Errorf("%s：两万圈峰值 %d 字节，那一块大概还没人拆", one.name, used)
		}
	}
}

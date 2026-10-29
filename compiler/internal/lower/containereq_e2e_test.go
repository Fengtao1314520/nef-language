// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import "testing"

// TestContainerEqualityIsIdentity 固定这条规则：容器之间判等按身份，与 `class`、`object` 用同一套规则。
//
// 内容相同不算相等（`[1, 2] == [1, 2]` 是 `false`），同一个对象的两次引用相比才算相等。
// 本测试只固定现行行为、不改实现：判等按身份是现行为，这份测试防止它后续漂移。
func TestContainerEqualityIsIdentity(t *testing.T) {
	got, code := run(t, `let a:list<int> = [1, 2]
let b:list<int> = [1, 2]
console.print(a == a, a == b, a != b)

let d:dict<int, int> = {1: 2}
let e:dict<int, int> = {1: 2}
console.print(d == d, d == e)

let s:hashset<int> = {1}
let u:hashset<int> = {1}
console.print(s == s, s == u)
`)
	if code != 0 {
		t.Fatalf("退出码 %d：%s", code, got)
	}
	want := "true false true\ntrue false\ntrue false\n"
	if got != want {
		t.Fatalf("容器判等不是按身份：\n得到 %q\n期望 %q", got, want)
	}
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"testing"
)

// TestStringIndexAndIterate 盯文本那两条：按下标取一个码点与按码点遍历。
// 文本按 UTF-8 存，第 i 格没有现成地址，只能从字节 0 一路跳过去——跳错一个字节就解成别的字。
// 后半段是越界：`"中文abc"` 只有 5 个码点，取第 5 格当场抛 10031 号。
func TestStringIndexAndIterate(t *testing.T) {
	got, code := run(t, `let word:string = "中文abc"

console.print(length(word), word[0], word[3], word[4])

let seen:int = 0
for (ch:unicode in word) {
    seen = seen + 1
    console.print(ch)
}

console.print("共", seen, "个")
`)
	want := "5 中 b c\n中\n文\na\nb\nc\n共 5 个\n"
	if got != want || code != 0 {
		t.Fatalf("文本的下标与遍历不对：\n got %q 码 %d\nwant %q", got, code, want)
	}

	// 四字节码点（一个 emoji）也要跳得对：长度算法只认首字节的高几位
	mix, mixCode := run(t, `let s:string = "a😀b"
console.print(length(s), s[0], s[1], s[2])
`)
	if mix != "3 a 😀 b\n" || mixCode != 0 {
		t.Fatalf("混着四字节码点跳错了：\n got %q 码 %d\nwant %q", mix, mixCode, "3 a 😀 b\n")
	}

	bad, badCode := run(t, `let word:string = "中文abc"
console.print(word[5])
`)
	if badCode != 1 {
		t.Fatalf("越界该以 1 收尾，实际码 %d、输出 %q", badCode, bad)
	}

	neg, negCode := run(t, `let word:string = "abc"
console.print(word[-1])
`)
	if negCode != 1 {
		t.Fatalf("负下标也该拦住，实际码 %d、输出 %q", negCode, neg)
	}
}

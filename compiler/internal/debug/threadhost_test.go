// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// threadhost_test.go 记下线程那一层开工前的一处实测：调试宿主这一头现在只有一份停住记录。
// 这一段不改任何行为，只把"现在是什么样"钉成一条测试，后面那句"只跟主线程"要落在它上面。
//
// en: threadhost_test.go records one measurement taken before rule 119 starts. It changes no
// behaviour: it pins "what the host looks like today" so that segment 7's "follow the main
// thread only" has something to land on.
package debug_test

import (
	"encoding/json"
	"testing"

	"neflang.org/nf/internal/debug"
)

// TestSecondHitOverwritesTheOnlyStopRecord 钉这一处：一次命中记一份现场，第二次把第一次盖掉。
//
// 宿主与子进程之间那根线上没有线程号（debug.Message 的字段里没有，OnHit 收的三个数是检查点号、
// 帧基、帧深），所以"哪一条线程停住了"这件事今天根本传不过来；dap 写给编辑器的那一头，线程号
// 恒为 1、线程列表恒一条、allThreadsStopped 恒真（internal/dap/dap.go 的 threadID）。
// 真起第二条线程之后，两条各命中一次就是一次盖一次：编辑器看到的位置与变量全是后那一条的。
//
// en: TestSecondHitOverwritesTheOnlyStopRecord pins this: one hit records one scene, the next
// hit overwrites it. The wire between host and child carries no thread id - Message has no such
// field and OnHit takes a checkpoint number, a frame base and a depth - so "which thread
// stopped" never arrives today; on the editor side dap always reports thread id 1, one thread in
// the list and allThreadsStopped true. With a second thread running, two hits overwrite each
// other: the position and the variables the editor shows are whichever came last.
func TestSecondHitOverwritesTheOnlyStopRecord(t *testing.T) {
	s := debug.New(sites)
	if got := s.SetBreakpoints("t.nf", []int{2}); len(got) != 1 {
		t.Fatalf("第 2 行该钉得住，交回 %v", got)
	}
	if s.OnHit(1, 0x1000, 1) != debug.Stop {
		t.Fatal("第 2 行的断点没停")
	}
	firstLast, firstRBP, firstDepth := s.Last, s.RBP, s.Depth
	s.Continue()
	if s.OnHit(2, 0x2000, 4) != debug.Stop {
		t.Fatal("同一条断点上的第二个检查点也该停")
	}
	if s.Last == firstLast || s.RBP != uintptr(0x2000) || s.Depth != 4 {
		t.Fatalf("第二次命中没把那一份现场盖掉：Last=%d RBP=%x Depth=%d（上次 RBP=%x Depth=%d）",
			s.Last, s.RBP, s.Depth, firstRBP, firstDepth)
	}
	if at, ok := s.Site(); !ok || at.ID != 2 {
		t.Fatalf("现在问停在哪，答的只能是后那一次：%+v ok=%v", at, ok)
	}
}

// TestWireMessageHasNoThreadField 钉实测的另一半：那根线上的话里没有线程号这一项。
// 子进程哪怕写来一个 thread，读进来的那一份也不会带上它——字段表里没有这一项。
//
// en: TestWireMessageHasNoThreadField pins the other half of the measurement: a message on that
// wire has no thread number. Even if the child wrote a thread, the message read back would not
// carry it - the field table has no such entry.
func TestWireMessageHasNoThreadField(t *testing.T) {
	var m debug.Message
	if err := json.Unmarshal([]byte(`{"event":"stopped","reason":"breakpoint","thread":7,"threadId":7}`), &m); err != nil {
		t.Fatalf("这一条话本该读得进来：%v", err)
	}
	if m.Event != "stopped" || m.Reason != "breakpoint" {
		t.Fatalf("已有的字段读歪了：%+v", m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"thread", "threadId", "thread_id"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("答复里不该有线程号那一项（%s）：%s", key, b)
		}
	}
}

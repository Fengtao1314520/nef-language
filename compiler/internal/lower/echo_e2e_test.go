// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

// echo_e2e_test.go 把 samples/echo.nf 编成真产物、起起来、从另一个进程连过去跑一遍。
//
// 钉的是这一份对外样例本身：命令行参数进得来、端口拿得到、等连接的那一句真能打印出来、
// `accept` 接到的是能收能发的句柄、一个连接里能回好几趟、对方一关这一份就自己收尾退出。
// 样例写错或语言这一层退化了，这里先响。

package lower_test

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"neflang.org/nf/internal/exetest"
)

func TestSampleEchoServesGoClient(t *testing.T) {
	const srcPath = "../../../samples/echo.nf"
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("读样例失败：%v", err)
	}
	port := freePort(t)
	exe := exetest.Write(t, buildAt(t, srcPath, string(raw)), "nf-echo.exe")

	cmd := exec.Command(exe, strconv.Itoa(port))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("接不上标准输出：%v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("样例起不来：%v", err)
	}
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	// 任何一步走不下去都先杀掉这一台，不然它会留在这个测试进程外面占着端口。
	fail := func(f string, args ...any) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf(f, args...)
	}
	next := func(what string) string {
		select {
		case got := <-lines:
			return got
		case <-time.After(20 * time.Second):
			fail("%s：等不到下一行", what)
		}
		return ""
	}

	if got := next("等端口那一行"); got != "在等连接: "+strconv.Itoa(port) {
		fail("第一行要的是「在等连接: %d」，得到 %q", port, got)
	}

	c, derr := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if derr != nil {
		fail("连不上自己起的样例：%v", derr)
	}
	// 回两趟：第二趟证明收到回声之后那一条 `recv_into` 还在这个连接上等着，而不是接一次就收摊。
	for _, word := range []string{"ping-one", "ping-two"} {
		if _, werr := c.Write([]byte(word)); werr != nil {
			_ = c.Close()
			fail("送 %q 送不出去：%v", word, werr)
		}
		back := make([]byte, len(word))
		_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
		if _, rerr := io.ReadFull(c, back); rerr != nil {
			_ = c.Close()
			fail("回声 %q 没读回来：%v", word, rerr)
		}
		if string(back) != word {
			_ = c.Close()
			fail("回声不对：%q 要 %q", string(back), word)
		}
	}
	if got := next("连进来一个"); strings.TrimSpace(got) != "连进来一个" {
		_ = c.Close()
		fail("连进来之后那一句不对：%q", got)
	}
	if err := c.Close(); err != nil {
		fail("客户端这一头关不掉：%v", err)
	}
	if got := next("对方关了，收摊"); strings.TrimSpace(got) != "对方关了，收摊" {
		fail("对方关了之后那一句不对：%q", got)
	}
	// 送出收尾那一行之后这一份就该自己退出，退出码 0。
	select {
	case extra := <-lines:
		if strings.TrimSpace(extra) != "" {
			fail("收摊之后还多印了一行：%q", extra)
		}
	case <-time.After(20 * time.Second):
		fail("对方都关了，这一份还没退出")
	}
	if err := cmd.Wait(); err != nil {
		fail("样例收尾不干净：%v", err)
	}
}

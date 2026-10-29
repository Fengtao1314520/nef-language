// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package lower_test

import (
	"strings"
	"testing"
)

// TestContainerBoxingRefusedByFrontend 覆盖这条规则在后端的另一头：
// 整只容器装箱的报错出自检查那一句，不再进"后端还没实现这些"那一组。
//
// 此前它由前端放行、到发码那一步才点名，原话是"把 list<int> 装进盒子：这一条还空着，
// 没有拆开的写法"——那是一句语言规则（盒子只装 class、struct 和值类型），
// 被说成了后端少做了一块，还让 `nf check` 通过后的程序运行不起来。
func TestContainerBoxingRefusedByFrontend(t *testing.T) {
	err := compileErr("let l:list<int> = [1]\nlet o:object = l as object\nconsole.print(o)\n")
	if err == nil {
		t.Fatal("整只容器装箱该拦下来")
	}
	got := err.Error()
	if !strings.Contains(got, "装不进盒子：盒子只装 class、struct 和值类型") {
		t.Fatalf("报错不是前端那一句：%v", got)
	}
	if strings.Contains(got, "后端还没做到这些") {
		t.Fatalf("这一条已改成前端规则，不该再进后端那一组：%v", got)
	}
}

// TestBoxTwiceRefusedByFrontend 覆盖重复装箱规则的另一头：`o as object`（`o` 已声明为
// `object`）的报错出自检查那一句。此前也是检查放行、发码那一步才报"盒子转盒子这一版不发码"。
func TestBoxTwiceRefusedByFrontend(t *testing.T) {
	err := compileErr("class Bag {\n    n:int\n}\nlet o:object = new Bag(n: 1) as object\nlet p:object = o as object\nconsole.print(p)\n")
	if err == nil {
		t.Fatal("盒子再装箱该拦下来")
	}
	got := err.Error()
	if !strings.Contains(got, "不必再装箱") {
		t.Fatalf("报错不是前端那一句：%v", got)
	}
	if strings.Contains(got, "后端还没做到这些") {
		t.Fatalf("这一条已改成前端规则，不该再进后端那一组：%v", got)
	}
}

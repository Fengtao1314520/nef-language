// Copyright 2026 Feng Tao (Nate)
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// lower 阶段的结构化报错。
//
// check 的报错是带文件名、行、列的结构，编辑器里能点着走；lower 从前只把一串字符串
// 拼成一句「后端还没做到这些」，连行号都没有，于是检查过了的程序到编译才失败、屏幕上
// 又说不出位置。这里照 check.Error 的形状做一份：每一条都带位置，Run 返回的是它们的
// 集合，命令行按条打印，语言服务器也能逐条转成诊断。
//
// en: Structured errors for the lowering pass. check errors carry a file, a line and a column,
// so an editor can point at them; lowering used to join plain strings into one "the backend has
// not done these yet" sentence with no position at all. This mirrors check.Error: every entry
// has a position, Run returns a collection of them, the command line prints them one by one,
// and the language server can turn each into a diagnostic.
package lower

import (
	"errors"
	"fmt"
	"sort"

	"neflang.org/nf/internal/ast"
)

// Error 是一条降级阶段的报错，字段与 check.Error 对齐。
//
// Line 为 0 表示这是整程序一级的问题（例如包里的名字表没对齐），没有具体源码位置；
// 这类报错附加到入口文件的第一行，文案里说明是哪一层的错。
//
// en: Error is one lowering error, with the same fields as check.Error. Line 0 marks a
// whole-program problem with no source position; such an error is attached to line one of the
// entry file and says in its message which layer it came from.
type Error struct {
	File string
	Line int
	Col  int
	Msg  string
}

func (e Error) Error() string {
	if e.Line <= 0 || e.File == "" {
		return e.Msg
	}
	return fmt.Sprintf("%s 第 %d 行第 %d 列：%s", e.File, e.Line, e.Col, e.Msg)
}

// Errors 是若干条 lower 报错，按位置排序后逐条打印。
//
// en: Errors is a set of lowering errors, printed one by one after sorting by position.
type Errors []Error

func (e Errors) Error() string {
	if len(e) == 1 {
		return e[0].Error()
	}
	s := fmt.Sprintf("降级阶段有 %d 处没做出来：", len(e))
	for _, one := range e {
		s += "\n  " + one.Error()
	}
	return s
}

// atError 给一条报错补上源码位置。file 为空或 at 为 0 时退回没有位置的文案。
//
// en: atError attaches a source position to an error; an empty file or a zero position falls
// back to a message without one.
func (c *fc) atError(at ast.Pos, format string, args ...any) error {
	e := Error{File: c.filePath, Msg: fmt.Sprintf(format, args...)}
	if at.Line > 0 {
		e.Line, e.Col = at.Line, at.Col
	}
	return e
}

// asError 从一个 error 里取出结构化报错。包在 %w 里的也认得。
//
// en: asError extracts a structured error, including one wrapped with %w.
func asError(err error, dst *Error) bool {
	return errors.As(err, dst)
}

// sortErrors 按文件、行、列、文案排序。dedupe 在调用方做，这里只管顺序。
//
// en: sortErrors orders errors by file, line, column and message. De-duplication is the
// caller's job.
func sortErrors(errs []Error) []Error {
	sort.Slice(errs, func(i, j int) bool {
		a, b := errs[i], errs[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return a.Msg < b.Msg
	})
	return errs
}

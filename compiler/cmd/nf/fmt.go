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

// fmt 把一份源文件按编辑器格式化那套规则排好。格式化器只有一份——internal/ls 里
// 给 textDocument/formatting 用的那一个——这里只把它的那一条改动取出来，不另写排版。
//
// en: fmt lays out one source file with the same rules the editor uses. There is only one
// formatter, the one in internal/ls behind textDocument/formatting; this command just takes
// its single edit and writes the result, writing no layout logic of its own.
package main

import (
	"fmt"
	"os"

	"neflang.org/nf/internal/ls"
)

// cmdFmt 排一份 .nf 文件：默认把排好的文本打到标准输出，-w 时原地写回。
// 词法过不去时格式化器不动文本，这里原样输出并返回 0—— nf fmt 不是检查器。
//
// en: cmdFmt lays out one .nf file: by default the result goes to standard output, with -w
// it is written back in place. When lexing fails the formatter leaves the text alone, so the
// original is printed and the exit code stays 0 - fmt is not a checker.
func cmdFmt(args []string) int {
	write := false
	var path string
	for _, a := range args {
		switch a {
		case "-w":
			write = true
		default:
			if path != "" {
				fmt.Fprintln(os.Stderr, "nf fmt 只收一个 .nf 文件")
				return 2
			}
			path = a
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "要给一个 .nf 文件")
		return 2
	}
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	text := string(src)
	edits := ls.Formatting(path, text, ls.FormattingOptions{TabSize: 4, InsertSpaces: true})
	out := text
	if len(edits) > 0 {
		out = edits[0].NewText
	}
	if write {
		if err := os.WriteFile(path, []byte(out), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		return 0
	}
	fmt.Print(out)
	return 0
}

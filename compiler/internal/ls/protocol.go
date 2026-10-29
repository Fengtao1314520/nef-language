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

// protocol.go 只建这一截用得到的协议类型（LSP 3.17）。写全表要引一份外部定义，
// 这一份跟编译器一样不依赖任何东西，所以按需手写。
//
// en: protocol.go defines only the protocol types this slice needs (LSP 3.17). Writing the full
// schema would mean importing an external definition; like the compiler, this package depends
// on nothing, so the parts used are handwritten.
package ls

// Position 是行与列，两个数都从 0 起，列数的是 UTF-16 单元（不是字节）。
//
// en: Position is a line and column, both counted from 0, where the column counts UTF-16 units
// rather than bytes.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Diagnostic 是一条波浪线。Severity 用协议那张表的数：1 是错。
//
// en: Diagnostic is one squiggle. Severity uses the protocol's own numbers: 1 is an error.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

const SeverityError = 1

// publishDiagnostics 的参数。
//
// en: The parameters of publishDiagnostics.
type diagParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// initialize 的答复里那三样：同步方式、服务器名字与版本。
//
// en: The three things in the initialize reply: how documents sync, plus the server's name and
// version.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type syncOptions struct {
	OpenClose bool `json:"openClose"`
	Change    int  `json:"change"`
}

type capabilities struct {
	TextDocumentSync syncOptions `json:"textDocumentSync"`
	// CompletionProvider 与 HoverProvider：声明了这两条编辑器才会发那两种请求。
	CompletionProvider *completionOptions `json:"completionProvider,omitempty"`
	HoverProvider      bool               `json:"hoverProvider,omitempty"`
}

// completionOptions 是补全那一条的声明：`.` 是触发键——打到点号后面要立刻把成员列出来，
// 等编辑器自己的"单词边界"判定是指望不上的（中文名字、数字后缀都会漏）。
//
// en: completionOptions declares the completion capability: `.` is a trigger character - after
// a dot the members must be listed immediately, because the editor's own "word boundary"
// judgement cannot be relied on (it misses Chinese names and numeric suffixes).
type completionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	ResolveProvider   bool     `json:"resolveProvider"`
}

// CompletionItem 是一条补全。Kind 用协议那张表的数：2 方法、3 函数、5 字段、6 变量、
// 7 类、8 接口、13 枚举值、14 关键字。
//
// en: CompletionItem is one completion entry. Kind uses the protocol's numbers: 2 method, 3
// function, 5 field, 6 variable, 7 class, 8 interface, 13 enum member, 14 keyword.
type CompletionItem struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Kind   int    `json:"kind,omitempty"`
	// Documentation 是写在被补的那一个声明上面的注释，编辑器把它显示在弹出列表的右侧。
	Documentation string `json:"documentation,omitempty"`
}

// markup 是带格式的一段话：kind 是 "markdown" 时编辑器按 Markdown 印。
//
// en: markup is a formatted string: when kind is "markdown" the editor renders it as Markdown.
type markup struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// HoverResult 是鼠标停在名字上那一个浮窗的答复。
//
// en: HoverResult is the reply for the popup shown when the mouse rests on a name.
type HoverResult struct {
	Contents markup `json:"contents"`
}

// positionParams 是补全与悬停共用的那一种参数：哪一份文件、光标在哪。
//
// en: positionParams is the parameter shape shared by completion and hover: which file, and
// where the cursor is.
type positionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position Position `json:"position"`
}

type initResult struct {
	Capabilities capabilities `json:"capabilities"`
	ServerInfo   serverInfo   `json:"serverInfo"`
}

// 文档同步取"整份文本"这一档（协议里 change=1）：每次改动编辑器把整份推过来。
// 增量合并要按行列改缓冲区，那份钱不值得为一份几十行的程序付。
//
// en: Document sync takes the full-text option (change=1 in the protocol): the editor pushes
// the whole text on every edit. Incremental merging would need a line/column buffer editor, and
// that cost is not worth paying for a program of a few dozen lines.
const syncFull = 1

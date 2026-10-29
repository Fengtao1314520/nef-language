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

// uri.go 在编辑器那一侧的 file: URI 与检查器这一侧的斜杠路径之间来回翻。
//
// 不直接用 net/url 拼：Go 那一侧拼 Windows 路径时不把盘符后的冒号编成 %3A，
// 而 LSP 约定（以及 VS Code 的默认写法）是 file:///c%3A/…。
//
// en: uri.go converts back and forth between the editor's file: URIs and the slash-separated
// paths used on the checker's side. net/url is not used for assembling: Go does not encode the
// colon after a drive letter, while the LSP convention (and VS Code's default spelling) is
// file:///c%3A/...
package ls

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// pathFromURI 交出斜杠、不解 "./" 的那一种路径，与 check.Program 里 File.Path 同一种写法。
// 不是 file: 的（例如 Untitled:）原样当名字用：这一版只服务存在盘上的文件。
//
// en: pathFromURI returns the slash-separated, non-normalised ("./" not resolved) path, the
// same spelling as File.Path in check.Program. Something that is not a file: URI (Untitled: for
// instance) is used as-is as a name: this version only serves files that exist on disk.
func pathFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p := u.Path
	if len(p) >= 3 && p[0] == '/' && p[1] != '/' && p[2] == ':' {
		p = p[1:] // /c:/x 是 Windows 的写法，检查器那一头认 c:/x
	}
	return p
}

func uriFromPath(p string) string {
	if len(p) >= 3 && p[1] == ':' && isASCIILetter(p[0]) {
		p = "/" + p
	}
	var b strings.Builder
	b.WriteString("file://")
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '-' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c) // 按 UTF-8 的字节编，中文名字才翻得回去
	}
	return b.String()
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isEOF 认读到尽头：编辑器关掉那一侧时 stdio 上是 unexpected EOF，不是干净的 io.EOF。
//
// en: isEOF recognises reaching the end: when the editor closes its side, stdio reports
// unexpected EOF rather than a clean io.EOF.
func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

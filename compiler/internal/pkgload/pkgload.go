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

// Package pkgload 把一句 `import a from "libs/a"` 变成一份读得到的源文件。
//
// 两条路都认：`libs/a.nfpack` 那一份容器，和 `libs/a/` 那一份源文件目录。
// 容器里被吞进来的依赖在 `pkg/<名>/<版本>/` 那一层下面，容器里那一句 `libs/c` 就指到那一层，
// 版本由吞它的那一份包的 nf.json 说，不写在 import 那一行上。
//
// 读盘、开容器都只在这一侧；检查那一层（internal/check）只看路径与两张表。
//
// en: Package pkgload turns one line of `import a from "libs/a"` into a source file that can be
// read. Both shapes are accepted: the
// `libs/a.nfpack` container and the `libs/a/` source directory. Dependencies swallowed into a
// container live under `pkg/<name>/<version>/`, so a `libs/c` written inside the container
// points at that layer, and the version is decided by the nf.json of the package that swallowed
// it rather than written on the import line. Disk reads and container opening happen only on
// this side; the checker (internal/check) sees paths and two tables.
package pkgload

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/pkgfmt"
)

// container 是一份容器开出来的东西：里面那些条目与它自己的那份 nf.json。
// 同一个容器只开一次，开不成的那次也记着，免得每一句 import 再试一遍。
//
// en: container is what one container yields when opened: its entries plus its own nf.json. A
// container is opened once and a failed opening is recorded too, so no later import line tries
// the same broken file again.
type container struct {
	meta  pkgfmt.Meta
	files []pkgfmt.File
	err   error
}

// Loader 是一份装载过程自己的账。命令行一次跑用一份，编辑器每回算诊断另用一份。
//
// en: Loader is the bookkeeping of one loading pass. The command line uses one per run; the
// editor uses a fresh one each time it recomputes diagnostics.
type Loader struct {
	containers map[string]*container
	dirMetas   map[string]cachedMeta
}

func New() *Loader {
	return &Loader{containers: map[string]*container{}, dirMetas: map[string]cachedMeta{}}
}

func (l *Loader) open(containerPath string) *container {
	if c, ok := l.containers[containerPath]; ok {
		return c
	}
	meta, files, err := pkgfmt.Open(filepath.FromSlash(containerPath))
	c := &container{meta: meta, files: files, err: err}
	l.containers[containerPath] = c
	return c
}

// rootMeta 读一个包根自己的那份 nf.json：容器根就开容器，源文件目录与容器里吞进来的
// 那一层都读它们各自那一份清单（层里那一份是打包时写进去的）。
// 返回 ok 为假说的是"这一份清单读不到"，不是"这份清单没写 name"——工程根那份本来就只有 deps。
//
// en: rootMeta reads the nf.json of one package root: a container root is opened, a source
// directory and a swallowed layer inside a container each read their own manifest (the one
// inside a layer was written there by nf pack). A false ok means "this
// manifest could not be read", not "this manifest has no name" - the project root's manifest
// only ever carries deps.
func (l *Loader) rootMeta(root string) (pkgfmt.Meta, bool) {
	if strings.HasSuffix(root, pkgfmt.Suffix) {
		c := l.open(root)
		return c.meta, c.err == nil && c.meta.Name != ""
	}
	if got, ok := l.dirMetas[root]; ok {
		return got.meta, got.found
	}
	text, ok := l.ReadSource(root + "/" + pkgfmt.Manifest)
	var m pkgfmt.Meta
	if ok {
		if json.Unmarshal([]byte(text), &m) != nil {
			ok = false
		}
	} else {
		m = pkgfmt.Meta{}
	}
	l.dirMetas[root] = cachedMeta{meta: m, found: ok}
	return m, ok
}

// inContainer 说这一个包根是不是住在某一份容器里：容器本身，或者容器里吞进来的那一层
// （`libs/a.nfpack/pkg/b/1.0`）。第二层往后的依赖只能在这一份容器里找——
// 那句 `libs/c` 拿回到盘上翻工程的 `libs/`，翻到的就不是这一包要的那一份。
//
// en: inContainer says whether this package root lives inside a container: either the container
// itself, or a swallowed layer inside it (`libs/a.nfpack/pkg/b/1.0`). From the second layer
// down, dependencies can only be looked for inside that same container - taking `libs/c` back
// out to the project's own `libs/` on disk would find a different package than the one being
// asked for.
func inContainer(root string) bool {
	return strings.HasSuffix(root, pkgfmt.Suffix) || strings.Contains(root, pkgfmt.Suffix+"/")
}

type cachedMeta struct {
	meta  pkgfmt.Meta
	found bool
}

// ReadSource 读一份源文件的文字：容器里那一条从容器里取，其余从盘上读。
//
// en: ReadSource reads the text of one source file: an entry inside a container comes out of
// the container, everything else comes off the disk.
func (l *Loader) ReadSource(path string) (string, bool) {
	if i := strings.Index(path, pkgfmt.Suffix+"/"); i >= 0 {
		c := l.open(path[:i+len(pkgfmt.Suffix)])
		if c.err != nil {
			return "", false
		}
		return pkgfmt.Find(c.files, path[i+len(pkgfmt.Suffix)+1:])
	}
	src, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return "", false
	}
	return string(src), true
}

func (l *Loader) exists(path string) bool {
	_, err := os.Stat(filepath.FromSlash(path))
	return err == nil
}

// caseInsensitiveFS 说这台机器的文件系统区不区分大小写。现在只有 Windows 这一侧（nef 目前
// 只出 Windows 的产物：`cmd/nf/run_windows.go` 与后端那一套 PE 输出）。macOS 默认也不区分，等它进
// 范围时改成开一次盘上探针，别现在猜。
//
// en: caseInsensitiveFS says whether this machine's file system ignores letter case. Right now
// that is only the Windows side (nef ships Windows products only: `cmd/nf/run_windows.go` and
// the backend's PE output). macOS also ignores case by default; when it enters scope, replace this
// with a probe run once against the disk instead of guessing now.
var caseInsensitiveFS = runtime.GOOS == "windows"

// RealCase 把一条路径里每一段的写法换成磁盘上真正的那一种。
//
// 为什么需要：`import greet from "libs/greet"` 与 `import Greet from "libs/Greet"` 在不区分
// 大小写的系统上指的是同一份包，而包的身份今天是一个路径字符串（名字表按"包根 + 原名"分家）
// ——两种写法就成两份包、每个类型两个身份、那份包的顶层跑两遍，违的是"一个文件被两条路都
// 引到，只算一次"与"同一个包的顶层只跑一次"这两条语言规则。
//
// 只改写法，不改形状：进来的相对的还是相对的，带盘的照原样。某一段对不上就返回原样——
// 后面读盘那一步自己会说"找不到"。区分大小写的系统上这一条整个不走：那两头本来就是两个目录。
//
// en: RealCase rewrites every segment of a path into the spelling actually stored on disk. Why
// it is needed: on a case-insensitive system `import greet from "libs/greet"` and `import Greet
// from "libs/Greet"` mean the same package, while a package's identity is today a path string
// (the name table splits by "package root + original name") - so the two
// spellings would become two packages, every type would get two identities, and that package's
// top level would run twice, breaking the language rules "a file reached by two paths still
// counts once" and "one package's top level runs once". Only the spelling changes, not the shape: a
// relative path stays relative, a path with a drive keeps it. If one segment matches nothing,
// the original text is returned and the later disk read says "not found" itself. On a
// case-sensitive system this whole step is skipped - there the two spellings really are two
// directories.
func RealCase(p string) string {
	if !caseInsensitiveFS {
		return p
	}
	given := filepath.ToSlash(p)
	if !strings.Contains(given, "/") {
		return given
	}
	var out []string
	for i, seg := range strings.Split(given, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasSuffix(seg, ":") {
			out = append(out, seg)
			continue
		}
		parent := strings.Join(out, "/")
		if parent == "" && i > 0 {
			parent = "/" // 绝对路径的第一段在根那一层里找
		}
		if strings.HasSuffix(parent, ":") {
			parent += "/" // `C:` 单着是"C 盘的当前目录"，要的是盘根
		}
		out = append(out, matchFold(parent, seg))
	}
	return strings.Join(out, "/")
}

// matchFold 在 parent 那一层里找 seg：写法一模一样的直接要；只有一个不区分大小写对得上的，
// 用磁盘上那一种写法。一层里同时活着两个只差大小写的名字（在区分大小写的系统上才会这样）
// 不猜，原样返回。
//
// en: matchFold looks for seg inside the directory parent: an exact spelling wins; if exactly
// one entry matches ignoring case, the spelling on disk is used. When one directory holds two
// names differing only in case (possible only on a case-sensitive system), nothing is guessed
// and the segment is returned as written.
func matchFold(parent, seg string) string {
	dir := parent
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(filepath.FromSlash(dir))
	if err != nil {
		return seg
	}
	var one string
	n := 0
	for _, e := range entries {
		if e.Name() == seg {
			return seg
		}
		if strings.EqualFold(e.Name(), seg) {
			one, n = e.Name(), n+1
		}
	}
	if n == 1 {
		return one
	}
	return seg
}

// OwnerDir 从这份文件往上找最近一份 nf.json 所在的目录，找不到就是它自己那一层。
// 工程的入口文件靠它认自己的清单在哪。
//
// en: OwnerDir walks up from this file to the nearest directory holding an nf.json, and falls
// back to the file's own directory. A project's entry file uses it to find its own manifest.
func (l *Loader) OwnerDir(fromFile string) string {
	dir := filepath.ToSlash(filepath.Dir(fromFile))
	for d := dir; ; {
		if l.exists(d + "/" + pkgfmt.Manifest) {
			return d
		}
		if !strings.Contains(d, "/") || d == "" || d == "." {
			break
		}
		d = d[:strings.LastIndex(d, "/")]
	}
	return dir
}

// Resolve 把一句包引入解析成 (包根, 门面文件路径)，并登记进 prog.PkgRefs。
// root 是引它的那一份文件所属的包（工程自己的文件传空）。门面路径要交给装载那一侧接着递归。
//
// en: Resolve turns one package import into (package root, facade path) and records it in
// prog.PkgRefs. root is the package the importing file belongs to (the project's own files pass
// an empty one). The facade path is handed back to the loader, which recurses from there.
func (l *Loader) Resolve(prog *check.Program, fromFile, ref, root string) (string, string, error) {
	name := ref
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// `./` 与 `../` 起头的那一句走的是包那一种写法（不带花括号），而这一种要的路径不是相对
	// 本文件的，是 `libs/` 里那一个包。写成本地路径最容易出的就是这一句，指到"没有这份容器"
	// 上读的人只会去查容器在不在，查不到自己要改的是那个花括号。
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		return "", "", fmt.Errorf("%s：引自己工程里的文件要写 import { 名字 } from %q，不带花括号那一种是引包", fromFile, ref)
	}
	if name == "" || name == "." {
		return "", "", fmt.Errorf("%s：这一句包引入认不出是哪个包：写成 libs/包名，或者直接写包名", ref)
	}
	who := root
	if who == "" {
		who = l.OwnerDir(fromFile)
	}
	meta, hasMeta := l.rootMeta(who)
	want, hasWant := "", false
	if hasMeta {
		want, hasWant = meta.Deps[name]
	}

	// 容器里那一句：依赖就在同一个容器的 pkg/<名>/<版本>/ 那一层下面。
	// 这一条也管容器里那一层自己再往下引的依赖（a → b → c：b 住在 pkg/b/1.0 里，
	// 它要的 c 就在同一份容器的 pkg/b/1.0/pkg/c/1.0 那一层）。
	if inContainer(root) {
		if !hasWant {
			return "", "", fmt.Errorf("%s 里没有写明 %s 哪一版：引了这个包就得在它的 deps 里写一行", root+"/"+pkgfmt.Manifest, name)
		}
		layer := path.Clean(root + "/" + pkgfmt.DepPrefix(name, want))
		facade := layer + "/" + name + ".nf"
		if _, ok := l.ReadSource(facade); !ok {
			return "", "", fmt.Errorf("%s 说要 %s %s，可这份容器里没有 %s 那一层：%s", root, name, want, name, layer)
		}
		prog.SetPkgRef(fromFile, ref, layer, facade)
		return layer, facade, nil
	}

	for dir := l.OwnerDir(fromFile); ; {
		// 每一条路径都过 path.Join：从一份光名字的入口（`nf check main.nf`）起算，dir 是 "."，
		// 拼出来的是 `./libs/greet/greet.nf` 这一种写法，而那句 import 自己算出来的是
		// `libs/greet/greet.nf`（check.ResolveImportPath 折过）——两种写法指同一份文件，
		// 在包根表与名字表里就成了两个身份（"一个文件被两条路都引到，只算一次"）。
		if cp := RealCase(path.Join(dir, "libs", name+pkgfmt.Suffix)); l.exists(cp) {
			c := l.open(cp)
			if c.err != nil {
				return "", "", fmt.Errorf("%s：%v", cp, c.err)
			}
			if hasWant && c.meta.Version != want {
				return "", "", fmt.Errorf("要 %s %s，%s 里那一份是 %s", name, want, cp, c.meta.Version)
			}
			facade := RealCase(path.Join(cp, name+".nf"))
			if _, ok := l.ReadSource(facade); !ok {
				return "", "", fmt.Errorf("%s：这份容器里没有门面文件 %s", cp, name+".nf")
			}
			prog.SetPkgRef(fromFile, ref, cp, facade)
			return cp, facade, nil
		}
		if dirPkg := RealCase(path.Join(dir, "libs", name)); l.exists(dirPkg + "/" + name + ".nf") {
			pmeta, _ := l.rootMeta(dirPkg)
			if hasWant && pmeta.Version != "" && pmeta.Version != want {
				return "", "", fmt.Errorf("要 %s %s，%s 里那一份是 %s", name, want, dirPkg, pmeta.Version)
			}
			facade := RealCase(path.Join(dirPkg, name+".nf"))
			prog.SetPkgRef(fromFile, ref, dirPkg, facade)
			return dirPkg, facade, nil
		}
		up, more := parentDir(dir)
		if !more {
			break
		}
		dir = up
	}
	return "", "", fmt.Errorf("%s 这一句引的 %s 认不出：既没有 %s 这份容器，也没有带门面文件的 %s", fromFile, ref, "libs/"+name+pkgfmt.Suffix, "libs/"+name)
}

// parentDir 返回再往上那一层；到顶返回 more 为假。
//
// 相对路径的顶就是"站着跑命令的那一个目录"：`libs` 再往上什么也不是，而 `libs/shout` 正是
// 在那一层里找的——两个包做兄弟靠的就是这一层（入口写成光一个 `main.nf` 时 OwnerDir 返回 "."，
// 它旁边的 `libs/` 里放着工程自己装的每一份包）。绝对路径没有这一层：`C:` 与 `/` 再往上没有
// 东西可找，顺着盘根去翻一个 `libs/` 只会捞出与这个工程无关的目录。
//
// en: parentDir returns one level further up; at the top it returns a false more. For a
// relative path the top is "the directory the command was run from": `libs` has nothing above
// it, and `libs/shout` is exactly what gets looked for at that level - that is how two packages
// become siblings (when the entry is spelled as a bare `main.nf`, OwnerDir returns ".", and the
// `libs/` beside it holds every package the project installed). An absolute path has no such
// level: above `C:` and above `/` there is nothing to search, and following a drive root to
// find a `libs/` would turn up directories unrelated to this project.
func parentDir(d string) (string, bool) {
	if d == "" || d == "." {
		return "", false
	}
	i := strings.LastIndex(d, "/")
	switch {
	case i < 0:
		if strings.HasSuffix(d, ":") { // `C:` 单着的是"C 盘的当前目录"，不是工程的某一层
			return "", false
		}
		return "", true
	case i == 0:
		if d == "/" {
			return "", false
		}
		return "/", true
	}
	return d[:i], true
}

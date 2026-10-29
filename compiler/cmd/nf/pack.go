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

// nf pack：把一个包的源文件装进一份容器。
//
// 命令吃门面文件，产物默认是门面旁边的 `<包名>.nfpack`。门面同目录那份 nf.json 记这个包的
// 名字、版本和直接依赖。门面里 `./` 起头的引入算包内的另一份文件，一路收到收完；
// 整块挂名那一种（`import c from "libs/c"`）算依赖，从包目录旁边的 `libs/` 里取，
// 连它自己吞过的东西一并并进来，放在 `pkg/<依赖名>/<版本>/` 那一层下面。
// 花括号那一种的路径必须 ./ 或 ../ 开头，它伸不进包里，也不许当依赖收——
// 收下去的那一份容器到用它的人那儿编译不过，所以报在打包这一步。
//
// 这一步只收不判：包里的名字撞不撞、门面有没有把包内类型漏出去，是检查阶段的事。
//
// en: nf pack: put a package's source files into one container. The command takes the facade
// file and by default writes `<package name>.nfpack` beside it. The nf.json in that same
// directory records this package's name, version and direct dependencies. Inside the facade, an
// import starting with ./ counts as another file of the same package and is collected
// transitively until none is left; a whole-module named import (`import c from "libs/c"`) counts
// as a dependency, taken from the `libs/` beside the package directory, together with everything
// that dependency already swallowed, stored under the `pkg/<dependency name>/<version>/` layer.
// A brace-form path must start with ./ or ../; it cannot reach into a package and must not be
// collected as a dependency - a container that swallowed one would fail to compile at the user's
// end, so it is reported here at packing time. This step only collects, it judges nothing:
// whether names inside a package collide, and whether the facade leaks package-internal types,
// are the checker's business.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"neflang.org/nf/internal/ast"
	"neflang.org/nf/internal/check"
	"neflang.org/nf/internal/lexer"
	"neflang.org/nf/internal/parser"
	"neflang.org/nf/internal/pkgfmt"
)

// cmdPack 走 `nf pack 门面.nf [-o 容器名]`。
//
// en: cmdPack handles `nf pack facade.nf [-o container.nfpack]`.
func cmdPack(args []string) int {
	out := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "-o 后面要给一个文件名")
				return 2
			}
			out = filepath.ToSlash(args[i+1])
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "要给一份门面文件")
		return 2
	}
	entry := filepath.ToSlash(rest[0])
	if !strings.HasSuffix(entry, ".nf") {
		fmt.Fprintf(os.Stderr, "%s：门面文件的名字得是 <包名>.nf\n", rest[0])
		return 2
	}
	abs, err := filepath.Abs(entry)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	facade := filepath.ToSlash(abs)
	pkgDir := facade[:strings.LastIndex(facade, "/")]
	name := strings.TrimSuffix(filepath.Base(facade), ".nf")
	if out == "" {
		out = pkgDir + "/" + name + pkgfmt.Suffix
	}

	meta, err := pkgfmt.ReadMeta(pkgDir + "/" + pkgfmt.Manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := meta.CheckPackage(); err != nil {
		fmt.Fprintf(os.Stderr, "%s：%s\n", pkgDir+"/"+pkgfmt.Manifest, err)
		return 1
	}
	if meta.Name != name {
		fmt.Fprintf(os.Stderr, "%s 里写的名字是 %s，门面文件却是 %s：这两处得是同一个名字\n", pkgfmt.Manifest, meta.Name, filepath.Base(facade))
		return 1
	}

	files, err := packPackage(pkgDir, meta, map[string]bool{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := pkgfmt.Pack(out, meta, files); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("打包完成：%s（%d 份源文件）\n", out, len(files))
	return 0
}

// packPackage 收一个包的全部条目：自己的源文件，加上每个依赖的条目（带 pkg/ 那一层前缀）。
// seen 认的是"包名加版本"，两处依赖绕回同一个包不收第二遍。
//
// en: packPackage collects every entry of one package: its own source files plus the entries of
// each dependency, prefixed with the pkg/ layer. seen keys on "package name plus version", so
// two dependencies that lead back to the same package are collected once.
func packPackage(pkgDir string, meta pkgfmt.Meta, seen map[string]bool) ([]pkgfmt.File, error) {
	facade := pkgDir + "/" + meta.Facade()
	if _, err := os.Stat(filepath.FromSlash(facade)); err != nil {
		return nil, fmt.Errorf("%s：这个包没有门面文件 %s", pkgDir, meta.Facade())
	}

	var own []pkgfmt.File
	var deps []string
	inOwn := map[string]bool{}
	queue := []string{facade}
	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		rel := path.Clean(strings.TrimPrefix(filepath.ToSlash(file), pkgDir+"/"))
		if inOwn[rel] {
			continue
		}
		text, err := os.ReadFile(filepath.FromSlash(file))
		if err != nil {
			return nil, fmt.Errorf("%s：读不到这个文件", file)
		}
		inOwn[rel] = true
		own = append(own, pkgfmt.File{Rel: rel, Text: string(text)})

		imports, err := fileImports(file, string(text))
		if err != nil {
			return nil, err
		}
		for _, imp := range imports {
			if !check.IsLocalImportPath(imp.Path) {
				// 不带 ./ 的那一种：整块挂名才是引依赖。花括号那一种在这里收下来，
				// 交出去的那一份容器到用它的人那儿当场编译不过（检查那一侧不认这一种路径），
				// 所以要报在打包这一步——写包的人手上就有 nf.json，看到的那一句才指得准。
				if imp.Package == "" {
					return nil, fmt.Errorf("%s 第 %d 行第 %d 列：包引自己的依赖要写 import 包名 from \"libs/包名\"，这一句是花括号那一种，只引工程里的文件",
						filepath.ToSlash(file), imp.PathPos.Line, imp.PathPos.Col)
				}
				if !seen["#"+imp.Path] {
					seen["#"+imp.Path] = true
					deps = append(deps, imp.Path)
				}
				continue
			}
			target := check.ResolveImportPath(filepath.ToSlash(file), imp.Path)
			if !strings.HasPrefix(target, pkgDir+"/") {
				return nil, fmt.Errorf("%s 引了 %q，那是这个包目录外面的东西：包只能带自己的文件", rel, imp.Path)
			}
			if _, err := os.Stat(filepath.FromSlash(target)); err != nil {
				return nil, fmt.Errorf("%s 引的 %q 读不到", rel, imp.Path)
			}
			queue = append(queue, target)
		}
	}

	out := append([]pkgfmt.File(nil), own...)
	placed := map[string]string{}
	for _, f := range own {
		placed[f.Rel] = f.Text
	}
	for _, imp := range deps {
		name, ok := depName(imp)
		if !ok {
			return nil, fmt.Errorf("%s 引的 %q 认不出是哪个包：得写成 libs/包名 那样", pkgfmt.Manifest, imp)
		}
		version, ok := meta.Deps[name]
		if !ok {
			return nil, fmt.Errorf("%s 这一句引了 %q，%s 的 deps 里却没写 %s 哪一版", pkgfmt.Manifest, imp, pkgfmt.Manifest, name)
		}
		key := name + "/" + version
		if seen[key] {
			continue
		}
		seen[key] = true
		sub, subMeta, err := loadDep(pkgDir, name, version)
		if err != nil {
			return nil, err
		}
		prefix := pkgfmt.DepPrefix(name, version)
		// 依赖自己那份清单跟着进它那一层：它写的 deps 说的是"我这一版要 c 哪一版"，
		// 不带进来，用到第二层（a → b → c）就没人认得 c 是哪一版，只看得见一句"认不出这个包"。
		head, err := json.MarshalIndent(subMeta, "", "  ")
		if err != nil {
			return nil, err
		}
		manifest := pkgfmt.File{Rel: prefix + pkgfmt.Manifest, Text: string(head) + "\n"}
		for _, f := range sub {
			rel := prefix + f.Rel
			if old, ok := placed[rel]; ok {
				if old != f.Text {
					return nil, fmt.Errorf("%s 这一条有两个不同版本的内容并到了一起：%s %s", rel, name, version)
				}
				continue // 两处依赖吞过同一个包的同一份文件，不收第二遍
			}
			if rel == manifest.Rel {
				continue // 这一层那份清单由下面这一笔写，条目里只许有一条
			}
			placed[rel] = f.Text
			out = append(out, pkgfmt.File{Rel: rel, Text: f.Text})
		}
		out = append(out, manifest)
		placed[manifest.Rel] = manifest.Text
	}
	return out, nil
}

// loadDep 取一个依赖的条目。容器和源文件目录两条路都认：包作者改一行不用先打一次包。
// 返回的第二个值是那一个包自己的清单，打包那一侧要把它写进它那一层。
//
// en: loadDep takes one dependency's entries. Both a container and a source directory are
// accepted, so a package author does not have to pack first after editing one line. The second
// returned value is that package's own manifest, which the packing side writes into its layer.
func loadDep(pkgDir, name, version string) ([]pkgfmt.File, pkgfmt.Meta, error) {
	container := pkgDir + "/libs/" + name + pkgfmt.Suffix
	if _, err := os.Stat(filepath.FromSlash(container)); err == nil {
		meta, files, err := pkgfmt.Open(container)
		if err != nil {
			return nil, pkgfmt.Meta{}, err
		}
		if meta.Version != version {
			return nil, pkgfmt.Meta{}, fmt.Errorf("要 %s %s，%s 里那一份是 %s", name, version, container, meta.Version)
		}
		return files, meta, nil
	}

	dir := pkgDir + "/libs/" + name
	meta, err := pkgfmt.ReadMeta(dir + "/" + pkgfmt.Manifest)
	if err != nil {
		return nil, pkgfmt.Meta{}, fmt.Errorf("%s：这个包既没有 %s 那一份容器，也没有带 nf.json 的源文件目录 %s", name, container, dir)
	}
	if err := meta.CheckPackage(); err != nil {
		return nil, pkgfmt.Meta{}, fmt.Errorf("%s：%s", dir+"/"+pkgfmt.Manifest, err)
	}
	if meta.Name != name {
		return nil, pkgfmt.Meta{}, fmt.Errorf("%s 里写的名字是 %s，跟要的那个包 %s 不一样", dir+"/"+pkgfmt.Manifest, meta.Name, name)
	}
	if meta.Version != version {
		return nil, pkgfmt.Meta{}, fmt.Errorf("要 %s %s，%s 里那一份是 %s", name, version, dir, meta.Version)
	}
	files, err := packPackage(dir, meta, map[string]bool{})
	if err != nil {
		return nil, pkgfmt.Meta{}, err
	}
	return files, meta, nil
}

// fileImports 读一份源文件，返回它每一句 import（要路径原文，也要它是那一种写法：
// 整块挂名的是依赖，花括号的才是包内另一份文件）。
//
// en: fileImports reads one source file and returns every import line in it - both the path as
// written and which form it is: a named whole-module import is a dependency, a brace form is
// another file of the same package.
func fileImports(file, text string) ([]*ast.Import, error) {
	toks, err := lexer.Lex(text)
	if err != nil {
		var le *lexer.Error
		if errors.As(err, &le) {
			return nil, fmt.Errorf("%s 第 %d 行第 %d 列：%s", filepath.ToSlash(file), le.Line, le.Col, le.Msg)
		}
		return nil, fmt.Errorf("%s：%v", filepath.ToSlash(file), err)
	}
	f, perr := parser.Parse(filepath.ToSlash(file), toks)
	if perr != nil {
		var pe *parser.Error
		if errors.As(perr, &pe) {
			return nil, fmt.Errorf("%s 第 %d 行第 %d 列：%s", filepath.ToSlash(file), pe.Line, pe.Col, pe.Msg)
		}
		return nil, fmt.Errorf("%s：%v", filepath.ToSlash(file), perr)
	}
	return f.Imports, nil
}

// depName 从一句不带 ./ 的引入里取出包名：`libs/c` 与 `c` 都算 c。
//
// en: depName takes the package name out of an import that does not start with ./: both
// `libs/c` and `c` mean c.
func depName(p string) (string, bool) {
	name := path.Base(strings.ReplaceAll(p, `\`, "/"))
	if name == "" || name == "." || name == "/" {
		return "", false
	}
	return name, true
}

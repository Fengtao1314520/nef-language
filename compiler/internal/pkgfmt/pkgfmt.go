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

// Package pkgfmt 是包的容器形状：一份 nfpack 文件装一个包的源文件。
//
// 里面是一个 zip：一条 nf.json 记这个名字与版本，往后的每条都是一份源文件，
// 名字是包内的相对路径。门面按约定取 `<包名>.nf`，不写进清单。
//
// 用 zip 是为了零依赖：Go 的标准库自带 archive/zip。不压缩也行，
// zip 允许"只存不压"，读出来的字节跟磁盘上那份源文件一字不差，
// 报错的行列才跟程序员看到的行号对得上。
//
// en: Package pkgfmt is the container shape of a package: one nfpack file holding one package's
// source files. Inside is a zip: one nf.json records the
// name and version, and every other entry is a source file named by its path relative to the
// package. The facade is taken by convention as `<package name>.nf` and is not written into the
// manifest. zip was chosen for zero dependencies - Go's standard library brings archive/zip.
// Entries need no compression: zip allows store-only, so the bytes read back are identical to
// the file on disk, which is what keeps error line and column numbers matching what the
// programmer sees.
package pkgfmt

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Manifest 是清单的文件名。工程根一份记 deps，容器里一份记自己是谁。
//
// en: Manifest is the manifest file's name. The one at a project root records deps; the one
// inside a container records who that package is.
const Manifest = "nf.json"

// Suffix 是容器文件的后缀。
//
// en: Suffix is the file extension of a container.
const Suffix = ".nfpack"

// Meta 是一份 nf.json。工程根那一份只用 Deps。
//
// en: Meta is one nf.json. The project root's copy only uses Deps.
type Meta struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Deps    map[string]string `json:"deps"` // 包名 → 那一个版本，写死，不比大小
}

// File 是容器里的一份源文件。Rel 是包内的相对路径，用正斜杠。
//
// en: File is one source file inside a container. Rel is its path relative to the package,
// written with forward slashes.
type File struct {
	Rel  string
	Text string
}

// Dep 前缀：吞进来的依赖放在这一层下面，读的时候靠它认出"这一份是别人的包"。
//
// en: Dep prefix: swallowed dependencies are stored under this layer, and reading uses it to
// recognise "this entry is somebody else's package".
const DepDir = "pkg"

// DepPrefix 返回某个依赖在容器里那一层的路径前缀，以 / 结尾。
//
// en: DepPrefix returns the path prefix of one dependency's layer inside a container, ending
// with /.
func DepPrefix(name, version string) string {
	return DepDir + "/" + name + "/" + version + "/"
}

// ReadMeta 读磁盘上一份 nf.json。缺字段不算错（工程根那份本来就没有 name），
// 由调用方按用途再问一次 CheckPackage。
//
// en: ReadMeta reads an nf.json off the disk. A missing field is not an error (the project
// root's copy has no name at all); the caller asks again with CheckPackage according to what it
// needs the manifest for.
func ReadMeta(file string) (Meta, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return Meta{}, fmt.Errorf("%s：读不到这份清单", file)
	}
	var m Meta
	if err := json.Unmarshal(src, &m); err != nil {
		return Meta{}, fmt.Errorf("%s：这份清单不是能读的 JSON：%s", file, strings.TrimPrefix(err.Error(), "json: "))
	}
	return m, nil
}

// CheckPackage 问一份清单能不能当"一个包"的清单用：名字与版本都得写着。
//
// en: CheckPackage asks whether a manifest may serve as the manifest of a package: both a name
// and a version must be written.
func (m Meta) CheckPackage() error {
	if m.Name == "" {
		return errors.New("清单里没写 name：这一个包叫什么得写在 nf.json 里")
	}
	if m.Version == "" {
		return errors.New("清单里没写 version：这一版是几号得写在 nf.json 里")
	}
	return nil
}

// Pack 把一批源文件写成容器。out 是目标文件名，已有内容整份换掉。
//
// en: Pack writes a batch of source files into a container. out is the target file name, and
// any content already there is replaced wholesale.
func Pack(out string, m Meta, files []File) error {
	if err := os.MkdirAll(dirOf(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	seen := map[string]bool{}

	head, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := put(zw, Manifest, string(head)+"\n", seen); err != nil {
		return err
	}
	for _, src := range files {
		if err := put(zw, src.Rel, src.Text, seen); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

// put 存一条不压的条目。同一个名字出现两次就是打包那头的错，直接报出来。
//
// en: put stores one uncompressed entry. The same name appearing twice is a bug on the packing
// side, so it is reported straight away.
func put(zw *zip.Writer, name, text string, seen map[string]bool) error {
	name = path.Clean(strings.TrimPrefix(name, "/"))
	if seen[name] {
		return fmt.Errorf("容器里 %s 这一条重复了", name)
	}
	seen[name] = true
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}

// Open 读出容器的清单与源文件。条目里的 nf.json 返回在 Meta 里，不进 files。
//
// en: Open reads a container's manifest and source files. The nf.json entry comes back in Meta
// and is not put into files.
func Open(container string) (Meta, []File, error) {
	zr, err := zip.OpenReader(container)
	if err != nil {
		return Meta{}, nil, fmt.Errorf("%s：读不开这个容器", container)
	}
	defer zr.Close()
	var m Meta
	var out []File
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			return Meta{}, nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return Meta{}, nil, err
		}
		name := path.Clean(zf.Name)
		if name == Manifest {
			if err := json.Unmarshal(data, &m); err != nil {
				return Meta{}, nil, fmt.Errorf("%s 里的 %s 不是能读的 JSON", container, Manifest)
			}
			continue
		}
		out = append(out, File{Rel: name, Text: string(data)})
	}
	if m.Name == "" {
		return Meta{}, nil, fmt.Errorf("%s 里没有 %s，认不出这是哪一个包", container, Manifest)
	}
	return m, out, nil
}

// Facade 返回这个包的门面条目名：`<包名>.nf`（开发期在源文件目录里也是这个名字）。
//
// en: Facade returns this package's facade entry name: `<package name>.nf` - the same name
// during development, where the package is a source directory.
func (m Meta) Facade() string {
	return m.Name + ".nf"
}

// Find 在一批条目里按包内相对路径取一份源文件；找不到返回 ok 为假。
//
// en: Find takes one source file out of a batch of entries by its path relative to the package;
// when nothing matches it returns a false ok.
func Find(files []File, rel string) (string, bool) {
	want := path.Clean(strings.TrimPrefix(strings.ReplaceAll(rel, `\`, "/"), "/"))
	for _, f := range files {
		if f.Rel == want {
			return f.Text, true
		}
	}
	return "", false
}

func dirOf(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "."
	}
	return p[:i]
}

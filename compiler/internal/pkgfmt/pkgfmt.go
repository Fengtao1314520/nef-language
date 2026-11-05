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
// 里面是一个 zip：一条 nf.json 记这个名字与版本，还有每一条源文件的字节数与 SHA-256；
// 往后的每条都是一份源文件，名字是包内的相对路径。
// 门面按约定取 `<包名>.nf`，不写进清单。
//
// 校验值是为了能查出"装进 libs/ 的那一份是不是打包时那一份"：加载的时候逐条核对，
// 对不上就报出是哪一条、期望与实际各是什么。nf.json 自己算不进清单——一份文件核对不了自己。
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
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

// Meta 是一份 nf.json。工程根那一份只用 Deps；容器里那一份还带着 Files 那一串校验值。
//
// en: Meta is one nf.json. The project root's copy only uses Deps; the copy inside a container
// also carries the Files checksum list.
type Meta struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Deps    map[string]string `json:"deps"` // 包名 → 那一个版本，写死，不比大小
	// Files 是容器里每一条源文件的校验值：名字、字节数、SHA-256。`nf pack` 写进去，
	// 加载包的那一步逐条核对（见 Verify）。工程根那一份没有这一串。
	//
	// en: Files is the checksum list of every source entry in the container: name, byte count and
	// SHA-256. nf pack writes it and loading checks each line against it (see Verify). A project
	// root's manifest carries none of it.
	Files []Entry `json:"files,omitempty"`
}

// Entry 是校验值清单上的一条。
//
// en: Entry is one line of the checksum list.
type Entry struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
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
// 清单里除了名字与版本，还替每一条源文件记下字节数与 SHA-256：加载那一步逐条核对（见 Verify）。
//
// en: Pack writes a batch of source files into a container. out is the target file name, and any
// content already there is replaced wholesale. Besides the name and version, the manifest records
// a byte count and a SHA-256 for every source entry; loading checks each one (see Verify).
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

	entries := make([]Entry, 0, len(files))
	for _, src := range files {
		entries = append(entries, noteOf(src.Rel, src.Text))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	m.Files = entries

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

// noteOf 替一条源文件算出清单上那一行。
//
// en: noteOf computes one manifest line for a source entry.
func noteOf(rel, text string) Entry {
	sum := sha256.Sum256([]byte(text))
	return Entry{Name: entryName(rel), Bytes: len(text), SHA256: hex.EncodeToString(sum[:])}
}

// entryName 把写出来的路径落成容器里那一条的名字，与 put 用的是同一个算法。
//
// en: entryName turns a written path into the entry name inside the container - the same
// calculation put uses.
func entryName(rel string) string {
	return path.Clean(strings.TrimPrefix(rel, "/"))
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
			return Meta{}, nil, fmt.Errorf("%s：%s 这一条读不出来：%v", container, path.Clean(zf.Name), err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			// zip 自己那一份 CRC 先报错：这一条说明"容器里这一条字节被改过"，
			// 说的必须是哪一条，不能只把 zip 那句英文交出去。
			//
			// en: the zip's own CRC trips first, and it says only "a byte changed here" - what the
			// reader needs is which entry, so that line is wrapped instead of passed through.
			return Meta{}, nil, fmt.Errorf("%s：%s 这一条与打进去的字节对不上（这一条被改过）：%v", container, path.Clean(zf.Name), err)
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
	if err := verify(container, m, out); err != nil {
		return Meta{}, nil, err
	}
	return m, out, nil
}

// verify 逐条核对容器里的源文件与清单上记的那一行：清单上没记的、记了却不在容器里的、
// 字节数与校验值对不上的，各报一句，说的都是哪一条。
//
// en: verify checks every source entry against the line written for it: one the list does not
// carry, one the list carries but the container lacks, and a byte count or checksum that does not
// match - each says which entry it is.
func verify(container string, m Meta, files []File) error {
	if len(m.Files) == 0 {
		return fmt.Errorf("%s：这份容器没有校验值清单（%s 里的 files）：用现在的 nf pack 重新打过一次", container, Manifest)
	}
	want := make(map[string]Entry, len(m.Files))
	for _, e := range m.Files {
		want[e.Name] = e
	}
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f.Rel] = true
		e, ok := want[f.Rel]
		if !ok {
			return fmt.Errorf("%s：%s 这一条没有记在清单上：用现在的 nf pack 重新打过这份包", container, f.Rel)
		}
		if e.Bytes != len(f.Text) {
			return fmt.Errorf("%s：%s 这一条与清单对不上：字节数要 %d、得到 %d", container, f.Rel, e.Bytes, len(f.Text))
		}
		if got := noteOf(f.Rel, f.Text).SHA256; got != e.SHA256 {
			return fmt.Errorf("%s：%s 这一条与清单对不上：校验值要 %s、得到 %s", container, f.Rel, e.SHA256, got)
		}
	}
	for _, e := range m.Files {
		if !seen[e.Name] {
			return fmt.Errorf("%s：清单上记的 %s 在容器里没有这一条：用现在的 nf pack 重新打过这份包", container, e.Name)
		}
	}
	return nil
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

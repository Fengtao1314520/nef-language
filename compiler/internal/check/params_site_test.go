// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

// params_site_test.go 校验站上 `site/stdlib/` 那十四页表格里"写法"那一列与 params.go 这张表一致：
// 参数名一律照 `params.go` 的 stdParamNames，站上不另写一套中文占位。
//
// 这条测试覆盖的是：`params.go` 是签名的唯一出处，屏幕上（补全与悬停）那一行由它拼出来，
// 网页上那一行如果各写各的，照着网页写的人就会与编辑器对不上。
// 站上原先有 149 处中文占位（`json.parse(文本)`、`bytes.new(容量)`、
// `console.print(值, 值, …)`），全部照这张表替换过，这条测试固定替换后的结果。
//
// 站上把成员写成 `s.trim()`、`Socket.send(data)`、`xs.add(value)` 这一种形状：接收者叫什么不算，
// 落在哪一张表上才算。所以每一页带着它管的那些前缀（下面 siteParamShapes），按"前缀 + 动作名"查这张表。
// 括号里写成实参的那些单元格（`math.floor(-3.9)`、`text.to_int("三十")`）是举例，不参与比对。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// siteParamShapes 是每一页管着的前缀。`044_index.md` 与 `049_containers.md` 不在这里：
// 前者是总览，后者那张表是"作用 | list | dict | hashset"四列横着排的，
// 由 docs_site_test.go 按列拼出落点名去比那一句说明。
var siteParamShapes = map[string][]string{
	"045_console.md":            {"console"},
	"046_text.md":               {"text", "string", "number", "length"},
	"047_bytes.md":              {"bytes"},
	"048_number.md":             {"number", "math"},
	"050_file.md":               {"file"},
	"051_path.md":               {"path"},
	"052_environment.md":        {"environment"},
	"053_process.md":            {"process"},
	"054_time.md":               {"time"},
	"055_regular_expression.md": {"regular_expression"},
	"056_json.md":               {"json", "dict"},
	"057_net.md":                {"net", "socket", "listener", "bytes"},
	"058_http.md":               {"http"},
}

// siteArgNames 认括号里那一段是不是"参数名"的样子：小写字母、数字与下划线，
// 可以带一个默认值（`timeout_ms = 3000`），变长写成 `...values`。
func siteArgNames(s string) ([]string, bool) {
	if strings.TrimSpace(s) == "" {
		return nil, false
	}
	var out []string
	for _, piece := range strings.Split(s, ",") {
		piece = strings.TrimSpace(piece)
		if i := strings.IndexByte(piece, '='); i >= 0 {
			piece = strings.TrimSpace(piece[:i])
		}
		piece = strings.TrimPrefix(piece, "...")
		if piece == "" {
			return nil, false
		}
		for _, r := range piece {
			if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return nil, false
			}
		}
		out = append(out, piece)
	}
	return out, true
}

// siteParamKey 把站上一个调用形状换成这张表里的落点名。
func siteParamKey(prefixes []string, shape string) (string, bool) {
	if _, ok := stdParamNames[shape]; ok && strings.IndexByte(shape, '.') < 0 {
		return shape, true // `length` 这种只有一个名字的
	}
	_, tail, hasDot := strings.Cut(shape, ".")
	if !hasDot {
		return "", false
	}
	for _, p := range prefixes {
		if k := p + "." + tail; stdParamNames[k] != nil {
			return k, true
		}
	}
	return "", false
}

func TestSiteSignaturesUseTheParamNames(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "site", "stdlib")
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("列出站上标准库那十四页失败：%v", err)
	}
	sort.Strings(files)
	written := 0
	total := 0
	for _, f := range files {
		base := filepath.Base(f)
		prefixes, ok := siteParamShapes[base]
		if !ok {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s 失败：%v", base, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "|") {
				continue
			}
			// 只比第一个单元格：那一格写的是"要这样写"，说明那一格里举例用的写法不算签名
			cells := splitRow(line)
			if len(cells) == 0 {
				continue
			}
			for _, cell := range cells[:1] {
				for _, piece := range strings.Split(cell, "、") {
					piece = strings.TrimSpace(piece)
					open := strings.IndexByte(piece, '(')
					if open <= 0 || !strings.HasSuffix(piece, ")") {
						continue
					}
					shape, args := piece[:open], piece[open+1:len(piece)-1]
					key, ok := siteParamKey(prefixes, shape)
					if !ok {
						continue
					}
					total++
					got, ident := siteArgNames(args)
					if !ident {
						continue // 举例用的实参，不是签名
					}
					want := stdParamNames[key]
					written++
					if strings.Join(got, ",") != strings.Join(want, ",") {
						t.Errorf("%s 第 %d 行 %s(%s)：params.go 上这一条的参数名是 (%s)",
							base, i+1, key, args, strings.Join(want, ", "))
					}
				}
			}
		}
	}
	if written < 80 {
		t.Fatalf("只比对了 %d 处签名，站上那十三页大概没读进去（认出 %d 处形状）", written, total)
	}

	// 反过来：这张表里的每一条动作，站上那十四页里至少写到一次
	all, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("列出站上标准库那十四页失败：%v", err)
	}
	var text strings.Builder
	for _, f := range all {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s 失败：%v", f, err)
		}
		text.Write(raw)
	}
	body := text.String()
	for key, want := range stdParamNames {
		if strings.Contains(body, key+"(") {
			continue
		}
		_, tail, _ := strings.Cut(key, ".")
		if strings.Contains(body, "."+tail+"(") {
			continue
		}
		t.Errorf("params.go 上的 %s（参数 (%s)）在站上那十四页里一处都没写到", key, strings.Join(want, ", "))
	}
	t.Logf("站上认到 %d 处调用形状，逐字比对 %d 处；params.go 的 %d 条动作在站上都有对应的一行", total, written, len(stdParamNames))
}

// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 站上 `site/stdlib/` 那十四页里"说明"那一句与这张表的 Zh 必须是同一句话：站上那一句要原样出现在
// 表里这一句里。表里可以多带一层细节，不可以换一种说法——换了就等于屏幕上与网页上说法不一致，
// 而这张表现在是内建动作说明文字的出处。
//
// 站上两种表都要识别：一行一个动作的那种（045–048、050–058）与三种容器合在一张上对照的那种（049）。
// 一个单元格并列写两个动作的那种（`s.to_upper()`、`s.to_lower()` 共用一句"转换大小写"）不进逐句比对：
// 那一句同时说两件事，落到单独一个动作上不成立。这种动作只要求站上那一页写到过它。
func TestStdDocZhCarriesTheSiteSentence(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "site", "stdlib")
	say, present, err := siteSentences(dir)
	if err != nil {
		t.Fatalf("读站上那十四页失败：%v", err)
	}
	if len(say) < 90 || len(present) < 140 {
		t.Fatalf("从站上只抽出 %d 句、只认到 %d 个动作，抽法大概错了", len(say), len(present))
	}
	merged := 0
	for _, k := range StdDocKeys() {
		if k == "length" {
			// 长度这一个写法在站上按四种形状各写一句（码点个数、有多少个字节……），
			// 表里那句是合起来说的，不参与逐句比对
			continue
		}
		s, ok := say[k]
		if !ok {
			if !present[k] {
				t.Errorf("%s 在站上的表里一行都没有", k)
			}
			merged++
			continue
		}
		if !strings.Contains(cleanCell(stdDocs[k].Zh), cleanCell(s)) {
			t.Errorf("%s 两边不是同一句\n站上：%q\n表里：%q", k, s, stdDocs[k].Zh)
		}
	}
	if merged > 0 {
		t.Logf("可比对 %d 句，与另一个动作并列在同一行里、只登记写到过的 %d 个", len(say), merged)
	}
}

// siteShapeColumns 是容器那一张表里各列对应的形状：作用 | list | dict | hashset。
var siteShapeColumns = []string{"list", "dict", "hashset"}

// siteShortNames 是"一行一个动作"那种表里短接收者对应的形状：站上写 `s.trim()`、`b.slice()`、`p.wait()`。
var siteShortNames = map[string]string{
	"s": "string",
	"b": "bytes",
	"p": "process",
}

// siteSentences 抽出每个落点名对应的那一句说明，另外给一张"这个动作在站上写到过"的表。
func siteSentences(dir string) (map[string]string, map[string]bool, error) {
	say := map[string]string{}
	present := map[string]bool{}
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		container := strings.Contains(filepath.Base(f), "containers")
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "|") {
				continue
			}
			cells := splitRow(line)
			if len(cells) < 3 {
				continue
			}
			if container {
				for i, col := range cells[1:] {
					if i >= len(siteShapeColumns) {
						break
					}
					for _, name := range memberNames(col) {
						key := siteShapeColumns[i] + "." + name
						present[key] = true
					}
					if len(memberNames(col)) == 1 {
						setFirst(say, siteShapeColumns[i]+"."+memberNames(col)[0], cells[0])
					}
				}
				continue
			}
			shapes := callShapes(cells[0])
			if len(shapes) == 0 {
				continue
			}
			sentence := cells[len(cells)-1]
			if len(sentence) < 6 {
				continue
			}
			for _, sh := range shapes {
				if key, ok := siteKey(sh); ok {
					present[key] = true
				}
			}
			if len(shapes) == 1 {
				if key, ok := siteKey(shapes[0]); ok {
					setFirst(say, key, sentence)
				}
			}
		}
	}
	return say, present, nil
}

// splitRow 把一行 markdown 表格拆成各个单元格：去掉首尾的竖线，去掉行内代码的反引号，空的单元格与分隔行丢掉。
func splitRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ReplaceAll(p, "`", ""))
		if p == "" || strings.Trim(p, "-: ") == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// callShapes 取一个单元格里写着的每一个调用形状，只留括号之前那一段名字。
func callShapes(cell string) []string {
	var out []string
	for _, piece := range strings.Split(cell, "、") {
		piece = strings.TrimSpace(piece)
		i := strings.IndexByte(piece, '(')
		if i <= 0 {
			continue
		}
		out = append(out, piece[:i])
	}
	return out
}

// memberNames 只取成员名（点号后面那一段），给容器那一张表按列拼落点名用。
func memberNames(cell string) []string {
	var out []string
	for _, shape := range callShapes(cell) {
		i := strings.LastIndexByte(shape, '.')
		if i < 0 {
			continue
		}
		out = append(out, shape[i+1:])
	}
	return out
}

// siteKey 把站上写的调用形状换成这张表的落点名：组名打全的那种直接用（`console.print`、`text.join`），
// 类型名打全的那种换成小写形状名（`Socket.close()` 换成 `socket.close`），`length` 本来就只有一个名字，
// 剩下的短接收者按 siteShortNames 映射。
func siteKey(shape string) (string, bool) {
	if shape == "length" {
		return "length", true
	}
	head, tail, ok := strings.Cut(shape, ".")
	if !ok {
		return "", false
	}
	if IsGroup(head) || head == "text" || head == "number" {
		return head + "." + tail, true
	}
	if strings.ToUpper(head[:1]) == head[:1] {
		return strings.ToLower(head) + "." + tail, true
	}
	if g, isShort := siteShortNames[head]; isShort {
		return g + "." + tail, true
	}
	return "", false
}

func setFirst(out map[string]string, key, sentence string) {
	if _, ok := out[key]; ok {
		return
	}
	out[key] = firstSentence(sentence)
}

// firstSentence 取到第一个句号为止；站上有些说明写得很长，这里只比那第一句。
// 句号是三个字节的一个汉字标点，切的时候要把整个切进来，不然抽出的句子尾上是断开的半个字节。
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "。"); i >= 0 {
		return s[:i+len("。")]
	}
	return s
}

// cleanCell 去掉比对时不该参与比对的内容：反引号、空白、粗体记号，页内链接
// （`[字节块](/stdlib/047_bytes)` 在屏幕上只会是一串无用的路径，表里写的是"字节块"三个字），
// 以及"（见下文某一节）"这种只有站在那一页上才成立的指路。
func cleanCell(s string) string {
	s = stripLinks(s)
	s = pagePointers.ReplaceAllString(s, "")
	r := strings.NewReplacer("`", "", " ", "", "\t", "", "　", "", "**", "")
	return r.Replace(s)
}

// markdownLinks 匹配 [文字](地址) 这一种链接写法。
var markdownLinks = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// pagePointers 匹配"（见下文“随机数”一节）"这一种页内指路：屏幕上的悬停没有"下文"可看。
var pagePointers = regexp.MustCompile(`（见[^）]*）`)

// stripLinks 把页内链接收成那个文字本身，反复做直到没有链接为止（一个单元格里可能有两个）。
func stripLinks(s string) string {
	for {
		m := markdownLinks.FindStringSubmatchIndex(s)
		if m == nil {
			return s
		}
		text := s[m[2]:m[3]]
		s = s[:m[0]] + text + s[m[1]:]
	}
}

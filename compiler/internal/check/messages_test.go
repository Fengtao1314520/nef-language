// Copyright 2026 Feng Tao (Nate). Apache-2.0. See LICENSE.

package check

// 报错文字清单：编译器与运行期对用户说的每一句话，一条一行存在 testdata 里。
//
// 这条测试覆盖的是：`expectErr` 只要求"报错里出现我写的这一段"，所以一句话改了一半、
// 留下的那半还在，测试照样通过。升 v1.0.0 要的是"改一个字就有人发现"，这一条就是那个人。
//
// 做法：从源码里收集写死的报错句子，与清单逐条比对。多了、少了、改了字，都当场红，
// 并把两边各是哪些句子打印出来。
//
// 怎么更新清单：go test ./internal/check -run TestErrorMessageList -update
// 更新之前先想清楚：报错说法改了就是一轮 MINOR。
//
// 三种不成"调用"的写法各归各的规矩：句子先建好再递进来的（c.fail(位置, "%s", msg)）看
// messageFuncs 那张表；填进 Error{Msg: …} 那个位置的跟着收；叫 Error 的那个方法是
// "文件 第几行第几列：那句话"的打印框架，不收。新增这类写法时要把名字加进 messageFuncs，
// 否则清单收不到，而这条检查不会告诉你它漏了。

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var updateList = flag.Bool("update", false, "把源码里收集到的句子写回清单")

// messageRule 是一种"这里出现的字符串是一句对用户说的话"。
// at 是那句话在第几个参数上；onlyIn 非空就只在某个目录下生效；
// through 非空就要求调用写在这个包名后面（`fmt.Errorf`、`errors.New`）。
type messageRule struct {
	name    string
	at      int
	onlyIn  string
	through string
}

var messageRules = []messageRule{
	{name: "fail", at: 1},                                  // c.fail(位置, "……")
	{name: "err", at: 0, onlyIn: "internal/parser"},        // p.err("……")
	{name: "strObject", at: 0, onlyIn: "internal/runtime"}, // 运行期那句消息
	{name: "Errorf", at: 0, through: "fmt"},                // fmt.Errorf("……")
	{name: "New", at: 0, through: "errors"},                // errors.New("……")
}

// messageFuncs 是"先建好句子再交出去"的那几个小函数：调用点只写 c.fail(位置, "%s", msg)，
// 句子写在它们自己的 return 里，所以按函数名收一遍。新增这类写法要把名字加进来。
var messageFuncs = map[string]bool{"badName": true, "switchable": true}

func TestErrorMessageList(t *testing.T) {
	root := compilerRoot(t)
	got := collectMessages(t, root)

	listPath := filepath.Join(root, "testdata", "报错文字.txt")
	if *updateList {
		var b strings.Builder
		b.WriteString("# 编译器与运行期对用户说的每一句话，一条一行。由测试生成，别手改。\n")
		b.WriteString("# 更新：go test ./internal/check -run TestErrorMessageList -update\n")
		for _, m := range got {
			b.WriteString(strconv.Quote(m) + "\n")
		}
		if err := os.WriteFile(listPath, []byte(b.String()), 0o644); err != nil {
			t.Fatalf("写不进清单 %s：%v", listPath, err)
		}
		t.Logf("清单已更新：%d 句", len(got))
		return
	}

	want := readList(t, listPath)
	extra, missing := diffMessages(got, want)
	if len(extra) == 0 && len(missing) == 0 {
		if len(got) == 0 {
			t.Fatalf("一句报错文字都没收集到——八成是收集规则失效了")
		}
		return
	}

	t.Errorf("报错文字与清单对不上（清单 %s，共 %d 句；源码里 %d 句）",
		filepath.ToSlash(listPath), len(want), len(got))
	for _, m := range extra {
		t.Errorf("  源码里有、清单上没有：%s", m)
	}
	for _, m := range missing {
		t.Errorf("  清单上有、源码里没有：%s", m)
	}
	t.Errorf("  改一句报错说法就是一轮 MINOR，先记下这一改，" +
		"再跑 go test ./internal/check -run TestErrorMessageList -update")
}

// compilerRoot 从测试的工作目录（internal/check）往上找到 compiler 那一级。
func compilerRoot(t *testing.T) string {
	t.Helper()
	here, err := os.Getwd()
	if err != nil {
		t.Fatalf("拿不到工作目录：%v", err)
	}
	root := filepath.Clean(filepath.Join(here, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s 里没有 go.mod，收集范围找不到", root)
	}
	return root
}

// collectMessages 走一遍源码，按 messageRules 与两处补充写法收集每一句，去重后按文字排序。
func collectMessages(t *testing.T, root string) []string {
	t.Helper()
	files := goFiles(t, root)
	consts := stringConsts(files)
	messageFuncsLeft := map[string]bool{}
	for name := range messageFuncs {
		messageFuncsLeft[name] = true
	}

	set := map[string]bool{}
	add := func(msg string) {
		if strings.TrimSpace(msg) == "" || placeholderOnly(msg) {
			return
		}
		if strings.Contains(msg, "%w") {
			return // 那是把另一句包装进来的写法，自己不是一句话
		}
		set[msg] = true
	}

	for _, f := range files {
		for _, call := range callExprs(f) {
			rule, ok := matchRule(f.rel, call)
			if !ok {
				continue
			}
			if rule.at >= len(call.Args) {
				t.Errorf("%s 那一处调用的参数不够：%s 第 %d 个参数不存在",
					f.rel, rule.name, rule.at)
				continue
			}
			msg, ok := messageText(call.Args[rule.at], consts)
			if !ok {
				// 走到这里说明这一处既不是写死的文字、也不是一个查得到值的常量：
				// 清单收不到它，得改成常量，或把建句子的小函数加进 messageFuncs。
				t.Errorf("%s:%d 那一句不是写死的文字，清单收不到它：%s",
					f.rel, f.position(call.Pos()).Line, rule.name)
				continue
			}
			add(msg)
		}
		for _, lit := range errorLiterals(f, consts) {
			add(lit)
		}
		for _, decl := range f.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				delete(messageFuncsLeft, fn.Name.Name)
			}
		}
	}

	// messageFuncs 里写了名字却没有那个函数：那张表过期了，会有句子悄悄漏掉。
	for name := range messageFuncsLeft {
		t.Errorf("messageFuncs 里的 %s 在源码里找不到：这个检查收不到它，把名字改对或者删掉", name)
	}

	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

type goFile struct {
	rel  string // 相对 compiler 的路径，用正斜杠
	file *ast.File
	fset *token.FileSet
}

func (g goFile) position(p token.Pos) token.Position { return g.fset.Position(p) }

// goFiles 解析 compiler 下面所有非测试的 .go 文件。
func goFiles(t *testing.T, root string) []goFile {
	t.Helper()
	var out []goFile
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == "testdata" || name == ".git" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Errorf("%s 读不了，收集就不可信：%v", filepath.ToSlash(rel), err)
			return nil
		}
		out = append(out, goFile{rel: filepath.ToSlash(rel), file: file, fset: fset})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("走一遍源码就失败了：%v", walkErr)
	}
	if len(out) == 0 {
		t.Fatalf("%s 下面一个 .go 文件都没找到", root)
	}
	return out
}

// stringConsts 收全部包里的字符串常量，供 c.fail(at, bareNullMsg) 那一类写法查值。
func stringConsts(files []goFile) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) == 0 {
					continue
				}
				if v, ok := messageText(vs.Values[0], out); ok {
					for _, name := range vs.Names {
						out[name.Name] = v
					}
				}
			}
			return true
		})
	}
	return out
}

// callExprs 交出这一份文件里所有的调用式。
func callExprs(f goFile) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(f.file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			out = append(out, call)
		}
		return true
	})
	return out
}

// errorLiterals 交出两种不成"调用"的写法里那些句子：
// 一条是 messageFuncs 那类小函数 return 出去的文字，
// 一条是 Error{Msg: …} 那种把句子填进 Msg 那个位置的做法。
// 两处都认 fmt.Sprintf 的第一个参数，因为句子常常是它建起来的。
// 叫 Error 的那个方法跳过：它是"文件 第几行第几列：那句话"那个打印框架，不是给用户看的一句报错。
func errorLiterals(f goFile, consts map[string]string) []string {
	var out []string

	// 认一句写死的文字：字面量、常量、拼起来的、fmt.Sprintf 或 fmt.Errorf 的头一个参数。
	// 带 %w 的跳过：那一句是把另一句包装进来，自己不算一句话。
	var text func(ast.Expr) (string, bool)
	text = func(e ast.Expr) (string, bool) {
		if call, ok := e.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "fmt" &&
					(sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Errorf") && len(call.Args) > 0 {
					s, ok := text(call.Args[0])
					if !ok || strings.Contains(s, "%w") {
						return "", false
					}
					return s, true
				}
			}
			return "", false
		}
		return messageText(e, consts)
	}

	for _, decl := range f.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.Name == "Error" {
			continue
		}
		wanted := messageFuncs[fn.Name.Name]
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ReturnStmt:
				if !wanted {
					return true
				}
				for _, e := range x.Results {
					if s, ok := text(e); ok {
						out = append(out, s)
					}
				}
			case *ast.CompositeLit:
				for _, elt := range x.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Msg" {
						continue
					}
					if s, ok := text(kv.Value); ok {
						out = append(out, s)
					}
				}
			}
			return true
		})
	}
	return out
}

// matchRule 看这一处调用是不是 messageRules 里那一条。
//
// 带句点的（`c.fail(位置, "…")`、`fmt.Errorf("…")`）与只有名字的（`strObject("…")`）都要识别：
// 只认前一种，运行期那三十来句就成了清单上的空白。
// 因此 onlyIn 收紧到各自那个目录，免得谁的 `New()` 也被当成一句报错。
func matchRule(rel string, call *ast.CallExpr) (messageRule, bool) {
	var name, through string
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fn.Sel.Name
		if id, ok := fn.X.(*ast.Ident); ok {
			through = id.Name
		}
	case *ast.Ident:
		name = fn.Name
	default:
		return messageRule{}, false
	}
	for _, r := range messageRules {
		if r.name != name {
			continue
		}
		if r.onlyIn != "" && !strings.HasPrefix(rel, r.onlyIn+"/") {
			continue
		}
		if r.through != "" && r.through != through {
			continue
		}
		return r, true
	}
	return messageRule{}, false
}

// placeholderOnly 判的是"这段文字除了一些占位符之外什么都没有"——那不是句子，
// 是从别处建好递进来的消息（c.fail(位置, "%s", msg)），那种写法看 messageFuncs 那张表。
func placeholderOnly(s string) bool {
	r := []rune(s)
	out := make([]rune, 0, len(r))
	for i := 0; i < len(r); i++ {
		if r[i] == '%' && i+1 < len(r) {
			i++ // %s、%q、%T 这一类，跳过去
			continue
		}
		out = append(out, r[i])
	}
	return strings.TrimSpace(string(out)) == ""
}

// messageText 把一句话读成文字：写死的字符串、拼起来的常量、常量名都算。
// 返回的仍是带 %s 的位置串（源码里怎么写就怎么记）。
func messageText(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, ok := messageText(v.X, consts)
		if !ok {
			return "", false
		}
		right, ok := messageText(v.Y, consts)
		if !ok {
			return "", false
		}
		return left + right, true
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.ParenExpr:
		return messageText(v.X, consts)
	}
	return "", false
}

func readList(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到清单 %s：%v——先跑 go test ./internal/check -run TestErrorMessageList -update", path, err)
	}
	var out []string
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, err := strconv.Unquote(line)
		if err != nil {
			t.Fatalf("清单里这一行读不成字符串：%s", line)
		}
		out = append(out, s)
	}
	return out
}

func diffMessages(got, want []string) (extra, missing []string) {
	inGot := map[string]bool{}
	inWant := map[string]bool{}
	for _, m := range got {
		inGot[m] = true
	}
	for _, m := range want {
		inWant[m] = true
	}
	for _, m := range got {
		if !inWant[m] {
			extra = append(extra, m)
		}
	}
	for _, m := range want {
		if !inGot[m] {
			missing = append(missing, m)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return extra, missing
}

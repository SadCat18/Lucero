package test

// T-431：契约① §3 规则点名的那几个「包.符号」，今天到底有没有被生产码走到过 —— 机械判据。
//
// 为什么要有这一条（派发理由）：§3.0-c 那张现状表是**手数的**，手数的数会烂。
// 所以本文件的输入**只有契约**：符号从 `specs/contracts-internal-v0.1.md` §3.0-c 那五行现读，
// **脚本里不内嵌任何清单快照**（09-2x 立的那条：validator 不许带契约快照 —— 带快照的校验器
// 会在契约改完之后继续判旧世界是对的）。
//
// 口径三条，按契约 §3.0-c 自己那句「非测试文件、并排除定义它自己的那个包」：
// 　① 断的是**调用点**（AST 里的 SelectorExpr 调用），不是"有没有人提这个字"、也不是"有没有写口" ——
//    三种计数不等价；本条只断调用点，别把它读成"没人用这张表"。
// 　② 「符号在盘上不存在」与「存在但零调用点」是**两种红**，分开报（巡检 T-431 附的那枚坑：
//    断一个不存在的符号永远是 0 调用点，看上去像"没人用"，其实"根本没这个符号"）。
// 　③ 表读不到（改名／挪走／换形状）就停 —— 一条什么都看不见的判据不许报绿。
//
// 换树取证用 LUCERO_WIRING_ROOT（只换**代码侧**扫描根，契约那一侧不换）：
// 正向对照与反例都在 .scratch 的 HEAD 副本里做，真实产品码一个字节不动。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type wiringRow struct {
	symbol string // 契约点名的全名，如 permission.Engine.Decide
	defLoc string // 「定义位」那一格的原话
}

// parseWiringTable 从契约里读 §3.0-c 那张表 —— 唯一输入源。
func parseWiringTable(t *testing.T) []wiringRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "specs", "contracts-internal-v0.1.md"))
	if err != nil {
		t.Fatalf("契约① 读不到：%v ⇒ 这条判据没有输入源，不算通过", err)
	}
	ls := strings.Split(string(raw), "\n")
	hdr := -1
	for i, l := range ls {
		if strings.Contains(l, "§3.0-c 接线现状") {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		t.Fatalf("契约① 里找不到「§3.0-c 接线现状」那一节 ⇒ 表挪了位置或改了名，这条判据现在什么都看不见")
	}
	var rows []wiringRow
	for i := hdr + 1; i < len(ls); i++ {
		l := strings.TrimSpace(ls[i])
		if !strings.HasPrefix(l, "|") {
			if len(rows) > 0 {
				break // 表已经结束
			}
			continue
		}
		cells := strings.Split(l, "|")
		if len(cells) < 4 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		def := strings.Trim(strings.TrimSpace(cells[2]), "`")
		if name == "" || name == "东西" || strings.HasPrefix(name, "--") || !strings.Contains(name, ".") {
			continue // 表头与分隔行
		}
		rows = append(rows, wiringRow{symbol: name, defLoc: def})
	}
	if len(rows) == 0 {
		t.Fatalf("§3.0-c 那一节里一张表格行都没解析出来 ⇒ 表改了形状，这条判据现在是永真式")
	}
	t.Logf("从契约读到 %d 行点名符号（唯一输入源=specs/contracts-internal-v0.1.md §3.0-c，本文件不内嵌清单）", len(rows))
	return rows
}

// wiringCodeRoot：代码侧扫描根，默认工作树 core/。
func wiringCodeRoot(t *testing.T) (string, string) {
	t.Helper()
	if v := os.Getenv("LUCERO_WIRING_ROOT"); v != "" {
		return v, "LUCERO_WIRING_ROOT"
	}
	return filepath.Join(repoRoot(t), "core"), "默认（工作树 core/）"
}

// splitSymbol 把 permission.Engine.Decide 拆成 包名段／类型段／末段。
func splitSymbol(sym string) (pkgIdent, typ, last string) {
	parts := strings.Split(sym, ".")
	if len(parts) <= 2 {
		return parts[0], "", parts[len(parts)-1]
	}
	return parts[0], parts[len(parts)-2], parts[len(parts)-1]
}

func relTo(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// resolveDefFile 把契约那一格（允许只写文件名，如 permission.go:269）落到真实文件上。
// 第三个返回值是"定位不到"时给人看的诊断（候选都有谁、我用什么条件消歧失败）。
func resolveDefFile(coreRoot, def, pkgIdent string) (file, pkgDir, diag string, ok bool) {
	base := def
	if i := strings.LastIndex(base, ":"); i > strings.LastIndex(base, "/") {
		base = base[:i] // 去掉 :行号（行号不参与定位，它会漂）
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "", "", "那一格是空的", false
	}
	direct := filepath.Join(coreRoot, filepath.FromSlash(base))
	// 契约那一格是文本：解析完必须仍在 core/ 里面（防 ".." 之类把判据带出仓库）
	if !strings.HasPrefix(filepath.ToSlash(direct), filepath.ToSlash(coreRoot)+"/") {
		return "", "", "定义位想跑出 core/：" + def, false
	}
	if st, err := os.Stat(direct); err == nil && !st.IsDir() {
		return relTo(coreRoot, direct), relTo(coreRoot, filepath.Dir(direct)), "", true
	}
	var hits []string
	filepath.Walk(coreRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(p, ".go") ||
			strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if filepath.Base(p) == filepath.Base(base) {
			hits = append(hits, p)
		}
		return nil
	})
	if len(hits) == 0 {
		return "", "", "全树没有叫 " + filepath.Base(base) + " 的非测试 .go", false
	}
	if len(hits) == 1 {
		return relTo(coreRoot, hits[0]), relTo(coreRoot, filepath.Dir(hits[0])), "", true
	}
	// 同名文件不止一个（盘上确实有两个 approval.go：internal/approval 与 internal/execution）
	// ⇒ 用符号自己的包名段消歧；不猜，也不静默取第一个
	var pick []string
	for _, p := range hits {
		if filepath.Base(filepath.Dir(p)) == pkgIdent {
			pick = append(pick, p)
		}
	}
	if len(pick) == 1 {
		return relTo(coreRoot, pick[0]), relTo(coreRoot, filepath.Dir(pick[0])), "", true
	}
	var names []string
	for _, p := range hits {
		names = append(names, relTo(coreRoot, p))
	}
	return "", "", "同名候选 " + strings.Join(names, " / ") + "，按包名段 " + pkgIdent +
		" 也没能唯一确定（契约那一格请写全路径）", false
}

// symbolOnDisk 在定义文件里找「类型 + 方法」。接口方法不以 FuncDecl 出现，
// 所以「类型存在 + 该接口声明过同名方法」也算存在。返回 (是否存在, 该文件声明的类型名, 匹配到的方法名)。
func symbolOnDisk(coreRoot, defFile, typ, last string) (bool, []string, []string) {
	full := filepath.Join(coreRoot, filepath.FromSlash(defFile))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, full, nil, 0)
	if err != nil {
		return false, nil, nil
	}
	var types, matched []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.TypeSpec:
			types = append(types, d.Name.Name)
			if it, ok := d.Type.(*ast.InterfaceType); ok && d.Name.Name == typ {
				for _, m := range it.Methods.List {
					for _, id := range m.Names {
						if id.Name == last {
							matched = append(matched, last)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name != last || len(matched) > 0 {
				return true
			}
			if typ == "" {
				matched = append(matched, last)
				return true
			}
			if d.Recv == nil || len(d.Recv.List) != 1 {
				return true
			}
			rn := ""
			switch e := d.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				if id, ok := e.X.(*ast.Ident); ok {
					rn = id.Name
				}
			case *ast.Ident:
				rn = e.Name
			}
			if rn == typ {
				matched = append(matched, last)
			}
		}
		return true
	})
	return len(matched) > 0, types, matched
}

// countCallSites 数「非测试、且不在定义包内、且**导入了定义包**」对 .last( 的调用点（AST，不是字面 grep）。
// 为什么要加"导入"这道闸：`Request`／`Resolve` 这类方法名在别的包里也可能有同名的，
// 光按名字数会把别人的调用算成这条接线（假绿）。第二个返回值是被这道闸排除掉的同名调用数 ——
// **排除项必须计数打印**，否则它就是一条安静变窄的口子。
func countCallSites(coreRoot, pkgDir, last string) (int, []string, int) {
	var n, excluded int
	var where []string
	filepath.Walk(coreRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		r := relTo(coreRoot, p)
		if filepath.Dir(r) == pkgDir {
			return nil // 排除定义它自己的那个包（契约 §3.0-c 的口径）
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return nil
		}
		importsDef := false
		for _, im := range f.Imports {
			ip := strings.Trim(im.Path.Value, `"`)
			if ip == pkgDir || strings.HasSuffix(ip, "/"+pkgDir) {
				importsDef = true
			}
		}
		var mine int
		ast.Inspect(f, func(node ast.Node) bool {
			ce, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			se, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok || se.Sel.Name != last {
				return true
			}
			mine++
			if importsDef {
				n++
				if len(where) < 3 {
					where = append(where, r+":"+strconv.Itoa(fset.Position(ce.Pos()).Line))
				}
			}
			return true
		})
		if !importsDef {
			excluded += mine
		}
		return nil
	})
	return n, where, excluded
}

// TestWiringSurfaceHasCallSites 就是 T-431 那条机械判据：契约点名的每个符号，
// 今天必须能在**它自己包外**的非测试码里找到至少一个调用点。
func TestWiringSurfaceHasCallSites(t *testing.T) {
	coreRoot, src := wiringCodeRoot(t)
	rows := parseWiringTable(t)
	t.Logf("代码侧扫描根=%s（来源=%s）", coreRoot, src)
	for _, r := range rows {
		r := r
		pkgIdent, typ, last := splitSymbol(r.symbol)
		t.Run(r.symbol, func(t *testing.T) {
			defFile, defPkg, diag, ok := resolveDefFile(coreRoot, r.defLoc, pkgIdent)
			if !ok {
				t.Fatalf("契约那一格「%s」在 core/ 下定位不到唯一文件：%s ⇒ 判据不猜", r.defLoc, diag)
			}
			exists, declared, _ := symbolOnDisk(coreRoot, defFile, typ, last)
			if !exists {
				t.Errorf("契约点名的 %s 在盘上**不存在**：%s 里没有类型 %s（该文件声明的类型=%s）"+
					"⇒ 这不是「没人调」，是**名字写错或已改名**；先纠正契约或补实现，别让它长期冒充前者",
					r.symbol, defFile, typ, strings.Join(declared, " "))
				return
			}
			n, where, excluded := countCallSites(coreRoot, defPkg, last)
			if n == 0 {
				t.Errorf("%s（定义文件 %s 里的 %s）非测试、排除本包、且要求导入定义包之后的调用点 = **0**"+
					" ⇒ 接线没走到它。口径：断的是调用点，不是「没人用这张表」，也不等于写口／提及那两种计数"+
					"（另有 %d 处同名调用因为没导入 %s 而被排除，不算它头上）",
					r.symbol, defFile, last, excluded, defPkg)
				return
			}
			t.Logf("%s ⇒ 包外非测试调用点 %d 处：%s（同名但被「没导入定义包」排除 %d 处）",
				r.symbol, n, strings.Join(where, "、"), excluded)
		})
	}
}

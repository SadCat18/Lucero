package test

// 本文件是「§6 有名字、非测试码里没有生产者」这张名单的执行者（T-346② 裁的甲 + T-355 钉的回解深度）。
//
// 判的是什么：§6 展开后的词表里，**没有任何一个发射位能产出这个名字**的那些，必须与事件契约散文里
// 「本名单当前是…」那一句**双向**相等。名单只判当前态（历史那两段"划掉/补进"留档不参与对拍）。
//
// 为什么不能按「常量被引用过」判（REQ-T31 出生现场）：同一种发射有**两种写法** ——
// `internal/task/service.go` 在发射位传常量、`internal/project/service.go` 的发射位是裸字面量
// `Type: "project_created"`。只认常量就会把后者判成"没有生产者"，那是假红。
//
// 为什么又不能只认「Type 上直接是字面量或常量」（T-355 现场）：`internal/task/service.go` 的真实发射是
//
//	evType, payload, err := edgeEvent(edge, to, cmd)      // 汇聚调用
//	db.EventDraft{..., Type: evType, ...}                  // 发射位拿到的是变量
//
// 而 `edgeEvent` 内部是 `typ, v = EventRunning, RunningPayload{...}` 的一整排 switch。
// 一个汇聚位能把十几个名字汇到同一条 `Type: 变量` 上 —— 只认直接常量的话，那十几个**全部**掉进
// 「无生产者」，盲区就静默缩成了假红。所以回解必须走四层（第④层由 T-371① 裁定计入覆盖）：
//
//	① 包常量（带包名这一层：task.EventCreated 与 execution.EventCreated 同名不同值）
//	② 同文件里的局部赋值链（`evType := edgeEvent(...)` ← `edgeEvent` 体内 `typ = EventRunning`）
//	③ 汇聚函数的返回值（含跨文件同包：常量表与函数值表都按包合并）
//	④ 形参 ← 同包调用点的实参，且**只吃常量/字面量**（`Type: evType` 的值从 takeEdge 的调用点流进来）
//
// 原来那条"同文件出现过的字面量"兜底**不再产生覆盖**（T-371① 之后它只报「未决所及」）：
// 一个不相干的同文件字符串就能替一个看不见的发射位作证，那是假绿方向。
//
// 放宽方向是**单向**的：候选只许偏大（偏大只会少报无生产者），绝不许偏小（偏小就是假红）。
// 因此同一文件内做近似、不按函数作用域精修，是故意的。
//
// 发射位的认定：平台层四个写入口（`Tx.Create` / `Tx.CreateVersionless` / `Tx.TouchTransition` /
// `Tx.AppendEventOnly`）的实参都是 `db.EventDraft`（签名见 `internal/platform/db/touch.go`），
// 所以扫「EventDraft 复合字面量的 Type 字段」就够；`Tx.Append` 签名里没有事件，**故意不认**——
// 认了就是把「只写行」算成「发了事件」，与 REQ-DB-6 相反。
//
// §6 的展开约定①~⑤**只有一份实现**：同包的 `parseEventCatalog` → `expandCatalogRows` → `expandEventCell`
// （`TestEventCatalogMatchesJournalConstants` 用的就是它）。本文件不另拄一张表、不重写展开。
//
// 三条腿（T-346② 钉的形状）在 `TestEventProducerScannerLegs` 里：
// 　甲 裸字面量发射位不算无生产者；乙 只有定义位+注册表位（含 map 里那个 `true` 条目）必须算无生产者；
// 　丙 汇聚位（`Type: 变量` ← 函数内一排常量）覆盖到的名字不许进无生产者集合，且回解不通的位点必须
// 　　逐个列出它覆盖到的候选名字。

import (
	"fmt"
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

// constTable：包名 → 标识符 → 该标识符可能取的字面值集合。
type constTable map[string]map[string]map[string]bool

// fnBits 是一个函数体的原始材料：字面量 + 可能产出 event_type 的表达式 + 体内的赋值。
// 表达式要留到常量表收齐之后再解析（否则 `typ = journal.Running` 这种在同一个文件里也解不开），
// 所以这里先存 AST 节点、第三遍才回解。
type fnBits struct {
	lits map[string]bool
	refs []ast.Expr
	asgn [][2]string // [变量名, 右侧字面量] —— 只有右侧是字面量的直接记，其余走回解
	ids  []ast.Expr  // 右侧表达式，等 funcs/alias 齐了再解
}

// fileFacts：一个文件里能拿来做回解的四样东西（②③ 两层都在这里）。
type fileFacts struct {
	pkg   string
	alias map[string]map[string]bool          // 变量名/同包常量名 → 可能取的字面值（含赋值链）
	funcs map[string]map[string]bool          // 函数名 → 它可能产出的字面值（汇聚函数的来源）
	lits  map[string]bool                     // 函数体内出现过的字面量（兜底用；不含 import 路径与常量声明）
	src   map[string]string                   // 变量名 → 喂给它的那个表达式原文（看不见时点名用）
	call  map[string]map[string]*ast.CallExpr // 函数裸名 → 变量名 → 右侧那个调用（**必须按函数分格**，见 recordCall）
	bits  map[string]*fnBits
}

func newFileFacts(pkg string) fileFacts {
	return fileFacts{pkg: pkg,
		alias: map[string]map[string]bool{}, funcs: map[string]map[string]bool{},
		lits: map[string]bool{}, src: map[string]string{}, call: map[string]map[string]*ast.CallExpr{},
		bits: map[string]*fnBits{}}
}

func (f fileFacts) addAlias(name string, vals ...string) {
	if len(vals) == 0 {
		return
	}
	if f.alias[name] == nil {
		f.alias[name] = map[string]bool{}
	}
	for _, v := range vals {
		f.alias[name][v] = true
	}
}

// draftTypeSite：一个发射位上 `Type:` 表达式的回解结果。
// values 是它**可能**产出的名字集合：0 个 = 彻底看不见；1 个 = 认到；多个 = 认到一批（汇聚位）。
type draftTypeSite struct {
	file    string
	line    int      // `Type:` 那个值表达式的行号
	litLine int      // 复合字面量起始行号（两行一起给，免得点名被读成假 0 命中）
	expr    string   // `Type:` 右侧原文
	srcExpr string   // 右侧是变量时：同文件里喂给它的那个表达式原文（看不见时能顺着查）
	fnName  string   // 所在函数/方法名
	values  []string // 回解出的全部候选（含垃圾：格式串、错误文案）
	covered []string // values ∩ §6 词表 ⇒ 只有这个集合算覆盖
	byL4    []string // 只靠第四层（形参←调用点实参）才认到的名字，读数里单标
	byL5    []string // 第五层（闭包 return 第一位）并进来的名字
	impact  []string // 未决所及：这一处若再放宽一层会捞到哪些词表名字（只诊断，不算覆盖）
	pkg     string   // 发射位所在包名（第四层只走同包）
	kind    string   // literal / const / alias / call / lits / none
	dropped int      // len(values)-len(covered)：因不在词表被丢掉的数量
}

// draftScan：整个扫描结果。
type draftScan struct {
	sites        []draftTypeSite
	emitted      map[string]bool // 被至少一个发射位覆盖到的名字（含汇聚位、第四层、兜底覆盖的）
	pending      []draftTypeSite // 一个词表内候选都定不到的发射位 = 「未决」，与「无生产者」分开
	pendingNames []string        // 未决位点牵连到的名字：既不判"有生产者"，也不许写进"无生产者"
	files        int
}

// producible 是「不许被判成无生产者」的并集：已确认有生产者的 + 未决牵连到的。
// 未决的名字**不算有生产者**（打印时另起一行、并让用例 FAIL），只是不能写成"确认无生产者"——那是要进名单的假事实。
func (sc draftScan) producible() map[string]bool {
	m := map[string]bool{}
	for v := range sc.emitted {
		m[v] = true
	}
	for _, v := range sc.pendingNames {
		m[v] = true
	}
	return m
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func litValue(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(bl.Value)
	return v, err == nil
}

// collectFile 分四遍收材：
// 　P1 包常量 + 每个函数体的字面量/表达式/赋值（import 路径整条跳过：它不是 event_type 的候选）
// 　P2 赋值链（右侧是常量或别名的），此时还没算 funcs，不做调用展开
// 　P3 算 funcs[f] = 体内字面量 ∪ 体内表达式解析出的值
// 　P4 再来一轮赋值链 —— **这一轮才允许查 funcs**，`evType := pick(to)` 只有在这里才接得上
// 顺序是有原因的：`edgeEvent` 那种汇聚位就是 P3→P4 这条链，少一遍就会把十几个名字判成无生产者。
func collectFile(fset *token.FileSet, path string, consts constTable) (fileFacts, *ast.File, bool) {
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fileFacts{}, nil, false
	}
	f := newFileFacts(node.Name.Name)

	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.GenDecl:
			if x.Tok != token.CONST && x.Tok != token.VAR {
				return true
			}
			for _, spec := range x.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != len(vs.Names) {
					continue
				}
				for i, nm := range vs.Names {
					v, ok := litValue(vs.Values[i])
					if !ok {
						continue
					}
					if consts[f.pkg] == nil {
						consts[f.pkg] = map[string]map[string]bool{}
					}
					if consts[f.pkg][nm.Name] == nil {
						consts[f.pkg][nm.Name] = map[string]bool{}
					}
					consts[f.pkg][nm.Name][v] = true
					f.addAlias(nm.Name, v) // 同包裸名（`Type: EventCreated`）走这一条
				}
			}
			return true
		case *ast.FuncDecl:
			bb := &fnBits{lits: map[string]bool{}}
			ast.Inspect(x.Body, func(m ast.Node) bool {
				if _, isImp := m.(*ast.ImportSpec); isImp {
					return false
				}
				switch y := m.(type) {
				case *ast.BasicLit:
					if v, ok := litValue(y); ok {
						bb.lits[v] = true
					}
				case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr:
					bb.refs = append(bb.refs, m.(ast.Expr))
				case *ast.AssignStmt:
					if len(y.Lhs) != len(y.Rhs) {
						// 多变量 ← 单个调用（`evType, payload := eventOf(cur)`）：值得等回解，但**右侧那个调用现在就得记**，
						// 否则第五层永远没有入口 —— 这里原来直接 return，那一格静默失踪（真树 service.go 的 :190 就是这个形状）。
						if len(y.Rhs) == 1 {
							if ce, ok := y.Rhs[0].(*ast.CallExpr); ok {
								for _, lhs := range y.Lhs {
									if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
										f.recordCall(x.Name.Name, id.Name, ce)
									}
								}
							}
						}
						return true
					}
					for i, lhs := range y.Lhs {
						id, ok := lhs.(*ast.Ident)
						if !ok {
							continue
						}
						if v, ok := litValue(y.Rhs[i]); ok {
							f.addAlias(id.Name, v)
							bb.asgn = append(bb.asgn, [2]string{id.Name, v})
							continue
						}
						bb.ids = append(bb.ids, y.Rhs[i])
						if _, has := f.src[id.Name]; !has {
							f.src[id.Name] = renderExpr(y.Rhs[i])
						}
						if ce, ok := y.Rhs[i].(*ast.CallExpr); ok && id.Name != "_" {
							f.recordCall(x.Name.Name, id.Name, ce) // 第五层入口
						}
					}
				}
				return true
			})
			f.bits[x.Name.Name] = bb
			return false
		}
		return true
	})

	return f, node, true
}

// finishFile 在**所有文件的 P1 都跑完之后**才做解析：常量表这时才是全的。
// 少这一步就会漏掉跨文件同包那一格 —— `EventRunning = "task_running"` 在 `internal/task/events.go`、
// 而 `typ = EventRunning` 在 `internal/task/service.go`，先到先解的那个文件看不见后到的常量，
// 汇聚位就又瞎了（09-27 我第一次跑出来 covered 只有 15 个、49 条假红，就是这个原因）。
func finishFuncs(node *ast.File, consts constTable, f fileFacts) {
	assignPass(node, consts, f, false)
	for name, bb := range f.bits {
		vals := map[string]bool{}
		for v := range bb.lits {
			vals[v] = true
			f.lits[v] = true
		}
		for _, e := range bb.refs {
			if _, isLit := e.(*ast.BasicLit); isLit {
				continue
			}
			for _, v := range f.exprValues(consts, e, false) {
				vals[v] = true
			}
		}
		if len(vals) == 0 {
			continue
		}
		f.funcs[name] = vals
		// 并进入**包级**命名空间：Go 里包级常量与函数同属一个作用域（不可能同名），
		// 所以跨文件调用（`eventOf` 在 execution/events.go、调用点在 execution/service.go）
		// 与跨包选择器都能从同一张表里查到 —— 不分层收就会把汇聚位 again 判成瞎。
		if consts[f.pkg] == nil {
			consts[f.pkg] = map[string]map[string]bool{}
		}
		if consts[f.pkg][name] == nil {
			consts[f.pkg][name] = map[string]bool{}
		}
		for v := range vals {
			consts[f.pkg][name][v] = true
		}
	}
}

// finishAliases 在包级函数值表并进常量表之后才跑：`evType, payload := eventOf(cur)` 这类
// 跨文件调用要到这里才接得上。
func finishAliases(node *ast.File, consts constTable, f fileFacts) {
	assignPass(node, consts, f, true)
}

// assignPass 扫全文件的赋值：把右侧能回解成字面值的接进 alias。useFuncs=false 那轮不做调用展开。
func assignPass(node *ast.File, consts constTable, f fileFacts, useFuncs bool) {
	for round := 0; round < 3; round++ {
		grew := false
		ast.Inspect(node, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) == 0 || len(as.Rhs) == 0 ||
				(len(as.Lhs) != len(as.Rhs) && !(len(as.Rhs) == 1 && len(as.Lhs) > 1)) {
				return true
			}
			for i, lhs := range as.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				// 多返回值赋值（`evType, payload, err := edgeEvent(...)`）右侧只有一个表达式：Go 按位置对应
				// 返回值，而我不知道第 i 个是什么 ⇒ 把该调用可能产出的全部值发给每个左侧变量。
				// 方向仍是"只往更不确定放宽"：只会少报无生产者，不会造假红。
				rhs := as.Rhs[0]
				if len(as.Rhs) == len(as.Lhs) {
					rhs = as.Rhs[i]
				}
				have := len(f.alias[id.Name])
				for _, v := range f.exprValues(consts, rhs, useFuncs) {
					if f.alias[id.Name] == nil {
						f.alias[id.Name] = map[string]bool{}
					}
					if !f.alias[id.Name][v] {
						f.alias[id.Name][v] = true
						grew = true
					}
				}
				if len(f.alias[id.Name]) > have {
					grew = true
				}
			}
			return true
		})
		if !grew {
			return
		}
	}
}

// exprValues：字面量 → 包常量/同包名 → 本文件别名 → 函数产出（useFuncs 时才查，避免自递归）。
func (f fileFacts) exprValues(consts constTable, e ast.Expr, useFuncs bool) []string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if v, ok := litValue(x); ok {
			return []string{v}
		}
	case *ast.Ident:
		// 裸名先查**本包的常量表**（跨文件也算）：`internal/task/service.go` 里写的 `EventRunning`
		// 声明在同包的 `events.go`，只查本文件的 alias 就永远接不上 —— 汇聚位就是这么瞎掉的。
		if v := consts[f.pkg][x.Name]; len(v) > 0 {
			return keys(v)
		}
		if v := f.alias[x.Name]; len(v) > 0 {
			return keys(v)
		}
		if useFuncs {
			if v := f.funcs[x.Name]; len(v) > 0 {
				return keys(v)
			}
		}
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return nil
		}
		if v := consts[pkg.Name][x.Sel.Name]; len(v) > 0 {
			return keys(v)
		}
	case *ast.CallExpr:
		switch fn := x.Fun.(type) {
		case *ast.Ident: // 同包（可能同文件，也可能跨文件——跨文件那份在包级表里）
			if useFuncs {
				if v := f.funcs[fn.Name]; len(v) > 0 {
					return keys(v)
				}
				if v := consts[f.pkg][fn.Name]; len(v) > 0 {
					return keys(v)
				}
			}
		case *ast.SelectorExpr: // 跨包：`journal.Pick(...)`
			if pkg, ok := fn.X.(*ast.Ident); ok {
				if v := consts[pkg.Name][fn.Sel.Name]; len(v) > 0 {
					return keys(v)
				}
			}
			if useFuncs {
				if v := f.funcs[fn.Sel.Name]; len(v) > 0 {
					return keys(v)
				}
			}
		}
	}
	return nil
}

// constOrLit 是**第四层专用**的窄回解：只认字符串字面量与包级常量/变量的值。
// 函数产出一律挡掉（`f.funcs` 与 finishFuncs 并进包表的那一份都在 fnNames 里）—— 主控把第四层限定成
// "实参可回解为常量或字面量"（T-371①），再宽就变成"任何调用都能凭空造生产者"。
func (f fileFacts) constOrLit(consts constTable, fnNames map[string]bool, e ast.Expr) []string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if v, ok := litValue(x); ok {
			return []string{v}
		}
	case *ast.Ident:
		if fnNames[x.Name] {
			return nil
		}
		if v := consts[f.pkg][x.Name]; len(v) > 0 {
			return keys(v)
		}
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok || fnNames[x.Sel.Name] {
			return nil
		}
		if v := consts[pkg.Name][x.Sel.Name]; len(v) > 0 {
			return keys(v)
		}
	}
	return nil
}

// resolveClosureOrLit 是**第五层**的解析器：在第四层那一档之上只多认一种形状 ——
// 实参本身是个 FuncLit，且它 `return` 的**第一位**是包级常量或字符串字面量。
// 这认的就是 `s.advance(..., func(t Task) (string, any) { return EventQueued, Payload{...} })`：
// 名字从闭包里回出来，前四层都看不见。再宽（闭包里的计算值、二层回调）一律不认 ——
// 过程间数据流一开，"看不见"就变成"什么都算看得见"，那才是把判据换成绿灯。
func resolveClosureOrLit(f fileFacts, consts constTable, fnNames map[string]bool, e ast.Expr) []string {
	fl, ok := e.(*ast.FuncLit)
	if !ok {
		return resolveConstOrLit(f, consts, fnNames, e)
	}
	var out []string
	ast.Inspect(fl.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) == 0 {
			return true
		}
		out = append(out, resolveConstOrLit(f, consts, fnNames, ret.Results[0])...)
		return true
	})
	return out
}

// recordCall 记下"某函数里某变量的右侧那个调用"。按函数名分格是必须的：
// `internal/task/service.go` 里 `evType` 在 :116（`edgeEvent(...)`）与 :190（`eventOf(cur)`）各被赋过一次，
// 不分格就是第一处吃掉第二处 —— 第五层会对着 `edgeEvent` 回解，:197 那一处永远补不上闭包的名字。
func (f fileFacts) recordCall(fn, name string, ce *ast.CallExpr) {
	if f.call[fn] == nil {
		f.call[fn] = map[string]*ast.CallExpr{}
	}
	if _, has := f.call[fn][name]; !has {
		f.call[fn][name] = ce
	}
}

// enclosingFn 给表达式所在函数的**裸名**（不带接收者），与 recordCall 的键一致。
func enclosingFn(node *ast.File, e ast.Expr) string {
	for _, d := range node.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if fd.Body.Pos() <= e.Pos() && e.Pos() < fd.Body.End() {
			return fd.Name.Name
		}
	}
	return ""
}

// closureCandidates 把"变量 ← 调用一个形参 ← 该形参在同包调用点收到的 FuncLit ← return 第一位"这条链走通。
// 每一步走不通就回空（不猜）：这层是给判据加深度的，不是给绿灯兜底的。
func closureCandidates(node *ast.File, f fileFacts, consts constTable, fnNames map[string]map[string]bool,
	want map[string]bool, nodes map[string]*ast.File, facts map[string]fileFacts, te ast.Expr) []string {
	id, ok := te.(*ast.Ident)
	if !ok {
		return nil
	}
	ce, has := f.call[enclosingFn(node, te)][id.Name]
	if !has {
		return nil
	}
	callee, ok := ce.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	fn, idx, ok := paramRef(node, callee)
	if !ok {
		return nil
	}
	return argValuesAt(fn, idx, f.pkg, want, nodes, facts, consts, resolveClosureOrLit, fnNames)
}

// 两个解析器：第四层用窄的那个，未决诊断用宽的那个（宽只报影响面，不进覆盖集）。
func resolveConstOrLit(f fileFacts, consts constTable, fnNames map[string]bool, e ast.Expr) []string {
	return f.constOrLit(consts, fnNames, e)
}

func resolveWide(f fileFacts, consts constTable, _ map[string]bool, e ast.Expr) []string {
	return f.exprValues(consts, e, true)
}

// resolveTypeExpr 是发射位上 Type 的那次回解，带 kind（读数里要说清是认到的还是兜底兜到的）。
func (f fileFacts) resolveTypeExpr(consts constTable, e ast.Expr) ([]string, string) {
	if v, ok := litValue(e); ok {
		return []string{v}, "literal"
	}
	if vals := f.exprValues(consts, e, true); len(vals) > 0 {
		kind := "alias"
		if id, ok := e.(*ast.Ident); ok && len(f.alias[id.Name]) > 0 && len(f.funcs[id.Name]) > 0 {
			kind = "alias"
		} else if _, ok := e.(*ast.CallExpr); ok {
			kind = "call"
		} else if _, ok := e.(*ast.SelectorExpr); ok {
			kind = "const"
		} else if id, ok := e.(*ast.Ident); ok {
			if _, isConst := f.alias[id.Name]; isConst && len(consts[f.pkg][id.Name]) > 0 {
				kind = "const"
			}
		}
		return vals, kind
	}
	return nil, "none"
}

func isEventDraftLit(cl *ast.CompositeLit) bool {
	switch ty := cl.Type.(type) {
	case *ast.SelectorExpr:
		return ty.Sel.Name == "EventDraft"
	case *ast.Ident:
		return ty.Name == "EventDraft"
	}
	return false
}

func renderExpr(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Value
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok {
			return pkg.Name + "." + x.Sel.Name
		}
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	case *ast.CallExpr:
		return renderExpr(x.Fun) + "(…)"
	}
	return "（非字面量表达式）"
}

// relPath 把路径折成相对扫描根的样子：两棵树（工作树 / HEAD 快照）的读数要能逐行对读。
func relPath(base, path string) string {
	if r, err := filepath.Rel(base, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

// codeScanRoot：只换**代码侧**的扫描根（取证用，不是跳过判据的开关）。
// 默认工作树 core/；设 LUCERO_CODE_ROOT 就指到一棵导出来的快照（我用 `git archive HEAD core` 解进 .scratch 比过），
// 为的是把「别人未入册的在途改动」与「已入册的那棵树」分开读——契约① 第 24 条「证据生命周期」第⑧款要的是这个。
// 词表那一侧不换：specs/ 无未提交项时两份是同一个文件（起腿前量过）。
func codeScanRoot(t *testing.T) string {
	if v := os.Getenv("LUCERO_CODE_ROOT"); v != "" {
		return v
	}
	return filepath.Join(repoRoot(t), "core")
}

// runDraftScan：扫一棵树，返回发射位与被覆盖到的名字集合。
// catalogSet / intersect：兜底的候选必须是词表里的名字，否则一个 `Type: 变量`
// 会把整份文件的字符串（import 路径、错误码、payload 键名）都算成生产者候选 —— 那等于没判。
func catalogSet(catalog []string) map[string]bool {
	m := map[string]bool{}
	for _, c := range catalog {
		m[c] = true
	}
	return m
}

func intersect(all []string, want map[string]bool) []string {
	var out []string
	for _, s := range all {
		if want[s] {
			out = append(out, s)
		}
	}
	return out
}

func runDraftScan(t *testing.T, root string, catalog []string) draftScan {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("扫 %s 失败：%v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("扫描面为空（%s 下一个非测试 .go 都没有）⇒ 这条判据什么都看不见", root)
	}

	fset := token.NewFileSet()
	consts := constTable{}
	facts := map[string]fileFacts{}
	nodes := map[string]*ast.File{}
	for _, path := range files { // P1：先把所有文件的常量收齐
		if f, node, ok := collectFile(fset, path, consts); ok {
			facts[path], nodes[path] = f, node
		}
	}
	for path, node := range nodes { // P2：先算每个函数的候选值，并同步进包级表
		finishFuncs(node, consts, facts[path])
	}
	for path, node := range nodes { // P3：包级函数表齐了，才做带调用展开的别名轮
		finishAliases(node, consts, facts[path])
	}

	sc := draftScan{emitted: map[string]bool{}, files: len(files)}
	want := catalogSet(catalog)
	// 包级"函数名"集合：第四层靠它把函数产出挡在门外（它只吃常量/字面量）。
	fnNames := map[string]map[string]bool{}
	for _, f := range facts {
		for name := range f.funcs {
			if fnNames[f.pkg] == nil {
				fnNames[f.pkg] = map[string]bool{}
			}
			fnNames[f.pkg][name] = true
		}
	}
	for _, path := range files {
		// 用 P1 那份 AST，不再 parse 一次：go/token 每次 AddFile 都给新的基址，
		// 跨 AST 比位置就永远对不上（第五层在这上面空转过一轮 —— paramRef 恒 false）。
		node, parsed := nodes[path]
		if !parsed {
			continue
		}
		f := facts[path]
		ast.Inspect(node, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isEventDraftLit(cl) {
				return true
			}
			var te ast.Expr
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue // 按位置传参：EventDraft 九个字段，猜位置就是把误读写进判据
				}
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Type" {
					te = kv.Value
				}
			}
			s := draftTypeSite{file: relPath(root, path), line: fset.Position(cl.Pos()).Line,
				pkg: f.pkg, expr: "（这个 EventDraft 没有 Type 字段）", kind: "none"}
			if te != nil {
				vals, kind0 := f.resolveTypeExpr(consts, te)
				s = draftTypeSite{file: relPath(root, path), line: fset.Position(te.Pos()).Line,
					litLine: fset.Position(cl.Pos()).Line, expr: renderExpr(te),
					values: vals, kind: kind0, fnName: fnOf(node, te), srcExpr: f.srcOf(te)}
				s.pkg = f.pkg
				if s.kind == "none" {
					// 第四层（T-371① 裁"算覆盖"）：`Type: 形参` 顺着同包调用点的实参回解，只吃常量/字面量。
					if fn, idx, ok := paramRef(node, te); ok {
						if v := argValuesAt(fn, idx, f.pkg, want, nodes, facts, consts, resolveConstOrLit, fnNames); len(v) > 0 {
							s.values, s.kind, s.byL4 = v, "param", v
						}
					}
				}
				if s.kind == "none" {
					// 兜底降级（T-371①）：只能靠"同文件里出现过这个字符串"说话的位点，**不算生产者**。
					// 它以前会被整个塞进候选 ⇒ 那是假绿方向（同文件一个不相干的字面量就能替一个发射位作证）。
					// 现在只把它报成"未决所及"：既不写"有生产者"，也不写"无生产者"。
					s.impact = intersect(keys(f.lits), want)
					s.kind = "lits"
				}
			}
			// 第五层（T-394 给的方向）**并入**已有候选：:197 那一处的 `evType` 早被"同文件近似"认成 17 个候选，
			// 可那 17 个里没有闭包回出来的 `task_queued`/`task_accepted` —— 只等 kind==none 才试第五层就永远轮不到它。
			for _, v := range closureCandidates(node, f, consts, fnNames, want, nodes, facts, te) {
				if !hasValue(s.values, v) {
					s.values = append(s.values, v)
					s.byL5 = append(s.byL5, v)
				}
			}
			if len(s.byL5) > 0 {
				sort.Strings(s.values)
				if s.kind == "none" {
					s.kind = "closure" // 只有原本什么都认不到时，才把分类改标成第五层认的
				}
			}
			// **候选先与 §6 词表取交集，才算覆盖**（T-365②：不筛就会把 `task: %s`、`payload: %w` 这类
			// 格式串当成"有生产者的名字"，覆盖集虚高 ⇒ 下一轮真红从这条缝里漏掉）。
			s.covered = intersect(s.values, want)
			s.dropped = len(s.values) - len(s.covered)
			sc.sites = append(sc.sites, s)
			if len(s.covered) == 0 {
				// 未决：一个词表内候选都定不到。宽解析器只用来报"这一处牵连到哪些名字的归类"，不算覆盖。
				if te != nil {
					if fn, idx, ok := paramRef(node, te); ok {
						for _, v := range argValuesAt(fn, idx, f.pkg, want, nodes, facts, consts, resolveWide, fnNames) {
							if !hasValue(s.impact, v) { // 兜底报的与调用点报的并起来，谁都不许覆盖谁
								s.impact = append(s.impact, v)
							}
						}
						sort.Strings(s.impact)
					}
				}
				sc.pending = append(sc.pending, s)
			}
			for _, v := range s.covered {
				sc.emitted[v] = true
			}
			return true
		})
	}
	// 已由别的位点确认有生产者的名字不必再挂未决；剩下的才是"归类待定"的那批。
	pend := map[string]bool{}
	for _, s := range sc.pending {
		for _, v := range s.impact {
			if !sc.emitted[v] {
				pend[v] = true
			}
		}
	}
	sc.pendingNames = keys(pend)
	return sc
}

// splitRoster：派生集合 = 词表 − 被覆盖的名字；再与名单**双向**对拍（单向相等不算对拍）。
func splitRoster(catalog []string, covered map[string]bool, roster []string) (derived, missing, extra []string) {
	inRoster := map[string]bool{}
	for _, r := range roster {
		inRoster[r] = true
	}
	inCatalog := map[string]bool{}
	for _, c := range catalog {
		inCatalog[c] = true
		if !covered[c] {
			derived = append(derived, c)
		}
	}
	sort.Strings(derived)
	for _, r := range roster {
		if !derivedHas(derived, r) {
			missing = append(missing, r) // 名单说它无生产者、我却找到了覆盖位 ⇒ 要么真有、要么我判宽了
		}
		if !inCatalog[r] {
			extra = append(extra, r+"（名字不在 §6 词表里）")
		}
	}
	for _, d := range derived {
		if !inRoster[d] {
			extra = append(extra, d)
		}
	}
	return derived, missing, extra
}

func derivedHas(s []string, x string) bool {
	i := sort.SearchStrings(s, x)
	return i < len(s) && s[i] == x
}

// TestProducerRosterComparisonLegs 是 T-411② 要的**常驻反例腿**：它造的是「派生集合里有、名单没登记」
// 这个真漏登记场景本身，断的是 splitRoster 的**集合行为**，不判任何一句红话的字面 ——
// 所以主控把 :1022 那句措辞改成什么样，本条腿都必须无感（T-411③：不许只验打印文本）。
// 四档缺一不可：只断正例会退化成恒真，只断反例会退化成恒假，第三档「登记对了」是"该放行必须放行"那一腿。
func TestProducerRosterComparisonLegs(t *testing.T) {
	cases := []struct {
		name        string
		catalog     []string
		covered     map[string]bool
		roster      []string
		wantDerived []string
		wantMissing []string
		wantExtra   []string // 精确到名字；ghost 那一档另有一截说明，用前缀判
		extraByPref bool
	}{
		{
			name:        "真漏登记：词表里有、没人覆盖、名单也没登记",
			catalog:     []string{"evt_covered", "evt_nobody"},
			covered:     map[string]bool{"evt_covered": true},
			roster:      []string{},
			wantDerived: []string{"evt_nobody"},
			wantMissing: nil,
			wantExtra:   []string{"evt_nobody"},
		},
		{
			name:        "登记对了：确实没人发、名单里就写着它 ⇒ 两头都不许喊",
			catalog:     []string{"evt_covered", "evt_nobody"},
			covered:     map[string]bool{"evt_covered": true},
			roster:      []string{"evt_nobody"},
			wantDerived: []string{"evt_nobody"},
			wantMissing: nil,
			wantExtra:   nil,
		},
		{
			name:        "登记反了：名单说它没人发、扫描却找到覆盖位 ⇒ missing 必须点名",
			catalog:     []string{"evt_x"},
			covered:     map[string]bool{"evt_x": true},
			roster:      []string{"evt_x"},
			wantDerived: []string{},
			wantMissing: []string{"evt_x"},
			wantExtra:   nil,
		},
		{
			name:        "名单里混进一个词表外的名字 ⇒ 漏登记与越界两头都得喊（只判名字，不判说明文字）",
			catalog:     []string{"evt_x"},
			covered:     map[string]bool{},
			roster:      []string{"evt_x", "evt_ghost"},
			wantDerived: []string{"evt_x"},
			wantMissing: []string{"evt_ghost"},
			wantExtra:   []string{"evt_ghost"},
			extraByPref: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			derived, missing, extra := splitRoster(tc.catalog, tc.covered, tc.roster)
			if got, want := strings.Join(derived, ","), strings.Join(tc.wantDerived, ","); got != want {
				t.Errorf("派生集合=%q，期望 %q", got, want)
			}
			if got, want := strings.Join(missing, ","), strings.Join(tc.wantMissing, ","); got != want {
				t.Errorf("missing=%q，期望 %q —— 名单登记了无生产者却被扫到覆盖位，这一半没抓住", got, want)
			}
			if !tc.extraByPref {
				if got, want := strings.Join(extra, ","), strings.Join(tc.wantExtra, ","); got != want {
					t.Errorf("extra=%q，期望 %q —— 真漏登记这一半没抓住，:1022 那句红话就永远不会响", got, want)
				}
			} else {
				if len(extra) != 1 || !strings.HasPrefix(extra[0], tc.wantExtra[0]) {
					t.Errorf("extra=%v，期望恰好 1 条且指向 %s", extra, tc.wantExtra[0])
				}
			}
			t.Logf("catalog=%v covered=%v roster=%v ⇒ 派生=%v missing=%v extra=%v",
				tc.catalog, tc.covered, tc.roster, derived, missing, extra)
		})
	}
}

// coverKinds 报出覆盖到某个名字的那些位点的 kind 集合：长度 1 且是 param ⇒ "仅靠第四层才认到"。
func coverKinds(sites []draftTypeSite, v string) map[string]bool {
	m := map[string]bool{}
	for _, s := range sites {
		if hasValue(s.covered, v) {
			m[s.kind] = true
		}
	}
	return m
}

func countKind(sites []draftTypeSite, kind string) int {
	n := 0
	for _, s := range sites {
		if s.kind == kind {
			n++
		}
	}
	return n
}

// siteLine 的读数形状是主控点名的：候选 N / 因不在词表被丢掉 M / 实际覆盖 K，三个数都要出现。
// 行号给两行：`Type:` 值所在行与复合字面量起始行（那位点跨两行，只给一行会被读成"文件里没这东西"）。
func siteLine(s draftTypeSite) string {
	tail := ""
	if s.srcExpr != "" {
		tail = fmt.Sprintf("，值来源=%s", s.srcExpr)
	}
	if s.fnName != "" {
		tail += fmt.Sprintf("，所在函数=%s()", s.fnName)
	}
	if len(s.byL4) > 0 {
		tail += fmt.Sprintf("，仅第四层认定=%s", strings.Join(s.byL4, " "))
	}
	if len(s.byL5) > 0 {
		tail += fmt.Sprintf("，第五层并进=%s", strings.Join(s.byL5, " "))
	}
	if len(s.impact) > 0 {
		tail += fmt.Sprintf("，未决所及（诊断、不算覆盖）=%s", strings.Join(s.impact, " "))
	}
	return fmt.Sprintf("%s:%d（字面量起 %d；表达式=%s，%s，候选 N=%d / 因不在 §6 词表被丢掉 M=%d / 实际覆盖 K=%d）%s",
		s.file, s.line, s.litLine, s.expr, s.kind, len(s.values), s.dropped, len(s.covered), tail)
}

// fnOf 报出表达式所在的那个函数/方法名：盲区必须点名到"谁的汇聚位看不见"。
func fnOf(node *ast.File, e ast.Expr) string {
	name := ""
	for _, d := range node.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if fd.Body.Pos() <= e.Pos() && e.Pos() < fd.Body.End() {
			name = recvName(fd) + fd.Name.Name
		}
	}
	return name
}

func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	rt := fd.Recv.List[0].Type
	if star, ok := rt.(*ast.StarExpr); ok {
		rt = star.X
	}
	if id, ok := rt.(*ast.Ident); ok {
		return id.Name + "."
	}
	return ""
}

// paramRef 报出"这个表达式是所在函数的第几号形参"。走调用点要靠位序对齐，
// 一个字段可以绑多个名字（`a, b string`），所以位置要按名字个数累加。
func paramRef(node *ast.File, e ast.Expr) (string, int, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", 0, false
	}
	for _, d := range node.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Type.Params == nil {
			continue
		}
		if fd.Body.Pos() > e.Pos() || e.Pos() >= fd.Body.End() {
			continue
		}
		pos := 0
		for _, fl := range fd.Type.Params.List {
			n := len(fl.Names)
			if n == 0 {
				n = 1
			}
			for k, nm := range fl.Names {
				if nm.Name == id.Name {
					return fd.Name.Name, pos + k, true
				}
			}
			pos += n
		}
	}
	return "", 0, false
}

// argValuesAt 是第四层与"未决诊断"共用的那条腿：顺着"同包里谁调用了这个函数"把第 idx 号实参回解一遍。
// resolve 决定吃多宽：第四层传窄的（算覆盖），未决诊断传宽的（只报影响面，不算覆盖）。
// 只走同包：跨包调用点的 import 别名是另一套解法，不塞进这一层。
func argValuesAt(fn string, idx int, pkg string, want map[string]bool,
	nodes map[string]*ast.File, facts map[string]fileFacts, consts constTable,
	resolve func(fileFacts, constTable, map[string]bool, ast.Expr) []string,
	fnNames map[string]map[string]bool) []string {
	seen := map[string]bool{}
	for path, node := range nodes {
		f := facts[path]
		if f.pkg != pkg {
			continue
		}
		ast.Inspect(node, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch callee := ce.Fun.(type) {
			case *ast.Ident:
				name = callee.Name
			case *ast.SelectorExpr:
				name = callee.Sel.Name
			}
			if name != fn || idx >= len(ce.Args) {
				return true
			}
			for _, v := range resolve(f, consts, fnNames[f.pkg], ce.Args[idx]) {
				if want[v] {
					seen[v] = true
				}
			}
			return true
		})
	}
	return keys(seen)
}

// srcOf 在回解失败时回一句"喂给这个变量的表达式"原文：下一位至少能顺着查，不必从 0 猜。
func (f fileFacts) srcOf(e ast.Expr) string {
	id, ok := e.(*ast.Ident)
	if !ok {
		return ""
	}
	return f.src[id.Name]
}

func pendingList(pending []draftTypeSite) string {
	if len(pending) == 0 {
		return "（无：这一轮没有任何定不到的发射位）"
	}
	var b []string
	for _, s := range pending {
		b = append(b, siteLine(s))
	}
	return strings.Join(b, "; ")
}

// TestEventProducerRosterMatchesDerivedSet 是真树上那条腿。
func TestEventProducerRosterMatchesDerivedSet(t *testing.T) {
	catalog, err := parseEventCatalog(t)
	if err != nil {
		t.Fatalf("§6 词表解析失败：%v", err)
	}
	if len(catalog) < 20 {
		t.Fatalf("§6 只解析出 %d 个事件名（下界 20）⇒ 词表改了形状或区间被截断，这条腿现在什么都看不见", len(catalog))
	}
	root := repoRoot(t)
	if root == "" {
		t.Fatal(errAnchor("仓库根没找到"))
	}
	scanRoot := codeScanRoot(t)
	roster, err := parseProducerRoster(t, filepath.Join(root, "specs", "events", "event-contract-v0.1.md"))
	if err != nil {
		t.Fatalf("散文名单读不到：%v", err)
	}
	sc := runDraftScan(t, scanRoot, catalog)

	// 「未决」牵连的名字只从派生集合里摘出去，**不算有生产者**（下面单独打印、并让用例 FAIL）。
	derived, missing, extra := splitRoster(catalog, sc.producible(), roster)
	t.Logf("代码侧扫描根=%s（LUCERO_CODE_ROOT=%q）；词表侧=%s", scanRoot, os.Getenv("LUCERO_CODE_ROOT"),
		filepath.Join(root, "specs"))
	var onlyL4 []string
	for _, s := range sc.sites { // "仅靠第四层"= 覆盖到它的位点全是第四层那一种
		for _, v := range s.byL4 {
			if len(coverKinds(sc.sites, v)) == 1 && !hasValue(onlyL4, v) {
				onlyL4 = append(onlyL4, v)
			}
		}
	}
	t.Logf("§6 词表 %d 个名字；非测试 .go %d 个；EventDraft 发射位 %d 处（字面量 %d / 常量 %d / 别名或调用 %d / 第四层 %d / 兜底 %d / 未决 %d）",
		len(catalog), sc.files, len(sc.sites), countKind(sc.sites, "literal"), countKind(sc.sites, "const"),
		countKind(sc.sites, "alias")+countKind(sc.sites, "call"), countKind(sc.sites, "param"),
		countKind(sc.sites, "lits"), len(sc.pending))
	t.Logf("【桶一·确认有生产者】%d 个（其中仅靠第四层认到的 %d 个：%s）",
		len(sc.emitted), len(onlyL4), strings.Join(onlyL4, " "))
	t.Logf("【桶二·确认无生产者（派生集合）】%d 个；名单 %d 个：%s",
		len(derived), len(roster), strings.Join(roster, " "))
	t.Logf("【桶三·未决（既不算有也不算无，等回解）】位点 %d 处、牵连名字 %d 个：%s",
		len(sc.pending), len(sc.pendingNames), strings.Join(sc.pendingNames, " "))
	candTotal, dropTotal := 0, 0
	for _, s := range sc.sites {
		candTotal += len(s.values)
		dropTotal += s.dropped
	}
	t.Logf("交集过滤统计：位点候选合计 N=%d，其中 M=%d 个因不在 §6 词表被丢掉（不算覆盖），去重后实际覆盖词表名字 K=%d 个",
		candTotal, dropTotal, len(sc.emitted))
	// 交集与第四层有没有牙，反例都在合成件那几条腿里（戊：词表外字面量必须被丢掉；己：第四层要算覆盖；
	// 庚：第四层只吃常量/字面量，函数实参不许算），这里只把数报出来。

	// 未决位点逐个点名并 FAIL：只报个数字的话，下一轮没人能判断"这几处到底遮住了谁"（T-371①）。
	for _, s := range sc.pending {
		t.Errorf("未决发射位：%s", siteLine(s))
	}
	// 汇聚位与兜底位也点名：它们解释了"为什么某几个名字不算无生产者"，是这轮读数的凭据。
	for _, s := range sc.sites {
		if s.kind == "alias" || s.kind == "call" || s.kind == "lits" {
			t.Logf("宽覆盖 %s ⇒ %s", siteLine(s), strings.Join(s.covered, " "))
		}
	}
	for _, r := range missing {
		t.Errorf("名单登记 %s 无生产者，但扫描找到了覆盖它的发射位 ⇒ 要么它真有生产者、要么这名字写错；两边都得有人改", r)
	}
	for _, e := range extra {
		// FAIL 必须点名"我查过哪些位点、它们为什么覆盖不到这个名字"，否则等于把看不见写成没有。
		t.Errorf("派生集合里有 %s 而名单没登记 ⇒ 两条出路任选一条，别选错：(a) 这名字已在 §6 登记、码里却还没人发 —— 那要补的是常量与发射位（那是半成品），把它登记成无生产者就是往契约里写假事实；(b) 确认确实没人发 —— 才把它加进事件契约里那句现状名单。本轮未决位点：%s", e, pendingList(sc.pending))
	}
}

// parseProducerRoster 读事件契约里那句「本名单当前是…」。名单以"划掉一条/补进一条"的散文修订史存在，
// 当前态是**文件里最后一句**（主控 T-385③ 定的形状）。三件事一起钉：
// 　① 命中多于一句时把**每一句的行号都打出来** —— 以后有人在最后一句之后又补一句同锚，读数里看得见，不是静默换；
// 　② 锚句自称「N 条」与实际按反引号切出的名字数**必须相等**，不等就停（这是形状与内容脱节的唯一自动喊法）；
// 　③ 锚不到仍然 errAnchor 停 —— 主控点名要保留的 fail-closed 方向。
func parseProducerRoster(t *testing.T, path string) ([]string, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errAnchor("读 " + path + " 失败: " + err.Error())
	}
	var line string
	var hits []string
	for i, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, "本名单当前是") {
			hits = append(hits, strconv.Itoa(i+1))
			line = l
		}
	}
	if line == "" {
		return nil, errAnchor("事件契约里找不到「本名单当前是…」那句 ⇒ 名单当前态没有可锚的形状，这条判据不能凭历史段落跑")
	}
	if len(hits) > 1 {
		// REQ-T37 裁「甲」（主控 09-30 先把两句历史行的锚字改成「当时是」，全文件锚数回到 1）：
		// 当前态**必须唯一**，多一处就停。以前的"取最后一句"是拿文件顺序当语义，
		// 谁在现状之后再补一句自洽的同锚句子，判据就会安静读到那句 —— 三道自检都拦不住。
		return nil, errAnchor(fmt.Sprintf("事件契约里有 %d 处「本名单当前是」（行号 %s）⇒ 当前态不唯一，这条判据不猜哪句是现状；"+
			"要把历史留着，请像 09-30 那样把历史句的锚字改成「当时是」", len(hits), strings.Join(hits, ", ")))
	}
	var names []string
	for _, seg := range strings.Split(line, "`") {
		s := strings.TrimSpace(seg)
		if looksLikeEventName(s) {
			names = append(names, s)
		}
	}
	if len(names) == 0 {
		return nil, errAnchor("那句读到了但一个名字都没取出来：" + line)
	}
	if declared, ok := declaredRosterCount(line); ok {
		if declared != len(names) {
			return nil, errAnchor(fmt.Sprintf("锚句自称 %d 条、实际按反引号切出 %d 个 ⇒ 名单的形状与内容脱节，不能拿它当现状：%s",
				declared, len(names), line))
		}
		t.Logf("名单锚句自检：自称 %d 条 = 实取 %d 个（相等）", declared, len(names))
	} else {
		t.Logf("名单锚句里没有可解析的「N 条」声明 ⇒ 少一道自检，只报不拦（形状若改了请连这条一起改）：%s", line)
	}
	sort.Strings(names)
	return names, nil
}

// declaredRosterCount 取锚句里「本名单当前是 **N 条**」那个 N（阿拉伯数字或中文数字）。
// 只看锚点之后到**第一个**「条」为止的那段：新句子里括号引用旧句（`取代上面那句"三条"`）会带来第二个「条」，
// 拿错就成了拿历史段落当现状 —— 所以按第一个，且要求它在锚点之后紧贴的位置。
func declaredRosterCount(line string) (int, bool) {
	i := strings.Index(line, "本名单当前是")
	if i < 0 {
		return 0, false
	}
	rest := line[i+len("本名单当前是"):]
	j := strings.Index(rest, "条")
	if j <= 0 {
		return 0, false
	}
	num := strings.Trim(strings.TrimSpace(rest[:j]), "* ")
	if n, err := strconv.Atoi(num); err == nil {
		return n, true
	}
	return cnNumber(num)
}

// cnNumber 认 0-999 这档中文数字（名单量级用不到更大的；解析不了就回 false，不猜）。
func cnNumber(s string) (int, bool) {
	val := map[rune]int{'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, cur, seen := 0, 0, false
	for _, r := range s {
		switch r {
		case '百', '十':
			unit := 10
			if r == '百' {
				unit = 100
			}
			if cur == 0 {
				cur = 1
			}
			total += cur * unit
			cur = 0
			seen = true
		default:
			v, ok := val[r]
			if !ok {
				return 0, false
			}
			cur = v
			seen = true
		}
	}
	if !seen {
		return 0, false
	}
	return total + cur, true
}

// looksLikeEventName 只认名字形状（小写字母开头、字母数字下划线），
// 不拿"必须含下划线"当筛子 —— 那种筛子会静默丢掉合法名字。
func looksLikeEventName(s string) bool {
	if len(s) < 3 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// TestEventProducerScannerLegs 是三条腿（合成件，不碰仓库）+ 兜底方向与"看不见必须点名"的自证。
func TestEventProducerScannerLegs(t *testing.T) {
	dir := t.TempDir()
	writeFixtureGo(t, dir, "db/draft.go", `package db

type EventDraft struct {
	EventID string
	Type    string
}
`)
	writeFixtureGo(t, dir, "journal/events.go", `package journal

const (
	A       = "alpha_created"
	B       = "beta_created"
	Running = "running_state"
	Waiting = "waiting_state"
	LocalDup  = "wrong_value"
	Wide      = "wide_only_state"
)
`)
	// 甲之一：常量直接上发射位。
	writeFixtureGo(t, dir, "svc/a.go", `package svc

import (
	"x/db"
	"x/journal"
)

func a() db.EventDraft { return db.EventDraft{Type: journal.A} }
`)
	// 甲之二：裸字面量上发射位（我 09-25 那版规则漏的就是它）。
	writeFixtureGo(t, dir, "svc/b.go", `package svc

import "x/db"

func b() db.EventDraft { return db.EventDraft{Type: "beta_created"} }
`)
	// 乙：只有定义位 + 注册表里那个 true 条目 ⇒ 必须算无生产者。
	writeFixtureGo(t, dir, "svc/c.go", `package svc

const gamma = "gamma_created"

var extra = map[string]bool{gamma: true}
`)
	// 丙：T-355 的汇聚位 —— 发射位拿到的是变量，值来自同文件那个 switch 函数。
	writeFixtureGo(t, dir, "svc/agg.go", `package svc

import (
	"x/db"
	"x/journal"
)

func pick(to string) string {
	var typ string
	switch to {
	case "running":
		typ = journal.Running
	case "waiting":
		typ = journal.Waiting
	default:
		typ = journal.A
	}
	return typ
}

func emit(to string) db.EventDraft {
	evType := pick(to)
	return db.EventDraft{Type: evType}
}
`)
	// 丁：参数直传，一个候选都定不到 ⇒ 必须逐个点名，不许悄悄把谁判成无生产者。
	writeFixtureGo(t, dir, "svc/d.go", `package svc

import "x/db"

func d(v string) db.EventDraft { return db.EventDraft{Type: v} }
`)

	// 戊：T-365② 的反例。它进 values（我确实看见了）、不许进 covered/emitted（它不是任何词表名字的生产者）。
	writeFixtureGo(t, dir, "svc/e.go", `package svc

import "x/db"

func e() db.EventDraft { return db.EventDraft{Type: "task: %s"} }
`)
	// 己：T-365④ 的反例。`Type: t` 定不到候选 = 盲区；**同包另一个文件**里那个调用点把
	// `local_only_state` 喂进来 ⇒ 影响面要报出来，覆盖集不许跟着涨。
	// 调用点必须放在另一个文件：放同一个文件就会被 lits 兜底先洗掉（09-27 第一版就是这么暴露出兜底偏宽的）。
	writeFixtureGo(t, dir, "svc/f.go", `package svc

import "x/db"

func emitLocal(t string) db.EventDraft { return db.EventDraft{Type: t} }
`)
	writeFixtureGo(t, dir, "svc/g.go", `package svc

import "x/db"

func callIt() db.EventDraft { return emitLocal("local_only_state") }
`)
	// 庚：T-371① 的"窄"那一头。`Type: p` 的实参是**函数返回值** ⇒ 第四层（只吃常量/字面量）必须拒绝，
	// 这一处落「未决」；宽回解能从同包那张包级函数值表报出"它牵连 pending_via_state" ⇒ 两头都不许站。
	// 汇聚函数必须写在**另一个文件**：写进发射位同一个文件，它的字面量会被本文件兜底先捞走（第一版就踩在这上面）。
	writeFixtureGo(t, dir, "svc/h.go", `package svc

import "x/db"

func emitParam(p string) db.EventDraft { return db.EventDraft{Type: p} }
`)
	writeFixtureGo(t, dir, "svc/i.go", `package svc

import "x/db"

func pickPending() string { return "pending_via_state" }

func callUnknown() db.EventDraft { return emitParam(pickPending()) }
`)
	// 辛：只能靠"同文件字面量"说话的位点 ⇒ 未决。以前兜底会把 note() 里那个词表字面量算成覆盖（假绿方向）。
	writeFixtureGo(t, dir, "svc/k.go", `package svc

import "x/db"

func emitSink(p string) db.EventDraft { return db.EventDraft{Type: p} }

func note() string { return "sink_only_state" }
`)

	// 癸（第五层的"窄"那一头）：闭包 return 的第一位是**函数调用结果** ⇒ 第五层不许认。
	// 不钉这一头，"任何闭包都能造生产者"就是下一个假绿洞（Q4 变异实测 SILENT 才照出来）。
	writeFixtureGo(t, dir, "svc/wide.go", `package svc

import (
	"x/db"
	"x/journal"
)

func pickWide(x string) string { return journal.Wide }

type S2 struct{}

func (s *S2) run(name string, fn func(string) (string, any)) db.EventDraft {
	evType, _ := fn(name)
	return db.EventDraft{Type: evType}
}

func callWide(s *S2) db.EventDraft {
	return s.run("a", func(x string) (string, any) { return pickWide(x), nil })
}
`)

	// 壬（T-394 方向的反例）：名字从**闭包**里回出来 —— `advance(..., func(t) (string, any) { return journal.Running, nil })`。
	// 前四层看不见它；第五层必须并进来，否则这个名字会安静地躺在"无生产者"名单里（写进名单=写假事实）。
	writeFixtureGo(t, dir, "svc/adv.go", `package svc

import (
	"x/db"
	"x/journal"
)

func (s *S) advance(name string, fn func(string) (string, any)) db.EventDraft {
	evType, _ := fn(name)
	return db.EventDraft{Type: evType}
}

type S struct{}

func callAdv(s *S) db.EventDraft {
	return s.advance("running", func(x string) (string, any) { return journal.Running, nil })
}
`)
	cat := []string{"alpha_created", "beta_created", "gamma_created", "running_state", "waiting_state", "other_created",
		"local_only_state", "pending_via_state", "sink_only_state", "wide_only_state"}
	sc := runDraftScan(t, dir, cat)
	if sc.files < 13 {
		t.Fatalf("合成树只扫了 %d 个文件 ⇒ 覆盖面不对，后面的断言都无意义", sc.files)
	}
	for _, want := range []string{"alpha_created", "beta_created", "running_state", "waiting_state"} {
		if !sc.emitted[want] {
			t.Errorf("%s 该被认出有生产者却没认出（发射位 %d 处、未决 %d 处）", want, len(sc.sites), len(sc.pending))
		}
	}
	if sc.emitted["gamma_created"] {
		t.Errorf("只有定义位+注册表位的 gamma_created 被判成有生产者 ⇒ 这条腿会把名单越读越窄")
	}
	// 汇聚位必须以 alias/call 被认出，而不是靠兜底：否则真树那几处还是瞎的。
	agg := false
	for _, s := range sc.sites {
		if (s.kind == "alias" || s.kind == "call") && hasValue(s.values, "running_state") {
			agg = true
		}
	}
	if !agg {
		t.Errorf("没认出汇聚位（`Type: evType` ← pick 里那排 switch）；当前分类：%s", kindsOf(sc.sites))
	}
	if len(sc.pending) == 0 {
		t.Errorf("一处「未决」都没有 ⇒ 要么被兜底悄悄吞了、要么合成件没造出来")
	}
	for _, s := range sc.pending {
		if s.file == "" || s.line == 0 {
			t.Errorf("未决位点没带 file:line：%+v", s)
		}
	}
	mustDerived, _, _ := splitRoster(cat, sc.producible(), []string{"gamma_created"})
	// 癸这条腿：闭包回的是**计算值** ⇒ 既不许进覆盖，也不许被说成"有生产者"；它应当留在无生产者那一侧。
	if sc.emitted["wide_only_state"] {
		t.Errorf("wide_only_state 只出现在闭包 return 的函数调用结果上，被第五层算成了覆盖 ⇒ 这一层放宽成万能后门")
	}
	if !hasValue(mustDerived, "wide_only_state") {
		t.Errorf("wide_only_state 该留在派生的无生产者集合里却没在 ⇒ 第五层在暗处把它吞了")
	}
	// 壬这条腿（T-394）：闭包回出来的名字必须由第五层并进来，且单标 byL5；否则它会在名单里躺着说"没有生产者"。
	l5 := false
	for _, s := range sc.sites {
		if !hasValue(s.byL5, "running_state") {
			continue
		}
		l5 = true
		if !hasValue(s.covered, "running_state") {
			t.Errorf("byL5 有 running_state 却没进 covered ⇒ 第五层认到又没算数：%s", siteLine(s))
		}
	}
	if !l5 {
		t.Errorf("svc/adv.go 那处闭包没被第五层认出来（running_state 仍看不见）⇒ T-394 那一层空转：%s", kindsOf(sc.sites))
	}
	// 交集这一腿的自证（两个方向）：反例没被收进候选 = 空转；反例算进了覆盖 = 交集失效。
	junkSite := false
	for _, s := range sc.sites {
		if !hasValue(s.values, "task: %s") {
			continue
		}
		junkSite = true
		if hasValue(s.covered, "task: %s") {
			t.Errorf("词表外的候选 `task: %%s` 进了 covered ⇒ 交集没生效：%s", siteLine(s))
		}
		if s.dropped == 0 {
			t.Errorf("词表外的候选被收进来了却没报 M（N/M 两个数只剩一个）：%s", siteLine(s))
		}
	}
	if !junkSite {
		t.Errorf("戊那条合成件没被收进候选 ⇒ 这一腿空转")
	}
	for v := range sc.emitted {
		if !catalogSet(cat)[v] {
			t.Errorf("覆盖集长出了 §6 词表外的名字 %s ⇒ 这条判据对「谁算生产者」没有约束", v)
		}
	}
	// 第四层（T-371① 裁"算覆盖"）这一腿两个方向：
	// ① 同包调用点给的是字面量 ⇒ 必须认成生产者，且 kind=param、单标"仅第四层"；
	// ② 同包调用点给的是函数返回值 ⇒ 窄回解必须拒绝，这一处只能落「未决」，名字两头都不许站。
	l4 := false
	for _, s := range sc.sites {
		if !hasValue(s.byL4, "local_only_state") {
			continue
		}
		l4 = true
		if s.kind != "param" {
			t.Errorf("第四层认到的位点 kind 没标成 param（标的 %q）⇒ 读数里分不出它是哪层认的：%s", s.kind, siteLine(s))
		}
		if !hasValue(s.covered, "local_only_state") {
			t.Errorf("byL4 有 local_only_state 却没进 covered ⇒ 第四层认到又没算数：%s", siteLine(s))
		}
	}
	if !l4 {
		t.Errorf("svc/f.go + svc/g.go 那条第四层没认出来（形参 ← 同包字面量实参）⇒ 裁定的那一层空转：%s", kindsOf(sc.sites))
	}
	if !sc.emitted["local_only_state"] {
		t.Errorf("local_only_state 该被第四层认成有生产者却没认出 ⇒ 它会掉进无生产者名单，那是假事实")
	}
	if sc.emitted["pending_via_state"] {
		t.Errorf("pending_via_state 的实参是函数返回值，第四层按裁定只吃常量/字面量 ⇒ 不该算覆盖")
	}
	if !hasValue(sc.pendingNames, "pending_via_state") {
		t.Errorf("pending_via_state 没进「未决」名字集合 ⇒ 未决被吞掉了，下一轮它会以假红面目出现：%s", kindsOf(sc.sites))
	}
	if len(sc.pending) < 3 {
		t.Errorf("丁（形参无调用点）、庚（实参是函数产出）、辛（只有同文件字面量可依赖）三处都该落未决位点集合，现在只有 %d 处：%s",
			len(sc.pending), kindsOf(sc.sites))
	}
	// 辛这条腿：兜底不许再替未决说话（它曾经把同文件的词表字面量算成覆盖，那是假绿方向）。
	if sc.emitted["sink_only_state"] {
		t.Errorf("sink_only_state 只是与发射位同文件的一个字符串，被兜底算成了覆盖 ⇒ 未决被洗白")
	}
	if !hasValue(sc.pendingNames, "sink_only_state") {
		t.Errorf("sink_only_state 没进「未决」名字集合 ⇒ 兜底降级没生效，它会以假红面目进名单：%s", kindsOf(sc.sites))
	}
	// 「未决」牵连的名字只从派生集合里摘出去，不算有生产者 —— 用 producible() 才是真树那一条的形状。
	dv0, _, _ := splitRoster(cat, sc.producible(), []string{"gamma_created"})
	if hasValue(dv0, "sink_only_state") {
		t.Errorf("sink_only_state 被判成了无生产者 ⇒ 兜底降级后它该挂未决")
	}
	if hasValue(dv0, "pending_via_state") {
		t.Errorf("pending_via_state 被判成了无生产者 ⇒ 「未决」与「无生产者」没分开，这是要写进名单的假事实")
	}
	if !hasValue(dv0, "gamma_created") {
		t.Errorf("gamma_created 只有定义位，该在派生的无生产者集合里却没在 ⇒ 派生集合漂了：%v", dv0)
	}
	// 名单对拍两条腿：相等时无差集；漏登记与多登记都要各自点名。
	_, m0, e0 := splitRoster(cat, sc.producible(), []string{"gamma_created", "other_created", "wide_only_state"})
	if len(m0) != 0 || len(e0) != 0 {
		t.Errorf("名单与派生集合相等时不许报差集：missing=%v extra=%v", m0, e0)
	}
	_, m1, e1 := splitRoster(cat, sc.producible(), []string{"alpha_created"})
	if !hasValue(e1, "gamma_created") || !hasValue(m1, "alpha_created") {
		t.Errorf("两个方向的差集没各自点名：missing=%v extra=%v", m1, e1)
	}
	_, _, e2 := splitRoster([]string{"gamma_created"}, map[string]bool{}, []string{"not_in_catalog"})
	if j := strings.Join(e2, ","); !strings.Contains(j, "not_in_catalog") {
		t.Errorf("名单写了 §6 里没有的名字却没点名：extra=%v", e2)
	}
}

// TestProducerRosterParserLegs 钉 T-385③ 那件事：锚句多命中取最后一句、自称条数与实取必须相等、锚不到必须停。
// 四条腿各管一个失效方向；变异在 .scratch/test/mut_t385.py（取第一句 / 自检关掉 / 锚不到返回空名单）。
func TestProducerRosterParserLegs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "contract.md")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("写合成契约失败：%v", err)
		}
	}
	// 腿一（REQ-T37 裁甲之后翻转）：两句同锚 = 当前态不唯一 ⇒ **必须停**，不再"取最后一句"。
	// 这条断的是旧行为，改行为前先 grep 到有它在断，才没有悄悄把判据换掉。
	write("> 所以本名单当前是**三条**：`a_created` / `b_created` / `c_created`。\n" +
		"中间\n> 所以本名单当前是 **4 条**（取代上面那句\"三条\"）：`a_created` / `b_created` / `c_created` / `d_created`。\n")
	if got, err := parseProducerRoster(t, p); err == nil {
		t.Errorf("两句同锚却照常返回（读到 %d 个）⇒ 硬闸没生效，以后有人补一句就会静默读到错的那句", len(got))
	}
	// 腿一乙（该放行必须放行）：单句命中要正常读全 —— 真契约 09-30 起就是这个形状（锚数 1）。
	write("> 所以本名单当前是 **两条**：`a_created` / `b_created`。\n")
	if got, err := parseProducerRoster(t, p); err != nil || len(got) != 2 {
		t.Errorf("单句锚读不出 2 个名字（got=%d err=%v）⇒ 硬闸拦错了东西，真树会假红", len(got), err)
	}
	// 腿二：自称条数与实取名字数不一致 ⇒ 必须停（形状与内容脱节的唯一自动喊法）。
	write("> 所以本名单当前是 **9 条**：`a_created` / `b_created`。\n")
	if _, err := parseProducerRoster(t, p); err == nil {
		t.Errorf("锚句自称 9 条、实取 2 个却照常返回 ⇒ 自检没生效，下一次脱节没人喊")
	}
	// 腿三：锚不到 ⇒ 必须停。这是主控点名要保留的 fail-closed 方向。
	write("> 名单挪到别处去了。\n")
	if _, err := parseProducerRoster(t, p); err == nil {
		t.Errorf("锚不到却返回了名单 ⇒ 这条判据可以凭空气跑")
	}
	// 腿四：中文数字必须**真的参与那次比较**。第一版这条腿是空的 —— 把 cnNumber 摘掉，
	// 走的是"认不出就只报不拦"那一支，测试照样绿（变异 N4 实测 SILENT 才照出来）。
	// 所以改成"自称三条却列四个 ⇒ 必须停"：认不出「三」就停不下来，这才算咬住。
	write("> 所以本名单当前是**三条**：`a_created` / `b_created` / `c_created` / `d_created`。\n")
	if _, err := parseProducerRoster(t, p); err == nil {
		t.Errorf("自称「三条」实列 4 个却没停 ⇒ 中文数字没进自检，那道检查是摆设")
	}
	if n, ok := declaredRosterCount("> 所以本名单当前是**二十七条**：`a_created`。"); !ok || n != 27 {
		t.Errorf("组合中文数字「二十七」没认成 27（got=%d ok=%v）⇒ 名单上百条时没人对得上分母", n, ok)
	}
	if n, ok := declaredRosterCount("> 所以本名单当前是**三条**：`a_created`。"); !ok || n != 3 {
		t.Errorf("「三条」没认成 3（got=%d ok=%v）", n, ok)
	}
}

func hasValue(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func kindsOf(sites []draftTypeSite) string {
	var b []string
	for _, s := range sites {
		b = append(b, siteLine(s))
	}
	return strings.Join(b, "; ")
}

func writeFixtureGo(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("建 fixture 目录失败：%v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("写 fixture %s 失败：%v", rel, err)
	}
}

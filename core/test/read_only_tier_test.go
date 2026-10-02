package test

// T-450：给契约② §16.7「只读档」加三枚机械判据。形状照 wiring_surface 那一族立：
// 输入**只读契约**（脚本里不嵌清单快照）、**几种红分开报**、读不到锚就停（看不见东西的判据不许报绿）。
//
// 　① 档位形状：被登记成 READ_ONLY 的包里不得出现写链的**构造**；
//    「根本没实现」与「实现了但走不到」分开报 —— 前者是合规范围，后者才是自扩范围。
// 　② 严禁假成功：只读档写链的答复必须是 §15.1 **已登记**的码；新造一个码就红。
//    （派发半边「返回 2xx 就是红」今天**没有可派发的对象**：只读档那个包还不存在，见 ① 的诊断。）
// 　③ 额度三档：0 与 NULL 与有值是三件事（§7），这一枚用真数据钉，不再只留在文档里。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// contractLine 从契约里按锚字取一段文本（锚不在就停，不猜）。
func contractText(t *testing.T, rel, anchor string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "specs", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("契约读不到 %s：%v ⇒ 这几枚判据没有输入源", rel, err)
	}
	ls := strings.Split(string(raw), "\n")
	for i, l := range ls {
		if strings.Contains(l, anchor) {
			return sectionOf(ls[i:])
		}
	}
	t.Fatalf("契约 %s 里找不到锚字 %q ⇒ 那一节挪了位置或改了名，判据不猜", rel, anchor)
	return nil
}

// sectionOf 把「从锚字到文件尾」收成「从锚字到下一个同级/更高一级标题」—— 不切这一刀，
// 后面别的小节点到的码名会被当成 §16.7 说的话（我第一版就是这么误判的）。
func sectionOf(ls []string) []string {
	for i := 1; i < len(ls); i++ {
		if strings.HasPrefix(ls[i], "## ") || strings.HasPrefix(ls[i], "### ") {
			return ls[:i]
		}
	}
	return ls
}

var codeCell = regexp.MustCompile("`([A-Z][A-Z0-9_]{3,})`")

// nameCell 认 `session/create`、`guide`、`start_now` 这类反引号里的方法/模式名。
var nameCell = regexp.MustCompile("^[a-z][a-z0-9_]*(?:/[a-z0-9_]+)*$")

// parse15Codes 读 §15.1 那张码表的「码」列（第一格反引号里的大写名）。
func parse15Codes(t *testing.T) map[string]bool {
	t.Helper()
	sec := contractText(t, "adapter-protocol/adapter-protocol-v0.1.md", "### 15.1 Adapter 层码")
	out := map[string]bool{}
	for _, l := range sec[1:] {
		if strings.HasPrefix(l, "#### ") || (strings.HasPrefix(l, "## ") && !strings.HasPrefix(l, "###")) {
			break // 小节结束
		}
		cells := strings.Split(l, "|")
		if len(cells) < 3 {
			continue
		}
		first := strings.Trim(strings.TrimSpace(cells[1]), "`")
		if first == "码" || strings.HasPrefix(first, "--") {
			continue
		}
		if m := codeCell.FindStringSubmatch(cells[1]); m != nil {
			out[m[1]] = true
		}
	}
	if len(out) < 8 {
		t.Fatalf("§15.1 只解析出 %d 个码 ⇒ 表改了形状，这条判据现在是永真式（下界 8）", len(out))
	}
	t.Logf("§15.1 读到 %d 个已登记码（唯一输入源=契约② 那张表）", len(out))
	return out
}

// parseForbiddenWrites 从 §16.7 第 1 条读「不实现也不接」那份方法名清单。
func parseForbiddenWrites(t *testing.T) []string {
	t.Helper()
	sec := contractText(t, "adapter-protocol/adapter-protocol-v0.1.md", "### 16.7")
	for _, l := range sec {
		if !strings.Contains(l, "不实现也不接") {
			continue
		}
		var names []string
		for _, seg := range strings.Split(l, "`") {
			s := strings.TrimSpace(seg)
			if nameCell.MatchString(s) {
				names = append(names, s)
			}
		}
		if len(names) < 5 {
			t.Fatalf("§16.7 第 1 条只读到 %d 个「不实现也不接」的方法名 ⇒ 那一行改了形状", len(names))
		}
		t.Logf("从 §16.7 读到 %d 个写链方法名（唯一输入源=契约② 那一条）：%s", len(names), strings.Join(names, " "))
		return names
	}
	t.Fatalf("§16.7 里没有「不实现也不接」那一行 ⇒ 只读档的范围被改写，判据不猜")
	return nil
}

// readOnlySubjects 找非测试码里**支持 READ_ONLY 档**的包，排除声明它自己的那个包。
// usesReadOnlyTier：只有把只读档用到能力声明/判定里的文件，才算档位使用者。
func usesReadOnlyTier(s string) bool {
	return strings.Contains(s, "adapter.LevelReadOnly") || strings.Contains(s, "SupportLevel: LevelReadOnly") ||
		strings.Contains(s, "SupportLevel: adapter.LevelReadOnly")
}

func readOnlySubjects(t *testing.T, coreRoot string) []string {
	t.Helper()
	set := map[string]bool{}
	filepath.Walk(coreRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		s := string(b)
		// 声明/枚举那个常量本身的包不算对象（它是词表，不是档位使用者）
		if !strings.Contains(s, "LevelReadOnly") && !strings.Contains(s, `"READ_ONLY"`) {
			return nil
		}
		// 词表/映射包不算对象：handshake.go 在**声明**那个档位、protocol.go 在**映射**那个 JSON 码，
		// 都不是"以只读档行事"的适配器包（我第一版把 internal/adapter 当成对象，是误判）
		if strings.HasPrefix(relTo(coreRoot, p), "internal/adapter/") || !usesReadOnlyTier(s) {
			return nil
		}
		d, _ := filepath.Rel(coreRoot, filepath.Dir(p))
		set[filepath.ToSlash(d)] = true
		return nil
	})
	var out []string
	for d := range set {
		out = append(out, d)
	}
	return out
}

// TestReadOnlyTierBuildsNoWriteChain 是 ①。两种红/一种合规分开：
// 　· 构造（出现在调用实参里）⇒ 红；
// 　· 只登记在映射表或分支里 ⇒ 「实现了但走不到」，点名不判红；
// 　· 一个都没出现 ⇒ 「根本没实现」，正是 §16.7 第 1 条要的形态。
func TestReadOnlyTierBuildsNoWriteChain(t *testing.T) {
	coreRoot, src := wiringCodeRoot(t)
	forbidden := parseForbiddenWrites(t)
	subjects := readOnlySubjects(t, coreRoot)
	t.Logf("代码侧扫描根=%s（来源=%s）；READ_ONLY 档的使用者包 = %v", coreRoot, src, subjects)
	if len(subjects) == 0 {
		t.Errorf("① 采不到对象：**0 个**非测试包把只读档用到能力声明/判定里 ⇒ 这一条今天没有覆盖面，" +
			"不算绿也不算被绕过。只读档适配器一落地它就开始咬；我不把它降成打印，因为" +
			"「只读」被后人读成「暂时没做完」正是 §16.7 开头要挡的那件事")
		return
	}
	for _, pkg := range subjects {
		pkg := pkg
		t.Run(pkg, func(t *testing.T) {
			constructed, mentioned := scanTierPackage(t, coreRoot, pkg, forbidden)
			if len(constructed) > 0 {
				t.Errorf("① 只读档包里出现了**写链的构造**（%d 处）：%s ⇒ 越过 §16.7 第 1 条自扩范围了",
					len(constructed), strings.Join(constructed, "、"))
			}
			t.Logf("① 包 %s：构造 %d 处（判红）／仅登记在映射或分支里 %d 处（「实现了但走不到」那一档，只点名）：%s",
				pkg, len(constructed), len(mentioned), strings.Join(mentioned, "、"))
		})
	}
}

// scanTierPackage 两遍扫：先把"作为调用实参出现"的位置收进 constructed，
// 再把其余同名串收进 mentioned（第二版才有这个 —— 第一版只扫实参，
// 于是 map 字面量里的 `guide` 被算成「什么都没提」，那是个假干净的读法）。
func scanTierPackage(t *testing.T, coreRoot, pkg string, forbidden []string) ([]string, []string) {
	t.Helper()
	isForbidden := func(v string) bool {
		for _, m := range forbidden {
			if v == m {
				return true
			}
		}
		return false
	}
	var constructed, mentioned []string
	filepath.Walk(filepath.Join(coreRoot, filepath.FromSlash(pkg)), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(p, ".go") ||
			strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return nil
		}
		argPos := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, a := range ce.Args {
				if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					argPos[fset.Position(lit.Pos()).String()] = true
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v := strings.Trim(lit.Value, `"`+"`"+`"`)
			if !isForbidden(v) {
				return true
			}
			where := relTo(coreRoot, p) + ":" + fmt.Sprint(fset.Position(lit.Pos()).Line)
			if argPos[fset.Position(lit.Pos()).String()] {
				constructed = append(constructed, v+" @"+where)
			} else {
				mentioned = append(mentioned, v+" @"+where)
			}
			return true
		})
		return nil
	})
	return constructed, mentioned
}

// parseDomainCodes 读实现侧那份领域码表（`internal/adapter/protocol.go` 的 `domainCodes`），
// 只取 map 字面量里的字符串值 —— 名字仍然只从代码里读，脚本不嵌清单。
func parseDomainCodes(t *testing.T) []string {
	t.Helper()
	coreRoot, _ := wiringCodeRoot(t)
	full := filepath.Join(coreRoot, "internal", "adapter", "protocol.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, full, nil, 0)
	if err != nil {
		t.Fatalf("读不到 %s：%v ⇒ ② 没有可核对的实现侧码表", relTo(coreRoot, full), err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for _, nm := range vs.Names {
			if nm.Name != "domainCodes" {
				continue
			}
			for _, cv := range vs.Values {
				lit, ok := cv.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, e := range lit.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if s, ok := kv.Value.(*ast.BasicLit); ok && s.Kind == token.STRING {
						out = append(out, strings.Trim(s.Value, `"`))
					}
				}
			}
		}
		return true
	})
	if len(out) < 8 {
		t.Fatalf("实现侧 domainCodes 只读到 %d 枚 ⇒ 那张表改了形状，② 现在是永真式（下界 8）", len(out))
	}
	return out
}

// TestReadOnlyWriteExitIsRegisteredCode 是 ② 的机械半（"不许新造码"那一半）。
// 为什么不断"§16.7 点名的码"：它故意把选型交给 backend（"具体码由既有表里取"），
// 所以那一节**永远不会**点名 —— 我第一版在这里 t.Fatalf，是把一个设计当成了缺陷（记这一笔）。
// 真正会咬人的是反方向：实现映射给 Core 的名字，必须每一枚都在 §15.1 那张表里。
func TestReadOnlyWriteExitIsRegisteredCode(t *testing.T) {
	codes := parse15Codes(t)
	impl := parseDomainCodes(t)
	if !codes["WRITE_ENTRY_NONE"] && !codes["CAPABILITY_UNAVAILABLE"] {
		t.Errorf("② §15.1 里既没有 WRITE_ENTRY_NONE 也没有 CAPABILITY_UNAVAILABLE ⇒ 只读档写链的" +
			"「能力不可用」答复**没有已登记出口**，那 2xx 就会变成默认（§16.7 第 2 条要挡的就是这个）")
	}
	var invented []string
	for _, n := range impl {
		if !codes[n] {
			invented = append(invented, n)
		}
	}
	if len(invented) > 0 {
		t.Errorf("② 实现把 %d 枚名字当领域码发给 Core，可它们不在 §15.1 那张表里：%s ⇒ 就是「新造码」，"+
			"要么补进契约② §15.1、要么改掉实现里的名字", len(invented), strings.Join(invented, " "))
	}
	var unused []string
	seen := map[string]bool{}
	for _, n := range impl {
		seen[n] = true
	}
	for n := range codes {
		if !seen[n] {
			unused = append(unused, n)
		}
	}
	// 反方向只打印不判红：表里登记了、实现还没映射，可能是 Provider 侧才用得上的（我不替它编理由，也不假装它错）
	t.Logf("② §15.1 登记 %d 枚；实现侧 domainCodes %d 枚；表里有而实现未映射（只报数，不判红）%d 枚：%s",
		len(codes), len(impl), len(unused), strings.Join(unused, " "))
}

// TestQuotaTiersSeparateZeroFromUnknown 是 ③：三档真数据，放行腿与拦下腿都在，
// 而且拦下腿必须**因为所说的那个理由**红 —— 我第一版的夹具漏了 OverallConfidence，
// 两格"拦下"其实是被 overall_confidence 那句话拦的，那种绿没有意义（同一族错我 09-30 记过）。
func TestQuotaTiersSeparateZeroFromUnknown(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	row := func(used, rem *float64, conf string) adapter.QuotaObservation {
		return adapter.QuotaObservation{QuotaKey: "qoder.session", QuotaType: "tokens",
			ObservationKind: adapter.ObservationObserved, UsedPercent: used, RemainingPercent: rem,
			Confidence: conf, Source: "provider.local_data"}
	}
	mk := func(obs []adapter.QuotaObservation, overall string) adapter.QuotaResult {
		return adapter.QuotaResult{Observations: obs, OverallConfidence: overall}
	}
	high, med := adapter.ConfidenceHigh, adapter.ConfidenceMedium
	cases := []struct {
		name   string
		q      adapter.QuotaResult
		want   bool
		needle string
		reason string
	}{
		{"档一·有值（62.5/37.5 + HIGH）",
			mk([]adapter.QuotaObservation{row(f(62.5), f(37.5), high)}, high), true, "",
			"真数据必须放行，否则这条腿会把合法观测也吃掉"},
		{"档二·cost=0 被观测成 HIGH（整行全零）",
			mk([]adapter.QuotaObservation{row(f(0), f(0), high)}, high), false, "used_percent",
			"什么都不知道的 Provider 就长这样：必须落 UNKNOWN，不是 OBSERVED 0%"},
		{"档三·NULL 被产出成一行 0 值观测（全字段 nil）",
			mk([]adapter.QuotaObservation{row(nil, nil, med)}, med), false, "carries no value",
			"取不到的字段是 null+降置信度；整行无值 = 这条观测不该存在"},
		{"整包读不到：observations 空 + UNKNOWN",
			mk([]adapter.QuotaObservation{}, adapter.ConfidenceUnknown), true, "",
			"§7 给「什么都读不到」留的那一格"},
		{"空观测却报 HIGH",
			mk([]adapter.QuotaObservation{}, high), false, "must report overall_confidence UNKNOWN",
			"没有观测就没有高置信度"},
		{"有观测却整体 UNKNOWN",
			mk([]adapter.QuotaObservation{row(f(10), f(90), high)}, adapter.ConfidenceUnknown), false, "reserves UNKNOWN",
			"UNKNOWN 被留给空答复"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.q.Validate()
			if tc.want && err != nil {
				t.Errorf("该放行没放行：%v ⇒ %s", err, tc.reason)
			}
			if !tc.want {
				if err == nil {
					t.Errorf("该拦下没拦下：%s", tc.reason)
				} else if !strings.Contains(err.Error(), tc.needle) {
					t.Errorf("拦是拦了，理由却不是那一件：%v（要它因为「%s」而拦）⇒ %s", err, tc.needle, tc.reason)
				}
			}
			t.Logf("%-44s 放行=%v err=%v", tc.name, err == nil, err)
		})
	}
	// Provider 各自四舍五入的那点差必须放行，否则合法数据会被我这条判据统计成违规。
	if err := mk([]adapter.QuotaObservation{row(f(62.55), f(37.45), high)}, high).Validate(); err != nil {
		t.Errorf("容差内（62.55+37.45）被拒：%v", err)
	}
}

package test

// 本文件是契约② 判据表 C8-6 / C8-7 / C8-10 / C8-11 / C8-12 / C8-13 / C8-14 七条行的执行者。
// 判据文本住在契约里，这里只留指针（REQ-T10）；两处口径若漂，以契约为准
// （本文件与 lint 都只是它的执行者）。
//
// C8-6/C8-7 为什么单独在 Go 里再来一遍（lint 的 check 8 早就比过这对集合）：
// 主控 09-22 指出 `grep -rn "15\.2" core/test/*.go` 零命中 —— 那条判据只在"有人跑我的 lint"时才活着，
// 而 `go test ./test/...` 是所有人的默认入口。判据住在契约、执行者得在默认入口里。
//
// 全部用例**都不用 t.Fatal**：本文件是 T-124 那族错的整改对象 —— 嵌套子用例会整段被父级 `t.Fatal`
// 静默吞掉（"包 ok"但它没跑）。所以每条都是独立顶层用例 + 只用 t.Errorf，
// 解析器坏了也只是各条各自报红，不会互相遮掉。

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var codeNameRE = regexp.MustCompile(`^[A-Z][A-Z_]{3,}$`)

// snakeLowerRE 是"看起来像一个小写码名/事件名"的形状（§15.2 左列与事件目录共用）。
// 用它做闸门：解析出来的东西不符合形状 ⇒ 判自己坏了，不拿半张表去判人。
var snakeLowerRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// contractPath 找适配器协议文档：不写死相对层数，向上找到仓库根再拼。
func contractPath(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	if root == "" {
		return ""
	}
	p := filepath.Join(root, "specs", "adapter-protocol", "adapter-protocol-v0.1.md")
	if !fileExists(p) {
		t.Errorf("%s 不在了（契约文档改名要回写判据表）", p)
		return ""
	}
	return p
}

// repoRoot 从工作目录向上找同时含 `core/` 与 `specs/` 的那一层。
// 为什么认两个目录而不是认 `.git`：判据读的是这两棵树，缺哪棵都说明锚点找错了地方。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Errorf("拿不到工作目录: %v", err)
		return ""
	}
	for {
		if isDir(filepath.Join(dir, "core")) && isDir(filepath.Join(dir, "specs")) {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Errorf("从 %s 往上找不到同时含 core/ 与 specs/ 的仓库根", dir)
			return ""
		}
		dir = up
	}
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// sliceSection 取两个标题锚点之间的行（锚点是**标题文本**，不是行号）。
// 找不到锚点就返回 err —— 那正是"静默空集"必须报警的地方（我今天栽过：锚点命中代码块里的示例）。
func sliceSection(text, fromPrefix, toPrefix string) ([]string, error) {
	var out []string
	seen := false
	for _, line := range strings.Split(text, "\n") {
		if !seen {
			if strings.HasPrefix(line, fromPrefix) {
				seen = true
			}
			continue
		}
		if strings.HasPrefix(line, toPrefix) {
			return out, nil
		}
		out = append(out, line)
	}
	if !seen {
		return nil, errAnchor(fmt.Sprintf("找不到起点锚 %q（标题措辞改了？），拒绝当作空集", fromPrefix))
	}
	return out, errAnchor(fmt.Sprintf("找到了起点锚 %q 但没有终点锚 %q ⇒ 区间会吃到文件尾，不可信", fromPrefix, toPrefix))
}

type anchorError string

func (e anchorError) Error() string { return string(e) }

func errAnchor(s string) error { return anchorError(s) }

// codeTables 是一次解析读到的全部东西。为什么用结构体而不是五个返回值：
// C8-10 的形状判据比的是**行数**，而 C8-6/C8-7 的集合判据比的是**去重后的码名** ——
// 同一列的两种数法必须分开带出来，混成一个数就会在"两行引用同一个码"那天假红。
type codeTables struct {
	codes151     []string // §15.1 码列（去重排序）
	named        []string // §15.2『来自』列里的码名（去重排序）
	namedRows    int      // 『来自』带码名的行数（不去重）
	dashRows     int      // 『来自』以 `—` 开头的行数
	totalRows    int      // §15.2 数据行总数
	skipped      int      // 长得像表格行却一行都没计入的次数（C8-10 ① 的落点）
	namedRaw     int      // 『来自』码名的原始条数（与去重后的差 = 违反 §15.2.2 的一对一，C8-14 判红）
	transportRaw int      // §15.2 左列的原始条数（去重前）—— 这一列重复是漂移，已在解析时判红
	transport    []string // §15.2 左列 = HTTP 面自己会发的码（去重排序，C8-11 的比对集）
	namedAt      []httpNamePair
}

// httpNamePair 是 §15.2 一行里"左列 HTTP 码 ↔『来自』Adapter 码名"这一对。
// C8-14 要"出现两次即红并点名两行"，所以解析时就得把行身份留下 ——
// 只交一个去重集合，红话就说不出是谁和谁撞了（左列本身唯一，C8-10 已判，所以拿它当行身份）。
type httpNamePair struct {
	httpCode string // 左列
	adapter  string // 『来自』列的 Adapter 码名；`—` 行不产这一项（不是被过滤掉，是结构上不出现）
}

// parseCodeTables 解析 §15.1 的码列与 §15.2 的两列。
//
// 一行"计没计入"必须自己说清楚：探针 M-C 那天我把某行的左列反引号拿掉，
// 三个数（总行数 / 码名行 / `—` 行）同时少 1、彼此依旧闭合 ⇒ C8-10 ① 一声不响，
// 只有 C8-11 因为"代码在发的那个码从表里消失了"才红。所以"看着像表格行却一行都没计入"
// 必须单独计数并判红 —— 那才是 ① 想防的"第三种形状"，求和式自己碰不到它。
func parseCodeTables(t *testing.T) (codeTables, error) {
	t.Helper()
	var ct codeTables
	path := contractPath(t)
	if path == "" {
		return ct, errAnchor("契约文档路径没找到")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return ct, errAnchor(fmt.Sprintf("读 %s 失败: %v", path, e))
	}
	text := string(raw)
	s151, e := sliceSection(text, "### 15.1 ", "#### 15.1.1")
	if e != nil {
		return ct, e
	}
	s152, e := sliceSection(text, "### 15.2", "### 15.3")
	if e != nil {
		return ct, e
	}
	return parseCodeRows(s151, s152)
}

// parseCodeRows 是 §15.1/§15.2 两张表的纯解析：喂行、出集合，不碰文件。
// 为什么要拆出来：§1.1 第 16 条要求「喂一行重名进解析器必须红」，
// 而只有真数据能红的判据等于没闭上（C8-12/C8-13 那条腿早就是这么做的）。
func parseCodeRows(s151, s152 []string) (codeTables, error) {
	var ct codeTables
	for _, line := range s151 {
		if c := firstCellMatch(line, codeNameRE); c != "" {
			ct.codes151 = append(ct.codes151, c)
		}
	}
	for _, line := range s152 {
		cells := strings.Split(line, "|")
		if !strings.HasPrefix(line, "| `") {
			switch {
			case !strings.HasPrefix(line, "|"), strings.HasPrefix(line, "|---"), strings.Contains(line, "HTTP 码"):
				continue // 散文、分隔行、表头 —— 三种都不是数据行
			default:
				ct.skipped++ // 像表格行却不认识：静默少读一行，交给 C8-10 ① 判红
				continue
			}
		}
		if len(cells) < 4 {
			ct.skipped++
			continue
		}
		ct.totalRows++
		// 表格列的索引：split 出来的 cells[0] 是行首 `|` 之前的空串，所以 awk 的 $4 == cells[3]。
		// （第一版按 awk 的习惯写了 cells[4]，结果第 1 行就撞上"第三种形状"守卫 —— 守卫是对的，索引是我错的。）
		left := strings.NewReplacer(" ", "", "`", "").Replace(cells[1])
		if !snakeLowerRE.MatchString(left) {
			return ct, errAnchor(fmt.Sprintf("§15.2 第 %d 行的左列不像一个 HTTP 面码名：%q ⇒ 表形状变了", ct.totalRows, cells[1]))
		}
		ct.transport = append(ct.transport, left)
		from := strings.NewReplacer(" ", "", "`", "").Replace(cells[3])
		switch {
		case strings.HasPrefix(from, "—"):
			ct.dashRows++
		case codeNameRE.MatchString(from):
			ct.namedRows++
			ct.named = append(ct.named, from)
			ct.namedAt = append(ct.namedAt, httpNamePair{httpCode: left, adapter: from})
		default:
			return ct, errAnchor(fmt.Sprintf("§15.2 第 %d 行的『来自』既不是码名也不是 `—` 开头：%q ⇒ 解析形状变了", ct.totalRows, cells[3]))
		}
	}
	// §1.1 第 16 条第二条腿在错误码表上的落点：**该表按定义不许重名的那两列**判重复。
	// §15.1 是码的登记表、§15.2 左列是 HTTP 码 —— 两张都是"一行一个键"，重名就是漂移。
	if dup, ok := firstDup(ct.codes151); ok {
		return codeTables{}, errAnchor(fmt.Sprintf("§15.1 码列里 %s 出现两次 —— 集合双向相等看不见重复，只有这条腿看得见", dup))
	}
	if dup, ok := firstDup(ct.transport); ok {
		return codeTables{}, errAnchor(fmt.Sprintf("§15.2 左列（HTTP 码）里 %s 出现两次 —— 一行一个 HTTP 码是这张表的定义", dup))
	}
	// 『来自』那一列**在这里仍然不判重**，但不是"放行"：一对一是契约② §15.2.2 的语义，
	// 由 C8-14（`TestAdapterCodeMapsToSingleHTTPName`）专门判红并点名两行。
	// 这条腿只把 `namedAt`（行身份）留下来给它用 —— 解析器同时判两件事会让红话说不清是谁的判据。
	// 历史（保留一句，免得有人再把它改回"只打印"）：02:3x 曾裁为"只打印不判红"，
	// 因为那时契约没说话；03:1x 主控先把双射写进 §15.2.2、再立 C8-14 —— 先立语义再上判据。
	ct.namedRaw = len(ct.named)
	ct.transportRaw = len(ct.transport)
	ct.codes151 = sortedUniq(ct.codes151)
	ct.named = sortedUniq(ct.named)
	ct.transport = sortedUniq(ct.transport)
	return ct, nil
}

// firstDup 返回第一个出现两次以上的元素（集合比对的盲区专用：`sortedUniq` 会把重复悄悄吃掉）。
func firstDup(in []string) (string, bool) {
	seen := map[string]int{}
	var order []string
	for _, s := range in {
		if seen[s] == 0 {
			order = append(order, s)
		}
		seen[s]++
	}
	for _, s := range order {
		if seen[s] > 1 {
			return s, true
		}
	}
	return "", false
}

// firstCellMatch 取表格行的首列，只有它整体符合 re 才认（首列不是码名的行是表头/分隔行）。
func firstCellMatch(line string, re *regexp.Regexp) string {
	if !strings.HasPrefix(line, "| `") {
		return ""
	}
	cells := strings.Split(line, "|")
	if len(cells) < 2 {
		return ""
	}
	c := strings.NewReplacer(" ", "", "`", "").Replace(cells[1])
	if re.MatchString(c) {
		return c
	}
	return ""
}

func sortedUniq(in []string) []string {
	sort.Strings(in)
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// TestErrorCodeContractTablesParse 执行 C8-10：**判形状、不判字面行数**。
// 这一条红的时候，本文件其余各条的结论都不可信 —— 所以它必须自己独立成一个顶层用例（不能被谁吞掉）。
// 为什么把 C8-8 那三条字面量废掉：09-22 主控自己往 §15.2 收了 `duplicate_project_path`，
// 29/12 立刻红 —— 那是"把基线数字当判据"的代价，码表本来就该随功能长。
func TestErrorCodeContractTablesParse(t *testing.T) {
	// §1.1 第 16 条的"合成反例"那一半：重名必须喂得进解析器（只有真数据能红 = 没闭上）。
	// 五组里第 4 组是**过严守卫**：一对多是合法形状，解析器不许顺手把它判红。
	dupCases := []struct {
		name         string
		s151, s152   []string
		wantErr      bool
		namedRawWant int
		why          string
	}{
		{
			name:    "正常两行",
			s151:    []string{"| `TIMEOUT` | -32001 | Adapter | ✗ |", "| `INTERNAL` | -32000 | Adapter | ✗ |"},
			s152:    []string{"| `timeout` | 504 | `TIMEOUT` |", "| `internal_error` | 500 | `INTERNAL` |"},
			wantErr: false, namedRawWant: 2, why: "两行两码，token 数与去重数相等",
		},
		{
			name:    "§15.1 码列重名（第 16 条主案）",
			s151:    []string{"| `TIMEOUT` | -32001 | Adapter | ✗ |", "| `TIMEOUT` | -32002 | Adapter | ✗ |"},
			s152:    []string{"| `timeout` | 504 | `TIMEOUT` |"},
			wantErr: true, why: "集合双向相等看不见重复，只有这条腿看得见",
		},
		{
			name:    "§15.2 左列重名",
			s151:    []string{"| `TIMEOUT` | -32001 | Adapter | ✗ |"},
			s152:    []string{"| `not_found` | 404 | `TIMEOUT` |", "| `not_found` | 409 | `TIMEOUT` |"},
			wantErr: true, why: "一行一个 HTTP 码是这张表的定义",
		},
		{
			name:    "一对多合法（过严守卫）",
			s151:    []string{"| `SESSION_NOT_FOUND` | -32003 | Adapter | ✗ |"},
			s152:    []string{"| `not_found` | 404 | `SESSION_NOT_FOUND` |", "| `session_gone` | 410 | `SESSION_NOT_FOUND` |"},
			wantErr: false, namedRawWant: 2, why: "两个 HTTP 码映射到同一个 Adapter 码是合法形状 ⇒ 只报原始/去重两个数，不判红",
		},
		{
			name:    "第三种形状仍然报错",
			s151:    []string{"| `TIMEOUT` | -32001 | Adapter | ✗ |"},
			s152:    []string{"| `timeout` | 504 | not_a_code_name |"},
			wantErr: true, why: "『来自』既不是码名也不是 — ⇒ 表形状变了，不静默",
		},
	}
	for _, d := range dupCases {
		got, err := parseCodeRows(d.s151, d.s152)
		switch {
		case d.wantErr && err == nil:
			t.Errorf("第二条腿失效：%s 本该红却解析成功（named=%v）—— %s", d.name, got.named, d.why)
		case !d.wantErr && err != nil:
			t.Errorf("判据过严：%s 本该通过却报错：%v —— %s", d.name, err, d.why)
		case !d.wantErr && d.namedRawWant > 0 && got.namedRaw != d.namedRawWant:
			t.Errorf("原始计数不对：%s 期望 namedRaw=%d 实得 %d（这条数要留给人才看得见漂移）",
				d.name, d.namedRawWant, got.namedRaw)
		}
	}
	ct, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("解析器坏了（本文件其余各条此刻不可信）：%v", err)
		return
	}
	// ① 形状闭合：两种形状加起来必须等于总行数，且不许有"像表格行却一行都没计入"的行。
	//    两边都用**行数**：拿"去重后的码名数"去比行数，两行引用同一个码就会假红。
	if ct.namedRows+ct.dashRows != ct.totalRows {
		t.Errorf("§15.2 码名行 %d + `—` 行 %d ≠ 总行数 %d ⇒ 有第三种形状混进来，解析器不能装看不见",
			ct.namedRows, ct.dashRows, ct.totalRows)
	}
	if ct.skipped > 0 {
		t.Errorf("§15.2 里有 %d 行看着是表格行却没被计入任何一类 ⇒ 表形状变了，静默少读一行比读错一行更坏", ct.skipped)
	}
	// ② 码名集合与 §15.1 双向相等。这里与 C8-6/C8-7 有意重叠：
	//    那两条是"谁缺映射"的判据，本条是"解析器读到的确实是这两张表"的自证，重叠部分正是交叉验证。
	var onlyIn152, onlyIn151 []string
	for _, n := range ct.named {
		if !containsStr(ct.codes151, n) {
			onlyIn152 = append(onlyIn152, n)
		}
	}
	for _, c := range ct.codes151 {
		if !containsStr(ct.named, c) {
			onlyIn151 = append(onlyIn151, c)
		}
	}
	if len(onlyIn152) > 0 {
		t.Errorf("§15.2 的『来自』引用了 §15.1 没有的码：%v", onlyIn152)
	}
	if len(onlyIn151) > 0 {
		t.Errorf("§15.1 的码在 §15.2 没有映射行：%v", onlyIn151)
	}
	// ③ 防"锚点命中别处 ⇒ 静默空集"：只判下界，不判字面数。
	if ct.dashRows < 1 {
		t.Errorf("§15.2 里一条『来自』以 `—` 开头的行都没解析到 —— Core transport 自产的码不可能为零，判判据坏了")
	}
	if ct.totalRows < 20 {
		t.Errorf("§15.2 只解析出 %d 行（下界 20）⇒ 区间被截断了，不许拿半张表去判人", ct.totalRows)
	}
	if len(ct.codes151) == 0 {
		t.Errorf("§15.1 一个码都没解析到 ⇒ 判判据坏了")
	}
	// ④ 三个数字照常打印：漂移要看得见，但不咬人。
	t.Logf("形状自证：§15.1=%d 码、§15.2=%d 行（码名行 %d / `—` %d，去重码名 %d），双向相等。数字只跟趋势",
		len(ct.codes151), ct.totalRows, ct.namedRows, ct.dashRows, len(ct.named))
	// §1.1 第 16 条要求的"原始 / 去重"两个数另起一行：左列与 `—` 的重复已在解析时判红，
	// 『来自』那一列的重复是**合法的一对多**（两个 HTTP 码映射到同一个 Adapter 码），所以只报数不判。
	t.Logf("计数对（原始/去重）：§15.2 左列 %d/%d（重复即红）、『来自』%d/%d（重复由 C8-14 判红，本条只报数）",
		ct.transportRaw, len(ct.transport), ct.namedRaw, len(ct.named))
}

// TestAdapterCodeMapsToSingleHTTPName 执行 C8-14：§15.2『来自』列里每个 Adapter 码名**最多出现一次**
// （语义在契约② §15.2.2 —— 09-23 03:1x 主控先把双射写进正文、再立这条判据，顺序不能反）。
//
// 为什么不并进 C8-10：C8-10 判的是"两张表的码名集合互相相等"，而**集合语义看不见重复** ——
// 17 行引用 17 个码名各一次，与"某个码名被引用两次、另一个码名没被引用"在双向相等下可以同时是绿的。
// C8-14 判的是行数与码数之间的关系，正是 §1.1 第 16 条那一族的第三条腿。
//
// `—` 行不参与：**不是一条过滤规则，是结构上进不来** —— switch 里 `—` 那一支先命中，
// 码名那一支（本检查唯一读的地方）永远不会收到它。盘上现有 13 行 `—` 而本条绿，
// 就是这件事在真数据上的样子；所以我**不加**一条"喂两行 `—` 进去不红"的合成用例（那会是个零作为的空判据）。
func TestAdapterCodeMapsToSingleHTTPName(t *testing.T) {
	// 合成反例（§1.1 第 16 条第③件）：绿/红两边都摆出来，否则"检查根本没看这些对"也能交成通过。
	synthetic := []struct {
		name    string
		in      []httpNamePair
		wantMin int // 期望报出的冲突条数下界（0 = 必须干净）
		why     string
	}{
		{"各一次", []httpNamePair{{"timeout", "TIMEOUT"}, {"internal_error", "INTERNAL"}}, 0, "一对一的合法形状"},
		{"同码名两行", []httpNamePair{{"timeout", "TIMEOUT"}, {"adapter_timeout", "TIMEOUT"}}, 1,
			"红话必须点名两行的左列，不然读者还得自己去数是谁撞了"},
		{"同码名三行", []httpNamePair{{"a", "SAME"}, {"b", "SAME"}, {"c", "SAME"}}, 3,
			"两两配对 ⇒ 三行报 3 对，不是只报第一对"},
	}
	for _, c := range synthetic {
		got := dupAdapterNames(c.in)
		switch {
		case c.wantMin == 0 && len(got) > 0:
			t.Errorf("C8-14 判据过严：%s 本该通过却报了 %v", c.name, got)
		case c.wantMin > 0 && len(got) < c.wantMin:
			t.Errorf("C8-14 合成反例失效：%s 本该至少报 %d 条，实得 %d 条（%v）—— %s",
				c.name, c.wantMin, len(got), got, c.why)
		}
	}
	ct, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("§15.1/§15.2 解析坏了，本条无从判断：%v", err)
		return
	}
	// 行身份必须与它自称的行数对得上：不然"点名两行"会点向不存在的行。
	if ct.namedRows != len(ct.namedAt) {
		t.Errorf("namedRows=%d 与留下的行身份数 %d 不等 ⇒ 解析器数了两遍不同的东西，本条结论不可信",
			ct.namedRows, len(ct.namedAt))
	}
	for _, d := range dupAdapterNames(ct.namedAt) {
		t.Errorf("违反契约② §15.2.2 的一行一码：%s —— 同一个 Adapter 码名被两行『来自』引用；"+
			"要么拆成两个码，要么先在 §15.2.2 改掉双射语义，**不许改判据来迁就表**", d)
	}
	t.Logf("C8-14：『来自』带码名 %d 行、去重 %d 个、`—` 行 %d（结构上不参与，非过滤）⇒ 一对一成立",
		ct.namedRows, len(ct.named), ct.dashRows)
}

// dupAdapterNames 把"同一个 Adapter 码名被多行引用"两两配成对报出来，
// 用左列 HTTP 码当行身份（左列自身唯一由 C8-10 保证，所以这个身份不含糊）。
func dupAdapterNames(in []httpNamePair) []string {
	byCode := map[string][]string{}
	var order []string
	for _, p := range in {
		if _, ok := byCode[p.adapter]; !ok {
			order = append(order, p.adapter)
		}
		byCode[p.adapter] = append(byCode[p.adapter], p.httpCode)
	}
	var out []string
	for _, code := range order {
		rows := byCode[code]
		for i := 0; i < len(rows); i++ {
			for j := i + 1; j < len(rows); j++ {
				out = append(out, fmt.Sprintf("%s 同时出现在『来自』的两行（左列 %q 与 %q）", code, rows[i], rows[j]))
			}
		}
	}
	return out
}

// TestHTTPErrorCodesComeFromAdapterSet 执行 C8-6 正向：§15.2 每个带码名的『来自』都必须在 §15.1 里，
// 否则 HTTP 面在转述一个 Adapter 协议没有的码。
func TestHTTPErrorCodesComeFromAdapterSet(t *testing.T) {
	ct, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("解析器坏了，本条无从判断：%v", err)
		return
	}
	for _, n := range ct.named {
		if !containsStr(ct.codes151, n) {
			t.Errorf("§15.2 引用了码 %s，但 §15.1 的码列里没有它（新增码两处必须同改）", n)
		}
	}
	t.Logf("§15.2 的 %d 个码名逐个在 §15.1 里找到了", len(ct.named))
}

// TestAdapterCodesHaveHTTPMapping 执行 C8-7 反向：§15.1 每个码都要出现在 §15.2 的『来自』列。
// 例外不点名放行 —— 判的是形状：『来自』以 `—` 开头的行本来就是 Core transport 自产的，不参与这条。
func TestAdapterCodesHaveHTTPMapping(t *testing.T) {
	ct, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("解析器坏了，本条无从判断：%v", err)
		return
	}
	var missing []string
	for _, c := range ct.codes151 {
		if !containsStr(ct.named, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		t.Errorf("§15.1 有 %d 个码在 §15.2 没有映射行：%v —— 新增码必须同时改两张表", len(missing), missing)
	}
	t.Logf("反向也过：§15.1 的 %d 个码都有 HTTP 映射行；`—` 开头的 %d 行按形状不参与，不靠点名",
		len(ct.codes151), ct.dashRows)
}

func containsStr(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// diffSets 返回"在 a 里但不在 b 里"的元素（两侧都已排序去重）。
func diffSets(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !containsStr(b, s) {
			out = append(out, s)
		}
	}
	return out
}

// equalStrs 逐项比两个有序切片（展开规则自证要的是"顺序与个数都要对"）。
func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- C8-11 ----

// TestTransportCodesAreInContract 执行 C8-11：`core/api/http/**` 里每一个
// `writeError(..., "<code>", ...)` 的字面码必须 ∈ 契约② §15.2 左列。
//
// 这条是三条腿里唯一"当场看得见新码"的那条：`duplicate_project_path` 是 backend 22:4x 落地
// `projects.go` 时才出现的第 13 个 transport 码，§15.2 的收编按定义不可能早于它存在 ——
// C8-6/C8-7/C8-10 都只比两张契约表，它们对"代码已经发了但表里没有"是盲的。
//
// 口径边界（写清楚，免得下次以为它管得更宽）：只判**字面量**码。码若改成常量或变量传进来，
// 这条看不见 —— 所以非字面量的次数照常打印，不判红（超出契约加严是我今天栽过的错）。
func TestTransportCodesAreInContract(t *testing.T) {
	literals, nonLiteral, files := scanTransportCodes(t)
	if files < 3 {
		t.Errorf("core/api/http/** 只扫到 %d 个非测试 .go ⇒ 目录挪了或判据失效，不能判绿", files)
	}
	rawLiterals := len(literals)
	literals = sortedUniq(literals)
	if len(literals) < 5 {
		t.Errorf("只解析出 %d 个字面码：%v ⇒ 提取形状变了，判判据自己坏了", len(literals), literals)
	}
	ct, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("§15.2 解析坏了，本条无从判断：%v", err)
		return
	}
	if len(ct.transport) < 20 {
		t.Errorf("§15.2 左列只解析出 %d 个码（下界 20）⇒ 半张表不能当全集用", len(ct.transport))
	}
	for _, c := range literals {
		if !containsStr(ct.transport, c) {
			t.Errorf("代码会发的 transport 码 %s 不在契约② §15.2 左列 —— 要么补表（契约），要么改代码，两边都得动", c)
		}
	}
	t.Logf("C8-11：扫 %d 个生产 .go、%d 个字面码全部在 §15.2 的 %d 个码里；非字面量传码 %d 处（这条腿看不见，只能靠打印留痕）；"+
		"字面码 原始/去重 = %d/%d（同一处代码路径重复发同一个码是合法的，只报数）",
		files, len(literals), len(ct.transport), nonLiteral, rawLiterals, len(literals))
}

// scanTransportCodes 扫 core/api/http/** 的非测试 .go，取每次 writeError 调用第 3 个实参的字面码。
// 三个不显然的地方：
// ① 先去注释再找 —— 文档注释里出现的 `writeError(..., "xxx", ...)` 不是代码会发的码；
//
//	注释里的字面量当过我的证据一次，不再犯。
//
// ② `func writeError(...)` 定义行的形参表不是调用，不跳过的话每次扫都多一个假实参。
// ③ 第 4 个实参是人话消息，贪婪匹配会把消息里的 ASCII 词当成码名（中文消息掩盖了这个 bug）。
//
//	所以按顶层逗号切参数、按下标取第 3 个，而不是拿一条正则整行去套。
func scanTransportCodes(t *testing.T) (literals []string, nonLiteral int, files int) {
	t.Helper()
	root := repoRoot(t)
	if root == "" {
		return nil, 0, 0
	}
	dir := filepath.Join(root, "core", "api", "http")
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		files++
		src := stripGoComments(string(raw))
		for _, args := range callArgs(src, "writeError") {
			if len(args) < 3 {
				return errAnchor(fmt.Sprintf("%s 里有一处 writeError 只有 %d 个实参 ⇒ 签名变了，本条的取法不可信", filepath.Base(p), len(args)))
			}
			if s, ok := stringLiteral(args[2]); ok {
				literals = append(literals, s)
			} else {
				nonLiteral++
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Errorf("扫 %s 失败: %v", dir, walkErr)
	}
	sort.Strings(literals)
	return literals, nonLiteral, files
}

// stripGoComments 把 // 与 /* */ 注释换成单个空格，字符串/字符/原始串原样保留。
// 不删换行以外的事务：这里只用来做形状匹配，行号一概不报（T-72）。
func stripGoComments(src string) string {
	var out []byte
	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == '`' || c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' && c != '`' {
					j += 2
					continue
				}
				j++
			}
			if j < len(src) {
				j++
			}
			out = append(out, src[i:j]...)
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out = append(out, ' ')
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
			} else {
				i += end + 4
			}
			out = append(out, ' ')
		default:
			out = append(out, c)
			i++
		}
	}
	return string(out)
}

// callArgs 找出 src 里每次 `fn(` 调用的顶层实参（跳过函数定义、跳过括号没闭合的残文）。
func callArgs(src, fn string) [][]string {
	var calls [][]string
	for from := 0; ; {
		k := strings.Index(src[from:], fn+"(")
		if k < 0 {
			break
		}
		open := from + k + len(fn) + 1
		from = open
		if before := strings.TrimRight(src[:open-len(fn)-1], " \t\n\r"); strings.HasSuffix(before, "func") {
			continue
		}
		args, closed := splitTopLevelArgs(src, open)
		if !closed {
			continue
		}
		calls = append(calls, args)
	}
	return calls
}

// splitTopLevelArgs 从开括号之后切实参：只有深度 0 的逗号算分隔，字符串内部的括号/逗号不算。
func splitTopLevelArgs(src string, start int) ([]string, bool) {
	var (
		args  []string
		depth int
		cur   strings.Builder
	)
	for i := start; i < len(src); {
		switch c := src[i]; c {
		case '(', '[', '{':
			depth++
			cur.WriteByte(c)
			i++
		case ')', ']', '}':
			if depth == 0 {
				return append(args, cur.String()), true
			}
			depth--
			cur.WriteByte(c)
			i++
		case ',':
			if depth == 0 {
				args = append(args, cur.String())
				cur.Reset()
			} else {
				cur.WriteByte(c)
			}
			i++
		case '"', '`', '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' && c != '`' {
					j += 2
					continue
				}
				j++
			}
			if j < len(src) {
				j++
			}
			cur.WriteString(src[i:j])
			i = j
		default:
			cur.WriteByte(c)
			i++
		}
	}
	return args, false
}

func stringLiteral(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || !strings.HasPrefix(s, `"`) || !strings.HasSuffix(s, `"`) {
		return "", false
	}
	v, err := strconv.Unquote(s)
	if err != nil {
		return "", false
	}
	return v, true
}

// ---------------------------------------------------------------- C8-12 ----

// TestEventCatalogMatchesJournalConstants 执行 C8-12：事件契约 §6 目录登记的事件类型集合，
// 与 `core/internal/eventjournal` 的常量值集合**双向相等**。
//
// 与 C8-11 是一对：那条判"代码会发的码 ⊆ 契约"，本条判"契约登记的类型 ↔ 代码有的常量"，
// 缺任何一边都得补（目录补了常量没补、或常量有了目录没登记，都是同一个洞的两半）。
// 契约① §6 的那条引文（REQ-DB-5）明确把词表闭合判据派给 `test`，就是这一条。
func TestEventCatalogMatchesJournalConstants(t *testing.T) {
	// 先自证展开规则：契约① §6 的"合并行机械展开约定"（①按 / 切 ②去反引号与空白
	// ③词干 = 该简写之前**最近一个**全名里最后一个 `_` 及其之前 ③b 合并格只许"全是全名"或
	// "一个全名 + 若干简写"两种形态，混排必须报错 ④不带 `_` 前导的原样收 ⑤孤儿 `_xxx` 必须报错）。
	// 两个解析器（`db` 的 eventTypesIn 与本函数）各猜一套就会在将来某天分歧，所以规则要钉在判据里。
	expansion := []struct {
		cell    string
		want    []string
		wantErr bool
		why     string
	}{
		{" `notification_created` / `_handled` / `_read` ", []string{"notification_created", "notification_handled", "notification_read"}, false, "契约原文那一行就是这个形状（③b 形态二）"},
		{" `execution_output_flushed` / `_failed` ", []string{"execution_output_flushed", "execution_output_failed"}, false, "③：词干取名字里最后一个 `_`，不是第一个（否则展开成 execution_failed）"},
		{" `grant_issued` / `grant_revoked` ", []string{"grant_issued", "grant_revoked"}, false, "④/③b 形态一：整行都写全名也合法（守门不许顺手把这种行也拒掉）"},
		{" `task_paused` ", []string{"task_paused"}, false, "④：单名行"},
		{" `a_b` / `c_d` / `_e` ", nil, true, "③b：多个全名 + 简写的混排行 ⇒ 报错，不许在两个词干里挑一个"},
		{" `a_b` / `_c` / `d_e` ", nil, true, "③b 的另一半：简写后面还跟全名"},
		{" ` _orphan ", nil, true, "⑤：本行还没有全名 ⇒ 必须报错，不许静默少收一个类型"},
		{" `NotAnEventName` ", nil, true, "形状不认识（大写）就报错，不猜"},
	}
	for _, e := range expansion {
		got, err := expandEventCell(e.cell)
		switch {
		case e.wantErr && err == nil:
			t.Errorf("展开规则自证：%s（%s）本该报错却返回了 %v", e.cell, e.why, got)
		case !e.wantErr && err != nil:
			t.Errorf("展开规则自证：%s（%s）本该展开成 %v 却报错：%v", e.cell, e.why, e.want, err)
		case !e.wantErr && !equalStrs(got, e.want):
			t.Errorf("展开规则自证：%s（%s）展开成 %v，期望 %v", e.cell, e.why, got, e.want)
		}
	}
	catalog, err := parseEventCatalog(t)
	if err != nil {
		t.Errorf("事件目录解析坏了（本条无从判断）：%v", err)
		return
	}
	constants, files, err := parseJournalEventConstants(t)
	if err != nil {
		t.Errorf("events.go 常量解析坏了（本条无从判断）：%v", err)
		return
	}
	// 第三格的合成反例（§1.1 第 16 条要三格同型）：两个常量同值在 Go 里编得过、集合比也看不出来，
	// 但对词表是一一映射破了 —— 所以必须能喂进纯函数判红，而不是等真数据出错。
	// 这一格**不需要**"注释里也算一个"的用例：`journalConstRE` 行首要求大写标识符 + `=`，
	// `//` 开头的行结构上就进不来（我第一版写了这么一组，是空判据，已删）。
	// 另：现盘 63 个常量值全部互不相同（原始数 == 去重数），下面那组"同值"是**该类的合成反例**，
	// 不是断言盘上存在这一对 —— 把类别读成清单正是今晚 §6 展开约定上踩过的形状。
	constCases := []struct {
		name    string
		src     []string
		wantErr bool
		why     string
	}{
		{"正常两个常量", []string{"package e\n\nconst (\n\tA = \"a_x\"\n\tB = \"b_y\"\n)\n"}, false, "两个名字两个值"},
		{"两个常量同值", []string{"package e\n\nconst (\n\tA = \"a_x\"\n\tB = \"a_x\"\n)\n"}, true, "Go 编得过、sortedUniq 吃得掉，只有这条腿看得见"},
		{"什么都没解析到", []string{"package e\n\nfunc f() {}\n"}, true, "空集比空集是最坏的假绿"},
	}
	for _, c := range constCases {
		got, cerr := journalConstValues(c.src)
		switch {
		case c.wantErr && cerr == nil:
			t.Errorf("第三格的腿失效：%s 本该红却得到 %v —— %s", c.name, got, c.why)
		case !c.wantErr && cerr != nil:
			t.Errorf("第三格过严：%s 本该通过却报错：%v", c.name, cerr)
		}
	}
	// 两条下界都是防"锚点/正则命中别处 ⇒ 静默空集"：空集比空集相等，是最坏的假绿。
	if len(catalog) < 20 {
		t.Errorf("§6 只解析出 %d 个事件类型（下界 20）⇒ 区间被截断了", len(catalog))
	}
	if len(constants) < 20 {
		t.Errorf("eventjournal 只解析出 %d 个常量值（下界 20）⇒ 正则或文件位置变了", len(constants))
	}
	onlyCatalog := diffSets(catalog, constants)
	onlyCode := diffSets(constants, catalog)
	// 巡检 23:20 替 db 转的那条陷阱要**当场看得见**，不能只靠"62=62 恰好相等"推断：
	// `notification_read` 这个字面量在契约① 里只出现 1 次、且那次是说明文字，§6 表格本体写的是
	// 简写 `` `notification_created` / `_handled` / `_read` `` ⇒ 按字面名 grep §6 会把它判成未登记。
	// 这三个名字**只能从合并行展开出来**（不是全名行），所以它们在册就是"展开真的做了"的直接证据。
	for _, derived := range []string{"notification_read", "execution_failed", "process_crashed", "approval_resolved", "handoff_ready"} {
		if !containsStr(catalog, derived) {
			t.Errorf("简写展开少了 %s —— 它只存在于合并行里，缺它就是解析器按字面名读表（db 23:20 报的那条假红）", derived)
		}
	}
	for _, e := range onlyCatalog {
		t.Errorf("契约 §6 登记了 %s，但 eventjournal 没有对应常量 ⇒ 谁发的这条边没有合法类型可用（补常量那边）", e)
	}
	for _, e := range onlyCode {
		t.Errorf("eventjournal 有常量 %s，但契约 §6 没登记 ⇒ 写它即 §9.1 P0（补目录那边）", e)
	}
	t.Logf("C8-12：目录 %d 类 / 常量 %d 个（扫 %d 个非测试 .go）；目录独有 %v、代码独有 %v",
		len(catalog), len(constants), files, onlyCatalog, onlyCode)
}

// parseEventCatalog 解析契约① §6 的首列，并按契约自己的话把合并同族的行摊开成逐条类型
// （"表中合并同族的行仍按逐条类型登记"+"③b 混排非法"，展开规则全在 expandEventCell 的注释里逐条对号）。
func parseEventCatalog(t *testing.T) ([]string, error) {
	t.Helper()
	root := repoRoot(t)
	if root == "" {
		return nil, errAnchor("仓库根没找到")
	}
	path := filepath.Join(root, "specs", "events", "event-contract-v0.1.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errAnchor(fmt.Sprintf("读 %s 失败: %v", path, err))
	}
	sec, err := sliceSection(string(raw), "## 6. ", "## 7. ")
	if err != nil {
		return nil, err
	}
	return expandCatalogRows(sec)
}

// expandCatalogRows 是 **契约① §1.1 第 16 条**那条通用规矩在事件目录上的落点（T-178 的第二条腿）。
//
// 为什么必须有第二条腿：集合语义**天生看不见重复**。09-23 主控把 `project_archived` 登记了两次，
// §6 / yaml / 常量三方都是 63 ⇒ C8-12 与 C8-13 的第一腿全绿，当晚只有 frontend 的"重名即停"真响了。
// 所以这里判两件事：① 展开前的 token 数 == 展开后**去重**的名字数；② 哪个名字、在哪两行重复，点名到行。
// 放在解析器里而不是放在某一个用例里，是因为 C8-12 与 C8-13 共用这一份解析 —— 一条腿补上，两条判据同时闭上。
func expandCatalogRows(lines []string) ([]string, error) {
	var names []string
	tokens := 0
	seen := map[string]string{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 2 {
			return nil, errAnchor(fmt.Sprintf("§6 有一行只有 %d 列：%q", len(cells), line))
		}
		produced, err := expandEventCell(cells[1])
		if err != nil {
			return nil, err
		}
		// expandEventCell 是"一个 token 出一个名字"，所以 tokens 就是展开前的长度；
		// 它不等于去重后的数量，只可能是同一格登记了两次。
		tokens += len(produced)
		row := strings.TrimSpace(cells[1])
		for _, n := range produced {
			if first, dup := seen[n]; dup {
				return nil, errAnchor(fmt.Sprintf("§1.1 第 16 条第二条腿：%s 被登记了两次（首次 %q，又见 %q）"+
					"⇒ 展开前 %d 个 token、展开后只有 %d 个名字，三方集合照样相等、第一腿全绿", n, first, row, tokens, len(seen)))
			}
			seen[n] = row
		}
		names = append(names, produced...)
	}
	if len(names) == 0 {
		return nil, errAnchor("§6 一个事件类型都没解析到 ⇒ 区间或表形状坏了，不拿空集去比")
	}
	if tokens != len(seen) {
		return nil, errAnchor(fmt.Sprintf("展开前 %d 个 token ≠ 去重后 %d 个名字 ⇒ 有重名，逐个点名却在这里没抓到（判自己坏了）",
			tokens, len(seen)))
	}
	return sortedUniq(names), nil
}

// expandEventCell 把目录首列拆成逐个事件类型；形状不认识就报，不猜。
//
// 规则是契约① §6 的"合并行机械展开约定"，逐条对号：
// ① 按 `/` 切；② 去反引号与空白；③ 词干 = 该简写之前**最近一个**全名里最后一个 `_` 及其之前
// （注意：主控 23:49 那版原文写的是"本行第一个全名"，我这份实现从第一天取的就是"最近一个"——
// 那时 §6 没有能区分两种读法的行，所以零分歧；现在 ③b 直接把混排判非法，歧义类被取消，两种读法
// 在任何合法输入上不可能再给出不同结果，所以这条注释跟着翻一次而不是猜一次）；
// ③b 合并格只许两种形态：**全是全名**，或**一个全名打头 + 若干 `_xxx`** ⇒
// 多个全名后面还跟简写、或简写后面还跟全名，都判表格形状坏了、**必须报错**（不许静默挑一个词干）；
// ④ 不带 `_` 前导的 token 原样收下；⑤ 还没有全名就出现 `_xxx` ⇒ 报错。
func expandEventCell(cell string) ([]string, error) {
	var (
		out      []string
		stem     string
		fullSeen int
		sawShort bool
	)
	for _, part := range strings.Split(cell, "/") {
		name := strings.NewReplacer("`", "", " ", "").Replace(part)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "_") {
			switch {
			case fullSeen == 0:
				return nil, errAnchor(fmt.Sprintf("⑤ 目录首列 %q 里简写 %q 前面没有任何全名", cell, name))
			case fullSeen > 1:
				return nil, errAnchor(fmt.Sprintf("③b 混排行非法：简写 %q 跟在 %d 个全名之后（词干该取哪一个？契约要的是报错，不是挑一个）", name, fullSeen))
			case stem == "":
				return nil, errAnchor(fmt.Sprintf("③ 全名 %q 里没有 `_`，无法给简写 %q 取词干", cell, name))
			}
			sawShort = true
			name = stem + name
		} else {
			if sawShort {
				return nil, errAnchor(fmt.Sprintf("③b 混排行非法：全名 %q 出现在简写之后（合并格只许「全是全名」或「一个全名 + 若干简写」）", name))
			}
			fullSeen++
			if i := strings.LastIndex(name, "_"); i > 0 {
				stem = name[:i]
			}
		}
		if !snakeLowerRE.MatchString(name) {
			return nil, errAnchor(fmt.Sprintf("目录首列解析出不像事件名的 %q（原文 %q）", name, cell))
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, errAnchor(fmt.Sprintf("目录首列 %q 一个类型都没解析到", cell))
	}
	return out, nil
}

// journalConstRE 匹配 `Name = "value"` 形式的常量行（tab 缩进、值必须是小写蛇形）。
var journalConstRE = regexp.MustCompile("(?m)^[ \\t]*([A-Z][A-Za-z0-9]*)[ \\t]+=[ \\t]+\"([a-z][a-z0-9_]*)\"[ \\t]*$")

// parseJournalEventConstants 取 eventjournal 包里所有 `X = "x"` 常量的值集合。
// 扫整个目录而不点名 events.go：常量挪到同包另一个文件是本包内部的正常演化，
// 判据认"这个包的常量集合"，认具体文件名只会挪一次就假红。
func parseJournalEventConstants(t *testing.T) ([]string, int, error) {
	t.Helper()
	root := repoRoot(t)
	if root == "" {
		return nil, 0, errAnchor("仓库根没找到")
	}
	dir := filepath.Join(root, "core", "internal", "eventjournal")
	var contents []string
	files := 0
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		files++
		contents = append(contents, string(raw))
		return nil
	})
	if walkErr != nil {
		return nil, files, errAnchor(fmt.Sprintf("扫 %s 失败: %v", dir, walkErr))
	}
	if files == 0 {
		return nil, files, errAnchor(fmt.Sprintf("%s 里一个非测试 .go 都没有 ⇒ 包搬家了，判据不能对着空气判绿", dir))
	}
	vals, err := journalConstValues(contents)
	return vals, files, err
}

// journalConstValues 是第三格的**纯解析**（同 §1.1 第 16 条：合成反例必须喂得进来，
// 只有真数据能红的判据等于没闭上）。
// 两个常量指向同一个事件值在 Go 里合法，但对事件词表是缺陷 —— 同一个类型两个名字，
// 谁都能发、事后对不上是谁；`sortedUniq` 会把它悄悄吃掉，所以按"原始 / 去重"两个数判红。
func journalConstValues(contents []string) ([]string, error) {
	var vals []string
	for _, src := range contents {
		for _, m := range journalConstRE.FindAllStringSubmatch(stripGoComments(src), -1) {
			vals = append(vals, m[2])
		}
	}
	if len(vals) == 0 {
		return nil, errAnchor("一个 X = \"x\" 形状的常量都没解析到 ⇒ 常量写法变了，不拿空集去比")
	}
	if dup, ok := firstDup(vals); ok {
		return nil, errAnchor(fmt.Sprintf("两个常量同值 %q ⇒ 一个事件类型两个名字，词表不再是一一映射", dup))
	}
	return sortedUniq(vals), nil
}

// ---------------------------------------------------------------- C8-13 ----

// TestEventTypeTripleEquality 执行 C8-13：事件类型词表**三格**必须相等 ——
// §6 目录 ↔ `specs/openapi/core-v1.yaml` 的 `EventType` 枚举 ↔ `eventjournal` 常量。
//
// 为什么要有第三条腿（T-172 的起因）：`project_updated` 那次登记只补了 §6 与 Go 常量，
// yaml 那一格没人管，而 frontend 的 `enum ↔ 目录` 守门只在有人跑 `npm run validate` 时才响 ——
// "谁醒了才炸"。判据搬到大家都已经在跑的地方（`go test ./test/...`）才是正解，
// 所以**这里只读 yaml，绝不改它**（那一格归 frontend，codegen 也从它生成）。
//
// ③c（合并只许用于 payload 相同的同族行）**这条腿不机检**，理由写在这里而不是含糊过去：
// 本判据比的是**名字集合**，③c 约束的是**payload 列**；就算某行违反了 ③c，
// 展开出来的名字集合照样是对的 ⇒ 机检它得去解读 payload 那格的自然语言，
// 那是替契约读散文，不是判据。留给人复核（01:3x 那次已经由主控自己拆成三行修掉了）。
func TestEventTypeTripleEquality(t *testing.T) {
	// 第二条腿的合成反例（§1.1 第 16 条第③件："只有真数据能红的不算闭上"）。
	// 这一族我已经有两个先例（C8-11 的 M-A~M-K、C8-12 的展开自证表），照同一形状做。
	dupRows := []struct {
		name    string
		lines   []string
		wantErr bool
		why     string
	}{
		{"合法两行", []string{"| `project_created` | project | `name` | – |", "| `project_updated` | project | `name` | – |"}, false, "两行两个名字"},
		{"合法合并行", []string{"| `notification_created` / `_handled` / `_read` | notification | `x` | – |"}, false, "一行三个 token、三个名字，token 数 == 名字数"},
		{"跨行重名", []string{"| `project_archived` | project | `reason` | – |", "| `project_archived` | project | `reason` | – |"}, true, "01:48 真发生过的那次：三方都是 63，第一腿全绿，只有这条腿看得见"},
		{"格内重名", []string{"| `tasks_paused` / `tasks_paused` | task | `reason` | – |"}, true, "同一格把同一个类型写两遍"},
		{"合并撞全名", []string{"| `a_x` / `_y` | x | `p` | – |", "| `a_y` | x | `p` | – |"}, true, "合并行展开出的 `a_y` 与另一行的全名撞上 —— 只有展开之后才看得见，所以反例必须喂在展开后"},
	}
	for _, d := range dupRows {
		got, err := expandCatalogRows(d.lines)
		switch {
		case d.wantErr && err == nil:
			t.Errorf("第二条腿自证失效：%s 本该红却解析成 %v（%s）", d.name, got, d.why)
		case !d.wantErr && err != nil:
			t.Errorf("第二条腿过严：%s 本该通过却报错：%v", d.name, err)
		}
	}
	catalog, err := parseEventCatalog(t)
	if err != nil {
		t.Errorf("第一格（事件契约 §6）解析坏了：%v", err)
		return
	}
	yamlTypes, err := parseYamlEventTypes(t)
	if err != nil {
		t.Errorf("第二格（openapi 的 EventType 枚举）解析坏了：%v", err)
		return
	}
	constants, files, err := parseJournalEventConstants(t)
	if err != nil {
		t.Errorf("第三格（eventjournal 常量）解析坏了：%v", err)
		return
	}
	// 三格各自的下界：任何一格读空都会让"相等"变成假绿（空集等于空集）。
	if len(catalog) < 20 || len(yamlTypes) < 20 || len(constants) < 20 {
		t.Errorf("某一行解析出的条数低于下界 20（§6=%d / yaml=%d / 常量=%d）⇒ 判判据坏了，不判谁漂移",
			len(catalog), len(yamlTypes), len(constants))
		return
	}
	// 三条边各报一次，红话点名到"哪一格缺、哪一格多"——两两报才说得清漂移发生在哪条通道上。
	edges := []struct {
		name         string
		a, b         []string
		aName, bName string
	}{
		{"§6→yaml", catalog, yamlTypes, "事件契约 §6 目录", "core-v1.yaml 的 EventType 枚举"},
		{"yaml→§6", yamlTypes, catalog, "core-v1.yaml 的 EventType 枚举", "事件契约 §6 目录"},
		{"常量→yaml", constants, yamlTypes, "eventjournal 常量", "core-v1.yaml 的 EventType 枚举"},
		{"yaml→常量", yamlTypes, constants, "core-v1.yaml 的 EventType 枚举", "eventjournal 常量"},
	}
	for _, e := range edges {
		for _, miss := range diffSets(e.a, e.b) {
			t.Errorf("三格不相等 [%s]：%s 里有 %s，%s 里没有 —— 登记事件类型要三处同改（§6 / yaml / 常量）",
				e.name, e.aName, miss, e.bName)
		}
	}
	// 红的当次不能再打印"三格相等 / 四条边全空"那种绿味句子：读证据的人按 Logf 抄进交账，
	// 一次自相矛盾的输出比不打印更坏（探针合成件 a 当场暴露）。
	if t.Failed() {
		t.Logf("C8-13 三格计数：§6=%d / yaml=%d / 常量=%d —— 漂移已在上面点名到具体的边与类型",
			len(catalog), len(yamlTypes), len(constants))
		return
	}
	t.Logf("C8-13 三格相等：§6=%d 类、yaml=%d 条、常量=%d 个（扫 %d 个非测试 .go）；四条边全空",
		len(catalog), len(yamlTypes), len(constants), files)
}

// parseYamlEventTypes 读 `specs/openapi/core-v1.yaml` 里 `EventType` 的 flow 枚举。
//
// 锚点全用**整行相等 / 前缀**，不用行号（§1.1 第 14、15 条）；四道失败出口都是报错，
// 因为这里最坏的失效不是红，是"读到空集 ⇒ 与另一张空集相等 ⇒ 假绿"。
func parseYamlEventTypes(t *testing.T) ([]string, error) {
	t.Helper()
	root := repoRoot(t)
	if root == "" {
		return nil, errAnchor("仓库根没找到")
	}
	path := filepath.Join(root, "specs", "openapi", "core-v1.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errAnchor(fmt.Sprintf("读 %s 失败: %v", path, err))
	}
	lines := strings.Split(string(raw), "\n")
	anchor := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == "    EventType:" {
			if anchor >= 0 {
				return nil, errAnchor("yaml 里出现两个 `    EventType:` 锚点，取哪一个？判据不能猜")
			}
			anchor = i
		}
	}
	if anchor < 0 {
		return nil, errAnchor("yaml 里找不到 `    EventType:` 这一格（改名或挪位置要回写判据表 C8-13）")
	}
	// 从锚点往下找 `enum: [`，再找到收尾的 `]`；跨过下一格的 key（同缩进或非缩进）就算失败。
	open := -1
	for i := anchor + 1; i < len(lines); i++ {
		t0 := strings.TrimSpace(lines[i])
		if i > anchor && t0 != "" && !strings.HasPrefix(t0, "-") && !strings.HasPrefix(t0, "#") &&
			!strings.HasPrefix(lines[i], "        ") && !strings.HasPrefix(lines[i], "      ") {
			return nil, errAnchor(fmt.Sprintf("EventType 那一格里没找到 enum 列表就撞到了下一格（%q）", t0))
		}
		if strings.HasPrefix(t0, "enum:") {
			if !strings.Contains(t0, "[") {
				return nil, errAnchor(fmt.Sprintf("enum 不是 flow 序列形状（%q）—— 换成块式列表要同步改这个解析器", t0))
			}
			open = i
			break
		}
	}
	if open < 0 {
		return nil, errAnchor("EventType 那一格里没有 `enum:` —— 这一格被删了？")
	}
	var out []string
	closed := false
	for i := open; i < len(lines) && i-open < 200; i++ {
		body := lines[i]
		if i == open {
			if parts := strings.SplitN(body, "[", 2); len(parts) == 2 {
				body = parts[1]
			}
		}
		if idx := strings.Index(body, "]"); idx >= 0 {
			body = body[:idx]
			closed = true
		}
		body = strings.TrimSpace(strings.Split(body, "#")[0])
		for _, tok := range strings.Split(body, ",") {
			name := strings.NewReplacer("`", "", "\"", "", " ", "", "\t", "").Replace(tok)
			if name == "" {
				continue
			}
			if !snakeLowerRE.MatchString(name) {
				return nil, errAnchor(fmt.Sprintf("yaml enum 里出现不像事件名的条目 %q（第 %d 行附近）", name, i+1))
			}
			out = append(out, name)
		}
		if closed {
			break
		}
	}
	if !closed {
		return nil, errAnchor("enum 的 `[` 找不到收尾的 `]` ⇒ 区间会吃到文件尾，不可信")
	}
	return sortedUniq(out), nil
}

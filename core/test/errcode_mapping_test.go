package test

// §15.1 的码集合 ↔ §15.2 的『来自』列（契约② §15.2 末段："一致性判据不比行数，按码名集合双向比"，
// 且**例外的判据是形状不是名字**：只有『来自』以 `—` 开头的行不参与，而不是点名放行 version_conflict）。
//
// 为什么单独在 Go 里再来一遍（lint 的 check 8 早就比过这对集合）：
// 主控 09-22 指出 `grep -rn "15\.2" core/test/*.go` 零命中 —— 那条判据只在"有人跑我的 lint"时才活着，
// 而 `go test ./test/...` 是所有人的默认入口。判据住在契约、执行者得在默认入口里。
// 两处口径若漂，以契约② §15.2 为准（本文件与 lint 都只是它的两个执行者）。
//
// 三条都**不用 t.Fatal**：本文件是 T-124 那族错的整改对象 —— 嵌套子用例会整段被父级 `t.Fatal`
// 静默吞掉（"包 ok"但它没跑）。所以每条都是独立顶层用例 + 只用 t.Errorf，
// 解析器坏了也只是三条各自报红，不会互相遮掉。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 解析出的三个数（09-22 15:22 实测盘上就是这个）：
// §15.2 表 29 行 = 17 行『来自』带反引号码名 + 12 行以 `—` 开头；§15.1 码列 17 个。
// 任何一个对不上 ⇒ 判解析器坏了（报数，不下"谁缺码"的结论）。
const (
	wantRows152      = 29
	wantNamedRows152 = 17
	wantDashRows152  = 12
	wantCodes151     = 17
)

var codeNameRE = regexp.MustCompile(`^[A-Z][A-Z_]{3,}$`)

// contractPath 找适配器协议文档：不写死相对层数，向上找到仓库根再拼。
func contractPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Errorf("拿不到工作目录: %v", err)
		return ""
	}
	for {
		if p := filepath.Join(dir, "specs", "adapter-protocol", "adapter-protocol-v0.1.md"); fileExists(p) {
			return p
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Errorf("从 %s 往上找不到 specs/adapter-protocol/adapter-protocol-v0.1.md", dir)
			return ""
		}
		dir = up
	}
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

// parseCodeTables 解析 §15.1 的码列与 §15.2 的『来自』列。
func parseCodeTables(t *testing.T) (codes151 []string, named152 []string, dashRows int, rows152 int, err error) {
	t.Helper()
	path := contractPath(t)
	if path == "" {
		return nil, nil, 0, 0, errAnchor("契约文档路径没找到")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, nil, 0, 0, errAnchor(fmt.Sprintf("读 %s 失败: %v", path, e))
	}
	text := string(raw)
	s151, e := sliceSection(text, "### 15.1 ", "#### 15.1.1")
	if e != nil {
		return nil, nil, 0, 0, e
	}
	s152, e := sliceSection(text, "### 15.2", "### 15.3")
	if e != nil {
		return nil, nil, 0, 0, e
	}
	for _, line := range s151 {
		if c := firstCellCode(line); c != "" {
			codes151 = append(codes151, c)
		}
	}
	for _, line := range s152 {
		cells := strings.Split(line, "|")
		// 表格列的索引：split 出来的 cells[0] 是行首 `|` 之前的空串，所以 awk 的 $4 == cells[3]。
		// （第一版按 awk 的习惯写了 cells[4]，结果第 1 行就撞上"第三种形状"守卫 —— 守卫是对的，索引是我错的。）
		if len(cells) < 4 || !strings.HasPrefix(line, "| `") {
			continue
		}
		rows152++
		from := strings.NewReplacer(" ", "", "`", "").Replace(cells[3])
		switch {
		case strings.HasPrefix(from, "—"):
			dashRows++
		case codeNameRE.MatchString(from):
			named152 = append(named152, from)
		default:
			return nil, nil, 0, 0, errAnchor(fmt.Sprintf("§15.2 第 %d 行的『来自』既不是码名也不是 `—` 开头：%q ⇒ 解析形状变了", rows152, cells[3]))
		}
	}
	sort.Strings(codes151)
	codes151 = uniq(codes151)
	sort.Strings(named152)
	named152 = uniq(named152)
	return codes151, named152, dashRows, rows152, nil
}

func firstCellCode(line string) string {
	if !strings.HasPrefix(line, "| `") {
		return ""
	}
	cells := strings.Split(line, "|")
	if len(cells) < 2 {
		return ""
	}
	c := strings.NewReplacer(" ", "", "`", "").Replace(cells[1])
	if codeNameRE.MatchString(c) {
		return c
	}
	return ""
}

func uniq(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// TestErrorCodeContractTablesParse 只做一件事：证明解析器读到的形状与盘上一致。
// 这一条红的时候，另外两条的结论都不可信 —— 所以它必须自己独立成一个顶层用例（不能被谁吞掉）。
func TestErrorCodeContractTablesParse(t *testing.T) {
	codes, named, dash, rows, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("解析器坏了（下面两条的结论此刻不可信）：%v", err)
		return
	}
	if len(codes) != wantCodes151 {
		t.Errorf("§15.1 码列解析出 %d 个，期望 %d", len(codes), wantCodes151)
	}
	if rows != wantRows152 {
		t.Errorf("§15.2 解析出 %d 行，期望 %d", rows, wantRows152)
	}
	if len(named) != wantNamedRows152 {
		t.Errorf("§15.2 里『来自』带码名的行 %d 条，期望 %d", len(named), wantNamedRows152)
	}
	if dash != wantDashRows152 {
		t.Errorf("§15.2 里『来自』以 `—` 开头的行 %d 条，期望 %d", dash, wantDashRows152)
	}
	if len(named)+dash != rows {
		t.Errorf("两种形状加起来 %d ≠ 总行数 %d ⇒ 有第三种形状混进来了，解析器不能装看不见", len(named)+dash, rows)
	}
	t.Logf("解析器自证：§15.1=%d 码、§15.2=%d 行（码名 %d / `—` %d）", len(codes), rows, len(named), dash)
}

// TestHTTPErrorCodesComeFromAdapterset 正向：§15.2 每个带码名的『来自』都必须在 §15.1 里，
// 否则 HTTP 面在转述一个 Adapter 协议没有的码。
func TestHTTPErrorCodesComeFromAdapterSet(t *testing.T) {
	codes, named, _, _, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("解析器坏了，本条无从判断：%v", err)
		return
	}
	for _, n := range named {
		if !containsStr(codes, n) {
			t.Errorf("§15.2 引用了码 %s，但 §15.1 的码列里没有它（新增码两处必须同改）", n)
		}
	}
	t.Logf("§15.2 的 %d 个码名逐个在 §15.1 里找到了", len(named))
}

// TestAdapterCodesHaveHTTPMapping 反向：§15.1 每个码都要出现在 §15.2 的『来自』列。
// 例外不点名放行 —— 判的是形状：只有『来自』以 `—` 开头的行不参与（那 12 行本来就是 Core transport 自产的）。
func TestAdapterCodesHaveHTTPMapping(t *testing.T) {
	codes, named, _, _, err := parseCodeTables(t)
	if err != nil {
		t.Errorf("解析器坏了，本条无从判断：%v", err)
		return
	}
	var missing []string
	for _, c := range codes {
		if !containsStr(named, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		t.Errorf("§15.1 有 %d 个码在 §15.2 没有映射行：%v —— 新增码必须同时改两张表", len(missing), missing)
	}
	t.Logf("反向也过：§15.1 的 %d 个码都有 HTTP 映射行；带 `—` 的那 12 行按形状不参与，不靠点名", len(codes))
}

func containsStr(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

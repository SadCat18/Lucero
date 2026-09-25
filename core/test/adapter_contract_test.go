package test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// subject 是 Adapter 契约测试的被测面（协议 §1~§7）。故意只暴露这几则断言需要的动词：
// 桩实现走进程内（下面 newInProc），FakeAdapter 就绪后换成真子进程实现同一组动词，
// 用例本身一行都不用改——这就是 §14「FakeAdapter 先于真 Adapter」的可执行形式。
type subject interface {
	name() string
	initialize(peer adapter.InitializeResult) error
	call(method string) error
	delivery(method, mode string) error
	quota() (adapter.QuotaResult, error)
}

// subjects 返回当前可跑的被测实现。adapters/fake 一旦能拉起子进程，在这里追加一条即可。
func subjects() []subject { return []subject{newInProc()} }

// ---- 进程内最小桩 ----------------------------------------------------------

type inProc struct {
	core adapter.Version
	h    *adapter.Handshake
}

func newInProc() *inProc { return &inProc{core: adapter.Version{Major: 1, Minor: 0}} }

func (s *inProc) name() string { return "in-proc stub (internal/adapter)" }

func (s *inProc) initialize(peer adapter.InitializeResult) error {
	h, err := adapter.NewHandshake(s.core, peer)
	if err != nil {
		return err
	}
	s.h = h
	return nil
}

// call 在未握手时把 nil *Handshake 直接透传下去：§2 要求这一律 PROTOCOL_NOT_INITIALIZED，
// 而"忘了握手"与"握手失败"必须是同一个不可用状态。
func (s *inProc) call(method string) error           { return s.h.Call(method) }
func (s *inProc) delivery(method, mode string) error { return s.h.Delivery(method, mode) }

func (s *inProc) quota() (adapter.QuotaResult, error) {
	if err := s.h.Call("quota/read"); err != nil {
		return adapter.QuotaResult{}, err
	}
	return adapter.UnknownQuota(), nil // 桩读不到任何额度：正是 §7 要钉的那一档
}

// ---- 夹具 ------------------------------------------------------------------

func cap(name, level, backend string, write bool, modes ...string) adapter.Capability {
	return adapter.Capability{Name: name, SupportLevel: level, EffectiveBackend: backend,
		WriteCapable: write, DeliveryModes: modes, Confidence: adapter.ConfidenceHigh,
		LastVerifiedAt: 1789800000000}
}

func hello(caps ...adapter.Capability) adapter.InitializeResult {
	return adapter.InitializeResult{
		AdapterKey: "fake", AdapterVersion: "0.1.0",
		Protocol:         adapter.Version{Major: 1, Minor: 0},
		SoftwareVersions: map[string]string{"cli": "0.154.0", "desktop": "26.915.3509.0"},
		AuthMode:         adapter.AuthReuseClient, Health: adapter.HealthHealthy,
		Capabilities: caps, WriteEntry: true,
	}
}

// codeOf 取协议错误码。ErrorCode 现在是 JSON-RPC 的整数线格式（CO-4），
// 打印时靠它的 String() 还原成大写符号名；0 不是任何合法码，表示"根本不是协议错误"（多半是放行）。
func codeOf(err error) adapter.ErrorCode {
	var e *adapter.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// §4 方法表里 Core 会调用的全部方法（debug/* 除外，它们另测）。
var protocolMethods = []string{"adapter/health", "session/list", "session/read", "session/send",
	"session/stop", "session/locate", "model/list", "model/select", "quota/read",
	"approval/list", "approval/respond"}

// writeMethods 是"§3 的 write_entry 门禁必须拦"的方法集合。
// 只列契约文本自身能确定的三条（§4 标 C→A 且改动 Provider 侧状态）。
// approval/respond 与 session/create 的归类是待裁事项，见 TestWriteEntryOpenQuestions。
var writeMethods = []string{"session/send", "session/stop", "model/select"}
var readMethods = []string{"adapter/health", "session/list", "session/read", "model/list",
	"quota/read", "approval/list"}

// ---- 用例 ------------------------------------------------------------------

// TestMajorMismatchRefused §2.2：major 不同直接拒，不猜。
func TestMajorMismatchRefused(t *testing.T) {
	for _, s := range subjects() {
		for _, tc := range []struct {
			desc string
			peer adapter.Version
		}{
			{"peer major 高一位", adapter.Version{Major: 2, Minor: 0}},
			{"peer major 低一位", adapter.Version{Major: 0, Minor: 9}},
		} {
			res := hello(cap("session.send", adapter.LevelNative, adapter.BackendNativeAPI, true, adapter.DeliveryStartNow))
			res.Protocol = tc.peer
			if err := s.initialize(res); err == nil {
				t.Errorf("%s: %s 握手竟然成功", s.name(), tc.desc)
			} else if code := codeOf(err); code != adapter.ErrProtocolVersionMismatch {
				t.Errorf("%s: %s 错误码 %s，期望 PROTOCOL_VERSION_MISMATCH", s.name(), tc.desc, code)
			}
			// 握手被拒之后必须仍处于"未初始化"，不得留下可用的句柄。
			if err := s.call("session/list"); codeOf(err) != adapter.ErrProtocolNotInitialized {
				t.Errorf("%s: 握手被拒后 call 得到 %s，期望 PROTOCOL_NOT_INITIALIZED", s.name(), codeOf(err))
			}
		}
		// minor 不同必须放行，且对端 minor 更高时要标记 EXPERIMENTAL。
		up := hello(cap("session.send", adapter.LevelNative, adapter.BackendNativeAPI, true, adapter.DeliveryStartNow))
		up.Protocol = adapter.Version{Major: 1, Minor: 9}
		if err := s.initialize(up); err != nil {
			t.Errorf("%s: major 相同、minor 更高应放行，实得 %v", s.name(), err)
		}
	}
}

// TestCallsBeforeHandshakeRefused §2：未握手调用任何方法一律 PROTOCOL_NOT_INITIALIZED。
func TestCallsBeforeHandshakeRefused(t *testing.T) {
	for _, s := range subjects() {
		for _, m := range protocolMethods {
			if err := s.call(m); codeOf(err) != adapter.ErrProtocolNotInitialized {
				t.Errorf("%s: 未握手调 %s 得到 %v（码 %s），期望 PROTOCOL_NOT_INITIALIZED", s.name(), m, err, codeOf(err))
			}
		}
		// 三态投递与额度也不能在未握手时被问到。
		if err := s.delivery("session/send", adapter.DeliveryStartNow); codeOf(err) != adapter.ErrProtocolNotInitialized {
			t.Errorf("%s: 未握手问投递模式得到 %s", s.name(), codeOf(err))
		}
		if _, err := s.quota(); codeOf(err) != adapter.ErrProtocolNotInitialized {
			t.Errorf("%s: 未握手读额度得到 %s", s.name(), codeOf(err))
		}
	}
}

// TestDeliveryModesExplicitNotSilent §5：不支持的态必须显式 DELIVERY_MODE_UNSUPPORTED，
// 绝不能"降级成 queue 但报告说按我说的做了"。
func TestDeliveryModesExplicitNotSilent(t *testing.T) {
	all := []string{adapter.DeliveryStartNow, adapter.DeliveryGuide, adapter.DeliveryQueue}
	for _, s := range subjects() {
		for _, declared := range [][]string{
			{adapter.DeliveryStartNow},                                               // 只能立即开轮
			{adapter.DeliveryStartNow, adapter.DeliveryQueue},                        // 没有插话
			{adapter.DeliveryGuide},                                                  // 只能插话
			{adapter.DeliveryStartNow, adapter.DeliveryGuide, adapter.DeliveryQueue}, // 三态全支持
		} {
			if err := s.initialize(hello(cap("session.send", adapter.LevelNative, adapter.BackendNativeAPI, true, declared...))); err != nil {
				t.Fatalf("%s: 握手失败 %v", s.name(), err)
			}
			for _, mode := range all {
				err := s.delivery("session/send", mode)
				want := has(declared, mode)
				if got := err == nil; got != want {
					t.Errorf("%s: 声明 %v 时 delivery=%s ok=%v，期望 ok=%v（err=%v）", s.name(), declared, mode, got, want, err)
				}
				if !want && codeOf(err) != adapter.ErrDeliveryModeUnsupported {
					t.Errorf("%s: 未声明的 delivery=%s 错误码 %s，期望 DELIVERY_MODE_UNSUPPORTED", s.name(), mode, codeOf(err))
				}
			}
			// 未知模式同样不能被当成"默认那档"。
			if codeOf(s.delivery("session/send", "immediately_please")) != adapter.ErrDeliveryModeUnsupported {
				t.Errorf("%s: 臆造的投递模式没有被显式拒绝", s.name())
			}
		}
		// §4：没有声明的能力不得被当成"可以调"。Core 侧的门禁在 Delivery/Effective 这条路上；
		// Adapter 侧真的回 CAPABILITY_UNAVAILABLE（而不是"已停止了事"）要等子进程被测面接上。
		if codeOf(s.delivery("session/stop", adapter.DeliveryStartNow)) != adapter.ErrCapabilityUnavailable {
			t.Errorf("%s: 未声明 session.stop 却没报 CAPABILITY_UNAVAILABLE（码 %s）", s.name(),
				codeOf(s.delivery("session/stop", adapter.DeliveryStartNow)))
		}
	}
}

// TestWriteEntryOpenQuestions 两条待主控裁定的归类问题（test 角色不自裁，只钉现状）。
//
// Q1 approval/respond 算不算 §3 的"写方法"？
//
//	契约② §3 的字面是"任何写方法返回 WRITE_ENTRY_NONE"，但没给写方法清单。
//	internal/adapter/handshake.go 的 writeMethods 刻意排除了 approval/respond，理由写在其注释里：
//	"approvals travel on the permission channel and are gated by Permission Engine, not by write_entry"，
//	并引用"§12.11 (ruled 2026-09-19)"——**该 §12.11 在 specs/ 与 docs/adr/ 里全文检索不到**。
//	要么这条裁定还没回写契约（那它就不构成判据），要么引用有误。
//	现实后果：write_entry:false 的只读 Adapter 目前允许 Core 调 approval/respond。
//
// Q2 session/create 出现在 writeMethods 里，但协议 §4 方法表没有这个方法。
//
// 主控裁定后本用例改为硬断言：批准=写 → 把 approval/respond 加进 writeMethods 列表；
// 批准=权限通道 → 在契约② §3 补一句"审批类方法由 Permission Engine 门禁"，并把 §12.11 落进文件。
// 主控 09-24 10:36 T-273③ 让我自己复量再决定要不要重开 ⇒ 复量结论：**两问都已被裁进契约② §4.1，
// 代码也跟上（`writeMethods` 现含 approval/respond，注释直接引 §4.1）**，所以本用例从 SKIP 改成硬断言。
// 三条腿（§1.1 第 16 条）：① §4.1 的写集合 ↔ 代码 `writeMethods` 双向相等；② 两侧 token 数 == 去重数；
// ③ 合成反例喂进**同一个**比较函数（少一项必须红）。`debug/*` 在代码侧由 `isDebugName` 兑现 ⇒ 单独一判，不静默豁免。
func TestWriteEntryOpenQuestions(t *testing.T) {
	cpath := contractPath(t)
	if cpath == "" {
		t.Fatal("找不到契约② 路径 ⇒ 这条判据看不见东西，不许给干净结论")
	}
	specRaw, err := os.ReadFile(cpath)
	if err != nil {
		t.Fatalf("读契约② 失败：%v", err)
	}
	specSet, err := specWriteMethods(string(specRaw))
	if err != nil {
		t.Fatalf("契约② §4.1 的写方法集合解析失败：%v", err)
	}
	hsRaw, err := os.ReadFile(filepath.Join(repoRoot(t), "core", "internal", "adapter", "handshake.go"))
	if err != nil {
		t.Fatalf("读 internal/adapter/handshake.go 失败：%v", err)
	}
	codeSet, err := codeWriteMethods(string(hsRaw))
	if err != nil {
		t.Fatalf("代码侧 writeMethods 解析失败：%v", err)
	}

	// 腿②：任何一侧内部重名都是漂移（集合比较会把重名静默吸收）。
	if dup, ok := firstDup(specSet); ok {
		t.Errorf("契约 §4.1 的写集合里 %q 出现两次 ⇒ 清单本身漂了", dup)
	}
	if dup, ok := firstDup(codeSet); ok {
		t.Errorf("代码 writeMethods 里 %q 出现两次 ⇒ 同一个方法两条判定", dup)
	}

	// 腿①：双向相等。`debug/*` 是契约侧的写法，代码侧由 isDebugName 兑现 ⇒ 比之前先把它摘出来单独核。
	specNoDebug := diffSets(specSet, []string{"debug/*"})
	if only := diffSets(specNoDebug, codeSet); len(only) > 0 {
		t.Errorf("契约 §4.1 判给写方法、代码却没认：%v", only)
	}
	if only := diffSets(codeSet, specNoDebug); len(only) > 0 {
		t.Errorf("代码当写方法、契约 §4.1 却没列：%v", only)
	}
	if !containsStr(specSet, "debug/*") {
		t.Errorf("契约 §4.1 的写集合里没有 debug/* 那一项 ⇒ 下面那条 isDebugName 的核法失效了")
	}
	if !containsStr(codeSet, "approval/respond") {
		t.Errorf("Q1 裁定是「approval/respond 算写方法」，代码 writeMethods 却没它 ⇒ 只读 Adapter 会被放行审批回应")
	}

	// 腿③：合成反例喂进同一个比较函数（只有真数据能红的不算闭上）。
	// 两个方向各一条：`diffSets(a,b)` 是"a 有 b 没有"，所以期望值要在**两个方向的并集**里找——
	// 这样如果哪天有人只实现了一个方向，另一条用例立刻红（我自己第一版就把这条写反了，被本用例当场抓住）。
	for _, c := range []struct {
		name     string
		spec     []string
		code     []string
		wantOnly string
	}{
		{"代码少一项", []string{"session/send", "approval/respond"}, []string{"session/send"}, "approval/respond"},
		{"契约少一项", []string{"session/send"}, []string{"session/send", "approval/respond"}, "approval/respond"},
		{"两边一致", []string{"session/send"}, []string{"session/send"}, ""},
	} {
		only := append(diffSets(c.spec, c.code), diffSets(c.code, c.spec)...)
		if c.wantOnly == "" && len(only) > 0 {
			t.Errorf("合成反例「%s」本该一致却报了差集：%v", c.name, only)
		}
		if c.wantOnly != "" && !containsStr(only, c.wantOnly) {
			t.Errorf("合成反例「%s」没点名 %q：实际差集 %v", c.name, c.wantOnly, only)
		}
	}

	// Q1/Q2 的"钉现状"部分：契约 §4 方法表里必须真有 session/create 那一行（Q2 的原始症状就是代码有、表里没有）。
	if !strings.Contains(string(specRaw), "| `session/create` |") {
		t.Errorf("§4 方法表里没有 `session/create` 那一行，而它在代码 writeMethods 里 ⇒ Q2 那个「表少一行、代码多一项」的分裂回来了")
	}
	// 生产码不许再引用不存在的条款号（这条正是 Q1 当初被挂住的根因）。
	prodRefs, err := codeClauseRefs(string(hsRaw))
	if err != nil {
		t.Fatalf("解析 handshake.go 的 §引用失败：%v", err)
	}
	for _, ref := range prodRefs {
		if ref == "12.11" {
			t.Errorf("生产码又引用了 §12.11 —— 那是个不存在的条款号（主控已裁：注释只准引 §4.1 或方法名）")
		}
	}
	t.Logf("§4.1 写集合 %d 项 / 代码 %d 项（debug/* 由 isDebugName 兑现，另核）；§引用 %d 处",
		len(specSet), len(codeSet), len(prodRefs))
}

// specWriteMethods 从契约② §4.1 里取「本文口径下的写方法集合」那一段的 `a/b` 形方法名。
// **限定解析节**（读到下一段加粗标题就停）：整份文档里 backtick 的方法名遍地都是，不限定就会把读方法算进来。
func specWriteMethods(text string) ([]string, error) {
	lines := strings.Split(text, "\n")
	start, end := -1, -1
	for i, l := range lines {
		if start < 0 && strings.Contains(l, "口径下的写方法集合") {
			start = i
			continue
		}
		if start >= 0 && strings.Contains(l, "**读方法") {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("没找到 §4.1 那段写方法清单（锚点「口径下的写方法集合」或「**读方法」被改）：start=%d end=%d", start, end)
	}
	var out []string
	for _, l := range lines[start:end] {
		for _, m := range methodTokenRE.FindAllString(l, -1) {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("§4.1 那段里一个方法名都没解析出来 ⇒ 形状变了，判据不能对着空集给绿")
	}
	return out, nil
}

var methodTokenRE = regexp.MustCompile(`[a-z]+/[a-z*]+`)

// codeWriteMethods 取 `var writeMethods = map[string]bool{…}` 里的键。
func codeWriteMethods(src string) ([]string, error) {
	mark := "var writeMethods = map[string]bool{"
	i := strings.Index(src, mark)
	if i < 0 {
		return nil, errors.New("找不到 writeMethods 的 map 定义（改名或换结构了 ⇒ 回写判据）")
	}
	body := src[i+len(mark):]
	if j := strings.Index(body, "\n}"); j >= 0 {
		body = body[:j]
	}
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if k := strings.Index(l, "\""); k >= 0 {
			if e := strings.Index(l[k+1:], "\""); e >= 0 {
				out = append(out, l[k+1:k+1+e])
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("writeMethods 里没解析出任何键 ⇒ 形状变了")
	}
	return out, nil
}

var clauseRefRE = regexp.MustCompile(`§(\d+\.\d+)`)

// codeClauseRefs 抽生产码注释里引的条款号（这条判据只管 Q1 那一格：不许再引不存在的 §12.11）。
func codeClauseRefs(src string) ([]string, error) {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if !strings.Contains(l, "//") {
			continue
		}
		for _, m := range clauseRefRE.FindAllStringSubmatch(l, -1) {
			out = append(out, m[1])
		}
	}
	return out, nil
}

// TestErrorCodeIsJSONRPCNumber §1「JSON-RPC 2.0 语义」+ CO-4：error.code 在线格式上必须是
// 整数，符号名只能活在 Go 侧与 message 里。字符串码会让任何标准 JSON-RPC 客户端解不出来。
// 具体数值不写死（adapter 十几分钟内换过一次映射），只钉"线上是数字、且等于 Go 常量"。
func TestErrorCodeIsJSONRPCNumber(t *testing.T) {
	f := adapter.NewErrorReply("1", "019e0000-0000-7000-8000-000000000001",
		adapter.ErrProtocolNotInitialized, "not handshaken")
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 不再用 "code":" 子串判——adapter 现在把符号名放在 error.data.code 里（合规），
	// 子串会误伤。真正的判据是下面那次解码：code 若是字符串，json.Number 就解不出来。
	var probe struct {
		Error *struct {
			Code json.Number `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Error == nil {
		t.Fatalf("标准客户端解不开这个 error 帧: %v (%s)", err, raw)
	}
	got, err := probe.Error.Code.Int64()
	if err != nil {
		t.Errorf("error.code 不是数字: %s", raw)
	}
	if want := int64(adapter.ErrProtocolNotInitialized); got != want {
		t.Errorf("error.code=%d，与 Go 常量 %s(%d) 不符", got, adapter.ErrProtocolNotInitialized, want)
	}
}

// TestWriteEntryNoneRefusesWrites §3：write_entry:false 时所有写方法必须被拒；
// 只读方法照常；除非该 Adapter 显式声明了对应能力是 UI_AUTOMATION 且 write_capable。
func TestWriteEntryNoneRefusesWrites(t *testing.T) {
	for _, s := range subjects() {
		res := hello(
			cap("session.send", adapter.LevelNative, adapter.BackendNativeAPI, true, adapter.DeliveryStartNow),
			cap("session.stop", adapter.LevelNative, adapter.BackendNativeAPI, true),
			cap("model.select", adapter.LevelNative, adapter.BackendNativeAPI, true),
			cap("approval.respond", adapter.LevelNative, adapter.BackendNativeAPI, true),
			cap("session.list", adapter.LevelNative, adapter.BackendNativeAPI, false),
			cap("quota.read", adapter.LevelNative, adapter.BackendNativeAPI, false),
		)
		res.WriteEntry = false
		if err := s.initialize(res); err != nil {
			t.Fatalf("%s: write_entry:false 是合法声明，握手却失败: %v", s.name(), err)
		}
		for _, m := range writeMethods {
			if err := s.call(m); codeOf(err) != adapter.ErrWriteEntryNone {
				t.Errorf("%s: write_entry=false 时 %s 得到 %v（码 %s），期望 WRITE_ENTRY_NONE", s.name(), m, err, codeOf(err))
			}
			if err := s.delivery(m, adapter.DeliveryStartNow); codeOf(err) != adapter.ErrWriteEntryNone {
				t.Errorf("%s: write_entry=false 时 %s 的投递检查绕过了写入门禁", s.name(), m)
			}
		}
		for _, m := range readMethods {
			if err := s.call(m); err != nil {
				t.Errorf("%s: write_entry=false 不该挡只读方法 %s: %v（码 %s）", s.name(), m, err, codeOf(err))
			}
		}
		// 例外必须是"显式声明"，不是隐式放行。
		uia := hello(cap("session.send", adapter.LevelUIAutomation, adapter.BackendUIAutomation, true, adapter.DeliveryStartNow))
		uia.WriteEntry = false
		if err := s.initialize(uia); err != nil {
			t.Fatalf("%s: UIA 声明握手失败 %v", s.name(), err)
		}
		if err := s.call("session/send"); err != nil {
			t.Errorf("%s: 显式声明 UI_AUTOMATION + write_capable 的写方法仍被拒（§3 允许）: %v", s.name(), err)
		}
	}
}

// TestQuotaUnknownIsNotZero §7：读不到时 observations:[] + overall UNKNOWN，且**绝不为 0**。
// 这里同时查 Go 面和 JSON 面：0 冒充"没有数据"正是能穿过结构体、穿过 UI 的那一类 P0。
func TestQuotaUnknownIsNotZero(t *testing.T) {
	var f float64
	p := func(v float64) *float64 { return &v }
	for _, s := range subjects() {
		if err := s.initialize(hello(cap("quota.read", adapter.LevelNative, adapter.BackendNativeAPI, false))); err != nil {
			t.Fatalf("%s: %v", s.name(), err)
		}
		q, err := s.quota()
		if err != nil {
			t.Fatalf("%s: quota 读取报 %v", s.name(), err)
		}
		if q.Observations == nil {
			t.Errorf("%s: observations 是 nil，§7 要求 []（前端据此区分空列表与缺字段）", s.name())
		}
		if len(q.Observations) != 0 {
			t.Errorf("%s: 桩读不到额度，observations 却有 %d 条", s.name(), len(q.Observations))
		}
		if q.OverallConfidence != adapter.ConfidenceUnknown {
			t.Errorf("%s: overall_confidence=%s，期望 UNKNOWN", s.name(), q.OverallConfidence)
		}
		if err := q.Validate(); err != nil {
			t.Errorf("%s: UNKNOWN 空回复被自己的 Validate 拒了: %v", s.name(), err)
		}
		raw, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("%s: marshal: %v", s.name(), err)
		}
		body := string(raw)
		if !strings.Contains(body, `"observations":[]`) {
			t.Errorf(`%s: 线上 JSON 没有 "observations":[]，实得 %s`, s.name(), body)
		}
		if strings.Contains(body, `"observations":null`) {
			t.Errorf("%s: 线上 JSON 出现 observations:null，实得 %s", s.name(), body)
		}
		if strings.Contains(body, "used_percent") || strings.Contains(body, `"remaining_percent"`) {
			t.Errorf("%s: 空回复里出现了百分比字段，实得 %s", s.name(), body)
		}
	}

	// 表格那半边：把"知道Nothing"包装成 OBSERVED 的三种形态都必须被拒。
	for _, tc := range []struct {
		desc string
		q    adapter.QuotaResult
	}{
		{"值全为 0（Phase 0 实测 Qoder 的 usage 形状）", adapter.QuotaResult{
			Observations: []adapter.QuotaObservation{{QuotaKey: "weekly", QuotaType: "PERCENT",
				ObservationKind: adapter.ObservationObserved, UsedPercent: &f, RemainingPercent: &f,
				Confidence: adapter.ConfidenceHigh, Source: "message.usage"}},
			OverallConfidence: adapter.ConfidenceHigh}},
		{"只有字段名没有值", adapter.QuotaResult{
			Observations: []adapter.QuotaObservation{{QuotaKey: "weekly", QuotaType: "PERCENT",
				ObservationKind: adapter.ObservationObserved, Confidence: adapter.ConfidenceMedium,
				Source: "x"}},
			OverallConfidence: adapter.ConfidenceMedium}},
		{"reset_at 用 0 表示未知", adapter.QuotaResult{
			Observations: []adapter.QuotaObservation{{QuotaKey: "weekly", QuotaType: "PERCENT",
				ObservationKind: adapter.ObservationObserved, UsedPercent: p(62.5), RemainingPercent: p(37.5),
				ResetAt: &zeroish, Confidence: adapter.ConfidenceHigh, Source: "x"}},
			OverallConfidence: adapter.ConfidenceHigh}},
		{"空列表却报 HIGH", adapter.QuotaResult{Observations: []adapter.QuotaObservation{},
			OverallConfidence: adapter.ConfidenceHigh}},
		{"正常观测", adapter.QuotaResult{
			Observations: []adapter.QuotaObservation{{QuotaKey: "weekly", QuotaType: "PERCENT",
				ObservationKind: adapter.ObservationObserved, UsedPercent: p(62.5), RemainingPercent: p(37.5),
				Confidence: adapter.ConfidenceHigh, Source: "app_server/GetAccountRateLimits"}},
			OverallConfidence: adapter.ConfidenceHigh}},
	} {
		err := tc.q.Validate()
		wantOK := tc.desc == "正常观测"
		if gotOK := err == nil; gotOK != wantOK {
			t.Errorf("%s: Validate()=%v，期望 ok=%v", tc.desc, err, wantOK)
		}
	}
}

var zeroish int64

// TestDebugGating §9：debug.* 只准 adapter_key=fake 且 support_level=EXPERIMENTAL 声明。
func TestDebugGating(t *testing.T) {
	for _, s := range subjects() {
		for _, tc := range []struct {
			key, level string
			ok         bool
		}{
			{"fake", adapter.LevelExperimental, true},
			{"fake", adapter.LevelNative, false},
			{"codex", adapter.LevelExperimental, false},
			{"codex", adapter.LevelNative, false},
		} {
			res := hello(cap("debug/crash", tc.level, adapter.BackendNativeAPI, true))
			res.AdapterKey = tc.key
			err := s.initialize(res)
			if gotOK := err == nil; gotOK != tc.ok {
				t.Errorf("%s: %s 声明 debug/crash=%s 的握手结果 ok=%v，期望 ok=%v（err=%v）",
					s.name(), tc.key, tc.level, gotOK, tc.ok, err)
			}
		}
	}
}

// ---- 第二个被测面：真子进程 FakeAdapter（协议 §14 的验收对象） ----------------
//
// 与上面的表驱动用例分开是故意的：进程内桩能被喂任意 capability 夹具，真 Adapter 只会
// 报它自己声明的那套，所以子进程只跑"线上协议层"能判的断言（§2 门禁、§2.2 版本、
// §7 额度、§5 投递模式），不去假装它能被夹具摆布。
//
// 默认只在能启动且握手可用时才跑；否则把**实测到的失败原文**写进 Skip 理由，
// 绝不用桩的绿灯冒充真进程验过。开启：LUCERO_TEST_FAKE_ADAPTER=1（需 go 可用）。

type fakeProc struct {
	t          *testing.T
	cmd        *exec.Cmd
	in         io.WriteCloser
	out        *bufio.Reader
	nextID     int
	caps       []adapter.Capability
	writeEntry bool
	// frames 由一个常驻读取器喂：对端在一次调用前后会发通知帧（§6 的 stream/*、§4.2 的取证两帧），
	// 谁读、读了几帧必须是确定的，否则"按 id 配对响应"就没法做（见 request）。
	frames  chan []byte
	skipped int // 配对响应时路过的通知帧条数：超时的报错里要能看见它，否则分不清"沉默"与"在发通知"
}

// buildFakeAdapterExe 把 FakeAdapter build 到临时目录。抽成函数是因为有两个被测面要用它：
// 我这条 ndjson 直连的 harness，以及 turn_attribution_test.go 里经 process.Supervisor 起的那条。
func buildFakeAdapterExe(t *testing.T) (string, error) {
	t.Helper()
	goBin := os.Getenv("LUCERO_GO")
	if goBin == "" {
		goBin = "go"
	}
	exe := filepath.Join(t.TempDir(), "fake-adapter.exe")
	build := exec.Command(goBin, "build", "-o", exe, "./cmd/fake-adapter")
	build.Dir = moduleRoot()
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build 失败: %v\n%s", err, out)
	}
	return exe, nil
}

// fakeAdapterEnabled 是"要不要为子进程面 build + 起真进程"的开关。默认关：
// 每次 `go test ./test/...` 都付一次 build 的钱不值，但**交付前必须开着跑一遍**（判据表里写明）。
func fakeAdapterEnabled() (bool, string) {
	return os.Getenv("LUCERO_TEST_FAKE_ADAPTER") == "1", "设 LUCERO_TEST_FAKE_ADAPTER=1 才会 build+启动子进程"
}

func startFakeAdapter(t *testing.T) (*fakeProc, error) {
	if ok, why := fakeAdapterEnabled(); !ok {
		return nil, fmt.Errorf("未开启（%s）", why)
	}
	exe, err := buildFakeAdapterExe(t)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard // 诊断走 stderr 是 §1 的要求，这里不需要收
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = in.Close() })
	p := &fakeProc{t: t, cmd: cmd, in: in, out: bufio.NewReader(outPipe), frames: make(chan []byte, 64)}
	// 常驻读取器：通知帧与响应帧混在一条流里，必须由一个人按顺序收，再让各条判据按 id 取自己的那一帧。
	// 每次判据现开一个 goroutine 读会在超时后把迟到那一帧丢掉 —— 那正是"没读到"被当成"没有"的形状。
	go func() {
		defer close(p.frames)
		for {
			line, err := p.out.ReadBytes('\n')
			if len(line) > 0 {
				p.frames <- line
			}
			if err != nil {
				return
			}
		}
	}()
	return p, nil
}

func moduleRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			return dir
		}
		dir = up
	}
}

// nextFrameID 分配一帧的 id：JSON-RPC id 与 request_id 同源，别让人对不上号。
func (f *fakeProc) nextFrameID() (string, string) {
	f.nextID++
	return fmt.Sprintf("t%d", f.nextID),
		fmt.Sprintf("019e0000-0000-7000-8000-%012d", f.nextID)
}

// request 发一帧并读回**属于它自己的**那一帧响应；超时算失败（Adapter 不回话 = Core 会挂到 deadline）。
// 必须按 id 配对：真 Adapter 在一次调用前后会发通知帧（§6 的 stream/*、§4.2 的取证两帧），
// 而通知帧没有 error 字段 —— 把"下一帧"当响应会把通知读成"它成功了"，那是假绿不是实测。
// 收到的通知帧不丢，攒在 notes 里给取证那一腿用。
func (f *fakeProc) request(method string, params any) (*adapter.Frame, error) {
	id, reqID := f.nextFrameID()
	if err := f.write(map[string]any{
		"jsonrpc": "2.0", "id": id, "request_id": reqID,
		"method": method, "params": params,
	}); err != nil {
		return nil, err
	}
	for {
		fr, err := f.readFrame(method)
		if err != nil {
			return nil, err
		}
		if fr.ID == id {
			return fr, nil
		}
		f.skipped++ // 通知帧（或别的调用的回话）：跳过，但条数留在超时信息里
	}
}

// readFrame 取一帧并解析（原始字段的判据请直接用 recv）。
func (f *fakeProc) readFrame(what string) (*adapter.Frame, error) {
	raw, err := f.recv(what)
	if err != nil {
		return nil, err
	}
	fr := &adapter.Frame{}
	if err := json.Unmarshal(raw, fr); err != nil {
		return nil, fmt.Errorf("帧解不开: %w (%s)", err, raw)
	}
	return fr, nil
}

// callAsync 发一帧请求但不读回执，返回它的 JSON-RPC id。
// 这是"有一 call 在飞"的唯一形状，C8-5 的 $/cancel 必须先有它才有对象可取消。
func (f *fakeProc) callAsync(method string, params any) (string, error) {
	id, reqID := f.nextFrameID()
	if err := f.write(map[string]any{
		"jsonrpc": "2.0", "id": id, "request_id": reqID,
		"method": method, "params": params,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// write 发一帧原始 JSON，不等待回话。
func (f *fakeProc) write(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := f.in.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("写入失败（进程可能已退出）: %w", err)
	}
	if fl, ok := f.in.(interface{ Flush() error }); ok {
		_ = fl.Flush() // 有缓冲时不 flush 会让对端一直等
	}
	return nil
}

// recv 从常驻读取器取一帧**原始字节**（不先过 Go 结构体）——C8-5 要判的是线上有没有 error 这个键，
// 而 Frame.Error 带 omitempty，从结构体反推不出"线上有没有"。
func (f *fakeProc) recv(what string) ([]byte, error) {
	select {
	case raw, ok := <-f.frames:
		if !ok {
			return nil, fmt.Errorf("对端已经没有话可读了（%s）", what)
		}
		return raw, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("%s 5s 内没有可读的帧（Adapter 沉默 = Core 会挂到 deadline）", what)
	}
}

func (f *fakeProc) name() string { return "FakeAdapter 子进程 (core/cmd/fake-adapter)" }

func (f *fakeProc) initialize(peer adapter.InitializeResult) error {
	res, err := f.request("adapter/initialize", map[string]any{
		"protocol": peer.Protocol, "core_version": "0.1.0",
	})
	if err != nil {
		return err
	}
	if res.Error != nil {
		return fmt.Errorf("握手被拒: %s", res.Error)
	}
	var got adapter.InitializeResult
	if err := json.Unmarshal(res.Result, &got); err != nil {
		return fmt.Errorf("握手 result 解不开: %w", err)
	}
	f.caps, f.writeEntry = got.Capabilities, got.WriteEntry
	return nil
}

func (f *fakeProc) call(method string) error {
	res, err := f.request(method, map[string]any{})
	if err != nil {
		return err
	}
	if res.Error == nil {
		return nil
	}
	return &adapter.Error{Code: res.Error.Code, Message: res.Error.Message}
}

func (f *fakeProc) delivery(method, mode string) error {
	res, err := f.request(method, map[string]any{
		// 参数必须一次给全，否则对端在 §4 的参数校验里就先拒了，
		// 我拿到的错误码与"这一态支不支持"毫无关系 —— 判据要打在待验那一层上。
		"remote_session_id": "fake-session-1", "text": "contract probe",
		"delivery": mode, "idempotency_key": "ik-" + mode,
	})
	if err != nil {
		return err
	}
	if res.Error == nil {
		return nil
	}
	return &adapter.Error{Code: res.Error.Code, Message: res.Error.Message}
}

func (f *fakeProc) quota() (adapter.QuotaResult, error) {
	res, err := f.request("quota/read", map[string]any{})
	if err != nil {
		return adapter.QuotaResult{}, err
	}
	if res.Error != nil {
		return adapter.QuotaResult{}, fmt.Errorf("quota/read 被拒: %s", res.Error)
	}
	var q adapter.QuotaResult
	return q, json.Unmarshal(res.Result, &q)
}

// TestFakeAdapterSubprocess 真子进程的线上协议断言。握手都不通时，后面的断言一条都跑不到，
// 所以这里先判握手，失败就把实测原文放进 Skip 理由（可见，不伪装成通过）。
func TestFakeAdapterSubprocess(t *testing.T) {
	f, err := startFakeAdapter(t)
	if err != nil {
		t.Skipf("blocked: %v", err)
	}
	// §2：未握手时任何方法都必须被拒，且错误码是 PROTOCOL_NOT_INITIALIZED。
	if e := f.call("session/list"); codeOf(e) != adapter.ErrProtocolNotInitialized {
		t.Errorf("未握手调 session/list 得到 %v（码 %s），期望 PROTOCOL_NOT_INITIALIZED", e, codeOf(e))
	}
	// §2：握手必须能成功。
	if e := f.initialize(hello()); e != nil {
		t.Skipf("FakeAdapter 握手不通，后续 §3/§5/§7 线上断言跑不到；实测原文：%v", e)
	}
	// §2.1：software_versions 必须是多值映射，不是字符串。
	if len(f.caps) == 0 {
		t.Error("握手成功却没声明任何 capability（§3 要求逐项显式声明）")
	}
	// §7：额度读不到时必须是 [] + UNKNOWN，不得为 0。
	q, e := f.quota()
	if e != nil {
		t.Errorf("quota/read: %v", e)
	} else if err := q.Validate(); err != nil {
		t.Errorf("真 Adapter 的 quota 回复不满足 §7: %v", err)
	}
	// §5：只能按它自己声明的模式投；没声明的模式必须显式 DELIVERY_MODE_UNSUPPORTED。
	modes := map[string]bool{}
	for _, c := range f.caps {
		if c.Name == "session.send" {
			for _, m := range c.DeliveryModes {
				modes[m] = true
			}
		}
	}
	if len(modes) > 0 && !modes["guide"] {
		if e := f.delivery("session/send", "guide"); codeOf(e) != adapter.ErrDeliveryModeUnsupported {
			t.Errorf("未声明 guide 却得到 %v（码 %s），§5 禁止静默降级", e, codeOf(e))
		}
	}
}

// TestCancelReplyIsResultFrame 判据表 C8-5（契约② §15.3 规则 5）：取消的回执走**结果帧**，
// `CANCELLED` 不得作为 JSON-RPC error 出现在线上。
// 断言打在 FakeAdapter 真子进程的原始帧字节上（T-74④：不要导出的 process.Connection，
// 同进程构造连接对象会把"只有真进程测得出隔离"这件事又测没）。error 这个键在不在只看线上，
// 不看 Go 结构体——Frame.Error 带 omitempty，从结构体反推不出原文有没有这个键（T-29）。
func TestCancelReplyIsResultFrame(t *testing.T) {
	f, err := startFakeAdapter(t)
	if err != nil {
		t.Skipf("blocked: %v", err)
	}
	if e := f.initialize(hello()); e != nil {
		t.Skipf("FakeAdapter 握手不通，C8-5 的帧级断言跑不到；实测原文：%v", e)
	}
	// §10 的 deadline 场景：debug/hang 停止回话，只读 $/cancel。
	hangID, err := f.callAsync("debug/hang", map[string]any{})
	if err != nil {
		t.Fatalf("发出 debug/hang: %v", err)
	}
	cancel, err := adapter.NewCancel("t-cancel", hangID)
	if err != nil {
		t.Fatalf("构造 $/cancel: %v", err)
	}
	if cancel.ID != "" {
		t.Fatalf("$/cancel 必须是通知（带 id 就是在等回执，§1 不给它回执）：%+v", cancel)
	}
	if err := f.write(cancel); err != nil {
		t.Fatalf("发出 $/cancel: %v", err)
	}
	// 被取消的那一 call 收到回执；$/cancel 本身必须没有回执——有回执就会先读到它。
	raw, err := f.recv("$/cancel 之后 debug/hang 的回执")
	if err != nil {
		t.Fatalf("取消后拿不到回执: %v", err)
	}
	// 判定抽成纯函数：同一段代码既判真子进程回来的帧，也判下面的反例。
	// 只有正例没有反例，这条判据就是空的（和 T-74③ 给 W13-3 钉反向用例是同一个道理）。
	for _, msg := range cancelAckFaults(raw, hangID) {
		t.Errorf("%s（原文：%s）", msg, raw)
	}
	// 反例 1：把取消回成 error 帧的实现，必须被同一段判据抓住。
	red := []byte(`{"jsonrpc":"2.0","id":"` + hangID +
		`","error":{"code":-32010,"message":"cancelled","data":{"code":"CANCELLED"}}}`)
	redFaults := cancelAckFaults(red, hangID)
	if len(redFaults) == 0 {
		t.Error("C8-5 的判据是空的：error 形态的取消回执没被判红")
	}
	// 反例 2：合规形状必须判得出绿，否则这条判据只是"永远响"。
	green := []byte(`{"jsonrpc":"2.0","id":"` + hangID + `","result":{"cancelled":true}}`)
	greenFaults := cancelAckFaults(green, hangID)
	if len(greenFaults) != 0 {
		t.Errorf("C8-5 判据过严：合规的结果帧回执被判成违反：%v", greenFaults)
	}
	// 两条反例的实测值打出来，报告里就能看见判据两边都活着（不是"期望 0/非 0"的空话）。
	t.Logf("反向用例：error 形态 %d 条违反 %v；合规形态 %d 条违反", len(redFaults), redFaults, len(greenFaults))
}

// cancelAckFaults 判 $/cancel 的回执帧（契约② §15.3 规则 5、判据表 C8-5）：必须是回显
// 被取消那一 call 的 id 的**结果帧**、result.cancelled=true，且线上不得出现 error 键或
// "CANCELLED" 码名。只看原始字节，不看 adapter.Frame —— Frame.Error 带 omitempty，
// 从结构体反推不出"线上有没有这个键"。
func cancelAckFaults(raw []byte, targetID string) []string {
	var faults []string
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return []string{fmt.Sprintf("回执帧不是合法 JSON: %v", err)}
	}
	if _, ok := keys["error"]; ok {
		faults = append(faults, "C8-5 违反：取消回执带 error 键 —— CANCELLED 上了 JSON-RPC 的错误面")
	}
	res, ok := keys["result"]
	if !ok {
		faults = append(faults, "C8-5 违反：取消回执没有 result 键（§15.3 规则 5 要求它是结果帧）")
	} else {
		var body struct {
			Cancelled *bool `json:"cancelled"`
		}
		if err := json.Unmarshal(res, &body); err != nil {
			faults = append(faults, fmt.Sprintf("result 解不开: %v", err))
		} else if body.Cancelled == nil || !*body.Cancelled {
			faults = append(faults, fmt.Sprintf("result.cancelled=%v，期望 true（取消不许沉默）", body.Cancelled))
		}
	}
	if got := string(keys["id"]); got != `"`+targetID+`"` {
		faults = append(faults, fmt.Sprintf("回执 id 回显 %s，期望 %q（回执要给被取消的那一 call）", got, targetID))
	}
	if strings.Contains(string(raw), `"CANCELLED"`) {
		faults = append(faults, `线上原文出现 "CANCELLED" 码名（它只是聚合状态，不属于错误面）`)
	}
	return faults
}

// TestFakeAdapterDeliveryModesOnTheWire 件一（T-98）：BUG-T6 已闭（`session/send` 实现了、
// handshake.go 的 delivery 门也补了），所以三态投递这条判据**必须有真子进程面**——
// 进程内桩是"没人发的词汇"，它绿了不算数。
// 一行一态逐个判，**不许整案 Skip**：某一行今天没有对家，那一行单独记 待建 并点名缺什么。
func TestFakeAdapterDeliveryModesOnTheWire(t *testing.T) {
	f, err := startFakeAdapter(t)
	if err != nil {
		t.Skipf("blocked: %v（这一条要真子进程，旗标 LUCERO_TEST_FAKE_ADAPTER=1）", err)
	}
	if e := f.initialize(hello()); e != nil {
		t.Skipf("FakeAdapter 握手不通，投递三态在线上判不了；实测原文：%v", e)
	}
	declared := map[string]bool{}
	for _, c := range f.caps {
		if c.Name == "session.send" {
			for _, m := range c.DeliveryModes {
				declared[m] = true
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("握手成功却没声明任何 delivery 模式 —— §3 要求逐项显式声明，这本身就是缺陷")
	}
	// §5 的三态 + 一个编出来的态：编出来那个是"未声明"的对照，钉的是"没声明必须显式拒"。
	for _, mode := range []string{"start_now", "guide", "queue", "steering"} {
		t.Run(mode, func(t *testing.T) {
			err := f.delivery("session/send", mode)
			code := codeOf(err)
			if declared[mode] {
				// 声明支持却被按"不支持"拒 = 声明与实现两张嘴；别的错误码（会话不存在等）不算这条。
				if code == adapter.ErrDeliveryModeUnsupported {
					t.Errorf("%s 在握手声明里，线上却被拒 DELIVERY_MODE_UNSUPPORTED —— §5 禁止声明与裁决不一致: %v", mode, err)
				}
				t.Logf("声明含 %s ⇒ 未被按模式拒绝（code=%s）", mode, code)
				return
			}
			if code != adapter.ErrDeliveryModeUnsupported {
				t.Errorf("%s 未声明却得到 code=%s（err=%v），期望显式 DELIVERY_MODE_UNSUPPORTED —— §5 严禁静默降级成 queue", mode, code, err)
			} else {
				t.Logf("未声明 %s ⇒ 显式拒绝，没有静默降级", mode)
			}
		})
	}
	// write_entry:false 那一面（TestWriteEntryNoneRefusesWrites 的子进程对应行）：
	// FakeAdapter 今天恒声明 write_entry=true，所以这一行只能 待建 —— 单独 Skip 这一行，
	// 不把上面三态的实测一起吞掉。等 §10 给 fake 一个只读声明的开关（CO-23）即改硬。
	t.Run("write_entry_false 面", func(t *testing.T) {
		if !f.writeEntry {
			t.Fatal("握手声明了 write_entry=false，那这条就不该再是 待建 —— 去把写方法逐个打成 WRITE_ENTRY_NONE 并断言")
		}
		t.Skip("待建：FakeAdapter 恒声明 write_entry=true，线上没有只读那一面（§10 未给开关，CO-23）")
	})
}

// INTERNAL 且不崩、不装懂。断言全打在 Degrade()/Symbol() 自己的返回值上（T-29）。
// TestFakeAdapterModelSelectLegs T-103（契约② §4 的 CO-36 定形）：`model/select` 的三条拒绝腿
// 加一条正例，全打在 FakeAdapter **真子进程**的线上回话上。
//
// 判据见 **契约② §4 判据表 MS**（MS-1~MS-6；15:2x 由本文件注释镜像进本文，本体以契约为准，
// 这里不再抄一份免得两处真相）。状态列是主控 15:18:51→15:19:38 逐条实跑标定的，不是抄我自述。
//
// 两点只属于"怎么把它跑起来"、不是判据，所以留在代码侧：
// ① 旗标只管 MS-1~MS-5（这五个要走真子进程，默认 SKIP：一次 build 的钱不该让每次 `./test/...` 都付）。
//
//	**MS-6（`TestModelSelectShapeMatchesContract`）不碰旗标、常跑** —— 它判的是 Core 自己的类型形状，
//	没有子进程可起。别把"这条要旗标"写成整张表的性质：我 15:5x 那版就是这么误标过一次，
//	后果是下一个人把 MS-6 的绿也算成"没跑过的那一半"。
//	`-run` 是前缀匹配，**不带 `-v` 看不见那行 SKIP、包照样报 ok** —— "计数命令假绿"的现场版。
//
// ② MS-4 的两个取值都**当场从对端问来**（`session/create` + `model/list`），不写死 fixture：
//
//	写死等于把实现方的表抄进判据，对端换 fixture 时我会红一条根本不存在的错。
func TestFakeAdapterModelSelectLegs(t *testing.T) {
	f, err := startFakeAdapter(t)
	if err != nil {
		t.Skipf("blocked: %v", err)
	}
	if e := f.initialize(hello()); e != nil {
		t.Skipf("FakeAdapter 握手不通，model/select 判据跑不到；实测原文：%v", e)
	}
	// 会话与模型都问对端要（MS-4 的正例原料，也是 MS-3 的"合法 offer"边）
	created, err := f.request("session/create", map[string]any{"client_request_id": "crid-ms-1"})
	if err != nil || created.Error != nil {
		t.Fatalf("session/create 拿不到会话：err=%v frame=%v", err, created)
	}
	var cs struct {
		RemoteSessionID string `json:"remote_session_id"`
		Created         bool   `json:"created"`
	}
	if err := json.Unmarshal(created.Result, &cs); err != nil || cs.RemoteSessionID == "" {
		t.Fatalf("session/create 的回执里没有 remote_session_id：%s（%v）", created.Result, err)
	}
	ml, err := f.request("model/list", map[string]any{})
	if err != nil || ml.Error != nil {
		t.Fatalf("model/list: err=%v frame=%v", err, ml)
	}
	var ls struct {
		Models []struct {
			ModelKey string `json:"model_key"`
		} `json:"models"`
	}
	if err := json.Unmarshal(ml.Result, &ls); err != nil || len(ls.Models) == 0 {
		t.Fatalf("model/list 给了 0 个模型（%s）⇒ 正例无原料", ml.Result)
	}
	offered := ls.Models[0].ModelKey

	cases := []struct {
		name   string
		params map[string]any
		want   adapter.ErrorCode // 0 = 期望这一发被接受
	}{
		{name: "MS-1a 缺 model_key", params: map[string]any{"remote_session_id": cs.RemoteSessionID}, want: adapter.ErrInvalidParams},
		{name: "MS-1b 缺 remote_session_id", params: map[string]any{"model_key": offered}, want: adapter.ErrInvalidParams},
		{name: "MS-2 未知会话", params: map[string]any{"remote_session_id": "no-such-session", "model_key": offered}, want: adapter.ErrorCode(-32006)},
		{name: "MS-3 模型不在 offer 里", params: map[string]any{"remote_session_id": cs.RemoteSessionID, "model_key": "model-nobody-offers"}, want: adapter.ErrCapabilityUnavailable},
		{name: "MS-4 正例", params: map[string]any{"remote_session_id": cs.RemoteSessionID, "model_key": offered}, want: 0},
	}
	seen := map[string]adapter.ErrorCode{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := f.request("model/select", tc.params)
			if err != nil {
				t.Fatalf("model/select 没有回话: %v", err)
			}
			if tc.want == 0 {
				if res.Error != nil {
					t.Errorf("正例被拒：%s ⇒ %v（对端自己 offer 的模型 + 自己刚建的会话）", tc.params, res.Error)
					return
				}
				// 契约 §4 CO-36 定形："回执同形状"。多带一个布尔就是第二真相。
				var keys map[string]json.RawMessage
				if err := json.Unmarshal(res.Result, &keys); err != nil {
					t.Fatalf("回执解不开：%s", res.Result)
				}
				if len(keys) != 2 || keys["remote_session_id"] == nil || keys["model_key"] == nil {
					t.Errorf("回执形状 = %s，期望恰是 {remote_session_id, model_key}（§4 定形：回执同形状）", res.Result)
				}
				var echo struct {
					RemoteSessionID string `json:"remote_session_id"`
					ModelKey        string `json:"model_key"`
				}
				_ = json.Unmarshal(res.Result, &echo)
				if echo.RemoteSessionID != cs.RemoteSessionID || echo.ModelKey != offered {
					t.Errorf("回执回显 (%q,%q)，期望 (%q,%q)", echo.RemoteSessionID, echo.ModelKey, cs.RemoteSessionID, offered)
				}
				t.Logf("MS-4 绿边：会话 %q + 模型 %q ⇒ %s", cs.RemoteSessionID, offered, res.Result)
				return
			}
			if res.Error == nil {
				t.Errorf("该拒的没拒：%v ⇒ 回执 %s", tc.params, res.Result)
				return
			}
			if res.Error.Code != tc.want {
				t.Errorf("%v 得到 code=%d，期望 %d（%v）", tc.params, int(res.Error.Code), int(tc.want), res.Error)
			}
			seen[tc.name] = res.Error.Code
			t.Logf("%s ⇒ code=%d %v", tc.name, int(res.Error.Code), res.Error.Message)
		})
	}
	// MS-5：三条拒绝腿不许塌成同一个码 —— 那是三种不同的用户结论。
	if a, b := seen["MS-2 未知会话"], seen["MS-3 模型不在 offer 里"]; a != 0 && b != 0 && a == b {
		t.Errorf("MS-5：未知会话与未知模型都是 %d，§4.1 要求两处门不混", int(a))
	}
	if a, b := seen["MS-1a 缺 model_key"], seen["MS-3 模型不在 offer 里"]; a != 0 && b != 0 && a == b {
		t.Errorf("MS-5：缺参数与不支持同一个码 %d ⇒ Core 会一直改参数而这一轮永远跑不起来", int(a))
	}
}

// MS-6 —— 契约形状 ↔ Core 侧类型互校（§4 的 CO-36 定形说"请求 {remote_session_id, model_key}、
// 回执同形状"；上面 MS-4 已经从**线上**钉住了对端的形状，这一条钉的是 Core 自己的类型有没有跟着长歪）。
// 两条腿：字段名集合要恰是那一对；json tag 不许带 omitempty —— 带了就允许"省略"这第三种"没有"，
// 正是 §4 幂等/同步游标那两条裁定要收掉的形状。
// 注意：这两个类型 14:01 才出现在 `internal/adapter/send.go`（T-102② 那半件，此刻尚未提交）。
// 所以它红的时候先按 T-68 查 mtime 分清"在制品"与"回归"，别急着报给别人。
func TestModelSelectShapeMatchesContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		prob any
	}{
		{name: "请求 ModelSelectParams", prob: adapter.ModelSelectParams{}},
		{name: "回执 ModelSelectResult", prob: adapter.ModelSelectResult{}},
	} {
		typ := reflect.TypeOf(tc.prob)
		got := map[string]string{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			if strings.Contains(f.Tag.Get("json"), "omitempty") {
				t.Errorf("%s 的 %s 带 omitempty ⇒ 键可省略 = 第三种\"没有\"（§4 明令收掉的形状）", tc.name, f.Name)
			}
			got[tag] = f.Name
		}
		if len(got) != 2 || got["remote_session_id"] == "" || got["model_key"] == "" {
			t.Errorf("%s 的线上字段 = %v，期望恰是 {remote_session_id, model_key}（§4 CO-36 定形）", tc.name, got)
			continue
		}
		t.Logf("MS-6 %s ⇒ %v", tc.name, got)
	}
}

// TestUnknownErrorCodeDegradesToInternal T-45⑤：Core 收到 §15.1 之外的码时必须降级为
// INTERNAL 且不崩、不装懂。断言全打在 Degrade()/Symbol() 自己的返回值上（T-29）。
func TestUnknownErrorCodeDegradesToInternal(t *testing.T) {
	unk := &adapter.Error{Code: adapter.ErrorCode(-32999), Message: "provider said something new",
		Data: map[string]any{"code": "SOMETHING_NEW", "provider_raw": map[string]any{"x": 1}}}
	got := unk.Degrade()
	if got == nil {
		t.Fatal("Degrade 返回 nil：Core 不能因为看不懂就把错误丢掉")
	}
	if got.Code != adapter.ErrInternal {
		t.Errorf("未知码 %d 降级成 %s，期望 INTERNAL", int(unk.Code), got.Code)
	}
	// 这一条是本用例存在的理由：把未知码原样念回去 = Core 假装自己懂，
	// 也是 Degrade 注释里点名要防的那件事。
	if s := got.Symbol(); s != "INTERNAL" {
		t.Errorf("降级后 Symbol()=%q，期望 INTERNAL（未知码不得借 data.code 复活）", s)
	}
	d, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data 结构变了：%T", got.Data)
	}
	if _, still := d["code"]; still {
		t.Errorf("data.code 没摘掉，Symbol() 会重新念出未知码：%v", d)
	}
	if v, _ := d["original_code"].(int); v != -32999 {
		t.Errorf("original_code 没保留对端真正说的码（Audit 要能查到原始事实）：%v", d["original_code"])
	}
	if _, kept := d["provider_raw"]; !kept {
		t.Errorf("对端原始信息被丢了：%v", d)
	}
	for _, known := range []*adapter.Error{
		{Code: adapter.ErrMethodNotFound, Message: "no such method"},
		{Code: adapter.ErrWriteEntryNone, Message: "read-only adapter"},
		{Code: adapter.ErrInvalidParams, Message: "bad params"},
	} {
		if out := known.Degrade(); out != known {
			t.Errorf("已登记的 %s 被 Degrade 改动了（协议层码不该被降级）", known.Code)
		}
	}
	if out := (*adapter.Error)(nil).Degrade(); out != nil {
		t.Error("nil Error 的 Degrade 造出了对象")
	}
}

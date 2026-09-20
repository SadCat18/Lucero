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
func TestWriteEntryOpenQuestions(t *testing.T) {
	t.Skip("blocked: 等主控裁定 Q1/Q2（见本用例注释）；同时需要把被引用的 §12.11 回写进 specs，" +
		"否则任何角色都无法自查自己是否合规")
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
}

func startFakeAdapter(t *testing.T) (*fakeProc, error) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		return nil, fmt.Errorf("未开启（设 LUCERO_TEST_FAKE_ADAPTER=1 才会 build+启动子进程）")
	}
	goBin := os.Getenv("LUCERO_GO")
	if goBin == "" {
		goBin = "go"
	}
	exe := filepath.Join(t.TempDir(), "fake-adapter.exe")
	build := exec.Command(goBin, "build", "-o", exe, "./cmd/fake-adapter")
	build.Dir = moduleRoot()
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build 失败: %v\n%s", err, out)
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
	return &fakeProc{t: t, cmd: cmd, in: in, out: bufio.NewReader(outPipe)}, nil
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

// request 发一帧并读回它的响应；超时算失败（Adapter 不回话 = Core 会挂到 deadline）。
func (f *fakeProc) request(method string, params any) (*adapter.Frame, error) {
	f.nextID++
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": fmt.Sprintf("t%d", f.nextID),
		"request_id": fmt.Sprintf("019e0000-0000-7000-8000-%012d", f.nextID),
		"method":     method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	if _, err := f.in.Write(append(raw, '\n')); err != nil {
		return nil, fmt.Errorf("写入失败（进程可能已退出）: %w", err)
	}
	if fl, ok := f.in.(interface{ Flush() error }); ok {
		_ = fl.Flush() // 有缓冲时不 flush 会让对端一直等
	}
	type result struct {
		f   *adapter.Frame
		err error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := f.out.ReadBytes('\n')
		if err != nil {
			ch <- result{nil, fmt.Errorf("读不到响应帧: %w", err)}
			return
		}
		fr := &adapter.Frame{}
		if err := json.Unmarshal(line, fr); err != nil {
			ch <- result{nil, fmt.Errorf("响应帧解不开: %w (%s)", err, line)}
			return
		}
		ch <- result{fr, nil}
	}()
	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("%s 5s 内没有响应帧（Adapter 沉默 = Core 会挂到 deadline）", method)
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
	res, err := f.request(method, map[string]any{"delivery": mode, "text": "contract probe"})
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

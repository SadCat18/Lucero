package main

// T-181 的①与②的落点证据。分工写清楚，免得两条腿互相顶包：
//   - `api/http/adapters_test.go` 判 transport（形状、501/405 分流、缺幂等键、§15.2 映射、一个字都不补）；
//   - 这个文件判**装配**：没配 Adapter 时那条口是不是真的还 501（不是 panic、也不是 400），
//     `-adapter` 参数怎么解析，以及失败那一路库里落了什么。
//   - 唯一一条要 build + 起真子进程的是 `TestProbeOverRealHandshakeWithFakeAdapter`：
//     它跟 `core/test/adapter_contract_test.go` 共用同一个开关 `LUCERO_TEST_FAKE_ADAPTER=1`
//     （一套开关两个地方认，别各造一个），交付前必须开着跑一遍 —— 跑没跑要能在计数里看出来。

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// probePOST 打真监听上的那条口；返回状态与解出来的错误码（或整个响应体）。
//
// **必须配 `bootProbeCore` 用**：只 `boot()` 不 `serve()` 的话 `core.ln` 已经建好、却没人 `Accept`，
// 于是 TCP 连得上、请求发得进、响应永远不来 —— 实测这条把整包挂到 go test 自己的 600s 超时。
// 这一格写在这里是因为它不会以"报错"的形式露出来，只会以"没有输出"的形式露出来。
func probePOST(t *testing.T, core *Core, key, idem string) (int, map[string]any) {
	t.Helper()
	url := "http://" + core.endpoint + "/api/v1/adapters/" + key + "/probe"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+core.token)
	req.Header.Set("X-Lucero-Principal", "local-user")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	// 带期限的客户端：默认那个会永远等 —— "没人 Accept"这种故障的表现是没有输出，
	// 而不是一个能读的错；给它一个上限，故障就会以失败的形式露出来（而不是烧掉整包超时）。
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求没跑完（连得上但没人应答，还是 boot 了没 serve？）: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("响应不是 JSON（%d）: %v", resp.StatusCode, err)
	}
	return resp.StatusCode, out
}

func errorCodeOf(t *testing.T, body map[string]any) string {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("错误体形状不符契约③ §2: %v", body)
	}
	code, _ := e["code"].(string)
	if code == "" {
		t.Fatalf("错误体缺 code: %v", body)
	}
	return code
}

// bootProbeCore 是"boot 完还要真的 serve"那一句的机械化：只 boot 的话监听 socket 已建、没人 Accept，
// 客户端连得上但永远等不到响应（实测挂满 go test 自己的 600s 超时）。
func bootProbeCore(t *testing.T, cfg config) *Core {
	t.Helper()
	core, err := boot(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() { _ = core.serve(ctx) }()
	return core
}

func TestAdapterFlagCollectsAndRejects(t *testing.T) {
	var f adapterFlag
	if err := f.Set("fake=..\\.scratch\\fake-adapter.exe"); err != nil {
		t.Fatalf("合法参数该收： %v", err)
	}
	if err := f.Set(" claude = D:\\some\\path.exe "); err != nil {
		t.Fatalf("两端空格该容忍： %v", err)
	}
	if got := f.String(); got != `fake=..\.scratch\fake-adapter.exe,claude=D:\some\path.exe` {
		t.Errorf("回读形状不对: %q", got)
	}
	for _, bad := range []string{"novalue", "=only-path", "only-key=", "", "fake=dup"} {
		var dup adapterFlag
		if err := dup.Set("first=x"); err != nil {
			t.Fatal(err)
		}
		if err := dup.Set(bad); bad == "fake=dup" {
			// 不重复的 key 一定收得下（上面那个是 first），所以这一格只验"没炸"。
			if err != nil {
				t.Errorf("%q 是合法的 key=路径，不该拒: %v", bad, err)
			}
			continue
		}
		if err := dup.Set(bad); err == nil {
			t.Errorf("%q 该被拒（key 与路径都得有），实际收下了", bad)
		}
	}
	// 重复 key 在参数层就拒，不等 Registry 报"两个条目"（那时读的人先要懂 Registry 才知道是自己写重了）。
	var dup adapterFlag
	if err := dup.Set("fake=a"); err != nil {
		t.Fatal(err)
	}
	if err := dup.Set("fake=b"); err == nil {
		t.Error("同一个 key 登记两次该被拒")
	}
}

// TestProbeStaysUnavailableWithoutAdapters 钉住 `adaptersField()` 存在的那件事：
// 没配 `-adapter` ⇒ `core.probe` 是 nil ⇒ 那条口必须回 501。
// 直接把 typed nil 塞进接口会得到"非 nil 接口 + nil 指针"，症状是 **panic 而不是 501** ——
// 所以这一格判的是真 HTTP 答案，不判字段。
func TestProbeStaysUnavailableWithoutAdapters(t *testing.T) {
	core := bootProbeCore(t, testConfig(t))

	code, body := probePOST(t, core, "fake", "k-1")
	if code != http.StatusNotImplemented || errorCodeOf(t, body) != "not_implemented" {
		t.Errorf("一个 Adapter 都没配时 probe 该 501 not_implemented，实际 %d %v", code, body)
	}
}

// TestProbeFailureRecordsHealthOnly 是 §7.2 那句"失败 ⇒ 只记 health_state 变化并回 §15.2 对应码，
// 不伪造 capability 行"的落库证据：起不来（路径不存在）⇒ 一行 instance、**零行 capability**、
// 一个 `adapter_registered` 事件（首次见到这个 key），HTTP 侧 503 `adapter_unresponsive`。
func TestProbeFailureRecordsHealthOnly(t *testing.T) {
	cfg := testConfig(t)
	cfg.Adapters = adapterFlag{specs: []adapterSpec{{
		key: "broken", displayName: "broken", command: filepath.Join(cfg.DataDir, "没有这个文件.exe"),
	}}}
	core := bootProbeCore(t, cfg)

	code, body := probePOST(t, core, "broken", "k-1")
	if code != http.StatusServiceUnavailable || errorCodeOf(t, body) != "adapter_unresponsive" {
		t.Fatalf("起不来的 probe 该 503 adapter_unresponsive，实际 %d %v", code, body)
	}
	var (
		id, health, metadata string
		enabled              int
		caps                 int
		events               string
	)
	if err := core.db.Read.QueryRow(
		`SELECT id, health, enabled, metadata_json FROM adapter_instances WHERE adapter_key = ?`, "broken",
	).Scan(&id, &health, &enabled, &metadata); err != nil {
		t.Fatalf("失败也要留下一行 instance（否则\"探过\"与\"没探过\"事后分不开）: %v", err)
	}
	if health != "UNAVAILABLE" {
		t.Errorf("health 该是 UNAVAILABLE，实际 %q", health)
	}
	if !strings.Contains(metadata, "launch") {
		t.Errorf("失败原因要留在 metadata_json 里（拿空 reason 冒充知道是另一回事），实际 %q", metadata)
	}
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM adapter_capabilities WHERE adapter_instance_id = ?`, id).Scan(&caps); err != nil {
		t.Fatal(err)
	}
	if caps != 0 {
		t.Errorf("一次都没握上手，就不该有任何 capability 行，实际 %d 行", caps)
	}
	if err := core.db.Read.QueryRow(
		`SELECT group_concat(event_type) FROM event_journal WHERE aggregate_id = ?`, id,
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != "adapter_registered" {
		t.Errorf("首次失败该只留一条 adapter_registered（没有\"从没变到没变\"的健康变化），实际 %q", events)
	}
}

// TestUnknownAdapterKeyIsBadRequestNotOutage 钉 §7.2 明写的那个分岔：
// "没人登记过的 key" 是**请求写错了**（400），不是"那个 Adapter 病了"（503）。
// 分错的话，面板会在一个根本不存在的名字上一直重试。
func TestUnknownAdapterKeyIsBadRequestNotOutage(t *testing.T) {
	cfg := testConfig(t)
	cfg.Adapters = adapterFlag{specs: []adapterSpec{{
		key: "broken", displayName: "broken", command: filepath.Join(cfg.DataDir, "没有这个文件.exe"),
	}}}
	core := bootProbeCore(t, cfg)

	code, body := probePOST(t, core, "never-registered", "k-1")
	if code != http.StatusBadRequest || errorCodeOf(t, body) != "validation_failed" {
		t.Fatalf("未知 key 该 400 validation_failed，实际 %d %v", code, body)
	}
	// 而且**一行都不许留**：未知 key 连"探过"都不算，写一行 UNAVAILABLE 就是给一个不存在的 Adapter 建档。
	var n int
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM adapter_instances WHERE adapter_key = ?`, "never-registered").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("未知 key 不该建 instance 行，实际 %d 行", n)
	}
}

// TestProbeResponseIdentityOnHealthChange 钉一件**别人家的**缺口，钉法是把现状写死：
// `adapter.SQLiteStore.RecordProbe` 只在"建行"与"health 没变"两条分支里给 `written` 赋了值，
// 中间那条"health 真的变了"没赋 ⇒ 返回值是空串 ⇒ `ProbeResult.AdapterInstanceID` 空 ⇒
// 我这侧 200 响应里的 `id` 也是空串（yaml 的 required 有 id，所以前端拿到的是一个"存在但没名字"的实例）。
// 我这里**不补**：§7.2 写死"handler 里不许补字段"，装配侧同理 —— 该改的是 probe。
//
// **这条用例变红的时候，说明 adapter 把那一格修好了**：那时把断言改成"id 非空且两次相同"，
// 并在账上把 C38 关掉。别把它当回归删掉。
func TestProbeResponseIdentityOnHealthChange(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UnixMilli()
	store, err := adapter.NewSQLiteStore(d, func() int64 { return at })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := store.RecordProbe(ctx, adapter.ProbeResult{
		AdapterKey: "fake", DisplayName: "fake", HealthState: "HEALTHY", VerifiedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatalf("建行那一格该回 id（前提变了，见注释）: %q", first)
	}
	second, err := store.RecordProbe(ctx, adapter.ProbeResult{
		AdapterKey: "fake", DisplayName: "fake", HealthState: "DEGRADED", VerifiedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second != "" {
		t.Fatalf(`health 变了那条分支现在也会给 written 赋值了 —— C38 已被修，`+
			`把本用例改成"两次 id 相同且非空"并记账（实际第二次回 %q）`, second)
	}
	// 空串不是"什么都没写"：库里那一行确实被 Touch 过，只是**返回的 id 丢了**。
	var health string
	var version int64
	if err := d.Read.QueryRow(`SELECT health, version FROM adapter_instances WHERE adapter_key = ?`, "fake").
		Scan(&health, &version); err != nil {
		t.Fatal(err)
	}
	if health != "DEGRADED" || version != 2 {
		t.Errorf("第二探该把行推进到 DEGRADED v2（事实没丢，丢的是返回的 id），实际 %q v%d", health, version)
	}
	// 而"health 没变"的第三探又是有 id 的 —— 这条差异本身就是判据没被写成"永远空"的证据。
	third, err := store.RecordProbe(ctx, adapter.ProbeResult{
		AdapterKey: "fake", DisplayName: "fake", HealthState: "DEGRADED", VerifiedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if third != first {
		t.Errorf("第三探（health 未变）该回到同一个实例 id %q，实际 %q", first, third)
	}
}

// TestProbeOverRealHandshakeWithFakeAdapter 是这一批最硬的一条：真子进程、真 ndjson 握手、真落库。
// 它同时也是 T84-7b 触发条件②（"有真连接"）与 frontend 那条 skipped 格子的前提。
//
// 开关与 `core/test` 同一套：`LUCERO_TEST_FAKE_ADAPTER=1`；Go 的路径用 `LUCERO_GO`（没给就用 PATH 上的 go）。
// 开着跑的时候**必须数得出它跑过**（-v 里的 PASS 行），不带 -v 的 ok 不算证据。
func TestProbeOverRealHandshakeWithFakeAdapter(t *testing.T) {
	if os.Getenv("LUCERO_TEST_FAKE_ADAPTER") != "1" {
		t.Skip("未开 LUCERO_TEST_FAKE_ADAPTER=1：不 build、不起真子进程（交付前必须开着跑一遍）")
	}
	exe := buildFakeAdapterHere(t)

	cfg := testConfig(t)
	cfg.Adapters = adapterFlag{specs: []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: exe}}}
	core := bootProbeCore(t, cfg)

	code, body := probePOST(t, core, "fake", "k-real-1")
	if code != http.StatusOK {
		t.Fatalf("真握手该 200，实际 %d %v", code, body)
	}
	for k, want := range map[string]any{
		"adapter_key": "fake", "display_name": "Fake Adapter", "enabled": true,
	} {
		if body[k] != want {
			t.Errorf("%s 该是 %v，实际 %#v（响应体：%v）", k, want, body[k], body)
		}
	}
	if id, _ := body["id"].(string); id == "" {
		t.Errorf("首探是\"建了一行\"那一路，id 不该为空: %v", body)
	}
	if h, _ := body["health"].(string); h == "" || h == "UNKNOWN" {
		t.Errorf("握手成功却没报出 health（UNKNOWN 在这里等于没探），实际 %q", body["health"])
	}
	if v, _ := body["last_healthcheck_at"].(float64); v == 0 {
		t.Errorf("verified_at 必须落到 last_healthcheck_at，实际 %#v", body["last_healthcheck_at"])
	}
	// 必填五格齐了才算投影完整（少一格，前端的 generated client 就拒这个响应）。
	for _, k := range []string{"id", "adapter_key", "display_name", "health", "enabled"} {
		if _, ok := body[k]; !ok {
			t.Errorf("响应缺 yaml 的必填格 %s: %v", k, body)
		}
	}

	var caps int
	if err := core.db.Read.QueryRow(
		`SELECT count(*) FROM adapter_capabilities WHERE adapter_instance_id = (SELECT id FROM adapter_instances WHERE adapter_key = ?)`, "fake",
	).Scan(&caps); err != nil {
		t.Fatal(err)
	}
	if caps == 0 {
		t.Error("握手成功却没落任何能力行 ⇒ \"registered 但 offer 为空\"，正是 §3 说的那种假状态")
	}
	// ②的证据：桥上的 recorder 现在真会在生命周期那一格出声（stdout 日志里）。
	// 这里判的是"探过一次之后库里那一行活着"，出声本身由 processpipe_test.go 那条钉（两边别互相顶）。
	var version int64
	if err := core.db.Read.QueryRow(`SELECT version FROM adapter_instances WHERE adapter_key = ?`, "fake").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Errorf("首探只该推进一次版本（ADR-017），实际 v%d", version)
	}
}

// buildFakeAdapterHere 与 core/test 那份同一形状（同一套开关、同样显式 CGO_ENABLED=0）。
// 没有复用它是因为它在别人的包里、没导出 —— 要复用得先跨角色改文件，不值一次 build 的钱。
func buildFakeAdapterHere(t *testing.T) string {
	t.Helper()
	goBin := os.Getenv("LUCERO_GO")
	if goBin == "" {
		goBin = "go"
	}
	exe := filepath.Join(t.TempDir(), "fake-adapter.exe")
	build := exec.Command(goBin, "build", "-o", exe, "./cmd/fake-adapter")
	build.Dir = moduleRootForProbeTest()
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake-adapter 失败: %v\n%s", err, out)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("build 说成功了但产物不在盘上: %v", err)
	}
	return exe
}

func moduleRootForProbeTest() string {
	// 测试的工作目录就是包目录（cmd/core），模块根在上一层。
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Dir(filepath.Dir(wd))
}

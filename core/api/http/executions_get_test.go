package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// sortedKeys 返回响应体的顶层键集（排序后的），用来钉"单条读与列表同形"。
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestGetExecutionReadsRowAndThreeSnapshots 是第七十九批那一格的正面证据：
// yaml `getExecution`（core-v1.yaml:496，GET /api/v1/executions/{id}）从 501 变成 200 之后，
// 出来的必须是**库里那一行**，且三份快照是库里的真值（不是 transport 手写的 {}）。
func TestGetExecutionReadsRowAndThreeSnapshots(t *testing.T) {
	srv, _ := newExecutionHarness(t, true)

	code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-1", "local-user", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET {id} 该 200（读面已装配），实际 %d %s", code, body)
	}
	one := mustParseMap(t, body)
	if one["id"] != "ex-1" || one["task_id"] != "task-1" || one["status"] != "SUCCEEDED" {
		t.Errorf("读到的不是夹具里那行（ex-1 / task-1 / SUCCEEDED），实际 id=%v task=%v status=%v",
			one["id"], one["task_id"], one["status"])
	}
	// yaml `Execution.required` 七键：少一个就是给面板一个不合法的响应体。
	for _, k := range []string{"id", "task_id", "status", "attempt_no", "trigger_type", "state_confidence", "version"} {
		if _, ok := one[k]; !ok {
			t.Errorf("缺必填键 %s：%v", k, one)
		}
	}
	// 三份快照：键必在、且是**空对象** —— 夹具没写过这三列 ⇒ 迁移默认值 `'{}'` 才是库里的真值；
	// 出 null 或 "" 就是 transport 在折算，出缺键就是读面没读。
	for _, k := range []string{"model_snapshot", "permission_snapshot", "environment_snapshot"} {
		v, ok := one[k]
		if !ok {
			t.Errorf("缺快照键 %s：%v", k, one)
			continue
		}
		obj, ok := v.(map[string]any)
		if !ok || len(obj) != 0 {
			t.Errorf("%s 该是空对象 {}（库里的默认值），实际 %#v", k, v)
		}
	}
	// 扩了快照不等于松了红线：**schema `Execution`（core-v1.yaml:1148）里没有的键一个都不许冒出来** ——
	// 尤其 `model_config_json`（编排侧那一格，`domain.go` 顶注明写别顺手加）与两列带 `_json` 的内部列。
	// 上一批这条红线写的是「域里没有的九个键」，第八十批把那九个键按 yaml 补进域之后，
	// 红线的边界就从「域里有什么」挪到「schema 声明了什么」（值的真伪在 TestGetExecutionWideFieldsComeFromTheRow）。
	for _, k := range []string{"model_config", "model_config_json", "error_details_json", "metadata_json"} {
		if _, ok := one[k]; ok {
			t.Errorf("schema 没声明 %s，不许发明：%v", k, one)
		}
	}

	// 单条读与列表读必须同形：面板只该写一套读法（两条口的键集逐字相等）。
	_, listBody := call(t, srv, http.MethodGet, "/api/v1/executions?task_id=task-1", "local-user", "", "")
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(listBody), &page); err != nil || len(page.Items) == 0 {
		t.Fatalf("列表读面拿不到行：%v %s", err, listBody)
	}
	var same map[string]any
	for _, it := range page.Items {
		if it["id"] == "ex-1" {
			same = it
		}
	}
	if same == nil {
		t.Fatalf("列表里没有 ex-1：%s", listBody)
	}
	if a, b := sortedKeys(one), sortedKeys(same); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("单条与列表不同形：单条=%v 列表=%v", a, b)
	}
}

// TestGetExecutionGates 钉三岔的次序与归属：方法不对 405（**先于**接线判定 —— 方法是请求自己的事实）、
// 没这条行 404、读面没注入 501 且**点名缺的是读通道**（不是 catch-all 那句「Core 没登记」，也不是 404）。
func TestGetExecutionGates(t *testing.T) {
	srv, _ := newExecutionHarness(t, true)

	// 404：库里没这条行。`Repository.Get` 把 sql.ErrNoRows 包了一层 ⇒ 404 判据就是"包了也能认出来"。
	if code, body := call(t, srv, http.MethodGet, "/api/v1/executions/nope", "local-user", "", ""); code != http.StatusNotFound ||
		codeOf(t, body) != "not_found" || !strings.Contains(body, "没有这条 Execution") {
		t.Errorf("未知 id 该 404 not_found 且说清没有这条 Execution，实际 %d %s", code, body)
	}

	// wired=false ⇒ Executions 没注入；Store 在场 ⇒ 前缀路由**仍登记**（第七十九批改的那道闸）。
	bare, _ := newExecutionHarness(t, false)
	// 方法不对：405，且要压过 501（否则"接没接线"会把请求自身的问题盖掉）。
	if code, body := call(t, bare, http.MethodPost, "/api/v1/executions/ex-1", "local-user", "k-g", `{}`); code != http.StatusMethodNotAllowed ||
		codeOf(t, body) != "method_not_allowed" {
		t.Errorf("POST {id} 该 405（先于接线判定），实际 %d %s", code, body)
	}
	// 读面没装配：501，文案要点名缺的是读通道，不许说成"没登记"（那是对 §3 登记过的路径说假话）。
	code, body := call(t, bare, http.MethodGet, "/api/v1/executions/ex-1", "local-user", "", "")
	if code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Fatalf("读面没注入该 501 not_implemented，实际 %d %s", code, body)
	}
	if !strings.Contains(body, "还没装配读通道") {
		t.Errorf("501 文案要说明缺的是读通道，实际 %s", body)
	}
	if strings.Contains(body, "Core 没登记") {
		t.Errorf("这条路径 §3 登记过，说「Core 没登记」是假话（T-370② 同族），实际 %s", body)
	}
	// 同一个 Config 下写面也缺 ⇒ send 分支自己答 501，不冒充 404、也不落到 catch-all。
	if code, body := call(t, bare, http.MethodPost, "/api/v1/executions/ex-1/send", "local-user", "k-s",
		`{"text":"hi","delivery":"queue"}`); code != http.StatusNotImplemented || codeOf(t, body) != "not_implemented" {
		t.Errorf("写面没注入该 501 not_implemented，实际 %d %s", code, body)
	}
}

// executionWideKeys 是 yaml `Execution`（core-v1.yaml:1148~1174）properties 的**全集**，
// 逐键手抄自 schema：少一个 = 读面欠账（第七十九批之前欠的那九个就在里面），
// 多一个 = 发明（`model_config_json` / `error_details_json` / `metadata_json` 这些 schema 没声明的不许进来）。
// 顺序无所谓，断言前自己排。
var executionWideKeys = []string{
	"id", "task_id", "adapter_instance_id", "session_id", "model_id", "predecessor_execution_id",
	"attempt_no", "trigger_type", "status", "state_confidence", "provider_execution_id",
	"model_snapshot", "permission_snapshot", "environment_snapshot",
	"result_code", "error_code", "error_message",
	"started_at", "heartbeat_at", "finished_at",
	"version", "created_at", "updated_at",
}

// wideFieldsOf 从库里逐列读回第八十批扩的那九个字段（**不经过读面**），是下面对照的唯一真值。
// NULL 还原成 nil（响应里就是 null），NOT NULL 的两把时刻还原成 float64（json 数字解出来是这个型）。
func wideFieldsOf(t *testing.T, d *db.DB, id string) map[string]any {
	t.Helper()
	var (
		modelID, resultCode, errorCode, errMsg sql.NullString
		started, heartbeat, finished           sql.NullInt64
		created, updated                       int64
	)
	if err := d.Read.QueryRow(`SELECT model_id, result_code, error_code, error_message,
		started_at, heartbeat_at, finished_at, created_at, updated_at
		FROM executions WHERE id = ?`, id).
		Scan(&modelID, &resultCode, &errorCode, &errMsg, &started, &heartbeat, &finished,
			&created, &updated); err != nil {
		t.Fatalf("读库对照失败（%s）: %v", id, err)
	}
	ns := func(v sql.NullString) any {
		if !v.Valid {
			return nil
		}
		return v.String
	}
	ni := func(v sql.NullInt64) any {
		if !v.Valid {
			return nil
		}
		return float64(v.Int64)
	}
	return map[string]any{
		"model_id": ns(modelID), "result_code": ns(resultCode),
		"error_code": ns(errorCode), "error_message": ns(errMsg),
		"started_at": ni(started), "heartbeat_at": ni(heartbeat), "finished_at": ni(finished),
		"created_at": float64(created), "updated_at": float64(updated),
	}
}

// TestGetExecutionWideFieldsComeFromTheRow 是第八十批（第 297 条）的正面证据：
// yaml `Execution` 里那九个「库里有列、上一批没出」的键，**单条读面出口的就是库里那一行的值** ——
// 有值的出值、没写者的出 null（不折算成 ""/0、也不缺键），列表与单条逐键等值。
// 404/405/501 三个邻居不在这条里复写：它们的判据在 TestGetExecutionGates，本批没动过。
func TestGetExecutionWideFieldsComeFromTheRow(t *testing.T) {
	srv, d := newExecutionHarness(t, true)

	// ── 键集：恰好 schema 那 23 个，一个多一个少都红（"不发明"与"不欠账"是同一条断言的两面）。
	code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-1", "local-user", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET {id} 该 200，实际 %d %s", code, body)
	}
	one := mustParseMap(t, body)
	wantKeys := append([]string(nil), executionWideKeys...)
	sort.Strings(wantKeys)
	if got := strings.Join(sortedKeys(one), ","); got != strings.Join(wantKeys, ",") {
		t.Errorf("键集该逐字等于 yaml `Execution` 那 23 个：%s，实际 %s", strings.Join(wantKeys, ","), got)
	}

	// ── ① 有值那一行（ex-1 夹具写了 model_id / result_code / started_at / finished_at / 两把时刻）：
	//    逐键与**库里那一行**对照，读面不许折算、不许编、不许少带一列。
	for k, w := range wideFieldsOf(t, d, "ex-1") {
		if got := one[k]; got != w {
			t.Errorf("ex-1 的 %s 该是库里那一行的 %#v，实际 %#v", k, w, got)
		}
	}
	if one["model_id"] != "m1" || one["result_code"] != "OK" {
		t.Errorf("ex-1 的 model_id/result_code 该是夹具里那行的 m1/OK，实际 %#v / %#v",
			one["model_id"], one["result_code"])
	}

	// ── ② 没写过的那一行（ex-3 一列都没写）⇒ 缺省形态是 **null**：
	//    这一行从来没走过写路径 ⇒ 包括第八十一批已补写者的那四列（result_code/error_code/
	//    started_at/finished_at）在内仍是 NULL，加上今天仍无信号源的 error_message/heartbeat_at，
	//    出 "" 或 0 就是把 NULL 折算了，缺键就是读面没读。
	//    （写路径真落过值的形态不在这里：那条判据在 send_columns_test.go 走生产链对照。）
	code, body = call(t, srv, http.MethodGet, "/api/v1/executions/ex-3", "local-user", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET ex-3 该 200，实际 %d %s", code, body)
	}
	three := mustParseMap(t, body)
	for k, w := range wideFieldsOf(t, d, "ex-3") {
		if got := three[k]; got != w {
			t.Errorf("ex-3 的 %s 该是库里那一行的 %#v（没写者 ⇒ null），实际 %#v", k, w, got)
		}
	}
	for _, k := range []string{"model_id", "result_code", "error_code", "error_message",
		"started_at", "heartbeat_at", "finished_at"} {
		if v, ok := three[k]; !ok {
			t.Errorf("缺键 %s（可空也要显式出 null）：%v", k, three)
		} else if v != nil {
			t.Errorf("%s 这一行从没被写过、库里是 NULL，出口该 null，实际 %#v", k, v)
		}
	}
	if three["created_at"] == nil || three["updated_at"] == nil {
		t.Errorf("created_at/updated_at 是 NOT NULL 毫秒时钟，不许出 null：%v", three)
	}

	// ── ③ 列表与单条同形不只在键集上：**同一条 row 的这九个值在两条口上逐个相等**
	//    （契约③ §7 同族要求：不许列表一种口径、单条另一种）。
	_, listBody := call(t, srv, http.MethodGet, "/api/v1/executions?task_id=task-1", "local-user", "", "")
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(listBody), &page); err != nil {
		t.Fatal(err)
	}
	var same map[string]any
	for _, it := range page.Items {
		if it["id"] == "ex-1" {
			same = it
		}
	}
	if same == nil {
		t.Fatalf("列表里没有 ex-1：%s", listBody)
	}
	if a, b := sortedKeys(one), sortedKeys(same); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("单条与列表不同形：单条=%v 列表=%v", a, b)
	}
	for _, k := range executionWideKeys {
		// 快照那三格是 map，`!=` 会 panic（uncomparable）⇒ 这里走 DeepEqual。
		if !reflect.DeepEqual(one[k], same[k]) {
			t.Errorf("同一条 ex-1 在单条与列表上 %s 不等：%#v vs %#v", k, one[k], same[k])
		}
	}
}

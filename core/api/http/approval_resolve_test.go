package httpapi_test

// approvals 两条写/读腿的 transport 判据（T-426 ①：`:resolve` 的 handler 接线归 backend）。
//
// 与 T-414 那条列表判据同一条红线：**本文件一行都不碰 `approvals` 表** —— 连测试夹具也不破例。
// 所以这里判的全是 transport 该判的四件事：
//   - 注入的那一对（`GetByID` + `Resolve`）**有没有被真的调用**、参数怎么传（决议人从头部来、
//     主体不许从 body 自报）；
//   - 出口形状（200 那一份与列表那条口**逐键同形**）；
//   - 那一侧给出的答案怎么翻译成码（有名分的才翻译，没名分的一律 500，不拿文案猜）；
//   - 幂等那一把键：同键重放逐字节等于首击、同键换体 409 且一次副作用都不许多做。
//
// 这一包绿**不证明**真审批行能被裁决 —— 那是 `internal/approval` 自己的 96 条与 `core/test` 端到端的事
// （归属清楚，谁也不冒充谁）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// decisionStub 是被注入的那一对能力：只记录收到什么，按吩咐返回行或错误。
type decisionStub struct {
	row        approval.Approval
	getErr     error
	resolveErr error
	seenIDs    []string
	seen       []approval.Resolution
	getCalls   int
}

func (s *decisionStub) GetByID(context.Context, string) (approval.Approval, error) {
	s.getCalls++
	if s.getErr != nil {
		return approval.Approval{}, s.getErr
	}
	return s.row, nil
}

func (s *decisionStub) Resolve(_ context.Context, id string, res approval.Resolution) error {
	s.seenIDs = append(s.seenIDs, id)
	s.seen = append(s.seen, res)
	return s.resolveErr
}

func stubApproval(id, state string) approval.Approval {
	return approval.Approval{
		ID: id, TaskID: "task-1", Type: "TOOL_EXECUTION", Action: "tool.execute",
		RiskLevel: "MEDIUM", State: state, RequestJSON: `{}`, ResolutionJSON: `{}`,
		RequestedAt: 1, Version: 2,
	}
}

func decisionServer(dec httpapi.ApprovalDecider, store httpapi.TaskStore) http.Handler {
	return httpapi.NewHandler(httpapi.Config{
		Token: "sekret-token", Version: "test-1",
		State:            func() string { return "READY" },
		MigrationVersion: func() int64 { return 3 },
		ApprovalDecision: dec,
		Store:            store,
	})
}

func callApproval(t *testing.T, h http.Handler, method, path, body, principal, idem string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer sekret-token")
	if principal != "" {
		req.Header.Set("X-Lucero-Principal", principal)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// openIdemStore 只为幂等那一把键开一座真库：`idempotency_keys` 是**我的**那张可变表
// （绝对禁区的那张叫 approvals，两件事不许混）。
func openIdemStore(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	// 平台层没初始化脱敏就**拒绝写**（fail closed，`redact.go`），所以这座库跟 `newSendHarness` 一样
	// 自带最小规则集：少了这一步，第一次认领直接 500 —— 我今天就是先撞上它、再撞上外键的。
	if err := db.InitRedaction([]string{"token"}, []string{secretShape}, redactMarker); err != nil {
		t.Fatal(err)
	}
	// `idempotency_keys.principal_id` 有外键 —— 生产里这一行由 `cmd/core` 的 ensurePrincipal 保证，
	// 夹具不补就会让第一次认领直接 500（我今天就是这么撞上的，不是判据错）。
	if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
		VALUES ('local-user', 'USER', 'local-user', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGetApprovalServesTheInjectedRead(t *testing.T) {
	stub := &decisionStub{row: stubApproval("ap-9", "PENDING")}
	rec := callApproval(t, decisionServer(stub, nil), http.MethodGet, "/api/v1/approvals/ap-9", "", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("注入了 by-id 读法 ⇒ 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	if len(stub.seenIDs) != 0 || stub.getCalls != 1 {
		t.Fatalf("by-id 这一腿该只调 GetByID 一次，实际 getCalls=%d seenIDs=%v", stub.getCalls, stub.seenIDs)
	}
	var one map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one["id"] != "ap-9" || one["state"] != "PENDING" {
		t.Errorf("出口的两格该就是那一行的，实际 %v / %v", one["id"], one["state"])
	}
	// **两条口同形**：同一行从列表与从详情出去，键集必须一模一样（面板生成一份类型，两份形状必漂移）。
	lst := &approvalStub{items: []approval.Approval{stub.row}}
	lrec := callApproval(t, approvalsServer(lst), http.MethodGet, "/api/v1/approvals", "", "", "")
	var lpage struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(lrec.Body.Bytes(), &lpage); err != nil {
		t.Fatal(err)
	}
	if len(lpage.Items) != 1 {
		t.Fatalf("列表替身该出一条，实际 %d", len(lpage.Items))
	}
	if k1, k2 := keysOf(one), keysOf(lpage.Items[0]); !reflect.DeepEqual(k1, k2) {
		t.Errorf("`GET /approvals/{id}` 与 `GET /approvals` 的键集不一致：详情 %v vs 列表 %v", k1, k2)
	}
}

// TestApprovalReadsAndWritesTranslateBySentinelNotByText 是"只翻译有名分的答案"那一格：
// 同一个 `ErrNotFound` 走 404，任何别的错误走 500 —— **不许拿文案猜码**。
// 为什么这一格现在有牙：§3/§4 还没落（主控要按 audit 那三行落），今天把无名分的错误折成 409
// 就是我先替契约定了"什么算冲突"。
func TestApprovalReadsAndWritesTranslateBySentinelNotByText(t *testing.T) {
	for _, tc := range []struct {
		name       string
		method     string
		getErr     error
		resolveErr error
		wantCode   int
		wantCode2  int // 允许两种（404/500 之类）时填
	}{
		{name: "详情：那一侧说没有这条", method: http.MethodGet, getErr: approval.ErrNotFound, wantCode: http.StatusNotFound},
		{name: "详情：读不出来不是没有", method: http.MethodGet, getErr: errors.New("boom"), wantCode: http.StatusInternalServerError},
		{name: "决议：那一侧说没有这条", method: http.MethodPost, resolveErr: approval.ErrNotFound, wantCode: http.StatusNotFound},
		{name: "决议：没名分的错不自编码", method: http.MethodPost, resolveErr: errors.New("approval: 想不到的"), wantCode: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &decisionStub{row: stubApproval("ap-1", "PENDING"), getErr: tc.getErr, resolveErr: tc.resolveErr}
			body := `{"state":"APPROVED"}`
			path := "/api/v1/approvals/ap-1"
			if tc.method == http.MethodPost {
				path += ":resolve"
			}
			rec := callApproval(t, decisionServer(stub, nil), tc.method, path, body, "local-user", "")
			if rec.Code != tc.wantCode {
				t.Fatalf("该 %d，实际 %d %s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantCode2 != 0 && rec.Code != tc.wantCode2 {
				t.Fatalf("该 %d 或 %d，实际 %d", tc.wantCode, tc.wantCode2, rec.Code)
			}
			var env struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			want := "internal_error"
			if tc.wantCode == http.StatusNotFound {
				want = "not_found"
			}
			if env.Error.Code != want {
				t.Errorf("错误码该是 %s，实际 %s（%s）", want, env.Error.Code, rec.Body.String())
			}
			if tc.method == http.MethodPost && tc.resolveErr != nil && len(stub.seenIDs) != 1 {
				t.Errorf("决议只该被转发一次，实际 %d 次", len(stub.seenIDs))
			}
			// 决议失败时**不许**还去回读那份审批（回读成功会造出"看起来成了"的 200）。
			if tc.method == http.MethodPost && tc.resolveErr != nil && stub.getCalls != 0 {
				t.Errorf("决议没成就该停住，却回读了 %d 次 ⇒ 可能报出个假 200", stub.getCalls)
			}
		})
	}
}

// TestResolveForwardsTheDecisionNotThePolicy 钉转发的那几格：决议人从**头部**来、
// state/note 从 body 来、两条事件身份同源且非空、`Temporary` 由 transport 一律留空
// （那张请求体里没有 grant 的位置，yaml `:1228~1233` 只有两个键）。
func TestResolveForwardsTheDecisionNotThePolicy(t *testing.T) {
	stub := &decisionStub{row: stubApproval("ap-7", "APPROVED")}
	rec := callApproval(t, decisionServer(stub, nil), http.MethodPost, "/api/v1/approvals/ap-7:resolve",
		`{"state":"APPROVED","note":"同意"}`, "local-user", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("决议该被接下来，实际 %d %s", rec.Code, rec.Body.String())
	}
	if len(stub.seen) != 1 || stub.seenIDs[0] != "ap-7" {
		t.Fatalf("转发恰一次并点名这一单，实际 %d 次 ids=%v", len(stub.seen), stub.seenIDs)
	}
	got := stub.seen[0]
	if got.By != "local-user" {
		t.Errorf("决议人必须是那个写主体（头部），实际 %q —— 从 body 自报就是替面板开了伪造门", got.By)
	}
	if got.State != "APPROVED" || got.Note != "同意" {
		t.Errorf("state/note 该原样转，实际 %q/%q", got.State, got.Note)
	}
	if got.Temporary != nil {
		t.Errorf("transport 不许发明一个 temporary grant 请求，实际 %+v", got.Temporary)
	}
	if got.CorrelationID == "" || got.CausationID != got.CorrelationID {
		t.Errorf("事件身份两条要同源且非空（契约② §4），实际 %q/%q", got.CorrelationID, got.CausationID)
	}
	// 200 那一份必须是**回读**来的（不是把请求回显出去）：替身把库里那份的 state 设成 APPROVED。
	var one map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one["state"] != "APPROVED" {
		t.Errorf("响应该是回读到的那一份（state=APPROVED），实际 %v", one["state"])
	}
	if stub.getCalls != 1 {
		t.Errorf("决议之后必须回读一次（yaml 的 200 体就是 Approval schema），实际 %d 次", stub.getCalls)
	}
}

// TestResolveRejectsShapesWithoutTouchingTheDecision 是"形状在这一层拒、策略不在这一层判"的两面：
// 四格形状问题都该 400 且**一次都不转发**；而"这个主体能不能批"这一问这里一次都不出现
// （替身永远答应，所以只要它被调到就说明我没有越权判）。
func TestResolveRejectsShapesWithoutTouchingTheDecision(t *testing.T) {
	for _, tc := range []struct{ name, body, wantTxt string }{
		{"没有请求体", "", "state"},
		{"请求体不是 JSON", "not-json", "state"},
		{"state 缺失", `{}`, "state"},
		{"EXPIRED 不是面板能填的", `{"state":"EXPIRED"}`, "APPROVED"},
		{"CANCELLED 同上", `{"state":"CANCELLED"}`, "APPROVED"},
		{"PENDING 更不是决议", `{"state":"PENDING"}`, "APPROVED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &decisionStub{row: stubApproval("ap-2", "PENDING")}
			rec := callApproval(t, decisionServer(stub, nil), http.MethodPost,
				"/api/v1/approvals/ap-2:resolve", tc.body, "local-user", "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("该 400，实际 %d %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantTxt) {
				t.Errorf("报错里要看得见缺的是哪一格（含 %q），实际 %s", tc.wantTxt, rec.Body.String())
			}
			if len(stub.seen) != 0 {
				t.Errorf("形状就没对，一次都不许转发（转出去就是替那一侧做了决定）：%+v", stub.seen)
			}
		})
	}
	// 中间件那一格：没有主体 ⇒ 403，且 handler 一次都没被叫到。
	// （我**不在 handler 里再判一次主体**：那一格在 `handler.go:235` 已经挡在所有写操作前面，
	// 在这里再写一支 403 就是留一条谁也观察不到的分支 —— 第 306 条那种"无人能观察的状态不留"。）
	stub := &decisionStub{row: stubApproval("ap-3", "PENDING")}
	rec := callApproval(t, decisionServer(stub, nil), http.MethodPost,
		"/api/v1/approvals/ap-3:resolve", `{"state":"APPROVED"}`, "", "")
	// 401 不是 403：`handler.go:235` 那一格给的就是 unauthorized（"没带身份"与"有身份但没权限"
	// 是两件事，v0.1 只有前者会发生）。这里断的是**中间件那一层的话**，不是我另写一支。
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "X-Lucero-Principal") {
		t.Fatalf("缺写主体该被中间件 401 并说清缺哪一头，实际 %d %s", rec.Code, rec.Body.String())
	}
	if len(stub.seen) != 0 {
		t.Errorf("被挡下的那一发不许走到转发：%+v", stub.seen)
	}
}

// TestApprovalViewHandsBackTheGrantIDTheDomainGave 是 T-440 那一格：面板点了「批准并临时允许」之后，
// 出口必须看得见它刚开的那扇门（`grant_id`）。**边界①**：值只从域层已经交出来的那份决议记录里读
// （`internal/approval/approval.go:398` 把它写进 `resolution_json`），本层一行 SQL 都不写、不碰 `temporary_grants`。
// **边界③**：键名逐字取盘上现读（`grant_id`），openapi 那一半归 frontend 续答，我不碰 `specs/**`。
// 三格一起判：有 → 出那个值；没有 → 出 null（**不是省略、也不是猜**）；两条口同值（列表与详情不许各说一套）。
func TestApprovalViewHandsBackTheGrantIDTheDomainGave(t *testing.T) {
	withGrant := stubApproval("ap-g1", "APPROVED")
	withGrant.ResolutionJSON = `{"schema":1,"state":"APPROVED","resolved_by":"local-user","grant_id":"grant-77","grant":{"scope_type":"PROJECT","expires_at":999}}`
	noGrant := stubApproval("ap-g2", "REJECTED")
	noGrant.ResolutionJSON = `{"schema":1,"state":"REJECTED","resolved_by":"local-user"}`

	t.Run("开了临时授权就出那个 id", func(t *testing.T) {
		stub := &decisionStub{row: withGrant}
		rec := callApproval(t, decisionServer(stub, nil), http.MethodGet, "/api/v1/approvals/ap-g1", "", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("该 200，实际 %d %s", rec.Code, rec.Body.String())
		}
		var one map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
			t.Fatal(err)
		}
		if one["grant_id"] != "grant-77" {
			t.Errorf("`grant_id` 该是域层交出来的那一个，实际 %#v（null=没映射上，别的值=编的）", one["grant_id"])
		}
		// 那份记录本身不许被搬空：`resolution` 里还在（面板由此看得见期限那半：grant.expires_at）。
		res, _ := one["resolution"].(map[string]any)
		if res == nil || res["grant"] == nil {
			t.Errorf("`resolution` 里那份 grant 对象不许被吞掉（时限只有它带着），实际 %#v", one["resolution"])
		}
	})
	t.Run("没开授权就出 null：不省略、也不猜", func(t *testing.T) {
		stub := &decisionStub{row: noGrant}
		rec := callApproval(t, decisionServer(stub, nil), http.MethodGet, "/api/v1/approvals/ap-g2", "", "", "")
		one := map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
			t.Fatal(err)
		}
		v, ok := one["grant_id"]
		if !ok {
			t.Fatalf("`grant_id` 这个键必须在（省略是第二种表示，REQ-F-19 同一条理由），实际 %s", rec.Body.String())
		}
		if v != nil {
			t.Errorf("没开临时授权却给出了值 %#v ⇒ 那是编的", v)
		}
	})
	t.Run("列表与详情同值", func(t *testing.T) {
		lst := &approvalStub{items: []approval.Approval{withGrant, noGrant}}
		lrec := callApproval(t, approvalsServer(lst), http.MethodGet, "/api/v1/approvals", "", "", "")
		var page struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(lrec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("两条都该出，实际 %d", len(page.Items))
		}
		if page.Items[0]["grant_id"] != "grant-77" || page.Items[1]["grant_id"] != nil {
			t.Errorf("列表那两条该是 grant-77 / null，实际 %#v / %#v", page.Items[0]["grant_id"], page.Items[1]["grant_id"])
		}
	})
}

// TestResolveConflictIsFourOhFourNoMore 判 §3 规则 8 的第二支：同一个 id 第二次给**不同**的决议
// ⇒ 409（audit 在 T-438 交了名字 `ErrConflictingResolution`，我这才敢翻；之前它落 500 是诚实的"没名分不猜"）。
// 反向那一支（同一份意图重发 ⇒ 幂等成功）由 `...ByteIdentical...` 那格钉着，两支不许并成一个码。
func TestResolveConflictIsFourOhNineNotFiveHundred(t *testing.T) {
	stub := &decisionStub{row: stubApproval("ap-c1", "APPROVED"), resolveErr: approval.ErrConflictingResolution}
	rec := callApproval(t, decisionServer(stub, nil), http.MethodPost,
		"/api/v1/approvals/ap-c1:resolve", `{"state":"REJECTED"}`, "local-user", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("不同决议撞已决的单子该 409，实际 %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"illegal_transition"`) {
		t.Errorf("码名必须是 §15.2 已登记的那一个（yaml `ErrorCode` 是闭集，里面没有泛用 conflict），实际 %s", body)
	}
	if stub.getCalls != 0 {
		t.Errorf("被拒的决议不许还去回读一份假的现状，实际回读了 %d 次", stub.getCalls)
	}
}

// TestResolveIdempotencyIsByteIdenticalAndRunsOnce 是那把键的本体：
// 同键同体重放 ⇒ 逐字节等于首击且**只转发一次**；同键换体 ⇒ 409 且不再转发。
// 为什么这条真需要一座库：重放的那一份响应要来自库里封存的那串（C18 那条"回读的是库里那一份"），
// 替身做不到"伪造一份字节相同的历史"。
func TestResolveIdempotencyIsByteIdenticalAndRunsOnce(t *testing.T) {
	d := openIdemStore(t)
	stub := &decisionStub{row: stubApproval("ap-4", "APPROVED")}
	srv := decisionServer(stub, d)
	first := callApproval(t, srv, http.MethodPost, "/api/v1/approvals/ap-4:resolve",
		`{"state":"APPROVED","note":"同意"}`, "local-user", "k-ap-1")
	if first.Code != http.StatusOK {
		t.Fatalf("首击该 200，实际 %d %s", first.Code, first.Body.String())
	}
	replay := callApproval(t, srv, http.MethodPost, "/api/v1/approvals/ap-4:resolve",
		`{"state":"APPROVED","note":"同意"}`, "local-user", "k-ap-1")
	if replay.Code != http.StatusOK {
		t.Fatalf("重放该 200，实际 %d %s", replay.Code, replay.Body.String())
	}
	if got, want := replay.Body.String(), first.Body.String(); got != want {
		t.Errorf("同键重放必须逐字节等于首击：\n首击 %s\n重放 %s", want, got)
	}
	if n := len(stub.seen); n != 1 {
		t.Fatalf("两次同键请求只该转发一次决议，实际 %d 次：%+v", n, stub.seen)
	}
	// 同一把键换一份决议：必须是 409，且**第三次转发也不许发生**。
	mismatch := callApproval(t, srv, http.MethodPost, "/api/v1/approvals/ap-4:resolve",
		`{"state":"REJECTED","note":"改主意"}`, "local-user", "k-ap-1")
	if mismatch.Code != http.StatusConflict {
		t.Fatalf("同键换体该 409，实际 %d %s", mismatch.Code, mismatch.Body.String())
	}
	if !strings.Contains(mismatch.Body.String(), "idempotency_mismatch") {
		t.Errorf("该用房规那个码名（与 task/project 同一条），实际 %s", mismatch.Body.String())
	}
	if n := len(stub.seen); n != 1 {
		t.Errorf("被拒的那一发不许碰决议，实际转发了 %d 次", n)
	}
	if n := len(stub.seenIDs); n != 1 {
		t.Errorf("回读也不该被多调（决议成没成是那一侧的事），实际 GetByID %d 次", stub.getCalls)
	}
	// 库里那一行确实是 COMPLETED + 带着响应（判据不靠猜：直接数 `idempotency_keys`，那是我的表）。
	var state, resp string
	if err := d.Read.QueryRow(`SELECT state, response_json FROM idempotency_keys WHERE key='k-ap-1'`).
		Scan(&state, &resp); err != nil {
		t.Fatal(err)
	}
	if state != "COMPLETED" || resp != first.Body.String() {
		t.Errorf("封存该是 COMPLETED + 首击那一份，实际 state=%s response_json=%q", state, resp)
	}
}

// TestResolveWithoutKeyStillRunsOnce 钉"没带键按现状放行"那一格（与 `:accept` 同一条既有裁量）：
// 不强制键是 v0.1 的现状（要不要 403 强制归主控），但放行也不能顺手登记一行键。
func TestResolveWithoutKeyStillRunsOnce(t *testing.T) {
	d := openIdemStore(t)
	stub := &decisionStub{row: stubApproval("ap-5", "APPROVED")}
	srv := decisionServer(stub, d)
	rec := callApproval(t, srv, http.MethodPost, "/api/v1/approvals/ap-5:resolve",
		`{"state":"REJECTED"}`, "local-user", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("没带 Idempotency-Key 也该放行，实际 %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM idempotency_keys`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("没给键就不该往幂等表里写一行，实际 %d 行", n)
	}
	if len(stub.seen) != 1 || stub.seen[0].State != "REJECTED" {
		t.Errorf("转发该恰一次且原样，实际 %+v", stub.seen)
	}
}

// TestResolveReadBackFailureDoesNotPretendFourOhFour 是最容易写错的一格：
// **决议已经落了**，这时回读失败不许回 404（那会说成"没有这条审批"，而库里刚被这一发改掉），
// 也不许回 200 空壳 ⇒ 只能是 500 + retryable，并把"带同一把键重试"这条路说清楚。
func TestResolveReadBackFailureDoesNotPretendFourOhFour(t *testing.T) {
	stub := &decisionStub{row: stubApproval("ap-6", "APPROVED"), getErr: approval.ErrNotFound}
	rec := callApproval(t, decisionServer(stub, nil), http.MethodPost,
		"/api/v1/approvals/ap-6:resolve", `{"state":"APPROVED"}`, "local-user", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("回读失败该 500（决议已落，404 是假话、200 是冒充），实际 %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"retryable":true`) {
		t.Errorf("这一格必须可重试（重试走重放/续跑那条腿），实际 %s", body)
	}
	if strings.Contains(body, "没有这条 Approval") {
		t.Errorf("不许把回读失败说成没有这一单：%s", body)
	}
	if len(stub.seen) != 1 {
		t.Errorf("转发仍是一次，实际 %d", len(stub.seen))
	}
}

package httpapi_test

// T-370② 那两格的判据：两条 §3 登记过、Core 侧还没装配的路径，都不许再回一句假话。
// 修的是文案与登记面，**不是把 200 面做出来** —— 所以这一族的判据全是"状态码 + code + 措辞 + 归属说清"，
// 一条也不去断"它能不能返回数据"（那要等 T-370① 那条线，和 T-372 那个读法）。
//
// 两发反例（判决记在账本）：
//   ① 把 splitter 里的 `/output` 那一支摘掉 ⇒ 请求落回 `!ok` ⇒ "不含形状句"那条必须红；
//   ② 把 approvals 的 GET 从 501 改成 200 空页（"冒充有面"那一手）⇒ 501 那条必须红；
//   另有一发：往 approvals.go 里塞一个 `database/sql` 引用 ⇒ 那个"本文件不碰表"的形状判据必须红。

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// errEnvelopeOf 只取这一族要断的三样：code、retryable、message。
type errEnvelopeOf struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func parseErr(t *testing.T, body string) errEnvelopeOf {
	t.Helper()
	var e errEnvelopeOf
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("错误体解不出来：%v（原文 %s）", err, body)
	}
	return e
}

// TestApprovalRoutesAreRegisteredButSayNotWired 钉 ② 的后半：`/api/v1/approvals` 三条形状在 §3 登记过，
// 不能再落兜底那句"这条路径 Core 没登记"；而**真正**没声明过的形状仍然要说那句话（ else 修完 501 就换了一句假话）。
func TestApprovalRoutesAreRegisteredButSayNotWired(t *testing.T) {
	hs := newHarness(t)
	// declared = §3 登记过的那几形（501 文案必须点名"归谁"）；真没声明过的形状不适用这一条。
	cases := []struct {
		name, method, path string
		wantCode           int
		wantContains       []string
		wantAbsent         []string
		wantRetryable      bool
		declared           bool
	}{
		{"列表 GET", http.MethodGet, "/api/v1/approvals", http.StatusNotImplemented,
			[]string{"§3 登记过", "没装配"}, []string{"这条路径 Core 没登记"}, true, true},
		{"列表 POST 是方法不对", http.MethodPost, "/api/v1/approvals", http.StatusMethodNotAllowed,
			[]string{"只声明 GET"}, []string{"没装配"}, false, false},
		{"详情 GET", http.MethodGet, "/api/v1/approvals/ap-1", http.StatusNotImplemented,
			[]string{"§3 登记过", "getApproval"}, []string{"这条路径 Core 没登记"}, true, true},
		{"详情 DELETE 是方法不对", http.MethodDelete, "/api/v1/approvals/ap-1", http.StatusMethodNotAllowed,
			[]string{"只声明 GET"}, nil, false, false},
		{"resolve POST", http.MethodPost, "/api/v1/approvals/ap-1:resolve", http.StatusNotImplemented,
			[]string{"§3 登记过", "resolveApproval"}, []string{"这条路径 Core 没登记"}, true, true},
		{"resolve 用 GET 是方法不对", http.MethodGet, "/api/v1/approvals/ap-1:resolve", http.StatusMethodNotAllowed,
			[]string{"只声明 POST"}, nil, false, false},
		// 下面三形是"确实没声明过"的：那句"Core 没登记 / 没听过"必须是**真话**，别一并改掉。
		{"多一段是没声明的形状", http.MethodGet, "/api/v1/approvals/ap-1/steps", http.StatusNotImplemented,
			[]string{"这条路径 Core 没登记"}, nil, true, false},
		{"空 id 是没声明的形状", http.MethodGet, "/api/v1/approvals/:resolve", http.StatusNotImplemented,
			[]string{"这条路径 Core 没登记"}, nil, true, false},
		{"没听过的子动词", http.MethodGet, "/api/v1/approvals/ap-1:weird", http.StatusNotImplemented,
			[]string{"没听过", "不猜它该走哪条腿"}, nil, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := hs.do(tc.method, tc.path, "sekret-token", "local-user", "")
			if rec.Code != tc.wantCode {
				t.Fatalf("%s %s 该 %d，实际 %d %s", tc.method, tc.path, tc.wantCode, rec.Code, rec.Body.String())
			}
			e := parseErr(t, rec.Body.String())
			want := "not_implemented"
			if tc.wantCode == http.StatusMethodNotAllowed {
				want = "method_not_allowed"
			}
			if e.Error.Code != want {
				t.Errorf("code 该 %q，实际 %q（%s）", want, e.Error.Code, rec.Body.String())
			}
			if e.Error.Retryable != tc.wantRetryable {
				t.Errorf("%s %s 的 retryable 该 %v，实际 %v（%s）", tc.method, tc.path, tc.wantRetryable, e.Error.Retryable, e.Error.Message)
			}
			for _, s := range tc.wantContains {
				if !strings.Contains(e.Error.Message, s) {
					t.Errorf("文案里该有 %q，实际 %q", s, e.Error.Message)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(e.Error.Message, s) {
					t.Errorf("文案里不许再有 %q（那是假话），实际 %q", s, e.Error.Message)
				}
			}
			// 归属必须说清（只对 §3 声明过的形状）：不能只回"没装配"就把人支到 backend 这边来。
			if tc.declared && !strings.Contains(e.Error.Message, "T-372") {
				t.Errorf("501 文案要点名这一格归谁（T-372），实际 %q", e.Error.Message)
			}
		})
	}
}

// TestApprovalsFileNeverTouchesTheTable 是红线判据的形状那一腿：
// `approvals.go` 整个文件不许出现平台读写口的任何一个入口（`approvals` 表是 backend 的绝对禁区）。
// 为什么只能判源：这一格的行为面就是"什么都不读"，而"什么都不读"在用例里等价于"没调用任何东西"⇒
// 断不出来；能断的只有"这一层根本没有拿到入口"。
func TestApprovalsFileNeverTouchesTheTable(t *testing.T) {
	src, err := os.ReadFile("approvals.go")
	if err != nil {
		t.Fatalf("读 approvals.go 失败（这条判据的地基）：%v", err)
	}
	text := string(src)
	// 只列"入口符号"，不列表名 + SQL 关键字的字面组合：`check_ownership.sh` 的 1/9 与 2/9 两条都按字面扫，
	// 连注释与文案一起扫 —— 本批我就是因为在这段注释里写了那一串（为了说明为什么不能写它）而被判越界，
	// 改口之后仍被同一处注释红了一次。教训记进账本第 290 条：**这条门禁看的是字面，不看意图**，
	// 所以"这一层拿不到读写口"这件事只能用入口符号来表达，绝不把那张表的形状抄进非归属包。
	for _, banned := range []string{
		`"database/sql"`, "internal/platform/db", "QueryRow", "Exec(", "Append(", "Touch(", "Create(",
		"Write(", "Store", "SELECT", "INSERT",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("`approvals.go` 里出现了 %q ⇒ 这一格越过了红线（本文件只能是 transport 桩）", banned)
		}
	}
	if !strings.Contains(text, "http.StatusNotImplemented") {
		t.Errorf("`approvals.go` 里没有一处 501 ⇒ 那说明有人把 200 面做在这儿了，归属就又被搬错家")
	}
}

// TestExecutionOutputRouteSaysNotWiredNotUnknownShape 钉 ② 的前半：
// `GET /executions/{id}/output` 是 §3 登记过的路径（yaml `:505`），不能再听它说"形状我不认识"。
func TestExecutionOutputRouteSaysNotWiredNotUnknownShape(t *testing.T) {
	// 路由认领（`registerExecutionRoutes` 的前缀那条）第七十九批起是「读面或写面**任一在场就登记**」，
	// 不再是旧闸 `c.ExecutionSend == nil || c.Store == nil ⇒ 整条不注册`（executions.go 那段注释留着旧口径的来历）。
	// 所以这一格现在**不依赖写面**：`newExecutionHarness(..., wired=false)`（只装 Store）也认领前缀，
	// 五处断言不会再落回兜底那句「这条路径 Core 没登记」—— 那是对 §3 登记过的路径说假话（T-370②）。
	// 这一格要的是路由形状、不读那一行，所以 id 用 ex-out、只保证夹具里那行存在。
	srv, _ := newSendHarness(t, sendSeed{
		execID: "ex-out", status: "RUNNING", sessionID: "sess-out", seedSession: true,
	}, true)

	code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-1/output", "", "", "")
	if code != http.StatusNotImplemented {
		t.Fatalf("GET /output 该 501（登记过、没装配），实际 %d %s", code, body)
	}
	e := parseErr(t, body)
	if e.Error.Code != "not_implemented" || !e.Error.Retryable {
		t.Errorf("该是 retryable 的 not_implemented，实际 %+v", e.Error)
	}
	if strings.Contains(e.Error.Message, "形状我不认识") {
		t.Errorf("对一条 §3 声明过的路径说\"形状我不认识\"是假话（T-370② 点名的就是这句），实际 %q", e.Error.Message)
	}
	for _, want := range []string{"§3 登记过", "读通道没装配", "流式批次", "T-370"} {
		if !strings.Contains(e.Error.Message, want) {
			t.Errorf("文案里该有 %q（要说清缺的是写者不是路由），实际 %q", want, e.Error.Message)
		}
	}

	codePOST, bodyPOST := call(t, srv, http.MethodPost, "/api/v1/executions/ex-1/output", "local-user", "", `{"text":"x"}`)
	if codePOST != http.StatusMethodNotAllowed || !strings.Contains(bodyPOST, "只声明 GET") {
		t.Errorf("POST /output 该 405 并说清只声明 GET，实际 %d %s", codePOST, bodyPOST)
	}

	// 反面对照：真没声明过的子路径必须**仍然**说"形状不认识"，否则这一格是把假话换了个位置。
	code2, body2 := call(t, srv, http.MethodGet, "/api/v1/executions/ex-1/steps", "", "", "")
	if code2 != http.StatusNotImplemented || !strings.Contains(body2, "形状我不认识") {
		t.Errorf("未声明的形状该继续落 !ok 那一支，实际 %d %s", code2, body2)
	}
	// 同一条对照还钉住"别把冒号形状放进来"：approvals 有 :resolve，executions 没有。
	code3, body3 := call(t, srv, http.MethodGet, "/api/v1/executions/ex-1:cancel", "", "", "")
	if code3 != http.StatusNotImplemented || !strings.Contains(body3, "形状我不认识") {
		t.Errorf("executions 名下没有 :verb 形状（yaml 里 :cancel 是另一条路径），该继续被拒，实际 %d %s", code3, body3)
	}
}

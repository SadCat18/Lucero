package httpapi_test

// 第八十一批（甲档）在**生产路径**上的判据：send 那条链真跑一遍之后，
// `GET /api/v1/executions/{id}` 出来的 started_at / result_code / finished_at 是**库里写进去的真值**
// （与 `wideFieldsOf()` 从同一条真行 SELECT 回来的值逐键相等），而没到终态那一趟 finished_at 仍是 null。
//
// 域里的写者形状在 internal/execution/columns_test.go；这一族只管两件那边管不到的事：
// 生产调用点（`executions.go:375` 那一笔）真的把值带出来了，以及读面回读一致。
//
// 两发反例挂在这一族上（判决记账本）：
//   - M1 摘掉终态那笔列写 ⇒ 「真到终态」那半该红；
//   - M2 把列写提前到非终态 ⇒ 「未到终态」那半的 finished_at/result_code 守卫该红。

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// columnKeys 是本族逐键对照的范围：五个甲档列 + 乙档恒 null 的 heartbeat_at。
func columnKeys() []string {
	return []string{"result_code", "error_code", "error_message",
		"started_at", "heartbeat_at", "finished_at"}
}

// getExecMap 走 GET 单条读面拿一份视图（200 之外一律 fatal，别拿错误体当视图比）。
func getExecMap(t *testing.T, srv *httptest.Server, id string) map[string]any {
	t.Helper()
	code, body := call(t, srv, http.MethodGet, "/api/v1/executions/"+id, "local-user", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET /executions/%s 该 200，实际 %d %s", id, code, body)
	}
	return mustParseMap(t, body)
}

// TestSendWritesExecutionColumnsAndReadsThemBack 两腿：
// 只投递（泵那一腿还没收帧）⇒ 到 RUNNING 为止（started_at 有值、终态两列 null）；
// 代泵补一帧 FAILED ⇒ 落终态（result_code/finished_at 出真值，error 两列与 heartbeat_at 仍 null）。
// 第二腿本批改的就是这一格：上一版让替身回执带 FAILED 帧、由 handler 写终态；「Ⅰ」之后那条路已经拆掉。
func TestSendWritesExecutionColumnsAndReadsThemBack(t *testing.T) {
	t.Run("未到终态：started_at 有值，finished_at/result_code 仍 null", func(t *testing.T) {
		srv, d, _ := shapeHarness(t, "ex-fin") // 「Ⅰ」之后 handler 根本不看帧 ⇒ 未终态是默认形状
		code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-fin/send",
			"local-user", "k-cols-open", `{"text":"未裁的码","delivery":"start_now"}`)
		if code != http.StatusAccepted {
			t.Fatalf("send 该 202，实际 %d %s", code, body)
		}
		got := getExecMap(t, srv, "ex-fin")
		row := wideFieldsOf(t, d, "ex-fin")
		for _, k := range columnKeys() {
			if got[k] != row[k] {
				t.Errorf("%s 该是库里那一行的 %#v，实际 %#v", k, row[k], got[k])
			}
		}
		if got["started_at"] == nil {
			t.Errorf("这一趟真走了 execution_started ⇒ started_at 该有值，实际 %v", got["started_at"])
		}
		if got["finished_at"] != nil {
			t.Errorf("没到终态，finished_at 必须是 null（防提前写终态），实际 %#v", got["finished_at"])
		}
		if got["result_code"] != nil {
			t.Errorf("没到终态，result_code 必须是 null，实际 %#v", got["result_code"])
		}
	})

	t.Run("真到终态：result_code/finished_at 出真值，error 与 heartbeat 仍 null", func(t *testing.T) {
		srv, d, _ := shapeHarness(t, "ex-fin")
		code, body := call(t, srv, http.MethodPost, "/api/v1/executions/ex-fin/send",
			"local-user", "k-cols-fin", `{"text":"失败那一格","delivery":"start_now"}`)
		if code != http.StatusAccepted {
			t.Fatalf("send 该 202，实际 %d %s", code, body)
		}
		// 终态不再由投递那一拍写（REQ-TURN-1「Ⅰ」）⇒ 这里代泵补那一帧，列才有真值可读。
		driveFrame(t, d, "ex-fin", "FAILED")
		got := getExecMap(t, srv, "ex-fin")
		row := wideFieldsOf(t, d, "ex-fin")
		for _, k := range columnKeys() {
			if got[k] != row[k] {
				t.Errorf("%s 该是库里那一行的 %#v，实际 %#v", k, row[k], got[k])
			}
		}
		if got["result_code"] != "FAILED" {
			t.Errorf("result_code 该是帧里原话 FAILED，实际 %#v", got["result_code"])
		}
		if got["finished_at"] == nil || got["started_at"] == nil {
			t.Errorf("终态后 started_at/finished_at 该都有值，实际 started=%v finished=%v",
				got["started_at"], got["finished_at"])
		}
		// 收尾那一笔不带 error 信号（`FinishFromTurn` 只吃 result_code）：
		// turn/finished 帧只有 result_code ⇒ 这两列该是 null，**不许**把 transport 文案塞进来。
		if got["error_code"] != nil || got["error_message"] != nil {
			t.Errorf("这一趟没有 error 信号，error_code/error_message 该是 null，实际 %#v / %#v",
				got["error_code"], got["error_message"])
		}
		// 乙档已裁不动：无心跳信号源 ⇒ heartbeat_at 仍是 null。
		if got["heartbeat_at"] != nil {
			t.Errorf("heartbeat_at 是乙档（无信号源），不许写，实际 %#v", got["heartbeat_at"])
		}
	})
}

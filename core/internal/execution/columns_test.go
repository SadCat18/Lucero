package execution_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// 第八十一批（甲档）的判据：`executions` 那几列「schema 有、全仓无写者」里，
// 有信号的四列（result_code / error_code / started_at / finished_at）在**已有的写入时机**落列 ——
// 不新增事件、不改 §6 payload、不动迁移（写者是 `columnWrites`，service.go:216，与 status 同一笔 Touch）。
// 没信号的 error_message 与乙档的 heartbeat_at / provider_execution_id 不写，NULL 是真值，这里就断 NULL。
//
// 两发反例挂在这一族上（判决记账本）：
//   - M1 摘掉终态那笔列写 ⇒ TestFinishFromTurnWritesTerminalColumns 四格全红 + http 终态那腿红（实测 5 行）；
//   - M2 把列写提前到非终态 ⇒ TestStartedWritesStartedAtOnly 的 finished_at=NULL 守卫红，
//     并连坐本用例四格的中途守卫与 http 未到终态那腿（实测 6 行）。

// fiveColumnsOf 直接从库里读回那五列（不经过读面），NULL 还原成 Valid=false ——
// 「NULL 与空串两义不分叉」是这一族断言的地基。
type fiveColumns struct {
	resultCode sql.NullString
	errorCode  sql.NullString
	errorMsg   sql.NullString
	startedAt  sql.NullInt64
	finishedAt sql.NullInt64
}

func fiveColumnsOf(t *testing.T, d *db.DB, id string) fiveColumns {
	t.Helper()
	var c fiveColumns
	if err := d.Read.QueryRow(`SELECT result_code, error_code, error_message, started_at, finished_at
		FROM executions WHERE id = ?`, id).
		Scan(&c.resultCode, &c.errorCode, &c.errorMsg, &c.startedAt, &c.finishedAt); err != nil {
		t.Fatalf("读五列失败（%s）: %v", id, err)
	}
	return c
}

// startFixture 把 execution 摆在 STARTING（「起步那一步当作已发生」，与 service_test:137 同一口径），
// task 摆在 RUNNING（收尾要投影，那是另一个聚合的既有事实，见 finishFixture 的顶注）。
func startFixture(t *testing.T, d *db.DB, execID string) {
	t.Helper()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	must(`UPDATE tasks SET status='RUNNING' WHERE id='task-1'`)
	must(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, created_at, updated_at)
		VALUES ('sess-cols', 'ad-codex', 'remote-cols', 1, 1)`)
	must(`INSERT INTO executions (id, task_id, adapter_instance_id, session_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, 'task-1', 'ad-codex', 'sess-cols', 1, 'STARTING', 'HIGH', 'INITIAL', 1, 1)`, execID)
}

// TestStartedWritesStartedAtOnly 钉「开始」那个时机：`execution_started`（STARTING→RUNNING）
// 那一笔同笔写 started_at；**没到终态** ⇒ finished_at / result_code / error 三列仍是 NULL。
// 后半句是防「提前写终态」的那道守卫（反例 M2 咬的就是它）。
func TestStartedWritesStartedAtOnly(t *testing.T) {
	d := openDB(t)
	seedWriteFixture(t, d)
	startFixture(t, d, "ex-cols-start")
	ctx := context.Background()
	svc := execution.NewService()

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.Started(ctx, tx, "ex-cols-start", execution.Command{
			EventID: "ev-cols-st", PrincipalID: "system",
		})
		return e
	}); err != nil {
		t.Fatalf("STARTING→RUNNING 该走通：%v", err)
	}

	c := fiveColumnsOf(t, d, "ex-cols-start")
	if !c.startedAt.Valid || c.startedAt.Int64 <= 0 {
		t.Errorf("execution_started 那一笔该同笔写 started_at，实际 %+v", c.startedAt)
	}
	if c.finishedAt.Valid {
		t.Errorf("还没到终态，finished_at 必须是 NULL（防提前写终态），实际 %d", c.finishedAt.Int64)
	}
	if c.resultCode.Valid || c.errorCode.Valid || c.errorMsg.Valid {
		t.Errorf("没到终态就不该有结果码/错误列：result=%+v error=%+v msg=%+v",
			c.resultCode, c.errorCode, c.errorMsg)
	}
}

// TestFinishFromTurnWritesTerminalColumns 钉「终态」那个时机：
// 起步（started_at 首刻）→ 收尾（finished_at + result_code），失败那一格再看 error 两列。
// error_code 只在**调用方真给了这个信号**（§6 ResultPayload 的键）时落；error_message 今天全仓
// 没有信号源（§6 payload 没这个键、turn/finished 帧没有 error 字段）⇒ 任何一格都断 NULL。
func TestFinishFromTurnWritesTerminalColumns(t *testing.T) {
	cases := []struct {
		name         string
		resultCode   string
		givenErrCode string // 调用方给的 error_code；"" = 这一趟没有这个信号
	}{
		{"成功那一格", "SUCCEEDED", ""},
		{"失败且没有 error 信号", "FAILED", ""},
		{"失败且调用方给了 error_code", "FAILED", "E_PROVIDER_TIMEOUT"},
		{"取消那一格", "CANCELLED", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := openDB(t)
			seedWriteFixture(t, d)
			id := "ex-cols-" + tc.resultCode + "-x"
			startFixture(t, d, id)
			ctx := context.Background()
			svc := finishSvc()

			// ① 起步：STARTING→RUNNING，started_at 在这一笔落。
			if err := d.Write(ctx, func(tx *db.Tx) error {
				_, e := svc.Started(ctx, tx, id, execution.Command{
					EventID: "ev-cols-go-" + id, PrincipalID: "system",
				})
				return e
			}); err != nil {
				t.Fatalf("起步该走通：%v", err)
			}
			mid := fiveColumnsOf(t, d, id)
			if !mid.startedAt.Valid || mid.finishedAt.Valid {
				t.Fatalf("起步之后该是 started_at 有值、finished_at NULL，实际 %+v", mid)
			}

			// ② 收尾：帧里的 result_code 原话落列（与 §6 payload 同源）。
			if err := d.Write(ctx, func(tx *db.Tx) error {
				_, _, e := svc.FinishFromTurn(ctx, tx, id, tc.resultCode, execution.Command{
					EventID: "ev-cols-fin-" + id, TaskEventID: "ev-cols-t-" + id,
					TaskWaitingAcceptanceEventID: "ev-cols-w-" + id,
					PrincipalID:                  "local-user",
					ErrorCode:                    tc.givenErrCode,
				})
				return e
			}); err != nil {
				t.Fatalf("%s 落终态该走通：%v", tc.resultCode, err)
			}

			c := fiveColumnsOf(t, d, id)
			// 终态三件：result_code 原话、finished_at 非 NULL、started_at 从开始起仍非 NULL。
			if !c.resultCode.Valid || c.resultCode.String != tc.resultCode {
				t.Errorf("终态后 result_code 该是帧里原话 %q，实际 %+v", tc.resultCode, c.resultCode)
			}
			if !c.finishedAt.Valid || c.finishedAt.Int64 <= 0 {
				t.Errorf("终态后 finished_at 该非 NULL，实际 %+v", c.finishedAt)
			}
			if !c.startedAt.Valid || c.startedAt.Int64 <= 0 {
				t.Errorf("这一格走过起步 ⇒ started_at 该非 NULL，实际 %+v", c.startedAt)
			}
			if c.startedAt.Valid && c.finishedAt.Valid && c.startedAt.Int64 > c.finishedAt.Int64 {
				t.Errorf("started_at(%d) 不该晚于 finished_at(%d)", c.startedAt.Int64, c.finishedAt.Int64)
			}
			// error_code：有信号才落，没信号不许自己编一个。
			if tc.givenErrCode == "" {
				if c.errorCode.Valid {
					t.Errorf("这一趟没人给 error_code，列该是 NULL，实际 %+v", c.errorCode)
				}
			} else if !c.errorCode.Valid || c.errorCode.String != tc.givenErrCode {
				t.Errorf("调用方给了 error_code ⇒ 该同笔落列 %q，实际 %+v", tc.givenErrCode, c.errorCode)
			}
			// error_message：今天没有任何信号源（§6 payload 与 turn 帧都没有它）⇒ 一律 NULL。
			if c.errorMsg.Valid {
				t.Errorf("error_message 没有信号源，不许写：实际 %+v", c.errorMsg)
			}
		})
	}
}

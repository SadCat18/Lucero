package execution_test

// `executions.provider_execution_id` 那一列的写者判据（T-408 ① 走的通道 (i)）。
//
// 判的是三条硬时机在**域这一侧**的形状（契约① 第 25 条 ④-a :741，09-30 主控裁）：
//   1. 只在 `turn/claimed` 那一刻 ⇒ 本层管不到帧，管得到的是"这一笔只由 `RecordClaim` 产出"，
//      时机那一腿在 `cmd/core/turnpump_claim_test.go`；
//   2. 不许在 `dispatched` 补写 ⇒ 同上，另有一腿在本文件：`TestStartedDoesNotBackfillTheHandle`
//      （started 那一笔的 payload 本来就带那个键，所以"顺手也写进列"是最省事的错法）；
//   3. 不许借这一笔把状态显示成 running ⇒ 用一条 **STARTING** 的行来打：SET 里没有 `status`，
//      所以这一笔之后它必须还是 STARTING。拿 RUNNING 的行来打就看不见这一格（值相同 ⇒ 假绿）。
//
// 幂等那一格与终态那一格各有一条独立用例：同把手重放 = 什么都不补；异把手 = 报错且一行不写。
// 后者不是啰嗦：一个 Execution 只有一次 Provider 侧的执行（ADR-013/015），"把手换了"只有两种真相
// —— 对端在撒谎，或 Core 把两轮的帧归到同一行。两种都不许用 UPDATE 抹平。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// claimSeedRow 播 `seed()` 里没有的那几形：一条**没定局**的执行（可选带一个已有把手）。
// 状态特意取 STARTING —— 三条硬时机里第三条（不许显示成 running）只有从 STARTING 出发才看得出来。
func claimSeedRow(t *testing.T, d *db.DB, id, status, provider string) {
	t.Helper()
	if provider == "" {
		if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
			status, state_confidence, trigger_type, created_at, updated_at)
			VALUES (?, 'task-1', 'ad-codex', 9, ?, 'HIGH', 'INITIAL', 1, 1)`, id, status); err != nil {
			t.Fatalf("播 %s（%s）: %v", id, status, err)
		}
		return
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, provider_execution_id, created_at, updated_at)
		VALUES (?, 'task-1', 'ad-codex', 9, ?, 'HIGH', 'INITIAL', ?, 1, 1)`, id, status, provider); err != nil {
		t.Fatalf("播 %s（%s，把手 %s）: %v", id, status, provider, err)
	}
}

func claimHandle(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var p string
	if err := d.Read.QueryRow(`SELECT COALESCE(provider_execution_id, '') FROM executions WHERE id = ?`, id).
		Scan(&p); err != nil {
		t.Fatalf("读 %s 的把手: %v", id, err)
	}
	return p
}

func claimVersion(t *testing.T, d *db.DB, id string) int64 {
	t.Helper()
	var v int64
	if err := d.Read.QueryRow(`SELECT version FROM executions WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("读 %s 的 version: %v", id, err)
	}
	return v
}

// auditSpy 数"这一笔叫了几次审计"（T-421 ②：换掉原来那句 `SELECT count(*) FROM audit_entries`）。
type auditSpy struct{ calls int }

// svcWithAuditSpy 把域**唯一**那一扇审计门（`RecoveryDeps.Audit` —— §6 里 Audit 列 ✓ 的事件走它）
// 接上一枚绊线并带出来。为什么绊线而不是数表：数表 = 业务包测试直查别人的那张表（正是这一次要换掉的形状），
// 而 import `internal/audit` 借它的读嘴会撞 C14（业务包的测试不 import 规则包；`recovery.go:41` 明写着这条，
// 起因是 audit 在途的一个 `undefined: sync` 把本包带红过一次）。
// ⇒ 本包能看见的最强形状是"这一笔没有任何一条通往审计的调用"；"库里真零行"那一半挪到 `cmd/core`
//
//	（装配层 import audit 是它的本职，见 turnpump_claim_test.go 的链尾读数）。
func svcWithAuditSpy(spy *auditSpy) *execution.Service {
	s := execution.NewService()
	s.EnableRecovery(execution.RecoveryDeps{Audit: func(context.Context, *db.Tx, execution.AuditRecord) error {
		spy.calls++
		return nil
	}})
	return s
}

// claimedWall 认那道**平台事件目录**的闸（与 `catalogueWall` 同一道墙、不同的名字）：
// `db/touch.go:564` 在 appendEvent 第一句问 `eventjournal.Registered(ev.Type)`，目录住的地方不在本账 SCOPE。
// 登记那半边（db 的常量 + 目录条目、主控的 §6 行与 yaml 枚举，:752 那句"三处同改"）落了之后，
// 本文件那两条"完整形状"用例自动开跑，不需要谁改测试 —— 探针只认那一句话，不认开关。
func claimedWall(t *testing.T, err error) bool {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "is not in the event journal catalogue") {
		return false
	}
	t.Skipf("等 external 两半：`internal/eventjournal/events.go` 的 catalog 缺 execution_claimed（db 的面包）"+
		"+ §6 那一行与 yaml 枚举（主控与 frontend 的面包，T-408 ②「三处同改」）⇒ 落点今天按平台规矩明确失败。"+
		"这一格的完整形状由本用例断，来源：契约① 第 25 条 ④-a :741。实际错误：%v", err)
	return true
}

func claimCmd(eventID, provider string) execution.Command {
	return execution.Command{EventID: eventID, PrincipalID: "system",
		CorrelationID: "corr-claim", CausationID: "corr-claim", ProviderExecutionID: provider}
}

// TestRecordClaimWritesTheHandleWithoutTouchingStatus 是那一笔写的正身：列真落了、版本 +1、
// 恰一条 `execution_claimed`（payload 只有一个键），而 **status 一格没动**、task 一侧零事件、
// §6 的 Audit 列是 `–` ⇒ `audit_entries` 一行都不该有。
func TestRecordClaimWritesTheHandleWithoutTouchingStatus(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	claimSeedRow(t, d, "ex-claim", "STARTING", "")
	spy := &auditSpy{}
	svc := svcWithAuditSpy(spy)
	ctx := context.Background()

	changed, err := claimOnce(ctx, d, svc, "ex-claim", claimCmd("ev-claim-1", "prov-42"))
	if claimedWall(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("把手没落成：%v", err)
	}
	if !changed {
		t.Error("第一次该真写一笔，返回了 changed=false（那调用方就以为没发生）")
	}
	// 硬时机第三条：这一笔不许把状态显示成 running。行是从 STARTING 起步的，所以这一条真有牙 ——
	// 换成 RUNNING 的行来打，"SET 里加了 status" 那种改法也能蒙过去。
	if st, _, _ := execRowWide(t, d, "ex-claim"); st != "STARTING" {
		t.Errorf("claimed 不许动状态（契约② §6 规则 1：开始证明只认 dispatched），实际 %s", st)
	}
	if got := claimHandle(t, d, "ex-claim"); got != "prov-42" {
		t.Errorf("把手该落进 executions.provider_execution_id，实际 %q", got)
	}
	if v := claimVersion(t, d, "ex-claim"); v != 2 {
		t.Errorf("写了一列就该升一版（ADR-017），实际 v%d", v)
	}
	evID, evType, payload := journalRow(t, d, "ex-claim")
	if evType != execution.EventClaimed {
		t.Errorf("事件名该是 §6 裁的那一行 execution_claimed，实际 %q", evType)
	}
	// 载荷只有一个键（:741 点名的 `provider_execution_id`）；`schema` 由平台层补，不算多写的键。
	if !strings.Contains(payload, `"provider_execution_id":"prov-42"`) {
		t.Errorf("payload 里缺点名的那个键，原文 %q", payload)
	}
	for _, ban := range []string{`"status"`, `"from"`, `"result_code"`, `"reason"`} {
		if strings.Contains(payload, ban) {
			t.Errorf("payload 里出现了 %s —— 这一事件不推状态边、也不带结果: %q", ban, payload)
		}
	}
	if n := journalCount(t, d, "ex-claim"); n != 1 {
		t.Errorf("恰一条事件，实际 %d 条（最近那条 id=%s）", n, evID)
	}
	if n := eventsOfType(t, d, "ex-claim", "execution_started"); n != 0 {
		t.Errorf("这一笔不许顺手发 execution_started（那是 dispatched 那半边的事），实际 %d 行", n)
	}
	if n := journalCount(t, d, "task-1"); n != 0 {
		t.Errorf("claimed 不进 §5.3 的边集 ⇒ task 一侧零事件，实际 %d 条", n)
	}
	if spy.calls != 0 {
		t.Errorf("§6 那一行的 Audit 列是 `–` ⇒ 这一笔不该叫审计，实际叫了 %d 次", spy.calls)
	}
}

// TestRecordClaimSameHandleIsNotASecondEvent 是幂等那一格：同把手重放 ⇒ changed=false，
// 库里**一列都没再写**、版本不动、事件不补第二条。
// 泵在真链路上会见到两次同名把手吗：会 —— `turn/claimed` 与它后面那条 `turn/dispatched` 带的是同一个值
// （`cmd/fake-adapter/main.go:542~547`），而重连之后同一轮也可能再来一帧。
func TestRecordClaimSameHandleIsNotASecondEvent(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	claimSeedRow(t, d, "ex-replay", "STARTING", "prov-same")
	svc := execution.NewService()
	ctx := context.Background()
	before := claimVersion(t, d, "ex-replay")

	changed, err := claimOnce(ctx, d, svc, "ex-replay", claimCmd("ev-replay-1", "prov-same"))
	if claimedWall(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("同把手重放该安静地返回，实际报错：%v", err)
	}
	if changed {
		t.Error(`库里已经是这个事实 ⇒ 不该报成"又写了一笔"`)
	}
	if v := claimVersion(t, d, "ex-replay"); v != before {
		t.Errorf("没写就不许升版本：v%d → v%d", before, v)
	}
	if n := journalCount(t, d, "ex-replay"); n != 0 {
		t.Errorf("重放不许补第二条事件，实际 %d 条", n)
	}
}

// TestRecordClaimRefusesWithoutWritingAnything 把五道"不该写"的闸一起钉：
// 每格都要求**列没动、零事件、零 audit**。
// 这一条今天就能绿 —— 五格全在 `Touch` 之前被拦住，撞不到平台目录。
func TestRecordClaimRefusesWithoutWritingAnything(t *testing.T) {
	cases := []struct {
		name       string
		id         string
		status     string
		have       string // 库里已有的把手
		want       string // 帧里带来的把手
		cmdEventID string
		wantErr    error  // nil = 只要求"明确失败"（行不存在那一格的真哨兵在平台层，不在这儿认）
		wantText   string // 报错里必须看得见的原因：为什么拒要说得出
	}{
		{"空串不是把手", "ex-r1", "STARTING", "", "", "ev-r1", execution.ErrInvalidExecution, "provider_execution_id"},
		{"缺事件身份（域不生成 id）", "ex-r2", "STARTING", "", "prov-x", "", execution.ErrInvalidExecution, "event_id"},
		{"异值不许覆盖", "ex-r3", "RUNNING", "prov-old", "prov-new", "ev-r3", execution.ErrClaimConflict, "不许覆盖"},
		{"已定局的行不接受迟到的 claimed", "ex-r4", "SUCCEEDED", "", "prov-late", "ev-r4", execution.ErrIllegalTransition, "已定局"},
		{"行不存在", "ex-none", "", "", "prov-ghost", "ev-r5", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := openDB(t)
			seed(t, d)
			seedSystemPrincipal(t, d)
			initRedactionForWrite(t)
			if tc.status != "" {
				claimSeedRow(t, d, tc.id, tc.status, tc.have)
			}
			spy := &auditSpy{}
			svc := svcWithAuditSpy(spy)
			ctx := context.Background()

			changed, err := claimOnce(ctx, d, svc, tc.id,
				execution.Command{EventID: tc.cmdEventID, PrincipalID: "system",
					ProviderExecutionID: tc.want})
			if err == nil {
				t.Fatalf("这一格该明确失败，却返回了 changed=%v", changed)
			}
			// 认哨兵而不是只认中文：文案会改，`errors.Is` 那一层才是"为什么拒"的合同。
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("这一格该以 %v 失败，实际 %v", tc.wantErr, err)
			}
			if tc.wantText != "" && !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("报错里要看得见原因（含 %q），实际：%v", tc.wantText, err)
			}
			if changed {
				t.Error(`被拒的一格不许报告"写过了"`)
			}
			if tc.status != "" {
				if got := claimHandle(t, d, tc.id); got != tc.have {
					t.Errorf("被拒的一格不许动那一列：应是 %q，实际 %q", tc.have, got)
				}
				if n := journalCount(t, d, tc.id); n != 0 {
					t.Errorf("被拒的一格零事件，实际 %d 条", n)
				}
			}
			if spy.calls != 0 {
				t.Errorf("被拒的一格不许记审计：叫了 %d 次", spy.calls)
			}
		})
	}
}

// TestRecordClaimStopsAtTheJournalCatalogue 是**今天就能绿**的那一条（与 `execution_unknown` 同形）：
// 目录还没登记 `execution_claimed` 时，必须明确失败且一行都不写。它挡的是三种省事的坏修法：
//   - 绕过目录自己 INSERT event_journal（§9.1 P0：写口只有一个）；
//   - 借目录里现成的邻居名先变绿（`execution_started` 是最近的邻 —— 而它带的是"开始证明"，正好是被禁的那格）；
//   - 发现没登记就退化成"只改列不发事件"（那就是 (ii) 那道被明拒的口子，从一个调用点偷回来）。
func TestRecordClaimStopsAtTheJournalCatalogue(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	claimSeedRow(t, d, "ex-wall2", "STARTING", "")
	svc := execution.NewService()
	ctx := context.Background()

	_, err := claimOnce(ctx, d, svc, "ex-wall2", claimCmd("ev-wall-1", "prov-wall"))
	if err == nil {
		// 目录已登记（三处同改落了）⇒ 这一格的正身自动变成"完整形状"，别再等这句话。
		if got := claimHandle(t, d, "ex-wall2"); got != "prov-wall" {
			t.Fatalf("Touch 成功却没写把手（实际 %q）⇒ 请把本用例改成断完整形状", got)
		}
		t.Skipf("事件目录已含 execution_claimed ⇒ 本用例的墙没了；完整形状由 TestRecordClaimWritesTheHandleWithoutTouchingStatus 断")
		return
	}
	if !strings.Contains(err.Error(), "is not in the event journal catalogue") {
		t.Fatalf("今天这道墙应是平台事件目录那句拒绝，实际：%v", err)
	}
	if got := claimHandle(t, d, "ex-wall2"); got != "" {
		t.Errorf("明确失败之外不许有副作用：列被写成 %q", got)
	}
	if n := journalCount(t, d, "ex-wall2"); n != 0 {
		t.Errorf("被拒的一格零事件，实际 %d 条", n)
	}
	// 点的名必须是 execution_claimed —— 说明走的是包常量，不是被顺手换成的邻居。
	if !strings.Contains(err.Error(), `execution_claimed`) {
		t.Errorf("拒绝原因里点的名应是 execution_claimed：%v", err)
	}
	if n := eventsOfType(t, d, "ex-wall2", "execution_started"); n != 0 {
		t.Errorf("不许为了变绿去发现成的邻居事件，execution_started %d 行", n)
	}
}

// TestClaimedIsNotAStateEdgeAndCarriesNoAudit 钉的是这一事件**不属于**哪几张表：
//   - 任何一条迁移都不该产出它（`EventOf` 全对遍历）—— 它是列值的事件，不是状态边；
//     有人要是把它塞进 `EventOf`，`advance` 就可能在某条边上发出 claimed，那正是"补写"的一条后门；
//   - 不在 `auditRequired`（§6 那一行的 Audit 列是 `–`）；
//   - 与邻居 `execution_started` 不同名（那两个键集也不同：started 带 model_snapshot_sha256）。
func TestClaimedIsNotAStateEdgeAndCarriesNoAudit(t *testing.T) {
	all := []execution.State{execution.Created, execution.Starting, execution.Running,
		execution.WaitingApproval, execution.WaitingInput, execution.Paused, execution.Transferring,
		execution.Succeeded, execution.Failed, execution.Cancelled, execution.Interrupted, execution.Unknown}
	for _, from := range all {
		for _, to := range all {
			if got := execution.EventOf(from, to); got == execution.EventClaimed {
				t.Errorf("EventOf(%s,%s) 产出了 execution_claimed ⇒ 它被登记成一条状态边了（它不是）", from, to)
			}
		}
	}
	if execution.RequiresAudit(execution.EventClaimed) {
		t.Error("§6 那一行的 Audit 列是 `–` ⇒ claimed 不许进 auditRequired")
	}
	if execution.EventClaimed == execution.EventOf(execution.Starting, execution.Running) {
		t.Error(`claimed 与 started 并成一个名字 ⇒ "开始证明只认 dispatched"这条就没事件可查了`)
	}
	m, err := execution.Payload(execution.ClaimedPayload{ProviderExecutionID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Errorf("payload 只该有一个键（:741 点名的 provider_execution_id），实际 %v", m)
	}
}

// TestStartedDoesNotBackfillTheHandle 是硬时机第二条（**不许在 dispatched/started 补写**）在域这一侧的形状。
//
// 为什么这一格值得单独一条而不是只写在注释里：`execution_started` 的 §6 payload **本来就带**
// `provider_execution_id`（`event-contract-v0.1.md:214`），而 `Command` 里也有那个字段 ——
// 所以"顺手把事件里那个值也写进列"是一次极其自然的改动（`columnWrites` 加一项就行），
// 加完之后事件层什么都不会红，只有这一条会。裁 (i) 之后列的写者只有 `RecordClaim` 一个。
// 这一条今天就能绿（它不碰 `execution_claimed`，撞不到平台目录）。
func TestStartedDoesNotBackfillTheHandle(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)
	claimSeedRow(t, d, "ex-nobf", "STARTING", "")
	svc := execution.NewService()
	ctx := context.Background()

	if _, err := claimAdvanceToRunning(ctx, d, svc, "ex-nobf", "prov-from-started"); err != nil {
		t.Fatalf("STARTING→RUNNING 该走得通：%v", err)
	}
	if got := claimHandle(t, d, "ex-nobf"); got != "" {
		t.Errorf("started 那一笔不许把把手补进列（:741 硬时机第二条），实际 %q", got)
	}
	// 而事件 payload 里那个键是 §6 那行要求的，不许被这一条一起禁掉：
	// 列与事件不是同一个事实的同一份记录（列 = "Core 从 claimed 那一刻接住的把手"）。
	_, evType, payload := journalRow(t, d, "ex-nobf")
	if evType != execution.EventStarted {
		t.Fatalf("最近那条事件该是 execution_started，实际 %q", evType)
	}
	if !strings.Contains(payload, `"provider_execution_id":"prov-from-started"`) {
		t.Errorf("execution_started 的 §6 payload 里要点名那个键，原文 %q", payload)
	}
}

// claimAdvanceToRunning 走 `Started` 那一笔（域自己的合法推进路径）。
func claimAdvanceToRunning(ctx context.Context, d *db.DB, svc *execution.Service, id, provider string) (execution.Result, error) {
	var res execution.Result
	err := d.Write(ctx, func(tx *db.Tx) error {
		var e error
		res, e = svc.Started(ctx, tx, id, execution.Command{
			EventID: "ev-nobf-1", PrincipalID: "system", SnapshotSHA256: "sha-1", ProviderExecutionID: provider})
		return e
	})
	return res, err
}

// claimOnce 把"开一笔写事务调一次"这一步收起来：几个用例都要问同一句，别让返回值抄两遍。
func claimOnce(ctx context.Context, d *db.DB, svc *execution.Service, id string,
	cmd execution.Command) (bool, error) {
	var changed bool
	err := d.Write(ctx, func(tx *db.Tx) error {
		c, e := svc.RecordClaim(ctx, tx, id, cmd)
		changed = c
		return e
	})
	return changed, err
}

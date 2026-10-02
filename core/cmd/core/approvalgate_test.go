package main

// 审批门那一腿的泵侧判据（T-442 ② 第一到第三步；域那一格"这条边叫什么、载荷几个键"在
// `internal/execution/approval_edges_test.go`，第 317 条已交）。
//
// 这一格里最容易写错的四件事，各有一条对着它：
//   1. **把"门"做成"通知"**：落了审批行却没推那条边 ⇒ 执行还在 RUNNING，Provider 照旧往下走，
//      面板上那张单子谁也不知道该不该批（`TestApprovalRequestFrameClosesTheGate` 断状态 + 恰一条事件）；
//   2. **自己回答适配器**：本轮没收到用户的决议就发 `approval/respond` ⇒ 审批成了装饰
//      （同一条用例反断：库里不许出现 `execution_approval_resolved`，Core 这一腿一条出站帧都不许有）；
//   3. **词表抄第二份**：风险等级不叫 `permission.RiskLevel`、写成一个本地常量
//      （`TestApprovalRequestRecordsTheRiskLevelPermissionSays` 用一枚**未登记**的动作钉死：
//      真入口给 `UNKNOWN`，本地常量给不出）；
//   4. **迟到的帧也照落**：那一轮已定局还去插审批行/推边 ⇒ 库里多一张没人等的 PENDING
//      （`TestLateApprovalRequestAfterTheTurnSettlesWritesNothing` 走真收尾帧把轮定掉，再送这一帧）。
//
// 用真 Engine、真 `approval.Service`、真库，不用桩：这三件凑在一起才是"这一腿在生产里会长什么样"。
// 桩能给的答案，`permission` 那张真表也能给，而桩会给出一个**盘上不存在**的答案（§3.0-d 那句
// "拿不到就留空"只有走真入口才可证）。

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

// gateNow 是一只**钉死的钟**：这一腿要的期限是"15 分钟后的那个绝对毫秒数"，
// 所以判据可以直接断 `expires_at - requested_at == 900000`，不需要推进时间。
const gateNow int64 = 1_700_000_000_000

func gateClock() func() int64 { return func() int64 { return gateNow } }

// approvalRequestFrame 造一条 §6 第七条通知（真形状：方法名与三个字段都从 adapter 包读，不自己拼字符串）。
func approvalRequestFrame(id, action, resource string) process.Inbound {
	return process.Inbound{ExecutionID: id, Frame: &adapter.StreamFrame{
		Method: adapter.MethodApprovalRequest, Action: action, Resource: resource}}
}

// ensurePrincipal 补一条主体行：`event_journal.principal_id` 有外键，而这一腿用的主体是
// `local-user`（§3 规则 7 那句"唯一可接受的字面量"）。已经在就什么都不做——
// 这条判据不许靠"我先插了一次"成立。
func ensurePrincipal(t *testing.T, d *db.DB, id string) {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM principals WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		return
	}
	if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
		VALUES (?, 'USER', ?, 1, 1)`, id, id); err != nil {
		t.Fatalf("播 principals(%s): %v", id, err)
	}
}

// wireGate 把门接到真三件上（Engine 读真 policy_rules/空表、Service 写真 approvals 行、读面是我自己的 Repository）。
// 返回那个 Service 只是为了让用例**用 audit 交出来的读法**回读那一行，不去碰那张表。
func wireGate(t *testing.T, hub *turnHub, d *db.DB) *approval.Service {
	t.Helper()
	asks := approval.NewService(approval.NewSQLiteStore(d, gateClock()), gateClock())
	hub.enableApprovalGate(permission.New(permission.NewSQLiteSource(d), gateClock()),
		asks, execution.NewRepository(d.Read), gateClock())
	return asks
}

func gateStatus(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.Read.QueryRow(`SELECT status FROM executions WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func countJournalType(t *testing.T, d *db.DB, id, evType string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal
		WHERE aggregate_type = 'execution' AND aggregate_id = ? AND event_type = ?`, id, evType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countAuditRows(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM audit_entries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// countAuditFor 数"这一条边自己的归因行"。为什么不数总行数：同一笔里除了我那一条，
// `approval.Service.Request` 那一腿也会按它自己的链留下行（那归 audit 的口径），
// 数总数会把别人的正确行为读成我的多余，反之也成。
func countAuditFor(t *testing.T, d *db.DB, action string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM audit_entries WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// auditActions 只为把失败现场说清（红了要能一眼看见落的是哪几种 action）。
func auditActions(t *testing.T, d *db.DB) []string {
	t.Helper()
	rows, err := d.Read.Query(`SELECT action FROM audit_entries ORDER BY audit_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func journalPayload(t *testing.T, d *db.DB, id, evType string) map[string]any {
	t.Helper()
	var raw string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal
		WHERE aggregate_type = 'execution' AND aggregate_id = ? AND event_type = ?`, id, evType).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("%s 的 payload 读不成对象: %v（原文 %q）", evType, err, raw)
	}
	return m
}

// pendingApprovals 用 audit 的列表口数挂着的单子（**不碰 approvals 表**：那条禁令对测试夹具也不例外）。
func pendingApprovals(t *testing.T, svc *approval.Service) []approval.Approval {
	t.Helper()
	pending := approval.PENDING
	rows, _, err := svc.ListPage(context.Background(), approval.ListFilter{State: &pending, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func newGateHub(t *testing.T, id string) (*turnHub, *db.DB, *logTap, func()) {
	t.Helper()
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	hub := pipe.hub
	// 这一腿会写审计行 ⇒ 泵那份 `auditedStore` 必须带上锚点计数器：`rejections.go:141` 那句
	// `after.Seq == before.Seq` 的早退保护不了"写了审计行的那一笔"，没计数器就 panic（我第一发就是这么红的，
	// 红的是我的装配，不是产品码 —— 产品码在 boot 里一直带着 `core.checkpoints`，见 main.go:183 与 :229）。
	hub.store = auditedStore{d: d, checkpoints: audit.NewCheckpointCounter(
		&audit.Anchor{Path: filepath.Join(t.TempDir(), "audit.head")}, gateClock())}
	restore := swapLogf(hub)
	var tap logTap
	hub.logf = tap.f
	ensurePrincipal(t, d, "local-user")
	seedOwnedRow(t, d, id, "ad-fake", "fake")
	return hub, d, &tap, restore
}

// TestApprovalRequestFrameClosesTheGate 是这一腿的正面：一条高危动作的 `approval/request` 进来之后
// 库里必须同时出现三样东西，缺一不可 —— 一张 PENDING 的审批行（走 audit 的读法回验，四个字段逐个对）、
// 一条 `execution_approval_requested`（聚合 execution、四键与那一行同值）、一条同事务的 audit 记录；
// 状态停在 WAITING_APPROVAL，而**没有**任何 resolved 事件（门，不是通知）。
func TestApprovalRequestFrameClosesTheGate(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-gate")
	defer restore()
	asks := wireGate(t, hub, d)
	auditBefore := countAuditRows(t, d)

	hub.handle("fake", 1, approvalRequestFrame("ex-gate", "filesystem.write", "src/a.txt"))

	if got := gateStatus(t, d, "ex-gate"); got != "WAITING_APPROVAL" {
		t.Errorf("库里 status = %q，门必须把这一格关上（WAITING_APPROVAL）；还是 RUNNING 就等于把审批做成了通知", got)
	}
	rows := pendingApprovals(t, asks)
	if len(rows) != 1 {
		t.Fatalf("挂着的审批行 %d 张，应为恰一张", len(rows))
	}
	a := rows[0]
	if a.Action != "filesystem.write" || a.Resource != "src/a.txt" {
		t.Errorf("那一行的动作/资源 = %q/%q，应为 filesystem.write/src/a.txt", a.Action, a.Resource)
	}
	if a.Type != "PERMISSION" {
		t.Errorf("approval_type = %q，§3.0-d 裁的是 v0.1 唯一来源 PERMISSION", a.Type)
	}
	if a.RiskLevel != "HIGH" {
		t.Errorf("risk_level = %q，§3.1 的内置分类说 filesystem.write 是 HIGH", a.RiskLevel)
	}
	if a.ExpiresAt-a.RequestedAt != 900000 {
		t.Errorf("期限 = %d ms（%d→%d），§3.0-d 裁的是 15 分钟 = 900000", a.ExpiresAt-a.RequestedAt, a.RequestedAt, a.ExpiresAt)
	}
	if a.ExecutionID != "ex-gate" {
		t.Errorf("那一行没绑到这条执行上：execution_id = %q", a.ExecutionID)
	}

	if n := countJournalType(t, d, "ex-gate", execution.EventApprovalRequested); n != 1 {
		t.Errorf("%s 事件 %d 条，一条状态边该恰带一条", execution.EventApprovalRequested, n)
	}
	m := journalPayload(t, d, "ex-gate", execution.EventApprovalRequested)
	for k, want := range map[string]string{"approval_id": a.ID, "action": "filesystem.write",
		"resource": "src/a.txt", "risk_level": "HIGH"} {
		if s, _ := m[k].(string); s != want {
			t.Errorf("事件载荷 %s = %v，应为 %q", k, m[k], want)
		}
	}
	// 门没开：不许有任何"已放行"的证据。
	if n := countJournalType(t, d, "ex-gate", execution.EventApprovalResolved); n != 0 {
		t.Errorf("%s 出现了 %d 条：用户还没批，Core 不能自己把门打开", execution.EventApprovalResolved, n)
	}
	// §6 那两行的 Audit 列是 ✓ ⇒ 同事务一条归因行。
	if n := countAuditFor(t, d, execution.EventApprovalRequested); n != 1 {
		t.Errorf("%s 的归因行 %d 条，这一腿该带恰一条（§6 那两行的 Audit 列是 ✓）；当场 action 全列:%v",
			execution.EventApprovalRequested, n, auditActions(t, d))
	}
	if n := countAuditRows(t, d); n <= auditBefore {
		t.Errorf("audit_entries 一共 %d 条（改前 %d）⇒ 这一腿一条都没留下", n, auditBefore)
	}
	t.Logf("这一笔之后 audit_entries 的 action 全列：%v", auditActions(t, d))
	if n := tap.count("停在审批门上"); n != 1 {
		t.Errorf("门关上了要恰喊一声，实际 %d 声；那一行的原文:%q", n, tap.lineWith("停在审批门上"))
	}
}

// TestApprovalRequestRecordsTheRiskLevelPermissionSays 钉"风险等级只有一个出处"（③ 那枚反例的正身）：
// 未登记的动作必须落成 `UNKNOWN`。写死一个本地常量的人会让这一格红——那正是 §3.0-d 拒绝的第二份词表。
func TestApprovalRequestRecordsTheRiskLevelPermissionSays(t *testing.T) {
	for _, c := range []struct{ name, action, want string }{
		{"内置高风险集里的动作", "command.exec", "HIGH"},
		{"分类器不认识的动作", "vendor.unregistered.action", permission.RiskUnknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			hub, d, _, restore := newGateHub(t, "ex-risk")
			defer restore()
			asks := wireGate(t, hub, d)

			hub.handle("fake", 1, approvalRequestFrame("ex-risk", c.action, ""))

			if got := gateStatus(t, d, "ex-risk"); got != "WAITING_APPROVAL" {
				t.Fatalf("库里 status = %q：两种动作都该失败关闭成「要人批」（§3.1 第 1、2 行）", got)
			}
			rows := pendingApprovals(t, asks)
			if len(rows) != 1 {
				t.Fatalf("审批行 %d 张", len(rows))
			}
			if rows[0].RiskLevel != c.want {
				t.Errorf("那一行的 risk_level = %q，`permission.RiskLevel(%q)` 给的是 %q",
					rows[0].RiskLevel, c.action, c.want)
			}
			m := journalPayload(t, d, "ex-risk", execution.EventApprovalRequested)
			if s, _ := m["risk_level"].(string); s != rows[0].RiskLevel {
				t.Errorf("事件里 risk_level = %v 而库里那一列是 %q：两处同事实必须同值（不许一边 UNKNOWN 一边 HIGH）",
					m["risk_level"], rows[0].RiskLevel)
			}
			if s, _ := m["action"].(string); s != c.action {
				t.Errorf("事件里 action = %v，应为 %q", m["action"], c.action)
			}
		})
	}
}

// TestLowRiskApprovalRequestWritesNothing 钉判定说"不用人批"那一支：不插单、不推边、只喊。
// 这一格今天**不送任何出站帧**（协议没有第二条 A→C 的答案帧可复用），日志里要能看见它为什么没动。
func TestLowRiskApprovalRequestWritesNothing(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-allow")
	defer restore()
	asks := wireGate(t, hub, d)
	auditBefore := countAuditRows(t, d)

	hub.handle("fake", 1, approvalRequestFrame("ex-allow", "session.send", "sess-1"))

	if got := gateStatus(t, d, "ex-allow"); got != "RUNNING" {
		t.Errorf("库里 status = %q：低/中风险不该被挂到审批门上（§3.1 第三行 DEFAULT_ALLOW）", got)
	}
	if n := len(pendingApprovals(t, asks)); n != 0 {
		t.Errorf("不该有审批行，实际 %d 张", n)
	}
	if n := countJournalType(t, d, "ex-allow", execution.EventApprovalRequested); n != 0 {
		t.Errorf("不该有 %s 事件，实际 %d 条", execution.EventApprovalRequested, n)
	}
	if n := countAuditRows(t, d); n != auditBefore {
		t.Errorf("audit_entries %d→%d：一行都不写的时候归因行也不许多", auditBefore, n)
	}
	if line := tap.lineWith("不用人批"); line == "" || !strings.Contains(line, "ALLOW") {
		t.Errorf("这一支要把「判定是什么、所以没动」喊出来（ALLOW 得在句子里），实际:%q", line)
	}
}

// TestLateApprovalRequestAfterTheTurnSettlesWritesNothing 是主控点名的那枚反例的正面：
// 那一轮已经由 `turn/finished` 定局 ⇒ 迟到的 `approval/request` 不许落审批行、不许改终态、只喊。
// 收尾走**真路径**（真帧进真泵真落库），不用裸 SQL 造一个终态行——那会把这条判据建立在假数据上。
func TestLateApprovalRequestAfterTheTurnSettlesWritesNothing(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-late")
	defer restore()
	asks := wireGate(t, hub, d)

	hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-late",
		Frame: &adapter.StreamFrame{Method: adapter.MethodTurnFinished, ResultCode: "SUCCEEDED"}})
	if got := gateStatus(t, d, "ex-late"); got != "SUCCEEDED" {
		t.Fatalf("前置不成立：收尾帧没把这一轮定掉（库里是 %q）", got)
	}
	auditBefore := countAuditRows(t, d)

	hub.handle("fake", 1, approvalRequestFrame("ex-late", "db.write", "orders"))

	if got := gateStatus(t, d, "ex-late"); got != "SUCCEEDED" {
		t.Errorf("迟到的审批帧把已定局的一轮改成了 %q ⇒ 正是 §6 规则 8 要堵的那一格", got)
	}
	if n := len(pendingApprovals(t, asks)); n != 0 {
		t.Errorf("迟到的帧落了 %d 张审批行：那一轮没有人会去批它", n)
	}
	if n := countJournalType(t, d, "ex-late", execution.EventApprovalRequested); n != 0 {
		t.Errorf("迟到的帧推了 %d 条 %s 事件", n, execution.EventApprovalRequested)
	}
	if n := countAuditRows(t, d); n != auditBefore {
		t.Errorf("audit_entries %d→%d：不写库就不该有归因行", auditBefore, n)
	}
	if line := tap.lineWith("迟到的 approval/request"); line == "" || !strings.Contains(line, "一行都不写") {
		t.Errorf("迟到那一格要喊出来（否则日志里「什么都没发生」与「没走到」分不开），实际:%q", line)
	}
}

// TestGateWithoutWiringRefusesTheFrame 钉装配缺位那一支：没接门的泵收到这一帧**一行都不写**，
// 并且要把"是门没装配"这句话说出口 —— 不然库里干净会被读成"这一轮没要审批"。
func TestGateWithoutWiringRefusesTheFrame(t *testing.T) {
	hub, d, tap, restore := newGateHub(t, "ex-nowire")
	defer restore()

	hub.handle("fake", 1, approvalRequestFrame("ex-nowire", "secret.read", "vault/token"))

	if got := gateStatus(t, d, "ex-nowire"); got != "RUNNING" {
		t.Errorf("没装配门却把状态推成了 %q", got)
	}
	if n := countJournalType(t, d, "ex-nowire", execution.EventApprovalRequested); n != 0 {
		t.Errorf("没装配门却落了 %d 条事件", n)
	}
	if line := tap.lineWith("审批门没装配"); line == "" {
		t.Errorf("要把缺装配这件事点名喊出来（不然库里干净会被读成「这一轮没要审批」），日志里没有那一行")
	}
}

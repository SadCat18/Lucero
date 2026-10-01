package execution

// 两张审批状态边**落得了库**的判据（T-442 ①）。
//
// 为什么这一格值得单独钉：平台规矩是"没有 §6 事件的状态边写不进去"（`advance` 里那句
// `§6 没有对应事件（REQ-DB-4）` 直接翻 ErrNoEventForTransition），而 ① 之前 `EventOf` 对这两条边
// 返回的正是 ""。所以 ① 的全部意义不是"多两个字符串"，而是**那两条边从写不通变成写得通**——
// 这件事只能在真库上断，只断 `EventOf` 的返回值是不够的（名字对了但 payload 拼不出来、
// 或者 Touch 被聚合校验挡下，一个纯函数用例全都看不见）。
//
// 为什么住在 `package execution` 而不是 `execution_test`：要走的是 `advance` 那条**所有推进共用**的
// 私有路径。本包今天没有导出方法能把执行推到 WAITING_APPROVAL（② 落 `cmd/core` 那一腿时才需要它，
// 名字归那一批）；在这里另造一个只给测试用的导出口，就是造一个"没人调的接口"——那正是 §3.0-c
// 那张表要防的形状，我不干。
//
// 建库与播数据这几样在本包的 `execution_test` 里各有一份（`openDB`/`seedWriteFixture`），
// Go 不让两个测试包互拿助手，所以这里是一份同规矩的副本：外键前提走裸 SQL，因为它们是
// **别的聚合的既有事实**，不是本包要测的写路径。
//
// 还有一处口径要说清：`APPROVED` 那个串写的是字面量。它的权威住在 `internal/approval`（audit 的包），
// 而 C14 禁止本包（含本包测试）import 别的规则包 ⇒ 抄字面量是这里唯一诚实的写法，
// 语义在 §3 规则 8 与那张状态表里，不在这一行。

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

func approvalEdgeDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	// 平台层不初始化脱敏就拒绝写（fail closed）；规则的最小副本自带，理由同 seedWriteFixture。
	if err := db.InitRedaction([]string{"token"}, []string{`sk-[A-Za-z0-9_-]{16,}`}, "[REDACTED]"); err != nil {
		t.Fatalf("装脱敏规则: %v", err)
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Read.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		must(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0])
	}
	must(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES ('ad-edge', 'edge', 'edge', 1, 1)`)
	must(`INSERT INTO tasks (id, title, created_at, updated_at) VALUES ('task-edge', '审批边', 1, 1)`)
	return d
}

// edgeCmd 给一笔推进的身份四件（事件 id 由调用方预生成，域不自己铸 —— §3）。
func edgeCmd(eventID string) Command {
	return Command{EventID: eventID, PrincipalID: "local-user",
		CorrelationID: "corr-edge", CausationID: "corr-edge"}
}

// blockedExecution 把一条执行真推到 RUNNING；withGate 为真时再走一次**好的**请求边，
// 让它停在 WAITING_APPROVAL。推状态一律走写口，不用裸 SQL 改 status：那等于把
// "这条边落得了库"的判据建立在一行假数据上。
func blockedExecution(t *testing.T, ctx context.Context, d *db.DB, svc *Service, id string, withGate bool) {
	t.Helper()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Create(ctx, tx, NewExecution{
			ID: id, TaskID: "task-edge", AdapterInstanceID: "ad-edge", AttemptNo: 1,
			TriggerType: TriggerInitial, EventID: "ev-" + id + "-created",
			PrincipalID: "system", CorrelationID: "corr-" + id, CausationID: "corr-" + id,
		})
		return err
	}); err != nil {
		t.Fatalf("建执行: %v", err)
	}
	for _, step := range []struct {
		to  State
		cmd Command
	}{
		{Starting, edgeCmd("ev-" + id + "-starting")},
		{Running, edgeCmd("ev-" + id + "-started")},
	} {
		to, cmd := step.to, step.cmd
		if err := d.Write(ctx, func(tx *db.Tx) error {
			_, err := svc.advance(ctx, tx, id, to, cmd)
			return err
		}); err != nil {
			t.Fatalf("推到 %s: %v", to, err)
		}
	}
	if !withGate {
		return
	}
	cmd := edgeCmd("ev-" + id + "-requested")
	cmd.ApprovalRequested = ApprovalRequestedPayload{
		ApprovalID: "apr-" + id, Action: "filesystem.write", Resource: "src/a.txt", RiskLevel: "HIGH"}
	if _, err := edgeAdvance(ctx, d, svc, id, WaitingApproval, cmd); err != nil {
		t.Fatalf("前置那一格（好的请求边）自己就没走通: %v", err)
	}
}

// edgeAdvance 走一次真写事务，返回 advance 的结果与错误（判据要的是"库里到底落没落"）。
func edgeAdvance(ctx context.Context, d *db.DB, svc *Service, id string, to State, cmd Command) (Result, error) {
	var res Result
	err := d.Write(ctx, func(tx *db.Tx) error {
		r, e := svc.advance(ctx, tx, id, to, cmd)
		res = r
		return e
	})
	return res, err
}

func edgeStatus(t *testing.T, d *db.DB, id string) (string, int64) {
	t.Helper()
	var status string
	var version int64
	if err := d.Read.QueryRow(`SELECT status, version FROM executions WHERE id = ?`, id).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	return status, version
}

type edgeRow struct {
	Type, Aggregate, Payload string
}

// edgeJournal 把这条执行的事件按写入顺序读回来。排序用 seq 而不是 id：
// `event_journal` 的 `seq` 才是那列有序证据（§6 规则 3 的对拍靠它）。
func edgeJournal(t *testing.T, d *db.DB, id string) []edgeRow {
	t.Helper()
	rows, err := d.Read.Query(`SELECT event_type, aggregate_type, payload_json FROM event_journal
		WHERE aggregate_type = 'execution' AND aggregate_id = ? ORDER BY seq`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []edgeRow
	for rows.Next() {
		var r edgeRow
		if err := rows.Scan(&r.Type, &r.Aggregate, &r.Payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func edgePayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("payload 不是能读的 JSON: %v（原文 %q）", err, raw)
	}
	return m
}

func edgeKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestApprovalBlockEdgeLandsInLibrary 钉 RUNNING→WAITING_APPROVAL：状态真的停住了、恰多出一条
// `execution_approval_requested`、**聚合是 execution**（这一格就是 REQ-APR-1 那一刀 —— 目录昨天写
// approval，那样平台层会当场拒绝，句子是 `aggregate_type %q but touching %q`）、版本恰好 +1。
func TestApprovalBlockEdgeLandsInLibrary(t *testing.T) {
	ctx := context.Background()
	d := approvalEdgeDB(t)
	svc := NewService()
	blockedExecution(t, ctx, d, svc, "ex-block", false)
	before := edgeJournal(t, d, "ex-block")
	_, verBefore := edgeStatus(t, d, "ex-block")

	cmd := edgeCmd("ev-block-requested")
	cmd.ApprovalRequested = ApprovalRequestedPayload{
		ApprovalID: "apr-1", Action: "filesystem.write", Resource: "src/a.txt", RiskLevel: "HIGH"}
	res, err := edgeAdvance(ctx, d, svc, "ex-block", WaitingApproval, cmd)
	if err != nil {
		t.Fatalf("这条边昨天落不了库（EventOf 给的是空串），今天必须走得通: %v", err)
	}
	if !res.Advanced || res.From != Running || res.To != WaitingApproval {
		t.Errorf("推进结果 = %+v，应为 Running→WaitingApproval 且 Advanced", res)
	}
	status, ver := edgeStatus(t, d, "ex-block")
	if status != string(WaitingApproval) {
		t.Errorf("库里 status = %q，应为 WAITING_APPROVAL（门没关上就是假的）", status)
	}
	if ver != verBefore+1 {
		t.Errorf("版本 %d→%d，一条状态边该恰好推一格", verBefore, ver)
	}
	after := edgeJournal(t, d, "ex-block")
	if len(after) != len(before)+1 {
		t.Fatalf("事件 %d→%d 行，一条状态边应恰带一条事件", len(before), len(after))
	}
	if last := after[len(after)-1]; last.Type != EventApprovalRequested {
		t.Errorf("最后一条是 %q，应为 %q", last.Type, EventApprovalRequested)
	}
}

// TestApprovalEdgesCarryTheNamedApproval 把两条边的载荷逐键对回去：四键恒在场、值就是给的那个、
// 两条都以 execution 聚合入库，且放行那一格不许顶着 `execution_resumed` 的名字。
func TestApprovalEdgesCarryTheNamedApproval(t *testing.T) {
	ctx := context.Background()
	d := approvalEdgeDB(t)
	svc := NewService()
	blockedExecution(t, ctx, d, svc, "ex-payload", false)

	block := edgeCmd("ev-pay-requested")
	block.ApprovalRequested = ApprovalRequestedPayload{
		ApprovalID: "apr-9", Action: "command.exec", Resource: "", RiskLevel: "HIGH"}
	if _, err := edgeAdvance(ctx, d, svc, "ex-payload", WaitingApproval, block); err != nil {
		t.Fatal(err)
	}
	release := edgeCmd("ev-pay-resolved")
	release.ApprovalResolved = ApprovalResolvedPayload{
		ApprovalID: "apr-9", State: "APPROVED", GrantID: "", ResolvedBy: "local-user"}
	if _, err := edgeAdvance(ctx, d, svc, "ex-payload", Running, release); err != nil {
		t.Fatal(err)
	}

	j := edgeJournal(t, d, "ex-payload")
	if len(j) < 2 {
		t.Fatalf("至少该有请求与放行两条，实际 %d 条", len(j))
	}
	req, got := j[len(j)-2], j[len(j)-1]
	if req.Type != EventApprovalRequested || got.Type != EventApprovalResolved {
		t.Fatalf("两条边的名字 = %q/%q，应为 %q/%q",
			req.Type, got.Type, EventApprovalRequested, EventApprovalResolved)
	}
	for _, r := range []edgeRow{req, got} {
		if r.Aggregate != "execution" {
			t.Errorf("%s 的 aggregate_type = %q，REQ-APR-1 之后必须是 execution", r.Type, r.Aggregate)
		}
		// 放行不是续跑：`execution_resumed` 的载荷只有一个 `from`，用它记一次人工决定就是撒谎。
		if r.Type == EventResumed {
			t.Errorf("审批放行被记成了 %s", EventResumed)
		}
	}
	for _, c := range []struct {
		name  string
		raw   string
		want  map[string]string
		empty string
	}{
		{"requested", req.Payload, map[string]string{"approval_id": "apr-9", "action": "command.exec",
			"risk_level": "HIGH"}, "resource"},
		{"resolved", got.Payload, map[string]string{"approval_id": "apr-9", "state": "APPROVED",
			"resolved_by": "local-user"}, "grant_id"},
	} {
		m := edgePayload(t, c.raw)
		// 库里那一列比 §6 多一个键，而且**该**多：`schema` 由平台层在 appendEvent 里缺键时补
		// （`internal/platform/db/touch.go:577`–`:578`），不是本包塞的（`Payload()` 的输出里没有它，
		// 那一格由 events_test.go 的 TestPayloadKeysMatchCatalog 钉着）。所以这里数的是**业务键**四个。
		biz := map[string]any{}
		for k, v := range m {
			if k != "schema" {
				biz[k] = v
			}
		}
		if len(biz) != 4 {
			t.Errorf("%s 载荷 %d 个业务键 %v，§6 那两行各是四键", c.name, len(biz), edgeKeys(biz))
		}
		if _, ok := m["schema"]; !ok {
			t.Errorf("%s 载荷里没有平台补的 schema 那一键：%v", c.name, edgeKeys(m))
		}
		for k, want := range c.want {
			if s, _ := m[k].(string); s != want {
				t.Errorf("%s 载荷 %s = %v，应为 %q", c.name, k, m[k], want)
			}
		}
		// 可空那一键：键**必须在**、值是空串（"没有"）—— 键不在与值为空是两回事，读端不该两种都防。
		v, ok := m[c.empty]
		if !ok {
			t.Errorf("%s 载荷缺 %s 那一键（可空不等于可以省略）：%v", c.name, c.empty, edgeKeys(m))
		} else if s, _ := v.(string); s != "" {
			t.Errorf("%s 载荷 %s = %v，没人给的值不许编出来", c.name, c.empty, v)
		}
	}
}

// TestApprovalEdgesRefuseToNameNoApproval 钉两格的失败关闭：说不清"等谁批 / 谁批的"的事件
// 一条都不许落（落了就是一条指向不存在的审批的证据，比不写更坏）。每个子格都验"零写入"那一半，
// 只红错误不够 —— 半写状态是这类判据最容易漏的东西。
//
// 前置状态分两拨是**必须的**，不是排版：请求边的两格要从 RUNNING 起步，放行边的两格要先真的
// 停进 WAITING_APPROVAL。要是四格都从 RUNNING 起步，后两格会先撞 §5.2 矩阵（RUNNING→RUNNING 不合法）
// 而就"红"这件事与载荷校验混在一起 —— 那种红证明不了任何关于载荷的话。
func TestApprovalEdgesRefuseToNameNoApproval(t *testing.T) {
	ctx := context.Background()
	d := approvalEdgeDB(t)
	svc := NewService()
	blockedExecution(t, ctx, d, svc, "ex-refuse-block", false)
	blockedExecution(t, ctx, d, svc, "ex-refuse-gate", true)

	for _, c := range []struct {
		name   string
		id     string
		to     State
		wantAt State
		cmd    Command
	}{
		{
			name: "请求边说不出在等哪张审批", id: "ex-refuse-block",
			to: WaitingApproval, wantAt: Running,
			cmd: func() Command {
				cmd := edgeCmd("ev-refuse-no-id")
				cmd.ApprovalRequested = ApprovalRequestedPayload{Action: "db.write", RiskLevel: "HIGH"}
				return cmd
			}(),
		},
		{
			name: "请求边说不出被挡的是什么动作", id: "ex-refuse-block",
			to: WaitingApproval, wantAt: Running,
			cmd: func() Command {
				cmd := edgeCmd("ev-refuse-no-action")
				cmd.ApprovalRequested = ApprovalRequestedPayload{ApprovalID: "apr-1", RiskLevel: "HIGH"}
				return cmd
			}(),
		},
		{
			name: "放行边说不出决定是什么", id: "ex-refuse-gate",
			to: Running, wantAt: WaitingApproval,
			cmd: func() Command {
				cmd := edgeCmd("ev-refuse-no-state")
				cmd.ApprovalResolved = ApprovalResolvedPayload{ApprovalID: "apr-ex-refuse-gate", ResolvedBy: "local-user"}
				return cmd
			}(),
		},
		{
			name: "放行边说不出是谁批的", id: "ex-refuse-gate",
			to: Running, wantAt: WaitingApproval,
			cmd: func() Command {
				cmd := edgeCmd("ev-refuse-no-by")
				cmd.ApprovalResolved = ApprovalResolvedPayload{ApprovalID: "apr-ex-refuse-gate", State: "APPROVED"}
				return cmd
			}(),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			rowsBefore := len(edgeJournal(t, d, c.id))
			res, err := edgeAdvance(ctx, d, svc, c.id, c.to, c.cmd)
			if err == nil {
				t.Fatalf("这一格该整笔不写，实际推进成了 %+v", res)
			}
			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("该翻成有名字的 ErrIllegalTransition，实际 %v", err)
			}
			if res.Advanced {
				t.Errorf("被拒的一格不许声称 Advanced: %+v", res)
			}
			if status, _ := edgeStatus(t, d, c.id); status != string(c.wantAt) {
				t.Errorf("库里 status = %q，被拒必须一行不写（应停在 %q）", status, c.wantAt)
			}
			if n := len(edgeJournal(t, d, c.id)); n != rowsBefore {
				t.Errorf("事件 %d→%d 行，被拒的一格连事件都不该留", rowsBefore, n)
			}
		})
	}
}

// TestApprovalEdgesAreAuditRequiredEvents 钉这张表的两个新成员与 §6 那两列的 ✓ 对得上，
// 同时钉住"不是所有事件都要审计"那一半（只断 true 的话，把整张表改成恒真也能骗过去）。
func TestApprovalEdgesAreAuditRequiredEvents(t *testing.T) {
	for _, e := range []string{EventApprovalRequested, EventApprovalResolved} {
		if !RequiresAudit(e) {
			t.Errorf("%s 的 §6 Audit 列是 ✓，这张表却说不要审计", e)
		}
	}
	for _, e := range []string{EventOutputFlushed, EventClaimed, EventCreated, EventResumed, EventStarted} {
		if RequiresAudit(e) {
			t.Errorf("%s 的 §6 Audit 列不是 ✓，别顺手给它加待遇（claim 那一格被明写过不许补）", e)
		}
	}
}

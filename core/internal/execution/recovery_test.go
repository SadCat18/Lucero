package execution_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// factsFor 造一只按 adapter 分格的进程事实口：rows 里没有的那台 = "查不到任何行"（CO-44② 的输入）。
func factsFor(rows map[string][]execution.ProcessFact) execution.ProcessFacts {
	return func(_ context.Context, adapterInstanceID string) ([]execution.ProcessFact, error) {
		return rows[adapterInstanceID], nil
	}
}

// auditSink 造一只只记不写的 audit 口。判据要看"写进去了什么"，不是只看"没报错" ——
// fail 非 nil 时它照常先记再失败，好让回滚那条用例同时说得出"审计被叫过、且是它把整笔打回的"。
func auditSink(calls *[]execution.AuditRecord, fail error) execution.AuditFunc {
	return func(_ context.Context, _ *db.Tx, rec execution.AuditRecord) error {
		*calls = append(*calls, rec)
		return fail
	}
}

// deps 组一份依赖，Audit 可换（这是四件里唯一有行为要验的一件）。
func deps(rows map[string][]execution.ProcessFact, audit execution.AuditFunc) execution.RecoveryDeps {
	n := 0
	return execution.RecoveryDeps{
		Facts:       factsFor(rows),
		NewEventID:  func() (string, error) { n++; return fmt.Sprintf("ev-recovery-%d", n), nil },
		PrincipalID: "system",
		Audit:       audit,
	}
}

// wiredDeps 给一份齐的依赖（事件 id 用递增串，避免万一两条都走到 Touch 撞 UNIQUE）。
func wiredDeps(rows map[string][]execution.ProcessFact) execution.RecoveryDeps {
	return deps(rows, func(_ context.Context, _ *db.Tx, _ execution.AuditRecord) error { return nil })
}

// journalCount 数某聚合在 event_journal 里有几条：改判必须与事件成对（没事件的改判 = 假账）。
func journalCount(t *testing.T, d *db.DB, aggregateID string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_id = ?`, aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// journalRow 取该聚合最新那条事件的 id / 类型 / payload 原文（带证据 = 事件本身要能被读出来）。
func journalRow(t *testing.T, d *db.DB, aggregateID string) (eventID, evType, payload string) {
	t.Helper()
	err := d.Read.QueryRow(`SELECT event_id, event_type, payload_json FROM event_journal
		WHERE aggregate_id = ? ORDER BY seq DESC LIMIT 1`, aggregateID).
		Scan(&eventID, &evType, &payload)
	if err != nil {
		t.Fatalf("读 %s 最新那条事件: %v", aggregateID, err)
	}
	return eventID, evType, payload
}

// initRedactionForWrite 是写库用例自带的那份脱敏规则最小副本（与 service_test.go 的 seedWriteFixture 同形）：
// 平台层不初始化脱敏就拒绝写（fail closed，redact.go:222）。权威是 audit.Rules()，但业务包的测试
// 不 import 规则包（C14：audit 在途改动会把本包带红）。改判真落库之后这条才咬到本文件 ——
// 恒停在 §6 墙前的那段日子里 Touch 根本走不到。
func initRedactionForWrite(t *testing.T) {
	t.Helper()
	if err := db.InitRedaction([]string{"token"}, []string{`sk-[A-Za-z0-9_-]{16,}`}, "[REDACTED]"); err != nil {
		t.Fatalf("InitRedaction: %v", err)
	}
}

// seedSystemPrincipal 补 principals 那一行：event_journal.principal_id 有外键，而 seed() 只播
// 读面要看的那几行。改判恒停在 §6 墙前的那段日子里谁也摸不到这条外键 —— 改判真落库之后它就露出来了；
// 生产里这一行由 cmd/core 的 ensurePrincipal（main.go:242）保证。
func seedSystemPrincipal(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
		VALUES ('system', 'SYSTEM', 'Core', 1, 1)`); err != nil {
		t.Fatalf("补 principals: %v", err)
	}
}

// scanRun 在一笔写事务里跑一次扫描，把报告与错误都带出来（错误非 nil ⇒ 调用方 db.Write 已回滚）。
func scanRun(t *testing.T, d *db.DB, svc *execution.Service) (execution.RecoveryReport, error) {
	t.Helper()
	ctx := context.Background()
	var rep execution.RecoveryReport
	err := d.Write(ctx, func(tx *db.Tx) error {
		var e error
		rep, e = svc.ScanRecovery(ctx, tx)
		return e
	})
	return rep, err
}

// TestScanRecoveryRefusesWhenNotWired 钉"没接线就明确失败，一个候选都不碰"：
// 四件外部输入缺任一件都要走这一格，不许拿默认值（空主体 / 猜的身份 / 省掉那条 audit）凑合着往下读。
// 第五格是 T-391 第二段新加的：audit 缺了 ⇒ 改判会带着"§6 要的证据没有"落库，那比不改判更坏。
func TestScanRecoveryRefusesWhenNotWired(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	ok := wiredDeps(nil)
	cases := []struct {
		name string
		deps execution.RecoveryDeps
	}{
		{"什么都没接", execution.RecoveryDeps{}},
		{"缺进程事实", execution.RecoveryDeps{NewEventID: ok.NewEventID, PrincipalID: "system", Audit: ok.Audit}},
		{"缺事件身份", execution.RecoveryDeps{Facts: ok.Facts, PrincipalID: "system", Audit: ok.Audit}},
		{"缺改判主体", execution.RecoveryDeps{Facts: ok.Facts, NewEventID: ok.NewEventID, Audit: ok.Audit}},
		{"缺同事务 audit", execution.RecoveryDeps{Facts: ok.Facts, NewEventID: ok.NewEventID, PrincipalID: "system"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := execution.NewService()
			svc.EnableRecovery(tc.deps)
			rep, err := scanRun(t, d, svc)
			if !errors.Is(err, execution.ErrRecoveryNotWired) {
				t.Fatalf("应是 ErrRecoveryNotWired，实际 %v", err)
			}
			if len(rep.Candidates) != 0 || len(rep.Unattributed) != 0 || len(rep.Blocked) != 0 {
				t.Errorf("没接线时连候选都不该读出来，报告却有内容: %+v", rep)
			}
		})
	}
}

// TestScanRecoveryWithoutProcessRowsChangesNothing 钉证据闸门的那一面：
// 查不到该台的进程行 ⇒ 一条都不改判（CO-44② 的"0 行是 0 行"，且不许静默 —— 报告要把它们列出来）。
// 夹具（seed）里两台 adapter 都没有进程行，所以这里也是"夹具不受扫描影响"那条前提的正身；
// 顺带钉"闸门先于写"：连 audit 都不该被叫到（叫到了就是给一条没落地的改判记了账）。
func TestScanRecoveryWithoutProcessRowsChangesNothing(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	var calls []execution.AuditRecord
	svc := execution.NewService()
	svc.EnableRecovery(deps(nil, auditSink(&calls, nil)))

	rep, err := scanRun(t, d, svc)
	if err != nil {
		t.Fatalf("查不到进程行不该算失败（那是「什么都不猜」的正常出口）: %v", err)
	}
	if len(rep.Candidates) != 2 {
		t.Errorf("五态候选应有 ex-2/ex-3 两条，实际 %v", rep.Candidates)
	}
	if len(rep.Unattributed) != 2 || len(rep.Marked) != 0 || len(rep.Blocked) != 0 {
		t.Errorf("两条都该落「查不到」那一格，实际 %+v", rep)
	}
	if len(calls) != 0 {
		t.Errorf("闸门挡下的候选不许记 audit 行（那是给没落地的改判记账）: %+v", calls)
	}
	got, err := execution.NewRepository(d.Read).Get(context.Background(), "ex-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != execution.Starting || got.Version != 1 {
		t.Errorf("行被改了: status=%s v%d（期望 STARTING v1）", got.Status, got.Version)
	}
	if n := journalCount(t, d, "ex-2"); n != 0 {
		t.Errorf("一条事件都不该多（没有改判）: %d", n)
	}
}

// TestScanRecoveryMarksInterruptedWithEvidence 是 T-391 第二段的正面判据（09-29 主控裁「分」之后）：
// Core 重启那一支 = RUNNING→INTERRUPTED（FIRST SPRINT §7 interrupted / recoverable），
// 一次扫描里四件事同时成立 —— 缺一条这条用例就红：
//
//  1. 改判真落库（ex-2 STARTING→INTERRUPTED、版本 +1），且**不是** FAILED（§7/CO-8：进程死不是失败的证据）；
//  2. 同事务带一条 §6 事件 execution_interrupted，payload 是 §6 那三个键；
//  3. 同事务带那条 audit 记录（§6 Audit 列 ✓）：id 与事件同源、理由原文带得出进程行；
//  4. 没有进程行的那台一格都不碰（ex-3 仍是 WAITING_INPUT，Unattributed）。
//
// 反例落点：把 markInterrupted 里 RequiresAudit 那一段摘掉 ⇒ 第 3 条红（改判有、证据没有）。
func TestScanRecoveryMarksInterruptedWithEvidence(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	initRedactionForWrite(t)
	seedSystemPrincipal(t, d)
	var calls []execution.AuditRecord
	svc := execution.NewService()
	// ad-qoder（ex-2，STARTING）有一行进程事实；ad-codex（ex-3）故意不给 ⇒ 两条各落一格。
	svc.EnableRecovery(deps(map[string][]execution.ProcessFact{
		"ad-qoder": {{ID: "proc-1", PID: 4242, Status: "RUNNING", Attempt: 2}},
	}, auditSink(&calls, nil)))

	rep, err := scanRun(t, d, svc)
	if err != nil {
		t.Fatalf("证据齐、事件现成 ⇒ 这一支应当落下去（09-29 裁：Core 重启 = INTERRUPTED）: %v", err)
	}
	if len(rep.Marked) != 1 || rep.Marked[0] != "ex-2" {
		t.Errorf("ex-2 该进 Marked，实际 %+v", rep)
	}
	if len(rep.Blocked) != 0 {
		t.Errorf("没有受阻项，实际 %+v", rep.Blocked)
	}
	wantUnattributed := map[string]bool{"ex-3": true}
	if len(rep.Unattributed) != 1 || !wantUnattributed[rep.Unattributed[0]] {
		t.Errorf("ex-3 该落「查不到进程行」那一格，实际 %v", rep.Unattributed)
	}

	got, err := execution.NewRepository(d.Read).Get(context.Background(), "ex-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != execution.Interrupted || got.Version != 2 {
		t.Errorf("改判没落库（或落错了）: status=%s v%d，应为 INTERRUPTED v2", got.Status, got.Version)
	}
	if got.Status == execution.Failed {
		t.Errorf("进程死不是失败的证据（§7/CO-8），落成 FAILED 就是把崩溃当结局")
	}

	if n := journalCount(t, d, "ex-2"); n != 1 {
		t.Fatalf("改判必须恰有一条事件（多了是假账，少了是无证据的改判），实际 %d", n)
	}
	eventID, evType, payload := journalRow(t, d, "ex-2")
	if evType != execution.EventInterrupted {
		t.Errorf("事件类型是 %q，§6 该是 %q（EventOf 说了算，不许自己拼一个）", evType, execution.EventInterrupted)
	}
	for _, key := range []string{"result_code", "error_code", "usage"} {
		if !strings.Contains(payload, `"`+key+`"`) {
			t.Errorf("payload 缺 §6 的键 %q: %s", key, payload)
		}
	}

	if len(calls) != 1 {
		t.Fatalf("§6 的 Audit 列 ✓ ⇒ 恰一条同事务 audit 记录，实际 %d 条: %+v", len(calls), calls)
	}
	rec := calls[0]
	if rec.EventID != eventID {
		t.Errorf("audit 记录指的事件 %q 与 event_journal 里那条 %q 不是同一条", rec.EventID, eventID)
	}
	if rec.ExecutionID != "ex-2" || rec.TaskID != "task-1" || rec.PrincipalID != "system" {
		t.Errorf("audit 记录的归属写错: %+v", rec)
	}
	if !strings.Contains(rec.Reason, "ad-qoder") || !strings.Contains(rec.Reason, "pid=4242") {
		t.Errorf("Reason 必须说得出依据（哪台、哪一行进程）: %q", rec.Reason)
	}
	if !strings.Contains(rec.Reason, "interrupted/recoverable") {
		t.Errorf("词按 09-29 裁的「分」走：这一支是 interrupted/recoverable，不是\"不可判定\": %q", rec.Reason)
	}

	// 另一台一个字都不许动（按台分格 + 闸门）。
	other, err := execution.NewRepository(d.Read).Get(context.Background(), "ex-3")
	if err != nil {
		t.Fatal(err)
	}
	if other.Status != execution.WaitingInput || other.Version != 1 {
		t.Errorf("ex-3 被动了: status=%s v%d", other.Status, other.Version)
	}
}

// TestScanRecoveryRollsBackWhenAuditFails 钉"任一失败整笔回滚、零事件新增"这句在 audit 这一步同样成立：
// §6 要的那条 audit 行写不出去 ⇒ 改判一起不许留（只改状态不记账 = 半真历史，比不改更坏）。
// 报告要把真原因带出事务（Blocked），调用方日志据此说得出卡在哪。
func TestScanRecoveryRollsBackWhenAuditFails(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	initRedactionForWrite(t)
	seedSystemPrincipal(t, d)
	var calls []execution.AuditRecord
	svc := execution.NewService()
	boom := errors.New("audit 链头读不出来")
	svc.EnableRecovery(deps(map[string][]execution.ProcessFact{
		"ad-qoder": {{ID: "proc-1", PID: 4242, Status: "RUNNING", Attempt: 1}},
	}, auditSink(&calls, boom)))

	rep, err := scanRun(t, d, svc)
	if !errors.Is(err, boom) {
		t.Fatalf("audit 失败必须上抛（不许静默当成功），实际 %v", err)
	}
	if len(rep.Marked) != 0 {
		t.Errorf("Marked 只在 err == nil 时可信，这次 err != nil 却记了 %v", rep.Marked)
	}
	if len(rep.Blocked) != 1 || rep.Blocked[0].ID != "ex-2" || !errors.Is(rep.Blocked[0].Err, boom) {
		t.Errorf("ex-2 该带着真原因进 Blocked，实际 %+v", rep.Blocked)
	}
	if len(calls) == 0 {
		t.Error("失败发生前 audit 口应当被叫到过（不然红的不是 audit 这一步）")
	}
	got, err := execution.NewRepository(d.Read).Get(context.Background(), "ex-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != execution.Starting || got.Version != 1 {
		t.Errorf("失败的扫描不许留下改动: status=%s v%d", got.Status, got.Version)
	}
	if n := journalCount(t, d, "ex-2"); n != 0 {
		t.Errorf("没落地的改判不许留事件: %d", n)
	}
}

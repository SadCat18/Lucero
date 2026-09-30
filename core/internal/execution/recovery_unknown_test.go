package execution_test

// REQ-RC-2「Adapter 崩溃那一支」的落点判据（T-401② 给的那一半：常量 + 落点 + 用例；
// 「谁喊它」那一腿按 :716 与 T-401③ 仍等 adapter 的导出面 = T-398①，本文件不造触发条件）。
//
// 分层是 09-30 主控裁的「丙」（契约① 第 25 条 ④-a :710~723）：`UNKNOWN` 归 execution 层的轮次结果，
// `INTERRUPTED` 归 task 投影层的态 ⇒ 本文件两条线一起钉：崩溃那一支只写 execution、
// 而 boot 扫描那一支**不许**改成 UNKNOWN（最后那条用例就是这一格的反面）。
//
// §6 那一行不在本批里由我加（:718~719：先登 §6 会撞 C8-12 双向对拍；登记方式 = 我这边常量与落点、
// 主控那边同批补 §6 行 + db 补 eventjournal 目录，三者进同一个提交）。
// 所以这里断的是**代码侧形状**（事件名同源、落库成套、邻居不混、非终态不写 finished_at），
// 而"§6 与目录集合相等"那一枪归 `core/internal/eventjournal/contract_parity_test.go`，我不复刻一份表。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// unknownSeedRow 补 `seed()` 里没有的那一形：一条 RUNNING 的在飞执行（崩溃那一支的输入）。
// 不复用 ex-3（WAITING_INPUT）是因为我要断的正是"从 RUNNING 改判"这一条边的列写形状。
func unknownSeedRow(t *testing.T, d *db.DB, id, from string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, 'task-1', 'ad-codex', 9, ?, 'HIGH', 'INITIAL', 1, 1)`, id, from); err != nil {
		t.Fatalf("播 %s（%s）: %v", id, from, err)
	}
}

func execRowWide(t *testing.T, d *db.DB, id string) (status string, finished int64, resultCode string) {
	t.Helper()
	err := d.Read.QueryRow(`SELECT status, COALESCE(finished_at, -1), COALESCE(result_code, '')
		FROM executions WHERE id = ?`, id).Scan(&status, &finished, &resultCode)
	if err != nil {
		t.Fatalf("读 %s 的宽行: %v", id, err)
	}
	return status, finished, resultCode
}

func eventsOfType(t *testing.T, d *db.DB, aggregateID, evType string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE aggregate_id = ? AND event_type = ?`,
		aggregateID, evType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestEventOfUnknownIsNamedByTheStateConstant 是 T-399③ 要的那条"能区分名字对/错"的主判据。
//
// 期望值**从状态常量算出来**（`"execution_" + lower(Unknown)`），不是把 §6 的字面抄第二遍：
// 抄表就是本仓记忆里那条"validator 不得内嵌契约快照"的反面 —— 抄来的那份会跟着表一起错。
// 于是三件事各自能被分开判：
//   - 名字对（execution_unknown）⇒ 绿；
//   - 换成 execution_crashed / execution_recovered / 任何新造词 ⇒ 红（常量与状态常量分叉）；
//   - 复用邻居 `execution_interrupted` ⇒ 由本条第二腿与下一条用例一起挡（"同形"被读成"同一个事件"是最省事的错法）。
//
// 这条**证不到**的：这名字该不该叫这个 —— 那是语义裁定，出处在契约① :717，不在这里。
func TestEventOfUnknownIsNamedByTheStateConstant(t *testing.T) {
	want := "execution_" + strings.ToLower(string(execution.Unknown))
	got := execution.EventOf(execution.Running, execution.Unknown)
	if got != want {
		t.Errorf("→UNKNOWN 的事件名该由状态常量算出来是 %q，实际 %q（对不上就是改名/另造，两边都得有人改）", want, got)
	}
	if got != execution.EventUnknown {
		t.Errorf("本包常量 EventUnknown=%q 与推导值 %q 分叉 ⇒ 有一处是手写死的", execution.EventUnknown, got)
	}
	// 邻居不许同名：两个场景两条边（09-29 的「分」+ 09-30 的「丙」），一个事件名就意味着一边没落点。
	if other := execution.EventOf(execution.Running, execution.Interrupted); other == got {
		t.Errorf("INTERRUPTED 与 UNKNOWN 拿到同一个事件名 %q ⇒ 两条边被并成一条", other)
	}
	// Audit 必带（:717 那句）也要跟着名字走，别让"名字对了、审计没有"。
	if !execution.RequiresAudit(got) {
		t.Errorf("%s 按 §6 是 Audit 列 ✓ 的那一行，auditRequired 里却没有它", got)
	}
}

// catalogueWall 认那道**平台事件目录**的闸：`db.touch.go:564` 在 appendEvent 第一句问
// `eventjournal.Registered(ev.Type)`，而目录住在 `internal/eventjournal/events.go`（`catalog` 那张表）——
// **那是 db 角色的面包，不在本账 SCOPE**。所以"我这半已接、别人那半未落"今天是真的会撞墙，
// 而撞墙的正确表现是：**明确失败 + 一行都不写**（不是静默、不是绕过目录自己 INSERT）。
//
// 三件同批（REQ-DB-4 顺序，契约① :719 那句「backend 加常量 + 主控批 §6 行」的**第三件是 db 的目录**，
// 见台账第 304 条）：① 我这半（已完成）、② db 的 catalog 条目、③ 主控的 §6 行 + yaml 枚举。
// ② ③ 落了之后本文件那两条"完整形状"用例自动开跑（探针只认那一句话，不认开关）。
func catalogueWall(t *testing.T, err error) bool {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "is not in the event journal catalogue") {
		return false
	}
	t.Skipf("等 external 两半：`internal/eventjournal/events.go` 的 catalog 缺 execution_unknown（db 的面包）"+
		"+ §6 那一行与 yaml 枚举（主控的面包）⇒ 落点今天按平台规矩明确失败。"+
		"这一格的完整形状由本用例断，来源：契约① 第 25 条 ④-a :717~719。实际错误：%v", err)
	return true
}

// TestMarkCrashUnknownLandsUnjudgedStateWithAudit 是那一笔写的全套形状：
// 状态改判 + 恰一条事件（payload 是 `from` + `reason`）+ 同事务恰一条 audit +
// **非终态 ⇒ finished_at/result_code 都不许写**（"不可判定"与"已失败"在列上的唯一区别）+
// **task 一侧一行都不动**（:720~723：`execution_unknown` 不进 §5.3 七条边）。
func TestMarkCrashUnknownLandsUnjudgedStateWithAudit(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	unknownSeedRow(t, d, "ex-crash", "RUNNING")
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)

	rows := map[string][]execution.ProcessFact{
		"ad-codex": {{ID: "proc-9", PID: 4242, Status: "EXITED", Attempt: 2}},
	}
	var calls []execution.AuditRecord
	svc := execution.NewService()
	svc.EnableRecovery(deps(rows, auditSink(&calls, nil)))
	taskBefore := taskStatus(t, d, "task-1")

	ctx := context.Background()
	var res execution.Result
	err := d.Write(ctx, func(tx *db.Tx) error {
		var e error
		res, e = svc.MarkCrashUnknown(ctx, tx, "ex-crash", rows["ad-codex"])
		return e
	})
	if catalogueWall(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("崩溃那一支的改判失败：%v", err)
	}
	if res.To != execution.Unknown || !res.Advanced {
		t.Errorf("返回的结局该是「推上 UNKNOWN」，实际 %+v", res)
	}

	status, finished, resultCode := execRowWide(t, d, "ex-crash")
	if status != "UNKNOWN" {
		t.Errorf("status 该是 UNKNOWN，实际 %s", status)
	}
	if finished != -1 || resultCode != "" {
		t.Errorf("UNKNOWN 是非终态 ⇒ finished_at/result_code 都不该有值，实际 finished=%d result_code=%q",
			finished, resultCode)
	}

	evID, evType, payload := journalRow(t, d, "ex-crash")
	if evType != "execution_unknown" {
		t.Errorf("事件名该是 execution_unknown，实际 %q", evType)
	}
	if n := eventsOfType(t, d, "ex-crash", "execution_interrupted"); n != 0 {
		t.Errorf("崩溃那一支不许发 execution_interrupted（两条边两个场景），实际 %d 行", n)
	}
	// payload 两个键按契约 :717 点名：from + reason。键名逐字对，reason 里要能看见依据原文。
	for _, must := range []string{`"from":"RUNNING"`, `"reason"`, "pid=4242", "attempt=2"} {
		if !strings.Contains(payload, must) {
			t.Errorf("payload 里缺 %s，原文 %q", must, payload)
		}
	}
	// 词也是判据：这一支是"不可判定"，不许被写成失败或被写成可恢复（协议 §8 / FIRST SPRINT §7 两个词各归一支）。
	for _, ban := range []string{"FAILED", "可恢复"} {
		if strings.Contains(payload, ban) {
			t.Errorf("payload 的 reason 里出现了 %q —— 那一支的词不归这一支用: %q", ban, payload)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("§6 的 Audit 列 ✓ ⇒ 同事务恰一条，实际 %d 条: %+v", len(calls), calls)
	}
	if calls[0].EventID != evID || calls[0].ExecutionID != "ex-crash" || calls[0].PrincipalID != "system" {
		t.Errorf("audit 行指不回那次改判：%+v（事件 id 应为 %s）", calls[0], evID)
	}

	// task 一侧：既没被投影、也没被"顺手补一条"（§5.3 七条边里没有 execution_unknown）。
	for _, typ := range []string{"task_agent_completed", "task_waiting_acceptance", "task_failed", "task_interrupted"} {
		if n := eventsOfType(t, d, "task-1", typ); n != 0 {
			t.Errorf("崩溃那一支不许驱动 task 投影，%s 却有 %d 行", typ, n)
		}
	}
	if st := taskStatus(t, d, "task-1"); st != taskBefore {
		t.Errorf("task 态不许被 execution 的 UNKNOWN 带跑：改判前 %s、之后 %s", taskBefore, st)
	}
}

// taskStatus 住在 finish_test.go（同包），本文件直接复用，不再定义第二份。

// TestMarkCrashUnknownRefusesWithoutWritingAnything 把四道"不该写"的闸一起钉：
// 每格都要求**状态没动、事件零条、audit 一次没被叫**（叫了就是给一条没落地的改判记账）。
func TestMarkCrashUnknownRefusesWithoutWritingAnything(t *testing.T) {
	cases := []struct {
		name  string
		from  string // 空串 = 用 ex-1（SUCCEEDED，已定局）
		id    string
		rows  map[string][]execution.ProcessFact
		wired bool
		want  error
	}{
		{"已定局的行不许被改判成 UNKNOWN", "SUCCEEDED", "ex-1", nil, true, execution.ErrIllegalTransition},
		{"没有进程行就没有主体可归属", "", "ex-crash", map[string][]execution.ProcessFact{}, true, execution.ErrRecoveryNotWired},
		{"缺事件身份", "", "ex-crash", nil, false, execution.ErrRecoveryNotWired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := openDB(t)
			seed(t, d)
			unknownSeedRow(t, d, "ex-crash", "RUNNING")
			seedSystemPrincipal(t, d)
			initRedactionForWrite(t)
			if tc.from == "SUCCEEDED" {
				// ex-1 已在 seed 里是 SUCCEEDED；这一格拿它当"已定局"的样本。
				tc.id = "ex-1"
			}
			rows := tc.rows
			if rows == nil && tc.wired {
				rows = map[string][]execution.ProcessFact{
					"ad-codex": {{ID: "proc-9", PID: 1, Status: "EXITED"}},
				}
			}
			var calls []execution.AuditRecord
			in := deps(rows, auditSink(&calls, nil))
			if !tc.wired {
				in.NewEventID = nil // 缺事件身份：宁可明确失败，不拿猜的 id 往下写
			}
			svc := execution.NewService()
			svc.EnableRecovery(in)

			ctx := context.Background()
			err := d.Write(ctx, func(tx *db.Tx) error {
				_, e := svc.MarkCrashUnknown(ctx, tx, tc.id, rows["ad-codex"])
				return e
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("应是 %v，实际 %v", tc.want, err)
			}
			status, _, _ := execRowWide(t, d, tc.id)
			wantStatus := "RUNNING"
			if tc.want == execution.ErrIllegalTransition && tc.id == "ex-1" {
				wantStatus = "SUCCEEDED"
			}
			if status != wantStatus {
				t.Errorf("被拒的一格不许动状态：%s 实际 %s（期望 %s）", tc.id, status, wantStatus)
			}
			if n := journalCount(t, d, tc.id); n != 0 {
				t.Errorf("被拒的一格零事件，实际 %d 条", n)
			}
			if len(calls) != 0 {
				t.Errorf("闸门挡下的改判不许记 audit：%+v", calls)
			}
		})
	}
}

// TestMarkCrashUnknownRollsBackWhenAuditFails 与扫描那条同形：审计落不下去 ⇒ 整笔回滚。
// 这一格防的是"先把状态改了、审计失败就当没看见"——那正是 §6 要求同事务的理由。
func TestMarkCrashUnknownRollsBackWhenAuditFails(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	unknownSeedRow(t, d, "ex-crash", "RUNNING")
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)

	rows := map[string][]execution.ProcessFact{"ad-codex": {{ID: "proc-9", PID: 7, Status: "EXITED"}}}
	var calls []execution.AuditRecord
	svc := execution.NewService()
	svc.EnableRecovery(deps(rows, auditSink(&calls, errors.New("链写不进去"))))

	ctx := context.Background()
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.MarkCrashUnknown(ctx, tx, "ex-crash", rows["ad-codex"])
		return e
	})
	if catalogueWall(t, err) {
		return
	}
	if err == nil {
		t.Fatal("audit 失败该把整笔打回")
	}
	if status, _, _ := execRowWide(t, d, "ex-crash"); status != "RUNNING" {
		t.Errorf("回滚后状态该还是 RUNNING，实际 %s", status)
	}
	if n := journalCount(t, d, "ex-crash"); n != 0 {
		t.Errorf("回滚后零事件，实际 %d 条", n)
	}
	if len(calls) != 1 {
		t.Errorf("审计确实被叫过一次（是被它打回的，不是没走到）：%d 次", len(calls))
	}
}

// TestScanRecoveryNeverMarksUnknown 是分层的另一侧：**boot 扫描那一支不许顺手改判成 UNKNOWN**。
// 它是 T-401③ 那句"别为了提前变绿去手搜一条绕过 Run 的路"的机器形状 —— 今天唯一接线的入口是启动扫描，
// 如果它开始产出 UNKNOWN，就等于把"Core 活着、对端死"那一支的结论借到"Core 自己重启"的场景上，
// 而 ④-a 的裁定恰恰是两支两个态（09-29 的「分」+ 09-30 的「丙」）。
// 这一格在触发腿落地之后仍然成立：那一支该有自己的调用点，不该长在这里。
func TestScanRecoveryNeverMarksUnknown(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	unknownSeedRow(t, d, "ex-run", "RUNNING")
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)

	rows := map[string][]execution.ProcessFact{
		"ad-codex": {{ID: "proc-9", PID: 11, Status: "EXITED"}},
		"ad-qoder": {{ID: "proc-8", PID: 12, Status: "RUNNING"}},
	}
	var calls []execution.AuditRecord
	svc := execution.NewService()
	svc.EnableRecovery(deps(rows, auditSink(&calls, nil)))

	rep, err := scanRun(t, d, svc)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(rep.Marked) == 0 {
		t.Fatal("这一格的前提是扫描真改了判（都没改就谈不到「改错了态」）")
	}
	for _, id := range rep.Marked {
		if st, _, _ := execRowWide(t, d, id); st == "UNKNOWN" {
			t.Errorf("boot 扫描把 %s 改成了 UNKNOWN ⇒ 那一支的结论被借到这一支来了（正身应是 INTERRUPTED）", id)
		}
	}
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = 'execution_unknown'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("execution_unknown 只许由崩溃那一支发，扫描这一路却产了 %d 条", n)
	}
}

// TestMarkCrashUnknownStopsAtTheJournalCatalogue 是**今天就能绿**的那一条：落点写不下去的时候，
// 必须明确失败、且一行都不写 —— 这既是"闸门先于写"的正面证据，也挡住三种省事的坏修法：
//   - 绕过目录自己 INSERT event_journal（§9.1 P0：写口只有一个）；
//   - 把事件名换成目录里现成的邻居（比如 execution_interrupted）去"先变绿"—— 那正是 ④-a 两支并一边的错法；
//   - 失败之后回退成只改状态不发事件（那会留下"改判有、证据没有"的半真历史）。
//
// 这一格**不是**在替 REQ-RC-2 交账：它断的是"没登记之前不许写"，登记之后它同样成立
// （那时 Touch 不再返回这句话，本用例走第二条支：断改判真落了）。
func TestMarkCrashUnknownStopsAtTheJournalCatalogue(t *testing.T) {
	d := openDB(t)
	seed(t, d)
	unknownSeedRow(t, d, "ex-wall", "RUNNING")
	seedSystemPrincipal(t, d)
	initRedactionForWrite(t)

	rows := map[string][]execution.ProcessFact{"ad-codex": {{ID: "proc-w", PID: 99, Status: "EXITED"}}}
	var calls []execution.AuditRecord
	svc := execution.NewService()
	svc.EnableRecovery(deps(rows, auditSink(&calls, nil)))

	ctx := context.Background()
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.MarkCrashUnknown(ctx, tx, "ex-wall", rows["ad-codex"])
		return e
	})
	if err == nil {
		// 目录已登记（db 那半落了）⇒ 这一格的正身改成"改判真落了"，别再等这句话。
		if st, _, _ := execRowWide(t, d, "ex-wall"); st != "UNKNOWN" {
			t.Fatalf("Touch 成功却什么都没改：status=%s（目录已登记 ⇒ 请把本用例改成断完整形状）", st)
		}
		t.Skipf("事件目录已含 execution_unknown ⇒ 本用例的墙没了；完整形状由 TestMarkCrashUnknownLandsUnjudgedStateWithAudit 断")
		return
	}
	if !strings.Contains(err.Error(), "is not in the event journal catalogue") {
		t.Fatalf("今天这道墙应是平台事件目录那句拒绝，实际：%v", err)
	}
	if st, _, _ := execRowWide(t, d, "ex-wall"); st != "RUNNING" {
		t.Errorf("明确失败之外不许有任何副作用：状态被改成 %s", st)
	}
	if n := journalCount(t, d, "ex-wall"); n != 0 {
		t.Errorf("被拒的改判零事件，实际 %d 条", n)
	}
	if len(calls) != 0 {
		t.Errorf("被拒的改判不许记 audit：%+v", calls)
	}
	// 而且名字必须是 execution_unknown —— 撞墙的那句话里点到的是它，不是被顺手换成的邻居名。
	if !strings.Contains(err.Error(), `execution_unknown`) {
		t.Errorf("拒绝原因里点的名应是 execution_unknown（说明事件名走的是 EventOf，不是硬凑的邻居）：%v", err)
	}
	if n := eventsOfType(t, d, "ex-wall", "execution_interrupted"); n != 0 {
		t.Errorf("不许为了变绿去发现成的邻居事件，execution_interrupted %d 行", n)
	}
}

package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// ErrRecoveryNotWired 是恢复扫描的四件外部输入缺了任何一件：宁可明确失败，
// 也不拿"我猜不到"当成"没有可归因的主体" —— 那正是 CO-44 ① 否决的那条路（把一次观察变成猜测）。
var ErrRecoveryNotWired = errors.New("execution: recovery scan is not wired (EnableRecovery 未调用或缺字段)")

// ProcessFact 是装配层从 process_instances 取回来的"这台 adapter 上登记过哪些进程行"。
//
// 这张表归 internal/process（契约③ §7），而 §0 禁止业务包之间跨包 SQL ⇒ 本包**不许**自己去 SELECT 它：
// 事实由 cmd/core 查好交进来（依赖靠注入，不靠 import —— §0 末句）。本包只消费事实、不认得那张表。
type ProcessFact struct {
	ID      string // process_instances.id：Reason 必须能指回这一行
	PID     int64
	Status  string // 行自己那个 status（STARTING/…/EXITED），原样搬运，不折算
	Attempt int    // CO-45② 记在 metadata_json 里的 attempt 序号；读不到 = 0（不猜）
}

// ProcessFacts 按 adapter_instance_id 交出**该台登记过的全部进程行**（含已退出的，按登记顺序）。
// 交不出任何行 = "没有主体可归属" ⇒ 调用方必须按 CO-44② 出声，且一行都不许改判。
type ProcessFacts func(ctx context.Context, adapterInstanceID string) ([]ProcessFact, error)

// RecoveryDeps 是恢复扫描的四件外部输入，全走 consumer-side 注入（与 EnableSend/EnableTurn 同一族）：
//
//   - Facts：见上（跨包 SQL 禁令的落点）；
//   - NewEventID：§3 要 UUIDv7、域不自己生成身份（重试要复用同一批 ⇒ 只能由调用方铸）；
//   - PrincipalID：这次改判记在谁名下。boot 扫描 = "system"（Core 自己发起的观察，不是某个人），
//     与 cmd/core 写 core_ready 时那个主体同源；
//   - Audit：§6 的 Audit 列 ✓ 的那一行（execution_interrupted 必带）。谁写也是装配层 ——
//     C14：业务包不 import audit，§0：业务包不写别人的表 ⇒ 链、脱敏、归因那套全住在 cmd/core。
//
// 四件缺一 ⇒ ScanRecovery 返回 ErrRecoveryNotWired，一个候选都不碰。
type RecoveryDeps struct {
	Facts       ProcessFacts
	NewEventID  func() (string, error)
	PrincipalID string
	Audit       AuditFunc
}

// AuditRecord 是同事务那条 audit_entries 的全部输入：为什么改判（Reason 原文）、改的是哪一条
// （ExecutionID / TaskID）、事件身份（EventID —— audit_id 从它派生，与 §5.4 同形）、记在谁名下。
type AuditRecord struct {
	ExecutionID string
	TaskID      string
	EventID     string
	PrincipalID string
	Reason      string
}

// AuditFunc 落 §6 里 Audit 列 ✓ 的那一行，必须与改判同一笔事务（不是事后补记：
// 事后补记在崩溃之后会变成"改判有、证据没有"）。
type AuditFunc func(ctx context.Context, tx *db.Tx, rec AuditRecord) error

// RecoveryReport 是一次恢复扫描的可读结果；调用方（cmd/core）必须把它整个喊进日志 ——
// 这份结构存在的理由是 CO-44②：静默改判与没改判在事后必须可分。
type RecoveryReport struct {
	Candidates   []string  // Crash Recovery Query（五态）读出来的 id
	Unattributed []string  // process_instances 里查不到该台的行 ⇒ 不改判，只喊
	Marked       []string  // 真推上 INTERRUPTED 边的 id；**只在返回 err == nil 时可信**（出错整笔回滚）
	Blocked      []Blocked // 有主体可归属、但这条边落不下去的（今天只剩 Check/事件身份/audit 三处会挡）
}

// Blocked 把"有证据却改判不了"的真原因带出事务：日志不许只说一句"失败"。
type Blocked struct {
	ID  string
	Err error
}

// EnableRecovery 接上恢复扫描的四件外部输入。装配方（cmd/core / 测试 harness）在构造后调一次。
func (s *Service) EnableRecovery(d RecoveryDeps) { s.recovery = d }

// ScanRecovery 是 REQ-RC-1 要的那个"入口"：恢复扫描器成为 Occasion=Recovery 在生产码里的调用者
// （契约① :700~703 立项时它一个生产调用点都没有）。两遍走：
//
//  1. 读：Crash Recovery Query 取五态候选（repository.go，只读），逐条取该台的进程事实；
//     取不到行 ⇒ 记进 Unattributed，不改判（CO-44②：0 行不许静默，也不许拿猜的补上）；
//  2. 写：有事实的才推 INTERRUPTED —— Evidence.Reason 说得出依据 → Check（Occasion=Recovery）→
//     EventOf 取 §6 事件 → payload → Touch → **同事务**那条 audit（§6 的 Audit 列 ✓）。
//
// 改判正身按 09-29 主控裁的「分」（契约① :706~723，"两者不是二选一，是两条边、两个场景"）：
//
//   - **Core 重启那一支（本函数今天唯一的场景，启动扫描）⇒ RUNNING→INTERRUPTED**：FIRST SPRINT §7
//     原话就是"重启 Core 后正确识别 interrupted / recoverable"，事件 execution_interrupted §6 现成、
//     auditRequired 已含 ⇒ 撞不到 §6 那堵墙；
//   - **Adapter 崩溃那一支（Core 活着、对端进程死）⇒ →UNKNOWN，本函数不喊它**：触发点是 Supervisor
//     观察到子进程死亡，依赖 W2-7 点名的 Supervisor.Run 那一格（生产码零调用点；导出面已派 adapter，
//     T-398①）。T-401 落的"登记授权"只到**常量 + 落点 + 用例**那一半（`MarkCrashUnknown`），
//     §6 那一行由主控在我交账后同批补（:718~719：先登 §6 会撞 C8-12 双向对拍）；
//     而"谁喊它"这一腿仍等 adapter 交账 —— 本函数不为那一支加半个口子，也不许顺手把 UNKNOWN
//     塞进 boot 扫描的结果里（那等于绕过 Run 手搜一条触发路径，T-401③ 明禁）。
//
// 调用方拿到非 nil err 时整笔回滚 ⇒ 报告里的 Marked 只在 err == nil 时可信；报告本身不受回滚影响，
// 是为了让日志照样说得出"读到了什么、卡在哪一步"。
func (s *Service) ScanRecovery(ctx context.Context, tx *db.Tx) (RecoveryReport, error) {
	var rep RecoveryReport
	if s.recovery.Facts == nil || s.recovery.NewEventID == nil || s.recovery.PrincipalID == "" || s.recovery.Audit == nil {
		return rep, fmt.Errorf("execution: 恢复扫描没装配齐（Facts/NewEventID/PrincipalID/Audit）: %w", ErrRecoveryNotWired)
	}
	cands, err := NewRepository(tx).ListCrashRecoveryCandidates(ctx)
	if err != nil {
		return rep, err
	}
	// 第一遍只读：把"有没有主体可归属"问清楚，写之前先有整份名单（日志要说全）。
	type attributed struct {
		e     Execution
		facts []ProcessFact
	}
	var ok []attributed
	for _, e := range cands {
		rep.Candidates = append(rep.Candidates, e.ID)
		facts, err := s.recovery.Facts(ctx, e.AdapterInstanceID)
		if err != nil {
			return rep, fmt.Errorf("execution: 归因 %s（%s）: %w", e.ID, e.AdapterInstanceID, err)
		}
		if len(facts) == 0 {
			rep.Unattributed = append(rep.Unattributed, e.ID)
			continue
		}
		ok = append(ok, attributed{e: e, facts: facts})
	}
	// 第二遍才写；一条失败就停（调用方回滚整笔，不许留一个"改了一半"的现场）。
	for _, a := range ok {
		if err := s.markInterrupted(ctx, tx, a.e, a.facts); err != nil {
			rep.Blocked = append(rep.Blocked, Blocked{ID: a.e.ID, Err: err})
			return rep, err
		}
		rep.Marked = append(rep.Marked, a.e.ID)
	}
	return rep, nil
}

// markInterrupted 是一条候选的改判五步：Evidence → Check → EventOf → payload/Touch → 同事务 Audit。
//
// 三处不许顺手改：
//   - 改判正身是 Interrupted 不是 Unknown（09-29 主控裁「分」，见 ScanRecovery 那段）；
//   - Audit 由 RequiresAudit 那张表说了算（§6 的 Audit 列）：表里有这个事件就必须写、
//     没有就不写 —— 不在这里给"要不要审计"另立第二个判断；
//   - 事件类型只从 EventOf 来、audit 行只从注入的 Audit 来。自己拼一个 event_type 或自己
//     INSERT audit_entries 都是 §9.1 P0 / C14（业务包不 import audit、不写别人的表）。
//
// 反例落点仍在第 1 步与第 3 步之间：Reason 改成空串 ⇒ Check 打回 ErrIllegalTransition（证据先于目录）；
// 摘掉 Audit 那一段 ⇒ 判据红（改判有、§6 要的证据没有，正是"半真历史"）。
func (s *Service) markInterrupted(ctx context.Context, tx *db.Tx, e Execution, facts []ProcessFact) error {
	reason := recoveryReason(e, facts)
	if err := Check(e.Status, Interrupted, Recovery, Evidence{Reason: reason}); err != nil {
		return fmt.Errorf("execution: %s→%s（恢复扫描，REQ-RC-1）不在 §5.1 矩阵里: %w", e.Status, Interrupted, err)
	}
	evType := EventOf(e.Status, Interrupted)
	if evType == "" {
		return fmt.Errorf("execution: %s→%s 在 §6 没有对应事件（REQ-DB-4 登记顺序），我不自己造一个: %w",
			e.Status, Interrupted, ErrNoEventForTransition)
	}
	eventID, err := s.recovery.NewEventID()
	if err != nil {
		return fmt.Errorf("execution: 恢复扫描铸事件 id: %w", err)
	}
	cmd := Command{EventID: eventID, Reason: reason}
	payload, err := eventPayload(e.Status, Interrupted, cmd)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	setSQL, setArgs := columnWrites(e, Interrupted, cmd, now)
	if _, err := tx.Touch(db.AggExecution, e.ID, setSQL, setArgs, db.EventDraft{
		EventID: eventID, Type: evType, Aggregate: db.AggExecution, AggregateID: e.ID,
		PrincipalID: s.recovery.PrincipalID, Payload: payload,
	}); err != nil {
		return fmt.Errorf("execution: touch %s（→INTERRUPTED, Occasion=Recovery）: %w", e.ID, err)
	}
	if !RequiresAudit(evType) {
		return nil
	}
	if err := s.recovery.Audit(ctx, tx, AuditRecord{
		ExecutionID: e.ID, TaskID: e.TaskID, EventID: eventID,
		PrincipalID: s.recovery.PrincipalID, Reason: reason,
	}); err != nil {
		return fmt.Errorf("execution: %s 的 %s 没带上 §6 要的 audit 行: %w", e.ID, evType, err)
	}
	return nil
}

// recoveryReason 组一份"说得出依据"的 Reason：Evidence 非空是 Recovery 那几条边的入场券
// （空串会被 Check 直接打回 ErrIllegalTransition），而它同时是 §6 那条 audit 行 details_json 里
// "为什么改判"的唯一出处 —— 事件 payload 本身（ResultPayload 三个键）装不下这段话。
// 依据只有两类：这一台登记过的进程行（id/pid/status/attempt），和本代 Core 启动扫描那一刻
// "这些进程还没被接管"这个观察 —— 后半句锚在 boot 那一次调用上（§2.3 的 Recovery 槽位），
// 谁要把这个扫描挪到运行期再扫一遍，这半句话得跟着改，别让它变成一句没人核过的断言。
// 词按 09-29 主控裁的「分」：这一支是 interrupted / recoverable，**不是**"不可判定"
// （那是 Adapter 崩溃那一支的词，协议 §8，本轮不接线）。
func recoveryReason(e Execution, facts []ProcessFact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "恢复扫描（REQ-RC-1）：%s 上登记过进程行", e.AdapterInstanceID)
	for _, f := range facts {
		if f.Attempt > 0 {
			fmt.Fprintf(&b, "，%s(pid=%d status=%s attempt=%d)", f.ID, f.PID, f.Status, f.Attempt)
			continue
		}
		fmt.Fprintf(&b, "，%s(pid=%d status=%s)", f.ID, f.PID, f.Status)
	}
	fmt.Fprintf(&b, "；本代 Core 启动扫描时尚未接管这些进程，in-flight %s v%d 判为可恢复（FIRST SPRINT §7 interrupted/recoverable）",
		e.ID, e.Version)
	return b.String()
}

// MarkCrashUnknown 是「Adapter 崩溃那一支」的落点（契约① 第 25 条 ④-a，`contracts-internal-v0.1.md:713`：
// "Core 活着、对端进程死；触发点 = Supervisor 观察到子进程死亡 ⇒ `→UNKNOWN`"）。
//
// **今天它没有生产调用者，这是裁定的形状而不是欠账**：那一格的接线按 :716 明写「依赖 `Supervisor.Run`
// 那一格 ⇒ 排在本轮之后」，导出面已派 adapter（T-398①）。T-401②③ 给我的范围是「常量 + 落点 + 用例」，
// 并明写「别为了提前变绿去手搜一条绕过 `Run` 的路」⇒ 我不在这里造触发条件，也不让 `ScanRecovery`
// 顺手喊它（boot 扫描那一支的正身是 INTERRUPTED，09-29 裁的「分」）。
//
// 落库四件，与 `markInterrupted` 同一条序列、不同的态与不同的词：
//  1. Check(cur→Unknown, **Occasion=Recovery**, Evidence 非空) —— §5.5 三条件的"Recovery + 非空 Evidence"；
//     "非终态"那一条件由 `statemachine.go:93~98` 那句 `!Terminal(from)` 把守：已定局的行不许被改判成
//     UNKNOWN（§12 丢历史红线）。**这里不另立第二个判断**；
//  2. 事件类型只从 `EventOf` 来。这一支不再写一道 `evType == ""` 的守卫：`EventOf(x, Unknown)` 今天恒返回
//     `EventUnknown`，那道空值检查是死分支；真正的兜底在第 3 步 —— `eventPayload` 的 default 支会带着
//     `ErrNoEventForTransition` 出声，且它跑在任何写之前，所以"§6 没这行"依然一行都不写。
//  3. payload = `from` + `reason`（契约 :717 点名那两个键，见 `UnknownPayload` 注释里我报的那处字面分歧）；
//     Touch 只动 status/updated_at —— `Terminal(Unknown) == false` ⇒ **不写 finished_at**，这正是"不可判定"
//     与"已失败"在列上的区别，别为了"看起来完整"去补那一列。
//  4. 同事务 Audit：`RequiresAudit(EventUnknown)` 为真（:717「Audit 必带」），链/脱敏/归因仍住在 cmd/core（C14）。
//
// **不发 §5.3 的 task 投影**：契约 :720~723 明写 `execution_unknown` 不进那七条边、task 留在 `RUNNING`
// 是矩阵不是欠账 ⇒ 这里一次都不调 projector，也不喊"投影被拒"。要动 task 态的是恢复扫描的 INTERRUPTED 那一支。
//
// facts 由调用方递（同 `ScanRecovery`：本包不碰 `process_instances` 的 SQL，§0 跨包禁令）；
// 空 facts ⇒ 没有主体可归属，按 CO-44② 明确拒绝、一行不写 —— 与扫描那条"查不到行就不改判"同一口径。
func (s *Service) MarkCrashUnknown(ctx context.Context, tx *db.Tx, id string, facts []ProcessFact) (Result, error) {
	if s.recovery.NewEventID == nil || s.recovery.PrincipalID == "" || s.recovery.Audit == nil {
		return Result{}, fmt.Errorf("execution: 崩溃改判没装配齐（NewEventID/PrincipalID/Audit）: %w", ErrRecoveryNotWired)
	}
	if len(facts) == 0 {
		return Result{}, fmt.Errorf("execution: %s 这一支没有进程行可归属（崩溃观察无主体）⇒ 一行不写: %w", id, ErrRecoveryNotWired)
	}
	e, err := NewRepository(tx).Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	reason := crashReason(e, facts)
	if err := Check(e.Status, Unknown, Recovery, Evidence{Reason: reason}); err != nil {
		return Result{}, fmt.Errorf("execution: %s→%s（崩溃恢复，REQ-RC-2）不在 §5.1 矩阵里或证据不足: %w", e.Status, Unknown, err)
	}
	evType := EventOf(e.Status, Unknown)
	eventID, err := s.recovery.NewEventID()
	if err != nil {
		return Result{}, fmt.Errorf("execution: 崩溃改判铸事件 id: %w", err)
	}
	cmd := Command{EventID: eventID, Reason: reason}
	payload, err := eventPayload(e.Status, Unknown, cmd)
	if err != nil {
		return Result{}, err
	}
	now := time.Now().UnixMilli()
	setSQL, setArgs := columnWrites(e, Unknown, cmd, now)
	res, err := tx.Touch(db.AggExecution, e.ID, setSQL, setArgs, db.EventDraft{
		EventID: eventID, Type: evType, Aggregate: db.AggExecution, AggregateID: e.ID,
		PrincipalID: s.recovery.PrincipalID, Payload: payload,
	})
	if err != nil {
		return Result{}, fmt.Errorf("execution: touch %s（→UNKNOWN, Occasion=Recovery）: %w", e.ID, err)
	}
	if !RequiresAudit(evType) {
		return Result{From: e.Status, To: Unknown, Advanced: true, Version: res}, nil
	}
	if err := s.recovery.Audit(ctx, tx, AuditRecord{
		ExecutionID: e.ID, TaskID: e.TaskID, EventID: eventID,
		PrincipalID: s.recovery.PrincipalID, Reason: reason,
	}); err != nil {
		// 与扫描那条同一个处置：审计行落不下去 ⇒ 整笔回滚，不改判"有证据没有"的半真历史。
		return Result{}, fmt.Errorf("execution: %s 的 %s 没带上 §6 要的 audit 行: %w", e.ID, evType, err)
	}
	return Result{From: e.Status, To: Unknown, Advanced: true, Version: res}, nil
}

// crashReason 是「不可判定」那一支的依据文本。词与扫描那条**刻意不同**：
// FIRST SPRINT §7 的 interrupted/recoverable 说的是"Core 自己重启、进程没被接管"，
// 而这一支是"Core 还活着、对端那台死了" —— 协议 §8 给它的是 UNKNOWN（非 FAILED），
// 所以这里不许出现"可恢复"或"失败"任何一个词，也不许把 status 折算成"死了"：行原样搬。
// 还有一个不那么显然的约束：**reason 里不写否定句**（原话里带 "不是 FAILED" 那种写法，会让
// "reason 里不许出现那个词"这条判据咬到自己 —— 探针 M-k 就是这么抓出来的）。裁定那句留在注释与契约里，
// reason 只说观察与结论。
func crashReason(e Execution, facts []ProcessFact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "崩溃恢复（REQ-RC-2）：%s 上登记过进程行", e.AdapterInstanceID)
	for _, f := range facts {
		if f.Attempt > 0 {
			fmt.Fprintf(&b, "，%s(pid=%d status=%s attempt=%d)", f.ID, f.PID, f.Status, f.Attempt)
			continue
		}
		fmt.Fprintf(&b, "，%s(pid=%d status=%s)", f.ID, f.PID, f.Status)
	}
	fmt.Fprintf(&b, "；调用方（触发点 = Supervisor 观察到子进程死亡，那一格尚未接线）报告这些进程已终止，"+
		"而 Core 仍活着 ⇒ in-flight %s v%d 的轮次结果不可判定（协议 §8：崩溃那一支转 UNKNOWN）",
		e.ID, e.Version)
	return b.String()
}

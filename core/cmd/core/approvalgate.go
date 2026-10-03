package main

// 审批门（T-442 ② 的第一到第三步）：§6 那**第七条** A→C 通知 `approval/request` 到了 Core 干什么。
//
// 顺序本身就是判据，一步都不许换（契约② §6 那一行 + 主控 22:2x 那句"这一条是『门』与『通知』的唯一区别"）：
//
//	读那一行 → `permission.Engine.Decide` → 要批就 `approval.Service.Request` 落一行 PENDING
//	→ 同一笔事务里推 `RUNNING → WAITING_APPROVAL`（带 §6 那条 `execution_approval_requested` 与它 ✓ 列要的审计行）
//	→ **停在这里**：Core 自己不发 `approval/respond`，那一句留给用户 `:resolve` 之后（第四步，未接）。
//
// 三件事为什么必须住在这里而不是别处：
//   - `permission` 与 `approval` 两个包谁都不能 import 谁（C14），而这一腿天生要同时叫它们俩 ⇒ 装配层；
//   - `approval.Service.Request` **自带事务**，把它套进我的 tx 就是 CO-45 那格挂死（写池只有一条连接）
//     ⇒ 这里必然是**两笔短事务**，中间那个"一半成了"的窗口由顺序压住：先落审批行、后推状态边。
//     反过来会留下一条**楔死**的执行——状态在等审批、库里却没有可批的单。
//   - 风险等级只有一个出处（§3.0-d 那句"不许 backend 抄一份"）⇒ 这里直接叫 `permission.RiskLevel`，
//     拿到什么写什么；`registered=false` 时它返回的 `UNKNOWN` 也照原样写，那才是 §3.1 第二行的失败关闭。
//
// 适配器帧里那个 `risk_hint` **一个字节都没被用**：协议明写"不认识的串由 Core 判成 UNKNOWN 并失败关闭，
// 不许适配器自己降级"，所以那半格是 Provider 的自述，不是判定输入。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/permission"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// approvalGatePrincipal 是这一腿里**唯一的**主体来源。它不是从帧上读来的（A→C 通知里没有主体），
// 也不是从 `executions` 读来的（那张表没有主体列）——它是契约① §3 规则 7 那句
// 「v0.1 是本地单用户……唯一可接受的字面量 `local-user`」的直接引用。
// **这是引用不是发现**：多用户那天主体必须从"这一轮是谁起的"取（`event_journal.principal_id` 里已经有），
// 换的是这一行，不是那两处调用。
const approvalGatePrincipal = "local-user"

// approvalWindowMillis 是 §3.0-d 那条尺寸的原文：`ExpiresAt` 必填、v0.1 取 15 分钟（主控裁的数）。
// 为什么必填（同一条款给的）：`SweepExpired` 靠它把 PENDING 翻成 EXPIRED，
// 一个空期限就是"永远堵着那条执行的锁"。
const approvalWindowMillis = int64(900000)

// approvalGateType 是 v0.1 唯一的审批来源（§3.0-d：`Type` 固定 `"PERMISSION"`，
// 因为今天只有一个地方会造审批行；第二个来源出现那天再谈词表）。
const approvalGateType = "PERMISSION"

// dispatchGateAction 是"Core 要把这一轮派出去"这道题在 **§3.1 那张内置分类表**里的名字：`session.send`。
//
// 它与协议 §4 那条 C→A 方法名 `session/send` 是**两套词汇**，不要合成一个串（我第一版就合了，
// 后果被 B-2a 那枚反例当场照出来：分类表里没有 `session/send` ⇒ `RiskLevel` 答未登记 ⇒ §3.1 第二行失败关闭 ⇒
// **装配好的 Core 会把每一发正常派发都判成"要人批"并落一张待审批**）。
// 协议方法名的出处在 `sendpipe.go` 那个调用里（那是线格式，不是权限词表）；这里只引用 §3.1 的串，
// 而那张表的权威住在 `internal/permission`（`permission.go:136`–`:140` 的 lowMediumActions）⇒ 我不抄第二份判定，
// 只按它的写法给出这一个动作名。
const dispatchGateAction = "session.send"

// grantScope 是「这一件事之后都允许」选的作用域，取 TASK：
// `temporary_grants` 的合法值里只有它与那句话对得上（GLOBAL 是整台机器，SESSION/ADAPTER/MODEL
// 都不带「这一件事」的归属），而 `approval.GrantRequest.ScopeType`（`internal/approval/approval.go:152`）
// 用的是同一个枚举 ⇒ 这里不翻译，直接拿它的常量转串。
const grantScope = string(permission.ScopeTask)

// executionRows 是本腿要的那一个读方法（窄接口：`*execution.Repository` 正好满足，
// 与 transport 那些读面注入同一把尺）。
type executionRows interface {
	Get(ctx context.Context, id string) (execution.Execution, error)
}

// approvalGate 是三件外部输入加一只钟的容器。`turnHub.gate == nil` 表示**没装配**，
// 那时这一帧只喊不写（见 gateApproval 第一格）。
type approvalGate struct {
	perm *permission.Engine
	asks *approval.Service
	rows executionRows
	now  func() int64
}

// enableApprovalGate 是泵的注入位（与 `Service.EnableSend`/`EnableRecovery` 同一套做法：
// 不动 newTurnHub 那条已经分散在六处的签名，注入位单独开）。
// `now` 传 nil 就用系统钟——这一腿不需要可推进的时钟（期限是 15 分钟后的一个绝对毫秒数，
// 不是"活多久"），但测试要能把那一格钉成一个可断言的定值。
func (h *turnHub) enableApprovalGate(perm *permission.Engine, asks *approval.Service, rows executionRows, now func() int64) {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	h.gate = &approvalGate{perm: perm, asks: asks, rows: rows, now: now}
}

// gateApproval 是 `approval/request` 那一帧的全部后果。每一步失败都**只喊不猜**：
// 喊出来的那一句带着它在哪一步停下的，因为事后看日志的人要能区分"没走到"与"走到了但不批"。
func (h *turnHub) gateApproval(key string, gen uint, executionID string, s *adapter.StreamFrame) {
	if h.gate == nil {
		// 没装配 ≠ 没有这一帧。失败关闭成"一行都不写"，并且把这一格点名出来，
		// 免得日后有人看见库里干净就以为"这一轮没要审批"。
		h.logf("turn 泵 %s #%d: execution=%s 收到 approval/request(action=%q) ⇒ 但审批门没装配（perm/asks/rows 三件缺任一），一行都不写",
			key, gen, executionID, s.Action)
		return
	}
	ctx, cancel := h.callCtx()
	defer cancel()

	ex, err := h.gate.rows.Get(ctx, executionID)
	if err != nil {
		h.logf("turn 泵 %s #%d: 收到 approval/request 但那一行读不出来（execution=%s, action=%q）⇒ 一行都不写: %v",
			key, gen, executionID, s.Action, err)
		return
	}
	if ex.Status != execution.Running {
		// 这一格就是主控那枚反例的正面：那一轮已经定局（或根本没在跑）⇒
		// 不许落审批行、不许改终态、不许推任何边，只喊。
		late := "不在跑（库里是 " + string(ex.Status) + "）"
		if execution.Terminal(ex.Status) {
			late = "这一轮已定局（库里是 " + string(ex.Status) + "）"
		}
		h.logf("turn 泵 %s #%d: execution=%s 迟到的 approval/request(action=%q) ⇒ %s，一行都不写",
			key, gen, executionID, s.Action, late)
		return
	}

	// ① 风险等级：只有一个出处（`registered` 只用来把话说清，不改写值）。
	level, registered := permission.RiskLevel(s.Action)

	// ② 判定：这一格判的是"要不要人来批"，不是"这个人有没有资格批"（§3 规则 7 ①：两件事永远不许混）。
	eff, reason, err := h.gate.perm.Decide(ctx, permission.Request{
		PrincipalID: approvalGatePrincipal,
		Action:      s.Action,
		Resource:    s.Resource,
		TaskID:      ex.TaskID,
		ExecutionID: ex.ID,
		SessionID:   ex.SessionID,
		AdapterID:   ex.AdapterInstanceID,
		ModelID:     ex.ModelID,
	})
	if err != nil {
		// 读不出来（紧急停止状态未知 / 策略读不了 / 授权读不了）⇒ 失败关闭：既不放行，也不造审批行。
		h.logf("turn 泵 %s #%d: execution=%s 的权限判定读不出来(action=%q) ⇒ 一行都不写，也不当作放行: %v",
			key, gen, executionID, s.Action, err)
		return
	}
	if eff != permission.RequireApproval {
		// ALLOW / ALLOW_TEMPORARY / DENY 三支今天**都不写库**：§6 那句硬要求只裁了 REQUIRE_APPROVAL 一支，
		// 而"门开了要送什么出去"协议里没有第二条出站帧 ⇒ 只喊，并把这一格留在待裁。
		h.logf("turn 泵 %s #%d: execution=%s 的 approval/request(action=%q, 风险 %s/登记=%v) 判定是 %s(%s) ⇒ 不用人批，一行都不写",
			key, gen, executionID, s.Action, level, registered, eff, reason)
		return
	}

	// ③ 落那一行 PENDING 并推那条边（两笔短事务，顺序由 raiseGate 钉死）。
	a, err := h.raiseGate(ctx, ex, s.Action, s.Resource, level)
	if err != nil {
		// raiseGate 把「停在哪一步」写在错误里；这里只补一句是哪个 key/代次在喊。
		h.logf("turn 泵 %s #%d: execution=%s 这道门没关成(action=%q): %v", key, gen, executionID, s.Action, err)
		return
	}

	// 这一声必须把"没人会自己把它打开"说清楚：第四步（用户 `:resolve` 之后经 C→A `approval/respond`
	// 回话、并把 `WAITING_APPROVAL → RUNNING` 那条放行边推掉）今天**没接**。
	// 不写出来的话，库里那条 WAITING_APPROVAL 会被下一个人读成"链已经通了"。
	h.logf("turn 泵 %s #%d: execution=%s 停在审批门上（approval=%s, action=%q, 风险=%s, 判定=%s/%s）⇒ 本轮到此为止，Core 不替用户答复。注意：批准之后那一腿（approval/respond + 放行边）还没接，这一格不会自己打开",
		key, gen, executionID, a.ID, s.Action, a.RiskLevel, eff, reason)
}

// gateCommand 给这一笔的身份：一条事件一个 id（`event_journal.event_id` 是 UNIQUE 列），
// 域不自己生成（§3 的 UUIDv7 由调用方铸），与 `claimCommand` 同一把尺。
// 载荷四键里 `risk_level` 取的是 **`Request` 返回的那一行**（`a.RiskLevel`）而不是我塞进 Draft 的那个串：
// 它会把空值归一成 `UNKNOWN`（`internal/approval/approval.go:261`–`:263`），
// 而库里那一列与事件里那一键必须同值（两处同事实、必然同值，与 `columnWrites` 给 result_code 的约定同形）。
func (h *turnHub) gateCommand(approvalID, action, resource, riskLevel string) execution.Command {
	correlation := adapter.NewRequestID().String()
	return execution.Command{
		EventID:           adapter.NewRequestID().String(),
		PrincipalID:       approvalGatePrincipal,
		CorrelationID:     correlation,
		CausationID:       correlation,
		ApprovalRequested: execution.ApprovalRequestedPayload{ApprovalID: approvalID, Action: action, Resource: resource, RiskLevel: riskLevel},
	}
}

// gateAudit 是 §6 那两行的 Audit 列 ✓ 的落地：**同一笔事务**里带一条 audit_entries。
// 形状照 `recovery.go:77 recoveryAudit`（链头、脱敏执行、归因行都在装配层，C14/W13-2 同一条规矩），
// 差别只在 `Action` 用这条边的事件名，且 `details_json` 多带 `approval_id`——
// 事后要看的是"哪一次执行被哪一张单子挡住了"，那一格正是这条边存在的全部理由。
func gateAudit(ctx context.Context, tx *db.Tx, now func() int64, from execution.State,
	executionID, taskID, eventID, approvalID, action, riskLevel, principal string) error {
	details, err := json.Marshal(map[string]any{
		"event_id": eventID, "approval_id": approvalID,
		"action": action, "risk_level": riskLevel, "from": string(from),
	})
	if err != nil {
		return fmt.Errorf("审批门的 audit details_json 编不出来: %w", err)
	}
	auditID := "audit-" + eventID
	ch := &audit.Chain{Head: audit.HeadQuery(tx), Insert: tx, Now: now, Scrub: auditScrub}
	if _, err := ch.Append(ctx, audit.Entry{
		AuditID: auditID, PrincipalID: principal,
		Action: execution.EventApprovalRequested, ResourceType: "execution", ResourceID: executionID,
		TaskID: taskID, ExecutionID: executionID, CorrelationID: eventID,
		Outcome: audit.OutcomeSuccess, DetailsJSON: string(details),
	}); err != nil {
		return fmt.Errorf("审批门的 audit 行没写出去: %w", err)
	}
	if _, err := ch.RecordRedaction(ctx, audit.Entry{
		AuditID: "redaction." + auditID, PrincipalID: principal,
		Action: "redaction." + execution.EventApprovalRequested, ResourceType: "execution",
		ResourceID: executionID, TaskID: taskID, ExecutionID: executionID, CorrelationID: eventID,
	}, redactionHits(tx, ch)); err != nil {
		return fmt.Errorf("审批门的脱敏归因行没写出去: %w", err)
	}
	return nil
}

// permissionRequest 把「这一行」与「要做的动作」拼成 §3 那道题的输入。
// 两条腿（入站帧 / 派发前）共用它，所以字段不会一边有一边没有。
// **没填进去的三格**（ProjectID/TrustDomainID/TaskMode）是真没有：这一行上没有这三样，
// 空串到 `Engine` 那一侧就是匹配不上那个 rung（`permission.go:323` 的 inScope 口径），
// 不是「匹配到全局」—— 这两种「没填」的差别是这套判据唯一不能含糊的地方。
func permissionRequest(ex execution.Execution, action, resource string) permission.Request {
	return permission.Request{
		PrincipalID: approvalGatePrincipal,
		Action:      action,
		Resource:    resource,
		TaskID:      ex.TaskID,
		ExecutionID: ex.ID,
		SessionID:   ex.SessionID,
		AdapterID:   ex.AdapterInstanceID,
		ModelID:     ex.ModelID,
	}
}

// raiseGate 是「要批」那半段的唯一实现：落一行 `PENDING`（三键按 §3.0-d）→ 另一笔短事务推
// `RUNNING → WAITING_APPROVAL`，并在同一笔里带上 §6 ✓ 列要的那条归因行。
//
// 顺序是**先落单、后推边**（第 318 条那条裁定在这里复用）：反过来会留下一条楔死的执行 ——
// 状态在等审批、库里却没有可批的单。这里只返回错误让调用方喊：不悄悄重试（同一张单重试会撞
// Service 自己的幂等口径），也不回滚别人的行（那张审批行不归我）。
func (h *turnHub) raiseGate(ctx context.Context, ex execution.Execution, action, resource, level string) (approval.Approval, error) {
	a, err := h.gate.asks.Request(ctx, approval.Draft{
		PrincipalID:       approvalGatePrincipal,
		TaskID:            ex.TaskID,
		ExecutionID:       ex.ID,
		SessionID:         ex.SessionID,
		AdapterInstanceID: ex.AdapterInstanceID,
		Type:              approvalGateType,
		RiskLevel:         level,
		Action:            action,
		Resource:          resource,
		ExpiresAt:         h.gate.now() + approvalWindowMillis,
	})
	if err != nil {
		return approval.Approval{}, fmt.Errorf("审批行没落下去(action=%q) ⇒ 门不关，一行都不写: %w", action, err)
	}
	cmd := h.gateCommand(a.ID, action, resource, a.RiskLevel)
	if err := h.store.Write(ctx, func(tx *db.Tx) error {
		res, err := h.svc.BlockForApproval(ctx, tx, ex.ID, cmd)
		if err != nil {
			return err
		}
		return gateAudit(ctx, tx, h.gate.now, res.From, ex.ID, ex.TaskID, cmd.EventID,
			a.ID, action, a.RiskLevel, approvalGatePrincipal)
	}); err != nil {
		// 那个「一半成了」的窗口如实喊出来：库里现在有一张 PENDING，而执行还在 RUNNING。
		return a, fmt.Errorf("审批行落了（approval=%s）但门没关上 ⇒ 库里现在有一张没人等的 PENDING: %w", a.ID, err)
	}
	return a, nil
}

// pendingGate 问一次 `ApprovalStates.BlockingState`：这条执行现在是不是挂着一张**活的**待审批。
//
// 这一格挡的不是「库里已经写着 WAITING_APPROVAL」—— 那一格 §5.2 的状态机自己就挡下了。
// 它挡的是**半成窗口**：审批行落了、边没推上（`raiseGate` 第二笔失败那条路），执行表面还在 RUNNING。
// 没有这一步，下一次 send 会把这一轮直接派出去，等于把门关着当没门 —— 也正是主控那句
// 「摘掉 BlockingState 那一格 ⇒ 待审批的执行仍能起来」要红的地方。
func (h *turnHub) pendingGate(ctx context.Context, executionID string) (string, bool, error) {
	state, err := h.gate.asks.BlockingState(ctx, executionID)
	if err != nil {
		return "", false, fmt.Errorf("问不出这条执行挂没挂审批（execution=%s）⇒ 不派发: %w", executionID, err)
	}
	if state != approval.PENDING {
		return "", false, nil
	}
	list, err := h.gate.asks.ForExecution(ctx, executionID)
	if err != nil {
		return "", true, fmt.Errorf("说 PENDING 却读不出那一张（execution=%s）⇒ 按挡住处理: %w", executionID, err)
	}
	now := h.gate.now()
	for _, a := range list {
		// 只认 `Live`：过期但还没被 sweep 的行不挡路，与 BlockingState 自己那一格同一个口径
		// （`internal/approval/approval.go:106`），不在这两处各写一份判断。
		if a.Live(now) {
			return a.Action, true, nil
		}
	}
	// 走到这里 = BlockingState 说 PENDING、按 Live 筛却一张都不剩（过期与决议之间那道夹缝）。
	// **不猜成「没挂」**：返回空 action，下一格 CheckTemporaryGrant 会因缺 action 明确报错 ⇒ 失败关闭。
	return "", true, nil
}

// gateBeforeDispatch 是派发一轮之前的三道题，**顺序不许反**（T-452 主控钉的）：
//
//	待审批（BlockingState）→ 临时授权（CheckTemporaryGrant）→ 判定（Engine.Decide）→（要批）落一行待审批并停住
//
// 为什么先查授权才轮到判定：上一批做的「8 小时临时允许」如果没人读，它是一条**落得进去、没人取得出来**的死码。
// `Engine.Decide` 内部也走 grant 那一rung，所以「有读者」这件事并不只靠这一次调用；但它是这一腿上
// **唯一能把「挂着待审批」改判成「放行」的读法**（授权绕过的是那张挂着的单子，不是默认策略），
// 所以必须排在判定之前 —— 排在后面就永远走不到那一支。
//
// 返回的错全部落在**已登记的出口**上：拒绝派发一律包 `execution.ErrIllegalTransition`，
// transport 那一侧走现成的 409 `illegal_transition`（`api/http/executions.go:420`）⇒
// 不新造码、不 202、不静默、也不把「被拒」读成「适配器病了」。
func (h *turnHub) gateBeforeDispatch(ctx context.Context, executionID string) error {
	if h.gate == nil {
		// 没装门的 Core 也不会有这一腿的入站帧（泵与门都在同一个 hub 上），所以这不是「跳过检查放行」，
		// 而是「这台机器上从来没人问过审批」—— 入站那一腿会在自己的第一格里喊出来。
		return nil
	}
	action, blocked, err := h.pendingGate(ctx, executionID)
	if err != nil {
		return err
	}
	ex, err := h.gate.rows.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("派发前要读的那一行读不出来（execution=%s）⇒ 不派发: %w", executionID, err)
	}
	if blocked {
		granted, err := h.gate.perm.CheckTemporaryGrant(ctx, approvalGatePrincipal, grantScope, ex.TaskID, action)
		if err != nil {
			return fmt.Errorf("挂着待审批（action=%q）而临时授权问不出来 ⇒ 不派发: %w", action, err)
		}
		if granted {
			// 这一句就是「8 小时」那条记录**有人读**的证据；它省下的不是判定，是那张挂着的门。
			h.logf("send %s: 挂着待审批(action=%q)但 %s 范围的临时授权覆盖了它 ⇒ 放行，本轮不再二次询问用户",
				executionID, action, grantScope)
			return nil
		}
		return fmt.Errorf("%w: 这条执行挂着待审批（action=%q）⇒ 本轮不派发，等面板 :resolve 答复",
			execution.ErrIllegalTransition, action)
	}

	// 没挂着门：先问授权（命中就省掉一次判定），未命中再问判定。
	granted, err := h.gate.perm.CheckTemporaryGrant(ctx, approvalGatePrincipal, grantScope, ex.TaskID, dispatchGateAction)
	if err != nil {
		return fmt.Errorf("临时授权问不出来（action=%q）⇒ 失败关闭，不派发: %w", dispatchGateAction, err)
	}
	if granted {
		h.logf("send %s: %s 范围的临时授权覆盖了 %q ⇒ 直接派发，不再问判定", executionID, grantScope, dispatchGateAction)
		return nil
	}
	eff, reason, err := h.gate.perm.Decide(ctx, permissionRequest(ex, dispatchGateAction, ex.SessionID))
	if err != nil {
		return fmt.Errorf("派发前的判定读不出来（action=%q）⇒ 一行都不写、也不派发: %w", dispatchGateAction, err)
	}
	if eff != permission.RequireApproval {
		// ALLOW / ALLOW_TEMPORARY 直接派发；DENY 这一支今天既不派发也不落单，出站语义仍待裁（第 318 条）。
		return nil
	}
	level, _ := permission.RiskLevel(dispatchGateAction)
	if _, err := h.raiseGate(ctx, ex, dispatchGateAction, ex.SessionID, level); err != nil {
		return err
	}
	return fmt.Errorf("%w: 这一轮要人批（判定=%s/%s）⇒ 已落一行待审批，本轮不派发",
		execution.ErrIllegalTransition, eff, reason)
}

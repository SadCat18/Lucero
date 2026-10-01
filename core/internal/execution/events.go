package execution

import (
	"encoding/json"
)

// 事件类型 = §6 目录中触发者为 execution 的行。
//
// `execution_approval_requested` / `execution_approval_resolved` **今天在本包定义，昨天不在**：
// 目录那两行原写 `aggregate_type = approval`，而同一份 §6 的边表（`event-contract-v0.1.md:131`/`:132`）
// 把这两条边挂在 execution 的状态机上 —— 按 REQ-B-1「`aggregate_type` 决定谁能发它」，
// 只要目录写 approval，`Touch(db.AggExecution, …)` 就永远发不出它（平台层那句
// `aggregate_type %q but touching %q`，`internal/platform/db/touch.go:571`）。
// 主控 10-01 裁的就是这一格（REQ-APR-1「改目录不改边表」，`git show 734931f` 现读那两行已改成 **execution**），
// 所以名字落在我包里。**边仍然是执行的状态边**：审批对象自己的 `approval_requested` / `approval_resolved`
// 仍归 `internal/approval`（audit）发，两族各有其位，不许谁替谁发。
const (
	EventCreated       = "execution_created"
	EventStarted       = "execution_started"
	EventOutputFlushed = "execution_output_flushed"
	// EventApprovalRequested 是 `RUNNING → WAITING_APPROVAL` 那一格的事件（§6 边表 :131）。
	EventApprovalRequested = "execution_approval_requested"
	// EventApprovalResolved 是 `WAITING_APPROVAL → RUNNING` 那一格的事件（§6 边表 :132，"批准"）。
	// 它**不与 `EventResumed` 互换**：`execution_resumed` 说的是"暂停/等输入/被打断之后续跑"，
	// 载荷只有一个 `from`；把审批放行写成 resumed，就等于说"这一格没有过一次人工决定"。
	EventApprovalResolved = "execution_approval_resolved"
	EventPaused           = "execution_paused"
	EventResumed          = "execution_resumed"
	EventStarting         = "execution_starting" // REQ-DB-4（09-24）：CREATED→STARTING 由它表达，§6 那行 payload 只有 `from`
	EventSucceeded        = "execution_succeeded"
	EventFailed           = "execution_failed"
	EventCancelled        = "execution_cancelled"
	EventInterrupted      = "execution_interrupted"
	// EventUnknown 是「Adapter 崩溃那一支」（Core 活着、对端进程死）的事件名，出处不是我：
	// 契约① 第 25 条 ④-a 那句「REQ-RC-2 裁「甲」：`→UNKNOWN` 的 §6 事件名**定 `execution_unknown`**」
	// （`specs/contracts-internal-v0.1.md:717`，T-401 ① 把这三段原文重新指给我，之前我说"没落契约正文"是错的）。
	// **§6 那一行不在本批里由我加**：:718~719 明写「现在不登 §6 —— C8-12 是双向对拍，先登行必红；
	// 登记授权随落地批给出：backend 落 UNKNOWN 路径的同一提交里加常量 + 主控批 §6 行」。
	// 所以本常量是那一枚"落地批的码"半边，§6 行 + `internal/eventjournal` 目录那半边分别归主控与 db。
	EventUnknown = "execution_unknown"
	// EventClaimed 不是状态迁移的事件，而是**一列的值**的事件：REQ-RC-3 通道裁 (i)（契约①
	// 第 25 条 ④-a :741，09-30 19:3x，回我第 304 条 ① 的甲/乙问）——
	// 「**新增一行 §6 事件 `execution_claimed`**（aggregate `execution`、载荷 `provider_execution_id`、Audit `–`）」，
	// (ii)（db 开"聚合行无事件列写"的口子）被明拒，理由是"每次 `Touch` 带一条事件"是审计链地基。
	// 三条硬时机照原文落点在 claim.go：只在 `turn/claimed` 帧到达那一刻写、**不许**在 `dispatched` 补写、
	// **不许**借这一笔把状态显示成 running（开始证明仍只认 `dispatched`，契约② §6 规则 1）。
	//
	// 所以它**不进 `EventOf`**：那张表按"迁移"索引，而这一笔不推任何边（`From == To`）。
	// 由 `EventOf` 反查它必然得到空串，那是正确的，别为了"让它有事件名"把它塞进矩阵。
	// §6 那一行归主控、`internal/eventjournal` 的目录条目归 db（:752 那句"三处同改"），我不碰 specs。
	EventClaimed = "execution_claimed"
)

// EventOf 返回该迁移对应的事件类型；"" 表示该迁移不由本包发事件
// （WAITING_INPUT / TRANSFERRING 在 §6 目录里根本没有事件）。
// 事件存在与否与"是否驱动 Task"是两件事：§5.3 之外的事件一律不驱动 Task。
//
// 两张审批边（RUNNING ⇄ WAITING_APPROVAL）从 REQ-APR-1 那一次裁定起归本包发 ——
// 在此之前它们是 ""，而"无事件的状态边落不了库"是平台规矩（`advance` 当场翻
// ErrNoEventForTransition），所以那两条边当时**在写面上根本走不通**，不是"有别家替我发"。
func EventOf(from, to State) string {
	switch to {
	case Starting:
		return EventStarting
	case WaitingApproval:
		return EventApprovalRequested
	case Succeeded:
		return EventSucceeded
	case Failed:
		return EventFailed
	case Cancelled:
		return EventCancelled
	case Interrupted:
		return EventInterrupted
	case Unknown:
		// 这一支今天没有生产调用者：触发点是 Supervisor 观察到子进程死亡（契约① :716 明写
		// 「这条的接线依赖 `Supervisor.Run` 那一格 ⇒ 排在本轮之后」，导出面已派 adapter = T-398①）。
		// 加这一格是为了让"落点 + 用例"先到位（T-401②③），**不是**为了绕过那条依赖去手搜触发路径。
		return EventUnknown
	case Paused:
		return EventPaused
	case Running:
		switch from {
		case Starting:
			return EventStarted
		case WaitingApproval:
			return EventApprovalResolved
		case Paused, WaitingInput, Interrupted:
			return EventResumed
		}
	}
	return ""
}

// auditRequired = §6 目录的 Audit 列 ✓：这些事件必须同事务带一条 audit_entries。
// `EventUnknown` 在列里是契约① :717 那句「Audit 必带」的落点（与 `execution_interrupted` 同待遇）；
// §6 那一行由主控在同批补，**这一张表先于表**是本批的形状，不是越界登记 —— 登记（写 §6）与
// 消费（这里要不要审计）是两件事，后者归执行落地的那一方，前者归 specs。
//
// `EventClaimed` **不在**这张表里，而且不许补进去：:741 那行的 Audit 列是 `–`（一条把手的观察不是
// 一次需要证据的裁决）。它要是被补进来，claim.go 那一笔就会去要一条没人给的 audit 行、当场写不下去。
//
// 两张审批边进来是跟着 §6 那两列的 ✓ 走的（`event-contract-v0.1.md:216`/`:217` 现读都是 ✓，
// 不是我自己给的待遇：一次人工决定正是这一列要留证据的那种事）。**但要说清这张表今天谁在读**：
// 只有 `recovery.go` 那两条腿按它决定要不要审计（`RequiresAudit`，:179/:273），
// 通用的 `advance` 路径**不看它** —— 所以 `EventFailed`/`EventInterrupted` 在生产里也没带过审计行。
// 这张表加这两格是为了让"要不要审计"这个事实只有一个出处；**真正去带那条审计记录，是接 ② 那一腿的活**
// （`cmd/core` 装配，与 `recoveryAudit` 同形），不是把名字塞进这张表就算做完。
var auditRequired = map[string]bool{
	EventFailed: true, EventInterrupted: true, EventUnknown: true,
	EventApprovalRequested: true, EventApprovalResolved: true,
}

func RequiresAudit(eventType string) bool { return auditRequired[eventType] }

// Payload 把 payload 结构体转成 db.EventDraft.Payload 要的 map。
// "schema":1 由平台层在 appendEvent 里缺键时补（db 已确认），本包不再重复保证。
func Payload(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// 事件目录 §6 的执行域 payload。一个事件类型一个结构体。
type CreatedPayload struct {
	TaskID            string      `json:"task_id"`
	AdapterInstanceID string      `json:"adapter_instance_id"`
	ModelID           string      `json:"model_id,omitempty"`
	AttemptNo         int64       `json:"attempt_no"`
	TriggerType       TriggerType `json:"trigger_type"`
}

type StartedPayload struct {
	ProviderExecutionID string `json:"provider_execution_id,omitempty"`
	ModelSnapshotSHA256 string `json:"model_snapshot_sha256"`
}

type OutputFlushedPayload struct {
	BatchFirstSeq int64  `json:"batch_first_seq"`
	BatchLastSeq  int64  `json:"batch_last_seq"`
	CharCount     int64  `json:"char_count"`
	Channel       string `json:"channel"`
}

type PausedPayload struct {
	Reason string `json:"reason"`
}

// ResumedPayload 的 from 区分 PAUSED / WAITING_INPUT / INTERRUPTED 三种恢复来源。
type ResumedPayload struct {
	From State `json:"from"`
}

// StartingPayload 是 §6 的 `execution_starting` 那一行：目录里只登记了 `from` 一个键。
// 不复用 ResumedPayload（键集今天相同是巧合，§6 是两行，以后改一行不该牵动另一行）。
type StartingPayload struct {
	From State `json:"from"`
}

// ResultPayload 同用于 succeeded / failed / cancelled / interrupted；
// 区分靠 event_type，不靠压缩事件类型（§6 表下注）。三个键恒在场：
// 空串表示"没有"，与 omitempty 的"键不存在"是两回事，别让读端两种都要防。
type ResultPayload struct {
	ResultCode string         `json:"result_code"`
	ErrorCode  string         `json:"error_code"`
	Usage      map[string]any `json:"usage"`
}

// UnknownPayload 是 §6 将来那一行 `execution_unknown` 的 payload，键按契约① :717 那句点名的
// **`from` + `reason`** 装配，不复用 ResultPayload。
//
// 这里有一处字面分歧，我按"键名那句"走、把分歧报给主控而不是自己抹平：:717 写的是
// 「与 `execution_interrupted` 同形，payload `from` + `reason`」，而 §6 现在那行 `execution_interrupted`
// （`event-contract-v0.1.md:218` 的合并格）登记的键是 `result_code` `error_code` `usage` ——
// 这两句不可能同时成立，所以"同形"只能读成"同样是一行 execution 域事件 + Audit 列 ✓"，
// 键集以它显式点名的 `from` + `reason` 为准。**若主控落 §6 时选择把 `_unknown` 并进 :218 那条合并行**
// （即键沿用 result_code/error_code/usage），这一格要跟着改回来 —— 那是两行形状之差，不是改名。
//
// 那处分歧已按上面这一读闭合：§6 现在给它的是**独立一行**（`event-contract-v0.1.md:219` 的
// `execution_unknown` | execution | `from` `reason` | ✓），不是并进 :218 的合并行 ⇒ 这两个键是权威，别改。
type UnknownPayload struct {
	From   State  `json:"from"`
	Reason string `json:"reason"`
}

// ClaimedPayload 是 §6 那一行 `execution_claimed` 的 payload，**只有一个键**：
// 载荷按 :741 点名的 `provider_execution_id`，别的键一个都不加 —— 这一事件的存在理由就是
// "Provider 的把手什么时候第一次被 Core 知道"，它不是状态、不是结果、也不带 usage。
// 与列同源：claim.go 那一笔把**同一个值**同时写进 `executions.provider_execution_id` 和这里的键，
// 与 `columnWrites` 给 result_code 的约定同形（两处同事实、必然同值）。
type ClaimedPayload struct {
	ProviderExecutionID string `json:"provider_execution_id"`
}

// ApprovalRequestedPayload 是 §6 那一行 `execution_approval_requested` 的四键
// （`event-contract-v0.1.md:216` 现读：`approval_id` `action` `resource` `risk_level`）。
// 这一行带的是"这次等待卡在哪个审批上"—— 没有 `approval_id` 的事件等于说"在执行、在等，
// 但没人知道等谁批"，那正是 §3 规则 1 要防的形态，所以 `advance` 那一腿对它是**拒写**而不是补空串。
//
// `risk_level` 与 `approvals.risk_level` 是**同一个事实的两处出口**，必须同值（与 `columnWrites`
// 给 result_code 立的约定同形）。取法不是从 `Draft` 回读：`approval.Service.Request` 会把空的
// RiskLevel 归一成 `"UNKNOWN"`（`internal/approval/approval.go:261`–`:263`），
// 所以权威是**它返回的那一行**的字段，不是调用方塞进去的那个串。
//
// `resource` 可空是真的可空（`approvals.resource` nullable），所以它没有配一份"必填"的校验；
// 四个键恒在场（不走 omitempty），与 ResultPayload 那条"空串表示没有、与键不存在是两回事"同规矩。
type ApprovalRequestedPayload struct {
	ApprovalID string `json:"approval_id"`
	Action     string `json:"action"`
	Resource   string `json:"resource"`
	RiskLevel  string `json:"risk_level"`
}

// ApprovalResolvedPayload 是 §6 那一行 `execution_approval_resolved` 的四键
// （`:217` 现读：`approval_id` `state` `grant_id` `resolved_by`）。
// `grant_id` 可空是真的可空：批了但没开有时限的门时库里就是 NULL（同一件事在 transport 出口是
// `approvalView.GrantID` 的 null，两边都不许编值）。`state`/`resolved_by` 少了这条边就说不清
// "是谁的哪一个决定把它放开的"，所以那两个与 `approval_id` 一起构成这一腿的最低必填。
type ApprovalResolvedPayload struct {
	ApprovalID string `json:"approval_id"`
	State      string `json:"state"`
	GrantID    string `json:"grant_id"`
	ResolvedBy string `json:"resolved_by"`
}

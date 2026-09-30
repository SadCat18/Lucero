package execution

import (
	"encoding/json"
)

// 事件类型 = §6 目录中触发者为 execution 的行。execution_approval_requested /
// execution_approval_resolved 的触发者是 approval（audit 角色），不在本包定义。
const (
	EventCreated       = "execution_created"
	EventStarted       = "execution_started"
	EventOutputFlushed = "execution_output_flushed"
	EventPaused        = "execution_paused"
	EventResumed       = "execution_resumed"
	EventStarting      = "execution_starting" // REQ-DB-4（09-24）：CREATED→STARTING 由它表达，§6 那行 payload 只有 `from`
	EventSucceeded     = "execution_succeeded"
	EventFailed        = "execution_failed"
	EventCancelled     = "execution_cancelled"
	EventInterrupted   = "execution_interrupted"
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
// （WAITING_APPROVAL→RUNNING 由 approval 包发 execution_approval_resolved；
// RUNNING→WAITING_INPUT 在 §6 目录里根本没有事件）。
// 事件存在与否与"是否驱动 Task"是两件事：§5.3 之外的事件一律不驱动 Task。
func EventOf(from, to State) string {
	switch to {
	case Starting:
		return EventStarting
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
		case Paused, WaitingInput, Interrupted:
			return EventResumed
		}
	}
	return ""
}

// auditRequired = §6 目录的 Audit 列 ✓：这三个事件必须同事务带一条 audit_entries。
// `EventUnknown` 在列里是契约① :717 那句「Audit 必带」的落点（与 `execution_interrupted` 同待遇）；
// §6 那一行由主控在同批补，**这一张表先于表**是本批的形状，不是越界登记 —— 登记（写 §6）与
// 消费（这里要不要审计）是两件事，后者归执行落地的那一方，前者归 specs。
//
// `EventClaimed` **不在**这张表里，而且不许补进去：:741 那行的 Audit 列是 `–`（一条把手的观察不是
// 一次需要证据的裁决）。它要是被补进来，claim.go 那一笔就会去要一条没人给的 audit 行、当场写不下去。
var auditRequired = map[string]bool{EventFailed: true, EventInterrupted: true, EventUnknown: true}

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

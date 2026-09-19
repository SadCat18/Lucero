# Lucero
## Core Domain Model + SQLite Schema v0.1

**状态：Frozen for Phase 1 implementation**  
**核心后端：Go**  
**数据层：SQLite / Local First**  
**架构原则：稳定性 > 核心功能完整度 > 使用体验 > 扩展功能**

---

# 1. 本版目标

v0.1 只冻结 Phase 1 真正需要稳定存在的核心领域模型：

- Project
- Adapter
- Adapter Capability
- Model
- Session
- Message
- Task
- Execution
- Task ↔ Session Link
- Handoff
- Approval
- Temporary Grant
- Quota
- Event Journal
- Audit
- Notification
- Blob / Attachment
- Process Runtime
- Execution Lease
- Idempotency

以下能力在架构上预留，但不要求进入首个数据库迁移：

- 完整 Workflow DAG
- Git Worktree 管理
- Browser Automation
- SSH / Server
- Database Center
- Deploy Center
- Remote Device
- Plugin Marketplace
- Semantic Vector Store
- Panel AI Memory
- Task Result Card（Phase 2；v0.1 先由 Task/Execution/Event 保留生成它所需的全部事实）

这些能力后续使用新的 migration 增加，不修改已冻结实体语义。

---

# 2. 核心建模原则

## 2.1 Task 不属于某一家 AI 软件

Task 表示用户真正想完成的目标。

例如：

> 修复登录接口超时问题。

这个 Task 可以先由 Codex 执行，额度不足后转给 Qoder，再由 OpenCode 做验证。

因此：

```text
Task
 ├─ Execution #1 → Codex
 ├─ Execution #2 → Qoder
 └─ Execution #3 → OpenCode
```

Provider 可以失败、切换、恢复。

Task 不能因此消失。

---

## 2.2 Task 与 Execution 必须分离

### Task

描述：

- 用户目标
- 优先级
- 当前总体状态
- 是否项目任务
- 是否最终验收

### Execution

描述：

- 某一次真实执行
- 使用哪个 Adapter
- 使用哪个 Session
- 使用哪个 Model
- 使用什么参数
- 为什么开始
- 为什么结束
- 消耗多少
- 是否发生错误

一个 Task 可以拥有多个 Execution。

---

# 3. ID 规范

所有跨模块核心实体统一使用：

**UUIDv7**

SQLite 中保存为：

```text
TEXT
```

原因：

- 全局唯一
- 可在多个进程安全生成
- 大体按时间有序
- 方便未来 API / PWA / Plugin
- 不暴露数据库内部自增 ID

Event Journal / Audit 可额外使用 SQLite INTEGER sequence 保证严格本地顺序。

---

# 4. 时间规范

所有持久化时间统一保存：

```text
Unix Time Milliseconds
```

类型：

```text
INTEGER
```

数据库只保存 UTC 时间点。

UI 根据用户时区格式化。

不在数据库保存本地时间字符串。

---

# 5. JSON 规范

可变 Provider 数据使用 JSON TEXT，例如：

- 原始状态
- 模型能力
- 模型参数 Schema
- Adapter 元数据
- Provider 原始额度
- Error Details

原则：

> Normalized data 与 Provider raw data 同时保留。

例如：

```text
normalized_status = RUNNING
provider_status_raw = {"state":"thinking"}
```

不能因为统一抽象而丢失原始状态。

---

# 6. Aggregate Root 划分

v0.1 主要 Aggregate：

```text
Project
AdapterInstance
Session
Task
Execution
Approval
QuotaAccount
Notification
```

Event Journal 与 Audit 是跨 Aggregate 基础设施。

---

# 7. Project

Project 是项目级上下文容器。

核心字段：

```text
Project
├─ id
├─ name
├─ canonical_path
├─ project_type
├─ trust_domain_id
├─ status
├─ metadata_json
├─ created_at
└─ updated_at
```

普通聊天任务允许：

```text
project_id = NULL
```

## Project Path

由于同一项目可能被不同软件识别成不同路径表示：

```text
D:\MZO
D:/MZO
\\?\D:\MZO
```

需要 `project_paths` 保存：

- primary
- alias
- detected

Core 启动时统一 canonicalize。

---

# 8. Trust Domain

虽然复杂策略后续实现，但 Project 从 v0.1 就预留 Trust Domain。

例如：

```text
个人项目
公司项目
实验项目
生产系统
```

用于未来：

- 项目隔离
- Sensitive Data Policy
- Remote Policy
- Deployment Policy

---

# 9. Adapter Instance

一个 Adapter 表示某个 AI 软件接入实例。

例如：

```text
codex
qoder
opencode
zcode
workbuddy
```

Adapter 本身不是 Model。

结构：

```text
AdapterInstance
├─ id
├─ adapter_key
├─ display_name
├─ adapter_version
├─ protocol_version
├─ enabled
├─ health
├─ effective_backend
├─ software_version
├─ last_heartbeat_at
├─ last_healthcheck_at
└─ metadata_json
```

health：

```text
UNKNOWN
STARTING
HEALTHY
DEGRADED
UNAVAILABLE
STOPPED
```

---

# 10. Adapter Capability

每一个 Adapter 必须声明自己实际支持什么。

例如：

```text
session.list
session.read
session.send
session.stop

model.list
model.select
model.reasoning

quota.read

approval.read
approval.respond
```

每项能力单独记录：

```text
support_level
effective_backend
health
last_verified_at
metadata_json
```

support_level：

```text
NATIVE
COMPATIBLE
CLI
LOCAL_DATA
UI_AUTOMATION
READ_ONLY
EXPERIMENTAL
UNSUPPORTED
```

这样 UI 可以准确告诉用户：

> 当前“读取额度”是 Native，但“跳转原客户端会话”是 UI Automation。

---

# 11. Model Registry

每一个实际模型属于某个 Adapter。

统一 ID：

```text
adapter:model
```

数据库中仍使用独立 UUID 主键。

例如：

```text
adapter_instance = Codex
remote_model_id = gpt-x
canonical_key = codex:gpt-x
```

保存：

- display name
- capabilities
- parameter schema
- availability
- provider raw data

Execution 启动时必须将实际模型快照写入 Execution，避免之后模型列表变化导致历史不可解释。

---

# 12. Session

Session 表示一个真实可持续对话上下文。

一个 Session 通常对应：

> 某一家原软件中的一个真实已有会话。

结构：

```text
Session
├─ id
├─ adapter_instance_id
├─ project_id?
├─ remote_session_id?
├─ title
├─ managed
├─ sync_state
├─ sync_cursor
├─ remote_revision
├─ provider_status_raw
├─ last_message_at
├─ last_synced_at
└─ metadata_json
```

## managed

```text
0 = 普通扫描到的历史会话
1 = 受管理会话
```

只有 Managed Session 持续启用：

- 双向同步
- 状态监控
- 跨软件通信
- 通知
- 额度关联

---

# 13. Session Sync State

建议：

```text
UNKNOWN
SYNCED
SYNCING
LOCAL_AHEAD
REMOTE_AHEAD
CONFLICT
DEGRADED
UNAVAILABLE
```

如果 Adapter 无法确认真实状态：

必须使用：

```text
UNKNOWN
```

不能猜测。

---

# 14. Message

Message 属于 Session。

字段：

```text
Message
├─ id
├─ session_id
├─ remote_message_id?
├─ role
├─ kind
├─ sequence_no
├─ text_content
├─ content_json
├─ provider_raw_json
├─ created_at
└─ synced_at
```

role：

```text
USER
ASSISTANT
SYSTEM
TOOL
```

kind：

```text
MESSAGE
TOOL_CALL
TOOL_RESULT
STATUS
SUMMARY
```

`content_json` 为未来多模态消息预留。

---

# 15. 流式输出

不建议将每一个 token 写 Event Journal。

否则：

- Event 表增长过快
- 恢复成本高
- Audit 噪音巨大

v0.1 使用：

```text
execution_output_chunks
```

按批次持久化流式输出。

例如每：

```text
250-500ms
```

或达到一定字符数后 flush。

Execution 完成后：

可以将 Chunk compact 成正式 Message。

这样即使 Core Crash，也不会丢失整个正在生成的回复。

---

# 16. Task

Task 是系统一级实体。

字段：

```text
Task
├─ id
├─ project_id?
├─ parent_task_id?
├─ title
├─ description
├─ task_type
├─ mode
├─ priority
├─ status
├─ risk_level
├─ requested_by
├─ budget_json
├─ policy_snapshot_json
├─ created_at
├─ updated_at
├─ accepted_at?
└─ metadata_json
```

---

# 17. Task State Machine

v0.1 冻结：

```text
CREATED
QUEUED
PREPARING
RUNNING
WAITING_APPROVAL
WAITING_INPUT
BLOCKED
PAUSED
TRANSFERRING
AGENT_COMPLETED
VERIFYING
WAITING_ACCEPTANCE
ACCEPTED
FAILED
CANCELLED
INTERRUPTED
UNKNOWN
```

重要：

```text
AGENT_COMPLETED != ACCEPTED
```

AI 自己说完成，只能进入：

```text
AGENT_COMPLETED
```

最终验收才进入：

```text
ACCEPTED
```

---

# 18. Task Priority

```text
LOW
NORMAL
HIGH
CRITICAL
```

数据库不将 priority 直接作为执行顺序。

真正 Scheduler 还需要考虑：

- dependency
- provider concurrency
- project concurrency
- user pause
- quota
- risk

---

# 19. Task Dependency

v0.1 预留：

```text
task_dependencies
```

关系类型：

```text
BLOCKS
WAITS_FOR
USES_RESULT
RELATED
```

用于后续：

```text
Task B 等 Task A
Task C 读取 A 的 Result Card
```

---

# 20. Task ↔ Session

一个 Task 可能：

- 起源于 Codex Session A
- 后续转到 Qoder Session B
- 再让 OpenCode Session C Review

因此使用：

```text
task_session_links
```

而不是在 Task 上只放一个 session_id。

role：

```text
ORIGIN
EXECUTION
HANDOFF
REVIEW
COLLAB
REFERENCE
```

---

# 21. Execution

Execution 是系统恢复能力最关键的实体。

字段：

```text
Execution
├─ id
├─ task_id
├─ adapter_instance_id
├─ session_id?
├─ model_id?
├─ predecessor_execution_id?
├─ attempt_no
├─ status
├─ trigger_type
├─ model_snapshot_json
├─ model_config_json
├─ permission_snapshot_json
├─ environment_snapshot_json
├─ started_at
├─ heartbeat_at
├─ finished_at
├─ result_code
├─ error_code
├─ error_message
├─ error_details_json
├─ provider_execution_id
└─ metadata_json
```

---

# 22. Execution State

```text
CREATED
STARTING
RUNNING
WAITING_APPROVAL
WAITING_INPUT
PAUSED
TRANSFERRING
SUCCEEDED
FAILED
CANCELLED
INTERRUPTED
UNKNOWN
```

Task State 与 Execution State 不完全相同。

例如：

```text
Execution #1 = FAILED / QUOTA_EXHAUSTED
```

Task 可以进入：

```text
TRANSFERRING
```

随后创建：

```text
Execution #2
```

---

# 23. Execution Trigger Type

记录为什么出现这次执行：

```text
INITIAL
RETRY
HANDOFF
RESUME
MANUAL_SWITCH
AUTO_SWITCH
REVIEW
RECOVERY
```

这对后续时间线和额度分析非常重要。

---

# 24. Execution Lease

为避免 Core Crash / 多 Runner 导致同一个 Execution 重复执行：

使用：

```text
execution_leases
```

字段：

```text
execution_id
owner_id
acquired_at
heartbeat_at
expires_at
```

Runner 只有持有有效 Lease 才能执行。

Lease 过期后由 Recovery Engine 判断：

- reconnect
- resume
- interrupt
- manual decision

---

# 25. Handoff

跨软件转交是一级实体。

```text
Handoff
├─ id
├─ task_id
├─ source_execution_id
├─ target_adapter_instance_id
├─ target_session_id?
├─ target_execution_id?
├─ reason
├─ state
├─ package_blob_id?
├─ package_json
├─ created_at
└─ completed_at
```

state：

```text
CREATED
PREPARING
READY
STARTING_TARGET
COMPLETED
FAILED
CANCELLED
```

Handoff Package 不能只存在 Prompt 中。

必须结构化持久化。

---

# 26. Approval

所有需要用户决策的危险操作统一变成 Approval。

不能由 Adapter 自己弹一个 Core 不知道的审批状态。

字段：

```text
Approval
├─ id
├─ task_id?
├─ execution_id?
├─ session_id?
├─ adapter_instance_id?
├─ type
├─ risk_level
├─ action
├─ resource
├─ request_json
├─ state
├─ expires_at
├─ requested_at
├─ resolved_at
├─ resolved_by
└─ resolution_json
```

state：

```text
PENDING
APPROVED
REJECTED
EXPIRED
CANCELLED
```

---

# 27. Temporary Grant

用户点击：

> 当前任务期间允许完全访问

不能只改变 UI 状态。

必须生成：

```text
TemporaryGrant
```

例如：

```text
scope_type = TASK
scope_id = ...
action = filesystem.write
expires_at = task end
```

所有权限检查均经过 Permission Engine。

---

# 28. Principal

虽然系统目前只有一个人使用，但 Audit 和 API 必须知道：

> 谁做了操作。

统一使用 Principal：

```text
USER
SYSTEM
ADAPTER
PLUGIN
DEVICE
API_TOKEN
```

Phase 1 至少创建：

```text
local-user
system
```

Adapter 进程也可以对应 Principal。

---

# 29. Permission Policy

复杂 Policy Engine 后续实现。

v0.1 先冻结数据结构：

```text
policy_rules
```

支持：

- ALLOW
- DENY
- REQUIRE_APPROVAL

Scope：

```text
GLOBAL
TRUST_DOMAIN
PROJECT
TASK_MODE
TASK
SESSION
ADAPTER
MODEL
```

规则本身使用：

```text
condition_json
```

后续可以升级表达式系统，而不改表结构。

---

# 30. Quota Account

“额度”不能只绑定软件名称。

因为：

- 同一软件可能不同账号
- 两个软件可能共享同一 Provider 额度
- 同一个账号存在多种窗口额度

使用：

```text
quota_accounts
```

保存：

- adapter
- external account reference
- display label
- quota source
- metadata

不保存账号密码。

---

# 31. Quota Snapshot

每次获取额度生成 Snapshot。

例如：

```text
5H_WINDOW
WEEKLY
MONTHLY
CREDITS
TOKENS
REQUESTS
CURRENCY
CUSTOM
```

字段：

```text
raw_value_json
used_value
remaining_value
percentage_used
percentage_remaining
window_start
window_end
reset_at
observed_at
source
confidence
```

必须区分：

```text
OBSERVED
ESTIMATED
```

预测不能写进 Observed Snapshot。

---

# 32. Event Journal

所有重要 Domain State Change：

必须产生 Durable Event。

表：

```text
event_journal
```

关键字段：

```text
seq
event_id
event_type
aggregate_type
aggregate_id
aggregate_version
correlation_id
causation_id
principal_id
payload_json
created_at
```

`seq` 使用：

```text
INTEGER PRIMARY KEY AUTOINCREMENT
```

作为本机严格顺序。

---

# 33. Aggregate Version

每个 Aggregate Event 应拥有：

```text
aggregate_version
```

并建立唯一约束：

```text
aggregate_type
aggregate_id
aggregate_version
```

防止同一个版本重复写入。

---

# 34. Correlation / Causation

例如：

```text
User Command
    ↓
TaskCreated
    ↓
ExecutionStarted
    ↓
ApprovalRequested
    ↓
ApprovalApproved
    ↓
ExecutionResumed
```

整条链：

共享：

```text
correlation_id
```

每个事件记录：

```text
causation_id
```

用于构建真正可追踪时间线。

---

# 35. Audit

Audit 与 Event Journal 分开。

Event：

> 系统发生了什么。

Audit：

> 谁以什么权限执行了什么敏感行为。

高风险内容进入：

```text
audit_entries
```

使用：

```text
previous_hash
entry_hash
signature
```

形成 Tamper Evident Chain。

---

# 36. Notification

Notification 是持久化实体。

不是纯 Windows Toast。

字段：

```text
type
priority
state
title
body
actions_json
project_id
task_id
approval_id
created_at
read_at
handled_at
```

state：

```text
UNREAD
READ
PENDING
HANDLED
IGNORED
EXPIRED
```

---

# 37. Blob

大对象统一通过 Blob Store。

数据库只存 metadata。

```text
Blob
├─ id
├─ storage_class
├─ relative_path
├─ sha256
├─ size_bytes
├─ mime_type
├─ created_at
└─ archived_at
```

storage_class：

```text
HOT
COLD
CACHE
```

---

# 38. Attachment

Attachment 是业务层附件引用。

可关联：

- Session
- Message
- Task
- Project

v0.1 使用独立 link tables 避免 generic FK 失去数据库完整性。

---

# 39. Process Runtime

Core 管理的外部进程统一记录：

```text
process_instances
```

类型：

```text
ADAPTER
TERMINAL
WORKER
PROJECT_SERVICE
BROWSER
SANDBOX
```

字段包括：

```text
pid
owner_type
owner_id
status
restart_policy
started_at
heartbeat_at
exited_at
exit_code
```

Crash Recovery 使用它判断：

> 重启前到底运行过什么。

---

# 40. Idempotency

以下行为必须支持 Idempotency Key：

- Create Task
- Send Message
- Resolve Approval
- Create Handoff
- Deploy（以后）
- DB Write（以后）
- Workflow Step（以后）

表：

```text
idempotency_keys
```

记录：

```text
key
operation
request_hash
response_json
state
expires_at
```

避免 UI 重试、网络重连、Core 恢复导致高风险动作重复执行。

---

# 41. Entity Relationship Overview

```text
TrustDomain
    │
    └── Project
          │
          ├── Task ────────────────┐
          │    │                    │
          │    ├── Execution       │
          │    │      │             │
          │    │      ├── Approval  │
          │    │      ├── Output    │
          │    │      └── Lease     │
          │    │                    │
          │    ├── Handoff          │
          │    └── Result Card      │
          │                         │
          └── Session ──────────────┘
                │
                └── Message

AdapterInstance
    │
    ├── AdapterCapability
    ├── Model
    ├── Session
    ├── Execution
    └── QuotaAccount
            │
            └── QuotaSnapshot

All Domain Changes
    ↓
EventJournal

Sensitive Operations
    ↓
AuditEntry
```

---

# 42. SQLite 连接策略

所有数据库连接启动后执行：

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
PRAGMA busy_timeout = 5000;
```

稳定性优先，因此 v0.1 默认：

```text
synchronous = FULL
```

后续实测性能不足再讨论 NORMAL。

---

# 43. Go DB 并发建议

不要让几十个 goroutine 无组织同时争写 SQLite。

推荐：

```text
Read Pool
+
Serialized Write Path
```

业务写操作尽量通过 Transaction Boundary。

原则：

```text
修改 Aggregate
+
写 Event Journal
```

必须在同一个 SQLite Transaction 中完成。

---

# 44. Transaction Pattern

例如 Task 状态从 RUNNING 变为 WAITING_APPROVAL：

```text
BEGIN

UPDATE tasks

INSERT approvals

INSERT event_journal

COMMIT
```

只有 Commit 成功：

才向内存 Event Bus / WebSocket 发布。

这样 UI 永远不会看到数据库中不存在的状态。

---

# 45. Repository Boundary

每个模块维护自己的 Repository。

例如：

```text
task.Repository
session.Repository
quota.Repository
approval.Repository
```

禁止出现：

```text
GlobalDatabaseRepository
```

让所有模块随意查询所有表。

跨模块访问通过 Service API。

---

# 46. Go Domain Interface Example

Task Service 建议：

```go
type Service interface {
    Create(ctx context.Context, cmd CreateTaskCommand) (*Task, error)
    Queue(ctx context.Context, taskID string) error
    Pause(ctx context.Context, taskID string, reason string) error
    Resume(ctx context.Context, taskID string) error
    Cancel(ctx context.Context, taskID string, reason string) error
    Accept(ctx context.Context, taskID string) error
    Transfer(ctx context.Context, cmd TransferTaskCommand) (*Handoff, error)
}
```

Execution Supervisor：

```go
type Supervisor interface {
    Start(ctx context.Context, executionID string) error
    Cancel(ctx context.Context, executionID string) error
    Recover(ctx context.Context) error
}
```

---

# 47. Adapter Interface Example

Core 不直接引用 Codex SDK。

统一：

```go
type Adapter interface {
    Handshake(ctx context.Context) (Manifest, error)
    Health(ctx context.Context) (Health, error)

    ListSessions(ctx context.Context, req ListSessionsRequest) ([]SessionRef, error)
    ReadSession(ctx context.Context, req ReadSessionRequest) (SessionSnapshot, error)
    SendMessage(ctx context.Context, req SendMessageRequest) (MessageStream, error)
    Cancel(ctx context.Context, req CancelRequest) error

    ListModels(ctx context.Context) ([]ModelDescriptor, error)
    ReadQuota(ctx context.Context) ([]QuotaObservation, error)
}
```

实际 Adapter 可以运行在独立进程。

Go Core 中的接口是 RPC Client abstraction。

---

# 48. Event Publishing Rule

任何模块不得：

```text
先发 WebSocket
后写数据库
```

必须：

```text
DB Transaction
↓
Durable Event
↓
Commit
↓
Publish
```

---

# 49. 状态置信度

对于外部软件状态，统一增加：

```text
confidence
```

例如：

```text
HIGH
MEDIUM
LOW
UNKNOWN
```

如果只是根据 Windows Process 推断：

不能标 HIGH。

如果来自官方实时接口：

可以 HIGH。

---

# 50. Provider Raw Preservation

以下数据建议全部保留 raw JSON：

- Provider status
- Provider error
- Quota response
- Model metadata
- Approval metadata
- Session metadata

这样软件升级后：

即使旧 Parser 不认识新字段，也不会丢掉原始证据。

---

# 51. Migration Strategy

使用：

```text
000001_core_schema.sql
000002_...
000003_...
```

每次 migration：

- 不修改已发布 migration
- 新变化只增加新 migration
- 升级前自动创建 SQLite backup
- Migration failure 立即停止 Core Ready

---

# 52. Phase 1 Schema Freeze

以下表进入 `000001_core_schema.sql`：

```text
schema_migrations
principals
trust_domains
projects
project_paths

adapter_instances
adapter_capabilities
models

sessions
messages

tasks
task_dependencies
task_session_links

executions
execution_leases
execution_output_chunks
handoffs

approvals
temporary_grants
policy_rules

quota_accounts
quota_snapshots

blobs
attachments
message_attachments
task_attachments
project_attachments

process_instances

notifications

event_journal
audit_entries

idempotency_keys

settings
```

---

# 53. Phase 1 暂不建表

暂缓到新 migration：

```text
workflows
workflow_nodes
workflow_edges

worktrees

terminal_sessions
browser_sessions
ssh_hosts
remote_project_mappings

database_connections
deployment_definitions

devices
api_tokens

knowledge_documents
vector_embeddings
```

原因：

这些实体的语义还会在对应模块设计时继续细化。

但所有基础 ID、Event、Blob、Permission、Audit 机制已经足够支撑后续平滑增加。

---

# 54. 关键唯一约束

必须至少保证：

```text
adapter_instances.adapter_key UNIQUE

models(adapter_instance_id, remote_model_id) UNIQUE

sessions(adapter_instance_id, remote_session_id)
    在 remote_session_id 非 NULL 时唯一

messages(session_id, remote_message_id)
    在 remote_message_id 非 NULL 时唯一

event_journal(event_id) UNIQUE

event_journal(
    aggregate_type,
    aggregate_id,
    aggregate_version
) UNIQUE

execution_leases(execution_id) UNIQUE

quota_accounts(adapter_instance_id, external_account_ref)
    在 external_account_ref 非 NULL 时唯一
```

---

# 55. Delete Strategy

核心业务数据默认：

**不物理级联删除。**

Project / Session / Task 后续使用：

```text
archived_at
deleted_at
```

软删除策略。

v0.1 对关键表不使用：

```text
ON DELETE CASCADE
```

去删除 Task / Session 历史。

附件 link 可 cascade。

---

# 56. Crash Recovery Query

Core 启动时重点扫描：

```sql
SELECT *
FROM executions
WHERE status IN (
    'STARTING',
    'RUNNING',
    'WAITING_APPROVAL',
    'WAITING_INPUT',
    'TRANSFERRING'
);
```

再结合：

```text
execution_leases
process_instances
adapter health
provider execution id
```

判断恢复动作。

---

# 57. UNKNOWN 是合法状态

所有外部同步实体必须允许：

```text
UNKNOWN
```

例如：

- Session 状态未知
- Adapter 能力未知
- Execution 恢复状态未知
- Quota 未知

UI 不能把 Unknown 转成：

```text
0
Offline
Failed
```

除非确实有证据。

---

# 58. Audit 与 Secret

禁止以下内容写入：

- event payload
- normal logs
- audit payload
- error_message

中的明文：

```text
Password
API Key
Private Key
Cookie
Token
```

进入持久化前先经过：

```text
Redaction Layer
```

Audit 可以记录：

```text
secret_ref = vault://...
```

不能记录 secret value。

---

# 59. 数据生命周期

v0.1 先保留字段：

```text
archived_at
```

Blob 支持 HOT/COLD。

未来 Retention Engine 根据项目规则执行：

- Archive
- Compact
- Delete Cache
- Preserve Audit

---

# 60. 推荐的 Go 模块对应关系

```text
internal/
├─ project/
├─ adapter/
├─ model/
├─ session/
├─ task/
├─ execution/
├─ approval/
├─ permission/
├─ quota/
├─ handoff/
├─ notification/
├─ audit/
├─ eventjournal/
├─ blob/
├─ process/
├─ recovery/
└─ settings/
```

`task` 与 `execution` 建议拆成两个包。

因为 Execution 生命周期以后会越来越复杂。

---

# 61. 第一条实现链

数据库建好后不要立刻做真实 Codex Adapter。

先实现：

```text
Fake Adapter
```

然后跑通：

```text
Create Project
↓
Discover Fake Session
↓
Create Task
↓
Create Execution
↓
Send Message
↓
Persist Stream Chunks
↓
Approval Requested
↓
Approval Resolved
↓
Execution Completed
↓
Task → AGENT_COMPLETED
↓
User Accept
↓
Task → ACCEPTED
```

同时检查：

```text
Event Journal
Audit
Notification
Crash Recovery
```

全部是否正确。

---

# 62. v0.1 验收标准

Core Domain Model + Schema 可以认为合格，必须满足：

1. 一个 Task 可以跨多个 Execution。
2. 一个 Task 可以关联多个不同软件 Session。
3. Provider 切换不会产生新的 Task。
4. Execution Crash 不会让 Task 消失。
5. Adapter Crash 不会破坏数据库。
6. Approval 可以恢复。
7. Stream 输出 Crash 后可找回最近持久化内容。
8. 所有核心状态变化有 Durable Event。
9. 所有高风险操作可进入 Audit。
10. Quota Unknown 与 0 明确区分。
11. Provider raw data 被保留。
12. 同一高风险请求不能因重试被重复执行。
13. UI 不参与业务真相判断。
14. Phase 2 新功能可以通过新 Migration 增加而无需推翻核心表。

---

# 63. v0.1 冻结结论

本版正式冻结四个最关键概念：

## Task

用户真正想完成的目标。

## Execution

某个 Provider/Model 对 Task 的一次真实执行。

## Session

某个 AI 软件中持续存在的真实上下文。

## Event

系统状态发生变化的持久化事实。

四者关系：

```text
Session
   │
   │ used by
   ▼
Execution
   │
   │ executes
   ▼
Task
   │
   │ changes generate
   ▼
Event Journal
```

最终原则：

> **Task 属于统一工作台。**
>
> **Session 属于对应软件。**
>
> **Execution 是二者之间的一次连接。**
>
> **Event Journal 记录事实。**
>
> **Audit 记录敏感行为责任链。**

这是后续整个系统能够跨 Codex、Qoder、OpenCode、Z Code、WorkBuddy 继续扩展而不推倒重来的基础。

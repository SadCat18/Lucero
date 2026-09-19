# Lucero
## Core Domain Model + SQLite Schema v0.1

**状态：Frozen for Phase 1 implementation**  
**核心后端：Go**  
**数据层：SQLite / Local First**  
**架构原则：稳定性 > 核心功能完整度 > 使用体验 > 扩展功能**

---

# 1. 本版目标

v0.1 冻结 Phase 1 必须稳定存在的核心领域模型：

- Project
- Adapter / Adapter Capability
- Model
- Session / Message
- Task / Task Dependency / Task ↔ Session Link
- Execution / Execution Lease
- Handoff
- Approval / Temporary Grant / Policy Rule
- Quota Account / Quota Snapshot
- Event Journal
- Audit
- Notification
- Blob / Attachment
- Process Runtime
- Idempotency

暂缓到后续 migration 的能力：

- Workflow DAG
- Git Worktree
- Browser Automation
- SSH / Server
- Database Center
- Deploy Center
- Remote Device
- Plugin Marketplace
- Semantic Vector Store
- Panel AI Memory
- Task Result Card

这些能力后续通过新的 migration 增加，不修改已冻结实体语义。

---

# 2. 核心建模原则

## 2.1 Task 不属于某一家 AI 软件

Task 表示用户真正想完成的目标。

例如：

> 修复登录接口超时问题。

同一个 Task 可以先由 Codex 执行，额度不足后转给 Qoder，再由 OpenCode 做验证。

```text
Task
 ├─ Execution #1 → Codex
 ├─ Execution #2 → Qoder
 └─ Execution #3 → OpenCode
```

Provider 可以失败、切换、恢复。

Task 不能因此消失。

## 2.2 Task 与 Execution 必须分离

Task 描述：
- 用户目标
- 优先级
- 当前总体状态
- 是否项目任务
- 是否最终验收

Execution 描述：
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

# 3. ID 与时间规范

所有跨模块核心实体统一使用 **UUIDv7**，SQLite 保存为 TEXT。

Event Journal / Audit 使用 SQLite INTEGER sequence 保证严格本地顺序。

所有持久化时间统一保存为 Unix Time Milliseconds（UTC），UI 再按用户时区格式化。

---

# 4. Provider Raw Preservation

统一抽象时必须同时保留原厂数据。

例如：

```text
normalized_status = RUNNING
provider_status_raw = {"state":"thinking"}
```

以下数据建议保留 raw JSON：

- Provider status
- Provider error
- Quota response
- Model metadata
- Approval metadata
- Session metadata

软件升级后，即使当前 Parser 不认识新字段，也不能丢掉原始证据。

---

# 5. Aggregate Root

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

Event Journal 与 Audit 为跨 Aggregate 基础设施。

---

# 6. Project

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

普通聊天任务允许 `project_id = NULL`。

不同 Provider 可能产生不同路径表示，因此使用 `project_paths` 保存 primary / alias / detected path，由 Core canonicalize。

---

# 7. Trust Domain

Project 从 v0.1 就预留 Trust Domain，例如：

```text
个人项目
公司项目
实验项目
生产系统
```

用于未来项目隔离、敏感信息、远程和部署策略。

---

# 8. Adapter Instance

一个 Adapter 表示某个 AI 软件接入实例，例如：

```text
codex
qoder
opencode
zcode
workbuddy
```

Adapter 本身不是 Model。

Health：

```text
UNKNOWN
STARTING
HEALTHY
DEGRADED
UNAVAILABLE
STOPPED
```

---

# 9. Adapter Capability

每个 Adapter 必须声明实际支持能力，例如：

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

Support Level：

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

UI 必须能告诉用户当前某项能力到底通过哪一层实现。

---

# 10. Model Registry

每一个实际模型属于某个 Adapter。

Canonical Key 建议：

```text
adapter:model
```

例如：

```text
codex:gpt-x
qoder:model-y
ollama:qwen-x
```

Execution 启动时必须保存实际模型快照，避免以后模型列表变化导致历史不可解释。

---

# 11. Session

Session 表示 Provider 中真实持续存在的上下文。

关键字段：

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

Managed Session 才持续启用：
- 双向同步
- 状态监控
- 跨软件通信
- 通知
- 额度关联

Sync State：

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

Adapter 无法确认状态时必须使用 UNKNOWN，不能猜。

---

# 12. Message

Message 属于 Session。

Role：

```text
USER
ASSISTANT
SYSTEM
TOOL
```

Kind：

```text
MESSAGE
TOOL_CALL
TOOL_RESULT
STATUS
SUMMARY
```

`content_json` 为未来多模态消息预留。

---

# 13. 流式输出

不把每一个 Token 写 Event Journal。

v0.1 使用 `execution_output_chunks`，按批次持久化流式输出，例如每 250–500ms 或达到阈值后 flush。

Execution 完成后可把 chunks compact 成正式 Message。

这样 Core Crash 时不会丢失整个正在生成的回复。

---

# 14. Task

Task 是系统一级实体。

关键字段：

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

Priority：

```text
LOW
NORMAL
HIGH
CRITICAL
```

真正 Scheduler 还要考虑 dependency、Provider/Project concurrency、quota、risk 等。

---

# 15. Task State Machine

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

关键原则：

```text
AGENT_COMPLETED != ACCEPTED
```

AI 自己说完成，只能进入 AGENT_COMPLETED。最终验收后才能进入 ACCEPTED。

---

# 16. Task Dependency

v0.1 预留 `task_dependencies`：

```text
BLOCKS
WAITS_FOR
USES_RESULT
RELATED
```

---

# 17. Task ↔ Session

一个 Task 可能起源于 Codex Session A，后续转到 Qoder Session B，再由 OpenCode Session C Review。

因此使用 `task_session_links`，而不是 Task 上只放一个 session_id。

Role：

```text
ORIGIN
EXECUTION
HANDOFF
REVIEW
COLLAB
REFERENCE
```

---

# 18. Execution

Execution 是恢复能力最关键的实体。

关键字段：

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

Execution State：

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

Trigger Type：

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

---

# 19. Execution Lease

为避免 Core Crash / 多 Runner 导致同一个 Execution 重复执行，使用 `execution_leases`。

Runner 只有持有有效 Lease 才能执行。

Lease 过期后由 Recovery Engine 判断：
- reconnect
- resume
- interrupt
- manual decision

---

# 20. Handoff

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

State：

```text
CREATED
PREPARING
READY
STARTING_TARGET
COMPLETED
FAILED
CANCELLED
```

Handoff Package 必须结构化持久化，不能只存在于 Prompt。

---

# 21. Approval

所有需要用户决策的危险操作统一变成 Approval。

State：

```text
PENDING
APPROVED
REJECTED
EXPIRED
CANCELLED
```

Approval 可关联 Task / Execution / Session / Adapter。

---

# 22. Temporary Grant

例如用户选择：

> 当前任务期间允许完全访问

必须生成 TemporaryGrant，而不是只改变 UI 状态。

支持 scope、action、resource、constraints、expires_at。

所有权限检查统一经过 Permission Engine。

---

# 23. Principal

虽然是单用户系统，但 Audit/API 必须知道谁做了操作。

统一 Principal Type：

```text
USER
SYSTEM
ADAPTER
PLUGIN
DEVICE
API_TOKEN
```

Phase 1 至少创建 local-user 和 system。

---

# 24. Permission Policy

v0.1 冻结 `policy_rules` 数据结构。

Effect：

```text
ALLOW
DENY
REQUIRE_APPROVAL
```

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

条件使用 JSON，为后续更复杂表达式保留升级空间。

---

# 25. Quota

额度不能只绑定软件名称。

使用 Quota Account + Quota Snapshot。

原因：
- 同一软件可能不同账号
- 两个软件可能共享同一 Provider 额度
- 同一账号可能有多种窗口额度

Quota Snapshot 支持：

```text
PERCENT
CREDITS
TOKENS
REQUESTS
CURRENCY
TIME_WINDOW
CUSTOM
```

事实与预测必须分开：

```text
OBSERVED
ESTIMATED
```

---

# 26. Event Journal

所有重要 Domain State Change 必须产生 Durable Event。

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

`seq` 使用 INTEGER PRIMARY KEY AUTOINCREMENT 作为本机严格顺序。

Aggregate Version 建立唯一约束，防止同一版本 Event 重复写入。

---

# 27. Correlation / Causation

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

整条链共享 correlation_id，每个事件用 causation_id 指向直接原因。

---

# 28. Audit

Event Journal 回答“系统发生了什么”。

Audit 回答“谁以什么权限执行了什么敏感行为”。

高风险内容进入 `audit_entries`。

采用：

```text
previous_hash
entry_hash
signature
```

形成 tamper-evident chain。

---

# 29. Notification

Notification 是持久化实体，不是纯 Windows Toast。

State：

```text
UNREAD
READ
PENDING
HANDLED
IGNORED
EXPIRED
```

可携带可执行 actions。

---

# 30. Blob / Attachment

大对象进入 Blob Store，数据库只保存 metadata。

Storage Class：

```text
HOT
COLD
CACHE
```

Attachment 通过独立 link table 关联 Project / Task / Message，避免 generic FK 失去数据库完整性。

---

# 31. Process Runtime

Core 管理的外部进程统一记录在 `process_instances`。

Process Type：

```text
ADAPTER
TERMINAL
WORKER
PROJECT_SERVICE
BROWSER
SANDBOX
```

Crash Recovery 使用这些记录判断重启前运行过什么。

---

# 32. Idempotency

以下行为必须支持 Idempotency Key：

- Create Task
- Send Message
- Resolve Approval
- Create Handoff
- Deploy（以后）
- DB Write（以后）
- Workflow Step（以后）

用于防止 UI 重试、网络重连和 Core 恢复导致高风险操作重复执行。

---

# 33. Entity Relationship Overview

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
          │    └── Handoff          │
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

# 34. SQLite 策略

所有连接：

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
PRAGMA busy_timeout = 5000;
```

稳定性优先，v0.1 默认 synchronous = FULL。

推荐 Read Pool + Serialized Write Path。

修改 Aggregate 与写 Event Journal 必须在同一个 Transaction 中完成。

例如：

```text
BEGIN
UPDATE tasks
INSERT approvals
INSERT event_journal
COMMIT
```

只有 Commit 成功后才发布到内存 Event Bus / WebSocket。

---

# 35. Repository Boundary

每个业务模块维护自己的 Repository，例如：

```text
task.Repository
session.Repository
quota.Repository
approval.Repository
```

禁止 GlobalDatabaseRepository。

跨模块访问通过 Service API。

---

# 36. Adapter 接口原则

Core 不直接引用 Codex/Qoder 私有 SDK。

统一由 Adapter RPC Client abstraction 对接。

最低能力包括：

```text
Handshake
Health
ListSessions
ReadSession
SendMessage
Cancel
ListModels
ReadQuota
```

---

# 37. 状态置信度

外部状态增加：

```text
HIGH
MEDIUM
LOW
UNKNOWN
```

例如仅根据 Windows Process 推断的状态不能标 HIGH。

---

# 38. Migration Strategy

使用：

```text
000001_core_schema.sql
000002_...
000003_...
```

规则：

- 不修改已发布 migration
- 新变化只能新增 migration
- 升级前自动备份 SQLite
- Migration failure 时 Core 不进入 Ready

---

# 39. Phase 1 Schema Freeze

首个 migration 包含：

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

后续再增加：

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

---

# 40. Delete Strategy

核心业务历史默认不物理级联删除。

Project / Session / Task 使用 archived/deleted 语义。

附件 link 可以 cascade。

---

# 41. Crash Recovery Query

Core 启动重点扫描：

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

再结合 execution_leases、process_instances、adapter health、provider_execution_id 判断恢复动作。

---

# 42. UNKNOWN 是合法状态

外部同步实体必须允许 UNKNOWN。

UI 不能把 UNKNOWN 自动解释成：

```text
0
Offline
Failed
```

没有证据时宁可显示未知。

---

# 43. Audit 与 Secret

禁止在 event payload、normal logs、audit details、error_message 中写入明文：

```text
Password
API Key
Private Key
Cookie
Token
```

持久化前经过 Redaction Layer。

Audit 只记录 secret_ref，例如 `vault://...`。

---

# 44. Go 模块建议

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

Task 与 Execution 拆开，因为 Execution 生命周期后续会明显更复杂。

---

# 45. 第一条实现链

数据库建好后不要立刻实现真实 Provider。

先做 FakeAdapter：

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

同时验证 Event Journal、Audit、Notification、Crash Recovery。

---

# 46. v0.1 验收标准

必须满足：

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
14. Phase 2 新功能可通过新 Migration 增加而无需推翻核心表。

---

# 47. v0.1 冻结结论

四个最关键概念：

## Task
用户真正想完成的目标。

## Execution
某个 Provider/Model 对 Task 的一次真实执行。

## Session
某个 AI 软件中持续存在的真实上下文。

## Event
系统状态变化的持久化事实。

关系：

```text
Session
   │ used by
   ▼
Execution
   │ executes
   ▼
Task
   │ changes generate
   ▼
Event Journal
```

最终原则：

> Task 属于 Lucero。  
> Session 属于对应软件。  
> Execution 是二者之间的一次连接。  
> Event Journal 记录事实。  
> Audit 记录敏感行为责任链。

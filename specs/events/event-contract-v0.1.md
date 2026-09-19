# Event Contract v0.1（Wave-1 契约①）

- 状态: **accepted**（用户 2026-09-19 批准，含把事件命名定为小写下划线这一处对交接示意图的偏离）
- 适用范围: `event_journal` 的全部写入方，以及 `/ws/v1/events` 的事件帧格式
- 上游依据: ADR-008 Durable Event Journal、领域模型「Transaction Pattern」「Event Publishing Rule」
  「Aggregate Version」「Correlation / Causation」「UNKNOWN 是合法状态」
- 缺口来源: 交接包**没有**定义事件类型命名与 `aggregate_version` 的存储位置，本契约补齐

## 1. 命名规范

```
<domain>_<snake_case_action>          例：task_created / execution_started
```

- 全小写 + 下划线。**不用** `PascalCase`（交接主文档的时间线示意图用 `TaskCreated` 只是行文示意，不是协议）。
- `domain` 取自固定词表：
  `project` `adapter` `model` `session` `message` `task` `execution` `output` `approval`
  `grant` `policy` `quota` `blob` `attachment` `process` `notification` `handoff` `core`
- 动作一律**过去式**（事实已发生）。进行态属于状态机，不属于事件类型。
- 新增事件：先改本文件的目录表，再写代码。禁止代码里出现目录外的 `event_type`。
- 已发布的事件类型**不得改名、不得删除**；语义变化用新名字 + 在目录里标注替代关系。

## 2. 事件信封

| 字段 | 规则 |
|---|---|
| `seq` | SQLite 自增，本机严格顺序，**客户端游标用它** |
| `event_id` | UUIDv7，全局唯一 |
| `event_type` | §1 规范 + 目录内已登记 |
| `aggregate_type` | 单数域名单数名，与 `event_type` 前缀一致（`task`、`execution`…） |
| `aggregate_id` | 聚合根 UUIDv7 |
| `aggregate_version` | 见 §3，`> 0` |
| `correlation_id` | 见 §4 |
| `causation_id` | 见 §4 |
| `principal_id` | `local-user` / `system` / adapter principal |
| `payload_json` | 必含 `"schema": 1`（契约版本，不加新列）。**入库前必须过强制脱敏** |
| `created_at` | Unix 毫秒 |

WS 帧：`{"type":"event","data":<event_journal 一行>}`。心跳：`{"type":"ping","server_seq":<N>}`——
**故意不带 `event_id`**，客户端据此区分"活着但没有新事件"，不得伪造空事件。

## 3. `aggregate_version`（ADR-017，已批准）

`000002` 给 **10 张**聚合根表加 `version INTEGER NOT NULL DEFAULT 1`（不加 CHECK，理由见 ADR-017），
取号与改状态合并成一条语句，由 db 平台层唯一入口执行：

```sql
UPDATE tasks SET status = ?, updated_at = ?, version = version + 1
WHERE id = ? RETURNING version;
```

- append-only 表（`messages` `event_journal` `audit_entries` `execution_output_chunks`）**不加版本列**。
- 同事务内「改聚合 + 插事件」，提交后才发布。先推 WS 后写库 = P0 缺陷。
- `UNIQUE(aggregate_type, aggregate_id, aggregate_version)` 冲突 ⇒ 有代码绕过平台层，按缺陷修到源头。
- 同一事件只允许产生一次。重放保护靠 `Idempotency-Key`（领域模型「Idempotency」）+ 上述唯一约束。

## 4. Correlation / Causation

- `correlation_id` = 链条**第一个**事件的 `event_id`；一条用户命令及其全部下游共享它。
  用户命令本身还没有 event id 时，先分配一个 UUIDv7 作为 correlation id。
- `causation_id` = 直接触发本事件的那个事件的 `event_id`；由用户命令直接产生的事件，其 `causation_id = correlation_id`。
- 跨 Recovery 的边界：Recovery Engine 重放产生的新命令链开**新的** correlation id，
  并在 `payload_json.recovered_from` 里记下旧 correlation id，保证两条链都能查通。

## 5. 状态机迁移矩阵（唯一合法迁移集合）

矩阵外的迁移一律返回 `ErrIllegalTransition` 并写 Audit，**不允许就地"顺手修正"状态**。
`recovery` 列 = Core 重启扫描（「Crash Recovery Query」）后允许执行的迁移。
终态：`ACCEPTED`、`CANCELLED`（Task）；`SUCCEEDED`、`FAILED`、`CANCELLED`（Execution）。

### 5.1 Task

| from | 正常后继 | recovery 允许 |
|---|---|---|
| `CREATED` | `QUEUED` `CANCELLED` `BLOCKED` | — |
| `QUEUED` | `PREPARING` `PAUSED` `BLOCKED` `CANCELLED` `FAILED` | `CREATED` |
| `PREPARING` | `RUNNING` `WAITING_APPROVAL` `BLOCKED` `PAUSED` `CANCELLED` `FAILED` | `QUEUED` `INTERRUPTED` |
| `RUNNING` | `WAITING_APPROVAL` `WAITING_INPUT` `BLOCKED` `PAUSED` `TRANSFERRING` `AGENT_COMPLETED` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `WAITING_APPROVAL` | `RUNNING` `BLOCKED` `PAUSED` `CANCELLED` `FAILED` | `INTERRUPTED` |
| `WAITING_INPUT` | `RUNNING` `BLOCKED` `PAUSED` `CANCELLED` `FAILED` | `INTERRUPTED` |
| `BLOCKED` | `QUEUED` `RUNNING` `PAUSED` `CANCELLED` `FAILED` | `QUEUED` `INTERRUPTED` |
| `PAUSED` | `QUEUED` `RUNNING` `CANCELLED` | `QUEUED` |
| `TRANSFERRING` | `RUNNING` `QUEUED` `BLOCKED` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `AGENT_COMPLETED` | `VERIFYING` `WAITING_ACCEPTANCE` `RUNNING` `CANCELLED` | `INTERRUPTED` |
| `VERIFYING` | `WAITING_ACCEPTANCE` `RUNNING` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `WAITING_ACCEPTANCE` | `ACCEPTED` `RUNNING` `VERIFYING` `PAUSED` `CANCELLED` | — |
| `INTERRUPTED` | `QUEUED` `RUNNING` `TRANSFERRING` `CANCELLED` `FAILED` | — |
| `FAILED` | `QUEUED` `CANCELLED` | — |
| `ACCEPTED` `CANCELLED` | ∅ | ∅ |
| `UNKNOWN` | **仅** Recovery Engine 携带证据时可出，且必须同时写 Audit | — |

`AGENT_COMPLETED != ACCEPTED`：AI 自称完成只能到 `AGENT_COMPLETED`。`UNKNOWN` 不得被显示成
`FAILED` / `0` / `Offline`，除非有证据。

### 5.2 Execution

| from | 正常后继 | recovery 允许 |
|---|---|---|
| `CREATED` | `STARTING` `CANCELLED` `FAILED` | — |
| `STARTING` | `RUNNING` `WAITING_APPROVAL` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `RUNNING` | `WAITING_APPROVAL` `WAITING_INPUT` `PAUSED` `TRANSFERRING` `SUCCEEDED` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `WAITING_APPROVAL` | `RUNNING` `PAUSED` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `WAITING_INPUT` | `RUNNING` `PAUSED` `FAILED` `CANCELLED` | `INTERRUPTED` |
| `PAUSED` | `RUNNING` `CANCELLED` | `RUNNING` `INTERRUPTED` |
| `TRANSFERRING` | `FAILED` `CANCELLED` | `INTERRUPTED` |
| `INTERRUPTED` | `RUNNING` `FAILED` `CANCELLED` | — |
| `SUCCEEDED` `FAILED` `CANCELLED` | ∅ | ∅ |
| `UNKNOWN` | 同 Task 规则 | — |

### 5.3 投影规则（Task 不镜像 Execution，只由边沿驱动）

| Execution 事件 | Task 迁移 |
|---|---|
| `execution_started` | `PREPARING → RUNNING` |
| `execution_approval_requested` | `RUNNING → WAITING_APPROVAL` |
| `execution_approval_resolved`（批准） | `WAITING_APPROVAL → RUNNING` |
| `execution_succeeded` | `RUNNING → AGENT_COMPLETED` |
| `execution_failed` | → `FAILED`，除非 Handoff 已 `READY`/`STARTING_TARGET` 则 → `TRANSFERRING` |
| `execution_interrupted` | `RUNNING → INTERRUPTED` |
| `execution_cancelled` | `RUNNING → CANCELLED` |

Execution 的非边沿变化（`RUNNING → WAITING_INPUT → RUNNING`）**不产生** Task 事件。

## 6. 事件目录 v0.1

| event_type | 触发者（包） | payload 关键字段 | Audit |
|---|---|---|---|
| `project_created` | project | `name` `canonical_path` | – |
| `project_archived` | project | `reason` | – |
| `adapter_registered` | adapter | `adapter_key` `protocol_version` | – |
| `adapter_health_changed` | adapter | `from` `to` `confidence` | – |
| `adapter_capability_verified` | adapter | `capability_name` `support_level` | – |
| `model_discovered` | model | `remote_model_id` `canonical_key` | – |
| `session_discovered` | session | `remote_session_id` `adapter_instance_id` | – |
| `session_managed` | session | `managed` `project_id` | – |
| `session_synced` | session | `sync_state` `sync_cursor` `remote_revision` `confidence` | – |
| `message_recorded` | session | `role` `kind` `sequence_no` `remote_message_id` | – |
| `task_created` | task | `title` `priority` `mode` `risk_level` `project_id` | – |
| `task_queued` | task | `priority` | – |
| `task_preparing` | task | `execution_id` | – |
| `task_running` | task | `execution_id` | – |
| `task_waiting_approval` | task | `approval_id` | ✓ |
| `task_agent_completed` | task | `execution_id` `result_code` | – |
| `task_verifying` | task | `check_kind` | – |
| `task_waiting_acceptance` | task | `execution_id` | – |
| `task_accepted` | task | `accepted_by` | ✓ |
| `task_failed` | task | `error_code` | ✓ |
| `task_cancelled` | task | `reason` | ✓ |
| `task_interrupted` | task | `execution_id` `lease_state` | ✓ |
| `task_transferring` | task | `handoff_id` | – |
| `task_state_corrected` | recovery | `from` `to` `evidence` `recovered_from` | ✓ |
| `execution_created` | execution | `task_id` `adapter_instance_id` `model_id` `attempt_no` `trigger_type` | – |
| `execution_started` | execution | `provider_execution_id` `model_snapshot_sha256` | – |
| `execution_output_flushed` | execution | `batch_first_seq` `batch_last_seq` `char_count` `channel` | – |
| `execution_approval_requested` | approval | `approval_id` `action` `resource` `risk_level` | ✓ |
| `execution_approval_resolved` | approval | `approval_id` `state` `grant_id` `resolved_by` | ✓ |
| `execution_cancelled` / `_failed` / `_succeeded` / `_interrupted` | execution | `result_code` `error_code` `usage` | ✓(failed/interrupted) |
| `approval_requested` / `_resolved` / `_expired` / `_cancelled` | approval | 同上 | ✓ |
| `grant_issued` / `grant_revoked` / `grant_expired` | permission | `scope_type` `scope_id` `action` `expires_at` | ✓ |
| `quota_observed` | quota | `quota_account_id` `quota_key` `observation_kind` `confidence` | – |
| `notification_created` / `_handled` | notification | `notification_type` `priority` `task_id` `approval_id` | – |
| `handoff_created` / `_ready` / `_completed` / `_failed` | handoff | `source_execution_id` `target_adapter_instance_id` `package_sha256` | – |
| `process_started` / `_exited` / `_crashed` | process | `process_type` `pid` `exit_code` `owner_type` `owner_id` | ✓(crashed) |
| `core_ready` / `core_shutdown` | core | `version` `migration_version` | ✓ |

（表中合并同族的行仍按逐条类型登记，实现时不得把多个类型压成一个。）

## 7. 垂直链事件序列（测试 oracle）

契约测试与状态机测试以此为准，不多不少、顺序允许并发交错但因果关系必须成立：

```
project_created → session_discovered → session_managed
→ task_created → task_queued → task_preparing
→ execution_created → execution_started → task_running
→ message_recorded → execution_output_flushed × N
→ execution_approval_requested → task_waiting_approval
→ approval_requested → notification_created
→ approval_resolved → grant_issued → execution_approval_resolved → task_running
→ execution_output_flushed × N → execution_succeeded → task_agent_completed
→ task_waiting_acceptance → task_accepted
```

## 8. 客户端游标与幂等

- 客户端持久化 last `seq`；重连时 `/ws/v1/events?from_seq=N`，服务端从 journal 补发再切实时。
  journal 已被裁剪时返回 `resync_required`，客户端改走 REST 全量拉取。
- 高风险写操作（`task_created` / `send_message` / `approval_resolved` / `handoff_created`）必须带
  `Idempotency-Key`，键 = `(principal_id, operation, request_hash)`。命中已完成记录时
  **返回首次的 `response_json`，不重放副作用、不产生新事件**。

## 9. 禁止事项（P0）

1. 未登记的 `event_type` 入库。
2. 先 `Publish` 后 `Commit`。
3. 修改已落库的事件行（`event_journal` 只 INSERT）。
4. 把 `UNKNOWN` 猜测成确定状态。
5. 未过脱敏的 payload 入库（见内部契约的 Redaction）。
6. 用 `event_journal` 当聚合当前版本号的唯一来源（那是聚合表 `version` 列的职责）。

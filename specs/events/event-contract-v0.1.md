# Event Contract v0.1（Wave-1 **事件契约**）

> **编号消歧（2026-09-22）**：本文与 `specs/contracts-internal-v0.1.md` 曾同时自称"契约①"，
> 于是"契约① §2/§8"这种引用**两边都能对上**（本文 §2=事件信封、§8=客户端游标；那份 §2=Redaction、没有 §8）。
> 约定：**本文一律写作「事件契约」，`契约①` 单写时指 `contracts-internal-v0.1.md`**。
> 引用只写编号不写文件名的一律按此读；今后新写的引用请带文件名。

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

WS 帧全集（**形状以本文为准，游标权威与首帧规则以契约③ §5 为准**，两处不重复写规则、只互指）：
`{"type":"event","data":<event_journal 一行>}`；心跳 `{"type":"ping","server_seq":<N>}`——
**故意不带 `event_id`**，客户端据此区分"活着但没有新事件"，不得伪造空事件；
`{"type":"hello","from_seq":N}`（**客户端→服务端**的第一个帧）；
服务端首帧二选一：回显 `{"type":"hello","from_seq":N}`（hello 带了游标）**或** `{"type":"hello_ack","server_seq":head}`
（hello 省略游标 ⇒ 起点=journal 头、**不补发**，客户端必须把 `server_seq` 记成新游标）；
`{"type":"resync_required"}`（journal 已裁到无法补发 ⇒ 客户端改走 REST 全量拉）。
> 2026-09-22 LAG-2 回写：本段此前只列了 `event` 与 `ping` 两种，`hello_ack` 从未出现在事件契约里，
> 而 §8 又只教了 `?from_seq=` —— 两处合起来正好把"缺省即全量重放"这条被禁止的行为写成了唯一读法。

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
| `CREATED` | `STARTING` `CANCELLED` `FAILED` | — |   ← **REQ-DB-4 裁定（09-24 08:5x）**：这条 `CREATED → STARTING` 从今天起由 **`execution_starting`** 表达
   （此前 §6 里没有对应事件，`execution.Service` 宁可明确失败也不自己造事件 ⇒ 正向写第一步落不了库）。
   **登记顺序照 §1.1 第 16 条那把尺子：先写本目录与 §6 那一行，再改常量与判据**——
   `platform/db`/`eventjournal` 那侧必须同轮补常量与注册集合，否则"目录 ↔ 常量"那条双向判据会红一轮，
   **那是预期的中间态，不是谁的回归**。
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

### 5.4 投影无合法目标时（C1 裁定，2026-09-19）

§5.1 是**唯一权威**：投影只能沿 §5.1 的边走。若某 Execution 事件在 Task 当前状态下**找不到合法目标**
（典型：`QUEUED`/`PAUSED`/`BLOCKED` 时收到 `execution_failed` 且 Handoff 已 READY），则：

1. Task **不推进版本、不产生任何 `task_*` 事件**——§3 的"一个版本一个事件"绑定的是状态变化，
   没有状态变化就没有 Task 版本。
   **C12 更正（17:45，此处原文有歧义已被 audit 顶回）**：原文写"不产生任何事件"字面不成立，
   因为下面第 2 条要写 Notification，而 Notification 是**独立聚合**，必然在 `event_journal` 落一行。
   精确口径是：**`aggregate_type=task` 零新增版本、零事件**；允许且**应当**恰有一行
   `notification_created`（`aggregate_type=notification`）；Audit 走行哈希链，不进 `event_journal`。
2. 必须产出一条 **Notification**（`notification_type = projection_rejected`，经
   `notification.Service.Notify` → `Tx.Create(AggNotification, …)`，正常发 `notification_created` 事件）
   + 一条 **Audit**（`action = execution_failed`，`outcome = UNKNOWN`，`details_json` 带 `task_state`、
   `execution_event`、`reason`）。Notification/Audit 走各自通道，不碰 Task 版本。
3. 该情况**不得静默吞掉**——静默 = "丢任务"的早期形态。`test` 角色需有一条用例钉住此行为。

### 5.5 UNKNOWN 的进入与离开（C3 裁定）

§5.1/§5.2 原文只写了"仅 Recovery 携带证据时可出"，此处补全：

- **进入 UNKNOWN** 与**离开 UNKNOWN** 都必须满足三条件：`Occasion = Recovery` + 非空 `Evidence` + **起点不是终态**。
- **终态（Task `ACCEPTED`/`CANCELLED`；Execution `SUCCEEDED`/`FAILED`/`CANCELLED`）永不被改判成 `UNKNOWN`**——
  那等于抹掉已发生的历史，属 §12「丢 Task 或 Execution 历史」。
- `UNKNOWN` 不得被渲染或折算为 `FAILED` / `0` / `Offline`，除非另取得证据。


## 6. 事件目录 v0.1

> **本表是事件类型名的唯一权威（REQ-T24 裁定，09-24 09:4x）。** 起因：同一个动作在三份文档里出现过三种写法——
> 契约③ §4 写 `approval_resolved`(+`grant_issued`)、本表登记 `execution_approval_resolved`、契约① §1.2.1 规则 2 两个都用。
> **结论是它们本来就该是两个名字**：`approval_resolved` 属 **approval** 聚合、`execution_approval_resolved` 属 **execution** 聚合，
> 一条审批链上两者都要落（前者是后者的 `causation_id` 来源，契约① 那句是对的）。
> **由此两条规矩**：① 其它文档引用事件名时**必须写本表的完整名**（不许简写、不许省聚合前缀），
> 因为 `task_running` 与 `execution_started` 这种"看起来同义"的名字本来就是两码事；
> ② 本表允许**合并行登记**（如 `execution_paused` / `execution_resumed` 一行两名），
> **凡按字面 grep 本表来对账的判据，必须先走 §6 的展开约定 ①~⑤ 再比对**——直接 grep 必然假红
> （`test` 09:3x 先自曝了一次**假警报**：它报"§6 缺 `approval_resolved`"，其实是本表有**合并行**、字面 grep 数不到——它把这次的教训立成了长期规矩，所以本条的依据是**行内容**而不是"我 grep 到了"）。

| event_type | 触发者（包） | payload 关键字段 | Audit |
|---|---|---|---|
| `project_created` | project | `name` `canonical_path` | – |
| `project_updated` | project | `name` `metadata` | – |
| `project_archived` | project | `reason` | – |
| `adapter_registered` | adapter | `adapter_key` `protocol_version` | – |
| `adapter_health_changed` | adapter | `from` `to` `confidence` | – |
| `adapter_downgraded` | adapter | `capability_name` `from_backend` `to_backend` `reason` | ✓ |
| `adapter_restart_exhausted` | adapter | `pid` `attempt_count` `backoff_ms` | ✓ |
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
| `task_paused` | task | `reason` | – |
| `task_blocked` | task | `reason` `blocked_on` | – |
| `task_resumed` | task | `from`（`PAUSED`/`BLOCKED`） | – |
| `task_transferring` | task | `handoff_id` | – |
| `task_state_corrected` | recovery | `from` `to` `evidence` `recovered_from` | ✓ |
| `execution_paused` / `execution_resumed` | execution | `reason` / `from` | – |
| `execution_starting` | execution | `from` | – |
| `execution_created` | execution | `task_id` `adapter_instance_id` `model_id` `attempt_no` `trigger_type` | – |
| `execution_started` | execution | `provider_execution_id` `model_snapshot_sha256` | – |
| `execution_output_flushed` | execution | `batch_first_seq` `batch_last_seq` `char_count` `channel` | – |
| `execution_approval_requested` | **execution**（10-01 17:5x 裁，REQ-APR-1；**本行原写 `approval`**，那与 §6 边表 `:131` 把这条边挂在 execution 状态机上直接矛盾——按契约① REQ-B-1「`aggregate_type` 决定谁能发它」，execution 的 `Touch` 永远发不出 approval 聚合的事件 ⇒ **要么改边表、要么改目录。我裁改目录这一格**：状态边的载体，聚合就是状态所属的那个聚合。载荷四键一字未动，它带的正是"这次等待卡在哪个审批上"） | `approval_id` `action` `resource` `risk_level` | ✓ |
| `execution_approval_resolved` | **execution**（同上一条裁定；`approval_resolved` 那条**仍归 approval 聚合**、由 `internal/approval` 自己发，两族各有其位、不重复：**审批对象自己的事件** vs **执行的状态边**） | `approval_id` `state` `grant_id` `resolved_by` | ✓ |
| `execution_cancelled` / `_failed` / `_succeeded` / `_interrupted` | execution | `result_code` `error_code` `usage` | ✓(failed/interrupted) |
| `execution_unknown` | execution | `from` `reason` | ✓ |
| `execution_claimed` | execution | `provider_execution_id` | – |
| `approval_requested` / `_resolved` / `_expired` / `_cancelled` | approval | 同上 | ✓ |
| `grant_issued` / `grant_revoked` / `grant_expired` | permission | `scope_type` `scope_id` `action` `expires_at` | ✓ |
| `policy_created` / `policy_updated` | permission | `scope_type` `scope_id` `action_pattern` `effect` | ✓ |
| `quota_observed` | quota | `quota_account_id` `quota_key` `observation_kind` `confidence` | – |
| `notification_created` / `_handled` / `_read` | notification | `notification_type` `priority` `task_id` `approval_id` | – |
| `handoff_created` / `_ready` / `_completed` / `_failed` | handoff | `source_execution_id` `target_adapter_instance_id` `package_sha256` | – |
| `process_started` / `_exited` / `_crashed` | process | `process_type` `pid` `exit_code` `owner_type` `owner_id` | ✓(crashed) |
| `core_ready` / `core_shutdown` | core | `version` `migration_version` | ✓ |

（表中合并同族的行仍按逐条类型登记，实现时不得把多个类型压成一个。）

> **合并行的机械展开约定（2026-09-22 落文，因为今天有两个解析器要各自猜它）**
> 本表第一列允许写成 `` `notification_created` / `_handled` / `_read` `` 这种同族合并形状。展开规则**只有一条**：
> ① 按 `/` 切；② 每个 token 去掉反引号与空白；③ **词干 = 该 `_xxx` 之前"最近一个全名"里最后一个 `_` 及其之前的全部文字**，
> 以 `_` 开头的 token ⇒ **词干 + 该 token 去掉前导 `_`**（`_read` 在 `notification_created` 之后 ⇒ `notification_read`）；
> **③b 而且合并格只许两种形态**：要么**全部写全名**（`grant_issued` / `grant_revoked` / `grant_expired`），
> 要么**一个全名打头 + 若干 `_xxx` 简写**（`notification_created` / `_handled` / `_read`）。
> **"多个全名 + 后面还跟简写"这种混排（假想 `a_b` / `c_d` / `_e`）是表格形状坏了，解析器必须报错** ——
> 这一条把"第一个还是最近一个"这个歧义**取消**而不是裁决：混排非法，两种读法就再没有能区分它们的输入。
> （09-22 23:49 巡检读码坐实：我原先写的"词干 = 本行**第一个**全名…"与 `db` 实现（`default:` 分支每个全名都重写 `stem`
> ⇒ 取的是**最近一个**）不是一回事，而 §6 今天没有能区分两者的行 ⇒ **零分歧、但那句"与现实现一致"是误导**。
> 现按 `db` 的实际语义定边，并加 ③b 关掉歧义类。`frontend` 已在自己那侧加了"歧义行必须停"的守门（探针 E），
> **它不替契约挑边、只把冲突报上来，这是对的做法** —— ③b 就是给它的那个守门的契约依据。）
> **③c 合并只许用于"payload 关键字段相同"的同族行（09-23 01:3x 补，起因是我自己造的那次重名）**：上面 `project_created` / `project_updated` / `project_archived` 三条**各自的 payload 都不一样**，所以它们**必须各占一行** —— 我 09-22 为了"少一行"把它们塞进一格，就得在 payload 列里写 "created：… updated：… archived：…"这种带前缀的表意，**那正是"这一格其实是三件事"的信号**。我当时还漏删了原来那行 `project_archived` ⇒ 展开后重名，`frontend` 的守门"重名即停"**先于**它要抓的 `enum ↔ 目录` 触发，`npm run validate` exit=2 —— **一个为了省行的合并，把别人的判据挡在了自己前面**。判据：**payload 不同 ⇒ 不合并；要合并先确认三个 payload 写出来是同一串。**
> **③c 的机检状态（09-23 01:5x 裁定）：只管人，不机检。** 理由不是嫌麻烦，是**这张表的结构里没有那个信息**：合并格只有**一个** payload 列，"三条 payload 各不相同"这件事我是写在同一格内的散文里（"created：… updated：…"），机器要判它就得先把 payload 形式化成"每事件一列"——那是**为了一个风格规矩去改整张表的 schema**，代价大于风险。**真正兜住损害的是另一条可机检的断言**：合并/漏删造成的实际危害是"同一名字出现两次"，而它由契约② 判据表 **C8-13 的第二条腿**（"展开后 token 数 == 去重后名字数"）精确拦住。⇒ **③c 留在人侧当写作规矩，机器侧由 C8-13 兜底**；若将来 payload 真的形式化成每事件一列，再回来机检 ③c。
> ④ 不带 `_` 前导的 token 就是全名，原样收下（`grant_issued` / `grant_revoked` 那种整行写全的形态合法）；
> ⑤ **`_xxx` 出现在"本行还没有全名"的位置 ⇒ 判表格或解析器坏了、必须报错**，
> **不许静默跳过**（静默少收一个类型，正是这类解析器最坏的失效方式：它一直报绿）。
> 现有解析器：`core/internal/eventjournal/contract_parity_test.go` 的 `eventTypesIn`（`db`）。
> **它第 ⑤ 条今天不满足**（`if prefix != ""` 会把孤儿 `_xxx` 直接丢掉）⇒ 已派 T-142 改成报错。
> 契约② 判据表 **C8-12**（`test` 落的那条"目录 ↔ 常量双向相等"）**必须按本约定展开**，
> 否则两个包各猜一套规则，就会有一个把合法合并行读成缺失常量。

> **`notification_read` 是 2026-09-22 补登记的（REQ-DB-5 裁定）**：`notifications.state` 从 `000001` 起就有
> `UNREAD→READ` 这条边、也有 `read_at` 列，`internal/notification` 侧要落这条边就必须发事件，可目录里只有
> `_created`/`_handled` ⇒ backend 的 `POST /notifications/{id}:read` 今天**没有合法事件可发**，只能挂 501。
> 这与下面那条"已登记 ≠ 有生产路径"是**同一个洞的相反方向**：那条防"登记了没人发"，这条是"有人发却没登记"。
> 补登记的同时要补常量：`internal/eventjournal/events.go` 现有 `NotificationCreated`/`NotificationHandled` 两个，
> 缺 `NotificationRead`（归 `db`）。**词表闭合判据归 `test`**：目录里的类型集合 ⊇/⊆ `events.go` 的常量集合，
> 双向差集任一非空即红——这条今天还没有，见契约② 判据表 C8-11 同族的 T-135。

> **「已登记 ≠ 有生产路径」**：登记进本目录只是允许该事件类型存在，不代表有代码会发它。
> v0.1 已知有名字无生产者的**三条**：`policy_created` / `policy_updated`
> （`permission` 只**读** `policy_rules`，全仓没有写路径；写 CRUD 判给 Wave-3，见账本 11:47 裁定）
> **＋ `session_discovered`（主控 09-23 04:5x 实测补上，此前名单漏它）**：`internal/session/events.go`
> 的 `EventDiscovered` 与 `internal/eventjournal/events.go` 的 `SessionDiscovered` 常量都在、合法集合里也在，
> 可**全仓非测试码没有任何一处发它**（`grep -rn '"session_discovered"' core --include=*.go` 去掉 `_test.go`
> 只剩那两条常量定义），且 `sessions` 表在非测试码里**没有任何写路径**（`session.Repository` 四个方法全读；
> `Append("sessions"` 零命中）⇒ **§7 那条 `project_created → session_discovered → session_managed` 的第 2 步
> 今天不可能真跑**。生产者那半其实已经在盘上：`cmd/fake-adapter/service.go` 按 §4 回 `session/list` 载荷，
> 而 Core 侧零消费者 ⇒ 缺的只有 `sessions:discover` 把载荷落库这一步（活派在账本 T-198）。
> 判据/评审时别把"事件目录里有这行"当成"该功能已具备"——`test` 的 lint 也不得据此要求存在性。

> **【名单现状 · 主控 09-25 19:2x 复量】上面那段要按两处更正来读，原文照例留档不删：**
> ① **`session_discovered` 从名单里划掉**——"非测试码没有任何一处发它 / `sessions` 表无非测试写路径"已被事实超过：
> `internal/session` 的 `discover.go` 走 `Tx.Create(db.AggSession, …)` 并带恰一条 `EventDiscovered`，
> `cmd/core` 的 discover 腿也已装配（`newAdapterServices` → `Core.discoverField()`，没配 Adapter 时给 nil ⇒ 501）。
> 复量命令：`grep -rn "EventDiscovered" core/internal/session/discover.go`（非测试码命中即证明有生产者）。
> 上面那句"§7 第 2 步今天不可能真跑"随之失效——**但"能真跑"仍要以 `sessions:discover` 的端到端用例为证，不是以这段更正为证**。
> ② **新补一条进名单**：`adapter_downgraded` —— 非测试码只剩 `internal/eventjournal` 的 `AdapterDowngraded` 常量
> 与注册表那个 `true` 条目，**零发射点**（09-25 19:2x 实测）。归属与发射时机已裁在契约① §1.1.22：
> 发在**降级发生的那一刻**（`internal/adapter` 的能力核定/注册域，Touch `adapter_instances`），
> **`POST /sessions/{id}:sync` 的 503 出口只回码、不发这条事件**（守卫机械不允许，见契约① 同条引的
> `TestAdapterDowngradedCannotRideASessionTouch`）。
> 所以本名单**当时**是**三条**（此行为留档，取代链：09-27 的 27 条 → 09-28 的 26 条 → 09-30 的 21 条（当前）；**故意不再使用"当前是"那个锚字**，见 REQ-T37 裁甲）：`policy_created` / `policy_updated` / `adapter_downgraded`。

> **【名单扩容 · 主控 09-27 16:3x 按 `test` 的名册判据登记，原文留档不删】上面那句"三条"已被取代：名单现为 27 条。**
> 新增的 **24 条**由判据机械导出（不是我手抄）：`go test -count=1 -v -run '^TestEventProducerRosterMatchesDerivedSet$' ./test/`
> 打印的"派生集合里有 X 而名单没登记"逐条取回，分类是 `test` 给的（**22 条只有定义位、2 条只在错误文案里出现**）：
> `adapter_restart_exhausted`、`approval_cancelled`、`core_shutdown`、`execution_approval_requested`、
> `execution_approval_resolved`、`execution_output_flushed`、`grant_revoked`、`handoff_completed`、`handoff_created`、
> `handoff_failed`、`handoff_ready`、`model_discovered`、`project_archived`、`quota_observed`、`session_synced`、
> **`task_accepted`**、`task_blocked`、`task_paused`、`task_preparing`、`task_queued`、`task_resumed`、
> `task_state_corrected`、`task_verifying`、`task_waiting_acceptance`。
> **两条明确不进名单**：`process_crashed` / `process_exited` —— 四层回解（形参←同包调用点实参）生效后
> 判为**有生产者**（契约① 第 24 条⑧款那格、账本 T-371① 裁的就是这一层）。
> ⚠ **`task_accepted` 在这一批里最值得单看**：它是 §7 垂直链的收尾事件，而名册说它今天**没有生产者**
> ⇒ Wave-2 第 8 格即使 `:accept` 走通，也要确认这一条**真的落进 `event_journal`**，
> 别把"HTTP 200"当成"链尾事件已发生"（判据面：契约① §1.1 那串垂直链 oracle + 账本 T-381/T-382）。
> **口径提醒**：本名单是"登记了但没人发"的**已知缺陷清单**，不是功能清单；补登记不等于补功能。
>
> 所以**本名单当时是 27 条**（09-27 16:3x 起，取代上面那句"三条"）：`policy_created` / `policy_updated` / `adapter_downgraded` / `adapter_restart_exhausted` / `approval_cancelled` / `core_shutdown` / `execution_approval_requested` / `execution_approval_resolved` / `execution_output_flushed` / `grant_revoked` / `handoff_completed` / `handoff_created` / `handoff_failed` / `handoff_ready` / `model_discovered` / `project_archived` / `quota_observed` / `session_synced` / `task_accepted` / `task_blocked` / `task_paused` / `task_preparing` / `task_queued` / `task_resumed` / `task_state_corrected` / `task_verifying` / `task_waiting_acceptance` 。

>
> **【名单摘条 · 随 backend 第七十八批落地（REQ-ACCEPT-1 裁「甲」，09-28 实测登记），上面那句"27 条"已被取代、原文留档不删】**
> 摘掉的只有 **1 条**：`task_waiting_acceptance` —— 它今天**真有生产者**：Core 在任务投影落定 `AGENT_COMPLETED`
> 的**同一笔事务**里接着推 `WAITING_ACCEPTANCE`（`core/internal/task/service.go` 的 `AdvanceToWaitingAcceptance`，
> 发射位的 `Type` 是常量，所以名册判据认得出；闭包里回出来的值认不出，`task_queued`/`task_accepted` 因此还躺在上面那句里）。
> 实测（`go test -count=1 -run '^TestEventProducerRosterMatchesDerivedSet$' ./test/`）：桶一 37→38、桶二（派生集合）27→26、未决 0 处。
> **`task_verifying` 维持无生产者**（v0.1 没有验证信号源，裁语点名不许顺手补）；其余 25 条一条没动。
> 口径不变：名单仍是"登记了但没人发"的已知缺陷清单、补登记不等于补功能 —— 这一条是**先补了功能才摘**，
> 摘条的授权随契约④-c（`specs/contracts-internal-v0.1.md` 裁「甲」那一段）一并给出。
>
> 所以**本名单当时是 26 条**（09-28 起至 09-30，取代上面那句"27 条"；本行留档，已被下面 09-30 的 21 条取代）：`policy_created` / `policy_updated` / `adapter_downgraded` / `adapter_restart_exhausted` / `approval_cancelled` / `core_shutdown` / `execution_approval_requested` / `execution_approval_resolved` / `execution_output_flushed` / `grant_revoked` / `handoff_completed` / `handoff_created` / `handoff_failed` / `handoff_ready` / `model_discovered` / `project_archived` / `quota_observed` / `session_synced` / `task_accepted` / `task_blocked` / `task_paused` / `task_preparing` / `task_queued` / `task_resumed` / `task_state_corrected` / `task_verifying` 。
> 所以**本名单当时是 21 条**（09-30 15:5x 起至 10-01，取代上面那句"26 条"；本行留档，已被下面 10-01 的 20 条取代）：`policy_created` / `policy_updated` / `adapter_downgraded` / `adapter_restart_exhausted` / `approval_cancelled` / `core_shutdown` / `execution_approval_requested` / `execution_approval_resolved` / `execution_output_flushed` / `grant_revoked` / `handoff_completed` / `handoff_created` / `handoff_failed` / `handoff_ready` / `model_discovered` / `project_archived` / `quota_observed` / `session_synced` / `task_blocked` / `task_state_corrected` / `task_verifying` 。
> **【摘条 · 10-01 14:3x 主控裁，随 backend 的 T-424 产物 `core/internal/execution/output.go` 同一个提交入库】** 摘掉的只有 **1 条**：`execution_output_flushed`。
> 实测（`go test -count=1 -v -run '^TestEventProducerRosterMatchesDerivedSet$' ./test/`，10-01 14:3x 起跑前现读）：判据在我改名单**之前**就是红的，红的原话是
> 「名单登记 `execution_output_flushed` 无生产者，但扫描找到了覆盖它的发射位」，宽覆盖行点名 `internal/execution/output.go:136`、所在函数 `Service.FlushOutput()`；
> 同一读数：§6 词表 66 个名字、**桶一（确认有生产者）46 个**、**桶二（派生集合）20 个**、桶三（未决）0 处。⇒ 名单 21 与派生 20 只差这一条，摘完两桶相等（20/20）。
> 口径不变：**先补了功能才摘**（与 `task_waiting_acceptance` 那次同形）；泵那一侧的调用位（`core/cmd/core/turnpump.go` 的 `flushBatch`）在攒批/帧边界/泵退场三处落它。
>
> 所以**本名单当前是 20 条**（10-01 14:3x 主控裁，取代上面那句"21 条"）：`policy_created` / `policy_updated` / `adapter_downgraded` / `adapter_restart_exhausted` / `approval_cancelled` / `core_shutdown` / `execution_approval_requested` / `execution_approval_resolved` / `grant_revoked` / `handoff_completed` / `handoff_created` / `handoff_failed` / `handoff_ready` / `model_discovered` / `project_archived` / `quota_observed` / `session_synced` / `task_blocked` / `task_state_corrected` / `task_verifying` 。
> **09-30 15:5x 摘掉 5 条**（`task_accepted` / `task_paused` / `task_preparing` / `task_queued` / `task_resumed`）—— 测试角色这轮给名册判据加了**第五层**（回解 `eventOf func(Task) (string, any)` 闭包回出的常量），五条各自的发射位在 `core/internal/task/service.go:326 / :338 / :347 / :364 / :385` 的 `return EventXXX, …Payload{…}`，而承载它们的方法 `Queue`/`Prepare`/`Pause`/`Resume`/`Accept` **都有非测试调用点**：`core/api/http/tasks.go:483 / :518 / :574 / :629 / :605` ⇒ 按本契约「有发射位即有生产路径」的口径，这五条该离开名单。**本行由主控改（`specs/**` 归主控），不是 worker 自改**；U13-8 那条判据自己会盯住以后谁把它们悄悄加回去或删掉生产路径。

## 7. 垂直链事件序列（测试 oracle）

契约测试与状态机测试以此为准，不多不少、顺序允许并发交错但因果关系必须成立：

```
project_created → session_discovered → session_managed
→ task_created → task_queued → task_preparing
→ execution_created → execution_starting → execution_started → task_running
→ message_recorded → execution_output_flushed × N
→ execution_approval_requested → task_waiting_approval
→ approval_requested → notification_created
→ approval_resolved → grant_issued → execution_approval_resolved → task_running
→ execution_output_flushed × N → execution_succeeded → task_agent_completed
→ task_waiting_acceptance → task_accepted
```

## 8. 客户端游标与幂等

- 客户端持久化 last `seq`；**重连时游标的唯一权威是连上后第一个客户端帧 `{type:"hello","from_seq":N}`**，
  服务端据此从 journal 补发 `seq > N` 再切实时。`?from_seq=N` 只是**"根本没收到 hello"时的回落**（超时路径），
  省略 `from_seq` 的 hello 会主动把 query 上的游标作废 —— 完整口径与三条判据在**契约③ §5**（REQ-F-20），本文不重述。
  journal 已被裁剪时返回 `resync_required`，客户端改走 REST 全量拉取。
  **禁止**把"没带游标"实现成"从 0 全量重放"（§9 禁止事项同源：那等于自己 DDOS 自己）。
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

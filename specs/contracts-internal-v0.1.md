# Internal Module Contracts v0.1（Wave-1 契约①）

- 状态: **accepted**（用户 2026-09-19 批准：redaction 机制/规则分离 + `approvals` SQL 只准出现在 `internal/approval`）
- 适用: `core/internal/**` 包之间的调用方向与接口形状
- 上游依据: ADR-004 模块化单体、ADR-011 集中式 Permission Engine、ADR-012 敏感操作进 Audit、
  领域模型「Repository Boundary」「Temporary Grant」「Approval」「Audit 与 Secret」
- 补齐的缺口: 交接包没写 `aggregate_version` 由谁维护（见 ADR-017）、也没定义"脱敏层"放在哪一层
  （本轮编排裁决：机制归 db 平台层，规则归 audit，见 §2）

## 0. 依赖方向（单向，违反即缺陷）

```
cmd/core ──► api/{http,websocket} ──► 业务包(task/execution/session/quota/…)
                                          │
                                          ├──► internal/platform/db   (平台层，不含业务语义)
                                          ├──► internal/audit          (规则与哈希链，不含写入机制)
                                          └──► internal/eventjournal   (事件定义，不 import 业务包)

业务包之间：只准调对方导出的 Service 接口，禁止 import 对方的 repository、禁止跨包 SQL。
internal/platform/db 不得 import 任何业务包。
internal/audit 不得 import internal/platform/db。
两者在 cmd/core 的启动顺序里接线（见 §2.3）——依赖方向靠注入，不靠 import。
```

`000001` 的模块清单与包名一一对应，不新增 `internal/common`、`internal/utils`、`internal/shared` 这类口袋包。

## 1. db 平台层：所有模块的唯一写路径

`000001` 已建 `UNIQUE(aggregate_type, aggregate_id, aggregate_version)`，但没有地方存"聚合当前第几版"。
ADR-017 补上 `version` 列后，取号必须与改状态原子完成，因此**只允许**走这一个入口：

```go
package db

type AggregateKind string
const (
    AggProject AggregateKind = "project"; AggAdapter AggregateKind = "adapter"
    AggModel AggregateKind = "model";    AggSession AggregateKind = "session"
    AggTask AggregateKind = "task";      AggExecution AggregateKind = "execution"
    AggApproval AggregateKind = "approval"; AggQuota AggregateKind = "quota_account"
    AggNotification AggregateKind = "notification"; AggHandoff AggregateKind = "handoff"
)

type EventDraft struct {
    EventID   string          // UUIDv7，调用方生成（重试要复用 → 见契约事件文档 §8）
    Type      string          // 必须已在事件目录登记
    Aggregate AggregateKind
    AggregateID string
    CorrelationID string
    CausationID   string
    PrincipalID   string
    Payload       map[string]any   // 必含 "schema":1；入库前强制脱敏，见 §2
}

// Touch = 改聚合 + 取版本 + 插事件，一个事务、一个版本、一行事件。
// setSQL 形如 "status = ?"，参数与 args 按位对应；列名白名单校验，不接受拼接。
func (tx *Tx) Touch(a AggregateKind, id, setSQL string, args []any, ev EventDraft) (newVersion int64, err error)

// Append 用于 append-only 行（messages / execution_output_chunks / audit_entries）：不占版本号。
func (tx *Tx) Append(table string, columns []string, values []any) (int64, error)

func (db *DB) Write(ctx context.Context, fn func(tx *Tx) error) error  // BEGIN IMMEDIATE + 串行写通道
```

硬性规则：

1. `Touch` 内部执行 `UPDATE <tbl> SET <setSQL>, version = version + 1 WHERE id = ? RETURNING version`，
   再用返回值插事件；两者同事务。
2. **提交之后**才允许 `Publish`。`Tx` 不提供任何发送事件的能力；发布只发生在
   `Write` 成功返回之后，由调用方交给 EventBus。
3. 业务包各自持有自己的 `Repository`（只读查询 + 组装聚合），但**没有任何一个包能拿到
   别的表的写权限**。禁止 `GlobalDatabaseRepository`。
4. 连接级 PRAGMA（`foreign_keys`、`busy_timeout`）由 `db` 在每条新建池化连接上设置；
   库级 PRAGMA（`journal_mode`、`synchronous`）只在迁移/初始化时执行一次，
   **绝不在事务内执行**（`journal_mode` 在事务里是 no-op，会静默失效）。
5. **记账约定**：每个 `.sql` 迁移文件末尾自己 `INSERT INTO schema_migrations(version, name, applied_at)`
   （`000001`、`000002` 均已如此），迁移器只查这张表来决定执行哪个文件，**自己不写记账行**；
   迁移器**不得**再包一层事务（脚本自带 `BEGIN IMMEDIATE;…COMMIT;`，嵌套会直接报错）。
   因此每个迁移文件都**不可重放**：重跑会撞 `schema_migrations` 主键或 `CREATE TABLE` 已存在。
   执行前必须先备份 DB，失败则 Core 不进入 Ready。新迁移从下一号顺延，不改已发布的文件。

## 2. Redaction：规则归 audit，强制点在 db

「哪些算敏感」是安全策略（audit 的知识），「保证一定被处理」是写入机制（db 的职责）。
把机制交给业务包自觉 = `Secret 泄露进日志` 这条 P0 的成因，因此：

### 2.1 audit 只导出规则

```go
package audit

type RedactionRules struct {
    DenyKeys []string  // JSON 键名命中即整值替换：token access_token refresh_token id_token
                      // api_key authorization cookie password secret private_key client_secret session_key
    Patterns []string  // 正则（RE2，编译期即可校验）：PEM 头、ghp_/gho_/ghu_/ghs_/github_pat_、
                       // sk-、JWT 三段式、`Bearer <opaque>`
    Marker   string    // 默认 "[REDACTED]"
}
func Rules() RedactionRules   // 唯一导出面；不实现替换逻辑
```

### 2.2 db 执行并保证 fail-closed

```go
package db
func InitRedaction(r audit.RedactionRules) error   // 编译正则；任一失败 → Core 不进入 Ready
```

- 强制点：`Tx.Touch` 与 `Tx.Append` 在序列化 `payload_json` / `details_json` / 任意 TEXT 列**之前**递归遍历，
  命中 `DenyKeys` 的键整值替换，字符串值再跑 `Patterns`。没有"跳过脱敏"的参数。
- 每次命中记一条 `RedactionHit{column, key|pattern, count}` 进 `audit_entries.details_json`（只有计数与名字，**不含原值**）。
- 日志走同一个 redactor：日志 handler 出口处套用，不在各业务调用点各自处理。
- Secret 原值只允许以 `vault://<name>` 引用形式持久化（DPAPI abstraction，First Sprint 只留接口不实现）。
- 边界外文本（Provider 返回的自由文本里可能自带密钥）只能靠 `Patterns` 兜，属已知上限
  ——`ponytail: pattern-only for free text, entropy detection if false negatives bite`。

### 2.3 接线位置

`cmd/core` 启动顺序（Config → 单实例锁 → Vault → SQLite → Migration → Event Journal → Recovery →
Adapter Supervisor → Scheduler → HTTP/WS → Health → Ready）中，`InitRedaction` 必须在
SQLite 之后、Event Journal 可用之前完成；失败即不 Ready。

## 3. 审批与权限：裁决权与推进权分家

对应红线「Approval 应用于错误 Execution」「重复执行高风险动作」：批准的人不能同时是推进的人。

```go
package permission          // 只做判定，不写 approvals 表
type Effect string          // ALLOW / DENY / REQUIRE_APPROVAL / ALLOW_TEMPORARY
type Request struct {
    PrincipalID, ProjectID, TrustDomainID, TaskID, ExecutionID, SessionID string
    AdapterID, ModelID, TaskMode, Action, Resource string
}
type Service interface {
    // 优先级链：紧急停止 → 显式 Deny → Trust Domain → Project → Task Mode
    //          → Session Override → Temporary Grant → 默认
    Decide(ctx context.Context, r Request) (Effect, error)
    CheckTemporaryGrant(ctx context.Context, principalID, scopeType, scopeID, action string) (bool, error)
}

package approval            // approvals 聚合的唯一拥有者
type Resolution struct { State string /*APPROVED|REJECTED|EXPIRED|CANCELLED*/; By string; Note string }
type Service interface {
    Request(ctx context.Context, a Draft) (Approval, error)          // REQUIRE_APPROVAL 时由它建单
    Resolve(ctx context.Context, id string, res Resolution) error    // 幂等：已决态直接返回当前值，不产生第二个事件
    ForExecution(ctx context.Context, executionID string) ([]Approval, error)
    BlockingState(ctx context.Context, executionID string) (string, error) // backend 唯一能问的问题
    SweepExpired(ctx context.Context) error
}
```

规则：

1. `approvals` 表的 SQL **只准出现在 `internal/approval` 包内**。backend 需要"我能不能继续"时调
   `BlockingState(executionID)`，不许自己查表。可执行检查（`test` 角色）：
   `grep -rn "FROM approvals\|UPDATE approvals\|INSERT INTO approvals" core/internal --include=*.go`
   命中路径必须全在 `internal/approval/` 下。
2. `Resolve` 必须经 `Tx.Touch`（写 `approval_resolved` 事件）+ 一条 `audit_entries`，
   并把结果事件作为 `execution_approval_resolved` 的 `causation_id` 来源。
3. 批准后要发 `ALLOW_TEMPORARY` 时，必须落 `temporary_grants` 行（带 `scope_type`/`expires_at`），
   **不能只改 UI 状态**。回收靠 `SweepExpired`，不靠前端记得关掉。
4. Approval 与 Execution 的绑定靠 `execution_id` 精确匹配；无 `execution_id` 的审批不得被
   任何 Execution 视为"已批准"（防"Approval 应用于错误 Execution"）。

## 4. 跨模块读

| 需求 | 走法 |
|---|---|
| backend 想知道某 task 的 sessions | `task` 包不暴露 link 表；由 `session.Service.ForTask(ctx, taskID)` 提供 |
| frontend/远程端要任何数据 | 只走 REST/WS，禁止 import Go 类型（契约③会钉死） |
| audit 要事件 | 直接读 `audit_entries`；`event_journal` 与 `audit_entries` **不互相外键、不共用行** |

## 5. 待补（下一批契约）

- 契约②：Adapter JSON-RPC 握手与 capability 声明粒度（输入：Phase 0 已发现 Codex 有
  379 文件的 v2 协议 schema；**不自造 capability 枚举**，见账本上行输入）。
- 契约③：REST/WS 资源形状 + 文件所有权矩阵。
- 契约①批准后需要落的第一件事：`000002` 迁移草案（仅 ADR-017 的 9 张表加 `version` 列）。

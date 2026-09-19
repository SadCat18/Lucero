OWNER_ROLE: db
TASK_ID: db-platform
STATUS: complete
SCOPE: Core 的 DB 平台层唯一写路径（连接与 PRAGMA 分层、串行写通道、事务原语、迁移器、Event Journal 写入、强制脱敏）；不含业务语义
EVIDENCE_DIR: .qoder/evidence/
LAST_UPDATED: 2026-09-19T17:40+08:00

## Objective

交付 `core/internal/platform/db` 与 `core/internal/eventjournal`，使 scoped 验收命令
`CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test ./internal/platform/... ./internal/eventjournal/...` 全绿，
并覆盖主控给出的 6 项验收。

## Completion criteria

- [x] 空库依次应用 `000001` → `000002` 均成功（`000002` 实测无需修改，10 张表的 `version` 列全部落到位）
- [x] `Open` 的 PRAGMA 分层与 STRICT/FTS5 能力有测试断言
- [x] `RETURNING version` 可用且递增；并发 48 次改同一聚合不产生重复版本
- [x] 绕过 `Touch` 直插重复 `(aggregate_type, aggregate_id, aggregate_version)` 被唯一约束拒绝
- [x] 迁移器重跑不报错、不重复记账；半途失败后 `PRAGMA integrity_check` 为 ok 且脏表不存在
- [x] deny-key 命中后原值不出现在 `payload_json`；正则编译失败与未初始化都返回 error（fail-closed）
- [x] **验收口径已由主控更正为 scoped**：`CGO_ENABLED=0 go test ./internal/platform/... ./internal/eventjournal/...` 全绿；
  全量 `./...` 只作信息记录，其失败不算本角色验收失败（见 Verification evidence 的 INFO 段）

## Scope boundaries

- In scope: `core/internal/platform/**`、`core/internal/eventjournal/**`、`core/migrations/*.sql`（`000001` 只读）、本文件
- Out of scope: `core/go.mod`（主控）、`approvals` 表任何 SQL（audit）、业务包 Service/Repository、cmd/core 接线、git 写操作

## Current checkpoint

- Stage: 巡检 T-6（Q4）与 T-7（①P0 ②catalog ③改名）三条全部落地并实测；含一次变异测试证明用例会抓到该 P0
- Status: complete
- Last verified action: scoped 门禁 `go test ./internal/platform/... ./internal/eventjournal/... -count=1` → 2 包 ok、
  `gate_exit=0`；`go build ./internal/platform/db/` 与 `go vet` 同范围均 exit 0
- Next action: 通知 audit 收窄 `IsVaultRef` 的文档措辞（见 Coordination Q6），其余等主控验收
- Verification: `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test ./internal/platform/... ./internal/eventjournal/... -count=1`

## Completed

- **交付物 1 打开 + PRAGMA 分层**（`db.go`）：`Open(path)` 建读池（`MaxOpenConns(4)`）与写通道
  （`MaxOpenConns(1)`——单连接本身就是串行写通道，未另加 mutex）。连接级 `foreign_keys=1`/
  `busy_timeout=5000`/`synchronous=FULL` 走 DSN `_pragma`，每条新连接自动生效；
  库级 `journal_mode=WAL` 在 `initStorage` 于事务之外执行一次并回读断言。
- **交付物 2 迁移器**（`migrate.go`）：`Migrate(dir)` 读 `NNNNNN_name.sql`，按 `schema_migrations`
  已有版本号决定执行哪个文件；不包事务（脚本自带 `BEGIN IMMEDIATE;…COMMIT;`）、不自己写记账行，
  但**校验**脚本确实记了账（`recorded != 1` 即报错）；执行前用 `VACUUM INTO` 备份（空库跳过备份）；
  脚本报错时显式 `ROLLBACK`（否则唯一写连接会卡在未结束的事务里，后续迁移全堵）；重复版本号报错。
- **交付物 3 事务原语**（`touch.go`）：`DB.Write`（`BEGIN IMMEDIATE` + 串行通道 + 出错回滚）、
  `Tx.Touch`（`UPDATE … version = version + 1 WHERE id = ? RETURNING version` 与事件插入同事务）、
  `Tx.Create`（新增行 + version=1 事件同事务，见 Q2）、`Tx.Append`（append-only 行；聚合表与
  `schema_migrations`/`event_journal` 被拒）。列名白名单**不硬编码**：从 `pragma_table_info` 反射 +
  缓存，`setSQL` 只接受 `column = ?` 形式（正则校验后重建 SQL），`version`/`seq` 为平台保留列。
- **交付物 4 强制脱敏**（`redact.go`）：`InitRedaction(denyKeys, patterns, marker)` 编译正则，
  任一失败即返回 error；未初始化时 `Touch`/`Append`/`Create` 全部报错（没有任何跳过参数）。
  递归遍历 map/slice，命中 deny key 整值替换，字符串值再跑 patterns；预序列化 JSON 字符串也会被解码后
  重跑 deny key。**已对齐 audit 落地的 `Rules()` 语义**：键名走 `CanonicalKey`
  （小写 + 去掉 `-_.`），`vault://` 引用值豁免替换。
- `core/internal/eventjournal/events.go`：52 个事件类型常量 + `Registered()`；`Touch`/`Create` 写事件前
  校验，未登记类型直接拒绝（事件契约 §9.1 的 P0）。
- `000002_aggregate_version.sql`：**实测通过，未作修改**（不是"改对了"，是"验证过它本来就对"）。
- **契约 §1.1 对齐（主控第 2 条）**：
  - §1.1.6 `Create` 改为正文规定的形状 `Create(a AggregateKind, id string, columns []string, values []any, ev EventDraft) error`
    ——`id` 由调用方预生成并单列成参；INSERT 里**显式写 `version = 1`**（不再依赖 ADR-017 的 DEFAULT），
    事件 `aggregate_version` 亦为 1；`columns` 里再给 `id` 直接拒（"id is a Create argument, not a column"）。
    `ErrNoAggregate` → 改名 **`ErrAggregateNotFound`**，且 Touch 对不存在的 id 只报错、**不建行**（已测）。
  - §1.1.7 多列 `setSQL` 早已支持（`"status = ?, updated_at = ?"`），新增用例钉住；
    调用方写 `version` 被 `platformOwned` 拒（原有行为）。`AggTask`/`AggExecution` 等常量本就由本包导出。
  - §1.1.8 `eventjournal.Registered()` 只在本包 `appendEvent`（`Touch`/`Create` 共同入口）校验一次，
    业务包无需重复——全仓仅此一处调用点。
  - §1.1.9 `internal/eventjournal` 保持"目录 + 校验面"：只有 52 个类型常量、catalog 表与 `Registered()`，
    无 payload 结构体、不 import 任何包。**未**往里面加业务类型。
- **巡检 T-7①（REQ-A2，§12 P0）已修**：`redact.go` 删掉 `scrubString` 的 vault 提前返回。
  **并且多修了一处主控没点名的同型漏洞**——`walk()` 里"键命中 deny key 且值以 `vault://` 开头就整值放行"
  是第二条完全等价的泄密路径（`"token": "vault://x sk-<密钥>"` 也会原样落库）。
  现改为 `isCleanVaultRef(s)`：前缀对**且**整串过一遍 patterns 后不变样，才允许存活；
  否则照旧替换。自由文本路径（`note`）不再有任何豁免。取舍与主控一致：丢引用可发现，漏密钥是 P0。
- **巡检 T-7② 已补**：`eventjournal` 增 `AdapterDowngraded = "adapter_downgraded"`、
  `AdapterRestartExhausted = "adapter_restart_exhausted"`，**并同步行写进 `catalog` map**
  （只加常量不加 map，`Registered()` 仍然会说谎）。常量数 52 → 54。adapter 现在可以写这两个类型。
- **巡检 T-6 / Q4 已改**：`AggQuota = "quota_account"` → **`"quota"`**；`aggTable` 映射仍是 `quota_accounts` 表不变。
  已核：全仓（`internal` + `test`）无任何消费方引用旧值或 `ErrNoAggregate`，改名不碎别人。
- **巡检 T-6 其余裁定已核**：Q5（`InitRedaction(denyKeys, patterns []string, marker string) error`）
  与本角色实现一致，未动；Q3（db 只收集、audit 落链）与本角色实现一致；Q1（两种引擎行为都不许依赖 +
  必须回读 `PRAGMA journal_mode`）本角色早已在 `initStorage` 回读断言、并有专测覆盖两种行为。

## Remaining

- 未做（本次四项判据之外，未获指令）：`blobs` 大对象存储路径、迁移文件 `fs.FS`/embed 入口、
  `Tx` 内读方法。任一需要请直接说，各约 10~20 行。
- 已知上限：本机 CGO 不可用 → `-race` 跑不了，并发用例只能证明"逻辑不产生重复版本"，
  不能证明无数据竞争。

## Blockers

- 无。

## Coordination

- **Q1（已按裁定收口）** 契约改为"两种引擎行为都不许依赖 + 必须回读 `PRAGMA journal_mode` 断言实际值"。
  本角色实现本就如此：`initStorage` 设完立即回读、非 `wal` 即 error 不 Ready；
  `TestJournalModeInsideTransactionDoesNotTakeEffect` 对"报错"与"静默无效"两种行为都判通过，
  并断言最终值——不会因引擎换版本而翻红。
- **Q4（已按裁定改）** `AggQuota = "quota"`。
- **给 backend（两轮累积的破坏性变化，改一次就好）**：
  ① `Create` 现按契约 §1.1.6 的形状 `Create(a, id, columns, values, ev)`——`id` 单列成参，
  不要再写进 `columns`；② `db.ErrNoAggregate` 已改名 `db.ErrAggregateNotFound`；
  ③ 建聚合用 `Create`/`Touch`，用 `Append` 写聚合表会被直接拒。
  另：`event_journal.principal_id` 是真外键，`EventDraft.PrincipalID` 非空时必须先有 `principals` 行；
  空串会存成 NULL。`payload` 缺 `schema` 键时平台层自动补 `"schema":1`。
- **给 adapter（T-7② 已解）** `adapter_downgraded` / `adapter_restart_exhausted` 现已登记进 catalog，
  Supervisor 可以直接写；本轮同时补进 `Registered()` 的 map（只有常量而没有 map 条目时校验仍会拒）。
- **Q5（裁定本角色对，实现未动）** `InitRedaction(denyKeys, patterns []string, marker string) error`。
- **Q3（已进契约，仍待 audit 写消费方）** db 只在事务内收集，经
  `tx.RedactionHits() []RedactionHit{Column, Match, Count}` 暴露（只含规则名与计数，不含原值），
  由 audit 在同一事务里自己 `Append("audit_entries", …)` 落链。db 侧接口已就绪，等 audit 用。
- **Q6（新，给 audit）** `internal/audit/redaction.go:60-67` 的 `IsVaultRef` 文档写的是
  "vault 引用值 **must survive redaction untouched**, including when they sit under a denied key"——
  这个**无条件**措辞正是 REQ-A2 的成因（本角色照它实现了豁免）。现 db 侧收窄为
  "前缀对且整串不含任何 pattern 命中"才存活。请 audit 把该注释改成同样有条件，否则下一个照它实现的人
  会再犯一次；`vault://` 语义本身（ADR-012）没变。
- → `cmd/core` 接线：`Migrate` 目前接收**迁移目录路径**（`embed` 需要 `core/migrations` 有包文件，越界）。
  生产要内嵌的话，请主控在 cmd/core 侧建 embed 并传 `fs.FS`，本角色可加一个 `fs.FS` 版入口。

## Changed files

- `core/internal/platform/db/db.go`（新建）— Open / PRAGMA 分层 / 读池 + 单连接写通道
- `core/internal/platform/db/migrate.go`（新建）— 迁移器 + VACUUM INTO 备份
- `core/internal/platform/db/touch.go`（新建）— Write / Tx.Touch / Tx.Create / Tx.Append / 列白名单
- `core/internal/platform/db/redact.go`（新建）— InitRedaction + 递归脱敏 + RedactionHit
- `core/internal/platform/db/db_test.go`、`migrate_test.go`、`touch_test.go`（新建）
- `core/internal/eventjournal/events.go`、`events_test.go`（新建）
- `core/migrations/000002_aggregate_version.sql` — **未改**（实测通过）
- `core/migrations/000001_core_schema.sql` — **未改**（红线①）
- `core/go.mod`、`core/go.sum` — **未改**（归主控；无新依赖需求）

## Verification evidence

命令均在 `D:\Project\Lucero\core` 下执行。

- `D:/DevTools/go/bin/go.exe version` → `go version go1.27.1 windows/amd64`；`go env CGO_ENABLED` → `0`；
  `modernc.org/sqlite@v1.59.0` 已在 `D:\DevTools\go-data\mod`，全程未 `go get`/`go mod tidy`
- **GATE（scoped，本轮最后复跑）** `CGO_ENABLED=0 go test ./internal/platform/... ./internal/eventjournal/... -count=1` →
  `ok internal/platform/db 2.410s` / `ok internal/eventjournal 0.417s`，`gate_exit=0`；
  `go build ./internal/platform/db/` → exit 0；`go vet` 同范围 → exit 0；`go fmt` 无输出。
- **REQ-A2 变异测试（证明用例有牙）**：临时把 `if strings.HasPrefix(s, vaultRefPrefix) { return s }`
  塞回 `scrubString` 后重跑 `TestVaultPrefixDoesNotSmuggleSecret` →
  ```
  --- FAIL: TestVaultPrefixDoesNotSmuggleSecret
      P0: the vault:// prefix smuggled a secret into payload_json:
      {"clean":"vault://github_token","note":"vault://x sk-ABCDEFGHIJKLMNOP",
       "schema":1,"token":"vault://x sk-ABCDEFGHIJKLMNOP","untouched":"vault://plain_ref"}
  ```
  → 该输出同时证明**两条路径都漏**（`note` 是主控点名的 pattern 路径，`token` 是 `walk` 的 deny-key 路径），
  所以第二处必须一起修。恢复修复版后：
  ```
  stored payload: {"clean":"vault://github_token","note":"vault://x [REDACTED]",
                   "schema":1,"token":"[REDACTED]","untouched":"vault://plain_ref"}
  --- PASS
  ```
  干净引用 `vault://github_token`（就在 deny key `token` 下面）仍然存活，未被误伤。
- **T-7③ / Q4 复跑**：`TestAggQuotaMatchesEventPrefix` → PASS，
  日志 `reached the FK as expected: create quota quota-1: constraint failed: FOREIGN KEY constraint failed (787)`
  ——撞到的是 FK 而非"未知聚合"，证明 `quota_observed` + `AggQuota("quota")` 已通过 `appendEvent` 的
  aggregate 校验；并断言 `event_journal` 里 `aggregate_type='quota_account'` 的行数为 0。
- **T-7② 复跑**：`TestRegistered` → PASS，显式断言 `Registered("adapter_downgraded")` 与
  `Registered("adapter_restart_exhausted")` 为真（契约 §6 第 154~155 行登记、代码 catalog 曾缺）。
  常量与 map 条目复核：`grep -cE '^[[:space:]]+[A-Z][A-Za-z]+[[:space:]]+=[[:space:]]+"' events.go` → **54**。
- **改名波及面自查**：`grep -rl "platform/db" --include=*.go internal test` → 现在有
  `internal/audit/redaction.go`(注释)、`internal/quota/domain.go`、`internal/session/domain.go`、
  `internal/session/repository.go`、`test/crash_test.go`；
  对这些包再 grep `AggQuota|quota_account"|ErrAggregateNotFound|ErrNoAggregate` → **零命中**，
  即本包的改名与错误变量更名没有打断任何消费方（`internal/quota`、`internal/session` 测试均 ok）。
- **INFO（全量，不作门禁）** `go test ./... -count=1` 本轮红 2 个包，均与本角色无关，
  且都不涉及 `db.*`：`internal/adapter`（`handshake_test.go` 多处
  `cannot use "" (untyped string constant) as ErrorCode value`）与 `core/test`
  （`adapter_contract_test.go:81:9: cannot use "<not-an-adapter.Error>" as adapter.ErrorCode value`）。
  两者都是 `adapter.ErrorCode` 类型收紧后测试未跟改，转 `adapter` 角色。本角色未触碰。
  （上一轮红的 `internal/task`、`internal/execution` 本轮已自行转绿，不再追记。）
- 范围说明：主控示例里的 `./internal/migration/...` **不存在**——迁移器落在
  `internal/platform/db/migrate.go`，仍在本角色声明的可写范围内。
- `CGO_ENABLED=0 go test ./internal/platform/db/ -count=2` → `ok … 2.677s`（全局脱敏状态不会串测试）
- 复核越界：`internal/eventjournal/events.go` 事件常量 52 个（T-7② 后为 **54**，见上一条复核）；
  `grep -rn "FROM approvals\|UPDATE approvals\|INSERT INTO approvals" internal --include=*.go` → **全仓零命中**；
  本角色两个包里 `approvals` 只作为表名字符串出现（`touch.go:38` 白名单映射、`migrate_test.go:102` 断言列表）。
- **回主控第 1 条「我的包编译不过」：不可复现**。backend 报的三处正是本角色 16:05 前自己修掉的中间态：
  - `CGO_ENABLED=0 go build ./internal/platform/db/` → **exit 0**（改 §1.1 前后各跑一次，都 0）
  - `redact.go:19` 现内容为修好后的声明 `var red atomic.Pointer[redactor]`；
    报错原文 `atomic.Pointer[redactor] (type) is not an expression` 对应的是我漏写 `var` 的那一版，
    同一次 `go build` 输出里我自己看到并当场修掉了
  - `d.columns` 定义在 `internal/platform/db/touch.go:344`（与 308/322 同包），
    `grep -n "func (d \*DB) columns" internal/platform/db/*.go` → 唯一命中该行
  - 文件 mtime 全为本角色写入（15:48~16:09），无第三方改动痕迹
  → 结论：backend 读的是中间态快照。请让其重跑 `go build ./internal/platform/db/`；
    现在更需注意的变化是 **`Create` 多了 `id` 参数**、**`ErrNoAggregate` 改名 `ErrAggregateNotFound`**（§1.1.6）。
- 逐项验收的观测输出（`-v`）：
  - `linked SQLite = 3.53.4`；STRICT 表建成并**拒绝**字符串写入 INTEGER 列；FTS5 虚拟表建成 → 驱动判定成立
  - `journal_mode = "wal"`；新读连接上 `foreign_keys=1`/`busy_timeout=5000`/`synchronous=2`
  - `in-tx PRAGMA journal_mode=WAL rejected: SQL logic error: cannot change into wal mode from within a transaction (1)`
    → 事务内改不动，事务外改为 wal 并持久（Q1 的实测依据）
  - 迁移：`ledger after migrate = [1 2]`；重跑后仍 `[1 2]`；10 张聚合表 `pragma_table_info` 均报
    `version` 且 `notnull=1 default=1`；空库备份数 0、有表时备份数 1
  - 半途失败：`apply migration 000003_broken.sql: SQL logic error: no such table: no_such_table (1)` →
    `PRAGMA integrity_check = ok`、ledger 仍 `[1 2]`、`half_baked` 表不存在、修好脚本后能续跑到 `[1 2 3]`
  - 未记账脚本：`migration 000003_unbooked.sql changed the schema but recorded nothing (want exactly 1 ledger row, got 0)`
  - `RETURNING version`：同一任务连拿 `1,2,3,4,5`，`event_journal` 里该聚合的版本正是 `[1 2 3 4 5]`
  - 并发：6 goroutine × 8 次 Touch 同一行 → 48 个互不相同的版本、零错误，
    journal 内 `GROUP BY aggregate_version HAVING count(*)>1` 为 0 行
  - 绕过写：`UNIQUE constraint failed: event_journal.aggregate_type, event_journal.aggregate_id,
    event_journal.aggregate_version (2067)`；同语句换版本号为 99 则成功（证明被拒原因确是约束）
  - 脱敏入库后的实际行：
    `{"authorization":"[REDACTED]","keep":"visible","list":[{"password":"[REDACTED]"},"safe"],
     "nested":{"API_KEY":"[REDACTED]"},"note":"the header was [REDACTED]","schema":1,
     "token":"vault://github_token","x-api-key":"[REDACTED]"}`
    → 原值 `super-secret-value` / `sk-abc…` / `hunter2hunter2` / bearer 载荷全部不在库内；
    `vault://` 引用存活；自由文本靠 pattern 命中
  - fail-closed：`redaction pattern "(unclosed": error parsing regexp: missing closing )`；
    未初始化时 `column tasks.id: redaction is not initialized (call db.InitRedaction before any write)`，
    且失败的 `Create` 没留下 `tasks` 行
  - 白名单：`status = ?; DROP TABLE tasks` / `status = 'RUNNING'` / 未知列 / `version = ?` / `id = ?` / 空 SET
    六种全部被拒，`tasks` 表仍在、版本仍为 2
  - 未登记事件类型 `TaskCreated` 被拒后，同一事务里的 `UPDATE` 一起回滚（版本仍 2、事件 0 行）
- 越界自查：`git diff 900038d --stat -- core/go.mod core/go.sum core/migrations/` → 仅 go.mod(+13)/go.sum(+20)，
  均为主控在基线后的改动；`core/migrations/` 零 diff
- §1.1 新增两测的实测输出：
  `TestTouchDoesNotCreateAndCreateStartsAtVersionOne` → PASS，
  日志 `duplicate id refused: create task task-c1: constraint failed: UNIQUE constraint failed: tasks.id (1555)`、
  `columns [id title] refused: create task: id is a Create argument, not a column`、
  `columns [version title] refused: column version belongs to the platform, not to a caller`；
  并断言 `Touch("ghost")` 返回 `ErrAggregateNotFound` 后 `tasks` 行数与事件行数均为 0、
  新任务 `version=1` 且其 `task_created` 事件 `aggregate_version=1`。
  `TestTouchAcceptsMultipleColumns` → PASS（三列一次改，版本 2→3，`status`/`priority` 回读一致）。

## Recovery note

- Interruption point: 巡检 T-6/T-7 三条全部落地并实测（含 REQ-A2 的变异测试），scoped 门禁 exit 0；
  仅余 Q3（等 audit 写 `RedactionHits()` 的消费方）与 Q6（等 audit 收窄 `IsVaultRef` 注释）
- First action after recovery: 重跑 scoped 门禁确认仍绿；若 `internal/quota` 或 `internal/session`
  报"未知聚合 / aggregate_type 不符"，先查它们是否还在用旧字符串 `"quota_account"`

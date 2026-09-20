OWNER_ROLE: backend
TASK_ID: backend-core
STATUS: in_progress
SCOPE: Task / Execution / Session / Quota 四个业务包的领域类型、状态机、投影与只读查询；不写迁移、不碰 approvals 表
EVIDENCE_DIR: none
LAST_UPDATED: 2026-09-19T17:30+08:00

## Objective

按 event-contract-v0.1.md §5 把 Task 与 Execution 两套状态机做成矩阵驱动的可执行规格，
并用测试证明：合法迁移全覆盖、矩阵外一律 ErrIllegalTransition、UNKNOWN 不被自动转成确定值、
Task 不镜像 Execution、AGENT_COMPLETED != ACCEPTED、换 Provider 不新建 Task。

## Completion criteria

- [x] `CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test ./internal/task/... ./internal/execution/...` 通过（实跑见 Verification evidence）
- [x] §5.1 / §5.2 每条合法迁移都有用例（75 + 35 条，测试内独立手抄第二份表与实现逐行对账）
- [x] 矩阵外迁移返回 ErrIllegalTransition，非法用例 24（Task）+ 20（Execution）条，含四个终态往外跳
- [x] UNKNOWN 只能由 Recovery + 证据进出，且不被转成 FAILED/0/Offline
- [x] §5.3 投影：Execution RUNNING→WAITING_INPUT→RUNNING 不产生 Task 事件
- [x] AGENT_COMPLETED != ACCEPTED（直跳被拒）
- [x] Provider 切换/Handoff 三次链式尝试 TaskID 不变、attempt_no 递增、三个不同 Session
- [x] C1~C7 裁定落地：§5.4 拒绝记录形状、C2 的 5 个新事件类型 + §6 目录对账测试（四包各一张表）
- [x] `internal/session` / `internal/quota` 领域类型 + **只读** Repository，跑在真表上（直接执行 000001+000002）
- [ ] 五包的**写侧** Service —— 已解除阻塞并开始交付：`task.Service.ApplyExecutionEvent`（§5.3/§5.4/If-Match）
      完成；`Create`/`Queue` 等其余动作与 `execution.Service` 未做（见 Remaining）
- [x] §5.4 的"投影被拒 → `event_journal` 零新增、Task 状态与版本未变" —— 真库真表差分断言（第 18 条），
      并按变异验证确认断言不是空的（第 19 条）
- [x] §5.4 的另一半"Notification + Audit 各一条" —— 第六批闭环（真表 + 真 notification.Service，第 32 条）；
      Audit 侧目前是接缝 + 计数用例，生产适配器属 `cmd/core` 接线（见 Remaining）
- [x] `internal/notification` 新归属：`Service.Notify` + 只读 `Get`/`ListByState` + §6 键对账（第 31 条五包全绿）

## Scope boundaries

- In scope: `core/internal/task/**`、`core/internal/execution/**`、`core/internal/session/**`、
  `core/internal/quota/**`；C12 裁定（契约③ §7 补行）另把 **`core/internal/notification/**`** 归 backend；
  契约③ §7（16:52 补）另把 `core/api/http/**`、`core/api/websocket/**`、`core/cmd/core/**` 也归了 backend，
  限定为"业务 Service 的薄投影，不得含业务判断或状态迁移"
- Out of scope: `core/go.mod`（主控）、`core/migrations/*.sql` 与 `internal/platform/**`、`internal/eventjournal/**`（db）、
  `internal/approval|permission|audit`（audit）、`core/test/**` 与 `scripts/**`（test）

## Current checkpoint

- Stage: Wave-2 backend 第六批 —— B-backend-2/3 均解除；写侧第一步（§5.3/§5.4 落库路径）+ notification 包落地跑绿
- Status: in_progress（写侧目前只完成 `ApplyExecutionEvent`；其余 Task 动作、transport 层未做）
- Last verified action: Verification evidence 第 30~34 条
- Next action: 给 `task.Service` 补剩余状态动作（`Create`/`Queue`/`Prepare`/`Pause`/`Resume`/`Cancel`/`Accept`），
  复用本轮那条同事务路径（读 status+version → If-Match → `Check` → `Touch`），每个动作一条真表用例
- Verification: `CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 180s ./internal/task/... ./internal/execution/... ./internal/session/... ./internal/quota/... ./internal/notification/` 全 ok

## Completed

- `core/internal/task/statemachine.go` — §5.1 全 17 态逐行拄成 `successors map[State]map[State]bool`
  （value=true 即 recovery-only），`Check(from,to,Occasion,Evidence)` 为唯一裁决点，终态 ACCEPTED/CANCELLED。
- `core/internal/task/projection.go` — §5.3 七条边沿；投影输出必须再过 `Check`，因此造不出矩阵外的状态。
- `core/internal/task/events.go` — §6 的 13 个 `task_*` 事件类型常量 + 每个类型一个 payload 结构体 +
  `Payload()` 强制注入 `"schema":1`（§2 信封）。
- `core/internal/execution/{statemachine,events,domain,approval}.go` — §5.2 全 12 态矩阵、
  `EventOf(from,to)`（WAITING_INPUT 等迁移返回 `""` = 不产生事件）、§23 八个 TriggerType、
  `Execution.NewAttempt`（ADR-013/015：换 Provider 只产生新 Execution，TaskID 原样传递）、
  本地接口 `ApprovalStates.BlockingState`（由 cmd/core 注入 internal/approval，本包不 import 它）。
- 测试 5 个文件、17 个用例函数。

### 第二批（同会话，C1~C7 裁定之后）

- `task/projection.go` — 按 §5.4 改成三值返回 `(State, bool, Rejection)`；`Rejection` 的三个 json 键
  就是 §5.4 要求 Audit `details_json` 带的 `task_state` / `execution_event` / `reason`，写侧照搬即可、不会再漂移。
  全表扫描证明：任何被拒投影都不改状态、且解释非空。
- `task/events.go` / `execution/events.go` — 补登 C2 新增的 5 个事件类型与 payload：
  `task_paused` / `task_blocked` / `task_resumed(from)` / `execution_paused` / `execution_resumed(from)`；
  各自加 `RequiresAudit()`，取值逐行对齐 §6 的 ✓ 列。
- `task/events_test.go`、`execution/events_test.go`、`session/repository_test.go`、`quota/repository_test.go`
  — 四个包各有一张独立手抄的 §6 目录表，双向对账（我声明了但目录没有 = §9.1 P0；目录有而我没实现 = 漏做），
  并把每个 payload 结构体 marshal 后与目录的"payload 关键字段"列逐键比对。
- `execution/events.go` 的 `EventOf` 补齐：`RUNNING→PAUSED = execution_paused`；
  `PAUSED|WAITING_INPUT|INTERRUPTED → RUNNING = execution_resumed`；`WAITING_APPROVAL→RUNNING` 仍是 `""`
  （那条事件归 approval 包）。**待主控确认**：把 `INTERRUPTED→RUNNING` 也算 resumed 是我的推断（见 C9）。
- `internal/session/{domain,events,repository}.go` — Session/Message 领域类型 + §13 八个 SyncState、
  §49 四级 Confidence、§20 六个 link role + **只读** Repository（`Get` / `ForTask` / `ListMessages`）。
  `ForTask` 就是内部契约 §4 要求的 `session` 侧走法（task 包不暴露 link 表）。
- `internal/quota/{domain,events,repository}.go` — Account/Snapshot 类型 + §31 七个 quota_type、
  OBSERVED/ESTIMATED、四级 confidence + **只读** Repository（`Account` / `AccountsForAdapter` /
  `LatestSnapshots`：每个 quota_key 只取最新一条，同刻并列用 id 定序）。
- 两个 Repository 的依赖都是本包自定义的最小 `Queryer` 接口（这个工具链的 `database/sql` 没导出
  `sql.QueryerContext`，实测 `go doc database/sql` 查无此符号），`*sql.DB` 天然满足 → 不与 db 包耦合。
- 生产代码零写 SQL 已核：`grep -rn "INSERT INTO|UPDATE |DELETE |approvals" --include=[a-z]*.go`（排除 _test.go）
  只命中 `execution/approval.go` 的一行注释。
  **本批更新：这条已经作废** —— 写侧自 `task/service.go` 起就在生产代码里读 `tasks`（`tx.QueryRowContext`）
  并经 `Tx.Touch`/`Tx.Create` 落库。仍然成立的部分（18:1x 当场重跑）：
  `grep -rn "INSERT INTO\|UPDATE \|^DELETE FROM" 我这五包 --include=*.go | grep -v _test.go` → **零命中**（生产代码不写裸 SQL）；
  `grep -rn "approvals" 这五包 --include=*.go | wc -l` → **1**，就是 `execution/approval.go:7` 那行注释，
  即 `approvals` 表零读写。
- session/quota 的测试直接执行 db 角色的 `000001`+`000002` 两份 .sql 建**真表**后跑读查询
  （不复制 DDL、不改文件、不引新依赖）；因此这轮读侧的词表/列名/NULL 处理都是实测而非推测。

### 第六批：写侧第一步 + `internal/notification`（B-backend-2/3 解除后）

- `internal/notification/{domain,events,service,repository}.go` — 新归属落地。
  `Service.Notify(ctx, tx Writer, Draft)`：校验 Draft（缺 id/event_id/type/title 或 priority 不在词表 →
  `ErrInvalidDraft`，且不留任何行）→ `Tx.Create(db.AggNotification, …)` 发 `notification_created`，
  默认 `state=UNREAD` / `priority=MEDIUM` / `version=1`。三个可空外键**只在真有值时进列**（空串=非 NULL，
  会直接撞 `task_id`/`approval_id` 外键 —— 这条有专门用例）。只读侧 `Get` / `ListByState`。
  audit/adapter 要发通知只能调这个 Service，不碰 `notifications` 表。
- `internal/task/service.go` — `Service.ApplyExecutionEvent`：§1.1 第 11 条那条顺序的落地，
  同一事务内 `SELECT status, version` → 比对 If-Match（`WantVersion<=0` = 不设前置，给 Scheduler/Recovery 用）
  → §5.3 投影 → 合法才 `Tx.Touch`（一版本一事件，事件类型按 §6 换），被拒则交注入的 `RejectionReporter`
  写 §5.4 那两条记录并**不 Touch**。状态机的 `from` 一律取自库里，不接受调用方口头给。
  没注入 reporter 时直接报错，不静默吞（§5.4 第 3 条）。
  两个 id（Notification + 事件）由调用方预生成 —— 契约 §1 明确 EventID 调用方生成以便重试复用，
  所以我没有引入 `github.com/google/uuid`（那会动到主控独占的 go.mod 语义）。
- `internal/task/rejection_db_test.go` 的计数按 §5.4 **更正后的口径**收窄到 `aggregate_type='task'`，
  否则那条断言会把"应当恰有一行 notification_created"误判成违规。
- 完成判据里"§5.4 的另一半（Notification+Audit 各一条）"本轮已交付，见第 32 条。

## Remaining

- `task.Service` 的其余状态动作：`Create`（`Tx.Create` + `task_created`）、`Queue`/`Prepare`/`Pause`/`Resume`/
  `Cancel`/`Accept`，全部复用本轮那条同事务路径；§46 接口里还差 `Transfer`（要 handoff 包，未建）。
- `execution.Service`（写侧）：`Create` 首行 + `STARTING`/`RUNNING` 推进，事件类型用 `EventOf` 决定；
  `WAITING_APPROVAL` 那一步先问 `ApprovalStates.BlockingState`（接缝已在 `execution/approval.go`）。
- §5.4 那条 Audit 的**生产实现**：接缝 `RejectionReporter` 已定义并有用例，但把 `RejectionReport` 映射成
  `audit.Entry` + `audit.Chain` 的适配器属 `cmd/core` 接线（还没建，见 C10）。
- 列表读的 keyset cursor（契约③ §2 禁 offset）：`task.ListByStatus` / `notification.ListByState` 现在只有 limit，
  源码里带 `ponytail:` 标注，等 transport 落地时统一加。
- `session.Service` / `quota.Service` 公开面，以及 §3 新补的 `POST /executions/{id}/send` 所需的面。
- §7 垂直链的完整端到端测试属 `test` 角色；我这五包只有 Task 半边（`TestVerticalChainTaskHalf`）。

## Blockers

- ~~B-backend-2 写侧落库不可写~~ **已解除**（T-19 裁定 + 当场实测）：`internal/platform/db/touch.go:135`
  有 `func (tx *Tx) QueryRowContext(...)`、`:139` 有 `QueryContext(...)`，If-Match 按 §1.1 第 11 条
  做成"同事务 SELECT version → 比对 → 不符返回 `ErrVersionConflict` 且不 Touch"，`TouchIfVersion` 那条路
  已被主控否掉，我没再要求。
- ~~B-backend-3 Notification 无主~~ **已解除**（C12 裁定）：`core/internal/notification/**` 归 backend，
  本轮已建包；`Tx.Create(db.AggNotification, …)` 发 `notification_created`，audit/adapter 要发通知只能调
  `notification.Service.Notify`。§5.4 更正后的口径（Task 聚合零新增版本/零事件、恰一行
  `notification_created`、Audit 走行哈希链）已用真表跑通，见第 32 条。
- **B-backend-4（新，别人家的红，不属我范围）** `internal/approval`（audit 角色）编译不过：
  `permission` 的 `Decide` 现在返回三个值，他们的 `vertical_slice_test.go:58` 等 10 处还是两个变量的旧写法。
  交付前当场重跑确认仍红（第 34 条原文），我没碰那个目录。提醒 audit 注意 §1.1 第 10 条那类连锁：
  改导出签名要在同一轮改完自己的调用点/用例。

### 第五批：处理巡检投递 T-8（含 T-4）

25. **T-8② session 超时复跑**（原文，命令照主控给的）：
    `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test ./internal/session/ -count=1 -timeout 90s -v`
    ```
    === RUN   TestForTaskReturnsSessionsOfEveryProvider
    --- PASS: TestForTaskReturnsSessionsOfEveryProvider (0.01s)
    --- PASS: TestGetDoesNotCoerceUnknown (0.01s)
    --- PASS: TestListMessagesOrdersBySequenceThenTime (0.01s)
    --- PASS: TestEventCatalogMatchesContract6 (0.00s)
    --- PASS: TestWordListsMatchSchemaChecks (0.01s)
    PASS
    ok  	github.com/SadCat18/lucero/core/internal/session	1.665s
    ```
    结论：**没有超时**，1.665s。那个 601.566s 是第三批（第 12 条）定位并修掉的 `ForTask` 单连接死锁
    —— 边持游标边对同一个 `*sql.DB` 发第二条查询。栈里的 `sql.DB.connectionOpener` 是任何 `sql.DB`
    都有的后台 goroutine，不是"开了库等不到东西"的证据；真正的证据是超时 + 栈停在 `(*DB).query`。
    修完到现在每次重跑都是 1.5~1.7s。vet 通过确实不等于测试通过，所以我按主控要求跑的是测试而不是只交 vet。
26. **T-8③ 注释事实核实：我那句是错的，但投递给的理由也不准确 —— 结论是符号在另一个包。**
    四条可复现命令的实测结果：
    ```
    $ go doc database/sql                    # 导出类型里没有任何 Queryer / QueryerContext / Execer
    $ go doc database/sql.QueryerContext     # doc: no symbol QueryerContext in package database/sql
    $ var _ sql.QueryerContext               # 编译报 undefined: sql.QueryerContext
    $ go doc database/sql/driver.QueryerContext
    package driver // import "database/sql/driver"
    type QueryerContext interface { QueryContext(ctx, query string, args []NamedValue) (Rows, error) }
    ```
    即：`QueryerContext` 从来只在 `database/sql/driver`（Go 1.8 加的就是 driver 包），
    而且是**驱动侧 SPI**（参数 `[]driver.NamedValue`、返回 `driver.Rows`），`*sql.DB` 并不满足它；
    `database/sql` 压根没导出过这个名字。所以我上一轮注释里"本工具链没导出它"的框定是错的（好像这台机器异常），
    投递里"`database/sql` 自 Go 1.8 起导出 `QueryerContext`"同样是错的（包名搞错）。
    做法不变（窄 `Queryer` 保留），四包注释已改成上面这个准确理由，证据命令留在
    `internal/session/repository.go`。探针目录 `core/.scratch/` 已删（它在 module 内，留着会红 `go build ./...`）。
27. **T-4 两处破坏性变更：我的代码本来就没踩**（实测过才敢说）：
    `tx.Create(db.AggTask, id, []string{"title", ...}, ...)` 里 `id` 一直是独立参数、没进 `columns`
    （`TestRepositoryReadsWhatTouchWrote` / `TestPayloadLandsInJournalWithSchema` / `rejection_db_test.go`
    三处真表写入全过）；`ErrAggregateNotFound` 我还没在业务代码里引用（写侧 Service 才用得上）。
    聚合表从不用 `Append` 写：只 `Append("principals", ...)`，而 principals 不在 aggTables 里。
    `event_journal.principal_id` 外键这件事我在第 18 条之前就是靠"先建 principals 行"过的，
    与 db 给的事实一致（空 PrincipalID → NULL，不撞外键）。
28. **`"schema":1` 由平台层补 → 我把业务包里那份重复保证删了**（C8 就此关闭）：
    `task.Payload()` / `execution.Payload()` 现在只做 struct→map；两个 catalog 测试的期望键
    改成与 §6 的"payload 关键字段"列**逐键相等**（不再自己加一个 schema 键），
    另两个重复的 `TestPayloadCarriesSchemaVersion` 删除；保证本身改由真表钉：
    `TestPayloadLandsInJournalWithSchema` 读 `event_journal.payload_json` 断言含 `"schema":1`
    且 `title`/`priority` 在场 —— 少一个环节都会红。
29. **交付前最后一次重跑**（第 26 条的四包注释改动之后，此后只动过 markdown）：
    `CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 120s ./internal/task/... ./internal/execution/... ./internal/session/... ./internal/quota/...`
    ```
    ok  	github.com/SadCat18/lucero/core/internal/task	1.776s
    ok  	github.com/SadCat18/lucero/core/internal/execution	1.704s
    ok  	github.com/SadCat18/lucero/core/internal/session	1.596s
    ok  	github.com/SadCat18/lucero/core/internal/quota	1.580s
    ```
    `gofmt -l internal/` → 空；`go vet` 这四包 → 无输出。
    （更早一次同批重跑是 `1.826 / 1.731 / 1.607 / 1.582`，那是注释改动之前，只作过程记录。）

### 第六批：写侧第一步 + notification 包（B-backend-2/3 解除后）

30. **B-backend-2 解除的当场证据**：`grep -n "func (tx \*Tx)" internal/platform/db/touch.go` →
    `125 RedactionHits / 135 QueryRowContext / 139 QueryContext / 146 Touch / 200 Create / 236 Append`；
    `AggNotification` 也在注册表里（`touch.go:32`，`:45` 映射到 `notifications`）。
31. **交付前最后一次重跑**（本批所有代码改动之后，此后只动过 markdown）：
    `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 180s ./internal/task/... ./internal/execution/... ./internal/session/... ./internal/quota/... ./internal/notification/`
    ```
    ok  	github.com/SadCat18/lucero/core/internal/task	1.879s
    ok  	github.com/SadCat18/lucero/core/internal/execution	1.640s
    ok  	github.com/SadCat18/lucero/core/internal/session	1.540s
    ok  	github.com/SadCat18/lucero/core/internal/quota	1.546s
    ok  	github.com/SadCat18/lucero/core/internal/notification	1.634s
    ```
    同批 `gofmt -l` 这五包 → 空；`go vet` 这五包 → 无输出。
32. **§5.4 闭环跑通**（`TestApplyExecutionEventRejection`，真库真表 + 真 `notification.Service`）：
    Task 停在 `PREPARING` v1、`aggregate_type='task'` 事件零新增；`aggregate_type='notification'` 恰一行
    （`notification_type=projection_rejected`、`state=UNREAD`、`priority=HIGH`、`task_id` 指向那条 Task）；
    Audit 恰一次；`Result.Rejection.Reason` 非空。同组另三条：推进路径 `→ WAITING_APPROVAL v2` 且
    `task_waiting_approval` 的 payload 真带 `approval_id`；If-Match 不符 → `ErrVersionConflict`，
    事件行与状态版本都没动；reporter 为 nil → 返回 error 而不是静默吞。
33. 第 32 条那组断言做过**变异验证**：临时让被拒分支照样 `Touch`，用例按要求失败（对照组不受影响）：
    ```
    --- FAIL: TestApplyExecutionEventRejection (0.04s)
        service_test.go:166: §5.4：aggregate_type=task 的事件必须零新增（只剩建行那条），实际 2
    ```
    变异代码从 `/tmp/service.go.bak` 还原后才跑的第 31 条。
34. **别人的红（只报不修）**：`go test -count=1 ./internal/approval/` →
    ```
    internalpprovalertical_slice_test.go:58:17: assignment mismatch: 2 variables but eng.Decide returns 3 values
    （:61 :67 :70 :87 :98 :108 :115 :121 :202 同错，too many errors）
    FAIL	github.com/SadCat18/lucero/core/internal/approval [build failed]
    ```
    `permission.Decide` 改三值返回后 audit 侧调用点没同批改完 —— 正是内部契约 §1.1 第 10 条说的连锁。
    全仓其余包当场 `ok`（第六批那次 `./internal/...` 除这一包外十个包全绿），只有它红。

## Coordination

- **C1 → 已裁定为 §5.4（关闭）** 实现照裁定走：`ProjectTaskState` 现在返回 `(State, bool, Rejection)`，
  `Rejection` 的 json 键 = `task_state`/`execution_event`/`reason`。
  **还差的半条**：Notification+Audit 各一条、`event_journal` 零新增的端到端断言，需要写侧才能测（B-backend-2）。
- **C2 → 已补目录（关闭）** `task_paused`/`task_blocked`/`task_resumed`/`execution_paused`/`execution_resumed`
  五条我已登记进 payload + 常量，并被 §6 目录对账测试逐键钉住。`task_resumed.from` 区分 PAUSED/BLOCKED ✓。
- **C3 → 已裁定为 §5.5（关闭）** 与我实现的行为一致（进/出都要 Recovery + 非空 Evidence + 起点非终态；
  终态永不改判 UNKNOWN），代码未改。`TestUnknownIsNotCoercible` 两包各一条继续守着。
- **C4 → 已裁定（关闭）** ① payload 结构体长期留在业务包（不搬）；② `Registered()` 校验由
  `db.Touch`/`db.Create` 单点做，业务包不再各自校验。eventjournal 实测只有 catalog + `Registered()`。
- **C5 → db（已裁定并实测）** §1.1 规则 6~7 确认了 `Create` 是建首行的入口、多列 `setSQL` 合法、
  `AggTask`/`AggExecution` 由 db 导出。我实测 `grep` 到 `AggTask`/`AggExecution`/`ErrAggregateNotFound` 均在。
  本条关闭。
- **C6 → 已解除** db 角色自己修好了 `internal/platform/db`（17:4x 实测 `go build` 干净 + 其测试 ok），未越界动手。
- **C7 → 主控（已裁定 + 一条新事实）** `go.mod`/`go.sum` 归主控独占，`+13/+20` 属 `26616e9`（15:41），确认非我。
  新事实：工作树的 `core/go.sum` 现在比 HEAD **多 15 行未提交**（全是 modernc 的构建期依赖图 `/go.mod` 哈希：
  `modernc.org/cc|ccgo|gc|opt|token|fileutil|goabi0`、`golang.org/x/{mod,sync,tools}`、`google/pprof`、
  `hashicorp/golang-lru/v2`），`core/go.mod` 未变。这 15 行出现在 16:2x 之后，而我那之后的 go 命令全是
  默认 `-mod=readonly`（不会写 go.sum），我的两次 `-mod=mod` 都在 16:2x 快照之前、且当时 go.sum 正好还是
  20 行 = HEAD 状态 → 不是我加的。归属请主控查（很可能是 db 在建 platform 层时跑的）。
  我不碰 `go.*`，只做提醒。
- **C8 → 已关闭（T-4 给出事实后我自己收的）** db 确认 `payload` 缺 `schema` 键时平台层补 `"schema":1`，
  所以我删掉了 `task`/`execution` 两份 `Payload()` 里的重复注入与两个重复用例，改为真表断言
  `TestPayloadLandsInJournalWithSchema`（读 `event_journal.payload_json`）。
  `task`/`execution` 仍保留 struct→map 的 marshal 助手（C4-① 要求 payload 留业务包），只是不再各自保证 schema。
- **C15 → 主控（T-8③ 的核对结果：两边说法都不准，正解在第三个包）** 我说"这台工具链没导出
  `sql.QueryerContext`"、投递说"`database/sql` 自 Go 1.8 起就导出了"——实测 `QueryerContext` 只在
  **`database/sql/driver`**（Go 1.8 新增的正是 driver 包那批 `*Context` 接口），且是驱动侧 SPI
  （`[]driver.NamedValue` / `driver.Rows`），`*sql.DB` 不满足；`database/sql` 从来没导出过这个名字。
  完整命令与输出见 Verification 第 26 条。窄 `Queryer` 按主控意见保留，四包注释已改成准确理由。
  不需要动工具链，也不需要改任何契约；这条只是把事实钉平，别让下一个人再去试 `sql.QueryerContext`。
- **C9 → 主控（我的推断，需确认）** C2 补登 `execution_resumed`（payload `from`）后，我把它定义为
  `PAUSED|WAITING_INPUT|INTERRUPTED → RUNNING` 三条边共用；其中 `INTERRUPTED→RUNNING` 归 resumed
  是我的推断（§5.2 只给了边，§6 没说 from 的取值集合）。若只想要前两条，删 `events.go` 里那一个 case 即可，
  其余不动。
- **C10 → 已裁定（关闭，只剩 handler 待做）** 主控 16:52 确认 `core/api/{http,websocket}/**` 与 `core/cmd/core/**` 归 backend，
  三条限制我复述确认：① handler 是薄层，零业务判断/零状态迁移，只调我 `internal/*` 的 Service；
  ② WS 推送只能在 `Write` 成功返回之后交给 EventBus（ADR-008，先推后写算 P0）；
  ③ 只绑 `127.0.0.1`、除 `/health` 外必带 `Authorization: Bearer <core-token>`、不开 CORS、
  写操作缺 `X-Lucero-Principal: local-user` 即 401（§1 第 4 条：`system`/adapter principal 不接受外部声明）。
  优先级按主控给的顺序排：修红（已完成）> 写侧 Service（第六批已开工）> handler。
  `cmd/core` 骨架仍然不搭：Config/Vault/Recovery/Scheduler/AdapterSupervisor 五个组件都还不存在，
  现在写等于摆一排 nil 占位（ponytail：不建没人的脚手架）。唯一例外是 §5.4 那条
  `RejectionReporter`→(`notification.Service` + `audit.Chain`) 的适配器，它两边都已存在，等下轮开 `cmd/core` 时第一批就是它。
  **REQ-F-1 已答**：`POST /api/v1/executions/{id}/send`（body 带 `delivery`、必带 Idempotency-Key、
  不支持的态返 `DELIVERY_MODE_UNSUPPORTED`）已补进契约③ §3，handler 面按那份走。
- **C11 → 已解除（关闭）** db 已按 §1.1 第 11 条交付 `tx.QueryRowContext` / `tx.QueryContext`
  （`internal/platform/db/touch.go:135/139`），写侧已用上（第 30 条）。
- **C12 → 已裁定并落地（关闭）** ① `internal/notification/**` 归 backend，第六批已建包，
    audit/adapter 发通知只能调 `notification.Service.Notify`；
  ② "零新增"歧义已按主控的更正实现并测：口径是 **`aggregate_type=task` 零新增版本零事件**，
    允许且应当恰有一行 `notification_created`（`aggregate_type=notification`），Audit 走行哈希链。
    我的两处计数断言都收窄到 `aggregate_type='task'`（第 32、33 条），真表验证过。
- **C13 → 主控/db（只记录，不自行改）** 内部契约 §2.2 写的是 `db.InitRedaction(r audit.RedactionRules) error`，
  实现是 `db.InitRedaction(denyKeys []string, patterns []string, marker string)`。形状不同但可接线，
  我按实现写。谁改哪边请裁。
- **C14 → 主控（跨包耦合的教训，来自本轮真实事故）** 我一度在 `internal/task` 的测试里 `import internal/audit`
  取 `audit.Rules()`，正好撞上 audit 包在途的 `undefined: sync` → **我的包**变红。
  已改成测试内自带最小规则集（权威仍是 `audit.Rules()`，本包不测脱敏），并在注释里指向本条。
  建议写进 §9.2：业务包（含其测试）不 import 别的业务/规则包拿配置，接线只发生在 `cmd/core`（§2.3 本来就这么定）。

## Changed files

第一批（纯领域层）：
- `core/internal/task/statemachine.go`（新建）— §5.1 矩阵 + Check
- `core/internal/task/projection.go`（新建→改）— §5.3 投影，第二批按 §5.4 加了 `Rejection` 返回值
- `core/internal/task/events.go`（新建→改）— §6 task 域事件类型与 payload，第二批补 3 条 + `RequiresAudit`
- `core/internal/task/statemachine_test.go`、`projection_test.go`（新建→改）、`events_test.go`（新建）
- `core/internal/execution/statemachine.go`、`domain.go`、`approval.go`（新建）
- `core/internal/execution/events.go`（新建→改）— 补 `execution_paused`/`execution_resumed`、
  `EventOf` 扩表、`RequiresAudit`、`ResultPayload` 去掉 omitempty
- `core/internal/execution/statemachine_test.go`（改）、`events_test.go`（新建→改）

第二批（session/quota 只读侧 + 裁定落地）：
- `core/internal/session/domain.go`（新建）— Session/Message 类型 + §13/§49/§20 三张词表
- `core/internal/session/events.go`（新建）— §6 的 4 个 session 域 payload
- `core/internal/session/repository.go`（新建）— 只读 `Get` / `ForTask` / `ListMessages` + 最小 `Queryer`
- `core/internal/session/repository_test.go`（新建）— 真表（跑 000001+000002）读测试 + §6 对账 + 词表 vs CHECK
- `core/internal/quota/domain.go`、`events.go`、`repository.go`、`repository_test.go`（新建）— 同上形状，
  `LatestSnapshots` 每个 quota_key 只出一条、NULL 额度保持 nil
- `.qoder/progress/backend.md`（本文件）

第三批（修红 + §5.4 真表断言）：
- `core/internal/task/rejection_db_test.go`（新建）— 真库真表的 §5.4 差分测试（拒投影 0 事件/状态版本不动
  vs 合法投影 +1 事件/version 2）；`internal/task` 的调用点已全部对齐三值签名

我未创建/修改：`core/api/**`、`core/cmd/**`（见 C10）、`core/migrations/**`、`internal/platform/**`、
`internal/eventjournal/**`、`internal/approval|permission|audit/**`、`core/go.mod`、`core/go.sum`、`specs/**`。

第六批（写侧第一步 + notification 新包）：
- `core/internal/notification/{domain,events,service,repository}.go`（新建，C12 新归属）
- `core/internal/notification/service_test.go`（新建）— 真表：一行通知+一行事件、外键 NULL 安全、五种坏 Draft 全拒且不留行、§6 键对账
- `core/internal/task/service.go`（新建）— `ApplyExecutionEvent` + `RejectionReporter`/`RejectionReport`/`Command`/`Result` + `ErrVersionConflict`
- `core/internal/task/service_test.go`（新建）— 真表 4 条：推进、If-Match 409、§5.4 闭环（真 notification.Service + Audit 计数）、无 reporter 必失败
- `core/internal/task/rejection_db_test.go`（改）— 计数收窄到 `aggregate_type='task'`

第四批（只读侧补齐）：
- `core/internal/task/domain.go`（新建）— Task 聚合类型（**没有** adapter/session 字段，ADR-013 的结构性体现）
- `core/internal/task/repository.go`（新建）— 只读 `Get` / `ListByStatus` / `StatusCounts` + 最小 `Queryer`
- `core/internal/task/repository_test.go`（新建）— 真库真表：读回 Touch 写的行、UNKNOWN 不被折算
- `core/internal/execution/domain.go`（改）— 补 `Version`、`StateConfidence` + §49 四级 Confidence
- `core/internal/execution/repository.go`（新建）— 只读 `Get` / `ListByTask` / `ListCrashRecoveryCandidates`（§56 原文范围）
- `core/internal/execution/repository_test.go`（新建）— 真表：恢复扫描只收五个状态、跨软件多次尝试同 TaskID

## Verification evidence

1. `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/task/... ./internal/execution/...`
   → `ok github.com/SadCat18/lucero/core/internal/task 0.698s` / `ok .../internal/execution 0.677s`
2. 同上加 `-v`：`TestMatrixMatchesContract51` 输出 `§5.1 合法迁移覆盖 75 条`，
   `TestMatrixMatchesContract52` 输出 `§5.2 合法迁移覆盖 35 条`；17 个测试函数全 PASS。
   非法用例数：Task 24 条、Execution 20 条（含 ACCEPTED/CANCELLED/SUCCEEDED/FAILED 四个终态往外跳、自环、表外状态名）。
3. `gofmt -l internal/` → 空；`go vet ./internal/task/... ./internal/execution/...` → 无输出（干净）。
4. `grep -rn "FROM approvals\|UPDATE approvals\|INSERT INTO approvals" internal --include=*.go` → 无命中（exit=1），
   本角色未建 `internal/approval`，只留了注入点接口 `execution.ApprovalStates`。
5. `git -C /d/Project/Lucero status --short core/` → 只有 `?? core/internal/`（新文件），
   我未修改 `go.mod`/`migrations`/`internal/platform`（db 角色并行产物的改动不是我做的）。
6. `ls internal/platform/db/` → `db.go migrate.go redact.go`，`grep "func (tx \*Tx)"` 无命中 → Touch 确实未就绪（阻塞成立，非推测）。
   **该条快照已于 16:2x 过期**：`touch.go` 随后落盘，见第 7 条。
7. `CGO_ENABLED=0 D:/DevTools/go/bin/go.exe build ./internal/platform/db/`（在 `core/` 下）→
   ```
   internal\platform\db\redact.go:19:11: atomic.Pointer[redactor] (type) is not an expression
   internal\platform\db\touch.go:308:17: d.columns undefined (type *DB has no field or method columns)
   internal\platform\db\touch.go:322:17: d.columns undefined (type *DB has no field or method columns)
   ```
   → Touch 形状已到位但包编译不过，写侧仍不可动。
8. `git diff 900038d --stat` → 仅 `.qoder/progress/{orchestration,survey}.md`、`core/go.{mod,sum}`、
   以及未跟踪的 `core/internal/**`；我可写范围外的跟踪文件一个都没被我改过。
   `git diff 900038d -- core/go.mod` 的内容与我会话开始时 `cat` 到的完全一致 → 非本角色所改。

第二批（裁定落地 + session/quota）：

9. `CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 90s ./internal/task/... ./internal/execution/... ./internal/session/... ./internal/quota/...`
   → 四包全 `ok`（task 0.901s / execution 0.835s / session 1.545s / quota 1.516s）。
10. `-v` 逐条：session 包 5 个用例全 PASS（含 `TestForTaskReturnsSessionsOfEveryProvider` = 一个 Task 挂
    Codex/Qoder/OpenCode 三个 Session 的**真表**证据）、quota 包 4 个用例全 PASS
    （`TestLatestSnapshotsKeepsNullsUnknown` 断言 NULL 额度是 nil 而不是 0）。
11. `gofmt -l internal/task internal/execution internal/session internal/quota` → 空；
    `go vet` 这四包 → 无输出。
12. **本轮抓到的真 bug（不是测试噪音）**：`ForTask` 原来在游标未关时对同一 `*sql.DB` 再发查询，
    `SetMaxOpenConns(1)` 下直接死锁 → session 包 `FAIL ... 601.589s`（测试超时 panic，栈停在
    `sql.(*DB).query` ← `session.(*Repository).ForTask`）。改成"先收 link 关游标、再逐条 Get"后
    同一用例 0.01s PASS。生产上单连接串行写通道同样会撞这个死锁，所以修在实现里而不是改测试。
13. 实测环境事实：这个工具链的 `database/sql` **没有导出 `sql.QueryerContext`**
    （`go doc database/sql | grep Queryer` 无命中）→ 两包各自声明了最小 `Queryer`；
    `sql.Open("sqlite", ...)` + 直接执行 `000001`/`000002` 两份 .sql 可用，未改 `go.mod`/`go.sum`
    （`git diff --stat` 里 `core/go.*` 与第二批开始前完全一致）。
14. `grep -rn "func (tx \*Tx)" internal/platform/db/*.go` → 只有 `Touch`/`Create`/`Append`/`RedactionHits`
    + 私有的 `appendEvent`/`scrubArgs`/`ctx` → B-backend-2 的"事务内无法读"成立（非推测）。

### 第三批：修 internal/task 编译红 + §5.4 的真表断言（主控指出后）

15. **主控报的红是真的，但已在交付前修好。** 现场：`projection.go` 在 16:12 改成三值返回，
    `projection_test.go` 到 16:15 才重写 —— 中间那 3 分钟 `internal/task` 确实编不过（6 个调用点）。
    我上一条消息的"四包全绿"是 16:2x 那次重跑的结果，不是交付前最后一次。已按"改签名必须同批改完调用点"纠正。
16. **交付前最后一次重跑**（在这轮所有代码改动之后，含第 20 条新增用例）：
    `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 120s ./internal/task/... ./internal/execution/... ./internal/session/... ./internal/quota/...`
    ```
    ok  	github.com/SadCat18/lucero/core/internal/task	1.715s
    ok  	github.com/SadCat18/lucero/core/internal/execution	0.922s
    ok  	github.com/SadCat18/lucero/core/internal/session	1.640s
    ok  	github.com/SadCat18/lucero/core/internal/quota	1.628s
    ```
    同批：`gofmt -l` 这四包 → 空；`go vet` 这四包 → 无输出。
17. `go test -count=1 ./internal/...`（主控那条命令）→ 十个包全 ok，含 `internal/task 0.992s`，exit=0。
    之后 `internal/audit` 又出现 `redaction.go:86: undefined: sync`（audit 在途），我没碰。
18. **新增 `internal/task/rejection_db_test.go`**：真库真表（`db.Open` + `Migrate(../../migrations)` +
    `db.Write`/`Tx.Create`/`Tx.Touch`）跑 §5.4 的差分断言，原文：
    ```
    --- PASS: TestRejectedProjectionWritesNoTaskEvent (0.07s)
        --- PASS: TestRejectedProjectionWritesNoTaskEvent/§5.4_无合法目标 (0.04s)
        --- PASS: TestRejectedProjectionWritesNoTaskEvent/对照：合法投影 (0.03s)
    ok  	github.com/SadCat18/lucero/core/internal/task	1.620s
    ```
    拒投影 → 该 Task 的 `event_journal` 新增 0 条、`tasks.status` 仍 PREPARING、`version` 仍 1；
    对照组（同一条路径收到合法边沿）→ +1 条事件、RUNNING、version 2 → 证明前一条不是空断言。
19. 变异验证（借 `test` 角色的规矩）：把被测路径临时改成"被拒时照样 Touch"，用例按要求失败：
    ```
    rejection_db_test.go:123: 该 Task 的 event_journal 新增 1 条，§5.4 要求 0 条
    rejection_db_test.go:127: 落库结果 = TRANSFERRING v2，期望 PREPARING v1
    ```
    对照组不受影响 → 两条断言都是活的。变异代码已还原，还原后才是第 16 条那次重跑。
20. 契约重读一致性检查（`f007372` 之后）：`git show f007372 -- specs/events/event-contract-v0.1.md`
    的改动行**只在** §5.4/§5.5 正文与事件目录，§5.1/§5.2 两张矩阵一字未动 →
    我的两份手抄表与 `successors` 仍然对齐（`go test` 已复核）。
    §1.1 规则 6~9 逐条对上：`Create` 建首行（新用例就在用）、多列 `setSQL`（`status = ?, updated_at = ?`
    实测通过，C5 关闭）、`Registered()` 与 `"schema":1` 由 `appendEvent` 单点做（读码确认，见 C13 的签名差异）、
    payload 留在业务包。§5.5 与我实现完全一致，代码未改。
    新增 `TestContract54ExamplesAreRejected`：§5.4 自己举的 `QUEUED`/`PAUSED`/`BLOCKED` + `execution_failed`
    (Handoff 已 READY) 三个例子必须走拒绝路径，不许退化成 `FAILED` 去"顺手修正"（§5 前言禁止）。

### 第四批：task / execution 只读 Repository（契约③ §7 新归属确认之后）

21. **交付前最后一次重跑**（本批所有代码改动之后）：
    `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 120s ./internal/task/... ./internal/execution/... ./internal/session/... ./internal/quota/...`
    ```
    ok  	github.com/SadCat18/lucero/core/internal/task	1.792s
    ok  	github.com/SadCat18/lucero/core/internal/execution	1.726s
    ok  	github.com/SadCat18/lucero/core/internal/session	1.652s
    ok  	github.com/SadCat18/lucero/core/internal/quota	1.635s
    ```
    同批：`gofmt -l` 这四包 → 空；`go vet` 这四包 → 无输出。
22. 全仓 `go test -count=1 ./internal/...` → **十个包全 ok**（adapter/approval/audit/eventjournal/execution/
    permission/platform/db/quota/session/task）。第 17 条里 audit 的 `undefined: sync` 已由 audit 自己修掉，我没碰。
23. `grep -n "^func (tx \*Tx) [A-Z]" internal/platform/db/*.go` → 仍然只有 `Touch`/`Create`/`Append`/
    `RedactionHits` → **B-backend-2 本轮复查仍未解除**，写侧 Service 依旧没动（没有绕道提前落库）。
24. 新用例都跑真库真表（`db.Open` + `Migrate(../../migrations)`）：
    `TestRepositoryReadsWhatTouchWrote`：`db.Create`(v1) + 一次 `Tx.Touch`(v2) 写的行，Repository 读回
    status=QUEUED、version=2、六个可空列保持"没有值"而不是被造出的值；
    `TestRepositoryKeepsUnknownReadable`：status='UNKNOWN' 读回来仍是 UNKNOWN，accepted_at=555 不被折算成 0；
    `TestRecoveryScanTakesOnlyTheFiveStates`：「Crash Recovery Query」只收那五个状态 —— 库里另有
    SUCCEEDED 与 UNKNOWN 两行走不进来（有反面数据，所以不是空断言）；
    `TestListByTaskShowsEveryAttempt`：同一 Task 跨两家软件四次尝试，TaskID 全部不变、attempt_no 递增。

## Recovery note

- Interruption point: 四包领域层 + 只读侧全绿；写侧未动，卡在 B-backend-2（`Tx` 事务内无读方法）。
  第 6、7 条证据是**历史快照**，已被第 12~14 条取代（db 包现已编译干净、测试通过）。
- First action after recovery: 重跑 Verification 第 9 条确认四包仍绿；然后做 Next action 里的
  `task`/`execution` 只读 Repository。B-backend-2 解除后（db 给出事务内读或带 expected-version 的 Touch）
  才动写侧：`Create` 建首行 → `Touch` 改状态 → 同事务补 audit → `Write` 返回后 Publish → §5.4 拒投影发 Notification+Audit。

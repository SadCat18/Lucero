# Internal Module Contracts v0.1（Wave-1 契约①）

> **编号消歧（2026-09-22）**：`契约①` 单写时**指本文**；`specs/events/event-contract-v0.1.md` 一律写作「事件契约」。
> 此前两份契约同时自称"契约①"，"契约① §2/§8"两边都能对上。规则见事件契约文件头。

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
    AggApproval AggregateKind = "approval"; AggQuota AggregateKind = "quota"
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
   库级 PRAGMA（`journal_mode`、`synchronous`）只在迁移/初始化时执行一次，**绝不在事务内执行**。
   **表述修正（Q1，2026-09-19 双引擎实测）**：事务内改 `journal_mode` 的后果**随引擎版本不同**——
   SQLite 3.45.1（Python 内置）静默无效并回读原模式；modernc 内置 SQLite 3.53.4 直接报
   `cannot change into wal mode from within a transaction`。因此实现**两种行为都不许依赖**：
   库级 PRAGMA 必须在任何事务之外执行一次，且**必须回读 `PRAGMA journal_mode` 断言实际值**，
   不得假定"执行过就等于生效"。测试断言按当前驱动（modernc 3.53.4）写，并在注释里标明版本敏感。
5. **记账约定**：每个 `.sql` 迁移文件末尾自己 `INSERT INTO schema_migrations(version, name, applied_at)`
   （`000001`、`000002` 均已如此），迁移器只查这张表来决定执行哪个文件，**自己不写记账行**；
   迁移器**不得**再包一层事务（脚本自带 `BEGIN IMMEDIATE;…COMMIT;`，嵌套会直接报错）。
   因此每个迁移文件都**不可重放**：重跑会撞 `schema_migrations` 主键或 `CREATE TABLE` 已存在。
   执行前必须先备份 DB，失败则 Core 不进入 Ready。新迁移从下一号顺延，不改已发布的文件。

### 1.1 C4/C5 裁定（2026-09-19，回 backend 的 Coordination）

6. **新建聚合走 `Create`，不走 `Touch`**（原契约漏了这个入口）：

   ```go
   // 首行落库 + 版本恒为 1 + 插首个事件。id 由调用方以 UUIDv7 预生成（幂等重试要复用同一个）。
   func (tx *Tx) Create(a AggregateKind, id string, columns []string, values []any, ev EventDraft) error
   ```

   `Create` 内部写 `version = 1`，事件的 `aggregate_version` 亦为 1。
   `Touch` 遇到不存在的 id 必须返回 `ErrAggregateNotFound`，**不得**顺手建行。
7. **`setSQL` 允许多列**（`"status = ?, updated_at = ?"` 合法）：白名单校验的是**列名**与占位符形态，
   不限制列数；`version = version + 1` 由 `Touch` 自己追加，调用方写了 `version` 直接拒。
   `AggTask` / `AggExecution` 等常量由 `internal/platform/db` 导出，业务包只引用不自定义。
8. **事件类型注册校验只在一处**：`Tx.Touch` / `Tx.Create` 内部调
   `eventjournal.Registered(ev.Type)`，未登记即返回 error。业务包**不必**各自重复校验。
   依赖方向 `platform/db ──► eventjournal` 合法（`eventjournal` 不 import 任何业务包）。
9. **payload 结构体长期留在业务包**（`internal/task/events.go` 等），因为它们编码的是域事实；
   `internal/eventjournal` 只做目录与校验面，不放业务 payload 类型，避免变成口袋包。
11. **`Tx` 必须提供事务内读**（B-backend-2 裁定，17:45）：`If-Match`/乐观并发的语义要求
    "读版本 → 比较 → 改状态"发生在**同一个事务**里；在 `Write` 之外读版本会留竞态窗口
    （读到的 version 到 Touch 时可能已被别的事务推进）。裁定采最小方案——
    透传而非新抽象：

    ```go
    func (tx *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
    func (tx *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
    ```

    调用方自己保证 `SELECT` 的表属于它自己的模块（越表读仍受 §45 约束）。
    典型用法：`SELECT version FROM tasks WHERE id=?` → 比对 `If-Match` → 不符直接返回
    `version_conflict` 且**不 Touch**（不产生事件、不推进版本）；符合才 `Touch`。
    否掉的方案 `TouchIfVersion(...)`：把期望版本塞进 `Touch` 会让签名再变一次、
    并诱发"所有模块都想要条件写"的连锁扩展；透传读一行解决，且不加抽象。
12. **故意不发事件的可变表**（写它们不需要 `Touch`/`Create`，用 `Append` 或各自的窄接口即可，
    不必为它们补聚合版本，也别被"没有事件"当成漏设计）：`execution_leases`（heartbeat 高频，
    进 journal 只会淹没事件）、`idempotency_keys`（纯基础设施状态，不是域事实）、
    `settings`（配置而非业务状态）、`blobs.archived_at`（冷热迁移，无 `blob_*` 登记事件类型）、
    `process_instances`（除 `process_started`/`process_exited`/`process_crashed`
    **CO-38 裁定（13:5x）：`process_instances.heartbeat_at` 在 v0.1 显式作废、无人写也无人读**——三件事一起不成立：没有写入者（`internal/process` 的 Supervisor 只发 `Event{STARTED|EXITED|CRASHED}`，它自己在 `supervisor.go` 里就把这张表的归属标成未决）、没有读取者（全仓 `heartbeat_at` 的读方只有 `internal/execution/repository.go` 的 `ORDER BY heartbeat_at`，**那是 `executions` 的列**）、而 §8 要判的"是进程死了还是 Core 死了"**由别的机制承担**：`core.lock` 残留 + 新进程启动时的恢复扫描判"Core 死过",Supervisor 观察子进程退出/管道断判"Adapter 死了"——**两者都不需要这一列**。**这条真正要防的是撞名**：库里 heartbeat 类列一共有 **4 个**——`adapter_instances.last_heartbeat_at`(:98)、`executions.heartbeat_at`(:315，且有 `executions(status, heartbeat_at)` 索引)、`execution_leases.heartbeat_at`(:339 NOT NULL)、`process_instances.heartbeat_at`(:569)。**判"执行卡住"看 `executions`，判"租约过期"看 `execution_leases`，`process_instances` 那列今后谁要用都得先写 REQ 说清分工。**
    三个已由 `process` 聚合表达的外，不逐条进 journal）、
    **`principals`（REQ-A6② 裁定，12:3x 补）**：本机主体是**引导期一次写入、之后不可变**的身份行，
    不是聚合根，因此**没有 `Create` 路径不是缺口**。用 `tx.Append("principals", …)` 就是合法正解
    （现有两处测试夹具这么插是对的，不算"绕过平台层"）；不要为它加 `version` 列或 `principal_*` 事件。
    将来若引入多用户/凭据轮换，再按新 ADR 升级为聚合。
    **`task_session_links`（REQ-LK-1 裁定，09-25 18:2x 补）**：它是**关系行**，不是聚合根，
    且 §6 目录里**没有任何 `*_link` / `*_linked` 事件**（09-25 grep `link` 在事件契约里 **0 命中**）⇒
    写它**就是纯 link，不发事件**，也不给它补 `version` 列；
    **谁都不许为了"让 link 也有事件"现造一个事件名**——那是 §6 的登记变更，要单独开 REQ 走"先登记再实现"。
13. **`Tx.UpdateOnce`：无事件可变表的唯一合法 UPDATE 入口（REQ-DB-1 裁定，13:5x 按 db 交付原文写）**
    第 12 条只说了"这些表不需要 `Touch`/`Create`"，却没给 UPDATE 入口，于是幂等中间件封不了自己的响应
    （backend 只能回 `409 idempotency_replay`）。db 已交付，签名与语义以 **`platform/db` 的
    `Tx.UpdateOnce`** 为准（**本文一律用符号引用定位，不写行号**——理由见本节末尾的"行号是易腐品"条）：

    ```go
    func (tx *Tx) UpdateOnce(table, id, guardCol string, guardVal any,
        setSQL string, args []any) (int64, error)   // 返回实际改动行数
    ```

    - **白名单写死在 db 里**：变量 **`narrowTables`** 当前**只有 `idempotency_keys`**，
      可写列 `state`/`response_json`/`updated_at`/`expires_at`，主键 `key`。白名单外一律 error
      （"not in platform/db's narrow update whitelist"）。**加表要过我这关**（写 REQ，不许顺手加）。
    - **REQ-DB-6 裁定（09-23 00:3x）：`process_instances` 的退出/崩溃改写不进 `narrowTables`，本条是"加表要过我这关"的一次正式答复：不准。**
      理由不是它不够窄，是**它要发事件**：事件契约 §6 登记着 `process_started`/`process_exited`/`process_crashed`
      （`aggregate_type=process`），而本入口的定性就是"**不取 version、不写事件**"（判据 U13-4）。
      走 `UpdateOnce` 写这两格 = **行改了、事件静默丢掉**，正是 REQ-A10/A11 那一族"字节对了、归因缩水"。
      `db` 在 `narrowTables` 的注释里把这条守得很干净（"没有消费者的入口就是投机开的门"）——**现在有了消费者，
      但来的不是这个门**：真正的洞是 **REQ-DB-2**（"无版本、但要发事件"的表没有写路径），
      `process_*` 与 `core_ready`/`core_shutdown` 是**同一个洞的两半**（`core` 根本没有表），所以**一次做掉、共用一条通道**，
      不要给 `process_instances` 单开窄门、也不要为它补 `version` 列（`000002` 没给它版本是有道理的：
      它的改写来自 Supervisor 的异步观察，不是用户的 `If-Match`，加了版本也没有并发语义可保护）。
      **⇒ backend 那两格继续阻塞在 REQ-DB-2 上，不算它欠账；`RecordStart` 走 `Append` 不受影响。**
    - **聚合表物理封死**：`Tx.UpdateOnce` 开头对 **`aggTables`** 的命中判断直接 error，
      所以第 12 条那张表**不可能被误用来绕过版本**。
    - **`guardCol`/`guardVal` 是"只封存一次"的关键**：第一次写把 `state` 改掉并落 `response_json` → 1 行；
      重放时条件不匹配 → **0 行**，调用方据此**回读已封存的响应**而不是覆盖它。
      平台层因此**不需要理解状态机语义**（"谁算第一次"由调用方的 guard 表达），这是这条入口能保持窄的原因。
    - **仍然跑脱敏**：`Tx.UpdateOnce` 与 `Touch`/`Create` 共用 **`Tx.scrubArgs`**，
      `response_json` 正是最可能带密钥的列，别指望它是第二个豁免口。
    - **不发事件、不取 version**（这就是"无事件表"的定义）。
    - 判据归 `test`：`UpdateOnce` 打聚合表必须 error；打白名单外表必须 error；
      guard 不匹配时返回 0 行且**原值未被覆盖**；`response_json` 里放一个密钥形状的值，
      落库后必须是被脱敏的（这一条打在 db 的返回值/直接产物上，不是打在读侧）。
    - **响应体口径（C18 裁定：选"首次也回读库"，不选"契约允许键序不同"）**。
      起因是 backend 报的"首次体与重放体内容一致、键序不同"，但我 14:5x 读 `api/http` 的
      `sealIdempotency` 发现**问题比键序大**：它 `return string(body)`，返回的是
      **自己 `json.Marshal` 出来的那一份**，而落库的是经 `Tx.scrubArgs` 处理后的另一份。
      两者在"没有值被改写"时只差键序，**一旦被脱敏命中就是真实的内容分叉**——
      那等于**首次响应把未脱敏值发到线上，而库里存的是打码版**，正好绕开 §2.2 想守的那条线。
      所以不接受"契约声明字节序无关"这种把分叉合法化的写法（选项 ①/③ 都属此类）。
      **规定：对外 body 一律取"封存后回读库的那一份"**（`sealIdempotency` 在 `UpdateOnce`
      返 1 行之后按 `key` 读回 `response_json` 再写出），于是
      ① 首次与重放**逐字节相等**，不需要任何"允许键序不同"的特例断言；
      ② **wire 上的内容与存储内容按构造一致**，脱敏规则怎么变都不会漏；
      ③ 成本是本地单用户下多一次按主键读，可忽略。
      `test` 的断言形状：**同一次 POST 的 201 body 与后续重放的 body 必须字节相等**，
      并且往响应里塞一个密钥形状的值时，**两份都必须等于打码后的存储值**。
      （backend 原本准备把 `sealIdempotency` 里 `n != 1` 的守卫当死代码删掉，是变异证明它可达才留下的
      —— 这个做法正确，记为范例：**"看着像死代码"不是删除理由，变异验证才是**。）

#### 判据表 U13（`test` 逐行解析用：一行一判据，命令列当场可跑）

解析约定：**按 `|` 分列**；`定位符` 只允许符号名/用例名/表名（**本文禁止行号**，见 §1.1 第 14 条）；
`状态` 列 `现存` = 用例已在库里（15:11:47 全包重跑时随包为 `ok`），`待建` = 还没有人写、
**该行由"归属"列的角色负责，`test` 不许把 `待建` 当成红**。命令里的包路径相对 `core/`。

| ID | 断言 | 定位符（用例名） | 当场命令 | 状态 / 归属 |
|---|---|---|---|---|
| U13-1 | `UpdateOnce` 打聚合表必须 error | `TestUpdateOnceRefusesEverythingElse` | `go test -count=1 -v -run TestUpdateOnceRefusesEverythingElse ./internal/platform/db/` | 现存 / db |
| U13-2 | 打白名单外任意表必须 error | `TestWriteWhitelistRejectsAnythingElse` | `go test -count=1 -v -run TestWriteWhitelistRejectsAnythingElse ./internal/platform/db/` | 现存 / db |
| U13-3 | guard 不匹配返 0 行**且原值未被覆盖**（"只封存一次"） | `TestUpdateOnceSealsResponseExactlyOnce` | `go test -count=1 -v -run TestUpdateOnceSealsResponseExactlyOnce ./internal/platform/db/` | 现存 / db |
| U13-4 | 该入口**不取 version、不写事件** | `TestUpdateOnceTakesNoVersionAndWritesNoEvent` | `go test -count=1 -v -run TestUpdateOnceTakesNoVersionAndWritesNoEvent ./internal/platform/db/` | 现存 / db |
| U13-5 | `response_json` 落库必须已脱敏（含"调用方已序列化的 JSON"也要扫） | `TestUpdateOnceRedactsPreSerializedJSON` | `go test -count=1 -v -run TestUpdateOnceRedactsPreSerializedJSON ./internal/platform/db/` | 现存 / db |
| U13-6 | 首次 201 body 与重放 body **逐字节相等**（C18 选②） | `TestIdempotentReplayBodyIsByteIdentical` | `go test -count=1 -v -run TestIdempotentReplayBodyIsByteIdentical ./test/` | **SKIP（blocked）**：用例在 `core/test/crash_test.go`，自带 `t.Skip("blocked: 需要 POST /api/v1/tasks 与幂等回读路径可用（backend）")` / 待 backend |
| U13-7 | 响应里含密钥形状时，**首次与重放两份都等于打码后的存储值** | 建议名 `TestIdempotentFirstResponseIsRedacted` | `go test -count=1 -v -run TestIdempotentFirstResponseIsRedacted ./api/http/` | **待建 / backend**（U13-5 只证到 db 落库层，**没有证到"首次响应体不外泄"**——C18 的真实风险点目前缺一层断言） |
| U13-8 | §6 词表里每个事件名**必须有发射位覆盖**，或**在本节名单里被登记为"无生产者"**；两个方向都算红（漏登记 ⇒ 红、多登记而其实有生产者 ⇒ 也红）。回解深度四层（字面量 / 常量 / 别名或调用 / 形参←同包调用点实参），**回解不出者单列「未决」并让用例 FAIL，绝不计入"无生产者"**（把看不见的东西写成没有，就是往名单里塞假事实） | `TestEventProducerRosterMatchesDerivedSet` | `go test -count=1 -timeout 120s -v -run '^TestEventProducerRosterMatchesDerivedSet$' ./test/` | **现存 / test**（**主控 09-30 19:5x 亲跑**；被验树 = HEAD `2c65177`、构建时刻 19:57:13、指纹 `6b5f3b01de843723b568f5e1fa3288f2`；跑前 `git status --porcelain core/ specs/ packages/` 现读 **0 件脏** ⇒ 这一面没混别人的在途码）：主腿 `--- PASS (0.04s)`，与 C8-12／C8-13 同趟，那趟 `=== RUN` 计数 = **3**（不是空匹配）；单包 `go test -count=1 -timeout 420s -v ./test/` = **ok 14.069s、顶层 53 PASS / 6 SKIP / 0 FAIL** @19:35:06；全仓带 `-v` = **18 包 ok / 顶层 640 PASS / 21 SKIP / 0 FAIL** @19:31:53。**桶一 44 个**（比 16:0x 那趟的 43 多一枚，多的正是 `execution_unknown`；`internal/task` 那五条仍由第五层并进 `internal/task/service.go:197`）、**桶二 21 ＝ 名单 21**、**未决 0 处 0 个**；锚句自检「自称 21 条 = 实取 21 个（相等）」，且 `grep -c "本名单当前是" specs/events/event-contract-v0.1.md` = **1**（唯一锚点，这句是 fail-closed 的前提）。**反向腿仍在**：名单退回 26 不摘那 5 条 ⇒ 同一条命令 `RC=1`、5 条各点名一次（`event_producer_roster_test.go:1018`）⇒ **这句绿依赖锚句被改对，不是恒绿**。**形状警告（写给下一个改名单的人）**：名单那一整行只准放名字 —— 判据读整行、把该行所有反引号内的 `[a-z0-9_]` token 都算成名字，在锚句那行里补一句带反引号的解释就会**静默改名单**；摘条说明要另起一行。**两枚旗标面读数不许混进本格**：`LUCERO_TEST_FAKE_ADAPTER=1` 时 `./test/` 有两条真红（`TestTurnRefCrossCheck`、`TestDroppedFrameReachesAuditSeam`，同一条根因，且在干净 HEAD `738cb13` 快照上一字不差同样红 ⇒ 非本批改坏），那一面记台账第一百四十九批、归 test 的 T-410；**本格只说默认面 + 名册三腿**。） 〔旧格留档、被本格取代〕**现存 / test**（主控 09-27 16:3x 自己跑：`--- PASS (2.00s)`、`ok`；跑前一遍是 `FAIL` 24 条未登记，登记后归 0。名单落点见事件契约「本名单当前是 27 条」那句——**它是这条判据的唯一锚点**，改了那句不重跑 = 立刻红）|

**状态列只允许三个值**（16:01 逐个实跑标定，不是推断）：`现存` = 该 `-run` 目标**真的跑出 PASS**
（判据：加 `-v` 后 `--- PASS` 计数等于期望条数；U13-1~5 实测 **5**）；`SKIP（blocked）` = 用例存在但被
`t.Skip`，**既不是红也不是绿**，必须连阻塞方一起读；`待建` = 库里没有这个用例。
**为什么非要区分**：`go test -run <名字>` 在**一个都不匹配时照样输出 `ok`**——不加 `-v` 就会把"没跑到"
读成"跑过了"。**这条判据自己就长在一个假绿坑上**，所以命令列从现在起一律带 `-v`。

#### 判据表 A18（ADR-018 链头锚点；**行为级，禁止用"某目录 grep 到符号"当代理**）

**这张表是纠我自己的错立起来的。** 我 11:17 用 `grep -rn "Advance(" core/cmd/` 零命中判成"T-64 ② 没接线"，
12:07 全仓重跑证伪：`internal/approval/store_sqlite.go` 在 `d.Write(...)` **返回无错之后**才调 `ch.Checkpoint()`，
`internal/audit/append.go` 再转 `Checkpoints.Advance(n, Certified())`。**接线本来就在，是我的判据看不见它**
——同族错误今天第二次（`UseCheckpoints` 那次是单文件 grep 下全仓结论）。**锚点由谁推进是实现细节，判据只能钉可观察行为。**

| ID | 断言 | 怎么测（行为，不是"有没有命中"） | 状态 / 归属 |
|---|---|---|---|
| A18-1 | 连续成功 append 跨过阈值（1000）必须出现**新的一行锚点**，且该行 `seq` 等于库内当时最后一行的 `seq` | 测试里连推过阈值 → 读 `<data-dir>/audit.head` 末行 → 与 `SELECT max(seq) FROM audit_entries` 比对 | 现存 / audit（`TestCheckpointCounterCrossing`、`TestAdvanceAnchorsOnlyTheHeadItSaw`；13:28:57 逐条 `--- PASS`）|
| A18-2 | 事务**提交失败**不得留下任何锚点行（锚点绝不领先库内状态） | 强制提交阶段失败，断言锚点文件行数不变 | 现存 / audit（`TestAnchorCommitRejectsWithoutWriting`；另 `TestAnchorLineAheadOfDatabaseIsUnverified`、`TestCheckWithoutAnchorIsUnverifiedNotVerified` 钉住「锚点领先或缺失不许报 VERIFIED_LOCAL」）|
| A18-3 | **推进必须幂等：同一 `seq` 区间被推进两次，锚点不得多写一行**（12:4x 改判：从"人守白名单"换成"结构上双计无效"） | 对同一提交结果连调两次推进入口，断言 `audit.head` 行数不变、第二次不产生新行 | 现存 / audit（选①已实现：`TestCheckpointCounterIgnoresEmptyWork`、`TestChainCheckpointsOnlyWhatItAppended`）|

**✅ 残余洞（13:2x 记 → 13:41:20 复采已闭）**：幂等只在**推进入口**这一层成立；测试若绕过它、直接给计数器喂合成 `n`（`Advance(999, Head{Seq: 库里很小的 seq})`）仍能伪造「跨过阈值」，于是「双计无效」这个保证在**用例层**又被打穿。backend 在我那条派发送达之前**就自己按同一方向改完了**：`cmd/core/core_test.go` 已无合成 n，改成 `core.checkpoints.Advance(1, audit.Head{Seq: 从真库读回的 seq})`；13:41:20 我复采 `TestAnchorAdvancesOnlyAfterCommit`、`TestWiredAnchorCounterIsAttached` 两条 `--- PASS`，且 `./cmd/core/` 与 `./internal/audit/` 两包全 `ok`。**「判据要经得起合成入参作弊」这句留在这里当中常驻纪律**——洞闭了不是删掉它的理由。

**A18-3 为什么改成"结构幂等"而不是"白名单"**：12:37:12 实测**推进路径已有两条独立生产点**
（`internal/approval/store_sqlite.go` 的 `ch.Checkpoint()` 在提交成功后；`cmd/core/rejections.go` 的
`checkpoints.Advance(int(after.Seq-before.Seq), after)`），所以"指定 `cmd/core` 为唯一推进者"这个选项
**在事实层就不成立**——要唯一就得删掉一条在跑的路径，而那条的消费者根本不在 audit 包内。
**裁定选①：计数器改 seq 驱动**（记住"已锚定到哪个 seq"，`after.Seq - anchoredSeq >= 1000` 才写一行；
第二次调用差值为 0 → 天然 no-op）。于是"谁在推进"不再是正确性前提，白名单退化成**记录**而不是闸门。
**同时更正我记账时引用的一条错前提**：audit 说的"`UseCheckpoints` 零生产调用方"**字面今天已不成立**
（`cmd/core/main.go:140` 确实在生产里调），它真正成立的是"**今天没人写 approvals，所以那条腿 dormant**"。
两种说法对阈值隐患的结论相同，但**"谁在跑"必须说准**，否则选路会被错前提带走。
阈值语义仍要钉死：双计不会造出领先库的 head（head 取自 `Certified()`），但会把"每 1000 条"悄悄变成"每 500 条"。




**为什么 U13-7 不能省**：U13-5 证明"库里存的是打码版"，C18 那个缺陷恰恰是
**"库里对、发出去的那一份错"**——只测前者等于没测。两条合起来才封住
"wire 内容 = 存储内容"这条口径；缺 U13-7 时 §1.1 第 13 条的响应体口径处于**未受检**状态，
这一条要在 backend 交回时同时补齐，否则它那句"201/201 内容相等"只是自述。
**变异要求（照旧）**：U13-1~7 每条都要能靠"把被测点改坏"证明会红，
且变异**必须命中在声称的那一层**（T-29 与 audit 那条"早退导致假绿"的活例）。




14. **契约里只准用符号引用定位，禁止写行号（db 提，14:4x 采纳并泛化为全项目规矩）**
    实证就在这次：我 13:5x 按**当时实读**写进第 13 条的四处行号（`touch.go:156/174/175-177/172-173`），
    40 分钟后被 db 自己**新加的 3 行注释**整体推后 3（`narrowTables` 156→159、`UpdateOnce` 174→177、
    聚合表守卫 175→178、`scrubArgs` 206→209），而其中"跑脱敏 `:172-173`"其实指的是**注释行不是调用点**——
    **我写的那一刻就没指对东西**。这不是 db 的错，是"行号"这种引用形式本身不持真。
    判据：
    - **定位一律写**：包名 + 标识符（`Tx.UpdateOnce`、`narrowTables`、`(*Core).writeEndpointFile`）、
      或表名/列名/约束名（`execution_output_chunks` 的 `UNIQUE(execution_id, sequence_no)`）。
      这些是**改名才算破坏性变更**的东西，改行号不是。
    - **行号只允许出现在两处**：① 账本/进度文件里作为"某时刻观察"的证据（须带当场 `date`）；
      ② 测试断言的失败输出。**契约正文不许有**，因为契约是被长期引用的一方。
    - 需要"证明代码此刻长这样"时，写**可跑的命令**（`grep -n "func (tx \*Tx) UpdateOnce" ...`），
      比写行号更强：命令每次都能重新定位，行号只会过期。
    - 同一条也解释了今早那两次"契约里的错指针"（`//go:embed ../../../migrations` 指向非法位置、
      `core/migrations/embed.go` 给不存在的文件派了 owner）：**指针越具体，越容易在写下的下一秒变成假的。**

15. **判据表的"当场命令"列必须可复现 = 包路径 + 锚定的用例名，两个都不许省（REQ-T15 裁定，09-23 00:1x 采纳 `test` 的提案）**。
    理由不是格式洁癖，是 **Go 的 `-run` 是无锚定的子串匹配**：`-run TestDroppedFrameReaches` 会**同时命中两条**，
    而盘上现在**真有**这一对只差一个 `The` 的名字 ——
    `TestDroppedFrameReachesTheAuditSeam`（在 `core/internal/process/connection_test.go`，`adapter` 的）与
    `TestDroppedFrameReachesAuditSeam`（在 `core/test/turn_attribution_test.go`，`test` 的）。
    **只写用例名不写包，下一个人就会把别人的 PASS 记到错的归属上** —— 这正是本条第 14 项"引用形式本身不持真"的兄弟：
    行号会在别人插行后指错，**用例子串会在别人起同名测试后指错人**。
    判据：
    - 命令写成 `go test -count=1 -v -run '^TestXxx$' ./pkg/path/`（**全名带 `^…$` 锚定 + 显式包路径**）；
    - 同一判据若两家各有一条近名用例，**契约里必须两条都写出来并标归属**（见契约② 判据表 T84-7a / T84-7b），
      不许靠"名字差不多"合并；
    - 通用一句：**凡"可复现命令"里的定位符，要选改名才算破坏性变更的那一种**（包路径 + 全标识符），
      子串、行号、目录里的第一个命中都不算。

16. **凡判"两边集合相等"的判据，都要再判一条"解析/展开过程中的元素数 == 去重后的数"，否则重复登记会被静默吸收**
    （09-23 01:5x 巡检指出、我核过成立，由 C8-13 的第二条腿泛化而来）。集合语义天生看不见"同一个东西出现两次"：
    `map[string]bool` 会把重复吃掉，所以"新建合并行 + 漏删原行"这种事故在**所有** set-based 判据上都是隐形的 ——
    09-23 我登记 `project_archived` 两次，`§6 目录 ↔ yaml 枚举 ↔ Go 常量` 三方全是 63，**三条判据都绿**，
    今晚唯一真响的是 `frontend` 的"重名即停"。**判据形状因此固定为三条**：①集合双向相等；②**展开前的 token 数 == 展开后
    去重的名字数**（不等即红、点名到行）；③喂一行重复的**合成反例**（只有真数据能红的不算闭上）。
    适用面：**两类**都算 —— (a) 从文档解析出集合再比对（事件目录、错误码表、任何"读 specs 再对代码"的判据）；(b) **纯代码侧的集合比对**（例：`eventjournal` 里"两个常量指向同一个值"——Go 语法合法、`sortedUniq` 会吃掉、集合比看不出来，但词表因此出现两个名字一个事实）。**不分角色、不分文档内外。**
    **并且这条规矩的"落地形状"允许按表的结构调整**（09-23 02:3x 采纳 `test` 的一处偏离并认它更对）：
    第②条在**左列/名字列**上是硬判据（重复即红），但在**有意的多对一映射列**上（如契约② §15.2 的『来自』列，
    两个 HTTP 码映射到同一个 Adapter 码**在未立语义前**不判红 —— 判它等于**替契约把"映射"改写成"双射"**，那是新增语义、不是防漂移。
    **但这条收窄有一个前置**：收窄是"契约还没说话"时的默认，**一旦契约作者把语义写进正文，判据就要跟着转红**。
    现成例子（09-23 03:1x）：契约② §15.2.2 原本没规定『来自』列是否一对一，我 02:3x 据此裁"只打印"；
    巡检 03:10 给出"现盘 17 行 / 去重 17 / 无重复 ⇒ 今天转红零误伤，再晚要迁就既有行"这个事实后，
    我**先把双射写进 §15.2.2 的正文**（并指出 §15.2 开头那句"新增码必须同时改 §15.1 与 §15.2"本来就预设了它），
    **再**加判据表 **C8-14** 去 enforce。**顺序必须是先立语义、再上判据**——反过来就是拿工具替契约做决定。
    **判据作者有权收窄，但收窄理由与"何时该转红"都要写进表里；而每一次转红都必须由契约正文先说话。**
    那是新增语义、不是防漂移。**判据作者有权在表结构上收窄本条，但必须把理由与"什么时候该转红"写进表里**，
    不许默默省略那一腿。

10. **把一张表加入聚合注册表是破坏性变更**（17:22 补，起因是我自己下达的 T-12 把 `internal/approval` 打红）：
   一旦某张表进了 `AggregateKind → 表名` 映射，`Append` 对它就失效（`aggregate rows must go through Touch or Create`），
   所有既有建卡路径必须同轮改成 `Create`。因此**db 改注册表时必须自己先跑全仓**
   `grep -rn "Append(\\\"" *.go` 与 `go test ./internal/...`，并在同一轮把受影响方列进 Coordination，
   不得只报"我这边绿了"。只新增 `AggregateKind` 常量**不是**破坏性的（Go 加常量不影响调用方）——
   破坏来自映射与校验行为的改变，别把两件事混为一谈。

17. **REQ-DB-2 裁定（09-23 05:07 · 05:18 由巡检 05:14 的一问纠正两处，见文末"更正记录"）：非版本聚合的写入口 —— 版本住在事件流里，不住在行里。**
    三个消费方在等同一条通道：① `process_started`（**建行**）；② `process_exited`/`process_crashed`（**改行**）；
    ③ `core_ready`/`core_shutdown`（**`core` 根本没有表**）。
    今天 `cmd/core/dropaudit.go` 的三格 `RecordStart`/`RecordExit`/`RecordCrash` **全部只出声不落库**——
    这是 T-168（01:11）我自己下的令：那次 `RecordStart` 里有一条 `tx.Append("process_instances", …)`，
    它同时犯了两件事（从 `cmd/core` 写别人的表、且 `Append` 不写事件 ⇒ `process_started` 静默丢掉），已被收回。
    REQ-DB-6 已裁"不进 `narrowTables`、也不给 `process_instances` 补 `version` 列"，本条回答"那到底走哪道门"。
    - **新增两个聚合类型**：`AggProcess AggregateKind = "process"`、`AggCore AggregateKind = "core"`。
      **它们不进 `aggTable`（聚合→表）那张映射**，因而也不进 `aggTables`（这条与第 10 条同向：进映射才是破坏性变更）。
      **但"不进映射"绝不等于"可以继续用 `Append` 建行"**：第 12 条那句"故意不发事件的可变表"后面的
      **"除 `process_started`/`process_exited`/`process_crashed` 三个外"是排除**，也就是这三件是**要发事件的事实**，
      而 `Append` 恰恰不写事件 ⇒ **`process_instances` 的建行腿只能走下面第 ① 个入口，`Append` 对它不再是合法出口**
      （`check_ownership.sh` 的 W13 那一族已经把"表名字面量出现在谁的包里"钉住了，这里补的是语义那一半）。
      取而代之，`platform/db` 里另立一张**独立登记表** `versionlessAgg = {process: "process_instances", core: ""}`，
      **加一项要过主控**（与 `narrowTables` 同口径）。
    - **版本号怎么来（本裁定的核心）**：`= 该聚合在 `event_journal` 里已有的事件条数 + 1`，
      即 `SELECT COUNT(*)+1 FROM event_journal WHERE aggregate_type=? AND aggregate_id=?`，**在同一事务内取**。
      新建的行它自己就是第一条 ⇒ 序号天然是 1，与既有 `Create` 写死 `version=1` 的做法同形。
      **并发后果写明白、不许瞒**：两个并发事务会算出同一个序号 ⇒ 后提交者撞
      `UNIQUE(aggregate_type, aggregate_id, aggregate_version)` 而**整笔回滚**。这不是缺陷，**正是要的行为**：
      同一条状态边只允许落一次。调用方必须把"撞 UNIQUE"当作"这一格已被别人落过"来处置（幂等），
      **不许把它重试成第二个事件**——那等于把一次崩溃观察复制成两份事实。
    - **三个入口**（每一个都对应一种物理情形，少一个就会有人拿另外两个硬凑）：
      ```go
      // ① 建行 + 恰一条事件（`process_started`）；这张表没有 version 列，所以不复用 Create。
      func (tx *Tx) CreateVersionless(a AggregateKind, id string,
          columns []string, values []any, ev EventDraft) error
      // ② 状态边：改一行 + 恰一条事件；没有 version 可靠，幂等闸就是"从哪个状态改走"。
      func (tx *Tx) TouchTransition(a AggregateKind, id, guardCol string, guardVal any,
          setSQL string, args []any, ev EventDraft) (int64, error)   // 0 = 这条边已落过，且**不插事件**
      // ③ 没有表的聚合（`core`）：只落事件。
      func (tx *Tx) AppendEventOnly(a AggregateKind, id string, ev EventDraft) error
      ```
      **为什么不是"`Append` + `AppendEventOnly` 两步拼"**：那等于把"行与事件同生同死"从入口里挪到调用方的自觉，
      而"忘了调第二个"正好就是 T-168 那次事故的形状——**入口存在的意义就是把这件事变成不可能**。
      `TouchTransition` 的 SQL 固定为 `UPDATE <table> SET <set> WHERE id = ? AND <guardCol> = ?`；
      **`guardCol` 逐表白名单**，v0.1 `process_instances` 只允许 `status`。
      **0 行 ⇒ 不插事件**是"行与事件同生同死"的唯一定义方式（否则重放一次就多一条事件，审计链长出幽灵迁移）。
      三个入口都必须走既有 `appendEvent` ⇒ `Registered(ev.Type)` 与 REQ-B-1 的 `aggregate_type` 闸一并生效，
      **不许绕过**；脱敏仍在密封之前（REQ-A10/A11），参数与 payload 同其它入口一样过 `scrubArgs`/`redactValue`。
      **③ 号入口的规范句（09-24 07:2x 补，db 07:12 指出它那道收紧在正文里查不到依据 ⇒ 现在有了）**：
      **`AppendEventOnly` 只准给"没有表的聚合"用**（v0.1 即 `table == ""` 的那一个：`core`）。
      **有表的无版本聚合（`process`）一律走 ①/②**——否则会出现"`process_exited` 落在事件流里、
      而 `process_instances` 那一行还停在**建行时写进去的那个状态**（v0.1 = `STARTING`）。**09-24 07:4x 纠正：这里原本写的是 `RUNNING`，那个值是我编的**——`supervisor.go` 发 `EventStarted` 带的是 `StatusStarting`，而全仓没有任何事件带 `StatusRunning`（见第 20 条 ⑤）；db 07:4x 转告 adapter 时照抄了我这个错值，被巡检当场拦下 ⇒ **错在我这行，不在转述方**。"，那是第 12/13 条那个洞的**反方向**
      （原来防"行改了、事件静默丢掉"，这里防"事件写了、行没动"）。
      **配套判据双向**：`AppendEventOnly(AggProcess, …)` 必须报错、`AppendEventOnly(AggCore, …)` 必须成功。
      "观察到进程退出但库里根本没有它的行"这一格**不进事件流**，走 `RecordDrop` 那一族的审计登记（与第 20 条 CO-44 同源）。
    - **落点与角色（§7 归属矩阵定的，不是我新发明的）**：入口实现归 `db`（`internal/platform/db`）；
      **"把一次生命周期观察落成一行 + 一条事件"这件事的语义住在 `internal/process`，那是 `adapter` 的包**；
      `cmd/core/dropaudit.go` 只做**装配**（把 `db.Tx` 递给 `internal/process` 的那个写口、并把错误喊出来），
      归 `backend` ⇒ **`process_instances` 这个表名字面量仍不许出现在 `cmd/core`**（W13 那条判据不变）。
      **`adapter` 合验的行级那一半在这三件落地之前仍只到"显式未接"**——`lifecyclecolumns_test.go` 里那个
      `LUCERO_REQ_DB2=1` 开关就是钉这件事的，**谁都不许为了变绿把它改成 SKIP**。
    - **落地判据（这条通道"什么叫做完"）**：`LUCERO_REQ_DB2=1 CGO_ENABLED=0 go test -count=1 -run 'TestLifecycleRowLandsTheFiveColumns' ./cmd/core/`
      必须 **PASS**（不是 SKIP、不是那个用例里"未接上真写路径即 `t.Fatal`"那一句）——
      **并且从 09-24 08:5x 起，这一格算"三条腿 + 每腿一条反例"**（`adapter` 08:46:48 的整包级证据推出来的，它对的这一半我收下）：`RecordStart`（建行腿）、`RecordExit`/`RecordCrash`（改行腿）**各自**都要有"装配好的形状下被驱动"的断言 —— **默认面不驱动不算**（那条终态边今天只活在你没开 flag 的那格之外，摘掉改行腿整包仍 `ok`，正是它测到的形状）。**反例形状固定为**：摘掉某一条腿 ⇒ 对应用例必须红，且**与基线输出两两不同**（整包绿 ≠ 有牙）。**不另开一节硬前置**：这条并入本判据即可，`test` 只在合验时复核、不重做（契约③ §7.3 只钉 nil 那一格）。
      **验收方是 `backend`（装配那一格）+ `adapter`（`internal/process` 的写口），`db` 只负责三个入口存在且形状对**。
      **另：REQ-DB-2 落地之前不要顺手带 `LUCERO_REQ_DB2=1` 跑全仓**，那条按设计就是红的
      （该用例里"未接上真写路径即 `t.Fatal`、且明写不许改成 SKIP 蒙过去"那一句是故意的）。
      **主控 09-23 05:09 实跑为证**：带 flag 单跑该用例 ⇒ `--- FAIL (0.00s)`、报的就是那句 ⇒
      这是**已知未闭**，不是新线索，谁拿它去判别人的批次红都算误伤。
      **REQ-T20 补半句（09-24 10:2x）**：所有"n 次 0 红"式量级判据里 **n = 真跑过的次数** —— `--- SKIP` **不算样本**（`buildCore` 在 `cmd/core` 编不过时按设计 `t.Skipf`，那几次一行断言都没执行）。**验收跑里出现 SKIP = 那次验收不成立**，不是"通过得比较宽松"；发生率与 SKIP 并存时按有效样本换算。
    - **更正记录（09-23 05:14 巡检问出来的两处，它只报读数没代裁）**：
      ① 本条 05:07 那版写着"`Append("process_instances", …)` 建行那条腿（`RecordStart`）**继续合法**"——
      **作废**。那句话把"不进 `aggTables` 所以 `Append` 不被拒"（形状层，成立）误读成"所以可以拿它建行"（语义层，不成立）；
      第 12 条的"除三件外"就是禁止它的那一句，而盘上 `RecordStart` 早在 T-168（01:11）就被我改成只出声了，
      **我 05:07 写这条时没去读那 20 行**。⇒ 补第 ① 个入口 `CreateVersionless`，并把"两行拼一步"明确禁掉。
      ② 05:07 那版把"三格改过去"整个派给 `backend`，**归属错了一半**：`internal/process` 是 `adapter` 的包，
      落库语义住那里；`cmd/core` 只做装配。⇒ 见上面"落点与角色"那一格。

18. **C34 裁定（09-23 05:07）：`process_instances.owner_type` / `owner_id` 的词表。** backend 明说"我不猜"，是对的——
    这两列在 schema 里是**无 CHECK 的可空 TEXT**，所以值域只能由契约登记、由 lint 钉，代码里"看起来自然"的写法不算数。
    - **v0.1 唯一合法值**：`owner_type = 'adapter_instance'`，`owner_id = adapter_instances.id`（UUIDv7）。
      **不登记 `'project'` / `'core'`**——今天没有任何生产者会那样写，登记了就是第 17 条那把尺子反着用的例子
      （"有名字无生产者"）；**等真有生产者再登记**，那时按同一条规矩连带登记它的 `owner_id` 指向哪张表。
    - **为什么是实例 id 而不是 `adapter_key`**：与 T-204 那条 `{id}` 裁定同一形状，且 `sessions.adapter_instance_id`
      已经 `REFERENCES adapter_instances(id)`——归属列跨表可比才有用，写 key 就Join不上。
    - **NULL 保留给"建行时还不知道归属"**。**（09-23 05:18 更正：这一句原本写着"`RecordStart` 现在就是这个状态：
      它的 `Append` 连这两列都没写"——那是幽灵引用。盘上 `RecordStart` 自 T-168（01:11）起**只出声、不写库**，
      函数体里没有 `tx.Append`；所以"建行时"在 REQ-DB-2 落地之前根本不存在。）**
      要把归属补上，走第 17 条的 `CreateVersionless`（建行时一起给）**或**落地后再改一次 `status` 腿的
      `TouchTransition`；**不许**为这两格开任何 `Append` 之外的裸写通道。
    - **key → id 的反查已有现成件**：`cmd/core/discoverassembly.go` 开头那句
      `SELECT adapter_key FROM adapter_instances WHERE id = ?` 是同一条查询的反方向，**复用那条 SQL 的形状、别新造第三份查找**。

19. **REQ-L-1 裁定（09-23 05:23）：单实例锁的"持有者还活着"不能由 pid 判 —— 这是 Wave-2 闸门上的产品缺陷，不是测试噪声。**
    来源：`test` 05:14~05:16 的定档（**单跑 63 次红 3、整包两趟 1 红 ⇒ 4/65 ≈ 6%**；现场三同形 05:08:10 / 05:09:52 / 05:11:18，
    第二代子进程 stderr 明写"已有 Core 在跑同一个数据目录"）。它**没开 SKIP、没放宽 25s、没重试到绿**，只把读数交上来 —— 这个处置我认。
    - **代码自己早就招了**：`cmd/core/lock.go` 的包注释有一条 `ponytail:`——
      "已知上限是 pid 复用会误判成'还活着'；真撞上再换 LockFileEx 或让端口当锁"。**这次就是"真撞上"那一次**，
      所以下面两个方向都算它预留过的路，不算新开设计。
    - **判活为什么错（把机理写清，别让下一个人再猜）**：`holderIsDead` 走 `os.FindProcess(pid)`，
      而 Windows 上 `FindProcess` 是 `OpenProcess`——**进程对象在任一句柄关闭前一直存在**，
      所以"已退"与"`OpenProcess` 失败"不是同一件事；父方还持有子进程句柄（测试就是父方）、或 pid 被复用，
      都会被判成"还活着" ⇒ 死锁接管不触发 ⇒ 第二代 Core 直接 `ErrAlreadyRunning` 退出、`core.endpoint` 不换。
      **那条"进程已退就报错"的注释只写了 error 分支，结论写过头了。**
    - **修法（选一个，别混）**：**(a) 端口当锁**（`lock.go` 注释里我自己列的备选、也是最小的一种）：
      绑不上就是真有活的 Core，绑上了就证明前一个必已死 —— **把判活交给 OS，不再做任何 pid 启发式**，
      `-take-over-lock` 那格随之变成不必要；唯一要补的是诚实处置：绑不上时对该端口发一次 `/health`，
      答得上来才报 `ErrAlreadyRunning`，答不上来就是"端口被别人占了"，**不许复用那句假原因**。
      **REQ-T21 裁定（10:2x）：这里原来只有两态，盘上至少三态**——第三个态是**"还在启动中的 Core"**：`main.go` 的顺序是 绑端口 → 拿锁 → 开库 → 迁移 → 才 `Serve`，所以"端口已绑、HTTP 还不应答"是一段**合法且必然存在**的窗口（2s 期限后 `healthAnswersAt` 会返回 false）。**处置**：这一态**不许直接判成"端口被非 Core 占用"**（那是把"正在启动"伪装成"别人占了"，与删掉 pid 启发式同一个毛病），要**有界重试**——按固定间隔轮询该端口，直到它开始应答（⇒ 老实报 `ErrAlreadyRunning`）或超过期限（⇒ 才允许报 `ErrPortTakenByOther`）。**期限由 backend 交账时带依据**：必须 **>** 实测的"迁移 + 首次 Serve"耗时，不许拍一个整数；顺带把那段时间量出来报进账——它是这条判据的地基。
      **(b) boot-id + 新鲜度**：锁文件写 `pid` + 本次启动随机标识 + 心跳时刻，判活要"心跳在时限内新鲜"**且** pid 对得上 —— 更重，多一个后台写文件的活。
      **v0.1 我倾向 (a)**（更少机器、更权威），但**实现方（backend）有权在交账前换 (b) 并说明理由**。
    - **验收判据两条都要**（这是关键：**间歇缺陷不能只靠"再跑 100 次不红"来关**）：
      ① **确定性那条**：造一个"pid 可 `OpenProcess` 成功、但那其实不是活 Core"的锁文件，`holderIsDead`（或替代物）
      必须判**死**并让接管发生 —— 只有真数据能红的不算闭上（§1.1 第 16 条同族）；
      ② **量级那条**：按契约② FL 表 **FL-1 那一行的命令形状**（`flaky_watch.sh` 带用例名与主包）把 `-n` 提到 **100**，
      对 `TestCrashRestartSameDataDir` 采样 ⇒ **单跑 100 次 0 红**（今天的基线是 4/65 ≈ 6%），两批之间不许改测试预算。
      **具体那一行由 `test` 写进 FL 表并自证能跑通**（本契约只登记"要跑到什么量级"，不代它抄一条没验过的命令行）。
      **采样必须同时交"被验树指纹 + 构建时刻"（09-24 07:5x 补，`test` 自报一次作废之后收编）**：
      一次 100 跑只有在**指纹证明确实带着修好的那份码**时才算数，**无指纹的 0 红不是证据**。
      起因（`test` 07:3x 自报、巡检 07:43 转述）：**那一跑 122/122 全绿，但构建里混进了别人未跟踪的在制品 ⇒ 它自己把那次采样作废了**，
      换用 `go list -deps` 当指纹门。⇒ 结论我按转述写成规则，不冒充我读到的原话：**同一份采样在未修码上也可能全绿**。
      **这一格与 FL-8 的时间线档不是一件事**：那一档问"窗口里有没有人在改"，这一格问"**改的是不是被验的那份码**"——**两问都要在，缺一不可**。
      **在两条都绿之前，Wave-2 那道"真子进程 + 崩溃注入"的门每 ≈15 次合验会真红一次 ⇒ 合验结论里必须写明这一格未闭。**
    - **归属**：`core/cmd/core/**` 归 `backend`（§7）；`scripts/flaky_watch.sh` 归 `test`（它只管采样与定档，不改产品）。

20. **CO-44 / CO-45 裁定（09-24 07:0x，回 `adapter` 与 `db` 的两问）：`process_instances` 的写口语义。**
    背景：`platform/db` 三个入口 09-23 05:35:54 已到货且签名与第 17 条 3/3 一致，这两问是那条链上**唯一没答的题**，
    `adapter` 因此"前提消失仍不落码"（按"前提消失 ≠ 禁令解除"）——**这个处置我认，答案在我这边。**
    - **CO-44（`TouchTransition` 返回 0 行的两种现实）：平台层保持不可区分，区分责任放回调用方。**
      ① **0 行就是 0 行**：不插事件、**不做第二次 guard 尝试**、不重试到"落成功"。
      **明确否决**"第一次 0 行时再拿 `STARTING` 试一次"那个选项——它把一次观察变成两条猜测顺序的分支，
      而"行还停在 `STARTING`（从没收到 start）"这件事**没有主体可归属**：正确的出口是**审计登记**
      （"我知道有个 pid 退了，库里没有它的行"，`RecordDrop` 那一族），**不许用聚合事件去冒充一个没有主体支撑的事实**
      ——与第 17 条末段对 `AppendEventOnly` 的收紧是同一条规矩的两面。
      ② **但 0 行不许静默**：`internal/process` 的写口拿到 0 行时**必须喊出来**（`logf` 或审计），
      否则"幂等"与"从来没成功过"在事后不可分。
      ③ **平台层不为此改签名**：`db` 提的"0 行时补一次 `SELECT status` 并把区别放进返回值"**否决**（YAGNI；
      区分只在调用方有语义，平台层多一个返回值就是多一个所有人都要解释的空位）。今天的形状
      （两种都给 `(0, nil)` 且不插事件、`TestTouchTransitionZeroRowsBooksNoEvent` 双向钉住）就是我要的。
      ④ **对判据的连带后果**：`exit_code`/`exited_at` 的可断言面**只在"先落了 started 那一行"之后存在** ⇒
      `TestLifecycleRowLandsTheFiveColumns` 必须**先建再落 exit**，不许指望从 0 行状态里把退出码落进库。
      ⑤ **`guardVal` 只能用"写口自己写进库的那个状态"，不许用"它观察到的状态"**（09-24 07:2x 由 `adapter`
      落码时用的 `landedStatus` 反推成规则，不是我凭空定的）。**量出来的前提**：`supervisor.go` 发 `EventStarted`
      时带的是 `StatusStarting`，而**没有任何一种事件带 `StatusRunning`** ⇒ 行建出来是 `STARTING`、也停在能被 guard 的位置；
      拿"我观察到它在跑"当 guard 会**每次都得 0 行**，于是每一次退出都被静默判成"这条边已落过"。
      **写口必须自己记账：我建的那一行当时是什么状态，我就拿什么当 guard。**
      **配套一句（同一来源）**：一次尝试一行 ⇒ **一行只落一条终态边**，所以 `landedStatus` 在这一格里**不推进是有意的**；
      将来谁要在同一行上落第二条边（例如 `FAILED → EXITED`），**必须同时把 guard 换成自己上一次落进去的那个状态**，
      并且 0 行照旧喊出来——**不许为了让第二条边落成功而回头用旧的 guard 再试一次**（那是 CO-44 ① 已否决的那条路）。
    - **CO-45（同一个 Supervisor 第二次拉起撞 `PRIMARY KEY`）：裁 (a) 一次尝试一行。**
      理由：表名与 `idx_process_instances_active(process_type, status)` 都读成"一次实例一行"，
      而 (b)（第二次改走 `TouchTransition(FAILED→RUNNING)`）**会把 pid 历史压没**——崩溃预算要数的正是"哪一次尝试、哪个 pid"。
      连带三条：① **id 源由 `internal/process` 的写口持有**（注入 `NewID func() (string, error)`），
      平台层不铸号（`CreateVersionless` 的 `id` 本来就由调用方预生成，第 17 条不改）；
      ② **`metadata_json` 记 attempt 序号 + 上一条 instance id**（**不开新列**：`000001` 冻结、迁移是 `db` 地盘），
      这样"一个 Supervisor 的第 N 次尝试"可复原，而 pid 历史留在各行里；
      ③ **Recovery 数"活进程"的口径登记在此**：`status IN ('STARTING','RUNNING','STOPPING')` 按 `process_type` 分组数行数。
      **谁都不许自己发明第二个口径**；要改（例如把 `STOPPING` 算死）写 REQ 给我，**改本条而不是改代码注释**。

21. **`notifications` 的状态机边集（09-25 19:2x 裁定，回 `backend` 第六十六批的问题："`:handle` 的边集不在契约里，我不自己定"）**。
    `backend` 这句话是对的：`grep HANDLED specs/` 只命中 `core-v1.yaml` 的 enum，两份契约里**从来没有一张通知状态机表**，
    而"CHECK 约束里有 `HANDLED`"**不是裁定**——它只说明这一格能存，不说明谁能走到。现在补上。
    - **词表（读自盘）**：`000001_core_schema.sql` 里 `CREATE TABLE notifications` 的 `state` 列 CHECK 给六个值
      `UNREAD` `READ` `PENDING` `HANDLED` `IGNORED` `EXPIRED`，Go 侧同名常量是 `internal/notification` 的 `State` 那一组
      （`StateUnread`…`StateExpired`，`domain.go`）。
    - **裁定（认 `backend` 在 C36④ 自挑的默认，并补它没问的两格）**：
      ① `→ HANDLED` 的合法前态 = **`UNREAD` / `READ` / `PENDING`**；
      ② `HANDLED → HANDLED` = **200 no-op**：版本不动、`handled_at` 不覆写、**不发事件**（与 C36① 已认的 `:read` 重放同一口径）；
      ③ `IGNORED → HANDLED` 与 `EXPIRED → HANDLED` = **409 `illegal_transition`**（沿用 `ErrIllegalTransition`；
      §15.2 那张表明写这一码"来自各 Service 而非 Adapter"，正是这里该用的那个）。
    - **为什么 `READ` 必须在允许集里（这就是那道二选一）**：`HANDLED` 记的是"用户处置过"，`READ` 记的是"用户看过"，
      **两件事没有先后关系**。只放 `READ → HANDLED` 等于把"先看后处理"写成产品流程：面板要在用户点"已处理"时先补发一次 `:read`，
      于是**一次用户动作在 journal 里留下两条边**，而其中第一条（`notification_read`）根本不是用户做的事。
      **准入集跟着"哪些状态还没有处置痕迹"走，不跟着 UI 的点击顺序走。**
    - **与 `:read` 的不对称是有意的，别来"统一"它**：`:read` 只放 `UNREAD`/`PENDING`（C36②，已认），`:handle` 多放一个 `READ`。
      同一条理由的两面——**`HANDLED`/`IGNORED`/`EXPIRED` 是终态**（`IGNORED` 本身就是用户的处置、`EXPIRED` 是系统判定的时效已过，
      都不是另一个动作该覆盖掉的痕迹），而"读过"不是处置痕迹，把它改成"已处理"不抹掉任何已发生的事实。
    - **`read_at` 与 `handled_at` 各归各（`backend` 没问，但这一格一定会被猜）**：`UNREAD → HANDLED` **只写 `handled_at`，不补 `read_at`**。
      两列都已在库里（`CREATE TABLE notifications` 的 `read_at` / `handled_at`，均可空）。把"处置"折成"也算读过"，就是替面板抹掉"未读但已处理"这个真实形状。
    - **事件面不新增任何东西**：`notification_handled` 早已登记（事件契约 §6 的合并行 `notification_created` / `_handled` / `_read`
      那一格），payload 就是那一格的四键 `notification_type` `priority` `task_id` `approval_id`，`aggregate_type = notification`；
      **不要求 `Idempotency-Key`**（C36③ 已认，同一条子树同一口径）。
    - **落地判据（这一格怎么算证到）**：同一事务里 `Touch(AggNotification, …, "state = ?, handled_at = ?")` + 恰一条 `notification_handled`；
      用例至少钉六格：`UNREAD→HANDLED` / `READ→HANDLED` / `PENDING→HANDLED` / `HANDLED` 重放（断**版本不动**且 **journal 不增长**两样）/
      `IGNORED→409` / `EXPIRED→409`，且 409 那两条要断**一行都没写**。**反例两条起步**：摘掉 `handled_at` 的写入 ⇒ 必须有用例红；
      把 409 那道闸改成"任意前态都盖" ⇒ `IGNORED`/`EXPIRED` 那两条必须红。

22. **`adapter_downgraded` 的归属：`:sync` 的 503 半格归 `adapter`，且 503 出口不发事件（09-25 19:2x，回 `backend` 第六十六批的另一问）**。
    `backend` 把这一格从"等主控 REQ-B-1"改成"归 `adapter`"，**换归属换对了**，但它引的依据不是最硬的那条，所以这里连依据一起钉下来。
    - **它引的**：事件契约 §6 `adapter_downgraded` 那一行的触发者列 = `adapter`。**成立。**
    - **更硬的一条（机械不可达，不是"没做完"）**：这个事件的 `aggregate_type` 是 `adapter`，而 `Tx.Touch` 要求事件上的聚合
      **等于被 Touch 的那个聚合** ⇒ 从 session 这条腿发它，要么被守卫拒，要么把它错标到 session 名下。
      **盘上已有现场证据**：`internal/session` 的 `TestAdapterDowngradedCannotRideASessionTouch`
      断的就是"必须红，且红的理由含 `aggregate_type` 「adapter」但 touching「session」"（原文是那两条反引号里的英文，别照抄我这行的写法去 grep）。
    - **落点**：只有 `internal/adapter` 能写 `adapter_instances`（契约③ REQ-AD-1 裁定段：`internal/process` 按设计不持 DB 句柄；
      §7 归属行 `core/internal/adapter/**` + `process/**` = `adapter`）。所以这条事件的发射点在 `internal/adapter` 的注册/核定域里，
      Touch 的是 `adapter_instances` 那一行。
    - **发射时机（防止被做成"每次 503 顺手补一条事件"）**：`adapter_downgraded` 的语义是**一次能力核定的变化**，
      与"某个客户端某次请求拿到 503"无关。① 只在降级**发生**的那一刻发（握手 / probe 核定出 effective < requested 那一处）；
      ② **`:sync` 的 503 出口只回码、不发事件、不 Touch 任何聚合**——`backend` 现在的形状是**正确终态**，不是半成品，
      不许再为它开"半个事件"的口子；③ 响应里若要体现"为什么降级"，那是**读**已落库的 capability/health 行，
      不是在响应路径上写事件（否则一次轮询刷一条 journal，与 C36①"重放不发事件"是同一条理由）。
    - **REQ-B-1 的处置：结案，且不需要新通道**。守卫的行为正是我要的 ⇒ **不新增裸写入口、不为这一格改 `platform/db` 的签名**
      （第 17 条那句"不许为这两格再开一条 `Append` 之外的裸写通道"照旧生效）。
    - **排程：不排在 Wave-2 闸门之前**。这一格在 `adapter` 的队列里，等闸门之后由主控点名；今天只补归属与登记，
      **不给任何角色新增前置**。同时按 §6"已登记 ≠ 有生产路径"，把 `adapter_downgraded` 记进**有名字无生产者**名单
      （09-25 19:2x 实测：全仓 `adapter_downgraded` / `AdapterDowngraded` 在非测试码只剩 `internal/eventjournal` 的
      `AdapterDowngraded` 常量与注册表里那个 `true` 条目两处）。
    - **同一条 grep 顺出来的更正**：事件契约 §6 名单里 `session_discovered` 那两断句（"非测试码没有任何一处发它"、
      "`sessions` 表在非测试码里没有任何写路径"）**已被事实超过**——`internal/session` 的 `discover.go` 走
      `Tx.Create(db.AggSession, …)` + 恰一条 `session_discovered`，`cmd/core` 的 discover 腿也已装配（`newAdapterServices` 的返回值
      经 `Core.discoverField()` 注入，没配 Adapter 时给 nil ⇒ 501）。名单改成"两条 + 一条新补"，原文按"历史不静默改"留档不删。


23. **REQ-S-1 裁定（09-25 19:55）：写事务里不许有网络往返、起子进程 —— 这条是 Wave-2 闸门上的产品缺陷，不是测试噪声。**
    触发事实（`frontend` 19:22 交的红，证据在 `.qoder/evidence/frontend/probe-t286lk.txt` / `live-t286lk.txt`）：
    `POST /executions/{id}/send` 期限内不回应，**随后 `POST /projects` 也不回应，而 `GET /health` 仍 200**。
    - **结构性的那半我 19:54 直接读代码量到，不依赖任何计时**：`api/http/executions.go` 的 send handler 把
      `svc.Send` 整段包在 `h.store.Write(...)` 里，而 `execution.Service.Send` 在**同一个 `tx` 上**调
      `CallSendTurn`（其注释就写着"`CallSendTurn` 排在 seal 之前：失败 ⇒ 整笔回滚"），`cmd/core` 的 `sendPipe` 那一步做的是
      `Connect`（**起子进程**）→ `Initialize`（握手）→ `CallTurn`（等回执）。而写通道是 `internal/platform/db` 的
      **`write.SetMaxOpenConns(1)`** —— 一条串行的写腿。**⇒ 一次 send 能把整个 Core 的写停住**，
      读面与 `/health` 走另一条池所以照答。这不是"某个 Adapter 慢了"，是**形状**问题。
    - **期限那一层今天没有真的界住（这一半不由我判成因）**：`sendPipe.CallSendTurn` 只在**入参 ctx 没有期限**时
      才套 `probeAttemptDeadline`（那是 **probe 的 10 秒常量**，语义是"一次探针尝试"，不是"用户等一次投递"）。
      `frontend` 的读数是"挂住 >20 秒仍未恢复"（它那两条用例的客户端超时分别 5s 与 20s，20s 那条也红）
      ⇒ **要么兜底没生效、要么 10 秒之后还有别的东西持着写腿**。两种都还没排除，**所以修之前先量**（见"要交的读数"）。
    - **裁定：修，且排在 Wave-2 闸门之前**（崩溃注入那半条腿要的就是"Adapter 挂住 / 崩掉时 Core 还能活着答话"，
      拿今天这个形状去跑，测到的会是"整个 Core 停摆"，那不是我们要断言的东西）。
      **不变量**：`db.Tx` 的生命周期内不许出现 ①起子进程 ②协议握手 ③等对端回执。适用于**所有**走连接面的腿
      （`send` / `probe` / `discover`）—— 后两条今天是否同形，**由当事人自己量了回报，我不猜**。
    - **修法边界（三条，越界即退回）**：
      ① **不许靠调大/调小期限糊过去** —— 那只是把停摆从"无限"改成"N 秒"，面板上仍是一次假死；
      ② **两段式要明写幂等语义**：第一笔事务落"意图"（消息 + 状态边 + 事件）并**提交**，第二笔落连接面的结果
      （成功推进、超时/失败落 `UNKNOWN`）。**重试拿到的答案不许悄悄变** —— 今天"失败整笔回滚 ⇒ 重试得到新答案"这条
      语义（`internal/execution/send.go` 注释里写明的）如果要改，**改本条并登记**，不许只改代码；
      ③ **挂住的 Adapter 一律落 `UNKNOWN`，不许 `FAILED`**（契约② 三态口径；**适用范围由第 25 条④收窄：只指"崩溃恢复"那一支
      （`Occasion=Recovery`），同步失败/挂住这一支 v0.1 留 `RUNNING` + 503 `adapter_unresponsive`**），且 Core 其余的写必须在**一个明确上界内**恢复。
    - **要交的读数（先归因、后动码）**：`backend` 交"兜底为什么没界住"的三段分别计时 —— `Connect`（起子进程 + 等 hello）、
      `Initialize`（握手）、`CallTurn`（等回执）在 ctx 取消后各多久返回；`adapter` 交它包里那两段的"吃不吃 ctx"事实。
      **谁持锁谁量**，我不替任何一方判成因。
    - **落地判据（必须走 handler 层，低层证据不算证到这一格）**：真起 fake adapter + `debug/hang` 注入，
      在 `send` 挂住期间另发一条 `POST /projects`，**断它 1 秒内 201**（N = 1s：不变量的意思是别的写**根本不该等**，
      不是在等一个期限到期）；再加一条反例 —— **把连接面那一步搬回写事务里，用例必须红**（证明它有牙，不是装饰）。
      **`--- SKIP` 不算样本**，注入不生效的用例不许报绿。

    **19:5x 补：第二次探针（`frontend` 19:36，`.qoder/evidence/frontend/probe-send20.txt` + `corelog-send20.txt`）把"哪一步卡住"收窄了三格** —— `probe` 177ms 200、`discover` 11ms 200、`executions?task_id=` 13ms 200，**唯独 `send` 30 秒不返回**；随后 `POST /projects` 8 秒仍不返回；Core 日志全文 6 行、`grep -cE 'panic|deadline|timeout|ERROR'` = **0**。
        - **排除①："读不到自己未提交的库"不是成因。** `discover` 那条腿做的是同一件读事，毫秒级就过了；`sendpipe` 第一发 `p.read.QueryRowContext` 走的是**读池**（4 条连接 + WAL），不会被自己那条写事务挡住。
        - **排除②：不是 SQLite 等锁超时。** `connPragmas` 里 `busy_timeout(5000)` ⇒ 真在等锁的写会**报错**（`database is locked`），而不是永不返回；日志里也一条没有。
        - **卡点因此只剩一种形状：ctx 到期之后仍有同步步骤不返回。** `internal/process` 的 `Connection.request` 我 19:54 读过，它是 `select { reply / ctx.Done() / c.dead }` —— **等回执那一腿吃 ctx**。所以不返回的只可能在 select 之外的同步调用上，三处待实测点名：`Connect` 里"起孩子 + 等 hello"那一段、ctx 分支里的 `c.Cancel(id)`（它还要往**同一条**连接 `write`，孩子不读 stdin 时就是二次堵塞）、以及 `sendPipe` 的 `defer conn.Close()`。**这三处由 `adapter` 交读数，我不替它挑。**
        - **另一条独立的平台事实，登记在这里（它解释"为什么别的写会无限等，而不是 5 秒报错"）**：`POST /projects` 卡的是**从池里取那唯一一条写连接** —— `write.SetMaxOpenConns(1)` 的等待发生在 Go 侧 `database/sql`，**`busy_timeout` 管不到它**，而 HTTP 请求的 ctx 没有期限 ⇒ **等下去没有上界**。⇒ 只要任何 handler 在写事务里做外部 I/O，全 Core 的写就是无限期挂起。**这条对所有 handler 成立，不只对 `send`。**
        - **修法有现成先例，不是新发明**：`cmd/core/discoverassembly.go` 今天的顺序就是 `Connect` → `Initialize` → `Call` → **再** `dbh.Write(tx)`（它没有外部副作用，所以"后写"够用）；`internal/adapter` 的 `Registry.Probe` 同形（`exchange` 完了才 `RecordProbe`）。**`send` 不能照抄"后写"** —— 连接面那边会留下一次真投递，所以它要的是上面 (b) 那格：**预留 → 提交 → 投递 → 第二笔落结果**。期限只是止痛，不变量照旧。
    - **20:2x 归因读数已交（`adapter` 第 24 轮，`ctx-probe.sh` / `ctxprobe_test.go.txt` 20:08~20:09），我按字核过并纠正两处**：
      ① `Supervisor.Start(ctx)` 函数体 29 行里 **一次都不读 ctx**（我 grep：体内 `ctx` 出现 1 次、`ctx.Done`/`ctx.Err` 0 次）
      ⇒ 拿一个**已取消**的 ctx 进去照样把孩子起出来（它量的 **588ms**、`err=nil`）；`Connect(ctx)` 只透传 + 组装
      （**2.25ms 返回、不等 hello**，等 hello 在 `Initialize`→`request`）。**⇒ 我上一条列的候选 (a)"Connect 等 hello 那一段"排除掉**，
      但它换成了一件更糟的事：**取消之后仍然会起一个没人负责的孩子**。
      ② **`request` 那一腿确实吃 ctx**（400ms 期限 → 400.31ms 返回、err 里带 `sent $/cancel`、`pending` 归 0 不泄漏）
      ⇒ 候选 (b) 在"小帧 + 孩子还在读 stdin"时不堵；**真正剩着的两处是 `Cancel` 里那次 `write`（孩子不读 stdin 且管道缓冲满时是第二次堵塞）与 `Connection.Close()`** —— 这两处仍待现场量（要真 handler + 真 `session/send` + 卡死孩子，探针形状不算）。
      ③ **"三条路径都不杀进程、`Child` 没有 Kill"这句不实，我按盘更正**：`Supervisor.Stop(c, timeout)` 里就是
      **`c.cmd.Process.Kill()` + 有期限的 `c.Wait(ctx)`**（先例 `cmd/core/processpipe.go` 用 **2 秒**）。
      真相是**连接收尾没走它**：`Connection.Close()` 只做 `kill(错误)`（把连接标死）+ `child.CloseStreams()`（关管道），
      **没有一步杀进程** ⇒ 探针里四个孩子全活着是"没人调 `Stop`"，不是"缺这个能力"。
      **裁定**：连接收尾要接上**既有的** `Stop`，期限 **2 秒**、与 `processpipe` 同值 —— **不新增第三种期限、不另开 Kill**。
      "关 stdin 让孩子自退"可以作为 `Stop` 之前的礼貌一步，但**不许把它当成回收机制**（孩子不守规矩时没人兜底）。
      **通用教训**：看任何带 `ctx` 参数的接口，别信签名 —— 数函数体里 `ctx.Done()`/`ctx.Err()` 出现几次，
      再拿一个**已经取消的 ctx** 真跑一次量耗时；判"取消生效"要同时看**多久返回**与**副作用有没有停下来**（进程、句柄、在写的行）。
      ④ **20:4x 登记第三处（`Connection.write` 不可取消）——归属已定，见本条末段**：`request` 的 ctx 分支是在期限**之后**
      才调 `Cancel`→`write`，而那一次 `write` 里没有 `select ctx.Done()`；实测形状 = "给一个卡死的孩子发 256KB 帧，
      400ms 期限过后又堵了 **44.8 秒**"。
      **归属与证据状态（21:1x 二次更正，最终版）**：这条秒数**现在两份日志都不在盘上**，按第 24 条第 ④ 款处理 ——
      **不判归属，降级为"曾量到、输出未留存"**。过程留档防重演：`backend` 那份
      （`.scratch/backend/reqs1_timing.txt`）我 20:46:40 量到 12410 字节 / 四段命中，20:47:38 已被它自己的
      `find -newermt "10 minutes ago" -delete` 清掉；`adapter` 那份（`.scratch/adapter/ctxprobe_out.txt`，
      一行 `took=44833.49ms`）我 20:49 量到在盘、**21:04 复量 `test -f` 已不在**，`grep -rn '44833' .scratch/`
      只剩我自己的两份转抄。⇒ 我 20:5x 那句"只有 `adapter` 那份可复跑"**从写下那刻就在衰减**，那是个状态句，已删。
      **不受影响的**：`Connection.write` 的 ctx 分支里没有 `select ctx.Done()` 是**读代码就能证**的，不依赖那个秒数。
      "收尾顺序"那一格**另有一份现在就在盘上的证据** —— `.scratch/adapter/probe4-full.log`（1433 字节）三档：
      `A5 Stop(child,2s) 耗时=1ms`、`B1 先 Stop 后 CloseStreams 耗时=1ms`、
      **`C3 孩子已礼貌自退时 Stop 报 err=kill: invalid argument` 且 `Reaped()=false`** ⇒ 它就是 `REQ-S-2` 的现成反例。
      **21:1x 三次更正：我 21:1x 把另一句当证据写了进来，那句引错了，收回** —— 我写的是
      "`delivery-r25-203909.log` 里有 `killStreams took 30.44s` 这一计时读数"，**不成立**：
      我 21:14:44 自己复跑 `grep -c killStreams` = **0**，而 `30.44` 只命中一处 ——
      **第 822 行 `--- FAIL: TestCloseReapsTheChild (30.44s)`**，同一份文件末尾是 `FAIL`。
      ⇒ 那是**一条红的用时**，不是计时读数（"红了 30 秒"与"先还管道会卡到超时"方向一致，**只能当旁证**）。
      **那条 FAIL 已被后一批取代**，同样当场用现盘读数证：`.scratch/adapter/delivery-r25-210428.log`
      （21:04，94177 字节）`grep -c "^FAIL\|--- FAIL"` = **0**，同一条用例在第 821 行
      **`--- PASS: TestCloseReapsTheChild (0.55s)`**。⇒ 引"顺序代价"这一格**只许引 `probe4-full.log` 那三档**；
      要带载量级就得重跑一次并把输出留住（回到第 ④ 款）。
      **裁定**：`write` 放进一个可放弃的 goroutine，`select { 写完 / ctx.Done() / c.dead }`，超时即标死连接、交给 `Close` 回收孩子。
      **顺序：先交 Stop 接线那一批，再补这一格**（两批判据不同，不许合成一批）。附带一句实话：Stop 接线一落地，本格的严重性会自动降一档
      —— 孩子被杀 ⇒ 管道断 ⇒ 堵住的 `write` 拿到 EPIPE 自行解开；但这正说明"回收"与"可取消"是同一件事的两半，
      **两半都要**：只靠 EPIPE 是撞运气，不是设计。

    - **⑧ 调用方义务：连接面上每一次调用必须自带期限（09-27 13:3x 登记，实测出处是 `adapter` 的 CO-53）**。
      `internal/process` 的连接面**不检测、也不自救"不带期限的 ctx"**：孩子不再读 stdin 时，一次 `request(context.Background(), 256KB)`
      —— **5 秒内不返回、`c.dead` 仍 `false`、`pending` 残留 1、`writeMu` 被永久占住**（没期限就没有任何自救）；
      排在它后面**自己带 400ms 期限**的那一条仍在 400ms 返回（第二批的 `abandon` 生效，楔子不扩散给有期限的一侧）；
      **唯一能解开它的是 `Close`（实测 2ms）**。出处：`.scratch/adapter/co53-probe7-130609.log`（`--- PASS`，跑窗 13:06:09→13:06:18）
      —— 按第 24 条⑤款：**这份 log 一旦灭失，本条只按"曾量到"引用，不许再拿那几个秒数当已证事实**。
      ⇒ 义务写在**调用方**，不写在平台层：**每个走连接面的 handler / 装配点必须自带一个期限常量**。
      今天第一个落点是 `cmd/core` 的 `sendAttemptDeadline`（09-27 13:11 我给 `backend` 的那个数），第二格是 `probeAttemptDeadline`。
      **平台层不分"谁忘了带"，所以这条既不做成 lint、也不做成运行期 panic**（与 CO-44 同一口径：调用方必须自己喊）。
24. **证据生命周期与"判不存在"的正确跑法（09-25 20:5x 立，今天我自己撞的两条）**。
    - **① 证据要活到对方交账那一刻。** 一次性产物照旧只进 `.scratch/<角色>/`，但**"一次性"的判定时刻是交账之后，不是我跑完那一刻**。
      实例：一条关键读数（256KB 帧发给卡死的孩子、400ms 期限之后仍堵 44.8 秒）出自 `.scratch/backend/reqs1_timing.txt`
      —— 我 20:46:40 量到它在盘上（12410 字节、四段命中），**20:47:38 已被当事人自己的
      `find -newermt "10 minutes ago" -delete` 清掉**，而他 20:45 还在引它。
      ⇒ **`find -delete` 这类清理要避开刚被引用的文件**；要清，先在交账里写一句"日志留着在 X"或"已灭失"。
    - **② 判"某个文件不存在"，禁止用截断过的输出反证。** 我 20:44:49 跑的是 `ls -l .scratch/backend/ | tail -6`，
      而 `reqs1_timing.txt` 按字母序排在最前 —— **正好被 `tail` 砍掉**。我据此判人家"引用不存在的证据"并发了纠正信，
      20:46:40 复量后收回。正确跑法二选一：`test -f <确切路径> && echo yes || echo no`、或 `ls -la <确切路径>`
      —— **给确切路径，不要给目录**。判"某句话在不在账上"同理：**中文 pattern 经这个 shell 传给 grep 会静默 0 命中**
      （GBK 控制台 vs UTF-8 文件），要用 python 计数或直接读那一行原文。
      **并且：派发行里凡是否定结论，必须把我跑过的命令原文一起写进去**，让对方能自己重跑 ——
      否则我的一句话比对方一条留存的日志更没证据力。
    - **③ 归属判据：同一条读数有两个主张时，证据现在还在盘上的那一方拿归属**（注释、转述、记忆都不算证据）。
      今天的落点（**已被 ⑤ 推翻，原文留着防重演**）：我 20:5x 据此判 44.8 秒归 `adapter`，
      因为 `backend` 那份灭失、而 `.scratch/adapter/ctxprobe_out.txt` 我 20:49 量到还在（一行
      `write 256KB→stuck-child: req=400ms deadline, took=44833.49ms`）。
    - **④ 拿"某文件现在还在"当前提的裁定，必须在使用那一刻重新 `test -f`；状态句不许写进契约。**
      21:01:48~21:02:28 巡检三条命令（`test -f`、`grep -rn '44833' .scratch/`、`grep -rn '44\.8' .scratch/adapter/`）
      证明 **`adapter` 那份也没了** ⇒ 我 20:5x 那句"只有它还能复跑"**从写下的那一刻起就在衰减**，
      而我把它写成了永久状态。**规矩：契约里只许写"某时刻 + 某命令 + 某结果"，不许写"现在可复跑"这类无时戳状态。**
      **两份都灭失时的处置（本案就用这条）**：**其一 不判归属** —— 出处本身也标"未定"（两份台账各说各的、日志都没了，
      再判就是凭偏好）；**其二** 读数降级为**"曾量到、输出未留存"**，不许当已证事实引用；
      **其三** 涉及的角色归属**改由归属表支撑**（`internal/process` 归 `adapter`，见契约③ §7 —— 这条本来就不依赖那个秒数）；
      **其四 要恢复成事实只有一条路：重跑**。脚本还在盘上（`.scratch/adapter/ctx-probe4.sh`，20:42），
      输出必须留在 `.scratch/` 里活到我入册之后（回到第 ① 款）。
      **其五 本条结论不受影响**：`Connection.write` 的 ctx 分支里没有 `select ctx.Done()` 是**读代码就能证**的，
      44.8 / 30.44 这类秒数只是把它的重要性量化，**没有它们这一格照样要修**。
    - **⑤ 附一条同族的"格式串漏参"**（`adapter` 那份日志里同一批行写着 `got=%d` 未被格式化）：
      数值本身是真的，但**别人读到 `got=%d` 会以为这一档没量到** ⇒ 探针脚本里的 printf 参数要对齐，
      交账前自己先读一遍输出原文（与"守门判形状不判字面行数"同族：读的是**能不能被误读**）。
    - **⑥ 引一条读数之前，先看它所在那一行的前缀是什么**（09-25 21:1x 由巡检抓我第二次，我 21:14:44 自己复跑确认）。
      我把 `--- FAIL: TestCloseReapsTheChild (30.44s)` 的 **30.44 秒**当成"某个操作耗时 30.44s"的计时读数写进了契约 ——
      那其实是一条**红的用时**（同一份 log 末尾就是 `FAIL`，且 `grep -c killStreams` = 0，我引的字段名根本不在文件里）。
      **同一段字节挂在 `PASS` 行、`FAIL` 行、还是裸日志行上，是三件不同的事实**；`grep -c` 命中数一样，含义可以完全相反。
      ⇒ 交法：**引用时连行号 + 行首前缀一起贴**（`822:--- FAIL: …` 而不是"30.44s"），
      并顺手查这份 log 的**尾部是 `PASS`/`ok` 还是 `FAIL`** —— 尾部一个 `FAIL` 就能让整份"绿"不成立。
      **判"某条红有没有被后一批取代"也按这条办**：我去 21:04 那份 log 里查到
      `--- PASS: TestCloseReapsTheChild (0.55s)`、全文 FAIL 计数 0 ⇒ 才算取代，不靠"后来没人提"推断。
    - **⑦ 本格的最终证据状态（21:20 复量，读这一条就够，别再去翻上面三段）**：
      **所有带载秒数现在都不在盘上了** —— `probe4-load.log` 与 `ctxprobe6_out.txt` 我 21:20:08 逐条 `test -f` 都是
      **NO**（它们 21:1x 还各自给过 30.44s / 30.439741s），`44.8` 那份两份台账也都灭失。
      ⇒ 44.8 与 30.44 一律按第 ④ 款处理：**"曾量到、输出未留存"，不判归属、不作已证事实引用**。
      **现在还在盘上的只有两份**：`.scratch/adapter/probe4-full.log`（空机三档 `A5 Stop 耗时=1ms`、
      `B1 先 Stop 后 CloseStreams 耗时=1ms`、`C3 err=kill: invalid argument` 且 `Reaped()=false`）与两份
      `delivery-r25-*.log`（20:39 那份末尾 `FAIL` / 21:04 那份 FAIL 计数 0、同一条用例 `--- PASS (0.55s)`）。
      **结论不因此变弱**：`Close` 收尾顺序要"先 `Stop` 杀进程、再还管道"，靠的是**行为断言**（C3 那一档就是现盘反例：
      孩子已退 ⇒ `Kill` 报 `invalid argument`），不是靠"反过来会卡 30 秒"那个数量级；**判据也按行为写，不写秒数**。
      要恢复量级证据只有一条路：重跑 `.scratch/adapter/ctx-probe4.sh` 并让输出活到我入册之后。
    - **⑧ "被验对象自己未入册"时，"全量干净"的字面判据会永不可达（09-27 12:4x 裁，由 `test` 的真树腿那一问提出）**：
      判据若写成"被验面 = `core/` 全量干净才起跑"，而**被验的那个文件本身还没入册**（我的规矩是"当事人交账之后我才按角色切一笔入册"）
      ⇒ 这条腿**今天不可达**，而**不可达的判据不配留在表里**。定形：**排除"被验件本身"，其余必须真干净**，并附三条：
      **Ⅰ** 起跑前现跑 `git status --porcelain -- core/`，把**输出逐字存进读数**，并明写哪一件是"我自己未入册的在途件"；
      **Ⅱ** 除那一件之外**任何** `M` / `??` 还在 ⇒ **不起该腿**（等，不是绕）；
      **Ⅱ-b 替代路径（09-27 14:5x 补，由 `test` 报出的真空子）**：改跑一棵 **HEAD 快照**（不动工作树、不跳判据），
      读数里注明是哪一棵树（HEAD 短码 + 采样时刻），并要求**工作树 A 与快照 B 同分类**。
      加这一条是因为按字面执行会留一个缝：**"当事人已交账、主控尚未入册"那段窗口里这条腿永不可起**
      —— 09-27 13:01:38 起 `backend` 账上写着已交账、盘上仍有五件 ` M`，就是这一格；
      `test` 当轮的绕法（读 HEAD 快照 + 证明 A/B 同分类）比"干等入册"更强，故升成款。
      **Ⅲ** 该读数**不得当任何 REQ 或 Wave 的关闭证据**——它证的是判据自身，不是被改的行为。入册之后 Ⅰ~Ⅲ 自然消失。
      **同族要分开的两件事**：**"不可达"**（`REQ-RC-1`：今天没有生产路径 ⇒ 立项、不派工、不改状态机）
      与**"该等"**（真树腿：等别人的在途件写完 ⇒ 排除被验件自己就能跑）不是一回事，别拿前者当后者的理由。
    - **⑨ 状态句的时效（09-27 15:0x 补，由 `巡检` 的一条现量反驳逼出来；管契约也管账本行与派发行）**：
      任何"门开着 / 已经干净 / 现在可以起 / 只剩 X 件"这类**状态句**，落笔时必须二选一 ——
      **⑴ 带上测量时刻与它所依据的命令**，或 **⑵ 直接写成收件人可自跑的判据**
      （正例：「除被验件之外 `git status --porcelain -- core/` 零命中 ⇒ 真树腿可起」）。
      **今天撞的实例**：我 14:51:58 写"Ⅱ 的门开了"，**9 分钟后就反了** —— `backend` 收了 T-370 回到 `core/`，
      15:00 现量 5 件；而 `test` 已按 Ⅱ-b 绕开，所以我那句**既没促成什么、也差点误导一轮**。
      ⇒ 不带树与时刻的状态句存活期只有几分钟，**不如不写**。
    - **⑩ 交付前必跑清单是两面的，不许只交默认面（10-01 00:5x 补，由 `test` 的建议采纳并加码）**：
      **面 1 默认**：`go build ./...` + `gofmt -l` + `go test -count=1 -v ./...`（全仓）；
      **面 2 真子进程**：`LUCERO_TEST_FAKE_ADAPTER=1 go test -count=1 -timeout 1200s ./cmd/core/ ./test/`。
      **⑩ 款作用域（10-01 14:0x 收紧，来自 `frontend` 的实测反驳、我复现同值）**：`LUCERO_TEST_FAKE_ADAPTER` 在**非测试码里 0 个读取点**（含该名的 .go 文件共 **7** 个、全部是 `_test.go`；我 14:0x 自己 `grep -rln ... | wc -l` 数过，**原稿写的 13 是抄来的、已更正**）⇒ 这一面**只存在于带旗标门用例的那两个包**（`./cmd/core/`、`./test/`），**不是产品的一条运行开关**；产码路径的等价姿势是**现场 `go build` 出 fake-adapter，再用 `-adapter fake=<路径>` 起 Core**（`frontend` 走的就是这条，它比我原句更准）。**其他包不许被要求「造一面旗标」**——`adapter` 那句「旗标面对我是空集，按事实报不造一面」是对的，我照收。
      两面都要报**顶层 `--- PASS` / `--- SKIP` / `--- FAIL` 三个计数**，且**只能从带 `-v` 的那一趟数**（第 24 条⑤款那条盲区的延伸：非 `-v` 的输出里根本没有 `--- SKIP` 行，数出来 0 是「看不见」不是「没有」）。
      **两条理由都是这三天量出来的，不是预防性的**：① `TestTurnRefCrossCheck` / `TestDroppedFrameReachesAuditSeam` 在默认面是 SKIP、旗标面才是红，藏了不止一天（10-01 00:40:50 快照复验时现身）；② 同一份代码整包跑出现过 **3 趟 1 红**（`TestPumpReconnectsAfterTheAdapterDies`，孤立 `-count=5` 却 5/5 绿）⇒ **一条 `-count=1` 的绿不构成「交付证据」**，负载敏感的格必须写清复现率（`-count=N` + N 次读数）。
      配套禁令：**不许为了过旗标面关旗标、加 sleep、或把用例改成只跑孤立**——那是把红藏回看不见的地方。

25. **REQ-S-1 两段式的幂等语义与"投递结果未知"那一支（09-25 22:0x 裁，回 `backend` 第 276 / 281 条；它把草稿备好了、等裁不动码）**。
    - **① 语义定稿（把第 23 条那句"失败整笔回滚"改掉）**：`send` 是**一次可安全重放的尝试，答案来自第一次完成的尝试**。
      第一笔（意图：消息行 + 状态边 + 事件）**提交即成为既成事实**；同一 `Idempotency-Key` 的重试
      **不得**产生第二条消息或第二条状态边；若第一笔已提交而进程在第二笔之前中断，claim 行保持未密封 ⇒
      同 key 重试走**续跑**（再打一次连接面 + 密封），**不是**永久 409 `idempotency_replay`。
      ⇒ 第 23 条②里那句"重试拿到的答案不许悄悄变"**按本条定义读**（"不许变"指**库里事实**不许多算一次，
      **不是**"永远回同一份字节"）。
      ⚠ **09-27 更正我自己在这一格里写下的字面**：原文写的是"重放响应体里的 `version` 必须是**当前库里的版本**"，
      而 `SendResult` 今天**没有 `version` 这个键**（现读 yaml：只有 `delivery` / `accepted` / `message_id` / `execution_id` 四键，`required` 是前两个）
      ⇒ **一句引用不存在字段的判据是假判据**。"当前性"因此**经读面回库里那一行来判**（`GET /api/v1/tasks/{id}`），不判响应体字段。
      **要往 `SendResult` 加 `version` 属于改对外契约**：必须连面板侧的类型与生成物一起推，**另开 REQ，不为一条判据开路**
      （`backend` 第 285 条问的就是这个 —— 它没偷偷加字段，做得对）。
    - **② 续跑凭证放哪儿：`idempotency_keys.response_json`，且必须登记这一列的两义**。
      `backend` 量的地基我复现了：`claimIdempotency` 只在 `state='COMPLETED'` 时读 `response_json` ⇒
      **`IN_PROGRESS` 期间那一格是无人读的闲置位**。裁：**claim 时把第一笔身份写进 `response_json`，密封时覆盖成真响应**。
      **这条"一列两义"必须连同读取条件一起钉住**，否则下一个人只看到"response_json = 响应"就会读错
      （与 REQ-F-21"同一个字段名两套口径"、F20 那格同族：键的存在与含义都要有唯一说法）。
      ⇒ **不开新列、不动迁移、不改 ID 规范**（migration 是 `db` 的地盘；确定性派生 id 换的是 ID 规范，风险另一档，两条都否决）。
    - **③ 并发双续跑：容忍，但要喊**。密封已经是 `UpdateOnce(… guard state != 'COMPLETED')` ⇒ 只有第一个能封上，
      第二个拿 0 行 ⇒ 按 CO-44 那套**必须出声**（不许静默当成功），且**第二条真投递由 §4 透传的 `Idempotency-Key` 在 adapter 侧挡掉**
      ——这就是"至多一次真投递"的落点。**不许为这件事给 `platform/db` 加双列 guard**（第 17 条那句"不为单个调用方改签名"照旧生效）。
    - **④ "超时/挂住那一支" v0.1 定稿 = 丙，并顺手把第 23 条③的字改对**：
      第一笔提交后 execution **留在 `RUNNING` 不动**，第二笔只密封幂等结果，HTTP 答案走已绑 503 的
      **`adapter_unresponsive`（retryable=true）**；**同步路径上不新造 `UNKNOWN` 边**（`Occasion` 只有 `Normal`/`Recovery`、
      `advance` 硬写 `Normal`，甲=改领域边集要 ADR、乙=`state_confidence` 是一列两义且该列今天没有生产写入者 —— 两条都否）。
      **但第 23 条③原文"挂住的 Adapter 一律落 `UNKNOWN`"字面与丙不合 ⇒ 本条更正它的适用范围**：
      **`UNKNOWN` 只属于"崩溃恢复"那一支** —— 即 `Occasion=Recovery` 那条既有边（`to==Unknown` 且 `Reason` 非空、非终态）。
      ⇒ 契约②那条"Adapter 崩溃 ⇒ in-flight Execution 转 `UNKNOWN`（非 `FAILED`）"的验收项，落点是**恢复扫描**，不是同步失败响应。
      **连带一个真实缺口，立项不派工**：`internal/execution` 非测试码里 **`Recovery` 没有任何生产者**（全仓非测试命中只有
      `internal/task/projection.go` 那一处，那是 task 的状态机）⇒ **REQ-RC-1**（恢复扫描器要成为 `Recovery` 的入口，
      并按第 20 条 CO-45③ 那个口径数活进程）。**排在第 8 格端到端之后、Wave-3 之前，今天不占闸门前面**；
      在此之前，崩溃注入的测试**不许**为了变绿去改 `advance` 或硬塞 `Recovery`。
      （**更新**：第 8 格端到端已随 Wave-2 于 09-28 21:4x 签字 ⇒ 本条解封，**T-391 已于 09-28 派 backend 落地**
      —— `internal/execution/recovery.go:76 ScanRecovery` + `cmd/core/recovery.go:38 runRecovery`、`main.go:255` 接线。）
      **⇒ 连带裁「分」（09-29 00:0x 主控裁，回 T-391 交回的甲/乙问：改判正身是 `INTERRUPTED` 还是 `UNKNOWN`）**
      —— **两者不是二选一，是两条边、两个场景**，矩阵里都留，扫描器按场景分叉：
      - **Core 重启那一支（启动扫描当下的场景，T-391 已接线的那支）⇒ `RUNNING→INTERRUPTED`**：
        FIRST SPRINT §7 原话是"重启 Core 后正确识别 **interrupted / recoverable** Execution"；事件
        `execution_interrupted` §6 现成（Audit 必带，`events.go:52 auditRequired` 已含）、Task 投影现成
        （`projection.go:46 EdgeInterrupted → task INTERRUPTED`）⇒ **今天就能落，撞不到 §6 墙**；
        `task/projection.go:61` 与 `projection_test.go:46` 那句"INTERRUPTED 只能由恢复扫描写入"**照旧成立**；
      - **Adapter 崩溃那一支（Core 活着、对端进程死；触发点 = Supervisor 观察到子进程死亡）⇒ `→UNKNOWN`**：
        协议 §8"崩溃后果…只影响该 Adapter 上的 in-flight Execution，**一律转 `UNKNOWN`**" + 上文
        "契约②那条…落点是恢复扫描" + §5.5 三条件（Recovery + 非空 Evidence + 非终态）+ 同事务 Audit。
        **这条的接线依赖 `Supervisor.Run` 那一格**（W2-7 点名生产码零调用点，归属 backend/process）⇒ **排在本轮之后**；
      - **REQ-RC-2 裁「甲」**：`→UNKNOWN` 的 §6 事件名**定 `execution_unknown`**，payload `from` + `reason`，Audit 必带。
        **09-30 18:0x 更正本条字面**：原写「与 `execution_interrupted` 同形」是错的 —— §6 里那一枚是 :218 的**合并行**
        （`result_code` / `error_code` / `usage`），而 `UNKNOWN` 的正身就是**没有 `result_code` 可填**；并进合并行会
        诱导出「给一次未知编一个结果码」的假数据，与契约②「收到帧 ≠ 成功」「不许 Core 替 Provider 编终态」同族。
        ⇒ §6 给它**单独一行**（`from` + `reason`；`from` 有先例 = :208 `task_resumed`，`reason` 有先例 = :183/:204/:206）。
        **盘上落点核对**（18:0x 现读）：`core/internal/execution/events.go:26` = `EventUnknown = "execution_unknown"`，
        `auditRequired` 已含它，`Terminal(Unknown) == false` ⇒ **不写 `finished_at`**（这正是「不可判定」与「已失败」在列上的区别）。**现在不登 §6** —— C8-12 是双向对拍，先登行必红；
        登记授权随落地批给出：backend 落 UNKNOWN 路径的同一提交里加常量 + 主控批 §6 行，顺序照 REQ-DB-4；
      - **Task 侧本轮不动**：`execution_unknown` 不进 §5.3 七条边 ⇒ 投影不产生 Task 事件
        （`projection.go:6-8` 那句"不在这张表里的一律不产生"照读），task 留在 `RUNNING` —— 这不是欠账是矩阵：
        §5.1 里 task `RUNNING` 的恢复列只有 `INTERRUPTED`，本就没有 `RUNNING→UNKNOWN` 这条边。
        task 自己的 `UNKNOWN` 行今天无生产者，与 `task_verifying` 同格（有意 idle，不立项）。
      - **REQ-RC-3 裁「丙」（09-30 18:0x）：v0.1 不建 `execution_leases` 的写路径，也不给「死／挂／还在跑」
        补第三态信号源 —— 三态里今天只有「进程级存活」可判，另两态**承认不可判定**（落 `UNKNOWN`，不猜）。
        三条理由，都带盘上出处：① 它要守的那个并发面在文档里**不存在** —— 「Runner」全仓只有 §24 两行与 handoff
        摘要，无定义、无 `owner_id` 生产者、无 acquire/release 协议、无 TTL 数值；② 今天任何写它的代码路径都会被
        `core/internal/platform/db/touch.go:201` 的 `narrowTables` 拒（只放 `idempotency_keys`），**那道闸是有意的**
        （扩表要过 §1.1.13 的 REQ 门），绕过它等于拆自己的门；③ 崩溃恢复的最小需求「别把在飞的活当成功」已经由
        `→UNKNOWN` + `Terminal(Unknown)==false` + 不许替 Provider 编终态覆盖，lease 只是把它做成「多 Runner 抢占」，那是 v0.2 的面。
        **连带裁**：`executions.provider_execution_id` **不是缺信号源，是缺一次落库调用** —— 值今天已经在 Core 手里
        （`turn/claimed` 经泵进 `TurnTracker.ProviderExecutionID()`，时机按契约② :189 必须是 claimed 那一刻），
        写列归 `internal/execution`（backend），**随泵的接线同批做**（它不该单独开一批去写这一列）。
        若 v0.2 真要做 lease：**先补文档（Runner 定义 + owner_id + acquire/release + TTL 数值），再走 §1.1.13 扩表门**，
        顺序反了就会得到「表建好了没人写、写了没人读」的那种僵尸面。
      - **REQ-RC-3 通道裁 (i)（09-30 19:3x，回 backend 第 304 条 ① 的甲/乙问）**：**新增一行 §6 事件 `execution_claimed`**
        （aggregate `execution`、载荷 `provider_execution_id`、Audit `–`，与 `execution_started` 同域不同事）。
        **为什么非要一个新事件**：§6 今天只有 :214 `execution_started` 带 `provider_execution_id`，而契约② 那条规则 3 明写
        「`provider_execution_id` **必须挂在 `claimed` 上**，因为 **Core 可能在 claimed-未-dispatched 这个窗口里死掉**；
        Recovery 要先拿它去问 Provider，**才允许**把这轮判成 `UNKNOWN`。少了这个字段，崩溃恢复就只能猜」；而规则 1 又明写
        「`claimed` 故意不带 `turn_id`——接下了不等于开始了，**唯一的开始证明是 `dispatched`**」⇒ **claimed 与 started 是两个时刻，
        把列推迟到 started 写就是亲手把那个窗口打开**。现读盘上这个风险是真的：泵已经把 claimed 的 id 记进内存
        （`cmd/core/turnpump.go:150 noteClaim`），**Core 一死内存就没了**。
      - **拒 (ii)（db 开一个「聚合行无事件列写」的入口）**：它要在平台层永久留一个「版本推进可以不配事件」的口子，
        而「每一次 `Touch` 都带一条事件」正是审计链与 §5.5 三条件能成立的地基（REQ-DB-2 那三个入口是**收口**、不是留缝）。
        为了少加一行 §6 去拆地基，代价不成比例；且它会让「这条列有没有审计」变成随调用方心情的事。
      - **落法与边界**：`execution_claimed` 走**三处同改**（§6 行 = 我、`core-v1.yaml` 的 `EventType` 枚举 = frontend、
        常量 + 目录 = db/execution），**与 `execution_unknown` 那批合成同一批入册**（省一轮，且 C8-12／C8-13 是双向对拍，
        半批必红）。**时机硬要求**：只在 `turn/claimed` 帧到达那一刻写，**不许**在 `dispatched` 补写、**不许**顺带改状态显示
        （running 的证明仍只认 `dispatched`，规则 2 照读）。事件名是我批的、不是 backend 自造的 —— 它若需要改这枚名字，走 REQ 报我。
    - **④-b `REQ-TURN-1`（09-27 16:5x 立项，随 CO-54=丙 那道"借来的绿"一起登记，排 Wave-2 之后、真 adapter 接入之前必须裁）**：
      **`turn/finished` 与 send 回执的先后时序在 v0.1 未定义**。今天的形状是"`start_now` 一次调用一条连接、返回即 `Close`"
      （C42 连接复用后置未裁），而 `cmd/fake-adapter` 的收尾在**排水**那一步、不在 send 那一跳内 ⇒
      **真 adapter 的收尾帧几乎必然后到**，届时"帧到了没人收"会变成常态而不是边角。
      三条出路留给那时裁：**Ⅰ** 常驻入站泵（原 CO-54 的乙/C42 那一支）；**Ⅱ** send 改成异步回执（`202 + 轮询/WS`，
      与 §15.2 现有码兼容）；**Ⅲ** 明确"v0.1 只支持同步收尾的 Provider"并把它写成 Provider 准入条件。
      **记账要点**：U13 那一格今天用 `debug/hold` 注入判据（T-386）证的是**Core 会正确落终态并投影**，
      **不证**"生产语义里帧什么时候到已定"——谁把 Wave-2 说成"投递语义已验完"，先拿本条来对。
      **⇒ 已裁「Ⅰ」（09-28 21:4x 主控裁，Wave-3 开工单第一百三十九批；三条出路里只有它同时答得了
      "帧什么时候到"与"谁在收"）：每 adapter 实例一条常驻连接 + 常驻入站泵，C42 随裁放开**
      —— 连接生命周期从 `sendpipe.go:8` 注释那句"一次 send 现起一条连接、连完就关"改为由 **Supervisor 持有**
      （起、健康探测、死亡即重连并降健康），入站泵常开、不随回执关闭。
      - **否 Ⅱ（send 改异步回执）：答非所问**。HTTP 面今天已是 202 + `accepted`，改回执形式解决不了
        "帧来了没人收"——收尾帧只存在于活着的 stdio 管道上，连接被 `Close` 之后什么回执形式都收不到；
      - **否 Ⅲ（只支持同步收尾的 Provider）：把产品挡在门外**。`start_now` 的 §5 语义就是"立即开始一轮"，
        收尾晚于回执是四家真 Provider 的常态，写成准入条件等于宣布真接入不可用，且与 §6 规则 1 直接冲突；
      - **定形**：**回执之后帧迟到是常态**。泵按 `turn_ref`→execution 归因、走现成的第二笔短事务落终态
        （`sendreap.go` 那套"只收不写 + 三态分辨"整搬进泵，`sendpipe` 里回执后那条收割腿删除）；
        回执**之前**就到的帧（`session/stop`、队列排干两腿）由同一条泵顺带收，**这两腿的行为一字不许变**；
      - **协议面连带（保强度，不是放宽）**：adapter-protocol §1「不做：…长连接复用…」按本裁**划掉"长连接复用"一项**
        （HTTP 服务发现、跨机连接仍不做）—— v0.1 的 per-send 形状本来就是欠账：§6 规则 2 的
        `since_seq` 断线补齐、§15.1 `ADAPTER_UNRESPONSIVE` 的 Supervisor 健康降级，都预设常驻连接；
      - **生产者义务（与 T-382 同批，adapter 交）**：`start_now`/`guide` 必须发 `turn/finished`，
        **它的常态位置是回执之后**；不许为赶回执把收尾挤到回执之前（那是 CO-54 已否的"即时收尾 + 改排干点"）、
        **不许 Core 在回执后替 Provider 编一个终态出来**（T-361②/T-377④ 照旧生效）；
      - **崩溃腿交汇**：连接死亡时在飞的 execution 走 `Occasion=Recovery` 的 `UNKNOWN` 边，
        入口是 **REQ-RC-1 恢复扫描器**（第 25 条④ 那句适用范围照读：同步失败响应不是它的落点）；
      - **判据（三条全过才算落地）**：(a) **迟到收尾真落**：回执已返回、连接未关、延迟 `turn/finished`
        到达 ⇒ execution 转终态 + 事件（fake-adapter 注入端点的豁免范围同 T-275；机制由实现方提，
        **不许为省这一格去弱化"不 sleep/轮询等帧"那条边界**）；(b) **反例**：把连接改回 per-send
        （回执即 `Close`）⇒ (a) 必须红；(c) **零外溢**：`SendResult` 四键与 202 语义、§15.2 映射、
        CO-25 排水点，一字不动。
      - **记账要点更新**：Wave-2 已签（09-28 21:4x），**但"投递语义已验完"这句话的生效点是判据 (a) 通过**——
        签的是验收框，不是这句；(a) 落地前，引用 Wave-2 的人必须连本条一起引。
      - **状态更新（10-01 00:5x）**：判据 **(a)(b)(c) 三条均已落地并实测** ⇒ 常驻连接 + 常驻入站泵在 `2c65177` 起进版本库，期限与排空那一半在 `48c2547` 补齐（`pumpCallDeadline = 10 * time.Second` 由 `TestPumpCallDeadlineMatchesItsFamily` 钉住，`TestPumpKeepsDrainingWhileTheWriterIsBusy` 证「写面被占住时泵仍排空入站帧」）。**所以「投递语义已验完」这句从今天起可以引用，但仍须连本条一起引**——它验的是 Core 侧的收/落/喊，**不含**任何真 Provider（四家探过、未接一家），也**不含**那条 1/3 负载敏感的重连用例（台账第一百五十五批⑤、T-416 待定性）。
      - **攒批归属（10-01 14:3x 裁，回 backend 第 (d) 问，派发 T-429）**：`turnHub.pending` 是**一张按 `execution_id` 分格、不分「代」也不分「台」的全局表**（`turnpump.go:133`，写入口 `noteOutput` :544，退场扫全表 `flushAllPending` :574–:583 —— 它 `for id := range h.pending` **不带 key 条件**）。所以我的现读比 backend 提的问题**更宽一格**：不止「两台同适配器两代连接重叠时老泵提前带走新泵批次」，而是**任何一台泵退场都会带走全仓所有在攒的批次，跨适配器 key 也一样**（单适配器测试世界里这条看不见 ⇒ 判据必须自带两台，见下）。
        **裁定：要，批次必须有主人，退场只带走自己那一条连接攒的**。理由不是整齐，是它能把内容**送进一个必然失败的落库**：新代已经把那一轮收尾落定了，老泵退场时把新代还在攒的批次踢出去，`FlushOutput` 按契约② §6 规则 8 对终态后的迟到内容**只喊不写** ⇒ 那段内容永久不在库里，而面板只看得到「批次已踢、日志喊过」。
        形状由实现方定（`pendingOutput` 记主人 / 表按 `key#gen` 分格都行），三条边界一起算落地：
        **(i)** 退场只清自己的 ⇒ 反例要有牙：**两台适配器并发攒批 + 其中一台泵退场 ⇒ 另一台的批次一行不动、随后仍照常落**；
        **(ii)** 自己的内容不许因为「只清自己的」而永久滞留（每一代的退场路径都能清掉自己那一格，无泄漏）；
        **(iii)** 零外溢：`outputBatchFrames` / `outputBatchChars` 两个阈值、`execution_output_flushed` 载荷四键与契约② §6 规则 3 的攒批语义一字不动 —— 本裁只改「谁替它落盘」，不改「什么时候落盘」。
    - **④-c `REQ-ACCEPT-1`（09-27 16:5x 立，由主控第一次亲自全量跑 Wave-2 验收检查时量出；与 REQ-TURN-1 并列，同排 Wave-2 之前必须裁）**：
      链尾那两条边**今天都没有驱动者**。现量：非测试码里除 `internal/task` 自己的常量/类型/payload 之外，
      **没有一处把任务推成 `WAITING_ACCEPTANCE`**（`grep -rn "WaitingAcceptance" core --include=*.go` 去掉 `_test.go`
      只剩常量、注册表条目、`Accept` 命令的起始态守卫 `onlyWaitingAcceptance`），而 `task_waiting_acceptance` / `task_accepted`
      **两条都在"已登记 ≠ 有生产路径"名单里**（09-27 16:3x 那次登记的第 18、24 条）。
      ⇒ 验收用例 `TestChainStopsBeforeAcceptanceNotAtAccept` 乙格今天是**先 `UPDATE tasks SET status='WAITING_ACCEPTANCE'` 再 `:accept`** 才落到 `ACCEPTED` 的——
      **这把"链跑通"偷换成了"末端那一跳跑通"**。要裁的是：**Agent 完成之后，由谁、在什么信号上宣布"待验收"**
      （候选：Core 侧在 `AGENT_COMPLETED` 落定时同笔推 `WAITING_ACCEPTANCE`；或面板/审批侧显式调用；或 §5.3 那条边本就该由 `handoff_*` 承担，而 `handoff_*` 四条今天也全在名单里）。
      **裁死之前，Wave-2 那一格不许勾。**
      ⇒ **已裁「甲」（09-28 01:2x 代主控裁，随 backend 第七十八批落地）**：由 **Core 侧在任务投影落定
      `AGENT_COMPLETED` 的同一笔事务里继续推 `WAITING_ACCEPTANCE`** —— 边已在 §5.1 矩阵（`AgentCompleted→WaitingAcceptance`），
      事件 `task_waiting_acceptance` payload 带 `execution_id`；**不新造 `:verb` 路由**（§3 归主控、且面板显式调用还要多一跳）、
      **不借 `handoff_*`**（那四条今天同样无生产者，救不了这条）。`task_verifying` 维持无生产者（v0.1 没有验证信号源），
      事件名单只从 27 里摘掉 `task_waiting_acceptance` 一条（27→26，随该批一起改名单锚句，改名单授权随本裁给出）。
      判据 = 链测试**不再手工 `UPDATE tasks SET status`**：send 之后自然落 `WAITING_ACCEPTANCE` → `:accept` 200 落
      `ACCEPTED`；错态 `:accept` 仍 409；终态重复验收仍 409 且事件不增。
    - **⑤ 判据（这一格怎么算证到）**：三条用例 —— (a) 崩在密封前 ⇒ 同 key 重试后 `messages` 仍**恰一行**、
      `executions` 仍**恰两条状态边**、幂等行被密封；"版本是当前库里那一行"**经 `GET /api/v1/tasks/{id}` 回读判**
      （09-27 更正：原写"响应 `version` 等于当前库版本"是引用了一个 `SendResult` 里不存在的键 ⇒ 按上一句改判读面）；
      (b) 双客户端同 key 并发 ⇒ 只有一行密封成功、另一条走 0 行分支**并且喊出来**；
      (c) 挂住/超时 ⇒ 库里 `RUNNING` 不变 + 503 `adapter_unresponsive` + `retryable=true` + **不多发任何事件**。
      反例两条起步：把续跑改回 `errReplay` ⇒ (a) 必须红；把密封 guard 摘掉 ⇒ (b) 必须红。



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
// 原语参数而非共享类型：db 不得 import audit（§0 依赖方向），audit 也不得 import db。
// cmd/core 一行注入：db.InitRedaction(r.DenyKeys, r.Patterns, r.Marker)
func InitRedaction(denyKeys, patterns []string, marker string) error   // 编译正则；任一失败 → Core 不进入 Ready
```

**签名修正（Q5，2026-09-19）**：本条原文写的是 `InitRedaction(r audit.RedactionRules)`，
那会要求 db import audit，与 §0 的依赖方向自相矛盾。`db` 改用三个原语参数是**正确实现，契约错了**，
故改契约不改代码。同理 db 里那份内置 `InterimRedactionRules()` 在 audit 的 `Rules()` 落地后**必须删除**（已删）。

- 强制点：`Tx.Touch` 与 `Tx.Append` 在序列化 `payload_json` / `details_json` / 任意 TEXT 列**之前**递归遍历，
  命中 `DenyKeys` 的键整值替换，字符串值再跑 `Patterns`。没有"跳过脱敏"的参数。
- **命中记录的责任划分（Q3，2026-09-19 裁定）**：`db` 只在事务内**收集**，经
  `tx.RedactionHits() []RedactionHit{Column, Match, Count}` 暴露（**只有列名、规则名、次数，绝无原值**）；
  由 **`audit` 在同一事务内自己 `Append("audit_entries", …)`**。
  理由：`audit_entries` 带 `previous_hash`/`entry_hash` 链，属 audit 的知识，db 去写它等于伪造链条。
- **命中结构必须自带类型标签（REQ-T3，11:28 裁定）**：原先 db 用 `RedactionHit{Column, Match, Count}` 单槽、
  audit 用 `{Column, Key, Pattern, Count}` 双槽，分流靠 `store_sqlite.go` 里
  `strings.CutPrefix(h.Match, "key:")` 这个**口头约定**。该串字参与 `entry_hash` 计算 —— 一旦 db 忘带或换前缀，
  审计会把"deny-key 命中"静默记成"正则命中"，**归因错位且事后无法修正（改了链就断）**。
  统一成带 kind 的一种形状，废除前缀约定：

  ```go
  type HitKind string
  const (HitKey HitKind = "key"; HitPattern HitKind = "pattern")
  type RedactionHit struct { Column string; Kind HitKind; Match string; Count int }
  ```

  属破坏性变更，按 §1.1 第 10 条走：db 与 audit 同一轮改完，`test` 的
  `TestRedactionCountersAreSealedBeforeHash` 是护栏。
  日志走同一个 redactor：日志 handler 出口处套用，不在各业务调用点各自处理。
- Secret 原值只允许以 `vault://<name>` 引用形式持久化（DPAPI abstraction，First Sprint 只留接口不实现）。
- 边界外文本（Provider 返回的自由文本里可能自带密钥）只能靠 `Patterns` 兜，属已知上限
  ——`ponytail: pattern-only for free text, entropy detection if false negatives bite`。

### 2.2.1 REQ-A10 裁定：`entry_hash` 必须由"最终落库的那串字节"算出（14:2x，P0 类）

**缺陷（audit 14:10 报，巡检只读证实同一顺序，我 14:16:51 复核）**：密封发生在脱敏**之前**。
`audit/append.go` 里 `Chain.Append` 先 `Seal(e, lastHash)` 再调 `Insert`（即 `Tx.Append("audit_entries", …)`），
而脱敏在 db 内部的 `Tx.scrubArgs` 里跑。于是：哈希算的是 `{"note":"token=ghp_AAAA…"}`，
落库存的是 `{"note":"token=[REDACTED]"}` —— **这一行的哈希从此重算不出来**，
`VerifyChain` 从那行起往后永久 `UNVERIFIED`。方向是 fail-closed（不会误报"通过"），
但后果不可接受：**一次完全诚实的写入就能永久致盲审计面板，而且症状长得像篡改**；
Wave-2 之后没有重建窗口（回填要重算 `entry_hash` = 伪造链，T-28② 已判死）。

**不变量（今后写在契约里，不再靠实现顺序碰运气）**：
> **参与 `entry_hash` 的字节，必须与写进库的字节逐字节相同。**任何"哈希之后再改值"的路径都是缺陷。

**修法三件，缺一不可**（都在既有边界内，**不新增抽象层**）：
1. **db 导出唯一那一份脱敏实现，且**必须把命中记录一起交出来**（14:5x 二次定稿，REQ-A11）**：
   ```go
   func Scrub(table string, cols []string, args []any) ([]any, []RedactionHit, error)
   ```
   **为什么签名里必须有 hits**（这是 db 14:51 报的、我 14:57:40 读码证实）：现存的
   `redact.go:194` 是 `return scrubArgsInto(newHits(), table, cols, args)` —— **一个现成的命中收集器被
   当场丢弃**。audit 改成 Scrub→Seal→Insert 之后，密封前那次拿不到归因，而 `Tx.Append` 里那次二扫
   **只会重新记上 key 命中（键驱动，键还在），pattern 命中永久丢失（值驱动，秘密已被换成 marker）**。
   这些计数要进 `details_json` 并被 `entry_hash` 覆盖 ⇒ **"字节对了但归因静默缩水"**，
   正是本条要防的那类"看起来正常"的失效。
   **配套的第二件（同一次改完，别留半截）**：对 **`audit_entries`** 这张表，`Tx.Append` 内部的二次 scrub
   **不得再累计 hits**（归因由密封前那次负责）。否则 key 命中会被记两遍 —— 同一个洞的相反方向。
   实现任选（Append 内对该表传一个丢弃型收集器即可），**但必须有用例钉住两侧**：
   ① 一次含密钥形状的写入 → `details_json` 里 pattern 与 key 两类命中都在；
   ② 同一行的 key 命中**计数为 1 而不是 2**；③ `TestScrubIsByteStable` 那条幂等性质继续成立。
   **规则仍归 audit（§2.1），执行仍只有 db 这一份代码**——**不许出现第二个脱敏器**。
2. **`audit.Chain` 的 `Append` 顺序改成 Scrub → Seal → Insert**。
   注入方式**沿用该文件已有的形状**（`Chain` 已经是 `Head func(...)` / `Now func()` / `Insert` 的注入体），
   加一个 `Scrub` 函数字段即可；**`internal/audit` 不 import `platform/db`**（§0 依赖方向不变），
   函数值由 `cmd/core` 装配时注入 —— 与"接口归 audit、装配归 cmd/core"同一条路。
3. **db 内部 `Tx.Append` 那次 scrub 保持不动**（纵深防御），但必须加一条**常驻不变量用例**：
   **同一批字节二次脱敏后不得改变一个字节**。这条是 ①② 的粘合剂 —— 没有它，
   "两处各自脱敏一次"迟早又漂回同一个缺陷，只是这次变成静默。

**被否掉的方案（记下来免得有人再试）**：`Chain.Append` 在运行时拒绝未脱敏输入（audit 自己先写了、随即自撤）——
它把 audit 变成第二个执行点（踩 §2.1 的分工），并与 audit 自己的 `TestStoreRedactsSecretsBeforeTheyLand`
直接矛盾；那个用例断言的正是"落库前已脱敏"，运行时拒绝等于把责任推给调用方。

**验收下限（归 `test`/audit）**：写一行含密钥形状的 audit 记录 → 之后 `VerifyChain(整条链, head)` **必须仍然通过**；
以及第 3 条的"二次脱敏不变"用例。audit 已在 `chain_health_test.go` 留一条 `t.Skip` 等这件事落地后删。

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
5. **`Resolution.By` 取自已过门的认证主体，永不取请求体**（10-01 14:3x 裁，落 audit 第 (2) 项那条硬发现）：
   v0.1 的"认证"只有一道门 —— `core/api/http/handler.go:235` 要求所有写操作带 `X-Lucero-Principal: local-user`，
   所以**已认证 principal 的唯一来源就是那个过门 header**。`By` 一旦从请求体取，调用方就能自报"是谁批准的"，
   而它是三样东西的身份：`approvals.resolved_by` 列（`store_sqlite.go:220`）、`approval_resolved` 事件的
   `PrincipalID`（:225）、审计链条目的行为人（:240）⇒ 伪造它等于同时伪造这三样。
   `validateResolution` 现在只查非空（`approval.go:362`），那一格**不收紧到域层**（域层拿不到 header，
   硬要它区分就会去猜），改法是 transport 只准这样填 + 判据盯住：见 §3.0-c。
   连带禁令：`approvals.go:130` 那个 `resolved_by` **请求体字段是死路** —— 要么删掉，要么显式拒绝，
   不许"读了但不用"（那会留一条"传了似乎就生效"的错觉）。
6. **一条审批记录里有两个身份，各有其位，不许互换**（同批裁）：
   **决议人** = `Resolution.By`（上面那条）；**受益人/被批准的主体** = `request_json.principal_id`
   （`approval.go:317` 的 `grantFor` 取的就是它，这是**对的**，不是"两套身份系统并存"的缺陷 ——
   临时授权是给"要执行那个动作的主体"开的，给批准人自己开一张条等于批准即自授）。
   `temporary_grants.principal_id` 必须来自 `RequesterPrincipal()`，**不许**改成 `res.By`；
   `request_json` 里没有 `principal_id` 时 `RequesterPrincipal()` 明确报错（`approval.go:183`–`:186`），这条不许放宽。

**§3.0-c 接线现状（10-01 14:3x 现读，不是我推断的措辞，是五个 `grep` 的读数）**
上面六条规则今天**没有任何一条被生产码走到过**。逐个点名（口径：非测试文件、并排除定义它自己的那个包）：

| 东西 | 定义位 | 非测试调用点 |
|---|---|---|
| `permission.Engine.Decide` | `internal/permission/permission.go:195` | **0** |
| `permission.Engine.CheckTemporaryGrant` | `permission.go:269` | **0**（排除本包后 `grep` 无输出） |
| `approval.Service.Request` | `internal/approval/approval.go:241` | **0** |
| `approval.Service.Resolve` | `approval.go:279` | **1**（10-01 15:2x 现读：`core/api/http/approval_resolve.go:117`，backend T-426 ① 交的那条 `:resolve` 路由；**本行原写 0，那是 14:3x 那棵树的读数，留着不删**） |
| `execution.ApprovalStates.BlockingState` | `internal/execution/approval.go:8` | **0**，且**没有装配位**：现读 `grep -rn ApprovalStates --include=*.go core/` 只命中定义它自己的那两行（`:5` 注释、`:8` 类型），`cmd/core/main.go` 零引用。注：类型位在 `:8`、方法位在 `:9`（**这句以前塞在第二格里，那两个反引号让判据解不出唯一文件 —— 现在移到这一格来写**） |
| — **本列的格式规矩（10-01 15:5x，backend 报回、我 15:55 现读原文核对成立）** — | 「定义位」那一格**只准放一个反引号对**（``包/文件.go:行``），因为 `core/test/wiring_surface_test.go:68` 只做 `Trim(cells[2], "`")` 再去定位：**两个反引号对 ⇒ 判据 `t.Fatalf`「定位不到唯一文件」，那一格即使被装配完也不会转绿**。补充说明请写到第三格（判据只读前两座）。**这条不是挑剔排版：是我把合同写成数据源之后必然要付的口径 —— 数据源就得按数据源的样子写** | |
| — **更正** — | 本行原写 `ApprovalGate` | **那个名字是我编的**：我从没读过 `:5`/`:8`，是照概念造的词（同族旧错：09-27 的 `harvestTurnFinished`）。盘上真名 **`ApprovalStates`**（现读附在上一条命令里）。结论一字未变——**两种写法都是 0 个非测试调用者**，改的只是名字 |


**机械判据已上线（10-01 15:2x，test 交 T-431）**：上面那五格的「非测试调用点」从今天起不再靠我手数 ——
`core/test/wiring_surface_test.go` 的 `TestWiringSurfaceHasCallSites` **只以本节那张表为输入**（读不到表或解析出零行就 `t.Fatalf` 停，
所以它不会变成永真式），并把**「符号在盘上不存在」与「存在但零调用点」两种红分开报**。
⇒ **表是它的数据源、不是它的快照**（我 09-2x 立的那条「validator 不许内嵌契约」在这里的反向应用：宁可让判据读契约，也不在测试文件里再抄一份清单）。
**以后谁改了调用点，就改这张表；判据自己跟着变。** 本笔入册时它在 HEAD 上报 **4 枚红**（`Decide`／`CheckTemporaryGrant`／`Service.Request`／`ApprovalStates.BlockingState`），
**每一枚都有归属（全在 T-430 那四格里）**，另 **1 枚已绿**（`Service.Resolve`）⇒ **这 4 枚红是为 T-430 立的闸门，不许为了"全绿"摘判据、也不许把表改成"已知为零"来喂它**。

判定：这不是"安全没做"，是**审批链整条没接线**（**这句在 14:3x 那棵树上是全真；15:2x 起 `Service.Resolve` 已有生产调用者，其余四条仍是 0 —— 上面那格留着两个读数，不静默替换**） —— 域层、存储层、事件、审计都建好了并且有测试，缺的是
「谁在什么动作上问一次 `Decide`」和「面板按了批准之后走哪条 HTTP 口子」。所以：
- **路由面**（`POST /api/v1/approvals/{id}:resolve` + `By` 从 header 填）**已经在途**：backend 10-01 14:0x 起的
  `core/api/http/approval_resolve.go`（`:56 by := r.Header.Get("X-Lucero-Principal")`、`:121 By: by`）——
  本条规则 5 是**据它落的**，不是给它下的命令；它已交账前我不判它对错，只把口径钉住。
- **发起面**（执行前问一次 `Decide`、高风险动作先落审批行、`ApprovalStates` 那个死接口要么装配要么删掉）
  归 `backend`，**派发 T-430** —— 它才是让名单里 `execution_approval_requested` / `execution_approval_resolved`
  两条离开的大件；与 `execution_output_flushed` 那次同一个规矩：**先补了功能才摘**。
- `permission`/`approval` 两个包**不新增写口**（归 `audit`，它这次交的是发现不是改动，正确）。
- 本表现由 `test` 角色加机械判据盯住（**T-431**）：不许再出现"接口声明了、没人构造、没人调、也没人红"。


7. **v0.1 的"谁能批准"只有一个答案，而且它不是权限模型**（10-01 15:5x 裁，回 audit 那句"今天没有任何权限判定"）：
   能落决议的调用 = **过了 `handler.go:235` 那道门的那个已认证主体**（唯一可接受的字面量 `local-user`）。
   明写成人话：**v0.1 是本地单用户，所以"谁能批"今天不由策略决定、由"能不能连上这台 Core"决定**。
   三条连带要求一起成立，别把这句读成"权限已经做完了"：
   ① `permission.Engine.Decide` 判的是**这个动作要不要人来批**，不是**这个人有没有资格批**——两件事永远不许混进同一个函数；
   ② 真做多用户时，资格判定必须新增在 `permission`（它的包），而 **`By` 仍然只取过门的 header**（规则 5 不因扩容而放宽）；
   ③ 面板不许自己藏"只有我能批"这类逻辑（§7：transport 是业务 Service 的薄投影，不得含业务判断）。
8. **"重复决议"有两种，必须给出两个不同答案**（10-01 15:5x 裁，回 audit 那句"`resolved_by` 从哪来/重复怎么回"）：
   - **同一份意图再发一次 ⇒ 幂等成功**：走 backend 已经接好的那条腿（`approval_resolve.go:104`–`:109` 的 idempotency 认领，
     冲突时 409 `idempotency_replay`；`Service.Resolve` 对已决行返回 `nil`，`:291`–`:292`）。这一支**照旧成立，不用改**。
   - **同一个 `id` 第二次给的是不同的决议**（第一次 `APPROVED`、第二次 `REJECTED`，或反过来）⇒ **必须 409，且不许静默成功**。
     今天它是 `nil` ⇒ 被翻成 200，也就是**"你以为改判了，其实什么都没发生"**。
     修法不许由 transport 猜：`internal/approval` 必须**给出一个有名字的答案**（哨兵错误或"已有决议"返回值均可），
     handler 才按 `writeResolveError`（`approval_resolve.go:187`–`:191`）那句自己的规矩翻成 409——
     **它明写了"不拿文案当合同，该回 409 的那几支要有名字"**，这条要求由它的注释产生，不是我发明的。
     ⇒ 派发 **T-438**（`internal/approval` 归 audit），backend 的 409 分支等那个名字，不等别人。

**§3.0-d 落 `approval.Draft` 时那三个键的口径（10-01 15:5x 裁，回 backend 第 ④(a) 问）**
backend 的原话是"不给口径就提 REQ、不跨包定语义"，它说得对，所以这里给死：

- **`Type`：v0.1 固定填 `"PERMISSION"`。** 依据是盘上读数，不是我的偏好：`approvals.approval_type` 是
  `TEXT NOT NULL` **且没有 CHECK**（`core/migrations/000001_core_schema.sql:407`）⇒ 词表既不在库里也不在契约里，
  而 v0.1 只有**一个**来源会造审批行（执行前的权限判定要求人来批）。**第二个来源出现那天再谈词表与迁移**（改列 CHECK 是重建表，归 db、`000001` 冻结）。
- **`RiskLevel`：必须从 `permission` 那份内置分类拿，transport 与 backend 都不许自己抄一份。**
  那份清单是 `highRiskActions`（`internal/permission/permission.go:131`）加 classifier（`:143`–`:145`，命中即 `RequireApproval` + `ReasonDefaultHighRisk`），
  今天**都是未导出的** ⇒ 需要 audit 开一个导出入口（并入 **T-438**）。
  **为什么不许 backend 抄一份**：同一张词表存两处 = 我 09-2x 裁过的那枚反面（"两包各存一份词表值 ⇒ 由观察方导出单一来源"）；
  而且 §3.1 那句"风险等级来自固定的内置分类，不来自策略表"里的**"内置"只有一个归属**，抄一份就有第二种"内置"。
  拿不到入口之前，`Draft.RiskLevel` **宁可留空**——`Service.Request` 会把空串写成 `"UNKNOWN"`（`approval.go:261`–`:263`，
  它自己的注释是"never claim a risk level we were not told"），而 `approvals.risk_level` 的 CHECK 五值里**含 `UNKNOWN`**（`000001` :408–`:410`）⇒
  留空是**诚实**，编值是**造假**。
- **`ExpiresAt`：必填，不许 0。** `approvals.expires_at` 是 nullable（`000001` :418），而 `SweepExpired` 靠它把 `PENDING` 翻成 `EXPIRED`
  并发 `approval_expired`（`store_sqlite.go:341`）⇒ 一个空的期限就是**一条永远不会过期、永远堵着那条执行的锁**。
  同族我先例是 `temporary_grants.expires_at`（nullable、0 被读成"永不过期"），那次我裁"写口必须自带窗口检查"（`grantWindow`，`approval.go:345`）。
  **尺寸由我定**：v0.1 取 **15 分钟**（`900000` ms）。这是**裁定不是发现**——盘上今天没有任何既有期限值可引（`MaxGrantDuration = 8h` 只管临时授权，`approval.go:51`），
  所以它归用户改：用户若要别的值，改这一行即可，`T-438`/`T-439` 都不动结构。



### 3.1 无规则命中时的默认（REQ-A4⑤ 裁定，17:55）

空 `policy_rules` 不等于"全部要求审批"，也不等于"全部放行"。默认**由动作风险等级决定**，而风险等级
来自固定的内置分类，不来自策略表（策略表为空时它仍然成立）：

| 情形 | 默认 | `reason` |
|---|---|---|
| 动作在内置**高风险集**：`filesystem.write`、`command.exec`、`db.write`、`deploy`、`remote.*`、`secret.read`、`adapter.debug.*` | `REQUIRE_APPROVAL` | `DEFAULT_HIGH_RISK` |
| 动作风险为 `UNKNOWN` / 未登记 / 分类器不认识 | `REQUIRE_APPROVAL`（**不确定就失败关闭**） | `DEFAULT_UNKNOWN_RISK` |
| 其余已登记为低/中风险的动作（含 `session.list`、`session.read`、`session.send`、`model.select`、`execution_started`） | `ALLOW` | `DEFAULT_ALLOW` |

三条硬要求：

1. 默认值**必须出现在决策输出里**（`Effect` + `Reason`），不得静默放行 —— 看不见的 fail-open 比看得见的
   fail-closed 危险得多，也是"重复执行高风险动作"这条 P0 的温床。`Decide` 的返回改成
   `(Effect, Reason string, error)`，`Reason` 取上表值或命中规则 id。
2. **优先级链不变**：紧急停止 → 显式 DENY → Trust Domain → Project → Task Mode → Session Override
   → Temporary Grant → **本节默认**。也就是说一旦任何一层有规则，默认就不起作用。
3. `execution_started` 属于低风险 ⇒ Wave-2 的垂直链**不会**在起跑前被审批卡住；
   链上那次"请求审批"必须是 FakeAdapter 主动要求的一次真实高危动作审批，
   而不是默认兜底造出来的 —— 否则 §7 验收路径测的是兜底逻辑，不是审批链路。


## 4. 跨模块读

| 需求 | 走法 |
|---|---|
| backend 想知道某 task 的 sessions | `task` 包不暴露 link 表；由 `session.Service.ForTask(ctx, taskID)` 提供 |
| frontend/远程端要任何数据 | 只走 REST/WS，禁止 import Go 类型（契约③会钉死） |
| audit 要事件 | 直接读 `audit_entries`；`event_journal` 与 `audit_entries` **不互相外键、不共用行** |
| **C1（REQ-T22 裁定，10:2x）**：要 JOIN 别人表的语句该放哪 | **放消费方自己的包里**（谁要这份数据，语句就住在谁的仓库文件里），与 `session.Service.ForTask` 同一形状。**跨模块"读"不设归属限制、也不升成判据**——裸 SQL 的 `SELECT` 今天不红是**设计**（本节允许跨模块读与 JOIN），不是漏判；**只有写才看归属**（契约③ §7 矩阵 + W13 白名单）。**代价量出来了**：非 `platform/db` 的生产码里 `FROM`/`JOIN` + 真表名的形状共 **42 行**，里面"归属包读自己的表"与"跨模块读"混在一起 ⇒ 收它不是加一条正则，是**替 locality 换一套语义**；要换请先开 REQ 改本节，别在脚本里收紧（`test` 已把"现状 = read"写成自测断言，将来谁不改契约就把它改严，自测会红）。**唯一写死的例外仍是 `approvals`**（§1.2.1 第 1 条：那张表的 SQL 只准出现在 `internal/approval`）——那是裁决数据，不是普通读。 |

## 5. 待补（下一批契约）

- 契约②：Adapter JSON-RPC 握手与 capability 声明粒度（输入：Phase 0 已发现 Codex 有
  379 文件的 v2 协议 schema；**不自造 capability 枚举**，见账本上行输入）。
- 契约③：REST/WS 资源形状 + 文件所有权矩阵。
- 契约①批准后需要落的第一件事：`000002` 迁移草案（仅 ADR-017 的 9 张表加 `version` 列）。

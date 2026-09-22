OWNER_ROLE: db
TASK_ID: db-platform
STATUS: complete
SCOPE: Core 的 DB 平台层唯一写路径（连接与 PRAGMA 分层、串行写通道、事务原语、迁移器、Event Journal 写入、强制脱敏）；不含业务语义
EVIDENCE_DIR: .qoder/evidence/
LAST_UPDATED: 2026-09-23T00:02+08:00

## Objective

交付 `core/internal/platform/db` 与 `core/internal/eventjournal`，使 scoped 验收命令
`CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test ./internal/platform/... ./internal/eventjournal/...` 全绿，
并覆盖主控给出的 6 项验收。

## Completion criteria

- [x] 空库依次应用 `000001` → `000002` → `000003` 均成功（13 张聚合表的 `version` 列全部实测到位）
- [x] `Open` 的 PRAGMA 分层与 STRICT/FTS5 能力有测试断言
- [x] `RETURNING version` 可用且递增；并发 48 次改同一聚合不产生重复版本
- [x] 绕过 `Touch` 直插重复 `(aggregate_type, aggregate_id, aggregate_version)` 被唯一约束拒绝
- [x] 迁移器重跑不报错、不重复记账；半途失败后 `PRAGMA integrity_check` 为 ok 且脏表不存在
- [x] deny-key 命中后原值不出现在 `payload_json`；正则编译失败与未初始化都返回 error（fail-closed）
- [x] **验收口径已由主控更正为 scoped**：`CGO_ENABLED=0 go test ./internal/platform/... ./internal/eventjournal/...` 全绿；
  全量 `./...` 只作信息记录，其失败不算本角色验收失败（见 Verification evidence 的 INFO 段）
- [x] **T-16 新规则（契约 §1.1 第 10 条）已并入本角色收尾步骤**：动注册表（`AggregateKind`/`aggTable`）
  当轮必跑 `go test ./internal/...` 全仓内部 + grep 新晋聚合表的写点，受影响方同轮写进 Coordination，
  不许只报"我这边绿"。已补做，结果见 Verification evidence 的 T-16 段。

## Scope boundaries

- In scope: `core/internal/platform/**`、`core/internal/eventjournal/**`、`core/migrations/*.sql`（`000001` 只读）、本文件
- Out of scope: `core/go.mod`（主控）、`approvals` 表任何 SQL（audit）、业务包 Service/Repository、cmd/core 接线、git 写操作

## Current checkpoint

- Stage: T-149 ② 已落（③b 三种非法形状各自报错，取值逻辑未动），T-142 已闭单
- Status: complete
- Last verified action: `go vet` VET_OK；scoped 门禁 → 2 包 ok；全仓 `go test ./internal/... -count=1`
  → **13 个包 ok、FAIL 行 0**；变异标记 0 命中；基线现取 `82f01f6`，
  `git diff --stat HEAD` 本角色范围**只有 `contract_parity_test.go` (+36/−24)**，
  `events.go`、`platform/db/`、`core/migrations/` 均不在 diff 内 = 与基线逐字节相同
- Next action: 无待办；账上无未闭合项
- Verification: `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test ./internal/platform/... ./internal/eventjournal/... -count=1`

## Completed

- **T-149 ② 已落（③b 混排必须报错）**：`eventTypesIn` 现在只认 §6 的两种合法形状
  —— **全部写全名** 或 **一个全名打头 + 若干 `_xxx`**；三种非法形状各自报错：
  ① 全名之前出现简写（孤儿，如 `_handled / _read`）；② 已有 ≥2 个全名后出现简写
  （主控给的假想例 `a_b / c_d / _e`，旧实现会**静默展开成 `c_e`**）；③ 简写之后又出现全名
  （`notification_created / _handled / task_queued`）。
  **取值逻辑按令未改**（`default:` 分支仍取"最近一个全名"的词干）。
  - `TestEventTypesInExpansion` 加了合成反例（含 `a_b / c_d / _e` 与 `task_created / grant_issued / _failed`），
    合法侧同步加了 `execution_paused / execution_resumed`（两个全名并列，必须**不**报错）。
  - **变异按主控说的做了并给出对照**：把守卫退回 ③b 之前的 `if stem == ""` →
    两条混排合成例报 `returned no error`（用例红），**而同一次跑里 `TestCatalogMatchesContract` 仍 `ok`**
    ⇒ 与主控那句完全一致：对拍看不见解析器少收/多收，只有直接喂合成反例的那条能看见。
  - **真表零误报**：收紧后 §6 实际仍解析出 62 个类型、双向差集为空 ⇒ 现有合并行没有一条踩 ③b。
  - **我自己的测试当场抓出一个我新引入的洞**：第一版把守卫写成 `fulls != 1`，
    漏了"全名存在但没有词干"的情形（`x / _read` 会静默展开成裸 `read`）—— 是反例跑红暴露的，
    已改成 `fulls != 1 || stem == ""`。另修一条断言措辞（原来只说 orphan，现在覆盖 ③b 全体）。
  - scratch 备份 `.scratch/db/contract_parity_t149_before.go`，还原后 `grep T149MUT` 0 命中，
    文件与目录都删净；`/tmp` 零接触。
  - **T-142 闭单已收到**（巡检 23:44 / 主控 23:50 各自读码确认 `return nil, fmt.Errorf` 在位），无异议。
- **T-142 已修（主控指出的真错，展开约定第 ⑤ 条）**：`eventTypesIn` 原先在 `stem == ""` 时 `continue`，
  孤儿简写被**静默丢掉**。现按契约 §6 的展开约定改成报错
  （`row ... starts with the shorthand "_handled" before any full name; the event contract §6 table or
  this parser has lost its shape`），且 `contractCatalog` 用 `t.Errorf` **逐行报、不早退、继续跑完差集**。
  ①~④ 与约定逐字对齐（词干 = 最后一个 `_` 及其之前；无 `_` 前导的 token 原样收下），未改其行为。
  新增 `TestEventTypesInExpansion` **两侧都测**：5 种合法行形状必须**不报错**（含
  `` `notification_created` / `_handled` / `_read` ``、整行写全的 `grant_*`、单名行、
  两个全名并列的 `execution_paused` / `execution_resumed`），2 种畸形行必须报错。
  - **把 ⑤ 改回静默跳过去做变异，测出一条比"我的用例会红"更值钱的结论**：
    ```
    --- FAIL: TestEventTypesInExpansion  （两条 orphan 各报 returned no error）
    --- PASS: TestCatalogMatchesContract  contract §6 and the catalogue agree on 62 event types
    ```
    ⇒ **对拍自己确实会绿着骗过去**，主控说的后果是实打实的；只有 parity 不够，
    必须另有一条直接打在解析器上的用例——这条我此前确实没有。
  - 还原后 `grep T142MUT` 0 命中。备份落在 `.scratch/db/contract_parity_test.go.t142`（绝对路径），
    用完连文件带目录删净，`/tmp` 零接触；**scratch 文件名从此带角色+编号**，
    因为上一轮我几乎把两个 mutant 存成同名互相覆盖。
  - 三处自纠：删上个 sink 时顺手删掉的 `fmt` import 让本轮第一次编译就失败（实测逼出来的，不是想出来的）；
    头两版"畸形行"反例我误把**合法**形状当成畸形来断言，也是跑出来才发现。
- **T-125 采纳：REQ-A2 那条"同口径"前提不成立，未改任何代码**。主控逐字读的结果我复核认同：
  `platform/db` 里**没有 `details_json` 字面量**（非测试码零命中；`--include=*.go` 会连 `_test.go` 一起算，
  我这边的 5 处命中全在 `attribution_test.go` 与 `scrub_test.go` 两条 §2.2.1 用例里——
  **巡检补的那条口径提醒成立，我复采时也看到 5 而非 0**，结论不变）；
  主控指的分支在 `internal/audit/append.go` 的一排纯列映射，无豁免语义；
  db 的 `vault://` 判断在 `redact.go` 且是**值级**。两类东西确实没有口径要对齐。
  **收下同一课的第二遍**：否定性结论必须标口径（排除测试与否）并贴原文，不能只贴计数。
- **T-136 重复投递（23:00 再收到一次）**：本条已在 23:05 那一轮交付完，本轮**未改任何代码**，
  只复采确认现状：`events.go:60` 有 `NotificationRead = "notification_read"`、`:94` 的 `true` 集合含它、
  `TestNotificationReadIsUsable` 与 `TestCatalogMatchesContract` 均 PASS。
  巡检 22:58:46 那次实测（"只有 Created/Handled"）是我落笔**之前**的盘，不是没做。
  **两条顺带查出来的、会影响别人判定的事实**：
  ① **给 `test`（C8-12 判据）**：事件契约里字面量 `notification_read` 只出现 **1 次，且那 1 次是第 214 行的说明文字**，
     §6 表格本体（第 207 行）写的是简写 `` `notification_created` / `_handled` / `_read` ``。
     ⇒ 若 C8-12 用字面名去 grep §6，会把这条**已登记**的类型判成未登记（假红）；解析器必须展开 `_x` 简写
     （我这边的 `eventTypesIn` 已处理，写法可参考 `contract_parity_test.go`）。这与今天
     `不再累计 / 不得再累计` 那次是同一种"口径没标就误判"。
  ② **给 backend/主控（别让这一行被当成端点已通）**：`core/api/http/notifications.go:57` 仍按"依赖没注入就 501"，
     且 `notifications_test.go:253` 正**钉着** `:read`/`:handle` 返回 501。所以事件面已备好，
     但 `POST /notifications/{id}:read` 不会因我这行自动转绿——还差 backend 注服务 + 翻那条断言。
     `grep -rn "NotificationRead\|notification_read" --include=*.go core/`（排除 `internal/eventjournal`）当前**零命中**，与此一致。
- **T-136 / REQ-DB-5 原交付（23:05 那轮，无迁移，`core/migrations/` 与基线零 diff）**：
  ① `eventjournal` 补 `NotificationRead = "notification_read"` 常量并写进 catalog map（`Registered()` 的真身）。
  ② 新增 `contract_parity_test.go`：**现场解析**事件契约 §6 表格（含 `x_y / _z` 简写展开），
     与 catalog 做**双向差集**，两个方向各钉一次；另有一条"解析到 <40 种即报错"的守门，
     防止解析器与文档漂移后这测试悄悄变成空过。**不在测试里存契约名单副本**——存了就会继续绿着漏。
  **这条用例第一次跑就抓到 5 个真实缺口**：契约 §6 第 191–196 行登记了
  `task_paused` / `task_blocked` / `task_resumed` / `execution_paused` / `execution_resumed`，
  而 catalog 里没有——与 T-7② 那两个 `adapter_*` 完全同型（backend 一旦暂停/阻塞任务就会撞墙）。
  已一并补齐（常量 + map 各 5 条）。现在两侧一致，用例自己报数：
  `contract §6 and the catalogue agree on 62 event types`，常量数 `grep` 复核 = **62**。
  变异验证两侧各打一次：删 `NotificationRead: true` → 第一方向红（首轮已实测）；
  塞一个 `notification_bogus` 进 map → `the catalogue registers [notification_bogus] which event contract §6 never registered`。
  还原后 `grep notification_bogus` **0 命中**，`.scratch/db/` 连目录清掉，`/tmp` 零接触。
- **T-115 / REQ-A11 已交付（两侧一次改完，不留半截）**：
  1. `Scrub` 现按契约定稿返回 **`([]any, []RedactionHit, error)`** —— 收集器不再被丢弃，
     audit 在密封前就拿到归因。实现仍是把 `Tx.scrubArgs` 与 `Scrub` 共同委托到
     `scrubArgsInto`，**全项目只有这一份脱敏代码**（§2.1 分工未破）。
  2. **对 `audit_entries` 这张表，`Tx.Append` 内的二次 scrub 换成丢弃型收集器**
     （`touch.go` 里按表名分流，注释点明归因归密封前那次）。非 `audit_entries` 的表照旧在事务内累计，
     已由 `TestOtherTablesStillAccumulateHits` 反向钉住，防止我把豁免做宽。
  3. 三条用例落地（§2.2.1 ①②③）：`TestAuditEntryAttributionIsCountedOnce` —— 预 Scrub 拿到
     pattern+key 两类命中、`Tx.Append` 内二扫**返回 0 条命中**、合并后 key 计数恰为 1、
     落库 `details_json` 实测 `{"note":"the header was [REDACTED]","token":"[REDACTED]"}` 零明文；
     `TestScrubIsByteStable` 幂等性质继续成立并新增"命中含 key 与 pattern 各至少一条"；
     `TestScrubFailsClosedAndValidates` 守 fail-closed 与列/参数数校验。
  - **两个变异各打各的层**：① 把分流改回 `sink := tx.hits` →
    `the re-scrub inside Tx.Append accumulated [{audit_entries.details_json key token 1}]; audit_entries must not double-count`
    （正是"key 记两遍"那一侧）；② 把 `Scrub` 退回 `return out, nil, nil` →
    `Scrub returned hits []; want at least one "key" and one "pattern"`（"归因静默缩水"那一侧）。
    还原后 `grep T115MUTA\|T115MUTB` **0 命中**，`.scratch/db/` 连目录清掉。
  - **一致性隐患按令未动**：`walk` 的 `vault://` 豁免与 `redactValue` 的 `case string` 提前分支同口径那族，
    主控记为待裁、要求别顺手改 → 本轮一行未碰。
  - **契约不需要补文**：巡检 15:05 说"② 那一半契约零命中"，它 15:15 已自纠（搜的是 `不再累计`、
    契约写的是 `不得再累计`）。我 15:22 自己读了第 341 行原文确认：
    「**不得再累计 hits**（归因由密封前那次负责）。否则 key 命中会被记两遍 —— 同一个洞的相反方向。」
    ⇒ 主控不欠这条文档，我也无需再提。**我这边收下同一课**：下"零命中"结论前先把同义写法都扫一遍，
    并贴原文而不是只贴计数——我这几轮报别人的"零命中/未被引用"都只给计数，形状一样。
  - **对齐取证（重要，因为签名改动可能打断并发中的装配）**：HEAD 里 `cmd/core` **没有** `db.Scrub` 调用；
    工作树里 `cmd/core/rejections.go` 与 `internal/audit/append_test.go` 已按**三返回值**形式在用
    （前者 `out, hits, err := db.Scrub(...)`，后者注入同形假函数）。
    改完 `go build ./...` → **exit 0**，`go test ./internal/... `→ **12 包全 ok**，
    即 `db` / `audit` / `cmd core` 三处对新签名已一致。中间那段是否红过我不判定（无法回溯），
    只报此刻的实测。
- **T-107 / REQ-A10 已交付本角色的那两件**（第 2 件 `audit.Chain` 的 Scrub→Seal→Insert 在 audit 包里，未碰）：
  1. **导出唯一一份脱敏实现的入口**：`db.Scrub(table string, cols []string, args []any) ([]any, error)`。
     做法是把原 `Tx.scrubArgs` 的函数体抽成 `scrubArgsInto(h *hits, …)`，`Tx.scrubArgs` 与 `Scrub`
     各一行委托过去 → **全项目仍只有一个脱敏器**（§2.1 分工没破）。
     `Tx.Append` 内部那次 scrub **原样保留**当纵深防御，行为未改。
     改前实测确认主控那句：`grep -h "^func [A-Z]" redact.go` 只有 `InitRedaction`（入参校验，不是脱敏入口）。
  2. **常驻不变量用例** `TestScrubIsByteStable`：同一批字节二次 `Scrub` 必须逐字节相同，打在 `Scrub`
     返回值上；覆盖 map 载荷、预序列化 JSON 串、含 Bearer/sk 的自由文本、干净 `vault://` 引用、
     `int64` 与纯文本直传，并额外比 `reflect.TypeOf` 防"类型偷偷变了"。
     配套 `TestScrubFailsClosedAndValidates`：未 `InitRedaction` 时报错、列数与参数数不符时报错。
  - **变异验证**（把 `encode` 输出包一层 `[...]` 制造非幂等）→ 用例精确打出两遍的差异
    `pass1=[{"api_key":…}]` vs `pass2=[[{"api_key":…}]]` → FAIL；还原后 `grep T107MUTANT` **0 命中**、
    用例转绿，`git status` 本角色范围只剩 `M redact.go`、`M touch.go`、`?? scrub_test.go` 三个应有的改动。
  - **⚠ 探针实测出一条要让 audit/test 与主控知道的后果**（探针文件已按精确名删除并证零残留）：
    `Scrub` 按契约建议签名**不返回命中记录**，而 pattern 命中是"值驱动"、deny-key 命中是"键驱动"，
    所以 audit 采用 Scrub→Seal→Insert 之后，`Tx.Append` 里那次再扫一遍**只会重新记上 key 命中，
    pattern 命中永久丢失**：
    ```
    不预 Scrub : [{Kind:pattern Match:(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,} Count:1} {Kind:key Match:token Count:1}]
    预 Scrub 后: [{Kind:key Match:token Count:1}]
    ```
    这些计数要进 `audit_entries.details_json` 并被 `entry_hash` 覆盖 —— 属于 REQ-A10 同类缺陷的另一面
    （字节对了，归因计数静默缩水）。**最小解**：`Scrub` 多返回一份命中
    （`func Scrub(...) ([]any, []RedactionHit, error)`），audit 合并两份即可，我这侧约三行改动。
    但它与 §2.2.1 第 1 条刚定文的签名不一致，**我没有擅自改**，等主控一句话裁定。
- **T-72 已并入收尾协议（规则采纳，未改代码/测试）**：
  ① 交回时除测试输出外，默认附一条 `cd /d/Project/Lucero && git diff --stat HEAD -- <我的路径>`；
     基线取当轮最新快照 `git log -1 --format=%h`，**不把哈希写死**（我此前几轮的证据命令里钉的是
     `900038d`，那是当时的基线、现已是 `cd46e42`，正好是这条规矩要防的事）。
     同时记牢它的边界：**diff 只证"某些文件我没动"，不证"我没改坏别人"**，后者靠最后一次全包重跑。
  ② 结构性编辑**禁止拿 `## ` 行当锚点**，改完立刻跑归属自检（本节结构复查已计入该自检）。
  **本轮当场用新形状取到的证据**：`git log -1 --format=%h` → `cd46e42`；
  `git diff --stat HEAD -- core/internal/platform/ core/internal/eventjournal/ core/migrations/` → **空输出**；
  `git status --short` 同范围 → **空输出**。即本角色全部产物（含 000003 迁移、四个源文件、六个测试文件）
  已由主控提交，**当前与基线逐字节相同、零未提交改动**。
- **T-66 已并入收尾协议（只写规则，未改代码/测试）**——变异验证的"备份-回滚"硬规矩：
  ① 备份只落绝对路径 `D:\Project\Lucero\.scratch\db\<具名文件>`；
  ② 回滚后**必须证明变异版不在盘上**（`grep` 一个只有变异版才有的标记串并贴出 0 命中，或 `diff` 与库内版本一致）；
  ③ 拿不到这条证据就**不许报"变异已还原"**；
  ④ 变异不许藏在早退后面——判据是"变异必须改变是否走到被验证的那段代码"。
  **本角色当场回补了 ② 的证据**（此前几轮我只 grep 过"原句在不在"，那属于正向核对，
  不满足新规矩要的"变异标记 0 命中"，所以按旧口径的"已还原"说法严格讲是证据不足）。
  现在把今天用过的每一个变异标记串都查了一遍，`grep -rn -F`（范围 `core/internal/platform` + `core/internal/eventjournal`）：
  ```
  scrubbed := args                               0
  tx.db.Read.QueryRowContext(ctx, q, args...)    0
  PolicyCreated:     false                       0
  "key:"+k                                       0
  !--                                             0
  ```
  并加两道正向核对：`grep -c 'scrubbed, err := tx.scrubArgs' touch.go` → **4**、
  `redact.go` 的 `return r.encode([]byte(t), column, h)` → **仍在**、
  `grep -c '^ALTER TABLE' 000003_*.sql` → **3**。
  **最硬的一条**：`git diff --stat HEAD -- core/internal/platform/ core/internal/eventjournal/`
  只列出 `migrate.go`(±4)、`migratefs_test.go`(16)、`touch.go`(+85) 三个我本轮/上轮有意改的文件，
  **`redact.go` 与 `events.go` 完全不在 diff 里 = 与库内版本逐字节相同**，
  即我今日动过变异的两个文件回滚是干净的、没给下一轮留破坏态。
  **④ 的自我判定**：我这三处变异都不早退、都改变被测路径的产物——guard 子句删除后失败信息是
  `replay changed 1 rows`（走到了 UPDATE 并真的写了行）、deny-key 前缀改回后失败在
  `Match still carries the "key:" prefix`（走到了 `h.add`）、JSON 短路那条打出
  `smuggled a secret: {"zebra":1,...,"token":"super-secret-value"}`（走到了 encode 的调用点）。
  **`settings` 写入口按主控裁定故意不开**（REQ-DB-2 待议），`narrowTables` 本轮复核仍只有
  `idempotency_keys` 一张，无需我动。
- **T-60 已接**：
  - **"契约正文与 REQ 只用符号引用"并入收尾协议**：本文件 `## Coordination` 里原先给 backend 的
    `core/api/http/tasks.go:301` 之类行号已全部改成符号（`claimIdempotency` / `sealIdempotency` /
    `narrowTables` / `Tx.UpdateOnce`）。行号只保留在①带当场 `date` 的观察证据、②测试失败原文。
  - **两处指针的归因我接受主控的更严判定**：`:172-173` 那处**从写下起就指着注释行**，
    不是我那 3 行注释推歪的——我上一轮的"被我的注释推后 3"只对上了 `:156/:174` 那一组，
    把另一处也一并算成行号漂移是我归因过宽。
  - **顺手补掉一个真实的覆盖缺口**（不是为验收，是查 backend 用法时看到的）：
    "已序列化 JSON 字符串"这条分支此前**没有任何用例走过 deny-key**。补
    `TestUpdateOnceRedactsPreSerializedJSON`，并**变异验证命中在声称的那一层**：
    把 `redactValue` 里 JSON 串的 `r.encode(...)` 短路成原样返回后 →
    `a pre-marshaled payload smuggled a secret: {"zebra":1,"alpha":2,"token":"super-secret-value"}`，
    恢复后 PASS。顺带产出上面那条键序规范化观察。临时探针文件按精确名删除、备份只落 `.scratch/db/`，
    用完连目录一起清掉（`ls .scratch/db/` → 不存在）。
- **T-57 已接（规则 + 契约对账）**：
  - "加表白名单必须走 REQ 过主控"已写进收尾协议，并**落在诱发点本身**——`narrowTables` 上方的注释现在
    明写这条，将来谁要在 map 里加表，第一行就会撞见它（未改任何行为）。
  - **契约 §1.1.13 与代码逐条对账**：签名、白名单表与四个可写列、主键 `key`、聚合表先于白名单被拒、
    guard 的"第一次 1 行 / 重放 0 行且回读不覆盖"语义、仍走脱敏、不发事件不取 version —— **全部与实现一致**。
  - **两处引用需要主控改**（都不是语义问题，是指针问题）：
    ① 原文"仍然跑脱敏（`:172-173`）"指的其实是函数上方的注释行，真正的调用是
      `scrubbed, err := tx.scrubArgs(...)`；
    ② 我这次插了 3 行注释，把你引的行号整体推后 3：**`narrowTables` 156→159、
      `UpdateOnce` 174→177、聚合表守卫 175→178、scrubArgs 206→209**。
    建议改用**符号引用**（`narrowTables`、`Tx.UpdateOnce`）而不是行号——你 13:5x 按实读写的行号
    到我这里已经漂了一次，行号在契约里是易腐品。
  - **`test` 那四条判据在 db 侧已各有通过用例**（14:29 实跑，可直接抄成它的断言，不必从零设计）：
    ```
    聚合表 error   → tasks.status refused: aggregate rows must go through Touch or Create
    白名单外 error → settings.value_json refused: not in platform/db's narrow update whitelist
    guard 不匹配   → 重放 RowsAffected=0 且 response_json 与首次逐字相同（用例直接比字符串）
    落库已脱敏     → sealed response_json: {"body":"created","token":"[REDACTED]"}
    ```
    第三个用例（不发事件不取 version）另有 `TestUpdateOnceTakesNoVersionAndWritesNoEvent` 守着。
- **T-48 / REQ-DB-1 交付：窄更新入口（三行原文，请主控折进 §1.1）**
  1. **方法名**：`func (tx *Tx) UpdateOnce(table, id, guardCol string, guardVal any, setSQL string, args []any) (int64, error)`
     —— 返回被改动的行数；`0` 表示"已被封存"，调用方据此把 `409 idempotency_replay` 换成"返回首次响应"。
  2. **白名单**（写死在 `platform/db/touch.go` 的 `narrowTables`，**当前只一张**）：
     `idempotency_keys`，主键 `key`，可 SET/可 guard 的列 = `state`、`response_json`、`updated_at`、`expires_at`。
     `execution_leases`、`settings` 按主控取证（前者全仓非测试零命中、后者只有 `permission/source_sqlite.go:30` 的读）**不进**，
     与 T-28③ 判 `policy_rules` 不建同一把尺子：没有生产消费者的入口就是投机件。
  3. **拒绝条件**（四类，全部有用例）：非白名单表 → `not in platform/db's narrow update whitelist`；
     **13 张聚合表（ADR-017 十张 + 000003 三张）即使将来被误加进白名单也再挡一层** → `aggregate rows must go through Touch or Create`；
     白名单表内的非列（如 `request_hash`、`principal_id`）→ `is not allowed` / `is not a guardable column`；
     SET 形态仍只认 `column = ?`（`state = 'COMPLETED'`、`state = ?; DROP TABLE …`、空串全拒）。
     另外它**不取 version、不发事件**（不变量 c 的定义），但**脱敏照走**（`response_json` 正是会带密钥的那种列）。
  - 形状上"首次完成即封存"是靠 `WHERE <pk> = ? AND <guardCol> <> ?` 表达的：
     db **不认识 `COMPLETED` 这个词**，状态语义留在调用方，平台层只提供"条件成立才写、并告诉你写了几行"。
  - 变异验证两条：删掉 guard 子句 → `replay changed 1 rows; want 0`；绕过 `scrubArgs` →
     `unsupported type map[string]interface {}`（证明序列化与脱敏确实同一条路）。恢复后全绿。
- **T-47 已并入收尾协议（硬规则）**：一次性产物只落 `D:\Project\Lucero\.scratch\db\`，
  删除只删自己建的具体文件名，**永不对 `/tmp`、`%TEMP%`、`$env:TEMP` 做任何通配删除或清理**。
  **本角色自曝**：前几轮我做变异验证时的 `.bak` 备份放在 `/tmp/`（`/tmp/r.bak`、`/tmp/ev.bak`、
  `/tmp/touch.bak`、`/tmp/m3.bak`、`/tmp/redact.go.bak` 共 5 个），**只用 `rm` 删过这些我自己建的精确文件名，
  从未跑过通配删除**，但按新规矩落点本身就是错的。本轮起改放 `.scratch/db/`（本次已用
  `.scratch/db/touch.go.orig` 并删掉该单文件），`/tmp` 不再写入。
  对主控那两句问话（@test/@frontend）与我无关，不代答。
- **T-52 已并入收尾协议（只写规则，未改任何代码或测试）**：`*.bak` 也算一次性产物，只准进
  `D:\Project\Lucero\.scratch\db\`；备份要么落自己角色目录，要么用带内容的唯一名；
  只删自己建的具体文件名；**永不对 `/tmp`、`%TEMP%`、`$env:TEMP` 做通配删除或"清理"**。
  `.bak` 不是证据物（证据是仓库源码现状 + 重跑输出），因此**不为其建任何保留/恢复机制**。
  本角色的变异验证今后一律：备份 → `.scratch/db/<唯一名>` → 恢复后只删那一个具体文件。
  **本轮当场核到的状态**（全部只读命令，13:41 由 `date` 取时）：
  - `find core/internal/platform core/internal/eventjournal .scratch/db -name "*.bak"` → **零命中**；
  - `ls .scratch/db/` → **空**（本轮用的 `.scratch/db/touch.go.orig` 已按精确文件名删掉）；
  - `ls -la /tmp/*.bak` → 还剩 `ev2.bak`(11:57) 与 `ev3.bak`(12:52) 两个，**不是本角色建的**
    （我历轮只建过 `/tmp/redact.go.bak`、`/tmp/m3.bak`、`/tmp/ev.bak`、`/tmp/touch.bak`、`/tmp/r.bak`，
    且均已按精确文件名删除；命名递增序列 ev2/ev3 与主控 13:26 归给 adapter/frontend 的形态一致）。
    **故我不碰它们**：不是我建的就不删，`/tmp` 更不做任何通配操作。仅如实上报供主控归并。
- **T-44 已执行**：`MigrateFS` → 包内 `migrateFS`。`grep -h "^func (d \*DB) [A-Z]"` 现在只剩
  `JournalMode` / `Close` / `Migrate` / `Write` 四个导出方法，公开面少一个无人调的 API。
  5 条 FS 用例改调私有名即可（`platform/db` 六个测试文件全是 `package db` 同包）。
  主控对 T-33 的自我纠正成立："保留"出自派发指令、不是契约③ §7，我上一轮把派发指令当成了契约依据 ——
  以后凡引用契约，我先 `grep` 出原文行号再说话，不引用派发话术当契约条文。
- **T-43 已并入收尾协议（硬规则）**：本文件任何时间戳，**写之前必须先跑 `date`**；没跑就只准写
  "未计时（本机未跑 date）"。且**不重写历史时间戳**（那只会造出第二批不可信数字），
  改为在文件顶部挂一条作废声明（已挂，见 Verification evidence 首段）。
- **REQ-T3c 的 db 侧取证（修复本身在 audit，见 T-40）**：db 产出的 `Kind` **不存在为空或未知的路径**——
  全包 `RedactionHit` 只有一处构造点（`redact.go:123`，在 `hits.add` 内），
  而 `h.add` 只有两个调用点（`redact.go:144` 传 `HitPattern`、`:163` 传 `HitKey`），
  两者都是包内字面量、**不接受调用方传入 Kind**。所以 audit 那道 `RedactionDetails` 校验
  在 db→audit 这条线上是纵深防御，不是补我的漏；用例侧我另有两条 guard 守着
  （`Match` 不得带 `key:` 前缀、`Kind` 必须是两值之一）。
- **T-33（fs.FS 入口的理由改为可测性）**：`MigrateFS` 保留、公开形状不变，但文档注释里
  "给 go:embed 供数据"的说法已按主控 12:10 的撤回改掉，现在写明"v0.1 交付走
  `cmd/core -migrations <dir>`，本入口的存在理由是让测试用内存 FS 驱动真迁移器"。
  **顺带删掉了上一轮的 embed 脚手架**：`internal/platform/db/testdata/migrations/*.sql` 两个文件与
  `//go:embed testdata/migrations` 全部移除，5 条 FS 用例改用 `fstest.MapFS`（含两条偏差用例，
  正是 T-33 点名的那两条）。覆盖面没减：顺序、幂等、真实迁移集、不回退磁盘、两种偏差各有断言；
  少的是"为一条已撤回的路径维护两份 .sql 副本"。
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
- **T-12（R-002 重开）已做**：
  - `core/migrations/000003_grant_policy_version.sql`（新建，未碰 000001/000002）：给
    `temporary_grants`、`policy_rules`、`adapter_capabilities` 各加 `version INTEGER NOT NULL DEFAULT 1`，
    沿用 000002 形状与自记账约定，不加 CHECK。
  - `AggregateKind` 增 `AggGrant = "grant"`、`AggPolicy = "policy"`、`AggAdapterCapability = "adapter_capability"`；
    `aggTable` 映射到 `temporary_grants` / `policy_rules` / `adapter_capabilities`。
    `aggTables`（Append 的聚合表黑名单）由 `aggTable` 反推，自动跟上，无需第二处维护。
  - `grant_test.go`（新建）三个生命周期用例；`migrate_test.go` 的 ledger 期望从 `[1 2]` 升到 `[1 2 3]`，
    并把合成脚本顺延到 `000004_*`；`copyMigrations` 改为**复制整个真实迁移集**，以后再加迁移不必改测试。
  - 版本列断言扩到 **13 张**聚合表。
- **T-20 已做**：
  - Q8 解卡：`eventjournal` 补 `PolicyCreated = "policy_created"`、`PolicyUpdated = "policy_updated"`，
    **常量与 catalog map 同步**（map 是 `Registered()` 的真身，只加常量等于没加）。事件常量 54 → **56**。
    `policy_rules` 从此与另两张表同形：`TestPolicyRulesTakesVersions` 走
    `Create(policy_created)` → `Touch(policy_updated)`，`version` 1→2、事件版本 `[1 2]`。
  - §1.1.11 事务内读：`Tx.QueryRowContext` / `Tx.QueryContext` 透传到事务自己那条写连接。
    **刻意不给 `ExecContext`**——那等于开一条绕过 `Touch` 的写路径。选哪张表读仍由调用方负责（§45）。
  - `TestTxReadsItsOwnUncommittedWork`：同一事务内 `Create` 之后，`tx.QueryRowContext` 读到 version=1、
    `tx.QueryContext` 读到自己的事件行；**同一时刻读池读同一行必须 `sql.ErrNoRows`**。
    这条两侧不对称正是 §1.1.11 说"在 `Write` 之外读版本会留竞态窗口"的实测形态。
- **fs.FS 迁移入口（T-24）已交付**（`migrate.go` 重构 + `migratefs_test.go` + `testdata/migrations/`）：
  - `MigrateFS(fsys fs.FS, dir string) error` 与 `Migrate(dir string)` **共用** `(*DB).migrate(fsys, dir, label)`；
    `Migrate(dir)` 的实现现在就是一行 `d.migrate(os.DirFS(dir), ".", dir)`。排序、两种偏差检查、
    备份、不包事务、不自写记账行——全部只有一份，不可能各走各路。
  - **要求①（只读 fs.FS）**：`migrate()` 里读脚本只剩 `fs.ReadFile(fsys, …)`，`os` 只用于
    `os.DirFS` 这一处构造。专测 `TestMigrateFSNeverFallsBackToDisk` 传空 FS，
    即便真实 `core/migrations` 就在磁盘上若干层外，仍报 `no .sql migrations in fs.FS(.)`。
  - **要求②（两种偏差都要报，不能只扫文件系统）**：
    - 记账了但文件丢了 → 动手**之前**拿 ledger 反查文件集：
      `schema_migrations records version 2 but no migration in fs.FS(.) provides it`（实测捕获）；
    - 有文件但未记账 → 每个脚本执行后回读 ledger 该版本号：
      `migration 000001_silent.sql changed the schema but recorded nothing (want exactly 1 ledger row, got 0)`（实测捕获）。
    - 我起初还加了一遍"应用完再全量对账"的后置 sweep，**随后自己删掉了**：它遍历的每个 present 版本
      要么本来就在 ledger 里、要么刚被 `recorded != 1` 放行，永远不可能命中 → 死代码。
      偏差方向没少，只是少一次多余的 `appliedVersions()` 往返。
  - **要求③（真跑 `embed.FS`）**：`//go:embed testdata/migrations` + `TestMigrateFSFromEmbed`
    把两步自记账脚本灌进一个真 `embed.FS`（非临时目录、非 MapFS），跑完 ledger `[1 2]`、
    第 2 步的 `ALTER` 落地、重跑幂等。**真实生产内容**也由
    `TestMigrateFSOverRealMigrations` 走同一个 FS 入口（`MigrateFS(os.DirFS(真实目录), ".")` → `[1 2 3]`）。
    两边合起来才等于"生产路径有人测过"：embed 管道用真 `embed.FS` 测，真脚本内容用真文件测。
- **T-27（REQ-T3）已交付**：`redact.go` 换成契约 §2.2 定文的形状
  `type HitKind string` + `HitKey="key"`/`HitPattern="pattern"` +
  `RedactionHit{Column, Kind HitKind, Match, Count}`（json tag `kind`）。
  分流不再靠字符串前缀：`hits` 的聚合键从 `column+"\x00"+match` 改成结构体键
  `{column, kind, match}`，`h.add` 显式收 kind。
  **`Match` 对 HitKey 记的是规则名的 canonical 形式**（`x-api-key`→`xapikey`），不是 payload 里的拼法——
  这样 audit 存进去的值与改造前逐字相同（他们本来就在 `CanonicalKey` 一下），
  且 `Authorization`/`authorization` 这种同规则异拼法在 db 侧就合并成一条，不会给审计留重复行。
  db 侧**没有任何地方再生产 `"key:"` 前缀**，用例里有专门断言守着这一点（见证据段）。
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
- **Q3（已闭环，本角色前一轮的说法作废）** 上一轮我写"db 侧接口已就绪，等 audit 用"——**过期断言**。
  实测：`internal/audit/append.go:133` 有 `func (c *Chain) RecordRedaction(ctx, base Entry, hits []RedactionHit) (int64, error)`，
  `internal/approval/store_sqlite.go:376` 与 `:438` 两处 `ch.RecordRedaction(…, toAuditHits(tx.RedactionHits()))`
  在同一事务里落链。契约 §2.2 的"命中记账"链路已完整，无需任何一方再动。
- **Q7（已由 audit 自行修好，关闭）** `internal/approval` 当时转 ok；
  `store_sqlite.go:212` 的注释已写明"temporary_grants is an aggregate since 000003 (AggGrant),
  so this goes through …"。（我当时附带的"死代码 `ErrGrantRevocationUnsupported` 可删"一条已作废，
  见下面"给 audit（撤回一条）"。）
- **Q8（已解卡关闭）** `policy_created` / `policy_updated` 已进 catalog（事件契约 §6 第 193 行由主控补上），
  `policy_rules` 与另两张表同形，`TestPolicyRulesTakesVersions` 实测通过。
- **给 backend（§1.1.11 现成可用，零适配）** `Tx.QueryRowContext` / `Tx.QueryContext` 的签名与你们
  已有的窄读接口**逐字一致**，直接把 `*db.Tx` 当参数传进去就行，不必再包一层：
  `internal/execution/repository.go:14`、`internal/quota/repository.go:14`、`internal/session/repository.go:15`、
  `internal/audit/append.go:28` 声明的都是 `QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row`。
  乐观并发照 §1.1.11：事务内读 version → 比对 `If-Match` → 不符就直接返回，**不要 Touch**（不产生事件、不推进版本）。
  平台层刻意不提供 `Tx.ExecContext`，别指望它绕开 `Touch` 写聚合行。
- **给 audit（撤回一条）** 我上一轮提的"`store_sqlite.go:35` 的 `ErrGrantRevocationUnsupported` 是死代码"
  已被 audit 于 17:44 自行删除，本角色当场 `grep -rn ErrGrantRevocationUnsupported --include=*.go internal`
  复核为**零命中**，该建议作废、无需再处理。
- **给 backend / audit：两条红包都不必追，是并发编辑的瞬时态**。本轮收尾全仓里先后出现过
  `internal/approval`（`vertical_slice_test.go:6` 未使用 import）与 `internal/task`
  （`service.go:149` `edgeEvent + ""` 把函数当字符串拼）两次 build failed，
  **最后一次重跑（18:52）10 个包全 ok**，两处均已由当事方自行收掉。本角色未触碰任何一个包。
- **Q9（本角色造成的红，audit 请收；改法你们已写在代码里）** db 的 `RedactionHit` 已带 `Kind HitKind`，
  且**不再生产 `"key:"` 前缀**。`internal/approval/store_sqlite.go:441 toAuditHits` 里的
  `strings.CutPrefix(h.Match,"key:")` 从此恒为 false，导致 deny-key 命中全被记成 `kind:"pattern"`，
  实测红在 `store_sqlite_test.go:347`（原文见 Verification evidence 的 T-27 段）。
  按你们自己在 `store_sqlite.go:437-440` 注释里写的方案收口即可，两行：
  `a := audit.RedactionHit{Column: h.Column, Kind: <映射 h.Kind>, Match: h.Match, Count: h.Count}`
  并删掉 `CanonicalKey(k)` 那步——**db 交出的 `Match` 对 HitKey 已经是 canonical 形式**（`x-api-key`→`xapikey`），
  与你们改造前存进 `entry_hash` 的值逐字相同，所以链上字段不会漂移。
  两边 `HitKind` 常量值都是 `"key"`/`"pattern"`，可以直接按字符串转或各自声明同名常量。
  护栏是 `test` 的 `TestRedactionCountersAreSealedBeforeHash`。本角色未触碰 `internal/approval/**`。
  **Q9 已闭环（本角色交回前最后一枪实测）**：`grep -n CutPrefix internal/approval/store_sqlite.go` →
  **零命中**（猜测已删），`TestStoreRedactsSecretsBeforeTheyLand` → **ok**，
  收尾全仓 `go test ./internal/... -count=1` → **12 个包全 ok**。
  即 §2.2 要求的"db 与 audit 同一轮改完"在本次达成，中间那次红只存在了约一分钟。
- **REQ-DB-1 已闭环（给 backend，改用符号引用，按契约 §1.1 第 14 条不再给行号）**：
  实读 `core/api/http/tasks.go` 的 `claimIdempotency` 与 `sealIdempotency`，backend 已把入口接对——
  `sealIdempotency` 用 `UpdateOnce("idempotency_keys", key, "state", "COMPLETED", …)` 且
  `n != 1 → errReplay`（0 行时事务回滚、不覆盖已封存响应），方向完全正确。此条无需 backend 再动。
  **一条实测观察请 backend 注意**：平台层为跑 deny-key 会把 JSON 字符串解码后重新编码，
  因此**落库字节的键序被规范化**（Go 对 map 按字典序输出）。实跑对照：
  入 `{"zebra":1,"alpha":2,"token":"super-secret-value"}` →
  存 `{"alpha":2,"token":"[REDACTED]","zebra":1}`。
  语义等价但**与首次返回给客户端的原始字节不同**：若 `sealIdempotency` 首次返回 `string(body)`、
  重放时由 `claimIdempotency` 回读存库值，同一请求两次拿到的 JSON 字节序不一致。
  若下游有任何按字节/哈希比较（不是按语义比较）的地方会踩到；不改也大概率无害，请自行判定。
  **加表白名单一律走 REQ**（契约 §1.1.13 已明写，本角色也在 `narrowTables` 上方注释就地提示）。
- **REQ-A4（已消解，留此存照）** 我曾提"`core/migrations/embed.go` 该谁建"。
  前提本身在 Go 里不成立——`//go:embed` 的模式不能含 `..`（编译器实测
  `pattern ../../../migrations: invalid pattern syntax`），只能取包目录自身或其子目录。
  主控 T-33 裁定 **v0.1 不做迁移内嵌**，交付走 `cmd/core -migrations <目录>`，故无需任何人建 embed 文件；
  `ls core/migrations/` 当场复核只有三个 .sql、无 `embed.go`（本角色从未建过）。
  三个可行解，都需要越出各自的边界，故请主控选：
  1. **`core/migrations/embed.go`**（`package migrations`，`//go:embed *.sql`）——最直白，
     但该目录本归本角色，且历史上"不在 migrations 建包文件"是主控的裁定；
  2. **`core/embed.go`**（模块根建一个小包，`//go:embed migrations/*.sql`）——不碰 migrations 目录，
     `core/` 根目前无 .go 文件，需主控认可模块根放包文件；
  3. **维持现状**：`cmd/core` 继续用 `-migrations` 目录参数（`Migrate` 已经能用），
     代价是 Core 不能单文件分发。
  本角色的 `MigrateFS` 对三者**一律可用**，不用再改平台层；backend 拿到哪一份 `embed.FS` 都只需
  `d.MigrateFS(fsys, ".")`（或子目录名）。
- **REQ-A3（给主控贴，本角色不写 `specs/`）** 你让"在内部契约里显式声明无事件表清单"，
  但 `specs/contracts-internal-v0.1.md` 按所有权表是主控独占、其余只能提 REQ，所以文本备好了给你贴
  （§1.1 第 10、11 条已被占，建议记为第 12 条）：
  ```markdown
  12. **故意不发事件的可变表**（写它们不需要 Touch/Create，用 Append 或各自的窄接口即可，
      不必为它们补聚合版本，也别被"没有事件"当成漏设计）：
      `execution_leases`（heartbeat 高频，进 journal 只会淹没事件）、
      `idempotency_keys`（纯基础设施状态，不是域事实）、
      `settings`（配置而非业务状态）、
      `blobs.archived_at`（冷热迁移，`process_*`/`blob_*` 无登记事件类型）、
      `process_instances`（其生命周期除 `process_started`/`process_exited`/`process_crashed`
      三个已由 `process` 聚合表达的外，不逐条进 journal）。
  ```
- → `cmd/core` 接线：`Migrate` 目前接收**迁移目录路径**（`embed` 需要 `core/migrations` 有包文件，越界）。
  生产要内嵌的话，请主控在 cmd/core 侧建 embed 并传 `fs.FS`，本角色可加一个 `fs.FS` 版入口。

## Changed files

- `core/internal/platform/db/db.go`（新建）— Open / PRAGMA 分层 / 读池 + 单连接写通道
- `core/internal/platform/db/migrate.go`（新建）— 迁移器 + VACUUM INTO 备份
- `core/internal/platform/db/touch.go`（新建）— Write / Tx.Touch / Tx.Create / Tx.Append / 列白名单
- `core/internal/platform/db/redact.go`（新建；T-27 改）— InitRedaction + 递归脱敏 + `RedactionHit{Column,Kind,Match,Count}`
- `core/internal/platform/db/db_test.go`、`migrate_test.go`、`touch_test.go`（新建）
- `core/internal/platform/db/update_test.go`（T-48 新建）— 封存一次、11 条拒绝、不取版本不发事件
- `core/internal/eventjournal/events.go`、`events_test.go`（新建）
- `core/migrations/000003_grant_policy_version.sql`（T-12 新建）— 三张可变表补 `version`
- `core/internal/platform/db/migrate.go`（T-24 重构）— `Migrate` 走 `os.DirFS`，新增 `MigrateFS`，两偏差检查
- `core/internal/platform/db/migratefs_test.go` + `testdata/migrations/00000{1,2}_probe_*.sql`（T-24 新建）
  — 真 `embed.FS`、真实迁移集走 FS 入口、空 FS 不回退磁盘、两种偏差各一条
- `core/internal/platform/db/grant_test.go`（T-12 新建）— grant/capability 生命周期 + policy 缺口钉桩
- `core/migrations/000002_aggregate_version.sql` — **未改**（实测通过）
- `core/migrations/000001_core_schema.sql` — **未改**（红线①）
- `core/go.mod`、`core/go.sum` — **未改**（归主控；无新依赖需求）

## Verification evidence

> **时间戳作废声明（T-43 要求的写法，不重写历史）**：本文件 **12:23 之前的所有时间戳均为估写、一律作废**
> ——那是我凭感觉写的、从未跑 `date`，不可用来对时或推因果。
> 本机真值由 `date '+%Y-%m-%dT%H:%M:%S%z'` 读得：12:22:24 → 12:59:54，日期已是 **2026-09-20**。
> 规则：本文件今后任何时刻，写之前先跑 `date`；跑不了就写"未计时（本机未跑 date）"。

命令均在 `D:\Project\Lucero\core` 下执行。

- **T-60 实测（时刻取自当场 `date`）**：
  - 新用例 + 变异：`TestUpdateOnceRedactsPreSerializedJSON` 先 PASS；把 `redactValue` 中
    JSON 串的 `return r.encode([]byte(t), column, h)` 短路为 `return t, nil` 后 →
    `--- FAIL: ... a pre-marshaled payload smuggled a secret: {"zebra":1,"alpha":2,"token":"super-secret-value"}`；
    恢复后 `grep` 确认该行回到原处、用例转绿。变异命中在声称的那一层，不是旁证。
  - backend 用法实读：`sealIdempotency` 里 `n != 1 → errReplay`、`claimIdempotency` 在
    `state == "COMPLETED"` 时回读 `response_json` —— guard 语义用对了，REQ-DB-1 对 backend 已无待办。
  - 临时件纪律：备份写在 `.scratch/db/redact.go.t60`，恢复后按精确文件名删除，`.scratch/db/` 已不存在；
    `/tmp` 本轮零写入、零删除。
  - **收尾全仓 `go test ./internal/... -count=1` → 10 ok / 3 红**（15:04 前后）：
    `internal/adapter`（`stream_test.go` 编译不过：`undefined: MethodMethodTurnClaimed` 等三个，
    看着像 `Method` 前缀叠了两次的笔误）、`internal/process`（4 条用例红）、`internal/task`（1 条红）。
    **三者均判为本角色之外且高度疑似实时编辑的瞬时态**，取证：
    - 单独重跑 `go test ./internal/task/ -run TestResumeRefusesStatesThatWereNeverSuspended` → **ok**；
    - 单独重跑 `./internal/adapter/` → 报的是**编译期 undefined**，与我包无关（`grep -l "UpdateOnce\|\.Touch(\|AggGrant"`
      在 adapter/process 里零命中，只有 `internal/task` 用我的写入口，而它单独跑是绿的）；
    - 我本轮改动只有 `touch.go` 的注释、`update_test.go` 的新用例与一次已恢复的 `redact.go` 变异，
      无任何行为变化；scoped 门禁全程 2 包 ok。
    → 三条红按"别人的包 + 瞬时态"上报，不归因本角色、也不替他们改。
  - `TestUpdateOnceSealsResponseExactlyOnce` → PASS，封存后的真实列值：
    `sealed response_json: {"body":"created","token":"[REDACTED]"}"`；首次 `RowsAffected=1`、
    重放 `=0`，且重放后 `response_json` 与首次逐字相同（用例直接比对字符串）。
  - `TestUpdateOnceRefusesEverythingElse` → 11 条全部被拒并打出各自的理由，摘三条：
    ```
    tasks.status refused: update on tasks: aggregate rows must go through Touch or Create
    execution_leases.heartbeat_at refused: update on execution_leases is not allowed: it is not in platform/db's narrow update whitelist
    idempotency_keys.request_hash refused: update idempotency_keys: request_hash is not a guardable column
    ```
    三条恶意 SET（`state = 'COMPLETED'` / `state = ?; DROP TABLE idempotency_keys` / 空串）全拒，
    种子行仍在（`count=1`）。
  - `TestUpdateOnceTakesNoVersionAndWritesNoEvent` → PASS：`event_journal` 行数前后不变。
  - 变异验证两条（先证会红再恢复）：去掉 guard 子句 → `replay changed 1 rows; want 0`；
    绕过 `scrubArgs` → `sql: converting argument $2 type: unsupported type map[string]interface {}, a map`。
    恢复后 `grep` 确认 `scrubbed, err := tx.scrubArgs` 与 `AND " + guardCol + " <> ?` 两处原文回到位。
- **T-48 / T-47 实测**（时刻由 `date` 读得：13:24、13:26）：
  - `go vet ./internal/platform/... ./internal/eventjournal/...` → VET_OK；
    scoped 门禁 → `ok platform/db 2.954s` / `ok eventjournal 0.657s`。
  - 收尾全仓两次：第一次 11 ok / 1 红 `internal/process`，第二次 **12 ok**；
    该包单独重跑两次均 `ok`（0.919s / 0.842s），且
    `grep -rn "UpdateOnce\|idempotency_keys" --include=*.go internal/process` → **零命中** →
    判为瞬时态（adapter 正在写该包），**不归因本角色、也不当别人的债报**。
  - 导出面复查：`grep -h "^func (d \*DB) [A-Z]"` → `JournalMode`、`Close`、`Migrate`、`Write`；
    新增的是 `*Tx` 上的 `UpdateOnce`（有真实消费者 backend，见 `core/api/http/tasks.go:157`）。
  - `.scratch/db/` 存在且 `git check-ignore -v .scratch/db/x` → 命中 `.gitignore:31`。本轮所有临时件都在此，
    `/tmp` 未再写入。
- **T-44 / T-43 实测**：
  - 导出面：`grep -h "^func (d \*DB) [A-Z]" internal/platform/db/*.go` →
    `JournalMode`、`Close`、`Migrate`、`Write` 四个（`MigrateFS` 已不在其中）；
    `grep -rn "MigrateFS" --include=*.go core/` 只剩测试函数名与错误文案，无调用方。
  - `go vet ./internal/platform/db/` → VET_OK；scoped 门禁 → `ok platform/db 2.241s` /
    `ok eventjournal 0.550s`、`gate_exit=0`。
  - **收尾全仓 `go test ./internal/... -count=1`**（改名后最后一步）→ **12 个包全 ok、零 FAIL**，
    含上一轮红的 `internal/process`（adapter 已收完，3.214s ok）。受影响方：**无**。
  - Kind 取值面取证：`grep -n "h\.add(" internal/platform/db/*.go` → 仅
    `redact.go:144`(HitPattern) 与 `redact.go:163`(HitKey)；
    `grep -n "RedactionHit{" internal/platform/db/*.go` → 仅 `redact.go:115`(map 类型) 与
    `redact.go:123`(唯一构造点，在 `add` 内)。即 db 侧无"空 Kind"产出路径。
  - 时钟：`date '+%Y-%m-%dT%H:%M:%S%z'` 于本轮两次读得 **12:22:24** 与 **12:59:54**，
    `date '+%A'` → Sunday，日期为 **2026-09-20**。

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
- **T-20 实测与变异验证**：
  - 四条新断言先各改坏一次确认会红，再恢复：
    - 把 catalog map 里 `PolicyCreated/PolicyUpdated` 两行注掉 →
      `--- FAIL: TestRegistered — "policy_created" is in the contract catalogue but not registered`
    - 把 `Tx.QueryRowContext` 的实现从 `tx.conn.…` 换成 `tx.db.Read.…` →
      `--- FAIL: TestTxReadsItsOwnUncommittedWork — in-tx read could not see its own row: sql: no rows in result set`
    - 恢复后两条命令均回到 PASS。第一轮我 sed 的锚点没匹配上（gofmt 重排过列），
      是靠回读 `grep -n "PolicyCreated:"` 才发现"变异没生效"，不是测试放过它。
  - scoped 门禁（恢复后）→ `ok internal/platform/db 2.582s` / `ok internal/eventjournal 0.838s`；`go vet` 同范围 VET_OK。
  - `CGO_ENABLED=0 go build ./...`（全部非测试包）→ **exit 0**，即生产代码无一处编译不过。
  - T-16 规定的收尾全仓 `go test ./internal/... -count=1` → 9 ok，1 红：
    `internal/approval [build failed]`，当场命令与输出（**隔了两次跑，同一条错误，确认非抖动**）：
    ```
    $ go test ./internal/approval/ -count=1 -run TestVerticalSlicePermissionToApprovalToGrant
    internal\approval\vertical_slice_test.go:6:2: "strings" imported and not used
    FAIL	github.com/SadCat18/lucero/core/internal/approval [build failed]
    ```
    **归因：不是本角色**。① 它只是测试文件的一个未使用 import，且 `go build ./...` 全绿；
    ② 本轮我对 eventjournal 只做**新增**（两个类型 + map 条目），未改任何已有类型的行为，
    也未再动 `AggregateKind → 表名` 映射（§1.1.10 认定的破坏源是映射与校验行为，不是新增常量）；
    ③ `grep -rn "policy_created\|policy_updated" --include=*.go internal`（排除本角色两包）→ 零命中，
    没有任何一方断言过这两个类型"必须被拒"。受影响方：**audit**（它 17:44 刚动过 `store_sqlite.go`，
    这个测试文件是同一次编辑没收尾）。
    **该"受影响方"判定随后被自己的重跑推翻**：最后一次 `go test ./internal/... -count=1`（18:52）→
    **10 个包全 ok**（approval 与中途另一处 `internal/task/service.go:149 edgeEvent + ""` 的 build failed
    都已由当事方收掉）。教训同 §1.1.10 同源：**并发工作树里"谁红了"必须是最后一个样本**，
    单点采样点名会把瞬时态误报成别人的债。
  - 零适配性的正面证据：`internal/task/repository.go:13` 声明的正是
    `QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)`，
    与本角色新增的 `Tx.QueryContext` 逐字一致 → backend 可直接把 `*db.Tx` 传进它已有的窄接口。
- **T-16 补做的全仓复核（契约 §1.1 第 10 条）**：
  - `CGO_ENABLED=0 go test ./internal/... -count=1` →
    ```
    --- FAIL: TestTurnTrackerKeepsEvidence
        stream_test.go:384: after claim: turnID="" state="CLAIMED"
        stream_test.go:387: Provider handle "pre-7" set before turn/dispatched
        stream_test.go:412: INTERNAL_ERROR: turn/dispatched names execution "e9" but the claimed turn is "t9" (§6.4)
    FAIL  github.com/SadCat18/lucero/core/internal/adapter
    ok    …/internal/approval  audit  eventjournal  execution  permission  platform/db  quota  session  task
    ```
    即 11 ok / 1 红。红的是 `internal/adapter` 自己的 §6.4 turn 认领时序，
    **归因证据**：`grep -rn "platform/db\|temporary_grants\|AggGrant" --include=*.go internal/adapter` → **零命中**，
    adapter 包根本不碰平台层。受影响方：`adapter` 角色。本角色未触碰。
  - **交回前最后一步再跑一次 `./internal/...`（同 18:30 左右）→ 10 个包全 ok**：
    adapter 在它自己那一轮里把 `TestTurnTrackerKeepsEvidence` 修好了，`internal/...` 现已全绿。
    本轮 scoped 门禁 `./internal/platform/... ./internal/eventjournal/...` → 2 包 ok、`gate_exit=0`。
  - 新晋聚合表的写点 sweep（按**表名**查，比字面 `Append(` 宽）：
    `grep -rn "\"temporary_grants\"|\"policy_rules\"|\"adapter_capabilities\"" --include=*.go internal`
    → 除本角色三个文件（`touch.go` 注册表、`grant_test.go`、`migrate_test.go`）外**零命中**；
    `internal/audit/append.go:117` 的 `Append("audit_entries", …)` 与
    `internal/task/{repository,rejection_db}_test.go` 的 `Append("principals", …)` 都不在新晋聚合表上，不受影响。
  - **教训**：上一轮我只 grep 了字面 `tx.Append(`，正好错过 approval 用辅助函数写 grants 的那处
    （它后来自己换成 `Create(AggGrant,…)`）。按表名 sweep 才不会漏；本轮起把
    `go test ./internal/...` + 表名 sweep 钉成"改注册表当轮的最后一个动作"，而不是收尾报告前的一次。
- **T-12 实测**：
  - `TestGrantLifecycleTakesConsecutiveVersions` → PASS：`temporary_grants.version = 3`，
    三个 grant 事件 `aggregate_version` 恰为 `[1 2 3]`；断言 `aggregate_type` 为 `grant`
    且 `'temporary_grants'` 行数为 0；`Append("temporary_grants", …)` 被拒
    （`aggregate rows must go through Touch or Create`）。
  - `TestAdapterCapabilityTakesVersions` → PASS：`adapter_capabilities.version = 2`，事件版本 `[1 2]`
    （用已登记的 `adapter_capability_verified`）。
  - `TestPolicyRulesSchemaReadyButCatalogueMissing` → PASS：`UPDATE policy_rules SET … version = version + 1
    WHERE id = 'pr-1' RETURNING version` 直接跑通并返回 2（schema 半边已好），
    `Touch(AggPolicy, …)` 唯一卡点是 catalog（断言错误串含 `not in the event journal catalogue`，
    且不是 `ErrAggregateNotFound`），失败的 tx 回滚后 `version` 仍为 2。
  - `TestMigrateAppliesRealMigrationsThenIsIdempotent` → PASS：`ledger after migrate = [1 2 3]`、
    重跑仍 `[1 2 3]`，13 张聚合表 `pragma_table_info` 全报 `version` 且 `notnull=1 default=1`。
  - **变异验证**：把 000003 的三条 `ALTER TABLE` 注释掉（迁移仍自记账成功）后 →
    ```
    --- FAIL: TestGrantLifecycleTakesConsecutiveVersions
        grant lifecycle: create grant grant-1: SQL logic error: table temporary_grants has no column named version (1)
    --- FAIL: TestAdapterCapabilityTakesVersions
        capability lifecycle: create adapter_capability cap-1: … adapter_capabilities has no column named version (1)
    --- FAIL: TestPolicyRulesSchemaReadyButCatalogueMissing
        policy_rules cannot take the Touch numbering statement: … no such column: version (1)
    ```
    恢复后三个全绿 → 用例真的依赖这次迁移，不是顺带通过。
  - 波及面：`grep -rn "ErrGrantRevocationUnsupported" --include=*.go internal` 仍命中
    `internal/approval/store_sqlite.go:294` 与 `store_sqlite_test.go:437`（audit 侧待删，见 Q7）。
- **INFO（全量，本轮）** `go test ./... -count=1`：`platform/db`、`eventjournal`、`adapter`、`audit`、
  `permission`、`quota`、`session`、`task` 均 ok；红 2 个包：
  - `internal/approval` → **本角色 000003 造成**（见 Q7，audit 需把 `Append` 换成 `Create`）；
  - `internal/execution` → 与本角色无关，是它自己的测试夹具：`repository_test.go:58` 直插 `executions`
    时 `FOREIGN KEY constraint failed`（`session_id='sess-q'`、`predecessor_execution_id='ex-1'` 等父行未种）。
    本角色未触碰该包。
- **T-33 实测**：
  - `grep -rn "go:embed|embed\." --include=*.go internal/platform internal/eventjournal` → **零命中**（脚手架清干净）；
    `ls core/migrations/` → 只有 `000001/000002/000003` 三个 .sql，**没有 embed.go**（本角色从未建过，
    与契约③ §7 第 137 行"`find core -name embed.go` 零命中"一致）。
  - `go test -run MigrateFS -v` → 5 条全 PASS，两条偏差仍各自打出错误原文：
    `schema_migrations records version 2 but no migration in fs.FS(.) provides it` /
    `migration 000001_silent.sql changed the schema but recorded nothing (want exactly 1 ledger row, got 0)`；
    空 FS 那条打 `no .sql migrations in fs.FS(.)`（真实脚本就在同仓库磁盘上，证明不回退）。
  - scoped 门禁 → `ok platform/db 2.506s` / `ok eventjournal 0.725s`；`go vet` 同范围 VET_OK。
  - **收尾全仓 `go test ./internal/... -count=1`** → 11 ok / 1 红：`internal/process [build failed]`。
    **归因：adapter 正在实时编辑该文件，与本角色无关**，证据是相隔 20 秒两次 `go build ./internal/process/`
    报的是**不同的错**（文件内容在变）：
    ```
    12:21:46  connection.go:8:2: "time" imported and not used
              connection.go:169:11: c.turnLocked undefined ...
    12:22:06  connection.go:282:94: syntax error: unexpected newline in argument list
    ```
    本角色不触碰 `internal/process/**`。受影响方：**无**（本轮只动了 `MigrateFS` 的注释与 db 包内测试文件，
    没有任何导出签名变化；`Migrate`/`RedactionHit` 等消费方零影响）。
  - 顺带核到一条事实：`MigrateFS` 目前**全仓零外部调用方**（`grep -rn MigrateFS --include=*.go core/`
    排除本包后无命中），`cmd/core/main.go:115` 用的是 `d.Migrate(cfg.Migrations)`。
    契约③ §7 说该入口"保留"，故本角色保留其导出形态；若主控认为 v0.1 无生产调用方就不该公开，
    改成包内 `migrateFS` 是一行事（测试同包，可直接调），请一句话定。
- **T-27 实测与变异验证**：
  - scoped 门禁 → `ok platform/db 2.530s` / `ok eventjournal 0.706s`、`gate_exit=0`；`go vet` VET_OK。
  - 用例把 kind 与 match 成对断言：`{HitKey,"xapikey"} {HitKey,"apikey"} {HitKey,"password"}
    {HitKey,"authorization"}` 四条必现，pattern 侧必须**恰好一条**且是 bearer 那条正则；
    另有两条硬Guard：`h.Match` 不得以 `"key:"` 开头、`h.Kind` 必须是 `HitKey`/`HitPattern` 之一。
  - **变异验证**：把 `h.add(column, HitKey, canonicalKey(k))` 改回 `"key:"+k` →
    `--- FAIL: TestDenyKeyValuesNeverReachPayloadJSON — REQ-T3 not honoured:
     Match still carries the "key:" prefix convention: {Column:payload_json Kind:key Match:key:x-api-key Count:1}`
    → 断言确实守着前缀约定，不是摆设。恢复后转绿。
  - **收尾全仓（破坏性变更必做）**：`go test ./internal/... -count=1` → 11 ok / 1 红，
    红的是 `internal/approval/TestStoreRedactsSecretsBeforeTheyLand`（隔两次重跑同一错误，非抖动）：
    ```
    store_sqlite_test.go:347: audit redaction row missing "kind":"key":
      {"redactions":[{"column":"approvals.request_json","count":1,"kind":"pattern","match":"apikey"},
                     {"column":"approvals.request_json","count":1,"kind":"pattern",
                      "match":"gh[pousr]_[A-Za-z0-9]{20,}"}],"schema":1}
    ```
    **这条输出正好是 REQ-T3 描述的那个缺陷的实拍照**：db 交出去的是 `{Kind:key, Match:"apikey"}`，
    audit 的 `CutPrefix("key:")` 猜不到 → 整条被记成 `kind:"pattern"`，
    即"deny-key 命中被静默记成正则命中"。归因：**本角色此改动造成**，
    修法在 audit 范围（Q9），本角色按红线未代改 `internal/approval/**`。
    注：输出里 `match:"apikey"` 与改造前一致，印证了"db 记 canonical 名"让 audit 只需改 Kind 一行、
    其余存链字段零漂移。
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

- **T-24（fs.FS 入口）实测**：
  - `go test -run MigrateFS -v` → 5 条全 PASS，两条偏差用例把错误原文打了出来：
    ```
    --- PASS: TestMigrateFSFromEmbed                (ledger [1 2]，第 2 步 ALTER 落地，重跑幂等)
    --- PASS: TestMigrateFSOverRealMigrations       (真迁移集走 FS 入口 → [1 2 3])
        migratefs_test.go:63:  no .sql migrations in fs.FS(.)
    --- PASS: TestMigrateFSNeverFallsBackToDisk
        migratefs_test.go:86:  schema_migrations records version 2 but no migration in fs.FS(.) provides it
    --- PASS: TestMigrateFSReportsLostMigration
        migratefs_test.go:105: migration 000001_silent.sql changed the schema but recorded nothing (want exactly 1 ledger row, got 0)
    --- PASS: TestMigrateFSReportsUnbookedMigration
    ```
  - scoped 门禁全量复跑 → `ok platform/db 2.255s` / `ok eventjournal 0.507s`，`gate_exit=0`；`go vet` VET_OK。
  - **收尾全仓（T-16 规定动作）** `go test ./internal/... -count=1` → **11 个包全 ok**
    （adapter / approval / audit / eventjournal / execution / notification / permission / platform/db /
    quota / session / task）。受影响方：**无**——公开入口 `Migrate(dir string) error` 一字未改，
    改签名的是未导出的 `readMigrations`。消费方当场核对：
    `grep -rn "\.Migrate(" --include=*.go internal cmd` →
    `approval/execution/notification/permission/task` 五处测试 + `cmd/core/main.go:114`，
    全部走磁盘入口且全绿。
  - **`go:embed` 硬限制的实测证据**（给 REQ-A4）：在本包临时建 `//go:embed ../../../migrations` →
    `probe.go:5:12: pattern ../../../migrations: invalid pattern syntax`；
    同一位置改指本包内 `testdata/x` → 编译通过。即 embed 只能取**包目录自身或其子目录**。
    现场读到的 `cmd/core/main.go:59` 已写着"迁移 .sql 目录（go:embed 归属未定，见 progress REQ-T-2）"，
    说明 backend 自己也没落成 embed，目前走的是 `-migrations` 磁盘目录参数。

## Recovery note

- Interruption point: db-platform 全部判据 + T-6/T-7/T-12/T-20/T-24 均已落地实测；
  本角色范围内无未完事项，开放项只剩需要别人动的：REQ-A4（`go:embed` 归属，主控拍板）、
  REQ-A3（无事件表清单文本待贴进契约）、Q6（audit 收窄 `IsVaultRef` 注释）
- First action after recovery: 跑 scoped 门禁 `go test ./internal/platform/... ./internal/eventjournal/...`
  确认仍绿；若 `cmd/core` 的迁移装载要改成 embed，按 REQ-A4 选定的那一份 `embed.FS`
  调 `d.MigrateFS(fsys, ".")` 即可，平台层不需再改
- 历史包袱提醒：Q4 已把 `AggQuota` 定为 `"quota"`，若谁又报"未知聚合"，先查它是否还在写旧字符串
  `"quota_account"`

OWNER_ROLE: db
TASK_ID: db-platform
STATUS: complete（T-413 两行已落、C8-12 转 PASS、`go test ./...` 18 包 ok／0 FAIL；只剩合批入册这一件不在我手里）
SCOPE: Core 的 DB 平台层唯一写路径（连接与 PRAGMA 分层、串行写通道、事务原语、迁移器、Event Journal 写入、强制脱敏）；不含业务语义
EVIDENCE_DIR: .qoder/evidence/
LAST_UPDATED: 2026-09-30T22:06+08:00
UNCOMMITTED: core/internal/eventjournal/events.go —— **T-413 的两行**（`git diff HEAD --numstat` = `2 1`，21:58:11 实测；归属 db）。
  同族等合批的另两件不在我名下、也还未入库：`specs/events/event-contract-v0.1.md`（主控 §6 一行）与
  `specs/openapi/core-v1.yaml`（frontend T-415 一枚 token）；backend 的 `claim.go` 等仍是未跟踪 `??`。
  ⇒ HEAD `09dd199` 里 `execution_claimed` **三处全 0（自洽，不会红）**，工作树三处齐（全绿）。
  上一批 `execution_unknown` 已在库（`2c65177` 19:57:03）。
  被验树指纹（本轮末次）：**构建时刻 2026-09-30T21:57:37、HEAD `09dd199`、未提交 20 处、编译输入 2380 个、
  指纹 `38bf468ccdd2273adb60cf582da4f841`、包 [./cmd/core ./test]**（前一枚 `28ca76db…` 是 21:56:44、
  frontend 落 yaml 之前那棵树，留着标"被取代"）

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

- Stage: **T-413（主控 21:5x 补派的那两行）已落，全仓转绿**——`events.go:49` 常量 `ExecutionClaimed` +
  `catalog` 的 `:93` 一枚 `true`，与今天已入库的 `ExecutionUnknown`（`:48`/`:92`）逐字同构；
  上一批 **T-403/407 整条链由 `2c65177`（19:57:03）一次入库并复测通过**（没留 09-24 那种窗口）
- Status: complete（**代码上不等任何人**；我那两行等主控合批入册，这与"等裁"不同——见下面"等（三问）"）
- Last verified action: **最后一次重跑 = 09-30 21:55:01→21:57:37（当场 `date` 实读；本行于 22:01 刷新）** ——
  scoped 门禁 `-v`：**`=== RUN` 49、49 `--- PASS`、0 FAIL、0 SKIP**（`ok platform/db 2.601s` + `ok eventjournal 0.514s`），
  我包内对拍 `contract §6 and the catalogue agree on **66** event types`；`gofmt -l` 空、`go vet` exit 0；
  主控点的那条判据 **从红转 PASS**：`C8-12：目录 66 类 / 常量 66 个；两个"独有"都空`（21:55:17）；
  `./test/` 整包 **55 PASS／0 FAIL／7 SKIP**、`./internal/...` **13 包 ok**、`go test ./...` **18 包 ok／0 FAIL**（21:57:37）
  ——**那 7 个 SKIP 不在我包里**（我 scoped 的 SKIP = 0），本轮没去逐一点名它们等谁；
  变异取证：摘掉 `ExecutionClaimed: true`（`subs=1`）→ **FIRED**（§6 registers 但 catalogue 没有），
  **同变异下 C8-12 仍 PASS ⇒ map 那枚的唯一见证是我那条对拍**（与 T-252 同一格）；
  被验树指纹 **21:57:37、HEAD `09dd199`（21:31:58）、未提交 20 处、编译输入 2380、
  指纹 `38bf468ccdd2273adb60cf582da4f841`**（前一枚 21:56:44/`28ca76db…` 对应 frontend 落 yaml 之前的树）；
  **末两次复跑 22:02:49→22:02:51**：`LUCERO_REQ_DB2=1` 那条 `--- PASS (0.19s)`、
  `C8-12 66/66` 与 **`C8-13 三格相等 §6=66 / yaml=66 / 常量=66，四条边全空`** ⇒ 三处在**工作树**已齐，只差入库；
  **交账前最后一次 22:06:23** 再跑 scoped：**`RUN` 49／49 PASS／0 FAIL／0 SKIP**，
  指纹 22:06:24/HEAD `09dd199`/`a7753907b8a7333918a41315c235e7e8`（未提交 21 处、编译输入 2381）
  ——**别人在途会让指纹每跑必变**，所以配对读数的是"哪一枚对应哪一次判据"，不是"哪一枚最新"
- 等（三问，按 T-276②）：**等主控合批**——**等谁**＝主控；**等什么**＝`execution_claimed` 那一族的入库
  （21:58:11 实测 HEAD 内三处全 0、工作树三处齐：我的 `events.go` +2/−1、主控的 §6、frontend 的 yaml，
  外加 backend 那 3 个未跟踪件）；**谁解锁**＝主控（入册）。
  **HEAD 那棵树这一轮是自洽的、不会红**（backend 的发射位 `claim.go` 还是 `??` 未跟踪）⇒ 只是"未入库"，不是"缺一致性"。
  - 已成立的历史格（留档勿再当待办读）：~~等合批入册（execution_unknown 那一族）~~ ⇒ **19:57:03 由 `2c65177` 落地**、
    我 20:25:47 复跑全绿；~~等主控在 (a)/(b) 之间选~~ ⇒ T-407① 已裁 **(a)**；
    ~~等主控把 `versionless_test.go` 那 +3 行入册~~ ⇒ 09-25 18:11:58 复量已成立（`2c56023`、HEAD 内 = 1）。
- 范围自证：**基线现取 `09dd199`（09-30 21:31:58，`git cat-file -t` = commit）**——本轮 HEAD 一路换过
  `738cb13`(19:02) → `07d2163`(20:02) → `89b32b3`(20:27) → `09dd199`(21:31)，所以每个数都带自己那次的时刻、不互相顶替
  （19:2x 那三枚哈希的分歧已核完、只 `73c926d` 无效且巡检已自纠：详见 `## Completed` 的 T-407 收单段）。
  **本轮我名下只有 1 件、且未入库**：`git diff HEAD --numstat` = **`2 1 core/internal/eventjournal/events.go`**
  （T-413 那两行），`git status` 同一件 ` M`；`touch.go`、`versionless_test.go`、`core/migrations/` 对 HEAD 零差异；
  **未跟踪件本角色 0 个**（backend 的 `claim.go`/`claim_test.go`/`turnpump_claim_test.go` 是它的，我没碰）。
  上一批 `ExecutionUnknown` 的两行确认在库：`git log -1 -- …/events.go` = **`2c65177`（19:57:03）**、
  `git show HEAD:…/events.go | grep -c ExecutionUnknown` = **2**。
  计数口径并列不推：21:57:37 指纹报"未提交 20 处"（含我 1 件），19:33:55 我数 numstat 总 23；
  `events.go` 的 mtime 现读 **21:56:36**（T-413 还原后的字节），旧句"18:27:16/17:37:08"都随各自那次为真、现已过期，就地留着不抹
- Next action: **无待办**；下一次开工先复跑 `Verification` 那两条 + `go test ./test/ -run TestEventTypeTripleEquality`
  确认合批后三处仍在库、基线没被带歪（上一句"等合批"针对 `execution_unknown`，已于 19:57:03 落地并复跑完；
  本轮 `execution_claimed` 的合批是同一件事的下一次，**不阻塞我**）。**我不动 `specs/**`、不动 `core/test/**`、不放宽对拍。**
  - 已作废/已成立的历史层（T-276③：头部只留活动项，历史按"留档"读，勿再当待办）：
    ① ~~把 T-233 与 adapter 那两处更正按改后口径转告 adapter~~（T-243 08:20 销账：巡检 07:50 已覆盖同一格，
      且"分开两种错形"本是 **adapter 07:46 先纠出来的**——它按 `landedStatus` 落码在先，
      第 20 条 ⑤ 是 07:5x 才把它的做法升成规则 ⇒ 我不重发是对的，不造第二份真相）；
    ② ~~确认 `git show HEAD:core/internal/eventjournal/events.go | grep -c ExecutionStarting` 应为 2~~
      （10:11:49 实测 = **2**，经 `38cafa8` 10:03:46 入册 ⇒ 成立、划掉，不再复跑）；
    ③ ~~等 yaml 那一格并专门复跑 C8-13~~（T-258 FYI① 采纳；frontend 09:24:45 已落地、
      我 09:26:42 一并看到 **C8-13 三格相等 + 两条 PASS**，yaml 由 `b47c14c` 09:35:24 入册，我没碰过它）。
  - 内容已在正文落实的原文留档（T-233 段）：① 建行写进去的状态 v0.1 是 `STARTING`（`supervisor.go:420`
    带 `StatusStarting`），不是我 07:33 说的 `RUNNING`（那个值出自契约正文里主控自认编的那一处，
    已就地改准；第 20 条 ⑤：`guardVal` 只能用"写口自己写进库的那个状态"）；
    ② 我那句"拿观察态当 guard……第二次还撞 `event_id` 的 UNIQUE"把两种错形合成了一种——
    固定用观察态 ⇒ 每次干净 0 行 + 一句喊、永远走不到记事件；只有"guard 跟着状态前进"才会撞 `event_id`
    的 UNIQUE（adapter 用 m3 复现过）。⇒ 本角色代码零改动，改的是我这两次转述。
- Verification: 三条，本轮全部复跑过（① 21:56:55→21:57:37、②③ 22:02:49→22:02:51；判据自带执行证明：
  `-v` 里 `grep -c '^=== RUN'` 必须 > 0，scoped 那轮实读 **49** ⇒ 不是"空匹配也 ok"）——
  ① `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test ./internal/platform/... ./internal/eventjournal/... -count=1`
  （21:56:55 复跑：`RUN` 49／49 PASS／0 FAIL）
  ② 契约① 第 17 条的落地判据（只在带 flag 时**单跑那一条**，不带 flag 全仓）：
  `cd /d/Project/Lucero/core && LUCERO_REQ_DB2=1 CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -v -run 'TestLifecycleRowLandsTheFiveColumns' ./cmd/core/`
  （**本轮 22:02:49 复跑 → `--- PASS (0.19s)`**；不带 flag 时同一用例是 `--- SKIP`，两跑对照在 09-25 18:14 记过）
  ③ 三格同步判据（在 test 角色名下，我只跑不改）：
  `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -v -run 'TestEventCatalogMatchesJournalConstants|TestEventTypeTripleEquality' ./test/`
  → 22:02:51 **两条 PASS**，原文 `C8-12：目录 66 类 / 常量 66 个；两个"独有"都空`、
  `C8-13 三格相等：§6=66 类、yaml=66 条、常量=66 个；四条边全空`

## Completed

- **T-413（09-30 21:54：登记 `execution_claimed`）两行已落，全仓转绿**（这是主控 T-412① 漏派、21:5x 补开的那张，
  主控自己写明"这一格是我漏派给你的，不是你的欠账"）：
  - **改的两处与 `ExecutionUnknown` 逐字同构**：`events.go:49` 常量 `ExecutionClaimed = "execution_claimed"`
    （紧跟 `:48`）、`catalog` 的 `:93` 加一枚 `ExecutionClaimed: true`（`gofmt -w` 后那一行重排，
    尾部 `ExecutionPaused/Resumed` 两枚**没被我吃掉**——我改完把 90~94 行整片读回来核过）。
    仍**没有 payload 表**（依据同前：`Registered()` 只查 `catalog[ev]`）。`git diff HEAD --numstat` = **`2 1`**。
  - **判据按主控点名的那条走，并带执行证明**：`TestEventCatalogMatchesJournalConstants`
    从主控给的现读"目录 66 / 常量 65、独有 [execution_claimed]"变成 **`PASS：目录 66 类 / 常量 66 个，
    两个"独有"都空`**（21:55:17）；我包内 `TestCatalogMatchesContract` 同步从 65 变
    **`contract §6 and the catalogue agree on 66 event types`**，scoped 门禁 `-v` **`=== RUN` 49／49 PASS／0 FAIL**（21:56:55→21:57:37）。
  - **"目录那一枚 true 有没有见证"我又变异验了一次，第一次取证无效（我的 cwd 错）**：
    把 `ExecutionClaimed: true` 从 map 里摘掉（`subs=1`）→ **FIRED**
    （`event contract §6 registers [execution_claimed] but the catalogue has no entry/constant for them`），
    而**同一变异下 C8-12 仍 PASS**（它比的是 §6 ↔ 常量声明，看不见 map）⇒ 与 T-252 同一格：
    **map 那枚的唯一见证是我那条对拍**。
    **第一次跑我在 `internal/eventjournal/` 目录里用了相对包路径**，拿到的是 `[setup failed]` 一类的空输出
    （我 09-30 05:4x 记过的坑又踩一次）⇒ 那次不作证，回到模块根 `core/` 重跑才有上面那行 FIRED；
    还原以 `md5sum -c` = OK、`grep -Fc 'ExecutionClaimed: true'` = 1 自证。
  - **三处同改这次几乎同时到位，只剩入库**：§6 由主控 21:3x 前落（工作树 1 命中）、
    yaml 由 frontend 在 **21:56:44→21:56:55 之间**落下（我 21:55:17 量 C8-13 还是 `yaml=65` FAIL，
    40 秒后复跑就 `PASS §6=66/yaml=66/常量=66` 一类相等 ⇒ **两条读数都真，差在落地那一刻**），
    于是 `./test/` 整包 **55 PASS／0 FAIL／7 SKIP**（那 7 个 SKIP 不在我包里，我 scoped 是 0 SKIP）、
    `./internal/...` 13 ok、`go test ./...` **18 包 ok／0 FAIL**（21:57:37）。
    上一轮 20:27 那枚 `TestEventProducerRosterMatchesDerivedSet` 的红也随 §6 那行自消（我没碰 `core/test/**`）。
  - **指纹两枚**（同一轮内树动过一次）：21:56:44/HEAD `09dd199`/`28ca76dbd248e7593c89f07964f9707d`（未提交 20 处）
    与 **21:57:37/HEAD `09dd199`/`38bf468ccdd2273adb60cf582da4f841`（未提交 20 处、编译输入 2380）**；
    后者对应上面那组全仓绿读数。
  - **入库状态实测（21:58:11/21:58:34，不猜）**：HEAD `09dd199`（21:31:58）里 `execution_claimed` **三处全 0**
    （`git show HEAD:specs/events/event-contract-v0.1.md`、`HEAD:specs/openapi/core-v1.yaml`、
    `HEAD:core/internal/eventjournal/events.go` 各 0），工作树里三处各 1/1/2 ⇒ **我的、主控的、frontend 的那一件
    现在都还在树里等合批**。
    **但这一轮"干净检出 HEAD 会不会红"答案是不会**：backend 的发射位 `core/internal/execution/claim.go`
    仍是**未跟踪**（`git ls-files --error-unmatch` 报 "did not match any file(s) known to git"），
    `HEAD:core/internal/execution/events.go | grep -c ExecutionClaimed` = **0** ⇒ HEAD 那棵树自洽，
    没有 20:27 那枚 roster 红所依赖的"发射位已入库、词表未入库"不对称。
    **给主控的时机信息（不是我的账）**：这一族要同一批进（我 1 件 + §6 1 件 + yaml 1 件 + backend 那 3 个未跟踪/2 个改动），
    分批提交会临时造出 C8-13 或 roster 的红。
- **09-30 20:25 合批后复测轮（执行 Next action"等入册后复跑"，零代码）**：
  - **入库已确认**（不是转抄巡检的数）：`git log -1 -- core/internal/eventjournal/events.go` =
    **`2c65177`（19:57:03，"三处同改一次入库"）**；`git show HEAD:…/events.go | grep -c ExecutionUnknown` = **2**；
    §6 与 yaml 在 HEAD 里各 1 命中 ⇒ 三处这次同批齐；我三路径 `git status` 与 `git diff HEAD --numstat` **都空**。
  - **复跑读数**（20:25:47→20:29:08）：scoped 门禁 `-v` **`=== RUN` 49／49 PASS／0 FAIL／0 SKIP**、
    `gofmt`/`vet` 干净、两条三格判据 `C8-12`/`C8-13` **PASS（65/65/65）**、`./internal/...` **13 包 ok**。
  - **全仓剩的一枚红不是我面，我把因果查到断言行才敢这么说**：`./...` 里 `FAIL core/test`，
    唯一失败用例是 `TestEventProducerRosterMatchesDerivedSet`，它的 `t.Errorf` 在
    `core/test/event_producer_roster_test.go:1009`（"未决位点逐个点名并 FAIL"那条腿），点名对象是
    **`internal/execution/claim.go:69` 发射 `EventClaimed`**，而 `execution_claimed` 在
    §6（HEAD 与工作树）= **0**、yaml = **0** ⇒ 是 **backend 按 T-408 在途的那半未齐**
    （`claim.go`/`claim_test.go`/`turnpump_claim_test.go` 都还是未跟踪 `??`）。
    我的名字 `execution_unknown` 在同一份日志里被算进"有生产者"的宽覆盖清单（`recovery.go:174/:267`、
    `service.go:209` 三处都列到它）⇒ **不是我这一格遮住谁**。按巡检 20:2x 的提醒先分清归属、不动别人文件。
  - **HEAD 五分钟内换了三次**：`738cb13`(19:02:00) → `07d2163`(20:02:28) → `89b32b3`(20:27:55)，
    指纹相应换两枚：**19:26:27/`738cb13`/`6b5f3b01de843723b568f5e1fa3288f2`**（旧）与
    **20:29:08/`89b32b3`/`abffa276e877fa6b08da4b0f3d74d6d1`、未提交 17 处、编译输入 2381**（本轮）。
    两枚并列、旧的标"被取代"不删。
- **T-403（09-30 18:2x：登记 `execution_unknown`）两行已落，但我不交绿**（**这句是 18:34 交账当时的状态；
  19:27 收口见本节末条与 `## Current checkpoint`**）：
  - **改的就是派发稿点的那两处**：`events.go:48` 常量 `ExecutionUnknown = "execution_unknown"`
    （照 `:47 ExecutionInterrupted` 的形状），`catalog` 的 `:92` 加一枚 `ExecutionUnknown: true`
    （在 `ExecutionInterrupted: true` 后面）。**没有加 payload 表**——我读了 `:105`
    `func Registered(eventType string) bool { return catalog[eventType] }`，目录确实不校验载荷键。
    `git diff --numstat HEAD` 对我范围只有一件：**`2 1 core/internal/eventjournal/events.go`**（常量 +1、
    catalog 那行一改一删），gofmt 已跑（`-l` 空）。
  - **判据自带执行证明**（主控点名"别交 0 命中也 ok 的读数"）：`-v` 输出里 **`=== RUN` 计数 = 49 > 0**，
    与 `48 PASS + 1 FAIL = 49` 相互对得上 ⇒ 不是空匹配。
  - **红的那一条是我自己的守门，而且它点的是缺文不是代码**：
    `--- FAIL: TestCatalogMatchesContract`，原文
    `contract_parity_test.go:225: the catalogue registers [execution_unknown] which event contract §6 never registered`；
    `platform/db` 同批 `ok 3.018s`。同因的两条全仓判据（18:28:06 实测）：
    `C8-12：目录 64 类 / 常量 65 个；代码独有 [execution_unknown]` FAIL、
    `C8-13 三格计数：§6=64 / yaml=64 / 常量=65` FAIL。
    **成因我核到原文**：`grep -c execution_unknown specs/events/event-contract-v0.1.md` = **0**（18:26:19），
    而 契约① 有 3 处（`:717` 「REQ-RC-2 裁『甲』：`→UNKNOWN` 的 **§6 事件名定 `execution_unknown`**，
    payload `from` + `reason`，Audit 必带」、`:722` 盘上落点核对、`:725` Task 侧不动）
    ⇒ **裁定在 契约① 里、登记表那一行还没进 事件契约 §6**，而 T-258 刚裁过"§6 是唯一权威"（09-24 10:12 我
    自己还核过 契约① 只有 §0~§5）⇒ 这次是"先落码后补表"，与那条权威规矩的顺序反着走。
  - **功能上确实解锁了**（这点也报给主控，免得它被我的红掩盖）：
    `go test ./internal/execution/` → `ok 2.655s`，其中
    `TestMarkCrashUnknownLandsUnjudgedStateWithAudit` **`--- PASS (0.04s)`**（18:28:41），
    另一条 `TestMarkCrashUnknownStopsAtTheJournalCatalogue` **自己转成 SKIP** 并写明
    `recovery_unknown_test.go:361: 事件目录已含 execution_unknown ⇒ 本用例的墙没了；完整形状由 … 断`
    ⇒ backend 那两格按它说的"探针只认错误文本、不需人改测试"如期翻面，我没有为它改任何测试。
  - **我为什么不自己补 §6 让它变绿**：`specs/**` 不是我的可写面（§7 归属 + T-258 那条"第四处真相"的教训），
    而且**放宽自己的对拍**是这一格最坏的消红方式（会把"名字没登记"变成永远看不见）。所以停下等二选一。
  - **收口（T-407① 裁 (a)，19:25~19:27 我自己复跑，不转抄）**：§6 那一行由主控补进 `:219`
    （备文与我为它写的那行字面一致：`| \`execution_unknown\` | execution | \`from\` \`reason\` | ✓ |`）、
    yaml 那一枚由 frontend 于 19:25:03 落地（T-406）⇒ **我那两行保留、门禁全绿**：
    scoped `-v` = **`=== RUN` 49／49 PASS／0 FAIL／0 SKIP**（19:25:01），
    `C8-12 目录 65/常量 65`、`C8-13 三格相等 §6=65/yaml=65/常量=65` 双 PASS（19:25:22），
    `./internal/...` 13 ok、`./...` 18 ok／0 FAIL。
    指纹换过一枚（旧的那枚覆盖 18:28:44 的树、HEAD `6961df9`）：**19:26:27、HEAD `738cb13`、
    指纹 `6b5f3b01de843723b568f5e1fa3288f2`、未提交 25 处、编译输入 2377**。
    **一条口径记录**：主控 ③ 说"HEAD `53c926d` 是绿的"、巡检 19:24 说现读 `73c926d`、我 19:26:27 实读
    **`738cb13`（19:02:00）** 三者互不相同 ⇒ 我按"哈希只能抄自本轮命令"的规矩用自己的读数，
    谁被谁取代由主控记，我不改它们的行。
    **→ 19:31:12 我把三枚哈希按对象本身查了（不是转抄），并收回我 19:27 那句过强的话**：
    `git cat-file -t` → **`53c926d` = commit（有效）**，18:30:50「第一百四十六批」，
    且是 HEAD 的**直接父**（`git rev-list --count 53c926d..HEAD` = **1**）；
    **`73c926d` = fatal: Not a valid object**（巡检 19:30 已自纠：它拄错了 `git rev-parse` 的输出）；
    `738cb13` = commit（19:02:00，本轮 HEAD）。⇒ **主控那枚是真对象、只是时点被 19:02 那笔取代**，
    我"请核出处、别让它进判据链"那句**收回**；假的是巡检那枚而它自己报了。
    **"哈希只抄自本轮命令"这条规矩仍成立**，只是这次分歧的成因是**时点**、不是编造。另：19:24:17 我量 yaml 的 `execution_unknown` = 0、
    46 秒后（19:25:03）它变 1 —— **两条都真，差在落地那一刻**。
- **09-25 18:12 基线复测轮（执行 Next action，零代码零判据改动）**：
  - **读数全部当场取**（`date` 18:11:58 / 18:12:10→18:15:32 / 18:17:00）：scoped 门禁 `-v` **49 PASS／0 FAIL／0 SKIP**
    （`ok platform/db 2.599s`、`ok eventjournal 0.484s`、`agree on 64 event types`）、`gofmt -l` 空、
    `go vet` 两包 exit 0、`go build ./...` exit 0、`./internal/...` **13 包 ok**、`./...` **18 包 ok／0 FAIL**、
    `check_ownership.sh` exit 0"0 项越界"。
  - **REQ-DB-2 那条落地判据我自己复跑到 PASS**（不转抄主控的读数）：
    `LUCERO_REQ_DB2=1 … -run 'TestLifecycleRowLandsTheFiveColumns' ./cmd/core/` → `--- PASS (0.13s)`；
    **同一条不带 flag 是 `--- SKIP`**（18:14 两跑对照），所以我带 flag 也只跑契约点名的那**一条**、
    没有带 flag 跑全仓（那条纪律仍按 ① 第 17 条末段执行）。⇒ 三入口这条链在契约意义上已闭。
  - **"等主控入册"那一格已成立并关闭**（三路径 diff HEAD = 0 行、`2c56023` 09-25 17:47:36 收了那 +3 行注释）；
    头部五行（`Stage`/`Status`/`Last verified action`/`Next action`/`Verification`）与 `UNCOMMITTED` 同轮刷完，
    `Verification` 那行从一条命令扩成两条（把我本轮实际用来复测的落地判据也写进去，下次接手照跑）。
  - **一次"别人脚本的红"我按在途处理、没上报成缺陷**：18:13:5x 第一次跑 `check_ownership.sh` 打了
    `scripts/check_ownership.sh: line 1127: 结论: command not found`（exit 仍是 0、结论照打）；
    我没有据此判 test 的脚本坏，先做两件事：**读 `hdr()` 定义**（`:52` 是 `printf`，不构成那个错）＋
    **同一条命令复跑**（18:15:32 → `grep -c "command not found"` = **0**、exit 0），
    再对照该文件 mtime = **18:13:34**（我第一次跑前 20 秒）⇒ 判为 **bash 边读边执行撞上 test 正在改脚本**，
    不是脚本缺陷。**不进任何人的待办，只留这条读数**；若它们下次仍复现再由它们自己判。
- **T-276（头部五行 = 机器读的索引）已按新规矩执行，零代码改动**（10:54:23 收件 → 10:55:12 刷新完）：
  - **巡检量到的那两格确实是旧的，我核对后自己承认**：`Stage` 停在 T-219（其后还有 T-233/243、T-252、T-266 三批），
    `Last verified action` 写的是 **07:18:31→07:18:40**，而我最后一次真跑是 **10:02:43**（为 09:4x 新落的
    事件契约 §6 表头规矩重推守门那一次）⇒ 头部落后正文约 3.5 小时。**成因不是量法错，是"交账时只刷了
    `LAST_UPDATED` 与正文、没回头刷那五行"**——`LAST_UPDATED` 每次都新，所以头部自己看起来是新的。
    - 现在五行都刷了（时刻全部当场 `date` 实读）：`Stage` / `Status` / `Last verified action` / `Next action` /
    `Verification`（最后一行的命令本轮核过仍正确：就是 scoped 门禁那条），
    并新增 **`- 等（三问）`** 一行（等谁＝主控、等什么＝`versionless_test.go` 那 +3 行入册、谁解锁＝主控）；
    头部另加 `UNCOMMITTED:` 字段（原本就有，这轮补上"HEAD 内 0 命中"的实测读数）。
  - **顺手做了一次"头部瘦身"**：`Next action` 那格被我叠成了三层作废历史（T-243 划掉、判据成立划掉、yaml 那格作废），
    机器读到第一行就是作废句 ⇒ 按 T-276③ 的意思**只重排不删除**：活动项提到第一行，三层作废史与 T-233 留档
    降到子项，内容一字未丢（改完回读：以 `- Next action` 开头的行仍只有 1 处、`^## ` 段数仍 = 11）。
  - **主控那句辟谣我收下但不据此改账**：「`db.md` 头部带着 08:20 那次销账标注、巡检说它停在 T-219 偏严」——
    这是主控与巡检之间的口径差（那晚 08:20 我确实刷过销账标注，而 `Last verified action` 也确实是旧的），
    两边说的不是同一行，我不需要选边；**规矩落地按最严的那条**：以后每次交账五行一起刷（含 `Verification` 那行）。
  - **顺手抓到自己造了一次"第二份真相"**：我先把这条规矩写进了
    `memory/project-lucero-session-orchestration.md`，随后发现内存里已有专档
    `lucero-ledger-header-sync.md`（今晨主控那轮建的）⇒ **改回一行指针**，只把我这轮实测出的两条
    **加进那个单源**：④ `Next action` 第一行只能放活动项（作废史降为子项留档）、
    ⑤ 只刷 `LAST_UPDATED` 不算刷头部（它每轮都新，所以头部"看起来"新——正是我这次的成因）。
- **T-266（三句：一句划界 + REQ-T24 已裁 + 主控自己那格背书）已核，零代码改动**（10:02:17 收件）：
  - **① 划界我认**：`internal/adapter` 与 `adapter_instances` 的读出口归 adapter（T-263），C43 那句"只导出读接口"
    与我无关；**若今后 `db` 需要动那张表的读出口，我开 REQ，不顺手加**（这句按主控要求明写在账上）。
  - **② REQ-T24 的答复我这边不用重复问**：权威 = 事件契约 §6，**契约① 不进**（09:12 核过它只有 §0~§5，
    我 09:24:50 也自己 `grep -n '^## '` 复现过同一结论并记在 T-252 段）。
  - **②b 新表头规矩 → 我按"契约改口径要重推守门"重跑了自己的判据，不是默认没事**（关键：新 prose 落在
    `## 6.` 与表头之间，而我那条对拍就是解析这张表）：10:02:43 复跑 `./internal/eventjournal/...` →
    **5 条全 PASS**、原文仍是 `contract §6 and the catalogue agree on 64 event types`，
    `./internal/platform/...` `ok 2.831s`（同批 10:02:43）⇒ **规矩 ②（"必须先走 §6 展开约定 ①~⑤ 再比对"）
    正是我 `contractCatalog`/`eventTypesIn` 已有的做法**（含 ③b 与 token/去重双计数），新 prose 没进表行解析、
    也没改变计数；**守门与新规矩一致，无需改动**。
  - **②c 规矩 ①（引用事件名必须写完整名、不许省前缀）我扫了自己的范围**：
    `grep -rlE '`[a-z_]*`'` 型简写在 `core/internal/platform/db`、`core/internal/eventjournal`、`core/migrations` 里
    只有 **`contract_parity_test.go` 8 处命中**，且**全部是 `TestEventTypesInExpansion` 的合成反例输入**
    （形如 `` `execution_cancelled` / `_failed` / `_succeeded` ``——即 §6 允许写在合并行里的形状本身，是解析器的料，
    不是"拿简写引用一个事件名"）⇒ **除测试料之外 0 命中，规矩 ① 我这边本就合规**，不改注释。
  - **③ 主控那格（"二选一"里"真去扫契约①"不成立）由 test 与主控收口**，我不参与裁定；
    入库风险这一格**仍未关**（巡检 09:58:52 与我 09:47:11 同一读数：HEAD 里 `ExecutionStarting` = 0、工作区 = 2、
    `git log -1 -- …/events.go` = `753900f` 09-23 01:03:04），按红线① 我不提不催，
    Next action 里那条复测判据保留。
    **→ 10:10~10:11 已关（三时刻链：09:47:11 我读到 0 / 09:58:52 巡检读到 0 —— 两条当时都为真，
    悬树约 1 小时；入册发生在 10:03:46）**：我 10:11:49 自己重取，
    `git rev-parse --short HEAD` = **`a3b2910`（10:04:11）**、
    `git show HEAD:core/internal/eventjournal/events.go | grep -c ExecutionStarting` = **2**（工作区同为 2），
    `git log -1 -- …/events.go` = **`38cafa8` 10:03:46**（`snapshot: db 的 REQ-DB-4 常量入册…`）
    ⇒ Next action 那条"入册后应为 2"的判据**成立、划掉**，守门读数沿用 10:02:43 那次（不必重跑）。
    **但同一族还剩一格**：`core/internal/platform/db/versionless_test.go` 那 **+3 行注释仍未入库** ——
    10:11:49 实测 `git show HEAD:…versionless_test.go | grep -c fixture` = **0**（工作区 = 1）、
    `git status` = ` M core/internal/platform/db/versionless_test.go`、该文件最后一次提交仍是
    `1ad0c37`（07:10:24，早于我 07:52:15 那次注释编辑）⇒ **HEAD 里现在没有那三行**。
    它不含行为、不影响任何判据（HEAD 里那版测试自己就是绿的），但**"树里有一格 HEAD 里没有"这个形状**
    与刚被关掉的那格同源，我照实报、不自己入册（红线①）。
    **另记我自己刚犯的一次误读**：10:11:05 那次我把 `git status` 的输出读成"我范围全干净"，
    实际那一行 ` M versionless_test.go` 就在眼前——**换了量法（`git show HEAD:<文件> | grep -c <符号>`）才发现**。
    ⇒ 结论：判"某格在不在库里"用"读 HEAD 那份内容"，不用"扫 status 有没有我的行"。
- **T-252（REQ-DB-4：登记 `execution_starting`）已落，只动主控点的两格**（09:02:37 收件 → 09:05:49 复测）：
  - ① `core/internal/eventjournal/events.go:39` 加常量 `ExecutionStarting = "execution_starting"`
    （位置按 §7 序列放在 `ExecutionCreated` 与 `ExecutionStarted` 之间）；
    ② 同文件 `catalog`（`:87`）加 `ExecutionStarting: true`，与 `AggExecution` 那族现有写法同形。**其余一字未动**
    （`internal/eventjournal/` 里没有 payload struct 那一层，`grep -c struct events.go` = 0，所以没有第三件可改）。
  - **两边对拍都从"差一条"变成相等**：我包内 `TestCatalogMatchesContract` →
    `contract §6 and the catalogue agree on 64 event types` PASS；
    test 角色那条 `TestEventCatalogMatchesJournalConstants`（08:56:18 主控量到 FAIL）→ 09:05 实测 PASS，
    原文 `C8-12：目录 64 类 / 常量 64 个（扫 1 个非测试 .go）；目录独有 []、代码独有 []`。
  - **②那一条有见证（我专门变异验过，第一次锚点还是无效的）**：`gofmt -w` 之后 map 那行变成
    `ExecutionCreated:   true,`（三空格），我第一条变异用两空格锚点 ⇒ **`subs=0` = 根本没改，那次"全绿"不作证**；
    重做用真锚点（`subs=1`）把 `ExecutionStarting: true` 摘掉 →
    `--- FAIL: TestCatalogMatchesContract`（原文 `event contract §6 registers [execution_starting]
    but the catalogue has no entry/constant for them`）＝ **FIRED**。
    同一次变异里 C8-12 **仍然 PASS**（它只比"目录 ↔ 常量"，看不见 map）⇒ **"注册进 `Registered()` 集合"这一格
    的唯一见证就是我那条对拍**，别以为 C8-12 绿了就是两件都落。
  - **我这两件把红往前推了一格，不是消掉了**：`go test ./test/` 现在红在
    `TestEventTypeTripleEquality`（09:06:40 与 09:07:31 两次同点复现），原文
    `三格不相等 [§6→yaml]：事件契约 §6 目录里有 execution_starting，core-v1.yaml 的 EventType 枚举里没有` +
    `C8-13 三格计数：§6=64 / yaml=63 / 常量=64`。缺的那一格在 **`specs/openapi/core-v1.yaml:872`**
    （`execution_created, execution_started, …` 那一行没有 `execution_starting`），
    而 契约② §7:233 明写那份 yaml **由 `frontend` 首次生成并长期维护** ⇒ **不是我这一格能补的，我也没碰它**。
    另：`grep -c execution_starting specs/contracts-internal-v0.1.md` = **0**（09:02 实读），
    要不要在 契约① 也留一行由主控定（没有判据拿它比对，所以我这边不红）。
    **→ T-258（09:23）答：不进 契约①。** 我自己重读了顶层节（09:24:50）：`grep -n '^## '
    specs/contracts-internal-v0.1.md` 只有 **§0 依赖方向 / §1 db 平台层 / §2 Redaction / §3 审批与权限 /
    §4 跨模块读 / §5 待补** ⇒ 契约① 从来没有"事件目录"那一节，权威登记就是 事件契约 §6（三处命中）；
    **再抄进 契约① 就是第四处真相，而那正是 C8-13 要防的东西**。我账本里所有 "§6" 都指 事件契约 §6
    （09:25 把 T-179 段一处裸写的 "§6" 补成 "事件契约 §6"，消除歧义；纯文字、零行为）。
  - 交付时读数（09:05:49→09:06:09）：`gofmt -l` 空、`go vet` 两包 exit 0、
    scoped 门禁 `-v` → **49 PASS／0 FAIL／0 SKIP**、`go test ./internal/...` → **13 包 ok、FAIL 0**；
    信息性 `go test ./...` → 16 ok／5 行 FAIL（`./test/` 那一红 = 上面 C8-13；
    另一次 `internal/process` 的 FAIL 在 09:06:40 单独重跑是 `ok 4.833s` ⇒ adapter 在写、按"在途"处理、不进我结论）。
- **T-233 + adapter 07:42 的两处更正落在我的话上，不在代码上（复量：07:51:13 与 07:53:19）**：
  - **① 那个 `RUNNING` 值是主控正文里编的，我照抄了**：契约① 第 17 条那句"那一行还停在 `RUNNING`"
    07:4x 已就地改准成"**建行时写进去的那个状态**（v0.1 = `STARTING`）"并写明"那个值是我编的…
    **错在我这行，不在转述方**"（`grep -n 我编的 specs/contracts-internal-v0.1.md` = 第 345 行，1 命中，07:51:13 我亲读）。
    我从盘上复核：`supervisor.go:35` 定 `StatusStarting Status = "STARTING"`、`:420` 发
    `Event{Kind: EventStarted, PID: c.PID, Status: StatusStarting}`，而 `grep -rn StatusRunning --include=*.go core/`
    排除测试 = **1 命中且就是常量定义 `:36`** ⇒ 全仓没有事件携带 `RUNNING`，三方读数一致。
    **我犯的是一格更通用的错**：转述一个字面量时我只信了派发行里的引号，没去读发事件那一行——
    机制我读对了（guard 必须是"自己写进去的值"），值我没读。**已把这条记进记忆（禁编值）并改口**。
  - **② 我把两种错形合成了一种**："拿观察态当 guard ⇒ 第一次 0 行、`exit_code`/`exited_at` 全丢，
    **第二次还撞 `event_id` 的 UNIQUE**" —— 后半句错。按 `TouchTransition` 自己的顺序（0 行 ⇒ 直接
    `return (0, nil)`，**根本走不到记事件**），固定用观察态时每次都是干净的 0 行 + 一句喊，永远不会有第二次 UNIQUE；
    会撞 `event_id` UNIQUE 的是**另一种**写法：**guard 跟着状态前进**（落边后把 `curStatus` 改成 `ev.Status`，
    于是同一行第二次 exit 的 guard 正好命中自己 ⇒ 又改一次行、又记一条同 `event_id` 的事件）——
    adapter 07:1x 就是这么错的，它用变异 m3 复现过原文
    （`append event process_exited for proc-a v3: UNIQUE constraint failed: event_journal.event_id`，`adapter.md:663/:1510` 我 07:5x 读到原句）。
    **为什么这条值得单记**：我那句合成会让下一个人去找一个**不会发生的碰撞**，而不是真正的"第一次就 0 行"陷阱。
  - **代码零改动的自证**：`core/internal/platform/db/touch.go` mtime 仍是 **07:10:37**、
    `versionless_test.go` 只加了三行注释（说明"fixture 里那个 `RUNNING` 是测试自选数据，
    本平台层从不读写生命周期状态值"，防下一个人把它当契约值去"改正"）；
    改完末次重跑 07:52:21→07:52:27：`gofmt -l` 空、scoped 门禁 `-v` **49 `--- PASS`／0 FAIL／0 SKIP**、
    `ok platform/db 3.695s` + `ok eventjournal 0.678s`；
    **再跑一次作为交账读数（07:55:15→07:55:21）**：`go build ./...` exit 0、`go vet` 两包 exit 0、
    同一门禁 **49/0/0**，`ok platform/db 3.048s` + `ok eventjournal 0.640s`；
    HEAD 现取 **`885f16e`**，本角色范围 `git diff --stat HEAD` = **仅 `versionless_test.go +3`**
    （那三行就是上面那条注释），`touch.go` 与 `core/migrations/`、`internal/eventjournal/` 对 HEAD 零差异。
  - **T-243（08:20）口头销账 + 主控给的口径一句，我按它重测后落档**：口径是
    "**值的来源永远是盘上那个事件带出来的状态，不是任何人的转述**"。08:22:23 我自己重读产生点：
    `supervisor.go:35` `StatusStarting Status = "STARTING"`、`:420`
    `s.emit(Event{Kind: EventStarted, PID: c.PID, Status: StatusStarting})`，
    `grep -rn StatusRunning --include=*.go core/` 排除测试 = **1 命中且就是 `:36` 的常量定义** ⇒ 口径与盘上一致。
    那格转告**不必再发**（巡检 07:50 已覆盖，且"分开两种错形"本是 adapter 07:46 先纠的）
    ⇒ 我按"加作废注明、不删历史"把 Next action 划掉、正文转留档，**本角色账上无待办**。
    销账后复测一次（08:22:23→08:22:27，代码零改动）：scoped `-v` → **49 `--- PASS`／0 FAIL／0 SKIP**，
    `ok platform/db 2.689s` + `ok eventjournal 0.344s`；`git log -1 --format=%h` 现取 **`378004b`**
    —— **主控 08:19:50 引用的 `598c3f3` 到我 08:22:27 读时已被 `378004b` 取代**，
    与他自报的 `5f4f81c` 那次同类：**HEAD 只能现取、不能转抄**。
- **T-219（`AppendEventOnly` 那一格确实还开着）已偿清**：入口 ③ 今天只判"是不是无版本聚合"，
  于是 `AppendEventOnly(AggProcess, …)` 也放行 —— 而"事件落了、行还停在 `RUNNING`"正是契约① 第 12/13 条
  （**07:55 标作废：这句里的 `RUNNING` 是主控正文当时的错值，v0.1 实为 `STARTING`；洞的形状不变，值以第 20 条 ⑤ 为准**）
  那个洞的**反方向**。主控 05:32 写下这条、因巡检熔断挂了 25 小时，09-24 07:0x 直投过来。
  - **收紧**：`AppendEventOnly` 现在要求 `spec.table == noTable`，否则拒
    （原文 `append-event process: it owns table process_instances, so an event with no row move would
    desync them — use TouchTransition or CreateVersionless (契约 §1.1 item 17)`）。
    函数注释同步改掉那句 "or a fact observed without a row to move"，写成"只有 `core` 能走这里；
    '进程退了但库里没有它的行'是审计登记（`RecordDrop` 那一族），不是聚合事件"。
  - **双向判据**（派发点名"只有真数据能红的不算闭上"）：新 `TestAppendEventOnlyRefusesAggregatesThatOwnRows`
    两侧都断 —— `AggProcess` 必须报错**且**行与事件计数一字不动（回读 `status` 与 `eventLog`），
    `AggCore` 必须成功**且**事件真落（`1:core_ready`）。
  - **变异补两条、都是双向**：`n1` 把闸写成 `&& false`（`process` 被放过）→ 新用例红；
    `n2` 把条件反成 `== noTable`（谁都进不来）→ 新用例**与** `TestAppendEventOnlyNumbersCoreEvents` 一起红。
    加上 T-207 的 13 条，共 **15 条全 FIRED、0 SILENT**。
  - **哨兵按 T-219① 补、且只改名不动行为**：`const noTable = ""` + 注释点明"`core` 的 `table` 就是空的、这是设计"，
    四处比较（`versionlessTables` 反转 `:85`、`CreateVersionless:411`、`TouchTransition:461`、`AppendEventOnly:529`）
    统一用它；`noTable` 的值就是 `""`，所以这一格**没有**夹带任何行为改动 —— 变异与全量门禁都是在改名之后重跑的。
  - **一句要留给主控核对的（我没动别人范围）**：`AppendEventOnly` 的这道闸，契约① 第 17 条今天的正文只有
    代码注释那句「③ 没有表的聚合（`core`）：只落事件」，**没有**"只准给 `table == noTable` 的用、否则拒"
    这句规范文字（`grep -n "只准给\|绕过改行" specs/contracts-internal-v0.1.md` = 0 命中，07:12 实读）；
    而第 20 条 CO-44 段末尾把它称作"第 17 条末段对 `AppendEventOnly` 的收紧"⇒ **那半句还在裁定里、没进契约正文**。
    文本我备在 Coordination，贴不贴由主控定。
- **T-207（REQ-DB-2）已落地，按契约 §1.1 第 17 条 *05:14/05:18 更正后* 的文本做，不是按派发那版**：
  派发（05:07 之前的口径）只点了两个入口 + "显式声明无版本聚合"，而盘上契约在我动手期间被主控改过：
  ① 新增 `CreateVersionless`（建行 + 恰一条事件），② 并**作废**了"因为不进 `aggTables` 所以 `Append` 建行继续合法"
  那一句。我按盘上契约实现，adapter 在 05:22~05:26 的复量也独立记录了"`CreateVersionless` 在 `core/` 下 0 命中、
  建行那条腿还没有门"（`.qoder/progress/adapter.md` 第 12 轮 / CO-39 段），两边读数一致。
  - **注册表**：`AggProcess = "process"` / `AggCore = "core"` 两个 kind，**不进 `aggTable`**（因而也不进 `aggTables`）；
    另立 `versionlessAgg = map[AggregateKind]versionlessAggSpec{table, guards}`
    （`process → {process_instances, {status}}`、`core → {"", {}}`）。
    **`AggCore.table == ""` 是有意的，不是没填完的 TODO**（05:15 那问、05:22 巡检记"随 T-213 转告 db"，此处回话）：
    000001 冻结件里 Core 根本没有表，第 17 条 ③ 就是"只落事件"，所以空串 = "没有行可改"，
    `TouchTransition`/`CreateVersionless` 见到空表都拒绝并指向 `AppendEventOnly`，`guards` 空集，
    两条都有测试钉（`TestVersionlessAggregatesStayOutOfAggregateRegistry` 数 `len(guards)==0`；
    `core has no table` 两条拒绝用例）。
  - **版本号住在事件流里**：`nextEventVersion` = `SELECT count(*)+1 FROM event_journal WHERE aggregate_type=? AND aggregate_id=?`，
    在同一事务内取。并发算出同号 ⇒ 后提交者撞 `UNIQUE(aggregate_type, aggregate_id, aggregate_version)` ⇒ **整笔回滚**；
    `TestVersionlessEventConflictRollsBackTheWholeWrite` 用"预置一条 version=3 的事件造出空洞"把这个形状演出来，
    断的三件：错误里含 `UNIQUE`、`status` 仍是回滚前的 `RUNNING`（行改与事件一起退回）、回滚后写通道没被 parked
    （同一条边随后正常落成 `2:process_exited`）。
  - **`TouchTransition` 的 0 行**：SQL 固定 `UPDATE <table> SET <set> WHERE id = ? AND <guardCol> = ?`，
    `guardCol` 走 `versionlessAgg` 的逐表白名单（v0.1 只有 `process_instances:status`），
    **0 行 ⇒ 返回 `(0, nil)` 且不插事件**（重放不生成幽灵迁移）。
  - **`Append` 对 `process_instances` 关闭**：`versionlessTables`（由 `versionlessAgg` 反推）命中即拒，
    错误点名 `CreateVersionless`。契约那句"入口存在的意义就是把'两行拼一步'变成不可能"落成了机械闸；
    破坏面当场数清：全仓（含测试）`Append("process_instances"` 只有我自己那条断言，**无外部调用方**；
    `principals` 这类"合法无事件建行"的表仍走 `Append`，同一测里留了正向对照，防它变成"什么都红"。
  - **注册表形状断言（派发第 ④ 项）**：`process`/`core` 不在 `aggTable`、在 `versionlessAgg`，
    外加一条**schema 事实**（`process_instances` 至今没有 `version` 列，谁补列这条就红并写明"那就把它搬进 `aggTable`"）。
    变异 `m01`（顺手把 `AggProcess: "process_instances"` 加进 `aggTable`）→ **只有这一条红**，
    而这恰恰证明它必须存在：fixture 已改走 `CreateVersionless`，别的路径不会替它报警。
- **本轮自己撞到的三个"看起来绿其实没牙"（比实现更值得记）**：
  ① `AppendEventOnly` 的 `empty id` 用例是**哑的**——我在表里写了 case 名却仍传 `"core-a"` 进去，
    第一次跑就因为它没红我才发现（红的是"事件多了一条"）；修法是给用例带 `aggID` 字段。
  ② **"参数与 SET 列数对不上"这一类，驱动只帮你挡一半**：临时探针实测（`modernc.org/sqlite`，跑完即删，
    原文见 Verification evidence 的 T-207 段）——**多绑的参数被静默忽略**（`err=<nil>` 且 UPDATE 已经生效），
    只有**少绑**才报 `missing argument with index N`。所以"SET 里漏写一列而 args 还带着那一列的值"
    在 SQL 层**没有任何见证**，平台层那句 `len(cols) != len(args)` 才是承重的（变异 m12 摘掉它 ⇒ ① 那条立刻红）。
    配套改法：状态边的 SET 固定带一个**非 guard 的事实列**（`restart_policy`）并把它读回来断言，
    否则"漏一列"永远伪装成成功。第一版 fixture 的 SET 只有 `status`/`updated_at` 两列真实值，
    根本没有这种错位可测——那才是它当时"看起来绿"的真实原因。
  ③ 变异 `if len(columns) < 0` 那一次报 `[setup failed]`，**不是编译失败而是我 cwd 错了**：
    在 `.../platform/db` 目录里跑 `go test ./internal/platform/db/` ⇒
    `stat ...\internal\platform\db\internal\platform\db: directory not found`。
    从模块根 `core/` 重跑同一变异 → `--- FAIL: TestCreateVersionlessBooksRowAndEventTogether`（FIRED）。
    教训：`[setup failed]` 要先读它自己的那一行，别顺手记成 BUILD-BROKEN。
- **T-179 已补（对拍的重名盲区，一行变三处）**：`contractCatalog` 原先 `out[n] = true` 会把
  事件契约 §6 里出现两次的同一名字**静默吸收**，照样报 "agree on 63"。现在按 token 累计与去重数对拍：
  解析改为经**唯一累加点 `addNames(counts, cell)`** 计数，收尾用 `duplicated(counts)` 点名并 `t.Errorf`
  （附带 `tokens=N distinct=M` 两个数），只 Errorf 不 Fatalf，后面的差集照跑。
  - **新增 `TestDetectsDuplicateRegistration` 三条合成例，两侧都有**：① 合并格自己撞自己
    （`` `notification_created` / `_created` `` → tokens=2 distinct=1 → 点名）；
    ② 两行撞同一类型（`` `project_updated` `` + `` `project_created` / `_updated` `` → 点名）；
    ③ **正向对照**：全是不同名字的一行合并 + 一行全名 → tokens=5 distinct=5、必须**不**报。
    ③ 是防止我把它写成"什么都红"的假守门。
  - **一次自我发现的设计漏洞（比补的东西更值得记）**：第一版我把计数写在测试自己的循环里，
    与 `contractCatalog` 里的循环是**两份** ⇒ 把生产侧改成 `counts[n] = 1` 这种退化时，合成例根本看不见。
    抽出 `addNames` 让两侧共用同一个累加点之后，变异才测得出：
    ```
    --- FAIL: TestDetectsDuplicateRegistration
        merged cell expands onto a name the same row already lists: duplicated() = ""; want "notification_created"
        two rows list the same type: duplicated() = ""; want "project_updated"
    --- PASS: TestCatalogMatchesContract   contract §6 and the catalogue agree on 63 event types
    ```
    ⇒ 合成例抓机制、对拍守内容，分工正是主控要的那条；且**不需要等 `test` 的 C8-13**，包内这条已闭上。
    还原后 `grep T179MUT` **0 命中**。
  - 巡检三条附带确认：① 前提我自己在盘上复核过（`map[string]bool` + 那四个词 0 命中），非过期派发；
    ② 这是增量不是旧欠账，收到；③ "空变异"的提醒落地成上面的正向对照③。
- **T-161 / REQ-DB-7 第二半已闭（两行，无迁移）**：`events.go` 补 `ProjectUpdated = "project_updated"` 常量
  **并同步进 `true` 集合**（`ProjectCreated: true, ProjectUpdated: true, ProjectArchived: true`）。
  改前 `TestCatalogMatchesContract` 正红着，原文：
  `event contract §6 registers [project_updated] but the catalogue has no entry/constant for them`；
  改后 → `contract §6 and the catalogue agree on 63 event types`，常量 `grep` 复核 **63**。
  这条顺带证实 ③b 守门**没有误伤真表**：§6 第 167 行是
  `` `project_created` / `project_updated` / `project_archived` ``（"全部写全名"的合法形态），
  解析器正确提出 3 个名字、没报"形状坏了"。
  - **故意没加单类型用例**：`Registered("project_updated")` 已被 parity 覆盖，再写一条只是抄一遍断言；
    REQ-DB-5 那条专用用例留着是因为它还要钉 `notification_*` 三兄弟与近似拼法，这条没有同等额外面。
  - **上游半条不在我范围**：`specs/openapi/core-v1.yaml` 的 `EventType` 枚举仍缺 `project_updated`
    （巡检已报、归 frontend）；`PATCH /projects/{id}` 本体是 backend 的活 ——
    我这行只把"`Tx.Touch` 写不进去"那个硬阻塞拿掉。
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
  **（T-24/T-44/T-20 已把后两项做掉：`fs.FS` 入口收为包内 `migrateFS`、`Tx.QueryRowContext`/`QueryContext` 已在；
  仍开放的就只剩 `blobs` 那一件，且它没有消费方。）**
- 已知上限：本机 CGO 不可用 → `-race` 跑不了，并发用例只能证明"逻辑不产生重复版本"，
  不能证明无数据竞争。

## Blockers

- **T-403 一条（09-30 18:3x）⇒ 已解，留原文不删（T-407① 裁 (a)）**：~~`execution_unknown` 的两行已落，但
  事件契约 §6 还没有那一行 ⇒ 我包里的对拍 + 全仓 C8-12/C8-13 三条同因红~~
  主控 19:0x 补了 §6（`specs/events/event-contract-v0.1.md:219`，`Audit` 带 `✓`），frontend 19:25:03 补了 yaml 的
  `EventType` token ⇒ **19:25:01 我复跑：scoped 门禁 `=== RUN` 49／49 PASS／0 FAIL，C8-12 与 C8-13 双 PASS
  （§6=65 / yaml=65 / 常量=65）**。**本角色无阻塞**（20:25:47 复跑确认三处同改已由 `2c65177`（19:57:03）一次入库，连"等入册"也没有了）。
  两条我自排除的消红方式仍然成立、也没用上：不改 `specs/**`（归属不在我）、不放宽对拍（那会把"名字没登记"变成永远看不见）。

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
  **此条已闭（T-24 落 `fs.FS` 半边、T-44 裁"收为包内 `migrateFS`，导出面砍掉"）**：embed 那一半边仍在主控/backend，
  本包 `d.Migrate(dir)` 与内部 `migrateFS(fsys, dir, label)` 两条都在，拿哪一份 `fs.FS` 都无需再改平台层。
- **T-207 → adapter / backend（三个入口现在都在盘上，`supervisor.go:182` 那句"`CreateVersionless` 仍 0 命中"可以划掉了）**：
  `tx.CreateVersionless(AggProcess, id, columns, values, ev)` / `tx.TouchTransition(AggProcess, id, "status", from, setSQL, args, ev)` /
  `tx.AppendEventOnly(AggCore, id, ev)`。三条共同点：都走既有 `appendEvent`（`Registered()` + REQ-B-1 的
  `aggregate_type` 闸一并生效）、参数与 payload 都过 `scrubArgs`/`redactValue`（脱敏在密封之前）、
  版本号都是"该聚合已有事件数 + 1"。**`process_instances` 的建行腿从这一轮起不能再走 `Append`**（会直接被拒并点名
  `CreateVersionless`）；`id` 仍由调用方预生成 UUIDv7，平台层不铸号。
- **T-207 → 主控：两条未裁的语义我按"调用方的处置不改变平台层形状"实现，不替谁裁**
  - **CO-44**（`internal/process` 的账上第 11 轮提出）：`status` 有 6 个取值，而 `guardVal` 只能一个 ⇒
    "行还停在 `STARTING`（从没收到 start）"与"`EXITED` 已落过"在同一个 0 行里**不可区分**。
    平台层今天的答复是两种都给 `(0, nil)` 且不插事件，`TestTouchTransitionZeroRowsBooksNoEvent` 把**两种都钉住**
    （replay 一支 + 行根本不存在的一支）。若裁定要求可区分，最小改动是 `TouchTransition` 在 0 行时补一次
    `SELECT status`（同事务）并把区别放进返回值——那是**新签名**，等裁再动，不自扩。
  - **CO-45**：同一个 Supervisor 第二次拉起若仍用构造期那个 id，`CreateVersionless` 会撞
    `PRIMARY KEY`（`process_instances.id`）。平台层今天的表现是整笔回滚、不留孤儿事件
    （`TestCreateVersionlessBooksRowAndEventTogether` 末尾那条断言就是它）。(a) 一次尝试一行 / (b) 一个 Supervisor 一行
    两条路我这边都通：(a) 只要调用方带新 id，(b) 只要第二次起改走 `TouchTransition("FAILED" → "RUNNING")`。
- **T-207 回答 05:15 那问（巡检记"随 T-213 转告 db"）**：`versionlessAgg` 里 `AggCore: {table: ""}` **是有意设计**，
  不是没填完的 TODO —— Core 在冻结的 000001 里没有表，第 17 条 ③ 就是"只落事件"。空串的语义是"没有行可改"，
  所以 `CreateVersionless`/`TouchTransition` 见到它都拒并指向 `AppendEventOnly`，`guards` 是空集。
  今天已落进代码注释（`versionlessAggSpec` 上方）与两条测试，不必再挂问号。
- **T-219 → adapter（请主控转告，并写回你那行疑问里，别让它再落在我自己账本里）**：
  `AppendEventOnly` 已收紧为**只接受 `table == noTable` 的聚合**，也就是只有 `core`；对 `AggProcess` 现在会拒。
  我 07:13 实读过你 `internal/process/lifecycle.go`（未跟踪，mtime 07:11:32）：建行走 `CreateVersionless:126`、
  状态边走 `TouchTransition:199`，**没有一处调 `AppendEventOnly(AggProcess, …)`** ⇒ 这次收紧不打断你。
  若你后面需要"观察到退出但库里没有那一行"，按契约第 20 条 CO-44① 走**审计登记**（`RecordDrop` 那一族），
  不要拿聚合事件补。
- **T-219 → 主控：契约正文缺那一小句**（不是要你改实现，是实现比正文严的那一格该补文）。
  第 17 条正文目前只有代码注释「③ 没有表的聚合（`core`）：只落事件」；第 20 条 CO-44 段把它称为
  "第 17 条末段对 `AppendEventOnly` 的收紧"。建议贴在 §1.1 第 17 条那个 go 代码块之后：
  ```markdown
  **入口 ③ `AppendEventOnly` 只准 `versionlessAgg` 里 `table == noTable`（即 `core`）的聚合用**：
  有行的聚合走这里 = "事件落了、行没动"，是第 12/13 条那个"行改了、事件丢了"的**反方向**，
  平台层直接拒。"观察到退出但库里没有那一行"走审计登记（第 20 条 CO-44①），不许洗成聚合事件。
  ```
  （按第 14 条用符号引用、不写行号；`noTable` 是我包里的常量名，契约里要不要出现这个名字你定。）
- **T-219 → 主控，一条证据形状的提醒（我没有要谁改，只是这一格今后会反复坑人）**：
  `1ad0c37`（07:10:24，消息写的是"09-23 05:54 收工态入册"）把我**当时正在写**的 `touch.go` 与
  `versionless_test.go` 一并入了册——我那次收紧落盘约 07:09、门禁重跑在 07:10:54，**提交发生在中间**。
  后果：worker 交账里"`git diff --stat HEAD` = 我的范围干净"这句话在这一刻**失去含义**（空 diff 不再是"我没碰"，
  而是"我的东西已在 HEAD 里"）。我已在 Current checkpoint 改成用 `git diff 97d0a21 HEAD` + 逐标记命中数框改动面。
  建议以后 snapshot 前扫一眼各角色 mtime 在 5 分钟内的文件，或者把这类提交写成
  `snapshot: 含 db 07:0x 在制品`——**日期写错的那半句我按"记账错"处理，不追究**（`1ad0c37` 标的是 09-23 05:54，
  实际提交 09-24 07:10:24）。
- **CO-44/CO-45 裁定已收到（契约① 第 20 条）——本角色零改动**：CO-44③ 否决了我提的"0 行时补一次
  `SELECT status` 并把区别放进返回值"，平台层签名不动，今天的形状（两种都给 `(0, nil)`、不插事件、
  双向用例）就是裁定要的；CO-45 裁 (a) 一次尝试一行 ⇒ `CreateVersionless` 的 `id` 仍由调用方预生成、
  平台层不铸号（本来就这样），`metadata_json` 记 attempt 也不落在我这层。
- **T-252 → frontend + 主控（NEEDS YOU，一个 token，我不代改）**：`execution_starting` 已进 事件契约 §6 与
  `internal/eventjournal`（常量 + `Registered()` 集合；09:05:49 两边对拍 64/64 相等），
  但 **`specs/openapi/core-v1.yaml:872` 的 `EventType` 枚举还没有它** ⇒ `./test/` 的
  `TestEventTypeTripleEquality` 现在红在这里（原文 `C8-13 三格计数：§6=64 / yaml=63 / 常量=64`，
  09:06:40 与 09:07:31 两次同点复现）。契约② §7:233 写明那份 yaml 归 `frontend` 长期维护
  ⇒ **补一个 token 即三格齐**；我没碰 yaml、也没改判据去绕过它。
  另提请主控：`grep -c execution_starting specs/contracts-internal-v0.1.md` = **0**（09:02 实读）
  ——没有判据拿 契约① 比对所以不红，要不要补一行由你定。
  **（T-258 09:23 已答：不补，契约① 没有事件目录那一节，抄进去就是第四处真相 ⇒ 本条问句作废，
  `specs/` 我不动。）**
  **（同批 T-256 已把那一 token 派给 `frontend`；巡检 09:22:09 实量 `grep -c execution_starting
  specs/openapi/core-v1.yaml` 仍 = 0 ⇒ 这一格现在等 frontend 交账，不由我跑。）**
  **→ 09:26:42 我自己复量，那一格已经到了**：`core-v1.yaml` mtime **09:24:45**、`:872` 现在写着
  `execution_created, execution_starting, execution_started, …`、`grep -c execution_starting` = **1**，
  于是 `./test/` 两条一起转绿：`C8-13 三格相等：§6=64 类、yaml=64 条、常量=64 个；四条边全空` PASS、
  `C8-12：目录 64 类 / 常量 64 个` PASS；我两包同时 `ok eventjournal 0.573s` / `ok platform/db 2.967s`（09:26:47）。
  **注意归属与状态（两时刻都带，按 T-220 的规矩）**：yaml 那一行是 **frontend 按 T-256 自己动手的交付**，
  我 09:26 读到的是它**未提交**的工作树改动（`git status` = `M specs/openapi/core-v1.yaml`）；
  **09:44:17 复量已入库** —— 现取 HEAD = **`b47c14c`（09:35:24，`snapshot: frontend 的 T-256 交付入册…`）**，
  `git show HEAD:specs/openapi/core-v1.yaml | grep -c execution_starting` = **1**，工作树同一文件干净。
  ⇒ 我 09:26 那句"仍未入库/不替它宣布合验"当时为真、现已过期，按"标过期不删除"就地补这段。
  **T-260 的更正落不到我账上**：主控说"你账上那行'主控 09:24:45 补的'请改成'frontend 交付'"——
  我全文只有一处写这个归属（本节 + Next action 各一次），两处**原文都写的是 frontend**：
  "`specs/openapi/core-v1.yaml` 我没碰、那是 frontend 的未提交改动"（09:26 写下），没有一句把改动记到主控头上；
  所以我**没有可改的错句**，改的是那半格**状态**（未提交 → 已入册 `b47c14c`）。
  成因我认同主控自认的那一半：**从"最后一次写的时间"推"是谁在写"**——`mtime 09:24:45` 只证它被写过，
  不证谁写的（我记忆里那条老规矩的第三次复现）。**我这边补一条动作**：今后引用别人的改动只写
  "盘上现读是 X（路径+行号+时刻）"，**归属要么查 `git log` 要么标"未证"**，不从 mtime 推。
  - **顺手量到一格只有主控能关（09:47:11 读数，我只报不办）**：`b47c14c` 把 frontend 那半边入册了，
    而我那半边还在工作树里 —— `git show HEAD:core/internal/eventjournal/events.go | grep -c ExecutionStarting` =
    **0**（工作区 = 2），三份 `execution_starting` 计数在 HEAD 是 **§6=3 / yaml=1 / events.go=0**，
    工作区才是 3/1/2。⇒ **干净检出 HEAD 时 C8-13 是 64/64/63 的红**，正是它自己那条提交信息里写的
    "红已在盘上闭、却未入库这一格随时可能被别人的下一批覆盖掉"——**现在被覆盖的风险还留在我这一格上**。
    我无 git 写权限（红线①），**只能把它报给主控按角色入册**；我不因此重跑或改动任何东西。
- **T-403 → 主控（~~NEEDS YOU，二选一~~ 已裁 (a)、本条转留档，19:27 标）**：`execution_unknown` 的**裁定已进 契约①**
  （`:717` REQ-RC-2 裁「甲」）但**登记表那一行还没进 事件契约 §6**，而我包里的对拍只认 §6（T-258 你裁过
  "§6 是唯一权威"）⇒ 我那两行落下去之后 `TestCatalogMatchesContract` + 全仓 C8-12/C8-13 三条同因红，
  且都点名同一条边。**要转绿只需你在 §6 表格补这一行**（格式照 216 行那种带 `✓` 的写法，
  `Audit` 必带的依据是 契约① 第 25 条 ④-a 那句"payload `from` + `reason`，Audit 必带"）：
  ```markdown
  | `execution_unknown` | execution | `from` `reason` | ✓ |
  ```
  位置建议紧接 212 行那条 `execution_starting`（同族、都是 execution 聚合的状态边事件）。
  **补完之后 frontend 还要在 `specs/openapi/core-v1.yaml` 的 `EventType` 枚举加一个 token**，否则 C8-13
  会从"常量多一条"变成"yaml 少一条"（这条链我 09-24 已经量过一次）。
  若你选**「先立文后落码」要我先退这两行**，回一句即可，我一条 `Edit` 撤回、门禁立刻回 49/49。

## Changed files

- `core/internal/eventjournal/events.go`（T-252 改，+4/-1）— 常量 `ExecutionStarting` 进 `events.go:39`、`catalog`（`:87`）加一条；
- `core/internal/eventjournal/events.go`（T-403 改，`git diff HEAD --numstat` = 2 加 1 删）— 常量
  `ExecutionUnknown = "execution_unknown"`（`:48`，照 `:47` 形状）+ `catalog` 的 `:92` 一枚 `true`；
  **未加 payload 表**（`:105 Registered()` 只查 `catalog[ev]`，目录不校验载荷键）；T-407① 裁 (a) 后保留不撤回
  只为 REQ-DB-4 登记的 `execution_starting`（事件契约 §6 第 202 行），其余一字未动
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
- `core/internal/platform/db/touch.go`（T-207 + T-219 改，`97d0a21..HEAD` = +210）— `AggProcess`/`AggCore`、
  `versionlessAgg` + `versionlessTables` + `noTable` 哨兵、`CreateVersionless` / `TouchTransition` /
  `AppendEventOnly`（③ 只准 `table == noTable`）/ `nextEventVersion`、
  `Append` 对非版本聚合表关闭、`aggregateTable` 的指向性错误
- `core/internal/platform/db/versionless_test.go`（T-207 新建、T-219 增一条，`97d0a21..HEAD` = +572 新档）— 9 条：
  注册表形状、① 行与事件同生同死、② 每条边恰一事件、② 0 行不插事件、②③ 白名单外一律拒、
  ③ core 逐聚合编号、**③ 只准无表聚合（双向）**、UNIQUE 冲突整笔回滚、脱敏在密封前
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

- **T-207（REQ-DB-2 三个入口）实测**（`date` 实读 05:47:29 后收尾重跑；下列计数都出自最后那一次）：
  - **scoped 门禁 `-v`**：`go test -v -count=1 -timeout 300s ./internal/platform/... ./internal/eventjournal/...`
    → `gate_exit=0`、**`--- PASS` 48 条（顶层 48 / 嵌套 0）、`--- FAIL` 0、`--- SKIP` 0**，
    `ok platform/db 3.118s` + `ok eventjournal 0.799s`。**"全"= 这两个包**（`go list` 确认
    `./internal/platform/...` 只含 `platform/db` 一个包）；本角色范围内没有跳过任何包、没有 SKIP 用例。
    `gofmt -l` 两个目录都是空输出；`go vet` 两包 exit 0；`go build ./...` exit 0。
  - **本轮新增 8 条逐条**：
    ```
    --- PASS: TestVersionlessAggregatesStayOutOfAggregateRegistry (0.03s)
    --- PASS: TestCreateVersionlessBooksRowAndEventTogether       (0.03s)
    --- PASS: TestTouchTransitionBooksOneEventPerEdge             (0.03s)
    --- PASS: TestTouchTransitionZeroRowsBooksNoEvent             (0.03s)
    --- PASS: TestTouchTransitionRefusesOutsideItsWhitelist       (0.04s)
    --- PASS: TestAppendEventOnlyNumbersCoreEvents                (0.03s)
    --- PASS: TestVersionlessEventConflictRollsBackTheWholeWrite  (0.03s)
    --- PASS: TestTouchTransitionRedactsBeforeSeal                (0.03s)
    ```
  - **拒判的原文（`t.Logf` 打出来的，不是我照格式串推的）**：
    ```
    append to process_instances: these rows carry a lifecycle event, so they go through CreateVersionless (契约 §1.1 item 17)
    create-versionless task: not a versionless aggregate (see 契约 §1.1 item 17)
    create-versionless core: this aggregate has no table, use AppendEventOnly
    touch-transition process: restart_policy is not a guardable column on process_instances
    touch-transition: id is the row key, not a settable column
    column version belongs to the platform, not to a caller
    event type "process_restarting" is not in the event journal catalogue
    create-versionless process proc-cv: constraint failed: UNIQUE constraint failed: process_instances.id (1555)
    append event process_exited for proc-4 v3: constraint failed:
      UNIQUE constraint failed: event_journal.aggregate_type, event_journal.aggregate_id, event_journal.aggregate_version (2067)
    ```
    最后一条就是第 17 条要的"后提交者整笔回滚"：断言 `status` 仍是回滚前的 `RUNNING`、
    预置那条 version=3 的事件也一起消失（同一事务），且回滚后单写通道没被 parked
    （同一条边随后落成 `2:process_exited`）。
  - **一次性探针（05:4x，跑完即删，不在交付面上）**：想记"绑定参数与占位符数不一致会怎样"，
    临时建 `argcount_probe_test.go` 打在真库上，原文：
    ```
    more args than placeholders:  err=<nil>                          status=EXITED
    fewer args than placeholders: err=missing argument with index 4  status=RUNNING
    guard alone, no id in args:   err=missing argument with index 3  status=RUNNING
    ```
    ⇒ 结论按字面收窄：**多绑的静默忽略、少绑的才报错**，所以 `len(cols) != len(args)` 那类 Go 侧检查是承重的。
    探针文件同轮 `rm`（`ls` 确认不存在）后重跑整包 → `ok platform/db 3.116s`。
  - **变异判决 13 条，全 FIRED、0 SILENT**（每条：备份 → 改 → 跑整包 → 还原，同一命令内完成；
    收尾 `md5sum -c` 与 `grep -Fc <marker>` = 0 双重自证）：
    | 变异 | 红的是 |
    | --- | --- |
    | m01 `AggProcess` 加进 `aggTable` | 注册表形状那条（且只有它 ⇒ 证明它必须存在） |
    | m02 `AppendEventOnly` 的 empty-id 改成 `return nil` | ③ 编号那条 |
    | m03a `count(*)+1` → `max(aggregate_version)+1` | 7 条一起红 |
    | m03b 数号不看 `aggregate_id` | ③ 编号那条（"另一个 core 从 1 起"） |
    | m04 0 行分支改成返回 error | 0 行不插事件那条 |
    | m05 `guardCol` 白名单 `&& false` | 白名单外一律拒那条 |
    | m06 ② 绕过 `scrubArgs` | 脱敏那条 |
    | m07 `aggregateTable` 退回 `unknown aggregate` | 注册表形状那条（指向性错误） |
    | m08 `if changed == 0` → `< 0`（改了行也算 0、事件照插） | 0 行不插事件那条 |
    | m09 `Append` 的版本聚合闸 `&& false` | 注册表形状那条（`Append` 关闭 + `principals` 正向对照） |
    | m10 ① 不插事件 | 6 条红（含 ① 与全部用 ① 建行的 fixture） |
    | m11 ① 绕过 `scrubArgs` | 脱敏那条 |
    | m12 ① 的列/值长度校验 `&& false` | ① 同生同死那条 |
  - **注册表变更的规定动作（§1.1 第 10 条）**：`go test ./internal/... -count=1` → **13 包 ok、`^FAIL` 0**；
    新晋被禁的写点全仓 grep（含测试）：`grep -rn --include=*.go 'Append(\s*"process_instances"' .` →
    **1 命中，且就是我自己那条断言**（`versionless_test.go:155`）⇒ 无外部调用方被打断；
    `internal/process`、`cmd/core` 只余注释与读操作（`cmd/core/lifecyclecolumns_test.go:137` 是它自己的
    裸 `INSERT` 夹具，不经 `Append`，不受影响）。
  - **信息性全仓**：`go test ./... -count=1` → 18 包 ok、`^FAIL` 0、`no test files` 0；
    **未带 `LUCERO_REQ_DB2=1`**（按契约那句"落地之前不要顺手带 flag 跑全仓"），
    实读 `cmd/core` 默认跑仍是 `--- SKIP: TestLifecycleRowLandsTheFiveColumns`，我没有为变绿去动它。
  - **越界自检**：`bash scripts/check_ownership.sh` → exit 0、结论行"本次扫描 0 项越界"，
    其中依赖方向那一节确认 `platform/db: 仅 internal/eventjournal`。
  - **范围自证**：基线现取 `git log -1 --format=%h` = **`97d0a21`**（本轮内主控又落了一次盘，`f841335` → `97d0a21`），
    `git diff --stat HEAD -- core/internal/platform/db core/internal/eventjournal core/migrations` →
    **只有 `touch.go` 一条：+194 / −1**；`versionless_test.go` 是未跟踪新档；
    `core/migrations/`、`internal/eventjournal/` 零 diff。
    diff 只能证明"我没碰别的文件"，不证明"我没弄坏别人"——后一句靠上面那 13 包全绿。

- **T-219（`AppendEventOnly` 收紧 + `noTable` 哨兵）实测**（计数出自 `date` 实读的 07:10:54 与末次 07:18:31 两轮，读数一致：49 PASS / 0 FAIL / 0 SKIP）：
  - scoped 门禁 `-v` → **49 条 `--- PASS`（顶层 49 / 嵌套 0）、0 FAIL、0 SKIP**，
    `ok platform/db 2.568s` + `ok eventjournal 0.447s`；`gofmt -l` 空、`go build ./...` exit 0、`go vet` 两包 exit 0；
    `go test ./internal/...` → **13 包 ok、FAIL 0**；信息性 `go test ./...` → 18 包 ok（未带 `LUCERO_REQ_DB2=1`）；
    `bash scripts/check_ownership.sh`（07:12:06）→ exit 0"本次扫描 0 项越界"。
  - 新用例两侧都跑到了，拒判原文（`t.Logf` 打出来的，不是我照格式串推的）：
    ```
    --- PASS: TestAppendEventOnlyRefusesAggregatesThatOwnRows
    refused as expected: append-event process: it owns table process_instances, so an event with no row
      move would desync them — use TouchTransition or CreateVersionless (契约 §1.1 item 17)
    ```
    正向那一侧同时要求事件真落：`core-gate events = [1:core_ready]`。
  - **两条新变异（双向，对应"只有真数据能红的不算闭上"）**：
    | 变异 | 判决 | 红的是 |
    | --- | --- | --- |
    | `n1` 闸写成 `spec.table != noTable && false`（`process` 被放过） | FIRED | `TestAppendEventOnlyRefusesAggregatesThatOwnRows` |
    | `n2` 条件反成 `== noTable`（谁都进不来） | FIRED | 同上 **+** `TestAppendEventOnlyNumbersCoreEvents` |
    两条都 `subs=1`、跑整包、还原后 `md5sum -c` 与改前一致，且 `grep -Fc 'noTable && false'` = 0；
    `spec.table == noTable` 的 2 处命中是本来就该有的挡（`CreateVersionless`/`TouchTransition`）。
    连同 T-207 的 13 条：**15 条全 FIRED、0 SILENT**。
  - **收紧的破坏面当场数清**：`grep -rn --include=*.go 'AppendEventOnly(' .` 排除我自己包内 = **0 命中**；
    adapter 在写的 `internal/process/lifecycle.go`（未跟踪、mtime 07:11:32）只用
    `CreateVersionless:126` 与 `TouchTransition:199` ⇒ 这一格收紧不打断任何在飞的工作。
  - **收紧之后又复量一次（07:17:20，为的是不把"我绿的"当成"合起来绿"）**：
    `go test -count=1 ./internal/...` → **13 包全 ok**，其中 `internal/process 5.460s` 就是 adapter 那对
    未跟踪新档（`lifecycle.go`/`lifecycle_test.go`，07:11:32 / 07:13:20）所在的包
    ⇒ 我的 `AppendEventOnly` 闸与他们的写口**同盘并存是绿的**，不是各绿各的。
  - **范围自证换了写法**（原因见 Coordination 那条 snapshot）：基线现取 `34074a0`，本角色范围 `git diff HEAD`
    **为空并不表示"我没改"**——`1ad0c37`（07:10:24）把在制品入了册。改由
    `git diff --stat 97d0a21 HEAD -- core/internal/platform/db core/internal/eventjournal core/migrations`
    框出：`touch.go +210` / `versionless_test.go +572`，**2 files changed, 781 insertions(+), 1 deletion(-)**；
    `it owns table`/`noTable`/`RefusesAggregatesThatOwnRows`/`CreateVersionless` 四个标记在 `97d0a21` 各 0 命中
    （那里还没有 `versionless_test.go` 这个文件），在 HEAD 分别 1 / 4 / 1 / ≥1 命中；
    `core/migrations/`、`internal/eventjournal/` 在 `97d0a21..HEAD` 零改动；工作树的未跟踪件里没有我的东西
    （只有 `internal/process/lifecycle{,_test}.go`，adapter 的）。
  - **契约正文差半句**：`grep -n "只准给\|绕过改行" specs/contracts-internal-v0.1.md` → **0 命中**（07:12 实读）
    ⇒ 第 20 条引用的"第 17 条末段对 `AppendEventOnly` 的收紧"目前只在裁定与代码里；文本备在 Coordination，贴否由主控定。
    **（T-226 已闭合，两时刻都带上：07:12 我量到 0 命中为真，主控 07:2x 把规范句补进第 17 条。）**
    07:31:37 复量：`grep -c 只准给 specs/contracts-internal-v0.1.md` = **1**，原文在第 343 行
    「**`AppendEventOnly` 只准给"没有表的聚合"用**（v0.1 即 `table == ""` 的那一个：`core`）」，
    且随句带了"配套判据双向"与"这一格不进事件流、走 `RecordDrop` 那一族审计登记"；
    `git show HEAD:specs/contracts-internal-v0.1.md | grep -c 只准给` = **1**（07:32:15）⇒ 该句已在 HEAD `319f00d` 里
    （巡检 07:27 那次量的"+15 行未提交"在它那一刻为真，之后被 07:27:52 的提交追上了）。
    **我的实现那三行（`spec.table != noTable` + 拒判文案 + 注释禁令）与新句逐条对齐，未回改。**
  - **T-226 只要求回一句确认，代码零改动**；我仍按自己的规矩把三件读数自己重取了一遍（07:31:37）：
    `or a fact observed without a row to move` 在 `core/internal/platform/db/` **0 命中**、
    `TestAppendEventOnlyRefusesAggregatesThatOwnRows` 在 `core/` 内 **1 处**（定义即用例本体），
    并末次重跑 scoped 门禁（07:32:00→07:32:05）→ `go build ./...` exit 0、
    **49 `--- PASS`／0 FAIL／0 SKIP**，`ok platform/db 2.924s`、`ok eventjournal 0.617s`；
    我范围两个文件 mtime 仍是 07:10:37 / 07:10:04 ⇒ 这轮没有新代码，确认建立在这次的读数上而不是旧结论上。

## Recovery note

- Interruption point: db-platform 全部判据 + T-6/T-7/T-12/T-20/T-24/T-27/T-33/T-44/T-48/T-57/T-60/T-66/T-72/
  T-107/T-115/T-125/T-136/T-142/T-149/T-161/T-179/T-219 均已落地实测；本角色范围内无未完事项。
  开放项全在别人手里：REQ-A4（`go:embed` 归属，主控拍板）、Q6（audit 收窄 `IsVaultRef` 注释）、
  C34 词表的落地（backend/adapter），以及主控自己要不要把 CO-44/CO-45 裁成"平台层要多返回一格"
- First action after recovery: 跑 scoped 门禁 `go test ./internal/platform/... ./internal/eventjournal/...`
  确认仍绿；`adapter`/`backend` 一旦接上三个入口，`LUCERO_REQ_DB2=1 go test -run TestLifecycleRowLandsTheFiveColumns ./cmd/core/`
  那一条该由他们跑到 PASS（本角色不带 flag 跑，也不为它改实现）
- 历史包袱提醒：Q4 已把 `AggQuota` 定为 `"quota"`，若谁又报"未知聚合"，先查它是否还在写旧字符串
  `"quota_account"`

OWNER_ROLE: adapter
TASK_ID: adapter-protocol
STATUS: in_progress
SCOPE: Core↔Adapter 协议实现（帧编解码/握手/capability/三态/流式/额度）+ 子进程监管 + FakeAdapter 本体；不改 specs/docs，不碰其它角色的包
EVIDENCE_DIR: .qoder/progress/adapter.md（本轮为纯单测证据，不落 .qoder/evidence/）
SCRATCH_DIR: D:\Project\Lucero\.scratch\adapter\（T-47：一次性产物只准落这里，仓库已 ignore；
             永不碰 /tmp、%TEMP%、$env:TEMP 的通配删除；只删自己建的具体文件名）
LAST_UPDATED: 2026-10-01T19:45:12+08:00  # 本机 clock 实读（第 38 轮：T-441 把 §4 的第七条 Adapter→Core 通知 approval/request 落到线上；11 枚变异 11/11 FIRED、A 面 459 PASS/6 SKIP/0 FAIL；读数取于 HEAD=17ca538；**本轮 9 件未入册，第 35/36/37 三批现读已随 `d060579`/`3ff8271` 入册**——上一版这行尾句写的是「四批都没入册」，那是我没复量就说的一句话，`git status --porcelain` 对那五件为空即已推翻，改在此处并注明）
   # 自纠两处（T-43）：① 本行上一度写成 `14:36:20`，那**不是** `date` 读出来的、是估的（当时最后一次实读是
   # 14:31:49）；② 09-21 11:11 我用一条 `\S+` 的正则改这行，把 `LAST_UPDATED:` 前缀 itself 吃掉了，
   # 当场用 Edit 修回 —— 记账本自己的教训：**改标头行必须锚全文，不能用 `^KEY: \S+` 这种吞前缀的写法**。
   # 作废值不重写进历史，只在此说明；14:37:11 与 14:26:09（交付跑）均为 `date` 实读。

## Timestamps（T-43）

本节 09-19 与本轮之前的多数时间戳是在没有当次 `date` 的情况下写的（含被巡检点名的
`LAST_UPDATED: 09-19T18:22` 对 `mtime 09-20 12:10` 这一对），**一律作废，不重写**：
它们是当时的估计值，不是测量值。此后规则：写具体时刻前先跑 `date`，没跑就写"未计时"。
证据性时间戳（`Last verified action`、Verification evidence 里的重跑时刻）从 09-20 12:57 起均为 `date` 实读。

## Objective

按已 accepted 的 `specs/adapter-protocol/adapter-protocol-v0.1.md` 落地 Core 侧协议层与
Process Supervisor，并用 FakeAdapter 的 11 个方法 + 4 种故障注入跑通契约测试（契约 §14），
交回主控时带可重跑的绿灯命令。

## Completion criteria

- [ ] `internal/adapter`：ndjson 帧编解码 + `adapter/initialize` 握手（major 不同即拒）+ capability 校验 + 三态投递 + 流式 + 额度 UNKNOWN，表驱动测试覆盖
      → 除 §5 的 `session/send` **请求参数面**（`delivery` 字段与结果形状，随派发层一起做）外全部已落；见 Completed
- [ ] `internal/process`：Supervisor 拉起/登记/退避重启 `1s,2s,4s,8s,≤30s`/连续 5 次崩溃→UNAVAILABLE+停重启
- [ ] Adapter 崩溃回归：Core 仍存活、该 in-flight **Execution**（状态值，非 Task）转 `UNKNOWN`（非 `FAILED`）、其它 Adapter 不受影响
- [x] 未握手的任何方法 → `PROTOCOL_NOT_INITIALIZED`（`TestCallGate` 4 行 + 变异检验）
- [ ] 重复帧按 `sequence_no` 幂等丢弃（已做，`TestCursor`/`TestCursorResetOnRestart`）；`stream/delta` 不进 event_journal
      → 包内已由 `purity_test.go` 结构钉住（零 DB/事件依赖），跨库断言仍 blocked（B-a3）
- [ ] `core/cmd/fake-adapter/`：11 方法 + `debug/fail|crash|hang|unknown_quota`，`debug.*` 仅 `adapter_key=fake` 可声明
      → 落点由 CO-1 裁定（2026-09-19，契约 §10）；`adapters/fake/` 只放 fixture
- [ ] 危险开关白名单在参数面生效（命中即拒 + Audit `outcome=DENIED`）→ 注入面已裁（CO-3），随 Supervisor 落
- [ ] 交付前最后一次重跑：`CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test ./internal/adapter/... ./internal/process/...` 绿

## Scope boundaries

- In scope: `core/internal/adapter/**`、`core/internal/process/**`、**`core/cmd/fake-adapter/**`**（契约 §10 CO-1 裁定）、
  `adapters/fake/**`（只放 fixture 与说明）、`.qoder/progress/adapter.md`
- Out of scope: `specs/**`、`docs/**`（主控）、`core/go.mod`（主控，禁 `go get`/`go mod tidy`）、
  `internal/platform`（db）、`internal/{task,execution,session,quota,models,project,scheduler}`（backend/backend-wp/session）、
  `internal/{approval,permission,audit}`（audit）、`internal/api` 与 `cmd/core`（backend）、`config.*.json`（主控独占）、
  五家真实软件与 Phase 0 探测（survey + 用户单独授权）

## Current checkpoint

- Stage: **第 38 轮 = T-441 把 §4 的第七条 Adapter→Core 通知 `approval/request` 落到线上**（帧模型 + FakeAdapter 真发 + 泵不结案，三层都在我包里；消费那一半是 backend 的 T-442，我没碰）。
  ① **帧**：`MethodApprovalRequest` 常量 + 进 `CarriesTurnRef` 那个 switch。**判据按族判、不按契约正文那句「全部六条」判**——我把代码与注释里按数字写的措辞换成按族写（现读 `grep -rn "all six"` 我三包 = **0**；`grep 六条` 只剩两处，两处都是「别按那句数」的说明），载荷 = §4:91 那四个字段（`action` 必填 + `resource?`／`risk_hint?`／`provider_execution_id?`），**一个字段都没自加**（主控那句「别自己加字段，回来提 REQ」我按字办）；缺 `action` 生产侧与消费侧都拒，走的是同一个谓词。
  ② **真路径发**：`session/send` 的提示词 `run tool write|exec` ⇒ 先发 `stream/item{state:AWAITING_APPROVAL}` 再发 `approval/request`，然后**停在闸门**——`settle` 见 `t.awaiting` 就不写 `turn/finished`（§6 规则 1 只有它能收尾）。`action` 串**逐字取自 `internal/permission` 那份内置分类**（`filesystem.write`／`command.exec`），而且不是靠我肉眼挑：`TestApprovalRequestActionsAreClassifierVocabulary` **现调 `permission.RiskLevel()`** 证「登记**且**为 HIGH」，另带一枚反例（旧 fixture 串 `write_file` 必须仍未登记）⇒ 主控警告的那个「用例因意外而通过」被机械判据挡住，不是被我的判断挡住。fixture 里那行审批记录也顺手换成分类器的串，两处词表并成一份。
  ③ **方向一字未动**：`approval/list` 仍答它自己的 id、`approval/respond` 仍是 C→A；新帧是通知（`ID == ""`、`request_id` 有自己的 UUIDv7、发出后 300ms 内 stdout 静默 ⇒ 没人应答它）。泵侧 `applyTurn` **故意不给 `approval/request` 任何 case** ⇒ 转发 + 按 ticket 归属 + **不结案**；同一枚用例还补了「随后 `turn/finished` 一到就 CLOSED/SUCCEEDED」那一腿，防这条被读成「通知被忽略」。
- Status: **idle-ready；本轮我名下 9 件（8 件 ` M` + 1 枚未跟踪 `approval_request_test.go`）未入册**。整仓未提交 30 处，HEAD `17ca538`。**第 35/36/37 三批我原来写着「仍未入册」，这次现读推翻了自己**：`git log -1 --format=%h -- <路径>` = `supervisor.go`/`supervisor_test.go` 随 **`d060579`（10-01 16:29）**、`declaration_test.go`/`delivery_test.go`/`main_test.go` 随 **`3ff8271`（10-01 17:28）**入册，且 `git status --porcelain` 对这五件**为空** ⇒ 巡检转告那句「你前三批已入册」是成立的，我第 37 轮头部与 CO-62 ③ 那句「盘上确是未提交」当场作废（留在原处不删，标被取代）。**B 面不是全量绿**：`core/test` 两枚红（原文与归属在证据格，契约 §3.0-c 自己写着它们是 T-430/T-442 的闸门），**点名我三包的红 = 0**，我不修也不消红。
- Last verified action: **19:08:47→19:10:34 `mut-r38.py` 十枚**（基线 **153 PASS / 0 FAIL / rc=0**、十枚锚点命中全 = 1、判决 **10/10 FIRED**、四文件按字节还原 PRE=POST：`stream.go aa34793521fa55ca`、`connection.go d00554855f3a6d3d`、`main.go beb9417de21be9a5`、`contract_table_test.go 7dfd00f44d043a2f`；`grep -rn ZMUT core/` = **0**）+ **19:19:33 一枚补跑 a5b**——a5 在批里只打中 `TestConnectionDedupsAndTracksTurn`、**没打中我的新用例**（循环停在 3 帧，第四帧留在通道里没人看），所以我给「重复帧不转发」那一腿补了一条**测量过的缺席**（读满 3 帧后再等 300ms，不得再有第四帧），复跑 **FIRED** 且把那一帧原文打印出来 ⇒ **本轮共 11 枚判决、11/11 FIRED**，其中一枚是我自己交账前发现的覆盖面缺口，不是主控点的。
  **19:23:21→19:23:46 默认面 `-count=3 -v`**（`faceA-r38-count3.log`，`A_RC=0`）= **459 PASS / 6 SKIP / 0 FAIL、三包 ok**（459 = 153×3；153 = 第 37 轮 149 + 本轮 4 枚新用例）。**旗标面对我仍不成立**：现读 `grep -rl "go:build\|+build"` 我三包 = **0 个文件** ⇒ 「旗标面与默认面各 `-count=3`」在我这里塌缩成「默认面已跑」，按事实报、不造一面交差。
  **19:23:56→19:24:21 B 面 `go test ./...`**（`faceB-r38.log`）：`ALL_RC=1`、**17 包 ok + 1 包红（`core/test`）**；**「全」= 18 包**（17 + `core/test`，本轮 no-test-files = 0）；build failed 0、panic 0。
  指纹（**19:32:16 / 19:32:17 现跑**，`bash scripts/build_stamp.sh`，HEAD `17ca538`、未提交 30 处）：闭包三包 = **`09e575f65e18d6d8f262f68201c958bb`**（1703 输入，比第 37 轮那枚多 1 = 本轮那枚未跟踪测试文件），`cmd/fake-adapter` 单包 = **`a1549e74c6fe77cb890d7c0271d1398e`**（1695 输入）。**第 37 轮那两枚（`87d90b5a…` / `77b35fa9…`）留着不删**，它们只代表那棵旧树。
- Verification: `## Verification evidence` 顶部 = **"第 38 轮（T-441：§4 的第七条通知落到线上）"** —— 里面有三处按字复核（没碰 `specs/**`、没碰别人名下包、`applyTurn` 那一格为什么必须空着）、11 枚变异判决表、两面读数与「全」是哪个集合、B 面两枚红的**原文 + 归属**、四文件字节还原，以及**我这一半的坐标**（给主控跟 backend 的 T-442 对表用）。当场可复跑：`python .scratch/adapter/mut-r38.py`、`cd core && CGO_ENABLED=0 go test -count=3 -timeout 900s -v ./internal/process/ ./internal/adapter/ ./cmd/fake-adapter/`。
- Next action: **idle-ready，等主控点名**。等格：
  ① 等什么：**本轮这 9 件入册**（谁解锁：主控。交付面**含 1 枚未跟踪文件** `core/cmd/fake-adapter/approval_request_test.go`（203 行现读），`git diff --numstat HEAD` 看不见它，要按 `??` 点名；前三批的入册我已自己复量完，见 Status 那格的更正）；
  ② 等什么：**backend 交 T-442 的消费半**（谁解锁：backend。`approval/request` 进 Core 之后要走的 `permission.Engine.Decide` → `approval.Service.Request` 不归我，我不去 `cmd/core` 替他接；我这一半的坐标在证据格末与 CO-63）；
  ③ 等什么：**CO-63 裁「批了之后谁收尾」**（谁解锁：主控。契约只裁了「这一轮停在闸门」，没裁「答复回来之后这一轮怎么结束」；今天我的双是**停在闸门不动**——不伪造续跑、也不自己发终态，三档选项与推荐在 CO-63）；
  ④ 等什么：CO-57 三档 + CO-58 ②③、§10 那一行由主控落字（照第 37 轮，未闭）；
  ⑤ **我不宣布 B 面那两枚红关闭**：不在我名下，且契约 §3.0-c 明文「谁都不许为了让它绿而改这张表」——判据读表、表读盘。
  - 【以下五格是第 37 轮（10-01 15:55~16:06，T-436：把「目标早不在」那一层装进 `Stop` 本体）写的，按 T-276 原样留档、不当现状读；它那句「本轮 2 件改动未入册」与 CO-62 ③ 都被第 38 轮的现读推翻（那五件已随 `d060579`/`3ff8271` 入册，见上面的 Status），闭包两枚指纹也换成 `09e575f6…`/`a1549e74…`——树动了、码也动了，旧枚只代表旧字节】
  - Stage: **第 37 轮 = T-436 修在 `Stop` 本体**（主控判给我、用例留在 backend 包里不动，我完全没碰 `cmd/core`）。
  收口的返回值从此只回答一个问题——**「这个孩子的退出状态收到没有」**：`Kill` 失败不再直接当成收口失败，
  先在有界 `Wait` 里收状态；收到了就是 nil（不管 `Kill` 当时答了什么），收不到才报错，
  且报的是 `Kill` 那一手（调用方能据此行动的那一半）。这正是第 19 条那句「收口函数必须自带『目标早不在』那一层过滤」
  的落地，而且**不靠分类 OS 错误码**——`ChildAlreadyGone` 问的是「这个 pid 还归我不归我」，
  而"进程已死、句柄没 release"它答「归我」；`Reaped()` 问的是「有人调过我的 Wait」，那时也没人调过。
  **两枚新用例都在我包里**：`TestStopIsQuietWhenTheChildAlreadyExited`（正例：孩子自己先退、`done` 没人排空 ⇒
  Stop 返回 nil **且**状态确实收到）与 `TestStopStillReportsAChildItCouldNotStop`（活孩子 + 零期限 ⇒ 必须仍报失败，
  防的就是主控点名的「放宽成允许任何错误」）。正例的确定性不靠 sleep：**排空孩子的 stderr 到 EOF 就是它已死的时刻**。
  **三枚反证（`mut-r37.py`，基线 `internal/process` + `cmd/core` = 146 PASS / 0 FAIL）**：
  s1 把 T-436 之前的写法搬回去 ⇒ **FIRED 两枚**，而且其中一枚直接打印出那句 OS 错原文
  （`supervisor_test.go:397: cleanup Stop: kill: TerminateProcess: Access is denied.`）⇒
  **backend 那枚 15 趟 1 红的形状，在我包里可以 100% 复现**，不是靠概率抓的；
  s2「不收状态直接 return nil」（= err == nil 式吞掉）⇒ **FIRED 四枚**（含 `TestCloseReapsTheChild`、
  `TestCloseAfterAnExistingReapStaysQuiet`）；s3「`Kill` 的错从此不报」⇒ **SILENT，且我给理由不假装挡住**：
  `Kill 失败` 且 `状态收不到` 这个组合对我自己 spawn 的孩子不可构造（活着 Kill 必成、死了 Wait 必答），
  所以那一档没有可执行判据；可构造的两档（吞掉不收状态、活孩子报成功）各有用例并且都 FIRED。
  - Status: **idle-ready；本轮 2 件改动未入册（`supervisor.go` 产品码 + `supervisor_test.go` 用例），
  而且第 35/36 轮那三件也仍未入册**（现读：`?? core/cmd/fake-adapter/declaration_test.go`、
  ` M core/cmd/fake-adapter/{delivery_test.go,main_test.go}`——见下一格的复量命令与结果）。
  整仓未提交 26 处，我占 5 处。巡检与主控都提过「T-431 那两件我已入册」，**我账上没有 T-431 这个东西**
  （台账里出现过的最大号是 T-423/T-424，`grep -o "T-4[0-9][0-9]" .qoder/progress/adapter.md` 现读），
  盘上我那三批也确实是未提交状态 ⇒ 那句要么号错、要么指别家；我不据此调整方向，也不删自己的在途件。
  - Last verified action: **15:59:25→16:01:21 mut-r37**：基线两包 **146 PASS / 0 FAIL**、三枚锚点命中全 = 1、
  判决 **s1 FIRED / s2 FIRED / s3 SILENT（理由在上格）**、`supervisor.go` 按字节还原 PRE=POST
  **`4cbd362e80357f7b`**、ZMUT 0、CR 0、备份与盘上字节相同；每枚动手前跑了一次本包预检（第 33 轮那条规矩，本批沿用）。
  **16:02:12→16:02:36 默认面 `-count=3`**（`faceA-r37-count3.log`，`A_RC=0`）= **447 PASS / 6 SKIP / 0 FAIL、三包 ok**
  （447 = 149×3，149 = 第 36 轮的 147 + 本轮两枚新用例）；**16:02:53→16:03:53 `./cmd/core/` 也跑 `-count=3`**
  （`cmdcore-r37-count3.log`）= `ok`、59.158s、**日志里 `Access is denied` 出现 0 次**——
  这只算「没复发」（原判 1/15，3 趟本来就抓不到），关闭权在第 19 条的结构修 + s1 那次 100% 复现。
  计数一律只从带 `-v` 的那趟取（新口径）。指纹（16:03:53~54 现跑，HEAD `9a6a8d5`、未提交 26 处）：
  闭包三包 = **`87d90b5a6948265087db64681bdb4eb4`**（1702 输入）、`internal/process` = **`77b35fa9114d8daefd85ef99917d007c`**
  （1695 输入，与第 36 轮那枚 1694 **不同包**（那枚是 `cmd/fake-adapter` 的输入数），不是涨跌；16:05:38 复测同值）。
  **与第 36 轮两枚旧值并列不删**（`b2120db5…` / `25f6bd3a…` → 本轮 `87d90b5a…` / `77b35fa9…`）：树动了、码也动了。
  - Verification: `## Verification evidence` 顶部 = **"第 37 轮（T-436：收口自带『目标早不在』那一层）"** ——
  红的成因链（三条问句各自答什么：`Kill` 答"句柄还能不能Terminate"、`ChildAlreadyGone` 答"pid 还归我不归我"、
  `Wait` 答"状态拿到没有"）、为什么不能靠分类 errno、s1 打印出的 OS 错原文、s3 为什么 SILENT、
  以及**顺带收到的一条好消息**：backend 已按 CO-51 把两处调用切成 `process.ChildAlreadyGone`
  （`cmd/core/processpipe.go:222`、`cmd/core/turnpump.go:493`，他们那枚 `processpipe_test.go:214-225` 还钉着
  「本包不许再有私有副本」）⇒ CO-51 从他们那半已闭；修完之后那一判从「唯一防线」变成「第二道」，
  两处调用点不用改、也仍然正确。当场可复跑：`python .scratch/adapter/mut-r37.py`、
  `cd core && CGO_ENABLED=0 go test -count=3 -timeout 900s -v ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`。
  - Next action: **idle-ready，等主控点名**。等格：
  ① 等什么：**第 35 + 36 + 37 三批被一起入册**（谁解锁：主控。现读三批都还在工作树里；
  交付面含**两枚未跟踪文件**（`declaration_test.go` 211 行），`git diff --numstat HEAD` 看不见，要按 `??` 点名——
  这已经是连着第二次提醒同一件事）；
  ② 等什么：**backend 复跑他那枚 `lifecyclecolumns_test.go:304` 并交回读数**（谁解锁：backend。
  用例我没搬、`cmd/core` 我一字未改；我这面的证据是 s1 在 `internal/process` 里 100% 复现了同一句 OS 错，
  所以"能不能关掉他那枚红"要由他的读数说话，**我不替他宣布关闭**）；
  ③ 等什么：**§10 那一行由主控落字**（拟稿在 CO-60；落完我下一轮做「契约里查得到这枚注入端点」的双向判据）；
  ④ 等什么：**`CO-57` 三档 + `CO-58 ③`**（`execution_leases` 甲/乙/丙、`provider_execution_id` 列写点名、
  `Run` 另一半形状、单次连接丢失算 DEGRADED 还是 UNAVAILABLE）与 `CO-58 ②` 的联合反例（等 backend 接上那次调用）；
  ⑤ 等什么：**「你 T-431 那两件我已入册」这句的指代**（谁解锁：主控——我账上没有 T-431，盘上三批未提交；
  要更正只需一句，我不按它删自己的在途件）；
  ⑥ 我名下没有欠着的动作：T-436 的三条要求（判据能命中、三种消红不接受、不自裁）都按字办，
  第三种（s3）我交的是"不可构造"的读数与理由，不是自裁。
  - 【以下五格是第 35 轮与第 36 轮（10-01 12:57~13:32，T-423 三格落地 + 新计数口径抓出我自己的跨流 flake）写的，按 T-276 原样留档、不当现状读；第 36 轮那两枚 `-count=3` 读数已被第 37 轮换掉（我又加了两枚用例，147→149 是集合扩大），闭包指纹也换成 `87d90b5a…`】
  - Stage: **第 36 轮 = 主控的新计数口径落地，并且它当场抓出我一枚真 flake**（不是别人的、不是产品码）。
    13:23 那次 `go test -count=3 ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/` 出
    **440 PASS / 6 SKIP / 1 FAIL**，红的是我第 31 轮写的 `TestFakeAdapterTurnFinishThreeStatesOnTheWire`
    （`delivery_test.go:509`：`the dropped code was never reported`，而同一枚用例在 `-count=1` 下从来没红过）。
    **成因是跨流**：那条断言读的是 `p.stderr`，而 os/exec 用自己的协程拷孩子的 stderr，
    活进程期间读它**可以正好丢掉要断言的那一行**——这条道理**本包 `badexec_test.go:28` 早就写了并给了正解 `stop()`**，
    是我第 31 轮没照办。修法按既有惯例、不加新机械：三处「活进程期间读 stderr」的断言全搬到 **`p.stop(t)` 之后**
    （`delivery_test.go` 两处的 508/325 与 `main_test.go:266` 那处；`main_test.go:388` 那处本来就在 `cmd.Wait()` 之后，没动）。
    搬完必须证明没被搬成永真 ⇒ 三枚变异各删一行日志，**3/3 FIRED**：d4 删「nothing is open to close」那行 ⇒ 三态用例红，
    d5 删「hanging on」⇒ hang 用例红，d6 删「fake-adapter up」⇒ 握手前那枚 §1 判据红。
    复跑：那四条（三枚搬迁过的 + 新判据）`-count=6` = **24 RUN / 24 PASS / 0 FAIL**；默认面 `-count=3` 全量 =
    **441 PASS / 6 SKIP / 0 FAIL、三包 ok**（441 = 147×3，RUN=447 = PASS+SKIP 对得上）。
    **旗标面对我没有实体**：三包内 `os.Getenv` 只有 `LUCERO_HELPER`/`FAKE_ADAPTER_HELPER`（替身进程的机制）与
    `envBadExecutionID`（由用例自己 `t.Setenv` 开的注入门），没有任何一面靠外部旗标才跑 ⇒ 「旗标面与默认面各 -count=3」
    在我这里塌缩成「默认面 -count=3 已跑」，这条我按事实报、不造一面来交差。
  - Status: **idle-ready；本轮 3 件测试文件改动（`cmd/fake-adapter/{delivery_test.go,main_test.go}` 两处搬迁 +
    第 35 轮那枚未跟踪新文件仍在）+ 产品码仍零改动**（`main.go` sha 仍是 `c11aaf2ffe55adbf`，
    变异批 PRE=POST 同值可复核）。整仓未提交 26 处，我占 3 处（1 新 + 2 测试文件），HEAD 已从 `3a0d7c7` 走到 `2463251`。
  - Last verified action: **13:23:54→13:24:18 首次 `-count=3`**（`faceA-r35-count3.log`）：440 PASS / 6 SKIP / **1 FAIL**
    ——这一份是**病灶记录**，留着不删；**13:27:09→13:27:16 那四条用例 `-count=6`**（`flakefix-r36-count6.log`）
    24 RUN / 24 PASS / 0 FAIL；**13:28:06→13:29:01 mut-r36**（基线两包 **83 PASS / 0 FAIL**、三枚锚点命中全 = 1、
    **3/3 FIRED**、`main.go` 还原 PRE=POST `c11aaf2ffe55adbf`、ZMUT 0、CR 0，每枚前跑本包预检）；
    **交付重跑 13:29:27→13:29:51 默认面 `-count=3`**（`faceA-r36-count3.log`，`RC=0`）=
    **441 PASS / 6 SKIP / 0 FAIL、三包 ok**，负载条件=正常桌面、无并发构建（计数只从这一趟 `-v` 读数取，按新口径）。
    指纹（13:29:51~52 现跑，HEAD `2463251`、未提交 26 处）：闭包三包 = **`b2120db5d9cc65be88078fd853718be3`**（1702 输入），
    `cmd/fake-adapter` = **`25f6bd3a17a1af2fb756db020f1f778e`**（1694 输入）。
    **同一枚 `cmd/fake-adapter` 今天三值**：`6a429a88…`(13:06) → `b06a8927…`(13:12) → `25f6bd3a…`(13:29)，
    依次对应「删重复 Logf」「搬 stderr 判据」两次编辑 ⇒ 前两只算「同一项目更早的采样」，交付值认最后一枚。
  - Verification: `## Verification evidence` 顶部 = **"第 36 轮（新计数口径抓出的跨流 flake + 三枚搬迁后仍有牙）"** ——
    红的原文与行号、为什么 `-count=1` 抓不到、本包早有的正解（`badexec_test.go:28` 的注释与 `stop()`）、
    三枚变异判决表、`-count=6`/`-count=3` 两组读数、旗标面对我不成立的 grep 证据、以及
    「主控那句入册提交差一枚」的盘上核对（`git log -1 --format=%h -- <路径>` 五件全指 `2c65177`）都在里面；
    CO-61 记两件同步的回执。当场可复跑：`python .scratch/adapter/mut-r36.py`、
    `cd core && CGO_ENABLED=0 go test -count=3 -timeout 600s -v ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`。
  - Next action: **idle-ready，等主控点名**。等格：
    ① 等什么：**第 35 + 36 轮一起被入册**（谁解锁：主控；本轮交付面 = 1 枚未跟踪新文件 + 2 处测试文件搬迁，
    `git diff --numstat HEAD` 看不见那枚未跟踪的，别按差分行数漏掉）；
    ② 等什么：**§10 那一行由主控落字**（拟稿在 CO-60；落完我下一轮把「契约里能查到这枚注入端点」做成双向判据）；
    ③ 等什么：**CO-58 ② 那条联合反例**（谁解锁：backend——泵侧那次 `DowngradeHealth` 调用还没接；主控本封确认 T-421/T-424 已在它手上，时点后移）；
    ④ 等什么：**`CO-57` 三档 + `CO-58 ③`**（`execution_leases` 甲/乙/丙、`provider_execution_id` 列写点名、`Run` 另一半形状、单次丢失算 DEGRADED 还是 UNAVAILABLE）；
    ⑤ **新口径从此默认执行**：计数只从 `-v` 那趟取、可能红的先落文件再看尾巴、旗标面与默认面各 `-count=3`
    ——我三包无旗标面这件事本轮已用 grep 证过，若以后引入一枚（例如按 env 开的门），交付就要两面各 ×3；
    ⑥ 我名下**没有欠着的动作**：三格已按裁定办完，本轮另加一枚自己 flake 的定位与修复。

  - 【以下五格是第 35 轮（10-01 12:57~13:14，T-423 三格落地：格一双向判据、格二不建、格三出拟稿）写的，按 T-276 原样留档、不当现状读；它那句「A 面 147 PASS（-count=1）」已被第 36 轮的 -count=3 口径取代，同一枚判据仍然在，只是计数面变宽了】
  - Stage: **第 35 轮 = T-423 三格按裁定落地**（格一准甲、格二「v0.1 不发」、格三准甲「拟稿我出、字主控落」）。
    **格一已交**：新文件 `core/cmd/fake-adapter/declaration_test.go` 里的
    `TestFakeAdapterDeclarationMatchesDispatch`，**只读不改产品语义**，三条臂：
    **A 向**「声明为有效的能力 ⇒ 这台真接」、**B 向**「这台真接 ⇒ 有声明（等级不限）」、
    **C 向**「例外表不许过期」——三枚变异 **3/3 FIRED**：d1 把 `debugCapability("debug.hold")` 那行删掉
    （**就是 h4 当时的形状**）**只红我这一枚用例**（PASS=82 / FAIL=1，说明这条臂此前确实无人守），
    d2 把 `case methodDebugHold:` 删掉红 10 枚（A 向 + 交错判据一起抓），d3 让例外表留一条不再服务的方法红 1 枚。
    两个集合都是**从盘上读出来的、不是手抄的**：served 由解析 `(*fake).serve` 的 dispatch 得到（含 switch 前的
    `$/cancel` 那一支），符号再从两个包的 const 声明解出来；今天量到 **served=19 / declared=16 / effective=15 / 例外=3**
    （effective 少的那一枚正是 `session.locate`——接、答 `CAPABILITY_UNAVAILABLE`、声明 UNSUPPORTED，
    是 B 向的合法形状，不需要开例外）。解析器带三枚下限（served<12、declared<14、effective<13 直接 `t.Fatal`），
    并把集合大小打进日志，免得某天悄悄变窄还一路绿。
    **格二按裁定办**：不建发射器、不动 `specs/events/**` 那一行（现读名单仍是 21 条、含 `adapter_downgraded`）；
    主控把我那两条机械理由原样写进契约后，这一格从「缺口」变成「已裁的有意 idle」，我不会再在交账里把它当欠账报。
    **格三拟稿已出**（全文在 CO-60，含参数形状、`code` 只在解除侧、常态位置在 send 回执之后、只许 fake+EXPERIMENTAL
    那四层意思，以及「补这一行的记账理由」那一短句）；字归主控落，我不碰 `specs/**`。
  - Status: **idle-ready；本轮 1 件新文件（未跟踪，211 行），零产品码改动**。
    现读证据：`git diff --numstat HEAD -- core/cmd/fake-adapter/` = **0 行**（`main.go` 与它已跟踪的测试一字未动，
    `main.go` sha 仍是第 31 轮交付那枚 `c11aaf2ffe55adbf`），未跟踪只多 `declaration_test.go` 一枚。
    整仓未提交 19 处，其余是 backend/test/db/frontend 的在途（`internal/execution/output.go` 等）。
  - Last verified action: **13:10:33→13:11 mut-r35（最终字节上）**：基线两包 **83 PASS / 0 FAIL**、三枚锚点命中全 = 1、
    **3/3 FIRED**、每枚输出与基线两两不同、两文件按字节还原（`main.go` PRE=POST `c11aaf2ffe55adbf`、
    `declaration_test.go` PRE=POST `58550f30f14398c6`）、ZMUT 0、CR 0；每枚动手前跑了一次本包预检（第 33 轮那条规矩）。
    **13:11:42→13:11:52 A 面交付重跑**（`faceA-r35-final.log`，`A_RC=0`）：**147 PASS / 2 SKIP / 0 FAIL、三包 ok**
    （147 = 第 34 轮的 146 + 本轮这 1 枚）。
    **13:07:14→13:07:38 B 面**（`faceB-r35.log`）：`ALL_RC=1`、**16 包 ok + 2 枚红**，红因都在别人名下——
    `api/http/TestExecutionOutputRouteSaysNotWiredNotUnknownShape`（`declared_not_wired_test.go:147`，
    GET `/output` 该 501 实得 404，backend 的 T-424 在途）与 `core/test/TestEventProducerRosterMatchesDerivedSet`
    （`event_producer_roster_test.go:1096`：`execution_output_flushed` 登记无生产者却扫到发射位，同一件在途码引起）。
    **整份日志里点到我名下文件的 FAIL 行 = 0**，我也没去修它们。
    指纹（13:12:17 现跑，`stamp-r35.txt`）：`cmd/fake-adapter` = **`b06a8927218ced81b98fb2469f389b95`**、
    闭包 `[internal/process + cmd/fake-adapter + internal/adapter]` = **`f820d5da4d0c8d2caa9530331c4d9892`**
    （1702 输入，比第 33 轮多一枚 = 本轮新增的测试文件），HEAD `3a0d7c7`、工作树未提交 19 处。
    **同一天两枚 `cmd/fake-adapter` 指纹（13:06:59 的 `6a429a88…` 与 13:11:52 的 `b06a8927…`）中间只隔着我自己
    一次测试文件编辑**（删掉重复的 `t.Logf`）⇒ 交付值认后者，前者按「同一轮更早的一次采样」留着。
  - Verification: `## Verification evidence` 顶部 = **"第 35 轮（T-423：格一的双向判据 + 格三拟稿 + 格二记账）"** ——
    判据为什么只读源码不手抄（手抄一份名单就是造第二个真相，而它漂移的方式正是 h4）、三枚变异判决表、
    两处我自己当场抓住的缺陷（符号表漏了 `const CancelMethod = "$/cancel"` 这种行首单行常量 ⇒ 它是**以 `t.Fatal` 暴露**
    而不是以绿暴露，改完才过；以及一次重复的 `t.Logf`）、三枚集合大小的现读数、格二的"不再当欠账报"声明都在里面。
    当场可复跑：`python .scratch/adapter/mut-r35.py`、
    `cd core && CGO_ENABLED=0 go test -count=1 -timeout 120s -v -run TestFakeAdapterDeclarationMatchesDispatch ./cmd/fake-adapter/`。
  - Next action: **idle-ready，等主控点名**。等格写全：
    ① 等什么：**本轮那枚新判据被复跑/入册**（谁解锁：主控；判据是未跟踪新文件，`git diff --numstat HEAD` 看不见它，
    入册时要按 `git status --porcelain` 的 `??` 那行点名，别按差分行数漏掉）；
    ② 等什么：**§10 那一行由主控落笔**（拟稿在 CO-60；落完我在下一轮把「契约里能查到这枚注入端点」做成一条判据，
    否则它又会退回"只活在代码与台账里"）；
    ③ 等什么：**CO-58 ② 那条联合反例**（谁解锁：backend——泵侧 `retire`/`closeConn` 调 `DowngradeHealth` 的那一次
    调用今天还没接，主控本封也确认了「时点后移」，因为 backend 同时领了 T-421/T-424）；
    ④ 等什么：**`CO-57` 三档**（`execution_leases` 甲/乙/丙、`provider_execution_id` 的列写点名、`Run` 另一半形状）
    与 `CO-58 ③`（单次连接丢失算 DEGRADED 还是 UNAVAILABLE）——**这四句还挂着，我没自己动**；
    ⑤ 我名下**没有欠着的动作**：格一已交、格二已按裁不建、格三只差主控落字。

  - 【以下五格是第 34 轮（10-01 12:23~12:35，T-419：三格压成三条、零代码）写的，按 T-276 原样留档、不当现状读；那一轮交的三个问题**主控已在 T-423 全部裁定**（格一准甲＝本第 35 轮已落地、格二 v0.1 不发、格三准甲），所以那三格不再算「等裁」，只有它列出的另几格仍在等】
  - Stage: **第 34 轮 = T-419 交回三格，每格三条（要裁哪件事 / 选项各关掉哪格 / 推荐哪个与理由），零代码改动**。
    主控说不按编号猜语义，所以这三格全部**用盘上证据自己解释自己**，每条断言带 `文件:行号`：
    **格一（我账上叫 CO-55）**——要裁的是「Core 发帧之前查不查那张能力表」。今天 `Handshake.Call`（`internal/adapter/handshake.go:342-368`）
    只查两件事：握过手（§2）与 `write_entry=false` 时有没有 UI_AUTOMATION 兜底（§3）；`internal/process` 的 `gate`（`connection.go:213-218`）
    只是转调它 ⇒ 方法声明没声明**没人查**。我推荐**甲：只补一条双向判据，不动运行时**——因为 §9 的真闸门已经长在声明侧
    （`handshake.go:244`：非 `fake` 声明 `debug.*`、或等级不是 `EXPERIMENTAL`，`NewHandshake` 直接拒），
    而「Core 本地拒」这一档实测会把 §4 指定的答案换掉：`session/locate` 今天答的是 Adapter 的 `CAPABILITY_UNAVAILABLE`
    （`cmd/fake-adapter/service.go:468-472`），拦在它前面就不是加保险而是改协议答案。
    **格二（§22 那条事件的发射点）**——要裁的**不是「发不发」而是「有没有能装它的写点」**：`adapter_downgraded` 的聚合是 `adapter`，
    守卫要求事件聚合等于被 Touch 的聚合（`internal/platform/db/touch.go:571` 的原文格式串就是 `aggregate_type %q but touching %q`），
    而「降级」发生在能力行 `adapter_capabilities`（另一个聚合）⇒ **今天从能力那条腿发它在机械上不可达**，不是没做完。
    更深一格：`resolveEffective`（`handshake.go:197-219`）今天只做**包含/剔除**，没有任何代码把一个能力的 `effective_backend`
    从 NATIVE 改成 CLI ⇒ 事件 payload 的 `from_backend`/`to_backend`（`event-contract-v0.1.md:186`）**没有定义域**。
    所以我推荐**先裁「什么算降级」再选发射点**；若主控只要收口，**保持名单里那句「有意 idle」是诚实答案**，
    我不建议在无定义域时建发射器（同第 33 轮那句「没有裁定依据的话我不写成规则」）。
    **格三（§10 注入表补行）**——要裁的是「第五枚注入端点在契约表里要不要看得见」：表今天只有四行
    （`adapter-protocol-v0.1.md:376-379` = `debug/fail`/`crash`/`hang`/`unknown_quota`），`debug/hold` **不在表里**，
    却已经是三条判据的仪器（迟到收尾 (a)、§6 规则 5 的两轮交错、第 31 轮的三态分辨）并带一个只在解除侧生效的 `code`。
    我推荐**甲：补一行**（含参数形状与「收尾帧落在 send 回执之后」那句），措辞限定为「替身的注入端点，不描述任何真 Adapter」。
  - Status: **idle-ready；本轮零代码、零新事实需要复跑判据，等主控裁这三格**。第 32/33 轮主控已入册（`1cca6fe` 现读：
    `internal/adapter/**` 与 `internal/process/**` 对 HEAD **零差异**）⇒ 我没有待交的代码，只有三个待裁问题。
  - Last verified action: **12:25:09→12:2x 全读码取证，未跑测试**（本轮不产码，跑测试没有新增可证的东西；
    台账里第 33 轮那两面读数仍是交付字节上的最后一次）。复量清单：`git rev-parse --short HEAD` = **`1cca6fe`**
    （10-01 12:05:44 那一笔）、`git status --porcelain` = **5 行**（db/frontend 两本账、`apps/desktop/.../live-core.test.ts`、
    `core/cmd/core/turnpump_late_test.go`、`orchestration.md`）——**我名下两包零差异**；
    `grep -n '状态更新（10-01' specs/contracts-internal-v0.1.md` = **`:796`**（主控那句「REQ-TURN-1 契约文本已落」对得上盘）；
    `grep -n 'debug/hold' specs/adapter-protocol/adapter-protocol-v0.1.md` = **0 命中**（格三的前提成立）；
    `grep -n '本名单当前是' specs/events/event-contract-v0.1.md` 那一整行含 `adapter_downgraded` ⇒ 名单 21 条为真（格二的前提成立）；
    其余四条落点（`handshake.go:342/244/197-219`、`connection.go:213`、`touch.go:571`、`service.go:468`、`main.go:700`）
    全部逐条 `grep -n` 现读，行号是本轮读到的，不是上轮的。
  - Verification: `## Verification evidence` 顶部 = **"第 34 轮（T-419：三格压成三条，零代码）"** —— 三格全文（含每档选项
    关掉/打开哪一格、我推荐哪档、以及两条"选项看起来保守但其实会改协议答案/机械不可达"的反例证据）都在那一节；
    段末新增 **CO-59** 记这三格，`CO-55` 原条目**不删**（它是 h4 那次 SILENT 的出处），只标已被 CO-59 推进。
    往下是第 33 轮（T-402）、第 32 轮补、第 32 轮（T-398）、第 31 轮补（T-397）、第 31 轮（T-392）。
  - Next action: **idle-ready，等主控裁这三格**。主控要给的三句 + 仍挂着的四句：
    ① **格一**：认「只补双向判据、不动运行时」还是「Core 发帧前本地拒（哪几类）」——若选后者，§15.2 要先加一行错误码，那是契约改动；
    ② **格二**：先裁「什么算一次能力降级」（`to_backend` 填什么），再选发射点；或直接确认「v0.1 不发，名单里那句是有意的」；
    ③ **格三**：§10 补 `debug/hold` 那一行（拟稿我可以给，但 `specs/**` 归主控，我不写）；
    ④ `CO-57 ①`（重启阶梯与常驻连接共用一台孩子还差"每起一台交回一个连接"那一步）、`CO-57 ②`（`execution_leases` 甲/乙/丙）、
    `CO-57 ③`（`provider_execution_id` 的列写点名归 execution）、`CO-58 ③`（单次连接丢失算 DEGRADED 还是 UNAVAILABLE）；
    ⑤ `CO-58 ②` 那条**联合反例**仍等 backend 把泵侧调用接上（我这半 h1 已有牙，他们那半今天无人证）。
    我名下没有欠着的动作：三格的问题已经压实，裁完我才动码。

  - 【以下五格是第 33 轮（09-30 17:24~17:45，T-402：health 写口 + h1..h8 全 FIRED + 给泵的调用面 CO-58）写的，按 T-276 原样留档、不当现状读；那一轮的两包改动**已于 `1cca6fe` 前后入册**（现读：两包对 HEAD 零差异），所以本块的「未入册」那句对该轮不再成立，只对第 32 轮之后新加的码成立】
  - Stage: **第 33 轮 = T-402 的 `adapter_instances.health` 写口交完**（与 T-398① 分两批，没合批）。
    新导出面 `(*SQLiteStore).DowngradeHealth(ctx, adapterKey, to) (bool, error)`：按 `adapter_key` 走、
    **`health` / `last_healthcheck_at` / `updated_at` 同笔三列**（① 那句「别只改一列」直接落成一条断言），
    每次都带 §6 给的那三把事件键（`from`/`to`/`confidence`，`event-contract-v0.1.md:185`），
    方向表是**闭集**：只收 H→D、H→U、D→U、UNKNOWN→U，抬健康一律拒（那是探针的证据不是泵的），
    同状态是 `(false, nil)` 不是错误（泵每次重连都撞一次很正常），表里没有这台 ⇒ 错误里保住
    `db.ErrAggregateNotFound`（**泵不许把「连接丢了」变成「注册了一台 Adapter」**）。
    两件我按盘上事实改了主控的前提，都写在证据里：① **这张表今天已有健康写口** ——
    `MarkUnverified` 就在写这三列（字面 `INSERT/UPDATE adapter_instances` 全在夹具里，生产走 `tx.Touch`，
    所以「零写者」那句只在字面 grep 口径下成立）；我没有另起第二个写手，而是**把两条门合到同一个 `healthWrite`**，
    并给它配了一枚回归位（h7：合并把探针那半的 `metadata_json` 理由吃掉就红）。② **`ProbeStore` 接口我没扩**——
    它是探针的两方法合同，加方法会打死别人家的替身；泵直接使 `core.instances *adapter.SQLiteStore`（`main.go:76` 已在）。
  - Status: **idle-ready；本轮 2 件（1 改 + 1 新用例文件）未入册，且第 32 轮那 3 件仍在同一棵树上等入册**。
    整仓未提交 14 处，我占 5 处（`probe_store_sqlite.go`、`health_store_test.go` + 第 32 轮三件），其余 9 处是
    backend 在落的 `internal/execution/{events,recovery,service}.go`、`recovery_unknown_test.go` 与别家账本。
  - Last verified action: **17:37:24→17:41:01 mut-r33b（v2，带基线预检）**：基线三包 **322 PASS / 0 FAIL**、
    八枚锚点命中全 = 1、**8/8 FIRED**（h1 摘掉写口红 **6** 枚，正是主控点名的那条），每枚输出与基线两两不同，
    `probe_store_sqlite.go` 还原 PRE=POST **`3c9ee50b81cd433e`**（17505 字节）、ZMUT 0、CR 0、备份与盘上字节相同。
    **17:31:49→17:32:01 A 面**：**146 PASS / 2 SKIP / 0 FAIL、三包 ok**（135 → 146 = 本轮 6 条顶层 + 5 条子用例）。
    **17:41:12→17:41:34 B 面**（`faceB-r33.log`）：**`ALL_RC=0`、18 包全 ok、0 FAIL、0 build failed**。
    指纹（现跑）：`internal/adapter` = **`3059c8d157feb07a6ffab24575f90f09`**（1687 输入），
    闭包 `[internal/process + cmd/fake-adapter + internal/adapter]` = **`6f3dd41a7f121347e34081255c10e445`**
    （1701 输入，比第 32 轮的 1700 多一枚＝新增的 `health_store_test.go`），HEAD `6ced981`、未提交 14 处。
    **一条流程上的自纠写在这里**：17:35 的 v1 批次里 h5/h6/h8 判成 BUILD-BROKEN，**不是我的变异**——那一刻别家在改
    `internal/eventjournal/events.go`（`ExecutionUnknown redeclared`）把三个依赖包一起打死；v2 给每一枚动手前加了一次
    **基线预检**（只跑我这包，不绿就记 VOID 不记判决），重跑才是 8/8 FIRED。⇒ 跨包变异批从此默认带预检，
    否则「别人正编不过」会冒充成我这批的判决。
  - Verification: `## Verification evidence` 顶部 = **"第 33 轮（T-402：health 写口 + 两条门合到一个写手）"** ——
    「零写者」那句在两种口径下各自成立/不成立（我复跑了 `grep -c` 两遍并各带口径）、`healthWrite` 合并的形状与
    h7 那枚回归位、方向表为什么这么闭（含 `000001:93-96` 的 CHECK 有六个值而协议只有四个这一格差异）、
    为什么不扩 `ProbeStore`、给泵的调用面与**那条跨家判据的配方**（他们接上之后该摘哪一行）都在里面；
    往下是第 32 轮补、第 32 轮（T-398）、第 31 轮补（T-397）、第 31 轮（T-392）。当场可复跑：
    `python .scratch/adapter/mut-r33b.py`、`cd core && CGO_ENABLED=0 go test -count=1 -timeout 300s -v ./internal/adapter/ -run 'TestDowngrade|TestBothHealth'`。
  - Next action: **idle-ready，等主控点名**。八格"等"写全：
    ① 等什么：**第 32 轮 + 第 33 轮被复跑/入册**（谁解锁：主控；当场可复量上面那三条命令；
    注意两批在同一棵树上，入册要一起入，别只挑一件）；
    ② 等什么：**泵侧那一次调用由 backend 接**（谁解锁：backend。要求 ② 说"调用方是泵、backend 不许自己 UPDATE 这张表"——
    我这面已经导出可调用；**他们接上之后才有那条"连接死了要降健康"的用例**，在那之前 h1 是唯一证人，我不替他们声称）；
    ③ 等什么：**DEGRADED 与 UNAVAILABLE 的分档**（谁解锁：主控。我今天写成闭集但**没有裁定依据**：
    一次连接丢失算 DEGRADED、§8 预算烧完（`ErrUnavailable`）算 UNAVAILABLE 是我的读法；
    契约只裁了后者（`adapter-protocol §8`），前者没原文 ⇒ 要么认我这个分档，要么改成"泵只发 UNAVAILABLE"，一句就行）；
    ④ 等什么：**`execution_leases` 的甲/乙/丙**（CO-57 ②，第 32 轮开的，仍未裁）；
    ⑤ 等什么：**`provider_execution_id` 的列写点名**（CO-57 ③，归 execution）；
    ⑥ 等什么：**`Run` 的另一半形状**（CO-57 ①：要让重启阶梯与常驻连接共用一台孩子，还差"每起一台交回一个连接"）；
    ⑦ 等什么：**§10 表第五枚补 `code`、CO-55、§22 `adapter_downgraded` 发射点、`REQ-TURN-1` 契约文本**（四格都在主控名下，各自独立）；
    ⑧ 等什么：**合验两半**（契约② §6 规则 6 + 第 25 条 (a)，谁解锁：主控发令；我不先动 `cmd/core`）。
    我名下没有欠着的动作：两轮的改动分别在 `core/internal/process/` 与 `core/internal/adapter/`，等入册。

  - 【以下五格是第 32 轮（09-30 15:56~17:00，T-398：NewConnection 闸门 + REQ-RC-3 归属 + 假绿复证）写的，按 T-276 原样留档、不当现状读；其中「A 面 135 PASS」那一枚已被第 33 轮换掉（本轮新增 11 条用例，同一集合的扩大不是波动），三枚指纹也换成第 33 轮那两枚——树动了，旧枚只代表旧字节】
  - Stage: **第 32 轮 = T-398 三件都交**。① 加了主控要的导出入口 **`(*Supervisor).NewConnection(child, coreVersion)`**
    （"从被监督的孩子构造连接"，不再 Start），并把它的前提做成能测的东西：孩子不是本 Supervisor 起的 ⇒
    **`ErrForeignChild`**（`Child.sup` 是包内字段，伪造不出来）；一个孩子已有活连接 ⇒ **`ErrChildBusy`**（两根泵读一根
    stdout 不是"安静一点的版本"，是把帧流切开）；**`Run` 在有连接挂着时直接拒**并点名那个 pid ⇒ ① 那一句"不许各起一台
    子进程"落成了断言（判据读的是 Launcher 的**起进程次数**，不是 pid）。闸门只挡不放是死锁不是规则，所以
    **`Connection.Close` 是唯一释放点**（`defer`，包含"上报回收失败"那条出口也在放）。
    ② **REQ-RC-3 的写者归属交回两条不同的答案**（详见 CO-57）：`executions.provider_execution_id` **不是没有信号源**，
    是**值已经到 Core 手里、没人落库**（我这半今天就在交：`turn/claimed` 经泵进 `TurnTracker.ProviderExecutionID()`，
    时机按契约必须是 claimed 那一刻，`adapter-protocol:189`；写列归 `internal/execution`）；
    `execution_leases` 则是**它守的那个并发面在文档里不存在**（"Runner" 全仓只有 §24 两行 + handoff 摘要，
    无定义、无 `owner_id` 生产者、无 acquire/release 协议、无 TTL 数值），而且今天**任何写它的代码路径都被
    `platform/db` 的 `narrowTables` 拒**（`touch.go:201` 只放 `idempotency_keys`，加表要过 §1.1.13 那道 REQ 门）
    ⇒ 我**没有**替它发明归属，三条可选形状与各自缺的原文都摆在 CO-57 等裁。③ 主控 T-398 与 T-392 里那两条
    `-run '^TestDelivery'` / `'^TestStartNow'` 在**入册后的新树上复跑仍是 0 命中**（RUN 计数各 0、
    `ok … [no tests to run]`），真实名字的三条各跑各绿，读数在下一格。
    两面绿：**A 面 135 PASS / 2 SKIP / 0 FAIL（三包 ok，五枚新用例逐条 PASS）**；
    **B 面 `go test ./...` = 18 包全 ok、`ALL_RC=0`、0 FAIL** —— 这一跑含 `core/test`（e2e/崩溃骨架），
    也就是说**我这半没有把 backend 那根刚入册的泵碰红**。
  - Status: **idle-ready；本轮 3 件改动未入册**（2 改 + 1 新，`+122/−1`），另有别家 5 处在途（`core/` 面 8 处未提交里
    我只占 3 处）。**0dceb09 已把我第 31 轮入册**（主控 ③ 那句），所以线序那半不再是在途件。
  - Last verified action: **16:54:46→16:54:57 A 面最终重跑**（`faceA-r32-final2.log`，`A_RC=0`、
    **135 PASS / 2 SKIP / 0 FAIL、三包 ok**，枚数与 16:50:44→16:51:01 那次（`faceA-r32-final.log`）**完全相同**——
    同一集合的复跑，不是两个集合；两次之间同样只差那处注释。
    那一次的原文按「同一轮更早的一次采样」留着：16:50:44→16:51:01（`faceA-r32-final.log`，`A_RC=0`、
    **135 PASS / 2 SKIP / 0 FAIL、三包 ok**；五枚新用例 `TestNewConnectionRidesTheChildWithoutStartingAnother` 0.95 /
    `TestNewConnectionRefusesAChildItDidNotStart` 0.96 / `TestOneChildCarriesOneConnection` 0.01 /
    `TestRunRefusesToStartASecondProcessBesideAConnection` 0.00 /
    `TestRunIsAllowedAgainOnceTheConnectionIsClosed` 0.02 逐条 `--- PASS:`）。
    **16:55:11→16:56:34 mut-r32 跑在最终交付字节上**：基线两包 **82 PASS / 0 FAIL**、六枚锚点命中全 = 1、**6/6 FIRED**
    （a4/a5 各红 **2** 枚、a6 红 3 枚）、六枚输出与基线两两不同；`supervisor.go` 还原 PRE=POST
    **`559fad27a8d6d1f2`**、`connection.go` **`a308276700796e52`**，ZMUT 残留 0、CR 0。
    **并列不删**：16:49:09→16:50 那一次也是 6/6 FIRED（当时 `supervisor.go` = `9d79878b8bdd41d0`），两次之间只多了
    一处**注释改动**（`supervisor.go` 里「这台闸门不覆盖什么」那一段）——注释不动行为，但**动了字节 ⇒ 换了指纹**，
    所以交付判据认后面那一次。
    **16:48:24→16:48:46 B 面**（`faceB-r32.log`，`ALL_RC=0`）：**18 包 ok / 0 FAIL / 0 build failed**，
    这一跑"全"= 18 包（17 + `core/test`），**与第 31 轮那句"17 包不含 core/test"是两个集合，别对拍**。
    指纹（现跑 `bash scripts/build_stamp.sh`，HEAD `5215256`、工作树未提交 8 处）：
    闭包 `[./internal/process ./cmd/fake-adapter ./internal/adapter]` = **`7eb1e8505c1a6334269ea380f3dd195b`**（1700 输入，16:54:57），
    单包 `internal/process` = **`44bde8bacd227e86995595b7887deab6`**（16:54:58），`cmd/core` = **`7989430625ad28c850586976fc929489`**（16:55:10）。
    **16:50:44~49 那三枚不删，降级为「同一轮更早的一次采样」**：闭包 `f8ace7c0a8ecbb823c5a80f928ed9c0e`、
    `internal/process` `a466f42115a4c3c7d1b459aa86db9cb3`、`cmd/core` `5147a2e5a41f8ddb6ea7c6e027e10a9f`。两次取号之间
    我只改过一处注释、没有行为改动，但**这句只有编辑记录、没有 diff 能自证到旧时刻**，所以处理方式是以最终字节上那三枚
    为交付值。B 面「我这半没碰红 backend」跑在 16:48:24→16:48:46（早于那处注释）⇒ 要最终字节上的 B 面就现跑
    `cd core && CGO_ENABLED=0 go test -count=1 -timeout 600s ./...`，我不拿旧时刻的 18 包 ok 冒充新字节。
    **③ 那两条的读数**：16:26:59→16:27:11 照字 `-run '^TestDelivery'` 与 `'^TestStartNow'` ⇒ `no tests to run` +
    `[no tests to run]`、`grep -c '^=== RUN'` **各 = 0**；16:27:19→16:27:33 真名三条
    `'^TestFakeAdapterClosingFrameFollowsItsReceipt$'` **0.83s PASS** / `'^TestFakeAdapterStartNowClosesItsRun$'`
    **0.65s PASS** / `'^TestFakeAdapterTurnFinishThreeStatesOnTheWire$'` **0.66s PASS**，另消费者半
    `'^TestLateTurnFinishedClosesTheTurnItBelongsTo$'` **1.03s PASS**（`t398-readings-162659.log`）。
  - Verification: `## Verification evidence` 顶部 = **"第 32 轮（T-398：NewConnection 闸门 + REQ-RC-3 归属 + 假绿复证）"** ——
    闸门为什么只挡一个方向（另一方向今天挡不了，因为 `Run` 不把手上的孩子交出去，那半写成 CO-57 的问题而不是伪装成实现）、
    "reaped 逃逸口"那一格怎么被变异查出来并当场收掉、REQ-RC-3 两半的原文落点（`000001:335-341`、
    `CORE_DOMAIN_MODEL:792/:810`、`touch.go:201`、`adapter-protocol:189/:346`）、六枚变异判决表、
    两面读数（含 18 包那个新集合）与脏件归属都在里面。当场可复跑：
    `python .scratch/adapter/mut-r32.py`、`bash scripts/build_stamp.sh ./internal/process ./cmd/fake-adapter ./internal/adapter`、
    `cd core && CGO_ENABLED=0 go test -count=1 -timeout 300s -v ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`。
  - Next action: **idle-ready，等主控点名**。七格"等"写全：
    ① 等什么：**本轮（第 32 轮）被复跑/入册**（谁解锁：主控；当场可复量上面那三条命令）；
    ② 等什么：**`execution_leases` 的三选一裁定（CO-57 的 甲/乙/丙）**（谁解锁：主控。我不代裁：
    甲=v0.1 明写租约不进范围、乙=先开 `narrowTables` 的 REQ 门再补"Runner 是谁/何时 acquire/TTL 多少/谁判过期"四件原文、
    丙=承认今天只有进程级存活；三条都不在 `internal/process` 里落码就能解决）；
    ③ 等什么：**`executions.provider_execution_id` 那一列的写入点名**（谁解锁：主控 + backend。
    我这半的值今天已经在 `TurnTracker` 里、泵也在收，缺的是 execution 侧一次列写，**不是信号源缺失**）；
    ④ 等什么：**`Run` 那一格另一半的形状**（CO-57 末段：要让"重启阶梯 + 常驻连接"共用一台孩子，
    还差"每起一台交回一个连接"那一步，那是 backend 的泵与主控共同的决定，我不替它定）；
    ⑤ 等什么：**§10 表第五枚补 `code` 参数那一行 + CO-55 + §22 `adapter_downgraded` 发射点**（谁解锁：主控，三格各自独立）；
    ⑥ 等什么：**`REQ-TURN-1` 的契约文本**（生产者侧常态位置我第 31 轮已钉，但"v0.1 未定义"那格仍要主控改口径；
    **我不拿形状冒充裁定**）；
    ⑦ 等什么：**合验两半（契约② §6 规则 6 + 第 25 条 (a)）**（谁解锁：主控发令；泵已在 `0dceb09`，
    前提齐了，但那一跑要连 `cmd/core` 一起过，按 T-397 尾句我仍不先动它）。
    我名下没有欠着的动作：3 件改动都在 `core/internal/process/`，等入册。

  - 【以下五格是第 31 轮（09-30 14:46~16:01，T-392 生产者半 + T-397 三件：改号映射、0 命中判据复跑、k1 再现）写的，按 T-276 原样留档、不当现状读；其中 A 面那三个数已被第 32 轮换掉（130 → 135 是新增 5 枚用例，不是同一集合的波动），闭包指纹也从 `ec662dd6…` 换成第 32 轮那一枚——树动了，旧枚不再代表交付字节】
  - Stage: **第 31 轮 = T-392 的生产者半落地：`turn/finished` 的常态位置换成「send 回执之后」，三态分辨第一次
    能整组从真双进程身上指出来**。`sessionSend` 拆成两段：`accept` 只把这一发**开出来的轮次**交回来（CO-8 那一对
    证据帧仍在回执之前——回执敢声称的东西必须已经在线上，CO-7 没让路），收尾改由新的 `settle()` 在
    `f.reply(...)` 之后发；`debug/hold` 的**解除**多带一个可选 `code`，于是「有帧但码未裁」这一格不再只能靠
    in-process 替身演（`turn/finished` 缺 `result_code` 会被 `NewStream` 拒，所以「未裁」在这条线上只能是表外值，
    这一点写进注释免得下个人去追一个发不出来的形状）。**CO-25 的排干点一字未动**（排水仍挂在 `finish` 那一帧上，
    `k8` 摘掉它立刻红）。消费者侧新增 `TestLateTurnFinishedClosesTheTurnItBelongsTo`：帧到之前 tracker 是
    `RUNNING`/`""`（回执处不许替 Provider 编终态），帧到之后落 `CLOSED`，表外码 `INTERRUPTED` 原样递出去、
    不被泵洗成成功。变异 **k1..k8 全 FIRED**（k1 就是把收尾搬回回执之前＝已否的甲，红在
    「1 turn/finished arrived before reply#r1」那一句上；k7 证明 guide 那一腿单独有人证）。
    两面绿分两面写：**A 面 130 PASS / 2 SKIP / 0 FAIL、三包 ok**；B 面这一跑 **不是全量绿** —— `api/http` 与
    `cmd/core` **build failed**，落点是 backend 正在落的 T-391 那根泵（`cmd/core/sendreap.go` 已删、
    `cmd/core/turnpump.go` 未跟踪），另 `core/test` 名册 1 条红（`task_accepted`/`task_paused`/`task_preparing`/
    `task_queued`/`task_resumed` 五名，扫描落点全在 `internal/task/service.go` 与契约名册）。**整份 B 面日志里没有
    一行点到我这 4 个文件**，我也不替他们跑。
  - Status: **idle-ready；本轮读数全部取于 HEAD=`264d562`（14:51 主控那一笔之后），未入册**。
    被验面带我 4 件（1 非测试 + 3 测试，`+298/−26`），另有 backend/audit/test 在途 18 件；
    `cmd/core` 那两件的编不过**不是**我的红，但它是两半的接缝，写在 CO-56。
  - Last verified action: **交付前最后一次重跑 = 15:15:18→15:15:29 A 面**（`faceA-r31-final.log`，`FINAL_RC=0`、**130 PASS / 2 SKIP / 0 FAIL、三包全 ok**，与 15:09:27 那一次同形状；五条新判据逐条 `--- PASS:`：`…ClosingFrameFollowsItsReceipt` :0.02 / `…TurnFinishThreeStatesOnTheWire` :0.01 / `TestLateTurnFinishedClosesTheTurnItBelongsTo` :0.90 / `…StartNowClosesItsRun` :0.01 / `…QueueDrainsAtTurnFinishedNotAtNextSend` :0.01）。**15:15:37 复算闭包指纹仍是 `ec662dd69c073989781c31b239b2e275`**（与 15:08:49 同值 ⇒ 两次读数同一棵树；但整仓未提交从 21 处涨到 **27 处**，增出来的全在 backend 名下：`cmd/core/turnpump_test.go` 与 `cmd/core/turnpump_late_test.go` 两枚 ??——**那枚 `_late` 正是 T-391 判据 (a) 的落点，我这一半的生产者形状与它咬合，但它跑不跑得起不在我账上**）。下面先给 15:09:27→15:09:37 A 面（`go test -count=1 -timeout 300s -v
    ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`，日志 `faceA-r31.log`）
    **exit=0、顶层 130 PASS / 2 SKIP / 0 FAIL、三包全 ok**；
    **15:07:53→15:08:10 B 面**（`go test -count=1 -timeout 300s ./...`，日志 `faceB-r31.log`）
    `ALL_RC=1`、**15 包 ok + 2 包 build failed + `core/test` 1 红**，`grep -n "fake-adapter\|internal.process"`
    在该日志里只命中两行 `ok`（计数 0 条 FAIL）。
    **15:05:16→15:07:21 变异批** `python .scratch/adapter/mut-r31.py`：基线两包 **77 PASS / 0 FAIL**、
    八枚锚点命中全 = 1、**8/8 FIRED**、每枚输出与基线非逐字相同、`main.go` 按字节还原
    （PRE-SHA = POST-SHA = `c11aaf2ffe55adbf`，ZMUT 残留 0、CR 0）。
    指纹（现跑 `bash scripts/build_stamp.sh`，**16 位旧枚一律作废**）：
    闭包 `[./internal/process ./cmd/fake-adapter ./internal/adapter]` = **`ec662dd69c073989781c31b239b2e275`**
    （15:08:49 与 15:09:48 两次同值 ⇒ 读数与被验树同一棵），单包 `internal/process`
    **`681226c050b7832fca1184766ae5e4af`**、`cmd/fake-adapter` **`85a8962766bc02bbfaa1a7d1e3eafd9d`**、
    `internal/adapter` **`4d8e65bdfbc1380e90370d93b5663794`**（15:09:46~47，HEAD `264d562`，
    工作树未提交 21→22 处——增量在 `internal/task`/`cmd/core` 那一侧，不在我闭包里）。
    **主控 T-392 里引的那一对（`33d54df2a154e3ee` / `46377e8752bf1298`）是我第 29 轮的旧枚，不是今天的树；
    今天这棵树上交账用的是上面那三枚 32 位值（build_stamp 截 32 位，与旧 16 位枚不可对拍）。** "全" = A 面 3 包 / B 面 17 包（15 ok + 2 build failed），
    **不含 `core/test/` 与 `scripts/`**。
  - Verification: `## Verification evidence` 顶部 = **"第 31 轮（T-392 生产者半：收尾在回执之后 + 三态分辨）"** ——
    `accept`/`settle` 的分段为什么这么切、`code` 为什么只挂在解除上、`session/stop` 那一格的顺序**故意没跟着搬**
    （它的回执声称"停了"，帧是它的证据，与 send 回执方向相反）、三态各自钉在哪一条用例、k1..k8 判决表、
    两面读数与两个时刻的脏件归属、B 面那两处 build failed 的原文都在里面。当场可复跑：
    `python .scratch/adapter/mut-r31.py`、`bash scripts/build_stamp.sh ./internal/process ./cmd/fake-adapter ./internal/adapter`。
  - Next action: **idle-ready，等主控点名**。六格"等"写全：
    ① 等什么：**本轮（第 31 轮）被复跑/入册**（谁解锁：主控；当场可复量
    `cd core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -timeout 300s ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`）；
    ② 等什么：**§10「故障注入（`debug/*`）」表里 `debug/hold` 那一行补上解除侧的 `code` 参数**（谁解锁：主控，
    `specs/**` 归他；拟稿在第 31 轮正文，T-275 的豁免只覆盖"这枚端点存在于 fake-adapter"）；
    ③ 等什么：**backend 把 T-391 那根常驻泵落完**（谁解锁：backend。现量：`cmd/core` 与 `api/http` 此刻 build failed）。
    接缝写在 CO-56：**我的常态位置要求收件方在回执之后还在听**——`reapTurnFinished` 那种"回执后同步扫一次缓冲"
    在新顺序下必然 `Seen=false`。这不是我这一跑的红，但**两半只落一半就是红的**，所以别把我的绿当成可以单独入册；
    ④ 等什么：**CO-55 裁"能力声明与可达方法的一致性要不要立判据"**（谁解锁：主控；h4 已证今天没人查表）；
    ⑤ 等什么：**§22 `adapter_downgraded` 发射点的点名**（谁解锁：主控；归我但不排在闸门之前）；
    ⑥ 等什么：**`REQ-TURN-1` 的契约文本**（谁解锁：主控）。本轮我钉住的是**生产者侧的常态位置**（回执之后），
    这不等于"v0.1 未定义"那一格已经裁完——**我不拿形状冒充裁定**，也不拿它去填 Wave-3 那格。
    我名下没有欠着的动作：4 件改动都在 `core/cmd/fake-adapter/` 与我包的一条消费者用例里，等入册。

  - 【以下五格是第 30 轮（09-27 16:52~17:13，T-386 丙：`debug/hold` + 即时收尾）写的，按 T-276 原样留档、不当现状读；其中「两面绿」那两个数与两枚 16 位指纹已被第 31 轮作废，「即时收尾」那个位置本身被 T-392 ① 换掉了】
  - Stage: **第 30 轮 = T-386 的"丙"落地：`debug/hold` 存在且只推迟收尾，`start_now`/`guide` 默认发
    `turn/finished{SUCCEEDED}`**。③Ⅰ 现在有专名用例钉在真子进程的原始线节点上；
    "解除那一刻才是收尾点"另有一条钉住（否则 `on:false` 无人证）。
    定形并留了反例的一处：**排干出来的轮次不受 hold 影响**（让 hold 吞掉排干＝移动 CO-25 的排干点，正是甲被否的原因）。
    变异两包同跑（基线 74 PASS/0 FAIL）= **h1/h2/h3/h5 FIRED、h4 SILENT 且已查到机制**
    （`Handshake.Call` 不查能力表 ⇒ 那行声明不是闸门）⇒ 开 CO-55 请裁要不要立判据。
    两面绿：A 面 **121 PASS / 2 SKIP / 0 FAIL**（三包 ok）、B 面 **544 PASS / 8 SKIP / 0 FAIL、17 包 ok**；
    指纹换成 **`internal/process be4ef95cdea707dc`**、**`cmd/fake-adapter baa270cd1cba56bd`**（第 29 轮那两枚作废）。
  - Status: **idle-ready；本轮未入册，等复跑/入册 + §10 补第五枚那一行**。被验面带我 5 件，另有 backend/test 13 件。
  - Last verified action: **17:10:01→17:10:26 两面各一次**（`bash .scratch/adapter/delivery-r25.sh` +
    `go test -v` 三包，日志 `delivery-r25-171001.log` / `faceA-r386.log`）：A 面 `exit=0`、顶层 **121 PASS / 2 SKIP / 0 FAIL**、
    三包全 ok、我这 5 条关键用例在 :26/:30/:217/:227/:229 全 `--- PASS:`；
    B 面 **544 PASS / 8 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**。
    指纹 **`internal/process = be4ef95cdea707dc`**、**`cmd/fake-adapter = baa270cd1cba56bd`**（构建时刻 17:10:03），
    `deps 67a1287e29df29c3`、`internal/adapter afbc9a1b8fa9b74f` 未变；HEAD `cde8d79`。
    变异 `mut-r386.py` + h3 补跑：锚点命中全=1、每枚按字节还原并 assert sha，
    **`main.go` 现读 sha `26a0604493beab5c` 与变异批 PRE-SHA 同值 ⇒ 判据证在交付字节上**。
    ZMUT = 0、`zz_*` 残留 0。**"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
  - Verification: `## Verification evidence` 顶部 = **"第 30 轮（T-386 丙：`debug/hold` + 即时收尾）"** ——
    落地形状、"排干不受 hold 影响"这条定形与它的反例、③ 三条现在各自钉在哪、五行变异判决表
    （含 h4 SILENT 的机制核查）、§10 拟稿那一行、REQ-TURN-1 的记账口径、两面读数与两个时刻的脏件清单都在里面；
    往下是第 29 轮（T-382 ②、岔口与那条流程教训）、第 28 轮（T-362 对拍）、第 27 轮补、第 27 轮（停摆后复量）。
    本轮真跑过的判据命令：`python .scratch/adapter/mut-r386.py`（+ h3 补跑那一枚）、
    `bash .scratch/adapter/delivery-r25.sh`、
    `cd core && CGO_ENABLED=0 go test -count=1 -v ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`。
  - Next action: **idle-ready，等主控点名**。六格"等"写全：
    ① 等什么：**本轮（第 30 轮）被复跑/入册**（谁解锁：主控；当场可复量 `bash .scratch/adapter/delivery-r25.sh`）；
    ② 等什么：**§10 的"故障注入（`debug/*`）"表补上第五枚 `debug/hold` 那一行**（谁解锁：主控，`specs/**` 归他；
    拟稿在第 30 轮正文）；
    ③ 等什么：**CO-55 裁"要不要把能力声明与可达方法的一致性做成判据"**（谁解锁：主控。h4 证明今天没人查表；
    我不擅自往 `Handshake.Call` 加门，那会改变所有能力的调用语义）；
    ④ 等什么：**backend 复跑端到端那一腿**（挂住留 `RUNNING`+503、崩溃不许写成功；谁解锁：backend，
    按 T-382④ 不替它跑）+ **backend 把 `cmd/core/processpipe.go:228` 改成调用 `ChildAlreadyGone`**（CO-51 收尾）；
    ⑤ 等什么：**Wave-2 闸门之后 `adapter_downgraded` 发射点的点名**（§22 归我但不排在闸门之前；谁解锁：主控；
    判据是"降级那一刻发一条、`effective == requested` 时不发"两侧都有用例）；
    ⑥ 等什么：**`REQ-TURN-1`**（`turn/finished` 与 send 回执的先后时序在 v0.1 未定义；谁解锁：主控，
    排 Wave-2 之后、真 adapter 接入之前）。**我不拿本轮的绿去填它。**
    我名下没有欠着的动作：五件改动都在 `core/cmd/fake-adapter/` 与我包的两条消费者用例里，等入册。
  - 【以下五格是第 29 轮（09-27 16:12~16:33，T-382）写的，按 T-276 原样留档、不当现状读；其中「① 未落 / Ⅰ 交不了」那两句已被第 30 轮取代】
  - Stage: **第 29 轮 = T-382 的 ② 两半落地、① 交回一个岔口（CO-54）**。
    关键发现：**被抢占那一帧同时是 CO-25 的排干点** —— 只删帧会把排在它后面的队列饿死
    （实测 `dq2`/`dq3` 一线节都没有），所以我把"一轮结束"与"发 turn 级码"拆开：排水留、帧不发。
    `"OK"` → `SUCCEEDED`（按名字对齐 §6 第 6 条那一份表，不抄第二份）。消费者侧
    `TestConnectionSendOverRealPipe` 按裁定改写并**把"没有那一帧"做成断言**。
    ① 我实现过、也回退了：即时收尾会同时撞掉 §5 转向 / §5 stop / CO-25 / §6 规则 5 四个现裁形状（5 条红），
    三条可选形状与代价都在 CO-54。变异 **n1..n4 全 FIRED**，其中 **n4 跨包**（恢复被禁的那一帧 ⇒ fake 侧与我包侧同时红）。
    两面绿：A 面 119 PASS / 2 SKIP / 0 FAIL（三包 ok），B 面 541 PASS / 8 SKIP / 0 FAIL、17 包 ok（仍含别家在途件）。
  - Status: **idle-ready；① 等 CO-54 裁定，其余无欠账**。被验面带我 4 件在途（未入册），另有 backend 约 10 件在途。
  - Last verified action: **16:29:48→16:30:09 两面各一次**：A 面（`./internal/process/... ./internal/adapter/...
    ./cmd/fake-adapter/...`）`exit=0`、顶层 **119 PASS / 2 SKIP / 0 FAIL**、三包全 ok；
    B 面整仓 **541 PASS / 8 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**（`delivery-r25-162946.log`）。
    指纹 **`cmd/fake-adapter = 46377e8752bf1298`**、**`internal/process = 33d54df2a154e3ee`**（旧 `397a7b999dc89720` 作废），
    `deps 67a1287e29df29c3`、`internal/adapter afbc9a1b8fa9b74f` 未变。HEAD `f1558cc`。
    变异 `mut-r38.py` + `mut-r38b.py`：基线两包 72 PASS / 0 FAIL、锚点命中全 = 1、**4/4 FIRED**、按字节还原 assert sha。
    ZMUT = 0、`zz_*` 残留 0。**"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
  - Verification: `## Verification evidence` 顶部 = **"第 29 轮（T-382）"** —— ②-b 那个排干点发现、① 的回退与三条
    可选形状、断言改写的理由、n1..n4 判决表（含 n4 跨包那枚）、两面读数与脏件清单、以及"改 fake 必须连消费者包一起跑"
    那条流程教训都在里面。可当场复跑：`bash .scratch/adapter/delivery-r25.sh`、
    `python .scratch/adapter/mut-r38.py`、`python .scratch/adapter/mut-r38b.py`。
  - Next action: **等 CO-54 裁定**（"一个 `start_now` 轮次何时算跑完"），裁完我才能把 ① 与 ③Ⅰ 交出去；
    裁之前我不拿"队列那一路也发 `SUCCEEDED`"冒充。其余四格同前（本步入册、§22 发射点、`core/test/` 端到端、
    backend 若开口我陪一次共享 `gateTurn`）。另按 ④ 请转告 backend：**端到端那一腿现在可以复跑了**。
  - 【以下五格是第 28 轮（09-27 13:44~13:53，T-362/T-382 之间）写的，按 T-276 原样留档、不当现状读】
  - Stage: **第 28 轮 = T-362 三句都办了：CO-51 对拍 + 单一来源导出、CO-53 落点按小节名指认、第 11 格没碰**。
    对拍是机械的：两份 `childAlreadyGone` 函数体**逐字节相同**（212B / sha `7364ae9e5ca630a8`）⇒ 语义一致 ⇒
    我包内那枚改名导出 **`ChildAlreadyGone`** 作唯一来源，`cmd/core` 那份等 backend 改调用（我不碰他们面包）。
    新增 `TestChildAlreadyGoneAnswersBothQuestions`（两问分开判），本步变异 **m6a/m6b/m6c 全 FIRED**；
    **A 面 `exit=0`、119 PASS / 2 SKIP / 0 FAIL**，我那 8 条用例逐条 `--- PASS:`；
    B 面整仓 525 PASS / 0 FAIL 但**含 backend 3 改 + test 1 新**，只记"这一跑 0 红"、不写全仓绿。
    **`internal/process` 指纹换成 `397a7b999dc89720`**（这次是真改码，旧枚 `9c750c42077d17c0` 失效）。
  - Status: **idle-ready，无阻塞、无欠账**；CO-51 从"等裁"转"等 backend 一次改调用"。
    被验面带我 4 件在途（本步改动**未入册**），另外 3 改 1 新是 backend / test 的。
  - Last verified action: **13:50:36→13:50:54 两面各一次**：A 面（我的三包）`exit=0`、
    顶层 **119 PASS / 2 SKIP / 0 FAIL**、8 条我的用例在 `faceA-r28.log` :32/:34/:36/:38/:42/:44/:46/:97
    全 `--- PASS:`；B 面整仓 **17 包全 ok / 0 FAIL / build failed 0 / panic 0**（`delivery-r25-135034.log`，
    HEAD 跑前跑后同 `9660e68`）。指纹 **`internal/process = 397a7b999dc89720`**（13:50:36 构建）、
    其余三面未变。变异批 `mut-r28.py`：基线 46 PASS / 0 FAIL、锚点命中全 = 1、**3/3 FIRED**、
    两份被改文件按字节还原且 sha 与跑前同值。ZMUT = 0、`zz_*` 残留 0。"全" = 17 包，不含 `core/test/` 与 `scripts/`。
  - Verification: `## Verification evidence` 顶部 = **"第 28 轮（T-362）"** —— 对拍的命令原文与两个 sha/字节数、
    单一来源的归属交代、新用例的两问设计（以及第三问为什么不放单测）、m6a/m6b/m6c 三枚判决、
    两面读数与两个时刻的脏件清单都在里面；往下是第 27 轮补（CO-53 实测 + 三条腿回报）、第 27 轮（停摆后复量）、
    第 26 轮（第二批 5 FIRED）。本步可当场复跑：`bash .scratch/adapter/delivery-r25.sh`、
    `python .scratch/adapter/mut-r28.py`、`cmp -s <(sed -n '/^func ChildAlreadyGone/,/^}/p' …) <(…)`。
  - Next action: **idle-ready，等主控点名**。五格"等"写全：
    ① 等什么：**backend 把 `cmd/core/processpipe.go:228` 那枚副本改成调用 `ChildAlreadyGone`**
    （谁解锁：backend，T-361⑤/T-362① 已派；我不碰那三个文件）；
    ② 等什么：**本步（第 28 轮）被复跑/入册**（谁解锁：主控；当场可复量 `bash .scratch/adapter/delivery-r25.sh`）；
    ③ 等什么：**Wave-2 闸门之后 `adapter_downgraded` 发射点的点名**（§22 归我但不排在闸门之前；谁解锁：主控；
    点名时我要的判据是"降级那一刻发一条、`effective == requested` 时不发"两侧都有用例）；
    ④ 等什么：`core/test/` 六类全量的端到端复核（`turn_evidence_test.go`，谁解锁：`test`，我不代跑也不算进自己的绿）；
    ⑤ 等什么：**backend 若开口，我陪一次那枚共享 `gateTurn` 的间歇挂**（谁解锁：backend；我不进他们文件）。
    我名下没有欠着的动作：本步四件全在 `core/internal/process/`，等入册。
  - 【以下五格是第 27 轮 / 第 27 轮补（09-27 12:32~13:09）写的，按 T-276 原样留档、不当现状读】
  - Stage: **第 27 轮 = 停摆 47h 后的盘上复量（09-27 12:49），零代码改动、零新判据**。
    结论三句：**两批都已在 HEAD**（第一批 `ba03ce2`、第二批 `ae26c61` = `connection.go +63/−8` +
    `connection_test.go +108/0`）；**我的绿 = A 面三包**（`internal/process`/`internal/adapter`/`cmd/fake-adapter`，
    12:51:40→47、`exit=0`、118 PASS / 2 SKIP / 0 FAIL、我那 7 条用例逐条 `--- PASS:`）；
    **整仓那一跑（17 包 ok）只记"这一跑 0 红"、不写成全仓绿** —— 它含 backend / test 的未提交件（T-343 补口径 12:36）；
    **`internal/process` 指纹 `9c750c42077d17c0` 与 09-25 交付时一字不差**
    ⇒ 这两天没人动我的包，我名下 dirty = 0。期间我看到 backend 在落 REQ-S-1 停摆那一半（四件在途 + 一枚新测试）。
  - Status: **idle-ready，无阻塞、无欠账**。我名下三个可写路径全干净；`core/` 现在的脏件全是 backend / test 的。
  - Last verified action: **12:51:40→12:51:59 两面分取**（`bash .scratch/adapter/two-face-r27.sh`，
    一次跑完 A/B、同一时刻读 HEAD 与 porcelain）：**A 面 = 我的三包 `exit=0`、顶层 118 PASS / 2 SKIP / 0 FAIL**
    （`faceA-r27.log`，我那 7 条用例在 :32/:34/:36/:38/:42/:44/:46 全 `--- PASS:`）、
    **B 面 = 整仓 17 包 ok / 包 FAIL 0**（12:51:50→59；该跑没带 `-v`，所以顶层计数是 0，不是没跑）。
    两面同处 HEAD `67f064b`。**"全仓绿"这个写法我 12:36 之后不再用**：被验面此刻有 4 改 2 新全是 backend / test 的。
    更早 12:48:49→59 那次整仓（`delivery-r25-124848.log`、521 PASS / 7 SKIP / 0 FAIL）同属 B 面性质。三面指纹 `deps 67a1287e29df29c3` / `afbc9a1b8fa9b74f` / `38a594aa65efbca8` /
    **`internal/process 9c750c42077d17c0`（构建时刻 12:48:49）** —— 后三枚与 09-25 那两批同值。
    **前一次 12:32:45→12:42:52 那份是 `exit=1`（16 包 ok、`api/http` 整包 `panic: test timed out after 10m0s`）**，
    挂点在 backend 在途件的 `gateTurn` 替身上、且他们 12:46:07 还在写 ⇒ **我没记成他们的红**，12:48 同命令已过。
    我这 7 条用例在该 log 里逐条 `--- PASS:`（行号 :821/:823/:825/:827/:831/:833/:835，见第 27 轮正文）。
    **"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
  - Verification: `## Verification evidence` 顶部 = **"第 27 轮（09-27 停摆后复量）"** —— 两批的 `git show --numstat`
    原文、7 条用例的行号与行首前缀、两份全量（红的与绿的）、"没报出去的那条红"的完整形状与两条分辨命令都在里面；
    往下是第 26 轮补 2 / 第 26 轮补 / 第 26 轮（第二批 5 FIRED）、第 25 轮补、第 25 轮（T-314 三处点名 + ⑤）。
    判据命令今天真跑过：`bash .scratch/adapter/delivery-r25.sh`（两份日志）、
    `cd core && CGO_ENABLED=0 go test -count=1 -timeout 100s ./api/http/`（复挂用，归 backend）。
  - Next action: **idle-ready，等主控点名**。四格"等"写全（内容与 09-25 交账时同，只是今天重新按字复量过）：
    ① 等什么：**CO-51 裁"哪一份 `childAlreadyGone` 成唯一来源"**（谁解锁：主控；裁成包内这枚的话，
    backend 下一次动 `processpipe.go` 时改调用它 —— 我不碰那两个文件）；
    ② 等什么：**Wave-2 闸门之后 `adapter_downgraded` 发射点的点名**（§22 归我但不排在闸门之前；谁解锁：主控；
    点名时我要的判据是"降级那一刻发一条、`effective == requested` 时不发"两侧都有用例）；
    ③ 等什么：`core/test/` 六类全量的端到端复核（`turn_evidence_test.go`，谁解锁：`test`，我不代跑也不算进自己的绿）；
    ④ 等什么：**backend 若开口，我陪一次那枚共享 `gateTurn` 的间歇挂**（谁解锁：backend；我不进他们文件、不主动查，
    两条分辨命令与留档证据已在第 27 轮正文）。
    当场可复量任一格：`bash /d/Project/Lucero/.scratch/adapter/delivery-r25.sh`。
    我这边没有欠着的动作：`git status --porcelain -- core/internal/process/ core/internal/adapter/ core/cmd/fake-adapter/` = **0 条**。
  - 【以下五格是第 26 轮 / 第 26 轮补（09-25 21:2x~21:54）写的，按 T-276 原样留档、不当现状读】
  - Stage: **第 26 轮 = 第二批（可取消的 `write`，T-326③ 裁定形状）已落地并 5 FIRED/0 SILENT；第一批已在 `ba03ce2`**。
    形状一句话：写放在一枚**有界可放弃**的 goroutine 上、`select { 写完 / ctx.Done() / c.dead }`、
    超时**只标死连接**、回收仍归 `Close`（`sendpipe.go:76-80`、`discoverassembly.go:89` 两条腿都真的 defer 了 `Close`，
    我按字核过）。实测 **44.8055391s → 400.8158ms**（同一条探针命令、改前改后两份日志都在盘上）。
    **本轮最该被读的一句**：我在 `abandon` 里顺手写了 `c.closed = true`，而 `Close()` 顶上那句
    `if c.closed { return nil }` 会让它**早退不杀孩子** ⇒ 3 枚 goroutine 永久挂 + 孤儿，
    **正好把第一批刚闭的泄漏在那条新路上开回去**；是我为"§9.2 那句注释"补的泄漏用例抓的（两次差值稳定 = 3）。
    **21:4x 更正一格措辞**：巡检发现我把 `--- FAIL` 行的 30.44s 当耗时写进了 `Close` 的注释（已随 `ba03ce2` 入册），已重写为「慢且红、不是单步计时」，承重依据换成 `cmd/core` 先例；判据与结论未动，第二批证据整批重取过（5 FIRED 不变、交付跑 21:48:53 仍 `exit=0`）。
  - Status: **两批都交完、第二批未入册**；T-339 ⑤ 与我手里另两封给的排程**读出不一样**，已上交等主控处置（不撤、不自己入册）。被验面现带我 2 件 + backend 4 改 1 新，我这侧 21:44:48 起已停笔。
    被验面现带我 2 件 + backend 4 改 1 新。
  - Last verified action: **第二批更正注释后的重取 = 交付跑 21:48:42→21:48:53**
    （`bash .scratch/adapter/delivery-r25.sh`，日志 `delivery-r25-214842.log`），`exit=0`、
    **518 PASS / 7 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**；三面指纹与 21:38 那批**完全相同**
    （`internal/process 9c750c42077d17c0`、`deps 67a1287e29df29c3`、`afbc9a1b8fa9b74f`、`38a594aa65efbca8`）
    —— 因为这次注释是 **7 行换 7 行、没有位移行号**（上午那次净 +1 行就换了枚，见验证"第 26 轮补"）。
    `mut-r26.py` 在更正后的字节上整批重跑：基线 45 PASS/0 FAIL、锚点命中全 = 1、**m1..m5 仍 5 FIRED / 0 SILENT**、
    还原 sha 同值（`fb4982ac1a69a41e`，`mut-r26-out-214504.log`）。第一批三条 `TestClose*` 全程未红。
    **44.8 那一格现在有三份留档输出**（改前 44.8059183s / 44.8055391s、第二批后 400.8158ms），
    恢复成"可复跑事实"的那一步已按 §24 ⑤ 其四做完。**"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
  - Verification: `## Verification evidence` 顶部 = **"第 26 轮（第二批 = 可取消的 `write`）"** ——
    改前/改后成对读数、§9.2 原文核对、⑤ 那件自引入回归与它的抓法、第二批五条变异判决、交付跑三证都在里面；
    往下是第 25 轮补（注释降级 + 指纹换枚 + 那份被别家在途件吃掉的全量）、第 25 轮（T-314 三处点名 + ⑤）、
    第 24 轮（ctx 三段，"没有 Kill"那句已作废留档）。四条判据命令都真跑过：
    `bash .scratch/adapter/ctx-probe2.sh`、`python .scratch/adapter/mut-r26.py`、
    `bash .scratch/adapter/delivery-r25.sh`、`bash .scratch/adapter/ctx-probe3.sh`。
  - Next action: **idle-ready，等主控点名**。四格"等"写全：
    ① 等什么：**第二批（可取消的 `write`）被复跑/入册**（谁解锁：主控；当场可复量：
    `bash /d/Project/Lucero/.scratch/adapter/delivery-r25.sh` + `python .../mut-r26.py`）；
    ② 等什么：**CO-51 裁"哪一份 `childAlreadyGone` 成唯一来源"**（谁解锁：主控；裁成包内那枚的话，
    backend 下一次动 `processpipe.go` 时改调用它，我不碰那两个文件）；
    ③ 等什么：**Wave-2 闸门之后 `adapter_downgraded` 发射点的点名**（§22 归我但不排在闸门之前；谁解锁：主控；
    点名时我要的判据是"降级那一刻发一条、`effective == requested` 时不发"两侧都有用例）；
    ④ 等什么：`core/test/` 六类全量的端到端复核（`turn_evidence_test.go`，谁解锁：`test`，我不代跑也不算进自己的绿）。
    **REQ-S-2（`%15` 那格）按 T-328④ 登记为 P2 观察项：不排在 Wave-2 之前、不为它改 `Wait` 语义、今天不派活。**
    我这边没有欠着的动作：`git status --porcelain -- core/internal/process/` 只有我这 2 件、untracked **0 条**。

  - 【以下五格是第 25 轮 / 第 25 轮补（09-25 20:31~21:15）写的，按 T-276 原样留档、不当现状读】
  - Stage: **第 25 轮 = T-314 三处点名已量、⑤ 的 `Close→Stop` 已落地（09-25 20:55 交付跑绿）**。
    三处各一句：**(a) `Start`/`Connect` 不读 ctx**（已取消仍花 588ms 起一个没人负责的孩子）、
    **(b) `Cancel` 签名里没有 ctx ⇒ `request` 的 ctx 分支之后还有第二道不可取消步骤**（管道已满时 3s 窗内未返回）、
    **(c) `Close` 从"瞬间返回 + 孩子存活"变成"2ms + 孩子已收走"，并解开那次卡住的 write**。
    **⑤ 落地时量出两件必须一起处理的缺陷**：`Stop` 要排在还管道**之前**（否则就是那一次 30.44s 的带载红）、
    孩子自己先死时 `Kill` 给 `kill: invalid argument`（⇒ 正常收尾被报成失败）⇒ 复刻 `cmd/core` 的
    `childAlreadyGone`，两份归一开成 **CO-51**。
    **21:1x 接令**：T-326③ 已裁 `write` 那一格（放进可放弃的 goroutine、`select { 写完/ctx.Done()/c.dead }`、
    超时标死连接交给 `Close`），**明确排在『本批交账之后』、且两批判据不许合成一批** ⇒ 那是我的下一批、不是这一批。
  - Status: **已交账，回到 ready（无阻塞、无欠账）**。**被验面带着我自己的 4 件脏件**
    （T-321② 已按要求明写：这一批读的就是这 4 件在制品，不是干净面）。
  - Last verified action: **新字节上我能归因的绿 = `internal/process` 单包，21:11:54**
    （`go test -count=1 -v ./internal/process/` → `exit=0`、**顶层 `--- PASS` 36 / SKIP 1 / FAIL 0**；
    连子用例那一面是 42，两面别混用；`Close` 四条用例全 PASS）。
    **17 包全绿那一份 = 21:04:28→21:04:39（508 PASS / 7 SKIP / 0 FAIL，porcelain 只有我那 4 件）**；
    其后 21:10:30→21:10:40 复取 **`exit=1`**、`包 ok` = **16** —— `api/http` 因 backend 未跟踪在制品
    `tasks_get_test.go`（`strconv` 未用 / `httptest` 未定义 ×3 / `itoa` 未定义）**整包 build failed**，
    按 T-323② 我不引用别家那份绿、也不把这条红算进自己。
    **`internal/process` 指纹再换一枚：`c64734a5309c1a89`（构建时刻 21:10:30），`341c5cd8fce19e0f` 作废**
    （改注释也挪行号表 ⇒ 哈希变；"只改注释 ≠ 哈希不变"）。另三面未变
    （`deps 67a1287e29df29c3`、`afbc9a1b8fa9b74f`、`38a594aa65efbca8`）。
    变异批 `mut-r25.py`（**在 21:10 的注释降级之前跑的**）：基线 42/0、锚点命中全 = 1、
    m1/m5 FIRED、m2/m3/m4 SILENT、还原 sha 同值。"全" = 17 包，不含 `core/test/` 与 `scripts/`。
  - Verification: `## Verification evidence` 顶部 = **"第 25 轮（T-314 三处点名 + ⑤ 落地）"** ——
    那张"三处 × {吃不吃 ctx、卡死孩子下耗时、孩子去向}"表、两件新缺陷、五行变异判决表、交付跑三证都在里面；
    往下依次是第 24 轮（ctx 三段量测，**其中"没有 Kill"那句本轮已按字作废、原文留着**）、第 23 轮（空机复跑）、
    第 22 轮（合验）。四条判据命令全部真跑过：`ctx-probe3.sh`、`ctx-probe4.sh`、`mut-r25.py`、
    `delivery-r25.sh`（都在 `.scratch/adapter/`）。
  - Next action: **idle-ready，等主控点名**。四格"等"写全：
    ① 等什么：**CO-51 裁"哪一份 `childAlreadyGone` 成唯一来源"**（谁解锁：主控；解锁后那一步：
    若裁成包内这一枚，backend 下一次动 `processpipe.go` 时改调用它 —— 我不碰那两个文件）；
    ② 等什么：**CO-52 —— `request` 的 ctx 分支里 `c.Cancel(id)` 这道第二不可取消步骤要不要界住**
    （谁解锁：主控。加期限/改异步都是 §23 的形状决策，三个可选形状在 CO-52 正文里，我只在注释里点名、没动行为；实测数字在正文 ①）；
    ③ 等什么：**Wave-2 闸门之后 `adapter_downgraded` 发射点的点名**（§22 归我但不排在闸门之前；谁解锁：主控；
    点名时我要的判据是"降级那一刻发一条、`effective == requested` 时不发"两侧都有用例）；
    ④ 等什么：`core/test/` 六类全量的端到端复核（`turn_evidence_test.go`，谁解锁：`test`，我不代跑也不算进自己的绿）。
    解锁后任一格都能当场复量这一条：`bash /d/Project/Lucero/.scratch/adapter/delivery-r25.sh`。
    我这边没有欠着的动作：`git status --porcelain -- core/` 只有我自己这 4 件、untracked **0 条**。
  - 【以下五格（`Stage`/`Status`/`Last verified action`/`Verification`/`Next action`）是第 24 轮
    （09-25 20:06~20:09）写的，按 T-276 **原样留档、不当现状读**】

  - Stage: **第 24 轮 = T-310 的 ctx 量测已交（09-25 20:09）；三张交付仍在盘上，本轮零交付改动**。
    量的结论：**不可取消那一段是 `Start`/`Connect`** —— 传**已取消**的 ctx 进去，`Start` 仍花 **588ms** 真起一个孩子、
    `err=nil`、孩子存活；`Connect` 本体 **2.25ms** 且**不等 hello**（主控 ① 那句前提我纠正了）。
    **`request` 腿安全**：400ms 期限 → **400.31ms** 返回、err 带 `sent $/cancel`、`pending` 归 0 不泄漏。
    **三条路径都不收孩子**（`Child` 只有 `Wait`/`CloseStreams`，没有 `Kill`）。
  - Status: **已交账，回到 ready（无阻塞、无欠账）**。
  - Last verified action: 探针跑窗 **20:09:08→20:09:12**（`bash /d/Project/Lucero/.scratch/adapter/ctx-probe.sh`，
    临时件 `trap` 删除、包内残留 = 0，被验的 `internal/process` 零脏件）。
    **空机复跑没欠**（T-310 ④）：19:43:15→19:43:28 与 19:52:50→19:53:02 两批，与带载的 19:12 那批
    **数字全同**（`exit=0`、498 PASS / 7 SKIP / 0 FAIL、17 包全 ok）且三面指纹一字未变
    （`deps 67a1287e29df29c3`、`afbc9a1b8fa9b74f`/`c6a53803aa7ce5c1`/`38a594aa65efbca8`）。
    **20:06:00 复量后不再取第四份全量**：`git status --porcelain -- core/` 现读 **7 条**（backend 在写
    `internal/notification`/`api/http`，最新 mtime 20:0x）⇒ 现在跑就是拿正在被改的树做合验，
    **这次拦住我的是我自己那条 `idle_probe()` 判据**。**"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
  - Verification: `## Verification evidence` 顶部 = **"第 24 轮（T-310 ctx 量测）"** ——
    那张"三段 × {吃不吃 ctx、实测耗时、孩子去向}"的表 + 可复跑命令都在里面；往下依次是第 23 轮（空机复跑三批）、
    第 22 轮（合验交账）、第 21 轮（T-295 口径）。
  - Next action: **idle-ready，等主控点名**。三格"等"写全：
    ⓪ **REQ-S-1 的两段落点**（归 backend：两段式 `Connect`）——我的读数替他定位了**不可取消那一段是 `Start`/`Connect`、不是 `request`**；
    谁解锁：backend/主控。**我不会去改 `cmd/core`，也不会擅自给 `internal/process` 加"顺手杀进程"的行为**——
    那属于 §23 的形状决策（谁杀、何时杀、失败怎么喊），要主控裁；
    ① 等什么：**Wave-2 闸门之后 `adapter_downgraded` 发射点的点名**（§22 归我，但明确不排在闸门之前；谁解锁：主控；
    点名时我要的判据是"降级那一刻发一条、`effective == requested` 时不发"两侧都有用例）；
    ② 等什么：`core/test/` 六类全量的端到端复核（`turn_evidence_test.go`，谁解锁：`test`，我不代跑也不算进自己的绿）；
    ③ 等什么：若还要一份空机读数，**重跑同一条命令即可**（我不改任何人的文件去凑干净）。
    我这边没有欠着的动作：三门交付都在 HEAD、`git status --porcelain -- <我三路径>` = **0 条**。
  - （**旧 `Next action`，09-24 10:54 写的，按 T-276 留档不当现状**；live 的那条在上面）：
  "idle。三格'等'，每格都写谁解锁"：① **等主控发合验令** —— 等什么：`test` 侧的 Wave-2 合验开跑；谁解锁：主控；
  ② **等 backend 补终态边判据** —— 等什么：§1.1 第 17 条"三条腿 + 每腿反例"里
  "改行腿在装配好的形状下被驱动"那一腿（10:46 复量：`cmd/core` 里 `RecordExit/RecordCrash` 只 1 处、且那是 nil-store 支）；
  谁解锁：backend（配方在他手里：先 start 再落边、断 `status`/`exited_at`/`exit_code`，反例 = 摘掉 `TouchTransition` 那条腿必须红）；
  ③ **等 backend 用 `ResolveInstance` 接完分派腿** —— 谁解锁：backend，我不催、不替他接。
  **不需要我再动码的任何一件**；T-275 那扇门已交付（默认关、只在 fake、不改帧形状，用法见第 18 轮）。
- 【以下三段（旧 `Stage` / 旧 `Status` / 旧 `Last verified action`，写于 09-22 22:5x）按 T-276 **原样留档、不当现状读**：
  巡检 10:53:00 量到我头部比正文旧约 1.5 天，事实成立、我照规矩把新索引放上来、旧的搬下来，不删】

  - （**旧 `Stage`，09-21~22 写的，按 T-276 留档不当现状**）：第 5 轮 B 段完成 = **§10 的 11 项全部到齐**（新落地 `session/list`、`session/read`、`session/stop`、
  `model/list`、`approval/list`、`approval/respond`）+ §6 规则 1"必发面"补 `stream/state`/`stream/item`
  （六类通知各至少一帧，且**分"正常轮次段 / 收尾段"两段断言**——这个拆法是变异 n1 第一次 SILENT 逼出来的：
  `session/stop` 也发 `stream/state`，混在一起断言会被它顶包）+ §6.3 状态机链用例。
  另按 §4.2 留给 adapter 的两处矛盾下了结论（CO-32/CO-33）——**主控 12:45:43 commit `17ef3a7` 直接裁定了这两问**，
  且两处都是"代码要改"，所以第 5 轮 C 段就是把裁定落进代码（见 Completed 末条）。**CO-25 已由 T-110 裁定并落地**（排空点 = `turn/finished`，且**全文件只剩这一个排空点**）；
    **CO-34 四处已由主控 14:39 修完**，我代码里那两处"契约仍写四条 / §4.1 缺名"的注释随之改成实测现状。
    FakeAdapter 的 §4 C→A 面**已全部服务完毕**：`model/select`（第 6 轮 ①）与 `session/create`（第 6 轮 ③，REQ-T8 由主控 13:39 落 §4:88、签名照 CO-5 原样）都已落地，`TestFakeAdapterServesEveryContractMethod` 把这句话从账本里的声明变成运行时对拍。
  **盘上实况（15:02:15 重量，别拿上轮数字）**：契约② 现为 **474 行 / mtime 14:39:33**（CO-34 那一批），
  §4.2 的裁定段与表格已对齐（`turn/claimed` 载荷含 `turn_ref`、`turn/dispatched` 写"前三者必填 + 句柄可空"）、
  §6 规则 5 已写"扩到全部六条"、§4.1 写集合已含 `session/create` ⇒ **CO-34 四处全闭，我不再提**；
  `^### 6.6` 仍为 **0**，所以我继续引 `§6 规则 5` + `§4.2 裁定 ①/②`；
  §4 方法表**已有** `session/create` 一行（REQ-T8 闭），互校用例里那条豁免与"代码有契约没有"的状态一并撤掉，
  现在 `writeMethods` 的每个名字都必须能在 §4 找到、且 §4.1 声明的写必须都在我集合里（双向对拍，t5 会红）。
  - （**旧 `Status`**，留档）：in_progress
  - （**旧 `Last verified action`，09-22 22:5x 写的，按 T-276 留档不当现状**）：交付前最后一次全仓跑 **09-22 22:56:35 起 / 22:56:43 收**（`date` 实读），
  `-v` + `-timeout 900s` 的 `go test -count=1 ./internal/... ./cmd/... ./api/...` =
  **338 PASS / 6 SKIP / 1 FAIL、16 包 ok**，且**日志里没有 `panic: test timed out`** —— 这就是 T-132 的 P0
  （"probe 让 `go test ./...` 整仓挂死"）已经消除的直接证据，不是"我看它不挂了"。
  我三包逐包：`internal/adapter` **50 PASS / 0 / 0**（声明 50，probe 六条 + store 两条 + 丢弃接缝前的一条都在）、
  `internal/process` **24 PASS / 1 SKIP / 0 FAIL**（声明 25），`cmd/fake-adapter` 本轮未改。
  唯一那条 FAIL 是 `cmd/core/TestLauncherDeadlineBoundsReads`（`processpipe_test.go:124: Close 报错:
  kill: invalid argument`，backend 的文件与 Windows 上 kill 语义，不是我改的形状：那文件能编译通过，
  说明 Recorder 第四方法没有打断它的实现）。`gofmt -l` 我三目录空、`grep -rn ZMUT core/` = 0。
  **今天(09-22)我一度 6 小时没交报告**：15:19 被唤醒、15:54 还在写文件，账本停在 09-21 16:05 ——
  T-132 点得对，"代码在盘 ≠ 验收闭口"是我前天自己立的。这一段就是补账。
  - （**旧 `Next action`，08:44~10:46 写的，同样按 T-276 留档不当现状**；live 的那条在本节顶部）：
  "idle，等三件、没有一件要我再动码"。"等 backend 接 `cmd/core` 装配"与
  "等 `LUCERO_REQ_DB2=1` 转 PASS"**两格都已过**：08:42:56 `cmd/core` 整包带 flag `ok`，08:44:16→08:44:24 全量
  **457 PASS / 6 SKIP / 0 FAIL、17 包全跑、0 build failed**（08:13 那条 build-red 与 08:18 那条 panic 都不复存在）。
  现在等的是：**① `test` 侧的合验发令（主控）**；~~② backend 删掉那份字面量抄本~~ ⇒ **10:01 复量已闭**
  （`core/cmd/core/` 非测试里 `"adapter_instance"` 字面量 **0**、`OwnerTypeAdapterInstance` 引用 **2 处**）；
  **③ CO-50① 那条终态边判据落地**（T-248 判归 backend，我不动 `cmd/core`；08:43 复量的事实是
  "摘掉改行腿 ⇒ `cmd/core` 整包仍 `ok`"，配方已交给他们）。
  **④ 已交付、等 backend 用它**：T-263 要的只读解析出口 `(*SQLiteStore).ResolveInstance` 本轮落完
  （交付跑 10:06:19→10:06:28，三条用例 + 五条变异，见第 17 轮）；分派腿解析 `adapter_key` 现在不必再自己抄 SQL。
  **我不催，也不替它接。**
  **⑤ 已交付、等 `test` 用**：T-275 那扇门 10:46 落完（`LUCERO_FAKE_ADAPTER_BAD_EXECUTION_ID=1`，
  默认关、只在 fake、不改帧形状，正反例与 6 发变异都在第 18 轮）。**用法一句话**：起子进程时把这个环境变量设成 `1`，
  那条 `session/send` 的两帧取证信号里的 `execution_id` 就会与 Core 为同一张票绑定的值矛盾；
  子进程 stderr 会同时喊出"注入值"与"原本绑定的值"，`test` 不必自己反推。**我没有替 `test` 写那条 T84-7b 的断言**
  （那格在 `core/test/`，归他，T-273② 已直接通知到他）—— 我俩各实现一遍的形状就此避开。
  **合验那一刻我有一腿要陪验**（§17 现在明写三条腿，验收方是 backend + adapter）：摘我改行腿 ⇒ 对应用例必须红、
  且与基线两两不同——这一腿**要等 backend 先把"装配好的形状下驱动终态边"落下来**才存在，我今天量它还不存在（见第 16 轮 ②）。
  **报绿口径更新**：以后每一句"全仓绿"都必须带"= 17 包，**不含 `core/test/` 与 `scripts/`**"，
  并且别人报的红若在我日志里 0 命中，**先怀疑我的集合**（09:03 那次就是这么撞上的）。
  我 08:17/08:43 两轮双向自证的结论都在 `## Coordination` 的 CO-50 里，**`cmd/core` 的红绿由 backend 交账**，我不去改他们的文件。
  历史一句（08:14 那次写的，保留）：当时我以为整包红在 `TestLauncherCarriesARecorder` 需要等回来才重取读数——
  **它 08:42 就绿了，我等的是别人交付、不是等一个已经消失的现象**。
  **T-240 更正后的 Wave-2 我那一半已无需新形状**：`session/send` 下发 + `turn/claimed`/`turn/dispatched` 取证两帧
  都在盘上（`stream.go:31-32`、`send.go` 载荷校验、`CarriesTurnRef` 集合），**要连接生命周期或崩溃注入配合就开 CO，不自扩对外形状**（T-230 原话）。
  **09-24 07:2x 更新：禁令解除的那句话已经用完——`internal/process/lifecycle.go` 落完了**（`LifecycleStore` 三方法、
  9 条变异全 FIRED、全仓 exit=0 447 PASS），形状与消费点已写进 `## Coordination` 的 CO-46 转告 backend。
  **我不去碰 `cmd/core`、也不提前跑合验**（那两格分别是 backend 的与主控发令的）。协议侧那一半仍随时能交。
  **T-230 立的前置检查挂在这一格上**：backend（或主控）的方案带 Coordination 进我账本时，
  **动手前先对契约核"那个方法名/端点存不存在"**，0 命中就不动手、开 CO 等回话——第一次执行已经是 CO-48
  （`turn/dispatch` 在 `specs/` 全树 0 命中 ⇒ 我按 `session/send` 准备，不写迎合的代码）。
  **并且口径要写清**：我第一次用 `specs/*.md` 量的 0 命中是**假否定**（契约② 在 `specs/adapter-protocol/`），
  递归 `specs/` 才是这条检查的正确扫法。
  **以下两段是 05:2x/05:40 那一头的读数，原样留着（它们当时为真）**：等主控对 CO-44/CO-45 的一句话，
  以及 T-211 那条"等 db 交三个入口"的前置在 05:37:20 就已消失——**"前提消失 ≠ 禁令解除"这一条我执行到位了，主控也认了**。
  **05:2x 复量（本轮，`date` 两个端点 05:22:46 / 05:26:07）**：
  ① 第 ① 个入口 `CreateVersionless` 仍 **0 命中**（`grep -rn "CreateVersionless" core/ --include=*.go` = 0，
  含测试文件；它目前只存在于契约① `:327` 的签名和派发里）⇒ 建行那条腿没有门，我这格不动。
  ② 第 ②③ 个 `TouchTransition`(`touch.go:384`)/`AppendEventOnly`(`:446`) 在盘上，且
  `AggProcess`/`AggCore`(`:42-43`) 与 `versionlessAgg{process:"process_instances", guards:{status}, core:""}`(`:68-69`)
  也已经出现 ⇒ **上一轮"db 那五个名字 0 命中"现在只对第 ① 个成立**，引用它的人要按这一条改口。
  ③ db 仍在写：`touch.go` mtime **05:22:45(21814B) → 05:22:56(21790B)**，另有未跟踪新文件
  `versionless_test.go`(05:21:41)；`git status --porcelain core/internal/platform/db/` = `M touch.go` + `?? versionless_test.go`。
  ⇒ 顺序"db → 我 → backend"里我这一格仍未解锁。本轮我只做了两件不落实现的事：
  把 `supervisor.go` 那段**比代码强、并且引用了一条不存在的契约条款**的注释改成实测现状（见 Completed 末条），
  以及把写口的形状预研记进账本——CO-44/CO-45 两问未裁，所以**一行实现都没写**。
  **以下是 05:13:39 那一轮的读数，原样留着（它当时为真）；只有①里"那两个入口"这半句被 T-211/213 的"三个入口"取代**：
  **05:13:39 我自己复量后，T-209 那句等待条件要说得更准**（主控的"不发令"结论不变，但①这一半的依据已经过期）：
  ① "等 db 交第 17 条那两个入口"——**正在交，还没停**：`tx.TouchTransition` 在
  `internal/platform/db/touch.go:384`、`tx.AppendEventOnly` 在 `:446`，注册表 `versionlessAgg`
  里 `AggProcess: {table: "process_instances", guards:{status:true}}` 已出现；
  该文件 mtime **05:13:15 → 05:13:31**（我 30 秒内两次采到不同值）⇒ db 此刻仍在写，
  巡检 05:09 那次"五个名字 0 命中"是**当时为真、现在已假**的快照，不能当现状引用。
  ② "等 backend 把三格 Recorder 改过去"——**一行都没改**：`grep -rn "TouchTransition|AppendEventOnly|AggProcess" cmd/core/ --include=*.go`
  排除测试后 **0 命中**，`cmd/core/dropaudit.go` mtime 仍是 **01:30:01**。
  ⇒ 合验的真实前置收缩成"**backend 那一半**"，db 那半落地稳定后我不用再做任何事。
  **顺带一问（不主张、不改动）**：`versionlessAgg` 里 `AggCore: {table: ""}` 是空表名——若这是"`core_ready`
  没有聚合行、只发事件"的有意设计，建议在注释里点明，否则下一个人会读成没填完的 TODO；这是 `db`/主控的判断，我不碰。
  协议侧我随时能交：握手与能力核定、`CarriesTurnRef` 六条范围、票号↔execution_id 一致/矛盾两侧、
  丢弃到达接缝、fake 的 claimed→dispatched→close→下轮链；行级那五条在 backend 改完之前只会是
  "skip（flag 未开）/ 显式 `t.Fatal`（flag 开了但没接上）"——两者都**不算绿**，我不会拿它当覆盖交。
  **T-199 的两个归属结论我照办**：`.scratch/adapter/verify-req-t12.sh` 只当私量工具（随清理消失可接受），
  我**不往 `scripts/` 或 `core/test/` 落任何文件**（契约③ §7 归 `test`）；
  而且**我不另开那条 REQ**——理由：判据的长期落点已经在 `cmd/core/lifecyclecolumns_test.go` 里以 `go test`
  形式存在（含双向断言与 flag 门），再造一道 `scripts/` 门就是第二份真相，正是今晚一直在防的东西。
  主控若仍要它进 `test` 的持久门，说一声我再写那一行。
  **上一版这句已过期并划掉**："把 probe 交给 backend 接线 / 真子进程那条腿要等 backend 递进真 Supervisor"——
  03:10 实测前置条件已经成立：`core/api/http/adapters.go:32 registerAdapterProbeRoute`、
  `core/cmd/core/probeassembly.go`、`core/cmd/core/processpipe.go:76 Rec: newDropRecorder(l.store, adapterKey, l.now, l.logf)`、
  `core/cmd/core/dropaudit.go` 全在 HEAD（`917b6c4` 03:06:09）；而且 `Registry.Probe` 的成功路径也不再只有进程内——
  backend 有 `TestProbeOverRealHandshakeWithFakeAdapter`。**我这段时间不动 `cmd/core`/`api/http`**（T-185 的禁令，
  且 `api/http/adapters_test.go` 还压着 +34/−7 未提交、T-186 会再叫 backend 动它）。
  **预备时发现的那条缺口**（不等发令就能报）：REQ-T12 五条判据是**行级**判据，而 `cmd/core` 全部测试里
  真读 `process_instances` 的只有 **1 处**（`dropaudit_test.go:146`，断言 `count(*)=0`，即"丢弃不写生命周期行"）；
  `exit_code` / `restart_policy` / `owner_type` / `metadata_json` / `status` 在**该查询里各 0 处**
  **【09-23 05:13 作废注明】下面这句是 03:19 当时的状态，已被后续复量取代：backend 的五条测试确实写进了 `lifecyclecolumns_test.go`（03:48:56，双向断言 + flag 门），缺的收窄成"backend 把三格改过去 + db 那半停笔"；保留原文是因为它记的是"我如何发现合验验不到落库层"这个过程。**
  ⇒ 判据 ①②③④ 今天无人断言，⑤ 由我包内 `TestEnumsFitTheFrozenProcessInstancesTable` 前置挡住枚举漂移、
  但"真写一次没被 CHECK 拒"仍需 ① 那条用例。落点只能在 `cmd/core`（`package main` 不可被我 import），
  所以我交的是判据与命令，不是那一半测试。
  - （**旧 `Verification`**，按 T-276 留档不当现状；它那条命令我今天 10:58 实测**仍可跑**，
    只是包集是旧的（三点形式 `./dir/...` 合法，我上一轮记的"这条命令 `[setup failed]`"指的是当时 `internal/process` 还不存在，
    不是形式错））：
    `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/...`

## Completed

- 前置阅读与现状勘查（见上）。关键复用发现：危险开关白名单**已由 audit 角色实现**在
  `core/internal/permission/adapterargs.go:29`（`CheckAdapterArgs`，C2 裁定归 permission），
  我不重造；接入点在 Supervisor 启动子进程前，跨包 import 的合法性见 Coordination CO-3。
- **帧编解码**（`core/internal/adapter/protocol.go`）：契约 §1 全部落地——
  ndjson 一行一帧、`jsonrpc:"2.0"`、请求/通知/响应三态按存在键判定、`request_id`+可选 `deadline_at`、
  `$/cancel` 作为 §4 `域/动作` 命名的唯一例外、响应不得同时带 result 与 error。
  `Writer` 写前校验（坏帧绝不上线）+ `SetEscapeHTML(false)`（provider_raw 逐字节保真）。
  「帧内不得含裸换行」由 JSON 字符串转义天然保证，测试 `TestFramePerLineInvariant` 钉住：
  文本含 `\n`/`\r\n`/`<script>`/中文时，N 帧恰好 N 个换行。
  Reader 区分**语义错**（流仍可继续读，测试 `TestReaderKeepsStreamAfterContentError` 证明）
  与**语法错**（流位置不可靠，交上层处置）——这是 Supervisor 恢复路径的前提。
- **握手与能力表**（`handshake.go`）：§2 帧形 + §2.1 `software_versions` 为 map（字符串形态在
  **decode 阶段**就不可判定，见 `TestSoftwareVersionsMustBeAMap`）+ §2.2 `Negotiate`
  （major 不同→`PROTOCOL_VERSION_MISMATCH` 且不返回版本；peer minor 高于 Core→放行但 `Experimental=true`
  供调用方记 audit）+ §3 八等级/五 backend/confidence/delivery_modes 枚举与「每能力一行」查重
  + §9 `debug.*` gating（点分与斜杠两种拼法都拒，`adapter_key!=fake` 或等级非 EXPERIMENTAL 即拒）
  + §3 有效能力计算（`resolveEffective` 到不动点：UNSUPPORTED 与 `requires` 未满足者出局，
  循环依赖自然留在无效集）+ §2 未握手门（**nil `*Handshake` 即未握手态**，任何方法 →
  `PROTOCOL_NOT_INITIALIZED`，Core 侧无法"忘了初始化就摸到 Adapter"）+ §3 `write_entry:false` 门
  （写方法 → `WRITE_ENTRY_NONE`，仅当该能力以 `UI_AUTOMATION` 等级显式声明且 write_capable 才放行——
  即"降级点 UI 假装成功"默认非法）+ §5 `Delivery` 门（未声明的态 → `DELIVERY_MODE_UNSUPPORTED`，
  错误消息带**该 Adapter 实际声明的态列表**，UI 不需猜）。
- 12 个表驱动测试；`gofmt`/`go vet` 干净；两次变异检验证明断言有效（见 checkpoint）。
- **第 2 轮：流式（`stream.go`）**：§6 四条通知的解码与构造。`stream/delta` 的 `channel` 限定
  `ASSISTANT/THOUGHT/TOOL/STATUS`；`stream/item` 的 body 按契约原样透传（§6 没给字段，CO-11 已提）；
  `provider_raw`/`usage` 全程 `json.RawMessage`，Core 不解释（§6.4）。
  `Cursor` 落 §6.2：**单调 watermark**、`<= last` 幂等丢弃、跳号记第一个未见号供 `since_seq` 续传、
  首帧之前不算丢帧（没订阅就无所谓错过）、`Reset` 供重连与**进程重启**（A-5 已把
  「握手完成后、喂第一帧之前必须 Reset」钉成我的第 3 轮硬约束）。
  §6.1 新落条款当场跟进：**四条通知一律要求 `sequence_no >= 1`**（解码侧与 `NewStream` 双侧都拦），
  续传游标是 `(execution_id, sequence_no)` 二元组 → 第 3 轮的 pump 必须**按 Execution 各持一个 Cursor**，
  不是按连接一个。
- **第 2 轮：额度（`quota.go`）**：§7 数值字段全用指针（plain float64 分不出"没数据"和 0，
  而那条正是 §12 的直接实例）。`Validate` 拦：无观测却报非 UNKNOWN、有观测却报 overall UNKNOWN、
  **整行全空值**（那就是拿空行冒充"读到过"）、`used+remaining` 不构成满窗（Qoder 实测"字段齐全但全为 0"
  在这条上必红）、百分数越界、`reset_at/window_*` 为 0、窗口倒挂。契约 §7 的示例 JSON 原文当作
  fixture 直接解码并放行，避免我按自己的复述实现。
- **第 2 轮：错误码改整数（`protocol.go`）**：CO-4 裁定落地为 §1 表
  （`PROTOCOL_NOT_INITIALIZED -32600`、`WRITE_ENTRY_NONE -32601`、`CAPABILITY_UNAVAILABLE -32602`、
  `DELIVERY_MODE_UNSUPPORTED -32603`、`INVALID_PARAMS -32604`），`ErrorCode` 改 `int` 型——
  wire 上就是裸数字，Go 侧仍用符号常量分支，`String()` 供日志/Audit。
  `TestErrorCodeTable` 钉住 号↔名 双向、查重、**并断言 Core 侧任何码都不得落进
  `-32000..-32099`（那段留给真 Adapter 的原生错误）**；`TestErrorFromWireRoundTrips` 钉住
  数字解码回符号、以及无 code 的错误响应按缺码拒绝而不是读成 0。
  注意：我原先建议的"`-32000` 留给 transport 错"方向与裁定的表相反，已按**契约为准**改，不留我的版本。
- **第 2 轮：`adapter/health`（`handshake.go` 的 `HealthResult`）**：§4.1 钉的三元组
  `health`/`confidence`/`checked_at`，`checked_at` 必须为正 Unix ms（无日期的读数无法复核），
  并拦住 §4.1 的冒充——我把它推广成"确定态（HEALTHY/DEGRADED/UNAVAILABLE）配 UNKNOWN confidence 即拒"，
  这是 Core 唯一真能检出的形式，推广本身记 CO-12 待确认，不假装是契约原文。
- **第 2 轮：写方法清单（§12.11）**：`writeMethods` 按裁定收窄为
  `session/send`、`session/stop`、`session/create`、`model/select` + `debug/*` 前缀；
  `approval/respond`/`approval/cancel` **移出**（审批走 Permission 通道，不受 `write_entry` 门），
  `approval/list`/`quota/read` 保持可读。测试同步改了 4 行，删掉与裁定冲突的旧断言行。
- **第 2 轮：`session/send` 回复形状（`send.go`）**：§7.2 的 `accepted`/`reason`/`queue_position`/
  `turn_id`/`provider_execution_id`/`idempotency_hit`。校验跑在**Core 实际请求的投递态**上，
  因此"`start_now`/`guide` 的回复里带 `queue_position`"直接判 `DELIVERY_MODE_UNSUPPORTED`——
  这就是 §5 严禁的静默降级，从回复形状上堵住；`guide` 因"当前轮 INTERRUPTED + 新开轮"同样必须带
  `turn_id`（不带就等于没开新轮）；`queue` 只承认"已入队 + 位次"，位次必须 ≥1（0 不许冒充未知）；
  `accepted=false` 必须带 `reason`，否则 Core 无从落"待核实"。
  `provider_execution_id` **不强制非空**：强制一个 Provider 可能真没有的字段等于逼它编 ID，
  合法表达缺失的方式记 CO-13 请主控定。
- **FakeAdapter 切片 1（`core/cmd/fake-adapter/`，契约 §10 裁定的落点）**：`main.go` 的
  `run(in, out, logs)` 把三条流分开（§1 的 stdout 只放帧），`main_test.go` 用
  **re-exec 测试二进制**（`FAKE_ADAPTER_HELPER=1` → 直接调 `main()`）跑**真子进程**，
  不调 go toolchain、不引入 `go.work`（否则 `internal/` 会拒）。四条用例：
  ① 握手前任何方法（含 `debug/crash`）一律 `PROTOCOL_NOT_INITIALIZED`，跨进程边界成立；
  ② 它自己的握手回复**再过一遍 Core 侧 `NewHandshake`**，等于让契约②的校验器审它的自述
  （`debug.crash` 必须 EXPERIMENTAL、`session.locate` 是 UNSUPPORTED 因而不进有效能力、
  `session.send` 三态齐、`software_versions` 两行）；③ `adapter/health` 回复过 §4.1 校验，
  未实现的方法回 `INTERNAL_ERROR: not implemented in this build`（不伪装成契约级拒绝）；
  ④ `debug/crash` 回完答复后进程**退出码 2**，父测试进程照常存活并被断言。
- 这条自校验当场抓出我自己的两个真 bug（不是写完就绿）：① `initializeResult()` 漏 `Protocol`
  字段 → 回出去的版本是 `{0,0}`，Core 侧 `major 0 vs 1` 直接拒；② `capability()` 助手把等级写死
  `NATIVE` → `debug.crash` 以 NATIVE 声明，被 §9 的门拒在自己的握手校验里。两处都已修，
  并且第②处现在有显式的 EXPERIMENTAL 声明与注释说明"只有 fake 可以"。
- 自校验还顺手把 `adapter/initialize` 从 §2 未握手门里豁免出来（原来它自己也吃 `PROTOCOL_NOT_INITIALIZED`，
  等于谁都握不了手），并拒绝第二次 initialize（§2.2 运行中不改版本）。
- `ErrorCode` 的两个"待补号"从 `-32005/-32006` 挪到 `-32048/-32049`：`test` 角色报来 §1 表把
  `-32005/-32006` 给了 `READ_ONLY`/`EXECUTION_STATE_UNKNOWN`，同号两符号是误读温床（见 CO-4 与 B-a4 的复测：
  那张表在本 worktree 仍查不到，所以这是**留间距**而非修现网冲突）。
- **T-24 的两个硬阻塞（都用真子进程复现后才动手，没采信转述）**：
  - **BUG-T6-①** 复现结论与报告不同：**单次 `adapter/initialize` 是通的**（fake 的 `handle()` 早就豁免了它），
    真正的坑在库级门禁 `Handshake.Call` —— 它把 `adapter/initialize` 自己也按"未握手"拒掉，
    所以任何"把所有方法都过这道门"的调用者（`test` 的 in-proc 桩、我要写的 Supervisor、以及删掉本地副本之后的 fake）
    永远握不上手。修法是把规则收进 `Call` 一处：未握手时 initialize 放行、已握手后再来 initialize
    回 `INVALID_PARAMS`（§2.2 运行中不重谈版本）。fake 里那份重复豁免随即删除，单一事实源。
  - **BUG-T6-②** 复现成立：`run()` 在每次回复后要把"自己回复的字节"再解码来做自校验，
    于是**第二次 initialize 的错误回复（`result` 为空）解不开 → 进程直接死 → 其后一帧永远没有回话**，
    调用者只能等 deadline。修法是结构性的：自校验改成对静态声明做（启动时一次 + 握手成功时一次），
    不再有任何"每帧回包再解码"的路径；`replyResult`/`json.Unmarshal(reply.Result)` 整段删除。
  - 两条各配一条真子进程回归测试：`TestFakeAdapterSurvivesSecondInitialize`
    （第二次 initialize 收到拒绝，且随后 `adapter/health` 仍能正常服务）；
    库侧 `TestCallGate` 新增"未握手时 initialize 放行 / 已握手后再来回 INVALID_PARAMS"两行。
- **第 3 轮 Supervisor 骨架（`core/internal/process/`，520 行含测试）**：
  `BackoffFor` 落 §8 的 1s/2s/4s/8s、30s 封顶（`attempt>=6` 到顶；8s 之后是否先走 16s 记 CO-19 待裁）；
  `Supervisor{Launch, Restart, Sink, Backoff, MaxCrashes}` + `Start/Run/Stop`；
  `Child` 带分离的 stdout/stderr/stdin 与**可重复调用**的 `Wait`；事件 `STARTED/EXITED/CRASHED/UNAVAILABLE`
  通过 `Sink` 回调交出去，**这个包不写 DB、不发事件**（§0 与 CO-3：登记与 §14 状态写由持库句柄的调用方做）。
  五个用例：退避表；连续 5 次崩溃后 `ErrUnavailable` 且**恰好只启 5 次**；干净退出不吃预算；
  `Stop` 杀子进程并回收状态；
  **§8 崩溃隔离** `TestCrashedAdapterLeavesAnotherAlone`：一个 Adapter 进程连崩到 UNAVAILABLE，
  父进程（在这里就是测试二进制，充当 Core）存活且与另一个 Adapter 继续完整对话、
  并核对 §1 的 stderr 分流（诊断行不混进帧流）。
  它**只覆盖 Supervisor 能证明的那半**：Execution 转 `UNKNOWN` 是 execution owner 写的状态值，不在这里冒充。
- **按 §15.1 改回包形状**（见 B-a4 那条）：`Error` 现在同时携带数字 `error.code` 与
  §15.1 的名字 `error.data.code`，`Error.Symbol()` 优先采信对端给的 `data.code`，
  未知号码显示成 `UNKNOWN_CODE(-n)` 而不是瞎猜名字。
- **第 3 轮：协议 pump（`internal/process/connection.go` + `internal/adapter/id.go`，本轮新增约 900 行含测试）**：
  `Connection` 挂在 Supervisor 的 `Child` 上，`Connect → Initialize → Call/Cancel/Inbound → Close`。
  落到的契约点：§1 的 `request_id` 是 UUIDv7（`NewRequestID` + `Timestamp()` 反解，id 自身可排序，
  重放和卡住的请求在日志里能看出来）、`deadline_at` 由 ctx 推得、超时**必须**发 `$/cancel` 并把 ctx 错误带回；
  §2 的门双向生效 —— 出站 `Call` 先过 `hs.Call`，入站握手前到达的 `stream/*` 按违规上报且**不动游标**；
  §6.2 的幂等丢弃由 pump 唯一持有游标（`LastSeq`/`MissingSeq` 就是重连要发的 `since_seq`）；
  CO-8 的 `(execution_id, sequence_no)` 通过"按 Execution 各持一个 `TurnTracker`"落实；
  §6.3/§8 的死亡路径：`kill` 一次性把挂着的 `Call` 全部失败返回（绝不等 deadline），
  并把未闭合的 turn 转 `UNKNOWN`、保留 `provider_execution_id` 给 Recovery 回查。
  写侧加了 `writeMu`：`Call` 与 `Cancel` 来自不同 goroutine，半行字节就违反 §1 的一帧一行。
- **本轮两个由测试暴露的设计修正，不是样式问题**：
  ① pump 若只在握手后启动，握手回包本身就写不进无缓冲 pipe → 自锁；改成 `Connect` 即启动、
    用"握手前入站帧算 §2 违规"来保住 CO-6 的游标语义；
  ② `Connection.Turn()` 返回的 tracker 明确成"pump 唯一写者、调用方只读"，
    死亡回归测试也因此改为**用真帧驱动 pump**（claim/dispatch 走 Inbound），
    不再从第二个 goroutine 写 tracker 状态。
- **跨真进程的端到端**：`TestConnectionAgainstRealFakeAdapter` 用 `go build` 现场编出
  `cmd/fake-adapter` 再以真子进程连上（`exec.CommandContext`，非裸 exec）：握手后
  `session.send` 三态齐、`debug.crash` 作为 fake 的 EXPERIMENTAL 能力有效、
  `adapter/health` 回复过 §4.1 校验、未实现方法在真管道上回显式 `INTERNAL` 而不是超时。
- **T-36 三件（都在我自己包内，没碰别人）**：
  - (a) **17 个域码 + 2 个协议码的完整数字表已交**（正文在报告里，代码是 `protocol.go` 的 const 块，
    `TestErrorCodeTable` 逐行钉住号↔名，`test` 可直接 grep 这两个表）。
    分段：`-32000..-32049` 契约域码、`-32050..-32099` 保留（Core 永不回答，`TestErrorCodeTable` 有断言）、
    `-32601/-32602` 用 JSON-RPC 预定义号表达 `METHOD_NOT_FOUND`/`INVALID_PARAMS`。
  - (b) `PROTOCOL_NOT_INITIALIZED`/`WRITE_ENTRY_NONE`/`CAPABILITY_UNAVAILABLE` 从 `-32600..-32602`
    迁进 `-32000/-32003/-32002`；`DELIVERY_MODE_UNSUPPORTED` 同理由一并迁走（它当时占 `-32603`，
    在预定义段里是 internal error，通用客户端同样会误路由——这条你没点名但同病，我一起改了，若要退回说一声）。
    守卫：表里任何域码落在 `-32700..-32600` 就红。
  - (c) **判定：留 JSON-RPC 标准号，删我自己的重复表达**。理由一行：`-32601/-32602` 的含义任何客户端都已经知道，
    再给同名 `data.code` 就是同一概念两套真相；所以 §15.1 的名字只随域码走
    （`NewErrorReply` 现在只对 `domainCodes` 里的码写 `data.code`，协议级错误只带数字），
    `TestDomainNameTravelsInDataCode` 两侧各钉一条。
  - 附带把我 CO-22 的答案落地：进程在飞时死掉，回包码由 `INTERNAL` 改为 **`ADAPTER_UNRESPONSIVE`**
    （连接级不健康，正是你给它的定义），不再是"我们内部出错"这种误导。
- **`READ_ONLY` 仍无调用点**：§3 的 `write_entry:false` 门与 §12.11 的"只读型伪装"是同一道门，
  我按 §3 回 `WRITE_ENTRY_NONE`；`READ_ONLY` 号已占（-32016）、名字已进表，等 audit/backend 给出
  "对象级只读"的实际拒绝点再改代码 —— 这不是我这边能自己定的语义。
- **T-45（码表分类与未知码降级）**：
  - ②/③ 按"只有 `Core:*` 生产方需要 Go 常量"执行：删掉 9 个投机常量
    （`ErrLocalDataUnavailable`/`ErrSessionNotFound`/`ErrAuthRequired`/`ErrRateLimited`/
    `ErrDangerousFlagRefused`/`ErrCancelled`/`ErrTimeout`/`ErrProviderError`/
    `ErrExecutionStateUnknown`/`ErrReadOnly` 中除 `Core:internal/adapter` 生产的以外全部收进
    `domainCodes` 的数字键），词汇表仍完整 17 项，`Symbol()`/`Known()`/日志照旧识别对端发来的每一个码。
    `TestErrorCodeTable` 现在反过来钉这条规则：**生产方不是本包却声明常量 → 红**
    （我把 `AUTH_REQUIRED` 硬塞一个常量试过一次，两条断言同时报红）。
  - 表已按 §15.1.1 折进契约，我逐项对过：数字、码名、生产方、有无常量四列全等；
    判据也按他们定稿改成"行数差集恰好是那两行协议层码"，不再是 17==19 的硬编码。
    豁免清单被我换成"只有 `Core:internal/adapter` 生产才要求常量"，
    免得以后的 `DANGEROUS_FLAG_REFUSED`/`EXECUTION_STATE_UNKNOWN` 在 process 里长出常量时默默失效。
  - §15.1.1 要我回一句话的那条，答案是：**词汇归 `internal/adapter` 统一持有、由 `internal/process` 触发**。
    理由已写进 `protocol.go` 注释：对端回复里出现 `ADAPTER_UNRESPONSIVE` 时，编解码层必须认得它，
    否则"谁能发"会变成第二个真相源。
  - ⑤ 第二件（未知码降级）做成了真行为而不只是函数：pump 收到回复即 `f.Error = f.Error.Degrade()`，
    未知号 → 降级 `INTERNAL`，`data` 保 `original_code`（对端若还带了名字则保 `original_name`），
    连接不断。端到端 `TestConnectionDegradesUnknownReplyCode` 走真 pump。
    **顺手记一个我自己写的真 bug**：`Degrade()` 第一版把假名字写进 `data.code`，
    而 `Symbol()` 优先读 `data.code`，于是"降级成 INTERNAL"在打印时又变回 `UNKNOWN_CODE(-32499)`；
    是新加的端到端测试当场戳出来的（`Symbol() = "UNKNOWN_CODE(-32499)", want INTERNAL`），已修并补断言
    （降级后 `data.code` 必须不存在）。
    一处按 T-45 与 §15.3 的张力照旧执行并上报：未知码我降成 `INTERNAL(-32013)`（T-45 原话），
    §15.3 规则 1 另有 `-32603 未映射的内部故障`，两者都讲得通；要说用后者我改一行。
  - ⑤ 里"T-45⑤ 说 `READ_ONLY`/`EXECUTION_STATE_UNKNOWN` 在 `protocol.go:59-64` 预留 `-32005/-32006`"
    **与盘上不符**：那两行是 `LOCAL_DATA_UNAVAILABLE`(-32005) 与 `SESSION_NOT_FOUND`(-32006)，
    而这两个码在 §15.1.1 里是 `-32016`/`-32015`，且符号早已存在（现已按 ② 把 `READ_ONLY` 等收为词汇）。
    我按盘上的事实改，不照转述改。
- 过程中的两次自伤，如实记：
  ① helper 子进程原本 `sleep 30s` 等父进程杀，导致每个测试被拖满、并留下过 `process.test.exe` 泄漏进程
  （已 `taskkill`，helper 改成"stdin 关闭即退"）；
  ② 第二次变异（把干净退出也当崩溃）与四包并跑叠在一起，让 `core/test` 出现 120s 超时 FAIL——
  单独复跑 `./test/` 是 `ok 3.099s`，还原变异后四包并跑又是 `ok 3.243s`，
  所以那次 FAIL 只出现在我带着变异的那次命令里，不作为别人包红的结论报出去。
- 变异检验累计 16 次。本轮新增 5 次，每次都只坏对应那条：
  | 变异 | 结果 |
  |---|---|
  | 握手前入站门改恒不触发 | `pre-handshake inbound = {...Err:<nil>}` + `LastSeq = 5, want 0` |
  | §6.2 的 `!keep` 丢弃改 `false && !keep` | `turn/claimed(accepted=true) arrived while the turn is CLAIMED` —— 不去重就会把回放当新证据 |
  | 死亡时不做 `ProcessRestarted` | `turn state after death = "RUNNING", want UNKNOWN (§6.3, not FAILED)` |
  | deadline 后不发 `$/cancel` | `no frame arrived from the connection within 2s` |
  | 预算判据 `>=`→`>` / 干净退出当崩溃（上轮补测） | `launched 6 times, want exactly 5` / `budget exhausted ... (exit 0)` |
  记一笔过程诚实：M2 的第一次写法（`if false {`）让 `keep` 变成未使用变量而**编译失败**，
  那不是有效变异；改成 `false && !keep` 重做才算。
  另有一次**天然变异**：`Call` 的 initialize 豁免被外部编辑回退后，fake 的三条测试立刻全红
  （错误原文就是 T-24 转述的那句），这同时是"断言会失效"的证据，也是下面 B-a7 的证据。
- 第 2 轮共 4 次变异检验，全部命中：`session/create` 从清单删掉 → `TestCallGate` 1 行 FAIL；
  `QueuePosition` 降级拦截改成恒假 → `TestSendResultValidate` 3 行 FAIL；
  §6.1 `sequence_no` 门退回"只拦负数" → `TestDecodeStream` 1 行 FAIL；
  Core 码挪进 `-32050` → `TestErrorCodeTable` 4 行 FAIL。每次都只坏对应断言，还原后复绿。

- **B-a8 已闭：我自己包里的竞态真缺陷（不是测试抖动）**。四包同跑时 `internal/process` 报 FAIL，
  单独跑却绿 —— 我没接受这个解释，连跑三次抓到：
  `TestConnectionDeathMarksTurnUnknown` 报 `Call = <nil>, want ErrConnectionLost`。
  根因在实现不在测试：`kill()` 会给"死之前已登记的在飞调用"合成一帧 error 回复（`err == nil`），
  而"死之后才登记的"走 `<-c.dead` 分支返回 `ErrConnectionLost` —— **同一个条件两种返回形状**，
  调用方拿到什么取决于竞态。改法：`kill` 不再合成回复帧（等待者本就在 select `c.dead`），
  两条路径统一成 `*LostError{Method}`（`errors.Is(ErrConnectionLost)` 成立、
  消息点名 `ADAPTER_UNRESPONSIVE`），测试同时钉"死之前发起"与"死之后发起"两种顺序。
  变异检验：把形状退回裸哨兵 → `want a *LostError carrying the method` 报红；`-count=10` 复跑不抖。
  顺带一条要回给主控的契约措辞：Core 侧**不会把 `ADAPTER_UNRESPONSIVE` 放上 wire**
  （回复必须有对端请求才能发），它出现在 Go 错误文案与 §6.3 的 Inbound 通知里；
  §15.1.1 那行的生产方若要精确，建议改成"Core:internal/process（连接级条件，不经 error 响应）"。
- **T-47 落地**：一次性产物改落 `D:\Project\Lucero\.scratch\adapter\`（`git check-ignore` 命中 .gitignore:31）；
  `buildFakeAdapter` 从手工 `os.MkdirTemp("")`（会在共享临时根留孤儿目录、且失败时不清理）
  改成 `t.TempDir()`（testing 包自建的具名目录，测试结束自删）；
  我这轮此前确实违反过：变异备份的 `*.bak` 落在 `/tmp` 根 —— 已按"只删自己建的具体文件名"清掉
  16 个具名文件，`ev2.bak`/`ev3.bak` 不是我建的原样不动；`/tmp`、`%TEMP%` 的任何通配删除我此后不做。
- **第 4 轮 A 段：FakeAdapter 的投递面（`core/cmd/fake-adapter/main.go` 从 181 行长到 480 行量级）**。
  可跑判据：`CGO_ENABLED=0 go test -count=1 ./cmd/fake-adapter/... ./internal/process/...` 全绿，
  且 `TestFakeAdapterDeliversThreeStates` 断言的是**整条线上顺序**（含响应位置），不是"迟早会收到"。
  - **§5 三态是真行为不是声明**：`start_now` 开一轮（`turn/claimed` → `turn/dispatched` → `stream/delta`，
    再回 `accepted`+`turn_id`+`provider_execution_id`）；`guide` 落在**同一个开放 turn** 上只追加一个 delta
    （`steer.TurnID == now.TurnID` 被钉住），没有可插话的轮时它**报告**自己改开了新轮而不是闷声开；
    `queue` 只给 `queue_position`，**一帧证据都不发**（CO-7 的"排队只能声称排队+位置"）。
  - **§4 Idempotency-Key**：同一个 key 第二次进来返回首次那份答案 + `idempotency_hit:true`，
    并用"这一问一答之间流上帧数为 0"钉住它没有偷偷再发一轮（m3 变异证明这条判据会咬）。
  - **§10 四种注入到齐**：`debug/fail{code,nth}`（名字或裸数字都收，"答一个 §15.1 之外的码"就是走数字这条路）、
    `debug/hang`（不答，直到 `$/cancel` 点名它；§15.3 规则 5 的确认是**结果帧** `{"cancelled":true}`）、
    `debug/unknown_quota`（把 `quota/read` 切成 `observations:[]`+UNKNOWN）、`debug/crash`（原有）。
    `quota/read` 由此第一次有两种真答案，§7 的 UNKNOWN≠0 在**同一个进程**里可对照。
  - **一轮 turn 的存活期 = 到下一次 send 为止**，队列在那一刻排空（每条排队项拿自己的完整证据对）。
    选它是因为它确定性、零计时器；契约本身没定义排空点 → 已记 CO-25。
  - **新增一条跨边界用例** `TestConnectionSendOverRealPipe`：真 `process.Connection` 驱动真子进程跑
    `session/send`，断言游标推进无洞、`TurnTracker` 到 RUNNING、`turn/finished` 按 execution 归属正确，
    并用 m7（回复说谎）/m8（pump 架空 tracker）两条变异证明它不是自证。
  - 顺带修掉三处"注释与代码不一致"：`Connection` 的 reader 启动点注释（起于 `Connect`，因为 `io.Pipe`
    无缓冲）、`codeTable` 新增反向查找断言（§15.3 规则 3 的名字↔数字唯一真相）、`SendParams` 的
    `execution_id` 注释直接写明它为什么可选（绕开 CO-24 而不是假装契约有这一项）。
  - **刻意不做**：不自加 `debug/delivery_semantics`/`task/accept`（§10 注入表只四条）；
    `session/create` 虽在 §12.11 写方法清单里，但不在 §10 的 must-list，留到下一段和它一起做。
- **第 4 轮 B 段：CO-21 / §6.5 `turn_ref` 全线落地**（A 段刚交付的投递面被这条裁定重画了形状，
  我按契约文本改，不按投递里的转述改）。可跑判据：三包 + `core/test` 全绿，且 §6.5 的每条子项都有
  一条会咬的断言（变异 10 次，见 Verification）。
  - **范围最终定死为 §6 表格那四条**：`stream/delta`、`stream/item`、`stream/state`、`turn/finished`
    必带 `turn_ref`；`turn/claimed`/`turn/dispatched` 不在其中（CO-8 给了它们 `execution_id` 自证归属，
    契约里也从没给它们 `sequence_no` 以外的票号）。这条口径我在 10 分钟内收到过**三种**互斥说法
    （"四条=表格四条"→"六条全带"→"四条=claimed/dispatched/delta/finished"→又回"表格四条"），
    最后按盘上可证的那条实现：`CarriesTurnRef` 的集合与 §6.7/§6.5 括弧里那份名单逐字相同。
  - **票号只许回显，来源是 Core 发的 JSON-RPC `id`**（`protocol.go:43` 的 `Frame.ID`）。FakeAdapter 把它
    存进 `turn.ref`，`stream/delta`/`turn/finished` 原样带回，`turn/claimed`/`turn/dispatched` 不带。
  - **`execution_id` 从那四条上退出**：`StreamFrame.ExecutionID` 现在只可能出现在 CO-8 的两帧上；
    幂等键改成 **`(turn_ref, sequence_no)`**，`sequence_no` 本身仍是连接级单调（§6.5 子项 3 明说），
    所以连接级 watermark + hole 保留、每票号另记一个 `lastByRef`。
  - **A-7 决议 ① 落地**：CO-8 的 `turn/claimed` 也必须带 `sequence_no`（我这边 §6 规则 2/CO-6 早就这么判，
    是契约写漏），新增两行用例钉住"缺 `sequence_no` 的 claimed/dispatched 被拒"。
  - **pump 归属来源改成调用方**：新增 `Connection.CallTurn(ctx, executionID, method, params)`，
    写帧之前把 `turn_ref → execution_id` 记进**只在本连接有效**的表；普通 `Call` 不绑定。
    未知票号的帧**丢弃 + 只回 `Inbound.Err`**（`Frame` 为 nil），且**不推进任何游标** —— 这一条与
    "已知但违反 §6.4 的帧仍然带着 Err 转发"是两种处置，§6.5 要的就是前者。
  - **§10 那条新验收真的能测了**：`TestConnectionDedupsAndTracksTurn` 改成"一条连接、两个 execution、
    第二个的号比第一个的水位低"，旧设计（只看连接级 watermark）会把第二个的帧当重复丢掉 —— 变异 n4 证明
    这条断言会咬；`TestConnectionDropsUnboundTurnRef` 钉"不猜、丢弃、不推进"；
    `TestConnectionSendOverRealPipe` 现在跑的是打断语义（`start_now` 抢占在飞的那轮，被抢占那轮的尾巴
    和 `turn/finished{INTERRUPTED}` 在新轮开始**之后**才到），这就是"并发两个 execution"的帧级证据。
  - **FakeAdapter 的 turn 生命周期跟着改**：一轮不再被下一轮"正常收尾"，而是被 `start_now` **打断**；
    被打断轮的 `turn/finished` 结果是 `INTERRUPTED` 而不是 `OK`（A 段那版把打断当成了正常完成，
    这是 CO-7 语义上的失真，顺手改掉）。
  - 引用号最终按主控 15:50 的指令回到 **§6.5**（`§6.7` 在 `core/` 下 0 命中；契约里 §6.5 就是那个
    一直存在的老编号，`### 6.5` 计数 0 说明它不是小节而是列表第 5 项 —— 这条我按主控的裁定执行，不再纠）。

- **第 5 轮 B 段：FakeAdapter 的 §4 读面全到（`core/cmd/fake-adapter/service.go` 新文件 + `readers_test.go`）**。
  可跑判据：三包 78 个测试函数全绿、§10 的 11 项按 `serve()` 的 switch 数出来 15 个分发标签全覆盖。
  - **`session/list` / `session/read`**：游标是"下一页偏移量"的十进制串，`next_cursor` 只在这里被本
    fake 造出来，所以"空串 = 到尾"这条线两端都用例钉住（翻到底必须给空、给了假游标要 `INVALID_PARAMS`）。
    `remote_revision` 按 §4 只要求"能比出日志动过"，所以是字符串不是号。
  - **`session/stop`**：真闭合在飞的那轮（`stream/state(RUNNING→STOPPING)` → `turn/finished{CANCELLED}` →
    再回 `stopped:true` + 那个 turn 的 id）；**没有可停的东西时回 `stopped:false` + reason**，
    绝不按 §4 禁止的那种"已停止了事"；停错会话（`remote_session_id` 对不上在飞的那轮）直接
    `INVALID_PARAMS`，因为"停到别人身上"和 §12"消息发到错会话"是同一类事故。
  - **`approval/list` / `approval/respond`**：载荷是结构化对象；`respond` 用 `state` 字段名对齐事件契约的
    `execution_approval_resolved`，并且**必须有 idempotency key**（同一 key 二次调用返回首次结果 +
    `idempotency_hit`；换 key 再决同一条审批**不覆盖**已定的结论、只报告）。值集合 APPROVED/DENIED
    在全部 specs/docs 里查不到（`grep -rn "approve\|APPROVAL_DECIDED" specs docs` 零命中）→ 记 **CO-31**，
    我没把它当成已冻结。
  - **`session/locate`** 按 §4 的 v0.1 口径回 `CAPABILITY_UNAVAILABLE`，并在用例里反向钉"不许退化成
    'not implemented in this build'"——前者是 Provider 能力的真相，后者会被读成"这个包还没写"。
  - **`model/list` 的线上字段名**：`parameters` / `provider_raw`（主控 12:1x 定的 (b) 口径）；
    `parameter_schema_json` 是 `process_models` 的**列名**，用例反向断言线上不出现它。
    ⚠ 盘上契约② §4 第 84 行此刻仍写着 `parameter_schema_json`，我按裁定实现线上名、把差异报给主控改文档。
  - **§4.2 留给 adapter 的两处矛盾，我下了结论（CO-32 / CO-33）**：
    ① **两帧不带 `turn_ref`**：`CarriesTurnRef` 就是 §6 规则 5 那四条（delta/item/state/finished），
      `notice()` 只给这四类发票号；pump 的 `turn_ref → execution_id` 因而**只能来自 `CallTurn`**（调用方
      显式绑定），这与 §6 规则 5 决议②、C20"票号不出 process 包"完全一致。
      我中途一度让两帧也带票号（照投递里"载荷注释 preferred"那句），盘上 §4.2 的载荷表把两帧定义为
      `{execution_id, accepted, queue_position?, provider_execution_id}` 之后收回，并留下 o3 变异证明
      "fake 给两帧塞票号"会红。
    ② **`turn/dispatched` 的 `provider_execution_id` 允许为空**：按 CO-13（空串合法、且**不许**被回读成
      "从未运行"），必填的只有 `turn_id` + `execution_id`；`turn/claimed` 仍必填该字段，因为 §4.2 语义 3
      说它是 Recovery 在 claimed-未-dispatched 窗口里唯一能拿去问 Provider 的东西。
      原来那条"三 id 缺一即拒"的用例被我改写并注明**它以前测的是什么**（测试规矩：改判据必须写清旧判据）。
- **§6 规则 1 的"必发面"补齐**：`stream/state`（RUNNING）与 `stream/item` 进入正常轮次路径，
  六类通知在一轮真进程里各至少一帧，`TestFakeAdapterEmitsEveryNotificationType` 分两段数
  （send 段五类、stop 段收尾类），所以"少发一类"必红 —— n1/n2 两条变异各自命中，
  且 n1 第一次是 SILENT 才暴露出"用 stop 的 state 顶掉正常路径的 state"这个覆盖洞。

- **第 5 轮 C 段 = 主控 12:45:43 的两条裁定落进代码**（`17ef3a7`，commit 里明写"§4.2 两问由我裁定"、
  两条都是"代码要改"，所以这一段没有"我再答一遍"的空间，只有实现）：
  ① **`turn_ref` 一律必带**，六条通知无例外，`claimed`/`dispatched` 只是**额外**再带 `execution_id`。
    落点：`CarriesTurnRef` 收成六条（生产者/消费者同一谓词，两侧不可能各解释一遍）；
    `attributeLocked` **只读不写** `execByRef` —— 绑定唯一来源是 `CallTurn`，帧自己带的 `execution_id`
    降级成"可交叉验证的冗余"，与绑定不一致即丢弃并回报（这正是裁定要的那句"不是第二真相源"）；
    `turnKey` 因此没有存在必要了，删掉，幂等键回到一条 `(turn_ref, sequence_no)`。
    改完的连锁：契约里"其余四条通知"的三处措辞（§4.2 表格载荷列、语义 4、§6 规则 5 的"四条"）
    全部变成裁定前的口径 —— 我不能改契约，按 CO-34 交回。
  ② **`turn/dispatched` 的 `provider_execution_id` 可空，但不得与 `claimed` 的非空值冲突**，
    且校验点明确落在 **Core 侧 tracker**：`Dispatch` 不再无条件覆盖句柄（空值保留 claimed 那个，
    否则 Recovery 在 §8 就没名字可问了），非空且不同 ⇒ `UNKNOWN` + 报错。
  三条新用例把这两条各钉在自己那一层：`TestNewStreamRefusesUnattributedFrame`（生产者侧拒发）、
  `TestTurnTrackerConflictingHandleIsTwoTruths`（tracker 侧拒两个真相）、
  `TestConnectionDropsContradictoryIdentity`（pump 侧真丢、且不推进游标）。
  7 条变异 q1/q1b/q2/q3/q4/q5/q6 全部 FIRED 在**各自声称的那一层**（详见 Verification）。

- **第 6 轮 ①：`model/select`（§4 第 84 行 + §4.1 的两道门）**。要保的判据是 §4.1 那句
  "**两个码不许互相代替，因为它们对应两个不同的用户结论**"，所以我把它做成**四类答案各一个码**，
  并让它们在同一轮真进程里可分辨：
  | 情形 | 码 | 结论 |
  |---|---|---|
  | 参数缺 `remote_session_id` 或 `model_key` | `INVALID_PARAMS` | 调用方写错了请求 |
  | 会话不存在 | `-32006`（§15.1 SESSION_NOT_FOUND） | 引用了一个不存在的会话 |
  | 模型不在 `model/list` 的 offer 里 | `CAPABILITY_UNAVAILABLE` | 这家 Provider 现在做不到（Phase 0：Qoder/Z Code 只能 UIA） |
  | `write_entry:false` | `WRITE_ENTRY_NONE` | 这家根本不让写 —— **这一条在 Core 侧门**（`handshake.go`），fake 永远声明 true，所以留在 §3 门表里，不在真进程里演 |
  第四行拒绝的**反向断言**也做了：不支持的回答里不许出现 "not implemented"，否则"没建"与"Provider 不行"
  就会混成一谈（那正是 §4.1 警告的替代失败）。`knownModelKey` 与 `modelList` 读同一个 `fixtureModels()`，
  防止"自己发布的 offer"与"自己拒绝的理由"长成两张表。
  **砍掉的东西**：先写了 `store.selected`（记住每轮选中哪个模型）后自己删了 —— §4 没有任何读路径会
  把会话的模型报回来，存了也没人能观察到，那是一段**永远无法被断言的**状态；留下它只会让测试误以为
  自己在验持久化。回执因此只做"把请求的那一对原样回答"，并写明这就是这个方法欠的全部答案。
  另两处顺带改动：`main_test.go` 与 `connection_test.go` 的"这个方法这版还没建"探针从
  `model/select` 挪到 `session/create`（挪的理由写在注释里：§4 行未落，先接就是代码跑到契约前面）。

- **第 6 轮 ②：`Recorder` 落库合验 → 结论是"合验的前提不成立"，并把前提变成可执行的**。
  **先更正我自己上轮那句错话**：我上一轮写的是"`grep "internal/process" cmd/core/*.go` = 2 命中，
  他们 12:0x 已装配" —— 那 2 个命中是 `cmd/core/main.go:4` 和 `:6` 两行**注释**，而 `:6` 说的恰恰是
  "Vault、Recovery、**Adapter Supervisor**、Scheduler 四个组件还没人交付"。我把"字面命中"当成了"在跑"，
  是 T-70/T-72 那一族错误的复发（而且是**我自己**在报别人的时候犯下）。按全仓重量的真结论：
  **`grep -rl 'lucero/core/internal/process"'` 排除我自己的包之后 = 0 个文件**，即 `internal/process`
  今天**没有任何生产 import 方**，`Recorder` 没有实现者、也没有装配点。所以"合验 `process_instances` 落库"
  这件事本身无法进行，我把结论连同取证命令一起交回（REQ-T12），不假装合验过。
  **顺手把我包内唯一能自己收口的两处收口**：
  ① 新增 `internal/process/schema_enums_test.go::TestEnumsFitTheFrozenProcessInstancesTable`
    —— `000001` 是冻结物、不许改来迁就代码，而它用 CHECK 约束钉住 `process_type`/`status`/`restart_policy`
    三张枚举；我的 Go 常量是同名的**第二份清单**，一旦漂出一个表外的值，症状是**生产里 SQLite 拒写**，
    且正好落在 Recovery 读 `process_instances` 这条最难查的路上。所以运行时直接读迁移文件对拍
    （不内嵌名单副本），并**顺带钉住接口成立所依赖的列还在**（`id`/`owner_type`/`owner_id`/`executable`/
    `metadata_json` 存在、`crashes` **不存在** —— 后者是我文档里"崩溃计数只能走 metadata_json"那句话的依据）。
    解析为空 ⇒ fatal；文件不可达 ⇒ Skip 并写明"跳过不等于通过"。
  ② `Recorder` 的文档里我写过一句**错事实**："the registered AggregateKind is `processes`"。
    实测 `internal/platform/db/touch.go` 的注册表里**根本没有 `process`/`processes` 这个 kind**，
    `process_instances` 也不在 `aggTable` 里 —— 这恰好就是契约① §12 把它列进"用 `Append`、不需要
    `Touch`/`Create`"那批表的Mechanism。文档已按实测重写，并留下 CO-39（§12 的"已由 `process` 聚合表达"
    与注册表无此 kind 的措辞冲突，请主控定调，否则会有人去加一个 `AggProcess` 给一张没有 version 列的表）。
  **另记一条我自己的 T-43 违规（当场改）**：写 ② 那句"实测于 HH:MM:SS"时我落了 `13:29:30`，
  那是**凭空写的**，真实 `date` 读数是 13:34:37；已在同一轮内改正并把这条写进账本。
  规则本身没变，是我在"顺手补时间"时没先跑 `date`。
  **同一次交付里第三次自伤（账本本身）**：splice 脚本里两个 cut() 连续改同一段邻域，第二个只按
  "- Next action:" 这个 marker 定位、没有 assert 它全文件唯一 —— 结果新块插进去了，旧块的**首行标记被吃掉、
  正文留成孤儿**（4 行 + 一个空格开头的残段）。症状骗人：节标题计数还是 12、行数也正常，
  **只有把那一段 Read 出来才看得见**。修法与判据已回写进记忆：marker 必须唯一、替换后要 grep 旧块尾句
  确认 0 命中、一趟 splice 里后一个 cut 不许依赖前一个的产物位置；**并且写中文正文时不要在反引号里放
  换行转义**（本条第一次落盘就是因为这个在正文里造出了一个真的换行 + 一个伪标题行，把节计数从 12 顶成 13）。

- **第 6 轮 ③：`session/create` 落地 + "FakeAdapter 覆盖 §4" 从声明变成断言 + §4 载荷类型归位**。
  ① `session/create`（§4:88 新行）走 `internal/adapter` 里已有的 `CreateSessionParams`/`CreateSessionResult`
    与它们的 `Validate()`（主控那行就是从这两个类型读出来的，所以代码与契约不可能各说一遍）。
    判据没有放在"回执说了什么"，而是放在 **`session/list` 的行数**上：`created:false` 是幂等命中，
    最坏的形状恰恰是"嘴上说命中、手上又建了一条"，只有数行数抓得住它。`paginate` 默认一页两行，
    所以清点时显式要 `Limit:50` —— 不写这一条，"没有变多"会在数据被分页截断时被误判成绿。
    另外两次拒绝（缺 `client_request_id` / `base_sha` 不是 40-hex）跑完之后行数必须**没动**：
    拒绝不许留下副作用。
  ② `TestFakeAdapterServesEveryContractMethod`：运行时读契约 ② 的 §4 方法表，把每一个 `C→A` 名字都拿空参数
    探一遍真进程，**只要回答是 "not implemented in this build" 就判红**。这条替代了我在账本里写了好几轮的
    "FakeAdapter 只剩某方法未实现"那种自述；主控哪天往 §4 加一行 C→A，这条就用例红替我记住它。
    同一条用例里 `debug/*` 这一族探 `debug/fail`（四件里唯一不会真崩/真挂的那个），
    解析为空 ⇒ fatal，§4 定界符找不到 ⇒ fatal，文件不可达 ⇒ Skip 且写明跳过不等于通过。
  ③ 两处"这版还没建"的探针从 `session/create` 移到 `session/push`（§4 无行、CO-25 未裁 ⇒ 真没建），
    并把"为什么是它"写进注释。
  ④ **把 `model/select` 的载荷类型搬进 `internal/adapter`**（`ModelSelectParams`/`ModelSelectResult` + `Validate`）：
    主控 CO-36 的登记说"实现为零、Core 侧形状未定"，巡检补了"只对 Core 侧成立"。两边都对，
    真正的不对称是**§4 的其它 C→A 载荷（`SendParams`、`CreateSessionParams`）都住在 `internal/adapter`，
    只有它住在替身里**。搬过来之后 Core 侧有类型、有校验，替身只是用它 —— 那句"Core 侧形状未定"随之不成立。
    `model/select` 依然**不存状态**（§4 没有任何读路径会报回会话的模型，理由见第 6 轮 ①）；
    与 ① 正好成对照：`session/create` 的效果能被 `session/list` 数出来，所以我给它建了 `created` 映射。

- **第 7 轮：CO-25 裁定落地（队列排空点 = `turn/finished`）+ 一处"规则有第二个执行点"的洞**。
  排空点从"下一次 `session/send`"搬到闭合帧之后，`startTurn` 尾部的显式排空删掉、`finish` 成为唯一排空点。
  **本轮真正的产出是两条 SILENT 逼出来的**（都记下来，因为它们各自是一族通用错误）：
  ① 我写的新用例第一次跑 `u1`（退回旧读法）**没红** —— 查下去发现 `service.go` 的 `sessionStop` 里
    还留着一处**显式 `f.drainQueue()`**，它把旧读法兜住了。也就是说：**"排空点"这件事在代码里有两个执行点，
    所以任何只测其中一个的用例都测不到规则本身**。删掉第二点（并写明为什么这里不再调用），`u1` 立刻 FIRED。
    这条和 T-110 裁定给的理由是同一个形状：规则写在两处，早晚只有一处被验。
  ② 顺序断言（排队项的 `turn/claimed` 必须落在闭合帧**之后**）第二版又 SILENT —— 我把它写成了
    `if 已经看到闭合帧 { 比较位置 }`，而变异体里闭合帧来得更晚，条件不成立就整段跳过。改成
    **先各等一次两个票号的闭合、再无条件比位置**，并且位置取不到就 `t.Fatalf`（"没法判断顺序"是失败不是跳过），
    `u2` 才 FIRED（`queued claim at 9 arrived before the close at 15`）。
    同时承认：`u2` 我第一版变异写成了**行为等价物**（闭合帧仍然先发），那不是覆盖洞是我的变异错，
    重写后才真正改变顺序。**判据：一条 SILENT 要先怀疑变异本身没改行为，再怪用例。**
  ③ 旧读法为什么以前全绿也有了解释：`TestFakeAdapterDeliversThreeStates` 那个脚本恰好在闭合后又发了一次
    send，**两种读法产出同一条 trace**，所以它没有分辨力。本轮补的用例是第一条能分辨的（`u4` 反过来证明
    旧的那条也确实会在"完全排空"被摘掉时红，两者不互相替代）。

- **第 8 轮：CO-40 落地（`session/push` 撤销）+ 把"§10 已全部交到"从账本自述变成运行时对拍**。
  ① 主控 T-117 裁"**v0.1 明确不做 `session/push`**"，写进契约② 新增的 **§4.0**（15:20:09 实测：
     标题行「### 4.0 session/push —— v0.1 明确不做（CO-40 裁定，15:2x）」）。
     裁定理由②与我提 CO-40 时那条是同一件事、但**说得比我硬**：
     自发流是 A→C，而裁定① 刚把归属收成"全部通知带 `turn_ref`"的无例外规则 ——
     **Core 从未发出 request，就没有 Core 签发的票号可回显**，这是死结不是措辞。
     我因此**撤掉探针里那句"CO-25 还欠它一个票号故事"**（CO-25 已闭、push 已撤），两条注释改成引 §4.0：
     现在这两处守的是**一个决定**（谁偷偷实现 push，就是在重开刚收干净的归属模型），不再是"一个待办"。
  ② 主控 15:27 说"回主线：§10 剩下的 C→A 方法与 4 种注入"。我没有照字面开工，先量：§10 的"必须实现"句
     共 **11 个名字**、注入表 **4 行**，逐条对到我这个真进程上**已经全在服务**（第 5 轮 B 段交的）。
     但"已经全在"这句话以前只活在我账本里，所以本轮把它变成
     **`TestFakeAdapterSatisfiesContractSection10`**：运行时读契约② §10（不内嵌名单副本），
     ① 每个必须实现的名字都不许回答 "not implemented in this build"；② `stream/*` 这一族三种
     （delta/item/state）必须真上线；③ 四行注入各按**自己那一行承诺的行为**验 ——
     `debug/crash` 用自己的子进程验**退出码 2**（不用 `silent`，进程死了读管道会 EOF 误报成失败），
     `debug/hang` 用自己的子进程验**它真的不回答**（若共用一个子进程，这两行会把后面所有断言静默删掉），
     `debug/fail`/`debug/unknown_quota` 在共享子进程上验在服务。
     解析为空 / 锚点搬家 ⇒ 一律 fatal；文件不可达 ⇒ Skip 且写明跳过不等于通过。
     **效果**：主控哪天往 §10 加一行，或我从 §10 掉一项，用例红，不再需要有人记得来改账本。
  ③ 顺带把纪律 ⑥b（派发句里出现方法/端点名，发出前先 grep 它的契约定义处）记进账本 —— 本轮它就是为我这次
     "先量再动"背书的：我若照 15:27 那句直接开工，就会去实现一个契约里 0 定义、且刚被判死结的东西。

- **第 9 轮：§6 与 §4.2 的通知表 ↔ 六个常量 双向互校**（`TestStreamNotificationsMatchContractTables`）。
  这是 T-123 解除 blocked 之后我自己挑的活，选它的理由是**它防的正是这两天反复出现的那一族**：
  `session/push` 之所以被 CO-40 撤销，就是因为"第 7 条通知"会撞上无例外的票号规则 —— 也就是说
  **通知集合的变动是一个裁定的边界，而不是一次普通的加常量**，而此前这条边界只写在我和主控的对话里。
  设计上有两处刻意：
  ① **"我这侧"的清单不许手抄**。第一版我把六个 `Method*` 手写成 map，随即删掉 —— 手抄清单正是这个用例
    要抓的"第二份真相"，而且新加常量时人一定会漏抄。改成运行时解析 `stream.go` 自己的 const 块，
    两侧都是从盘上读的。
  ② 三个解析面各自有 fatal：§6 行数为 0、§4.2 一行没贡献、`stream.go` 解析出 0 个名字 —— 全部判 fatal
    而不是拿空集合对空集合（那种组合永远绿）。锚点找不到同理。
  双向断言的具体含义：契约有、代码没有 ⇒ 红（收到就解不开）；代码有、契约没有 ⇒ 红（就是发明通知，
  CO-40 那个死结的形状）；契约点名的任何一条若被 `CarriesTurnRef` 排除 ⇒ 红（裁定① 的六条范围不许悄悄缩回）。
  **5 条变异 w1..w5 全部 FIRED**，每条落在自己那一层（详见 Verification），其中 w1 的失败消息
  `this package emits "turn/progress", which no contract table names: that is an invented notification`
  就是这条用例存在的全部理由。
  **顺带更正一处我自己刚写下的**：Next action 上一版写"除 REQ-T12 外我包内无未交项 ⇒ 转 blocked/等待"，
  这话把"等别人"和"没有可做的"混成一件了；本轮起按 T-123 的解除口径写成"等 REQ-T12，等的时候做自维护件"。

- **第 10 轮（09-22）：`Registry.Probe`（§7.2 / REQ-AD-1 / T-129）+ Recorder 的丢弃接缝（T84-7 前半）**，
  以及 T-132 抓出的两个 P0。
  交付面：`probe.go`（Launcher 注入 + Entry/Registry/ProbeResult/Probe + `AdapterInstance()` 投影）、
  `probe_store_sqlite.go`（`adapter_instances`/`adapter_capabilities` 的 upsert，写只走 platform/db，
  事件只用 `adapter_registered`/`adapter_health_changed`/`adapter_capability_verified` 三个已登记名）、
  用例 8 条（6 probe + 2 真库 store）+ 丢弃接缝 1 条。
  **两个 P0 都是我写的，都被现场栈/失败输出定位，不是猜的**：
  ① **挂死**：夹具 `scriptedLauncher.Start` 里 `in, _ := io.Pipe()` / `_, fwd := io.Pipe()` —— `io.Pipe`
    无缓冲且**四条腿都得有人握着**，我各扔掉一端，等于 Core 写的字节没有读者、替身要读的没有写者，
    `Writer.Write` 永久阻塞，整个包的测试二进制被带走（所以是"整仓挂死"而不是"一条用例红"）。
    主控 22:41 逐条单跑定位到 `probe_test.go:106` 的栈，判"根因在夹具不在实现"——**判对了**。
  ② **实现问题**：`ctx` 有期限时能中断，但归因丢了 —— 报的是 `write initialize: io: read/write on
    closed pipe`，因为我把写放在 select 之前、又在关闭管道之后才取写错的错。修法是把写/读整体搬进
    同一个 goroutine、select 只可能报期限，并按 §7.2 补写新增的两条硬要求：
    **无期限时 Probe 自加 `defaultProbeTimeout = 10s`（具名常量，不是只活在测试里的字面量）**，
    以及"原因分层"（消息必须同时说出「没有 initialize 回应」和「期限」）。
  **两条 SILENT/自伤也记账**：x1 变异第一次是 SILENT —— 我的断言只 `Contains("deadline")`，而
  `ctx.Err()` 本身打印 "context deadline exceeded"，**归因错了也能通过**；收紧成"事实 + 原因"两半都必须在，
  x1 才 FIRED。这与记忆里"不得只验声明文本"同族，是今天第二次踩在同一类断言上。
  ③ 变异批 `mut-r10.py` 第一次 x1 报 **BUILD-BROKEN**（我又一次往多行 Sprintf 里插行尾 `/*…*/` 改坏参数表），
  改成整串替换后才是有效判决。

- **第 12 轮（09-23 05:2x，`date` 两个端点 05:22:46 / 05:26:07）· T-211 收单：归属更正 + 写口形状预研，未抢跑实现。**
  - **T-211 我照收，并且它改的是我账本里的归属**。契约① §1.1 第 17 条已按此重写（`specs/contracts-internal-v0.1.md:343-344`
    "落点与角色"、`:360` 文末更正记录②），不是我口头认领：`internal/process` 是 adapter 的包，
    "把一次生命周期观察落成一行 + 恰一条事件"的语义住这里；`cmd/core/dropaudit.go` 只做装配（递 `db.Tx`、把错误喊出来），归 backend；
    **`process_instances` 这个表名字面量仍不许进 `cmd/core`（W13 不变）**。
    顺序 = `db` 三入口 → 我这一格 → backend 装配 → 才谈合验，所以本轮**一行实现都没写**。
  - **等待条件按字复量（三处，不是引用上轮）**：第 ① 个入口 `CreateVersionless` 在 `core/` 下 **0 命中**
    （`--include=*.go`，**含测试文件**；仓库内只有两处提到它：契约 `:327` 的签名、orchestration 的 T-213 派发行）
    ⇒ 建行那条腿没有门。第 ②③ 个 `TouchTransition`(touch.go:384)/`AppendEventOnly`(:446) 与
    `AggProcess`/`AggCore`(:42-43)、`versionlessAgg{AggProcess:{table:"process_instances",guards:{status:true}}, AggCore:{table:""}}`(:68-69) **已在盘上**。
    db 还在写：`touch.go` mtime **05:22:45(21814B) → 05:22:56(21790B)**（我两次 `stat`/`ls -lt` 采到不同值），
    未跟踪新文件 `versionless_test.go`(05:21:41)。⇒ **T-209 那轮"db 五个名字 0 命中"里，只有 `CreateVersionless` 这一半还成立**，
    下一个读者要按这一条改口，别拿 05:09 的快照当现状。
  - **写口的形状（预研，供主控先裁 CO-44/CO-45 再落码）**：
    落点 `core/internal/process/`（文件名暂定 `lifecycle_store.go`）；
    装配期事实进一个 `ProcessRow{ID, Kind, OwnerType, OwnerID, Executable, RestartPolicy, Metadata}`
    （`OwnerType` 在 v0.1 只有 `'adapter_instance'`、`OwnerID = adapter_instances.id`，按 §1.1 第 18 条），
    构造时校验枚举 membership（复用 `TestEnumsFitTheFrozenProcessInstancesTable` 那把尺，别新增第二份表）；
    三个方法**返回 `error`**——void 那三条冻的是 `Recorder` 那道接缝、不是写口，写口若也 void 就和"把错误喊出来"自相矛盾；
    `Start` 走 `tx.CreateVersionless(db.AggProcess, id, cols, vals, db.EventDraft{Type: eventjournal.ProcessStarted, Aggregate: db.AggProcess, AggregateID: id, ...})`，
    `Exit`/`Crash` 走 `tx.TouchTransition(..., guardCol: "status", guardVal: <from>, setSQL: "status = ?, exited_at = ?, exit_code = ?, updated_at = ?", args, ev)`；
    状态取 `RUNNING` / `EXITED` / `FAILED`（000001 的 CHECK 六值之内，`crashes` 无列 ⇒ 继续走 `metadata_json`）；
    测试用真库（`db.Open` + `Migrate(../../migrations)`，与 `probe_store_test.go` 同一套），断"一行 + 恰一条事件"两侧。
    **一个省下来的结论**：入口收的是 `AggregateKind` 而不是表名 ⇒ 我这格**本来就不必出现 `process_instances` 字面量**，
    W13 那把尺天然满足，不需要为它再设计什么。
  - **我自己那处"注释比代码强"（本轮唯一动过的代码，`supervisor.go:158-191`，+27/−17，纯注释）**：
    旧文写 "internal/process **must not** import platform/db or eventjournal (**internal contract §0's direction**)，
    so the row shapes stay the injector's business"。**两处错**：
    ① §0 原文（`:13-25`）只禁 `db → 业务包` 与 `audit → db`，全篇没出现过 `internal/process`——
    **我把自己的推断写成了条款引用**，正是我自己那条"引用了某个契约条款而 `grep` 在 `specs/` 里查不到 ⇒ 该条款不构成判据"在防的事；
    ② "row shapes stay the injector's business" 就是 T-211 刚判错归属的那半句，留着它会把这一格又推回 `cmd/core`。
    同段另一条实测作废：`AggProcess` 现在**在** registry 里（我 13:34:37 那次读的是"没有 process kind"），
    所以 CO-39 那问被裁成了反方向（`process` 是真聚合、只是没有 version 列）。
    改法：把"没有 DB import"收窄成它真正成立的那句——**`Recorder` 接缝不出现 DB 类型**（T-62 冻的形状），
    包级 import 由写口承担；并注明"0 命中"的准确边界（`core/` 下、含测试）。
    `go build ./...` exit=0、`gofmt -l` 我三目录空（编译即时复验，防"替换右端吃掉整行"那一族）。


- **第 13 轮（09-24 07:0x~07:18，T-221 禁令解除）· `internal/process` 生命周期写口落地**。
  先读 T-221 原文与**契约① §1.1 第 20 条**（完整裁定在契约正文，派发是三句摘要），再动手；落点两个新文件：
  `core/internal/process/lifecycle.go`（226 行）+ `lifecycle_test.go`（430 行，8 条顶层用例 + 6 条子用例）。
  - **形状**：`ProcessRow{Kind, OwnerType, OwnerID, Executable, RestartPolicy}` = 装配期事实；
    `NewLifecycleStore(row, newID func() (string,error), now func() int64, logf)` **构造期**就把不可落库的形状拒掉
    （kind 不在我能监督的两个值里、owner_type 不是第 18 条登记的 `adapter_instance`、有 owner_type 没 owner_id、
    restart_policy 不在 CHECK 表里、没有 id 源、没有时钟）；三个方法**返回 error**（`RecordProcessStart` 还返回它铸出来的 id）：
    `void` 冻的是 `Recorder` 接缝、不是写口，写口静默就是 §2.2.1 那种假账。
  - **三条裁定各自落在哪一行**（不是"实现了 CO-45"这种空话）：
    ① 一次尝试一行 = 每次 `RecordProcessStart` 现铸 id、`attempt` 自增、`metadata_json` 带
    `attempt` + `previous_instance_id`（**没开新列**，000001 一个字没动）；
    ② 0 行不插事件、不换 guard 再试 = `takeEdge` 里 `changed == 0` 之后**只喊、不重试、直接返回**；
    ③ 0 行不许静默 = `logf` 一行必须含**期望的 from-state 与 row key**（用例钉这两样都在，缺一就是空喊）。
    外加"根本没行可改"那一格：不写任何行/事件，返回哨兵 **`ErrNoLifecycleRow`** 让装配方去登记审计
    （**我没有开审计出口**：`audit_entries` 的字面量与出口不归我，W13-2 那条对我也成立）。
  - **实现期我自己抓到两处、都写成了用例或变异**：
    ① `created_at`/`updated_at` 一开始跟着 `ev.Timestamp` 走 ⇒ 把"行的出生"与"观察时刻"缝成一个数，
    晚记的 start 会伪装成"当时就写了"；现在 created/updated 走 `s.now()`、`started_at` 才走观察的 ms（m9 变异专咬这条）。
    ② **guard 跟着状态前进 ⇒ 重放会移动自己的后态**：我原来在落边后做 `s.curStatus = ev.Status`，
    于是同一行第二次 exit 的 guard 变成 `EXITED`、正好命中自己 ⇒ 又改一次行、又记一条事件，
    **只被 `event_journal.event_id` 的 UNIQUE 拦住**（m3 复现原文：`append event process_exited for proc-a v3: UNIQUE constraint failed`）。
    改成 `landedStatus`（**=this cell 当初写进去的状态，永不前进**），幂等闸才真的咬得住。
    通用一条：**幂等 guard 的 from-state 必须是"我写进去的那个值"，不能跟着观察走。**
  - **不 fudge 状态**：`status` 直接取 `ev.Status`（`supervisor.go` 发的是 `STARTING` + 真 pid），
    不在库里把它升成 `RUNNING`；第 20 条 Recovery 的口径把 `STARTING` 算活，所以没什么需要补。
    **副作用要交给别人**：`exit_code`/`exited_at` 的可断言面只在"先落了 started 那一行"之后存在（契约④已经写了），
    ⇒ `TestLifecycleRowLandsTheFiveColumns`（backend/test 的格）**必须先建再落 exit**，我已列进 Coordination 转告。
  - **包内边界从注释变成测试**：`TestOnlyTheWriteCellTouchesThePlatform` 用 `go/parser` 读真实 import，
    只许 `lifecycle.go` import `platform/db`/`eventjournal`，并带一条"扫到的非测试文件数 < 2 ⇒ `t.Fatal`"防架空
    （CO-43 的教训：注释里那条"§0 不许 import"是我自己造的，所以这次不留给注释）。
  - 顺手按主控要求把 `supervisor.go` 那句"理由是 CO-44/45 未裁"改成"已裁，见契约① 第 20 条"（+9/−7，纯注释），
    并把 `AggCore: {table: ""}` 那一问**作废**——T-224③ 说 db 早在自己账本里答过（有意设计，空串 = "没有行可改"），
    答案落在我看不见的地方；这条不是我该继续挂着的待办。

- **第 13 轮补记（09-24 07:31~07:36，T-227 交账两件）**：
  - **② 双向自证**跑完，**结论比"做完了"更值得记**：在**我自己那一面**它成立且很硬——把写口两腿分别摘掉
    （`r1` 摘 `CreateVersionless` 那条、`r2` 摘 `TouchTransition` 那条，各是**一处早退**、其余代码原样）：
    r1 ⇒ 顶层 **5 条 FAIL / 27 PASS / 1 SKIP**，红的正是那 5 条真库用例，首行
    `no process_instances row for proc-a`；r2 ⇒ **4 FAIL**，首行 `status = STARTING, want EXITED`；
    r1+r2 ⇒ 同 r1（5 FAIL）。**0 条 SKIP、0 条 BUILD-BROKEN**，摘与还原在同一条命令内、`restored_identical=True`、
    `grep -rn MUTANT-R13 internal/` **0 残留**。
  - **但主控点名那一格（`TestLifecycleRowLandsTheFiveColumns`）现在证不出来，成因已实测**：`cmd/core` 里
    `grep -rn LifecycleStore core/cmd/core/` = **0 命中**（装配未动，那是 backend 的 T-225③ 之后），
    而带 `LUCERO_REQ_DB2=1` 跑它，**摘我的写口与不摘的输出逐字只差计时行**（`0.244s` vs `0.248s`，diff 只有 2 行），
    失败原因两句完全同一：`lifecyclecolumns_test.go:290: LUCERO_REQ_DB2=1 但这段还没接上真写路径…`。
    ⇒ 它今天红是**因为 backend 那格没接**，不是因为我的写口；**"摘掉写口 ⇒ 它必须红"这条要等装配落地才有意义**，
    届时同一条命令（带 flag、`-run` 单跑、且必须 `--- FAIL` 而非 SKIP/build failed）就是验收。我没有去动 `cmd/core`。
  - **① 的形状交账**照旧（见 Verification 末段），HEAD 已在 `34074a0` 之后继续推进，本轮**只改 `lifecycle.go` 一处注释**：
    `landedStatus` 那段现在点名**契约① §1.1 第 20 条 ⑤**（主控把我那条做法升成了契约规则），
    并把你给的前提写死在注释里：**没有任何事件带 `StatusRunning`** ⇒ 谁拿"我观察到它在跑"当 guard 就**恒 0 行**、
    每次退出都会被静默判成"这条边已落过"。
    **第 20 条 ⑤ 在我这面本来就有牙**：上一批变异 **m4 就是把 guard 换成 `StatusRunning`** ⇒ `TestExitMoves…` FIRED
    （0 行、`exit_code` 从没落进库），所以这条契约规则不是靠注释守的。巡检 07:31 的那三条行号引用我逐条复核为真
    （`supervisor.go:420` 发 `StatusStarting`；全仓非测试 `StatusRunning` 只 1 处、正是 `:36` 的常量定义；
    `lifecycle.go:51/:56/:150/:209` 形状对得上）。**引用一律按文字不按行号**（这条纪律我自己刚破过一次，见上一条 CO-47 的更正；
    而且**就在复核之后我改了 `landedStatus` 那段注释、这四个号当场又漂了**——它们只作为 07:31 那次的现场读数留着）。
  - 顺带对一句时间线，**并且承认我这边的证据也不全**：T-227 说"07:07 收到禁令解除、07:17 落码"。
    `07:17 落码` 与盘面对得上（`lifecycle.go` mtime 07:17:32、`supervisor.go` 07:17:57）；
    **但"07:07 收到"我既不能证实也不能证伪**——我在读到 T-221 到读完第 20 条那一段**没有落任何 `date` 读数**，
    本轮第一个真实时钟是 07:18:08 那次跑。所以**我不写"实际是 07:2x 才读到"那种句子**（那是我上一版差点留下的第二个未取证时间）。
    记这条的意义：主控的进度判断里若混进一个没取证的时刻，下一个读者会拿它当基准；**修法是我自己在每个动作前落 `date`**，
    而不是要求别人每句话都带读数。

- **第 13 轮补记 2（09-24 07:42~07:44，T-230 + db 代转；零代码改动）**：
  - **db/patrol 那两条我先按内容复核（不按 db 给的行号，我 07:36 改过注释后那些号又漂了）**：
    建行写的 status 是 `string(ev.Status)`（`lifecycle.go:142`）、落边也是 `string(ev.Status)`（`:218`）、
    `s.landedStatus = ev.Status` 在建行成功后（`:157`）、guard 用 `string(s.landedStatus)`（`:216`）、
    本文件 `AppendEventOnly` **0 命中** ⇒ **巡检 07:42:32 的纠正为真、我的形状不用改**：初态是事件自带的 `STARTING`
    （`supervisor.go:420`），**不是 `RUNNING`**；我要是把 guard 改成 `RUNNING` 就正好踩进第 20 条 ⑤ 那个陷阱（第一次就 0 行、
    `exit_code`/`exited_at` 全丢，第二次撞 `event_id` 的 UNIQUE）。**顺带纠 db 那句括号里的一个措辞**：
    它说"② 拿观察态当 guard 第二次还撞 UNIQUE"——**UNIQUE 是"guard 跟着前进"那种写法才会撞**（就是我 07:1x 那个被 m3 复现过的错），
    今天的形状下第二次是干净的 0 行 + 一句喊（`TestReplayedEdge…` 钉的就是这一对）。
  - **T-230 让我"动手前先核方法名/端点在契约里存不存在"，我当场对 T-230 自己那句用了一遍，量出一个 0 命中**：
    原文"`turn/dispatch` 发出、拿 adapter 回执"里那个 **`turn/dispatch`（不带 -ed）在 `specs/` 全树 0 命中**，
    而契约② §4 的 C→A 方法表里**一行 `turn/*` 都没有**（`awk` 量 70~112 行 = 0 命中）；
    有名字的是 **`turn/dispatched`（5 命中：§4.2 标题 + 载荷表 + §6 规则 5 + …）**，它是 **A→C 的取证帧**，
    而"把活交给 Adapter"那条腿在契约里叫 **`session/send`**（C→A、§5 三态、`Idempotency-Key` 必填；
    §4.2 那句还写明"`session/send` 必须把 `execution_id` 交给 Adapter（CO-24）"）。
    ⇒ **我不猜主控是想说 `session/send` 还是 `turn/dispatched`**，写成 CO-48 请回一句；Wave-2 连接侧那一半的**对外形状我不自扩**（T-230 原话）。
  - **一条我自己的取证纠错，因为它差点变成假结论**：我第一次 grep 用的是 `specs/*.md`，**0 命中两种拼法**——
    契约② 其实在子目录 `specs/adapter-protocol/adapter-protocol-v0.1.md`，模式没覆盖到目录 ⇒ 差点报成"契约里连 `turn/dispatched` 都没有"。
    改成递归 `specs/` 才是上面的数。**"0 命中"要先问一遍我的口径盖住了什么**（记忆里那条老规矩，今天又差一次）。
  - 等待条件不变，并按主控 07:41 那句自量复核：`grep -rn LifecycleStore core/cmd/core/` = **0**、
    `ProcessRow` = **0**、`CreateVersionless|TouchTransition` = **0**，`dropaudit.go` mtime 仍是 **09-23 01:30:01**
    ⇒ backend 那格还没动（它 07:2x 起在 `lock.go` 上，REQ-L-1）。

- **第 14 轮（09-24 08:12~08:14，T-240 收单）· 零代码改动，但三句读数必须报**。
  - **T-240 是更正 + 结案，我照"你这轮不必落码"做了**：本轮没动任何 `.go`（对 `lifecycle.go` 只有一次**字节级摘除→还原**，
    脚本内 `assert 读回==原字节` 通过；见下条为什么我还跑它）。
  - **同一封信里那句"你的等待条件仍是等 backend 接 `cmd/core` 装配"，在我复量时已经不成立**（这就是"前提要复测"那条的现场）：
    `grep -rn LifecycleStore core/cmd/core/` = **5 命中**、`ProcessRow` = **1**，`dropaudit.go` mtime 从 09-23 01:30:01 走到 **08:10:47**，
    `processpipe.go:114` 在构造 `process.NewLifecycleStore(process.ProcessRow{...})`。
    我只读不判他们的批次，两件与我形状直接相关的复核过了：
    ① **一个 Supervisor 一个 store**（`processpipe.go:77` 与 `Rec:` 同一处构造）⇒ **CO-45(a) 的 `attempt`/`previous_instance_id` 链成立**
    （若它被写成每次拉起新建一个 store，那条链就会永远停在 attempt 1）；
    ② `writeLifecycle` 三格共用一条通道：**成功也喊（带行 id）、失败也喊（带原因）、没装配喊另一句**——
    "没人挂桥"与"挂了但没落库"在日志里是两句话，这正是 void 接缝唯一诚实的补法，我这边不需要为它改形状。
    顺带一条真发现进 **CO-49**：§18 那唯一词表值现在两包各存一份 const。
  - **T-227 ② 那条双向自证今天仍然交不出，而且这次的原因和昨天不同，我不能拿它冒充结果**：
    `cmd/core` **此刻测试构建红**（`lifecyclecolumns_test.go:286:13 undefined: strconv`、`:392:28 undefined: process`，
    该文件 mtime 08:13:15 → 08:13:35 → 08:13:58 = backend 正在写）。我在 08:13:35 试过一轮三个变体，
    **三份输出逐字相同**——因为**它根本没编译** ⇒ 那是**空洞的相等，不是"摘掉也红"的证据**，我把它划掉。
    等 `cmd/core` 测试恢复可编译，同一条命令就是那一格的验收判据（配方在 Verification），**我这边随时能配合重取**。
  - 一句纪律留档：巡检说"本行是更正通知 + 结案，收到即可"，同时也说"你这轮不必落码"。
    **两件事我分开处理**：不落码照做；但"还在等装配"这句不成立，我就如实报"已接上"，
    **不自作主张开工，也不假装还在等**（后者会让主控的进度表少一格）。

- **第 15 轮（09-24 08:41~08:44，T-248）· CO-49 动作做完 + CO-50① 复量把主控那句"过期了"纠正回"只对一半"**：
  ① 按裁定导出 `OwnerTypeAdapterInstance`（三处引用 + 注释点名 §1.1 第 18 条 + 理由"装配层不含业务判断，值也一样"），
  **零行为变更**、旧名 0 残留、build/gofmt/32 条用例全绿；backend 那份字面量抄本由 T-247② 收走。
  ② 把 T-248 那句"你的读数过期了，不用动码"按字复量成三条整包级证据（基线 `ok`、摘建行腿两条用例红、
  **摘改行腿整包仍 `ok`**）⇒ **`nil` 那一半确实过期，终态边那一半没有**；措辞上我不判 backend，
  只把"别让整包绿被读成终态边有牙"和配方交给他们（详见 CO-50①）。
  ③ 顺手把两条已经消失的现象标成历史读数（08:13 的 `cmd/core [build failed]`、08:18 的 `TestLauncherCarriesARecorder` panic），
  并给 `lifecycle.go` 换新指纹（导出改了内容，旧 sha256 随之作废）——**"这条读数还成立吗"这个问题每轮都要先问一遍**，
  今天它对我自己同样适用。

- **第 16 轮（09-24 09:03~09:05，T-255 收单）· 零代码改动，四句按字复核 + 一条我自己的口径漏洞**：
  - **① 我的配方已经进了契约正文**：§17 的落地判据那段现在写着"**三条腿 + 每腿一条反例**"
    （`RecordStart` 建行腿 / `RecordExit`+`RecordCrash` 改行腿 **各自**都要在"装配好的形状下被驱动"，
    **默认面不驱动不算**）、反例形状固定为"摘掉某一条腿 ⇒ 对应用例必须红、且与基线输出两两不同"、
    并写明"**不另开一节硬前置**，`test` 只在合验时复核、不重做"（主控 ② 的理由我认：两节各钉一条"必须红"会留下"听哪节"的空隙）。
    验收方仍是 `backend`（装配）+ `adapter`（写口）⇒ **这条对我不是"已交付"，是"我有一腿要陪验"**。
  - **② CO-50① 那条腿今天还没闭**（不是催，是别让"契约已改"被读成"判据已有"）：
    `grep -rn "RecordExit\|RecordCrash" core/cmd/core/lifecyclecolumns_test.go` = **1 处**、该文件 mtime **08:38:06**
    ⇒ 三条腿里"改行腿在装配好的形状下被驱动"这一条**尚无实现**；配方在正文里，落点在 backend。
  - **③ CO-49 我侧按字闭**：`grep -rn ownerTypeAdapterInstance core/internal/process/` = **0 残留**、
    导出名在 `lifecycle.go:105`；`grep -rn OwnerTypeAdapterInstance core/cmd/core/` = **0** ⇒ backend 那份 `:87` 抄本未改引用
    （T-253 明写它排在分派腿之后、不催）。三方读数一致（我 09:03:22 / 主控 08:56 / 巡检 09:01:23）。
  - **④ 一条我自己的口径漏洞，比上面三条都值得记**：主控说"全仓现在有一条预期红"，我去我的 -v 日志里找它 ——
    **0 命中**。原因是 `TestEventCatalogMatchesJournalConstants` 在 **`core/test/errcode_mapping_test.go:748`**，
    而我交付协议里的"全仓"从来是 `./internal/... ./cmd/... ./api/...` = **17 包，不含 `core/test/`（`test` 角色的门）**
    ⇒ **我报的"全仓 exit=0"一直少盖一个目录**，只是过去没撞上。今天补两件事：
    报绿时"全"要显式写"不含 `core/test/`"；并且顺手单跑一次它 —— **09:04:53 `--- PASS`（`C8-12：目录 64 类 / 常量 64 个`、双向相等）**
    ⇒ **主控 09:01 那句"现在有一条红"到 09:04 已经不成立**（db 的 P1 落完了；我只报读数，不判它的批次）。
  - 一次读数丢失的处置：09:03:37 那趟 `-v` 跑完，我的 `echo "exit=$RC"` 那一行在批量回显里**整段不见了**
    （这一族今天第二次）。我**没有**从"0 条 `--- FAIL`"去推 exit code，而是 09:04:16 重跑一趟 quiet 版取回
    **`exit=0`（17 行 `ok`/`FAIL`、0 条 `FAIL`）** —— 计数与判决要分别有出处。

- **第 17 轮（09-24 10:01~10:07，T-263 乙案）· 导出 `adapter_key -> instance id` 的只读解析**。
  - **动手前先把这一格的现状量了一遍**（backend 09:35 数的、主控核过，我自己的读数是 10:01:07）：
    `lookupInstance` 确在 `probe_store_sqlite.go:59`、**包级私有**（`func lookupInstance(...)`，不是方法），
    两个内部调用点（`:84` RecordProbe、`:195` MarkUnverified）；`ProbeStore` 导出面**只有两个写方法**
    ⇒ "非测试码里没有只读出口"这条前提成立，本行不过期。
  - **落法**：方法挂在具体类型 `*SQLiteStore` 上（`ResolveInstance(ctx, adapterKey) (string, error)`），
    **没有塞进 `ProbeStore` 接口** —— 那个接口是 `Registry.Probe` 消费的写侧密封，加一个读方法会逼所有替身实现它；
    Go 的惯例是消费方自己声明需要的那一面，backend 一行 `interface{ ResolveInstance(...) }` 就够，不需要我动形状。
  - **边界两条都守住了**：① 只回 `id` 一个 string，**不返回行结构**（`instanceRow{id,health}` 仍私有）也不加任何写腿；
    ② 没碰 `specs/openapi/`（`grep -rn "ProbeStore\|ResolveInstance" specs/` = **0 命中**，
    我的导出面从来没被契约逐条列过 ⇒ 这一次**没有制造"代码有、契约没有"的滞后**；要不要把这一句补进 §7.2 归主控，我不自行登记）。
  - **哨兵那一半按 transport 的真形状对过字**（不是我推测的）：`core/api/http/adapters.go:55` 写的就是
    `case errors.Is(err, sql.ErrNoRows), errors.Is(err, db.ErrAggregateNotFound):` ⇒ 404 那一臂认这两个。
    我用 `db.ErrAggregateNotFound` **包一层带上 key 名**（`%w`，`errors.Is` 仍真），
    并且**空 key 不特判**：它匹配不到行，就回同一个 not-found —— 少一条我自己发明的分类。
  - **用例三条（真库，`probe_store_test.go` +67 行）+ 变异五条（4 FIRED / 1 条预测中的 SILENT）**：
    `TestResolveInstanceNamesTheRowProbeWrote`（q3 读 health 不读 id ⇒ FIRED，原文 `resolved "HEALTHY", want the id RecordProbe wrote`）、
    `TestResolveInstanceSaysNotFoundForAnUnknownKey`（q1 静默返空串、q2 把 `%w` 写成 `%v` 双双 FIRED，
    q2 的证据行是 `err = resolve adapter ghost: aggregate row not found, want errors.Is(...)` ⇒ **"看着像、errors.Is 不认"这种错被抓住了**）、
    `TestResolveInstanceWritesNothing`（q4 让读口 miss 时顺手 `MarkUnverified` ⇒ FIRED，`instance rows went 1 -> 2`）。
    **q5 是预测中的 SILENT**：把 miss 分支里那次 `rows.Err()` 先查再判 not-found 摘掉，没有任何用例能红 ——
    真 SQLite 单行 SELECT 在进程内造不出"迭代中途报错"。我**留着这三行**，因为它的失效方式是"把库故障答成 404"（归因错），
    并在代码注释里写死了这个理由，**不拿"有测试覆盖"冒充它被覆盖**。
    还有一条**没有变异、只能读码**的断言：这一腿走 `d.Read` 而不是 `d.Write`（读不该排到串行写通道后面，
    与 approval/permission 的读法同形）—— 把它改成 `Write` 需要换签名，进程内两条路径给出的答案一模一样，
    **所以这条是"码读得到"，不是"测试测得到"**。
  - **等待条件按巡检那句改了代**：CO-49 我 10:01:07 自己复量确认**两半已合**（`core/cmd/core/` 非测试里
    `"adapter_instance"` 字面量 **0 命中**、`OwnerTypeAdapterInstance` 引用 **2 处**）⇒ 那一格从我的"欠账"里销掉。
    今天真正欠的只剩 **`test` 侧的合验发令**（§17 三条腿里"改行腿在装配好的形状下被驱动"那条仍归 backend，10:01 量它还不存在）。

- **第 18 轮（09-24 10:37~10:46，T-275）· 给真黑盒加了一扇门**。
  - 动手前复量：`grep -rn LUCERO_FAKE_ADAPTER_BAD_EXECUTION_ID core/ specs/` = **0**（本行不过期），
    并确认了"为什么今天证不出来"这句话的形状事实：两条取证帧的 `execution_id` 都取自 `t.executionID`
    （`main.go` 的 `evidence()`，值来自 Core 递进来的 `p.ExecutionID`）⇒ **外面没有任何路径能造出矛盾**，
    绑定关系在 Core 侧（`connection.go` 的 `execByRef`），假适配器只会照抄。
  - **落点是一处、不是两处**：`evidence()` 里算一个 `exec`，两条帧共用 ⇒ 门只有一个 choke point；
    开关读取在 `run()` 里做一次（`badExecID` 字段），没有新增 `debug/*` 方法、没进 §10 的能力面。
  - **三条限定各自都有反例，不是我口头保证**：
    "只在 fake 生效"= 本轮改动只在 `core/cmd/fake-adapter/`（产品码 `internal/**` 零改动，见 Changed files）；
    "默认关闭"= `TestBadExecutionIDGateOffEchoesWhatCoreBound` + **`...IgnoresOtherValues`**（`0`/`yes`/`true`/空格都算关，
    变异 b1 把条件放宽成"非空即开" ⇒ FIRED，这条用例的存在才有意义）；
    "不改协议声明与帧形状"= **`...MovesNoFrameShape`**（逐帧比对 params 的**键集合**、顺序、`turn_ref` 不动，
    只有 `execution_id` 的值变）+ 变异 **b5'** 证明它有牙：**只在门开着时多加一个 `turn_id` 键 ⇒ FIRED**。
    b5 的第一版我做错了一次（把多余的键**无条件**加上，于是开/关两边都有同一个新键，对拍自然看不见）——
    **那次不是 SILENT，是无效变异**，改对成 b5' 之后立刻红，这条区别按老规矩写清。
  - 还有一条我自己加的限定（主控没要求，但它属于"这扇门能被误用"那一族）：**门必须喊出来**。
    `...OnContradictsTheBinding` 里断子的 stderr 同时含 env 名、注入的 id、Core 原本绑的 id
    —— 黑盒测试把审计 seam 撞出来之后，要能说出"我谎报的是哪一个"，而不是让读的人再从帧里反推。
    变异 b4（删掉那行 `Fprintf`）⇒ FIRED。**这条断言读的是子进程 stderr，所以 `sendOnce` 里先 `stop()` 等进程退出再读**：
    `os/exec` 拷 stderr 是它自己的 goroutine，进程还活着就读缓冲区会吃掉正好那一行。
  - 一个"合法形状"的实证细节：b6 把注入值换成空串 ⇒ `DecodeStream` 当场拒（
    `INVALID_PARAMS: turn/claimed has no execution_id (CO-8)`）⇒ 用例红 ⇒ 证成"注入值必须仍是合法形状"这句话有牙，
    而不是一句自我表扬。
  - 交付跑：**10:46:11→10:46:19，exit=0，469 PASS / 6 SKIP / 0 FAIL，17 包全 `ok`**；
    `cmd/fake-adapter` 逐包 **22 → 26 PASS**（+4 正对本轮四条用例）。变异 6 条有效、**6/6 FIRED**、还原 `restored_identical=True`。

- **第 19 轮（09-24 10:53~10:56，T-276 落规）· 承认这是我的账本缺陷，不是巡检误报**：
  - 巡检 10:53:00 量到：我文件 mtime 10:49（正文在动），但头部 `- Last verified action` 现读仍是 **09-22 22:56:35/22:56:43**，
    `Stage` 停在第 5 轮（09-21~22），`Status: in_progress` —— **旧了约 1.5 天，属实**。
    成因是我的老习惯：我每批刷 `LAST_UPDATED` 与 `- Next action:`，**却把 `Stage`/`Status`/`Last verified action` 当成"某次大交付时顺手写的一段历史"**；
    而巡检"谁在等什么"那格指纹**只读头部** ⇒ 我这三行对它就是**每轮必读错的索引**。
    这与今晚主控自己认的两条同族（拿队列计数当因果、拿 mtime 当作者）：**索引滞后不让证据出错，只让下一个查索引的人做错决定**。
  - 处置：头部四行按新规矩全部重写成现状（`Stage` = 三件已交付 + backend 已接线；`Status` = idle 无阻塞；
    `Last verified action` = **当场重跑取的真读数 10:54:03→10:54:11、exit=0、469/6/0、17 包全 ok**；
    `Next action` = **三格"等"各自写全"等什么 / 谁解锁"**）。旧的四段**一个字没删**，改为缩进子条目留在本节下方并逐段标注
    "（旧 …，按 T-276 留档不当现状）"。自检口径也升级：`grep -c '^- Stage:' / '^- Status:' / '^- Last verified action:' / '^- Next action:'`
    **每个都必须 = 1**（这条我现在跑，结果见 Verification）。
  - 我自己再加一条限定，因为 T-276 第 ③ 条有个残留风险：**"头部与正文不一致时以正文为准"是读侧的兜底，不是写侧的免责**——
    以后我的收尾协议里"头部四行已刷 + 四个计数各 =1"是**交账完整的定义的一部分**，
    少一行就自我记"头部未刷 = 交账不完整"，不等别人来纠。
  - **这把尺当场就抓出我自己的两个错**（所以才值得写成机械计数，而不是"我小心点"）：
    ① 我一开始只看了 `Stage`/`Status`/`Last verified action`/`Next action` 四行，漏了**第五行 `Verification`**
    （主控这次说的是"那四行"、记忆里那条写的是"五行"）⇒ 加了新的 `- Verification:` 之后**同一节里出现两条同名 live 字段**，
    `grep -c '^- Verification:'` = 2 才把它逼出来，旧那条已降级为留档；
    ② 我在留档注里顺手写了一句"那条旧命令三点形式 Go 不接受、本身是 `[setup failed]`"——**没验，是编的**；
    10:58:54 实跑证明 `./dir/...` 形式合法、三包 `ok`，我记错的是**当年那条命令失败的原因**（当时 `internal/process` 这个包还不存在，
    不是形式错）。当场把这句改成实话。**同一轮里我用一把新尺子量出自己两个错，这两条都算数、都不是别人抓的。**
  - 现在的头部五字段自检读数（10:59）：`^- Stage:`=1、`^- Status:`=1、`^- Last verified action:`=1、
    `^- Next action:`=1、`^- Verification:`=1，`^## `=12。

- **第 20 轮（09-25 18:32~18:36，T-288 条件式合验令）· 到 ready，且把"预备"做成一条可复跑的命令**。
  - **先复量触发前提，不照抄巡检那份清单**（18:32:51 实读）：巡检说的两件已经变了 ——
    `core/api/http/tasks.go` **不再脏**、`scripts/check_ownership.sh` **不再脏**（他 18:20 那份已过期），
    现在脏的是 **backend 的 T-286 在途**：`core/cmd/core/sendpipe.go`(M) + `?? core/cmd/core/sendpipe_test.go` + `core/internal/task/service.go`(M)，
    外加 frontend 两件与五本台账。⇒ **判据"backend 那件消失 + HEAD 前进"未成立，本轮不是触发**。
  - **一条对主控 ② 有直接影响的坏消息（好消息是我这边不欠）**：我那三个未跟踪件（`lifecycle.go`、`lifecycle_test.go`、`badexec_test.go`）
    **已在 HEAD 里**（`git log -1 -- <文件>` 三件都指 `2c56023`），所以"工作区必须干净"这一条**不会再被我的在制品卡住**；
    现在卡住它的是 backend 那三件 + frontend 那两件 + 台账。
  - **协议侧六项逐条落到具体用例名**（不是为了好看，是因为 T-288 要求"逐条带读数"，而"整包绿"给不出逐条）：
    握手 = `TestNegotiate`/`TestNewHandshake`/`TestFakeAdapterHandshakeSatisfiesCore`/`…RefusesBeforeHandshake`/
    `TestConnectionStreamBeforeInitializeIsRefused`/`…RefusesBeforeInitialize`/`…InitializeStartsThePump`（7 条）；
    能力核定 = `TestEffectiveCapabilities` + Probe 六条 + `TestSQLiteStoreRecordsOneProbeEndToEnd`（8）；
    `CarriesTurnRef` 六条 = `TestStreamNotificationsMatchContractTables`（契约双向对拍）+ `TestStreamFrame*`（运行时构造/解码）（2）；
    票号↔`execution_id` **两侧都有** = 正例 `TestConnectionDedupsAndTracksTurn`、`TestConnectionAgainstRealFakeAdapter`、
    `TestBadExecutionIDGateOffEchoesWhatCoreBound`；反例 `TestConnectionDropsContradictoryIdentity`、
    `…DropsUnboundTurnRef`、`TestBadExecutionIDGateOnContradictsTheBinding`（8）；
    丢弃到审计接缝 = `TestDroppedFrameReachesTheAuditSeam` + 写口的 `TestEdgeWithoutARow…AuditSentinel`（2）；
    链 = `TestFakeAdapterChainIsClaimedThenDispatched`、`…QueueDrainsAtTurnFinishedNotAtNextSend`、
    `TestConnectionChainClosesBeforeItOpens`（3）。**六项 30 条，实跑非绿 = 0**。
    **一处归属要说清**：六条全量的端到端那份在 `core/test/turn_evidence_test.go:234`（`test` 的门），
    **不在我这 17 包里** —— 我这边是"契约对拍 + 运行时两侧"，别把我的读数当成第六类全量的替代。
  - **预备脚本本身被抓出一次错**：第一版 `item()` 里我写成 `${pat)}` ⇒ 六项全部打印 `bad substitution`、
    数字全空。是我的 dry-run 抓的（**"抄进契约/脚本的命令行必须先真跑一次"这条老规矩又一次生效**）；
    修好 + 加"编译不过的包名与首条错误"两行后才拿到上面那组数。
  - **合验该等不等得到，我给了个客观信号**：同一条命令两次跑，`internal/task/service.go` 的**首条编译错都变了**
    （18:34 `:227:31 undefined: session` → 18:35 `:309:10 undefined: newLinkID`）⇒ backend 正在写；
    而我三包的构建指纹两次**完全一致**（`afbc9a1b…`/`c6a53803…`/`38a594aa…`、`deps 67a1287e29df29c3`）
    ⇒ **被验面里属于我的那一半没有动**，这就是"合验不能跑在正在被改的树上"的现形版本。

- **第 21 轮（09-25 18:53~18:55，T-295 归因口径）· 零代码改动，预备读数换了一代**。
  - **T-295 我按"报告形状"落地，不是按"记一条规矩"落地**：把预备脚本的 `item()` 改成**印用例名清单**
    （= 主控要的"覆盖的判据行"），**输出里不出现任何人名/作者字段**——归属只进这本台账，不进合验结论。
    改完真跑一次才算数（老规矩：抄进交付物的命令行必须真跑过），输出见本轮 Verification。
  - **触发判据我自己复量（18:53:17，不抄巡检那份）**：HEAD `4b4739e`（18:47:07）；
    porcelain 15 条 = 六本台账（含我自己这本）+ frontend 两件 + **backend 五件 ` M`**
    （`core/api/http/tasks.go`、`cmd/core/sendpipe.go`、`internal/task/{domain,repository,service}.go`）
    + **两件 `??`**（`api/http/tasks_link_test.go`、`cmd/core/sendpipe_test.go`）
    ⇒ 主控那两条（backend 那几件消失 + HEAD 从 `4b4739e` 前进）**都还没到位**，我不启动。
  - **但一个障碍已经消失，这条必须报，否则下一轮的人还会去找那四包**：18:54:09→18:54:2x 那次全量
    **`exit=0`、498 PASS / 7 SKIP / 0 FAIL、17 包全 `ok`、`build failed` = 0**
    —— 我 18:36 那次读到的"4 包编译不过（根因 `internal/task/service.go` 未定义符号）"**已经被 backend 自己修掉了**，
    他们现在只是**没提交**、不是**编不过**。⇒ T-293① 那道"预检不过就不起采样"的门，此刻**预检是过的**；
    挡着的只剩"未提交"这一条，而那正是主控判据的内容，不由我判。
  - **`95b01ca` 那个事实我核过，写进台账**：`git show --stat 95b01ca` = 4 个文件
    （`orchestration.md +22/−4`、**`scripts/check_ownership.sh +20/−3`**、`api-contract +17`、`contracts-internal +4`），
    message 只写 "specs+ledger" ⇒ 确实漏的是**message 没写**、不是"混提"本身；
    形状 E 那道排除**归属仍记 `test`**，我这边一句"这是谁写的"都不写，按新口径只写指纹与覆盖行。
  - 我的三面指纹两次一致（构建时刻 18:34:48 与 18:54:13）：`deps 67a1287e29df29c3`、`bin internal/adapter afbc9a1b8fa9b74f`、
    `internal/process c6a53803aa7ce5c1`、`cmd/fake-adapter 38a594aa65efbca8`，
    **而 HEAD 从 `95b01ca` 前进到了 `4b4739e`** ⇒ "被验的是不是那份码"这件事，**HEAD 变了不等于我的面变了，HEAD 没变也不等于没人在改**（backend 五个文件在两个 HEAD 之间一直是脏的）。
    这正是 T-295 要的结论：**能归因的是指纹，不是提交字段。**

- **第 22 轮（09-25 19:11~19:15，T-297 合验令已执行）· 协议侧 + 行级两条腿的反例都在同一棵树上跑完**。
  - **发单前提我先自己复量（19:11:54），两条都成立**：HEAD `f25ee6c`（19:04:01，父 `a9f975e`）、
    `git status --porcelain -- core/` = **空输出**、全仓只剩六本台账 + `apps/desktop` 两件 + `scripts/flaky_watch.sh`
    —— 与主控/巡检那份**逐字对得上**（我不抄，是自己量的）。
  - **协议侧六项 30 条覆盖行、非绿 = 0**（19:12:39→19:12:4x 那次，全量 `exit=0`、498 PASS / 7 SKIP / 0 FAIL、17 包全 ok、
    `build failed` 0）；三面指纹与 18:35/18:54 两次**一字不差**（`deps 67a1287e29df29c3` + 三个 bin），
    即"上一代对照"这一段（巡检 19:10 建议留着，我留了）。
  - **行级那一半今天第一次真跑（带 `LUCERO_REQ_DB2=1`）**：`TestLifecycleColumnAssertionsHaveTeeth`、
    `TestLifecycleWriterLandsRowAndShouts`、**`TestLifecycleThreeLegsEachMoveTheRow`**、`TestLifecycleRowLandsTheFiveColumns`
    **四条全 PASS**、包 `ok`（19:12:5x）。
  - **§17 "三条腿 + 每腿一条反例"我按字验了，两条反例都是真红**（摘我包里的入口、字节级还原在同一条命令内、`restored_identical=True`）：
    摘建行腿 ⇒ `cmd/core` **三条全红**（首行 `process_instances 该恰好一行…: sql: no rows in result set`）+ 我自己包 **5 条红**；
    摘改行腿 ⇒ **`TestLifecycleThreeLegsEachMoveTheRow` 红**，首行
    `干净那条腿该把行改成 EXITED/exit_code=3，实际 STARTING / 有=false 值=0` + 我包 **4 条红**。
    ⇒ **CO-50① 那个缺口今天由 backend 的 `TestLifecycleThreeLegsEachMoveTheRow` 补上，并有反例证明它有牙**，我这边判闭（不是"看它绿了"）。
  - **一条我自己差点交上去的空判决，如实记**：反例脚本第一版我用 `-run 'TestLifecycle…'` 去过滤**我自己的包**，
    可我包里的用例叫 `TestStartLands…`/`TestExitMoves…` ⇒ **一条都没匹配上，输出 "PASS=0 FAIL=0 exit=0"**，
    长得像"我的包不敏感"。是我自己那行 `基线 internal/process | PASS=0` 的可疑零逼我回去看的（`-run` 空匹配也报 ok 那条老规矩）。
    改成不过滤重跑才是上面那两组数（32 基线 / 5 红 / 4 红）。**如果不补这一趟，我就把一次"没跑"报成了"跑了没红"。**
  - **并发负载（T-297 要求写明）**：`test` 的 FL-10 电池**在我这批跑的同时在动**——起跑前探针
    19:12:36 见 `flaky10b_seg_190822.log` 6088B 且 3 秒前刚写过；19:12:59 那个文件已不在；19:14:03 又出现
    `flaky10c_run.out`/`flaky10b_segments.txt`。⇒ **我的读数是带载测的**。
    **为什么我没等它**：我这批全是**判决型**断言（PASS/FAIL、反例必须红），不是量级/时序型；
    而"等不等"这件事本身我没有可验证的收敛条件（电池何时结束不由我知）。**若哪条真红了，我会单包复跑一次再归因**，
    这批一次都没红，所以没触发那条。**这条判断留给主控复核**：如果合验结论要参与量级腿的对比，应当只取"无并发"那一批。

- **第 23 轮（09-25 19:42~19:46，T-302 ①②③④：同一条命令的空机复跑）· 零代码改动**。
  - **复跑与 19:12 那批逐项对齐，两句都成立**（指纹一字不差 **且** 读数一致）：
    `deps 67a1287e29df29c3`、`bin afbc9a1b8fa9b74f`/`c6a53803aa7ce5c1`/`38a594aa65efbca8`（与 18:35、18:54、19:12 四代同值）；
    全量 **`exit=0`、498 PASS / 7 SKIP / 0 FAIL、17 包全 ok、`build failed` 0**（起跑 19:43:15、构建时刻 19:43:18、收 19:43:28）；
    六项 7/8/2/8/2/3 = **30 条，非绿 0**；行级带 `LUCERO_REQ_DB2=1` **4 条全 PASS**；
    两条反例照旧真红（摘建行腿 cmd/core 3 红 + 我包 **5 红**；摘改行腿 `…ThreeLegsEachMoveTheRow` 红 + 我包 **4 红**），
    `restored_identical=True`。⇒ **19:12 那批不是靠并发凑出来的**，主控要的"无并发补一份"已交。
  - **"空机"两证按当下盘量，不照抄谁的基线数**：`tasklist` 在 19:42:33 / 19:43:42 / 19:44:14 / 19:44:35 / 19:45:36 / 19:46:02
    **六次探针都没有 `go.exe`、`test.exe`**；`.scratch/test/` 最新项在**我 harness 起跑前**（19:43:15）是
    `selftest_recheck.log` **19:28:18**（静默约 15 分钟）⇒ **主批次确实是空机**。
    **但有两件必须如实写，不许把复跑说成全程空机**：
    ① test 在 **19:44:36** 开始写 `oc_now.log`（11450B，19:45:04 末次）⇒ 我 **19:44:14~19:44:35 那次 legs 重跑落在它恢复活动的边界上**，
    所以我**又跑了一腿 19:45:36→19:46:02**，那一段的前后探针里 `oc_now.log` 的 mtime **停在 19:45:04 不动**、tasklist 仍无 go.exe
    ⇒ **报告里用的 legs 读数是这一腿（干净的）**，19:44 那一腿只当重复确认；
    ② 19:43:42 我一度得到"` .scratch/test/` 不存在"的读数——**那是假的**：那条命令的 cwd 在 `core/`，
    我用的是相对路径 ⇒ **会话 cwd ≠ 仓库根造出的经典假否定**。改用绝对路径后目录一直在，19:44:36 起还在被写。
    **如果我没回头核，我会把"test 把自己的目录删了"当成一条空机证据报上去。**
  - **③ 的判据我按主控的口径实现，不写成"HEAD 等于某个值"**：HEAD 现读 **`c46339c`**（19:42:00）——**不是**主控那份里说的 `6b28868`
    （它是父提交，19:38:34，两笔都只动 specs 与台账）。三条证据：`git status --porcelain -- core/` = **空**；
    三面指纹与 19:12 一字不差；**再加一条更硬的**：`git diff --name-only f25ee6c..HEAD -- core/` = **0 个文件**
    ⇒ 从被验那批（`f25ee6c`）到现在，**Go 面一个字没变**，HEAD 前进的两笔全是 specs/台账。
  - **⑤ `:sync` 的 503 半格：登记、今天不做、不阻塞任何人**。按契约① 第 22 条原文落账：
    发射时机 = **只在降级发生的那一刻**（握手 / probe 核定出 effective < requested 那一处，在我 `internal/adapter` 的核定域里）；
    **`:sync` 的 503 出口只回码、不发事件、不 Touch 任何聚合**——`backend` 现在的形状是**正确终态**，不许为它开"半个事件"的口子；
    响应里要说明"为什么降级"走**读**已落库的 capability/health 行，不在响应路径上写（一次轮询刷一条 journal = C36① 同一条理由）。
    **今天盘上事实**：`adapter_downgraded` 只有 `eventjournal/events.go:14`（常量）与 `:77`（catalog 登记）两处非测试命中
    ⇒ **有名字、无发射点**，这条我按 §17 那把尺标成"未实现"，**不许**有人拿常量在位当"已接线"。
    解锁方式：Wave-2 闸门之后主控点名发射；点名时我要的判据是"降级那一刻发一条、`effective == requested` 时**不发**"两侧都有用例。

- **第 24 轮（09-25 20:06~20:09，T-310）· 零交付改动，量"连接面吃不吃 ctx"**。
  形状：**临时探针 + 一条可复跑命令**（`.scratch/adapter/ctx-probe.sh`：把 `ctxprobe_test.go.txt` 复制成
  `core/internal/process/zz_ctxprobe_test.go` → 跑 → `trap` 删除；跑完 `ls | grep -c ctxprobe` = **0**，包内不留件）
  —— 本封明写"零代码改动、只要事实"，所以探针不进交付物，但留在 `.scratch` 里可复跑。
  - **主控 ① 的前提有一处要纠正**：`Connect` **不等 hello**（它只 `Start` + 组装 + `go conn.read()` 就返回，
    实测 2.25ms）；等 hello 是在 `Initialize`→`request` 里。⇒ 三段实际是"起子进程 / Connect 本体 / 等回应"。
  - **量的结果（表在下条，含耗时与"孩子去向"）三句话**：
    ① **`Start(ctx)` 全文不读 ctx**——传一个**已经取消**的 ctx 进去，它照样花 **588ms** 真起了一个孩子、返回 `err=nil`、
    孩子**存活**；② `request` 那一腿**确实吃 ctx**（400ms 期限 → 400.31ms 返回，err 里带 `sent $/cancel`，
    `pending` 归 0 不泄漏）；③ **三条路径没有一条会收走孩子**（cancel、判死、期限都只处理连接，`Child` 根本没有 `Kill`）。
  - **一个我没预料到的分支**：`Initialize` 对"永远只回 `pong`、从不成帧"的孩子，**4.9ms 就返回**了，
    走的是 **`c.dead`（连接判死）那一腿、不是 ctx**，err = `adapter process is gone; in-flight state is unknown …
    (ADAPTER_UNRESPONSIVE)` ⇒ **"很快就返回"不等于"ctx 生效"**，这一格如果只看耗时会误判成安全。
  - **④ 空机复跑没欠着**：T-302 那封要的"同一条命令 + 无并发"我已经交过两次 ——
    **19:43:15→19:43:28** 与 **19:52:50→19:53:02**（后者起跑前后 `ins_completed.py` mtime 停在 19:48:27 未变、
    `tasklist` 两次无 `go.exe`），两批与带载那批（19:12）**数字全同**：`exit=0`、498 PASS / 7 SKIP / 0 FAIL、17 包全 ok、
    三面指纹一字未变。**20:06 之后不再取第四份**：`git status --porcelain -- core/` 现读 **7 条**
    （backend 在写 notification/`api/http`，最新 mtime 20:0x）⇒ 现在跑就是拿正在被改的树做合验，
    正是我自己判据该拦的那件事（`idle_probe()` 这次确实拦住了我）。**要第四份，等 `core/` 干净，一条命令即可。**

## Remaining

- FakeAdapter 剩余 **6 个 C→A 方法**（14:2x `grep -c 'case "<method>"'` 量的，见 Verification）：
  `session/list`、`session/read`、`session/stop`、`model/list`、`approval/list`、`approval/respond`。
  §10 的 **4 种注入已全部落地并有真进程用例**（crash/fail/hang/unknown_quota）；§14 的"全 11 方法"
  现在到 4 个 C→A 方法 + `stream/*`（`turn/claimed`+`turn/dispatched`+`stream/delta`+`turn/finished`）。
  **R-004 出口条件仍不成立**，缺的就是上面那 6 个
- Supervisor 到 DB 的那段：`process_instances` 登记、§14 三列（`state`/`last_error`/`shutdown_reason`）
  与 §8 的 `adapter_restart_exhausted`/Notification —— `internal/process` 只发事件不落库，
  接线方仍未点名（CO-20）
  **【09-24 07:18 划掉一半】** "`internal/process` 只发事件不落库" 与 CO-20 那半已闭：
  写口 `LifecycleStore`（lifecycle.go）今天落库、CO-20 由 T-211/第 20 条点名（写口=adapter、装配=backend）。
  **仍未点名的是 §8 的 `adapter_restart_exhausted`/Notification 那一腿**，且 §14 的 `state`/`last_error`/`shutdown_reason`
  三列**不在 `process_instances` 上**（000001 那表只有 `status`/`exit_code`/`metadata_json`）——谁再提这三列，
  要先说清它落在哪张表。
- `$/cancel` 与 deadline 的**调用侧**：帧构造（`NewCancel`）与 `deadline_at` 编解码已就绪，
  缺的是持有 pending 请求表的 pump goroutine —— 它必须与进程同生命周期，故归第 3 轮 `internal/process`
  （§9.2 第 5 条"不裸启动无限 goroutine"）
- Supervisor：拉起 / 登记 `process_instances` / stdout-stderr 分流 / 退避 `1s,2s,4s,8s,≤30s` /
  连续 5 次→`UNAVAILABLE`+停重启+Notification / 每次重启后 `Cursor.Reset`
- §8 崩溃回归（包内版：Core 存活 + 该 in-flight **Execution 状态值**转 UNKNOWN + 其它 Adapter 不受影响）
- FakeAdapter（第 4 轮，落点 `core/cmd/fake-adapter/`）：§4 全 15 个 C→A 方法 + 4 种故障注入
  —— **本轮不再自加 `debug/delivery_semantics`/`task/accept`**：§10 的注入表就四条，多造方法等于
  改契约。"注入一个假阳性投递"这条需求改由 `debug/fail` 的码面 + `SendResult.Validate` 承担，
  真正的"同步成功但未入队"注入面记 REQ-T7 等主控裁
- §9 白名单接入 Supervisor 启动路径：注入面已裁（CO-3，`func(args ...string) []string`），待落

## Blockers

- **B-a1 已解除**（CO-1 裁定，commit `cea05b7` 落的契约 §10 文本）：FakeAdapter 住
  `core/cmd/fake-adapter/`（真子进程，`adapters/fake/` 只放 fixture）；第二个 module 与 `go.work`
  一并否掉。**注意**：主控消息里那次"方案②、`adapters/fake/` 起独立 go.mod + go.work"的表述
  与契约 §10 相反，我以契约文本为准（`internal/` 规则那条理由就是我提的）。
- **B-a2 已解除**：`core/internal/eventjournal/events.go:13-14,67` 现有
  `AdapterDowngraded` / `AdapterRestartExhausted` 两行常量，第 3 轮发事件不再撞未登记。
  登记方式已裁为 §11 双事件（audit 写 `audit_entries` 事件行 + adapter 写 `event_journal`）。
- **B-a7（新，需要主控处置）我的独占路径被别的会话并发改写**：本轮 `core/internal/adapter/handshake.go`
  里我加的 `Call` initialize 豁免被回退过一次（`Call` 回到"未握手一律拒答"的原样），
  而 `core/cmd/fake-adapter/main.go` 里我删掉的本地副本没被回退 —— 两边一撞正好合成 T-24 报的死锁，
  症状完全对得上；同一文件我三次编辑里两次提示"file changed since your last read / 已被回退"。
  我已重新落库级修复并用四包重跑确认。请确认还有谁在写 `core/internal/adapter/**` 与
  `core/cmd/fake-adapter/**`（按契约③ §7 两处都归 adapter），否则这类"修好又被打回"会反复出现。
- **B-a4 部分解决（09-20 12:0x 复测，HEAD `0367aa5`）**：契约② 现在 **264 行**，多出的是
  **`## 15. 错误码集合`（212 行起，REQ-T-3 补）** —— 也就是我一路在报的"§1/§3 引用了一节却从未写出来"，
  这一节终于存在了。但它与消息里那张表不是一回事，差集如下（我已按 §15 对齐能对齐的部分）：
  - §15.1 把符号钉在 **`error.data.code`**（大写蛇形），`error.code` 仍是 JSON-RPC 的数字 ——
    我已实现：`NewErrorReply` 同时发两者，`Error.Symbol()` 优先读对端给的 `data.code`
    （对端可能用一个 Core 没被给过号码的 §15 码），并在 `TestErrorCarriesSymbolInData` 钉住。
  - §15.1 的符号名是 **`INTERNAL`**，不是 `INTERNAL_ERROR` → 已改。
  - §15.1 **没有任何数值**：我用的数字仍只存在于消息里（`-32600..-32603`、`-32001..-32006`、
    `-32050..-32099` 保留），仍要落进 §15 才能被别的角色引用（CO-4 未闭）。
  - §15.1 缺我用到的 `INVALID_PARAMS`、`METHOD_NOT_FOUND`，也缺消息里点过名的
    `READ_ONLY`、`ADAPTER_UNRESPONSIVE`、`EXECUTION_STATE_UNKNOWN`；同时 §15.1 新增了 8 个我还没有调用点的码
    （`SESSION_NOT_FOUND`/`AUTH_REQUIRED`/`RATE_LIMITED`/`DANGEROUS_FLAG_REFUSED`/`CANCELLED`/`TIMEOUT`/`PROVIDER_ERROR`
    加已有的若干）—— 我**没有**把它们一次性声明成死常量，各自等有调用点的那轮（pump 的 `$/cancel`→`CANCELLED`、
    deadline/`debug/hang`→`TIMEOUT`、§9 白名单→`DANGEROUS_FLAG_REFUSED`）。
  - §15 末尾要求"新增码必须同时改 §15.1 与 §15.2，`test` 可 grep 两表条目数是否相等"——
    两表当前都是 14 行 + HTTP 专有一行，`test` 若按条目数校验请注意这一点。
  - 其余条款仍未落盘（当场计数全为 0）：`### 4.1`、`### 4.2`、`### 6.1`、`### 6.2`、`### 6.3`、
    `### 7.1`、`### 7.2`、`### 12.11`、`turn/claimed`、`checked_at`、`client_request_id`、`queue_strategy`。
    注意 `db` 与主控引用的"`§6` 表那两行 / §6.4 在 135~143 行"在 `0367aa5` 上不存在——
    §6 仍是原来的四行表。可能又被回退或在另一份工作副本上。
- **B-a3 仍未解除**："`stream/delta` 不进 event_journal"的**跨库**断言要等 db/backend 的
  chunk 写路径。包内已有结构约束（`purity_test.go`：零跨包 import + 禁 `database/sql`/`os/exec`），
  但那只证明"这一层写不进去"，不等于"链路上没人替它写"。
- **B-a4（新，需主控处理，不阻塞我）**：多次裁定的条款**没落进契约文件**。实测：
  `git cat-file -t 117725f / 1d71535 / 9463c30` 全部 `Not a valid object name`；
  `git worktree list` 只有 `D:/Project/Lucero`；HEAD 在 `4f6c3e1`；
  `grep -n "32600\|checked_at\|client_request_id\|queue_strategy" specs/adapter-protocol/adapter-protocol-v0.1.md`
  **零命中**，文件仍是 215 行、章节只到 §1~§14（无 §4.1/§4.2/§6.1/§6.2/§6.3/§6.4/§7.1/§7.2）。
  后果不是好看与否：`test` 角色的规矩是"引用不存在的契约条款即作废"，而我这一轮的断言全部引这些编号；
  别的角色只读 `specs/` 就会按旧形状实现。我按消息里逐字贴出的值实现（数字明确、集中在一个 const 块，
  改回成本一处），并在 `protocol.go` 包注释与 `internal/adapter/README.md` 里都标了"已裁待落盘"。
  **请主控补落盘或更正**；若某个数字你后来改了，我这边只有 `protocol.go` 的 const 块
  + `TestErrorCodeTable` 两处要动。
  **按 `test` 角色的规矩 3（"查不到的条款即作废"）复述一遍后果**：我现有 34 个测试里，凡引用
  §1/§4.1/§4.2/§5.1/§6.1/§6.3/§6.4/§7.1/§7.2 的断言，在契约文件补上这些条款之前都属于
  "引用查不到的条款"，正式验收时应视为未生效；能立刻生效的是引用 §1~§14 原文与 §11 的那批
  （§11 的两行事件常量我核过在 `core/internal/eventjournal/events.go:13-14,67`）。
  落盘之后我不需要改代码，只需重跑一次并把结果按规矩 5 重测给你。
- **B-a5（新）**：A-11 派的是"Wave-3 第 5 轮 `adapters list` / `adapter/health` 真子进程"，
  并写明"你第 4 轮（Adapters CLI + FakeAdapter 真子进程）已完成"。**实况**：我刚交完第 2 轮，
  第 3 轮 Supervisor（`internal/process`）未开始，FakeAdapter 一个方法都还没写。
  A-11 依赖真子进程与 Supervisor 在线，现在做只能拿假链路验证。等主控确认顺序，我不跳步。

## Coordination



- **CO-54（09-27 16:30，T-382 ① 的岔口，请裁"一个 `start_now` 轮次何时算跑完"）**：② 我照办了（字面 + 不发帧），
  ① 照字做完会把 §5/CO-25/§6 规则 5 三条现裁同时撞掉（实测 5 条红，含队列永久搁浅），所以已回退、不交半成品。
  三条可选形状与代价写在验证「第 29 轮」正文：**(甲)** 即时收尾 + 修 CO-25 的排干点；**(乙)** §4 加一个
  "正常结束"的调用（协议变更）；**(丙)** 一个 `debug/hold` 注入（需要 T-275"不新增 debug/*"的豁免）——
  **我倾向 (丙)**，只有它同时保住"两轮同时在飞"这个 §6 规则 5 的验收形状。谁解锁：主控。
  裁之前 ③Ⅰ 交不了，我不会拿"队列那一路也发 SUCCEEDED"冒充它。**已按 ④ 请主控转告 backend**：
  端到端那一腿（挂住留 `RUNNING`+503、崩溃不许写成功）现在可以复跑了，两包绿、且 n4 证到跨包有牙。

- **CO-54 · 已按裁定（T-386 丙）落地（09-27 17:10）**：`debug/hold` 存在于 fake-adapter、
  `start_now`/`guide` 默认在输出结束时发 `turn/finished{SUCCEEDED}`，解除才是被按住那一轮的收尾点。
  变异 4 FIRED / 1 有原因的 SILENT；两包 74 PASS 基线、A 面 121 PASS/0 FAIL。⇒ 从"等裁"转"等入册 + 等 §10 补一行"。
- **CO-55（09-27 17:10，h4 的 SILENT 查出来的，请裁要不要立判据）**：§9 说 `debug/*` 只对
  `adapter_key=fake` 且 `EXPERIMENTAL` 合法，但今天**没有判据**区分"声明了却没资格 / 没声明却被调用"：
  `Handshake.Call`（`internal/adapter/handshake.go:342-369`）只查 §2 未握手、§2.2 重复协商、§3 `write_entry`
  三件事，**不查能力表**（查表的是 `Handshake.Delivery`），所以我把 `debugCapability("debug.hold")` 删掉后
  两包 74 条全绿（变异 h4 = SILENT）。⇒ 声明的作用目前是"对外诚实"，不是强制。
  要不要把它做成判据、放在哪一层（fake 侧自查 / Core 侧调用前查 / `core/test` 对拍声明与可达方法），
  **请主控裁**；我不擅自往 `Handshake.Call` 加门——那会改变所有能力的调用语义，超出 T-386 授权。
- **CO-51 · 已按裁定办
  （212B、sha 前缀 `7364ae9e5ca630a8`），语义一致 ⇒ `internal/process` 那枚**已改名导出为 `ChildAlreadyGone`**
  作为单一来源；**`cmd/core/processpipe.go:228` 那份请 backend 下次动该文件时改成调用包内这一枚**
  （T-361⑤ 已派，我不去动）。两条判据已重证：m6a（改名后的 m5）与 m6b/m6c 全 FIRED。
  ⇒ 这一格从"等裁"转"等 backend 的一次改调用"，**我这边没有欠账**。
- **CO-53（09-27 13:06 量出来的，零代码改动）：连接面对"不带期限的 ctx"不检测、不自救 ⇒ 请把它写成契约里的调用方义务**。
  实测（探针 7，`.scratch/adapter/co53-probe7-130609.log`，`--- PASS`，跑窗 13:06:09→13:06:18）：
  孩子不再读 stdin 时，`request(context.Background(), 256KB)` 这一发 ——
  **A 5s 内未返回、`c.dead` 仍=false、`pending` 残留 1、`writeMu` 被永久占住**（也就是"没期限就没有任何自救"）；
  **B 排在它后面、自己带 400ms 期限的那一条仍在 400ms 返回**（第二批的 `abandon` 生效，楔子不扩散给有期限的一侧）；
  **D 唯一能解开它的是 `Close`：实测 2ms**，随后那条无期限的写在 5.551s 带着
  `adapter process is gone…` 回来、goroutine 排空、孩子被收（E：存活=否）。
  ⇒ 结论按字：这不是"我包里少写了一个 watchdog"，而是**⑤ 明令不许新增第三种期限**的必然推论；
  期限必须在调用方，所以它需要一句契约，而不是我擅自补一个常量。
  **拟稿（我的笔不碰 `specs/**`，请主控落）**：接在契约① §1.1 第 23 条不变量那段之后 ——
  「**凡走 `process.Connection`（`Connect`/`Initialize`/`Call`/`CallTurn`）的调用必须自带期限**；
  连接面对无期限的调用**不检测、不自救**（实测：写堵死时 `c.dead` 不会关、`pending` 会残留，只有 `Close` 能解开）。
  因此新加一条走连接面的腿时，兜底期限是**这条腿的责任**，不许依赖包内超时。」
  **顺带把 §23 里那句"后两条今天是否同形，由当事人自己量了回报"一次答清**（三条腿我逐条读了码，13:0x 现量）：
  - **send** = `cmd/core/sendpipe.go:66-69`：`ctx` 无期限时兜 `probeAttemptDeadline` ⇒ **有兜底，但用的是 probe 的 10s 常量**
    （§23 自己已经标了"send 要自己的期限、且期限只是止痛"，这一格仍归 backend）；
  - **discover** = `cmd/core/discoverassembly.go:78-81`：**与 send 同形**（同一个 `probeAttemptDeadline` 兜底），
    而且它 :74-75 的注释已经把这条 invariant 写在码上了（"连接这条路必须有期限…没期限就是挂住的 Adapter 拖住一条 handler 线程"）；
  - **probe** = `internal/adapter/probe.go`：**不同形、也不需要走连接面** —— 它用 `Launcher` 给的
    `io.ReadWriteCloser`（:86），且 `exchange`（:277-286）**自己兜底**：调用方无期限时用 `defaultProbeTimeout`，
    有期限则取剩余时间、且把"已过期"钉成 1ns 而不是无界。⇒ **probe 这条腿不依赖调用方，也不受 CO-53 这一格影响**。
  **我这轮零改动**：没碰 `cmd/core`、`api/http`、`internal/execution`（backend 在途）、没碰 `specs/**`；
  探针件跑完 `zz_*` 残留 = 0、`grep -rn "ZMUT" core/` = 0。A 面读数不受影响（下一节复量）。

- **CO-52（09-25 21:0x，T-314 ②(b) 量出来的，请裁要不要界住；我不擅自改形状）**：
  `Connection.request` 的 ctx 分支在期限到期后要发一句 `c.Cancel(id)`，而
  **`Cancel` 的签名里没有 ctx**（`connection.go:212`）⇒ 那是 `request` 里**第二道不可取消的步骤**。
  实测：孩子不再读 stdin、且另有一次 write 正卡在管道里时，`Cancel` **在 3s 观察窗内没有返回**
  （探针 b2；对照组"管道未满"是 0s、`err=nil`）。
  ⇒ REQ-S-1 那条"别的写必须在一个明确上界内恢复"如果只界住 `Connect`/`Call`，这一格仍会从 `send` 那条腿漏出去。
  **三个可选形状**（都超出 ⑤ 的授权范围，所以我一个都没做，只在 `Cancel` 的注释里点名）：
  ① 给 `Cancel` 收 ctx；② `request` 的 ctx 分支把 cancel 丢成"发了就算"的一次有界写（带 deadline 的 write）；
  ③ 不改 `Cancel`，改成"`request` 到期即返回、cancel 交给写泵串行投"。
  **我倾向 ③**（它不新增期限、也不让调用方等第二次），但这是 §23/§1 的形状决策、且会碰 `internal/adapter` 的写面，
  **谁解锁：主控。裁定前我不动它。**
  **【21:1x 主控已裁（T-326 ③，契约① §1.1.23 第 ④ 处）】**：`write` 要放进**可放弃的 goroutine**，
  `select { 写完 / ctx.Done() / c.dead }`，超时即**标死连接、交给 `Close`**（不新增期限种类、不加第二种 Kill）。
  **顺序：先把 `Stop` 接线这一批交账，再补 `write` 那一格；两批判据不同，不许合成一批。**
  ⇒ CO-52 从"等裁"转"等我自己实现"，我那三个可选形状作废（裁的是形状 ② 的加强版）。

- **CO-52 · 第二批已实现（21:39，等复跑）**：按裁定形状落了 `writeWithin`（可放弃 goroutine +
  `select { 写完 / ctx.Done() / c.dead }`）+ `abandon` 只标死、回收仍归 `Close`。
  CO-52 那句"第二道不可取消步骤"由 `Cancel` 拒绝死连接一并关掉，实测从"3s 窗内未返回"变成快拒（<1s）。
  两批判据分开：第一批三条 `TestClose*`、第二批三条，各自一套变异批（第二批 **5 FIRED / 0 SILENT**）。
  **等主控点名复跑/入册**（谁解锁：主控）。附带一条**只上交不自裁**的引用勘证：验证 ② 与 ③ ——
  `ctxprobe_out.txt` 不是我脚本的产物、`§9.2 规则 5` 的限定词是"无限"（`processpipe.go` 的注释丢了它）。
  我不改 `cmd/core`，要不要回改那句注释归 backend。
- **CO-51（09-25 20:56，T-314 ⑤ 落地时量出来的，请裁哪一份成唯一来源）**："孩子已经不在了"这条规则现在有两份：
- **CO-51（09-25 20:56，T-314 ⑤ 落地时量出来的，请裁哪一份成唯一来源）**："孩子已经不在了"这条规则现在有两份：
  `cmd/core/processpipe.go:228` 的 `childAlreadyGone`（backend 的在制品，我没动）与
  `core/internal/process/supervisor.go` 里我新加的同名私有副本。两份是同一条判断
  （`errors.Is(err, os.ErrProcessDone)` ∨ `os.FindProcess(pid)` 失败），因为 `Stop` 的 `Kill` 对一个
  已被 `cmd.Wait` 释放句柄的孩子返回的是 `kill: invalid argument`（Windows 实测），不吞它就会把一次
  **正常收尾报成失败**。
  **我的建议：唯一来源放 `internal/process`**（`Stop` 就在这个包里，"它能不能收口"的知识属于它），
  backend 下一次动 `processpipe.go` 时改成调用包内那一枚。反面选项是"两份并存 + 跨包对拍判据",
  那正是 09-25 裁过要避免的形状（词表两处各存那一次）。谁解锁：主控。
  **裁定前我这枚已经能用，不阻塞 REQ-S-1 的任何一半**（⑤ 那一半我已闭，`sendPipe` 的两段式仍是 backend 的）。
- **CO-50（第 22~23 轮）· 已闭**：① 判闭；`:sync` 的 503 半格按 §22 只登记不做，发射点等 Wave-2 闸门后点名。

- **T-9 的"新红点"当场不复现（附命令，本机 18:01~18:02）**：投递说"`ErrorCode` 收紧成具体类型后
  `internal/adapter` 与 `core/test` 的调用/测试没跟改（db 全量跑时暴露）"。实测：
  ```
  $ cd /d/Project/Lucero/core && CGO_ENABLED=0 go build ./...          # 无输出，通过
  $ CGO_ENABLED=0 go vet ./test/...                                    # 无输出，编译干净
  $ CGO_ENABLED=0 go test -count=1 ./test/...
  ok  	github.com/SadCat18/lucero/core/test	2.495s
  $ CGO_ENABLED=0 go test -count=1 ./internal/adapter/...             # 18:01:16
  ok  	github.com/SadCat18/lucero/core/internal/adapter	0.959s
  $ grep -rln "internal/adapter" --include=*.go . | grep -v "^./internal/adapter/"
  ./test/adapter_contract_test.go                                     # 全模块只有这一个外部消费者
  ```
  `test/adapter_contract_test.go:76-84` 的 `codeOf` 已按整数线格式写（注释里点名 CO-4），
  `TestErrorCodeIsJSONRPCNumber` 只钉"线上是数字且等于 Go 常量"、**不写死具体数值**
  （它自己的注释还记录了原因："adapter 十几分钟内换过一次映射"）——所以
  "请通过 Coordination 让 test 确认期望值"这一条不需要我做，他们没写死期望值。我不需要改任何文件。
- **但 `core/test` 里有一处真被 B-a4 卡住，值得主控看**：
  `test/adapter_contract_test.go` 的 `TestWriteEntryOpenQuestions` 现在是 `t.Skip`，理由原文：
  「blocked: 等主控裁定 Q1/Q2；同时需要把被引用的 §12.11 回写进 specs，否则任何角色都无法自查自己是否合规」。
  也就是说 **test 角色独立撞到了同一条 B-a4**（比我单方声明更强的证据）。裁定其实已下达：
  审批类走 Permission 通道、写方法集 = `session/{send,stop,create}` + `model/select` + `debug/*`，
  我的 `writeMethods` 与此逐字一致（`internal/adapter/handshake.go`）。§12.11 落盘后他们把 skip 撤掉即可，
  期望值与我的实现不会打架。我不碰 `core/test/**`（归 test）。
- **CO-1/CO-2 的执行状态更正（免得账本留错）**：CO-1 的 §10 定死文本我已核实在 `specs/`（`git show cea05b7`）；
  CO-2 的两行事件常量确实由 db 补上了（`core/internal/eventjournal/events.go:13-14,67`），我没有、也不会去改它。
  另核：`core/cmd/` 仍不存在（18:01 `ls cmd` → No such file），所以 FakeAdapter 与 `cmd/core` 都还没人落地，
  我第 4 轮的落点仍是空目录，顺序不变（先 Supervisor）。

**已裁并落地**（无需再议）：CO-1 FakeAdapter 落点（§10）、CO-2 双事件常量、CO-3 注入
`func(args ...string) []string`（`internal/adapter` 保持零跨包 import，守卫已加进 `purity_test.go`）、
CO-4 整数码、CO-6 点分/斜杠两套拼法（确认有意，映射在 §4）、CO-7 验收命令口径、
CO-8 `min(core,peer)`、CO-9 两条假设、§12.11 写方法清单。

- **CO-4 收尾 → 主控**（§1 表已按 A-2 落地，只剩三件事）：
  ① 我用了 `-32600..-32603`、`-32001..-32004`（`ADAPTER_UNRESPONSIVE`）与 JSON-RPC 预定义段错开的号；
  `TestErrorCodeTable` 现钉的表就是你贴的那份，外加我占在"留待后续"段里的两个**未裁号**：
  `PROTOCOL_VERSION_MISMATCH = -32005`、`LOCAL_DATA_UNAVAILABLE = -32006` —— 请补进 §1 或改号
  （改号只动 `protocol.go` 的 const 块与 `TestErrorCodeTable` 两处）。
  ② `READ_ONLY`（§12.11）我这边**没有调用点**，因此不声明、不占号：§3 的
  `write_entry:false` 门与 §12.11 的"只读型伪装"从同一道门进来，我按 §3 回 `WRITE_ENTRY_NONE`。
  请指定：哪些情形回 `READ_ONLY`、哪些回 `WRITE_ENTRY_NONE`（否则 FakeAdapter 第 4 轮只能猜）。
  ③ 已断言 Core 侧任何码不得落进 `-32050..-32099`（§1 保留段）。
  顺带一个事实供你决定：`-32601/-32602/-32603` 在 JSON-RPC 预定义里分别已是
  method not found / invalid params / internal error，而 §1 把这三个数字给了
  `WRITE_ENTRY_NONE / CAPABILITY_UNAVAILABLE / DELIVERY_MODE_UNSUPPORTED`；我把
  `METHOD_NOT_FOUND/INVALID_PARAMS/INTERNAL_ERROR` 放到 `-32001..-32003` 正是为了避开冲突。
  只有我们的 Adapter 说这套协议，所以不互通性成本为零；但若以后要挂第三方 JSON-RPC 实现，
  §1 的这三个会打架。
- **CO-5 → 主控**（仍未裁，第 1 轮就提过）：`confidence` 的取值集合没有任何契约冻结
  （§3/§7 只出现过 `HIGH` 与 `UNKNOWN`）。我按 `HIGH|MEDIUM|LOW|UNKNOWN` 校验，
  并且**把它同时用作 §4.1 health 与 §6 stream/state 的枚举**——三处共用一套。
  若你另设集合（比如加 `PROVIDER_REPORTED`），三处一起改，改点在各文件顶的 `confidences` 表。
- **REQ-T6 已被 `test` 改掉并复测**（09-20 12:57:21 四包同跑 `ok core/test 3.153s`）。原记录留此备查：
  `core/test/**` 归 `test`，我未改动他们的文件。
  `test/adapter_contract_test.go:223` 的
  `if strings.Contains(string(raw), ` + '`"code":"`' + `)`
  现在会误报：§15.1 要求 Adapter 层符号放在 `error.data.code`（大写蛇形字符串），
  所以我按 §15 发出的回包里出现了第二个 ` "code":" `。当场原文：
  ```
  {"jsonrpc":"2.0","id":"1","error":{"code":-32600,"message":"not handshaken",
   "data":{"code":"PROTOCOL_NOT_INITIALIZED"}},"request_id":"019e..."}
  --- FAIL: TestErrorCodeIsJSONRPCNumber
  ```
  顶层 `error.code` 明明是数字 `-32600`。建议直接删掉那行 substring 判断：
  它下面的 `probe.Error.Code json.Number` + `.Int64()` 已经是更强、且不会误伤的写法
  （它只解 `error.code` 那一个字段）。除这条之外 core/test 是 21 PASS / 0 其他 FAIL。
  我没有为了让这条旧断言变绿而撤回 §15.1 的形状。
- **CO-21 → 主控**（契约缺口，影响游标粒度）：`stream/delta` 的载荷里没有 `execution_id`（§6 的样例只有
  `{sequence_no, channel, text, provider_raw}`），而 CO-6 把续传游标钉成 `(execution_id, sequence_no)` 二元组。
  现在 pump 只能按"每个 Adapter 进程一个游标"实现（§6.2 字面如此），配对由落 chunk 表的人补。
  要么给四条通知都加 `execution_id`，要么把游标语义改成 `(adapter_instance, sequence_no)` —— 我按你的选择改，
  两种都不动协议帧形状之外的代码。
- **CO-22 → 主控**（要用 §15 的哪个码）：进程在请求在飞时死掉，Core 侧现在回
  `INTERNAL("connection lost: ...")`。§15.1 里没有专门的码，而语义上这是"状态未知"而不是"我们内部错了"。
  是不是该用 §11 提过的 `EXECUTION_STATE_UNKNOWN`？给了号我就换。
- **CO-23 → 主控**（形状确认）：我把"握手完成前到达的 `stream/*`"按 §2 违规上报（不动游标、不转发）。
  契约只写了"未握手的任何方法一律拒答"，入站方向没说；另一种合理做法是直接断开该连接。
  现选择上报而非断开，`TestConnectionStreamBeforeInitializeIsRefused` 钉住。
- **CO-19 → 主控**（一处未写明的退避细节）：§8 的 `1s,2s,4s,8s,上限30s` 没说 8s 之后是先 16s 还是直接 30s。
  我实现成"继续翻倍、超过即封顶"，即 `attempt5=16s, attempt>=6=30s`，并在 `TestBackoffFor` 里钉住。
  你要的是后者的话，改 `BackoffFor` 一行 + 表里两行期望。
- **Supervisor 的接缝形状先确认一下（省返工）**：这轮 `internal/process` 刻意**不 import**
  `platform/db`/`eventjournal`/`audit`，只把 `STARTED/EXITED/CRASHED/UNAVAILABLE` 事件从 `Sink` 回调交出去。
  也就是说 §8 的"发 Notification + `adapter_restart_exhausted`"、§14 的 `state`/`last_error`/`shutdown_reason`
  三列写，都要由持库句柄的接线方（backend 的 app 层？还是我在 supervisor 里收注入的写函数？）来做。
  请点名归谁，我在下一轮接上；现在这样不会自己偷偷写库。
- **CO-17 → 主控**（契约缺口）：`quota_type` 的取值集合全仓库无冻结（`grep quota_type` 只有 §7 的
  `PERCENT` 一例）。我只做非空校验并留 `ponytail` 式余地；额度归 backend，若你要 Core 侧按类型
  分派显示，请给集合与单位语义（`PERCENT`/`CREDITS`/`REQUESTS`?）。
- **CO-11 → 主控**（契约缺口）：`stream/item` 的载荷形状 §6 没给（只说"一个完整条目就绪"）。
  我按"整帧 params 原样透传 + 必带 `sequence_no`"实现。§10 追加的 `debug/delivery_semantics`
  若也要走 item，请给字段。
- **CO-12 → 主控**（假设登记）：§4.1 只写了"不得用 UNKNOWN 冒充 HEALTHY、不得用 HEALTHY 冒充 DEGRADED"，
  但"冒充 DEGRADED"在 Core 侧无法检出（没有对照真值）。我实现成**可检出的那半**：
  确定态（`HEALTHY/DEGRADED/UNAVAILABLE`）配 `confidence=UNKNOWN` 即拒，`checked_at<=0` 即拒。
  要收窄或加严请示下。
- **CO-13 已裁并落地 → 无需再议**：不加字段、空串合法、Recovery 不得从空值反推"未运行"。
  我原本在 `turn/claimed`/`turn/dispatched` 上**硬拦空 `provider_execution_id`**（那会拒绝合法帧），
  已按裁定放开：现在必需的是 `execution_id`（回查寻址键）与 `dispatched` 的 `turn_id`（唯一开跑证据），
  `provider_execution_id` 允许为空，并各留一行用例钉住"空句柄仍然是证据/仍然是 CLAIMED"。
  `SendResult.ProviderExecutionID` 同样不强制非空。
- **§6.4 是通知不是错误码，已核**：`turn/claimed`/`turn/dispatched` 在我的实现里就是
  A→C 通知（`DecodeStream` 只接受 notification 形状，请求/响应形状直接拒），
  不占 §1 码表，与 A-2 的裁定一致。
- **CO-14 → 主控**（契约缺口）：§4.2 只裁了 `base_sha` **不匹配**时的分支，没裁
  **Provider 根本没有 commit 概念**（纯 SaaS 会话、或 Z Code 那种无 CLI 无本地库的形态）时怎么回。
  现在 `base_sha` 是可选，不传即不问；若你要显式声明"无从对齐"，给个字段我加。
- **CO-15 → 主控**（形状确认）：§4 用 HTTP 头式的 `Idempotency-Key` 写法，但 ndjson 帧没有头。
  我放在 `params.idempotency_key`（`SendParams`）。若你要它跟 `request_id` 平级做成帧字段，说一声，
  一处改完（含 `TestSendParamsValidate`）。
- **CO-16 → 主控**（契约缺口）：`stream/state` 的 `normalized_status` 取值集合没有任何契约冻结，
  而它最终要映射到 Execution 状态。我只做非空 + `confidence` 枚举校验，不去私设一套映射
  （私设 = 第二真相源）。§6.3 已裁"检测锚点不得依赖 `stream/state`"，所以这条不再阻塞我，
  但 `task_running` 的**展示**要看它，仍建议钉集合。
- **§6.1 覆盖面待确认**：§6.1 写"四条通知"必带 `sequence_no`，§6.4 又新增 `turn/claimed`、
  `turn/dispatched` 两条 → 现在实际是六条。我对六条一律要求 `sequence_no>=1`
  （`DecodeStream` 与 `NewStream` 双侧拦）。若你只想管原来那四条，我按你指定的名单放开。

- **CO-24 → 主控**（契约缺口，本轮新发现，已按最小方案绕开）：CO-8 要 `turn/claimed`/`turn/dispatched`
  带 `execution_id`，而那是 **Core 自己的 id** —— 可 §4 的 `session/send` 载荷表里没有任何一项能让 Core
  把它交给 Adapter，§5/§6 也没补。结果：证据对在协议上无法闭合，除非 Adapter 猜一个。
  我的处理是**不猜**：`SendParams` 加了可选的 `execution_id`（`send.go`），FakeAdapter 拿到才回显、
  拿不到就一帧证据都不发（于是 §6.3 正确地把它留在 UNKNOWN）。请把它写进 §4 的 send 载荷（必填还是
  选填请一并裁），并点名 Core 侧由谁生成（Execution id 还是 request_id 的派生）。
- **CO-25 → 主控**（契约缺口）：§5 的 `queue` 说"排队，等当前轮结束"，但 §4 没有任何方法让排队的那条
  **开始**，§6 也没定义"队列位置变了"的信号。真 Provider 自己排空，Core 只能被动收 `turn/claimed`；
  FakeAdapter 需要一个确定性排空点，我选的是"下一次 `session/send` 先闭合上一轮再排空队列"（无计时器，
  故用例不抖）。这不是契约里的规则，只是这个 fakes 的约定 —— 若 `test` 要写"排队后真的跑了"的用例，
  请先裁定排空点（候选：`session/stop` 之后、新增 `queue/flush` 注入、或 `stream/state` 带 `queue_position`）。
- **REQ-T7 → 主控**：我撤掉了上一版计划里的 `debug/delivery_semantics`（§10 注入表就四条，不自加方法）。
  若 §12 P0 的"同步成功却根本没入队"要有端到端用例，需要一个注入面；请裁定用哪条（REQ-T7 与 CO-25 同源）。
- **T-50 回执（三条不重做 + 一句已答）**：(a) 全表已交、(b) 三处越界已修、(c) `INVALID_PARAMS`/
  `METHOD_NOT_FOUND` 只挂标准码 —— 三条**确认不重做**，`protocol.go` 与 `codeTable` 一字未动。
  `ADAPTER_UNRESPONSIVE` 的归属我 13:29 那份报告已答（词汇由 `internal/adapter` 统一持有、`internal/process`
  只触发），并已写进 `protocol.go` 注释；本轮按 T-54 补的正是这一行：`ErrAdapterUnresponsive ErrorCode = -32014`
  在 `protocol.go:75`，**它一直有常量**。
- **T-52 回执**：变异验证的备份从今往后只落 `.scratch\adapter\<带内容的名字>.bak`；本轮全部照此执行
  （`main.go.orig`/`quota.go.orig`/`connection.go.orig`/`T54-protocol.go.bak`），`/tmp` 本轮零新增。
  这些 `.bak` 不是证据物，不建保留机制。
- **T-54 回执（三条当场量，命令与输出见 Verification evidence）**：
  ① `-32099` 在我这边**从来不是表里的一行**，只作 CO-4 的段界常量 `adapterCodeFirst` 与注释存在（grep 三处）；
  ② `-32006/-32007/-32010` 三条已在 `domainCodes` 里，`Err*` 常量数 **0**（乙类不许有符号），
  且 §15.3 规则 5 那条"CANCELLED 不进 error 响应"现在有正反两面用例：FakeAdapter 只发
  `result.cancelled=true`，测试在它以 error 出现时直接 `Fatalf`；
  ③ 契约 §15.1 的数字表（19 行）与我的 `codeTable`（19 行）**逐字 diff 相同**，
  连"是否有常量"那一列也 19/19 相同（命令与输出见 Verification）。
  **但 T-54② 的划分与盘上那张表不符，请主控看一眼**：你的原话是"甲类 4 个（有常量）、乙类 15 个（无）"，
  并把 `DANGEROUS_FLAG_REFUSED`、`EXECUTION_STATE_UNKNOWN` 点成"必须有 Go 常量"；
  盘上 §15.1 的常量列实测是 **9 个 ✓ / 10 个 ✗**，且这两行都写着 ✗
  （`| -32009 | DANGEROUS_FLAG_REFUSED | Core:internal/process | ✗ |`、
  `| -32015 | EXECUTION_STATE_UNKNOWN | Core:internal/process | ✗ |`）。
  我按**表**实现（这两处无常量，符合 T-45"由产生方在自己包里声明符号"的口径），
  **不改实现去凑描述**。如果你要的是"4/15 那份新划分"，那是改表的动作、且会连带 §15.1.1 的常量列，
  请回写后再投给我；三条新符号本身我这边一字未动，`Err*` 常量数为 0。

- **CO-26 → 主控**（契约内部自相矛盾，我不改实现去凑）：T-54② 的甲类名单把
  `DANGEROUS_FLAG_REFUSED`、`EXECUTION_STATE_UNKNOWN` 算成"Core 会发的、必须有 Go 常量"，
  而 §15.1 那张数字表的"是否有常量"列这两行都是 **✗**（同一列里 ✓ 共 9 个）。
  两张表在同一个文件里对同一件事给了相反答案。§15.3 规则 3 指定"数字表（含常量列）为唯一真相"，
  所以我按它实现：这两处**没有** `Err*` 常量。要改成 §15.2 那份划分，请改数字表的常量列并点名
  常量落在 `internal/adapter` 还是 `internal/process`（按我给 T-50 的答案，词汇归前者、触发在后者）。
  另注：`ADAPTER_UNRESPONSIVE` 不在 §15.2 的 17 行里，只在 §15.1/§15.1.1 —— 它**已经**有常量
  （`protocol.go:75`），T-54① 那句"Go 侧没有对应符号"是旧快照。
  **→ 14:4x 主控自己闭合**："甲类就是 9 个有常量的那 9 个，乙类就是 10 个无常量的那 10 个，
  19 行加总对得上，两张表没有矛盾，是我消息里写错数量"，并明确"`ADAPTER_UNRESPONSIVE` 归 §15.1 的甲类、
  有常量，我这条补充没把它列进 §15.2 的甲类名单"，**T-54 关闭**。CO-26 随此作废（不删，留作口径记录）。

- **CO-21 已裁并落地（§6.5）→ 撤**：四条输出通知带 `turn_ref`（= Core 发的 JSON-RPC `id` 的字符串形式），
  幂等键 `(turn_ref, sequence_no)`，`since_seq` 按票号分别记，未知票号丢弃 + Audit。
  **A 段交付时我在 `stream/delta` 上发的 `execution_id` 现在按 §6.5 撤掉了** —— 那是 Core 的内部身份，
  不该进 Provider 的日志/落盘。
- **CO-24 状态更新（没关闭，但紧迫性降了）**：`SendParams.ExecutionID` 仍然是 CO-8 两帧的回显来源
  （§4 的 `session/send` 载荷至今没有这一项，盘上 `grep -n execution_id specs/.../§4` 仍 0 命中）。
  §6.5 决议 ② 把"归属来源"改成 `Call` 侧显式传 `executionID`，所以**它不再是帧归属的前提**，
  只剩 CO-8 与 Recovery 还需要它。请一并裁：`turn/claimed` 上的 `execution_id` 是否降级为可选
  （若降级，§6.5 就把 Core 身份彻底挡在边界外了，我这边删两处、加两处）。
- **T-56 回执（只需记账）**：契约② `v0.1.1` / `content_sha256=4649c41e…` / `contract_commit=6c71d11`
  收到。我的引用口径是**只指存在的符号、绝不写行号**：`§6.5`、`§15.3 规则 1.1`、`§10`、`§9`、`§12.9`
  这类符号进注释与错误文案；等 backend 的 check 14 落地，我这边无需改码（本来就没有 `§6.7`、也没有行号）。
  顺带一条事实：本轮 §6.5 的编号在盘上经历过 `§6 第 5 条 → ### 6.7（15:03:45 落盘又丢）→ §6.5` 三次变动，
  **两次落盘失败是主控自己用 `git diff HEAD` 证实的**（该文件与 HEAD 逐字节相同），我没动过 `specs/**`。
- **B-a9（新增，非阻塞、但值得主控知道）契约② 在 15:0x 被回退过两次**：我 15:08:44 实测
  `### 6.7` 在 153 行、mtime 15:03:45；15:15:28 再测 mtime 退回 **14:46:17**、`grep -c '^### 6\.[67]'` = 0、
  `git status --porcelain` 对该文件**无输出**（工作树等于 HEAD），`git stash list` 空、
  其余 48 个 tracked 文件仍是脏的 —— 所以**不是整树 stash，是单路径回退**，落在 15:04:17(afbcb69) 与
  15:15:28 之间。`specs/**` 是主控独占，我只取证不动手；这条与 B-a7 同源，建议**同一轮 commit 掉**。
- **B-a10（09-21 第 5 轮 B 段已闭，闭账见 Coordination 末条）**（原为新增，非阻塞）§6.1 规则 1 改写后我差两帧**：新口径是"正常路径必发
  `turn/claimed`/`turn/dispatched`/`stream/state`，唯一强制收尾仍是 `turn/finished`，只有
  `stream/delta`/`stream/item` 允许不发"。FakeAdapter 现在发 claimed/dispatched/delta/finished、
  **不发 `stream/item` 也不发 `stream/state`** —— 按新 §6.1 这是一处不符，且 §6.5 的四条里
  `stream/item`/`stream/state` 还没在真进程里出现过（`CarriesTurnRef` 有它们、用例没有）。
  已排进第 5 轮 ② 的尾巴，`§10 ①` 的判据届时按新口径重测。

- **T-62 / C20 回执**：`turn_ref → execution_id` 住 `internal/process`、连接级 —— 与我实现一致。
  C20 已落死：`Inbound` 现在带 `ExecutionID`（解析好的归属），转发的 `Frame.TurnRef` **在边界处清空**
  （`connection.go` 的 `s.TurnRef = ""`），消费者拿得到 `execution_output_chunks` 需要的
  `(execution_id, sequence_no, channel, text)`，拿不到票号；未知票号那条帧仍然只回 `Err`、不推进任何游标。
  用例：`TestInboundNeverCarriesTheTicket`。**我把投递里那句"包外 0 命中"自己复跑了一遍，它不准**：
  `grep -rl "turn_ref\|TurnRef\|execByRef" --include=*.go core/ | grep -v _test` =
  `internal/adapter/{send,stream}.go`、`internal/process/connection.go`、**外加 `cmd/fake-adapter/main.go`（4 处）**。
  后者是**合法的**：FakeAdapter 是协议对端（Adapter 侧），它必须按 §6.5 回显票号，不是"Core 消费者按票号路由"。
- **CO-27 → 主控（写进契约前必须知道，否则 check 15 会误红）**：check 15 若按"`internal/adapter`
  与 `internal/process` 之外的生产 `.go` 零命中 `turn_ref|TurnRef|execByRef`"来判，
  **今天就会红在我自己的 `cmd/fake-adapter/main.go`**（4 处 `TurnRef` 赋值，那是 §6.5 要求的回显）。
  建议判据改成：扫 `core/internal/**` 里除那两个包之外的生产文件（把 `cmd/fake-adapter` 显式列为
  "协议对端侧、允许"），或者扫 `grep -n "case adapter\.MethodStreamDelta" 的调用方` 这类
  "谁在消费 Inbound.Frame"的结构判据 —— 按符号扫字面量迟早被生产者/消费者两种身份绊倒。
  另：`stream/item` 的票号是塞进不透明 body 里的（`NewStream` 注入 `sequence_no` + `turn_ref`），
  所以"§6 表格那四条里只算 `stream/delta` 和 `turn/finished` 两条"这个说法与盘上不符 ——
  `CarriesTurnRef` 的四条与契约括弧那份名单逐字相同，我这边不缺件。
- **T-66 回执（两条都已按做）**：① 备份一律绝对路径落 `.scratch\adapter\`；② 回滚后**证变异版不在盘上**：
  每条变异用"只有变异版才有的标记串"逐文件 `grep -c` = 0，外加 `cmp` 与批前备份**逐字节相同**。
  **自报一条本轮踩的坑**：我第一版证明脚本把 `grep -rc` 的输出用 `awk -F:` 求和，而 Windows 路径
  本身带冒号（`D:/...`），六个标记的计数全部假报 0 —— 结论碰巧对、证明过程是坏的。发现后改成
  逐文件相对路径 `grep -c`，输出贴在 Verification evidence。**"证据机制本身坏了"和"证据为 0"是两件事**。
  ④ 变异不藏早退：本轮 8 条变异都是改判据/改分支条件，不是让被测路径不执行（`s3` 那条把
  `RecordStart` 调用换成注释，验证的是"start 行必须被写"，判据仍是计数而不是"没崩"）。
  - **改名预告（T-61 ② / T-62）**：A-8 落地把 `turn_ref` 从 §6 规则 5 升成 **§6.6** 之后，
    我要做一次 `§6 规则 5` → `§6.6` 的替换（代码与账本同一批），本文件此刻的 `§6 规则 5` 是
    对**当前盘上文本**的正确描述，不是待修项；两份契约文本（§6.1 必发面与 §6.6 票号面）在 A-8 里
    并轨后，我 FakeAdapter 的 `stream/state`/`stream/item` 补齐才算对上判据。

- **T-58/T-59/T-61 收口（09-21 上午）**：
  - **Q1 落地**：`writeMethods` 现在是 §4.1 那五个 —— `session/send`、`session/stop`、`model/select`、
    `approval/respond`、`session/create`（最后这个是 **REQ-T8**：§4 表里没这行、`grep -rn "session/create" specs/`
    只在 §4.1 的缺口表里出现过一次，CO-5 的签名从未回写。我选"保留 + 提 REQ"，因为删掉它等于让一个写
    方法逃掉 §3 的门）。`debug/*` 由 `isDebugName` 覆盖（§4.1 把它算进写集合，已符合）。
  - **删掉一个幽灵**：`handshake_test.go` 原有两行用例把 `approval/cancel` 当存在的方法测（它
    在 §4、在代码、在任何规格里都不存在）—— 连同"审批走权限通道所以豁免"的旧 CO-9 注释一起删了，
    并在注释里写明它引用的 **§12.11 是个不存在的条款号**（`grep -rn "12\.11" specs/` 零命中）。
  - **反漂移守卫 `TestWriteMethodSetIsContractWrites`**：写集合的每一个名字必须是 §4 定义的方法，
    或是带 REQ 的已知例外；读方法混进来当场红；集合大小钉成 5。w4/w5 两条变异证明它会咬。
  - **Q10 有用例了**：`TestFakeAdapterGuideOnQuietSessionIsQ10` —— 静默会话发 `guide` 时
    `accepted:true`、`reason` 非空且说明"改开了一轮"、`turn_id` 非空、无 `queue_position`
    （`SendResult.Validate` 也跑过），并钉住这一问一答的完整帧序。w6/w7 两条变异证明判据会咬。
  - **全仓引用审计（`.scratch/adapter/cite-audit.sh`，只读）**：三包 **397** 处 `§` 引用逐 token 对
    `specs/**` + `docs/**` 的标题核。**结论：`§6.1`–`§6.5` 命中的是 `docs/handoff`（Phase 0 那套编号），
    不是契约②** —— 而契约② §6 根本没有子标题，只有规则 1..5。已全部改成 `§6 规则 N`；
    指 CO-8 的 `§6.3`/`§6.4` 改成 `CO-8`；`§9.4` 那处改成"事件契约 §5.5 与 §9"；
    跨文档的 `§3.4`/`§9.2` 加上"架构文档/交接包"前缀。**审计现在唯一的 NONE 是
    `handshake_test.go` 里那句"§12.11 不存在"的历史说明**（故意保留、有替换指向，不是缺陷）。
  - **CO-29 / REQ-T9（新发现，要主控处理）**：`grep -n "turn/claimed\|turn/dispatched" 契约②` =
    **0 命中** —— CO-8 那对两段式信号从未回写进 §6 的表或 §10 的验收清单，只活在裁定与我的代码里。
    这正是 T-59③ 定义的缺陷类型，而且它比一个错号子更严重：§10 的"stream/*"验收项字面覆盖不到它们。
    我没有"顺手补契约"，请主控回写 §6 表 + §10 第 11 行，之后我把注释里的 `CO-8` 换成真锚点。
- **T-62 硬门禁已交（T-55 我这一半）：接口形状的最终原文**（`core/internal/process/supervisor.go`）：
  ```go
  type Recorder interface {
      RecordStart(ev Event) // fires once per live instance; carries PID (§14 需要它)
      RecordExit(ev Event)  // clean exit: ev.Status EXITED + real ev.ExitCode (0)
      RecordCrash(ev Event) // abnormal: ev.Crashes counts toward §8
  }
  // Supervisor 上一个字段即可注入，nil 就是不落库、照常监督：
  //   type Supervisor struct { ...; Sink func(Event); Rec Recorder; ... }
  ```
  `Event{Kind, PID, Status, ExitCode, Crashes, Timestamp, Err}`；`RecordStart` 的 `Kind` 是
  `EventStarted`、`PID>0`、`Status=STARTING`、`Timestamp` Unix ms；`RecordExit` 的 `Kind` 是
  `EventExited`、`ExitCode==0`、`Status` 是 §14 的 `EXITED`；`RecordCrash` 的 `Kind` 是
  `EventCrashed`、`ExitCode!=0`、`Status` 是 `FAILED`、`Crashes` 是连续崩溃数。
  **定位方式：`(ev.Kind → §14 的 process_role 由注入方自己配置, Supervisor.Kind)` + "最新行"**，
  接口不带行句柄（我加了又撤，理由与 s1/s2 变异见 Verification evidence）；
  三种记录各自都是**恰好一次**的单次写路径（`RecordExit` 在"崩溃后 supervision ended"那条事件上
  被 `ev.Crashes == 0` 守卫挡住，所以崩溃不会被写成干净退出行）。
  `internal/process` 不 import `platform/db`、不 import `eventjournal`（`purity` 侧由 CO-3 的
  import 方向守卫钉住），三方法均无返回值、调用点不阻塞重启循环。
  §6.5/§12.13 的 chunk 归属走消费者侧：消费者拿 `Inbound{ExecutionID, Frame}`，票号在边界清空。

- **CO-30 → 主控**（§10 与 T-47/T-52 互相矛盾，我按"不落盘"绕开了）：§10 末句要 FakeAdapter
  "只读自己 `.scratch/` 下的 fixture 状态"，但 T-47/T-52 把 `.scratch/<角色>/` 定成**一次性产物目录**
  （别的角色会通配删、别人也会清），fixture 放那里等于把验收数据的真相源放在可被抹掉的地方；
  而 CO-1 又说 `adapters/fake/` 才是放 fixture 的地方。三者对不上。
  我的处理：FakeAdapter 的会话/消息/模型/审批数据**编译进 `service.go`**，一个文件都不读 ⇒
  确定、可审、不碰共享路径，也不违 §10 的原意（不许碰 Core 私有结构）。请改 §10 那句为
  "`adapters/fake/` 下的 fixture 或不落盘的内置夹具"，否则下一个实现者会去读一个随时会被清的目录。
- **CO-31 → 主控**（值集合未冻结）：`approval/respond` 的 `state` 取值，全部 specs/docs 里
  `grep -rn "approve\|APPROVAL_DECIDED\|APPROVED" specs docs` 零命中，只有 `granted` 出现在事件目录的
  `approval_resolved` 上。FakeAdapter 现在自认 `APPROVED`/`DENIED` 并在 `INVALID_PARAMS` 文案里明说
  "值集合未冻结"。请裁定 `state` 的集合与是否复用审批状态机（PENDING/APPROVED/DENIED/EXPIRED/CANCELLED）。
- **CO-32 已定，落 §4.2 的矛盾 ①**：**两帧不带 `turn_ref`**。票号只属于 §6 规则 5 那四条输出通知；
  `turn/claimed`/`turn/dispatched` 靠 §4.2 自己带的 `execution_id` 归属，因此 pump 的
  `turn_ref → execution_id` 映射**只能来自调用方显式绑定**（`Connection.CallTurn`），不是从帧里"学"出来的。
  请把 §4.2 那句"两处实现内部自相矛盾"改成这条结论，并同步 §6 规则 5 的适用范围（别写成"六条"）。
- **CO-33 已定，落 §4.2 的矛盾 ②**：**`turn/dispatched` 的 `provider_execution_id` 允许为空**（CO-13），
  必填只有 `turn_id` + `execution_id`；`turn/claimed` 仍必填该字段（§4.2 语义 3 的窗口要靠它问 Provider）。
  空句柄**不许**被回读成"从未运行"——这条现在有两处代码守卫加三条用例（`TestTurnTracker` 的两行 +
  `TestTurnTrackerEmptyHandleIsNotNeverRan`），并按规矩改写了旧判据、注明它以前测的是"三 id 缺一即拒"。
- **B-a10 已闭**：§6 规则 1 新"必发面"要求的 `stream/state`、`stream/item` 已在正常轮次路径发出，
  且有分两段数的用例（`TestFakeAdapterEmitsEveryNotificationType`）；§10 ① 的判据按新口径重测过。
- **T-50/T-58/T-59/T-62/T-66/T-72/T-73 各条**：均已按原文核对并回执，无未答项；
  T-50 的 (a)(b)(c) 三条**没重做**，`codeTable` 与 §15.1.1 的 19/19 一致性自那以后一字未动。

- **T-83 两问已答（09-21 12:4x，代码即答案，主控可直接拄进 §4.2）**：
  - **矛盾 ①（两帧带不带 `turn_ref`）→ 不带。** 判据是 `stream.go` 的 `CarriesTurnRef()`，
    集合就是 §6 规则 5 括弧里那四条：`stream/delta`、`stream/item`、`stream/state`、`turn/finished`。
    FakeAdapter 的 `notice()` 走同一个函数决定发不发，所以生产侧与消费侧不可能各说一套
    （`grep -n "CarriesTurnRef" core/cmd/fake-adapter/main.go` 命中一处即证）。
    连带结论：pump 的 `turn_ref → execution_id` 映射**只能由调用方 `Connection.CallTurn` 绑定**，
    两帧不参与归属。§4.2 第 143 行现在这么写是对的，**不用改**；要改的是 §6 规则 5 的"适用范围"
    一句——别写成"六条"。
  - **矛盾 ②（`turn/dispatched` 的 `provider_execution_id` 必填吗）→ 不必填，代码已改。**
    我 12:1x 已按 CO-13 改掉校验：`StreamFrame` 解码分支只剩 `turn_id` + `execution_id` 两个必填
    （`grep -n "turn/dispatched has no" core/internal/adapter/stream.go` = 2 行），
    `TurnTracker.Dispatch` 同口径，并新增用例 `TestTurnTrackerEmptyHandleIsNotNeverRan`
    钉"空句柄 ≠ 从未运行"；变异 o5（把跟踪器改回要求句柄）命中，证明这条判据会咬。
    **所以 §4.2 第 144 行"三者皆必填"要改成"两 id 必填、句柄可空"**（主控说本文按代码记录，
    现在代码是新口径）。`turn/claimed` 的句柄仍必填（语义 3 的窗口要靠它问 Provider），那行不用动。
  - **REQ-T8 的签名（主控采纳"保留 + 提 REQ"，这是给 §4 补那一行用的原文）**：
    `session/create` C→A，写方法（§4.1）。请求 `{client_request_id(必填，Core 生成 UUIDv7),
    project_id?, title?, base_sha?}`；`base_sha` 给了就必须是 40 位十六进制，否则 `INVALID_PARAMS`；
    `client_request_id` 缺失即拒（这一调用天然非幂等，没钥匙的重试会造出第二个远端会话）。
    回复 `{remote_session_id(必填), created, provider_raw?}`，`remote_session_id` 为空视为非法回复；
    `created:false` **不是失败**，是幂等命中（返回首次建的那个会话）。实现与校验在
    `core/internal/adapter/send.go` 的 `CreateSessionParams` / `CreateSessionResult`（CO-5）。
    FakeAdapter 侧还没发这个方法（第 5 轮 C 段的第二件，等 §4 有这一行之后我再接）。
  - **T-83 ② 收**：`WRITE_ENTRY_NONE` 的语义锚点 = **§4.1 + §3 两处**，我上轮那句"§4.1 只回答哪些是写方法"作废。

- **T-83 ③ 的答案是"没有，我补"**：78 个函数里原先只有 `TestWriteMethodSetIsContractWrites`
  （把 §4 的名字**抄进测试**当下限），没有与契约互校的用例。已补
  `core/internal/adapter/contract_table_test.go::TestWriteMethodsMatchContractSection`：
  运行时读契约② 的 **§4 方法表**与 **§4.1 的写集合句**，两边互校（我 gating 的名字必须是 §4 定义的；
  §4.1 声明的写必须都在我集合里；`debug/*` 走 `isDebugName` 验）。按 lint 规矩：**解析面收窄到 §4 那一节**
  （第一版扫全文，结果 §4.1 缺口表里的 `approval/cancel`、`session/create` 被当成"已定义方法"，
  幽灵名变异 **p1 当场 SILENT** —— 是这个 SILENT 暴露了我自己的解析越界，收窄后 p1b FIRED）；
  **解析为空 ⇒ 直接 fatal**，不许拿空集合判绿（p3 变异把锚点改名后确实 `FAIL`，报
  `§4.1's write set parsed to 0 names`）；文件不可达时 `t.Skip` 并在理由里写明"跳过不等于通过"。
- **CO-32 / CO-33 收（主控 12:45:43 `17ef3a7` 直接裁定，不再等我的答复）**：两条都判"代码要改"，
  我已按裁定实现并交证形（见 Completed 第 5 轮 C 段 + Verification）。**我原来的两问答复与裁定不一致，
  以裁定为准**：我上轮交的是"两帧不带 `turn_ref`"（照 §4.2 表格与语义 4 读的），裁定把范围收成"六条全带"；
  我没有理由维持旧读，也不打算沉默改代码 —— 差异写在这里，理由是裁定自己给的：
  归属只留一种写法，比留一张"哪一类靠哪一个"的例外清单更可执行。
- **CO-34（请主控落契约② 的三处裁定前措辞）**：裁定段（第 155-169 行）与它**上面**的表格/语义句冲突，
  三处都还写着旧口径 —— ① §4.2 表格 `claimed`/`dispatched` 的载荷列没有 `turn_ref`；
  ② 表格 `dispatched` 行仍写"**三者皆必填**"，与裁定 ② 的"可空但不得冲突"相反；
  ③ 语义 4 与 §6 规则 5 仍写"其余**四条**通知只靠 `turn_ref`"，而裁定 ① 是"所有六条"。
  我不自行改（`specs/**` 归主控）。这三处不修，下一个读者还会像我上轮那样答错，
  `core/test` 的 T84-4 也就是照着①②写的（见 CO-35）。
- **CO-35（一处越界红，按规矩只报不判）**：裁定 ① 落地后 `core/test/turn_evidence_test.go::
  TestTurnRefOnlyOnFourNotifications` 红（`./test/...` 唯一失败项）。不是我改的：该文件 mtime **12:23:53**、
  在 `git status` 里是 test 角色的未跟踪文件，且它第 20 行自己写着"⚠1（claimed 带不带 turn_ref）**未定**，
  所以 T84-4 只断'取证两帧不被要求携带'"。裁定已把 ⚠1 定为"必须携带"，所以该用例的前提消失了，
  判据需要 test 角色按 §4.2 裁定 ① 反转（连带它第 268 行那句 `§4.2 语义 4` 引用）。
  请主控决定是转给 test 还是授权我改 —— **在拿到答复前我不动 `core/test`**。
  顺带一条对上一句更正：T-83 ③ 交回时我说"三包 79 个测试函数"，那是 C 段之前的数；现在是 **82**（41/23/18）。
- **T-92 收（12:47:56 落账，13:05:02 我读到（`date` 实读））**：这条就是我上面 CO-32/CO-33 的答复，主控代答了。
  我按裁定实现，**没有沉默改代码**，也**不维持我上轮的相反答复**；差异与理由都写在 CO-32/CO-33 那条里。
  裁定 ① 里"不一致 = 丢弃 + Audit"这一句我只做到**丢弃 + 把理由交给消费者**：pump 的丢弃路径发的是
  `Inbound{Err: ...}`（消息含方法名、票号、两个 execution_id），`Inbound.ExecutionID` 在这条上**故意留空**
  ——争的就是"这帧属于谁"，我替它填任何一个都是在制造第二个真相。写 `audit` 行不归我包（§7 所有权），
  接线在 backend/cmd/core，若主控要我在丢弃时直接调 audit 写入，那是跨包依赖、请先给一句话。
- **CO-35 已被 T-93 接走**：主控 12:47:56 给 test 的 T-93 正是"把那两个 SKIP 落成硬断言"（①两帧必带票号、
  不一致丢弃 + Audit；②dispatched 句柄可空但不得冲突、且打在 Core 侧 tracker）。所以 CO-35 那条红
  **不需要我改 `core/test`**，等 test 反转判据即可；我这边三条用例已经把两侧都断上了。

- **CO-36（§4 的 `model/select` 行没有载荷字段，我按最小读法实现了，请主控确认或改写）**：
  §4 第 84 行只写了行为"对已有会话切模型；不支持必须显式报错"，**一个字段名都没给**。我的读法：
  请求 `{remote_session_id, model_key}`（复用 `model/list` 已经在发的 `model_key` 词汇，不再造第二个名字），
  回执 `{remote_session_id, model_key}`（**不加** `switched`/`applied` 这类布尔：§4 没有，
  而错误回答已经是否定答案）。落点 `cmd/fake-adapter/service.go` 的 `modelSelectParams`/`modelSelectResult`。
  请主控把这一行补进 §4（或告我换成 `models`/`model` 等既有词表里的名字）—— 在这之前
  它是"代码有、契约没有形状"的形状，我按 T-73 的规矩显式挂着，不当既成事实。
- **CO-37（两处红不是我造成的，附证据；13:16:50 那次全包跑）**：
  ① `internal/notification` 的 `TestNotifyWritesOneRowAndOneEvent`、`TestListByStatePagesWithKeysetCursor`
  —— 13:16:58 我第一次采到 `[build failed]`，**13:16:39 `go vet ./internal/notification/` 已经干净**，
  而 `internal/notification/service_test.go` 的 mtime 是 **13:17:20**（我取证那几秒它正在被写）⇒ 这是
  backend 在途保存的瞬时态，不是稳定红。
  ② `cmd/core` 的 `TestAnchorAdvancesOnlyAfterCommit`、`TestWiredAnchorCounterIsAttached`
  —— 正是 T-87/T-90 派给 audit 的 A18-1/A18-2，`cmd/core/core_test.go` mtime 12:09:09、
  `git status` 里 `M core/cmd/core/main.go` + 两个未跟踪新文件都在 audit/backend 名下。
  两处的包对我三包的 import 数 = **0**（`grep -rln "lucero/core/internal/adapter\|lucero/core/internal/process" cmd/core/*.go internal/notification/*.go` 空），
  所以不是我改断了它们。按 T-61 的规矩只报不判，我不去动它们一行。
- **REQ-T11 收（T-94，13:12:01 `6fc5e38`，对所有角色生效的证形升级）**：交回证形的第②句从"两句"升为**三句**
  —— ① `git diff --stat <HEAD> -- <我的路径>`、② `git status --porcelain -- <我的路径>` 专看 `??`、
  ③ **对每个新文件** `git diff --no-index --stat /dev/null <文件>` 给行数量级。
  第③句补的正是 B-a7 那条我早就发现、却写进默认证形时漏了反面的洞（`git diff` 对未跟踪文件隐形）。
  本轮我三个新文件的第③句实测（13:21:16）：`service.go` 468 行、`readers_test.go` 441 行、
  `contract_table_test.go` 148 行。往后每轮都按三句交，不再只交两句。
- **T-94 / T-95 都不指向 adapter**（分别给 test 与 backend），我读到只是确认没有新塞给我的活；
  其中 T-94 顺带确认了 §4.2 两问已裁、test 那两个 SKIP 由 test 落成硬断言，与我 CO-32/CO-33/CO-35 的口径一致。

- **CO-37 结案（自我更正口径）**：13:16:50 那 4 条外包红到 13:34:59 全部消失（291 PASS / 0 FAIL、16 包 ok）。
  我当时的判断是"在途保存的瞬时态"，依据是 `internal/notification/service_test.go` mtime 13:17:20
  与 `go vet ./internal/notification/` 13:16:39 已干净 —— 现在被复采证实。记这条是为了防我下轮把
  "瞬时态"当成免检结论：**每次仍要重跑再看**，而不是引用这次的判断。
- **CO-38（`process_instances.heartbeat_at` 没有认领人）**：000001 第 551 行那张表**有** `heartbeat_at INTEGER`
  这一列，事件契约第 197 行 `process_started/_exited/_crashed` 的载荷里**没有**它，契约① 与契约② 也
  都没说谁负责刷新；而 `docs/architecture/CORE_DOMAIN_MODEL_V0.1.md` 里的 `heartbeat_at` 属于
  `execution_attempts` 与 `execution_leases`（**执行侧**，不是进程侧）。于是这列今天注定永远是 NULL。
  §8 的 Recovery 判"进程死了还是 Core 死了"要的就是心跳，但适配器协议侧我能给的只有生命周期三笔 +
  `adapter/health` 的应答 —— **要不要由 `internal/process` 定时 poll health 写这列，是一个新交付面的取舍，
  我不自行决定**（决定权与判据都在主控/db/audit 的交叉处）。请裁：① 归我（则需给 poll 周期与失败码）；
  ② 归执行侧，本列在 v0.1 显式作废并写进契约；③ 交给 Recovery 的 lease 机制，与本列无关。
- **CO-39（§12 的"已由 `process` 聚合表达"与注册表冲突）**：见上面 Completed ②。要一句定调，
  不改代码；我已把实测事实写进 `Recorder` 的文档，避免下一个人照"有 process 聚合"去加 `AggProcess`。
- **REQ-T12（backend 的 T-55 另一半：`Recorder` 装配 + 合验，判据我写成可执行形式）**：
  接口三方法定稿不变（T-62），要做的只有装配。列映射按 `000001` 实测：
  | `process_instances` 列 | 来源 |
  |---|---|
  | `process_type` / `status` / `pid` / `exit_code` / `started_at` / `exited_at` | 我给的 `Event`（`Kind`/`Status`/`PID`/`ExitCode`/`Timestamp`） |
  | `id` / `created_at` / `updated_at` | 注入器自己生成（UUIDv7 + 时间）——**不回传句柄**，这是 T-62 的定稿 |
  | `restart_policy` / `owner_type` / `owner_id` / `executable` | **装配期事实**：注入器与 Supervisor 同处构造，Event 里没有也不该有 |
  | `metadata_json` | 承载 `Event.Crashes`（表里**没有** crashes 列，见 CO-38/§12） |
  合法写路径 = `tx.Append`（`process_instances` 不在 `aggTable`，`Touch`/`Create` 表达不了）；
  不许为它加 version 列，也不许分配 `transition_no`（§14 生命周期行不推进）。
  **合验判据（等你们那半边落地，我当场跑同一条）**：起真 `Supervisor` + 真 `fake-adapter` 子进程 + 真库，
  跑完"干净退出 / 崩溃一次 / 崩溃到 UNAVAILABLE"三条路径，断言 `process_instances`：
  ① 每条路径的行数 = 启动次数（不是我包内的次数）；② 干净退出那条 `status='EXITED' AND exit_code=0`
  且 `exited_at` 非空；③ 崩溃那条**不许**出现 `EXITED`（我包内 `emit` 已保证 `Crashes==0` 才发 RecordExit，
  行侧要能对上）；④ 崩溃到预算耗尽后 `metadata_json` 里的计数 = 实际崩溃次数；
  ⑤ 全表 `process_type`/`status`/`restart_policy` 三个 CHECK 没有一个被 SQLite 拒过（我的互校用例是它的前置保险）。
  落点建议 `cmd/core/core_test.go`（backend）或 `core/test/`（test 角色）——**两处都不在我 SCOPE**，
  所以我只能出判据、不能出那一半测试。
- **REQ-T8 收 / CO-36 收 / CO-38 收（三件都在 13:39~13:50 落账）**：
  `session/create` 已按 CO-5 的签名进 §4:88，我这轮的 ③ 就是它；CO-36 主控登记成
  "待建 / adapter / 实现为零"，巡检 13:45 实测更正为"只对 Core 侧成立"（替身里 `:26/:89/:283/:313-321` 真在）
  —— 我没有停在"被误登记"上，直接把类型搬进 Core 侧（第 6 轮 ③ ④），让那句登记作废；
  CO-38 主控裁 **`process_instances.heartbeat_at` 在 v0.1 作废、无人写无人读**，并给出真正要防的东西是
  **撞名**（库里 heartbeat 类列共 4 个：`adapter_instances.last_heartbeat_at`、`executions.heartbeat_at`、
  `execution_leases.heartbeat_at`、`process_instances.heartbeat_at`）。我不建 poll，也不碰那列。
- **CO-34 续（三处残留，其中一处是 REQ-T8 落地之后新产生的）**：① §6 规则 5 正文仍写
  "**四条** Adapter→Core 通知都必须带它"（`grep -n "四条" specs/adapter-protocol/…` 第 210 行），
  与裁定 ① 的"六条无例外"相反；② §4.2 的 `turn/dispatched` 那一行，**载荷列**已改成
  `{turn_ref, turn_id, execution_id, provider_execution_id?}` + "前三者必填"，但**同一行的校验列**仍写
  "任一为空 → INVALID_PARAMS …"has no provider_execution_id"" —— 一行之内自己和自己相反；
  ③ **§4.1 那句"本文口径下的写方法集合"里没有 `session/create`**（列表仍是 `session/send`/`session/stop`/
  `model/select`/`approval/respond`/`debug/*`），而 §4 表已经把它当 C→A 行收了。③ 是危险的一处：
  谁照 §4.1 逐字对齐 `writeMethods`，就会把"建远端会话"从 `write_entry:false` 这道门下摘出去。
  我在 `contract_table_test.go` 里把它钉成独立断言（t5 变异证明这条断言真的会红），
  但**契约文本仍要主控补**，否则测试是在替契约兜底。
  **本条 15:02 作废**：主控 14:39 把四处（含第 ④ 处两行 `model/select` 收成 1 行）全部修完，
  我 14:48 与 15:02 两次复采确认（残留"四条 Adapter→Core" = 0、`^| \`model/select\`` = 1、§4.1 含 `session/create`）；
  收口记录在下面的 CO-34 收。**保留原文不删**，因为它记的是"契约与代码各自有一份权威表而没互校"这个错形状。
- **快照与证形的关系变了（主控 13:59:09 `03b4c36` 起）**：那张"17/17 全绿基线"快照把我**未提交**的工作
  一并收了进去，于是 `git diff --stat HEAD -- <我的路径>` 与 `??` 都变空 —— 第②句若照旧交，
  等于交了一张空证。新形状我按两段写：**该轮交付 = `git show --stat 03b4c36 -- <我的路径>`**（18 files,
  2399 insertions(+), 222 deletions(-)，含全部 4 个新文件）**＋ 快照之后的脏差 = `git diff --stat HEAD -- <路径>`**
  （本轮 = 3 files, 37(+)/28(-)，即 ④ 那次类型搬迁）。请主控确认这个替代写法，或明确说"以快照为准就不用再交"。
- **CO-34 收（14:39 主控把四处全修完）**：① §6 规则 5 已写"12:45 由 §4.2 裁定① 扩到全部六条"、
  ② `turn/dispatched` 校验列已收成"`turn_ref`/`turn_id`/`execution_id` 三者任一为空即拒；
  `provider_execution_id` 允许为空、非空则须在 `TurnTracker.Dispatch` 比对"、③ §4.1 写集合已含
  `session/create`、④ `model/select` 表行 1 条。我这边同步撤掉两条**已经变成假话的注释**
  （`stream.go` 的"§6 规则 5 仍写四条"、`handshake.go` 的"§4.1 仍漏这个名字"），
  换成实测现状 + 保留"为什么当年不能照 §4.1 逐字对齐"的教训。复采用语未改（`TestWriteMethodsMatchContractSection`
  与 `TestFakeAdapterServesEveryContractMethod` 继续双向对拍，t5 证明它仍会红）。
- **CO-25 收**：主控裁"**排空点 = `turn/finished`，不是下一次 send**"，理由②③与我自己写 CO-25 时怕的
  是同一件事（没人再 send 的会话里排队项永久滞留，而症状长得像正常；一轮永不结束时必须保持**可见的 QUEUED**、
  绝不静默丢弃、也不许用计时器"看起来推进了"）。我按裁定实现，**没给相反理由**：它用的正是我列的候选之一，
  而且零计时器这一点保住了我当初选"下一次 send"的唯一动机（用例不抖）。
  实现落在替身侧（`cmd/fake-adapter`），因为它只是"这个 double 什么时候排空"的语义，契约① 的
  "真 Provider 自己排空、Core 只被动收 `turn/claimed`" 不变。
- **CO-40（新，请主控补定义再派活）**：T-110 末尾"**`session/push` 现在可以做，不必等本行以外任何东西**"
  与盘上不符。14:58:39 实测 `specs/adapter-protocol/adapter-protocol-v0.1.md` 里 **`push` 命中 0、`推送` 命中 0**；
  `grep -rln "推送" specs/ docs/` 的 7 个文件全是 api-contract / openapi（那是 Core↔客户端的 WS 推送，另一回事）
  与 phase0/handoff 观察记录；§4 方法表 16 行也没有这个名字。也就是说 push 缺的是一整套：
  ① 方向（C→A 还是 A→C？A→C 的话它是 §6 的第 7 条通知，票号规则要重开）；② 载荷；
  ③ **自发流的 `turn_ref` 从哪来**（这才是 CO-25 原本的同族问题：裁定 ① 把归属收成一条无例外之后，
  "不是 Core 发起的那一轮"没有票可挂）；④ 它和 §5 队列、和刚定的排空点是什么关系。
  我可以按最小读法提一版草案（A→C 通知 `session/push` = 无请求轮的 output 流，票号由 Core 在
  `session/create`/订阅时预发），**但那属于"写契约"，归主控**；给话之前我不动工，避免又造一次
  "代码有、契约没有"（`model/select` 那次就是这么来的）。
- **CO-40 收（主控 15:2x 裁定，契约② §4.0）**：撤销、不补四样；理由②（A→C 自发通知 vs 无例外的票号规则 =
  死结）我认，它比我原来的表述更硬，因为它不是"还没写"而是"写了就自相矛盾"。
  我这轮的对应动作只有两处注释同步（`main_test.go`、`connection_test.go` 的未建探针从"§6.6/CO-25 pending"
  改成"§4.0 明确不做"），**没有代码行为变更** —— 探针本来就断 `INTERNAL`，那个断言从"守待办"变成"守决定"。
  重开时必答的四样我照 §4.0 记着，不再自己列一遍。
  **同时更正我自己账本里一处旧说法**：第 6 轮起我在 Next action 里写"session/push 等 CO-25 那一句话"，
  这把两件事缝在一起了 —— CO-25 问的是**队列排空点**（T-110 已裁、已落地），push 缺的是**整条定义**。
  缝错的后果是我把一个不存在的东西当待办挂了三轮。记这条不是为了认错，是因为"待办写错来源"会持续误导下一轮的我。
- 第 9 轮：`core/internal/adapter/contract_table_test.go`（`TestStreamNotificationsMatchContractTables`
  + `methodConst`/`notificationRow` 两个解析器，复用同文件的 `contractFile`/`readContract`/`section`，
  不另起第二套读契约的代码）；`core/internal/adapter/stream.go` 无改动（曾被 w1/w2 变异又还原）
- **CO-43（自报，且是要主控补一句话的那种，不是求豁免）**：我 `supervisor.go` 里有一段挂了若干轮的
  "internal/process must not import platform/db or eventjournal（**internal contract §0's direction**）"，
  本轮按字复量：**§0（`specs/contracts-internal-v0.1.md:13-25`）只禁 `db → 业务包` 与 `audit → db`，全篇没有 `internal/process`**。
  那条"条款"是我自己的推断穿了条款的外衣（`grep -n "internal/process" specs/` 本轮才第一次跑）。
  注释我已就地改窄（"`Recorder` 接缝不出现 DB 类型"），但**请主控在 §1.1 第 17 条"落点与角色"那一格补一句**：
  写口所在包 import `platform/db`/`eventjournal` 合法（§0 的两条禁令方向不变）。
  不补的代价是具体的：下一个读到我旧注释的人（很可能是 backend 合验那轮的自己）会得出"写口不能在 adapter 的包里"，
  于是把 T-211 刚纠正的归属**再推回 `cmd/core` 一次**。
- **CO-44（真语义缺口，我不代裁；它决定我落码时 0 行怎么处置）**：契约第 17 条给 `TouchTransition` 的 0 行只定义了**一种**解释
  （"这条边已落过"⇒ 不插事件）。但 `process_instances.status` 有 **6** 个取值，而 `guardCol` 白名单 v0.1 只有 `status`、
  `guardVal` 只能一个值 ⇒ **"行还停在 `STARTING`（从没收到过 start 事件）"与"`EXITED` 已经落过"这两件事在同一个 0 行里不可区分**。
  我的默认处置（不阻塞，等裁）：0 行 ⇒ 写口返回 `nil`（幂等，按契约的"这一格已被别人落过"），**绝不做第二次尝试**；
  要不要允许"第二次 guard（`STARTING`）只在第一次 0 行时才跑"（最多仍是一条事件，但会把"没收到 start 就退出"这一格也落进库）——
  这一句要主控裁，因为它同时决定 `TestLifecycleRowLandsTheFiveColumns` 那五条判据里 `exit_code`/`exited_at` 的可达面。
- **CO-45（同上，形状题；它决定我要不要一个 id 源）**：`Supervisor` 每次拉起都会发一次 `EventStarted`
  （`supervisor.go:307` 那个 switch），而 `ProcessRow.ID` 按 T-62 的形状是**构造期**给的 ⇒
  重启第二次触发 `CreateVersionless` 会撞同一行的 PRIMARY KEY。两条路：
    (a) **一次尝试一行**：写口自己注入 id 源（`NewID func() (string, error)`），`metadata_json` 记 attempt 与上一条 instance id；
    (b) **一个 Supervisor 一行**：第二次起 `Start` 改走 `TouchTransition`（`FAILED → RUNNING`，guard 仍是 `status`）。
  我倾向 (a)：表名与 `idx_process_instances_active(process_type, status)` 都读成"一次实例一行"，且 (b) 会把 pid 历史压没。
  但它决定 **Recovery 怎么数活进程**（§8 崩溃预算的口径），归主控裁；裁下来之前我不落码。
  **05:37:20 补一句：这两问已经不再有"等 db 交付"这个前置**——`CreateVersionless` 已于 05:35:54 落地（`touch.go:401`），
  三入口签名与契约 §17 一致，`Append` 也已经对 versionless 表报错（`touch.go:367`）⇒ **CO-44/CO-45 现在是这条链上唯一没答的题**。
- **CO-44 / CO-45 收（T-221，契约① §1.1 第 20 条）**：两条都按裁定落码了（见 Completed 第 13 轮），
  我原来提的"0 行时再拿 `STARTING` 试一次"被否决、理由我认（一次观察不许分支成两条猜顺序的路径）。
  **落码后回读一句给主控核对**：第 20 条 Recovery 那句"`status IN ('STARTING','RUNNING','STOPPING')` 按 `process_type` 分组数行数"
  我这边的写法**只会产出 `STARTING`（建行）与 `EXITED`/`FAILED`（落边）三种值**，`RUNNING`/`STOPPING` 在今天没有任何生产者——
  这不影响计数（`STARTING` 算活），但**别把"没有 RUNNING 行"读成漏写**；要真用 `RUNNING` 得先有第二个观察点，那是新 REQ。
- **CO-46（转告 backend，第 20 条④的连带，主控不必裁）**：`cmd/core/dropaudit.go` 那三格从今天起可以真写了，形状是
  `process.NewLifecycleStore(process.ProcessRow{...}, newID, now, logf)` + 在既有 `store.Write(ctx, func(tx *db.Tx) error {...})` 里
  调 `RecordProcessStart/Exit/Crash(tx, ev)`；`RecordProcessStart` 会返回它铸的 id（日志与审计要用），
  `ErrNoLifecycleRow` 是**哨兵不是故障**（收到它就登记一条审计，"我知道有个 pid 退了、库里没有它的行"）。
  两点别踩：① `process_instances` 字面量仍不许进 `cmd/core`（W13 不变，我这格也不需要它——入口收的是 `AggregateKind`）；
  ② `TestLifecycleRowLandsTheFiveColumns` 必须**先落一行 started 再落 exit**，否则 `exit_code`/`exited_at` 无从断言
  （这正是第 20 条④写下的那条），而 `LUCERO_REQ_DB2=1` 那格**只有装配接上之后才可能 PASS**，在那之前它照 `t.Fatal`。
  ③ 顺带一条会让下一次误判的：`process_instances.heartbeat_at` 我**没写**（CO-38 判它 v0.1 作废），
  如果装配侧那五条判据里有人指望这列有值，那是又一处"契约作废 vs 代码没填"的错位，先来找我对字。
- **CO-43 收一半（我不自行判闭）**：包内那道上报边界已经变成测试（`TestOnlyTheWriteCellTouchesThePlatform`），
  注释里那条不存在的"§0 禁令"已删；但**请主控仍在 §1.1 第 17 条"落点与角色"那一格补一句**
  "写口所在包 import `platform/db`/`eventjournal` 合法"——我的测试只守得住 `internal/process` 这一个包，
  别人读到契约正文没有这句话时仍可能再造一次"归属推回 cmd/core"。
- **`AggCore: {table: ""}` 那一问作废（不是我等到了答案，是答案本来就在）**：T-224③ 记了 `db` 早在自己账本里答过
  （有意设计，空串语义 = "没有行可改"），而提问人在自己的账本里看不到 ⇒ 规矩补句"答案要写回提问人那一行"归主控。
  我这边的处置：账本里这条不再挂着，`AppendEventOnly`/`CreateVersionless` 对空表的显式拒绝我读过了
  （两处原文 `this aggregate has no table, use AppendEventOnly`，在 `CreateVersionless` 与 `TouchTransition` 各一处；
  **行号我按 07:1x 的盘读到 412/462，但引用一律按这段文字**——上一版我凭印象写 406/458，差 6 行，已按实测改），形状自洽。

- **CO-47（回给 db / 主控，07:26:14 我自己量的，不是转述）**：巡检把 db 那句"这道收紧在契约① 正文里还没有对应规范句"转给我时，
  我按字复核了，**结论与它相反、而且原因对 db 是好消息**：`grep -c "只准给\|绕过改行" specs/contracts-internal-v0.1.md` = **1**，
  原文就在第 17 条 ③ 的下面 —— **`AppendEventOnly` 只准给"没有表的聚合"用**（v0.1 即 `table == ""` 的那一个：`core`）。
  而 `git show HEAD:specs/contracts-internal-v0.1.md | grep -c "只准给"` = **0**、该文件 mtime **07:25:43**、`git diff --stat HEAD` = **+15 行未提交**
  ⇒ **db 07:12 那次 0 命中当时为真，主控刚刚（07:25:43）把那句补上了**；db 不必再等，去读盘上那一条即可（**引用按这段字面，别按我的行号**）。
  顺带两条同批读数：全仓 `AppendEventOnly(` 的调用方**只有 db 自己的 `versionless_test.go`**（生产侧 0 个）⇒ 这道收紧今天不碰任何人；
  `core/internal/platform/db` 与 `core/internal/process` 在 **07:26:42 各自 `ok`**。对我今天落的写口**零影响**：`lifecycle.go` 只用 ①②、`grep AppendEventOnly` 在我三路径 = **0**（巡检这条我复测成立）。
  **对我真正相关的是后面那一格**：接 `core_ready`/`core_shutdown` 时 `AppendEventOnly` **只能配 `db.AggCore`**——
  那两格不归我（`core` 聚合不是 `internal/process` 监督的对象），谁接我都提这一句，别拿 `AggProcess` 走它。

- **CO-48（请主控回一句，我不自己选定；它只关系到 Wave-2 那一腿的名字，不关系到今天的代码）**：
  T-230 说"连接侧那一半是你的：`turn/dispatch` 发出、拿 adapter 回执"。按你 09-23 立的 ⑥b 规矩我当场核了一遍，
  **`turn/dispatch`（不带 -ed）在 `specs/` 全树 0 命中，契约② §4 的 C→A 表里一行 `turn/*` 都没有**；
  盘上有的两个名字是 **`session/send`（C→A 的下发腿，§5 三态 + `Idempotency-Key` 必填 + CO-24 要带 `execution_id`）**
  与 **`turn/dispatched`（A→C 的取证帧，§4.2）**。三种读法我都可能选错：
  ①你说的是 `session/send` 之后收 `turn/claimed`/`turn/dispatched` 那对回执；
  ②你要新增一条 C→A 的 `turn/dispatch`（那是要**进契约**的新方法，不是我改代码）；
  ③你只是把 `turn/dispatched` 顺口写成了现在分词缺尾。
  **我按 (①) 准备、但不写任何一行迎合它的代码**，直到你回一句；选 (②) 的话它是契约变更 + 新用例，别默认我能顺手加上。
  **另：`turn/dispatch` 若真被登记进 §4 的 C→A 表，我这边有一条现成的判据会自动红**——
  `TestStreamNotificationsMatchContractTables` 与 `TestWriteMethodsMatchContractSection` 是**双向对拍**的
  （契约有名字而我没有 ⇒ 红；我有而契约没有 ⇒ 也红），这正是 ⑥b 那类"派一个 0 命中的名字"最省事的兜底。

- **CO-48 收（T-240，08:12）**：主控认账"那条 0 命中的方法名是我凭印象造的"，并按 ⑥b 更正：
  Wave-2 里我那一半是 **`session/send` 的下发（由 backend 编排层调用）+ `turn/claimed`/`turn/dispatched` 两帧取证**，
  **不是"发 `turn/dispatch`"**。我 08:13:03 独立复量（**递归口径**，与巡检 08:12:12 三方一致）：
  `turn/dispatch[^e]` = **0**、`turn/dispatched` = **5**、`session/send` = **8**、`turn/claimed` = **5**。
  **我这一半的形状今天已经在盘上、不需要新形状**：`internal/adapter/stream.go:31-32` 那两个 const + `:18` 的
  `CarriesTurnRef` 集合（`turn/claimed`/`turn/dispatched` 都在）+ `send.go` 的 `session/send` 载荷校验。
  主控另收下一条纪律（我 07:46 自己抓的那种错）：**凡"0 命中"的否定结论，先确认扫描面是不是全树**
  —— 我第一次用 `specs/*.md` 漏了 `specs/adapter-protocol/`，那次差点变成假否定。
- **CO-49（新，两包各存一份 §18 词表值；我不自行导出，等主控裁）**：backend 装配 08:10:47 落 `cmd/core` 之后，
  第 18 条那唯一词表值现在有**两处定义**：`core/internal/process/lifecycle.go:102` 与
  `core/cmd/core/processpipe.go:87`（**同名同值的两个非导出 const**，`grep '"adapter_instance"' core/ --include=*.go` 排除测试 = 2 处定义 + 1 处注释）。
  **今天行为没错**（两个串一字不差），但代价是将来 §18 登记第二个词或改名时**不会编译失败、只会静默不一致**：
  我的构造器拿**我那份**校验、他们的装配代码拿**他们那份**赋值。两条路：
  ① 我把 const **导出**（`process.OwnerTypeAdapterInstance`），`cmd/core` 改用它 ⇒ 一份真相、编译器兜住；
  ② 维持两份，但补一条**跨包一致性用例**（读两边字面量对拍）。
  我倾向 **①**（它比"再造一道判据"便宜，且正是 T-230 反面的反面：这不是"扩对外形状去迎合一个不存在的方法名"，
  是把已经存在的第二个真相合并回一处）。**但它动的是我的对外面 + backend 那一行**，所以按 T-230 的规矩先开 CO、不动手。

- **CO-50（转告 backend + 主控，08:17~08:18 实跑，两条都是行级覆盖面的事，不改我的码）**：
  ① **`TestLifecycleRowLandsTheFiveColumns` 对"改行腿"没有牙**（这是 T-227 ② 那条双向自证真正跑出来的结果）：
  基线带 flag **`--- PASS` (0.19s)**；把我建行腿摘掉 ⇒ **`--- FAIL`**、原文
  `lifecyclecolumns_test.go:430: \`process_instances\` 该恰好一行…: sql: no rows in result set` ⇒ **正向那一半成立**；
  但**把改行腿（`TouchTransition`）整个摘掉它仍然 PASS** —— 成因不神秘：那条判据的期望值就停在
  `status: string(process.StatusStarting)`（`:434`）、`startOneAttempt` 只驱动一次 start。
  我又把范围放大到**整个 `cmd/core` 包**跑了一遍去排除"别的用例在守"这个可能：
  唯二驱动 `RecordExit`/`RecordCrash` 的两处（`lifecyclecolumns_test.go:370`、`dropaudit_test.go:134-135`）
  **都是拿 `newDropRecorder(..., nil)` 构造的**，也就是它们断的正是"没装配 ⇒ 不落库"那一支。
  ⇒ **今天 `exited_at`/`exit_code` 的落库面只有我包内 `TestExitMoves…`/`TestCrashKeeps…` 在守**（对同一处摘除两者都 FIRED）。
  **要不要在行级那一半补一条"先 start 再 exit、断 `EXITED`+`exited_at`+`exit_code`"的判据，由主控定**（第 20 条 ④ 已经开了口子，
  我不擅自替 backend 那格加判据，也不动 `cmd/core`）。
  ② **`cmd/core` 此刻整包红在 `TestLauncherCarriesARecorder`（08:18:25，panic 不是断言失败）**：
  栈是 `cmd/core/rejections.go:142 auditHead()` ← `auditedStore.Write()` ← 你们的 `writeLifecycle`（`dropaudit.go`），
  症状是 `nil pointer dereference`、`auditedStore` 里那个 `*db.DB` 是 0x0。
  **因果我只说到栈能支持的程度**：`writeLifecycle` 在 `lifecycle != nil` 时才调 `store.Write`，而该用例的 core 没有库通道；
  它在我摘不摘写口时读数完全一致 ⇒ **不是我的写口造成的，也不是我能修的（那三个文件不在我范围内）**。
  报给主控/`test`：这条红 08:18 之后可能几秒就被 backend 自己的下一笔改掉（`processpipe.go` mtime 08:18:39 正在动），
  **我不拿它判 backend 的批次**，只留现场。

- **CO-49 收（T-248 裁定：单一来源 = `internal/process` 导出它）· 08:42 已执行，零行为变更**：
  `ownerTypeAdapterInstance` → **`OwnerTypeAdapterInstance`**，注释里点明"词表见契约① §1.1 第 18 条"、
  并写明导出的理由（写这一列的是本包，装配层只读它、不再抄字面量 —— §7 "cmd/core 不得含业务判断，值也一样"）。
  改动三处引用（定义 + `NewLifecycleStore` 里那句比较 + 错误文案的参数），旧名在本包 **0 残留**。
  **还差 backend 那一半**：`core/cmd/core/processpipe.go` 的 `:87` 定义 + `:118` 使用要删/改引用（T-247② 已派给他们），
  在他们改完之前**盘上仍是两份**，所以这条不由我判闭；我的导出已经让"改引用"成为一行事。
  **副作用要记**：这次导出改了文件内容 ⇒ 我 08:19 落的 sha256 指纹**作废**，新值（08:43:22 取）
  `lifecycle.go` = **`f4903acedf72acd2a38ecffa…`**（236 行，比 233 多的 3 行就是这段注释）。
- **CO-50① 复量（08:43:10~08:43:22，整包三条，不是抽查）——主控 08:33 那句对了一半，另一半我不能再默认它**：
  `cmd/core` 整包（带 `LUCERO_REQ_DB2=1`）**基线 `ok` exit=0**（08:42:56）⇒ `nil` 那一半确实过期了，backend 三格接上了；
  **摘掉我建行腿 ⇒ 两条用例红**（`TestLifecycleWriterLandsRowAndShouts` + `TestLifecycleRowLandsTheFiveColumns`，
  比我 08:20 那次多了一条在守建行）；**但摘掉改行腿整包仍 `ok`**（与基线输出只差计时，`r2=False` 说明变异确实进了观察面、不是空跑）。
  ⇒ **终态边（`EXITED`/`FAILED` + `exited_at`/`exit_code`）今天仍无任何 `cmd/core` 判据在 store 装配好的情况下驱动**。
  **这条不动码、也不由我补**（T-248 明写"归 backend 交账带断言，不归你"），我把配方原样交给他们：
  一次 `startOneAttempt` 之后再驱动一次 `RecordExit`/`RecordCrash`，断 `status` 变成 `EXITED`/`FAILED` 且 `exited_at`/`exit_code` 非 NULL；
  自检就是"把 `TouchTransition` 那一腿摘掉（一处早退）必须红"。报这条不是判 backend 的红 —— 他们是 08:38 才动完的，
  只是**别让"整包绿"被读成"终态边有牙"**。
- **等待条件按 T-248 更新，并且我又量了一次它是否已经变了（答案：又变了）**：
  主控写的"现在等的是 `LUCERO_REQ_DB2=1` 那格转 PASS（backend）"——**08:42:56 那一格已经 PASS 了**（整包 `ok`）。
  ⇒ 我今天真正等的只剩 **`test` 侧的合验发令（主控）** + **backend 删掉 CO-49 那份字面量** + **CO-50① 那条断言落地**。
  我不去动 `cmd/core` 的任何文件，也不提前跑合验。

- **CO-56（09-30 15:10，T-392 的两半接缝 + 一条我没擅自合并的约定）**：① **生产者半与消费半互相咬**：
  我把 `turn/finished` 移到 send 回执之后（T-392 ①），而 `cmd/core/sendpipe.go` 今天还是
  "回执后 `reapTurnFinished(conn.Inbound(), …)` 同步扫一次缓冲"（非阻塞 `default` 即返回）——**在新顺序下那条腿必然
  `Seen=false`**，于是"回执之后落终态"这一格只有在 backend 的常驻泵（T-391 ④）落地之后才可能绿。
  两半只落一半必然红，所以**别把我这一轮的 A 面当成可以单独入册的绿**；我不去碰 `cmd/core` 补那一半。
  现量（15:08:32）：泵那一半正在落（`sendreap.go` 已删、`turnpump.go` 未跟踪）。
  ② **一条看起来不一致、我故意没合并的顺序**：`session/stop` 与 `debug/hold` 的解除，收尾帧仍在**各自的回执之前**，
  因为它们的回执声称的正是"停了 / 收了"这件事（帧=证据，CO-7 方向）；send 回执不声称结束，所以结束在它之后。
  契约② §6 如果要把"收尾在回执之后"写成通则，这一处得先裁是不是例外——我不替主控裁。谁解锁：主控。

- **CO-57（09-30 16:51，T-398 ① 的开放半 + ② 的写者归属，请裁）**：
  ① **一台 Supervisor 一个孩子的闸门只挡了一向**：`Run` 拒绝在有连接时起跑（已实现 + 有牙），但
  "泵先起跑、连接后接上"那一向今天**只能靠交回孩子**才能解决 ⇒ 需要"每起一台孩子把这台交给调用方"的形状
  （回调或 `Supervised()` 句柄两条，我倾向回调，因为句柄会在重启的瞬间变成过期指针）。**这半归谁定**：泵在
  `cmd/core`，所以是 backend + 主控共同决定；**这台闸门今天不覆盖裸 `Start`**——`cmd/core` 的健康/发现那条腿
  要在同一条 Supervisor 上起短生命孩子，那是合法用法。所以「连起两次 Connect 且第一次不 Close」今天仍然能造出两台孩子；
  要把它也挡掉，认领就得从「记一个 `last`」改成「记一族」，那是另一格形状，我摆在这里不顺手扩。我不替它定形状，只要主控点头，`NewConnection` 已经是它需要的全部导出面。
  ② **`execution_leases` 的写者归属交回三档**（原文都在 `## Verification evidence` 第 32 轮那一格，逐条带 `文件:行号`）：
  **(甲)** v0.1 明写"租约不进范围"——理由是它唯一的立表前提"多 Runner"在 specs/代码里**没有任何定义与生产者**
  （"Runner" 全仓 3 处命中，两处在 §24 的两行里）；恢复输入退回 §56 另三件（`process_instances` 有行、
  adapter health 有探测、`provider_execution_id` 见 ③）。**(乙)** 保留租约——那么先要 `platform/db` 的
  `narrowTables` 开口（REQ-DB-1 §1.1.13，db 角色 + 主控），再补四件今天**完全没有原文**的东西：谁是 Runner、
  acquire 落在哪个调用点、TTL 数值、谁判过期。**(丙)** 承认 v0.1 只有进程级存活（`process_instances.heartbeat_at`
  那条已作废的列不再试图复活）。**我不选**，三条都需要契约文本；CO-38 那句"我不建 poll、不碰那列"在三档里都成立。
  ③ **`provider_execution_id` 请换个说法登记**：它不是"零写者 ⇒ 无信号源"，而是"**信号已到 Core、缺一次列写**"——
  值从 `turn/claimed` 经我的泵进 `TurnTracker`（有判据），落 `executions` 那一列的动作归 `internal/execution`
  （名册在 `check_ownership.sh:238`），时机按 `adapter-protocol:189` 必须是 claimed 那一刻（不能等 dispatched）。
  如果主控同意，REQ-RC-3 的"两半"就该分名册记：**一半是 execution 的列写（今天可闭），一半是租约的归属裁定（等裁）**。

- **CO-58（09-30 17:41，T-402 的调用面与那条跨家判据的配方）**：
  ① **给 backend 的调用面**（我不碰 `cmd/core`，这里只把形状讲清）：
  `(*adapter.SQLiteStore).DowngradeHealth(ctx, adapterKey, to) (bool, error)`；
  `core.instances` 今天就是这个具体类型（`cmd/core/main.go:76`），`retire`/`closeConn` 两支直接调即可，
  **不需要新装配、也不需要我扩 `ProbeStore`**（那个接口是探针的两方法合同，加方法会打死所有替身）。
  返回值语义：`(true, nil)` 真的降了；`(false, nil)` 已经在这个状态（**别当错误记**）；
  `errors.Is(err, db.ErrAggregateNotFound)` = 这张表没有这台（**别当成"注册它"**）；
  `*adapter.Error` + `INVALID_PARAMS` = 方向不被这扇门接受（抬健康 / 目标 UNKNOWN / 值不在协议四枚举）。
  ② **主控要的那条判据，今天只有一半能由我交**：「把写口摘掉 ⇒ 那条"连接死了要降健康"的用例必须红」——
  我这边的 h1 红了 6 枚（写口本身有牙），**但"泵在连接死亡时调了它"这条用例在 backend 名下，今天还不存在**，
  我不替他们声称。配方请随派发一起给他们：接上之后加一条用例（让连接死 ⇒ 断 `adapter_instances.health` 降了
  + 一条 `adapter_health_changed`），**它的反例是把 `DowngradeHealth` 那一次调用删掉（锚点=调用那一行，命中必须 = 1）**，
  跑法照我这轮的规矩：每枚变异前先跑基线预检，否则别人在途会把自己的枚判成 BUILD-BROKEN（本轮 v1 就被
  `eventjournal` 的一次在途编辑污染了三枚，原文留着在 `mut-r33-h*.log`）。
  ③ **一句我没有裁定依据的话**：一次连接丢失到底算 `DEGRADED` 还是 `UNAVAILABLE`，契约只裁了后者的一半
  （§8 连续 5 次崩溃 ⇒ UNAVAILABLE），没写单次丢失；我按"H→D 与 D→U 都收、U 是终点"实现，
  **但这是读法不是裁定**——要么主控认这一句，要么改成"泵只发 UNAVAILABLE"（那时我把 D 那一格从表里删掉，
  两条用例跟着收）。谁解锁：主控。

- **CO-59（10-01 12:3x，T-419 的三格决策文本；CO-55 由这一条推进，原条目留着不删）**：
  主控要求「每格三条、不要抛编号」，全文在 `## Verification evidence` 第 34 轮那一节，这里只留三句结论与三句我要的话：
  ① **能力表今天不拦调用**（`handshake.go:342-368` 只查 §2/§3；§9 的真闸门在声明侧 `:244`）⇒ 我推荐**只补双向判据、不动运行时**；
  若选「Core 发帧前本地拒」，请先在 §15.2 给它一行错误码，并接受 `session/locate` 那类**由 Adapter 给的**
  `CAPABILITY_UNAVAILABLE` 会被换成 Core 的本地拒（这条我有盘上证据：`cmd/fake-adapter/service.go:468-472`）。
  ② **能力降级今天没有可写的载体，也没有定义域**：事件的聚合必须是 `adapter` 而降级发生在能力行（守卫原文
  `platform/db/touch.go:571`），且 `resolveEffective`（`handshake.go:197-219`）只做包含/剔除、不改 `effective_backend`
  ⇒ 请**先裁「什么算一次降级」（`to_backend` 填什么）**，再在「v0.1 有意不发」与「骑在实例行 Touch 上发」之间选；
  我不在无定义域时建发射器。名单里那句「有名字无生产者」如果就按字面留着，请明写「有意的」，
  否则下一轮会有人把它当缺口来「补」。
  ③ **§10 表今天四行、没有 `debug/hold`**（`adapter-protocol-v0.1.md:376-379`，`grep 'debug/hold' specs` = 0 命中），
  而它已被三条判据正式使用 ⇒ 我推荐**补一行**并限定「只描述替身」。拟稿我可以给，字归主控写。
  **我要的话就一句**：这三格都是「要一句裁定」而不是「要我一段实现」——裁完任一条我当天就能把判据与码补齐。

- **CO-60（10-01 13:12，T-423 落地三件事：格一已办、格二按裁不办、格三拟稿在此等你落字）**：
  ① **格一（准甲）已交**：`cmd/fake-adapter/declaration_test.go` 的 `TestFakeAdapterDeclarationMatchesDispatch`，
  三条臂 + 三枚变异 3/3 FIRED（d1 就是 h4 那一档，今天只红这一枚新用例 ⇒ 此前无人守）。产品码一字未改。
  ② **格二（v0.1 不发）按裁办**：没建发射器、没动名单那行；我这边从此不再把它当欠账报。
  ③ **格三拟稿（`specs/adapter-protocol/adapter-protocol-v0.1.md` §10「故障注入（`debug/*`，EXPERIMENTAL）」那张
  两列表，接在 `debug/unknown_quota` 那行之后）**——
  > | `debug/hold` | 推迟/立刻发出这一轮唯一的强制收尾帧：`{"on": true}` 挂住（这一轮不收尾，测试才能把两个 Execution
  > 真放在同一条连接上、或把队列停在一轮在飞的轮次后面）；`{"on": false}` 在**解除那一刻**收尾，**CO-25 的排干点就落在
  > 这条帧上**。`on` 必须显式；解除可带 `code`（表内表外都收、默认 `SUCCEEDED`；`code` 挂在 `on: true` 上直接
  > `INVALID_PARAMS`）。`turn/finished` 的**常态位置在 send 回执之后**（契约① REQ-TURN-1 裁「Ⅰ」），这一枚只让它更晚到。
  > 只许 `adapter_key=fake` + `EXPERIMENTAL`（§9），**只描述替身，不描述任何真 Adapter**。 |
  >
  > 表下建议再留一行记账：「`debug/hold` 是 §10 的第五枚，CO-54 裁「丙」给的豁免只覆盖『这枚端点可以存在于
  > fake-adapter』这一件事。它今天是三条判据的仪器：REQ-TURN-1 判据 (a)（迟到收尾真落）、契约② §6 规则 5
  > （一条连接上并发两个 Execution）、§6 第 6 条的三态分辨（中间那一态「有帧但码未裁」靠解除侧的 `code` 产出）。
  > **补这一行的记账理由（10-01 现读）**：`grep -c 'debug/hold' specs/adapter-protocol/adapter-protocol-v0.1.md` = **0**，
  > 同枚在 `core/cmd/fake-adapter/main.go` = **9 处** —— 注入面只活在代码与台账里，别人复现判据只能读我的源码。」
  **落完请说一声**：我想在下一轮把它做成一条判据（「§10 表里声明的每一枚 `debug/*`，替身必须服务；服务着的每一枚
  `debug/*`，§10 表里必须有行」）——那正好是格一那条新判据在契约面上的镜像，双向都能守，不需要谁的记忆。
  ④ **顺带回一句排队**：主控本封确认 backend 同时领了 T-421/T-424 ⇒ CO-58 ② 那条「摘掉泵里那次调用 ⇒ 用例必须红」的
  联合反例我继续等，不先动 `cmd/core`；我这半的 `DowngradeHealth` 今天仍在树上、无调用点，所以那句「两半只落一半就是红的」
  对 health 这一格同样成立，别把我的绿单独入册成"降级已生效"。

- **CO-61（10-01 13:29，T-423 直投的两件同步回执 + 一句归属更正）**：
  ① **「已入册」那句的号差一枚**：我第 32/33 轮那五件在 **`2c65177`**（09-30 19:57:03「三处同改一次入库」）里，
  不在 `48c2547`；核对命令与逐件输出抄在 `## Verification evidence` 第 36 轮那一格。
  "已入册、不用重交"这个结论不变，我只是不跟着写错号——**下一个读台账的人拿 `48c2547` 去找 `NewConnection` 会找不到**。
  ② **§6 三处同改的配方收到**：新事件类型必须与 §6 名单（主控）、`openapi/core-v1.yaml`（frontend）、
  常量+目录（db/execution）**同一笔提交**，拆开必红 ⇒ 我这边的推论写死一条：**我不会在自己的批次里单独动事件名单或 yaml**，
  格二那种「要发新事件」的主张只会以三行决策文本交回，由主控排同批的三处同改。
  ③ **新计数口径已执行并当场见效**（它抓到的是我的 flake，不是别人的）：以后我每批交付默认跑
  「默认面 `-count=3` + `-v` 取数、可能红的先落文件再看尾巴」；我三包今天证明**没有旗标面**（grep 证据同格），
  所以那一半对我是空集，我不会为了交差造一面。
  ④ **顺带请主控记一句**（不是我要改契约，是这条已经第二次咬人）：**跨流断言要选一条流**。
  今天 `turn/finished` 与 send 回执的时序、以及这一轮 stdout/stderr 的丢行，是同一类问题的两面——
  判据凡是"读另一条流的东西"都要么在进程收尾后读（本包 `stop()`），要么把事实搬进被断言的那条流（回复/事件）。

- **CO-62（10-01 16:04，T-436 交账 + 两件要回给主控的盘上事实）**：
  ① **T-436 按三条要求办完**：判据能命中（s1 FIRED 且打印出 `TerminateProcess: Access is denied` 原文）、
  三种消红都不接受（前两种各有 FIRED 反证；第三种 s3 我交的是"不可构造"的理由，不是自裁——见证据格）、
  归属我没自辩（这格判给我我就在我包里修，`cmd/core` 一字未动）。
  **backend 那枚 1/15 红的关闭我不宣布**，请他复跑 `lifecyclecolumns_test.go:304` 那一格交读数；
  我只负责说清"为什么这次不是靠概率"：那分支在我包里有同步点（排空 stderr 到 EOF），所以判据是必然命中，不是撞。
  ② **CO-51 从 backend 那半已经闭了**（我读盘看到的，不是听转述）：`cmd/core/processpipe.go:222` 与
  `cmd/core/turnpump.go:493` 都改成调 `process.ChildAlreadyGone`，且他们的 `processpipe_test.go:214-225`
  钉着「本包不许再有私有副本」⇒ 请主控把 CO-51 记成**已结**（结法不是我导出符号那一步，是他们切调用那一步）。
  修完 T-436 之后那一判的角色从"唯一防线"降成"第二道"，两处调用点不用动、也仍然正确。
  ③ **「你 T-431 那两件我已入册」这句我对不上盘**：`grep -o "T-4[0-9][0-9]" .qoder/progress/adapter.md` 现读
  = T-402 / T-414 / T-419 / T-421 / T-423 / T-424，**没有 T-431**；而 `git ls-files --error-unmatch` +
  `git status --porcelain` 现读：第 35/36 轮那三件（`declaration_test.go`、`delivery_test.go`、`main_test.go`）
  **仍未提交**。⇒ 要么号错、要么指别家；我按"没入册"继续带着它们在途，不删也不重交，等主控一句。

- **CO-63（10-01 19:37，T-441 交账 + 一格请裁 + 两件盘上事实）**：
  ① **要我决定的到底是哪一件事**：用户在面板批了／拒了之后，`FakeAdapter` 停在 §4 闸门的那一轮**由谁、在什么时候**
  写 `turn/finished`。契约 §4:91 只裁了「这一轮停在这里，不许继续把动作送进 Provider」，**没裁「答案回来之后」**；
  而 C→A `approval/respond` 已经存在，双今天收到它只更新审批行、**不续跑也不收尾**（现读：`grep -n "awaiting" core/cmd/fake-adapter/*.go`
  只命中 `main.go:93/98/553/589`，没有任何一处把 `awaiting` 清掉 ⇒ 盘上事实就是"停住即终局"）。
  ② **选项与各自关掉/打开哪一格**：
     **(甲)** 答复续跑：`approval/respond{APPROVED}` ⇒ 双发 `stream/item{state:"COMPLETED"}` + `turn/finished{SUCCEEDED}`；
       `DENIED` ⇒ `turn/finished{CANCELLED}`（§6 第 6 条那三个合法码之内，**不新增终态、不加字段**）。
       关掉「v0.1 能不能端到端演一次审批往返」这一格，并给 backend T-442 一个真对端；打开的只是我包内那双的
       `approvalRespond` 语义（今天它只记不续）。
     **(乙)** 保持现状：停住就是终局，只有 `session/stop` 或进程死才收尾。省一步，代价是面板批准后线上永远等不到终态、
       backend 那一半只能靠手工造帧来测，且常驻连接上会留一条一直 `RUNNING` 的轮次（§14 那一行只能写"在飞"）。
     **(丙)** 现在不定，等「Provider 侧审批有没有期限/超时」那一格一起裁。最省，但 T-442 的验收会卡在"没有对端"上。
     **我推荐 (甲)**，理由一句话：契约已经把 `approval/respond` 写成「把答案送回去」，**送回去却没人接着跑，
     那条方法在 v0.1 就没有任何可观察的效果**；而 (甲) 只动我包里的双，不动协议、不加字段、不碰别人家。
     **没裁之前我按现状交**（第 38 轮盘上就是 (乙) 的形状），不自己加语义。谁解锁：主控。
  ③ **两件盘上事实上交，不是问题、也不要谁改代码**：
     一、§6 规则 5 正文仍是「全部六条」+ 一句计数注记。我按主控那句「别按那句计数来」把自己三包内**所有按数字写**的
     措辞换成按族写（现读 `grep -rn "all six" core/internal/adapter/ core/internal/process/ core/cmd/fake-adapter/` = **0**、
     `grep -rn "六条"` = 2 处，两处都是「这句不绑数字」的说明）；`specs/**` 里那句话本身归主控，我不动。
     二、B 面 `core/test` 两枚红（`TestWiringSurfaceHasCallSites` 5 格零调用点、`TestEventProducerRosterMatchesDerivedSet`
     两条名单）原文与归属都在证据格，契约 §3.0-c 已把它们钉在 T-430/T-442 上并明文「不许为了转绿改这张表」⇒
     **我一颗调用点都没造**，只报读数。


## Changed files

- **第 38 轮（10-01 T-441）· 9 件 = 4 件产品码 + 5 件测试（含 1 枚未跟踪新文件），全在我名下三包**：
  现读 `git diff --numstat HEAD`（19:34:15）= `internal/adapter/stream.go +29/−4`、
  `internal/adapter/stream_test.go +26/−3`、`internal/adapter/contract_table_test.go +23/−4`、
  `internal/process/connection.go +5/−5`、`internal/process/connection_test.go +113/−0`、
  `cmd/fake-adapter/main.go +73/−3`、`cmd/fake-adapter/readers_test.go +16/−3`、
  `cmd/fake-adapter/service.go +4/−1`，另**未跟踪** `cmd/fake-adapter/approval_request_test.go`（203 行，`wc -l` 现读，别按 numstat 找它）。
  - `stream.go`（产品码）：`MethodApprovalRequest` 常量、`Action`/`Resource`/`RiskHint` 三个字段
    （`provider_execution_id` 复用已有那一格，不另造名字）、进 `CarriesTurnRef` 的 switch、
    `DecodeStream` 缺 `action` 即拒；注释里四处「按数字写」的措辞改成「按族写」。
  - `main.go`（产品码）：`approvalGate`（真高危路径发帧 + 落一条 PENDING 审批行 + `t.awaiting = true`）、
    `riskyTools` 那份 action/resource 表（串取自分类器）、`settle` 认闸门、`turn` 多一个 `awaiting` 字段。
  - `connection.go`（产品码，**只有注释**）：两处 `all six` 换成按族写 ⇒ 本轮 `internal/process` 的行为零改动，
    `applyTurn` 不给 `approval/request` 加 case 就是交付形状，不是漏做。
  - 测试：`stream_test.go` 往返表 + 生产者拒绝表 + 取证表各加一行（缺 `action`／缺 `turn_ref`／缺 `sequence_no` 三枚反例）；
    `contract_table_test.go` 的守卫补第三个解析源（§4 的 A→C 方向列）并把那句「六条全部带」的措辞去掉；
    `connection_test.go` 新增 `TestConnectionForwardsApprovalRequestWithoutClosingTheTurn`；
    `readers_test.go` 的家庭判据接上第七条；`approval_request_test.go` 三枚（真路径 + 分类词表 + 方向未动）。
  - **没碰** `specs/**`、`docs/**`（`git diff --numstat HEAD -- specs/ docs/` 现读 = 0 行）、`cmd/core`、
    `internal/{permission,approval,execution}`（只在 `_test.go` 里读 `permission.RiskLevel`）；
    `approval/list`/`approval/respond` 的方向与形状一字未改。
    证据：`faceA-r38-count3.log`、`faceB-r38.log`、`mut-r38-*.log`（12 份，含基线与 a5b 补跑）、四个 `*.bak-r38`。

- **第 37 轮（10-01 T-436）· 产品码 + 用例各一枚，都在 `internal/process/`**：
  `supervisor.go`（`Stop` 的收口逻辑与它头上的契约说明 + `ChildAlreadyGone`/`Stop` 文档各补一句）
  与 `supervisor_test.go`（两枚新用例）。现读 `git diff --numstat HEAD -- core/internal/process/` =
  现读**只有两件**：`supervisor.go +29/−5`、`supervisor_test.go +69/−0`（第 32 轮的 `connection.go`/`attach_test.go` 已随 `2c65177` 入册，本轮是在那之上又动的）。
  - `Stop`：`killErr := Kill()` → 有界 `Wait` → **收到状态就是 nil**；收不到才报，优先报 `killErr`。
    **不分类 OS 错误码**（不认 `Access is denied` 字符串、不认 `syscall.Errno`）。
  - 新用例：`TestStopIsQuietWhenTheChildAlreadyExited`（确定性靠「排空 stderr 到 EOF」= 它已死，无 sleep）、
    `TestStopStillReportsAChildItCouldNotStop`（活孩子 + 零期限必须仍报失败）。
  - **没碰** `cmd/core`（backend 那枚 1/15 红的用例留在原处，我没搬也没改）、`specs/**`、`internal/adapter`、
    `cmd/fake-adapter`；`ChildAlreadyGone` 符号与它的两处 cmd/core 调用点原样保留。
    证据：`faceA-r37-count3.log`、`cmdcore-r37-count3.log`、`mut-r37-*.log`（4 份）、`supervisor.go.bak-r37`。

- **第 36 轮（10-01 T-423 直投 + 新口径）· 只动测试，产品码仍零改动**：
  `git diff --numstat HEAD` 现读 = `cmd/fake-adapter/delivery_test.go +11/−5`、`cmd/fake-adapter/main_test.go +4/−3`，
  加第 35 轮那枚未跟踪 `declaration_test.go`（211 行）；`main.go` sha 仍是 `c11aaf2ffe55adbf`（变异批 PRE=POST 同值）。
  - 三处「活进程期间读 `p.stderr`」的断言搬到 `p.stop(t)` 之后：`delivery_test.go` 的
    `with code "INTERRUPTED", but nothing is open` 与 `hanging on h1`，`main_test.go` 的 `fake-adapter up`。
    每处都留一句成因注释（os/exec 的拷贝协程），并把「为什么在 stop 之后读」写死，防下一个人搬回去。
  - `main_test.go:388`（`crashing on demand`）本来就在 `cmd.Wait()` 之后 ⇒ 未动，**别把它也算成搬迁过的一枚**。
  - **没碰**产品码、`specs/**`、`cmd/core`、`internal/adapter`、`internal/process`。
    证据：`faceA-r35-count3.log`（病灶那份，红着的那次）、`flakefix-r36-count6.log`、`faceA-r36-count3.log`、
    `mut-r36-*.log`（4 份）、`main.go.bak-r36`。

- **第 35 轮（10-01 T-423）· 交付面 = 一枚未跟踪新文件**：`cmd/fake-adapter/declaration_test.go` **211 行**
  （1 条用例 + 3 个解析助手 + 1 条例外表 + 1 个 `dispatchEnd` 小助手）。
  `git diff --numstat HEAD -- core/cmd/fake-adapter/` 现读 = **0 行** ⇒ **产品码一字未改**（主控裁的就是「不动产品语义」），
  `main.go` 的 sha 仍是第 31 轮交付值 `c11aaf2ffe55adbf`。
  - 新判据 `TestFakeAdapterDeclarationMatchesDispatch`：A 向（声明有效 ⇒ dispatch 里有）、B 向（dispatch 里有 ⇒ 声明表里有）、
    C 向（`notACapability` 三条例外必须仍在服务、且不许长过三条）。
  - 三枚下限 + 一行 `t.Logf` 集合大小（served/declared/effective/exceptions），解析器变窄会先在这里露。
  - **没碰** `specs/**`（格三只交拟稿）、`cmd/core`、`internal/adapter`、`internal/process`、`specs/events/**` 那一行名单。
    证据留档：`faceA-r35.log`、`faceA-r35-final.log`、`faceB-r35.log`、`mut-r35-*.log`（4 份：基线 + 三枚）、
    `stamp-r35.txt`、`declaration_test.go.bak-r35` 与 `main.go.bak-r35`。

- **第 34 轮（10-01 T-419）· 零代码改动**：本轮产出是**三格决策文本**（在 `## Verification evidence` 第 34 轮那一节）
  与 `## Coordination` 的 CO-59。`git status --porcelain` 现读：`core/internal/adapter/**` 与 `core/internal/process/**`
  **对 HEAD 零差异**（第 32/33 轮已入册）；未跟踪文件里**没有我这轮的产物**，一次性脚本仍只在 `.scratch/adapter/`。

- **第 33 轮（09-30 T-402）· `git diff --numstat HEAD` 现读（17:41，HEAD `6ced981`）**：
  `internal/adapter/probe_store_sqlite.go +103/−8`，加一枚未跟踪新文件
  **`internal/adapter/health_store_test.go`**（6 条顶层用例 + 5 条子用例 + 2 个助手，未跟踪 ⇒ `--numstat HEAD` 看不见）
  ⇒ **交付面 = 1 改 + 1 新**。
  - `probe_store_sqlite.go`：新导出的 `(*SQLiteStore).DowngradeHealth`、`healthWrite`（两条门共用的那一个写手，
    同笔三列 + §6 的三把事件键）、`healthDowngrades` 闭集表与 `isDowngrade`、
    `MarkUnverified` 那段自己的 `tx.Touch` 改为调 `healthWrite`（行为不变，h7 是它的回归位）。
  - `health_store_test.go`：`TestDowngradeHealthMovesTheTrioInOneStatement` /
    `…RefusesWhatItIsNotEntitledToSays`（5 条子用例）/ `…DoesNotRegisterAnAdapterItOnlyLost` /
    `…FromAnUnprovedRowOnlyAcceptsFailure`（UNKNOWN 与 STOPPED 两个起点）/
    `TestBothHealthDoorsWriteTheSameTrio` / `…RejectsAValueOutsideTheProtocolEnum`。
  - **没碰** `cmd/core`（泵的接线归 backend，我只把入口导出）、`internal/execution`、`internal/platform/db`、
    `api/http`、`specs/**`、`docs/**`；`ProbeStore` 接口一字未改。
    证据留档：`faceA-r33.log`、`faceB-r33.log`、`mut-r33-*.log`（v2 那一批 9 份 + v1 被外部破坏污染的那几份仍在原地，
    标着「判决不作数」）、`probe_store_sqlite.go.bak-r33`。

- **第 32 轮（09-30 T-398）· `git diff --numstat HEAD` 现读（16:49，HEAD `5215256`）**：
  `internal/process/supervisor.go +99/−1`、`internal/process/connection.go +23/−0`（`git diff --numstat HEAD` 只数已跟踪的
  两件），加一枚未跟踪新文件 **`internal/process/attach_test.go`**（5 条用例 + 2 个助手，`+222`，
  未跟踪 ⇒ `--numstat HEAD` 看不见，只能按文件行数记，别当成"改动少了"）
  ⇒ **交付面 = 3 files, +122/−1 已跟踪 + 1 新文件**。
  - `supervisor.go`：`Supervisor.mu/last` 两个字段、`Child.sup/conn` 两个字段、`ErrForeignChild`/`ErrChildBusy`
    两个哨兵、`ladderMayStart`（Run 的那半闸门）、`attach`/`release`（认领与归还）、`Start` 结尾登记 `s.last`、
    `Run` 开头一句拒。
  - `connection.go`：新导出的 `(*Supervisor).NewConnection`（`Connect` 改成 Start + 它）、`Close` 里那一条
    `defer c.sup.release(c.child, c)`（每条出口都放，含回收失败那条）。
  - `attach_test.go`：五枚（rides-without-starting / refuses-a-child-it-did-not-start / one-child-one-connection /
    run-refuses-beside-a-connection / run-allowed-again-after-close），"起没起第二台"一律读 Launcher 计数。
  - **没碰** `cmd/core`、`api/http`、`internal/execution`、`internal/platform/db`、`specs/**`、`docs/**`
    （`execution_leases` 与 `narrowTables` 那两件的归属不在我，写者归属只以 CO-57 交回）。
    证据留档：`faceA-r32.log`、`faceA-r32-final.log`、`faceB-r32.log`、`mut-r32-*.log`（7 份：基线 + 六枚）、
    `t398-readings-162659.log`（③ 的五条读数）。

- **第 31 轮（09-30 T-392）· `git diff --numstat HEAD` 现读（15:10，HEAD `264d562`）**：
  `cmd/fake-adapter/main.go +61/−24`、`cmd/fake-adapter/delivery_test.go +134/−2`、
  `cmd/fake-adapter/main_test.go +25/−0`、`internal/process/connection_test.go +78/−0`
  ⇒ **4 files changed, 298 insertions(+), 26 deletions(-)**。
  - `main.go`：`sessionSend` 收尾后移（`accept` 多回一个"这发开出来的轮次"）、新增 `settle()`、
    `debugHold` 的解除带可选 `code`（布防带码直接拒）、`startTurn` 不再自己收尾。
  - `delivery_test.go`：`TestFakeAdapterClosingFrameFollowsItsReceipt`（① 的位置判据，start_now + guide 两条腿）、
    `TestFakeAdapterTurnFinishThreeStatesOnTheWire`（③ 三态一枚进程里出齐）、Q10 与 ③Ⅰ 两条按新顺序改写。
  - `main_test.go`：`next()`（读回执**之后**那一帧）与 `rawClose()`（收尾帧的原始 params 字节）两个助手。
  - `internal/process/connection_test.go`：新增 `TestLateTurnFinishedClosesTheTurnItBelongsTo`
    （帧到之前不许有终态、表外码不许被洗）。
  - **没碰** `cmd/core`、`api/http`、`internal/execution`、`internal/task`、`specs/**`、`docs/**`。
    证据留档：`faceA-r31.log`、`faceB-r31.log`、`mut-r31-*.log`（九份：基线 + 八枚）。

- **第 30 轮（09-27 T-386）· `git diff --numstat HEAD` 现读（17:10）**：
  `cmd/fake-adapter/main.go +85/−3`、`delivery_test.go +75/−8`、`readers_test.go +12/−2`、
  `main_test.go +12/−0`（`armHold` 助手）、`internal/process/connection_test.go +32/−19`
  ⇒ **5 files changed, 216 insertions(+), 32 deletions(-)**。
  - `main.go`：`methodDebugHold` 常量、`fake.hold` 字段、`debugHold()`（`on` 必须显式，缺省即拒）、
    `startTurn` 末尾的默认收尾、`drainQueue` 的"排干不受 hold 影响"定形注释、`debug.hold` 能力声明。
  - 四枚 fake 用例改造 + 两枚新增（`…StartNowClosesItsRun` = ③Ⅰ、`…HoldReleaseIsWhereTheCloseGoes`）。
  - `internal/process/connection_test.go`：两条消费者用例改用 `holdOpen()` 要一个在飞的轮次
    （§6 第 6 条③ 改了默认，"有轮在飞"不再是免费的前提）。
  - **没碰** `api/http`、`internal/execution`、`cmd/core`、`specs/**`、`docs/**`。
    证据留档：`mut-r386-out-170753.log`、`faceA-r386.log`、`delivery-r25-171001.log`。

- **第 29 轮（09-27 T-382）· `git diff --numstat HEAD` 现读（16:31）**：
  `cmd/fake-adapter/main.go +18/−2`、`cmd/fake-adapter/delivery_test.go +17/−6`、
  `cmd/fake-adapter/readers_test.go +4/−2`、`internal/process/connection_test.go +19/−19`
  ⇒ **4 files changed, 58 insertions(+), 29 deletions(-)**。
  （`connection_test.go` 那 19/19 几乎成对，是因为改写的是同一段通知循环：换条数、换断言，不是加代码。）
  - `main.go`：`resultSucceeded` 常量（按名字对齐 §6 第 6 条，不抄第二份枚举）；
    抢占分支 `finish(prev, "INTERRUPTED")` → `drainQueue()`（**排水留、帧不发**）；`drainQueue` 的字面换掉。
  - 两枚 fake 测试：线节顺序去掉那一帧 + **明写"不许有那一帧"的循环断言**；`"OK"` 断言换 `"SUCCEEDED"`。
  - `connection_test.go`：消费者侧 `TestConnectionSendOverRealPipe` 按裁定改写（7→6 条通知、
    tracker 必须留 `RUNNING`/空码、线节上不得出现被抢占轮的收尾帧）。
  - **没碰** `api/http`、`internal/execution`、`cmd/core`、`specs/**`、`docs/**`（backend 正在落 REQ-S-1 那几件）。

- **第 28 轮（09-27 13:44~13:51，T-362）· `git diff --numstat HEAD -- core/internal/process/` 现读**：
  `connection.go +4/−4`、`connection_test.go +1/−1`、`supervisor.go +7/−5`、`supervisor_test.go +19/−0`
  ⇒ **4 files changed, 31 insertions(+), 10 deletions(-)**（`--stat` 的每列是缩放数，这里全是 numstat 真实行数）。
  - `supervisor.go`：`childAlreadyGone` → **`ChildAlreadyGone`（导出，单一来源）**，文档注释写进对拍数字
    （212 字节 / sha `7364ae9e5ca630a8` / 两份定义的行号）。
  - `connection.go` + `connection_test.go`：**只是跟着改名**（调用点 1 处 + 注释 4 处），无行为变化。
  - `supervisor_test.go`：**新增** `TestChildAlreadyGoneAnswersBothQuestions`（+19/−0），并补 `fmt` import。
  - **没碰** `cmd/core/processpipe.go`（那份副本按裁定由 backend 改调用，不是我去改）、
    没碰 `api/http`、`internal/task`、`specs/**`、`docs/**`。

- **第 26 轮（第二批 = 可取消的 `write`）· 只在 `core/internal/process/`，`numstat` 对 HEAD `99a30f0`**：
  `git diff --numstat HEAD -- core/internal/process/` = **`connection.go +56/−1`、
  `connection_test.go +108/−0`，合计 2 files changed, 164 insertions(+), 1 deletion(-)**
  （第一批那 4 件已在 `ba03ce2` 里，不重复计入本轮）。
  - `connection.go`：`writeWithin`（写放可放弃 goroutine + `select { 写完 / ctx.Done() / c.dead }`）、
    `abandon`（只 `kill`，**不设 `c.closed`** —— 原因见验证 ⑤）、`request` 首次写改走 `writeWithin`、
    `Cancel` 增看 `c.dead`。**`Close`/`Stop`/`write` 三处一字未改**（第一批的形状没动）。
  - `connection_test.go`：三条用例（期限快返 / `Cancel` 快拒 / **goroutine 不泄漏**），
    加 `TestAbandonedWriteComesBackInsideTheDeadline` 里那行把实测耗时**印出来**的 `t.Logf`
    —— 只断言不打印的判据，别人复核不了我到底量的是什么。
  - **没有** `cmd/core/**`、**没有** `api/http/**`、**没有** `specs/**`、`docs/**`。
    证据一律留在 `.scratch/adapter/` 并**活到入册之后**（§24 ①）：`mut-r26-out-213345.log`、
    `ctxprobe2-retake-211821.log`、`ctxprobe2-retake2-212000.log`、`ctxprobe2-after-batch2-213733.log`、
    `delivery-r25-213855.log`。包内 `zz_*` 残留 0、`grep -rn "ZMUT" core/` = 0。

- **第 25 轮（09-25 20:31~20:56，T-314 三处点名 + ⑤ 落地）· 只动 `core/internal/process/**`**：
  `git diff --stat HEAD -- core/internal/process/` 现读 = **4 files changed, 176 insertions(+), 6 deletions(-)**
  （21:13:47 复量；这一批之后我又按 T-326④ 降级过一次注释，175→176 就是那一次。
  **`--stat` 那列是缩放数、`--numstat` 才是真实行数**，两枚数长得不一样，别互相抄）
  （**命令按包限定**：21:06 起 `core/` 还有 backend 的 `api/http/tasks.go`，按 `-- core/` 数就不是这个数了）
  - `connection.go` **numstat +46/−3**（`--stat` 那一列显示 49，是缩放数）：`const closeReapTimeout = 2 * time.Second`；`Close()` 接上既有
    `Supervisor.Stop`（先 `Stop` 后还管道、`childAlreadyGone` 过滤）；`Cancel` 的注释点名
    "`request` 的 ctx 分支之后还有第二道不可取消步骤"（**没改它的行为、没加期限**）；
    删掉一条被本轮实现推翻的旧注释（`Close stops the pump … It does not kill the child`）。
  - `supervisor.go` **+33**：`(*Child).Reaped()`、`childAlreadyGone(err, pid)`、补 `os` import。
    **`Stop` 本体没改**（⑤ 要的是"接上它"，不是"改它"）。
  - `connection_test.go` **numstat +91/−2**：4 条新用例（`TestCloseReapsTheChild`、
    `…AfterAnExistingReapStaysQuiet`、`…WithoutASupervisorReapsNothing`、
    `…AfterTheChildAlreadyCrashedStaysQuiet`）+ 一条 **flake 修正**
    （`TestConnectionDeathMarksTurnUnknown` 的等待谓词换成被断言的那个谓词）。
  - `supervisor_test.go` **numstat +6/−1**：助手模式 `sleep`（不理 stdin、30s 后自退 = 卡死孩子形状）。
  - **没有** `cmd/core/**`、**没有** `api/http/**`（T-314 ③ 的硬边界），**没有** `specs/**`、`docs/**`；
    探针与脚本全在 `.scratch/adapter/`（`ctx-probe3.sh`+`ctxprobe3_test.go.txt`、
    `ctx-probe4.sh`+`ctxprobe4_test.go.txt`、`ctxprobe5_test.go.txt`、`mut-r25.py`、
    `delivery-r25.sh`、`delivery-r25-205535.log`、`delivery-r25-203909.log`、`probe4-full.log`）。
- 第 18 轮（09-24 10:37~10:46，T-275 黑盒注入门）：**只动 `core/cmd/fake-adapter/**`，产品码零改动**
  （`git diff --stat HEAD -- core/cmd/fake-adapter/main.go` = **1 file changed, 32 insertions(+), 3 deletions(-)**；
  sha256 `fb6bc82e643f617a8024ee7d…`，634 行）
  - `main.go` 三处：`const envBadExecutionID`（+ 一段把三条限定写死的注释）、
    `const conflictingExecutionID = "0194f4c2-7c3e-7fff-8000-000000000bad"`、
    `fake.badExecID` 字段 + `run()` 里读一次环境 + `evidence()` 里那个唯一的 choke point `exec`（两条帧共用，并在开时喊一行 stderr）。
  - **新** `badexec_test.go`（156 行，`git diff --no-index --stat` = **156 insertions**；sha256 `ae1a4578e981e428412cfa4d…`）
    —— 4 条用例 + 两个夹具（`sendOnce` 返回"取证帧 + 子进程日志"、`(*fakeProcess).stop` 先等进程退出再读 stderr）。
  - 本轮**没碰** `internal/**`、`specs/**`（含 `specs/openapi/`，T-275 明写归 frontend/T-264）、`cmd/core/**`。
- 第 17 轮（09-24 10:01~10:07，T-263 导出只读解析）：
  - `core/internal/adapter/probe_store_sqlite.go`（**+36**，`git diff --stat HEAD` 实量；sha256 `60e63a8dc6f941de3d227f88…`）
    —— 新增 `(*SQLiteStore).ResolveInstance(ctx, adapterKey) (string, error)`：读 `d.Read`、只回 id、
    miss 回 `fmt.Errorf("resolve adapter %s: %w", …, db.ErrAggregateNotFound)`；未加写腿、`instanceRow` 保持私有、
    `ProbeStore` 接口未动。
  - `core/internal/adapter/probe_store_test.go`（**+67**）—— 三条用例 + 一个 `probeOnce` 小夹具（复用既有 `openMigrated`/`countRows`）。
  - 本轮**没碰** `core/internal/process/**`（`lifecycle.go` sha256 仍是 `f4903acedf72acd2a38ecffa…`，与 08:44 交账同值）、
    没碰 `specs/`、没碰 `cmd/core`。
- 第 15 轮（09-24 08:41~08:43，T-248 的 CO-49 动作）：`core/internal/process/lifecycle.go`
  —— `ownerTypeAdapterInstance` **导出为 `OwnerTypeAdapterInstance`**（+ 一段点明"词表见契约① §1.1 第 18 条 / 装配层只读它"的注释、
  `NewLifecycleStore` 里那句比较与错误文案改引用导出名）。**零行为变更**：值一字未改、比较逻辑未改，
  文件 233 → **236 行**，本包旧名 0 残留，`go build ./...` exit=0、`gofmt -l` 空、`internal/process` 仍 **32 PASS / 1 SKIP / 0 FAIL**。
  本轮**只动这一个文件**（`lifecycle_test.go` 与 `supervisor.go` 未再动）。
- 第 13 轮（09-24 07:0x~07:18，T-221 落写口）：
  - **新** `core/internal/process/lifecycle.go`（226 行）—— `ErrNoLifecycleRow`、`ProcessRow`、`LifecycleStore`
    + `NewLifecycleStore`（构造期校验）、`RecordProcessStart`（`CreateVersionless`）、`RecordProcessExit`/`RecordProcessCrash`
    （同一道 `takeEdge` 走 `TouchTransition`，guard = `landedStatus`）；这是本包**唯一**允许 import
    `platform/db`/`eventjournal` 的生产文件。
  - **新** `core/internal/process/lifecycle_test.go`（430 行）—— 8 条顶层用例 + 6 条子用例，全走真库
    （`db.Open` + `Migrate(../../migrations)`，`audit.Rules()` → `InitRedaction` 在前）。
  - 07:36 追补（T-227）：`lifecycle.go` 的 `landedStatus` 注释 **+7 行**（226 → 233），把"guard 只能用我自己写进去的状态"点名挂到**契约① §1.1 第 20 条 ⑤**，并写死"没有任何事件带 `StatusRunning`"那条前提；零行为变更（双向自证见 Verification 补记）。
  - `core/internal/process/supervisor.go`（**+9 / −7，纯注释**）—— "已裁，见契约① 第 20 条"、
    "platform/db **is** imported here (lifecycle.go only)"、以及那三道守卫/边界改由测试守。
- 第 12 轮（09-23 05:2x，T-211 收单）：**只有注释，零实现**
  `core/internal/process/supervisor.go`（`git diff --stat HEAD` = 1 file changed, **+29 / −17**；中途那次 +27/−17 已被后续两处注释修正取代）——
  删掉"§0 不许 internal/process import platform/db"这条**不存在的条款引用**、
  "row shapes stay the injector's business"（T-211 刚判错归属的那半句）、
  以及已被 05:22 读数作废的"registry 里没有 process kind"；换成实测现状 + `CreateVersionless` 仍 0 命中的准确边界。
- 第 10 轮（09-22，probe + 丢弃接缝 + T-132 两个 P0）：
  `core/internal/adapter/probe.go`（新）、`core/internal/adapter/probe_store_sqlite.go`（新）、
  `core/internal/adapter/probe_test.go`（新，6 条）、`core/internal/adapter/probe_store_test.go`（新，2 条真库）、
  `core/internal/adapter/purity_test.go`（**判据改写**：§6 规则 4 的"全包零非 stdlib import"收成
  "只有 `probe_store_sqlite.go` 与 `*_test.go` 可 import 白名单里的三个包，`database/sql`/`os/exec` 全文件仍禁，
  `internal/process` 任何文件都禁"，并加"allow-list 里的文件必须存在"的反向检查 —— 依测试自己原先写的
  "要放宽就连包名和允许它的条款一起写进来"）；
  `core/internal/process/supervisor.go`（`Recorder` 第四方法 `RecordDrop` + `DroppedEvent` +
  nil-safe 的 `recordDrop`）、`core/internal/process/connection.go`（丢弃点接上接缝 + `boundTo`；
  第一版在 nil `sup` 上取 `.Kind` 直接把 read 循环 panic 掉，已由 `TestDroppedFrameReachesTheAuditSeam` 与全仓跑兜住）、
  `core/internal/process/supervisor_test.go`（stub 加 `drops`）、`core/internal/process/connection_test.go`
  （`TestDroppedFrameReachesTheAuditSeam`）
- 第 8 轮（CO-40 收口 + §10 对拍）：`core/cmd/fake-adapter/readers_test.go`（`section10` 解析器 +
  `TestFakeAdapterSatisfiesContractSection10`，含两条"注入各自要验它那一行的行为"的子进程路径）、
  `core/cmd/fake-adapter/main.go` 与 `core/cmd/fake-adapter/service.go`（无行为改动，仅随上轮排空点收口的注释）、
  `core/cmd/fake-adapter/main_test.go` 与 `core/internal/process/connection_test.go`（未建探针注释改引 §4.0）
- 第 7 轮（CO-25 落地 + CO-34 注释收口）：`core/cmd/fake-adapter/main.go`（`finish` 成为唯一排空点、
  `startTurn` 尾部显式排空删除）、`core/cmd/fake-adapter/service.go`（`sessionStop` 里第二处 `drainQueue()`
  删除并写明理由）、`core/cmd/fake-adapter/readers_test.go`（`waitForTicket`/`framesWithTicket` 共日志等待器 +
  `TestFakeAdapterQueueDrainsAtTurnFinishedNotAtNextSend`，含位置无条件比对）、
  `core/internal/adapter/stream.go` 与 `core/internal/adapter/handshake.go`（两条失效注释改成实测现状）
- 第 6 轮 ③：`core/cmd/fake-adapter/service.go`（`methodSessionCreate` + `sessionCreate` + `store.created`
  幂等映射；`modelSelect` 改用 Core 侧类型与 `Validate`）、`core/cmd/fake-adapter/main.go`（dispatch 一列）、
  `core/cmd/fake-adapter/readers_test.go`（`TestFakeAdapterSessionCreateIsIdempotentByClientRequestID` +
  `TestFakeAdapterServesEveryContractMethod` + `section4`/`contractFile` 解析器）、
  `core/internal/adapter/send.go`（`ModelSelectParams`/`ModelSelectResult`/`Validate` 归位）、
  `core/internal/adapter/handshake.go` 与 `core/internal/adapter/contract_table_test.go`（REQ-T8 例外撤销、
  `session/create` 的门改成直接断言）、`core/cmd/fake-adapter/main_test.go` 与
  `core/internal/process/connection_test.go`（未建探针改指 `session/push`）
- 第 6 轮 ②（`Recorder` 合验 → 否定结论 + 冻结表互校）：`core/internal/process/schema_enums_test.go`（新）、
  `core/internal/process/supervisor.go`（`Recorder` 文档：AggregateKind 事实更正 + 装配期列映射 +
  Crashes 走 metadata_json；无行为改动）
- 第 6 轮 ①（`model/select`）：`core/cmd/fake-adapter/service.go`（`methodModelSelect` +
  `modelSelectParams`/`Result` + `modelSelect` + `fixtureModels()` 单一 offer 来源）、
  `core/cmd/fake-adapter/main.go`（dispatch 一列）、`core/cmd/fake-adapter/readers_test.go`
  （`TestFakeAdapterModelSelectKeepsRefusalsApart`）、`core/cmd/fake-adapter/main_test.go` 与
  `core/internal/process/connection_test.go`（"未实现"探针改指 `session/create`）
- 第 5 轮 C 段（§4.2 裁定落地）新增/改动：
  `core/internal/adapter/stream.go`（`CarriesTurnRef` 六条、生产者/消费者同谓词、`Dispatch` 冲突与保留句柄）、
  `core/internal/process/connection.go`（`attributeLocked` 只读绑定 + 交叉校验、删 `turnKey`）、
  `core/internal/adapter/stream_test.go`（3 条新用例 + 4 行判据反转）、
  `core/internal/process/connection_test.go`（`openTurn` 走真绑定 + 矛盾身份用例）、
  `core/cmd/fake-adapter/readers_test.go`（链用例改判"票号与 execution_id 必须成对且互相印证"）、
  `core/cmd/fake-adapter/delivery_test.go`（trace 里取证两帧带上 `@ticket`）
- `core/internal/adapter/contract_table_test.go` — 与契约② 的互校用例（T-83 ③）：§4 方法表 +
  §4.1 写集合两边对拍；解析面限定在 §4 那一节、解析为空即 fatal，不内嵌名单副本


- `.qoder/progress/adapter.md`（新建）— 本文件
- `core/internal/adapter/protocol.go` — 帧、§1 整数码表、`$/cancel`、Reader/Writer
- `core/internal/adapter/handshake.go` — 版本协商、capability 校验与有效能力、未握手/只读门、
  三态投递门、§12.11 写方法清单、§4.1 `HealthResult`
- `core/internal/adapter/stream.go` — §6 六条通知的解码/构造、§6.2 `Cursor`（幂等丢弃 + 续传游标）、
  §6.3/§6.4 `TurnTracker`
- `core/internal/adapter/quota.go` — §7 额度形状与 UNKNOWN≠0 校验
- `core/internal/adapter/send.go` — §5/§7.1 请求形状（含 `queue_strategy`）、§7.2 回复形状、
  §4.2 `session/create` 幂等形状
- `core/internal/adapter/{protocol,handshake,stream,quota,send,purity}_test.go` — 表驱动，包内 36 个测试函数
- `core/internal/adapter/id.go` — §1 的 UUIDv7 `request_id`（含 `Timestamp()` 反解）
- `core/internal/adapter/README.md` — 点分/斜杠两套拼法的唯一说明 + 引用状态
- `core/internal/process/supervisor.go` — 拉起、退避、崩溃预算、事件回调（不写库、不发事件）
- `core/internal/process/connection.go` — 协议 pump：握手门、pending 表、deadline→`$/cancel`、
  §6.2 去重、§6.3/§8 死亡路径（挂着的调用一律返回、未闭合 turn 转 UNKNOWN）
- `core/internal/process/{supervisor,connection}_test.go` — 13 个测试函数，含真子进程与真 `go build` 出来的 fake-adapter
- `core/cmd/fake-adapter/main.go` — FakeAdapter 的 C→A 面：握手/health/`session/send`（§5 三态 +
  `queue_strategy` + Idempotency-Key 幂等缓存 + CO-8 两段式证据 + §5 队列在下一轮 send 时排空）/
  `quota/read`（§7 可读与 UNKNOWN 两种真答案）/ §10 四种注入 / `$/cancel` 接收侧（§15.3 规则 5：
  确认是结果帧）。单 goroutine、无计时器，故用例不抖（`-count=3` 实测）
- `core/cmd/fake-adapter/{main,delivery}_test.go` — 11 个真进程测试（原 5 + 新 6）；`call()` 按 id 等回复
  且把路过的每一帧记进 `seen`，`trace()` 因此是**不跳帧**的定序判据（测试规矩第 8 条）
- `core/internal/adapter/protocol.go` — 新增 `CodeByName`（§15.1.1 名字↔数字的反向真值，供
  `debug/fail` 这类"只拿得到名字"的调用方使用）
- `core/internal/adapter/send.go` — `SendParams` 增 `execution_id` 回显字段（CO-8 的证据键；
  §4 的 send 载荷没这一项 → CO-24），可选、不新增校验
- `core/internal/process/connection.go` — `Connection` 的文档注释与代码对齐：reader goroutine 起于
  `Connect` 而非 `Initialize`（io.Pipe 无缓冲，等握手再起会死锁），握手前的帧按 §2 上报且不动游标
- 一次性产物（变异备份、临时脚本）只准落 `D:\Project\Lucero\.scratch\adapter\`，**`*.bak` 也算**
  （T-47 + T-52）；早先写在本行的 `%TEMP/adapter-mut/` 已作废，`/tmp` 里我那 7 个 `.bak`
  在 13:29~13:37 被别的角色通配删掉了（正是 T-47/T-52 要防的那类操作），我未搬未删、本轮零新增

- 第 4 轮 B 段（§6.5）落在同一批文件里的形状改动：`stream.go` 加 `TurnRef` 字段 + `CarriesTurnRef`
  单点判据（生产/消费两侧共用）+ 四条必带票号的解码与构造；`send.go` 的 `ExecutionID` 注释改口径；
  `connection.go` 加 `CallTurn`/`SinceSeq`/`lastByRef`/`execByRef`/`attributeLocked`/`turnKey`，
  `cursor` 退回连接级；`cmd/fake-adapter/main.go` 的 `turn` 多一个 `ref`、`notice` 按判据发票号、
  `startTurn` 改打断语义（被打断轮 `INTERRUPTED`）；三份测试跟着改定序与归属断言

- `core/cmd/fake-adapter/service.go` — §4 的读面：session/list 与 session/read（游标 + remote_revision）、
  session/stop（真闭合在飞那轮，没得停就报 false + reason）、model/list、approval/list、approval/respond
  （幂等 + 不覆盖已定结论）、locate 的 CAPABILITY_UNAVAILABLE 桩；夹具编译进来，不读盘（CO-30）
- `core/cmd/fake-adapter/readers_test.go` — 6 个新用例：分页、审批幂等、stop 的两半、§10 ① 六类通知、
  §4.2 的链与边界（两帧不带票号、输出帧不带 execution_id）、locate 的诚实桩
- `core/internal/adapter/stream.go` — §4.2 的两处结论落地：`CarriesTurnRef` 只含四条、
  `turn/dispatched` 放开 Provider 句柄（CO-13）、跟踪器同口径；注释与 §4.2 载荷表对齐
## Verification evidence

- **第 38 轮（10-01 18:33~19:37，T-441：`approval/request` 从契约 §4:91 落到真管线）**
  - **动手前按字复核的三处**（主控那句「别自己加字段，回来提 REQ」与红线，我先量盘再落笔）：
    `git diff --numstat HEAD -- specs/ docs/` = **0 行**（两份契约我不动）；`git status --porcelain` 里我名下只有
    8 件 ` M` + 1 件 `??`，`cmd/core`、`internal/{permission,approval,execution}` 一字未动；
    `internal/permission` 只出现在一枚 `_test.go` 的 import 里（`internal/adapter` 的 `purity_test.go` 不许它的
    产品码导入任何 Core 内部包，这条我照着把校验放在**双**那侧，而不是放在协议层）。
  - **「第七条」这件事的写法**：契约正文那句仍是「全部六条 + 计数注记」，主控明确说「你动代码时也别按那句计数来」
    ⇒ 我没有在任何判据里写 6 或 7，判据**按族判**（`CarriesTurnRef` 的 switch + 契约表的 A→C 行）。
    本轮把我三包里的 `all six` 措辞清掉（现读 `grep -rn "all six" internal/adapter/ internal/process/ cmd/fake-adapter/` = **0**），
    只剩两处 `六条` 且两处都在解释「别按那句数」。守卫本体也加了第三个解析源：§4 方法表的 `**A→C**` 方向列，
    少它就把契约里的一帧判成"我发明的"（a10 就是打这一格，FIRED 时守卫自己 `t.Fatalf`）。
  - **11 枚变异判决**（`mut-r38.py` 基线 = 153 PASS / 0 FAIL / rc=0，锚点命中全 = 1，逐枚输出与基线非逐字相同）：
    a1 第七条离开 `CarriesTurnRef` ⇒ **FIRED 3 枚**（契约守卫 + 生产者拒绝表 + 线上用例）；
    a2 缺 `action` 不拒 ⇒ **FIRED**（取证表那一行）；a3 连 case 一起删（方法被判 unknown）⇒ **FIRED 6 枚**（跨三包）；
    a4 泵在闸门处结案（`applyTurn` 加一个 `Finish("CANCELLED")`）⇒ **FIRED**（新用例）；
    a5 泵不按 (turn_ref, sequence_no) 去重 ⇒ 批内 **FIRED**，但**只打中 `TestConnectionDedupsAndTracksTurn`、没打中我的新用例**
    （循环读满 3 帧就停，第四帧留在通道里没人断）⇒ 补一条「测量过的缺席」（再等 300ms 不得有第四帧），
    **19:19:33 a5b 复跑 FIRED** 并把那第二帧原文打印出来——这条按第 42 条口径记：**批内 FIRED ≠ 每枚判据都打中自己那一格**；
    a6 双不发（`approvalGate` 调用摘掉）⇒ **FIRED 3 枚**（含 §10 家庭判据那条，它这一轮也从「只数六条」变成罩第七条）；
    a7 停闸门那一轮自己 `turn/finished` ⇒ **FIRED 2 枚**；a8 `action` 换成分类器不认的串 ⇒ **FIRED 2 枚**
    （词表用例 + 线上用例，**主控点名的「因意外而通过」这一档有牙**）；a9 只问不停（删 `t.awaiting = true`）⇒ **FIRED 2 枚**；
    a10 守卫失去 §4 那个解析源 ⇒ **FIRED**（守卫自己 Fatalf，不静默少一条）。
    四文件还原 PRE=POST：`stream.go aa34793521fa55ca`、`connection.go d00554855f3a6d3d`、
    `main.go beb9417de21be9a5`、`contract_table_test.go 7dfd00f44d043a2f`；`grep -rn ZMUT core/` = 0；CR = 0。
  - **两面读数**（计数只从带 `-v` 的那趟取）：A 面 `-count=3` = **459 PASS / 6 SKIP / 0 FAIL、三包 ok**
    （459 = 153×3，153 = 第 37 轮 149 + 本轮 4 枚）；B 面 `./...` = **17 包 ok + `core/test` 1 包红**，
    **「全」= 18 包**。B 面那两枚红的原文与归属：
    ① `TestWiringSurfaceHasCallSites` 报 5 格零调用点（`permission.Engine.Decide`／`CheckTemporaryGrant`／
      `approval.Service.Request`／`execution.ApprovalStates.BlockingState`／`permission.RiskLevel`）——
      **契约 §3.0-c 自己写着**「本笔入册时它在 HEAD 上报 4 枚红……每一枚都有归属（全在 T-430 那四格里）」，
      而 `RiskLevel` 那格是主控 18:2x 亲手登表的，原文给了转绿条件：**「T-442 落地 ⇒ 这一格转绿」**、
      **「谁都不许为了让它绿而改这张表」**。⇒ 这 5 格是 backend T-430/T-442 的闸门，**不是我这一帧的欠账**：
      我要是让 `permission.RiskLevel` 在自己包里出现，反而同时踩 `purity_test.go` 和那句裁定。
    ② `TestEventProducerRosterMatchesDerivedSet`：名单登记 `execution_approval_requested`／`execution_approval_resolved`
      「无生产者」而扫描在 `internal/execution/{recovery,service}.go` 找到发射位（判据原文：「两边都得有人改」）⇒
      execution 域 + `specs/**` 那两侧的事，**我三包不产任何事件**（本轮我的改动里 `EventDraft` 新增 = 0）。
      点名我名下三包的 FAIL 行 = **0**（`grep` 全文计数已跑）。**我不修、不消红、也不宣布关闭**，只上交。
  - **我这一半的坐标**（主控要跟 backend 的 T-442 对表； patrol 那句「谁先交都算未闭」按字处理）：
    帧 = `core/internal/adapter/stream.go:44`（`MethodApprovalRequest`）、`:15-22`（归族）、`:84-86`（载荷三字段）、
    `:196-202`（缺 `action` 即拒）；生产者 = `core/cmd/fake-adapter/main.go:510`（`approvalGate` 的调用点）、
    `:526-529`（`riskyTools` 那份分类器词表）、`:538-575`（发 `stream/item` + `approval/request` + 落 PENDING 行）、
    `:587-592`（`settle` 见闸门不写终态）；泵 = `core/internal/process/connection.go:630-646`
    （`applyTurn` 的 switch **没有** `approval/request` 那一格，这是交付形状）；
    判据 = `internal/adapter/stream_test.go`（往返 + 生产者拒绝 + 取证表 4 行）、
    `internal/process/connection_test.go:704-795`、`cmd/fake-adapter/approval_request_test.go`（3 枚）、
    `cmd/fake-adapter/readers_test.go:873-882`。**backend 那一半要接的动作**（`Decide` → `approval.Service.Request`
    → 落 `PENDING` → 面板 `:resolve` → C→A `approval/respond`）我一行都没替它写。
  - **两处自己的错，当场认**：① 线上用例第一版把 `trace` 的下标当成「通知序列」的下标用（`trace` 含响应帧），
    跑出一枚 `index out of range [7] with length 7` 的 panic——判据自身出错的样子，不是产品码错；
    ② 同一枚用例断 `"method":"approval/request"` 出现在 `request.Params` 里——方法名在信封上、不在载荷里，
    这句永远断不到，改成断 `request.Method` 常量 + 载荷原始字节两半。两处都是**判据面**的错，跑出来才看得见。

- **第 37 轮（10-01 15:55~16:04，T-436：把「目标早不在」那一层装进 `Stop` 本体）**
  - **三条问句各自答什么，是这一格的全部难点**（我先摆这个，再摆修法，因为第 19 条那次自纠就是这么来的）：
    `Kill` 答「这个句柄还允许不允许 Terminate」；`ChildAlreadyGone(err, pid)` 答「这个 pid 还归不归我」；
    `Wait` 答「退出状态拿到没有」。flake 的组合是**进程已死、句柄未 release** ⇒ `Kill` 说 `Access is denied`、
    `ChildAlreadyGone` 说「还归我」（`FindProcess` 成功，`os.ErrProcessDone` 也没有）、`Reaped()` 说「没人排空过」——
    **三道旧判据同时答错**，所以修法只能是「把返回值改成问对那个问题」：Stop 的成功判据 = 状态收到，
    `Kill` 失败只是中途观察。**我没有去分类 errno**：那是把 OS 的措辞当契约，Windows 一句、下次换一句。
  - **正例的确定性不靠 sleep、也不靠概率**：`helperLauncher("crash")` 起一个立刻 `os.Exit(2)` 的孩子，
    然后 `io.Copy(io.Discard, child.Stderr)` 到 EOF——写端随进程消失，这一句就是「它已经死了」的同步点；
    此时 `done` 里躺着 reaper 的结果、`Reaped()` 仍为 false，与 backend 那一跑的形状一致。
    **s1 反证证明这条判据真的走的就是那个分支**：把 T-436 之前的写法搬回去 ⇒ 两枚用例红，
    其中 `supervisor_test.go:397` 打印出 `cleanup Stop: kill: TerminateProcess: Access is denied.`
    ——**主控给的 OS 错原文在我包里 100% 复现**（backend 那 1/15 是同一分支的计时敏感版本）。
  - **s3 SILENT 我不盖**：「`Kill` 的错从此不报」这一枚没有任何用例能抓，理由是
    **`Kill 失败` 且 `状态收不到` 对自己 spawn 的孩子不可构造**（活着 ⇒ Kill 必成；死了 ⇒ Wait 必答）。
    所以我不加一条靠 `if true` 才能红的假判据，也不宣称"三种消红都被挡住"——
    可构造的两档各有用例：吞掉不收状态 = s2（FIRED 四枚，含 `TestCloseReapsTheChild`），
    活孩子报成功 = `TestStopStillReportsAChildItCouldNotStop`（在 s1/s2 下都红）。
  - **与 patrol 转的那句「别与 `Child.Wait` 的 `waitMu` 拧着做」的关系**：修完 `Stop` 仍然只调**一次**有界 `Wait`
    （和以前一样），只是过去在 `Kill` 失败时提前 return、现在多走一条到 `Wait` 的路 ⇒ 没有新增嵌套持锁、
    也没有绕开那个锁。backend 自己那半（N-f）我没看也没碰。
  - **读数与集合**：默认面 `-count=3`（`faceA-r37-count3.log`）= **447 PASS / 6 SKIP / 0 FAIL、三包 ok**
    （447 = 149×3；149 = 第 36 轮 147 + 本轮 2 枚）。backend 那一包 `./cmd/core/` `-count=3` = `ok`、59.158s、
    日志里 `Access is denied` **0 次**——按第 36 条口径这句只是"没复发"，**1/15 的概率 3 趟抓不到**，
    所以关闭权在 backend 的复跑读数，不在我这里。基线（变异批那次）两包 146 PASS / 0 FAIL。
    未入册状态我自己复量了：`git ls-files --error-unmatch` 与 `git status --porcelain` 现读 ⇒
    第 35/36 轮那三件仍是 `??` + ` M`，第 37 轮再加两件 ⇒ 我名下**五件在工作树里**，
    「T-431 已入册」那句与盘不符（我账上也没有 T-431 这个号），已在 CO-62 上交。

- **第 36 轮（10-01 13:23~13:29，T-423 直投两件同步 + 新计数口径落地）· 承接第 35 轮，不覆盖它**
  - **新口径第一天就抓到东西，抓到的是我自己的**：`-count=3` 下 440 PASS / 6 SKIP / **1 FAIL**，
    红的是我第 31 轮的三态用例最后那一腿（`delivery_test.go:509`，报 `the dropped code was never reported`），
    而它 `-count=1` 从来没红过——**这正是「一次不复现≠已修好」的反面：一次绿也不等于没病**。
  - **成因不是产品码，是跨流**：那条断言在读 `p.stderr`，而 os/exec 用**自己的协程**拷孩子进程的 stderr；
    回复帧走 stdout、日志走 stderr，两者之间**没有顺序保证**，所以活进程期间读那个 buffer 可以正好丢掉被断言的那一行。
    这条道理**本包早就写着**：`badexec_test.go:28` 的注释原文是「os/exec copies a child's stderr in its own goroutine,
    and reading that buffer while the process is alive races - it can lose exactly the line being asserted」，
    并给了正解 `stop()`（先 `stdin.Close()` + `cmd.Wait()` 再取字符串）。**我第 31 轮没照它办**，这轮按既有惯例改回三处，
    不新增机械。教训不是"stderr 不可靠"，而是：**同一个包里的判据形状本身就是判据的一部分，写过的正解要对齐**。
  - **搬迁不等于修好——用三枚变异证明它还咬着**（`mut-r36.py`，基线两包 83 PASS / 0 FAIL，每枚前有本包预检）：

    | 变异 | 判决 | 读数 | 证的是哪条搬迁后的判据 |
    |---|---|---|---|
    | d4-no-dropped-code-log | **FIRED** | 82 PASS / 1 FAIL（三态用例） | 解除参数被丢弃必须喊出来（搬到 stop() 之后仍在读同一份日志，只是不再丢行） |
    | d5-no-hang-log | **FIRED** | 82 PASS / 1 FAIL（hang 用例） | §10 的 hang 场景在 stderr 上留痕这条 |
    | d6-no-startup-log | **FIRED** | 82 PASS / 1 FAIL（握手前那枚 §1 判据） | stdout 只放帧、启动行进 stderr 这条分界 |

    复跑两组：那四条用例（三枚搬迁过的 + 第 35 轮新判据）`-count=6` = **24 RUN / 24 PASS / 0 FAIL**；
    默认面全量 `-count=3` = **441 PASS / 6 SKIP / 0 FAIL、三包 ok**（441 = 147×3；RUN=447 = PASS+SKIP）。
    **`-count=6`/`-count=3` 的 0 红不是"修好了"的证据，结构性搬迁 + 三枚 FIRED 才是**——采样只用来证明没有当场回归。
  - **旗标面对我不成立（按事实报，不造一面）**：`grep -rn "os.Getenv" --include=*.go internal/process internal/adapter cmd/fake-adapter`
    只有 `LUCERO_HELPER`、`FAKE_ADAPTER_HELPER`（把测试二进制当替身进程重跑的机制）与 `envBadExecutionID`
    （由用例自己 `t.Setenv` 开的 T-275 注入门，见 `badexec_test.go:42`）⇒ **没有一面需要外部旗标才会跑**，
    所以「旗标面与默认面各 `-count=3`」在我这里就是默认面 ×3（已跑）。
  - **主控那句「第 32/33 轮已随 `48c2547` 入册」我核了盘，差一枚提交**（不是质疑"已入册"，那句是真的）：
    `git log -1 --format="%h %ci" -- <路径>` 对 `internal/process/{attach_test.go,supervisor.go,connection.go}`、
    `internal/adapter/{health_store_test.go,probe_store_sqlite.go}` 五件**全部指 `2c65177`**（09-30 19:57:03，
    「三处同改一次入库」），`git show --name-only 48c2547`（10-01 00:38:48「四处同改 + T-414」）里**没有我这五件**；
    `git merge-base --is-ancestor 2c65177 48c2547` = 真（48 在后）。顺带说一句：主控自己在契约里写的
    「泵自 `2c65177`、期限与排空自 `48c2547`」与盘上一致，所以只有本封那句"两件同步"里的号差一枚，改一下就行。
  - **§6 三处同改那条我收下并记进 CO-61**：以后我这边任何**新事件类型**（我这轮没有）都必须与
    §6 名单（主控）、`specs/openapi/core-v1.yaml`（frontend）、常量+目录（db/execution）**同一笔提交**走，
    拆开必红——这也是为什么格二我不动名单那一行：动它就不是我一个人的提交。

- **第 35 轮（10-01 12:57~13:12，T-423：格一落地、格二记账、格三出拟稿）· 承接第 34 轮那三条**
  - **格一为什么必须"从源码读"而不是"手抄一份名单"**：这一格要守的就是"第二真相"问题——h4 那次我摘掉声明表里的一行，
    没有任何一条用例发现，因为**没有人在比对声明与实现**。如果新判据里再手抄一份方法名单，它就变成一个会随时间漂移的
    第二真相，跟它要守的东西一模一样。所以：`servedMethods` 解析 `cmd/fake-adapter/main.go` 里 `(*fake).serve` 的
    dispatch（`case X:` 与 switch 前那一支 `req.Method == adapter.CancelMethod`），符号值再从两个包的非测试源码里
    按 const 行解出来。锚点找不到 ⇒ `t.Fatal` 说"是解析器过期了，不是这台空了"（`section4` 那枚既有解析判据用的就是
    这个形状，我照抄它的纪律而不是照抄它的数据）。
  - **当场抓住的两处我自己的错**（都留下了原文，因为它们是这一格的强度来源）：
    ① 符号表第一版正则要求行首一个 TAB，于是漏掉了 `internal/adapter/protocol.go:27` 的
    `const CancelMethod = "$/cancel"`（行首单行常量），判据**以 `t.Fatal` 暴露**而不是以绿暴露：
    `pre-switch arm names "adapter.CancelMethod", and no const ... resolves it`（`declaration_test.go:109`）。
    改成 `^\t?(?:const )?...` 后才过。**如果第一版是"跳过未解析项"而不是喊出来，这条判据就已经是假的了。**
    ② 我加日志时把 `t.Logf` 插了两遍（第二次抓到后才删）——同一轮的指纹因此有两枚，交付值认 13:11:52 那枚。
  - **三枚集合大小的现读**（写进日志也写进这里，下一轮可比）：**served=19、declared=16、effective=15、例外=3**。
    `effective` 比 `declared` 少的那一枚是 `session.locate`：它接、答 `CAPABILITY_UNAVAILABLE`（`service.go:468-472`）、
    声明 UNSUPPORTED ⇒ **B 向通过（有声明）、A 向不参与（不是有效）**。这就是第 34 轮我预告「要列成允许的例外」的那个形状，
    实际不需要例外表——两条臂各自看正确的集合，就已经不误伤它。
  - **变异批（`mut-r35.py`，13:10:33→13:11，两包 = `cmd/fake-adapter` + `internal/process`）**：
    基线 **83 PASS / 0 FAIL**；三枚锚点命中全 = 1；**3/3 FIRED**；两文件按字节还原且 sha 与 PRE 同值；ZMUT 0。

    | 变异 | 判决 | 读数 | 盯的是哪条臂 |
    |---|---|---|---|
    | d1-declaration-line-gone | **FIRED** | 82 PASS / **1 FAIL**（只红新判据） | **h4 原样**：声明少一行、方法仍可达 ⇒ B 向今天有牙了（第 30 轮那一枚 SILENT 就是这一档） |
    | d2-case-dispatched-gone | **FIRED** | 73 PASS / 10 FAIL | A 向（声明有效却没接），并连带把交错判据一起拽红——说明这份 dispatch 确实是消费者在读的那张 |
    | d3-stale-exception | **FIRED** | 82 PASS / 1 FAIL | C 向：例外表里留一条不再服务的方法就红，例外不许攒成挡箭牌 |
  - **格二 · 记账一句**：主控裁「v0.1 不发 `adapter_downgraded`」并把我的两条机械理由写进契约 ⇒
    我从今天起**不再把它当欠账报**（此前每轮 Next action 里那句"等 §22 点名"按这裁定作废）；
    名单那一行我一个字没动（现读 `specs/events/event-contract-v0.1.md:315` 那一整行仍含 `adapter_downgraded`、21 条）。
    以后若有人要发这事件，按主控本封的话：**先定义「什么算降级」再走 §6 三处同改**，别拿"面板想看"当理由。
  - **B 面归属（本轮唯一的外部红，我不认领也不修）**：13:07:14→13:07:38 `ALL_RC=1`、16 包 ok、两枚红
    （`api/http` 的 `GET /output` 501/404、`core/test` 的名册 `execution_output_flushed`），
    落点分别是 `declared_not_wired_test.go:147` 与 `event_producer_roster_test.go:1096`，
    同源都是 backend 本批在途的 `internal/execution/output.go`（未跟踪新文件）⇒ 与我的判据无涉；
    我这面三包在全量跑里仍是 `ok`，整份日志点到我名下文件的 FAIL 行 **0**。

- **第 34 轮（10-01 12:23~12:3x，T-419：CO-55 / 能力降级发射点 / §10 注入表 —— 每格三条，零代码）**
  主控的原话是「不要直接抛编号给我，我按编号猜语义就会写出假判据」，所以这三格每条断言都自带盘上落点；
  落点全是本轮 `grep -n` 现读的行号（HEAD `1cca6fe`），不是任何上一轮记忆里的行号。

  - **格一 · 要裁的事**：**Core 把一帧发给 Adapter 之前，要不要先查那张能力表？**
    现状（三条）：`internal/adapter/handshake.go:342-368` 的 `Call` 只查 §2（握过手）与 §3（`write_entry=false` 的写方法要有
    `UI_AUTOMATION` 兜底）；`internal/process/connection.go:213-218` 的 `gate` 只是转调 `Call`；能力表 `Effective()`
    （`handshake.go:319-325`）今天被 §5 的投递核定用，**不被"能不能调这个方法"用**。
    选项与各自关掉/打开哪一格：**甲**＝不动运行时，只补一条**双向**判据（声明为有效的能力这台 Adapter 接不接 / 接了的方法
    声明表里有没有 ⇒ 关掉「声明可以撒谎」这格；打开 0 格）。注意判据第一天就要把 `session/locate` 这类「服务但声明
    UNSUPPORTED」列为**允许的例外**，否则一开就红。**乙**＝Core 发帧前本地拒（关掉「白跑一趟」这格；打开两格：
    表过期时拦死合法调用——今天没有任何重探机制会让表过期，以及 §15.2 需要一个新错误码行，那是契约改动）。
    **丙**＝只拒「自己声明过 UNSUPPORTED」那类，看着最保守，**但实测会换掉 §4 指定的答案**：`session/locate` 今天由
    Adapter 答 `CAPABILITY_UNAVAILABLE`（`cmd/fake-adapter/service.go:468-472`，注释还明写这是 §4 认为对的答案），
    拦在它前面就是把协议答案改成 Core 的本地拒。**推荐甲**：§9 的真闸门已在声明侧（`handshake.go:244`），
    运行时再拦只剩「少一次往返」的收益，而代价是动契约错误码表；h4 那次 SILENT（摘掉 `debug.hold` 的声明行，
    替身照旧服务 `debug/hold`）是这一格今天唯一实测到的洞，甲正好钉住它。
  - **格二 · 要裁的事**：**「一次能力降级」在 v0.1 有没有能装下它的写点？**（先把这句话裁出来，「发不发」是它的推论）
    两条机械事实：① `adapter_downgraded` 的 `aggregate_type` 是 `adapter`（`event-contract-v0.1.md:186` 列它属 adapter，
    payload 四把键 `capability_name`/`from_backend`/`to_backend`/`reason`），而 `platform/db` 的守卫要求事件聚合等于被
    Touch 的聚合——报错原文格式串在 `internal/platform/db/touch.go:571`（`event %s: aggregate_type %q but touching %q`）；
    「降级」发生在 `adapter_capabilities` 那一行（另一个聚合）⇒ **从能力那条腿发它在机械上不可达**（同族现场证据：
    `internal/session` 的 `TestAdapterDowngradedCannotRideASessionTouch`）。② `resolveEffective`
    （`handshake.go:197-219`）只做**包含/剔除**（声明了 + 不是 UNSUPPORTED + 依赖齐 ⇒ 有效），
    **没有任何代码把一个能力的 `effective_backend` 从 NATIVE 改成 CLI** ⇒ `from_backend`/`to_backend` 今天**没有定义域**。
    选项：**甲**＝v0.1 不发，名单里那句「有名字无生产者」就按主控今天的说法读成**有意 idle**（零代码，关掉「为发事件
    而 Touch 一次实例行」的灰面；打开的是 §15.1 那句「降级是运行时可观测的事件」在 v0.1 无实现，要明写在契约里）。
    **乙**＝事件骑在 `RecordProbe` 对 `adapter_instances` 的那笔 Touch 上（健康变化时已经有这笔，`probe_store_sqlite.go:148`），
    「核定结果与库里那一行不一样」时同笔带 `adapter_downgraded`（关掉「事件永远是空名」这格；新打开一格：
    健康没变、只有能力变时也必须 Touch 一次实例行——只为带事件——需要主控认一句「事件载体的 Touch 允许只动 `updated_at`」）。
    **丙**＝先定义降级（把「declared 但被剔除」算降级，则 `to_backend` 填什么要先裁；把「后端被替换」算降级，
    则要先有做替换的代码，而它不存在）。**推荐：先裁丙的前半句，再在甲/乙里选**；
    若主控只想收口，甲是诚实答案。**我不在无定义域时建发射器**——第 33 轮那条「没有裁定依据的话我不写成规则」是同一条纪律。
  - **格三 · 要裁的事**：**第五枚注入端点 `debug/hold` 要不要出现在 §10 那张故障注入表里？**
    现状：表只有四行——`adapter-protocol-v0.1.md:376-379` 是 `debug/fail`/`debug/crash`/`debug/hang`/`debug/unknown_quota`；
    `grep -n 'debug/hold' specs/adapter-protocol/adapter-protocol-v0.1.md` = **0 命中**（本轮现读）。
    而它已经是三条判据的仪器：REQ-TURN-1 判据 (a) 的迟到收尾、§6 规则 5 的两轮交错、第 31 轮的三态分辨（中间那一态
    靠解除侧的可选 `code`，默认 `SUCCEEDED`、挂在 `on:true` 上直接拒）。
    选项：**甲**＝补一行（`方法 / 行为 / 参数 / 期望 / 只许 adapter_key=fake + EXPERIMENTAL`，并写「收尾帧落在 send 回执之后」）
    ——关掉「读 §10 会以为注入面只有四枚」这格；打开的只是主控自己的一行文档。**乙**＝不进表，作为替身私有工具、
    判据里指到代码——保住「§10 只描述契约要求的注入」这层含义，但**打开更坏的一格**：三条判据的仪器在契约里不可见，
    别人复现只能读我的代码。**推荐甲**，措辞限定「这是替身的注入端点，不描述任何真 Adapter」；拟稿我可以给，
    `specs/**` 归主控，我不写。
  - **这轮没做的事**：没跑测试（本轮不产码，第 33 轮那两面读数仍是交付字节上的最后一次）、没改 `specs/**`、
    没碰 `cmd/core`、没自己把 `adapter_downgraded` 从名单里挪（那是主控的裁定项，巡检也提醒过）。
- **第 33 轮（09-30 17:24~17:41，T-402：`adapter_instances.health` 的写口归我）· 不与第 32 轮合批**
  - **主控那句「非测试码零写者」我复跑过，并且它只在一种口径下成立**（两种口径都留命令原文）：
    `grep -rn -e 'INSERT INTO adapter_instances' -e 'UPDATE adapter_instances' --include='*.go' core | wc -l` = **25**（巡检同值），
    这 25 行**行号全在 `_test.go`** ⇒ 字面写 SQL 的生产代码确实 0 处。**但同一张表的写口不是 0**：
    `core/internal/adapter/probe_store_sqlite.go:227`（`MarkUnverified`）**今天就在写** `health`、`last_healthcheck_at`、
    `updated_at`、`metadata_json`，只是走 `tx.Touch(db.AggAdapter, …)` 拼出来的列名，不是字面 `UPDATE adapter_instances`。
    ⇒ 结论按盘上事实写：**这张表已经有健康写口、已经在 `internal/adapter`，缺的是"泵能调的那扇门"**；
    我没有另起第二个写手，而是把两条门合到同一个 `healthWrite`（同笔三列只组装一次）。
    引用这条时请带上口径：**「字面 INSERT/UPDATE 数」≠「写口数」**（巡检也提醒过"提及数 24"是第三种数）。
  - **门为什么长这样**（`(*SQLiteStore).DowngradeHealth(ctx, adapterKey, to) (bool, error)`）：
    ① 按 `adapter_key` 走，与唯一读点同形（`probe_store_sqlite.go:61` = `SELECT id, health FROM adapter_instances WHERE adapter_key = ?`）；
    ② 三列同笔（① 那句"别只改一列"），h2/h3 各摘掉一列都红；
    ③ 事件用 §6 给的那三把键，**没有第四个参数**——`event-contract-v0.1.md:185` 只列 `from`/`to`/`confidence`，
      往签名上塞一个 `reason` 就是把契约改动偷运进接口（探针那条门的 reason 走 `metadata_json`，那是 §7.2 已有的形状，我没改它）；
    ④ 同状态 = `(false, nil)`：泵每次重连都撞一次「已经在这里了」很正常，把它做成错误会让调用方去写 `if err != nil { 记一条红 }`；
    ⑤ 表里没有这台 = 错误里保住 `db.ErrAggregateNotFound`（h6 摘掉 `%w` 就红），**泵不许把"连接丢了"变成"注册了一台 Adapter"**——
      注册是探针的动作（`RecordProbe`），这条边界比签名好看更重要。
  - **方向表为什么闭**：只收 `HEALTHY→DEGRADED`、`HEALTHY→UNAVAILABLE`、`DEGRADED→UNAVAILABLE`、`UNKNOWN→UNAVAILABLE`。
    抬健康一律拒（那是探针的证据，不是"没听到死亡信号"的证明）、`→UNKNOWN` 一律拒（`openapi` 里
    "UNKNOWN 不得折成 UNAVAILABLE"是同族禁令的另一面：UNKNOWN 是**没有证据**，不是一个可选状态）。
    还有一格差异我写进代码注释而不是猜：**列的 CHECK 有六个值**（`000001:93-96` 还有 `STARTING`/`STOPPED`），
    而协议枚举 `healthValues`（`handshake.go:69-72`、`:84`）只有四个 ⇒ 这张表**永远不该出现**那两值，
    真出现了这扇门拒绝处理它（`TestDowngradeHealthFromAnUnprovedRowOnlyAcceptsFailure` 用夹具插了一行 STOPPED 把这条钉住），
    而不是顺手编一条规则。**主控如要把 STARTING/STOPPED 变成 Core 写的状态，那是契约面的一格，不归我这轮。**
  - **`MarkUnverified` 的合并是本轮唯一动到既有行为的地方**，所以给它配了回归位：h7（把 `reason != ""` 那一支摘掉 ⇒
    探针那条门丢 `metadata_json`）**FIRED 在 `TestBothHealthDoorsWriteTheSameTrio`**；既有探针用例（322 条基线里）无一转红。
  - **变异批 v2（`mut-r33b.py`，17:37:24→17:41:01，三包 = `internal/adapter` + `cmd/core` + `api/http`）**：
    基线 **322 PASS / 0 FAIL**；八枚锚点命中全 = 1；**8/8 FIRED**；每枚输出与基线两两不同；
    还原 PRE=POST `3c9ee50b81cd433e`、ZMUT 0、CR 0。

    | 变异 | 判决 | 读数 | 这一枚在证什么 |
    |---|---|---|---|
    | h1-door-removed | **FIRED** | PASS=316 **FAIL=6** | **主控点名的那条**：摘掉写口 ⇒ 降健康一律红（红面最宽的一枚） |
    | h2-loses-updated-at | **FIRED** | PASS=319 FAIL=3 | ① 那句「同笔把 `updated_at` 一起走」 |
    | h3-loses-check-time | **FIRED** | PASS=319 FAIL=3 | 同笔里的 `last_healthcheck_at` |
    | h4-direction-table-open | **FIRED** | PASS=317 FAIL=5 | 方向表放开 ⇒ 泵可以把健康抬回去 |
    | h5-same-state-becomes-a-write | **FIRED** | PASS=319 FAIL=3 | 同状态写成一次变化（§10「别把没变化写成变化」） |
    | h6-missing-row-stops-naming-itself | **FIRED** | PASS=321 FAIL=1 | `errors.Is(…, db.ErrAggregateNotFound)` 这条契约 |
    | h7-probe-door-loses-its-reason | **FIRED** | PASS=321 FAIL=1 | 我这次合并有没有吃掉探针那半的 reason（**本轮唯一动旧行为的地方**） |
    | h8-event-loses-confidence | **FIRED** | PASS=321 FAIL=1 | §6 三把键少一把就是契约面被改 |
  - **v1 那三枚 BUILD-BROKEN 不作数，原文留着**：17:35 的批次里 h5/h6/h8 三包全 `FAIL [build failed]`，
    报错是 `internal/eventjournal/events.go:51:2: ExecutionUnknown redeclared in this block`（**别家此刻在改 `eventjournal`/`execution`**，
    与我的锚点无关；`mut-r33-h5-same-state-becomes-a-write.log` 头三行就是这条）。
    ⇒ 这一批的规矩改成：**每枚动手前先跑一次基线预检**（只跑我这包，秒级），预检不绿就记 VOID 不记判决。
    教训按"变异批必须自带被验树可编译的证明"入册，不写进代码注释。
  - **两面读数与集合口径**：A 面 **146 PASS / 2 SKIP / 0 FAIL**（三包；第 32 轮是 135，本轮 +6 顶层 +5 子用例）；
    B 面 17:41:12→17:41:34 **18 包全 ok、`ALL_RC=0`、0 FAIL**（含 `core/test`，口径同第 32 轮，不与第 31 轮的"17 包"对拍）。
    本轮的 `cmd/core` 与 `api/http` 都在变异批里被跑过（它们**直接持有 `*adapter.SQLiteStore`**，`main.go:76`），
    所以"我没把泵碰红"这句话有两面证据，不是一次 `go build`。

- **第 32 轮（09-30 15:56~16:51，T-398 三件）· 承接第 31 轮补（T-397），不覆盖它**
  - **① 为什么闸门只挡一个方向，我没有假装挡两头**：`Run` 的循环把手上的 `*Child` 留在里面（`supervisor.go:387` 起孩子、
    `:391` Wait），外面**没有任何句柄**能拿到那台孩子 ⇒ "Connect 撞 Run"这一向今天挡不了（挡了就是把 backend 唯一
    能用的形状也锁死）。所以我做的是：① 给"从被监督的孩子构造连接"开口（`NewConnection` + `Child.sup` 包内字段证明
    归属，伪造不出来）；② 把能挡的那一向挡死并让它可测（`ladderMayStart`）。剩下的"每起一台交回一个连接"那一步写成
    **CO-57 的问题**交回主控，不伪装成实现。这条自约束写在 `NewConnection` 的注释里，代码与注释同向。
  - **reaped 逃逸口是变异查出来的，不是我想到要修的**：第一版 `ladderMayStart` 写成
    `s.last.conn != nil && !s.last.Reaped()`，a4/a5（把 `release` 掏空 / 让 `Close` 不放）**只红了一枚用例**——
    `TestRunIsAllowedAgainOnceTheConnectionIsClosed` 照样绿，因为 `Close` 会回收孩子，"已 reaped"这条腿替它答了案。
    ⇒ 条件收成"`conn != nil` 就拒，唯一释放点是 Close"，a4/a5 从 FAIL=1 变 **FAIL=2**（那枚用例真的在看释放）。
    这一格记两条：① **一条断言在"另一道闸也会给出同样答案"时不算证**（同族教训我写过，这次是自己踩）；
    ② 收紧后的语义也更对：死了但没人 Close 的连接仍然占着这台 Supervisor，因为那个 Connection 对象的
    `execByRef`/tracker 还在声称它描述这轮——放行它会得到"第二台孩子 + 一份还在世的旧连接"。
  - **② REQ-RC-3 的两半分开交（原文落点全部现读，没有一句凭记忆）**：
    · `executions.provider_execution_id`：列在 `core/migrations/000001_core_schema.sql:313`（可空、无 CHECK）。
      契约要求它挂在 `claimed` 上的理由在 `specs/adapter-protocol/adapter-protocol-v0.1.md:189`
      （"**Core 可能在 claimed-未-dispatched 这个窗口里死掉**"）、`:204` 重述、`:221` 是 T84-3 判据；
      §8 `:346` 说 Recovery 用 lease/heartbeat/它 判 reconnect / resume / interrupt。
      **我这半已经在交值**：泵的 `Inbound` 带 `turn/claimed` 的 `ProviderExecutionID`，并落进
      `TurnTracker.ProviderExecutionID()`（`internal/adapter/stream.go` 的导出读口），
      `TestNewConnectionRidesTheChildWithoutStartingAnother` 与 `TestConnectionSendOverRealPipe` 钉住"穿过泵不变样"。
      ⇒ 归属答案：**写列的是 `internal/execution`（`executions` 归它，`scripts/check_ownership.sh:238`），
      时机是泵收到 `turn/claimed` 那一刻，缺的是一次列写而不是一个信号源**。
    · `execution_leases`：表在 `000001:335-341`（`execution_id` PK / `owner_id NOT NULL` / `acquired_at` /
      `heartbeat_at NOT NULL` / `expires_at NOT NULL`）+ `:343` 按 `expires_at` 的索引；语义只有
      `docs/architecture/CORE_DOMAIN_MODEL_V0.1.md` §24（`:792` 立表理由、`:810` "Runner 只有持有有效 Lease 才能执行"、
      `:812` 过期后由 Recovery Engine 判断），§56 `:1785-1791` 把它列进恢复判据四件之一。
      **今天写不进去**：`core/internal/platform/db/touch.go:201` 的 `narrowTables` 只有 `idempotency_keys`，
      注释 `:190` 明写"加表要过 REQ（契约 §1.1.13）"，并且 `update_test.go:92` 直接把
      `{"execution_leases", "heartbeat_at"}` 当**会被拒**的用例钉着。**表名册归属是 `internal/execution`**
      （`check_ownership.sh:239`）。
      **前置概念不存在**（现读，非推断）：`grep -rn "Runner" docs specs core` ⇒ 只有
      `CORE_DOMAIN_MODEL:792`、`:810` 与 `docs/handoff/Lucero_Qoder_Handoff_v0.1.md:400` 三处，
      **没有 Runner 的定义、没有 `owner_id` 的任何生产者、没有 acquire/release 协议、没有 TTL 数值**；
      `grep -rn "SweepingExpired"` = **0**（`specs/contracts-internal-v0.1.md:917` 那个 `SweepExpired` 是
      `approval`/`temporary_grants` 的，不是租约）。⇒ 归属答案：**今天没有一个角色能名正言顺写它**，
      三档可选形状（甲：v0.1 明写租约不进范围、恢复输入退回 §56 另三件；乙：先开 DB 门再补四件原文；
      丙：承认只有进程级存活）都写在 CO-57，**由主控裁**，我不选。
      另按已裁的 **CO-38**：我不建 poll、也不碰 `process_instances.heartbeat_at`（那列 v0.1 已显式作废）。
  - **③ 两条 0 命中判据的复证（同一棵树，入册之后）**：16:26:59→16:27:11 照字跑
    `go test -count=1 -timeout 90s -v ./cmd/fake-adapter/ -run '^TestDelivery'`（与 `'^TestStartNow'`）⇒
    两条都是 `testing: warning: no tests to run` + `ok … [no tests to run]`，`grep -c '^=== RUN'` **各 0**；
    机制复核 `grep -c "^func Test\(Delivery\|StartNow\)" cmd/fake-adapter/delivery_test.go` = **0**
    （包内用例全部 `TestFakeAdapter` 起头）。真实名字的四条各跑各绿（16:27:19→16:27:33）：
    位置判据 0.83 / 必须发帧 0.65 / 三态 0.66 / 消费者接缝 1.03，全部 `--- PASS:` 且各 RUN=1。
    第 31 轮那条"那句反例文字要跑变异才打印"今天仍然成立：`:410` 在位，打印它的是 `mut-r31.py k1-…`。
  - **两面读数与集合口径**：A 面 **135 PASS / 2 SKIP / 0 FAIL**（三包；比第 31 轮多 5 枚＝本轮新用例）；
    B 面 **18 包全 ok、`ALL_RC=0`、0 FAIL、0 build failed**，18 包 = 上一轮的 17 + `core/test`（14.987s，
    崩溃骨架与 e2e 都在里面）⇒ **"全"这个字在本轮换了集合**，两枚数不可对拍。
    这条 B 面的意义是：我改的是 `Connect` 的实现与 `Run` 的入口检查，**backend 刚入册的那根泵（`cmd/core` 19.798s ok）
    没有被我的闸门碰红**——它只用 `Connect`，没用 `Run`。
    未提交面 16:48:57 = `core/` 3 件（全是我：`connection.go`、`supervisor.go`、未跟踪的 `attach_test.go`）；
    整仓 8 处（其余 5 处是别家的账本与别家的在途件）。

- **第 31 轮补（09-30 15:56~15:58，T-397 巡检投递：改号 + 合验前两半的形状自查）· 不新起轮次、不改上面第 31 轮正文**
  - **② 改号映射（凭证就是这一行）**：主控 ② 明写"号请以 **T-395** 为准，正文不用回改"⇒ 我账上
    现读 `grep -n "T-391" .qoder/progress/adapter.md` ⇒ 指泵的共**六处**：`:63`（`- Stage:`）、`:70`（`- Last verified action:`
    里那句"`_late` 正是 T-391 判据 (a) 的落点"）、`:98`（`- Next action:` ③）、`:2088`（`## Coordination` 段末的 CO-56）、
    `:2355` 与 `:2397`（复量那行、B 面 build failed 的归属那行）——**字面全部原样留着**，一律读作**常驻泵那封 = T-395**；
    `T-391` 这个号归 backend 的 `REQ-RC-1`（09-29 已落）。行号会漂，认的是那句"backend 正在落的…那根泵"。
    我不回改，是因为改了会把主控那笔"编号行"的历史挪位；映射只在此处登记一次。
  - **③ 第一件：那句判据文本还在不在**（我自己现读，不拄巡检）：
    `grep -n 'turn/finished arrived before reply' core/cmd/fake-adapter/delivery_test.go` ⇒
    **`410:`** 命中 1 行，源码原文是
    `t.Errorf("%s: %d turn/finished arrived before reply#%s; T-392 ① puts the close after the receipt",`——
    **带格式动词，所以干净树上看不到那句话**（这一条 pass 时不打印任何东西）。
    主控引的那句成形文字（`start_now: 1 turn/finished arrived before reply#r1; …`）是**甲的位置回来时**的输出：
    15:58 我在今天的树上单跑 `python .scratch/adapter/mut-r31.py k1-settle-before-reply`（脚本这轮加了 argv 过滤，
    只跑这一枚），**逐字再现于 `mut-r31-k1-settle-before-reply.log:24`**，PRE-SHA = POST-SHA = `c11aaf2ffe55adbf`、
    ZMUT 残留 0。⇒ 合验要用这句话当反例证据时，**命令是"跑 k1"，不是"跑测试"**；这点必须在派发里写清，否则下一个人会在绿树上等一条不会打印的消息。
  - **③ 第二件：主控给的 `-run` 两条照字跑＝0 命中的假绿**（这是主控的账，我按"判据命令行必须真跑过"上交）：
    15:56:59→15:57:10 现跑 `go test -count=1 -timeout 90s -v ./cmd/fake-adapter/ -run '^TestDelivery'`
    与 `-run '^TestStartNow'` ⇒ 两条都是 `testing: warning: no tests to run` +
    `ok github.com/SadCat18/lucero/core/cmd/fake-adapter 1.7s **[no tests to run]**`，`grep -c '^=== RUN'` **各 = 0**。
    机制：我这包里的用例名全部以 `TestFakeAdapter` 起头（`grep -c "^func Test\(Delivery\|StartNow\)" cmd/fake-adapter/*_test.go`
    四份文件**全 0**），`^TestDelivery` / `^TestStartNow` 是照文件名猜的、不是照函数名。
    ⇒ **合验判据别写这两条**：`-run` 空匹配在 Go 里是 `ok`，不带 `-v` 连 `no tests to run` 都看不见。
  - **③ 第三件：真实名字的三条单跑（各 RUN=1 / PASS=1 / FAIL=0，15:57:20→15:57:31，`t397-readings-155720.log`）**，
    都是单个正则、不含竖线（守主控"竖线会把表格行劈开"那条）：
    `-run '^TestFakeAdapterClosingFrameFollowsItsReceipt$'` **0.84s PASS**（= ① 的位置判据，:410 那句的东家）、
    `-run '^TestFakeAdapterStartNowClosesItsRun$'` **0.67s PASS**（= §6 第 6 条③ 的"必须发帧"）、
    `-run '^TestFakeAdapterTurnFinishThreeStatesOnTheWire$'` **0.77s PASS**（= 三态分辨）。
    不过滤的那一次全量仍算第 31 轮的 15:15:18→15:15:29（**130 PASS / 2 SKIP / 0 FAIL、三包 ok**）。
  - **归属与可归因**：整仓 HEAD 已从第 31 轮的 `264d562` 换到 **`bfcbd88`**、未提交 27→**32 处**；
    但我这三包的闭包指纹 `bash scripts/build_stamp.sh ./internal/process ./cmd/fake-adapter ./internal/adapter`
    在 15:08:49 / 15:09:48 / 15:15:37 / **15:58:29** 四次**同值 `ec662dd69c073989781c31b239b2e275`（1699 个编译输入）**
    ⇒ 上面所有读数（含 15:57 那三条与 15:58 那枚 k1）**与我第 31 轮交账的是同一棵被验树**，别人在 `cmd/core`/`internal/task` 那一侧的搬运进不来。
  - **没做的事**：没有合跑两半（按 ③ 尾句等 backend 交账）、**没碰 `cmd/core` 一个字节**；
    它那一半的形状我只读巡检证据（`turnpump.go` 16099B@15:22、`sendreap*.go` 已无、`sendpipe.go:144` 已改口），**没跑它的包**；
    名单也不代加：自己现读 `specs/events/event-contract-v0.1.md` 的"本名单当前是"锚句 **命中 1、自称 26 条**
    （用 python 计数，不用中文 grep），要不要涨是主控的裁定项。

- **第 31 轮（09-30 14:46~15:10，T-392 生产者半：`turn/finished` 移到 send 回执之后 + 三态分辨）· 取代第 30 轮的「即时收尾」那一格**
  - **开工前先复量（⑤）**：14:46:38 `git rev-parse --short HEAD` = **`ade8085`**、`git status --porcelain -- core/`
    只有 `core/internal/approval/approval.go` 一件（不是我包的；14:47 整仓读是 3 件，另两件是 `store_sqlite.go` 与
    `specs/api-contract-v0.1.md`）；`test -f scripts/build_stamp.sh` 在。
    与主控那句"长在 HEAD=ade8085"当场相符 ⇒ 方向不改。**到 15:08:32 复量时树已经换了**：HEAD=`264d562`（14:51:31 主控那一笔）、
    未提交 21 件，其中 `cmd/core/sendreap.go` 是 **D**、`cmd/core/turnpump.go` 是 **??** ⇒ backend 正在落 T-391 的泵。
    **所有读数因此都带 `264d562`，没有一条来自 `ade8085`。**
  - **为什么"回执之后"是一条判据而不是一个细节**：收尾若在回执之前，"Core 有没有接住这轮结束"这个问题
    在观察上无法分辨——帧已经在缓冲里了。搬回之前那枚变异（k1）红的正是这一句：
    `delivery_test.go:410: start_now: 1 turn/finished arrived before reply#r1; T-392 ① puts the close after the receipt`。
    `call()` 在返回回执之前把它读到的一切记进日志，所以"回执之前没有"是一句**测出来的否定**，不是没看。
  - **分段为什么这么切**：`accept` 负责"这发开出了什么、证据帧上线"，`settle` 负责"这一发开出的轮次什么时候算完"。
    CO-8 那一对（claimed/dispatched）**留在回执之前**——回执声称"开了这一轮"，声称的东西必须已经在线上（CO-7）；
    收尾不是回执声称的内容，所以它是回执之后的第一帧。`queue` 与 guide 转向两条路 `opened == nil`，不 settle。
  - **`session/stop` 那一格的顺序我没搬**，并且是有意的：它的回执声称 `stopped=true`，收尾帧就是这条声称的证据，
    方向与 send 回执相反（`TestFakeAdapterStopClosesTheRunningTurn` 把"帧在回执之前"钉在原处）。
    两格读起来像不一致，其实是同一条规则的两个方向：**回执只能声称它发出去之前的东西**。
  - **`code` 只挂在解除上**：布防带码 = 一个可能永远用不到的参数被静默吞掉，所以直接 `INVALID_PARAMS`（k6 摘掉这层
    立刻红）。空 `code` 默认成 `SUCCEEDED` 是**承重件**不是糖：`internal/adapter/stream.go:175` 对
    `turn/finished` 缺码是拒发的，所以 k5（去掉默认）会让整条解除路径发不出帧——两枚各证一半。
  - **三态分辨各自钉在哪**（全在一枚真子进程里，`TestFakeAdapterTurnFinishThreeStatesOnTheWire`）：
    **没帧** = a1 被 a2 抢占、终局日志里 a1 零枚 `turn/finished`（外加"它确实被抢过"那条 status delta，
    否则这条否定什么都没证）；**有帧但码未裁** = 解除时给 `INTERRUPTED`，断的是 `rawClose()` 回来的**原始 params 字节**
    含 `"result_code":"INTERRUPTED"`；**有帧且落终态** = a3 走默认路径、`SUCCEEDED`，不带任何注入。
    消费者侧同一枚进程里再加两条：`Result()` 在帧到之前必须是 `""`，表外码穿过泵不许变样。
  - **变异批（`mut-r31.py`，15:05:16→15:07:21，两包同跑＝跨包判据，因为 `internal/process` 在测试里 `go build` 这份源码）**：
    基线 **77 PASS / 0 FAIL**；锚点命中全部 = 1；**8/8 FIRED**；每枚还原后 assert sha 与 PRE-SHA `c11aaf2ffe55adbf` 同值。

    | 变异 | 判决 | 读数 | 证的是哪一腿 |
    |---|---|---|---|
    | k1-settle-before-reply | **FIRED** | 75 PASS / 2 FAIL，红在"arrived before reply#r1" + Q10 顺序 | ① 的位置本身（甲回来了） |
    | k2-never-settle | **FIRED** | 73 PASS / 4 FAIL，含 `…StartNowClosesItsRun`、`…ClosingFrameFollowsItsReceipt` | §6 第 6 条③ 必须发帧 |
    | k3-hold-stops-deferring | **FIRED** | 69 PASS / 8 FAIL，含 `TestConnectionSendOverRealPipe`、`TestConnectionChainClosesBeforeItOpens` | `debug/hold` 还是不是判据 (a) 的仪器 |
    | k4-release-drops-the-code | **FIRED** | 75 PASS / 2 FAIL，含 `TestLateTurnFinished…` | 「有帧但码未裁」这一格 |
    | k5-no-default-code | **FIRED** | 75 PASS / 2 FAIL，红在 `…HoldReleaseIsWhereTheCloseGoes` | 空码默认值是承重件 |
    | k6-arming-accepts-code | **FIRED** | 76 PASS / 1 FAIL，红在三态用例第一句 | 布防不许带码 |
    | k7-guide-does-not-settle | **FIRED** | 75 PASS / 2 FAIL，红在 `no turn/finished with ticket "g1"` | guide 那一腿单独有人证 |
    | k8-finish-stops-draining | **FIRED** | 76 PASS / 1 FAIL，红在 `…QueueDrainsAtTurnFinishedNotAtNextSend` | CO-25 排干点没被搬家带走 |
  - **§10 表第五枚那一行的拟稿（补第 30 轮那一行的解除侧参数）**：
    `debug/hold` · 参数 `{"on": true|false}`，`on:false` 另收可选 `code`（默认 `SUCCEEDED`）·
    语义：推迟/立刻发出这一轮唯一的强制收尾帧（CO-25 的排干点随之落在这条帧上）·
    期望：`turn/finished` 出现在 send 回执**之后** · 只许 `adapter_key=fake` + `EXPERIMENTAL`（§9）。
  - **B 面的两处 build failed 原文（不认领、也不替它修）**：
    `cmd/core/sendpipe_test.go:216:37: unknown field launch in struct literal of type sendPipe` 等 3 条 +
    `cmd/core/discoverassembly_test.go:113:32: assignment mismatch: 4 variables but newAdapterServices returns 5 values`；
    `api/http/accept_chain_test.go:29:47: undefined: execution.TurnFinish` 等 7 条。
    归属：`git status --porcelain` 里 `cmd/core/*` 5 改 + `turnpump.go` ?? + `internal/execution/{finish,send}.go` 2 改
    都在 backend 名下（T-391），`core/test/event_producer_roster_test.go` 在 test 名下（T-394）。
    **我这一跑的 A 面闭包不含以上任何一包**（`go list -deps -test` 只含
    `internal/{adapter,audit,eventjournal,platform/db,process}` + `cmd/fake-adapter`）。

- **第 30 轮（09-27 16:52~17:10，T-386：CO-54 裁"丙" ⇒ `debug/hold` + 即时收尾）· 取代第 29 轮里"① 未落"那一格**
  （第 29 轮的正文与判决不删，按 T-276 当历史读；被取代的只有"① 未满足 / Ⅰ 交不了"那两句）。
  - **落地的形状**：`startTurn` 在 `evidence()` 之后默认 `finish(t, SUCCEEDED)` —— §6 第 6 条③ 那一格从此在
    **真子进程的原始线节点上**可见（`turn/finished/6@…`）；`debug/hold{on:true}` 把这一轮的收尾**推迟**，
    `{on:false}` 就是收尾点本身。⇒ 需要"有轮在飞"的形状（§5 转向、§4 stop、§5 队列位次、§6 规则 5 交错）
    现在**明着要**，不再靠双进程"永远不收尾"这个副作用。
  - **一处我主动定形并留了判据的选择**：**排干出来的那一轮不受 hold 影响**（它仍然在自己的运行结束时收尾）。
    理由写进 `drainQueue` 的注释了：hold 推迟的是"调用方按住的那一轮"，而 CO-25 的排干点正是"某一轮结束"；
    让 hold 把排干也吞掉就等于**移动排干点**，那是甲被否掉的原因。**h5 变异就是这一格的反例**
    （把排干改成也听 hold ⇒ `TestFakeAdapterDeliversThreeStates` 与 CO-25 那条同红）。
  - **③ 三条现在的状态**：
    **Ⅰ 已交**：`TestFakeAdapterStartNowClosesItsRun`（真起进程、一次 start_now、断的是**帧**不是回执，
    并要 `result_code == SUCCEEDED`）；另加 `TestFakeAdapterHoldReleaseIsWhereTheCloseGoes` 钉住
    "收尾出现在**解除那一刻**、不是下一次 send、不是计时器"（`on:false` 那半边没有这条就没人证，我先量到了这点）。
    **Ⅱ/Ⅲ 的 emit 侧**：被抢占/挂住/崩掉三档都不发 turn 级码 —— 由 `TestFakeAdapterDeliversThreeStates` 里
    "线节点上不许出现属于被抢占轮的 `turn/finished`"那段循环 + 消费者侧 `TestConnectionSendOverRealPipe` 钉住。
    **Core 侧**（挂住留 `RUNNING`+503、崩溃不许写成功）按 ④ 归 backend，**我没跑、也没替它跑**。
  - **变异（两包同跑，基线 74 PASS / 0 FAIL；锚点命中全=1；每枚按字节还原并 assert sha；
    `main.go` 现读 sha = `26a0604493beab5c` 与变异批 PRE-SHA 同值 ⇒ 证的就是交付字节）**：
    | 变异 | 摘掉的是什么 | 判决 | 红的用例 |
    |---|---|---|---|
    | h1 | 默认收尾（① 丢） | **FIRED** | `…StartNowClosesItsRun` + `…GuideOnQuietSessionIsQ10` |
    | h2 | hold 失效（装了照收） | **FIRED** | **7 条**：三态、链、队列排干、stop、解除点、+ 我包两条消费者用例 |
    | h3 | 解除时不收尾 | **FIRED** | `…HoldReleaseIsWhereTheCloseGoes` |
    | h4 | 能力表不声明 `debug.hold` | **SILENT**（**有原因，见下**） | — |
    | h5 | 让排干也听 hold（= 移动 CO-25 的排干点） | **FIRED** | 三态 + `…QueueDrainsAtTurnFinishedNotAtNextSend` |
    **h4 的 SILENT 我查了机制才敢登记**：`Handshake.Call`（`internal/adapter/handshake.go:342-369`）只查三件事——
    §2 未握手、§2.2 不得重复协商、§3 `write_entry` ——**不查方法在不在能力表里**；查表的是 `Delivery()` 与
    消费侧。所以那行 `debugCapability("debug.hold")` 的作用是"**对外诚实声明**"，不是运行时闸门：
    去掉它，双进程仍然照收 `debug/hold`。⇒ 开 **CO-55**：§9 说"`debug/*` 只对 `adapter_key=fake` 且
    EXPERIMENTAL 合法"，今天**没有任何判据**在"声明了却没资格"或"没声明却被调用"之间做区分（fake 侧 h4 静默、
    Core 侧无门槛）。要不要把这条做成判据、归谁做，请主控裁——我不擅自去 `Handshake.Call` 里加一道门，
    那会改变所有能力的调用语义，超出 T-386 授权。
  - **§10 的表还缺第五枚**（specs 归主控）：`specs/adapter-protocol/adapter-protocol-v0.1.md` 的
    "故障注入（`debug/*`，EXPERIMENTAL）"表现在只有 `debug/fail`/`crash`/`hang`/`unknown_quota` 四行。
    拟稿一行：**`| `debug/hold` | `{on:true|false}`：按住则本轮不在输出结束时收尾，解除即收尾（并触发 CO-25 的排干）。T-386 ① 授权 |**
  - **REQ-TURN-1 我按 ③ 记账、不越界**：这一格证的是"**Core 收得到帧并正确落终态**"，
    **不等于**"生产语义里帧什么时候到已经定了"。今天 `start_now` 的连接是"一次调用一条、返回即 `Close`"
    ⇒ 真 adapter 的收尾帧几乎必然后到。这条不裁死，Wave-2 的绿就是借来的绿——我不拿本轮的绿去填它。
  - **交付读数（两面，两个时刻的 HEAD 与脏件清单都带）**：
    A 面（`./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/`）**`exit=0`、顶层 121 PASS / 2 SKIP / 0 FAIL**、
    三包全 ok；本轮五条关键用例在 `faceA-r386.log` 的行号与行首前缀：
    `:26 --- PASS: TestConnectionChainClosesBeforeItOpens (0.86s)`、`:30 --- PASS: TestConnectionSendOverRealPipe (0.86s)`、
    `:217 --- PASS: TestFakeAdapterDeliversThreeStates`、`:227 --- PASS: …HoldReleaseIsWhereTheCloseGoes`、
    `:229 --- PASS: …StartNowClosesItsRun`。
    B 面整仓 17:10:03→17:10:26、**`exit=0`、544 PASS / 8 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**
    （`delivery-r25-171001.log`）；**那一刻 `core/` 有 18 条脏件（我 5 件、其余是 backend/test 的）** ⇒ 只记"这一跑 0 红"。
    指纹：**`internal/process = be4ef95cdea707dc`**、**`cmd/fake-adapter = baa270cd1cba56bd`**
    （第 29 轮那两枚 `33d54df2a154e3ee` / `46377e8752bf1298` 自此作废），`deps 67a1287e29df29c3`、
    `internal/adapter afbc9a1b8fa9b74f` 未变。HEAD `cde8d79`（16:59:02）。
    **"全" = 17 包，不含 `core/test/` 与 `scripts/`。** 我名下 .go 最新 mtime 17:09:44（那是变异还原的写回，
    sha 已核与跑前同值）早于 17:10:01 起跑 ⇒ 绿的是最后一次的字节。

- **第 29 轮（09-27 16:12~16:30，T-382：`turn/finished` 这一格）· ② 两半都落了、① 交回一个岔口**。
  改动全在 `core/cmd/fake-adapter/**` + 我包内一条被裁定改写了行为的用例（`internal/process/connection_test.go`）。
  - **②-a 字面**：`drainQueue` 的 `"OK"` → `const resultSucceeded = "SUCCEEDED"`。注释里写明
    §6 第 6 条只有一份取值表（住在 `internal/execution` 的状态常量），这里是**按名字对齐**、不是第二份枚举 ——
    fake 不该为了说"这轮成功了"去 import Core 的领域包。
  - **②-b 不发帧，但排水留着（本轮最要紧的一条发现）**：被 `start_now` 抢占的那一轮现在只发
    `stream/delta`(STATUS) 然后 `drainQueue()`，**不再发 `finish(prev, "INTERRUPTED")`**。
    我第一版是"整段删掉"，结果 **CO-25 那条"队列只在 turn/finished 处排干"的排干点就是这一帧** ——
    删帧连同删排水会让排在被抢占轮后面的队列**永久搁浅**（实测：`dq2`/`dq3` 一线节点都没有，
    `TestFakeAdapterQueueDrainsAtTurnFinishedNotAtNextSend` 直接红）。
    ⇒ 把"**一轮结束了**"与"**发一个 turn 级码**"拆成两件事：前者保留（排水照走），后者按 ② 取消。
  - **① 没照字落，因为照字落会撞掉另一条已裁的规矩**。我实现了"evidence 发完即 `finish(t, SUCCEEDED)`"
    并跑了一遍：**5 条红**，其中至少一条是真回归（队列搁浅，同上），另外三条是 §5 的
    `guide 转向正在跑的轮次`、`session/stop 结束在飞的轮次`、§6 规则 5 的交错 ——
    **一轮一旦即时收尾，这些形状在 fake 里就都不可达了**（`f.open` 永远是 nil）。
    我没有留下这个半成品（已回退），因为它同时违反 §5 与 CO-25 的现裁；三条可选形状与代价都在下面 ③。
  - **③ 交回主控的岔口：一个 `start_now` 轮次究竟何时算"跑完"** —— ① 要收尾帧，而 fake 的"跑完"目前没有内部边界：
    - **(甲) 即时收尾 + 修 CO-25**：`accept` 在"无人飞行时收到 queue"就地排干 ⇒ 需改一条已裁的排干点，
      且 §5 的"queued behind the open turn"回话文案要跟着变；
    - **(乙) Core 给一个"这一轮跑完了"的调用**：§4 那 11 个方法里现在没有（`session/stop` 的语义是 CANCELLED，
      用它发 SUCCEEDED 就是替 Core 编终态）⇒ 要新增方法，属协议变更；
    - **(丙) 一个"让这一轮先别完"的注入**（`debug/hold` 之类）：能把 ① 与 §5/规则 5 的形状都保住，
      但 T-275 定过"不新增 `debug/*`"，要这条豁免。
    **我的倾向是 (丙)** —— 只有它让"瞬时假适配器"仍能演出"两轮同时在飞"，而那是 §6 规则 5 的验收项；
    但三条都要动契约或动已裁的规矩，不是我这一格能自决的。
  - **③ 那三条自证现在能交到哪（不含水分）**：
    Ⅰ **未满足**（就是要 ① 那格裁完才有）；`SUCCEEDED` 这个值本身**已经在真子进程的线节点上出现过**
    （队列排干那一路，`TestFakeAdapterQueueDrainsAtTurnFinishedNotAtNextSend` 读的就是原始线节），
    但"start_now 自己那一轮"还发不出收尾帧 —— 我不拿前者冒充后者。
    Ⅱ/Ⅲ 的 **emit 侧**由我包内 `TestConnectionSendOverRealPipe` 钉住（下面那条断言更新），
    **Core 侧**（留 `RUNNING` + 503 `adapter_unresponsive`、崩溃那档不许写成功）按 ④ 归 backend，我没跑也不替它跑；
    本轮改完已具备让它复跑的条件（两包绿 + 跨包有牙）。
  - **断言更新（裁定改掉了被测行为，不是我的实现坏了）**：`internal/process/connection_test.go` 里
    `TestConnectionSendOverRealPipe` 原钉 `exec-1 = CLOSED/"INTERRUPTED"`、并要求读到 **7** 条通知；
    现按 ② 改成 **6** 条 + 两条新断言：**线节点上没有任何 `turn/finished` 属于被抢占的那一轮**、
    且 tracker 必须留在 `RUNNING`/空码（"没有收尾信号"不许被读成终态，§7、CO-8 同一口径）。
    它同时仍是 §6 规则 5 的验收位置：交错还在（exec-1 的尾巴落在 exec-2 的五帧之后），只是少了那一帧。
  - **本步变异（4 枚全 FIRED；基线两包 72 PASS / 0 FAIL；每枚按字节还原并 assert sha）**：
    | 变异 | 摘/换的是什么 | 判决 | 红的用例 |
    |---|---|---|---|
    | n1 | 连排水一起删掉（= ② 的"顺手做法"，帧与排水一起没了） | **FIRED** | `TestFakeAdapterDeliversThreeStates` |
    | n2 | 字面退回 `"OK"` | **FIRED** | `…QueueDrainsAtTurnFinishedNotAtNextSend` |
    | n3 | 给被抢占的一轮补 `turn/finished`（SUCCEEDED） | **FIRED** | `TestFakeAdapterDeliversThreeStates` |
    | **n4** | **把被禁的 `INTERRUPTED` 那一帧放回去** | **FIRED（跨包）** | 生产者 `TestFakeAdapterDeliversThreeStates` **+** 消费者 `TestConnectionSendOverRealPipe` |
    n4 是这一步真正的牙：**fake 少发/多发一帧，消费侧的用例会一起红**，因为
    `buildFakeAdapter` 现编 `cmd/fake-adapter`（`connection_test.go:883-894`）而不是引一个旧二进制。
  - **交付读数（两面，跑指纹与交账同一时刻各读一次 HEAD/脏件）**：
    A 面（我的三包）`exit=0`、顶层 **119 PASS / 2 SKIP / 0 FAIL**，`internal/process`/`internal/adapter`/`cmd/fake-adapter` 全 ok；
    B 面（整仓 17 包）16:29:48→16:30:09、**`exit=0`、541 PASS / 8 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**
    （`delivery-r25-162946.log`）。**B 面这一刻仍含别家在途件**：`M api/http/executions.go`、`M cmd/core/sendpipe.go`、
    `M internal/execution/{send,service}.go` + `?? cmd/core/{sendreap,sendstall,sendwedge}*`、`?? internal/execution/finish*.go`、
    `?? api/http/send_finish_test.go` ⇒ 那 541 只记"这一跑 0 红"。
    指纹：**`cmd/fake-adapter = 46377e8752bf1298`**、**`internal/process = 33d54df2a154e3ee`**（我改了它的用例）；
    `deps 67a1287e29df29c3`、`internal/adapter afbc9a1b8fa9b74f` 未变。HEAD `f1558cc`（16:28:03）。
    "全" = 17 包，不含 `core/test/` 与 `scripts/`。
  - **一条流程教训（我差点只交半面）**：16:25 那次我只跑了 `cmd/fake-adapter` 那一包就准备往下走，
    全量重跑才看见 `internal/process` 红 —— **改 fake 的行为必须连消费者包一起跑**，
    因为这两个包之间是"现编二进制"的真实依赖，不是编译期依赖，`go build ./...` 与单包测试都看不见它。

- **第 28 轮（09-27 13:44~13:51，T-362：CO-51 对拍 + CO-53 落点指认 + 单一来源导出）**。
  代码改动只在 `core/internal/process/`（四件、`numstat` 见 `## Changed files`），
  **没碰** backend 在途的 `api/http/send_two_phase_test.go`、`cmd/core/sendpipe.go`、`cmd/core/sendpipe_test.go`，
  没碰 `specs/**`。
  - **① 对拍结果（机械证明，不是目测）**：两份 `childAlreadyGone` 的**函数体逐字节相同** ——
    `cmp -s <(sed -n '/^func childAlreadyGone/,/^}/p' internal/process/supervisor.go) <(…cmd/core/processpipe.go)`
    ⇒ **相同**；两份各 **212 字节**、sha256 前缀同值 **`7364ae9e5ca630a8`**；两边的 import 面也同（`errors`×1、`os`×1）。
    定义位置现读：`internal/process/supervisor.go:154`、`cmd/core/processpipe.go:228`。
    语义一致 ⇒ **按裁定做单一来源**：我包内那枚**改名导出为 `ChildAlreadyGone`**，
    `cmd/core` 那份由 **backend 下次动该文件时改成调用它**（T-361⑤ 已派，我不去动他们面包）。
    **注释里我把对拍数字写进了函数文档**，这样"谁是原件"这件事不依赖这条账本活着。
  - **① 附带的牙**：导出一个谓词而不钉它，等于把一条规则交给调用方的自觉。新增
    `TestChildAlreadyGoneAnswersBothQuestions` —— 两问分开判：
    (a) `fmt.Errorf("kill: %w", os.ErrProcessDone)` **即使 pid 是本进程**也必须读成"已不在"（错误本身就是答案）；
    (b) `kill: invalid argument` + **活着的 pid**必须读成"真失败"（不许把杀不掉的错误吞掉）。
    第三问（pid 真没了）**不放单测**：它要一个已回收的 pid，撞上 pid 复用就变成 flaky 见证，
    而它已由端到端那条 `TestCloseAfterTheChildAlreadyCrashedStaysQuiet` 覆盖 —— 这一句取舍写进注释了。
  - **② CO-53 的落点我按小节名指认**（不引编号行，编号会漂）：契约① §1.1 第 23 条里新增的那款
    **「调用方义务：连接面上每一次调用必须自带期限」**（现读 `specs/contracts-internal-v0.1.md:592`）。
    内容与我的实测一致（5s 未返回、`c.dead=false`、`pending=1`、`writeMu` 永久占、只有 `Close` 2ms 解开），
    出处记的是 `.scratch/adapter/co53-probe7-130609.log`，并附了"该 log 灭失后只按'曾量到'引用"那句 ——
    与第 24 条⑤款同口径。**我这边不改契约、也不再打算在包里加 watchdog**：
    把 `closeReapTimeout`（2s，语义=收尸）挪成写期限就是混语义，而⑤明令不新增第三种期限。
  - **③ 第 11 格我不碰**：`turn/finished` 的**发射侧**在我包里有（六类通知 + `TestConnection*` 钉着），
    缺的是 Core 的**消费腿**，已派 backend（T-361①）。我没有、也不会去动 `internal/task` 或 `cmd/core` 帮它过。
  - **本步自己的变异批（3 枚，全 FIRED；改名之后必须重证一次牙还在）**：
    `python .scratch/adapter/mut-r28.py` → 判决留档 `mut-r28-out-134939.log`。基线 **46 PASS / 0 FAIL**、锚点命中全 = 1、
    每枚按字节还原并 assert sha（`connection.go dea3a88e47479d9e`、`supervisor.go d77bf094b9f42f17` 还原一致）。
    | 变异 | 摘掉的是什么 | 判决 | 红落在哪 |
    |---|---|---|---|
    | m6a | `Close` 里那层过滤（退回 `if err != nil`） | **FIRED** | `TestCloseAfterTheChildAlreadyCrashedStaysQuiet` |
    | m6b | 谓词第一问（`ErrProcessDone` 那一支删掉） | **FIRED** | `TestChildAlreadyGoneAnswersBothQuestions` |
    | m6c | 谓词第二问（末尾 `return false` 改成 `true`） | **FIRED** | 同上新用例 |
    **m6a 就是改名后的 m5**：第一批那条"过滤器有牙"的判决在新符号名下复现，没有因为改名变成 SILENT。
  - **交付读数（两面分开，两个时刻的 HEAD 与脏件清单都带）**：
    - **A 面（能算我的）** = `./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/...` 三包：
      **`exit=0`、顶层 119 PASS / 2 SKIP / 0 FAIL**（含子用例那一面 125）；我那 8 条在 `faceA-r28.log` 的
      **:32 / :34 / :36 / :38 / :42 / :44 / :46 / :97** 逐条 `--- PASS:`（前缀不是 FAIL，行首就是 `--- PASS:`）。
    - **B 面（整仓 17 包，含别家在途件，只记"这一跑 0 红"）**：13:50:36→13:50:54、`exit=0`、
      **525 PASS / 7 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**（`delivery-r25-135034.log`）。
      量指纹那一刻 13:50:34 的 `git status --porcelain -- core/` = 我 4 件 +
      `M api/http/send_two_phase_test.go`、`M cmd/core/sendpipe.go`、`M cmd/core/sendpipe_test.go`、
      `?? core/test/event_producer_roster_test.go` ⇒ **有 3 改 1 新不是我写的**，B 面那 525 不能当我的验收。
    - **指纹**：**`internal/process` = `397a7b999dc89720`**（13:50:36 构建）—— 这一步是真改了码，所以**换枚是应当的**；
      `deps 67a1287e29df29c3`、`internal/adapter afbc9a1b8fa9b74f`、`cmd/fake-adapter 38a594aa65efbca8` 未变
      ⇒ 我没碰它们的编译面。旧值 `9c750c42077d17c0`（第二批入册时那枚）自此失效，对拍的人请换新值。
      跑前后 HEAD 同为 `9660e68`（13:36:52）。"全" = 17 包，不含 `core/test/` 与 `scripts/`。
      **补一句免得 HEAD 标签对不上**：13:53 现读 HEAD 已到 `00bb3c4`，而那笔 `git show --stat HEAD -- core/`
      **无输出**（只动台账）⇒ 编译面自我读数以来未变，这两面读数对**当前码树**仍成立；
      要引"截至哪棵树"就写 `9660e68`（读数那一刻）并注明后续两笔皆非码。

- **第 27 轮补（09-27 13:06~13:08）· 一格可测的边界 + §23 那句"后两条同形与否"的回报**。
  零代码改动；证据 `.scratch/adapter/co53-probe7-130609.log`（探针 7，`--- PASS`）。
  内容全在 **CO-53**（`## Coordination` 顶部那条）：连接面对无期限 ctx **不检测、不自救**（A：5s 未返回、
  `c.dead=false`、`pending=1`、`writeMu` 永久占；B：排在后面的有期限请求仍 400ms 返回；D：只有 `Close` 解得开，2ms），
  以及三条腿的逐条读码结果（send `:66-69` 兜底 / discover `:78-81` 同形且注释已立该不变量 /
  probe `probe.go:277-286` 自己兜底且根本不走连接面）。
  **A 面复量确认（13:08:44）**：`go test -count=1 ./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/...` = **exit=0、三包全 ok**，HEAD 已到 `2964ecb`（这一小时里又推进了两笔），我名下脏件仍 **0 条**。
  ⚠ **同一次我差点报出一条假红**：那轮回量是用 python 的 `subprocess` 跑的，而我**重造了 env**（只塞 `CGO_ENABLED`/`PATH`，丢了 `SystemRoot`/`TEMP`）⇒ `exit=1` 且**一条 `ok`/`FAIL` 都没有**。形状记住：**退出码非 0 + 没有任何包级结果行 = 我的命令坏了，不是树坏了**；改用正常 shell 重跑就是 exit=0。报红之前先看自己的命令是不是构造对了。
  **为什么不顺手在包里加个 watchdog**：⑤ 明令"不新增第三种期限"，而把 `closeReapTimeout`（2s，语义是收尸）
  挪用成写期限就是混语义；所以这一格以**契约句**交回，不以我的常量补掉。

- **第 27 轮（09-27 12:32~12:49，停摆 47h52m 之后的复量）· 零代码改动、零新判据**。
  起手先按"被打断就重量盘上"办：T-343 到我手上时落款还是 09-25 21:3x，而中间发生过 47h 停摆 + 两批都已入册，
  所以那一封的 ①②③④ 我不照抄回答，全部换成今天的读数。
  - **② 两批分开报，数字从 git 现读**（不引任何人转抄过的瞬时数）：
    第一批 = **`ba03ce2`（09-25 21:17:25）**；第二批 = **`ae26c61`（09-25 22:02:21）**，
    `git show --numstat ae26c61 -- core/` = **`connection.go +63/−8`、`connection_test.go +108/0`**
    ⇒ 合计 2 files, **171 insertions / 8 deletions**。（巡检 21:41 量的 `55/0 + 108/0` 是我 30.44s 注释
    更正**之前**的那一枚快照，两个数都对，只是不同时点。）两条判据各自成套：**回收** = 三条 `TestClose*`，
    **可取消** = 三条 `TestAbandoned*`/`TestCancelAfter*`，没有合批。
  - **③ 今天的行号级读数**（树 = HEAD `67f064b`，日志 `.scratch/adapter/delivery-r25-123245.log`）：
    行首前缀全部是 `--- PASS:` —— `:821 TestCloseReapsTheChild (0.48s)`、`:823 …AfterAnExistingReapStaysQuiet`、
    `:825 …WithoutASupervisorReapsNothing`、`:827 …AfterTheChildAlreadyCrashedStaysQuiet`、
    `:831 TestAbandonedWriteComesBackInsideTheDeadline (0.41s)`、
    `:833 TestCancelAfterAnAbandonedWriteRefusesFast (0.40s)`、
    `:835 TestAbandonedWriteDoesNotLeakItsGoroutine (0.40s)`；我的包 `:886 ok … internal/process 7.462s`。
    **该批尾部是 `FAIL` 收尾**（`exit=1`、16 包 ok、`api/http` `panic: test timed out after 10m0s`），
    所以那一份不能当验收交；**随后 12:48:49→12:48:59 整仓过了**（`exit=0`、521 PASS / 7 SKIP / 0 FAIL、
    17 包全 ok、`delivery-r25-124848.log`），但我 **12:36 主控口径已到：整仓那一跑含别人未提交件 ⇒ 只记"这一跑 0 红"，
    不写"全仓绿"、更不是我的验收**。⇒ 报绿改认下面那行的 **A 面**。
  - **12:36 口径落地的形状（两面 + 两个时刻的 HEAD 与脏件清单）**，脚本
    `.scratch/adapter/two-face-r27.sh`（一次跑完两面）：

    | 面 | 集合 | 读数 | 时刻 | 算不算我的 |
    |---|---|---|---|---|
    | **A 我的面** | `./internal/process/... ./internal/adapter/... ./cmd/fake-adapter/...` = **3 包** |
    |   `exit=0`、顶层 **118 PASS / 2 SKIP / 0 FAIL**、7 条用例逐条 `--- PASS:`（`faceA-r27.log` :32/:34/:36/:38/:42/:44/:46） |
    |   12:51:40→12:51:47 | **算** |
    | **B 整仓** | `./internal/... ./cmd/... ./api/...` = 17 包 | `exit=0`、**17 包 ok / 包 FAIL 0**；
    |   顶层 PASS 计数 0 是**这一跑没带 `-v`** 所致（不是没跑，别当假绿读） | 12:51:50→12:51:59 | **不算**（含在途件） |

    两面同处 **HEAD `67f064b`**（跑前跑后各读一次，同值）。
    **12:54:19 我第三次自量同一枚清单 = 6 条**（4 改 2 新，HEAD 仍 `67f064b`，我名下 0 条）。与主控 12:34:28 / 12:36:36 两次的 **5 条不矛盾**：多的那一条是 `M core/api/http/send_test.go`，即 backend 在 12:36:36 之后、我 12:51:40 之前开始动它 —— **两次都对，只是不同时点**，别让下一个人以为谁数错了。（主控 12:53 自报的那处分钟错我已核：**我台账里从来没引过那个分钟** —— 没抄过，无需回改；**这条备注本身也不写下那个数字，否则「几处命中」那句会被它自己证伪。**）**量指纹那一刻与交账那一刻的脏件清单**：
    12:32:45 = `M api/http/executions.go`、`M api/http/tasks.go`、`M internal/execution/send.go`、
    `?? api/http/send_two_phase_test.go`、`?? core/test/event_producer_roster_test.go`；
    12:51:40 = 以上五条**仍在**、并多一条 `M api/http/send_test.go` ⇒ **两时刻都有两家在写，我只多干了一件事：
    把"含别人在途件"这句写在读数旁边，而不是等下一个人自己发现**。
  - **A 面能脱开 backend 的在途件，这条是硬证不是说法**：`go list -deps -test` 我那三包的闭包 = **12 个包**，
    `grep -E 'api/http|internal/execution|cmd/core'` = **0 命中**（清单 `faceA-deps-r27.txt`）⇒ 我这枚绿
    不经过他们正在写的三个件；反向我也不引他们的（T-323 ②）。
  - **REQ-S-1 不关单，我这侧再钉一次**（12:36 主控那句提醒与本封同紧）：泄漏半只闭到"**走过 `Close()` 的那条腿**"，
    "取消就不该起孩子"那一格仍开着（`Start` 不读 ctx，量测在第 24/25 轮）；停摆半归 backend 的两段式。
    这轮我**没碰** backend / test 在写的那四个件 —— 自证：
    `git status --porcelain -- core/internal/process/ core/internal/adapter/ core/cmd/fake-adapter/` = **0 条**。
  - **一条我没报出去的红（形状要写清，别让下次照抄）**：12:44 那次整包跑挂住的是 backend 在途件里的
    `TestSendConcurrentSealLoserShouts`（栈停在 `api/http_test.(*gateTurn).CallSendTurn`，**两个 goroutine 共享同一枚
    `gateTurn` 指针**；dump 里 `internal/process` 帧数 = **0** ⇒ 不是我的连接面）。
    但我 **没把它记成 backend 的缺陷**：`core/api/http/executions.go` mtime = **12:46:07**（挂住那次之后、
    复跑之前），HEAD 也从 `98ff8ee` 推到 `67f064b`，而 12:48 同一条整包命令**过了**、隔离跑 0.05s 也过。
    ⇒ 我读到的是**别人正在写的树**，按第 24 条与"别把自己的读数排到别人正在写的树上"这条不算一条红。
    **只留一句风险给他们**（查不查归他们，我不进他们文件）：那枚 gate 是共享状态、曾在编辑中途的树上挂满 100s，
    若真有间歇挂，两条命令能分辨 —— 挂：`cd core && CGO_ENABLED=0 go test -count=1 -timeout 100s ./api/http/`；
    不挂：`… -run 'TestSendConcurrentSealLoserShouts' ./api/http/`。证据留档 `api-http-hang-r27.log`。
  - **归属与"两天里没人动我的包"三证**：HEAD 里我那五处关键行（`closeReapTimeout` / `Stop(c.child, closeReapTimeout)` /
    `childAlreadyGone(err, …)` / `writeWithin` / `abandon`）**5/5 在原位**；`git log -1 -- core/internal/process/`
    = `ae26c61`（我自己那笔）；`internal/process` 编译指纹 **`9c750c42077d17c0` 与 09-25 21:38/21:48 那两批一字不差**。
    我名下三个可写路径 `git status --porcelain` = **0 条**；`zz_*` 残留 0、`grep -rn ZMUT core/` = 0。
  - **排程**：T-343 ⑤ 那句"`write` 那批排在 backend 之后"的争议已不需要裁定 —— 第二批在 09-25 22:02 就入了册。
    REQ-S-1 我这半边（回收 + 可取消）两格都算交完；**停摆那一半今天在动的是 backend**
    （`internal/execution/send.go`、`api/http/{executions,tasks,send_test}.go`、`?? send_two_phase_test.go` 全在途）。

- **第 26 轮补 2（21:52，T-339 巡检核到行号这一格按字补齐）· 零代码改动**。
  - **② 取代关系按第 24 条第 ⑥ 款的形状写全**（两枚行号我 21:52 现读，不是抄巡检的）：
    旧红 = `.scratch/adapter/delivery-r25-203909.log` **第 822 行** `--- FAIL: TestCloseReapsTheChild (30.44s)`
    （同文件 :820 是 RUN）→ 新绿 = `.scratch/adapter/delivery-r25-210428.log` **第 821 行**
    `--- PASS: TestCloseReapsTheChild (0.55s)`（:820 也是 RUN），后者 `grep -c -e '^--- FAIL'` = **0**。
    ⇒ **那条 20:39 的红被 21:04 那份逐字取代，我这边不再为它写任何解释性计时**（它的成因已在第 25 轮正文 ③
    归到"顺序"那一格，而它作为**秒数**已从我的代码注释里撤掉，见第 26 轮补）。
    **命令坑记一条**：`grep -c '--- FAIL' 文件` 会被 GNU grep 当成选项而 exit 2 ⇒ 一律写 `-e '^--- FAIL'`
    （巡检 21:23 也踩过同一枚）。
  - **④ 主控指的那条路我走了**：`bash .scratch/adapter/ctx-probe4.sh` 重跑并留住输出 =
    **`.scratch/adapter/probe4-retake-215219.log`**（21:52:19→21:52:24，`--- PASS` ×3 + `ok … 2.896s`），
    三档现读：`A5 Stop(child,2s) 耗时=1ms err=<nil>` + `A6 Reaped()=true`、
    `B1 Stop 先 0s` + `B2 CloseStreams 后 0s`，以及**C3 那一格 = `C2 Reaped()=false` 而 `C3 Stop 之后才来 err=kill: invalid argument`**。
    ⇒ 但**C3 现在不该只活在探针日志里**：同一形状在我包里已是用例
    `TestCloseAfterTheChildAlreadyCrashedStaysQuiet`，并有反例变异 **m5 钉住（FIRED）**。以后要引就引用例，别引秒数。
  - **④ 我对"用行为断言写"的答复，含一句不同意见**：**"先 `Stop` 再还管道"这个顺序本身没有确定性观测量**
    （第一批 m4 = 换成 pipes-first 后整包全绿，我如实登记过 SILENT）—— 因为**决定"留不留孤儿"的是
    有没有调用 `Stop`，不是它排在 `CloseStreams` 前还是后**。所以可断言的是**结果**而不是顺序，
    我按三种孩子形状各钉一条、正好凑成 ④ 要的那组行为判据：
    | 孩子形状 | 用例 | 反例变异 |
    |---|---|---|
    | 卡死、不理 stdin | `TestCloseReapsTheChild`（断 `Reaped()` 为真，不断秒数） | m1 FIRED |
    | 已崩、句柄已被 `cmd.Wait` 释放 | `TestCloseAfterTheChildAlreadyCrashedStaysQuiet`（断 `Close` 不报错） | m5 FIRED |
    | 已被别人收走 | `TestCloseAfterAnExistingReapStaysQuiet` | **m2 SILENT**（`childAlreadyGone` 吞掉同一个可观测量，理由与无判据分开写） |
    外加 `TestCloseWithoutASupervisorReapsNothing`（无监管者的收尾）。**若主控仍要把"顺序"本身做成判据**，
    唯一我能想到的确定性观测量是"礼貌 EOF 到底有没有送达孩子"（需要一个会自报 EOF 的助手模式）——
    但那会把"我今天选择不先关 stdin"这个**后果**冻成契约，所以我没有擅自加，等一句话。
  - **③ 一处时效更正（只报事实，不重开裁定）**：主控 21:2x 量"44.8 那两份也灭失"，
    而我按 T-337 ②/§24 ⑤ 其四做的重跑**现在在盘上**（21:18/21:20 两批）：
    `test -f .scratch/adapter/ctxprobe2-retake-211821.log` = **在**（`E … 实测返回=44.8059183s`）、
    `test -f .scratch/adapter/ctxprobe2-retake2-212000.log` = **在**（`44.8055391s`）、
    `test -f .scratch/adapter/ctxprobe2-after-batch2-213733.log` = **在**（`400.8158ms`，第二批之后同一支命令）。
    ⇒ "曾量到、输出未留存"这一句对 `30.44` 成立（它本来就不该当计时用，我已从注释撤掉），
    对 **`44.8` 现在不成立**。改不改契约那句是主控的笔，我只把 `test -f` 原文放这儿。
  - **⑤ 排序冲突上交，不自裁**：本封说"`write` 可取消那批排在 backend 的 REQ-S-1 写面之后"，
    而我手里另外两封（T-326 ③ 原文"顺序：先把 Stop 接线这一批交账，**再补 `write` 那一格**"、
    以及 `ba03ce2` 落账时那句"落完直接继续第二批，不用等我点名"）读出来的顺序是**排在我自己的第一批之后**。
    事实：第二批已在 21:24~21:39 落地、判据独立成套、21:45 与 21:48 重取过，**未入册、仍在盘上等我复跑**。
    两批在码上不互相依赖（我没碰 `cmd/core`/`api/http`），所以先后只影响合流摩擦、不影响正确性。
    **处置权在主控**：要我先按住等 backend 落完再入册，我按住；要我撤，我还原得出来（`git diff` 全在这两枚文件里）。
  - **① 复取状态**：全包级绿已取过三份（21:20:51→21:21:00、21:38:57→21:39:08、**21:48:42→21:48:53**），
    最新那份 **518 PASS / 7 SKIP / 0 FAIL、17 包全 ok**，三面指纹与 21:38 那批一字不差
    （`internal/process 9c750c42077d17c0`）。等树安静这件事我这边已经停笔（21:44:48 之后无 `.go` 写）。

- **第 26 轮补（21:43~21:48，T-337 巡检核打到我已入册的注释上）· 一条措辞更正 + 一次证据重取**。
  - **巡检抓对的那条**：`delivery-r25-203909.log` 里 **`grep -c killStreams` = 0**、`30.44` 只命中一处 =
    第 822 行 **`--- FAIL: TestCloseReapsTheChild (30.44s)`**（同一文件尾部第 1592 行就是 `FAIL` 收尾）。
    我 21:4x 亲量两条一致。⇒ 那是**一条红的用时**，不是任何一步的耗时；而我在 `Close` 上的注释
    （已随 `ba03ce2` 入册）写成了 "a full-suite run showed pipes-first **taking 30.44s**" ——
    **正是主控在契约① 第 24 条第 ⑥ 款自己收回的那类错，我把它带进代码注释里了。**
  - **改法**：那段注释重写为"**慢且红**（30.44s 挂在 `--- FAIL` 行上）、不是任何单步的计时读数、
    空机复现不出来（探针 A 同几步 0s）"，并把顺序的**承重依据换成 `cmd/core` 的先例 + ⑤ 那句"与 processpipe 同值"**，
    明写"下面这段不要求你相信任何秒数"。**判据与结论一个字没改**（`Stop` 先、还管道在后不变）。
  - **一次实测把上午那条记忆证伪了一半**（重要，别再传错）：这次注释是 **7 行换成 7 行**，
    `go test -c` 的 `internal/process` 哈希 **`9c750c42077d17c0` 一字未变**；而我 21:0x 那次注释降级
    **净 +1 行**，哈希就从 `341c5cd8…` 变成 `c64734a5…`。⇒ 真正的判据是**行数有没有位移**
    （现读 `git diff --numstat` 的增/删是否相等），不是"改没改注释"。第 39 条我已按这个改口径。
  - **证据重取（不偷懒，全部留档）**：`mut-r26.py` 在更正后的字节上**整批重跑** = 基线 45 PASS/0 FAIL、
    锚点命中全 = 1、**m1/m2/m3/m4/m5 仍 5 FIRED / 0 SILENT**、还原 sha 同值
    （`PRE-SHA fb4982ac1a69a41e`，判决 `mut-r26-out-214504.log`）；交付跑 **21:48:42→21:48:53、`exit=0`、
    518 PASS / 7 SKIP / 0 FAIL、17 包全 ok、build failed 0、panic 0**（`delivery-r25-214842.log`），
    三面指纹与 21:38 那批**完全相同**（`9c750c42077d17c0` + `67a1287e29df29c3` + `afbc9a1b8fa9b74f` + `38a594aa65efbca8`）。
    **"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
  - **44.8 那一格的现状（交主控定口径，我不重开归属之争）**：主控 ① 撤裁定时量的"两份都灭失"
    已被我 21:18/21:20 的两次重跑取代，**输出这次留在盘上**：
    `ctxprobe2-retake-211821.log` = **44.8059183s**、`ctxprobe2-retake2-212000.log` = **44.8055391s**、
    第二批落地后同一支 = `ctxprobe2-after-batch2-213733.log` = **400.8158ms**。
    ⇒ 按第 24 条 ⑤ 其四"要恢复成事实只有一条路：重跑"，这一格**现在是可复跑事实**；
    要不要请主控把"曾量到、输出未留存"那句改回去，是他的笔、我不改契约。
  - **③ 里两份证据我只引真的那份**：`probe4-full.log`（巡检逐字对得上，含 A5/B1/**C3**，
    尾部 `--- PASS` + `ok … 2.159s`）**作 REQ-S-2 的现成反例主证**；
    `delivery-r25-203909.log` **只作"那条红出现过、且已被后续四批绿取代"的证据**，
    后续绿 = 20:55:37→47、21:04:28→39、21:20:51→21:21:00、21:38:57→21:39:08、21:48:42→53
    （同一枚用例每批都在里面 PASS；`TestCloseReapsTheChild` 全文再无 FAIL）。
  - **排程事实（T-335③ 独占 `core/` 那条现在被破了，我只报不拆）**：21:48:42 现读
    `git status --porcelain -- core/` = 我 2 件 + backend 4 改 1 新
    （`api/http/{handler,tasks,tasks_test}.go`、`cmd/core/main.go`、`?? api/http/tasks_get_test.go`）。
    **我这侧从 21:44:48 起已停笔**（之后只有台账与记忆的写），第二批判据也已重取完 ⇒
    **`core/` 的写窗口对他们让开**；第二批随时可入册，入册后我这面就干净了。

- **第 26 轮（第二批 = 可取消的 `write`，T-326③ 落地，09-25 21:24~21:39）· 与第二批之外的东西不合批**。
  可复跑：`bash .scratch/adapter/ctx-probe2.sh`（改前/改后同一支）、`python .scratch/adapter/mut-r26.py`
  （判决全文留在 `.scratch/adapter/mut-r26-out-213345.log`）、`bash .scratch/adapter/delivery-r25.sh`。
  - **① 先按 §24 ⑤ 其四把 44.8 那一格救回"可复跑事实"**：我重跑了 E 探针**并这次把输出留住**
    （`.scratch/adapter/ctxprobe2-retake-211821.log` = **44.8059183s**、
    `ctxprobe2-retake2-212000.log` = **44.8055391s**，两批同形状）。第二批落地后同一条命令再跑
    （`ctxprobe2-after-batch2-213733.log`）= **400.8158ms** ⇒ **改前/改后成对**，比值 112 倍。
    探针在改后当场记到"E 孩子去向=存活"（那一刻 `Close` 还没跑）——**这不是回归，是裁定的形状本身**：
    超时只标死连接，回收归 `Close`。
  - **② 一条引用勘误（不改契约，交给主控落笔）**：`.scratch/adapter/ctxprobe_out.txt` **不是我任何脚本的产物**。
    `grep -rn "ctxprobe_out" .scratch/adapter/` = **0 命中**（我那四支探针全部只往 stdout 打印，没有一支重定向过文件名），
    而且被引作依据的那一行 `took=44833.49ms` 无论**格式**还是**数值**都不是我的输出（我的形状是
    `实测返回=44.8055391s`，见 ①）。⇒ 第 24 条 ⑤"只有它还能复跑"那句引用的不是我这枚文件。
    **我不需要据此重判归属**：按 ⑤ 其三（`internal/process` 归 adapter 由归属表支撑）+ 其四（我已重跑并留存），
    这一格现在的事实不靠那个文件名成立。同理 T-328 ④ / T-332 ③ 里那个 `got=%d`：
    `grep -rn --include=*.go --include=*.txt --include=*.sh -e 'got=%d' -e '%15' -e 'took=' .scratch/adapter/` =
    **0 命中**（只有 `.exe` 里 Go runtime 的格式串会命中，那不是"我日志里的漏参"）。
  - **③ §9.2 规则 5 的原文我核过才动手**（这条决定我能不能起那枚 goroutine）：
    `docs/handoff/Lucero_Qoder_Handoff_v0.1.md` §9.2 第 5 条原话是 **"不裸启动<u>无限</u> goroutine"**（限定词是"无限"），
    而"禁止散落裸 goroutine；通过 Supervisor/WorkerPool/Scheduler 启动**长任务**"在 **§3.4**。
    ⇒ `cmd/core/processpipe.go` 的注释"§9.2 规则 5 禁'裸 goroutine'"是**条款号对、限定词丢**，
    照它字面理解会得出"任何 watchdog 都不许起"的错结论。**我没改那个文件**（backend 的落点），只把原文记在这儿。
    我起的这枚是有界的：单次 send 进缓冲通道、`Close` 杀掉孩子之后三枚 goroutine 全部退出（m4 是它的反例）。
  - **④ 实现**（只在 `internal/process`，`numstat` 见 `## Changed files` 第 26 轮那条）：
    `(*Connection).writeWithin(ctx, f)` = `select { 写完 / ctx.Done() / c.dead }`，写放在一枚可放弃 goroutine 上；
    `abandon(method, cause)` 只做 `kill(...)`；`request` 的首次写改走 `writeWithin`；
    `Cancel` 在 `closed` 之外**增看 `c.dead`**，死了直接 `ErrConnectionLost`，不排到那把卡住的 `writeMu` 后面。
    超时返回的错误是 `errors.Join(ctx.Err(), lostError(...))` ⇒ `errors.Is` 对
    `context.DeadlineExceeded` 与 `ErrConnectionLost` **两问都答 yes**（调用方按哪一档分支都行）。
  - **⑤ 我自己引入、又被判据抓回去的一件回归（这条是本轮最该被读到的一句）**：
    `abandon` 第一版顺手写了 `c.closed = true` ⇒ `Close()` 顶上那句 `if c.closed { return nil }`
    **早退、一步都不杀孩子** ⇒ 泵、reaper、被放弃的写 goroutine 三枚永久挂着 + 孩子活着
    ——**正好把第一批刚闭掉的"孤儿"在那条新路上开了回去**。抓到它的就是我为"注释里那句 §9.2"补的
    `TestAbandonedWriteDoesNotLeakItsGoroutine`：两次跑出 **11 vs 基线 8**、**20 vs 17**（差值稳定 3，不是噪声），
    栈 dump 直接点名三枚的落点（`adapter.(*Reader).Read` / `adapter.(*Writer).Write` / `Supervisor.Start.func1`）。
    修法 = `abandon` 只 `kill`、拒绝新写改由 `Cancel` 看 `c.dead`。**m3 就是这一格钉住的反例（FIRED）**。
    ⇒ 一句纪律：**"标死"与"关掉"是两个状态，把它们合并到一个 flag 上会让另一个拥有者的收尾变 no-op。**
  - **⑥ 第二批自己的判据（与第一批两套，不共用一条）**：
    用例三条 = `TestAbandonedWriteComesBackInsideTheDeadline`（400ms 期限 → 实测 400/401ms、
    两个 `errors.Is` 都成立、第二次写被**快拒** <1s）、
    `TestCancelAfterAnAbandonedWriteRefusesFast`（CO-52 那第二道不可取消步骤：3s 观察窗内必须有返回、
    且 err 是 `ErrConnectionLost`）、`TestAbandonedWriteDoesNotLeakItsGoroutine`（⑤）。
    变异批 `mut-r26.py`（**整包不过滤 -run**，基线 45 PASS / 0 FAIL、锚点命中全 = 1、每枚按字节还原并 assert sha）：

    | 变异 | 摘掉的是什么 | 判决 | 红落在哪 |
    |---|---|---|---|
    | m1 | `request` 退回不界住的 `c.write` | **FIRED** | 期限那条 + 泄漏那条（2 红） |
    | m2 | `abandon` 不再 `kill` | **FIRED** | 期限那条 + `Cancel` 那条（2 红） |
    | m3 | 把 `c.closed = true` 塞回 `abandon`（⑤ 那个 bug） | **FIRED** | 泄漏那条 |
    | m4 | `done` 改成无缓冲 | **FIRED** | 泄漏那条（被放弃的写无处可送） |
    | m5 | `Cancel` 不再拒绝死连接 | **FIRED** | `Cancel` 那条 |

    **5 FIRED / 0 SILENT**，还原后 `POST-SHA == PRE-SHA`（`9a0cbcb0e698077a`）。第一批那三条 `TestClose*` 全程未红。
  - **⑦ 交付跑（报绿只认这批）**：21:38:57→21:39:08、`exit=0`、**518 PASS / 7 SKIP / 0 FAIL、17 包全 ok、
    `build failed` 0、panic 0**（`.scratch/adapter/delivery-r25-213855.log`）。三面指纹里
    **`internal/process` = `9c750c42077d17c0`（构建时刻 21:38:57）**，`deps 67a1287e29df29c3` /
    `afbc9a1b8fa9b74f` / `38a594aa65efbca8` 未变 ⇒ **第二批起 `internal/process` 是新的**（T-323 ③ 要的那句）。
    **"全" = 17 包，不含 `core/test/` 与 `scripts/`。**
    被验面现读：`git status --porcelain -- core/` = 我 2 件（`connection.go`/`connection_test.go`）
    + backend 4 改 1 新（`api/http/handler.go`、`tasks.go`、`tasks_test.go`、`cmd/core/main.go`、
    `?? api/http/tasks_test…` 那族），第一批的 `supervisor*.go` 已随 `ba03ce2` 进 HEAD（我 21:39:23
    按字复量：`git show HEAD:...connection.go` 里那三行关键改动 **3/3 在原位**）。
    这批比 21:10 那次干净是因为 backend 那枚编不过的在途件已能编译；**我不引用别家的绿、也不替它担红**。
  - **⑧ 生产侧前提我核过（只读，没动文件）**：`cmd/core/sendpipe.go:76-80` 与
    `cmd/core/discoverassembly.go:89` 两条腿都 `defer conn.Close()` ⇒ 裁定那句"超时即标死、交给 `Close`
    回收孩子"**今天真的有人接**。若哪天某条腿不 Close，⑤ 那三枚 goroutine 就会回来——泄漏用例是唯一的闸。

- **第 25 轮补（21:08~21:12，接 T-323 / T-326）· 一次注释降级 + 一次没拿到绿的复取**。
  可复跑：`bash .scratch/adapter/delivery-r25.sh`（同一支脚本，读数按批次换文件）。
  - **① T-326 ④ 的处置**：`connection.go` 里那句"the measured case: … blocked 44.8s …"**降级成**
    "有一条探针读数、**未入册前不许下游当事实引用**"（只动 `//` 行，可执行语句一字未进出 ——
    这条我按自己那次编辑的 old/new 两端陈述，**不是 diff 证据**：21:04 与现在之间没有提交点可对照）。
  - **② 新字节上我能归因的那份绿**：`CGO_ENABLED=0 go test -count=1 -v ./internal/process/` →
    `exit=0`、**顶层 `--- PASS` = 36 / SKIP = 1 / FAIL = 0**（**另一个计数面**：连子用例一起数是 42，
    变异批报的就是这一面，两面别混用）；`Close` 那 4 条用例全 PASS；两趟 `ok` 5.261s / 6.356s。
    日志 `.scratch/adapter/proc-r25b-2111.log`（21:11:54）。
  - **③ 全量复取今天拿不到**（这是 ② 与"17 包全绿"分开写的那一半）：21:10:30→21:10:40 那批 `exit=1`、
    **`包 ok` = 16**、`api/http` = **`[build failed]`**；五条编译错全部落在
    **`core/api/http/tasks_get_test.go`**（`"strconv" imported and not used`、`undefined: httptest` ×3、
    `undefined: itoa`）⇒ 那枚是 **backend 的未跟踪在制品**（`?? core/api/http/tasks_get_test.go`，
    同刻 `core/` 有 8 改 1 新，我名下仍只有那 4 件）。`api/http` 整包编不出来 ⇒ 顶层 PASS 从 508 掉到 398，
    **那 110 的差不是回归、是一包没跑**。按 T-323 ②"**谁也不许引用对方那一份当自己的依据**"：
    我不引用 backend 的 18 包绿，也不把这一条红算成我的。**我的 17 包全绿那一份仍是
    21:04:28→21:04:39（源码为 21:0x 那版字节）**。
  - **④ 指纹又换了一枚，这条要喊给 `test`**：`internal/process = `**`c64734a5309c1a89`**（构建时刻 21:10:30），
    **`341c5cd8fce19e0f` 只活了 6 分钟即作废** —— 改注释会挪行号表、哈希跟着变，所以
    **"只改注释" ≠ "哈希不变"**，别拿这个直觉省复采。`deps 67a1287e29df29c3`、
    `internal/adapter afbc9a1b8fa9b74f`、`cmd/fake-adapter 38a594aa65efbca8` 三面仍未变。
  - **⑤ 探针第六封**（T-326 ③ 之前先替 ③ 量清楚，21:08:36，`.scratch/adapter/ctxprobe6_test.go.txt`）：
    已取消的 ctx 进 `Connect` → **仍真起一个孩子**（这批量到 833ms，第 24 轮 588ms ⇒ **毫秒数受负载影响、
    别当常量引用**，稳的是"起了一个真进程"这个结构结论）；随后 `Close()` **1ms、孩子已收走**。
    **没人调 `Close` 时孩子照样活着**（pid 35648）⇒ 我对 T-321 ③ 只认领"**走过 `Close()` 的腿不再留孤儿**"，
    **"取消就不该起孩子"那一格不写进我的关闭权**（要 `Start` 读 ctx 或 backend 的两段式才算）。
    临时件跑完 `zz_*` 残留 = 0、`grep -rn "ZMUT" core/` = 0。

- **第 25 轮（T-314 三处点名 + ⑤ 落地，09-25 20:31~20:56）**。可复跑命令四条：
  `bash /d/Project/Lucero/.scratch/adapter/ctx-probe3.sh`（(b)(c) 两处，跑窗 **20:32:15→20:32:22**）、
  `bash .../ctx-probe4.sh`（把 Close 拆成四步各自计时，**20:42:40→20:42:44**，完整读数在
  `.scratch/adapter/probe4-full.log`）、`python .../mut-r25.py`（变异批）、
  `bash .../delivery-r25.sh`（交付跑 + 三面指纹）。三份临时探针件都 `trap` 删除，包内 `zz_*` 残留 = 0
  （每封跑完当场 `ls` 量过），`grep -rn "ZMUT" core/` = **0 命中**。

  **① T-314 ② 那三处的读数**（承接第 24 轮的表，本轮补 (b)(c)，并把"卡死孩子"那一列做实）：

  | 处 | 是否 select `ctx.Done` | 卡死孩子下的实测返回耗时 | 孩子进程去向 |
  |---|---|---|---|
  | (a) `Connect` 起孩子 + 等 hello | **`Start` 函数体一次都不读 ctx**；`Connect` 只透传 | **`Connect` 2.25ms、不等 hello**；传已取消的 ctx 进去 `Start` 仍花 **588.0ms** 真起一个孩子 `err=nil` | **存活**（第 24 轮量的） |
  | (b) ctx 分支里那句 `c.Cancel(id)` | **签名里没有 ctx ⇒ 根本没有可 select 的腿**（`connection.go:212`） | 管道未满：**0s、err=nil**；另一次 write 正卡在管道里：**3s 观察窗内未返回**（探针 b2） | 不适用（它不碰进程） |
  | (c) `sendPipe` 的 `defer conn.Close()` | 无 ctx | **修法前 0s**（第 24 轮 F 探针：瞬间返回 + 孩子存活）；**⑤ 之后 2ms**，并且**把那次卡住的 write 解开**（探针 c′：401ms 后带 `write \|1: file already closed` 返回） | **修法前：存活 ⇒ 孤儿**；**修法后：已退出/已收走** |

  由此定死三句：**(b) 是 `request` 在 ctx 到期之后的第二道不可取消步骤**（我只在 `Cancel` 的注释里点名，
  **没有擅自给它加期限** —— 那是 §23 的形状决策，开成上面 `Next action` 第 ② 格）；
  **(c) 的 44.807s 与 30.44s 是两件事**，前者在 `select` **之前**（`c.write`，第 24 轮 E 探针），
  后者是 `Close` 自己的步骤顺序（下面 ③）；**`Cancel` 不背"回收孩子"这一格**，孩子由 (c) 收。

  **② ⑤ 的落地**：`Connection.Close()` 现在接**既有的** `Supervisor.Stop(c.child, closeReapTimeout)`，
  `closeReapTimeout = 2 * time.Second`（与 `cmd/core/processpipe.go:212` 同值）。
  **不新增 Kill、不新增第三种期限、`Stop` 本身一个字没改**。新增两枚：`(*Child).Reaped()`、
  `childAlreadyGone(err, pid)`（原因见 ③②）。

  **③ 落地时量出两件必须和 ⑤ 一起处理的缺陷（都是量出来的，不是想到的）**：
  1. **顺序**：`Stop` 必须排在把管道还回去**之前**。我第一版按"先关管道是礼貌一步"的字面写成
     `CloseStreams → Stop`，**带载全量跑里 `Close` 花了 30.44s**（≈ 助手生命期）；同几步在空机是 **0s**
     （探针 A1/A2/A3 全 0s），探针 B 的 `Stop→CloseStreams` = **1ms + 0s**。
     **30.44s 那一次我事后复现不出来**，所以"关一个还挂着读的句柄要等孩子本身"这句按**排除法留下的解释**读，
     不当测量定律：`Connect`/`Reaped` 亚毫秒、`Stop` 被 2s 界住，`Close` 里唯一能等满孩子生命期的只剩关句柄那一步。
     `cmd/core` 的 `duplexPipe.Close`（`:207-221`）早就是这个顺序 ⇒ ⑤ 那句"与 processpipe 同值"含这一层。
     ⇒ **"关 stdin 让孩子自退"我最后没有排在 `Stop` 前面**：⑤ 说它"可以作为礼貌一步"、不是机制，
     而把它排前面正好制造下面第 2 件那个"孩子已死"分支。
  2. **already-gone**：孩子**自己先死了**（crash / 握在手里崩掉）时，后台 reaper goroutine 的 `cmd.Wait`
     已释放进程句柄 ⇒ `Stop` 的 `Kill` 拿到 **`kill: invalid argument`**（探针 P1 实测，从 `Close` 返回给调用方）
     ⇒ 一次正常收尾被报成失败。`cmd/core` 对同一个孩子已有 `childAlreadyGone` 这一层过滤，我在自己包里
     复刻了同一条规则 ⇒ 两份归一开成 **CO-51**（我不动 `cmd/core`）。
     顺带一条按字结论：**`Reaped()` 那道闸拦不住这一件** —— 它读的是"有人调过我的 `Wait`"，不是"孩子死了"。

  **④ 变异批（`mut-r25.py`，整包不过滤 `-run`，2 FIRED / 3 SILENT）**：基线 `exit=0`、包内
  **42 PASS / 0 FAIL**；五枚锚点命中数全 = 1（**没有 INVALID**）；每枚跑完立刻按字节还原并 assert sha 相同
  （`PRE-SHA == POST-SHA = f9fc7e9df5ef43ed`，跑前跑后各测一次）。

  | 变异 | 摘掉的是什么 | 判决 | 红落在哪一条 |
  |---|---|---|---|
  | m1 | `Stop` 那一句不执行 | **FIRED** | `TestCloseReapsTheChild`：`Close returned while pid 44660 is still unreaped` |
  | m2 | `!c.child.Reaped()` 那道闸 | **SILENT** | —（见下面整段"m2 翻过案"） |
  | m3 | `c.sup != nil` | **SILENT** | —（`Stop` 自己第一行 `c.cmd == nil ⇒ return nil` 先给出同一个答案） |
  | m4 | 换成"先还管道再 `Stop`" | **SILENT** | —（30.44s 只在带载那一次出现，做不成确定性判据） |
  | m5 | `childAlreadyGone` 过滤 | **FIRED** | `TestCloseAfterTheChildAlreadyCrashedStaysQuiet`：拿到原话 `kill: invalid argument` |

  **m2 翻过案，这段要按字读**：第一遍批里 m2 判 **FIRED**，红的却是**别人** ——
  `TestConnectionDeathMarksTurnUnknown`（`connection_test.go:361`
  `turn state before the crash = "CLAIMED", want RUNNING`）。我查了机制：那条用例等的是
  `c.LastSeq() == 2`（游标，在 `c.mu` 里推进），断的却是 tracker 状态（`applyTurn` 在**放锁之后**才跑）
  ⇒ **等的信号和断的东西不是同一个**，带载就能错开；而它用的是 `pipeConnection`（`sup == nil`），
  m2 那个改动**在字面上碰不到它**。我把等待谓词换成被断言的那个谓词（`LastSeq` 那格降级成第二条断言、
  没有删），改完同一枚 m2 复跑判 **SILENT**。
  ⇒ 两个结论：① **同一枚变异在修掉一条 flake 前后给出不同判决，这件事本身就是那一次红的归属证明**；
  ② `Reaped()` 从此**没有确定性判据**（它防的是 pid 复用，测试里造不出来），我按
  "有理由、无判据"登记，**不拿 `TestCloseAfterAnExistingReapStaysQuiet` 冒充它有牙**
  （那条用例的注释已改成这么写，代码注释里 m2/m3/m4 三格也都标了 SILENT）。

  **⑤ 交付跑（报绿只认最后一次重跑）**：跑窗 **20:55:37→20:55:47**，`exit=0`、
  **508 PASS / 7 SKIP / 0 FAIL、17 包全 ok、`build failed` 0、panic 0**
  （日志 `.scratch/adapter/delivery-r25-205535.log`）。**其后 21:04:28→21:04:39 又复跑一批，
  数字与三面指纹全同**（`delivery-r25-210428.log`，HEAD 已推进到 `a579fc5`，跑窗内
  `git log --oneline 1bcdcfd..HEAD -- core/` 仍为空）⇒ 报绿认这一批。**"全" = 那 17 个包，不含 `core/test/` 与 `scripts/`**；
  498→508 的差额里含我本轮新增的 4 条 `Close` 用例，其余是别家自 19:43 以来入库的，我没有逐条分账。
  - **三面指纹 + 构建时刻**：`deps 67a1287e29df29c3`、`internal/adapter afbc9a1b8fa9b74f`、
    `cmd/fake-adapter 38a594aa65efbca8` 三枚**未变**；**`internal/process 341c5cd8fce19e0f` 变了**
    —— 变的正是我改的那个包，构建时刻 **20:55:37**。（旧值 `c6a53803aa7ce5c1` 自此失效，别再拿它对拍。）
  - **被验面（T-321② 要求明写）**：`git status --porcelain -- core/` = **4 条，全部我自己**
    （`connection.go`/`connection_test.go`/`supervisor.go`/`supervisor_test.go`），untracked **0 条**
    ⇒ **这一批的读数不在干净被验面上**，它验的就是我这 4 件在制品；跑窗内
    `git log --oneline 1bcdcfd..HEAD -- core/` = **空**（没有别人把码落进 `core/`），HEAD 现读 **`6763a18`**。
  - **负载披露**：交付跑起跑前 `tasklist` 无 `go.exe`、`.scratch/test/` 最新项 **20:44:56**（起跑前 10 分 41 秒静）
    ⇒ 比 20:39 那批干净得多，但只能声称"近似空机"、不能声称整机静。
    **上一条 20:39:13→20:39:52 是带载的**（`test` 的变异批在跑：`mut{A,B,C}.out` 20:33~20:34、
    `tmp.K3M9ILd1RF` 20:38:43），它 `exit=1`、506 PASS / 7 SKIP / **1 FAIL**，红的就是
    `TestCloseReapsTheChild` 的 30.44s —— **那一条红把我引到了顺序缺陷上，我没有把它当 flake 放过去**。
- **第 24 轮（T-310 连接面 ctx 量测，09-25 20:06~20:09）**。可复跑命令：
  **`bash /d/Project/Lucero/.scratch/adapter/ctx-probe.sh`**（探针源 `.scratch/adapter/ctxprobe_test.go.txt`，
  跑完 trap 删除；本次跑窗 **20:09:08→20:09:12**，包内残留 = 0）。被验的包 `internal/process` **零脏件**
  （`git status --porcelain -- core/internal/process` 只出现那枚临时件），所以结论与 backend 同时在写的
  `notification`/`api/http` 无关。

  | 段 | 是否 select `ctx.Done` | 实测返回耗时（ctx 状态） | 孩子进程去向 |
  |---|---|---|---|
  | **`Supervisor.Start(ctx)`**（起子进程） | **否**——函数体内一次都不读 ctx | **588.0ms**（传入**已取消**的 ctx，仍真起进程）`err=nil` | **存活**（pid 41188；`Wait` 60ms 期限到都没退出） |
  | **`Supervisor.Connect(ctx)`**（组装 + 起读循环，**不等 hello**） | 否（透传给 `Start`） | **2.25ms**（已取消的 ctx）`err=nil` | **存活**（pid 39856） |
  | **`Initialize`→`request`**（等 hello / 等回应） | **是**（`connection.go:337` 的 `case <-ctx.Done()`，全文唯一一处） | **400.3ms**（400ms 期限，孩子 `debug/hang` 挂着不回包）err=`debug/hang: context deadline exceeded (sent $/cancel)` | **存活**（cancel 只是通知，不 kill） |
  | 旁支：**`request` 走 `c.dead`**（连接判死那一腿） | 不是 ctx | **4.9ms**（400ms 期限远未到期就返回）err=`adapter process is gone; in-flight state is unknown: adapter/initialize: no reply (ADAPTER_UNRESPONSIVE)` | **存活** |

  两条附赠事实：**`pending` 在 ctx 取消与判死两条路径上都归 0**（`forget` 有效、不泄漏）；
  **`Child` 只有 `Wait(ctx)` 与 `CloseStreams()`、没有 `Kill`** ⇒ 收尸要么靠"关 stdin 让孩子自己退"（`live` 助手就是这样），
  要么根本没有。**这是盘上现状的陈述，不是我对 REQ-S-1 的建议**；给 backend 的定位是：
  **不可取消的那一段是 `Start`/`Connect`（588ms 量级、已取消仍起进程、无人收），不是 `request`。**
  **更正（T-314 ④，09-25 20:5x）**：上面那句"`Child` 只有 `Wait(ctx)` 与 `CloseStreams()`、没有 `Kill`"
  **不实，原文按"历史不静默改"留着**。`Child` 自己确实没有 `Kill`，但 **`Supervisor.Stop(c, timeout)` 里就是
  `c.cmd.Process.Kill()` + 有期限的 `Wait`**（本轮现读 `internal/process/supervisor.go:460/:466`）。
  真相不是"缺这个能力"，是 **`Connection.Close()` 一步都没走向它**。照那句行事会去新开一个 Kill，
  而那正是 ⑤ 明确禁止的形状。第 25 轮的实现与读数在本节最上面一条。
- **第 23 轮 = 空机复跑（T-302 ③④，09-25 19:42~19:46）· 零代码改动**。三段的原文都在
  `.scratch/adapter/heyen-fp.txt` + `heyen-idle.log`（19:43 这批）与 `legs-r22.py` 的 stdout 里：
  - **① 指纹 / 构建时刻 / HEAD**：`git rev-parse --short HEAD` = **`c46339c`**（19:42:00；主控派发行里写的 `6b28868` 是它的父，
    19:38:34，两笔 `--stat` 都只有 specs 与台账）；`go list -deps` = **`67a1287e29df29c3`**；
    `go test -c` 三面 = `afbc9a1b8fa9b74f` / `c6a53803aa7ce5c1` / `38a594aa65efbca8`；构建时刻 **19:43:18**（起跑 19:43:15）。
    **与 18:35 / 18:54 / 19:12 四代同值** ⇒ "被验的是不是那份码"这一问在四个 HEAD 之间都是"是"。
  - **② 被验面干净的三证**：`git status --porcelain -- core/` = **空**；三面指纹一字不差；
    **`git diff --name-only f25ee6c..HEAD -- core/` = 0 个文件**（这条最硬：从合验那批到现在 Go 面没动过）。
    全仓 porcelain 现读 **8 条**（六本台账 + 两个 `apps/desktop` TS + `scripts/flaky_watch.sh`，都不在 Go 编译面）。
  - **③ 读数与带载那批完全一致**：全量 `exit=0`、**498 PASS / 7 SKIP / 0 FAIL、17 包全 ok、`build failed` 0、无超时 panic**；
    六项 = 7/8/2/8/2/3（**30 条，非绿 0**）；行级带 `LUCERO_REQ_DB2=1` = **4 条全 PASS**（`ok ./cmd/core 0.651s`）。
  - **两条反例（19:45:36→19:46:02 这一腿，前后探针 `oc_now.log` 停在 19:45:04 不动、tasklist 无 go.exe）**：
    摘建行腿 ⇒ `cmd/core` 红 3（`TestLifecycleWriterLandsRowAndShouts`、`…ThreeLegsEachMoveTheRow`、`…RowLandsTheFiveColumns`，
    首行 `sql: no rows in result set`）+ 我包红 **5**；摘改行腿 ⇒ `…ThreeLegsEachMoveTheRow` 红
    （首行 `该把行改成 EXITED/exit_code=3，实际 STARTING / 有=false 值=0`）+ 我包红 **4**；
    我包基线（不过滤）= 32 PASS / 1 SKIP / 0 FAIL；`restored_identical=True`。
  - **同一把尺子当场又抓到我一次假否定**（记下来因为它差点变成"空机证据"）：19:43:42 那条 `ls -t .scratch/test/`
    是在 `cwd=core/` 下跑的相对路径 ⇒ 报"目录不存在"。**目录一直在**（19:44:14 绝对路径可列、19:44:36 起还在被写）。
    判据换成：**探针一律用绝对路径**，并且"空机"这件事只用**同一判据的前后两次一致**来支撑，不用"某目录消失"这种意外。
  - **同一轮内补正（19:49:18，收到巡检那封"HEAD 已换值"之后当场复量）**：我报告里贴的 **`c46339c` 就是当前值**
    （19:49:18 复读未再前进）⇒ **不存在要改的读数**；`6b28868`→`c46339c` 之间 `git diff --name-only … -- core/` = **0 个文件**，
    而我用的是更宽那道（`f25ee6c..HEAD -- core/` = **0**）⇒ ③ 的"Go 面没变"三方独立量到同一结论。
    **顺手把"HEAD 是易腐品"变成可复跑的动作**：`heyen-readout.sh` 印的 HEAD 是**跑那一刻自己量的**；之后任何人要复核这批还不可信，
    只需 `git diff --name-only c46339c..HEAD -- core/` **为空** + 三面指纹同值，**不要求 HEAD 仍等于 `c46339c`**。
    再记一条时间线，免得后来人以为我这批全程空机：`.scratch/test/` 最新项 19:43:15 = `selftest_recheck.log`(19:28:18)、
    19:44:36 起 = `oc_now.log`(末次 19:45:04)、**19:48:27 又出现 `ins_completed.py`** ⇒ test 在我这批**之后**恢复写入；
    三次 `tasklist` 探针（19:45:36 / 19:46:02 / 19:49:18）都无 `go.exe`/`test.exe` ⇒ 它写的是脚本产物，不是 Go 负载。
    **T-305（19:42 落账那句更正）与已交付这批同义 ⇒ 未重跑代码，只补空机复跑与一枚复核章**：
    **账上不钉 HEAD 值**（19:43=`c46339c`、19:49=`c46339c`、19:52 又前进到 `a5ea0da`，全是台账/ specs），
    可复跑的判据写成两句：**`git status --porcelain -- core/` = 空** + **`git diff --name-only f25ee6c..HEAD -- core/` = 0 个文件**
    （`c46339c..HEAD` 同样 0），再叠三面指纹逐字同 19:12 / 19:43 两批（`deps 67a1287e29df29c3`、
    `afbc9a1b8fa9b74f`/`c6a53803aa7ce5c1`/`38a594aa65efbca8`）⇒ 主控那两条判据（**不是"HEAD 等于某个值"**）持续成立。
    **第三份空机读数（19:52:50→19:53:02，脚本自带两证了）**：起跑前后 `.scratch/test/` 最新项都是
    `ins_completed.py` **19:48:27 未变**、`tasklist` 两次都无 `go.exe` ⇒ 全量 **`exit=0`、498 PASS / 7 SKIP / 0 FAIL、17 包全 ok**，
    与 19:12（带载）、19:43（空机）**三批数字完全一致**。
    **同一轮里我又踩了一次自己三分钟前刚记下的坑**：19:51:02 那次探针我又拿相对路径 `ls .scratch/test/`（cwd 在 `core/`）
    ⇒ 又读到"目录不存在"这个假否定，绝对路径重探才是上面那个值。
    **这条教训已经从"写在账本里"落进工具本体**：`heyen-readout.sh` 现在有 `idle_probe()`，
    在**起跑前与收完后各印一行**（`.scratch/test/` 最新项的绝对路径 mtime + `tasklist` 有无 `go.exe`），
    19:52:50 / 19:53:02 那两行就是它自己打出来的 ⇒ **这份"空机"从此不依赖我手敲命令时记不记得用绝对路径**。
  - **窗口何时关闭，写死在这里，免得我的"core/ 为空"被以后的人当常态读**：
    三批读数的干净时刻分别有实测撑着 —— 19:12 那批是 19:11:54 量到 `core/` 空；19:43 那批是 19:49:18 / 19:51:02 / **19:53:23 三次都 = 0**；
    19:52:50→19:53:02 那批复用的是同一时刻前后的 `ins_completed.py` 19:48:27 未变 + 无 `go.exe`。
    **19:54:05 复量：窗口关闭** —— `git status --porcelain -- core/` 现在 = **2 条**
    （`M core/internal/notification/service.go` mtime 19:53:34、`M core/api/http/notifications.go` mtime 19:54:02，
    `git diff --stat` = +73/−7，是通知 `HANDLED` 那一格），**backend 又开始写了**。
    **两条限定**：① 这两个包**都不在我三面指纹的依赖里**（`go list -deps ./internal/adapter ./internal/process ./cmd/fake-adapter`
    不含 `internal/notification`/`api/http`；我三路径脏件 = **0**），所以**我的三面读数不被它影响**；
    ② 但"17 包全量"那一份**跨了它**——所以 **19:43 / 19:52 那两份全量读数的有效期就到 19:53:3x**，
    之后任何人要引用"全量 exit=0"，得在 `core/` 重新变干净之后再取一次。**我不重跑**：现在跑就是拿一棵正在被改的树做合验，
    正是 09-24 那批作废读数那一族。这一条留给主控决定下一个窗口。
  - 头部五字段自检（T-276）：`^- Stage:`=1、`^- Status:`=1、`^- Last verified action:`=1、`^- Next action:`=1、
    `^- Verification:`=1；`^## `=12。`grep -rn ZMUT core/` = **0**。
- **第 22 轮 = 合验交账（T-297 已执行，09-25 19:11~19:15）**。判据命令：`bash .scratch/adapter/heyen-readout.sh`
  + `LUCERO_REQ_DB2=1 … -run 'TestLifecycle…' ./cmd/core/` + `.scratch/adapter/legs-r22.py`（两条反例）。
  - **① 被验树指纹 / 构建时刻 / HEAD**（19:12:39→19:12:43，两个时刻各自 `date -Iseconds`）：
    `git rev-parse --short HEAD` = **`f25ee6c`**（19:04:01，父 `a9f975e`）；`go list -deps` 三面 = **`67a1287e29df29c3`**；
    编译指纹 `internal/adapter afbc9a1b8fa9b74f`、`internal/process c6a53803aa7ce5c1`、`cmd/fake-adapter 38a594aa65efbca8`
    —— **与 18:35、18:54 两次一字不差**（这一代里我的被验面没有动过，跨了 `95b01ca`→`4b4739e`→`f25ee6c` 三个提交）。
  - **② 工作区原样贴**（19:12:4x，`git status --porcelain` 共 **9 条**，**我没有为了"看着干净"动过任何人的文件**）：
    ` M .qoder/progress/{adapter,backend,db,frontend,orchestration,test}.md`、
    ` M apps/desktop/src/__tests__/live-core.test.ts`、` M apps/desktop/src/stores/panel.ts`、` M scripts/flaky_watch.sh`。
    **它们为什么不影响被验面**：`git status --porcelain -- core/` = **空输出**（六本台账是文档、两个 TS 在 `apps/desktop`、
    `flaky_watch.sh` 是 `test` 的脚本，都不进 Go 编译面），合验的指纹与判据都来自 `core/`。
  - **③ 协议侧六项逐条**（全量 `exit=0`、**498 PASS / 7 SKIP / 0 FAIL、17 包全 `ok`、`build failed` 0、无超时 panic**）：
    握手 7 / 能力核定 8 / `CarriesTurnRef` 2 / 票号↔`execution_id` **两侧** 8 / 丢弃到审计接缝 2 / 链 3 = **30 条，非绿 0**，
    覆盖的用例行清单在 `heyen-fp.txt` 里逐条印着（本报告不写任何"这一格是谁写的"，按 T-295）。
  - **行级那一半（今天第一次真跑，带 `LUCERO_REQ_DB2=1`）**：`TestLifecycleColumnAssertionsHaveTeeth`、
    `TestLifecycleWriterLandsRowAndShouts`、`TestLifecycleThreeLegsEachMoveTheRow`、`TestLifecycleRowLandsTheFiveColumns`
    = **4 条全 PASS**、`ok ./cmd/core`。
  - **两条反例（§17 "每腿一条"）**：摘建行腿 ⇒ cmd/core **3 条红** + 我包 **5 条红**；摘改行腿 ⇒
    **`TestLifecycleThreeLegsEachMoveTheRow` 红**（`…该把行改成 EXITED/exit_code=3，实际 STARTING / 有=false 值=0`）+ 我包 **4 条红**。
    我包基线（不带 `-run` 过滤）：**32 PASS / 1 SKIP / 0 FAIL**，含子用例 6 条。**CO-50① 判闭。**
  - **并发负载披露**：`test` 的 FL-10 电池与这批同时跑（19:12:36 见其日志在涨、19:14:03 又出现新文件）
    ⇒ 本批是**带载读数**；我的断言全是判决型不是量级型，且**全程零红**，所以未触发"红先单包复跑再归因"那条。
  - **一次我自己差点交上去的空判决**：反例脚本第一版用 `-run 'TestLifecycle…'` 过滤**我自己的包**（我包用例名不匹配），
    得到 `PASS=0 FAIL=0 exit=0`，长得像"我的包不敏感"——**是那个可疑的 0 让我回去看的**，改成不过滤重跑才有上面那组数。
    记这条是因为它属于我最熟的那一族：**"`-run` 空匹配也报 ok"，而"0 红"必须先证明它跑过。**
- **第 21 轮（T-295 口径落地，09-25 18:53~18:55）· 零代码改动**。判据命令仍是 `bash .scratch/adapter/heyen-readout.sh`：
  （已按新口径改成印"覆盖的判据行"，输出零人名字段；日志 `.scratch/adapter/heyen-run-185409.log`、汇总 `heyen-fp.txt`）：
  - **① 被验树指纹**：HEAD `4b4739e`（18:47:07）、`deps 67a1287e29df29c3`、
    `bin internal/adapter afbc9a1b8fa9b74f` / `internal/process c6a53803aa7ce5c1` / `cmd/fake-adapter 38a594aa65efbca8`，
    构建时刻 **18:54:13**（起跑 18:54:09，两个时刻各自 `date -Iseconds`）。
  - **② 工作区**：`git status --porcelain` **非空 = 15 条**（18:54:09 实读；含我自己这本 `adapter.md`）。
    **本轮报告里我不写任何一条"这是谁写的"** —— 归属在上一节正文里，按 T-295 只留台账。
  - **③ 全量 + 六项逐条**：`exit=0`、**498 PASS / 7 SKIP / 0 FAIL、17 包全 `ok`、`build failed` 0、无超时 panic**。
    六项覆盖的判据行（= 30 条，非绿 0）：
    握手 7 = `TestNegotiate` `TestNewHandshake` `TestConnectionStreamBeforeInitializeIsRefused`
    `TestConnectionRefusesBeforeInitialize` `TestConnectionInitializeStartsThePump`
    `TestFakeAdapterRefusesBeforeHandshake` `TestFakeAdapterHandshakeSatisfiesCore`；
    能力核定 8 = `TestEffectiveCapabilities` `TestSQLiteStoreRecordsOneProbeEndToEnd` + Probe 六条
    （`…RecordsWhatTheHandshakeSaid`/`…TriesOnceAndNeverRetries`/`…KeepsTheTwoRefusalsApart`/
    `…RefusesAnotherMajorWithoutGuessing`/`…GivesUpOnTheCallersDeadline`/`…CarriesEveryRequiredAdapterInstanceField`）；
    `CarriesTurnRef` 2 = `TestStreamNotificationsMatchContractTables` `TestStreamFrameMustBeNotification`；
    票号↔`execution_id` 两侧 8 = `TestConnectionDedupsAndTracksTurn` `TestConnectionDropsUnboundTurnRef`
    `TestConnectionDropsContradictoryIdentity` `TestConnectionAgainstRealFakeAdapter` + 门控四条
    （`…OffEchoesWhatCoreBound`/`…OnContradictsTheBinding`/`…MovesNoFrameShape`/`…IgnoresOtherValues`）；
    丢弃接缝 2 = `TestDroppedFrameReachesTheAuditSeam` `TestEdgeWithoutARowWritesNothingAndReturnsAuditSentinel`；
    链 3 = `TestConnectionChainClosesBeforeItOpens` `TestFakeAdapterQueueDrainsAtTurnFinishedNotAtNextSend`
    `TestFakeAdapterChainIsClaimedThenDispatched`。
    **"全" = 17 包，不含 `core/test/`（六条全量的端到端 `turn_evidence_test.go` 在那儿，归 `test` 带）与 `scripts/`。**
  - **上一条 18:36 那代读数（`exit=1`、4 包 build failed）已被本轮取代**，两份都留着：前者是"预检会拦住"的现场证据，
    后者是"预检已过、只差未提交"的现场证据。**别拿前一份当现状。**
  - 头部五字段自检（T-276）：`^- Stage:`=1、`^- Status:`=1、`^- Last verified action:`=1、`^- Next action:`=1、
    `^- Verification:`=1；`^## `=12。
- **第 20 轮（T-288 预备，09-25 18:32~18:36）· 零代码改动**：
  - **判据命令 = `bash .scratch/adapter/heyen-readout.sh`**（一次产出 T-288 要的三段；今天**真跑两次**，
    第一次暴露我自己的 `${pat)}` 拼写错，第二次才是下面这组数）。日志
    `.scratch/adapter/heyen-run-183444.log` 与 `heyen-run-183551.log`，汇总 `heyen-fp.txt`；
    `.scratch/` 在 `.gitignore:31` 里 ⇒ **这条预备跑不会把被验树弄脏**（脚本连编译出的 `.test.exe` 都落在 `.scratch/adapter/` 里并即时删掉，
    不碰 `/tmp`）。
  - **① 被验树指纹 + 构建时刻 + HEAD**：HEAD `95b01ca`（09-25 18:29:43）；`deps 67a1287e29df29c3`；
    编译指纹 `internal/adapter afbc9a1b8fa9b74f`、`internal/process c6a53803aa7ce5c1`、`cmd/fake-adapter 38a594aa65efbca8`；
    构建时刻 18:34:48 / 18:35:5x 两次，**三包指纹两次一致**。
  - **② 工作区**：`git status --porcelain` **非空**（18:35 读到 10 条：5 本台账 + frontend 2 + backend 的
    `cmd/core/sendpipe.go`(M)/`?? sendpipe_test.go` + `internal/task/service.go`(M)）。**"我的在制品"不在其中**
    （三件都已进 HEAD `2c56023`）⇒ 触发时 ② 通不通只取决于 backend/frontend 收口 + 台账，与我无关。
  - **③ 六项逐条读数**（`-v` 全量 304 PASS / 2 SKIP / 0 FAIL、13 包 ok、**4 包 build failed**）：
    握手 7/7、能力核定 8/8、`CarriesTurnRef` 2/2、票号↔`execution_id` 8/8、丢弃接缝 2/2、链 3/3，**非绿合计 0**。
    **那 4 个 build failed 全不在我被验的三包内**，根因一条：`internal/task/service.go` 未定义符号（backend 在途）。
    **"全" = 17 包，不含 `core/test/`（`test` 的门）与 `scripts/`**；六条全量的端到端在 `core/test/turn_evidence_test.go`，
    不在我的读数面上 —— 合验时那条由 `test` 自己带。
  - **头部五字段自检**（T-276 那把尺，本轮又用了一次）：`^- Stage:`=1、`^- Status:`=1、`^- Last verified action:`=1、
    `^- Next action:`=1、`^- Verification:`=1、`^## `=12。**旧的四段全部降级留档，没有删**。
- **第 19 轮（T-276 落规，09-24 10:53~10:56）**：
  - **头部自检（这条就是新规矩的判据本身）**：`grep -c '^- Stage:'`、`^- Status:`、`^- Last verified action:`、`^- Next action:`
    **四个都必须 = 1**（多于 1 = 有旧行没降级成留档，等于 0 = live 指针丢了）；
    同时 `grep -c '^## '` 仍 = **12**、`wc -l` 变化 = 本次写入行数。四个数与两条结构数一起看，
    是因为旧的四段我选择**留在同一节下方**而不是删掉，光数节标题看不见它们。
  - **头部那次"当场读数"不是从上一批抄的**：`go test -count=1 -timeout 900s -v ./internal/... ./cmd/... ./api/...`
    起 **10:54:03**、收 **10:54:11**（两个时刻各自 `date -Iseconds`），**exit=0，469 PASS / 6 SKIP / 0 FAIL，17 包全 `ok`、
    `build failed` 0、`panic: test timed out` 0**；逐包 `internal/adapter` 53、`internal/process` 32/1SKIP、`cmd/fake-adapter` 26/1SKIP；
    `gofmt -l` 我三目录空、`grep -rn ZMUT core/` = **0**；HEAD 实取 `978a72e` 10:46:22。**"全" = 17 包，不含 `core/test/` 与 `scripts/`**。
    本轮**零代码改动**（只动账本头部与这两段），所以 10:46 那批的三项交账（144 insertions / 3 条 `??` / 变异 6 FIRED）仍是最新形状。
- **第 18 轮（T-275 黑盒注入门，09-24 10:37~10:46）**：
  - **交付前最后一次全量跑：起 10:46:11、收 10:46:19（两个时刻各自 `date -Iseconds` 求值），`exit=0`**，
    `CGO_ENABLED=0 go test -count=1 -timeout 900s -v ./internal/... ./cmd/... ./api/...` ⇒
    **469 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`、`^ok ` = 17 个包**、`build failed` 0、无 `panic: test timed out`、
    `grep -rn ZMUT core/` = **0**、`gofmt -l` 我三目录空。**"全" = 17 包，不含 `core/test/`（`test` 的门）与 `scripts/`**。
    **469 比 10:06 的 464 多 5 条：我的 +4（`cmd/fake-adapter` 逐包 **22 → 26 PASS**）+ 别人的 +1**（`internal/adapter` 53、
    `internal/process` 32/1SKIP 一字未变）。逐包：`internal/adapter` **53/0/0**、`internal/process` **32/1/0**、
    `cmd/fake-adapter` **26/1/0**（那条 SKIP 是 `TestHelperFakeAdapter`，helper 门的既有形状，不是本轮新增）。
  - **变异批 7 发（`.scratch/adapter/mut-r18.py` + `mut-r18b.py`）**：**有效 6 发全 FIRED，另有 1 发是我自己的无效变异（不记 SILENT）**：
    b1 条件放宽成"非空即开" ⇒ `...IgnoresOtherValues` FIRED（`gate="0": turn/claimed carried "0194f4c2-…"`）；
    b2 只让 claimed 说假话 ⇒ FIRED（`turn/dispatched carried execution_id "exec-1", want the injected …`）；
    b3 根本不读环境 ⇒ FIRED（**这一发同时回答"测试到底在看真子进程吗"**）；
    b4 删掉 stderr 喊话 ⇒ FIRED（`the gate never announced "LUCERO_FAKE_ADAPTER_BAD_EXECUTION_ID"`）；
    **b5 第一版无效**（多余键无条件加 ⇒ 开/关两边都有，形状对拍天然看不见），**b5' 修成"只在门开着时多一个 `turn_id` 键" ⇒ FIRED**
    （`frame 0 param keys = [… turn_id …], want off's set […]`）；
    b6 注入值换成空串 ⇒ FIRED 两条（`Core could not decode our own turn/claimed notification: INVALID_PARAMS: turn/claimed has no execution_id (CO-8)`）。
    每发都带"与基线输出是否不同 = True"，还原全部 `assert 读回==原字节`、`restored_identical=True`、基线先跑（4 PASS/0 FAIL）。
  - **REQ-T11 三句（HEAD 实取 `978a72e` 09-24 10:46:22）**：
    ① `git diff --stat HEAD -- <我三路径>` = `main.go 32+/3-`、`probe_store_sqlite.go 36+`、`probe_store_test.go 67+`、
    `supervisor.go 9+/7-` ⇒ 合计 **4 files changed, 144 insertions(+), 10 deletions(-)**；
    ② `git status --porcelain -- <同范围>` = **4 行 ` M` + 3 条 `??`**
    （`??` = 本轮新增 `badexec_test.go`，以及上一轮的两件 `lifecycle.go`/`lifecycle_test.go`）；
    ③ 逐新文件 `git diff --no-index --stat /dev/null`：**`badexec_test.go` 156 insertions**（sha256 `ae1a4578e981e428412cfa4d…`）、
    `lifecycle.go` 236（`f4903acedf72acd2a38ecffa…`，本轮未动）、`lifecycle_test.go` 430（未动）。
    改动的 `main.go` 内容指纹 `fb6bc82e643f617a8024ee7d…`（634 行）。
- **第 17 轮（T-263 只读解析，09-24 10:01~10:07）**：
  - **交付前最后一次全量跑：起 10:06:19、收 10:06:28（两个时刻各自 `date -Iseconds`），`exit=0`**，
    `CGO_ENABLED=0 go test -count=1 -timeout 900s -v ./internal/... ./cmd/... ./api/...` ⇒
    **464 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`、`^ok ` = 17 个包**、`build failed` 0、无 `panic: test timed out`、
    `grep -rn ZMUT core/` = **0**、`gofmt -l` 我三目录空。
    **"` 全 "这个字的集合照第 16 轮定下的口径写**：= `core/` 下 17 包，**不含 `core/test/`（`test` 的门）与 `scripts/`**。
    **458 → 464 这 6 条里我加的是 3 条**（`internal/adapter` 逐包 **50 → 53 PASS**，正对我新写的三条），
    另外 3 条不是我加的（`internal/process` 32/1SKIP、`cmd/fake-adapter` 22/1SKIP 一字未变）。
  - **变异批 5 条（`.scratch/adapter/mut-r16.py`，基线先跑、每例改→跑→还原在同一条命令内、`restored_identical=True`）**：
    q1 静默返空串 **FIRED**；q2 `%w`→`%v` **FIRED**（`err = resolve adapter ghost: aggregate row not found, want errors.Is(...)`）；
    q3 读 `health` 不读 `id` **FIRED**（`resolved "HEALTHY", want the id RecordProbe wrote`）；
    q4 读口 miss 时顺手写 **FIRED**（`instance rows went 1 -> 2`、`journal rows went 1 -> 2`）；
    **q5 摘掉 miss 分支里的 `rows.Err()` 先查 —— SILENT，且这条 SILENT 是我在跑之前就写进代码注释的预测**
    （真 SQLite 单行 SELECT 在进程内造不出迭代中途报错）。每条 FIRED 都带"与基线输出是否不同 = True"。
  - **REQ-T11 三句（HEAD 实取 `a3b2910` 09-24 10:04:11）**：
    ① `git diff --stat HEAD -- <我三路径>` =
    `probe_store_sqlite.go | 36 +` / `probe_store_test.go | 67 +` / `supervisor.go | 16 +++---（+9 / −7）`
    ⇒ **3 files changed, 112 insertions(+), 7 deletions(-)**；
    ② `git status --porcelain -- <同范围>` = **3 行 ` M` + 2 条 `??`**（那两条 `??` 仍是 `lifecycle.go`/`lifecycle_test.go`，本轮未动）；
    ③ 逐新文件 `--no-index --stat` 本轮**无新文件**（两个改动文件都已被 HEAD 跟踪，数字在①里）；
    内容指纹：`probe_store_sqlite.go` sha256 `60e63a8dc6f941de3d227f88…`（300 行）、`lifecycle.go` 仍 `f4903acedf72acd2a38ecffa…`（236 行）。
    **mtime 只用来排先后**（`probe_store_sqlite.go 10:05:35` 是变异还原写回、`probe_store_test.go 10:03:45`），
    "改完没再改"由 10:06:19 那次跑 + 上面两个哈希共同证。
- **第 16 轮（T-255 收单，09-24 09:03~09:05）· 零代码改动**：
  - **"` ` 全"这个字的集合，从今天起写死**：我的交付集合 = `core/` 下 `./internal/... ./cmd/... ./api/...` = **17 个包**，
    **不含 `core/test/`（`test` 角色的门）与 `scripts/`**。09:03 那次就撞上代价：主控点名的预期红
    `TestEventCatalogMatchesJournalConstants` 在 `core/test/errcode_mapping_test.go:748`，我的 17 包日志里 0 命中。
  - **两趟读数**（同一棵树，间隔 30 秒）：`-v` 全量 09:03:37→09:03:46 ⇒ **458 `--- PASS` / 6 SKIP / 0 FAIL、17 个 `^ok `、`build failed` 0**
    （`grep -c '^--- PASS'` 从日志文件重算过一次，因为那趟的 `exit=` 回显整段丢失）；
    quiet 重跑 09:04:16→09:04:24 ⇒ **exit=0**、17 行 ok/FAIL 里 0 条 `FAIL`。**458 vs 08:44 的 457 = +1，不是我加的**
    （我三包仍是 50 / 32+1SKIP / 22+1SKIP）。
  - **单跑那一条别人点名的红**：`go test -count=1 -run TestEventCatalogMatchesJournalConstants -v ./test/`
    09:04:53 ⇒ **`--- PASS`**，自证行原文 `C8-12：目录 64 类 / 常量 64 个（扫 1 个非测试 .go）；目录独有 []、代码独有 []`
    ⇒ 双向相等、两侧都空 ⇒ 主控 09:01 那句"现在有一条红"到 09:04 已过期（**只报读数**）。
  - **REQ-T11 三句（HEAD 实取 `20ea3c5` 09-24 08:57:19）**：本轮零 `.go` 改动 ⇒
    `git diff --stat HEAD -- <我三路径>` 仍是 `supervisor.go | 16（+9 / −7）`；
    `git status --porcelain -- <同范围>` = **1 行 ` M` + 2 条 `??`**；未跟踪件数字未变：
    `lifecycle.go` **236 insertions**（sha256 `f4903acedf72acd2a38ecffa…`，09:03 复算同值）、`lifecycle_test.go` **430 insertions**。
    `grep -rn ZMUT core/` = **0**、`gofmt -l` 我三目录空。
- **第 15 轮（CO-49 导出 + CO-50① 复量，09-24 08:41~08:44）**：
  - **交付前最后一次全量跑：起 08:44:16、收 08:44:24（`date -Iseconds` 各一次），exit=0**，
    **457 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`、`^ok ` = 17 个包全跑、`build failed` 0 命中**、无 `panic: test timed out`、
    `grep -rn ZMUT core/` = **0**、`gofmt -l` 我三目录空。**453 → 457 那 4 条不是我加的**：
    我三包逐包重跑仍是 **50 / 32+1SKIP / 22+1SKIP**（与 08:17 那份一字不差），红的是 backend 08:3x 新加的用例。
    上一条 08:13:54 那次 `cmd/core [build failed]` 与 08:18 那次 `TestLauncherCarriesARecorder` panic **今天都不复存在**
    （08:42:56 整包 `ok`、08:44:24 全量 0 红）⇒ 那两条读数**按时间留着，别再被人拿来当现状**。
  - **内容指纹（比 mtime 硬）**：`lifecycle.go` sha256 **`f4903acedf72acd2a38ecffa…`**（08:44:24 复算与 08:43:22 同值），
    236 行；mtime 08:43:22 是"摘除→还原"写回的痕迹，**它早于 08:44:16 那次跑**，而"内容没被偷改"由哈希证、不由 mtime 证。
  - **双向自证（CO-50① 的那三条，整包级、非抽查）**：基线 `cmd/core` 带 flag **`ok` exit=0**；
    摘建行腿 ⇒ **`--- FAIL: TestLifecycleWriterLandsRowAndShouts` + `--- FAIL: TestLifecycleRowLandsTheFiveColumns`**；
    摘改行腿 ⇒ **整包仍 `ok`**。两次变异与基线的输出**都不是逐字相同**（`r1=False`、`r2=False`，只差计时行），
    所以"仍 ok"不是变异没生效，而是**那条判据面真的没人断终态边**。还原全部 `assert 读回==原字节`、`restored_identical=True`、标记 0 残留。
  - **REQ-T11 三句（HEAD 实取 `064b383` 09-24 08:33:41）**：
    ① `git diff --stat HEAD -- <我三路径>` = `core/internal/process/supervisor.go | 16 +++++++-------（+9 / −7）`
    （`lifecycle.go`/`lifecycle_test.go` 未跟踪，不进 diff，见②③）；
    ② `git status --porcelain -- core/internal/adapter core/internal/process core/cmd/fake-adapter adapters/fake` =
    **1 行 ` M`（supervisor.go）+ 2 条 `??`（`lifecycle.go`、`lifecycle_test.go`）**；
    ③ 逐未跟踪件 `git diff --no-index --stat /dev/null` = `lifecycle.go` **1 file changed, 236 insertions(+)**（导出那轮从 233 涨到 236）、
    `lifecycle_test.go` **430 insertions**（未动）。
- **第 14 轮补（08:17~08:19）· T-227 ② 那条双向自证这次是真跑完的，不是配方**：
  `CGO_ENABLED=0 LUCERO_REQ_DB2=1 go test -count=1 -timeout 300s -run TestLifecycleRowLandsTheFiveColumns -v ./cmd/core/`
  三份读数：**基线 `--- PASS (0.19s)`** → **摘掉建行腿 `--- FAIL`**（`lifecyclecolumns_test.go:430: process_instances 该恰好一行…: sql: no rows in result set`）
  → **摘掉改行腿仍 `--- PASS`**。第三份不是坏消息藏着不报，它量出一个覆盖面缺口，已放大范围排除"别的用例在守"之后写进 **CO-50①**。
  摘除都是一处早退、**每份都带"与基线逐字是否相同"的比对**（`False`/`False`，即三次输出确实不同 ⇒ 变异进了判据的观察面），
  还原全部 `assert 读回==原字节`、标记零残留。
  **顺手补一条比 mtime 更硬的凭据**：本轮反复"字节级摘除→还原"之后，`lifecycle.go` 的 mtime 已经是 08:18:25，
  拿它证明"我没改过码"失效了 ⇒ 现在给内容落指纹（08:19 取，`sha256sum`）：
  `lifecycle.go` = **`f5c5e9fa8ffc1a32c4e16e0d…`**、`lifecycle_test.go` = **`68eec9216bd4815054daf151…`**，
  同框的还有行数 **233 / 430** 与我三包用例数 **50 / 32+1SKIP / 22+1SKIP**。
  **一条自报（同族第四次的变体）**：我本来要把这段用 `python -c "…"` 写进账本，**正文里的反引号在双引号 shell 串里被当命令替换执行**，
  当场炸出 `go: command not found` / `syntax error near unexpected token '('` 一串 —— 那正是 09-23 把我账本从 12 段写成 24 段的同一族。
  这次没造成破坏（python 在拼接前就失败、文件未被写；`grep -c '^## '` 仍是 12、无"第 14 轮补"残段），
  改法是**账本一律走 Edit/Write**（这次就是），bash 里永远不携带含反引号的正文。
- **第 14 轮（T-240 收单，09-24 08:12~08:14）**：
  - **我对盘上最后一次的读数不是绿的，而且红不在我这**：全量跑 起 08:13:54 / 收 08:14:02（`date -Iseconds` 各取一次），
    **exit=1，413 `--- PASS` / 2 `--- SKIP` / 0 `--- FAIL`，`^ok ` = 16 个包**，第 17 个是
    **`FAIL github.com/SadCat18/lucero/core/cmd/core [build failed]`**（`undefined: strconv` / `undefined: process`）。
    `CGO_ENABLED=0 go build ./...` **exit=0** ⇒ 坏的是**测试构建**，生产码不缺件；`PASS` 从 453 掉到 413 就是
    `cmd/core` 那 ~38 条今天压根没跑。**这是 08:14 的现场，不是谁的判决**——backend 那一刻正在写这个文件（三次 mtime 移动）。
    **我只报读数、不据此判 backend**（记忆里那条"别拿别人的红判别人的批次"）。
  - **我三包在 08:14:02 那次全量里都是 `ok`**：`internal/adapter 2.166s`、`internal/process 5.778s`、`cmd/fake-adapter 3.563s`；
    **我最后一次"作为交付凭据的全绿跑"仍是 07:46:28→07:46:36（exit=0，453 PASS / 6 SKIP / 0 FAIL，17 包全 `ok`）**。
    `grep -rn ZMUT core/` = **0**、`grep -rn MUTANT-R13 core/` = **0**（自证脚本的临时早退没留残）。
  - **一条 mtime 的用法纠错（因为它会影响别人判断"你有没有改过码"）**：本轮那次"摘除→还原"把 `lifecycle.go` 的
    **mtime 推到了 08:13:36**，而内容仍是 07:36:23 那份（脚本内 `assert 读回==原字节`）。⇒ **从现在起，"我没改码"只能靠 `cmp`/字节 assert 证，
    不能再拿 mtime 早于某次跑当证据**；这条正是记忆里"mtime 只证最后写过、不证正在写"的加强版。
  - **留给 `cmd/core` 恢复可编译后的那一条判据（T-227 ② 的真正形态，配方写死在这里）**：
    ① `CGO_ENABLED=0 LUCERO_REQ_DB2=1 go test -count=1 -timeout 300s -run TestLifecycleRowLandsTheFiveColumns -v ./cmd/core/`
    ⇒ 必须 **`--- PASS`**；② 临时把我 `lifecycle.go` 的建行腿摘掉（一处早退）再跑同一条 ⇒ 必须 **`--- FAIL`**，
    且**不许变成 SKIP、不许变成 build failed**；③ 还原按字节、`cmp` 证一次。
    **两步都要跑，只跑 ① 就是"绿过一次"的旧病**；我这边随时配合重取，不需要主控或 backend 猜我的形状。
  - **REQ-T11 三句（HEAD 实取 `bc1fa9b` 09-24 08:00:38）**：本轮零代码改动 ⇒
    `git diff --stat HEAD -- <我三路径>` 仍是 `supervisor.go | 16（+9 / −7）`；
    `git status --porcelain -- <同范围>` = **1 行 ` M` + 2 条 `??`**（`lifecycle.go` 233 行、`lifecycle_test.go` 430 行，
    `--no-index --stat` 两处数字与 07:3x 那次相同）；**本轮没有新文件**，`--no-index` 那一问无新对象。
- **第 13 轮补记（T-227 两件交账，09-24 07:31~07:37）**：
  - **交付前最后一次全量跑：起 07:37:23、收 07:37:34（两个时刻各自 `date -Iseconds`），exit=0**，
    `CGO_ENABLED=0 go test -count=1 -timeout 900s -v ./internal/... ./cmd/... ./api/...` ⇒
    **452 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`、17 个包全 `ok`、一个都没跳**、无 `panic: test timed out`。
    `grep -rn ZMUT core/` = **0**、`grep -rn MUTANT-R13 core/` = **0**（自证脚本的标记不留盘）、`gofmt -l` 我三目录空。
    **447 → 452 那 5 条不是我加的**：我这三包一条没变（`internal/process` 重跑仍是 **32 PASS / 1 SKIP / 0 FAIL**），
    `internal/adapter`/`cmd/fake-adapter` 本轮**没有文件改动**（mtime 07:17:57 之后我只动过 `lifecycle.go` 的注释），
    所以只重跑了 `internal/process` 一包 + 全量 17 包；那 5 条来自别人在 07:23 之后的推进，我不认领。
    源文件 mtime 全部早于起跑：`lifecycle.go 07:36:23`、`lifecycle_test.go 07:21:17`、`supervisor.go 07:17:57`。
  - **REQ-T11 三句（HEAD 实取 `f3c845a` 09-24 07:37:07）**：
    ① `git diff --stat HEAD -- <我三路径>` = `core/internal/process/supervisor.go | 16（+9 / −7）`；
    ② `git status --porcelain -- <同范围>` = **1 行 ` M` + 2 条 `??`**（`lifecycle.go`、`lifecycle_test.go`）；
    ③ 逐未跟踪件 `git diff --no-index --stat /dev/null core/internal/process/lifecycle.go` =
    **1 file changed, 233 insertions(+)**（07:18 交账时是 226，多的 7 行就是 ② 那条 `landedStatus` 引到契约第 20 条 ⑤ 的注释），
    `lifecycle_test.go` 仍 **430 insertions**（本轮一字未动）。
  - **双向自证（07:3x 跑，脚本 `.scratch/adapter/selfproof-r13.py`）——两句分开写**：
    **在我自己那一面成立**：`r1` 摘建行腿 ⇒ 顶层 **5 FAIL / 27 PASS / 1 SKIP**，红的正是 5 条真库用例，
    首行 `no process_instances row for proc-a`；`r2` 摘改行腿 ⇒ **4 FAIL**，首行 `status = STARTING, want EXITED`；
    `r1+r2` ⇒ 5 FAIL。**0 SKIP、0 BUILD-BROKEN**、还原在同一条命令内 `assert` 字节相同、`restored_identical=True`。
    **在主控点名那一格今天证不出来（成因已实测，不是我偷懒）**：`grep -rn LifecycleStore core/cmd/core/` = **0**，
    带 `LUCERO_REQ_DB2=1` 单跑 `TestLifecycleRowLandsTheFiveColumns` 时**摘与不摘我的写口，输出逐字只差计时行**
    （`diff` 只有 2 行：`0.244s` vs `0.248s`），两句失败原文完全同一：
    `lifecyclecolumns_test.go:290: LUCERO_REQ_DB2=1 但这段还没接上真写路径…` ⇒ 它红是红在 **backend 那格没接**，
    不是红在写口。装配落地后同一命令（带 flag、`-run` 单跑、要求 `--- FAIL` 且不许变 SKIP / build failed）就是那一格的验收判据；
    **我没有去动 `cmd/core`**（T-227 明写那三格归 backend，T-225③ 已告诉他上一格不等了）。
- **第 13 轮（写口落地）的交付证据**：
  - **交付前最后一次全量跑（认这条）：起 09-24 07:23:48、收 07:23:56（两个时刻各自 `date -Iseconds` 求值），exit=0**
    ⇒ **447 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`、17 个包全 `ok`、无 `panic: test timed out`、`ZMUT` 0、`gofmt -l` 空**，
    逐包 50 / **32** / 22 PASS（`internal/process` 含 1 条既有 SKIP）。
    三个源文件的 mtime 全部早于起跑：**`lifecycle.go 07:17:32`、`lifecycle_test.go 07:21:17`（那个 mtime 是负向对照还原回来的时间，字节与 07:18 那次跑的对象 `assert` 相同）、`supervisor.go 07:17:57`**。
    上一条 07:18:08→07:18:16 的同款跑法读数完全一致（447/6/0/17），它现在是"负向对照之前那一次"，两段都留着因为中间我动过测试文件。
  - **交付前最后一次全量跑：起 09-24 07:18:08、收 07:18:16（两个时刻各自 `date -Iseconds` 求值），exit=0**，
    `-count=1 -timeout 900s -v ./internal/... ./cmd/... ./api/...` ⇒ **447 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`**、
    `^ok ` = **17 个包、一个都没跳**、`panic: test timed out` 0、`grep -rn ZMUT core/` **0**、`gofmt -l` 我三目录空。
    日志 `.scratch/adapter/final-r13.log`；`supervisor.go`/两个新文件 mtime 均 **< 07:18:08**。
    **438 → 447 这 9 条的来历分开写**：我自己 +8（`internal/process` 24 → **32 PASS**，正对我新写的 8 条顶层用例），
    另外 **+1 不是我的**（db/backend 在 07:10 之后自己推进的），我没去认领它。
  - **逐包单跑**（`-count=1 -timeout 300s -v`，各自 exit=0）：`internal/adapter` **50/0/0**、
    `internal/process` **32 PASS / 1 SKIP / 0**、`cmd/fake-adapter` **22/1/0** ⇒ 我的三包 **104 PASS / 2 SKIP / 0 FAIL**。
    含子用例的 `--- PASS` 总数在 `internal/process` 是 38（32 顶层 + 6 子用例），两个数别混着报。
  - **变异批 9 条，9/9 FIRED、0 条 SILENT、0 条 BUILD-BROKEN，`restored_identical=True`**
    （`.scratch/adapter/mut-r13.py`：字节级备份 + **还原在同一条命令内** + 还原后 assert）：
    m1 create 记错事件类型→FIRED(`TestStartLands`)；m2 0 行变静默→FIRED，原文
    `shout = [], want exactly one 0-row line (幂等 and 从来没成功过 must stay separable)`；
    m3 guard 跟着状态走→FIRED，原文 `append event process_exited for proc-a v3: UNIQUE constraint failed: event_journal.event_id`
    （**这条就是我原来的写法**，红得对、错在我）；m4 guard 用我没写过的状态→FIRED(`TestExitMoves`)；
    m5 attempt 不自增→FIRED；m6 第二次尝试复用上一行 id→FIRED，原文
    `create-versionless process proc-a: UNIQUE constraint failed: process_instances.id`；
    m7 崩溃预算不进 metadata→FIRED；m8 无行返回 nil→FIRED，原文 `err = <nil>, want ErrNoLifecycleRow`；
    m9 行的出生跟观察时刻走→FIRED(`TestStartLands` 的 `created_at`)。
    **四条我另外单独取了失败详情**（`.scratch/adapter/mut-r13-detail.py`），确认咬在所声称那一层而不是顺带撞红。
  - **合成反例（不架空）之一：import 守卫的负向对照**。**这里要先纠正我自己**：上一版这句我写成"落码前跑过它红过一次"，
    那是**没跑过的说法**（凭"应该会红"写下的），发现后当场补跑，下面是补跑的真实读数：
    把 `allowed` 那张白名单临时清空（`map[string]bool{}`）⇒ **exit=1**、原文
    `lifecycle.go imports .../internal/eventjournal - the persistence surface is lifecycle.go only (T-211/CO-43)`
    与 `.../internal/platform/db` 同形两条 ⇒ 守卫确实看得见 import，不是只看文件名的摆设；
    还原在同一条命令内完成，`assert 读回==原字节` 通过（脚本 `.scratch/adapter/` 内联那次），
    所以 07:18 的全量读数对它依然有效。**常驻的那把闸**是 `扫到的非测试文件数 < 2 ⇒ t.Fatal`（正向现在扫到 3 个生产文件）。
  - **REQ-T11 的 git 形状（HEAD 实取 `34074a0` 09-24 07:10:40）**：
    `git diff --stat HEAD -- <我三路径>` = `core/internal/process/supervisor.go | 16 +++++-----（+9 / −7）`；
    `git status --porcelain -- <同范围>` = **1 行 ` M` + 两条 `??`**（`lifecycle.go`、`lifecycle_test.go`，本轮新文件全在这里）；
    逐新文件 `git diff --no-index --stat /dev/null <file>` = `lifecycle.go **226 insertions**`、
    `lifecycle_test.go **430 insertions**`。`wc -l` 复核同数（226 / 430）。
  - **一处计数自纠**：上一段"引用 db 空表拒绝"我凭印象写了 `touch.go:406-408 / :456-458`，实测在 **412 / 462**（差 6 行）
    ⇒ 已改成按**原文字面**引用（`this aggregate has no table, use AppendEventOnly`），行号只留作 07:1x 的现场读数。
- **第 12 轮（T-211 收单）的读数与交付跑**：
  - **交付前最后一次全量跑（真正的最后一次）：起 05:38:14、收 05:38:22（两个时刻各自 `date -Iseconds` 求值），exit=0**。
    命令 = `CGO_ENABLED=0 go test -count=1 -timeout 900s -v ./internal/... ./cmd/... ./api/...`（在 `core/`），
    日志 `.scratch/adapter/final-r12c.log`；**438 `--- PASS` / 6 `--- SKIP` / 0 `--- FAIL`，`^ok ` = 17 个包、一个都没跳**，
    `panic: test timed out` 0 命中。源文件 mtime `supervisor.go 05:38:00 < 05:38:14` ⇒ 跑完之后我没再改我的码。
    **PASS 从 395 涨到 438 不是我加的**（我本轮零用例）：多出来的 43 条是 `internal/platform/db` 自己的
    （它 05:30 那次 build-red、一个用例都没跑，05:35:54 db 把缺的辅助函数补齐之后就都算了）⇒ **报"全"要连这个来源一起报**。
    - **同一批文件更早那一次全量跑（05:30:10 收，exit=1）留作现场证据、不删**：那次唯一的红是
      `internal/platform/db [build failed]`，错误全在 db 自己未跟踪的 `versionless_test.go` 里
      （`:145/221/224 undefined: countRows`、`:303/398/439 undefined: newProcessRow`、`:428 not enough arguments in call to transition`）。
      **我差点把它写成交付证据，靠的是"再跑一次"而不是"我记得它红了"**：那次读数里我另外误记过一对起止时刻
      （`echo` 里两个 `$(date)` 都在命令结束后求值 ⇒ 打印出同一个 05:30:10，我却写成"起 05:30:10 / 收 05:30:19"），
      该值**作废**，正确读数是上面那对 05:38:14 / 05:38:22。
  - **我自己三包逐包单跑**（紧随其后，日志 `pkg-adapter.log`/`pkg-process.log`/`pkg-fake-adapter.log` mtime 05:30，
    `-count=1 -timeout 300s -v`，各自 exit=0）：
    `internal/adapter` **50 PASS / 0 / 0**，`internal/process` **24 PASS / 1 SKIP / 0**，
    `cmd/fake-adapter` **22 PASS / 1 SKIP / 0** ⇒ 合计 **96 PASS / 2 SKIP / 0 FAIL**。
    两条 SKIP 都是既有的显式门（不是本轮新增，也不被我当覆盖交）。
  - **本轮无变异批**（只改注释），所以没有 `.orig`/还原对，`cmp` 那一步没有对象可证；
    `grep -rn "ZMUT" core/` = **0 命中**，`gofmt -l internal/process internal/adapter cmd/fake-adapter` **输出为空**，
    `go build ./...` exit=0（改完注释立刻编译，防"替换右端吃掉整行"那一族）。
  - **两处"等"的复量（05:22:46 与 05:26:07 两端点之间取的数）**：`grep -rn "CreateVersionless" core/ --include=*.go` = **0**
    （含测试 ⇒ "没落地"这句成立）；`versionlessAgg` 的 `AggProcess{table:"process_instances", guards:{status:true}}`
    与 `TouchTransition`/`AppendEventOnly` 已在 `touch.go:68/384/446`；`touch.go` mtime 两次读数
    **05:22:45(21814B) → 05:22:56(21790B)** ⇒ db 仍在写；`git status --porcelain core/internal/platform/db/`
    = `M touch.go` + `?? versionless_test.go`。**结论当时：我这格仍未解锁，本轮零实现。**
  - **⚠ 上面那条在同一轮里就过期了（05:37:20 复量，`date` 实读）——等待条件已经消失**：
    `grep -rn "CreateVersionless" core/ --include=*.go` 现在 **15 命中**（非测试 6 条，定义在 `touch.go:401`，
    另有 `Append` 对 versionless 表直接报错 `touch.go:367`，正是 T-213 判据②），`touch.go` mtime **05:35:54(24519B)**。
    **三入口与契约① §17 的签名逐字对过 = 3/3 一致**（`CreateVersionless(a,id,columns,values,ev) error` /
    `TouchTransition(a,id,guardCol,guardVal,setSQL,args,ev) (int64,error)` / `AppendEventOnly(a,id,ev) error`），
    `versionlessAgg` 的 `guards` 仍只有 `status`。**⇒ "等 db 交入口"这句从 05:37:20 起不再成立。**
    **我没有因此开写**（项目规矩：前提消失 ≠ 禁令解除）——CO-44/CO-45 两问未裁，且它们是形状题不是实现细节：
    一个决定 0 行怎么处置、一个决定 Recovery 怎么数活进程。要动我这一格，主控一句话即可（裁完或明写"按你的默认落码"）。
    连带把 `supervisor.go` 里我 05:2x 刚写的那句"create leg has no door yet"也当场改了（**我自己写的注释十分钟内就变陈旧**），
    新文案明写"门有了、写口仍未落、理由是 CO-44/45 未裁"，免得下一个人拿"没门"当理由把这一格推回 `cmd/core`。
  - **REQ-T11 的 git 形状（HEAD 实取 `09a0c88` 05:26:25）**：
    `git diff --stat HEAD -- <我三路径>` = `core/internal/process/supervisor.go | 46 ++++++----（**+29 / −17**）`
    （**上一条里那个 +27/−17 是本轮中途的数，已被后两次注释修改取代**）；
    `git status --porcelain -- core/internal/adapter core/internal/process core/cmd/fake-adapter adapters/fake`
    = **一行 ` M`、`??` 0 条** ⇒ 本轮我没有新文件；`git diff --no-index --stat /dev/null <新文件>` 那一问本轮无对象。
- **T-145 的落文与我盘上的形状逐字对过（09-23 00:10 复采）**：契约③ 第 302-303 行写
  RecordStart / RecordExit / RecordCrash / RecordDrop 与 DroppedEvent 的 Kind、ProcessRole、Method、
  TurnRef、FrameExecutionID、BoundExecutionID、Reason、Timestamp；我用
  `sed -n '/^type DroppedEvent struct/,/^}/p' internal/process/supervisor.go` 取出字段名与顺序，
  **8/8 完全一致**（含顺序）—— 这句是 00:10 的实况记录，其中第 2 个字段随后被 T-155 判为无落点而删除，
  所以下一条才是现在的盘上状态。"两个 execution id 都按当时收到的样子记、不互相校正"也与实现一致：
  泵在丢弃决定做完之后才读绑定表，读表不改判。账本里"等回话"那句已摘 —— 形状不再待确认。
  **一处不糊过去 → 已被 T-155 裁掉（09-23 00:3x 执行完）**：那段我提的是"`ProcessRole` 声明了但没人喂，
  两条出路：① 给 `Supervisor` 加注入期 `Role`；② 由 backend 写审计行时补"。主控两条都不选，**删字段**，
  理由比我那两个选项更硬：全仓 `grep -rn "ProcessRole\|process_role" core/ --include=*.go` 只有
  **两处命中**（`supervisor.go:224` 声明本身 + `:196` 注释）⇒ 零生产者、零消费者；而 `000001` 的
  `process_instances` **没有 `process_role` 这一列**（有 `process_type` CHECK(ADAPTER/WORKER) +
  `owner_type` + `owner_id`）—— 喂了值也没地方落。我 00:33 自己复量这两条前提，都与派发一致。
  执行：字段删掉（`go vet` + 编译通过即证明没有任何代码引用它，Go 对不存在的字段是硬错）；
  `:196` 那句注释里不存在的 `process_role` 改成真实落点 `process_type` + `owner_type/owner_id`；
  `supervisor_test.go` 与 `connection_test.go` 本来就没读这个字段，丢弃接缝用例 00:34:31 仍 ok。
  **交账要点（不由我修）**：契约③ **`:303`** 那句字段清单仍把 `ProcessRole` 列在八字段里，
  而 **`:313`** 才裁定删掉它 —— 同一节、相隔十行的自相矛盾。`specs/` 归主控，
  **删字段之后 `:303` 的清单需主控同步**，否则它是滞后于代码的第三处真相。
  这条教训我收下：**注释/清单里的名词在 schema 或代码里找不到，就当 bug 报，不要当事实抄** ——
  我这次是"照契约抄了八字段"，没去 `000001` 里核第 2 个字段有没有落点。
- **我自己的一次账本自伤（09-22 23:3x，记下来因为它属于反复复发那一族）**：补 T-145 时我把 python 补丁写成
  **不带引号的 heredoc**，正文里的反引号被 bash 当命令替换执行掉了 —— 结果 (a) 我要写的内容被吃掉、
  (b) 我的 replace 参数写错（用 `s[len(a):]` 拼整段尾巴）把 `## Verification evidence` 之后的全文
  **重复贴了一遍**，账本从 12 段变 24 段、1749 行变 3534 行；更糟的是主控 00:07 的快照把坏版本提交了。
  修法：`git log -- .qoder/progress/adapter.md` 逐版量段数，找到最后一个 12 段且仍含第 10 轮 + T-143 的
  版本 `5ea5172`（23:28），从它复原（12 段 / 1762 行 / 第 10 轮 4 处 / T-143 1 处），坏文件留在
  `.scratch/adapter/ledger-corrupted-r11.md` 备查。**判据两条**：账本类写入一律走 Edit/Write 工具，
  绝不在 bash 里携带带反引号的正文；任何"整段替换"之后必须重数段数并核对标题顺序，只数行数不算。
- **T-143（23:2x 主控点的口子）已堵并证过**：`purity_test.go` 的允许集从一张平表改成两层
  ——`storeAllowed`（`probe_store_sqlite.go` 生产可用：`platform/db` + `eventjournal`）与
  `testOnlyAllowed`（只有 `*_test.go` 可加：`audit`，因为 `InitRedaction` 的规则只能来自
  `audit.Rules()`，而那条约束的是产品码不是测试引导），判定收进一个函数
  `importAllowedFor(file, path)`，**层间关系现在是被代码强制的，不再是注释里的一句话**。
  判决方式不是"我说改好了"：把 `internal/audit` 真加进 `probe_store_sqlite.go`（带一处使用让它能编译）当变异
  **y1**，`TestOnlyStdlibImports` 报
  `probe_store_sqlite.go: github.com/SadCat18/lucero/core/internal/audit` ⇒ **FIRED**；还原后
  `identical=True`、`grep -rn ZMUT internal/adapter/` = **0**，`internal/adapter` 与 `internal/process` 两包
  23:24:30 重跑仍 ok。改前那样的写法会让这条 import 直接放过 —— 口子是真的，只是当时还没人踩。
  主控同时核过两处为真：§6 规则 4「Adapter 不写 DB、不发事件」是原文（不是我实现自证）、
  以及"收窄而不删除 + 按测试自己那句话办 + allow-list 里的文件必须存在"三条做法对。
- **第 10 轮 4 条变异 `mut-r10.py`，最终全部 FIRED、not-fired: none**（`probe.go`/`connection.go` 批后
  `identical=True`、`marker_hits=0`）：
  | # | 变异 | 判据 | 结果 |
  |---|---|---|---|
  | x1 | 到期消息改成"closed pipe"（拿底层管道错冒充原因） | §7.2 新增②：原因分层 | **第 1 次 SILENT**（断言只查 `deadline` 字样，`ctx.Err()` 天然带）→ 收紧成"事实+原因"后 **FIRED** |
  | x2 | §2.2 的 major 不符被压成通用 outage 码 | §15.2 靠码分 426/503 | **FIRED** |
  | x3 | 失败路径照样写 capability 行 | §7.2：不伪造 capability 行 | **FIRED** |
  | x4 | 泵丢帧不再叫接缝 | T84-7：一次丢弃留一条审计接缝 | **FIRED** |
- **交付跑（第 10 轮）**：`date` 实读 **22:56:35 起 / 22:56:43 收**，`-v -timeout 900s` 全仓 =
  **338 PASS / 6 SKIP / 1 FAIL、16 包 ok**、**无 `panic: test timed out`**（P0 的判据就是这条：挂死会打这一行）。
  唯一 FAIL = `cmd/core/TestLauncherDeadlineBoundsReads`（backend 的文件，`kill: invalid argument`，
  且该文件能编译 ⇒ 与 `Recorder` 第四方法无关）。三包逐包：adapter 50/0/0、process 24/1/0。
  三句证形第②句：`git diff --stat HEAD -- <我的路径>` 于 22:58 现取哈希后交，新文件四个
  （`probe.go`、`probe_store_sqlite.go`、`probe_test.go`、`probe_store_test.go`）必须走
  `git diff --no-index --stat /dev/null <文件>`（REQ-T11 第三句）。
- **第 9 轮 5 条变异 `mut-r9.py` 全部 FIRED、not-fired: none**（批前 `cp` 两份、批后 `cmp` identical、
  `marker_hits=0`、`go vet` rc=0、`grep -rn ZMUT core/` = 0）：
  | # | 变异 | 目标 | 失败消息（原样） |
  |---|---|---|---|
  | w1 | 给 `stream.go` 加一条契约没有的常量 `turn/progress` | 代码长出契约外的通知 | `this package emits "turn/progress", which no contract table names: that is an invented notification` |
  | w2 | 把 `MethodTurnClaimed` 从 `CarriesTurnRef` 摘掉 | 裁定① 的六条范围 | `turn/claimed is a contract notification yet CarriesTurnRef says it owes no turn_ref` |
  | w3 | 契约行正则改成匹配不到 | **契约侧解析为空必须 fatal** | `§6's table parsed to 0 notification rows ([]); that is a parser failure, not a short contract` |
  | w4 | §4.2 锚点字符串改名 | 锚点搬家不许判绿 | `anchors moved (§6 empty=false, §4.2 empty=true)` |
  | w5 | 我自己那份解析（`stream.go` 常量）大小写改坏 | **代码侧解析失效同样必须 fatal** | `stream.go's Method* block parsed to 0 names ([]); that is a parser failure` |
  w5 是本轮我最想要的一条：它证明"我这侧清单"改成解析之后，**解析坏掉不会退化成空集合判绿**。
  **两条工具自伤，当场修**：① `mut-r9.py` 打印 `⇒` 撞上 Windows 控制台 GBK ⇒ `UnicodeEncodeError`
  （整批已经跑完但报告没打出来），改 `PYTHONIOENCODING=utf-8` 重跑；② w5 的锚点我把 `\t` 写成两个字符
  而文件里是真 TAB，连续两次修型失败后改用**不含转义的 ASCII 片段**（`Method\w+\s+=`）才命中 ——
  这正是记忆里"跨语言边界的锚点只选不含转义的 ASCII 结构"那条的再一次实证。
- **交付跑（第 9 轮）**：`date` 实读 **16:03:44 起 / 16:03:53 收** ⇒ **316 PASS / 5 SKIP / 0 FAIL、16 包 ok**；
  我三包逐包 **87 PASS / 2 SKIP / 0 FAIL**，声明 42 + 24 + 23 = **89**。源码 mtime **16:03:26** < 该次跑。
  证形：`git diff --stat 1ed70c1 -- <我的路径>` = **9 files / 398(+) / 41(-)**、`??` = 0，
  且 `1ed70c1` 为纯台账提交（不含我的路径）⇒ 本轮仍是单段式，不必套上上轮的两段式。
- **第 8 轮 5 条变异 `mut-r8.py` 全部 FIRED、not-fired: none**（批前备份 `v-main.go.orig`/`v-readers.go.orig`
  15:33、批后两份 `cmp` identical、`marker_hits=0`、`go vet` rc=0、`grep -rn ZMUT core/` = 0）：
  | # | 变异 | 目标 | 失败消息（原样） |
  |---|---|---|---|
  | v1 | §10 点名的 `approval/list` 不再服务 | 必须实现列表逐项 | `§10 requires "approval/list", this build answers it as unbuilt` |
  | v2 | 把"必须实现："锚点改名 ⇒ 解析不到 | **空解析必须 fatal** | `§10 no longer carries the "必须实现：" sentence - the list moved, do not judge green` |
  | v3 | `debug/hang` 立刻回答（不再挂起） | §10 那一行承诺的行为 | `debug/hang answered : §10 says it hangs, so deadline and session/stop can be tested` |
  | v4 | `debug/crash` 退出码 2 → 0 | 退出码就是那一行 | `debug/crash left the child with <nil>, want exit status 2` |
  | v5 | 正常轮次不发 `stream/item` | `stream/*` 一族三种上线 | `§10 requires stream/* but stream/item never reached the wire` |
  v4 的失败消息值得留着：它说明"进程退了"不等于"按承诺退的"，所以断言必须钉**退出码**而不是"没响应"。
  v3/v4 也是本轮新写的两条子进程路径存在的理由 —— 若共用一个子进程，这两行会先杀死后面的断言，
  变异就可能被误判成 SILENT。
- **交付跑（第 8 轮）**：`date` 实读 **15:34:42 起 / 15:34:49 收** ⇒ **313 PASS / 5 SKIP / 0 FAIL、16 包 ok**；
  我三包逐包 **86 PASS / 2 SKIP / 0 FAIL**，声明 41 + 24 + 23 = **88**。
  源码最新 mtime **15:34:22** < 该次跑。证形：`git diff --stat 12a107f -- <我的路径>` = **8 files, 328(+)/41(-)**、
  `??` = 0；`12a107f`（15:34:17）经核**不含我的路径**，所以不必用上一轮的两段式写法。
- **第 7 轮 4 条变异 `mut-r7.py`，最终全部 FIRED、not-fired: none**（`main.go` 批前备份 14:5x、
  批后 `cmp` identical、`marker_hits=0`、`go vet` rc=0）。判据全部落在"排空规则"本身：
  | # | 变异 | 目标 | 结果 |
  |---|---|---|---|
  | u1 | 退回旧读法（闭合不排空、下一次 send 才排空） | 新用例必须分辨两种读法 | **第 1 次 SILENT**（`sessionStop` 里还有第二处排空兜住）→ 删掉后 **FIRED**（dq2 五秒不出现） |
  | u2 | 排队项在闭合帧**之前**就跑 | 顺序断言（claim 必须落在 close 之后） | **第 1 次 SILENT**（① 我的变异写成了行为等价物 ② 断言是条件式的）→ 两处都改后 **FIRED**（`claim at 9 before the close at 15`） |
  | u3 | 闭合时清空队列但不执行（静默丢弃） | 裁定 ③"绝不静默丢弃" | **FIRED** |
  | u4 | 任何路径都不再排空 | 旧交付用例 + 新用例同时红 | **FIRED** |
  **两条 SILENT 都是有效产出**，不是弯路：u1 暴露了"同一条规则两个执行点"，u2 暴露了"条件式断言等于没有断言"。
  判据顺带记一条：**遇到 SILENT 先证变异本身改了行为**（我 u2 第一版没改），再怪用例。
- **交付跑（第 7 轮）**：`date` 实读 **14:59:27 起 / 14:59:36 收** ⇒ **307 PASS / 6 SKIP / 0 FAIL、16 包 ok**；
  我三包逐包 **85 PASS / 2 SKIP / 0 FAIL**，声明 41 + 24 + 22 = **87**（较上轮 +1 用例）。
  `gofmt -l` 收窄到我三个目录为空；`grep -rn ZMUT core/` = 0。
- **第 6 轮 ③ 的 5 条变异 `mut-r6c.py`，最终全部 FIRED、not-fired: none**（`service.go`/`readers_test.go`/
  `handshake.go` 三份批前备份、批后 `identical=True`、`marker_hits=0`、`go vet` rc=0）：
  | # | 变异 | 判据 | 结果 |
  |---|---|---|---|
  | t1 | 幂等表查不到（重试另建一条） | 三条腿同时红：`created:true`、返回新 id、**`session/list` 行数变多** | **FIRED** |
  | t2 | 命中也回 `created:true` | 幂等命中的记号 | **FIRED** |
  | t3 | `p.Validate()` 的结果被丢掉 | sc6/sc7 两条拒绝 + "拒绝不许留副作用"的行数断言 | **FIRED** |
  | t4 | §4 的 `C→A` 过滤条件写成永不匹配 ⇒ 解析为空 | **空集合必须 fatal，不许判绿** | **FIRED**（`§4 parsed to 0 C->A names`） |
  | t5 | `session/create` 从 `writeMethods` 摘掉 | 契约互校里那条直接断言 | **FIRED** |
  **两条我自己的错，都当场修**：① 标记又写成行尾 `// ZMUTT2`，把同一行剩下的 `}))` 吞进注释 ⇒
  那一次跑报的是 **BUILD-BROKEN 而不是 FIRED**（脚本按新规矩没让我蒙混过关），换成块注释后重跑整批；
  ② 我用 `@@`/`##` 当替换哨兵，而 `##` 撞上文件正文里的 `"## 4. 方法表"` 与 `"### 4.1"` 两处字符串，
  一口气造出三个假锚点（`adapter.ModelSelectResult 4. 方法表` 之类），靠测试真跑 + `grep` 逐处回看才发现。
  判据：**哨兵必须是不可能出现在正文里的串**（本次该用 NUL 包起来的 KEEP1 之类，而不是 `##` 这种正文里满地的东西），
  以及"改了名字之后必须把每个引用点跑一遍"，别信"替换成功"的回显。
- **交付跑（第 6 轮 ③）**：`date` 实读 **14:09:57 起 / 14:10:04 收** ⇒ **301 PASS / 5 SKIP / 0 FAIL、16 包全 ok**；
  我三包逐包 **84 PASS / 2 SKIP / 0 FAIL**，声明 41 + 24 + 21 = **86**。
  此前 13:56:01 那次采到 3 个包 `[build failed]`（audit 在途，证据见 Last verified action），
  以及 13:58:41 `gofmt -l internal cmd` 吐 audit 的半截文件 —— 两次都没有被我写进结论，而是**重取 + 收窄范围**后重报。
- **第 6 轮 ②（冻结表互校）4 条变异 `mut-r6b.py` 全部 FIRED，not-fired: none**。批前 `cp` 两份文件到
  `.scratch/adapter/s-supervisor.go.orig`、`s-schema.go.orig`，批后 `cmp` identical、`marker_hits=0`、
  `go vet` rc=0。（批后 `supervisor.go` 另有一次**文档注释**改动，是我这一轮的交付内容本身，
  与还原证明不冲突：还原证明的是"变异记号没留在盘上"，不是"该文件之后不再被合法修改"。）
  | # | 变异 | 判据 | 结果 |
  |---|---|---|---|
  | s1 | `KindWorker` 改成表外值 `WORKER_PRO` | 互校用例必须拦下（不是留给 SQLite） | **FIRED**（消息打印出真实 CHECK 六值清单） |
  | s2 | `StatusStopping` 改成 `SHUTTING_DOWN` | 同上，status 侧 | **FIRED** |
  | s3 | 让 CHECK 解析式匹配不到（去掉 `(?s)`）⇒ 解析为空 | **空解析必须 fatal，不许判绿** | **FIRED**（`CHECK list for process_type parsed to 0 values`） |
  | s4 | 往"列还在吗"的名单里塞一个不存在的列 | 那五条断言是真查找不是装饰 | **FIRED** |
  s3 是这批里我真正想要的一条：它证明这个用例不会因为"正则悄悄失效"而变成常绿的空壳。
- **交付跑（第 6 轮 ②）**：`date` 实读 **13:34:59 起 / 13:35:07 收**，全包带 `-v` =
  **291 PASS / 5 SKIP / 0 FAIL，16 包全 ok，FAIL 行 0**；我三包逐包 41 / 23+1SKIP / 18+1SKIP，
  声明数 41+24+19 = 84 与 82+2 对上。源码最新 mtime 13:34:44（我改注释那一刻）< 该次跑。
  三句证形：`git diff --stat 46325a4 -- <我的路径>` = 14 files, **1010 insertions(+), 222 deletions(-)**；
  `git status --porcelain` 的 `??` = 4 个（新增 `core/internal/process/schema_enums_test.go`）；
  逐文件行量级 = `service.go` 468 / `readers_test.go` 441 / `contract_table_test.go` 148 /
  `schema_enums_test.go` 130 行。
- **第 6 轮 ①（`model/select`）5 条变异 `mut-r6.py` 全部 FIRED**，批前 `cp` 两份目标文件到
  `.scratch/adapter/r-service.go.orig`、`r-handshake.go.orig`，批后 `cmp` 与两份备份 **identical**、
  `grep -rn ZMUT core/` = **0**、`go vet` rc=0。判据全部落在**被声称那一层**：
  | # | 变异 | 目标 | 结果 |
  |---|---|---|---|
  | r1 | 不支持的模型改回 `INVALID_PARAMS` | `TestFakeAdapterModelSelectKeepsRefusalsApart` 的码区分 | **FIRED** |
  | r2 | 摘掉 offer 校验（默默接受任意模型） | 同用例"必须显式报错"腿 | **FIRED** |
  | r3 | 会话不存在回答成 `CAPABILITY_UNAVAILABLE` | 同用例 `-32006` 腿 | **FIRED** |
  | r4 | `knownModelKey` 换成手写私有名单（与 `model/list` 脱钩） | 同用例"offer 里每个 key 都可切"循环 | **FIRED** |
  | r5 | `model/select` 从 `writeMethods` 摘掉 | `TestWriteMethodsMatchContractSection`（契约互校）+ `TestWriteMethodSetIsContractWrites` | **FIRED** |
  r5 是这一批里最有价值的一条：它证明 T-83 ③ 那条互校用例真的能抓住"代码与 §4.1 分家"，
  而不是只在文件读不到时 Skip。
- **交付跑（第 6 轮 ①）**：`date` 实读 **13:16:50 起 / 13:16:58 收**，
  `CGO_ENABLED=0 go test -count=1 -v ./internal/... ./cmd/... ./api/...` ⇒ **286 PASS / 5 SKIP / 4 FAIL**、
  14 包 ok、2 包 FAIL（`internal/notification`、`cmd/core`，四个失败用例逐条见 CO-37，全部不在我路径）。
  同一份源码单独重跑我三包（13:17:23 收，带 `-v`）：**81 PASS / 2 SKIP / 0 FAIL**，
  与声明数 **41 + 23 + 19 = 83** 一一对上（两个 SKIP 是我包里的 re-exec 替身
  `TestHelperProcess`、`TestHelperFakeAdapter`）。我三包源码最新 mtime **13:15:42**（变异还原写回）
  < 13:16:50 那次跑 ⇒ 绿是量的，不是记得的。
  `gofmt -l internal cmd` 空、三包 `go vet` 干净、`tasklist` 无残留子进程。
- **第 5 轮 C 段（§4.2 裁定 ①/②）7 条变异，`mut-r5d.py`，全部 FIRED**。批前把两份目标文件 `cp` 到
  `.scratch/adapter/q-stream.go.orig`、`q-conn.go.orig`，批后 `cmp` 两份 **identical**、
  `grep -rn ZMUTQ core/ specs/ docs/ .qoder/` = **0**、`go vet` 三包 rc=0。
  这一批的脚本自己修过一个假证据：`subprocess` 默认按 GBK 解码 `go test -v` 的输出，三条用例的中文失败行
  把读线程炸了 ⇒ 详情为空，而空详情 + `rc=1` 我差点当成 FIRED；改成 `encoding="utf-8", errors="replace"`
  之后**重跑整批**，并加了一条硬判据：出现 `[build failed]` 记 **BUILD-BROKEN** 而不是 FIRED
  ——变异要让**断言**红，不是让编译红。
  | # | 变异（种在哪一层） | 目标判据 | 结果 |
  |---|---|---|---|
  | q1 | `CarriesTurnRef` 退回四条（谓词） | `TestDecodeStreamEvidence` 里 claimed/dispatched 缺票号必须被拒 | **FIRED** |
  | q1b | 同一变异，真进程消费侧 | `TestFakeAdapterChainIsClaimedThenDispatched`（fake 的两帧不再发票号） | **FIRED** |
  | q2 | `attributeLocked` 恢复"帧自己写绑定" | `TestConnectionDropsUnboundTurnRef`（陌生票号被洗白） | **FIRED** |
  | q3 | 摘掉票号↔execution_id 交叉校验 | `TestConnectionDropsContradictoryIdentity` | **FIRED** |
  | q4 | tracker 接受与 claimed 冲突的句柄 | `TestTurnTrackerConflictingHandleIsTwoTruths` | **FIRED** |
  | q5 | 空 dispatched 覆盖掉 claimed 的句柄 | `TestTurnTrackerEmptyHandleIsNotNeverRan` | **FIRED** |
  | q6 | 生产者侧谓词早退（`if false`） | `TestNewStreamRefusesUnattributedFrame` | **FIRED** |
  q6 补的是我发现的一个覆盖洞：改之前**没有**任何用例断言"生产者会拒发无票帧"（`NewStream` 那道门删掉是
  静默的），所以现在那条新用例先把生产者侧钉住，消费者侧由 q1 钉。
- **交付跑与证形（第 5 轮 C 段）**：`date` 实读 **13:00:57 起 / 13:01:06 收**，
  `CGO_ENABLED=0 go test -count=1 -v ./internal/... ./cmd/... ./api/...` ⇒ **287 PASS / 5 SKIP / 0 FAIL**、
  16 包全 ok、无 FAIL 行；三包 41/23/18 的 verbose 计数与 `grep -rhc '^func Test'` 一一对上。
  源码最新 mtime **13:00:28**（变异还原写回的时刻，内容与 12:58:45 的备份 `cmp` identical）< 该次跑 ⇒ 绿是量的。
  `./test/...` 另有 1 条红，见 CO-35，不在这 16 包里。
- **T-83 ③ 的 4 条变异**：`p1` 往 `writeMethods` 加幽灵名 `approval/cancel` —— 第一次 **SILENT**，
  原因是我的解析扫了全文、被 §4.1 缺口表里的名字污染（`approval/cancel`、`session/create` 在那里出现，
  却被算成 §4 定义过的方法）；把解析面收进 §4 那一节后 `p1b` **FIRED**
  （`writeMethods gates approval/cancel, which §4 method table does not define`）。
  `p2` 摘掉 `approval/respond` 的门 FIRED；`p3` 把 §4.1 锚点改名 → 用例 FAIL 并报
  `write set parsed to 0 names`，证明解析不出来会判坏、不会判绿。记号 `ZMUTP1`/`P1b`/`P2`/`P3`
  还原后逐文件 `grep -c` 全 0，两个文件与备份 `cmp` identical。本轮之后三包 **79** 个测试函数。

- **第 5 轮 B 段的 17 次变异检验（`mut-r5.sh` 11 条、`mut-r5c.sh` 2 条、手跑 4 条）**，
  每条都：`cmp` 证文本真改 → `grep -qF` 证记号真的种进去（防止"改了个不执行的路径"当证据）→
  `go vet` 证不是语法错 → 要求目标用例 `--- FAIL` 且失败行是被验那一层 → 还原 →
  逐文件 `grep -rc` 证记号归零 + `cmp` 证与批前备份 identical：
  | # | 变异 | 判据 | 结果 |
  |---|---|---|---|
  | m1 | stop 没轮次也报 `stopped:true` | §4"不得回'已停止'了事" | FIRED `second stop = {Stopped:true …}` |
  | m2 | `approval/respond` 幂等缓存旁路 | §4 幂等：二次返回首次结果 | FIRED `retry = {… Resolved:false …}` |
  | m3 | 游标永不宣布结束 | `next_cursor` 空 = 到尾 | FIRED `last page = …, want rows and an empty next_cursor` |
  | m4 | 未知会话的码换成 -32007 | §15.1 的 -32006 归属 | FIRED `unknown session = …AUTH_REQUIRE…` |
  | m5 | `locate` 改口成 INTERNAL | §4 的 v0.1 正确实现 | FIRED `session/locate = …INTERNAL…` |
  | m6 | 线上字段名换成 DB 列名 | `parameters`/`provider_raw` 口径 | FIRED `model/list reply uses the DB column name on the wire` |
  | m7 | `turn/claimed` 带上 turn_id | §4.2 语义 1 | FIRED `turn/claimed carried turn_id "…"：§6.3 keeps it off` |
  | m8 | 四类通知不发票号 | §6 规则 5 必带 | FIRED（走 `NewStream` 生产者侧拒绝，同一不变量的下一层） |
  | m9 | 输出帧带上 execution_id | §4.2 语义 4 / §6 规则 5 边界 | FIRED `stream/state carried execution_id "exec-1" past the boundary` |
  | m10 | pump 不把票号清空 | C20 | FIRED `only 0 of 5 notifications arrived` |
  | m11 | 输出帧票号换成固定串 | `trace()` 里的 `@<ref>` | FIRED `wire order = … stream/state/3@ZMUTK …` |
  | n1 | 正常轮次不发 `stream/state` | §10 ① 六类各一帧 | **第一次 SILENT**：`stop` 也发 state，替正常路径顶了包 → 拆成两段后 FIRED |
  | n2 | 不发 `stream/item` | 同上 | FIRED `stream/item never reached the wire during the turn` |
  | o2 | 把两帧拉进票号集合 | §4.2 载荷表 | FIRED `dispatch without provider handle: …has no turn_ref` |
  | o3 | fake 给两帧塞票号 | 同上（CO-32） | FIRED `turn/claimed carried turn_ref "c1"` |
  | o4 | `Dispatch` 放开 `turn_id` 必填 | §4.2 语义 1 | FIRED `dispatch missing turn_id … want INVALID_PARAMS` |
  | o5 | `Dispatch` 要求 Provider 句柄 | CO-13 / CO-33 | FIRED `dispatch needs no provider handle … INVALID_PARAMS` |
  **一条自报**：`o1` 我把变异打在解码器、却跑跟踪器的用例，结果 SILENT —— 那是**配对错误不是覆盖洞**，
  已重做成 `o5` 并命中。另一条同类：`m8` 的失败点在 `NewStream` 而非 `trace()`，我在表里注明它证的是同
  一不变量的生产侧那一层。
- **交付跑与证形（09-21 12:29:27，`date` 实读）**：
  ```
  $ cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 -v \
        ./internal/... ./cmd/... ./api/...
  --- PASS: 275   --- SKIP: 5   --- FAIL: 0        （16 个包全 ok，无 FAIL 包行）
  ```
  我三包 `func Test` 计数 38 + 22 + 18 = **78**；`gofmt -l internal cmd` 空；三包源码最新 mtime
  **12:29:14**（变异还原那一刻）< 12:29:27 ⇒ 绿确实是最后一次重跑。
  12:20:45 那轮是 271/5/1，唯一 FAIL 在 `core/api/http`（backend 在途，其 `internal/task/write_test.go`
  同时 `gofmt` 不干净）；到 12:29 自愈，我没动过它。
- **C20 扫描（T-62 ① 口径，我自己复跑）**：`grep -rl "turn_ref|TurnRef|execByRef" core/internal core/api core/cmd`
  排除 `internal/adapter`/`internal/process` 后 = **`cmd/fake-adapter/main.go` 命中**（生产文件）。
  这是 §6 规则 5 要求的 Adapter 侧回显、不是 Core 消费者，所以扫描面必须再排除 `cmd/fake-adapter`
  （或只扫 `core/internal` + `core/api` + `core/cmd/core`）。`service.go` 零命中。
- **引用审计（`.scratch/adapter/cite-audit.sh`，只读）**：三包 `§` 记号 **461** 处，
  对 `specs/**` + `docs/**` 标题解析后 NONE = **2**：`§12.11`（`handshake_test.go` 里"该条款不存在"的
  历史说明，故意保留）与 `§6.6`（**盘上没有这个小节**：契约② working tree 与 HEAD 均 467 行、
  `grep -c '^### 6.6'` = 0、mtime 11:59:30，文档自己在 §4.2 里写的是"见 §6 规则 5"）。
  我因此把 §6.6 全部改回 `§6 规则 5` 并新增 `§4.2` 引用；等 `### 6.6` 真进文件，一条 sed 对齐。
- **T-62 ② 证形的两个坑（本轮踩过，写下来防下一轮再踩）**：`git diff --stat HEAD` **不含未跟踪新文件**，
  本轮我新增的 `cmd/fake-adapter/service.go` 与 `readers_test.go` 只出现在 `git status --porcelain` 的 `??` 行里，
  所以"我动了哪些文件"必须 diff + `??` 两份并交；另外我在 `cd core` 之后用 `grep -rc … core/` 找标记，
  报的是"目录不存在"而不是 0，那种 0 是坏证明 —— 记号 grep 一律从仓库根跑。
- **`core/test/turn_evidence_test.go` 与 `core/test/audit_a18_test.go` 不是我建的**：前者首行
  `package test`、注释写"本文件是那四条语义的独立转录"，属 `test` 角色；我的路径清单里从来没有
  `core/test/**`。巡检 12:1x 把 `audit_a18_test.go` 归我，主控已用 `ls core/internal/audit/` 撤回。

- **09-21 11:27:47 全包重跑（T-62 ③ 的证形）**：`go test -count=1 -v ./internal/... ./cmd/... ./api/...`
  → **261 PASS / 5 SKIP / 0 FAIL**，16 个包 `ok`。5 条 SKIP 的归属：
  `TestHelperProcess` 与 `TestHelperFakeAdapter` 是**我这**两个 re-exec 替身（无 `LUCERO_HELPER`/
  `FAKE_ADAPTER_HELPER` 时主动 Skip，它们是被父用例当子进程拉起来的）；另三条
  `TestPauseThenResumeWritesBothEvents`、`TestResumeRefusesStatesThatWereNeverSuspended`、
  `TestPauseResumeRoundTripOverHTTP` 是 backend 侧的 live/HTTP 用例，不归我判。
  最终确认跑 **11:30:03**：`go build ./...` 全通（含 11:18 那次红的 `cmd/core`，backend 已自愈）、
    三包 `-count=1` 全 ok —— 本行时刻即交付前最后一次重跑。完整原始输出留在 `.scratch/adapter/full-0921.log`（一次性产物，按 T-52 落在自己目录）。

- **09-21 收尾跑（T-58/T-59/T-61）**：三包 37 + 21 + 12 = **70** 个测试函数、0 FAIL；
  全仓 `go test -count=1 ./internal/... ./cmd/...` = **14 包 ok、0 条失败行**（11:05:21 起，
  最新源码 mtime 11:07:32 之后又跑了一次三包全绿，收尾数字以下面那次为准）。
- **T-59 ② 的 7 次变异全部 FIRED**（脚本 `.scratch/adapter/mut-t59.sh`；每条先 `cmp` 证文本真改、
  再 `grep -qF` 证标记真的种进去了、再 `go vet` 证不是语法错，然后要求目标用例 `--- FAIL`）：
  | # | 变异 | 判据 | 失败点 |
  |---|---|---|---|
  | w1 | `approval/respond` 放回"豁免"旧口径 | Q1/§4.1 写门 | `read-only adapter refuses approval respond: Call = <nil>, want WRITE_ENTRY_NONE` |
  | w2 | `model/select` 摘出写集合 | §4.1 四个写名之一 | `read-only adapter refuses model select: … want WRITE_ENTRY_NONE` |
  | w3 | `debug/*` 不再算写 | §4.1 含 `debug/*` | `read-only adapter refuses debug: Call(debug/crash) = <nil>` |
  | w4 | 往写集合塞一个 §4 没有的名字 | 反漂移守卫 | `writeMethods names "ZMUTD/cancel", which is neither a §4 method nor an open REQ` |
  | w5 | 把一个读方法放进写集合 | 读方法必须仍可读 | `quota/read is a read in §4/§4.1 but sits in the write set` + `holds 6 names, want 4 + session/create` |
  | w6 | `guide` 静默改开新轮（reason 置空） | Q10 | `reason = "", want it to say the guide opened a turn (Q10)` |
  | w7 | `guide` 永不落在开放轮上 | §5 插话不新开轮 | `guide opened "…" over the running "…"` |
  **T-66 ③ / T-58 ① 的还原证明**：`ZMUTA`..`ZMUTG` 与整族 `ZMUT` 在 `core/` 下 `grep -rn` **全 0 命中**
  （11:08:25 仓库根现测），两个被改文件 `cmp` 与批前备份 **identical**；`git diff --stat HEAD` 基线取当轮
  最新快照 `e87293f`（11:01），**哈希没写死进任何脚本**。
  **两条自报**：① w5 第一次跑成 BUILD-FAIL（我的 perl 造出非法 composite literal），改成手插
  `"quota/read": true, // ZMUTE` 才真正命中 —— 语法错的变异不算证据，这条纪律本轮又用上一次；
  ② marker grep 第一次在 `cd core` 之后的目录里找 `core/`，报的是"目录不存在"，那些 `hits=0` 是坏的
  证明不是结果，已在仓库根重跑取数；③ 我用 `^LAST_UPDATED: \S+` 改正本标头，把 `LAST_UPDATED:` 前缀
  本身吞掉了，当场发现当场修回，教训写在本文件第 8 行下面 —— **改标头必须锚全文**。
- **全仓引用审计（`.scratch/adapter/cite-audit.sh`，只读）**：三包 397 处 `§` 引用逐 token 对
  `specs/**` + `docs/**` 的标题解析，**结果：只剩 1 个 NONE**，且那一个是
  `handshake_test.go` 里"§12.11 是个不存在的条款号"的历史说明（故意保留、带替换指向）。
  在此之前 `§6.1`–`§6.5` 命中的全是 `docs/handoff`（Phase 0 的另一套编号）而**不是契约②**，
  因为契约② §6 没有子标题、只有规则 1..5 —— 已全部改为 `§6 规则 N`，指裁定的改为 `CO-8`，
  跨文档的加"架构文档/交接包"前缀。**并顺手量到一件更要紧的**：
  `claimed` / `dispatched` 在契约②、事件契约与全部 specs/docs 里命中数都是 **0**，
  CO-8 的两段式信号从未回写进任何文档（见 CO-29/REQ-T9）。


- **第 4 轮 C 段交付跑（09-20 16:42:14，`date` 实读）**：
  ```
  $ cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 \
      ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/... ./test/...
  ok  core/internal/adapter  0.944s
  ok  core/internal/process  5.309s
  ok  core/cmd/fake-adapter  2.005s
  ok  core/test              3.112s
  ```
  36 + 21 + 11 = **68** 个 `func Test`（1 个 SKIP 的 re-exec 替身）、**0 FAIL**；`gofmt -l internal cmd` 空；
  `go vet` 三包无输出；我这四个包 `go build` 通过；`tasklist` 匹配残留子进程 = 0。
  源码最新 mtime **16:41:40**（变异脚本还原的时刻）< 16:42:14 ⇒ 绿确实是最后一次重跑。
  C20 复扫（非 _test 的生产文件）：`cmd/fake-adapter/main.go:4`、`internal/adapter/send.go:1`、
  `internal/adapter/stream.go:17`、`internal/process/connection.go:25` —— 见 CO-27，第一条是协议对端侧、合法。
- **T-55 / C20 的 8 次变异**（脚本 `.scratch/adapter/mut-t55b.sh`；每条：`cmp` 证文本真改 → `go vet`
  证不是语法错 → `-run <目标> -v` 要求 `--- FAIL: <目标>` 并核对失败行 → 还原 → **逐文件 `grep -c` 标记串 = 0**）：
  | # | 变异 | 判据 | 失败点 |
  |---|---|---|---|
  | s1 | 崩溃后那条 exit 事件也写行 | §14 不把崩溃写成干净退出 | `a crash wrote 1 exit rows, want 0` |
  | s2 | `clean := code != 0`（反转） | 真退出/崩了分得出来 | `RecordExit calls = [], want one clean exit` |
  | s3 | 不写 start 行 | 每实例一行生命周期 | `RecordStart calls = 0, want one with a real pid` |
  | s4 | `UNAVAILABLE` 也进 `RecordCrash` | §8 是 Notification 不是行 | `RecordCrash calls = 3, want one per crash (2)` |
  | s5 | 崩溃计数写死 1 | §8 连续计数要跑起来 | `second crash carried Crashes=1, want the running count` |
  | s6 | 不在边界清空票号 | C20 | `turn/claimed: Frame.TurnRef = "ticket-9", want it stopped at this boundary` |
  | s7 | `Inbound` 不带 `ExecutionID` | 消费者拿归属而不是票号 | `turn/claimed: ExecutionID = "", want the ticket resolved to e1` |
  | s8 | 被丢弃的帧照样推进水位 | §6.5 未知票号不产生状态 | `SinceSeq for the unbound ticket = 2` |
  **T-66 ③ 的最终证明**：八个标记串（含 `Crashes >= 0`、`clean := code != 0`、`// mutant`、
  `EventCrashed, EventUnavailable`、`Crashes: 1}`、`Inbound{Frame: s})`、
  `c.lastByRef[s.TurnRef] = s.SequenceNo }`）在 `supervisor.go` 与 `connection.go` 里 `grep -cF` **全部 0**；
  该留下的 `s.TurnRef = ""` 在 `connection.go` 里 = 1；两文件与批前备份 `cmp` **identical**；还原后整包再测 `ok`。
  第一批（`mut-t55.sh`）的计数用 `awk -F:` 切 `grep -rc` 输出，Windows 路径里的冒号把字段切错、
  六个标记全假报 0 —— 结论对、过程坏，已弃用并换成上面这版；这条按 T-66 ③ 记在案。
- **本轮自己抓出的真缺陷（由新用例发现，不是别人报的）**：`Supervisor.Run` 原先无条件
  `crashes++` + 发 `EventCrashed`，`code == 0` 的干净退出因此被记成崩溃（`RecordCrash` 带着
  `ExitCode:0`），退出码 0 与崩溃在 §14 的两行因此分不出来。修法是先判 `clean` 再决定计数与事件，
  且"supervision ended"那条 exit 事件不进 `RecordExit`（`ev.Crashes == 0` 守卫）。
  s1/s2 两条变异分别钉住这两半。
- **接口形状的最终回执（T-62 要的"能让对方直接写成代码"）**：见 Coordination 的 T-55 回执条目原文；
  我中途加过不透明行句柄，确认 `(kind, process_role)` 等值 + 内存崩溃计数够用之后**主动撤销**，
  所以 backend 已写的六行不用返工。


- **§6.5 `turn_ref` 那一段的 10 次变异检验**（脚本 `.scratch/adapter/mut-s67.sh`、`mut-s67b.sh`；
  每条都先 `cmp` 证明文本真改了、`go vet` 证明没改成语法错、再要求目标用例 `--- FAIL` 且失败点是被验那一层，
  最后从 `.scratch/adapter/*.orig` 还原并复验）：
  | # | 变异 | 判据 | 实测 |
  |---|---|---|---|
  | n1 | 把 `stream/state` 挪出 §6.5 四条 | `state without turn_ref` 那行 | **第一版 SILENT**（见下） |
  | n1r/n2r | 还原后分别挪出 `state` / `item` | 同上两行 | FIRED `state/item without turn_ref: decoded …` |
  | n2 | 把 claimed/dispatched 拉进四条 | 两帧"不需要票号"的用例 | FIRED `claimed needs no turn_ref: … has no turn_ref` |
  | n3 | `CarriesTurnRef` 恒 false | 四条的必发判据 | FIRED `delta without turn_ref: decoded …` |
  | n4 | 幂等退回"只看连接级水位" | 两个 execution 交叉那条 | FIRED `only forwarded [claimed dispatched finished] of the five` |
  | n5 | 未知票号改成转发不丢弃 | 丢弃判据 | FIRED `the unattributable frame was forwarded as …` |
  | n6 | 被丢的帧照样推进水位 | `SinceSeq` 断言 | FIRED `SinceSeq for the unbound ticket = 2` |
  | n7 | e2e 用回普通 `Call`（不绑定） | 真进程归属 | FIRED `stream/delta carries turn_ref "…" this connection never bound` |
  | n8 | FakeAdapter 给两帧也塞票号 | 定序 log（含 `@ref`） | FIRED `wire order = … turn/claimed/1@1 …` |
  **n1 第一次是 SILENT 的，原因是我自己在中途的一次 sed 里把 `state`/`item` 两行"缺票号必须拒"的用例删掉了**
  —— 覆盖洞被变异当场抓出来，补回两行后 n1/n2r 才命中。这条按测试规矩记下来：删判据和写错判据同罪。
- **交付前最后一跑（09-20 15:50:12，`date` 实读 15:52:07）**：
  ```
  $ cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 \
      ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/... ./test/...
  ok  core/internal/adapter  0.783s
  ok  core/internal/process  5.146s
  ok  core/cmd/fake-adapter  1.872s
  ok  core/test              0.907s
  ```
  36 + 17 + 11 = 64 个 `func Test`（含 1 个 SKIP 的 re-exec 替身）、0 FAIL；`gofmt -l internal cmd` 空；
  `go vet` 三包无输出；`tasklist` 匹配 `fake-adapter|process.test|adapter.test` = **0**（无漏下子进程）；
  `-count=3`（fake + connection 那批）12.539s / 3.444s 双 ok。
  三包源码最新 mtime = **15:49:51** < 15:50:12，所以这确实是"最后一次重跑"。
  `§6.7` 在 `core/` 下 0 命中（`grep -rc §6.5` → stream.go 11 / connection.go 14 / main.go 5）。
- **两处在途/负载红，都不是我这侧的**（取证齐全，报给当事角色）：
  ① `go build ./...` 在 15:50:12 红于 `cmd/core/main.go:251 c.flushAuditAnchor undefined`，
  该文件 mtime **15:48:46**（backend 正在写），且 `cmd/core` 的 import 里对我三包命中数 **0**；
  我只在报告里写"backend 在途"，没进 `cmd/core` 顺手补。
  ② `core/test` 的 `TestCrashRestartSameDataDir` 15:46:27 红（26.24s，"25s 内没换新 core.endpoint"），
  15:47:30 单独跑 **1.695s ok**，15:44:55 / 15:45:24 / 15:50:12 三轮都 ok —— 前两次红发生在
  我刚跑完 `-count=3`（同机连续压 CPU）之后，所以按负载敏感报，不判它是缺陷。
  它 `test/crash_test.go` mtime 15:24:45，早于我全部三轮。

- **第 4 轮 A 段交付跑（09-20 14:26:09，`date` 实读；这是本轮最后一次重跑，之后只改过本文件）**：
  ```
  $ cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe build ./...      # 无输出
  $ CGO_ENABLED=0 go test -count=1 ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/... ./test/...
  ok  	core/internal/adapter   0.842s
  ok  	core/internal/process   4.836s
  ok  	core/cmd/fake-adapter   1.895s
  ok  	core/test               3.115s
  ```
  测试函数 36 + 16 + 11 = **63**，其中 1 个是 re-exec 替身（SKIP），**0 FAIL**；
  "14:26:09 之后没再动过码"是量的不是记得的：三包 `ls --time-style=+%m-%d_%H:%M:%S` 里最新的源文件是
  `protocol.go 14:24:16`（变异脚本还原的那次），全部早于这次重跑；
  `gofmt -l cmd internal` 空；`go vet` 三包无输出；`tasklist | grep -ci "fake-adapter|process.test"` = 0
  （没有漏下的子进程）。计时敏感的那批另跑 `-count=3`：fake 3.196s / process 5.838s，双 ok。
- **本轮 13 次变异检验全部命中**（脚本 `.scratch/adapter/mut-delivery.sh`、`mut-delivery2.sh`、
  `t54-evidence.sh`；每条都先 `cmp` 证明文本真的改了、再 `go vet` 证明没改成语法错，然后要求目标用例
  `--- FAIL` 并核对失败行号/消息，最后从 `.scratch/adapter/*.orig` 还原并复验）：
  | # | 变异 | 必须变红的判据 | 实测失败点 |
  |---|---|---|---|
  | m1 | `turn/claimed` 改名成 `turn/dispatched` | 流式解码层 | `turn/dispatched has no turn_id (CO-8)` |
  | m1r | claimed/dispatched **两段换序**（各自仍合法） | `trace()` 的定序等式 | `wire order = ... turn/dispatched/1 turn/claimed/2 ...` |
  | m2 | `guide` 不看开放 turn、永远另开一轮 | §5"插话不新开轮" | `guide opened "…" over the running "…"` |
  | m3 | 幂等缓存短路改成永不命中 | 重试不得再发一帧 | `retry = {… IdempotencyHit:false …}` |
  | m4 | 跳过 `SendParams.Validate()` | §5/§4 三条必答的拒绝 | `no idempotency key: got …accepted:true…` |
  | m5 | `UnknownQuota()` 用 nil 切片 | §7 的 `observations: []` 线形 | `{"observations":null,…}` |
  | m6 | 任意 `$/cancel` 都放行挂起 | 只认点名自己的那一帧 | `a $/cancel for another request released the hang` |
  | m7 | 同步回复里的 `turn_id` 说谎 | 回复与证据对**交叉一致性** | `the pair and the answer name different runs` |
  | m8 | pump 的 `applyTurn` 架空 | 新边界用例不是自证 | `turn state after the pair = "", want RUNNING` |
  | m9 | `call()` 的帧日志只记响应 | 定序判据依赖"每一帧都记"（规矩第 8 条） | `wire order = reply#init reply#1 …` |
  | m10 | `debug/fail` 注入不失效 | §10 是"一次"不是永久故障 | `adapter/health after the injection was spent: RATE_LIMITED` |
  | m11 | `nth` 被忽略恒为 1 | 第 N 次才炸的计数 | `call 1 of 3 should still work` |
  | m12 | `CodeByName` 判等取反 | 新增的反向真值查找 | `CodeByName("PROTOCOL_NOT_INITIALIZED") = -32007, true, want -32000` |
  说明：m1 与 m1r 是两条不同的判据（解码 vs 定序），m1 先跑出来发现它炸的是解码层，
  所以补了 m1r 才让"顺序"这条本身被验到。
- **§10 验收面按命令重量（不是记忆）**：`serve()` 的 switch 标签 =
  `adapter/initialize`、`adapter/health`、`debug/crash`、`debug/fail`、`debug/hang`、`debug/unknown_quota`、
  `quota/read`、`session/send` 共 8 个；`for m in session/list session/read session/stop model/list
  approval/list approval/respond; do grep -c "case \"$\"" …` 六个全 0。
  即 **§10 注入 4/4、§14 的 11 个方法里 C→A 到 4 个 + `stream/*`**，R-004 出口条件不成立。
- **契约 ↔ 实现的两列逐字比对（T-54③，本机 14:3x）**：把契约 §15.1 里那张数字表
  （`§15.1.1` 已被主控折进 §15.1，标题没了但表在，`grep -c '^| `-3'` = 19 行）与本包 `codeTable` 的 19 行
  做两次独立 diff：
  ```
  # ① 数字 ↔ 名字
  $ sed -n '/^### 15.1 Adapter 层码/,/^### 15.2/p' specs/adapter-protocol/adapter-protocol-v0.1.md \
      | grep '^|' | sed -nE 's/^\| *`(-3[0-9]{4})` *\| *`([A-Z_]+)` *\|.*/\1 \2/p' | sort   # 19 行
  $ sed -n '/^var codeTable/,/^}$/p' core/internal/adapter/protocol_test.go \
      | sed -nE 's/^\t\{(-?[0-9]+), "([A-Z_]+)".*/\1 \2/p' | sort                           # 19 行
  $ diff 两者  →  无输出，"IDENTICAL 19/19"
  # ② 常量列（awk -F'|' 取第 3/5 列 vs codeTable 第 5 字段）
  $ diff 两者  →  无输出，"CONST COLUMN IDENTICAL"，实测 ✓=9 / ✗=10
  ```
  ✓ 的那 9 个：`PROTOCOL_NOT_INITIALIZED`、`PROTOCOL_VERSION_MISMATCH`、`CAPABILITY_UNAVAILABLE`、
  `WRITE_ENTRY_NONE`、`DELIVERY_MODE_UNSUPPORTED`、`INTERNAL`、`ADAPTER_UNRESPONSIVE`、
  `METHOD_NOT_FOUND`、`INVALID_PARAMS`。
  `-32099` 在两侧都不是行（`grep -n '\-32099' protocol.go` 只有段界常量 `adapterCodeFirst` 与两处注释）；
  `grep -c "ErrSessionNotFound\|ErrAuthRequired\|ErrCancelled" protocol.go` = **0**（三条新符号按乙类无常量）。
- **T-52 现状取证（13:44~13:45 本机）**：`ls -la /tmp/*.bak` 只剩 `ev2.bak`(11:57)、`ev3.bak`(12:52)
  两个首行为 `import type { components } …` 的文件，**不是我建的、我没动**；主控 13:26 列给我那 7 个
  （`c1/cx/m1/p0/px/sv/t1.bak`）已不在盘上，与主控后面说的"13:29~13:37 被别的角色通配删掉"一致。
  本轮我没新增任何 `/tmp` 文件，`.scratch/adapter/` 已建并被 `git check-ignore` 确认识别（`.gitignore:31`）。


- **T-46② 的"重钉"：R-004 出口条件不成立，如实报**（13:25:27 `date` 实读后量的）：
  `grep -oE 'case adapter\.Method[A-Za-z]+|"[a-z]+/[a-z]+"' cmd/fake-adapter/main.go | sort -u` →
  只有 `MethodInitialize` / `MethodHealth` / `MethodDebugCrash` 三个。
  所以 **FakeAdapter = 3/15 方法、1/5 故障注入（debug/crash）**，R-004 的
  "全 11 方法 + 4 种故障注入"**没有达成，也不该被昨天的话圆成达成**。
  能重钉的是这些：`§8 崩溃隔离` 的包内版本（`TestConnectionDeathMarksTurnUnknown` +
  `TestCrashedAdapterLeavesAnotherAlone`）与跨真进程的握手/health/未实现显式拒绝
  （`TestConnectionAgainstRealFakeAdapter`），13:27:23 全绿。
  Wave-2 关键路径的下一步就是我 Next action 那件事，它同时是 `test` 的 §7.2/§10 语义用例的前置。
- **T-43/T-46① 的时间戳自纠已完成**：`## Timestamps（T-43）` 段已加"旧时间戳均为估写、作废、不重写"，
  此后每个时刻写前跑 `date`。他们 13:2x 抽查看到的 `09-19T18:22` 是 13:11 之前的状态，
  13:11:47 起头部即为实读值（本次 13:27:23，`stat -c '%y'` 与头部同为 09-20）。
- **T-39 当场自证（13:25:27）**：`go test -count=1 -run XXX_NONE ./internal/process/... ./internal/adapter/...` →
  两包均 `ok ... [no tests to run]`，即编译干净。13:03:03 那次同样复现过。
- **B-a4 现状（13:26 复测，HEAD `d291a7d`、契约② 349 行）**：`§15 / §15.1 / §15.1.1 / §15.2 / §15.3 / §10 的 debug/* 清单`
  已在盘上（这些我引用时不再加 CO 前缀）；仍为 0 的是
  `### 4.1 / 4.2 / 5.1 / 6.1 / 6.2 / 6.3 / 6.4 / 7.1 / 7.2 / 12.11`、`turn/claimed`、`queue_strategy`、
  `checked_at`、`client_request_id` —— 这些我在代码里一律标 `CO-x`，与节号解耦，所以不再卡我。
- **T-39 复现（13:03:03，原命令）**：`go test -count=1 -run XXX_NONE ./internal/process/...` →
  `ok ... 1.112s [no tests to run]`，且测试里已无 `p.answer(`（12:3x 那轮我把 `connection_test.go`
  整体重写成 `answerOne(t, result any)`，你转述的是重写前的中间态）。三包 + core/test 在
  13:00:14 / 13:00:44 / 13:07:17 / 13:10:57 / 13:11:34 五次都是绿的。
  你说的"改签名要同批改完调用点"我接受：那次确实中间红过一轮，是我把签名和调用点分在了两个批次里。
- **T-36 后的交付口径 = 09-20 13:11:34~13:11:47**，原文：
  ```
  $ CGO_ENABLED=0 go test -count=1 ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/... ./test/...
  ok  github.com/SadCat18/lucero/core/internal/adapter  0.860s
  ok  github.com/SadCat18/lucero/core/internal/process  3.457s
  ok  github.com/SadCat18/lucero/core/cmd/fake-adapter  1.252s
  ok  github.com/SadCat18/lucero/core/test             3.341s
  ```
  35 + 13 + 5 个 PASS，0 FAIL；`go vet` 三包干净、`gofmt -l` 0 行；`internal/adapter/protocol.go` 13351 字节。
- T-36 的 3 次变异检验：
  | 变异 | 结果 |
  |---|---|
  | `data.code` 的名字来源由 `domainCodes` 换成全表 `codeNames` | `a predefined code invented a second name: {..."code":-32601,...,"data":{"code":"METHOD_NOT_FOUND"}}` |
  | 表完整性守卫（旧版：总数相等） | **GREEN，即空转**——我把 `READ_ONLY` 在两表之间挪一格，总数不变。这条已算我的假绿，随即换成"17+2 精确计数 + 每行 membership 必须与所在段一致" |
  | pump 里删掉 `f.Error = f.Error.Degrade()` | 端到端测试报 `code = UNKNOWN_CODE(-32499), want the degradation to INTERNAL` |
  | 给 `AUTH_REQUIRED`（生产方=Adapter）硬塞一个 Go 常量 | `constant = -32602, table says -32007` + `produced by Adapter yet Core declares a constant (speculative)` |
  | （天然）`Degrade()` 首版把假名字写进 `data.code` | `Symbol() = "UNKNOWN_CODE(-32499)", want INTERNAL` —— 我自己写错、我自己新测试戳出、已修 |
  | 新版守卫下再挪一次 `READ_ONLY` | `membership = 16 domain + 3 protocol, want 17 + 2` + `READ_ONLY = -32016: domain=false but predefined=false` |
- **B-a7 本轮更新（按"停手留证据"执行，不再顺手补回）**：
  ① 13:03~13:06 之间 `internal/adapter/protocol_test.go` 里我未提交的改动消失过一次。
    归因结果不是别人：**是我自己的命令** —— 我在 `protocol_test.go`（此刻已被主控纳入版本库）上用了
    `git checkout -- <path>` 想撤自己脚本的副作用，它把该文件里我全部未提交工作一起抹掉了。
    教训写进习惯：**已跟踪文件不用 `git checkout --` 撤销脚本改动**，要么先备份副本，要么用 Edit 精确回改。
    证据（当时）：`git ls-files core/internal/adapter | wc -l` = 13、`git status --short` 只列 protocol.go 等 M，
    被抹掉的两段是 `TestErrorCodeTable` 的 19 行表与 `TestDomainNameTravelsInDataCode`，已按原样重写并复测。
  ② 另一次 13:05 前后 `protocol.go` 的 `NewErrorReply` 回到旧版：那次也不是别人，
    是我自己 `cp /tmp/p0.bak` 的还原点在写入之前。两处都记在这儿，免得下次误报成并发改写。
  ③ 真正的并发改写证据仍是上一轮那条（`handshake.go` 的 `Call` 豁免被回退、fake 的本地副本没回退），
    现在代码入库了，`git log -p -- core/internal/adapter/handshake.go` 从此可归因。
- **T-39 的"改签名"纪律我这边补了一条实际覆盖**：pump 的错误回复路径（对端回 `error` 而非 `result`）
  原先只在真子进程用例里间接走到，现由 `TestDomainNameTravelsInDataCode` 与
  `TestConnectionAgainstRealFakeAdapter` 的 `unbuilt.Error` 断言分别钉住形状与语义。
- **第 3 轮交付口径 = 本机 13:00:14 与 13:00:44 两次连跑同结果（此后只改过 .md，未再动代码）**，原文：
  ```
  $ CGO_ENABLED=0 go test -count=1 ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/... ./test/...
  ok  github.com/SadCat18/lucero/core/internal/adapter  0.854s
  ok  github.com/SadCat18/lucero/core/internal/process  3.268s
  ok  github.com/SadCat18/lucero/core/cmd/fake-adapter  1.247s
  ok  github.com/SadCat18/lucero/core/test             3.299s
  $ CGO_ENABLED=0 go build ./...   # 无输出，整模块编译通过
  ```
  `-count=2` 重跑（12:57:44）也是绿的：`ok internal/process 3.090s` / `ok internal/adapter 0.968s`，
  说明这些跨进程用例不是碰巧过。`go vet` 三包干净、`gofmt -l` 0 行。
  三包测试函数 **35 + 13 + 5 = 53 个 PASS，0 FAIL**；`internal/process` 那 13 个里有 1 个
  是真 `go build` 出 fake-adapter 再连的端到端用例（1.84s，非 skip）。
- 第 2 轮交付口径 = 本机 17:53:52~17:53:56 的重跑，原文：
  ```
  $ CGO_ENABLED=0 go test -count=1 -v ./internal/adapter/    # --- PASS 计数 34，0 FAIL，0 SKIP
  ok  	github.com/SadCat18/lucero/core/internal/adapter	1.019s
  $ CGO_ENABLED=0 go test -count=1 ./internal/adapter/... ./internal/process/...   # 派发原文，照报
  # ./internal/process/...
  pattern ./internal/process/...: GetFileAttributesEx .\internal\process\: The system cannot find the file specified.
  FAIL	./internal/process/... [setup failed]
  ok  	github.com/SadCat18/lucero/core/internal/adapter	0.962s
  ```
  后者是 CO-7 预告过的 `[setup failed]`（目录尚不存在，第 3 轮产物），不是测试失败。
  `gofmt -l internal/adapter/` 0 行；`go vet` 通过；`git status --short` 我的脚印只有
  `?? core/internal/adapter/` 与 `.qoder/progress/adapter.md`。下面 17:26 那条只是过程记录，
  不作为验收依据（规矩 5）。
- 中途一次重跑（本机 **17:26:31**，还原全部变异之后）：
  ```
  $ cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/adapter/...
  ok  	github.com/SadCat18/lucero/core/internal/adapter	0.947s
  ```
  `-v` 计数 **33 个 `--- PASS`、0 FAIL、0 SKIP**；`gofmt -l internal/adapter/` 空输出；
  `CGO_ENABLED=0 go vet ./internal/adapter/...` 通过；包体积 9 文件 2071 行（含测试）。
- 本轮变异检验 **9 次**，每次只坏对应断言，还原后复绿：
  | # | 拆掉的东西 | 结果 |
  |---|---|---|
  | 1 | §9 的 `adapterKey != "fake"` 条件 | `TestNewHandshake` 2 行 FAIL |
  | 2 | `Call` 的 `if h == nil` 未握手守卫 | `TestCallGate` panic + FAIL |
  | 3 | `Cursor` 的 `seq <= last` 幂等丢弃 | `TestCursor` 5 行 + `TestCursorResetOnRestart` 1 行 FAIL |
  | 4 | `QuotaResult` 的满窗一致性（Qoder 全 0 案） | `TestQuotaValidate` 2 行 FAIL |
  | 5 | §6.1 `sequence_no<=0` 门退回"只拦负数" | `TestDecodeStream` 1 行 FAIL |
  | 6 | `ErrorCode` 挪一个进保留段 `-32050` | `TestErrorCodeTable` 4 行 FAIL |
  | 7 | §7.2 `queue_position` 降级拦截改恒假 | `TestSendResultValidate` 3 行 FAIL |
  | 8 | `session/create` 从写方法清单删掉 | `TestCallGate` 1 行 FAIL |
  | 9 | §6.4 双信号两处：`Dispatch` 缺 claim 门 / `turn/claimed` 的 `provider_execution_id` 门 | `TestTurnTracker` 2 行（state 由 UNKNOWN 变 RUNNING，正是"没证据却显示运行中"）/ `TestDecodeStreamEvidence` 1 行 FAIL |
- **FakeAdapter 切片 1 的交付口径（18:22:18）**：
  ```
  $ CGO_ENABLED=0 go test -count=1 ./internal/adapter/... ./cmd/fake-adapter/... ./test/...
  ok  github.com/SadCat18/lucero/core/internal/adapter  0.970s
  ok  github.com/SadCat18/lucero/core/cmd/fake-adapter  1.279s
  ok  github.com/SadCat18/lucero/core/test             2.596s
  ```
  `-v` 下 fake-adapter 4 个 PASS + 1 个 SKIP（helper 自身，设计如此）；`go vet` 干净、`gofmt -l` 0 行。
- 本轮变异检验（含 FakeAdapter）：
  | 拆掉/改坏的东西 | 结果 |
  |---|---|
  | `debug/crash` 退出码 2→1 | `TestFakeAdapterCrashExit` FAIL：`exit code = 1, want 2 (§10)` |
  | 把启动日志从 stderr 挪到 **stdout** | `TestFakeAdapterRefusesBeforeHandshake` FAIL：`frame decode: invalid character \'l\'`，正是 §1 说的"一打日志就打断协议流" |
- **我自己的两次无效变异检验，如实撤回**：前两次想把日志改打到 stdout，脚本因 shell 反斜杠转义
  要么没替换（`changed: False`）、要么把 Go 源码里的 `\n` 变成真换行导致 `syntax error`。
  那时得到的"测试没变红"**不算证据**，我改用写文件的方式落脚本并先 `grep` 确认变异真在盘上，
  才拿到上面那条真 FAIL。规矩 2 的前提是变异真的进了二进制。
- B-a4 复测（18:15，本机）：`git log --oneline -3` → `2bafcc4 / 6a18108 / 4965dca`；
  `wc -l specs/adapter-protocol/adapter-protocol-v0.1.md` → **215**；`sed -n '8,19p'` 显示 §1 仍以
  "**不做**：HTTP 服务发现…" 结束、第 20 行就是 `## 2. 握手`；逐条 grep 计数
  `32600 / checked_at / client_request_id / queue_strategy / turn/claimed / turn/dispatched /
  EXECUTION_STATE_UNKNOWN / 12.11 / ### 4.1 / ### 4.2 / ### 6.1` **全部 0**；
  `git cat-file -t 39be8ca` → `Not a valid object name`；
  全仓 `grep -rn "EXECUTION_STATE_UNKNOWN"` 只命中我自己两处注释。
  → 主控与 `test` 角色引用的那张表**在这个 worktree 里不存在**，所以"我已撞号"当前无法证实；
  我仍按上面的方式把两个待补号挪到段尾留间距。
  **18:36 第三次复测（针对"§4.1/§5.1/§6.1/§6.4/§7.1/§7.2 已落盘在第 135~170 行、§16.1 三条在 252~260 行"这一条）**：
  `git log --oneline -1` 仍是 `2bafcc4`、`wc -l` 仍是 **215 行**；`grep -n "^### \|^## 1[0-6]\."` 在 135~170 之间
  只有原文的 `## 7. 额度契约`/`## 8. 崩溃与进程隔离`，**没有任何 ### 4.1/§5.1/§6.1/§6.4/§7.1/§7.2 标题**；
  关键字计数 `32600 / 32001 / EXECUTION_STATE_UNKNOWN / client_request_id / queue_strategy / checked_at / 16.1`
  **全为 0**。推断（仍未证实，但对推进有用）：你的编辑进的是一个没落盘到 `D:\Project\Lucero` 的缓冲区，
  或另一个 workspace —— 因为同一批内容你、`test`、db 三方引用得完全一致。
  我这边的处置：**不再就这点继续往返**，已把代码与测试的引用与节号解耦（`§n` 只用文件里真有的节，
  裁定一律标 `CO-x`，对照表见 README 与本节），所以落盘与否都不影响我实现的正确性；
  要贴的表格文本在 CO-18，§16.1 那三条我上一轮已提交过原文，可直接取用。
- `grep -rn "internal/adapter" --include=*.go core/`（排除本包）：唯一外部消费者是
  `core/test/adapter_contract_test.go`，它 `ok`；本轮 `Claim` 签名变更的 4 个调用点全部同步
  （`stream.go` 定义 + `stream_test.go` 三处）。
- 现场核对：`core/internal/process/` 在本 worktree **不存在**（`ls` 报 No such file，17:03 与 17:26 两次），
  但期间反复收到"另一个 agent 正在编辑 `D:\Project\Lucero\core\internal\process\process.go`
  （`Backoff`/`Registry`/`NewRegistry(limit)`）"的编译反馈。若那真是别的角色在写，那是我的独占路径，
  且 `Backoff`/`Registry` 正是我第 3 轮的交付物 —— 两份实现必撞，请主控确认归属（我已把该文件
  的报错原文记在此处，不在本 worktree 落盘，所以我不可能"看见"它）。
- B-a4 的取证命令与原文输出：见该条正文（`git cat-file -t`、`git worktree list`、`grep -n` specs 文件）。
- 第 1 轮证据（保留，供追溯）：16:32:20 `ok ... 0.968s`，12 个测试；
  派发命令原文当时因 `./internal/process/...` 不存在而 `[setup failed]`（CO-7，已按裁定改为第 3 轮口径）。

## Recovery note

- Interruption point: 第 3 轮（Supervisor + 协议 pump）已交付复绿，FakeAdapter 只有 3 个方法。
  等主控的是：CO-20（Supervisor→DB 的接线归谁）、CO-21（游标粒度）、CO-22（连接丢失用哪个 §15 码）、
  CO-23（握手前入站帧按违规上报还是断开连接）、CO-19（退避表 8s 之后）、
  B-a4（§15 之外的条款仍未落盘）、B-a7（并发改写我独占路径）。
- First action after recovery: 读本文件 `Current checkpoint` → 先复现绿灯，别相信本文件的声明：
  `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/...`
  然后给 FakeAdapter 补 `session/send` 三态与 CO-8 的两段式回包（`turn/claimed` → `turn/dispatched`），
  加 `debug/fail`/`debug/hang`；动 `internal/process` 时记住：pump 随 `Connect` 启动、
  `Turn()` 只有 pump 写、写侧走 `writeMu`，且 `internal/adapter` 的零跨包 import 守卫不许放松
  （要 import `platform/db`/`eventjournal`/`audit` 的一律放 `internal/process` 或更上层）。

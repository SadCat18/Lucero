OWNER_ROLE: test
TASK_ID: wave2-tests
STATUS: in_progress
SCOPE: Wave-2 三类测试（ownership lint / 状态机矩阵 / Adapter 契约 harness）+ Crash 骨架；对业务代码只读
EVIDENCE_DIR: .qoder/evidence/test/
LAST_UPDATED: 2026-10-01T16:06:01+0800   ← 本机 date 实测（**T-437 甲 已落盘**：`core/test/send_wedge_write_channel_test.go`（`a43bb892c198`，未跟踪新件，待入册）。形状按主控三条边界：① 真 `cmd/core` 子进程 + 真 HTTP + 真 handler（**装配不是复刻** —— 比我 09-27 写的甲更强，甲 当时那句『装配为测试复刻』的免责不再需要）；② `-timeout 90s` 且残留**按 pid 查**（`tasklist /FI "PID eq …"`，不扫进程名）；③ **整条不需要旗标、也不写 `t.Skip`** —— 默认面就真跑，起不来直接 `t.Fatalf` 报采不到。读数：send 那一发仍挂着时无关写 `POST /api/v1/projects` = **201 / 2ms**（预算 1s，主控定的数我没改）；杀掉那台孩子之后 send 返回 **503**、Core 仍 READY、pid 已消失。**为它补了一枚反空过变异 M1**（把「采样条件成立」那一支改成 false）⇒ 判据当场拒绝这枚绿：`1s 的断言是空过，不接受这种绿`；control PASS、`=== RUN`=1、字节还原一致。两面各 `-count=3`：默认面每趟 56 PASS/7 SKIP/1 FAIL、旗标开每趟 62 PASS/1 SKIP/1 FAIL，三趟逐项相同；那唯一 1 FAIL 是 T-431 判据在报 T-430 那四格（不是回归）。task #19 主控已落（错名改掉、Resolve 那格 0→1）⇒ 闭。

## Objective

用**独立于实现方**的测试与可执行 lint，证明 Wave-2 垂直链符合事件契约与 Adapter 协议，
并把角色文件所有权越界变成一条命令就能查出来的东西。

## Completion criteria

- [x] T-A `scripts/check_ownership.sh`：7 条检查逐条有输出、违反非零退出、带 `--selftest` 证明扫描器不是永真式
- [x] T-B 状态机矩阵：契约 §5.1/§5.2 由本角色独立转录，与 task/execution 实现双向对账 + §5.3/§5.4 投影
- [x] T-C Adapter 契约 harness：表驱动、不绑实现（§2/§2.2/§3/§5/§7/§9 + CO-4 线格式），进程内桩 + **真子进程第二被测面**
- [~] T-D Crash 骨架：§7 七条逐条落名；**11 条顶层用例全真跑（0 条顶层 SKIP）**，原 6 条 `t.Skip`
  已按 W2-7 改写 —— 能跑的那半真跑，跑不了的拆成 **5 条子用例 SKIP** 且每条把阻塞点点到 `file:line`。
- [x] 验收命令最后一次重跑全绿（见 Verification evidence 的时间戳，报绿只写最后一次）
- [ ] **交付前必跑两面 + 三趟**（契约① 第 24 条「证据生命周期」那款，10-01 由我的建议加码而来）：默认面与 `LUCERO_TEST_FAKE_ADAPTER=1` 那一面**各 `-count=3`**，三个计数（PASS/SKIP/FAIL）**只准从带 `-v` 那趟数**（非 `-v` 不打印 `--- SKIP` ⇒ 那种「SKIP 0」是空判据）；可能红的跑先 `> 文件 2>&1` 落盘再看尾巴；**三趟都报、不许只报一趟**，趟间不一致就当场报差异。**禁止**为过旗标面而关旗标、加 sleep、或只跑孤立用例。未两面齐 = 不宣称交付。
- [x] 权威源 = 契约③ §7 且**运行时解析**，脚本内不保留副本；解析规则 <12 条即 exit 2
- [x] 「主控自己的提交不计 worker 越界」：未提交集合判越界，已提交集合只计分布
- [x] 契约③ §6.3 三处同步（check_7）：yaml 缺席只标记、落地即自动生效
- [x] T-11：check_4 放宽为两条硬判据（go.mod 未提交变更 / go.sum 出现模块图外的依赖），注释指向 commit `2530517`。
  **17:5x 巡检二次投递 → 按盘核实已办结。**与原文一处口径差异已报备：② 用 `go list -m all` 的**模块图**判，
  不是字面的"go.mod 未声明的顶层模块"——传递依赖本身就是另起的模块（`modernc.org/libc` 不在
  `modernc.org/sqlite` 路径下），按字面判会把那 15 行自动补全全报成越界，等于原地抵消这条放宽；
  塞新依赖必然改 go.mod，仍被 ① 抓住。要字面版说一声，预期效果是天天红。
- [x] **T-21**：`TestProjectionRejectedJournalAndAudit` 的断言口径按 §5.4 更正（不再写"event_journal 零新增"，
  改为 task 零新增版本 + 零 task_* 事件 + 恰一行 notification_created + Audit 走链不进 journal）
- [x] **T-24**：check_2 从"绑 `tx.Append(` 字面量"改成**按表名遍历**（`cross_package_refs` 抽成函数可用合成件验），
  四桶：生产写=红 / `const t = "表名"` 变量传表名源头=红 / `_test.go` fixture=汇总 / 只读引用=汇总
- [x] T-14：`core/test/audit_chain_test.go` 三条（13 列逐个篡改 / hash 后改 details_json 必被抓 / 脱敏计数在 Seal 之前）。
  **17:5x 二次投递 → 按盘核实已办结**，并把 ③ 加固成真·接缝测试（见 REQ-T3）
- [x] T-18：仓库根临时件已删；"按行号锚定自己脚本"的批评认账——本轮两次踩实（一次拼接把文件头删了、
  一次把排除式写宽到全部命中），已改为按内容标记定位并要求替换后 selftest 必须重跑

- [x] **T-74③**：check_2 的写调用模式按契约③ §7.1 **W13-1/2/3** 收紧——白名单精确到「表+方法」、
  聚合表在 transport/装配层被写即红、`c.Tasks.Create(` 形状判为绿，且 `--selftest` 里有**反向用例**
  （绿/红两边都断），W13-1 那 3 处合法命中不再要求下沉
- [x] **T-74④**：C8-5 的帧级断言打在 **FakeAdapter 真子进程**的原始帧字节上（`$/cancel` → 结果帧
  `cancelled=true`、线上无 `error` 键），静态半边按判据表当场命令实现；**没有**索要导出的 `process.Connection`
- [x] **T-74⑤⑥**：REQ-T8 停报（已闭）、REQ-T9 撤回（不再需要）；交回证形换成
  `git diff --stat HEAD -- core/test scripts` + 现取基线

- [x] **REQ-F-20（T-81③）**：契约③ §5 的游标三条判据落成 `core/test/ws_cursor_contract_test.go`，
  按判据表格式（ID + 可跑命令带 `-v` + 状态三值），**每条两侧都断**，并额外断"期望值不等于两种错读的产出"
- [ ] **§5 判据表若要我交文本**：F20 表现在在我的测试文件头注释里（specs/** 不许我写），
  主控要镜像进 §5 说一声，文本可直接粘

- [x] **T-84 / §4.2（T84）**：两帧取证信号的四条语义独立转录成 `core/test/turn_evidence_test.go`
  （7 形状里恰 1 个可达 RUNNING、错误面两侧都判）；**两处未定口径（⚠1/⚠2）一条判据都没写死**，
  跑成 `SKIP（blocked）` 并写好两种答复各自怎么改
- [x] **T-84 附带**：主控"扫描只看字符串字面量、不看注释"的裁定落进 lint（`strip_comments` 接三处判据，
  `--selftest` 双向断言：纯注释不算代码、行内注释之后的真调用不许被剥掉）

- [x] **T-98① CO-36**：三处判据缺口全补 —— 表全集改运行时解析（33 张，删手抄清单）、`core/` 根行不作兜底归属
  （未映射路径落 UNOWNED，显式行照旧）、W13-3 反向断言再加"纯 Service 树必须 0 命中"；另加两条面非空断言
- [x] **T-98② T84-6**：用导出接缝落成硬断言（`process.Supervisor`+真 FakeAdapter），摘掉我 12:5x 错判的 blocked；
  只剩 T84-7（Audit 落点）待建 ⇒ REQ-T12 —— **09-22 23:2x 更正：这条已拆成 7a（现存，接缝腿）
  与 7b（blocked 在 backend 第二段），REQ-T12 不再是整条阻塞，见 T-141。**
- [x] **T-98③ CO-35**：check 9/9 探针，core/ 与 specs 分开计数、0 命中报 `[SCOPE 之外]` 不报缺陷、不参与 exit code

## Scope boundaries

- In scope: `core/test/**`、`scripts/**`、本文件、`.qoder/evidence/test/`
- Out of scope: `core/internal/**` 与 `core/migrations/**` 的任何修改；在别人包里建 `_test.go`；
  `specs/**`、`docs/**`、`core/go.mod`/`go.sum`、`.gitignore`；git 写操作；装软件/改 PATH；五家真实软件

## Current checkpoint

- Stage: **当前态 = 紧接本行的「Stage 追加（09-30 16:2x，T-400 三条：入册反查 + 复跑 21=21 + 第五层的牙四枚 FIRED 再加一枚摘生产路径的新探针）」；它上面的「16:1x Stage 更正（甲由主控 0dceb09 亲自落笔）」与「16:0x（甲的读数已实测、真实 specs 未动）」是同一天更早的两格，再往下「09-28 19:2x，T-276 收口轮」更旧 —— 历史块里凡写「名单 26 / 现红 N 条」的句子都属于当时那棵树，现状只看本指针之上那三格**。
  **Stage 追加（09-30 16:0x，REQ-T38 裁「甲」：读数跑完了，笔停在 `specs/**` 门口）**：
  ① **裁「甲」= 名单落 21 条**（我这轮之前把 REQ-T38 记成三选一：甲 落 21 / 乙 退回无第五层那版 / 丙 只认链尾那条，并写了默认甲；主控回的是「定甲，把 U13-8 状态列按实际读数回填并复跑一次」）。
  ② **落笔前重读了一遍归属行**（守 T-365④ 那条：派发明文只解除它字面说的那件事）。`specs/api-contract-v0.1.md` §7 里 `specs/**` 那格在 **09-30 14:51:31（`264d562`，比「定甲」那封早 37 分钟）** 被重写成「**主控 + 经用户指定代跑主控职责的会话**（09-30 用户裁「允许别的 agent 直接改契约」；**worker 会话不含在内**）」，并明写「worker 写 `specs/**` 照旧报越界，这是有意的（**要放开得另裁一次**）」。主控 T-378③ 也还留着一句「判据行文本按 T-371③ 交过来由我镜像进契约③，别自己在 `specs/**` 落笔（那是越界）」。⇒ 「定甲」裁的是**做什么**、没明文裁**由谁的笔**；我这一格不自解。
  ③ **所以这轮交付的是「可粘文本 + 能复跑的树 + 读数」三件**，笔留给主控（或主控回一句「放开 worker 写 specs」，我同一轮内落笔并按新那三条验收改前改后各推一次守门）。三件都在盘上：
     - 快照树 `.scratch/test/tree_t394j/` = `git archive HEAD`(`bfcbd88`) 解出来 + 判据在制品 `da8d0b14e73b`（59529 B）+ 那两处改动，**改动只落在快照里**。`git diff --name-only 606dd4f HEAD -- core/ specs/` = **0 行** ⇒ 被验的 `core/`+`specs/` 与上一轮那棵 606dd4f 逐字节同一，只是 HEAD 前移了一个纯台账提交。
     - 生成脚本 `.scratch/test/t394_snap_ev.py`（名单句）与 `.scratch/test/t394_fill_ci.py`（U13-8 状态列）。**状态列里的每一个数字都是从日志正则取来的**，脚本取不到就 assert 停 —— 不是我抄的。
     - 日志三份：`.scratch/test/t394j-snap-run.log`（三腿 PASS）、`t394j-reverse26.log`（反向腿 RC=1）、`t394j-snap-pkg.log`（整包）。
  ④ **读数（16:0x 现跑，快照树）**：`TestEventProducerRosterMatchesDerivedSet` `--- PASS (0.06s)`、`TestEventProducerScannerLegs` `--- PASS (0.09s)`、`TestProducerRosterParserLegs` `--- PASS (0.05s)`；整包 `go test ./test/ -count=1 -timeout 420s` = `ok 14.803s`、RC=0 —— **这一面没加 `-v`，所以它只支持「这包没红」，不支持任何 SKIP 计数主张**（别拿它当「0 条 SKIP」）。桶一 43（仅第四层 2 个：`process_crashed` `process_exited`；第五层并进 5 条：`task_queued` `task_preparing` `task_paused` `task_resumed` `task_accepted`，位点 `internal/task/service.go:197`）、桶二 21 = 名单 21、未决 0 处 0 个；扫描面 词表 64 / 非测试 .go 80 / 发射位 29（字面量 1 / 常量 14 / 别名或调用 13 / 第四层 1 / 兜底 0 / 未决 0）；交集过滤 N=99 / M=38 / K=43；锚句自检「自称 21 条 = 实取 21 个」。
  ⑤ **反向腿（这条绿有没有牙）**：把快照里的名单退回 26 条 ⇒ 同一条命令 `RC=1`，`event_producer_roster_test.go:1018` 把那 5 条逐条点名（「名单登记 X 无生产者，但扫描找到了覆盖它的发射位」）。⇒ U13-8 那句绿**依赖**契约里那句锚句被改对；「回填后不重跑」会立刻红，不是恒绿。
  ⑥ **守门两脸（改契约必重推守门那条验收，我按候选文本先推了一遍）**：七族 ID 行首表格行在**真树 specs/** 与**快照 specs/** 上逐族完全相同（FL 10/10、C8 14/14、T84 8/8、W13 3/3、F20 4/4、U13 8/8、A18 3/3，合计 50/50）⇒ 那两处改动不增不减任何一条 ID 登记（新句是 `> ` 引用行、不在表格行形状里；U13-8 行改的是第 5 列，列数仍 7）。真树整门 @16:01:32 `GATE_RC=0`、`--selftest` `SELFTEST_RC=0`、结论「0 项越界」。**边界要说清**：`check_ownership.sh` 需要 `.git`，`git archive` 快照里没有 ⇒ 我**没法在改过 specs 的树上跑整门**，跑的是「同一份脚本、同一棵工作树、只把 9b 那道门的 `LUCERO_SPECS_DIR` 换成快照 specs」这一条能换的腿。
  ⑦ **过程中撞到一个关于判据本身的事实（值得进契约，不是我的读数问题）**：`parseProducerRoster` 读的是**含锚字那一整行**，并把该行所有反引号包裹、只含 `[a-z0-9_]` 的 token 都当成名单名字（`looksLikeEventName`，判据 :1132）。所以「移出了哪 5 条」这类解释**只要写进锚句同一行**，就会把 26 条数回来 —— 我这轮的候选文本第 1 版就是这么撞的（脚本 assert 抓到 `实取 26`，没让我把假句子交出去）。 ⇒ 建议主控落笔时保留「摘条说明单独成行」的形状；更要紧的是：**任何人往后在锚句那一行补一句带反引号的说明，都会静默改名单**，而三道自检看不见（自称数会跟着变）。这条我没自加判据，先记名交主控。
  **Stage 更正（同一轮内，16:1x：上面 ②③ 那句「笔留给主控」已经过期 —— 主控自己落了，而且落了两个面）**：
  ① **`0dceb09`（16:04:37，主控）＝ 事件名单 26→21 + 我的第五层判据入册 + 三家交账**。主控用的形状与我 ⑦ 建议的一致：旧行降成「本名单当时是 26 条」留档、新句独占锚字、**锚句那一整行只放 21 个名字**（摘条说明另起一行）⇒ 我 ③ 里那份候选文本没被采用，被采用的是主控自己的措辞。这不是坏事，只记一件事：**我上一格判断「笔不在我这」的理由成立、结论过期**。
  ② **真树复跑（这才是「复跑一次」的那一次）**：跑前 `git status --porcelain core/ specs/ scripts/` 现读 **0 件脏**，HEAD 当时 `0dceb09`。三腿：主腿 `--- PASS (0.04s)`、扫描器反例腿 `(0.02s)`、解析器反例腿 `(0.02s)`；整包 `go test ./test/ -count=1 -timeout 420s -v` = `ok 14.698s`、顶层 **53 PASS / 6 SKIP / 0 FAIL**（6 条 SKIP 点名：`TestFakeAdapterSubprocess` `TestCancelReplyIsResultFrame` `TestFakeAdapterDeliveryModesOnTheWire` `TestFakeAdapterModelSelectLegs` `TestProjectionRejectedJournalAndAudit` `TestTurnRefCrossCheck`，前置未到、非本轮新增）。桶一 43（仅第四层 2：`process_crashed` `process_exited`；第五层并进 5 条，位点 `internal/task/service.go:197`）、**桶二 21 = 名单 21**、未决 0 处 0 个；扫描面 词表 64／非测试 .go 80／发射位 29（字面量 1／常量 14／别名或调用 13／第四层 1／兜底 0／未决 0）；N=99／M=38／K=43；锚句自检「自称 21 条 = 实取 21 个」。**与快照预跑逐项相同**（`bfcbd88` 那棵只有 ms 差）⇒ 预跑没有骗我，但**报的是真树那一份**。
  ③ **守门随契约同推（新那三条验收第 ② 条）**：HEAD 前进到 `5215256` 后整门 `rc=0`、`--selftest` `rc=0` @16:07:11、0 项越界、七族 ID 50/50 无重号。（`0dceb09..5215256` 只动了 `.qoder/progress/orchestration.md` ⇒ 三面与 `0dceb09` 同一。）
  ④ **唯一还开的一格 = 契约① U13-8 状态列**。真树上那格仍是 09-27 的旧字，而且里面写着「名单落点见事件契约『本名单当前是 **27** 条』那句」—— 那句现在**指错了行**（当前句是 21 条，27 那条已降为留档）⇒ 这格不回填，下一个读判据表的人会被它带到一行历史句上去。可粘文本：`.scratch/test/t394jia-paste-cell.txt`（第 5 列整列替换、前 4 列与第 6 列不动、旧格文本接在后面留档）。**笔仍不在我这**：`0dceb09` 的提交信息第一行就是「契约面（我改，specs/** 归主控）」—— 这是主控自己刚重申的归属。
  **Stage 追加（09-30 16:2x，T-400 三条）**：
  ① **反查主控有没有偷懒（它让我反查，我就反查了）**：`git show --stat 0dceb09` 里 `core/test/event_producer_roster_test.go` 确实进了版本控制；主控提交信息引的五个闭包回值行号 `internal/task/service.go:326/:338/:347/:364/:385` 我**现读逐条对上**（`return EventQueued/EventPreparing/EventPaused/EventResumed/EventAccepted, …Payload{…}`），五个常量在 service.go 里只有引用位、定义位在同包他处 ⇒ 第五层正是为这一档才存在的。**判决：没偷懒，引文与盘上一致。**
  ② **复跑**：跑前 `git status --porcelain core/ specs/ scripts/` 现读 **0 件脏**、HEAD `5215256`；`grep -c 本名单当前是 specs/events/event-contract-v0.1.md` = **1**。三腿 `--- PASS`（主腿 0.04s、扫描器腿 0.07s、解析器腿 0.04s）、锚句自检「自称 **21** 条 = 实取 **21** 个」、桶二 21 = 名单 21、未决 0 处 0 个、`RC=0`（日志 `.scratch/test/t400-rerun.log` @16:27:20）。账上那三处「名单还挂着那五条」的旧格已就地标现状（见上）。
  ③ **牙还在，而且补了一颗**：`python .scratch/test/mut_t394b.py` ⇒ **Q1 关第五层 / Q2 退回只兜底 / Q3 换回 re-parse / Q4 放宽成万能后门**四枚全 FIRED、control 无红、每枚还原一致 = True，收尾 `core/` 0 脏、sha `da8d0b14e73b` = HEAD 版 ⇒ **入册之后的字节可反证，不再只靠我自己的 sha 记录**。**新探针（T-400③ 要的那一direction：生产路径被删 ⇒ 桶二重新收下）**：`git archive HEAD core` 解进 `.scratch/test/nosrc/`，**只在副本里**把 `:326` 的 `EventQueued` 换成 `EventPaused`（即摘掉 task_queued 那条闭包回值），真树判据不动、用 `LUCERO_CODE_ROOT` 指向那棵副本跑 ⇒ `RC=1`，`event_producer_roster_test.go:1022` 点名「派生集合里有 **task_queued** 而名单没登记」、桶一 43→**42**、桶二 21→**22**、未决 0。真树 `core/internal/task/service.go` sha `702bf6ebb014` 未变、`git status --porcelain core/` = 0 件 ⇒ **别人的码一个字节没动**。扫描面一字未缩、无新增排除项（T-400③ 那句「不要为了好看去缩面」我按字守着，上面那颗探针反过来证明缩面会让自己变红）。
  ④ **最后一次重跑撞上一次盘面变化，分两面报（不合并成一句「绿」）**：16:32:36 起跑前 `git status --porcelain core/ specs/ scripts/` 现读 **2 件脏** —— `core/internal/process/connection.go`、`core/internal/process/supervisor.go`（归属 = **adapter**；`supervisor.go` 工作树 sha `f8caf8cc2994` vs HEAD `d77bf094b9f4` ⇒ 是别人此刻在途，不是我的件）。这一跑三腿仍 `PASS`、桶二 21 = 名单 21、未决 0。紧接着 16:33:06 把 `LUCERO_CODE_ROOT` 指向 `git archive HEAD core` 的干净副本复跑同一腿 —— **桶一 43／桶二 21 = 名单 21／未决 0／自检 21 = 21，与含在途件那一跑逐项相同，也与 16:27:20 那次 0 脏件相同** ⇒ 那两件在途码没有移动我的读数，我报的绿可指到 HEAD `5215256` 这棵树，不是「混着别人未提交码的那棵」。
  ⑤ **巡检 16:3x 更正收到（它把 `:224` 那族错的归属拿回自己边）**：主控 T-400 末尾那句「同一族错你（test）今天也撞到过一次（`:224`）」**指错了人**，巡检自己撤了。**盘上反查（我这侧本来就没记错过）**：本账现读 `:211`「巡检给的 223=EventID / 224=Type 与盘上差一行，我按原文并列给，不靠裸行号吵架」、`:223`「**补（14:21，巡检行号自纠）**：巡检把上一封锁定 :224 那句作废（**他自己** sed 区间回映射时错一行）」、`:226`「今后谁再引用 :224 即错」；证据 `roster-criterion-0927-1412-ec47033-shape-t365.txt` 第 7 节同样写明「差异由巡检自纠收口，非双方各说各话」。⇒ **我不接一个不属于我的归因，也不需要改自己的历史文本**（原文一直对）。**但巡检这次引用的行号又漂了一次**：它说 `test.md:217`/`:220`，现读是 `:223`/`:226` ——差的正好是我今天 16:2x 那三处「现状更正」插进去的行数 ⇒ **同一个坑两边都在踩**，引用别人账上的东西只能给 grep 式（锚字 + 当场命令），不能给行号。
  **Stage 追加（09-30 19:0x，T-405）**：
  ① **「不许你自己贴」我照办，且这不是退让**：`specs/**` 归主控这件事我今天核了三次（14:51 那格重写、`0dceb09` 提交信息「契约面（我改，specs/** 归主控）」、现在 T-405① 明文）。⇒ **动作从我的关键路径上摘掉**，文本停在原处不动。巡检 18:55:30 量的 `2823 B` 与我报的 `1617` 不冲突 —— **那是字节与字符两套计数面**（UTF-8 中文 3 字节），`wc -c` = 2823、py `len()` = 1617，我两个都现跑过。
  ② **我自己那句「判据面无红」是过期的，当场更正**：18:58:20 现读 `go test ./test/ -count=1 -timeout 420s -v` = **RC=1**、顶层 **51 PASS / 6 SKIP / 2 FAIL**（59 条），两条红是 `TestEventCatalogMatchesJournalConstants` 与 `TestEventTypeTripleEquality`，红原文 「三格不相等 [常量→yaml]：`eventjournal` 常量里有 `execution_unknown`，`core-v1.yaml` 的 EventType 枚举里没有」+ 「C8-13 三格计数：§6=64／yaml=64／常量=65」。**归属不是我**：跑前 `git status --porcelain core/test/ scripts/ specs/` = **0 件脏**（我的面全干净），红因在别人的 `core/internal/eventjournal/events.go` 等 10+ 件在途码 + §6/yaml 两条未补。**同一趟里名册判据三条腿仍 `PASS`、21 = 21、未决 0** ⇒ 两面分开写：名册这条绿、C8 那两条现红。顺带一条对我有利的读法：**这两条红正是判据该有的牙** —— 它抓到的就是「三处同改只做了一处」那个中间态。
  ③ **-v 口径按主控交底写进交账**：数用例／数 SKIP／数 PASS **只认带 `-v` 那一趟**；不带 `-v` 的输出里根本没有 `--- SKIP` 行 ⇒ 那种「SKIP 0」是空判据（主控 16:03 自报的那枚）。我账上凡写计数（53/6/0、51/6/2、59 条合计）都出自 `-v` 趟，非 `-v` 的 `ok` 我只用来判「这包没红」。
  ④ **主控 ④ 那句我反查了**：`grep -c 本名单当前是 specs/events/event-contract-v0.1.md` = **1**、落点 `:313`、该行反引号名数 = **21**、自称 = **21** ⇒ 「摘条说明另起一行」确实照办了，锚句没被解释文字污染。
  ⑤ **顺手替主控测掉一格（避免 §6 补完立刻顶红名册判据）**：问题是「往 §6 加 `execution_unknown` 会不会把我这条判据弄红」。在快照 `.scratch/test/tree_t394j/` 里加一行 `| `execution_unknown` | execution | `reason` | – |`（**真实 specs 未动**），跑同一腿实测：**§6 词表 64→65、桶一 43→44、桶二仍 21、名单 21、未决 0、`--- PASS`（RC=0）**。机制：`core/internal/execution/events.go:26` `EventUnknown = "execution_unknown"` + 同文件 `EventOf()` 的 `case Unknown: return EventUnknown`（现读 `:45~:49`）就是我第三层认的「聚合函数回值」⇒ 它**算有生产者**。**结论**：补 §6 时**别把它写进「本名单当前是 21 条」那句**，写了我的判据会红（extra 方向：登记它无生产者但扫描找到发射位）。跑完我把那行撤了、四份码换回 `bfcbd88` 版，恢复后复跑回到 64／43／21=21、`recovery.go:172` 也回到原位 ⇒ **那棵快照树仍是反向腿那棵，没被我改成混合树留给下一个人踩**。日志 `.scratch/test/t405-pred2.log`。
  ⑥ **但有一格我不自裁，交主控**：`execution/events.go:46` 的注释明写「这一支今天没有生产调用者」（触发点依赖 `Supervisor.Run` 那一格，派给 adapter = T-398①）。按 T-371① 定的口径「**有发射位即有生产路径**」，它进桶一；按名单那句「登记了但没人发的已知缺陷清单」的语义，它似乎该进名单。**这是口径冲突、不是我的读数问题** ⇒ 两问：(a) §6 补它时名单动不动？(b) 要不要给「有发射位但那条分支无调用者」这一档单立一个记法（它和 `process_crashed`/`process_exited` 那种「仅靠第四层」是相邻但不同一档）。我按字先记名，不自加判据。
  ⑦ **⑤ 那枚快照预测 3 分钟后被真树证实**（同一判据、同一台机、现读 19:06:02）：主控 19:0x 正把 `execution_unknown` 补进 §6（盘上 `git status --porcelain specs/` = ` M specs/events/event-contract-v0.1.md`，是**主控未提交**的那次改动），我立刻复跑同一条腿 ⇒ **§6 词表 64→65、桶一 43→44、桶二仍 21、名单 21、未决 0、`--- PASS`**，与我快照里量到的那一组**逐项相同**（`t405-pred2.log` vs 现在这趟）。同时另一面：**C8-12 `TestEventCatalogMatchesJournalConstants` 已转 PASS（常量 65 = §6 65），`TestEventTypeTripleEquality` 仍 FAIL（yaml 还缺那一枚 token）** ⇒ 卡的那一格现在只剩 yaml/frontend 那半，与 db 交账里说的「补完 §6 还要 frontend 加 token」一致。**这一面绿不是我造成的、红也不是我这侧的**。另记一条纪律：这一趟的 65 依赖主控**未提交**的 §6 改动 ⇒ 它只作为「预测被证实」的旁证，不作为可指到干净树的全量读数（要那一份得等入册后重取）。
  **Stage 追加（09-30 19:5x，T-409）**：
  ① **REQ-T39 裁「名单不动、也不立『有发射位无调用者』新档」，理由我复核成立**：前提自己消失了 —— `grep -n MarkCrashUnknown core/cmd/core/turnpump.go` 现读 `:346`（在 `markCrash` 体内，函数定义在 `:344`）、`handleDeath :292`、`watchDeath :274`、`sup.NewConnection :207` ⇒ 崩溃那一支今天**有非测试调用者**，我那问的第二档根本不成立 ⇒ task #16 闭。同一批里 `execution_unknown` 在名册判据的归属也再确认一次：§6 补那行之后 桶一 43→44、桶二仍 21、名单 21 不动、判据 `PASS`（与我 19:0x 的快照预测逐项相同）。
  ② **主控② 那条原则我接，并把同样的尺用在巡检问的那一格上**（「要不要现在把 `execution_claimed` 纳入那三条腿的反例」）：**不纳入**，理由是实测、不是感觉。副本树 `.scratch/test/tree_t409/` = 现在这棵工作树的完整副本（含主控未提交那 17 行；**真实 `specs/` 与 `core/` 一字未动**，反证：真实 §6 里 `execution_claimed` 命中 = 0），只往 §6 加一行 execution_claimed（载荷 `provider_execution_id`、Audit 横杠），跑五条腿 ⇒ **三处各自点名**：`errcode_mapping_test.go:833`「契约 §6 登记了 execution_claimed，但 eventjournal 没有对应常量 ⇒ 谁发的这条边没有合法类型可用（补常量那边）」+ `:838`「C8-12：目录 66 类／常量 65 个；目录独有 [execution_claimed]、代码独有 []」；`:1092`「三格不相等 [§6→yaml]」+ `:1099`「C8-13：§6=66／yaml=65／常量=65」；`event_producer_roster_test.go:1022`「派生集合里有 execution_claimed 而名单没登记」+ 桶二 21→22。⇒ 再加一枚合成反例是**重复覆盖**；这一档三个方向（§6 先到／常量先到／yaml 后到）今天各自真红过一次，两向都有原文可引。日志 `.scratch/test/t409-probe.log`。
  ③ **一枚我自己的缺陷，先报不动手**：上面那枚实测暴露出名册腿的红话**会把人引向错修法** —— 在「§6 先到、常量未到」这一档它喊的是「无生产者名单少了它」，照着修就是把 `execution_claimed` 加进「本名单当前是 21 条」那句；而那时它其实是**待补常量的新事件、不是缺陷清单项** ⇒ 写进契约就是假事实（与 T-371① 同一条理由）。C8-12 那句指向才对（「补常量那边」）。**建议改法**：把 `:1022` 那句改成两条出路并列（(a) 补常量 + 发射位；(b) 确实没人发才登记进名单）—— 只改措辞、不动判据语义、不缩扫描面。**为什么现在不改**：主控明说「现在一次入册」，我这文件一改就换指纹，那份要入册的绿读数立刻失效（这条是我自己记过的规矩：「只改注释」照样换哈希）。排在入册之后，落点属我自己的 `core/test/**` ⇒ task #17。
  ④ **替主控把入册前要的两条读数取好（只读，没动他的面）**：19:56:14 整包 `go test ./test/ -count=1 -timeout 420s -v` = **rc=0、顶层 53 PASS / 6 SKIP / 0 FAIL（59 条）**，C8-13 随 yaml 那枚 token 一起转绿；19:58:46 `check_ownership.sh` 与 `--selftest` 两 rc=0、0 项越界、七族 ID **50/50** ⇒ 契约① 新加那 15 行是 bullet、不是行首表格行，9b 那道门不受影响。**两面都含主控未提交的 `specs/` 改动**（`M specs/contracts-internal-v0.1.md`、`M specs/events/event-contract-v0.1.md`、`M specs/openapi/core-v1.yaml`）⇒ 它们是「你这一批自洽」的证据，**不是**「HEAD 上已入册」的证据。
  **Stage 追加（09-30 20:3x，T-410）**：
  ① **判决（按主控给的顺序，先判性质）**：这两条红**不是**产品时序没走通，**是**判据前提被 T-392① 那笔线序改动打破 —— 而且是被我原来的**测量方式**打破：`waitUntil(State()==RUNNING)` 断的是一个**回执之后就不存在**的瞬态。取证两条（都不是推理）：
     - 真子进程现场（临时诊断件，跑完即删、内容留档 `.scratch/test/t410_diag_test.go.txt`，日志 `t410-diag.log`）：CallTurn 返回那一刻 tracker 已是 `CLOSED`、`SinceSeq=6`、`MissingSeq=0`；随后读到的六帧是 `turn/claimed(seq=1)`、`turn/dispatched(seq=2, err=nil)`、`stream/state(3)`、`stream/delta(4)`、`stream/item(5)`、`turn/finished(6)` ⇒ **dispatched 上了线、也没被判重**（我先前那个「(turn_ref,sequence_no) 吞帧」的怀疑被这枚读数当场否掉，幸好没把它写成结论）。
     - 机制在 `internal/adapter/stream.go` 里是死的：`Finish` 只有从 `RUNNING` 那一支才会落 `CLOSED`+result（:336），从 `CLAIMED`／空 那一支会得 `UNKNOWN` + 错误（:339）⇒ 观测到 `CLOSED/SUCCEEDED` 就是**曾经 RUNNING** 的机械凭据；另一半 `TurnID` 只在 `Dispatch` 成功那一步被写入（:322）⇒ 它是持久痕迹，不像 RUNNING 会溜走。
  ② **一行产品码都没改**（T-410② 那条禁令按字守着）；改的只有我名下的 `core/test/turn_attribution_test.go`：新增 `evidenceApplied`／`waitEvidence` 两个原语（读持久痕迹），把 T84-6a 与 T84-7a 那两条前提腿从「等 RUNNING」换成「等痕迹」，日志同时印 `State` 与 `turn_id` 让下一次取证不用重跑。红话仍然点名差在哪一帧（`tracker 停在 %q 且 turn_id 一直是空 ⇒ turn/dispatched 没落地`）。`gofmt -l` 无输出、`go vet ./test/` rc=0。
  ③ **两枚反例 + 两枚变异**（T-410③ 要的形状：线序搬过「回执」之后，读到回执就停的助手会静默失去覆盖面）：
     - `TestEvidencePrimitiveHasTeeth`（合成，四档，双向）：走完两帧⇒放行；`dispatched 缺位`／`只有 claimed`／`accepted=false` 三档⇒拦住，且拦住必须带理由（`!ok && why==""` 也算错）。现读 State 分别是 CLOSED/UNKNOWN/CLAIMED/空 ⇒ 四个档位可区分，不是恒真恒假。
     - `TestEvidenceGateOnRealWire`（真子进程）：翻 FakeAdapter 现成的矛盾 id 门 `LUCERO_FAKE_ADAPTER_BAD_EXECUTION_ID=1`（T-275 留的注入缝，**不改任何码**）⇒ 证据被 Core 丢弃，`waitEvidence` 必须喊不成立，且丢弃必须留下接缝记录（顺带把 §4.2「丢弃 + Audit」那一半也盯住）。
     - 变异 `python .scratch/test/mut_t410.py`：**M1**（把尺退回「只要有个状态就算走通」= 读到回执就停那种助手）⇒ 反例腿 FIRED，点名 `dispatched 缺位` 与 `只有 claimed` 两档；**M2**（`waitEvidence` 恒真）⇒ 真线反例腿 FIRED；**control 全 PASS**；每枚跑完字节还原（sha `f18ecd8b858b` 三次一致，`finally` 兜底也 True）。
  ④ **默认面（旗标关）跑出一条新红，归因不在我这侧**：20:32:16 整包 `-v` = RC=1、顶层 53 PASS / **7 SKIP** / 1 FAIL，唯一 FAIL 是我的名册真树腿，红在**桶三 未决 1 处** = `internal/execution/claim.go:69`（`EventClaimed`，候选 1 个、因不在 §6 词表被丢掉 1 个）—— 那是 **backend 未跟踪的在途新文件**（T-408(i) 半成品，常量+发射位先到、§6 与 yaml 还没进），正是我 19:5x 那枚实测预言的「三处同改中间态」。反证：把 `LUCERO_CODE_ROOT` 指到 `git archive HEAD(07d2163) core` 的干净副本，同一条腿 **PASS、未决 0、桶一 44、桶二 21 = 名单 21**（20:36:00）。⇒ 按 T-371① 未决必须红、必须点名，这条红是**功能正常**。
  ⑤ **顺带办结 #17**：`:1022` 那句红话原本只给一条出路（「无生产者名单少了它」），会把人引向「把它加进现状那句名单」= 往契约写假事实；现改为两条出路并列（(a) 补常量与发射位；(b) 确认确实没人发才登记）。改法前先 `grep` 过有没有东西在断旧文案（只有这一处，无消费者）。**并且实测它真会被打印**：在快照里把名单降到 20 条造出 missing 场景 ⇒ `PROBE_RC=1`、新文案出现 1 次、点名 `task_verifying`；跑完快照复原（名单回 21 条、判据回 HEAD 版 59529 字节）。日志 `.scratch/test/t410-redmsg-probe.log`。
  ⑥ **口径变化要明写**（不然下一轮「全绿」的含义就换了）：顶层用例 **59 → 61**（我新增两枚腿），默认面 SKIP **6 → 7**（新增的 `TestEvidenceGateOnRealWire` 在旗标关时 SKIP，属设计）；带旗标那一面新增两枚真跑 PASS。**旗标开**才是 T84 这一族的覆盖面 ⇒ 交账时那句「整包 53 PASS/6 SKIP」**已过期**，现值是默认面 53/7（含 backend 那枚红）、旗标面定向 4 条全绿。
  **Stage 追加（09-30 20:4x，T-411 三条验收）**：
  ① **措辞**：`:1022`（现读位置随插入下移到 :1100 那一支）已拆成两条出路、不预设修法 ——(a) 名字已在 §6 登记而码里还没人发 ⇒ 那是三处同改的中间态，要补的是常量与发射位，**别动名单**；(b) 确认确实没人发 ⇒ 才加进事件契约那句现状名单。改前 grep 过全仓只有这一处、无消费者断旧文案。
  ② **常驻反例腿**：新增 `TestProducerRosterComparisonLegs`（不是上轮那枚一次性快照探针）。四档都断**集合行为**：真漏登记 ⇒ `extra` 恰好点名那个名字；登记对了 ⇒ 两头都不许喊（这是"该放行必须放行"那一腿）；登记反了 ⇒ `missing` 点名；名单里混进词表外的名字 ⇒ 两头同时喊（只判名字前缀，不判说明文字）。**「改前改后都必红」两半都跑了**：同一个把名单降到 20 条的漏登记场景，用**旧措辞版**判据跑 ⇒ `rc=1`（红话＝「无生产者名单少了它」），用**新措辞版**跑 ⇒ 同样 `rc=1`（红话＝两条出路并列），两边都点名 `task_verifying`。日志 `.scratch/test/t411-*.log`、`mut_t411.py` 的输出即原始记录；场景造在快照里，跑完快照复原（名单回 21 条、判据回 HEAD 版 `da8d0b14e73b`）。
  ③ **不许只验打印文本** —— 这条我用变异来证，不靠嘴说：`R3` 把那句红话**整行退回旧措辞** ⇒ 常驻腿仍 `PASS`（说明它判的不是文本）；`R1` 摘掉比对的 `extra` 那一半 ⇒ 当场 FIRED（2 个子档）；`R2` 摘掉 `missing` 那一半 ⇒ FIRED（3 个子档）；control 全 PASS；三枚各自字节还原一致（`还原一致 = True` ×3）。
  ④ **三面终读（20:41:12~20:41:33，同一棵工作树）**：默认面（旗标关）`RC=1`、顶层 **54 PASS / 7 SKIP / 1 FAIL（62 条）**，唯一 FAIL 仍是名册真树腿、红因 `未决发射位：internal/execution/claim.go:69`（**backend 在途**，未跟踪文件，不是我）；把 `LUCERO_CODE_ROOT` 指到 `git archive HEAD(07d2163) core` 的干净副本 ⇒ 同一条腿 **PASS、未决 0**；旗标开那一面定向 7 条（T-410 四腿 + 新常驻腿 + 扫描器／解析器反例腿）**全 PASS、RC=0**。**顶层数又从 61 变 62**（我加了常驻腿），交账里那句「53 PASS／6 SKIP」彻底作废，现值以上面这三面为准。
  ⑤ **主控的禁令按字收下**：`execution_claimed` **不许**进「本名单当前是 21 条」那行 —— 盘上现读 `grep -c execution_claimed specs/events/event-contract-v0.1.md` = **0**；而我这条判据在「常量／发射位先到、§6 未到」时把它报成**未决**（正是现在那枚红的形态），在「§6 与 yaml 齐、发射位也有」时把它算进**桶一**——两种都不是登记它的理由，所以禁令与判据行为一致，不需要我另加闸。
  **Stage 追加（10-01 12:2x，T-418）**：
  ① **REQ-S-1 的选项对不上，我按你留的口子先写三行、不套你的措辞**：**判据名** = `core/test/` 里那条「send 面在 handler 写事务内做网络往返」的判据（我 09-27 T-312 提的，账上别名「handler 层判据」）。**现在闭到哪** = 一个字没落（既没写用例也没进契约），前置是「backend 把连接面搬出写事务」（T-346 裁乙），它今天是否已完成我没现读，你一句话我就核。**要你挑的是哪两个** = 我账上框的是**三选一**（甲＝我落用例但装配是复刻／乙＝backend 先搬出去并开一条注入面／丙＝backend 自己交现场、我只出行文本），而 T-418① 讲的是「走过 `Close()` 那半已闭／abandon 那半不立自救判据 + 另立一条资源判据」——**两组不是同一件事**，所以我不把你的裁定复述成我的选项，等你先认这个差异。
  ② **两面口径已升成我自己的验收项**（见上面 Completion criteria 那条新勾），引用按章节名不写编号：契约① 第 24 条「证据生命周期」里新加那款。现读 `grep -n 交付前必跑清单是两面的 specs/contracts-internal-v0.1.md` = `:671`。
  ③ **你要的那条非计时资源判据，从包外写不出来 —— 障碍是实测不是推辞**：`Connection.pending` 未导出，而 `Connection` 的导出面只有 12 个方法（`grep -n '^func (c \*Connection) [A-Z]'` 现读：Child／Handshake／Initialize／Call／CallTurn／Cancel／Inbound／Turn／LastSeq／MissingSeq／SinceSeq／Close），**没有任何一个能读出 pending 的大小**。包内那条 `TestAbandonedWriteDoesNotLeakItsGoroutine`（`internal/process/connection_test.go:1276`）盯的是 `runtime.NumGoroutine()` 回落到 base、且带 5s 轮询 —— **既不是 pending、也不是非计时**，而它调的是未导出的 `conn.request(...)` ⇒ 这类断言只能住在那个包里，而红线不许我在别人包里建 `_test.go`。读码补一条：`request()` 四条出口（写失败 :446／回执 :456／ctx 取消 :464／dead :467）都调 `forget(id)`，`kill` 只 `close(c.dead)` 加遍历 `c.turns`、**不遍历 pending** ⇒ 今天没有「abandon 留下 pending」的已知路径，但也没有任何包外的眼睛盯它。
  ④ **因此请你二选一（我不碰产品码，要动先报你）**：**(A)** 派 `adapter` 在 `internal/process` 开一个只读面（如 `PendingCount() int`，属观察面不属自救逻辑，不与第 23 条⑧款冲突），我在 `core/test/` 落那条非计时资源判据 ⇒ 覆盖面变成一条可指认的命令；**(B)** 这条判据由包内测试覆盖（backend／adapter 自己交），我只在契约① 判据表那格写明「包内判据、外部不可见」。我推荐 A，理由是 B 会把这条永远挂在别人的自觉上；但如果 A 被认为开了产品面的先例，我就按 B 落文本、不自作。
  ⑤ **顺带把 `execution_claimed` 那批的量完状态交给你**（它正好重演并走完了我 19:5x 预言的那条路）：现读 §6=1、`core-v1.yaml`=1、`internal/execution/claim.go` **已在 HEAD 里** ⇒ 名册判据 §6 词表 66、发射位 31、**桶一 45**（新名由 `claim.go:69 Service.RecordClaim()` 认出，M=0／K=1）、**桶二 21 = 名单 21 —— 那句现状名单一个字没加**（与你的禁令一致，也不需我另加闸：它算有生产者，天然进不了名单）。
  ⑥ **两面终读（12:27:42~12:28:02，HEAD `1cca6fe`，我名下只动 `projection_test.go` 一处文案）**：默认面 `go test ./test/ -count=1 -v` = **rc=0、55 PASS / 7 SKIP / 0 FAIL（62 条）**（昨天那枚 backend `claim.go` 的未决红已随三处同改补齐而消失）；旗标开定向 8 条（T-410 四腿 + T-411 常驻腿 + 三条名册腿）= **rc=0、8 PASS**。新文案的落点与原文都在 `t418-default.log` 里。
  **Stage 追加（10-01 12:5x，T-422）**：
  ① **主控要的逐字三行**（原文来自本账 09-27 T-312 那一格，锚字「三选一」；`grep -n 三选一 .qoder/progress/test.md` 现读落点 `:209`（我 12:2x 写的 :192 已被我自己这几轮的插入顶掉 ⇒ 行号只能当场取；选项正文另在 `:2544`），**行号会漂、认锚字**）：
     - 判据名：`core/test/` 里那条「send 面在 handler 写事务内做网络往返」的判据（账上别名「handler 层判据」，REQ-S-1）。
     - 现在闭到哪：**一个字没落** —— 没写用例、没进契约；前置是「backend 把连接面搬出写事务」（09-27 T-346 裁乙时给的条件）。
     - 要你挑的是哪三个（**不是两个**）：甲＝我落用例但装配是复刻（`httpapi.Config` 23 个字段要复刻，复刻装配≠生产装配）／乙＝backend 先搬出去并开一条注入面（新生产面）／丙＝backend 自己交现场、我只出行文本。
     **新量到的事实（12:59:30，可能让这道题变简单）**：乙 的前置**盘上已经完成** —— `core/api/http/executions.go` 现在两处 `Store.Write`（:316／:371）的闭包里只剩 `ExecutionSend.Reserve` ／ `recordSendIdentity` ／ `sealIdempotency` 这些 DB 动作，`Deliver(...)` 与 `CallSendTurn`（实现落 `cmd/core/sendpipe.go:118`、调用者 `internal/execution/send.go:277`）都在闭包**外** ⇒ 网络往返已不在写事务里。**所以要你决定只剩一句话**：这条判据由**我落（甲，装配是复刻）**还是**backend 自己交现场（丙，我只出行文本）**——乙 那半（搬出去）不用选了，它已经发生。我不替你选，也不把这句话替你复述成别的形状。
  ② **(B) 的执行边界与交接**：判据只能住 `internal/process` 包内，而那按 §7 归 **adapter**，且红线不许我在别人包里建 `_test.go` ⇒ 我这侧交付的是**可粘文本 + 三条读数 + 一条真跑过的当场命令**，见 `.scratch/test/t422-pending-handoff.txt`（判据名／为什么只能包内／非计时口径：`Close()` 返回后**立即读一次** `len(pending)` 回基线，不许 sleep 或轮询『过一会儿就没了』——那正是现有 goroutine 用例的形状）。**我不写包内那条，也不开导出 API**（(A) 已被否，且我自己也说清了 A 的代价：为一条测试把内部状态升成对外契约）。
  ③ **一次自纠**：我 12:29 那封里把「包外写不出来」讲成了道理；这一轮按主控要的口径**去撞了一次编译**，读数在 `t422-compile-attempt.log`，临时件 `core/test/t422_try_test.go` 已删（`cmp` 证明 `projection_test.go` 逐字节未变、`git status` 我名下仍只有它那件 ` M`）。巡检那条「命中数≠语义」也记我账上：`grep -c '尚无 owner'` 现在仍会命中 1，因为那一处正是我新写的引用句 —— **那一格今后用行原文判，不用命中数判**。
  ④ **等谁更新**：`TestProjectionRejectedJournalAndAudit`（§5.4 那格）的前置从「等主控裁 owner」改为**等 backend 交 T-421（`FinishFromTurn` 三枚 id）与 T-424（`/output` 写者）**；它交完我就开旗标转真跑并报名字（解锁人＝backend，不是主控、也不是我）。
  ⑤ **T-422 直投版的 ⑤ 已升成我的验收项**（就是上面那条改写的勾），本轮就是按它跑的：`-count=3` × 两面 = 6 趟，切趟的方法是先数顶层 `--- ` 行（186 行 = 62×3）再按首条用例名回映分段，**不靠 `cut`/`tail` 猜**。日志 `t422-default-c3.log`、`t422-flagon-c3.log`（都是先落盘再读尾巴）。
  ⑥ **一枚不在我名下的新红，和它的摘条候选**：见头部那段。要提醒的口径差异：旗标开那一面是 **60 PASS / 1 SKIP**，默认面是 **54 PASS / 7 SKIP** —— 差的 6 条正是旗标打开后才真跑的那族（含 T-410 的 `TestEvidenceGateOnRealWire`），这也就是 ⑩ 款存在的理由。另外**这次红是我这条判据在正常情况下该红的样子**：它点名到 `execution_output_flushed` 与发射位行号，两边都有人改（名单归主控、发射位归 backend），我没有为了让它绿去缩扫描面。
  ⑦ **②③ 的答复已交（本账 12:5x 那几格），主控直投版没改结论**：② 裁 (B) 归包内、owner=adapter，我这边交的是失败读数 + 包内可写证据 + 非计时口径（`t422-pending-handoff.txt`）；③ 的逐字三行在 `:2544~2551`（短版 `:209`），并附「乙 的前置已实测完成」⇒ 只剩 甲／丙 一句要裁。
  **Stage 追加（10-01 15:0x~15:1x，T-431）**：
  ① **判据形状**：`core/test/wiring_surface_test.go`。输入源=契约① §3.0-c 那张表（现读 5 行），脚本里**不内嵌清单**；每条一个子用例，红话点名到符号、定义文件与实际声明的类型名。扫描口径三层收口：非测试文件 → 排除定义它自己的那个包 → **要求调用方文件导入定义包**（最后一层是我自己撞出来的：`Request`／`Resolve` 这类常见名光按名字数会把别人的调用算成这条接线；排除项**计数打印**，不静默变窄）。表一行都读不到就 `t.Fatalf` 停 —— 看不见东西的判据不许报绿。
  ② **两面读数**：HEAD `dbd5a51` 的 `git archive` 副本 = **5 行全 FAIL**（`Decide`／`CheckTemporaryGrant`／`Request`／`Resolve` 四处「调用点 0」+ `ApprovalGate.BlockingState` 一处「符号不存在」）；含 backend 在途的工作树 = **4 FAIL / 1 PASS**，绿的那行是 `approval.Service.Resolve` ⇒ 1 处调用点 `api/http/approval_resolve.go:117`（该文件未提交，`git cat-file -e HEAD:` 判定不在 HEAD 里）。⇒ 与派发那句「今天应当红，红的就是那五处」一致，且多给了一枚**表已经开始烂**的证据。
  ③ **自证三支**（都在 .scratch 副本里做，真实产品码零改动、残留 0）：**正向对照** = 副本 `cmd/core` 里加一个带三条 import 的非测试文件，造出四个包外调用点 ⇒ `Decide`／`CheckTemporaryGrant`／`Request`／`Resolve` **四行 FAIL→PASS**，而 `ApprovalGate` 那行**不动**（它红在符号不存在，加调用者救不了 —— 两支红的性质不同，这一枚把它证出来了）；**反例** = 摘掉那个文件 ⇒ 回到 5 行全红、且与基线**逐行相同**；**③** = 带 `-v` 那趟 `=== RUN` 计数 6（>0）。日志 `t431-{baseline-head,positive,negative}.log`。
  ④ **交回主控两格**（已开 task #19，笔不在我这）：(1) §3.0-c 第 5 行的 `execution.ApprovalGate.BlockingState` 是**错名** —— 盘上实名 `internal/execution/approval.go:5` 的 `ApprovalStates`（`ApprovalGate` 全树命中 0）；(2) 第 4 行 `approval.Service.Resolve` 标的「0」在 backend 那件入册的同一刻就变了 1，要么同步改表、要么让判据替它说话（现在它会以 PASS 的方式替这张表说话）。
  ⑤ **名册判据那枚红已经没了，而且不是我改松的**：15:10:52 现读契约 `:322` 写着「本名单当前是 **20** 条」、实取 20 个、`execution_output_flushed` 已不在句里 ⇒ 是主控按 task #18 的出路 (1) 落的笔。两面各 `-count=3`：默认面每趟 55 PASS / 7 SKIP / 1 FAIL、旗标开每趟 61 PASS / 1 SKIP / 1 FAIL —— 那唯一 1 FAIL 就是**新判据本身**（今天该红），名册判据三趟全绿。判据文件我一字未动。
  ⑥ **口径又变一次，必须明写**：顶层用例 **62 → 63**（新增 `TestWiringSurfaceHasCallSites`），默认面 FAIL 从 0（13:2x 那次是 1，因 roster 红）现在稳定为 **1**，且这一枚红是**我这条新判据在报接线没落地**，不是我的用例坏了。交账时「全绿」这个词我不会用，我只报三个计数与它们各自的归属。
  **Stage 追加（10-01 16:0x，T-437 甲 落盘）**：
  ① **为什么这一版比我自己 09-27 写的甲更好**：甲 原写「测试自持 adapter 子进程 + 真 handler」，我当时的顾虑是 `httpapi.Config`（现读结构体字段 **17 个**）要复刻 ⇒ 装配不是生产装配。落盘时改走了 `crash_test.go` 已有的那套真 Core 夹具（`buildCore` / `coreRun.boot` / `apiCall` / `seedCrashFixture` / `awaitPID` / `killPID`，同包可直接复用）⇒ **跑的是生产装配、生产路由、生产 SQLite**。
  ② **枚出来的实测障碍（它决定了形状）**：真 fake-adapter 那条路**不能**用 —— Core 侧没有 `debug/*` 的 HTTP 路由（`grep -rn "debug/" core/api/http/*.go` 非测试零命中），从外面装不上 `debug/hang`；而真 fake 的 `session/send` 回执在 T-392① 之后来得很快 ⇒ 阻塞窗口≈0，那条「1 秒内 201」会**空过**。所以挂的那台用 `-adapter hang=<本测试二进制>`（孩子连握手都不回，crash_test 已证这样的请求真的挂着）—— 顺带这条**不需要 `LUCERO_TEST_FAKE_ADAPTER` 旗标**，也就躲开了「被 SKIP 遮住」那一族。
  ③ **自证**：`control` PASS（=== RUN=1）；**M1** = 把「send 还挂着」那一支强制改成不成立 ⇒ 判据当场拒绝：`采样条件没成立…1s 的断言是空过，不接受这种绿`（日志 `t437-M1.log`）；改完字节还原一致 （`a43bb892c198` 两次相同）。锚点这次是**按行**定位的（前两次数缩进把自己改坏过两次，记这一笔）。
  ④ **口径**：顶层用例 **63 → 64**（新增这一条），默认面 PASS 由 55→56。交付口径按 ⑩ 款：两面各 `-count=3`、只从带 `-v` 那趟数；两面三趟的 FAIL 集合完全相同 = `TestWiringSurfaceHasCallSites` 一格（四子档），那是 T-430 派给 backend 的四格接线，**我不替它关、也不把判据降成打印**。
  **Stage 追加（09-28 19:2x，T-276 收口轮：修越界 + 三腿）**：① **那条真红是我自己上一格造的，自造自认** ——
  `core/test/crash_test.go:1409` 在 `TestCrashApprovalNotLost` 重启后那段用了裸
  `SELECT state, execution_id, expires_at FROM approvals WHERE id = ?`，**违反契约③ §3.2.1 第 1 条**
  （`approvals` 表的 SQL 只准出现在 `internal/approval` 包内）**与 §4 C1**（跨模块读今天不红是设计，
  **唯一写死的例外是 approvals** —— 裁决数据连只读 SELECT 也不许出那个包）；
  19:03:18→19:04:50 那次正跑 `OWN_EXIT=1`、1 项 VIOLATION 指名到这一行。
  ② **修法**（按你给的推荐，先自己核过再改）：`internal/approval/store_sqlite.go:439 func (s *SQLiteStore) Get` 现成可用，
  ⇒ `crash_test.go:1408~1418` 改成 `approval.NewSQLiteStore(d, now).Get(ctx, approvalID)`，
  取 `a.State / a.ExecutionID / a.ExpiresAt`；**断言语义一个字没动**（PENDING / execution_id 未换 / expires_at 非 0 三条原样，
  1419~1428 未触碰），行消失的分支由 `sql.ErrNoRows` 换成 `errors.Is(err, approval.ErrNotFound)`
  （`Get` 把 `ErrNoRows` 包成 `ErrNotFound`，见 store_sqlite.go:441~443），文案仍是「§7：Approval 不丢」。
  ③ **自查 grep（契约给的口径）** `rg -n "FROM approvals|UPDATE approvals|INSERT INTO approvals" core --include=*.go`
  ⇒ **15 条命中、全在 `core/internal/approval/` 下**，`core/test/crash_test.go` **0 命中**；
  ④ **三腿**：ownership selftest 19:10:02→19:10:22 `SELFTEST_EXIT=0`、正跑 19:10:31→19:11:45 `OWN_EXIT=0` /
  VIOLATION 0；t47-smoke 与 build_stamp 是**别人的腿，我代跑、原文交主控入册，不占我自己的判据面**
  （smoke 22 断言 21 OK / **1 FAIL**，见 Verification evidence 的归因；build_stamp `STAMP_EXIT=0` 拿到 HEAD 短码与构建指纹）。
  09-24 07:5x：T-223 交完。①主控收回对我判据的指控，我已就地记账（并把"0/55 那个数不能被读成
  反对修法 (a)"钉死——它只否掉了"端口复用是红因"这一件事）。②量级判据落成 FL 表的一行 = **REQ-T19**
  （`single` 档、**n=122**，从基线 4/65 的 Wilson 下界 2.42% 反推；两腿里只有确定性那条能关 REQ）。
  **自证把我自己的工具打回炉**：122 次里 9 次 SKIP 被旧 `classify` 印成"122 次都没红" ⇒
  见 SKIP 现在 exit 2、红+SKIP 按跑过的次数换算、FAIL/SKIP 都自带现场；`--selftest` 10 ⇒ 13 条（双向变异验过）。
  **另一件大事在我采样的过程中发生**：REQ-L-1 的修法 (a) 已被 backend 落盘（**未提交**，`lock.go` md5 `16e09a92`），
  验收① 的确定性反例 `cmd/core/lock_test.go` 的 `TestLockJudgesDeadByPortNotPid` 也在盘上 ⇒
  **我的量级腿等他们交账后再跑**；新锁代码上带 pid 流失载荷两批合计 1/99 红（07:36 那批 39 绿 1 红、
  现场丢了；07:46 批 60 绿 0 红），机制未定、已连候选一起写进 Completed 交 backend。
  我的旧归因按字回改（`crash_test.go` 形状判定不再指向已删除的 `os.FindProcess`），并提成可测函数 +
  新增第 56 条顶层用例（4 个合成输入、两支变异各自精确命中）。
  最后重跑：默认面 **顶层 56 = 56（42 PASS / 14 SKIP / 0 FAIL）**、子用例 19（17/2/0）；
  开 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **47 PASS / 9 SKIP / 0 FAIL**、子用例 31（29/2/0）；
  vet 0、`go fmt` 改写 0 个文件、lint 与其 selftest、`flaky_watch --selftest`、`crash_recovery` 全 exit 0。
  **本轮没跑 18 包全量**（`core/` 里 7 处未提交 + 3 个未跟踪文件都是别人的在途码，那份读数什么都证不了）。
  等主控（10:48 更新）：**REQ-T19 / T20 / T21 / T22 / T24 已全部裁定并落文**（T-270、T-271、T-265；落点我逐项在盘上自读过）。
  现在悬着的只有：**REQ-T25（判据表 ID 重号这条腿要不要加 —— 已有一例：契约② 里两个 `FL-6`）**、REQ-T14 剩余镜像、
  T84-7b 的 backend 第二段，以及 FL-10（原 FL-9）量级腿等 backend 提交 REQ-L-1。
  **REQ-T24 已裁（T-265，09:23/10:01）**：权威 = 事件契约 §6；两个名字本来就该是两个；引用侧必须写完整名；
  比对前先走 §6 展开约定 ①~⑤ ——**这条裁定把我自己那条"缺行"观察也否掉了（那是字面 grep 的假红），已就地纠正**。
  另有一件**不属于我但合验前该确认的**：db 的 `events.go` 那格还没提交 ⇒ 现在做干净检出会红一条（10:02:28 复量）。
  **Stage 追加（08:37，T-236 已办）**：ownership lint 加了四类"值位"形状排除（路径段 / 与变量比较 / `return` 非首元素 / `strings.*` 非首参），
  并配六个"不许红"+ 三个"必须红/归类不许漂"的合成件与两次双向变异。**注意：08:16 我重跑时它已经不红了**，
  因为当事人把字面量拆成了 `strings.TrimPrefix("/messages", "/")`（`core/api/http/sessions.go:101` 的注释自陈是刻意躲守门）
  —— 那条绿是躲出来的、不是修出来的，证据已交主控转 backend；我这轮的修法把"躲的必要"直接拿掉了。
  **Stage 追加（11:12，T-273 七件）**：③ 那两问我自己复量后 closes 掉了（契约② §4.1 已裁 + 代码跟上），
  `TestWriteEntryOpenQuestions` 从 SKIP 变硬断言 ⇒ 两面各 +1 PASS/−1 SKIP、顶层仍 56、**0 FAIL**；
  ④ 的"表名来自变量"计数已落地并验过不是死腿；⑤ 清点表在 Completed；⑥ 可粘文本在 REQ-T26；
  ① 的 7b 复量后仍 SKIP，但缺的那一段现在有名有姓（Core 侧没人发起 turn）。**新悬着等主控的：REQ-T25（两个 `FL-6`）、
  REQ-T26（F20 四行补锚 + 对照腿）、REQ-T22 已裁无需动作、REQ-T21 归 backend。**
  **Stage 追加（18:52，T-280 两件）**：① 已办并已入 HEAD（`95b01ca` 里含 E 类键位排除，`git status` 对我地盘 = 0）；
  ② FL-10 量级腿**今天没跑成**：18:17/18:34/18:35/18:41 四批全被自己的门挡下（SKIP 不算样本 ×1、
  116/39 次没拿到 `---` 行 ×2、前后指纹跨版本 ×3 次，其中一批是 122 全绿），**没有一批我交出去**；
  已换成分段累计跑法（同指纹才并池、每段自带载荷 spawn 数），在跑。**等的是这棵树安静下来，不是判据。**
  **Stage 追加（19:1x，T-280② 收尾 + T-299 放行；本段是 Stage 的当前态，上面 18:52 那段"在跑"已过期）**：
  树静下来了、backend 也交账了（`a9f975e` 19:03:11 提那 7 个文件）⇒ **FL-10 量级腿交齐：19:08:22~19:14:02 一次跑完 122 次、
  0 红、前后同指纹、两道前置门起跑时都在**（门① 已长进 `flaky_watch.sh` 的 `preflight()`，selftest 14→17 条、两次变异验过）。
  18:48 那 124 次的并池批我**不拿它交关**（当时门① 没跑、门② 是"未提交"），只当旁证 —— 它和新批指纹逐字相同，
  说明那批采的就是后来原样交上来的这版码。**REQ-L-1 不由我关**（确定性腿在 backend）。
  新挂出去的两条：**REQ-T27**（第一问已被 T-299 答掉、销；留了"起跑前现读判据行"这条我自己的补法）与
  **T-293④ 的答案 —— 它落在 REQ-T25 里**（ID 去重腿"加不加、放哪儿"我答了"加、放 `check_ownership.sh`"，
  七族基线现量已附；我先写成 REQ-T28 一条，随后自己作废并并进 REQ-T25 —— 报"重号登记"的人不能自己长第二条）。
  等主控 = REQ-T25 的裁 + REQ-T26 的落文；等 backend = 确定性腿那一格的现场；我自己下一批要补的 = 窗口内每 30s 记进程数。
  **Stage 追加（19:4x，T-303 四件）**：① 主控 19:32/19:40 两次盘上核到我的读数（五项逐字对得上）；② **FL-10 正式交账已写成可粘文本 = REQ-T29**（含状态列整段替换文本、负载条件原文 `载荷=churn-1 spawned=9311`、并发实事、四批作废留档）；③ **19:3x 起我不再起任何采样**，窗口让给 adapter 的空机复跑，再采等主控通知或等对方交账；④ §6 那次更正我自己核过（`6b28868` = 13 行插入 / 0 删除、全是 `>` 散文）⇒ 判据输入没变、不重跑，lint INFO 与 19:20 那份逐字相同。**新增等主控：REQ-T29 落文 + "无生产者名单"要不要做成判据（新格，我不自拍）。**
  **Stage 追加（20:1x，T-312 新格 REQ-S-1）**：只交三样。
  **事实**（全现读）：`debug/hang` 在生产码零调用点（只有 `cmd/fake-adapter/main.go` 与它自己的测试）⇒ ① 那一形只能走
  「测试自持 adapter 子进程 + 真 `api/http.NewHandler`」，**不是真 Core 进程**；而 `httpapi.Config` 有 23 个字段要复刻 ⇒
  复刻装配 ≠ 生产装配。**现状本身就是 ② 的反例**：`api/http/executions.go` 在 `Store.Write` 里同步 `CallSendTurn`，
  写通道是 `internal/platform/db/db.go` 的 `SetMaxOpenConns(1)`（**巡检给的 `platform/db/db.go` 少了一层 `internal/`**，
  行号 40/48 对）。
  **行草稿**在 REQ-T30（状态列写「未自证」，不写现存）。**三选一**：甲=我落用例但装配是复刻 / 乙=backend 先搬出去
  并开一条注入面（新生产面）/ 丙=backend 自己交现场、我只出行文本。**本轮没跑也没落文件**：⑤ 的空机窗口 adapter 还占着
  （其账本 19:55:21 第 23 轮）、`core/` 未提交 7 处含我要依赖的 `api/http/handler.go` ⇒ 读数不可归属；
  未跑通的命令也不许进契约（我立过的那条）。**等主控 = 三选一裁定；等排程 = 窗口还给我。**
  **Stage 追加（20:4x，T-316④⑤）**：④ 已落地 = `check_ownership.sh` 的 `check_id_unique`（标 9b/9、没改 /9 号）
  —— 真树 **49/49 无重号**、selftest exit 0，三把变异锤（后缀摘掉 / 重号支永假 / 下界闸摘掉）分别打 2、1、2 条 FAIL，
  还原 md5 `024c755a` 逐字相同 ⇒ **REQ-T25 销账**。⑤ 裁「做」但我**没落**：派生规则（常量被引用=有生产者）量出
  21 个零生产者 vs 名单 3 个，而我读到 `internal/project/service.go` 的发射位是**Type 字段的值是裸字面量**
  ⇒ 规则天然看不见这种写法，落地就是 18 个假红。两条路写在 **REQ-T31**（甲 = 落到 `core/test/` 用 `go/ast`
  + 常量回解，我推荐；乙 = 仍进 shell，但要 backend 先给「什么算发射」的机械定义）。
  另：门① 的脚本已被主控入册成 `e94c989`（我 19:48 那句「仍未入册」当场作废）。
  **等主控 = ⑤ 两条路挑一条（+ 要不要我做那张分诊表）+ ④/⑤ 的落文；等排程 = REQ-S-1 那格按⑥排在修完之后。**
  **Stage 追加（09-27 12:2x，T-346 三件）**：停摆 47 小时先复量再动手 —— ④ 那句「现在是窗口」在我 12:0x
  复量时成立、12:18 起不成立（backend 落笔 `execution/send.go` 写 T-346① 那一搬，`SendResult{}` 当
  `SendReservation` 用 ⇒ `./test/` 整包 build failed）。我按自己的门①停下：**没有绕过去拿数**，也没把
  「曾经绿过」那次当真读数 —— 12:17 那次连指纹都是假的（内嵌 sed 报错 ⇒ 空输入 ⇒ 空清单指纹永真）。
  **② 代码已落地**（`core/test/event_producer_roster_test.go`：AST 扫 `EventDraft` 发射位 + 按包回解 +
  复用同一份 §6 展开 + 双向对拍，三条腿齐），**自证两腿等编译面修好**；**③ 分诊按住**（分的是别人手上那半棵树），
  先交形状普查：字面量发射 10 处 / 标识符发射 43 处 ⇒ 两种写法并存，但**统一走常量不是我这格的前置**；
  **① 按裁乙不建过渡件**（恒红不进判据表，且 backend 正在搬）。可复现那半句：9b 门 12:2x 复跑 49/49、
  lint 与两个 selftest 全 exit 0 ⇒ `scripts/check_ownership.sh` 这一版能被下一轮逐字复现，等主控按角色切一笔。
  **等 backend = 交账（一次解锁②两腿 + ③分诊 + ①判据）；等主控 = 9b 入册 + 那一刀要不要立项。**
  **Stage 追加（12:4x，T-346 补裁）**：① 真树腿触发条件裁 (b)、我写成三条附条件的执行版（REQ-T33）；
  ② 失明语义改成两级 —— 能归因到名字的 FAIL 并打印 `file:line`、归不到的打 INFO，合成件加了同包名歧义那一格喂它，
  12:43:41 重跑 `TestEventProducerScannerLegs` **PASS**、`go vet ./test/` **exit 0**，树 = `HEAD=67f064b`、
  指纹 `b7fe9fe6…`（编译输入 2354 个，前后同值）；**真树腿没起**：`core/` 脏 6 行里除我那件还有 5 件在 backend 手上（Ⅱ）。
  **补一句（12:46:50）**：裁 (b) 已落进**契约① 第 24 条「证据生命周期」第⑧款**，我以后交账按这个名字引、不引编号；
  款里把「不可达」与「该等」分开了——我这条腿**不是不可达，是该等**（等 backend 那五件），措辞按款改口。
  **等 backend = 那五件落定（同时解锁真树腿、③分诊表、①判据）；等主控 = 我交账后按角色切一笔入册。**
  **Stage 追加（13:2x，T-355 修回解）**：主控的反例（`task_running` 走 `Type: 变量` 汇聚）推翻了我那句
  「回解不通不影响归类」；回解改成三层（包常量 → 局部/跨文件赋值链 → 词表内的文件兜底），五轮都被合成腿抓着改，
  覆盖从 15 → 49 个、红从 49 → 26 条、看不见从 12 → 1 处。**判据现在站得住且现红**：词表 64、查无生产者 29、
  名单 3 ⇒ **26 条未登记**，每条都机械核验（非测试码里除定义位零痕迹）；A（工作树）/B（HEAD 快照）两棵树同分类。
  三面计数：整包 58 条，唯一的 FAIL 就是这条判据（面 A 45/1/12、面 B 50/1/7）⇒ 我预告过的「我的在途件会吃掉
  全仓绿」成立，红因是 finding 不是坏用例。**没扩名单、没降 INFO、没缩范围**（T-355 三条不许都守着）。
  **等主控 = REQ-T34 那两件**（26 条怎么处置 + 行文本现在交还是稍后交 + ⑧款Ⅱ 那句「可跑 HEAD 快照」认不认）；
  **等 backend = 无**（它已入册，我这轮不靠它）。
  **Stage 追加（13:3x，T-359 自查）**：巡检那句「只剩你自己那一件只在 13:18~13:19 成立」把我自己的标签照出来了——
  我上一轮写的「B = HEAD 快照」其实是 13:06:44 导的 `2964ecb` 树，**早于 `9f07a80`（13:09:08，backend 那五件**
  的入册）**。结论没翻（那仍是可归因的已入册树），但按 ⑧款Ⅲ 这属于「把哪棵树说错了」⇒ 13:33 重导真 HEAD
  （`69e96ec`）重跑 B2：**红仍 26 条、与 A 的红名单逐字相同**；补记已追加进同一份证据文件，原段不删。
  13:34 复读 `core/` 又脏了三件（backend 在做 send 期限那一格）⇒ A 面不重跑，B2 才是这轮的可归因面。
  **补（13:4x，T-363）**：Ⅱ 又关了（backend 三件在 `core/`）⇒ 工作树那面我没起；巡检要的当场数字走 HEAD 快照给的：
  **26 条未登记**（44/41 是修回解前的中间读数，状态列不用它）。三份已入册树 + 一份工作树的红名单逐字相同。
  **全仓绿我没交、也交不出**（唯一 FAIL 就是这条判据）。仍等 REQ-T34 那两件（26 条处置 + 认 HEAD 快照那句）。
  **Stage 追加（14:13，T-365②③④ + T-346③ 处置表）**：主控点的三件都落了，而且**没有为了让扫描看得见去碰 adapter 一行码**。
  ② 覆盖集以前不筛词表 ⇒ 我把 `task: %s`、`payload: %w` 这类格式串当成「有生产者」，报出去的「覆盖 49」里有 14 个是垃圾。
     现在 `covered = values ∩ §6 展开集`，只有 covered 写进 emitted；**危害不是今天红，是下一轮漏真红** —— 这一条按字修掉了。
  ③ 每个位点打印三个数：`候选 N / 因不在 §6 词表被丢掉 M / 实际覆盖 K`，汇总行另给一对（N=109、M=64、K=35）。
     兜底（lits）那一层**故意不预先筛**，否则 N/M 只剩一个数、过滤生效与否就没证据了。
  ④ 唯一看不见的位点 `internal/process/lifecycle.go:223` 报成**具名盲区**：盘上原文 221=`db.EventDraft{`、222=`EventID:`、
     223=`Type:` ⇒ 巡检给的「223=EventID / 224=Type」与盘上差一行，我按原文并列给，不靠裸行号吵架。
     看不见的原因写清楚了：`evType` 是 `takeEdge` 的第 4 号**形参**，值从 :166/:173 两个调用点流进来，而我只解三层。
     这一处**影响哪几个名字的归类**是算出来的（`blindImpact`，只诊断、一个字都不写进覆盖集）：`process_crashed process_exited`。
  ⑤ 处置表（`roster-triage-0927-1409-00bb3c4.txt`）：26 条 = ① 盲区所及 2、② 只有定义位 22、⑤ 只在错误文案里 2
     （`task_accepted` service.go:382、`task_preparing` service.go:335 两处 fmt.Errorf）、③④ 各 0。
     **现状更正（09-30 16:2x，T-400②）**：这一行是 09-27 那棵树的分类读数，**其中两条今天已翻面** —— `task_accepted`/`task_preparing` 不再属于「只在错误文案里」，第五层认到它们的真发射位（现读 `internal/task/service.go:385`、`:338`，**行号与上面那两个不同：上面是 09-27 那棵树的位置，别拿它引契约**）。名单已随 `0dceb09` 摘到 21 条 ⇒ 这一格的 26 已不是现状。
     v1 那张表把注释也算成「其它引用」、得出 10 条待查 —— 分类错了，v1 删掉只留这份；**结论面从「26 条一锅端」变成 24 归 backend、2 归我这层要不要通**。
  变异四判各自 FIRED、文案互不相同（甲 摘交集 / 乙 摘反例 / 丙 摘影响面 / 丁 让影响面渗进覆盖），跑完字节级还原（sha 前 12 位 7f82e97eb8a1 四次一致）。
  己那条合成件第一版踩到自己一脚：调用点写在同一个文件 ⇒ 被 lits 兜底先洗成覆盖 —— **同文件里任何字符串都能替一个看不见的形参说话**，
  这条已知松弛方向是「少报红」，已写进证据文件第 3 节。三棵树（`00bb3c4` 快照 / `ec47033` 快照 / 14:09 脏工作树）读数逐字相同：
  词表 64 / 非测试 .go 75 / 位点 27 / K=35 / 派生 29 / 红 26。**新裁项 = REQ-T35（第四层「形参 ← 同包调用点实参」算不算覆盖）**，
  我不自裁：算进去会少 2 条红（假绿方向），不算就永久具名失明。
  **补（14:21，巡检行号自纠）**：巡检把上一封「锁定 `:224`」那句作废（他自己 sed 区间回映射时错一行）。我不采信任何一方转述，当场自己复量：
  `grep -n -e 'Type:' -e 'EventID:'` ⇒ 222=EventID、**223=Type**；`sed -n '218,226p' | grep -n ''` ⇒ 区间第 6 行=Type；
  该文件在工作树 / `00bb3c4` / `ec47033` 三处 **cmp 逐字节相同** ⇒ 判据点名的 `:223` 与更正后一致，**代码与判据都不用改**；
  证据文件第 4 节里「与盘上差一行」那段原话不删，只在第 7 节标明已由自纠收口 —— 今后谁再引用 `:224` 谁错。
  **Stage 追加（15:08，T-371 五件：①②③④ 已落、⑤ 的门前一刻又关了）**：
  ① 第四层「形参 ← 同包调用点实参」**算覆盖**，且按裁**只吃字面量/包级常量**（`resolveConstOrLit`；函数产出一律挡）；
     定不到的另立**「未决」桶** —— 与「无生产者」分开打印、每处 `t.Errorf` 点名 file:line，名单只登记"确认无生产者"那一桶。
     顺带按同一分类学把 **lits 兜底降级**：以前同文件一个字符串就能替一个看不见的发射位作证（假绿方向），现在它只报"未决所及"。
     我默认那条（只诊断不进覆盖）被否，理由我接受：**把真有生产者的名字写进"无生产者"名单是写假事实**，比多一道诊断严重。
  ② (丙)+(甲) 收到：判据行文本 + 名单补 24 条的可粘句子都写进 **REQ-T36**，我一行 `specs/**` 都没碰；(乙) 不采 ⇒ 我没去派 backend。
  ③ 判据行现在就交 = REQ-T36 的第一段（U13-8，五列、定位符只用例名、命令是真跑过的锚定形）。
     **状态列我写的是「现存·现红 24 条」而不是您原话的 26**：① 落地后 `process_crashed`/`process_exited` 由第四层认到，
     26 是旧口径的数 —— 两个口径我在同一封里都给，避免第二次"44 vs 26"（巡检 14:5x 就是这条约的）。
  ④ Ⅱ-b 我这一轮就用上了：15:03:38 现读 `core/` 有 backend 四件在途（`executions.go`/`handler.go` ` M` + `approvals.go`/
     `declared_not_wired_test.go` `??`）⇒ 您 ⑤ 与巡检 14:51:58 那句"干净"在当时成立、起跑前已不成立 ⇒
     **关闭证据走 `bc03526` 的 HEAD 快照**，工作树那一读只按 Ⅰ 附脏度用。两棵树红名单 `diff` 逐字相同。
  六条变异各自 FIRED、文案互不相同（关第四层 / 第四层改吃宽 / 派生不摘未决 / 未决渗进覆盖 / 兜底升回覆盖 / 摘交集），
  合成件四条反例（戊词表外字面量、己第四层必须算、庚实参是函数产出必须拒、辛只能靠同文件字面量必须落未决）。
  整包 `core/test`：顶层 **45 PASS / 1 FAIL / 12 SKIP（58 条）**，唯一 FAIL 就是这条判据（红因=24 个未登记名字）。
  证据 `roster-criterion-0927-1506-t371-layer4-bc03526.txt`。
  **补（15:12，巡检 15:11 的时效更正到我这里时已经跑完了）**：它提醒"别在工作树上取一个看起来全绿的数"——我 15:03:38 就是这么绕的；
  趁这封又起了一棵**新的 HEAD 快照 `93af696`（15:07:16，导出前后 rev-parse 一致）**复跑 ⇒ 三棵树
  （`bc03526` 快照 / `93af696` 快照 / 15:04 脏工作树）**红名单逐字相同、桶一 37 / 桶二 27 / 现红 24 也相同**
  ⇒ 主控要粘的状态列不只在某一棵旧树上成立。Ⅱ 的门到 15:12 现读**仍是 5 件**（backend 那四件未入册）。
  主控 15:04:09 已把**契约① 第 24 条第 ⑨ 款「状态句的时效」**落文，其中点名我这一格（"test 已按 Ⅱ-b 绕开"）⇒
  **划掉一句派发里的假引用（15:13:43 现读 date，巡检自纠第 3 次同族）**：T-371 那封末尾的"你 14:21 那份里写的 `LUCERO_REQ_DB2` 类锁不在你手上"
  —— 巡检确认那句是他编的，我 14:21 那份没有这句话。盘上自证（两条都可复跑）：
  `grep -c 'LUCERO_REQ_DB2' .qoder/progress/test.md` = **0**、`grep -rc LUCERO_REQ_DB2 .qoder/evidence/test/` 全部 = 0
  ⇒ **它从没进过我的台账与证据，所以没有要改的动作**；这句话从派发原文里作废，我不拿它当约束。
  它真正的归属（同样盘上读来，不是我推测）：`LUCERO_REQ_DB2` 是 `core/cmd/core/lifecyclecolumns_test.go` 里
  `if os.Getenv("LUCERO_REQ_DB2") != "1"` 那个 **REQ-DB-2 升格格开关**，讨论分布在 orchestration(28)/backend(18)/adapter(15)/db(7) 四个台账里，
  **与我 `core/test/` 那包无关**。⇒ 判据：别人引用"我账上的东西"时，我先 `grep` 我自己的落点文件并报命中数，命中 0 就当场说"盘上没这句"。
  往后我这类读数一律写成"截至 HH:MM:SS 的 `93af696` 这一棵树 + 命令原文"，不写不带树与时刻的状态句。
  **自记（15:12:33）**：我 15:11 那次给头部行做"切片替换"把 `LAST_UPDATED` 刷成了 `LAST_UPDATED::12:23+080000`（一行废字符串），
  15:12:33 用当轮 `date` 整行重写修好。教训：**头部那种带格式的行只能整行替换，不许拼偏移切片** —— 台账最多人读的一行坏了我自己没第一眼发现。

  **Stage 追加（16:00，T-379 门禁 + 干净树全仓绿；先修门禁后取数，顺序照 ④）**：
  ① 洞找到了，是**四条**没接 `strip_comments`：`check_1` 的 approvals SQL、`check_2` 末尾的 `.Exec(` 旁门、`check_6` 的三条 import 方向、`varpass_sites`。
     修法不是各条腿补一次 strip，而是加一个 `strip_then_match <正则>`：**剥完还要按同一正则重匹配** —— 只剥的话
     `const q = "x" // UPDATE approvals …` 这种行被切掉注释后仍带着 `file:line:` 前缀留在结果流里，会以「一行没 SQL 的代码」被判越界。
     修前证据（不是推断）：step1 只加反例不修复 ⇒ `--selftest` 恰好 1 条 FAIL（注释里的 `UPDATE approvals` 被判越界），
     而代码里真写 SQL 那条腿同时保持通过 ⇒ 反例两侧都有牙。
  ② 新增 F「响应消息/文案位」两条形状（F1 表名被反引号包在另一个双引号串内部；F2 整字面量落在 `write*()` 的第 4 位参数）：
     单独一类 `msg`、**连条数带 `tbl@file` 位置一起打印**（排除不打印=悄悄放宽，这条是我自己立的）。窄性由两条反向断言钉住：
     定义位仍 literal、写口首参仍 write —— M4 把 F 放宽成万能后门时这两条腿立刻红。
  ③ **没有删改任何人的解释性注释或 501 文案**（T-379③）。`core/**` 全程只读，改动只落在 `scripts/check_ownership.sh`（sha `877ada453cc8`）。
  四变异 + control：M1/M2/M3/M4 各自 FIRED、文案互不相同；control 未变异 SILENT/0 FAIL ⇒ 判决有效。
     这里先栽了一次测量工具的坑：`subprocess.run(["bash", …])` 在 Windows 命中 `System32\bash.exe`（WSL）报 HCS 错，
     四条判决全成假的「SILENT」；换绝对路径 + 加 control 腿重跑才算数。修前/修后同一棵树两跑 `diff` 只差首行时间戳 ⇒ 没放走真命中。
  ④ **干净树全仓绿交了，但先把集合说清**：Ⅱ 门 15:54:25 现读是 10 行（backend 九件在途）⇒ 走 Ⅱ-b 全量导出 `46dafae`；
     **第一次两遍跑出 18 包全 ok 我差点就这么交** —— 那是 `git archive` 的坑：它**不含未跟踪文件**，我自己那份判据根本不在快照里。
     把工作树那份逐字节相同（md5 双 `6eddcbb0`、45334 字节）放进快照重跑 ⇒ 四张表：A 573/0/19、B 581/0/11（不含我判据，rc 0）；
     C 574/**1**/19、D 582/**1**/11（含我判据，rc 1），唯一 FAIL 恒为 `TestEventProducerRosterMatchesDerivedSet`，四张表 DATA RACE 0 / panic 0。
     ⇒ 交付口径只能是「除我这条设计上有红的判据之外，干净树上全仓零红」；「全绿」这个签名我不签。C/D 的红仍是 **24 条**，
     与 `bc03526`/`93af696`/脏工作树三读逐字相同 ⇒ **四棵树同数**。名单一落，C/D 就变 18 ok。
  ⑤ REQ-T36 现状核对（盘上，不是我推测）：`U13-8` 在 `specs/contracts-internal-v0.1.md` 命中 **0**、事件契约那句「本名单当前是」**仍是三条**
     ⇒ 编号已被主控登记（T-378①），**正文两段还没落**。
  **自记（16:05:02，我这一轮自己造的第二个错，比第一个更严重）**：16:00 那次改台账，我用「按索引切片」重写
  `- Last verified action:` 到 `- Next action:` 之间那一段，结果把**上一轮记在同一区里的 15 行**一起吃了 ——
  15:11 巡检时效更正的补记、15:13:43 划掉派发假引用的盘上自证、15:12:33 头部行自纠，全没了。
  发现方式不是眼看，是**同一轮开头那份带轮次名的备份**（`.scratch/test/test.md.bak_t379`，sha `01dc360376aa`）与当前文件做 difflib 逐段对照；
  已按备份原文逐行放回（脚本 `.scratch/test/t379_restore.py`），本轮 Stage 块也一并补回（插回 37 行，放回后 sha `a85914dec1eb`）。
  两条规矩：① **台账只许锚点整行替换或锚点插入，禁止按索引切片重写** —— 切片会把「我以为只是这一条、其实是一整段」全带走；
  ② 改账前 `cp` 一份带轮次名的备份，改完立刻与备份做集合差分。这次靠它才发现；只读 diff 我会漏。
  **Stage 追加（16:43，T-385 三件）**：
  ① 盘上核对：`grep -n 'U13-8' specs/contracts-internal-v0.1.md` ⇒ **204** 行五列齐；名单那句在
    `specs/events/event-contract-v0.1.md` 有**两处**（`:283` 旧三条、`:300` 新 27 条），我的解析取最后一句 = :300 ✓。
    两段都在 `f1558cc`（16:28:03），`git show --stat` 只动 specs 与主控台账 ⇒ **两棵树代码逐字相同**。
  ② 主控要的那次对照我自己复现，不采信转述：登记后快照 `f1558cc` ⇒ 桶二 27 = 名单 27 ⇒ **PASS/rc 0**（16:35、16:40 两跑同果）；
    登记前快照 `a432b96` ⇒ 桶二 27 vs 名单 3 ⇒ **FAIL、红 24**（两跑同果）。⇒ "判据 stuck-green" 这一格是关着的。
  ③ 锚句三件事已进实现：多句命中**把每处行号打进读数**（今天 `行号 283 300`）、**自称条数与实取名字数必须相等**（阿拉伯与中文都认，
    「二十七」这类组合也认）、锚不到照旧 `errAnchor` 停。新增 `TestProducerRosterParserLegs` 四条腿 + 四条变异。
    **N4 第一版 SILENT 是我自己造的**：我把"认不出条数"做成"只报不拦"，于是把 cnNumber 摘掉测试照样绿 ⇒ 那道自检是摆设；
    当场把腿四改成"自称三条却列四个必须停"+ 直接断言条数解析，N4 才 FIRED。教训入册：**排除/降级分支必须自带一条会被它放走的断言**。
  ④ 名册转绿的交付口径分两份写：可指到干净树的绿 = 快照 `f1558cc`（76 个非测试 .go）三条腿全 PASS；
    旁证（不作关闭）= 16:40:56 脏工作树整包 **47 PASS / 0 FAIL / 12 SKIP（顶层 59，+1 是新增那条 parser 腿）**。
  ⑤ 残留那一格我不自己收：多句同锚今天靠"取最后 + 自检 + 打行号"够；但有人在最后之后补一句**自洽的**同锚句子时，
    只有"命中数必须为 1"拦得住 ⇒ 那是契约形状的裁定，提成 **REQ-T37** 交主控（要动 `specs/**`，我不加闸也不删那句）。
- Status:
  **补（16:44:27，落文后归我那道门自己数的那件事兑现了）**：`bash scripts/check_ownership.sh` ⇒ 9b 那条从
  「49 条 ID / 去重 49」变成 **「七族 × 行首表格行 共 50 条 ID、去重后仍 50 ⇒ 无重号」**（新 ID 就是 `U13-8`，被同一条腿数到），
  lint exit 0、VIOLATION 0 条、`--selftest` exit 0 且 0 条 SELFTEST FAIL。
  主控登记的 U13-8 状态列已写明登记后归 0（不是我回填的），我这边独立复现同果；两次用时不同（它 2.00s、我 0.05s）只是构建缓存冷热之差，不改判决。
  **Stage 追加（09-30 15:39，T-394）**：① 入册我自己核：`git show --stat 25f5bd3` 里那两个文件都在（+1351 / +173），
    `git ls-files` 也列出来了 ⇒ 不是「听说入册了」。
  ② REQ-T37 裁「甲」已落：`parseProducerRoster` 命中 > 1 就 errAnchor 停（错误里带每处行号），另加「该放行必须放行」那一腿；
    现读盘上锚数 = **1**、自检 **26 = 26**（正是主控要的那个数）。新用例 `TestProducerRosterParserLegs` 四条腿；
    变异 N1/N2/N3 FIRED、**N4 第一版 SILENT**（「认不出条数就只报不拦」让自检成了摆设）⇒ 改硬后 FIRED。
  ③ 方向那件做完了，但**数与主控的预测不一样**：第五层（闭包 `return` 第一位的常量/字面量）一开，从名单里出来的是 **5 条不是 2 条** ——
    `task_queued`/`task_preparing`/`task_paused`/`task_resumed`/`task_accepted` 全走 `internal/task/service.go` 的
    `advance(..., eventOf func(Task) (string, any))`（:173 签名、:190 取值、:197 发射位、六个调用点各传一个闭包）
    ⇒ **名单该 26→21**（可粘句子在证据第 5 节 A）。顺带挖掉一个静默坑：站点循环原本对每个文件再 parse 一次，
    而 `go/token` 每次 AddFile 给新基址 ⇒ 跨 AST 比位置永远不命中（`paramRef` 恒 false —— 这就是第五层先前空转的唯一原因；
    变异 Q3 把它改回 re-parse 就 FIRED，证明这一条是承重的，不是顺手改的）。
  ④ 现状读数（HEAD 快照 `606dd4f`，15:34:19 起跑）：桶一 38→**43**、桶二 26→**21**、名单 26（未动）、未决 0 ⇒
    **用例 FAIL：5 条 missing**。这条红是真的（契约里那 5 条登记与代码矛盾），不是假红，也不是产品缺陷（面板链绿）⇒
    **现状更正（09-30 16:2x，T-400②）**：这条红**已闭** —— 主控 16:04:37 以 `0dceb09` 把名单摘成 21 条 ⇒ 16:27:20 复跑 `--- PASS (0.04s)`、桶二 21 = 名单 21、未决 0。原文留档不删。
    **我不签全绿、不自己消音、没改别人一行码、没碰 `specs/**`**。15:34 现读 `git status --porcelain core/` = 14 件在途 ⇒
    整包那一跑（顶层 43/1/15）只按 Ⅰ 用，与 14:47 干净树那次（53/0/6）不互引；SKIP 从 6 涨到 15 是别人的在途码让 `cmd/core` 起不来。
  ⑤ 三选一记成 **REQ-T38**（甲 落 21 条 / 乙 退回无第五层那版 / 丙 只认链尾那条）—— 我的默认是甲。
    两份在制品 sha 都在盘上可切换：有第五层 `da8d0b14e73b`（59529 字节）/ 无第五层 `46f37c6aec04`；
    证据 `.qoder/evidence/test/roster-layer5-t394-0930-1536-606dd4f.txt`（八条变异：N1–N4、Q1–Q4，其中 N4/Q4 第一版都 SILENT、补断言后才 FIRED）。
- Status: in_progress（09-28 19:2x 收口轮：**我自己造的那条 approvals 越界已修、ownership 正跑转绿 `OWN_EXIT=0`/VIOLATION 0**；
  全仓 18 包 ok / FAIL 0、定向 9 PASS / 5 子 SKIP / 0 FAIL、`TestCrashApprovalNotLost` 仍 PASS；仍悬的只有 REQ-T37 那一格契约形状
  与两件等入册 —— 我不改名单、不降判据、不动别人的码）
  **补（09-28 15:48，T-276 W2-7）**：`core/test/crash_test.go` 六条 Skip 已全部改写 —— **顶层 0 SKIP、
  11 条顶层用例全真跑**，跑不了的 5 格拆成子用例 SKIP 并点名到 `file:line`（REQ-RC-1 ×2、Supervisor.Run 接线 ×1、
  approval 接线 ×2）；第 6 条按 T-66④ 对 `core/api/http/tasks.go` 做了一发变异 **FIRED**、字节级还原
  （md5 `21C7105E8F5A19B7F826299C38D3AF28` 前后一致、`MUTATION` 标记 0 处残留）。**仍未动别人的码**：
  `core/api/http/tasks.go` 只是临时变异，已还原成与备份逐字节相同；`specs/**`、`core/internal/**`、`core/migrations/**` 全程只读。
- Last verified action: 09-27 16:3x~16:43（2026-09-27T16:43:13+0800 本机 date）：T-385 三件落在 `core/test/event_producer_roster_test.go`
  （未跟踪，sha 前 12 `5232192f2dfe`；`gofmt -l` 无差异、`go vet ./test/` exit 0；文件头的"回解走三层"已改成四层并注明兜底不再产生覆盖）。
  两面复现（同一份判据、两棵 HEAD 快照，导出前后 `git rev-parse` 一致、判据文件与盘上 `cmp` 相同）：
  登记后 `f1558cc` ⇒ 锚句 2 处命中取 :300、自检 自称 27 = 实取 27、桶一 37（仅第四层 2）/ 桶二 27 / 名单 27 / 未决 0 ⇒
  **`--- PASS`、rc=0**（16:35:05 与 16:40:42 同果）；登记前 `a432b96` ⇒ 名单 3 ⇒ **FAIL、红 24**（两跑同果）
  ⇒ 换回旧契约就红，判据不是 stuck-green。合成腿三条 `TestEventProducerScannerLegs` / `TestProducerRosterParserLegs` 全 PASS；
  变异 N1/N2/N3/N4 **各自 FIRED**（N4 第一版 SILENT，改硬腿四之后才咬住），control 未变异 PASS，四次还原 sha 一致。
  旁证（不作关闭，16:40:56 脏工作树、非测试 .go 78）：`core/test` 整包 47 PASS / 0 FAIL / 12 SKIP = 顶层 59。
  证据 `.qoder/evidence/test/roster-green-t385-0927-1641-f1558cc.txt`。
- Last verified action（09-28 19:09~19:19，2026-09-28T19:20:15+0800 本机 date，T-276 收口轮）：
  落点 `core/test/crash_test.go`（1571 行，sha1 前 12 `545493572759`）。
  ① **越界自查**：`rg -n "FROM approvals|UPDATE approvals|INSERT INTO approvals" core --include=*.go`
  ⇒ **15 命中全在 `core/internal/approval/`**，`core/test/crash_test.go` 0 命中（证据 `own_grep_after.txt`）；
  ② **ownership**：selftest 19:10:02→19:10:22 `SELFTEST_EXIT=0`；正跑 19:10:31→19:11:45
  `OWN_EXIT=0`、结论行「本次扫描 0 项越界」（`own_selftest.txt` / `own_real.txt`）；
  ③ **全仓**：`go test ./... -count=1` 19:16:35→19:16:57 ⇒ **18 行 `ok`、FAIL 0 行、exit 0**（`go_test_all.txt`）；
  ④ **定向** `-run 'TestCrash|TestIdempotent' -count=1 -v` 19:19:00→19:19:13 ⇒ **PASS 9 / SKIP 5 / FAIL 0、exit 0**
  （`crash_v_full.txt`；`TestCrashApprovalNotLost` PASS，5 SKIP 全是子用例，与上一格同集合）；
  ⑤ `gofmt -l test` 空、`go vet ./...` exit 0（19:15:54→19:15:55）。
  **代跑两腿（别人的腿，原文交主控）**：t47-smoke 19:11:55→19:12:08 与复跑 19:12:58→19:13:08；
  build_stamp 19:15:17→19:15:39 `STAMP_EXIT=0`。
- Last verified action（09-28 15:4x~15:48，2026-09-28T15:48:10+0800 本机 date，T-276 W2-7）：
  落点 `core/test/crash_test.go`（1442 行，sha1 前 12 `7F67B8DCAF90`，`gofmt -l` 无输出、`go vet ./...` exit 0）。
  读数原文（证据三份，均 09-28 现取）：
  ① **全仓**：`go test ./... -count=1` ⇒ **18 个 `ok`、FAIL 0 行**（`core/test 13.945s`）；
  ② **整包** `go test ./test/ -count=1 -v` ⇒ **PASS 70 行 / FAIL 0 / SKIP 13**（顶层 SKIP 6 条**全在别的文件**：
  `TestFakeAdapterSubprocess`、`TestCancelReplyIsResultFrame`、`TestFakeAdapterDeliveryModesOnTheWire`、
  `TestFakeAdapterModelSelectLegs`、`TestProjectionRejectedJournalAndAudit`、`TestTurnRefCrossCheck`；
  子用例 SKIP 7 = 本轮 crash_test 的 5 + `TestDroppedFrameReachesAuditSeam` 的 2）；
  ③ **定向** `-run 'TestCrash|TestIdempotent' -count=1 -v` ⇒ **PASS 9 / SKIP 5 / FAIL 0**，
  5 条 SKIP 全是 crash_test 的子用例。
  **SKIP 前后计数**：crash_test.go 改前 6 条顶层 Skip（文件里 `t.Skip` 6 处）⇒ 改后 **顶层 0 条、
  子用例 5 条**（现文件 `^\s*t.Skip(` 共 5 处，全在 `t.Run` 里）；全包 SKIP 改前 12（上一轮账本口径）⇒
  现 13 —— 增减**不在本轮**：本轮净减 6 条顶层、净增 5 条子用例，别的文件那 8 条我一条没碰。
  证据：`.qoder/evidence/test/crash-skip-t276-0928-repo18pkg.txt` / `…-directed.txt` / `…-pkg.txt`。
- Next action:
  **（10-01 16:0x 唯一动作）**：等主控入册我名下这一件新件 —— `core/test/send_wedge_write_channel_test.go`（`a43bb892c198`，REQ-S-1 甲）。入册后我这侧的待办只剩：**adapter 的 T-436**（收口层「目标早不在」过滤）与 **backend 的 T-430 四格**（我那四枚红等它们自己关）；REQ-S-1 之外无等主控项。另提请主控一处：甲 的「装配为测试复刻」那句免责可以**从判据行里去掉**——落盘版跑的是生产装配，比 09-27 那版强，别让我写的行文本反而把覆盖面写窄了。
  **补（09-28 19:2x，T-276 收口轮，两问交主控）**：
  ① **t47-smoke 那条 FAIL 不是我造的，请按别人的腿入册**：22 断言 **21 OK / 1 FAIL**，红在
  `RESULT_SEND_TWO_PHASE`（期望 `RUNNING_v3`、实测 `st=SUCCEEDED v=4`），**两次跑同一值、不是抖动**。
  归因三条：脚本 `.scratch/backend/t47-smoke.sh` mtime 仍是 **09-27 15:09:40、本轮一字未改**；
  我本轮唯一的码改动是 `_test.go`（不进 `core.exe`/`fake-adapter.exe`，脚本第 14/17 行自己 build 的是 `./cmd/core`、
  `./cmd/fake-adapter`）；而工作树上挂着 backend 未提交的 send/finish WIP
  （`git status`：`core/cmd/core/sendreap.go`、`core/internal/execution/finish.go` 未跟踪 +
  `core/internal/execution/{send,service,repository,domain}.go`、`core/cmd/fake-adapter/main.go` 等 9 文件修改 334 行）
  ⇒ 执行到 `SUCCEEDED v=4` 正是这条在途码的行为，历史读数（backend.md:5386）那批是 `RUNNING_v3`。
  **我不倒着编旧数、不改脚本、也不替 backend 判这条算不算绿** —— 交主控裁。
  ② **等入册两件**（仍是我的在途件，别的角色别误判成越界）：`core/test/event_producer_roster_test.go`、
  `scripts/check_ownership.sh`；REQ-T37 那一格契约形状仍等主控裁（解锁人=主控，我不自己加闸）。
  **（下面 ①~③ 是 09-28 15:48 的 T-276 三问、其后 ①~⑥ 是 09-27 留下的；两处都还有效时按"上面的两问"先办）**
  **三问（09-28 15:48）**：
  ① **要不要把 5 条子用例 Skip 的"点名"升成主控派工**？—— 三处前置我已逐条落到 `file:line`，
  分别是 **REQ-RC-1 Recovery Engine 未交付**（`cmd/core/main.go:3~7`、`internal/execution/repository.go:69`
  生产码零调用、`specs/contracts-internal-v0.1.md:700~703/:662`，归 **backend/recovery**）、
  **`process.Supervisor.Run` 没有生产调用点**（唯一 emit `EventCrashed` 的地方是 `internal/process/supervisor.go:405`，
  而 `cmd/core/processpipe.go:162/:211` 只调 `Start`/`Stop`，归 **backend/process**）、
  **approvals 三条 501 + `approval.NewService`/`SweepExpired` 无生产调用点**
  （`core/api/http/approvals.go:30/:48/:56`、`cmd/core/main.go:174` 只有 `NewSQLiteStore`，归 **T-372 + backend**）。
  ② **T-66④ 的变异判据要不要扩到第 2、3 条端点**？—— 本轮只在 `createTask` 打了一发（FIRED：首次
  body 含明文、无打码标记、首次≠重放、wire≠存储）；`sendExecution` 与 approvals 的幂等回读面还没打过。
  ③ **这版用例能不能被下一轮逐字复现**？—— 命令面：`$env:LUCERO_GO="D:\DevTools\go\bin\go.exe"` 之后
  `go test ./test/ -run 'TestCrash|TestIdempotent' -count=1 -v`（9 PASS / 5 SKIP）与
  `go test ./... -count=1`（18 包 / FAIL 0）。**照旧**：这 5 格今天只拆不删，
  前置一到就按各用例注释里写好的口径把子 Skip 换成硬断言（不是整条转 PASS）。
  **（下面 ①~⑥ 是上一轮 09-27 留下的）**
  ①**等主控裁 REQ-T37（名单锚句要不要收"命中数必须为 1"那一格）**：今天两处同锚、取最后一句 + 自检都成立，
  残留只在"有人补一句自洽的同锚句子在 :300 之后"。要收就得改 `specs/**`（把 :283 那句改写或删），**解锁人=主控，我不自己加闸**。
  ②**等入册两件**：`core/test/event_producer_roster_test.go`（未跟踪，`5232192f2dfe`）+ `scripts/check_ownership.sh`（`877ada453cc8`）。
  ②**等入册两件**（现在都是我的在途件，别的角色别误判成越界）：`core/test/event_producer_roster_test.go`
  （未跟踪新文件，`1409c588bd0f` / 45334 字节）+ `scripts/check_ownership.sh`（9b 门之后又叠了 T-379 的接线与 F 形状，` M`，
  sha `877ada453cc8`）。落文归主控，`U13-8` 这个新 ID 落文后正好归我那道 9b 门自己数。
  ③**REQ-T34 / REQ-T35 已裁、不再悬着**：T-371① 裁第四层算覆盖（已落地）、② 裁 (丙)+(甲)（文本已交）、
  ④ 把 Ⅱ-b 补进款（我这轮就是这么跑的）。裁下来的动作里**没有任何一条要别人改码喂我的扫描**，T-365④ 那条禁令继续有效。
  按 T-299② 我带这句：**这版用例与这版脚本都能被下一轮逐字复现** —— 命令面没变，判据的现红也会如实复现
  （新口径 24 条；同一棵树换回三层回解的旧口径是 26 条，两个数都印在桶二那一行里，别再拿一个数去对另一个数）。
  ④**REQ-S-1（T-312 那条 handler 层判据）现在前置门已开**（backend 已入册 `9f07a80`）：按裁乙我落判据，
  但排在 ①② 之后 —— 同一时间我只盯一个红，且这条腿要起真子进程、得另挑无并发窗口（⑧款之外的排程纪律）。
  ⑤**T84-7b 仍三段前提两真一假**（`CallTurn(` 生产调用点 0）⇒ 继续 SKIP；解锁人 = 把消息链接到 `CallTurn` 的那一位。
  ⑥**合验时**按 T-254③ 复核契约① 第 17 条三腿形状，并把我自己五条三腿判据（C8-12/C8-13 + 9b + 合成腿 + U13-8 这条）
  按「两次输出两两不同」对一遍；U13-6/REQ-T7 落地后给 F20 三条加真子进程被测面；REQ-T13 那格仍 `[SCOPE 之外]`。
- Verification: `.qoder/evidence/test/` —— **本轮全部重取过（mtime 实测，不是"应该有"）**：
  `go-test-final.txt`（面 A，连 SKIP **名单**一起归档）与 `go-test-fakeadapter-final.txt`（面 B）= 09-25 19:17；
  `ownership-lint-final.txt`、`ownership-selftest-final.txt`、`selftest-final.txt`（`flaky_watch --selftest` 17 条）、
  `crash-final.txt` = 09-25 19:20；量级腿两份 = `flaky10-0925-1908-122single-0red-bothgates-newlock.txt`（两道门都在，交账用这份）
  与 `flaky10-0925-1848-124pooled-0red-newlock-gate2-dirty.txt`（并池旁证，头部自陈两道门不满足）。
  数值见 Verification evidence 首两条；旧快照不在此处保留，历史实测与在途取证都逐轮记在 Completed / Verification evidence 里。
  例：09-20 15:2x 曾出现 SKIP 10→12，当场取证为 `go build ./cmd/core` 报
  `c.Tasks.Pause undefined`、`c.Tasks.Resume undefined`（backend 在途），
  两条真跑用例据此按设计跳过并打印同一原因——没有拿桩绿冒充。

## Completed

- **09-28 19:0x~19:2x 轮（T-276 收口：自造自认一条 approvals 归属越界并修掉 + 三腿）**：
  ① **红从哪来**：上一格 W2-7 我在 `core/test/crash_test.go:1409`（`TestCrashApprovalNotLost` 重启后读行那段）
  留了裸 SQL `SELECT state, execution_id, expires_at FROM approvals WHERE id = ?` —— 违反契约③
  `specs/contracts-internal-v0.1.md` **§3.2.1 第 1 条**（approvals 表 SQL 只准在 `internal/approval` 内）
  与 **§4 C1**（跨模块读默认不红是设计，approvals 是**唯一写死的例外**）；
  19:03:18→19:04:50 正跑 `OWN_EXIT=1` / VIOLATION 1 指名此行 ⇒ **是我自己造的，记我账上**。
  ② **改法**：换用包内现成读面 —— `core/internal/approval/store_sqlite.go:439 SQLiteStore.Get`（`:58 NewSQLiteStore`、
  `:441~443` 把 `sql.ErrNoRows` 包成 `approval.ErrNotFound`，见 `approval.go:31`）⇒
  `crash_test.go:1408~1418` 改为 `approval.NewSQLiteStore(d, now).Get(context.Background(), approvalID)`，
  取 `a.State / a.ExecutionID / a.ExpiresAt`；**断言三判据一字未动**（PENDING、`execution_id` 未换、
  `expires_at != 0`，1419~1428 未触碰）；「行不见了」分支由 `sql.ErrNoRows` 换成
  `errors.Is(err, approval.ErrNotFound)`，文案仍是「§7：Approval 不丢」。净 +1 行（1570→1571）。
  ③ **自查 grep（契约口径）**：`rg -n "FROM approvals|UPDATE approvals|INSERT INTO approvals" core -g "*.go"`
  ⇒ **15 命中全在 `core/internal/approval/`**（store_sqlite.go 4 / store_sqlite_test.go 5 /
  blocking_sweep_test.go 5 / vertical_slice_test.go 1），`core/test/crash_test.go` **0 命中**；
  留档 `.qoder/evidence/test/own_grep_after.txt`。
  ④ **三腿**：腿 1 ownership `--selftest` 19:10:02→19:10:22 `SELFTEST_EXIT=0`（`own_selftest.txt`）、
  正跑 19:10:31→19:11:45 `OWN_EXIT=0` / **VIOLATION 0**、baseline=900038d HEAD=cde8d79、
  扫描面 190 个 .go（`own_real.txt`）；腿 2 t47-smoke **代跑**（别人的腿，只跑不改，脚本 mtime 仍
  09-27 15:09:40）两遍 19:11:55→19:12:08 / 19:12:58→19:13:08，**23 行断言 = 22 OK / 1 FAIL**、
  `RESULT=DONE`、残留进程 0（`tasklist | findstr` MATCH_EXIT=1）；腿 3 build_stamp **代跑**
  19:15:17→19:15:39 `STAMP_EXIT=0`，`构建时刻=2026-09-28T19:15:17+08:00 HEAD=cde8d79
  工作树未提交=42 文件/编译输入=2367 项/指纹=1a97a7f8d455202f7bc0d89afefa60f6 [./cmd/core ./test]`
  （`t47_smoke.txt` / `t47_smoke_run2.txt` / `build_stamp.txt`）。
  ⑤ **smoke 那条 FAIL 我不认领也不背书**：`RESULT_SEND_TWO_PHASE` 期望 `st==RUNNING ver==3`、
  实得 `st=SUCCEEDED v=4`，两跑同值；我本轮唯一码改是 `_test.go`（不进 core.exe/fake-adapter.exe），
  脚本一字未动；工作树上挂着 backend 未提交的 send/finish WIP（`core/cmd/core/sendreap.go`、
  `core/internal/execution/finish.go` 等 9 文件 +334 行）⇒ 归因与裁断交主控/backend（见 Next action ①）。
  ⑥ **验证读数**：`gofmt -l test` 空（GOFMT_EXIT=0）、`go vet ./...` exit 0（19:15:54→19:15:55 无输出）、
  全仓 `go test ./... -count=1` 19:16:35→19:16:57 **18 ok / FAIL 0**、定向
  `-run 'TestCrash|TestIdempotent' -v -count=1` 19:19:00→19:19:13 **PASS 9 / SKIP 5 / FAIL 0**
  且 `TestCrashApprovalNotLost` PASS（纯 `-run TestCrash` 另跑一次 = 8 PASS，`TestIdempotent…`
  不匹配该正则；两份留档 `crash_v_full.txt` / `crash_v.txt`）。

- **09-28 15:0x~15:48 轮（T-276 W2-7：crash_test 六条 Skip 逐条重估与改写）**：
  判决表（六条，一条一格；"改前"= `core/test/crash_test.go` 里那条 `t.Skip` 的原文口径）：

  | # | 用例 | 改前 | 判决 | 今天真跑的断言 / 仍缺前置 |
  |---|---|---|---|---|
  | ① | `TestCrashRestartIdentifiesInterrupted` | 整条 Skip（"Recovery 未交付"） | **拆** | 顶层真跑：崩溃前 seed 的行**还在**、不落 `FAILED/CANCELLED/SUCCEEDED`、**状态若动则该 aggregate 的事件数也同增**（改口判据）、崩溃前 `seq` 一条不丢。子 Skip ×1：把 interrupted 认出来并改判 —— REQ-RC-1 |
  | ② | `TestCrashAdapterKillDoesNotTakeCoreDown` | 整条 Skip（"Adapter 进程隔离没验"） | **拆** | 顶层真跑：`-adapter hang=<本测试二进制>` + TestMain 注入 pid 文件 ⇒ `kill -9` 后 Core **仍 READY、端口不变**、probe 实测 **503 `adapter_unresponsive`**、同源 `other` Execution 仍 `RUNNING`、`process_instances WHERE pid=?` 恰 1 行。子 Skip ×2：`process_crashed`+FAILED（Supervisor.Run 无生产调用点）、in-flight → `UNKNOWN`（REQ-RC-1） |
  | ③ | `TestCrashNoDuplicateMessageAfterRestart` | 整条 Skip（"幂等没验，端点也没定"） | **拆（端点改口）** | 顶层真跑：端点按现量协议改 `POST /api/v1/executions/{id}/send` + `delivery:"queue"` + 同一 Idempotency-Key ⇒ 重启后**逐字节相等**、`messages` 与非 `core_ready` 事件数不变、`idempotency_keys.state=COMPLETED` 且 `response_json==first`、**再发一发仍相等** |
  | ④ | `TestCrashApprovalNotLost` | 整条 Skip（"approvals 没接线"） | **拆** | 顶层真跑：真 `approval.NewService(approval.NewSQLiteStore(d, now), now).Request(...)` 落 `PENDING` ⇒ 重启后行在、仍 `PENDING`、`execution_id` 不变、`expires_at != 0`。子 Skip ×2：`SweepExpired` 无生产调用点、approvals 三条 501 |
  | ⑤ | `TestCrashEventJournalComplete` | 整条 Skip（"WS 面够不着"） | **拆（能跑的那半真跑）** | 顶层真跑：崩溃前 `POST /api/v1/tasks` ×3 ⇒ DB 侧 `missingSeqs()` 为空；WS `from_seq=0` 收 hello echo 后 `got == want` 连号，再收一帧断言走到**重启后**的 `core_ready`（本轮实测崩溃前 4 条、`seq=[1 2 3 4]`）。**没有 WS 客户端的缺口自己补**（文件内自带最小 RFC6455 客户端，因为 `cmd/core` 是 `package main` 不可 import） |
  | ⑥ | `TestIdempotentReplayBodyIsByteIdentical` | 整条 Skip（"要真 Core 才能验"） | **重写文案 + 真跑 + 变异** | 顶层真跑：真 Core 崩溃前后跨段重放 ⇒ **逐字节相等**、`stored==first==replay`、**无明文含打码标记**、tasks/事件数不变、`tasks.title` 无明文。**T-66④ 变异一发 = FIRED**（见下） |

  **第 6 条变异判决（T-66④）**：变异前备份 `core/api/http/tasks.go` ⇒ md5 `21C7105E8F5A19B7F826299C38D3AF28`；
  在 `createTask` 的 `sealed, err = sealIdempotency(...)` 之后插入"用 handler 自己的
  `json.Marshal(viewOf(created))` 覆盖 `sealed`" ⇒ 跑 `TestIdempotentReplayBodyIsByteIdentical`
  **红了，一次 FIRED**，且咬中**该咬的那几条**（首次 body 含明文、首次 body 无打码标记、
  首次≠重放、wire≠存储；重放体是 `[REDACTED]` 的打码版、键序也从结构体序变成 DB 序）。
  随后**字节级还原**：`Copy-Item 备份 → tasks.go`，md5 复验 **`21C7105E8F5A19B7F826299C38D3AF28` 一致**、
  `MUTATION` 字样残留 **0 处**；还原后同一条用例 **`ok … 1.751s`、exit 0**（回绿）。
  **判据不是永真式**这一格由此关上：变异在、测试红；变异撤、测试绿。

  **改动文件清单**：`core/test/crash_test.go`（1442 行，sha1 前 12 `7F67B8DCAF90`）、
  `.qoder/progress/test.md`（本文件）、`.qoder/evidence/test/crash-skip-t276-0928-{repo18pkg,directed,pkg}.txt`（三份读数）。
  **临时变异且已还原**：`core/api/http/tasks.go`（md5 前后同一）。

  **仍缺前置 · 归属清单**（这 5 格只拆不删，前置一到就按各用例注释里的口径换成硬断言）：
  ① **REQ-RC-1 Recovery Engine 未交付** → **backend/recovery**（`cmd/core/main.go:3~7` 自陈未交付；
  `internal/execution/repository.go:69 ListCrashRecoveryCandidates` 生产码零调用、全仓只有
  `repository_test.go:124` 一个调用点；`specs/contracts-internal-v0.1.md:700~703` 要求恢复扫描器是 Recovery 的入口、
  立项不派工，`:662` 不可达 ⇒ 立项不派工就不改状态机）；
  ② **`process.Supervisor.Run` 无生产调用点** → **backend/process**（唯一 emit `EventCrashed` 的地方是
  `internal/process/supervisor.go:405 Supervisor.Run`，而 `cmd/core` 只走 `processpipe.go:162 Start` / `:211 Stop`；
  原 Skip 文案"Notification 有 process_crashed"与协议 §8 不符 —— Notification 属**连续 5 次崩溃预算耗尽**）；
  ③ **approvals 接线** → **T-372 + backend**（`core/api/http/approvals.go:30/:48/:56` 三条 501、
  `cmd/core/main.go:174` 只构造 `approval.NewSQLiteStore`、**无 `approval.NewService`**，
  故 `SweepExpired` 无生产调用点）。

- **09-27 16:3x~16:43 轮（T-385 三件）**：
  ① **两段登记在盘上核对**（绝对 grep + `git show --stat` 证明两棵树代码相同）；
  ② **转绿我自己两面复现**：`f1558cc` PASS / `a432b96` FAIL 24 条 ⇒ 排除"判据已经 stuck-green"这一种假设；
  ③ **锚句三件事**（多命中打行号 / 自称条数=实取数 / 锚不到必停）+ 新用例 `TestProducerRosterParserLegs` 四条腿 + 四变异；
  ④ **N4 那条空腿当场改硬**（"认不出条数就只报不拦"会让自检变摆设 ⇒ 排除/降级分支必须自带一条会被它放走的断言）；
  ⑤ 残留那一格提成 REQ-T37 交主控，不自裁。

- **09-27 15:3x~16:00 轮（T-379 门禁 + 干净树全仓四张表）**：
  ① **接线四条**：`check_1` approvals SQL、`check_2` 的 `.Exec(` 旁门、`check_6` 的三条 import 方向、`varpass_sites` ——
    全部走新的 `strip_then_match`（剥注释 + 按同一正则重匹配），接线点集中一处。
  ② **F「响应消息/文案位」两条形状**（引号内嵌 / `write*()` 第 4 位参数），单独一类 `msg` 且连条数带位置打印；
    定义位与写口首参两条反向断言钉住窄性。
  ③ **零自喂**：没删改任何人的注释或文案；改动只在 `scripts/check_ownership.sh`。
  ④ **自证**：修前只加反例 ⇒ 1 条 SELFTEST FAIL；control SILENT；M1–M4 各自 FIRED；修前/修后同树输出逐字相同。
  ⑤ **全仓绿按集合分两面交**（Ⅱ-b 的 `46dafae` 全量导出）：不含我未跟踪判据 = 18 包 ok、顶层 573/581 零红；
    放进我的判据 = 17 包 ok + 唯一那条已知红（24 条，等名单落文）。`git archive` 不含未跟踪文件这个坑是我自己抓的，
    不抓就会交出一份「绿得不含新判据」的数。

- **09-27 14:5x~15:08 轮（T-371 五件：①②③④ 落地、⑤ 的门前一刻改道）**：
  ① **第四层落地且只吃常量/字面量**：`Type: 形参` 顺着同包调用点回解，认到的名字单标「仅第四层认定」；
     **未决**成桶（位点逐个 `t.Errorf` 点名 + 牵连名字另列），名单只准登记"确认无生产者"那一桶。
  ② **lits 兜底降级**：从"产生覆盖"改成"只报未决所及"——同文件一个字符串不能替一个看不见的发射位作证（那是假绿方向）。
     这条是庚那条合成件逼出来的：它的汇聚函数原本写在发射位同文件 ⇒ 未决永远测不到。
  ③ **判据行文本 + 名单可粘句都交了**（REQ-T36），`specs/**` 一个字没动。
  ④ **两个口径同时给**：旧 29/26、新 **27/24**，差的两条就是 `process_crashed`/`process_exited`（第四层认到）。
  ⑤ **六条变异 + 四条合成反例**；关闭证据走 Ⅱ-b 的 HEAD 快照 `bc03526`，与脏工作树那一读红名单逐字相同。

- **09-27 13:5x~14:13 轮（T-365②③④ + T-346③ 处置表）**：
  ① **交集过滤**：发射位回解出的候选先与 §6 展开集取交集才算覆盖；兜底（lits）不预先筛，好让 M 这个数存在。
    覆盖集从 49（含 14 个格式串/文案）收敛到 35。派生集合与红条数**没变**（29 / 26）⇒ 修的是形状，不是取数。
  ② **N/M/K 上屏**：每个位点 `候选 N=%d / 因不在 §6 词表被丢掉 M=%d / 实际覆盖 K=%d`，双行号（`Type:` 值行 + 字面量起行），
    盲区另加 `值来源=` 与 `所在函数=`；汇总行 N=109 / M=64 / K=35。
  ③ **具名盲区**：`internal/process/lifecycle.go:223`（221 字面量起、222 EventID、223 Type）唯一失明处，
    `blindImpact` 顺着**同包调用点**把第 4 号形参的实参回解一遍，打印 `process_crashed process_exited`，**不进覆盖集**。
    没改 adapter/backend 任何一行码；「要不要通」提成 REQ-T35。
  ④ **处置表**：26 条分四账 —— 盲区所及 2、只有定义位 22、只在错误文案 2、代码零出现 0、待查真引用 0；
    每条带 `file:line` 原文，计数面 = `00bb3c4` 快照的 75 个非测试 .go（脚本 `.scratch/test/triage_t365b.py` 可复跑）。
  ⑤ **两条合成反例（戊/己）+ 四条变异**，v1 表分类错误（把注释当真引用）自发现、自作废、重做。

- **09-27 12:46~13:28:30 轮（T-355：把那条 AST 判据从「44 条假红」修到「站得住且现红 26 条未登记」）**：
  - **主控的反例是对的，我原先那句「回解不通不影响任何名字的归类」被当场推翻**：`internal/task/service.go`
    先 `typ, v = EventRunning, RunningPayload{...}` 汇聚、再 `Type: typ` 建 draft —— 一个汇聚位把十几个名字
    汇到同一条 `Type: 变量` 上，我原来那句等于把「看不见」当成「没有生产者」。
  - **回解改成三层，方向单向放宽**（只往更不确定放宽，永不往「更没有」放宽）：
    ① 包常量（带包名，`task.EventCreated` 与 `execution.EventCreated` 同名不同值）
    ② 同文件/同包的局部赋值链（含 `evType := pick(...)` 这种**跨文件同包**调用）
    ③ 本文件函数体内出现过、且在词表里的字面量（最后兜底，候选偏大只会少报无生产者）。
    发射位仍只认 `EventDraft` 的 `Type:`（四个写入口末位实参都是它；`Tx.Append` 不认）。
  - **五轮都被合成腿抓、没有一轮是我自己想通的**（记下来是因为每一轮我都差点直接去看真树数）：
    ① 只认常量 ⇒ 41 条假红；② 兜底没限定词表 ⇒ 「被覆盖」冲到 164 > 词表 64；
    ③ `a, b, err := f()` 多 LHS / 单 RHS 被我按等长配对跳过 ⇒ 汇聚位没接上；
    ④ 函数值表按**文件**收 ⇒ 跨文件同包（`eventOf` 在 `execution/events.go`）还是瞎；
    ⑤ 裸标识符只查本文件 alias、不查包常量表 ⇒ 同一个洞的另一半。
    收完这五刀：覆盖 15 → 49 个，红 49 → 26 条，看不见从 12 处降到 1 处。
  - **现在的结论（可归因、A/B 两棵树同分类）**：词表 64、查无生产者 29、名单只登记 3 ⇒ **26 条未登记**；
    每条都机械核验过（非测试码里去掉定义位后零痕迹），不是抽查。**判据现红，但红的是盘上事实，不是判据坏了。**
    三条「不许」我都守着：没扩名单、没把 26 条降成 INFO、没缩扫描范围。
  - **`task_running` 与 `project_created` 都不在红名单里了** —— 主控给的那条反例现在是这条判据的合成腿之一
    （甲 裸字面量 / 乙 只有定义位+注册表 / 丙 汇聚位覆盖 / 丁 定不到必须点名带 `file:line`）。
  - **这条红现在挡着全仓那份绿**，而处置权不在我：要么把名单从 3 条改到 29 条（`specs/**` 是主控的面），
    要么由 backend 补发射。**我不自己动任何一边**，交账见 REQ-T34。


- **09-27 12:36~12:45:08 轮（T-346 补裁：条件裁(b) 我照办、失明分级落进代码）**：
  - **① 真树腿触发条件裁 (b)**：「`core/` 除我自己那一件之外无 M/?? 即可，读数里逐条列脏清单」。
    主控给的理由是**按字面会死锁**（我那个未入册文件正是被验对象本身，而规矩是「你交账我才入册」）——
    这条我认，并且把它写成了三个附条件的执行版：Ⅰ 起跑前把 `git status --porcelain -- core/` 输出**逐字**进读数
    并标出哪一行是我自己；Ⅱ 别人那几件任意一件在 ⇒ **不起**；Ⅲ 该读数不当 REQ-S-1 / Wave-2 关闭证据。
    入册时机按主控那句：**我带着三腿自证 + 这个读数形状交账，他按角色切一笔**；入册后限制自然消失。
  - **② 失明分级**（见 Verification 同段）：从「有失明就全红」改成 **fail-closed 但只红在能归因的那一格**。
    这一改不是我松手，是主控把语义钉准了：定不到候选名字的失明**不该**变成别人的假红。
  - **③ 展开一份**：同包 `parseEventCatalog → expandCatalogRows → expandEventCell`，未另拄表、未重写①~⑤，
    与 T-346② 与主控 ④ 里否掉「再开一处展开规则」的原意一致。
  - 本轮**没起真树腿**（Ⅱ 不成立），也没起任何采样；只跑了 `go vet ./test/` 与一条 anchored 合成用例
    （两者都在 67f064b 那棵树上取了指纹）。

- **09-27 12:0x~12:2x 轮（T-346 三件：②代码落地、③我按住没跑、①按裁乙不建过渡件）**
  —— 停摆 47 小时后第一件事是复量，不是接着写（我 09-25 曾凭空写过一次提交，那条教训记着）：
  - **② 代码已落地**：新增 `core/test/event_producer_roster_test.go`（未跟踪、在我地盘内）。形状按 T-346② 裁的甲：
    `go/ast` 扫**发射位**——只认 `EventDraft` 复合字面量的 `Type:` 字段（平台层四个写入口 `Tx.Create` /
    `Tx.CreateVersionless` / `Tx.TouchTransition` / `Tx.AppendEventOnly` 的末位实参都是它），
    `Tx.Append` 签名里没有事件、**故意不认**（认了就是把「只写行」算成「发了事件」，与 REQ-DB-6 相反）；
    常量**回解成字面值**、与 §6 展开后的词表做减法、再与散文「当前态」那句双向对拍。
    §6 的展开**没有第二份实现**：直接调同包的 `parseEventCatalog`/`expandCatalogRows`。
    三条腿在 `TestEventProducerScannerLegs`：字面量发射位不算无生产者 / 只有定义位+注册表位的必须算 /
    双向差集两边都点名，外加一条**回解不通必须点名到 file:line**（不许静默当成「没有生产者」——
    上次那 18 个假红的成因正是这个）。
  - **★ 我把上一版规则里第三个变体抓出来了**：真树第一跑（12:17:03）报 7 处「回解不通」，其中含
    `internal/task/service.go` 与 `internal/execution/service.go` 的 `Type: EventCreated` ——
    **task 包与 execution 包各有一个同名常量、值不同**（`task_created` / `execution_created`）。
    我按「标识符名」查常量表 ⇒ 同名多值 ⇒ 判成看不见 ⇒ 那两个**真有发射位**的名字会被算进无生产者集合。
    这是同族错的第三个变体（第一个是漏 `[a-z]*`、第二个是只认常量不认字面量）。
    已改成按 **包名 → 标识符 → 值** 三级查（裸标识符按本文件的包查），合成件补了同名跨包那一格。
  - **② 的自证未完，卡在树上**：12:18:38 起 `go vet ./test/` 与 `go test` 全部 build failed，断点是
    `internal/execution/send.go` 一连串 `cannot use SendResult{} as SendReservation value`；
    `SendReservation` 在工作树 4 处、**HEAD 里 0 处**，`git status --porcelain -- core` 只有这一个 ` M`
    ⇒ backend 正在写 T-346① 那一搬的在途态，不是回归、也不该我修。按我自己那条门①：**预检站不住就不起采样**，
    所以我没有再去「想办法绕过去拿数」。
  - **③ 分诊我按住没跑**，一条理由就够：要分的正是 backend 手上那半棵树（`execution`/`task` 的发射位就在改动面上），
    现在分出来的类别几十分钟后就变。可以先给形状普查（粗筛、非判据读数）：非测试码 `Type: 字面量` **10 处** /
    `Type: 标识符或常量` **43 处** / `EventDraft{` 字面量 **27 处** ⇒ 两种写法**已在同一族结构体里并存**，
    所以「发射统一走常量」值得立，但**它不是我这格的前置条件**（包限定的回解已能吃下两种写法）。
  - **① 按裁乙**：那条过渡件我不建。理由与主控一致——它现在必然恒红，恒红不进判据表；
    而 backend 正在写的就是那一搬，我此时搭 23 字段的 `httpapi.Config`，他们收口后大概率对不上，
    那份红既不能当反例也不能当正例。等 `send.go` 交账，我一轮之内把两腿跑出来。
  - **④ 那句「现在是窗口」已经不成立**（我 12:0x 复量时成立：`core/` 0 处、`HEAD=af30a9a`；12:17 变 `d4e0009` 仍干净；
    12:18 起 backend 落笔 ⇒ 现在编不过）。可复现的那半句我先交：9b 门 **12:2x 复跑 lint exit 0、49 条 ID / 去重仍 49、
    `--selftest` exit 0**，`flaky_watch.sh --selftest` 同轮 exit 0 ⇒ **这两版脚本都能被下一轮逐字复现**，命令面没变。


- **09-25 20:2x~20:4x 轮（T-316④⑤：ID 去重门落地面；「无生产者名单」我按回现实）**：
  - **④ 已落地**：`scripts/check_ownership.sh` 新增 `check_id_unique`（标成 **9b/9**，沿用既有 2b/9 那种子编号 ——
    不把 /9 整体改成 /10：别人账上写着 2/9、8/9，改号会把那些引用全变成假 0）。三条腿按裁的形状：
    ① 每族**在同一文件内**字面计数 == 去重计数（跨文件重复按镜像惯例只数不判、INFO 打印）；
    ② 合法增行不误伤（带字母后缀，`T84-7a` / `T84-7b` 算两条）；③ 合成重号必报 + 表失明要喊。
    **真树读数：七族 × 行首表格行 共 49 条 ID、去重后仍 49 ⇒ 0 红**；`--selftest` **exit 0**（9b 四条断言全过）。
  - **④ 的自证不止「跑一次绿」**：三把变异锤（锚点先数过，避免把拒改当无效）——
    **A** 摘掉 `[0-9]+[a-z]*` 的后缀（5 处全落在 9b 自己那几行里）⇒ selftest 立刻 2 条 FAIL / exit 2，
    腿① 报「合法表被判成重号（VIOL=1）」——**这就是我 19:22 亲手造过的那个假红，现在有腿拦着它了**；
    **B** 把重号那一支改成永不自反 ⇒ 只有腿② 红（VIOL=0，期望 1）；
    **C** 摘掉族为空的下界闸 ⇒ 腿③ 两条都红（只喊了 0 族，期望 7；空表还打印了「无重号」那句 = 拿空集当证据）。
    三把锤 exit 均是 2、FAIL 行数 **2 / 1 / 2** 两两不同；每把都在同一批命令里按备份还原，
    **还原后 md5 与变异前逐字相同（`024c755a`）**。
  - **⑤ 我先按裁的形状试做派生，量到它现在不能落成红**（详见 REQ-T31）：规则「常量在非测试码被引用过 ⇒ 有生产者」
    派生出 **21 个零生产者名字**，而散文名单只有 **3 个**。我一度以为名单漏了 18 个 —— 直到读到
    `internal/project/service.go` 的发射现场：`tx.Create(..., db.EventDraft{EventID: d.EventID, Type: "project_created", ...})`，
    **event_type 是裸字面量、不是常量引用** ⇒ 我的规则对它天然失明，`project_created` 明明有生产者却被判成零。
    **这条判据一落地就是 18 个假红**，正是我替别人挡过多次的「拿判据逼出更坏的码」⇒ 我没落它，改成两条路交回主控裁。


- **09-25 19:43~19:48 轮（T-303 四件：正式交账文本 + 让窗 + §6 更正自查 + 一个新格上报）**：
  - **② 交账**：FL-10 行状态列的**整段可粘文本**写成 **REQ-T29**（我不碰 `specs/**`）。里面按主控点名的形状齐了：
    有效样本 122 / PASS=122 FAIL=0 SKIP=0 BROKEN=0、两戳逐字相同、门①②各自"怎么在位"写清、
    **负载条件直接用段日志原文 `载荷=churn-1 spawned=9311`**（巡检说这比我转述硬，我采纳），
    **并发那一栏写的是实事**：这批与 adapter 合验同时段（它侧三件产物 mtime 19:12/19:12/19:13 落在我窗口里），
    并明写"**空机读数不在本批里**、归 19:3x 让给 adapter 的那个窗口 —— 排程必然、不是采样缺陷"；
    末列还留了**四批作废史**（07:1x 两批 / 18:17 / 18:26 并池 124 降级为旁证），免得下一位以为 122 是第一把就成的。
  - **③ 让窗**：本轮起我不再起任何采样，**连 `go test ./test/` 两面也没重跑** —— 那两面会起真子进程，
    正是要让给 adapter 的无并发窗口。**这不是偷懒是有依据的**：`git log --since 19:05 -- core` = **空输出**
    ⇒ 19:05 之后没有任何提交动过编译面（`4b4739e`/`f25ee6c`/`6b28868`/`c46339c` 全是 specs+ledger），
    所以那两面读数至今仍归属当前这棵树；**我不把它写成"刚跑过"，只写成"仍有效 + 为什么不重跑"**。
  - **④ §6 更正我按字核了、结论与主控一致**：`6b28868` 对事件契约 = **13 insertions / 0 deletions**，
    新增行**全部是 `>` 引用块散文** ⇒ §6 词表一行未动 ⇒ C8-11 族判据输入不变、不重跑；
    行为面证据是 `check_ownership.sh` exit 0 且 **INFO 全块与 19:20 归档 `diff` 无输出**（逐字相同）。
    另把"要不要把'有名字无生产者'名单做成判据"当**新格**报上去（形状只有"派生集合 == 散文名单"双向对拍这一种是硬的，
    三个坑随附），**没裁就不动**。

- **09-25 18:48~19:2x 轮（T-280② + T-299：并池 124 次我按两道新门不交关 → 把门① 长进采样器 → 19:08 起的合格批 122 次已交）**：
  - **为什么改跑法**：18:17/18:34/18:35/18:41 四批全被自己的门挡下（SKIP 不算样本、116/39 次没拿到 `---` 行、
    前后指纹跨版本）。根因是这棵树上每一两分钟就有人在写 `internal/task`、`api/http`、`cmd/core`，
    而 FL-10 从没要求"122 次时间上连续"，它要求的是**这 122 次跑的是同一版码** ⇒ 换成 4 段 × 31 次、
    **段内前后指纹相同且 0 SKIP/0 BROKEN 才计**、同一指纹值才并池（`.scratch/test/run_flaky10b.sh`，一次性取证脚本）。
  - **读数**：并池有效样本 **124**（= 真跑过、拿到 `---` 行的次数；PASS=124 FAIL=0 SKIP=0 BROKEN=0），
    四段八个戳逐字相同 = `6b36e9e3930864b5f6c17fcec38e1ff9`，窗口内无 `.go` 被改写，`lock.md5=6c379e1c`
    （= REQ-L-1 那版端口锁，归属提交 `2c56023`）。负载：三臂各 ~2440~2590 次 spawn / 段时长 ~89~95s
    ⇒ **≈80 进程/秒**，比 FL-10 行举的 ~128/秒那批低 ⇒ 只报"载荷下没红"，不跟那批比发生率。
    证据：`.qoder/evidence/test/flaky10-0925-1848-124pooled-0red-newlock-gate2-dirty.txt`。
  - **★ 这批我不拿它交关**：主控在 18:47:07 提交 `4b4739e` 给 FL-10 行**补了两条前置门**，比我起跑（18:48:58）早 1 分 51 秒，
    我按的是 10:3x 那一版行文本 ⇒ 逐条自评估：**门①（预检先于采样）当时没跑**（事后 18:56:30 补跑：
    `go build ./...`、`go vet ./test/`、`go vet ./cmd/core` 全 exit 0，那批树确实编得过，但"事后补"不满足"先于"）；
    **门②（只在已交账的提交上跑）不满足** —— 被验面 `core/` 未提交 7 处（5 改 + 2 未跟踪，全是 backend 在途码），
    这批的指纹里编进了别人没交的码，正是行里点名的"替别人制造假绿"那种形状。
  - **门① 的修法 = 长进采样器，不留躲法**：`scripts/flaky_watch.sh` 新增 `preflight()`，排在取可归因戳之前
    （戳是"这批属于哪版码"的凭据，烧在编不过的树上取的戳没有归属价值）：先 `go build ./...`、再 `go vet "$MAIN"`，
    任一站不住就打印"哪个包哪一行编不过 + 等当事人交账"并 **exit 2、一次采样都不起**。**故意没有跳过预检的开关或环境变量**
    —— 一条能被躲掉的门不叫门（跟 §1.1 第 19 条那条指纹门同理）。
  - **自证（三条腿，双向）**：`--selftest` 从 14 条变 **17 条**、exit 0。假 `go` 桩分别让 `build`/`vet` 失败 ⇒ 两条"拦下"腿各自落地；
    桩两个都绿 ⇒ "放行"腿落地。**变异 A**（把两处 `rc != 0` 改成永假）⇒ 两条"拦下"腿立刻 [SELFTEST FAIL]、selftest exit 2
    （门若变摆设，自测会喊）；**变异 B**（改成永真 = 永拦）⇒ "放行"腿 [SELFTEST FAIL]（门若锁死采样也会喊）。
    两次变异都在同一批命令里按备份还原，还原后 md5 与变异前逐字相同、残留 0；真路短跑（`-n 2`）打印
    `预检（FL-10 门①）：… ⇒ 起采样。` 后正常出判决 ⇒ 门在真实路径上是通的。
  - **★ 19:08:22~19:14:02 那一道合格批（T-299 放行后起的，两道门都在起跑时成立）**：
    backend 那 7 件由主控 19:03:11 入册 `a9f975e` ⇒ 19:08:10 实测 `git status --porcelain -- core` = **0 行**（门② 到位），
    而我 18:5x 把门① 长进了 `flaky_watch.sh` ⇒ 起跑时预检自己打印 `go build ./... exit 0、go vet ./test/ exit 0`。
    **读数：有效样本 122（PASS=122 / FAIL=0 / SKIP=0 / BROKEN=0）**，前后两戳逐字相同
    （`指纹=6b36e9e3930864b5f6c17fcec38e1ff9`、`HEAD=f25ee6c`、`lock.md5=6c379e1c`），窗口内无人写 `.go`，
    载荷三臂各 spawned 9311/9322/9332（≈82 进程/秒）。判决仍是「**未复现**」⇒ 量级腿交齐，
    **REQ-L-1 不由我关**（确定性反例那腿在 backend 的 `lock_test.go`）。
    证据：`.qoder/evidence/test/flaky10-0925-1908-122single-0red-bothgates-newlock.txt`。
  - **★ 我在这批里改了自己一句话**：写证据头部时我读到 T-297 是「待投递」，就写成"窗口内没有并发"；
    19:15:05 再读它已变「**已投递（19:11，巡检）**」——**19:11 就在我这批窗口里**。已就地改成
    "自测载荷 ≈82/s + 窗口内 19:11 有合验令投递（并发量未知，不报成空机）"，并记下能证的只有
    adapter 账本停在 18:57:51、进程表两次读数 0 —— **"账本没动"不等于"没跑过"**（T-276③ 同族：我拿一个
    弱读数当了近因）。下一批的补法：窗口内每 30s 记一次进程数，别只在收尾取一次。


- **09-25 18:02~18:2x 轮（T-280 两件：① ownership lint 第四例假阳性，② FL-10 量级腿真跑）**：
  - **① 复现成立**：18:02:44 `check_ownership.sh` **exit 1**、`1 处：attachments@core/internal/session/record.go`，
    命中的那一行现读是 `b, err := json.Marshal(map[string]any{"attachments": cmd.Attachments})` ——
    **`attachments` 是 map/JSON 的键位，不是"表名以变量传入"**。前三例（`c.Tasks.Create(` 函数名、复述规则的注释、
    `/messages` 路径段 + `verb == "messages"`）都是同一族：**这个词既是表名又是别的什么**。
  - **修法 = 给 2/9 补第五类值位形状 E**：`{` 或 `,` 之后紧跟引号包住的名字、**且名字后面紧跟冒号**才算键位。
    三条要求逐条对上了：⑴ **排除要计数打印** —— 沿用 W13 那节的既有做法，INFO 从 `4 处` 变 **`5 处`**，
    并把枚举补成"…、map/JSON 键位"（原来那句只列到 return/strings，**文案比代码窄就是新的误导**）；
    ⑵ **写腿排在排除之前** —— 次序没动（W13 调用形 → 裸 SQL 写 → 值位 → literal/tag/read）；
    ⑶ **双向反例** —— 合成件里同一个 `quota_accounts` 写两行：`key.go`（map 键位）必须不红、
    `delrow.go`（`DELETE FROM quota_accounts WHERE id = ?`）**必须照旧红**；只验前一半等于把名字放进豁免表。
  - **定义位没被吃掉**：E 认的是"名字后面是冒号"，`const t = "attachments"` 后面是 `=`、`Append("attachments",` 后面是 `,` ⇒
    都进不来；已有的 `dyn.go`（定义位必须仍是 literal）与 `w2.go`（写口首参必须仍是 write）两条断言原样在位。
  - **牙是量出来的，不是说的**（备份/还原同条命令、`落锤=1` 先证变异真落上）：
    ME1 把 E 的 `[{,]` 改成不可能匹配 ⇒ selftest 两条立刻红（`key.go 没被单列成 value` + `仍被判成 literal/write`），
    并且**真树的红原样回来了**（`attachments@…record.go` 1 命中、lint exit 1）⇒ 合成件与真实案例同一形状，不是我另编一个；
    ME2 把 SQL 写腿的 `DELETE` 改坏 ⇒ **只**喊 `DELETE FROM quota_accounts 没判成 write`（lint 仍 0）⇒ 排除与写腿互不掩护。
    还原后 `[ZZ]`/`:ZZDELETE` 残留 **0**、`bash -n` 干净、lint 与 selftest 都 exit 0。
  - **一次我自己差点把"超时"读成"没结果"**：变异电池跑过 300s 被挪到后台，我第一件事是回读盘上有没有残留变异
    （`[ZZ]=0 :ZZDELETE=0`、备份文件已消失）才继续 —— **变异没还原就交账，比漏做更坏**。

- **09-24 10:51~11:09 轮（T-273 七件：①②③④ 动手、⑤ 清点、⑥ 交文本、⑦ 复量）**：
  - **① 先纠一件会误伤的事**：我 10:51 无旗标跑 `TestDroppedFrameReachesAuditSeam` 看到 **7a 也 SKIP**，
    差点当成新缺陷——实为**旗标门**（父体要 `LUCERO_TEST_FAKE_ADAPTER=1` 才起子进程），带旗标跑 7a 照旧 PASS（0.77s）。
    **读数前先确认自己开的哪一面**，这条我写过 Yet 又差点犯。
  - **① T84-7b 复量：三段前提现在两真一假，缺的那一段我说得出名字**。①丢弃记账确实挂上桥
    （生产调用点唯一一处，构造函数已从 4 参变 **5 参**：多了 lifecycle 写通道 —— 我的门只认"调用点存在"，不受签名影响）；
    ②a 注入面 **adapter 已经落地**（`cmd/fake-adapter` 那个门控常量在盘上；巡检 10:36 量到 0 命中是**它落地之前**），
    丢弃判定本身也早就在 `internal/process` 的归属函数第二分支里（帧带 execution_id 且与票号绑定的不等 ⇒ 丢弃 + 记账）；
    **②b 仍不成立：Core 生产码里没有任何地方发起 turn（`CallTurn(` 调用点 0，只剩定义与测试）** ⇒ 门开着、没人推。
    ⇒ 7b **继续 SKIP**，但 SKIP 文案重写成上面这三段可核事实，并附**三条自查命令**（谁把这段话改坏了，跑一遍就看得出来）。
    顺带清掉我旧文案里两处 `dropaudit.go:38`/`processpipe.go:30` 式**行号引用**（按 §1.1 第 14 条早该换成符号）。
  - **③ Q1/Q2 我自己复量完就 closes 掉了，没重开**：契约② 新增 **§4.1 写方法清单**（裁定"approval/respond 算写方法"），
    代码侧 `writeMethods` 也已含它、注释直接引 §4.1；`session/create` 在 §4 方法表里有了自己的行（Q2 的原始症状消失）；
    生产码不再引用那个不存在的 §12.11（`12.11` 在 core/ 还有 4 处命中，**逐条读过**：2 处在我自己注释里、
    1 处在 adapter 的测试注释里解释"当初引了个不存在的条款"、1 处在我 SKIP 文案的历史说明里 ⇒ 生产码 0 引用）。
    ⇒ `TestWriteEntryOpenQuestions` 从 SKIP 改成**硬断言**，形状就是我自己那三条腿：
    §4.1 写集合 ↔ 代码 `writeMethods` 双向相等（`debug/*` 由代码侧 `isDebugName` 兑现 ⇒ 摘出来单独核，不静默豁免）、
    两侧内部重名各判一次、**合成反例喂进同一个比较函数**。实测 **§4.1=6 项 / 代码=5 项**，等式成立。
    - **这条用例第一版就被自己抓住一次写反**：我的合成表里"契约少一项"那行期望 `diffSets(spec,code)` 点名多出来的方法——
      但那个方向是"a 有 b 没有"，多出来的一项在**另一方向** ⇒ 用例当场 FAIL。改成"两个方向的并集里找"，
      并写明为什么这样仍然锁得住方向性（只实现一个方向的话，另一条用例会红）。
    - 牙也验了：M1 把解析锚点改坏 ⇒ `t.Fatal("契约② §4.1 的写方法集合解析失败…")`；
      M2 把"§4 表里必须有 `session/create` 那一行"改成引用不存在的行 ⇒ 精确 FAIL。两次都在同一条命令里还原，残留 0。
  - **④ REQ-T15 后半按你裁的形状落了**：`varpass_sites`（写 API 首参是标识符 ⇒ 表名来自变量）在 2/9 那节
    **INFO + 计数打印**，明写"这条腿看不见它传的是哪张表，计数在此留痕、不当已验"；真树今天读出 **0 处**。
    "0 处"我没直接信：单独把它做成一个函数（检查与自测共用同一份正则，不抄第二份），
    合成件三向都进 `--selftest` —— 变量传参**必须数到**、字面量首参**必须不算**（两档不许互稀）、
    `platform/db` 自己**必须排除**；再把正则里的方法名改坏 ⇒ 正向那条立刻红 ⇒ 那个 0 是测量不是死腿。
  - **⑤ REQ-T10 清点（你要的三列：表 / 行数 / 命令或状态列指向我地盘）**：
    `C8 14/9`、`F20 4/3`、`W13 3/0`、`U13 7/1`、`A18 3/0`、`T84 8/7`、`FL 11/5`、`MS 6/2`（11:09:14 现量）。
    **注意 FL 那个 11** —— FL-1~FL-10 只有 10 个号 ⇒ 与我 10:4x 报的**两个 `FL-6`** 对得上，两件是同一件事。
    `W13 3/0`、`A18 3/0` 不是漏指针：它们的执行者是 `check_ownership.sh` 的 2b/8 两节，契约里按**表名**引用而非路径，
    我这把 grep 只认 `./test/`、`scripts/`、三个脚本名 ⇒ **这一格要写清口径**，否则又是"0 命中当结论"。
  - **⑥/⑦**：F20-1 的可粘贴行文本连同自证读数交在下面的 **REQ-T26**；
    ⑦ 那格我复量了（`ProcessRole` 在契约③ 4 处命中，逐条读：字段清单已改准、⚠ 后继注保留、另两处是裁定原文与其旧实测），
    与巡检描述一致，我不重做。

- **09-24 10:32~10:46 轮（T-270 五条裁定收到 + T-272② 改我自己那句钩子；顺手抓到契约② 表里一个重号）**：
  - **T-272② 那句钩子改完了**。旧文案（`check_ownership.sh` 的 `[契约冲突]`）有三段不该由脚本说：
    ① 它**根本没读** orchestration.md 就断言"摘要表判给 adapter"；② §7 其实**没有针对这条路径的窄行**，
    命中的是宽行 `specs/**`→orchestrator，"§7 判给主控"把"宽行罩住"和"指名归属"混成了一件事；
    ③ 最坏是"**且 adapter 已据此在改**"——脚本不可能知道谁在改，而这次改的是**主控自己**。
    现在三段各归各位：摘要表**真的去读**（现读到角色就报名字，读不到就明说"表里没找到这一行"），
    §7 那半句改说"没有窄行、命中的是宽行"，"谁在改"整段**删掉**。定位按内容（`ownership_conflict_note`），
    没用行号 —— 巡检特意提醒的正是我这轮刚立过的坑。
  - **三条新断言 + 两次变异**（都验过不是空断言）：合成表里有一行 ⇒ 必须报出现读到的角色；表里没有 ⇒ 必须老实承认；
    以及一条**形状禁令**：那句话里不许再出现 `已据此在改 / 正在改 / 已在改 / 已提交待复核` 任何现状断言。
    MU1（把"现读"退化成写死读不到）⇒ 只喊第一条；MU2（把旧那句"已据此在改"塞回去）⇒ 只喊形状禁令。
    **一次差点误报给自己**：还原后我那句 `grep -c '已据此在改\|-n ""'` 报了 **3**，看着像没还原干净——
    逐行回读才看清 3 处全在**解释旧文案的注释**和自测的词表里，`-n ""` 实际 0 处。
    记下来是因为这正是我今晚反复讲的"**非 0 命中也要读原文**"，这次当事人是我自己的检查。
  - **五条裁定的落点我自己逐项在盘上核过**（不靠转述）：契约② FL 表 `FL-9` = 同档内自我矛盾（REQ-T17）、
    `FL-10` = 量级腿，且 FL-10 那行正文里就写着"test 提案时编号是 FL-9，被 REQ-T17 占了所以顺延，**镜像以本行为准**"
    ⇒ **我 07:1x 起草的那版行文本就此作废**（下面 Coordination 的 REQ-T19 同步标注）；REQ-T20 两半都落了
    （FL-6 行扩了 `--- SKIP` 那半句 + 契约① §1.1 第 19 条那句"**n = 真跑过的次数**"）；
    REQ-T22 走的是我自己那个 ②（跨模块读不升判据），C1 裁在契约① §4："要 JOIN 别人表的语句**放消费方自己的包里**"；
    REQ-T21 判给 backend（T-271），我不动。我地盘里 `grep FL-9\|FL-10` = **0 命中** ⇒ 没有需要改号的引用（这条也是量过的）。
  - **★顺手抓到一件：契约② FL 表里 `FL-6` 有两行**（`grep -c '^| FL-6 '` = **2**，两处行号相邻，
    一行是旧版、一行带 REQ-T20 那半句 ⇒ **扩句时另起一行而不是就地改**）。其余 FL-1~FL-10 各一行，没有漏号。
    后果不是排版：**判据表按 ID 引用**（"按 FL-6 那行验"），重号之后哪一行是权威说不清，
    而更根本的是——**今天没有任何一条判据管"判据表的 ID 不许重号"**（我的 `firstDup` 只用在 §15.1 码名、
    transport 名、journal 常量值三处；W13 那三行我量过是唯一的）。已写成 **REQ-T25** 交主控：
    要么把两行并成一行，要么我给"三张判据表的 ID 列"补一条重号即停的判据（**要谁做、值不值得做由你定，我不自作主张加闸门**）。

- **09-24 09:23~09:34 轮（T-257 三条：② 是我的文案债、① 那条红自己关了、③ 计数口径我认）**：
  - **② 判完之后按"改文案"那一支做，但主控的诊断要补一句**：那条腿**代码没扫错文件**——它拿 `$SPEC1`
    （`specs/events/event-contract-v0.1.md`，§6 事件目录就在这份里，`## 6. 事件目录 v0.1` 在 167 行）做目录，
    只是**打印时把事件契约叫成了"契约① §6"**；契约①（`contracts-internal-v0.1.md`）确实只有 §0~§5，
    所以"真去扫契约①"这个选项**根本不成立**（那里没有可扫的目录），不是我二选一里被否掉的那一支。
    **⇒ 10:01 T-265③ 主控已认下这条**（原话："主控上一条给你的'二选一'里有一个选项不成立，它认了"），
    并已同步回 db（T-266）。账上那句"二选一"就此标成**唯一可做的形状 = 改文案**，别有人再拿它当第三条路。
    ⇒ 四处字符串改准（标题、注释、bad、info），并**顺带把这一腿的强度写实**：它判的是"整份事件契约里出现过"，
    **不等于"在 §6 目录表里登记过"**。我明写在注释里：真按 §6 收紧要重做"合并行展开"的解析，
    而那份解析已经住在我的 Go 用例（C8-12/C8-13）里 —— **同一条规则不在两处各写一份**，那是第四处真相。
  - **② 顺手抓到一个更坏的形状（比文案值得记）**：这一腿原先**没有下界闸** —— 一旦契约③ 的 §4 改号或挪走，
    `sed` 抓到空区间，它会打印"**0 个事件名全部已登记**"照样绿。**空集比空集相等是最坏的假绿**，
    而这条腿今晚恰好就靠这句活着。补 `n < 5 ⇒ bad（这条腿现在什么都看不见）`，
    并把这一腿做成可喂合成件的（`LUCERO_API_SPEC3` / `LUCERO_EVENT_SPEC` 两个钩子，跑出厂实现本身）：
    三向对照 = 全登记⇒0 红、少一个⇒点名 1 红、**空区间⇒下界闸必须响**。
    两次变异各自精确命中且互不牵连：把 `n < 5` 改成 `n < -1` ⇒ 只喊"空区间没被拦住"；
    把比对目标换成名字自己的来源文件（自问自答）⇒ 只喊"少登记一个事件名没被点出来（VIOL=0）"。还原后残留 0。
  - **① 那条红自己关了，我不认领也不消红**：frontend 09:24:45 把 `execution_starting` 补进 `core-v1.yaml`
    （`grep -c` 从 0 变 1），09:33:30 单跑 `TestEventTypeTripleEquality` ⇒ **PASS**；两面整包
    09:33:46→09:34:01 回到 **0 FAIL**（默认 42 PASS/14 SKIP、旗标 47/9，顶层 56=56，子用例 19/31）。
    **我一行判据没改、§6 一行没动、也没 SKIP 它** —— 09:08 那两条"1 FAIL"的读数原地保留并标注已被超越。
  - **③ 认。** 从今往后我交账里的"全仓/全绿"一律写成：**含不含 `core/test/` 与 `scripts/`**、
    **包计数面与用例计数面分开**（本回合两份读数都按这个格式写的）。
    顺带把我自己那条 09:06:24 自曝的口径账落在 Coordination，免得下一个人又拿 17 包当全仓。
  - **一条新发现，交给主控裁（REQ-T24）**：为了把文案写准我把 §6 区间单独数了一遍，结果两个名字在
    §4 与 §6 之间**对不上形状**：`execution_succeeded` 在 §6 是**合并行** `execution_cancelled / _failed / _succeeded / _interrupted`
    里的一个（所以按 §6 区间做字面 grep 会**假红**——这就是我不在 shell 里重做展开的原因）；
    `approval_resolved` 则是 §4 写 `approval_resolved (+grant_issued)`、§6 只登记 `execution_approval_resolved`，
    而契约① §1.2.1 第 2 条两个名字都用了（"写 `approval_resolved` 事件…作为 `execution_approval_resolved` 的 causation_id 来源"）
    ⇒ **三个文档三种写法，是不是同一个事件我不替你们判**，只把三处原文与行号级定位交上来。

  - **一条必须写进交账的口径纠正（巡检 09:54 提出、我 10:02:28 自己复量确认）**：上面那句"两面 0 FAIL"是
    **工作树口径**，不是干净检出口径。实测：`git show HEAD:core/internal/eventjournal/events.go | grep -c ExecutionStarting`
    = **0**，而工作树 = 2、该文件状态 ` M `、最后提交还是 `753900f` 09-23 01:03 ⇒ **谁现在做干净检出，
    C8-12/C8-13 就会报 `§6=64 / yaml=64 / 常量=63` 红一条**（yaml 那侧 frontend 已提交：HEAD 里 `execution_starting` = 1，
    所以缺的只有 Go 常量那一处）。**这不是判据缺陷、不是 frontend 的锅，也不是我能替他提交的**——
    归 db 把 `events.go` 那格落进一次提交。在这之前，我引用 64/64/64 一律带"工作树"三字，
    合验发令前这一格值得先确认（否则干净检出那天第一条红会被当成新缺陷）。
- **09-24 09:06~09:09 轮（T-254 只知会的那条红：复核后发现它换了格，且不在 T-252 范围内）**：
  - 事实链：db 09:05:35 补上 `ExecutionStarting` ⇒ **C8-12 已转绿**（我 09:06:33 单跑 + 09:08 整包两次读数一致）；
    同一遍里红的是 **C8-13**（`TestEventTypeTripleEquality`），点名 `[§6→yaml]` 与 `[常量→yaml]` 两条边、
    同一个类型 `execution_starting`，计数 `§6=64 / yaml=63 / 常量=64`。
  - ⇒ **主控 ① 的预告只覆盖了两份登记物中的两份**：契约 §6 与 Go 常量都在了，**第三份 `specs/openapi/core-v1.yaml`
    的 `EventType` 枚举还缺一条**，而那一格按 T-172/T-183 对我**只读**（frontend 的守门保留）——
    **T-252 关不掉这条红，要另派 yaml 枚举那一格的所有者**。我按禁令一行判据没改、§6 一行没动、不拿"全绿"当盘面。
    派单指向已写成 **REQ-T23**（见 Coordination）。
  - 顺带把我自己的一次坏探针记账：想独立数 yaml 枚举的那条 awk 口径不对、数出 0，**不拿它当第二份证据**
    （字面 `grep -c execution_starting …yaml` = 0 才是可用佐证）。

- **09-24 08:51~09:02 轮（T-241 换了验收顺序：别拿"重跑不红"当已修 —— 三步照序做）**：
  - **T-251 只知会的那格我不做**：§7.3 那条"nil 反例"的形状判据归我复核、实现与证据归 backend，
    我不另写判据、不在合验包里重做（`specs/api-contract-v0.1.md:379` 的归属句巡检已复量在正文）。
  - **① 先立"真绕过照旧红"，而且加了一个更狠的形状**：原来的两个红件（`tx.Append("quota_accounts", m)` 首参、
    语法正确的 `UPDATE quota_accounts SET …`）之外，新增 **`w3.go` = `db.QueryRow("UPDATE quota_accounts: SET label = ?", 1)`** ——
    **SQL 语法不合法**（表名后面是个冒号）。加它是因为判据一旦暗含"这串 SQL 得能跑"，一句冒号就绕过关键字腿。
    三个红件都排在形状排除**之前**（写腿 → 值位排除 → literal/tag/read），所以排除开多宽都吞不到它们。
  - **② 形状排除已在前一轮落地，这一轮换的是"两个方向都要断"的证据**：六个值位合成件不许红 +
    三个红件/归类件不许漂（`dyn.go` 定义位仍是 literal、`tag.go` 仍是 tag、`ret2.go` 带调用的 return 不许被当成值位）。
  - **③ 最后才做"还原后的真代码不报红"—— 而且这一步今天能做了**：backend 08:45:43 把派生量撤了
    （`sessionMessagesTail`/`sessionMessagesVerb`/`TrimPrefix` 那对都不在了，`:104` 只剩一条正常的
    `strings.TrimPrefix(path, "/api/v1/sessions/")`），原样三行回到盘上。**拿 HEAD 里那份改前的脚本跑同一棵树**
    （939 行、`grep -c VALUE_SHAPES`=0 证明确实是改前那份）⇒ **exit 1、"2 处：整个表名字符串出现在非归属包里"**；
    我这版跑同一棵树 ⇒ **exit 0**、值位 INFO 计数 **4 处**。探针副本当场删除，残留 0。
  - **逐行对得上（不是只报两个总数）**，`core/api/http/sessions.go` 现盘：
    `:81 case verb == "messages":` 旧 RED → 新 value；`:115 return sub, "messages", true` 旧 RED → 新 value；
    `:111 strings.CutSuffix(rest, "/messages")` **旧就是 read/INFO，从没红过** → 新 value。
    ⇒ **要纠正一处引用**：T-236 与 T-241 都把这行当作被误报的两行之一，实测那"2 处"是 `:81` 与 `:115`，
    `"/messages"` 这种带前导斜杠的字面量结构上就打不到"整个字面量等于表名"那条腿（引号没紧贴表名）。
    这不影响修法（A 类排除照做、合成件 `cut.go` 钉住它），但**报出去的归因要按实测改**。
  - **摘一条腿要能自证（M3）**：把 SQL 写腿的 `UPDATE` 改成不可能匹配的 `ZZUPDATE` ⇒ selftest 精确喊出两条、
    且**只**喊这两条（`w.go` 与 `w3.go`），其余全绿；`落锤=1` 先证变异真落上，还原后残留 0、复跑 exit 0。
    （连同前一轮的 M1"排除全死"→8 条、M2"排除过宽 `.*`"→3 条，三向都齐了。）
  - **REQ-T22 换成有数版的口径**（见 Coordination）：`SELECT … FROM <表>` 这一腿今天不红是**设计**，
    而且契约里唯一写死的裸 SQL locality 规则只管 `approvals` 一张表（§1.2.1 第 1 条 + 它自己那条可执行 grep），
    跨模块读要不要收，§1.2.1 明写"等 §1.3 第 4 条 C1"——那格还没裁。我量了代价：**生产码里非 `platform/db`
    的 `FROM`/`JOIN`+真表名形状共 42 行**，动它不是加一行正则的事。

- **09-24 08:16~08:34 轮（T-236：ownership lint 的假阳性在逼出更坏的码，修判据形状）**：
  - **现状先说清**：我 08:16:11 重跑 `check_ownership.sh` 已经 **exit 0 不红了**——但不是因为判据修好了，
    而是因为当事人把字面量**拆**了：`core/api/http/sessions.go`（mtime 07:52:29）`:103` 是
    `const sessionMessagesTail = "/messages"`、`:105` 是 `var sessionMessagesVerb = strings.TrimPrefix(sessionMessagesTail, "/")`，
    而 `:101` 一句注释自陈"这里刻意不出现『整个字面量就等于 `messages`』的串：归属守门…"。
    **这就是 T-237② 明令禁止的"拆字面量换绿"，而且它拆出的形状（运行时 TrimPrefix 造出 verb 常量）比原来更坏。**
    巡检 07:54 已把这条上报，我不替它判动机；我只说：**这条绿是躲出来的，不是修出来的**。
    修完之后**躲的必要没有了**——`"/messages"` 属于我新排除的 A 类，它本来就不该红。
  - **① 四类"值位"形状不再参与表名比对**（只判语法位置，不要求代码留标记、也不要求注释喂它）：
    A 前导 `/` 的字面量（路径段）；B 与变量比较（`x == "tbl"`、`"tbl" != y`、`case "tbl":`）；
    C `return` 里非首元素且**中间没有 `(`**（`return sub, "messages", true`）；D `strings.*(…, "tbl")` 非首参。
    **顺序是承重的**：写腿（W13 调用形 + 裸 SQL 写）排在值位排除**之前**，literal/tag 排在**之后** ——
    于是"排除开得太宽"顶多吞掉 literal/tag/read，**吞不掉真写表越界**。这一条不是我嘴上说的，M2 变异量出来的（见 Verification）。
    **被排除的必须报出来**：新增 `value` 一类并打 INFO 计数（真树上现在是 `2 处 … messages@core/api/http/sessions.go`），
    静默排除与静默放行分不开就是 W13 那一节早就有的"非表名"计数思路。
  - **② 双向反例都进了 `--selftest`**：六个"不许红"的合成件（path/cut/verb/ret/case/fold，其中 `cut.go` 按主控
    原话那行 `strings.CutSuffix(rest, "/quota_accounts")` 逐字对照）+ 三个"必须红/必须归类不变"的合成件：
    `w2.go` 非归属包里 `tx.Append("quota_accounts", m)` 判 write、`ret2.go` `return nil, tx.Append("x", "quota_accounts", m)`
    **不许**被判成 value（C 类的边界，宽一点就开一个新洞）、`dyn.go` 的 `const tblName = "quota_accounts"`（定义位）
    照旧是 literal。**"排除值位、保留定义位"这个不对称就是整套放行的全部风险所在**，所以它是一条断言不是一句注释。
  - **两次我自己的错，都记在这**：
    ① **两条新断言一开始是永假的**：我在 ERE 里写了 `^(literal\|write)\|`，`\|` 在 ERE 里是"竖线字面量"不是择一
      ⇒ 那两个 grep 永不匹配 ⇒ "仍被判成 literal/write"那一腿完全没牙（与记忆里"`grep -E` 不认 `\s`"同族第四次）。
      发现方式是M1 变异跑出 8 条 FAIL 时**我预期的那两条却不在里面**，回头才读出自己的 pattern。
    ② **第一轮变异是空操作**：sed 锚点（含反引号与转义那行）没落上，脚本一字未改，我却看到"selftest 仍 exit 0"——
      差点把"变异后照样绿"当成结论写出去。现在固定打法：**改行号定位 + 改完立刻 `sed -n '<行>p'` 回读命中数**，
      没落上就不算做过变异（与"锚点不唯一⇒拒改＝无效"同族，这次是锚点根本不存在）。
  - **`SELECT … FROM messages` 那一格我没动，也不打算偷偷动**（主控 ② 的例子里它是红，盘上它是 read/INFO）：
    §1.3/§4 允许跨模块读与 JOIN，所以"非归属包读别人的表"今天按设计不判红；
    我把它**断成"现状 = read"**放进自测（哪天有人不改契约就把它改严，自测会红），并把"要不要把『业务包自行执行裸 SQL』
    升成一条判据"作为 **REQ-T22** 交主控裁——那是契约问题，不是我该在 shell 里替契约收紧的事。
  - **范围守住了**：只改 `scripts/check_ownership.sh`（我的地盘），`core/api/http/sessions.go` 那三行我一行没碰，
    只把证据报给主控转 backend。
  - **T-241 补记（09:02）**：三步照序做完 —— ①先立三个"必红"件（含新增的语法不合法写 `UPDATE quota_accounts: SET`）；
    ②双向断言补齐（六不红 / 三归位 / 一不被吞）；③**backend 08:45:43 已还原派生量**，于是"还原后的真代码"这一步做到了：
    改前那份脚本跑同一棵树 **exit 1（2 处）**、我这版 **exit 0（值位 4 处计数）**，逐行表在 Verification 08:56 那条。
    **并纠正一处被引用两次的归因**：`"/messages"`（前导斜杠）从来打不到"整个字面量等于表名"那条腿，
    当年那"2 处"实际是 `:81 case verb == "messages":` 与 `:115 return sub, "messages", true`。

- **09-24 08:0x~08:1x 轮（T-234 ③：把那道门从"我本地的做法"变成谁采样都逃不掉的）**：
  - **新增 `scripts/build_stamp.sh`（可归因戳，交付面）**：一次打印 `构建时刻 + HEAD + 工作树未提交处数 + 编译输入个数 + 编译输入 md5`；
    谁都能单独调（`bash scripts/build_stamp.sh`、换包传参、`--sh` 出变量）。**它只产读数、不立口径** ——
    判据在契约① 第 19 条验收② 的补句里（主控收编；**我自己复量过**：08:12:47 `grep -n "指纹" specs/contracts-internal-v0.1.md`
    ⇒ 3 处，第 408~411 行，原文含"一次 100 跑只有在**指纹证明确实带着修好的那份码**时才算数，**无指纹的 0 红不是证据**"，
    而它给的正当理由就是我在转述里那句"同一份采样在未修码上也可能全绿"）。
    清单取不到（< 50 个文件）就 **exit 2 拒绝给戳**，而不是给一个"看着像"的戳 —— 这条闸的由来写进了脚本注释：
    模板误写成 `{{$_.Dir}}` 时 `go list` 报错、被下游 `2>/dev/null` 咽掉 ⇒ 空清单 ⇒ **对空集求 md5 前后必然相等**
    ⇒ 一条永真的门比没有门更坏。
  - **`flaky_watch.sh` 每批自动在电池前后各取一次戳**（不用调用者记得）：两戳不同 ⇒ 这批**自己判"探测不成立" exit 2**，
    位置排在"红"那一支**之后**（红照旧是证据，但加一句"红落在哪一版说不清，只能当线索"），排在 SKIP/未复现之前。
    `--selftest` 13 ⇒ **14 条**（新那条喂"全绿 + 跨版本"的合成计数，断言它不许印成"未复现"）。
  - **两个对照都是端到端真跑，不是只测合成计数**（详见 Verification 08:09 两条）：
    正向 = 电池进行到 45 秒时往**我自己的** `crash_test.go` 末尾追加一行注释 ⇒ **40/40 全绿、0 SKIP**，
    而判决是「探测不成立 —— 编译输入前后不一致」、exit 2、`★跨版本：起=3d0bf9cd… 收=4ea87045…`；
    备份与还原在同一条命令里，事后回读"残留对照行=0"。
    反向 = 没人动码时两戳**逐字相同**（`803e0776…` == `803e0776…`）、判决照常给、exit 0、不误伤。
    这一正一反就是 T-234 那句"无指纹的 0 红不算证据"的形状 —— 顺手也回答了"这会不会一惊一乍"：
    它只在**真的改到我编译输入里**时才响。
  - **FL-9 行跟着补了第 ③ 条限定**（REQ-T19 文本已更新，主控镜像时以最新那份为准）：交回要带前后两次戳；
    并写明它与 FL-8 不重复 —— FL-8 问"窗口里有没有人在改"，这一格问"**改的是不是我验的那份码**"。
  - **对个账（两处，都按字复核过）**：
    ① 巡检 07:43 与主控那条"待投数 3 vs 4"之争我无话，记账在 T-235①；
    ② 巡检说"那道门目前还只在你本地做法里" —— **当时为真**（07:5x 的 grep 口径是 `core/test/`，
      而我当时确实只有 `.scratch/test/run_flaky_fp.sh` 一份）。现在它住在 **`scripts/`，不在 `core/test/`**：
      复量请用 `ls scripts/ | grep -i stamp` 与 `grep -c build_stamp scripts/flaky_watch.sh`（应 ≥1）。
      放 `core/test/` 反而不成立：戳必须在**被采样的那个进程之外**取，测试自己盖的戳证不了测试没被换掉。
  - **`lock.go` 现状按字重测（08:04:11 与 08:12:08，同一答案）**：工作树 ` M `、mtime 07:36:45、6808B、md5 `16e09a92`，
    `os.FindProcess` 只剩注释（第 27 行），第 33 行明写 `holderIsDead` 整体删除；**HEAD 里那份仍是旧码**（2466B、`dfd84639`，
    最后一次提交 `b91c537` 09-20 12:34）⇒ 我的说法保持为"**修法已落盘、尚未交账**"，
    而**不是**"REQ-L-1 未修"（那两个读数巡检已替我分清，我 07:3x 那句"仍是 `os.FindProcess`"当时为真、现在已过期）。
    **另一处对不上我不替谁圆**：巡检 07:41:36 读到的是 `mtime 07:38:37 / 8390B`，我 08:04 读到 `07:36:45 / 6808B` ——
    mtime 只会随写入前进，**更晚的时刻读出更早的 mtime 是不可能的**，除非有人用保留时间戳的方式换过文件；
    而我这边 `find` 全仓只有这一个 `lock.go`、没有 `.bak/.orig` 副本。两个读数都成立不了同一个事实，
    请巡检回一次**路径 + 命令原文**（我不据此说是谁错）。

- **09-24 07:1x~07:5x 轮（T-223 ②：量级判据落成 FL 表的一行并自证 —— 自证把我自己的工具打回炉了）**：
  - **交付物**：REQ-T19（契约② 判据表 FL 的 FL-9 行，文本可直接粘；**T-270 后表内编号是 FL-10**））。档钉 `single`、次数钉 **122**
    （从基线 4/65 的 **Wilson 95% 下界 2.42%** 反推，`n ≥ ln0.05/ln(1−p下界)=121.8`；上一轮我写的 50 是拿
    **点估计 6%** 反推的 ⇒ 那叫拿自己最不确定那一端当预算）。命令行当场验过能跑：
    `bash scripts/flaky_watch.sh -r TestCrashRestartSameDataDir -p ./test/ -m single -n 122`。
    绿样实测 **2.39s/次**（空机 291s/122 次）、红样 ~28s/次 ⇒ 122 次全绿约 5 分钟。
  - **★本轮最值钱的一条是我自己工具的假绿，被我自己跑出来**：07:33 那批 122 次里 **9 次返回 `--- SKIP`**
    ——`buildCore` 在 `cmd/core` 编不过时按设计 skip（"不拿桩绿冒充真跑"，用例没错），那 9 次**一行断言都没执行**，
    而旧 `classify` 照样印"实跑档 [single] 各 122 次都没红"。**一条能被"整批没执行"满足的验收判据不是判据** ——
    如果那天 REQ-L-1 已经修完、而 backend 恰好有一次全仓编译中断，我就能拿一份"122 次 0 红"替别人关掉验收。
    三条修法都进了 `scripts/flaky_watch.sh` 并各自带牙：
    ① 见 SKIP ⇒ **探测不成立 exit 2**，并明写"这批 122 次里有效样本只有 113 个"；
    ② 红与 SKIP 并存 ⇒ 判决照给 REGRESSION，但发生率按**跑过的次数**换算（"有效样本 38 次，发生率按 1/38 算"）；
    ③ FAIL 与 SKIP **都自带现场**（`│ SKIP 现场 …` 逐行印 `go test` 输出尾部）——这是 05:1x 那条 `io.Discard`
      教训的下一级：07:36 那批我采到过一次真红，而采样器只留下一个 "FAIL" 字，**我知道红过却说不出它为什么红**。
    `--selftest` 10 ⇒ **13 条**（条数仍由自增算出）。变异验双向：把限定语换成"这就说明已修" ⇒ 只有新那条红、
    旧的 `check_verdict 未复现` 原样 [ok]（**它只读判决第一句，对这件事是瞎的**）；把 SKIP 门改成 `> 999999` ⇒
    那两条红、且"未复现 —— 各 3 次都没红"这句**当场复活**，就是它能骗过验收的现场。两次变异都在同一条命令里
    `cp` 备份 + `cp` 还原，还原后逐项回读（`switch false` 0 处、两条 needle 各 1 处、备份文件已消失）。
  - **自证跑了四批，其中两批我自己判作废**（这是本轮第二值钱的：作废的理由写得出、不是感觉）：
    07:20 批 122 绿 0 红 —— **作废**：那台二进制编译进了别人**未提交**的在途码（`internal/process/lifecycle.go`
    是 `??`、07:17 刚落盘，`lifecycle_test.go` 07:21:17 又在我的窗口内被写）。所以那句"未修的代码也能 122 次 0 红"
    我**不发布**——它只在"`lock.go` 一字节未动（md5 `dfd84639`，最后一次提交 `b91c537` 09-20 12:34）"这一格里成立，
    其余部分不是同一版代码。07:31 批 113 绿 9 SKIP —— **作废**：编译指纹 `0ad0b4f8`→`3095b929` 前后不一致。
    于是取证脚本换了判法：**`go list -e -deps` 取出真正进我二进制的 .go 全集**（2331 个文件，仓库内 143），
    跑前跑后各取一次 md5 指纹，相等才承认这批可归因；并且**文件数 < 50 直接拒跑** —— 这一道闸救了我一次：
    模板我第一版写成 `{{$_.Dir}}`（`go list` 报 `undefined variable "$_"`，被我接的 `2>/dev/null` 咽掉）
    ⇒ 清单为空 ⇒ 指纹对空集算 ⇒ **跑前跑后永远相等**，一条永真的指纹比没有指纹更坏。
  - **REQ-L-1 在我采样的过程中被修掉了**（未提交，07:31~07:36 落盘）：`lock.go` 换成**端口当锁** ——
    `net.Listen(锁里记的端口)` 绑得上就判死并接管，绑不上打一次 `/api/v1/health`，
    答上来才是 `ErrAlreadyRunning`、答不上来是**另一条** `ErrPortTakenByOther`（`holderIsDead` 那套 pid 启发式整体删除、
    `-take-over-lock` 降级为"只喊一句已忽略"）。**验收① 的确定性反例也已经在盘**：`core/cmd/core/lock_test.go` 的
    `TestLockJudgesDeadByPortNotPid` —— 锁文件里写 `os.Getpid()`（`OpenProcess` 对自己必成功 ⇒ 旧启发式必然误判）
    配一个**空着的**端口（⇒ 新规则必然判死），正是 §1.1 第 16 条要的"真数据能红"那一腿。
    **我没有拿它当已验收**：它是 `??` 未提交、且它的用例归 backend 的包，等他们交账后我再跑量级腿。
  - **一条还没闭合的线索（我把它当观察，不当结论）**：新代码上带 pid 流失载荷采了两批 ——
    07:36 批 **39 绿 1 红**（指纹稳定、可归因，但那一次红**没有现场**，我的 ③ 号修法晚于它 8 分钟落地）；
    07:46 批 **60 绿 0 红**（同样可归因）。两批合计 **1/99 ≈ 1%**，载荷是三个循环各 ~128 进程/秒、
    两批分别 spawn 18,178 / 20,890 个真实 PE 进程。**机制我不知道**（现场丢了就不猜），
    两个候选都写在 REQ-T21 旁边交 backend：新方案下"上一代 socket 尚未随进程终止回收"会落到
    `ErrPortTakenByOther` 这一支，而那一支**今天没有重试**（`acquireLock` 只在 `holderFree`/`holderNoPort` 时重试）。
    下一次红会自带 stderr，不用再猜。
  - **我自己那句旧话按字回改了**（`crash_test.go` 的形状判定）：REQ-L-1 一落地，我那段
    "存活判定在 `lock.go` 的 `os.FindProcess(pid)` 上"就变成**指向一段已经不存在的代码**的假归因 ——
    同族旧账（归属被纠正后要回改自己包里的旧话）第三次犯到自己头上。改法不是换个说法，是把它提成
    `classifySecondBootStderr` 并配第 **56** 条顶层用例（4 个合成输入：两条 needle 各落一支、
    对不上必须**沉默**、两句话都不许出现会腐烂的实现符号）。两条变异各自精确命中：
    `switch false` ⇒ 4/4 全红；把第二条 needle 换成第一条 ⇒ 只有"端口被别人占着"那一支红、
    其余三支照旧绿（证明这张表不是"怎么改都红"）。
  - **另一处当场纠正**：我一度以为自己那条 `dropRecorderMounted` 门"测成 0 调用点"是坏的 —— 实际是
    **我这一轮的上下文摘要过期**：门在 09-23 02:40:32 就自己翻到"①已满足（找到调用点
    `Rec: newDropRecorder(l.store, adapterKey, l.now, l.logf)`，共 1 处）"，账本原话就在 Completed 02:4x 那格。
    今天复测 ② 仍不成立：生产码里 `CallTurn(` **调用点 0 处**（唯一命中是 `internal/process/connection.go:184` 的定义），
    所以 T84-7b 继续 SKIP，**不是我漏翻、也不是门不灵**。

- **09-23 05:3x~05:5x 轮（T-216：FL 的 `-run` 假命令列 + 量级判据我来写）**：
  - **主控点的 getopts 陷阱我复现成立**（05:30:58，本脚本）：`-run TestFoo -p ./test/ -n 1` 打印出
    `用例=un 主包=./cmd/core/`，`-p`/`-n`/用例名全落进位置参数 ⇒ **照旧文案抄的人永远拿不到读数**，
    而且它 fail-closed 成"探测不成立"，所以三年没人发现。改了 4 处文案（头部用法、`-h`、默认值注释、
    探测失败原因里那句"-run 前缀写错了"），**:221~:224 那四处内部 `go test -run "$RUNRE"` 一个字没动**
    **防混淆补一句（09-30，巡检提醒之后我复核）**：上面这个「:221~:224」是**本脚本自己的行区间**（四处把带锚的 `$RUNRE` 交给 `go test` 的调用点），与 `internal/process/lifecycle.go:224` **不是同一件事、不是同一份文件**；**而且这两个行号也已经漂了** —— 现读那四处在 `scripts/flaky_watch.sh` 的 **:437/:440/:443/:446**（脚本共 489 行）。⇒ 引这段的人别拿 :221~:224 当现读坐标，它只证明当年那次「只改文案、不改 go test 旗标」的口径。
    （那是 go test 的旗标，巡检也专门提醒过别改）。
  - **只改文案还会再犯，所以加了拒绝**：脚本收到 `-run` 或 `-run=` 直接 `exit 2` 并解释为什么
    （getopts 不认长选项 ⇒ 拆成 `-r un` + 一堆位置参数）。守卫放在 getopts 之前。
    真实退出码验过：`-run X` ⇒ 2、`-run=X` ⇒ 2（不是管道里 head 的 0）。
  - **量级判据要先有牙齿就得能单选档**：我按旧口径试 `-n 50`，**四档连跑 10 分钟没跑完**（05:32:19~05:43），
    而这次故障串行就能复现，多三档只花钱不产信息 ⇒ 加了 `-m single|shuffle|isolated|parallel|all`（默认 all，老行为不变）。
  - **`-m` 上线当场抓出我自己的三个 bug，全都是"跑过才发现"**：
    ① 默认值 `MODES=all` 被我插在 getopts **之后** ⇒ 把 `-m` 解析结果盖掉，`-m single` 照样四档连跑（05:49:31 实测）；
    ② `classify` 直接相加四档计数，而只跑一档时另外三档的变量**根本没初始化** ⇒ `set -u` 下
      `shuffle_BROKEN: unbound variable` 崩掉、**屏幕上没有判决、退出码 1**（05:50:39）——
      一个看起来像红的崩溃比漏判更坏，因为它会被读成结论；现在全部走 `${x:-0}`；
    ③ 判决文案写死"四档各 N 次"，只跑一档时那就是假话 ⇒ 改成打印**实跑档** `[single]`。
  - **还有第四个判决缺陷，是被自己的采样撞出来的**：`-m single -n 6` 抓到 1 红，分类器印
    "REGRESSION —— 这是稳定复现，派归属角色"。**6% 的事件在 single 档本来就会红一两次**
    （n=6 至少一红的概率 ≈ 31%），把 1/6 说成稳定复现会把人派去修一个不可复现的 bug。
    现在按 `x==N` 与 `x<N` 分两句：全红才说"稳定复现"，部分红说"间歇，发生率 x/N ≈ p%"，
    并明写"也别反过来记成 flaky"。selftest 三条新断言（3/3⇒稳定复现、1/3⇒间歇、**1/3 里不得出现"稳定复现"**）
    全绿；反向验（把 `single_RED == SAMPLES` 写成 `>= 0`）⇒ selftest exit 2 并精确喊出两行，还原后 0。
  - **`-m` 的两条新路径也都进了 selftest**（只跑一档时必须有判决），不是只在真跑时顺手验一次。
- **09-23 05:0x~05:1x 轮（T-201：巡检把一条我判过的红退回来让我判 —— 判完发现我 00:07 那条口径也偏软）**：
  - **先说结论**：`TestCrashRestartSameDataDir` 的红**不是 flaky、不是端口代理、也不是"撞上游改写不算证据"，
    是产品侧间歇：第二代 Core 偶尔拒绝起锁**。抓到现场的原始行（05:08:10、05:09:52、05:11:18 三次同形）：
    `轮数=166 文件被改写=false 口令换过=false 新一代沿用旧端口=false …
     第二代 stderr 尾部：core: cmd/core: 已有 Core 在跑同一个数据目录`
    ⇒ 第二代根本没写 endpoint，我的等待窗口只是把它的不肯起来读成了"超时"。
  - **机制候选（读码，不下判决，归 backend）**：`cmd/core/lock.go:66` 的存活判定是
    `os.FindProcess(pid)`，而它自己的注释写着"Windows：FindProcess 走 OpenProcess，进程已退就报错，这一条足够判定"。
    我的样本与这句话相反：被 `Kill()`+`Wait()` 收掉之后，`OpenProcess` 仍可能成功
    （pid 被复用、或进程对象还有别的句柄持着）⇒ 判"活着"⇒ 拒起锁。
    **我从外面分不开这两种**（pid 复用 vs 僵尸句柄），要说清得在锁判定里加一次 `GetExitCodeProcess`/
    `WaitForSingleObject(0)`；这条我不替 backend 做，只把两种可能都列出来。
  - **我自己的两处错，都记在这**：
    ① **00:07 那条"撞上游改写、不算证据"是把它当成了终点**。FL-8 的"窗口脏 ⇒ 不算证据"只该用来
      **禁止下结论**，不该用来**免除取证**——被验对象的自述（子进程 stderr）一直摆在手边，
      而我写的是 `second.Stderr = io.Discard`：**我把证据丢掉了，然后说"没证据可看"**。
      这一格与"非 0 命中也要读原文"同族：不接 stdout/stderr 就等于让产品替我沉默。
    ② 巡检的**端口假设我实测否掉了**：换代时"新 core 沿用旧端口"在 **55 次单跑**里出现 **0 次**
    （收紧后的诊断变量从 05:03 那批才存在，所以只算那 12+20+20+3 次，不拿 05:02 那 8 次充数）
      （诊断变量 `samePortAsOld` 现在只在"口令已换"之后才判，第一版它在 8/8 次里都真——因为第一轮读到的
      本来就是旧文件，那是一次我自己差点交出去的假统计）。
      不过**"拿端口当换代见证"仍然是错的判法**，已换成口令（`cmd/core/main.go:177~182` 每次 boot
      `crypto/rand` 现编 32 字节，不入库），并把"同端口发还"变成一条会打印的日志而不是一个会红的原因。
  - **09-24 07:2x 就地记账（T-223 ①：主控正式收回对我判据的那句指控）**：T-215 里主控怀疑"`crash_test` 断的
    是'端口换了'而不是'新一代就绪'，可能是这条红的原因之一"，我的 55 次单跑把它否掉 ⇒ **指控不成立，
    红因只有一条：`lock.go` 的 `os.FindProcess` 判活**。收回我认，但这里要顺手钉一句，**别让上面那个 0/55 被读错方向**：
    0/55 证的是"端口复用不是这条红的原因"，它**既不反对也不支持修法 (a)**。修法 (a)（端口当锁）的正当理由是
    **"把判活交给 OS、不再做任何 pid 启发式"**（绑不上 ⇒ 真有活的 Core；绑上了 ⇒ 前一个必已死），
    **不是**"避开端口复用"——我手里没有一条证据支持后一种说法，写下来免得下一个人拿我的 0/55 去反推 (a) 无效、
    或者拿 (a) 好传来反证端口会复用。我这边不改产品码，`crash_test.go` 的换代见证仍然用口令（那一格与选 (a) 还是 (b) 无关）。
    契约① §1.1 第 19 条的修法文本已按这个理由定稿（主控 07:0x 自改，我核对过原文）。
  - **失败信息现在自带分类**（`>>> 形状判定：第二代进程拒绝起锁…`），三种红分开：
    文件没改写=锁没回收；改写了但 health 不 READY=新一代没起来；改写+口令变+READY=换代成功（此时才轮到我判据的锅）。
    诊断辅助另加一个 `probeHealth`（非致命版）——我第一版拿会 `t.Fatal` 的 `health()` 做窗口内试探，
    结果"上一代端口暂时不通"这个**正常状态**把我的用例当场打死（05:01:43 那次 1.45s FAIL 就是它），
    **诊断不能有权力杀人**。
  - **采样口径要写清，否则 6% 会被读成"偶发"**：单跑 8+12+20+20+3 次 = **63 次、红 3 次**，
    加上整包 2 趟里各 1 次 ⇒ **4/65 ≈ 6%**；
    顺带给 FL 一条硬事实：**n=3 的四档流程对 5% 量级的事件本来就该报"未复现"**——
    我 00:05 那次 12/12 PASS 不是"已修"，是采样不够。
  - 待主控决定的两问（我不自己动契约）：①**FL 表缺"同档内自我矛盾"这一档**，我的提案见 Coordination REQ-T17；
    ②这条用例在 Wave-2 闸门里的位置——它是那道门的**路径本身**，现在它每 ~15 次真红一次，
    闸门绿不绿该由这个 6% 说了算，**我不为了让它好过而放宽 25s 或改成"重试到绿"**。
- **09-23 03:2x 轮（T-188：C8-14 落地，『来自』从"只打印"翻成判红）**：
  - **先认顺序**：主控 02:3x 那句"判据不许替契约发明语义"我没收回、也不该收回；变的是事实——
    §15.2.2 现在把双射写进了正文，所以我那条**收窄的前提消失了**。
    "先立语义、再上判据"这条已进契约① §1.1 第 16 条的收窄条款，我这边照着把注释改口：
    `parseCodeRows` 里那段现在写的是**这一列在此处仍不判重，但不是放行**——一对一由
    `TestAdapterCodeMapsToSingleHTTPName`（C8-14）专门判红并点名两行，解析器只负责把行身份 `namedAt` 留下来给它用；
    同时保留一句历史（02:3x 曾裁"只打印"），免得后人又把它改回去。
  - **"零误伤"这个数我自己量了**（T-188 点 2 要的就是别引第三方）：03:20:51 当场
    `§15.2『来自』带码名 17 行 / 去重 17 / `uniq -d` 为空 / `—` 行 13` ⇒ 今天转红不误伤任何一行；
    C8-14 首跑打印的就是这组数：`『来自』带码名 17 行、去重 17 个、`—` 行 13 ⇒ 一对一成立`。
  - **点 3 那句我按实回答：`—` 是"自然不出现"，不是"被排除"**。机制写在注释里：`parseCodeRows` 的
    `switch` 里 `strings.HasPrefix(from, "—")` 那一支先命中，码名那一支（C8-14 唯一读的地方）结构上收不到它。
    **因此我拒绝加一条"喂两行 `—` 进去不红"的合成用例** —— 那正是我上一轮自己删掉的那类零作为空判据；
    盘上现有 13 行 `—` 而本条绿，已经在真数据上覆盖了这件事。
  - 合成反例三组、绿红两边都有断言（各一次⇒干净 / 两行同码名⇒≥1 / 三行同码名⇒两两配对应≥3），
    外加一条"行身份数必须等于自称的行数"的自证（`namedRows != len(namedAt)` 就判自己坏了 —— 不然
    "点名两行"会点向不存在的行）。**反向验**（03:23:14，把配对内层循环短路成永不到达）：
    两条 dup 用例同时喊"合成反例失效：同码名两行/三行 本该至少报 1/3 条，实得 0 条"，而"各一次"那组仍绿；
    还原 `MUT-C814` 残留 0、`gofmt` 空、复跑 `ok`。
  - 一次自己写坏又当场改掉：我初稿在合成表里塞了个占位 hack（`{...[]httphttpNameHolder(), true, ...}` + 循环里
    `continue` 跳过），那是**编译不过 + 把一条判决藏进早退**的双重错（记忆 item：变异不得藏早退）。
    改成第三组直接写进表里，判决不藏在任何跳过后面。
- **09-23 02:4x 轮（T-183 三件 + 那条门第一次在真代码上自己翻支）**：
  - **②的门自己走了"①成立"那一支**：02:3x 后端把 `Rec: newDropRecorder(l.store, adapterKey, l.now, l.logf)`
    装进 `cmd/core/processpipe.go`（非测试码，全仓仅此一处）⇒ 我 02:40:32 复跑，
    `dropRecorderMounted` **自己**从"①装配点未接"改口成"前提①已满足（找到调用点 …共 1 处）"，
    仍然 SKIP。这是它第一次在真代码上走这条分支（之前只有合成件走过），所以这条门的"会随进度改口"是实测过的，
    不是我承诺的。
  - **但②不是"没测"，我把它测成了否定结论**：Core 生产码里**没有任何 `CallTurn(` 调用方**
    （`internal/process/connection.go:184` 是定义；其余命中都在测试与 handshake 的方法白名单里），
    而 `Registry.Probe`（`internal/adapter/probe.go:187`）是 `defer pipe.Close()` 的短连接、只握手不发
    `session/send` ⇒ 取证帧 `turn/claimed`/`turn/dispatched` 在生产路径上不出现 ⇒ 丢弃分支不可达。
    这段推理与自查命令一起写进 SKIP 文案（不带日期、不写现状数字，避免又埋一颗会腐烂的快照）。
    **反面也写明**：只成立一半不摘 —— T-175 禁的是"拿走不到的路径判绿"，半条路径判绿同样坏。
  - **①的收窄条款照要求补齐了"什么时候该转红"**：`parseCodeRows` 里现在写着
    "契约② §15.2 先落一条 REQ 明写一一映射，这条腿再翻成判红；在那之前只打印两个数"。
    **顺手量了决策成本**：现盘 §15.2 的『来自』列**没有一对多的实例**（17 行 / 去重 17、`uniq -d` 为空），
    所以真要双射，今天转红**不会误伤任何一行** —— 这一点交给主控，比我默默保留可见性更有用。
    另一半也量了：`eventjournal` 常量原始 63 / 去重 63 / 重复 0 ⇒ 我加的那句
    "现盘无此对，此为该类的合成反例"是有据的，不是 hedge。
  - **③卫生**：`LAST_UPDATED` 从 `2026-09-22T15:45` 改成 `02:42:27`（当场 `date -Iseconds`），
    并按 T-43 的写法注明它指的是哪一批重跑。**这笔账该我认**：正文写了 11 个小时，头部时间戳一次没动，
    巡检不说它就是错的。
  - **一条与"全仓面"有关的新事实，必须报**：02:43 那趟 `go test ./...` **不再 18/18** ——
    `cmd/core` 的 `TestProbeStaysUnavailableWithoutAdapters` 挂到 Go 看门狗超时
    （`panic: test timed out after 10m0s`，包退出 600.264s），T-132 那一族在 `cmd/core` 里回来了。
    **但我不据此判 backend 有死锁，理由要说准**：① 该测试文件 `core/cmd/core/probeassembly_test.go`
    此刻仍是 **`??` 未跟踪**（不是交付物），且 mtime 02:50:14 晚于我这趟跑的整个窗口 ⇒ 它还在被人写；
    ② 用 FL 时间线档复算失败时刻 02:43:19（前 300s / 后 90s）：扫 147 个 .go、
    **2 个落在窗口里，都是我自己的文件**（`errcode_mapping_test.go` 02:41:39、`turn_attribution_test.go` 02:41:25）
    ⇒ 按 FL-8 这份观察本身就不算证据。所以既不算 flaky 也不算回归，**我不写因果**；
    等它入册后我复采一次全仓面，那时还挂才轮到报"新的一条挂死"（并附它自己的栈）。
    我这轮自己的面：`core/test` 在两趟里都是 `ok`（3.850s / 3.530s）。
  - **02:56:53 复采：`cmd/core` 单包 `ok 2.606s`（那条挂没再复现）**，02:57:43~02:59:01 那趟
    `go test -timeout 240s ./...` 回到 **18/18 ok**；文件仍是 `??`，我不改口成"是 bug"也不改口成"已修"。
    顺手把这次教训装进工具：**`flaky_watch.sh` 的四个档位现在都带 `-timeout "${LUCERO_FLAKY_TIMEOUT:-180s}"`** ——
    别人的包挂 10 分钟，会把我的四档 ×N 变成 40 分钟干等；现在 180s 内拿不到结论就判"探测不成立"，
    让栈去说话而不是让等待去说话。改后 `--selftest` exit 0、真跑一档 4/4 PASS（02:57:22~02:57:33）。
    （第一版我把这条写成"撞在别人正在写它的窗口里"，是**推因果推重了**——02:50 那次改写在窗口之后，
    档上不支持那个说法；已改成只列档上能证的两条。）
- **09-23 02:0x 轮（T-178：§1.1 第 16 条的三条腿，一次补到 C8-12/C8-13 与 C8-10/C8-11 两处）**：
  - **第二条腿（重名可见）落在解析器里，不落在某一个用例里**：事件目录那侧新增 `expandCatalogRows(lines)`
    （`parseEventCatalog` 改为调用它）——累加**展开前的 token 数**、逐名记账，撞见第二次登记就报错并
    **点名到两个行文本**（首次在哪行、又见在哪行）。因为 C8-12 与 C8-13 共用这份解析，
    所以一条腿补上、**两条判据同时闭上**（01:48 那次 `project_archived` 双登：三方都是 63、第一腿全绿，
    只有这条腿看得见）。
  - **第三条腿（合成反例）喂得进解析器**：C8-13 里五组 `dupRows`（合法两行 / 合法合并行 /
    跨行重名 / 格内重名 / **合并行展开的名字撞上另一行的全名**）——最后一组是"只有展开之后才看得见"的那种，
    真数据里今天不存在，所以必须合成。
  - **同一型第二条腿回照错误码表**（第 16 条写的是"不分角色、任何从文档解析出集合再比对的判据"，
    所以没只做新判据）：把 §15.1/§15.2 的行解析拆成纯函数 `parseCodeRows(s151, s152)`，
    再喂五组合成行。**关键取舍写进代码**：`§15.1 码列` 与 `§15.2 左列` 判重复（两张都是"一行一个键"），
    而 **`『来自』列故意不判重** —— 两个 HTTP 码映射到同一个 Adapter 码是合法的一对多，
    判它等于替契约把映射写成一对一；改为**打印 `原始/去重` 两个数**（`左列 30/30（重复即红）、『来自』17/17（重复合法，仅打印）`）。
  - **两条腿各自反向验过**（备份与还原同条命令，跑完 `MUT` 残留 0）：把 `firstDup` 的阈值推到不可能
    （`seen[s] > 9`）⇒ `02:06:03` 当场红两条、且**过严守卫那一组仍然绿**：
    `第二条腿失效：§15.1 码列重名（第 16 条主案） 本该红却解析成功（named=[TIMEOUT]）`、
    `第二条腿失效：§15.2 左列重名 本该红却解析成功`。事件目录那侧的五组本身是双向断言
    （wantErr 真/假都有），所以"永真式"在这一族里无处藏。
  - **主控点的"哪一格"我按名字与红话分开**：我这条叫 `TestEventTypeTripleEquality`（在 `core/test`），
    db 那条叫 `TestCatalogMatchesContract` / `TestEventTypesInExpansion`（在 `core/internal/eventjournal`），
    红话一律点名两格（例：`[§6→yaml]：事件契约 §6 目录 里有 project_updated，core-v1.yaml 的 EventType 枚举 里没有`）。
    两边各加计数器不算重复建判据（一个包内快反馈、一个是 `core/test` 的门），我不替 db 改它的用例。
  - **③c 不再由我写**：01:53 已裁"只管人、不机检"，真正的损害（同名两次）由这轮的第二条腿拦。
  - 顺手去掉一处**会腐烂的现状数字**：C8-11 的报错文案里原写着"（今天盘上是 13 个）"——
    那是 §1.1 第 12 条那一族（现状快照第二天就假），改成只报实数、不写基线。
  - **回头查"三格是否真的同型"时发现第三格自己也有同一个盲区**：`eventjournal` 里
    **两个常量指向同一个事件值**在 Go 里合法、`sortedUniq` 吃得掉、集合比看不出来，但对词表是
    "一个类型两个名字"。于是把 `parseJournalEventConstants` 拆成"走目录 + 纯函数 `journalConstValues`"，
    喂三组合成行（正常 / 同值⇒红 / 空⇒红）。**三格同型才算闭上**，不是只在被点名的那一格上做。
  - **两条自己抓到的自我纠正，都记下来别粉**：
    ① 第一版合成件我写了 `const B = "b_y"` 单行形式，而 `journalConstRE` 行首要大写标识符 + `=` ⇒
    正则结构上不认，那组变成"零解析"假失败——**是我合成件错、不是解析器错**，被我自己的 `wantErr` 断言当场抓住；
    ② 改完之后我又加过一组"注释里同值不算"，随即发现它**是空判据**（`//` 开头的行结构上就进不了这条
    行首锚定的正则，剥不剥注释结果一样）⇒ 删掉，并在注释里写明"这一格不需要那个用例、为什么"。
    **空判据比没有判据更坏**：它会在交账里被读成"已覆盖"。
  - **三格同型的反向验一次做完**（把 `firstDup` 阈值推到 `> 9`，02:11:41~02:11:44）：
    §15.1 码列重名、§15.2 左列重名、两个常量同值**三条同时喊**（"第二条腿失效…"/"第三格的腿失效…"），
    而"一对多合法"那一组仍然绿 ⇒ 过严守卫同时成立。还原 `MUT-DUP3` 残留 0。
- **09-23 01:40~01:49 轮（T-172 第三条腿 C8-13 + T-175 把 7b 的触发条件写进代码）**：
  - **C8-13 落地 `TestEventTypeTripleEquality`**（名字沿用暂定名，契约行不必回写）：三格两两比四条边
    —— **事件契约 §6 目录 ↔ `specs/openapi/core-v1.yaml` 的 `EventType` 枚举 ↔ `eventjournal` 常量**，
    红话点名到"哪一格缺哪个类型"。现盘 **63 / 63 / 63，四条边全空**。yaml 那一格**只读不改**（归 frontend，
    codegen 也从它生成）；frontend 自己那条 `enum ↔ 目录` 守门保留不动（它还要给 codegen 用，两边不互替）。
    解析器按 §1.1 第 15 条的写法：锚点是**整行 `    EventType:`（并要求全文件唯一，出现两个就报错）**，
    再向前找 `enum:`、找收尾 `]`；四道失败出口全部报错不静默：锚点缺失、跨过下一格（`撞到了下一格 "Health:"`）、
    非 flow 序列形状、条目不像事件名。另有三格各自 `<20` 的下界（空集等于空集是这类判据最坏的假绿）。
  - **四条判决当场证过（合成件 a/b/c/d，01:46:30~01:47:07，原文
    `.qoder/evidence/test/c813-mutation-probe.txt`）**：
    a=删掉 yaml 里的 `project_updated` ⇒ 两条红分别点名 `[§6→yaml]` 与 `[常量→yaml]`；
    b=`enum:` 改名 ⇒ 解析坏（并正确喊出"撞到下一格 Health:"）；c=混进 `Not_A_LowerCase` ⇒ 形状错；
    **d=把枚举块整段倒序 ⇒ PASS 63/63/63（正向对照：集合比较不许被排版影响）**。
    合成件由 python 从 `    EventType:` 那一格定位生成 —— 我第一版 python 用"文件里第一个 `enum:`"
    抓到了 `enum: [HIGH, MEDIUM, LOW, UNKNOWN]`（第 772 行），**造出来的 d 是空变异**；
    出厂 Go 解析器没这个毛病（它是从锚点向前找，不是全文件找第一个），但这次是"探针自己失效"的第四次，
    已按 item 26① 的规矩改成**先证明变异真的落进了被解析区间**。真 yaml 全程未被改动
    （`git status --porcelain -- specs/` 空、md5 留档）。
  - **探针顺带暴露我自己一处输出卫生问题（已修）**：`a` 那一次在 FAIL 的同一段输出里还打印了
    "三格相等…四条边全空"——**绿味句子印在红当次**，而主控正是按我的 Logf 抄交账的。
    现在改成 `t.Failed()` 分支只打印计数并说"漂移已在上面点名"。修完复跑 a：红两条 + 计数行，无相等句 ✓。
  - **③c 这条腿不机检，理由写明（T-170/T-172 都授权这条"明写"）**：C8-13 比的是**名字集合**，
    ③c 约束的是 **payload 列**（合并只许用于 payload 相同的同族行）。就算某行违反 ③c，展开出的名字集照样对 ⇒
    要机检就得去解读 payload 那格的自然语言，那是**替契约读散文**，不是判据。这条留给主控/归属方复核
    （01:3x 那次是主控自己把三行拆开的，正是该由人做的那一步）。**我没有把它当成"已接上"来交账。**
  - **T-175 的两条触发条件写进了 7b 的门**：`Rec` 真被装配 + 真 Adapter 连接存在（probe/`:sync`）。
    01:41:40 现盘复量与主控 01:35 一致：`newDropRecorder(` 排除测试后原始命中 **2 处**，
    而 `dropaudit.go:38` 是函数定义、`processpipe.go:30` 是一句注释 ⇒ **真调用点 0**；
    我的门（跳 `func ` 定义行 + 先剥注释）判"①装配点未接"，与结论一致。
    SKIP 文案现在直接引 T-175 原话"**拿一条走不到的代码路径判绿，比 SKIP 更坏**"，
    并把"注入面"从前提改成**重开条件**（①②都成立后仍造不出矛盾帧，再开 REQ 讨论，不许为判据扩产品结构）。
- **09-23 01:26~01:31 轮（T-170：合成用例照 §7.1 写成"同一张表的成对判决"）**：
  - **巡检点 1 我复核成立**：`git show HEAD:core/cmd/core/dropaudit.go`（HEAD=`d814572` 01:13）里
    `:18` 那句带引号的注释原文**还在**，`:66` 的真写也还在 ⇒ 被裁的那次改写在**工作区、从未入库**
    （`git diff --stat HEAD -- 该文件` = 14+/36-，含 T-168 的代码改动）。所以成对判决就地可复现，
    不必跨到 `2529d5b`——我 01:14 那份 `2529d5b` 复证仍然有效，但**下一个人照 HEAD 跑更省事**。
  - **判决由出厂实现算，不由我的副本算**：新脚本把 `W13_RE` / `strip_comments` / `w13_hits` 三段
    用 `sed` 从 `check_ownership.sh` 里抽出来 `source` 之后再跑（**不另写一份匹配器**——
    那证明的是副本，不是交付物）。结果：原始 grep 命中行号 = **18 21 66**，经出厂 `w13_hits` 之后
    **只剩 `:66`**。原文冻在 `.qoder/evidence/test/w13-head-dropaudit-pair.txt`（含取样时刻与 HEAD 短码）。
    **交账里写清用的是哪一份**（T-170 点 2 要的就是这个）：**成对判决 = HEAD 版**；工作区版上
    `:66` 已被 T-168 拿走 ⇒ 只剩"注释不红"这一半可判。
  - **合成用例改成 §7.1 的字面口径**：`comment.go` 现在同时装 `Append("tasks"` 与 `Append("audit_entries"`
    两种注释复述，而 `agg.go` 里是**同一张表 `tasks` 的真写** ⇒ "同表两角色"成对：真写判红、注释不红。
    再加 `trailing.go`（行内注释之后的真写⇒红）防"剥过头"。三条子断言 + 一条成功时也打印的
    `[W13 注释自证·成对]` 行。**反向验**（01:28:48）：把 strip 管道换回 `cat` ⇒ `rc 0→2`，
    喊出 `命中 4 处，期望 2` 与 `只在注释里出现的 Append("tasks" / Append("audit_entries" 被 W13 判成写调用`；
    还原后 `rc=0`、`MUT` 残留 0。
  - **T-170 那句"不要因为 backend 复原注释再调整匹配器"我照做**：修的是**匹配器**（跳过注释），
    不是任何人的注释。所以 backend 恢复 `:18/:21` 之后这条腿的行为不变——上面那份 HEAD 版判决就是证据
    （HEAD 里注释是原文，匹配器只报 `:66`）。
- **09-23 01:19~01:22 后续（T-166 正式投递到达时我已做完；这条绿不是我把信号弄哑）**：
  - 巡检在投递里补的那句"前提漂移"我自己先撞上了：01:14 我想拿当前工作区复现那条假红，
    `grep audit_entries` 已经 **0 命中**（backend 01:06:08 用改注释消掉了字面量）——所以我才去
    `git show 2529d5b:` 取恢复版做复证。**同一条规矩第二次成立：报别人红之前先用 mtime 判在途**，
    这次反过来也成立：**我自己复现别人的断言，也要先判它是不是已经被改掉了**。
  - **01:19:52 backend 按 T-168 改了代码**：`RecordStart` 现在是
    `r.logf("REQ-DB-2 未落：生命周期那格没建行 …")`，与 `RecordExit`/`RecordCrash` 同一形状（喊、不写）。
    ⇒ `process_instances` 那处真越界**离开树了**，我的 lint 因此 **exit=0**、`[W13-2] process_instances: 0`。
  - **这条绿要能被反查，我不留口头账**，三条证据链：
    ① 修完 strip 之后、代码改动之前，01:12:44 实跑仍是 exit=1、3 条 VIOLATION 全部只关于
      `Append("process_instances")`（原文在 `.qoder/evidence/test/ownership-lint-final.txt` 的上一版里，
      判决行抄在 Verification 01:17 那条）；
    ② 01:14:25 对恢复版（`git show 2529d5b`）跑同一条管道：原始 3 命中 → **剥注释后剩 `:66` 一处**
      （`.qoder/evidence/test/w13-restored-dropaudit.txt`）；
    ③ selftest 的合成反例常驻：`[W13 注释自证] 合成树 2 处命中 = agg.go(真写) + trailing.go(行内注释之后的真写)；
      comment.go(注释里复述本规则) 不红`，且把 strip 摘掉后 selftest rc 0→2 会精确喊出那两条（01:11:12 实测）。
    ⇒ **如果有人 checkout `HEAD=d814572` 跑我的 lint，它会红**（那一版的 `:66` 还在），这是预期行为。
  - 顺手复采：`go test ./cmd/core/` 01:22:44 **ok（2.555s）**、`go test ./...` 01:22:53 **18/18 ok**
    ⇒ backend 那一改是自洽的（他们的 `dropaudit_test.go` 没有硬要求那行落库）。
- **09-23 00:5x~01:1x 轮（T-163 观察面裁定 → T84-7b 的门；T-166 一真一假两个 W13 越界）**：
  - **T-163 裁定照办**：`Recorder` 不给导出、留在 `package main`；T84-7b 走**黑盒**（起真 Core + 数
    `audit_entries`/`event_journal` 两张表的行数差）。我把那格从"等人回话才翻"改成**门自己判**：
    `dropRecorderMounted(root)` 扫 `cmd/core` 的**生产码**（排除 `_test.go`、排除注释、排除 `func ` 定义行）
    找 `newDropRecorder(` 的调用点；没挂上 ⇒ SKIP 并点名三段前提，挂上了 ⇒ **仍然 SKIP，但理由换成
    "①已满足、还差②活连接与③注入面"** —— 一条会随别人进度自己改口的 SKIP，比一条要我重新想起来改的注释强。
    **两个分支都当场证过**（01:00:42 双向对照）：真实树判"①装配点未接（7 个生产 .go 里 0 处调用点）"；
    把 `dir` 临时指向 `.scratch` 里一份合成"已装配"文件 ⇒ 判"①已满足（找到调用点
    `return newDropRecorder(store, "codex", now, logf)`）"。跑完 `MUT-GATE` 残留 0、gofmt 干净。
  - **我自己在这条门上重犯了一次同族错**：第一版把**定义行** `func newDropRecorder(...)` 当成了调用点，
    门当场报了个假"①已满足"。这正是我在 C8-11（`writeError`）专门为它写过的守卫 ——
    **同族错误第二次，且第二次犯在"新写的提取器没复用已有那条规矩"上**。已加 `strings.HasPrefix(trimmed,"func ")` 跳过，
    并把这件事写进函数注释。
  - **T-166（一真一假）修的正是我自己的一条腿**：`audit_entries` 那条来自
    `dropaudit.go:18` 的**注释**（"这里出现 `Tx.Append(\"audit_entries\"` 字面量即 W13-2 红"）——
    代码在用注释复述本规则，被规则本身判成违反。根因不是 strip_comments 坏了，是
    **`w13_hits` 这条腿根本没接它**（同一文件里 check_2 / C8-5 / 常量腿都接了，只有 W13 没接）。
    修：`w13_hits` 现在 `| strip_comments | grep -vE '^[^:]+:[0-9]+:[[:space:]]*$'`（后半道是防"整行都是注释"
    留下空文本行，被下游判成"命中却解析不出表名"的另一种假红）。
  - **T-166 要的"两个判决都打出来"落地了**：`--selftest` 里喂三种形状
    （`agg.go` 真写 ⇒ 红、`trailing.go` 行内注释之后的真写 ⇒ 红、`comment.go` 注释里复述规则 ⇒ 不红），
    计数断言 `wn == 2` + 两条子断言，另加一条**成功时也打印**的 INFO 行，免得"没输出"又被读成"没判"。
    **反向验**（01:11:12）：临时把 strip 管道换回 `cat` ⇒ selftest **rc=2** 并精确喊出
    `[SELFTEST FAIL] W13 模式命中 3 处，期望 2` 与 `只在注释里出现的 Append("audit_entries" 被 W13 判成写调用`；
    还原后 rc=0、`MUT` 残留 0。
    （中途一次我用 python 子进程跑 selftest 看到 rc=1 就以为"证明成立了"，其实是 **python 自己的
    gbk 编码在 print 中文时崩了** —— 判决要落在被验对象自己的产物上，不能落在包装脚本的退出码上。）
  - **T-169 那半件事我也跟着验了**：backend 被要求把 `:18`/`:21` 两行注释恢复原样，我从
    `git show 2529d5b:core/cmd/core/dropaudit.go` 取**恢复版**跑匹配器：原始命中 3 处
    （`:18` `:21` `:66`）⇒ 剥注释后只剩 **`:66` 一处**（`tx.Append("process_instances"`）。
    原文与时刻在 `.qoder/evidence/test/w13-restored-dropaudit.txt`。**结论：他们恢复注释我不会再误报。**
  - **lint 今天 exit=1 是有意的，不是坏了**：三条 VIOLATION 全部只关于 `process_instances`
    （`:66` 真写调用，归 T-168/backend）。`audit_entries` 那条假红已经消失、且不会靠"叫 backend 删注释"消失。
    在我把这条写进账本之前，主控验收时请照这个口径看：**绿了才说明我把真信号弄哑了。**
- **09-23 00:1x 轮（T-153：第 15 条是通用口径，先回照我自己）**：主控把"命令要 `^全名$` + 显式包路径"
  升级成**契约① §1.1 第 15 条**，所以第一件事是查**我自己**写的命令有没有违规，而不是等着别人来提。
  - **实测把这条的"为什么"钉死**（00:12:22）：`go test -count=1 -v -run TestDroppedFrameReaches ./test/ ./internal/process/`
    ⇒ **两条近名用例都被命中**、两份 `--- PASS` 来自两个包（谁的绿记到谁头上全靠运气）；
    换成 `-run '^TestDroppedFrameReachesAuditSeam$' ./test/` ⇒ 只命中我这条，
    且**两个子用例照常跑**（7a PASS 0.89s、7b SKIP）——顺带回答"锚定会不会把子用例掐掉"：不会。
  - `scripts/flaky_watch.sh`：新增 `RUNRE='^'"$RE"'$'`，四个档位的 `-run` 都改用它；
    **`RE` 本身必须保持原样**——probe 靠它去匹配 `--- PASS: <名字>` 那一行，
    我要是一时省事直接把 `^…$` 拼进 `RE`，那 4×N 次采样会**全部判成"探测不成立"**（exit 2）。
    这个"加固自己却把自己弄瞎"的形状写在脚本注释里了。用法行同步注明"`-r` 收用例名、不收正则"。
    复跑：`-r TestDroppedFrameReachesAuditSeam -n 1` 四档 4/4 PASS、探测失败 0、`--selftest` exit 0、`bash -n` 干净。
  - `scripts/crash_recovery.sh`：`-run 'TestCrash' ./test/...` ⇒ `-run '^(TestCrash[A-Za-z0-9]*)$' ./test/`
    （整族是故意的，所以写成锚定的族形而不是裸前缀；裸前缀明天会被别人加的 `TestCrashRecoveryXxx` 静默并进我的清单）。
    改后实测选择集不变：**3 PASS + 4 SKIP 共 7 条 `TestCrash*`**、exit 0。
  - **删掉我自己留的那份表文副本**：REQ-T14 下面那个 ```markdown 围栏里还贴着 C8-6~C8-9 四行可粘文本，
    而它已经滞后——里面 C8-8 还写着 29/12，契约正文今天标的是"已作废、被 C8-10 取代"。
    这条规矩是我自己 09-22 写下的（"契约有表还留副本就是第三处真相"），今天轮到我自己执行。
    删除用 python 做整块替换，**两个 assert 先行**（含 `| C8-6 |` 的围栏块必须恰好 1 个、且每个 ``` 都有闭合行），
    删后复验 `grep -c '^## '` = 11、`| C8-6 |` 命中 0。REQ-T14/REQ-T15 两条同时标 **已办结**。
  - **主控对我要的那个口径的裁定记在案**：非字面量传码 **不升红**——"把码改成常量/变量传"是合法 Go 风格，
    为它开红等于惩罚合法重构；真正的不变量用**类型**收口比用 lint 收口强。
    **重开条件**写进 C8-11：该计数从 0 变 ≥1 时动类型或明写例外，不许回来把这条改成红。今天仍 0 处、仍只打印。
- **09-23 00:0x 轮（T-151 巡检①：契约① §6 的展开约定改了口径，我跟着翻一次判据）**：
  原文把 ③ 从"本行**第一个**全名"改成"该简写之前**最近一个**全名"，并新加 **③b：合并格只许
  「全是全名」或「一个全名 + 若干简写」两种形态，混排（假想 `a_b` / `c_d` / `_e`）是表格坏了、
  解析器必须报错** —— 这一条把"第一个还是最近一个"的歧义**取消**而不是裁决（`:221` 明写）。
  - **我这份实现从第一天取的就是"最近一个"**（`default:` 分支每个全名都重写词干，与 db 同形），
    而 §6 今天没有能区分两种读法的行 ⇒ **盘上结果零变化（仍 62/62）**。所以这次不是"我错了"，
    是**契约新要求一条我原本没有的守门：③b 混排必须报错** —— 我原来会静默挑一个词干把它读过去。
  - 落法：`expandEventCell` 加两个守门（简写前已见 ≥2 个全名 ⇒ 错；简写之后又出现全名 ⇒ 错），
    外加"全名里没有 `_` 却跟了简写"也报错；展开自证表加两行混排反例，并把"整行都是全名"那行
    的 why 改成**"守门不许顺手把形态一也拒掉"**（另一半对照，防我把自己写宽）。
  - **两条守门都当场证明会咬**（MUT-3b-A/B，00:02:26~00:02:41，备份与还原同条命令）：
    关掉后 `a_b / c_d / _e` 静默展开成 **`[a_b c_d c_e]`**（取了最近词干 `c_`）、
    `a_b / _c / d_e` 展开成 **`[a_b a_c d_e]`**（取了第一个词干 `a_`）——
    **这两个结果本身就是那条歧义的现形记**，比"契约说不许混排"更有说服力，已抄进报告。
  - 第一次探针我图省事把两处条件写成 `case false:` / `if false {`，结果 `sawShort` 变成只写不读、
    **包根本编不过**，而我那条 grep 只匹配 `^--- ` ⇒ 屏幕上什么都没留下，差点被我记成"探针没反应"。
    记成规矩：**关掉分支要让被关的变量仍被读**（`x > 99` / `x && false`），
    且探针的 grep 必须同时兜 `build failed`/`declared and not used`，否则"没输出"会被当成"没红"。
  - 同轮把 `expandEventCell`/`parseEventCatalog` 头上那两处**旧口径措辞**（"本行第一个全名"）改成
    与契约① 一致，并注明"这条注释跟着翻一次而不是猜一次"。
- **09-22 22:5x~23:2x 轮（账本第五十批 T-135 三件 + 我在账本里读到的 T-141 第一段）**：
  - **C8-10 落地，C8-8 的字面数闸门一并废掉（取代、不是并列）**：`TestErrorCodeContractTablesParse`
    删掉 `29/17/12/17` 四个常量，改判**形状**——①`码名行 + —行 = 总行数`；②码名集合与 §15.1
    **双向相等**；③下界 `—≥1`、`总行数≥20`（防"锚点命中别处 ⇒ 静默空集"）；④三个数字照旧 `t.Logf`
    打印，漂移看得见但不咬人。命令不变（还是 `-run TestErrorCodeContractTablesParse`）。
  - **①里藏着一个我自己造的洞，是探针逼出来的（M-C）**：第一版把某行左列的反引号拿掉之后
    三个数**一起少 1、彼此依旧闭合** ⇒ ①一声不响，只有 C8-11 因为"代码在发的码从表里消失了"才红。
    求和式因此是条构造上碰不到的兜底。补了 `skipped` 计数器：**长得像表格行却没被计入任何一类 ⇒ 判红**
    （M-C/M-C2 两例现在各咬得住，`静默少读一行比读错一行更坏`）。
  - **C8-11 落地 `TestTransportCodesAreInContract`**：扫 `core/api/http/**` 非测试 .go（4 个），
    13 个字面码全部 ∈ §15.2 左列（30 个）。三处不显然的取法都写在函数头上：先去 `//` 与 `/* */`
    注释再找（M-I 里注释内那条 `writeError(..., "comment_only_code", ...)` 确实没被算成证据）、
    跳过 `func writeError` 定义行（今天 `非字面量传码 0 处` 就是这个跳过生效的旁证）、
    **按顶层逗号切实参取第 3 个**而不是拿一条正则整行套（第 4 个实参是人话消息，贪婪匹配会抓错）。
    **口径边界写明**：码若改成常量/变量传进来这条腿看不见 —— 只打印不判红（超出契约加严是我今天
    栽过的错），要判红得契约加一条（REQ-T15 里提了）。
  - **C8-12 落地 `TestEventCatalogMatchesJournalConstants`**：契约① §6 目录 ↔
    `core/internal/eventjournal` 常量集合，**双向**判；今天两侧各 **62**、两个差集都空 ⇒ **不红**。
    主控预期"第一天红在 `notification_read`"没有发生，原因不是判据钝：db 23:10 那版（`5d6ac66`）
    已补齐 `NotificationRead` 与 `task_paused`/`task_blocked`/`task_resumed`/`execution_paused`
    五个常量，并且**自带了一条包内对拍** `TestCatalogMatchesContract`（我 23:10:05 带 `-v` 复跑：
    62 类、PASS）。两个执行者的观察面不同：那条比 `catalog` map、我这条比 `const` 声明，
    合起来才盖住"常量与 map 各漏一个"的形状。
  - **契约① §6 的"合并行机械展开约定"（`3a71ea7` 23:19:52 落文）我这边已合规**：①~⑤ 逐条对过，
    特别是 **③词干取最后一个 `_`** 与 **⑤孤儿 `_xxx` 必须报错、不许静默跳过**（db 的 `eventTypesIn`
    今天栽在⑤，被派 T-142）。C8-12 里加了 6 行展开自证表把这套约定钉进判据
    （含假想行 `execution_output_flushed / _failed ⇒ execution_output_failed`，正是主控说"两种读法会分歧"那条）。
  - **T-141（我在账本里读到、还没投递到我手上的一条）第一段落地**：不再把 REQ-T12 当整条阻塞。
    `TestDroppedFrameReachesAuditSeam/T84-7a` 今天能跑：反例先断（一致那一发 ⇒ 接缝 0 条记录），
    正例断矛盾那一发留下 **2 条** `DroppedEvent`（`turn/claimed` + `turn/dispatched`），
    每条带齐**帧里的 id**与**票号绑的 id**两个不同值 + `turn_ref` + 非空 `Reason` + `Kind`。
    `T84-7b`（一行审计 + `event_journal` 零新增）**显式 SKIP**，点名 `cmd/core` 里那个
    `process.Recorder` 实现（backend 23:0x 在途），并把三条待断言的形状写在注释里。
    `Supervisor.Rec` 是 nil-safe 的第四方法，我自带一个只记账的实现 + `var _ process.Recorder` 编译期钉子。
    **一次差点判错**：日志里"生命周期三方法被调 1 次" —— 我原本想顺手断"丢弃不该碰生命周期"，
    那句依据是 adapter 写在接口注释里的、不是契约，而且实测确实调了 1 次 ⇒ 断言会假红；改成只打印。
  - **变异探针 12 例（副本落 `.scratch/test/mutroot/`，业务码与契约原文零改动、也不参与编译）**：
    M-A/M-B/M-C/M-C2/M-D/M-E/M-F/M-G/M-H/M-I 全部 FIRED，**M-J/M-K 两条正向对照 SILENT（=全绿）**。
    两条对照是这轮最值钱的：M-J 往 §15.2 表里合法加一行 ⇒ C8-8 那种字面量闸门必然假红，
    形状闸门报 `31 行（码名 17 / — 14）` 放行；M-K 表与代码同时补 ⇒ 两条腿都认。
    探针自己也翻过两次车，都记下来：①第一版把新行 `append` 到**文件尾**，落在 §15.3 之后，
    判据根本看不见 ⇒ 那是**假对照**（M-K 当时"期望不符"就是这么来的）；②M-B 第一版只替换 `—` 前缀、
    把说明文字留在格里 ⇒ 撞成第三种形状，走成了 M-A，②那条腿当时没被单独判到。
    T84-7a 另做两例（只改我自己文件）：抹平矛盾 ⇒ `接缝 0 条记录` 那条 Fatalf 咬住（2.91s）；
    把字段期望换成绑定 id ⇒ 逐条记录各红一次。两例跑完 `diff` 与备份**逐字节无差异**。
- **09-22 15:2x~15:3x 轮（T-131 三件 + T-124 落点）**：
  - **① T84 表已镜像 ⇒ 指针翻转**：`core/test/turn_evidence_test.go` 的文件头七行表删掉，换成
    "判据见契约② §4.2 判据表 T84"+一句教训（契约改一次判据跟着翻一次）；账本 `Coordination` 里
    那份 T84 交本同步删除。T84-7 的三段归属按契约执行（adapter 加 `Recorder` 的"丢弃"事件、
    backend 在 `cmd/core` 注实现、我落判据），我这边仍是 `待建`——**但契约现在钉了一条我没实现的禁令**：
    "审计行不许在 `process`/`cmd` 里出现表名字面量"。这条归 lint（我的 `check_ownership.sh` 正好有
    按表名扫的机器），已作为 **C8-9/新判据**列在下面 ② 的交本里一起交。
  - **② 新判据：§15.1 码集 ↔ §15.2『来自』列，Go 侧执行者补上了**。新文件
    **`core/test/errcode_mapping_test.go`**，三个各自独立的顶层用例、**全程只用 `t.Errorf` 不用 `t.Fatal`**
    （理由见下面 T-124 那条——被父级 `t.Fatal` 吞掉的子用例今天咬过 audit）：
    - `TestErrorCodeContractTablesParse`：解析器自证。守门数取自主控给的口径，且 15:22 在盘上实测对上：
      §15.2 表 **29 行 = 17 行带码名 + 12 行以 `—` 开头**；§15.1 码列 **17** 个；两者加起来 ≠ 总行数 ⇒ 红。
    - `TestHTTPErrorCodesComeFromAdapterSet`：正向，§15.2 引用的每个码名必须在 §15.1 里。
    - `TestAdapterCodesHaveHTTPMapping`：反向，§15.1 每个码都要有 HTTP 映射行；
      **例外按形状判**（『来自』以 `—` 开头的 12 行不参与），不点名放行 `version_conflict`。
    - 锚点用标题文本（`### 15.1 ` → `#### 15.1.1`、`### 15.2` → `### 15.3`），找不到锚点或区间没有终点
      ⇒ 直接报"解析器坏了"，**拒绝把空集当结论**（主控说的那四个坑我这边第一个就撞上过）。
    - **守门今天真的咬了我一次**：第一版按 awk 的列习惯写 `cells[4]`（split 之后 `来自` 其实是 `cells[3]`），
      解析器当场报"§15.2 第 1 行的『来自』既不是码名也不是 `—` 开头" ⇒ 三条用例全红。
      **这条红就是"守卫不是永真"的实测证据**，不用再造变异。修完三条全 PASS（15:3x）。
    - lint 里 check 8 的 C8-4 早就是同一对集合的 bash 执行者。两处并存我在交本里写明主从：
      **Go 用例是主**（`go test ./test/...` 是所有人的默认入口），lint 那条降为镜像提示。
  - **③ FL 表逐条对过（脚本没骗表）**：契约里 FL-1~FL-8 与 `scripts/flaky_watch.sh` 的分支一一对上；
    FL-8 要求的"报扫过的文件数"在两个分支里都印（`扫了 122 个 .go 文件…`）✓。
    `flaky_watch.sh` 已由主控 `5ccd1a0` 补提交入册（`git ls-files scripts/` 现在含它）——
    我这边接手盯住它不再漂回未跟踪：每次交回都跑 `git status --porcelain -- core/test scripts`。
  - **④ T-124 那族新错的落点**（规矩第 22 条，我自己这边的处置）：
    - 落点一：`core/test/errcode_mapping_test.go` 文件头第 12~14 行——三条判据**各自独立顶层用例 + 只用
      `t.Errorf`**，就是为了不在父级掐掉兄弟子用例；
    - 落点二：本文件"验证规矩"里新增一条对账命令（"包 ok"不算跑过）：
      `grep -hc "^func Test" core/test/*.go` 之和 与 `-v` 输出里顶层 `--- ` 行数必须相等。
      09-22 15:3x 实测 **50 = 50** ✓。**这条规矩今天以另一种形式咬了我一次**：我 `-run` 里把
      `TestHTTPErrorCodesComeFromAdapterSet` 误打成 `...Adapterset`，那条用例整条没跑而包照样 `ok`——
      是我按新规矩数 `=== RUN` 才发现的，不是我本来想看的。
    - 落点三：后半句按巡检的建议**并进同一节**（不另开一条）：**任何计数都必须配原文行**——
      计数只证明"数量对"，原文行才证明"是那条"。
  - 交付状态（09-22 15:44:16~15:45:28）：六条命令全 exit 0；默认面 50 个顶层用例声明数 = 顶层结果数 50，PASS 计数含子用例；开旗标 65 条 PASS / 9 顶层 SKIP / 0 FAIL。
- **16:06~16:16 轮（T-121 + T-124：时间线核对成为 FL 的前置档，另纠正我一句错标）**：

  - **`flaky_watch.sh` 加了 `-a <失败时刻>` 一档（只核对时间线、不跑测试）并全仓扫**：
    `TL_DIRS=(../core ../specs ../scripts)`，报**扫过的 .go 文件数**，命中改写窗口就判
    "既不计 flaky 也不计回归，先请改动方交还原后的复采"。这一档就是 T-121② 要 backend 用的那个方法，
    用法一行：`bash scripts/flaky_watch.sh -a 2026-09-21T15:29:31+08:00`（默认全仓，可选位置参数换范围）。
  - **我自己 15:58 那次判断被这台机器直接翻案了，认**：当时我只扫 `cmd/core` 与它 import 的那几个包，
    判"15:58 不是变异窗口"。用新的全仓默认重跑 15:29:31 那一次 ⇒
    `cmd/core/core_test.go` mtime **15:30:25（失败后 54s）**、`cmd/core/rejections.go` **15:30:29（失败后 58s）**，
    判决"观察落在改写窗口里（2 个文件），既不计 flaky 也不计回归" —— 与 backend 第 123 条自陈的
    变异电池时间线**独立对上**（他们是 15:29:51→15:31:13，我这个只读 mtime 就撞进了同一段）。
    规矩落在工具里而不是靠我记得：**判"不是变异窗口"必须等价于"全仓扫过"。**
  - **T-124② 的记账**：`cmd/core 16:10:54` 那次（`-count=2` 两跑全红、16:11:06 无 flag 绿）
    = **FLAKY（同二进制并发）**。这正是我 FL 表里 **FL-3** 那一行（`shuffle` 红而 `single` 绿），
    16:15:31 那次 5 样本 PASS 也符合"概率性"这一性质 —— **这条判据第一次由实测落地，不是我把话搭上去**。
    最小复现手段（`-shuffle=on` + `-count=2` 同包内跑）已经写在 FL-3 的命令列里，backend 可直接用。
  - **T-124① 纠正我写错的一句**：`grep -n "整条要" core/test/*.go` ⇒ 命中的是
    **`adapter_contract_test.go:827`**（不是投递交给我的 `ws_cursor_contract_test.go:101`；那行是 F20 的
    指针段，里面没有旗标相关的话）。那句"整条要 `LUCERO_TEST_FAKE_ADAPTER=1`（默认 SKIP…）"确实错了：
    它写在覆盖 MS-1~MS-6 的段落里，而 **MS-6 常跑、不 Skip** —— 后果不是措辞难看，是下一个人会把
    MS-6 的绿也算成"没跑过的那一半"。现已改成"旗标只管 MS-1~MS-5；MS-6 不碰旗标"，并注明我这次误标的教训。
  - 复跑（16:14:34 与 16:16:0x）：`-a` 档两次（全仓 0 命中改写窗口，扫过的文件数已打印）、
    `-run TestModelSelectShapeMatchesContract ./test/...` ⇒ PASS（确认它不依赖旗标）、
    `flaky_watch.sh --selftest` ⇒ 六句判决各就各位 exit 0。四条验收见 Verification evidence 首条。

- **15:50~15:59 轮（T-119 test 这一半：把"间歇红"落成判据）**：
  - **新交付 `scripts/flaky_watch.sh`**（判据本体在契约② 判据表 FL，脚本里只实现不另立口径）。
    四个档位，专门为了**分辨**主控 15:55 点出的两种形状（②那一维我加进去了）：
    `single`（单独跑一个包一个用例）/ `shuffle`（同二进制内 `-count=2 -shuffle=on`）/
    **`isolated`（多包一起跑但 `-p 1`：去掉跨包并发、保留包内 shuffle）** /
    `parallel`（不限制 `-p`，即 `go test ./...` 的默认）。
    判据要求"`-run` 命中的那一行 `--- PASS/FAIL/SKIP`"，**不取包级 ok** —— 不带 `-v` 时 SKIP 看不见、
    包照样报 ok，那正是这次要防的假绿。每次采样前取一次 `date -Iseconds`（无时刻的数字不算证据）。
  - **分类器带反向用例，不需要等真红一次**：`bash scripts/flaky_watch.sh --selftest` 喂六组合成计数，
    六句判决各就各位（15:57:41 实测全 `[ok]`）：REGRESSION / FLAKY（同二进制并发）/ FLAKY（跨包并发）/
    FLAKY（未定维度，parallel 与 isolated 都红——这种就不许怪"一起跑"）/ 未复现 / 探测不成立。
    **`探测不成立` 那一档是 fail-closed**：任一采样没拿到该用例的 `---` 行（编译不过、用例改名），
    脚本 exit 2 并拒绝下结论 —— 否则"别人的包此刻编不过"会被我写成"未复现"或"已绿"。
  - **对本次间歇红的实测（判决：未复现，按口径**不写成已绿**）**：
    `15:57:55→15:58:05` 四档各 2 次，全部 PASS（8/8）；
    再补一次最接近主控给的那个形状的全仓跑：`15:58:27→15:58:57 go test -count=2 ./...`
    ⇒ `cmd/core 3.036s ok`（那个用例没红），但 `internal/adapter` 报 `[setup failed]`
    （`contract_table_test.go:139:49: unknown escape sequence`）。**这条我不报缺陷**：
    那文件 mtime 是 **15:58:49**，正好在我那次跑的中间，`15:59:13` 复跑 `go vet ./internal/adapter/` ⇒ **0**
    —— 按 T-68 判为 adapter 在制品已自愈，与 15:29:31 那次红无关。
  - **判据表 FL 的镜像文本**在 `## Coordination → REQ-T10（第三张）`（与 T84 同族，主控说可一并镜像）。
  - **两件与投递文本不一致的地方，取证在后，不是反驳**：
    ① "你账本里写了『backend 从没枚举过』" —— 我账本里**没有这句话**：
      `grep -c 枚举 .qoder/progress/test.md` = **0**、`grep -rn "从没枚举" .qoder/progress/test.md scripts/` = 0。
      但我把**正面事实**记下了：backend 确实枚举过相关的一条，位置不是 226~240（那段是幂等 REQ-DB-1 的账），
      而是 `.qoder/progress/backend.md` 第 **110** 条（约 **2242~2258** 行），里面就写着
      `--- FAIL: TestAnchorAdvancesOnlyAfterCommit  core_test.go:510: open <TempDir>/audit.head: ... cannot find the file`
      以及它的根因（`internal/audit` 把 `count int` 换成 `lastSeq/anchoredSeq/seeded` 那次改判）。
      **差别有意义**：那不是一个"共享路径冲突"，是**跨包契约改动打红别人的用例** —— 恰好说明
      "共享面"不止那四个文件，所以我在 FL 文本里把第四类叫"同批包里被改到的共享契约"。
    ② "DataDir 独享就不算共享"这条我核了但**不采信为结论**：脚本自己数了 `cmd/core/*_test.go` 的
      `Config{` vs `DataDir:` vs `t.TempDir()`，报的是"没有测试吃默认目录"（`core_test.go` TempDir=2、
      `ws_smoke_test.go` 没构造 Config），这是一条可复查的事实；至于红的原因，留给 backend。
  - **交付状态**：`bash -n` 通过、`--selftest` exit 0、采样窗口 15:57:55~15:58:57 全绿（按口径记为"未复现"）；
    四条验收见 Verification evidence 首条（本轮最后一次重跑）。

- **15:29~15:36 轮（T-118：MS 表已镜像 → 翻指针 + 照契约命令原文复跑）**：
  - `core/test/adapter_contract_test.go` 的 MS 表换成一行指针（"判据见契约② §4 判据表 MS"），
    只留两条**非判据**的实现说明（门控为什么存在、MS-4 的取值为什么不当场问对端要）。
    账本里那份粘过去的 MS 表也删了（`grep -c "^| MS-" test.md` = 0）——契约是权威，账本再留一份就是第三处真相。
  - **照契约表里的命令原文逐条复跑**（15:35:28，不是跑我自己写的版本）：
    带门控 `-run TestFakeAdapterModelSelectLegs ./test/` ⇒ 顶层 + `MS-1a/1b/2/3/4` **共 6 条 `--- PASS`**；
    `-run TestModelSelectShapeMatchesContract ./test/` ⇒ `--- PASS`（无门控）；
    **不带门控 + 带 `-v`** ⇒ 看得见 `--- SKIP`；**不带 `-v`** ⇒ 只剩一行 `ok`。
    主控②那句"没跑过的那一半不许算成证据"我认，MS-1~5 在我 14:4x 那批里确实只在**开旗标**的跑里绿过；
    契约现在标"现存（门控内 PASS）"是准确写法，我原来在注释里标"现存"偏松。
  - **一句要回给主控的核对（又一次定位偏差，附上取证）**："改回 `ws_cursor_contract_test.go` 122 行那句
    『model/select 实现为零』" —— 我全部交付物里搜不到这个说法：
    `grep -rn 为零 .qoder/progress/test.md core/test/ scripts/` ⇒ **0**；
    `grep -rn "model/select" core/test/` 只有两处，都是合法用途（`writeMethods` 清单、未握手拒绝清单）。
    那措辞的真实位置是**契约② §4 那行自己**（现在写的是"我上一版把这条写成『实现为零』是错的"）。
    **实质你说得对**（状态列偏松），但那处文本已经随着 MS 表整体删除而消失了，所以我没有可改的第二个地方。
  - T-117（`session/push` 在 v0.1 撤销）与我无关但要一句：`grep -rn session/push core/test/ scripts/` ⇒ **0 命中**，
    我的断言面与 lint 里都没有这个方法名，不需要跟着改。
  - **我自己的同族错误，第三次，先说错再改对**：上一轮我报"已删掉重复的 `## Completed`"，
    实际那条删除命令根本没跑（我把"打算跑"当成"跑过了"）。这轮 `grep -c "^## "` 复验才发现，
    现已真删并复验 = 11。**规矩加成：删/插之后必须在同一条命令里跑 `grep -c "^## "` 与已知值比对，
    不许"下一轮再说已修"。**

- **14:31~14:42 轮（T-103：`model/select` 三条拒绝腿进断言面）**：
  - **新增判据表 MS（6 行，全绿）在 `core/test/adapter_contract_test.go`**：
    MS-1a/1b 缺 `model_key` / 缺 `remote_session_id` ⇒ `-32602 INVALID_PARAMS`；MS-2 未知会话 ⇒ `-32006`；
    MS-3 模型不在 offer ⇒ `-32002 CAPABILITY_UNAVAILABLE`；MS-4 **正例**（会话取自 `session/create`、
    模型取自 `model/list`，两个值都是**当场问对端要**的，不写死 fixture）⇒ 无 error，且回执
    `{"remote_session_id":"rsn-new-1","model_key":"fake-fast"}` 恰是 §4 CO-36 定形的"同形状"两键；
    MS-5 三条拒绝码两两不同（§4.1"两处门不要混"）；MS-6 **契约形状 ↔ Core 侧类型互校**
    （`ModelSelectParams`/`ModelSelectResult` 的 json 字段恰为那一对、且**不许带 `omitempty`**——
    带了就允许"省略"这第三种"没有"，正是 §4 同步游标那条裁定要收掉的形状）。
    绿红两边都在：MS-4 证明 MS-1~3 不是"永远拒"，MS-6 的两条腿各自可红。
  - **MS-6 我没等 T-102②，因为"Core 侧为空"这句话已经过期**（当场取证，不是反驳派发）：
    `internal/adapter/send.go` mtime **14:01:57**、工作树里 `git diff --name-only` 有它 ⇒ 类型是刚长出来的、**尚未提交**。
    13:53 巡检那次 grep 当时是对的（确实为空），现在不成立了。所以我把互校腿直接写成硬断言并跑绿，
    同时在注释里写明"红的时候先按 T-68 查 mtime 分清在制品与回归"。
    `grep -rn "ModelSelect" core/internal/adapter/*.go`（排除测试）今天命中 `send.go:152/159/166` 与 `handshake.go:409`。
  - **一句要回给主控的核对**：T-103 说"顺带改你 `core/test/ws_cursor_contract_test.go` 里那句『实现为零』的错话"。
    我全盘搜了：`grep -rn 为零 .qoder/progress/test.md core/test/ scripts/` ⇒ **0 命中**；
    那个措辞只存在于**契约② §4 那行**（148/89 行现在写的是"我上一版把这条写成『实现为零』是错的"）。
    所以我这边没有可改的文本 —— 如果你指的是别的文件或别的词，给我行号外的一句原文我就去改。
  - 交付状态：**14:40:59~14:42:16** 四条重跑全 exit 0；默认面 **46 PASS / 14 顶层 SKIP / 0 FAIL**
    （SKIP +1 是 MS 腿要旗标），开 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **62 PASS / 9 SKIP / 0 FAIL**；
    lint 第七轮 exit 0、selftest exit 0、crash exit 0。基线 `e013f66`；
    工作树只剩 `M core/test/adapter_contract_test.go` + `M core/test/ws_cursor_contract_test.go`
    （后者是 F20 指针，已被 `03b4c36` 收过又改了一次注释）——`2 files changed, 155 insertions(+), 8 deletions(-)`。

- **14:21~14:22（T-97 投递与本仓实际进度的对账，无需新动作）**：
  - T-97 说的"顺序问题不存在了、可以改指针"**我 14:0x 那轮已经做完**（`ws_cursor_contract_test.go`
    mtime 14:12:31，工作树里就剩它一个 `M`，`1 file changed, 8 insertions(+), 8 deletions(-)`）。
    本封到达时没有待办，所以我只做了一件增量：**照契约表里的命令原文逐条跑一遍**，验的不是我自己的注释而是
    镜像后的权威文本 —— 14:22:08 实测：
    `-run TestHelloCursorOmittedOverridesQuery ./test/` ⇒ `--- PASS`；
    `-run TestHelloCursorOverridesQuery ./test/` ⇒ **3 条 PASS**（1 顶层 + `query_小于_hello`、`query_大于_hello`），
    与主控 13:17:27 标定和状态列写的"PASS×2 子用例"逐字对得上。**镜像过去没有跑不动的命令。**
  - 两条还开着的（都在 Coordination，不催第二次）：
    ① F20-1 的镜像行里没带我请求的**两条对照腿**（新事件仍要实时推得下来、显式 `from_seq=0` 必须补发全部）
      —— §5 那张表 mtime 13:19，之后没再动；
    ② **T84 还没镜像**（契约里现有 C8/F20/W13/U13/A18 五张，T84 仍在 `turn_evidence_test.go` 的注释里），
      可粘文本在 `## Coordination → REQ-T10（续）`。粘进去之后我翻那一处的指针，在那之前不动。
  - REQ-F-22（`""`/缺键）按巡检说的归 frontend，不进我的表；我只在 §5 那张 F20 上留对照腿的请求。
  - 全量四条验收仍以 14:13:17~14:14:24 那批为准（默认 45 PASS / 13 顶层 SKIP / 0 FAIL，
    开旗标 55 PASS / 9 SKIP / 0 FAIL，lint/selftest/crash 全 exit 0）；14:22 之后没有任何代码改动。

- **14:01~14:06 轮（T-94：REQ-T10 交表 + REQ-T11 采纳 + Q3/Q4 复查）**：
  - **先纠两条投递里的事实（都用只读命令当场取证，不是反驳派发）**：
    1. 巡检 13:04 那句 `grep -c "F20" specs/api-contract-v0.1.md` = **0** 已经不成立：我 14:01 复跑 = **5**，
       而且 §5 里已有 **`#### 判据表 F20`**（148 行）+ 主控 13:17:27 逐条实跑标定"三条全 PASS"，
       159 行明写"`test` 侧可以改成指针了"。**镜像已经发生**，所以我按授权直接翻了指针，
       没有再交一次文本（交也是重复）。这条防的是"按过期快照行动"，巡检自己也说了"别扑空"。
    2. `grep -c "turn_ref 一律必带" specs/adapter-protocol/…` 我第一次跑 = 0，是因为契约里写的是
       `` **`turn_ref` 一律必带** ``（带反引号与星号）。换正确 pattern 再查 ⇒ 命中在 §4.2 裁定第 1 条。
       **同一个坑我今天差点踩两次**（否定结论必须先怀疑自己的 pattern）。
  - **`core/test/ws_cursor_contract_test.go` 的文件头表已改成一行指针**（判据本体搬进契约，注释里不留副本），
    同时在指针下面留了一段**请主控补进表里**的话：镜像版 F20-1 只写了"不补发"，
    但用例其实还断两条对照腿（① 随后新事件仍要实时推得下来；② 显式 `from_seq=0` 必须补发全部），
    少了它们这判据就是半边的；F20-3 的"两个方向"建议把"query 大 / hello 小 专抓取较大者"写进去。
  - **REQ-T11 采纳记录**：默认证形改三句（`git diff --stat` + `git status --porcelain` 看 `??`
    + 对新文件 `git diff --no-index --stat /dev/null <文件>`）。我这边从 12:0x 起就一直在交三句，
    今天 B-a7/T-94 两次印证它是漏洞而不是我多事。
  - **T84 表交文（REQ-T10 的续）**：契约里现在已有 **C8 / F20 / W13 / U13 / A18** 五张表，
    **只有 T84 还住在我的测试注释里** —— 按同一道理请镜像进 **契约② §4.2**（C8 那张就在同一文件）。
    子用例计数取自 14:05 那批 evidence 文件，不是印象数字。
  - 交付状态：14:05:12~14:06:20 四条重跑全 exit 0；默认面 **45 PASS / 13 顶层 SKIP / 0 FAIL**，
    开旗标 **55 PASS / 9 SKIP / 0 FAIL**；lint 第六轮 exit 0。基线现取 `7c40d3b`。
    （其后 14:13:17~14:14:24 又因"改了一个注释"整体重跑一次，同样全 exit 0、数字不变。）
  - **入账方式变了，记一笔**：主控 13:59 的快照提交 `03b4c36`（"17/17 全绿基线，13:57:29 主控复采"）
    把我 T-98 那批交付物全部收进版本库 —— `git log -1 -- <文件>` 逐个核过：
    `turn_attribution_test.go` / `turn_evidence_test.go` / `ws_cursor_contract_test.go` /
    `scripts/check_ownership.sh` 四件都在该提交里。所以这批不再是"worker 自述待复采"，
    是主控自己量过的。我现在唯一的工作树改动就是 F20 表翻指针（`1 file changed, 8 insertions(+), 8 deletions(-)`）。

- **13:24~13:47 轮（T-98 三件）**：
  - **件① CO-36 三处洞全补**（三处都是我自己的）：
    1. 表全集改**运行时解析**：删掉手抄的 13 张 `AGGREGATE_TABLES`，新增 `biz_tables()`
       从 `core/migrations/*.sql` 解析 `CREATE TABLE` ⇒ 实测 **33 张**。那张手抄清单本身就是新的假绿来源
       （迁移加第 34 张时它不在清单上 ⇒ 掉进 `not-a-table` 只报 INFO）。
    2. **`core/` 根行不再当兜底归属**：`load_owner_table` 遇键 `core/` 只登记不注册。
       `--explain` 实测：`core/zz.go -> UNOWNED [无规则命中]`、`core/go.mod -> orchestrator [core/go.mod]`、
       `core/internal/newpkg/x.go -> UNOWNED`。今天全仓 0 越界没被这次收紧改红（显式行照旧命中）。
    3. **W13-3 的反向断言自己也被反向验一次**：新造一棵"只有 Service 形状"的树
       （`c.Tasks.Create(ctx, in)`/`c.Sessions.Touch(ctx, id)`/`q.UpdateOnce(ctx, id)`）⇒ 必须 **0 命中**；
       原来只有"混合目录命中 1 处"那一半，证明不了宽模式没残留。
       另加两条面非空断言：生产 .go ≥3（实测 9）、解析表数 ≥30（实测 33），不满足直接 `bad` 并 return。
    - W13 报告形状照"一行一表、整批扫过"：先印「判据面：33 张迁移表逐个扫 × 9 个生产 .go；白名单 3 处」，
      再逐行 `[W13-2] <表名>: 0`。
    - **要回报的事实差**：预告的"红一批"**没有发生**。`messages`/`quota_accounts`/`approvals`/`handoffs`
      在 `core/api/**` 与 `core/cmd/core/**` 的生产代码里**任意写形状 0 命中**；全目录只 4 行提到 `approvals`，
      都是 `core.approvals = approval.NewSQLiteStore(...)` 这种字段名/构造函数，不是表写。
      所以我交的是"面非空 + 逐表 0"。如果那两个名字的红是别人转述的，请回推源头。
  - **件② T84-6 落成真判据，Skip 摘掉**（主控指的路径是对的，我 12:5x 判成 blocked 判错了）：
    新文件 **`core/test/turn_attribution_test.go`**，只用导出的接缝：
    `process.Supervisor{Kind, Sink, Launch}` → `Connect` → `Initialize` → `CallTurn(ctx, 绑定id, "session/send", {execution_id: …})`，
    **不碰未导出成员、不要导出 `Connection`**（REQ-T9 的教训还在）。矛盾帧不需要注入面：
    `CallTurn` 绑 `exec-t846-b`、参数里写 `exec-t846-evil`，FakeAdapter 照参数回显即自然矛盾。
    - **T84-6a 一致 ⇒ 收下**：`turn_ref="01a0c27b-…4ae9"` 游标推到 5、`Turn("exec-t846-a").State()=RUNNING`。
    - **T84-6b 不一致 ⇒ 丢弃**：`exec-t846-b` 与 `exec-t846-evil` 两个 tracker 都停在 `""`、假 id 的帧 **0 条转发**。
    - **我先红了一次，且是判据错不是实现错**：第一版断"丢弃后 per-ref 游标必须停在 0"，实测 10。
      回去读 `notify` 才看清：同一次调用的 `stream/state`/`delta`/`item` **不带 `execution_id`**，
      无 id 可比就不构成矛盾 ⇒ 水位前移是对的，裁定 ① 只管"两者同时在场"。改成判"两个 tracker 都不动 + 0 转发"。
      **这是 §7.1"命中先看接收者"在我自己身上复发**：我又把"看起来更严"当成了"更符合契约"。
    - 表里剩 **T84-7 待建**：`丢弃 + Audit` 的 Audit 半边今天没有可断对象
      （`Recorder` 只有 RecordStart/Exit/Crash，包内 grep `audit` 零命中）⇒ **REQ-T12**。
  - **件③ CO-35 探针**：新增 **check 9/9**（原八条编号统一改 `/9`）。命令 `grep -rn delivery_unacknowledged core/ --include=*.go`
    与 `grep -rnI … specs/ docs/` **分开计数、以 core/ 为准**（只在 specs 命中 = 文档登记过、代码里没人发，不算接上）。
    实测输出：`[SCOPE 之外] core/ 扫到 0 行，这条判据不成立，请派归（specs+docs 0 行）`，不参与 exit code。
    - **必须报给主控的一件事**：这名字在契约里也搜不到（13:4x 全仓取证，去掉 .git/node_modules）：
      `grep -rniI delivery_unacknowledged specs/ docs/` ⇒ **0**；`grep -rn "规则 6" specs/ docs/` ⇒ **0**。
      所以 T-50/T-98③ 那两点判据今天只存在于派发文本。我在 check_9 注释里**不引用"§6 规则 6"**、只引用派发；
      名字与两点回写进事件目录 §6 之前，这条探针只能对派发文本负责（并到 REQ-T12 一起问）。
  - **一处 harness 级假信号（红在先，还是我的）**：子进程面 `request()` 原来"读下一帧当响应"，
    而真 FakeAdapter 一次 `session/send` 会先发通知帧（claimed/dispatched/state/delta/item），
    通知帧没有 `error` 字段 ⇒ 我把 `steering` 那一发读成"被接受"，当场判了 adapter 一条**假红**。
    修法：**常驻读取器 + 按 id 配对响应**（`frames chan []byte`，路过的通知计进 `skipped` 并出现在超时信息里），
    C8-5 那种要看原始字节的判据继续走 `recv`。改完 `steering` 显式被拒 ⇒ PASS。
    同时把 `f.delivery` 的参数补全（`remote_session_id`/`idempotency_key`）—— 参数不全会先撞 §4 参数校验，
    根本测不到投递门，同一个 T-29 坑。
  - 交付状态：13:46:12~13:47:19 四条验收重跑，全部 exit 0；默认面 **45 PASS / 13 顶层 SKIP / 0 FAIL**，
    开 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **55 PASS / 9 顶层 SKIP / 0 FAIL**。

- **12:58~13:08 轮（T-93 / §4.2 两处口径由主控裁定，我的 SKIP 转硬断言）**：
  - **Q3/Q4 已闭 → T84-4、T84-5 从"不判"改成硬判据**（我 12:2x 那版是按 §4.2 原文"两处未定"写的）：
    - **T84-4 改名 `TestTurnRefMandatoryOnEveryNotification`**：裁定 ① 把例外取消 —— **六种通知全部必须带
      `turn_ref`**（含 `turn/claimed`/`turn/dispatched`）。逐个判两侧：缺 ⇒ `INVALID_PARAMS`、带上 ⇒ 成帧；
      再加一条 `len(all) != 6 ⇒ 红`，防 §4 将来加第七种通知时这条悄悄少测一个。
      （12:2x 版本里"取证两帧不带也合法"那一半当场翻反 —— 这正是转录契约而非转录实现的好处：
      契约翻了，判据跟着翻，且**红在明处**。）
    - **T84-5 改名 `TestDispatchedHandleOptionalButNeverContradicts`**：裁定 ② 三条形状全打在
      **Core 侧 tracker**（主控点名，与 T-29 同源）——
      实测原文：`claimed=prov-1、dispatched="" ⇒ State="RUNNING" Handle="prov-1" err=<nil>`
      ／ `同名 ⇒ RUNNING`／ `异名 ⇒ State="UNKNOWN" err=INTERNAL: …names provider "prov-2"…`；
      另断"空值那一腿不许把 Handle 清空"（清空 = 把已经问得到的 run 变成查不到，正撞 CO-8）。
    - **T84-2 加第 8 个形状**：`claimed` 带空 provider 名字 ⇒ 必须被拒，且**不许留下确定状态**。
      这里我用了一个哨兵 `t84NotDefinite` 而不是写死 `""` 或 `UNKNOWN`：**契约只要求"不许确定"，
      被拒之后 tracker 里留哪个非确定值是实现细节**，写死就是替实现挑答案。
  - **T84-6（新，`SKIP（blocked）`）= 裁定 ① 的交叉校验那一腿**，缺什么写得很具体：
    校验在 `internal/process` 的 `attributeLocked`（未导出），`Connection` 只能由
    `(*Supervisor).Connect` 拿到而它会**真起子进程**，而 FakeAdapter 只会照 `session/send` 带来的
    execution_id 发帧 ⇒ **一致**那一半能走通、**不一致**那一半要让对端故意发一个与 `turn_ref` 绑定不符的值。
    三条期望断言（一致放行／不一致丢弃且不推进任何 tracker／未知 turn_ref 丢弃且不动游标）
    已写在用例注释里，落地即改硬。**没有**为此要 adapter 导出 `Connection`（REQ-T9 那条教训还在）。
  - **两条我自己的错，都当场取证并记下**：
    1. **我拿下游日志的值反推了上游**：看到 T84-5 的日志印着 `State="UNKNOWN"` 就以为"被拒的 Claim 会落成 UNKNOWN"，
       给 T84-2 新行写了 `wantState: TurnUnknown` ⇒ 当场红。真相是那个 UNKNOWN 来自**之后那一发 Dispatch**
       （日志是在两次调用之后取的），`Claim` 被拒时状态根本没动。这正是我天天写在包文档里的 T-29
       "判据要打在待验那一层的原始输出上"——我自己犯在**读日志**上。修法：那一行改成哨兵断言（见上）。
    2. **拼接把文件写成了 512 行**（`head -n 211` + tail 的 splice 跑了两遍，函数重复定义）。
       13:06:57 那次 T84-4 的 `--- FAIL` 出自这个自我中间态，不是 adapter 在途；
       靠 `wc -l`(512→274) + `grep -c '^func Test'`（每个名字 1 次）取证后复原，连跑两次绿。
       备份在 `.scratch/test/turn_evidence_test.go.orig`（绝对路径具名文件，T-66①），未碰共享 Temp。
  - **纠正一条挂到我头上的转述**：巡检说"你 12:25:31 报的『全仓 17 包 0 FAIL』按 worker 自述对待"。
    **那句不是我报的**：`grep -n "17 包" test.md` 只命中 09-20 的 **db** 段（"全仓 17 包 0 FAIL"是 db 的自述，
    且同段记着我当时纠它"18 个不是 17 个"）；我自己的验收范围从来只有 `./test/...` + 两个脚本，
    时间戳也是 12:24:12~12:25:15。**这条我复采不了别人的账，请转回 db 那条线**。

- **12:13~12:25 轮（T-84 / §4.2 两帧取证信号 + "只看字符串字面量"裁定）**：
  - **新交付 `core/test/turn_evidence_test.go`（269 行）**：§4.2 那四条语义的**独立转录**，判据表 T84-1..T84-5
    （表在文件头注释里，格式同 C8/U13/W13/F20：ID + 断言 + 带 `-v` 的命令 + 状态三值）。
    被测面是 `adapter.TurnTracker`（`Claim`/`Dispatch`/`State`/`TurnID`/`ProviderExecutionID`）与
    `adapter.CarriesTurnRef`/`NewStream`，期望值一条都不从 `stream.go` 里抄。
    - **T84-2 是这条的主身体**（"没有 dispatched 不许显示 running"，与 §12"不许把 UNKNOWN 显示成确定值"同线）：
      7 个形状里**只有一条**能到达 RUNNING（Claim→Dispatch 同一 execution_id），其余 6 条
      （只 claimed / accepted=false / dispatched 先到 / execution 对不上 / 缺 turn_id / 重复 dispatched）
      逐条断"不等于 RUNNING"，并加一条 `positives != 1 ⇒ 红` —— 防的是"State() 永远空"那种塌成一团的假绿。
      错误面也两侧都断：该拒的 4 个形状不许静默收下，不该拒的 3 个不许乱拒（把合法 claimed 当错误会让
      Core 把"接下了"当事故）。实测 7/7 PASS，`-v` 里每行的 `State=` 与 `err=` 原文可见。
    - **T84-3**：`provider_execution_id` 挂在 claimed 上，连 **`accepted=false` 的拒绝路径也要留下两个 id**
      （Core 可能死在"claimed 了还没 dispatched"窗口里，Recovery 要先拿它问 Provider）；
      反向半边：空 Handle 的 claimed 必须 INVALID_PARAMS，且**不许留下"看起来有名字"的假证据**（断言被拒后值为空）。
    - **T84-4**：四条通知（delta/item/state/finished）必须带 `turn_ref`（缺即拒、带即成帧，两侧都断），
      取证两帧不被要求带 —— §4.2 语义 4 那个"execution_id 唯一允许越进程边界"的例外。
  - **未定的两处一条都不写成判据**（主控 09-21 原话："别先把它当已定数据写死"）：
    ⚠2 `dispatched` 的 `provider_execution_id` 必填（代码）还是可空（CO-13 注释）⇒ 落成
    `TestDispatchedProviderHandleUnruled`，**SKIP（blocked）**，理由里带今天盘上的实测值 + 两种读法各自
    该把断言改成什么。判绿 = 把"暂按代码"钉成契约，判红 = 替 adapter 做决定，两个都不干。
    ⚠1 `claimed` 带不带 turn_ref 也未定 ⇒ T84-4 只断"不被要求携带"，**不断**"携带即违规"。
  - **主控那条"扫描只看字符串字面量，不看注释"的裁定落进 lint**（这是 `approval/cancel` 那条注释天然豁免的根据）：
    新增 `strip_comments`（按字符扫一行，引号/反引号内的 `//` 不算注释起点，行内 `//` 之后整段丢掉），
    接进三处判据：`cross_package_refs` 的 hits（替掉原来那条只认"行首注释"的弱过滤）、
    **C8-3 的常量存在性 grep**、`c85_err_hits`。C8-3 这一处是真·假绿来源：常量删了而注释还写着
    `"PROTOCOL_NOT_INITIALIZED"`，裸 `grep -qF` 照样命中就报绿。
    `--selftest` 配了两条相反方向的断言：纯注释里的 `quota_accounts` **不许**被算成代码（防假红/假绿双向），
    而行内注释**之后**的真写调用 `tx.Append("quota_accounts", m) // 注释` **不许**被剥过头（漏报比误报更坏）。
  - 修的过程中我自己错了一次：T84-2 第一版用 `wantErrNil` 默认 false，等于要求"所有非正例都必须返回错误"，
    而"只 claimed""accepted=false"本来就是合法回答 ⇒ 两条子用例当场红。改成 `mustErr` 显式逐行标，
    红的是我的判据不是实现。**记账：把"错误必须存在"当默认值 = 又一次单向断言，正是这轮在防的那个毛病。**

- **11:52~12:07 轮（T-81 巡检 / REQ-F-20 落判据）**：
  - **新交付 `core/test/ws_cursor_contract_test.go`（458 行，全新增）**：契约③ §5 那三条判据按判据表格式落成
    可跑用例，**每条两侧都断**。判据表 F20 现在写在该文件的包文档注释里（specs/** 不是我可写的范围，
    要挪进 §5 我就把文本交出去，见 Coordination）：

    | ID | 断言 | 当场命令（一律带 `-v`） | 状态 |
    |---|---|---|---|
    | F20-1 | 只给 `?from_seq=` + hello **省略**游标 ⇒ 首帧 `hello_ack{server_seq}`、历史不补发；**且**随后新行仍实时推得到（"不补发"不许靠关推送）；对照腿：显式 `from_seq:0` 必须补发 `[1 2 3]` | `go test -count=1 -v -run TestHelloCursorOmittedOverridesQuery ./test/...` | 现存 |
    | F20-2 | 只给 `?from_seq=` + 超时**不发 hello** ⇒ 首帧回显 `hello{from_seq=query}`（不是 ack）、补发**恰为** `seq>query` 的升序序列 | `go test -count=1 -v -run TestNoHelloFallsBackToQueryCursor ./test/...` | 现存 |
    | F20-3 | hello 游标与 query 不一致 ⇒ **以 hello 为准**，两个方向都判；再加一条"错读对照"：期望值必须 ≠ 「query 优先」与「取较大者」两种错读的产出 | `go test -count=1 -v -run TestHelloCursorOverridesQuery ./test/...` | 现存 |
  - **接缝选择与理由（不连真 Core）**：`wsapi.Handler(Config{Tickets, Journal, HelloTimeout, Poll})` 是导出的注入面，
    我在本包自建 `Journal` 替身（`var _ wsapi.Journal = (*wsJournal)(nil)` 编译期钉住接口，实现改签名我不会静默空跑）。
    真链路今天造不出"游标下有历史、游标上有新行"这组数据：`cmd/core` 起来 `event_journal` 是 0 行（REQ-T7）、
    `POST /api/v1/tasks` 还没有（U13-6）。等它们落地，同一批断言再加一个真子进程被测面（已记进 Remaining）。
    等待预算全部从**我自己设定的** `wsHelloTimeout=300ms` 推导，不抄实现的 5s 默认（"别写死实现数值"）。
  - **客户端是我自己按 RFC 6455 写的**（掩码、单帧、≤125 字节），故意不复用包内 framer；握手用 RFC §1.3
    示例向量（`dGhlIHNhbXBsZSBub25jZQ==` → `s3pPLMBiTxaQ9kYGzzhZRbK+xOo=`）钉住"我的客户端没写错"，
    这一钉取自 RFC 而不是被测代码。`httptest` + 裸 `net.Dial` 走握手，绕开本机代理（T-31③a）。
  - **区分度证据（不是"把实现念一遍"）**：F20-3 的 `-v` 日志实测印出三种读法的产出 ——
    `query=9、hello=2` 时「query 优先」发 `[]`、「取较大者」发 `[]`、§5 要求 `[3 4 5 6]`；
    `query=0、hello=5` 时「query 优先」发 `[1..6]` 而 §5 要求 `[6]`。⇒ 两个方向都能把错读判红。
    F20-1 的解析器活性对照：同一套 `seqs()` 在"显式 0"腿里读出 `[1 2 3]`，所以"省略"腿里的空是真没有。
  - **今天第二条同源假阳性，在我自己这边**：跑全量时 lint 冒出 1 红 —— `core/cmd/fake-adapter/service.go`
    的 4 处 `json:"sessions"` / `"messages"` / `"models"` / `"approvals"` 被判成"表名以变量传入"。
    实际是**Go 结构体 tag 的序列化名**，不是表引用。按 T-74 那条教训当场核对每一条命中的语法角色后修掉：
    `literal` 腿前面加 `tag` 分支（形如 backtick + `json:"表名"` 的 Go 结构体 tag 归 tag ⇒ INFO），并把合成件
    「type row struct { QA []string ·json:"quota_accounts"· }」（两侧点是 Go 的 raw-string 反引号）加进 `--selftest`，
    同时保留"`const tblName = "quota_accounts"` 必须仍判 literal"那条 —— **绿红两边都断**。
    修完 lint 回到 exit 0，那 4 处以 INFO 明文可见（不删发现）。
  - **`cmd/core` 编译失败是 in-flight，不是回归**（按老规矩取证）：12:01 那轮 `TestCrashKillCoreAndRestart` /
    `TestCrashRestartSameDataDir` 从 PASS 掉到 SKIP，Skip 理由自己打印了
    `main.go:138:7: core.reporter undefined`；`stat -c %y core/cmd/core/main.go` = **12:02:42**（我的测试跑在 12:01:29~12:01:5x，
    正好夹在保存中间），12:06:07 重跑 `go build ./cmd/core` ⇒ **exit 0**，两条随即回到 PASS。
    我的用例按设计 Skip 并打印原因，没有拿桩绿冒充——这就是那 2 条 SKIP 的来龙去脉。
  - **交回证形有个盲点要报备**：`git diff --stat HEAD` **看不见未跟踪的新文件**。本轮新增的
    `core/test/ws_cursor_contract_test.go` 就是这样差点漏掉，所以我一并交回
    `git status --porcelain -- core/test scripts`（列出 `??`）+ `git diff --no-index --stat /dev/null <新文件>`
    （458 insertions）。建议这条并进 T-72 的默认证形。

- **10:45~11:1x 轮（T-74 ③④⑤⑥）**：
  - **③ W13 三条判据全部落进 lint**（判据本体在契约③ §7.1，脚本里只解析、不抄表）：
    - **模式收紧**：`W13_RE='\.Append\("|\.UpdateOnce\("|\.Touch\("|\.Create\("'`（表名字面量紧跟左括号）。
      旧形状 `.Append(`/`.Create(`/`.Exec(` 有两处假阳性来源：`c.Tasks.Create(` 被当写表（REQ-WS-3 第 4 条），
      以及大小写无关的 `UPDATE` 会吞掉 Go 的 `UpdateOnce(` —— 后者是本轮收紧时才露出来的，
      白名单已经放行 Append 却仍报 1 条红，查下来是 SQL 文本腿在咬方法名，已加边界
      `(^|[^[:alnum:]_])UPDATE[[:space:]]`。教训：**同一条"放宽的字面量"会在多个判据里各自复发**。
    - **新增 2b/8 `check_w13`**：扫 `core/api` + `core/cmd`（§7.1 当场命令的形状），逐条抽 表名+方法 后分类：
      W13-1 白名单（`idempotency_keys:Append` / `idempotency_keys:UpdateOnce` / `principals:Append`，
      **表+方法**级，`principals Touch` 不放行）→ 绿；聚合表（ADR-017 的 10 张 + 000003 的 3 张）在
      `core/api/**`/`core/cmd/core/**` 被写 → **红**（W13-2）；已知表但非白名单 → **红**（§7.1 收尾"闭集，
      要豁免改表"）；`"字符串"` 不是已知表名 → INFO 判为绿（Service 方法的字符串首参）；
      `core/cmd/fake-adapter/**` 落在 W13-2 声明范围之外 → INFO 不判红，并写明"要扩范围请改判据表"——
      我没有替契约扩大范围。
    - **W13-1 的三处合法命中不再要求下沉**：`cross_package_refs` 的 write 腿现在共用同一份 `W13_ALLOW`
      （不另立口头豁免），改判为 `w13-allow` 桶 → INFO。
    - **W13-3 反向用例进 `--selftest`**（你说"没这条就是空判据"，认账）：合成件里
      `c.Tasks.Create(ctx, in)` + `c.Sessions.Touch(ctx, id)` 必须 0 命中、`tx.Append("tasks"` 必须 1 命中；
      `w13_classify` 另有 6 条形状断言（aggregate-red / allow / 方法级不放行 / known-table-red /
      not-a-table=绿 / out-of-scope=不判红）。
    - **变异证据（T-66）**：把 `W13_RE` 改回旧形状拷进 `scripts/w13_mut_tmp.sh` 跑 ⇒
      `SELFTEST FAIL：W13 模式命中 3 处，期望 1`，并且真扫里 `core/api/http/tasks.go … 引用表 tasks … 写调用`
      那条假阳性**当场复活**、exit 1；变异版已 `rm` 并证明不在盘上
      （`find . -name 'w13_mut_tmp.sh'` = 0、`ls scripts/` 只剩两个真脚本、`git status --porcelain scripts/`
      只列 `M scripts/check_ownership.sh`）。变异件放在 `scripts/` 而不是 `.scratch/`，是因为脚本按自身位置推
      仓库根——放 .scratch 里跑的是 `.scratch` 那棵空树，测不到真扫描面。
  - **④ C8-5 帧级那一半已在真子进程上实测，没有向 adapter 要导出入口**：新增
    `TestCancelReplyIsResultFrame`（`core/test/adapter_contract_test.go`）——握手 → `debug/hang` 发出去不读回执
    （制造"有一 call 在飞"）→ `adapter.NewCancel` 发 `$/cancel` 通知 → 读回**原始字节**并判：
    无 `error` 键、有 `result` 键、`result.cancelled=true`、`id` 回显被取消那一 call、整帧不出现 `"CANCELLED"` 码名。
    判据抽成纯函数 `cancelAckFaults`，同用例里带**两条反例**（合成的 error 形态回执必须判红、
    合规结果帧必须判绿），并把两个计数 `t.Logf` 打进 `-v` 输出，报告里能直接看见判据两边都活着。
    只看线上字节不看 `adapter.Frame`：`Frame.Error` 带 `omitempty`，从结构体反推不出"键在不在"（T-29）。
    C8-5 的**静态半边**改按你表里那条命令实现（`c85_err_hits`：`"CANCELLED"` 落在
    `errReply|Error{|NewErrorReply|ErrorCode` 形状上才算违反），且现在会区分
    "有引用都不在 error 侧"（INFO 证据）与"根本没引用"（INFO **暂无证据，不是通过**）——
    今天实测是后者（fake-adapter 生产代码 0 处），我没拿它当绿。
  - **⑤ REQ-T8 停报**（已闭，`.gitignore` 那行你 12:5x 落了、4 个 .py 是你的一次性补丁器）；
    **REQ-T9 撤回**：按你给的方向改在子进程帧面上做，④ 已证它不需要导出 `process.Connection`。
  - **⑥ 证形**：交回带 `git diff --stat HEAD -- core/test scripts`（基线 `git log -1 --format=%h` 现取，本轮
    实测从 `1298fed` 漂到 `e87293f` —— 主控在并行落盘，所以每次现取不写死）。
  - **判据表格式对账（①/②）**：§7.1 这张表是**我按 `|` 分列直接读到的第一张契约③ 表**，措辞、锚点、
    现状列都可解析，不需要另一种格式。两处按你 T-70 的约定执行了：不拿"现状"列的数字当断言
    （"命中 3 处"我只报数不判等），`状态=待建`/无证据一律 INFO 不进 exit code。

- **16:0x~16:17 轮（T-70 判据表定稿 + T-72 证形 + audit 的解除项）**：
  - **两张表都可解析**：C8（契约② `#### 判据表 C8`，5 行）、U13（内部契约 §1.1#13，7 行）。
    按 `|` 分列 + 标题文本锚点即可读；`状态=待建` 不判红。U13 六行我照表内命令原样跑：
    U13-1~5 `--- PASS` 计数 = **5**、U13-6 = `--- SKIP`（与你 16:01 标定一致，无需纠你）。
  - **C8 对齐**：C8-1/2/3/4 全部落地。C8-3 按你给的命令升到**判红**（正向：`Core:*` 且 ✓ 的行
    必须在 `core/internal/adapter` 搜到码名）；反向（表标 ✗ 却出现字符串）保留 INFO——
    因为字符串可能只是数字表的一行，契约没定义那种情形，我不想拿自己的猜测判别人红。
    活性证据：查一个不存在的码名 `grep -rF '"NOT_A_CODE"' core/internal/adapter/` = **0 命中**，
    说明这条腿真的会因缺失而红。C8-1/4 的差集/双向也都按"集合"实现，**没有把 17/19/18 写进断言**。
  - **audit 那条解除项已办**：`RedactionDetails` 现在拒空/非法 `Kind` 与空 `Match`
    （`CutPrefix` 全仓 grep 零命中）。`TestRedactionKindIsMandatory` 从 Skip 升成 5 行硬断言
    （空 Kind / 自造 Kind / 无 Match ⇒ 必须 error；key+Match、pattern+Match ⇒ 必须放行）+ 原值不入明细的检查。
    16:17 重跑：`go test -count=1` **24 PASS / 10 SKIP / 0 FAIL**（Skip 少一条正是这道升硬）。
  - **T-72 证形已换**：不再靠"我记得埋了什么标记串"，改为
    `git diff --stat HEAD -- core/test scripts .qoder/progress/test.md`（基线 `cd46e42`，
    用 `git log -1 --format=%h` 现取、不写死）⇒ 只列出 3 个文件是我本轮真动的，
    其余交付文件与库内基线逐字节相同。按你的界定：它只证"某些文件我没动"，
    "没改坏别的"仍由全包重跑承担（16:17 那次）。
  - **账本里我自己引用过的行号全部作废**（`redaction.go:127`、`store_sqlite.go:445` 已失效，
    `CutPrefix` 整条消失）：按 §1.1 第 14 条改判为符号引用。
  - **未覆盖清单（`待建`/归属在他人，不判红）**：C8-5 的线级那一半（等 REQ-T9 导出
    `process.Connection` 构造入口，现只有静态号码判据）；U13-6（等 backend 的 POST /tasks + 幂等回读）；
    U13-7 库里根本没有（归属列写明后我按同一形状补）。
    **【09-21 更新】此清单剩一条**：C8-5 线级已按 T-74④ 在真子进程帧面办结、REQ-T9 已撤回，
    不再等任何人；U13-6 仍 blocked on backend。

- **15:1x~15:24 轮（T-61 / T-66）**：
  - **T-66②③ 当场取证**：本轮 6 次变异的专用标记串逐个 grep `core/test`，**全部 0 命中**
    （`res.WriteEntry = true`、`committedRows  = 4`、`"signature":      func`、
    `"QUEUED", "CANCELLED", "BLOCKED", "INTERRUPTED"`、major 变异行、以及那份"已提交的 doomed"——
    `db.AggProject, "019e…999"` 只出现 1 次即合法的那条未提交行）。**变异版不在盘上，有据。**
  - **T-66 规矩钉进交付物本体**：`core/test/statemachine_test.go` 包文档加第 3 条
    （备份只落 `.scratch/<角色>/<具名>` 绝对路径、回滚后必须贴 0 命中证据、
    变异必须改变"是否走到被验证的那段代码"，藏在 skip/flag 早退后面的变异不算变异）。
  - **T-61 落点**：读了 §1.1 第 13 条"响应体口径"（`contracts-internal:154`）与第 14 条
    "契约只准符号引用"（`:171`）。按该节把 C18 的两条断言写成具名用例
    `TestIdempotentReplayBodyIsByteIdentical`（①201 body 与重放 body 逐字节相等；
    ②命中脱敏的值时两份都等于库里打码版），当前 Skip 点名 backend，不拿桩绿。
  - **两次误判被自己纠正（都记下来，因为形状很典型）**：
    ① 15:21 我跑 `./test/...` 编不过（`internal/adapter/stream.go: undefined: carriesTurnRef`），
       一度当成我的失败 —— 实际是 adapter 正在存盘（函数在 `stream.go:14` 已存在）。
    ② 15:25 再撞到同一文件报"syntax error"，注释掉了一个 `/`、`'` 变成 `.`，
       我当场写下"**这次不是 in-flight，是盘上真坏了**"——**60 秒后自证为 premature**：
       `gofmt -e stream.go` 返回 0、mtime 15:26:12，那只是一次保存中途的快照。
       教训：**单帧快照不构成"损坏"的证据**，要判损坏至少要看 mtime 是否停止变化 + 重取一次。
       这正是 T-66③ 说的"包突然红了可能是在途写作"，也是我自己天天在讲的"取证要打在待验那一层"。
  - 15:27:47 全套（交付前最后一次重跑）：`go test -count=1` **23 PASS / 11 SKIP / 0 FAIL**
    （11 是新增的 C18 骨架）；`check_ownership.sh` exit 1（4 命中）；`--selftest` exit 0；`crash_recovery.sh` exit 0。
    此前 15:23 那一次是 23 PASS / 10 SKIP / 0 FAIL，被 15:25 的在途编译失败夹断，不作为报绿依据。`check_ownership.sh` exit 1
    （4 命中 = REQ-WS-3）；`--selftest` exit 0；`crash_recovery.sh` exit 0。

- **14:5x 轮（T-52 `*.bak` 规则）**：本角色 `/tmp` 零命中已由主控 13:26 盘点确认；我另把自己那
  16 个一次性片段从 `.scratch/` 顶层归入 `.scratch/test/`，并把 `check_ownership.sh --selftest` 的
  三处 `mktemp -d` 改道 `-p "$SCRATCH"`（= `.scratch/test`），删除仍只针对自己刚建的那一个路径。
  **改完发现脚本已被我此前多轮补丁改坏**（两处 `printf` 的 `
` 变成真换行、一处 sed 吃掉收尾引号、
  `case` 分支里赋值串含 `(` 让 bash 解析器报错）—— 全部定位并修复，`bash -n` 通过，
  `--rules`/`--explain`/`--selftest` 复验正常（32 条规则、--explain 输出带规则来源）。
  教训：`.bak` 不是证据物，证据是仓库现状 + 重跑输出；我后面不再往 /tmp 放备份。

- **14:4x 轮（T-51 check 8 正式判据）**：
  - **①** 按盘复算与你的取证完全一致：§15.1=17、§15.1.1=19，
    `comm -13` = 恰好 `INVALID_PARAMS METHOD_NOT_FOUND`，`comm -23` = 空。
    check_8 改成**反向包含 + 差集恰好等于那两个**（不写 17==19、不写相等）。
    模拟验证：往 §15.1.1 多塞一个码 ⇒ 立即报红；给正确的两码 ⇒ 通过。
  - **②** 复核现有实现已经只在 `Core:*` 且「有常量=✓」的行上要求常量；
    `Adapter`/`result-frame` 行不要求 —— 9 条假红的来源已从判据里消失（上一轮就删了缺口清单）。
    顺带保留双向漂移检测：表说 ✓ 而包里搜不到 = 红；表说 ✗ 而包里有 = 红（并提示可能是投机常量）。
  - **③** 新增两条：表内自相矛盾检测（`result-frame` 行又标 ✓），以及
    "别处生产代码拿 `-32010` 造 error 回复"检测。**第一版我按码名 `CANCELLED` 搜 ⇒ 立刻假红**，
    因为它同时是 Task/Execution 的状态字符串；改成按**号码**搜（排除 protocol.go 与测试）后归零。
    行为级那一半仍要真测：`protocol.go:100` 已经把 -32010 标成结果帧专用，
    而 FakeAdapter 的 `$/cancel` 应答形状仍需线级断言 —— 卡在 REQ-T9（无导出构造入口）。
  - grep 边界按你的提醒处理了：取 §15.1 用 `/^### 15\.1/,/^#### 15\.1\.1/`（显式截到 15.1.1 之前），
    §15.1.1 单独取段且只认行首 `` `-3 `` 的行，所以数字行不会混进码名列。
  - 收尾：`check_8` 0 违规；`--selftest` 0；`go test -count=1` **23 PASS / 10 SKIP / 0 FAIL**；
    lint 剩 4 条命中 = REQ-WS-3 的 3 处 transport 层写表（同表两处）。

- **14:3x 轮（T-47 广播 /tmp 禁令）**：答复见报告。取证：`ls /tmp | grep -ciE '^r.*\.json$'` = **0**
  ——我没在 /tmp 顶层放过任何 `.json`，所以 backend 那次 `rm -f /tmp/r*.json` 没有我的受害件。
  但我这轮确实把一次性东西放到了 /tmp 顶层：`crash.bak`/`ac.bak`/`ac2.bak`（变异验证备份）、
  `fake-adapter.exe`/`fa2.exe`/`core.exe`（探针）、`ins.txt`/`new3.sh`/`fake.err`/`e2.err`/`coretest/`。
  其中三个 `.bak` **本次不是我删的却已经不在了** —— 共享 Temp 会被别人动，这条我记着。
  已按"只删自己建的具体文件名"清掉上面列出的其余项（不用任何通配），清完 `--selftest` 仍 exit 0。
  按 T-47 要求**不改代码**：`check_ownership.sh` 的 selftest 仍用 `mktemp -d`（落点在共享 Temp，
  但只 `rm -rf` 自己拿到的那个路径）；若要它改落 `.scratch/test/`，说一声。
  跨会话记忆新增一条 [[windows-shared-temp-not-scratch]]。
  **14:4x 补做**：我那 16 个一次性片段此前直接落在 `.scratch/` 顶层（违反新规矩），已全部
  `mv` 进 `.scratch/test/`；未碰任何其他角色的文件。

- **13:0x~13:48 轮（T-35 / T-41 / T-43）**：
  - **T-43**：本文件与报告里的时间戳全部改成 `date` 实取（上一条 13:48 即 `date -Iseconds` 输出）。
    历史记录不重写。
  - **T-35① 更正我的过期数字**：§15.1 = **17 码**、§15.2 = **18 行**（自取：
    `sed -n '/^### 15\.1/,/^### 15\.2/p' … | awk -F'|' '/^\| `/ {print $2}'` = 17，来自列 17）。
    check_8 的**判红部分改成契约 §15.2 末尾指定的算法**：码名集合双向比、不比行数，
    `version_conflict` 天然落在例外（它右列写 `—` 不进集合）。
    我上一版拿 §15.1 去比 Go 常量并判红，属"自己发明更聪明的算法"，已降级为**非门面的缺口清单**。
  - **T-35② 数字腿按 §15.3 规则 1/2 实现**：领域码必须落在 -32000~-32099，占用预定义码即报。
    实测现状：`protocol.go:51-67` 已把 17 个领域码排到 **-32000..-32016**，协议层用 -32601/-32602 ⇒
    **那 3 处越界（-32600/-32601/-32602）已经不见了，adapter 随 T-36 改完了**。
    规则 3（逐项相等）按你的指示标 blocked/INFO：§15.1 的数字列还是空的，不拿契约断言实现红。
  - **T-35③ REQ-T6 判定：是"断言写死了旧值"，不是真缺口**。
    成因就是我本会话删掉的那行 `t.Errorf(\`…"code":-32000…\`)`——把 JSON-RPC 数字写死在测试里；
    数字一改必红。现在改成结构化判据（`json.Number` 解不出即失败 + 与 Go 常量相等），
    同一教训也用到 check_8：门面只判契约文本内部一致性，Go 侧一律非门面。
  - **T-41① REQ-T3c 已归 audit（T-40）**，我那条 Skip 保持，落地即转硬断言（口径写在注释里）。
  - **T-41③ 扫描面扩到 core/ 全部 .go**（99→101 个文件），并取消"机制表整类跳过"。
    **立刻抓出 3 条以前结构性不可见的越界**（见 REQ-WS-3），`cmd/core/journal.go` 直查
    `event_journal` 的两处（:24/:54）现在也进了只读清单可见。
  - **T-41④ 分级落地**：缺口清单 INFO 不参与 exit code；`CANCELLED`（§15.3 规则 5，走结果帧）
    与 `DANGEROUS_FLAG_REFUSED`（生产点在 internal/process）两个豁免写明理由；
    exit 1 只留给真越界（当前 3 条 = REQ-WS-3）。

  ### 本轮我的三处错误结论 / 自伤（都要记账）
  1. **撤回**：我 12:4x 报的"9 个 §15.1 的码 Go 侧没有符号"**是错的**——锚点 `var codeNames` 已被
     adapter 换成 `var domainCodes`，我的提取空转 ⇒ 把 17 个码全算成缺口。现在加了"锚点找不到就报
     lint 自己坏"的守卫（`bad "…找不到 domainCodes/codeNames 锚点…"`），不再允许静默空集。
  2. **撤回**：我把 `.qoder/scratch/` 那 4 个 .py 归给 adapter（依据是 `grep -ln qoder/scratch` 命中
     adapter.md）。你 13:4x 说明那是**主控自己的**补丁器。教训：**"文档里提到某路径"不等于"谁写的"**，
     归属要靠文件内容/作者证据，不能靠一个 grep 命中。
  3. 同一个坑两次：`	` 在 grep 的 ERE 里**不是制表符**（`grep -E '^	Err'` 恒 0 命中），
     它先后让数字腿和配对提取静默空转；另外 python 里 ` ` 被转义吃掉一次、
     给违规消息加字段时把 `|` 分隔符打乱一次。都已在脚本注释里写下替代写法（`[[:space:]]`、awk 去引号冒号）。

- **12:2x~12:38 轮（T-31 三件）**：
  - **① 新增 check_8「错误码三方一致」**：§15.1 的码集合 ↔ §15.2 的「来自」列 ↔ `internal/adapter` 的
    `codeNames`，五条腿（A 规范有码 Go 无符号 / B 每个码必须有 HTTP 行 / C Go 多出符号须有出处 /
    D HTTP 列必须是三位数字 / E 「无 Adapter 来源」行只许 `version_conflict`）。
    按 T-31① 不做行数硬抹平：`version_conflict` 走例外腿 E，`INVALID_PARAMS`/`METHOD_NOT_FOUND`
    走已登记的传输层专用清单。
  - **实测结果与你的快照不同，报备**：§15.1 现在不是 14 个码而是 **17 个**（后加了
    `READ_ONLY`、`ADAPTER_UNRESPONSIVE`、`EXECUTION_STATE_UNKNOWN`），§15.2 相应 18 行——两表自身一致（腿 B 零命中），
    但 Go 侧只有 10 个符号 ⇒ **9 个码 Adapter 现在发不出来**（AUTH_REQUIRED / CANCELLED /
    DANGEROUS_FLAG_REFUSED / EXECUTION_STATE_UNKNOWN / PROVIDER_ERROR / RATE_LIMITED / READ_ONLY /
    SESSION_NOT_FOUND / TIMEOUT）。§10 要求 FakeAdapter 能注入故障，这 9 个补不补由你拍。
  - **② `migration_version` 不再写死**：`TestCrashKillCoreAndRestart` 改成
    "health 报的值 == 库里 `MAX(version)`"（拿 Core 死后的库直接问，不引用常量），
    这条正是 §12「UI 显示的状态与 Core 实际状态不一致」的一个实例。
  - **③(a) 代理坑当场踩到我头上**：我的 crash 用例里 `http.Client{Timeout:...}` 走默认 Transport，
    会读 `HTTP_PROXY/ALL_PROXY` 把 127.0.0.1 的请求转给本机代理 ⇒ 已改成
    `Transport: &http.Transport{Proxy: nil}` 并注明原因。③(b) 复核：本包**没有**任何文件权限断言
    （grep 0700/0600/FileMode 零命中），不新增。
  - **REQ-T8 已闭环**（复核）：`.qoder/scratch/` 现已被忽略（tracked=0、untracked=0，
    `git check-ignore` 命中），所以 lint 不再报它——不是我改的。
  - 修 lint 自己的两处误伤：§15.1 提取式原本把所有反引号大写词都算成码（`LOCAL_DATA`、`UNKNOWN`、
    `FAILED` 这些别的列的值混进来，20 vs 真 17）；§15.2 的图例行「领域错误的唯一数字码…真判别符在
    data.code」被当成 HTTP 码判成违规。都改成按列 + 按行型精确提取。

- **12:0x~12:13 轮（T-29）**：
  - T-29 的两条规范写进 `core/test/statemachine_test.go` 的包文档（全包共用一处，不复制多份）：
    变异必须打在**待验层的原始输出**上（返回值 / 它直接交给下游的那一行数据），
    禁止用下游消费者的判断给上游发绿。
  - 按这条规范给 `TestRedactionCountersAreSealedBeforeHash` 加了**自破坏探针**：把 audit 刚交给
    Appender 的那一行拿回来，在 hash 之后改 `details_json`，再让同一条判据去查 →
    必须同时看到"hash 不等"与"`VerifyChain` 拒"。这条探针读的是那一行本身，不借任何下游判断。
  - 全包自审结论（逐处核过断言对象）：状态机/投影打 `Check`/`ProjectTaskState` 返回值；
    链打 `Seal` 返回值与 Appender 收到的那一行；lint 打扫描器自己的输出（合成件）；
    Crash 打库里的行数与 health 响应体。没有"拿上一层验下一层"的残留。
  - **REQ-T3 复核后收窄**：`db.RedactionHit` 已带 `Kind HitKind`（redact.go:87、97-101），
    approval 的 mapper 改成 `audit.HitKind(h.Kind)` 直传、`CutPrefix("key:")` 已消失；
    Skip 理由相应改成只剩 ①`RedactionDetails` 不校验空 Kind（REQ-T3c 给 audit）。
  - **又一处我自己的假红，当场纠正**：`TestErrorCodeIsJSONRPCNumber` 用 `"code":"` 子串判线格式，
    而 adapter 把符号名合规地放在 `error.data.code` 里 → 我的断言误伤。删掉子串判断，
    保留 `json.Number` 解码那条结构化判据（code 若真是字符串就解不出来）——
    顺带说明：结构化判据本来就覆盖它，多余的第二条判断只是噪声源。

- **11:3x~11:59 轮（复批后的六项）**：
  - **T-D 两条从骨架变真跑**：`TestCrashKillCoreAndRestart`（build `cmd/core` → 起进程 → 读 `core.endpoint`
    → Bearer 打 `/api/v1/health` 拿到 `state=READY/migration_version=3` → `Process.Kill()`（≈SIGKILL）
    → 热 `-wal` 保留 → 平台层重开 → `PRAGMA integrity_check=ok` + tables≥33 + schema_migrations 记账在）
    与 `TestCrashRestartSameDataDir`（**同一数据目录、不加 `-take-over-lock`** 重启 → 换新 endpoint → 仍 READY，
    证明残留锁与热 wal 被自动回收）。两条都 PASS。
  - REQ-T5 **已闭环**：§7 11:25 补了 `core/cmd/fake-adapter/**`→adapter，我的运行期解析当场跟上
    （规则 30→31 条），那两条命中自动消失。REQ-T1/T2 的前提也已按盘复核为**不存在**
    （`packages/` 只剩 api-client；`git status --untracked-files=all | grep -c node_modules` = 0），不再报。
  - REQ-T3 我这边跟上新形状（`audit.RedactionHit{Column, Kind, Match, Count}`），
    并新增 `TestRedactionKindIsMandatory` 记录**未落的另一半**（三条实测行号见下）。
  - BUG-T6 **进展核实**：`adapter/initialize` 不再被自己的未握手门禁挡掉，握手能通；
    但 `quota/read` 回 `INTERNAL_ERROR: quota/read is not implemented in this build` →
    旗标开时我的子进程用例会红（如实报），旗标关时 Skip。
  - 脚本加了 **fail-closed 前置检查**：缺 `core/internal`/`core/go.mod`/契约③ 或 `.go` 文件数 <20 时
    `exit 2` 拒绝出报告，并在报告头打印扫描面（本次 73 个文件）——防的是"扫描面为空 → 每条 0 命中 → 全绿"这种
    比误报更危险的伪装成功。`--selftest` 里用「把脚本拷进空仓库副本」验它必须 exit 2。
  - T-25 两问的答案：**①** loader 的补丁全部落进脚本本体（`load_owner_table` / `OWNER_HOLE` / `ANYROLE` /
    `cross_package_refs` / `orphan_modules` / `api_prefix_diff` / `OWNER_ROWS` 七个标记逐个 grep 到）；
    **②** 不需要重跑——一次性补丁器，以后改脚本用按内容定位的直接编辑。所以删除比留档干净，谢谢改口认可。


- **T-A**（`scripts/check_ownership.sh`，7 秒跑完）7 条（17:2x 按主控要求改版）：①approvals SQL 局部性 ②跨包写表 +
  `Append(表名)` + `.Exec` 旁门 ③变更文件 vs 契约③ §7 所有权矩阵（含冻结件 `000001`）
  ④go.mod/go.sum 归属与来路 commit ⑤evidence/.scratch 未入库 + .gitignore 有效 ⑥依赖方向
  （eventjournal 无内部 import；platform/db 只 eventjournal；audit 不 import db）
  ⑦契约③ §6.3 三处同步：事件目录那一腿常判（18 个事件名全在契约① §6 登记），
  yaml↔契约③ 资源前缀双向差集在 `specs/openapi/core-v1.yaml` 落地后自动激活（11 对 11 一致）。
- **权威源改为运行时解析 §7**（17:3x 主控二次要求"不要内嵌旧副本"）：`load_owner_table` 直接读
  `specs/api-contract-v0.1.md` 的表格行，`owner_of` 查这张运行期表。踩到并修掉的三个真问题：
  ① `specs/*.md` 带 CR，键变成 `...
` 导致全表不命中（→ 全部 UNOWNED 的假红）；
  ② `${p%**}` 在 bash 里是空操作，glob 没剥掉（同上）；
  ③ §7 的缩写约定（`core/internal/task/**`、`execution/**`）里"不带目录的 `X/**`"必须展开成
     `core/internal/execution/`，且判据用"根目录存不存在"而不是"拼上去存不存在"——
     后者在 `internal/process` 还没被创建时会把 adapter 的包误判成无归属。
  现在解析出 30 条规则；新增 `--explain <path>`（打印归属 + 命中的规则）和 `--rules`。
  §7 尚未覆盖的三条走 `OWNER_HOLE` 补偿表并显式标注来源（`core/internal/message`→backend、
  `docs/adapters/phase0`→survey、`README.md`→主控），§7 补齐后应删这张表。
- **「排除主控自己的提交」落进判据**：author 过滤在本机无效（所有会话共用一个 git 身份），
  改成结构性判据——未提交集合 = worker 在做（判越界）；基线之后已提交且工作树不再改的 = 主控落盘
  （只计入归属分布）。今天的效果：主控的 12 个已提交文件不可能被报成 worker 越界。
  `--selftest` 用合成越界证明三类扫描器都会报警（含"拼接盲点"这条负向断言）。
- **T-B**（`core/test/statemachine_test.go` + `projection_test.go`）：§5.1(17 态)/§5.2(12 态) 独立转录，
  正向可达 + 全笛卡尔积反向拒绝双向对账；终态不可外跳（含→UNKNOWN）；UNKNOWN 只能 Recovery+证据 进/出、
  终态不得改判为 UNKNOWN；未知状态名必须拒。§5.3 八条投影 + §5.4 拒绝语义（Rejection 三字段完整、
  被拒时不得返回目标态）+ 投影结果必须仍是 §5.1 的边。
  **变异验证**：往我的转录表里插一条假边 `CREATED→INTERRUPTED` → 两处用例立即报红（已还原）。
- **T-C**（`core/test/adapter_contract_test.go`）：`subject` 接口 = 契约 seam，桩 `newInProc()` 走
  `internal/adapter` 真代码；§2.2 major 不匹配必拒且拒后仍 PROTOCOL_NOT_INITIALIZED、
  未握手 11 个方法全拒、三态投递未声明必须 DELIVERY_MODE_UNSUPPORTED（含臆造模式）、
  `quota` 空回复 = `observations:[]` + UNKNOWN 且 JSON 面不得出现百分比字段、
  write_entry:false 拦全部写方法（UIA 显式声明者例外）、debug.* gating 只准 fake+EXPERIMENTAL、
  error.code 线上必须是整数（CO-4）。
  **变异验证**：把 major 不匹配改成匹配、把 `WriteEntry=false` 改成 true → 立即报红（已还原）。
- **T-D**（`core/test/crash_test.go` + `scripts/crash_recovery.sh`）：
  `TestCrashProcessDeathKeepsDurableData` **真跑**——子进程提交 5 条后在第 6 个事务中途 `os.Exit(9)`
  （Windows 无 SIGKILL，等价注入），父进程重开同一批文件（含热 -wal）断言
  `PRAGMA integrity_check=ok`、`journal_mode=wal`、projects/event_journal 各 5、
  未提交那条在**两张表里都不存在**、version 全为 1。
  **变异验证**：让 doomed 那条真提交 → 三条计数断言同时报红（已还原）。
  注意：`committedRows` 单独改动不会被发现（父子共用常量），所以补了独立于计数的 doomed 缺席判据。
- 交付物：`scripts/check_ownership.sh`、`scripts/crash_recovery.sh`、
  `core/test/{statemachine,projection,adapter_contract,crash}_test.go`

## Remaining

- REQ-T3 **已全闭**（09-21 复核）：`db.RedactionHit` 带 `Kind HitKind`、approval 的 mapper 直传
  `audit.HitKind(h.Kind)`、`audit.RedactionDetails` 现在拒空/非法 `Kind` 与空 `Match`；
  `TestRedactionKindIsMandatory` 上一轮已从 Skip 升成 6 条硬断言并 PASS。**这条不再等任何人。**
- FakeAdapter 的 §14 全量：`adapter/initialize`、`adapter/health`、`quota/read`、`session/send`、
  `debug/{crash,fail,hang,unknown_quota}` 已实现（实测 26 PASS 里含真子进程两条），
  其余方法仍落 `not implemented in this build`（default 分支）—— 等 adapter 补齐后我把
  `subjects()` 追加第二条被测面，用例本身一行不用改。
- **U13-6 与 §5.4 记录**：`TestIdempotentReplayBodyIsByteIdentical`、`TestProjectionRejectedJournalAndAudit`
  仍 Skip，前置都是 backend 的 `POST /api/v1/tasks` + 幂等回读 + `projection_rejected` 产出方。
- T-D 剩余 4 条：interrupted/recoverable 识别（Recovery Engine）、kill Adapter 不带崩 Core（Process Supervisor）、
  Idempotency 不重发、Approval 不丢、Journal WS 补发
  ——`cmd/core` 现在只有 health + ws-ticket + 单实例锁，其余 501，断言清单已在各 Skip 里。

## Blockers

- **T-D 剩 3 条 blocked**（11:5x 更新：5 条里已有 2 条变真跑）：
  `TestCrashRestartIdentifiesInterrupted` / `TestCrashNoDuplicateMessageAfterRestart` /
  `TestCrashApprovalNotLost` / `TestCrashEventJournalComplete` → 等 **backend** 的 Recovery Engine 与
  带副作用的 HTTP 端点（`cmd/core` 现在只有 health + ws-ticket + 单实例锁，其余 501）；
  `TestCrashAdapterKillDoesNotTakeCoreDown` → 等 **adapter** 的 Process Supervisor（第 3 轮已派）。
- `TestFakeAdapterSubprocess` / `TestCancelReplyIsResultFrame`：**不再 blocked**（09-21 实测旗标开启
  两条都 PASS，握手 + `quota/read` + `$/cancel` 都通）。默认关闭时它们 Skip —— 因为要 build 子进程，
  不该让每次 `go test ./test/...` 都付那个钱。
- **C8-5 静态半边今天无证据**（不是阻塞、是没货）：`core/cmd/fake-adapter/` 的生产代码里
  `"CANCELLED"` 出现 0 次，所以那条"不得出现在构造 error 帧的位置"的 leg 只能报 INFO 暂无证据。
  判据本身有合成件自证（`--selftest` 的 bad.go/ok.go 两条反例），等 adapter 真上线该词时自动有牙。
- **T84-7b（丢弃落成一行审计 + `event_journal` 零新增）blocked 在 backend 第二段**（09-22 23:2x 更新）：
  第一段（`adapter` 的 `Recorder.RecordDrop` + `DroppedEvent`）22:5x 已落地，我这半已经跑通（T84-7a）；
  剩下的是 `cmd/core` 里那个真正的 `process.Recorder` 实现（账本 T-141 写 23:0x 在途）。
  **这条不再阻塞 T84-7 整体**，也不允许我拿 `internal/process` 的 `stubRecorder` 去判产品行为（T-29）。
- `TestProjectionRejectedJournalAndAudit`：§5.4 的两条记录仍无产出方（`projection_rejected` 全仓只命中注释）；
  已归入 backend 本轮 `task.Service` 动作集，我代做等于替它写业务。
- `TestWriteEntryOpenQuestions`：等主控裁 Q1/Q2（见 Coordination）。
  `TestCoreReadyEventIsJournalized`：等 backend 落 REQ-T7。
  ~~`TestRedactionKindIsMandatory`~~ **已不再是 blocker**（09-21 复核：REQ-T3 全闭，它升成硬断言并 PASS）。
- ~~`TestDispatchedProviderHandleUnruled`（Q3/Q4）~~ **已闭**：主控 12:4x 裁了 §4.2 两处，
  T84-4/T84-5 已转硬断言并 PASS（13:07 那轮）。
- **`TestTurnRefExecutionCrossCheck`（T84-6，新 blocked）**：裁定 ① 的"turn_ref 与 execution_id 不一致 ⇒ 丢弃"
  那一腿今天证不了 —— 校验住在 `internal/process` 的未导出函数里，`Connection` 只能经
  `(*Supervisor).Connect` 真起子进程得到，而 FakeAdapter 无法被要求"故意发一个与绑定不符的 execution_id"。
  等的是**一个注入面**（adapter 的 `debug/*` 或 process 的可注入帧入口），不要导出 `Connection`。
  三条期望断言已写进用例注释，落地即改硬。

- **被验树此刻编不过（09-27 12:18:38 起；非回归、归 backend 在途）**【补正 14:13：这一条已不成立 —— 14:10:03
  `go vet ./test/` exit 0、`go test ./test/ -count=1` 能跑完整包（唯一 FAIL 是我的判据本身），
  `2254cf8`（14:04:54）把 connection/supervisor 那半笔收进册后能编了。留原文不删，只标失效时刻】：
  `core/internal/execution/send.go` 单文件 ` M`，
  `go build ./internal/execution` 报 `send.go:153/158/161/168/174: cannot use SendResult{} as SendReservation value`；
  `SendReservation` 工作树 4 处 / HEAD 0 处 ⇒ 这是 T-346① 那一搬的中间态。
  **影响面说全**：`./test/` 整包 build failed ⇒ 我的新用例两条腿都跑不了；反过来**我的在途件同样会吃掉别人的全量绿**
  —— `core/test/event_producer_roster_test.go` 还没被类型检查过，backend 收口后第一次全量若红在我这个文件上，
  那是我这一轮的账、不是他们的，我在同一轮内修。
  解锁 = backend 交账（一次动作同时解锁②两腿、③分诊、①判据）。

## Coordination

- **REQ-T15（T-135/T-141 交付回写，判据表文本 —— 请主控落进契约② 判据表，我不改 `specs/**`）**：
  **→ 09-23 00:1x 已办结（账本 T-153）**：五行全部落进契约②，`①′` 按我的 M-C 探针改写并被采纳；
  我要的那个口径主控裁了 **"不升红"**（把码改成常量/变量传是合法 Go 风格，为它开红等于惩罚合法重构；
  真正的不变量用**类型**收口比用 lint 收口强），并把**重开条件**写进 C8-11：
  "非字面量传码"从 0 变成 ≥1 时动类型或明写例外，**不许回来把这条改成红**。今天仍 0 处、仍只打印。
  另外这条口径被升级成**契约① §1.1 第 15 条**（不止 T84-7）：判据表命令列一律 `^全名$` + 显式包路径。s/**`）**：
  - **C8-8 行**：状态改 **`已被 C8-10 取代（09-22 23:2x，不是并列）`**，正文前面加一句
    「本行把 29/12 写成了字面闸门，09-22 被自己的一次合法扩列（C28① 收 `duplicate_project_path`）判红」。
  - **C8-10 行**：状态 `待建` ⇒ **`现存（09-22 23:19:03 实跑：§15.1=17、§15.2=30=码名 17+— 13，双向相等）`**；
    ①那半句建议改写成 **"①′ 总行数 = 码名数 + `—` 数，且『长得像表格行却没被计入任何一类』的行数为 0"** ——
    探针 M-C 证明只写求和式碰不到"整行没打反引号"这种失效（三个数一起少、依旧闭合）。
  - **C8-11 行**：**命令格现在没有可跑命令**（写的是"`bash -c` 不用；解析与比对都落在 Go 用例里"），
    请补 `go test -count=1 -v -run TestTransportCodesAreInContract ./test/...`；
    状态 ⇒ **`现存（23:19:03：4 个生产 .go、13 个字面码全部 ∈ §15.2 的 30 个左列码）`**。
  - **C8-12 行**：状态 ⇒ **`现存（23:28:29：目录 62 类 ↔ 常量 62 个，两个差集都空 ⇒ 今天不红）`**，
    并把"第一天会红在 `notification_read`"这句改掉 —— db 在 `5d6ac66`（23:10）已补齐
    `NotificationRead` 与 `task_paused`/`task_blocked`/`task_resumed`/`execution_paused`，
    预期红因此**没有发生，且不是因为判据钝**（M-F/M-G/M-H 三例各咬得住）。
    建议在同一格补一句：**"另有 `db` 的包内对拍 `core/internal/eventjournal/contract_parity_test.go`
    （`TestCatalogMatchesContract`，23:10:05 复跑 62 类 PASS）—— 它比 `catalog` map、本行比 `const` 声明，
    两个观察面不重复。"** 若主控认为一条判据只留一个执行者，请明说删哪条，我不擅自合并。
  - **T84-7 行**：拆成两格状态 —— `T84-7a 丢弃必须到达 Recorder 接缝（现存）`，命令按 §1.1 第 15 条写成
    `LUCERO_TEST_FAKE_ADAPTER=1 go test -count=1 -v -run '^TestDroppedFrameReachesAuditSeam$' ./test/`
    （**请把包路径一起写上**：`internal/process/connection_test.go` 里 adapter 自带的那条叫
    `TestDroppedFrameReachesTheAuditSeam`，只多一个 `The`，两条并排读会认错归属——
    Go 的 `-run` 是无锚定子串匹配，写全名 + 写全包才是可复现命令。
    **00:12:22 我把这条实测了一遍**：`-run TestDroppedFrameReaches` 对 `./test/ ./internal/process/` 两包同跑
    ⇒ **两条近名用例都被命中**（两份 `--- PASS` 分别来自两个包）；换成 `^…$` + 单包 ⇒ 只命中我这条，
    且 **两个子用例照常跑**（7a PASS 0.89s / 7b SKIP），也就是"锚定不会把子用例一起掐掉"。
    主控把这条从"我这两行"升级成了**契约① §1.1 第 15 条**（通用口径），于是我自己账本与两个脚本里的
    命令也一并改成锚定形——见 09-23 00:1x 那条 Completed。）
    实测：2 条 `DroppedEvent`（`turn/claimed` + `turn/dispatched`），帧内 id 与票号绑定 id 两个不同值都在。
    `T84-7b 一行审计 + event_journal 零新增（SKIP（blocked）：等 cmd/core 里的 process.Recorder 实现）`。
  - **一个要主控裁的口径（不裁我就按现状留着）**：C8-11 只判**字面量**码。哪天有人把码改成常量或变量传进来，
    这条腿就看不见（用例已经把次数打印出来留痕：今天 0 处）。要不要升成红，是契约的决定，
    我不在自己的判据里替契约加严（今天已经为这件事红过一次自己的口径）。
- **REQ-T16（新，两问，都关于"摘 SKIP 的触发条件"到底算什么）**：
  - **①触发条件的口径对不上，请主控定一句**。T-163 说"backend 的 `RecordDrop` 一进 `cmd/core` 你就摘 SKIP"，
    T-168② 又说"你的 `RecordDrop` 一入册（`753900f`）就是 `test` 摘 SKIP 的触发条件"——
    按这个口径**触发已经发生了**（实现确实在 `753900f` 里）。但我 01:11 实测：`cmd/core` 的**生产码里没有一处
    `newDropRecorder(` 的调用点**（`processpipe.go` 里 `Rec` 仍是 nil，backend 自己的注释写着"等 probe 落地，
    装配点就是下面这一格"）。**"实现入册"≠"挂上桥"**，我今天把门建在后者上。请确认这个口径，
    否则下一轮会出现"主控以为我该翻了、我以为还不能翻"的对不上。
  - **②真黑盒还缺一个注入面**：就算 `Rec` 挂上、probe 落地（Core 有了活连接），
    我也**无法从外部造出"票号说 B、帧里写 EVIL"那一发** —— 绑定关系是 Core 自己持有的，
    FakeAdapter 只回显 Core 给它的 `execution_id`。要能让真链路上出现矛盾帧，需要 adapter 侧给一个
    门控开关（`debug/*` 或 `LUCERO_FAKE_ADAPTER_BAD_EXECUTION_ID=1` 之类）。
    这条我不自己发明（那是别人的包），请主控派给 adapter 或直接裁定"T84-7b 只到 7a 为止"。
  - 附带一条现状（不是请求）：`DroppedEvent.ProcessRole` 已被 T-155 删掉，而契约③ 的字段清单里还写着它
    —— 代码注释自己标成"契约侧待办"。我 7a 当初**没有**断这个字段（依据只在 adapter 的注释里，不在契约里），
    所以删字段没让我红；但如果哪天有人按契约③ 补回来，7a 的字段清单要跟着看一眼。
- **REQ-T12 → 我不再拿它当整条阻塞（T-141 的三段归属我照做了）**：第一段（`adapter` 的 `RecordDrop` +
  `DroppedEvent`）22:5x 已落地，所以"丢弃到没到接缝"这半今天就有判据（T84-7a，已跑通）；
  第二段（`backend` 在 `cmd/core` 注实现）我**不替它伪造**，落成显式 SKIP 的 T84-7b。
  剩下唯一还在别人手里的是第二段的实现，不是"这题没人做"。
- **REQ-T18（T-216 要我写的那行量级判据，文本可直接粘进契约① §1.1 第 19 条；我不改 specs）**：
  - 命令：`bash scripts/flaky_watch.sh -r TestCrashRestartSameDataDir -p ./test/ -n 50 -m single`
    （**`-r` 不是 `-run`**；`-m single` 是本轮新加的，别省 —— 不选档就是四档连跑）。
  - **档的选法与理由**：串行就能复现的故障只跑 `single`；只有当怀疑并发形状时才另跑 `-m parallel,isolated -n 12`
    （成本实测：单档一次绿样 2~3s、一次红样 ~28s（用例自己烧满 25s 窗口）⇒ `n=50` 全绿约 2~3 分钟，掺 k 次红加 28k 秒；
    四档 ×50 = 200 次，05:32 实测 10 分钟没跑完）。
  - **n 的下界按发生率反推**，不是拍数字：`n ≥ ln(0.05)/ln(1−p̂)` ⇒ p̂=6% ⇒ **n=50**；p̂=10% ⇒ n=30。
    **旧口径为什么不够**：`n=3` 时 6% 事件的漏检率是 `0.94³ ≈ 83%` ⇒ 以前那句"未复现"里八成是"没采到"，
    我 00:05 那次 12/12 全绿就是这一格 —— 它当时被读成"大概没事"，今天看是统计上没有证据。
  - **请一并登记的现状（发生率漂移，不推因果）**：本轮累计约 70 次采样里红 7 次 ≈ 10%；
    前段（05:02~05:08）4/65 ≈ 6%，后段（05:45~05:52）三趟各含 1 红。**并列观察**：同一时段 backend 在改
    `cmd/core`（`lifecyclecolumns_test.go`、`probeassembly_test.go` 的 mtime 都在这段）。
    "负载变高"与"改动引入"我分不开，需要 backend 交带时刻的复采才谈得上定档。
  - **验收第一条我应下**：backend 交 REQ-L-1 时做双向自证 —— 摘掉新判活规则 ⇒ 该用例必须红；
    装上 ⇒ `n=50 -m single` 必须 0 红。今天先记一条**反向已成立**的证据：05:52 那趟仍 1/2 红（规则还没改）。
  - 措词账（第 3 件）在案：REQ-L-1 未闭之前，我这边任何"崩溃注入通过"的说法都会带上"基线 4/65、REQ-L-1 未闭"。
  - **09-24 07:1x 就地更新（T-223 ②：这条从"契约正文里的一句话"升成 FL 表的一行）**：上面那条 `-n 50`
    已被下面的 **REQ-T19 的 `-n 122`** 取代，命令形状与档不变。**为什么把 50 抬到 122**：50 只配得上基线的
    **点估计** 6%（`ln 0.05 / ln 0.94 = 48.4`），而我自己在契约里写的就是那个 4/65 —— 它的 Wilson 95% 区间是
    **[2.42%, 14.78%]**。按**下界**反推要 122 次。**拿点估计钉次数 = 拿自己最不确定那一端当预算**，
    这一格昨晚是我说别人"n 配不上量级"，今天要用在自己身上才算数。
- **REQ-T20（T-223 自证跑出来的工具缺陷，请主控把 FL-6 的口径扩半句；我不改 `specs/**`）**：
  - **✅ 10:32 T-270③ 已裁并落文两处**：FL-6 扩了"拿到 `--- SKIP` 等同于没拿到 ⇒ 探测不成立 exit 2"，
    契约① §1.1 第 19 条那句也补成"**n = 真跑过的次数**、SKIP 不算样本、发生率按有效样本换算"
    （我 10:4x 在盘上读到原文，不是转述）。工具侧早已实现并有 3 条自测托着。
    **但落文时 FL-6 被另起一行而不是就地改 ⇒ 现在表里有两个 `FL-6`，见 REQ-T25。**
  FL-6 现在只把"**没拿到该用例的 `---` 行**"算作探测不成立。09-24 07:33 那批 122 次采样里 **9 次返回 `--- SKIP`**
  ——我的 `buildCore` 在 `cmd/core` 编不过时按设计 `t.Skipf("blocked: build cmd/core failed (backend in flight), 不拿桩绿冒充真跑")`，
  那 9 次**一行断言都没执行**，而旧 `classify` 照样印"各 122 次都没红"（9 次连跳、每次约 1.1s，远短于正常 2.4s，
  时刻 07:33:45~07:33:55 —— 与 backend 在 07:32 改写 `cmd/core/lock.go`/`core_test.go` 的窗口重合）。
  **SKIP 既不是红也不是绿，它连样本都不是**，所以：**§1.1 第 19 条验收② 的 n 必须是"跑过的次数"**，
  建议 FL-6 那句扩成"没拿到 `---` 行 **或拿到的是 `--- SKIP`** ⇒ 探测不成立，exit 2"。
  工具侧我已经改完并自证（`flaky_watch.sh`：见 SKIP 即 exit 2、红与 SKIP 并存时按有效样本换算发生率、
  红/SKIP 都自带现场；`--selftest` 从 10 条涨到 13 条，变异验过：把门改成 `> 999999` ⇒ 那两条立刻红、
  并且**旧的那条"未复现"判决原样复活**——这就是它能骗过验收的现场）。
- **REQ-T21（一条契约措辞的第三态，请主控裁；这是设计问题，不是谁的实现错了）**：
  第 19 条修法 (a) 那句诚实处置写的是"绑不上时对该端口发一次 `/health`，**答得上来才报 `ErrAlreadyRunning`，
  答不上来就是'端口被别人占了'**"。backend 照做了（`lock.go:100~110`、`healthAnswersAt`）。但这句话把世界分成了两半，
  而盘上至少有三态：**一个还在启动中的 Core 也答不上来** —— `main.go` 的顺序是
  绑端口（`:129`）→ **拿锁**（`:133`）→ 开库 → 迁移 → 才 `Serve`（`:116` 的注释自己列的就是这个顺序），
  所以"端口已绑、HTTP 还没开始应答"这段窗口里，`healthAnswersAt` 连得上但对不上（2s 期限后返回 false）
  ⇒ 第二个 Core 会把**同一个二进制还在启动的自己**判成"别的进程占着端口"。
  后果不致命（第二代拒绝起，不会双开），但**报错原因又假了一次**，而"不许复用假原因"正是这条 REQ 的立身之本。
  三条可选处置（都不在我范围内，我只登记）：① 把 `/health` 提到迁移之前就开始答 `BOOTING`
  （`api/http/handler.go:22` 那个 `StateBooting` 词已经在了，`Health` 结构也已经带 `ready`）；
  ② 探针超时与"连不上/答非所问"分两码（`ErrPortUncertain` vs `ErrPortTakenByOther`）；
  ③ 认了这一态，契约里明写"启动窗口内第二个实例会拒起并报 `ErrPortTakenByOther`，这是可接受的保守失败"。
  **今天没人需要它**：我的崩溃用例是"杀掉一代再起二代"，走不到"两代同时在启动"的那格 —— 登记它是因为
  下一句会用到：一旦有人给 Core 加"重复启动自动等待"，这条就会变成真路径。
  - **✅ 09-25 18:2x 结案（我自己复量，不是转述）**：backend 选的是我三条之外的第四种形状 ——
    **有界重试**：`lock.go` 绑不上端口后走 `waitForHealth(port, g)`（`lock.go:142`、`:153` 那句注释自称"有界重试那一腿"），
    在宽限期内轮询探针，所以"还在启动的 Core"最终会答上来、不再被误判成 `ErrPortTakenByOther`。
    盘上没有 `ErrPortUncertain`/`holderBooting` 这两个我提过的符号（各 0 命中）⇒ 实现既不是我列的 ① 也不是 ②。
    **而且这一格有用例托着**：`core/cmd/core/lock_test.go` 六条里有 `TestThirdStateBootingCoreIsNotJudgedTakenByOther`。
    ⇒ REQ-T21 我这边**销账**；它归 backend（T-271 的归属裁定），我只是提出问题的另一方。
- **REQ-T27（19:0x 新，FL-10 门② 的"被验面"口径 + 判据行改到一半就有人在跑的时序问题）**：
  - **★ 本条第一问已被 T-299 答掉（19:0x 投递）**：主控以"`core/` 面无未提交改动（backend 那 5+2 件已由我入册 `a9f975e`）"
    作为放行 FL-10 的条件 ⇒ 口径 = 我列的 **⑴（`core/` 全量）**，不是我倾向的 ⑵（依赖闭包）。两种读法在这批上结论相同
    （都判不合格），所以我不重问；只留一句：**按 ⑴ 更严**（`core/` 里任何一个 `.go` 未提交都拦，哪怕它不在 `cmd/core`
    的依赖闭包里）⇒ 以后我按 ⑴ 起判，**本问销账、不必再裁**。三种读法留档免得下一位重推：
    ⑴ `core/` 全量 → 不合格（未提交 7 处）；⑵ `cmd/core` 的编译依赖闭包 → 仍不合格（在途的
    `internal/task`、`api/http`、`cmd/core/sendpipe.go` 都在闭包里，`build_stamp.sh` 数的 2350 个编译输入含它们）；
    ⑶ 只指 REQ-L-1 改过的那份 `core/cmd/core/lock.go` → 合格（md5 `6c379e1c`、归属 `2c56023`）。
  - **19:08:10 补一个事实，它把那 124 次的处境说清了**：backend 交账后 `core/` 干净、重取戳 =
    `HEAD=f25ee6c 指纹=6b36e9e3930864b5f6c17fcec38e1ff9` —— **与 18:48 那批四段八个戳逐字相同** ⇒ 那批采的编译输入
    就是现在已交账的这版码（"编进未提交在途码"这件事在这批上没有改变结论：它后来被原样交上来了）。
    但这不等于当时门满足过（门① 没跑、门② 当时是"未提交"）⇒ **19:08:21 我起了一批新的 122 次一次跑完**，
    起跑时两道门都在（预检已长进采样器、`core/` 干净）⇒ **交账以新批为准，124 那批只当旁证**。
  - **时序问题（不追责、只补缺）**：`4b4739e` 在 18:47:07 给 FL-10 行补了两条门，我 18:48:58 起跑，差 **1 分 51 秒**，
    我按的是 10:3x 那一版文本 ⇒ 结果就是"跑完才发现门换了"。可复用的补法我先落在自己这边：**起跑前现读 FL-10 行文本、
    把读到的版本并入证据头部**（这批的头部已经补了自评估，但是事后补的）。
    要不要再加一条"改判据行的当场须点名在跑的采样方"，归主控裁。
  - **门① 我已经不需要别人配合**：它长进 `scripts/flaky_watch.sh` 了（新增 `preflight()` + 三条自测腿，58 行、未提交待入册）。
    如果主控要 FL-10 行的命令列/预检句改成"由 `flaky_watch.sh` 自带、无跳过开关"，那是 `specs/**` 的改动、我不自改；
    现文本我照跑，只是提醒一句：**门在采样器里比在表里少一次"记得"**。
- **REQ-T28（作废 —— 不另开一条）**：T-293④ 问的那格与 **REQ-T25 是同一件事**，答案、基线读数与两条形状坑全部并进 REQ-T25（19:2x）。我自己正在报「重号登记」这个缺陷，就不该先在台账里长出第二条同格条目 ⇒ 本行只作编号对账用，主控要落文请只看 REQ-T25。
- **REQ-T26（T-273⑥ 要的 F20-1 镜像文本；按 ③ 的格式给：可粘文本 + 自证读数。我不改 `specs/**`）**：
  - 可粘行（四列，与 F20 表同形；顺带把命令列补上锚，这是 §1.1 第 15 条要求的形状）：

```
| F20-1 | 只给 `?from_seq=` + `hello` 省略游标 ⇒ 首帧 `hello_ack{server_seq}`、**不补发历史**。两条对照腿同断、缺一条就是半边的：① 省略游标之后**新提交的事件仍要实时推下来**（"不补发"不许靠"干脆不推"装出来）；② **显式 `from_seq=0` 必须补发全部**（缺这条，①与"没补发"都可能只是帧解析失明的假绿） | `go test -count=1 -v -run '^TestHelloCursorOmittedOverridesQuery$' ./test/` | 现存（PASS；三段各自落地：首帧类型 + `server_seq` 等于替身行数、省略时若补发即判红、新事件必须作为 `event` 帧到达、显式 0 ⇒ 补发 `[1 2 3]`）/ test |
```

  - 自证读数（11:07:43 本机，`-run '^TestHelloCursorOmittedOverridesQuery$'` 单跑）：`--- PASS`、包 `ok 0.230s`；
    断言落点在 `core/test/ws_cursor_contract_test.go` 内该用例体内，四处 `t.Errorf/t.Fatalf` 对应上面四件事
    （首帧类型 / server_seq==journal 头 3 / 省略时不得补发 / 新事件必须到达 / 显式 0 补发 `[1 2 3]`——**五行**，
    其中"首帧类型"与"server_seq"是同一条腿的两半）。
  - **另两处一起求你顺手**：① F20-2/F20-3/F20-4 的命令列同样没有 `^…$`，四行一起补最省事；
    今天这三条 pattern 各自只匹到一个用例（11:09 现量），但 `TestHelloCursorOverridesQuery` 与
    `TestHelloCursorOmittedOverridesQuery` 只差中间一个词 —— 哪天再长出一个名字就会把别人的 PASS 记到错的行上；
    ② F20-3 那行的"两个方向都判"建议照我文件头的说法写实：**第二个方向（query 大、hello 小）是专门用来抓
    "取较大者"那种看着更保守的实现的**，不写就容易被读成一个方向。
- **REQ-T25（10:4x 新，判据表本身缺一条"ID 不许重号"的腿；顺带报一个已经发生实例）**：
  契约② 判据表 FL 今天有**两行 `FL-6`**（`grep -c '^| FL-6 ' specs/adapter-protocol/adapter-protocol-v0.1.md` = 2，
  旧版一行 + REQ-T20 扩句版一行）⇒ 扩句时被另起一行而非就地改。**按 ID 引用判据的地方从此有歧义**。
  两问请裁：① 要不要我加一条"**判据表 ID 列重号即停**"的机器判据（FL / C8 / W13 / F20 / U13 / A18 / T84 都在这个形状里，
  我的 `firstDup` 现成，半轮能落，含双向反例：重号必报、合法增行不误伤）；② 还是先手工并行的两行、不加闸门
  （成本更低，但同类漂移还会再来一次——今晚这套表已经被"重贴/扩行"咬过两回）。
  **我不在没人点头的情况下自己给别人的表加判据**，这条按你定的顺序走。
  - **19:2x 补：T-293④ 问我"说一句"的就是本条，答案在这里（不另开一条 REQ —— 我正在报"重号登记"这个缺陷，
    自己先长第二条同格条目就是打自己脸）**。**答：加，放 `scripts/check_ownership.sh`，不要新开第五个入口**。
    三条理由：① 那几份契约它已经在读；② 2/9（W13 词表）与 8/9（§15.1↔§15.2 码名对拍）本来就是同一族"数落点"的门，
    ID 唯一性是它们的第 0 步；③ **§6 的展开约定 ①~⑤ 已经在那条事件腿里实现过一次** —— 新开脚本就会长出第二份展开规则，
    而"同一个词表两处各存一份"正是 09-25 裁过要避免的形状。嫌 `check_ownership.sh` 名字不贴（它叫"所有权 lint"）
    ⇒ 改名/拆分归主控，我不动名字。
  - **基线（19:22:52 现量，行首表格形状 + 带字母后缀，扫 `specs/` 全部）**：
    FL 10/10、C8 14/14、T84 8/8（都在契约②）；W13 3/3、F20 4/4（契约③）；U13 7/7、A18 3/3（`specs/contracts-internal-v0.1.md`）
    ⇒ **七族此刻全唯一**，也就是这条腿现在挂上去是 0 红的静态门。
  - **★ 这条腿的形状坑（我自己刚才当场踩到、又自己捞回来的，必须写进判据要求）**：取 ID 的正则**不许少掉字母后缀**。
    我第一次用 `^\| T84-[0-9]+` 量出"字面 8 / 去重 7" ⇒ 差点报成"契约② 又多了一处重号"；
    实际是 `T84-7a` 与 `T84-7b` 被截成同一个 `T84-7`。换成 `^\| T84-[0-9]+[a-z]*` 后 8/8。
    ⇒ 判据必须自带**正向对照**：`T84-7a/7b` 这一对不许误伤（否则第一天就把人逼去改表体喂判据 —— 那是我们裁过的更坏结果）。
    另一侧同族坑：散文中也出现同一批 ID（`**A18-3` 那种加粗小标题、`CO-nn` 的裁定编号），
    **只有"行首 `|` 的表格行"算登记**，跨形状的引用不进集合 ⇒ 这条腿的覆盖面就写死成"七族 × 行首表格"，
    不宣称管住所有 ID（`CO-*`/散文引用它天生看不见，这不是缺陷是边界，要在 INFO 里说明）。
  - **三条腿（§1.1 第 16 条形状，缺一条我就不写）**：① 每族"字面计数 == 去重计数"；
    ② 反例 = 往某张表插一行重复 ID（内容随便），门必须喊出是哪族哪个 ID；
    ③ 摘掉这条腿整包仍 ok ⇒ 证明它没被别的检查顺带掩护。两次变异输出必须与基线两两不同。
    **只判 ID 列，不判表体语义、不把任何一行正文抄进脚本当期望值**（validator 内嵌契约快照那个老坑不再踩）。
    裁定后我做：一条 `check_N` + 合成件，插进现有 selftest，条数照旧自增不手写。
  - **★ 结案（T-316④ 已裁已落地）**：门进了 `scripts/check_ownership.sh` 的 `check_id_unique`（标 9b/9、没改 /9 号），真树 **49/49 无重号**、三把变异锤各自打红、还原残留 0 ⇒ **本条销账**。两处与裁的文本有偏差，是我在自己地盘里自决并在此报备：⑴ 没把 /9 改成 /10（用既有子编号，因为别人账上写着 2/9、8/9，改号会把那些引用全变成假 0）；⑵ 引用一律写「§1.1 第 16 条」而不写 `§1.1.16` —— 那是编号行，按字面 grep 必 0 命中。
- **REQ-T24（T-257② 的副产品）**：主控 09:4x 已裁（权威 = 事件契约 §6，两个名字本来就该是两个；
  引用侧必须写完整名、不许简写；比对前必须先走 §6 展开约定 ①~⑤）。**裁定我认，但这条账里有一格是我自己写错的，就地标明**：
  - **我那句"§6 只登记 `execution_approval_resolved`、缺 `approval_resolved` 那一行"是错的**，
    错因正是我自己前一小时才写进脚本注释的那条机制：**按字面 grep §6 必然漏掉合并行**。
    10:0x 复量读到 §6 第 53 行：`| approval_requested / _resolved / _expired / _cancelled | approval | 同上 | ✓ |`
    ⇒ 展开后 `approval_resolved` **一直在目录里**；同理 `execution_succeeded` 在第 42 行的合并行里。
    **我这条"发现"的两个候选，两个都是字面 grep 造的假红** —— 而我自己的 Go 用例（C8-12/C8-13 会先展开再比）
    从头到尾没报过这个差集（64/64、两个独有集合都空）。写下来是为了让下一个"顺手 grep 一下 §6"的人不再栽同一个坑。
  - **我自己地盘里的引用侧我复核了**：`core/test/projection_test.go` 用的是完整名（`execution_approval_resolved`）✓；
    另外把我 selftest 合成件里那个借用真事件名的 `approval_resolved` 换成了 `zz_probe_event`（合成件就该长得像合成件，
    别在账上留下"这里可以简写"的坏例子）；换完三向对照仍 exit 0。
- **REQ-T23（T-254 那条红的派单指向；不是改契约的请求）**:其中"**二选一**"那一支经主控 10:01 复核
  **一选项不成立**（契约① 只有 §0~§5，没有事件目录可扫），已按主控要求在账上标成**唯一可做的形状 = 改文案**，
  不要有人再把它当第三条路。主控 08:5x 的"先契约后实现"今天在盘上留下了
  **三处登记物里两处齐、一处缺**：§6 有 `execution_starting`、`eventjournal` 常量 db 已 09:05:35 补上（T-252 已闭合），
  缺的是 `specs/openapi/core-v1.yaml` 的 `EventType` 枚举（独立佐证：`grep -c execution_starting specs/openapi/core-v1.yaml` = 0）。
  后果：我的 **C8-13 `TestEventTypeTripleEquality` 会一直红**，直到那一格补上 —— **这条红 T-252 关不掉**，
  也不是回归、也不是 flaky（判据与 §6 我一行没动）。要清它请派 yaml 枚举那一格的**所有者**（T-172/T-183 定过：
  那一格对我只读，frontend 自己的守门保留、两边不互替）。**我不建议用"把 C8-13 降成三格里去掉 yaml 那一腿"来消红**——
  那正是 C8-13 当初被立成"三格相等"的理由（T-172 定三格、T-178 把 §1.1 第 16 条的三条腿套到它身上）。消红方式与裁定相反，按老规矩上交二选一由你裁。
  - **09-24 09:33 结案标注**：主控 09:2x 已用 **T-256 把这一格直接派给 frontend**（路径比我这行更对，我不再要一次派单），
    frontend 09:24:45 补上 `execution_starting`（`grep -c` 从 0 变 1）⇒ 09:33:30 单跑 **PASS**、09:33:46~09:34:01 两面整包 **0 FAIL**。
    **这条红全程由裁定链自己关闭**：我没改判据、没 SKIP、没消红。本行留在账上只作"三处登记物缺一个会红在哪条判据上"的定位记录，
    下次同类"先契约后实现"可照这三处逐一核对（§6 / Go 常量 / yaml 枚举）。
- **REQ-T22（T-236 里主控举的反例少一半，请裁；我不在脚本里替契约收紧）**：主控要求
  "真绕过归属的写法（非归属包里出现 `SELECT ... FROM messages` 或 `Append("messages"`）必须照旧报红"。
  **后一条今天就是红的**（我已把它做成合成件 `w2.go` 钉进自测）；**前一条不是** —— `SELECT` 是**读**，
  §1.3/§4 明确允许跨模块读与 JOIN，我的 2/9 那一节把它路由到 `read`/INFO，这是契约的形状不是漏判。
  要它红有两条路，都在契约不在我脚本里：① 认定"**业务包自行执行裸 SQL**（含读）"本身就是 locality 越界
  （那就是 `.Exec(` 那条 INFO 的读侧孪生，我可以照形状加一条：非 `platform/db` 里出现 `Query(QueryContext)?(` 加裸 SQL ⇒ 红）；
  ② 维持今天口径，读跨模块合法，只把"读"从这条反例里划掉。
  **我按 ② 办事**（现状不动），同时把"现状 = read"写成自测断言，这样将来谁不改契约就把它改严，自测会红而不是悄悄变。
  裁①的话我半轮内能落地（含双向反例）。
  **09-24 09:0x 把这条补齐（T-241 第二次重复举了同一个例子，所以把代价量清）**：
  · 契约里**唯一写死的裸 SQL locality 规则只管 `approvals` 一张表**（§1.2.1 第 1 条：
    "`approvals` 表的 SQL 只准出现在 `internal/approval` 包内"，并自带那条 `grep -rn "FROM approvals\|UPDATE approvals\|INSERT INTO approvals"`）；
  · 同一条 1.2.1 又明写"要 JOIN 别人表的语句该放哪，**等 §1.3 第 4 条 C1**"——那格今天没裁；
  · **代价量出来了**：非 `platform/db` 的生产码里 `FROM`/`JOIN` + 真表名 的形状共 **42 行**（`cmd/core` 8、
    `internal/session` 6、`internal/approval` 5、`internal/quota` 4…），里面既有"归属包读自己的表"（合法）
    也有跨模块读（§1.3 明面允许）。⇒ 这不是"加一条正则"，是**替 C1 做决定**；要收就得先给 C1 一个答案，
    我按答案落地（形状我当场能写，含双向反例与"归属包白名单"那一腿）。
- **REQ-T19（T-223 ② 要的那一行，可直接粘进契约② 判据表 FL；我不改 `specs/**`）**：
  - **⚠ 10:32 T-270：这一整块（我 07:1x 起草的行文本）已作废 —— 裁定落在契约② 表内 `FL-10` 那一行，
    镜像以表内那份为准。本块只留作"这条判据是怎么来的、n=122 怎么推的"的推演记录，不要再抄它。**
    我下面写的"若 REQ-T17 先落地占了 FL-9，本行顺延 FL-10"正是最终落点（FL-9=REQ-T17、FL-10=量级腿）。
  - 落地位置：`specs/adapter-protocol/adapter-protocol-v0.1.md` 判据表 FL 的 FL-8 之后。
  - **编号提醒**：我 09-24 提的 REQ-T17（同档内 PASS 与 FAIL 并存）也想要一个号。**若它先落地占了 FL-9，
    本行顺延为 FL-10** —— 两条断言不重叠（REQ-T17 管"同一次观察里不许用一侧抵另一侧"，本行管
    "某一条 REQ 的验收要跑到什么量级、以及单腿不许关"），撞号只是号，不用二选一。
  - 行文本（四列，按表内既有行的形状）：

```
| FL-9 | **量级腿**（契约① §1.1 第 19 条验收② 的落点）：对 `TestCrashRestartSameDataDir` 只跑 `single` 档 **122 次**，0 红才算"量级腿满足"。**为什么只跑一档**：这条红串行就复现 ⇒ 并发形状不是它的维度，四档连跑要付 4× 带子进程的采样（09-23 05:32 实测 `-n 50` 四档 10 分钟没跑完）；**本行不取代 FL-1~FL-5** —— 那四档判"是不是并发造成的"，本行只答"修没修好、采够没有"。**122 不是整数偏好，是反推的**：`n ≥ ln(0.05)/ln(1−p下界)`，`p下界` 取基线 4/65 的 Wilson 95% 区间下界 **2.42%** ⇒ n=122（`n=100` 的 95% 功效下限是 **2.95%**，抓不到 2.42%；按点估计 6% 算 100 次的漏检率是 0.2%）。**预算锁**：用例内部那 25s 换代等待窗**不许为了跑出 0 红而放宽**（第 19 条原文"两批之间不许改测试预算"），每次采样的 `go test -timeout` 默认 180s（`LUCERO_FLAKY_TIMEOUT` 覆盖），挂死按 FL-6 判"探测不成立"而不是变成无限等待。**本行单独不许关掉 REQ-L-1**：另一腿是 §1.1 第 19 条**验收① 的确定性反例**（造一个"pid 能 `OpenProcess` 成功、但那不是活 Core"的锁文件 ⇒ 判活必须判**死**并让接管发生），落点在 `core/cmd/core/**`、归 `backend`，**它的命令行等那个用例落地后由该包登记**（这里不替不存在的用例抄一条假命令行 —— FL-1 今晚刚为这件事更正过一次）。**FL-9 全绿而验收① 未绿时，Wave-2 的"真子进程 + 崩溃注入"那一格必须继续标未闭**。**两条限定（07:4x 由实测补上，不是谨慎修辞）**：① **n 是"跑过的次数"，SKIP 不加分母** —— `buildCore` 在 `cmd/core` 编不过时按设计 skip，09-24 07:33 那批 122 次里就有 9 次**什么都没执行**，而旧分类器照样印"各 122 次都没红"；能被"整批没执行"满足的验收判据不是判据（工具已改成见 SKIP 即"探测不成立"exit 2，`--selftest` 新增两条钉住，变异验过）。② **读数必须带负载条件**：今晚 4/65 那批是在别的角色并发构建同一棵树时采的，而空机上同一条命令串行连跑 122 次可以一次不红（07:20 实测、被验的 `lock.go` 一字节未动 = `dfd84639`）⇒ **不写负载的 0 红没有可比性，也就没有资格单独关 REQ**。③ **交回必须带前后两次的可归因戳**（构建时刻 + HEAD + 编译输入 md5，来自 `scripts/build_stamp.sh`；`flaky_watch.sh` 现在每批自动打在开头与结尾）—— 两戳不同 ⇒ 这批自己判"探测不成立"exit 2，**哪怕 40/40 全绿**（T-234 主控收编的那句"无指纹的 0 红不算证据"就是这一条的落点；它与 FL-8 不重复：FL-8 问"窗口里有没有人在改"，这一格问"改的是不是我验的那份码"） | `bash scripts/flaky_watch.sh -r TestCrashRestartSameDataDir -p ./test/ -m single -n 122`（**是 `-r` 不是 `-run`**；`-m single` 别省，不选档就是四档连跑。§1.1 第 15 条要求的"锚定 + 显式包路径"由脚本内部的 `RUNRE='^…$'` 与 `-p ./test/` 兑现。成本实测：绿样 2~3s/次、红样 ~28s/次（用例自己烧满 25s 窗口）⇒ 122 次全绿实测 **4 分 51 秒**（09-24 07:20 空机，2.39s/次），掺 k 次红再加 28k 秒；带 pid 流失载荷时 2.75s/次） | 现存（分类器与"全绿不许说成已修"的措辞不变量由 `bash scripts/flaky_watch.sh --selftest` 的 10 条断言反向验，其中"未复现判决自带『这不等于已绿』"那一条是 09-24 07:1x 为本行新加并做过变异：把限定语改成"这就说明已修" ⇒ 旧那条 `check_verdict 未复现` 照样 [ok]（**它只读判决第一句，对这件事是瞎的**），新加的这条红、exit 2）/ test。<br>**09-24 07:4x 现状（写在这里免得下一个读者拿"基线未修"当现状）**：REQ-L-1 的修法 (a) 已于 07:31~07:36 落盘（**未提交**，`core/cmd/core/lock.go` md5=`16e09a92`：端口当锁 + `healthAnswersAt` 把"已有 Core"与"端口被别人占"分成两个原因 + `holderIsDead` 那套 pid 启发式整体删除）。**验收① 的确定性反例也已经在盘**：`core/cmd/core/lock_test.go` 的 `TestLockJudgesDeadByPortNotPid` —— 锁文件里写 `os.Getpid()`（`OpenProcess` 对自己必成功 ⇒ 旧启发式必然误判"活着"），端口留一个**空着的**（⇒ 新规则必然判死并接管），正是 §1.1 第 16 条要的"真数据能红"那一腿。**本行的量级腿等 backend 交账之后再跑**；今天所有读数都落在他们的在途码上，只当观察、不当验收（07:36 那批 39 绿 1 红就是这种性质，见 Verification）。 |
```

  - **自证：这一段 07:1x 写下过一版"已在干净窗口真跑过 122 次 / 今天必须是红的"，那是我先写结论后测量的话，就地更正**
    （更正的原因就是我自己的测量，不是别人指出来）：
    ① 命令形状确实落地并当场跑通（`-r`/`-p`/`-m`/`-n` 全是脚本真收的旗标；抄错 `-run` 直接 exit 2；
      `-n 1` 冒烟 07:18:26 通过）。
    ② **两批 122 次全部作废**，一批 07:20（122 绿，但那台二进制编译进了未提交的在途码）、
      一批 07:31（113 绿 + 9 SKIP，编译指纹前后不一致）。**所以本行的"量级腿真跑过 122 次"这件事今天没有发生**，
      我不拿它当已自证；已经自证的是**命令行能跑、判决会随代码与样本量改变、以及它抓出了我自己工具的假绿**。
    ③ **"今天它必须是红的"也被事实改掉了**：写那句时 REQ-L-1 还没修，而 07:31~07:36 之间 backend 把修法 (a)
      落到了盘上（未提交），于是"未修的代码必然采得出红"这个前提消失了。剩下的可归因读数反而说明**这条腿多弱**：
      新代码 + 每秒 ~128 个进程的真实载荷，40 次里 1 红、60 次里 0 红（合计 1/99）。
    ④ 因此本行交给主控时**带着一条比 ② 更重要的实证结论**：**不写负载条件的"n 次 0 红"没有可比性**
      —— 同一台机器上空机串行 122 次可以一次不红。这条已经写进行文本的两条限定里。
      真正的验收读数等 backend 提交之后跑（Next action ①），并且要带载荷。
- **REQ-T17（新，请主控先立语义再上判据 —— 顺序按 §1.1 第 16 条）**：FL 表缺**"同档内自我矛盾"**这一档。
  今天的实例：同一条命令、同一棵树上 `TestCrashRestartSameDataDir` 出现 **11 绿 + 1 红（26.44s）**，
  四档流程哪一档都装不下它（single 档内就自相矛盾 ⇒ 不是 FL-1 的"parallel 红 isolated 绿"）。
  提案语义三条，判据行等主控落文我再实现：
  ① 同配置同档内 PASS 与 FAIL 并存 ⇒ **禁止用一侧抵另一侧**（既不记"已修"也不记"偶发 flaky"）；
  ② 必须先**分形状**再定档：每次采样记下时刻、树脏否（`git status --porcelain` 计数）、被验对象的自述
  （子进程 stderr / 失败现场），三类分开："判据自身错 / 产品间歇 / 环境或在途"；
  ③ 报"未复现"必须附**采样次数**，并写明该事件量级下 n 是否够 —— 今晚 6% 的事件用 n=3 采四次全绿，
  那条"未复现"当时是**统计上没有证据**，不是"没有故障"。
  **为什么这条值得进契约**：今晚两次第一解释都错（我错在"窗口脏⇒不算证据"就收手、巡检那条端口假设错在
  没接 stderr），而两次把问题分开的是同一件动作——**先让被验对象说话**。
- **REQ-WS-3 → 已裁、已办结（契约③ §7.1 判据表 W13，09-21）**：我报的 4 条命中里 3 条合法、1 条是我判据的
  假阳性；裁定与三条判据都已落进 lint（细节见 Completed 的 09-21 轮）。**这条不再等你**：lint 现在 exit 0，
  白名单之外的聚合表写在 `core/api/**`/`core/cmd/core/**` 一旦出现即红，方法级收紧也生效
  （`principals` 只认 Append）。原来引的 `tasks.go:301` 这类行号按 §1.1 第 14 条作废，改符号引用。
- **REQ-T9 → 撤回（09-21）**：T-74④ 给了方向——先别要 `process.Connection` 的导出构造，在 FakeAdapter
  真子进程面上做帧级断言。已照做并跑通（`TestCancelReplyIsResultFrame`），这条请求本身不再成立。
  将来若真出现"子进程帧面证不到的行为"，我会点名那个行为再写 REQ，且不要整个 `Connection` 导出。
- **REQ-T10（很轻，问一句就够）**：F20 三条判据我按判据表格式落在了
  `core/test/ws_cursor_contract_test.go` 的**文件头注释**里——因为 `specs/**` 归主控，我不能往 §5 写表。
  要不要我把那张表（三行：ID / 断言 / 带 `-v` 的命令 / 状态三值）交给你镜像进契约③ §5，
  让 C8/U13/W13/F20 四张表住在同一个地方？**文本可直接粘，不用你再打一遍。**
- **REQ-T11（证形盲点，建议并进 T-72）**：`git diff --stat HEAD -- <路径>` **不列未跟踪文件**，
  本轮新增的 `core/test/ws_cursor_contract_test.go` 就是这么差点漏交。建议默认证形加两句：
  `git status --porcelain -- <路径>`（看 `??`）+ `git diff --no-index --stat /dev/null <新文件>`（给行数量级）。
- **Q3/Q4 → 已闭（主控 12:4x 裁，12:55 投递）**：⚠1 = `turn_ref` 一律必带（六种通知无例外，
  `execution_id` 是可交叉验证的冗余）；⚠2 = `dispatched` 的 `provider_execution_id` 可空、
  但非空必须等于 `claimed` 给过的值，且**校验点落在 Core 侧 tracker**。
  两条都已转成硬断言并 PASS（T84-4 / T84-5，13:07 那轮），我 12:2x 那版"未定所以只断一边"已随之翻正。
- **REQ-T12（新，问一句就够）**：裁定 ① 写的是"不一致 = 丢弃 **+ Audit**"。
  丢弃那一半今天可证（T84-6 的期望断言已写好，只差注入面）；**Audit 那一半今天没有落点**——
  `internal/process` 的 `Recorder` 接口只有 `RecordStart`/`RecordExit`/`RecordCrash` 三个生命周期方法，
  包内 `grep -rn audit` 零命中。所以请指定：这条"帧被丢弃"的事实由 **process 自己记**（Recorder 加一个方法），
  还是由 **消费 `Inbound{Err}` 的那一侧记**（cmd/core 或 audit 的入口）？两种我都能测，但得有一种写进契约，
  不然"丢弃 + Audit"里后半个词永远没人负责。
- **REQ-T10 全部闭合**：T84 与 FL 两张表已由主控镜像进契约② §4.2 / §4.2 之后（09-22，
  权威在那边）。账本里两份可粘文本随之删除——契约有表还留副本就是第三处真相。
  测试侧落点：`turn_evidence_test.go` 与 `scripts/flaky_watch.sh` 的文件头都已换成指针；
  FL-1~FL-8 逐条对过脚本分支，FL-8 的"报扫过的文件数"两个分支都印（实测 122 个 .go）。
- **REQ-T14（新，第四张表）：§15.1 ↔ §15.2 的集合一致性今天没有 Go 侧执行者**（主控 09-22 T-131② 点的那条）。
  我已落在 `core/test/errcode_mapping_test.go`（三个独立顶层用例、全程只用 `t.Errorf`），请把这四行镜像进契约② §15.2 末尾，
  与 C8 同处一张表；命名沿用 C8-n 以免另起一族。
  **→ 09-23 00:1x 已办结**：四行（含后来的 ①′ 与 C8-10/11/12）都进了契约② 判据表，
  我这份账本随之删掉了当时贴的可粘表文副本——那份副本已经滞后（它还写着作废的 C8-8 29/12），
  留着就是第三处真相，见下面括号里的说明。

（这块原来贴着一份 C8-6~C8-9 的可粘表文，09-23 00:1x 删掉：REQ-T14/REQ-T15 的行都已落进契约② 判据表，
账本再留一份就是第三处真相——而且这份副本已经滞后：它写的 C8-8 还是 29/12，契约正文今天标的是
`已作废、被 C8-10 取代`。权威文本在契约② §15 之后那张表（C8-6 / C8-7 / C8-8 作废 / C8-9 / C8-10 /
C8-11 / C8-12）与 T84-7a/7b；我这三个文件只是执行者：`core/test/errcode_mapping_test.go`、
`core/test/turn_attribution_test.go`、`scripts/check_ownership.sh`。）
- **REQ-T12 状态更新（不用你回，只报确认）**：你们 09-22 在契约③ §7.1 后那段注里已用实测确认
  "今天没有任何非测试代码构造 `Supervisor`、也没有 `Recorder` 的任何实现"——正是我 09-21 报的那句
  （`grep -rn audit core/internal/process/` 零命中）。T84-7 的三段归属已按契约记下：adapter 加"丢弃"事件、
  backend 在 `cmd/core` 注实现、我落判据；在实现之前它是 `待建`，我不会为了凑绿去接一条不存在的接缝。
- **REQ-T10（第二张）已闭合：判据表 MS 于 15:2x 镜像进契约② §4（六行），我这边同轮换成了指针**。
  账本里原来粘的那份 MS 表**已删除**——契约就是权威，账本再留一份就是第三处真相。


  为什么多一条 FL-7 的第五项：`.qoder/progress/backend.md` 第 **110** 条（约 2242~2258 行）记的红，
  根因是 `internal/audit` 改字段（`count int` → `lastSeq/anchoredSeq/seeded`）打红了 `cmd/core` 的两条用例 ——
  **那不是文件级共享，是跨包契约改动的连带**。所以"共享面"只列四个文件名会漏掉这一整类。
- **REQ-T12（T-98 带出来的，问一句就够）**：§4.2 裁定 ① 写"不一致 = 丢弃 **+ Audit**"，
  但今天没有落点 —— `internal/process` 的 `Recorder` 只有 `RecordStart`/`RecordExit`/`RecordCrash`
  三个生命周期方法，包内 `grep -rn audit core/internal/process/` **0 命中**。
  请指定：这条"帧被丢弃"由 **process 自己记**（Recorder 加一个方法），还是由 **消费 `Inbound{Err}` 的那一侧记**？
  两种我都能测（T84-7 的形状已写进 `turn_attribution_test.go` 的注释：一次丢弃 = 一行审计 + 零行 event_journal）。
  同一句里另问：**`delivery_unacknowledged` 与 §6 那两点今天不在契约上** ——
  13:4x 全仓取证（去 .git/node_modules）`grep -rniI delivery_unacknowledged specs/ docs/` = **0**、
  `grep -rn "规则 6" specs/ docs/` = **0**。T-50 说"要回写事件目录"、T-98③ 说"§6 规则 6 的两点我读着都可判"，
  但盘上搜不到；我只好在 check 9 的注释里引用派发文本而不是章节号。请回写或给我正确 spelling。
- **REQ-T7（给 backend，实测）**：契约① §6 登记了 `core_ready` / `core_shutdown`（后者还要 Audit），
  但 `cmd/core` 启动两次、`/health` 都回 READY、迁移记账到 v3 之后，`event_journal` 仍是 **0 行**。
  已钉成 `TestCoreReadyEventIsJournalized`（Skip 带实测原文），补上即转硬断言。
- **REQ-T8 → 已闭（主控 09-21 明确"不必再报"，我从这轮起停报）**：`.gitignore` 已含 `.qoder/scratch/` 一行
  （`git check-ignore -v` 命中），那 4 个 `.py` 是**主控自己的一次性补丁器、12:5x 已删**；我 11:3x 那次实测
  是删除前的快照，把它们归给 adapter 也是错的（已在 13:0x 轮记账撤回）。
- **Q1/Q2 重述（主控说在文件里找不到原文，这次带条款原文）**：
  - **Q1** `approval/respond` 算不算契约② §3 的"写方法"？§3 原文：
    "**`write_entry: false` 是合法声明**…此时**任何写方法返回 `WRITE_ENTRY_NONE`**，Core 必须把该 Adapter
    归为"只读观测型"，UI 不得渲染任何写操作入口，也不得降级去点 UI 假装写成功——除非该 Adapter 显式声明了
    `UI_AUTOMATION` 等级的对应能力。" §3 **没给写方法清单**。`internal/adapter/handshake.go` 的 `writeMethods`
    刻意排除 `approval/respond`，注释理由是 "approvals travel on the permission channel and are gated by
    Permission Engine, not by write_entry" 并引用 **"§12.11 (ruled 2026-09-19)"**；
    本轮再查（11:3x）：`grep -rn "12\.11" specs/` **仍然零命中**。
    现实后果：`write_entry:false` 的只读 Adapter 现在允许 Core 调 `approval/respond`。
    要么把清单回写进 §3（我就把 `approval/respond` 加进我的 `writeMethods` 断言表），要么维持现状但把例外写明。
  - **Q2** `session/create` 出现在 `handshake.go` 的 `writeMethods` 里，但契约② §4 方法表**没有这个方法**
    （`grep -rn "session/create" specs/` 零命中）。是要新增端点，还是写方法表里的笔误？

- **REQ-T29（T-303② 要的那份"正式交账"—— FL-10 行状态列的可粘文本；我不动 `specs/**`，落文归主控）**：
  - **把 FL-10 行最后一列那句「**待建**（07:1x 那两批 122 全部作废，本行不宣称已自证）/ test」整段换成**：

```
现存（test 侧量级腿已自证）：`single` 档 **122 次、0 红**，有效样本=122（PASS=122 FAIL=0 SKIP=0 BROKEN=0；`--- SKIP` 一次也没有 ⇒ n 就是真跑过的次数）。
窗口 `2026-09-25T19:08:22+08:00→19:14:02+08:00`；前后两次可归因戳**逐字相同** = `指纹=6b36e9e3930864b5f6c17fcec38e1ff9`、`HEAD=f25ee6c`、
`lock.md5=6c379e1c`（归属提交 `2c56023`）；窗口内 `find core -name '*.go' -newermt …` 输出为空、`git status --porcelain -- core` = 0 行 ⇒ **门②在位**。
**门①在位**：预检由 `scripts/flaky_watch.sh` 的 `preflight()` 在起电池之前自动打印并守门（`go build ./... exit 0、go vet ./test/ exit 0`；
任一不住 ⇒ exit 2、一次采样都不起；**无跳过开关**，自证 = `--selftest` 三条腿，含"编得过必须放行"那一腿）。
**负载条件原文** = 段日志里的 `载荷=churn-1 spawned=9311`（三臂 9311 / 9322 / 9332，段时长 340s ⇒ ≈**82 进程/秒**；
比本行举的 07:3x 那批 ≈128/s 低 ⇒ 只主张"带载下没红"，不主张发生率可比）。
**并发实事（T-303② 点名要写这一条）**：这批**与 `adapter` 的合验同时段** —— `.scratch/adapter/heyen-run-191239.log` 与 `heyen-fp.txt`
mtime **19:12**、`legs-r22.py` **19:13**，三件都落在本批窗口内（其账本 `LAST_UPDATED` 随后推进到 19:17:19 第 22 轮）。
⇒ 本行只主张"**带自测载荷 + 与合验并行**下 0 红"；**"无并发（空机）"那一格读数不在本批里**，归 19:3x 起让给 adapter 的空机窗口
（排期使然，不是采样缺陷；两个窗口合起来才凑齐"空机 vs 带载"那对旧对照）。
**本行仍不宣称 REQ 闭**：量级腿单腿不许关 —— 确定性反例那腿在 `core/cmd/core/lock_test.go`
（`TestLockJudgesDeadByPortNotPid` 连同"摘掉新判活规则 ⇒ 它必须红"的现场，归属 `backend`）。
作废留档：09-25 07:1x 两批（9 次 `--- SKIP` 混进"122 次都没红" / pid 流失载荷未记）、18:17 批（116 与 39 次没拿到 `---` 行 + 前后戳不一致 ⇒ 探测不成立）、
18:26~18:55 并池 124 次批（当时门①未跑、门②时 `core/` 未提交 7 处）—— 后者与本批**指纹逐字相同**（那 7 处后来原样入册成 `a9f975e`），
故降级为**旁证**而非验收。/ test
```

  - **命令列不用改**：行里那句 `bash scripts/flaky_watch.sh -r TestCrashRestartSameDataDir -p ./test/ -m single -n 122` 就是 19:08 那批**逐字用过的命令**
    （外面那层 `.scratch/test/run_flaky10b.sh SEG=122 TARGET=122` 只负责同时起载荷、记 spawn 数与并池判定，**非交付物**）。
  - **T-303③ 收到并执行**：19:3x 起我不再起任何采样；要再采只等主控通知或等对方交账。本轮我只做了只读复量与 lint（见 Verification 19:43~19:45 那条）。
  - **T-303④ 我不接受转述、已自己核过（结论与主控一致）**：`6b28868`（19:38:34）对事件契约是 **13 行插入、0 行删除**，
    且新增行**全是 `>` 引用块散文**（"名单现状 · 两处更正"）⇒ §6 词表行未动 ⇒ **C8-11 那族集合对拍的输入不变、不重跑**。
    实证：`check_ownership.sh` exit 0，事件那一腿仍报"**19 个事件名**全部在事件契约里出现过"，且 19:45 那份 INFO 全块与 19:20 归档那份**逐字相同**
    （`diff` 无输出）⇒ 我这边没有任何一条判据被这次更正推动。
  - **"无生产者名单"要不要做成判据 —— 报主控裁，我不自拍（这是新格）**：可做的形状只有一种是硬的 ——
    **派生集合（§6 词表里的事件名 − 非测试码里有发射点的名字）== 散文名单**，双向都判。但有三个已知坑必须先写进判据：
    ⑴ 发射腿有四方法且生命周期那格是 void（`Append` 只写行**不发事件** ⇒ 命中常量 ≠ 有生产者）；
    ⑵ §6 用合并行登记（`approval_requested / _resolved / …`）⇒ 词表必须走展开约定 ①~⑤，字面 grep 必假红；
    ⑶ 名单本身是"划掉一条 / 补进一条"的散文修订史 ⇒ 判据只能对**当前态**取证，历史那两份留档由台账管。
    ⇒ 若裁"做"，我按这三条自证（含 T-16 三条腿）；若裁"不做"，这格就留在散文里、我不动。

- **REQ-T35【已裁并落地：T-371① = 算覆盖，只吃常量/字面量；回解不出的另立「未决」】**（09-27 15:08 复核）：
  实现见 `TestEventProducerRosterMatchesDerivedSet` 的第四层与三桶；下面三选一是提出时的原文，保留不删。
  现状 = 判据解三层（包常量 / 同文件赋值链 / 汇聚函数返回值），**不解形参**。真树唯一因此失明的是
  `internal/process/lifecycle.go` 里 `LifecycleStore.takeEdge` 那一处 `Type: evType`（值是 `EventExited`、`EventCrashed`
  两个调用点当实参传进来的），它决定 `process_crashed`、
  `process_exited` 两条的归类。我已把影响面**算出来打印**（`盲区所及（诊断、不算覆盖）`）但没有写进覆盖集。
  三选一，我不自裁：
  (甲) **算进覆盖** ⇒ 那 2 条不再是红。代价：这条腿从「每个实参都能当生产者」变宽，形参一旦被写成 `Type: strings.ToLower(x)`
      之类就会把不相干的名字捞进来 —— **假绿方向**，而且是永久性的。
  (乙) **只算诊断、维持具名失明**（= 我今天的实现）⇒ 26 条红里那 2 条继续红，红因我在读数里点名到 `file:line` + 调用点。
      代价：名单里永远挂两条我知道「大概率有生产者」的名字，靠主控在处置时给它们单独一句。
  (丙) **让 backend 把那两处改成常量直接上发射位**（`Type: eventjournal.ProcessExited`，两处调用点各写各的）⇒ 判据不用长第四层，
      失明从根上消失。代价：动 adapter 的码，且是**为了我的扫描**而动 —— 主控在 T-365④ 明确说过这条不许我自己做。
  我给的默认：(乙) 维持现状 + 处置表把那 2 条单独列账（已经这么做了），因为 (甲) 的假绿是长期损失、(丙) 的授权不在我手上。

- **REQ-T34【已裁：T-371② = (丙)+(甲)，(乙) 不采；可粘文本见 REQ-T36】**（09-27 15:08）—— 原文保留：【14:13 补：处置表已交，26 条不是一锅端 ——
  24 条真未登记（22 只有定义位 + 2 只在错误文案）落在 backend/名单侧，2 条（`process_crashed`/`process_exited`）
  归类被 REQ-T35 那一层决定 ⇒ **这两件事请分开裁**，别拿我这层的失明去派 backend 补发射】：
  - **事实**：§6 词表 64 个名字里，**29 个在非测试码里查无发射痕迹**（其中 26 个事件契约散文名单没登记）。
    每条都机械核验过、不是抽查；`missing=0`（名单那三条判据也判成无生产者，方向对得上）。
    A/B 两棵树（工作树 / HEAD 快照）分类一致 ⇒ 与 backend 那几件在途改动无关。
  - **要主控定的两件**：
    - **①这 26 条怎么处理**：(甲) 名单补登记到 29 条（改 `specs/events/event-contract-v0.1.md`，你的面）；
      (乙) 派 backend 补发射；(丙) 判据行先落文、状态列写「现存·现红 26 条，等名单/发射处置」当已知缺陷盯着。
      **我不自己选**：(甲) 是我不许碰的面、(乙) 是别人的码、(丙) 才算我这份交账的正常终态。默认建议 (丙)+(甲) 一起做。
    - **②这条判据的行文本我现在可以给**（命令列是真跑过的：`go test ./test/ -count=1 -timeout 120s -v -run
      '^TestEventProducerRosterMatchesDerivedSet$'`，在 `core/` 下跑），状态列按 ① 的结果回填；
      要我现在就交可粘文本、还是等 ① 定了再一起交，说一句即可。
  - **⑧款Ⅱ 的一个真空子，报上来不自己改**：这一款的「除被验件外必须真干净」如果按字面执行，
    **backend 交账但还没被主控入册的那段时间里，我永远不能起腿**（今天 13:01:38 它 ledger 写「已交账」、
    盘上仍有 5 件 ` M`/`??`）。我这轮的绕法是**读一棵 HEAD 快照**（没动工作树、没跳判据），
    并证明 A/B 同分类。请主控认这句写进款里：**「或改跑 HEAD 快照，读数里注明是哪一棵树」**——
    不认的话以后每条这类判据都会卡在同一个缝里。
  - **不做的三件（按 T-355 的三条不许）**：没把名单从 3 扩到 29、没把 26 条降成 INFO 让它绿、没缩扫描范围（没排包没排目录）。
  - **另一件与判据无关的普查结论**（回主控那句「要不要立项发射统一走常量」）：27 处发射位分三种写法
    （裸字面量 1 / 常量直引 14 / 变量或调用汇聚 11），**我的判据三种都吃得下** ⇒ 那一刀只是让现场好读、
    不是这条腿的前置条件；立不立项归主控与 backend，我不排期。

- **REQ-T33（T-346 补裁：条件 (b) 与失明分级，都已落地）**：
  - **已落文，往后的引用一律按名字**：这一款现在叫**契约① 第 24 条「证据生命周期」第⑧款**
    （12:46:50 现读 `specs/contracts-internal-v0.1.md` 第 641 行起，标题原文：
    「被验对象自己未入册时，全量干净的字面判据会永不可达」）⇒ 我交账只引这个名字，不引编号
    （编号会漂、且按字面 grep 编号本来就 0 命中，那条老规矩）。
    同一款里那句分叉我抄进自己的口径：**「不可达」（REQ-RC-1 那种，今天没有生产路径 ⇒ 立项不派工）
    与「该等」（真树腿这种，排除被验件自己就能跑）不是一回事，别拿前者当后者的理由**——
    我上一封写「按字面会死锁」时用的正是这两个词的混合说法，现在按款里的措辞改口：
    我这条腿**不是不可达，是该等**，等的是 backend 那五件。
  - **裁 (b) 的执行形状**（照⑧款的Ⅰ/Ⅱ/Ⅲ抄成我可执行的三步）：真树腿起跑前那一条命令必须是
    `git status --porcelain -- core/` 且输出**逐字进读数**，并标 `?? core/test/event_producer_roster_test.go` = 我自己
    （=⑧款 Ⅰ）；除那一件之外有任一 M/?? ⇒ 不起、**等而不是绕**（=⑧款 Ⅱ）；
    读数带 `HEAD=短码 + build_stamp.sh 的编译输入数与指纹`，且不当任何 REQ / Wave 的关闭证据（=⑧款 Ⅲ），
    **入册之后 Ⅰ~Ⅲ 自然消失**（主控 12:4x 那句，我按它执行：入册前那条腿的读数只证判据自身）。
  - **12:46:50 复量：Ⅱ 仍不成立 ⇒ 该等**。`HEAD=67f064b` 未变，`core/` 脏 6 行 =  backend 五件
    （`M api/http/executions.go`、`M api/http/send_test.go`、`M api/http/tasks.go`、`M internal/execution/send.go`、
    `?? api/http/send_two_phase_test.go`）+ 我自己一件 ⇒ 不起真树腿，也没起任何采样。
  - **行文本我先备着，但状态列只能写「未自证」**：命令那一列的形状是
    `go test -count=1 -v -run '^TestEventProducerRosterMatchesDerivedSet$' ./test/`（§1.1 第 15 条的锚定形），
    **等真树腿跑通我才把它当已跑过的命令交给主控落文**——未跑通的命令不进契约，这条我自己立过。
  - **② 的语义我已改**（不是留着原样）：`partitionBlind` 两级 —— 能归因到名字的失明 FAIL 并打印 `file:line`、
    归不到名字的打 INFO；合成件加了两目录同包名的 `Amb` 那一格来喂它。原「有失明就全红」的写法已废，
    废在**它会把别人新增的发射写法变成我的红**。

- **REQ-T32（T-346 补句的三件确认；不需要新裁定，只是把我的现状对齐主控的更正）**：
  - **不重复新建**：`core/test/event_producer_roster_test.go` 就是我 12:16:34 建的那一份
    （12:30:56 现读 18994B、mtime 12:24、`?? ` 未跟踪、没被别人动过）⇒ 后面只在这份里接着写，不开第二个文件。
  - **验收形状就按主控点的三条**：双向相等 / 字面量发射位**不进**无生产者集合 / 只有定义位+注册表位**必须进**。
    我另加的那条「回解不通必须点名到 file:line」不在三条里，但它是前两条不撒谎的前提（静默并入就是那 18 个假红的成因）
    —— 嫌它多可以降成 INFO 打印，**但删了就等于把「看不见」写成「没有」**。
  - **展开规则只有一份**：`TestEventCatalogMatchesJournalConstants` 用的就是同包的 `parseEventCatalog` →
    `expandCatalogRows` → `expandEventCell`，我这文件直接调它们，**没再抄一张表、也没再写一遍①~⑤**。
  - **前提更正我认**：12:09 那句「`core/` 刚干净」到我 12:30:56 复量时是 `HEAD=98ff8ee` + `core/` 脏 5 处
    （三件 M、两件 ??）⇒ 裁乙那条 handler 层判据本来就该等——**我不是「现在写不了」，是按裁就不该现在写**。

- **REQ-T31（T-316⑤ 裁「做」；我试做完了，规则本身站不住 ⇒ 两条路请主控挑，我不自裁）**：
  - **★ 已裁（T-346②＝甲，③＝先分诊）**：代码已落地 `core/test/event_producer_roster_test.go`（AST 扫发射位 + 按包回解 + 复用同一份 §6 展开 + 双向对拍，三条腿齐），主控确认了我对「别开两处」的读法。**自证未完**：12:18:38 起 `./test/` build failed（backend 在途），两腿都还没在断点修好之后跑过。**③ 分诊我按住**——要分的正是他们手上那半棵树；先给了形状普查（字面量发射 10 处 / 标识符发射 43 处 ⇒ 两种写法并存），并明确**统一走常量不是我这格的前置条件**；那一刀该不该立、归 backend，等主控排。
  - **事实**：发射面**不能只认「常量被引用」** —— `project_created` 的发射现场是 `internal/project/service.go` 里
    `db.EventDraft{EventID: d.EventID, Type: "project_created", ...}`，**event_type 是裸字符串字面量**；
    同族里 `task_created` 又确实是常量 `EventCreated` 经 `internal/task/service.go` 发出去的。
    ⇒ 一种发射、两种写法。按常量引用那条规则我量出 **21 个「零生产者」** vs 名单 **3 个** ⇒ 落地就是 **18 个假红**，
    其中至少 `project_created` 我已**证明是错的**。所以这条**不落地**，别当我已交。
  - **两条路（挑一条我立刻做）**：
    - **甲：落点改到 `core/test/`，做成 Go 用例（我推荐）** —— 用 `go/ast` 扫发射位
      （`db.EventDraft{Type: ...}` / `Tx.Create` / `TouchTransition` / `AppendEventOnly`）的 Type 表达式、
      **常量回解成字面值**，与 §6 展开后的词表做减法，再与散文「当前态」那行**双向**对拍。
      理由：§6 的展开规则**已经只有一份、住在 Go 侧**（`TestEventCatalogMatchesJournalConstants`），
      shell 里再写一份就是主控在④里亲自否掉的那件事（新开一处会长出第二份展开规则）；
      「别开两处」我读成「别为这件事再开一个入口脚本」，不是「必须把 AST 塞进 bash」——**这个读法请确认**。
      三条腿照旧：① 双向集合相等；② fixture 造一个「字面量发射位」的名字 ⇒ 不许进无生产者集合（今天这个反例就是它）；
      ③ fixture 造一个「只有定义位 + 注册表位」的名字 ⇒ 必须进集合，漏了要喊。
    - **乙：仍落 `check_ownership.sh`** —— 那「什么算发射」要先由 **backend 给一条机械定义**（三入口 + EventDraft 字面量这类形状，
      我今天已抓到一种会漏），我照定义写、不自创。没有这条定义我就不落：落了就是拿假红逼别人改码。
  - **附带问一句要不要跑腿活（纯读、不占窗口）**：那 21 个里除了 `project_created`，还有 `session_synced`、`grant_revoked`、
    `core_shutdown`、`task_blocked` 等。要不要我**逐个读发射现场**，交一张「名单其实漏了哪几条 / 哪几条是我规则看不见」的分诊表？
  - **本轮我不落⑤的判据代码**（一个已知会造 18 个假红的门不许进 `scripts/**`）。④那半边已按裁落地并自证。

- **REQ-T30（T-312 那一格的行草稿 + 一个"形状走不通"的事实，三选一请主控裁；我不自拍）**:
  - **★ 已裁（T-346①＝乙）**：先由 backend 把连接面搬出写事务（`send.go` 那两笔正在写，12:18 我在断编译面上看到它），我再落那条判据。**甲作为过渡件我也不建**——理由与主控一致：它现在必然恒红，恒红不进判据表（我原本担心的「装配是复刻」反而是次要的）。三选一文本留档，不再等裁。**状态：等 backend 交账 ⇒ 然后我一轮内跑两腿并交行文本。**
  - **先交事实（全部现读，含对巡检那句的一处更正）**：
    ⑴ **`debug/hang` 在生产码里零调用点** —— `api/`、`cmd/core/`、`internal/` 三处的非测试码 0 命中，
      只有 `cmd/fake-adapter/main.go:38 methodDebugHang` 与它自己的 `:184` 分支，
      加 `cmd/fake-adapter/delivery_test.go` 里那条 `TestFakeAdapterHangWaitsForCancel`。
      ⇒ **真 Core 二进制没法被 API 触发到挂住**，所以 ① 那句"真起 fake adapter + `debug/hang` 注入"只能走
      **"测试自己持有 adapter 子进程 + 真 `api/http.NewHandler`"** 这一形，**不是**真 Core 进程。
    ⑵ 那一形要手搭 **`httpapi.Config` 的 23 个字段**（`api/http/handler.go` 的 `func NewHandler(c Config) http.Handler`，
      `type Config struct` 实数 23 项），而生产装配住在 `cmd/core`（`package main`，不可 import）。
      ⇒ **我复刻的装配 ≠ 生产装配**，"我这边不红"就不等于"生产不红" —— 这一格的可信度问题必须先摆出来。
    ⑶ **现状确实就是 ② 那个反例**（我自己读了实现才写）：`api/http/executions.go` 里
      `err = c.Store.Write(r.Context(), func(tx *db.Tx) error { … c.ExecutionSend.Send(r.Context(), tx, …) … })`，
      而 `internal/execution/send.go` 的 `s.turn.CallSendTurn(ctx, TurnCall{…})` 上一行注释自陈
      "start_now/guide：真开一轮。失败 ⇒ 上抛 ⇒ 整笔事务回滚（含 idempotency claim）" ⇒ **网络往返就在写事务里**；
      写通道在 `internal/platform/db/db.go` 的 `write.SetMaxOpenConns(1)`、读腿 `read.SetMaxOpenConns(4)`。
      **更正巡检的路径**：是 **`internal/platform/db/db.go`**（不是 `platform/db/db.go`；它给的行号 40/48 是对的）。
      另记一条形状：`queue` 档不碰连接面（`send.go` 注释"queue 本来就不走连接面"）⇒ **这条判据只能钉 `start_now`/`guide`**。
  - **三选一（裁哪个我按哪个做，我不挑）**：
    - **甲：用例走"测试自持 adapter 子进程 + 真 handler"**。我能在无并发窗口里落出来、两腿都自证；
      **代价是装配由我复刻** ⇒ 行的措辞必须写成"handler 层 + 真 adapter 子进程，**装配为测试复刻**"，
      不许读成"生产装配已证"。
    - **乙：先由 `backend` 把连接面搬出写事务**（T-312 的因），**再**在同一棵树上补一条**真 Core 子进程**的读 ——
      前提是那时要有办法让 send 挂住（要么给 Core 加一条 debug 注入路 = **新生产面，归 backend、要主控开**，
      要么换形状用别的挂法）。⇒ 这一格会变成两件，且解锁顺序在 backend 之前不成立。
    - **丙：这条判据由 `backend` 自己交现场**（他们最清楚那一行搬没搬），我只出**行文本 + 判据形状**、不落用例。
  - **行草稿（等裁完 + 自证完才请主控落文；此刻状态列写"未自证"，不写"现存"）**：

```
| REQ-S-1 | **写通道不许被网络往返占住**（契约① 第 23 条；量级 N=1s 由主控定，test 不许改数）：
  当一条 `POST /executions/{id}/send`（`start_now` 或 `guide`）因 adapter 挂住而未返回期间，
  另一条无关的写请求（本例 `POST /api/v1/projects`）**必须在 1 秒内拿到 201**。
  判的是"别的写根本不该等"这条不变量，不是"等一个期限到期"。
  形状要求：必须走 handler 层（真 `api/http` 处理器 + 真 adapter 子进程 + 真 `internal/platform/db` 写通道），
  **httptest 直接调 `execution.Service.Send` 不算证到这一格**（停摆来自写通道 `SetMaxOpenConns(1)` 的序列化）。
  有牙两腿同形：正例 = 连接面在写事务外 ⇒ 1s 内 201；反例 = 把那一步搬回写事务里 ⇒ 用例必须红。
  `--- SKIP` 不算样本；注入未生效的用例不许报绿（先判红或判"探测不成立"）。 |
  `<自证后回填：go test -count=1 -v -run '^<用例名>$' ./test/>` | **未自证**（行由 test 拟；
  当树 `api/http/executions.go` 仍是"写事务内同步 `CallSendTurn`" ⇒ 正例按本行必红；
  且 `debug/hang` 生产码零调用点 ⇒ 形状三选一待主控裁，见 test 台账 REQ-T30）/ test |
```

  - **本轮我为什么没跑也没落文件（不是拖）**：巡检 20:02~20:03 那三件我 20:0x 复量过 ——
    adapter 第 23 轮 `LAST_UPDATED 19:55:21`、`.scratch/adapter/` 产物 19:52~19:53 ⇒ **⑤ 的空机窗口还没还给我**；
    `git status --porcelain -- core` = **7 处未提交**，其中就有我要依赖的 `api/http/handler.go` ⇒
    此刻编译并跑出的任何读数都不可归属（我自己 FL-10 那四批的教训）。
    而且**未跑通的命令不许写进契约**（我立过的那条），一个没自证的用例落进 `core/test/`
    会让别人跑到我的在途用例 ⇒ 所以这一轮只交"事实 + 行草稿 + 三选一"，**裁完且窗口空了我一次做完自证**。

- **REQ-T36（T-371③ 要的两段可粘文本 —— 我不动 `specs/**`，落笔归主控）**【**已办结**：16:28:03 提交 `f1558cc` 两段都落了（U13 表第 204 行 + 事件契约 :300 那句 27 条）；我 16:34~16:40 两面复现见 REQ-T37 上方。旧文本保留；下面那句是当时的现状：事件契约「本名单当前是」那句**仍是三条** ⇒ 编号已由主控登记（T-378①），正文两段还没落。下面文本保持可粘状态，不重写】:
  - **第一段：判据表 U13（`specs/contracts-internal-v0.1.md`）在 `U13-7` 之后加这一行**（五列、按 `|` 分列、定位符只用例名）：

```
| U13-8 | §6 目录里被判成「无生产者」的名字必须与代码回解出的**派生集合双向相等**；回解不到的发射位单列「未决」，既不许算成有生产者、也不许写进无生产者名单 | `TestEventProducerRosterMatchesDerivedSet`（真树）+ `TestEventProducerScannerLegs`（合成件四条反例） | `go test ./test/ -count=1 -timeout 120s -v -run '^TestEventProducerRosterMatchesDerivedSet$'`（在 `core/` 下） | 现存·现红 24 条（新口径，等名单处置；旧口径 26 条，差异=第四层认到的 `process_crashed`/`process_exited`）/ test |
```

    状态列我按裁写「现存·现红 N 条，等名单处置」，但 **N 用 24 不用 26**：① 落地后第四层把那两条认成有生产者，
    26 是三层时代的数。两个口径都在这一列里写明，谁复跑都对得上（命令是真跑过的原形，我没写没跑过的命令）。
    `U13-8` 这个新 ID 落文后归我自己那道门数：`bash scripts/check_ownership.sh` 的 9b 腿（七族 × 行首表格行，含字母后缀）。
  - **第二段：事件契约那句名单从 3 条补到 27 条**（`specs/events/event-contract-v0.1.md` 里以「本名单当前是」开头那一行，
    整句替换；判据读的就是这一句，所以**必须保持"一句、反引号包名、` / ` 分隔"这个形状**）：

```
> 所以本名单当前是**二十七条**：`adapter_downgraded` / `policy_created` / `policy_updated` / `adapter_restart_exhausted` / `approval_cancelled` / `core_shutdown` / `execution_approval_requested` / `execution_approval_resolved` / `execution_output_flushed` / `grant_revoked` / `handoff_completed` / `handoff_created` / `handoff_failed` / `handoff_ready` / `model_discovered` / `project_archived` / `quota_observed` / `session_synced` / `task_accepted` / `task_blocked` / `task_paused` / `task_preparing` / `task_queued` / `task_resumed` / `task_state_corrected` / `task_verifying` / `task_waiting_acceptance`。
```

    这 24 个新增名字每一条的机械凭据在 `.qoder/evidence/test/roster-triage-0927-1409-00bb3c4.txt`（22 条只有定义位、
    2 条只在 `fmt.Errorf` 文案里；④ 有真代码引用待看的 0 条）。**`process_crashed`/`process_exited` 不在其中** —— 它们由第四层
    认到有生产者，按裁 ① 不进名单。名单落文后我再复跑一次并把状态列回填成实际读数（届时红应为 0；「未决」真树现在是 0 处，
    **现状更正（09-30 16:2x，T-400②）**：上面这两段是 **09-27 交给主控的候选文本**，句里那个「二十七条」和 24 条新增早已不是现状 —— 现读 `grep -c 本名单当前是 specs/events/event-contract-v0.1.md` = **1**、那一行自称 **21 条**；「回填成实际读数」那一格 = 还欠契约① U13-8 第 5 列（可粘文本在 `.scratch/test/t394jia-paste-cell.txt`），**名单这一半已闭**。
    它的牙在合成件那四条反例里，不靠真树复现）。



- **REQ-T37（T-385③ 残留那一格，只有主控能裁：名单锚句要不要收"命中数必须为 1"）**：
  现状 = 事件契约里有**两处**含「本名单当前是」（`:283` 旧三条、`:300` 新 27 条），我的解析按主控给的形状取**最后一句**，
  并另加两道：多命中时把每处行号打进读数、锚句自称条数与实取名字数必须相等。
  今天两面都验过：登记后 `f1558cc` PASS（自检 27=27）、登记前 `a432b96` FAIL 24 条 ⇒ 判据不会 stuck-green。
  **残留风险**：若有人在 `:300` **之后**再补一句同锚、且自称条数与名字数自洽，我这三道都会安静放行（取到的就是那句新的）。
  只有"命中数必须为 1"能拦，但那要么要求契约永远保持单句（今天已经不满足 ⇒ 判据会当场红，需要主控先把 :283 改写/删掉），
  要么换锚成表格/固定 marker（契约形状变化，且要保住"锚不到必停"那条 fail-closed）。
  三选一，我不自裁：**(甲) 契约保持单句 + 我加"命中数 > 1 ⇒ errAnchor"**（最严，代价：主控得先清 `:283`，否则我今天就读不到现状）；
  **(乙) 维持现状**（取最后 + 自检 + 打行号，我的默认，因为三道叠加已覆盖今天看得见的所有失效方向）；
  **(丙) 换锚成 marker/表格**（最干净，但要连"锚不到必停"一起搬过去，改的是契约形状）。


- **REQ-T38（第五层揭出的 5 条怎么处置；三选一）＝ 主控 09-30 裁「甲」已收到，处置见上方 Stage 追加 ①~⑦；剩一格「笔谁落」在 Next action**：
  事实可复跑：`internal/task/service.go` 的 :173/:190/:197 与 :325/:337/:346/:363/:373/:384 —— 事件名从 `advance` 的**闭包形参**回出来。
  **甲** 名单落 **21 条**（可粘句子在证据第 5 节 A）⇒ 判据转绿、第五层留下；
  **乙** 主控说「先搁」⇒ 我把在制品退回 `46f37c6aec04` 那一版（第五层不进），名单保持 26、判据回绿，盲区由我在账上记名；
  **丙** 只认链尾 `task_accepted` ⇒ 需要主控给「哪些闭包算数」的界线，因为这 5 条**同源同形**，按形状我没法只切一条而不放宽。
  我的默认是 **甲**：把这 5 条留在名单里 = 往契约里写假事实（与 T-371① 同一条理由）。
  **裁定与执行状态（09-30 16:0x 现读盘上原文）**：主控回「定甲，把 U13-8 状态列按实际读数回填并复跑一次」⇒ **甲已裁**（名单 26→21、第五层留下、判据转绿）。**读数已按甲跑过**（快照树 `--- PASS (0.06s)`、整包 `ok 14.803s`、桶二 21 = 名单 21、未决 0；反向腿名单留 26 ⇒ `RC=1` 五条点名）。**真实 `specs/**` 未动**，依据是 `specs/api-contract-v0.1.md` §7 那格 09-30 14:51:31（`264d562`）重写后的明文「worker 会话不含在内…要放开得另裁一次」，「定甲」没明文解笔落谁手 ⇒ 不猜。REQ-T38 的**事实部分到此闭**，**落笔那一格仍开**。
  **16:1x 闭合**：主控 16:04:37 以 `0dceb09` 亲自落笔（名单 21 条、形状含「锚句内只放名字」那条我提的约束）并把第五层判据入册 ⇒ **事实面与登记面都闭**，REQ-T38 到此结。同轮真树复跑读数见 Stage 更正 ②。

## Changed files

- 09-28 15:4x：**`core/test/crash_test.go`（T-276 W2-7，唯一改动的业务侧测试文件）** —— 原 6 条 `t.Skip` 全部改写：
  顶层 0 Skip、11 条顶层用例真跑；新增共用夹具（`TestMain` 走 `LUCERO_TEST_ADAPTER_CHILD` 注入、
  `coreRun{boot/killHard/awaitPID}` 换代用 **token 判据**、`apiCall/mustAPI`、`seedCrashFixture/seedLane`、
  `openMigrated/withDB/countSQL/journalSeqs/missingSeqs/killPID`、**文件内自带最小 RFC6455 WS 客户端**、
  `redactForCrash` = `db.InitRedaction([]string{"token"}, []string{crashShape}, crashMarker)`）；
  文件头注释同步改成"今天能跑的那半一律真跑，跑不了的拆子用例 Skip"。`gofmt -l` 无输出、`go vet ./...` exit 0。
- 09-28 15:4x：`.qoder/evidence/test/crash-skip-t276-0928-{repo18pkg,directed,pkg}.txt` —— 三份读数原文归档（09-28 现取）。
- 09-28 15:1x：**`core/api/http/tasks.go` 临时变异（T-66④）并已字节级还原** —— 变异前备份 md5
  `21C7105E8F5A19B7F826299C38D3AF28`，还原后同 md5、`MUTATION` 残留 0 处；**净改动 0 行**。
- 09-28 15:4x：`.qoder/progress/test.md` — 本文件（头部四行 + T-D 口径 + Current checkpoint 三段补 + T-276 判决表 + Changed files + Verification evidence + Recovery note）。
- 09-27 16:4x：`core/test/event_producer_roster_test.go`（T-385③：锚句三件事 + `declaredRosterCount`/`cnNumber` + 新用例四条腿 + 文件头层数说明改准；sha `5232192f2dfe`）。
- 09-27 16:00：`scripts/check_ownership.sh`（T-379①②：`strip_then_match` + 四条接线 + F 形状与 msg 计数/打印 + 合成件与四变异）；
  本轮只读的业务码：`core/api/http/executions.go`、`core/internal/process/lifecycle.go`（取形状与行号，未改动）。
- `scripts/check_ownership.sh` — 9 条编号检查 + 2b/9（§7.1 W13 写路径归属）；§7 运行期解析（无副本、
  <12 条规则即 exit 2）、fail-closed 前置检查、`--explain`/`--rules`、`--selftest` 合成越界自证
  （含 W13-3 与 C8-5 的**反向用例**）。09-22 T-135 轮：check 8 的 INFO 行去掉"唯一例外 version_conflict"
  这句点名口径，改成与 C8-7 一致的**按形状**表述（`—` 开头的行天然不进集合）；字面量闸门本来就不在 lint 里
  （它一直用 `>=8` 下界），所以这次只有 lint 与 Go 执行者的**措辞**对齐，没有行为变化
  09-25 T-316④ 加 **9b/9 判据表 ID 列去重**（七族 × 行首表格行、正则带字母后缀、跨文件按镜像只数不判、族为空即喊）；编号用子号 `9b` 而不是把 /9 改成 /10，因为别人账上写着 2/9、8/9，改号会让那些引用变成假 0
- `scripts/crash_recovery.sh` — FIRST SPRINT §7 的跑法与 blocked 清单；09-23 00:1x 命令改锚定族形
  （`-run '^(TestCrash[A-Za-z0-9]*)$' ./test/`，见 §1.1 第 15 条）
- `scripts/flaky_watch.sh` — 契约② 判据表 FL 的执行者：四档（single/shuffle/isolated/parallel）+
  判决分类器（条数由 selftest 自增算出，不手写）+ `-a <ISO>` 时间线档（扫 `../core ../specs ../scripts`
  里 ±90s/前看 SLACK 的 .go mtime，并打印扫了多少个文件）；电池前后各取一次可归因戳（`build_stamp.sh`），
  两戳不同 ⇒ 整批判"探测不成立"；`--selftest` 喂合成计数证明分类器不是永真式。
  09-25 19:0x 轮：新增 **`preflight()` = FL-10 门①**，排在取戳之前 —— 先 `go build ./...`、再 `go vet "$MAIN"`，
  任一站不住就打印"哪个包哪一行编不过 + 等当事人交账"并 exit 2、**一次采样都不起**；
  **故意没有跳过预检的开关**（能被躲掉的门不叫门）。selftest 随之内插三条腿：假 `go` 让 build 红 ⇒ 拦、
  让 vet 红 ⇒ 拦、两个都绿 ⇒ 必须放行（防"永拦"把采样锁死）。
  09-22 23:4x 一处措辞整改：共享面枚举里"没有 `Config{` 字面量"这一档曾被印成"有测试吃了默认目录"，
  那是对 `cmd/core` 报一条**不存在的**风险（该包用 helper 构造配置，`DataDir: t.TempDir()` 在结构体字面量里）⇒
  现在两档各自说清"只证形状、不证结论"，真候选条件仍是 `Config{` 数 > `DataDir:` 数
- `core/test/statemachine_test.go` — §5.1/§5.2 独立转录，双向对账 + 终态 + UNKNOWN
- `core/test/projection_test.go` — §5.3/§5.4 投影（含 T-21 更正后的四条待填断言）
- `core/test/adapter_contract_test.go` — 协议 §2/§2.2/§3/§5/§7/§9 + CO-4 线格式；
  第二被测面 = 真子进程 FakeAdapter（真 build + stdio ndjson）
- `core/test/audit_chain_test.go` — T-14 哈希链时序不变量（13 列篡改 / Seal 顺序 / 脱敏接缝），
  已跟 REQ-T3 的 Kind 新形状；`TestRedactionKindIsMandatory` 记剩余一半
- `core/test/crash_test.go` — §7 Crash 清单：**09-28 T-276 W2-7 改写后 11 条顶层用例全真跑、0 条顶层 SKIP，
  跑不了的 5 格拆成子用例 Skip（判决表见 Completed 那一轮）**；09-23 那版的"2 条真跑 + 骨架"口径**已作废**。
  **09-28 19:1x 收口轮（唯一码改动）**：`:1408~1418` 的裸 `SELECT … FROM approvals`（W2-7 遗留、
  违契约③ §3.2.1 第 1 条 + §4 C1）换成 `approval.NewSQLiteStore(d, now).Get(...)`；
  断言 1419~1428 与文件其余部分一字未动，sha1 前 12 = `545493572759`、1571 行
- `core/internal/approval/store_sqlite.go` — **本轮只读未改**（`:58 NewSQLiteStore`、`:439 Get` 被 crash_test 调用）
- `core/test/turn_evidence_test.go` — §4.2 两帧取证信号四条语义的独立转录（判据表 T84-1..5）；
  7 形状只 1 形状可达 RUNNING；⚠1/⚠2 两处未定口径按"不写死"处理（T84-5 为 SKIP（blocked））
- `core/test/turn_attribution_test.go` — §4.2 裁定 ① 的交叉校验腿（T84-6a/6b）：真 `process.Supervisor`
  + 真 FakeAdapter + `CallTurn` 绑定，一致放行 / 不一致丢弃且两个 tracker 都不动、假 id 帧 0 转发。
  09-22 T-135 轮：文件头的两行小表改成**指向契约② T84 表的指针**（REQ-T10）；新增
  `TestDroppedFrameReachesAuditSeam`（T84-7a 现存 / T84-7b 显式 SKIP）+ 自带 `dropRecorder`
  观察面与 `var _ process.Recorder` 编译期钉子；`startSupervisedFake` 拆出 `startSupervisedFakeOpt(rec)`
- `core/test/errcode_mapping_test.go` — 契约② 判据表 C8-6/C8-7/C8-10/C8-11/C8-12/C8-13 六条行的 Go 执行者：
  §15.1/§15.2 双列解析（含 `skipped` 计数器与左列形状守卫）、`core/api/http/**` 的 `writeError`
  字面码提取（先去注释、再按顶层实参取第 3 个），事件目录 ↔ `eventjournal` 常量双向对拍 +
  合并行展开约定（①~⑤）的 6 行自证表。**五个顶层用例全部只用 `t.Errorf`、不用父级 `t.Fatal`**（T-124）
- `core/test/ws_cursor_contract_test.go` — REQ-F-20 游标权威三条判据（F20-1/2/3）：真 httptest +
  真 WS 握手 + 注入 `wsapi.Journal` 替身 + 自带 RFC6455 客户端；含"错读对照"与解析器活性对照
- `core/test/event_producer_roster_test.go` — **T-346② 裁的甲**：§6「有名字、非测试码无发射点」名单的执行者。
  `go/ast` 只扫**发射位**（`EventDraft` 复合字面量的 `Type:` 字段 —— 平台层四个写入口的末位实参都是它；
  `Tx.Append` 不认，它只写行不发事件），常量**按「包名 → 标识符 → 值」三级回解**（同名跨包是 09-27 首跑栽过的地方：
  `task.EventCreated` 与 `execution.EventCreated` 值不同），与 §6 展开后的词表做减法、再与散文「本名单当前是…」那句
  **双向**对拍；回解不通的必须点名到 `file:line`，不许静默算成「没有生产者」。
  合成件 `TestEventProducerScannerLegs` 四条：字面量发射位不算无生产者 / 只有定义位+注册表位的必须算 /
  同名跨包按本包回解 / 回解不通必须点名 + 双向差集两边都喊。**§6 展开没有第二份实现**（调同包的
  `parseEventCatalog`/`expandCatalogRows`）。真树腿的自证等 `core/` 干净，见 Verification 09-27 那条。
- `.qoder/progress/test.md` — 本文件

## Verification evidence

- **09-28 19:09~19:19（2026-09-28T19:20:15+0800 本机 date 实读）T-276 收口轮的全部读数**（全部现取，不抄上一轮；
  六件都留档 `.qoder/evidence/test/`）：
  - **① 越界自查（修后）**：`rg -n "FROM approvals|UPDATE approvals|INSERT INTO approvals" core -g "*.go"`
    ⇒ **15 命中、全在 `core\internal\approval\`**（store_sqlite.go 4 / store_sqlite_test.go 5 /
    blocking_sweep_test.go 5 / vertical_slice_test.go 1）；**`core\test\crash_test.go` 0 命中**。
    落盘 `own_grep_after.txt`。〔口径说明：bash 下写 `--include=*.go` 会出现污染输出，须用 `-g "*.go"` 才准〕
  - **② ownership selftest**：19:10:02 → 19:10:22，**`SELFTEST_EXIT=0`**，落盘 `own_selftest.txt`。
  - **③ ownership 正跑**：19:10:31 → 19:11:45，**`OWN_EXIT=0`、VIOLATION 0**、
    结论行「本次扫描 0 项越界」；baseline=900038d、HEAD=cde8d79、扫描面 **190 个 .go**。落盘 `own_real.txt`。
    （对照：修前 19:03:18→19:04:50 那次 `OWN_EXIT=1` / 1 VIOLATION = 本轮起因）
  - **④ t47-smoke（代跑别人的腿，脚本一字未改，mtime 09-27 15:09:40）**：
    run1 19:11:55→19:12:08、run2 19:12:58→19:13:08，两遍**同值**：
    断言行 **23 行 = 22 OK / 1 FAIL**（`RESULT_` 22 行里 21 OK + 1 FAIL，另有 `REQ_LK1= OK`）+ `RESULT=DONE`。
    **FAIL = `RESULT_SEND_TWO_PHASE`**，期望 `st==RUNNING ver==3`、实得
    `st=SUCCEEDED v=4`（run1/run2 各自 execution_id 不同，说明是同值复现、不是同一条残留）。
    其余 21 条 RESULT 全 OK：TICKET / REDACT / REPLAY / INBOX / PROJECT / PROJECT_CONFLICT / SYNC /
    PROJECT_LIST / SESSION_LIST / SESSION_GET / PATCH / PROJECT_GET / ADAPTER_PROBE / NOTIF_READ /
    SESSION_MANAGE / DISCOVER_CHAIN / LIFECYCLE_ROW / EXEC_LIST / QUEUE_KEY / GETTASK /
    DECLARED_NOT_WIRED。残留自查 `tasklist | findstr core.exe fake-adapter.exe` ⇒ MATCH_EXIT=1、**0 个残留**。
    落盘 `t47_smoke.txt` / `t47_smoke_run2.txt`。**归因见 Next action ①（不是我的码、不是我的判据面，交主控裁）。**
  - **⑤ build_stamp（代跑）**：19:15:17 → 19:15:39，**`STAMP_EXIT=0`**，原文 =
    `构建时刻=2026-09-28T19:15:17+08:00 HEAD=cde8d79 工作树未提交=42 文件/编译输入=2367 项/指纹=1a97a7f8d455202f7bc0d89afefa60f6  [./cmd/core ./test]`。
    落盘 `build_stamp.txt`。
  - **⑥ 我自己那三条验收**：
    `gofmt -l test`（cwd=core）**无输出 / GOFMT_EXIT=0**；
    `go vet ./...` 两次 **exit 0、无输出**（19:15:54→19:15:55 与 19:16:03→19:16:04，`vet.txt` 为空）；
    全仓 `go test ./... -count=1` 19:16:35→19:16:57 ⇒ **18 行 `ok` / FAIL 0 / `??` 0、GOTEST_EXIT=0**（`go_test_all.txt`）；
    定向 `go test ./test/ -run 'TestCrash|TestIdempotent' -v -count=1` 19:19:00→19:19:13 ⇒
    **PASS 9 / SKIP 5 / FAIL 0、exit 0**，`TestCrashApprovalNotLost` **PASS**
    （`crash_v_full.txt`）；另按你给的字面命令 `-run 'TestCrash'` 单跑一次 = **PASS 8 / SKIP 5 / FAIL 0**
    （`crash_v.txt`，差的那 1 = `TestIdempotentReplayBodyIsByteIdentical` 不匹配该正则，非丢绿）。
- **09-28 15:4x~15:48（2026-09-28T15:48:10+0800 本机 date）T-276 W2-7 的三份读数**（全部现取，不抄上一轮）：
  - **全仓 18 包**：`$env:LUCERO_GO="D:\DevTools\go\bin\go.exe"` 后 `go test ./... -count=1`
    ⇒ **18 行 `ok`、FAIL 0 行**（`api/http 8.223s` / `websocket 4.544s` / `cmd/core 19.367s` /
    `fake-adapter 3.710s` / `internal/*` 12 包 / `core/test 13.945s`）。落盘
    `.qoder/evidence/test/crash-skip-t276-0928-repo18pkg.txt`。
  - **定向 `-run`**：`go test ./test/ -run 'TestCrash|TestIdempotent' -count=1 -v`
    ⇒ **PASS 9 / SKIP 5 / FAIL 0**（`ok … 11.771s`）；9 条顶层全绿，5 条 SKIP 全是本轮拆出来的子用例：
    `…Interrupted/把_interrupted_认出来并改判（带证据）`、`…AdapterKill…/process_crashed_事件_+_process_instances_进_FAILED`、
    `…AdapterKill…/该_Adapter_的_in-flight_Execution_转_UNKNOWN_且带证据`、
    `…ApprovalNotLost/SweepExpired_只按_expires_at_判过期`、`…ApprovalNotLost/approvals_读写面（GET_list_/_GET_{id}_/_POST_{id}:resolve）`。
    落盘 `…-directed.txt`。
  - **整包**：`go test ./test/ -count=1 -v` ⇒ **PASS 70 行 / FAIL 0 / SKIP 13**，
    其中顶层 SKIP 6 条**全在别的文件**、子用例 SKIP 7（本轮 crash_test 5 + `TestDroppedFrameReachesAuditSeam` 2）。
    落盘 `…-pkg.txt`。
  - **SKIP 前后计数（逐条口径）**：`core/test/crash_test.go` 改前 **6 条顶层 `t.Skip`** ⇒ 改后
    **0 条顶层 + 5 条子用例**（现文件 `^\s*t.Skip(` 恰 5 处，全在 `t.Run` 内）；全包 SKIP 上一轮账本记 **12**、
    本轮实测 **13** —— 差额**不是本轮造的**：本轮净减 6（crash 顶层）净增 5（crash 子用例），
    别的文件那 8 条（顶层 6 + 子 2）本轮一条没碰。
  - **验收命令**：`gofmt -l test\crash_test.go`（无输出）、`go vet ./...`（exit 0）、
    `go test ./... -count=1`（18 包 / FAIL 0）、定向 `-run`（9/5/0）。**第 6 条变异**：
    `go test ./test/ -run TestIdempotentReplayBodyIsByteIdentical -count=1 -v` 变异时
    `--- FAIL`（FIRED，四条该咬的全咬住）⇒ 还原后 `ok … 1.751s`（回绿）。
- **09-27 16:3x~16:43（2026-09-27T16:43:13+0800）T-385 名册转绿的两面复现**：
  落点 `.qoder/evidence/test/roster-green-t385-0927-1641-f1558cc.txt`。命令（在快照的 `core/` 下，判据文件需从盘上 `cp` 进去再 `cmp`）：
  `go test ./test/ -count=1 -timeout 120s -v -run '^TestEventProducerRosterMatchesDerivedSet$|^TestEventProducerScannerLegs$|^TestProducerRosterParserLegs$'`；
  两棵树分别是 `git archive f1558cc` 与 `git archive f1558cc^` 的导出（`.scratch/test/roster-f1558cc` / `roster-a432b96`）。
  旁证那一跑在脏工作树（16 件在途），只按 Ⅰ 用；日志 `.scratch/test/pkg_t385.log`。

- **09-27 15:3x~16:00（2026-09-27T16:00:23+0800）T-379 与干净树全仓四张表**：
  落点 `.qoder/evidence/test/ownership-gate-t379-0927-1559-46dafae.txt`（含洞的定位四条、修前 FAIL 原文、四变异判决、
  修前/修后同树 diff、四张表与时刻）。复跑命令（都在 `core/` 下）：
  `bash scripts/check_ownership.sh --selftest`、`bash scripts/check_ownership.sh`；
  干净树那一套：`git archive HEAD | tar -x -C <快照目录>` 后在 `<快照>/core` 里
  `go test ./... -count=1 -timeout 900s -v`（第二面加 `LUCERO_TEST_FAKE_ADAPTER=1`）。
  **两条口径别混**：快照里有没有我那份未跟踪判据，决定了报 18 包 ok 还是 17 ok + 1 红。

- **09-27 14:5x~15:08（2026-09-27T15:08:10+0800）T-371 的复跑面**：落点 `.qoder/evidence/test/roster-criterion-0927-1506-t371-layer4-bc03526.txt`。
  命令（`core/` 下、锚定形）：
  `go test ./test/ -count=1 -timeout 120s -v -run '^TestEventProducerRosterMatchesDerivedSet$'`（快照那一跑要带
  `LUCERO_CODE_ROOT=<绝对路径>/snap-bc03526/core`），`go test ./test/ -count=1 -timeout 90s -run '^TestEventProducerScannerLegs$' -v`。
  两棵树对照：`bc03526` 快照（75 个非测试 .go，关闭面）与 15:04 脏工作树（76 个，旁证）⇒ 三桶与红名单 `diff` 无差异。
  Ⅱ 门现场原文（15:03:38 现读，5 行）已抄进证据文件第 1 节 —— 主控 ⑤ 与巡检 14:51:58 那句"干净"在它之前成立、在它之前不成立。
  **顺带一条 lint 读数（15:08:32，exit 1 = 2 项越界，全是别人在途码、不是我的改动引起的）**：
  `core/api/http/declared_not_wired_test.go`（未跟踪）里出现 `"UPDATE approvals"` / `"INSERT INTO approvals"`
  ⇒ 触发 /1 那腿（approvals 表 SQL 只准出现在 `internal/approval`）；`core/api/http/executions.go`（` M`）里
  `execution_output_chunks` 以变量表名形状出现 ⇒ 触发 /2 那腿。两条都落在 backend 未提交的文件上，
  **只上交、不代判**（归属与是否有意，当事人答）。同刻 `--selftest` exit 0、0 条 SELFTEST FAIL、9b 仍是 49/49 无重号。
  当刻 `HEAD=93af696`、`baseline=900038d`、扫描面 179 个 .go。**我这一轮的快照被验树是 `bc03526`，与本条读数不同树，两件事分开看。**
  第三棵树（15:12 现读，`HEAD=93af696` 15:07:16、非测试 .go 75）：`.scratch/test/C3_93af696.log` ⇒
  桶一 37（仅第四层 2）/ 桶二 27 / 名单 3 / **现红 24** / 桶三 0 / N=76 M=29 K=37，红名单与 `bc03526` 那份 `diff` 无差异。
  **自记一条我自己的老坑**：14:15 那次往本节插入时，我把 anchor 走进了一条**续行段落中间**，把上一条 T-365 条目劈成两半
  （续行不是 bullet，`startswith("- ")` 判不出段界）—— 本轮按段末重切回去了，记忆里有这条还是踩了。

- **09-27 13:5x~14:13（本机 date 2026-09-27T14:13:40+0800）T-365 形状修复的自证**：
  落点 `.qoder/evidence/test/roster-criterion-0927-1412-ec47033-shape-t365.txt`（含三棵树读数、四条变异原文判决、
  盘上 219-223 五行原文）；处置表 `.qoder/evidence/test/roster-triage-0927-1409-00bb3c4.txt`。
  命令面（锚定形，`core/` 下）：
  `go test ./test/ -count=1 -timeout 120s -run '^TestEventProducerRosterMatchesDerivedSet$' -v`（配 `LUCERO_CODE_ROOT` 指快照）、
  `go test ./test/ -count=1 -timeout 90s -run '^TestEventProducerScannerLegs$' -v`。
  一次坑自记：`LUCERO_CODE_ROOT` 我给成相对路径 ⇒ 包工作目录是 `core/test/`，快照判据报的是 `扫 … 失败：The system cannot find the path specified.`
  —— 那是**我自己起错路径**，不是判据红；换成绝对路径后才有数（否定结论要附命令原文，这条我照办）。
- **09-27 14:1x 一条结构性事实（影响所有证据的归属）**：`git check-ignore -v` 指着 `.gitignore:30` 的 `.qoder/evidence/`
  ⇒ **本角色全部证据文件都不在版本控制里**（`git ls-files .qoder/evidence/test` = 0 个）。所以「入册」这件事对证据这一层
  永远不成立：能被提交的只有 `core/test/**` 与 `scripts/**` 和 progress 正文。往后凡是主控要引用的读数，
  **关键数字必须抄进本文件的 Verification evidence**（那才是跟踪面），证据文件只是副本；这条也解释了 ⑧款Ⅱ 为什么必须认。


- **09-27 13:45:55~13:46:47（本机 date）T-363 要的「当场重跑 + 单一数字」**：
  - Ⅱ 现状逐字（13:45:55，`git status --porcelain -- core/`，存进 `.scratch/test/t363_dirty.txt`）：
    `M api/http/send_two_phase_test.go`、`M cmd/core/sendpipe.go`、`M cmd/core/sendpipe_test.go`、
    `?? test/event_producer_roster_test.go` ⇒ **除被验件外还有 3 件** ⇒ 工作树那一面不起（我照 ⑧款Ⅱ 停）。
  - 但巡检要一个当场数字，所以我走的是同一款里我请求认可的那条替代路：**HEAD 快照**。
    导出时 `HEAD=9660e68（13:36:52）`、快照 .go 176 个、未动工作树；跑法
    `LUCERO_CODE_ROOT=<快照>/core go test ./test/ -count=1 -timeout 120s -v -run '^TestEventProducerRosterMatchesDerivedSet$'`
    （用例实现仍是我工作树那份——快照换的是"被扫的代码"，不是判据本身）。
  - **单一数字：26 条未登记**（口径：词表 64 → 覆盖 49（字面量 1 / 常量 14 / 汇聚 11 / 兜底 1）→ 派生无生产者 29
    → 名单 3 ⇒ 26；`missing=0`；定不到的发射位 1 处、带 `file:line` 进读数）。
    **44/41 是我修回解之前那几版坏判据的中间读数**，状态列不许用它——这一句同时回主控 13:33 那行的"44 条"。
  - **稳定性**：三份已入册树（`2964ecb` 12:55 / `69e96ec` 13:17 / `9660e68` 13:36）+ 13:24 的工作树，
    红名单 `diff` **逐字相同** ⇒ backend 这三件在途改动不改变分类；这也是「认 HEAD 快照」那条的最硬实证。
  - **未交的事**：没交"全仓绿"（整包两面唯一 FAIL 就是这条判据：面 A 45/1/12、面 B 50/1/7，58 条）；
    没扩名单、没降 INFO、没缩扫描范围。证据 `roster-criterion-0927-1345-headsnap-9660e68-red26-rerun.txt`。

- **09-27 13:33~13:35:16（本机 date）T-359 转投后自查：我自己上一封的树标签是错的，已重做**：
  - 巡检测出「`core/` 只剩我自己那一件」只在 ~13:18~13:19 成立之后，我去核**我自己的**标签，抓到一个错：
    我在证据与交账里写的「B = HEAD 快照」，那份快照其实是 **13:06:44 导的 `2964ecb`（12:55:34）树**，
    而 backend 那五件是 **13:09:08 才入册成 `9f07a80`**（`git show --stat` = 6 个文件：
    `api/http/executions.go` +129、`api/http/send_two_phase_test.go` +315、`api/http/tasks.go` +78、
    `internal/execution/send.go` +107、`api/http/send_test.go` +2、`specs/contracts-internal-v0.1.md` +10），
    主控的 ledger 又推进到 `69e96ec`（13:17:57）⇒ 我 13:21 跑 B 的时候，"HEAD"这个词指的是**上一代**的树。
    结论方向没翻（那仍是一棵已入册、无 `M`/`??` 的可归因树），但按 ⑧款Ⅲ 与我自己的「必带某棵具体的树」，
    **标签说错就是不合格的描述**，所以我重导快照重跑：
  - **B2（真 HEAD=`69e96ec` 的快照，13:33:2x）**：`导出时 HEAD=69e96ec 13:17:57`、快照 `.go` 176 个、
    `词表 64 / 非测试 .go 75 / 发射位 27（字面量 1 · 常量 14 · 汇聚 11 · 兜底 1 · 定不到 1）/ 覆盖 49 / 派生无生产者 29 / 名单 3`
    ⇒ **红 26 条，与 A（工作树 13:24）的红名单 `diff` 逐字相同** ⇒ 「两棵树同分类」在正确标签下依然成立，
    只是现在才配叫 HEAD 快照。已把这段以「补记」追加进同一份证据文件（原来那段不删，就地超越）。
  - 13:34 复读 `git status --porcelain -- core/` = `M api/http/send_two_phase_test.go`、`M cmd/core/sendpipe.go`、
    `M cmd/core/sendpipe_test.go`、`?? test/event_producer_roster_test.go` ⇒ **backend 又回 `core/` 了**（在做 send 期限那一格），
    所以 A 那一面我不重跑（跑在别人的中间态上没有归属意义），B2 才是这轮的可归因面。
  - 时序按主控 ① 的口径对齐：13:05 巡检推我时 Ⅱ **不成立**（五件未入册）；`9f07a80`（13:09:08）之后 Ⅱ 成立；
    我在 13:21~13:26 起的是**修完回解之后**的那一次，红因写的是我自己那道门的 finding（26 条），没写成「被在途件挡住」。
    巡检自记那条（把「止笔」当「已入册」）我接受，并把它当成 REQ-T34 里那句「认 HEAD 快照」的又一个实证。

- **09-27 13:05~13:28:30（本机 date）AST 判据的首次可归因跑 + 五轮回解修复**：
  - ⑧款 Ⅰ 的逐字脏清单（13:05:00 现量、存进 `.scratch/test/t346_dirty_before.txt`）：6 行 =
    `M api/http/executions.go`、`M api/http/send_test.go`、`M api/http/tasks.go`、`M internal/execution/send.go`、
    `?? api/http/send_two_phase_test.go`、`?? test/event_producer_roster_test.go`（最后一件=我自己）。
    巡检 13:04 判「backend 止笔」量的是 mtime，⑧款Ⅱ 的字面量的是「有没有未入册」——两件事，所以我没按字面硬闯：
    **另跑一棵 `git archive HEAD core` 导出的快照**（未动工作树），A/B 两棵树的红名单 **diff 无差异** ⇒
    这轮之争对本读数是空的；已把这件事交主控（REQ-T34 附一句），不自己改口径。
  - 起腿/收尾戳：13:21:01 `HEAD=69e96ec 编译输入=2354个 指纹=7f642…`；13:26:32 同一枚指纹 ⇒ 这五分钟里编译面没动。
  - 真树腿（A=工作树 / B=HEAD 快照，命令都带锚定 `-run '^TestEventProducerRosterMatchesDerivedSet$'`）：
    `词表 64 个名字；非测试 .go 75 个；EventDraft 发射位 27 处（字面量 1 / 常量 14 / 别名或调用 11 / 兜底 1 / 定不到 1）`、
    `被覆盖到的名字 49 个；派生「无生产者」29 个；名单 3 个` ⇒ exit 1、26 条「派生有而名单没登记」。
    两条反例位现在都被认成有生产者：`task_running`（经 `edgeEvent` 汇聚）、`project_created`（裸字面量）。
    唯一看不见的位点带行号打进读数：`internal/process/lifecycle.go:223（表达式=evType，lits，覆盖 0 个候选）`。
  - **26 条的机械核验**（不是抽查）：对每个红名跑「非测试码里去掉 `Ident = "<名字>"` 这类定义/注册行后还剩几处」
    ⇒ **26 条全部为 0 痕迹**；另抽 `task_queued`/`notification_created`/`process_exited` 读原文，同样只剩两处定义
    （`internal/eventjournal/events.go` + 业务包 `events.go` 别名）。
  - 整包两面（本轮新增 2 条用例，56 → 58）：面 A **45 PASS / 1 FAIL / 12 SKIP**、面 B（带旗标）**50 / 1 / 7**；
    **唯一的 FAIL 就是本条判据**（两面同一条件、同一用例），其余全绿 ⇒ 我之前预告的「我的在途件会吃掉别人的全量绿」
    现在成立了，只是红因是 finding、不是我的用例坏。
  - `go vet ./test/` exit 0（退出码单独取）、`gofmt -l core/test/` 空；合成腿 `TestEventProducerScannerLegs` 每一版改完都重跑、
    最后 PASS exit 0。9b 门本轮没动（`scripts/**` 无新增改动）。
  - 证据：`.qoder/evidence/test/roster-criterion-0927-1326-admissible-26undeclared-red.txt`（含 A/B 两棵树、脏清单逐字、26 条名单与核验口径、写法普查）。

- **09-27 12:36~12:45:08（本机 date）T-346 补裁两件落地（真树腿触发条件 + 失明分级）**：
  - **② 的语义按主控钉的改了**：`resolveDraftExpr` 现在把**候选值**一起带出来（`valueList` 取代原 `singleValue`），
    新增 `partitionBlind`：**只有当某处回解不通、且它的候选值里含某个「无生产者」名字时**，那个名字才 FAIL
    并打印 `file:line`（同时从两边对拍里摘掉——判「没有」是假红、判「有」是假绿）；
    **候选为空的那类**（`Type: 变量`、字段访问）不影响任何名字的归类 ⇒ 只 `t.Logf` 报范围、不判红。
    理由照原文写进了注释：否则别人新增一种发射写法就把这条门变红 = 拿假红逼别人改码。
  - 合成件加了一格专门喂这个分级：两个目录都声明 `package journal`、同一个 `Amb` 取不同值
    ⇒ `journal.Amb` 定不到唯一值、候选里含 `gamma_created` ⇒ 断言 `blind == [gamma_created]`、
    `judged == 空`、`other_created`（候选里那个不在词表的值）不进 blind；
    `svc/d.go` 的 `Type: v`（零候选）断言**不**制造 blind。发射位计数随之从 4 改 5、失明位从 1 改 2。
  - **重跑读数（截至 12:43:41 的那棵具体的树）**：`HEAD=67f064b`、`go vet ./test/` **exit 0**（退出码单独取，
    不接管道尾巴）、`go test -run '^TestEventProducerScannerLegs$'` **PASS / exit 0**；
    前后两次 `build_stamp.sh`：`编译输入=2354个 指纹=b7fe9fe63b6eb28df8274e4806745641`（12:43:39 与 12:43:41 同值）。
  - **条件Ⅱ 仍不成立 ⇒ 真树腿没起**：`git status --porcelain -- core` 现读 **6 行**，逐字是
    `M core/api/http/executions.go`、`M core/api/http/send_test.go`、`M core/api/http/tasks.go`、
    `M core/internal/execution/send.go`、`?? core/api/http/send_two_phase_test.go`、
    `?? core/test/event_producer_roster_test.go`（**最后一件是我自己未入册的在途件，不是别人的**）
    ⇒ 除我那件之外还有 5 件在 backend 手上，按 Ⅱ「任意一件在 ⇒ 不起真树腿」我等。
    主控 12:36:44 复量也仍是那 5 件，两边读数不矛盾（我这把多了 `send_test.go` 一行，是 12:40:45 那次提交之后又动的）。

- **09-27 12:07~12:25:56（本机 date）T-346 复量与新用例首跑**：
  - 复量链（每步现读，不转抄 47 小时前的账）：`HEAD=af30a9a`（22:05:32，之后无提交）→ `core/` 未提交 0 处 →
    `scripts/check_ownership.sh` 里 `check_id_unique` 命中 5 处、其 `git log -1` 仍是 `95b01ca`（18:29:43）
    ⇒ **9b 门确实未入册**；`api/http/executions.go:170` 仍是 `c.Store.Write(...)`、
    `internal/execution/send.go:220` 仍在其中调 `CallSendTurn` ⇒ **① 裁乙的前提（还没搬）成立**。
    巡检「② 在盘上仍不存在」我认：写本文件之前 `grep -rl "go/ast" core/test/` = 0。
  - **两把读数要留档、不许当证据**：
    ⑴ 12:17:03 那次真树跑（§6 64 个名字、发射位 27 处、常量 19 / 字面量 1 / 回解不通 7、派生一大串）——
      它的「前后同指纹 `88f3dc17`」是**假的**：我那条内嵌 `sed` 在这个 shell 里报
      `unterminated s command`，喂给 `md5sum` 的输入是空 ⇒ 空输入永远得到同一个摘要 = **空清单指纹永真**
      （正是 `build_stamp.sh` 里 `MIN_FILES` 那道闸要拦的事）。改用 `bash scripts/build_stamp.sh` 后取到真值：
      `起 12:18:38 编译输入=2353个 指纹=e887e2907093a17…` / `收 12:18:39 同一指纹`——
      同一指纹这次是真同（一秒内取两次），但它测的是**断编译的那棵树**，所以仍然不算验收读数。
    ⑵ 那次的「回解不通 7 处」里有 **5 处是我规则的错**（同名跨包），不是代码的错 ⇒ 已按包限定重写。
  - 新用例两条腿的现状：`TestEventProducerScannerLegs` 12:16:44 那次 PASS 是**改包限定之前**的版本，不算数；
    12:26:03~12:26:26 在**改成按包查之后**重跑：**PASS、exit 0**，前后两次 `build_stamp.sh` 都是
    `编译输入=2353个 指纹=fe4d154285bf85e77ef850c5d7cec8b0`（同一指纹，这次是真取到的、不是空清单）——
    可那一刻的树是 **`HEAD=d4e0009` + `core/` 脏 4 处**（`M api/http/executions.go`、`M api/http/tasks.go`、`M internal/execution/send.go`、`?? test/event_producer_roster_test.go`——最后一件是我自己的在途判据），
    所以这条读数**只证两件事**：我的新文件类型检查过了、合成腿在按包查之后仍站住；
    **它不是 ② 的验收自证** —— 那一格要等别人的在途件落定 + 真树腿跑通才算，我没把它写成已交。
    按主控 12:18:3x 那句更正立成本轮的读数规矩：**任何全仓绿读数一律带「截至 HH:MM 的某棵具体的树」
    （HEAD 短码 + 脏文件全清单），并且不得当 REQ-S-1 或 Wave-2 的关闭证据**。
    口径再补一句、这条要主控对一个字：`core/` 里**我自己的那一件**（就是这条判据本身）不该被当成
    「别人的在途件」来阻塞自证 ⇒ 真树腿的触发条件我读成「`core/` 里除我这一件之外无 M / ??」；
    这与 T-299 定的「被验面 = `core/` 全量」字面有冲突，**我对齐之前不起真树腿**。
  - 编译面读数（12:18:38~12:21:29）：`go build ./internal/execution` 报 5 行同类错（`SendResult{}` 当
    `SendReservation` 用）；`gofmt -e` 我的新文件语法 OK、`gofmt -l test/` 空。
  - 形状普查（粗筛、非判据）：非测试码 `Type: 字面量` 10 处 / `Type: 标识符或常量` 43 处 / `EventDraft{` 27 处。
    ⚠ 三个数**不互相扣除**（`Type:` 也出现在别的结构体上），只用来回答「两种写法是否并存」这一个问题。

- **09-25 20:22:51~20:45:07（本机 date）T-316④ 落地与⑤探针的原始读数**：
  - 起跑前复量：`HEAD=8e0a376`（20:16:27）、`git status --porcelain -- core` = **0 行**、
    `git log -1 -- scripts/flaky_watch.sh` = **`e94c989`（20:08:00）** ⇒ 巡检说「门① 已按角色入册」成立，
    我 19:48 那句「仍未入册」当场作废、不留当现状。① 的落文回读：契约② FL-10 行含我那句
    「本行仍不宣称 REQ 闭」**1 处**；② 的回读：`^| F20-` = **4 行**、`^| FL-[0-9]` = **10**、`^| T84-[0-9]` = **8**。
  - 9b 门在真树上：共 49 条 ID、去重后仍 49；两条覆盖面 INFO 都打（含「散文引用不算登记、这条腿看不见它们」那句）；
    `bash -n` 干净、`check_ownership.sh` **exit 0**、`--selftest` **exit 0**。
  - 三把锤的锚点数：A = `[0-9]+[a-z]*` **5 处**（代码 4 处 + 注释 1 处，全在 9b 内）、
    B = `if (( n != u )); then` **1 处**、C = `if [[ -z "$files" ]]; then` **1 处**；
    每次还原后 `md5sum | cut -c1-8` 都等于变异前的 `024c755a`。
  - ⑤ 探针（纯读、不改任何文件）：`internal/eventjournal/events.go` 解出 **64 个事件名**；
    按「除定义位/注册表位外零引用」派生出 **21 个** —— 除名单那三条
    （`policy_created` / `policy_updated` / `adapter_downgraded`）之外还有
    `project_created`、`project_archived`、`session_synced`、`grant_revoked`、`core_shutdown`、`task_blocked`、
    `task_state_corrected`、`task_verifying`、`task_waiting_acceptance`、`approval_cancelled`、`handoff_*` 四条、
    `model_discovered`、`quota_observed`、`execution_output_flushed`、`adapter_restart_exhausted`。
    散文「本名单当前是三条」在事件契约第 **283** 行（只作定位用，不作 durable 引用）。
    **反例读证**：`internal/task/service.go` 用常量 `EventCreated` 发射 ⇒ 我的规则数得到（3 处引用，正确判「有生产者」）；
    `internal/project/service.go` 用裸字面量 `Type: "project_created"` 发射 ⇒ 规则数不到（误判「零生产者」）
    ⇒ **同一种发射有两种写法**，这就是⑤不能按我这条规则落地的直接证据。
  - 本轮脏的文件只有 `scripts/check_ownership.sh` 一个 ⇒ **没有 .go 改动**，19:05 那两面（44/12、49/7、0 FAIL）
    仍归属当前编译面；我**没重跑 Go 用例** —— ⑥ 说 adapter 正在跑 T-314 的**计时**读数，起 `go test` 会污染它的时间窗
    （这是排程约束，不是我省事）。

- **09-25 19:05:17~19:15:18（本机 `date` 收尾）合格批 + 交账前最后一次重跑**：
  - **FL-10 合格批**：窗口 `19:08:22→19:14:02`，`预检（FL-10 门①）` 打印在采样之前，
    起戳 `19:08:24 HEAD=f25ee6c 编译输入=2350个 指纹=6b36e9e3930864b5f6c17fcec38e1ff9`、
    收戳 `19:13:58` 同一指纹；`PASS=122 FAIL=0 SKIP=0 BROKEN=0`；载荷三臂 9311/9322/9332；
    窗口内 `find core -name "*.go" -newermt 19:08:22 ! -newermt 19:14:10` 空；`lock.md5=6c379e1c`。
  - **两面重跑（19:05:17~19:05:51）**：面 A（不带旗标）顶层 **44 PASS / 12 SKIP / 0 FAIL = 56**、子用例 17 PASS / 2 SKIP；
    面 B（`LUCERO_TEST_FAKE_ADAPTER=1`）顶层 **49 PASS / 7 SKIP / 0 FAIL = 56**；`go test ./test/ -count=1` = `ok … 5.164s`。
    ⚠ 与 11:10 那次（42/14）差 **+2 PASS / −2 SKIP**，**不是我这轮改的**（我这轮只动 `scripts/flaky_watch.sh` 的自测计数）。
    **但这二者的因果我不写**：早上那次的 SKIP **名单**我没落盘（`.qoder/evidence/test/go-test-final.txt` 最新一份是
    **09-24 11:10** 的 13 名，不是 09-25 的 14 名），所以对不上账、点不出是哪两条翻的 ⇒ 只报"两面各 +2/−2、
    发生在 `a9f975e` 前后的某处"，具体归因谁需要就说一句我去追。
    **规矩补一条给自己的**：计数面必须连**名单**一起归档，只归档数字 = 下次想对比就没有可比（本轮就是现例）。
    本轮已把两面名单连头注重新归档（见下）。**"全"的口径**：这两面只覆盖 `./test/` 一个包；**18 包全量我没跑**
    （跑法与跳过项按 T-257③ 分写：`go test ./...` 会吃 frontend/adapter 在途面，读数不属于我）。
  - **归档刷新（19:1x）**：`.qoder/evidence/test/go-test-final.txt`（面 A，头注含 HEAD/预检/两层计数）与
    `go-test-fakeadapter-final.txt`（面 B）重写在档；两面 SKIP 名单差 =
    `TestFakeAdapterSubprocess`、`TestCancelReplyIsResultFrame`、`TestFakeAdapterDeliveryModesOnTheWire`、
    `TestFakeAdapterModelSelectLegs`、`TestTurnRefCrossCheck` 五条（B 相对 A 少这五条 SKIP ⇒ 它们要真子进程替身才跑得动）。
    `crash-final.txt` 仍是 09-23 01:30 那份 —— 本轮没重跑 `crash_recovery.sh`，**不假装它刷新过**。
  - **五个脚本（19:07:26~19:07:4x）**：`check_ownership.sh` exit 0（"本次扫描 0 项越界" + INFO 值位计数照打）、
    `check_ownership.sh --selftest` exit 0、`flaky_watch.sh --selftest` **exit 0（17 条）**、`build_stamp.sh` exit 0、
    `crash_recovery.sh` 未在本轮重跑（它的判据面 = `./test/` 那 5 条真跑用例，两面读数已在上面）。
    **一处我自己的口径事故要记**：那条 for 循环用 `printf '%s' "$out" | grep -vE … | tail -1` 取末行时，
    `flaky_watch --selftest` 那格印成 `Binary file (standard input) matches` —— 我差点把它读成"输出坏了"。
    落盘重跑 + `tr -d '\000'` 前后字节数同为 2321 ⇒ **无 NUL，是我的外层管道把多字节判成二进制**；
    真读数（`selftest_exit=0`、17 条 OK）来自文件不回来自管道尾巴。
  - **`a9f975e` 的文件面（19:15:05 现读，不是转抄主控的行）**：7 个文件 +488/−11
    （`api/http/tasks.go`、`api/http/tasks_link_test.go`、`cmd/core/sendpipe.go`、`cmd/core/sendpipe_test.go`、
    `internal/task/{domain,repository,service}.go`）⇒ 正是我 18:48 那批里"未提交的 7 处"，**原样交上来的**，
    所以 124 那批的指纹与这版的指纹相同不是巧合，是同一份码换了归属状态。
  - **19:28:02~19:28:18 复量（T-293 被巡检重投一次，我按"不必重做、带复跑读数"回）**：
    `git log -1 -- scripts/flaky_watch.sh` 仍是 **`2c56023`（17:47:36）** ⇒ 我那 +58 行的预检门**还没入册**（`mine=1`），
    等主控按角色切一次提交；`preflight()` 在盘上 1 处、主路径第 416 行 `if ! preflight; then exit 2; fi`；
    `--selftest` 复跑 **exit 0 / 17 条**；FL-10 行自 `4b4739e`（18:47:07）后**没有再被改**（`git log -1 -- 契约②` 就是那条）。
    ④ 的现盘复量（带字母后缀那一版正则）：FL 10/10、C8 14/14、W13 3/3、F20 4/4、U13 7/7、A18 3/3、T84 8/8 ⇒ 七族全唯一，
    `^| FL-6` 只有 243 行一处 ⇒ 巡检那句"不必去现盘找那两行"我核过，成立。
    **回给巡检的一条口径**：那封里"18:49:58 量到四件仍 ` M` ⇒ 按门① 此刻预检应当站不住"是**从 porcelain 推的、不是量的**。
    我手上的读数是：18:41:21~18:46:58 那批 122/122 全 PASS（那六分钟编得过）、18:56:30 `go build ./...` exit 0；
    真正"编不过"的是 18:34（BROKEN=116）与 18:35（BROKEN=39）那两批。**`M` 只证内容改过，不证当前不能编译**
    —— 门② 用 porcelain 判"有没有交账"是对的，但别拿它当"预检必然拦得住"的证据，两问不同。
  - **ID 唯一性基线（两次数，第一次是错的、留着免得下一位再踩）**：

    19:10:26 第一次只量了四族（FL 10/10、C8 14/14、W13 3/3、F20 4/4）；19:22:52 扩到 REQ-T25 点名的七族并**改成带字母后缀的
    正则**：FL 10/10、C8 14/14、**T84 8/8**、W13 3/3、F20 4/4、U13 7/7、A18 3/3（U13/A18 在 `specs/contracts-internal-v0.1.md`，
    我第一版只扫三份文件里的两份、连"表在哪"都没找全）⇒ **七族此刻全唯一 = 这条腿挂上去是 0 红的静态门**。
    **★ 中途我自己造了一个假红**：用 `^\| T84-[0-9]+`（少 `[a-z]*`）量出"字面 8 / 去重 7"，差点当场报"契约② 又有一处重号"；
    真因是 `T84-7a`、`T84-7b` 被截成同一个 `T84-7` ⇒ 这正是我记过无数次的"对合并/后缀形状的词表做字面 grep 必假红"，
    今晚轮到我自己在**提判据的前一刻**踩它。取数一律锚行首（散文里 `F20-1/F20-2/F20-3` 同行并列、`**A18-3` 加粗小标题、
    `CO-nn` 裁定编号都不算登记），覆盖面只宣称"七族 × 行首表格行"，不宣称管住所有 ID。
  - **19:29:37~19:30:05 复量（T-299 被重投一次；巡检要"并发的具体时刻"，我给到了）**：
    adapter **确实在我 122 次的窗口里跑过合验** —— `.scratch/adapter/` 现读 `heyen-run-191239.log` 与 `heyen-fp.txt`
    mtime **19:12**、`legs-r22.py` **19:13**（窗口 = 19:08:22~19:14:02），其 `LAST_UPDATED` 已从 18:57:51 推进到
    **19:17:19（第 22 轮）** ⇒ 负载口径改成"自测 ≈82 进程/秒 **+ 19:12~19:13 adapter 合验并行（真子进程）**"，
    已把这段**补记进证据文件尾部**（原来"并发量未知"那几行不删、就地超越）。
    顺带纠一条巡检引用的读数：`flaky10b_seg_190822.log` 在 19:10 仍在涨**证的是我这批在跑**，不证 adapter 在跑；
    该文件现读 10853B / 19:14:01 = 我收尾时刻，此后不再涨。
    **我自己作废一条读数**：19:29:37 那次 `echo "x=$(tasklist … | grep -c …)"` 得到两个 `0` 是假的
    （`//FI` 被嵌套引号弄坏 ⇒ tasklist 报"无效参数/选项 - eq"，`grep -c` 数的是错误信息）；19:30:05 直接跑同一过滤器
    回的是 tasklist 自己的零命中文案"信息: 没有运行的任务符合指定标准" ⇒ 那才是真 0。
    **判"某进程不存在"要拿工具自身的零命中文案，别拿管道尾部的计数。**
  - **19:43:21~19:45:25 只读复量（T-303③ 之后我不再起采样，这一格全是读）**：
    - **④ 自查**：`git show --stat 6b28868 -- specs/events/event-contract-v0.1.md` = **13 insertions(+), 0 deletions(−)**，
      逐行看新增内容**全部是 `>` 引用块散文**（"名单现状 · 两处更正"）⇒ §6 词表行一行未动。
      行为证据：`check_ownership.sh` **exit 0**、事件那一腿仍报 **19 个事件名全部在事件契约里出现过**、
      且 19:45 那份 INFO 全块与 19:20 归档那份 `diff` **无输出**（逐字相同）⇒ **C8-11 族判据输入没变、不重跑**。
    - **树的状态（两次读数不一致，如实记）**：19:45:04 量到 `git status --porcelain -- core` = **1 行**，
      19:45:25 同一命令 = **0 行** ⇒ 那 1 处是**瞬时**（有人在那 21 秒里写了一次又收手，或跑了一次生成物）。
      **我没去查是谁**（③ 要求我把窗口让给 adapter），只记"截至 19:45:25 `core/` 干净"。**这条正好当反例**：
      单点 porcelain 读数不能写成"这段时间一直干净"。
    - `HEAD` 走到 **`c46339c`（19:42:00）**，`git show --stat` 只含 `.qoder/progress/orchestration.md`（+4/−2）⇒ ledger-only，编译面没动。
    - **我的交付面仍未入册**：`git log -1 -- scripts/flaky_watch.sh` 还是 `2c56023`（17:47:36）、`git status` = ` M scripts/flaky_watch.sh`。
    - 巡检 19:40 那条更正我核过且**同意**：当时 `.scratch/test/` 最新项是 19:28:18 的 `selftest_recheck.log`（自测不是采样）；
      19:45 之后该目录又多了 `oc_now.log`（lint 输出，同样不是采样）⇒ **别拿 `.scratch/test/` 的 mtime 当"test 在采样"的证据**，
      要判就判 `flaky10b_seg_*.log` 或 `churn.stop` 这类只属于采样的文件（我此刻**没有** `churn.stop` 残留）。
  - **台账卫生（我自己的一次自纠）**：先立了 REQ-T28、随即发现它与 REQ-T25 同格 ⇒ 用脚本把正文并进 REQ-T25、
    REQ-T28 只留一行作废指针（断言过：整段起点唯一、下一族标题唯一、替换后标题各出现 1 次、章节 11、围栏 4）。
    同一趟还抓出**这台机器上的一个假读数**：`grep -c $'\r' file` 报"2599/2599 行含 CR"，而 `tr -dc '\r' | wc -c` = **0**
    ⇒ 那个 `$'\r'` 在这套 shell 里塌成了空模式（匹配每一行）；**判行尾一律用字节计数，别用 `$'\r'` 的 grep**。
- **09-25 18:48:58~19:02:57（本机 `date` 收尾）FL-10 并池批 + 预检门的原始读数**：
  - 并池：4 段 × 31 次，段窗 `18:48:58→18:50:27`、`18:50:29→18:52:00`、`18:52:02→18:53:37`、`18:53:39→18:55:14`；
    每段 `exit=0 PASS=31 FAIL=0 SKIP=0 BROKEN=0`，段内前后指纹 `6b36e9e39308`×8 全等 ⇒ 并池 **有效样本 124**。
  - 归属：`HEAD=4b4739e`（18:47:07 那次提交，批内没有新提交落进来）、`lock.md5=6c379e1c`（归属 `2c56023`）、
    `core/ 未提交=7 处`（backend 在途：`api/http/tasks.go`、`cmd/core/sendpipe.go`、`internal/task/{domain,repository,service}.go`
    + 未跟踪 `api/http/tasks_link_test.go`、`cmd/core/sendpipe_test.go`）、`core/test + scripts 未提交=0 处`（起跑时）。
  - 窗口安静性：`find core -name "*.go" -newermt "2026-09-25 18:48:00" ! -newermt "2026-09-25 18:56:00"` **输出为空**。
  - 预检补跑（事后，18:56:30~18:56:4x）：`go build ./...` exit 0、`go vet ./test/` exit 0、`go vet ./cmd/core` exit 0。
    ⇒ 结论只能是"那批不是烧在编不过的树上"，**不能**倒推成"门① 满足了"。
  - 预检门落地后：`bash -n` 干净；`--selftest` **17 条 exit 0**；真路 `… -m single -n 2` 先打印
    `预检（FL-10 门①）：go build ./... exit 0、go vet ./test/ exit 0 ⇒ 起采样。`，随后 2 次 PASS、收戳指纹与起戳相同。
  - 变异 A/B 的锚点串 `if (( rc != 0 )); then` 命中数 **2**（改前先数，防"锚点不唯一 ⇒ 拒改＝无效"），
    两次变异各跑一次 selftest：A ⇒ 2 条 FAIL/exit 2；B ⇒ 1 条 FAIL/exit 2；还原后 `md5sum` 前后一致。
  - 19:02:57 证据面：`git diff --stat HEAD -- core/test scripts` = `scripts/flaky_watch.sh | 58 +`（1 file changed, 58 insertions）；
    `git status --porcelain -- core/test scripts` = 1 行（` M scripts/flaky_watch.sh`）；新证据文件
    `git diff --no-index --stat /dev/null …` = 52 insertions（`.qoder/evidence/` 被 `.gitignore` 忽略，只在盘上）。


- **09-25 18:02~18:16（本机 `date` 收尾 18:16:45）T-280① 键位排除的取证**：
  - 复现：18:02:44 `check_ownership.sh` **exit 1** + `1 处：attachments@core/internal/session/record.go`；
    命中行现读 `b, err := json.Marshal(map[string]any{"attachments": cmd.Attachments})`（`core/internal/session/record.go:59`）。
  - 修法落地后：**lint exit 0**、INFO 值位计数 **4 处 → 5 处**（枚举文案同步补"map/JSON 键位"）。
  - **ME1**（把 E 的 `[{,]` 改成不可能匹配的 `[ZZ]`，`落锤=1`）⇒ selftest 两条立刻红
    （`key.go 没被单列成 value`、`key.go 仍被判成 literal/write`），**且真树的红原样回来**：
    `attachments@…record.go` 命中 1、lint exit 1 ⇒ 我的合成件与真实案例是同一形状，不是我另编一条能过的用例。
  - **ME2**（把裸 SQL 写腿的 `DELETE` 改成 `:ZZDELETE`，`落锤=1`）⇒ **只**喊
    `DELETE FROM quota_accounts 没判成 write（键位排除顺带把 SQL 写位置也放走了）`，lint 自身仍 0
    ⇒ 排除与写腿互不掩护：同一个词在键位不红、在 SQL 写位置必须红。
  - 还原：电池超 300s 被挪到后台，我先回读盘上状态才继续 —— `[ZZ]`=0、`:ZZDELETE`=0、备份 `co5.bak` 已消失、
    `bash -n` 干净；复跑 lint 与 `--selftest` 均 **exit 0**（18:15 左右）。
  - 范围：只改 `scripts/check_ownership.sh`；`core/internal/session/record.go` 一字节未动，
    backend 也没为这条红改代码（主控 T-280① 明令禁止，T-239 禁令继续有效）。

- **09-25 18:17:04~18:23:00 FL-10 量级腿第一次尝试：116 绿 0 红，但我自己判它作废**（`.scratch/test/flaky_122_churn1_181652.log`）：
  - 起：`HEAD=7f8599f`、`core/ 未提交改动=0 处`、`lock.md5=6c379e1c`（= 主控说的入库那份端口当锁码）、指纹 `a6778fedb9a7f2cc08a56933aad3f056`；
    载荷三个循环共 spawn **34,037** 个真实进程（18:17→18:23，≈128/秒，就是 FL-10 行里写的那个量级条件）。
  - 收：指纹变成 `5baf5ef1bf2782d29957a80a28696133` ⇒ **跨版本**；窗口内 `core/cmd/core/sendpipe.go` 18:21、
    `sendpipe_test.go` 18:19 被人改写（backend 正在动 `cmd/core`）。巡检 18:00:59 量的 `git status` = 0 当时为真，
    现在不成立了（它自己也写了"你若看到更晚的 mtime 就当我没说"）。
  - 计数 **PASS=116 / FAIL=0 / SKIP=6 / BROKEN=0**，`flaky_watch exit=2`。那 6 次 SKIP 的现场是新加的那条
    `│ SKIP 现场` 打出来的：`crash_test.go:452: blocked: build cmd/core failed (backend in flight), 不拿桩绿冒充真跑`
    ⇒ 18:19~18:21 那阵 `cmd/core` 正处于别人改写到一半、编不过的窗口。**SKIP 不算样本**（FL-10/§1.1 第 19 条验收②
    补句），所以即便没有指纹那一条，这批也只有 116 个有效样本、且这 116 个跨了两版码。
  - **一次差点被我读错的信号**：后台任务通知写"exit code 0"，而电池真实 exit=2 —— 因为我的命令是
    `bash … | tail -30`，**管道末段的退出码是 `tail` 的**（记忆里那条老规矩，这次是它的第一次真实诱捕）。
    读数一律以日志里 `flaky_watch exit=` 那一行为准，通知的 exit 不算。
  - **18:34~18:47 连续三批全部作废（`.scratch/test/flaky_122_churn1_*.log` 与重试脚本输出）**，每一批都是我自己的门挡下来的：
    | 段 | 窗口 | 计数 | 为什么作废 |
    |---|---|---|---|
    | 1 | 18:34:09→18:35:51 | PASS=5 FAIL=0 SKIP=1 **BROKEN=116** | 116 次连 `---` 行都没拿到（那阵 `cmd/core` 编不过）＋ 窗口内 `internal/task/service.go` 被写 |
    | 2 | 18:35:54→18:40:18 | PASS=83 FAIL=0 SKIP=0 **BROKEN=39** | 窗口内 4 个 .go 被改写（`task/domain.go`、`task/repository.go`、`task/service.go`、`api/http/tasks.go`） |
    | 3 | 18:41:21→18:46:58 | **PASS=122 FAIL=0 SKIP=0 BROKEN=0** | 122 次全真跑全绿，**但窗口内仍有 3 个 .go 被改写** ⇒ 前后指纹 `838e55d2`→`6b36e9e3` 不一致，跨版本 |
    载荷证据这一轮**留住了**（上一批丢的那份已补：三次分别是 8,643 / 22,813 / 30,626 个真实进程 spawn）。
    ⇒ **FL-10 的量级腿今天没跑成**：缺的是一个 6 分钟没人写 .go 的窗口，不是判据松紧；三批的 0 红我一批都不交。
    已改成**分段累计**（`.scratch/test/run_flaky10b.sh`：每段 31 次、段首尾各取一次指纹，只有"前后一致且 0 SKIP/0 BROKEN"的段
    计入同一个指纹值的池子，累计到 122 才报读数）—— 理由写在脚本头：**FL-10 要的是"这 122 次跑的是同一版码"，
    没要求时间上连续**；把它写成"必须连续 6 分钟"是我自己加的约束，而那个约束今天在多人并发的树上不可满足。

    （近 3 分钟无 .go 被写才开跑；跑完前后指纹不一致或 exit 2 ⇒ 自动重来，最多 3 次、预算 40 分钟）。
    计数口径先对旧日志复算验证过（`awk` 取第 3 栏 ⇒ `116 0 6 0`，与工具自己那行 `小计：PASS=116 FAIL=0 SKIP=6` 一致），
    没用它是因为我第一版写的 `grep -c '  PASS '` 靠猜空格，数出 0 也会像读数。
- **09-25 18:26:20~18:31:56 重试第 1 次：这批**可归因**，但我不交，因为负载证据被我自己丢了**：
  `PASS=122 FAIL=0 SKIP=0 BROKEN=0`、`flaky_watch exit=0`、前后指纹**逐字相同**
  （`5baf5ef1bf2782d29957a80a28696133`）、窗口内无 .go 被改写、HEAD=`7f8599f` + `core/` 未提交 2 处（backend 的
  `sendpipe*`，已含在指纹里 ⇒ 这批对应的是"HEAD+那两处"这一版码，不是纯 HEAD）、`lock.md5=6c379e1c`、用时 5 分 36 秒（2.75s/次）。
  **可归因的那一份已经拿到，缺的是 FL-10 ①要求的"负载条件"**：我的脚本把 churn 的 spawn 计数文件
  **在打印之前就 `rm` 掉了** ⇒ 汇总行 `载荷=` 是空的。0 红 + 交不出载荷条件 = 这一条判据不满足，
  所以再跑一次而不是"凑着交"。同时给脚本加了闸：**拿不到 churn 计数就把这批判作废**（`VOID=1`），
  不是我下次再忘 —— 这条与"没有指纹的 0 红不算证据"同型，缺哪个维度就挡哪个维度。

- **09-24 11:07~11:12（本机 `date` 收尾 11:12:07）T-273 轮的最后一次重跑**（交付前最后一遍，之后只有记账）：
  - 默认面 `go test -count=1 -v ./test/`（cwd=`core/`）**exit 0**：顶层 **56 = 56（43 PASS / 13 SKIP / 0 FAIL）**、子用例 19；
    `LUCERO_TEST_FAKE_ADAPTER=1` **exit 0**：顶层 56（**48 PASS / 8 SKIP** / 0 FAIL）、子用例 31。
    **两面各 +1 PASS / −1 SKIP、顶层总数不变** ⇒ 这就是 `TestWriteEntryOpenQuestions` 从 SKIP 翻成硬断言的形状；
    工作树**两面 0 FAIL**（干净检出仍会因 db 未提交 `events.go` 红一条，见 10:02 那条）。
  - `go fmt ./test/` 改写 **0** 文件、`go vet ./test/` exit 0；五个脚本 exit 0
    （`check_ownership` 与其 `--selftest`、`flaky_watch --selftest` **14 条 = `[ok]` 14 行**、`crash_recovery`、`build_stamp`）。
  - **本轮四次变异/反向对照**（全部：备份与还原同条命令、`落锤` 先读命中数、事后回读残留 0）：
    ① `varpass_sites` 正则里方法名改坏 ⇒ 正向合成件立刻红（证明真树那个"0 处"是测量不是死腿）；
    ② 契约 §4.1 解析锚点改坏 ⇒ `t.Fatal("…解析失败…")`；③ "§4 表里必须有 session/create 那一行"改成不存在的行 ⇒ 精确 FAIL；
    ④ 一次**假阴性读数**要记：我第一次跑变异是在仓库根敲 `go test ./core/test/`（go.mod 在 `core/` 下）⇒
    屏幕整段为空，看着像"变异后照样绿"，实为"根本没跑到"。改回 `(cd core && go test …)` 才拿到 FAIL 原文。
    **同一坑我今晚栽第三次，判据升级：凡是"go test 没输出"先怀疑 cwd，再怀疑判据。**
  - 交付形：`git diff --stat HEAD -- core/test scripts` ⇒ `crash_test.go`、`adapter_contract_test.go`、
    `turn_attribution_test.go`、`check_ownership.sh`、`flaky_watch.sh` 五处；porcelain 另有 **`?? scripts/build_stamp.sh`**。
    一次性备份/探针（`act.bak`/`co4.bak`/`ocf*.sh`/`vet.log`）只落 `.scratch/test/`，`.bak` 全部当场删掉。

- **09-24 10:38~10:48:33（本机 `date`）T-270 / T-272② 这轮的取证**：
  - **计数面按 T-257③ 的规矩写**：本轮改的只有 `scripts/check_ownership.sh`（**不在 17 个 Go 包里**）。
    · **脚本面**：10:47:22→10:48:33 五个 exit 0（`check_ownership` 与其 `--selftest`、`flaky_watch --selftest` 14 条、
      `crash_recovery`、`build_stamp`）、`go vet ./test/` exit 0、`go fmt` 改写 **0** 个文件；
    · **用例计数面**：**没有新跑** —— 最后一次是 09:33:46→09:34:01 两面（默认 42 PASS/14 SKIP、旗标 47/9，
      顶层 56=56、**0 FAIL**，均**工作树口径**），而 `core/test/*.go` 最后一次改动是 **08:09:06** ⇒ 那份绿仍覆盖当前 Go 面；
      这轮我改的是 shell 与账本，所以不假称"刚跑过全包"。
  - **`ownership_conflict_note` 的两次变异**（备份/还原同条命令，`落锤` 各 = 1 先证真落上）：
    MU1 把"现读"退化成写死读不到 ⇒ **只**喊"摘要表里明明有这一行却没报出角色"；
    MU2 把旧那句"且 adapter 已据此在改"塞回输出 ⇒ **只**喊形状禁令（现状断言不许出现在判据文案里）。还原后 selftest exit 0。
    还原干净性的复核走了一次**读原文而不是数命中**：我先看到 `grep -c` 报 3 差点判"没还原"，逐行回读才确认
    3 处全在解释旧文案的注释与自测词表里，`-n ""` 实际 **0** 处。
  - **五条裁定落点的自读证据**（都是我自己跑的命令，不引转述）：契约② FL 表 FL-1~FL-10 **各一行，唯 `FL-6` 两行**
    （`grep -c '^| FL-6 '` = 2）；FL-10 行正文含"镜像以本行为准"；契约① §1.1 第 19 条含"**n = 真跑过的次数**"；
    契约① §4 有 C1 那一行（"要 JOIN 别人表的语句放消费方自己的包里"）；契约① 含 `ErrPortTakenByOther` 1 处（= T-271）。
    我地盘 `grep FL-9\|FL-10 scripts core/test` = **0 命中** ⇒ 无需改号的引用。
  - **交付形三句**：`git diff --stat HEAD -- core/test scripts` ⇒ 3 files changed, **333 insertions(+), 26 deletions(−)**；
    `git status --porcelain -- core/test scripts` ⇒ ` M core/test/crash_test.go`、` M scripts/check_ownership.sh`、
    ` M scripts/flaky_watch.sh` + **`?? scripts/build_stamp.sh`**；一次性备份/抽函数探针只落 `.scratch/test/`
    （`co3.bak` 已删，`ocf*.sh` 是本回合的 eyeball 探针，属一次性产物）。共享 `/tmp` 未写一个字节。

- **09-24 09:26~09:34（本机 `date` 收尾 09:34:01）T-257 轮的最后一遍取证**（交付前最后动作，之后只有记账）：
  - **计数面按 ③ 的规矩写**：本轮改的是 `scripts/check_ownership.sh`（**在 `scripts/` 里，不在 17 个 Go 包内**）。
    · **包计数面**：`go test ./...` 今天没跑（`core/` 里仍有别人未提交的在途码），**不拿它当现状**；
    · **用例计数面**：`./test/` 一个包两面 —— 默认 exit 0、**顶层 56 = 56（42 PASS / 14 SKIP / 0 FAIL）**、
      子用例 19（17 PASS + 2 SKIP）；`LUCERO_TEST_FAKE_ADAPTER=1` exit 0、**顶层 56（47 PASS / 9 SKIP / 0 FAIL）**、
      子用例 31（29 PASS + 2 SKIP）。**"全绿"这一格只覆盖 `core/test` 一个包**，`scripts/**` 的判据由下面四条脚本自证覆盖。
  - **C8-13 那条红的闭合读数**（不是推断）：`grep -c execution_starting specs/openapi/core-v1.yaml` = **1**（09:33:30 量，
    文件 mtime **09:24:45**）；单跑 `-run '^TestEventTypeTripleEquality$'` ⇒ **PASS**。
    09:08 那两条"1 FAIL"读数**原地保留**并在 Verification 上一节标注为"已被 08:56:18~09:24:45 这段超越"。
  - **事件名那一腿的三向对照 + 两次变异**（`--selftest` 新增 3 条，出厂实现本体被喂合成文档）：
    全登记⇒0 红、少 1 个⇒点名 `approval_resolved` 且 VIOL=1、§4 区间为空⇒下界闸响。
    M-A `n < 5`→`n < -1` ⇒ **只**喊"空区间没被拦住"；M-B 比对目标换成名字自己的来源文件 ⇒ **只**喊
    "少登记一个事件名没被点出来（VIOL=0，期望 1）"；两次 `落锤=1` 先证变异真落上，还原后残留 0、复跑 exit 0。
  - **脚本人工核对**：`bash -n` 干净；`check_ownership.sh` 真树 exit 0、其 `--selftest` exit 0；
    `flaky_watch.sh --selftest` exit 0（14 条）、`crash_recovery.sh` exit 0、`build_stamp.sh` exit 0；
    `go fmt ./test/` 改写 0 个文件、`go vet ./test/` exit 0。
  - **交付形三句**：`git diff --stat HEAD -- core/test scripts` ⇒ `crash_test.go` / `check_ownership.sh` / `flaky_watch.sh`
    三处；`git status --porcelain -- core/test scripts` ⇒ 三行 ` M ` + 一行 `?? scripts/build_stamp.sh`；
    新文件量形 `git diff --no-index --stat /dev/null scripts/build_stamp.sh` ⇒ 56 行。
    一次性备份/探针只落 `.scratch/test/`（`co2.bak` 用完即删），共享 `/tmp` 一个字节没写。

- **09-24 09:06:33~09:09:08（本机 `date`）T-254 那条"有主红"复核 —— 结论要纠正一处：红没有停在 C8-12，它移到了 C8-13**：
  - 09:06:33 单跑 `-run '^TestEventCatalogMatchesJournalConstants$'` ⇒ **PASS**；09:07:11 独立读数：
    `grep -c ExecutionStarting core/internal/eventjournal/events.go` = **2**、mtime **09:05:35**、`git status` 为 ` M `
    ⇒ **db 的 T-252 已经落地**（巡检 09:00:36 量到的是 0，那是它自己的时刻，两边都对，只是中间被人补上了）。
    09:08:03 整包里那一支也报 `C8-12：目录 64 类 / 常量 64 个；目录独有 []、代码独有 []` ⇒ 常量腿已闭合。
  - **但同一遍里有 1 条 FAIL，不是预告的那条**：`TestEventTypeTripleEquality`（C8-13）红，点名两条边、同一个类型：
    `[§6→yaml]` 与 `[常量→yaml]` —— `core-v1.yaml` 的 `EventType` 枚举里**没有 `execution_starting`**；
    计数 `§6=64 / yaml=63 / 常量=64`。**这条红不在 T-252 的范围内**（那一格要的是 Go 常量，已给），
    缺的是**第三份登记物**（yaml 枚举，T-172/T-183 定过那一格对我**只读**）⇒ **T-252 关不掉它，需要 yaml 的所有者动手**。
    独立佐证（不用我自己的解析器）：`grep -c execution_starting specs/openapi/core-v1.yaml` = **0**。
  - **我自己的一次坏探针，记下来免得别人拿它当证据**：想再数一遍 yaml 枚举时用了
    `awk '/EventType:/,/]/' … | grep -oE '^[[:space:]]*-[[:space:]]*[a-z_]+' | wc -l` ⇒ **0**——**口径错了**
    （缩进/写法与我的假设不符），这个 0 什么都不能断（"0 命中也可能是口径太窄"又应一次）；
    能用的是上面那条字面 grep 与 C8-13 自己那份带四种失败模式 fail-closed 的解析器。
  - **两面计数（09:08:03 → 09:08:19，这是本回合最后一次 Go 面重跑）**：默认面 **顶层 56 / 41 PASS / 14 SKIP / 1 FAIL**
    + 子用例 19；`LUCERO_TEST_FAKE_ADAPTER=1` **顶层 56 / 46 PASS / 9 SKIP / 1 FAIL** + 子用例 31。
    **两面那 1 条 FAIL 都是 `TestEventTypeTripleEquality`，成因如上，归主控 08:5x 那次"先契约后实现"的登记顺序**——
    不是回归、不是 flaky、我一行判据都没改（T-254 ① 的三条禁令我照守：不改判据、不改 §6、不派给别人）。
    ⇒ 本账本里 07:53~08:37 那几条"0 FAIL"的读数**都在 08:56:18 之前**，已被超越，不要当现状引用。
  - 脚本面不受这件事影响：09:00:23~09:02:21 五个脚本 exit 0（本轮 09:02 之后我没再改任何脚本）。

- **09-24 08:56~09:02（本机 `date` 收尾 09:02:21）T-241 三步的读数**：
  - **改前 / 改后跑同一棵树**（这就是 T-241 要的"别拿重跑不红当已修"的正面做法）：
    `git show HEAD:scripts/check_ownership.sh > scripts/zz_old_probe.tmp.sh`（939 行，`grep -c VALUE_SHAPES` = **0**
    ⇒ 证明确实是改前那份），跑当前还原后的 `core/api/http/sessions.go` ⇒ **exit 1** + `2 处：整个表名字符串出现在非归属包里`；
    我这版（1008 行）跑同一棵树 ⇒ **exit 0** + `另有 4 处表名字面量落在值位`。探针副本同条命令删除，`ls scripts/ | grep -c zz_old_probe` = 0。
  - **逐行表**（一次性探针脚本 `.scratch/test/probe_lines.sh`——我第一次想把四类形状塞进一条 `for` 里跑，
    里面的反引号让 bash 报 `unexpected EOF while looking for matching '"'`，**改写成文件再跑**，与记忆里
    "命令里别放反引号"是同一条坑）：`:81` 旧 RED→新 value；`:115` 旧 RED→新 value；
    `:111` 旧 read/INFO→新 value；`:108`（注释行）两边都不变。
  - **M3 = 摘掉 SQL 写腿的关键字**（280 行 `UPDATE[[:space:]]`→`ZZUPDATE[[:space:]]`，`落锤=1`）
    ⇒ selftest **exit 2**，只喊两条：`按表名扫描抓不到 task 包写 quota_accounts`、
    `语法不合法的裸 SQL 写（UPDATE quota_accounts: SET）没判成 write`；其余断言不受影响（说明这两件红件确实只靠那条腿）。
    还原后残留 0、复跑 exit 0。
  - **09:00:23~09:02:21 脚本面收尾**：`check_ownership.sh`、其 `--selftest`、`flaky_watch.sh --selftest`、
    `crash_recovery.sh`、`build_stamp.sh` **五个各 exit 0**，`bash -n` 干净。
    **脚本面为什么重跑**：M3 的还原把 `check_ownership.sh` 的 mtime 推到了 08:59:50（内容逐字节还原、
    但"最后一次动作之后没重跑"这件事不该由我来论证）⇒ 收尾这遍才是我交付的那份绿。
    Go 面仍是 08:35:46~08:36:02 那两遍（`core/test/*.go` 最后一次改动 08:09:06，之后我只动过 shell 与账本），
    两面都 **0 FAIL**、顶层 56=56。
  - **形状判据的红件/漂件断言总数**：三件必红（`w.go` 语法正确 SQL 写、`w2.go` 首参写口、`w3.go` 语法不合法写）
    + 三件必归位（`dyn.go` literal、`tag.go` tag、`sel.go` read）+ 一件必不被吞（`ret2.go`）+ 六件必不红
    （`path/cut/verb/ret/case/fold`）。

- **09-24 08:35:46~08:37:08（本机 `date`）T-236 之后的最后一次全量重跑**（交付前最后一遍，之后只有记账）：
  默认面 exit 0 **顶层 56 = 56（42 PASS / 14 SKIP / 0 FAIL）+ 子用例 19**；`LUCERO_TEST_FAKE_ADAPTER=1` exit 0
  **顶层 56（47 PASS / 9 SKIP / 0 FAIL）+ 子用例 31**；`go fmt` 改写 0 个文件、`go vet` exit 0；
  五个脚本各 exit 0（`check_ownership` 与其 `--selftest`、`flaky_watch --selftest`、`crash_recovery`、`build_stamp`）。
  Go 面重跑的理由不是多疑：08:09 那次变异对照把 `crash_test.go` 按字节还原过，**mtime 动到了 08:09:03**，
  所以"最后一次绿"必须在那之后重取一次才算数（内容相同不等于取证有效）。
  **仍未跑 18 包全量**（`core/` 里 20+ 处未提交/未跟踪、`cmd/core`/`api/http`/`internal/process` 都在被别人改写），
  这里的"全"只指 `core/test` 一个包两面。
  **交付形（REQ-T11 三句）**：`git diff --stat HEAD -- core/test scripts` ⇒ `crash_test.go`、`check_ownership.sh`、
  `flaky_watch.sh` 三处；`git status --porcelain -- core/test scripts` ⇒ 三行 ` M ` **加一行 `?? scripts/build_stamp.sh`**；
  新文件的逐字节量形 `git diff --no-index --stat /dev/null scripts/build_stamp.sh` ⇒ 56 行（08:14 那次已取，本轮未改它）。

- **09-24 08:26~08:34（本机 `date` 收尾 08:34:26）T-236 值位排除的双向变异**（备份/还原同一条命令，事后回读残留 0）：
  - 基线：`check_ownership.sh --selftest` **exit 0**；真树 `check_ownership.sh` **exit 0**（08:16:11 与 08:34 各一次）。
  - **M1 = 排除全死**（284 行的 `$VALUE_SHAPES` 改成引用一个不存在的变量；`落锤=1` 证明改动真落上）
    ⇒ selftest **exit 2**，喊出 8 条：`path.go / verb.go / ret.go / case.go / fold.go 没被单列成 value`
    加 `verb.go / ret.go / case.go / fold.go 仍被判成 literal/write` —— **就是主控 08:16 投诉的那类假红**（07:47 报的 2 处）。
  - **M2 = 排除开得太宽**（255 行 `VALUE_SHAPES=".*"`）⇒ selftest **exit 2**，喊出 3 条：
    `定义位 const tblName = "quota_accounts" 不再是 literal`、`结构体 tag 没被单列成 tag`、`裸 SQL 读的归类从 read 变了`。
    **注意它没喊 `w2.go`**，而这不是漏网：写腿排在值位之前，所以 `tx.Append("quota_accounts", …)` 在任何过宽排除下**仍然红**——
    这正是我把顺序当承重件的理由，也是一条可以复量的事实而不是一句设计偏好。
  - **我踩到的两条空断言**（先于 M1/M2 发现）：`^(literal\|write)\|` 里的 `\|` 在 ERE 中是字面竖线 ⇒
    该 grep 永不命中 ⇒ "排除没生效"那一腿是死的；改成 `^(literal|write)\|` 后 M1 才真的把它喊出来。
    第一次尝试 M1/M2 用的 sed 锚点**根本没落上**（`命中=0`），那两轮的"exit 0"是**没有变异**而不是"变异无效"。
  - 08:34 收尾：`bash -n` 干净、selftest exit 0（六个 value 合成件 + 三个反向合成件全在）、真树 lint exit 0、
    值位 INFO 计数 `2 处`。一次性备份只落 `.scratch/test/co.bak`，用完即删。

- **09-24 08:08~08:12（本机 `date` 收尾 08:12:47）T-234 那道门的两向对照 + 脚本面重跑**（这是交付前最后一次跑代码，之后只有记账）：
  - **正向对照（跨版本必须拒判）** `08:08:21 → 08:09:01`，`-r TestErrorCodeContractTablesParse -p ./test/ -m single -n 40`，
    电池进行到 45s 时往**我自己的** `core/test/crash_test.go` 末尾追加一行注释：
    **`小计：PASS=40 FAIL=0 SKIP=0 探测失败=0`（全绿）⇒ 判决仍是「探测不成立 —— 编译输入前后不一致」，exit=2**，
    并打 `★跨版本：起=3d0bf9cdd858 收=4ea87045e340`；备份与还原在同一条命令内，事后回读
    `残留对照行=0`、文件 mtime 08:09:03。**这批就是旧工具会交出去的一条假绿**（旧版会印"各 40 次都没红"）。
  - **反向对照（合法情况不许误伤）** `08:09:18 → 08:09:22`，同命令 `-n 3` 没人动码：
    起=`803e0776062c9a6d60807b60f9d9bb05` 收=`803e0776062c9a6d60807b60f9d9bb05`（**逐字相同**，不是我目测截断输出）
    ⇒ `跨版本告警行数=0`、判决照常给「未复现」、exit=0。
  - **自测与脚本人工核对**：`flaky_watch.sh --selftest` **exit 0 / `[ok]` 14 行 = 收尾自增数 14**（13→14，新那条断言
    "全绿 + 跨版本"不许被印成"未复现"）；`build_stamp.sh` 四条路径各跑过：默认包 exit 0、`--sh` 出五个变量、
    换包 `./api/http` exit 0（编译输入 2237 个、戳不同）、**假包 exit 2 且明写"拒绝给戳"**；`bash -n` 两个脚本都干净。
  - **脚本面**：`check_ownership.sh` exit 0、其 `--selftest` exit 0、`crash_recovery.sh` exit 0。
  - **Go 面（脚本改完后重跑，取代之前的 07:53 那对）** `08:10:31 → 08:10:47`：默认面 exit 0
    **顶层 56 = 56（42 PASS / 14 SKIP / 0 FAIL）+ 子用例 19**；`LUCERO_TEST_FAKE_ADAPTER=1` exit 0
    **顶层 56（47 PASS / 9 SKIP / 0 FAIL）+ 子用例 31**；`go fmt ./test/` 改写 **0** 个文件、`go vet ./test/` exit 0。
    **仍未跑 18 包全量**（`core/` 里 22 处未提交/未跟踪、其中 `cmd/core`、`api/http`、`internal/process` 都在被别人改写），
    所以这里的"全"只指 `core/test` 一个包的两面。
  - **契约正文复核**（不引用截断行）：`sed -n '406,413p' specs/contracts-internal-v0.1.md` 读完整段 ⇒
    补句原话为「一次 100 跑只有在**指纹证明确实带着修好的那份码**时才算数，**无指纹的 0 红不是证据**」，
    且第 19 条验收② 明写"两问都要在，缺一不可"（FL-8 那一档不被本格取代）。
  - **交付形三句（REQ-T11，08:14:16）**：`git diff --stat HEAD -- core/test scripts` ⇒ `crash_test.go +69` /
    `flaky_watch.sh +128`；`git status --porcelain -- core/test scripts` ⇒ 两行 ` M ` **加一行 `?? scripts/build_stamp.sh`**
    ——新交付的是一个**未跟踪**文件，只看 diff 就交了会把它整件漏掉（这正是那三句存在的理由）；
    `git diff --no-index --stat /dev/null scripts/build_stamp.sh` ⇒ **56 行**，逐字节量形在此。
    两份对照电池的输出留在 `.scratch/test/{xver,clean}.log`（我一开始把 xver/clean 落在了 `.scratch/` 根上，
    08:14 已挪进自己的角色目录 —— 一次性产物不许散在共享位置）。

- **09-24 07:18~07:49（本机 `date` 收尾 07:58:30）FL-9（现 FL-10）的四批采样 + 一次真 SKIP 端到端**（原始输出全部归档，文件名带判决）：
  - `07:18:26 → 07:18:30` 命令行形状冒烟（`-n 1`）⇒ `single PASS 包退出码=0`、exit 0。**这条先跑是为了不拿 5 分钟去撞一个拼错的旗标**。
  - `07:20:10 → 07:25:01` **122 次：PASS=122 / FAIL=0 / SKIP=0 / 探测失败=0**，用时 291s（2.39s/次）。
    **判作废**：那台二进制编译进了未提交的在途码（`core/internal/process/lifecycle.go` 是 `??`、07:17 落盘），
    且窗口内 07:21:17 又写了 `lifecycle_test.go`。作废只作废一件事——**它不能当任何一版的验收读数**；
    "未修的 `lock.go`（当时 md5 `dfd84639`，最后一次提交 `b91c537` 09-20 12:34）下串行 122 次可以一次不红"
    这一条我仍然记着，因为它就是"量级腿单独不关 REQ"的直接证据，只是**不发布成结论**。
  - `07:31:42 → 07:36:28` 122 次：**`小计：PASS=113 FAIL=0 SKIP=9 探测失败=0`**，而同一份归档的判决那一句印的是
    「**判决：未复现 —— 实跑档 [single] 各 122 次都没红**」——**两句并存，就是旧工具自相矛盾的现场**
    （`flaky122-0731-void-9skip-fingerprint-changed.txt`）。9 次 SKIP 挤在 07:33:45~07:33:55、每次约 1.1s
    （远短于正常 2.4s ⇒ 那几次没起子进程），与 backend 07:32 改写 `cmd/core/{core_test.go,lock_test.go}` 重合
    ⇒ 判为 `buildCore` 那格的 `t.Skipf`（用例设计如此，不是缺陷）。**同样判作废**：编译指纹
    `0ad0b4f8…` → `3095b929…` 前后不一致（窗口内 4 个 .go 被改写）。
  - `07:36:31 → 07:38:52` **带 pid 流失载荷 40 次：PASS=39 / FAIL=1 / SKIP=0**，指纹前后一致
    （`3095b929…`）⇒ **这批可归因**；被验的 `lock.go` md5=`16e09a92`（**已经是端口当锁那一版**）。
    载荷：三个循环共 spawn **18,178** 个真实 PE 进程（≈129 个/秒）。**这一次的红没有留下现场**——
    我的"红/SKIP 自带 stderr"修法在 8 分钟后才落地，所以那个红的机制我**不知道**，也不猜。
  - `07:46:39 → 07:49:24` 同样载荷 **60 次：PASS=60 / FAIL=0**，指纹前后一致（`2519588…`），`lock.go` 仍是 `16e09a92`，
    spawn **20,890**（≈127/秒）。⇒ 新锁代码上两批合计 **1/99 ≈ 1%**（不叫"发生率"，两批载荷相同但中间隔了别人的改动）。
  - `07:45:43 → 07:45:46` **真 SKIP 的端到端**（新代码路径要在真实采样上验，不只在合成计数上验）：
    拿今天恒 SKIP 的 `TestCoreReadyEventIsJournalized` 跑 `-m single -n 2` ⇒
    两次都印出 `│ SKIP 现场 … crash_test.go:509: blocked(REQ-T7 给 backend)…`，判决
    「**探测不成立 —— 有 2 次采样返回 SKIP**…这批 **2** 次里有效样本只有 **0** 个」，**exit=2**
    （这里的 2/0 是我**故意换了另一个 n** 采的：写死 122 的那版我上一分钟刚修掉，换个数才能证明它真的跟着 `n` 走）。
- **09-24 07:53:29~07:54:33（本机 `date` 07:55:52 收尾）T-223 轮的最后一遍全量重跑**（交付前最后一次，之后的动作只有记账）：
  - 默认面 `go test -count=1 -v -timeout 300s ./test/`（cwd = `core/`）⇒ **exit 0**，
    **顶层 `=== RUN` 56 = 顶层结果 56（42 PASS + 14 SKIP + 0 FAIL）**，子用例 19（17 PASS + 2 SKIP + 0 FAIL）。
    顶层比上一轮 +1 = 本轮新增的 `TestSecondBootShapeClassifierOnlySaysWhatItHeard`。
    计数分型写清：`^=== RUN   Test[^/]*$` 数顶层、`^=== RUN   Test[^/]*/` 数子用例 ——
    我第一版用 `^=== RUN   Test` 前缀匹配，把 19 条子用例也算进顶层（报出 75，与结果数 56 对不上才发现）。
  - 开 `LUCERO_TEST_FAKE_ADAPTER=1` 同一包 ⇒ **exit 0**，顶层 56（**47 PASS + 9 SKIP + 0 FAIL**）、子用例 31（29 PASS + 2 SKIP + 0 FAIL）。
  - `D:/DevTools/go/bin/go.exe vet ./test/` exit 0；`go fmt ./test/` **改写 0 个文件**（计数打成数字，不靠 `&& echo clean`）。
  - `check_ownership.sh` exit 0、`check_ownership.sh --selftest` exit 0、`crash_recovery.sh` exit 0、
    `flaky_watch.sh --selftest` **exit 0 / `[ok]` 13 行 = 收尾自增数 13**。
  - **这一轮我没有跑 `go test ./...`（18 包全量）**，理由不是省事：当时 `core/` 下有 7 处未提交改动、
    3 个未跟踪文件（`cmd/core/lock.go`、`main.go`、`core_test.go`、`internal/process/lifecycle.go`、
    `api/http/sessions.go`……全是别人的在途码），拿整仓的读数什么都证不了。
    **所以"全"这一轮只指 `core/test` 一个包的两面**，别当 18 包。
  - 采样批的原始输出已归档为**判决名**（文件名里带着它该被怎样引用）：
    `flaky122-0720-void-untracked-inflight-build.txt`（122 绿 / **作废**）、
    `flaky122-0731-void-9skip-fingerprint-changed.txt`（113 绿 + 9 SKIP / **作废**）、
    `flaky40-churn-0736-admissible-1red-newlock.txt`（39 绿 1 红 / 可归因、**无现场**）、
    `flaky60-churn-0746-admissible-0red-newlock.txt`（60 绿 / 可归因）。
    外加 `selftest-final.txt`、`ownership-selftest-final.txt`。取证脚本与 `.bak` 都落在 `.scratch/test/` 内，
    变异备份与还原在同一条命令里完成，事后逐项回读为 0 残留。
  - **交付形**：`git diff --stat HEAD -- core/test scripts` ⇒ `core/test/crash_test.go`、`scripts/flaky_watch.sh`
    两处（本轮改的正是这两个）；`git status --porcelain -- core/test scripts` ⇒ **只有这两行 ` M `、零 `??`**
    （我没在我的范围里留新文件，也没在别人包里落码）。

- **09-23 05:51:47~05:52:18（本机 `date`）T-216 工具改动落地后的重跑**：
  `flaky_watch.sh --selftest` **exit 0，收尾条数由自增算出、与实际 `[ok]` 行数一致 = 9**（原 5 句 + 稳定复现 + 间歇 + 不误标稳定复现 + 只跑一档也有判决）；我先写「11 句」是手数的、已改；脚本收尾那句也从写死的「五句」改成自增（05:53 它还写着五句，实际已 9 条）；
  `bash -n` 干净；`-m single -n 2` 实跑 ⇒ 屏幕上**没有另外三档的任何行**（grep 计数 0）、
  判决为 `REGRESSION（间歇，single 档 1/2 ≈ 50%）`（exit 1 = 分类为回归族，与旧版那句"稳定复现"已分开）；
  `-run` 守卫两条 exit=2。脚本 `mtime` 与 `git status`：本轮只动 `scripts/flaky_watch.sh` 一个文件（Go 侧零改动）。
  注：`-m single -n 6`（05:45:19）那趟是**修 `MODES` 默认值之前**跑的，当时四档混跑、判决文案还是旧的
  "这是稳定复现"——那一句被本条取代，保留在此只为留下"我说过假话"的现场（它正是 REQ-T18 要防的形状）。
- **09-23 05:1x 就地更正本文件 00:0x 那条处置（历史不静默改）**：那条把 「TestCrashRestartSameDataDir 的红」记成「撞上游在途改写，既不计 flaky 也不计回归，请 backend 交复采」。
  今天同一形状自己复现且带被验对象的自述（第二代 stderr：「已有 Core 在跑同一个数据目录」）⇒
  当时的 FL-8 程序没错（窗口确实脏），但**结论下软了**：我把「不算证据」当成终点，而子进程 stderr 一直被我丢进
  `io.Discard`——**没取证不等于有证据说没事**。现改记：**产品侧间歇**——单跑 63 次红 3 次、整包 2 趟红 1 次 ⇒ **4/65 ≈ 6%**，
  机制候选在 `cmd/core/lock.go:66` 的 `os.FindProcess(pid)` 存活判定，归 backend；端口那条替代解释已被
  55 次单跑否掉（换代时同端口发还 0 次）。详见 Completed 05:0x 与 REQ-T17。
- **09-23 05:10:45~05:11:18（本机 `date`）形状分类器上线后的采样与回归**：
  单跑第 3 次抓到红（05:11:18），红话自带 `>>> 形状判定：第二代进程拒绝起锁…`；
  本轮该用例**单跑 63 次（8+12+20+20+3）里红 3 次**，另有整包 2 趟里红 1 次 ⇒ **4/65 ≈ 6%**；
  `gofmt -l` 空、`go vet ./test/` 空。
- **09-23 03:23:25~03:24:39（本机 `date`）C8-14 落地后的最后一次重跑（所有编辑之后）**：
  默认面 `./test/...` ⇒ 顶层 `=== RUN` **55** = 顶层结果 **55** = 声明 **55**（+1 = C8-14），
  **41 PASS / 14 SKIP / 0 FAIL**；开旗标 ⇒ **46 PASS / 9 SKIP / 0 FAIL**；
  `go test -timeout 240s ./...` ⇒ **18/18 包 ok**（`cmd/core` 那条 02:43 的挂没复现，见 02:4x 条目）；
  lint / `--selftest` / `flaky_watch --selftest` / `crash_recovery` 四条各 **exit 0**。
  C8-14 单条命令照契约② 那格原文：`go test -count=1 -v -run '^TestAdapterCodeMapsToSingleHTTPName$' ./test/`
  ⇒ `RUN=1 PASS=1 FAIL=0`。基线 `070d242`（03:14:45）。
- **09-23 02:42:40~02:55:57（本机 `date`）T-183 三件落地后的重跑**：
  我自己那两面不变：**默认 40 PASS / 14 SKIP / 0 FAIL、开旗标 45 / 9 / 0**，顶层 `=== RUN` 54 = 结果 54 = 声明 54；
  `lint` / `lint --selftest` / `flaky_watch --selftest` / `crash_recovery` **各 exit 0**（02:54:15 那趟的 `lint=0 selftest=0 flaky=0 crash=0`）。
  **两个计数面分写**：`go test ./...` 02:11:57 那次 **18/18 ok**；02:43 那次 **17/18、`cmd/core` 因
  `TestProbeStaysUnavailableWithoutAdapters` 看门狗超时 FAIL（600.264s）** —— 处置与不判因果的理由见 Completed 02:4x 最后一条。
  **随后 02:56:53 单包复采 `cmd/core` `ok 2.606s`、02:57:43 起 `go test -timeout 240s ./...` 回到 18/18 ok**
  （文件仍 `??`，所以两头都不下结论：不叫"是 bug"，也不叫"已修"）。
  门的分支改口原始行（02:40:32，旗标开）：
  `前提①已满足（找到调用点 "Rec: newDropRecorder(l.store, adapterKey, l.now, l.logf),"（共 1 处）），但仍不测：第②条不成立…`
  ⇒ `--- SKIP: …/T84-7b`、`--- PASS: …/T84-7a`。
  两条"现盘量过才敢写"的数：`eventjournal` 常量 **原始 63 / 去重 63 / 重复 0**；
  §15.2『来自』列 **17 行 / 去重 17 / `uniq -d` 空**（⇒ 今天没有一对多的实例，双射化今日转红零误伤）。
  时间线档（02:55:57 跑 `-a 02:43:19 -s 300`）：扫 147 个 .go、窗口内 2 个文件（都是我自己的）⇒ 观察不算证据。
  证形（02:54:41）：基线 `75f06d0`（02:35:28），`git status --porcelain -- core/test scripts` ⇒
  `errcode_mapping_test.go`、`turn_attribution_test.go`、三个脚本共 **5 行 ` M`、无 `??`**；
  `LAST_UPDATED` 已改为当场 `date -Iseconds` = `2026-09-23T02:42:27+08:00`。
- **09-23 02:11:57~02:13:16（本机 `date`）三格同型全部落地后的最后一次重跑**：默认面 `./test/...` ⇒
  顶层 `=== RUN` **54** = 顶层结果 **54** = 声明 **54**，**40 PASS / 14 SKIP / 0 FAIL**；
  开旗标 ⇒ **45 PASS / 9 SKIP / 0 FAIL**；`go test ./...` ⇒ **18/18 包 ok**；
  lint / `--selftest` / `flaky_watch --selftest` / `crash_recovery` 四条各 **exit 0**。
  四条 C8 用例的原始行（同一棵树 02:11:41）：
  `计数对（原始/去重）：§15.2 左列 30/30（重复即红）、『来自』17/17（重复合法，仅打印不咬人）`、
  `C8-11：扫 5 个生产 .go、15 个字面码…非字面量传码 0 处；字面码 原始/去重 = …`、
  `C8-12：目录 63 类 / 常量 63 个…`、`C8-13 三格相等：§6=63、yaml=63、常量=63；四条边全空`。
  反向验（`firstDup` 阈值推到 `> 9`，02:11:41~02:11:44）⇒ **三条腿同时喊**（§15.1 码列重名、
  §15.2 左列重名、两个常量同值），"一对多合法"那一组仍绿；还原 `MUT-DUP3` 残留 0、`gofmt` 空。
  同一时刻别人的在途面（只报不判）：`core/api/http/{handler,projects}.go`、`internal/project/service.go`
  是 ` M`，另有 `?? projects_get_test.go`、`?? projects_patch_test.go` ⇒ backend 正在接 GET/PATCH。
  **本轮另记一次自我损坏与恢复**：我把 T-178 的四组子项插错了条目（锚点选了"下一条目的头"，
  等于插到了上一条目尾部）。发现靠的是 `grep -n` 看**标题出现顺序**而不是只看总数；
  修复用行角色定位的 python 整块移动（三把 assert：块必须在 02:0x 头与 01:40 头之间、
  块内每行都是子项、移动后 `^## ` 仍为 11）。**这正是我自己写过的"插入要插在目标条目末尾、
  锚点不能是下一条目开头"——第四次同族，见血泪账。**
- **09-23 02:06:21~02:07:35（本机 `date`）T-178 三条腿落地后的最后一次重跑（所有编辑之后）**：
  默认面 `./test/...` ⇒ 顶层 `=== RUN` **54** = 顶层结果 **54** = 声明 **54**，**40 PASS / 14 SKIP / 0 FAIL**；
  开旗标 ⇒ **45 PASS / 9 SKIP / 0 FAIL**；`go test ./...` ⇒ **18/18 包 ok**；
  `check_ownership.sh` 与其 `--selftest` 各 exit 0；`gofmt -l test/` 空、`go vet ./test/` 空。
  两条新腿的原始行（同一棵树，02:05:43 / 02:06:03）：
  `计数对（原始/去重）：§15.2 左列 30/30（重复即红）、『来自』17/17（重复合法，仅打印不咬人）`；
  `C8-13 三格相等：§6=63 类、yaml=63 条、常量=63 个（扫 1 个非测试 .go）；四条边全空`；
  反向验（把 `firstDup` 阈值推到 `> 9`）⇒ `--- FAIL: TestErrorCodeContractTablesParse` 加两条
  `第二条腿失效：… 本该红却解析成功（named=[TIMEOUT]）`，而"一对多合法"那一组**仍然绿**（过严守卫成立）。
  还原校验：`MUT-DUP` 残留 0、`keep-split`/`k3` 备份已删、1056 行、复跑 `ok`。
  本轮把 §15.1/§15.2 的行解析拆成纯函数 `parseCodeRows`（为的是让合成反例喂得进解析器），
  拆分前后四条形同 C8 的用例数字完全一致（17/30/13、15、63/63/63）⇒ 重构没改判决。
- **09-23 01:47:34~01:48:58（本机 `date`）C8-13 落地后的最后一次重跑（所有编辑之后）**：
  默认面 `./test/...` ⇒ 顶层 `=== RUN` **54** = 顶层结果 **54** = 声明 **54**（54=53+C8-13 一条），
  **40 PASS / 14 SKIP / 0 FAIL**；开旗标 ⇒ **45 PASS / 9 SKIP / 0 FAIL**；`go test ./...` ⇒ **18/18 包 ok**；
  `check_ownership.sh`、`--selftest`、`flaky_watch --selftest`、`crash_recovery` 四条各 **exit 0**；
  `gofmt -l test/` 空、`go vet ./test/` 空。C8-13 单条命令：
  `go test -count=1 -v -run '^TestEventTypeTripleEquality$' ./test/` ⇒ `RUN=1 PASS=1 FAIL=0`，
  原始行 `C8-13 三格相等：§6=63 类、yaml=63 条、常量=63 个（扫 1 个非测试 .go）；四条边全空`。
  变异残留反查：`grep -rn 'case.yaml|c813|a.yaml' core/test/*.go` = **0**；
  `.scratch/test/` 下的合成件与脚本已删；真 `specs/openapi/core-v1.yaml` 未改
  （`git status --porcelain -- specs/` 空，取样 01:45:32）。
- **09-23 01:29:37~01:30:57（本机 `date`）T-170 结案复采**：默认面 `./test/...` ⇒ 顶层 `=== RUN` 53 =
  结果 53 = 声明 53，**39 PASS / 14 SKIP / 0 FAIL**；开旗标 **44 / 9 / 0**；`go test ./...` **18/18 ok**；
  `check_ownership.sh` **exit=0**、`--selftest` **exit=0**（含新加的 `[W13 注释自证·成对]` 打印行）、
  `flaky_watch --selftest` exit 0、`crash_recovery` exit 0。
  证形：`git status --porcelain -- core/test scripts` ⇒ 只有 `check_ownership.sh`、`crash_recovery.sh`、
  `flaky_watch.sh` 三行 ` M`、**无 `??`**；本轮 Go 侧零改动（改动全在脚本 + 账本），
  所以 01:23 与 01:30 两次 Go 数字一致（53/39/14/0）不是巧合而是同一棵树。
  新增证据文件：`.qoder/evidence/test/w13-head-dropaudit-pair.txt`
  （原始命中 `18 21 66` → 出厂 `w13_hits` 之后只剩 `:66`，取样 01:28:01、HEAD=d814572）。
- **09-23 01:23:0x~01:23:5x（本机 `date`）T-166 结案复采（树在 01:19 被别人改动过，所以重测一遍而不是沿用 01:17）**：
  默认面 `./test/...` ⇒ 顶层 `=== RUN` 53 = 结果 53 = 声明 53，**39 PASS / 14 SKIP / 0 FAIL**；
  开旗标 ⇒ **44 / 9 / 0**；`check_ownership.sh` **exit=0、VIOLATION 0 条**；`--selftest`、
  `flaky_watch --selftest`、`crash_recovery` 各 exit 0；账本 11 个标题。
  **绿的来历要说清，别让它读起来像"test 把 lint 修绿了"**：越界本身被 backend 从树里拿掉了
  （`RecordStart` 改成 `logf("REQ-DB-2 未落：生命周期那格没建行 …")`，01:19:52），
  而**不是**我把那条腿弄哑——三条反查证据见上一条 Completed。判据面仍写着
  `[W13-2] process_instances: 0`、`命中 3 处：白名单 3 / 表越界 0`。
  同一时刻 `go test ./cmd/core/` ok（2.555s）、`go test ./...` 18/18 ok ⇒ 他们那一改自洽。
  **checkout `HEAD=d814572` 跑我的 lint 会红**（那一版 `:66` 还在）——这是预期，不是我引用了旧事实。
- **09-23 01:17:06~01:18:15（本机 `date`）T-166 + T84-7b 门那轮的最后一次重跑**：
  默认面 `./test/...` ⇒ `=== RUN` 顶层 53 = 顶层结果 53 = 声明 53，**39 PASS / 14 SKIP / 0 FAIL**；
  开旗标 ⇒ **44 PASS / 9 SKIP / 0 FAIL**；`go test ./...` ⇒ **18/18 包 ok**（`core` 这一个 Go 模块，
  01:01 那次 `cmd/core` 的 FAIL 到 01:11 已自行消失，是 backend 在途编辑，不是我要判的东西）；
  `flaky_watch --selftest`、`crash_recovery` 各 exit 0。
  **`check_ownership.sh` 这轮 exit=1、3 条 VIOLATION，全部只关于 `Append("process_instances")`
  （`cmd/core/dropaudit.go:66`，T-168/backend）——这条红是判据该有的行为，不是我的缺陷**：
  01:00 之前它是"两条真 + 一条假"，假的那条（`:18` 注释里复述本规则）已修；
  **如果它现在变绿，才说明我把真信号弄哑了**。逐条原文与判决在
  `.qoder/evidence/test/ownership-lint-final.txt`；恢复版注释的复证在
  `.qoder/evidence/test/w13-restored-dropaudit.txt`（原始 3 命中 → 剥注释后只剩 `:66` 一处）。
  反向验：selftest 里那条 `wn == 2` 在把 strip 管道换回 `cat` 后当场变 rc=2 并喊出两条精确 FAIL（01:11:12），
  还原后 rc=0、`MUT` 残留 0。
  证形三句（01:18:15 复采）：基线 `d814572`（01:13:44）；
  `git diff --stat HEAD -- core/test scripts` ⇒ `check_ownership.sh | 19 ++++++++++--`、
  `crash_recovery.sh | 5 ++++-`、`flaky_watch.sh | 14 ++++--`，合计 `3 files changed, 31 insertions(+), 7 deletions(-)`；
  `git status --porcelain -- core/test scripts` ⇒ 只有那三行 ` M`、**无 `??`**
  （本轮的 Go 改动已被主控随 `753900f` 入册，故第三命令 `--no-index` 不适用）。
- **09-23 00:07:28~00:08:27（本机 `date`）③b 那轮交付前最后一次重跑**，四条全部 exit 0：
  默认面 `./test/...` ⇒ `=== RUN` 顶层 53 = 顶层结果 53 = 声明 53，**39 PASS / 14 SKIP / 0 FAIL**；
  开旗标 ⇒ **44 PASS / 9 SKIP / 0 FAIL**；`go test ./...` ⇒ **18/18 包 ok、0 FAIL、0 "no test files"**
  （"全"= `core` 这一个 Go 模块的 18 个包，`apps/desktop` 与仓库根脚本不在里面）；lint exit 0。
  **同一条记录里必须写的上一次红**：00:03:18 那一趟默认面 `TestCrashRestartSameDataDir` **FAIL（26.42s，
  烧完 25s 预算）**，红句是"硬杀后同目录重启 25s 内没有换新 `core.endpoint` —— 残留锁没被自动回收"。
  我没有当场记 flaky、也没有当场记回归，做了三件事：
  ①**FL 四档 × 3**（00:05:33~00:06:24）⇒ 12/12 PASS、窗口内零改写 ⇒ 判决"未复现（不等于已绿）"；
  ②**时间线档** `-a 2026-09-23T00:03:18+08:00 -s 240` ⇒ 扫 139 个 .go、**6 个落在改写窗口里**：
  `core/api/http/projects.go`(前 133s)、`core/api/http/projects_test.go`(前 218s)、
  `internal/project/service.go`(前 151s)、`internal/eventjournal/contract_parity_test.go`(前 110s)、
  `core/test/errcode_mapping_test.go`(前 51s，我自己的 ③b 还原)、
  以及 **`cmd/core/core_test.go` 在失败之后 7 秒被改写** ⇒ 按 FL-8 的口径**这次观察不算证据**；
  ③**FL 四档看不到的那一维**（`-run` 过滤会跳过同包前置用例）：`go test -count=2 ./test/` 不过滤、
  旗标关闭、与红那次同一配置 ⇒ 00:07:02 **78 PASS / 28 SKIP / 0 FAIL**。
  **结论与请求**：这条记"撞上游在途改写，既不计 flaky 也不计回归"；要留档需要 **backend 就
  00:01~00:03:25 那段 `cmd/core` + `api/http/projects.go` 改动交一次带时刻的复采**
  （残留锁自动接管在 `cmd/core` 自己那侧有 `TestStaleLockIsTakenOver` 盯着，不是我这条的替代物）。
  **另记自己一条工具教训**：第一次给 ③b 守门做探针我把分支写成 `case false:` / `if false {`，
  `sawShort` 变只写不读 ⇒ **包编不过**，而我 grep 只兜 `^--- ` ⇒ 屏幕零输出，差点当成"探针没反应"。
  关掉分支要用 `> 99` / `&& false` 这类**仍读取该变量**的写法，且探针输出必须同时兜
  `build failed` / `declared and not used`。
- **09-22 23:34:19~23:35:0x（本机 `date`）T-135 交付前最后一次重跑**，六条命令全部 exit 0：
  `go test -count=1 -v ./test/...` ⇒ **53 顶层 RUN / 53 顶层结果 / 39 PASS / 14 顶层 SKIP / 0 FAIL**；
  开 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **44 PASS / 9 顶层 SKIP / 0 FAIL**（把子用例的 `--- ` 行一起数：
  69 PASS / 11 SKIP / 0 FAIL）；`check_ownership.sh` exit 0（第十二轮）、`--selftest` exit 0、
  `flaky_watch.sh --selftest` exit 0、`crash_recovery.sh` exit 0。
  **对账（T-124 规矩 ①）**：`grep -h '^func Test' core/test/*_test.go | wc -l` = **53**
  = 顶层 `=== RUN` 53 = 顶层 `--- ` 结果行 53 ⇒ 没有"声明了却没跑"。原始输出：
  `.qoder/evidence/test/{go-test,go-test-fakeadapter,ownership-lint,selftest,crash,flaky-watch-selftest,c8-mutation-probe}-final.txt`。
  本轮五条新腿的原始行（23:19:03 与 23:32:40 各跑过一次，结论一致）：
  `形状自证：§15.1=17 码、§15.2=30 行（码名行 17 / — 13，去重码名 17），双向相等`；
  `C8-11：扫 5 个生产 .go、15 个字面码全部在 §15.2 的 30 个码里；非字面量传码 0 处`；
  `C8-12：目录 62 类 / 常量 62 个（扫 1 个非测试 .go）；目录独有 []、代码独有 []`；
  `反向也过：§15.1 的 17 个码都有 HTTP 映射行；— 开头的 13 行按形状不参与，不靠点名`；
  `T84-7a：2 条丢弃记录（method=[turn/claimed turn/dispatched]），帧内 id="exec-t847-evil" / 票号绑定 id="exec-t847-bound" 两个都在；生命周期三方法被调 1 次（只留痕不判）`。
  **C8-12 今天不红的原因要写清楚**（免得下一轮误以为判据钝）：db 在 `5d6ac66`（23:10）补齐了
  `NotificationRead` 与 `task_paused`/`task_blocked`/`task_resumed`/`execution_paused`/`execution_resumed`，
  两侧同时到位 ⇒ 双向差集自然为空；**它咬人是由 M-F/M-G/M-H 三例当场证明的**，不是由"今天恰好相等"证明的。
  **一次撞见别人在途改写（按 T-121 先对时间线，没记 flaky）**：23:33:22 那次 `./test/...` 里
  `TestCrashKillCoreAndRestart`/`TestCrashRestartSameDataDir` 从 PASS 变 SKIP，用例自己打出的原因是
  `blocked: build cmd/core failed (backend in flight): cmd\core\main.go:222:13: undefined: session`
  —— 同一分钟 `go build ./cmd/core` 已经编得过（23:34:06 exit 0），23:34:19 重跑两条恢复 PASS。
  这两条本来就是"编不出真 Core 就显式 SKIP、不拿桩绿冒充"的设计，所以这是**别人在写、不是 flaky**。
  证形三句（T-72，**23:36:29 复采**，与上面那批重跑之间只有账本自己的编辑、代码零改动）：
  基线 `git log -1 --format=%h` = `a1b8664`（23:31:30，主控的快照提交）；
  `git diff --stat HEAD -- core/test scripts` ⇒
  `core/test/errcode_mapping_test.go | 624 +++++...--`、`core/test/turn_attribution_test.go | 141 ++++++-`、
  `scripts/check_ownership.sh | 2 +-`，合计 `3 files changed, 689 insertions(+), 78 deletions(-)`；
  `git status --porcelain -- core/test scripts` ⇒ 只有这三行 ` M`、**没有 `??`**
  （`errcode_mapping_test.go` 已在 `7a48a45` 之后入册，故 `--no-index` 那条第三命令本轮不适用）。
  **T-132 修好之后补的一档（巡检 23:00 那条投递到得比交付晚，但它带的新事实有用：全仓不再挂死）**：
  `go test ./...` 于 23:40:24~23:40:31 **exit 0、18 个包全 ok、无 `panic: test timed out`**
  （原始输出 `.qoder/evidence/test/full-module-final.txt`）—— 我此前为避免挂死而把验收限制在 `./test/...`，
  这条限制从今天起不再必要。既然全仓面可用，就用 FL 的四档跑了一次真跨包并发（挑最吃共享面的
  `TestCrashKillCoreAndRestart`，peers = `./cmd/core/ ./api/http/ ./internal/audit/ ./internal/process/`）：
  23:41:01~23:41:54 四档各 3 次 ⇒ **12/12 全 PASS，判决「未复现」**（分类器按 FL 的口径拒绝把它写成"已绿"），
  时间线窗口内相关文件零改写。这一跑还暴露我自己工具的一处过头措辞（见 Changed files 的
  `flaky_watch.sh` 条），已整改并复跑 `--selftest` exit 0。
  **巡检 23:22 那条 T-141 投递到得比我的实现晚（我 23:23 从账本读到原文就照做了），但它转的 db 陷阱②
  值得从"我碰巧没踩"变成"判据当场看得见"**：C8-12 里加了 5 个**只能由合并行展开得到**的名字
  （`notification_read`/`execution_failed`/`process_crashed`/`approval_resolved`/`handoff_ready`）
  做成员断言 —— 按字面名 grep §6 的那类解析器会在这里直接红，而不是靠"62 恰好等于 62"推断。
  探针 M-N（23:46:40，把 `handoff_ready` 换成盘上没有的名字）当场 FIRED，
  还原后 `grep -c handoff_zzz` = 0、`gofmt -l` 干净、复跑 ok（备份与还原在同一条命令里，见血泪账第 16 条）。
  **db 那句提醒我照记不转述**：`POST /notifications/{id}:read` 仍 501（`notifications_test.go:253` 正钉着），
  事件类型备好 ≠ 端点已通；我的 C8-12 只判词表闭合，不判那条端点的可用性，别把它当后者引用。
  变异判决的原文（12 例 + T84-7a 的 2 例）落在 `.qoder/evidence/test/c8-mutation-probe-final.txt`，
  末行是 `探针结束，期望不符 0 例`。
- **09-22 15:44:16~15:45:28（本机 `date -Iseconds`）T-131 交付前最后一次重跑**，六条命令全部 exit 0：
  `go test -count=1 -v ./test/...` ⇒ 0 FAIL；开 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **65 PASS / 9 顶层 SKIP / 0 FAIL**；
  `check_ownership.sh` exit 0（第十一轮）、`--selftest` exit 0、`crash_recovery.sh` exit 0、
  `flaky_watch.sh --selftest` exit 0（六句判决各就各位）。
  **新增的一条对账（T-124 规矩 ①）**：`grep -hc '^func Test' core/test/*.go` 求和 = **50**，
  `-v` 输出里顶层 `--- ` 行数 = **50** ⇒ 没有用例声明了却没跑。
  基线 `4f7fb43`；证形三句：`git diff --stat HEAD -- core/test scripts` ⇒ `1 file changed, 7(+), 18(-)`
  （T84 指针翻转），`git status --porcelain` ⇒ 另有一行 `?? core/test/errcode_mapping_test.go`，
  `git diff --no-index --stat /dev/null core/test/errcode_mapping_test.go` ⇒ `236 insertions(+)`（我先写了 195，复测才发现自己又犯了"计数没配原文"，改回来了）。
- **C8-6/7/8 三条判据的实测原文（15:3x，`-v`）**：
  `解析器自证：§15.1=17 码、§15.2=29 行（码名 17 / `—` 12）`；
  `§15.2 的 17 个码名逐个在 §15.1 里找到了`；`反向也过：§15.1 的 17 个码都有 HTTP 映射行；带 — 的那 12 行按形状不参与，不靠点名`。
  **守门非永真的证据不是变异，是我自己第一次跑错**：`来自` 列我按 awk 习惯取了 `cells[4]`（split 后应为 `cells[3]`），
  解析器当场报 `§15.2 第 1 行的『来自』既不是码名也不是 `—` 开头："" ⇒ 解析形状变了`，三条全红。
  这类"表格解析器静默读空"正是主控 09-22 让我自带守门的原因，实测它确实会拦。
- **今天我自己撞到的"包 ok 不算跑过"（新规矩的来源）**：我 `-run` 里把
  `TestHTTPErrorCodesComeFromAdapterSet` 误打成 `...Adapterset` ⇒ 那条整条没跑、包照样 `ok`；
  按 `=== RUN` 原文行数对账才发现。教训并入"计数必须配原文行"同一节。
- **09-21 16:14:06~16:15:23（本机 `date -Iseconds`）T-121/T-124 交付前最后一次重跑**：六条 exit 0（默认面 46 PASS / 14 顶层 SKIP / 0 FAIL、开旗标 62 / 9 / 0、lint 第十轮 exit 0、selftest、crash、flaky_watch 两档全 0）。
  `-a` 档全仓扫实测：`扫描范围=../core ../specs ../scripts`、`扫了 122 个 .go 文件`。

- **09-21 16:02:08~16:03:14（本机 `date -Iseconds`）T-119 交付前最后一次重跑**，六条命令全部 exit 0：
  默认面 `go test -count=1 -v ./test/...` **46 PASS / 14 顶层 SKIP / 0 FAIL**、开
  `LUCERO_TEST_FAKE_ADAPTER=1` **62 PASS / 9 SKIP / 0 FAIL**、`check_ownership.sh` **exit 0（第九轮）**、
  `check_ownership.sh --selftest` exit 0、`crash_recovery.sh` exit 0、
  **新**的 `flaky_watch.sh --selftest` exit 0（六句判决各就各位）。基线现取 `1ed70c1`。
  证形三句：`git diff --stat HEAD -- core/test scripts` ⇒ 2 个已跟踪文件（两处判据表指针）；
  `git status --porcelain` ⇒ 一行 `?? scripts/flaky_watch.sh`；
  `git diff --no-index --stat /dev/null scripts/flaky_watch.sh` ⇒ `1 file changed, 173 insertions(+)`。
- **`flaky_watch.sh` 对本次间歇红的采样（判决=未复现，按 FL-5 不写成已绿）**：
  `15:57:55→15:58:05` 四档各 2 次（`single`/`shuffle`/`isolated`/`parallel`）**8/8 全 PASS**；
  `15:58:27→15:58:57` 另跑一次最接近主控给的那个形状 `go test -count=2 ./...` ⇒ `cmd/core 3.036s ok`，
  但 `internal/adapter [setup failed]`（`contract_table_test.go:139:49: unknown escape sequence`）。
  **不报成缺陷**：该文件 mtime **15:58:49**（正在我这次跑的中间），`15:59:13` 复跑
  `go vet ./internal/adapter/` ⇒ **exit 0** 已自愈 ⇒ 判为 adapter 在制品（T-68），与 15:29:31 那次红无关。
  证据落 `.qoder/evidence/test/flaky-watch-TestSealCovers.txt`、`flaky-fullrun-1.txt`、`flaky-watch-selftest.txt`。
- **09-21 15:39:57~15:41:20（本机 `date -Iseconds`）T-118 交付前最后一次四条重跑**，全部 exit 0：
  默认面 **46 PASS / 14 顶层 SKIP / 0 FAIL**、开 `LUCERO_TEST_FAKE_ADAPTER=1` **62 PASS / 9 SKIP / 0 FAIL**、
  lint **exit 0（第八轮）**、`--selftest` exit 0、`crash` exit 0。基线现取 `12a107f`。
  证形三句：`git diff --stat HEAD -- core/test scripts` ⇒ `2 files changed, 154 insertions(+), 8 deletions(-)`
  （两处就是 MS 指针 + F20 指针）、`git status --porcelain` **无 `??`**（前几轮的新文件都已被主控
  `03b4c36` 收进库）、最后代码改动 15:34 早于本窗口。
- **照契约② §4 判据表 MS 的命令原文复跑（15:35:28，验的是镜像后的权威文本，不是我自己的注释）**：
  带门控 `-run TestFakeAdapterModelSelectLegs ./test/` ⇒ 顶层 + `MS-1a`/`MS-1b`/`MS-2`/`MS-3`/`MS-4` **共 6 条 `--- PASS`**；
  `-run TestModelSelectShapeMatchesContract ./test/` ⇒ `--- PASS`（无门控）；
  **去掉门控 + 带 `-v`** ⇒ 看得见那行 `--- SKIP`；**门控和 `-v` 都不带** ⇒ 只剩一行 `ok`。
  后两条就是"计数命令假绿"的现场演示；所以契约里 MS-1~5 标"现存（门控内 PASS）"是准确的，
  我原来在注释里标"现存"偏松 —— 这条按主控的更正收下。
- **09-21 14:40:59~14:42:16（T-103 交付前）四条重跑**，全部 exit 0：默认面 46 PASS / 14 顶层 SKIP / 0 FAIL、
  开旗标 62 PASS / 9 SKIP / 0 FAIL、lint 第七轮 exit 0、selftest exit 0、crash exit 0。
  MS 六条的实测码值（当场从 `-v` 输出抄）：`-32602`（缺字段 ×2）、`-32006`（未知会话）、
  `-32002`（模型不在 offer）、正例回执 `{"remote_session_id":"rsn-new-1","model_key":"fake-fast"}` 恰两键；
  MS-6 两个类型的 json 字段集 = `{model_key, remote_session_id}`、无 `omitempty`。
- **09-21 14:13:17~14:14:24（本机 `date -Iseconds`）T-94 交付前最后一次四条重跑**，全部 exit 0：
  默认面 **45 PASS / 13 顶层 SKIP / 0 FAIL**、开 `LUCERO_TEST_FAKE_ADAPTER=1` **55 PASS / 9 SKIP / 0 FAIL**、
  lint **exit 0（第六轮）**、`--selftest` exit 0、`crash` exit 0。基线现取 `7c40d3b`；
  证形三句：`git diff --stat HEAD -- core/test scripts` ⇒ **只剩 1 个文件**（`ws_cursor_contract_test.go`
  `8 insertions(+), 8 deletions(-)`，就是 F20 表翻指针），因为其余交付物已被主控 13:59 的 `03b4c36` 收进库；
  `git status --porcelain -- core/test scripts` 只有那一行 `M`、无 `??`；
  `git log -1 --format='%h %ad' --date=format:%H:%M -- <每个交付文件>` 逐个证明它落在 `03b4c36`。
- **09-21 13:46:12~13:47:19（本机 `date -Iseconds`）T-98 交付前最后一次四条重跑**，全部 exit 0：
  默认面 `go test -count=1 -v ./test/...` ⇒ **45 PASS / 13 顶层 SKIP / 0 FAIL**；
  `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **55 PASS / 9 顶层 SKIP / 0 FAIL**；`check_ownership.sh` ⇒ **exit 0（第五轮）**；
  `--selftest` ⇒ exit 0（含 CO-36 三条新腿）；`crash_recovery.sh` ⇒ exit 0。
  **绿晚于最后一次代码改动**：`stat -c %y` 里最晚的是 `scripts/check_ownership.sh` 13:44:36，
  重跑窗口 13:46:12~13:47:19，中间无任何代码写入（其后只改本文件）。
  基线现取 `e3f3d9e`；证形三句：`git diff --stat HEAD -- core/test scripts`（3 个已跟踪文件）、
  `git status --porcelain -- core/test scripts`（`?? turn_attribution_test.go`、`?? turn_evidence_test.go`、
  `?? ws_cursor_contract_test.go`）、`git diff --no-index --stat /dev/null <新文件>`。
- **T-98 的实测原文（不是我转述，都在 evidence 文件里）**：
  W13：`判据面：33 张迁移表逐个扫 × transport/装配层 9 个生产 .go；白名单 3 处` + 逐行 `[W13-2] <表>: 0`；
  `--explain core/zz.go -> UNOWNED [无规则命中]`、`core/go.mod -> orchestrator [core/go.mod]`；
  T84-6a `turn_ref=…4ae9 的游标推到 5，State=RUNNING`；
  T84-6b `两个 tracker 都没动（"exec-t846-b"=""、"exec-t846-evil"=""），假 id 的帧 0 条转发；
  per-ref 游标推到 10 —— 那是同一次调用里不带 execution_id 的帧，不构成矛盾`；
  check 9 `[SCOPE 之外] core/ 扫到 0 行…（specs+docs 0 行）`。
- **一次假红的自我纠正（重要，因为它先指向了别人）**：`steering` 那一发我一度报成 adapter 违反 §5。
  真因是我自己的 `request()` 把通知帧当响应读（通知没有 `error` 字段 ⇒ 看起来"被接受"）。
  改成常驻读取器 + 按 id 配对后 adapter 显式回 `DELIVERY_MODE_UNSUPPORTED` ⇒ PASS。
  **教训并入 §7.1 那条同源纪律：报别人红之前，先证明我的读数方式能区分"响应"与"路上经过的帧"。**
- **09-21 13:07:47~13:08:49（本机 `date -Iseconds`）T-93 交付前最后一次四条重跑**，全部 exit 0：
  `go test -count=1 -v ./test/...` ⇒ **45 PASS / 12 顶层 SKIP / 0 FAIL**；带 `LUCERO_TEST_FAKE_ADAPTER=1`
  ⇒ **47 / 10 / 0**；`check_ownership.sh` ⇒ **exit 0（连续第四轮）**；`--selftest` ⇒ exit 0；`crash` ⇒ exit 0。
  顶层 SKIP 清单里 **`TestDispatchedProviderHandleUnruled` 已消失**（转成硬断言），换成
  **`TestTurnRefExecutionCrossCheck`**（T84-6，缺注入面，理由点名）。
  基线现取 `a30b804`（本轮内 `6843ed8`→`a30b804`，仍在漂）；
  `git diff --stat HEAD -- core/test scripts` ⇒ 3 个已跟踪文件 `384(+)/42(-)`；
  新文件按 REQ-T11 补：`?? core/test/turn_evidence_test.go`(274 行) + `?? core/test/ws_cursor_contract_test.go`(458 行)。
- **T-93 两条裁定的实测原文（`-v`，不是我转述）**：
  T84-4：`6 种通知逐个判过：缺 turn_ref ⇒ INVALID_PARAMS、带上 ⇒ 成帧`（六种含 claimed/dispatched）。
  T84-5：`claimed=prov-1、dispatched="" ⇒ State="RUNNING" Handle="prov-1" err=<nil>`；
  `同名 ⇒ RUNNING`；`异名 ⇒ State="UNKNOWN" Handle="prov-1" err=INTERNAL: turn/dispatched names provider "prov-2" …`；
  前提腿 `claimed(空) 被拒 ⇒ 停在 State=""；无打底的 dispatched(空) 拿不到 RUNNING`。
  T84-2 新增第 8 形状用哨兵断言（`wantState=t84NotDefinite`）：只要求"不是 CLAIMED/RUNNING/CLOSED"。
- **一次红，来源是我自己而不是别人**：13:06:57 T84-4/T84-2 各红一条 → 取证为我的拼接把文件写成 512 行
  （`head -n 211` + tail 跑了两遍，函数重复）。复原到 274 行、`grep -c '^func Test'` 每个名字 1 次之后
  连跑两次全绿；`internal/adapter/stream.go` mtime 全程 **13:00:28 未再变** ⇒ 排除 adapter 在途。
  另一次是我的判据错：把 T84-5 日志里"两次调用之后"的 `State="UNKNOWN"` 当成 `Claim` 的行为写进期望 ⇒ 红。
  两处都留在账上，因为它们都是"我这一侧的假信号"，等别人来纠正只会更慢。
- **09-21 12:24:12~12:25:15（本机 `date -Iseconds`）T-84 交付前最后一次四条重跑**，全部 exit 0：
  `go test -count=1 -v ./test/...` ⇒ **40 PASS / 12 SKIP / 0 FAIL**；加 `LUCERO_TEST_FAKE_ADAPTER=1`
  ⇒ **42 PASS / 10 SKIP / 0 FAIL**；`check_ownership.sh` ⇒ **exit 0（0 越界，连续第三轮）**；
  `--selftest` ⇒ exit 0；`crash_recovery.sh` ⇒ exit 0。
  这 12 条 SKIP 的顶层清单（每条都点名归属，无一伪装成通过）：
  `TestWriteEntryOpenQuestions`(主控裁 Q1/Q2)、`TestFakeAdapterSubprocess`/`TestCancelReplyIsResultFrame`(旗标关)、
  `TestCoreReadyEventIsJournalized`(REQ-T7)、`TestDispatchedProviderHandleUnruled`(**新，Q4 未定口径**)、
  `TestIdempotentReplayBodyIsByteIdentical`+`TestProjectionRejectedJournalAndAudit`(U13-6)、
  `TestCrashRestartIdentifiesInterrupted`/`TestCrashAdapterKillDoesNotTakeCoreDown`/
  `TestCrashNoDuplicateMessageAfterRestart`/`TestCrashApprovalNotLost`/`TestCrashEventJournalComplete`(§7 骨架)。
- **T84 判据两边都活的实测原文**（`-v`，`turn_evidence_test.go:159` 逐行打印）：
  `只 claimed ⇒ State="CLAIMED" err=<nil>`；`accepted=false ⇒ State="" err=<nil>`；
  `dispatched 先到 ⇒ State="UNKNOWN" err=INTERNAL: …CO-8 needs turn/claimed too`；
  `execution 对不上 ⇒ State="UNKNOWN" err=INTERNAL: names execution "e2" but the claimed turn is "e1"`；
  `缺 turn_id ⇒ State="CLAIMED" err=INVALID_PARAMS`；`重复 dispatched ⇒ State="UNKNOWN" err=INTERNAL`；
  `正例 ⇒ State="RUNNING" err=<nil>` —— 7 条里恰 1 条到 RUNNING，用例末尾对 `positives != 1` 判红。
  T84-3 实测：空 Handle 的 claimed ⇒ `INVALID_PARAMS: turn/claimed needs execution_id and provider_execution_id, got "e1" / ""`
  且被拒后 `ProviderExecutionID()` 仍为空；`accepted=false` 那一轮两个 id 都留下、State 不是 CLAIMED/RUNNING。
- **本轮证形**：基线现取 `git log -1 --format=%h` = `c1edf1b`（本轮内 `a727a1b`→`c193894`→`c1edf1b` 继续漂）；
  `git diff --stat HEAD -- core/test scripts` ⇒ 3 个已跟踪文件 `384 insertions(+), 42 deletions(-)`；
  两个新文件按 REQ-T11 的补法给出：`git status --porcelain` 列 `?? core/test/turn_evidence_test.go`
  与 `?? core/test/ws_cursor_contract_test.go`，`git diff --no-index --stat /dev/null core/test/turn_evidence_test.go`
  ⇒ `269 insertions(+)`。**没出现这些的 = 我没动**：别人的包、`specs/**`、`core/migrations/**`、`go.mod/go.sum`、
  `.gitignore` 全部不在我的改动集合里。
- **09-21 12:06:20~12:07:13（本机 `date -Iseconds`）REQ-F-20 交付前最后一次四条重跑**，全部 exit 0：
  `go test -count=1 -v ./test/...` ⇒ **29 PASS / 11 SKIP / 0 FAIL**；
  加 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **31 PASS / 9 SKIP / 0 FAIL**；
  `check_ownership.sh` ⇒ **exit 0**；`--selftest` ⇒ exit 0；`crash_recovery.sh` ⇒ exit 0（3 PASS）。
  证据：`.qoder/evidence/test/{go-test,go-test-fakeadapter,ownership-lint,selftest,crash}-final.txt`。
- **证形（含新发现的盲点）**：`git log -1 --format=%h` 现取 = `a727a1b`（本轮内 `1298fed`→`bd86192`→`e87293f`→`c193894`→`a727a1b`，
  主控持续落盘）；`git diff --stat HEAD -- core/test scripts` ⇒ 3 个已跟踪文件 `350 insertions(+), 40 deletions(-)`；
  **但 `git diff` 不列未跟踪文件** ⇒ 补 `git status --porcelain -- core/test scripts`（多出一行
  `?? core/test/ws_cursor_contract_test.go`）与 `git diff --no-index --stat /dev/null <该文件>` ⇒ `458 insertions(+)`。
  全部四类路径都在 `core/test/**`、`scripts/**`、我的 progress 文件之内；`specs/**`、`core/migrations/**`、
  `go.mod/go.sum`、`.gitignore` 一条都没有。
- **F20 三条判据的实测原文（`-v` 日志可见，不是我转述）**：
  F20-1 `{"type":"hello"}` ⇒ 首帧 `{"server_seq":3,"type":"hello_ack"}`；追加 seq=4 ⇒ 收到 `{"data":{"seq":4,…},"type":"event"}`；
  对照腿显式 `from_seq:0` ⇒ 同一套解析器读到补发 `[1 2 3]`。
  F20-2 不发 hello ⇒ 首帧 `{"from_seq":2,"type":"hello"}`、补发到达序列 `[3 4]`。
  F20-3 `query=0/hello=5` ⇒ 首帧 `{"from_seq":5,…}`、补发 `[6]`（错读「query 优先」会发 `[1 2 3 4 5 6]`）；
  `query=9/hello=2` ⇒ 首帧 `{"from_seq":2,…}`、补发 `[3 4 5 6]`（两种错读都发 `[]`）。
- **在途取证（12:01→12:06）**：`TestCrashKillCoreAndRestart`/`TestCrashRestartSameDataDir` 12:01 掉成 SKIP，
  Skip 理由自带 `main.go:138:7: core.reporter undefined`；`stat -c %y core/cmd/core/main.go` = `12:02:42`；
  12:06:07 `go build ./cmd/core` ⇒ exit 0，两条恢复 PASS。判定为 backend 保存中，未进别人目录动手。
- **09-21 11:16:32~11:17:22（本机 `date -Iseconds` 实取）交付前最后一次四条重跑**，全部 exit 0：
  | 命令 | 结果 |
  |---|---|
  | `CGO_ENABLED=0 go test -count=1 -v ./test/...` | **24 PASS / 11 SKIP / 0 FAIL** |
  | 同上 + `LUCERO_TEST_FAKE_ADAPTER=1 LUCERO_GO=…go.exe` | **26 PASS / 9 SKIP / 0 FAIL**（+2 = 子进程两条从 Skip 转 PASS） |
  | `bash scripts/check_ownership.sh` | **exit 0，0 项越界**（上一轮是 exit 1 / 4 条） |
  | `bash scripts/check_ownership.sh --selftest` | exit 0，`[SELFTEST OK]` |
  | `bash scripts/crash_recovery.sh` | exit 0，3 PASS / 5 SKIP（SKIP 逐条点名归属） |
  证据落 `.qoder/evidence/test/{go-test,go-test-fakeadapter,ownership-lint,selftest,crash}-final.txt`。
  **两条验收命令都带 `-v`**（`-run` 打空也会印 `ok`，不带 -v 就证不了跑了几条）。
- **本轮证形（T-72 形状）**：`git log -1 --format=%h` 现取基线 = `e87293f`（本轮内从 `1298fed`→`bd86192`→`e87293f`
  漂了两次，主控在并行落盘，所以每次都现取不写死）；
  `git diff --stat HEAD -- core/test scripts` ⇒ **只有 3 个文件**：
  `core/test/adapter_contract_test.go` +166/-…、`core/test/audit_chain_test.go` +32（上一轮 REQ-T3c 升硬）、
  `scripts/check_ownership.sh` +163 ⇒ 合计 `325 insertions(+), 36 deletions(-)`。
  别人的包、`specs/**`、`core/migrations/**`、`go.mod/go.sum`、`.gitignore` 一条都不在里面。
- **C8-5 判据两边都活（`-v` 输出里可读）**：`TestCancelReplyIsResultFrame` PASS（1.83s，真 build + 真子进程），
  同用例 `t.Logf` 印出反向用例实测值 ——
  合成 error 形态回执 ⇒ **3 条违反**（带 error 键 / 没有 result 键 / 线上出现 `"CANCELLED"` 码名）；
  合规结果帧 ⇒ **0 条**。真子进程回来的那一帧判为 0 条，且 `id` 回显 = 被取消那一 call 的 id。
- **W13 收紧的变异证据（T-66）**：`W13_RE` 换回旧形状的拷贝放 `scripts/w13_mut_tmp.sh` ⇒
  `SELFTEST FAIL：W13 模式命中 3 处，期望 1`；真扫里 `core/api/http/tasks.go … 引用表 tasks … 写调用`
  当场复活、lint exit 1。删变异件后取证：`find . -name 'w13_mut_tmp.sh'` = **0**、
  `ls scripts/` 只剩 `check_ownership.sh` + `crash_recovery.sh`、`git status --porcelain scripts/`
  只列 `M scripts/check_ownership.sh` ⇒ **变异版不在盘上，有据**。
  一次性产物（`w13_run*.txt`、`w13_mut.sh`、`gotest_*.txt`）全部落 `.scratch/test/`，未碰共享 Temp。
- 11:59 四条验收一次性重跑：`go test -count=1 -timeout 350s ./test/...` → **22 PASS / 10 SKIP / 0 FAIL**（exit 0）；
  `check_ownership.sh` → exit 1（4 条 = REQ-T8）；`--selftest` → exit 0；`crash_recovery.sh` 前两条为 PASS
  → `.qoder/evidence/test/{go-test,ownership-lint,selftest}-final.txt`
- 真跑凭据：`core.endpoint` 由 Core 自己写出（`{"host_port":"127.0.0.1:xxxx","token":...}`）、
  `/api/v1/health` 回 `{"state":"READY","version":"0.1.0-dev","migration_version":3,"ready":true}`、
  `Process.Kill()` 后目录里留着 `lucero.db-wal/-shm`、重开后 `PRAGMA integrity_check` = `ok`、
  tables=44、schema_migrations=[1,2,3]
- **本轮三次自纠（都是我先说错/写错、核实后改）**：
  ① 我怀疑 adapter 伪造 `CO-1` 引用来扩 SCOPE —— 实际 CO-1 在契约② §10:169，引用成立；真问题是 §7 漏同步（REQ-T5，已由你补行）。
  ② 我的 `literal` 桶第一版放宽到"字符串里含表名"，被 `"stream frames must be notifications"`
     和 `--dangerously-bypass-approvals-and-sandbox` 里的 `approvals` 淹掉 → 收紧为"整个字面量等于表名"。
  ③ 我写的同目录重启用例有竞态（读上一代残留 `core.endpoint` 拿已死端口去请求）→ 改成"文件 mtime 前移 + 端口换掉"；
     另外两条占位代码 `Process.Wait()` 造成 600s / 150s 死锁，已删。
- 变异验证累计：T-B 假边、T-C major/write_entry、T-D 补提交、T-14 塞 `signature`、§6.3 前缀合成件、
  T-24 `cross_package_refs` 三断言、前置检查必须 exit 2 —— 每次都能红，还原后绿
- REQ-T1/T2 前提复核为不存在：`packages/` 只剩 `api-client`、`git status --untracked-files=all` 里 node_modules 计数 0

## Recovery note

- Interruption point（09-28 19:20，T-276 收口轮交付完成 —— **时效取代下面 15:48 与 09-22 那两条**）：
  ① `core/test/crash_test.go:1408~1418` 的 approvals 归属越界**已修**（裸 SQL → `approval.SQLiteStore.Get`，
  断言 1419~1428 一字未动，1571 行 / sha1 前 12 `545493572759`）；自查 grep 15 命中全在
  `core\internal\approval\`、本文件 0 命中。② ownership **`OWN_EXIT=0` / VIOLATION 0**
  （selftest `SELFTEST_EXIT=0`）。③ 验收：`gofmt -l test` 空、`go vet ./...` exit 0、
  全仓 **18 ok / FAIL 0**、定向 **9 PASS / 5 SKIP / 0 FAIL** 且 `TestCrashApprovalNotLost` PASS。
  ④ **代跑两腿（别人的腿，原文交主控入册）**：t47-smoke 两遍 **21 OK / 1 FAIL**
  （红在 `RESULT_SEND_TWO_PHASE`，期望 RUNNING_v3 实得 SUCCEEDED_v4，两跑同值）、残留 0；
  build_stamp `STAMP_EXIT=0`（HEAD=cde8d79、指纹 `1a97a7f8d455202f7bc0d89afefa60f6`）。
  **我的在途件**（别的角色别误判成越界）：`core/test/crash_test.go`、
  `core/test/event_producer_roster_test.go`、`scripts/check_ownership.sh`、本文件。
  **等主控裁两件**：t47-smoke 那条 FAIL 归谁（见 Next action ①）、**REQ-T37**（名单锚句那一格）。
  仍缺前置（已点到 `file:line`，见 Completed 归属清单）：REQ-RC-1 → backend/recovery、
  `Supervisor.Run` 接线 → backend/process、approvals 接线 → T-372 + backend。
- First action after recovery（09-28 19:2x 版）：①**先复验没被盖掉**：`grep -c '^## '`（应 11 个标题）、
  `grep -n '^## '` 顺序应为 Objective / Completion criteria / Scope boundaries / Current checkpoint /
  Completed / Remaining / Blockers / Coordination / Changed files / Verification evidence / Recovery note；
  ②**越界自查先跑**（这是本轮的新门）：
  `rg -n "FROM approvals|UPDATE approvals|INSERT INTO approvals" core -g "*.go"` ⇒ 命中**只能**落在
  `core\internal\approval\`，`core\test\crash_test.go` 必须 0 命中；
  ③**四条读数**（须先 `$env:LUCERO_GO="D:\DevTools\go\bin\go.exe"`，否则 `buildCore` 找不到 go
  会**静默 `t.Skipf`** 把绿变成假绿）：`gofmt -l test`（空）、`go vet ./...`（exit 0）、
  `go test ./test/ -run 'TestCrash|TestIdempotent' -count=1 -v`（应 9 PASS / 5 SKIP / 0 FAIL）、
  `go test ./... -count=1`（应 18 包 ok / FAIL 0）；④**要再打变异**就照 W2-7 的套路：先
  `Get-FileHash … -Algorithm MD5` 记原值、`Copy-Item` 出备份，变异与还原**必须在同一批命令里做完**，
  还原后复验 md5 + 重跑该条用例必须回绿（跨命令复用旧备份 = 09-22 那一次的自我损坏）；
  ⑤前置一到（REQ-RC-1 / `Supervisor.Run` / approvals 任一接线）就把对应子 Skip 按用例注释里的口径
  换成硬断言 —— **是"换成硬断言"，不是把整条标成 PASS**；⑥**t47-smoke / build_stamp 仍是别人的腿**，
  再跑也只跑不改、原文交主控，不占我判据面。
- Interruption point（09-28 15:48，T-276 W2-7 —— **已由上面 19:20 那条时效取代，仅留档**）：
  `core/test/crash_test.go` 六条 Skip 已全部改写并跑绿（顶层 0 SKIP / 定向 9 PASS / 全仓 18 包 FAIL 0）；
  第 6 条按 T-66④ 的变异已打、已判 **FIRED**、已**字节级还原**（`core/api/http/tasks.go` md5 前后
  `21C7105E8F5A19B7F826299C38D3AF28` 一致，净改动 0 行）。**我的在途件**（别的角色别误判成越界）：
  `core/test/crash_test.go`、`core/test/event_producer_roster_test.go`、`scripts/check_ownership.sh`、
  本文件；**临时且已还原**：`core/api/http/tasks.go`。等主控回的：**REQ-T37**（名单锚句那一格）。
  仍缺前置（已点到 `file:line`，见 Completed 的归属清单）：REQ-RC-1 → backend/recovery、
  `Supervisor.Run` 接线 → backend/process、approvals 接线 → T-372 + backend。
- First action after recovery（09-28 版）：①**先复验没被上一轮盖掉**：`grep -c '^## '`（应 11 个标题）、
  `grep -n '^## '` 顺序应为 Objective / Completion criteria / Scope boundaries / Current checkpoint /
  Completed / Remaining / Blockers / Coordination / Changed files / Verification evidence / Recovery note；
  ②**三条验收命令**（须先 `$env:LUCERO_GO="D:\DevTools\go\bin\go.exe"`，否则 `buildCore` 找不到 go
  会**静默 `t.Skipf`** 把绿变成假绿）：`gofmt -l test\crash_test.go`、`go vet ./...`、
  `go test ./test/ -run 'TestCrash|TestIdempotent' -count=1 -v`（应 9 PASS / 5 SKIP / 0 FAIL）；
  ③**再跑全仓** `go test ./... -count=1` 应 18 包 ok / FAIL 0；
  ④**要再打变异**就照本轮的套路：先 `Get-FileHash … -Algorithm MD5` 记原值、`Copy-Item` 出备份，
  变异与还原**必须在同一批命令里做完**，还原后复验 md5 + `Select-String -Pattern 'MUTATION'` 命中 0 +
  重跑该条用例必须回绿（跨命令复用旧备份 = 血泪账里 09-22 那一次的自我损坏）；
  ⑤前置一到（REQ-RC-1 / `Supervisor.Run` / approvals 任一接线）就把对应子 Skip 按用例注释里的口径
  换成硬断言 —— **是"换成硬断言"，不是把整条标成 PASS**。
- Interruption point: 09-22 23:2x 交付完成（T-135 三件 C8-10/C8-11/C8-12 + T-141 第一段 T84-7a）。
  最新状态：默认面 `./test/` 顶层 53 条（声明 53 == 顶层结果 53），开 `LUCERO_TEST_FAKE_ADAPTER=1`
  再摘掉一条 SKIP；lint `exit 0`、`--selftest exit 0`、`flaky_watch.sh --selftest exit 0`。
  基线在 `3a71ea7`（23:19:52，那次提交只动契约① §6 与账本，我的判据在它之后重跑过）。
  等主控回的话：**REQ-T15**（上面那五行判据表回写 + "非字面量传码要不要判红"那一句要裁）、
  **REQ-T10**（五张表的镜像指针翻转还有没有剩）、Q1/Q2（`approval/respond` 与 `session/create` 的写方法归属）。
  在别人手里的：**T84-7b / REQ-T12 第二段**（backend 在 `cmd/core` 注 `process.Recorder` 实现）、
  U13-6、REQ-T7、T-D 剩余 4 条、T-142（db 的展开⑤）、T-143（adapter 的 import 白名单）。
- First action after recovery: ①最后一步必做的一次重跑（命令按**契约① §1.1 第 15 条**写：`^全名$` + 显式包路径）：
  `go test -count=1 -v -run '^(TestErrorCodeContractTablesParse|TestHTTPErrorCodesComeFromAdapterSet|TestAdapterCodesHaveHTTPMapping|TestTransportCodesAreInContract|TestEventCatalogMatchesJournalConstants)$' ./test/`
  （当前 5 PASS）+ `LUCERO_TEST_FAKE_ADAPTER=1 go test -count=1 -v -run '^TestDroppedFrameReachesAuditSeam$' ./test/`
  （7a PASS / 7b SKIP）；②**先查 §15.2 与事件目录有没有被再次扩列**（`git log -1 --format=%h` 对基线）——
  这两张表都会随功能长，判据已经改成不数行数，但趋势数一变我仍要重跑而不是抄上一次的数字；
  ③查 `cmd/core` 里有没有出现 `process.Recorder` 的实现（`grep -rn 'RecordDrop' core/cmd/`）——
  有就把 T84-7b 从 SKIP 改成硬断言：数 `audit_entries` 与 `event_journal` 两张表的**行数差**，
  判据形状已经写在该用例注释里（N 条丢弃 ⇒ N 行审计、`event_journal` 0 新增、两个 id 逐字保留）；
  ④查 U13-6/REQ-T7（`grep -rn core_ready core/ --include=*.go`、`POST /api/v1/tasks` 的路由），
  任一从 Skip 转 PASS/FAIL 即按注释里的口径改成硬断言。**F20/T84/C8 将来都要"加"被测面而不是换掉**：
  注入面证协议形状，真子进程证隔离，两面各证一件事。
  ⑤变异要重做时用这套（本轮的副本树是**一次性**的，交付前我已删）：
  `mkdir -p .scratch/test/mutroot/{specs/adapter-protocol,specs/events,core/api/http,core/internal/eventjournal}`
  + 把四类被测文件 `cp` 进去 + 在 `repoRoot()` 里临时钉一层该目录（跑完 `cp` 回备份并 `diff` 验无差异）。
  **教训别忘**：新行必须插进**表格区间内**（`append` 到文件尾落在 §15.3 之后 ⇒ 判据看不见 = 假对照）。
- 血泪账（供下一轮自查用）：本文件 13:1x 被我自己用**按行号**的 `sed -i '682,691d'` 删掉了整个
  Recovery note（前面的指针插入让行号整体位移）。恢复靠的是重读 + 用**内容锚点**的 Edit，不是行号。
  **这条正是 T-72①/记忆 item "别按行号锚定自己的文件"的第三次复发** —— 以后对这个文件只允许
  ①Edit（内容唯一锚点）或 ②先 grep 确认行号、再在同一条命令里做完、再复验 11 个标题。
  **09-22 23:3x 第四次复发（换了个新形态，同样是自我损坏）**：变异探针跑完我用 `cp 备份 目标` 还原，
  而那份备份是**四分钟前**做的 —— 中间我新加的 6 行展开自证表被那次还原**静默抹掉**了，
  而且探针跑完的输出看起来一切正常（备份本身是合法文件，diff 也"无差异"，因为比的就是它自己）。
  发现靠的是重跑后 `扫 5 个生产 .go、15 个字面码` 与上一次的 4/13 不一致 ⇒ 去查才看见文件少了 40 行。
  **立规**：还原用的备份必须**在变异前一刻**生成、且只在**同一条命令**里用；跨命令复用旧备份 = 拿旧文件盖掉新工作。
  还原后要判"回到变异前"不能只 `diff 备份`，还要**复验行数与用例清单**（`grep -c '^func Test'` + `wc -l`）。
  **09-23 02:1x 第五次同族（换了个新形态）**：往 `## Completed` 插新条目时我把锚点选成了
  "**下一条目的开头**"，于是新内容落到了上一条目尾部 —— `grep -c '^## '` 仍是 11、行号也连续，
  **只有按标题出现顺序回读才看得出归属错了**。立规：插入的锚点必须是**目标条目自己的末尾行**
  （或"条目头 + 其后第一条"这种同条目内锚）；插完立刻用 `grep -n '^- \*\*09-2'` 看**块的先后**
  而不是只数标题总数；修复用整块移动脚本，移动前后各 assert 一次"块必须夹在两个已知头之间"。

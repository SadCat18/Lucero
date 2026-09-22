OWNER_ROLE: test
TASK_ID: wave2-tests
STATUS: in_progress
SCOPE: Wave-2 三类测试（ownership lint / 状态机矩阵 / Adapter 契约 harness）+ Crash 骨架；对业务代码只读
EVIDENCE_DIR: .qoder/evidence/test/
LAST_UPDATED: 2026-09-22T15:45+08:00（本机 date 实取；T-131 的四条验收在 15:44:16~15:45:28 完成）

## Objective

用**独立于实现方**的测试与可执行 lint，证明 Wave-2 垂直链符合事件契约与 Adapter 协议，
并把角色文件所有权越界变成一条命令就能查出来的东西。

## Completion criteria

- [x] T-A `scripts/check_ownership.sh`：7 条检查逐条有输出、违反非零退出、带 `--selftest` 证明扫描器不是永真式
- [x] T-B 状态机矩阵：契约 §5.1/§5.2 由本角色独立转录，与 task/execution 实现双向对账 + §5.3/§5.4 投影
- [x] T-C Adapter 契约 harness：表驱动、不绑实现（§2/§2.2/§3/§5/§7/§9 + CO-4 线格式），进程内桩 + **真子进程第二被测面**
- [~] T-D Crash 骨架：§7 七条逐条落名；2 条今天真跑，5 条 `t.Skip` 并点名依赖方
- [x] 验收命令最后一次重跑全绿（见 Verification evidence 的时间戳，报绿只写最后一次）
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

- Stage: **09-23 00:0x 追加**：契约① §6 展开约定改了口径（③"最近一个全名"+ 新 ③b"混排非法必须报错"），
  我跟着翻了一次判据（`expandEventCell` 加两条守门 + 自证表加两行反例，两条守门各当场证明会咬）。
  之前：T-135 三件落完（C8-10 取代 C8-8 的字面数闸门、C8-11 与 C8-12 两条新腿）+ 账本 T-141 第一段
  （T84-7a 现存 / T84-7b 显式 SKIP）。
  lint 连续十四轮 exit 0；默认面 **53 个顶层用例（声明 53 = 顶层结果 53）/ 39 PASS / 14 顶层 SKIP / 0 FAIL**，
  开 `LUCERO_TEST_FAKE_ADAPTER=1` ⇒ **44 PASS / 9 顶层 SKIP / 0 FAIL**（含子用例 69 PASS、0 FAIL）；
  挂死解除后 `go test ./...` 也进了验收面：**18/18 包 ok**（"全"只指 `core` 这一个 Go 模块）。
  一次 `TestCrashRestartSameDataDir` 的红按 FL 处理成"撞上游改写、不算证据"，细节在 Verification 首条。
  三件等主控：REQ-T15（五行判据表回写 + "非字面量传码要不要判红"）、REQ-T14 剩余镜像、
  T84-7b 的 backend 第二段。
- Status: in_progress
- Last verified action: 23:34:19~23:35:0x 四条验收（`./test/...` 两面 + lint + 两个 selftest + crash_recovery），
  另跑"声明数=顶层结果数"对账 53=53
- Next action: ①**T84-7b**：`cmd/core` 里出现 `process.Recorder` 实现就摘 skip 取数
  （`grep -rn RecordDrop core/cmd/`），判据形状已在用例注释里；
  ②**REQ-T15** 五行回写后翻指针（C8-8 作废、C8-10/11/12 状态、C8-11 缺可跑命令、T84-7 拆两格）；
  ③**U13-6** + **REQ-T7** 落地后给 F20 三条**加**真子进程被测面（`turn_attribution_test.go` 已证明这条路通：
  `process.Supervisor`+真 FakeAdapter，不需要导出 `Connection`）。仍悬着：REQ-T10/T11、Q1/Q2。
  ④**backend 正在 `core/api/http/` 里加 `sessions.go`（此刻 `??` 未跟踪）**：C8-11 已自动把它纳入
  （4 个面码 `adapter_unresponsive`/`method_not_allowed`/`not_implemented`/`session_not_found` 全在 §15.2 左列），
  所以这条腿不用我盯，新码一出现它自己会红。
- Verification: `.qoder/evidence/test/{go-test,go-test-fakeadapter,ownership-lint,selftest,crash}-final.txt`
  （最新一批 12:24:12~12:25:15，数值见 Verification evidence 首条；旧快照不在此处保留，
  历史实测与在途取证都逐轮记在 Completed / Verification evidence 里。
  例：09-20 15:2x 曾出现 SKIP 10→12，当场取证为 `go build ./cmd/core` 报
  `c.Tasks.Pause undefined`、`c.Tasks.Resume undefined`（backend 在途），
  两条真跑用例据此按设计跳过并打印同一原因——没有拿桩绿冒充。

## Completed

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
- **REQ-T12 → 我不再拿它当整条阻塞（T-141 的三段归属我照做了）**：第一段（`adapter` 的 `RecordDrop` +
  `DroppedEvent`）22:5x 已落地，所以"丢弃到没到接缝"这半今天就有判据（T84-7a，已跑通）；
  第二段（`backend` 在 `cmd/core` 注实现）我**不替它伪造**，落成显式 SKIP 的 T84-7b。
  剩下唯一还在别人手里的是第二段的实现，不是"这题没人做"。
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

## Changed files

- `scripts/check_ownership.sh` — 9 条编号检查 + 2b/9（§7.1 W13 写路径归属）；§7 运行期解析（无副本、
  <12 条规则即 exit 2）、fail-closed 前置检查、`--explain`/`--rules`、`--selftest` 合成越界自证
  （含 W13-3 与 C8-5 的**反向用例**）。09-22 T-135 轮：check 8 的 INFO 行去掉"唯一例外 version_conflict"
  这句点名口径，改成与 C8-7 一致的**按形状**表述（`—` 开头的行天然不进集合）；字面量闸门本来就不在 lint 里
  （它一直用 `>=8` 下界），所以这次只有 lint 与 Go 执行者的**措辞**对齐，没有行为变化
- `scripts/crash_recovery.sh` — FIRST SPRINT §7 的跑法与 blocked 清单；09-23 00:1x 命令改锚定族形
  （`-run '^(TestCrash[A-Za-z0-9]*)$' ./test/`，见 §1.1 第 15 条）
- `scripts/flaky_watch.sh` — 契约② 判据表 FL 的执行者：四档（single/shuffle/isolated/parallel）+
  五句判决分类器 + `-a <ISO>` 时间线档（扫 `../core ../specs ../scripts` 里 ±90s/前看 SLACK 的 .go mtime，
  并打印扫了多少个文件）；`--selftest` 喂六组合成计数证明分类器不是永真式。
  09-22 23:4x 一处措辞整改：共享面枚举里"没有 `Config{` 字面量"这一档曾被印成"有测试吃了默认目录"，
  那是对 `cmd/core` 报一条**不存在的**风险（该包用 helper 构造配置，`DataDir: t.TempDir()` 在结构体字面量里）⇒
  现在两档各自说清"只证形状、不证结论"，真候选条件仍是 `Config{` 数 > `DataDir:` 数
- `core/test/statemachine_test.go` — §5.1/§5.2 独立转录，双向对账 + 终态 + UNKNOWN
- `core/test/projection_test.go` — §5.3/§5.4 投影（含 T-21 更正后的四条待填断言）
- `core/test/adapter_contract_test.go` — 协议 §2/§2.2/§3/§5/§7/§9 + CO-4 线格式；
  第二被测面 = 真子进程 FakeAdapter（真 build + stdio ndjson）
- `core/test/audit_chain_test.go` — T-14 哈希链时序不变量（13 列篡改 / Seal 顺序 / 脱敏接缝），
  已跟 REQ-T3 的 Kind 新形状；`TestRedactionKindIsMandatory` 记剩余一半
- `core/test/crash_test.go` — §7 Crash 清单：2 条真跑（杀 Core + 同目录无干预重启 + integrity）+ 骨架
- `core/test/turn_evidence_test.go` — §4.2 两帧取证信号四条语义的独立转录（判据表 T84-1..5）；
  7 形状只 1 形状可达 RUNNING；⚠1/⚠2 两处未定口径按"不写死"处理（T84-5 为 SKIP（blocked））
- `core/test/turn_attribution_test.go` — §4.2 裁定 ① 的交叉校验腿（T84-6a/6b）：真 `process.Supervisor`
  + 真 FakeAdapter + `CallTurn` 绑定，一致放行 / 不一致丢弃且两个 tracker 都不动、假 id 帧 0 转发。
  09-22 T-135 轮：文件头的两行小表改成**指向契约② T84 表的指针**（REQ-T10）；新增
  `TestDroppedFrameReachesAuditSeam`（T84-7a 现存 / T84-7b 显式 SKIP）+ 自带 `dropRecorder`
  观察面与 `var _ process.Recorder` 编译期钉子；`startSupervisedFake` 拆出 `startSupervisedFakeOpt(rec)`
- `core/test/errcode_mapping_test.go` — 契约② 判据表 C8-6/C8-7/C8-10/C8-11/C8-12 五条行的 Go 执行者：
  §15.1/§15.2 双列解析（含 `skipped` 计数器与左列形状守卫）、`core/api/http/**` 的 `writeError`
  字面码提取（先去注释、再按顶层实参取第 3 个），事件目录 ↔ `eventjournal` 常量双向对拍 +
  合并行展开约定（①~⑤）的 6 行自证表。**五个顶层用例全部只用 `t.Errorf`、不用父级 `t.Fatal`**（T-124）
- `core/test/ws_cursor_contract_test.go` — REQ-F-20 游标权威三条判据（F20-1/2/3）：真 httptest +
  真 WS 握手 + 注入 `wsapi.Journal` 替身 + 自带 RFC6455 客户端；含"错读对照"与解析器活性对照
- `.qoder/progress/test.md` — 本文件

## Verification evidence

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

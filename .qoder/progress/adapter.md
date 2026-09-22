OWNER_ROLE: adapter
TASK_ID: adapter-protocol
STATUS: in_progress
SCOPE: Core↔Adapter 协议实现（帧编解码/握手/capability/三态/流式/额度）+ 子进程监管 + FakeAdapter 本体；不改 specs/docs，不碰其它角色的包
EVIDENCE_DIR: .qoder/progress/adapter.md（本轮为纯单测证据，不落 .qoder/evidence/）
SCRATCH_DIR: D:\Project\Lucero\.scratch\adapter\（T-47：一次性产物只准落这里，仓库已 ignore；
             永不碰 /tmp、%TEMP%、$env:TEMP 的通配删除；只删自己建的具体文件名）
LAST_UPDATED: 2026-09-23T00:35:39+08:00  # 本机 date 实读（T-155 删字段后）
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

- Stage: 第 5 轮 B 段完成 = **§10 的 11 项全部到齐**（新落地 `session/list`、`session/read`、`session/stop`、
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
- Status: in_progress
- Last verified action: 交付前最后一次全仓跑 **09-22 22:56:35 起 / 22:56:43 收**（`date` 实读），
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
- Next action: 把 probe 交给 backend 接线（`api/http` 薄投影 + `cmd/core` 构造注入 Launcher），
  并附上 §7.2 新增两条要求已落地的证据；合验 REQ-T12 时一起验丢弃接缝写出的审计行。
  另：`Registry.Probe` 的**成功路径目前只被进程内测过**（真握手走 `scriptedLauncher`），
  真子进程那条腿要等 backend 递进真 Supervisor。
- Verification: `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/...`

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


## Remaining

- FakeAdapter 剩余 **6 个 C→A 方法**（14:2x `grep -c 'case "<method>"'` 量的，见 Verification）：
  `session/list`、`session/read`、`session/stop`、`model/list`、`approval/list`、`approval/respond`。
  §10 的 **4 种注入已全部落地并有真进程用例**（crash/fail/hang/unknown_quota）；§14 的"全 11 方法"
  现在到 4 个 C→A 方法 + `stream/*`（`turn/claimed`+`turn/dispatched`+`stream/delta`+`turn/finished`）。
  **R-004 出口条件仍不成立**，缺的就是上面那 6 个
- Supervisor 到 DB 的那段：`process_instances` 登记、§14 三列（`state`/`last_error`/`shutdown_reason`）
  与 §8 的 `adapter_restart_exhausted`/Notification —— `internal/process` 只发事件不落库，
  接线方仍未点名（CO-20）
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

## Changed files
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

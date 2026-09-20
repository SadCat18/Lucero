OWNER_ROLE: adapter
TASK_ID: adapter-protocol
STATUS: in_progress
SCOPE: Core↔Adapter 协议实现（帧编解码/握手/capability/三态/流式/额度）+ 子进程监管 + FakeAdapter 本体；不改 specs/docs，不碰其它角色的包
EVIDENCE_DIR: .qoder/progress/adapter.md（本轮为纯单测证据，不落 .qoder/evidence/）
LAST_UPDATED: 2026-09-19T18:22+08:00  # 本机时钟；主控账本里的事件时间（16:35/16:38/18:10）领先本机，以本机 `date` 为准记录，不套用主控时间

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

- Stage: 第 2 轮（协议层）已交付；按主控两次指令跳到 FakeAdapter，已落**第 4 轮切片 1**
  （真子进程骨架：握手门 + health + debug/crash + §1 stdout 纯度）。
  **第 3 轮 `internal/process` Supervisor 仍空的**——§8 的"崩溃不带崩 Core"完整回归要到那轮才成立，
  现在拿到的只是它的原语（真进程、退出码 2、父进程存活可断言）
- Status: in_progress
- Last verified action: 交付前最后一次重跑（本机 **18:22:18**，三包一起，还原全部变异之后）：
  `CGO_ENABLED=0 go test -count=1 ./internal/adapter/... ./cmd/fake-adapter/... ./test/...`
  → `ok internal/adapter 0.970s` / `ok cmd/fake-adapter 1.279s` / `ok core/test 2.596s`；
  `go vet ./cmd/fake-adapter/ ./internal/adapter/` 干净、`gofmt -l` 0 行、`go build ./...` 无输出
- Next action: 回到第 3 轮 `core/internal/process/`：Supervisor 拉起 + stdout/stderr 双管道 +
  握手门前置 + 退避 `1s,2s,4s,8s,≤30s` + 连续 5 次→`UNAVAILABLE`+停重启+Notification；
  **消费 `000003` 的 §14 三列**（写 `state`、`RUNNING`→`STARTING`、按状态对账写 `STOPPING` 或
  `last_error`/`shutdown_reason`、退出后回读 `state` 再判 §6.3），并真正发
  `adapter_downgraded`/`adapter_restart_exhausted`（§11 双事件，走注入的写函数）；
  握手完成后、喂第一帧之前调 `Cursor.Reset`
- Verification: `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test ./internal/adapter/... ./internal/process/... ./cmd/fake-adapter/...`
  （`internal/process` 落地前那一段仍会 `[setup failed]`，CO-7 口径）

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
- 第 2 轮共 4 次变异检验，全部命中：`session/create` 从清单删掉 → `TestCallGate` 1 行 FAIL；
  `QueuePosition` 降级拦截改成恒假 → `TestSendResultValidate` 3 行 FAIL；
  §6.1 `sequence_no` 门退回"只拦负数" → `TestDecodeStream` 1 行 FAIL；
  Core 码挪进 `-32050` → `TestErrorCodeTable` 4 行 FAIL。每次都只坏对应断言，还原后复绿。

## Remaining

- §5/§7.2 的**请求参数面**：`session/send` 的入参形状（`text`/`delivery`/`Idempotency-Key` 必填）
  与 `guide` 命中时的双态记录（当前轮 INTERRUPTED + 新轮）属派发层，第 3 轮一起做
- `$/cancel` 与 deadline 的**调用侧**：帧构造（`NewCancel`）与 `deadline_at` 编解码已就绪，
  缺的是持有 pending 请求表的 pump goroutine —— 它必须与进程同生命周期，故归第 3 轮 `internal/process`
  （§9.2 第 5 条"不裸启动无限 goroutine"）
- Supervisor：拉起 / 登记 `process_instances` / stdout-stderr 分流 / 退避 `1s,2s,4s,8s,≤30s` /
  连续 5 次→`UNAVAILABLE`+停重启+Notification / 每次重启后 `Cursor.Reset`
- §8 崩溃回归（包内版：Core 存活 + 该 in-flight **Execution 状态值**转 UNKNOWN + 其它 Adapter 不受影响）
- FakeAdapter（第 4 轮，落点 `core/cmd/fake-adapter/`）：§4 全 15 个 C→A 方法 + 4 种故障注入
  + §10 追加的 `task/accept`、`debug/delivery_semantics`（注入"同步成功但实际未入队"的假阳性）
- §9 白名单接入 Supervisor 启动路径：注入面已裁（CO-3，`func(args ...string) []string`），待落

## Blockers

- **B-a1 已解除**（CO-1 裁定，commit `cea05b7` 落的契约 §10 文本）：FakeAdapter 住
  `core/cmd/fake-adapter/`（真子进程，`adapters/fake/` 只放 fixture）；第二个 module 与 `go.work`
  一并否掉。**注意**：主控消息里那次"方案②、`adapters/fake/` 起独立 go.mod + go.work"的表述
  与契约 §10 相反，我以契约文本为准（`internal/` 规则那条理由就是我提的）。
- **B-a2 已解除**：`core/internal/eventjournal/events.go:13-14,67` 现有
  `AdapterDowngraded` / `AdapterRestartExhausted` 两行常量，第 3 轮发事件不再撞未登记。
  登记方式已裁为 §11 双事件（audit 写 `audit_entries` 事件行 + adapter 写 `event_journal`）。
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

## Changed files

- `.qoder/progress/adapter.md`（新建）— 本文件
- `core/internal/adapter/protocol.go` — 帧、§1 整数码表、`$/cancel`、Reader/Writer
- `core/internal/adapter/handshake.go` — 版本协商、capability 校验与有效能力、未握手/只读门、
  三态投递门、§12.11 写方法清单、§4.1 `HealthResult`
- `core/internal/adapter/stream.go` — §6 六条通知的解码/构造、§6.2 `Cursor`（幂等丢弃 + 续传游标）、
  §6.3/§6.4 `TurnTracker`
- `core/internal/adapter/quota.go` — §7 额度形状与 UNKNOWN≠0 校验
- `core/internal/adapter/send.go` — §5/§7.1 请求形状（含 `queue_strategy`）、§7.2 回复形状、
  §4.2 `session/create` 幂等形状
- `core/internal/adapter/{protocol,handshake,stream,quota,send,purity}_test.go` — 表驱动，共 33 个测试函数
- `core/internal/adapter/README.md` — 点分/斜杠两套拼法的唯一说明 + 引用状态
- `.qoder/scratch/turntests.txt`（临时，已删）

## Verification evidence

- **交付口径（规矩 5）= 本机 17:53:52~17:53:56 的最后一次重跑**，原文：
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

- Interruption point: 第 2 轮已交付复绿（33 个测试）。三件事等主控：B-a4 契约文本落盘、
  CO-13/CO-15 两个形状裁定、B-a5 的第 3/第 5 轮顺序确认。
- First action after recovery: 读本文件 `Current checkpoint` → 先重跑
  `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/adapter/...`
  复现绿灯（不要相信本文件的声明），再建 `core/internal/process/`：
  Supervisor 拉起 + stdout/stderr 分流 + 退避 `1s,2s,4s,8s,≤30s` + 连续 5 次→`UNAVAILABLE`，
  **握手完成后、喂第一帧之前必须调 `Cursor.Reset`**，`internal/process` 侧才允许 import `internal/{audit,eventjournal,platform/db}`
  （`internal/adapter` 的零 import 守卫不许放松）。

# Adapter Protocol v0.1（Wave-1 契约②）

- 状态: **accepted**（用户 2026-09-19 16:35 批准，Wave-2 的 `adapter` 角色据此派发）
- 上游依据: ADR-005 Adapter 进程隔离、ADR-006 协议版本化、架构文档「Adapter」一节的能力等级
- 关键输入: Phase 0 能力矩阵 §6 的六条设计影响（本契约逐条落地，见 §12 对照表）
- 范围: Core ↔ Adapter 进程之间的协议。**不含** Core↔UI 的 REST/WS（那是契约③）

## 1. 传输与帧格式

- **ndjson over stdio**：Core 用 `exec` 拉起 Adapter 作为**子进程**，Adapter 永不反向连接。
  一行一个完整 JSON 对象，**帧内不得含裸换行**。
- **stdout 只准放协议帧**；日志、进度、诊断一律走 **stderr**（Core 原样收进 blob，不落 event_journal）。
  违反此条 = Adapter 一打日志就打断协议流，属 P0 实现缺陷。
- JSON-RPC 2.0 语义：`{jsonrpc:"2.0", id, method, params}` / `{jsonrpc:"2.0", id, result|error}` /
  通知 `{jsonrpc:"2.0", method, params}`（无 `id`）。
- 每个请求带 `request_id`（UUIDv7）与可选 `deadline_at`（Unix ms）。取消用通知 `$/cancel`，
  携带原 `id`；Adapter 必须让对应 Execution 进入 `CANCELLED` 或 `INTERRUPTED`，不得静默吞掉。
- **不做**：HTTP 服务发现、长连接复用、跨机连接。要远程 Provider 时另开 ADR。

## 2. 握手 `adapter/initialize`

Core 启动 Adapter 后**必须**先调它；未握手的任何方法一律拒答（`PROTOCOL_NOT_INITIALIZED`）。

```jsonc
// → params
{"protocol": {"major": 1, "minor": 0}, "core_version": "0.1.0"}
// ← result
{
  "adapter_key": "codex",
  "adapter_version": "0.1.0",
  "protocol": {"major": 1, "minor": 0},
  "software_versions": {"cli": "0.154.0", "desktop": "26.915.3509.0"},   // 见 §2.1
  "auth_mode": "REUSE_CLIENT" | "OWN_LOGIN" | "API_KEY" | "NONE" | "UNKNOWN",
  "health": "HEALTHY" | "DEGRADED" | "UNAVAILABLE" | "UNKNOWN",
  "capabilities": [ /* §3 */ ],
  "write_entry": true                                   // false 时本 Adapter 只能观测
}
```

### 2.1 `software_versions` 必须是多值映射（不是单个字符串）

Phase 0 实测：五家里有多家存在 **2~3 条独立版本线**（Codex CLI 0.154.0 / Desktop MSIX 26.915.3509.0；
WorkBuddy 桌面 5.5.6 / 内置 CLI 2.137.1 / sandbox 5.5.5）。`adapter_instances.software_version` 是 `TEXT`，
**存 JSON 文本**（key 由 Adapter 自报，值语义各家不同），UI 按 key 展开显示。

### 2.2 版本兼容规则

`major` 不同 → 直接拒绝启动并置 `UNAVAILABLE`（不猜）。`major` 相同、`minor` 高于 Core 已知 → 允许，
但必须记 `EXPERIMENTAL` 到 audit。协议版本只在握手协商，**运行中不改**。

## 3. Capability 声明

```jsonc
{"name": "session.send", "support_level": "NATIVE",     // 八等级，架构文档冻结的集合
 "effective_backend": "app_server_jsonrpc",              // native_api | cli | local_data | ui_automation | read_only
 "write_capable": true,
 "delivery_modes": ["start_now", "guide", "queue"],      // 见 §5，逐项显式声明
 "confidence": "HIGH", "requires": ["session.list"],
 "last_verified_at": 1789800000000,
 "metadata_json": {}}
```

- `support_level` 八值集合不变（`NATIVE/COMPATIBLE/CLI/LOCAL_DATA/UI_AUTOMATION/READ_ONLY/EXPERIMENTAL/UNSUPPORTED`），
  **Core 只消费"当前有效能力"**，并写进 `adapter_capabilities`（每能力一行）。
- 降级是**运行时可观测的事件**，不是启动期的一次决定：`adapter_downgraded` 事件（→ 事件目录需补登，见 §11）。
- **`write_entry: false` 是合法声明**（Phase 0 §6.1：Z Code 实测无 CLI、无本地监听、无 API）。
  此时任何写方法返回 `WRITE_ENTRY_NONE`，Core 必须把该 Adapter 归为"只读观测型"，
  UI 不得渲染任何写操作入口，也不得降级去点 UI 假装写成功——除非该 Adapter 显式声明了
  `UI_AUTOMATION` 等级的对应能力。
- **UIA 是一等实现，不是临时兜底**：`effective_backend: "ui_automation"` 与 `cli`/`local_data` 同级，
  同样要有 `last_verified_at`、`confidence` 和降级链记录。

## 4. 方法表（v0.1）

| 方法 | 方向 | 说明 |
|---|---|---|
| `adapter/initialize` | C→A | §2 |
| `adapter/health` | C→A | 返回 `health` + `confidence`，不得用 `UNKNOWN` 冒充 `HEALTHY` |
| `session/list` | C→A | 分页 `cursor`；返回 `remote_session_id`、`title`、`project_path_hint`、`updated_at` |
| `session/read` | C→A | 全量或从 `since_cursor` 起；返回 `messages[]` + `next_cursor` + `remote_revision` |
| `session/send` | C→A | §5 三态；`Idempotency-Key` 必填（高风险写） |
| `session/stop` | C→A | 中止当前生成；不支持则 `CAPABILITY_UNAVAILABLE`，**不得返回"已停止"了事** |
| `model/list` | C→A | 带 `parameter_schema_json` 与 `provider_raw_json` |
| `quota/read` | C→A | §7 额度契约 |
| `approval/list` | C→A | 挂起审批；载荷必须结构化 |
| `approval/respond` | C→A | 幂等；同一审批二次回应返回首次结果 |
| `session/create` | C→A | **REQ-T8 / CO-5 回写（13:3x，从 `internal/adapter/send.go` 的 `CreateSessionParams` / `CreateSessionResult` 直接读，不是转述）**：请求 `{client_request_id（必填）, project_id?, title?, base_sha?}`；回执 `{remote_session_id（必填）, created, provider_raw?}`。`client_request_id` 由 Core 生成 UUIDv7 且强制，因为**这一调用天然非幂等，没有它重试会建出两个远端会话**；`created: false` **不是失败**，是幂等命中、返回首次那条；`base_sha` 可空由 CO-14 允许（对不齐 commit 的 Provider 分支留空）。回执缺 `remote_session_id` → `INVALID_PARAMS`（原文："CO-5 has no third shape"） |
| `model/select` | C→A | **CO-36 定形（13:5x，两半分开记）**：**请求 `{remote_session_id, model_key}`，回执同形状**（`model_key` 必须是 `model/list` 给过的值）。<br>**⚠ 我上一版把这条写成"实现为零"是错的，而且是同族第三次**：我只 grep 了 `core/internal/adapter/`，没 grep 全仓——**Adapter 侧其实已经真在跑**：`core/cmd/fake-adapter/service.go` 的 `methodModelSelect` 常量、`modelSelectParams` 结构（注释自说"§4 那行没给形状，本处补上"）、以及三条拒绝腿（缺字段 / 未知 `remote_session_id` / 未知 model），`readers_test.go` 还有四条表驱动用例。**准确说法是"Adapter 侧已实现、Core 侧形状待补"**（`internal/adapter` 里确实只有 `handshake.go` 的 `writeMethods` 一项，没有请求/回执类型）。<br>**@test：这条现在有可断言面**，别按我那句错话漏验 FakeAdapter 那三条拒绝腿；Core 侧一旦补出类型，就把它升成契约② §15.1.1 那种"码↔形状"互校用例。 |

#### 判据表 MS（`model/select` 真子进程 + Core 类型互校；从 `core/test/adapter_contract_test.go` 镜像进本文，15:2x）

**状态列是主控 15:18:51→15:19:38 逐条实跑标定的**，不是抄 test 的自述：
`TestFakeAdapterModelSelectLegs` **默认 `SKIP`**（live 门控 `LUCERO_TEST_FAKE_ADAPTER=1`），
**带上门控后 `--- PASS`，子用例 MS-1a/1b/2/3/4 全 PASS**；`TestModelSelectShapeMatchesContract`（MS-6）**常跑即 PASS**。
**注意 `-run` 是前缀匹配**：`-run TestFakeAdapterModelSelect` 会命中 `...Legs` 并只报 `ok`，
**不带 `-v` 就看不见那行 `SKIP`** —— 这正是 test 自己那条"计数命令假绿"规矩的现场版。

| ID | 断言 | 当场命令 | 状态 |
|---|---|---|---|
| MS-1 | 缺 `remote_session_id` 或缺 `model_key` ⇒ `INVALID_PARAMS` | `LUCERO_TEST_FAKE_ADAPTER=1 go test -count=1 -v -run TestFakeAdapterModelSelectLegs ./test/` | 现存（门控内 PASS：MS-1a/1b） |
| MS-2 | 引用不存在的 `remote_session_id` ⇒ `SESSION_NOT_FOUND` | 同上 | 现存（MS-2 PASS） |
| MS-3 | `model_key` 不在 `model/list` 的 offer 里 ⇒ `CAPABILITY_UNAVAILABLE`（**不支持必须显式说，不许静默换模型**） | 同上 | 现存（MS-3 PASS） |
| MS-4 | 正例：会话取自 `session/create`、模型取自 `model/list` ⇒ 无 error 且回执同形状；**两个取值都当场问对端要，不写死 fixture 常量** | 同上 | 现存（MS-4 PASS） |
| MS-5 | 三个拒绝码两两不同（§4.1"两处 `model/select` 的门不要混"） | 同上 | 现存（同批子用例） |
| MS-6 | **Core 侧类型 ↔ 本节定形互校**：字段恰为 `{remote_session_id, model_key}`，json tag 不许漂 | `go test -count=1 -v -run TestModelSelectShapeMatchesContract ./test/` | 现存（无门控，直接 PASS） |
| `session/locate` | C→A | **v0.1 只留桩**：Phase 0 §5 结论是五家 deep-link 到会话粒度全部未实测，
  只有 OpenCode `/tui/select-session` 有程序化证据。返回 `CAPABILITY_UNAVAILABLE` 即为正确实现 |
| `stream/*` | A→C | 通知流，§6 |
| `debug/*` | C→A | §9，FakeAdapter 专属 |

### 4.0 `session/push` —— **v0.1 明确不做（CO-40 裁定，15:2x）**

**先撤我说过的一句错话**：T-110 里我写"`session/push` 现在可以做，不必等本行以外任何东西"——
**盘上没有这个东西**：`grep` 全仓 `specs/` 与 `docs/` 对 `session/push` **0 命中**，§4 方法表也没有它的行。
**adapter 比我先拒绝了我**：`cmd/fake-adapter/main_test.go` 现在就断言 `session/push` 必须返回 `INTERNAL`，
理由原文是"**§4 还没有这一行、CO-25 还欠它一个 `turn_ref` 故事，现在接就是代码跑到契约前面**"。
它没有照我那句话动工，这个纪律是对的。

**裁定：撤销，不补四样。** 三个理由：① **没有消费者**——`§10` 的验收清单、Wave-2/3 的出口条件都不需要它；
② **它是 A→C 自发通知**，而裁定① 刚把归属收成"全部通知一律带 `turn_ref`"的无例外规则 ——
**Core 从未发出过 request，就没有 Core 签发的票号可回显**，这是一个真实死结而不是措辞问题；
③ 为一条不存在的需求去重开刚定稿的归属模型，代价是**把刚刚收干净的东西再松开一次**。

**重开条件（将来谁要它，先答完这四样再动代码）**：
方向（A→C 通知 ⇒ 归属规则怎么办）、载荷形状、**自发流的 `turn_ref` 从哪来**、
以及它与 §5 三态队列 + `turn/finished` 排空点（CO-25）的关系。
答不完就写代码 = 再一次"代码有、契约没有"，本条就是为拦那件事存在的。

方法名固定为 `域/动作`（小写、斜杠分隔）。Adapter 内部方法名与其原生协议**无关**——
Codex 的 `ThreadList` 由 Codex Adapter 自己映射到 `session/list`，映射表写进 Adapter 的
`metadata_json`，Core 不感知原生命名。

### 4.1 写方法清单（Q1 裁定，16:2x —— 补上 §3 一直没给的清单）

**起因有两层。** 表层：`internal/adapter` 的 `writeMethods` 排除了 `approval/respond`，
注释理由是"审批走 permission channel、由 Permission Engine 而不是 `write_entry` 管"，并引用
**"§12.11 (ruled 2026-09-19)"** —— 而 `grep -rn "12\.11" specs/` **零命中**（`test` 11:3x 先报，我 16:2x 复核一致）。
**那是一个不存在的条款号。** 里层更要紧：我去核对这张表时发现**代码与本文的方法集合本身就不一致**——

| 方法 | 在 §4 方法表里？ | 在代码 `writeMethods` 里？ | 处置 |
|---|---|---|---|
| `session/create` | **✗ 没有** | ✓ 有 | **§4 少一行、代码多一项**：要么本文补 `session/create` 的签名与语义，要么代码删这一项。**归 adapter 选，选完同轮回执**（我不擅自判，因为我不知道它是不是第 3 轮为 FakeAdapter 加的） |
| `approval/cancel` | ✗ 没有 | ✗ 没有 | **它根本不存在**。我起草本节时把代码注释里的这句当事实抄了一遍，16:3x 核对才拦下——**这正是" phantom 条款号"会怎么扩散的现场样本**：一句无出处的注释，两轮后就长进了权威文档 |
| `approval/respond` | ✓ 有（C→A，幂等） | **✗ 被刻意排除** | 见下面的裁定 |

**裁定：`approval/respond` 算写方法**，声明 `write_entry:false` 的 Adapter 必须对它返回 `WRITE_ENTRY_NONE`。
理由不是分类洁癖：
- **回应审批 = 授权一个外部动作被执行**（写文件、跑命令），是全部能力里后果最重的一次写。
- **两道门不许互相顶替**：Permission Engine 管"**Core 要不要问这个审批**"，`write_entry` 管
  "**这个 Adapter 物理上能不能办事**"。拿前者豁免后者，等于"因为门禁把关了，所以允许往一扇没有把手的门推人"。
- **实测面**：Phase 0 #17「审批响应」在 Z Code 是 `UI_AUTOMATION`、WorkBuddy 桌面端是 `CLI` ——
  一个声明无写入口的 Adapter **本来就没有回答审批的通道**。放行它只会让 Core 发出一条注定失败
  或被对端静默忽略的指令，而 **UI 以为审批已经送达** —— 那就是 §12 的"把 UNKNOWN 显示成确定值"，P0 同型。

**本文口径下的写方法集合**（只列 §4 方法表里真实存在的名字）：
`session/send`（三态 `start_now`/`guide`/`queue` 全在内）、`session/create`（CO-5，见 §4 表：`client_request_id` 强制正是因为它非幂等）、`session/stop`、`model/select`、
**`approval/respond`**、`debug/*`（§9，仅 `adapter_key=fake`）。
**读方法 / 非 C→A 调用**：`adapter/initialize`（握手，§2 豁免）、`adapter/health`、`session/list`、
`session/read`、`model/list`、`quota/read`、`approval/list`、`session/locate`（v0.1 只留桩），
以及 `stream/*`（**A→C 通知，不是"方法调用"，不在写入口判定范围内**）。

**两处 `model/select` 的门不要混**：代码注释说它的拒绝"其实是能力门（`CAPABILITY_UNAVAILABLE`）"——
**两句都对**：`write_entry:false` 时返 `WRITE_ENTRY_NONE`，声明了写入口但当前不具备时返
`CAPABILITY_UNAVAILABLE`。判据：**两个码不许互相代替**，因为它们对应两个不同的用户结论（"这家不能写" vs "现在不行"）。

**代码侧引用规矩**：注释里引本文**只准写 `§4.1`** 或方法名，禁止引用不存在的条款号。
可机判形式：任何 `§<数字>` 引用都必须能被 `grep -n "^#\+ <同号>" specs/**/*.md` 查到，查不到即缺陷。

### 4.2 两帧取证信号 `turn/claimed` / `turn/dispatched`（CO-29 回写，11:5x）

**这是主控欠的文档，不是实现的问题。** 11:48:03 实测：这两个名字在 `specs/` 与 `docs/` 里
**0 个文件命中**，在 `core/` 里 **7 个文件在用**（`internal/adapter/stream.go` 定义与校验、
`cmd/fake-adapter` 发出、`internal/process` 消费）。CO-8 当年裁了这套两段式信号，**但从没回写进本文**，
所以 §10 的验收清单字面盖不到它，`test` 的状态机用例与 adapter 自己的 §7.2/§10 语义用例都缺前置。
以下按 `stream.go` 的**实现与校验代码**记录，不是按转述：

| 帧 | 载荷 | 含义 | 校验（实现在 `StreamFrame.Validate`；**13:0x 曾按裁定前口径写本表，现已与 ✅ 段对齐**） |
|---|---|---|---|
| `turn/claimed` | `{turn_ref, execution_id, accepted, queue_position?, provider_execution_id}` | **Adapter 接下了这条指令** | `execution_id` 为空 → `INVALID_PARAMS`；`provider_execution_id` 为空 → `INVALID_PARAMS`，理由原文："Recovery cannot query a run it cannot name" |
| `turn/dispatched` | `{turn_ref, turn_id, execution_id, provider_execution_id?}` —— **前三者必填**；`provider_execution_id` 可空，但给了非空就必须等于 `claimed` 那个值 | **一个 turn 真的开始了** | `turn_ref`/`turn_id`/`execution_id` **任一为空 → `INVALID_PARAMS`**；`provider_execution_id` **允许为空**（12:45 裁定②，采 CO-13），但给了非空就必须等于同一 `turn_ref` 在 `claimed` 里给过的那个值——**这条比对在 Core 侧 `TurnTracker.Dispatch` 做，不在 `StreamFrame.Validate`**（原校验里那句 "has no provider_execution_id (CO-8…)" 属于裁定前口径，已作废） |

**四条语义，一条都不能丢**：
1. **`claimed` 故意不带 `turn_id`** —— "接下了"不等于"开始了"；**唯一的开始证明是 `dispatched`**。
2. **没有 `dispatched`，Core 不许把状态显示成 running**（这正是 §12"不得把 UNKNOWN 显示成确定值"在本层的落点）。
3. **`provider_execution_id` 必须挂在 `claimed` 上**，因为 **Core 可能在 claimed-未-dispatched 这个窗口里死掉**；
   Recovery 要先拿它去问 Provider，**才允许**把这轮判成 `UNKNOWN`。少了这个字段，崩溃恢复就只能猜。
4. **这两帧是 `execution_id` 唯一允许出现的地方**（其余四条通知**只有** `turn_ref`；而按 12:4x 裁定，**这两帧也一律带 `turn_ref`**，见下方 ✅），
   代价是 §4 的 `session/send` 必须把 `execution_id` 交给 Adapter（CO-24）——
   这条例外要承认它存在，别假装 §6 规则 5 是绝对的。

**✅ 两处"注释与代码相反"已由主控裁定（12:4x）——不必再等回答；adapter 若不同意给理由即可，别沉默改代码**：

1. **`turn_ref` 一律必带**，包括 `turn/claimed`/`turn/dispatched`。归属规则收成一条无例外：
   **所有 Adapter→Core 通知都带 `turn_ref`；`claimed`/`dispatched` 额外再带 `execution_id`。**
   两者同时在场时 **Core 必须校验一致**，不一致 = `丢弃 + Audit`（沿用 §6 规则 5 的处置）。
   理由：与其保留"这类靠 A 归属、那类靠 B 归属"的例外清单（今天已经数不出统一答案），
   不如让归属只有一种写法，并把多带一个字段变成**可交叉验证的冗余**，而不是第二真相源。
   → `stream.go` 那句"carry no turn_ref"要改，载荷注释与字段注释归一。
2. **`provider_execution_id`：`claimed` 必填不变；`dispatched` 改为"可空，但不得与 `claimed` 的非空值冲突"**。
   CO-8 要它挂 `claimed`，是因为 **Core 可能死在 claimed-未-dispatched 窗口**，Recovery 必须先有个能问的名字；
   而 CO-13 承认**有的 Provider 到 dispatched 时给不出名字** —— 此时强制非空，真 Adapter 就发不出 `dispatched`，
   于是**那一轮永远不显示 running**。这个失败模式比"缺一个字段"严重得多（且直接撞 §12：不许把"看不见"演成确定状态）。
   所以 `dispatched` 允许空，**前提是同一 `turn_ref` 的 `claimed` 已给过非空值**；给了非空就必须等于 `claimed` 那个值。
   → **这条是代码要改**（现在 `StreamFrame.Validate` 对 dispatched 空值直接 `INVALID_PARAMS`），
   且校验点落在 **Core 侧 tracker**（"这条 turn 是否已拿到过名字"），不是让 Adapter 自己保证。

#### 判据表 T84（§4.2 两帧取证信号；从 `core/test/turn_evidence_test.go` 镜像进本文，2026-09-22）

14:05 逐条实跑标定，除 T84-7 外全 `--- PASS`（命令一律带 `-v`：`go test -run` 空匹配也报 `ok`）。
被测面是 Core 侧导出接缝（`TurnTracker` / `CarriesTurnRef` / `NewStream`，以及 `process.Supervisor` + 真 FakeAdapter），
**一条期望值都不从 `stream.go` 的实现里抄**。T84-4/T84-5 是下面两条裁定的转录，改名过一次（历史见 `test` 的 progress）。

| ID | 断言（两侧都要断） | 当场命令 | 状态 / 归属 |
|---|---|---|---|
| T84-1 | `claimed` 不给 `turn_id`，`dispatched` 才给 ⇒ `TurnID()` 前空后有 | `go test -count=1 -v -run TestClaimedCarriesNoTurnID ./test/...` | 现存（PASS）/ test |
| T84-2 | 没有 `dispatched` 不许显示 RUNNING：8 个形状里**恰 1 个**可达 RUNNING（正例），其余逐条判"不等于 RUNNING"；错误面也两侧判（该拒的 4 个不许静默收下、不该拒的 3 个不许乱拒） | `go test -count=1 -v -run TestRunningRequiresDispatch ./test/...` | 现存（PASS×8 子用例）/ test |
| T84-3 | `provider_execution_id` 必须挂在 `claimed` 上，**含 `accepted=false` 的拒绝路径也要留下两个 id**；空值被拒且不许留下"看起来有名字"的假证据 | `go test -count=1 -v -run TestClaimedKeepsProviderHandle ./test/...` | 现存（PASS）/ test |
| T84-4 | 裁定第 1 条：**六种通知一律必带 `turn_ref`**（缺 ⇒ `INVALID_PARAMS`、带 ⇒ 成帧，逐个判） | `go test -count=1 -v -run TestTurnRefMandatoryOnEveryNotification ./test/...` | 现存（PASS）/ test |
| T84-5 | 裁定第 2 条：`dispatched` 的 `provider_execution_id` 可空（前提 `claimed` 给过非空）、给了非空就必须等于它；异名 ⇒ UNKNOWN。全打在 `TurnTracker` 上 | `go test -count=1 -v -run TestDispatchedHandleOptionalButNeverContradicts ./test/...` | 现存（PASS×3 子用例）/ test |
| T84-6 | 裁定第 1 条的另一半：票号绑定与帧内 `execution_id` **一致 ⇒ 收下并推进**；**不一致 ⇒ 丢弃**（两个 tracker 都不动、假 id 的帧 0 条转发） | `LUCERO_TEST_FAKE_ADAPTER=1 go test -count=1 -v -run TestTurnRefCrossCheck ./test/...` | 现存（PASS×2 子用例）/ test（`turn_attribution_test.go`） |
| T84-7a | **丢弃必须到达 `Recorder` 接缝**（裁定第 1 条"丢弃 + Audit"的前半格）：一次被丢弃的帧，要带着**帧内 `execution_id` 与票号绑定 `execution_id` 两个不同值**原样交到注入接缝上，不许先互相校正 | `go test -count=1 -v -run '^TestDroppedFrameReachesAuditSeam$' ./test/`（`test` 的，跨包真子进程那条）；另有 `adapter` 包内那条 `go test -count=1 -v -run '^TestDroppedFrameReachesTheAuditSeam$' ./internal/process/` | **现存（两条都 PASS：`test` 那条我 09-23 00:03 复跑、`adapter` 那条 09-22 23:30:56 复跑；实测 2 条 `DroppedEvent` = `turn/claimed` + `turn/dispatched`，两个 id 值不同都在）/ test + adapter 各一条**。<br>**⚠ 这两条名字只差一个 `The`，而 Go 的 `-run` 是无锚定子串匹配** —— 写 `-run TestDroppedFrameReaches` 会同时命中两个、把归属记错人。所以本行命令一律**锚定全名 + 写全包**（通用口径见契约① §1.1 第 15 条，REQ-T15 采纳）。 |
| T84-7b | **一次丢弃落一行审计、且 `event_journal` 零新增**（裁定第 1 条"丢弃 + Audit"的后半格 —— 真那个 Audit） | 暂无可跑判据：`cmd/core` 里还没有 `process.Recorder` 的实现（09-23 00:02 全仓非测试码 `func .*) RecordDrop(` 仍 0 命中）| **SKIP（blocked）** / test 落判据、backend 给实现（REQ-T12）。**显式 SKIP 而不是不写**：`test` 已把断言形状写好并挂在阻塞条件上（同 T-141 那条"前提失败要让子用例显式 SKIP，不许静默消失"）。实现侧约束照旧：审计行必须走 `audit` 自己的出口，出现 `Tx.Append("audit_entries"` 表名字面量即 W13-2 红。<br>**新增一条断言（09-23 00:2x，随契约③ §7.2 的 `ProcessRole` 裁定）**：审计行的 `details_json` **必须能指出是哪个 Adapter**（`adapter_key` 或 `owner_id`，由持有配置的装配方填），**不许留空串冒充"知道"** —— `DroppedEvent` 里那个从未被喂、也没有落点的 `ProcessRole` 已裁定删除，身份信息的正解是走审计行、不是走跨接缝的事件结构。 |

**本表即权威**：`test` 的文件头注释换成指向本节的指针（与判据表 MS/F20 同一做法），本文件之外不再留第二份。


#### 判据表 FL（间歇红；`test` 用 `scripts/flaky_watch.sh` 实现，09-21 15:46 定，2026-09-22 镜像进本文）

一句话口径：**只在 `./...` 全跑时出现、单跑不复现的失败记为 flaky 并带采样时刻，既不计绿也不计回归。**
四档采样（缺一档就分辨不出并发形状）：`single` 单独跑一个包一个用例 / `shuffle` 同二进制内
`-count=2 -shuffle=on` / `isolated` 多包一起跑但 `-p 1`（去跨包并发、留包内 shuffle）/
`parallel` 不限 `-p`（即 `go test ./...` 的默认并行）。
**每次采样前取一次 `date`；判的是 `-run` 命中的那行 `--- PASS/FAIL/SKIP`，不判包级 `ok`**——
`-run` 是前缀匹配且不带 `-v` 时 SKIP 看不见、包照样报 ok。**全量采样归主控**（worker 的 build/test 禁令不变）。

| ID | 断言 | 当场命令 | 状态 / 归属 |
|---|---|---|---|
| FL-1 | 只有 `parallel` 红、`isolated` 全绿 ⇒ 判 **FLAKY（跨包并发）**，带两次采样时刻 | `bash scripts/flaky_watch.sh -run <用例名> -p <包> -n 2` | 现存（分类器由 `--selftest` 反向验，不靠真红一次）/ test |
| FL-2 | `single` 也红 ⇒ 判 **REGRESSION**（稳定复现），派归属角色，**不许标成 flaky** | 同上 | 现存 / test |
| FL-3 | `shuffle` 红而 `single` 绿 ⇒ **FLAKY（同二进制并发）**：同包内共享的东西（包级变量、临时资源计数）才是方向，跨包端口不是 | 同上；最小复现手段 = 同包 `-shuffle=on` + `-count=2` | 现存（09-21 16:10:54 第一次由实测落地：`cmd/core` 两跑全红、无 flag 绿，见账本 09-21 16:10 段）/ test |
| FL-4 | `parallel` 与 `isolated` **都红** ⇒ **FLAKY（未定维度）**：不是"一起跑"造成的，方向要回到共享面枚举 | 同上 | 现存 / test |
| FL-5 | 四档都不红 ⇒ **未复现**，只报采样次数与时刻，**不得写成"已绿/已修"** | 同上 | 现存（09-21 15:57 与 09-22 15:11 两次全跑均落到这一档）/ test |
| FL-6 | 任一采样没拿到该用例的 `---` 行（编译不过 / 用例改名）⇒ **探测不成立**，exit 2 并拒绝下结论 | `bash scripts/flaky_watch.sh --selftest` 里喂合成计数验 | 现存 / test |
| FL-7 | 共享面必须**可枚举**，不许一个一个猜：`core.lock` / `core.endpoint` / `audit.head` / 真子进程端口 / **同批包里被改到的共享契约** | 脚本末尾自动列（数 `cmd/core/*_test.go` 的 `Config{` 与 `DataDir:`/`t.TempDir()` 是否相称 + 三个文件名引用处） | 现存 / test |
| FL-8 | **前置档，比 FL-1~FL-5 优先**：记 flaky 之前先核时间线 —— 失败时刻往前 `-s`（默认 120）秒、往后 90 秒内，**全仓**（`core/`+`specs/`+`scripts/`）有任何 .go 的 mtime 落进窗口 ⇒ 这次观察**不算证据**，既不计 flaky 也不计回归；判决必须报扫过的文件数 | `bash scripts/flaky_watch.sh -a 2026-09-21T15:29:31+08:00` | 现存（那次实测命中：`core_test.go` 失败后 54s、`rejections.go` 失败后 58s 被改写）/ test |

**FL-7 为什么要列第五项**：`cmd/core` 曾经那两跑全红的根因是 `internal/audit` 改了字段形状，
连带打红别人包里的用例——**那不是文件级共享，是跨包契约改动的连带**。只列四个文件名会漏掉一整类。





## 5. 三态投递原语（Phase 0 §6.4 的直接结论）

`session/send` 的 `delivery` ∈ **`start_now` / `guide` / `queue`**，是协议一等概念：

| 态 | 语义 | 四家独立证据 |
|---|---|---|
| `start_now` | 立即开始一轮 | 各家默认 |
| `guide` | 当前轮进行中转插话，不新开轮 | ZCode `delivery=startNow` 之外；Qoder steering |
| `queue` | 排队，等当前轮结束 | ZCode `queue`；Codex `codex queue --thread`；OpenCode `prompt_async` |

- Adapter **不支持某态时必须显式返回 `DELIVERY_MODE_UNSUPPORTED`**，严禁"降级成 queue 但报告说按我说的做了"。
- 这个错发/错态属 §12 P0 的"消息错发到错误 Session"同类事故，因为用户以为在插话实际在排队。
- Task/Execution 侧不新增状态：三态是 Execution 的参数（`trigger_type` 保持原枚举不变）。

## 6. 流式契约（token 级是可选的，turn 级是必须的）

Adapter→Core 通知：

| 通知 | 用途 |
|---|---|
| `stream/delta` | `{turn_ref, sequence_no, channel, text, provider_raw}`；`channel ∈ ASSISTANT/THOUGHT/TOOL/STATUS` |
| `stream/item` | 一个完整条目（工具调用/结果）就绪 —— **同样带 `turn_ref`** |
| `stream/state` | `{turn_ref, normalized_status, provider_status_raw, confidence}` |
| `turn/finished` | `{turn_ref, result_code, usage?, provider_raw}` —— **唯一的强制收尾信号** |

规则：

1. **只承诺 `turn/finished` 是必须的**。`stream/delta` 可以完全没有——
   Phase 0 §6.2 实测 **Qoder 真流式 UNSUPPORTED**，协议必须容得下"只有 turn 粒度"的 Provider。
2. `sequence_no` 由 Adapter 单调递增维护；断线重连后 Core 用 `since_seq` 补齐，
   重复帧按 `sequence_no` 幂等丢弃。**幂等键是 `(turn_ref, sequence_no)`，不是光 `sequence_no`**（见规则 5）。
3. Core 侧按 250–500 ms 或达到字符阈值批量 flush 进 `execution_output_chunks`（领域模型「流式输出」规则），
   **每个 delta 绝不写 event_journal**。
4. Adapter 不写 DB、不发事件；`stream/delta` 不是 Durable Event，掉电即丢，靠 chunk 表兜。
5. **`turn_ref`（CO-21 裁定，13:5x；12:45 由 §4.2 裁定① 扩到** ***全部*** **六条 Adapter→Core 通知）**：`stream/delta`/`stream/item`/`stream/state`/`turn/finished` 之外，`turn/claimed` 与 `turn/dispatched` **也一律带 `turn_ref`**，缺任一帧即协议违规。（本条原文只写"四条"，与 §4.2 裁定① 相反，14:4x 由巡检逐行读出、我复核后改正——**归属只有一种写法，不留例外清单**。）
   起因是我这边的两处不自洽，不是 adapter 实现问题：`000001` 迁移里
   `execution_output_chunks` 有 `UNIQUE(execution_id, sequence_no)`，而本节原来**只给 `sequence_no`**，
   全篇又没有 `correlation` 字段（13:4x `grep -n correlation` 在本文 **0 命中**）——
   一条长连接上跑多个 execution 时，Core 拿到一个裸 `sequence_no` **无法判定它属于谁**，只能猜
   "该连接唯一一个在飞的就是它"，那会在多会话并发时把两个 execution 的输出混进同一行键。

   定形：**`turn_ref` 是 Core 自己发出的关联串（就是那次请求的 JSON-RPC `id` 的字符串形式），
   Adapter 的职责只有"原样回显"，不理解也不生成它。**
   - **为什么不直接把 `execution_id` 放进帧**（这是更省事的做法，但否掉）：那是 Core 的内部身份，
     把它交给外部进程等于**让 Provider 侧的日志/落盘里出现我们的主键**，
     Codex 的 rollout JSONL、OpenCode 的 `opencode.db` 都可能把它写下来；
     而且真 Adapter 根本不知道 Core 的 `execution_id`，只能靠 Core 事先塞给它——那等于要求每个 Adapter 维护一份我们的状态。
     **回显一个我们发的字符串**成本一样低，语义却是"你的票号"，不是"你的身份"。
   - Core 侧必须有 `turn_ref → execution_id` 的映射，且**只在本次连接生命周期内有效**；
     收到未知 `turn_ref` 的帧 → **丢弃 + Audit**（不许"归给当前唯一在飞的那个"），
     这条与 §8 崩溃语义同源：宁可丢一个流式帧，不可串两个 execution 的输出。
   - 断线重连后 `since_seq` 也要按 `turn_ref` 分别补，不同 execution 的游标互不相干。
   - **FakeAdapter（§10）必须测这一条**：一条连接上并发两个 execution，各自的 delta 只进自己的 chunk 行。

## 7. 额度契约（UNKNOWN 与 0 是两回事）

```jsonc
{"observations": [
  {"quota_key": "weekly", "quota_type": "PERCENT", "observation_kind": "OBSERVED",
   "used_percent": 62.5, "remaining_percent": 37.5,
   "reset_at": 1789900000000, "window_start": null, "window_end": null,
   "confidence": "HIGH", "source": "app_server/GetAccountRateLimits", "provider_raw": {}}
], "overall_confidence": "HIGH"}
```

- 任一字段取不到就 `null` + 降低 `confidence`；**整个读不到就返回 `observations: []` 且 `overall_confidence: UNKNOWN`**。
- **严禁用 0 表示"没有额度数据"**。Phase 0 实测 Qoder 的 `message.usage` 字段齐全但值全为 0——
  那是 `UNKNOWN`，不是"用完了"。这是 §12 P0 的直接实例，`adapter` 与 `test` 都要有对应用例。
- 预测值一律 `observation_kind: ESTIMATED`，不得进 `OBSERVED`。

## 8. 崩溃与进程隔离

- Adapter 进程**必须**由 Core 的 Process Supervisor 拉起并登记 `process_instances`
  （`process_type=ADAPTER`、`owner_type=adapter`、`pid`、`restart_policy`）。
- 崩溃后果按"影响最小"原则：**只影响该 Adapter 上的 in-flight Execution**，一律转 `UNKNOWN`
  （**不是 `FAILED`**——我们没有证据说它失败了），由 Recovery Engine 结合 lease/heartbeat/
  `provider_execution_id` 判 reconnect / resume / interrupt。
- 重启退避 `1s, 2s, 4s, 8s, 上限 30s`；连续 5 次崩溃 → `health=UNAVAILABLE` + 停止自动重启 + 发 Notification。
- Adapter 崩溃**绝不允许**导致 Core 退出；Core 退出**绝不允许**损坏 SQLite。这两条各有回归测试。

## 9. 危险开关的 gating

- `debug/*` 能力（§10）只有 `support_level=EXPERIMENTAL` 且 `adapter_key=fake` 才允许声明；
  Core 在握手阶段就拒收真实 Adapter 的 `debug.*` 声明并记 Audit。
- 任何"绕过审批/沙箱/权限"的原生开关——实测到的有 WorkBuddy `--auth none`（其 help 自述允许任意本地进程
  经该服务执行命令与读写文件）、Codex `--dangerously-bypass-approvals-and-sandbox`、
  ZCode/WorkBuddy 的 `bypassPermissions`——**一律不得由编排或自动重试启用**。
  Adapter 的参数字段走白名单，命中即拒绝并记 `audit_entries`（`outcome=DENIED`）。

## 10. FakeAdapter 规格（First Sprint 验收对象）

**落点裁定（CO-1，2026-09-19）**：FakeAdapter 源码住 **`core/cmd/fake-adapter/`**（独立 `main` 包），
由 Core 的 Process Supervisor **作为真子进程拉起**；`adapters/fake/` 只放 fixture 与说明文档。
两条理由：① Go 的 `internal/` 规则使 `adapters/fake`（在 `core` module 之外）无法被 `core/internal/adapter`
import，这是 `adapter` 角色实测到的硬约束；② **更重要**：只有跑真进程，才测得到 §8 那条 P0
「Adapter 崩溃不得带崩 Core」。同进程 import 会把隔离性整个测没了。需要第二个 module 或 `go.work` 的方案一并否掉。

必须实现：`adapter/initialize`、`adapter/health`、`session/list`、`session/read`、`session/send`
（三态全支持）、`session/stop`、`model/list`、`quota/read`、`approval/list`、`approval/respond`、`stream/*`。

故障注入（`debug/*`，EXPERIMENTAL）：

| 方法 | 行为 |
|---|---|
| `debug/fail` | 设定下一次（或第 N 次）调用返回指定错误码 |
| `debug/crash` | 立即 `os.Exit(2)`，用于"Adapter 崩溃不能带崩 Core"的回归 |
| `debug/hang` | 挂起不返回，用于测 deadline 与 `session/stop` |
| `debug/unknown_quota` | 让 `quota/read` 返回 UNKNOWN，验证 0 与 UNKNOWN 不可混同 |

FakeAdapter **不许**碰 Core 私有结构：只走本协议；只读自己 `.scratch/` 下的 fixture 状态。

## 11. 本契约需要回写事件目录的两项

`specs/events/event-contract-v0.1.md` §6 需补登：`adapter_downgraded`、`adapter_restart_exhausted`。
（发现于本契约起草；属新增事件类型，不改既有事件语义。）

## 12. Phase 0 六条设计影响的落地对照

| Phase 0 结论 | 落在本契约 |
|---|---|
| 6.1 「每家都能被程序化驱动」不成立 | §2 `write_entry:false` 合法 + §3 UIA 一等 |
| 6.2 流式/额度并非人人有，Qoder 五项全缺 | §6 只强制 `turn/finished` + §7 UNKNOWN≠0 |
| 6.3 会话正文不一定在本地；CLI 空集返回纯文本 | §4 `session/read` 必须可返回 `LOCAL_DATA_UNAVAILABLE`；§10 fixture 处理文本哨兵 |
| 6.4 五家只有 3 个本地数据形制族；三态投递独立出现 | §5 三态一等原语；LOCAL_DATA reader 只需 3 个（工作量结论，非协议条款） |
| 6.5 Codex 协议可机器读取（379 schema） | §13 版本契约测试 |
| 6.6 跨家"绕过审批"开关 | §9 白名单 + 禁止自动启用 + 必记 Audit |

## 13. 版本契约测试（Codex 专属，Phase 0 §6.5）

Codex Adapter 的 CI 步骤：跑 `codex app-server generate-json-schema --out <tmp> --experimental`，
与仓库内已登记的 schema 摘要做 diff。差异即视为"能力面变更"，要求重新核定 `capabilities` 与
`last_verified_at`，并在 PR 里显式记录新增/删除/改名。目的：把"厂商升级悄悄改变能力"变成可检测事件，
而不是等运行时炸。

## 15. 错误码集合（REQ-T-3 补：此前 §1/§3 两次引用本节却从未写出来）

**命名分裂是故意的，不要"统一"**：Adapter 层（JSON-RPC `error.data.code`）用**大写蛇形**，
HTTP 层（契约③ §2 错误体 `error.code`）用**小写蛇形**。它们是两套协议的各自方言，
强行合并只会把映射关系藏进字符串里。映射表在 §15.2，`frontend`/`test` 一律以本表为准，
不得在 yaml 里放开成 free string。

### 15.1 Adapter 层码

| 码 | 含义 | 可重试 |
|---|---|---|
| `PROTOCOL_NOT_INITIALIZED` | 未完成握手就调任何方法（`initialize` 自身除外，见 §2） | 否 |
| `PROTOCOL_VERSION_MISMATCH` | 握手 `major` 不符 | 否 |
| `CAPABILITY_UNAVAILABLE` | 该能力当前无效（等级降级/未声明） | 视 health |
| `WRITE_ENTRY_NONE` | 该 Adapter 声明 `write_entry:false`，任何写方法合法拒绝 | 否 |
| `DELIVERY_MODE_UNSUPPORTED` | 三态投递里某态不被支持（**禁止静默降级**） | 否 |
| `LOCAL_DATA_UNAVAILABLE` | 读到了 Provider 但本地正文取不到（Phase 0：WorkBuddy 桌面端） | 视源 |
| `SESSION_NOT_FOUND` | `remote_session_id` 不存在/无权见 | 否 |
| `AUTH_REQUIRED` | 需要用户在自己客户端完成登录（**不得代登录**） | 否 |
| `RATE_LIMITED` | 供应商侧限流/额度耗尽，`data` 带 `retry_after_ms` 与 quota 快照 | 是 |
| `DANGEROUS_FLAG_REFUSED` | 参数命中危险开关白名单外（`--auth none`、`bypassPermissions` 等，见 §9） | 否 |
| `CANCELLED` | 收到 `$/cancel` 后确认中止 | — |
| `TIMEOUT` | 超 `deadline_at`（含 §10 `debug/hang` 场景） | 是 |
| `PROVIDER_ERROR` | 供应商原始错误透传，`data.provider_raw` 必须原样保留 | 视情 |
| `INTERNAL` | Adapter 自己的 bug，必须同时进 `stderr` 与 Audit | 否 |
| `READ_ONLY` | **目标对象**（某会话/某执行）当前只读：它由只读链（`LOCAL_DATA`）发现，这一轮没有写入口。与 `WRITE_ENTRY_NONE` 的分界是"整个 Adapter 声明无写入口" vs "对象级/本轮不可写" | 视链 |
| `ADAPTER_UNRESPONSIVE` | 进程存活但请求/心跳在 `deadline_at` 内无任何响应，Supervisor 已把 health 降级（应同时产生 `adapter_downgraded` 事件）。与 `TIMEOUT` 的分界：`TIMEOUT` 是**这一次操作**超期，本码是**这条连接**不健康 | 是（等恢复） |
| `EXECUTION_STATE_UNKNOWN` | 该 Execution 的真实状态无法确认（§7 崩溃语义：`UNKNOWN` 是合法态，**不是** `FAILED`），故拒绝一切"推进/取消/发消息"类请求 | 视证据 |

#### 15.1.1 数字码与生产方（13:2x 折入 `adapter` 交表，本文即唯一真相）

来源：`core/internal/adapter/protocol_test.go` 的 `codeTable`（13:22 实读）。**这张表的意义是"谁能发、谁必须有常量"**：
`Core:*` 行必须有 Go 常量；`Adapter` 行是 Core **只识别不生产**的词汇（`Symbol()` 优先读对端 `data.code`），
**给它们造没人发的常量就是投机代码**；`result-frame` 行永不上 wire。

| 数字码 | 码 | 生产方 | 有 Go 常量 |
|---|---|---|---|
| `-32000` | `PROTOCOL_NOT_INITIALIZED` | `Core:internal/adapter` | ✓ |
| `-32001` | `PROTOCOL_VERSION_MISMATCH` | `Core:internal/adapter` | ✓ |
| `-32002` | `CAPABILITY_UNAVAILABLE` | `Core:internal/adapter` | ✓ |
| `-32003` | `WRITE_ENTRY_NONE` | `Core:internal/adapter` | ✓ |
| `-32004` | `DELIVERY_MODE_UNSUPPORTED` | `Core:internal/adapter` | ✓ |
| `-32005` | `LOCAL_DATA_UNAVAILABLE` | `Adapter` | ✗ |
| `-32006` | `SESSION_NOT_FOUND` | `Adapter` | ✗ |
| `-32007` | `AUTH_REQUIRED` | `Adapter` | ✗ |
| `-32008` | `RATE_LIMITED` | `Adapter` | ✗ |
| `-32009` | `DANGEROUS_FLAG_REFUSED` | `Core:internal/process` | ✗ |
| `-32010` | `CANCELLED` | **`result-frame`**（§15.3 规则 5，不发 `error`） | ✗ |
| `-32011` | `TIMEOUT` | `Adapter` | ✗ |
| `-32012` | `PROVIDER_ERROR` | `Adapter` | ✗ |
| `-32013` | `INTERNAL` | `Core:internal/adapter` | ✓ |
| `-32014` | `ADAPTER_UNRESPONSIVE` | `Core:internal/process` | ✓ |
| `-32015` | `EXECUTION_STATE_UNKNOWN` | `Core:internal/process` | ✗ |
| `-32016` | `READ_ONLY` | `Adapter` | ✗ |
| `-32601` | `METHOD_NOT_FOUND` | `Core:internal/adapter`（**协议层**） | ✓ |
| `-32602` | `INVALID_PARAMS` | `Core:internal/adapter`（**协议层**） | ✓ |

三点读法：
1. **原来占住 JSON-RPC 预定义码的三处越界已修**：`PROTOCOL_NOT_INITIALIZED` 从 `-32600` 迁到 `-32000`，
   `WRITE_ENTRY_NONE` 从 `-32601` 迁到 `-32003`，`CAPABILITY_UNAVAILABLE` 从 `-32602` 迁到 `-32002`。
2. **T-36④(c) 那个"同一概念两套表达"的问题，adapter 的解法比我预期的好**：`METHOD_NOT_FOUND`/`INVALID_PARAMS`
   保留在**标准数字码** `-32601`/`-32602` 上（协议层专用），**不再**另设 `-32001`/`-32002` 的领域副本 —— 重复表达消失了。
3. ~~一处待确认~~ **13:5x 已由 adapter 定并写进 `protocol.go` 注释（我折进本表）**：
   `ADAPTER_UNRESPONSIVE` 与 `EXECUTION_STATE_UNKNOWN` 的生产方是 `Core:internal/process`
   （Supervisor 判连接不健康 / 崩溃后状态不可确认），但**常量统一住在 `internal/adapter`** ——
   取的是"词汇由协议包统一持有、由 process 触发"这一种。因此本表的"有 Go 常量"列指的是
   **`internal/adapter` 里有没有这个名字**，不是"生产方包里有没有"；`test` 的 check 8 要按这个读，
   否则会判出一条根本不存在的错。

**`test` 的 check 8 判据据此定稿**（替掉 T-41④ 里的临时口径）：**两表行数不相等是设计如此**——
§15.1.1 = 19 行、§15.1 = 17 行，差集**必须恰好是** `METHOD_NOT_FOUND`/`INVALID_PARAMS` 这两个协议层行
（13:2x 实测：`comm -13` = 这两个、`comm -23` = 空；命令与判据一起写进你的用例，别硬编码 17==19）。
其余逐条核：**§15.1 的码集合 ∈ §15.1.1**（反向包含，不是相等）、**数字全部落在 `-32000`–`-32099` 或标准协议层码**、
**只有 `Core:*` 且"有 Go 常量=✓"的行要求在指定包里有常量**；`Adapter`/`result-frame` 行**不得**要求常量（要求了就会天天误报）。

#### 判据表 C8（`test` 逐行解析用；命令里的区间锚点用**标题文本**，不用行号）

锚点写法固定：`/^### 15.1 /`→`/^#### 15.1.1/` 取 §15.1 的码表；`/^#### 15.1.1/`→`/^### 15.2/` 取本表；
`/^### 15.2/`→`/^### 15.3/` 取 HTTP 映射表。**这些锚点改标题措辞时要同步本表**。

| ID | 断言 | 当场命令 | 期望 |
|---|---|---|---|
| C8-1 | §15.1 的码 ⊆ §15.1.1，且差集恰为 `METHOD_NOT_FOUND`/`INVALID_PARAMS` | 两区间各取首列反引号码名排序后 `comm -13`（15.1→15.1.1 方向）与 `comm -23` | 前者两个名字、后者空 |
| C8-2 | §15.1.1 的数字全部落在 `-32000`–`-32099`，或等于 `-32601`/`-32602` | 取本表首列数字，逐个判 `-32099 ≤ n ≤ -32000 \|\| n==-32601 \|\| n==-32602` | 19 行全成立、0 例外 |
| C8-3 | **只有**生产方为 `Core:*` 且"有 Go 常量=✓"的行才要求常量 | 对这类行的码名做**全仓**检索：`grep -rn "\"<码名>\"" core/internal/adapter/`（常量表 `codeNames` 是 `protocol.go` 里的 map 字面量） | 每行命中 ≥1；`Adapter`/`result-frame` 行**不得**出现在这条检查里 |
| C8-4 | §15.1 每个码都出现在 §15.2 的『来自』列，且 §15.2 除 `version_conflict` 外每个来源都在 §15.1 | 两表码名集合双向 `comm` | 两个方向都空<br>**⚠ 本行的"除 `version_conflict`"已被 C8-7 取代**（15.2.1 收编后有 12 行是 `—`，按名字豁免会自相矛盾）。保留本行只为记历史，**执行按 C8-7 的形状判据** |
| C8-5 | `CANCELLED` 不作为 JSON-RPC `error` 出现（只走结果帧） | 对 FakeAdapter 的 `$/cancel` 路径断言响应里**没有** `error` 字段；同时 `grep -rn '"CANCELLED"' core/cmd/fake-adapter/` 不得出现在构造 error 帧的位置 | 违反即红 |
| C8-6 | 正向：§15.2『来自』列每个反引号码名都必须 ∈ §15.1 码列 | `go test -count=1 -v -run TestHTTPErrorCodesComeFromAdapterSet ./test/...` | 现存（PASS）/ test |
| C8-7 | 反向：§15.1 每个码都要有 HTTP 映射行；**例外按形状判**——只有『来自』以 `—` 开头的行不参与，不点名放行 `version_conflict` | `go test -count=1 -v -run TestAdapterCodesHaveHTTPMapping ./test/...` | 现存（PASS）/ test |
| C8-8 | **解析器自证**：§15.2 必须解析出 29 行 = 17 带码名 + 12 以 `—` 开头，且两者之和 = 总行数；§15.1 码列 17 个。任一不符 ⇒ 判判据自己坏了（红），**不许拿半张表去判人**。锚点用标题文本（`### 15.1 `→`#### 15.1.1`、`### 15.2`→`### 15.3`），锚点找不到或区间没有终点也报同一种红 | `go test -count=1 -v -run TestErrorCodeContractTablesParse ./test/...` | **已作废，被 C8-10 取代**（同一个用例符号 `TestErrorCodeContractTablesParse`，`test` 09-22 23:46 已改判形状、我 23:50:29 复跑 PASS）。本行只留作历史：**它把 29/12 写成了字面量，而 22:5x 我收编 `duplicate_project_path` 之后真实是 30/13 —— 它当场变红，红得对，代价是"每次合法扩列都要人来改一次数字"**。判据要钉形状（C8-10），不要钉基线快照。 |
| C8-10 | **闸门判形状、不判字面行数**：**①′ 总行数 = 码名数 + `—` 数，且"长得像表格行却没被计入任何一类"的行数必须 = 0**（`test` 的探针 M-C 证明：只写求和式碰不到"整行没打反引号"这种失效 —— 那种行会让三个数**一起少**、等式照样闭合）；②码名集合与 §15.1 码列**双向相等**；③`—` 数 ≥ 1 且总行数 ≥ 20（防解析器静默空集，同 C8-8 想防的那件事）；④三个数字照常打印，漂移看得见但不咬人 | `go test -count=1 -v -run '^TestErrorCodeContractTablesParse$' ./test/` | **现存（PASS，09-22 23:53:44 主控复跑，非转述；原文：`形状自证：§15.1=17 码、§15.2=30 行（码名行 17 / `—` 13，去重码名 17），双向相等。数字只跟趋势`）/ test** ⇒ **本行取代 C8-8**（同一个用例符号，判据已从字面数改成形状）。命令列写法按契约① §1.1 第 15 条统一为"锚定名 + 包路径" |
| C8-11 | **线格式 ↔ 契约的那条腿**：`core/api/http/**` 里每一个 `writeError(..., "<code>", ...)` 的字面码必须 ∈ §15.2 左列（排除 `_test.go`）。**这一条才是真正会漏事的**：`duplicate_project_path` 是 backend 22:4x 落地 `projects.go` 时才出现的第 13 个 transport 码，我 15:1x 那次收编按定义不可能预见到它——只有这条判据能让它当天就红 | `go test -count=1 -v -run TestTransportCodesAreInContract ./test/...` | **现存（PASS，23:50:29；原文：`扫 5 个生产 .go、15 个字面码全部在 §15.2 的 30 个码里；非字面量传码 0 处`）/ test**。<br>**它自己声明的盲区要一起读**：码若以**变量**而不是字面量传进 `writeError`，这条看不见 —— 它今天数到 0 处并把这句话打出来，**"看不见"这件事本身留了痕**，比默默漏掉强。'（同 §7.1 W13 那条老教训："按被调用函数的字面形态查"会随重构静默失效。）<br>'
    '**REQ-T15 那个口径我裁了（09-23 00:1x）：不升成红。** 把码改成常量/变量传进来是**合法 Go 风格**，'
    '为它开红等于惩罚一次合法重构，而真正的不变量是"能上线的码 ⊆ §15.2"——**这件事用类型收口比用 lint 收口强**：'
    '若哪天 `writeError` 改收一个具名码类型、常量集中在 `protocol.go`，闭集就成立-by-construction，不需要 grep。'
    '**重开条件写明**：一旦"非字面量传码"从 0 变成 ≥1，就动类型（或明写例外），**不要**回来把本行改成红。'
    '今天 0 处，且这个 0 是被打印出来的 —— 保持打印。 |'
| C8-12 | **事件目录 ↔ `eventjournal` 常量双向相等**：事件契约 §6 登记的 `event_type` 集合与 `core/internal/eventjournal/events.go` 导出的常量集合双向相等（缺哪边补哪边，不许一边兼职）。**方向**：C8-11 判"代码会发的码 ⊆ 契约"，本行判"契约登记的类型 ⊆ 代码有的常量"，两条合起来才是词表闭合 | `go test -count=1 -v -run TestEventCatalogMatchesJournalConstants ./test/...` | **现存（PASS，23:50:29；原文：`目录 62 类 / 常量 62 个（扫 1 个非测试 .go）；目录独有 []、代码独有 []`）/ test**。<br>**"怎么测"必须是两条腿，一条对拍 + 一条直接打在解析器上（09-22 `db` 实测，比"我的用例会红"值钱）**：`db` 把展开器第⑤条**改回静默跳过**之后，`TestCatalogMatchesContract`（62 个类型双向对拍）**仍然 PASS** —— 因为当天的表里根本没有孤儿 `_xxx` 可丢 ⇒ **对拍只看得见"两边不一致"，看不见"两边同时漏掉同一个东西"**。真正咬住的是它另加的 `TestEventTypesInExpansion`（把合成一行喂给纯判定函数，断它必须报错、必须展开出 N 个名字）。<br>**与 `db` 包内那条的分工（`test` 给的精确版，我 09-23 00:02 两边都核过）**：`db` 的 `TestCatalogMatchesContract` 比的是 `events.go` 里那张 **`catalog` map**，本行比的是 **`const` 声明** ⇒ **一个常量声明了却没进 map、或进了 map 却没声明，两条判据各抓一半**，所以不重复、也不许合并。<br>**我原先预言"本行第一天会红在 `notification_read` 上"没有发生，而且不是因为判据钝**：`db` 23:10 除了 `NotificationRead` 还顺带补齐了 `task_paused`/`task_blocked`/`task_resumed`/`execution_paused`（我 grep 到 `events.go:31/32/33/46/60` 与 `true` 集合的 `:83/:88/:94`），`test` 的 M-F/M-G/M-H 三例各自都咬得住。同族旧例：契约③ §7.1 W13-3、判据表 F20 的"反向用例喂同一个纯函数"。 |
| C8-13 | **词表闭合的第三条腿（09-23 01:3x，REQ-F-25 裁定）**：事件契约 §6 目录 ↔ `specs/openapi/core-v1.yaml` 的 `EventType` 枚举 ↔ `eventjournal` 常量，**三方相等**（任一腿漂移即红，且要点名到哪一格）。**并且这条判据必须有第二条腿（09-23 01:5x 巡检指出、我核过它成立）**：**"§6 展开后的 token 数 == 去重后的名字数"，不等即红并点名到行** —— 因为三方比的是**集合**，而"新建合并行 + 漏删原行"会**静默去重成同样数目的名字**：09-23 那次我登记 `project_archived` 两次，`§6 目录 ↔ 枚举 ↔ 常量` 全是 63，**C8-12/C8-13 第一腿都会绿**；今晚唯一真响的是 `frontend` 的"重名即停"。而 C8-13 存在的目的恰恰是"不再依赖谁醒了才炸" ⇒ **只判集合相等，等于把这条判据的出生事故本身留成盲区**。**合成反例腿照 C8-11/12 的先例**：喂一行重名进解析器，必须红（只有真数据能红的不算闭上）。**为什么是 Go 判据而不是把 `npm run validate` 焊进 `build`**：那是跳角色闸门 —— 别人的文档漂移会让别人的 build 失败，代价比"晚一轮发现"更大。放进 `core/test` 之后，它跟着大家已经在跑的 `go test ./test/...` 走，**漂移不再需要"谁醒了才炸"** | `go test -count=1 -v -run '^TestEventTypeTripleEquality$' ./test/` | **待建** / test （T-172 本轮派发；用例名暂定，改名回写本行。`frontend` 自己那条 `enum ↔ 目录` 守门保留不动 —— 它还要给 codegen 用，两边不互替。）

| C8-9 | 契约③ §7.2 那句"`api/http`/`cmd/core` 出现 `adapter_instances`/`adapter_capabilities` 的表名字面量写调用即 W13-2 红"由 **W13-2 的运行时表全集**覆盖（`biz_tables()` 从迁移解析，两张都在里面），不另立判据 | `bash scripts/check_ownership.sh`（2b 段逐行印 `[W13-2] <表>: n`） | 现存（09-22 22:36 主控复跑：`core/api`+`core/cmd` 非测试码的表名字面量写恰为 `idempotency_keys`×2 + `principals`×1，聚合表 0 命中）/ test |

**用例落点**：C8-6~C8-8 在 `core/test/errcode_mapping_test.go`（三个独立顶层用例、全程只用 `t.Errorf`，
这样一条坏不会把另两条掐掉——同契约③ §5 判据表 F20 那条"嵌套子用例被父级 `t.Fatal` 吞掉"的教训）。
**第 4 列在 C8-1~C8-5 是"期望"、在 C8-6~C8-9 是"状态 / 归属"**（后者按判据表新格式转录，两种含义都写全了，
读的人按行的来源区分即可；合并成一张表是为了让"§15 这套词表到底谁在判"只有一处答案）。

**行数基线（会随加词漂移，别写死进断言）**：`### 15.1` 表 17 行、`#### 15.1.1` 表 19 行、`### 15.2` 表 **29** 行
（15.2.1 收编前是 18）。**C8-1/C8-4/C8-7 是集合判据，不依赖这些数字**；报数只为让人看见漂移，漂移本身不是缺陷。
**C8-8 是唯一把这些数字变成闸门的地方**——它判的是"解析器有没有坏"，所以它红的时候先修解析器、不修表。


### 15.2 → HTTP 映射（`error.code` 一律用左列，`http_status` 见右）

| HTTP 码 | 状态 | 来自 |
|---|---|---|
| `protocol_not_initialized` | 503 | `PROTOCOL_NOT_INITIALIZED` |
| `protocol_version_mismatch` | 426 | `PROTOCOL_VERSION_MISMATCH` |
| `capability_unavailable` | 503 | `CAPABILITY_UNAVAILABLE` |
| `write_entry_none` | 405 | `WRITE_ENTRY_NONE` |
| `delivery_mode_unsupported` | 422 | `DELIVERY_MODE_UNSUPPORTED` |
| `local_data_unavailable` | 409 | `LOCAL_DATA_UNAVAILABLE` |
| `session_not_found` | 404 | `SESSION_NOT_FOUND` |
| `auth_required` | 401 | `AUTH_REQUIRED` |
| `rate_limited` | 429 | `RATE_LIMITED` |
| `dangerous_flag_refused` | 403 | `DANGEROUS_FLAG_REFUSED` |
| `cancelled` | 409 | `CANCELLED` |
| `timeout` | 504 | `TIMEOUT` |
| `provider_error` | 502 | `PROVIDER_ERROR` |
| `internal` | 500 | `INTERNAL` |
| `read_only` | 409 | `READ_ONLY` |
| `adapter_unresponsive` | 503 | `ADAPTER_UNRESPONSIVE` |
| `execution_state_unknown` | 409 | `EXECUTION_STATE_UNKNOWN` |
| `version_conflict` | 409 | —（HTTP 层专有，来自 ADR-017 的 `If-Match`，不经 Adapter） |

#### 15.2.1 REQ-F-14 收编（2026-09-22）：Core transport 自有码 —— 是 **11 条**，不是 frontend 报的 4 条

frontend 的原始诉求成立（"`ErrorCode` 闭集必须覆盖线上真会出现的码"），但**它的采样面只扫了 `handler.go`**，
所以数是 4。我按"整个 transport 出口"重测了一遍：命令贴在下面，任何人可复跑——

```
grep -rhoE 'writeError\(w, http\.Status[A-Za-z]+, "[a-z_]+"' core/api/http/*.go \
  | sed -E 's/.*, "([a-z_]+)"/\1/' | sort -u          # 12 个码，_test.go 已被文件名排除
```

12 个里只有 `version_conflict` 原样在上表，**其余 11 个此前在 §15.2 没有行**，即：
`method_not_allowed`、`not_implemented`、`origin_not_allowed`、`unauthorized`（frontend 那 4 条，来自 `handler.go`）
+ `validation_failed`、`not_found`、`internal_error`、`illegal_transition`、`idempotency_key_required`、
`idempotency_replay`、`idempotency_mismatch`（来自 `tasks.go`/`notifications.go`，frontend 没扫到）。
`core/api/**` 之外没有任何 `writeError` 生产者（全仓 grep 已确认），所以这张表就是 HTTP 错误码的**全集**。

| HTTP 码 | 状态 | 来自 |
|---|---|---|
| `validation_failed` | 400 | —（Core transport：请求体/查询参数不合法，含 `limit`、`cursor`、`If-Match` 形状） |
| `unauthorized` | 401 | —（Core transport：`Bearer` 口令缺失或不对；与 Adapter 的 `auth_required` **同 HTTP 码、不同生产者**，见下） |
| `origin_not_allowed` | 403 | —（Core transport：`Origin` 不在白名单；与 `dangerous_flag_refused` 同理） |
| `idempotency_key_required` | 403 | —（Core transport：改业务状态的 POST 没带 `Idempotency-Key`，§3/契约③ 那条规则） |
| `not_found` | 404 | —（Core transport：库里没有这行；与 Adapter 的 `session_not_found` 不同生产者） |
| `method_not_allowed` | 405 | —（Core transport：路由存在但方法不对） |
| `idempotency_replay` | 409 | —（Core transport：同键命中已完成记录，返回首次响应，见契约③ §2 幂等行） |
| `idempotency_mismatch` | 409 | —（Core transport：同键不同 `request_hash`） |
| `illegal_transition` | 409 | —（Core transport：状态机拒绝该迁移，来自各 Service 而非 Adapter） |
| `not_implemented` | 501 | —（Core transport：端点尚未接线，走 catch-all；**不得**用假 200 代替） |
| `internal_error` | 500 | —（Core transport：兜底；与 Adapter 的 `internal` 同 HTTP 码、不同生产者） |
| `duplicate_project_path` | 409 | —（Core transport：`POST /projects` 的 canonicalize 后路径撞车，契约③ §3 明写要 409。C28① 裁定收编，2026-09-22 22:4x） |

**501 与 405 不许互换（C28② 裁定，同一轮）**：**路由存在、方法合法、但能力没接线 ⇒ 501 `not_implemented`**；
**方法本身不被这条路径接受 ⇒ 405 `method_not_allowed`**。`GET /api/v1/projects` 属于前者（yaml 里有这个操作，
只是 Core 还没实现），回 405 是在说"这个路径不接受 GET"——那是假话，而且会让客户端把它当成"改用 POST"而不是"等实现"。

**三对"同 HTTP 码、不同名字"是故意的，不许合并**：`unauthorized`/`auth_required`（401）、
`not_found`/`session_not_found`（404）、`internal_error`/`internal`（500）。
区别在**生产者**：右列是反引码名的，说明这个 HTTP 响应是**把 Adapter 的失败翻译出来的**；右列是 `—` 的，
说明 Core 自己就没做成那件事。客户端要区分的是"我这条请求在 Core 里就没过"还是"过 Core 到了 Provider 才挂"，
合并成一个名字就把这条信息丢了——这与 §15.1/§15.2 "命名分裂是故意的"是同一条线。

#### 15.2.2 REQ-F-23 裁定（2026-09-22 23:5x）：一个码有多个生产者时，『来自』列怎么写

**先给事实（09-22 23:5x 我量、09-23 00:00 巡检复量，`core/api` 非测试码）**：**只要一个码有 ≥2 个生产点，本口径就适用** ——
今天至少 `validation_failed`、`method_not_allowed`、`not_implemented` 三个码是这样（我 23:5x 数到 3/4/4 个文件，
巡检 23:58 数到 `method_not_allowed` 已经是 **5 个**：`handler.go`/`notifications.go`/`projects.go`/`sessions.go`/`tasks.go`）。
**⇒ 本段故意不钉具体个数**：多出来那个不是谁数错，是 backend 正在写 `:sync` 又加了一行。
**凡"某集合现在有几个"这类计数写进契约，都要写成阈值 + 带时刻的观察，而不是数字**（同契约① §1.1 第 15 条：定位符要选改名才算破坏性变更的那一种）。

**裁定：『来自』列只回答"这个名字属于哪套词汇"，不回答"哪个包发的"。** 也就是**只有两种合法形状**：
① 一个反引号 Adapter 码名（说明这个 HTTP 响应是把 Adapter 的失败翻译出来的）；② 以 `—` 开头（Core 自有，含几个生产者都算这一类）。
**生产点清单写在 `—` 后面那段自由文字里**，例如本表 `validation_failed` 那行已经写了"含 `limit`、`cursor`、`If-Match` 形状"。
**为什么不许加第三种形状**：C8-7/C8-10 的豁免判据是**按 `—` 这个字符**算的（"『来自』以 `—` 开头 ⇒ 不参与 §15.1 双向比"），
一旦允许写"生产者列表"这种新形状，要么它被误算进码名集合、要么就得给豁免判据再打补丁 ——
**为了记录一个本来就不属于这一列的信息，去动一条刚被证明有用的形状判据，是亏的**。
**要查生产点本来就有工具**：`grep -rlE 'writeError\(w, http\.Status[A-Za-z]+, "<码>"' core/api --include=*.go`，
而且 C8-11 已经在扫"每个字面码 ∈ §15.2"，多生产者对它完全透明。**结论：本表不动结构，`—` 那类码可以有任意多个生产点。**


**新增码必须同时改 §15.1 与 §15.2，缺一即算契约不一致。** 一致性判据**不比行数**，
按**码名集合**双向比（`test` 以此为准，别自己发明更聪明的算法）：
① §15.2『来自』列出现的每个反引号码 ∈ §15.1 码列；② §15.1 每个码 ∈ §15.2『来自』列。
**例外的判据是形状、不是名字**：『来自』列以 `—` 开头的行 = HTTP 层专有、天然不进 §15.1，
所以它从①的集合里排除、也不受②约束。**别把例外写成 `version_conflict` 这个名字**——
15.2.1 之前只有 1 行是 `—`，之前那句"唯一例外"就因此是错的（写死名字 ⇒ 收编后判据自相矛盾）。
当前基线：§15.1 = **17** 码，§15.2 = **30** 行（原 18 + 15.2.1 收编 11 + C28① 的 `duplicate_project_path`），
其中『来自』为 `—` 的 **13** 行（12 条收编 + 原有 `version_conflict`）、带反引码名的 **17** 行。`17 + 13 = 30`。
**这些数字只是报给人看的，不是判据**——把它写成硬断言的判据会在每一次合法扩列时变红，
从而训练人去"改数字"而不是"读结果"；闸门应该判**形状**（总数 = 码名数 + `—` 数、码名集合与 §15.1 双向相等、
`—` 数 ≥ 1 以防解析器静默空集），详见判据表 C8-8 与 C8-10。

### 15.3 数字码（JSON-RPC `error.code`）——**本节 12:4x 重写，撤回我上一版的越权裁定**

`error.code` 是 JSON-RPC 的**数字**字段，`error.data.code` 才是本文的大写蛇形字符串；两者并存、不互相替代。

**先记一条我自己的错**：本节初版写的是「所有领域错误统一走 `-32000`，`data.code` 承载判别符」。
**这条已撤回**——它和 **CO-4 的既有裁定**以及 `internal/adapter` 的 `ErrorCode` 常量块 **已落地的实现**冲突
（那里每个码各有一个数字，且 `internal/adapter` 的 `TestErrorCarriesSymbolInData` 在钉这个数字）。我在改契约前没读这段代码，
属于我这一轮第三次「契约行与盘上实现不符」。以本节现在的规则为准。

**规则（这部分与实现一致，且是真缺陷，保留）：**

1. **JSON-RPC 预定义码只许表达协议层错误**，不得被任何领域码占用：
   `-32700` 帧非法 JSON（stdout 混进非帧字节也算）、`-32600` 非法请求（缺 `jsonrpc`/`method`/`id`）、
   `-32601` 方法不在 §3 方法表内、`-32602` 参数不合 schema。
   **`-32603` 特指"协议层实现自己的故障"**（编解码炸了、对端回了个畸形 `error` 对象、连接被非正常关闭）——
   它**不是**"领域码不认识"的落点（原来我把它写成"未映射的内部故障"，那句就是 adapter 提问的根源，已改）。
   1.1 **收到 §15.1 之外的 `data.code` 怎么降级 —— 裁定用 `INTERNAL(-32013)`，不用 `-32603`**：
       对端给一个我们不认识的领域码，那是**一次合法的领域响应**（只是我方词汇表没覆盖），
       不是 JSON-RPC 层的故障，所以走 `INTERNAL`。实现已正确（`internal/adapter` 的 `(*Error).Degrade`：
       降级到 `INTERNAL` 但**把原码原样留在 `data` 里** —— "unknown code stays visible,
       Core just stops pretending it understands it"）。这与"不许把 `UNKNOWN` 显示成确定值"是同一条纪律：
       **Core 不假装看懂了，但也不销毁证据**。断言下限：未知码 → 对外 `INTERNAL`、`data` 里原码可读、且进 Audit。
2. **领域码的数字一律落在 `-32000`–`-32099`**（JSON-RPC 2.0 留给实现的区间）。
   → 现状有 3 个越界：`ErrProtocolNotInitialized=-32600`、`ErrWriteEntryNone=-32601`、
   `ErrCapabilityUnavailable=-32602` 占用了预定义码，任何通用 JSON-RPC 客户端都会误路由。要改。
3. **一个 §15.1 的码 ↔ 恰好一个数字**，且**数字表以本文为唯一真相**：
   §15.1 将增设「数字码」列，`protocol.go` 的常量必须与之逐项相等，由 `test` 用 grep 断言。
   理由就是我们刚踩的坑——两处各自维护枚举必然漂移。
4. **`INVALID_PARAMS` / `METHOD_NOT_FOUND` 不作为 §15.1 的码**：它们是协议层错误，用 `-32602` / `-32601` 表达。
   ~~但实现里另有 `ErrInvalidParams=-32001`、`ErrMethodNotFound=-32002`……这一条我不擅自判~~
   → **13:2x 已定稿，且是 adapter 自己给的解**：这两个名字**只挂在标准码上**（见 §15.1.1 最后两行），
   不再另设领域副本，"同一概念两套表达"随之消失。我当时判断"要问当事方"是对的，比我准备写的裁定好。
5. `CANCELLED` 不走 `error`：`$/cancel` 的确认是结果帧（`result.cancelled = true`）；
   本文保留 `CANCELLED` 只因为它是 HTTP 侧 `cancelled` 与 Audit 记录用的名字。§15.1.1 给它的生产方就是
   `result-frame`，**它虽有一个数字（`-32010`）但不许出现在任何 `error` 响应里**。

**§15.1 的数字表状态**：~~当前为空、等 adapter 交全表、test 的数字一致性断言标 blocked~~
→ **13:2x 已由 adapter 交出并折进 §15.1.1，`test` 的 check 8 判据同轮定稿**（见 §15.1.1 末尾），
**不再需要 blocked**；但请以 §15.1.1 为准重跑一次，因为我 12:5x 那句"等全表"期间它可能被改过。


## 14. 批准后 `adapter` 角色的第一件事

不是写真 Adapter，而是：**用本协议的 contract test 跑通 FakeAdapter 全 11 个方法 + 4 种故障注入**，
再交回主控。真 Adapter（Codex）的派发等 Wave-2 垂直链闭合之后。

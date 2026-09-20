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
| `model/select` | C→A | 对**已有会话**切模型；不支持必须显式报错（Phase 0：Qoder/Z Code 此项只能 UIA） |
| `quota/read` | C→A | §7 额度契约 |
| `approval/list` | C→A | 挂起审批；载荷必须结构化 |
| `approval/respond` | C→A | 幂等；同一审批二次回应返回首次结果 |
| `session/locate` | C→A | **v0.1 只留桩**：Phase 0 §5 结论是五家 deep-link 到会话粒度全部未实测，
  只有 OpenCode `/tui/select-session` 有程序化证据。返回 `CAPABILITY_UNAVAILABLE` 即为正确实现 |
| `stream/*` | A→C | 通知流，§6 |
| `debug/*` | C→A | §9，FakeAdapter 专属 |

方法名固定为 `域/动作`（小写、斜杠分隔）。Adapter 内部方法名与其原生协议**无关**——
Codex 的 `ThreadList` 由 Codex Adapter 自己映射到 `session/list`，映射表写进 Adapter 的
`metadata_json`，Core 不感知原生命名。

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
| `stream/delta` | `{sequence_no, channel, text, provider_raw}`；`channel ∈ ASSISTANT/THOUGHT/TOOL/STATUS` |
| `stream/item` | 一个完整条目（工具调用/结果）就绪 |
| `stream/state` | `{normalized_status, provider_status_raw, confidence}` |
| `turn/finished` | `{result_code, usage?, provider_raw}` —— **唯一的强制收尾信号** |

规则：

1. **只承诺 `turn/finished` 是必须的**。`stream/delta` 可以完全没有——
   Phase 0 §6.2 实测 **Qoder 真流式 UNSUPPORTED**，协议必须容得下"只有 turn 粒度"的 Provider。
2. `sequence_no` 由 Adapter 单调递增维护；断线重连后 Core 用 `since_seq` 补齐，
   重复帧按 `sequence_no` 幂等丢弃。
3. Core 侧按 250–500 ms 或达到字符阈值批量 flush 进 `execution_output_chunks`（领域模型「流式输出」规则），
   **每个 delta 绝不写 event_journal**。
4. Adapter 不写 DB、不发事件；`stream/delta` 不是 Durable Event，掉电即丢，靠 chunk 表兜。

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

**新增码必须同时改 §15.1 与 §15.2，缺一即算契约不一致。** 一致性判据**不比行数**，
按**码名集合**双向比（`test` 以此为准，别自己发明更聪明的算法）：
① §15.2『来自』列出现的每个反引号码 ∈ §15.1 码列；② §15.1 每个码 ∈ §15.2『来自』列。
**唯一例外**：`version_conflict` 在 §15.2 有行而 §15.1 无码（HTTP 专有，右列写的是 `—` 而非码名，所以它天然不会进①的集合）。
当前基线：§15.1 = 17 码，§15.2 = 18 行。

### 15.3 数字码（JSON-RPC `error.code`）——**本节 12:4x 重写，撤回我上一版的越权裁定**

`error.code` 是 JSON-RPC 的**数字**字段，`error.data.code` 才是本文的大写蛇形字符串；两者并存、不互相替代。

**先记一条我自己的错**：本节初版写的是「所有领域错误统一走 `-32000`，`data.code` 承载判别符」。
**这条已撤回**——它和 **CO-4 的既有裁定**以及 `core/internal/adapter/protocol.go:51-67` **已落地的实现**冲突
（那里每个码各有一个数字，且 `protocol_test.go:248` 在钉这个数字）。我在改契约前没读这段代码，
属于我这一轮第三次「契约行与盘上实现不符」。以本节现在的规则为准。

**规则（这部分与实现一致，且是真缺陷，保留）：**

1. **JSON-RPC 预定义码只许表达协议层错误**，不得被任何领域码占用：
   `-32700` 帧非法 JSON（stdout 混进非帧字节也算）、`-32600` 非法请求（缺 `jsonrpc`/`method`/`id`）、
   `-32601` 方法不在 §3 方法表内、`-32602` 参数不合 schema、`-32603` 未映射的内部故障。
2. **领域码的数字一律落在 `-32000`–`-32099`**（JSON-RPC 2.0 留给实现的区间）。
   → 现状有 3 个越界：`ErrProtocolNotInitialized=-32600`、`ErrWriteEntryNone=-32601`、
   `ErrCapabilityUnavailable=-32602` 占用了预定义码，任何通用 JSON-RPC 客户端都会误路由。要改。
3. **一个 §15.1 的码 ↔ 恰好一个数字**，且**数字表以本文为唯一真相**：
   §15.1 将增设「数字码」列，`protocol.go` 的常量必须与之逐项相等，由 `test` 用 grep 断言。
   理由就是我们刚踩的坑——两处各自维护枚举必然漂移。
4. **`INVALID_PARAMS` / `METHOD_NOT_FOUND` 不作为 §15.1 的码**：它们是协议层错误，用 `-32602` / `-32601` 即可。
   但实现里另有 `ErrInvalidParams=-32001`、`ErrMethodNotFound=-32002` 两个**领域**常量，
   与预定义码构成同一概念两套表达。**这一条我不擅自判**——CO-4 的原始意图只有 adapter 清楚，
   见第十二批 T-36②：由 adapter 给出「删哪个、留哪个、为什么」的一行结论，我再折进 §15.1。
5. `CANCELLED` 不走 `error`：`$/cancel` 的确认是结果帧（`result.cancelled = true`）；
   本文保留 `CANCELLED` 只因为它是 HTTP 侧 `cancelled` 与 Audit 记录用的名字。

**§15.1 的数字列当前为空**（等 adapter 交全表）。在它填上之前，`test` 那道数字一致性断言标 `blocked`，
**不要**拿本文去断言实现红——数字的真实现状在 `protocol.go` 里，缺的是权威表而不是代码。


## 14. 批准后 `adapter` 角色的第一件事

不是写真 Adapter，而是：**用本协议的 contract test 跑通 FakeAdapter 全 11 个方法 + 4 种故障注入**，
再交回主控。真 Adapter（Codex）的派发等 Wave-2 垂直链闭合之后。

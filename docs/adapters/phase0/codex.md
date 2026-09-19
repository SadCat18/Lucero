# Phase 0 调查报告：Codex

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ 方法：**纯静态取证**（CLI `--help`、协议 schema 生成、本地库结构、注册表、netstat）
- 被调查实物与版本号：
  - Codex CLI **`codex-cli 0.154.0`** — `%LOCALAPPDATA%\Programs\OpenAI\Codex\bin\codex.exe`（298,169,136 B，软链至 `~/.codex/packages/standalone/current/bin`）
  - Codex Desktop — MSIX 包 `OpenAI.Codex_26.915.3509.0_x64__2p2nqsd0c76g0`，主进程名 `ChatGPT.exe`（实测在运行）
  - 历史会话内 `session_meta.cli_version = 0.144.2` → **本机存在跨版本并存的会话数据**
- 认证方式（`~/.codex/auth.json` 键名取证，值未读取）：`auth_mode`、`tokens{id_token, access_token, refresh_token, account_id}`、`OPENAI_API_KEY`(本机为 null)、`last_refresh`
  → 主路径 **ChatGPT OAuth（含 refresh token）**，次路径 OpenAI API Key；协议侧对应 server→client 请求 `ChatgptAuthTokensRefresh*`
- 原始输出归档：`D:\Project\Lucero\.scratch\phase0-codex\`（`help_*.txt` ×9、`schema_x/` 47 顶层 + v1 + v2 共 379 文件、`probe_rollout.py`）
- 真实消息预算使用：**0 / 3**

## 关键结构性事实

Codex 官方提供 `codex app-server generate-json-schema --out <dir> --experimental`，**exit=0** 产出完整 JSON-RPC 协议契约：
`ClientRequest.json`(281,828 B) + `ServerRequest.json` + `ServerNotification.json` + v2 目录 379 个 Params/Response/Notification schema。
这意味着 Codex 的 capability 声明**不需要靠猜**，每次版本升级可重新生成机器可读契约。
但 `app-server` 与 `remote-control` 子命令官方标注 `[experimental]`，且 v1 / v2 并存 —— 见 §25。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | NATIVE | `schema_x/v2/{LoginAccount,LogoutAccount,GetAccount,AccountUpdated,ChatgptAuthTokensRefresh*}.json`；`auth.json` 键名 | 连接本机 app-server daemon 即以已登录账号工作，无需二次认证 | token 刷新是 **server→client 请求**：Adapter 若自起 app-server 必须实现 `ChatgptAuthTokensRefresh` 回调，否则长任务会掉登录。降级：改用 `codex exec` 子进程（复用 CLI 认证） |
| 2 | 软件版本发现 | CLI | `codex --version` | `codex-cli 0.154.0`，exit=0 | CLI 版本 ≠ Desktop 版本（MSIX 26.915.3509.0）。降级：读 rollout 内 `session_meta.cli_version` 得**每会话**版本 |
| 3 | 列出已有会话 | NATIVE | v2 `ThreadList` / `ThreadLoadedList` / `ThreadArchive`+`ThreadArchived`；`codex agents --help` | 命令与 schema 均在；`agents` = "Browse all agent sessions on the shared local app-server daemon" | 需 daemon 在跑：**实测 netstat 中当前无 Codex 监听端口** → 冷启动要自己拉起 daemon。降级：LOCAL_DATA 扫 `~/.codex/sessions/**/rollout-*.jsonl`（实测 129 个文件） |
| 4 | 读取完整会话 | NATIVE | v2 `ThreadRead` / `ThreadItemsList` / `ThreadTurnsList` / `ThreadTimelineList` / `ThreadSearch`；`session_meta.history_mode` | `history_mode = "paginated"` 实测存在 → 分页契约 | 分页需处理 `nextCursor`。降级：LOCAL_DATA 直读 rollout JSONL |
| 5 | 新消息增量同步 | NATIVE | v2 delta 通知集；`probe_rollout.py` 输出 | 推送侧 `AgentMessageDelta`、`ItemStarted/ItemCompleted`、`TurnStarted/TurnCompleted`、`ThreadStatusChanged`、`ReasoningTextDelta`；本地侧每行带**单调 `ordinal`** → 天然游标 | 未实测真实流（静态证据充分）。降级：LOCAL_DATA 按 `ordinal` 轮询 tail |
| 6 | 双向同步 | NATIVE | v2 `ThreadInjectItems`、`TurnStart`、`ThreadQueueChanged` | 出站事件 + 入站注入方法同时存在于同一 JSON-RPC 通道 | "双向"未经端到端实测，仅方法级证据 → Adapter 上线时列为需回归项。降级：只读单向 + CLI 写 |
| 7 | 向指定已有会话发消息 | NATIVE + CLI | v2 `TurnStart{threadId,…}`；`codex queue --thread <UUID\|name> --message <TEXT>`；`codex resume [SESSION_ID] [PROMPT]` | `queue` 帮助原文 "Queue a message for an existing session"，`--thread` 接受 **UUID 或精确会话名** | `queue` 依赖 daemon/共享 app-server；未实测投递。降级：`codex exec resume <id>` 起一次性子进程 |
| 8 | 流式响应 | NATIVE | v2 delta 通知；`codex exec --help` | `codex exec --json` = "Print events to stdout as **JSONL**"；另有 `--include-partial-messages` 类语义的 `apply_patch_streaming_events`(当前 `under development false`) | exec 的 JSONL 只在子进程内；长驻流要 app-server 通道。降级：exec JSONL 分段 |
| 9 | 中止当前生成/任务 | NATIVE | v2 `TurnInterrupt`、`ProcessKill`、`CommandExecTerminate`、`ThreadBackgroundTerminalsTerminate` | 四类中止粒度齐全（turn / 进程 / 命令 / 后台终端） | 未实测。降级：杀 CLI 子进程 |
| 10 | 读取模型列表 | NATIVE | v2 `ModelList` + `ModelProviderCapabilitiesRead` + `BedrockDiscover` | `ModelListResponse = {data: array, nextCursor: string\|null}` | 需账号在线（未离线实测）。降级：LOCAL_DATA 读 `config.toml` 的 `model` / `model_providers.*` |
| 11 | 切换模型 | NATIVE | v2 `ThreadSettingsUpdate{model}`、`TurnStart{model}`；`codex -m/--model` | 会话级与 turn 级两种切换都存在 | 另有 `allowProviderModelFallback` + `ModelRerouted` 通知 → **Codex 可能自行改模型**，Core 侧不能把"请求的模型"当"实际用的模型" | 
| 12 | 推理强度/参数控制 | NATIVE | v2 `ReasoningEffort`、`TurnStartParams{effort,reasoning_effort,summary}`；rollout `turn_context` | 实测样本 `effort ∈ {high, xhigh}`，`collaboration_mode.settings.reasoning_effort` 同步存在；`config.toml` 有 `model_reasoning_effort = "high"` | 未见"关闭推理"的枚举，只有档位。降级：config `-c model_reasoning_effort=` |
| 13 | 精确额度读取 | NATIVE | v2 `GetAccountTokenUsage`、`AccountRateLimitsUpdated`、`ThreadTokenUsageUpdated`、`NullableGetAccountTokenUsage` | `RateLimitSnapshot{limit, used, usedPercent, remainingPercent, windowDurationMins, resetsAt}`、`credits{balance, hasCredits, unlimited}`、`planType`、`spendControlReached` | 出现 `Nullable*` 变体 → **额度可能整体为 null**，Core 必须把 UNKNOWN 建模为一等状态 | 
| 14 | Reset 时间 | NATIVE | `AccountRateLimitsUpdatedNotification.json` 属性名 | 字段 `resetsAt` 实测存在于 snapshot 内；另有 `ConsumeAccountRateLimitResetCredit`(消费重置额度) | 未确认时区/毫秒单位语义。降级：LOCAL_DATA `event_msg/token_count` 的 payload 同时带 `info` 与 `rate_limits` |
| 15 | 任务实时状态 | NATIVE | v2 `ThreadStatusChanged`、`TurnStarted/Completed`、`ItemStarted/Completed`、`ThreadGoal{Get,Set,Updated,Cleared}` | 状态 + 目标(goal)双层；rollout 实测有 `task_started`(2) / `task_complete`(1) / `item_completed`(120) 事件 | 未实测。降级：LOCAL_DATA 按 `ordinal` 追事件 |
| 16 | 审批读取 | NATIVE | v2 `ServerRequest.json` + 8 个审批 Params：`ExecCommandApproval`、`ApplyPatchApproval`、`PermissionsRequestApproval`、`CommandExecutionRequestApproval`、`FileChangeRequestApproval`、`McpServerElicitationRequest`、`ToolRequestUserInput`、`ItemGuardianApprovalReviewStarted` | 审批是 **server→client 请求**（带 Response schema），不是轮询表 | 审批类型分散在 8 种请求里 → Adapter 必须全枚举，漏一种就卡死。降级：`-a never` 关掉审批（会丢审批可见性，属功能降级） |
| 17 | 审批响应 | NATIVE | 上述每个 `*ApprovalResponse.json`；CLI `--approve-for-me`、`-a/--ask-for-approval {on-request,never}`、`--dangerously-bypass-approvals-and-sandbox` | 响应契约成对存在 | 审批响应必须**在同一 JSON-RPC 连接内**回，跨进程不可补 → Adapter 必须长驻，不能"事后补批" |
| 18 | 附件 | NATIVE | v2 `TurnStartParams` 内 `image_url`、`audio_url`、`TextElement`、`byteRange`、`ImageDetail`；CLI `-i/--image <FILE>...`；`~/.codex/attachments/` 目录存在 | 输入侧多模态槽位齐全（图/音/文本元素/字节区间） | 未见文件类(非图)附件通道。降级：把文件放进 cwd 让工具自己读 |
| 19 | MCP/Skill/Rule/Hook | NATIVE | `codex mcp`/`codex plugin` 子命令；v2 `ListMcpServerStatus`、`McpServerToolCall`、`McpServerRefresh`、`McpServerOauthLogin`、`SkillsList`、`SkillsConfigWrite`、`SkillsChanged`、`HooksList`、`HookStarted`、`Plugin{List,Install,Search,Share*}`、`AppsList` | 四类扩展**全部有方法级契约**；CLI 侧 `--ignore-rules` 提到 user/project `.rules` execpolicy | Hook 有信任机制（`--dangerously-bypass-hook-trust`）→ 自动化需预置信任。降级：LOCAL_DATA 读 `config.toml` |
| 20 | 本地会话数据库 | LOCAL_DATA | `probe_rollout.py` 实测 | **不是 SQLite**：`sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl`，行 `{timestamp, ordinal, type, payload}`，type ∈ `session_meta\|event_msg\|response_item\|world_state\|turn_context\|compacted`；另有 `goals_1.sqlite` | 有 `migrate-rollouts` 与 `background_paginated_rollout_migration`(under development) → **Codex 自己正在改这个格式**，路径/事件名会变 |
| 21 | CLI | CLI | `codex --help` exit=0 | 25 个子命令：`agents exec review login logout mcp plugin app-server remote-control app completion update doctor sandbox debug apply resume queue archive delete migrate-rollouts unarchive fork cloud exec-server features help` | 交互式命令(`agents`,`resume` picker)默认渲染 TUI，脚本化必须走 `exec`/`queue`/app-server |
| 22 | SDK/API/App Server | NATIVE（官方标 EXPERIMENTAL） | `codex app-server --help`、`codex app-server generate-json-schema` exit=0、`generate-ts` | 传输可选 `stdio://`(默认) / `unix://` / `ws://IP:PORT`；WS 鉴权 `capability-token` 或 `signed-bearer-token`(含 issuer/audience/clock-skew)；`remote-control` 另有 daemon 管理 | **子命令描述为 `[experimental]`** → 协议可能破坏性变更。降级：CLI 层 |
| 23 | 精确跳转原客户端会话 | CLI | `codex resume [SESSION_ID]`；`HKCR\codex` 注册 `URL Protocol`，值 `URL:codex`（**未**在 HKCR 层看到 `shell\open\command`，handler 应由 MSIX 注册） | 命令行可用 session UUID 精确恢复；`--all` 可跨 cwd 列出 | deep link 是否接受 session id **未实测**（实测会弹桌面窗口，属打扰，本角色不做）。降级：CLI 恢复 |
| 24 | UI Automation 可行性 | UI_AUTOMATION | `features list` 中 `computer_use = stable true`；`config.toml` 的 `notify` 指向 `…\runtimes\cua_node\…\@oai\sky\bin\windows\codex-computer-use.exe`；`~/.codex/computer-use/{bin,runtimes}` | Codex **自带** computer-use 执行体，且 Windows 侧有 `windows.sandbox = elevated` 配置 | 优先级低：有 API 就不该用 UIA。仅作最后兜底 |
| 25 | 版本变化风险 | EXPERIMENTAL | 上述全部 + `codex features list` | 风险点实测到位：① `app-server`/`remote-control`/`cloud`/`exec-server` 均标 experimental；② v1 与 v2 协议并存；③ feature flag 有 `removed` / `under development` / `stable` 三态，本次观察到 `apply_patch_freeform`、`collaboration_modes`(removed 但 true) 等已移除项仍在生效；④ rollout 格式处于迁移中 | **缓解手段是本机已验证的**：升级后重跑 `generate-json-schema` 做 schema diff，把 diff 当版本契约测试 |

## 建议接入层级与 fallback 链

主链 **Native API（app-server）**：

```
NATIVE: codex app-server --listen stdio://（或 ws://）
   │  握手后按 generate-json-schema 的 bundle 声明 capability
   ├─ 失败(无 daemon/实验开关关闭) → CLI: codex exec --json（一次性）/ codex queue --thread（投递）
   │                                   └─ 失败 → LOCAL_DATA: 只读 tail rollout JSONL（ordinal 游标）
   └─ 只用于"确实需要写"的能力：不要 UI Automation
UI_AUTOMATION: 仅用于 Desktop-only 且无 API 的能力（本次未识别出此类）
```

## 仍需 UI Automation 的能力

**无。** Codex 是唯一"25 项全都有 API/CLI/本地数据证据"的一家。

## UNKNOWN 清单与原因

| # | 能力/字段 | 为什么是 UNKNOWN |
|---|---|---|
| 23 | Desktop deep link 的 session 粒度 | 实测需真的触发 `codex://` 跳转并观察桌面窗口，属对用户的可见打扰，本角色未做 |
| 1/13 | token 刷新与额度接口的**运行时行为** | 仅有 schema 级证据；动用真实消息预算才能观测，判定为不必要（3 条预算留给跨家对比） |
| 16/17 | 8 类审批请求的**实际触发顺序与字段填充** | 本机 `config.toml` 实测 `approval_policy = "never"` + `sandbox_mode = "danger-full-access"`，用户环境从未产生审批样本 |
| 22 | `--listen ws://` 的实际握手 | 需启动 daemon，本角色未启动（避免改变第三方软件状态） |

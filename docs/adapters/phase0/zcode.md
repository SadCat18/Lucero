# Phase 0 调查报告：Z Code

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ 方法：**纯静态取证**（exe 版本信息、SQLite schema + 枚举实测、配置结构、bundle 字符串扫描、netstat、注册表）
- 被调查实物与版本号（**两条独立版本线**）：
  - ZCode Desktop **3.12.3.7463** — `D:\Program Files\ZCode\ZCode.exe`（Electron，运行中，16 进程）
  - 引擎版本 **0.16.x** — 实测 `session.version ∈ {0.16.1, 0.16.3, 0.16.5}`，`schema_migration.app_version` 跨 0.2.0 → 0.16.5 共 22 条
  - 引擎 bundle：`resources/glm/zcode.cjs`（11,416,833 B）；工具：`resources/tools/{cua-helper, ripgrep, ugrep}`
  - 更新源 `app-update.yml` = `provider: generic, url: http://localhost:8081`（**不是远端 CDN** → 自动更新源在本机不可达）
- 目录澄清（本会话曾纠正一次）：`D:\ZCodeFile\.zcode\` 是**工作区目录**（`workspace/`、`plugin-workspace/`、`export-log-stage/`、`v2/` 内是任务索引），
  **真正的 app home 是 `%USERPROFILE%\.zcode\`**（`cli/`、`skills/`、`plugins/`、`v2/`、`workspace/`）。主库在 `~/.zcode/cli/db/db.sqlite`。
- 认证方式（`~/.zcode/v2/credentials.json` **键名**取证，值未读取）：
  `oauth:bigmodel:access_token`、`oauth:active_provider`、`zcodejwttoken`、`web-remote-control:external-relay:pass_hash`、
  `account-provider:coding-plan:account:*:api-key`；`config.json` 内 provider 形如 `kind=anthropic` + `options.{apiKey, baseURL}`
  → 三类并存：**Bigmodel/Z.ai OAuth**、**ZCode 自有 JWT**、**coding-plan 账号级 API Key**
- 原始输出归档：`D:\Project\Lucero\.scratch\phase0-zcode\`（`probe.py`）
- 真实消息预算使用：**0 / 3**

## 关键结构性事实（会影响 Adapter 协议设计）

1. **ZCode 完全没有对外编程接口**：实测 `netstat -ano -p tcp` 下 16 个 ZCode 进程**无一处于 LISTENING**；
   `find ~/.zcode "D:\Program Files\ZCode" -iname "*zcode*.exe"` 只有主程序与卸载器，**无 CLI 可执行体**；
   `zcode.cjs` 内的 http 路由字面量全是**远端服务**（`/chat/completions`、`/api/v1/agent/configs`、`/api/v1/client/configs`、`/api/auth/tokenByAuthCode`），不是本地服务。
   → 「Native API → CLI → Local Data → UI Automation」降级链在 ZCode 上**必然跳过前两级**。
2. **但它的本地库是五家中语义最完整的之一**（`~/.zcode/cli/db/db.sqlite`，208,707,584 B，23 表）：
   `session` 89 / `message` 12,213 / `part` 46,775 / `session_input` 901 / `model_usage` 8,264 / `turn_usage` 696 / `tool_usage` 9,186 / `todo` 241 / `input_history` 100。
   `message` 与 `part` 都带 **`sequence`** 列 → 增量游标原生存在。
3. **与 OpenCode 的本地库高度同构**（实测列名比对）：`session` 表 **20/20 列同名**
   （`id, project_id, workspace_id, parent_id, slug, directory, path, title, version, share_url,
   summary_additions, summary_deletions, summary_files, summary_diffs, revert, permission,
   time_created, time_updated, time_compacting, time_archived`，ZCode 另加 `task_type, title_source, title_message_id, trace_id`）；
   `session_input` / `todo` / `permission` / `part` 同族；`part.data.type` 取值集合是 OpenCode 的超集
   （共有 `tool, step-start, step-finish, reasoning, text, file`；ZCode 另加 `timeline, compaction`）。
   → **一个 LOCAL_DATA reader 可同时覆盖 OpenCode 与 Z Code 的只读链**。
4. 有 **微信机器人驱动通道**（`bot-config.v3.json` 的 `bots[{id,name,provider,providerUserId,allowedCommands,allowedWorkspaces,credentialRef,currentOptions,replyMode,enabled}]`
   + `bot-state.v3.json` 的 `{workspacePath, workspaceId, mode:"task", activeTaskId, weixinGetUpdatesBuf, weixinActivatedAt}`）
   → 存在一条**官方支持的带外注入路径**（对微信发消息 → 变成 ZCode 任务）。这不是本地 API，但对"向指定会话发消息"有架构意义。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | LOCAL_DATA | `credentials.json` 键名、`config.json` 的 `options.apiKey`、`coding-plan-cache.json` | 凭据**明文 JSON 落盘**（OAuth access_token / JWT / coding-plan api-key）， Adapter 理论上可读用 | 读别人 token 属最脆弱路径（有 refresh、有到期）；`last_refresh` 类字段未见。降级：只读观测，不代其发请求 |
| 2 | 软件版本发现 | LOCAL_DATA | exe VersionInfo；`session.version`；`schema_migration.app_version`；`.zcode-install-manifest`(57,891 B) | Desktop `3.12.3.7463` 与引擎 `0.16.5` **必须同时记录**，单记一个会误判 | 无 CLI `--version`。降级：exe 元数据 |
| 3 | 列出已有会话 | LOCAL_DATA | `~/.zcode/cli/db/db.sqlite` `session`(89)；`D:\ZCodeFile\.zcode\v2\tasks-index.sqlite` `tasks`(35) | 主库可列全量（含 `title, project_id, parent_id, slug, time_archived, task_type`）；索引库另有 `pinned, archived, deleted, unread_at, searchable_text, model, mode, provider` | 两个库职责不同：**索引库只有 35 条、主库 89 条** → 不能只读索引库。降级：文件系统扫 `~/.zcode/cli/agents/sess_<uuid>/`（实测 13 个） |
| 4 | 读取完整会话 | LOCAL_DATA | 同上；`~/.zcode/cli/rollout/model-io-sess_<uuid>.jsonl`；`~/.zcode/cli/agents/sess_<uuid>/agent_<uuid>/{transcript.jsonl, metadata.json, output.txt, task.output}` | `message`(12,213) + `part`(46,775) 完整可 JOIN；子代理正文实测 `transcript.jsonl` 达 **10,228,926 B**；`rollout/` 另有 model-io 级 3 个 JSONL | **无 API，只能读库**；库正在被写入（WAL 4,185,952 B）→ 只读连接需容忍快照滞后。降级：读 rollout/agents 文件 |
| 5 | 新消息增量同步 | LOCAL_DATA | `message.sequence`、`part.sequence`、`session_entry`、`-wal` 文件 | `sequence` 列实测存在 → 可按会话游标增量。`session_input.admitted_sequence/promoted_sequence` 另给一层输入队列游标 | **无推送通道**（无 socket / 无 SSE / 无 IPC server）→ 只能轮询。降级：mtime + WAL 尺寸变化 |
| 6 | 双向同步 | LOCAL_DATA | 同上 | 出站（观测）成立；入站无合法通道（写第三方库 = 禁区） | **单向**。降级：UIA 输入 或 微信机器人通道 |
| 7 | 向指定已有会话发消息 | **UI_AUTOMATION** | 无 CLI、无监听端口、无本地服务；`session_input` 表结构与 `delivery` 枚举实测（证明 ZCode 自己有这套语义，但没给我们入口） | 实测 `session_input.delivery ∈ {startNow, queue, guide}`、`status ∈ {admitted, promoted, cancelled, discarded, failed}`；本机 `startNow/promoted`=691、`queue/promoted`=197、`queue/cancelled`=11 → **排队与转向是 ZCode 的一等能力**，只是不可外部调用 | 唯一带外路径：微信机器人（`mode:"task"`, `activeTaskId`）与 `web-remote-control` relay —— 两者都经厂商云端，非本地。降级：写第三方库（**禁区，不采纳**） |
| 8 | 流式响应 | **UNKNOWN** | `model_usage.first_token_at`、`time_to_first_token_ms` | DB 记录了**首 token 延迟**（实测样本 `time_to_first_token_ms = 4091 / 16057`）→ 证明内部有流式，但外部无观测点 | 无法证实"外部能否看到流"。原因：无 API/CLI 可试；发消息预算用不上（没有可发消息的入口）。降级：UIA 截屏轮询（`mcp__computer-use__screenshot` 是 ZCode 自己的工具，实测 8 次调用） |
| 9 | 中止当前生成/任务 | **UI_AUTOMATION** | `model_usage.status` CHECK 枚举含 `'cancelled'`；`turn_usage.status` 同 | 枚举里有 `cancelled` → 该家支持取消这个动作，但无外部调用面 | 实测本机 `model_usage.status` 只观察到 `completed`；`cancelled` 计数未验证。降级：杀进程（丢任务，高代价） |
| 10 | 读取模型列表 | LOCAL_DATA | `config.json`(7,542 B)、`provider_config.json`(3,859 B) | 实测 provider `kind=anthropic`，其下 model 规则含 `GLM-5.3`、`GLM-5.3-Flash`，每模型有 `reasoning.{enabled,variants[3],defaultVariant}`、`limit.{context:1000000,output:128000}`、`modalities.{input,output}`、`zcode.{priority,modified}` | 模型目录是**套餐绑定**的（`builtin:bigmodel-coding-plan` / `-start-plan` / `zai-*`）→ 可用性随 entitlement 变。降级：`provider_config.json` 的 rules |
| 11 | 切换模型 | **UI_AUTOMATION** | `session_entry.type = "runtime/model_selection"` 实测 **89 行（每会话 1 条）**；`tasks-index.tasks.model`、`automations.model` | 会话内改模型**有历史记录**（`session_entry` 事件型），说明该动作真实存在且被持久化 | 只能观测不能触发。`automations.model` 说明**新建自动化时**可指定模型（那是配置写入路径，属禁区）。降级：UIA |
| 12 | 推理强度/参数控制 | LOCAL_DATA（读）+ UI_AUTOMATION（写） | `reasoning.variants` 实测 `[3 items]` 首末为 `low`/`max`，`defaultVariant="max"`；`tasks-index` 的 `automations.thought_level`、`off_peak_tasks.thought_level`；`model_usage.variant` 实测值 `"max"` | 该家把推理档位叫 **`thought_level`**（ZCode 侧）/ **`variant`**（model_usage 侧）两个名字 | 未见完整枚举（只观察到 low/max/defaultVariant）→ **禁止猜中间档位**。降级：UIA |
| 13 | 精确额度读取 | READ_ONLY | `model_usage`(8,264)、`turn_usage`(696)、`session_target.token_budget/tokens_used` | 实测 `model_usage.{input_tokens:98188, output_tokens, reasoning_tokens, cache_creation_input_tokens, cache_read_input_tokens, duration_ms, time_to_first_token_ms, tool_call_count, finish_reason}`；`turn_usage.computed_total_tokens` | 这是**用量**不是**余额**。`coding-plan-cache.json` 实测只有资格态：`{status:"available" \| "unavailable", reason:"coding_plan_not_entitled"}` → **套餐层无数字额度** | 
| 14 | Reset 时间 | **UNSUPPORTED**（本机可证范围内） | `coding-plan-cache.json` 全量实测 | 该文件只有 `entryStatus.items{status,reason}` + `updatedAt`，**无任何 reset/period 字段** | 原因：额度归 Bigmodel/Z.ai 账号侧，ZCode 本地不落。要拿必须调它的远端 API（= 用用户凭据打厂商接口，禁区）。降级：无 |
| 15 | 任务实时状态 | LOCAL_DATA | `tasks-index.automations{running,claimed_at,dispatch_status,next_run_at,last_run_at,retry_at,lifecycle_status}`、`automation_runs{trigger,dispatch_status,outcome,attempts,error,session_id,model_selection}`、`off_peak_tasks{status,queue_position,next_poll_at,claim_running,attempt_count,files_changed,failure_reason,server_ticket_id}`、`bot-state.activeTaskId` | 三级状态齐全，且**含调度态**（cron 自动化 + 错峰任务队列 + 队列位置）；`off_peak_tasks` 还有 `server_ticket_id` → 错峰任务是**云端排队**的 | 无推送 → 实时性受轮询频率限制。`off_peak_tasks` 本机 rows=0，其状态枚举未取到 |
| 16 | 审批读取 | READ_ONLY | `tool_usage.approval_status`、`permission` 表、`session.permission` 列、`tasks-index.{automations,off_peak_tasks}.permission_mode`、`AskUserQuestion` 工具 | 结构齐备：`tool_usage` 有 `approval_status`、`side_effect_scope`、`read_only`、`destructive`、`cancelled_by_user`、`exit_code`、`truncated`、`retry_count`；`permission` 表按 `project_id` 存规则 | **实测 `tool_usage.approval_status` 9,186 行全为 `'none'`** → 本机从未触发审批，无法证实待审批是否会写入此列。这是本家最大的证据缺口 |
| 17 | 审批响应 | **UI_AUTOMATION** | 同上 | 无任何可调用面 | 降级：无 |
| 18 | 附件 | LOCAL_DATA | `input_history.attachments` 列；`part.data.type="file"` 实测 **284 行**；`~/.zcode/cli/image-cache/`；`config.json` 的 `modalities.input` 含 `image`（GLM-5.3-Flash） | 附件既在输入历史（`input_history` 100 行含 `attachments`）也在 part 流（`file` 284 条） | 模态能力**分模型**：`GLM-5.3` input 只有 `text`，`GLM-5.3-Flash` 才有 `text,image` → Adapter 必须做模型级模态校验 | 
| 19 | MCP/Skill/Rule/Hook | LOCAL_DATA | `~/.zcode/cli/config.json`（`mcp.servers.node_repl{type:stdio, command:node, args, enabled}` + `plugins.enabledPlugins{freeglm-core@freeglm, cloudbase-skills@zcode-plugins-official, computer-use@zcode-plugins-official}`）；`~/.zcode/skills/{glm-vision,ima-skill,image-2-skill}`；`D:\ZCodeFile\.zcode\workspace\default\zcode-skills\{marketplace.json, plugins/, templates/, .zcode-plugin/}`；工具名 `Skill`、`mcp__node_repl__js`、`mcp__computer-use__screenshot` | 四类扩展都在，且**同时兼容 `.claude-plugin/marketplace.json` 与 `.zcode-plugin/`**（实测某仓库内两者并存）→ 它是 Claude-Code 插件生态的兼容方 | 纯文件约定，无查询接口。降级：读 config |
| 20 | 本地会话数据库 | LOCAL_DATA | 实测 | 两个库：`~/.zcode/cli/db/db.sqlite`(208 MB, 23 表) = 主事实源；`D:\ZCodeFile\.zcode\v2\tasks-index.sqlite`(2.6 MB, 9 表) = 跨工作区索引（含 automations / off_peak_tasks / task_groups）。另有 `checkpoints/`(191 MB, `{manifests, extra-manifests, pending, state.json, tmp}`) = 工作区快照 | **双库 + 双 app home**（C 盘引擎态 / D 盘工作区态）→ Adapter 配置必须给两条路径 |
| 21 | CLI | **UNSUPPORTED** | 全盘 find + PATH 探测 | 只有 `ZCode.exe` 与 `Uninstall ZCode.exe`；`~/.zcode/cli/` 是**数据目录**不是可执行体（无 bin/） | 原因：本机无实物。若厂商另有 CLI 需安装才能证实 → 撞红线④，本角色不申请 |
| 22 | SDK/API/App Server | **UNSUPPORTED** | netstat 实测 0 监听；zcode.cjs 路由扫描 | 无本地 server、无 daemon、无 IPC 端点（`~/.zcode` 顶层实测只有 `{cli, plugin-workspace, skills, v2, workspace}`）。存在的只有**厂商远端 API** 与 `web-remote-control:external-relay:pass_hash` + 微信 bot | 走远端 = 使用用户凭据打厂商私有接口，本项目禁区。降级：LOCAL_DATA + UIA |
| 23 | 精确跳转原客户端会话 | UNKNOWN | `HKCR\zcode` 注册 `URL Protocol`，`shell\open\command` = `"D:\Program Files\ZCode\ZCode.exe" "%1"` | 协议处理器**确实存在并把 URL 转交主程序** | URL 是否接受 session/task id **未实测**（实测会弹用户窗口）。降级：UIA 在列表里点选 |
| 24 | UI Automation 可行性 | UI_AUTOMATION | `resources/tools/cua-helper/{package.json, runtime-manifest.json, dist, node_modules}`；`plugins.enabledPlugins.computer-use@zcode-plugins-official = true`；`tool_usage` 中 `mcp__computer-use__screenshot` 实测 8 次调用 | ZCode **自带 computer-use 插件**（带独立 runtime manifest），说明它自己就在做 UI 自动化，且这条路在这台机器上被真实用过 | 对本项目含义：ZCode 的 UIA 链可以借它自己的 cua-helper 生态，而不是我们硬造 |
| 25 | 版本变化风险 | EXPERIMENTAL | 综合 | 实测风险：① Desktop 3.12.3 vs 引擎 0.16.x **两条线**；② 22 条 schema_migration，`message.data` 内 **`modelID`/`modelId`、`providerID`/`providerId` 双命名并存** → 字段改名正在进行；③ 配置文件带版本后缀（`bot-config.json`→`bot-config.v3.json`、`bot-state.v2`→`v3`、`tasks-index` 自带 `tasks_schema_migration` 表）；④ 更新源指向 `http://localhost:8081`（本机不可达）→ 版本可能长期漂移且与官方不一致 | 最高风险项：`message.data` 的 JSON 内部键**不受 SQL schema 约束**，双命名意味着同一含义两个键并存 → 读取必须做 `COALESCE(modelId, modelID)` |

## 建议接入层级与 fallback 链

```
（Native API：不存在）
（CLI：不存在）
LOCAL_DATA（唯一主链，只读）
  ├─ 观测：session / message / part / session_entry (+sequence 游标)
  ├─ 额度：model_usage / turn_usage 聚合；套餐只有 entitlement
  ├─ 状态：tasks-index.automations / automation_runs / off_peak_tasks
  └─ 失败（库被锁/结构变更）→ READ_ONLY 降级为"离线批量导入"模式
UI_AUTOMATION（一切"写"与"实时"的唯一路径）
  ├─ 向指定会话发消息 / 改模型 / 回应审批 / 观察流式
  └─ 可复用 ZCode 自带的 computer-use 插件生态（cua-helper）
带外（架构备选，需用户决策）：微信机器人 + web-remote-control relay —— 经厂商云端，非本地
```

## 仍需 UI Automation 的能力（ZCode 是唯一"UIA 为主链"的一家）

1. **#7 向指定已有会话发消息** —— 完全无入口。
2. **#17 审批响应** —— 完全无入口。
3. **#11 会话内切换模型** —— 只能观测历史，不能触发。
4. **#8 流式响应** / **#9 中止** —— 无推送、无命令面。
5. **#14 Reset 时间** —— UIA 也拿不到（本地只有 entitlement）；应判定为"该家不提供"。

## UNKNOWN 清单与原因

| # | 能力 | 为什么是 UNKNOWN |
|---|---|---|
| 8 | 流式响应能否被外部观测 | 无 API/CLI 可试；发消息预算无法使用（没有可发消息的本地入口）。只有 DB 内的 `time_to_first_token_ms` 证明内部是流式的 |
| 16 | 待审批是否会落库 | `approval_status` 9,186 行全为 `'none'`（本机 `permission_mode` 从未触发审批）→ 结构存在，**行为无样本**。要拿样本必须真触发一次审批，而 ZCode 无自动触发入口，只能 UIA 操作，本角色未做 |
| 12 | `thought_level` 完整枚举 | 只观察到 `low`、`max`、`defaultVariant=max` 与 `variants[3 items]` 的首末元素，中间档名称未取证 → 不猜 |
| 23 | deep link 的会话粒度 | 实测需弹用户窗口，属打扰 |
| 9 | `cancelled` 状态的实际产生条件 | 枚举在 CHECK 约束里，本机无该状态的行 |
| 21/22 | 是否存在**未被本机安装包含**的官方 CLI | 只能靠安装验证，撞红线④ |

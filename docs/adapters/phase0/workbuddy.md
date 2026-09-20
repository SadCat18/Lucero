# Phase 0 调查报告：WorkBuddy

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ **两轮取证**：
  - **第 1 轮 静态**（exe 版本信息、捆绑 CLI 的 `--help`/`--version`、SQLite schema + 枚举、审计日志、注册表）
  - **第 2 轮 动态实测**（用户单次授权：启动桌面端与 CLI、起 `--serve`、在 `.scratch` 一次性项目里发真实消息）
- 本报告凡标 **[实测]** 者为第 2 轮端到端跑过；标 **[静态]** 者仍只有结构证据。**第 1 轮结论中有 5 项被第 2 轮推翻或修正，见 §修正记录。**

## 实物与版本（第 1 轮 + 第 2 轮复核）

| 组件 | 版本 | 取证方式 |
|---|---|---|
| WorkBuddy Desktop | **5.5.6** | exe FileVersion |
| 内置 CodeBuddy Code CLI | **2.137.1** | `node bin/codebuddy --version`；[实测] `/api/v1/info.version`、`/api/v1/openapi` 的 `info.version` 三处一致 |
| sandbox 组件 | 5.5.5 | `cli/vendor/sandbox/5.5.5/` |
| OpenAPI 契约 | `CodeBuddy Code API (Beta)` **3.1.0 / 113 paths** | [实测] `GET /api/openapi.json` → 200（**公开路径，免鉴权**） |
| 自动更新 | `autoUpdate.enabled = true` | [实测] `GET /api/v1/info` |

实物路径：`D:\Program Files\WorkBuddy\resources\app.asar.unpacked\cli\`（`@genie/agent-cli`，`bin/{codebuddy,cbc}`、`dist/codebuddy.js` 23 MB、`dist/codebuddy-headless.js` 19 MB、`dist/web-ui/`）
运行时要求 Node ≥ 18.20.8；本次用 `D:\DevTools\node-v24.19.0-win-x64\node.exe`。厂商 Tencent，模型网关 `https://copilot.tencent.com`。

- 真实消息预算使用：**3 / 3（已用尽）**。明细：① `POST /api/v1/jobs` 派发（落为 1 条 user 消息，模型 turn 未执行）② `POST /api/v1/jobs/{id}/reply`（`delivered:true` 但 `saved:false`，未落库未执行）③ `codebuddy -p --output-format stream-json`（**端到端成功**）。另有 2 次 HTTP 400（缺字段）未触达模型，不计入。

## 认证方式

- [实测] `GET /api/v1/auth/status` → `{authEnabled: true, authenticated: true}`，**全程未出现登录/验证码/Windows Hello 界面**，也未读取任何凭据值。
- [实测] CLI `system/init` 事件字段 `apiKeySource = "copilot.tencent.com"` → CLI **复用了既有的厂商网关登录态**，无需二次认证。
- [静态] 凭据目录本体未打开：`config ls` 暴露 `sandbox.filesystem.denyRead` 明确把
  `~/AppData/Local/CodeBuddyExtension/Data/Public/auth/` 等三份 auth 路径列为**连自家 agent 都禁止读** → 厂商自己就把凭据隔离做了，Adapter 不应尝试绕过。
- 本地另有 3 个 home：`~/.codebuddy`（CLI 态）、`~/.workbuddy`（CLI 的自定义模型/插件/provider 配置，`CustomModelsProductProvider` 日志指向此）、`D:\WorkBuddyData\.workbuddy`（桌面态）。

## 关键结构性事实（**[实测] 本节全部为跑通后的结论**）

1. **会话正文确实落在本机**，且分两个物理隔离的根：
   - CLI 族：`~/.codebuddy/projects/<cwd-slug>/<sessionId>.jsonl`
   - 桌面族：`D:\WorkBuddyData\.workbuddy/projects/<cwd-slug>/<sessionId>.jsonl`（实测 11 个 cwd-slug 目录，含 `d-WorkBuddy-2026-08-05-15-44-51` 等）
   - `cwd-slug` 生成规则 [实测]：小写盘符 + 路径分隔转 `-`（`D:\Project\...\probe-proj` → `d-Project-Lucero-.scratch-phase0-workbuddy-probe-proj`）
   - **两族互不可见**：[实测] 我派发的 CLI 会话结束后，`workbuddy.db.sessions` 仍为 **36 行、匹配我 cwd 的桌面行 = 0**。
     → Adapter 驱动 CLI 时**看不到也不碰用户桌面里已有的会话**，这是 WorkBuddy 适配范围的决定性边界。
2. 同一份会话还会在 4 处留痕（[实测] 用唯一标记串全盘比对得出）：
   `projects/<slug>/<id>.jsonl`（正文）、`jobs/<shortId>/state.json`（作业态，含 `intent/initialPrompt/respawnArgs`）、
   `logs/<date>/<projName>__<32hex>.log`（服务日志，会**原样记录 prompt 文本**）、`traces/<pid>/trace_<id>.json`。
   → 后三处同样含用户正文，**任何采集都必须脱敏**，且日志会写进 prompt 原文这一点对 §12「Secret 不得进普通日志」直接构成风险。
3. `--serve` 的鉴权是**三层**：`X-CodeBuddy-Request: 1`（CSRF 类强制头，缺失即 403）+ `Bearer <password>` 或 `?password=`；
   豁免路径含 `/api/openapi.json`、`/api/v1/auth/{status,login}`、`/api/v1/webhooks/*`、静态资源。可用 `CODEBUDDY_DISABLE_REQUEST_VALIDATION=1` 关闭。
4. CLI 的 transcript 形制与 Qoder 的**不同族**（修正第 1 轮的判断）：WorkBuddy 的 jsonl 是**扁平** `{id, timestamp, type, role, content, providerData, __codebuddyLocal, sessionId, cwd}`，
   而 Qoder 是嵌套 `{type, uuid, parentUuid, message:{role,…}, toolUseResult}`。见矩阵 §同构族修正。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | **NATIVE** [实测] | `GET /api/v1/auth/status`；`system/init.apiKeySource`；`config ls` 的 denyRead 清单 | `{authEnabled:true, authenticated:true}`，无登录弹窗；`apiKeySource="copilot.tencent.com"` | 复用范围 = **CLI 自己的认证域**；桌面凭据目录被厂商显式列为禁读。降级：`--auth none`（**安全红线，禁用**） |
| 2 | 软件版本发现 | **NATIVE** [实测] | `GET /api/v1/info` | `{cwd,version:"2.137.1",os,arch,nodeVersion,gatewayMode:"local",tunnelUrl,uptime,userName,autoUpdate:{enabled:true}}` | 只有 CLI 版本，不含桌面 5.5.6 → 需与 exe 版本信息并记 |
| 3 | 列出已有会话 | **NATIVE** [实测] | `GET /api/v1/sessions`、`/sessions/across-projects`、`/sessions/workspaces` | `across-projects` 返回 `{id,name,createdAt,updatedAt,messageCount,isCurrent,projectId,cwd,status,isPlayground,isUserDefinedTitle}`；实测列出我的 2 个探针会话 | **只列 `.codebuddy` 族**，桌面 36 个会话不在其中（实测）。降级：LOCAL_DATA 读 `.workbuddy/projects/*/*.jsonl` 文件名 + `workbuddy.db.sessions` |
| 4 | 读取完整会话 | **LOCAL_DATA + NATIVE** [实测]（第 1 轮为 UNKNOWN，**已推翻**） | 实测定位 `projects/<slug>/<id>.jsonl` 双根；`GET /api/v1/sessions/{id}/history`、`/sessions/{id}/replay`、`/jobs/{id}/transcript` | `/history` 返回 `{sessionId,name,requests:[{userInput,finalReply}]}`，**`finalReply` 精确回读到我自己造的标记串**；`/jobs/{id}/transcript` 返回 ACP `updates[]` 帧 | `/transcript` 只回放了 `user_message_chunk`，**未含 agent 输出**（实测 100s 轮询无增长）→ 读回复要用 `/history` 或 jsonl。降级：直读 jsonl |
| 5 | 新消息增量同步 | **NATIVE** [实测] | `--include-partial-messages` 的 `stream_event`；`GET /runs/{runId}/stream`、`/jobs/{id}/stream`、`/jobs/events`、`/acp`(SSE) | 一次 `-p` 跑出 **32 个 `stream_event`** 增量帧 + 2 个 `assistant` + 1 个 `result` | `jobs/{id}/transcript` 不增长（见 #4）→ 增量要靠 `/jobs/events` SSE 或 stream 端点，**不要用 transcript** |
| 6 | 双向同步 | **NATIVE** [实测]（第 1 轮 EXPERIMENTAL） | 入站 `-p`/`/api/v1/jobs`，出站 stream-json/SSE | 端到端往返成功：发进去的标记串从 `result.result` 原样回来 | 写链稳定性差（见 #7） |
| 7 | 向指定已有会话发消息 | **EXPERIMENTAL** [实测]（第 1 轮 CLI） | `POST /api/v1/jobs/{id}/reply`；CLI `-r/--resume <sessionId>`、`--session-id`、`--fork-session` | 端点存在且**语法可对**（body 必填 `text`；我给 `message` 时 400）；用 `text` 后返回 `{delivered:true, saved:false, notice:"Reply sent"}` —— **消息未落库、未触发模型 turn**（实测 messageCount 仍 1、stats 未增） | **实测不通**：`delivered` 是虚报。`-r <既有会话>` 续发因预算耗尽未测。降级：每轮起新 `-p` 进程 + `--resume`（未测）或 `sourceSessionId` 继承上下文 |
| 8 | 流式响应 | **NATIVE** [实测] | `--output-format stream-json --include-partial-messages` | 事件族实测：`system/init`、`system/status`、`file-history-snapshot`、`stream_event`×32、`assistant`、`result/success`；服务端另有 `AgentMessageChunk`/`AgentThoughtChunk`/`ToolCall`/`PlanUpdate` 等 ACP schema（openapi 44 schemas） | `-p` 的流只在该子进程生命周期内；跨进程续流要用 `/runs/{id}/stream` 或 `/acp` |
| 9 | 中止当前生成/任务 | **NATIVE** [实测]（第 1 轮 CLI） | `POST /api/v1/jobs/{id}/stop` | 实测返回 `{data:{stopped:true}}`，随后 worker 进程消失 | CLI 侧 `kill/stop <pidOrName>` 亦在。未测 `/runs/{runId}/cancel` |
| 10 | 读取模型列表 | LOCAL_DATA [实测确认来源] | 服务日志 `[CloudProductManager]`、`AgentModelResolver`、`~/.workbuddy/models.json` | 模型目录来自**云端产品配置**：`fetch cloud product from https://copilot.tencent.com/v3/config success, agents(1): cli, models(52)` → 合并本地自定义后 `product config input: 55 models, 18 agents`；`models.json` = list[3] `{id,name,vendor,url,apiKey,reasoning,supportsReasoning,supportsImages,supportsToolCall,useCustomProtocol}` | **REST 无 `/models` 端点**（113 路径实测无）；且日志出现 `[Warning] enterpriseId is missing, skipping models API call` → 企业侧模型 API 会被跳过。降级：解析 `--help` 里的 model 枚举（脆） |
| 11 | 切换模型 | CLI（[静态] + 契约级实测） | `--model`、`--fallback-model`、`--text-to-image-model`、`--image-to-image-model`；`POST /api/v1/jobs` body 有 `model` 字段 | jobs 派发体确实接受 `model`（openapi 契约） | 未用真实消息验证切换生效（预算）。降级：新会话带 `--model` |
| 12 | 推理强度/参数控制 | CLI + NATIVE(契约) | `--effort <minimal,low,medium,high,xhigh,max>`；`POST /api/v1/jobs` body `effort` **带同一 6 值 enum**；`permissionMode` 6 值 enum | 档位枚举**在 OpenAPI 里是机器可读的** → 可直接映射到 Core 的归一字段 | 实测时我给 `-p` 传的是 `--permission-mode plan`（避免写操作），未测 effort |
| 13 | 精确额度读取 | **NATIVE（读）** [实测]（第 1 轮 READ_ONLY） | `result.usage`、`result.modelUsage`；`GET /api/v1/stats`、`/stats/session` | `usage={input_tokens:26976, output_tokens:80, cache_creation_input_tokens:13216, cache_read_input_tokens:13760}`；`modelUsage.auto={inputTokens,outputTokens,cacheReadInputTokens,cacheCreationInputTokens,contextWindow:256000,maxOutputTokens:32000}`；REST 侧 `stats.modelUsage.auto` **数值与 CLI 完全一致** + `dailyModelTokens:[{date,tokensByModel:{auto:27056}}]` | `total_cost_usd` 与 `costUSD` 实测 **= 0** → 有槽位、本次为 0，**费用语义/是否恒 0 未证实**，Core 不得当作"免费"结论 |
| 14 | Reset 时间 | **UNKNOWN** | 113 路径 + stats 全量 | 无 reset/quota-period 字段 | 唯一未能升上去的一项 |
| 15 | 任务实时状态 | **NATIVE** [实测] | `GET /api/v1/jobs`、`/jobs/{id}`、`/jobs/events`；`jobs/<shortId>/state.json` | `AgentJob` 三组枚举实测可用：`state ∈ {working,blocked,done,failed,stopped}`、`status ∈ {busy,idle,waiting,stopped}`、`tempo ∈ {active,idle,blocked}`，另有 `alive/settled/firstTerminalAt/webUrl/pid/detail/waitingFor/needs/block` | [实测坑] `GET /api/v1/jobs/{id}` 与 `GET /api/v1/jobs` **形状不同**（详情端点返回的 data 里取不到 `state`，列表端点取得到）→ 别按 create/list 的形状去 parse detail |
| 16 | 审批读取 | **NATIVE（契约）+ READ_ONLY（实测）** | `InterruptionRequest` schema、`AgentJob.block.questions[]`、`result.permission_denials`、`/api/v1/permission-bridge/request`、`audit-log/*.jsonl` | 实测 `permission_denials: []`（本次 `--permission-mode plan` 无拒绝）；审计日志哈希链 `{sessionId,requestId,toolCallId,category,eventType,decision,commandPreview,commandHash,sequence,prevHash,schemaVersion}` | **未制造出一次真实待审请求** → `InterruptionRequest`/`block.questions` 只有契约证据，载荷未验 |
| 17 | 审批响应 | CLI [静态] | `--permission-mode`、`-y`、`/jobs/{id}/reply`、`/permission-bridge/request` | 权限模式可传（实测 `--permission-mode plan` 生效为只读规划） | 应答挂起审批未实测。降级：UIA |
| 18 | 附件 | READ_ONLY [静态] | `models.json.supportsImages`、`input_history.attachments`、`blobs/<2hex>/<sha>` | 未测注入 | CLI help 无 `--image` 参数（Codex 有 `-i`）→ 注入通道未证实 |
| 19 | MCP/Skill/Rule/Hook | **NATIVE** [实测]（第 1 轮 CLI） | `/api/v1/plugins*` 12 端点（list/validate/reconcile/reload/enable/disable/uninstall/update/marketplaces×5）、`/api/v1/settings*`、`/workspace-dirs*`；日志 `[MCP] reconcile revision=N projected`、`McpReadyService: headless mode — initializing MCP config (DYNAMIC scope)` | 插件市场可**浏览/增删/自动更新开关**；MCP 有 revision 化 reconcile 与 scope 模型（user→project→local，实测日志顺序） | [实测噪声] `PluginManager … Failed to install product-declared plugins: …` → 离线/网络受限时插件安装会降级但**不阻断会话** |
| 20 | 本地会话数据库 | LOCAL_DATA [实测补全] | 实测 | `workbuddy.db`(10 表, 桌面元数据) + **`.workbuddy/projects/<slug>/*.jsonl`（桌面正文，第 1 轮漏查）** + `~/.codebuddy/projects/<slug>/*.jsonl`（CLI 正文）+ `~/.codebuddy/{jobs,traces,logs}` + `audit-log/*.jsonl` + `edge-sync-mapping-v[1-4].db` + `local_storage/entry_*.info` | **双根双格式**；元数据在 SQLite、正文在 JSONL → 一个 reader 不够，要 DB+JSONL 两半 |
| 21 | CLI | **CLI** [实测端到端] | `--help` 100 行 + 21 子命令 + 实跑 | `-p --output-format stream-json` exit=0、8.8 s、`num_turns:3`、`is_error:false` | [实测坑·第 1 轮已记] `ps --json` 空集时输出**纯文本 `No active sessions.`**，非合法 JSON |
| 22 | SDK/API/App Server | **NATIVE** [实测]（第 1 轮 EXPERIMENTAL） | `--serve` 实跑；`/api/openapi.json` 公开 | **113 个 `/api/v1/*` 端点 + 44 schemas**；分组：runs(发起/查询/SSE/cancel)、sessions、jobs(agent 派发/reply/stop/respawn/transcript/stream/events)、pty、fs(E2B 对齐 9 个)、process、files、plugins、settings、storage、workspace-dirs、acp(connect/SSE/JSON-RPC/DELETE)、instances/channels(wechat/wecom + qr)、webhooks、workers/daemon、traces/stats、keybindings、permission-bridge、team/messages | **Beta 标注** + 契约漂移：`POST /api/v1/runs` 的 OpenAPI body 只写 `{text,sender}`，实跑 400 要求 `{id,type}`（generic adapter 的 `parseInbound`）→ **OpenAPI 不能全信，写侧要实跑校验** |
| 23 | 精确跳转原客户端会话 | CLI [静态] | `attach <pidOrName>`；`webUrl` 字段；`HKCR\workbuddy` URL Protocol | [实测] `jobs` 列表里每个作业带 `webUrl:"http://127.0.0.1:<port>/"`（已过滤的安全 Web UI 入口）→ 可跳到**作业级** UI | 跳到**桌面端某会话**仍未测 |
| 24 | UI Automation 可行性 | UI_AUTOMATION | Electron + `@tencent/tencent-docs-ai-engine/bin/win32-x64/editor_sdk.exe`（在跑） | 可行 | [实测后] **优先级进一步下降**：读/列/停/额度全部有 API |
| 25 | 版本变化风险 | **EXPERIMENTAL**（实测加重） | `/api/v1/info.autoUpdate.enabled=true`；`update`/`install` 子命令；`edge-sync-mapping-v[1-4].db` 四代并存；API 标 (Beta) | [实测] 自动更新**开着** → CLI 版本会自己变（2.137.1 非长期常量） | 契约漂移已实测到一例（#22）→ Core 必须按 `info.version` 做能力门控，不能只记一次 |

## 建议接入层级与 fallback 链（[实测] 修订）

**双身份必须分开实现，且互不可见（实测）：**

```
CodeBuddy CLI 族 Adapter（可控、已端到端跑通）
  写/跑：CLI `codebuddy -p --output-format stream-json --include-partial-messages`（子进程，每轮一次）
    └─ 需要常驻：`--serve` REST（/api/v1/runs、/jobs、/acp）—— 但 /jobs reply 实测 delivered 而不落库
  读：  GET /api/v1/sessions/across-projects → /sessions/{id}/history（finalReply）→ GET /api/v1/stats
    └─ 服务没起时：LOCAL_DATA 直读 ~/.codebuddy/projects/<slug>/*.jsonl（同一 cwd-slug 规则可离线复算）
  停：  POST /api/v1/jobs/{id}/stop  [实测 {stopped:true}]
  禁止：--auth none（自述允许任意本地进程执行命令/读写文件）、自动开 bypassPermissions

WorkBuddy 桌面族 Adapter（只读）
  LOCAL_DATA：.workbuddy/projects/<slug>/*.jsonl（正文） + workbuddy.db(元数据/额度/自动化) + audit-log(审批，哈希链)
    └─ 唯一写入口：UI_AUTOMATION
  ⚠ 两族 session id 不互通：CLI 看不到桌面 36 个会话（实测 workbuddy.db 未新增行）
```

## 仍需 UI Automation 的能力（实测后收缩到 3 项）

1. **#14 Reset 时间** —— 先判"该家不提供"，UIA 也未必有。
2. **#17 应答挂起的审批** —— API 有 `InterruptionRequest` 契约但载荷未验、应答路径未跑通。
3. **#23 跳到桌面端某个既有会话** —— deep link 粒度未测。
（#11 会话内改模型已从 UIA 清单移出：`POST /jobs` 接受 `model`，属新会话指定而非改既有会话。）

## 修正记录（第 1 轮 → 第 2 轮，防后续角色引用旧结论）

| 项 | 第 1 轮结论 | 第 2 轮实测结论 | 原因 |
|---|---|---|---|
| #4 读取完整会话 | UNKNOWN（正文未定位） | **LOCAL_DATA + NATIVE，正文在本机两处** | **假阴性**：`find … \| head -10` 触发 SIGPIPE 提前杀掉了 find，遍历根本没走完 `.workbuddy`，`projects/` 子树从未被看到 |
| #22 SDK/API | EXPERIMENTAL（未探测） | **NATIVE，113 端点 + 公开 OpenAPI** | 未起服务，无法知 `--serve` 的真实面 |
| #1 复用登录 | UNKNOWN（不敢读凭据） | **NATIVE**（`auth/status` + `apiKeySource`） | 凭据禁读不影响判定"是否已复用" |
| #13 额度 | READ_ONLY（本地有数无接口） | **NATIVE(读)**，CLI 与 REST 数值互证 | 只跑了 CLI 才知有 usage/stats 双出口 |
| 同构族判断 | WorkBuddy ≈ Qoder（都 Claude-Code 形制） | **只对一半**：同为 jsonl 但 WorkBuddy 扁平、Qoder 嵌套 `{message:{…}}` | 第 1 轮只看键名集合，未比对嵌套结构 |

## UNKNOWN 清单与原因（实测后剩余）

| # | 能力 | 为什么仍是 UNKNOWN |
|---|---|---|
| 14 | Reset 时间 | 113 端点与 stats 全量无 reset/quota-period 字段 |
| 7 | 对**既有会话**续发的可靠路径 | `/jobs/{id}/reply` 实测 `delivered:true / saved:false` 且不执行；`-r <sessionId>` 需再花 1 条真实消息，**预算 3/3 已用尽** |
| 11/12 | 切模型与 effort 的实际生效 | 同上，预算耗尽；只有 CLI flag 与 OpenAPI 字段级证据 |
| 16 | 待审请求的真实载荷 | 全程 `--permission-mode plan`（刻意选只读档以免改用户文件），`permission_denials:[]`，未制造出待审样本 |
| 18 | 附件注入 | CLI 无 `--image` 类参数；未测 |
| 23 | 桌面端 deep link 粒度 | 未测（会弹用户窗口） |
| 13 | `total_cost_usd=0` 的语义 | 单次样本，无法区分"套餐 0 价"与"未计费上报" |

## 残留与清理（第 2 轮授权要求写清）

- **我自己起的服务/子进程：已全部终止。**
  - `pid 22600`（`node … codebuddy --serve --port 8791 --host 127.0.0.1`）→ `taskkill /PID 22600 /T /F` 成功
  - `pid 34852`（job worker）、`pid 37388`（会话进程）→ **#stop 之后自行退出**，收尾时 `Get-Process` 已不存在
  - 端口 `8791`：`netstat` 中**已无 LISTENING**，仅剩内核层 `TIME_WAIT` 数条（会自行消失）；作业自带端口 `55518` 已释放
  - 收尾时新增的 `127.0.0.1:51218/51219` 属 **AweSun（向日葵）**，与本探针无关（已核 pid 7272）
- **未被触碰**：WorkBuddy 桌面端 13 个进程全程在跑且未受影响；`workbuddy.db` 仍 36 行（实测计数未变）；桌面配置/账号/既有会话 **0 次写入**
- **留在 WorkBuddy 侧的文件 residue（未删，删除即为对第三方数据目录的写操作）**，均在 `~/.codebuddy`：
  `projects/d-Project-Lucero-.scratch-phase0-workbuddy-probe-proj/`（2 个 jsonl，正文只有我造的标记串）、
  `jobs/5c14baca/`、`sessions/22600.json`、`logs/2026-09-19/probe-proj__de303a4f….log`、`traces/37388/`、`traces/16604/`。
  → 如需清除请由用户或主控显式授权一次删除动作。
- **一个需要主控注意的副作用**：探针目录建在 `D:\Project\Lucero\.scratch\` 内，而 `D:\Project\Lucero` 是 git 仓库，
  实测 `[GitInfoCollector] … active: <仓库地址>` → **WorkBuddy 读到了本项目的 git 仓库上下文**（只读，未产生任何文件改动）。
  下次动态探针应放在 git 仓库之外。

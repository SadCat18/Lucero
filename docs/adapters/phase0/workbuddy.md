# Phase 0 调查报告：WorkBuddy

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ 方法：**纯静态取证**（exe 版本信息、捆绑 CLI 的 `--help`/`--version` 实测、SQLite schema + 枚举、审计日志结构、注册表）
- 被调查实物与版本号（**三条独立版本线**，本身即是风险信号，见 §25）：
  - WorkBuddy Desktop **5.5.6** — `D:\Program Files\WorkBuddy\WorkBuddy.exe`（Electron，运行中，12 进程）
  - 内置 **CodeBuddy Code CLI 2.137.1** — `D:\Program Files\WorkBuddy\resources\app.asar.unpacked\cli\`
    = npm 包 `@genie/agent-cli`，`bin/{codebuddy, cbc, cbc-prewarm}`，`dist/codebuddy.js`(23 MB)、`dist/codebuddy-headless.js`(19 MB)、`dist/web-ui/`
    实测启动方式：`node bin/codebuddy --version` → `2.137.1`（要求 Node ≥ 18.20.8；本机用 `D:\DevTools\node-v24.19.0-win-x64\node.exe`）
  - 沙箱组件 **5.5.5** — `…\cli\vendor\sandbox\5.5.5\{sandbox-cli,sandbox-cli-gc,sandbox-center}.exe`（运行中）
- 厂商：Tencent（`package.json` 描述 "Use CodeBuddy, Tencent's AI assistant"；`@tencent/tencent-docs-ai-engine` 随包）；连接器市场 `https://static.workbuddy.cn/connectors-config-v2/…`
- 认证方式：**未取证** —— `D:\WorkBuddyData\.workbuddy\credentials\` 与 `qimei-cache.json`、`device-id` 属用户凭据，本角色未打开（只记录存在）。
  可程序化侧的证据是 `--serve` 的 `--auth {password,none}` 与 `mcp/{name}/auth`。
- 原始输出归档：`D:\Project\Lucero\.scratch\phase0-workbuddy\`（`cb_help.txt` 100 行、`ps.json`）
- 真实消息预算使用：**0 / 3**

## 关键结构性事实

1. **WorkBuddy = 桌面端 + 一个功能完整的 Claude-Code 形制 CLI（CodeBuddy Code）**。
   该 CLI 不是附属品，它自带 `--serve`（Web UI + **REST API** + **ACP over HTTP SSE**）与 `--acp`（Agent Client Protocol over stdio ndJson）。
   ACP 是**跨厂商标准协议**，若 Lucero 的 Adapter 协议支持 ACP，WorkBuddy 的接入成本会大幅下降。
2. 桌面端为每个会话维护一个**本机 HTTP host**，注册表实测：
   `.workbuddy/sessions/<pid>.json` → `{pid, sessionId:"interactive-<pid>", url/endpoint:"http://127.0.0.1:<port>", kind:"interactive", mode:"local", version:"2.137.1", lastHeartbeat, startedAt, updatedAt, cwd}`（本机 95 个文件）。
   → **桌面端已有会话是可寻址的**，但端口的鉴权方式未取证。
3. **会话正文（message/part）未在本机定位到**。已系统性排除，见"UNKNOWN 清单 #4"。这是五家中唯一一个"元数据齐全、正文缺位"的形态。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | **UNKNOWN** | `.workbuddy/credentials/`（未打开）、`device-id`、`storage/user-<uuid>…` 目录命名 | 未取证：目录按 user id 分域，说明存在账号作用域存储 | 原因：读取凭据目录=使用用户登录态，本角色判定不越界。降级：让 Adapter 走 CLI 的既有登录态（CLI 与 Desktop 共享 `.codebuddy`/`.workbuddy`） |
| 2 | 软件版本发现 | CLI + LOCAL_DATA | `--version`、exe VersionInfo、`sessions/*.json.version`、`sandbox/5.5.5/` 目录名 | 四条版本线同时可取：Desktop 5.5.6 / CLI 2.137.1 / sandbox 5.5.5 / 运行中 host 的 `version` 字段 | 无单一"软件版本"→ Core 必须存**多字段版本**而非一个字符串 |
| 3 | 列出已有会话 | CLI | `codebuddy ps --help`、`agents --jobs --all --cwd`；LOCAL_DATA `workbuddy.db.sessions` | 实测 `ps --json` **exit=0**；`sessions` 表 36 行含 `status,cwd,model,mode,permission_mode,thought_level,archived(deleted_at)` | **已实测的坑**：无活动会话时 `ps --json` 输出纯文本 `No active sessions.` 而**不是合法 JSON** → Adapter 解析必须先判文本哨兵。降级：LOCAL_DATA 读 `sessions` 表（含桌面端历史会话，比 `ps` 全） |
| 4 | 读取完整会话 | **UNKNOWN**（元数据 = LOCAL_DATA） | 见"UNKNOWN #4"的排除清单；`codebuddy logs <pidOrName> --help` | `sessions` 表有 title/status/model **无正文**；`tasks/<uuid>/N.json` 实测是任务项 `{subject,description,activeForm,status,id,createdAt,updatedAt}`，非对话 | 正文若在云端（`product.cloudhosted.json`、`edge-sync-mapping-v[2-4].db` 存在）则本地 Adapter 永远读不到 → **需主控决定是否授权探测 127.0.0.1 host endpoint**。降级：`codebuddy logs <pid>` 只覆盖 CLI 起的会话 |
| 5 | 新消息增量同步 | LOCAL_DATA | `sessions/*.json{lastHeartbeat,updatedAt}`、`workbuddy.db.sessions.updated_at/last_activity_at`、`audit-log/*.jsonl{sequence,prevHash}` | 审计日志是**哈希链式、带 sequence** 的追加写 → CLI 侧会话可增量；`automation_runs.runs_json` 有运行历史 | 因 #4 正文缺位，桌面端会话的增量只能靠 `updated_at` 粗粒度。降级：轮询 mtime |
| 6 | 双向同步 | EXPERIMENTAL | `--serve`(REST+SSE)、`--acp`、`sessions/<pid>.json.endpoint` | 双向机制在设计上存在（CLI 侧证据充分），**桌面端 endpoint 未探测** | 未实测。降级：CLI 单向 + DB 只读 |
| 7 | 向指定已有会话发消息 | CLI | `-r/--resume [sessionId]`、`--session-id <uuid>`、`attach <pidOrName>`、`--fork-session`、`-c/--continue`、`--remote-control [client]` | `--resume` 明确接受 session id；`attach` 可接管后台会话 | **只能对 CLI 命名空间内的会话生效**；桌面端已有会话是否共享同一 session id 空间未证实（`sessions` 表 id 与 `interactive-<pid>` 两种形态并存）。降级：向桌面端 UIA 输入 |
| 8 | 流式响应 | NATIVE（CLI 侧） | `--output-format stream-json`、`--input-format stream-json`、`--include-partial-messages`("Output raw SSE incremental messages from model requests")、`--replay-user-messages` | 帮助文本明示 **raw SSE 增量帧**可控，且支持流式**输入** | 仅 `--print` 模式下可用；且只覆盖 Adapter 自己起的会话。降级：`--output-format json` 整包 |
| 9 | 中止当前生成/任务 | CLI | 子命令 `kill <pidOrName>` / `stop <pidOrName>`（互为别名）、`--max-turns`；`tool_usage` 类语义的 `cancelled_by_user` 未见于本家但 `automation_runtime_state.running` 存在 | kill/stop 双命令 + 后台会话按 pid 或 name 定位 | 只能中止 CLI/daemon 管的会话；桌面端 turn 中止无证据。降级：UIA 点停止 |
| 10 | 读取模型列表 | LOCAL_DATA + CLI | `.workbuddy/models.json`、`--help` 中 `--model` 的枚举说明 | `models.json` = list[3]，元素键 `{id,name,vendor,url,apiKey,reasoning,supportsReasoning,supportsImages,supportsToolCall,useCustomProtocol}`；help 直列 12 个可用 model id（auto / glm-* / kimi-* / minimax-* / deepseek-* / custom-local:*） | `models.json` 是**自定义 provider 表**，不等于全量可选模型（后者在 help 文本里，随版本变）。降级：解析 `--help`（脆，标注为临时手段） |
| 11 | 切换模型 | CLI | `--model`、`--fallback-model`、`--text-to-image-model`、`--image-to-image-model` | 四类模型分别可指（对话/回退/文生图/图生图）；`sessions.model`、`automations.model_id`、`model_is_thinking` 可观测 | **进程启动时参数**，未见"对已有会话改模型"的接口 → 切换 = 新会话。降级：UIA 下拉 |
| 12 | 推理强度/参数控制 | CLI + LOCAL_DATA | `--effort <minimal,low,medium,high,xhigh,max>`；DB `sessions.thought_level`、`automations.model_is_thinking`、`context_window`；`--autocompact <auto\|tokens>` | effort 六档枚举实测存在于 help；DB 有对应列 | DB 列的实际取值分布未验证（未查非空率）。降级：`--autocompact` 控制窗口 |
| 13 | 精确额度读取 | READ_ONLY | `workbuddy.db.session_usage`(29 行) 列 `{session_id, used, size, updated_at, credit_json}` | **`credit_json` 字段名直接指向额度**；`used`/`size` 是配额用量 | `credit_json` 内容与单位语义未取证（可能含账户标识）。判定为 READ_ONLY：本地有数据但无对外查询接口。降级：无 |
| 14 | Reset 时间 | **UNKNOWN** | 同上 + `automations.{rrule,next_run_at}` | 未找到 quota-reset 字段；`next_run_at` 是**定时任务**时间不是额度重置 | 不要把 `next_run_at` 误读为 reset 时间（易错点，特此标注） |
| 15 | 任务实时状态 | LOCAL_DATA + CLI | `sessions.status`(默认 `'Pending'`)、`automation_runtime_state{running,running_started_at,running_conversation_id}`、`automation_runs{status,dispatch_status,outcome,result_success,error}`、`tasks/<uuid>/N.json.status`、`ps --json` | 四级状态齐全：会话 / 自动化 / 单次运行 / 任务项；`automation_runs` 实测 18 行 | `sessions.status` 的完整枚举未取到（只观察到默认值 `Pending`）→ **禁止猜枚举**。降级：`codebuddy agents --jobs --all` |
| 16 | 审批读取 | LOCAL_DATA | `audit-log/YYYY-MM-DD.jsonl` + `audit-log/manifest.jsonl` + `mcp-approvals.json`(2 B 空) | 审计行结构实测：`{sessionId, requestId, toolCallId, category, eventType, decision, commandPreview, commandHash, messageKey, messageParams, timestamp, source, schemaVersion, id, sequence, prevHash}`；`manifest.jsonl` = `segment-closed` 分段落盘 `{entryCount, firstHash, lastHash, fileSha256}` | 这是**已决策的事后审计**，不是"待批队列"。取不到 pending 状态 → 无法实时观测审批。且 `commandPreview` 含用户命令原文，**入库前必须脱敏**（对齐红线：Secret/正文不得进普通日志） |
| 17 | 审批响应 | CLI | `--permission-mode <acceptEdits,bypassPermissions,default,plan,dontAsk,auto>`、`-y/--dangerously-skip-permissions`、`--permission-mode-before-plan`、`--subagent-permission-mode`、`--brief`(SendUserMessage) | 六档权限模式，且能为子代理单独设档；`--settings`/`--setting-sources` 可外部注入 | 全是**启动时**决定，不是"回应一个正在挂起的审批"。真正的 pending-approval 应答在 `--serve`/`--acp` 通道，**未探测**。降级：UIA 点按钮 |
| 18 | 附件 | READ_ONLY | `models.json.supportsImages`、`input_history.attachments`（实测 `{prompt 86, steered_input 14}` 中 **24 条带 attachments**）、`.workbuddy/clipboard-images/`、`blobs/<2hex>/<sha>.{png,jpg}` | 附件以**内容寻址 blob** 落盘（sha256 文件名 + 两级桶目录） | CLI help 中**未见** `--image` 类参数（Codex 有 `-i/--image`）→ 主动上传附件的通道未证实。降级：写文件进 cwd |
| 19 | MCP/Skill/Rule/Hook | CLI（五家最强） | 子命令 `mcp {add,add-json,remove,list,get}`、`plugin`、`config`；flag `--mcp-config <fileOrString>`、`--strict-mcp-config`、`--channels <plugin:name@marketplace \| server:configName>`、`--plugin-dir <dirs…>`、`--agents <json>`、`--settings`；目录 `skills/`、`plugins/`、`connectors/`、`connectors-marketplace/`、`experts/`、`.workbuddy/skills/` | MCP 可**每次调用注入**（`--mcp-config` + `--strict-mcp-config` 隔离）= 最适合被 Adapter 编排；插件有 marketplace 与 dev 目录两种来源 | `--dangerously-load-development-channels` 提示渠道有白名单管控。降级：读 `.workbuddy/settings.json` |
| 20 | 本地会话数据库 | LOCAL_DATA | 实测 open | `D:\WorkBuddyData\.workbuddy\workbuddy.db`(651,264 B, **10 表**, drizzle 迁移) + `edge-sync-mapping-v[1-4].db` 四代并存 + `local_storage/entry_*.info`(gzip+base64 键值) + `storage/{device,skeleton,user-<uuid>[-personal]}/{global,scoped}/` | 存储**分域且多代并存**；正文不在这些位置（见 UNKNOWN #4） |
| 21 | CLI | CLI | 实测 | 21 个子命令：`config mcp sandbox plugin doctor update install daemon ps logs attach kill stop rm respawn agents auto-mode agents(重名) + 主会话`；`--help` 100 行、`--version` 2.137.1 | `agents` 在 help 中出现**两次且语义不同**（agent view / 列出 agents，后者"requires --json"）→ 同一命令名两种行为，脚本化需实测确认 | 
| 22 | SDK/API/App Server | EXPERIMENTAL | `--serve`("Start HTTP server (Web UI, REST API, ACP over HTTP SSE). Without --acp, ACP does not use stdio")、`--acp-transport stdio\|streamable-http`、`--port`、`--host`、`--auth password\|none` | **ACP（Agent Client Protocol）** = 标准化 agent 驱动协议，ndJson 帧；REST + Web UI 同进程 | 未探测任何端点（避免使用用户凭据、避免在用户机器上开服务）。`--auth none` 的 help 自述"让任意本地进程通过服务执行命令和读写文件"→ **安全红线**，Core 侧禁用 |
| 23 | 精确跳转原客户端会话 | CLI | `attach <pidOrName>`、`HKCR\workbuddy` 注册 `URL Protocol`；`sessions/<pid>.json.endpoint` | attach 是按会话进入；deep link 存在但粒度未测 | Desktop 会话与 CLI 会话是否同一 id 空间未证实（见 #7）。降级：UIA |
| 24 | UI Automation 可行性 | UI_AUTOMATION | Electron；`WorkBuddyShareTarget.exe`、`resources/app.asar.unpacked/node_modules/@tencent/tencent-docs-ai-engine/bin/win32-x64/editor_sdk.exe` 在跑 | 可行 | 优先级低；仅用于 #17 pending 审批与 #11 会话内改模型 |
| 25 | 版本变化风险 | EXPERIMENTAL | 综合 | 实测风险：① 三/四条版本线独立演进（5.5.6 / 2.137.1 / 5.5.5）；② CLI 有 `update`/`install [target]` **自升级**能力 → 版本会自己变；③ `edge-sync-mapping.db` 存在 **v1/v2/v3/v4 四代**并存；④ 产品形态有 `product.{cloudhosted,internal,ioa,selfhosted}.json` 四种构建 | 高风险。Core 必须**按 CLI 版本而非 Desktop 版本**给 WorkBuddy 定契约，并把 `update` 关掉（若可配） |

## 建议接入层级与 fallback 链

**双身份必须分开设计**：`WorkBuddy Desktop`（观察为主）与 `CodeBuddy CLI`（可控为主）不是同一个 Adapter。

```
CodeBuddy CLI 链：
  EXPERIMENTAL: codebuddy --acp（ACP/ndJson）—— 若 Adapter 协议支持 ACP，这是首选
    └─ 失败 → CLI: codebuddy -p --output-format stream-json --include-partial-messages（每 turn 一子进程）
         └─ 失败 → LOCAL_DATA: 只读 workbuddy.db（元数据/额度/审计）
WorkBuddy Desktop 链：
  LOCAL_DATA(只读 sessions/automations/session_usage/audit-log)
    └─ 「向已有桌面会话发消息 / 读正文」：当前 UNKNOWN → 需先探测 sessions/<pid>.json 的 127.0.0.1 endpoint
         └─ 失败 → UI_AUTOMATION
```

## 仍需 UI Automation 的能力

1. **#17 对待审请求的实时响应** —— 桌面端的 pending 审批无法从审计日志读到（只有已决策记录），CLI 侧权限模式又是启动时参数。
2. **#11 在已有会话中切换模型** —— CLI 只有启动时 `--model`。
3. **#4 读取桌面端已有会话正文** —— 若 endpoint 探测失败，则只剩 UIA 滚动读取（**不推荐**，见矩阵结论）。

## UNKNOWN 清单与原因

| # | 能力 | 为什么是 UNKNOWN |
|---|---|---|
| 1 | 复用原客户端登录 | 认证材料在 `credentials/` 目录内，读取=使用用户登录态，本角色未越界 |
| 4 | **读取完整会话（桌面端正文）** | 系统性排除后仍未定位：`sessions/*.json` 只有运行主机注册表、`local_storage/` 只有 `.info`(gzip)、`app/data/` 只有 `expert-history.json`、`blobs/` 是内容寻址图片、`tasks/<uuid>/N.json` 是任务项、`find -name "*.jsonl"` 在 `.workbuddy` 树内**只命中 `audit-log/`**、`storage/user-*/global/conversations.json` 实测只有 `pinned[{groupKey,id}]`。剩余可能是云端（存在 `product.cloudhosted.json` 与 `edge-sync-mapping-v4.db`）或未探测的 `sessions/<pid>.json.endpoint` HTTP 面 |
| 6/22 | `--serve` / `--acp` 的实际 REST 与 ACP 帧 | 探测要在用户机器上启动一个可执行命令的本地服务（help 自述 `--auth none` 允许任意本地进程执行命令/读写文件）→ 属高风险副作用，本角色未做，**建议单独立项申请授权后在 `.scratch` 沙箱内做** |
| 13 | `session_usage.credit_json` 的字段与单位 | 内容可能含账户标识，未展开；只确认字段存在 |
| 14 | Reset 时间 | 无任何证据 | 
| 15 | `sessions.status` 完整枚举 | 本机只观察到 DDL 默认值 `Pending`，**禁止把默认值猜成全集** |

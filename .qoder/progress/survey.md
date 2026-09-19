OWNER_ROLE: survey
TASK_ID: phase0-survey
STATUS: in_progress
SCOPE: Phase 0 五家 AI 软件能力调查，只产出 `docs/adapters/phase0/*.md` 与 `PHASE0_CAPABILITY_MATRIX.md`；不写业务代码、不改架构文档、不碰任何第三方软件配置
EVIDENCE_DIR: D:\Project\Lucero\.scratch\phase0-<vendor>\
LAST_UPDATED: 2026-09-19T15:40+08:00

## Objective

把 `docs/adapters/PHASE0_ADAPTER_CAPABILITY_SURVEY.md` 的 25 项 × 5 家从 TBD 填成**有证据**的能力矩阵，
并产出：每家一份 phase0 报告、汇总分级与优先级、每家接入层级 + fallback 链、仍需 UI Automation 的能力清单。

## Completion criteria

- [ ] `docs/adapters/phase0/codex.md` 存在，25 项每项有等级 + 证据来源 + 版本号；无证据项为 UNKNOWN 并写明原因
- [ ] 同上 `qoder.md` `opencode.md` `zcode.md` `workbuddy.md`
- [ ] `docs/adapters/PHASE0_CAPABILITY_MATRIX.md` 存在，含分级打分、优先级排序、fallback 链、UI Automation 待办清单
- [ ] 全矩阵零猜测：每个非 UNKNOWN 格子可回溯到 `.scratch/` 里的一条原始输出
- [ ] 报告内零用户隐私：只有字段名、结构、枚举值、脱敏样例（无 token / account_id / 对话正文 / 仓库 URL / 本地路径中的项目名）

## Scope boundaries

- In scope: `docs/adapters/phase0/`、`docs/adapters/PHASE0_CAPABILITY_MATRIX.md`、本文件、`.scratch/phase0-*/`
- Out of scope: 四份架构文档（红线①）、`core/migrations/`、git 任何写操作（红线②）、安装/卸载软件（红线④）
- Out of scope: `docs/architecture/CORE_DOMAIN_MODEL_V0.1.md` 的 A/B 分叉（ADR-000 D1~D4 裁决中，本角色不依赖它）
- 红线⑤已由用户在本会话指令中逐次授权：只读浏览 + 启动观察 + 每家 ≤3 条真实消息（仅限 `.scratch/phase0-<vendor>/` 空项目）
- 自设约束：**未动用任何一条真实消息预算**。五家的定级全部由静态证据（CLI --help / 协议 schema / 二进制字符串 / SQLite schema / 注册表 / netstat）支撑，无需发消息

## Current checkpoint

- Stage: 静态证据采集**全部完成**（5/5 家）；进入产出物阶段
- Status: in_progress
- Last verified action: `netstat -ano -p tcp` → ZCode 16 个进程**无一监听 TCP**；Qoder pid 24480 监听 `127.0.0.1:58129`（与 `mcp-router.json.baseUrl` 端口一致，实测对上）；五家中无 Codex daemon 在监听
- Next action: 写 `docs/adapters/phase0/opencode.md`（25 项逐格，证据引用 E7~E12）
- Verification: 文件存在且 25 行能力表全部有等级与证据列，UNKNOWN 项均带原因

## Completed

- 装载 `ponytail`(full) + `project-checkpoint`；通读 4 份必读文件（调查表 / 架构 §9 / orchestration / ADR-000）
- 建立 `docs/adapters/phase0/` 目录
- **五家静态证据采集全部完成**，逐家定级依据见 Verification evidence 段 E1~E16
- 实物确认：Codex CLI `codex-cli 0.154.0` + Desktop `OpenAI.Codex_26.915.3509.0`（MSIX，进程名 ChatGPT.exe）；
  Qoder `0.3.3`（引擎 `entrypoint=cli`，transcript 内 `version=1.1.53`）；OpenCode Desktop `1.18.14`；
  ZCode `3.12.3.7463`（引擎 `session.version=0.16.x`）；WorkBuddy Desktop `5.5.6` + 内置 CodeBuddy CLI `2.137.1`
- 主目录定位纠偏一次：`D:\ZCodeFile\.zcode` 是**工作区目录**，真正的 app home 是 `C:\Users\韩涵\.zcode\`，
  其 `cli\db\db.sqlite`(208 MB) 才是 ZCode 的会话主库 —— 若未纠正会把 ZCode 错评为"只有索引 + 只能 UI Automation"

## Remaining

- 五份 phase0 报告落盘（codex 已完成则余 4 份）
- `PHASE0_CAPABILITY_MATRIX.md`：分级打分 + 优先级 + fallback 链 + UI Automation 待办清单
- 待主控决定的可选深挖：Qoder MCP router(127.0.0.1:58129) 的实际 REST 面 —— 需使用 `mcp-router.json.apiKey`，
  属"使用用户登录态"，本角色**未做**，需另行授权

## Blockers

- 无阻塞性 blocker。B1（ADR-000 裁决）不影响本角色。
- **需主控上报用户的两条红线⑦类冲突候选（详见 Coordination）：**
  1. Z Code 桌面端 **0 个本地监听端口、无 CLI 实物**，但其 208 MB 本地库能力极完整
     → 「Native API → CLI → Local Data → UI Automation」降级链在 ZCode 上**跳过前两级**，
     且「向指定已有会话发消息」在 ZCode 上只能 UI Automation 或写库（写库=改第三方数据，本角色判定为禁区）。
  2. Qoder **无流式、无中止、无模型切换、无额度**的对外接口（只有 7 个 builtin 会话工具，粒度为 turn 级），
     而它是本项目宿主。若 Adapter 协议把"流式响应/精确额度"当作各家的基本盘，Qoder 会大面积 UNSUPPORTED。

## Coordination

- → 主控：`specs/adapter-protocol`（adapter 角色）握手声明 Capability 时，**Codex 有现成的机器可读契约**：
  `codex app-server generate-json-schema --out <dir> --experimental` 每次升级可重新生成 379 个 v2 schema。
  建议 capability 枚举直接对齐该 bundle，不要自造。
- → 主控：**OpenCode 与 Z Code 的本地库 schema 高度同构**（`session` 表 20/20 列同名，
  `slug/share_url/summary_additions/summary_diffs/revert/permission/time_compacting/time_archived` 全一致；
  `session_input`、`todo`、`permission`、`part` 同族）→ 一个 LOCAL_DATA reader 可能同时覆盖两家的只读链。
- → 主控：**WorkBuddy(=CodeBuddy Code) 与 Qoder 的 transcript 都是 Claude-Code 形制**
  （uuid/parentUuid/toolUseResult/permissionMode/usage）→ 第二组同构族。
  结论：五家实际只有 **3 种本地数据族**，Adapter 协议不必为每家设计独立 schema。
- → 主控：颠覆性事实（会推翻"每家都有 API"这一隐含假设）：ZCode 无 API/无 CLI；WorkBuddy 会话正文未在本机定位。

## Changed files

- `docs/adapters/phase0/` (新建目录)
- `.qoder/progress/survey.md` (新建/更新) — 本文件
- `.scratch/phase0-codex/` help_*.txt × 9、schema_x/（47 顶层 + v1 + v2 379 文件）、probe_rollout.py、gen.log
- `.scratch/phase0-opencode/` db_schema.py、db_probe.py（+ 路由/SSE 扫描的一次性内联脚本输出）
- `.scratch/phase0-zcode/` probe.py（dbschema.py 已上移到 `.scratch/` 供五家复用）
- `.scratch/phase0-workbuddy/` cb_help.txt、ps.json
- `.scratch/phase0-qoder/` （transcript schema 探针为内联执行，输出记在本文件 E15）
- `.scratch/dbschema.py`、`.scratch/redact_json.py` — 两个跨家复用的小工具

## Verification evidence

### Codex（详见 `docs/adapters/phase0/codex.md`）

- **E1** `codex.exe --version` → `codex-cli 0.154.0`；实物 `…\AppData\Local\Programs\OpenAI\Codex\bin\codex.exe`
  (298,169,136 B) → 软链 `~/.codex/packages/standalone/current/bin`
- **E2** `codex --help` → 25 子命令；`codex exec --help` 有 `--json`(JSONL 事件流)、`--output-schema`、`--ephemeral`、
  `--thread-source`；`codex queue --thread <UUID|name> --message <TEXT> [-i 图片]`；
  `codex resume [SESSION_ID] --last --all --include-non-interactive`
- **E3** `codex app-server generate-json-schema --out schema_x --experimental` **exit=0** →
  `ClientRequest.json`(281 KB) + v1(2 文件) + **v2(379 文件)**。方法名集覆盖 25 项中 ≥18 项（全量清单已落盘 schema_x/）
- **E4** `AccountRateLimitsUpdatedNotification.json` 属性：`rateLimits[RateLimitSnapshot]{limit,used,usedPercent,remainingPercent,windowDurationMins,resetsAt}`、
  `credits{balance,hasCredits,unlimited}`、`planType`、`spendControlReached`、`rateLimitReachedType`
- **E5** `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl` 129 个文件；行 `{timestamp,ordinal,type,payload}`，
  type ∈ `session_meta|event_msg|response_item|world_state|turn_context|compacted`；**`ordinal` 单调 → 增量游标**；
  `session_meta` 含 `session_id,cwd,originator,cli_version,source,thread_source,history_mode("paginated"),git{…}`；
  `turn_context` 含 `turn_id,approval_policy,approvals_reviewer,sandbox_policy,permission_profile,model,effort(high|xhigh),collaboration_mode{settings{model,reasoning_effort}}`
- **E6** `~/.codex/auth.json` 键名（值未读）：`auth_mode`、`OPENAI_API_KEY`(null)、`tokens{id_token,access_token,refresh_token,account_id}`、`last_refresh`；
  `codex features list` → ~70 flag，`stable true` 含 `computer_use,browser_use*,apps,auth_elicitation,code_mode_host`，
  `under development false` 含 `code_mode*,exec_permission_approvals,apply_patch_streaming_events`
- **E16** `netstat -ano -p tcp` → 21 个 loopback 监听中**无 Codex/ChatGPT 进程** → app-server daemon 当前未常驻

### OpenCode（详见 `docs/adapters/phase0/opencode.md`）

- **E7** 版本：`OpenCode.exe` FileVersion/ProductVersion = **1.18.14**；`app-update.yml` → github `anomalyco/opencode`；
  实物 `%LOCALAPPDATA%\Programs\@opencode-aidesktop\`（Electron，app.asar 138,692,922 B）
- **E8** 本地数据：junction `%USERPROFILE%\.local\share\opencode` → `D:\opencode\data`；
  `opencode.db` **559,341,568 B**，19 表 + 16 索引。事件溯源：`event{id,aggregate_id,seq,type,data}` + `event_sequence{aggregate_id,seq,owner_id}`
- **E9** `event.type` 实测枚举（4 种）：`message.part.updated.1`(6270) / `message.updated.1`(1903) / `session.updated.1`(525) / `session.created.1`(9)；
  单会话 `seq` 实测范围 `0..1547` → 增量游标可用
- **E10** `part.data.type` 实测：`tool`(726) / `step-start`(464) / `step-finish`(463) / `reasoning`(332) / `text`(288) / `patch`(91) / `file`(4)；
  assistant `message.data` 含 `cost,tokens{cache,input,output,reasoning,total},modelID,providerID,agent,mode,variant,path,parentID`
- **E11** HTTP 面（grep app.asar 得到的路由字面量，141 条 v1 + 60 条 `/api/*` v2）：
  `/session/{id}/message|prompt_async|abort|permissions/{permissionID}|fork|revert|unrevert|share|shell|summarize|todo|command|diff|children|init`、
  `/session/status`、`/event`(SSE，`text/event-stream` 31 处命中)、`/global/health|event|config|dispose|upgrade`、
  **`/experimental/capabilities`**、`/permission/{requestID}/reply`、`/question/{requestID}/reply|reject`、
  `/provider`、`/models`、`/mcp/{name}/connect|disconnect|auth/authenticate|auth/callback`、`/tui/select-session|open-sessions|submit-prompt|execute-command`、
  `/api/session/{id}/interrupt|wait|history|event|model|agent|compact|context|prompt|permission/{requestID}/reply|question/{requestID}/reply|revert/{stage,commit,clear}`、
  `/api/session/active`、`/api/permission/{request,saved}`、`/api/integration/{id}/connect/{key,oauth}`、`/api/pty/{id}/connect-token`、`/api/{model,provider,health,event,skill?/command/agent}`
- **E12** 认证：`D:\opencode\data\auth.json` 结构 = 按 providerId 分桶的 `{type,key}`（本机有 2 个 provider，值未读）；
  `account`/`control_account`/`credential` 表 **rows=0** → 未登录 OpenCode 账号，走 provider API Key 直连
- 版本目录 `D:\opencode\cache\models.json` 4,690,520 B = models.dev 目录，221 个 provider 键（模型列表的 LOCAL_DATA 来源）

### ZCode（详见 `docs/adapters/phase0/zcode.md`）

- **E13** 版本：`ZCode.exe` FileVersion = **3.12.3.7463**；Electron；引擎 bundle `resources/glm/zcode.cjs`(11,416,833 B)；
  `resources/tools/{cua-helper,ripgrep,ugrep}`；`app-update.yml` → `provider: generic, url: http://localhost:8081`
- **E14** 主库 `C:\Users\韩涵\.zcode\cli\db\db.sqlite` **208,707,584 B**，23 表：
  `session`(89) / `message`(12213, 有 `sequence`) / `part`(46775, 有 `sequence`) / `session_entry`(2026) /
  **`session_input`(901)** / `model_usage`(8264) / `turn_usage`(696) / `tool_usage`(9186) / `permission`(0) /
  `todo`(241) / `input_history`(100) / `session_target` / `session_task_link` / `workflow_*` / `dwf_*` / `schema_migration`(22)
- **E14b 实测枚举**：`session_input.delivery ∈ {startNow(691), queue(209), guide(0 未触发)}` × `status ∈ {promoted,cancelled,discarded}`；
  `part.data.type ∈ {tool,step-start,step-finish,reasoning,text,file,timeline,compaction}`；
  `session.task_type ∈ {interactive(35), subagent_child(54)}`；`session.version ∈ {0.16.1,0.16.3,0.16.5}`；
  `schema_migration.app_version` 跨 0.2.0→0.16.5；`tool_usage.approval_status` **9186 行全为 `'none'`**（本机从未触发审批）；
  `tool_usage.tool_name` 含 `Bash,Read,Edit,Write,TodoWrite,WebFetch,WebSearch,Agent,Skill,TaskOutput,TaskStop,AskUserQuestion,mcp__computer-use__screenshot`；
  `model_usage` 有 `input_tokens`(实测 98188)/`output_tokens`/`reasoning_tokens`/`cache_read_input_tokens`/`time_to_first_token_ms`/`status`；
  `input_history.kind ∈ {prompt(86), steered_input(14)}`，其中 24 条带 `attachments`
- **E14c 无 API 面**：`netstat -ano -p tcp` → ZCode 的 16 个进程**无一为 LISTENING**；
  `find ~/.zcode "/d/Program Files/ZCode" -iname "*zcode*.exe"` → 只有 `ZCode.exe` 与卸载器，**无 CLI 可执行体**；
  zcode.cjs 内 http 路由字面量只有远端 `/chat/completions`、`/api/v1/agent/configs`、`/api/v1/client/configs`（非本地服务）
- **E14d 认证**：`~/.zcode/v2/credentials.json` 键名（值未读）：`oauth:bigmodel:access_token`、`oauth:active_provider`、
  `zcodejwttoken`、`web-remote-control:external-relay:pass_hash`、`account-provider:coding-plan:account:*:api-key`；
  `config.json` 内 provider `kind=anthropic` + `options.{apiKey,baseURL}`；
  `coding-plan-cache.json` 实测只有**套餐资格** `{status: available|unavailable, reason: coding_plan_not_entitled}` → 无数字额度
- **E14e skills/plugins**：`~/.zcode/skills/{glm-vision,ima-skill,image-2-skill}`、`~/.zcode/cli/plugins/`、
  `~/.zcode/cli/config.json` 的 `mcp.servers.node_repl{type:stdio,command:node,args,enabled}` 与 `plugins.enabledPlugins{freeglm-core@freeglm, cloudbase-skills@zcode-plugins-official, computer-use@zcode-plugins-official}`

### WorkBuddy（详见 `docs/adapters/phase0/workbuddy.md`）

- **E15** 版本：`WorkBuddy.exe` FileVersion = **5.5.6**；内置 CLI `…\resources\app.asar.unpacked\cli\`
  = `@genie/agent-cli`，`bin/{codebuddy,cbc,cbc-prewarm}`，`dist/{codebuddy.js(23 MB), codebuddy-headless.js(19 MB), web-ui/}`；
  `node bin/codebuddy --version` → **2.137.1**（Node ≥18.20.8 要求，实测用 `D:\DevTools\node-v24.19.0-win-x64\node.exe`）
- **E15b CLI 面**：`--help` 100 行 + 21 个子命令。关键：`-p/--print`、`--output-format text|json|stream-json`、
  `--input-format text|stream-json`、`--include-partial-messages`、`--json-schema`、`--replay-user-messages`、
  `-c/--continue`、`-r/--resume [sessionId]`、`--session-id <uuid>`、`--fork-session`、`--no-session-persistence`、
  `--serve`(Web UI + REST API + ACP over HTTP SSE) + `--port/--host/--auth password|none`、
  `--acp` + `--acp-transport stdio|streamable-http`、`--effort <minimal,low,medium,high,xhigh,max>`、`--max-turns`、
  `--permission-mode <acceptEdits,bypassPermissions,default,plan,dontAsk,auto>`、`--allowedTools/--disallowedTools/--tools`、
  `--mcp-config/--strict-mcp-config`、`--agents <json>/--agent`、`--settings/--setting-sources`、`--bg/--background/--name/--exec`、
  `--swarm`、`--remote-control [we…]`、`--sandbox container|<E2B url>`、`--worktree`、`--model`(help 内直列 12 个模型 id)、`--fallback-model`
- **E15c 会话管理子命令**：`ps --json`(实测 exit=0，**无活动会话时输出纯文本 `No active sessions.` 而非合法 JSON** → 已记录的坑)、
  `agents --jobs --all --cwd`、`logs <pidOrName>`、`attach`、`kill`/`stop`、`rm`、`respawn`、`daemon`、
  `mcp {add,add-json,remove,list,get}`、`plugin`、`config`、`doctor`、`update`、`install`
- **E15d 本地库** `D:\WorkBuddyData\.workbuddy\workbuddy.db`(651,264 B) 10 表：
  `sessions`(36：`status` 默认 `'Pending'`,`cwd`,`model`,`mode`,`permission_mode`,`thought_level`,`context_window`,`expert_id`,`deleted_at`)、
  **`session_usage`(29：`used`,`size`,`credit_json`)**、`automations`(3：`rrule`,`cron`,`push_to_wechat`,`push_to_wecom_bot`,`skills_json`,`connector_ids_json`,`model_is_thinking`)、
  `automation_runs`(18)、`automation_runtime_state`(2)、`workspaces`(3)、`buddy_snapshots`、`migration_meta`、drizzle 迁移表
- **E15e 审批/审计**：`D:\WorkBuddyData\.workbuddy\audit-log\YYYY-MM-DD.jsonl` 行结构
  `{sessionId,requestId,toolCallId,category,eventType,decision,commandPreview,commandHash,messageKey,messageParams,timestamp,source,schemaVersion,id,sequence,prevHash}`
  + `manifest.jsonl{event:"segment-closed",segment,entryCount,firstHash,lastHash,fileSha256}` → **哈希链式审计日志**；
  `mcp-approvals.json` = 2 B（空）；`tasks/<uuid>/N.json` = `{subject,description,activeForm,status,id,createdAt,updatedAt}`(任务清单，非正文)
- **E15f 运行主机会话注册表**：`.workbuddy/sessions/<pid>.json`（95 个）实测结构
  `{pid,lastHeartbeat,sessionId:"interactive-<pid>",cwd,startedAt,kind:"interactive",url:"http://127.0.0.1:<port>",endpoint:"http://127.0.0.1:<port>",mode:"local",version:"2.137.1",os,arch,hostname,updatedAt}`
  → 桌面端每个会话背后是一个本机 HTTP host；**但会话正文(message/part)未在 `.workbuddy` 树内定位到**
  （已排除：sessions/*.json 仅注册表、local_storage 只有 `.info` gzip 项、app/data 只有 expert-history.json、
  blobs 是内容寻址 png/jpg、find *.jsonl 只命中 audit-log）
- **E15g 模型清单**：`.workbuddy/models.json` = list[3]，元素键 `{id,name,vendor,url,apiKey,reasoning,supportsReasoning,supportsImages,supportsToolCall,useCustomProtocol}`

### Qoder（详见 `docs/adapters/phase0/qoder.md`）

- **E17** 版本：`D:\Qoder\.qoder-versions\0.3.3\Qoder.exe`；transcript 内 `version="1.1.53"`、`entrypoint="cli"`、`userType="external"`、`gitBranch="HEAD"`
- **E18 对外接口 = 7 个 builtin MCP 工具**（本机实测 `mcp_list` 描述原文）：
  `create_chat_session`(仅 `prompt` 参数)、`list_chat_sessions`("ordered by most recent update and contain IDs and current state, not transcripts"、有 `nextCursor`)、
  `read_chat_session`("**bounded snapshot** … may be truncated; `hasMoreBefore`")、
  `wait_chat_sessions`(**≤8 会话**、返回 `turn_completed|idle|fails|interrupted|needs_attention`)、
  `send_message_to_chat_session`("If the target is running, its normal steering or queueing policy applies")、
  `fork_chat_session`(`same-directory` 默认 / `worktree` 需 git)、`qoder_cron`
- **E19 本地 HTTP router**：`D:\QoderData\mcp-router.json` = `{schemaVersion:2,pid,baseUrl:"http://127.0.0.1:58129",apiKey,startedAt}`；
  `netstat` 实测 pid 24480 **确在 LISTENING 127.0.0.1:58129** → router 存活。**未探测其 REST 面**（需使用 apiKey = 使用用户登录态，越界）
- **E20 transcript**：`D:\QoderData\projects\<workspace-slug>\<sessionId>.jsonl`（本工作区 2 个会话：1,236,497 B / 902,611 B）
  + 同名目录 `{compression-v2, state.json{sessionId,revision=202,createdAt,updatedAt,items.s0{c,u,n,p(len452),t}}}`；
  行 type ∈ `workspace-directories|runtime-config|user|assistant|attachment|active-leaf|file-history-snapshot`；
  实测 `(type,message.role)` 仅 `assistant/assistant`(221) 与 `user/user`(102)；
  顶层键 `{type,sessionId,timestamp,uuid,parentUuid,isSidechain,cwd,userType,entrypoint,version,gitBranch,message,leafUuid,explicit,promptId,sourceToolAssistantUUID,toolUseResult,attachment,messageId,snapshot,isSnapshotUpdate,model,reasoningEffort,contextWindow,generation}`
  → `uuid/parentUuid` 树 + `active-leaf` 分支游标 + `isSidechain` 子链 + `attachment` 类型 + `runtime-config` 携带 `model/reasoningEffort/contextWindow/generation`
- **E21 额度实测为 0**：assistant `message.usage` 结构存在
  `{input_tokens,cache_creation_input_tokens,cache_read_input_tokens,output_tokens,server_tool_use{web_search_requests,web_fetch_requests},service_tier:"standard",…}`
  但本机实测**全部字段值为 0** → 有槽位无数据；`reasoningEffort` 实测 `null`；`contextWindow` 实测 `null`/`1000000`
- **E22 模型目录不可读**：`D:\QoderData\.models\default` = `{key:"qmodel_38max",uid,scene:"app",updatedAt}`（107 B，明文）；
  `.models\<uid>\catalog-v6`(95,580 B) 首 600 B 为**非 JSON 的 base64 密文** → 模型列表无法从本地读出
- **E23 跳转/自动化**：注册表 `HKCR\qoder` 与 `HKCR\qoder-app` 均有 `URL Protocol`；
  `HKCR\qoder\shell\open\command` = `"…\Qoder Launcher.exe" "%1"`（`zcode://` 同理为 `ZCode.exe "%1"`）；
  `D:\QoderData\bin\qoder-computer-use\QoderComputerUse.exe` 存在（Computer Use 实物）
- 六家协议面统一实测：`HKCR` 下 `codex`、`qoder`、`qoder-app`、`opencode`、`zcode`、`workbuddy` **均注册为 URL Protocol**（`codebuddy` 无）

### 环境坑（复用价值）

- git-bash 向 Python 传含中文的路径参数会乱码（`/c/Users/????/`）→ 一律在脚本内用 `os.path.expanduser`/硬编码 ASCII 盘路径，不从命令行传中文路径
- Python `sqlite3` 游标在 `for row in cur.execute(...)` 里再次 `cur.execute` 会**静默丢失后续行**（本会话第一次 dump ZCode 只出 1/9 张表就是此因）→ 先 `fetchall()`
- `du -sh` 遍历 OpenCode/WorkBuddy 大目录会超时，改 `ls` 定向列目录

## Recovery note

- Interruption point: 静态证据全部采集完毕并已归档；`codex.md` 已写；余 4 份报告 + 汇总矩阵待写
- First action after recovery: 读本文件 E7~E12 段，直接写 `docs/adapters/phase0/opencode.md`（无需重跑任何探测）

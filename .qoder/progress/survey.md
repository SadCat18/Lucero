OWNER_ROLE: survey
TASK_ID: phase0-survey
STATUS: complete
SCOPE: Phase 0 五家 AI 软件能力调查，只产出 `docs/adapters/phase0/*.md` 与 `PHASE0_CAPABILITY_MATRIX.md`；不写业务代码、不改架构文档、不碰任何第三方软件配置
EVIDENCE_DIR: D:\Project\Lucero\.scratch\phase0-<vendor>\
LAST_UPDATED: 2026-09-19T16:55+08:00

> **本文件有两轮记录**：第 1 轮 = 五家纯静态取证（预算 0/15）；第 2 轮 = WorkBuddy 单家动态实测（预算 3/3 用尽）。
> 凡结论属第 2 轮者均以 **[实测]** 标注，与静态推断严格分开。第 1 轮有 5 项结论被第 2 轮推翻，见 D-节《修正记录》。

## Objective

把 `docs/adapters/PHASE0_ADAPTER_CAPABILITY_SURVEY.md` 的 25 项 × 5 家从 TBD 填成**有证据**的能力矩阵，
并产出：每家一份 phase0 报告、汇总分级与优先级、每家接入层级 + fallback 链、仍需 UI Automation 的能力清单。

## Completion criteria

- [x] `docs/adapters/phase0/codex.md` 存在，25 项每项有等级 + 证据来源 + 版本号；无证据项为 UNKNOWN 并写明原因
- [x] 同上 `qoder.md` `opencode.md` `zcode.md` `workbuddy.md`（五份齐全，主表各 25 行，见 V3）
- [x] `docs/adapters/PHASE0_CAPABILITY_MATRIX.md` 存在，含分级打分、优先级排序、fallback 链、UI Automation 待办清单
- [x] 全矩阵零猜测：每个非 UNKNOWN 格子可回溯到 `.scratch/` 里的一条原始输出（等级图例仅用八等级 + UNKNOWN；UNKNOWN 在每家报告逐条附原因）
- [x] 报告内零用户隐私：隐私复扫 0 命中（见 V4）；只含字段名、结构、枚举值、脱敏路径（`%USERPROFILE%`/`%LOCALAPPDATA%` 形式）

## Scope boundaries

- In scope: `docs/adapters/phase0/`、`docs/adapters/PHASE0_CAPABILITY_MATRIX.md`、本文件、`.scratch/phase0-*/`
- Out of scope: 四份架构文档（红线①）、`core/migrations/`、git 任何写操作（红线②）、安装/卸载软件（红线④）
- Out of scope: `docs/architecture/CORE_DOMAIN_MODEL_V0.1.md` 的 A/B 分叉（ADR-000 D1~D4 裁决中，本角色不依赖它）
- 红线⑤已由用户在本会话指令中逐次授权：只读浏览 + 启动观察 + 每家 ≤3 条真实消息（仅限 `.scratch/phase0-<vendor>/` 空项目）
- 预算使用：第 1 轮 **0 条真实消息**（五家全靠静态定级）；第 2 轮 **WorkBuddy 3 / 3 用尽**（用户单次授权），其余四家 0 / 3 全剩余

## Current checkpoint

- Stage: **三轮闭环** —— 第 1 轮五家静态定级 ｜ 第 2 轮 WorkBuddy 动态实测与改判 ｜ **第 3 轮（09-27，授权 B5②）Qoder mcp-router 探测，负向收口**
- Status: complete
- Last verified action: 两发只读对照请求（带 key→`404` / 不带 key→`401`）+ 收尾核验：
  `netstat` 监听数 39→39 未增、`52393` 仍由 pid 30936(Qoder `0.4.2`) 独占、`Get-Process python` 无我的残留、
  `mcp-router.json` mtime 仍 `2026-09-27T12:04:05`（早于探测 ⇒ 确认只读未改）；apiKey 全程 `***(len 43)` 未落盘
- Next action: **交回主控**——Phase 0 已无「等授权才能定级」的悬空格子，本角色可关闭
- Verification: 见 V1~V7、D1~D10、**R1~R3（第 3 轮，见下）**

## Completed

- 装载 `ponytail`(full) + `project-checkpoint`；通读 4 份必读文件（调查表 / 架构 §9 / orchestration / ADR-000）
- 建立 `docs/adapters/phase0/` 目录
- **五家静态证据采集全部完成**，逐家定级依据见 Verification evidence 段 E1~E16
- 实物确认：Codex CLI `codex-cli 0.154.0` + Desktop `OpenAI.Codex_26.915.3509.0`（MSIX，进程名 ChatGPT.exe）；
  Qoder `0.3.3`（引擎 `entrypoint=cli`，transcript 内 `version=1.1.53`）；OpenCode Desktop `1.18.14`；
  ZCode `3.12.3.7463`（引擎 `session.version=0.16.x`）；WorkBuddy Desktop `5.5.6` + 内置 CodeBuddy CLI `2.137.1`
- **第 2 轮（动态实测，用户单次授权）WorkBuddy 探针完成并改判**：起 `codebuddy --serve`（127.0.0.1:8791，默认口令）→
  拉到 **OpenAPI 3.1.0 / 113 paths / 44 schemas** 的公开契约 → 实测鉴权三层、列会话、**读回正文**、
  token 级流式、额度数值、停作业、复用登录 → **原 #4 UNKNOWN 被推翻**（正文在本机两处 JSONL）；
  WorkBuddy 分数 50/80=62.5% → **74/88=84.1%**，P1 → **P0（范围限 CLI 会话族）**。详见 D1~D8
- 复盘出两条方法论教训并写入矩阵：① `find … | head` 的 **SIGPIPE 假阴性**导致第 1 轮误判"正文不在本机"；
  ② **API 返回成功 ≠ 生效**（`delivered:true / saved:false`）→ Core 不得把 vendor 的"已接受"当"已生效"
- 主目录定位纠偏一次（第 1 轮）：`D:\ZCodeFile\.zcode` 是**工作区目录**，真正的 app home 是 `%USERPROFILE%\.zcode\`，
  其 `cli\db\db.sqlite`(208 MB) 才是 ZCode 的会话主库 —— 若未纠正会把 ZCode 错评为"只有索引 + 只能 UI Automation"

## Remaining

- 本角色交付物**无剩余**。
- ~~1. WorkBuddy 桌面端会话正文位置~~ → **第 2 轮已解决**（正文在本机两处 JSONL；结论与修正记录见 `workbuddy.md`）
- 唯一剩余的待授权深挖：**Qoder mcp-router 的 REST 面**（`mcp-router.json.baseUrl` 指向实测在 LISTENING 的 `127.0.0.1:58129`）
  —— 需授权使用其 `apiKey`（= 使用用户登录态），可能把 Qoder 的 #8/#9/#13 从 UNSUPPORTED 拉回，**决定 Qoder 是否仍排 P2**
- WorkBuddy 侧仍有 5 个 UNKNOWN 因**预算耗尽**未能推进（#7 对既有会话续发的可靠路径 `-r <sessionId>`、#11/#12 切模型与 effort 生效、
  #16 待审载荷、#18 附件注入）→ 若主控要补这几项，需**新一次**消息预算授权（建议 ≥2 条，且探针目录放在 git 仓库之外）
- 预算现状：**WorkBuddy 3/3 用尽；Codex / Qoder / OpenCode / Z Code 各 3 条全部未动**

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
- → 主控：**WorkBuddy 与 Qoder 的 transcript 同为"Claude-Code 形 JSONL"，但 [实测] 并不同构**
  （WorkBuddy 扁平 `{role,content,providerData,__codebuddyLocal}`；Qoder 嵌套 `{uuid,parentUuid,message:{role},toolUseResult}`）
  → 原"3 个本地数据族"修正为 **4 个 reader**（A=OpenCode+ZCode、B1=WorkBuddy、B2=Qoder、C=Codex）
- → 主控：颠覆性事实更新（第 2 轮）：**ZCode 无 API/无 CLI 仍成立**；~~WorkBuddy 会话正文未定位~~ 已推翻 →
  改为新事实 **WorkBuddy 有两套互不可见的会话身份空间**（CLI 族 `.codebuddy` vs 桌面族 `.workbuddy`，实测桌面表零新增行）
  → `external_session_id` 必须带 namespace 维度，否则撞 id 空间。**这是本轮对 Adapter 协议最实质的新增要求。**
- → 主控：[实测] **vendor 的"已接受"响应不可信**（`/jobs/{id}/reply → delivered:true, saved:false` 且不执行）
  → Execution/Event 语义要求"投递后一次读回确认"，建议写进契约①

## Changed files

**交付物（本角色唯一可写范围）**

- `docs/adapters/phase0/codex.md` (新建, 13,032 B) — 25 项定级 + fallback 链 + 4 条 UNKNOWN
- `docs/adapters/phase0/opencode.md` (新建, 13,084 B) — 25 项 + 141+60 条路由证据 + 5 条 UNKNOWN
- `docs/adapters/phase0/workbuddy.md` (**第 2 轮整体重写**) — 25 项按 [实测]/[静态] 分档；12 项上调、#7 下调；UNKNOWN 由第 1 轮 6 条 → 第 2 轮 7 条（#4 已解决，新增 #13 费用语义与 #7/#11/#12 因预算耗尽未测）；新增《修正记录》与《残留与清理》
- `docs/adapters/PHASE0_CAPABILITY_MATRIX.md` (**第 2 轮改判**) — WorkBuddy 列 12 格更新 + 上标说明、打分表加"证据强度"列（WorkBuddy 62.5%→84.1%）、P0 由 2 家扩为 3 家、§6.3/§6.4/§6.5 重写、§7 边界声明改为两档证据口径
- `docs/adapters/phase0/zcode.md` (新建, 17,979 B) — 25 项 + 与 OpenCode 同构比对 + 6 条 UNKNOWN
- `docs/adapters/phase0/qoder.md` (新建, 15,562 B) — 25 项 + 7 工具契约原文 + 7 条 UNKNOWN
- `.qoder/progress/survey.md` (新建 → 本文件)

**明确未改**：`docs/adapters/PHASE0_ADAPTER_CAPABILITY_SURVEY.md`（原件 25 行 TBD 全部保留，见 V5）、
`docs/architecture/*`、`docs/adr/*`、`core/migrations/*`；
四家（Codex/Qoder/OpenCode/ZCode）软件的文件与配置 **0 次写入**；
WorkBuddy 的**配置、账号、默认项目、既有会话**同样 0 次写入 —— 第 2 轮唯一的写入是其 CLI 在 `~/.codebuddy/` 下
**自建的探针会话文件**（见 V7 与 `workbuddy.md《残留与清理》`），非用户数据、非配置改动

**过程产物（非交付物，可清理）**：`.scratch/phase0-{codex,opencode,zcode,workbuddy,qoder}/`、
`.scratch/{dbschema,redact_json}.py`、`.scratch/phase0-codex/schema_x/`（428 个 schema 文件，**建议保留**给 adapter 角色对齐契约）

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
- **E14** 主库 `C:\Users\<用户名>\.zcode\cli\db\db.sqlite` **208,707,584 B**，23 表：
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

### 第 2 轮动态实测（WorkBuddy，D1~D10）— 归档于 `.scratch/phase0-workbuddy/dyn/`

- **D1 起服务**：`node …/cli/bin/codebuddy --serve --port 8791 --host 127.0.0.1` → stdout 打印
  `Endpoint / Web UI(?password=…) / Password / Config` 四项（口令未记录，仅本地 log）；
  `netstat` 实测 `127.0.0.1:8791 LISTENING pid=22600`；`GET /api/v1/health` → `{status:"ok",uptime,platforms:["generic","wecom","wechat-kf"],pid}`
- **D2 鉴权契约**：任何 API 缺 `X-CodeBuddy-Request: 1` → `403 {"error":"Missing required header: x-codebuddy-request"}`；
  `GET /api/openapi.json` **属豁免路径 → 免鉴权 200**。openapi `securitySchemes` = `{BearerAuth, PasswordQuery(?password=), CustomRequestHeader}`，
  豁免路径明文列出（静态资源 / `auth/status` / `auth/login` / `webhooks/*` / `openapi.json` / `docs`），可用 `CODEBUDDY_DISABLE_REQUEST_VALIDATION=1` 关闭
- **D3 契约规模**：`info.title = "CodeBuddy Code API (Beta)"`，`info.version = 2.137.1`，**paths=113**，`components.schemas=44`
  （ACP 帧族齐全：`SessionUpdate/AgentMessageChunk/AgentThoughtChunk/UserMessageChunk/ToolCall/ToolCallUpdate/InterruptionRequest/SessionEnd/ModelUpdate/ModeUpdate/PlanUpdate/AvailableCommandsUpdate`…）
- **D4 #1 复用登录 [实测]**：`GET /api/v1/auth/status` → `{authEnabled:true, authenticated:true}`；
  CLI `system/init` 事件 `apiKeySource="copilot.tencent.com"`；**全程无登录/验证码/Hello 弹窗**，未读任何凭据值
- **D5 #4/#3 正文与列会话 [实测，推翻第 1 轮]**：唯一标记串自造后落盘比对，命中 4 处 ——
  `~/.codebuddy/projects/<cwd-slug>/<sessionId>.jsonl`（正文，扁平 `{id,timestamp,type,role,content,providerData,__codebuddyLocal,sessionId,cwd}`）、
  `~/.codebuddy/jobs/<shortId>/state.json`、`~/.codebuddy/logs/<date>/<proj>__<32hex>.log`（**原样写 prompt 正文**）、`~/.codebuddy/traces/<pid>/trace_*.json`；
  桌面族另有 `D:\WorkBuddyData\.workbuddy\projects\<cwd-slug>\*.jsonl`（11 个 slug 目录）；
  `GET /api/v1/sessions/across-projects` → `{id,name,createdAt,updatedAt,messageCount,isCurrent,projectId,cwd,status,isPlayground,isUserDefinedTitle}`；
  `GET /api/v1/sessions/{id}/history` → `{sessionId,name,requests:[{userInput,finalReply}]}`，**`finalReply` = 我造的标记串**（读回成功）
- **D6 #8/#13 [实测端到端]**：`codebuddy -p --output-format stream-json --include-partial-messages --permission-mode plan "<marker 提示>"`
  → **exit=0**、38 行 JSONL、事件族 `{system/init:1, system/status:1, file-history-snapshot:1, stream_event:32, assistant:2, result/success:1}`；
  `result` = `{duration_ms:8771, duration_api_ms:8771, num_turns:3, is_error:false, total_cost_usd:0,
  usage:{input_tokens:26976, output_tokens:80, cache_creation_input_tokens:13216, cache_read_input_tokens:13760},
  modelUsage:{auto:{…, contextWindow:256000, maxOutputTokens:32000}}, permission_denials:[], _meta:{traceparent,baggage}, _requestId}`；
  REST 侧 `GET /api/v1/stats` → `modelUsage.auto` 数值与 CLI **完全一致** + `dailyModelTokens:[{date,tokensByModel:{auto:27056}}]`
- **D7 两个实测负面结论**：
  ① `POST /api/v1/jobs` 派发后 run 卡在 `SessionRunStateMachine … event=RUN_ACCEPTED | to=pending`，**无 RUN_COMPLETED**，
     `jobs/<id>/state.json` 长期 `state:"working"` + `detail:"Idling with no task or recent output"` + `output:null`；
     `POST /api/v1/jobs/{id}/reply` → `{delivered:true, saved:false}` 且 messageCount 不增、stats 不增 → **HTTP 写链实测不可靠**
  ② 契约漂移：`POST /api/v1/runs` 文档 body 只有 `{text,sender}`，实跑 `400 "Invalid generic message format: missing required fields (id, type)"`
     （在 `codebuddy.js` 的 generic adapter `parseInbound` 里查到真实要求 `{id,type,version?,source?,payload}`）
- **D8 其余实测点**：`GET /api/v1/info` → `{cwd,version,os,arch,nodeVersion,gatewayMode:"local",tunnelUrl,uptime,userName,autoUpdate:{enabled:true}}`；
  `POST /api/v1/jobs/{id}/stop` → `{data:{stopped:true}}`；`GET /api/v1/jobs` 列表项带 `webUrl:"http://127.0.0.1:<port>/"`；
  模型目录来源 = 服务日志 `[CloudProductManager] fetch cloud product … success, agents(1): cli, models(52)` + `AgentModelResolver product config input: 55 models, 18 agents`
  + `CustomModelsProductProvider … ~/.workbuddy/models.json`（**REST 无 `/models` 端点**）；
  `GET /api/v1/jobs/{id}` 详情返回形状与列表不一致（我按列表形状解析取到 `state=None`）
- **D9 隔离性 [实测]**：CLI 会话跑完后 `workbuddy.db.sessions` 仍 **36 行**、`where cwd like '%phase0-workbuddy%'` = **0 行**
  → CLI 族与桌面族是两套会话身份空间
- **D10 清理**：`taskkill /PID 22600 /T /F` 成功；34852/37388 已自行退出；8791 无 LISTENING（剩 TIME_WAIT）；
  收尾新增的 51218/51219 经核属 **AweSun**（pid 7272），非本探针残留；桌面 WorkBuddy 13 进程未动

### 第 3 轮动态实测（Qoder mcp-router，R1~R5）— 归档于 `.scratch/phase0-qoder/`

- **R0 现量复测（静态，先做）**：`mcp-router.json` mtime `2026-09-27T12:04:05`、内容 `{schemaVersion:2, pid:30936, baseUrl:"http://127.0.0.1:52393", apiKey:***(len 43), startedAt:2026-09-27T12:04:05}`
  ⇒ **09-19 旧读数（pid 24480 / 58129）全部作废**；`netstat` 确认 `52393 LISTENING 30936`；`Get-Process` 确认该 pid = `D:\Qoder\.qoder-versions\0.4.2\Qoder.exe`（**已从 0.3.3 升到 0.4.2**）
- **R0b 零请求的路由穷尽（静态）**：在 `app.asar`(141,352,046 B) 内定位 `mcp-router.json` 写入点（偏移 38,180,795），读出常量与路由：
  `X-Api-Key` 头名（服务端要求 `rawHeaders` 中**恰好出现 1 次**，再 `sha256().digest()` 比对）、
  三条路径构造器 `/mcp/servers/${encodeURIComponent(id)}`、`/mcp/sessions/${token}/servers/${id}`、`/mcp/applications/${token}/servers/${id}`，
  另有 `/oauth/callback` 与 `/mcp/add`；`dIe()` 要求 id 解码后非空且不含 `/` 与 `\0`；`gVe(t)=t.profile.transportKind!=="inProcess"`
- **R1 [实测] 带 key 的 initialize**：`POST /mcp/servers/builtin`（JSON-RPC `initialize`, protocolVersion 2025-03-26, clientInfo=lucero-phase0-probe）
  → **404**；`content-type: text/plain; charset=utf-8`、`cache-control: no-store`、`Connection: close`；body 9 B `"Not Found"`
- **R2 [实测] 同请求不带 key** → **401**；body 12 B `"Unauthorized"`
  ⇒ **R1+R2 合起来的判据**：401 出现在鉴权阶段、404 出现在路由阶段，故 **key 已被服务端接受**，`builtin` 只是未注册上游。**单看 R1 的 404 不能得此结论**（这正是先设计对照再发第二发的理由）
- **R3 主动保留未发**：剩余可发目标只有 `/mcp/add`（注册上游＝改第三方配置）、`/oauth/callback`（涉凭据流）、以及需 32+ 位他方令牌的 token 路径（未取未猜）。多发一发只会得到又一个 404，不产生新信息
- **R4 [静态] 工具面变化实测**：`mcp_list` 复核 `mcp__builtin__skill_manage` 存在 ⇒ builtin 工具 **7 → 8**（09-19 为 7）
- **R5 [实测负向结论] 探针对 Qoder 的影响 = 零**：未启动进程、未新增监听（39→39）、未改任何文件（`mcp-router.json` mtime 未变）、未发消息、未消耗额度
- **未端到端跑通的东西一律没升级**：Qoder #22 仍 `EXPERIMENTAL`（本轮拿到的是**边界证明**，不是能力打通）；#8/#9/#11/#13/#14 五项缺口维持 `UNSUPPORTED`，且 router 出路已被**实测排除**

### 交付验收（V1~V7）

- **V1 文件齐全**：`ls -la docs/adapters/phase0/*.md docs/adapters/PHASE0_CAPABILITY_MATRIX.md` → 6 个文件非空；
  第 2 轮后 `workbuddy.md` 与 `PHASE0_CAPABILITY_MATRIX.md` 已重写/改判，余四家报告未动
- **V2 打分口径可复算**：权重 `N=4 C=3 CL=3 LD=2 RO=2 UA=1 EX=1 US=0`，只计 #1~#23，UNKNOWN 不计分改计覆盖率 →
  Codex 87/92=94.6%(覆盖 23/23，纯静态)、OpenCode 76/84=90.5%(21/23，纯静态)、
  **WorkBuddy 74/88=84.1%(22/23，含动态实测；第 1 轮静态为 50/80=62.5%)**、
  Qoder 48/84=57.1%(21/23，纯静态)、ZCode 32/84=38.1%(21/23，纯静态)
- **V3 25 行完整性**：`awk '/^\| # \| 能力 \| 等级/{flag=1;next} …END{print c}'` 逐文件 → 五份报告**各 25 行**（不多不少）
- **V4 隐私复扫**（第 2 轮加强版）：`grep -rnoE "<用户名>|<昵称>|<服务口令片段>|<仓主>|github\.com/[A-Za-z]|sk-[A-Za-z0-9]{6,}|<项目A>|<项目A>|<项目B>|<主机名>" docs/adapters/` → **0 命中**。
  第 2 轮新出现的三类敏感物均已排除：**账户昵称**（出现在 WorkBuddy 服务日志的 `auth_success` 行）、**`--serve` 自动口令**、**探针读到的 git 仓库地址** → 报告中一律用占位符或只写字段名
- **V5 未越界**：`grep -c TBD docs/adapters/PHASE0_ADAPTER_CAPABILITY_SURVEY.md` → 25 行 TBD 原样保留（不改基线文件）；
  `git` 写操作 **0 次**；第三方软件**配置/账号/既有会话 0 次写入**（`workbuddy.db.sessions` 前后均 36 行，实测）；
  软件 0 次安装/卸载；**未读任何凭据值**；全程未出现登录/验证码/Windows Hello 界面（故未触发"立即停止"条件）
- **V6 动态探针已彻底清理**：自起 `--serve` 的 pid 22600 已 kill，`netstat` 中 `8791` 与作业端口 `55518` 均无 LISTENING；
  监听数 33 → 34 的差值经核属第三方 AweSun，非本探针
- **V7 唯一遗留物 = 文件残留（未删）**：`~/.codebuddy/{projects/d-Project-Lucero-.scratch-…, jobs/5c14baca, sessions/22600.json, logs/2026-09-19/probe-proj__*.log, traces/{37388,16604}}`
  → 删除动作等于写第三方数据目录，需主控另行授权；内容仅含**我自造的标记串与提示词**，不含用户历史正文

## Closure record（09-27，主控 T-353 三件裁定已执行）

- **① 批准关闭本角色** ⇒ 已按裁定改写**两层因果**：`qoder.md` 推论段拆成
  「层 1 实测得到（404 只证明此刻无已注册上游）」与「层 2 由约束推出（要用此面须先 `POST /mcp/add` 注册上游 = 改第三方配置 = 禁区）」，
  并明写"若授权范围变这一格可重开"。矩阵 §7 同步加入三种负向成因的分类：
  **Qoder=被约束推出 ｜ WorkBuddy 5 项=被预算卡住 ｜ ZCode #21/#22=真·本机无实物（这一格才是"探不到"）**。
- **② UI Automation 不投** ⇒ 遵判：**未写任何 UIA 探针/脚手架**，本报告只留"将来若要把 Qoder 当 adapter 接，由用户单独授权"一句指向。
- **③ WorkBuddy 5 项维持 UNKNOWN，不追加授权** ⇒ 已在 `workbuddy.md` UNKNOWN 表头立口径：
  **"预算用尽未探" ≠ "探不到"**，两者分写不合并；并列出各自可试的具体命令（`-r <sessionId>`、`--model`、`--effort`）以示"只差授权"。
- **④ R3 主动保留** 获背书；剩余额度按裁定**不退不追**。④ 要求的自证形状（探针影响 = 零：监听 39→39、`mcp-router.json` mtime 未变、未消耗额度）
  已提到 `qoder.md` 第 3 轮章节**头部**供主控直接引用。
- 本角色状态：**关闭**（`STATUS: complete`，不再领新活；WorkBuddy 那 5 项由主控点名重开）。

## T-444 重开（2026-10-01，OpenCode 接入前档案）

- STATUS: complete（本任务四问已交；角色重开只为这一份档案，未再接别的活）
- 交付物：`docs/adapters/phase0/opencode-integration-profile.md`（13,993B，落在我角色唯一可写目录内）
- 时刻线（全部当场 `date`）：22:39:05 读契约 → 22:44:16 版本现量 → 23:01:16 启动桌面端并发现 `pid 23832 / 127.0.0.1:61677`
  → 23:0x 十一发只读端点全 `401` → 23:0x~23:1x 库/包静态穷尽 → **23:12:38 关闭我起的进程**
- 四问答案的一句话版：① 七条通知里 `stream/item` 映射干净、`stream/delta`/`turn/finished`/`turn/claimed`/`turn/dispatched` 都要 Adapter 合成、`approval/request` 有机制无载荷；
  ② **持久层判为伪流式**（text/reasoning 每 part 最多 2 帧、无 delta 字段；多帧的是 tool 状态机）⇒ 攒批够用、**`after_seq` 补拉不够用**；
  ③ `permission` 表列名 `action`/`resource` 与我们载荷天然对齐，但表 0 行 ⇒ 取值集合拿不到；**未加字段，改提 REQ-OC-1/2**；
  ④ 额度 `cost` 的 **0 与 NULL 在 OpenCode 自己数据里并存**（message 2,179 行：NULL 116 / =0 1600），建议 `=0 → LOW`、`NULL → 不产出`；模型清单走缓存文件；中断路由在、行为未测。
- 边界自证：**真消息 0 / 3 全未花**；未装软件、未改第三方配置、未截 IPC、未读凭据值；
  凭据只以 `***(len 35)`/`***(len 51)` 掩码形状与字段名出现；静态 `[静]` 与实测 `[实]` 与推断 `[推]` 全文分标。
- 残留：**零**（`Get-Process OpenCode` = 0；监听数 39 → 39；`61677` 仅 TIME_WAIT）。
  一次性产物全在 `.scratch/survey/opencode-integration-1001/`（未碰 /tmp、%TEMP%、$env:TEMP）
- **待主控裁的前置分叉（不是我能定的）**：接 OpenCode 的服务从哪来 ——
  (a) 装 `opencode` CLI 自起 `serve`（撞红线④，要用户点头，但能把 ②③④ 全部实测重做）
  (b) 复用桌面端内嵌服务（要用户提供服务口令，或允许写其配置 = 禁区）
  (c) 只做 LOCAL_DATA 只读（今天就能开工，写链为空，且 `turn/{claimed,dispatched,finished}` 三帧全靠 Adapter 自造 ⇒ **别拿它验收 §4.2 那两帧的取证强度**）
- Next action: 无。等主控对 (a)/(b)/(c) 的裁定，或 REQ-OC-1/2 的批复。

## Recovery note

- Interruption point: **无**——第 1 轮（五家静态）+ 第 2 轮（WorkBuddy 动态实测）均已闭环并改判落盘
- First action after recovery: 读 `docs/adapters/PHASE0_CAPABILITY_MATRIX.md` §7（两档证据强度）→ §6（5 条结构性事实）→ §3（P0 三家 / P2 / P3）；
  若恢复工作只剩一件：**向主控要 Qoder mcp-router 的一次只读探测授权**（唯一还可能改判的入口）。
  WorkBuddy 侧若要补剩余 5 项 UNKNOWN，需要**新的**消息预算授权（3/3 已尽），且探针目录务必放在任何 git 仓库之外

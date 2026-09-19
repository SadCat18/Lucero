# Phase 0 调查报告：Qoder

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ 方法：**纯静态取证**（exe 版本信息、builtin MCP 工具契约原文、本地 transcript JSONL 结构、配置文件结构、注册表、netstat）
- 特殊地位：Qoder 是**本次工作的宿主软件**。全程未修改 Qoder 的任何设置、配置、hooks、账号（本角色 self-imposed 边界）。
- 被调查实物与版本号：
  - Qoder Desktop **0.3.3** — `D:\Qoder\.qoder-versions\0.3.3\Qoder.exe`（**按版本建目录**的安装布局）
  - Agent 引擎 **1.1.53** — 实测 transcript 每行 `version:"1.1.53"`、`entrypoint:"cli"`、`userType:"external"`（345 行一致）
  - 配置目录：`QODER_CONFIG_DIR = D:\QoderData\`（`.auth .bin .cache .models app bin entry file-history logs memory plugins projects qoder-knowledge security-resources shell-snapshots skills tmp` + `settings.json mcp-router.json installation_id skill-usage.json .qoder-app-status.json`）；另有历史默认目录 `%USERPROFILE%\.qoder\`
- 原始输出归档：`D:\Project\Lucero\.scratch\phase0-qoder\`
- 真实消息预算使用：**0 / 3**

## 关键结构性事实（**本报告最需要主控读的一段**）

1. Qoder 对外的**全部可编程面 = 7 个 builtin MCP 工具**（原文取自本机 `mcp_list` 描述）：
   `create_chat_session`、`fork_chat_session`、`list_chat_sessions`、`read_chat_session`、
   `send_message_to_chat_session`、`wait_chat_sessions`、`qoder_cron`。
   这套接口的粒度是 **turn 级**，不是 token 级；没有 streaming、没有 abort/interrupt、没有模型/参数切换、没有额度。
2. `mcp-router.json` 实测结构 `{schemaVersion:2, pid, baseUrl:"http://127.0.0.1:<port>", apiKey, startedAt}`，
   且 `netstat -ano -p tcp` 实测该 pid **确实在 LISTENING `127.0.0.1:58129`** → Qoder 内部有一个带鉴权的本地 HTTP MCP router。
   **本角色未探测其 REST 面**：那需要使用 `apiKey`，属"使用用户登录态"，需主控另行授权。这是 Qoder 潜在的最高价值未开发入口。
3. transcript 是 **Claude-Code 形制**的 JSONL：`uuid`/`parentUuid` 对话树 + `isSidechain` 子链 + `active-leaf`(带 `leafUuid`,`explicit`) 分支游标 + `toolUseResult` + `attachment`。
   → 与 WorkBuddy/CodeBuddy 同族（见矩阵 §同构族）。
4. `create_chat_session` **只有 `prompt` 一个业务参数**（无 model / 无 cwd / 无附件）→ 与主控在 `orchestration.md` 里
   已记录的现实约束一致："worker 的模型档位设不了"。本报告为该结论提供字段级证据。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | NATIVE | 7 个 builtin 工具的行为本身；`.auth/machine_id`；`mcp-router.json{baseUrl,apiKey}` | 在 Qoder 内运行的 agent 天然以当前登录态工作，创建/驱动会话不需要二次认证 | 仅对**同机同用户**成立。外部进程要复用必须拿 router 的 apiKey（未授权）。降级：LOCAL_DATA 只读 |
| 2 | 软件版本发现 | LOCAL_DATA | exe VersionInfo；transcript `version`；`settings.json`；`.qoder-app-status.json` | Desktop `0.3.3` 与引擎 `1.1.53` **两个版本号**；安装目录本身带版本名 `.qoder-versions/0.3.3/` | 无 CLI `--version` 可跑（见 #21）。降级：读目录名 |
| 3 | 列出已有会话 | NATIVE | `list_chat_sessions` 描述原文 | "Find registered Qoder conversations before reading, waiting on, messaging, or forking them. Results are ordered by most recent update and **contain IDs and current state, not conversation transcripts**" + `nextCursor` | 明确不返回正文（设计如此）→ 列表后必须逐个 `read_chat_session`。降级：LOCAL_DATA 数 `projects/<slug>/*.jsonl` 文件 |
| 4 | 读取完整会话 | READ_ONLY（API）/ LOCAL_DATA（完整） | `read_chat_session` 描述原文；transcript 实测 | "returns a **bounded snapshot** without resuming execution… Messages and optional tool outputs **may be truncated**; `hasMoreBefore` means older history exists outside the returned window" | API 侧**保证会截断** → 完整性只能靠读盘。实测单会话 JSONL 达 1,236,497 B。降级：直读 jsonl（但见 #5 的实时性限制） |
| 5 | 新消息增量同步 | NATIVE（turn 级）/ LOCAL_DATA | `wait_chat_sessions` 描述；transcript `active-leaf{leafUuid,explicit}`、`uuid/parentUuid`；`state.json{revision:202}` | `wait` = "Return when any target **finishes its current turn**, becomes idle, fails, is interrupted, or needs human attention"，≤8 目标、超时也返回最新状态；`state.json.revision` 单调 → 变更游标 | **turn 粒度，无 part/token 粒度**；`wait` 上限 8 会话 → 大规模编排要分批。降级：jsonl 文件追加检测 |
| 6 | 双向同步 | NATIVE | `send_message_to_chat_session` + `wait` + `read` + `fork` | 读+写+等待+分支四件套齐；send 描述："If the target is running, its **normal steering or queueing policy** applies" | steering/queueing 具体策略**不透明**（无法指定"立刻插入"还是"排队"）。降级：等 turn 完成再发 |
| 7 | 向指定已有会话发消息 | NATIVE | `send_message_to_chat_session(sessionId, …)` | 用 `list_chat_sessions` 返回的 sessionId 精确投递；描述明确允许**不需用户再发消息**即可继续目标会话 | 不能发到当前会话自身（描述禁止）；不能发已归档会话（"unarchived"）。降级：`fork_chat_session` + send |
| 8 | 流式响应 | **UNSUPPORTED** | 7 个工具的参数与返回描述全量核对 | **无任何 token/part 级流式接口**。最接近的是 `wait_chat_sessions` 的 turn 完成事件 | 原因：工具集设计为"拉取快照 + 等待 turn"。降级：LOCAL_DATA 轮询 jsonl（**能拿到较细粒度，因为 tool_use/tool_result 逐行落盘**，但不是真流式） |
| 9 | 中止当前生成/任务 | **UNSUPPORTED** | 同上 | 7 个工具中无 interrupt/abort/cancel/stop；`wait` 只能**观测到** `interrupted` 状态 | 即"可观测、不可触发"。降级：UIA（点 Qoder UI 的停止按钮） |
| 10 | 读取模型列表 | **UNKNOWN** | `D:\QoderData\.models\default`(107 B 明文) + `.models\<uid>\catalog-v6`(95,580 B) | `default` = `{key:"qmodel_38max", uid, scene:"app", updatedAt}` 可读；**`catalog-v6` 首 600 B 为 base64 密文，非 JSON** → 模型清单读不出 | 无工具暴露模型列表。原因：目录被加密/编码，破解不属于本角色职责。降级：transcript `runtime-config.model` 只能得到**已用过的**模型（实测值 `"qmodel_38max"`） |
| 11 | 切换模型 | **UNSUPPORTED** | `create_chat_session` 参数面（仅 `prompt`）；transcript `runtime-config.model` | 工具层无 model 参数 → 无法为新会话指定模型，也无法改已有会话模型 | 与 `orchestration.md` 已记录结论一致（只有 `qoder_cron` 能指定模型）。降级：UIA 手动切档 |
| 12 | 推理强度/参数控制 | READ_ONLY | transcript `runtime-config{reasoningEffort, contextWindow, generation}` | 字段存在（**实测值 `reasoningEffort=null`、`generation=null`、`contextWindow=null`/`1000000`**）→ 可读但本机样本为 null，说明有槽位无数据 | 只能观测、不能设置；且 null 的含义（未设/不支持/未上报）**无法区分** → Core 侧必须当 UNKNOWN。降级：无 |
| 13 | 精确额度读取 | **UNSUPPORTED** | assistant `message.usage` 实测；无对应工具 | 结构齐全：`{input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens, server_tool_use{web_search_requests, web_fetch_requests}, service_tier:"standard", …}` —— 但**实测所有数值全为 0** | "有槽位、无数据"，与 `orchestration.md` 记录一致（"额度消耗读不到：没有任何工具能给出 token/费用数字"）。**禁止把 0 当真实用量上报**（P0 事故：把 UNKNOWN 显示成确定值） |
| 14 | Reset 时间 | **UNSUPPORTED** | 同上 | 无 reset/period/quota 字段 | 完全无证据 |
| 15 | 任务实时状态 | NATIVE | `wait_chat_sessions` 结果状态集；`list_chat_sessions` 的 "current state"；`state.json.revision` | 五态明确：`turn_completed`、`idle`、`fails`、`interrupted`、`needs_attention`；描述还警告 "turn_completed and idle describe execution state, **not acceptance of the requested work**" | ≤8 会话/次、单次 ≤60s（本会话实测口径）。降级：轮询 `list` |
| 16 | 审批读取 | NATIVE | `wait_chat_sessions` 的 `needs_attention` | "the result includes every target's current state… If the outcome is `needs_attention`, **surface the required human action** without inventing a response" → 能知道"在等人" | 只给"需要人"这一信号，**不给审批对象的类型/命令内容**（`read_chat_session` 可能带，但其描述强调截断）→ 细粒度审批载荷未证实 |
| 17 | 审批响应 | **UI_AUTOMATION** | 工具集核对 | 没有 approve/deny/answer 类工具。`send_message_to_chat_session` 是"发instructions"不是"回审批"；`wait` 的描述反而要求 agent **不要替用户编造回应** | 语义上被刻意设计为"人必须在环"。降级：由用户点，或 UIA 代点（**后者与本项目"审批不可自动通过"的原则冲突，不建议**） |
| 18 | 附件 | READ_ONLY | transcript `type:"attachment"`(实测 20 行) + 顶层 `attachment` 键；`D:\QoderData\file-history\`、`shell-snapshots\` | 附件**在会话里可读到**（20 条实测） | `create_chat_session` / `send_message_to_chat_session` **无附件参数** → 只能读不能注入。降级：把文件放进 workspace 让工具自己读 |
| 19 | MCP/Skill/Rule/Hook | NATIVE（五家最强的使用侧证据） | 本会话可见的 MCP 面：6 个 server / 100+ 工具；`D:\QoderData\{skills, plugins, memory, qoder-knowledge, security-resources}\`；`skill-usage.json`；可用 skill 清单 20+；`mcp-router.json` | MCP **可安装可调用可增删**（有 mcp-config 类 skill）；Skill 有 usage 统计落盘；Plugin 有 marketplace 来源；Rule/Hook 走 `.qoder/` 与 settings | 全是 Qoder 自己的生态，外部 Adapter 只能读文件不能调 API。本角色边界：**未改任何 Qoder 配置** |
| 20 | 本地会话数据库 | LOCAL_DATA | 实测 | 不是 SQLite，是**按 workspace slug 分目录的 per-session JSONL**：`D:\QoderData\projects\<workspace-slug>\<sessionId>.jsonl`（实测本工作区 2 个会话，1.2 MB / 0.9 MB）+ 同名目录 `{compression-v2/, state.json}`；另有 `logs/`、`file-history/`、`shell-snapshots/`、`tasks/<bgId>.output` | **workspace slug 是路径改写而来**（含中文被替换为 `-`），跨机器不稳定；`compression-v2` 表示历史会被压缩 → **老消息可能已从 jsonl 消失**，#4 的完整性受此威胁 |
| 21 | CLI | **UNKNOWN** | `D:\QoderData\entry\qoder.cmd` + `qoder-dispatcher.ps1`(5,971 B) | dispatcher 逻辑实测读得很清楚：优先 `$env:QODER_CLI_BIN` → PATH 上的 `qodercli` → `%USERPROFILE%\.qoder\bin\qodercli\qodercli.exe`。**三条路径本机全部不存在**（`~/.qoder/bin` 实测只有 `{7zr.exe, git, qoder-computer-use}`）；但 transcript `entrypoint:"cli"` 证明 app 内跑的确实是 CLI 运行时 | 找不到可执行体就无法 `--help` 取证。装 CLI = 红线④。降级：用 builtin MCP 工具面（已在 #3~#7 定级） |
| 22 | SDK/API/App Server | EXPERIMENTAL | `mcp-router.json` + netstat 实测监听 | 本地 HTTP server **活着**（`127.0.0.1:58129`，pid 24480 与 json 内 `pid` 一致，`schemaVersion:2`，`apiKey` 字段存在）。它是 MCP router 而非会话 API，**未探测** | 探测需使用 apiKey → 越界，需主控另行授权。这是 Qoder 唯一可能把 #8/#9/#13 从 UNSUPPORTED 拉起来的路径 |
| 23 | 精确跳转原客户端会话 | NATIVE（部分） | `HKCR\qoder` 与 `HKCR\qoder-app` 均注册 `URL Protocol`；`qoder\shell\open\command` = `"…\Qoder Launcher.exe" "%1"` | 两套协议并存（`qoder://` 走 Launcher，`qoder-app://` 另一路）→ 跳转机制确定存在 | URL 是否接受 sessionId **未实测**（会弹用户窗口）。降级：`read_chat_session` 给 sessionId，用户自己在 UI 找 |
| 24 | UI Automation 可行性 | UI_AUTOMATION | `D:\QoderData\bin\qoder-computer-use\QoderComputerUse.exe`；`resources/bin/appshot_bare_modifier_monitor.exe`；`~/.qoder/logs/runs/<ts>-<id>-p<pid>/qodercli.log` | Computer Use 执行体**已随包安装**，且有 appshot/修饰键监视辅助件 → 官方自动化链完备且在本机可用 | Qoder 是宿主：用它自己的 computer-use 驱动它自己 = 递归风险，需主控单独评估 |
| 25 | 版本变化风险 | COMPATIBLE | 综合 | 风险信号：① 安装按版本建目录（`.qoder-versions/0.3.3`）+ `resources/fast-update/` + `qoder-fast-update-trust.exe` → **会热更新**，版本会变；② `mcp-router.json.schemaVersion:2`、`.models/catalog-v6` 都带版本号 → 格式在演进；③ Desktop 0.3.3 与引擎 1.1.53 双版本线 | 缓解：MCP 工具契约是 Qoder 的公开面（描述文本本身即契约），比内部 router 稳定。**优先绑定 builtin 工具面，不要绑 router** |

## 建议接入层级与 fallback 链

```
NATIVE: builtin MCP 工具面（list / read / send / wait / create / fork / cron）
   ├─ 覆盖：列会话、读会话(截断)、发消息、turn 级状态、needs_attention
   ├─ 读链 fallback：LOCAL_DATA 直读 projects/<slug>/<sessionId>.jsonl
   │     （解决 read_chat_session 的截断问题；注意 compression-v2 可能已丢历史）
   └─ 未覆盖能力（流式 / 中止 / 改模型 / 额度）：
         EXPERIMENTAL: mcp-router 本地 HTTP（需另行授权探测）
            └─ 失败 → UI_AUTOMATION（QoderComputerUse.exe）—— 但宿主自反，需主控决策
禁止：把 usage 的 0 当真实额度；把 needs_attention 的具体载荷猜出来
```

## 仍需 UI Automation 的能力

1. **#9 中止当前生成** —— 无 API。
2. **#11 切换模型档位 / #12 设置推理强度** —— 无 API（`create_chat_session` 只有 `prompt`）。
3. **#17 审批响应** —— 无 API，且设计上要求人在环。
4. **#8 流式响应** —— 无 API；LOCAL_DATA 轮询比 UIA 更现实（UIA 读不了 token 流）。
5. 前置：先探测 `mcp-router` 能否把 1~4 变成 NATIVE，再决定是否投入 UIA。

## UNKNOWN 清单与原因

| # | 能力 | 为什么是 UNKNOWN |
|---|---|---|
| 10 | 模型列表 | `.models/<uid>/catalog-v6`(95,580 B) 实测为 base64 密文非 JSON；无任何工具暴露模型枚举 |
| 21 | CLI 能力 | dispatcher 引用的 `qodercli` 三条候选路径本机**全部实测不存在**；只有 `entrypoint:"cli"` 间接证明有 CLI 运行时。装 CLI = 红线④ |
| 22 | mcp-router 的 REST 面 | 探测需使用 `apiKey` = 使用用户登录态，本角色未越界。**建议主控决定是否授权一次隔离探测** |
| 16 | `needs_attention` 的载荷结构 | 未制造过一次真实审批（会打断用户，且属对宿主写状态） |
| 18 | 附件注入通道 | 工具无附件参数；本地 jsonl 里却有 `attachment` 行 → 说明存在通道但入口未知 |
| 13 | `usage` 全 0 的原因 | 无法区分"Qoder 不上报"与"该账号体系无此计量"。只有厂商侧能回答 |
| 23 | deep link 会话粒度 | 实测会弹用户窗口，属打扰 |

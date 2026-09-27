# Phase 0 调查报告：Qoder

- 调查角色：`survey` ｜ 第 1 轮 2026-09-19 纯静态；**第 3 轮 2026-09-27 加入 mcp-router 动态实测（用户经主控单次授权 B5②）**
- 本文凡标 **[实测]** 者为真发过请求并拿到响应；标 **[静态]** 者为文件/二进制/注册表推断。**未端到端验证**的格一律保留原等级。

## 第 3 轮：本地 mcp-router 探测结论（先给答案）

> **探针影响 = 零（本次授权的自证，主控只引用这一条）**：未启动任何进程或监听端口（两次请求均由一次性 Python 客户端发出即退）；
> `netstat` 监听数 **39 → 39 未增**；`52393` 仍且仅由 pid 30936 的 Qoder 持有；`D:\QoderData\mcp-router.json` mtime 仍 `2026-09-27T12:04:05`
> （早于探测 ⇒ **只读、未被改动**）；**未发消息、未调用任何工具、未消耗额度、未改 Qoder 任何设置/配置/hooks/账号**；
> `apiKey` 全程只存内存，报告与日志统一记 `***(len 43)`；未触发登录 / 验证码 / Windows Hello。

**主控最关心的问题 —— "mcp-router 能否把 Qoder 的 #8 流式 / #9 中止 / #13 额度 从 UNSUPPORTED 拉回 NATIVE"——答案是否定的，且是实测得到的否定。**

- **它不是 REST 列举面，是 MCP 上游代理面。** [静态，全量二进制枚举] 路由只有 4 类：
  `/mcp/servers/<encodeURIComponent(id)>`、`/mcp/sessions/<32+位 token>/servers/<id>`、`/mcp/applications/<32+位 token>/servers/<id>`、`/oauth/callback`；
  另有字面量 `/mcp/add`。解析器 `dIe()` 明确要求 serverId 解码后**非空且不含 `/` 与 `\0`** ⇒ id 是单段名字，不是 URL。
  **没有 health / info / version / servers / tools 之类的发现端点**（bundle 里那些字面量属别的组件，不可归给本 router）。
- **鉴权确实生效，且我手上的 key 通过了它。** [实测两发对照] 同一请求体同一路径：
  带 `X-Api-Key` → **`404` `text/plain "Not Found"`**；故意不带该头 → **`401` `"Unauthorized"`**。
  ⇒ 401 先于 404 说明**鉴权在路由之前** ⇒ `mcp-router.json.apiKey`（len 43，值不落盘不贴出）**已被服务端接受**；
  `builtin` 只是**不在已注册上游里**。鉴权实现 [静态]：要求 `rawHeaders` 中 `x-api-key` **恰好出现 1 次**，再对其值做 `sha256().digest()` 比对。
- **本机没有任何可寻的上游 MCP server**，且**没有发现面可问"有哪些"** ⇒ 外部进程拿不到合法 `id`。
  两条带 token 的路径需要 out-of-band 的 32+ 位会话/应用令牌 —— 我**未取、未猜、未伪造**（伪造只会再得 404，且属凭据试探）。
- **推论（重要，写给 adapter 角色）—— 两层因果必须分开，不可合并成"探不到"**：
  - **[层 1 · 实测得到]** Qoder 的 `builtin` 会话工具集（含本项目编排全靠的那 7~8 个）**不经这个 HTTP 面暴露**；
    该面**没有列举/发现端点**，故当前拿不到任何合法 `id`。[静态] 佐证：`gVe(t) = t.profile.transportKind !== "inProcess"` —— 进程内服务与外部上游走不同通道。
    ⇒ **R1 的那个 `404` 只证明"此刻没有已注册的上游"，不证明"此路永久不可用"。**
  - **[层 2 · 由约束推出，不是由 404 推出]** 要让此路变得可用，唯一动作是**先往 Qoder 注册一个上游 MCP server**（路由表里的 `/mcp/add`），
    而那是**对第三方软件配置的写操作** ⇒ 撞本项目禁区（本会话授权字面只到"用这把 key 发只读请求"）。
    ⇒ **"Qoder 侧没有本地 HTTP 出路"这个结论是被约束推出来的。** 换句话说：若将来授权范围改变（允许注册一个只回环、无副作用的探针上游），
    这一格是可以重开的；今天判它不通，不是因为服务器回了个 404。
  - 综合：**想从 Core 侧程序化驱动/观测 Qoder，可用入口仍是进程内 MCP 工具面（turn 粒度）**；本地 HTTP 面受约束封锁，非受能力封锁。

### 交账三件（⑨）

| # | 端点/动作 | 请求 | 状态码与响应形状 | 定性 |
|---|---|---|---|---|
| R1 | `POST /mcp/servers/builtin`（JSON-RPC `initialize`，protocolVersion 2025-03-26，clientInfo=lucero-phase0-probe） | **带 key** | **404**；`content-type: text/plain; charset=utf-8`、`cache-control: no-store`、`Connection: close`；body 9 B `"Not Found"` | key 通过，id 未注册 |
| R2 | 同一请求体、同一路径 | **故意不带 key** | **401**；body 12 B `"Unauthorized"` | 鉴权先于路由 ⇒ 反证 R1 已过鉴权 |
| R3 | 未发出 | — | — | 预算 2/3 已用，剩 1 发**主动保留**：可发的目标只剩 `/mcp/add` 与 `/oauth/callback`（均判为有副作用）与需令牌的 token 路径，多发一发只会得到又一个 404，不产生新信息 |

**判为有副作用、因此没试的端点（逐条给理由）**
- `/mcp/add` —— 字面语义是**注册/新增上游 MCP server** = 改第三方软件配置。禁。
- `/oauth/callback` —— OAuth 回调，涉凭据流（即便空参也可能触发流程状态机）。⑦ 类风险，禁。
- `/mcp/sessions/<token>/servers/<id>`、`/mcp/applications/<token>/servers/<id>` —— 需他方令牌；索取或猜测令牌超出"用这把 key 发只读请求"的授权字面。未试。
- 任何 `tools/call`（含 `mcp__builtin__*` 全部） —— 发消息 / 建会话 / 改设置 / 动账号 / 耗额度。未试。
- 停止条件核验：全程**未出现** 401-之后再无、要求登录、验证码、Windows Hello ⇒ 未触发 ⑦ 的停止分支。

### 残留（收尾要求）

- **本探针未启动任何进程或监听端口**：两次请求由一次性 Python 客户端发出，进程即退。
  复核 `Get-Process python` → 无我残留；`netstat` 监听数 **39 → 39 不变**；`52393` 仍且仅由 `pid 30936` 的 Qoder 持有（未受影响）。
- `D:\QoderData\mcp-router.json` mtime 仍为 `2026-09-27T12:04:05`（= 它自己启动时的写入时刻，早于探测）⇒ **只读，未被改动**。
- 未触碰 Qoder 任何设置 / 配置 / hooks / 账号。`apiKey` 值只在内存中使用，未写入本仓库任何文件、未进日志、未贴汇报。
- 探针脚本留在 `.scratch/phase0-qoder/`（`r1_initialize.py`、`r2_nokey.py`、`scan*.py`），脚本内**不含 key 字面量**（运行时读 json）。

---

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ 方法：**纯静态取证**（exe 版本信息、builtin MCP 工具契约原文、本地 transcript JSONL 结构、配置文件结构、注册表、netstat）
- 特殊地位：Qoder 是**本次工作的宿主软件**。全程未修改 Qoder 的任何设置、配置、hooks、账号（本角色 self-imposed 边界）。
- 被调查实物与版本号：
  - Qoder Desktop **0.3.3** — `D:\Qoder\.qoder-versions\0.3.3\Qoder.exe`（**按版本建目录**的安装布局）
  - Agent 引擎 **1.1.53** — 实测 transcript 每行 `version:"1.1.53"`、`entrypoint:"cli"`、`userType:"external"`（345 行一致）
  - 配置目录：`QODER_CONFIG_DIR = D:\QoderData\`（`.auth .bin .cache .models app bin entry file-history logs memory plugins projects qoder-knowledge security-resources shell-snapshots skills tmp` + `settings.json mcp-router.json installation_id skill-usage.json .qoder-app-status.json`）；另有历史默认目录 `%USERPROFILE%\.qoder\`
- 原始输出归档：`D:\Project\Lucero\.scratch\phase0-qoder\`
- 真实消息预算使用：**0 / 3**

## 关键结构性事实（**本报告最需要主控读的一段**）

1. Qoder 对外的**全部可编程面 = builtin MCP 工具**（原文取自本机 `mcp_list` 描述）：
   09-19 采到 **7 个** —— `create_chat_session`、`fork_chat_session`、`list_chat_sessions`、`read_chat_session`、
   `send_message_to_chat_session`、`wait_chat_sessions`、`qoder_cron`；
   **[第 3 轮实测] 09-27 复采为 8 个** —— 新增 `mcp__builtin__skill_manage`（`mcp_list` 关键字复核命中）。
   ⇒ 工具面**会随小版本增删**，Adapter 的能力声明必须按版本门控，且不得把"工具清单"当常量硬编码。
   这套接口的粒度是 **turn 级**，不是 token 级；没有 streaming、没有 abort/interrupt、没有模型/参数切换、没有额度。
2. `mcp-router.json` 实测结构 `{schemaVersion:2, pid, baseUrl:"http://127.0.0.1:<port>", apiKey, startedAt}`，
   且 `netstat -ano -p tcp` 实测该 pid **确实在 LISTENING `127.0.0.1:58129`** → Qoder 内部有一个带鉴权的本地 HTTP MCP router。
   **本角色未探测其 REST 面**（09-19 判断：需使用 `apiKey` = 越界）。**→ 09-27 经授权已探完，见首节：它是 MCP 上游代理面而非会话 API，不能承担 Adapter 入口。**
   [第 3 轮更正] 该读数是 09-19 的旧值：`pid 24480`/`58129` → 09-27 复测为 **`pid 30936`/`52393`**（`startedAt 2026-09-27T12:04:05`）⇒ **端口每次启动重分配、pid 必变，缓存即失效**。
3. transcript 是 **Claude-Code 形制**的 JSONL：`uuid`/`parentUuid` 对话树 + `isSidechain` 子链 + `active-leaf`(带 `leafUuid`,`explicit`) 分支游标 + `toolUseResult` + `attachment`。
   → 与 WorkBuddy/CodeBuddy 同族（见矩阵 §同构族）。
4. `create_chat_session` **只有 `prompt` 一个业务参数**（无 model / 无 cwd / 无附件）→ 与主控在 `orchestration.md` 里
   已记录的现实约束一致："worker 的模型档位设不了"。本报告为该结论提供字段级证据。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | NATIVE | 7 个 builtin 工具的行为本身；`.auth/machine_id`；`mcp-router.json{baseUrl,apiKey}` | 在 Qoder 内运行的 agent 天然以当前登录态工作，创建/驱动会话不需要二次认证 | 仅对**同机同用户**成立。外部进程要复用必须拿 router 的 apiKey（未授权）。降级：LOCAL_DATA 只读 |
| 2 | 软件版本发现 | LOCAL_DATA | exe VersionInfo；transcript `version`；`settings.json`；`.qoder-app-status.json`；**[实测] router pid 的 `Get-Process Path` 直接暴露版本目录** | Desktop 版本第 1 轮 `0.3.3` → **第 3 轮实测 `0.4.2`**（`D:\Qoder\.qoder-versions\0.4.2\Qoder.exe`）；transcript 内引擎 `version` 第 1 轮为 `1.1.53`（**本轮未复采**，见 UNKNOWN） | 无 CLI `--version` 可跑（见 #21）。降级：读目录名 / 读进程 Path |
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
| 22 | SDK/API/App Server | **EXPERIMENTAL（面已实测穷尽，用途仍不可用）** | `mcp-router.json` + netstat + **[实测] 两发对照** + **[静态] 全量路由枚举** | 服务真实存在且鉴权可过：**带 key → 404 / 不带 key → 401** ⇒ key 被接受；但面只有 `/mcp/{servers,sessions,applications}/…` + `/oauth/callback` + `/mcp/add`，**无任何列举/发现端点**，且 `builtin` 实测不在已注册上游中 | **等级不上调的理由（⑧）**：本次没有任何一次"经此面完成某项能力"的跑通 —— 拿到的是**边界证明（含负向）**，不是能力打通。对外部 Adapter 而言它等价于不可用。要升级需厂商开放"列举已注册 server"或暴露 builtin 通道 |
| 23 | 精确跳转原客户端会话 | NATIVE（部分） | `HKCR\qoder` 与 `HKCR\qoder-app` 均注册 `URL Protocol`；`qoder\shell\open\command` = `"…\Qoder Launcher.exe" "%1"` | 两套协议并存（`qoder://` 走 Launcher，`qoder-app://` 另一路）→ 跳转机制确定存在 | URL 是否接受 sessionId **未实测**（会弹用户窗口）。降级：`read_chat_session` 给 sessionId，用户自己在 UI 找 |
| 24 | UI Automation 可行性 | UI_AUTOMATION | `D:\QoderData\bin\qoder-computer-use\QoderComputerUse.exe`；`resources/bin/appshot_bare_modifier_monitor.exe`；`~/.qoder/logs/runs/<ts>-<id>-p<pid>/qodercli.log` | Computer Use 执行体**已随包安装**，且有 appshot/修饰键监视辅助件 → 官方自动化链完备且在本机可用 | Qoder 是宿主：用它自己的 computer-use 驱动它自己 = 递归风险，需主控单独评估 |
| 25 | 版本变化风险 | COMPATIBLE | 综合 + **[实测 8 天跨度的真实升级]** | **实测证据**：09-19 采到 `0.3.3` / pid 24480 / 端口 58129 → 09-27 复测为 **`0.4.2` / pid 30936 / 端口 52393**（`mcp-router.json` 的 `pid`/`baseUrl`/`startedAt` 三项全部变了，`schemaVersion` 仍 2）⇒ **8 天内完成一次小版本升级，且 router 监听端口每次启动重分配** | ① 任何"记一次端口/pid"的做法当场失效，Adapter 必须每次从 `mcp-router.json` 重读；② `.qoder-versions/<ver>` 目录化安装 + `fast-update` ⇒ 版本会变；③ 好消息：MCP 工具契约（描述文本本身）跨这两次采样未见变化，**绑工具面比绑 router 稳** |

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
| ~~22~~ | ~~mcp-router 的 REST 面~~ | **第 3 轮已解决（结论为否定式）**：面已穷尽 = MCP 上游代理 + OAuth 回调，无发现端点；key 实测可过鉴权（401/404 对照），但本机无可寻上游、`builtin` 不在此面 ⇒ 对外部 Adapter 不可用。详见本文首节 |
| 16 | `needs_attention` 的载荷结构 | 未制造过一次真实审批（会打断用户，且属对宿主写状态） |
| 18 | 附件注入通道 | 工具无附件参数；本地 jsonl 里却有 `attachment` 行 → 说明存在通道但入口未知 |
| 13 | `usage` 全 0 的原因 | 无法区分"Qoder 不上报"与"该账号体系无此计量"。只有厂商侧能回答 |
| 23 | deep link 会话粒度 | 实测会弹用户窗口，属打扰 |

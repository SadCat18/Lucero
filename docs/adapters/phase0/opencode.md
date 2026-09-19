# Phase 0 调查报告：OpenCode

- 调查角色：`survey` ｜ 日期：2026-09-19 ｜ 方法：**纯静态取证**（exe 版本信息、SQLite schema + 枚举实测、app.asar 路由字符串扫描、本地缓存文件）
- 被调查实物与版本号：
  - **OpenCode Desktop 1.18.14**（`FileVersion` / `ProductVersion` 双证）— `%LOCALAPPDATA%\Programs\@opencode-aidesktop\OpenCode.exe`（231,549,320 B，Electron，`app.asar` 138,692,922 B）
  - 更新源 `app-update.yml` → github `anomalyco/opencode`
  - 本机 **未安装 `opencode` CLI**（npm 全局目录只有 claude/codex/pnpm/vue，无 opencode；`D:\opencode\cache\bin\` 为空目录）
- 认证方式：`D:\opencode\data\auth.json` 实测结构 = 按 providerId 分桶的 `{type, key}`（本机 2 个 provider，值未读取）
  → **各家模型 Provider 的 API Key 直连**，不是账号登录。DB 内 `account` / `control_account` / `credential` 表**均 rows=0** → 用户未登录 OpenCode 账号。
  程序化侧对应 `/provider/auth`、`/auth/{providerID}`、`/api/integration/{id}/connect/{key,oauth}`、`/api/integration/attempt/{id}/complete`、`/api/credential/{id}`
- 原始输出归档：`D:\Project\Lucero\.scratch\phase0-opencode\`（`db_schema.py`、`db_probe.py`）
- 真实消息预算使用：**0 / 3**

## 关键结构性事实

1. OpenCode 桌面端**内置一个完整的本地 HTTP + SSE 服务**，且存在**两代 REST 命名空间并存**：
   旧版根路径（`/session/...`、`/event`、`/provider`）与新版 `/api/...`（`/api/session/{id}/interrupt|wait|history|model|…`）。
   实测路由字面量：旧版 141 条、新版 60 条。
2. 本地库 `opencode.db` 是**事件溯源**结构：`event{id, aggregate_id, seq, type, data}` + `event_sequence{aggregate_id, seq, owner_id}`，
   实测单会话 `seq` 连续范围 `0..1547` → 增量同步有原生游标。
3. 存在 `/experimental/capabilities` 端点 —— 与 Lucero「Adapter 握手声明 Capability」**语义直接对应**，值得优先探明其返回结构。

## 能力矩阵（25 项）

| # | 能力 | 等级 | 证据来源 | 实测结果 | 限制 / 失败降级 |
|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | COMPATIBLE | `auth.json` 结构、`account`/`credential` 表 rows=0、路由 `/provider/auth`、`/api/integration/*/connect/{key,oauth}` | 复用 = 复用同一份 provider key / 同一 credential 存储；OAuth 有 `authorize`+`callback`+`attempt/complete` 三段式 | **不是"复用客户端登录态"**：OpenCode 无统一账号，Adapter 要按 provider 逐个处理。降级：Adapter 自带 provider key（等于不复用） |
| 2 | 软件版本发现 | LOCAL_DATA | `Get-Item OpenCode.exe → VersionInfo`；`session.version` 列 | `1.18.14`；DB 内 `session.version` 实测值为 `"local"`（数据版本标记，非软件版本） | `/api/health`、`/global/health` 存在但**未探测**（需起服务）。降级：exe 版本信息 |
| 3 | 列出已有会话 | NATIVE | 路由 `GET /session`、`/api/session`、`/api/session/active`、`/experimental/session`；LOCAL_DATA `session` 表 | 表实测 9 行，含 `directory,project_id,parent_id,agent,model,permission,cost,tokens_*` | 服务需运行；桌面端是否自动拉起未实测。降级：LOCAL_DATA 直读 `session` 表 |
| 4 | 读取完整会话 | NATIVE | `GET /session/{id}/message`、`/api/session/{id}/history`、`/session/{id}/message/{messageID}`、`/part/{partID}` | `message`(490) / `part`(2368) 实测可 JOIN；part 类型 7 种齐全 | 未实测响应体分页。降级：LOCAL_DATA SQL 直读（同一 schema） |
| 5 | 新消息增量同步 | NATIVE | SSE `/event`、`/api/event`、`/api/session/{id}/event`；LOCAL_DATA `event` 表 | 实测 `event.type` 仅 4 种：`message.part.updated.1`(6270)、`message.updated.1`(1903)、`session.updated.1`(525)、`session.created.1`(9)；带版本号后缀 `.1` | 事件名带 `.N` 版本段 → Adapter 必须按版本分派。降级：LOCAL_DATA 按 `seq` 游标轮询 |
| 6 | 双向同步 | NATIVE | 入站 `POST /session/{id}/message`、出站 SSE + `event` 表 | 同一服务承载读写，天然双向 | 桌面端与 CLI 端**是否共享同一 event bus 未实测**。降级：LOCAL_DATA 只读 + HTTP 只写 |
| 7 | 向指定已有会话发消息 | NATIVE | `POST /session/{id}/message`(同步)、`/session/{id}/prompt_async`、`/api/session/{id}/prompt`、`/session/{id}/command`、`/session/{id}/shell` | 同步/异步两条投递路径 + 斜杠命令 + shell 直发；另有 `session_input` 表 `delivery`、`admitted_seq`、`promoted_seq` 三列（实测 0 行，结构在） | `session_input` 本机空 → 排队语义未取到样本。降级：CLI 不存在 → 直接写库不可接受（禁区），只走 HTTP |
| 8 | 流式响应 | NATIVE | `/event` + `text/event-stream`(asar 内 31 处命中) + `EventStream`(193) + SSE(466) | 事件类型 `message.part.updated.1` 实测占事件总量 **72%**(6270/8707) → 增量以 part 为单位推送 | part 级增量，非 token 级；token 级要看 `/api/session/{id}/event` 实际帧。降级：LOCAL_DATA WAL 轮询 |
| 9 | 中止当前生成/任务 | NATIVE | `POST /session/{id}/abort`、`/api/session/{id}/interrupt` | abort 与 interrupt 两代端点都在 | 未实测中断后状态机。降级：杀 OpenCode 进程（会丢会话进行中的 turn，代价高） |
| 10 | 读取模型列表 | NATIVE + LOCAL_DATA | `GET /provider`、`/api/model`、`/api/provider`、`/models`、`/config/providers`；`D:\opencode\cache\models.json` | 缓存目录实测 **4,690,520 B / 221 个 provider 键**（models.dev 目录，含 limit/cost/reasoning 元数据） | 缓存可能过期；provider 可用性要看 `/provider`。降级：LOCAL_DATA 读缓存 |
| 11 | 切换模型 | NATIVE | `PATCH/POST /api/session/{id}/model`；`session.model` 列 | 实测 `session.model = {"id":…,"providerID":…,"variant":…}` 三元组 JSON | 切换后是否影响进行中 turn 未实测。降级：LOCAL_DATA 只读（不可写库）→ 必须走 HTTP |
| 12 | 推理强度/参数控制 | COMPATIBLE | `session.model.variant`、assistant `message.data.variant`、`/session/{id}/command` | **OpenCode 用 `variant` 表达推理档位，没有独立的 effort 枚举** | 各家语义不同（Codex=`reasoning_effort`、ZCode=`thought_level`、WorkBuddy=`--effort`）→ Core 的归一字段要以 variant 为兜底。降级：provider 级 config |
| 13 | 精确额度读取 | READ_ONLY | `session.cost`、`tokens_{input,output,reasoning,cache_read,cache_write}`；assistant `message.data.cost/tokens` | 实测 `session.cost = 0.0053…`、`message cost = 0.00121198`、`tokens{cache,input,output,reasoning,total}` → **会话与消息级 token/费用可读且精确** | 这是"已花费"，**不是"账户剩余额度"**。asar 内 `rateLimit`(3)、`remaining`(15)、`quota`(1) 命中极少且无法归属端点 → 账户级额度 = UNKNOWN。降级：无 |
| 14 | Reset 时间 | **UNKNOWN** | 全量路由扫描 + property 扫描 | 未找到任何 quota-reset 端点或字段 | 原因：OpenCode 走 provider 直连计费，套餐重置周期归 provider 所有，本机无证据。需逐 provider 调查或联网核实 |
| 15 | 任务实时状态 | NATIVE | `GET /session/status`、`/api/session/active`、`/api/session/{id}/wait`；`session.time_compacting/time_archived` | `wait` 端点 = 阻塞直到会话空闲，是编排原语 | 未实测 wait 超时行为。降级：LOCAL_DATA `time_updated` + `event` 表 |
| 16 | 审批读取 | NATIVE | `GET /permission`、`/api/permission/request`、`/question`、`/api/question/request`、`/session/{id}/permissions/{permissionID}` | 审批(permission) 与提问(question) **是两类不同对象**；另有 `permission` 表(project_id,action,resource) 与 `session.permission` JSON 规则 | 本机 `permission` 表 **rows=0**、`session.permission` 只在子代理会话里有规则样本（`[{permission:"todowrite",pattern:"*",action:"deny"}]`）→ 待审请求实例未取到 |
| 17 | 审批响应 | NATIVE | `POST /permission/{requestID}/reply`、`/api/session/{id}/permission/{requestID}/reply`、`/question/{requestID}/{reply,reject}` | reply/reject 双动作，且 question 侧显式 reject | 未实测 reply payload 结构。降级：无（必须走 HTTP） |
| 18 | 附件 | NATIVE | `part.data.type = "file"` 实测 `{filename, mime, url, type}`；`/session/{id}/message` 请求体 | file part 实测存在(4 条)；另有 `patch` part(91 条) 承载 diff | url 形态（本地路径 / data URI / 远端）未确认。降级：放文件进 cwd |
| 19 | MCP/Skill/Rule/Hook | NATIVE | `/mcp/{name}/{connect,disconnect,auth,auth/authenticate,auth/callback}`、`/mcp/oauth/callback`；`/api/{skill,command,agent}`；config keys `mcp`/`skills`/`rules`/`hooks`/`instructions` | MCP 有完整生命周期含 **OAuth 三段**；配置项五类齐全 | 新版 `/api/skill`、`/api/command`、`/api/agent` 各自只命中 2 次 → 可能仍在成型，**不要当作稳定面**。降级：读 `opencode.jsonc` / `~/.config/opencode/` |
| 20 | 本地会话数据库 | LOCAL_DATA | 实测 open | `D:\opencode\data\opencode.db` **559,341,568 B**，19 表 + 16 索引，`synchronous`/WAL 模式（`-shm`/`-wal` 存在）| **写库属禁区**（改第三方数据）→ 只读。且 WAL 活跃时只读连接可能读到旧快照 |
| 21 | CLI | **UNKNOWN** | 路由扫描命中字符串 `opencode serve`(5)、`"serve"`(4)；npm 全局无 opencode；`cache/bin` 空 | 上游确有 CLI，但**本机无实物可执行体** → 无法验证任何 CLI 参数 | 原因：本机只装了 Desktop。要定级必须安装 CLI = 红线④（装软件），本角色不申请。降级：Desktop 内嵌 HTTP 服务 |
| 22 | SDK/API/App Server | NATIVE | 141 + 60 条 REST 路由、SSE、`/api/pty/*`、`/tui/*`、`/vcs/*` | 除 REST 外还有 **PTY 端点**(`/api/pty/{id}/connect`、`connect-token`) 与 **TUI 控制面**(`/tui/{append-prompt,submit-prompt,select-session,open-sessions,execute-command,control/next,control/response}`) | asar 内 `@opencode-ai/sdk` 字符串 **0 命中** → 桌面端未打包官方 TS SDK，Adapter 需自己按路由生成客户端。降级：LOCAL_DATA |
| 23 | 精确跳转原客户端会话 | NATIVE | `POST /tui/select-session`、`GET /tui/open-sessions`；`HKCR\opencode` 注册 `URL Protocol` | **TUI 控制面能把客户端切到指定会话** —— 五家中唯一有证据的"程序化精确跳转" | 作用对象是 TUI 客户端，Desktop 的跳转未实测。降级：`opencode://` deep link（粒度未测） |
| 24 | UI Automation 可行性 | UI_AUTOMATION | Electron（Chromium 全家桶 dll 齐备） | 可行但价值最低 | 有完整 REST 就不该用 UIA |
| 25 | 版本变化风险 | EXPERIMENTAL | 两代路由并存实测 + `/experimental/*`(46 条命中) + 事件名 `.1` 版本段 + `migration`(38 行) / `data_migration` 表 | 实测风险信号：① `/session/{id}/…` 与 `/api/session/{id}/…` **同时存在且子命令集不同**（新版有 `wait`/`interrupt`/`revert/{stage,commit,clear}`，旧版有 `abort`/`revert`/`unrevert`）；② DB 有 38 条 migration 记录；③ `session_message` 表存在但 rows=0 而 `message` 有 490 行 → **正在做表结构迁移** | 必须锁 `1.18.14` 起 Adapter；升级要重跑路由与 schema diff |

## 建议接入层级与 fallback 链

主链 **Native API（桌面端内嵌 HTTP/SSE）**：

```
NATIVE: 启动/附着 OpenCode 服务，用 REST + SSE
   ├─ 读链 fallback：LOCAL_DATA 只读 opencode.db（同 schema，seq 游标）—— 服务没起时仍可观测
   ├─ 写链 fallback：无（CLI 本机不存在；写第三方库属禁区）→ 服务不可用则 capability 直接降级为 READ_ONLY
   └─ 跳转：/tui/select-session（TUI 在跑时）
UI_AUTOMATION: 不建议投入
```

**结构性结论**：OpenCode 是"读链双保险（API + 本地库同 schema）、写链单点（只有 HTTP）"的形态。

## 仍需 UI Automation 的能力

- **#14 Reset 时间**：连 API 侧都无端点，UIA 也不一定有；应先判定为"该家不提供"，而非用 UIA 硬凑。
- 其余 24 项均有 NATIVE / LOCAL_DATA / READ_ONLY 路径。

## UNKNOWN 清单与原因

| # | 能力 | 为什么是 UNKNOWN |
|---|---|---|
| 14 | Reset 时间 | 全量路由与属性扫描未见 quota/reset 结构；OpenCode 用 provider 直连，账户额度不归它管。需联网核实上游文档或逐 provider 调查 |
| 21 | CLI 全部能力 | 本机未安装 `opencode` CLI（`npm -g` 无实物、`cache/bin` 空）。定级需装软件 → 撞红线④，本角色不申请 |
| 7 | `session_input` 排队/转向语义 | 表结构存在但本机 rows=0，拿不到 `delivery`/`admitted_seq`/`promoted_seq` 的实际取值 |
| 16 | 待审 permission 实例 | `permission` 表 rows=0；只取到规则(rule)侧样本，没取到"正在等人批"的样本 |
| 2/15/17 | 服务端点响应结构 | 需启动 OpenCode 并访问 `/api/health` 等；静态证据已足够定级，未动用动态预算 |

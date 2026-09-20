# API & Ownership Contract v0.1（Wave-1 契约③）

- 状态: **accepted**（用户 2026-09-19 16:52 批准，`frontend` 据此派发；§7 补 `core/api/**` 归属）
- 范围: Core ↔ 客户端（桌面 / 浏览器 / 未来的 PWA 与 CLI）的 REST + WebSocket，以及跨角色文件所有权
- 上游依据: ADR-009 REST + WebSocket、架构文档「API」节的端点清单、ADR-008（先落库再推）、
  领域模型「Idempotency」「Aggregate Version」「UNKNOWN 是合法状态」「Audit 与 Secret」
- 不依赖它就能做的事：Wave-2 的垂直链可由 `scripts/` 里的 CLI 驱动闭合（Sprint 允许），本契约主要服务 `frontend`

## 1. 监听与信任边界

1. **只绑 `127.0.0.1`**，端口由 Core 选定并写入数据目录下的 `core.endpoint`（`host:port` + `token`）。
2. **必须有共享口令**：除 `/api/v1/health` 外所有请求带 `Authorization: Bearer <core-token>`。
   理由不是防外网，是防本机任意进程与浏览器页面——Phase 0 实测到反面教材：
   WorkBuddy `--auth none` 的 help 自述"允许任意本地进程通过该服务执行命令、读写文件"。
   一个无鉴权的本地控制平面 = 任意本地进程可以驱动你的 AI 执行高风险动作。
2.1 **WebSocket 例外（REQ-F-2 裁定，17:35）**：W3C 规定浏览器不能在 `WebSocket` 握手携带自定义头，
   所以 `/ws/v1/events` **不接受** `Authorization` 头。改用**一次性短票**：
   `POST /api/v1/ws-ticket`（走正常 Bearer）→ 返回 `{ticket, expires_in: 30}` →
   连接 `/ws/v1/events?ticket=…`；服务端校验后**立即作废该票**，票为单次使用、30 秒过期、不入库明文。
   否掉的两个备选：`Sec-WebSocket-Protocol` 携带（把口令塞进协议协商字段，语义污染且要求服务端回显子协议）、
   同源反代注入（多一个组件，Tauri 未进场时无从属处）。
   **禁止**为绕开这条而放宽成"WS 无鉴权"——那等于给任意本地进程开一条只读的完整业务时间线。
3. **不开放 CORS**。带 `Origin` 的请求只放行：桌面 WebView 自身来源，**加上显式配置的 dev origin**
   （`LUCERO_DEV_ORIGIN`，必须精确到 host:port，如 `http://localhost:5173`）；其余一律 403。
   **REQ-T-4 裁定：不因"回环"而默认放行** —— 任意端口的 localhost 都放行等于把 403 变成装饰，
   任何本地网页都能驱动控制平面。dev 白名单是显式、精确、可关的。
4. 单用户，不做账号体系。写操作必须带 `X-Lucero-Principal`（允许值 `local-user`），
   缺失即 `401`；`system` 与 adapter principal 只能由 Core 内部产生，**不接受外部声明**。
   **REQ-T-4 裁定**：`POST /api/v1/ws-ticket` **不要求 Principal**（它是读侧连接的鉴权前置，不写业务状态），
   但**必须带 Bearer**——否则它就成了绕过 §1.2 的口子。
4.1 **`GET /health` 的 `state` 词表（REQ-T-1 裁定）**：`BOOTING` / `READY` / `STOPPING` / `STOPPED` / `UNKNOWN`。
   刻意**不设 `DEGRADED`**：Core 整体不该有"半健康"，降级属于 Adapter/Capability 层的词汇，
   混进来会让 UI 把"某个 Adapter 掉了"读成"整个 Core 可疑"。`migration_version` **不得写死常量**，
   必须取库内 `MAX(version)` 比对（backend 已实测：`000003` 一落地，写死 `==2` 的断言立刻假红）。
   `UNKNOWN` 保留为合法值——探针失败/还没读到库时不许谎报 `READY`。
5. 版本前缀 `/api/v1` 冻结：只可**增加**字段与端点；改名、删字段、改语义一律走 `/api/v2` 并配 ADR。

## 2. 全局约定

| 主题 | 规则 |
|---|---|
| 时间 | 一律 Unix 毫秒整数，不返回 RFC3339 字符串 |
| 分页 | `?cursor=<opaque>&limit=50` → `{items, next_cursor}`；**不得**用 offset |
| 幂等 | 所有**改变业务状态**的 POST 必须带 `Idempotency-Key`；命中已完成记录时**返回首次的响应**，不重放副作用（领域模型「Idempotency」）。**REQ-F-11 豁免（18:03 确认）**：凭证签发类端点必须排除在幂等缓存之外——目前唯一一条是 `POST /api/v1/ws-ticket`。理由与 frontend 的判断一致且更强：票是**单次使用 + 校验即作废**的，若被幂等缓存，命中缓存会返回同一张票或其"已作废"结论，等于让一次重放变成可预测行为。豁免不许靠约定，**必须有一条反向用例钉住**（同一请求两次 → 拿到两张不同的有效票）；将来新增豁免端点要显式列进本表，不能靠注释口头豁免 |
| 乐观并发 | 聚合读响应带 `version`（ADR-017 的列）；变更请求带 `If-Match: <version>`，不符 → `409 version_conflict` |
| 错误体 | `{"error": {"code": "...", "message": "...", "retryable": bool, "details": {…}}}`，`code` 复用契约② §错误码集合，HTTP 码与之对应 |
| 未知值 | 可空字段用 `null` + 伴随 `confidence`；**严禁**用 `0`/`false`/`"offline"` 表示"不知道" |
| 原始数据 | 凡归一化字段（`status`/`availability` 等）必须同时可取到 `provider_raw`，UI 有权展示"原始是什么" |
| 敏感 | 任何响应体不得含 secret 明文，只含 `vault://` 引用（redaction 在写入库时就已强制） |

## 3. 端点（First Sprint 需要面的最小集）

```
GET  /api/v1/health                      → {state, version, migration_version, ready}   （唯一免鉴权）
POST /api/v1/projects                    → 建项目（canonicalize 后的 path 冲突返回 409）
GET  /api/v1/projects?cursor=&limit=     → 列表；GET/PATCH /api/v1/projects/{id}
POST /api/v1/adapters/{key}/probe        → 触发握手+能力核定；GET /api/v1/adapters[/{id}]
GET  /api/v1/adapters/{id}/capabilities  → 逐能力等级 + effective_backend + last_verified_at
GET  /api/v1/models?adapter=             → 模型列表（含 parameter_schema_json）
POST /api/v1/adapters/{id}/sessions:discover → 从 Provider 拉会话清单、按 remote_session_id 去重 upsert 本地行，
                                          返回发现的 session id 列表（幂等：重复 discover 不产生新行）
POST /api/v1/sessions/{id}:manage      → 置 managed（受管理会话才开同步与通知）
GET  /api/v1/sessions[/{id}]           → 详情；GET /api/v1/sessions/{id}/messages?after_seq=
POST /api/v1/sessions/{id}:sync        → 显式增量同步，返回 sync_state + next_cursor
POST /api/v1/tasks                       → 建任务（必须带 Idempotency-Key）
GET  /api/v1/tasks?status=&project=&cursor=
PATCH /api/v1/tasks/{id}                 → 只允许 priority/title/description/mode；带 If-Match
POST /api/v1/tasks/{id}:queue|pause|resume|cancel|accept   → 状态动作（custom verb，不硬套 REST）
POST /api/v1/tasks/{id}:transfer         → 建 Handoff（body: target_adapter, target_session?, reason）
GET  /api/v1/executions?task_id=         → 该任务全部 Execution（含 attempt_no/trigger_type/predecessor）
GET  /api/v1/executions/{id}             → 含三份快照：model / permission / environment
POST /api/v1/executions/{id}/send        → **C10/REQ-F-1 补（17:45）**：映射契约② `session/send`；
                                          body `{text, delivery: start_now|guide|queue, attachments?}`，
                                          必带 `Idempotency-Key`；Provider 侧 delivery 态不支持时返回
                                          `DELIVERY_MODE_UNSUPPORTED`，**不得静默降级**
GET  /api/v1/executions/{id}/output?after_seq=  → 流式批次（读 execution_output_chunks，非事件）
POST /api/v1/executions/{id}:cancel      → 中止当前生成（映射 session/stop；不支持返回 501 而非假成功）
GET  /api/v1/approvals?state=PENDING     → 待决审批；POST /api/v1/approvals/{id}:resolve
GET  /api/v1/quota?adapter=              → 观测值 + observation_kind + confidence（UNKNOWN≠0）
GET  /api/v1/notifications?state=&cursor=; POST /api/v1/notifications/{id}:read|handle
GET  /api/v1/search?q=&scope=messages,tasks   → FTS5；返回命中 + 片段 + 上下文 id
GET  /ws/v1/events?from_seq=N            → 事件流（含 resync_required、ping）
```

规则：状态动作用 `:verb` 后缀（`tasks/{id}:accept`），**不引入 `/actions` 复数端点**；
`PATCH` 永不改状态（状态变更必须走 `:verb`，否则绕开状态机与事件）。

## 4. 垂直链的可执行映射（这条就是验收路径）

| 链上一步 | 客户端调用 | Core 侧效果 |
|---|---|---|
| Create Project | `POST /projects` | `project_created` |
| Discover Fake Session | `POST /api/v1/adapters/{id}/sessions:discover` → 取回 session id 列表 | `session_discovered` |
| Mark Managed | `POST /api/v1/sessions/{id}:manage`（id 来自上一步） | `session_managed` |
| Create Task | `POST /tasks` + `Idempotency-Key` | `task_created` |
| Create Execution | `POST /tasks/{id}:queue` 触发调度 | `execution_created` → `task_preparing` |
| Send Message | `POST /executions/{id}/send`（映射 `session/send`，带三态 `delivery`） | `execution_started` → `task_running` → `message_recorded` |
| Stream Output | WS `stream/delta` + `GET /executions/{id}/output` 补历史 | `execution_output_flushed` |
| Request Approval | WS `approval_requested` | `execution_approval_requested` → `task_waiting_approval` |
| Resolve Approval | `POST /approvals/{id}:resolve`（幂等） | `approval_resolved` (+`grant_issued`) → `task_running` |
| Continue → Succeeded | WS `turn/finished` | `execution_succeeded` → `task_agent_completed` |
| User Accept | `POST /tasks/{id}:accept` | `task_accepted` |

`test` 角色据此写端到端链测试，`frontend` 的面板必须**只能靠这些端点**把整条链显示出来；
任何"面板得知道内部字段才能渲染"的情况都算 API 设计缺陷，写 Coordination 而不是在 UI 里绕过。

## 5. WS 帧与游标（与契约① §2/§8 一致）

- 连接后第一帧 `{type:"hello", from_seq}`；服务端先补发 `event_journal` 中 `seq > from_seq` 的事件，再切实时。
- journal 已裁剪到无法补发 → 第一帧改 `{type:"resync_required"}`，客户端必须回去走 REST 全量拉，
  **不得**用部分事件流假装状态完整。
- 心跳 `{type:"ping", server_seq}`（故意无 `event_id`）；UI 用它区分"活着"与"卡住"。
- 推送只发生在 DB 事务提交之后（ADR-008）；UI 永远看不到库里不存在的状态。

## 6. OpenAPI 的归属（关键：我不写 yaml）

`specs/openapi/core-v1.yaml` 由 **`frontend` 角色**首次生成并长期维护，作为本契约③的机器可读投影。
规则：
1. 以本文件为**准**；yaml 与 Go handler 不一致 → 算缺陷，谁先发现谁写 Coordination，由主控裁定改哪边。
2. `frontend` 不得手写第二套类型定义：TS 类型必须从 yaml 生成（`packages/api-client`）。
3. 每次 `/api/v1` 面变更（新增字段/端点）→ yaml + 本文件 + 契约① 事件目录三处同步，缺一个 `test` 的
   ownership lint 要能报出来。

## 7. 文件所有权矩阵（`test` 的 lint 依此判定）

| 路径 | owner | 说明 |
|---|---|---|
| `core/migrations/**`、`core/internal/platform/**`、`core/internal/eventjournal/**` | `db` | 独占 `.sql` 修改权；`000001` 冻结 |
| `core/internal/task/**`、`execution/**`、`session/**`、`quota/**` | `backend` | |
| `core/api/http/**`、`core/api/websocket/**`、`core/cmd/core/**` | `backend` | **16:52 补**：transport 层原先无人归属。它是业务 Service 的薄投影，**不得含任何业务判断或状态迁移**；一律经各模块 Service 与 `db.Touch/Create`，WS 推送只能发生在事务提交之后（ADR-008） |
| `core/internal/approval/**`、`permission/**`、`audit/**` | `audit` | `approvals` 的 SQL 只准出现在 `internal/approval` |
| `core/internal/notification/**` | `backend` | **C12 补（17:45）**：此表原无 owner。Notification 是独立聚合，写入走 `Tx.Create(AggNotification,…)` 并发 `notification_created`；`audit`/`adapter` 需要发通知时**调它的 Service**，不得自己写 `notifications` 表 |
| `core/internal/adapter/**`、`process/**`、`adapters/fake/**`、`core/cmd/fake-adapter/**` | `adapter` | **11:25 补**：契约② §10 的 CO-1 裁定把 FakeAdapter 落点定在 `core/cmd/fake-adapter/`，但本表漏同步，导致 lint 把契约批准的正当落点判成越界（`test` 的 REQ-T5）。`adapters/fake/` 只放 fixture 与说明 |
| `core/test/**`、`scripts/**` | `test` | 不得在别人包里建 `_test.go` |
| `apps/desktop/**`、`packages/api-client/**`、`specs/openapi/**` | `frontend` | 禁止 import Core 内部类型 |
| `specs/**`、`docs/**`（角色自己的报告/进度文件除外）、`core/go.mod`、`core/go.sum`、`.gitignore`、`.gitattributes` | **主控** | worker 需变更时写 Coordination REQ |
| ~~`core/migrations/embed.go`~~ | — | **12:10 撤回本行：我曾给一个不存在的文件指派 owner**。`//go:embed ../../../migrations` 确实非法（embed 只能取本包及子目录），但结论不该是"那就在 migrations 里放 embed.go"——v0.1 **不做迁移内嵌**，交付方式是 `cmd/core -migrations <目录>`（backend 已这么跑，`find core -name embed.go` 零命中）。给不存在的路径派 owner = 制造"契约声明了却不存在的路径"，与我在事件契约 §6 刚写的「已登记 ≠ 有生产路径」自相矛盾。真要单二进制发布时（Phase 6）再补一行**并同时派工**。db 已接到的 `fs.FS` 版迁移入口保留，理由改为**可测性**（测试可用 `fstest.MapFS` 驱动迁移器，不碰磁盘），不再是内嵌 |
| `.qoder/progress/<role>.md` | 对应 role | 他人只读 |
| `core/`（根目录本身） | **主控** | 根目录只允许 `go.mod`/`go.sum` 两文件；新增任何根级文件都算越界（防依赖树、临时脚本落根） |
| `.qoder/progress/orchestration.md` | 主控独占 | 巡检额外可写心跳/待投递/NEEDS YOU 段 |
| `.qoder/evidence/**`、`.scratch/**` | 任意角色可写自己那份 | 已 gitignore，不入库 |

lint 判据：`git diff --name-only <基线>` 的每一项必须落在提交者所属角色的行内；命中即报越界。

## 8. 明确不做（v0.1）

认证体系与多用户、CORS 开放、`/api/v1` 之外的公开端点、GraphQL、长轮询替代 WS、
在 HTTP 层做任何业务状态裁决（裁决只在 Go Core）。

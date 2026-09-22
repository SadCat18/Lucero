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
2.2 **面板怎么拿到 core token（REQ-F-18 裁定，13:3x；本条是 §1 列表的第 2.2 项，紧跟第 2.1 项的票机制。
    注意别与内部契约的"§2.2 Redaction"混读 —— 跨文档引用请写全"契约③ §1 第 2.2 项"）**：
    **正式途径不是"前端持有 token"，而是前端根本不接触它。**
    Core 已把 `{host_port, token, version}` 写进 `<data-dir>/core.endpoint`（`core/cmd/core` 的 `(*Core).writeEndpointFile`），
    读它的是**本机进程**，不是页面：
    - **dev 期 = Vite dev-server 同源反代**：node 侧读 `core.endpoint`，转发 `/api/v1/*` 时注入
      `Authorization: Bearer …`；WS 用同一条代理（`ws: true`），**鉴权仍走 §2.1 的一次性票，不因代理而改变**。
      于是浏览器只跟 dev-server 同源通信 → **不需要**放宽 CORS（§3 不变），token 不进 JS、不进 bundle、不进浏览器内存。
    - **生产期 = Tauri shell 注入**（Rust 侧读文件后交给请求）。本轮 Rust/Tauri 整体推迟，
      所以 **v0.1 明确：观察面板不开放给真环境**，重开条件 = Tauri 进场那一轮。这不是缺口，是已划的边界。
    - **两条禁止**：① **不得**新增"把 token 发给客户端"的 HTTP 端点或配置项（那会把下面那条文件边界整个废掉）；
      ② **不得**用 `VITE_*`/`import.meta.env` 把口令烤进构建产物。
    - **⚠ 必须一起写清、不许美化的一条**：`(*Core).writeEndpointFile` 里 `os.WriteFile` 传的 `0o600` **在本机不成立** ——
      Windows 默认 ACL 会把 Go 的 0600/0700 映射成 `rwxrwxrwx`（本机实测，属既有环境事实）。
      所以 `core.endpoint` 对**同用户的任意进程**实际可读。准确说法是：
      **§1.2 的共享口令是"同用户边界内的门帘"，不是隔离机制**；正因如此更不能多开一个能远程要到口令的口。
      报告与 UI 里**不得**出现"token 受文件权限保护 / 仅本人可读"这类断言。
    - **§2.1 与本条的关系要说明白，免得两边互相打脸**：§2.1 否掉的是"**用反代给 WS 注入鉴权头**"
      （浏览器不发头，票方案更干净），**不是**否掉"反代作为 dev 期 REST 的凭证载体"。两者不冲突：
      票仍由 Core 签发、仍单次使用，代理只负责把带 Bearer 的 REST 请求送出去。
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
| 分页 | `?cursor=<opaque>&limit=50` → `{items, next_cursor}`；**不得**用 offset。**REQ-F-19 裁定（11:3x）：`next_cursor` 为""（空串）是"没有下一页"的唯一合法表示**，`null` 与"字段省略"都**不许**成为第二种口径（同一件事两种写法 = 生成客户端要判两次）。判据依据实现：`api/http` 的分页出口先 `next := ""`、只有取满一页且非空时才 `encodeCursor(...)` 覆盖，而 `encodeCursor` 永不为空串（内容=时间戳+id），**所以"空"与"有值"不可能撞**。frontend 自认的一条也记在这里：它先前在 yaml 写"无更多数据时为 null 不是空串"，那是**它自己的投影而不是契约原文**，已撤——本轮它主动改回来是对的 |
| 同步游标 | **REQ-F-21 裁定（12:2x）：不照抄上一行的分页口径，两者语义相反。** 依据是实现：`000001:160` 的 `sessions.sync_cursor TEXT`**可为 NULL 且无默认**、`internal/session` 把它当**每会话持久化的位置**存读（`repository.go` 的列清单 + `SyncedPayload.SyncCursor`，事件契约 §6 `session_synced` 的样例值是 `"20"`）。所以：① **`""`/NULL 在这个域里的意思是"从未同步过 → 下次从头拉"**，与分页的"没有下一页"**正好相反** —— 把上一行口径抄过来，就等于让客户端在"没新消息"时收到一个"从头开始"的信号，那正是 §5 明令禁止的**缺省即全量重放**；② **没有新条目时必须回显当前位置**（等于入参游标，或库里已存的 `sync_cursor`），**不许返回空串**；③ **"不知道 / 不可同步"由 `sync_state` + `state_confidence` 表达**，不许让游标空值兼职（§12"不许用 `0`/`false`/空表示不知道"的同一条线）；④ `null` 只允许一种含义 = **库里该列为 NULL（从未同步）**。当前 yaml 写 `required: [sync_state, next_cursor]` 同时 `type: [string,"null"]`、生成物里又出现可省略的 `sync_cursor?` —— **"省略 / null / 空串"三种"没有"并存就是要收掉的形状**，合法值只许两种：**非空字符串（有位置）** 或 **`null`（无位置）**。**REQ-F-22 续裁（12:5x，不等端点落地）**：`Session.sync_cursor` 与 `SyncResult.next_cursor` 在响应 schema 里一律 **`required: true` + 可空 + 字符串侧 `minLength: 1`** —— 也就是"键必须在、值只有两种合法形状"。理由就是本行上面那条：键可省略 = 客户端要处理"省略"这第三种"没有"，而 codegen 会把它生成 `sync_cursor?: string | null`（现在正是这样），第三种状态就永久留在类型里。**frontend 只收 `minLength`、没擅自加 `required` 是对的**（`specs/**` 归主控，它没越界）；现在补上 `required`，**backend 的 `GET /sessions` 与 `:sync` 出口在脱 501 时必须满足这条**（无位置就显式回 `null`，不许缺键）。`encodeCursor`/`sync_cursor` 的空串与非空由 `minLength` 分开：分页侧 `""` 合法且是唯一的"没有下一页"，同步侧 `""` 非法（要么位置串要么 `null`）——**两个域同一个字段名、两套口径，这是 REQ-F-21 的结论，别在 validator 里合并它们** |
| WebSocket 帧 | **REQ-WS-1 裁定（12:5x，含"为什么不引 `gorilla/websocket`"）**：Core 服务端自带 431 行手写 framer（`core/api/websocket/{ws,frames}.go`），已实测覆盖三处真会出事的地方——三处都在 `internal/api/websocket` 的 **`(*framer).read`** 里：入站帧**无掩码即拒**、**分片帧即拒**并回 `close(1002)`、64 位长度分支在 `make([]byte, n)` **之前**就查 `maxIncoming`（不会被巨型长度声明撑爆内存）。因此客户端约定：① **入站消息必须单帧、≤125 字节有效载荷**（我们的入站只有 `{type:"resubscribe", from_seq}` 一类小 JSON，够用）；② 不协商任何扩展，服务端**不回** `Sec-WebSocket-Extensions`，故浏览器不会启用 permessage-deflate；③ 入站只允许 text/binary/close/ping/pong 五种 opcode，其余按协议错误关。**重开条件**（满足任一条再引依赖，且要新 ADR）：要发/收 >125 字节的入站消息、要二进制分片、要 permessage-deflate、要服务端主动大扇出压帧。理由：这是本地优先产品的**第一个直接依赖**，而换掉一套已测绿的帧实现属于"拿不回的东西"。 |
| 幂等 | 所有**改变业务状态**的 POST 必须带 `Idempotency-Key`；命中已完成记录时**返回首次的响应**，不重放副作用（领域模型「Idempotency」）。**REQ-F-11 豁免（18:03 确认）**：凭证签发类端点必须排除在幂等缓存之外——目前唯一一条是 `POST /api/v1/ws-ticket`。理由与 frontend 的判断一致且更强：票是**单次使用 + 校验即作废**的，若被幂等缓存，命中缓存会返回同一张票或其"已作废"结论，等于让一次重放变成可预测行为。豁免不许靠约定，**必须有一条反向用例钉住**（同一请求两次 → 拿到两张不同的有效票）；将来新增豁免端点要显式列进本表，不能靠注释口头豁免 |
| 乐观并发 | 聚合读响应带 `version`（ADR-017 的列）；变更请求带 `If-Match: <version>`，不符 → `409 version_conflict` |
| 错误体 | `{"error": {"code": "...", "message": "...", "retryable": bool, "details": {…}}}`，`code` 复用契约② §错误码集合，HTTP 码与之对应 |
| 未知值 | 可空字段用 `null` + 伴随 `confidence`；**严禁**用 `0`/`false`/`"offline"` 表示"不知道" |
| 原始数据 | 凡归一化字段（`status`/`availability` 等）必须同时可取到 `provider_raw`，UI 有权展示"原始是什么" |
| 敏感 | 任何响应体不得含 secret 明文，只含 `vault://` 引用（redaction 在写入库时就已强制） |

## 3. 端点（First Sprint 需要面的最小集）

```
GET  /api/v1/health                      → {state, version, migration_version, ready}   （唯一免鉴权）
POST /api/v1/ws-ticket                   → {ticket, expires_in:30}；WS 握手前的读侧票（§1 第 2.1 条 + REQ-T-4 豁免
                                         Principal + REQ-F-11 豁免幂等缓存）。**本节原先没有这行** ⇒ LAG-1 回写时
                                         一并补：它**要** Bearer（免鉴权的只有 /health），但只写在 §1，
                                         照本表接线的客户端根本发现不了这个端点
POST /api/v1/projects                    → 建项目（canonicalize 后的 path 冲突返回 409）
GET  /api/v1/projects?cursor=&limit=     → 列表；GET/PATCH /api/v1/projects/{id}
POST /api/v1/adapters/{key}/probe        → 触发握手+能力核定；GET /api/v1/adapters[/{id}]
GET  /api/v1/adapters/{id}/capabilities  → 逐能力等级 + effective_backend + last_verified_at
GET  /api/v1/models?adapter=             → 模型列表（含 parameter_schema_json）
POST /api/v1/adapters/{id}/sessions:discover → 从 Provider 拉会话清单、按 remote_session_id 去重 upsert 本地行，
                                          返回发现的 session id 列表（幂等：重复 discover 不产生新行）
POST /api/v1/sessions/{id}:manage      → 置 managed（受管理会话才开同步与通知）
GET  /api/v1/sessions?project=&cursor=&limit=
                                       → 列表（`project` = project_id 过滤）；GET /api/v1/sessions/{id} → 详情；
                                         GET /api/v1/sessions/{id}/messages?after_seq=
POST /api/v1/sessions/{id}:sync        → 显式增量同步，返回 sync_state + next_cursor
POST /api/v1/tasks                       → 建任务（必须带 Idempotency-Key）
GET  /api/v1/tasks?status=&project=&cursor=
GET  /api/v1/tasks/{id}                  → **F-8 批准 · LAG-1 回写（2026-09-22）**：单任务详情。
                                         垂直链上 `:accept` 之前必须用它取最新 `version`（`If-Match` 的输入），
                                         没有这条读，客户端就只能拿列表里的旧版本去撞 409
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
GET  /api/v1/approvals?state=PENDING     → 待决审批；GET /api/v1/approvals/{id} → **F-8 批准 · LAG-1 回写**：单审批详情
                                         （面板要在不刷列表的前提下盯一条审批的状态变化，只有列表读就得整表重拉）
                                         ；POST /api/v1/approvals/{id}:resolve
GET  /api/v1/quota?adapter=              → 观测值 + observation_kind + confidence（UNKNOWN≠0）
GET  /api/v1/notifications?state=&cursor=; POST /api/v1/notifications/{id}:read|handle
                                       → **C26 裁定（2026-09-22 22:5x）：`state` 缺省 = 不过滤（六态全列），不是缺省 UNREAD。**
                                         理由不是"哪种更常用"，是**一个字段不许兼职两种语义**：若"不传"等于"只要 UNREAD"，
                                         面板就再也无法表达"我要全部"（除非再开一个参数，那才是真脏）。
                                         **决定性事实我核过**：`apps/desktop/src/stores/panel.ts` 的 inbox 调用**本来就显式带
                                         `state: 'UNREAD'`** ⇒ 这个默认值对面板零收益、纯留隐患，backend 那两行默认直接删。
                                         与 REQ-F-21（同步游标不许用空串兼职"没有位置"）、§12（UNKNOWN 不许显示成 0）同一条线。
                                         **`:read` 今天仍落不了库**：它要发的 `notification_read` 我刚补登记进事件契约 §6，
                                         但 `internal/eventjournal/events.go` 里还没这个常量（REQ-DB-5，归 `db`）——
                                         **在那之前 `:read` 保持 501，别发一个不登记的事件**（事件契约 §9 禁止事项 1）。
GET  /api/v1/search?q=&scope=messages,tasks   → FTS5；返回命中 + 片段 + 上下文 id
GET  /ws/v1/events?from_seq=N            → 事件流（含 resync_required、ping）
                                           # ⚠ 这个 query 参数只在"客户端根本没发 hello"时才生效；
                                           #   游标的唯一权威是 hello 帧的 from_seq —— 见 §5（REQ-F-20）
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

## 5. WS 帧与游标（与**事件契约** §2/§8 一致）

- **LAG-2 裁定（2026-09-22）：本条原先那句"连接后第一帧 `{type:"hello", from_seq}`"就是要收掉的歧义句** ——
  它没说是**谁**的第一帧，也没提服务端还有第二种首帧形状。现在按"客户端必做 / 服务端首帧"分两行写死：
  - **客户端必须先报游标，再等就绪**：连上后的**第一个客户端帧**必须是 `{type:"hello", from_seq}`。
    不发 hello 的连接有 `HelloTimeout` 倒计时，超时才回落到 `?from_seq=`（见下面 REQ-F-20）。
  - **服务端的首帧必有一个，形状由"hello 有没有带游标"决定**（两种都算就绪信号，客户端不会停在"无首帧"）：
    ① hello **带** `from_seq` → 首帧 `{type:"hello", "from_seq":N}` **回显**，N = 实际补发起点；
    客户端**只当就绪信号，不得据此改游标**。
    ② hello **省略** `from_seq` → 首帧 `{type:"hello_ack", "server_seq":head}`，起点抬到 journal 头（**不补发**），
    且客户端**必须**把 `server_seq` 记成自己的新游标。这一条同时堵死"缺省即从 0 全量重放"。
    ⇒ 面板的正确接线是：**收到 `hello_ack` 就覆盖游标，收到 `hello` 回显不要覆盖**；两者都不覆盖就等于
    把"服务端从哪儿开始"这件事丢掉，重连后会重复渲染同一段事件。
  判据分两半，别混成一条：**服务端那两形状已由 F20-1/F20-2/F20-3 钉住**（`test` 的
  `core/test/ws_cursor_contract_test.go`，F20-1 断的正是"首帧 `hello_ack{server_seq}` 且 server_seq == journal 头、
  一条都不补发"）。**客户端那一半（"收到 `hello_ack` 覆盖游标、收到 `hello` 回显不得覆盖"）此前没有任何判据**，
  它是面板行为 ⇒ 新增 **F20-4** 归 `frontend`（本轮派发，落在 `packages/` 自属面，不劳 `test` 去测别人的状态机）。
- 服务端先补发 `event_journal` 中 `seq > from_seq` 的事件，再切实时。
- **REQ-F-20 裁定（11:3x）：游标的唯一权威是 hello 帧；`?from_seq=` 只在"根本没有 hello 到达"时才生效。**
  实现是正确的、**误导的是本文原来那行 endpoint 写法**（我 11:32 逐处读码核过，两方转述都不准）：
  `initialFrom(r)` 确实解析 `?from_seq=` 并区分"没带"与"带了个 0"；但 `serve` 里只要读到一帧合法 `hello`，
  走的就是 `if h.FromSeq != nil && *h.FromSeq >= 0 { from, declared = ... } else { declared = false }` ——
  **省略 `from_seq` 的 hello 会主动把 query 带来的游标作废**，因为"省略"在本协议里是明确声明
  "我只要此后新事件"（与 §1 冻结的"缺省即全量重放 = 禁止"同源：**省略 ≠ 0，也 ≠ 沿用 URL 上的值**）。
  `HelloTimeout` 超时不到 hello 的那条路才回落到 `?from_seq=`（或起点=头）。
  **所以"只带 query、hello 不带游标 → 首帧 `hello_ack` 且不补发"是规定行为，不是缺陷**；
  面板同时发两处所以一直表现正确，但**照 §3 那行字面接线的客户端会以为自己补到了历史**——
  这才是 REQ-F-20 的真实危害，修的是文档。
  **判据（两侧都要断，别只断一边）**：① 只给 query + hello 省略游标 → **不补发**、首帧 `hello_ack{server_seq}`；
  ② 只给 query + 不发 hello（超时）→ **补发** `seq > query值`；③ hello 带游标与 query 不一致 → **以 hello 为准**。
  违反任一即口径漂移。归 `core/test`（`test` 角色，按判据表格式落，见契约① §1.1 第 14 条：只用符号不用行号）。

#### 判据表 F20（REQ-F-20 游标权威；从 `test` 的文件头注释镜像进本文，四张表住一处）

13:17:27 逐条实跑标定，**三条全 `--- PASS`**（命令一律带 `-v`：`go test -run` 空匹配也报 `ok`）。
用例名即 `core/test/ws_cursor_contract_test.go` 里的真实符号，**本文不写行号**。

| ID | 断言（两侧都要断） | 当场命令 | 状态 / 归属 |
|---|---|---|---|
| F20-1 | 只给 `?from_seq=` + `hello` 省略游标 ⇒ 首帧 `hello_ack{server_seq}`、**不补发历史** | `go test -count=1 -v -run TestHelloCursorOmittedOverridesQuery ./test/` | 现存（PASS）/ test |
| F20-2 | 只给 `?from_seq=` + 超时不发 `hello` ⇒ 回显 `hello{from_seq=query}` 并**补发** `seq > query` | `go test -count=1 -v -run TestNoHelloFallsBackToQueryCursor ./test/` | 现存（PASS）/ test |
| F20-3 | `hello` 游标与 `query` 不一致 ⇒ **以 `hello` 为准**，两个方向都判 | `go test -count=1 -v -run TestHelloCursorOverridesQuery ./test/` | 现存（PASS×2 子用例：`query_小于_hello`、`query_大于_hello`）/ test |
| F20-4 | **客户端那一半**：面板收到 `hello_ack` ⇒ 把 `server_seq` 写进自己的游标；收到 `hello` 回显 ⇒ **不得**覆盖。**两侧都要断**，且读帧的 helper 必须是"读字面上第一个返回的帧"——为躲心跳而 `continue` 的容错读法会把"首帧是 X"悄悄降级成"第 N 帧里出现过 X" | `cd apps/desktop && npx vitest run -t helloAckAdvancesCursor` | **待建** / frontend（LAG-2 本轮派发；用例名若改，必须回写本行，否则命令是假的） |

`test` 侧把文件头注释改成"判据见契约③ §5 判据表 F20"的指针，**现在可以改了**（表已在，不会再指向不存在的东西）。

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
| `core/internal/project/**` | `backend` | **REQ-P-1 裁定（15:3x，本轮新造的行）**：`projects` 是 ADR-017 十张聚合表之一，但此前 §7 从没给它 owner —— 于是 `POST /projects` 只能挂 501。**归 `backend`**：它的写出口只有 transport，且没有任何现存角色共享它的判断逻辑；为"只装一行 CRUD"另开一个别人的包，等于亲手造一条跨角色接缝。**配套禁令**：`project.Service` 只做聚合 CRUD 与版本推进，**不得**在里面调 adapter/process（依赖方向 §0）；需要探针的走 §7 已有的 `adapter_instances` 那条线。 |
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

### 7.1 写路径归属判据表 W13（REQ-WS-3 裁定，16:2x；`test` 的 lint 按行解析）

**背景**：T-41③ 把 lint 扫描面从 `core/internal` 扩到 `core/` 全部 `.go` 之后，立刻冒出 4 条命中，
`test` 如实报为 REQ-WS-3 等我说清。**逐条核下来结论是：3 条合法、1 条是判据自己的假阳性。**
假阳性那条是 `c.Tasks.Create(...)` —— 它是 **Service 方法**，被模式 `.Create(` 误当成"往表里写"。
**教训：扩面之后必须重看每一条命中的接收者，不然新能力第一件产物就是错案。**

判据（**只匹配"以字面量表名为首参"的调用**，即 `\.Append\("|\.UpdateOnce\("|\.Touch\("|\.Create\("`，
引号紧跟左括号）—— 这条把 Service 方法整个排除掉，因为 Service 调用不传表名字面量）：

| ID | 判据 | 当场命令 | 现状（16:21:05 实测） |
|---|---|---|---|
| W13-1 | transport/装配层允许写的表**只有** `idempotency_keys`（Append/UpdateOnce）与 `principals`（Append，引导期一次写） | `grep -rn --include=*.go -E '\.(Append\|UpdateOnce\|Touch\|Create)\("' core/api core/cmd \| grep -v _test.go` | 命中 3 处，全部在白名单内 → **绿** |
| W13-2 | 上面 3 处之外，`core/api/**` 或 `core/cmd/core/**` 出现任何**聚合表**写调用即越界 | 同 W13-1 的命中集，逐个比对 ADR-017 那 10 张 + 000003 那 3 张表名 | 命中 0 → **绿** |
| W13-3 | 判据**不得**把 Service 方法当写表：模式必须要求表名字面量紧跟左括号 | 对 `c.Tasks.Create(` 这类调用跑 W13-1 的模式，必须 0 命中 | 已按此收紧；**旧模式的 4 命中里 1 条即此假阳性** |

**为什么这三处合法、不再要求下沉**：`idempotency_keys` 在内部契约 §1.1 第 12 条已定性为
"**纯基础设施状态，不是域事实**"，`principals` 是"**引导期一次写入、之后不可变**"（同条我刚补的裁定）——
两者都**不发事件、不推版本**，把它们塞进某个 Service 只会造出一个"只有一行逻辑的 Service"，
那是为分层而分层。**但**：如果将来有人往这两张表之外的表在这里写，W13-2 直接红，不许以"同属基础设施"为由口头豁免——
**要豁免就改本表并说明理由**。

### 7.2 REQ-AD-1 裁定（2026-09-22）：`POST /api/v1/adapters/{key}/probe` 的两段归属

**先把事实摆出来（全仓、排除测试）**：
`grep -rn "Probe\|probe" core/ --include=*.go | grep -v _test.go` → **0 命中**。今天没有任何 probe 实现。
`internal/adapter` 的导出面全是协议元语（`NewHandshake`/`Negotiate`/`DecodeStream`/`CarriesTurnRef`…），
`internal/process` 的包注释明写它**不写库、不发事件**（DB 写入属于持有句柄的人，`Recorder` 就是为此存在的注入接缝）。
⇒ backend 维持 501、不写假 200，是对的。yaml 那侧其实早已钉死形状（`probeAdapter`：`Idempotency-Key` + `Principal` 必带、
路径参数是 `adapter_key` 而非实例 id、200 体是 `AdapterInstance`、产 `adapter_registered`/`adapter_health_changed`），
**缺的从来不是"要什么"，是"谁能被调"**。

**裁定（两段，同轮派发）**：

1. **入口归 `adapter`**，落 `core/internal/adapter/`（新 `probe.go` + `probe_store_sqlite.go`）。形状：
   `func (r *Registry) Probe(ctx context.Context, adapterKey string) (*ProbeResult, error)`，
   `ProbeResult` 至少带 `adapter_instance_id`、`software_version`、`capabilities[]`、`verified_at`、`health_state`，
   并且**必须能投影成 yaml 的 `AdapterInstance`**（handler 里不许补字段）。
   **为什么落 `internal/adapter` 而不是 `internal/process`**：process 按设计不持 DB 句柄，让它写 `adapter_instances`
   等于推翻它自己的包注释；而"能力核定要落库 + 发事件"是**域服务**，与 `internal/approval/store_sqlite.go` 同形
   （域包自带 SQLite store，写仍只走 `platform/db`）。`internal/adapter` 因此从"纯协议"变成"协议 + 注册域"，
   §7 归属行不变（同一 owner、同一条路径），不改表。
2. **handler 与接线归 `backend`**：`api/http` 只做薄投影（取 key → 调 `Registry.Probe` → 出 `AdapterInstance`），
   `cmd/core` 只放构造与注入。**W13 的白名单不为本条放宽**——这次不需要豁免：写就在域包里，
   `api/http`/`cmd/core` 出现 `adapter_instances`/`adapter_capabilities` 的表名字面量写调用即 W13-2 红。

**v0.1 边界（防止把 probe 顺手做成"自动恢复"）**：一次尝试、不重试；超时与健康度沿用 `internal/process` 里 `Supervisor`
已有的 backoff / 健康度口径（不新造一套）；失败 ⇒ 只记 `health_state` 变化并回 §15.2 对应码，**不伪造 capability 行**。
事件只用事件契约已登记的那三个（`adapter_registered`/`adapter_health_changed`/`adapter_capability_verified`），
**禁止新增 `event_type`**（事件契约 §9 禁止事项 1）。

> **REQ-T12 没落地这件事改变了 probe 的接法（2026-09-22 实测，全仓、排除测试）**：
> `grep -rn "Supervisor{\|NewSupervisor" core/ --include=*.go` 与非测试的 `func .*RecordStart` 都**只命中
> `internal/process` 自己那两行**（`Recorder` 的声明与 `Supervisor` 内部那次调用）——今天**没有任何非测试代码构造过
> `Supervisor`，也没有 `Recorder` 的任何实现**。所以 **`Registry.Probe` 不得依赖 `internal/process`**
> （那等于把 probe 排在 REQ-T12 后面，还平白多出一条跨角色 import）：改为吃一个**注入的窄接口**，
> 形状自定、语义固定为"把这个 key 的 Adapter 拉起来、交回一对管道"（例如
> `Start(ctx context.Context, adapterKey string) (io.ReadWriteCloser, error)`），握手用本包已有的
> `NewHandshake`/`Negotiate`。**把真 `Supervisor` 递进来的那一步归 `backend` 的 `cmd/core`**，与 REQ-T12 同一条腿。

> **`Probe` 必须自带期限（2026-09-22 22:4x 由实测钉出来，不是假想）**：两条 probe 用例在 `context.Background()` 下
> **永久挂死**（现场栈：`Registry.Probe → exchange → Writer.Write → io.(*pipe).write` 停在 `select`），
> 而第三条给了期限就能中断、却把原因报成 `write initialize: io: read/write on closed pipe` 而不是"到期限"。⇒ 两条硬要求：
> ① **ctx 没带 deadline 时 `Probe` 自己加一个默认期限**，默认值必须是导出常量或包内具名常量（不许只存在于测试里）——
> 调用方是 `api/http` 的 handler 线程，漏传期限就等于把一个卡住的 Adapter 变成一条永久挂住的 HTTP 请求；
> ② **失败原因分层**：到期限 ⇒ 消息里必须出现"期限"这个原因，**不许拿底层管道错误冒充**
> （`ADAPTER_UNRESPONSIVE` 的生产方是 Core 自己，归因写错了事后从 `details_json` 里翻不回来）。


## 8. 明确不做（v0.1）

认证体系与多用户、CORS 开放、`/api/v1` 之外的公开端点、GraphQL、长轮询替代 WS、
在 HTTP 层做任何业务状态裁决（裁决只在 Go Core）。

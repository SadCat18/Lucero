> 本文由同目录 `Lucero_Qoder_Handoff_v0.1.docx` 逐段转换而来（`Heading1/2` 到 `#`/`##`，`CodeBlock` 到围栏，`Callout` 到引用，表格到管道表）。

# Lucero

Qoder 项目交接主文档 · v0.1

Windows Local-First AI Control Plane

| 项目项 | 冻结结论 |
|---|---|
| 产品形态 | Windows 桌面应用，单用户，本地优先 |
| 核心目标 | 统一管理 Codex、Qoder、OpenCode、Z Code、WorkBuddy 等 AI 编程软件与模型执行 |
| 核心后端 | Go Core Daemon |
| 桌面端 | Tauri 2 + Vue 3 + TypeScript + Vite |
| 数据库 | SQLite + WAL + FTS5 |
| 原则 | 稳定性 > 核心功能完整度 > 使用体验 > 扩展功能 |
| 开发策略 | 分阶段开发，但从第一天按最终完整架构设计 |
| 文档用途 | 交给 Qoder 作为项目唯一开工基线；未更新 ADR 前不得自行推翻核心架构 |

> 文档状态：需求冻结 v0.1 + 系统架构基线 v0.1 + Core Domain/Schema v0.1

注意：Codex 内部特有的 SERVER-TEST / MINIPROGRAM-FRONTEND / ADMIN-FRONTEND / BACKEND 四角色模式不属于本项目统一工作台需求。

# 0. Qoder 开工入口

> Qoder 拿到本项目后，请按本章顺序执行。不要从 UI 开始堆页面，也不要先接真实厂商。

## 0.1 必须先接受的架构事实

- Task 属于统一工作台；Codex/Qoder/OpenCode 等只是 Execution Provider。
- Session 属于真实 Provider；跨软件接力时 Task 不变，Execution/Session 可以变化。
- Go Core 是唯一业务真相源；Tauri/Vue 不能维护独立业务状态机。
- Core 与 Desktop 分进程；UI 崩溃或关闭不能结束任务。
- 所有关键状态变化必须在 SQLite Transaction 中和 Durable Event Journal 一起提交。
- 所有高风险操作必须经过 Permission Engine；高风险行为进入 Audit。
- Provider 状态无法验证时必须显示 UNKNOWN，不能猜测为 0、Offline 或 Failed。
- Adapter、Browser Worker、Windows Bridge 都是隔离进程，任何一个崩溃都不能带崩 Core。

## 0.2 第一条开发主线

```text
    FakeAdapter
      ↓
    Discover Existing Session
      ↓
    Create Task
      ↓
    Create Execution
      ↓
    Send Message
      ↓
    Persist Stream
      ↓
    Approval Request / Resolve
      ↓
    Execution SUCCEEDED
      ↓
    Task AGENT_COMPLETED
      ↓
    User Acceptance
      ↓
    Task ACCEPTED
```

> 先把这条链做到可崩溃恢复、可审计、可重复测试，再接第一家真实 AI 软件。

# 1. 产品定位与目标

## 1.1 产品定位

本项目不是“把几个 AI 软件窗口放一起”，而是一个本地优先、面向个人开发者的多 Agent 控制平面。Codex、Qoder、OpenCode、Z Code、WorkBuddy、本地模型和自建模型都被视为 Execution Provider。

## 1.2 用户与平台

- 仅供单用户本人使用；不做团队、多用户、访客或角色体系。
- Windows 桌面软件为主；后台 Core 长期运行。
- 远程端后续通过响应式 Web/PWA 提供，架构保留原生移动 App 扩展。
- 启动默认进入 Dashboard。

## 1.3 最终用户体验

用户应在一个面板里完成：选择软件/模型、调整参数、发任务、查看流式回复、处理审批、查看精确额度、监控运行状态、多任务并发、跨软件转交、已有会话协作、Git/终端/测试/部署/服务器/浏览器操作。

## 1.4 核心连续性原则

> 厂商可以切换、模型可以切换、Session 可以转交，但 Task、项目状态、审计、历史、权限和结果必须连续。Provider Failure 不得导致 Task 丢失。

# 2. 冻结需求总览

## 2.1 软件、会话与双向同步

- 自动扫描各软件已有会话；重要会话可标记为“受管理会话”。
- 目标是原软件与统一面板双向同步；具体可实现深度由 Adapter Capability 决定。
- 默认一个会话固定绑定一个软件；需要时可完整转交到其他软件。
- 转交必须支持指定目标软件中的已有会话，而不只是新建临时会话。
- 不同软件已有会话支持手动转发、主动请求协作、临时协作组。
- 跨会话通信权限可按每个会话单独设置，并完整追踪消息链路。
- 支持从某条历史消息创建分支，并跨软件创建独立分支。
- 跨软件分支的代码工作区必须隔离，优先 Git Worktree。
- 会话健康度分析：上下文拥挤、偏题、重复、建议换新；支持一键无损换新会话。

## 2.2 模型与额度

- 选择软件后尽量完整映射其原生模型、推理强度、上下文、权限、Agent/Tool 等参数。
- 额度尽可能精确：原始单位、使用/剩余百分比、时间窗口、Reset、任务消耗。
- 提供统一 Availability Index 便于相对比较，但不得假装不同厂商额度完全等价。
- 支持额度趋势、耗尽预测、任务级消耗预测和多方案模拟；预测值必须与事实数据分离。
- 支持手动指定、推荐后确认、自动选择软件/模型三种调度模式。
- 推荐必须可解释，并说明为什么没有选择其他候选。
- 面板可学习全局/项目偏好，但只能提出规则建议，经用户确认后才写入。
- 额度不足时自动生成交接包；普通任务可按规则自动转交，重要任务可等待确认。
- 额度恢复后智能判断是否值得切回，而不是机械切换。

## 2.3 任务、执行与调度

- 支持项目任务和普通任务。
- 支持多个任务同时运行。
- 完整调度：优先级、队列、拖拽、暂停/恢复、定时开始、任务依赖、Provider 最大并发。
- 任务预算：运行时间、Token/额度、重试、调用次数、并发、CPU/内存、命令时长；可配置超预算策略。
- Task 完成状态区分 AI 已完成、自动验证通过、等待验收、用户已验收。
- 每个任务结束生成结构化结果卡，并可作为后续任务和跨软件转交输入。
- 支持咨询、只读分析、Review、执行修改、测试验证、部署、故障诊断等任务模式。
- 任务模式可自定义并继承。
- 所有执行型任务前做风险评估，高风险时建议或自动准备 Worktree、备份、快照、权限限制、Review 节点等防护。

## 2.4 审批、权限和安全

- 统一状态中心：运行中、等待审批、等待输入、阻塞、失败、完成、暂停、取消。
- 支持手动审批与自动审批规则。
- 常见权限至少统一映射为“允许访问 / 完全访问”等语义，并保留 Provider 原始含义。
- 临时授权支持单次、当前任务、限时、目录、命令、软件、模型等作用域，到期自动回收。
- 跨软件/跨项目敏感信息发送前检查并支持自动脱敏。
- 本地加密保险库使用 Windows 系统级保护；AI 默认不能看到明文 Secret。
- 一键紧急停止 + 分软件/项目/工作流停止 + 只读模式 + 禁止远程 + 关闭自动审批。
- 高风险远程操作可要求 PIN/Windows Hello/TOTP 二次确认。
- 重要审计日志采用防篡改完整性链并支持可验证、脱敏导出。

## 2.5 项目与开发工作台

- 自动发现项目、从各 AI 已有会话反推项目，并合并同一目录的多入口。
- 项目下会话支持手动分组、自动用途建议、多标签。
- 深度集成文件树、Git Diff、Git 状态、分支、终端和命令输出；不内置代码编辑器。
- 并行 AI 修改同一项目时自动选择 Git Worktree/临时工作副本隔离。
- 统一变更审查中心：按任务/软件/项目查看 Diff、部分暂存、接受/丢弃、Review、冲突预警。
- Git 远程仓库支持 PR/MR、Review、Issue、CI，并与 AI 会话联动。
- 终端支持 PowerShell/CMD/Git Bash/WSL、任务级独立终端、远程 SSH。
- 服务器辅助管理：资源、文件、进程、Docker、日志、端口、服务启停。
- 本地/远程项目映射，Git 优先同步，部署前检查未提交改动/版本/冲突/备份。
- 部署中心支持流程模板、健康检查和失败回滚；部署审批按步骤和项目配置。
- 数据库中心支持查询、执行计划、DDL/DML，并保护 DROP/TRUNCATE/无 WHERE UPDATE/DELETE 等高风险操作。
- 测试中心支持单元/接口/前端/E2E/构建/冒烟，失败日志可发给指定已有 AI 会话修复。
- 浏览器自动化支持完整页面操作、Console/Network、项目级/任务级隔离、人工接管与高风险暂停。

## 2.6 知识、搜索、附件和历史

- 全局知识库 + 项目知识库 + 会话级精确访问控制。
- README/docs/指定配置可自动同步；AI 提取的长期知识默认需确认。
- 统一附件中心，支持格式转换/文本提取/压缩/切片和项目级附件库。
- 全文搜索 + 高级筛选 + 语义搜索。
- 完整操作日志，支持按软件/项目/会话/时间/模型/状态/关键词搜索并导出 Markdown/JSON/PDF。
- 任务级和项目级交互时间线；可查看任意时间点当时的 Diff、终端、模型、权限、额度和快照。
- 支持两个时间点全状态对比，并由面板 AI 总结期间变化。
- 支持任务复盘和项目阶段复盘，给出改进建议但不自动改规则。
- 历史数据从第一版就按冷热分层设计。

## 2.7 工作流与面板 AI

- 支持任务预设和跨软件工作流模板。
- 工作流只手动启动；运行时可自动继续、每步确认或关键节点确认。
- 自然语言可生成工作流并自动补软件/模型/权限/输入输出/测试/确认/失败分支；也可反向解释工作流。
- 可视化 DAG 支持并行、汇合、审批和实时节点状态/耗时/额度。
- 面板自身有完整 AI 助手，可调用平台 API，但严格服从权限、审批、敏感信息和审计。
- 面板 AI 模型也走统一模型池/策略。
- 面板 AI 记忆分全局/项目/会话/单次任务四层，行为沉淀默认需用户确认。
- 支持复杂自然语言多步指令；计划发生关键变化时必须再次确认。
- 支持语音控制低风险面板操作；高风险操作不能仅通过语音批准。

## 2.8 模型网关、MCP、插件与扩展

- 本地/局域网/自建模型作为正式 Execution Provider：Ollama、LM Studio、OpenAI-compatible、自托管服务。
- 工作台成为统一模型网关，对外支持 OpenAI/Anthropic/Gemini/Ollama/自定义 HTTP 兼容接口。
- 支持 virtual model alias、智能路由和故障切换。
- 统一抽象 Tool Calling、Structured Output、视觉、文件、音频、Embedding、Rerank、Streaming、Long Context、Cache、Batch、Computer Use、Code Interpreter 等能力。
- 统一 MCP/Tool Gateway，聚合第三方 MCP，并按软件/项目/会话/设备/API Token 做权限隔离。
- 统一管理各软件 MCP/Skill/Rule/Hook，并做跨软件能力映射和双向同步。
- 适配器/插件系统可管理版本、健康、权限、日志和升级；插件安装前声明权限。
- 核心第三方组件成熟开源优先，许可证审查，所有核心组件通过抽象层可替换。

## 2.9 远程、通知和 UI

- 局域网与外网远程查看/操作；支持 Tailscale/ZeroTier 类型组网和自建公网方案。
- 远程端第一阶段使用响应式 Web/PWA，未来保留原生 App。
- 单用户账号密码登录；首次成功登录自动登记设备身份，设备可独立配置权限。
- Windows 系统通知 + 自定义声音 + 重复提醒 + 静音时段 + 优先级 + 聚合。
- 统一通知收件箱，通知本身可直接执行批准/拒绝/跳转/回滚/发给 AI 分析等动作。
- Dashboard、会话模式、开发工作台三种布局；Dashboard 可拖拽、缩放、隐藏、保存多套布局。
- 完整多窗口和多显示器布局记忆。
- 系统托盘提供状态、快速操作和低风险快速审批。
- 中英文、浅色/深色/跟随系统、缩放、字体、密度和强调色自定义。
- 全局快捷键、资源管理器右键、拖拽、Command Palette、自定义命令和参数。

## 2.10 稳定、恢复、数据与网络

- Core 后台分层运行；浏览器、AI 分析、沙箱、重日志处理按需启动。
- 完整 Crash Recovery：恢复队列、工作流位置、审批、跨会话关系并防止重复执行。
- 任务失败自动分类并安全恢复；失败后可生成脱敏故障诊断包。
- 项目级/全局工作现场快照，支持比较和部分恢复。
- 分类/增量备份、多恢复点、选择性恢复和冲突预览。
- 程序与数据目录分离；日志/附件/备份/Worktree/模型缓存可分盘，且项目可单独指定。
- 统一网络代理策略：System Proxy/HTTP(S)/SOCKS5、按软件/Provider/项目设置、多线路故障切换和安全恢复。
- 完整降级模式：在线 Provider 故障可转其他在线或本地模型；Adapter 可降级 CLI/UI Automation。
- 运行时/依赖版本隔离和兼容性管理；原软件本身只做版本检测与更新提醒。

# 3. 系统架构 v0.1

## 3.1 总体架构

```text
    ┌──────────────────────────────────────────┐
    │ Windows Desktop                          │
    │ Tauri 2 + Vue 3 + TypeScript + Vite      │
    └─────────────────┬────────────────────────┘
                      │ REST / WebSocket
                      ▼
    ┌──────────────────────────────────────────┐
    │ Go Core Daemon                           │
    │ Task / Execution / Session / Adapter     │
    │ Permission / Approval / Quota / Routing  │
    │ Event / Audit / Recovery / API           │
    └───────┬───────────────┬──────────────────┘
            │               │
            ▼               ▼
     AI Adapters        On-demand Workers
     Codex/Qoder/...    Node+Playwright
                        C# Windows Bridge
```

## 3.2 Core 为什么独立进程

- UI 崩溃/升级/关闭不影响正在运行的任务。
- 远程 PWA、CLI、插件、面板 AI 都能复用同一个 Core API。
- 后续 Crash Recovery、Task Lease、Idempotency 更容易建立。

## 3.3 Go Core 推荐目录

```text
    core/
    ├─ cmd/core/
    ├─ internal/
    │  ├─ platform/
    │  ├─ project/
    │  ├─ adapter/
    │  ├─ model/
    │  ├─ session/
    │  ├─ task/
    │  ├─ execution/
    │  ├─ approval/
    │  ├─ permission/
    │  ├─ quota/
    │  ├─ handoff/
    │  ├─ notification/
    │  ├─ audit/
    │  ├─ eventjournal/
    │  ├─ blob/
    │  ├─ process/
    │  ├─ recovery/
    │  └─ settings/
    ├─ api/http/
    ├─ api/websocket/
    └─ migrations/
```

## 3.4 模块通信约束

- 按业务模块垂直切分；不要使用全局 models/services/repositories 大杂烩。
- 模块间只通过 Public Service API 或 Domain Event 通信。
- Repository 属于模块；禁止 GlobalDatabaseRepository。
- 禁止散落裸 goroutine；通过 Supervisor/WorkerPool/Scheduler 启动长任务。
- 任何外部进程必须归属 Process Supervisor。

## 3.5 Durable Event Journal

```text
    SQLite Transaction
    ├─ Update Aggregate
    └─ Insert Event Journal
            ↓ COMMIT
    In-memory Event Bus
    ├─ WebSocket
    ├─ Notification
    ├─ Audit enrichment
    └─ Realtime UI
```

> 任何模块不得先推送 WebSocket 再写数据库。

# 4. Core Domain Model v0.1

## 4.1 四个最关键概念

| 实体 | 定义 | 关键原则 |
|---|---|---|
| Task | 用户真正想完成的目标 | 不绑定厂商；跨软件转交时 Task 不变 |
| Execution | 某 Provider/Model 对 Task 的一次真实执行 | 一次失败可创建下一次 Execution |
| Session | Provider 中真实持续存在的上下文 | 属于对应软件，可映射为 Unified Mirror |
| Event | 系统状态变化的持久化事实 | 先落库，再发布；支持恢复和时间线 |

## 4.2 Task / Execution 关系

```text
    Task #100
    ├─ Execution #1  Codex   -> QUOTA_EXHAUSTED
    ├─ Execution #2  Qoder   -> SUCCEEDED
    └─ Execution #3  OpenCode -> REVIEW
```

## 4.3 Task 状态机

```text
    CREATED -> QUEUED -> PREPARING -> RUNNING
                     ├-> WAITING_APPROVAL
                     ├-> WAITING_INPUT
                     ├-> BLOCKED
                     ├-> PAUSED
                     └-> TRANSFERRING
    RUNNING -> AGENT_COMPLETED -> VERIFYING
            -> WAITING_ACCEPTANCE -> ACCEPTED
    异常：FAILED / CANCELLED / INTERRUPTED / UNKNOWN
```

## 4.4 Execution 状态机

```text
    CREATED -> STARTING -> RUNNING
                        ├-> WAITING_APPROVAL
                        ├-> WAITING_INPUT
                        ├-> PAUSED
                        └-> TRANSFERRING
    RUNNING -> SUCCEEDED / FAILED / CANCELLED / INTERRUPTED / UNKNOWN
```

## 4.5 Session

- 保存 Adapter ID、Remote Session ID、Project、Managed Flag、Sync Cursor、Remote Revision、Provider Raw Status。
- 受管理会话才持续同步、监控、通知和跨会话协作。
- 会话冲突能安全合并则合并，不能则进入人工确认。

## 4.6 Handoff

- 跨软件转交必须是一级实体，不是一次复制 Prompt。
- 交接包包含任务目标、历史、Diff、命令、测试、模型/权限配置、附件、环境、失败记录、协作记录。

## 4.7 Approval 与 Temporary Grant

- 所有需要用户决策的危险操作统一转成 Approval。
- “当前任务允许完全访问”等操作生成 TemporaryGrant，带时效和作用域。

## 4.8 Quota

- Quota Account 不只绑定软件；允许不同软件共享同一 Provider 额度。
- Quota Snapshot 保存 Provider raw value、normalized view、时间窗口、Reset、Source、Confidence。
- Observed 与 Estimated 必须分开。

# 5. SQLite Schema v0.1

## 5.1 Phase 1 冻结表

| 领域 | 表 |
|---|---|
| 身份/项目 | principals, trust_domains, projects, project_paths |
| Adapter/Model | adapter_instances, adapter_capabilities, models |
| 会话 | sessions, messages |
| 任务 | tasks, task_dependencies, task_session_links |
| 执行 | executions, execution_leases, execution_output_chunks, handoffs |
| 权限 | approvals, temporary_grants, policy_rules |
| 额度 | quota_accounts, quota_snapshots |
| 文件 | blobs, attachments, message_attachments, task_attachments, project_attachments |
| 进程/通知 | process_instances, notifications |
| 可靠性 | event_journal, audit_entries, idempotency_keys, settings |

## 5.2 SQLite 运行参数

```text
    PRAGMA foreign_keys = ON;
    PRAGMA journal_mode = WAL;
    PRAGMA synchronous = FULL;
    PRAGMA busy_timeout = 5000;
```

## 5.3 数据存储边界

- SQLite 保存结构化数据和索引。
- 大日志、截图、附件、快照、Browser Trace 等进入 Blob Store/文件系统。
- Blob 使用 SHA-256 校验并区分 HOT/COLD/CACHE。
- 全文检索 v0.1 使用 SQLite FTS5。
- Semantic Vector Store 仅冻结接口，不锁具体引擎。

## 5.4 关键可靠性机制

- Execution Lease：防止多个 Runner 重复执行同一 Execution。
- Idempotency Key：防止 UI 重试、网络重连、Core 恢复造成高风险动作重复。
- Aggregate Version：防止同一 Aggregate 版本的 Event 重复写入。
- Correlation/Causation ID：建立完整任务时间线。
- Stream Chunk：流式回复按批次持久化，而不是每 token 写 Event。

# 6. Adapter Platform v0.1

## 6.1 Adapter 进程隔离

真实 Provider SDK/CLI 不直接编进 Go Core。Adapter 独立进程，通过版本化 JSON-RPC 与 Core 通信。

## 6.2 统一 Capability Registry

| Capability | 用途 |
|---|---|
| session.list/read/send/stop | 会话发现、读取、发消息、停止 |
| model.list/select/reasoning | 模型与参数控制 |
| quota.read/reset | 额度和重置时间 |
| approval.read/respond | 审批接收与回应 |
| client.launch/focus | 启动/定位原客户端 |
| config.read/write | Provider 配置映射 |

## 6.3 能力等级

NATIVE / COMPATIBLE / CLI / LOCAL_DATA / UI_AUTOMATION / READ_ONLY / EXPERIMENTAL / UNSUPPORTED

## 6.4 Adapter Fallback

```text
    Official API / SDK / App Server
                ↓
               CLI
                ↓
           Local Data
                ↓
          UI Automation
```

## 6.5 Phase 0

> 先完成五家 Adapter Capability Survey，再决定真实接入顺序。不得因为品牌偏好预先固定。

# 7. Permission / Audit / Recovery

## 7.1 Permission Engine

```text
    Input:
    Actor + Origin + Device + Project + Task + Session
    + Software + Model + Action + Resource + Risk
    Output:
    ALLOW / DENY / REQUIRE_APPROVAL / ALLOW_TEMPORARY
```

## 7.2 决策优先级

```text
    Emergency Stop
        ↓
    Explicit Deny
        ↓
    Trust Domain Policy
        ↓
    Project Policy
        ↓
    Task Mode Policy
        ↓
    Session Override
        ↓
    Temporary Grant
        ↓
    Default
```

## 7.3 Secret

- Secret 不写普通日志、Event Payload、Audit Details 明文。
- 使用 Redaction Layer；数据库只保存 vault://... 引用。
- Windows DPAPI/Credential abstraction 负责本地保护。

## 7.4 Audit

- 普通 Logs 可以按保留策略清理；高风险 Audit 长期保留。
- Audit 记录 approval、command、secret access、DB write、deploy、remote、API、plugin、cross-session。
- Hash chain + checkpoint signature，目标是 tamper-evident。

## 7.5 Crash Recovery

- Execution 维护 heartbeat/lease。
- Core 启动扫描 STARTING/RUNNING/WAITING_APPROVAL/WAITING_INPUT/TRANSFERRING。
- 结合 Adapter health、process、provider_execution_id 判断 reconnect/resume/interrupted。
- 不可恢复时生成恢复点和建议，不得重复执行高风险操作。

# 8. 技术栈冻结

| 层 | 技术 | 说明 |
|---|---|---|
| Desktop | Tauri 2 + Vue 3 + TypeScript + Vite | Windows 主界面、Tray、快捷键、通知 |
| Core | Go | 唯一业务真相源；模块化单体 |
| Database | SQLite + WAL + FTS5 | Local First；稳定优先 |
| API | REST + WebSocket | Desktop/PWA/CLI/Plugin 统一使用 |
| Adapter | JSON-RPC | 独立进程，语言无关 |
| Browser | Node.js + Playwright | 按需 Worker |
| Windows Bridge | C#/.NET | 仅 UI Automation/窗口控制需要时启用 |
| Terminal | xterm.js + ConPTY | 终端展示与 Windows PTY |
| Git | System Git CLI | 复用本机 Credential/SSH/完整 Git 能力 |
| Secret | Windows DPAPI abstraction | 本地安全存储 |
| Vector | Interface only | 先不锁实现 |

## 8.1 第一阶段明确不使用

Spring Boot、Electron、Redis、Kafka、RabbitMQ、MySQL Server、PostgreSQL Server、Kubernetes、Dockerized Core。它们不是不好，而是不符合当前单用户 Windows Local-First 控制平面的实际需求。

# 9. Monorepo 与代码规则

## 9.1 推荐仓库

```text
    lucero/
    ├─ apps/
    │  ├─ desktop/
    │  └─ web/
    ├─ core/
    │  ├─ cmd/
    │  ├─ internal/
    │  ├─ api/
    │  └─ migrations/
    ├─ adapters/
    │  ├─ fake/
    │  ├─ codex/
    │  ├─ qoder/
    │  ├─ opencode/
    │  ├─ zcode/
    │  └─ workbuddy/
    ├─ workers/
    │  ├─ browser/
    │  └─ windows-bridge/
    ├─ packages/
    │  ├─ ui/
    │  ├─ api-client/
    │  ├─ schemas/
    │  └─ protocol/
    ├─ specs/
    │  ├─ openapi/
    │  ├─ adapter-protocol/
    │  └─ events/
    ├─ docs/
    │  ├─ architecture/
    │  ├─ adr/
    │  └─ adapters/
    └─ scripts/
```

## 9.2 AI 编码强制规则

1. 不绕过 PermissionEngine。
1. 不在 UI 写业务状态机。
1. 不直接跨模块访问其他模块 Repository。
1. 不直接写 SQLite 绕过 Repository/Transaction。
1. 不裸启动无限 goroutine。
1. 不把 Secret 写日志/Event/Audit 明文。
1. 不绕过 Event Journal 修改关键 Aggregate 状态。
1. 不删除或伪造 Audit。
1. Adapter 不依赖 Core 私有结构。
1. 新功能先判断所属 Module，并优先补 Contract Test。

## 9.3 ADR

- ADR-001 Go Core
- ADR-002 Tauri 2 + Vue 3 + TypeScript
- ADR-003 Core/Desktop 分进程
- ADR-004 Modular Monolith, no microservices
- ADR-005 Adapter Process Isolation
- ADR-006 Versioned Adapter Protocol
- ADR-007 SQLite Local First
- ADR-008 Durable Event Journal
- ADR-009 REST + WebSocket
- ADR-010 On-demand Heavy Workers
- ADR-011 Central Permission Engine
- ADR-012 Sensitive Actions Audit
- ADR-013 Task decoupled from Provider
- ADR-014 Task / Execution separation
- ADR-015 Provider Failure must not lose Task

# 10. 分阶段版本规划

| 阶段 | 定位 | 主要交付 |
|---|---|---|
| Phase 0 | Adapter 能力调查 | 五家 Capability Matrix、Fallback 链、真实 Adapter 优先级 |
| Phase 1 | MVP 核心骨架 | Core、SQLite、Event、Adapter SDK、FakeAdapter、Session/Task/Approval/Quota/Dashboard |
| Phase 2 | 跨软件工作台 | 转交、跨会话通信、Git/Worktree、终端、结果卡、搜索、时间线 |
| Phase 3 | Developer Control Center | 测试、浏览器、SSH、Docker、数据库、部署、监控 |
| Phase 4 | Unified AI Platform | 本地模型、Model Gateway、MCP Gateway、插件、Sandbox |
| Phase 5 | Intelligence Layer | 面板 AI、自然语言控制、记忆、语义搜索、智能推荐 |
| Phase 6 | Remote & Polish | PWA、远程权限、多窗口、多屏、语音、备份、冷热数据、性能 |

## 10.1 Phase 1 验收

> 在不打开原软件主界面的情况下，从统一面板选择一个真实已有会话、选择模型、发送任务、看到实时回复、处理审批、查看任务状态和额度；Core/UI 重启不得丢任务。

## 10.2 Phase 1 不能偷懒的底层

- Adapter SDK
- Event Journal
- Permission Engine
- Audit Model
- Unified IDs
- Capability Registry
- Storage Abstraction
- API-first Core

# 11. Qoder 第一 Sprint

## 11.1 Sprint 目标

> 不要接真实 Codex/Qoder。先证明 Core 的可靠性和边界是正确的。

1. 建立 Monorepo 与 docs/adr。

2. 搭 Go Core Bootstrap：config、single instance、SQLite、migration runner、REST、WebSocket、structured logging、graceful shutdown。

3. 接入 000001_core_schema.sql。

4. 实现 Transactional Event Journal。

5. 实现 Adapter Protocol v0.1 和 FakeAdapter。

6. 实现 Project/Session/Task/Execution/Approval/Quota/Notification 最小 Service + Repository。

7. 打通第一条完整 FakeAdapter 垂直链。

8. 写 Adapter Contract Tests 和 Task State Machine Tests。

9. 执行 Kill Core / Kill Adapter Crash Test。

10. 输出第一份 ADR 与 Sprint 验收报告。

## 11.2 第一条垂直链

```text
    Create Project
    → Discover Fake Session
    → Mark Managed
    → Create Task
    → Create Execution
    → Send Message
    → Persist Stream Chunks
    → Approval Requested
    → Approval Resolved
    → Execution SUCCEEDED
    → Task AGENT_COMPLETED
    → User Accept
    → Task ACCEPTED
```

## 11.3 Sprint 暂不做

真实 Provider Adapter、高级 UI、MCP Gateway 完整版、Model Gateway 完整版、Playwright、SSH、部署、PWA、Plugin Marketplace、Panel AI。

# 12. P0 稳定性红线

- 丢 Task 或 Execution 历史。
- Core 重启导致高风险动作重复执行。
- 消息发送到错误 Provider Session。
- A 项目数据进入 B 项目。
- Approval 应用于错误 Task/Execution。
- Adapter Crash 导致 Go Core Crash。
- Core Crash 损坏 SQLite。
- Secret 写进普通日志、Event 或 Audit 明文。
- 自动转交后重要上下文、Diff、测试或权限信息丢失。
- UI 显示的状态与 Core 实际状态不一致。
- 无法确认状态时却显示确定的 Running/Failed/0 quota。

> 只要出现上述任何一项，优先修复，暂停非核心功能扩展。

# 13. Phase 0 Adapter 能力调查模板

| 能力 | 需要调查的内容 |
|---|---|
| 认证 | 能否完全复用原软件已有登录；是否改变计费/订阅来源 |
| 已有会话 | 列出、读取、增量、双向同步、指定会话发消息 |
| 模型 | 列表、切换、Reasoning/Thinking/其他参数 |
| 额度 | 精确额度、Reset、窗口、当前任务消耗 |
| 任务状态 | Running/Waiting Approval/Blocked/Done 等实时事件 |
| 审批 | 读取、响应、完全访问/普通权限映射 |
| 扩展 | MCP / Skill / Rule / Hook / Tool |
| 接入层 | API/SDK/App Server/CLI/Local DB/UI Automation |
| 客户端 | 版本检测、精确跳转、窗口嵌入可行性 |
| 风险 | 接口稳定性、版本变化、账号/隐私/许可证 |

> 调查完成前，不提前固定第一家真实 Adapter。

# 14. Qoder 执行指令

## 14.1 开工前

1. 读取本交接文档与包内 Markdown/SQL。

2. 建立 docs/adr，并将已冻结 ADR 写入。

3. 不要主动修改 Task/Execution/Session/Event 四个核心语义。

4. 若发现架构矛盾，先输出 ADR Proposal，不直接改实现方向。

## 14.2 每次提交必须说明

- 本次修改属于哪个 Module。
- 是否新增/修改 Domain State。
- 是否产生 Durable Event。
- 是否涉及 Permission/Audit。
- Crash 后如何恢复。
- Idempotency 如何保证。
- 有哪些 Contract/State Machine/Recovery Test。

## 14.3 第一阶段成功标准

> 不是“页面看起来能用”，而是 FakeAdapter 端到端链路在 Core Crash、Adapter Crash、重复请求、未知 Provider 状态等情况下仍保持数据一致、任务不丢、操作不重复。

# 15. 交接包文件索引

| 文件 | 作用 |
|---|---|
| Lucero_Qoder_Handoff_v0.1.docx | 本主文档：需求、架构、技术栈、开发路线和 Qoder 指令 |
| README_QODER_START_HERE.md | Qoder 首屏入口 |
| SYSTEM_ARCHITECTURE_V0.1.md | 架构快速参考 |
| CORE_DOMAIN_MODEL_V0.1.md | 领域模型与 Schema 设计说明 |
| 000001_core_schema.sql | Phase 1 SQLite 冻结迁移 |
| PHASE0_ADAPTER_CAPABILITY_SURVEY.md | 五家软件能力调查模板 |
| QODER_FIRST_SPRINT.md | 第一 Sprint 执行清单 |
| LUCERO_PROJECT_STRUCTURE.md | 推荐项目文件夹结构与最简开工排版 |

## 15.1 当前冻结结论

> 到 v0.1 为止，需求、技术栈、系统边界、核心领域模型和 Phase 1 Schema 已经具备开工条件。下一步不是继续无限设计，而是：Phase 0 适配器调查 + FakeAdapter 骨架实现。

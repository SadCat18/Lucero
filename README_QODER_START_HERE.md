# Qoder：请从这里开始

这是“Lucero”的项目交接包。请先不要直接写大量 UI，也不要自行改变架构。

## 必须先读

1. `Lucero_Qoder_Handoff_v0.1.docx`：项目总交接文档。
2. `SYSTEM_ARCHITECTURE_V0.1.md`：架构基线。
3. `CORE_DOMAIN_MODEL_V0.1.md`：Core 领域模型。
4. `000001_core_schema.sql`：SQLite 首个冻结迁移。
5. `PHASE0_ADAPTER_CAPABILITY_SURVEY.md`：首要调查任务。
6. `QODER_FIRST_SPRINT.md`：第一阶段开工任务。

## 已冻结的核心技术栈

- Desktop：Tauri 2 + Vue 3 + TypeScript + Vite
- Core：Go
- Database：SQLite / WAL / FTS5
- Realtime：WebSocket
- Core API：REST
- Adapter Protocol：版本化 JSON-RPC
- Browser Worker：Node.js + Playwright
- Windows Bridge：C#/.NET（按需）
- Terminal：xterm.js + ConPTY
- Git：System Git CLI
- Secret：Windows DPAPI / Credential abstraction

## 最重要的架构原则

- Task 属于统一工作台，不属于 Codex/Qoder。
- Session 属于真实 Provider。
- Execution 是 Task 在某个 Provider/Model 上的一次真实执行。
- UI 不是业务真相源；Go Core 才是。
- 状态变更先落库并写 Event Journal，再实时推送。
- 所有高风险操作经过 Permission Engine。
- Provider/Adapter 状态无法确认时使用 UNKNOWN，绝不猜测。
- Adapter/Worker Crash 不能导致 Core Crash。
- 不要把所有逻辑塞进 Tauri。
- 不要第一阶段引入 Redis/Kafka/RabbitMQ/MySQL/Kubernetes。

## 第一目标

先用 FakeAdapter 打通：

Project -> Existing Session -> Task -> Execution -> Send -> Stream -> Approval -> Quota -> Notification -> Accept

再开始接第一家真实软件。

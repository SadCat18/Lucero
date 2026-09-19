# Lucero 项目交接 v0.1

> ⚠️ **本页是不完整摘要，已于 2026-09-19 降级。** 完整交接主文档见同目录
> [`Lucero_Qoder_Handoff_v0.1.md`](Lucero_Qoder_Handoff_v0.1.md)（由 `.docx` 逐段转换而来）。
> 本摘要**缺失**：ADR-001~015 清单、§9.2 AI 编码强制规则、§12 P0 稳定性红线全文、
> §10 分阶段版本规划、§10.1 Phase 1 验收、§13 Phase 0 调查模板、§14 Qoder 执行指令。
> **worker 一律读全文版，不要以本页为准。** 裁决记录见 `docs/adr/ADR-000-baseline-divergence.md`。

Lucero 是一个 Windows Local-First 多 AI Agent 控制平面。

## 当前冻结技术栈

- Desktop：Tauri 2 + Vue 3 + TypeScript + Vite
- Core：Go Core Daemon
- Database：SQLite + WAL + FTS5
- Realtime：WebSocket
- Core API：REST
- Adapter Protocol：版本化 JSON-RPC
- Browser Worker：Node.js + Playwright
- Windows Bridge：C#/.NET（按需）
- Terminal：xterm.js + ConPTY
- Git：System Git CLI
- Secret：Windows DPAPI / Credential abstraction

## 产品核心原则

1. Task 属于 Lucero，不属于 Codex/Qoder/OpenCode 等 Provider。
2. Session 属于真实 Provider。
3. Execution 是 Task 在某个 Provider/Model 上的一次真实执行。
4. Go Core 是唯一业务真相源。
5. Core 与 Desktop 分进程；UI 崩溃不能导致 Task 丢失。
6. 关键状态变更先落 SQLite 与 Durable Event Journal，再向 UI 推送。
7. 所有高风险操作经过 Permission Engine，并进入 Audit。
8. Provider 状态无法确认时必须使用 UNKNOWN。
9. Adapter/Worker Crash 不能带崩 Core。
10. Provider Failure 不得导致 Task 消失。

## Qoder 开工顺序

1. 阅读根目录 `README_QODER_START_HERE.md`。
2. 阅读 `docs/architecture/SYSTEM_ARCHITECTURE_V0.1.md`。
3. 阅读 `docs/architecture/CORE_DOMAIN_MODEL_V0.1.md`。
4. 检查 `core/migrations/000001_core_schema.sql`。
5. 执行 `docs/adapters/PHASE0_ADAPTER_CAPABILITY_SURVEY.md`。
6. 按 `docs/planning/QODER_FIRST_SPRINT.md` 开始 FakeAdapter Sprint。

## 当前开发策略

分阶段开发，但从第一天按照最终架构设计。

优先级：

> 稳定性 > 核心功能完整度 > 使用体验 > 扩展功能

第一阶段不要直接接真实厂商，先用 FakeAdapter 打通：

```text
Project
→ Existing Session
→ Task
→ Execution
→ Send
→ Stream
→ Approval
→ Quota
→ Notification
→ Accept
```

并强制验证 Core Crash、Adapter Crash、重复请求和 UNKNOWN 状态。

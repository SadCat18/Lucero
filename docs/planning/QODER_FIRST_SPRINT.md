# Qoder First Sprint

## Sprint 目标

不接任何真实厂商，先证明架构骨架成立。

## 1. 建 Monorepo

```text
lucero/
├─ apps/
│  ├─ desktop/
│  └─ web/
├─ core/
├─ adapters/
│  └─ fake/
├─ workers/
├─ packages/
├─ specs/
├─ docs/
└─ scripts/
```

## 2. Go Core Bootstrap

完成：
- config
- single instance
- SQLite
- migration runner
- HTTP server
- WebSocket event stream
- structured logging
- graceful shutdown

## 3. 实现 v0.1 Migration

使用 `000001_core_schema.sql`。

启动时：
- 备份旧 DB（如果有 migration）
- 执行 migration
- migration 失败则 Core 不进入 Ready

## 4. Event Journal

实现统一 Transaction Pattern：

```text
Update Aggregate + Insert Event -> COMMIT -> Publish
```

## 5. FakeAdapter

支持：
- session.list
- session.read
- session.send
- fake streaming
- cancel
- model.list
- quota.read
- approval request
- fake failure
- fake adapter crash

## 6. 第一条垂直链

完整跑通：

```text
Create Project
-> Discover Fake Session
-> Mark Managed
-> Create Task
-> Create Execution
-> Send Message
-> Stream Output
-> Request Approval
-> Resolve Approval
-> Continue
-> Execution SUCCEEDED
-> Task AGENT_COMPLETED
-> User Accept
-> Task ACCEPTED
```

## 7. 必须验证 Crash

- 运行中 kill Core
- 重启 Core
- 正确识别 interrupted/recoverable execution
- 不重复发送消息
- Approval 不丢
- Event Journal 不丢
- SQLite integrity 正常

## 8. 暂时不要做

- 真实 Codex/Qoder Adapter
- 高级 Dashboard
- Model Gateway 完整版
- MCP Gateway 完整版
- Playwright
- SSH/部署
- PWA
- Plugin Marketplace
- Panel AI

先把 Core 可靠性做对。

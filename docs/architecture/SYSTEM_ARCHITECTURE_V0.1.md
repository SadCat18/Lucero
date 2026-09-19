# Lucero｜System Architecture v0.1

## 1. 架构基线

- Windows Desktop：Tauri 2 + Vue 3 + TypeScript + Vite
- Core：独立 Go Core Daemon
- Local First：SQLite + WAL + FTS5
- 客户端协议：REST + WebSocket
- Adapter 协议：版本化 JSON-RPC
- Browser Worker：Node.js + Playwright，按需启动
- Windows UI Automation Bridge：C#/.NET，必要时启用
- Terminal：xterm.js + Windows ConPTY
- Git：优先系统 Git CLI
- Secrets：Windows DPAPI / Credential abstraction
- Semantic Search：先定义 VectorStore 接口，不在 v0.1 锁实现

## 2. 总体原则

1. Go Core 是唯一业务真相源。
2. Task Lifetime 与 Desktop UI Lifetime 解耦。
3. 不做微服务；采用模块化单体 Core + 独立 Adapter/Worker。
4. 各厂商 AI 只作为 Execution Provider；Task、权限、额度、历史、审计属于统一工作台。
5. 所有关键状态变化先持久化，再发布实时事件。
6. Provider 状态不确定时使用 UNKNOWN，不猜测。
7. 高风险操作统一经过 Permission Engine，并进入 Audit。
8. Adapter/Worker 崩溃不能带崩 Core。
9. Task 与 Execution 分离，Provider Failure 不能让 Task 丢失。
10. 所有外部协议、Adapter、Backup Format 都版本化。

## 3. 进程边界

```text
Lucero.exe (Tauri/Vue)
        |
        | REST / WebSocket
        v
Lucero-Core.exe (Go)
        |
        +-- Adapter Supervisor
        |    +-- codex-adapter
        |    +-- qoder-adapter
        |    +-- opencode-adapter
        |    +-- zcode-adapter
        |    +-- workbuddy-adapter
        |
        +-- On-demand Workers
             +-- browser-worker (Node + Playwright)
             +-- windows-bridge (C#/.NET)
             +-- future sandbox/vector/log workers
```

## 4. Go Core 模块

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

## 5. Core 启动顺序

1. Config
2. Single Instance Lock
3. Vault
4. SQLite
5. Migration
6. Event Journal
7. Recovery
8. Adapter Supervisor
9. Task Scheduler
10. HTTP/WebSocket API
11. Health Check
12. Ready

## 6. Desktop 启动

```text
Desktop Start
  -> Detect Core
  -> Core 存在：Connect
  -> Core 不存在：Start Core -> Wait Ready -> Connect
```

关闭主窗口默认只隐藏到 Tray，Core 继续运行。

## 7. API

本地 API 默认只监听 127.0.0.1。

```text
/api/v1/projects
/api/v1/tasks
/api/v1/sessions
/api/v1/adapters
/api/v1/models
/api/v1/quota
/api/v1/approvals
/api/v1/notifications
/ws/v1/events
```

## 8. Durable Event Journal

任何重要业务状态修改必须：

```text
BEGIN
  Update Aggregate
  Insert Event Journal
COMMIT
Publish to in-memory bus/WebSocket/Notification
```

严禁先推 WebSocket、后写数据库。

## 9. Adapter

Adapter 进程隔离并在握手时声明 Capability。

能力等级：

- NATIVE
- COMPATIBLE
- CLI
- LOCAL_DATA
- UI_AUTOMATION
- READ_ONLY
- EXPERIMENTAL
- UNSUPPORTED

Adapter 可按 Native API -> CLI -> Local Data -> UI Automation 自动降级，Core 只消费“当前有效能力”。

## 10. 稳定性 P0

以下均属于 P0：
- 丢任务
- 重复执行高风险动作
- 消息错发到错误 Session
- 跨项目数据污染
- Approval 应用于错误 Execution
- Adapter Crash 带崩 Core
- Core Crash 损坏数据库
- Secret 泄露进普通日志
- 自动转交丢失上下文
- UI 状态与 Core 真相不一致

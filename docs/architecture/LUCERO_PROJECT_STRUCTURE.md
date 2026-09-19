# Lucero 项目目录（简洁版）

建议先创建一个根目录：`Lucero/`。第一阶段只保留真正会用到的目录，不提前建一大堆空模块。

```text
Lucero/
├─ README.md
├─ .gitignore
├─ go.work                       # 后面需要多 Go module 时再用
│
├─ apps/
│  └─ desktop/                   # Tauri 2 + Vue 3 + TypeScript
│
├─ core/                         # Go Core Daemon
│  ├─ cmd/
│  │  └─ core/
│  ├─ internal/
│  │  ├─ platform/
│  │  ├─ project/
│  │  ├─ adapter/
│  │  ├─ model/
│  │  ├─ session/
│  │  ├─ task/
│  │  ├─ execution/
│  │  ├─ approval/
│  │  ├─ permission/
│  │  ├─ quota/
│  │  ├─ eventjournal/
│  │  ├─ audit/
│  │  └─ recovery/
│  ├─ api/
│  │  ├─ http/
│  │  └─ websocket/
│  └─ migrations/
│     └─ 000001_core_schema.sql
│
├─ adapters/
│  └─ fake/                      # 第一 Sprint 先只做 FakeAdapter
│
├─ workers/                      # 先建目录，不急着实现
│  ├─ browser/
│  └─ windows-bridge/
│
├─ specs/
│  ├─ openapi/
│  ├─ adapter-protocol/
│  └─ events/
│
├─ docs/
│  ├─ architecture/
│  ├─ adr/
│  └─ adapters/
│
└─ scripts/
```

## 你现在创建项目时最简版本

如果你想先让 Qoder 开工，甚至可以只建：

```text
Lucero/
├─ README.md
├─ docs/
├─ core/
├─ adapters/
│  └─ fake/
└─ apps/
   └─ desktop/
```

其他 `workers/`、`specs/`、`scripts/` 让 Qoder 在实际需要时再补。这样项目不会一开始就充满空目录。

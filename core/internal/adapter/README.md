# `internal/adapter` — Core↔Adapter 协议层

实现 `specs/adapter-protocol/adapter-protocol-v0.1.md` 的帧与语义。零跨包 import
（见 `purity_test.go`），不碰 SQLite、不发事件、不拉进程（`os/exec` 归 `internal/process`）。

## 两套拼法，别写混

- **capability 名用点**：`session.send`、`quota.read`、`debug.crash` —— 它们进
  `adapter_capabilities.name`（契约 §3）。
- **方法名用斜杠**：`session/send`、`quota/read`、`debug/crash` —— 它们是帧里的 `method`
  （契约 §4，共 18 个，§4 的表是唯一清单）。
- 映射就是一处字符串替换：`capabilityName(method)`（`handshake.go`）把 `session/send` 变成
  `session.send`。§9 的 `debug.*` gating 对**两种拼法都拦**，因为安全面不容拼写歧义。

## 引用状态

本包注释里的引用分两类，读的时候分开对待：

- **`§n`**：指 `specs/adapter-protocol/adapter-protocol-v0.1.md` 的第 n 节，以该文件为准。
- **`CO-x`**：主控 2026-09-19 的逐条裁定（错误码表、health 形状、投递语义、turn 双信号等），
  台账在 `.qoder/progress/adapter.md` 的 `## Coordination`。
  往外转述前先 `grep -n` 一下契约文件在不在；不在就说 CO 编号，别说节号。

# Lucero ADR 索引

规则：本目录是架构决策的唯一落点。**未新增/更新 ADR，不得推翻下列任何一条**（交接主文档 §14.1）。
发现矛盾时先写 ADR Proposal，不直接改实现方向。引用文档时优先用「标题文字」而非 `§NN`。

| ID | 标题 | 状态 |
|---|---|---|
| ADR-000 | [基线分叉与真相源裁决](ADR-000-baseline-divergence.md) | accepted |
| ADR-001 | Core 用 Go | accepted (frozen) |
| ADR-002 | Desktop 用 Tauri 2 + Vue 3 + TypeScript + Vite | accepted (frozen) |
| ADR-003 | Core 与 Desktop 分进程 | accepted (frozen) |
| ADR-004 | 模块化单体，不做微服务 | accepted (frozen) |
| ADR-005 | Adapter 进程隔离 | accepted (frozen) |
| ADR-006 | Adapter 协议版本化 | accepted (frozen) |
| ADR-007 | SQLite Local First | accepted (frozen) |
| ADR-008 | Durable Event Journal | accepted (frozen) |
| ADR-009 | REST + WebSocket | accepted (frozen) |
| ADR-010 | 重组件按需启动 Worker | accepted (frozen) |
| ADR-011 | 集中式 Permission Engine | accepted (frozen) |
| ADR-012 | 敏感操作进 Audit | accepted (frozen) |
| ADR-013 | Task 与 Provider 解耦 | accepted (frozen) |
| ADR-014 | Task 与 Execution 分离 | accepted (frozen) |
| ADR-015 | Provider 失败不得丢 Task | accepted (frozen) |
| ADR-016 | SQLite 驱动锁 `modernc.org/sqlite` | accepted 2026-09-19 |
| ADR-017 | `aggregate_version` 存在各聚合表上 | accepted 2026-09-19 |
| ADR-018 | 审计链头锚点在库外文件，v0.1 不签名 | accepted 2026-09-20 |

ADR-001~015 由交接主文档「9.3 ADR」清单落盘。原文只给标题，下面的决策/后果正文由主控依据
「3. 系统架构」「8. 技术栈冻结」「12. P0 稳定性红线」三节补写，**不改变任何决策内容**。

---

## ADR-001 Core 用 Go

- **决策**：业务核心是独立的 Go 守护进程 `Lucero-Core.exe`。
- **后果**：真实 Provider SDK 不进 Core 编译单元（见 ADR-005）；交叉编译与免 CGO 是硬要求，直接影响 ADR-016。
- **依据**：技术栈冻结表「Core | Go | 唯一业务真相源；模块化单体」。

## ADR-002 Desktop 用 Tauri 2 + Vue 3 + TypeScript + Vite

- **决策**：Windows 主界面、Tray、快捷键、通知用 Tauri 2 + Vue 3。
- **后果**：不用 Electron。**Rust 工具链在本机仍缺失，且 First Sprint 明确不接 Tauri**——桌面端只做纯浏览器 Vite 应用，打包推迟。
- **依据**：技术栈冻结表；`QODER_FIRST_SPRINT` §8「暂时不要做」。

## ADR-003 Core 与 Desktop 分进程

- **决策**：Core 常驻后台，UI 只是它的一个客户端。关主窗口默认隐藏到 Tray，Core 继续跑。
- **后果**：Task 生命周期与 UI 生命周期解耦；远程 PWA / CLI / 插件 / 面板 AI 复用同一套 API。
- **依据**：§3.2「Core 为什么独立进程」。

## ADR-004 模块化单体，不做微服务

- **决策**：Core 是模块化单体，按业务模块**垂直切分**（`internal/task`、`internal/session`…）。
- **后果**：禁止 `models/services/repositories` 大杂烩；模块间只走 Public Service API 或 Domain Event；
  禁止 `GlobalDatabaseRepository`；禁止裸 goroutine，长任务走 Supervisor/WorkerPool/Scheduler。
- **依据**：§3.4 模块通信约束、§9.2 编码强制规则、领域模型 §45 Repository Boundary。
- **对编排的影响**：角色边界按此切分；`db` 角色是**平台层**而非"全局仓储层"，见 ADR-017 与内部契约。

## ADR-005 Adapter 进程隔离

- **决策**：每个厂商 Adapter 是独立进程，经版本化 JSON-RPC 与 Core 通信，由 Adapter Supervisor 监管。
- **后果**：Adapter 崩溃只影响该 Execution；Core 必须能观测到进程退出并分类恢复。真实 SDK/CLI 永不链进 Core。
- **依据**：§9.3 ADR-005、§6.1 Adapter 进程隔离、§10 稳定性 P0「Adapter Crash 带崩 Core」。

## ADR-006 Adapter 协议版本化

- **决策**：Adapter 协议带版本号，握手时声明 Capability；可按 Native API → CLI → Local Data → UI Automation 降级。
- **后果**：Core 只消费"当前有效能力"，能力等级必须逐条持久化（`adapter_capabilities`），不能整体降级为"支持/不支持"。
- **依据**：§9 Adapter、§6.2/6.3/6.4。
- **待 Phase 0 输入**：capability 枚举粒度应对齐被查软件的现成协议面，而非自造（见账本上行输入）。

## ADR-007 SQLite Local First

- **决策**：SQLite + WAL + FTS5，`synchronous = FULL`，`busy_timeout = 5000`，`foreign_keys = ON`。
- **后果**：库级 PRAGMA 一次性设置、连接级 PRAGMA 每条池化连接都要设；不引入 Redis/Kafka/RabbitMQ/MySQL/PG Server/K8s。
- **依据**：领域模型 §42 SQLite 连接策略、§8.1 第一阶段明确不使用。

## ADR-008 Durable Event Journal

- **决策**：重要状态变化必须在**同一个 SQLite 事务**里「改聚合 + 写 Event Journal」，提交后才发布。
- **后果**：严禁先推 WebSocket 再写库；UI 永远看不到库里不存在的状态。`event_journal.seq` 是本机严格顺序。
- **依据**：§3.5 / §8 Durable Event Journal、领域模型 §44 Transaction Pattern、§48 Event Publishing Rule。

## ADR-009 REST + WebSocket

- **决策**：Core 对外 REST（`/api/v1/*`）+ WebSocket（`/ws/v1/events`），默认只监听 `127.0.0.1`。
- **后果**：API-first，桌面端不得绕过后端自算业务状态。
- **依据**：§7 API、§3.2。

## ADR-010 重组件按需启动 Worker

- **决策**：Browser(Playwright)、Windows Bridge(UI Automation) 等重依赖作为按需 Worker，不常驻、不链进 Core。
- **后果**：First Sprint 不实现任何 Worker，仅保留目录；任何外部进程必须归属 Process Supervisor 并记 `process_instances`。
- **依据**：§3 进程边界、§9.3 ADR-010、领域模型 §39 Process Runtime。

## ADR-011 集中式 Permission Engine

- **决策**：所有高风险操作统一经 Permission Engine，输出 `ALLOW / DENY / REQUIRE_APPROVAL / ALLOW_TEMPORARY`。
- **后果**：优先级链为 紧急停止 → 显式 Deny → Trust Domain → Project → Task Mode → Session Override → Temporary Grant → 默认。
  不得绕过它写权限判断，UI 也不得自行放行。
- **依据**：§7.1/7.2、领域模型 §27 Temporary Grant、§29 Permission Policy。

## ADR-012 敏感操作进 Audit

- **决策**：Audit 与 Event Journal 分开——Event 记"发生了什么"，Audit 记"谁以什么权限做了什么"，
  用 `previous_hash`/`entry_hash`/`signature` 构成防篡改链。
- **后果**：Secret 只能以 `vault://…` 引用形式存在，任何明文（密码/API Key/私钥/Cookie/Token）
  进 Event payload、普通日志、Audit details、`error_message` 都算 P0。删除或伪造 Audit 禁止。
- **依据**：领域模型 §35、§58 Audit 与 Secret、§9.2。

## ADR-013 Task 与 Provider 解耦

- **决策**：Task 属于统一工作台，Codex/Qoder/OpenCode 等只是 Execution Provider。
- **后果**：切厂商/切模型不新建 Task；跨软件转交时 Task 不变，变的是 Execution 与 Session。
- **依据**：§4.1、§0.1 必须先接受的架构事实。

## ADR-014 Task 与 Execution 分离

- **决策**：Task = 用户目标；Execution = 某 Provider/Model 对 Task 的一次真实执行。
- **后果**：两套状态机**不完全相同**（如 Execution `FAILED/QUOTA_EXHAUSTED` 对应 Task `TRANSFERRING`）；
  Execution 启动时必须快照模型与权限配置，历史才可解释。
- **依据**：领域模型 §22、§21、§11「Execution 启动时必须将实际模型快照写入」。

## ADR-015 Provider 失败不得丢 Task

- **决策**：Provider 失败/中断后 Task 必须仍可恢复，只产生新 Execution。
- **后果**：需要 `execution_leases`（防重复执行）、`idempotency_keys`（防重试重复高风险动作）、
  Core 启动扫描未完成 Execution 并结合 heartbeat/process/`provider_execution_id` 判定 reconnect/resume/interrupt。
- **依据**：§7.5 Crash Recovery、领域模型 §24、§40、§56。

---

## ADR-016 SQLite 驱动锁 `modernc.org/sqlite`

- **状态**：accepted（用户 2026-09-19 明确「接受 modernc，不装 MSVC」）
- **背景**：本机 `gcc`/`cc`/`clang` 全无、无 VS Build Tools（实测 PATH 与 `/mingw64/bin` 均无）→ **CGO 不可用**，
  `mattn/go-sqlite3` 无法编译。目标机也不允许为依赖装 3~5 GB MSVC 链。
- **决策**：唯一 SQLite 驱动为纯 Go 的 `modernc.org/sqlite`；`database/sql` 注册名固定为 `sqlite`。
  连带要求：构建禁止开启 CGO（`CGO_ENABLED=0`），任何引入 cgo 依赖的提案需新 ADR。
- **后果**：`STRICT` 表与 FTS5 可用（该驱动内置 SQLite ≥ 3.37，实测版本由 `go test` 断言而非猜测）；
  纯 Go 实现写入吞吐低于 CGO 版——但 ADR-007 的 `synchronous=FULL` + 单用户串行写路径下不构成瓶颈。
  升级路径：若将来实测写入成为瓶颈，先做批量 flush 与写合并，再考虑装 MSVC。
- **附带环境决定**：Go/Node 以免安装 ZIP 落在 `D:\DevTools\`，**不修改 PATH**（属红线③）；
  所有构建/测试命令使用绝对路径 `D:\DevTools\go\bin\go.exe`。`GOPROXY=https://goproxy.cn,direct`、
  `GOPATH=GOMODCACHE=D:\DevTools\go-data`、`GOTOOLCHAIN=local` 已用 `go env -w` 固定。

## ADR-017 `aggregate_version` 存在各聚合表上

- **状态**：**accepted**（用户 2026-09-19 批准，含 `000002` 迁移草案已落盘）
- **背景**：ADR-008 要求每个事件带 `aggregate_version`，`000001` 已建
  `UNIQUE(aggregate_type, aggregate_id, aggregate_version)`；但 `tasks`/`sessions`/`executions` 等聚合表
  **都没有版本列**，因此"当前版本是几"无处可查，唯一约束在并发下保护不了任何东西。
  这是勘查阶段记录在案的头号缺口（交接包未定义）。
- **决策**：新增 `000002`，给 **10 张**聚合根表各加 `version INTEGER NOT NULL DEFAULT 1`：
  `projects` `adapter_instances` `models` `sessions` `tasks` `executions`
  `approvals` `quota_accounts` `notifications` `handoffs`。
  不加 `CHECK (version > 0)`——`ALTER TABLE ADD COLUMN` 在 STRICT 表上对约束支持面更窄，
  单调性由唯一取号语句与事件唯一约束共同保证。
  写路径由 db 平台层提供唯一入口，在**同一事务**内原子取号：

  ```sql
  UPDATE tasks SET status = ?, updated_at = ?, version = version + 1
  WHERE id = ? RETURNING version;
  ```

  随后用返回的 `version` 插事件。取号与改状态是一条语句，故无竞态；
  违反唯一约束即说明有代码绕过了平台层，应作为缺陷修到源头。
- **备选与被否**：① 从 `event_journal` 取 `MAX(aggregate_version)+1`——不持久化聚合自身版本，
  读放大且无法表达"聚合当前第几版"；② 独立 `aggregate_versions` 表——多一次写、多一处不一致窗口。
- **后果**：`messages`、`event_journal`、`audit_entries`、`execution_output_chunks` 是**append-only**，不加版本列；
  API 与 UI 展示聚合状态时可直接用 `version` 做乐观并发与"我看到的不是你现在的"提示。
- **不违反冻结**：领域模型 §51 Migration Strategy 明确「新变化只增加新 migration」，本条只增不改。

## ADR-018 审计链头锚点存在库外文件，v0.1 不签名

- **状态**：**accepted**（主控 2026-09-20 12:2x 裁定，回 audit 的「`Head` checkpoint 存哪儿、谁签名」）
- **背景**：`audit.VerifyChain(entries, head)` 已经能检出**改写中间条目**（`entry_hash` 不符）、
  **删中间条目**（链接与 seq gap）、**乱序追加**；`head` 传零值时它只证明"链内自洽"。
  **尾部删除只有 head 对不上才检得出**，而 `Checkpoint(entries)` 目前只在内存里算、没有任何持久锚点
  → "删掉最后 N 行"这条路径今天完全无感。audit 在进度文件里的自述成立：
  **在此项落地前，"审计已防篡改"这句话不许说**。
- **决策**：链头锚点存在 **SQLite 文件之外** —— 数据目录下 `audit.head`，**append-only 检查点日志**，
  每行 `seq \t entry_hash \t unix_ms`，只有持有 DB 写权的那个进程（Core）追加。追加时机三处：
  ① 启动校验通过后；② 干净关闭时；③ 每累计 1000 次成功 append（把"未检出的截断窗口"从"整段运行期"
  压到"至多 1000 条"，代价是一行写）。启动时取**seq ≤ 库内 `MAX(seq)` 的最大一行**作为期望 head 交给
  `VerifyChain`；对不上即事故，**不得重算哈希、不得删行、不得补写检查点**（§9.2）。
  **顺序硬约束**：检查点只在所属事务 `COMMIT` 成功之后追加，并 `Sync()` 落盘——
  检查点**绝不领先于库内状态**，否则崩溃后"文件里有、库里没有"会被误报成篡改。
- **备选与被否**：
  ① **存 `settings` 表 GLOBAL 行**（我最初的倾向）——**否**。与 `audit_entries` 同一个 DB 文件的人为可写面，
  能删行的人同样能改这一行，它检不出自己想检的那类损害；却把"链头"从一处真相变成两处，
  多一个不一致窗口和多一套修复逻辑。
  ② 完全不落，只靠 `VerifyChain` 自洽——**否**。那等于承认尾部删除不可检，而 §12 要求不可检的东西**别说成能检**。
  ③ 每次 append 都写检查点——**否**。写放大一倍且收益只是窗口从 1000 到 1，Wave-2 不需要。
- **诚实边界（必须写进报告与文档，不许被"防篡改"三个字吞掉）**：本锚点防的是
  **通过 SQL/接口面造成的损害**（Core 自身 bug、迁移事故、误操作脚本、换库文件）。
  拥有该目录写权限的人可以连带删掉 `audit.head`，**这不是本机制的承诺范围**（他能删整个目录），
  真要到那一步需要外部可验证锚点 = 签名/时间戳。
- **v0.1 不签名**：`audit_entries.signature` 保持空串，`Head` 结构不增密钥字段。
  Phase 6 引入设备密钥或外部时间戳服务时另开 ADR。
  在签名落地前，链状态对外词表只有 `VERIFIED_LOCAL` / `UNVERIFIED` / `UNKNOWN`，
  **禁止**出现"已签名 / signed / 防篡改 / tamper-proof"（与「不把 `UNKNOWN` 显示成确定值」同源）。
- **归属**：全部落在 `core/internal/audit/**`（契约③ §7 已归 `audit`），`cmd/core` 只把数据目录路径传进去。
  **不新增包、不新增 §7 owner 行**——我原本要为 `internal/settings/**` 开行，本裁定把它否掉了；
  全仓今天没有任何 Go 代码读写 `settings` 表（唯一命中是 `permission/source_sqlite.go` 的**读**，那是策略配置读取，属既有范围）。
- **不违反冻结**：不加迁移、不改 `audit_entries` 列、不动 `entry_hash` 算法。

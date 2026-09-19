# ADR-000: 基线分叉与真相源裁决

- 状态: **已接受 (accepted) — 2026-09-19，用户「按建议全办 D1~D4」**
- 日期: 2026-09-19
- 参与者: 主控会话 (orchestrator)
- 依据: docx §14.1「若发现架构矛盾，先输出 ADR Proposal，不直接改实现方向」

## 决议执行结果

| 待裁决项 | 决议 | 实际执行 | 验证 |
|---|---|---|---|
| **D1 真相源** | 以交接包 A 为准 | A 版 `CORE_DOMAIN_MODEL_V0.1.md` 覆盖仓库 B 版 | `tr -d '\r' \| sha1sum` 两侧同为 `0e4bd774` |
| **D2 编号** | 仓库版编号对齐 A 版 63 节 | 由 D1 的整文件覆盖直接达成（**不手工重排 1222 行**） | `grep -c '^# 63'` = 1，63 节齐全 |
| **D3 丢失事实** | 三项全部补回 | 同上，随覆盖一并回来 | `ON DELETE CASCADE` 1 处、`archived_at` 3 处、`aggregate_version` 4 处 |
| **D4 主文档** | docx 全文转 md 入库 + 原件并存 | `docs/handoff/Lucero_Qoder_Handoff_v0.1.md`（734 行 / 30.8 KB / 8 表 / 围栏配平）+ `.docx` 原件入库；原 1950 B 摘要页顶部加**降级警告**指向全文版 | `ADR-001`/`ADR-015`/`P0 稳定性红线`/`AI 编码强制规则`/`Emergency Stop` 各 1 处均在 |

转换脚本 `.scratch/docx2md.py`（一次性工具，`.scratch/` 已 gitignore）。
关键转换点：docx 的 `CodeBlock` 段落内换行是 `<w:br/>` 而非独立段落，必须保留段内换行**和前导空格**，
否则 `PRAGMA` 四条会被粘成一行、ASCII 架构图会错位。

> 编号引用纪律：本仓库文档现已恢复 A 版 63 节编号，`§NN` 引用重新有效。
> 但契约与 progress 文件仍**优先用标题文字引用**（如「领域模型 §45 Repository Boundary」），
> 因为后续 ADR 可能再次增删节。

## 背景

存在两份自称 v0.1 冻结的基线，内容不一致：

- **A. 本地交接包** `D:\Work资源\Lucero_Project_Starter_v0.1\Lucero_Project_Starter\`
- **B. GitHub `SadCat18/Lucero` main @ c94b8c2**（本仓库，即 B）

本仓库是 B 的 clone，因此**仓库内文档是 B 版**。

## 实测比对（逐文件，忽略行尾差异）

| 文件 | 结论 |
|---|---|
| `core/migrations/000001_core_schema.sql` | **完全一致** (31569 B) |
| `docs/architecture/SYSTEM_ARCHITECTURE_V0.1.md` | 一致 |
| `docs/architecture/LUCERO_PROJECT_STRUCTURE.md` | 一致 |
| `docs/adapters/PHASE0_ADAPTER_CAPABILITY_SURVEY.md` | 一致 |
| `docs/planning/QODER_FIRST_SPRINT.md` | 一致 |
| `README.md` / `README_QODER_START_HERE.md` / `.gitignore` | 一致 |
| `docs/architecture/CORE_DOMAIN_MODEL_V0.1.md` | **不一致**：A=2027 行/26630 B，B=1222 行/18242 B |
| `docs/handoff/` | **形态不同**：A=`Lucero_Qoder_Handoff_v0.1.docx` (55 KB)，B=`LUCERO_HANDOFF_V0.1.md` (1950 B) |

## 差异的实质

### 1. 编号整体漂移（最危险，影响面最大）

B 版把 A 版的 63 节合并重排为约 47 节。例如 A 的 `§3 ID 规范` + `§4 时间规范` → B 的 `§3 ID 与时间规范`；此后**每一节的编号都错位**。

后果：任何按 `§NN` 形式的引用在仓库里指向错误内容。本轮编排共识中引用过的
`§31 Quota Snapshot`、`§33 Aggregate Version`、`§42 SQLite 连接策略`、`§45 Repository Boundary`、
`§51 Migration Strategy`、`§52 Phase 1 Schema Freeze`、`§54 关键唯一约束`、`§55 Delete Strategy`、
`§58 Audit 与 Secret`、`§61 第一条实现链`、`§62 v0.1 验收标准` **全部是 A 版编号，在本仓库中失效**。

### 2. B 版确实丢失的内容（token 级验证）

| 事实 | A | B | 影响 |
|---|---|---|---|
| `ON DELETE CASCADE` 禁令（关键表不得物理级联删除） | 有 | **0 处** | worker 读 B 版不知道这条约束存在 |
| `archived_at` 软删除 / 数据生命周期字段 | 3 处 | **0 处** | 同上，§55/§59 的具体落地字段消失 |
| `aggregate_version` 定义 | 4 处 | 1 处 | **§33 被压成一句话**，会让人误以为该机制已定义（实际它仍是缺口） |
| `idempotency_keys` | 2 处 | 1 处 | 细节压缩 |

未丢失（抽查确认 B 版仍在）：`synchronous = FULL`、`previous_hash`、`execution_output_chunks`、
`OBSERVED`、`UI_AUTOMATION`、`vault://`、`Redaction`、`policy_rules`、`quota_snapshots`、
`task_session_links`、`attempt_no`、`predecessor_execution_id`。

> 更正一处自我审查：比对脚本曾报 `state_confidence`、`crash` 两侧皆 0，那是我的 grep
> 大小写与用词失误（`state_confidence` 只存在于 SQL 中，`Crash` 在 md 里是大写），**不是**文档差异。

### 3. 交接主文档：docx → 摘要 md

B 版用 1950 字节摘要替代 55 KB 主文档。摘要**保留了**技术栈、10 条核心原则、开工顺序、FakeAdapter 链。
**丢失了**：ADR-001~015 清单、§9.2 十条 AI 编码强制规则、§12 P0 稳定性红线全文、
§10 分阶段版本规划表、§10.1 Phase 1 验收、§13 Phase 0 调查模板、§14 Qoder 执行指令。

## 待裁决项（请逐项给结论）

- **D1 真相源**：以 A 为准回填 B？以 B 为准、A 降级为附件？还是「B 为正文、A 全文入库为附录」？
- **D2 编号**：若保留 B 版正文，是否允许我用一次 `000002` 之外的纯文档变更把 B 版重新编号回 A 版的 63 节，以恢复所有 `§NN` 引用？
- **D3 丢失事实**：§55 的 `ON DELETE CASCADE` 禁令、§59 的 `archived_at`、§33 的 `aggregate_version` 定义，是否确认按 A 版补回 B 版？
- **D4 主文档**：是否把 docx 正文完整转成 `docs/handoff/Lucero_Qoder_Handoff_v0.1.md` 入库（保留 ADR 清单、编码规则、P0 红线全文）？

## 建议决议

**D1+D4：A 为准、docx 全文转 md 入本仓库，`.docx` 原件一并入库留证。**
**D2：B 版重新编号对齐 A 版。**
**D3：三项丢失事实全部补回。**
理由：本仓库是唯一协作现场，7 个 worker 只读得到仓库里的东西；一份少了 P0 红线全文和编码强制规则的
摘要当基线，等于把 §12 红线的执行责任推给 worker 的自觉。

## 影响

D1~D4 未裁决前，Wave-2 不派发（所有 role 的边界都从这些文档推导）。
Wave-0 的 `survey` role 不受影响：它读的是五家软件本体，不读领域模型。

## 本 ADR 的产生方式

可复现：对 A/B 两侧做 `tr -d '\r'` 归一后的逐文件 sha1 比对 + 关键 token 计数。
原始输出未落盘（属于一次性诊断），复现命令见本仓库 `.qoder/progress/orchestration.md` 的
Verification evidence 段。

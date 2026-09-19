OWNER_ROLE: orchestrator
TASK_ID: orchestrator
STATUS: in_progress
SCOPE: Lucero 多会话编排的唯一对外接口：派活、验收、记账、升级红线；不亲自写业务代码
EVIDENCE_DIR: .qoder/evidence/
LAST_UPDATED: 2026-09-19T15:05+08:00

## Objective

用 Qoder 会话工具把一个 Go Core + SQLite 项目拆成 6 个 worker 会话并行推进 First Sprint，
用户只与本会话对话：每个 Wave 闭环时拿到一次带证据的汇报，其余时间不需要监管任何 worker。

## Completion criteria

- [ ] Wave-0：仓库、工具链、基线裁决入口就位；survey 会话在跑
- [ ] Wave-1：契约①（ADR + 事件命名 + aggregate_version + redaction 接口 + Approval Service API）经用户批准
- [ ] Wave-2：垂直链跑到 `Task ACCEPTED`，db/backend/adapter/audit/test 五方交付均有重跑通过的证据
- [ ] Wave-3：观察面板纯 REST/WS 驱动；kill Core / kill Adapter 崩溃测试通过
- [ ] 全程：blocked 项零丢失，红线零绕过

## Scope boundaries

- In scope: `.qoder/progress/orchestration.md`、`docs/adr/ADR-000*`、仓库与工具链搭建、派发与验收、cron 生命周期
- Out of scope: 业务代码实现（属各 worker）、`git push`（红线②）、修改已冻结件（红线①）、运行五家第三方软件（红线⑤）

## 编排规则（本会话与所有 worker 的共同契约）

### 默认技能

**每个会话（主控与 6 个 worker）启动即加载并全程遵守：**
- `ponytail` (full) — 最懒可行解：先问要不要存在 → 复用已有 → 标准库 → 平台原生 → 已装依赖 → 一行 → 才写最少代码。
  禁止未请求的抽象、样板、"为以后预留"的脚手架。故意留下的简化用 `ponytail:` 注释标出上限与升级路径。
  不许偷懒的部分：信任边界校验、防丢数据的错误处理、安全措施、以及 §12 P0 十条。
- `project-checkpoint` — 角色隔离的 progress 文件、一步一验、每条完成声明配 observed evidence、
  每次交接输出规定的 7 行报告。

### 角色与文件所有权（7 会话 = 1 主控 + 6 worker）

| role | 唯一可写 | 硬约束 |
|---|---|---|
| `db` | `core/migrations/*.sql`（独占 .sql 修改权）、连接与 PRAGMA、串行写通道、事务原语、Event Journal 写入机制、blob、**强制 redaction 中间件** | 不提供全局 Repository |
| `backend` | `internal/task` `execution` `quota` `session` `message` + 调度 | 只能经 audit 的 Service API 读 Approval，禁止直查 `approvals` 表 |
| `audit` | `internal/approval` `permission` `audit`、`approvals` 聚合与状态迁移、TemporaryGrant、`policy_rules`、敏感规则集 | 审批裁决权与任务推进权分家；只定规则不实现 redactor |
| `adapter` | `specs/adapter-protocol`、`adapters/fake/`、`internal/adapter` `process` | 进程崩溃不得影响 Core；`debug.*` 走 capability gating |
| `frontend` | `apps/desktop/`、`packages/api-client` | 只准 REST/WS 驱动，禁止 import Core 内部类型 |
| `test` | `scripts/`、contract / state-machine / crash 三类用例 | 不改业务代码 |
| `survey` | `docs/adapters/phase0/` | 只出报告，不碰代码 |

共享文件（`go.mod`、`specs/*/contracts*`、本文件）单一 owner，其余只能提 REQ。

### 红线（命中即停 → 写 blocked → 绝不绕道）

① 改已冻结物：`000001_core_schema.sql`、四份架构/规划文档、Task/Execution/Session/Event 语义
② `git push` / force push / 删分支 / `reset --hard` / 删非自建文件
③ 装全局软件、改 PATH、管理员操作 —— **降级为每日批量申请一次**
④ 下载依赖以外的东西 —— **同样每日批量申请**
⑤ 运行五家第三方软件、读其本地数据、使用用户登录态 —— **逐次申请**
⑥ 出现 §12 P0 十条任一症状（含把 UNKNOWN 显示成确定值）
⑦ 两 role 契约冲突且契约文件无解
⑧ 声称完成但主控重跑验收失败

唯一自动放行例外：progress 的 `Remaining` 中存在**预先经用户批准的**已排序备选 action。
禁止任何"跳过验证先交付"。

### 推进与打扰阈值

- 前台：`wait_chat_sessions(≤8, 60s)` → 读 worker 的 progress 文件（**不读 transcript**）→ 主控亲自重跑其验收命令 → 记账
- 用户说「挂起来跑」→ 启用 cron `every 10m` / `merged` / Auto / cwd=`D:\Project\Lucero`；
  cron 只准 `send_message` + 改本文件，禁止写代码，每轮 ≤2 动作，只输出一行状态
- 日报：每天 `09:00 Asia/Shanghai`，merged，只写本文件的留档段，不吵用户
- 闸门：活跃会话数 ≤ 当前 Wave 角色数；连续 3 轮零进展 → cron 自我 `disable` + 写 NEEDS YOU
- 用户说「回来了，给我现状」→ disable cron，先读本文件顶部 NEEDS YOU，只给状态表 + blocked 列表
- 汇报节奏：**每个 Wave 闭环叫用户一次**，必附 `.qoder/evidence/` 路径 + 主控重跑的原始输出

### 波次

- **Wave-0** 仓库 + 工具链 + ADR-000 + `survey` 启动
- **Wave-1** 主控写契约①（ADR-001~015 落盘 / 事件命名 / `aggregate_version` 存法 / redaction 规则接口 / Approval Service API）→ 用户批
- **Wave-2** `db`+`backend`+`adapter`+`audit`+`test` → 垂直链跑到 `ACCEPTED`
- **Wave-3** `frontend` 观察面板 + Permission Engine 完整版 + Crash 测试

`audit` 由 Wave-3 提前到 Wave-2：垂直链在 Wave-2 就要用 Approval（审批聚合归 audit），
且 db 的强制脱敏中间件要吃 audit 的敏感规则集。

### 现实约束（做不到 / 已替你放弃的）

- worker 的**模型档位设不了**：`create_chat_session` 只有 `prompt` 参数，没有 model 字段（只有 `qoder_cron` 能指定）。
  降档只能用户在 UI 里手动切那个会话。
- **额度消耗读不到**：没有任何工具能给出 token/费用数字。闸门只能用动作数 / 会话数 / 空转轮数。
- **无法推送通知**：cron 的输出留在 Automations 里。红线是拉取式——用户不回来问，链就停在 blocked 直到空转自禁。
- 放弃 sqlite3 CLI：Go 侧 `modernc.org/sqlite` 内置引擎，`integrity_check` 由 test 从 Go 里跑。

## Current checkpoint

- Stage: Wave-1 契约① 已成文，等用户批准（Wave-2 派发门禁）
- Status: blocked
- Last verified action: 工具链实跑通过（`go version go1.27.1 windows/amd64`、`node v24.19.0`）；
  `go env` 回读确认 `GOPROXY=https://goproxy.cn,direct`、`GOMODCACHE=D:\DevTools\go-data\mod`；
  契约① 三份文件落盘（`docs/adr/README.md` 11 KB、`specs/events/event-contract-v0.1.md` 11.8 KB、
  `specs/contracts-internal-v0.1.md` 10 KB）
- Next action: 用户批准契约① → 主控写 `000002` 迁移草案 → 派发 Wave-2 五个 worker
- Verification: `git -C /d/Project/Lucero log --oneline -1` 出现基线 commit 且 `git status --short` 干净

## Blockers

- **B1（新，Wave 门禁）** 契约① 需用户批准才可派 Wave-2。需拍板四点：
  ① `000002` 给 9 张聚合表加 `version` 列（ADR-017）；
  ② 事件命名改用 `task_created` 小写下划线，**偏离**交接主文档时间线示意图里的 `TaskCreated`
     （主控判断：示意图是行文而非协议，但这是冻结文档表述的改动，须你确认）；
  ③ Redaction 机制在 db、规则在 audit 的具体接口形状；
  ④ `approvals` 表 SQL 只准出现在 `internal/approval`（Q33=B 的落地）+ grep 可执行检查。
- **B2** 基线 commit 需红线②授权（worker 无 diff 可对照，ownership 违规也查不了）
- ~~B1(旧) ADR-000 未裁决~~ 已于 15:18 关闭：D1~D4 全按建议执行并验证


## 派发账本

| REQ | role | sessionId | 状态 | Wave | 出口条件 |
|---|---|---|---|---|---|
| R-001 | survey | `7726e47f-d772-4f98-9b2a-2588ec5fd46b` | running | 0 | 五家矩阵 + 优先级 + fallback 链 |
| R-002 | db | — | 未派（等 Wave-1） | 2 | |
| R-003 | backend | — | 未派 | 2 | |
| R-004 | adapter | — | 未派 | 2 | |
| R-005 | audit | — | 未派 | 2 | |
| R-006 | test | — | 未派 | 2 | |
| R-007 | frontend | — | 未派 | 3 | |

## 来自 worker 的上行输入

- **[影响 Wave-1 契约②] Codex 有现成的 native 协议面**：`codex app-server generate-json-schema`
  产出 v2 协议 **379 个 schema 文件**，方法名已覆盖 `ThreadList/Read/Resume/Fork/Rollback`、
  `TurnStart(threadId)`、`TurnInterrupt`、`ModelList`、`GetAccountRateLimits`、
  `ExecCommandApproval/ApplyPatchApproval/PermissionsRequestApproval`、`SkillsList/HooksList/McpServer*`。
  → **`specs/adapter-protocol` 不要自造 capability 枚举**，握手时应按该 schema bundle 的粒度声明；
    精确额度与 Reset 时间在 Codex 侧是 NATIVE（`usedPercent/remainingPercent/resetsAt/credits`）。
  → Codex 的 `session_meta` rollout JSONL 带**单调递增 `ordinal`**，是天然增量游标，
    正好落进 `sessions.sync_cursor`。
- 结论倾向：Codex 大概率是首选接入目标。等 survey 把另外四家填完再定序，**不提前固定**（docx §6.5 明令）。



## Completed

- 环境与仓库事实勘查：无 Go/Node/Rust/C 编译器；git 2.52、gh 已登录（token 含 `repo`+`workflow`）；
  远端仓库 **private**、`main` 单 commit。证据见下。
- 会话工具能力边界确认：`create` 无 `cwd`/`model` 参数、`wait` 上限 60s/8 会话、`read` 截断。
- 需求拷问 34 项，全部收敛并签字（含 2 处由主控推翻的排期：audit 提前、sqlite CLI 放弃）。
- `git clone D:\Project\Lucero` → 工作副本建立，旧 starter 目录按约定**原样只读保留**。
- 跨目录写入探针：Write 到 `D:\Project\Lucero\docs\adr\ADR-000-baseline-divergence.md` 成功且无权限弹窗
  → 结论：本会话可直接充当主控，用户无需手工开新工作区。
- ADR-000 基线分叉报告落盘（D1~D4 待裁决）。

## Remaining

- 工具链解压结果确认（后台任务 `b6typ7239`）
- `survey` 会话创建（需红线⑤逐次授权：允许它启动五家软件并读本地数据）
- `.gitattributes`（repo 内 LF，防 `core.autocrlf=true` 造成全文件 diff 噪音）+ `.gitignore` 增 `.qoder/evidence/`
- Wave-1 契约①

## Blockers

- **B1** ADR-000 的 D1~D4 未裁决 → Wave-2 全部 worker 的边界推导不出来。解除条件：用户给 D1~D4 结论。
- **B2** 基线 commit 需红线②授权。解除条件：用户说"提交"。

## Coordination

- → `survey`：Wave-0 唯一可并行角色，不依赖领域模型文档，不受 B1 阻塞
- → `db`：Wave-1 契约①需要它的 `aggregate_version` 方案输入（该字段在 B 版文档里被压成一句话，属真缺口）
- → `audit`：需要提供"哪些字段算敏感"的规则集，供 db 的强制中间件消费
- → 用户：本文件顶部 NEEDS YOU 段是唯一的打断渠道

## Changed files

- `docs/adr/ADR-000-baseline-divergence.md` (新建) — 基线分叉报告与待裁决项
- `.qoder/progress/orchestration.md` (新建) — 本文件，主控账本
- `D:\DevTools\go\`, `D:\DevTools\node-v24.19.0-win-x64\` (解压目标，非仓库文件)
- `D:\DevTools\Installers\archives\` — 两个已校验的归档

## Verification evidence

- `git --version` → `2.52.0.windows.1`；`go/node/rustc/sqlite3 --version` → 全部 command not found；
  `gcc/cc/clang` → 全部 not found（含 `/mingw64/bin`）→ **CGO 不可用，SQLite 驱动锁 `modernc.org/sqlite`**
- `gh repo view SadCat18/Lucero --json visibility,isEmpty` → `{"visibility":"PRIVATE","isEmpty":false}`
- 逐文件比对：对 A/B 两侧 `tr -d '\r'` 归一后 sha1，结论记入 ADR-000 表格
- `winget search` → `GoLang.Go 1.27.0` / `OpenJS.NodeJS.LTS 24.19.0` / `SQLite.SQLite 3.53.4`；`df -h /d` → 164G 可用
- 工具链下载用 Python（git-bash `curl` 走 schannel 报 `CRYPT_E_REVOCATION_OFFLINE`，Python TLS 正常）；
  Go 的 sha256 对 `go.dev/dl?mode=json` 校验、Node 对官方 `SHASUMS256.txt` 校验，脚本 `.qoder/../gettool.py`（临时，不落仓库）
- 小工具链命令一律用绝对路径 `D:\DevTools\go\bin\go.exe`，**不改系统/用户 PATH**（红线③）

## Recovery note

- Interruption point: Wave-0 中段，工具链在后台解压，ADR-000 待裁决
- First action after recovery: 读本文件的 `Blockers` + 跑 `git -C /d/Project/Lucero status --short` 与
  `ls /d/DevTools/go/bin /d/DevTools/node-v24.19.0-win-x64`，确认解压结果后再建 `survey` 会话

## ⚠ NEEDS YOU

1. **ADR-000 D1~D4 裁决**（建议全按 A 版补回：docx 全文转 md 入库 + B 版编号对齐 A 版 + 补回 §55/§59/§33 三处丢失事实）
2. **红线⑤ 单次授权**：是否现在就让 `survey` 会话开始启动五家软件并读 `D:\opencode\data`、`D:\WorkBuddy`（每家 ≤3 条真实消息）
3. **红线② 授权**：是否允许我把 Wave-0 产物做一次本地 commit（不 push）

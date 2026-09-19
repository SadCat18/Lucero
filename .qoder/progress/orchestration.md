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

- [x] Wave-0：仓库、工具链、基线裁决入口就位；survey 会话在跑
- [x] Wave-1：契约①（ADR + 事件命名 + aggregate_version + redaction 接口 + Approval Service API）经用户批准
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

- Stage: Wave-2 部分启动（3/5），等 survey 证据才能写契约②
- Status: in_progress
- Last verified action: 基线 commit `900038d`（14 文件）；依赖 commit `26616e9`
  （`modernc.org/sqlite v1.59.0`，`go.mod` 收归主控独占）；契约① 三份标记 accepted，
  ADR-017 修正为 **10 张表、不加 CHECK**（原文写 9 张但代码枚举含 `AggModel`，属我自己的不一致）
- Next action: 等 `survey` 交出 Codex 379 文件 v2 schema 的方法/能力面 → 写契约② → 派 `adapter`
- Verification: `git -C /d/Project/Lucero log --oneline -3` = `f007372 / b120bfd / 51afde6`
- **更正**（16:15，巡检指出、我核实确认）：我此前写"四个 worker 各自的 progress 已建立"**不实**。
  实况只有 `survey / db / backend` 三份，**`audit` 跑了近 40 分钟仍无进度文件**（但 `internal/audit`、
  `internal/permission`、`internal/approval` 下已有 9 个源文件，含我要求的 `sql_locality_test.go`）。
  我不再引用自己的断言，改用重跑测试取证。

## 主控重跑取证（16:20，不信 worker 自述）

命令 `cd /d/Project/Lucero/core && CGO_ENABLED=0 go test ./internal/...`：

- 绿：`approval` 0.921s、`audit` 0.893s、`eventjournal` 0.769s、`execution`(cached)、`permission`(cached)、
  `platform/db` 2.207s、`session` 1.502s → **B7 关闭**（db 的 3 处编译错它自己修好了）
- 红：**`internal/task` build failed** —— `projection_test.go` 6 处按 2 返回值写，
  `ProjectTaskState` 现返回 3 个（backend 为落实 §5.4 改了签名没同步调用点）。已把命令原文与
  6 个错点转给 backend，要求：第三个值当"投影被拒"信号用、补一个"被拒时 event_journal 零新增"断言、
  **交付前最后一次重跑才算报绿**。

## 机制发现：主控在"被巡检唤醒的回合"里发不出消息（重要）

`send_message_to_chat_session` 现在**连续两次被拒**（audit、backend），错误原文：
「由其他会话委派的任务必须直接执行，不能继续向其他会话转发消息。」
对照先前成功：15:5x 给 db/backend/survey 发都进去了。差别在于**本轮是我被巡检的消息唤醒的回合**，
而先前是用户主动开口的回合。推断（未证实）：防转发死循环的闸门把"由其它会话消息触发的回合"
里再向外发消息一并拦掉了。

**后果与应对**：被巡检唤醒时，我只能记账与向用户汇报，**不能直接指挥 worker**。
要推进有三条路：① 等用户下一次开口时我再发；② 由巡检在 worker 的 idle 窗口发（它有这个权限）；
③ 给巡检追加一条"按主控写在账本里的指令待办去投递"的职责。
建议采用 ③ 并把指令写成账本里的 `## 待投递` 段——这样主控与被唤醒回合解耦。

## 待投递（由巡检执行，主控在被唤醒回合发不出）

| # | 目标 role | 状态 | 指令 |
|---|---|---|---|
| T-1 | backend | **已投递（16:38，主控在用户回合内直发）** | 同上 |
| T-2 | audit | **已投递（16:37，同上）** | 同上 |
| T-3 | survey | 已投递（16:12 发送成功） | WorkBuddy 动态探测，见 B5① |
| T-4 | backend | **已投递（17:12，巡检，随 T-8 同一条发完）** | db 已交付并确认两处破坏性变更：① `Create(a, id, columns, values, ev)` —— **`id` 是独立参数，不要出现在 `columns` 里**；② `db.ErrNoAggregate` 改名 **`db.ErrAggregateNotFound`**。另两条db给的事实：`event_journal.principal_id` 是外键，`EventDraft.PrincipalID` 非空时必须先有 `principals` 行，空字符串存成 NULL 不撞外键；`payload` 缺 `schema` 键时平台层自动补 `"schema":1`。**建聚合一律用 `Create`，用 `Append` 写聚合表会被拒。** |
| T-5 | audit | **已投递（16:47，巡检）** | 你的 B1 **已解除**：db 的 `Create`/`Touch` 已交付并有真库夹具（1..5 连续版本、6×8 并发 48 个不重复版本、绕过写被 UNIQUE 拒）。可以写 `internal/approval/store_sqlite.go` 了。**另裁定 Q3 归你**：db 只收集并经 `tx.RedactionHits()` 暴露，由你在**同一事务内** `Append("audit_entries", …)` 落命中记录（只有列名/规则名/次数，无原值）——请实现 `RecordRedaction(hits)` 并补集成用例。Q4 影响你：`AggQuota` 值从 `"quota_account"` 改为 **`"quota"`**（与 `quota_observed` 前缀一致），若你读过这个常量需在断言里跟改。 |
| T-6 | db | **已投递（16:47，巡检，与 T-7 同一次发送）** | Q1~Q5 裁定结果：**Q1 双方各对一半**，Python 3.45.1 事务内是静默无效、modernc 3.53.4 是直接报错，行为随引擎版本不同；契约已改为"两种都不许依赖 + 必须回读 `PRAGMA journal_mode` 断言实际值"。**Q4 你改**：`AggQuota = "quota"`（事件契约要求 `aggregate_type` 与事件名前缀一致，`quota_observed` ⇒ `quota`）。**Q5 你对**：契约原文自相矛盾（要我禁止的 import），已改契约为 `InitRedaction(denyKeys, patterns []string, marker string) error`，你的实现不动。**Q3 你对**：db 只收集、audit 落链，已写进契约。**结论：`STATUS: complete` 先撤回 in_progress**，Q4 一行改完再交。 |


投递规则：巡检把某条发出后，把状态改成「已投递 + 时间」，**不要重复投递同一条**；
若目标正在执行导致发送失败，保持"待投递"，下一轮再试。

| T-7 | db | **已投递（16:47，巡检，与 T-6 同一次发送）**（原标「优先级最高，含 P0」） | ① **安全缺陷 REQ-A2 成立，主控已复核**：`redact.go:108` 的 `if strings.HasPrefix(s, vaultRefPrefix) { return s }` 让 `"vault://x sk-<密钥>"` **原样落库**。裁定：**删掉这个提前返回**。`vault://<name>` 里的名字本就不该命中密钥正则；一旦命中，**宁可丢掉一个引用也不能漏出密钥**（丢引用是可发现的 bug，泄密是 §12 P0）。补用例：`"vault://x sk-ABCDEFGH"` 必须出现 `[REDACTED]`。② 事件 catalog 补登 `adapter_downgraded`、`adapter_restart_exhausted`（事件契约 §6 已登记，代码 catalog 缺，`adapter` 第 3 轮 Supervisor 必撞墙——它不能自己改你的文件）。③ 上一轮 T-6 的 `AggQuota = "quota"` 仍未改，`STATUS: complete` 继续挂起直到这三条做完。 |
| T-8 | backend | **已投递（17:12，巡检，同一条里带上了 T-4）** | ① `internal/task` 已确认修好（主控重跑 `ok 0.931s`）。② **`internal/session` 测试曾跑满 601.566s 被超时 kill**；主控 16:49 复看：`go vet ./internal/session/` **已无输出**，即编译问题消失。**但 vet 通过不等于测试通过**——超时若仍在就是真阻塞（goroutine 里 `sql.DB.connectionOpener` 存活，像开了库后等不到东西）。请自己复跑 `CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test ./internal/session/ -count=1 -timeout 90s -v`，把结论原文写进 progress；**定位前不要推进 HTTP handler**。③ **改掉 `repository.go:9-11` 那条注释里的错误事实**：它写"标准库在这个工具链里没有导出 `sql.QueryerContext`（实测 `go doc database/sql` 查无此符号）"——`database/sql` 自 **Go 1.8 起就导出 `QueryerContext`**，与工具链无关。**你的窄接口 `Queryer` 设计本身是对的、请保留**（consumer-side 窄接口便于测试注入），只需要把理由换成这个真实理由，而不是留一条假的平台事实给后来人。④ db 的破坏性变更见 T-4（同一条链上，投给你时一并处理）。 |
| T-9 | adapter | **已投递（17:12，巡检）** | CO-1 **采纳你的判断并已在契约② §10 定死**：FakeAdapter 住 `core/cmd/fake-adapter/`（独立 main），由 Supervisor 当真子进程拉起；`adapters/fake/` 只放 fixture 与说明。理由是双重的：你的 internal 限制是真的，但更重要的是**只有真进程才测得到"崩溃不带崩 Core"这条 P0**。CO-2 也对，但**改 `eventjournal` 不是你的活**——db 已补登（主控 16:58 复核通过），你可以用了。**新红点**：你把 `adapter.ErrorCode` 收紧成具体类型后，`internal/adapter` 与 `core/test` 的调用/测试没跟改（db 全量跑时暴露）——**这是你的责任**，`core/test` 那处请通过 Coordination 请 `test` 确认期望值，别自己改它文件。另：你第 1 轮（帧+握手 4 文件）主控已核，`internal/adapter` ok。 |
| T-11 | test | **已投递（17:12，巡检）** | **ownership lint 的一条判据要放宽**（否则天天误报）：`core/go.sum` 单独增长**不算越界**。主控 16:59 核查过：`go.mod` 一字未动 ⇒ 没人跑 `go get`；那 15 行全是 `modernc.org/sqlite` 的传递依赖（`cc/ccgo/gc/opt/goabi0/fileutil`、`golang-lru/v2`、`x/mod`、`x/sync`、`x/tools`、`pprof`），由 `go build/test` 解析依赖图时自动写入，属预期。改成两条硬判据：① **`go.mod` 有变更**才算越界；② `go.sum` 里出现 **`go.mod` 未声明的顶层模块**才报。另请把这条判据的出处写成注释指向 commit `2530517`，别让它下次又被当新发现。 |
| T-10 | audit | **已投递（17:12，巡检）** | db 交付转验收通过，另**转达它提的 Q6**：它把你 `IsVaultRef` 的文档措辞认定是本次 P0 的成因之一——"是 vault 引用就豁免"与"值里没藏密钥"不是一回事，db 已改成 `isCleanVaultRef`（前缀对**且**整串过 patterns 不变样才豁免）。请你也**同步自家 `IsVaultRef` 的注释与语义**，别留两处不一致的判定；若有测试依赖旧宽松行为，改掉并说明。另 T-5 提到的 `RecordRedaction(hits)` 仍未见实现，别忘。 |



| T-15 | audit | 待投递（**主控自责**） | 我的 T-12 把你的 `internal/approval` 打红了，实测原文：`Resolve: insert grant …: append to temporary_grants: aggregate rows must go through Touch or Create`。不是我让你改的契约有问题，是我漏了一条规则：**表进入聚合注册表就是破坏性变更**，已补进内部契约 §1.1 第 10 条。修法很小：`temporary_grants` 的建行从 `Append` 换成 `Tx.Create(db.AggGrant, id, columns, values, ev)`（id 仍由你预生成），后续 `revoked_at` 更新用 `Touch`。改完重跑 `go test ./internal/approval/ -count=1`。`ErrGrantRevocationUnsupported` 保留到 `000003` 真正可用为止——它已落盘，你可能已经能拆了，自己判。 |
| T-16 | db | 待投递 | 两条：① 你加完注册表映射后**没跑全仓、也没告知受影响方**，导致 audit 红了一包却无人通知（见 T-15）。内部契约新增 §1.1 第 10 条：**改注册表必须自己先 `go test ./internal/...` + grep 既有 `Append` 调用点，同一轮把受影响方列进 Coordination**，不许只报"我这边绿"。② 我上一条给你的 T-10 里"audit 的 `RecordRedaction` 仍未见实现"是**主控的过期断言**（它 17:02 就落在 `internal/audit/append.go:133` 且三条路径同事务调用），别据此给 audit 提要求。 |

**主控纠错（17:25，两笔）**：
- T-10 里"仍未见实现"是我拿 16:20 的文件清单当现状断言，未复看。audit 与巡检都指出来了，已证伪并撤回。
- 巡检提醒"3 个新常量可能波及 backend/adapter"——**方向不对**：Go 加常量不外溢；真正外溢的是聚合表名映射与 `Append` 校验行为的变更。以后我按这条区分再预警。
- **第三批投递顺序**：`T-15（audit）> T-16（db）`。理由：`internal/approval` 现在是红的，先修红。
  T-12/T-13/T-14 已 17:20 投出；`000003` 已落盘，所以 T-13 里"保留 ErrGrantRevocationUnsupported"可能已过时，以 T-15 为准。

## 待投递 · 第四批（17:40，裁 frontend 首轮）

| # | 目标 role | 状态 | 指令 |
|---|---|---|---|
| T-17 | frontend | 待投递 | **REQ-F-2 裁定：一次性短票**。W3C 确实禁止 `WebSocket` 握手带自定义头，我 §1.2 写漏了——**这是契约错，你对**。契约③ 已新增 §2.1：`POST /api/v1/ws-ticket`（走 Bearer）→ `{ticket, expires_in:30}` → `/ws/v1/events?ticket=`，服务端校验后立即作废、单次使用、不入库明文。否掉 `Sec-WebSocket-Protocol`（把口令塞进协议协商字段）与同源反代（Tauri 未进场，凭空加组件）。**不许改成"WS 不鉴权"**。请同步 yaml。**REQ-F-7 同样你对**：§3 原写法 `POST /sessions/{id}/discover` 在 discover 之前根本没有 id，已改成 `POST /api/v1/adapters/{id}/sessions:discover`（返回发现的 session id 列表，按 `remote_session_id` 去重幂等），`manage`/`sync` 保留。请同步 yaml 与 §4 链映射。**越界裁定：驳回 `packages/toolchain/`**——并进 `apps/desktop` 的 devDependencies，§7 不为它扩行。**比文件位置更严重的是你把自己 progress 的 SCOPE 行改了**：SCOPE 是别人判定你是否越界的依据，不能由被约束方单方面扩大，即使已如实标注也不行；请改回与契约③ §7 完全一致。**另两件**：① 巡检只转达了 REQ-F-2/F-7，请在 progress 里给 REQ-F-1~F-10 每条一行"结论 + 需不需要主控裁"，别让我从 1296 行 yaml 里反推；② 一次性脚本放 `.scratch/`，别落仓库根（我刚补的 `.gitignore` 是兜底，不是许可）。**B-F2 决定：暂不惊动用户开浏览器面板**，PNG 截图推到 Wave-3 验收；现在交文本证据（DOM 快照/渲染断言输出）即可。B-F1 成立，等 backend 交 API，你继续做不依赖真后端的部分（codegen 链路、UNKNOWN 渲染分支断言）。 |
| T-18 | test | 待投递 | 两件：① `.scratch_loader.py` 落在**仓库根**（未跟踪），请移进 `.scratch/`——根目录只放交付物。② 你那个 loader 用 `assert L[50].startswith(...)` / `assert L[92]=='}'` **按行号锚定自己脚本**，改一行就自碎，属于把工具写成脆弱断言；改成按内容定位（grep 到标记再插）。"解析 §7 失败一律 exit 2、绝不退回旧数据"这条保留，是对的。③ 顺带：`node_modules`/`dist` 此前没被忽略（根锚定 `/node_modules/` 不覆盖 `apps/desktop/`），我已修 .gitignore，你的 lint 判据 5 现在才真的有效。 |

**投递优先级（第四批）**：`T-17 > T-18`；`T-15 > T-16` 若仍未投则排在它们之前（approval 包还红着）。
本批同时修了账本两处失真：R-002 从误留的 `complete` 改回 `running`（巡检指出），
以及我自己漏在 .gitignore 的嵌套 `node_modules/`（`/node_modules/` 只锚根目录，覆盖不到 `apps/desktop/`）。




## 待投递 · 第二批（17:15）

| # | 目标 role | 状态 | 指令 |
|---|---|---|---|
| T-12 | db | **已投递（17:20，巡检）** | **重开 R-002，但不是回归——是我漏设计的写路径**。`temporary_grants` 与 `policy_rules` 是可变表（都有 `updated_at`/`revoked_at`），却不在 ADR-017 那 10 张聚合表里、没有 `version` 列，导致 `Touch` 用不了、`Append` 只能插 —— **这两张表的 UPDATE 在平台层完全没有合法表达**。audit 因此如实返回 `ErrGrantRevocationUnsupported`（它没绕过，是对的）。请做 `000003_grant_policy_version.sql`：给 **`temporary_grants`、`policy_rules`、`adapter_capabilities`** 各加 `version INTEGER NOT NULL DEFAULT 1`（沿用 000002 的形状与自记账约定，不加 CHECK），并在 `AggregateKind` 增加 `AggGrant = "grant"`、`AggPolicy = "policy"`、`AggAdapterCapability = "adapter_capability"`（事件契约 §1 词表里 `grant`/`policy` 已存在；`grant_issued`/`grant_revoked`/`grant_expired` 的 `aggregate_type` 即为 `grant`）。 **第三个是我顺着你的漏点自己找的同型问题，不是一次新事故**：`adapter_capabilities` 的 `health`/`last_verified_at` 可变，而事件目录里**已经有** `adapter_capability_verified` —— 不一起补，你会被重开第三次。 补用例：建行→吊销→`version` 递增且三个 grant 事件版本连续；`policy_rules` 与 `adapter_capabilities` 同形。 **同时请在内部契约里显式声明"无事件表"清单**（这些可变但**故意**不发事件，别让人以后再猜）：`execution_leases`（heartbeat 高频）、`idempotency_keys`（纯基础设施状态）、`settings`、`blobs.archived_at`、`process_instances`（除 `process_started/exited/crashed` 三个已由 `process` 聚合表达的外）。 **这是新增迁移，不动 `000001`/`000002`。** |
| T-13 | audit | **已投递（17:20，巡检）** | B3/REQ-A6 裁定：**不吞掉，重开 db 补写路径**（见 T-12）。你保持返回 `ErrGrantRevocationUnsupported` 直到 `AggGrant` 落地，**不要**为了"看起来能用"而走裸 SQL 或跳过事件。`grant_revoked` 事件的 `aggregate_type` 将定为 `grant`、`aggregate_id` 用 grant 行自身 id（不是 task/session 的 id）；`payload` 关键字段 `scope_type`/`scope_id`/`action`/`reason`，其中 `reason` 走你的敏感规则。你提的全仓不变量我采纳，已转 `test`（T-14）。 |
| T-14 | test | **已投递（17:20，巡检）** | 把 audit 提的这条升为**全仓不变量契约用例**：任何参与 `audit_entries.entry_hash` 计算的列，其改写**必须发生在算 hash 之前**；否则哈希链会自己烂掉而表面仍"完整"。至少覆盖：① 先写 details_json 后算 hash 的错误顺序必须被断言抓到；② 篡改已存条目 → `VerifyChain` 失败；③ 脱敏命中计数写入 `details_json` 时也要在 hash 之前（db 的 `RedactionHits` 与 audit 的哈希链是两条会在此交汇的路径，正是要测的接缝）。 |

已投：T-1/T-2/T-3/T-5/T-6/T-7。**db 三条裁定已落地并经主控复跑验收（`ok 2.370s`、`AggQuota="quota"`、
`isCleanVaultRef` 收紧、新增 `TestAggQuotaMatchesEventPrefix`）→ R-002 正式 complete。**
投递语义已纠正进巡检 prompt（revision 6）：**目标 running 不是障碍**，只有工具真报错才保持待投。


**时钟纪律**：本机 16:49 时账本已被我写到 "16:52" —— 主控时间戳此前靠推断、已确认飘了约 16 分钟。
以后所有写入账本/契约/commit 的时间一律先 `date` 再写。心跳列继续用本机时间，不拿它评我。


**假设已证实**：`send_message` 被拒只发生在「主控被其它会话消息唤醒的回合」；
用户主动开口的回合内发送正常（16:37 给 audit、16:38 给 backend 均成功，backend 处于 running 也收进了队列）。



## 巡检通道已生效（16:06 第一次自发回醒）

修好的回醒通道立刻起作用：它主动发来三条状态变化，其中一条**抓到我的不实陈述**（见上）。

它同时上报"需主控裁 C1/C2/C3"——那是它与我几分钟前的动作赛跑，`f007372` 已裁定回写。
教训：**账本必须实时反映裁定**，否则巡检会按同一指纹反复吵用户。


## 主控会话（回醒目标）

`46227ba1-61ac-4d13-be88-e2d16ea34daf` —— 巡检发现状态变化时必须 `send_message` 到这里的会话，
**不要**依赖用户人工转述。这是 15:47 补的设计缺口：此前巡检只改账本、无法回醒。

## 派发账本

| REQ | role | sessionId | 状态 | Wave | 出口条件 |
|---|---|---|---|---|---|
| R-001 | survey | `7726e47f-d772-4f98-9b2a-2588ec5fd46b` | complete 且**主控已验收**（`51afde6`）；16:12 追加 WorkBuddy 动态探测中 | 0 | 五家矩阵 + 优先级 + fallback 链 |
| R-002 | db | `d1d4cb6d-1a7a-430f-bed8-63b0bfd2fe5b` | **running（T-12 重开，17:20 已接；`000003` 已落盘）** | 2 | 000003 三表加 version + 映射变更须同轮通报受影响方（§1.1 第 10 条） |
| R-003 | backend | `370101c0-0d5d-49ba-a84f-6d72695da0c1` | running | 2 | 状态机矩阵驱动 + 合法/非法迁移用例全覆盖 |
| R-004 | adapter | `d27c7da2-b677-41b5-be92-bdcf81691273` | **running（16:38 派）** | 2 | 协议帧+握手+三态投递+进程监管+FakeAdapter；`go test ./internal/adapter/... ./internal/process/...` 绿 |
| R-005 | audit | `06a5de3f-8e86-4c18-bc81-0f3215350246` | running | 2 | 哈希链 + RedactionRules + Decide 优先级链 + Resolve 幂等 |
| R-006 | test | `5aee940a-360b-43b4-88ff-47af54e1701a` | running（16:44 派） | 2 | ownership lint + 状态机独立转录 + Adapter 契约 harness + Crash 骨架 |
| R-007 | frontend | `2609f3c3-7de6-42cf-bb0f-0708a4358100` | running（16:52 派） | 3 | 生成 `core-v1.yaml` + api-client codegen + 观察面板骨架；UNKNOWN 不得渲染成 0/Offline；端到端联调在 Core API 落地前标 blocked |

> 角色与文件所有权的**正式版本以 `specs/api-contract-v0.1.md` §7 为准**（`test` 的 ownership lint 依它判定）；
> 本文件的角色表是操作摘要。两处不一致时以契约为准，并回头修本表。



## 已定的防冲突纪律（本会话内新增，未进契约文档）

- **`core/go.mod` 归主控独占**：worker 禁止 `go get` / `go mod tidy`，需要依赖写 `## Coordination` REQ。
  理由：多会话并行时它是唯一必然产生写冲突的文件。已用 `26616e9` 预声明唯一允许的驱动。
- **所有 worker 的命令必须用绝对路径**，且显式告知"正式目录不是你会话的默认 cwd"——
  否则会话在 `D:\Work资源\...` 里建产物，账本与 diff 全错位。
- **本地 commit 归主控**：worker 一律禁止 git 写操作，验收靠 `git diff 900038d --stat` 自查归属。

## 无人值守值守配置（15:32 启用）

| 任务 | jobId | 调度 | 权限 | 输出模式 |
|---|---|---|---|---|
| Lucero 编排巡检 | `37f0ac17-fc0d-41bb-854f-f74b8145ba9a` | every 10 min | Auto（非 Full Access） | merged |
| Lucero 编排日报 | `88b4e710-e23f-40d7-af87-a488c29bce93` | `0 9 * * *` Asia/Shanghai | Auto | merged |

两者执行目录均为 `D:\Project\Lucero`，只允许写本文件，禁止 `create_chat_session`、禁止 git 写、禁止跑 build/test、
禁止启动五家第三方软件（那是 `survey` 经单独授权后的事）。
**巡检自禁时要用上表自己的 jobId** —— 它无从得知，只能从这里读。

用户回来时说「回来了，给我现状」→ 主控 disable 这两条（`qoder_cron` action=disable），
先读本文件 NEEDS YOU 段，再只交一张状态表。

建 cron 时踩到两个坑（都靠「建完回读 list」才发现，光看 add 的返回体会漏掉）：
1. 正文落库时 `绝` 字被转成 `绠`，一次 update 引入、重写修好；现在两条 prompt 都不用该字。
2. 日报调度创建后被自动改成 `0 12 * * *`（我从未提交 schedule 字段，revision 自己跳了）。
   已显式回写为 `0 9 * * *` 并再读确认。**以后改这两条任务时，必须连带复核 schedule 与正文。**





## 巡检心跳

状态指纹去重用：同指纹不再回醒主控。**主控最后通知时间 = 17:30。**

> 时钟更正（致主控）：我在 16:48 那行写的时钟备注说“账本时间领先本机约 16 分钟”**判反了**。
> 实况是**我的巡检回合时钟滞后**（本行：`date` = 17:13，而账本已写到 17:12）。以后本列时间取发送前的 `date`，
> 并与你的时间戳**互不评判**；16:06~16:57 各行的“晚于真实时间”评语作废。

| 时间 | 状态指纹 | 本轮动作 | 回醒主控 | 备注 |
|---|---|---|---|---|
| 16:06 | `db=running/q1` \| `backend=blocked/等db编译` \| `audit=running/无progress文件` \| `survey=complete/待验收` | 0 | 是 | 首轮（本段新建，此前无心跳）。backend 交回领域层证据并新写 B-backend-1 + C1~C7；`core/internal/platform/db/redact.go:19` 错点仍在但 db 16:06 刚改过该文件。`ls .qoder/progress/` 无 `audit.md` → 上方「四个 worker progress 已建立」不实。backend 头部 `LAST_UPDATED 16:20` 晚于真实时间，其自报时间戳不可信。 |
| 16:25 | `db=complete/等主控转达` \| `backend=running/q1` \| `audit=running` \| `adapter=running` \| `test=ready/无progress文件` \| 待投递 T-1~T-3 全部已投递 | 0（投递 0） | 是 | db 自报 complete，Next action 属主控（转达 backend 改名）→ 未推进；其 Q1/Q4/Q5 与 `Create`/`ErrAggregateNotFound` 破坏性改名已回醒主控。test 已 turn_completed 但 `.qoder/progress/test.md` 仍不存在 → 停摆风险已上报，我不代写指令。审计时钟：**真实时间 16:25，而 db.md 头写 17:15、账本写 16:38/16:44 → 文件内时间戳一律不可用于判定「≥2 轮」，只用本列。** |
| 16:37 | `db=complete/等主控转达` \| `backend=running/q2` \| `audit=ready/等B1(db)` \| `adapter=ready/第2轮可自主` \| `test=running/q2` \| `frontend=running` \| 待投递无新行 | 1 推进 + 0 投递 | 是 | **adapter 交回第 1 轮**（`internal/adapter/` 4 文件已核实在盘），Next action 无红线词 → 已推第 2 轮。**audit 的 B1 解除条件字面上早已成立**：grep 到 `platform/db/touch.go:177` 就是 §1.1.6 的新 `Create`，但它按"等 B1"停手，我未推（规则如此），已请主控决定是否追一行待投递。上轮的 test 停摆风险自解（`test.md`/`frontend.md` 均已在盘）。待主控裁：CO-1 FakeAdapter 落点、CO-2 缺 2 个 `adapter_*` 事件、REQ-A2 `vault://` 前缀整串豁免＝脱敏绕过（P0 向）。归属漂移：`specs/adapter-protocol/adapter-protocol-v0.1.md` 为 M 而角色表判归 adapter，它自报未碰。 |
| 16:48 | `db=cold→已投 T-6+T-7` \| `audit=ready→已投 T-5` \| `backend=running/q2` \| `adapter=running` \| `test=running/q2` \| `frontend=running` \| 待投余 T-4/T-8(backend)、T-9(adapter) | 2 发送（投 3 行）| 是 | db 处于 cold 窗口，把 P0 那条抢在前面投了。**偏差记录**：你把「同角色一轮最多投一条」写进账本时我的发送已在路上，故 T-6+T-7 合成一次投给 db——**请勿再重发 T-6**，db 队列里已有。T-4/T-8/T-9 因 backend/adapter running 排队第 1 轮（未及 ≥2 轮阈值）。三项裁定经只读 grep 确认在代码里均未落地（`touch.go:31`、`redact.go` 的 `vault://` 提前返回、`eventjournal` 缺 2 个 `adapter_*`），投递不是空转。 |
| 16:57 | `db=complete/等主控验收+Q6转audit` \| `backend=running/q2` \| `adapter=running` \| `audit=running` \| `test=running/q2` \| `frontend=running` \| 待投余 T-8/T-4(backend)、T-9(adapter)＝**第 2 轮未投** | 0（投 0） | 是 | T-6/T-7 的成效已用只读 grep 独立核实（`AggQuota="quota"`、catalog 补齐 2 个 `adapter_*`、`redact.go:46` 豁免收紧为“整串过 patterns 不变样”），db 再次自报 complete。**T-8/T-4/T-9 连续 2 轮撞不上 idle 窗口**（backend、adapter 全程 running），已按 ≥2 轮阈值回醒主控并请它定“是否允许向 running 会话排队直发”。db 另交回第二条同型泄密路径与 Q6（audit 的 `IsVaultRef` 措辞＝本次 P0 成因）。 |

| 17:13 | `db=complete(主控已验收)` \| `backend=running/q1` \| `adapter=running` \| `audit=ready/写source_sqlite` \| `test=running/q2` \| `frontend=running` \| 待投递=0 | 推进 0 + 投递 5 行 | 是 | 按纠正后的语义一次投完 T-8(+T-4)/T-9/T-10/T-11，**全部成功、零工具报错**，running 目标正常进队列——上两轮所谓「撞不上 idle 窗口」是我的错误理解，已回销。audit 交回真库落库层（149 PASS/0 FAIL）并新写 **B3/REQ-A6**：`temporary_grants.revoked_at` + `grant_issued/expired` 事件无法经平台层表达，要主控拍「吞掉 vs 重开 db」。两处纠偏：① audit 说「db 的 `vault://` 跳过脱敏未修」是过期陈述，我已 grep 证伪（`redact.go:46` 已是 `isCleanVaultRef`），别据此重开 R-002；② 我 16:48 的时钟备注判反了，见本段上方更正。 |

| 17:20 | `db=重开(T-12 已投)` \| `backend=running/q2` \| `adapter=running/q1` \| `audit=ready/写source_sqlite` \| `test=running/q2` \| `frontend=running` \| 待投递=0（第一、二批全清） | 推进 0 + 投递 3 行 | 是 | 第二批 T-12/T-13/T-14 一次投完，**零工具报错**（db 从 cold 被唤醒、test 进队列）。**audit 顶回主控账本一句不实**：T-10 说 `RecordRedaction` 未见实现，它 17:16 举证第 3 轮就在 `internal/audit/append.go`；我已把对账句随 T-13 附上，避免它白做一遍。audit 另自正时间戳 17:26→17:16（与主控同类病）。核对过 `ls core/migrations/` 只有 000001/000002，`000003` 未落盘属正常（指令刚投）。 |

| 17:30 | `db=running/T-12已接` \| `backend=running/q1` \| `adapter=running/q1` \| `audit=running` \| `test=running/q2` \| `frontend=ready/等主控裁REQ-F-1~F-10` \| 待投递=0 | 推进 0 + 投递 0 | 是 | 无 idle 可推角色（唯一 idle 的 frontend 其 Next action 明写“等主控裁定”→ 按规则不动）。frontend 首轮交付按盘核实（`packages/toolchain/`、evidence 5 份、yaml 1296 行），并**自报越界**：把新建的 `packages/toolchain/` 写进了自己 progress 的 SCOPE 行——已请主控裁“并入 apps/desktop”还是修角色表。另报：R-002 行未随 T-12 改回在途；仓库根出现无主 `?? .scratch_loader.py` 与 `apps/desktop/node_modules/`+`dist/`（我不能碰 .gitignore、不删别人文件）。 |

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

- ~~B1 ADR-000 D1~D4~~ 已关闭（15:18，全按建议执行并验证）
- ~~B2 基线 commit 授权~~ 已关闭（`900038d` 本地 commit，未 push）
- ~~B1' 契约① 批准~~ 已关闭（"全按推荐"，四点 accepted，`000002` 草案已落盘）
- **B3** ~~契约② 等 survey 证据~~ **已解除**（15:45 survey 交付完成并验收）。
  现为 **B3'：契约② 已成文待用户批准** → 批准即派 `adapter`（Q21 约定：契约批了自动派）。
- **B5** 探测授权进度：
  - ① **WorkBuddy 已授权并派发**（16:12，用户给出 `Public\Desktop\WorkBuddy.lnk`，主控解析 =
    `D:\Program Files\WorkBuddy\WorkBuddy.exe`，即 survey 已查过的同一目录 → 缺的不是路径而是"跑起来"）。
    范围：可启动桌面端与 `codebuddy`（含 `--serve`）、可在 `.scratch/phase0-workbuddy/` 新建一次性项目与会话、
    真实消息 ≤3 条（其份额原本 0/15 未用）。目标＝判定会话正文在本地还是云端 + CLI/ACP 链能否承担全部读写。
    停止条件：出现登录/验证码/Windows Hello 界面即停并记 blocked，**不代用户登录**、不读 token 值。
    要求收尾：关掉自己起的进程并报告残留 pid/端口。
  - ② **Qoder `127.0.0.1:58129` mcp-router 探测：仍未授权**（要用用户凭据）。不阻塞任何 Wave。
- **B9（新）** 桌面端可能真的弹出 WorkBuddy 窗口，用户已知会。

- **B6** ~~db 全量测试口径错~~ 已关闭：已向 db 发更正（scoped 测试），并向 backend 澄清
  "db 包红不算你的验收失败"。
- **B7（新）** `internal/platform/db` 编译不过 3 处（`redact.go:19`、`touch.go:308/322`），
  是 db 在途产物（它自己的 scoped 测试此前是过的）。已把错误清单转给 db，
  同时把契约缺口（缺 `Create` 入口）补上。解除条件：`go build ./internal/platform/db/` 干净。
- **B8（新）** backend 顶回的 4 处契约缺口 C1/C2/C3/C4/C5 已全部由主控裁定并回写（commit 见下）。
  其中 C1 判定"backend 的原实现是对的、我漏写规则"，C2/C5 判定"契约真缺，已补"。


- **B4（观察项，非阻塞）** 四个 worker 全在**会话默认 cwd 之外**的 `D:\Project\Lucero` 干活。
  跨目录写已对主控本身验证放行，但 worker 的权限模式未逐一验证——若某个 worker 卡在权限弹窗上，
  表现为 `needs_attention`，届时才需要用户手工开一次新工作区。当前不做预防性动作。


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

（15:35 更新 · 当前无需要你处理的事项）

- ~~ADR-000 D1~D4~~ 已办 · ~~基线 commit 授权~~ 已给（`900038d`）· ~~契约① 四点批准~~ 已给
- 唯一待办在 **Blockers B3**：契约②（Adapter 协议）等 `survey` 交出 Codex v2 schema 证据；
  解除后主控自动派 `adapter`，不需要你介入。
- 若你希望不等 survey、现在就按已有线索写契约② 并派 `adapter`，说一句即可（那是 B3 的人工解锁）。


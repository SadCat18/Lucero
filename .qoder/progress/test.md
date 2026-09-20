OWNER_ROLE: test
TASK_ID: wave2-tests
STATUS: in_progress
SCOPE: Wave-2 三类测试（ownership lint / 状态机矩阵 / Adapter 契约 harness）+ Crash 骨架；对业务代码只读
EVIDENCE_DIR: .qoder/evidence/test/
LAST_UPDATED: 2026-09-20T12:13+08:00

## Objective

用**独立于实现方**的测试与可执行 lint，证明 Wave-2 垂直链符合事件契约与 Adapter 协议，
并把角色文件所有权越界变成一条命令就能查出来的东西。

## Completion criteria

- [x] T-A `scripts/check_ownership.sh`：7 条检查逐条有输出、违反非零退出、带 `--selftest` 证明扫描器不是永真式
- [x] T-B 状态机矩阵：契约 §5.1/§5.2 由本角色独立转录，与 task/execution 实现双向对账 + §5.3/§5.4 投影
- [x] T-C Adapter 契约 harness：表驱动、不绑实现（§2/§2.2/§3/§5/§7/§9 + CO-4 线格式），进程内桩 + **真子进程第二被测面**
- [~] T-D Crash 骨架：§7 七条逐条落名；2 条今天真跑，5 条 `t.Skip` 并点名依赖方
- [x] 验收命令最后一次重跑全绿（见 Verification evidence 的时间戳，报绿只写最后一次）
- [x] 权威源 = 契约③ §7 且**运行时解析**，脚本内不保留副本；解析规则 <12 条即 exit 2
- [x] 「主控自己的提交不计 worker 越界」：未提交集合判越界，已提交集合只计分布
- [x] 契约③ §6.3 三处同步（check_7）：yaml 缺席只标记、落地即自动生效
- [x] T-11：check_4 放宽为两条硬判据（go.mod 未提交变更 / go.sum 出现模块图外的依赖），注释指向 commit `2530517`。
  **17:5x 巡检二次投递 → 按盘核实已办结。**与原文一处口径差异已报备：② 用 `go list -m all` 的**模块图**判，
  不是字面的"go.mod 未声明的顶层模块"——传递依赖本身就是另起的模块（`modernc.org/libc` 不在
  `modernc.org/sqlite` 路径下），按字面判会把那 15 行自动补全全报成越界，等于原地抵消这条放宽；
  塞新依赖必然改 go.mod，仍被 ① 抓住。要字面版说一声，预期效果是天天红。
- [x] **T-21**：`TestProjectionRejectedJournalAndAudit` 的断言口径按 §5.4 更正（不再写"event_journal 零新增"，
  改为 task 零新增版本 + 零 task_* 事件 + 恰一行 notification_created + Audit 走链不进 journal）
- [x] **T-24**：check_2 从"绑 `tx.Append(` 字面量"改成**按表名遍历**（`cross_package_refs` 抽成函数可用合成件验），
  四桶：生产写=红 / `const t = "表名"` 变量传表名源头=红 / `_test.go` fixture=汇总 / 只读引用=汇总
- [x] T-14：`core/test/audit_chain_test.go` 三条（13 列逐个篡改 / hash 后改 details_json 必被抓 / 脱敏计数在 Seal 之前）。
  **17:5x 二次投递 → 按盘核实已办结**，并把 ③ 加固成真·接缝测试（见 REQ-T3）
- [x] T-18：仓库根临时件已删；"按行号锚定自己脚本"的批评认账——本轮两次踩实（一次拼接把文件头删了、
  一次把排除式写宽到全部命中），已改为按内容标记定位并要求替换后 selftest 必须重跑

## Scope boundaries

- In scope: `core/test/**`、`scripts/**`、本文件、`.qoder/evidence/test/`
- Out of scope: `core/internal/**` 与 `core/migrations/**` 的任何修改；在别人包里建 `_test.go`；
  `specs/**`、`docs/**`、`core/go.mod`/`go.sum`、`.gitignore`；git 写操作；装软件/改 PATH；五家真实软件

## Current checkpoint

- Stage: T-29 已落成硬规范并自审过全包；12:13 验收 22 PASS / 10 SKIP / 0 FAIL。
  同轮复核 T-27：②③ 已落（db 带 Kind、mapper 不再切 `key:` 前缀），REQ-T3 只剩 ①。T-D 里两条已从骨架变**真跑**（真启 Core、真硬杀、真重启）；
  REQ-T5 闭环（§7 补行后 lint 不再报）；REQ-T3 我这边形状已跟上新裁定，另一半仍是别人的活。
  11:59 验收：go test 22 PASS / 10 SKIP / 0 FAIL；lint exit 1（4 条，全是新发现的 `.qoder/scratch/*.py`）
- Status: in_progress
- Last verified action: 上述四条命令 11:59 一次性重跑；`core/cmd/core` 真启动真被杀真重启
- Next action: 把 REQ-T7（core_ready 未进 journal）与 REQ-T8（`.qoder/scratch/` 无归属且未被忽略）
  交主控转派；backend/adapter 任一落地后第一件事是重跑对应 Skip 用例并按需改成硬断言
- Verification: `.qoder/evidence/test/{go-test,ownership-lint,selftest}-final.txt`

## Completed

- **12:0x~12:13 轮（T-29）**：
  - T-29 的两条规范写进 `core/test/statemachine_test.go` 的包文档（全包共用一处，不复制多份）：
    变异必须打在**待验层的原始输出**上（返回值 / 它直接交给下游的那一行数据），
    禁止用下游消费者的判断给上游发绿。
  - 按这条规范给 `TestRedactionCountersAreSealedBeforeHash` 加了**自破坏探针**：把 audit 刚交给
    Appender 的那一行拿回来，在 hash 之后改 `details_json`，再让同一条判据去查 →
    必须同时看到"hash 不等"与"`VerifyChain` 拒"。这条探针读的是那一行本身，不借任何下游判断。
  - 全包自审结论（逐处核过断言对象）：状态机/投影打 `Check`/`ProjectTaskState` 返回值；
    链打 `Seal` 返回值与 Appender 收到的那一行；lint 打扫描器自己的输出（合成件）；
    Crash 打库里的行数与 health 响应体。没有"拿上一层验下一层"的残留。
  - **REQ-T3 复核后收窄**：`db.RedactionHit` 已带 `Kind HitKind`（redact.go:87、97-101），
    approval 的 mapper 改成 `audit.HitKind(h.Kind)` 直传、`CutPrefix("key:")` 已消失；
    Skip 理由相应改成只剩 ①`RedactionDetails` 不校验空 Kind（REQ-T3c 给 audit）。
  - **又一处我自己的假红，当场纠正**：`TestErrorCodeIsJSONRPCNumber` 用 `"code":"` 子串判线格式，
    而 adapter 把符号名合规地放在 `error.data.code` 里 → 我的断言误伤。删掉子串判断，
    保留 `json.Number` 解码那条结构化判据（code 若真是字符串就解不出来）——
    顺带说明：结构化判据本来就覆盖它，多余的第二条判断只是噪声源。

- **11:3x~11:59 轮（复批后的六项）**：
  - **T-D 两条从骨架变真跑**：`TestCrashKillCoreAndRestart`（build `cmd/core` → 起进程 → 读 `core.endpoint`
    → Bearer 打 `/api/v1/health` 拿到 `state=READY/migration_version=3` → `Process.Kill()`（≈SIGKILL）
    → 热 `-wal` 保留 → 平台层重开 → `PRAGMA integrity_check=ok` + tables≥33 + schema_migrations 记账在）
    与 `TestCrashRestartSameDataDir`（**同一数据目录、不加 `-take-over-lock`** 重启 → 换新 endpoint → 仍 READY，
    证明残留锁与热 wal 被自动回收）。两条都 PASS。
  - REQ-T5 **已闭环**：§7 11:25 补了 `core/cmd/fake-adapter/**`→adapter，我的运行期解析当场跟上
    （规则 30→31 条），那两条命中自动消失。REQ-T1/T2 的前提也已按盘复核为**不存在**
    （`packages/` 只剩 api-client；`git status --untracked-files=all | grep -c node_modules` = 0），不再报。
  - REQ-T3 我这边跟上新形状（`audit.RedactionHit{Column, Kind, Match, Count}`），
    并新增 `TestRedactionKindIsMandatory` 记录**未落的另一半**（三条实测行号见下）。
  - BUG-T6 **进展核实**：`adapter/initialize` 不再被自己的未握手门禁挡掉，握手能通；
    但 `quota/read` 回 `INTERNAL_ERROR: quota/read is not implemented in this build` →
    旗标开时我的子进程用例会红（如实报），旗标关时 Skip。
  - 脚本加了 **fail-closed 前置检查**：缺 `core/internal`/`core/go.mod`/契约③ 或 `.go` 文件数 <20 时
    `exit 2` 拒绝出报告，并在报告头打印扫描面（本次 73 个文件）——防的是"扫描面为空 → 每条 0 命中 → 全绿"这种
    比误报更危险的伪装成功。`--selftest` 里用「把脚本拷进空仓库副本」验它必须 exit 2。
  - T-25 两问的答案：**①** loader 的补丁全部落进脚本本体（`load_owner_table` / `OWNER_HOLE` / `ANYROLE` /
    `cross_package_refs` / `orphan_modules` / `api_prefix_diff` / `OWNER_ROWS` 七个标记逐个 grep 到）；
    **②** 不需要重跑——一次性补丁器，以后改脚本用按内容定位的直接编辑。所以删除比留档干净，谢谢改口认可。


- **T-A**（`scripts/check_ownership.sh`，7 秒跑完）7 条（17:2x 按主控要求改版）：①approvals SQL 局部性 ②跨包写表 +
  `Append(表名)` + `.Exec` 旁门 ③变更文件 vs 契约③ §7 所有权矩阵（含冻结件 `000001`）
  ④go.mod/go.sum 归属与来路 commit ⑤evidence/.scratch 未入库 + .gitignore 有效 ⑥依赖方向
  （eventjournal 无内部 import；platform/db 只 eventjournal；audit 不 import db）
  ⑦契约③ §6.3 三处同步：事件目录那一腿常判（18 个事件名全在契约① §6 登记），
  yaml↔契约③ 资源前缀双向差集在 `specs/openapi/core-v1.yaml` 落地后自动激活（11 对 11 一致）。
- **权威源改为运行时解析 §7**（17:3x 主控二次要求"不要内嵌旧副本"）：`load_owner_table` 直接读
  `specs/api-contract-v0.1.md` 的表格行，`owner_of` 查这张运行期表。踩到并修掉的三个真问题：
  ① `specs/*.md` 带 CR，键变成 `...
` 导致全表不命中（→ 全部 UNOWNED 的假红）；
  ② `${p%**}` 在 bash 里是空操作，glob 没剥掉（同上）；
  ③ §7 的缩写约定（`core/internal/task/**`、`execution/**`）里"不带目录的 `X/**`"必须展开成
     `core/internal/execution/`，且判据用"根目录存不存在"而不是"拼上去存不存在"——
     后者在 `internal/process` 还没被创建时会把 adapter 的包误判成无归属。
  现在解析出 30 条规则；新增 `--explain <path>`（打印归属 + 命中的规则）和 `--rules`。
  §7 尚未覆盖的三条走 `OWNER_HOLE` 补偿表并显式标注来源（`core/internal/message`→backend、
  `docs/adapters/phase0`→survey、`README.md`→主控），§7 补齐后应删这张表。
- **「排除主控自己的提交」落进判据**：author 过滤在本机无效（所有会话共用一个 git 身份），
  改成结构性判据——未提交集合 = worker 在做（判越界）；基线之后已提交且工作树不再改的 = 主控落盘
  （只计入归属分布）。今天的效果：主控的 12 个已提交文件不可能被报成 worker 越界。
  `--selftest` 用合成越界证明三类扫描器都会报警（含"拼接盲点"这条负向断言）。
- **T-B**（`core/test/statemachine_test.go` + `projection_test.go`）：§5.1(17 态)/§5.2(12 态) 独立转录，
  正向可达 + 全笛卡尔积反向拒绝双向对账；终态不可外跳（含→UNKNOWN）；UNKNOWN 只能 Recovery+证据 进/出、
  终态不得改判为 UNKNOWN；未知状态名必须拒。§5.3 八条投影 + §5.4 拒绝语义（Rejection 三字段完整、
  被拒时不得返回目标态）+ 投影结果必须仍是 §5.1 的边。
  **变异验证**：往我的转录表里插一条假边 `CREATED→INTERRUPTED` → 两处用例立即报红（已还原）。
- **T-C**（`core/test/adapter_contract_test.go`）：`subject` 接口 = 契约 seam，桩 `newInProc()` 走
  `internal/adapter` 真代码；§2.2 major 不匹配必拒且拒后仍 PROTOCOL_NOT_INITIALIZED、
  未握手 11 个方法全拒、三态投递未声明必须 DELIVERY_MODE_UNSUPPORTED（含臆造模式）、
  `quota` 空回复 = `observations:[]` + UNKNOWN 且 JSON 面不得出现百分比字段、
  write_entry:false 拦全部写方法（UIA 显式声明者例外）、debug.* gating 只准 fake+EXPERIMENTAL、
  error.code 线上必须是整数（CO-4）。
  **变异验证**：把 major 不匹配改成匹配、把 `WriteEntry=false` 改成 true → 立即报红（已还原）。
- **T-D**（`core/test/crash_test.go` + `scripts/crash_recovery.sh`）：
  `TestCrashProcessDeathKeepsDurableData` **真跑**——子进程提交 5 条后在第 6 个事务中途 `os.Exit(9)`
  （Windows 无 SIGKILL，等价注入），父进程重开同一批文件（含热 -wal）断言
  `PRAGMA integrity_check=ok`、`journal_mode=wal`、projects/event_journal 各 5、
  未提交那条在**两张表里都不存在**、version 全为 1。
  **变异验证**：让 doomed 那条真提交 → 三条计数断言同时报红（已还原）。
  注意：`committedRows` 单独改动不会被发现（父子共用常量），所以补了独立于计数的 doomed 缺席判据。
- 交付物：`scripts/check_ownership.sh`、`scripts/crash_recovery.sh`、
  `core/test/{statemachine,projection,adapter_contract,crash}_test.go`

## Remaining

- REQ-T3 的另一半：`db.RedactionHit` 仍无 `Kind`（platform/db/redact.go:87）、
  approval 的 mapper 仍在用被废除的 `key:` 前缀（approval/store_sqlite.go:445）、
  `audit.RedactionDetails` 不校验空 Kind（redaction.go:127）→ 12:0x 复核：前两条已随 T-27 落地，
  只剩这一条；改完我把 `TestRedactionKindIsMandatory` 转成硬断言
  （口径已写在注释里：`RedactionDetails(空 Kind hits)` 必须返回 error）。
- FakeAdapter 的 §14 全量：等 `quota/read` 等未实现方法补齐，再开 `LUCERO_TEST_FAKE_ADAPTER=1` 跑全表。
- T-D 剩余 3 条：interrupted/recoverable 识别（Recovery Engine）、Idempotency 不重发、Approval 不丢
  ——`cmd/core` 现在只有 health + ws-ticket + 单实例锁，其余 501，断言清单已在各 Skip 里。

## Blockers

- **T-D 剩 3 条 blocked**（11:5x 更新：5 条里已有 2 条变真跑）：
  `TestCrashRestartIdentifiesInterrupted` / `TestCrashNoDuplicateMessageAfterRestart` /
  `TestCrashApprovalNotLost` / `TestCrashEventJournalComplete` → 等 **backend** 的 Recovery Engine 与
  带副作用的 HTTP 端点（`cmd/core` 现在只有 health + ws-ticket + 单实例锁，其余 501）；
  `TestCrashAdapterKillDoesNotTakeCoreDown` → 等 **adapter** 的 Process Supervisor（第 3 轮已派）。
- `TestFakeAdapterSubprocess`：旗标开启时会红——握手已通但 `quota/read` 报
  `not implemented in this build`；默认关闭并如实 Skip（BUG-T6 剩这一半）。
- `TestProjectionRejectedJournalAndAudit`：§5.4 的两条记录仍无产出方（`projection_rejected` 全仓只命中注释）；
  已归入 backend 本轮 `task.Service` 动作集，我代做等于替它写业务。
- `TestWriteEntryOpenQuestions` / `TestRedactionKindIsMandatory` / `TestCoreReadyEventIsJournalized`：
  分别等主控裁 Q1/Q2、等 db+audit 落 REQ-T3 另一半、等 backend 落 REQ-T7。

## Coordination

- **REQ-T7（给 backend，实测）**：契约① §6 登记了 `core_ready` / `core_shutdown`（后者还要 Audit），
  但 `cmd/core` 启动两次、`/health` 都回 READY、迁移记账到 v3 之后，`event_journal` 仍是 **0 行**。
  已钉成 `TestCoreReadyEventIsJournalized`（Skip 带实测原文），补上即转硬断言。
- **REQ-T8（新越界，非我这个角色能修）**：`.qoder/scratch/` 下有 4 个一次性 Python 补丁器
  （`fin.py`/`fix_t6.py`/`upd.py`/`upd2.py`，时间戳 18:23~11:25，`grep -ln qoder/scratch` 指向 `adapter.md`，
  脚本内容是在改 `.qoder/progress/adapter.md`）。`.gitignore` 只忽略 `.qoder/evidence/` 与 `.scratch/`，
  **不覆盖 `.qoder/scratch/`**，§7 也没有这一行 ⇒ 主控任何一次 `git add -A` 会把别人的临时工具写进版本库。
  建议：并入已忽略的 `.scratch/<role>/`，或给 §7 补行 + .gitignore 加 `.qoder/scratch/`。
- **Q1/Q2 重述（主控说在文件里找不到原文，这次带条款原文）**：
  - **Q1** `approval/respond` 算不算契约② §3 的"写方法"？§3 原文：
    "**`write_entry: false` 是合法声明**…此时**任何写方法返回 `WRITE_ENTRY_NONE`**，Core 必须把该 Adapter
    归为"只读观测型"，UI 不得渲染任何写操作入口，也不得降级去点 UI 假装写成功——除非该 Adapter 显式声明了
    `UI_AUTOMATION` 等级的对应能力。" §3 **没给写方法清单**。`internal/adapter/handshake.go` 的 `writeMethods`
    刻意排除 `approval/respond`，注释理由是 "approvals travel on the permission channel and are gated by
    Permission Engine, not by write_entry" 并引用 **"§12.11 (ruled 2026-09-19)"**；
    本轮再查（11:3x）：`grep -rn "12\.11" specs/` **仍然零命中**。
    现实后果：`write_entry:false` 的只读 Adapter 现在允许 Core 调 `approval/respond`。
    要么把清单回写进 §3（我就把 `approval/respond` 加进我的 `writeMethods` 断言表），要么维持现状但把例外写明。
  - **Q2** `session/create` 出现在 `handshake.go` 的 `writeMethods` 里，但契约② §4 方法表**没有这个方法**
    （`grep -rn "session/create" specs/` 零命中）。是要新增端点，还是写方法表里的笔误？

## Changed files

- `scripts/check_ownership.sh` — 7 条检查；§7 运行期解析（无副本、<12 条规则即 exit 2）、
  fail-closed 前置检查（扫描面异常直接拒绝出报告）、`--explain`/`--rules`、`--selftest` 合成越界自证
- `scripts/crash_recovery.sh` — FIRST SPRINT §7 的跑法与 blocked 清单
- `core/test/statemachine_test.go` — §5.1/§5.2 独立转录，双向对账 + 终态 + UNKNOWN
- `core/test/projection_test.go` — §5.3/§5.4 投影（含 T-21 更正后的四条待填断言）
- `core/test/adapter_contract_test.go` — 协议 §2/§2.2/§3/§5/§7/§9 + CO-4 线格式；
  第二被测面 = 真子进程 FakeAdapter（真 build + stdio ndjson）
- `core/test/audit_chain_test.go` — T-14 哈希链时序不变量（13 列篡改 / Seal 顺序 / 脱敏接缝），
  已跟 REQ-T3 的 Kind 新形状；`TestRedactionKindIsMandatory` 记剩余一半
- `core/test/crash_test.go` — §7 Crash 清单：2 条真跑（杀 Core + 同目录无干预重启 + integrity）+ 骨架
- `.qoder/progress/test.md` — 本文件

## Verification evidence

- 11:59 四条验收一次性重跑：`go test -count=1 -timeout 350s ./test/...` → **22 PASS / 10 SKIP / 0 FAIL**（exit 0）；
  `check_ownership.sh` → exit 1（4 条 = REQ-T8）；`--selftest` → exit 0；`crash_recovery.sh` 前两条为 PASS
  → `.qoder/evidence/test/{go-test,ownership-lint,selftest}-final.txt`
- 真跑凭据：`core.endpoint` 由 Core 自己写出（`{"host_port":"127.0.0.1:xxxx","token":...}`）、
  `/api/v1/health` 回 `{"state":"READY","version":"0.1.0-dev","migration_version":3,"ready":true}`、
  `Process.Kill()` 后目录里留着 `lucero.db-wal/-shm`、重开后 `PRAGMA integrity_check` = `ok`、
  tables=44、schema_migrations=[1,2,3]
- **本轮三次自纠（都是我先说错/写错、核实后改）**：
  ① 我怀疑 adapter 伪造 `CO-1` 引用来扩 SCOPE —— 实际 CO-1 在契约② §10:169，引用成立；真问题是 §7 漏同步（REQ-T5，已由你补行）。
  ② 我的 `literal` 桶第一版放宽到"字符串里含表名"，被 `"stream frames must be notifications"`
     和 `--dangerously-bypass-approvals-and-sandbox` 里的 `approvals` 淹掉 → 收紧为"整个字面量等于表名"。
  ③ 我写的同目录重启用例有竞态（读上一代残留 `core.endpoint` 拿已死端口去请求）→ 改成"文件 mtime 前移 + 端口换掉"；
     另外两条占位代码 `Process.Wait()` 造成 600s / 150s 死锁，已删。
- 变异验证累计：T-B 假边、T-C major/write_entry、T-D 补提交、T-14 塞 `signature`、§6.3 前缀合成件、
  T-24 `cross_package_refs` 三断言、前置检查必须 exit 2 —— 每次都能红，还原后绿
- REQ-T1/T2 前提复核为不存在：`packages/` 只剩 `api-client`、`git status --untracked-files=all` 里 node_modules 计数 0

## Recovery note

- Interruption point: 11:59 交付完成。等主控转派 REQ-T7 / REQ-T8、裁 Q1/Q2，等 db+audit 落 REQ-T3 另一半、
  等 adapter 补 FakeAdapter 未实现方法、等 backend 落 Recovery Engine 与写端点
- First action after recovery: 依次重跑 `LUCERO_TEST_FAKE_ADAPTER=1 go test -run TestFakeAdapter`、
  `go test -run 'TestRedactionKind|TestCoreReady|TestCrash'`、`bash scripts/check_ownership.sh`，
  任一从 Skip 转 PASS/FAIL 即按注释里的口径把断言改成硬的；改完必须再跑一遍变异验证才算数

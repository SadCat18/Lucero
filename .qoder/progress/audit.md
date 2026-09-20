OWNER_ROLE: audit
TASK_ID: audit-permission
STATUS: in_progress
SCOPE: 权限判定、Approval 聚合与裁决、Temporary Grant、Audit 哈希链与敏感规则集、§9 绕审批门禁；不实现脱敏执行点，approvals/temporary_grants 的 SQL 只准出现在 internal/approval
EVIDENCE_DIR: .qoder/evidence/audit/
LAST_UPDATED: 2026-09-20T12:03+08:00

## Objective

把 `internal/audit`、`internal/permission`、`internal/approval` 三个包的**规则与判定层**做到可执行且被测试证明：
敏感规则集 + 哈希链纯函数、Permission 优先级链、Approval 聚合的幂等裁决与 Temporary Grant 发放、
绕审批开关的硬门禁（契约 §9），使 db 落地后只需实现 `Store`/接线，不需要再决定任何安全语义。

## Completion criteria

- [x] `CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test ./internal/audit/... ./internal/permission/... ./internal/approval/...` 通过（165 个 test/subtest PASS，0 FAIL；2026-09-20 11:33 最后一次重跑；全仓 14 个包中 13 ok，
  唯一红的是 `cmd/core`——别的角色 11:31→11:33 之间正在写的 `main.go:184 undefined: filepath`，与我无关）
- [x] 哈希链：改写、删除（含尾部删除）、乱序追加均被拒
- [x] 正则任一编译失败 → `Validate()` 返回 error（fail-closed，测试证明）
- [x] 脱敏：deny-key 命中后原值不出现；`vault://name` 完整保留；伪造 `sk-`/`ghp_` 喂入后落库结果与 Audit details 均无原值
- [x] `Decide` 优先级链：31 条表驱动用例，含「显式 Deny 时 Temporary Grant 不能翻案」
- [x] `Resolve` 幂等：连调 3 次只产生 1 次状态写与 1 个事件
- [x] `approvals` 表 SQL 局部性：契约 grep 的命中**全部**落在 `internal/approval/`（store_sqlite.go + 其测试）；
      包内另有可执行扫描测试 + 自检用例，防止扫描器坏掉导致假通过
- [x] **（C2 新增）契约 §9 绕审批开关门禁**：`permission.CheckAdapterArgs` 命中即返，含 §9 点名的三个真实开关；
      `audit.OutcomeDenied` 常量供调用方记 `audit_entries(outcome=DENIED)`
- [x] **（B1 解除后新增）`Store` 的 db 实现**：`internal/approval/store_sqlite.go` 走 `Tx.Create`/`Tx.Touch`/`Tx.Append`，
      真库集成测试 6 个（含 6 并发 Resolve 只 1 个事件）
- [x] **（B1 解除后新增）哈希链真正写入 `audit_entries` 的集成验证**：读回整条链 `VerifyChain` 通过；
      改一行 / 删最新一行均检不出为"合法"→ 报错
- [x] **（Q6 同步）** `audit.IsVaultRef` 与执行点 `db.isCleanVaultRef` 同一判定：
      前缀对 **且** 整串过 patterns 不变样才豁免；两侧漂移由新增的集成子测试当场报红（变异验证已做）
- [x] `permission.Source` 的 db 读实现：`source_sqlite.go` 三个读方法 + 4 组真库用例（规则行/授权行/紧急停止位**确实被读出来**）
- [x] **（T-23 / §3.1 已落地）** `Decide` 返回 `(Effect, Reason, error)`，默认由内置风险分类决定且
      显式出现在输出里（`DEFAULT_HIGH_RISK`/`DEFAULT_UNKNOWN_RISK`/`DEFAULT_ALLOW`），命中规则时 Reason=规则 id
- [x] **（REQ-A4⑤ 结案）** 默认不再"空表全卡"也不再"空表全放"；垂直链起跑不被审批卡住已由真库切片证明
- [x] **（Next action 已完成）垂直链切片真库用例**：`policy_rules`→`Decide`→`Request`→`Resolve(APPROVED+Temporary)`
      →`Decide=ALLOW_TEMPORARY`→过期→`SweepExpired`→回落默认，含事件次序与授权行删除的反证
- [x] ~~B3~~ **已闭合**：db 的 `AggGrant` + `000003` 落地后，授权发放走 `Tx.Create`(`grant_issued`)、
      回收走 `Tx.Touch`(`grant_expired`)，`ErrGrantRevocationUnsupported` 已删除；再 sweep 不重复产生事件
- [ ] `RedactionRules()` 被 `db.InitRedaction` 以**类型**消费 — 现靠调用点手工传字段（REQ-A1 仍开）
- [ ] `audit_entries.signature` 的 checkpoint 签名（密钥来源未定，见 Remaining）
- [ ] §9 门禁接进 adapter 启动路径（执行点在 `internal/adapter`/`process`，非我可写）— REQ-A5
- [x] `RevokeGrants` 落库 + `grant_issued`/`grant_expired` 事件（同上）

## Scope boundaries

- In scope: `core/internal/approval/**`、`core/internal/permission/**`、`core/internal/audit/**`
- Out of scope: `core/go.mod`、`core/migrations/*.sql`、`internal/platform/**`、task/execution/session/quota、
  Event 与 Audit 的**写入机制**、redactor 本体、`cmd/core` 接线

## Current checkpoint

- Stage: B1 已解除并完成落库层；`approvals`/`temporary_grants`/`audit_entries` 真库链路打通
- Status: in_progress
- Last verified action: 16:52 实跑 `go test -count=1` 三包 ok（**149 PASS / 0 FAIL**）+ 全仓 `go test ./...`
  11 个包全 ok（含新出现的 adapter/quota/session/test 包，无回归）+ grep 命中全在 internal/approval 下
- Next action: 无自主项——REQ-T3 我这半已闭合。等主控派发；**若 Wave-2 要一条"链完整性"验收**，
  现成可用的只有 `audit.VerifyChain(entries, Head{Seq,Hash})`，而 `Head` 必须由**链外**保管（DB 内任何一行都可被删），
  这一点需要主控定"checkpoint 存哪儿/谁签名"，定了我再动。
  （T-23 第 1 条驳回的"替 backend 预置骨架"已撤回，接口形状与错误语义改写进上面 Coordination。）
- Verification: `go test -count=1 ./internal/{audit,permission,approval}/...` = 3 包 ok
  （18:19:50 = 162 PASS / 0 FAIL；18:21:08 复核仍绿），全仓 13 包 0 FAIL

## Completed

### 第 10 轮（2026-09-20 11:56~12:03，接 T-28）

44. **REQ-T3 我这半收尾完成**：db 已落 `HitKind`（`redact.go:87-92`），于是
    `toAuditHits` 里那处 `strings.CutPrefix(h.Match, "key:")` 被删成直接复制 `h.Kind`，
    `strings` import 随之消失。`grep -rn CutPrefix internal/approval/*.go` → **0 hits**。
    两侧结构体现在逐字同形：`{Column; Kind HitKind; Match; Count}`。
45. 新增跨包哨兵 `TestHitKindsAgreeAcrossPackages`：kind 是**跨包字符串转换**过去的，
    所以断言 `audit.HitKey == db.HitKey` 且 `audit.HitPattern == db.HitPattern`。
    变异验证：把 `audit.HitKey` 篡成 `"denykey"` → 该用例红
    （`audit {denykey,pattern} and db {key,pattern} disagree`）。
    **一条诚实的限制**：同一轮里"落库行的 kind 值"用例**没有**跟着红——因为写进去的值来自 db 那份常量，
    audit 侧改字面只让命名不一致、不改数据。也就是说这条红是**命名哨兵**，不是数据哨兵；
    数据侧的归因正确性仍由 `TestStoreRedactsSecretsBeforeTheyLand` 的 `"kind":"key"` 断言看着。
46. **T-28② 窗口期处置收到**：不回填、不打标（回填=重算 `entry_hash`=伪造链），改为 Wave-2 验收前重建 dev DB。
    我这侧**不需要任何改动**：三包所有真库用例都在 `t.TempDir()` 里从 `000001` 重新迁移
    （`grep` 核对：approval / permission 两个夹具文件各只有 1 处建库点，全走 TempDir，无仓库内路径），
    所以重建开发库不会影响任何一条测试，我的结果也不依赖那个 dev DB 的历史。
47. **T-28③ policy CRUD 判不建，已按裁定不做**（我上一轮就是"不猜、等你点头"，现在答案是"不建"）。
    `policy_created`/`policy_updated` 保留登记、无人发是**已知状态**；已从 Remaining 划掉，不再当缺口报。
48. **REQ-A1 实测更新**：`cmd/core/main.go:109-110` 已是
    `rules := audit.Rules(); db.InitRedaction(rules.DenyKeys, rules.Patterns, rules.Marker)`
    ——**生产接线取的是 audit 的单一来源**，我原先担心的"cmd/core 可能传手抄列表"在事实上已关。
    残余只剩"摊平后的三个参数无类型约束"。另实测 db 的测试手抄本（`touch_test.go:15 auditRules()`）
    **今天与 `audit.Rules()` 逐字相同：25 keys / 8 patterns，差集为空**（用行级提取比对的，不是眼看）。
    → 请 `test`/`db` 把这份手抄换成注入或跨包等值断言（`test/` 可同时 import 两包），否则它哪天漂移都没人报。

### 第 9 轮（2026-09-20 11:19~11:33，接 T-23 三件事 + REQ-T3）

40. **驳回已收**：不替 backend 写任何东西。`permission.Service` 的构造、四值 `Effect`、`Reason` 取值规则、
    以及**错误语义 `(DENY, "", err) = 不知道、不是被拒绝`** 全部写进上面 Coordination，backend 自己接。
41. **REQ-T3（audit 半）**：`audit.RedactionHit` 换成契约给的带标签形状 `{Column; Kind HitKind; Match; Count}`
    + `HitKind ∈ {key, pattern}`，JSON 随之外变（`{"kind":"key","match":"apikey","count":1}`）。
    我原先的双槽 `Key`/`Pattern` 一并删掉。**db 半未落地**（11:31 实测其结构无 `Kind`），
    所以前缀猜测没被"顺手留着"而是**关进一个函数**：`toAuditHits` 是唯一 `CutPrefix("key:")` 的地方，
    注释点名它必须被删、以及为什么不能拖（该串进 `entry_hash`，归因错位事后修不了）。
42. **T-23 第 3 条（新用例）** `internal/approval/blocking_sweep_test.go`：
    - `TestBlockingStateRegressions`：空=NONE、自己的 pending 阻塞、**别的 execution 读 NONE**、空 id 报错、
      批准后解阻、再来一条 pending 又阻塞、**过期未 sweep 的 pending 既不阻塞也不放行**（回落到在案的 APPROVED）、
      换新 handle 重开同一文件后答案一致（证明来自磁盘不是内存）。
    - `TestSweepExpiredRegressions`：到期前不动、到期只翻该翻的（2 条事件 + 2 条审计 + version→2）、
      **NULL expires_at 永不超期**、**已决行不被改判**、再 sweep 不产生第二个事件也不再取版本、
      时钟推到很远仍不动无期限行、重开后 `BlockingState` 读到 `EXPIRED`。
    - `TestApprovalWithoutExecutionIDIsInvisibleToEveryExecution`（§3 第 4 条专项）：先断言库里那行**真的是
      `execution_id IS NULL`**（否则"通过"可能来自数据而不是逻辑），再对三个 execution id 逐个断言
      `BlockingState=NONE` 且 `ForExecution` 空，加**阳性对照**（同一个 service 对绑定行必须读得出 PENDING），
      再加**交叉一致性**：`st.ListByExecution` 的原始结果与 `SELECT id FROM approvals WHERE execution_id=?` 逐条对齐。
43. **变异验证（这轮抓到我自己的测试有一个是虚的）**：
    ① 去掉 service 侧的绑定复筛 → **没红**（SQL 还拦着）；② 去掉 SQL 的绑定条件 → **还是没红**（service 复筛兜住了）；
    交叉校验最初比的是 `svc.ForExecution`（已过复筛）所以也一样没抓到——**这一条我当时报了"通过"其实不成立**。
    改成直接比 **`st.ListByExecution` 原始输出** 后，同一个 SQL 变异立刻红：
    `ListByExecution(exec-1) returned … bound to "" — the query is not binding`。
    教训写进自述纪律：**两层防御会互相掩盖，交叉校验必须打在待验那层的原始输出上**，
    并且"某处坏掉仍然绿"本身就是设计缺陷，不能靠"另一层有测试"含混过去。
    ③ 默认整体放行 / 空 `Reason` / sweep 改判已决行 —— 三处都按预期红。

**policy 事件**：`policy_created`/`policy_updated` 已在 catalog（`events.go:50-51`），但 `Decide` 是纯读判定、
不发事件；要发它得先有人**写** `policy_rules`，全仓今天没有这条写路径。我没有按猜测建 policy CRUD（见 Coordination 待点头）。

### 第 8 轮（18:03~18:19，接 T-23：§3.1 风险默认）

35. `internal/permission/permission.go` — `Decide` 改签名 `(Effect, Reason, error)`；新增 `type Reason string`
    与 §3.1 的三个默认值；内置分类器 `highRiskActions` / `lowMediumActions`（**不读策略表**，空表也成立），
    两边都不认识的动作 → `REQUIRE_APPROVAL` + `DEFAULT_UNKNOWN_RISK`（不确定即失败关闭）。
    **删掉 `Engine.Default` 字段**：§3.1 之后可配置的默认已经不存在，留着就是第二个真相源
    （连带删掉 `decide_test.go` 的 `def` 列与"default is the last rung"用例，换成 §3.1 的分类用例）。
    `Rule` 加 `ID`，`source_sqlite.go` 的 SELECT 一并带上 —— 命中规则时 Reason 直接给**规则 id**，
    于是我把原先自己发明的 `EXPLICIT_DENY` 删了：报"哪条规则拦的"比报一个泛称有用。
36. 补契约 §3 要求但我此前没落的 `permission.Service` 接口（按 §3.1 新签名），加
    `var _ Service = (*Engine)(nil)` 编译期钉住，避免 backend 开工时签名对不上。
37. 切片按 §3.1 要求 ③ 重写（`vertical_slice_test.go`）：`execution_started` 与 `session.send`
    在**零策略行**下即 `ALLOW/DEFAULT_ALLOW`（起跑不卡），`filesystem.write` 是 `DEFAULT_HIGH_RISK`
    （链上那次审批是 FakeAdapter 主动要的真实高危动作），未登记动作 `telescope.align` 是 `DEFAULT_UNKNOWN_RISK`；
    再插一条 `PROJECT filesystem.write ALLOW` 证明**任何一层有规则即覆盖默认**（Reason=该规则 id），
    删掉后回到默认。批准后的 Reason 必须等于**授权行自身 id**，换 task 就退回高危默认，
    过期+sweep 后回 `DEFAULT_HIGH_RISK`。
38. 变异验证三次（先红后绿、无残留）：
    ① 把默认整体改成 `ALLOW/DEFAULT_ALLOW`（模拟静默放行）→ 12 个分类用例 + 链测试全红并逐条点名；
    ② 试图丢掉 Reason → **编译不过**（返回类型强制），改为把两个默认值篡成空串 →
       `effect REQUIRE_APPROVAL came back with no reason` 在多例红；
    ③ 上一轮的反证（删授权行）继续有效。
39. 全仓回归：`go test -count=1 ./...` → 13 个包全 ok（含本轮其他角色新增的包），0 FAIL。

**契约文字的一处不可执行点**：§3.1 写的是 `Decide` 返回 `(Effect, Reason string, error)`，
这在 Go 里不是合法签名（结果列表不能一半命名一半不命名）。我按语义实现为 `type Reason string` 的具名返回。
若主控要字面一致，就得写成 `(Effect, string, error)` 并丢掉类型检查 —— 我建议保留具名类型，改一句契约措辞。

### 第 7 轮（17:44~18:01，执行 Next action：垂直链切片）

30. `internal/approval/vertical_slice_test.go`（2 个用例）+ `store_sqlite_test.go` 的 `seed()` 补 `sessions` 行
    （`approvals.session_id` → `sessions` → `adapter_instances` 这条 FK 链之前没人踩到，切片第一次踩到）。
31. **REQ-A4⑤ 的实测答案（把主张换成数字）**：零规则行起步时，
    `Decide` 对 **`session.read` 这种只读动作也返回 `REQUIRE_APPROVAL`**
    —— 用例里这一步的断言原文就是"with no rows at all a read is gated too, so the default is not
    'only risky actions need approval'"。也就是说：
    **只要 Wave-2 用 `Engine.Default = REQUIRE_APPROVAL` 且库里没有 `policy_rules` 种子，
    §7 序列里的 `execution_started` 之前就会先卡住一个审批**。
    种子一条 `GLOBAL / session.read / ALLOW` 之后只读动作立刻放行、`filesystem.write` 仍要审批——
    证明这是"缺数据"而不是"逻辑错"。**要谁去插这条种子（db 的迁移 vs 主控指定的 seed 脚本）请拍。**
32. 切片还钉住三件此前只是"我说"的事：
    ① 待决审批**不会**自行放宽权限（pending 时 `Decide` 仍是 `REQUIRE_APPROVAL`）；
    ② 批准后 `Decide` 变 `ALLOW_TEMPORARY`，而**换个 task_id 就不认**（授权绑作用域，P0 那条错绑）；
    ③ 时钟推过期后不 sweep 也已不授权，sweep 之后 `Decide` 回默认；
    ④ 事件次序实测 `approval_requested → approval_resolved → grant_issued → grant_expired`
    （全序 `[…seed 5 条…, approval_requested, approval_resolved, grant_issued, grant_expired]`），审计链 3 条跨切片仍 `VerifyChain` 通过。
33. **反证用例** `TestVerticalSliceDependsOnRealRows`：审批保持 APPROVED 不动，只 `DELETE FROM temporary_grants`，
    `Decide` 必须从 `ALLOW_TEMPORARY` 掉回默认——如果谁把"已批准"直接当"已授权"写死，这条立刻红。
34. 变异验证三处（都先红后绿、无残留）：`Engine.Default` 翻成 `ALLOW` → 两个切片用例同时红并指出
    "empty policy: ALLOW"；把期望事件次序反排 → 报 `expected [grant_expired …] in order within [...]`；
    删授权行本身就是对第 33 条的内置变异。

### 第 6 轮（17:36~17:44，接 T-15：确认 + 取证）

26. **先更正上一条自述**：我 17:36 报告里写"`ErrGrantRevocationUnsupported` 连同它的用例一起删掉"——
    **那句不实**。补丁当时作用在 `approval.go`，而该变量声明在 `store_sqlite.go:35`，所以它一直活着
    （只是无人引用，包级变量不参与未使用检查，因此编译与测试都不会报）。
    本轮 grep 定位后真正删除：`grep -rn ErrGrantRevocationUnsupported --include=*.go .` → 无。
    教训：**删代码要用 grep 复验"引用为 0"，不能只信自己写过的编辑操作**；"我改过了"不等于"它不在了"。
27. **§1.1 第 10 条合规核对**：我包里剩下的 `Append` 只有 `audit_entries` 一处
    （`internal/audit/append.go:117`，append-only、不在聚合注册表内）；`temporary_grants` 的两条路径
    已分别是 `Tx.Create(AggGrant,…)` 与 `Tx.Touch(AggGrant,…)`。规则 10 对本轮的我方影响 = 0（上一轮已改完）。
28. 取证（17:44:31 同一分钟内）：`go test ./internal/approval/ -count=1` → ok；
    三包 → 157 PASS / 0 FAIL；`go test -count=1 ./...` → **11 个包全 ok**。
29. **撤回观察 2**：17:35 我报的 `internal/adapter` 编译红（`tr.Claim` 参数不匹配）在 17:44 已不复现，
    adapter 自己修好了；`core/test` 那次 build failed 同样是瞬态。两条都不再是事实，账里就地作废而不删除，
    以免下轮把它当成待办。

### 第 5 轮（17:16~17:32，接 T-13）

21. `internal/permission/source_sqlite.go` — `SQLiteSource` 三个读方法：`EmergencyStopped` 读
    `settings(scope_type='GLOBAL', scope_id IS NULL, setting_key='emergency_stop')` 的 `value_json.{"enabled":bool}`；
    `Rules` 读 `policy_rules WHERE enabled=1`；`Grants` 读 `temporary_grants WHERE principal_id=?`
    （**不在 SQL 里筛过期/撤销**，判定统一交给 `Grant.Active`，免得多处一套语义）。
    `Source.Rules` 顺手去掉没人用的 `Request` 参数（真接口 `permission.Service` 未动）。
    删掉我自己写的 `knownScope/knownEffect` 校验：两列在 000001 就有 CHECK，那是走不到的分支。
22. `internal/permission/source_sqlite_test.go` — 4 组真库用例（夹具直插 `policy_rules`/`temporary_grants`/`settings`）。
    覆盖：空库→默认；GLOBAL ALLOW 生效；PROJECT DENY 压过 GLOBAL ALLOW；**别的 project 的规则不得命中**；
    `enabled=0` 不是"弱一点的规则"；TRUST_DOMAIN 压过 PROJECT；**同格内** priority 高者胜、平手取更严者；
    授权 live/expired/revoked/别人的 四态；紧急停止 `true`/`false`/行缺失/`value_json` 形状不对（→ DENY+error，
    坏的停止位不能被读成"可以走"）；连接关掉后 `Decide`/`CheckTemporaryGrant` 只能给 DENY+error。
23. `internal/audit` — `TestFreeTextReasonIsScrubbedByPatterns`：`reason` 不是 deny key，
    所以它的保护只能来自 value patterns；3 例内嵌密钥被打掉 + 3 例干净 reason 不改写，字段名本身保留。
24. **B3 在本轮中途被 db 解除**（`AggGrant`+`AggPolicy`+`000003`）。我先做的事是**预测它会撞墙**：
    一旦 `temporary_grants` 进了 `aggTable`，我原来的 `tx.Append("temporary_grants", …)` 就会被平台拒绝——
    db 落地时**这条真的红了**（`aggregate rows must go through Touch or Create`），随即按新形状改掉：
    `insertGrant`→`Tx.Create(AggGrant,…)` 并**第一次真正发出 `grant_issued`**（以前只能把 `grant_id` 塞进
    `approval_resolved` 的 payload 兜着）；`RevokeGrants`→`Tx.Touch(AggGrant, id, "revoked_at = ?")` + `grant_expired`
    + 审计行；`ErrGrantRevocationUnsupported` 与其用例一并删除（不是留着"以防万一"）。
25. `TestSweepReclaimsGrants`：到期授权被回收的全证据 — `revoked_at` 写入、1 条 `grant_expired`、
    该事件 `aggregate_type='grant'` 且 `aggregate_id` 是**授权行自身 id**、grant `version` 1→2、
    **再 sweep 一次事件数仍是 1**、审计链跨 sweep 仍 `VerifyChain` 通过。

**变异验证两次（都先红后绿、无残留）**：
- `IsVaultRef` 退回"只看前缀" → audit 3 个用例 + 集成子测试全红；
- `RevokeGrants` 改成静默 `return nil` → `TestSweepReclaimsGrants` 报 `revoked_at = -1` 与 `0 grant_expired events`。

**一处与 T-13 字面不同，已写进代码注释等主控过目**：sweep 回收到期授权时我发的是 `grant_expired` 而非
`grant_revoked`——这批行不是谁撤销的，是到期了，目录里两个类型都已登记。`aggregate_type='grant'` 与
`aggregate_id=授权行自身 id` 完全照裁定执行；要字面上的 revoked 就是改一个常量。

### 第 4 轮（17:08~17:16，接 T-10：Q6 语义同步）

17. `internal/audit/redaction.go` — **`IsVaultRef` 语义改判**：从「前缀 + 名字是裸 token」改成
    「前缀 **且** 没有任何 pattern 命中该串」，与 db 的 `isCleanVaultRef` 对齐（`redact.go:45-47`）。
    旧的裸-token 判据**不够**：`vault://sk-Zx9QZx9QZx9QZx9QZx9Q` 没有空白，旧规则会豁免它 = 明文入库。
    pattern 集合用 `sync.OnceValue` 编一次；**任一 pattern 编译失败 → 返回 nil → 谁都不豁免**（fail-closed）。
    audit 仍然不做替换，这里只是"什么算合法引用"的规则判定。
    注释里点名 Q6 的因果：**「是 vault 引用」≠「值里没藏密钥」**，并写明在 REQ-A1 收回类型之前，
    这份语义与执行点那份是**两份实现**，必须同步改。
18. 测试：`TestIsVaultRef` 换成 10 例（含 `vault://sk-…`/`ghp_…`/`AKIA…` 三种"钥匙穿引用外衣"→ false，
    `vault://db pass`、`vault://` → true：没有密钥就没有泄露，垃圾引用不是安全问题）+
    新增 `TestVaultRefDoesNotExcuseEmbeddedSecrets` + 集成子测试
    `TestStoreRedactsSecretsBeforeTheyLand/key_wearing_a_vault_reference_is_redacted_end_to_end`
    （**同时**断言规则侧拒绝与真库里落下的字节被脱敏，两侧不一致就红）。
19. 顺手按「契约测试不写死实现数值」把我自己那条断言放宽：
    `"column":"approvals.request_json"` → `"column"`（列名怎么拼是 db 的格式选择，不是安全性质）。

**Q3 复核**：`RecordRedaction` 第 3 轮就在（`internal/audit/append.go` 的 `Chain.RecordRedaction`，
被 `store_sqlite.go` 的 `recordHits` 在同一事务内调用），T-10 转述的"仍未见实现"不成立——
巡检自己那句对账也确认了。这轮没重做，只补了上面那条变异验证。

### 第 3 轮（16:30~16:52，接 B1 解除 + Q3/Q4 裁定）

11. `internal/audit/append.go` — 哈希链的**写侧**：`Chain{Head, Insert, Now}`（三个注入点，`*db.Tx` 靠结构满足
    `Appender`，**audit 不 import platform/db**，§0 方向守住）+ `RecordRedaction(base, hits)`（Q3）+
    `HeadQuery(*sql.DB)` + `ErrChainFork`。`Entry` 补 `Signature` 字段（不进 hash：它是签名，不是链的一环）。
12. `internal/approval/store_sqlite.go`（约 330 行）— `Store` 的真库实现：`Insert`→`Tx.Create`(version 1)+
    `approval_requested`+audit；`MarkResolved`→同事务先读回当前态做 PENDING 守卫，再 `Tx.Touch`+`approval_resolved`+
    grant 行+audit；`ExpirePending` 逐行 `Touch`→`approval_expired`；`Get`/`ListByExecution` 走 `db.Read`。
13. 集成测试 6 个 + audit 单元测试 6 个（真库夹具：principals/adapter/task/2×execution 由 `db.Open`+
    `Migrate(000001+000002)`+`Create` 搭，脱敏用 `audit.Rules()` 现场开启）。
14. Q4 核对：全仓 `grep -rn "AggQuota|quota" internal/{approval,permission,audit}` → **none**，
    我没有读那个常量，无需跟改（db 侧 `"quota"` 已落，`aggTable` 仍映射 `quota_accounts`，与我只读的表无关）。

**这一轮被自己的检查抓到三个真 bug（都是先红后绿，不是事后叙述）**：
- **hash 与落库字节不一致**：`Tx.Append` 会对 JSON 列「解码→遍历脱敏→重编码」，Go 的 map 重编码**按 key 排序**，
  于是我 seal 时用的 `details_json` 和库里存下来的不是同一串字节 → `VerifyChain` 对**诚实写入的行**报错。
  修：`Chain` 在 Seal 前先做同样的规范化（`canonicalJSON`）。这条要告诉 `test`/`db`：
  **任何对 audit 行做 hash 覆盖列的改写，都必须发生在 hash 之前**，否则链自己先烂。
- **一个事务内二次 append 撞 seq**：新 `Chain` 各自从 read pool 读 head，而第一条 audit 行还在未提交事务里 →
  预测 seq 1、实存 2 → 被 `ErrChainFork` 挡下。修：一个事务共用一个 `Chain`（`ExpirePending` 批量场景同源，
  否则多行会全部预测成同一个 seq）。
- **`recordHits` 忘了 `AuditID`**：`NOT NULL` 列，被 `Chain` 的前置校验在写库前挡下。

**平台层缺口（Q3 执行时才暴露，见 REQ-A6）**：`Tx` 没有任何事务内读方法，
所以「head 必须在事务里读」这件事今天只能靠 read pool + seq 自检回滚；`principals` 不是聚合，没有 `Create` 路径，
夹具只能直插。这两点都不是我能在自己范围内补的。

### 第 2 轮（16:14~16:26，接 C2 裁定）

先记一条事实核对：`.qoder/progress/audit.md` **在 C2 判定前已存在**——`ls -la` 显示
`-rw-r--r-- 17697 Sep 19 16:13 audit.md`，我 16:14 的收尾报告也给了路径；主控 16:20 重跑时它在盘上。
但**顺序确实违规**：任务书「第一个动作」写的是 redaction.go，我照做后把建账留在最后，
导致 40+ 分钟无可查账——这条已作为 feedback 记进本角色行为，不再辩解。

7. `internal/permission/adapterargs.go`（65 行）— 契约 §9 绕审批/沙箱/权限开关的**硬门禁**规则集 +
   `CheckAdapterArgs(args ...string) []string`。做成独立 gate 而非 Decide 的一格：链上任何规则或 grant 都不该投掉它。
8. `internal/audit/hashchain.go` — 补 `OutcomeSuccess/Failure/Denied/Unknown` 四常量（对齐 `audit_entries.outcome` CHECK），
   并把 §5.4（投影被拒 → `outcome=UNKNOWN` + `details{task_state,execution_event,reason}`）与
   §5.5（进/出 UNKNOWN → `details{occasion:"recovery",evidence}`、终态永不改判）的 **details_json 键约定**
   写死在该常量注释里——行形状归 audit，避免三个角色各造一套。本角色不实现投影逻辑（属 backend）。
9. `internal/approval/approval.go` 的 `Store` 文档按 §1.1 第 6~9 条校准：`Insert`→`Tx.Create`(version 1)、
   `MarkResolved`/`ExpirePending`→`Tx.Touch`；事件类型注册校验由 db 单点做，我不重复；payload 结构体留在本包。
   纯文档改动，签名与测试不变。
10. `internal/permission/adapterargs_test.go`（62 行，3 个 test / 40 个断言）— 见 Verification 第 13 条。

第 2 轮过程中被测试抓出的三个自身缺陷（都不是事后补记，改前红改后绿）：
- 逐 token 匹配会漏掉 `--auth` + `none` 这种**两 token 形式**的开关 → 改成在 join 后的整行上匹配；
- 改完后归属错位：把命中记到了前一个无辜 token（`gpt-5`）上，会让 DENIED 审计写错参数名 → 按命中起始偏移反查 token；
- `--sandbox=off` 因分隔符类不含 `=` 而漏网。

### 第 1 轮（15:40~16:13）
本轮全部为本角色独占路径，未改动他人文件（`git status --short` 见 Changed files 一节）。

1. `internal/audit/redaction.go`（127 行）— `RedactionRules{DenyKeys,Patterns,Marker}` + `Rules()` 唯一导出面；
   `CanonicalKey()`、`IsVaultRef()`、`RedactionHit`/`RedactionDetails()`、`Validate()`。
   **不含任何替换逻辑**（§2.1 要求）。规则覆盖：**25** 个 deny key + 8 条 pattern（12:03 数过；本行原写 26 是错的）
   （PEM / `gh[pousr]_` / `github_pat_` / `sk-` / Slack `xox*` / AWS `AKIA` / JWT 三段式 / `Bearer`）。
2. `internal/audit/hashchain.go`（127 行）— `Entry`（对齐 `audit_entries` 列）、`ComputeEntryHash`、`Seal`、
   `VerifyChain`、`Head`/`Checkpoint`。哈希输入用**长度前缀**分帧，防止相邻字段之间挪字节不改总长的伪造。
3. `internal/permission/permission.go`（300 行）— `Effect`/`Scope`/`Request`/`Rule`/`Grant`/`Source`/`Engine.Decide`/
   `CheckTemporaryGrant`，链序完全按 §7.2；`Grant.Active` 读侧强制过期。
4. `internal/approval/approval.go`（370 行）— `Approval`/`Draft`/`Resolution`/`GrantRequest`/`Store`/`Service`
   的 `Request`/`Resolve`/`ForExecution`/`BlockingState`/`SweepExpired`。
5. 测试 4 份共 1399 行 / 133 个用例。
6. 顺带发现并修掉一处**脱敏绕过**（见 Verification evidence 第 6 条），并识别出 db 侧同源缺陷（Coordination REQ-A2）。

## Remaining

- `internal/approval/store_sqlite.go`：`Store` 的真库实现（唯一允许出现 `approvals` SQL 的地方），
  建行走 `Tx.Create`（version 恒 1）、改态走 `Tx.Touch`（§1.1 第 6 条），`setSQL` 可多列但**不得自己写 version**
  （§1.1 第 7 条：写了 `version` 会被 Touch 直接拒）；事件类型不需我再校验（§1.1 第 8 条）。
- `internal/permission/source_sqlite.go`：`Source` 的实现（**下一步唯一动作**，纯读，无阻塞）。
  `EmergencyStopped` 读 `settings` 的一个键还是新列，需要 db/主控一句话：`settings` 表有 key/value，
  我倾向 `key='emergency_stop'`，不新增迁移。
- ~~`internal/audit/append.go` 接 `audit_entries` 读写~~ 已完成（第 3 轮）。仍缺 `signature` 列的
  checkpoint 签名：ADR-012 要 hash chain **+ checkpoint signature**，而签名密钥归 vault/DPAPI（First Sprint 只留接口）。
  现状是能验链完整性、不能防"整库被换掉"——`Head{Seq,Hash}` 需要有人长期保管，否则尾部删除仍检不出。
- §9 门禁的**执行点接线**：`CheckAdapterArgs` 已在 permission，但真正拦在启动路径上要由
  `internal/adapter`/`internal/process` 调（非我可写）+ 落 `audit_entries(outcome=DENIED)`。见 REQ-A5。
- 事件 payload 形状与 `Idempotency-Key` 的对接（`approval_resolved` 属 §8 高风险写）。
- 与 `backend` 的 `BlockingState` 契约测试、与 `test` 的 §5.4「不得静默吞」用例（test 角色）。

## Blockers

- ~~**B1（依赖 db）**~~ **已于 16:30 主控解除并验证**：`Tx.Create`/`Touch`/`Append`/`RedactionHits` + `Migrate`
  可用，我的集成测试自己 `db.Open`+`Migrate` 搭夹具，不需要 db 提供测试脚手架。
- ~~**B3（平台写路径缺口）**~~ **已于 17:32 闭合**：db 交付 `AggGrant` + `000003`（给 `temporary_grants`
  加 `version`）后，我按 T-13 指示改走 `Create`/`Touch`，`grant_issued`/`grant_expired` 成真，
  `ErrGrantRevocationUnsupported` 删除。全程没有为"看起来能用"而走裸 SQL 或跳过事件。
  证据：`TestSweepReclaimsGrants`（revoked_at 写入、事件 1 条、version 1→2、二次 sweep 不重复、链仍自洽）
  并通过变异验证（把 RevokeGrants 改成静默 no-op 立刻红）。
- **B2（契约冲突，需主控裁决）** `db.InitRedaction` 实际签名是 `InitRedaction(denyKeys, patterns []string, marker string)`，
  契约 §2.2 冻结的是 `InitRedaction(r audit.RedactionRules)`。见 REQ-A1。
- 无红线触发：未改冻结物、无 git 写操作、未装软件、未改 PATH、未运行五方软件。
  第 2 轮只写了自己三个包 + 本进度文件。

## Coordination

- **REQ-A1 → 主控 / `db`**：`InitRedaction` 的形状偏离已批准的契约 §2.2。db 的实现把规则拆成三个裸 slice，
  于是**编译器不再保证 cmd/core 传的是 `audit.Rules()`**——db 自己的 `touch_test.go` 就是传本地
  `deny, patterns` 列表通过的。两种收法请选一种：
  (a) 按契约改回 `InitRedaction(r audit.RedactionRules)`（db 只 import audit 的类型，不破 §0 的
      「audit 不得 import db」——方向是 db→audit，§0 并未禁止）；
  (b) 保留现签名，但由 `test` 角色加一条断言 cmd/core 传参等于 `audit.Rules()` 的接线测试。
  我建议 (a)：它是编译期保证，且省掉一条只有测试能守的约定。audit 侧 `Rules()`/`CanonicalKey()`/`IsVaultRef()`
  已按契约导出，不需为 (a) 做任何改动。
- **REQ-A2 → `db`（安全缺陷，建议尽快修）**：`redact.go:108` `scrubString` 以 `strings.HasPrefix(s, vaultRefPrefix)`
  整串提前返回，因此 `"vault://name sk-<40位密钥>"` **会原样落库**。`walk()` 的 deny-key 分支（`redact.go:132`）同理。
  audit 侧我把 `IsVaultRef()` 收紧成「`vault://` + 不含空白/引号的裸 token」并加了用例
  （`TestIsVaultRef`、`TestDenyKeysAndVaultRefs/vault reference cannot smuggle a secret`）。
  db 里那份 `vaultRefPrefix` 常量副本存在同一缺陷；若采纳 REQ-A1(a)，这条随类型接线一并消失。
- **REQ-A3 → 主控**：`audit.RedactionHit{Column,Key,Pattern,Count}` 与 `db.RedactionHit{Column,Match,Count}` 是两份形状，
  而它落在 `audit_entries.details_json`——那是 Audit 的表。主张形状归 audit（契约 §35/§58），db 调用
  `audit.RedactionDetails(hits)` 或 cmd/core 转译。若主控判定形状归 db，我这 30 行删掉即可，请明确一句。
- **REQ-A4 → 主控（契约缺口，实现时已按最小解释定案）**：
  ① `approvals` 表**没有 requested_by/principal_id 列**，但 `temporary_grants.principal_id` 是 NOT NULL——
  批准时「授权给谁」无处可取。现按「requester 存在 `request_json.principal_id`」实现（`requestPayload`，
  不动冻结 schema、不需要新迁移）。若更想要真列，那是 db 的 `000003`。
  ② 契约 §3 的 `Resolve(ctx,id,res) error` 无法表达「本次是否产生新事件/新授权」，幂等断言只能靠假 Store 计数。
  现签名保持不变，事件与 grant 行**下推到 `Store.MarkResolved` 同事务**内产出。
  ③ `RedactionRules` 没有表达「vault:// 豁免」的字段，机制上只能靠 db 侧硬编码常量（见 REQ-A1/A2）。
  ④ §7.2 的 8 条链序与 `policy_rules` 的 8 个 `scope_type` 不对齐：链里没提 TASK/ADAPTER/MODEL，
  scope 里没有 Emergency Stop 与 Default。现按「§7.2 命名的 rung 顺序一字不动，多出的三个 scope 按
  特异性插在 TASK_MODE 之后与 SESSION 之后」实现，需确认。
  ⑤ 链的最后一格「默认」没有定义值。`Engine.Default` 现为 `REQUIRE_APPROVAL`（fail-closed 且让人留在环里）。
  这会直接影响 Wave-2 垂直链：`event-contract §7` 序列要求 `execution_started` 在审批之前发生，
  空库无规则时 `Decide` 会直接要审批 → **需要 `backend`/`db` 提供 ALLOW 规则种子数据**，否则链跑不通。
- **REQ-A5 → `adapter` / `process` / 主控**：契约 §9 的绕审批开关门禁，**规则与拒绝判定已在 audit 侧交付**
  （`permission.CheckAdapterArgs(args ...string) []string`，返回命中的参数；`audit.OutcomeDenied` 供写
  `audit_entries`）。执行点不在我可写范围：请 `adapter`/`process` 在**拼完启动参数、真正 fork 进程之前**调用它，
  并且把 adapter 的 config JSON 序列化后一并传进去（`bypassPermissions` 会以 JSON 键出现，不总是命令行 flag）；
  命中即拒绝启动 + 记 DENIED 审计，**不得因自动重试而放宽**。
  两条设计声明请知悉：① 我没把它做成 `Decide` 的一格，因为链上任何 ALLOW/grant 都不该投掉它；
  ② `bypass` 走宽匹配，`--no-bypass` 这类**反向**命名也会被拒——宁可拒绝一个安全参数，
  也不能放行一个绕过审批的参数；若真出现被误拒的合法参数，改名而不是给我开白名单。
- **观察 2 → 主控（17:35，不在我范围，未动）**：`internal/adapter` **编译红**：
  `internal/adapter/stream_test.go:321` 等处 `tr.Claim` 调用仍传 1 个参数，实现已改成
  `(bool, string, string)`——测试没跟着改。与我三个包**无任何 import 关系**（正反向都 grep 过），
  不是我这轮改出来的；`go test ./...` 其余 10 包含 `core/test` 全 ok。
  【17:44 已作废：adapter 自己修好了，全仓 11 包 ok。】另记一次瞬态：17:34 我看到 `core/test` 也 build failed，17:35 自行恢复——并行角色正在写文件，
  **报绿前的最后一次重跑必须是同一分钟内的**，别拿上一分钟的输出说话。
- **观察 → 主控（不在我范围，未动）**：C2 第 4 条说 `internal/task` 红。我 16:15 实跑
  `go test -count=1 ./internal/task/...` → `ok 0.797s`，**已经不复现**（backend 应已同步）。
  但同期 `go vet ./...` 出现新红点：`internal/session/repository.go:12:31: undefined: sql.QueryerContext`
  ——`database/sql` 只有 `Queryer`，`*sql.Tx` 实现的是 `ExecerContext`+`QueryRowContext`，这看着像接口断言写错名。
  属 session/backend 范围，**我没碰**。它不影响我的验收（我的三包 `-count=1` 全绿），故未记 blocked。
- **REQ-A6 → `db` / 主控（阻塞 B3）**：平台层今天表达不了两类写：
  ① **可变非聚合行**（`temporary_grants.revoked_at`）——`Touch` 要聚合、`Append` 只能 INSERT；
  ② **非聚合行的事件**（`grant_issued`/`grant_expired`/`policy_*`）——只有 `Touch`/`Create` 会写 `event_journal`，
     而 `Append("event_journal", …)` 被明确拒绝。
  另两处同源：`Tx` 没有事务内读（我的链头只能读 read pool + seq 自检回滚），`principals` 不是聚合所以无 `Create` 路径。
  建议 db 加 `func (tx *Tx) Exec(table, setSQL string, args []any, where Where, ev *EventDraft) error`
  或最小版 `tx.ExecNonAggregate(table, set, args, where) (int64, error)` + `tx.AppendEvent(EventDraft)`；
  白名单/脱敏/列校验沿用现有那套（不要为此把 temporary_grants 伪装成聚合）。
  我今天用 `grant_id` 进 `approval_resolved` 的 payload 兜住可追溯性，**没有伪造事件**。
- **REQ-A2 已由 db 关闭（16:5x，`isCleanVaultRef`）**：我这边同步跟进见 Completed 第 4 轮。
  留一条长期约束给主控：**在 REQ-A1 落实之前，`vault://` 豁免语义在两个包里是两份实现**
  （audit `IsVaultRef` / db `isCleanVaultRef`），改一处必须改另一处。我加的集成子测试就是这条约束的哨兵。
- **REQ-A6 收窄（不要整条撤）**：grants 这条已被 `AggGrant` 解掉，但同源三条仍在——
  ① `Tx` 仍**没有事务内读**：我的 `Chain.Head` 与 `MarkResolved`/`ExpirePending`/`RevokeGrants` 的
     "先读再改"都靠 read pool + 单写连接串行化 + seq 自检回滚兜着，能出错但**不是精确**；
     给 `Tx` 一个 `QueryRowContext` 就能把这三个"ponytail"注释一起消掉。
  ② `principals` 仍不是聚合，没有 `Create` 路径 → 所有需要 principal FK 的测试夹具只能直插（permission 与
     approval 两份夹具重复，因为 §0 禁止 internal/common 口袋包，我不想为此造共享包）。
  ③ **`AggPolicy` 已存在但事件目录里没有 `policy_*`**：谁现在去 `Create(AggPolicy, …)` 都会撞到
     `event_journal.Registered` 报错（§9.1 禁止未登记类型入库）。要么先在 event-contract §6 补登类型，
     要么别写 policy_rules。这条归主控与 `db`/`backend`，我只是先撞为敬地指出。
- **REQ-A7 → `db`**：`RedactionHit.Match` 用字符串前缀 `"key:"` 区分"命中 deny key"与"命中 value pattern"，
  我在 `toAuditHits` 里 parse 它。请改成结构字段（`IsKey bool` 或 `Kind`），否则这个前缀变成两个包之间的隐式协议。
- **REQ-A1 现状更新**：我的集成测试就是接线证据——`db.InitRedaction(r.DenyKeys, r.Patterns, r.Marker)` 传 `audit.Rules()`
  完全跑得通，行为与契约一致；但仍是**手工摊平**，且 db 自己的 `touch_test.go:auditRules()` 是一份**手抄副本**
  （今天与我的集合逐字相同）。裁决仍建议收回 `audit.RedactionRules` 类型；若保留现签名，请让 test 角色
  加断言"cmd/core 传的是 `audit.Rules()` 而不是任何手抄列表"。
- ~~**REQ-A4⑤**~~ **已裁并落地（T-23 / §3.1，第 8 轮 18:19 绿）**：默认由内置风险分类决定。
  我原先给的 (A) 种子规则 / (B) 改默认 两条都被 §3.1 的第三条路取代，**Wave-2 不再需要种子 ALLOW 规则**，
  `policy_rules` 空表也能起跑。仍需主控回一句：§3.1 的 `(Effect, Reason string, error)` 在 Go 里不合法，
  我实现成具名类型 `Reason`（见上），要字面一致就告诉我。
- **REQ-A4⑤ 历史（第 7 轮实测，已被上面的裁定取代，保留以说明为什么裁得快）**：零 `policy_rules` 起步时连 `session.read` 都会判
  `REQUIRE_APPROVAL`，因此 §7 的 `execution_started` 会被审批挡在前面。请从两条里挑一条：
  **(A)** 由 db 在 `0000xx` 里 seed 一组只读动作的 `GLOBAL ALLOW` 规则（我不写 .sql）；
  **(B)** 把 `permission.Engine.Default` 改成 `ALLOW`，改为"只对显式列出的高风险动作要求审批"。
  我倾向 (A)：(B) 会把"没配规则"这件事变成"全放开"，正是 ADR-011 想消掉的那种默认。
  选定后我这边都是一次小改 + 重跑切片。
- **`permission.Service` 的形状与错误语义（T-23 第 1 条：我不动 backend 的文件，这里给全）**：
  ```go
  eff, why, err := perm.Decide(ctx, permission.Request{...})
  ok, err := perm.CheckTemporaryGrant(ctx, principalID, scopeType, scopeID, action)
  var _ permission.Service = (*permission.Engine)(nil)   // 编译期已钉住
  ```
  - **构造**：`permission.New(NewSQLiteSource(d), now)`，`now` 必须非 nil（传 nil 直接 panic，这是故意的：
    没有时钟就没法判授权过期）。`Source` 只有三个读方法，Permission 不写任何表。
  - **`eff` 四值**：`ALLOW` / `DENY` / `REQUIRE_APPROVAL` / `ALLOW_TEMPORARY`。
  - **`why`（§3.1）**：命中规则=该 `policy_rules.id`；命中授权=`temporary_grants.id`；
    紧急停止=`EMERGENCY_STOP`；走到默认=`DEFAULT_HIGH_RISK` / `DEFAULT_UNKNOWN_RISK` / `DEFAULT_ALLOW`。
    **`err == nil` 时 `why` 一定非空**（有用例钉），所以调用方没有"不知道谁批的"这种状态。
  - **错误语义（关键）**：`err != nil` 当且仅当**判定不了**（`PrincipalID`/`Action` 为空、
    读不到 settings 的紧急停止位、读不到 policy_rules / temporary_grants、pattern 匹配不了）。
    此时返回的 `eff` **恒为 `DENY`**，`why` 为 `""`。**约定：`(DENY, "", err)` 不是"被拒绝"，是"不知道"，
    调用方不得当成 DENIED 记审计了事——要么修好数据源要么停下。** 真正的拒绝一定带非空 `why`。
  - **不要绕过它**：任何"我先自己看一眼 approvals 表 / 我先按 action 猜个风险"都算 §9.2 违规；
    默认策略已经内置（空 `policy_rules` 也能判），没有"策略表还没建所以先放行"的理由。
- **REQ-T3 状态：audit 这一半已改完，db 那一半还没落地**。11:31 实测 `internal/platform/db/redact.go:87` 的
  `RedactionHit` 仍是 `{Column, Match, Count}`、**无 `Kind`**。我已把 audit 侧改成契约给的新形状
  （`{Column; Kind HitKind; Match; Count}`，`HitKind ∈ {key,pattern}`，JSON 也跟着变）。
  唯一还在猜前缀的地方被隔离进 `approval.store_sqlite.go:toAuditHits` 一个函数、附注释点名它必须被删：
  db 一给 `Kind`，那里就是 `Kind: h.Kind` 两行。**在它改掉之前，任何已落库的 audit 行都属于"归因可能错位且事后无法修正"
  的窗口**（该串参与 `entry_hash`），所以这一条不要在 `000004` 之前用来做长期数据。`test` 的
  `TestRedactionCountersAreSealedBeforeHash` 是护栏。
- **policy 事件（T-23 末段）**：`policy_created`/`policy_updated` 我确认已在 catalog（`events.go:50-51`、已登记 `Registered`）。
  但**`Decide` 不写策略、也不改策略**，它是纯读判定，没有可发的"策略变更事件"——要发这两个事件必须有人先能
  **写 `policy_rules`**，而全仓今天没有这条写路径（`AggPolicy` 在 db 注册表里，但没人建行）。
  我没有按猜测去建 policy CRUD：那是 Wave-3 Permission Engine 完整版的活。你点头我就写（约 60 行 + 用例，归我包内）。
- **→ `backend`**：只能调 `approval.Service.BlockingState(executionID)`；`approvals` 表对你不可见。
  `BlockingState` 返回值域是 `PENDING / APPROVED / REJECTED / EXPIRED / CANCELLED / NONE`
  （`NONE` = 该 execution 没有绑定任何审批，可继续）。
- **→ `test`**：`redactTracking` 那份 test-only 镜像**仍需替换**——但注意：真执行点已经可用，
  `internal/approval/store_sqlite_test.go` 已经在真库上跑真 `InitRedaction`+真 `Tx.Append`（16:52 绿），
  可以直接把它的断言搬过去当 contract test。另外把两条不变量纳入你的清单：
  **(a) 任何覆盖 `entry_hash` 的列被改写前，必须先把 hash 算在改写之后**（`Tx.Append` 的 JSON 重编码会改字节）；
  **(b) 一个事务共用一个 `audit.Chain`**（两个 Chain 各自读 head 会撞 seq）。
  这两条我今天都是先撞红再修的。`internal/approval/sql_locality_test.go` 已把契约 §3.1 的
  grep 变成可执行检查并带自检用例（`TestScanActuallyFindsViolations`，防止扫描器坏掉导致假通过）。
  db 的 `InitRedaction` 落地后，请把 `redaction_test.go` 里那份 **test-only 镜像** `redactTracking` 换成对真入口的调用并删除镜像
  （文件内已注明）。
- **文档观察（不阻塞，仅记录）**：`orchestration.md` 与任务书都写「§12 P0 十条」，实际 §12 列了 **11** 条。

## Changed files

全部为本角色独占路径下的新建/自改；**未读改任何他人文件的写入**：

- 第 1 轮新建：`core/internal/audit/redaction.go`(127) `redaction_test.go`(313)、
  `core/internal/audit/hashchain.go` `hashchain_test.go`(177)、
  `core/internal/permission/permission.go`(300) `decide_test.go`(274)、
  `core/internal/approval/approval.go`(373) `approval_test.go`(493) `sql_locality_test.go`(111)
- 第 2 轮新建：`core/internal/permission/adapterargs.go`(65)、`adapterargs_test.go`(62)
- 第 3 轮新建：`core/internal/audit/append.go`(约 150)、`append_test.go`(约 145)、
  `core/internal/approval/store_sqlite.go`(约 330)、`store_sqlite_test.go`(约 380)
- 第 3 轮自改：`internal/audit/hashchain.go`（`Entry.Signature`）、
  `internal/approval/approval.go`（`Draft`/`Resolution` 加 CorrelationID/CausationID；`Store` 文档）
- 第 2 轮自改：`internal/audit/hashchain.go`（outcome 常量 + §5.4/§5.5 details 键约定）、
  `internal/approval/approval.go`（`Store` 文档按 §1.1 第 6~9 条校准，纯注释）、
  `internal/audit/hashchain_test.go`（用 `OutcomeDenied` 常量替换字面量）
- `.qoder/progress/audit.md`（本文件）
- 合计 11 个 .go 文件 / 2441 行

`git status --short` 同时列出的 `core/internal/{platform/db,task,execution,session,eventjournal}` 与
`.qoder/progress/{backend,db,orchestration,survey}.md` 属其它角色，我一个都没写。

## Verification evidence

命令均在 `D:\Project\Lucero\core` 下以绝对路径 Go 执行，`CGO_ENABLED=0`（无 C 编译器，ADR-016）。

1. **验收命令**：`CGO_ENABLED=0 D:\DevTools\go\bin\go.exe test -count=1 ./internal/audit/... ./internal/permission/... ./internal/approval/...`
   → 三包全部 `ok`；`-v` 统计 **133 个 `--- PASS`**、**0 个 `FAIL`**。
2. **全仓无回归**：`go test -count=1 ./...` → approval/audit/execution/permission/platform/db/task 全 `ok`
   （`eventjournal` no test files）；`go vet ./...` → 无输出。
3. **哈希链篡改可发现**：`TestRewriteIsDetected` 改 6 个字段 + 1 个「相邻字段间挪字节」用例（专打无长度分帧的实现）、
   `TestReSealAfterRewriteIsStillDetected`（改了字段又重算本条 hash → 下一环的 previous_hash 咬不上）、
   `TestDeletionIsDetected`（删中间 3 处 + 删尾部 + 清空）、
   `TestOutOfOrderAppendIsRejected`（时间倒挂 / seq 重复 / seq 跳号 / 首条 previous_hash 伪造）。
   ⚠ 结构性结论：**哈希链自身无法发现「删掉最新若干条」**——截断后的链内部依然自洽。
   因此 `VerifyChain(entries, head)` 强制带一个 `Head{Seq,Hash}` checkpoint，须由调用方存在链外（
   `audit_entries.signature` 或签名 checkpoint，见 Remaining）。测试里显式写了「不带 checkpoint 时截断检不出、
   带 checkpoint 才能检出」两条，避免后人误以为 hash chain 包打天下。
4. **fail-closed**：`TestRulesValidateIsFailClosed` 注入 `([unclosed` → `Validate()` 报错且错误串点名该 pattern；
   Marker 空 / DenyKeys 空同样报错。测试同时证明出厂规则集本身可通过。
5. **脱敏行为**：`TestDenyKeysAndVaultRefs`（deny key、大小写/连字符变体、嵌套 map/array、自由文本内嵌密钥）、
   `TestPatternsCatchCommonSecretFormats`（13 种真实密钥格式）、`TestPatternsDoNotOverMatch`（7 条不误伤）、
   `TestEveryDenyKeyIsLive`（**每个** deny key 用 pattern 抓不到的值单独验证，防止某条规则形同虚设）、
   `TestSecretsNeverReachAuditDetails`（`sk-…`+`ghp_…` 喂进 payload → 落库结果无原值、`vault://` 引用保留、
   Audit details 只有 column/key/count 且恰 2 次命中）。
6. **本轮修掉的安全缺陷**：`vault://` 若只做前缀判断，`"vault://x sk-<密钥>"` 会整串跳过脱敏。
   `IsVaultRef` 收紧为「裸 token」，`TestIsVaultRef` 7 例 + smuggle 用例锁死；db 侧同源缺陷见 REQ-A2。
7. **Decide 链序**：`TestDecidePriorityChain` 31 例逐 rung 证明，关键条目：
   `emergency stop beats global allow`、`deny at a low scope still beats global allow`、
   **`temporary grant cannot overturn a deny`**、`trust domain beats project`、`project beats task mode`、
   `task mode beats session override`、`session override beats adapter scope`、
   `higher priority wins in the same rung`、`priority tie falls to the stricter effect`、
   `unreadable policy is deny` / `unknown emergency stop is deny`（取不到状态一律不放行）、
   `missing action/principal is rejected`。另有 `TestCheckTemporaryGrant`（9 例）、
   `TestGrantActiveBoundary`（含 `now == expires_at` 即失效）、`TestMatchPattern`（15 例）。
   实现细节：`path.Match` 的 `*` 不跨越 `/`，会让 `repo:*` 静默匹配不到 `repo:src/main.go`（**权限静默失效**），
   故 pattern 自己实现为「字面量 + `*`」，用例 `{"repo:*","repo:src/main.go",true}` 钉住。
8. **Resolve 幂等**：`TestResolveIsIdempotent` 连调 3 次 APPROVED → 状态写 1 次、事件 1 个，
   随后用 REJECTED 再调 → 状态不变且不再产生任何写。`TestResolveLosesTheRaceWithoutDuplicates`
   用假 CAS 模拟双击/并发：落败方 0 事件 0 grant。幂等由两层保证：Service 先查 `Resolved()`，
   Store 再以 `WHERE state='PENDING'` CAS（真库并发靠后者）。
9. **Approval 不得错绑 Execution（P0）**：`TestUnboundApprovalIsNeverApprovedForAnExecution` —
   假 Store 故意「无视条件返回全部行」，`ForExecution("e-1")` 仍须排除 `execution_id` 为空与属于 `e-999` 的行
   （Service 侧二次过滤，不信任查询）；`TestBlockingState` 5 例覆盖 pending 压制 approved、
   已过期未 sweep 的行既不阻塞也不算批准、多条已决取最近一条。`BlockingState("")` 直接报错。
10. **Temporary Grant 真的落库**：`TestApproveWithTemporaryWritesGrant` 断言 grant 行带
    `scope_type=TASK`、`scope_id=t-1`、`expires_at=now+3h`、principal 从 `request_json` 复原、`effect=ALLOW`、
    action 继承审批的 action。`TestResolveRejectsBadResolutions` 9 例拒掉
    `TASK_MODE` 授权（`temporary_grants` 的 CHECK 没有该值）、无 scope_id、时长 ≤0、超 8h 上限、
    REJECTED 却要授权。回收：`TestSweepExpired` 证明只翻 PENDING 且过期的行，
    **不碰已决行**（碰了就是伪造状态），并同步 `RevokeGrants`；读侧 `Grant.Active`/`Approval.Live`
    独立拒绝过期行 → sweep 失败只会多问一次，不会留下权限。
11. **approvals SQL 局部性**：契约规定的
    `grep -rn "FROM approvals\|UPDATE approvals\|INSERT INTO approvals" core/internal --include=*.go` → **无命中**（exit=1）。
    另外把这条做成可执行测试：`TestApprovalsSQLStaysInThisPackage`（正则更宽，含大小写与 `insert\s+into`）+
    `TestScanActuallyFindsViolations`（合成 3 处命中、2 处违规，证明扫描器与分类都在工作，不是空过）。
12. **依赖**：只用到标准库 + 已在 `go.mod` 的 `github.com/google/uuid`（`uuid.NewV7`，
    `TestRequestMintsDistinctUUIDv7` 断言 `Version()==7`）。**未执行任何 `go get`/`go mod tidy`**；
    uuid 在 go.mod 里仍标 `// indirect`，归主控处理。第 3 轮起 approval 包**已在真库上跑**，仍不需要新依赖。

14. **真库集成（第 3 轮，`store_sqlite_test.go`）**：夹具 = `db.Open` → `Migrate(../../migrations)` →
    `InitRedaction(audit.Rules())`；FK 父行用 `Tx.Create` 搭（principals 无聚合路径，直插）。
    - `TestStoreWritesAggregateEventsAndChain`：`approval_requested` 1 条 / `approval_resolved` **1 条**
      （连调 2 次，且第二次是反向的 REJECTED，也没翻案）/ 该聚合共 2 个事件；`approvals.version` 1→2；
      resolved 事件 `aggregate_version=2`；`temporary_grants` 行 `TASK/t-1`、`expires_at=itNow+3h`、
      `effect=ALLOW`、principal 从 `request_json` 复原；audit 行 2 条且 `VerifyChain(读回的行, head)` 通过，
      改 `outcome` 或删最新行后 `VerifyChain` 必须报错（**同一批字节既是真库内容又是被 hash 覆盖的内容**）。
    - `TestStoreRedactsSecretsBeforeTheyLand`：把 `sk-…` 与 `ghp_…` 塞进 `Draft.Detail` 走真写入 →
      从库里 `SELECT request_json` **两个原值都不在**、marker 在、`vault://openai/prod` 完整保留；
      另有 `action="redaction.approval_requested"` 的 audit 行，实测内容
      `{"redactions":[{"column":"approvals.request_json","count":1,"key":"apikey"},
      {"column":"approvals.request_json","count":1,"pattern":"gh[pousr]_[A-Za-z0-9]{20,}"}],"schema":1}`
      —— 只有列名/规则名/次数。这就是 Q3 要求的形状。
    - `TestStoreNeverBindsAnUnrelatedApproval`（真库）：两条已 APPROVED 的审批（一条 `execution_id` 为 NULL、
      一条属于 `exec-2`）→ `BlockingState(exec-1)=NONE`；`exec-2` 才读到 APPROVED。
    - `TestConcurrentResolveSingleEvent`：6 个 goroutine 同时 Resolve 同一单 → `approval_resolved` **仍只有 1 条**、
      `version` 只到 2。这是幂等在真并发下的证据，不再只是假 Store 的计数。
    - `TestStoreExpirePending`：到期的变 EXPIRED(v2)+1 条 `approval_expired`；无 deadline 的不动；
      **已决行不被改写**；再 sweep 一次不产生第二个事件。`SweepExpired` 如实返回
      `ErrGrantRevocationUnsupported`（B3），不是悄悄成功。
    - `TestStoreGetMissing` → `ErrNotFound`。
15. **audit 写侧单测（`append_test.go`）**：链头咬合（`previous_hash`/`entry_hash` 落在正确的列位）、
    缺 `audit_id`/`action`/`outcome` 的行**不触达 appender**、`ErrChainFork`（预测 seq≠实存 seq）、
    insert 错误上抛、`RecordRedaction` 无命中不写行 / 有命中只写名字与次数。
20. **变异验证（第 4 轮）**：把 `IsVaultRef` 临时改回"只看前缀"→
    `TestIsVaultRef` 4 例红、`TestVaultRefDoesNotExcuseEmbeddedSecrets` 3 例红、
    `TestDenyKeysAndVaultRefs` 报 `secret leaked into output`、集成子测试报
    `audit's rule exempts a key wearing a reference`；还原后三包 `ok`，
    `grep -c MUTATION internal/audit/redaction.go` → 0（无残留）。
    → 这几条断言是**能失败的**，不是摆设。
16. **全仓无回归**（16:52）：`go test -count=1 ./...` → 11 包含新出现的
    `adapter`/`quota`/`session`/`test` 全 `ok`。我上一轮报的 `internal/session` vet 红已消失。
    契约 grep 命中的**三个**文件（`store_sqlite.go`、`store_sqlite_test.go`、`vertical_slice_test.go`）
    全在 `internal/approval/` 下 ⇒ 合规；
    `TestApprovalsSQLStaysInThisPackage` 自动判的就是这个，不靠人眼。
13. **§9 绕审批开关门禁**（第 2 轮新增，`TestCheckAdapterArgs*` 3 test / 40 断言）：
    - 15 个必须拒的开关逐个验证命中，含契约点名的三个真物：`--auth none`（WorkBuddy）、
      `--dangerously-bypass-approvals-and-sandbox`（Codex）、`bypassPermissions`（ZCode/WorkBuddy）；
      另覆 `--auth=none`、`--auth-mode none`、JSON 形态 `{"auth":"none"}`、`--no-sandbox`/`--disable_sandbox`/
      `--skip-sandbox`/`--sandbox=off`、`--auto-approve`、`--no-approval`、`--yolo`、`--permissions=bypass`。
    - 20 个普通参数必须**放行**（`--model gpt-5`、`--sandbox`、`--sandbox=docker`、`--auth token`、
      `{"auth":"oauth","sandbox":"docker"}`、`--approvals-file`、`--authorization-mode default`、路径与 `--timeout` 等）
      ——一把把所有启动参数都拒了的门禁不是安全控制，是把 FakeAdapter 垂直链打死。
    - 两 token 形式 `--auth` + `none` 分开传也要拒（返回 `--auth`）；空参数集返回空。
    过程红→绿三次（逐 token 漏两 token 形式、命中归属到无辜前一个 token、`--sandbox=off` 分隔符）
    都写进了 Completed，**不是事后补记的"已完成"**。

## Recovery note

- Interruption point: REQ-T3 我这半闭合（12:07:52 三包全绿）。
  Coordination 现状（12:08 复核）：**已关** REQ-A2（db `isCleanVaultRef`）、REQ-A3（两侧 RedactionHit 同形）、
  REQ-A4⑤（§3.1 裁定并已落地）、REQ-A7（Kind 字段已落，我这半已跟上）、B3（AggGrant + 000003）。
  **仍开**：REQ-A6① `Tx` 无事务内读（我的链头与三处"先读再改"靠单写串行化 + seq 自检兜，能错但会回滚）；
  REQ-A6② `principals` 不是聚合、没有 Create 路径（两份测试夹具因此直插）；
  REQ-A5 §9 门禁的执行点（在 adapter/process 包，等它们接）；checkpoint 签名"Head 存哪儿/谁签"待裁；
  REQ-A1 残余只是"摊平参数无类型约束"，生产接线已取 `audit.Rules()`。
- **自述纪律（第 6 轮新增）**：本轮第一次出现"报告里有据可查的错话"（声称已删的变量其实还在）。
  规则改为：**凡是"我删掉了 / 我改过了"这类否定性声明，必须附一条 grep 或实跑输出**，没有就不写。
- **流程账**：C2 指出的「先干活后建账」成立，本角色此后一律**先落进度文件再动代码**；
  同时留一条事实：本文件 16:13 已在盘上（17697 字节），16:20 主控复核时也在——违规在**顺序**，不在缺失。
- First action after recovery: 先跑
  `cd /d/Project/Lucero/core && CGO_ENABLED=0 D:/DevTools/go/bin/go.exe test -count=1 ./internal/audit/... ./internal/permission/... ./internal/approval/...`
  确认仍绿（别的角色在并行改 `core/internal/`、`cmd/`、`api/`，红点要先归因再动手），
  再看本文件 Blockers / Coordination 有没有被主控回话。我这边没有未阻塞的自主项。
- 已闭合、不要再当缺口报的：三包规则/判定/聚合层、§9 门禁、真库 Store 与哈希链持久化、
  `permission.Source` 读真库、垂直链切片、REQ-T3 带标签命中记录（两侧同形 + 跨包哨兵）。
- 第 3 轮**已**有真库证据的事：`approvals`/`temporary_grants` 落库、`approval_requested`/`_resolved`/`_expired`
  事件与版本、`audit_entries` 哈希链持久化与校验、写入前脱敏 + 命中记录。
- 仍**没有**证据、不得推断其存在的事：① `audit_entries.signature` 的 checkpoint 签名（未实现，且
  "Head 存哪儿/谁签名"未裁定——**没有它，尾部删除仍检不出**，`VerifyChain` 只能证明链内自洽）；
  ② §9 门禁在 adapter 启动路径上的实际拦截（我只交付判定与拒绝形状，执行点在别人包里）；
  ③ §5.4/§5.5 的投影与 UNKNOWN 行为（属 backend，我只交付了 outcome 常量与 details 键约定）；
  ④ `policy_rules` 的写路径（T-28③ 判不建，故策略表今天只能靠手工/夹具）。
  任何"审计已防篡改"的说法在 ① 落地前都不成立。

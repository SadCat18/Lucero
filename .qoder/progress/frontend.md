OWNER_ROLE: frontend
TASK_ID: api-client-panel
STATUS: in_progress
SCOPE: `apps/desktop/**`、`packages/api-client/**`、`specs/openapi/**`（契约③ §7 原文；禁止 import Core 内部类型）
EVIDENCE_DIR: .qoder/evidence/frontend/
LAST_UPDATED: 2026-09-22T23:45:26+08:00（T-144 子轮：§6 词干按契约字面改成"第一个全名" + 歧义守门 E/F；
闸门 **23:42:43** 五步全 exit=0（`gates-t144.txt`）；T-140 的 live **23:37:11 16 passed / 0 skip** 仍指当前代码：
23:37:11 之后只有 `gen/schema.ts`（codegen 自写）与 `validate-openapi.mjs`（23:42:21）被动过，运行时代码与 yaml 未变。
基线读到 `a1b8664`→`e4b214b`。更早各轮（T-139 23:08:53 / T-127+T-128 15:37:42）的"最后一次重跑"口径按原段读，别拿本轮数字覆它们）

> **外接结论（23:44 读到 `specs/adapter-protocol/**` 里一条在途新增的 C8-12，不是我改的）**：
> 那条写"对拍看不见**解析器少收东西**这种失效，它只看得见两边不一致"，并要求判据必须两条腿：
> ①集合对拍 + ②喂合成反例给同一个判定函数。我这台的形状已经是两条腿：
> ① `EventType enum ↔ §6 目录双向相等 62 类`；② 探针 C（孤儿 `_xxx` 必须报错）/ D（词干展开出的是哪个名字）/
> E（两个全名词干不同 + 有简写 ⇒ 停下来）/ F（整行写全的行必须放行，别误杀约定④）。
> 今天 §15.2 的行没被那次编辑动到，所以守门没有受在途改写影响（23:42 与 23:44 两次解析都是 30 行 = 17 + 13）。

## Objective

契约③ → `specs/openapi/core-v1.yaml` 的机器可读投影 + 「yaml → TS 类型」codegen 链路 +
只能靠 REST/WS 渲染契约① §7 那条 11 步垂直链的 Vite + Vue3 + TS 观察面板；UNKNOWN 任何分支都显示「未知」。

## Completion criteria

- [x] `cd /d/Project/Lucero/apps/desktop && npm.cmd run build` 无类型错误（**12:52:25 最后一次重跑 ✓ built in 390ms**）
- [x] yaml 与 18:03 版 §3/§4 + §2.1 + F-9/F-16/F-17 裁定一致（36 path / 41 operation，validator 双向判 + §4 交叉判）
- [x] TS 类型全来自生成物（本轮又清掉一处手写帧联合类型）
- [x] 断言 19 绿 + 变异验证累计 6 次全咬人（**补记 09-21T13:42：现为 25 passed | 10 skipped；本轮那次磁盘变异没咬人，
  已还原并改用进程内变异 —— 见下文 T-96 段与 `t96-mutation.txt`，"累计全咬人"这句当时是对的、现在读要小心**）
- [x] **真 Core REST 面联调 5/5 绿**（12:46:16，`live-core.txt`）+ 真浏览器被真 Core 驱动（`browser-live-core.txt`）
- [x] WS 面正向证据 — **已解除**（原记 blocked 于 REQ-F-17）：09-21 **11:22** `live-t76.txt` 里
  `Test Files 1 passed (1) / Tests 9 passed (9)`、**0 skip**，三条 WS 断言（hello_ack 抬游标 / 票单次使用被拒 / F-9 三径）都是正向帧证据。
  默认无 Core 时整份 skip，skip 不计入通过。
  **一处证据缺口，如实标**：T-80 轮台账写的是"加分页断言后 `Tests 10 passed (10)`（11:56:46）"，但那次**没有单独留 evidence 文件**
  （`grep -rn "10 passed" .qoder/evidence/frontend/` → 零命中）。所以"10/10"目前只有台账可引、无原始输出可查；
  下一次跑 live 时随链补一份 `live-t96.txt` 并在此回填路径 —— 现在这份证据文件里可查到的最强数字是 **9/9**。
- [ ] §4 的 11 步链 —— **blocked**：除 health/ws-ticket 外全部 501 not_implemented（实测确认）

## Scope boundaries

- In scope：§7 那三行 + 本角色 progress/evidence/.scratch
- Out of scope：契约其余文件与 docs、`core/**`（本轮只读源码取证，未改一行）、Tauri/Rust、五家真实软件、git 写、PATH/全局

## Current checkpoint

- Stage: T-30 四条裁定 + F-16/F-17 归因全部落地；REST 面真联调绿，WS 面等 backend 一行修复
- Status: in_progress
- Last verified action: 本机时间 **2026-09-20 12:52:25 +0800** 四道闸门重跑全绿（validate 全部通过 / codegen 69.9ms /
  vitest 19 passed + 8 skipped / build ✓），是在「F-16 变异 → 2 红 → 绝对路径回滚 → diff 干净」之后跑的；此后只改了本文件
- Next action: 等 REQ-F-17 修好 → 跑 `LUCERO_CORE_ENDPOINT_FILE=… LUCERO_WS_READY=1 npx vitest run src/__tests__/live-core.test.ts`
  取 WS 面正向证据（客户端 hello、票单次使用、hello_ack 抬游标三条已写好）
- Verification: `cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`

## Completed

### 第一至三轮（17:34 / 17:56 / 12:10 三次报绿，细节见旧版历史）
yaml 首版投影（§2/§3/§4/§5 + 契约① §6/§7 + 契约② §5/§7）；api-client（client/events/unknown）；面板骨架 8 组件 + store；
§2.1 短票（每次重连换一张）；discover 改 adapter 侧；`packages/toolchain` 按裁定删除并入 desktop；
F-1 send body 逐字（删我自造的 model_id、补 attachments）；F-8 三个读端点 + runChain 改用 `GET /tasks/{id}`；
F-9 `hello_ack`；F-11 幂等豁免集合相等断言；F-3 bootError 诊断位；§4 交叉核对断言。

### 第四轮（T-30 + F-16/F-17，本轮）
1. **§15.2 错误码闭集**：`ErrorCode` 从「枚举+pattern 兜底」改成**纯闭集 19 条** = §15.2 左列 15 条 + Core transport 自有 4 条
   （`unauthorized/origin_not_allowed/method_not_allowed/not_implemented`，实测自 `api/http/handler.go` 的 writeError）。
   顺带删掉我原先自造的 `illegal_transition`、`aggregate_not_found`（§15.2 里没有 → 不该存在）。
   validator 五条断言：闭集无兜底、15 条全在、多出的必须属于 transport 四条、全小写蛇形、`error.code` 指向它。
2. **§2.1/T-30 短票三条**：yaml 里 ws-ticket **免 Principal**（去掉该参数）+ 仍要求 Bearer + 成功码 **200（实测）**；
   validator 加了「免 Principal 集合相等」+「200 在位」+「ws-ticket 不许吃 Idempotency-Key」三向断言；
   client 加 `noPrincipal` 通道，只对这一条不发该头。
3. **`Health` 词表**：`state` 闭集 `BOOTING/READY/STOPPING/STOPPED/UNKNOWN`（**无 DEGRADED**）+ `ready` 与 `state==READY` 的关系写明；
   StatusHeader 改走 `statusText()`，真 Core 若在 boot 期漏 DEGRADED，面板也不会替它圆场。
4. **F-16 归我**：`openEventStream` 在 `onopen` 里**先发 client hello**（无游标 → `{"type":"hello"}`；有游标 → 带 `from_seq`），
   yaml 新增 `WsClientHello` + `x-websocket-frames.client_frames/order`，`known-gap` 显式记 F-17；
   两条新断言钉住发送形态。`migration_version` 按实测改 integer|null。
5. **REQ-F-17 的客户端防御**：`hello_ack.server_seq` 只在**大于**当前游标时才抬（源码里那句"低于游标就不抬"的判断我第一轮已经写对了，本轮补断言）。
6. **live-core.test.ts（真联调面）**：`describe.skipIf(!live)` + WS 半边另加 `LUCERO_WS_READY` 锁 ——
   没跑过就记 skipped，绝不记 passed。**实测 5/5 绿**：health 真值 / 无口令 401 `unauthorized` / 未实现端点 501 `not_implemented` retryable=true /
   回环 origin 放行 + 非回环 403 `origin_not_allowed` / 两次换票得到两张不同票且 `expires_in=30`。票值全程掩码。
7. **真浏览器 + 真 Core**：面板头部显示 `Core READY v0.1.0-dev / mig 3`（进程真值），`data.bootError=""`；
   无口令时 `POST /ws-ticket` 被真 Core 拒（"缺少或错误的共享口令"），面板如实显示换票失败并退避，**没有假装连上**；
   顺带暴露 **REQ-F-18**（见 Coordination）。我为不阻塞联调加了一条 dev 期入口 `?core_token=`（不落 storage、不进日志、源码里注明等裁定），
   没有发明鉴权方案。
8. **归因取证**（不是我猜的）：探针显示握手 101 成功（票被接受）→ 我方 hello 已发 → 服务端 4 秒零帧 → close 1006；
   `grep -rn hello_ack core/api/websocket/*.go` **零命中** ⇒ F-16 的"不带游标回 hello_ack"与 F-17 的空 journal 分支都还没实现。
   所以 WS 三条保持 skipped 而不是硬凑绿。

### 环境/流程事实（供其它角色复用）
- 起 Core：`cd /d/Project/Lucero/core && D:/DevTools/go/bin/go.exe run ./cmd/core -data-dir ../.scratch/frontend/coredata -migrations migrations`
- `core.endpoint` 实测是 **JSON** `{"host_port","token","version"}`，不是 key=value 行（我照 backend 文档写的解析器当场报错，已改）
- `timeout` 到期是 SIGTERM（bash 报 exit 143），Core 的关停日志会走完；见 OBS-F-1 的残留观察

## Remaining

- WS 面正向证据（等 F-17）；§4 的 11 步链（等业务端点脱掉 501）
- REQ-F-14/17/18 与 LAG-1/LAG-2 的契约文本回写

## Blockers

- **B-F1（半解除）**：REST 面已通（health + ws-ticket 真值；其余 501 实测确认）→ 11 步链仍跑不了。
  解除条件：projects/adapters/sessions/tasks/executions/approvals 端点实现。
- **B-F3（新）** WS 面控制帧：Core 不回 `hello_ack`（源码 grep 零命中 + 探针 4 秒零帧）。
  我的 live 测试已写好，`LUCERO_WS_READY=1` 一开即验。解除条件 = backend 修 F-17。
- ~~B-F2~~ PNG 推 Wave-3。

## Coordination

已闭：F-1、F-2、F-3（口径）、F-4、F-5、F-6、F-7、F-8、F-9、F-10、F-11、F-16（我的错，已修）。

> **补记（2026-09-21T13:38:36+08:00，T-96 轮）**：下面 F-17 / F-18 两条是**当时的原始记录，已不是当前状态** ——
> F-17 由 T-76 轮在自属实例上实测解除（A/B 对照，见 11:23 段）、F-18 由 T-53 轮交付；
> F-19 / F-20 / F-21 / F-22 全部关闭（F-22 见本轮段）。原始行不删不改（T-43），别再拿它们当阻塞读。
> 当前待裁项只剩 **LAG-1**（§3 缺 F-8 批准的三个读与 `?project=`）与 **OBS-F-1**（core.lock 存活判定，归 backend）；
> **REQ-F-14 与 LAG-2 已于 09-22 由 T-127/T-128 关闭**（enum 29 条三方对拍；§5 歧义句已重写、F20-4 用例 `helloAckAdvancesCursor` 已建）。
> **命名约定（同批下发）**：本台账 09-22 之前写的 `契约① §2/§5/§6/§7/§8` 全部指**事件契约**（内部契约当时还没有那些节）；
> 今后 `契约①` 单写指 `contracts-internal-v0.1.md`，事件那份一律写作「事件契约」—— 历史行不回头改。

- **REQ-F-14（要裁）**：`ErrorCode` 闭集必须覆盖线上真会出现的码，故我在 §15.2 的 15 条之外并进了
  `unauthorized / origin_not_allowed / method_not_allowed / not_implemented`（全部实测自 Core 的 writeError）。
  请把它们按 version_conflict 那种"HTTP 层专有、不经 Adapter"的形态收编进 §15.2，或指出别的落点。
- **REQ-F-17（等 backend）**：空 journal 时服务端不出任何控制帧；且 `api/websocket/` 里根本没有 `hello_ack` 写入点
  （grep 零命中），即 F-16 的"不带游标 → hello_ack{server_seq}"也未实现。修好我这边一个 env flag 就出证据。
- **REQ-F-18（要裁）**：F-3 定了"凭据由 client 自己走 Bearer/ticket、dev proxy 不代持"，但浏览器读不到 `core.endpoint`，
  ⇒ 面板没有任何**正式**途径拿到 core-token 给 `/api/v1/*` 用（真 Core 已实测拒绝）。
  需要一条正式口径（短时 REST 票？dev 期显式 `?core_token=`？桌面端注入？）。我暂时用后者解阻塞，并在源码注明是临时件。
- **LAG-1**：§3 仍无 F-8 批准的两个详情读与 `?project=`（yaml 已按批准补，validator 钉着）。
- **LAG-2**：§5 仍无 `hello_ack` 与"客户端先报游标"的表述（F-16 修正已下发但 §5 文本我看到的还是旧歧义句）；契约① §2/§8 同滞后。
- **OBS-F-1（交 backend/主控，不属我所有权）**：`timeout 400 go run …` 到期发 SIGTERM（`exit status 143`）后，
  数据目录里 `core.endpoint`（内容仍是本轮 `127.0.0.1:64487`，故非上一轮垃圾）与 `core.lock` **都还在**，
  与"异常退出后不再残留"的说法不符。但 `timeout → go run → core.exe` 的信号转发在 Windows 上不等价于真 Ctrl-C，
  **不能据此判它有问题**，需要 backend 自己用可观察的 SIGTERM/Ctrl-C 复测一次。我把命令与现象留在 `browser-live-core.txt`。

## Changed files

本轮：`specs/openapi/core-v1.yaml`（ErrorCode 闭集 / ws-ticket 免 Principal+200 / Health 词表+migration_version /
WsClientHello / client_frames / order / known-gap）、`packages/api-client/src/{client.ts,events.ts}`、
`packages/api-client/src/gen/schema.ts`（重生成）、`apps/desktop/scripts/validate-openapi.mjs`、
`apps/desktop/src/{stores/panel.ts,components/StatusHeader.vue}`、`src/__tests__/{chain.test.ts,live-core.test.ts}`。
依赖零新增。

## Verification evidence

- `gates-final.txt` —— **本机时间 2026-09-20 12:52:25 +0800**：validate `全部通过`、codegen 69.9ms、
  vitest `2 passed | 1 skipped` / `19 passed | 8 skipped`、build `✓ built in 390ms`
- `live-core.txt` —— 12:46:16 真 Core：**REST 5 passed / WS 3 skipped**，含 `两张票不同 ***(len 43) vs ***(len 43)、expires_in=30`、
  探针归因（101 成功 → hello 已发 → 4008ms 零帧 → close 1006）
- `browser-live-core.txt` —— 真浏览器渲染真值 `Core READY v0.1.0-dev / mig 3`、`bootError=""`、无口令时换票被拒的如实降级
- 变异验证累计 6 次全部咬人：`num()` 折 0 → 1 红；`statusText()` 折 UNAVAILABLE → 7 红；换票失败谎报 live → 1 红；
  `hello_ack` 不抬游标 → 1 红；**本轮：连上不发 client hello → 2 红**。每次均绝对路径回滚 + `diff` 确认后才重跑取绿
- 进程清理：我启的 Core（timeout 自然到期）与 dev server（TaskStop）都已停，`netstat` 无 64487/5173 监听
- 凭据纪律：core-token 与票值只出现在被 gitignore 的 `.scratch/frontend/coredata/core.endpoint`，**未写入任何 evidence/progress 文件**，
  断言与日志一律 `***(len 43)` 掩码

**未主张**：WS 面（含票复用被拒、hello_ack、resync 回落）仍无正向端到端证据；§4 的 11 步链未跑通。

## Recovery note

- Interruption point: 四道闸门 + live REST 全绿；停在等 REQ-F-17 修复与 §15.2/§5 文本回写
- First action after recovery: `cd /d/Project/Lucero/apps/desktop && npm run validate && npx vitest run && npm run build` 确认基线没被动；
  若 backend 修了 F-17 → 重起 Core（命令见上）→ `LUCERO_CORE_ENDPOINT_FILE=… LUCERO_WS_READY=1 npx vitest run src/__tests__/live-core.test.ts`
  → 绿了再把面板 dev server 接上真端口做浏览器 11 步链

## T-43 时间戳自审（本机 date 真值 2026-09-20 13:01:22 +0800）

规则已接受并即刻生效：**写任何具体时刻之前先跑 `date`；没跑就写"未计时"**。
按 mtime / 当场命令输出把本账本已写的时间戳分三类（**不重写历史值**，只作废其中不可信的那一类）：

1. **`date` 真值，可信**：11:27:30、12:46:16（live REST 取证）、12:51:05、12:52:25（交付前重跑）、13:00:48、13:01:22。
   对应证据文件里都有当场 `date` 行。
2. **机器输出锚定，非 `date` 但可核**：17:08:02 / 17:09:51 / 17:10:17 / 17:15:45（vitest 自带的 `Start at`）、
   17:53:30（PowerShell 给的进程 StartTime）、17:53:34（vite 日志行）。这些不是我估的，是命令自己吐的。
3. **估写，作废**（当时未跑 `date`，只是照上面两类推算或凭手感）：**17:05、17:25、17:26、17:34、17:36、17:52、
   17:54、17:56、12:05、12:10、12:47、12:50、12:54、12:56**，以及正文里所有"17:3x 报绿 / 12:1x 交付"这类行文时刻。
   其中可被 mtime 事后核上的（如 17:52 的三份闸门输出、12:52 的 gates-final）**结论方向没错**，
   但核不上的那些（17:25、17:34、12:10、12:47、12:54）**只能作废**，不再当证据用。

**受影响的判断（我自己扫过的三处）**：
- "报绿必须是最后一次重跑"这条纪律：**不依赖时钟**。它依赖的是事件顺序 —— 变异 → 回滚 → diff 干净 → 再跑 → 绿，
  顺序由 tool 调用序列与 `gates-final.txt`（mtime 12:52）内部自记的 `date` 行背书。结论保留。
- 我对 backend/巡检说过的"你的时间戳不可用于判定 ≥2 轮"这类话：**同样要靠 mtime 或当场命令，不能靠我的估写**。已按此改口。
- B-F1/B-F3 的 blocked 判定：不依赖时间戳。本轮 13:00:48 当场复查
  `grep -rn hello_ack core/api/websocket/*.go` → **仍零命中** ⇒ REQ-F-17 未修、B-F3 维持 blocked（当场命令已附）。

**证据文件的一处自纠**：`browser-live-core.txt` 头部原写"12:47"是估写，已在文件内追加补注：
真实写入时刻以 mtime = 12:50 为准，文件内的观察内容不受影响。

## 收尾协议更新（写进本角色长期纪律）

- 任何时刻值：先 `date`，否则写"未计时（本机未跑 date）"。
- 需要"先后顺序"时，优先用可核物证（文件 mtime、命令自带的 Start at / 日志时间戳），不用我推算的钟点。
- 否定性声明（"仍然没有"、"零命中"）必须附当场命令与当场输出 —— 本轮的 F-17 复查即按此格式。

## T-47 /tmp 自审（本机 date 真值 2026-09-20 13:18:37 +0800）

点名的那句：**否** —— 我从没在 `/tmp` 顶层放过 `r` 开头的 `.json`（当场命令 `ls -1 /tmp/r*.json | wc -l` → `0`）。

但我在 `/tmp` 顶层放过别的东西，按新规矩同样不该：
`unknown.bak`、`events.bak`、`ev2.bak`、`ev3.bak` —— 全是变异验证时的源文件回滚备份（`.bak`，不匹配 `r*.json`，
所以 backend 那次通配删没碰到它们）。当场 `ls` 显示 09-20 那两个（ev2/ev3，11:57 与 12:52）还在，
09-19 的两个（unknown/events）已不见 —— **丢了不要紧**：对应变异在当场就已经"回滚 + `diff` 确认干净"才算完成的，
备份从来不是我的证据物，证据物是仓库里的源码现状与 `gates-final.txt`。

顺带一条给主控的观察（同病，不是我该处理的）：`/tmp` 顶层此刻还有 `cx.bak / m1.bak / p0.bak / px.bak / sv.bak`，
时间戳 13:07–13:11（我没在跑），**说明至少还有别的角色在往共享临时目录写同名风格的备份**。
我不去动它们，也不删我自己那三个 —— 按"谁都不许碰 /tmp"的字面，我不再对该目录做任何操作，
清理由用户或各角色自己在 `.scratch/` 侧负责。

以后我的做法（写进收尾协议）：一次性产物与变异备份一律落 `D:\Project\Lucero\.scratch\frontend\`；
删除只删自己建的具体文件名；永不对 `/tmp`、`%TEMP%`、`$env:TEMP` 做通配删除或"清理"。
本轮唯一保留在 /tmp 的操作是只读的 `ls`（取证用）。

## T-53 轮（凭据正式化 + 负向断言 + F-17 判定）· 本机 date 2026-09-20 15:08:16 +0800

**REQ-F-18 关闭**：按裁定把凭据从面板彻底拿走 —— `createCoreClient()` 不再接受 token、`?core_token=` 入口删除、
`CoreToken` 类型删掉，客户端只留 `extraHeaders`（供 node 侧联调与未来的 Tauri shell 用，`Bearer` 字面量因此不进产物）。
dev 期注入正式落在 `vite.config.ts` 的 proxy `configure` 里（只挂 /api；WS 仍走 §2.1 短票，代理不代劳）。
口令唯一来源 = 进程环境 `LUCERO_CORE_TOKEN`，不写 .env、不进 `import.meta.env`。

**T-53④ 两条负向断言已建并进 build**：`npm run build` = `vue-tsc --noEmit && vite build && node scripts/check-no-secret.mjs`；
带真值跑法 `LUCERO_CORE_TOKEN=… npm run check:no-secret` → 产物里扫不到 Bearer / core_token / LUCERO_CORE_TOKEN / 口令真值。
脚本注释写明为什么**不**扫 `x-lucero-principal`（§1.4 要浏览器自己带，进产物是对的）。

**T-53⑤ 口径核对**：我全部文件里 grep `0600\|仅本人可读\|受文件权限` → 零命中，无需撤稿；
已把 yaml 的 bearerAuth 说明改成"同用户边界内的门帘，不是隔离机制"，并在证据第 5 节写清凭据去向。

**REQ-F-17 已修（实测，带归因限制）**：A/B 对照 —— 不发 client hello → 4 秒零帧；发 → `{"type":"hello_ack","server_seq":0}`；
live 测试 8/8（含票单次使用、游标抬升、events.ts 实走）。**但那台 Core 不是我起的**（我 spawn 的那台撞锁退出），
所以我把"归属只到当时的构建"写进证据第 4 节，并放弃用他人实例宣称我自己的复现（第 6 节）。
其余 9 个业务端点仍 501 not_implemented ⇒ §4 的 11 步链依旧跑不了，B-F1 维持。

**当前唯一动作**（覆盖上文 Recovery note 的）：等一次干净窗口重跑 `node .scratch/frontend/live-driver2.mjs > out.txt`
拿到"我自己实例"的 8/8；在那之前 WS 半边按"他人实例已实测通过 + 自属复现未取得"记。

**待主控处置的两条**：① 我遗留的 core.exe（数据目录 coredata-self，曾 54615）需要认领后清掉，我不猜 PID；
② OBS-F-1 已升级为可复现故障（残留锁挡启动 + 锁文件被持有删不掉），建议 backend 给锁加存活判定。

## T-66 收尾协议补丁：变异验证的备份-回滚钉成硬规矩（本机 date 2026-09-20T16:08:31+08:00；本轮不改代码）

**先认账**：我在本会话确实有 **两次**（不是一次）回滚用相对路径失败 —— 一次在 `apps/desktop` 下写
`../packages/.../unknown.ts`、一次写 `../packages/.../events.ts`，`cp` 静默失败过。两次我随后都改用绝对路径重做并 `diff` 确认，
但**当时的"reverted clean"是在补救之后才成立的，规则上我应该第一次就带绝对路径**。这条我收下。

**顺带把两处事实纠正在账本里（不是为了甩锅，是账目要准）**：
1. 我 13:18 自报的是"往 `/tmp` 顶层写了 4 个 `.bak`"（写侧的卫生问题），**不等于**"变异版留在盘上"（读侧的污染）。
   后者是另一件事，需要另一份证据回答，我下面当场查了。
2. `/tmp` 里那 4 个 `.bak` 与我的变异版是否残留无关 —— 备份丢了只会让我少一个回滚手段，不会往仓库里放坏代码。

**当场证据（三条，命令与输出都贴）**：
- 决定性标记搜索（我历次变异的注释里都带"变异"二字）：
  `grep -rn "变异" --include=*.ts --include=*.vue --include=*.mjs --include=*.yaml apps packages specs` → **0 命中**
- 逐处回真身，三处变异各应存在 1 次：`握手前换票失败` = 1、`ws?.send(lastSeq === null` = 1、`f.server_seq > lastSeq` = 1 ✓
  变异特征 `return `0${unit}`` = 0、`return 'UNAVAILABLE';` = 0 ✓
  （注：`o.onState('live');` 计数为 3 是**正常代码**里 hello/hello_ack/ping 三个分支各一处，不是残留 —— 这种"合法也命中"的串不能当判据，
  要挑只有变异版才有的串。这一点算我给其它角色的一条附带经验。）
- 与库内版本比对：`git status --short packages/api-client/src apps/desktop/src` → **0 条**；
  `git diff --stat HEAD -- packages/api-client/src apps/desktop/src` → 空（只有 `specs/openapi/core-v1.yaml` 1 行未提交，是我 T-53 的 known-gap 改写）
  ⇒ **结论：盘上没有我的变异版残留**，这一条现在是证据而不是自述。

**写进我的长期协议（照 T-66 三条 + ④）**：
① 变异备份只落 `D:\Project\Lucero\.scratch\frontend\<具名>.bak`，一律绝对路径，`/tmp` 一个字节都不再写；
② 回滚后必须证明变异版不在盘上 —— 优先 `git diff --stat HEAD -- <被变异文件>` 为空，或 grep 一个"只有变异版才有"的串贴 0 命中；
   只报 `cp` 成功或 `diff` 两个临时文件相等都不算（那只能证明备份与盘上一致，不能证明盘上不是被改坏的那版）；
③ 拿不到这条证据就不许写"变异已还原"，改写成"未验证还原"；
④ 变异必须改变"是否走到被验证的那段代码" —— 藏进早退后面的变异等于没变异。
   自查我这五次：`num` 折 0 / `statusText` 折 UNAVAILABLE / 换票失败谎报 live / `hello_ack` 不抬游标 / onopen 不发 client hello，
   五次都产生了**断言级**的失败信息（如 `expected null to be 1200`、`expected 90 to be 7`、`2 failed`），
   不是"测试没跑"或 0 test —— 这一条我判定自己合格，但把判据写下来供别人复核。

## T-72 轮（无任务行；用了新证形，并补上自属复现）· 本机 date 2026-09-20T16:20:00+08:00

**证形采纳**：`git diff --stat HEAD -- apps/desktop specs/openapi packages/api-client`（基线取当轮 `git log -1 --format=%h` = `cd46e42`，不写死哈希）
→ 本轮我只动了 2 个文件：`specs/openapi/core-v1.yaml`（1 行）+ 重生成的 `gen/schema.ts`（1 行）。其余与库内逐字节相同。

### 1. 自属复现到手（补上 T-53 欠的那半份归属）
根因是我上轮用 `go run`：杀父留子。改成 **先 `go build -o .scratch/frontend/core-t72.exe` 再直接跑二进制**，
于是 PID 是我自己的、停得掉、停完还能证：`Get-Process -Id 37868 → False`、`-Id 36592 → False`（两轮实例都已停，无孤儿、无残留锁）。
自属实例（127.0.0.1:60627 / 63201）结果：live 测试 **8/8 绿**，含 WS 三条；A/B 对照
`A 不发 client hello → []`、`B 发 client hello → {"server_seq":1,"type":"hello_ack"}` ⇒ **REQ-F-17 判定不再依赖别人的实例**。

### 2. backend 已落地 `POST /api/v1/tasks`（test 的 U13-6 可以转硬）
    POST /tasks（带 Idempotency-Key）→ 201 {id:"01a0bde3-…", status:"CREATED", version:1, requested_by:"local-user", …}
    同键重放                      → 201 同一 id、同一 created_at ⇒ §2「返回首次的响应、不重放副作用」实测成立
    GET  /tasks                   → 200 {"items":[…], "next_cursor":""}
    GET  /projects /quota /sessions → 仍 501 not_implemented ⇒ §4 的 11 步链依旧跑不通，B-F1 维持
§6.1 对账（handler 实际字段 vs 我 yaml 的 Task）：必填项零缺失、无"handler 有而 yaml 没有"的字段；
`project_id/description/metadata/…` 等可选项在值为空时不出现 —— 属正常 omitempty，不构成漂移。

### 3. REQ-F-19 从"无法复现的观察"升为"已复现"，并撤掉我 yaml 里的越权断言
`next_cursor` 实测是**空串**（自属实例两次一致）。我原先在 yaml 写"无更多数据时为 null，不是空字符串"——
那是我自己的投影而非契约原文（§2 只写 `{items, next_cursor}`），现已改成"两种都可表示没有下一页，正式口径等裁定"，
不再拿我的偏好当判据。

### 4. 本轮我自己的两个错（都记下来）
- 用 node 改 yaml 时把 JS 语句结尾的 `;` 一起写进了单引号标量 → `validate` 与 `codegen` 同时炸（799 行）。
  教训：脚本改文件后**必须立刻跑一次机器校验**，别把"改完就是绿的"当默认。这次就是 validate 替我抓到的。
- 上一轮 `go run` 留孤儿 = 我这轮差点又造一个删不掉的锁；换成"编译后跑二进制"才闭环。
  与 T-66 同源：**我启的东西必须可证明地停掉**（附 PID + Get-Process 的 False）。

### 5. 闸门（本机 date 2026-09-20 16:18:57 +0800，此后未改代码）
`validate` 全部通过 / `codegen` 稳定 / `vitest 19 passed + 8 skipped` / `build + check:no-secret` 全部通过。
注：16:18:13 那次 validate 因上面第 4 条的分号而失败，修好后 16:18:57 重跑为绿 —— 报的是后者。

## T-76 轮（收口三件）· 本机 date 2026-09-21T11:23:16+08:00

**先纠一条我自己的陈旧**：上轮 Next action 写的是"等 REQ-F-17 修好"，而 T-49 早已把它派给 backend、
且我 09-20 16:14 就在自属实例上实测到过 `hello_ack` —— 条件当时已不成立，我没把账本推进，是我的陈旧不是等待。

① **三条 WS 断言已转正向证据（0 skip）**：`live-core.test.ts` 现 9 条全绿，对象是我自己起的 Core
（先 `go build -o .scratch/frontend/core-t72.exe` 再跑二进制，PID 归我，`Get-Process alive=False` + 数据目录删除为证）。
默认无 Core 时该文件整份 skipped → 报的是 `19 passed | 9 skipped`（skip 不计过），带 Core 那次报 `9 passed (9)`。
② **yaml `known-gap` 已从"未修"改成"已解除 + 时刻 + 三条路径实测"**，并写明旧声明作废（T-76②：留着已闭的 gap 会被下一个人当现状读）。
③ **F-9 两条语义服务端与我的断言一致**（journal 非空实测）：不带游标 → `hello_ack{server_seq=头}`；
hello 带 `from_seq=1` → 首帧 `hello{from_seq:1}` 回显 + 只补发 `seq>1`（实测 `task_created#2`）；
游标超头 → 零事件不回灌。`hello` 不动游标那条另在 chain.test.ts 用假帧钉住。

**新发现 REQ-F-20（要主控定文档口径，非阻塞我）**：`?from_seq=` 查询参数现在是惰性的 ——
只给 query、hello 不带游标 → 首帧 `hello_ack` 且不补发；游标放进 hello 帧才起作用。
风险是契约① §8 与契约③ §3/§5 原文写的恰是 `/ws/v1/events?from_seq=N`，照文档接线的第三方会**静默**只拿到此后事件、以为补发了历史。
我的面板两个都发所以行为正确；请统一口径：要么让 query 生效，要么在 §5 写明"起点只认 hello 帧"。

**REQ-F-18 已交付**（T-53 那轮，主控侧可能没收到：投递被熔断截过）：面板零持有口令、
dev 注入正式化到 `vite.config.ts` 的 proxy `configure`（只 /api；/ws 不注入仍走票）、
`check:no-secret` 焊进 build。可复核形状见 evidence `live-t76.txt` 第 5 节。

**证形（T-72 默认）**：基线 `$(git log -1 --format=%h)` = e87293f，
`git diff --stat HEAD -- apps packages specs/openapi` → 3 个文件：
`live-core.test.ts`(+35/-1) / 重生成 `gen/schema.ts`(1 行) / `core-v1.yaml`(1 行)。

**本轮我自己的两处失手（已修）**：
- 第一版 F-9 断言按"带 from_seq 必回 hello"写死，实测在"只给 query"形态下回的是 `hello_ack` → 错在我的假设，已按三条路径重写；
- 重写后首跑是 `Test timed out in 5000ms`（3 次连接 × 2.5s 窗口 > vitest 默认 5s），属预算不是行为；改窗口 900ms + 显式 20s 后 9/9。
  报的绿是改后那次。

## 当前 checkpoint（覆盖此前所有同名段）
- Stage：WS 面收口完成；面板侧无待办，等的是业务端点与两条文档裁定
- Status：in_progress
- Last verified action：2026-09-21T11:23:16+08:00 前后两轮 —— `live 9/9`（自属实例）与四道闸门 `11:20:48`（validate 全部通过 / codegen / vitest 19 passed+9 skipped / build+no-secret 全通过）
- Next action：等 backend 让 `POST /api/v1/projects` 与 `POST /api/v1/adapters/{key}/probe` 脱掉 501 ——
  一到位就用面板「驱动整条链」跑 §4 的 11 步，逐跳 ok/blocked 写进 `.qoder/evidence/frontend/`；
  期间不主动改契约相关任何文件（REQ-F-19、REQ-F-20 等主控裁定后我再动 yaml）
- Verification：`cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`

## T-80 轮（两条裁定投影 + 一条动作补进协议）· 本机 date 2026-09-21T11:58:23+08:00

**先接受那条批评并补上机制**："观察到阻塞条件消失却没推进账本"确实是缺动作，不是态度问题。
补进收尾协议：**任何时候当场观察到某个 blocked 的前提已经不成立，当轮就要 (a) 改 Next action 一行、(b) 附 grep/命令证据、(c) 在 progress 里把旧阻塞标"已解除 + 证据位置"**；
不等主控敲门，也不把"我测到了"当成"账本知道了"。F-17 那次就是我自己跑出来了却没改口。

### REQ-F-20 已投影（实现没错，是我读到的文档在要求一个实现从未承诺的语义）
契约③ §5 新口径落进 yaml 的 `x-websocket-frames`：`order`（游标唯一权威 = 客户端 hello 帧；`?from_seq=` 只在"根本没收到 hello"的 HelloTimeout 路径回落）、
`cursor-authority`（省略 from_seq = 明确声明"只要此后新事件"，省略 ≠ 0，也 ≠ 沿用 URL 值）、
`cursor-default`（三条路径的各自首帧）、`criteria`（§5 判据 ①②③ 随投影一起落地，提醒"两侧都要断"，落表归 `core/test`）。
我的面板两处都发所以行为一直对；改的是口径，不是我。validator 加了两条断言钉住这些字段存在且措辞对得上（旧那条只认"禁止/全量重放"的断言我按新落点改了，没去迁就旧词）。

### REQ-F-19 已投影并**在真响应上验了**（不只是我 yaml 里写写）
`CursorPage.next_cursor` 收紧为 `type: string` + required，注释写明"空串是唯一合法空表示，null 与字段省略都不合法（同一件事两种写法会让生成客户端判两次）"。
另加一条 live 断言 `T-80/REQ-F-19`：对 `GET /tasks?limit=5` 要求 `typeof next_cursor === 'string'` 且值就是 `''` ⇒ **线上真值与收紧后的投影一致**。
两点如实记录：① `openapi-typescript` 把 allOf 合成后的 `next_cursor` 仍生成为可选（`next_cursor?: string`），是 codegen 的宽松，不是契约松动；
② 我没顺手把 `SyncResult.next_cursor` 一起收紧（裁定只覆盖 §2 的分页出口），留 **REQ-F-21** 问一句"同步游标是否同样空串唯一"，validator 里有一条断言防止我哪天擅自收紧。

### 闸门与证据形状（基线 `c193894`）
- 四道闸门（本机 date 2026-09-21 11:57:13 +0800）：`validate` 全部通过 / `codegen` 稳定 / `vitest 19 passed | 10 skipped`（无 Core 时 live 整份 skip，skip 不计过）/ `build + check:no-secret` 全部通过
- live（自属 Core，11:56:46 起）：**`Tests 10 passed (10)`、0 skip**；实例收尾 `PID alive=False` + 数据目录已删
- `git diff --stat HEAD -- apps packages specs/openapi` → 4 个文件：`validate-openapi.mjs`、`live-core.test.ts`、`gen/schema.ts`、`core-v1.yaml`
- 本轮我自己的一个错：第一版分页断言把 `listProjects` 也放进去，而 `/projects` 在 09-21 仍 501 —— 会假红。已改成只用已实现端点，并把这句理由留在测试注释里。

## T-89 轮（REQ-F-21 收口）· 本机 date 2026-09-21T12:39:45+08:00

**同步游标 ≠ 分页游标**这条我照四条落进 yaml（`SyncResult` + `Session.sync_cursor`）：`next_cursor` 保持 required + 允许 null +
`minLength: 1`，schema 描述里逐条写①回显当前位置②"不知道"归 sync_state/state_confidence③null=库里 NULL=从未同步④只许"非空串|null"两种；
并显式写"与 §2 分页口径相反、照抄会造出'从头开始'的假信号 = §5 禁止的缺省全量重放"，防下一个读的人误抄。

**生成物第一次把两个域分开**：`gen/schema.ts` 里分页是 `next_cursor: string`（776 行）、同步是 `next_cursor: string | null`（967 行）——
"同一件事两种写法"在客户端侧消失了。

**validator 从 1 条扩到 7 组断言**（④-a 必填 / ③ 允许 null / ④-b minLength 判空串非法 / ② 依据写进 schema / 两个游标不得同形 / 同域收口 / 反向防我抄错），
并按 T-66 做过变异验证：删掉 `SyncResult` 的 `minLength: 1` → 当场 `FAIL ④-b 缺 minLength:1 → 空串仍合法 = §5 禁止的缺省全量重放入口`；
回滚用绝对路径 + `diff` 证明一致（`minLength: 1` 计数回到 2），再重跑取绿。断言落点用行号结构定位，不再手抄中文锚点
（第一次尝试就因为我把结尾锚点记成 `// F-6：*_json`（实为 `// F-6 / F-1 / F-8`）而空跑一次）。

**新留一条 REQ-F-22（问一句，不猜）**：`sessions.sync_cursor` 在**响应里是否必须出现**（`required`）？
裁定④说"省略"是第三种非法形状，但我现在无法实测 —— `GET /sessions` 与 `POST /sessions/{id}:sync` 在 12:38 仍 501，拿不到真响应。
所以我只收了 `minLength`、**没有**擅自加 required；等 sync 端点落地后当场看一眼再定。

**证形（基线当轮 `git log -1 --format=%h`）**：`git diff --stat HEAD -- apps packages specs/openapi` → 4 个文件
（`validate-openapi.mjs` +21/-3、`live-core.test.ts`、`gen/schema.ts`、`core-v1.yaml`）。
闸门（本机 date **2026-09-21 12:38:06 +0800**）：`validate` 全部通过 / `codegen` / `vitest 19 passed | 10 skipped` /
`build + check:no-secret` 全通过。live 那 10 条本轮未重跑（改动只在 yaml 形状与校验脚本，不触碰任何线上行为；
下一次接 `/sessions:sync` 时随链一起重跑）。

## 当前 checkpoint（覆盖此前所有同名段）
- Stage：REQ-F-19 / F-20 / F-21 三条游标口径全部落 yaml 且各有断言；面板无待办
- Status：in_progress
- Last verified action：12:38:06 四道闸门全绿 + ④-b 变异验证（先红后回滚一致）
- Next action：等 backend 让 `POST /projects`、`adapters/{key}/probe`、`POST /sessions/{id}:sync` 脱 501 ——
  ① 到位即用面板「驱动整条链」跑 §4 的 11 步真链，逐跳 ok/blocked 入 `.qoder/evidence/frontend/`；
  ② 同时用 sync 的真响应当场定 REQ-F-22（sync_cursor 是否 required），不凭猜改
- Verification：`cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`

## T-96 轮（REQ-F-22 yaml 半边收口）· 本机 date 2026-09-21T13:38:36+08:00

主控当场量出的不对称照单收下：`SyncResult` 三条齐全、`Session.sync_cursor` 缺 `required`（`core-v1.yaml:968`）。
裁定"两侧对称 —— required + 可空 + 字符串侧 minLength 1"，并明确**不必等那三个 501**：这是 schema 形状，不依赖端点能跑。
这句话本身是我的教训——我上一轮把 REQ-F-22 挂在"等真响应"上，把"形状该由谁定"和"值长什么样"混成一件事了。

### 落点
1. **yaml**：`Session.required` 补 `sync_cursor`（现在是 `[id, adapter_instance_id, managed, sync_state, state_confidence, sync_cursor]`），
   描述换成 YAML block scalar，把 T-96 口径写死在 schema 里："缺键（省略）与空串都是非法的第三种『没有』；null 只表示库里 NULL=从未同步。REQ-F-22 关闭。"
   （block scalar 而不是单引号串 —— 上一轮我在单引号标量里塞进 `;` 炸过一次，这次避开了引号形态。）
2. **codegen 前后差**（`git diff` 原样，`gen/schema.ts:928`）：
   - 前：`sync_cursor?: string | null;`
   - 后：`sync_cursor: string | null;`
   生成物里"可选"消失 ⇒ 客户端不可能再拿 `undefined` 当"没有游标"传给下游。
3. **validator 加 2 条结构断言**（`validate-openapi.mjs`）：`Session.sync_cursor` 必须在 `required` 里；其形状必须是"可空 + minLength 1"（与分页口径相反）。跑出来两条都 ok。

### 新的断言层：把判据交给 schema，不交给我的记忆
上一轮的"缺键/空串非法"只写在 validator 里 —— 那验的是**我声明了什么**，不是**这份声明会拒绝什么**。
新增 `apps/desktop/src/__tests__/cursor-shape.test.ts`（6 条）做**实例级**校验：`SwaggerParser.dereference` + `ajv.compile` 直接把载荷喂给 schema。

- 合法：`sync_cursor: "20"` 过、`null` 过
- **缺键必须判红**（T-96 要的那条）：省略 `sync_cursor` → `false`
- **空串在同步侧必须判红**：`Session.sync_cursor: ""` → `false`，`SyncResult.next_cursor: ""` → `false`
- `SyncResult` 同样不许缺键；`UNKNOWN` + `null` 合法（"不知道"归 sync_state，不是归游标）
- **分页侧反过来**：`TaskList` 的 `next_cursor: ""` → `true`、`null` → `false`、缺键 → `false` —— 两域语义相反，测试钉住不许同形

### 变异验证：先撞了"磁盘变异不咬"，再改成进程内（T-66④）
本轮**确实做过一次磁盘变异**：把 `Session.sync_cursor` 的 `minLength: 1` 从 yaml 里删掉，想验"空串必须判红"到底是谁在拦。
结果 5 条形状测试**仍然全绿** —— 变异没咬，说明那些断言验的是别的东西（validator 读的是我写的字符串，不是编译后的 schema）。
备份按 T-66 落在绝对路径 `.scratch/frontend/yA.bak`，回滚后用 `diff` 证明一致：
`diff specs/openapi/core-v1.yaml .scratch/frontend/yA.bak` → **0 差异**（三者字节同为 67635，`grep -c 'minLength: 1'` 回到 2）。
命令与输出留在 `.qoder/evidence/frontend/t96-mutation.txt`。**"变异已还原"这句话现在是有 diff 撑的，不是我记得。**

因此判别力改在**进程内**自证：`degraded()` 用 `structuredClone` 降级 schema 副本 —— 删掉 `minLength` 后 `""` **必须被接受**、
把 `sync_cursor` 从 `required` 摘掉后缺键载荷**必须被接受**；同一测试紧跟着断言真 schema 判它 `false`。
这样"必须判红"不可能在验别的东西，也不留任何磁盘痕迹。

### 依赖：`ajv ^8.20.0` 从"传递依赖"转为"显式声明"
它是 `openapi-typescript` / `swagger-parser` 早已带进来的 `8.20.0`（实测 `node_modules/ajv/package.json` = 8.20.0），
我只是把它写进 `apps/desktop/package.json` 的 devDependencies —— 因为我的测试**直接 import 它**，靠别人树里的副本活着不算数。
`package-lock.json` 差 1 行（只加声明），没有新增包、没有联网解析新树。

### 本轮我自己的两个错（记下来，别再来一次）
1. 收紧 `required` 之后 **vue-tsc 当场红 4 条**：3 条是 `cursor-shape.test.ts` 的类型（把 dereference 结果当 `Document` 用、拿 `object` 喂 `compile`）→
   改成显式 `type Json = Record<string, any>` + 一处 cast；1 条是 `unknown.test.ts` 的 `session()` 夹具少了新必填的 `sync_cursor` →
   补 `sync_cursor: '20'`。**必填收紧会打到所有手搓夹具的地方，这是"生成物变严"的正确信号**，不是测试的错。
2. 第一次跑闸门整份 `exit=1`：报 `'node' 不是内部或外部命令`。原因是我在 `npm.cmd` 命令里没把 node 目录放进**该命令的** `PATH`。
   修法是命令内 `export PATH="/d/DevTools/node-v24.19.0-win-x64:$PATH"`（进程内、一次性），**没有**动系统 PATH（红线）。

### 证形（基线 `git log -1 --format=%h` = `46325a4`）
- `git status --porcelain -- apps packages specs/openapi` → 7 个 `M` + **1 个 `??`（`cursor-shape.test.ts` 是新文件，`git diff --stat` 看不到它）**
- `git diff --stat HEAD -- apps packages specs/openapi` → 7 files, +111/-9：
  `core-v1.yaml` +4/-2、`gen/schema.ts` +16/-3、`validate-openapi.mjs` +43/-2、`live-core.test.ts` +43/-1、
  `unknown.test.ts` +3/-1、`package.json` +1、`package-lock.json` +1
  （注：这是**相对基线的累计差**，含 T-80/T-89 尚未提交的改动，不等于本轮增量）
- 四道闸门（最后一次重跑，本机 date **2026-09-21 13:37:09 +0800**，全在 `.qoder/evidence/frontend/gates-final.txt`）：
  ① `validate` 全部通过（含 T-96 两条 + T-89 七组 + 游标两域）exit=0；
  ② `codegen` 稳定输出 `87.9ms`，生成物即上面那条非可选形状；
  ③ `vitest run` → **`Tests 25 passed | 10 skipped (35)`**，`cursor-shape.test.ts 6 passed`，skip 的是 `live-core.test.ts` 整份（无 Core，skip 不计过）；
  ④ `build`（`vue-tsc --noEmit` + `vite build` + `check-no-secret`）exit=0，产物 3 个文件扫不到 3 项禁用串
     （`LUCERO_CORE_TOKEN` 未提供 → 该项按设计跳过并注明，另四项照扫）。
- **未重跑的部分，如实标**：live 那 10 条本轮没重跑。理由：改动只在 schema 形状、校验/测试文件与 devDeps 声明，
  不触碰 `client.ts` / `events.ts` / `panel.ts` 任何线上行为路径；下一次接 `:sync` 时随 11 步真链一起重跑。

## 当前 checkpoint（覆盖此前所有同名段）
- Stage：REQ-F-19 / F-20 / F-21 / **F-22** 四条游标口径全部落 yaml，且每条除 validator 外另有实例级形状断言；面板无待办- Status：in_progress（阻塞在别人的端点，不在我）
- Last verified action：13:37:09 四道闸门全绿（validate / codegen / 25 passed|10 skipped / build+no-secret）；
  本轮磁盘变异一次且**不咬**（已用 `diff`=0 证明还原），判别力改由进程内 `degraded()` 自证
- Next action：等 backend 让 `POST /projects`、`adapters/{key}/probe`、`POST /sessions/{id}:sync` 脱 501 ——
  到位即用面板跑 §4 的 11 步真链，逐跳 ok/blocked 落 `.qoder/evidence/frontend/`；
  同一次里重跑 live 全量断言并留 `live-t96.txt`（补掉上面那条"10/10 无原始输出"的缺口）。
  期间不动契约文件；若当场观察到任何阻塞前提已消失，当轮就改这行并附 grep 证据（T-80）。
- Verification：`cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`
  （node 不在 PATH：命令内 `export PATH="/d/DevTools/node-v24.19.0-win-x64:$PATH"`）

## T-127 / T-128 轮（REQ-F-14 收编 + F20-4 归我）· 本机 date 2026-09-22T15:38:23+08:00

### 先认账：主控的数是实测的，我那句"4 条"是采样面太窄
我当初只扫了 `core/api/http/handler.go` 一个文件，就下了"transport 自有码 4 条"的结论 —— 这是"范围式否定"同族，
只是这次方向是**少数**（少数会导致 enum 少收，线上真返回的码在闭集外 ⇒ 客户端 `not_core_error_body` 之类误判）。
本轮把 validator 的采样面改成 **`core/api/**` 递归、排除 `_test.go`**：当场 5 个文件、12 个码，与 §15.2 里 `—` 那 12 行**逐字相等**。

### T-127①：`ErrorCode` enum 19 → **29**
补 10 条：7 条 transport（`validation_failed` `not_found` `internal_error` `illegal_transition`
`idempotency_key_required` `idempotency_replay` `idempotency_mismatch`）+ 3 条 §15.2 本来就有、我 `RPC_CODES` 快照漏抄的
（`read_only` `adapter_unresponsive` `execution_state_unknown`）。
**描述里那段 HTTP 状态码映射我整段删了** —— 那也是一份快照，正是这次的病根；改成"权威在 §15.2/§15.2.1 两张表，validator 每次现场解析并对拍"，
并把"三对同 HTTP 码、不同名字不许合并"（`unauthorized`/`auth_required`、`not_found`/`session_not_found`、`internal_error`/`internal`）
连**为什么**（区别在生产者）一起写进 schema。codegen 后生成物 `ErrorCode` 联合 = 29 条，10 条新增逐个核到（`schema.ts:779`）。

### T-127②：validator 不再内嵌任何一份码表
`RPC_CODES`(15) 与 `TRANSPORT_CODES`(4) 两段常量删净，改成 `parseHttpCodes()`：
锚点用 **`^### 15\.2 `（行首 + 尾随空格）** —— 这一条是主控给的坑里最阴的：`#### 15.2.1` 这个标题含子串 `### 15.2`，
不锚行首就会命中它；正文里那句 `` `/^### 15.2/` `` 示例同样会冒充标题。区间 `[15.2, 15.3)` 一次把 §15.2 的 18 行与 §15.2.1 的 11 行都读进来 = 29。
**守门按裁定的三个数**：`rows===29 / named===17 / dashed===12`，任一不对 → `broken()` → **exit 2** 并打印"解析器坏了 / 宁可停也不拿半张表判人"。
豁免判据按**形状**不按名字（『来自』列 `—` 开头 = HTTP 层专有），所以将来再收编 N 条我不需要改代码。
CR 一律先 `replace(/\r\n?/g,'\n')`，路径全部 `path.join(root,…)` 绝对化（root 由 `import.meta.url` 推，不受 cwd 影响）。
**顺手清掉同一病根的第三份快照**：`CHAIN_EVENTS`（我手抄的 22 类链事件）改成 `parseChainEvents()` 读事件契约 §7 的围栏块，
守门是"解出 <10 个 ⇒ 坏了"，不设写死的 22（契约自己的 C8 注就说了"行数会漂，别写死进断言"）。
**§6 事件目录我故意没解析**，理由写进注释：那张表有 `execution_cancelled / _failed / _succeeded` 这种合并族行，
展开要猜前缀，猜错就是一次**静默的半张表** —— 比快照更坏。`EventType` 仍是我的投影，这一条留给"合并行展开约定"或由主控定落点。

**守门自己也要有自破坏探针（T-66④）**，且**不改 specs/ 那个文件**（它在别人的写范围里，中途被第三方读到会串味）：
`probing` 模式下 `broken()` 改 throw，在内存里把契约改坏三种 —— 少一行 / 把 `—` 换成码名 / §15.2 标题改名 ——
三条全部 `ok 守门自证：… → 当场拒判`。enum 侧另做了一次**磁盘变异**（我自己的 yaml，备份 `.scratch/frontend/yaml-t127.bak`）：
拿掉 `read_only` ⇒ `exit=1`，且**两个方向同时报红**（`§15.2 有、enum 里没有` + `enum 里有契约之外的码`）。
回滚证明：`diff specs/openapi/core-v1.yaml .scratch/frontend/yaml-t127.bak` → **0 差异**，`grep -rn 'MUT-T12' specs/openapi packages apps/desktop/src` → **0 命中**。

### T-127③：`契约①` 歧义引用 6 处 → 「事件契约」
yaml 的 322 / 809 / 826 / 853 / 1280 / 1299 六处（建任务 summary、枚举对齐注释、状态机说明、事件目录投影、信封、WS 帧）全部改掉，
**另加我自己源码里同族的 4 处**（`ChainRail.vue`、`stores/panel.ts`、`chain.test.ts` 的 describe 名、`client.ts` 注释）—— 同一句话在 UI 里一样会被读错。
改法是逐条 assert 锚点命中数==1 再 `split/join`（不是 `String.replace`，避开 `$'` 那个坑）。
核完：`grep -rn 契约① specs/openapi apps/desktop/src packages/api-client/src` → **只剩生成物 0 处**（codegen 后 `契约①=0 / 事件契约=4`）。

### T-128：F20-4 归我，用例名照表写、不改名
判据表那一格写的命令是 `cd apps/desktop && npx vitest run -t helloAckAdvancesCursor`，
所以我把用例名**原样**取成 `helloAckAdvancesCursor`（**没改名 ⇒ 那一格不用回写，表里的命令不是假的**）。
实测输出：`Tests 1 passed | 35 skipped (36)`、`✓ … helloAckAdvancesCursor…（契约③ §5 F20-4）`、exit=0，时刻 15:37:45。
断两侧：首帧 `hello_ack{server_seq:1200}` ⇒ 游标 1200；首帧 `hello` 回显 ⇒ 游标**不动**（回显值取 9 / 42 / 900 三个方向都判，
"高于游标"那侧最危险：照抄就等于承认 43..900 看过，那段永不再补发）；再加一条可观察落点 —— 重连时第二根 socket 发出的
`hello` 帧必须声明 `from_seq:777`，这才是"写进自己的游标"外部唯一可证的形态。

**这条用例是第一版没咬人、被我自己变异抓出来的**（重要，别当已完成读过）：
第一版我断的是 `link.seq`（面板显示值），跑 M2 变异"把回显当游标用"→ **exit=0 全绿**。原因：`link.seq` 只由 `onSeq` 回调写，
`hello` 分支根本不回调它 ⇒ 断的是**错误的观察面**，而真正决定重连位置的是 `openEventStream` 内部的 `lastSeq`。
修法：断 `s.seq`（客户端自己的游标）+ 重连时声明的 `from_seq`。改完两条变异都红：
M1（`hello_ack` 不写游标）→ `expected { cursor: null … } to deeply equal { cursor: 1200 … }`、exit=1；
M2（`hello` 回显覆盖）→ `回显 from_seq=9 不得改写客户端游标: expected 9 to be 42`、exit=1。
备份 `.scratch/frontend/events-t128.bak`，回滚 `diff` **0 差异**、`MUT-T128` 全仓 **0 命中**。
"读字面第一帧"这条判据按 T-128 的要求落成 helper 契约：`afterFirstFrame()` 一次只喂一帧、喂的就是被断言那一帧，没有任何按类型筛帧的循环；
真实 WS 侧的 `firstFrame()`（live-core）本来就是"第一个 onmessage 就 resolve"，也不筛。

### 证形（基线 `git log -1 --format=%h` = `4f7fb43`，已被别人推进过，所以这份差≈本轮增量）
证据文件两份：`.qoder/evidence/frontend/gates-t127.txt`（五段：validate / codegen / vitest 全量 / F20-4 表内原命令 / build）
与 `.qoder/evidence/frontend/t127-t128-mutation.txt`（三次磁盘变异的原始失败行 + 回滚 diff + 守门自证三条）。
`git diff --stat HEAD -- apps packages specs/openapi` → 7 files，+233/−48：
`validate-openapi.mjs` +182 段里主体是解析器、`chain.test.ts` +48、`core-v1.yaml` 27、`gen/schema.ts` 18、
`ChainRail.vue` / `panel.ts` / `client.ts` 各 2（就是改名那一处）。`git status --porcelain` → **无 `??`**（本轮没建新文件）。
闸门（本机 date **2026-09-22 15:37:42 +0800** 那一轮，全量在 `.qoder/evidence/frontend/gates-t127.txt`）：
① `validate` 全部通过（含三条守门自证 + 三方对拍）exit=0；② `codegen` 78.6ms，enum 生成物 29 条；
③ `vitest run` → **`Tests 26 passed | 10 skipped (36)`**（skip 的仍是 `live-core.test.ts` 整份，skip 不计过）；
④ F20-4 表内原命令单独跑 1 passed；⑤ `build`（vue-tsc + vite + `check:no-secret`）exit=0。
**本轮没重跑的**：live 那 10 条。理由与上轮同：改动在 schema enum、注释措辞、校验脚本与面板测试，
`client.ts` 只改了一行注释、`events.ts` 最终与回滚后一致（`diff`=0 可证），线上行为路径未变。
"这份绿是最后一次重跑"的证法（不靠记忆）：五步在同一条命令链里跑完（15:37:39 起，各步 exit 全 0），
之后 `find specs/openapi packages/api-client/src apps/desktop/src apps/desktop/scripts -newermt "2026-09-22 15:37:42"` → **空输出**
⇒ 从 vitest 起跑那一刻起源码没再动过；codegen 那步排在 find 判定**之前**（生成物 mtime 必然晚于链条起点，反过来排就是自己骗自己）。

## 当前 checkpoint（覆盖此前所有同名段）
- Stage：REQ-F-14 关闭（enum 29 条三方对拍 + 守门自证）；「事件契约」引用改净；**F20-4 已建并双向变异验过**；面板无待办
- Status：in_progress（阻塞在别人的端点，不在我）
- Last verified action：15:37:42 四道闸门全绿（26 passed | 10 skipped）+ F20-4 表内命令 1 passed；
  磁盘变异 3 次（M1/M2/M3）**全部 FIRED**、全部 `diff`=0 还原、`MUT-T12*` 全仓 0 命中
- 留给主控的一句话（不是请求，是登记）：`ErrorCode` 那 3 对"同 HTTP 码不同名"我已在 schema 里写明理由；
  `EventType`（§6 目录）仍是唯一一份手抄投影，因为合并族行要推断才敢解析 —— **要么给一句展开约定，要么认这条留白**
- Next action：等 backend 让 `POST /projects`、`adapters/{key}/probe`、`POST /sessions/{id}:sync` 脱 501 ——
  到位即用面板跑 §4 的 11 步真链，逐跳 ok/blocked 落 `.qoder/evidence/frontend/`，同一次重跑 live 全量并补 `live-t96.txt`
  （补掉 09-21 那条"10/10 只有台账、无原始输出"的缺口）。期间不动契约文件；阻塞前提一旦消失当轮改这行并附 grep 证据（T-80）。
- Verification：`cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`
  （node 不在 PATH：命令内 `export PATH="/d/DevTools/node-v24.19.0-win-x64:$PATH"`）

## T-139 轮（守门第一次真响：C28① 把 §15.2 扩到 30 行）· 本机 date 2026-09-22T23:09:14+08:00

### 先记下这一响值在哪
我 09-22 那道守门（29 = 17 + 12 写死）在 23:01 真把主控拦下了一次 —— 但它拦的**不是"半张表"，是"合法扩列"**：
主控 22:4x 把 C28① 的 `duplicate_project_path` 收编进去，行数变 30/13，我的字面基线当场过期。
**同一份代码里对的那一半**是"锚点 + 不自洽即停"（`^### 15\.2 ` 行首锚点确实排除了 `#### 15.2.1`）；
**错的那一半**是把**内容规模**当判据 —— 主控给 `test` 的 T-135 那句话同样是对我说的，我照收：
字面基线每合法扩列红一次，就训练人去改数字而不是读结果。**这次红如果留着，下一次大家就跳过它了。**

### ① 核新表：三方仍然自洽，而且实现侧先动了一步
- §15.2 区间现在第 492..568 行 = **30 行（17 带 §15.1 码名 + 13 带 —）**；§15.1 码列 17 个；**双向集合相等**（两个方向差集都空，C8-1/C8-4 的形状）
- `duplicate_project_path` = 409、『来自』是 `—`（HTTP 层专有），契约③ §3 明写要 409 —— 与 yaml 侧无冲突，我没有异议
- **C28② 的"501/405 不许互换"我核了 yaml**：`grep -n "405" specs/openapi/core-v1.yaml` 只有我新写的那三行说明，
  **没有任何端点把"未接线"声明成 405**（未接线的都走 `not_implemented`）。这条口径我原文投影进 `ErrorCode.description`，
  并写了为什么对客户重要：**501 = 等实现，405 = 换方法**，把前者回成 405 是叫客户去试一条同样没接线的路径
- **顺带一条主控没提的事实**：Core 实测侧已经**跟着动了** —— 扫 `core/api/**` 递归得到 **13 个码 / 6 个文件**（第 6 个文件是
  `projects.go`，backend 22:4x 落地的），与 §15.2 那 13 行 `—` 逐字相等。也就是 C8-11 想防的那件事，
  这台扫描器今天已经能自己看见，不用等人肉 grep

### ② 改法：把"数字"降级成打印，把"不自洽"升级成判据
- **删掉** `C2_BASELINE = {rows:29,named:17,dashed:12}`，改成形状判据（全部当场算，没有一个字面规模）：
  零行 / 重码 / `named + dashed ≠ rows` / 『来自』两种形状都不像 / 带 `—` 的为零（静默空集）/
  **near-miss**（`| \``… 开头却不被主正则接受 ⇒ 表格一旦长出合并族行就停，不再静默丢行）/
  **§15.1 ↔ §15.2 码名双向集合不等**（要么契约自己缺一条，要么我把区间切错 —— 两种都不配继续判 yaml）
- 现在的行数只出现在一行 `ok(...)` 里，给人看
- enum 补 `duplicate_project_path` ⇒ **30 条**，与 §15.2 左列双向相等；codegen 重跑后生成物联合 30 条、该码在列
- 探针从 3 条扩成**两组 6 条**，两组都是内存改文本、绝不落盘动 `specs/`（别人的写范围）：
  **A 组"不自洽必须停"**：标题改名 / 『来自』写成散文 / 出现合并族行 / §15.1 那半张表整个没了 ⇒ 四条全 `ok 当场拒判`
  **B 组"合法变化必须放行"**：新收编一行（就是 22:4x 那个形状）/ 删掉一行 ⇒ 两条全 `ok 放行（判据交给集合对拍）`
  B 组是这次的**新加的那半**：只跑 A 组会养成一个一惊一乍的守门，而 22:5x 那次假红恰恰是 B 组缺失造成的

### 当场证据（都进 `.qoder/evidence/frontend/gates-t139.txt`）
- 交付前最后一次全量重跑 **23:08:53 +0800**，五步同一条链：`validate` 全部通过 exit=0 → `codegen` exit=0 →
  `vitest` **26 passed | 10 skipped (36)** → F20-4 表内原命令 **1 passed | 35 skipped** → `build`（vue-tsc + vite + `check:no-secret`）exit=0
- "这份绿是最后一次重跑"的证法（不靠记忆，也别拿含混输出当结论）：
  `ls --time-style` 实读 —— validator `23:06:28`、yaml `23:08:38`（M4 回滚那次拷贝）都**早于**链条起点 `23:08:53`；
  `packages/api-client/src/gen/schema.ts` 是 `23:08:54`，**它是链条里 codegen 自己写的**，不是后来有人改 ——
  证法：再跑一次 `npm run codegen` 后 `diff` 前后两份生成物 → **逐字节相同**（幂等），且
  `find specs/openapi apps/desktop/src apps/desktop/scripts -newermt "2026-09-22 23:08:54"` → **0 个**（生成物排除在外）。
  上一版这里我写的是"find 全源码目录 → 空输出"，那句**不准**：`gen/` 必然出现在结果里（codegen 排在判定之前也挡不住同秒写入），
  要写成"非生成物 0 个 + 生成物重跑幂等"两句才立得住。
- **M4 磁盘变异（在新代码上重做，不拿旧结论充当）**：enum 拿掉 `duplicate_project_path` ⇒ `exit=1`、
  `FAIL §15.2 有、enum 里没有（1 条）：duplicate_project_path` + `FAIL enum 里有契约之外的码`（两个方向一起红）；
  备份 `.scratch/frontend/yaml-t139.bak`，回滚 `diff` → **0 差异**，`grep -rn 'MUT-T139'` 四目录 → **0 命中**
  （原始输出 `.scratch/frontend/m4.txt`）
- 证形：基线 `git log -1 --format=%h` = `2297a8d`（又被推进，T-127/T-128 那批已入库），`git diff --stat HEAD` →
  3 files +87/−34：`validate-openapi.mjs` 111、`gen/schema.ts` 5、`core-v1.yaml` 5；`git status --porcelain` 无 `??`
- 一处我自己要防的坑（主控同轮踩到、我这类命令里也常见）：**管道会掩码 exit code** ——
  `npm run validate | tail` 的 `$?` 是 `tail` 的。本轮所有退出码都在**没有管道**的情况下取（`{ … } > 文件` 整链），
  需要展示时才 `grep` 那个文件；另有一次用 `${PIPESTATUS[0]}` 显式取管道首段。

### 没做的 / 留着的
- live 那 10 条本轮仍未重跑：改动只在 yaml enum + 校验脚本，`client.ts`/`events.ts`/`panel.ts` 零改动（`git diff` 里就没有这三个文件）。
- 事件契约 §6 目录那份手抄投影**依然没解析**，理由同上（合并族行要猜前缀）；
  **但本轮补了一条真正的兜底**：§15.2 侧的 near-miss 判据就是同一族形状的第一道警报，
  若 §6 哪天也要机械解析，缺的是"合并族行怎么展开"这一句约定，请主控给落点（重复登记，不催）。

## 当前 checkpoint（覆盖此前所有同名段）
- Stage：T-127/T-128 入库后，T-139 把守门从"字面基线"改成"判形状 + 打印"，enum 30 条三方对拍通过
- Status：in_progress（阻塞在别人的端点，不在我）
- Last verified action：23:08:53 五步链全 exit=0 + M4 变异先红后回滚 `diff`=0；守门探针 A 组 4 条停、B 组 2 条放行
- 留给主控的两句话（登记，不是催）：① §6 事件目录要机械解析还缺"合并族行怎么展开"一句约定；
  ② 判据表 **C8-8 那一行本身仍写着字面 29/12**（契约② 里，归 `test` 按 T-135/C8-10 改形状）—— 我这边已经改完，别拿我这台的绿去推那台
- Next action：等 backend 让 `POST /projects`、`adapters/{key}/probe`、`POST /sessions/{id}:sync` 脱 501 ——
  到位即用面板跑 §4 的 11 步真链，逐跳 ok/blocked 落 `.qoder/evidence/frontend/`，同一次重跑 live 全量并补 `live-t96.txt`
  （补掉 09-21 那条"10/10 只有台账、无原始输出"的缺口）。期间不动契约文件；阻塞前提一旦消失当轮改这行并附 grep 证据（T-80）。
- Verification：`cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`
  （node 不在 PATH：命令内 `export PATH="/d/DevTools/node-v24.19.0-win-x64:$PATH"`；退出码一律在无管道处取）

## T-140 轮（§4 第一格接真 Core + §6 目录解析器补上）· 本机 date 2026-09-22T23:37:54+08:00

### 先按 T-80 反查了那句过期阻塞，结论：巡检说的对
现场读码（不是我转述）：`core/api/http/handler.go:120` 有 `registerProjectRoutes(mux, c)`；
`projects.go:79` 那个 `mux.HandleFunc("/api/v1/projects", …)` 里 `if r.Method != http.MethodPost` 才回 501，
文案逐字是"GET /api/v1/projects 这一腿还没接；POST 已可用"。
⇒ **POST 通了、GET 仍 501**，我那句"等三个端点脱 501"确实过期一半，已改（见文末 checkpoint）。
`:sync` 与 `probe` 两格仍 skipped，没拿它们凑绿。

### ① 面板第一格真跑通了（live 16 passed / **0 skipped**）
`live-core.test.ts` 新增 6 条（`describe.skipIf(!live)`，与旧面共用同一把锁）：
- 建项目 **201** + `Project` 必填字段齐、`created_at` 是 Unix 毫秒（断 `> 1.7e12`，防"秒/毫秒"这类静默错）
  → 原文 `项目已建：id=01a0c9c3-… status=ACTIVE version=1`
- **同一 Idempotency-Key 重放** → 同 id、同 created_at、`version` 仍是 1（副作用不重复）
- 不带 Idempotency-Key → **403 `idempotency_key_required`**（T-139 刚收编的那条码线上真出现）
- 同一 canonical_path 换一种拼写（`d:\…\\dup\` 对 `D:/…/dup`）→ **409 `duplicate_project_path`**、`retryable:false`，
  原文 `冲突如实回 409：首个项目 …，未被第二条请求改写` ⇒ **词干式 canonicalize 折叠是真的在做**
- **C28② 的线上实证**：同一 path 未接线的 GET 回 **501 不是 405**（断言消息里写了为什么要紧）
- **垂直切片**：先换票建连拿到游标 → 再 `POST /projects` → WS 真收到 `project_created`
  → `data.seen.add(ev.event_type)` → `stepDone(CHAIN[0]) === true`，原文 `切片成立：seq=6 project_created → 第一格 ok，面板无一处自造状态`。
  这条是唯一能证"面板第一格不是我自己点亮"的形态：**先建连再写**（反过来就只测了个空窗口）
- 起法与收尾：`.scratch/frontend/run-t140.sh` → 自属二进制 `core-t140.exe`（23:25 由 `core/cmd/core` 现建，
  旧的 `core-t72.exe` 是 09-20 的，隔了 31 小时不能用）+ 独立数据目录 `coredata-t140`，
  `PID=804 → alive=False`、目录我自己删掉；`kill`(SIGTERM) 后 `core.lock` 仍在 —— 那就是 **OBS-F-1** 那条，归 backend，我不在这儿复述结论。
- **顺带闭掉 09-21 那条证据缺口**：原文已进 `.qoder/evidence/frontend/live-t140.txt`（+ `live-t140-head.txt` 记计数），
  当时能复核的最强数字从"9/9"变成"**16 passed (16)、0 skip**"。

### ② §6 目录解析器补上，当场量出我那份手抄少 3 条
巡检②说约定已落文（`event-contract-v0.1.md` mtime **23:18**、①~⑤ 五条）。我按它写了 `parseEventCatalog()`，
第一次跑就报：**`policy_created`、`policy_updated`、`notification_read`** 三条目录里有、我 enum 里没有
（前两条 **09-19 就在目录里** —— 我 09-21 那份原型解析只解出 35 行，正是因为合并族行没被接受，而我当时把这件事读成"§6 不能机械解析"）。
⇒ **认账**："解析会猜错"从来不是留快照的理由；约定⑤（孤儿 `_xxx` 必须报错、不许静默跳过）存在的目的就是把"猜错"变成"停下来"。
补齐后 `EventType enum ↔ §6 目录` **双向相等，62 类**；`§7 链 22 类 ⊆ 目录 62`；codegen 后生成物 `EventType` 62 条、三条新增逐个核到。
探针加了两组：**C 组**（孤儿 `_orphan_read` / §6 标题改名 / 每行都不再像表格 ⇒ 三条全"当场拒判"）+
**D 组**（假想 `probe_alpha_beta / _failed` ⇒ 必须展开成 `probe_alpha_failed`，**不是**按第一个 `_` 读出的 `probe_failed`）—— D 组是拿约定③那句"最后一个 `_`"当判据，不靠我记。

### ③ 本轮我自己抓出来的一条**假绿**（重要，比前面都重要）
实现侧扫描我顺手加严了一件事：不只比"码出现过"，还比 `writeError` 用的 `http.StatusXxx` 与 §15.2『状态』是否一致。
第一版我把 node `STATUS_CODES` 的反查表键写成 `'Status'+名字`，而正则取的是**去掉前缀的后缀**（`NotFound`）
⇒ **15 个码全部"反查不到、跳过"**，而屏幕上那行 ok 写的是 **"逐条核过 … 一致"** —— **零条比对，报的却是绿**。
两处一起修：① 键改成不带前缀；② 加了一条守卫"比对数=0 ⇒ 直接判红，不许说一致"。
并用 **M5** 证明这条守卫会红：把键改回那个错法 → `FAIL 状态一条都没比对上（15 个常量名全部反查不到数字）⇒ 扫描/反查器坏了，不许报"一致"`、`exit=1`；
回滚 `diff` **0 差异**、`grep -rn 'MUT-T140'` **0 命中**（`.scratch/frontend/m5.txt`、`validator-t140.bak`、`m5-insert.mjs`）。
修完之后：`HTTP 状态逐条核过：15/15 条比对一致`（这才叫核过）。

### ④ 新发现的口径问题 → 登记 **REQ-F-23**（不改文档，等主控裁）
扫描面这轮从 12 码/6 文件涨到 **15 码/7 文件**，其中两个是 Core 直接发的：
`session_not_found@sessions.go:75`（`session.ErrSessionNotFound`，域错误，不经 Adapter）、
`adapter_unresponsive@sessions.go:78`（**C25/T-120② 那支**：完全无可用 Provider ⇒ 503 用现成码，明令不许新造）。
代码没错，**问题在 §15.2『来自』那一列的语义**：它今天等于"这个 HTTP 响应是把 Adapter 的失败翻译出来的"，
而这两行现在是**两个生产者**（Adapter 会发、Core 域层也会发）。三对"同 HTTP 码不同名"讲的是不同名字，
这一条讲的是**同一个名字两个来源**，是新的形状。请裁：把那两个『来自』写成 `SESSION_NOT_FOUND` / —（双源）还是加一列"生产者"。
我在 validator 里让它**只打印不判红**（`待裁（不算红）：…`），等裁定再收。

### 证形（基线 `git log -1 --format=%h` = `a1b8664`，链条跑完后又有人提交到 `e4b214b`）
- `git status --porcelain -- apps packages specs/openapi` → ` M` 三个：`validate-openapi.mjs`、`gen/schema.ts`、`core-v1.yaml`（142+/20−）
- **要说清的一条**：我这轮改的 `live-core.test.ts` **不在这份 diff 里** —— 它被 `5ea5172`（23:28 那次 snapshot 提交）扫进去了
  （`git show HEAD:…live-core.test.ts | grep -c "§4 第一格"` → 2）。我**没做任何 git 写操作**，这是别人提交我文件的结果；
  教训照旧：**`git diff` 只能证"哪些文件我没动"，不能当交付清单**，本轮真实清单是这 4 个文件。
- 五步链（交付前最后一次全量重跑，**23:37:30 +0800**，`.qoder/evidence/frontend/gates-t140.txt`）：
  `validate` 全部通过 exit=0 → `codegen` exit=0（EventType 62 / ErrorCode 30）→
  `vitest` **26 passed | 16 skipped (42)**（无 Core 时 live 整份 skipped）→ F20-4 表内原命令 **1 passed | 41 skipped** →
  `build`（vue-tsc + vite + `check:no-secret`）exit=0。
  `find`（排除生成物）在链条起点之后 → **0 个**；生成物由 codegen 自写，已用"重跑 codegen 输出逐字节相同"证幂等。
- live 那次跑在 **23:37:11**（早于五步链 19 秒），代码状态与链条一致：链条之后 `find` 为 0 个，所以两份绿指向同一份源码。

## 当前 checkpoint（覆盖此前所有同名段）
- Stage：**§4 第一格已在真 Core 上点亮**（REST 写 → journal → WS → 面板投影，16/16 live 0 skip）；
  §6 目录解析器补上、手抄少收的 3 条已补齐（enum 62 与目录双向相等）；自抓一条"零比对报 ok"的假绿并加了守卫 + M5 证明
- Status：in_progress（还剩 `:sync` 与 `probe` 两格，另有 REQ-F-23 等裁）
- Last verified action：23:37:30 五步链全 exit=0；live 23:37:11 `16 passed (16)`；M5 变异先红后 `diff`=0 还原
- 待主控/待别人：① **REQ-F-23**（同码双生产者的『来自』列语义，见上面 ④）；② backend 把
  `POST /sessions/{id}:sync` 与 `adapters/{key}/probe` 脱 501（`GET /projects` 那一腿按 `projects.go:81` 是它自己的 Next action）
- Next action：等 `:sync` 与 `probe` 脱 501 → 接 §4 第二~四格（discover → manage → sync），
  用 `run-t140.sh` 那套起法把链逐跳 ok/blocked 落 `.qoder/evidence/frontend/`；**不空等 `GET /projects`**（它不在 §4 链上，面板自己分页就够）。
  期间不动契约文件；阻塞前提一旦消失当轮改这行并附读码证据（T-80/T-140 两次都是这一条）。
- Verification：`cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`
  （node 不在 PATH：命令内 `export PATH="/d/DevTools/node-v24.19.0-win-x64:$PATH"`；退出码一律在无管道处取）

## T-144 子轮（§6 展开约定的正式版到了：核对我的实现，改一处 + 报一处分歧）· 本机 date 2026-09-22T23:42:43+08:00

巡检 23:22 那句"§6 约定已落文"我在 23:37 那轮就照着实现了，T-144 是同一件事的正式版（带提交号 `3a71ea7` 与①~⑤全文），
**没有重做**，只做了两件事：

1. **对③的字面口径改了一处**：契约正文写"词干 = 本行**第一个**全名里最后一个 `_` 及其之前"，
   我 23:37 那版是"每遇到一个全名就更新词干"（= **最近**一个）。两种读法在今天的 §6 上结果相同（62 类、双向相等，两次都过），
   但在形如 `probe_alpha_beta` / `probe_x_y` / `_failed` 的行上会各自展开成 `probe_alpha_failed` 与 `probe_x_failed`。
   ⇒ 改成**按契约字面取第一个全名**，并加一条"遇到分歧就停下来"的守门：
   同一行有两个以上词干不同的全名、且行内还有简写 ⇒ `broken()`，**我不替契约与 db 挑一个读法**。
   探针：**E**（歧义行必须停）与 **F**（整行写全、无简写的多全名行照常放行，别误杀约定④）两条都 ok。
2. **纠正 T-144 正文里两处过期前提**（都当场取证，不是推断；引用一律用符号，行号只是当时刻度、23:41 实读）：
   - "③…与 `db` 的 `eventTypesIn` 现实现一致" —— **不一致**：`eventTypesIn` 的 `default:`（全名）分支里
     `if i := strings.LastIndexByte(name, '_'); i >= 0 { stem = name[:i+1] }` 是**每个全名都重写一次 `stem`** ⇒ 取的是"最近一个"；
     契约③正文写的是"第一个"。今天零分歧（§6 里没有"两全名词干不同 + 有简写"的行），
     但这句话作为"对齐依据"是错的，**要么改契约措辞、要么改 db 的实现**，两边各改各的就会在将来某天分歧（正是契约自己钉③的理由）。
   - "它那边第⑤条今天还是静默跳过（T-142 待改）" —— 现盘上**已经会报错**：同一个 `eventTypesIn` 的
     `case strings.HasPrefix(name, "_"):` 分支里 `if stem == "" { return nil, fmt.Errorf("row %q starts with the shorthand %q before any full name; …") }`，
     文件 mtime **23:26**（晚于 T-142 派发）。⇒ T-142 那半件按现状可判"已落"，不必再派。

### 证形（闸门 23:42:43 五步全 exit=0，`.qoder/evidence/frontend/gates-t144.txt`）
`validate` 全部通过（C×3 + D + E + F 六条探针全 ok；`EventType ↔ §6 双向相等 62 类`；`HTTP 状态 15/15 比对一致`）→
`codegen` exit=0 → `vitest` **26 passed | 16 skipped (42)** → F20-4 表内原命令 **1 passed** → `build` exit=0。
`find`（排除生成物）在 23:42:45 之后 → **0 个**。
**live 未重跑，且这不是"我以为不用"**：改动只落在 `apps/desktop/scripts/validate-openapi.mjs`（23:42:21），
`find packages/api-client/src/{client,events}.ts …/stores …/components …/__tests__ specs/openapi -newermt "2026-09-22 23:37:11"` →
只剩 `gen/schema.ts`（codegen 自写、yaml 未变）⇒ 23:37:11 那次 **16 passed / 0 skip** 描述的就是当前运行时代码。
本轮台账/记忆文件在链条之后修改，不进构建输入。

## T-150 子轮（两条裁定落地：③b 取消歧义、§15.2.2 关掉 REQ-F-23）· 本机 date 2026-09-23T00:03:26+08:00

主控 23:5x 把我报上去的两件事都裁了，两条都**改变了我代码里的判据**，不是"知道了"就完：

1. **③ 词干取法改回"最近一个全名"**（按 db 的实际语义定边），并把 ③b 落成守门：
   合并格只许"全部写全名"或"一个全名打头 + 若干简写"；`多全名 + 简写` = 表格形状坏了 ⇒ `broken()`。
   这条的价值在**取消歧义**而不是裁决：混排非法之后，"第一个 vs 最近一个"再也没有能区分它们的输入。
   探针跟着改：**E**（`probe_alpha_beta / probe_x_y / _failed` 必须停，理由归 ③b）、
   **G**（`全名 / 简写 / 全名` 这种"简写之后又来全名"是同一条 ③b，不能只看行首）、
   **F**（`probe_alpha_beta / probe_x_y` 两个全名、**词干不同**、无简写 ⇒ 必须放行，别误杀约定④）。
   两条都 ok，且 **D**（`_failed` 在 `probe_alpha_beta` 之后 ⇒ 展开成 `probe_alpha_failed`，不是 `probe_failed`）继续钉住"最后一个 `_`"。
2. **归因这条我自己差点写错**：③b 第一版我写成 `fulls.length !== 1`，于是 `fulls.length === 0` 的**孤儿简写**
   被抢去报成"③b 混排（0 个全名 + 1 个简写）"——那句读起来就像废话，而且**下次真改规则时没人知道该看哪条**。
   改成 ③b 只管 `fulls.length > 1`、孤儿留给 ⑤，并加一条**归因自证**：孤儿必须报在 ⑤ 名下且消息里不许出现 ③b。
   （两条规则抢归因 = 两边都不报错的那个洞的前兆，跟"零比对报 ok"同族。）
3. **REQ-F-23 关闭**（§15.2.2）：『来自』只答"这个名字属于哪套词汇"，不答"哪个包发的" ⇒ Core 直接用 Adapter 词汇名合法，
   我那条"待裁（不算红）"的打印改成**报现场事实**：`6 个码由 2 个以上文件发`（`method_not_allowed×5`、`not_implemented×4`、
   `validation_failed×3`、`idempotency_key_required×2`、`idempotency_replay×2`、`idempotency_mismatch×2`）。
   **巡检那句我照收并且已经不改口**：`method_not_allowed` 现盘是 **5 个文件**（backend 正在往 `sessions.go` 加第 5 个），
   主控量到的 4 在他量时是对的。我这条是**当场数**的，所以数字自己会跟着盘走 —— 这也是为什么把它做成打印而不是写进正文。
   yaml 侧同步：`ErrorCode.description` 加了一句"§15.2.2：那列不答生产者，客户端判据只看 `error.code` 这个名字，别去猜哪个包发的"。

### 证形（`00:03:26` 五步链，`.qoder/evidence/frontend/gates-t150.txt`）
`validate` 全部通过 exit=0（探针 A×4 / B×2 / C×3 / D / E / G / F / 归因 共 **14 条自证**全 ok；
`EventType ↔ §6 双向相等 62 类`；`Core 实测 15 码 ∈ §15.2 左列`；`HTTP 状态 15/15 比对一致`）→ `codegen` exit=0 →
`vitest` **26 passed | 16 skipped (42)** → F20-4 表内原命令 **1 passed | 41 skipped** → `build` exit=0。
`find`（排除生成物）在 `00:03:28` 之后 → **0 个**。
**live 未重跑，判据是同一条 find**（花括号那种简写在 bash 以外不成立，这里写实际跑过的形式）：
`find packages/api-client/src/client.ts packages/api-client/src/events.ts apps/desktop/src/stores apps/desktop/src/components apps/desktop/src/__tests__ -newermt "2026-09-22 23:37:11" -type f` → **空输出**
⇒ 23:37:11 那次 **16 passed / 0 skip** 描述的就是当前运行时代码；本轮改的只有校验脚本、yaml 的 description 与生成物注释。

### 台账纠正（T-43 式：不回头改历史，加作废注）
- 上一段（T-144 子轮）写的"改成**按契约字面取第一个全名**"**自 00:03 起作废**：主控 23:5x 把 ③ 定稿为"最近一个全名"并加 ③b。
  历史行留着是为了记"我当时按当时正文实现"，**现在生效的是 T-150 这一段的形状**。
- 上一段那条"探针 E 现在有了契约依据，不用自己扛"由主控 23:5x 落文（③b 就在 `event-contract` 里），已核对在盘。

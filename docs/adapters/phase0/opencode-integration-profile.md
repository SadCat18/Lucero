# OpenCode 接入前档案（T-444）

- 角色：`survey` ｜ 日期：**2026-10-01**（时刻逐条标）｜ 性质：**纯只读、零代码、零登录**
- 被调查版本：**OpenCode Desktop 1.18.32**（`FileVersion`/`ProductVersion` 双证，22:44:16 现量）
  ⇒ **注意：Phase 0 那份档案是 1.18.14 量的**，本轮已重验关键面（见各节"本版实测"），旧结论只当线索。
- 真消息花费：**0 / 3**（一条没花，原因见 §阻塞）。启动过桌面端并已关干净（23:12:38 复核：进程 0、监听数回到 39）。
- **静态 / 实测分写约定**：本节凡标 `[静]` = 只读浏览二进制/库/文件得到；`[实]` = 我自己发出请求或动作并拿到响应；`[推]` = 推断，不当证据用。

## ⚠ 先说结论里最该被决策的一条：主链今天接不上

`[实]` 23:01:16 启动桌面端 → 它自起一个本地服务：`pid 23832` 监听 `127.0.0.1:61677`（随机端口，非固定 4096）。
`[实]` 我打了 11 个只读端点（`/global/health`、`/health`、`/api/health`、`/provider`、`/config/providers`、`/session`、`/api/session`、`/permission`、`/question`、`/experimental/capabilities`、`/config`）→
**全部 `401`**，`{"_tag":"UnauthorizedError","message":"Authentication required"}`。

`[静]` 凭据来源排查：`~/.config/opencode/` 只有 `opencode.jsonc`(50B)/`.gitignore`/`node_modules`/`package*.json`；
`D:\opencode\data\auth.json`(201B) 里只有**两个 provider 的 API Key**（`deepseek` `***(len 35)`、`moonshotai-cn` `***(len 51)`，字段 `{type,key}`）——**那是模型提供方钥匙，不是这个本地服务的口令**。
⇒ **服务口令不落盘**（`[推]` 由进程内存生成、经 IPC 交给渲染层）。取它要么改配置、要么截 IPC，两者都超出"只读浏览"的字面授权。

**这条不是"探测失败"，是接入方案的前置分叉，需要主控裁：**
1. **(a) 装 `opencode` CLI 自起 `opencode serve`** —— 口令自己定、端口自己定、最干净。**代价：撞红线④（装软件），要用户点头。**（本机现状：`[静]` 22:44 `npm -g` 无 opencode、`D:\opencode\cache\bin\` 空目录）
2. **(b) 复用桌面端内嵌服务** —— 要用户提供该口令，或允许写它的配置项。代价：动第三方配置，禁区。
3. **(c) 只做 LOCAL_DATA 只读 Adapter** —— 不发消息、不碰服务，能覆盖"列会话/读正文/增量/额度读数"，写链为空。

**本轮按授权边界没有自扩**：没装东西、没改配置、没截 IPC、没花消息预算。

---

## ① 事件形态 ↔ §6 七条通知：逐条对（不给"大致支持"）

`[静]` 事实底座（23:0x，只读打开 `opencode.db` 674,971,648B）：持久事件 **37,674 行，type 只有 4 种**
（`message.part.updated.1` 27,047 ｜ `message.updated.1` 8,402 ｜ `session.updated.1` 2,201 ｜ `session.created.1` 24）；
`event` 表列 = `{id, aggregate_id, seq, type, data}`，**无时间戳列**；载荷顶层字段 `[part, sessionID, time]`。
`[静]` 1.18.32 与 1.18.14 在这点上**词表未变**（旧版实测同为这 4 类）⇒ 事件词表稳定性是它的一个优点。

| §6 通知 | 能不能逐条映射 | 依据（怎么量的） |
|---|---|---|
| `stream/delta` | **不能原生映射**，要 Adapter 合成 | `[静]` 逐帧统计 10,308 个 part 的更新次数：`text` 563 个里 91 个只 1 帧、472 个 2 帧、**最大 2**；`reasoning` 1,924 个**全部恰好 2 帧**；载荷字段里 **无 delta/increment/chunk/append 语义字段**（布尔判定 False），`part.text` 是整段。⇒ 见 ② 的"快照 vs 增量"判决 |
| `stream/item` | **能映射，且映射得干净** | `[静]` OpenCode 的 `part` 就是 item：一 part 一 `id`+`messageID`+`type`，type 实测 8 类 `{tool, text, reasoning, step-start, step-finish, patch, file, compaction}`；`tool` part 带 `state` → 工具调用/结果就绪点明确 |
| `stream/state` | **部分能**，但 `confidence` 得我们自填 | `[静]` `tool.state.status` 实测只有 3 值 `{completed: 3567, error: 18, running: 3}`；**没有独立的"状态通道"事件**（4 类里没有 session.state/idle）⇒ `normalized_status` 可由 status 映射，`provider_status_raw` 可原样带，`confidence` OpenCode 侧不提供，只能由 Adapter 按"是否收到该 part 终态"给 |
| `turn/finished` | **原生没有这一帧**，必须合成 | `[静]` 4 类事件里没有任何"一轮结束"类型；`message.updated.1` 只代表消息对象变化。⇒ Adapter 只能靠"末条 message 的 parts 全终态"合成收尾帧，`result_code` 映射建议：`completed→SUCCEEDED`、`error→FAILED`、我方取消→`CANCELLED`；**合成出来的收尾不是 Provider 断言，§6 规则 6① 的"收到≠成功"在这里更要注意** |
| `turn/claimed` | **无对应** | `[静]` 路由与事件词表都没有"认领"语义；`/session/{id}/prompt_async`（本版实测字面量 3 处）只表示受理，不回号 |
| `turn/dispatched` | **无对应** | 同上。⇒ §4.2 那两帧取证信号在 OpenCode 上**只能由 Adapter 自造时间戳+自号**，Provider 侧无法佐证。这点主控该知道：拿 OpenCode 测这两帧的取证强度，等于测我们自己的钟 |
| `approval/request` | **机制存在、载荷未知、通道今天不可达** | 见 ③ |

**通道注意**：`[实]` 以上是**持久 journal** 的形状。SSE（`/event`、`/global/event`、`/event-stream`，本版字面量 19/7/33 处）可能比 journal 更细——**未测**（401）。所以"七条能不能逐条对"的完整答案要等 §阻塞 那三条路选一条之后。

## ② `stream/delta`：真流式还是伪流式

**判决：在我们能读到的那一层，是"伪流式"——快照重发，无增量片段。**

`[静]` 怎么量的：对 27,047 条 `message.part.updated.1` 逐条解析 `part.id`，按 part 分组计数，再按 part 类型分桶：

| part 类型 | part 数 | 总帧数 | 单 part 最大帧 | 分布 |
|---|---|---|---|---|
| `tool` | 3,588 | 17,904 | **164** | {3:1944, 4:525, 5:174, 6:260, 7:198…}（状态/输出反复更新） |
| `reasoning` | 1,924 | 3,848 | **2** | 全部 2 帧 |
| `text` | 563 | 1,035 | **2** | 1 帧:91，2 帧:472 |
| `step-start`/`step-finish`/`patch`/`file` | 2056/2053/91/4 | 各=part 数或 2 倍 | 1~2 | 每 part 1~2 帧 |

`[静]` 载荷字段实测：`event` 顶层 `[part, sessionID, time]`；`part` 字段 `[id, messageID, sessionID, text, type]`；**无 delta 类字段**。
⇒ **多帧的是 tool（因为状态机），文本从头到尾最多两个快照。**

**对我们两件事的直接含义（这才是主控要的）：**
- **250–500ms 攒批够用**——够用得很反常：一路 text 最多产 2 个快照，攒批几乎无事可攒。
- **`after_seq` 补拉不够用**。`[静]` `seq` 是**聚合内单调号**（旧版实测单会话 `0..1547`），但它索引的是**已落盘的持久事件**；OpenCode 不落盘中间增量，所以断线重连后按 `seq` 补，**补回的是快照序列，补不回缺失的中间态**，而且 Core 侧的 `sequence_no` 不能直接沿用它的 `seq`（幂等键是 `(turn_ref, sequence_no)`，`seq` 只在 session 聚合内唯一，跨聚合会重号）。
- ⇒ 若我们要 `stream/delta` 有内容，**Adapter 必须做"相邻快照 diff"合成 delta**，并自己维护每 turn 单调 `sequence_no`。这是实现量，别当成 Provider 白送。

**诚实边界**：`[静]` 这不等于"OpenCode 的 SSE 不发 token 级帧"。持久层与总线路层可能不同，**未端到端验证**（401 未测）。按当前证据，稳妥设计是"当作伪流式"，若日后拿到服务凭据再翻案。

## ③ 审批：它自己那一侧有没有，我们那条载荷够不够

**有机制，但我今天看不到它的载荷。**

- `[静]` 本版路由字面量：`/permission` 8、`/permission/request` 3、`/permission/saved` 3、`/permission/{requestID}/reply` 2、`/permissions/{permissionID}` 2；另有独立一族 `question`：`/question` 6、`/question/request` 3、`/question/{requestID}/reply` 2、`/question/{requestID}/reject` 2。
  ⇒ **OpenCode 把"权限审批"和"向用户提问"做成两套对象**，不是一套。
- `[静]` 库里 `permission` 表结构 = `{id, project_id, action, resource, time_created, time_updated}`——**字段名 `action`/`resource` 与我们载荷里的 `action`/`resource` 天然对得上**（这是好消息）。
  但 **该表 0 行**（1.18.14 时也是 0 行）⇒ **本机从未产生过一条持久权限记录**，`action` 的**取值集合我拿不到**。
- `[静]` 事件词表里**没有任何 permission/question 类型** ⇒ 审批不落 journal ⇒ **即使不 401，也只在实时总线上可见，历史不可回放**。这对我们"审批可追溯"的要求是个额外缺口（要提 REQ，见下）。

**`action` 串 ↔ `permission` 内置分类的映射（只用实测到的工具名）**：
`[静]` part.tool 实测分布 = `read 1202 / bash 1157 / grep 577 / edit 481 / glob 92 / write 39 / todowrite 19 / task 17 / question 4`

| OpenCode 工具 | 我们的 action | 依据 |
|---|---|---|
| `bash` | `command.exec` | `[静]` 实测存在，语义明确 |
| `edit` / `write` | `filesystem.write` | `[静]` 实测存在 |
| `read` / `grep` / `glob` / `todowrite` | 非高危（只读/内部） | `[静]` 实测存在 |
| `task` | 无对应高危类（子代理派发） | `[静]` 实测存在 |
| `question` | 属"提问"不属"审批" | `[静]` 实测存在，且它有独立 `/question/*` 路由 |
| （webfetch / 远程类 / db / deploy） | 设想 `remote.*` / `db.write` / `deploy` | **`[推]` 样本里一个都没出现，不给结论** |

**载荷够不够**：`{turn_ref, action, resource?, risk_hint?, provider_execution_id?}`（协议 §4 表第 91 行原文是这 5 个，`risk_hint`/`provider_execution_id` 可选——主控口述成 4 个，我按原文）。
判断：**字段本身够用，缺的不是字段，是它的取值集合与实时通道**。⇒ **按 ③ 的要求我不加字段，回来提这两条 REQ：**
- **REQ-OC-1**：`approval/request` 的 `action` 需要一个"Provider 工具名 → 内置分类"的**映射表登记位**（各家工具名不同：OpenCode 小写 `bash/edit/write`，WorkBuddy 首字母大写 `Bash/Edit/Write`）。**不要为它扩 `permission` 的内置枚举**——扩枚举会把"失败关闭成要审批"那条默认绕过去。
- **REQ-OC-2**：OpenCode 审批**不落 journal**，所以 `approval/request` 是"错过即无"的一帧。若 Core 要可追溯，得在**我们侧**落 `approval` 聚合（现有设计已有 `approval_requested` 事件），这一条只是提醒：**不能假设可以从 Provider 侧回捞历史审批**。

## ④ 额度 / 模型清单 / 中断：实际形状与置信度

### 额度（§7 契约的直接落地）
`[静]` 怎么量的：对 `message`(2,179) 与 `session`(24) 的 cost/tokens 列分别数 NULL、=0、>0 三种情况：
- `session`：24 行，`cost IS NULL` **0**，`cost = 0` **15**，`cost > 0` **9**；`tokens_input = 0` **1**，`IS NULL` **0**。
- `message`：2,179 行，`cost IS NULL` **116**，`cost = 0` **1,600**（其余 >0）。
⇒ **0 与 NULL 在 OpenCode 自己的数据里同时存在**，且 0 是多数。
→ **契约② §7 落地方式**：`cost = 0` **既不能读成"用完了"也不能读成"免费"**；`cost IS NULL` 才明确是取不到。
建议 Adapter 的观测：`observation_kind: OBSERVED`，`source: "local_db/message.cost"`，
`confidence`：**`cost > 0` → HIGH；`cost = 0` → LOW（因为分不清真实 0 价与未上报）；`NULL` → 不产出该条**。
账户级额度：`[实]` `/provider`、`/config/providers` 全 401，`[静]` `/usage`(11)、`/quota`(2)、`/rate`(5) 字面量存在但**无法证实是额度端点**。⇒ `reset_at = null`、`window_start/end = null`、**`overall_confidence: UNKNOWN`**。这是今天的诚实默认，不是"读到了 0"。

### 模型清单
- `[实]` 实时通道：**不可达**（401）。
- `[静]` 离线通道：`D:\opencode\cache\models.json`（Phase 0 于 09-19 量得 4,690,520B / 221 个 provider 键；**本轮未重测大小**，只确认路径仍在）。
- ⇒ 若接 OpenCode，模型清单**默认走缓存文件**，且必须标 `source: cache_file` + `confidence: MEDIUM`（缓存可能过期）；不要用"读不到就不显示"糊过去。

### 中断
- `[静]` 本版存在：`/abort` 字面量 5 处、`/interrupt` 11 处、`/session/{id}/abort` 与 `/api/session/{id}/interrupt` 两代并存，另有 `/revert` + `/revert/{stage,commit,clear}`（各 3）。
- `[实]` **未调用**（401）。⇒ 中断能力**等级维持"路由存在、行为未验证"**，不许写成可用。
- 额外注意：`[静]` 两代命名并存（`abort` vs `interrupt`）在 1.18.32 **仍然如此** ⇒ §2 版本契约测试要按版本分派路径。

---

## 本轮没做的、以及为什么

| 事项 | 状态 | 原因 |
|---|---|---|
| 发真消息测 SSE 帧密度 | **blocked** | 服务 401 且口令不落盘；取口令需改第三方配置或截 IPC，超出授权 |
| 测 `/permission` 真实载荷 | **blocked** | 同上；且 `permission` 表 0 行，本机无历史样本 |
| 测 `/session/:id/abort` 行为 | **blocked** | 同上 |
| 装 `opencode` CLI 另起服务 | **未做** | 红线④（装软件），要用户点头 |
| 花那 3 条消息预算 | **0 条** | 没有可发消息的通道；不为"给出好看结论"而外推 |

## 给主控的一句话

**接 OpenCode 之前要先裁的是"服务从哪来"（上面 a/b/c 三选一），不是"驱动怎么写"。**
选 (a) 装 CLI ⇒ 顺带能把 ②③④ 全部实测重做一遍；选 (c) ⇒ 今天就能开工，但只能读、写链为空，
且 `turn/claimed`/`turn/dispatched`/`turn/finished` 三帧都得 Adapter 自造（取证强度弱，§4.2 那两帧别拿它验收）。

OWNER_ROLE: frontend
TASK_ID: api-client-panel
STATUS: in_progress
SCOPE: `apps/desktop/**`、`packages/api-client/**`、`specs/openapi/**`（契约③ §7 原文；禁止 import Core 内部类型）
EVIDENCE_DIR: .qoder/evidence/frontend/
LAST_UPDATED: 2026-09-20T12:10+08:00

## Objective

把契约③ 投影成机器可读的 `specs/openapi/core-v1.yaml`，建立「yaml → TS 类型」的 codegen 链路，
并搭出只能靠 REST/WS 渲染契约① §7 那条 11 步垂直链的 Vite + Vue 3 + TS 面板骨架；
UNKNOWN 在任何分支下都渲染成「未知」，不得折成 0 / Offline / Failed。

## Completion criteria

- [x] `cd /d/Project/Lucero/apps/desktop && npm.cmd run build` 无错无类型报错（12:08 重跑 exit 0）
- [x] yaml 与 18:03 版 §3/§4 逐字对齐：36 path / 40 operation，validator 双向校验（缺一条报 FAIL、多一条也报 FAIL）
- [x] TS 类型全部由 yaml 生成（本轮又抓出并修掉一处我自己手写的帧联合类型，见 Completed-9）
- [x] 断言 17 条全绿 + 四次变异验证证明会咬人
- [ ] 真浏览器跑完 §4 的 11 步链 — **blocked**（B-F1 正在解除：backend 交 health + ws-ticket 最小骨架后可跑）
- [ ] Wave-3 补 PNG（主控决定，当前只交文本证据）

## Scope boundaries

- In scope: 契约③ §7 的三行 —— `apps/desktop/**`、`packages/api-client/**`、`specs/openapi/**`；本角色的 progress / evidence / `.scratch/frontend/**`
- Out of scope: 契约其余文件与 `docs/**`、`core/**`、Tauri/Rust、五家真实软件、git 写操作、PATH/全局安装

## Current checkpoint

- Stage: T-26 八条回复全部落地（F-1/3/4/5/6/8/9/10/11）；联调侧已就绪，等 backend 的最小 core 骨架
- Status: in_progress
- Last verified action: 本机时间 **2026-09-20 12:08:08 +0800** 四道闸门一次性重跑全绿（validate 全部通过 / codegen 80.9ms / vitest 17 passed / build ✓ built in 630ms）；
  12:0x 真浏览器 DOM 实测（`browser-batch7.txt`）确认短票换连失败诚实降级 + bootError 上屏。此后只改了本进度文件。
- Next action: 等 backend 的 `GET /health` + `POST /ws-ticket` 一落地 → 设 `LUCERO_CORE_ORIGIN` 指到真实端口，
  在真浏览器验证「换票 → hello_ack 抬游标 → 实时事件」，再按 §4 逐步推 11 步链（REST 半边仍会 501，届时如实记 blocked）
- Verification: `cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`

## Completed

### 第一轮（至 17:34）
`core-v1.yaml` 首版投影（§2 游标/幂等/If-Match/principal/错误体/Unix 毫秒/provider_raw；§5 四帧用 `x-websocket-frames`；
契约① §6 → `EventType` 59 条；契约② §5/§7 → `DeliveryMode`、`QuotaResponse`）；`packages/api-client`
（client / events / unknown 三件，13 个查询参数类型全取 `operations[...]`）；`apps/desktop` 面板骨架（8 组件 + store，
无 pinia、无 UI 框架）；断言 + 变异验证；真浏览器（无 Core）实测 UNKNOWN 全分支。

### 第二轮（T-17，至 17:56）
§2.1 短票落地（yaml `POST /api/v1/ws-ticket` + `WsTicket`；`openEventStream.url` 改可异步 → **每次重连换一张票**；
换票失败按退避如实报 `reconnecting` 不谎报 live）；§3 discover 改 `POST /api/v1/adapters/{id}/sessions:discover` +
`:manage`/`:sync`；`packages/toolchain` 按裁定删除并并入 `apps/desktop`；SCOPE 行改回 §7 原文。

### 第三轮（T-26 八条回复）
1. **F-1**：§3 已补 send → 复核确认 yaml 的 `post /api/v1/executions/{id}/send` 与 §3/§4 一致；
   并把 §3 的 body 逐字对齐：**去掉我原先自造的 `model_id`**，补 `attachments`（数组）。
2. **F-4**：`error.code` 从 free string 改成 `ErrorCode` —— 已登记枚举
   `[version_conflict, illegal_transition, aggregate_not_found, delivery_mode_unsupported]` + `pattern ^[a-z][a-z0-9_]*$`
   两段式（枚举没写全前不把集合钉死，但命名必须是 HTTP 层小写蛇形）。validator 三条断言：两段式在位、
   pattern 正确、枚举里不许混进大写蛇形（RPC 面的码）。
3. **F-8**：补 `GET /api/v1/tasks/{id}`、`GET /api/v1/approvals/{id}`、`GET /api/v1/sessions?project=`；
   client 加 `getTask`/`getApproval`，`runChain` 的 accept 前不再用「列表 + find」而是 `GET /tasks/{id}` 取最新 version。
4. **F-9**：新增 `WsHelloAck{type,server_seq}` 帧并纳入 `WsFrame`；`events.ts` 处理：不带 from_seq 时首帧
   `hello_ack` → **把游标抬到 server_seq**（不抬就会永远落在旧位置），带 from_seq 的 `hello` 只是回显、不动游标；
   两者都不进时间线、不算事件。控制帧不回退游标（只前进）。`x-websocket-frames.cursor-default` 写明禁「缺省即全量重放」。
5. **F-11**：反向断言升级为**豁免集相等**断言 —— yaml 里带豁免的端点集合必须与 §2 表逐字相等
   （现在恰好只有 `post /api/v1/ws-ticket`；将来谁偷偷加豁免，这里直接红）。
6. **F-3**：dev proxy 维持「只转发不注入凭据」（与 17:35 否掉的那件事不是一回事，已按新口径确认）；
   补了诊断位：REST 首跳失败原因上屏（`data.bootError`），403 时直接提示「Origin 不在 LUCERO_DEV_ORIGIN 白名单内」。
7. **F-5 / F-6 / F-10**：F-5 无需改动（面板本来就是 flush 批次粒度，不做假节奏流式）；F-6 把 `JsonMap` 说明改成裁定口径
   （Core 边界解一次，UI 不再解第二遍）；F-10 不动（链判据归 `test`）。
8. **validator 加了 §4 交叉核对**：把 §4「客户端调用」列的 REST 半边写成 `CHAIN_CALLS` 逐条断言在位
   （9 条全 ok），这样「35/36 path 是否和 §3+§4 一致」是机器判的，不是我肉眼说对的。
9. **自纠两处**：
   - 我一开始在 `events.ts` 手写了 `WsFrame = WsHello | WsEventFrame | ...` 联合类型 —— 这是第二套接口定义，
     违反硬约束 2，且正因为重写所以 hello_ack 进来时 TS 报 `Type '"hello_ack"' is not comparable`。改成取生成物 `S['WsFrame']`。
     **这个错是 build 抓的，不是我抓的。**
   - 我用 `String.prototype.replace` 插 ErrorCode 时，替换串里的 `$'` 被当成「把匹配之后的全文插到这里」，
     于是 yaml 尾部被复制了 601 行（`Health:`/`TaskList:` 等都出现两份），并把同批另两处改动吞进了那份副本里。
     已定位、按锚点删除重复段、把丢掉的 send/WsFrame 改动重新落地。以后脚本里替换串一律用函数形式 `() => text`。
10. **工具教训**：本会话里 Edit/Write 的「updated code」回显与 `take_snapshot` 的 a11y 快照**多次是陈旧内容**
    （快照一度显示旧握手文案，而同一时刻 DOM innerText 与页面内模块实时值都是新的）。
    本轮起所有断言落地都靠 `grep` / `evaluate_script` 读真值复核；已写进 `browser-batch7.txt` 供 test 角色避坑。

## Remaining

- 等 backend 最小骨架 → 真浏览器联调（换票、hello_ack、实时事件、11 步链）
- 等契约文本回写（见下面 4 条 lag）后，按 §6.3 三处同步核一遍

## Blockers

- **B-F1（正在解除）**：`core/api/**` 仍不存在。backend 将先交 `GET /health` + `POST /api/v1/ws-ticket`（其余 501）。
  我的联调侧已备好：换票建连、`hello_ack` 抬游标、`resync_required` 回落 REST、501/404/非错误体一律记 blocked 不记 ok。
  **在拿到真响应之前不主张任何联调结论。**
- ~~B-F2~~ 已关：PNG 推 Wave-3。

## Coordination

已闭：F-2（短票）、F-7（discover 改形）、F-11（幂等豁免，反向断言 + 豁免集相等断言已钉）、F-5、F-6、F-8、F-9、F-10、F-1、F-3（口径已确认）。

**契约文本滞后（我按裁定实现了，但文件里还没写；§6.3 要求三处同步，故列入）**：

- **LAG-1（F-8）**：§3 仍未写 `GET /api/v1/tasks/{id}`、`GET /api/v1/approvals/{id}`、`GET /api/v1/sessions?project=`
  （消息里批了，文件里没有）。yaml 已按批准的补，validator 的 REQUIRED 清单含这三条。
- **LAG-2（F-9）**：§5 仍只有 `hello`/`resync_required`/`ping`，没有 `hello_ack`，也没写「不带 from_seq = 只收此后新事件」
  与「禁缺省全量重放」。yaml + 客户端已按裁定实现。契约① §2/§8 同样滞后。
- **REQ-F-12（新，F-4 的边角）**：§3 的 send 行写「不支持时返回 `DELIVERY_MODE_UNSUPPORTED`」—— 大写蛇形是 RPC 面的形状，
  与刚裁定的「HTTP 层小写蛇形」在这同一行里打架。我在 `ErrorCode` 里登记的是 `delivery_mode_unsupported`，
  并把「枚举里不许出现大写形」写成断言。请主控把 §3 那一行的 HTTP 面拼写改掉，或说明 HTTP 层就是要透传 RPC 原码。
- **REQ-F-13（新）**：§3 的 send body 有 `attachments?`，但 §3 没有任何上传/读取附件（`attachments`/`blobs` 表）的端点。
  我按「附件 id 字符串数组」投影，面板 v0.1 不做附件 UI。若 Wave-3 要附件，需要先补端点。
- 契约②「错误码集合」一节等主控补；补完我扩 `ErrorCode` 枚举并重生成。

## Changed files

第三轮：`specs/openapi/core-v1.yaml`（ws-ticket/hello_ack/ErrorCode/send body/F-8 三端点/cursor-default/JsonMap 口径）、
`apps/desktop/scripts/validate-openapi.mjs`（§4 交叉核对 + 短票与幂等豁免断言 + 错误码断言）、
`packages/api-client/src/{client.ts,events.ts}`、`packages/api-client/src/gen/schema.ts`（重生成）、
`apps/desktop/src/{stores/panel.ts,components/StatusHeader.vue}`、`src/__tests__/chain.test.ts`。
前两轮：见上文 Completed，以及 `packages/toolchain/**`（已删）。

依赖无新增（仍是 apps/desktop 那一套：vue / vite / @vitejs/plugin-vue / typescript / vue-tsc / @types/node / vitest /
@apidevtools/swagger-parser / openapi-typescript）。

## Verification evidence

- `.qoder/evidence/frontend/gates-final.txt` —— **本机时间 2026-09-20 12:08:08 +0800** 的一次性重跑：
  `npm run validate` → `全部通过`（36 path；§3 的 40 条逐一对上；无契约外端点；§4 九条 REST 半边全 ok；
  幂等豁免集与 §2 表完全一致：`post /api/v1/ws-ticket`；WS 帧 hello/hello_ack/event/resync_required/ping 齐备；
  from_seq 缺省语义钉死；error.code 两段式；SendMessageRequest 与 §3 逐字一致）；
  `npm run codegen` → 80.9ms；`npx vitest run` → `Test Files 2 passed (2)` / `Tests 17 passed (17)`；
  `npm run build` → `✓ built in 630ms`（vue-tsc 无错）
- `.qoder/evidence/frontend/browser-batch7.txt` —— 真浏览器 DOM 实测：
  `reconnecting | 握手前换票失败：Error: Internal Server Error（4000ms 后重试）`、`游标 seq=未知`、
  `Core API 未连通（core/api/** 尚未落地） · 500 not_core_error_body`；页面内模块实时值 `data.bootError` 同步为证。
  附「a11y 快照会返回陈旧文本，取证以 evaluate_script 为准」的坑位说明。
- 变异验证累计四次，全部会咬人：`num()` 折 0 → 1 红；`statusText()` 折 UNAVAILABLE → 7 红；
  换票失败谎报 `live` → 1 红；`hello_ack` 不抬游标 → 1 红（`expected null to be 1200`）。每次都绝对路径回滚 + `diff` 确认。
- 环境：`node v24.19.0` / `npm 11.17.0`（`D:\DevTools\node-v24.19.0-win-x64`，未入 PATH）；`netstat` 确认 5173 无残留监听。

**未主张**：不含任何端到端联调结论。§4 的 11 步链在真实 Core 上跑通仍未验证（B-F1）。

## Recovery note

- Interruption point: T-26 八条全部落地并四道闸门全绿；停在等 backend 最小 core 骨架与 4 条契约文本回写
- First action after recovery: `cd /d/Project/Lucero/apps/desktop && npm run validate && npx vitest run && npm run build`
  确认基线未被动过；若 backend 已交骨架 → 设 `LUCERO_CORE_ORIGIN`（只影响 dev proxy 目标）后真浏览器验
  「换票 → hello_ack → 事件流」，并按 §4 推 11 步链，逐跳记 ok/blocked 进 `.qoder/evidence/frontend/`

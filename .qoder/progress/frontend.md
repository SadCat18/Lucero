OWNER_ROLE: frontend
TASK_ID: api-client-panel
STATUS: in_progress
SCOPE: `apps/desktop/**`、`packages/api-client/**`、`specs/openapi/**`（契约③ §7 原文；禁止 import Core 内部类型）
EVIDENCE_DIR: .qoder/evidence/frontend/
LAST_UPDATED: 2026-09-19T17:56+08:00

## Objective

把契约③ 投影成机器可读的 `specs/openapi/core-v1.yaml`，建立「yaml → TS 类型」的 codegen 链路，
并搭出只能靠 REST/WS 渲染契约① §7 那条 11 步垂直链的 Vite + Vue 3 + TS 面板骨架；
UNKNOWN 在任何分支下都渲染成「未知」，不得折成 0 / Offline / Failed。

## Completion criteria

- [x] `cd /d/Project/Lucero/apps/desktop && npm.cmd run build` 无错无类型报错（`vue-tsc --noEmit && vite build`）
- [x] yaml 覆盖契约③ §3（含 17:35 裁定后的形态）+ §2.1 短票 + §2 约定，标准 OpenAPI 解析器可读（35 path / 42 operation）
- [x] TS 类型全部由 yaml 生成；`gen/` 之外只剩 2 个 interface，均为传输层配置而非契约结构
- [x] UNKNOWN 断言 16 条全绿（含组件级 SSR 渲染），且三次变异验证证明断言会咬人
- [ ] 真浏览器跑完 §4 的 11 步链 — **blocked**（B-F1：`core/api/**` 未落地）。PNG 按主控决定推到 Wave-3
- [ ] 面板业务化增强 —— 联调后按需做；高级 Dashboard 仍不做（SPRINT §8）

## Scope boundaries

- In scope: 契约③ §7 那三行 —— `apps/desktop/**`（含 package.json / lock）、`packages/api-client/**`、`specs/openapi/**`；
  以及本角色的 `.qoder/progress/frontend.md`、`.qoder/evidence/frontend/**`、`.scratch/frontend/**`
- Out of scope: 契约其余文件与 `docs/**`（缺口只写 Coordination）、`core/**`、Tauri/Rust/WebView、五家真实软件、
  git 写操作、PATH/全局安装、`packages/toolchain/**`（17:35 已驳回，已删除）

## Current checkpoint

- Stage: 第四批裁定（T-17）全部落地 —— §2.1 短票、§3 discover 改形、toolchain 并回 desktop、本文件 SCOPE 行改回 §7 原文
- Status: in_progress
- Last verified action: 17:52 四道闸门（validate / codegen / vitest / build）全部 exit 0；
  17:54 真浏览器复验握手改造无回归（只读 DOM，未再改代码）。证据见下。
- Next action: 停。等 REQ-F-1/3/4/5/6/8/9/11 的裁定与 `core/api/**` 落地 —— 见 Coordination
- Verification: `cd apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`

## Completed

### 第一轮（上岗至 17:34）

1. **`specs/openapi/core-v1.yaml`**（OpenAPI 3.1.0）—— 契约③ §3 端点表逐条投影：
   §2 游标分页 `CursorPage{items,next_cursor}`（next_cursor 可 null、无 offset）、`Idempotency-Key`、`If-Match`、
   `X-Lucero-Principal`、统一错误体 `{error:{code,message,retryable,details}}`、时间 Unix 毫秒、`provider_raw` 与归一化字段并存；
   §5 WS 四帧用根级 `x-websocket-frames` 表达（3.1 无标准 WS 语法）；契约① §6 事件目录 → `EventType` 59 条；
   契约② §5/§7 → `DeliveryMode`、`QuotaResponse{observations[],overall_confidence}`；资源字段名对齐冻结的 `000001`（只读投影，未改该文件）
2. **api-client**：`client.ts`（13 个查询参数类型全取 `operations[...]['parameters']['query']`，零手写第二套契约结构；
   非统一错误体的响应判定为「对端不是 Core」）、`events.ts`（seq 只前进、重叠按 seq 幂等丢弃、退避重连、stalled≠live、resync 交回 REST）、
   `unknown.ts`（§12 P0 显示层）
3. **面板骨架**：StatusHeader / ChainRail（11 步，完成只看事件）/ EventTimeline / TaskPanel / SessionPanel /
   ApprovalPanel（只发决议不裁决）/ QuotaPanel / OutputPanel，store 在 `src/stores/panel.ts`；无 pinia、无 UI 框架、手写 CSS
4. 断言 + 两道变异验证；真浏览器（Core 未起）实测 UNKNOWN 全分支

### 第二轮（接 T-17 裁定）

5. **§2.1 短票落地**：yaml 新增 `POST /api/v1/ws-ticket` → `WsTicket{ticket,expires_in}`，
   `x-websocket-frames.handshake` 写明 ticket 必填；`openEventStream.url` 改为可异步（票单次使用 → **每次重连换一张**）；
   换票失败按退避重来并如实报 `reconnecting`，不谎报已连接；store 的 `ticketedUrl()` 用 `URLSearchParams` 拼 ticket + from_seq。
   validator 加了两条断言：WS 握手必须要求 ticket；`ws-ticket` 端点**出现** Idempotency-Key 即报 FAIL（幂等缓存会交出已作废的票）。
6. **§3 discover 改形**：`POST /api/v1/sessions/{id}/discover` → **`POST /api/v1/adapters/{id}/sessions:discover`**
   （无请求体，返回 `DiscoveredSessionList{items[{session_id,remote_session_id}]}`）；`manage`/`sync` 转 `:verb`。
   client、store `runChain`（拿返回的 session_id 再 `:manage`；无可发现会话时标 blocked，不再客户端造 id）、测试同步。
7. **越界纠正**：`packages/toolchain/` 已删（含 node_modules）；`@apidevtools/swagger-parser` + `openapi-typescript`
   并入 `apps/desktop` devDependencies；校验脚本迁到 `apps/desktop/scripts/validate-openapi.mjs`；
   `npm run validate` / `npm run codegen` 收进 desktop。一次性脚本全在 `.scratch/frontend/`，仓库根无残留。
8. **SCOPE 行改回 §7 原文**。我第一轮把它写成了包含 `packages/toolchain/**` 的版本 —— 越界由主控指出，接受：
   约束对象的定义权不在被约束方，即使如实标注也不能自扩。以后只引用 §7 原文，不再自写 SCOPE。

## Remaining

- 等裁定 → 按结论改 yaml → `npm run codegen` 一次
- 等 `core/api/**` → 真浏览器 11 步链 + Wave-3 的 PNG

## Blockers

- **B-F1 端到端联调**：`core/api/**` 的 handler 不存在（含 §2.1 的换票端点与 `?ticket=` 校验）。
  面板/codegen/短票客户端链路可先行且已先行；**mock 只用于组件级断言，未据此报过任何绿**。
  解除条件：backend 交付可运行的 `/api/v1/*`（含 ws-ticket）+ `/ws/v1/events`。
- ~~B-F2~~ 已按主控决定关闭：PNG 推到 Wave-3，本轮交文本证据（`browser-no-core.txt`、`browser-ticket.txt`）。

## Coordination

（结论 = 当前既定事实；「要裁」= 需要主控拍板。本角色未改任何契约文件）

- **REQ-F-1** §4 的 `POST /executions/{id}/send` 在 §3 端点表里仍不存在；yaml 已按 §4 收录并纳入 validator 的 REQUIRED 清单 → **要裁**（补进 §3 或指认替代）。
- **REQ-F-2** WS 一次性短票 → **已裁，已全链路落地**（yaml/client/events/store/断言/浏览器复验）→ 不要。
- **REQ-F-3** §1.3「带 Origin 一律 403」与"面板在普通浏览器里"仍冲突。裁定否掉的是"反代注入鉴权"，我保留的是**只转发、不注入任何凭据**的 dev proxy（否则浏览器面板根本到不了 Core）。→ **要裁**：`http://localhost:5173` 这个 Origin 是否放行，或给正式 dev 白名单。
- **REQ-F-4** 契约② 仍无「§错误码集合」章节；`version_conflict`（小写）与 `DELIVERY_MODE_UNSUPPORTED`（大写）两套风格 → **要裁**（定落点 + 定命名）。yaml 里 `error.code` 暂为开放字符串。
- **REQ-F-5** Core→client 的 token 级帧仍无定义（`stream/delta` 是 A→C 通知），面板用 `GET /executions/{id}/output?after_seq=` 在 `execution_output_flushed` 时补拉 → **要裁**（不阻塞）。
- **REQ-F-6** `*_json` 列在响应里是对象还是 JSON 字符串仍未定；yaml 按对象（`JsonMap`）投影 → **要裁**（handler 落地时对齐一次即可）。
- **REQ-F-7** discover 改 adapter 侧 + `:manage`/`:sync` → **已裁，已落地**。但契约③ §4 链映射表仍写 `POST /sessions/{id}/discover`、`POST /sessions/{id}/manage` 两行旧写法，与 §3 矛盾（yaml 按 §3）→ **要主控顺手同步 §4 文字**（我不碰契约）。
- **REQ-F-8** 无 `GET /tasks/{id}`、`GET /approvals/{id}`，`GET /sessions` 无 `?project=`；我未擅自补，面板按项目筛会话只能全量翻页 → **要裁**（低优先）。
- **REQ-F-9** `/ws/v1/events` 不带 `from_seq` 的缺省语义仍未定义（§2.1 只补了 ticket）。resync 后我只能"不带 from_seq 重连"= 赌缺省 → **要裁**。
- **REQ-F-10** §4 的 Resolve Approval 一行含 `approval_resolved`/`grant_issued`，§7 另有 `execution_approval_resolved`；面板把三者都算该步完成 → 备注，不要裁。
- **REQ-F-11（新）** §2「有副作用的 POST 必须带 Idempotency-Key」与 §2.1 短票字面冲突（票被幂等缓存 → 重放交出已作废的票）。
  我在 yaml 里不要求该头，并加了反向断言钉住 → **要一句确认**：豁免成立否。若主控判"照样要带"，我改 yaml + client 一处即可。

## Changed files

第二轮：
- `specs/openapi/core-v1.yaml`（新增 ws-ticket / discover 改 adapter 侧 / `:manage` `:sync` / WS 握手说明）
- `apps/desktop/{package.json,package-lock.json}`（并入两个工具依赖 + validate/codegen 脚本）
- `apps/desktop/scripts/validate-openapi.mjs`（迁入 + 新端点与短票断言）
- `apps/desktop/src/stores/panel.ts`、`src/__tests__/chain.test.ts`
- `packages/api-client/src/{client.ts,events.ts}` + `src/gen/schema.ts`（重新生成）
- 删除 `packages/toolchain/**`
第一轮：`specs/openapi/core-v1.yaml`、`packages/api-client/src/{client,events,unknown}.ts` + `gen/schema.ts`、
`apps/desktop/{index.html,tsconfig.json,vite.config.ts,src/main.ts,src/App.vue,src/components/*.vue(8),src/stores/panel.ts,src/__tests__/*.test.ts(2)}`

新增依赖（全部 local，未全局、未改 PATH）：
`apps/desktop` = `vue@3.5.34`、`vite@6.4.3`、`@vitejs/plugin-vue@5.2.1`、`typescript@5.9.3`、`vue-tsc@2.2.12`、
`@types/node@22.19.15`、`vitest@2.1.9`（组件渲染用 vue 自带 `vue/server-renderer`，未引 @vue/test-utils 与 DOM 库）、
`@apidevtools/swagger-parser@10.1.1`（验收要求"标准 OpenAPI 解析器"）、`openapi-typescript@7.13.0`（§6.2 要求类型从 yaml 生成）。
后两个原本装在 `packages/toolchain`，按裁定搬进 desktop，未新增第三个依赖。

## Verification evidence

17:52 一次性重跑四道闸门，全部 exit 0（此后只做了浏览器只读复验，未再改代码）：

- `npm run validate` → `OpenAPI 3.1.0 解析通过：35 个 path`、`无契约外端点`、
  `35 个写操作的头声明齐备（ws-ticket 按 §2.1 语义豁免幂等键）`、`WS 握手要求一次性短票`、
  `契约① §7 链的 22 类事件全在目录内（目录 59 条）`、8 个状态枚举含 UNKNOWN、7 个可空字段可为 null → `全部通过`
  → `.qoder/evidence/frontend/validate-openapi.txt`
- `npm run codegen` → yaml → `packages/api-client/src/gen/schema.ts` → `.qoder/evidence/frontend/codegen.txt`
- `npx vitest run` → `Test Files 2 passed (2)` / `Tests 16 passed (16)` → `.qoder/evidence/frontend/tests.txt`
- `npm run build`（vue-tsc --noEmit && vite build）→ exit 0，`✓ built in 931ms` → `.qoder/evidence/frontend/build.txt`
- 真浏览器复验（Core 未起）→ `browser-ticket.txt`：头部渲染
  `reconnecting | 握手前换票失败：Error: Internal Server Error（4000ms 后重试） | 游标 seq=未知 | Core 未知 / mig 未知`
  —— 换不到票时不谎报连接；游标与额度仍「未知」不是 0。第一轮证据在 `browser-no-core.txt`（点链后只有 blocked 一行，无一步 ok）
- 断言变异验证三次：`num()` null 折 0 → 1 红；`statusText()` UNKNOWN 折 UNAVAILABLE → 7 红；
  `events.ts` 换票失败谎报 `live` → 1 红（`expected 'live' to be 'reconnecting'`）。三次均 `diff` 确认回滚干净后才重跑取绿。
  教训：变异回滚必须用绝对路径 —— 我从 `apps/desktop` 用 `../packages/...` 回滚过两次都静默失败过。

**未主张**：不含任何端到端联调结论。§4 的 11 步链在真实 Core 上跑通仍未验证（B-F1）。

## Recovery note

- Interruption point: T-17 裁定全部落地，四道闸门 + 浏览器复验全绿；停在等其余裁定与 `core/api/**`
- First action after recovery: 读本文件 Coordination。若有新裁定 → 改 `specs/openapi/core-v1.yaml` →
  `cd /d/Project/Lucero/apps/desktop && npm run validate && npm run codegen && npx vitest run && npm run build`；
  若 Core 已落地 → 设 `LUCERO_CORE_ORIGIN` 指向真实端口，重跑浏览器 11 步链并补 PNG

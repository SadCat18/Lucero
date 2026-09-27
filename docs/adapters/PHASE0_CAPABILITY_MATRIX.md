# Phase 0 汇总：Adapter Capability Matrix

- 编制：`survey` 角色 ｜ 日期：2026-09-19 ｜ 状态：**五家全部定级；WorkBuddy 已完成第 2 轮动态实测并据此改判**
- 本文只做**等级、打分、排序、层级建议**。逐项证据与 UNKNOWN 原因只在单家报告里：
  `codex.md` / `opencode.md` / `workbuddy.md` / `zcode.md` / `qoder.md`（同目录）
- 取证方式分两档（**必读 §7**）：
  - 四家（Codex / Qoder / OpenCode / Z Code）**纯静态**：CLI `--help`、协议 schema 生成、SQLite schema + 枚举实测、二进制路由扫描、注册表、netstat。**真实消息预算 0 / 12 未使用。**
  - 一家（WorkBuddy）**静态 + 动态实测**（用户单次授权）：起服务、发真消息、读回正文、验证流式/额度/中止/复用登录。**真实消息预算 3 / 3 已用尽。**

## 0. 被调查版本号（一行一家）

| 厂商 | 版本（实测） | 实物 |
|---|---|---|
| Codex | CLI **0.154.0** ／ Desktop MSIX **26.915.3509.0** | `~\AppData\Local\Programs\OpenAI\Codex\bin\codex.exe` |
| Qoder | Desktop **0.3.3** ／ 引擎 **1.1.53**(`entrypoint=cli`) | `D:\Qoder\.qoder-versions\0.3.3\Qoder.exe` |
| OpenCode | Desktop **1.18.14**（**CLI 本机未安装**） | `%LOCALAPPDATA%\Programs\@opencode-aidesktop\` |
| Z Code | Desktop **3.12.3.7463** ／ 引擎 **0.16.x** | `D:\Program Files\ZCode\ZCode.exe`（**无 CLI**） |
| WorkBuddy | Desktop **5.5.6** ／ 内置 CodeBuddy CLI **2.137.1**（API 契约 **OpenAPI 3.1.0 / 113 paths**）／ sandbox **5.5.5** | `D:\Program Files\WorkBuddy\` |

> 五家有 **2~3 条独立版本线**。Core 的 `software_version` 字段必须是**多值结构**，单个字符串放不下。

## 1. 能力矩阵（25 项 × 5 家，只填等级）

图例：`N`=NATIVE `C`=COMPATIBLE `CL`=CLI `LD`=LOCAL_DATA `UA`=UI_AUTOMATION `RO`=READ_ONLY `EX`=EXPERIMENTAL `US`=UNSUPPORTED `?`=UNKNOWN

| # | 能力 | Codex | Qoder | OpenCode | Z Code | WorkBuddy |
|---|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | N | N | C | LD | **N**ᴰ |
| 2 | 软件版本发现 | CL | LD | LD | LD | **N**ᴰ |
| 3 | 列出已有会话 | N | N | N | LD | **N**ᴰ ᶜ |
| 4 | 读取完整会话 | N | RO | N | LD | **N**ᴰ ᶜ |
| 5 | 新消息增量同步 | N | N | N | LD | **N**ᴰ |
| 6 | 双向同步 | N | N | N | LD | **N**ᴰ |
| 7 | 向指定已有会话发消息 | N | N | N | **UA** | **EX**ᴰ ᵈ |
| 8 | 流式响应 | N | **US** | N | ? | **N**ᴰ |
| 9 | 中止当前生成/任务 | N | **US** | N | UA | **N**ᴰ |
| 10 | 读取模型列表 | N | **?** | N | LD | LDᴰ ᵉ |
| 11 | 切换模型 | N | **US** | N | UA | CL |
| 12 | 推理强度/参数控制 | N | RO | C | LD | CL |
| 13 | 精确额度读取 | N | **US** | RO | RO | **N**ᴰ |
| 14 | Reset 时间 | N | **US** | ? | **US** | ? |
| 15 | 任务实时状态 | N | N | N | LD | **N**ᴰ |
| 16 | 审批读取 | N | N | N | RO | **N**ᴰ ᶠ |
| 17 | 审批响应 | N | **UA** | N | UA | CL |
| 18 | 附件 | N | RO | N | LD | RO |
| 19 | MCP/Skill/Rule/Hook | N | N | N | LD | **N**ᴰ |
| 20 | 本地会话数据库 | LD | LD | LD | LD | LDᴰ |
| 21 | CLI | CL | **?** | **?** | **US** | **CL**ᴰ |
| 22 | SDK/API/App Server | N(EX) | EX | N | **US** | **N**ᴰ |
| 23 | 精确跳转原客户端会话 | CL | N | N | ? | CL |
| 24 | UI Automation 可行性 | UA | UA | UA | UA | UA |
| 25 | 版本变化风险 | EX | C | EX | EX | EXᴰ |

**WorkBuddy 列上标**（本轮动态实测所得，其余四家仍为纯静态）：
ᴰ=第 2 轮动态实测确认；ᶜ=**仅 CLI 命名空间**，用户桌面既有会话不可见；ᵈ=`/jobs/{id}/reply` 实测返回 `delivered:true` 但 `saved:false` 且不触发模型 turn，**写链不可靠**；
ᵉ=模型目录来自云端产品配置（实测 52 → 合并本地 3 = 55 个），**REST 无 `/models` 端点**；ᶠ=端点与 schema 实测在，但待审载荷未造出来。

**Qoder 列的 09-27 第 3 轮复测**：#2 版本（`0.3.3→0.4.2`）、#22 router 面（401/404 两发对照 + 路由穷尽）、#25 版本风险（端口重分配实测）
三格已从「静态推断」变为「实测」；**但 #22 等级不上调**（实测结论是负向的：面存在且鉴权可过，却无发现端点、`builtin` 不经此面暴露，
详见 `phase0/qoder.md` 首节），**矩阵分值与名次因此不变**。

## 2. 打分

权重（只反映"离原生 API 有多远"）：`N=4`、`C=3`、`CL=3`、`LD=2`、`RO=2`、`UA=1`、`EX=1`、`US=0`。
**#1~#23 计入能力分**；#24/#25 是属性不是能力，单独定性讨论；`?`(UNKNOWN) 不计分，改为单列"取证覆盖率"，避免用猜测刷分。

| 排名 | 厂商 | 得分 | 满分 | 达成率 | 取证覆盖率 | 证据强度 | 形态概括 |
|---|---|---|---|---|---|---|---|
| 1 | **Codex** | 87 | 92 | **94.6%** | 23/23 = **100%** | 仅静态 | 全能力原生，唯一无 UNKNOWN 项 |
| 2 | **OpenCode** | 76 | 84 | **90.5%** | 21/23 = 91% | 仅静态 | REST+SSE 双代并存，本地库最完整 |
| 3 | **WorkBuddy** | **74** | 88 | **84.1%** | 22/23 = 96% | **静态 + 动态实测（唯一端到端跑通的一家）** | API 面极宽且已跑通，但 CLI/桌面两会话族物理隔离、写链不稳 |
| 4 | **Qoder** | 48 | 84 | **57.1%** | 21/23 = 91% | 仅静态 | 编排面够用，观测面（流式/额度/模型）全缺 |
| 5 | **Z Code** | 32 | 84 | **38.1%** | 21/23 = 91% | 仅静态 | **无任何对外接口**，只能读库 + UIA |

> WorkBuddy 第 1 轮静态打分为 50/80 = 62.5%；第 2 轮实测后 #1/#2/#3/#4/#5/#6/#9/#13/#15/#16/#19/#22 十二项上调、#7 因实测写链不可靠**下调**，净升至 84.1%。
> **达成率不等于可靠性**：WorkBuddy 的 84.1% 里有 12 项是跑过的，Codex 的 94.6% 全是契约级 —— 见 §7。

## 3. 首批真实 Adapter 优先级建议

### P0 —— 立即开工（**3 家**，WorkBuddy 本轮由 P1 升入）

**① Codex Adapter**（最高确定性）
- 层级：`app-server JSON-RPC (stdio/unix/ws)` → `codex exec --json` / `codex queue --thread` → `rollout JSONL 只读` → 不用 UIA
- 独有优势：**官方提供 `generate-json-schema` 可机器读取协议**。Capability 声明直接对齐该 bundle，升级即 diff，不需人肉维护枚举
- 开工前必须解决：daemon 无常驻（实测 netstat 无监听）→ Adapter 需要自己拉起并持有连接，且必须实现 `ChatgptAuthTokensRefresh` 与 8 类审批请求的 server→client 回调

**② OpenCode Adapter**（次高确定性，与 Codex 互补）
- 层级：`HTTP REST + SSE(/event)` → `opencode.db 只读（同 schema，seq 游标）` → 写链无 fallback（单点）
- 独有优势：**读链双保险**（API 与本地库同 schema）；`/api/session/{id}/wait`、`/experimental/capabilities`、`/tui/select-session` 三个端点对编排有直接价值
- 开工前必须解决：① 服务未运行时无法写（CLI 本机不存在，装它撞红线④）；② 选 `/api/*`(新) 还是根路径(旧) —— **建议 `/api/*` 为主、旧路由为兜底**，因为新版有 `wait`/`interrupt`

**③ WorkBuddy Adapter**（**P1 → P0，范围限定为 CodeBuddy CLI 会话族**）
- 升降依据：卡住它的 P1 前置问题（**#4 会话正文在哪**）已被实测**推翻式解决** —— 正文在本机两处 JSONL，
  且 `GET /api/v1/sessions/{id}/history` 能把 `userInput` 与我自造的标记串 `finalReply` 原样回读；
  #22 也从 EXPERIMENTAL 升为 NATIVE：`--serve` 暴露 **113 个端点 + 公开 OpenAPI 3.1 契约**。
  **→ 原 P1 前置条件已满足，故升 P0。**
- 但**范围必须切开**（实测）：CLI 会话族 `~/.codebuddy/projects/<cwd-slug>/*.jsonl` 与桌面会话族
  `D:\WorkBuddyData\.workbuddy\projects\<cwd-slug>/*.jsonl` + `workbuddy.db` **物理隔离、id 不互通**；
  实测 CLI 会话跑完后桌面 `sessions` 表仍 36 行、匹配 0 行。
  → **Adapter 若驱动 CLI，就看不到用户桌面里已有的 36 个会话。** 想覆盖桌面历史只能做只读 JSONL+DB reader（族 B 第二实例）。
- 层级（实测修订）：
  ```
  跑/写：CLI `codebuddy -p --output-format stream-json --include-partial-messages`（实测端到端：8.8s / num_turns 3 / 32 增量帧 / result.usage 有真值）
     └─ 常驻需求 → `--serve` REST（/api/v1/runs、/jobs、/acp）；⚠ /jobs/{id}/reply 实测 delivered:true 但 saved:false 且不执行 → 不可依赖
  读：  /api/v1/sessions/across-projects → /sessions/{id}/history → /api/v1/stats（实测与 CLI result 数值互证）
     └─ 服务未起 → LOCAL_DATA 直读 projects/<cwd-slug>/*.jsonl（slug 规则实测可离线复算）
  停：  POST /api/v1/jobs/{id}/stop（实测 {stopped:true}）
  桌面族：LOCAL_DATA only（写 → UIA）
  ```
- 开工前必须解决：① **#7 对既有会话续发的可靠路径**（预算耗尽未测 `-r <sessionId>`）；
  ② OpenAPI 契约漂移（实测一例：`POST /api/v1/runs` 文档 body 缺实跑必需的 `{id,type}`）→ **写侧端点必须逐个实跑校验，不能只信 spec**；
  ③ `autoUpdate.enabled=true` 实测确认 → 版本会变，须按 `GET /api/v1/info.version` 做能力门控。
- **禁用项**：`--auth none`（自述允许任意本地进程执行命令/读写文件）、自动开 `bypassPermissions`。

### P2 —— 排到观察面板之后

**④ Qoder Adapter**
- 编排能力（列/读写/等待/fork/cron）**已经是 NATIVE**，本项目本身就在用它，所以"能不能驱动"不是问题；
  问题在**观测**：流式、中止、改模型、精确额度四项全 `UNSUPPORTED`，`usage` 字段实测**全 0**
- **原以为可能翻转结论的入口已探完 —— 结论是负向的（09-27 实测）**：`mcp-router.json{baseUrl, apiKey}` 指向的本地 HTTP 服务确实活着且**我的 key 能过鉴权**（带 key→`404` / 不带 key→`401` 两发对照），但它**只是 MCP 上游代理面**（路由枚举究尽为 `/mcp/{servers,sessions,applications}/…` + `/oauth/callback` + `/mcp/add`），**无列举/发现端点**、本机无可寻上游、`builtin` 工具集不经此面暴露 ⇒ **拉不起 #8/#9/#13**。
- ⇒ **Qoder 的五项观测缺口现在没有已知的非 UIA 出路**（router 这条路已被**实测排除**，不再是“待授权”）。另两条实测新事实：① 版本 `0.3.3 → 0.4.2`、router **端口每次启动重分配**（58129→52393）、pid 必变 ⇒ 不得缓存；② builtin 工具数 **7 → 8**（新增 `skill_manage`）⇒ 能力声明必须按版本门控，工具清单不可硬编码。
- 禁止项：不要用 Qoder 的 computer-use 驱动 Qoder 自己（宿主自反）

### P3 —— 建议本阶段不投入

**⑤ Z Code Adapter**
- 实测 `netstat`：**16 个进程零监听**；无 CLI 实物；`zcode.cjs` 内路由全是远端私有 API。→ 前两级降级链直接不存在
- 可做的只有一个**只读观测 Adapter**：`LOCAL_DATA`（`~/.zcode/cli/db/db.sqlite` 23 表，12,213 条 message / 46,775 条 part，带 `sequence`）
- 需要"向指定会话发消息 / 回应审批 / 切模型"时必须 UIA —— 投入产出比最差，且它自带的 `cua-helper`/`computer-use` 插件生态说明 UIA 是它自己的主路径而非我们的
- 注意其额度只有 entitlement：`coding-plan-cache.json` 实测只有 `{status, reason:"coding_plan_not_entitled"}`，**没有数字**

## 4. 接入层级与 fallback 链总表

| 厂商 | 第 1 层 Native API | 第 2 层 CLI | 第 3 层 Local Data | 第 4 层 UIA | 写链是否有 fallback |
|---|---|---|---|---|---|
| Codex | `app-server` JSON-RPC（schema 可再生成） | `exec --json` / `queue --thread` | `rollout-*.jsonl`（`ordinal` 游标） | 不需要 | **有**（三级） |
| OpenCode | REST + SSE（`/api/*` 优先） | 本机无实物 | `opencode.db`（`event.seq` 游标） | 不建议 | **无**（只有 HTTP 能写） |
| WorkBuddy | **REST 113 端点 + OpenAPI + SSE/ACP**（[实测] `/api/v1/{sessions,jobs,runs,stats,plugins,pty,acp,fs}`） | `codebuddy -p --output-format stream-json`（[实测] 端到端） | `.codebuddy/projects/<slug>/*.jsonl`（CLI 族）· `.workbuddy/projects/`+`workbuddy.db`+`audit-log`（桌面族，只读） | 仅剩审批应答/桌面跳转 | [实测] 读链三级全通；**写链 `jobs/reply` 实测不可靠**，改用 CLI 子进程 |
| Qoder | builtin MCP 7 工具（turn 粒度） | dispatcher 在、实物不在 | `projects/<slug>/<id>.jsonl` | 仅中止/审批/改模型 | 有（但只读） |
| Z Code | **不存在** | **不存在** | `db.sqlite` 只读（唯一事实源） | **唯一写入口** | **无** |

## 5. 仍需 UI Automation 才能做到的能力清单

| 能力 | 需要 UIA 的厂商 | 有没有替代 |
|---|---|---|
| 中止当前生成/任务 | Qoder、Z Code | Qoder 可先用 `wait` 观测 `interrupted`（看得见停不了）；无真正替代 |
| 切换模型（对已有会话） | Qoder、Z Code | Z Code 可用 `session_entry=runtime/model_selection` **事后观测**；Qoder 无替代 |
| 审批响应（回应挂起的审批） | Qoder、Z Code、WorkBuddy(桌面端) | WorkBuddy CLI 有 `--permission-mode`/`/jobs` 的 `permissionMode` 字段（启动时）；Qoder 的 `needs_attention` 明确设计为"人必须在环"，**不建议 UIA 代点** |
| 流式响应 | Qoder（真流式无接口）、Z Code（无观测点） | Qoder 可用 jsonl 轮询拿近似增量；Z Code 无 |
| 推理强度设置 | Qoder | — |
| 向指定已有会话发消息 | Z Code | 带外：其微信机器人 / `web-remote-control`（经厂商云端，非本地） |
| 精确跳转（deep link 到会话） | 五家**全部未实测** | 只有 OpenCode 有 `/tui/select-session` 这一非 UIA 的程序化跳转证据 |

**只有 2 项是"全五家都要打洞"的**：#23 deep-link 会话粒度（五家都只证明协议注册存在，未证明能带 session id）、
以及"待审批载荷的实时可读"（Codex/OpenCode 有原生通道，其余三家靠边）。

## 6. ⚠ 会推翻 Adapter 协议设计的事实（需主控上报用户）

**6.1 「每家都能被程序化驱动」这一隐含假设不成立。**
Z Code 实测**无 API、无 CLI、无本地监听端口**，"Native API → CLI → Local Data → UI Automation"四级链对它**只剩最后两级**。
→ 协议必须允许 Adapter 声明"**完全没有写入口**"，并把 UIA 建模为一等实现而不是临时兜底。

**6.2 「各家都能提供流式与额度」不成立，而且本项目宿主自己不成立。**
Qoder 的 `#8 流式 / #9 中止 / #11 改模型 / #13 额度 / #14 Reset` **五项全 UNSUPPORTED**，
`message.usage` 虽有完整字段槽位但**实测全为 0**。
→ ① 事件模型必须允许"**只有 turn 粒度**"的供应商，`Event Journal` 不能假设 token 级 delta 一定存在；
② 额度必须允许 UNKNOWN 一等状态。**把 Qoder 的 0 当真实用量上报 = §12 P0 事故（把 UNKNOWN 显示成确定值）**。

**6.3（本轮已改写）WorkBuddy 的会话正文**在**本机**，但**分成两个互不可见的族** —— 原"正文可能不在本机"的担忧被实测推翻。
- 实测落点：CLI 族 `~/.codebuddy/projects/<cwd-slug>/<sessionId>.jsonl`；桌面族 `D:\WorkBuddyData\.workbuddy\projects\<cwd-slug>\<sessionId>.jsonl`（11 个 slug 目录）。
  同一会话还在 `jobs/<shortId>/state.json`、`logs/<date>/<projName>__<hash>.log`、`traces/<pid>/trace_*.json` 三处留痕，
  **其中服务日志原样写 prompt 正文** → 对 §12「Secret 不得进普通日志」直接构成风险，采集面必须排除这三处或强制脱敏。
- **真正的新事实**：`workbuddy.db.sessions` 36 行与 `~/.codebuddy/projects/` 是**两套会话身份空间**；
  实测 CLI 会话跑完后桌面表零新增行（仍 36）。→ **协议不能假设"一个 vendor 只有一个会话命名空间"**；
  `external_session_id` 必须带 `namespace/scope` 维度，否则 CLI 会话与桌面会话会撞 id 空间。
- 附带实测坑：`codebuddy ps --json` 空集时返回**纯文本 `No active sessions.`**（不是 JSON）；`GET /api/v1/jobs/{id}` 与 `GET /api/v1/jobs` 返回形状不一致。
- **方法论教训（写给所有 worker）**：本角色第 1 轮用 `find … -name "*.jsonl" | head -10` 得出"桌面树内只有 audit-log 有 jsonl"，
  实为 **SIGPIPE 提前杀死 find 造成的假阴性**（`projects/` 子树从未被遍历）。
  → 任何"某文件不存在"的结论必须由**完整遍历 + 无管道截断**的查询支撑，否则只能记 UNKNOWN。这条同样适用于 test 角色的断言。

**6.4 正面事实（降低设计复杂度）：五家实际只有 3 个本地数据族。**
- 族 A（**OpenCode 形制**）：OpenCode + **Z Code** —— `session` 表 **20/20 列同名**，`part.data.type` 取值同集合，`session_input.delivery` 同语义
- 族 B（**Claude-Code 形制 JSONL**）：WorkBuddy/CodeBuddy + **Qoder** —— 但 [实测修正] **两者并不同构**：
  WorkBuddy 是**扁平** `{id,timestamp,type,role,content,providerData,__codebuddyLocal,sessionId,cwd}`，
  Qoder 是**嵌套** `{type,uuid,parentUuid,message:{role,…},toolUseResult,attachment}`。
  → 族 B 需拆成 **B1/B2 两个 reader**（共用的只是"按 cwd-slug 分目录 + jsonl 追加"这层壳），实际族数 **3 → 4**。
- 族 C（**Codex 形制**）：`rollout-*.jsonl` + `ordinal` + `session_meta/turn_context/response_item`
→ **LOCAL_DATA 层只需实现 4 个 reader，不是 5 个**（A 族一家覆盖 OpenCode+ZCode 是最大的一笔节省）。
→ 且 `session_input.delivery ∈ {startNow, guide, queue}`（ZCode 实测有 691+197+11 条真实样本）
   与 Codex 的 `codex queue --thread` / OpenCode 的 `prompt_async` / Qoder 的 "steering or queueing policy" 是**同一个概念的四家独立表达**
   → Adapter 协议应把"**立即插话 / 引导 / 排队**"三态定为一等原语，而不是简单一个"发消息"。
   ⚠ [实测反例] WorkBuddy 的 `POST /jobs/{id}/reply` 声称 `delivered:true` 却 `saved:false` 且不执行 ——
   三态原语**必须配一次读回校验**，不能相信投递返回值。

**6.5 协议契约可以机器读取 —— 现在有 2 家（第 2 轮从 1 家扩到 2 家）。**
- Codex：`codex app-server generate-json-schema --out <dir> --experimental` exit=0，产出 v1 + **v2 共 379 个 schema**。
- WorkBuddy：[实测] `GET /api/openapi.json` **免鉴权返回 OpenAPI 3.1.0 / 113 paths / 44 schemas**，`info.version` 与 CLI 版本一致。
→ `specs/adapter-protocol` 的 Capability 枚举建议对齐这两份机器可读契约，并把"升级后重跑生成 + diff"写成 Adapter 的版本契约测试。
→ ⚠ **[实测] 但契约不等于现实**：`POST /api/v1/runs` 的 OpenAPI body 只声明 `{text, sender}`，实跑返回
`400 {"code":"BAD_REQUEST","message":"Invalid generic message format: missing required fields (id, type)"}`。
→ 版本契约测试**必须包含写侧端点的实跑**，光 diff schema 抓不到这类漂移。

**6.6 一条安全红线（跨家共性）。**
WorkBuddy `--auth none` 的 help 自述：允许任意本地进程通过该服务**执行命令、读写文件**；
Codex 有 `--dangerously-bypass-approvals-and-sandbox`；ZCode/WorkBuddy 均有 `bypassPermissions`。
→ Core 侧必须把"绕过审批"类开关列为**不可由编排自动启用**，且 `debug.*` capability gating 要覆盖它（与 `adapter` 角色既有约束一致）。

## 7. 本矩阵的边界声明（**第 2 轮已改写**）

- **证据强度分两档，不要混看**：
  - **Codex / Qoder / OpenCode / Z Code —— 纯静态**。CLI `--help`、协议 schema 生成、二进制路由扫描、SQLite schema + 枚举、注册表、netstat。
    **这四家的任何能力都没有被端到端跑通过**，定级的是"可达层级"，不是"已验证行为"。
  - **WorkBuddy —— 静态 + 动态实测（唯一跑通的一家）**。已端到端验证：起 `--serve`、鉴权三层、列会话、读正文回读、
    token 级流式（32 帧）、额度数值（CLI `result.usage` 与 REST `stats.modelUsage` 互证）、停作业（`{stopped:true}`）、
    复用登录（`authenticated:true` 且无登录界面）。
- **真实消息 / 请求预算（截至 09-27）**：
  - WorkBuddy：**真实消息 3 / 3 用尽**（1 次 jobs 派发 + 1 次 jobs reply + 1 次 `cli -p`；另有 2 次 400 未触达模型不计）。
  - Qoder：**只读 HTTP 请求 2 / 3**（09-27 授权 B5②；两发均为无副作用的 `initialize` 鉴权/路由对照，**未发任何消息、未消耗额度、未调任何工具**；第 3 发主动保留）。
  - Codex / OpenCode / Z Code：**真实消息 0 / 3 全部剩余**，"发消息→流式→读回"这条主环仍属契约级推断。
- **实测暴露出一个新的失败模式，值得所有角色记取**：
  API 返回成功 ≠ 操作生效。`POST /jobs/{id}/reply` 的 `{delivered:true, saved:false}` 就是活例。
  → Core 的 Execution/Event 语义**不得把 vendor 的"已接受"响应直接当"已生效"**，必须有一次读回确认。
- **动态探针的环境教训**：本次探针目录建在 git 仓库 `D:\Project\Lucero` 之内，实测 WorkBuddy 的
  `GitInfoCollector` 读到了本项目的仓库上下文（只读、零改动）。下次动态探针应放在任何 git 仓库之外。
- 两条待授权深挖的**收口状态**：~~Qoder mcp-router REST 面~~ → **09-27 已探完，负向结论**（拉不起 #8/#9/#13，见 Qoder §P2）；
  ~~WorkBuddy 会话正文~~ → **09-19 第 2 轮已解决**。⇒ **Phase 0 内已无「等授权才能定级」的悬空格子**；
  剩余的 WorkBuddy 5 项 UNKNOWN 全部只因**消息预算用尽未探（≠ 探不到）**，需**新的**授权（建议 ≥2 条）才可能推进。
- **"负向收口"的两种成因必须分开读，不要合并**：
  - **Qoder #22 = 被约束推出**：要真正用上那个 router，得先往 Qoder 注册一个上游（`/mcp/add`）= 改第三方软件配置 = 禁区。
    R1 那个 `404` 只证明"此刻无已注册上游"，**不证明"永远不可用"** ⇒ 授权范围若变，这一格可重开。
  - **WorkBuddy 5 项 = 被预算卡住**：都有可试的具体命令（`-r <sessionId>`、`--model`、`--effort`），只差新授权 ⇒ 同样**不是"探不到"**。
  - 与之相对，**Z Code #21/#22 = 真·本机无实物**（无 CLI 可执行体、无监听端口），这一格才是"探不到"，翻案需装软件（红线④）。
- 过程物与残留：本轮在 `~/.codebuddy/` 留下了探针文件（未删，删除即写第三方数据目录），
  自起的进程与端口已全部终止 —— 明细见 `phase0/workbuddy.md` 末节《残留与清理》。

# Phase 0 汇总：Adapter Capability Matrix

- 编制：`survey` 角色 ｜ 日期：2026-09-19 ｜ 状态：**静态取证完成，五家全部定级**
- 本文只做**等级、打分、排序、层级建议**。逐项证据与 UNKNOWN 原因只在单家报告里：
  `codex.md` / `opencode.md` / `workbuddy.md` / `zcode.md` / `qoder.md`（同目录）
- 取证方式：**纯静态**（CLI `--help`、协议 schema 生成、SQLite schema + 枚举实测、二进制路由扫描、注册表、netstat）。
  **真实消息预算 0 / 15（每家 3 条）未使用** —— 无需发消息即可定级。

## 0. 被调查版本号（一行一家）

| 厂商 | 版本（实测） | 实物 |
|---|---|---|
| Codex | CLI **0.154.0** ／ Desktop MSIX **26.915.3509.0** | `~\AppData\Local\Programs\OpenAI\Codex\bin\codex.exe` |
| Qoder | Desktop **0.3.3** ／ 引擎 **1.1.53**(`entrypoint=cli`) | `D:\Qoder\.qoder-versions\0.3.3\Qoder.exe` |
| OpenCode | Desktop **1.18.14**（**CLI 本机未安装**） | `%LOCALAPPDATA%\Programs\@opencode-aidesktop\` |
| Z Code | Desktop **3.12.3.7463** ／ 引擎 **0.16.x** | `D:\Program Files\ZCode\ZCode.exe`（**无 CLI**） |
| WorkBuddy | Desktop **5.5.6** ／ 内置 CodeBuddy CLI **2.137.1** ／ sandbox **5.5.5** | `D:\Program Files\WorkBuddy\` |

> 五家有 **2~3 条独立版本线**。Core 的 `software_version` 字段必须是**多值结构**，单个字符串放不下。

## 1. 能力矩阵（25 项 × 5 家，只填等级）

图例：`N`=NATIVE `C`=COMPATIBLE `CL`=CLI `LD`=LOCAL_DATA `UA`=UI_AUTOMATION `RO`=READ_ONLY `EX`=EXPERIMENTAL `US`=UNSUPPORTED `?`=UNKNOWN

| # | 能力 | Codex | Qoder | OpenCode | Z Code | WorkBuddy |
|---|---|---|---|---|---|---|
| 1 | 复用原客户端登录 | N | N | C | LD | ? |
| 2 | 软件版本发现 | CL | LD | LD | LD | CL |
| 3 | 列出已有会话 | N | N | N | LD | CL |
| 4 | 读取完整会话 | N | RO | N | LD | **?** |
| 5 | 新消息增量同步 | N | N | N | LD | LD |
| 6 | 双向同步 | N | N | N | LD | EX |
| 7 | 向指定已有会话发消息 | N | N | N | **UA** | CL |
| 8 | 流式响应 | N | **US** | N | ? | N |
| 9 | 中止当前生成/任务 | N | **US** | N | UA | CL |
| 10 | 读取模型列表 | N | **?** | N | LD | LD |
| 11 | 切换模型 | N | **US** | N | UA | CL |
| 12 | 推理强度/参数控制 | N | RO | C | LD | CL |
| 13 | 精确额度读取 | N | **US** | RO | RO | RO |
| 14 | Reset 时间 | N | **US** | ? | **US** | ? |
| 15 | 任务实时状态 | N | N | N | LD | LD |
| 16 | 审批读取 | N | N | N | RO | LD |
| 17 | 审批响应 | N | **UA** | N | UA | CL |
| 18 | 附件 | N | RO | N | LD | RO |
| 19 | MCP/Skill/Rule/Hook | N | N | N | LD | CL |
| 20 | 本地会话数据库 | LD | LD | LD | LD | LD |
| 21 | CLI | CL | **?** | **?** | **US** | CL |
| 22 | SDK/API/App Server | N(EX) | EX | N | **US** | EX |
| 23 | 精确跳转原客户端会话 | CL | N | N | ? | CL |
| 24 | UI Automation 可行性 | UA | UA | UA | UA | UA |
| 25 | 版本变化风险 | EX | C | EX | EX | EX |

## 2. 打分

权重（只反映"离原生 API 有多远"）：`N=4`、`C=3`、`CL=3`、`LD=2`、`RO=2`、`UA=1`、`EX=1`、`US=0`。
**#1~#23 计入能力分**；#24/#25 是属性不是能力，单独定性讨论；`?`(UNKNOWN) 不计分，改为单列"取证覆盖率"，避免用猜测刷分。

| 排名 | 厂商 | 得分 | 满分 | 达成率 | 取证覆盖率 | 形态概括 |
|---|---|---|---|---|---|---|
| 1 | **Codex** | 87 | 92 | **94.6%** | 23/23 = **100%** | 全能力原生，唯一无 UNKNOWN 项 |
| 2 | **OpenCode** | 76 | 84 | **90.5%** | 21/23 = 91% | REST+SSE 双代并存，本地库最完整 |
| 3 | **WorkBuddy** | 50 | 80 | **62.5%** | 20/23 = 87% | CLI/ACP 强，但桌面会话正文缺位 |
| 4 | **Qoder** | 48 | 84 | **57.1%** | 21/23 = 91% | 编排面够用，观测面（流式/额度/模型）全缺 |
| 5 | **Z Code** | 32 | 84 | **38.1%** | 21/23 = 91% | **无任何对外接口**，只能读库 + UIA |

## 3. 首批真实 Adapter 优先级建议

### P0 —— 立即开工（2 家）

**① Codex Adapter**（最高确定性）
- 层级：`app-server JSON-RPC (stdio/unix/ws)` → `codex exec --json` / `codex queue --thread` → `rollout JSONL 只读` → 不用 UIA
- 独有优势：**官方提供 `generate-json-schema` 可机器读取协议**。Capability 声明直接对齐该 bundle，升级即 diff，不需人肉维护枚举
- 开工前必须解决：daemon 无常驻（实测 netstat 无监听）→ Adapter 需要自己拉起并持有连接，且必须实现 `ChatgptAuthTokensRefresh` 与 8 类审批请求的 server→client 回调

**② OpenCode Adapter**（次高确定性，与 Codex 互补）
- 层级：`HTTP REST + SSE(/event)` → `opencode.db 只读（同 schema，seq 游标）` → 写链无 fallback（单点）
- 独有优势：**读链双保险**（API 与本地库同 schema）；`/api/session/{id}/wait`、`/experimental/capabilities`、`/tui/select-session` 三个端点对编排有直接价值
- 开工前必须解决：① 服务未运行时无法写（CLI 本机不存在，装它撞红线④）；② 选 `/api/*`(新) 还是根路径(旧) —— **建议 `/api/*` 为主、旧路由为兜底**，因为新版有 `wait`/`interrupt`

### P1 —— 待一次授权探测后再排（1 家）

**③ WorkBuddy Adapter**
- 卡点：**#4 读取完整会话 = UNKNOWN**（系统性排除后正文未在本机定位；存在 `product.cloudhosted.json` 与 `edge-sync-mapping-v4.db` → 可能在云端）。
  对一个"聚合各家会话"的产品，这是主功能级缺口，**不能在未解决前承诺 Adapter 范围**
- 建议：主控申请**一次**隔离授权（在 `.scratch` 内、由 Adapter 自己 `codebuddy --serve --auth password` 起服务并只访问自建会话），
  用来判定"CLI/ACP 链能不能承担全部读写"。若能 → WorkBuddy 升到 P0 且成为 ACP 协议落地点；若不能 → 桌面端只做只读元数据 Adapter
- 层级（预判）：`ACP (ndJson stdio/streamable-http)` → `CLI stream-json 子进程` → `workbuddy.db + audit-log 只读` → UIA
- 已实测的坑（务必写进 Adapter）：`codebuddy ps --json` 在**无活动会话时输出纯文本 `No active sessions.` 而非合法 JSON**

### P2 —— 排到观察面板之后

**④ Qoder Adapter**
- 编排能力（列/读写/等待/fork/cron）**已经是 NATIVE**，本项目本身就在用它，所以"能不能驱动"不是问题；
  问题在**观测**：流式、中止、改模型、精确额度四项全 `UNSUPPORTED`，`usage` 字段实测**全 0**
- **唯一可能翻转结论的入口**：`mcp-router.json{baseUrl, apiKey}` 指向一个实测在 `LISTENING 127.0.0.1:58129` 的本地 HTTP 服务。
  本角色**未探测**（需使用用户凭据）。建议主控决定是否授权一次只读探测 —— 这决定 Qoder 是"永远残缺"还是"可补齐"
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
| WorkBuddy | `--serve` REST / **ACP** | `codebuddy` 子进程 | `workbuddy.db` + `audit-log`（`sequence`+`prevHash`） | 仅审批/改模型 | **无**（正文位置未知） |
| Qoder | builtin MCP 7 工具（turn 粒度） | dispatcher 在、实物不在 | `projects/<slug>/<id>.jsonl` | 仅中止/审批/改模型 | 有（但只读） |
| Z Code | **不存在** | **不存在** | `db.sqlite` 只读（唯一事实源） | **唯一写入口** | **无** |

## 5. 仍需 UI Automation 才能做到的能力清单

| 能力 | 需要 UIA 的厂商 | 有没有替代 |
|---|---|---|
| 中止当前生成/任务 | Qoder、Z Code | Qoder 可先用 `wait` 观测 `interrupted`（看得见停不了）；无真正替代 |
| 切换模型（对已有会话） | Qoder、Z Code | Z Code 可用 `session_entry=runtime/model_selection` **事后观测**；Qoder 无替代 |
| 审批响应（回应挂起的审批） | Qoder、Z Code、WorkBuddy(桌面端) | WorkBuddy 有 `--permission-mode`(启动时)；Qoder 的 `needs_attention` 明确设计为"人必须在环"，**不建议 UIA 代点** |
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

**6.3 WorkBuddy 桌面端会话正文未在本机定位。**
`--json` 之外还实测到一个反例：`codebuddy ps --json` 空集时返回纯文本，**不是合法 JSON**。
→ "会话正文一定在本地"不能假设；且所有 CLI JSON 解析必须先处理文本哨兵。

**6.4 正面事实（降低设计复杂度）：五家实际只有 3 个本地数据族。**
- 族 A（**OpenCode 形制**）：OpenCode + **Z Code** —— `session` 表 **20/20 列同名**，`part.data.type` 取值同集合，`session_input.delivery` 同语义
- 族 B（**Claude-Code 形制**）：WorkBuddy/CodeBuddy + **Qoder** —— `uuid/parentUuid` 树、`toolUseResult`、`permissionMode`、`usage`
- 族 C（**Codex 形制**）：`rollout-*.jsonl` + `ordinal` + `session_meta/turn_context/response_item`
→ **LOCAL_DATA 层只需实现 3 个 reader，不是 5 个**。跨家 Adapter 工作量比交接包预估的低。
→ 且 `session_input.delivery ∈ {startNow, guide, queue}`（ZCode 实测有 691+197+11 条真实样本）
   与 Codex 的 `codex queue --thread` / OpenCode 的 `prompt_async` / Qoder 的 "steering or queueing policy" 是**同一个概念的四家独立表达**
   → Adapter 协议应把"**立即插话 / 引导 / 排队**"三态定为一等原语，而不是简单一个"发消息"。

**6.5 协议契约可以机器读取（Codex 独有）。**
`codex app-server generate-json-schema --out <dir> --experimental` exit=0，产出 v1 + **v2 共 379 个 schema**。
→ `specs/adapter-protocol` 的 Capability 枚举建议对齐该 bundle，并把"升级后重跑生成 + diff"写成 Adapter 的版本契约测试。

**6.6 一条安全红线（跨家共性）。**
WorkBuddy `--auth none` 的 help 自述：允许任意本地进程通过该服务**执行命令、读写文件**；
Codex 有 `--dangerously-bypass-approvals-and-sandbox`；ZCode/WorkBuddy 均有 `bypassPermissions`。
→ Core 侧必须把"绕过审批"类开关列为**不可由编排自动启用**，且 `debug.*` capability gating 要覆盖它（与 `adapter` 角色既有约束一致）。

## 7. 本矩阵的边界声明

- **全部结论为静态证据级**。凡"未实测/仅方法级证据"在各家报告的 UNKNOWN 清单里逐条列了原因，本文不重复。
- 未使用任何真实消息预算，因此**没有任何一项能力被端到端跑通过**。上表定的是"可达层级"，不是"已验证行为"。
- P1/P2 的排序依赖两次尚未授权的探测（WorkBuddy 正文、Qoder mcp-router）。这两次探测结论可能改变第 3 节的分层。

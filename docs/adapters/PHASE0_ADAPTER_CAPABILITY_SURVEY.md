# Phase 0：Adapter Capability Survey

对 Codex、Qoder、OpenCode、Z Code、WorkBuddy 分别调查，完成后再决定真实 Adapter 开发顺序。

| 能力 | Codex | Qoder | OpenCode | Z Code | WorkBuddy |
|---|---|---|---|---|---|
| 复用原客户端登录 | TBD | TBD | TBD | TBD | TBD |
| 软件版本发现 | TBD | TBD | TBD | TBD | TBD |
| 列出已有会话 | TBD | TBD | TBD | TBD | TBD |
| 读取完整会话 | TBD | TBD | TBD | TBD | TBD |
| 新消息增量同步 | TBD | TBD | TBD | TBD | TBD |
| 双向同步 | TBD | TBD | TBD | TBD | TBD |
| 向指定已有会话发消息 | TBD | TBD | TBD | TBD | TBD |
| 流式响应 | TBD | TBD | TBD | TBD | TBD |
| 中止当前生成/任务 | TBD | TBD | TBD | TBD | TBD |
| 读取模型列表 | TBD | TBD | TBD | TBD | TBD |
| 切换模型 | TBD | TBD | TBD | TBD | TBD |
| 推理强度/参数控制 | TBD | TBD | TBD | TBD | TBD |
| 精确额度读取 | TBD | TBD | TBD | TBD | TBD |
| Reset 时间 | TBD | TBD | TBD | TBD | TBD |
| 任务实时状态 | TBD | TBD | TBD | TBD | TBD |
| 审批读取 | TBD | TBD | TBD | TBD | TBD |
| 审批响应 | TBD | TBD | TBD | TBD | TBD |
| 附件 | TBD | TBD | TBD | TBD | TBD |
| MCP/Skill/Rule/Hook | TBD | TBD | TBD | TBD | TBD |
| 本地会话数据库 | TBD | TBD | TBD | TBD | TBD |
| CLI | TBD | TBD | TBD | TBD | TBD |
| SDK/API/App Server | TBD | TBD | TBD | TBD | TBD |
| 精确跳转原客户端会话 | TBD | TBD | TBD | TBD | TBD |
| UI Automation 可行性 | TBD | TBD | TBD | TBD | TBD |
| 版本变化风险 | TBD | TBD | TBD | TBD | TBD |

每一项填写：
- 支持等级：NATIVE / COMPATIBLE / CLI / LOCAL_DATA / UI_AUTOMATION / READ_ONLY / EXPERIMENTAL / UNSUPPORTED
- 证据来源
- 实测结果
- 认证方式
- 限制
- 版本号
- 失败/降级方案

最终输出：
1. Adapter Capability Matrix
2. 首批 2～3 家真实 Adapter 优先级
3. 每家接入层级和 fallback 链
4. 仍需 UI Automation 的能力清单

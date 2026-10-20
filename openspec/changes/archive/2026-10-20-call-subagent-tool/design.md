# Design：call_subagent 双装配源与预置工具

## D1 装配源两形态（tool desc 声明）

```yaml
# 形态一：agents 文件引用
call_subagent:
  agents_file: ./agents/reviewer.md     # md + frontmatter，自动装配
# 形态二：显式参数
call_subagent:
  prompt: ./prompts/reviewer.md         # 或内联 prompt 文本
  model: glm-5.3-flash
  skills: [go-service-ops]
  tools: [exec_run, mcp_call]
  mcp: [mcp-lsp]
```

frontmatter 字段对齐 `~/.qoder/agents/*.md` 形态：`name/description/model/prompt/skills/tools/mcp`；未声明字段取父 agent 同名字段（继承语义，显式参数覆盖）。**拒绝矩阵**：两种装配源同时给出、文件不可读、frontmatter 缺 name、字段类型错——全部具名拒（错误零副作用）。

## D2 执行=task 化委派（保留 tagent 价值面）

- 装配产物：用 tRPC agent 构建器构造 `agent.Agent`（复用，不重写 provider/stream）。
- 执行：调用既有 **`agent.Delegate`**（"admits one sub-agent call as an asynchronous task"）——Receipt 受理、TaskID 管理、状态变更通知发送者（S05 语义天然继承）。
- **被否方案**：直接用 `trpc tool/agent.NewTool(agent)` 同步调用——同步面无 task 义务账/通知回调，与特性 3 冲突，不选。

## D3 预置工具三类的配置引用

- `knowledge`：复用 `trpc knowledge/tool`（`NewCodeSearchTool` 等），配置面以 `ToolRef{Name:"knowledge_search", Params:{...}}` 引用；装配时按 ref 解析到 trpc 工具实例。
- `recall`：tagent 已有 memory 检索（04.7/04.9，Run.Recall）——以工具引用暴露（不新造检索）。
- `call_subagent`：本 change 实现。

## D4 plugin/event/event-loop 扩展面（核心能力保真）

- plugin：4 相位窄口（已实现，S04 覆盖）——本 change 不动语义，只在 spec 中把"扩展者需实现什么/与什么有关系"钉死（00.26 已做，纳入本 change 验收引用）。
- event：事件词汇（event.Type/Notice 族）与 **event loop 的消费扩展点**=mailbox 接纳 + Invocation 驱动。历史"任意事件类型注册消费（冥想/knowledge）"**不迁移**，作为扩展点示例写入文档（自定义消费=经宿主循环 + AdmitInbound 面），并补一个最小扩展测试（自定义事件类型经宿主循环被消费并投递结果）。
- event loop：Loop 归约唯一入口不变；扩展测试=自定义事件到达→消费逻辑调用 LLM/tool→结果投递（宿主侧最小形态）。

## D5 tRPC 复用登记

| 能力 | 复用面 | 不重造 |
|---|---|---|
| subagent 执行体 | agent 构建器（trpc llmagent 等） | provider/stream/工具协议 |
| knowledge 检索 | `trpc knowledge/tool` | 检索/分词/向量 |
| agent 工具形态参考 | `trpc tool/agent`（形态参考） | 同步调用语义（tagent 用 task 化） |
| 转交语义 | `trpc tool/transfer` | 仅参考，不引入 |

## 验收（三级）

- 叶级：每叶 verify 三锚（正/负/消费者锚，测试名冻结）。
- 场景级：golden S11（config 驱动 subagent+pipeline 顺序）/S12（call_subagent 双装配源→task→通知）入 `golden/runtime/scenarios.json`（deterministic scope）。
- 变更级：DoD=双装配源正负例全绿＋三类预置工具装配可复用＋event-loop 扩展测试绿＋全门 lint ok。

## 风险与回退

- frontmatter 解析引入 YAML 依赖：复用仓库既有 yaml 解码（config 已用严格解码），不新引库。
- 装配产物依赖 trpc agent 构建器版本：fork replace 现状不动；构建器签名变更由上游 PR #2637 跟踪（登记）。
- 回退：`tool/subagent` 独立包，删除不影响既有面；配置面新增引用为可选项（缺省不装配）。

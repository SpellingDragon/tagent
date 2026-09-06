# 配置迁移指南

> **权威来源**：当前配置格式以 [`config.go`](../config.go) 的结构体定义与
> [README「配置参考」章](../README.md#-配置参考) 为准；本篇只解决**从早期格式迁移**时的字段对照与
> 常见错写。行为契约见 [openspec/specs/](../openspec/specs/)。

## 概述

tagent 的配置格式经历过三代。仓内部分早期文档/示例仍残留第一、二代写法，按它们书写会导致
**YAML 解析成功但字段被静默忽略**（`yaml.Unmarshal` 对未知键不报错），表现为"配了不生效"。
本篇给出三代对照与自查清单。

## 三代格式演进

| 代际 | 形态 | 状态 |
|------|------|------|
| 第一代（扁平） | 顶层直接写 `tagent.name` / `system_prompt_file` / `knowledge_agent.enabled` 等平铺字段 | **已废弃**，字段在当前 `Config` 中不存在 |
| 第二代（`tagent:` 包裹 + `name`/`type`） | `tagent.agents.<x>` 外层再包一个 `tagent:` 根键；工具用 `name` + `type` + `config` | **已废弃**，当前顶层无 `tagent:` 根键，ToolRef 用 `kind`/`agent`/`id`/`properties` |
| 第三代（当前，声明式） | 顶层直接是 `entry` / `model` / `provider` / `providers` / `agents` / `mcp_servers` / `governance` / `evolution` / `reliability` / `working_dir` 等 | **当前格式** |

## 当前格式：最小可用示例

```yaml
# 顶层直接开始，无 `tagent:` 包裹
entry: tagent
prompt_dir: resources/prompts
model: glm-4-flash            # 全局默认模型；agent 省略 model 即回退此值
provider: zhipu               # 全局默认 provider；agent 省略即回退此值
providers:
  zhipu:
    provider: openai          # 协议实现（国产模型多为 openai 兼容）
    api_endpoint: "https://open.bigmodel.cn/api/coding/paas/v4"
    api_key_env: "ZAI_API_KEY"

agents:
  tagent:                     # agent 名 = map 的 key（AgentConfig 无 name 字段）
    system_prompt:
      files: [AGENTS.md, SOUL.md, TOOLS.md]   # CompositeConfig：inline → files → dir
    memory:
      type: localfile         # memory / file / localfile
      path: .wechat-config/data
    tools:
      - kind: agent           # 子 agent 作为工具
        agent: recall
        description_file: recall_tool_desc.md
        event_params: [event_keys]
      - kind: tool            # 内置 plain tool
        id: exec
        description_file: action_tool_desc.md
        properties:
          monitor: { dense_interval: 1s, dense_duration: 10s }

  recall:
    system_prompt:
      files: [recall_agent.md]
    memory:
      type: localfile
      path: .wechat-config/data
      read_namespaces: [tagent]   # 跨 agent 读须显式授权
    max_tool_iterations: 5
```

加载（注意 `LoadConfig` 返回**两个值**）：

```go
cfg, err := tagent.LoadConfig("tagent.yaml")   // 内部：解析 → ApplyDefaults → Validate
if err != nil {
    return err
}
ta, err := tagent.New(cfg, tagent.WithModel(model))
```

## 字段对照表（旧写法 → 当前写法）

| 旧写法（已废弃） | 当前写法 | 说明 |
|------|------|------|
| 顶层 `tagent:` 根键包裹全部配置 | 删除该层，`entry`/`agents` 等直接置顶层 | 第二代残留 |
| `agents.<x>.name: "my-agent"` | 删除——agent 名即 map 的 key | `AgentConfig` 无 `name` 字段 |
| `system_prompt_file: "prompts/system.txt"` | `system_prompt: { files: [system.md] }` | `PromptConfig` = `prompt.CompositeConfig` |
| `system_prompt_text: "..."` | `system_prompt: { inline: "..." }` | 同上 |
| 工具 `- name: "web_search"` + `type: tool` | `- kind: tool` + `id: web_search` | `ToolRef.Kind` / `ToolRef.ID` |
| 工具 `- name: "knowledge"` + `type: agent` | `- kind: agent` + `agent: knowledge` | `ToolRef.AgentID` |
| 工具 `config: { ... }` | `properties: { ... }` | `ToolRef.Properties`（各工厂自行反序列化） |
| 子 agent 的 `config.model` / `config.max_tool_iterations` 写在 ToolRef 内 | 写在被引用 agent 自身的 `agents.<name>` 定义处 | ToolRef 只声明引用关系；运行参数单一配置点 |
| `memory.data_dir` | `memory.path` | 见 `MemoryConfig.Path` |
| `memory.type: file`（无 rustviking 环境） | `memory.type: localfile` | `file` 需 rustviking CLI；`localfile` 零外部依赖 |
| `summary_model` 顶层平铺 | `agents.<x>.compress.summary_model` | 亦可经 `tagent.WithSummaryModel(...)` 注入 |
| `knowledge_agent.enabled` / `recall_agent.enabled` | 由 entry 的 `tools` 是否引用该 agent 决定 | 无独立开关字段 |
| `max_tool_iterations: 200`（第二代示例值） | 默认入口 50 / 子 agent 10，按需显式配置 | 见 `DefaultMaxToolIter` / `DefaultAgentMaxToolIter` |

## 迁移自查清单

- [ ] 顶层**无** `tagent:` 根键；`entry` 指向的 agent 名确实存在于 `agents` 中（`Validate` 会拦截）
- [ ] 每个 `kind: agent` 的工具都有 `description` 或 `description_file`（`Validate` 强制）
- [ ] 同一 agent 的 `tools` 内无重复 `agent`/`id`（`Validate` 强制）
- [ ] 提示词用 `system_prompt.files`（数组），不是 `system_prompt_file`（字符串）
- [ ] 工具专属配置写在 `properties`，不是 `config`
- [ ] 运行参数（`max_tool_iterations`/`max_tokens`/`temperature`）写在 agent 自身定义处，不是 ToolRef 内
- [ ] `memory.type: file` 已确认目标环境有 rustviking CLI，否则改 `localfile`
- [ ] 平台子系统（`governance`/`evolution`/`reliability`）与 `working_dir` 按需显式声明——**默认全关 = 零行为变化**
- [ ] 迁移后跑一次 `LoadConfig` + `Validate`（或直接启动 example）确认无解析/校验错误

> **静默失效提醒**：写错字段名不会报错，只会**被忽略**。迁移后务必用一次真实加载验证生效值
> （例如临时打印 `cfg.Agents[name].Memory.Type`），不要仅凭"启动没报错"判定迁移成功。

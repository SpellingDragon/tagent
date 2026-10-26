# 配置迁移指南

> **本篇是历史篇**：它描述的是 legacy 生产树（已随 `prototype rewrite` 移除）的 `config.go` 三代格式。仓库现在没有根 `config.go`，也没有「配置参考」章；当期声明面是 `api/config` 的 `Declaration`/`AgentSpec`/`ToolRef`，宿主文档由根包 `host.go` 的 `hostDocument` 经 `api/config.DecodeStrict` 严格解码（未知键拒启动），再经 `api/document.Document` 映射为声明。字段对照仍可参考，但**不要拿本篇当当前格式的权威**：当期 schema 以 `examples/wechat-bot/tagent.yaml` 头部注释与 `hostDocument` 为准。

## 概述

tagent 的配置格式经历过三代。仓内部分早期文档/示例仍残留第一、二代写法：写进 legacy 生产树时
会**解析成功但字段被静默忽略**（`yaml.Unmarshal` 对未知键不报错），表现为"配了不生效"；写进当期
宿主文档则被严格解码点名拒启动（见文末）。本篇给出三代对照与自查清单。

## 三代格式演进

| 代际 | 形态 | 状态 |
|------|------|------|
| 第一代（扁平） | 顶层直接写 `tagent.name` / `system_prompt_file` / `knowledge_agent.enabled` 等平铺字段 | **已废弃**，字段在当前码面不存在 |
| 第二代（`tagent:` 包裹 + `name`/`type`） | `tagent.agents.<x>` 外层再包一个 `tagent:` 根键；工具用 `name` + `type` + `config` | **已废弃**，当前宿主文档顶层无 `tagent:` 根键，工具引用用 `kind` + `id`/`agent` |
| 第三代（声明式） | 顶层直接是 `entry` / `model` / `provider` / `providers` / `agents` / `mcp_servers` / `governance` / `app` 等 | **宿主文档沿此形态**：`hostDocument` 读取的键见 `host.go`；未声明的键拒启动 |

## 宿主文档示例（以 `examples/wechat-bot/tagent.yaml` 为准）

```yaml
entry: tagent
prompt_dir: resources/prompts
model: glm-5.3-flash          # 全局默认；agent 省略即回退
provider: zhipu
providers:
  zhipu: { provider: openai, api_endpoint: "https://open.bigmodel.cn/api/coding/paas/v4", api_key_env: "ZAI_API_KEY" }
agents:
  tagent:
    system_prompt: { files: [AGENTS.md, SOUL.md, TOOLS.md] }
    memory: { type: localfile, path: .wechat-config/data }
    tools:
      - { kind: agent, agent: recall, description_file: recall_tool_desc.md, event_params: [event_keys] }
      - { kind: tool, id: exec }
  recall:
    system_prompt: { files: [recall_agent.md] }
    memory: { type: localfile, path: .wechat-config/data, read_namespaces: [tagent] }
```

加载（严格解码 → 映射声明 → 装配）：

```go
host, err := tagent.Boot("tagent.yaml")
if err != nil {
    return err
}
defer host.Close(ctx)
```

## 字段对照表（旧写法 → 当前写法）

| 旧写法（已废弃） | 当前写法 | 说明 |
|------|------|------|
| 顶层 `tagent:` 根键包裹全部配置 | 删除该层，`entry`/`agents` 等直接置顶层 | 第二代残留 |
| `agents.<x>.name: "my-agent"` | 删除——agent 名即 map 的 key | 宿主文档的 agent 名即声明身份 |
| `system_prompt_file: "prompts/system.txt"` | `system_prompt: { files: [system.md] }` | 宿主文档的 `system_prompt.files` |
| `system_prompt_text: "..."` | 无对应键；提示词写文件，由 `system_prompt.files` 引用 | 宿主文档的 `system_prompt` 只收 `files` |
| 工具 `- name: "web_search"` + `type: tool` | `- kind: tool` + `id: web_search` | 宿主文档映射到 `config.ToolRef.Name` |
| 工具 `- name: "knowledge"` + `type: agent` | `- kind: agent` + `agent: knowledge` | 映射到 `config.ToolRef.Name` + `Params` |
| 工具 `config: { ... }` | 无 `properties` 键；声明只给引用（`kind`/`id`/`agent`），实例由宿主贡献 | 未声明的键拒启动 |
| 子 agent 的 `config.model` / `config.max_tool_iterations` 写在 ToolRef 内 | 写在被引用 agent 自身的 `agents.<name>` 定义处 | ToolRef 只声明引用关系；运行参数单一配置点 |
| `memory.data_dir` | `memory.path` | 见 `MemorySpec.Path` |
| `memory.type: file`（无 rustviking 环境） | `memory.type: localfile` | `file` 需 rustviking CLI；`localfile` 零外部依赖 |
| `summary_model` 顶层平铺 | `agents.<x>.meditation` / 模型声明处 | 由声明面选模型，无顶层 summary 注入点 |
| `knowledge_agent.enabled` / `recall_agent.enabled` | 由 entry 的 `tools` 是否引用该 agent 决定 | 无独立开关字段 |
| `max_tool_iterations: 200`（第二代示例值） | 不在宿主 schema；轮次上限写 `final_round.max_rounds` | 未知键拒启动 |

## 迁移自查清单

- [ ] 顶层**无** `tagent:` 根键；`entry` 指向的 agent 名确实存在于 `agents` 中（映射会拦截）
- [ ] `kind: agent` 的工具如需专用描述就写 `description_file`（宿主文档没有 `description` 键；缺省时用被引用 agent 的声明目的）
- [ ] 同一 agent 的 `tools` 内无重复 `agent`/`id`（同名工具重复声明会引入歧义）
- [ ] 提示词用 `system_prompt.files`（数组），不是 `system_prompt_file`（字符串）
- [ ] 工具引用只写 `kind` + `id`/`agent`（可加 `description_file`/`event_params`），没有承载工具专属配置的键
- [ ] 运行参数（`max_rounds`/`max_tokens`/`temperature`）写在 agent 自身定义处，不是工具引用内
- [ ] `memory.type: file` 已确认目标环境有 rustviking CLI，否则改 `localfile`
- [ ] 平台子系统由 `governance`/`app` 段声明（schema 见 `hostDocument`）；其他顶层键拒启动
- [ ] 迁移后跑一次 `tagent.Boot`（严格解码 + 映射 + 装配）确认无解析/映射错误

> **严格解码提醒**：未知字段名会让加载**点名拒启动**（`unknown field`），不再被静默忽略。迁移必须以一次真实 `tagent.Boot` 验证。

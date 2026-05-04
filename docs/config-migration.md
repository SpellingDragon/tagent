# 配置迁移指南

## 概述

本指南介绍从旧版 YAML 配置格式迁移到新版格式的过程。新版配置使用 `AgentConfig` 结构体，支持多 Agent 声明式配置。

## 新旧格式对比

### 旧格式（不再支持）

```yaml
# 旧的扁平化配置，已废弃
tagent:
  name: "my-agent"
  system_prompt_file: "prompts/system.txt"
  summary_model: "gpt-4"
  knowledge_agent:
    enabled: true
    model: "gpt-4"
  recall_agent:
    enabled: true
    model: "gpt-4"
```

### 新格式

```yaml
# 新版声明式配置
tagent:
  agents:
    main:
      system_prompt_file: "prompts/system.txt"
      max_tool_iterations: 200
      max_tokens: 8000
      compress_threshold: 0.8
      tools:
        - name: "web_search"
          type: tool
        - name: "command"
          type: tool
        - name: "knowledge"
          type: agent
          config:
            model: "gpt-4"
            max_tool_iterations: 5
        - name: "recall"
          type: agent
          config:
            model: "gpt-4"
            max_tool_iterations: 5
```

### 格式差异要点

| 方面 | 旧格式 | 新格式 |
|------|--------|--------|
| Agent 定义 | 隐式（单个 agent） | 显式 `agents` map（支持多 agent） |
| 工具声明 | 独立配置字段 | 统一 `tools` 数组，`type` 区分 `tool`/`agent` |
| 子 Agent | `knowledge_agent`/`recall_agent` 独立字段 | tools 数组中 `type: agent` |
| 模型 | `summary_model` 等平铺字段 | 通过 `Option`（`WithSummaryModel`）注入 |

## 迁移步骤

### 步骤 1：创建 agents 外层结构

将旧的顶层配置移至 `tagent.agents.<name>` 下：

```yaml
tagent:
  agents:
    main:
      system_prompt_file: "prompts/system.txt"
      max_tool_iterations: 200
      max_tokens: 8000
      compress_threshold: 0.8
```

### 步骤 2：迁移工具声明

将工具从独立配置字段移至 `tools` 数组：

**旧格式：**
```yaml
tagent:
  knowledge_agent:
    enabled: true
    model: "gpt-4"
  recall_agent:
    enabled: true
    model: "gpt-4"
```

**新格式：**
```yaml
tagent:
  agents:
    main:
      tools:
        - name: "recall"
          type: agent
          config:
            model: "gpt-4"
            max_tool_iterations: 5
```

### 步骤 3：运行时注入

全局配置（模型实例、摘要模型等）通过 Go 代码中的 `Option` 注入：

```go
cfg := LoadConfig("config.yaml")
ta, err := New(cfg,
    WithModel(mainModel),
    WithSummaryModel(summaryModel),
)
```

## AgentConfig 字段说明

### 顶层 Config

| 字段 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `agents` | `map[string]AgentConfig` | 空 | Agent 声明，key 为 agent 名称 |

### AgentConfig

| 字段 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `name` | `string` | `"tagent"` | Agent 标识名 |
| `system_prompt_file` | `string` | `""` | System prompt 文件路径 |
| `system_prompt_text` | `string` | `""` | 直接使用的 system prompt 文本 |
| `max_tool_iterations` | `int` | `200` | 最大工具调用轮次 |
| `max_tokens` | `int` | `8000` | 最大 token 预算 |
| `compress_threshold` | `float64` | `0.8` | 压缩触发阈值（百分比） |
| `memory` | `MemoryConfig` | — | 内存存储配置 |
| `tools` | `[]ToolRef` | — | 可用工具列表 |

### ToolRef

| 字段 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `name` | `string` | 必填 | 工具标识名 |
| `type` | `string` | `"tool"` | 工具类型：`"tool"`（普通工具）或 `"agent"`（子 agent） |
| `config` | `map[string]any` | `nil` | 工具特定配置 |

### MemoryConfig

| 字段 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `type` | `string` | `"memory"` | 存储类型：`"memory"`（内存）、`"file"`（文件） |
| `data_dir` | `string` | `""` | 文件存储的数据目录 |

## Context

tagent 的 RL 轨迹记录系统（TrajectoryStore）在 `agent/trajectory.go` 中实现，记录 trpc-agent-go 事件通道中的每一个事件。调查发现 63 个交互中 46 个（73%）是空 streaming 事件（无 content、无 reasoning、无 tokens、无 completion_id），这些事件无 RL 训练价值。

AReaL 框架已有 `InteractionCache`（`areal/experimental/openai/cache.py`），在 completion 级别记录完整交互数据（含 logprobs、token_ids），在流式 generator 创建之前就写入 cache。tagent 的 TrajectoryStore 与 AReaL 的 InteractionCache 是两套独立的记录系统，存在数据冗余。

同时，工具执行结果（如 command/tmux 输出）直接进入上下文，无大小限制，导致 token 爆炸。trpc-agent-go 的 `executeSingleToolCall`（`graph/state_graph.go:4458`）在 `json.Marshal(result)` 后直接创建 `model.NewToolMessage`，无任何大小检查。

## Goals / Non-Goals

**Goals:**
- 在 tagent.go 中为所有工具添加输出大小拦截，超限时截断并返回错误，让 agent 感知
- 移除 tagent 的 TrajectoryStore/RewardFunc 代码，RL 数据完全依托 AReaL
- 更新 example 和脚本，确保运行前环境正确初始化

**Non-Goals:**
- 不修改 trpc-agent-go 的代码（所有改动局限于 tagent 工程）
- 不添加 context alert 注入机制（本 change 不涉及 BeforeModel 修改）
- 不修改 AReaL 的 InteractionCache 逻辑
- 不处理空 streaming 事件的过滤（根因在 trpc-agent-go 默认 Stream: true，移除 TrajectoryStore 后不再记录这些事件）

## Decisions

### D1: 工具输出拦截放在 tagent.go 的 buildToolFromRef 中

**选择**：在 `tagent.go` 的 `buildToolFromRef` 函数中，用拦截包装器包裹所有返回的 `trpctool.Tool`。

**理由**：
- `buildToolFromRef` 是所有工具创建的统一入口（agent-kind 和 tool-kind 都经过这里）
- 拦截逻辑对所有工具统一生效，无需修改各个工具的实现
- 符合 tagent 的"注入而非继承"设计哲学

**替代方案**：
- 在 trpc-agent-go 的 `executeSingleToolCall` 中添加拦截 → 需修改框架代码，违反"改动局限于 tagent"约束
- 在各个工具的 `Call()` 方法中添加拦截 → 需修改多个工具，不符合 DRY 原则

### D2: 拦截阈值 = MaxTokens / 2 对应的字符数

**选择**：拦截阈值 = `agentCfg.MaxTokens / 2`，按 1 token ≈ 4 字符（UTF-8）估算字符上限。

**理由**：
- 工具输出通常占上下文的主要部分，限制为 MaxTokens 的一半确保留有空间给 LLM 推理
- 用户明确要求"和最大 token 量的一半对齐"
- 字符估算而非精确 token 计数避免引入 tokenizer 依赖

**实现**：
```go
// 在 buildAgent 中计算阈值
maxOutputChars := acfg.MaxTokens / 2 * 4 // token → char 估算
// 在 buildToolFromRef 中包装
wrappedTool := NewOutputLimitTool(t, maxOutputChars)
```

### D3: 超限时截断 + 返回错误信息

**选择**：工具输出超限时，截断输出并附加错误提示，作为工具执行结果返回。

**格式**：
```
[原始输出前 N 字符]
...
[ERROR: Tool output exceeded {maxChars} characters, truncated. Total: {actualLen} characters. Consider optimizing your command or using more specific queries.]
```

**理由**：
- 符合用户的"让 agent 感知并自主决策"哲学
- 截断保留部分原始信息，让 agent 有上下文判断
- 错误提示明确告知问题，agent 可选择更精确的查询或不同工具

### D4: 移除 TrajectoryStore，HTTPAPI 保留 /healthz 和 /task

**选择**：
- 删除 `agent/trajectory.go`、`agent/reward.go` 及相关测试
- 从 `tagent_agent.go` 移除 `trajectoryStore`、`rewardFunc` 字段和所有 trajectory 采集逻辑
- 从 `http_api.go` 移除 `GET /trajectories` 和 `GET /trajectory/{id}` 端点
- 保留 `GET /healthz` 和 `POST /task` 端点
- `NewHTTPAPI` 签名移除 `store *TrajectoryStore` 参数

**理由**：
- AReaL 的 InteractionCache 已在 completion 级别完整记录交互数据
- tagent 的 TrajectoryStore 记录事件级别数据（含 73% 空事件），数据冗余且有噪声
- `/healthz` 和 `/task` 不依赖 TrajectoryStore，仍有价值

### D5: tagent_adapter.py 从 AReaL session 获取 completion_ids

**选择**：tagent_adapter.py 不再从 tagent 的 `GET /trajectory/{id}` 获取 completion_ids，改为从 AReaL 自身的 session 数据获取。

**理由**：
- AReaL 的 `export_trajectories` 端点已返回所有 interaction（含 completion_id）
- tagent 移除 TrajectoryStore 后不再提供 trajectory 查询端点
- adapter 仍通过 `POST /task` 提交任务，通过 `GET /healthz` 检查状态

## Risks / Trade-offs

- **[风险] 工具输出截断可能丢失关键信息** → 截断保留前半部分（通常包含最重要的输出开头），并附加明确的错误提示让 agent 决策
- **[风险] 移除 TrajectoryStore 后无法本地查看 RL 轨迹** → RL 数据查询应通过 AReaL 的 `export_trajectories` 端点完成
- **[风险] tagent_adapter.py 变更可能影响 AReaL 训练流程** → adapter 仍保留 `POST /task` 提交和 `GET /healthz` 轮询，仅 completion_ids 来源从 tagent 切换到 AReaL session
- **[折损] 字符估算不精确** → 1 token ≈ 4 字符是粗略估算，中文可能 1 token ≈ 1-2 字符。但这是防御性限制，精确度不是关键需求

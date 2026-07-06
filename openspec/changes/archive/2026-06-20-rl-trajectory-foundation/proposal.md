## Why

我们刚刚完成了 persistent-event-loop 的 OTLP 可观测性增强（per-batch span + 事件内容日志 + token/TTFT/tool call 统计）。但这不是终点——**可观测性是 RL（强化学习）的基础设施**。

### 可观测性 → RL 的逻辑链

```
per-batch OTLP span (已完成)
    ↓ 每个批量 = 一条 RL trajectory
Trajectory 数据层 (本提案)
    ↓ 结构化 prompt/completion/tool_calls/completion_id/reward
AReaL Bridge (本提案)
    ↓ Python adapter 实现 AReaL agent workflow 接口
PPO Trainer (AReaL)
    ↓ logprobs (proxy 捕获) + reward (tagent 计算)
模型权重更新
```

**关键发现**：AReaL 的 OpenAI-compatible proxy 在代理层捕获 logprobs 和 completion_id（`InteractionWithTokenLogpReward`）。tagent 只需：
1. 将 LLM endpoint 指向 AReaL proxy（config 变更，零代码改动）
2. 捕获每条 LLM 响应的 `Response.ID`（= AReaL completion_id）用于 reward 映射
3. 计算 reward 并返回给 AReaL

**当前 gap**：per-batch span 的属性是**截断的**（1000 字符预览），面向人类可观测。RL 需要的是**全保真**的结构化 trajectory 数据，包含完整的 prompt、completion、reasoning、tool call 序列、completion_id 和 reward。

### AReaL 集成模型

AReaL 的 agent workflow 接口：
```python
async def run(self, data: dict, **extra_kwargs) -> float | dict[str, float]
```
- AReaL 注入 `base_url`（proxy URL）、`api_key`、`http_client`
- Agent 通过 proxy 调用 LLM（proxy 捕获 logprobs + completion_id）
- Agent 返回 reward（float 或 `{completion_id: reward}`）

tagent 作为 Go agent，需要一个 Python adapter 桥接 AReaL 的 Python 接口。adapter 负责：启动 tagent → 喂数据 → 收 trajectory → 算 reward → 返回 AReaL。

## What Changes

### Phase 1: Trajectory 数据层（agent 包内）

- **新增 `agent/trajectory.go`**：`Trajectory` 结构体（全保真）+ `TrajectoryCollector`（在 loop 事件转发中同步采集）+ `TrajectoryStore`（内存 + JSONL 文件导出）
- **修改 `agent/tagent_agent.go`**：loop() 中的事件转发循环集成 TrajectoryCollector；`Response.ID` 作为 completion_id 记录到 trajectory

### Phase 2: Reward 接口（agent 包内）

- **新增 `agent/reward.go`**：`RewardFunc` 接口 + 内置实现（TaskCompletionReward、ToolCallEfficiencyReward）+ HTTP callback reward（外部评估器）
- **修改 `agent/tagent_agent.go`**：TagentConfig 新增 `RewardFunc` 字段；batch 完成后调用 reward 函数，reward 写入 trajectory

### Phase 3: AReaL Bridge（areal 包，新建）

- **新增 `areal/tagent_adapter.py`**：实现 AReaL agent workflow 接口的 Python adapter
- **新增 `areal/README.md`**：集成示例和配置说明
- **新增 `agent/http_api.go`**：tagent HTTP API（POST /task 提交任务、GET /trajectory/{id} 获取 trajectory、GET /healthz 健康检查），供 adapter 调用

### 不改动

- trpc-agent-go 框架源码（零修改）
- persistent-event-loop 的 OTLP span 逻辑（保持不变，trajectory 是并行采集）
- RunSimple / InjectMessage 签名（向后兼容）
- 现有测试（新增测试，不修改现有）

## Capabilities

### New Capabilities

- `rl-trajectory-collection`: 全保真 trajectory 采集——每个 batch 生成一条 Trajectory 记录，包含输入消息、LLM 交互序列（prompt/completion/reasoning/tool_calls/tool_results/completion_id/token_usage/TTFT）、最终响应、reward
- `rl-reward-computation`: 可插拔 reward 接口——内置 task completion / tool efficiency reward；支持 HTTP callback 外部评估器
- `areal-bridge`: AReaL 集成桥——Python adapter + tagent HTTP API，让 tagent 作为 AReaL RL 训练的 rollout agent

### Modified Capabilities

- `persistent-event-loop`: loop() 事件转发循环新增 trajectory 采集逻辑（与 OTLP span 并行，不干扰现有可观测性）

## Impact

- **agent/trajectory.go** (新增 ~200 行): Trajectory 结构体 + Collector + Store
- **agent/reward.go** (新增 ~150 行): RewardFunc 接口 + 内置实现 + HTTP callback
- **agent/http_api.go** (新增 ~120 行): HTTP API server (task/trajectory/healthz)
- **agent/tagent_agent.go** (修改 ~40 行): TagentConfig 加 RewardFunc 字段；loop() 集成 collector；StartLoop 初始化 store
- **areal/tagent_adapter.py** (新增 ~150 行): Python adapter
- **areal/README.md** (新增 ~80 行): 集成文档
- **agent/trajectory_test.go** (新增 ~200 行): trajectory 采集测试
- **agent/reward_test.go** (新增 ~100 行): reward 函数测试

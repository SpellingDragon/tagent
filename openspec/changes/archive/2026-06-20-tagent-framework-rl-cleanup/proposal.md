## Why

tagent 的 RL 轨迹记录系统（TrajectoryStore）存在根本性的设计问题：它记录了 trpc-agent-go 事件通道中的每一个事件，包括大量无信息的空 streaming chunk（占 73% 的交互数据），导致数据膨胀且无 RL 训练价值。同时，工具执行结果缺乏输出大小拦截，超长工具输出（如 tmux/command 输出）直接进入上下文，导致 token 爆炸。

AReaL 框架已有自己的交互记录系统（InteractionCache），在 completion 级别记录完整交互数据（含 logprobs、token_ids、reward），无需 tagent 重复记录。tagent 应移除自身的轨迹记录代码，依托 AReaL 完成 RL 数据采集。

## What Changes

- **新增工具输出拦截**：在 `tagent.go` 中为所有工具添加输出大小拦截包装器，当工具执行结果超过 `MaxTokens / 2` 对应的字符数时，截断并返回错误信息，让 agent 感知并自主决策
- **BREAKING** 移除 `agent/trajectory.go` 中的 `TrajectoryStore` 及相关数据结构（`Trajectory`、`Interaction`、`InputMessage` 等）
- **BREAKING** 移除 `agent/reward.go` 中的 `RewardFunc` 接口及 `TaskCompletionReward` 实现
- **BREAKING** 移除 `tagent.go` 中的 `WithTrajectoryStore` 和 `WithRewardFunc` Option
- **BREAKING** 移除 `agent/tagent_agent.go` 中的 trajectory 采集逻辑（`collector`、`storeTrajectory`、reward 计算）
- **BREAKING** 移除 `agent/http_api.go` 中的 trajectory 查询端点（`GET /trajectories`、`GET /trajectory/{id}`）
- 更新 `examples/wechat-bot/main.go`：移除 TrajectoryStore 初始化和 RewardFunc 配置
- 更新 `examples/wechat-bot/run.sh`：移除 `TAGENT_TRAJECTORY_FILE` 环境变量和 `--trajectory` 选项
- 更新 `areal/tagent_adapter.py`：适配无 TrajectoryStore 的 tagent，从 AReaL 自身 session 数据获取 completion_ids
- 移除相关测试文件（`trajectory_test.go`、`reward_test.go`、`http_api_test.go` 中的 trajectory 测试）

## Capabilities

### New Capabilities
- `tool-output-interception`: 工具执行结果输出大小拦截，超限时截断并返回错误，让 agent 感知上下文压力

### Modified Capabilities
- `persistent-event-loop`: 移除 trajectory 采集和 reward 计算逻辑，loop 仅负责事件转发和日志记录
- `example-rl-visibility`: 移除 tagent 侧的 RL 轨迹记录，RL 数据完全依托 AReaL 的 InteractionCache

## Impact

- **agent 包**：删除 `trajectory.go`、`reward.go`，修改 `tagent_agent.go`（移除 trajectory 字段和采集逻辑）、`http_api.go`（移除 trajectory 端点）
- **tagent.go**：移除 `WithTrajectoryStore`/`WithRewardFunc`，新增工具输出拦截包装逻辑
- **examples/wechat-bot**：`main.go` 移除 TrajectoryStore/RewardFunc，`run.sh` 移除 trajectory 环境变量
- **areal/tagent_adapter.py**：适配新的无 TrajectoryStore 架构
- **测试**：删除 `trajectory_test.go`、`reward_test.go`，更新 `http_api_test.go`、`tagent_agent_loop_test.go`
- **API 兼容性**：`GET /trajectories` 和 `GET /trajectory/{id}` 端点被移除（BREAKING）
- **依赖**：无新增外部依赖

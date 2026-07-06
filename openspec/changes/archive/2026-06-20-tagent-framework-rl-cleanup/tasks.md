## 1. 工具输出拦截包装器

- [x] 1.1 创建 `agent/output_limit_tool.go`：实现 `OutputLimitTool` 结构体，包装 `trpctool.Tool`，实现 `Definition()` 透传和 `Call()` 拦截逻辑
- [x] 1.2 在 `Call()` 中实现拦截逻辑：json.Marshal 返回值 → 检查字符数 → 超限时截断并附加 `[ERROR: Tool output exceeded N characters, truncated. Total: M characters. ...]` 错误信息
- [x] 1.3 处理 nil 返回值（不拦截）和 Marshal 失败（返回原始结果）的边界情况
- [x] 1.4 ~~在 `tagent.go` 的 `buildAgent` 中计算 `maxOutputChars`~~ 改为在 `NewTagentAgent` 中自动计算 `MaxTokens/2 * 4`，覆盖所有路径
- [x] 1.5 ~~在 `tagent.go` 的 `buildToolFromRef` 返回前用 `OutputLimitTool` 包装所有工具~~ 改为在 `NewTagentAgent` 中统一包装，覆盖 factory 路径
- [x] 1.6 编写 `output_limit_tool_test.go` 单元测试：覆盖未超限、超限截断、nil 返回、agent-kind 工具等场景

## 2. 移除 TrajectoryStore 和 RewardFunc

- [x] 2.1 删除 `agent/trajectory.go`（TrajectoryStore、Trajectory、Interaction、InputMessage 等全部数据结构和方法）
- [x] 2.2 删除 `agent/trajectory_test.go`
- [x] 2.3 删除 `agent/reward.go`（RewardFunc 接口、TaskCompletionReward 实现）
- [x] 2.4 删除 `agent/reward_test.go`
- [x] 2.5 从 `agent/tagent_agent.go` 移除 `trajectoryStore` 和 `rewardFunc` 字段
- [x] 2.6 从 `agent/tagent_agent.go` 的 `TagentConfig` 移除 `TrajectoryStore` 和 `RewardFunc` 字段
- [x] 2.7 从 `agent/tagent_agent.go` 的 `NewTagentAgent` 移除 trajectoryStore 和 rewardFunc 赋值
- [x] 2.8 从 `agent/tagent_agent.go` 的 `loop()` 移除 collector 相关逻辑（`NewTrajectoryCollector`、`RecordEvent`、`Finalize`、reward 计算、`storeTrajectory` 调用）
- [x] 2.9 从 `agent/tagent_agent.go` 移除 `storeTrajectory` 方法
- [x] 2.10 从 `tagent.go` 移除 `WithTrajectoryStore` 和 `WithRewardFunc` Option 函数
- [x] 2.11 从 `tagent.go` 的 `runtimeConfig` 移除 `trajectoryStore` 和 `rewardFunc` 字段
- [x] 2.12 从 `tagent.go` 的 `buildAgent` 移除 TrajectoryStore 和 RewardFunc 赋值到 agentCfg 的逻辑

## 3. 更新 HTTPAPI

- [x] 3.1 从 `agent/http_api.go` 移除 `GET /trajectories` 和 `GET /trajectory/{id}` 端点
- [x] 3.2 修改 `NewHTTPAPI` 签名：移除 `store *TrajectoryStore` 参数
- [x] 3.3 保留 `GET /healthz` 和 `POST /task` 端点不变
- [x] 3.4 更新 `agent/http_api_test.go`：移除 trajectory 相关测试，移除 `createTestAgentWithStore` 中的 TrajectoryStore/RewardFunc，更新 `NewHTTPAPI` 调用签名

## 4. 更新 tagent_agent_loop_test.go

- [x] 4.1 移除 loop 测试中所有 TrajectoryStore 和 RewardFunc 相关的断言和初始化（无引用，无需修改）
- [x] 4.2 确保 loop 测试仍验证事件转发、日志记录等核心逻辑（无变更，保持不变）

## 5. 更新 wechat-bot 示例

- [x] 5.1 从 `examples/wechat-bot/main.go` 移除 TrajectoryStore 初始化（trajPath、NewTrajectoryStore）
- [x] 5.2 从 `examples/wechat-bot/main.go` 的 opts 列表移除 `WithTrajectoryStore` 和 `WithRewardFunc`
- [x] 5.3 更新 `examples/wechat-bot/main.go` 的 HTTPAPI 启动：`NewHTTPAPI(ta)` 移除 trajStore 参数
- [x] 5.4 从 `examples/wechat-bot/run.sh` 移除 `TAGENT_TRAJECTORY_FILE` 环境变量默认值设置
- [x] 5.5 从 `examples/wechat-bot/run.sh` 的帮助文本和 `--trajectory` 选项移除
- [x] 5.6 从 `examples/wechat-bot/.gitignore` 移除 `trajectories.jsonl` 条目（如存在）

## 6. 更新 AReaL adapter

- [x] 6.1 更新 `areal/tagent_adapter.py`：移除 trajectory 轮询逻辑，简化为 POST /task + 等待 + 返回 reward
- [x] 6.2 更新 `areal/tagent_adapter.py` 的 `run()` 方法：返回 episode-level float reward，completion_ids 由 AReaL cache 管理
- [x] 6.3 更新 `areal/README.md`：移除 TrajectoryStore 相关说明，更新架构图和数据流

## 7. 更新文档

- [x] 7.1 更新 `docs/wiki/agent/agent-architecture.md`：移除 TrajectoryStore 和 RewardFunc 相关章节，更新 AReaL 集成架构图和端到端流程
- [x] 7.2 更新 `README.md`：无 RL trajectory 相关功能说明（无需修改）

## 8. 编译验证和清理

- [x] 8.1 运行 `go build ./...` 确认编译通过
- [x] 8.2 运行 `go test ./agent/...` 确认测试通过
- [x] 8.3 运行 `go vet ./...` 确认无警告
- [x] 8.4 检查是否有遗漏的 import 或引用（零残留）

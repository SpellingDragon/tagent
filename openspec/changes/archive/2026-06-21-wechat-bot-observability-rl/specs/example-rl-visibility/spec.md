## ADDED Requirements

### Requirement: HTTPAPI 服务启动
wechat-bot 示例 SHALL 在启动时同时启动 `agent.HTTPAPI` HTTP 服务，暴露 trajectory 查询端点，端口通过 `TAGENT_HTTP_PORT` 环境变量配置，默认 8089。

#### Scenario: 默认端口启动
- **WHEN** 用户启动 wechat-bot 且未设置 `TAGENT_HTTP_PORT`
- **THEN** HTTPAPI 在端口 8089 上监听

#### Scenario: 自定义端口启动
- **WHEN** 用户设置 `TAGENT_HTTP_PORT=9090` 并启动 wechat-bot
- **THEN** HTTPAPI 在端口 9090 上监听

#### Scenario: HTTPAPI 与 wechat bot 并行运行
- **WHEN** wechat-bot 启动后
- **THEN** HTTPAPI 和 wechat bot 同时运行，互不阻塞

### Requirement: Trajectory 列表查询
HTTPAPI SHALL 提供 `GET /trajectories` 端点，返回所有已记录的 RL 轨迹摘要列表（按 batch_index 排序）。

#### Scenario: 查询轨迹列表
- **WHEN** 用户发送 `GET http://localhost:8089/trajectories`
- **THEN** 返回 JSON 数组，每个元素包含 id、batch_index、status、has_final、tool_call_count、input_tokens、output_tokens、duration_seconds、reward、has_reward 字段

#### Scenario: 无轨迹时查询
- **WHEN** agent 尚未处理任何消息时查询 `/trajectories`
- **THEN** 返回空 JSON 数组 `[]`

### Requirement: 单条 Trajectory 详情查询
HTTPAPI SHALL 提供 `GET /trajectory/{id}` 端点，返回指定 ID 的完整轨迹数据（包含 input_messages、interactions、final_response 等全保真字段）。

#### Scenario: 查询存在的轨迹
- **WHEN** 用户发送 `GET http://localhost:8089/trajectory/{已知ID}`
- **THEN** 返回完整 Trajectory JSON，包含 input_messages、interactions（含 content、reasoning、tool_calls）、completion_ids、reward 字段

#### Scenario: 查询不存在的轨迹
- **WHEN** 用户发送 `GET http://localhost:8089/trajectory/nonexistent-id`
- **THEN** 返回 HTTP 404，body 包含 error 字段

### Requirement: 健康检查端点
HTTPAPI SHALL 提供 `GET /healthz` 端点，返回 agent 运行状态。

#### Scenario: Loop 活跃时健康检查
- **WHEN** agent 的 persistent event loop 正在运行时查询 `/healthz`
- **THEN** 返回 HTTP 200，JSON 包含 `loop_active: true`

### Requirement: Trajectory JSONL 自动导出
wechat-bot SHALL 默认启用 TrajectoryStore 的 JSONL 导出，导出路径通过 `TAGENT_TRAJECTORY_FILE` 环境变量配置，默认为 `.wechat-config/trajectories.jsonl`。

#### Scenario: 默认导出路径
- **WHEN** 用户启动 wechat-bot 且未设置 `TAGENT_TRAJECTORY_FILE`
- **THEN** 每个 batch 完成后，轨迹数据追加到 `.wechat-config/trajectories.jsonl`

#### Scenario: 自定义导出路径
- **WHEN** 用户设置 `TAGENT_TRAJECTORY_FILE=/tmp/traj.jsonl` 并启动
- **THEN** 轨迹数据追加到 `/tmp/traj.jsonl`

### Requirement: run.sh 可观测环境默认值
run.sh SHALL 为可观测和 RL 环境变量设置默认值，使用户无需手动配置即可获得本地可观测能力。

#### Scenario: 默认启动包含可观测配置
- **WHEN** 用户执行 `./run.sh` 不带任何额外参数
- **THEN** `TAGENT_TRAJECTORY_FILE` 默认为 `.wechat-config/trajectories.jsonl`，`TAGENT_HTTP_PORT` 默认为 `8089`

#### Scenario: 环境变量覆盖默认值
- **WHEN** 用户执行 `TAGENT_HTTP_PORT=9090 ./run.sh`
- **THEN** HTTPAPI 在端口 9090 上监听，覆盖默认值

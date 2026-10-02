# example-rl-visibility Specification

## Purpose
Provides HTTP API visibility and task submission for RL integration. tagent no longer stores trajectories — all RL data recording is handled by AReaL's InteractionCache at the proxy level.

## Requirements

### Requirement: HTTPAPI 服务启动

wechat-bot 示例 SHALL 在启动时同时启动 `agent.HTTPAPI` HTTP 服务，暴露健康检查和任务提交端点，端口通过 `TAGENT_HTTP_PORT` 环境变量配置，默认 8089。HTTPAPI SHALL NOT 提供 trajectory 查询端点。

#### Scenario: 默认端口启动

- **WHEN** 用户启动 wechat-bot 且未设置 `TAGENT_HTTP_PORT`
- **THEN** HTTPAPI 在端口 8089 上监听

#### Scenario: 自定义端口启动

- **WHEN** 用户设置 `TAGENT_HTTP_PORT=9090` 并启动 wechat-bot
- **THEN** HTTPAPI 在端口 9090 上监听

#### Scenario: HTTPAPI 与 wechat bot 并行运行

- **WHEN** wechat-bot 启动后
- **THEN** HTTPAPI 和 wechat bot 同时运行，互不阻塞

### Requirement: 健康检查端点

HTTPAPI SHALL 提供 `GET /healthz` 端点，返回 agent 运行状态。

#### Scenario: Loop 活跃时健康检查

- **WHEN** agent 的 persistent event loop 正在运行时查询 `/healthz`
- **THEN** 返回 HTTP 200，JSON 包含 `loop_active: true`

### Requirement: run.sh 可观测环境默认值

run.sh SHALL 为可观测环境变量设置默认值。run.sh SHALL NOT 设置 `TAGENT_TRAJECTORY_FILE` 环境变量。

#### Scenario: 默认启动包含可观测配置

- **WHEN** 用户执行 `./run.sh` 不带任何额外参数
- **THEN** `TAGENT_HTTP_PORT` 默认为 `8089`
- **AND** `TAGENT_TRAJECTORY_FILE` 不被设置

#### Scenario: 环境变量覆盖默认值

- **WHEN** 用户执行 `TAGENT_HTTP_PORT=9090 ./run.sh`
- **THEN** HTTPAPI 在端口 9090 上监听，覆盖默认值

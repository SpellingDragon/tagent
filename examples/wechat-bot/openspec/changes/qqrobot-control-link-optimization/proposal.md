# qqrobot-control-link-optimization

## Why

QQchannelRobot 的控制链路当前完全依赖 QQ 身份（频道消息 + 白名单），agent 无法在不冒充 QQ 用户的前提下向机器人下发业务指令；同时 robot→agent 方向的事件（掉线、告警）没有自动上报通道，需人工看日志。本计划打通双向链路：agent→robot 走本地 HTTP 指令通道（最高优先），robot→agent 走 tagent /task 喂任务，并同步收紧 QQ 侧白名单。

## What Changes

- **A（最高优先）本地指令通道**：扩展 `QQchannelRobot/ops_server.go`（127.0.0.1:9601，已有 /healthz /logs /safe）：新增 `POST /cmd`（body `{command, args[]}`，经 `service.GetCommand` 分发到既有指令注册表，合成 `CommandArgs{Author: "local-admin", From: config 默认消息频道}`）与 `GET /cmd`（列出可用指令清单）。本地操作员视为可信（绕过 QQ 白名单），与 C 组白名单收紧互为配套。httptest 覆盖。
- **B 事件联动（robot→agent）**：确认 tagent 框架 HTTP API（8089）`/task` 端点的路径与 payload 契约后，改造 `QQchannelRobot/resources/check.shell`（cron 每 30 分钟）：进程不在→拉起后 POST 告知 agent；healthz 异常/storm 失败/cookie 过龄→POST 告警；正常不打扰。顺带修 cron.log 里 `robot.log..gz` 双点文件名 bug。
- **C 白名单收紧**：`QQchannelRobot/robot.go` 或 `engine.go:670` 的 `IsInWhiteList` 硬编码 `return true`（DB 查询被注释）改为 config 驱动 `white_list` 列表：配置了→严格校验；未配置→兼容 allow-all 但启动时醒目 Warn。单测覆盖两态。
- **D 上线与验证**：go build + 全部测试；重启 qqrobot（分离会话方式，秒级盲窗，守护兜底）激活 A/C；实弹验证 /cmd（列指令 + 触发一条无害指令）、B 的干跑、C 两态行为；QQchannelRobot git 提交；skill 事实文档 `qqchannel-robot-facts.md` 同步。

## Impact

- 改动文件：`QQchannelRobot/ops_server.go`、`QQchannelRobot/service/commands.go`（如需导出注册表访问）、`QQchannelRobot/robot.go`/`engine.go`（白名单）、`QQchannelRobot/resources/check.shell`、`QQchannelRobot/config.yaml`（white_list 项）、`tagent/examples/wechat-bot/skills/go-service-ops/qqchannel-robot-facts.md`
- 新增测试文件：ops_server 与白名单的 `_test.go`
- 服务影响：需一次秒级重启（守护脚本兜底）；`cookie.json` 不碰
- 行为变更：QQ 侧白名单从隐式 allow-all 变为显式配置驱动（未配置时保持兼容但告警）

## Purpose

让机器人以"没事不吵"为原则，通过外部 agent 的 HTTP 喂任务端点自动上报自身运行事件（守护拉起、健康异常、凭据过龄），替代人工看日志。

## ADDED Requirements

### Requirement: 守护事件上报
check.shell（cron 驱动）SHALL 在以下情形向 agent 的 `/task` 端点 POST 上报：机器人进程不在被守护拉起后、healthz 异常、重连风暴失败、cookie 过龄。其余情形 MUST 保持静默（不打扰 agent）。

#### Scenario: 进程不在被拉起后上报
- **WHEN** check.shell 发现进程不在，守护拉起后
- **THEN** 脚本向 agent `/task` POST 告知拉起事件，包含时间与新旧 pid（若可得）

#### Scenario: 健康异常告警
- **WHEN** healthz 返回非 200 或异常 JSON
- **THEN** 脚本向 agent `/task` POST 告警，包含 healthz 原文与时间

#### Scenario: 日志轮转文件名
- **WHEN** check.shell 执行日志轮转
- **THEN** 生成的归档文件名不含连续双点（`..gz`），既存残留文件被改名修正

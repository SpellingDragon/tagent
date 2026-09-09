## Purpose

定义 tagent WeChat Bot 自重启（自杀→转世）与事件驱动唤醒闭环的可观察行为契约：独立遗嘱执行人如何在 agent 停机盲窗内接管二进制替换与服务拉起、8089 端点维持 loopback 无鉴权现状（用户知情接受）、转世后 agent 如何销假汇报、以及 QQchannelRobot 如何在真实事件（守护拉起动作/风暴级异常）发生时经 /task 通知 tagent（有事才叫）。

## ADDED Requirements

### Requirement: 遗嘱执行人独立接管重启

系统 SHALL 提供一个独立于 agent 进程树的重启执行人脚本（restart-tagent.sh），在独立 tmux 会话中运行。触发重启时，执行人 MUST 等待缓冲期让 agent 事件落盘，随后向 agent 进程发 SIGTERM；若超时未退出 MUST 升级 SIGKILL。

#### Scenario: 回合边界触发自杀重启

- **WHEN** agent 在回合边界决定重启并启动独立 tmux 会话中的 restart-tagent.sh
- **THEN** 脚本 sleep 缓冲期等待事件落盘，随后向 agent 进程发送 SIGTERM，全程时间线写入 restart.log

#### Scenario: 优雅停机超时升级

- **WHEN** agent 进程收到 SIGTERM 后在超时窗口内未退出（main.go signal.NotifyContext 优雅停机失败）
- **THEN** 脚本升级为 SIGKILL 强制终止并记录升级原因到 restart.log

#### Scenario: 二进制原子替换

- **WHEN** agent 进程已停止且新二进制已在 /tmp 构建完成
- **THEN** 脚本以 mv 原子替换部署二进制，随后启动新进程

#### Scenario: 启动后探活

- **WHEN** 新进程已启动
- **THEN** 脚本以 curl 循环探测 GET /healthz 直至就绪或超时，结果（含耗时）写入 restart.log

### Requirement: 8089 维持 loopback 无鉴权现状

系统 SHALL 维持 /task 与 /healthz 的无鉴权现状（loopback 场景，用户知情接受）。本次变更 MUST NOT 引入 TAGENT_API_TOKEN、middleware 或任何鉴权改动到 main.go。

#### Scenario: 无鉴权访问保持现状

- **WHEN** 任意时刻 POST /task（loopback）或 GET /healthz
- **THEN** 请求按现状直接受理，无 401 拦截，行为与变更前一致

### Requirement: 转世后销假

agent 转世（重启后）SHALL 读取 restart.log 核对重启时间线，验证 healthz 正常，并将销假汇报送达用户（微信渠道）。

#### Scenario: 转世后销假汇报

- **WHEN** agent 重启完成后收到下一轮任务注入或唤醒
- **THEN** agent 读 restart.log 验证重启时间线完整（停机、替换、探活各阶段证据），实测 healthz，将销假汇报经微信送达用户

### Requirement: QQchannelRobot 事件驱动通知（有事才叫）

QQchannelRobot 的守护脚本（check.shell）SHALL 仅在真实事件发生时经 /task 向 tagent 投递通知：本轮守护真的执行了拉起动作，或守护日志中检出风暴级异常。无事时 MUST 保持静默（不做机械式定时心跳）；唤醒来源只保留设计内来源（task_settled + 外部输入事件）。

#### Scenario: 拉起动作触发通知

- **WHEN** check.shell 守护段本轮真的执行了拉起动作（restart/restartall 等）
- **THEN** 末尾事件检测段经 POST /task 投递带 `[event-notify]` 前缀的通知消息，投递失败仅记日志不阻塞守护

#### Scenario: 风暴级异常触发通知

- **WHEN** 守护日志中检出风暴级异常（如短时间内大量 ERROR / 连续重启 / OOM 等模式）
- **THEN** 事件检测段经 POST /task 投递异常通知，tagent 收到后核实 cron.log 并处置

#### Scenario: 无事保持静默

- **WHEN** check.shell 每小时 cron 正常执行，无拉起动作、无风暴级异常
- **THEN** 不投递任何消息到 /task，tagent 对话流无噪音

#### Scenario: tagent 停机期间通知落空

- **WHEN** 事件检测段执行时 tagent 处于停机盲窗
- **THEN** 投递失败被记录，tagent 转世后由首轮用户消息或后续事件通知唤醒，销假不依赖该通知

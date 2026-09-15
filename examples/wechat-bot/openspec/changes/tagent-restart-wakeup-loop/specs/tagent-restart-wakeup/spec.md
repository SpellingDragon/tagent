## Purpose

定义 tagent WeChat Bot 自重启（自杀→转世）与外部唤醒闭环的可观察行为契约：独立遗嘱执行人如何在 agent 停机盲窗内接管二进制替换与服务拉起、/task 端点如何按 token 鉴权、转世后 agent 如何销假汇报、以及每小时守护心跳如何把 QQchannelRobot 与 tagent 两套系统闭合成互检环。

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

### Requirement: /task 端点 token 鉴权

/task 端点 SHALL 支持 token 鉴权：环境变量 TAGENT_API_TOKEN 非空时，请求 MUST 携带匹配 token 方可被受理；否则返回 401。TAGENT_API_TOKEN 未设置时，系统 MUST 保持现状不鉴权（向后兼容）。/healthz MUST 保持无鉴权（供重启脚本与守护心跳探针使用）。

#### Scenario: 未设置 token 环境变量

- **WHEN** TAGENT_API_TOKEN 未设置且请求 POST /task
- **THEN** 请求照常受理（与现状一致，不因升级而破坏）

#### Scenario: 无鉴权请求被拦截

- **WHEN** TAGENT_API_TOKEN 已设置且 POST /task 请求未携带 token
- **THEN** 返回 401，请求不进入 InjectMessage

#### Scenario: 携带正确 token

- **WHEN** TAGENT_API_TOKEN 已设置且 POST /task 请求携带匹配 token
- **THEN** 请求受理，任务注入事件循环

#### Scenario: healthz 始终可达

- **WHEN** 任意状态下 GET /healthz
- **THEN** 无论 TAGENT_API_TOKEN 是否设置，均不要求鉴权且正常响应

### Requirement: 转世后销假

agent 转世（重启后）SHALL 读取 restart.log 核对重启时间线，验证 token 拦截与 healthz，并将销假汇报送达用户（微信渠道）。

#### Scenario: 转世后销假汇报

- **WHEN** agent 重启完成后收到下一轮任务注入或唤醒
- **THEN** agent 读 restart.log 验证重启时间线完整（停机、替换、探活各阶段证据），实测 token 拦截与 healthz，将销假汇报经微信送达用户

### Requirement: 每小时守护心跳闭环

QQchannelRobot 的每小时守护脚本（check.shell）SHALL 在每轮守护完成后经 /task 向 tagent 投递带状态的心跳消息。心跳 MUST 携带守护结果（qqrobot 存活、日志轮转等状态摘要）；tagent 侧 MUST 能在收到心跳后被唤醒，消灭"宣称继续工作却无唤醒机制"的空窗。

#### Scenario: 每小时心跳投递

- **WHEN** check.shell 每小时 cron 执行完成守护段
- **THEN** 末尾追加的心跳段经 POST /task（携带 token）投递心跳消息，投递失败仅记日志不阻塞守护

#### Scenario: tagent 停机期间心跳落空

- **WHEN** check.shell 心跳段执行时 tagent 处于停机盲窗
- **THEN** 投递失败被记录，下一小时心跳重试；tagent 转世后首个心跳唤醒其销假

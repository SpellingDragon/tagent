## Purpose

QQchannelRobot 生产进程的自愈守护能力：cron 周期检测机器人进程存活状态，异常退出后自动拉起，并保证日志轮转、并发保护与可观测的执行留痕。

## ADDED Requirements

### Requirement: 进程存活判定与自动拉起

check.shell 每次被触发时 SHALL 按二进制命令行 `bin/qqrobot` 判定机器人进程是否存活（`pgrep -f 'bin/qqrobot'`）；进程在运行时 MUST 跳过拉起动作；进程不在运行时 MUST 执行拉起：启动 `QQchannelRobot/bin/qqrobot` 编译产物（不再使用 `go run`），标准输出与错误重定向到 robot.log，后台运行。

#### Scenario: 进程在运行时跳过拉起

- **WHEN** check.shell 被触发且 `pgrep -f 'bin/qqrobot'` 匹配到存活进程
- **THEN** 脚本输出跳过拉起的提示信息，不执行任何启动动作，继续执行后续日志轮转与清理段

#### Scenario: 进程不在运行时自动拉起

- **WHEN** check.shell 被触发且 `pgrep -f 'bin/qqrobot'` 无匹配
- **THEN** 先将 robot.log 最后 200 行转存到 `logs/log.error.$DATE`，再以 `bin/qqrobot` 后台拉起进程，输出重定向到 robot.log，并向 cron.log 记录拉起时间

### Requirement: 单实例并发保护

同一时刻 SHALL 只允许一个 check.shell 实例执行守护逻辑，防止 cron 重叠触发导致重复拉起进程或日志轮转竞争。

#### Scenario: cron 并发触发时仅一个实例生效

- **WHEN** 上一轮 check.shell 尚未结束时 cron 再次触发同一脚本
- **THEN** 后到实例通过 flock 检测到锁被持有，直接退出且不执行任何守护/轮转动作，退出不产生错误状态

### Requirement: 日志轮转与过期清理

check.shell SHALL 保持现有日志轮转行为：robot.log 非空时备份压缩到 logs/ 并 truncate 原文件；logs/ 下 robot.log.* 与 log.error.* 超过 7 天的文件 MUST 被清理。

#### Scenario: 轮转与清理路径正常

- **WHEN** check.shell 执行且 robot.log 非空
- **THEN** robot.log 被复制到 logs/robot.log.$DATE 并 gzip，原文件 truncate 至 0，超 7 天的 robot.log.* 与 log.error.* 被删除

### Requirement: 拉起后健康双层验证

自动拉起执行后 SHALL 通过两层验证确认服务恢复：观测端点 healthz 与 qqops 查询均返回正常。

#### Scenario: 停服自愈后双层验证通过

- **WHEN** 机器人进程被停止（模拟停服），随后 check.shell 被触发完成自动拉起
- **THEN** healthz 端点返回健康状态且 qqops 查询返回正常，确认服务恢复

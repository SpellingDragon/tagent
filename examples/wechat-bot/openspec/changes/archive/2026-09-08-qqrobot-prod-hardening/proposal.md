## Why

前置工作（cookie 续期、safe_mode 关闭、观测端点 a433be1）已完成，但 `resources/check.shell` 的自动拉起守护段仍被注释停用且原判定 `pgrep -f 'robot.go'` 存在永不匹配二进制进程的 bug，机器人进程挂掉后无法自愈；同时缺乏可复现的隔离构建脚本与历史测试残留文件需要清理。本次生产化变更让守护真正生效、构建可固化、仓库与运维知识保持一致。

## What Changes

- 修复 `resources/check.shell` 自动拉起守护段：判定改为按二进制名 `pgrep -f 'bin/qqrobot'`（修复原 `pgrep 'robot.go'` 永不匹配的 bug），拉起命令改用 `bin/qqrobot` 编译产物（不再 `go run`），加 `flock` 单实例防 cron 并发，启动前转存最后 200 行到 `logs/log.error.$DATE`，输出写 `cron.log`
- 新建 `resources/build.sh`：固化 go1.18.10 隔离构建（GOROOT/GOPATH/GOCACHE/GOPROXY/GOSUMDB 全显式）供重建用
- 确认 crontab hourly 任务指向修复后的脚本（路径不变，无需改 crontab 本身，仅需确认）
- 清理 kill 测试残留证据文件：`robot.log.pre_safeboot` / `robot.log.safeboot` / `robot.log.bak` / `cookie.json.bak` 归档到 `logs/history/` 或删除；`cookie.json.expired.bak` 保留至确认稳定
- 全部变更本地 git 提交（不 push——远端权限在用户）
- 更新 `tagent/examples/wechat-bot/skills/go-service-ops/qqchannel-robot-facts.md`（守护已启用等事实变化）
- 端到端验证：手动触发一次 check.shell 确认"进程在→跳过拉起"路径 + 轮转清理路径正常；模拟停服→check.shell 自动拉起→healthz/qqops 双层验证

## Impact

- 范围：`QQchannelRobot/resources/check.shell`（行为变更：守护启用）、`QQchannelRobot/resources/build.sh`（新增）、`QQchannelRobot/logs/`（历史归档）、QQchannelRobot 仓库 git 历史、go-service-ops skill 文档
- 最终产物：可自愈的生产守护脚本 + 可复现构建脚本 + 干净的仓库与最新运维事实文档
- 风险控制：kill/重启操作用精确 pid 或 pgrep 全词匹配防误杀；验证脚本 QQCHANNEL 相关路径全部绝对路径

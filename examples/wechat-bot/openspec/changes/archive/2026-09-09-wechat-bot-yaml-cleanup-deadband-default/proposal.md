## Why

昨天为解决压缩死区空转，在 `examples/wechat-bot/tagent.yaml` 打了 `unified_compress_target: true` 补丁（缩进 2 空格与 yaml 块 4 空格层级不符），导致手动拉起时 yaml 解析失败、启动失败；用户已把该行注释掉，bot 现稳定运行（PID 702169）。用户决定放弃该 yaml 配置开关：yaml 恢复干净、死区修复改为代码默认行为，做到"无配置即正确"。

## What Changes

- **yaml 恢复**：对照 `/tmp/tagent.yaml.bak.*` 备份，把 `examples/wechat-bot/tagent.yaml` 恢复到打补丁前状态，删除注释残留；重点核对 `keep_recent_tasks: 4` 是否被抢救时误注释，若有则恢复为生效态
- **行为内化**：`buildCompressorOpts` 中分龄压缩目标默认对齐触发线，从根上消除压缩死区空转，不再依赖 yaml 配置
- **配置字段删除**：删除 `config.go` 中 `UnifiedCompressTarget` 字段及 build_agent.go 映射、agent.go 消费分支等全部关联代码
- **构建验证**：`go build` 主框架 + wechat-bot 模块全绿
- **拉起机制排查**：排查进程停止后为何未被自动拉起（crontab / systemd / watchdog），只排查不改动运行中的 bot

## Impact

- 文件：`examples/wechat-bot/tagent.yaml`、主框架 config（config.go）、build_agent.go、agent.go 压缩选项构建逻辑
- 不重启运行中的 bot（PID 702169）；代码改动等下次计划内重启生效
- 产出：干净的 yaml、默认即正确的压缩行为、拉起机制排查结论

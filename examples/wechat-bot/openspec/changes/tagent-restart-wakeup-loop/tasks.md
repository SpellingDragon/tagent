## 1. 重启脚本（restart-tagent.sh）

- [ ] 1.1 编写 `restart-tagent.sh` 遗嘱执行人脚本：sleep 缓冲（10s 等事件落盘）→ pgrep 定位 → SIGTERM（30s 超时升级 SIGKILL）→ go build /tmp → mv 原子替换（含 wechat-bot.prev 备份与构建失败回滚）→ setsid nohup 拉起 → healthz 探活循环（120s 超时）→ 全程 log_evt 时间戳落盘 restart.log
- [ ] 1.2 脚本静态检查通过：`bash -n restart-tagent.sh` 无语法错误（shellcheck 可用时一并跑）
- [ ] 1.3 tmux 会话独立运行验证：`setsid tmux new-session -d -s tagent-restart 'bash restart-tagent.sh'` 启动后，`tmux ls` 可见 tagent-restart 会话，且会话进程父链不经过 agent 进程

## 2. 实测自杀→转世全链路

- [ ] 2.1 重启前置检查：df 确认 /tmp 与部署目录同文件系统（否则改同目录临时文件方案）、`ss -tlnp | grep 8089` 确认监听进程即 agent 自身、预跑 `go build -o /dev/null .` 确认可构建并记录构建耗时基线
- [ ] 2.2 回合边界实际触发重启：在 tmux 会话中跑起 restart-tagent.sh，观察服务盲窗（预期 10-20s，实际以 restart.log 为准），记录盲窗全程耗时
- [ ] 2.3 转世证据核对：restart.log 完整包含停机（TERM/KILL 及原因）、构建、替换、启动、探活各阶段时间戳；`pgrep -f wechat-bot` 新 PID 与旧 PID 不同
- [ ] 2.4 转世后服务恢复验证：`curl -fsS 127.0.0.1:8089/healthz` 返回 200；POST /task 注入任务端到端可达（InjectMessage 生效）；微信登录态经 token.json 免扫码恢复（WeChat poller 拉起，确认能否拉到停机期间离线消息并记录）
- [ ] 2.5 转世后销假：agent 读 restart.log 提取关键证据（PID 变化、探活耗时、盲窗耗时），经微信向用户送达销假汇报

## 3. QQchannelRobot 事件驱动联动（有事才叫）

- [ ] 3.1 读回 QQchannelRobot/resources/check.shell 现状，确认守护段/日志轮转段结构与拉起动作的记录方式（变量名、日志格式）
- [ ] 3.2 设计并追加事件检测段：仅当本轮守护真的执行了拉起动作、或日志检出风暴级异常（grep 模式按实际日志格式校准）时，经 POST 127.0.0.1:8089/task 投递带 `[event-notify]` 前缀通知；curl -m 10 超时、失败仅记日志不阻塞守护主体
- [ ] 3.3 验证有事才叫：无事件时 check.shell 正常轮次不产生任何 /task 投递（抽查 cron.log 无 [event-notify] 行）；构造拉起动作（或模拟异常日志）验证通知能达 tagent（InjectMessage 收到 [event-notify] 消息）

## 4. skill 文档沉淀

- [ ] 4.1 新增 `skills/tagent-self-restart/SKILL.md`：/task 通道用法（端点、payload 格式、事件通知示例）、重启 runbook（何时自杀、怎么调 restart-tagent.sh、restart.log 位置、销假流程）、稽核规则（销假汇报必须引用 restart.log 关键行）、空窗行为规则（先交代去向再自杀、转世后主动销假）、有事才叫规则（[event-notify] 收到即核实 cron.log 处置）
- [ ] 4.2 验证 skill 文档可被 tagent 运行时读取：确认 skills/ 目录位置正确（不参与 go build），SKILL.md 内容完整可引用

# tagent 自重启闭环 + 事件驱动唤醒机制化

## Why

tagent WeChat Bot 目前经 `HTTP /task` 入口已验证（POST 127.0.0.1:8089/task → InjectMessage 端到端通），但自重启（自杀→转世）全靠当次临场操作，无独立于 agent 进程树的遗嘱执行人；且"宣称继续工作"缺乏唤醒来源机制——agent 停机/空窗期间无人拉起、无人知道。用户已授权主 Agent 驱动全部变更并对结果负责，需要把这两件事机制化。8089 维持 loopback 无鉴权现状（用户知情接受，不再引入 token）。

## What Changes

- 新增 `restart-tagent.sh` 遗嘱执行人脚本（独立 tmux 会话运行，脱离 agent 进程树；回合边界触发时 sleep 缓冲等事件落盘；SIGTERM 优雅停机 + 超时升级 KILL；新二进制 /tmp 构建后 mv 唯一替换；启动后 curl /healthz 探活循环；全程落盘 restart.log）
- 构建新二进制 + 重启脚本就绪后，回合结束时实测自杀→转世全链路（预计 10-20s 服务盲窗，实际以 restart.log 实测为准）
- 转世后销假：读 restart.log 验证重启证据 + 验证 healthz 正常 + 微信送达用户
- QQchannelRobot 联动改为纯事件驱动：取消每小时机械心跳，仅在守护脚本真的执行了拉起动作、或日志检出风暴级异常时，才经 /task 发一条通知（有事才叫）；唤醒来源只保留设计内来源（task_settled + 外部输入事件）
- 沉淀：skill 文档更新（/task 通道用法、重启 runbook、空窗行为规则、事件驱动通知规则）

## Impact

**涉及文件：**
- `tagent/examples/wechat-bot/restart-tagent.sh` — 新增遗嘱执行人脚本
- `QQchannelRobot/resources/check.shell` — 增加事件驱动通知段（守护拉起动作 / 风暴级异常 → /task 通知），移除机械心跳设计
- `tagent/examples/wechat-bot/skills/` — 新增/更新 skill 文档（/task 通道用法、重启 runbook、空窗行为规则、有事才叫规则）
- `tagent/examples/wechat-bot/logs/restart.log` — 运行时产物（执行时生成）

**明确不涉及（已裁撤）：**
- ~~main.go middleware token 鉴权~~（8089 loopback 维持无鉴权现状）
- ~~.env 追加 TAGENT_API_TOKEN~~
- ~~check.shell 每小时 /task 心跳段~~

**最终产物：** 一套可重复执行的自杀→转世闭环：agent 在回合边界处自主触发重启，由独立 tmux 会话中的遗嘱执行人接管二进制替换与拉起，转世后自动销假并向用户汇报；QQchannelRobot 侧仅在真实事件（拉起动作/风暴级异常）时唤醒 tagent，消灭"宣称继续工作却无唤醒机制"的空窗，同时避免机械心跳噪音。

**服务影响：** 重启期间约 10-20s 服务盲窗（/healthz 不可达，实际以实测为准）；微信登录态依赖 `.wechat-config/token.json` 持久化（重启后免扫码，作为验证点实测）。

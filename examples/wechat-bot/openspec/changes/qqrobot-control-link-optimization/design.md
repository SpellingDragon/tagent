# Design: qqrobot-control-link-optimization

## Context

QQchannelRobot（`/home/lighthouse/QQchannelRobot`，Go 1.18 / 旧工具链 `/usr/local/go.bak` 构建）当前控制链路完全依赖 QQ 身份；`ops_server.go` 已在 127.0.0.1:9601 上提供 /healthz /logs /safe。约束见 proposal——服务在线（pid 3008871）、重启秒级可接受、cookie.json 不碰、所有改动读回验证。

关键现状（已在调研中核实）：
- `service/commands.go`：`commandRegistry map[string]Command`（未导出）、`GetCommand(keyword)`、`CommandArgs{From, Param, Author, AuthorName, GuildID, ChannelID, MsgID}`；指令实例带 `Do(ctx, api, i, cmdArgs)`，依赖 discordgo InteractionCreate——**/cmd 需要为无 QQ 交互上下文合成一个 InteractionCreate 或改造 Command 接口的调用方式**，这是 A 组实现的最大不确定点
- `ops_server.go` 是 package main 的一部分，与 robot.go 同包；端口 9601 仅绑定回环
- 白名单：engine.go:670 `IsInWhiteList` 硬编码 `return true`
- check.shell：每小时 cron，守护拉起 + 日志轮转；tagent 8089 的 /task 契约未确认

## Goals / Non-Goals

**Goals:**
- agent→robot：/cmd 端点复用既有指令注册表，本地视为可信
- robot→agent：check.shell 按需 POST 告警/告知，没事不吵
- 白名单 config 化，两态行为（严格/兼容+Warn）可测
- 一次重启激活 A/C，全程可回滚（git 提交前可 revert）

**Non-Goals:**
- 不改指令本身的业务逻辑（只动分发入口）
- 不做鉴权 token（本地回环 + 可信操作员模型；如需可后续叠加）
- 不碰 cookie.json、不改 B 站登录/录制链路
- 不动 qqops(1) 观察层（out-of-process 观察工具）

## Decisions

### D1: /cmd 分发走 GetCommand + 合成 CommandArgs
**选择**：POST /cmd 解析 `{command, args[]}` → `service.GetCommand(command)` → 合成 `CommandArgs{Author: "local-admin", ...}` 调用 `cmd.Do(...)`。
**理由**：注册表已存在（14 指令），复用最小化新代码面；GetCommand 是唯一稳定的查询面。
**备选**：为新通道写一套独立的 RPC 层——被否，会偏离"复用既有 14 指令"的目标且维护两套语义。
**风险点**：`Do()` 期望 `*discordgo.InteractionCreate`（QQ 频道交互上下文）。实现时需确认各指令对 InteractionCreate 的依赖深度；若普遍依赖 i 字段，则需合成最小可用的 InteractionCreate（含 MsgID/ChannelID/GuildID 与回调句柄），或在 Do 前置分支。这是 tasks 1.1 盘点要回答的问题。

### D2: GET /cmd 列表来自注册表枚举
**选择**：给 service 包加只读枚举（如 `ListCommands()`），返回 keyword/aliases/description。
**理由**：commandRegistry 未导出，package main 无法直接读；只读枚举函数面最小。

### D3: 白名单两态兼容
**选择**：config 新增 `white_list: []`，非空→严格校验；空/缺失→allow-all + 启动 Warn。
**理由**：硬改严格会打断现有用户（当前 return true 的现状等价 allow-all）；两态让收紧可灰度。
**备选**：默认严格（不兼容）——被否，违反"未配置→保持兼容"的用户约束。

### D4: 事件上报用 shell curl 直连 agent 8089
**选择**：check.shell 内嵌 curl POST，按事件类型拼 payload，无事静默。
**理由**：check.shell 已是 cron 真相源，加 curl 无新依赖；payload 契约先干跑确认再实弹。
**备选**：在 Go 进程内做事件上报——被否，进程死了就没法报"我死了"，守护脚本才是正确的观察位置。

### D5: 重启用分离会话 + 守护兜底
**选择**：按既有守护模式重启（`setsid nohup ... 9>&-` 铁律），读回验证 pid/日志/healthz。
**理由**：flock fd 继承与 pgrep 自匹配是已沉淀的坑，照既有模式走最稳。

## Risks / Trade-offs

- [Do() 对 InteractionCreate 的依赖深度未知] → tasks 1.1 盘点先行；若耦合过深，A 组降级为"先支持无上下文依赖的指令子集"，在 update 报账时上报实际覆盖面
- [白名单兼容态仍是 allow-all] → 启动 Warn 醒目提示；上线后观察一段再考虑默认严格
- [/cmd 无鉴权] → 仅回环绑定 + 本地可信操作员模型；若未来暴露给非本机，需加 token（记入 Non-Goal）
- [重启盲窗] → 秒级；守护脚本会兜底拉起，先 build 成功再重启
- [tagent /task 契约未确认] → tasks 4.1 先读源码确认，干跑后实弹，避免格式错误刷屏

## Migration Plan

1. A/C 代码 + 测试 → build + test 全绿（tasks 3.x）
2. 重启激活（tasks 5.1）→ 实弹验证 A/C（5.2/5.3）
3. B 干跑（4.3）→ 上 cron 实弹（5.4）
4. git 提交（5.5）+ 事实文档同步（5.6）
5. 回滚：重启前任何时刻可放弃改动（git checkout）；重启后发现异常 → `/safe POST v=true` 热切安全模式或 kill 后守护拉起旧二进制（bin/ 下保留当前版本备份）

## Open Questions

- tagent /task 的确切 payload 字段（task 4.1 读源码回答，不阻塞 A/C 开工）
- config 默认消息频道的具体 yaml 路径（task 1.1 顺带确认，避免 argo 段名猜错）

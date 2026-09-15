## Context

- 群 @ 管线已修复：`robot.go:219-287` `groupMessageHandler` + `isBotMentioned`（mentions[].is_you 主信号 + `config.ChannelConfig.BotOpenID` fallback），GROUP_MESSAGE_CREATE 事件，pid 3739707 实测通过。本计划在此基础上扩展，不改门控主路径。
- 主工程已有 eino v0.5.4 与成熟的 ReAct 范式：`service/react_agent.go` `NewReactAgent`（openai.ChatModel + `compose.ToolsNodeConfig` + `react.AgentConfig{MaxStep:15}`）；`service/react_tool.go` `BindReactTools`（:699）已示范"工具信息 + 实现 + BindTools"的注册模式，mute 等群管理工具按同一模式追加即可。
- **SDK fork DEFERRED（2026-09-08）**：GitHub 网络四路全败（git@ SSH 挂起、HTTPS clone/tarball 超时，仅 ssh -T 认证通），fork 工作区不可得；用户指示"SDK 拉不下来就先不优化 SDK"。原 v2 SDK 封装路线整体移入第 7 节 DEFERRED 队列，网络恢复后另立 change。
- **mute 改走 v2 REST 直调**：`robot.go:90-96` 已实证 `token.NewQQBotTokenSource(AppID/AppSecret)` + `StartRefreshAccessToken`（自动刷新）+ `botgo.NewOpenAPI(...).WithTimeout`——鉴权路径现成，主工程内 net/http 直调无需动 SDK。契约：`kb/api/qq/group_restrict_chat_setting.md`（QQchannelRobot/kb/api/qq/）——`POST /v2/groups/{group_openid}/restrict_chat_setting`，body `action_type`(add/del) + `member_openid`，头 `Authorization: QQBot <token>`；⚠️ 网传 `PUT .../member_mute` 路径不存在；⚠️ **白名单能力**（未开通报 11253，实弹前小流量验证）；⚠️ **契约无时长字段**（到期解除为平台语义，原"默认 60s / 上限 300s"不适用）。
- 构建：`bash build.sh`（go1.18 冻结工具链），不得用新工具链替换；config.yaml 不入 git（.gitignore 已含），敏感字段 cookie/token 注意脱敏。
- kb/ 归档结构：`kb/AGENT.md` + `kb/api` + `kb/ops` + `kb/timeline`。

## Goals / Non-Goals

**Goals:**
- mute 注册为 LLM 工具（v2 REST 直调），description 承载"说明与安抚"指引，LLM 自主调用
- 群主 @ / 昵称触发 + 活跃窗口 / 冷却 / 预算三重门控（方案已与用户对齐）
- per-group 串行 + 跨群并行 + 有界 worker 池的消息并发模型
- 全部经 build + 实测（禁言测试群实弹一次，前提白名单通过），kb/INDEX 与 timeline 更新
- SDK defer 项登记（D1-D6），kb/timeline 留网络恢复后的接力记录

**Non-Goals:**
- 不改群 @ 门控主路径（已修复，pid 3739707 验证）
- 不做硬编码审核流 / 关键词过滤（用户明确要求 LLM 自主判断）
- 不重构既有 ReAct 工具集（录播 / 剪辑工具不动）
- 不升级 go 工具链
- **本期不做 SDK fork**（网络不可达，DEFERRED 至网络恢复后另立 change）；角色查询工具随 SDK defer（契约未入 kb，直调无依据）

## Decisions

1. **mute 走 LLM 工具而非硬编码**：description 即 prompt 载荷（适用情形 + 说明安抚话术），LLM 在 ReAct 循环中自主判断调用。备选是关键词过滤 + 定时器硬编码，被否——用户明确要求"不做硬编码审核流"，且硬编码无法处理语境歧义。
2. **工具注册收敛到 BindReactTools 模式**：沿用 `react_tool.go` 既有模式（schema.ToolInfo + tool.BaseTool + chatModel.BindTools），不新开注册路径。群管理工具需携带群上下文（group_openid + 触发用户 openid），经 ReactCtx 或独立 ctx 结构注入。
3. **SDK fork DEFERRED，mute 走 v2 REST 直调**：fork 拉取四路全败，用户指示先不优化 SDK；主工程内直调客户端复用既有 QQBotTokenSource（robot.go:90-96 实证），契约以 `kb/api/qq/group_restrict_chat_setting.md` 为准。备选：等网络恢复再做 SDK 路线——被否，mute / 门控 / 并发不应被 SDK 阻塞。**调用层隔离**：工具 → 直调客户端 → REST，SDK 恢复后 D6 切换封装不改工具行为。
4. **契约修正**：正确路径 `POST .../restrict_chat_setting`（action_type add/del + member_openid）；网传 `PUT .../member_mute` 不存在。**工具不暴露时长参数**（v2 契约无 seconds 字段，到期解除为平台语义；原"默认 60s / 上限 300s"语义不适用），description 写"短时处置，到期自动解除"。
5. **门控在 handler 层、回复在 worker 层**：触发判定（@/昵称/活跃窗口/冷却/预算）同步在 handler 完成（零 LLM 成本），通过后进入 per-group 队列；LLM 谑用与回复发送在 worker 摸中执行。@ 全量处理，昵称触发受活跃窗口约束。
6. **并发模型**：per-group FIFO 队列（有界）+ 跨群并行由有界 worker 池承载（池大小配置化），单任务超时 + panic recover 隲离。备选：每消息一 goroutine——被否，消息风暴下 goroutine 无界。
7. **v2 直调鉴权与错误透传**：复用 QQBotTokenSource（短效 token 自动刷新）；非 2xx 错误体透传，白名单 11253 / 权限 / 非群成员错误可区分，工具层据此降级；401 时重取 token 重试一次。

## Risks / Trade-offs

- [v2 白名单未开通（11253）] → 1.3 小流量验证前置：未开通则记录结论、停用实弹项，工具层保留但 description 降级为口头警告优先（能力注册不受阻）
- [mute 实弹误伤真人] → 测试群先用机器人自己/测试号作目标；到期后 GET mute_expire_at 复核解除
- [昵称触发误匹配] → 词边界 / 全词匹配 + 大小写不敏感，活跃窗口收窄误触发面
- [LLM 误判导致错误禁言] → description 写明"仅在明确有害信息时使用，优先口头警告"约束 + 冷却期天然限流；误判案例回填 kb
- [config.yaml 敏感字段泄漏] → .gitignore 确认；文档示例用占位符
- [per-group 串行 + 有界池在极端负载下丢消息] → 队列上限 + 丢弃日志，属可接受降级（消息风暴下丢旧保新）
- [eino react.Agent 与新工具的 MaxStep 交互] → mute 类单步工具不进 ToolReturnDirectly；实测确认无循环
- [直调与未来 SDK 封装双轨并存] → 调用层隔离（Decisions 3），D6 切换时回归验证即可

## Migration Plan

1. v2 直调客户端（复用 token 体系）→ build 通过 → 白名单小流量验证（11253 探测）
2. 门控 + 并发模型合入（先门控后并发，各自可独立回滚）
3. mute 工具注册（依赖第 1 步就绪）→ 测试群实弹（前提白名单通过）
4. 归档：kb/INDEX、kb/timeline 更新（含 SDK defer 记录）；归档前确认 build.sh 产物存在
5. **网络恢复后**：另立 change 执行 D1-D6（fork 克隆 → v2 SDK 封装 → replace 接入 → 直调切换 SDK 封装）

回滚：门控/并发为独立提交，可单独 revert；直调客户端独立文件，移除即回无 mute 状态。

## Open Questions

- v2 白名单是否已开通（1.3 实测决定：11253 → 停用实弹项，其他 → 继续实弹）
- 预算窗口的粒度（日 / 小时）与具体阈值（执行时与用户对齐配置默认值）
- 群成员角色 v2 接口的具体路径与响应结构（随 SDK defer，网络恢复后以 fork 内接口文档为准）

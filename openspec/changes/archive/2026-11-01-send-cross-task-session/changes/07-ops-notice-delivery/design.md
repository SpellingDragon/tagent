# 07 技术要点

1. **固定目标**：通知目标=配置 `ops_session`（缺省宿主运维会址常量，`TAGENT_SESSION_ID` 前缀同源）；`InjectInternal` 空地址 pending 路径仅保留给非运维内部通知（receipt 类），启动通知不再走它。
2. **立即唤醒**：通知入链后驱动目标会话一轮（`Host.Invoke`，TriggerSource 记通知来源）；与 03 驱动面共用，无第二条唤醒路径。
3. **现场查询**：`wechat_notice.go` 的 WAL 尾部查询由 nil 改为受限真实查询面（有限条数上限常量，沿既有"degraded scene block"降级分支兜底）。
4. **正文保全清单**（重写=保全）：`[转世通报]`/`pid:`/`现场块不可用`/`[reincarnation]` 日志关键字、元数据段、`restartDoneFreshWindow` 新鲜度语义——逐项迁移核对。

文件清单：`examples/wechat-bot/wechat_notice.go`、`wechat.go`（注入口地址）、配置面（ops_session）。
风险：唤醒与 bot 登录时序（通知先于渠道绑定到达）——唤醒失败降级为"落链待读"，不丢（06 接纳面兜底）。

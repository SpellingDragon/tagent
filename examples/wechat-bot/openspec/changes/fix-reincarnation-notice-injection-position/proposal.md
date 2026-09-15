## Why

用户在轨迹逐字节验证中发现严重实现偏差：转世通报 REINCARNATION_NOTICE 在进程启动后固定 +5s 延迟注入（`examples/wechat-bot/reincarnation_notice.go:190` 附近 `time.Sleep(delay)`），而 WAL 恢复重放耗时不定（长 WAL 可达数分钟）；重放未完成时，事件循环 BeforeModel 的 TryPull（`agent/context_manager.go:654`）先消费通报并持久化进投影——落在恢复历史**之前**（头部、role=system），导致重启后送入 LLM 的 messages 前缀与重启前完全不同（重启后首调 messages 头部多出一条通报 system 消息、历史段整体后移）。用户拍板的设计意图：通报应在恢复现场完成后于**尾部**注入（作为 external input 落尾）。次要缺陷：注入 source 复用 "meditation"（`reincarnation_notice.go:213` `InjectMessageWithSource("meditation",...)`），语义混用且会误触发 meditation 投递门禁。

## What Changes

- **注入时机从固定 sleep 改为等「rebuild/投影就绪」信号**：通报注入等待投影 rebuild 完成标志（框架内现成信号；无则最小侵入新增），不再与 WAL 重放竞速。
- **source 独立化**：注入 source 由 "meditation" 改为 "reincarnation"，消除语义混用与 meditation 投递门禁误触发；通报作为尾部 external input（role=user）注入。
- **注入位置修复**：注入后通报自然落在恢复现场尾部（恢复历史之后），保持 system 头冻结（业界范式：系统级通知以尾部 user 消息注入，Claude Code system-reminder 范式）。
- **持久化语义不变**：通报仍经 ExternalInput → StoreEvent（WAL）→ projection.Append 落投影（事件溯源不变）。

## Impact

- 代码：`examples/wechat-bot/reincarnation_notice.go`（时机 + source）、`examples/wechat-bot/main.go`（dispatch 新增 "reincarnation" case 或确证 default 分支行为）、可能的最小侵入框架信号（`agent/` 下 rebuild 完成标志/回调）
- 测试：`examples/wechat-bot/reincarnation_notice_test.go` 扩展；`examples/wechat-bot/` 全包回归（rebuild/fallback/orphan 转世连续性）
- 并行协调：与「冥想门禁失效修复」计划在 main.go dispatch 的改动需协调不冲突（两处均为 switch case 增量，冲突面小，但需在 PR 前互查）
- 不变更：WAL 事件溯源、system 头语义、meditation 注入路径本身

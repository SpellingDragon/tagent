# Tasks — fix-reincarnation-notice-injection-position

## 1. 取证复核（spec/design 证据链复核）

- [ ] 1.1 复核框架侧注入/重建路径读回结论：`tagent/build_agent.go:547-550`（ownsPersistentState → RebuildProjectionFromWAL 同步执行）、`tagent/agent/projection_rebuild.go`（snapshot + tail 分页 / fallback 全量回放）、`tagent/agent/inject.go:29`（InjectMessageWithSource → persistentBus.Publish）、BeforeModel TryPull 消费点（`agent/context_manager.go:654` 附近）——确认 race 证据链与 design.md Context 逐条一致，产出复核清单（附行号）。
- [ ] 1.2 复核 example 侧证据：`examples/wechat-bot/reincarnation_notice.go:190`（time.Sleep 固定延迟）、`:213`（InjectMessageWithSource("meditation",...)）、`main.go:205`（go maybeInjectReincarnationNotice + 5*time.Second）、main.go 事件分发 switch 现状（确认 "reincarnation" 无显式 case、当前落 default 的行为）——确认与 spec ADDED Requirements 一一对应。

## 2. 框架侧实现（最小侵入信号）

- [ ] 2.1 在 `tagent/build_agent.go` ownsPersistentState 分支收尾处（rebuild + task registry 重建 + orphan 裁决之后）暴露「投影就绪」信号：实现时在回调 / 就绪标志 / channel 三种载体中择一（最小侵入原则），信号只携带"完成"事实不携带数据；`ownsPersistentState()` 为 false 的模式（executorOnly 热重建）不得挂信号路径。
- [ ] 2.2 框架侧单测：信号在 rebuild 完成后置位（长 WAL 回放期间信号不置位）；executorOnly 模式不触发信号；信号幂等（重复等待不阻塞）。
- [ ] 2.3 跑框架侧相关包测试（`tagent/`、`tagent/agent/`）确认零回归（build_cycle / projection_rebuild / partitions 既有测试全绿）。

## 3. Example 侧实现（wechat-bot）

- [ ] 3.1 `reincarnation_notice.go`：移除 `time.Sleep(delay)` 固定等待，改为等待 2.1 的投影就绪信号后才进入 detect → compose → inject 流程；信号等待时长打日志（替换原 5s 假设）。函数签名/参数语义同步调整（delay 参数退役或语义改为信号等待上限兜底）。
- [ ] 3.2 `reincarnation_notice.go:213`：`InjectMessageWithSource("meditation", ...)` → `InjectMessageWithSource("reincarnation", ...)`，role 保持 `model.RoleUser`；消费端事件链（trigger_source）与 WAL 事件记录同步呈现 source=reincarnation。
- [ ] 3.3 `main.go:205`：调用点适配新签名（传入信号等待方式）；main.go 事件分发 switch 新增 `case "reincarnation"`：静默消化 + 日志记录（`[reincarnation] notice consumed by event loop`），不落 default。
- [ ] 3.4 确认 notice 事件持久化链路不变：注入 → persistentBus → BeforeModel TryPull → StoreEvent（WAL）→ projection.Append，重启后重放可见该事件（source=reincarnation, role=user）。

## 4. 回归测试（长/短 WAL 双场景）

- [ ] 4.1 长 WAL 场景测试：构造重放耗时超过原 5s 固定延迟的 WAL（或注入人为延迟的重放），验证通报注入发生在重放完成信号**之后**、且 notice 事件位于投影尾部（恢复历史之后）——断言投影最后一条为通报事件、system 头无通报 system 消息。
- [ ] 4.2 短 WAL 场景测试：重放快于原 5s 完成，通报在信号后注入，行为不劣化（注入时刻 ≥ 投影就绪时刻，无回归）。
- [ ] 4.3 source 独立性测试：通报注入后事件中 source=reincarnation；meditation 投递门禁（分批/静默逻辑）不被触发；main.go 分发 case "reincarnation" 行为锁定（静默消化 + 日志）。
- [ ] 4.4 既有转世连续性回归：`examples/wechat-bot` 包既有单测全绿（detect/metadata/WAL tail/notice text/breakpoint 判定/rebuild/fallback/orphan 场景），无行为回归。
- [ ] 4.5 逐字节前缀验证（轨迹级）：构造重启前后两次 LLM 调用轨迹，断言重启后 messages 的公共前缀与重启前逐字节一致（对齐 batch 3315 前缀 311/311 EXACT 的理想形态），通报仅出现在尾部。

## 5. 部署与验收

- [ ] 5.1 部署：合并后滚动重启一次（保险链自替换即触发通报注入路径，生产验证）；确认重启日志中出现信号等待时长与 `[reincarnation] notice injected`（source=reincarnation）记录。
- [ ] 5.2 轨迹级验收：生产轨迹抽查重启前后首调 messages——通报位于尾部、公共前缀逐字节一致；通报事件在 WAL 中 source=reincarnation 且重放可见。
- [ ] 5.3 结项归档：全部任务完成并经产物实证后，plan agent 执行 archive。

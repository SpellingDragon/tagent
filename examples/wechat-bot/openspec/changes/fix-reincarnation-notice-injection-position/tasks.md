# Tasks — fix-reincarnation-notice-injection-position

> **修订记录（2026-09-15 取证终局）**：三连探针（416faf77 / 95eec2c9 / 563e21b9）证伪初始前提（头部注入不存在，前缀从未被破坏）；真缺陷重定义为 ①固定 5s sleep vs 不定长 WAL 重放竞态（迟到，s67 实证） ②source 复用 "meditation"。§1 已按终局重写并勾选（复核完成），§4 验收改为四不变量（时序/尾部/source/前缀），§5 部署改为与 b31f4bd 合批。详见 design.md Context（实证+代码双事实链）。

## 1. 取证复核（终局采信 + 代码证据链复核，已完成）

- [x] 1.1 框架侧证据链复核：`tagent/build_agent.go:547-550`（ownsPersistentState → RebuildProjectionFromWAL 同步执行）、`tagent/agent/projection_rebuild.go:28`（snapshot + tail 分页回放）、`tagent/agent/inject.go:29`（InjectMessageWithSource → persistentBus.Publish）——已读回核实，与修订后 design.md Context 代码事实链逐条一致（复核清单附行号见 design.md）。
- [x] 1.2 example 侧证据复核：`reincarnation_notice.go` maybeInjectReincarnationNotice 主流程 `time.Sleep(delay)` + 注入点 `InjectMessageWithSource("meditation", ...)`（头部注释即 "inject via the meditation source"）、`main.go:205` 调用点 +5s、`main.go:429` case "meditation" 静默消化（fail-closed gate main.go:418 已提交未部署）、`system_alert.go:45` 同构——已读回核实，与 spec ADDED Requirements 一一对应。
- [x] 1.3 取证终局记录（2026-09-15）：三探针（416faf77/95eec2c9/563e21b9，生产轨迹侧取证，产物已落 app workspace、plan agent 逐份读回实证）确认——12 次重启首调（batch==0 且 msgs>30）带完整元数据的通报全部落尾部（pos 58~382 递增）；s67 通报首调 rec#3315 缺席、rec#3405 pos=191 出现（迟到实证）；早前 "HEAD pos=3~8" 系压缩摘要卡字样残留（rec#3405 pos=3 验尸为 [context_compress] 卡）。结论：头部注入不存在，尾部现状正确，前缀不变量从未被破坏；缺陷重定义见 design.md 修订记录。
      - 探针产物可读路径（plan agent 已逐份读回实证，均在 app workspace `.tagent-workspace/tool-output/` 下）：①spawn 序+12 次重启首调落位表 `task-416faf77-1789466202024.txt`；②4 个 HEAD 记录全命中+reincarnated_at 区分新旧 `task-95eec2c9-1789466397020.txt`；③pos=3 异类验尸（压缩摘要卡实文+rec#3315/3405 头部 9 条结构+s68 通报落位扫描）`task-563e21b9-1789466697022.txt`。
      - §4 baseline 备注（plan agent 核验）：s68 通报 reincarnated_at=2026-09-15 03:19:32Z（本地 09:19:32，UTC+6）落 rec#3405（ts 09:20:43，msgs=236）pos=191、role=user——证据实文见探针②；restart→首调 ~71s 内必达且位于恢复历史之后（pos=191 之后仍有 44 条恢复历史消息，"尾部"指恢复历史之后、非字面末位）；不变量①「必达且不早于恢复完成」与不变量②「落尾部」在 s68 已成立，§4 修复后回归以此为对照基线。

## 2. 框架侧实现（最小侵入信号）

- [ ] 2.1 在 `tagent/build_agent.go` ownsPersistentState 分支收尾处（rebuild + task registry 重建 + orphan 裁决之后）暴露「投影就绪」信号：实现时在回调 / 就绪标志 / channel 三种载体中择一（最小侵入原则），信号只携带"完成"事实不携带数据；`ownsPersistentState()` 为 false 的模式（executorOnly 热重建）不得挂信号路径。
- [ ] 2.2 框架侧单测：信号在 rebuild 完成后置位（长 WAL 回放期间信号不置位）；executorOnly 模式不触发信号；信号幂等（重复等待不阻塞）。
- [ ] 2.3 跑框架侧相关包测试（`tagent/`、`tagent/agent/`）确认零回归（build_cycle / projection_rebuild / partitions 既有测试全绿）。

## 3. Example 侧实现（wechat-bot）

- [ ] 3.1 `reincarnation_notice.go`：移除 `time.Sleep(delay)` 固定等待，改为等待 2.1 的投影就绪信号后才进入 detect → compose → inject 流程；信号等待时长打日志（替换原 5s 假设）。函数签名/参数语义同步调整（delay 参数退役或语义改为信号等待上限兜底）。
- [ ] 3.2 `reincarnation_notice.go` 注入点：`InjectMessageWithSource("meditation", ...)` → `InjectMessageWithSource("reincarnation", ...)`，role 保持 `model.RoleUser`；消费端事件链（trigger_source）与 WAL 事件记录同步呈现 source=reincarnation。
- [ ] 3.3 `main.go:205`：调用点适配新签名（传入信号等待方式）；main.go 事件分发 switch 新增 `case "reincarnation"`：静默消化 + 日志记录（`[reincarnation] notice consumed by event loop`），不落 default——该日志即注入成功审计口径。
- [ ] 3.4 确认 notice 事件持久化链路不变：注入 → persistentBus → BeforeModel TryPull → StoreEvent（WAL）→ projection.Append，重启后重放可见该事件（source=reincarnation, role=user）。

## 4. 回归测试（四不变量 + 双场景）

- [ ] 4.1 竞态消除（不变量①）：构造重放耗时超过原 5s 固定延迟的 WAL（或注入人为延迟的重放），断言通报注入时刻 ≥ rebuild 完成信号时刻——重放期间不注入，信号后注入；同时断言尾部位置（不变量②）：notice 事件位于投影恢复历史之后（投影最后一条为通报事件、无头部通报 system 消息）。
- [ ] 4.2 短 WAL 场景：重放快于原 5s 完成，通报在信号后注入，行为不劣化（注入时刻 ≥ 投影就绪时刻，无回归）。
- [ ] 4.3 source 独立性（不变量③）：通报注入后事件中 source=reincarnation；meditation 投递门禁（分批/静默逻辑）不被触发；main.go 分发 case "reincarnation" 行为锁定（静默消化 + 日志）。
- [ ] 4.4 既有转世连续性回归：`examples/wechat-bot` 包既有单测全绿（detect/metadata/WAL tail/notice text/breakpoint 判定/rebuild/fallback/orphan 场景），无行为回归。
- [ ] 4.5 前缀逐字节不破坏（不变量④，轨迹级）：构造重启前后两次 LLM 调用轨迹，断言重启后 messages 的公共前缀与重启前逐字节一致（对照 batch 3315 理想形态：前缀全等、无头部通报消息、历史段不后移）——实证现状已满足，本项为修复后回归不变量（修复不得破坏现状）。

## 5. 部署与验收（与 b31f4bd 合批）

- [ ] 5.1 部署（修订）：与 Plan A 的 b31f4bd（fail-closed 主修复 + b287f8b 测试）**合批滚动重启**——一次换装同时带上 fail-closed gate 与通报修复，消除「通报以 meditation source 被 case "meditation" 静默消化、无审计」的隐性依赖中间态。确认重启日志出现信号等待时长与 `[reincarnation] notice injected`（source=reincarnation）记录。
- [ ] 5.2 轨迹级验收：生产轨迹抽查重启后首调——通报在场（不迟到）、位于尾部、公共前缀逐字节一致（对照 batch 3315 形态）；通报事件在 WAL 中 source=reincarnation 且重放可见。
- [ ] 5.3 结项归档：全部任务完成并经产物实证后，plan agent 执行 archive。

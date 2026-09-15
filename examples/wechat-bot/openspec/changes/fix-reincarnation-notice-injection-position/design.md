# Design — 转世通报注入位置修复（fix-reincarnation-notice-injection-position）

## Context

重启（保险链自替换）后，转世通报 REINCARNATION_NOTICE 的注入时机与 WAL 恢复重放竞速，导致通报可能落入恢复历史**之前**（头部注入，role=system），破坏重启前后送入 LLM 的消息序列公共前缀。见 proposal.md - Why（用户逐字节验证实证：头部注入场景从第 4 条起错位；理想形态 batch 3315 前缀 311/311 EXACT）。

已实证的机制事实链（顾问独立读回核实，非转述）：

1. **WAL 重放是 build 流程内的同步长操作**：`tagent/build_agent.go:547-550` `if mode.ownsPersistentState() { ta.RebuildProjectionFromWAL() }`；`RebuildProjectionFromWAL`（`tagent/agent/projection_rebuild.go:28`）在 `tagent.New()` → `buildAgent` 内**同步**执行——snapshot 恢复 + tail 分页回放（`tailPageSize=500`，分页取全），或无 compaction 锚时 fallback 全量回放（`fallbackCap=500` 截断）。长 WAL 下为分钟级长操作，期间 `New()` 不返回。
2. **通报注入用固定 +5s sleep，与重放时长无序依赖**：`examples/wechat-bot/reincarnation_notice.go:190` `time.Sleep(delay)`；调用点 `main.go:205` `go maybeInjectReincarnationNotice(ta, tagentCfg.Entry, filepath.Join("run"), 5*time.Second)`。固定 sleep 的 5 秒与重放耗时（可变、分钟级）之间无任何因果序——重放未完成时通报可先入总线，被事件循环持久化进投影，落在恢复历史之前 = 头部注入。
3. **source 复用 "meditation" 造成语义混用**：`reincarnation_notice.go:213` `ta.InjectMessageWithSource("meditation", ...)`。注入路径 `InjectMessageWithSource`（`tagent/agent/inject.go:29`）→ `persistentBus.Publish(NewExternalInputEvent(source, msg))` → 事件循环 BeforeModel TryPull → StoreEvent（WAL）→ projection.Append；source 同时是消费端分派键（StateDelta["trigger_source"]），复用 "meditation" 会误触发冥想投递门禁。
4. **注入时序约束（读回核实）**：notice goroutine 在 `New()` 返回后、`StartLoop` 之后由 main 启动（`main.go:205`），事件循环消费注入消息（BeforeModel TryPull，`agent/context_manager.go:654` 附近）。尾部注入的成立条件 = 注入时刻晚于投影 rebuild 完成且晚于 StartLoop 后首个消费周期，两项同时满足时 notice 事件在 WAL 与投影中均位于恢复历史之后。

**D0 原则（用户拍板，覆盖此前任何 role 视角表述）**：事件分类 / 投递路由 / 注入位置是**框架机制**，与 LLM 收到什么 role 无关。修复不引入任何"role 视角"的因果解释——通报作为 external input（role=user，仅是消息载荷的 role 字段）经事件溯源注入尾部，是因为事件写入序天然落在恢复历史之后；不是"因为它是 system 提醒所以放头/尾"。

## Goals / Non-Goals

**Goals:**

- 通报注入时机挂到「rebuild/投影就绪」信号之后（ownsPersistentState 分支收尾即注入点），替代固定 +5s sleep——机制级修法：注入天然落尾部、保重启前后 prompt 公共前缀（逐字节可比对）。
- 注入 source 独立化："meditation" → "reincarnation"，消除语义混用与冥想投递门禁误触发。
- main.go 事件分发对 source="reincarnation" 显式定义行为（投递或静默消化），不落 default 静默吞掉。
- 既有转世连续性行为（rebuild/fallback/orphan 场景）零回归。

**Non-Goals:**

- 不变更 WAL 事件溯源、投影 fold 语义、system 头冻结机制、meditation 注入路径本身。
- 不修 `system_alert.go`（main.go:208 `maybeConsumeSystemAlert` 同为固定 5s sleep + 复用 meditation source 的同构缺陷，`system_alert.go:45` InjectMessageWithSource("meditation",...)）——记录为后续独立修复项，本计划保持增量。
- 不引入框架级通用「启动就绪」事件总线新原语；仅在必要处最小侵入暴露 rebuild 完成信号。
- 不做任何 role 视角的注入位置论证（D0 原则禁止）。

## Decisions

### D1 注入时机：挂 rebuild 完成信号，替代固定 sleep

**决定**：通报注入改挂 `RebuildProjectionFromWAL` 完成信号之后——`build_agent.go` `ownsPersistentState()` 分支收尾处即注入点（rebuild + task registry 重建 + orphan 裁决完成之后）。框架侧提供最小信号面（回调 / 就绪标志 / channel 任选其一，实现时按最小侵入定夺），example 侧等待信号后再走 detect → compose → inject 流程。

**为什么（机制级，非 role 视角）**：注入时刻晚于 rebuild 完成 ⟹ StoreEvent 写入序晚于全部恢复事件 ⟹ projection.Append 落尾 ⟹ 下次重启重放时 notice 事件位于恢复历史之后。因果链全程与消息的 role 字段无关（D0），仅由事件写入序决定位置。

**备选方案与否决理由**：

- *备选 A：保留 sleep 但拉长（如 30s/60s）*——仍是时长猜测，与重放耗时无序依赖，长 WAL 下照样抢跑，仅推迟症状。否决。
- *备选 B：example 侧轮询 `MemStore()` 事件数稳定 N 秒才算就绪*——启发式，两个采样点之间重放仍在进行时误判；且消费投影内部状态破坏分层。否决。
- *备选 C：通报注入挪进 `New()` 内 rebuild 之后同步执行*——注入发生在事件循环 StartLoop 之前，bus 尚未消费，Publish 只是排队；但 detect/compose 需要 `ta.MemStore()`，且把 example 专属逻辑焊进框架 build 流程，污染框架。否决——框架只提供**信号**，example 持有注入逻辑。

### D2 source 独立化："reincarnation"

**决定**：`InjectMessageWithSource("reincarnation", model.Message{Role: model.RoleUser, ...})`。

**为什么**：source 是消费端分派键（trigger_source 贯穿事件链，分批/静默门禁按它判定）；复用 "meditation" 使通报事件被误归入冥想通道（门禁误触发、审计口径污染）。独立 source 后：
- 分发行为显式定义（见 D3）；
- 事件溯源记录中通报事件的 lineage 清晰（source=reincarnation, role=user）。

**备选与否决**："system_alert" 语义不符；"user" 会误触发 meditation novelty gate（`inject.go` `armMeditationNoveltyGate` 仅对 source=="user" 生效）。"reincarnation" 无此副作用。

### D3 main.go 分发显式 case

**决定**：main.go 事件分发 switch 新增 `case "reincarnation"`：静默消化（不回投用户，通报内容是给 agent 的现场交接，不是给用户的消息），日志记录 `[reincarnation] notice consumed by event loop`。

**为什么**：spec 要求显式处理而非落 default 静默吞掉；静默消化 + 日志是三者（投递/静默消化/落 default）中唯一既不打扰用户又不引入投递内容脱敏复杂度的选择。若后续需要用户可感知，可增量加投递（须过 T-G 敏感字段门禁）。

## Risks / Trade-offs

- [长 WAL 下通报等待时间变长（原 5s → 重放实际耗时）] → 这是正确行为：宁可晚注入也不能抢跑头部注入破坏前缀；日志记录等待时长供观测。
- [框架信号面侵入框架代码] → 最小侵入：优先复用现有结构（build_agent 已在 ownsPersistentState 分支收尾处有明确边界），信号只暴露"完成"事实不携带数据；测试覆盖信号时序。
- [system_alert.go 同构缺陷残留] → 本计划不动它，design 明确记录；后续独立修复项跟进（避免本 PR 混入两个机制变更）。
- [并发重启窗口（重启脚本在 build 期间再次触发重启）] → 通报 one-shot + consume-marker（D5 rename）语义保持不变，重复通报可接受（现有日志已声明）。
- [事件循环尚未消费 notice 期间进程再次死亡] → notice 事件已 StoreEvent 进 WAL（事件溯源持久），下次重启重放可见；consume-marker 已 rename 前死亡 → 重复通报，可接受（既有语义）。

## Migration Plan

1. 框架侧：build_agent.go ownsPersistentState 分支收尾暴露 rebuild 完成信号（最小侵入）。
2. example 侧：reincarnation_notice.go 移除 `time.Sleep(delay)` → 等待信号；source 改 "reincarnation"；main.go:205 调用点同步调整（delay 参数语义变为信号等待）；main.go 分发新增显式 case。
3. 测试：reincarnation_notice_test.go 扩展（信号等待注入、长 WAL 场景尾部注入、短 WAL 无回归、source 独立、分发 case 锁定）。
4. 部署：合并后滚动重启一次（保险链自替换即触发通报注入路径，生产验证）。
5. 回滚：单 PR 整体 revert 即可——无数据迁移、无事件 schema 变更、无配置变更（纯机制修复，事件流向后兼容：旧事件 source=meditation 的通报在回滚后重放不受影响）。

## Open Questions

无阻塞项。实现时按最小侵入在「回调 / 就绪标志 / channel」三种信号载体中择一（不改变 specs 与任务分解，属实现自由度）。

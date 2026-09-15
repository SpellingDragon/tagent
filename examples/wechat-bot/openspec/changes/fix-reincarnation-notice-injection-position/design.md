# Design — 转世通报注入时机修复（fix-reincarnation-notice-injection-position）

## Context

**修订记录（2026-09-15，取证终局）**：本计划初始前提——「通报头部注入、破坏重启前后前缀」——经三连探针（416faf77 / 95eec2c9 / 563e21b9）生产数据复核**证伪**。真缺陷重新定义为两个：**①固定 5s sleep 与不定长 WAL 重放的竞态（迟到）**；**②source 复用 "meditation"**。修法主体（挂 rebuild 完成信号 + source 独立化）不变，目标改述为「消灭竞态窗、保证通报必达且不早于恢复完成」。本节以下以实证结论重写。

### 实证事实链（前提证伪）

- **尾部注入现状正确**：12 次重启首调（batch==0 且 msgs>30）中，全部带完整元数据（reincarnated_at 可解析）的通报**无一例外落尾部**（role=user，pos 58~382，随历史长度递增）。
- **s67 迟到实证**：s67（03:19:32 重启）自己的通报在首调 rec#3315 **缺席**、在 rec#3405 pos=191 出现——注入发生在首调之后，是**迟到**而非位置错误。
- **早前探针读数修正**：此前报告的 "HEAD pos=3~8" 全部是**压缩摘要卡片内的字样残留**（〔历史综述〕提及通报文本，卡片本身无元数据块）——验尸 rec#3405 pos=3 全文证实其为 [context_compress] 历史归档卡。
- **重启首调头部真实结构**：prompt 拼接（system×N，逐字节稳定）+ 恢复投影头部系统注入（feedback/governance，设计内行为）+ 压缩摘要卡（设计内）+ 恢复历史。**前缀不变量从未被通报破坏。**
- 取证产物说明：三探针报告本地工程不可读（生产轨迹系统侧），结论以主 Agent 报账为准；顾问已独立读回代码侧证据链（见下），两侧自洽。

### 代码事实链（顾问独立读回核实，附行号）

1. **WAL 重放是 build 流程内的同步长操作**：`tagent/build_agent.go:547-550` `if mode.ownsPersistentState() { ta.RebuildProjectionFromWAL() }`；`RebuildProjectionFromWAL`（`tagent/agent/projection_rebuild.go:28`）在 `tagent.New()` → `buildAgent` 内**同步**执行——snapshot 恢复 + tail 分页回放（`tailPageSize=500`），长 WAL 下为分钟级长操作，期间 `New()` 不返回。
2. **通报注入用固定 +5s sleep，与重放时长无因果序**：`examples/wechat-bot/reincarnation_notice.go` maybeInjectReincarnationNotice 主流程 `time.Sleep(delay)`；调用点 `main.go:205` `go maybeInjectReincarnationNotice(..., 5*time.Second)`。固定 5 秒与可变重放耗时之间无任何因果序——**重放未完成时通报可能先入总线**；即便注入已发生，事件循环可能尚未消费——两者共同构成「通报迟到于首个 LLM 调用」的竞态窗。s67 即此窗口的实证（首调缺席、次调出现）。
3. **source 复用 "meditation" 造成语义混用**：`reincarnation_notice.go` 注入点 `ta.InjectMessageWithSource("meditation", model.Message{Role: model.RoleUser, ...})`（头部注释即 "inject via the meditation source (D2)"）；注入路径 `InjectMessageWithSource`（`tagent/agent/inject.go:29`）→ `persistentBus.Publish(NewExternalInputEvent(source, msg))` → 事件循环 BeforeModel TryPull → StoreEvent（WAL）→ projection.Append。source 同时是消费端分派键（trigger_source），复用 "meditation" 使通报事件 lineage 归入冥想通道。
4. **fail-closed 门禁落地后的隐性依赖**：`main.go:418` 起 FAIL-CLOSED gate（unified-event-delivery，b31f4bd 已提交）对未盖章输出内部消化；`main.go:429` `case "meditation"` 静默消化（"Meditation: internal output, don't send to user"）。通报以 meditation source 注入 → LLM 的相关回应输出会经此 case 静默消化——不投用户是**可接受的**，但无「注入成功」审计口径，且 lineage 混用使冥想/转世两类事件在事件溯源记录中不可区分。`system_alert.go:45` 同构（`InjectMessageWithSource("meditation",...)` + 固定 sleep），已登记 Non-Goals 后续独立修复。

**D0 原则（用户拍板，覆盖此前任何 role 视角表述）**：事件分类 / 投递路由 / 注入位置是**框架机制**，与 LLM 收到什么 role 无关。修复不引入任何 "role 视角" 的因果解释——通报作为 external input（role=user，仅是消息载荷的 role 字段）经事件溯源注入尾部，是因为事件写入序天然落在恢复历史之后；不是 "因为它是 system 提醒所以放头/尾"。

## Goals / Non-Goals

**Goals:**

- **消灭竞态窗（真缺陷①）**：通报注入时机挂到「rebuild/投影就绪」信号之后（`build_agent.go` ownsPersistentState 分支收尾即注入点），替代固定 +5s sleep——保证「通报必达且不早于恢复完成」：重启后首个 LLM 调用（含处理泄漏批/看板的关键窗口）之前，agent 已知道自己在转世现场。**不是修位置**：尾部注入现状已正确（实证），本项修复的是「迟到」。
- **source 独立化（真缺陷②）**："meditation" → "reincarnation"，消除 lineage 混用；main.go 分发对 source="reincarnation" 显式定义行为（静默消化 + 日志），建立「注入成功」审计口径。
- 既有转世连续性行为（rebuild/fallback/orphan 场景）零回归。
- 重启前后前缀不变量保持（现状已满足，修复后**不得破坏**——验收新增逐字节对照）。

**Non-Goals:**

- **不动注入位置**：尾部注入是现状且正确（三探针实证），本计划不做任何位置变更；也不做 role 视角的位置论证（D0 原则）。
- 不变更 WAL 事件溯源、投影 fold 语义、system 头冻结机制、meditation 注入路径本身。
- 不修 `system_alert.go`（同构缺陷：固定 5s sleep + 复用 meditation source，`system_alert.go:45`）——已登记为后续独立修复项，本计划保持增量。
- 不引入框架级通用「启动就绪」事件总线新原语；仅在必要处最小侵入暴露 rebuild 完成信号。
- 不改变压缩摘要卡（[context_compress]）与恢复投影头部系统注入（feedback/governance）——均为设计内行为，取证已确认其非缺陷。

## Decisions

### D1 注入时机：挂 rebuild 完成信号，替代固定 sleep（目标重述）

**决定**：通报注入改挂 `RebuildProjectionFromWAL` 完成信号之后——`build_agent.go` `ownsPersistentState()` 分支收尾处即注入点（rebuild + task registry 重建 + orphan 裁决完成之后）。框架侧提供最小信号面（回调 / 就绪标志 / channel 任选其一，实现时按最小侵入定夺），example 侧等待信号后再走 detect → compose → inject 流程。

**目标改述（对照初始版）**：初始版以「消灭头部注入、保公共前缀」为目标；实证表明头部注入不存在、前缀从未被破坏——D1 的真实价值是**消灭竞态窗**：保证通报必达且不早于恢复完成，使重启后首个 LLM 调用即携带转世现场。位置正确（尾部）在修复后作为**回归不变量**保留（不得因等待信号反而引入位置异常）。

**为什么（机制级，非 role 视角）**：注入时刻晚于 rebuild 完成 ⟹ StoreEvent 写入序晚于全部恢复事件 ⟹ projection.Append 落尾 ⟹ 重启后首个 LLM 调用消费事件循环时通报事件已在队首可见。因果链全程与消息的 role 字段无关（D0），仅由事件写入序决定时序与位置。

**备选方案与否决理由**：

- *备选 A：保留 sleep 但拉长（如 30s/60s）*——仍是时长猜测，与重放耗时无因果序，长 WAL 下照样迟到，仅推迟症状。否决。
- *备选 B：example 侧轮询 `MemStore()` 事件数稳定 N 秒才算就绪*——启发式，两个采样点之间重放仍在进行时误判；且消费投影内部状态破坏分层。否决。
- *备选 C：通报注入挪进 `New()` 内 rebuild 之后同步执行*——注入发生在事件循环 StartLoop 之前，bus 尚未消费，Publish 只是排队；且 detect/compose 需要 `ta.MemStore()`，把 example 专属逻辑焊进框架 build 流程，污染框架。否决——框架只提供**信号**，example 持有注入逻辑。

### D2 source 独立化："reincarnation"

**决定**：`InjectMessageWithSource("reincarnation", model.Message{Role: model.RoleUser, ...})`。

**为什么**：source 是消费端分派键（trigger_source 贯穿事件链，fail-closed gate 与分批/静默门禁按它判定）；复用 "meditation" 使通报事件 lineage 归入冥想通道，且 fail-closed 落地后通报的相关输出经 `case "meditation"` 静默消化——虽不投用户（可接受），但无注入成功审计、事件溯源记录不可区分。独立 source 后：

- 分发行为显式定义（见 D3），建立审计口径；
- 事件溯源记录中通报事件的 lineage 清晰（source=reincarnation, role=user）。

**备选与否决**："system_alert" 语义不符；"user" 会误触发 meditation novelty gate（`inject.go` `armMeditationNoveltyGate` 仅对 source=="user" 生效）。"reincarnation" 无此副作用。

### D3 main.go 分发显式 case

**决定**：main.go 事件分发 switch 新增 `case "reincarnation"`：静默消化（不回投用户，通报内容是给 agent 的现场交接，不是给用户的消息），日志记录 `[reincarnation] notice consumed by event loop`——该日志即「注入成功 + 被消费」的审计口径。

**为什么**：显式处理而非落 default 静默吞掉；静默消化 + 日志是三者（投递/静默消化/落 default）中唯一既不打扰用户又不引入投递内容脱敏复杂度的选择。若后续需要用户可感知，可增量加投递（须过 T-G 敏感字段门禁）。

### D4/D5（不变，沿用）

通报 one-shot + consume-marker（REINCARNATION_NOTICE → .notified rename）语义保持；重复通报可接受（现有日志已声明）。

## Risks / Trade-offs

- [长 WAL 下通报等待时间变长（原 5s → 重放实际耗时）] → 这是正确行为：等待期 LLM 无转世现场信息，但早注入同样无法被尚未启动的事件循环消费——信号等待把「随机迟到」变为「确定性等待」，不再有竞态。日志记录等待时长供观测。
- [框架信号面侵入框架代码] → 最小侵入：复用 build_agent ownsPersistentState 分支收尾的明确边界，信号只暴露「完成」事实不携带数据；测试覆盖信号时序。
- [system_alert.go 同构缺陷残留] → 本计划不动它，design 明确记录；后续独立修复项跟进（避免本 PR 混入两个机制变更）。
- [并发重启窗口（重启脚本在 build 期间再次触发重启）] → 通报 one-shot + consume-marker 语义不变，重复通报可接受（既有语义）。
- [事件循环尚未消费 notice 期间进程再次死亡] → notice 事件已 StoreEvent 进 WAL（事件溯源持久），下次重启重放可见；consume-marker 已 rename 前死亡 → 重复通报，可接受（既有语义）。
- [与 b31f4bd（fail-closed gate）的部署顺序耦合] → 见 Migration：两者合批部署，避免「gate 已部署而通报仍用 meditation source」的中间态长期存在。

## Migration Plan

1. 框架侧：build_agent.go ownsPersistentState 分支收尾暴露 rebuild 完成信号（最小侵入）。
2. example 侧：reincarnation_notice.go 移除 `time.Sleep(delay)` → 等待信号；source 改 "reincarnation"；main.go:205 调用点同步调整；main.go 分发新增显式 case。
3. 测试：reincarnation_notice_test.go 扩展（信号等待注入、长 WAL 场景尾部注入、短 WAL 无回归、source 独立、分发 case 锁定）。
4. **部署（修订）**：与 Plan A 的 b31f4bd（fail-closed 主修复 + b287f8b 测试）**合批滚动重启**——一次换装同时带上 fail-closed gate 与通报修复，消除「通报被 case "meditation" 静默消化」的隐性依赖中间态。已确认 b31f4bd 已提交未部署（main.go:418 gate 代码 + main_gate_test.go 存在），agent 包 16s 回归 + wechat-bot 全量 ok（主 Agent 报账）。
5. 回滚：单 PR 整体 revert 即可——无数据迁移、无事件 schema 变更、无配置变更（纯机制修复，事件流向后兼容：旧事件 source=meditation 的通报在回滚后重放不受影响）。

## Open Questions

无阻塞项。实现时按最小侵入在「回调 / 就绪标志 / channel」三种信号载体中择一（不改变 specs 与任务分解，属实现自由度）。

# 转世运行预期（Reincarnation Runtime Expectations）

> 本文档由 tasks 3.1–3.4 产出，与 design.md 分工：design.md 保留缺陷根因与修法，
> 本文档承载"转世后应该恢复什么"的**运行预期**与**降级设计**。
> 行号基准：修复 commit `4aedeef` / `d438666` 后的工作区（2026-09-13 换装前）。

## 1. 三类状态 × 预期行为 × 降级策略 × 可观测证据（tasks 3.1）

| 状态 | 恢复机制 | 预期行为（Happy path） | 降级/失效路径 | 可观测证据（日志/接口） |
|---|---|---|---|---|
| **A. 任务看板**（TaskManager registry） | `RebuildTaskRegistryFromWAL`：事实链 spawn−settle 全量回放，running→suspect（build_agent.go L546-571） | 上一世所有非终态任务恢复为 suspect；被跟踪会话提升 running；command/subagent/generic 各按闭包工厂重建 Spec | **无任务事件 → no-op**（空板，符合预期）；Declarative 缺失 → generic 展示卡片（不可 Resume） | 启动日志 `rebuild-task-registry` 计数；`/healthz` 后首轮 `List()` 板内容 |
| **B. 记忆投影**（projection） | `rebuildProjectionFromWAL`（projection_rebuild.go L34）：以最新 compaction 快照为锚 + 尾部重放 | 上一世已折叠过 → 快照 refs + 尾事件逐字节复原，prefix-cache 可复用 | **无 compaction 事件 → no-op → 空投影**（L46-49）【本文档 §3 fallback 设计目标】；投影非空时 WARN 跳过（L40-44，防 Replace-over-live） | 启动日志 `[rebuild-projection]` 一行：rebuilt key=N refs=M tail=K boundary=B 或 no-op |
| **C. 转世通报**（reincarnation notice） | 直读 WAL 尾部 N 条事件 + 元数据（独立于 projection；wechat-bot-reincarnation-notice change） | 上一世断点前 12 条事件注入新会话，带元数据 | WAL 尾部读失败 → 通报为空，不阻塞启动 | 通报注入条数；`logs/` 中转世事件计数 |

**总结论**：三类状态中 B 是唯一"无锚点即静默空"的路径——A 与 C 天然有 no-op/空结果自洽语义，B 的 no-op 语义是"**没有过去的记忆**"，agent 感知层面等于失忆。设计缺口在 B。

## 2. 记忆投影：无 compaction 快照的 fallback 重建设计（tasks 3.2）

### 现状（实证）

`projection_rebuild.go` L46-49：

```go
snapKey := cm.latestCompactionKey()
if snapKey == 0 {
    log.Infof("[rebuild-projection] no compaction event in fact chain, rebuild no-op")
    return
}
```

后果：上一世从未触发折叠（短会话/低频 agent）→ 新世纪空投影 → agent 无记忆。

### 设计

**分支改写**：`snapKey == 0` 不再 return，改走 fallback 路径：

1. **锚点**：`afterKey = 0`（WAL 头）——复用现成的 `fetchTailEvents(0)`（L136 起，分页取全 + key 升序）。
2. **过滤**：与尾部重放同款五类排除（L96-105）：`TypeContextCompressSummary` / `TypeTaskSpawned` / `TypeResidentSession` / `legacySnapshotMetaKey` / `task_inline_record`。
3. **预算 cap**：全量重放可能超上下文预算。加 `fallbackMaxRefs`（默认 500 条，与 `tailPageSize` 同量级）：
   - 若 WAL 总条数 ≤ cap → 全量重放，`fullBoundary = 0`；
   - 若超 cap → 只保留**最近 cap 条**（按 EventKey 降序截断），`fullBoundary` 置截断点（首个保留 key），并记 `log.Warnf` 标注截断。
4. **Role 派生**：同款 `EventTypeRole` 规则（KNOWN DEVIATION 已在 fetchTailEvents 注释中文档化，可复用）。
5. **日志**：成功行改为区分两种模式：`[rebuild-projection] rebuilt from snapshot key=N refs=M tail=K boundary=B` vs `[rebuild-projection] fallback rebuild (no snapshot): refs=M (of WAL total=T, truncated=%v) boundary=B`。

### 边界与风险

- **与 spill 重放互斥**：入口处已有 `projection.Len() > 0 → WARN skip`（L40-44），fallback 同样受此守卫，无新风险。
- **首次启动（真无历史）**：WAL 为空 → `fetchTailEvents` 返回 nil → 投影仍空，日志记 `fallback rebuild: empty WAL`，与现状行为一致。
- **prefix-cache**：fallback 投影与上一世运行期投影非逐字节一致（截断时）——可接受，fallback 本就是降级路径。
- **验收（spec 场景）**：`event-sourced-projection` spec 增补 fallback 场景：
  - Scenario: 无 compaction 事件且 WAL 非空 → 投影非空，含最近 N 条过滤后事件；
  - Scenario: WAL 为空 → 投影空，无 panic；
  - Scenario: WAL 条数超 cap → 截断至 cap，boundary 置于截断点。

## 3. 任务看板：nil-probe 任务的回收策略（tasks 3.3）

### 现状（实证）

两处判据让 nil-probe（`Spec.Alive == nil`，plan/subagent 类）任务在转世后永挂看板：

1. `reconcileZombies`（task_manager.go L712-715）：`need := t.Spec.Alive != nil && (running|suspect) && age ≥ zombieGrace` —— Alive==nil 直接出局，永不被 zombie 裁决。
2. `isTerminalExpired`（L202-215）：suspect 属活态 → 恒 false → `pruneTerminal` 永不回收。

加上 `Spawn` 的 byKey 去重：**活态任务按 Key 挡 re-spawn**（`pruneTerminal` 只在 terminal expired 后删 byKey），永挂 suspect = 同名任务永久无法再 spawn。实测 5 个 suspect plan 任务挂 1h41m。

### 设计

**双通道回收**：

**通道 1（启动期一次性裁决）**：在 `RebuildTaskRegistryFromWAL` 之后、重挂提升（build_agent.go L555-571）之前插入"上一世孤儿裁决"：
- 判据：`status == suspect && Spec.Alive == nil && Spec.Declarative != nil && !IsTrackedSession(Declarative.TaskID) && now − StartedAt ≥ reincarnationOrphanGrace`（建议默认 10min，与 zombieGrace 同量级）；
- 动作：走 terminal failed 路径（`t.status = TaskFailed` + `settledAt = now` + onSettle 一次），reason 文案 `(reincarnation orphan: nil-probe suspect untracked beyond grace - retired at rebuild)`；
- 效果：settledAt 置上后 `isTerminalExpired` 恢复正常运转，terminalTTL（2min）后 pruneTerminal 自然回收，byKey 释放，re-spawn 解禁。

**通道 2（运行期兜底）**：`reconcileZombies` 的候选判据增加一条：`t.Spec.Alive == nil && t.Spec.Declarative != nil && IsTrackedSession 未跟踪 && age ≥ reincarnationOrphanGrace`（复用通道 1 的同款判据，运行期持续生效）。

> 注意：**通道 1 优先**——它把孤儿挡在“重挂提升”之前，避免提升 running 后再被通道 2 裁决的抖动。通道 2 仅在重建后新产生的孤儿（如 subagent 会话被销毁后未 settle）兜底。

### 边界与风险

- **误杀活 subagent**：判据同时要求 `Alive == nil` **且** 未被 `IsTrackedSession` 跟踪——被跟踪的会话（活 monitor）永不被裁决。复用 build_agent.go L562-571 已有的跟踪信号，零新基建。
- **spec 更新**：`async-task-execution` 或新增 `task-registry-rebuild` spec 增补场景：
  - Scenario: 重建后 nil-probe suspect 未被跟踪且超 grace → 被 retire 为 failed，pruneTerminal 回收，byKey 释放；
  - Scenario: 被跟踪会话 → 不裁决（保持 suspect→running 提升）；
  - Scenario: Declarative 为空的 generic 任务 → 不裁决（展示保留）。

## 4. 验收标准与演练脚本（tasks 3.4）

### 验收矩阵

| # | 验收项 | 判定 | 证据 |
|---|---|---|--- |
| V1 | panic 消失 | 换装后 ≥5 轮含工具调用的消息处理，日志 grep 无 `panic`/`nil pointer`/`detector` 栈 | 日志时间窗切片（restart 起）+ 每轮工具调用返回摘要 |
| V2 | plan 工具恢复 | 换装后 plan 工具调用 ≥1 次返回规划结果（非 400） | 调用返回摘要（本 change 的报账即实测） |
| V3 | 二进制携带修复 | build_sha `f882854a5aae`、size 55283368、进程启动晚于 mtime、RESTART OK 日志、healthz ok | md5/ls/ps/restart.log/healthz 输出 |
| V4 | fallback 重建 | 无快照环境起进程 → 投影非空（refs>0）；WAL 空 → 投影空无 panic | `[rebuild-projection]` 日志行 + spec 场景测试 |
| V5 | nil-probe 回收 | 重建后 suspect nil-probe 任务 ≤ grace+TTL 内消失（retired failed→pruned），byKey 释放 | List() 板内容前后对比 + re-spawn 同名任务成功 |
| V6 | 转世通报 | 转世后注入条数 >0（路径 C 不回退） | 通报注入计数日志 |

### 演练脚本（下次转世演练复用）

1. **预置**：确认当前进程健康（healthz ok）+ `git log --oneline -3` 含修复 commit。
2. **制造场景**：
   - 场景 i（无快照 fallback）：选一个未触发折叠的 partition（或新 partition）+ 写入若干事件；
   - 场景 ii（nil-probe 孤儿）：spawn 一个 plan/subagent 任务且不 settle，挂过 grace。
3. **换装**：热替换二进制（同 2026-09-13 流程：build → md5 对比 → restart → RESTART OK）。
4. **观测**：
   - restart 起日志切片，查 `[rebuild-projection]`（fallback 或 snapshot 模式行）、`rebuild-task-registry`、`retired` 关键字；
   - 连续 ≥5 轮消息+工具调用，确认无 panic（V1）；
   - 调 plan 工具一次（V2）；
   - List() 看板确认孤儿已回收（V5）。
5. **留证**：日志切片 + healthz 输出 + 板前后对比存 `logs/reincarnation-drill-<date>.log`。

### 与既有 spec 的关系

- `task-registry-rebuild`（本 change 新增）：吸收 V1（nil detector panic 防护，已实现）+ V5 孤儿回收场景（待实现通道 1/2 后补 spec delta）。
- `event-sourced-projection`：吸收 V4 fallback 场景（待实现后补 spec delta）。
- `async-task-execution`：V5 的 byKey 释放语义不变（pruneTerminal 既有行为），无需 delta。

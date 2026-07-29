# Design: meditation-gate-split

## Context

冥想门控现状是单锚点三闸门：`lastEventTime`（输出侧，经 `makeOnEventCallback` 按 `trigger_source != "meditation"` 过滤更新）同时服务空闲闸门与新颖性闸门，外加"触发即重置"补丁（meditation.go L157-159）。失效路径已在探索阶段确证：冥想 turn 经任务层派生的工作以 `Source="task"` settle（`newTaskSettledEvent`，event_bus.go L124），其 final 被判定为真实活动 → 锚点前移 + 新颖性闸门重新武装 → 无用户参与的冥想永动机。次生缺陷：`extractTriggerSource` 取批次首个 external_input 的 Source 给整 turn 打标，混合批次导致双向误标（冥想内容标 `task` 污染锚点 / 用户任务结果标 `meditation` 被消费者丢弃）。

约束：不引入溯源管道（病毒式维护义务，已在探索阶段否决方案 A）；不改变 `trigger_source` 输出标注机制（消费者路由依赖）；不改变任务层 `Origin` 的 courier 定位。

## Goals / Non-Goals

**Goals:**
- 杀死冥想永动机：血统污染从"必须防住"降级为"无害延迟"。
- 净减复杂度：删除输出侧血统过滤、触发即重置补丁、双义时间戳；每个状态变量单一语义。
- 修复混合批次的双向误标（含"用户任务结果被冥想标吞掉"）。

**Non-Goals:**
- 不做输出侧血统追踪（Origin 打标 / settle 事件 Source 改写）。
- 不改变冥想 prompt、digest、★ 卡片沉淀行为。
- 不支持"用户任务 settle 单独武装新颖性闸门"（已确认接受：反思延后到下次用户互动）。

## Decisions

### D1. 双闸门拆分：按"真相便宜的位置"放置锚点

- **空闲闸门**：`lastTurnEnd`，任何 turn 结束**无条件**更新，血统无关。语义 = "agent 现在忙不忙"。冥想衍生 turn 更新它只会**推迟**冥想（后台在 churn 时本就不该冥想），无法**再武装**。
- **新颖性闸门**：`lastUserInput`，仅在注入点 `source == "user"` 时更新。注入点的 Source 是 ground truth，零传播成本，不可能被任务层洗白。
- 触发条件：`now - lastTurnEnd ≥ MinGap && lastUserInput > lastMeditation && lastUserInput != 0`。
- 备选（已否决）：方案 A 溯源管道——每条未来事件路径都须记得带标，黑名单覆盖开放集合，必然重蹈静默失效。

### D2. 锚点更新位置

- `lastUserInput`：更新逻辑收敛到 `InjectMessageWithSource` 与 `InjectMessageWithMetadata` 两个注入口（判断 `source == "user"`）。不放在 bus/loop 层——注入口是 source 语义的定义处。
- `lastTurnEnd`：更新在 `runEventLoop` 的 RunFlow 返回处（含错误返回与重试耗尽后），而非 `makeOnEventCallback`。理由：(a) "turn 结束"语义精确，覆盖无 final 输出的异常 turn（否则失败 turn 不刷新空闲锚点，冥想可能紧贴失败 turn 触发）；(b) MeditationManager 与事件回调解耦，回调回归纯投递职责。async ack turn 的 RunFlow 返回同样算 turn 结束（ack 也是一次 turn）。
- `MeditationManager` 暴露 `UpdateLastUserInput(t)` 与 `UpdateLastTurnEnd(t)` 两个原子方法，替换现 `UpdateLastEventTime`；调用点判空（`meditationMgr != nil`）沿用现有模式。

### D3. 删除项（减法清单）

1. `makeOnEventCallback` 的 meditation 排除分支（session.go L246-249）整体删除——回调不再触碰 MeditationManager。
2. `checkAndMeditate` 的触发即重置（L157-159）删除：触发后 `lastMeditation = now > lastUserInput`，新颖性闸门立即自锁，下一 tick 必被拦截，reset 冗余。
3. meditation.go L126-130 / L157-158 的过时与矛盾注释一并重写为双闸门语义。
4. `lastEventTime` 字段与 `UpdateLastEventTime`/`LastEventTime` 方法移除。**注意**：digest 渲染（`renderSelfStateDigest` 的 idle 参数）当前由 `checkAndMeditate` 的 `gap` 传入，改用 `now - lastTurnEnd`，语义不变。

### D4. 混合批次防御：冥想事件"只输不赢"

`runEventLoop` 在 `BuildInvocation` 前扫描批次：若同批存在 `Source == "meditation"` 与任何非 meditation 的 external_input，**移除冥想事件**并记 info 日志。理由：冥想事件与他事件同批 ⇔ Pull 时 agent 已非空闲 ⇔ 冥想的宪法前提（不打扰活动）已破。被丢弃的冥想不补偿注入：`lastMeditation` 已在 fire 时记录，本轮视为消耗；新颖性闸门在后续真实空闲时自然重新评估。纯冥想批次不受影响。
- 备选（已否决）：批次按血统拆成两个 turn——引入 turn 排序与 metadata 归属问题，复杂度不成比例。

### D5. 测试策略

- 重写 `TestOnEventCallback_MeditationFinalDoesNotResetIdleAnchor` → 断言回调不再影响冥想状态（或直接删除，被下述用例取代）。
- 新增永动机回归测试：`lastUserInput` 停在 T0 → 冥想触发 → 模拟 turn 结束更新 `lastTurnEnd`（模拟 task settle turn）→ 断言 MinGap 后冥想**不再**触发。
- 新增混合批次用例：`[task_settled, meditation]` 与 `[meditation, user]` 两个方向，断言冥想事件被移除、剩余事件 turn 血统正确。
- 保留并适配现有 `SkipsWithoutNewActivity`/`FiresWhenGapMet` 等用例到新 API。

## Risks / Trade-offs

- [用户任务 settle 不再武装新颖性闸门] → 已确认接受的语义取舍；素材仍在 MemoryStore，下次用户互动后的冥想可反思。若未来需要，可在注入口对 `source=="task"` 且 Origin 含 `chat_id`（用户衍生）的事件同步更新 `lastUserInput`——该扩展点与本设计正交。
- [失败 turn 也刷新空闲锚点] → 有意为之：RunFlow 连续失败重试期间 agent 事实上在忙，冥想不应插入；且失败 turn 后紧跟冥想会放大错误上下文。
- [丢弃冥想事件造成"本轮冥想凭空消失"] → 语义上等价于"冥想让位于活动"，且 debug 日志可追溯；`lastMeditation` 已推进不会导致频繁重 fire。
- [并发] → 两个新时间戳沿用 `atomic.Int64`；注入口与 loop goroutine 的并发写各自独立字段，无新增锁需求。

## Migration Plan

单 PR 落地，纯 `agent/` 包内改动，无配置/数据迁移；`go test ./agent/... -race` + 全量测试为门禁。回滚 = git revert。

## Open Questions

（无——探索阶段已消解全部开放点：语义取舍已拍板、锚点位置已定、混合批次策略已选丢弃式。）

# Proposal: meditation-gate-split

## Why

冥想的防自触发机制失效：冥想 turn 经异步任务层派生的工作（exec/子 agent）以 `task` 血统 settle，其 final 输出重置空闲锚点并重新武装新颖性闸门，形成"冥想 → spawn 任务 → task_settled → 再冥想"的无用户参与永动机，持续污染投影并空耗 LLM 调用。根因是单一时间戳 `lastEventTime` 同时服务空闲闸门与新颖性闸门，迫使输出侧回答"这个 turn 是否冥想衍生"——而输出侧血统经任务层必然丢失（`newTaskSettledEvent` 硬编码 `Source="task"`，`Origin` 行李不含 trigger_source）。

修复采取**减法**而非溯源管道：按"真相便宜的位置"拆分两道闸门——空闲闸门血统无关（任何 turn 结束都算忙），新颖性闸门锚定输入侧（注入点的 `source=="user"` 是零成本 ground truth）。血统污染从"必须防住"降级为"无害延迟"，并删除输出侧血统过滤与触发即重置补丁。

## What Changes

- `MeditationManager`：`lastEventTime`（双重语义）拆分为 `lastTurnEnd`（空闲闸门，任何 turn 结束无条件更新）与 `lastUserInput`（新颖性闸门，仅 `source=="user"` 注入时更新）。
- 触发条件改为：`now - lastTurnEnd ≥ MinGap` **且** `lastUserInput > lastMeditation`。
- **删除** `makeOnEventCallback` 中 `trigger_source != "meditation"` 的锚点排除分支（空闲锚点不再关心血统）。
- **删除** `checkAndMeditate` 的"触发即重置"（`lastEventTime.Store(now)`）——触发后 `lastMeditation > lastUserInput`，新颖性闸门立即自锁，reset 冗余；同时消除 meditation.go L157-158 与 L129 互相矛盾的注释。
- 混合批次防御：`Pull` 批次中冥想事件与任何非冥想 external_input 共存时，**丢弃冥想事件**（空闲前提已破；冥想不打扰活动是其宪法），消除整 turn 血统误标——既防冥想内容被标为 `task` 污染锚点，也防用户任务结果被标为 `meditation` 而被消费者丢弃。
- 输入侧锚点更新挂在 `InjectMessageWithSource` / `InjectMessageWithMetadata`；turn 结束锚点更新挂在 `runEventLoop` 的 RunFlow 返回处。
- 测试：替换现有锚点排除测试为输入侧断言；新增"冥想衍生任务 settle 后不再武装新颖性闸门"（永动机回归测试）与"混合批次丢弃冥想事件"用例。

不改变：`trigger_source` 在输出事件上的标注机制（消费者路由仍依赖它）；冥想 prompt、digest、★ 卡片沉淀；任务层 `Origin` 行李语义（保持 courier 定位）。

## Capabilities

### New Capabilities
- `meditation-idle-gating`: 冥想触发门控——双闸门语义（血统无关的空闲闸门 + 输入侧锚定的新颖性闸门）、无输出侧血统依赖的不变量、混合批次冥想事件丢弃、永动机防护。

### Modified Capabilities

（无。`meditation-self-state-digest` 的"空闲时长基于 agent 输出锚定"在新锚点语义下依然成立——turn 结束即 final 输出时刻，冥想输出也是 agent 输出，字面语义反而更贴合。）

## Impact

- **代码**：`agent/meditation.go`（状态拆分、闸门逻辑、注释修正）、`agent/session.go`（makeOnEventCallback 删过滤分支）、`agent/inject.go`（输入侧锚点更新）、`agent/event_loop.go`（turn 结束锚点 + 混合批次丢弃）。均在 `agent/` 包内，不触及 task 层、memory 层、框架接口。
- **测试**：`agent/meditation_test.go` 部分用例重写；新增回归用例。
- **行为取舍（已确认接受）**：用户发起的长任务在用户最后一条消息之后 settle，其结果不再单独武装新颖性闸门——冥想对该结果的反思延后到下一次用户互动之后（素材不丢失，事件仍在 MemoryStore）。
- **回滚**：纯 agent 包内改动，git revert 即可，无数据迁移。

## Why

tagent 的定位是朝 L4（长时间无人监管自主）演进。真正的门槛不是"会做事"，而是**长程自主下的自我觉察**——知道自己此刻处于什么状态、有没有卡住、有没有积累隐患。

当前 `MeditationManager` 只是定时（`interval` 默认 30m + `MinGap` 默认 2h 空闲判定，锚定 agent 输出）注入一段**静态/热加载的 prompt**。这是定期"提示"，不是"自省"：LLM 被要求反思，却**没有任何关于自身运行状态的材料**可依据，只能空谈。

连续的运行时自省（脱离报告式）目前仍太难；**先从"定时自省"入手**——让既有的冥想节拍携带一份自我状态快照，是通向运行时自省、成本最低、见效最快的第一步。

## What Changes

- 冥想事件在注入既有 prompt **之前**，前置一份**确定性生成的"自我状态 digest"**，供 LLM 基于真实运行态反思（清理、卡死任务处置、技能沉淀等）。
- digest 至少覆盖：
  - **任务层健康**（只读 `TaskController`）：按状态计数（active/running/stable/alive-detached/suspect/dead/failed），以及疑似卡死（`suspect`/`dead`）与近期任务的简摘。
  - **空闲时长**：距最近一次 agent 输出的间隔。
  - 可选：近期事件/记忆规模等轻量统计。
- digest 与任务看板同理：**确定性渲染、不参与压缩、有界**（上限截断）。
- **保持不变**：`interval` / `MinGap` 节拍、热加载 prompt、"锚定 agent 输出"的空闲判定。
- **优雅降级**：无 `TaskController`（未接任务层）时 digest 段为空/省略，冥想行为与现状一致。

## Capabilities

### New Capabilities
- `meditation-self-state-digest`: 定时冥想事件携带的、确定性生成的自我状态 digest（任务层健康 + 空闲时长 + 可选轻量统计），作为 LLM 定时自省的依据。

### Modified Capabilities
<!-- 无 requirement 级变更：task-registry-and-board 仅被只读消费，不改其要求；冥想此前无独立 spec。 -->

## Impact

- **代码**：`agent/meditation.go`（`MeditationManager` 前置 digest 渲染）、`agent/agent.go`（装配时把只读 `TaskController` 传入 MeditationManager）。可能新增 `agent/meditation_digest.go` 承载确定性渲染。
- **只读依赖**：复用 `TaskController.List()`（已存在），不改任务层本身。
- **行为**：冥想消息体变长（多一段 digest）；无任务层时无变化。
- **非目标**：连续运行时自省 / 脱离报告；自适应冥想节拍；digest 持久化。均为后续。

## Context

`MeditationManager`（`agent/meditation.go`）用一个 ticker（`interval`，默认 30m）周期检查：若距最近 agent 输出的空闲时长 ≥ `MinGap`（默认 2h），则注入一条 `[meditation]` `external_input`（内容为静态/热加载 prompt）。任务层 `TaskManager` 实现 `TaskController`（含 `List() []*Task`），已在 `agent.go` 装配为 `cm.taskController`。冥想此前无独立 spec。

## Goals / Non-Goals

**Goals:**
- 冥想事件携带一份**确定性生成**的自我状态 digest，让 LLM 基于真实运行态自省。
- digest 覆盖任务层健康（按状态计数 + suspect/dead + 近期任务简摘）与空闲时长。
- 复用既有节拍与空闲判定；无任务层时优雅降级。

**Non-Goals:**
- 连续运行时自省 / 脱离报告（后续）。
- 自适应冥想节拍、digest 持久化、LLM 生成 digest（digest 必须确定性、零 LLM、零阻塞）。

## Decisions

- **D1 数据源 = 只读 `TaskController`**：`MeditationManager` 新增可选 `TaskController` 依赖（构造期注入，可为 nil）。digest 只调 `List()`，不改任务层。与看板 `renderTaskBoard` 一致的只读消费模式，避免耦合。
- **D2 确定性渲染，独立函数**：新增 `renderSelfStateDigest(tasks []*Task, idle time.Duration) string`（放 `agent/meditation_digest.go`），纯函数、可单测、有界（任务明细上限 N，超出计数汇总）。不引入新状态。
- **D3 组装顺序**：`buildMeditationMessage` 产出 `[meditation] 头 + \n\n + <digest> + \n\n + <prompt>`。digest 在 prompt 之前——先给"我现在的状态"，再给"该如何反思"。
- **D4 空闲时长复用现有时钟**：idle = `now - lastEventTime`（已锚定 agent 输出），不新增计时。
- **D5 降级**：`TaskController == nil` 或 `List()` 为空 → 省略 digest 段（或渲染"无活跃任务"一行），冥想行为与现状等价。
- **D6 健康分类直接复用 `TaskStatus`**：不新增状态枚举；suspect/dead/failed 归为"需关注"，单列简摘（desc + 状态 + 年龄）。

## Risks / Trade-offs

- **消息变长**：digest 占用上下文预算。缓解：有界渲染 + 只在真正 meditate 时构建（非每次 check）。
- **快照瞬时性**：digest 是构建时刻的快照，可能与 LLM 读取时刻略有偏差。可接受——自省是趋势判断，非精确对账。
- **只读并发**：`List()` 在后台任务并发变更时读取；`TaskManager` 已自带锁，返回快照切片，无需额外同步。
- **"自省"仍是提示驱动**：本变更给了材料，但反思质量仍依赖 prompt 与模型。这是定时自省的固有边界，符合"第一步"定位。

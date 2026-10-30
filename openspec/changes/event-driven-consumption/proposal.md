# Event-driven consumption — 事件决定有什么，消费者决定怎么处理，turn 记录哪些上下文已完成

## Why

用户的原始症状（原话）：

- 「同一会话里，用户补充与相关任务结果，即使来源 InvocationID 不同，也允许进入同一次模型请求。」
- 「上一轮模型已返回、工具还在后台运行时，如果用户提出新要求，模型消费者可以处理新要求，不必等待上一轮全部工具完成。」
- 「对 A 的回答可以闭合已完成部分，B 留在下一段继续消费。」

当前实现做不到这三点，原因不是缺一把锁，而是**每次输入都要启动并占有一次独立执行**：

- `Run.Invoke` / `InjectEvent` 提交输入后立即进入 `Loop.Drive`（`assembly/invoke.go`、`assembly/run.go`），
- 多个 drive 进入同一会话后，`Host` 用地址门与 `acceptInflight` 把新输入「转写」给在途 range（`assembly/host.go`），`boundaryInput` 再按该身份过滤（`runtime/agent/loop.go`），
- `dispatch` 先派发全部工具、再逐个等待结果（`runtime/agent/loop.go`），慢工具把快结果与新输入一起挡住，
- 段切分只按链序遇到 `agent_output` 闭合（`memory/replay/age_segments.go`），无法表达「A 已闭合、B 尚未消费」。

prototype（`56c58eb1`）已经表达过正确形态并有断言：循环取批、模型产出回队列、工具结果回队列、慢任务未放闸时新输入仍得到回答。

## What Changes

1. **消费循环**：一次模型消费只看「已到达的事件」；工具调用派发后不整轮等待，结果作为后续批次的输入；同会话跨来源合批。
2. **入口统一**：提交即受理；同会话由同一会话消费者推进；不再把来源身份改写成在途 range，也不再以「谁在跑」决定「是否受理」。
3. **分段闭合**：段边界由实际消费关系判定；回答只闭合它实际处理过的输入；消费关系可回放，重启后段可重建。
4. **派生读面**：rl 导出、工具清单、环境块、capture 视图读同一次消费的事实快照。
5. **宿主迁移**：wechat-bot 与 cli-host 适配新入口语义，端到端症状锚落在宿主路径上。

## Capabilities

### Modified（既有能力扩展，不新增并行能力）

- `persistent-event-loop`：消费语义由「每输入一次 drive」改为「会话持续消费」（批次、工具解耦、跨来源合批、冻结批次）。
- `config-state-twinship`：受理与占用语义——提交即受理、占用不拒绝受理、不移写来源身份。
- `task-skeleton-compression`：段边界判据由纯链序扩展为「链序 + 实际消费关系」。
- `offline-training-export`：样本配对判据由 `InvocationID` 改为「输出消费键 ↔ 输入键」。

### New

- `host-migration`：示例宿主适配新入口语义；既有 specs 无宿主迁移能力（`cross-session-delivery` 管跨会话投递、`resident-session-continuity` 管会话续存，均不覆盖宿主判读），故新增。

## Impact

- 代码：`runtime/agent`（loop/dispatch/boundaryInput）、`assembly`（invoke/host/run/facts）、`memory/replay`（分段）、`features/rl`（导出）、`examples/*`（宿主）。
- 契约：`Invocation`/`Turn` 的角色重新表述；`Host.Invoke` 的同步返回语义不变（提交 + 等待结果）。
- 风险：消费语义属核心路径，必须红锚先行、全量门禁与独立审者放行。

## Non-Goals

- 不把压缩与预算事件化并入本变更（`compression-and-budget-as-events` 并行推进）。
- 不新增持久层 schema 迁移；消费关系的承载复用既有事实与决策记录。

# 05-inspection — 统一查询与观测（二级子变更 proposal）

> 边界：本目录 md 为本域**规划产物**（`planned` / `design-blocked`）。本轮**无产品实现授权**：不写产品代码/测试、不跑 build/test、不安装依赖、不提交、不归档。
> 语义来源：只读计划 `运行时_重设_实施计划_304e54c3.md` §4.2/§4.5/§7（05 行）。CLI 模板取自 `openspec instructions {proposal,design,specs,tasks} --change runtime-rebuild --json`。

## Why

运行时必须能**只读地**解释：有哪些 agent/session/任务/通知/租约在跑、每个阻塞由谁推进、以及观测（OTel/审计）**不拥有状态**。清理后没有该读面，诊断/SDK/看板无从落地。

## What Changes

- 提供 agent/session/invocation/task/notice/租约的**只读运行总图**；统计**从明细派生**，不把多维状态相加成"运行数"。
- 授权过滤、**不可变快照**、稳定分页、详情及订阅 **gap/reset**；游标绑定 revision 与过滤条件。
- 提交后插件接 OTel/审计；SDK 尝试观察**分层**；**关闭 OTel 不影响 Query**。
- 在重启/取消/退役/存储失败下**解释每条阻塞及下一推进者**。

## Capabilities

### New Capabilities

- `runtime-inspection`：**只读运行总图**（inventory/snapshot/cursor/subscription/详情）——现有主规格无对应能力，登记理由见下。

### Modified Capabilities

- `turn-tracing`：关闭 OTel 时 Query 能力不减（观测为旁路）。
- `trajectory-trace-correlation`：轨迹与 trace 关联在 Query 读面上可解释。
- `self-telemetry-audit`：自管遥测占比与分级动作**只在观测面**，不改状态。
- `committed-event-attribution`：Query 报告源水位与滞后、**仅报告已提交事实**。
- `degradation-behaviors`：退化/阻塞的可解释性与"下一推进者"。

### 新能力 `runtime-inspection` 非重合论证（对既有 registry specs）

| 既有能力 | 关注点 | 与 `runtime-inspection` 的区别 |
|---|---|---|
| `runtime-resource-ownership` | store/engine **租约与关闭**（所有权/释放） | 本能力只**只读呈现**资源持有者，不做获取/释放 |
| `task-registry-and-board` | 任务 registry 与**每 turn 看板投影**（写者语义） | 本能力是**跨维度只读总图 + 授权/快照/游标/订阅**，不渲染看板、不改 registry |
| `self-telemetry-audit` | 自管遥测**占比指标**与冻结动作 | 本能力是**通用只读查询面**，不产出审计动作 |
| `event-timeline-rendering` | 事件时间线**呈现** | 本能力是运行时**状态/归属/阻塞**视图，非事件渲染 |

结论：现有能力均不覆盖"授权过滤的只读运行总图 + 不可变快照/游标/订阅"，故新增 `runtime-inspection` 而非同义聚合。

## Impact

- 未来候选写域（未存在亦未获批）：Query 产品包与 `tests/integration/inspection/**`。
- 上游依赖：01-state-binding（session/mailbox/消费位置）、02-execution（任务/通知）、03-history（提交/水位）、04-capabilities（拒绝账本/阻塞源）。
- 非目标：Query **不是**统一大事务或万能 Store；不跨所有后端做原子快照；不修改 GrantRead 等授权事实。

## 边界与依赖

- **上游签名/实际实现**：`go.opentelemetry.io/otel*`（v1.38.0，`go.mod` 直接依赖）；Query 只读消费 01/02/03/04 的既有事实。签名**未经探针不称冻结**。
- **下游消费者**：SDK、CLI、诊断、看板（08-host）；训练/评估诊断。
- **禁止事项**：不改授权事实；不做跨 store 原子快照；不把多维状态相加成"运行数"；观测（OTel/审计）失败不回滚事实、不拥有状态；删除/同状态字段变化均可解释（订阅不得静默漏报）。

## 待核（不设假默认）

- 快照 revision 的**水位来源**（各 store 事实水位如何形成一致 revision）需设计闸审定（05.2）。
- 订阅 gap/reset 的**具名语义**与溢出阈值（05.2/05.4）。
- OTel 关闭下的可用性边界（哪些维度仍可查）需在 05.3 明示。
- 资源欠账见 `design.md`「资源与待核」。

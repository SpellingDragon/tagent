# 设计（静态决策与子域）

## Context

消费语义要同时满足三条已裁定边界（跨来源合批、慢工具不阻塞新消费、回答只闭合已处理部分）。当前实现把「输入提交」「模型执行」「上下文段」绑在同一个 `Drive` 上，于是需要门闩、在途转写与整轮等待来协调。

目标形态：**事件决定有什么要处理；消费者决定怎么处理；turn 描述哪些上下文已完成。**

- `Invocation`/`Turn` 保留为「一次模型消费及其产出」的关联身份（rl、审计、交付都用它），但不再代表「一次输入独占一次执行」。
- 会话消费者是每会话的推进者：取批 → 消费 → 继续；同会话单消费者，跨会话并行。
- 段（turn）由「实际消费关系」闭合，不只看链序。

## D1 消费批次语义

- 一次模型消费的输入 = 该会话当前「已到达且未被此前消费取走」的事件批次，含：外部输入、任务结果（`action_command`）、通知材料；不再要求它们同属一个 `InvocationID`。
- 「已到达」= 已落链且在本轮取批时刻可见。批量处理**不是**收齐等待：取到多少处理多少，慢工具不进入本批。
- 工具调用在派发后即离开本批；其结果作为未来批次的事件，不再作为本轮的同步 `observation` 阻塞下一次消费。
- 保留「一次模型请求只看一个固定批次」：请求构造后不再追加，保证 prompt 前缀可缓存、可重放。

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | 消费循环 | `event-consumer-loop` | 批次取用、工具解耦、跨来源合批；不整轮等待 | 无（`runtime/agent` 内部） |
| 02 | 会话入口 | `session-intake` | 提交即受理；同会话单消费者推进；不移写来源身份 | 01 的批次接口 |
| 03 | 分段闭合 | `turn-segmentation` | 段边界按实际消费关系判定；可回放重建 | 01 产出的消费关系 |
| 04 | 派生读面 | `derived-face-consistency` | rl 导出/工具清单/环境块/capture 读同一消费快照 | 01、03 |
| 05 | 宿主迁移 | `host-migration` | wechat-bot 与 cli-host 适配新入口；端到端症状锚 | 02 |

## D3 波次表（汇报分组；解锁=依赖已入 main）

| 波 | 内容 | 解锁条件 |
|---|---|---|
| W1 | 01 红锚 + 消费循环（含跨来源合批、工具解耦、批次边界） | 立案即准入 |
| W2 | 02 会话入口（提交即受理、单消费者推进、门闩语义移除） | 01 入 main |
| W3 | 03 分段闭合（消费关系落链、段判据、回滚重建） | 01 入 main（消费关系产出可用） |
| W4 | 04 派生读面（rl 导出、工具/环境/capture 同快照） | 01、03 入 main |
| W5 | 05 宿主迁移 + 收尾 F | 02 入 main |

W3 与 W2 可并行（触碰集不交）。

## D4 合并规则与共享面

- 文件簇独占：`runtime/agent/loop.go|dispatch|boundaryInput` 归 01；`assembly/host.go|invoke.go|run.go` 归 02；`memory/replay/age_*|projection` 归 03；`features/rl/export_*` 归 04；`examples/*` 归 05。
- 跨域接口由编排者在 W1 前冻结并写入本文件（消费批次结构、消费关系结构、会话消费者启动签名），冻结后各域可独立派发；签名变更视为跨域返工。
- 同文件簇必须合单或串行；共享面（`assembly/facts.go`、`api/event`）默认编排者单写。

## D5 三级验收

- 孙任务：任务行 `- [ ] 编号 动词产出 + 约束 —— 验证：<可复跑命令>`，exit 0。
- 子域：`openspec validate --specs --strict` 全绿，域内 scenarios 逐条可跑。
- 变更级 DoD（全部可勾选）：
  1. 症状直译锚：慢工具未返回时第二条消息得到回答；第一条回答闭合后第二条不在已完成历史中。**回滚本变更必红，修复后必绿，两次证据入 run-notes。**
  2. 根模块 `go test ./...` 全绿；`scripts/lint.sh` exit 0；`event-short` 十轮全绿；`event-long` 六分片全绿。
  3. 契约改动清单（`Turn`/`Invocation` 文档语义、`persistent-event-loop`、`task-skeleton-compression`）逐条记录。
  4. 独立验证子代理评估：对照任务行验收面（非追认缩水），frontier 空 + 扫描范围 + 逐项证据。

## D6 关键结构（自 W1 起冻结；变更即按跨域返工处理）

```go
// 一次消费的批次：会话当前可消费且未被取走的事件，跨来源。
type ConsumptionBatch struct {
    Address event.Address
    Events  []event.Event // 含 external_input / action_command / feedback(task)
}

// 一次模型消费实际使用的输入与产出：段闭合的唯一依据，且可回放。
type ConsumptionFact struct {
    DecisionID event.DecisionID
    InvocationID event.InvocationID // 关联身份，不再承担分批
    Inputs   []event.EventID // 本批次被本次请求实际携带的输入键
    Output   event.EventID   // agent_output 事实键
}
```

`ConsumptionFact` 的承载复用既有事实（`agent_output` 的 Meta 记录输入键集合 + `DecisionRecord.InputEvents`），不新增事实族；若两者冲突，以 `agent_output` Meta 为准（链上不可变）。

## 被否方案

1. **保留 gate，仅补 release 前标志**：覆盖症状 3，但症状 1/2 仍缺；且「谁在跑决定是否受理」的越权仍在。
2. **claim 账（session 级覆盖事件键集合 + 可恢复账）**：为「事实不带归属」而生；本设计保留来源关联，不需要额外账。
3. **Occupancy 持有期扩到 turn/range**：把决策级占位变成整回合锁，等于重建 turn 独占，与症状 2 冲突。
4. **把 SettleDriver 扩成所有输入的总调度中心**：通知驱动器的职责是确认与恢复；让它兼任会话消费者等于把特殊路径变主路径。
5. **段边界继续只看链序 `agent_output`**：无法表达「A 已闭合、B 未消费」，与症状 3 冲突。

## 风险与回退

- 风险：消费循环改动跨核心路径，回归面大。缓解：红锚先行；每域独立可回滚（域内改动不跨文件簇）；W1 后先跑 event-short。
- 回退：按域回滚到基线提交；症状锚回滚后必红即为回退完成的证据。

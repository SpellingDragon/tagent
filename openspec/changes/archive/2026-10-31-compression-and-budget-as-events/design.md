# 设计

## 总纲

**事件决定有什么需要处理，消费者决定怎么处理，turn 描述哪些上下文已经完成。**

三种边界分开：

| 边界 | 含义 | 载体 |
|---|---|---|
| **批次** | 这次取到了哪些事件 | 消费循环的一次拉取 |
| **模型请求** | 这次模型实际看到了哪些上下文 | 请求快照（`Decision.Request`） |
| **turn** | 哪些已完成的输入、过程与结论组成可折叠段 | `agent_output` 闭合的投影段 |

一个 turn 可跨多个批次、多个模型请求；一个工具任务可跨多个 turn。

## 消费循环

### 每会话一个消费循环

每个 `(agent, session)` 地址一个常驻消费 goroutine。行为对齐 prototype `56c58eb1` 的 `DefaultRun`：

```
for {
    events := pull(session)         // 阻塞等第一个事件 + 非阻塞取已到达
    for each event:
        dispatch by type            // 按事件语义选择消费行为
    if hasDriving(events):          // 有需要模型回应的事件
        invokeModel()               // 一次模型调用
    // 不等工具返回，回到 pull
}
```

- **驱动事件**：用户输入、有关的任务终态结果 → 组装上下文、调用模型。
- **非驱动事件**：任务 progress、受理回执、压缩判定 → 记录或触发对应消费，不直接调用模型。
- **工具调用**：模型产出工具调用 → 派发执行器 → 立即回到 pull（不等结果）。
- **模型产出**：工具调用事件、中间叙述、最终回复均作为事件回到队列。

### 入口变更

| 入口 | 现在 | 改后 |
|---|---|---|
| `Host.Invoke` | acceptInflight 或 driveLocked → Run.Invoke → Loop.Drive | 提交事实 → 唤醒消费循环 → 等待本次回答的 agent_output 或返回受理收据 |
| `InjectEvent` | commitFact → Loop.Drive | commitFact → 唤醒消费循环 |
| `fireNoticeTurn` | CommitToSession → driveLocked | CommitToSession → 唤醒消费循环 |

### A/B 闭合

模型产出不含工具调用的最终回复 → 立即 commit `agent_output` 事实 → 该事实闭合当前 turn 的上下文段。

后续到达的输入（B）属于下一段，在下一次消费批次中处理。**事实链顺序不改写**；投影中的段边界由消费关系决定，而非链上位置。

### 工具等待解除

dispatch 派发全部调用后**立即返回**，不等任何结果。结果以事件回到队列，消费循环在下一次拉取时读到。

需要的配对（工具调用→结果）通过事实上的 `CallID` 关联，不需要内存中的 await 通道。

### 冥想让路

冥想判据②改读消费循环的运行状态（「该会话是否有一个模型请求正在执行」），不再读 Occupancy 的独立占席表。消费循环本身知道它是否正在调用模型。

## 事件

### compress.product

- `Request CompressRequest.ID`：产出它的判定身份（一条 request 至多一条 product）。
- `Key EventID`：产物在投影中的负键（内容寻址 `-hist-<hex>` / `-seg-<hex>`）。
- `Kind string`：`segment_fold` / `history_summary`（已实现；键前缀承载）。
- `Covered []EventID`：被折叠覆盖的条目键，按原序。
- `Text string`：产物正文。
- `Producer string`：执行压缩的机制名。
- `Answers ProductAnswers{Request, Producer}`：合并字段（已实现）。
- `ID EventID`：内容寻址（request + producer + key），重复提交同一条。

### final_round.request

- `Range event.InvocationID`：判定的 range。
- `Reason string`：`rounds` / `budget`。
- `Rounds int`、`Elapsed time.Duration`：判定输入。
- `ID EventID`：内容寻址（range + reason），一个 range 至多一条。

## 生产与消费

- **压缩**：`AgeFoldPlanner`（PostCommit 观察者）评估体积与龄段，只提交 `compress.request`；`CompressionConsumer`（PostCommit 观察者）收到 `compress.request` 后读材料、执行折叠、提交 `compress.product`。
- **预算**：`FinalRoundPlanner`（PostCommit 观察者）按 `FinalRoundPolicy` 提交 `final_round.request`；消费循环在模型调用前读本会话的 request，读到则本次调用为收尾轮。

## 恢复

`seedReplay` 读到 `compress.product` 时把产物作为合成条目加入投影，不再调用 `FoldByAge` 重折。

消费循环重启：从事实链读出「最后一条 agent_output」，其后的输入为待处理材料。

## 兼容

- `compress.request` 契约不变。
- 未接 consumer 的宿主：判定照旧提交，产物不生成。
- `Run.Invoke` 保留为测试与直接驱动的入口（不经消费循环），但生产入口走消费循环。

## 应收敛的结构

| 结构 | 处置 |
|---|---|
| `addressGate` 受理判定（acceptInflight + running() 复核） | 删除 |
| `addressGate` 驱动锁（token + hold/release） | 删除（消费循环自身即唯一驱动者） |
| `Loop.Drive` 的多轮等待循环 | 保留结构但行为变更：不等工具、每轮读新事件、最终回复立即 commit |
| `dispatch` 内逐个 `await` | 删除；工具结果以事件回流 |
| `boundaryInput` 按 InvocationID 过滤 | 改为按消费循环的拉取位置过滤 |
| `acceptInflight` 把新输入写在途 range | 删除；输入事实保留自己的来源身份 |
| `Occupancy` 竞争协调 | 冥想让路改读消费循环状态；查询面读消费循环 |
| `markRangeHead` / `behindRangeHead` | 改为消费循环的拉取位置标记 |
| `recordMapped` 的 `rangeInvocation` / `endedAt` | 改为投影边界的推进规则（`agent_output` 闭合段） |

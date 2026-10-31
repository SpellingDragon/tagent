# 消费循环收敛 + 压缩与预算事件化

## Why

三个问题同源：tagent 的原始设计是「事件管线生产事实，消费逻辑批量拉取并处理」，但当前实现让每条输入直接启动一个独立 `Loop.Drive`，再用 gate/Occupancy 协调这些 drive。由此产生了：

1. **消费被工具等待挡住**：dispatch 先派发全部调用再逐个 await，快结果要等慢结果，用户补充要等全部工具返回。
2. **来源身份被用作消费隔离**：`acceptInflight` 把新输入写成在途 invocation，`boundaryInput` 按相同 invocation 过滤——生产者必须先知道消费者是谁。
3. **turn 闭合与执行生命周期绑死**：对 A 的回答落链要等整个 Drive 返回，不能闭合已完成部分而把 B 留给下一段。
4. **压缩与收尾轮内嵌**：`AgeFoldPlugin` 自己判自己折，产物不落链；`closingRound` 在 loop 内纯内存判定。

这些都不是事件驱动本身的必要结构，而是「多个入口都能启动 Drive」的生长物。

## What Changes

### 消费循环收敛（B1/B2/B3）

- **同会话合批**（B1）：同一 session 的消费循环拉取当前已到达的事件（用户补充、任务结果、其他），即使来源 InvocationID 不同也允许进入同一次模型请求，保留各自来源关联；确实要求独立上下文的工作显式隔离。
- **慢工具不阻塞新消费**（B2）：工具派发后消费循环继续；用户提出新要求时模型消费者可以处理，不必等待上一轮全部工具完成。
- **A/B 闭合**（B3）：对 A 的回答可以闭合已完成部分（commit agent_output），B 留在下一段继续消费；不要求等所有新输入都处理完才闭合。

### 压缩三段式（C1）

- producer 只判定并提交 `compress.request`；新增 `compress.product` 事实承载产物；独立 consumer 读 request 执行折叠并提交 product；投影从 product 事实恢复。

### 预算耗尽事件化（C2）

- 新增 `final_round.request` 事实；判定由插件在每次提交后产出；loop 在决策边界读该事实决定本轮是否收尾。

## Impact

- 删除 gate 的受理判定（acceptInflight）与 inflight 字段；gate 的驱动锁角色随消费循环统一而消失。
- Occupancy 的竞争协调职责消失；冥想让路改读消费循环的运行状态。
- `Host.Invoke` 变为提交事实 + 等待结果（或返回受理收据），不直接启动 Drive。
- `Loop.Drive` 的「等待全部工具返回」行为解除；消费循环持续拉取。
- turn 保留上下文分段与压缩的组织意义（agent_output 闭合段），但不承担执行排他性。
- fold 产物身份落链后稳定，smoke 断言恢复严格。

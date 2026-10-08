# O1 事实提交与因果可见性

## Why

当前 MemoryPlugin 的投影受 stored 闸保护，而 StateDelta 票据及 lastEventKeys 更新不受同一闸保护。同因果域的读父与提交分离，还需要并发回归确认线性化边界。本域修复事实已提交与消费者可见性的分歧，不新增持久因果管理器。

## What Changes

- 成功提交后发布票据与推进游标；失败不虚构可召回事件，保留原错误/正文传播及凭据降级。
- 同 `(partition,session)` 的提交链线性化，不以全局锁串行所有 session。
- 统一回溯的 partial 原因；保留已有 Complete/Capped 字段，不把缺键说成空历史。

## Capabilities
- 新增：`committed-event-attribution`（本目录 specs）；事件不可变和 StoreEvent 屏障契约不变。

## 边界与依赖
- 父变更：`docs-objective-review`，第三阶段；依赖父设计 D12–D20 的接口常数。
- 实现依赖：O1.6 等 O4.5 关系 WAL 发布回归通过；O1.1–O1.5 可先行。
- 被依赖方：O5.5 只在提交成功点关联 fact-link；O6 消费 partial。
- 接口面：既有 StateDelta 票据、回溯结果的可选 reason；无新事件格式迁移。
- 禁止：重写原文、猜测丢失原因、把 transient spill 当已提交、改 TTL、读取其他 agent 私有任务域。

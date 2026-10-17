## MODIFIED Requirements

### Requirement: task_settled 作为一等事件触发回收 turn

任务信号 SHALL 作为一等输入进入统一事件管线。`settle_wake=decision` 的已提交结果 SHALL 唤醒所属调用或持久循环，当前turn执行中只排队不打断；`observe` SHALL 提交原事实和精确input_disposition后消费，不单独运行模型。混合批次 MUST 保留每条输入，不以durable/volatile组成推断整批已提交。恢复和重复消费 SHALL 通过原key、partition和处置凭据判定，不凭正文或同名ID猜测。

#### Scenario: 纯观察不触发模型
- **WHEN** 循环只收到producer标为observe的任务观察
- **THEN** 原事实、投影和处置被提交，循环继续等待decision输入

#### Scenario: 用户和结果混批
- **WHEN** 用户输入与任务decision或observe同批
- **THEN** 全部事实独立持久化，只有decision输入组成当前模型请求，观察保留可查询证据

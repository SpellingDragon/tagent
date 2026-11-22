## ADDED Requirements

### Requirement: 每个可恢复消费者只有一份状态派生

每个可恢复消费者 SHALL 以一个有类型状态应用规则解释其已提交事实；在线提交成功后与恢复回放 SHALL 调用同一规则，MUST NOT 为恢复另立业务解释。时钟、唤醒、观测与外部副作用 SHALL 留在状态应用之外，状态等价比较 SHALL NOT 包含未持久化的进程内时间。

#### Scenario: 在线与恢复对照

- **WHEN** 同一提交序列分别经在线提交与重启恢复得到消费者状态
- **THEN** 状态、修订、轮次、义务与收据 SHALL 逐项一致
- **AND** 进程内时间戳 SHALL 按“未知/运行态”语义单列，不参与逐字段相等断言

#### Scenario: 恢复不执行副作用

- **WHEN** 一个消费者在恢复期回放事实并推导出待办
- **THEN** 恢复 SHALL NOT 发送、结算、折叠或驱动冥想等外部动作
- **AND** 待办 SHALL 在恢复完成后由拥有者继续推进

### Requirement: 切面覆盖自述且缺片不按空态启动

每个 checkpoint 切片 SHALL 自述其覆盖的事实范围；框架 SHALL 按切片判定覆盖，MUST NOT 用投影或另一消费者的进度代替。无切片、切片缺失、切片损坏与新消费者首次接入 SHALL 分别具名处理；缺片 SHALL 读取该消费者所需历史或具名拒绝，MUST NOT 以空状态启动。

#### Scenario: 旧切面没有新消费者切片

- **WHEN** 最近 checkpoint 不含某已挂载消费者的切片
- **THEN** 该消费者 SHALL 从其所需历史重建或具名拒绝 boot
- **AND** 重启后其未决工作 SHALL 与原事实一致

#### Scenario: 切片损坏

- **WHEN** 切片载荷不可读
- **THEN** boot SHALL 具名拒绝，MUST NOT 静默按空状态继续

### Requirement: 恢复前置关系由拥有者声明

消费者之间的恢复顺序依赖 SHALL 由拥有者的装配关系或类型化声明表达；协调者 MUST NOT 解析具体消费者的业务载荷来决定顺序。确需先行的账户 SHALL 在切片恢复前完成初始化，并由至少一个正式入口场景验证。

#### Scenario: 邮箱依赖任务账户

- **WHEN** 恢复顺序要求任务账户先于欠账通知投递
- **THEN** 该依赖 SHALL 由装配关系表达并有正式入口证据
- **AND** 新增消费者 SHALL NOT 要求修改协调者的业务分支

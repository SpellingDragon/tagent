# checkpoint-state（框架统一 checkpoint）

## ADDED Requirements

### Requirement: 框架统一 checkpoint 事实族

框架 SHALL 以单一 checkpoint 事实族承载全部可恢复状态切片（投影幸存者、宿主全局送达/结算账含开账、邮箱欠账、任务终态），并在一个分区 Seq 偏移的一致性切面聚合。压缩事实 SHALL 回归纯投影职责，仅作 checkpoint 触发源之一；MUST NOT 另立独立账本快照路线。

#### Scenario: 大窗口仍有 checkpoint 载体
- **WHEN** 会话声明大窗口（如 32768）极少触发折叠
- **THEN** 框架 SHALL 按有界节奏（周期/提交计数/close）补发 checkpoint 事实，恢复仍有载体

#### Scenario: 一致性切面
- **WHEN** checkpoint 聚合期间有结算事实并发提交
- **THEN** 快照状态 SHALL 对应 ≤偏移 的事实，晚于偏移的结算 SHALL 由回放覆盖，不重不漏

### Requirement: 状态管理委托框架

插件/消费者 SHALL 通过状态接缝注册 Snapshot/Restore，把可恢复状态委托框架管理；MUST NOT 各自实现全史恢复读路径。

#### Scenario: 消费者无 bespoke 恢复读者
- **WHEN** 新增一个持有可恢复状态的消费者
- **THEN** 它 SHALL 注册状态切片由框架 checkpoint/恢复，而非新增一条全量 durable 扫描

### Requirement: 重启成本由窗口与增量界定

重启恢复 SHALL 等于「读最近 checkpoint + 回放偏移后事实」，成本随窗口大小与偏移后增量界定，MUST NOT 随累积历史超线性增长。

#### Scenario: 多代重启 boot 成本平坦
- **WHEN** 同一存储上连续重启多代（如 121 代校准）
- **THEN** 每代 boot 恢复成本 SHALL 保持有界，不随代数二次增长

#### Scenario: 删除全史读者
- **WHEN** 恢复路径运行
- **THEN** 系统 MUST NOT 对每个 ActiveSession 全量读 durable 链（Unsettled 旧路径），送达账 MUST NOT 全史扫描

### Requirement: 义务无限期且折叠不丢债

未清送达义务 SHALL 随 checkpoint 存活（含 body/目标/lineage/attempt/hold）；折叠 MUST NOT 吃掉未履行的承诺。已结集合 SHALL 以分区 Seq 水位 + 开账清单紧凑表示，MUST NOT 用进程局部 factSeq 作全局水位。

#### Scenario: 折叠保留开账
- **WHEN** 一条 owed 输出在折叠时仍未送达
- **THEN** 该开账 SHALL 嵌入 checkpoint，重启后 SHALL 被补送，不静默丢失

#### Scenario: 已结去重不重发
- **WHEN** 重启后遇到一笔已结算身份
- **THEN** 系统 SHALL 由水位/结算态判定已送，MUST NOT 重发

### Requirement: 退役独立快照路线

RecoveryTicket 独立 KV 快照路线 SHALL 退役，由统一 checkpoint 事实族取代。

#### Scenario: 无独立快照残留
- **WHEN** 退役完成
- **THEN** 生产代码 MUST NOT 残留 WriteRecoveryTicket/fileBootRecoveryTicket/recoveryTicketWriter 调用（归档除外）
## MODIFIED Requirements

### Requirement: task_settled 为通知类 input 事件

后台任务的结算结果（task_settled)SHALL 被视为"通知"类外部输入事件：它 SHALL NOT 被视为对某次"等待中" tool 调用的协议应答（同步应答已在 spawn 的 sync-wait 窗口内以 ack/内联结果完成）；它 SHALL 作为新的驱动事件进入时间线并触发回收 turn。通知内容 SHALL 携带文本级关联标识（task id 与任务简述），使模型能在内容上将通知与先前的调用关联。

通知结果 SHALL **全量保真**：结算输出全文直接构造成事件 Content，框架 SHALL NOT 在构造时截断（含任何 `max_inline` 式上限）。全量随事件本体持久化于 MemoryStore，可经票据（evt key）召回，与任务层的 TTL 回收窗口解耦。视图有界化（摘要/卡片化）只发生在压缩管线的定级点，与其它 external_input 同权。

#### Scenario: 慢命令的应答-通知二段式

- **WHEN** 一个命令越过 dense 窗口转后台，稍后在后台 settle
- **THEN** 原调用处 SHALL 已返回 ack（含 task id)——这是该调用的同步应答
- **AND** settle 结果 SHALL 以 task_settled 通知（含同一 task id）驱动一个**新的** turn

#### Scenario: 通知携带可关联标识

- **WHEN** 渲染含 task_settled 通知的历史
- **THEN** 通知文本 SHALL 含 task id 与任务简述
- **AND** 同时间线中先前 ack 文本 SHALL 含同一 task id

#### Scenario: 大结果全量入库不截断

- **GIVEN** 一个 settle 输出远超历史 `task_settled_max_inline` 量级（如数万字符）
- **WHEN** task_settled 事件被构造并持久化
- **THEN** 事件 Content SHALL 为结果全文，SHALL NOT 含任何框架生成的截断标记或取回工具提示
- **AND** 事后凭该事件 key 召回（memory_recall）SHALL 取到全文，无论任务层 TTL 是否已过

## MODIFIED Requirements

### Requirement: task_settled 为通知类 input 事件

后台任务的结算结果（task_settled)SHALL 被视为"通知"类外部输入事件：它 SHALL NOT 被视为对某次"等待中" tool 调用的协议应答（同步应答已在 spawn 的 sync-wait 窗口内以 ack/内联结果完成）；它 SHALL 作为新的驱动事件进入时间线并触发回收 turn。通知内容 SHALL 携带文本级关联标识（task id 与任务简述），使模型能在内容上将通知与先前的调用关联。

通知结果 SHALL **有界化**（对齐同步路径的输出转储模式）：结果不超过转储阈值（与 OutputLimitTool 同公式，`MaxTokens/2×4` 字符）时全文内联；超过时全文 SHALL 转储到 workspace 的 tool-output 目录（受 Cleaner 周期清理），通知 Content SHALL 携带尾部摘录（对齐 ActionTool 的 2000 字符）与文件路径票据，事件本体 SHALL NOT 持有全文——凭票据召回该事件返回的是有界版+票据，大结果永不经召回回流上下文。全文消费 SHALL 经 `read_file(start_line, num_lines)` 行级分页。

#### Scenario: 慢命令的应答-通知二段式

- **WHEN** 一个命令越过 dense 窗口转后台，稍后在后台 settle
- **THEN** 原调用处 SHALL 已返回 ack（含 task id)——这是该调用的同步应答
- **AND** settle 结果 SHALL 以 task_settled 通知（含同一 task id）驱动一个**新的** turn

#### Scenario: 通知携带可关联标识

- **WHEN** 渲染含 task_settled 通知的历史
- **THEN** 通知文本 SHALL 含 task id 与任务简述
- **AND** 同时间线中先前 ack 文本 SHALL 含同一 task id

#### Scenario: 小结果全文内联

- **GIVEN** 一个 settle 输出低于转储阈值（如 800 字符）
- **WHEN** task_settled 事件被构造并持久化
- **THEN** 事件 Content SHALL 为结果全文，SHALL NOT 含截断标记或文件票据

#### Scenario: 大结果转储文件且事件有界

- **GIVEN** 一个 settle 输出远超转储阈值（如数万字符）
- **WHEN** task_settled 事件被构造
- **THEN** 全文 SHALL 已写入 tool-output 目录文件，通知 SHALL 含尾部摘录与文件路径票据
- **AND** 事件 Content SHALL 有界（不含全文），凭 evt key 召回 SHALL 返回有界版+票据，不复发大结果
- **AND** 模型 SHALL 可经 `read_file` 分页读取全文

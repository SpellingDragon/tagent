# async-task-execution 能力修改（delta）

## MODIFIED Requirements

### Requirement: 服务型任务转 alive-detached

**转移条件收紧为仅 service 型**：仅 `LifetimeOf(spec) == service` 的任务在首个输出稳定结算（`SettleStable`）时 SHALL 转入 alive-detached 观测态并发送一次性「就绪」通知；job 型（command/subagent）任务的 `SettleStable`/`SettleSuspect` SHALL NOT 转入 alive-detached、SHALL NOT 注入就绪通知事件（面板状态照常置 stable/suspect，终态结算照常通知）。

> **方向性标注（非本期验收）**：本需求的 alive-detached 转移与 detachedAt 持久化为**过渡态**——终态（信号发射矩阵：oneshot 不发射中间态信号、SettleStable 仅 interactive 消费）下 alive_detached 生产者归零，本需求整体随 `settle-signal-matrix` 拆除变更删除；承接者见 failure-polarity-passthrough/design.md「终态消融覆盖矩阵」。

service 型任务进入 alive-detached 观测语义时，其脱离时间戳（detachedAt）MUST 作为任务生命周期事实持久化（写入 Declarative 并随 task_spawned 链恢复）；恢复后的脱离时间 MUST 沿用原值，MUST NOT 以恢复时间替代。

#### Scenario: job 型静默任务不转就绪

- **WHEN** job 型任务（如 `sleep 180`、包安装命令）输出静默触发 `SettleStable`
- **THEN** 任务 SHALL NOT 进入 alive-detached，SHALL NOT 产生 `∞` 就绪通知；其终态结算照常通知

#### Scenario: service 型转移动机不变

- **WHEN** service 型任务（如常驻服务）首个 `SettleStable` 结算
- **THEN** 任务 SHALL 转 alive-detached 并收到一次性就绪通知（现状语义保持）

#### Scenario: detached 任务跨重启

- **WHEN** alive-detached 任务跨重启恢复
- **THEN** 恢复后的 detachedAt MUST 等于死亡前的原值，超龄观测/终止判定沿用真实时长

### Requirement: task_settled 为通知类 input 事件

后台任务的结算结果（task_settled)SHALL 被视为"通知"类外部输入事件：它 SHALL NOT 被视为对某次"等待中" tool 调用的协议应答（同步应答已在 spawn 的 sync-wait 窗口内以 ack/内联结果完成）；它 SHALL 作为新的驱动事件进入时间线并触发回收 turn。通知内容 SHALL 携带文本级关联标识（task id 与任务简述），使模型能在内容上将通知与先前的调用关联。

**失败极性表达**：命令非零退出（含信号死）、探测不可辨超限、kill 失败强拆三类路径的结算 SHALL 以失败极性呈现（✗/failed），通知文本 SHALL 携带 `exit_code=N`（判定与透传契约见 failure-polarity 能力规格）；零退出的成功极性（✓/completed）语义不变。

通知结果 SHALL **有界化**（对齐同步路径的输出转储模式）：结果不超过转储阈值（与 OutputLimitTool 同公式，`MaxTokens/2×4` 字符）时全文内联；超过时全文 SHALL 转储到 workspace 的 tool-output 目录（受 Cleaner 周期清理），通知 Content SHALL 携带尾部摘录（对齐 ActionTool 的 2000 字符）与文件路径票据，事件本体 SHALL NOT 持有全文——凭票据召回该事件返回的是有界版+票据，大结果永不经召回回流上下文。全文消费 SHALL 经 `read_file(start_line, num_lines)` 行级分页。纯空白载荷（去除首尾空白后为空且无 Err）SHALL 降级为单行票据，不投递空白正文。

#### Scenario: 慢命令的应答-通知二段式

- **WHEN** 一个命令越过 dense 窗口转后台，稍后在后台 settle
- **THEN** 原调用处 SHALL 已返回 ack（含 task id)——这是该调用的同步应答
- **AND** settle 结果 SHALL 以 task_settled 通知（含同一 task id）驱动一个**新的** turn

#### Scenario: 失败结算以失败极性通知

- **WHEN** 后台命令以非零退出码结束并结算
- **THEN** task_settled 通知 SHALL 为失败极性（✗/failed），文本 SHALL 含 `exit_code=N` 与 task id

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

#### Scenario: 空白载荷降级票据

- **GIVEN** 一个 settle 输出去除首尾空白后为空且无 Err
- **WHEN** task_settled 通知被构造
- **THEN** 通知 SHALL 为单行票据（id、简述、极性），SHALL NOT 投递空白正文

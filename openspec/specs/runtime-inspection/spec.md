# runtime-inspection Specification

## Purpose
新增能力：**只读运行总图**。提供跨维度的只读查询面——agent/session/invocation/task/notice/租约的清单、授权过滤、不可变快照、稳定分页、详情与订阅 gap/reset，以及阻塞/下一推进者解释。Query 是只读投影，**不是**统一大事务或万能 Store。非重合论证见 `proposal.md`。

## Requirements

### Requirement: 只读运行总图与派生统计

Query SHALL 只读返回 configured/resident/busy agent、session 状态、活动 decision、任务、通知阶段、资源持有者与阻塞义务；统计 SHALL **从明细派生**，MUST NOT 把多维状态相加成"运行数"。Query MUST NOT 修改 `GrantRead` 等授权事实。

#### Scenario: 总图覆盖各维度

- **WHEN** 调用运行总图接口
- **THEN** 返回含 agent/session/task/notice/租约各维度明细
- **AND** 各计数与对应明细条数一致（派生非相加）

#### Scenario: 只读不改授权（失败）

- **WHEN** 查询在授权边界执行
- **THEN** 不产生任何授权事实写入
- **AND** 越权维度被过滤而非被改写

### Requirement: 授权过滤 + 不可变快照 + 游标

返回的 Snapshot/List/Detail SHALL 共享**有界快照 revision**；游标 SHALL 绑定 revision 与过滤条件；旧窗口过期、订阅溢出、删除及同状态字段变化 SHALL 可解释；Query SHALL 报告源**水位与滞后**，MUST NOT 宣称跨所有后端原子。

#### Scenario: 稳定分页（并发）

- **WHEN** 以游标翻页期间底层新增/删除条目
- **THEN** 翻页 SHALL 基于同一 revision 的稳定序列
- **AND** 过期游标返回具名"窗口过期"错误而非静默错位

#### Scenario: 同状态字段变化可解释

- **WHEN** 某明细的同状态字段（如租约持有者）在快照间变化
- **THEN** 结果 SHALL 能解释该变化（具名/时间序）
- **AND** 不误判为新增或消失

#### Scenario: 报告水位与滞后（失败）

- **WHEN** 某源滞后于快照 revision
- **THEN** 结果携带该源的水位与滞后信息
- **AND** 不声称全局原子一致

### Requirement: 订阅 gap/reset 具名

订阅 SHALL 在多消费者下各自独立推进；当订阅溢出或断档时 SHALL 发出**具名 gap/reset**（含缺口范围/原因），MUST NOT 静默漏报。

#### Scenario: 溢出具名 reset（并发/失败）

- **WHEN** 订阅消费者落后超过溢出阈值
- **THEN** 发出具名 reset（含缺口范围），消费者可据此重新取快照
- **AND** 不静默丢失事件

#### Scenario: 取消订阅不泄漏（取消）

- **WHEN** 订阅被取消
- **THEN** 等待者被释放，无 goroutine/句柄泄漏

### Requirement: 阻塞解释与下一推进者

系统 SHALL 在重启/取消/退役/存储失败场景下，为每条**阻塞义务**给出可解释状态及**下一推进者**（推进该阻塞的执行者/通知/存储动作）。

#### Scenario: 阻塞具名给出推进者（重启）

- **WHEN** 重启后某任务的投递义务尚未结算
- **THEN** 列出该阻塞并指明下一推进者（例如等待的 notice 运输/重放）
- **AND** 不把"队列取空"当完成

#### Scenario: 存储失败可辨（失败）

- **WHEN** 某源存储不可读
- **THEN** 返回具名错误与受影响维度，而非空结果伪装正常

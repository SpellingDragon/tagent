# window-recovery Specification

## ADDED Requirements

### Requirement: 窗口界与策略分开

系统的窗口类上限 SHALL 只有 `max_tokens` 一个外部输入；其余窗口类上限 MUST 由机制派生或降为不可配常量。压缩时机与保留深度是策略，由 `compress_threshold` 与 `keep_recent_tasks` 可调；压缩策略本身 MUST NOT 因本变更改变。任何新增的窗口类可配项 SHALL 判红，除非登记为策略并说明理由。

#### Scenario: 新增上限类可配项即红

- **WHEN** 代码或配置面新增一个未登记的窗口类上限
- **THEN** 界登记门判红并指出该上限名

#### Scenario: 压缩策略不变

- **WHEN** 本变更完成后对同一输入序列回放
- **THEN** 折叠的段、产物与保留深度与变更前逐条一致

### Requirement: recall 与窗口解耦

召回与查询 SHALL 只读落盘的事件数据，MUST NOT 依赖 runtime 窗口内容；启动 MUST NOT 全量装载观察表或播种向量；语义召回 MUST NOT 在内存保留 runtime 窗口的向量状态或全史向量表，查询时只对本次候选集计算向量。

#### Scenario: 启动零装载与查询时嵌入

- **WHEN** 进程以任意长度的历史启动
- **THEN** 观察表装载条数为 0、启动与提交时的嵌入调用数为 0；只有一次召回查询才对本次授权候选的链上原文调用嵌入，结果与全量装载时逐条一致

#### Scenario: 嵌入失败不留下持久欠账

- **WHEN** 某候选的嵌入在查询中失败
- **THEN** 该查询退化为关键词腿；嵌入器恢复后同一进程的下一次查询重新嵌入候选并可按语义命中，MUST NOT 依赖重启或持久索引重建

#### Scenario: 授权先于正文读取

- **WHEN** 查询为空授权或只授权分区 11，而分区 12 的正文不可解码
- **THEN** 空授权返回空结果且不读正文，分区 11 的查询只返回 11 的事实；非法排序/分页在任何读取前具名拒绝，未授权分区的错误不得泄漏

#### Scenario: 父边按需推导且晚绑定

- **WHEN** recall 工具在 Run.Bind 前构造，且链上后提交事实携带更早时间戳
- **THEN** Bind 后的 trace/get-parent 仍按 `(PartitionID, Seq)` 返回父边，不创建或读取关系 journal，结果与直接读链一致


### Requirement: 原文正文按消息隔离

不同 EventID 的原文正文 SHALL 各自独立存储，即使字节完全相同也 MUST NOT 共用内容寻址 blob；digest 只用于该消息自身的完整性校验。同一 EventID 的多个 scope 仍指向这一条消息的正文。旧 digest 共享格式 MUST 具名拒绝，MUST NOT 被当成缺失正文。

#### Scenario: 相同内容也是两条独立事实

- **WHEN** 两个不同 EventID 提交完全相同的正文并重新打开存储
- **THEN** 两条消息各自持有独立正文且逐字节读回，原文数量为 2；任一引用的元数据不改变另一条

#### Scenario: 旧共享格式具名拒绝

- **WHEN** 持久层只有旧的 digest 共享正文而没有该 EventID 的独立正文
- **THEN** 打开返回 `ErrSnapshotCorrupt`，不静默丢弃该引用

### Requirement: 折叠锚恢复

压缩产物 SHALL 在该会话最新一条折叠的同步时刻携带完整检查点载荷：摘要正文、死亡名单（`Covered`）、按投影序的幸存者名单（含合成键到事实键的指向）与恢复使用的 seq 边界。检查点 MUST 只是可由链重建信息的紧凑表示，MUST NOT 提供任意外部状态注册载荷。重启恢复 SHALL 以该会话最新产物为锚，由锚载荷加本链中 seq 大于幸存者边界的全部事件重放重建状态；产物缺失、损坏或版本不符时 SHALL 退回全量重放并具名告警，结果 MUST 与锚路径逐条一致，且一次恢复解码的事件数 MUST NOT 与历史事件总数成正比。

#### Scenario: 锚重建与全量一致

- **WHEN** 一条长程折叠链（历史 1000 条、锚后增量 5 条）重启
- **THEN** 恢复只解码锚载荷与这 5 条增量，重建状态与全量重放逐字段一致

#### Scenario: 锚不可用退回

- **WHEN** 最新产物损坏或其载荷不可读
- **THEN** 恢复退回全量重放，结果与锚路径逐条一致，并记录具名告警

#### Scenario: 未折叠会话

- **WHEN** 一条从未触发折叠的短链重启
- **THEN** 恢复走全量重放，状态与运行期一致

### Requirement: 状态持久通道

链事件 SHALL 是语义真相，既有主人日志 SHALL 只承担机械重做；锚只是可由链重建的检查点，MUST NOT 成为第三真相或任意外部状态容器。审计 SHALL 枚举 Run/Host 全部内存态字段，优先删除冗余状态或按链重建；真正不可推导的状态 SHALL 先建模为事件或既有日志，MUST NOT 通过通用注册钩子注入。

#### Scenario: 提交序与事件时间不同

- **WHEN** 后提交的事实携带比先提交事实更早的时间戳
- **THEN** full replay 仍按 `(PartitionID, Seq)` 提交序重建，不按事件时间重排

#### Scenario: 审计红

- **WHEN** 某块状态既不落事件、不写既有日志，也不是可由链重建的派生态
- **THEN** 审计门判红并指出该状态字段

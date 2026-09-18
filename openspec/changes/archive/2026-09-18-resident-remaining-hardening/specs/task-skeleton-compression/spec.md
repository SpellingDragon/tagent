## ADDED Requirements

### Requirement: task settled 类外部输入纳入票据化折叠

压缩折叠判定 SHALL 把 content 以 `[task settled]` 前缀（或等价 Metadata 子型标记）的 external_input 事件识别为「结算通知类」，无论其所处段龄一律纳入折叠范围；多条连续结算 ref SHALL 合并为单张汇总卡片，每条一行 `✗/✓ [evt_key] 摘要行`，卡片 SHALL 携带「原文可用 recall 按 evt_key 召回」的提示。结算通知的落库语义不变（逐事件事实链记录 + registry 归并），本折叠只作用于投影呈现层。

#### Scenario: 结算风暴被折叠回收

- **WHEN** 投影中存在 50 条 `[task settled]` external_input 且压缩触发
- **THEN** 它们合并为单张汇总卡片（50 行票据），投影字符量下降 ≥80%，事实链与 registry 归并不受影响

#### Scenario: 原文可按 evt_key 召回

- **WHEN** 模型或宿主对折叠卡片中任一 `[evt_key]` 发起 recall
- **THEN** 对应原始结算事件全文可从事实链取回

### Requirement: 批量退役在源头汇总为单事件

reconcile/orphan 通道的批量退役 SHALL 在任务层聚合：per-task 的 settle 记录保持逐条 record-only 落链（registry 归并数据源不变），bus 对外只发布一条汇总 external_input（N 行票据摘要）。该回调为可选——未注册时保持逐个 settle 发布的既有行为。

#### Scenario: 批量退役不再产生事件风暴

- **WHEN** 一次 reconcile 退役 30 个孤儿任务且宿主注册了批量回调
- **THEN** 事实链新增 30 条 settle 记录（record-only），bus/投影只出现 1 条汇总 external_input

### Requirement: 浓缩导航丢失可观测

浓缩守卫的必需集（首尾/★）之外，被浓缩文本吞入散文的每张 recall 票据都是导航地址丢失。守卫 SHALL 保持浓缩的压缩权衡不因此失效，但此类消亡 MUST NOT 静默：接纳前 SHALL 计数并告警（累计值可经诊断面读取），事件本体仍按时间范围可召回的事实不受影响。

#### Scenario: 非必需票据被折入散文被计数

- **WHEN** 合法浓缩输出保留首尾/★ 票据但省略了旧半区其他票据
- **THEN** 该文本仍被接纳，但丢失票据数计入累计计数并记 Warn，诊断 getter 可读出非零值

#### Scenario: 全票据存活不告警

- **WHEN** 浓缩输出携带输入旧半区全部票据
- **THEN** 计数不增长、无告警

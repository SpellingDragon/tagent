## MODIFIED Requirements

### Requirement: 保留保护与回收协调

存储生命周期（存活键集扫描、搬迁、待搬迁标记与回收趟）SHALL 与保留租约**在同一存储生命周期内**协调：保留期内龄期到期、容量超限与内容降分辨率 MUST NOT 物理销毁所需原文——只拒销毁，无损搬迁允许。恢复清点未完成时 MUST NOT 以宽限时间放行潜在恢复材料。释放保护不等于原文立即不可读。

**原文可读性 SHALL 延续到搬迁之后**：待搬迁标记（`Tombstone`）不是物理销毁，原文 SHALL 保持可读直到搬迁完成；搬迁完成后原文 SHALL 仍**按身份可读**（冷层透明回读，`GetEvent(key)` 返回原字节），MUST NOT 因搬迁答复 missing（session-checkpoint-material/spec.md:44、memory-architecture.md:50、一级 §五 identity-read 不变量）。回收趟（`Reclaim`/`ReclaimTombstoned`，lifecycle.go:321/:76-78）在退位路径上的语义 SHALL 是**复制＋校验＋热层移除**，热层移除 SHALL 只在冷层逐字节校验通过之后发生；本能力 SHALL NOT 以任何路径删除、覆盖或改写已提交事实。

保留租约 SHALL 由**共享资源 owner** 持有（非任一 agent），SHALL 支持多持有者引用计数递减、归零才解除，MUST 幂等。保护/回收协调能力 SHALL 复用存储生命周期既有能力，端口按**真实 consumer 所需**表达。**脚手架 SHALL 接线生产**：`NewLifecycle`/`NewRetention`/`NewMemoryGuard`（lifecycle.go:143、retention.go:70、guard_memory.go:32）SHALL 有生产调用方，MUST NOT 停留在只被测试与 skeleton_assert.go:150 引用的未接线状态。

**义务租约 SHALL 是生产载体之一**：未结投递义务 SHALL 登记租约（`Hold`），结清 SHALL 释放（`Release`），接线点为结算状态注记（host_delivery.go:671-690）与热窗 pin 释放（run.go:2210）同一时机；重启后租约 SHALL 按链上结清事实与 checkpoint 切片重建，已结义务 SHALL NOT 持租约（host_delivery.go:2069 早退）。headless-retired 结清保留的审计正文（host_delivery.go:860-873）SHALL 按独立审计租约保留于存活键集。热窗 pin 豁免（run.go:2247 `keepPinnedLocked`）与租约 SHALL 同源，MUST NOT 另立第二套保护。

回收/保留协调能力缺失的处置 SHALL **分档**：**持久恢复档**启用时，若存储缺少保护/回收协调能力，系统 SHALL **具名报告能力不完整**并**拒绝启动或禁止破坏性清理**（fail-closed，`ErrCapabilityIncomplete`），MUST NOT 当作「无保护即成功」；**非耐久档**（不承诺进程恢复）SHALL 以**显式 capability 缺失**声明，不得伪造保护已生效。journal-only 之外的档位（rustviking file 档无跨键原子）SHALL NOT 实现退位搬迁，其缺失 SHALL 显式声明。

#### Scenario: 保留期内不物理销毁

- **WHEN** 一个受保留租约保护的原文到达龄期上限或超容量
- **THEN** 该原文 MUST NOT 被物理销毁（待搬迁标记可记），无损搬迁 SHALL 允许
- **AND** 受保护键的搬迁请求 SHALL 以 `ErrProtected`（lifecycle.go:95）具名拒绝

#### Scenario: 搬迁后原文仍按身份可读

- **WHEN** 一个原文完成搬迁（冷层逐字节校验通过、热层已移除）
- **THEN** 以同一身份读取 SHALL 返回原字节，MUST NOT 答复 missing
- **AND** 冷层正文读不出时 SHALL 具名拒绝（`ErrSegmentBodyMissing`），MUST NOT 静默丢事实

#### Scenario: 释放保护后不重盖时间戳

- **WHEN** 保留租约释放
- **THEN** 该 key SHALL 按其**原有时间戳**与原类型参与后续判定，MUST NOT 被重新盖时间（retention.go:36-48）
- **AND** 是否可搬迁 SHALL 由存活键集权威判定，释放本身 MUST NOT 使原文不可读

#### Scenario: 恢复清点未完成不放行

- **WHEN** 恢复清点尚未完成
- **THEN** 破坏性回收与搬迁 MUST NOT 以宽限时间放行潜在恢复材料（`ErrRecoveryIncomplete`，lifecycle.go:89）

#### Scenario: 多持有者引用计数

- **WHEN** 同一 key 被多个 owner 同时保护，其中一个释放
- **THEN** 保护 SHALL 因计数未归零而继续有效，直到最后一个持有者释放；重复释放 MUST 幂等

#### Scenario: 义务租约随结算释放

- **WHEN** 一条投递义务由未结（owed/held）转为结清（sent/refused/headless-retired，host_delivery.go:184-190）
- **THEN** 该义务租约 SHALL 在结算状态注记处释放，热窗 pin 同时解除
- **AND** 重启后已结义务 SHALL NOT 重新持有租约，未结义务 SHALL 按链重建租约
- **AND** headless-retired 保留的审计正文 SHALL 另按独立审计租约留在存活键集，MUST NOT 随义务租约释放而被搬迁

#### Scenario: 脚手架接线生产

- **WHEN** 持久恢复档启用并打开存储
- **THEN** 生命周期与保留租约 SHALL 由生产装配构造并接线（非仅测试引用），回收判据 SHALL 消费存活键集权威

#### Scenario: 持久恢复档缺能力 fail-closed

- **WHEN** 启用持久恢复档而存储缺少保护/回收协调能力
- **THEN** 系统 SHALL 具名报告能力不完整并拒绝启动或禁止破坏性清理（fail-closed），MUST NOT 当作「无保护即成功」处理

#### Scenario: 非耐久档显式 capability

- **WHEN** 使用不承诺进程恢复的存储档位，或使用无跨键原子的 file 档
- **THEN** 其保护/搬迁能力缺失 SHALL 以显式 capability 声明呈现，MUST NOT 伪造保护已生效

### Requirement: TTL check uses actual event timestamp

TTL 时间戳读取 SHALL 解析事件正文的 Timestamp 字段作为**原时间戳**（现载体为 `Lifecycle.Scan` 的 `recordedAt`/`cfg.Window`，lifecycle.go:238-245），MUST NOT 以段窗口时间戳近似替代，MUST NOT 重新盖时间。原时间戳 SHALL 用于报告与类型归类。

**回收判据 SHALL 是链可达，不是龄期**：搬迁判定 SHALL 消费单一存活键集权威 `live = checkpoint.Survivors ∪ {ref : ref.seq > Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)`（一级 D6.3/§五），搬其补集；龄期到期与容量超限 MUST NOT 单独触发搬迁或销毁，MUST NOT 另立第二套回收 oracle。仍在存活键集内的材料 SHALL NOT 因龄期被搬迁。

正文不可解析 SHALL 具名拒绝，MUST NOT 被当作「可搬迁」或「可跳过」处理——静默跳过等于静默丢事实（一级 D7）。

#### Scenario: 龄期到期但仍在存活键集

- **WHEN** 一条事实的龄期已超过配置窗口，但它仍被最新 checkpoint 点名、或仍有未结义务、或仍在尾
- **THEN** 该事实 SHALL NOT 被搬迁，MUST NOT 因龄期到期而离开热层

#### Scenario: 链不可达即可搬迁不看龄期

- **WHEN** 一条事实不被最新 checkpoint 点名、无未结义务、且非尾，而其龄期尚未超过配置窗口
- **THEN** 该事实 SHALL 可被搬迁（判据是链可达，不是龄期）

#### Scenario: 原时间戳原样保留

- **WHEN** 一条事实被登记保护后释放，或被搬迁到冷层
- **THEN** 其原时间戳 SHALL 原样保留，MUST NOT 被重新盖时间或以窗口时间戳近似替代

#### Scenario: 正文不可解析具名拒绝

- **WHEN** 某键的正文无法解析
- **THEN** 扫描 SHALL 具名拒绝该键并停止以其为依据的搬迁，MUST NOT 静默跳过或当作可搬迁
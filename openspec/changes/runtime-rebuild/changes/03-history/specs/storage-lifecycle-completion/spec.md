# storage-lifecycle-completion delta（03-history）

> 复用登记（非规范，不改本条语义）：保留保护与回收协调在同一存储生命周期内；引用计数、先登记后淘汰、无损搬迁允许。
> 本 delta 只新增「保留保护与回收协调」；本能力其余 Requirement（L1 to L2 compaction is automatically scheduled、L2 to L3 compaction is automatically scheduled、filterTombstoned removes tombstoned events、TTL check uses actual event timestamp）由主 spec `openspec/specs/storage-lifecycle-completion/spec.md` 继承（**证据映射，不改**）。

## ADDED Requirements

### Requirement: 保留保护与回收协调

存储生命周期（TTL 扫描、容量淘汰、压实清理）SHALL 与保留租约**在同一存储生命周期内**协调：保留期内 TTL 过期、容量淘汰、内容降分辨率与墓碑最终清理 MUST NOT 物理销毁所需原文——只拒销毁，无损搬迁（压实复制原文到高层段）允许。恢复清点未完成时 MUST NOT 以宽限时间放行潜在恢复材料。释放保护不等于原文立即不可读。

保留租约 SHALL 由**共享资源 owner** 持有（非任一 agent），SHALL 支持多持有者引用计数递减、归零才解除，MUST 幂等。保护/回收协调能力 SHALL 复用存储生命周期既有能力，端口按**真实 consumer 所需**表达，具体名称/签名**不在此冻结**（DESIGN-GATE(G1)），不另立旧方法清单。遗忘扫描的首轮破坏性动作 MUST 等待租约武装与登记屏障解除。

回收/保留协调能力缺失的处置 SHALL **分档**：**持久恢复档**启用时，若存储缺少保护/回收协调能力，系统 SHALL **具名报告能力不完整**并**拒绝启动或禁止破坏性清理**（fail-closed），MUST NOT 当作「无保护即成功」；**非耐久档**（不承诺进程恢复）SHALL 以**显式 capability 缺失**声明，不得伪造保护已生效。

#### Scenario: 保留期内不物理销毁

- **WHEN** 一个受保留租约保护的原文到达 TTL 或超容量
- **THEN** 该原文 MUST NOT 被物理销毁（墓碑可标记），无损搬迁 SHALL 允许

#### Scenario: 释放保护后按原时间戳复归类

- **WHEN** 保留租约释放
- **THEN** 该 key SHALL 按其**原有时间戳**、原类型的年龄窗口恢复参与淘汰，MUST NOT 被重新盖时间

#### Scenario: 恢复清点未完成不放行

- **WHEN** 恢复清点尚未完成
- **THEN** 破坏性回收 MUST NOT 以宽限时间放行潜在恢复材料

#### Scenario: 多持有者引用计数

- **WHEN** 同一 key 被多个 owner 同时保护，其中一个释放
- **THEN** 保护 SHALL 因计数未归零而继续有效，直到最后一个持有者释放；重复释放 MUST 幂等

#### Scenario: 持久恢复档缺能力 fail-closed

- **WHEN** 启用持久恢复档而存储缺少保护/回收协调能力
- **THEN** 系统 SHALL 具名报告能力不完整并拒绝启动或禁止破坏性清理（fail-closed），MUST NOT 当作「无保护即成功」处理

#### Scenario: 非耐久档显式 capability

- **WHEN** 使用不承诺进程恢复的存储档位
- **THEN** 其保护/恢复能力缺失 SHALL 以显式 capability 声明呈现，MUST NOT 伪造保护已生效

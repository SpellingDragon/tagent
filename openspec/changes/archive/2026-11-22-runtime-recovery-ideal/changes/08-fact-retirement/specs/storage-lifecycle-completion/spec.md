> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../../../CARRYOVER.md)逐项接收，不因转历史而取消。

## REMOVED Requirements

### Requirement: TTL check uses actual event timestamp

**Reason**: 该需求的执行面（TTL 扫描 / 墓碑标记 / 压实清理）已按「零遗留」裁定整体移除，`LifecycleManager.checkTTL` 与其 `Lifecycle.Scan` 载体在生产树中不存在，保留即死条款。

**Migration**: 时间真相不受影响——「原时间戳是唯一语义时间轴、不得以窗口时间戳近似」由主规格 `event-segment-store` 的「时间真相源单一」承接；原文过期由原文仓的召回窗口承接（见下一条）。

## MODIFIED Requirements

### Requirement: 保留保护与回收协调

**任何路径 MUST NOT 物理销毁已提交事实**：TTL 扫描、容量淘汰、墓碑标记与物理回收 MUST NOT 作为运行期销毁路径存在——只拒销毁，不得以「先标墓碑、压实再删」的形态复活。

**已提交事实的可读性 SHALL NOT 因任何有界化手段下降**：按身份读一条已提交事实 SHALL 恒返回其原字节与原索引位置，SHALL NOT 答 missing；任何路径 MUST NOT 删除、覆盖或改写已提交事实。热层字节的增长 SHALL 显式声明，MUST NOT 被声称为本要求已达成的有界。

**两条保护语义 SHALL 各由唯一活载体承接，MUST NOT 另立第二层**：未结投递义务的结算事实 SHALL 由热窗 pin 承接——pin 随义务未结登记、随结清解除，重启按链与 checkpoint 重建，已结义务 MUST NOT 重新持有 pin；原文过期 SHALL 由原文仓按召回窗口判定，窗口外的读取 SHALL 具名拒绝。MUST NOT 引入与这两条载体并行的保留租约账本、守卫计数或生命周期执行器，也 MUST NOT 留成「已定义、零调用、语义与现行判据冲突」的第三态。

**能力诚实 SHALL 落在打开档位的声明上（fail-closed）**：不承诺跨进程恢复的档位 SHALL 自报不持久，装配面 SHALL 具名告警；MUST NOT 把「无保护／无恢复面」当作成功，MUST NOT 伪造保护或恢复已生效。

#### Scenario: 已提交事实按身份恒可读

- **WHEN** 按身份读一条已提交事实
- **THEN** 以同一身份读取 SHALL 返回原字节与原索引位置，MUST NOT 答复 missing
- **AND** 正文读不出时 SHALL 具名拒绝（`ErrSegmentBodyMissing`），MUST NOT 静默丢事实

#### Scenario: 销毁路径不存在

- **WHEN** 审查提交后事实的任何后台动作（折叠、压缩、装配、开档）
- **THEN** 每个动作 SHALL 只改变可见性，MUST NOT 删除、覆盖或改写已提交事实
- **AND** MUST NOT 存在以 TTL 到期、容量超限或墓碑清理为条件的物理销毁调用点

#### Scenario: 未结义务的原文不被窗口裁掉

- **WHEN** 一条投递义务仍处未结（owed/held）状态
- **THEN** 其结算事实 SHALL 因 pin 留在热窗内，窗口裁剪 MUST NOT 把它移出
- **AND** 义务结清后 SHALL 解除 pin，该事实从此按普通窗口老化

#### Scenario: 重启后按身份重建 pin

- **WHEN** 进程重启
- **THEN** 未结义务的 pin SHALL 按链上结算事实与 checkpoint 切片重建
- **AND** 已结义务 MUST NOT 重新持有 pin

#### Scenario: 原文窗口外具名拒绝

- **WHEN** 一条原文的引用全部落在其召回窗口之外
- **THEN** 读取 SHALL 具名 `RecallExpired`／`ErrOriginalExpired`，MUST NOT 答复空召回或伪造命中

#### Scenario: 不承诺恢复的档位自报不持久

- **WHEN** 打开一个不承诺跨进程恢复的存储档位
- **THEN** 其 `StoreCapabilities.Durable` SHALL 为 false，装配面 SHALL 具名告警，MUST NOT 当作已具备恢复面

#### Scenario: 已移除的保护机制不得以零调用形态复活

- **WHEN** 审查 storage lifecycle 面
- **THEN** 生命周期、保留租约与内存守卫三个构造 SHALL 全部不存在（无定义、无引用），MUST NOT 以「在册未接线」形态重新引入
- **AND** 新增保护能力 SHALL 先具名它并入的那一条现行判据，MUST NOT 另立第二真相

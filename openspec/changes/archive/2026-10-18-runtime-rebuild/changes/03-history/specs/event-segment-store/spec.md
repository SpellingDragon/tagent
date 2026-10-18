# event-segment-store delta（03-history）

> 复用登记（非规范，不改本条语义）：LocalFileKV 分片原子快照为提交屏障；保留租约由共享资源 owner 持有、引用计数。
> 本 delta 修改「LocalFileKV Sync 为原子快照屏障」「未确认恢复材料的有限保留租约」，并**新 MODIFIED**「恢复能力经过包装层真实透传」（移除对旧 5 方法签名的冻结，能力改按真实消费者所需端口表达、名称不冻结；持久恢复档缺能力 fail-closed）；本能力其余 Requirement（Store event as JSON Lines segment by time window、seqCounter recovered from existing segment data、seq 槽位与事件身份一一对应、resolvePartitions 默认隔离、eventCount 在进程生命期内反映实际事件数、QueryEvents 召回正确性、时间过滤毫秒契约与窗口剪枝换算、窗口剪枝跨度按段层级取值、段边界可信性、早停判据以实际事件时间戳为基准、时间真相源单一、跨窗查询按 EventKey 去重、双实现查询语义一致与全序确定性、FileSegmentStore supports graceful shutdown via Close、后端不可变与隔离一致性、存储失败不可冒充正常缺失、compaction 源窗口读失败必须中止本轮、TTL 扫描读失败必须可观测、ReplayEvent 窗口重入与 StoreEvent 同做 sealed 降级）由主 spec `openspec/specs/event-segment-store/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: LocalFileKV Sync 为原子快照屏障

LocalFileKV 的 `Sync()` SHALL 将全部键值整序列化并以临时文件写入加原子 rename 落盘；快照 SHALL 按分区分片（每分区独立文件），`Sync()` 只重写自上次屏障以来变更过的分区——单次提交的屏障成本 SHALL 与其触碰的分区成正比，MUST NOT 与全库键数成正比。`Sync()` 返回成功后，独立新进程 SHALL 能读回该快照代表的全部键值。快照文件本体损坏 SHALL 启动失败（fail-fast）。FileSegmentStore.StoreEvent 的提交屏障契约不变：evt/idx/必需 meta 写完且 `Sync()` 成功后才发布缓存、计数与成功结果；屏障未完成 MUST NOT 报告 durable 成功，调用方 MUST 在成功后才投影。

后端档位 SHALL 各自**单独证明**实际屏障，MUST NOT 假设批量 API 天然原子。内存档（`InMemoryStore`）SHALL 复用同一业务合同（提交/隔离/去重语义一致），但**不承诺进程恢复**，MUST NOT 在重启后凭空补出未持久记录。系统 MUST NOT 为规避屏障合同而新增数据库或 WAL 选型。

#### Scenario: 屏障后跨进程读回

- **WHEN** StoreEvent 提交且 `Sync()` 返回成功，进程随即终止而不经 Close
- **THEN** 独立新进程可经 EventKey 取回原文及索引

#### Scenario: 屏障失败不报 durable 成功

- **WHEN** 事件提交遇快照写入或 rename 失败
- **THEN** 返回非 nil error、保留故障证据，投影不追加，消费者不收到 durable 成功

#### Scenario: 内存档复用合同但不耐久

- **WHEN** 使用内存档提交事件后重启
- **THEN** 提交/隔离/去重语义 SHALL 与持久档一致，但该记录 MUST NOT 被承诺跨进程恢复（重启不出「凭空补出」的未持久记录）

#### Scenario: 不新增数据库规避屏障

- **WHEN** 审查后端档位实现
- **THEN** SHALL 保持 memory/localfile/file 既有档位与各自屏障证明，MUST NOT 引入新的数据库/WAL 以逃避「不假设批量原子」的合同

### Requirement: 未确认恢复材料的有限保留租约

未确认 inbox envelope 的 prepared fact key 与 receipt key、以及普通 spill 的待重放 key SHALL 由恢复 owner 注册进共享存储层的保留租约（内存守护集）；持有者是共享资源 owner，不是任一 agent。租约 MUST 在启动时由现有未确认材料重建，MUST NOT 引入第二张持久保留表或全历史去重集合。同一 key 可同时被多个 owner 保护：释放按引用计数递减、归零才解除，MUST 幂等。**持久恢复档启用时**，若 store 缺少保护/回收协调能力，系统 SHALL **具名报告能力不完整并拒绝启动或禁止破坏性清理**（fail-closed），MUST NOT 以「无保护即成功」继续；**非耐久档**（不承诺进程恢复）缺少恢复能力 SHALL 以**显式 capability 缺失**声明，不得伪造保护已生效。

保护期内 TTL 过期、容量淘汰、内容降分辨率与墓碑最终清理 MUST NOT 物理销毁所需原文——只拒销毁，无损搬迁（压实复制原文到高层段）允许。显式删除受保护 key MUST 返回可判定的 protected 错误，MUST NOT 静默销毁。遗忘扫描的首轮破坏性动作 MUST 等待租约武装与登记屏障解除；无耐久恢复 owner 登记时该等待 MUST 有界（宽限值见 `memory/lifecycle.go` 的 `armGrace`），使租约缺位不会变成永久停止遗忘。envelope 安全清理（ack 目录同步成功或 spill 安全移除）后释放该 key，其后按原类型的年龄窗口、以其原有时间戳恢复参与淘汰，MUST NOT 重新盖时间。单个 agent 关闭 MUST NOT 释放共享存储仍然需要的 outstanding 保护。合法墓碑 MUST 被拒绝重放；恢复材料与墓碑皆已清除后的任意手工历史重放不属于自动恢复保证，MUST NOT 为此新增永久去重库。

`Sync()` 屏障与保留租约 SHALL 协同：屏障成功是提交对外可见的前提，租约则是「恢复清点完成前不得销毁」的保护，两者不得互相短路（屏障不因租约存在而放宽，租约不因屏障成功而解除）。

#### Scenario: 未确认材料越过普通 TTL

- **WHEN** prepared 输入与 receipt 已超过其类型的年龄窗口，而 envelope 尚未清理
- **THEN** 原文与确认依据保留，恢复仍能得到正确结论；envelope 清理并释放后，该 key 按其原有时间戳恢复参与年龄淘汰

#### Scenario: 重启时登记先于淘汰

- **WHEN** 后端打开时待恢复 key 名义上已过 TTL
- **THEN** 租约重建先于首轮破坏性扫描，扫描器不得"先淘汰再登记"

#### Scenario: 显式删除撞上受保护材料

- **WHEN** 删除一个仍被未确认恢复租约保护的 key
- **THEN** 调用返回 protected 错误，原文未被销毁

#### Scenario: 共享存储下的单 agent 关闭

- **WHEN** 一个 agent 关闭但仍有未确认材料，另一 agent 继续共享同一存储
- **THEN** outstanding 保护仍由资源 owner 持有，运行中的扫描器不得清理它

#### Scenario: 持久恢复档缺能力 fail-closed

- **WHEN** 启用持久恢复档而 store 未提供保护/回收协调能力
- **THEN** 系统 SHALL 具名报告能力不完整并拒绝启动或禁止破坏性清理（fail-closed），MUST NOT 当作无保护成功

#### Scenario: 非耐久档显式 capability

- **WHEN** 使用不承诺进程恢复的档位
- **THEN** 其恢复能力缺失 SHALL 以显式 capability 声明，MUST NOT 伪造保护已生效

### Requirement: 恢复能力经过包装层真实透传

内置后端及 engine／error-tracking 包装链 SHALL **递归**核验并透传**恢复/保留/登记屏障**所需能力；能力 SHALL 按**真实消费者**所需端口表达（具体端口名称/签名**不在此冻结**，DESIGN-GATE(G1) 依真实 consumer 收敛——不复述旧 5 方法清单）。包装层 MUST NOT 以外层满足接口代替内层能力，也不得以普通读写的存在作为重放能力的弱回退。

**持久恢复档启用时**，若内层缺少保护/回收协调能力，MUST fail-closed：**具名报告能力不完整**、拒绝启动或禁止破坏性清理，MUST NOT 静默降级为「无保护即成功」，也 MUST NOT 以「屏障类空操作」伪装协调已生效。**非耐久档**缺少恢复能力 SHALL 显式声明为 capability 缺失。

重放成功时 SHALL 把**内层返回的 canonical fact** 交给下游消费，MUST NOT 以传入副本代替。重放错误 MUST 到达恢复 owner：可靠收件箱已自持这些事件的 at-least-once 重试，因此重放失败**不得**再落一份 mem_spill 兜底（那会在兜底文件里造出重复条目）；普通写入原有的 spill 行为不变。spill 重建出的保留租约若失败，MUST 以错误上抛由调用方 fail-closed，旧实例继续服务。

绝对事件计数只在真实提交路径递增；重放判定为「已修复」或「已提交」的分支 MUST NOT 递增（否则一次幂等重放会被当作新增抬高淘汰压力），容量提示因此至多是建议性观察，不作删除依据。

#### Scenario: 持久恢复档缺协调能力 fail-closed

- **WHEN** 启用持久恢复档而内层包装链缺少保护/回收协调能力
- **THEN** 系统 SHALL 具名报告能力不完整并拒绝启动或禁止破坏性清理，MUST NOT 以空操作或「无保护即成功」降级

#### Scenario: 重放失败不被 mem_spill 兜底掩盖

- **WHEN** 重放调用失败
- **THEN** 错误 SHALL 到达恢复 owner（at-least-once 重试），MUST NOT 再落一份 mem_spill 兜底条目

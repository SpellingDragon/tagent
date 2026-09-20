## MODIFIED Requirements

### Requirement: eventCount 在进程生命期内反映实际事件数

支持分区枚举的存储 SHALL 在启动扫描器前重建完整、可发现、原文/索引身份一致且排除墓碑的去重逻辑存活计数。已知分区的新提交增量 +1，公共重复拒绝和已提交同内容重放 +0；首次墓碑化或直接删除存活事件减一，物理清理墓碑与压实搬迁不得重复增减。

可能改变持久状态的失败 SHALL 将相关分区 count_known=false；其容量淘汰暂停，不以猜测数执行删除。内部修复或不确定提交经过成功屏障后 MUST 在一致分区协调范围重算该分区，扫描完整才恢复 known。不支持枚举/扫描失败不得报告精确 0；缓存、replay 分类和引擎回调均不能作为计数真源。正常新写与模型装配不得逐事件全历史扫描。

#### Scenario: eventCount decremented on DeleteEvent
- **WHEN** 成功删除逻辑存活事件
- **THEN** live count 减一，重复删除不再递减

#### Scenario: eventCount decremented on compaction cleanup
- **WHEN** 压实搬迁同一 key 或清理已墓碑事件
- **THEN** 不重复增减，该 key 只计一次

#### Scenario: 重启后计数可恢复
- **WHEN** 新进程打开含 600 个完整存活事件与 40 个墓碑的后端
- **THEN** 扫描器启动前计数为 600，不完整记录不计为已提交

#### Scenario: 枚举失败不能冒充空库
- **WHEN** 枚举不支持或分区扫描失败
- **THEN** 对应计数 unknown、诊断可见、容量淘汰不运行

#### Scenario: 缓存淘汰及重启后重放
- **WHEN** 已提交事件缓存被淘汰或进程重启后重放多次
- **THEN** 每次重放后的逻辑计数保持不变，容量提示不把它当新事件

#### Scenario: 不确定写入恢复
- **WHEN** 任一原文/索引/meta/屏障阶段失败后又恢复
- **THEN** 受影响分区先 unknown，成功核对及重算后才 known；无关分区不被错误重算或淘汰

### Requirement: 后端不可变与隔离一致性

InMemoryStore 与 FileSegmentStore 的公共 StoreEvent SHALL 拒绝现有重复 key，包括相同内容，且保持显式分区查询与可变对象隔离。内部重放 SHALL 为独立窄能力，但与普通新增复用一个提交核心，通过策略区分新增拒重和恢复核对。重放必须校验输入 key/FullEvent key/分区及来源身份，错误不能靠覆盖字段消除。

提交身份探测、定位原槽、必要补写、屏障及计数发布 SHALL 与同分区删除、墓碑、封口、压实发布采用一致 mutation 协调；跨分区可并行，不持全局 registry 锁做 I/O。旁路回调在存储 mutation 锁释放后执行，禁止反向锁序。完整同内容重放仍经过真实后端屏障，缓存冷热不能改变提交分类或省略核验。

#### Scenario: 内存与文件后端契约一致
- **WHEN** 执行同内容/异内容重复公共写入、无分区查询、修改返回对象
- **THEN** 公共重复均拒绝，查询隔离，后续读取原事实不变

#### Scenario: 公共新增与内部恢复区分
- **WHEN** 相同已提交事实分别经公共写与内部重放
- **THEN** 前者 duplicate，后者核对成功返回 canonical fact，均不覆写及重复计数

#### Scenario: 同键并发与压实交错
- **WHEN** 同分区多个写入/重放与删除/压实并发
- **THEN** 提交/发布次序一致，没有同身份不同原文或重复计数，屏障失败不发布成功

#### Scenario: 错分区或错身份
- **WHEN** 参数 key、事实 key、分区或来源相互矛盾
- **THEN** 返回明确冲突，不重盖字段后提交

### Requirement: 存储失败不可冒充正常缺失

KV/事件接口 SHALL 区分 typed not-found、duplicate/conflict、forgotten 与 I/O；GetEvents/QueryEvents 的部分结果及非 nil error 可并存，不能吞错误报告完整。必要元数据或屏障失败不得报告提交成功。

内部重放 MUST 核验 idx 指向的真实 evt 及必要 meta：缺 evt 在原槽补写；缺 idx 在原放置窗和该分区已登记段定位同 key 原文并复用；缺 meta 不论 seq 是否零均修复。已有段层级/封口不可无条件重置，必要时间边界保持保守。双层同内容按既有高层优先规则选择，异内容冲突；合法墓碑不能复活。只有原文/索引/发现关系成立且屏障成功后返回 new/repaired/already 及 canonical fact；不能从缓存判定完整。

#### Scenario: 多分区部分扫描失败
- **WHEN** 某授权分区可读、另一个扫描失败
- **THEN** 返回可用部分及错误，不报告空库或完整成功

#### Scenario: 内部重放修复未完成写入
- **WHEN** 原文存在但索引或必要 meta 缺失
- **THEN** 重放复用原槽补齐并经过屏障，不生成第二原文

#### Scenario: 索引存在而原文缺失
- **WHEN** idx 指向的 evt 为 typed not-found
- **THEN** 在该槽真实补写，绕过缓存及独立进程重开均能读回

#### Scenario: 非首槽缺发现元数据
- **WHEN** 非零 seq 的 evt/idx 存在但 meta 丢失
- **THEN** 重放修复发现关系，不因为 seq 非零跳过

#### Scenario: 同内容缓存命中但屏障失败
- **WHEN** 已提交事实命中缓存，但本次重放的屏障被注入错误
- **THEN** 返回该错误而非 already 成功，不允许上层确认

#### Scenario: 合法遗忘与内容冲突
- **WHEN** 重放命中持久墓碑，或现存原文/来源与准备材料不同
- **THEN** 返回明确 forgotten/conflict 并保留恢复材料，不覆写或移除墓碑

## ADDED Requirements

### Requirement: 恢复能力经过包装层真实透传

内置后端及 engine/error-tracking 包装链 SHALL 递归验证并透传显式重放、材料保留和底层真实屏障能力。成功返回的 canonical fact SHALL 为下游消费对象，不能以传入副本代替。错误到达恢复 owner，不得降级为 GetEvent 成功即删除恢复材料；不支持能力明确拒绝并保留 pending。

可靠 inbox 拥有的输入/receipt 重放错误 SHALL NOT 再进入 mem_spill；普通写入原有 spill 保留，但 spill 重放也必须采用相同 canonical 契约。索引更新按 key 幂等，容量提示依据已知绝对计数，不能将 repaired/already 直接做新增增量。

#### Scenario: 包装后输入失败
- **WHEN** 最内层提交屏障失败而外层具有重放方法
- **THEN** 同一错误到达 inbox owner，不投影、不确认、不产生第二份输入 spill

#### Scenario: 后端实际不支持
- **WHEN** 包装器内层没有显式恢复能力
- **THEN** 能力检查失败；普通 spill 原件保留，不以普通写/读存在作为弱回退

#### Scenario: 修复回调不重复计数
- **WHEN** 同一 key repaired 后再次 already
- **THEN** 索引无重复逻辑项，容量观察与底层绝对计数一致

### Requirement: 未确认恢复材料的有限保留租约

未确认 envelope 的 prepared fact keys 与 receipt key、普通 spill 待重放 key SHALL 由恢复 owner 注册保留租约，持有者为共享资源 owner。租约从现有未确认材料重建，MUST NOT 引入第二持久保留表或全历史去重集合。

租约登记须早于可能淘汰材料的扫描/压实启动，以及 fresh prepare 的首条事实提交。保护期内 TTL/容量/内容降分辨率/墓碑最终清理不得破坏所需原文；无损搬迁允许；显式删除返回 protected。Ack 目录同步成功或 spill 安全移除后才释放，单 agent 关闭不得释放共享存储仍需的 outstanding 保护。释放后恢复原类型 TTL，receipt 按原 30 天年龄窗口，不重新盖时间。

合法墓碑 SHALL 被拒绝重放；恢复材料及墓碑均已清除后的任意手工历史重放不属于自动恢复保证，不能因此新增永久去重库。

#### Scenario: 未确认超过 TTL
- **WHEN** prepared 输入和 receipt 超过普通 TTL 且 envelope 尚未清理
- **THEN** 原文和确认依据保留，仍可正确恢复；成功清理后按原年龄恢复淘汰

#### Scenario: 重启时扫描器竞跑
- **WHEN** 后端打开、待恢复 keys 已过 TTL
- **THEN** 租约重建先于扫描器启动，不能先淘汰再登记

#### Scenario: 关闭一个共享 agent
- **WHEN** 一个 agent 关闭但尚有未确认材料，另一个继续共享存储
- **THEN** outstanding 保护仍在资源 owner 中有效，不被运行中的扫描器清理

#### Scenario: 保留与删除并发
- **WHEN** 注册/释放保留与 TTL/显式删除并发
- **THEN** 通过同一分区协调决定次序，受保护项不被删除，已合法删除项不能补写复活

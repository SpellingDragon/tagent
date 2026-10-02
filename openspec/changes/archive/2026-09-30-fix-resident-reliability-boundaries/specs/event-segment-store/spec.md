## MODIFIED Requirements

### Requirement: eventCount 在进程生命期内反映实际事件数

支持分区枚举的存储 SHALL 在启动扫描器前，从原文、索引、段发现关系一致且排除墓碑的 canonical EventKey 重建去重逻辑存活计数，成功新增提交后增量维护。首次墓碑化或直接删除存活事件 SHALL 递减一次，物理清理已墓碑事件与压实搬迁 SHALL NOT 再递减。公共重复拒绝及已提交事件的同内容内部重放 SHALL NOT 增计数。

部分写入失败使逻辑计数无法确定时，相关分区 MUST 标记 count_known=false，暂停其容量淘汰。内部修复或不确定状态恢复的成功屏障后 SHALL 在写入协调下重算受影响分区，扫描成功才恢复 known，不猜测 +1。不能完成枚举/扫描时 SHALL 保持 unknown，不报告精确 0。正常新写入和模型装配 SHALL NOT 因此逐事件扫描全库。

#### Scenario: eventCount decremented on DeleteEvent
- **WHEN** DeleteEvent 成功删除一个逻辑存活事件
- **THEN** live count 减一，重复删除不再递减

#### Scenario: eventCount decremented on compaction cleanup
- **WHEN** 压实清理已墓碑事件或搬迁相同 EventKey 到新段
- **THEN** live count 不重复减少，同一存活 EventKey 仅计一次

#### Scenario: 重启后计数可恢复
- **WHEN** 新进程打开含 600 个完整存活事件和 40 个墓碑的存储
- **THEN** 扫描器启动前 live count 为 600，容量淘汰包括存量

#### Scenario: 枚举失败不能冒充空库
- **WHEN** 后端不支持枚举或某分区扫描失败
- **THEN** 对应计数 unknown 且可观测，不基于猜测的 0 进行容量判断

#### Scenario: 成功后反复提交不增加计数
- **WHEN** 同一事件已提交，随后公共重复写入被拒及内部同内容重放多次成功
- **THEN** live count 始终只计一次，capacity hook 不把已存在事件当新增，容量淘汰不因此额外删除其他事件

#### Scenario: 失败后修复与重启对账
- **WHEN** 部分写入失败后修复，且覆盖同进程、缓存淘汰及独立进程 reopen
- **THEN** 修复前未知计数不驱动容量淘汰；修复并成功重算后计数与逻辑存活集合一致，再次重放不增加

### Requirement: 后端不可变与隔离一致性

InMemoryStore 与 FileSegmentStore SHALL 在公共 StoreEvent 上拒绝已存在的重复 EventKey，包括同内容重复；SHALL 对缺少显式分区过滤的 QueryEvents 返回空，并隔离输入/返回值中的可变 map/slice。内部重放 SHALL 使用单独显式能力，对同 key、同来源和规范化内容进行核对并补齐未提交写入，异内容 MUST 拒绝；公共写入不得覆写事实或被泛化为无条件幂等成功。两个内置后端及装饰链 MUST 保持该语义。

#### Scenario: 内存与文件后端契约一致
- **WHEN** 对两个后端执行重复键写入、无分区查询和返回对象修改
- **THEN** 重复写被拒、查询为空、后续 GetEvent 原文及元数据不变

#### Scenario: 内部重放与公共重复分离
- **WHEN** 已提交事件经公共 StoreEvent 再写，随后经内部重放核验同内容
- **THEN** 前者返回 typed duplicate，后者返回已有 canonical fact，两者均不覆写、不重复计数

#### Scenario: 同键并发
- **WHEN** 同一分区内多个调用并发提交或重放同一个 key
- **THEN** 身份核验、必要补写、屏障与计数发布受统一写入协调，不能产生不同原文或重复 live-count

### Requirement: 存储失败不可冒充正常缺失

KV/事件接口 SHALL 区分 typed not-found、duplicate 与存储 I/O 错误；GetEvents/QueryEvents SHALL NOT 吞掉 I/O 后返回完整成功。部分结果与非 nil error 可同时返回，上层 SHALL 处理 partial。元数据提交失败 SHALL NOT 被忽略为成功事件。

内部重放 MUST 核验 idx 指向的 evt 及必要 meta：缺少原文、索引或发现元数据时真实补齐，通过后端屏障后才返回可用 canonical fact；已有内容不一致、墓碑代表的合法遗忘或读取错误不得被当作可覆写空槽。既有段的层级与封口元数据 SHALL 不被无条件重置；缺索引时应复用现存同 key 槽，不生成第二原文。

#### Scenario: 多分区部分扫描失败
- **WHEN** 一个已授权分区可读而另一个扫描失败
- **THEN** 查询返回可用部分和明确错误，不报告空结果或完整成功

#### Scenario: 内部重放修复未完成写入
- **WHEN** 原文已写而索引/meta 未完成，使用相同身份和内容重放
- **THEN** 确定性补齐并经过屏障后才成功，不生成不同内容、重复原文或重复投影

#### Scenario: 索引存在但原文槽缺失
- **WHEN** idx 存在而指向的 evt 为 typed not-found
- **THEN** 内部重放真实补写该 evt 并执行屏障；独立查询与 reopen 可读到原文，不依赖缓存制造成功

#### Scenario: 补写或屏障再次失败
- **WHEN** orphan 修复遇 evt/idx/meta 写失败、Sync 失败或读取 I/O 错误
- **THEN** 返回错误且不发布成功投影/确认，不用缓存命中掩盖失败

#### Scenario: 墓碑不被重放复活
- **WHEN** 已删除事件的 key 被重放
- **THEN** 明确返回遗忘/冲突状态，不悄悄去除墓碑或计作新增

## ADDED Requirements

### Requirement: 内部重放能力经过装饰器保真

内部重放接口 SHALL 由内置存储及 engineBridge、ErrorTrackingStore 显式实现或透传，返回新提交/补齐/已有及 canonical fact。屏障和错误 SHALL 不被包装器擦除；已有事件不重复触发新增容量计数，索引按 key 幂等。inbox 已拥有原件的重放失败 SHALL NOT 再产生 mem_spill 输入副本；其他普通写入的既有 spill 行为保留，mem_spill 自身重放 SHALL 使用相同身份核验。

#### Scenario: 包装后修复失败
- **WHEN** ErrorTrackingStore 包裹 engineBridge 和 FileSegmentStore，底层内部重放屏障失败
- **THEN** 错误到达 inbox 消费者并上报退化，不写第二份可靠输入 spill，不投影、不 Ack

#### Scenario: 普通写入 spill 恢复
- **WHEN** 非 inbox 普通写入失败后进入 mem_spill，并与同 key 的已提交事实相遇
- **THEN** 重放核验 canonical 内容并幂等收敛，不因公共 duplicate 拒绝永久卡住，也不接受异内容

## MODIFIED Requirements

### Requirement: eventCount 在进程生命期内反映实际事件数

支持分区枚举的存储 SHALL 在启动扫描器前从事实链重建去重、排除墓碑的逻辑存活计数，并在成功提交事件后增量维护。首次墓碑化或直接删除存活事件 SHALL 递减一次；物理清理已墓碑事件与压实搬迁 SHALL NOT 再递减。写失败和重复键拒绝 SHALL NOT 增计数。不能完成枚举/扫描时 SHALL 标记 count_known=false 并暂停基于未知计数的容量淘汰，SHALL NOT 报告为精确的 0。

#### Scenario: eventCount decremented on DeleteEvent
- **WHEN** DeleteEvent 成功删除一个逻辑存活事件
- **THEN** live count 减一，重复删除不再递减

#### Scenario: eventCount decremented on compaction cleanup
- **WHEN** 压实清理已墓碑事件或搬迁相同 EventKey 到新段
- **THEN** live count 不重复减少，同一存活 EventKey 仅计一次

#### Scenario: 重启后计数可恢复
- **WHEN** 新进程打开含 600 个去重存活事件和 40 个墓碑的存储
- **THEN** 扫描器启动前 live count 为 600，容量淘汰包括存量

#### Scenario: 枚举失败不能冒充空库
- **WHEN** 后端不支持枚举或某分区扫描失败
- **THEN** count_known=false 且可观测，不基于 0 进行容量判断

### Requirement: LocalFileKV 写路径 fsync 耐久

LocalFileKV 的 WAL 追加 SHALL 在每批 ops Flush 后执行文件 Sync（fsync）；snapshot 重命名和首次 WAL 创建 SHALL 对目录执行 Sync。平台不支持目录同步时 SHALL 留痕并报告降级能力，其他 I/O 错误 SHALL 传播。fsync SHALL 可配置关闭且默认开启。直接 KVPut 为异步接收，Sync 成功才是其耐久屏障；localfile 的 FileSegmentStore.StoreEvent SHALL 在 evt/idx/必需 meta 写完且 Sync 成功后才发布缓存、计数、成功结果。调用方 SHALL 在成功后才投影。不能完成屏障 SHALL NOT 报告 durable 成功。

#### Scenario: 掉电窗口内的已确认写入
- **WHEN** KVPut 后 Sync 已返回，子进程随即终止而不经 Close
- **THEN** 新进程可读回该键值；测试报告区分进程终止与真实掉电证据

#### Scenario: 显式关闭 fsync
- **WHEN** memory.fsync=false
- **THEN** 保持 Flush 级行为，日志与 diagnostics 明示不具掉电耐久保证

#### Scenario: 事件级成功不早于屏障
- **WHEN** StoreEvent 写入不足周期 flush 阈值的单个事件后返回成功，进程立即终止
- **THEN** 独立新进程仍可经 EventKey 取回原文及索引

#### Scenario: 屏障失败
- **WHEN** 事件提交遇写失败、fsync 失败或非“不支持”的目录 Sync 错误
- **THEN** 返回非 nil error、保留故障证据，投影不追加，消费者不收到 durable 成功

## ADDED Requirements

### Requirement: 后端不可变与隔离一致性

InMemoryStore 与 FileSegmentStore SHALL 拒绝已提交的重复 EventKey，SHALL 对缺少显式分区过滤的 QueryEvents 返回空，SHALL 隔离输入/返回值中的可变 map/slice。内部重放可核对同键同内容完成未提交写入，异内容 MUST 拒绝；公共写入不得悄悄覆写事实。

#### Scenario: 内存与文件后端契约一致
- **WHEN** 对两个后端执行重复键写入、无分区查询和返回对象修改
- **THEN** 重复写被拒、查询为空、后续 GetEvent 原文及元数据不变

### Requirement: 存储失败不可冒充正常缺失

KV/事件接口 SHALL 区分 typed not-found、duplicate 与存储 I/O 错误；GetEvents/QueryEvents SHALL NOT 吞掉 I/O 后返回完整成功。部分结果与非 nil error 可同时返回；上层 SHALL 处理 partial。元数据提交失败 SHALL NOT 被忽略为成功事件。

#### Scenario: 多分区部分扫描失败
- **WHEN** 一个已授权分区可读而另一个扫描失败
- **THEN** 查询返回可用部分和明确错误，不报告空结果或完整成功

#### Scenario: 内部重放修复未完成写入
- **WHEN** 原文已写而索引/meta 未完成，使用相同身份和内容重试
- **THEN** 确定性补齐并经过屏障后才成功，不生成不同内容或重复投影

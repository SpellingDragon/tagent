# partition-local-storage-access Specification

## ADDED Requirements

### Requirement: 分区访问局部性与等价回退
LocalFileKV SHALL 对可证明属于单命名空间的Scan/Range仅访问该桶；模糊或跨桶查询 SHALL 保守遍历并保持现有词典序/limit结果。

#### Scenario: 单桶与模糊前缀
- **WHEN** 多分区库存分别查询完整pid前缀、部分数字前缀和跨桶范围
- **THEN** 结果与原全扫描参照逐位一致，单桶路径不访问其他桶。

### Requirement: 查询优化不改事实语义
减少解码 SHALL 保留授权、真实事件时间、活跃段参与、层级去重、offset/limit及partial错误；MUST NOT 把读取失败当无匹配。

#### Scenario: 乱序与重复层版本
- **WHEN** 同键存在多层且Timestamp与写入序不同
- **THEN** 结果与现行契约参照一致，坏元数据不误剪。

### Requirement: 同步屏障与磁盘格式不变
快照写入 MUST 保持原 JSON 语义、tmp+rename 及逐事件 Sync 后成功（编码缓存候选已在收益门裁决中撤回，现行实现为逐桶直接 marshal）；不允许提前发布 cache/count 或清除失败 dirty。

#### Scenario: 写盘失败重试与独立进程读回
- **WHEN** 直接编码写盘失败，再重试并独立进程打开
- **THEN** 失败不报提交成功，重试后全部已确认键可读。

### Requirement: 关系失败不发布虚假成功
RelationStore SHALL 仅在既有日志append成功后发布对应内存边，失败保留此前有效关系。

#### Scenario: 父关系append失败
- **WHEN** SetParent的日志写入失败
- **THEN** 返回错误且读侧仍见此前关系，不误认为新父已保存。

### Requirement: 性能主张具有同条件证据
性能结论 SHALL 附before/after原始数据、真实规模和冷热口径；编码缓存未通过规定收益/内存/退化门时 MUST 撤回该候选并保存负结果。

#### Scenario: 候选没有净收益
- **WHEN** 同条件基准的目标10k事件路径中位耗时改善不足10%，或同类路径退化超过10%、附加缓存超过8MiB、写盘字节增加
- **THEN** 不声称加速，直接编码继续工作，定向扫描单独验收。

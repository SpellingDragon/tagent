# partition-local-storage-access Specification

## Purpose
分区本地存储访问：按分区局部性组织读写并保留等价回退路径，查询优化不改变事实语义，同步屏障与磁盘格式在优化前后保持不变。
## Requirements
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

**一个提交 SHALL 一个屏障**：一次 `Commit` 成功后，该批全部键 SHALL 对该存储根上其后打开的视图可读；该批任一键不可读 SHALL 视为提交未成功，MUST NOT 以「部分键已读」表达成功。**逐键串行调用 MUST NOT 构成一次原子提交**：以每键一次外部调用表达一批写入的档位，SHALL 在收到多键批时具名声明能力缺失并拒绝，MUST NOT 以该形态实现「全有或全无」的提交语义，MUST NOT 让调用方从注释或文档推断其原子性。

**能力面 SHALL 显式**：一个档位是否具备跨键原子 SHALL 在其能力声明中可读（与是否耐久并列），调用方 SHALL 依该声明处置，MUST NOT 依档位名称或注释推断。原子化与有界化 SHALL NOT 改变既有磁盘格式的语义（本要求不授权任何就地迁移；格式变更另立并须源只读、复制＋校验）。

#### Scenario: 写盘失败重试与独立进程读回

- **WHEN** 直接编码写盘失败，再重试并独立进程打开
- **THEN** 失败不报提交成功，重试后全部已确认键可读

#### Scenario: 一个提交一个屏障

- **WHEN** 一次多键提交返回成功，随后另一视图打开同一存储根
- **THEN** 该批全部键 SHALL 可读，SHALL NOT 出现部分键缺失
- **AND** 一次返回失败的提交 MUST NOT 让其后打开的视图读到该批任何键

#### Scenario: 逐键档位拒绝多键批

- **WHEN** 以每键一次外部调用表达写入的档位收到含两个以上键的批
- **THEN** 该提交 SHALL 以具名能力缺失拒绝，MUST NOT 逐键写入后报成功
- **AND** 单键提交 MAY 继续被该档位接受

#### Scenario: 能力面可读

- **WHEN** 调用方打开一个存储档位
- **THEN** 该档位是否具备跨键原子 SHALL 在其能力声明中可读
- **AND** 调用方 MUST NOT 以档位名称、注释或既有文档推断该性质

#### Scenario: 原子化不改磁盘格式

- **WHEN** 把一条事实的两笔提交合并为一次提交
- **THEN** 已有键的编码语义、tmp+rename 与 Sync 屏障 SHALL 保持不变
- **AND** MUST NOT 触发任何就地数据迁移

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

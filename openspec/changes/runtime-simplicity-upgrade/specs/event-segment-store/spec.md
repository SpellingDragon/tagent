## MODIFIED Requirements

### Requirement: LocalFileKV Sync 为原子快照屏障
持久后端 SHALL 以追加记录承载键值变化，一条记录 MUST 完整写入并通过校验后才算提交。`Sync()` SHALL 成为唯一持久屏障：返回成功后，独立新进程 MUST 能读回该屏障覆盖的全部已提交记录。写入路径 MUST NOT 在记录未刷盘前把值发布为已提交；一次失败的写入 MUST NOT 被后续提交带入。日志中间帧损坏 SHALL 启动失败；末尾不完整帧 SHALL 被截除并保留此前完整记录。

#### Scenario: 追加写入不再整分片重写
- **WHEN** 已有 N 条记录后再提交一条
- **THEN** 写入量 MUST 与该条记录大小相关
- **AND** MUST NOT 重写此前全部记录

#### Scenario: 失败写入不被后续提交带出
- **WHEN** 一次提交写入或刷盘失败并返回错误
- **THEN** 该值 MUST NOT 出现在后续读取中
- **AND** 下一次成功提交 MUST NOT 使该值变为可见

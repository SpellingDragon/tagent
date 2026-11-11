## MODIFIED Requirements

### Requirement: 统一原子存储接口与引擎无关故障注入
存储 SHALL 暴露唯一公开原子接口（Commit 全有或全无、Read 只读已落盘、屏障失败具名 CommitUncertain）；引擎（localfile-shard/journal/未来引擎）SHALL 实现该接口且 OpenTier 返回接口类型；调用方与测试 SHALL 只依赖接口。故障注入 SHALL 在接口边界（port decorator）而非具体文件布局；同一套契约测试 SHALL 在任一引擎上绿。原文 body SHALL write-ahead 先落内容寻址 OriginalStore、事实原子提交后引用、失败孤儿可 GC。

#### Scenario: 换引擎零适配
- **WHEN** 切换存储引擎实现
- **THEN** 调用方与契约测试 MUST NOT 需要修改

#### Scenario: port 级写失败注入
- **WHEN** port decorator 令第 n 次 Commit 或 Barrier 失败
- **THEN** 该失败 MUST 具名返回且不被后续提交带出，与引擎布局无关

#### Scenario: 事实可见必 body durable
- **WHEN** 一条引用 body 的事实被读见
- **THEN** 其 body MUST 已 durable（write-ahead），失败提交孤儿 body MUST 可 GC

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

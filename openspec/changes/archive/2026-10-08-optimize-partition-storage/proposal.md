# O4 分区存储访问与提交成本

## Why

localfile已按脏分区快照，但Scan/Range会先展平全库；逐事件屏障又重复编码未变键值。本域优化真实访问路径及编码CPU/分配，不用放松提交保证换取表面吞吐。

## What Changes
- 对可判定命名空间的查询只访问目标桶，保留跨桶与模糊前缀回退。
- 减少查询过程重复解码和中间容器；保留事件时间、层级去重与partial错误。
- 在严格基准门后采用有界快照编码复用；磁盘JSON与每次Sync写入字节不改变。
- 补关系WAL失败不发布成功内存边的回归与修正，服务O1一致性。

## Capabilities
- 新增partition-local-storage-access；event-segment-store既有成功屏障、排序、TTL和隔离合同零变化。

## 边界与依赖
- 父docs-objective-review；O4不依赖其他域，可独立实施；O1.6依赖O4.5。
- 独占memory/kv、segment_store、lifecycle、relation_store及对应测试；基准脚本/跨包测试/文档由编排者接线。
- 禁止：新WAL/SQLite后端、异步提前报成功、Metadata通用索引、TTL统一、永久缓存表或生产耐久认证扩张。

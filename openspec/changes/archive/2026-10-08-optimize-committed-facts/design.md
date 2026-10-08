# O1 设计

## 已核实入口
- `plugin/memory_plugin.go:onEvent`：读父、StoreEvent、SetParent、投影、StateDelta、游标。
- `tool/recall/recall_subtools.go:walkTurnChain` 与 `recall.go`：已通过 Complete 表示不完整，缺少统一原因；不是“必然静默丢整轮”。
- `plugin/attribution.go:EchoCredential`：失败仍应 MarkRejected，不能误 ack。

## 实施决策
1. 采用同因果键串行提交段：为正在使用/等待的键保持锁记录，idle 游标继续按现有4096上界回收；不淘汰活跃键，不在全局 map 锁内执行存储 I/O。锁序固定为短 map 锁→键锁，禁止反向持有。
2. key分配、读父、StoreEvent、关系处理、游标推进在同键顺序内；不同 session 可并发。时间按接纳/提交顺序，不按 Timestamp 重排。
3. 存储成功才写 StateDelta 的 event_key/partition_id、Append 投影和推进最后已存键；失败清理本插件生成的票据字段，不删除无关框架 StateDelta。nil store 不发布伪持久票据；正文/分类可继续返回。
4. 内容已存但关系失败：事实与票据保持有效，关系失败有日志和回溯 partial，不通过回滚内容伪装原子事务；下一条仍接最后已存键。关系内存发布与 WAL 行为由 O4.5 同时守护。
5. 不新增“最后会话”恢复表或靠时间扫描猜父。无可信祖先时返回 partial。回溯共享实现增加可选 `reason`：missing_ancestor/relation_error/relation_unavailable/cycle/limit；首键不存在仍保持错误行为；Complete/Capped 兼容。
6. O5 的 call归因查找只能是附加操作，不能改变此提交闸、事件key或父链；其接入由编排者在本域冻结后单写。

## 文件所有权
O1独占 plugin/memory_plugin.go 与其测试、tool/recall 对应实现/测试。event/metadata.go、plugin/attribution.go、根包/跨包集成、所有 wiki 由编排者单写；本域提供精确改动清单。memory/relation_store.go 由 O4 独占。

## 兼容与回滚
成功事件格式不变；失败时不再返回可用票据是有意修正。reason为附加字段；保留Complete/Capped。无磁盘迁移，二进制回退不需改写事件。旧存量缺链不被本域自动修复。

## 验收与证据
红→绿覆盖 store失败后成功、nil store、关系失败、同session并发、跨session隔离、关闭/恢复、祖先缺失和环。既有真实框架事件管线测试必须保留。新增测试名以行为命名；本目录 tasks 的名字均为拟新增或扩展，零匹配不得勾选。

文档落点：docs/wiki/plugin/plugin-architecture.md 的 memory-plugin/attribution-carrier、docs/wiki/memory/memory-architecture.md 的因果链及 docs/wiki/tool/tool-architecture.md 的回溯契约。

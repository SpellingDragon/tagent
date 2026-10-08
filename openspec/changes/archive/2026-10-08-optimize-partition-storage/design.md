# O4 设计

## 可验证现状
LocalFileKV.KVScan/KVRange经allEntries遍历所有parts/global；flushLocked逐脏桶json.Marshal→tmp→rename。FileSegmentStore.finishCommit在Sync后才发布cache/count。优化不能更改此顺序。

## 决策及算法
1. 前缀明确含合法numeric pid加冒号时只遍历该桶；global明确命名空间走global；模糊前缀如`1`、跨桶Range、未知/空边界退回全桶遍历。Range仅在可证明[start,end)位于一个桶时收窄，仍按原完整字符串过滤排序。避免allEntries全库临时切片；不改变词典序或limit。
2. QueryEvents沿既有segment扫描单次提取轻量header（key/type/timestamp/summary及所需字段）；有keyword才解码正文，不把缺字段当匹配。去重继续选更高layer，分区授权、offset/limit、真实MinTime/MaxTime剪枝及活跃段总参与不变。生命周期已用header，复用同一解析帮助函数，不宣称此前全解码整结构。
3. 快照写入候选只复用JSON转义片段和编码缓冲：每条缓存必须以当前key/value内容命中，修改/删除失效；排序输出与原json.Marshal语义一致（含转义、UTF-8与空桶删除）；总附加缓存≤8MiB，单entry超过64KiB不缓存。缓存可随时丢弃并从map重建，不写入磁盘、不改变成功顺序、不在错误后清dirty。
4. 编码缓存只有D17性能门达标才保留，否则撤该候选并保留负结果。查询定向扫描仍为必交付，不用缓存失败掩盖无实质优化。该优化减少CPU/alloc，不声称减少每事件写盘字节。
5. RelationStore.SetParent的内存可见性须与日志append成功一致；在其既有锁内先形成操作、append成功再发布或失败恢复旧状态。遵从原同步级别，不新增跨事实/关系事务；失败数据不冒充已持久边。O4独占此文件，O1消费结果。

## 基准矩阵与门
沿用tests/offline_bench扩充语义命名benchmark：PartitionScan、SegmentQuery、SnapshotCommit。固定seed，矩阵不做笛卡尔积：常规1k/10k全库事件×1KiB×1/16桶；规模100k紧凑事件×1/16桶；大正文512事件×64KiB×1/16桶。单run fixture≤256MiB、总写盘≤3GiB。每档完整库存不得用采样数冒充；预装允许KVBatch+Sync构造等价fixture并验证冷启动，在计时外单列成本。计时内写入仍每次StoreEvent提交，不用批量躲屏障。冷热区分重开/应用cache失效/预热，OS缓存不归类冷盘。
同机before/after至少5轮，保存原始输出、JSON配置及ns/alloc/bytes/p95。定向查询以访问桶计数为硬断言；编码复用需目标10kcell中位数改善≥10%、其他同类cell退化≤10%、缓存≤8MiB、写盘bytes不增。负增益撤候选，不切新引擎。

## 验证与回滚
随机跨桶前缀/Range与原扫描参照对拍；乱序时间、sealed/unsealed、坏meta、跨层同key、部分IO失败、TTL保护均跑；Sync失败不发布cache/count，无Close子进程重启读回。磁盘文件完全兼容，去掉缓存即可退回直接编码。文档不升级localfile的抗掉电承诺。

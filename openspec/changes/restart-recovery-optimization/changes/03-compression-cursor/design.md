# 03-compression-cursor 设计

## 技术要点

被否方案 0 拷贝内存映射（可变 Go map 与只读页不兼容、需引入 unsafe、瓶颈不在拷贝）。票据字段：schema_version、blob 内容指纹+长度、覆盖到的最后 fact key/revision（游标）。不变式：全量重放 ≡ dump + 增量；四类失败（票据缺失/blob 截断/校验不过/版本不认识）一律退回全量；Put 后 Sync 才报成功；map 排序后确定性序列化。

## 实现定稿（r13 前设计冻结）

**接入点**：`NewDurableCommit`（runtime/state/durable.go）的调用方保持不变；恢复分流在 `NewDurableCommit` 内部——先读票据，命中且校验通过则从 blob 重建八表并补放增量，否则走既有 `load()` 全量。不改 FactLog 端口、不改调用方。

**票据形态**：新冻结类型 `recovery.ticket`（FACT-VOCABULARY 登记族 12；R3 登记同一 KV 面新前缀 `tagent/task/v1/ticket:`）。不复用 `CompressProduct.Text`——那是投影正文，塞票据会被 `RestoreProducts` 误恢复成投影条目。票据身份=内容寻址（schema_version+cursor+digest）；字段：schema_version、digest（blob 的 sha256）、length、cursor（覆盖到的最后 fact key）、written_at。

**blob 存储**：复用族 5 `OriginalStore`（memory/ledger/original_store.go）——Put 按 digest 内容寻址、Get 自带 digest+length 校验、截断/替换读失败。链上票据+外部只读 blob 的现成轨道，不新造存储。

**增量语义（与阶段 4 分工，r16 诚实化）**：blob 恢复八表后 mergeDelta 仍执行五前缀 Scan 并 Decode 全部事实（字典序 cursor 不构成全局时间序，不能安全跳过 Decode）；增量按 per-task ExpectedRevision 过滤 fold。cursor 的实际用途=latestTicket 的确定性选择与观测。本阶段净收益=跳过大部分表的重建与 fold CPU；I/O 点读化归阶段 4。

**确定性序列化**：八表先排序（taskID 升序、facts 按 ExpectedRevision、mutations 按 taskID+mutationID、owed/consumed/attempts/rejected 按 noticeKey 字典序）再 marshal；同状态两次序列化字节相同（3.1 正锚）。

**四类失败退回全量**（3.4 负锚具名）：票据缺失 / blob 截断或 digest 不符 / 版本不认识 / cursor 落后于链现状——一律 load() 全量重建，结果与无票据一致，义务不丢。

**落盘屏障**：blob 先 Put+Sync，票据后 Put+Sync；票据先于 blob 到位视为未命中（退回全量），blob 先于票据残留无害（无引用，归 lifecycle）。

## 风险与回退

见上；每单以复跑命令为准，失败即回退本单并回写计划。

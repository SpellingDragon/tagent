# 03-compression-cursor 设计

## 技术要点

被否方案 0 拷贝内存映射（可变 Go map 与只读页不兼容、需引入 unsafe、瓶颈不在拷贝）。票据字段：schema_version、blob 内容指纹+长度、覆盖到的最后 fact key/revision（游标）。不变式：全量重放 ≡ dump + 增量；四类失败（票据缺失/blob 截断/校验不过/版本不认识）一律退回全量；Put 后 Sync 才报成功；map 排序后确定性序列化。

## 风险与回退

见上；每单以复跑命令为准，失败即回退本单并回写计划。

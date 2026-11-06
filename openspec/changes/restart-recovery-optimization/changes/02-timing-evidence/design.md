# 02-timing-evidence 设计

## 技术要点

文件清单：runtime/state/durable.go、assembly/run.go、新增观测结构、runner 产物写入。实现事实：计数为顺序 append（load 单线程持有），字节和在计时窗内 O(pairs) 求和（小、如实计入读成本）；观测常开无开关，测试以真实写入断言。回退：整段计时为只读读数，删除即回到无观测，不影响恢复语义。

## 风险与回退

见上；每单以复跑命令为准，失败即回退本单并回写计划。

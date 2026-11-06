# 02-timing-evidence 设计

## 技术要点

文件清单：runtime/state/durable.go、assembly/run.go、新增观测结构、runner 产物写入。风险：观测本身开销污染测量——计数用原子累加、不逐键打印；回退：观测开关默认开、可单测关闭。

## 风险与回退

见上；每单以复跑命令为准，失败即回退本单并回写计划。

# design：plain-structure

## D1 平铺判据
声明字段多是本质复杂度；脏在行为漏进声明与隐式机制。显式字段>注册拼装（可发现性）。

## D2 子域表
| # | 子域 | 边界 | 依赖 |
|---|---|---|---|
| A | spec 归位平铺 | api/config＋四域 spec 迁移＋消费点 | config/域 |
| B | 删死码 | telemetry/recorder | telemetry |
| C | 域 PlanOf | meditation_plan→域 | meditation/根 |
| D | 重组收尾 | 根包命名/embedder 并/document 定案/应用层去壳 | 根/memory/api/examples |

## D3 波次
W1（A 合单＋B 并发）→W2（C→D）。A/B 触碰集不相交；C 碰 meditation（A3 也碰）→串行 W1 后。

## D4/D5
契约零改动（指纹 golden 字节对照）；三级验收=孙任务 exit0＋三点锚。

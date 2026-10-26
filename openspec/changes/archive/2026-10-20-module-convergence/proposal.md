# Proposal：模块化归收编（压缩事件化＋消费者声明第二辑）

## Why

化归审计（2026-10-19，逐 16 面）判定：真需要行为改造的只有**压缩三件**（①外置判定 ②curation ③Fold）；其余六面（Subscribe/Deliverer/Apply/Shutdown/Observer/Exporter）语义已对、缺消费器声明。用户判据：压缩应化归到三拓展点——**plugin 识别需压缩→发压缩事件；loop 消费事件→执行压缩→改 state**；读路径装箱（AssembleBudget）保持为投影呈现策略（不事件化，settle 记录除外）。

关键动机：Fold 现在是"读操作隐式写 state"（组装时折叠、写 CompactionRef）——违反"事件产出即锁定、投影只读"；压缩决策不可观测；压缩策略不可扩展。

## What Changes

- **01 压缩事件化**（行为重排，golden 对照）：
  - 新事实族 `compress_request`（判定：谁/为什么/折什么——plugin 或阈值判定产出）＋压缩消费（loop 或时间驱动消费器执行凝缩/折叠/外置，产出 artifact/ticket/fold 事实入 state）；
  - ①外置：BoundedTool 判定面改为"发压缩请求事件+同步轻量外置"（工具返回 preview/票已是事实形态——判定面显式化）；
  - ②curation：改为时间驱动消费器（Tick 同构）；
  - ③Fold：**预折叠**（压缩事件消费时折叠），组装路径只读；`settle`（降级决策）改为事实写入（replay-settle 事件）。
  - ④AssembleBudget 装箱保留为投影策略（判据：非状态变更）。
- **02 消费者声明第二辑**：六面归属声明（EXTENSIONS/FACT-VOCABULARY 附表）＋协议测试扩展（Subscribe 推消费/Exporter 导出消费/Apply 裁决消费/Shutdown 收尾消费/Observer 观察消费——各一断言"读→产/不改源事实"）。
- **03 判据收口**：EXTENSIONS 化归判据对照表（全模块三问归位表）＋doc budget 对账。

## Capabilities

### New
- `compression-events`：压缩请求事件族、压缩消费器（凝缩/折叠/外置）、settle 事实化。
### Modified
- `fact-vocabulary`（compress_request 族登记）、`consumer-topology`（六面消费声明＋策展消费器）。

## Impact

行为重排面：memory/curation、memory/replay_budget（Fold 路径）、workspace（判定面显式化）、run_request（组装只读）。回退=单机制还原（每机制独立提交粒度）；golden 12 场景为行为等价判据（压缩产物语义不变）。
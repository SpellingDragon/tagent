# Design：压缩事件化的五决策

## D1 压缩请求=事实族（不是命令）

`compress_request` 是事实（FactKey=request 身份/FactType="compress.request"）：记录**判定**（谁触发/为什么/对象）而非指令。产出者=plugin 相位观察或阈值判定（工具结果超界时）。消费=压缩执行器。

## D2 压缩执行=消费器（两种驱动源）

- **同步轻量外置**（①）：工具返回时即外置——因为 preview/票已是事实形态，事件化收益在"判定留痕"（发 compress_request 事实记录判定，外置动作仍在工具相位内完成，票据事实=压缩产物）。**被否**：完全异步外置——工具返回必须立刻有 preview（调用方等不起）。
- **时间驱动策展**（②）：curation 改为 Tick 同构消费器（读超界卡→凝缩/驱逐事实）。
- **预折叠**（③）：Fold 从组装路径移除——策展消费器在阈值触发时预折叠（compress_request→fold 事实），组装路径只读折叠结果。**被否**：组装时同步 Fold 保留——"读写 state"违反投影只读。

## D3 settle=事实（replay-settle 事件）

AssembleBudget 的降级决策留痕从事实链内联写改为 replay-settle 事实（消费器可审计）；"跳过不留痕"语义不变。

## D4 ④ 装箱=投影策略（不事件化）

判据：非状态变更、每次组装可重算、无持久后果。事件化=生产者等自己消费的循环＋伪事件风暴。

## D5 六面消费者声明（不改行为）

Subscribe（推消费）/Deliverer（投递消费）/Apply（裁决消费）/Shutdown（收尾消费）/Observer（观察消费，不产事实）/Exporter（导出消费，SealSnapshot=制品事实）——协议断言各一（读→产/不改源事实/身份保真）。rl 导出触发面保持授权调用（事件化收益低——裁决）。

## 验收

- 01：`^TestCompressionRequestIsAFact$`／`^TestCurationConsumesCompressionRequests$`／`^TestFoldIsPrecomputedNotInline$`／`^TestSettleLandsAsReplaySettle$`（正/负/消费者锚）＋golden 12 场景全 match（压缩产物语义等价）。
- 02：`^TestSecondWaveConsumersShareTheProtocol$`（六面各断言）。
- 03：doc-refs/lint 全绿＋doc budget 对账。
- 变更级：行为等价判据=golden 全 match＋压缩产物内容不变（同输入同 ticket/artifact/fold）。

## 风险与回退

行为重排=每机制独立提交；Fold 预折叠引入"待压缩"状态位（读路径如实呈现）；golden 对照兜底。
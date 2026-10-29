# 02 meditation-activity：冥想节奏门的 session 活跃闸

父变更：unified-intake（域 B）。

## 边界与依赖

- **依赖**：`meditation-idle-gating` 现条款（novelty/让位/水位执行语义一字不动；节奏门 MODIFIED）；`trigger_source` 读径（event-metadata-contract，接口常数）；决策席占用读面（01 域 A4 后稳定，接口常数——开发可并行，集成需其合并）。
- **被依赖方**：无。
- **接口面（零新概念）**：`Driver` 构造接收既有 `*state.Occupancy`（既有对象注入）；新鲜度判据读既有 novelty 扫描结果的 Timestamp/SelfManaged；零值=现行为。
- **禁止**：不引入时间阈值判忙闲（规格禁令保留）；自管谱系不重置冷却；让位/水位语义不动。

## 症状

「冥想要看的是 session 活跃」：现状节奏门只看执行间隔——用户刚发消息/对话进行中，冥想照跳。

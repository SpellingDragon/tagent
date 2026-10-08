# Design: D4 agent 引擎域评审

## 阅读顺序与技术要点

1. `agent-architecture.md`（主体 520 行）：runEventLoop → ContextManager 粘合 → 冥想 → 子 Agent 封装。
2. `event-flow.md`：一条消息从注入到回复的完整旅程——评审"事件驱动 vs ReAct"主张的实际形态。
3. `execution-generations.md`：执行器代际与生命周期收敛（热更换脑的引擎侧）。
4. `governance-enforcement.md`：治理分级处置与批准通道（warn|strict）。
5. `prototype-skeleton.md`（41 行）：126 行六件套原型→生产原语映射、三条不变量。

## 重点问题

- 事件驱动引擎的复杂度收益账：相对 ReAct 循环，多出的 EventBus/turn 原语/管线换来什么、付出什么（第一性原理核心考题）。
- turn 原语"统一壳"：入口循环与被调方调用环共用——同构声明是否真实，还是两套逻辑穿一件衣服。
- 冥想（meditation）默认关：反思沉淀机制的价值证据 vs 维护成本。
- 治理闸 critical 异步审批：审批请求"渗透为消息"对模型上下文的污染面（判准 R 的噪声轴）。
- 子 Agent 同构协作的调用绑定表：晚到结算路由是否确定（判准 T）。

## 代码抽查断言候选（≥2 个）

- runEventLoop 的 Pull→RunFlow 主循环（agent/ 包）；
- turn 原语统一壳的代码形态（入口与被调方是否同一函数/类型）；
- MeditationManager 空闲门控（interval/min_gap）；
- GovernanceTool 装饰器的风险分级插入点。

## 风险与回退

agent-architecture 与 event-flow 有重叠内容，交叉阅读时注意区分引擎机制与数据流叙述两层视角。

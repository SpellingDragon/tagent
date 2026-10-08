# Design: D6 外围运行域评审

## 阅读顺序与技术要点

1. `rl-architecture.md`（145 行）：端点 allowlist 与逐跳重定向（SSRF）防线、配置面与升级收紧、TrajectoryRecorder/SwappableModel/HTTPAPI 三件套。
2. `durable-delivery.md`（150 行）：持久投递与依赖退化（ReliableBus 磁盘溢出、AnchorStore、mem_spill）。
3. `wechat-bot-runtime.md`（103 行）：入站去重、窄接口、投递目标回退、大文件真链路验收——唯一"生产"运行面证据。
4. `comment-gate-tooling.md`（121 行）：注释文档门禁工具面（comment_policy 棘轮）。

## 重点问题

- 训练友好主审：轨迹是否完整含 (state, action, reward)——prompt/context 是否原样入轨迹？工具结果超大截断后轨迹还可用吗？
- SwappableModel 换模型对轨迹一致性的影响（同episode内换脑=分布漂移，RL 大忌，文档是否交代）。
- HTTPAPI fail-closed（默认 127.0.0.1）与 AReaL rollout 的实际对接形态：是文档愿景还是有 e2e 证据。
- 持久投递磁盘溢出的恢复语义与 memory 域事件链的职责重叠。
- wechat-bot 作为唯一示例的代表性：五 agent 编排是否被真实复杂度检验过。

## 代码抽查断言候选（≥2 个）

- TrajectoryRecorder 记录字段（rl/ 包：是否含 context/token usage/tool call 全量）；
- HTTPAPI fail-closed 与 TAGENT_RL_AUTH_TOKEN（rl/ 包）；
- ReliableBus 磁盘溢出实现（agent/reliability/）；
- endpoint allowlist 逐跳校验（CheckRedirect）。

## 风险与回退

全部短篇，无预算风险。RL 断言涉及 AReaL 对接形态，若代码证据不足，明确写"证据不足"而非臆断。

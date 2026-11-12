# Proposal: 11-derived-faces

父变更：hot-update-and-purity-closure（转移自 event-driven-consumption 04）

## Why

训练导出须按输出消费键集合配对（非 InvocationID），未消费输入不出样本；一次装配一次快照（工具/技能/环境/capture 同代、不跨请求缓存），供热更窗口内一致与 RL 样本保真。

## 边界与依赖

- 依赖：01 消费元数据(input_events)、05 代际快照（接口常数=装配开始代）；实现依赖 features/rl/export 与 assembly/request。
- 被依赖方：09（导出读落盘证据）。
- 接口面：features/rl/export*.go、assembly/request.go。
- 禁止事项：不改 assembly/read.go/run.go/host_*；不改 memory/**；不按 InvocationID 分组。
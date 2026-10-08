# docs-objective-review

tagent 三阶段工作索引：文档评审 → 代码探索 → 协调优化实施。

**状态：前两阶段保留完成记录；第三阶段已规划，尚未实施。** 第三阶段以 [design.md 的 D12–D20](design.md) 和 O1–O6 产品契约为准；旧总结中的P0/P1、强制trace与DAG/paused建议不是实施决定。用户选择六域协同及必要架构调整、采集保真加离线SFT消费，并要求本地真实模型测试通过。

- 一级编排：[tasks.md](tasks.md)、[设计](design.md)、[过程契约](specs/orchestration/spec.md)。
- 历史域：D1–D6、X1–X6；报告位于 docs/.dev/，仅作假设与取证记录。
- 第三阶段：6域×4件套、48个叶任务；I0前置→独占文件并行→单写集成→真实验收。

| 子变更 | 交付 |
|---|---|
| [O1](changes/O1-fact-consistency/proposal.md) | 事实提交、因果顺序与回溯partial |
| [O2](changes/O2-runtime-coherence/proposal.md) | 既有模型覆盖接线、真实热更与明确拒绝 |
| [O3](changes/O3-request-efficiency/proposal.md) | 完整请求预算、共享声明快照与摘要时限 |
| [O4](changes/O4-storage-efficiency/proposal.md) | 分区扫描/解码/编码优化，提交保证不变 |
| [O5](changes/O5-capture-fidelity/proposal.md) | 可选SDK决策保真、精确关联、丢失与封账 |
| [O6](changes/O6-training-export/proposal.md) | 授权离线导出、工具SFT模板与会话分割 |

真实模型/tokenizer缺条件或用例Skip不算实施完成；性能以同条件before/after为证。当前只改OpenSpec计划，未运行实施测试、模型或训练。产品delta后续按D20提升为独立change再正常归档；父过程契约不写入产品主spec。

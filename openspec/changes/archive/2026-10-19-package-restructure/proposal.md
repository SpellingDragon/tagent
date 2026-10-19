# Proposal：包结构重排（冥想归位＋memory 拆包＋分包判据）

## Why

深度审计（2026-10-19）确认：冥想（=curation 旧名，wiki"命名沿革"铁证）被塞进 memory 导致概念退化（正名沦为"已消化标记集"）；memory 是杂物抽屉（至少六个语义单元）；票据语义分裂（reference 与 externalize 两处）。用户判据：**按职责领域分包，每个领域包下挂三拓展点子包（业务/机制一眼分清）；不强行复活旧概念（简单才是美）；设计先行**。

## What Changes（收缩版——只做两件小的＋判据固化）

1. **冥想归位**：`memory/curation.go` 等迁至 `evolution/meditation/`（自进化引擎轴：冥想×refine×consolidation 三通道同循环——wiki 原始公式）；正名 meditation；`replay_budget` 的 `meditation` 标记集改名 `consumed`（归还语义）。
2. **memory 拆包**：ledger（事实存储：reference/lifecycle/retention/guard/localfile/rustviking）｜recall（search）｜replay（replay_budget/replay_settle）——projection 归 query 或留 memory（按依赖面定，D1 裁决）；票据语义并入 ledger。
3. **判据固化**：CONTRIBUTING 包结构节（机制子包只含三类文件：事件词汇/消费接线/扩展策略；第四类=业务逻辑归领域包）＋新包准入三问。
4. **不复活登记**（边界句，wiki 各一处）：mem_spill/error_tracking 设计已放弃；Summary 名不副实加语义注释（不是总结，是原文/工具行）。

## Capabilities

### New
- `package-conventions`：分包判据与机制子包挂法。
### Modified
- `fact-vocabulary`（meditation 正名/consumed 改名）、`consumer-topology`（包归属更新）。

## Impact

类型搬迁（curation/压缩面/search/存储面）＋引用更新；行为零变化（golden 12 场景全 match 判据）；architecture.json/hygiene/gen_godoc 登记同步。
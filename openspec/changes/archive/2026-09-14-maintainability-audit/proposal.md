# maintainability-audit — 提案

## Why

项目六周内经历三轮高强度演进（D1-D5 平台子系统、R1-R4 常驻连续性、build ownership 类型化），dev 单支三天 +7024 行；复杂度账单开始显影——生产首现 panic（2026-09-13 pruneTerminal nil detector，根因是「重建任务无 detector」与「zombie 退役路径」两个新件相遇）、三份外部评估（deep-agent 分析 / 行业趋势评析 / 迭代设计报告）均点名「复杂度已多次反噬作者本人」，且主 specs 曾实证漂移（c5399e2 纠正 24 项疑似失真）。R1-R4 已稳定落地、活跃变更清零，正是系统性盘点可维护性的窗口：**现在不审计，债息随下一轮迭代复利**。

## What Changes

- **逐包逐文件审阅**当前实现（main @ cf006e1），覆盖 20 个 Go 包：根包（tagent/build_agent/wiring/config/testing/org_hotreload）、agent、agent/task、agent/compress、agent/governance、agent/reliability、memory、memory/engine、memory/kv、memory/embedder、plugin、event、prompt、evolution、rl、tool/action、tool/mcp、tool/memoryx、tool/plan、tool/task、tool/govx；
- **逐包可维护性评分卡**，五维评级（复杂度 / 耦合与依赖方向 / 测试覆盖与质量 / 文档-spec-代码一致性 / 演进风险），每维 S/A/B/C 定级并附依据；
- **发现登记**：问题按 🔴（致错/致损）/ 🟠（漂移温床）/ 🟡（卫生债）三级分类，每条带 file:line 证据与复现/影响路径；
- **可维护性改进 backlog**：发现汇总为候选变更清单（每条标注建议立案形态：修复 / 重构 / 守护测试 / 文档），**本次只审计不实现**——改进动作各自独立立案；
- 产出落 `openspec/changes/maintainability-audit/audit/`（逐包报告 + 汇总台账），审计方法契约升格为主 spec。

## Capabilities

### New Capabilities

- `maintainability-audit`：逐包可维护性审计的产出契约——五维评分定义与定级标准、发现三级分类与证据要求（file:line 必须）、逐包报告与汇总台账的结构、审计只读性约束（不修改生产代码）、fail-before 之外的「发现可验证性」要求（每条发现须有代码引用或行为证据，禁止凭印象论断）。

### Modified Capabilities

（无——本变更为只读审计，不改变任何既有行为规格。）

## Impact

- **零生产代码改动**：审计全程只读（读码、跑测试、跑 race/vet 为取证手段，不改行为）；
- **产出物**：`openspec/changes/maintainability-audit/audit/` 下逐包报告（约 20 份）+ `summary.md` 汇总台账 + `backlog.md` 改进清单；`openspec/specs/maintainability-audit/spec.md` 方法契约；
- **后续影响**：backlog 条目将驱动独立的修复/重构变更（各自走完整 openspec 流程）；🔴 级发现建议标注优先处置窗口；
- **依赖**：无新增外部依赖；审计使用既有工具链（go vet / go test -race / grep / git log 考古）。

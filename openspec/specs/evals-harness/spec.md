# evals-harness Specification

## Purpose

组件级行为评估 harness:evals/ 一等目录、票据可召回率 roundtrip、工具选择 op 白名单守护、Bad Case 资产化(tests/README 教训转回归)、suites YAML 声明化——mock 可跑进 CI。
## Requirements

### Requirement: 票据可召回率评估
evals SHALL 提供票据可召回率组件评估:构造事件→触发压缩→收集卡片 key→逐 key 经 recall 取回→断言原文一致;纯工程实现(mock 可跑,进 CI)。

#### Scenario: 压缩后票据全量可召回
- **WHEN** 运行 ticket-recall suite(构造 N 条事件压缩后逐 key recall)
- **THEN** 全部 key 取回原文(成功率 100%;失败即 eval 红)

### Requirement: 工具选择评估与 Bad Case 资产
evals SHALL 提供工具选择组件评估(mock model 注入期望 tool_calls 断言路由)与 Bad Case 回归资产(tests/README 真实教训转为用例,如 hex 断裂 key 断言拒绝静默空结果)。

#### Scenario: Bad Case 回归守护
- **WHEN** cases/ 中 hex 断裂用例运行(畸形 key 格式 recall)
- **THEN** 断言显式拒绝/错误提示,而非静默空结果——历史教训不复发

# unified-tool-registry Specification（delta：端口候选注记预留）

> 本文件为 MODIFIED delta。恰六端口闭合契约与注册治理条款不变；新增"候选端口注记预留"条款（D12，Q14=b）。

## ADDED Requirements

### Requirement: 候选端口注记预留

插件端口表 SHALL 可登记"候选端口"注记（候选名、一句话职责、准入条件、不占位声明）；注记 MUST NOT 改变恰六闭合契约（`validatePort` 仍拒绝第七个实际端口）。候选端口在其准入条件（工作关联核心机制成型）满足前 MUST NOT 被实现或注册。

#### Scenario: 注记不占位

- **WHEN** 端口表登记了 route 候选注记
- **THEN** 注册第七个实际端口仍被 `validatePort` 具名拒绝

#### Scenario: 候选未达准入不实现

- **WHEN** 工作关联核心机制未成型
- **THEN** route 端口无实现、无注册点（仅注记存在）

# Proposal: 02 摘要策略热参补维（summary-knobs-hotparams）

## Why

域 01 审计若定谳摘要策略 knob"两不沾"（既不在 fp 子集、也不在 OrgHotParams 源），则摘要设置是用户清单中唯一的源面真缺口——热变更今日不生效。按归一机制，补法唯一：**源加字段+消费点读**，一处接线，零新机制。

## What Changes

- `OrgHotParams` 扩摘要 knob 字段（字段名以域 01 结论为准——接口常数）；`hotParamsFor` 解析；`SmartCompressor`/`ContextCompressor` 拉取契约扩；消费点（摘要动作发生处）读取。
- 消费点契约测：fail-before（改前红=域 01 的现状行为测试）→ 补维后绿（热变更后下一次摘要动作即用新值）。
- 矩阵增行登记（沿用域 01 的矩阵纪律）。

## 边界与依赖

- **依赖**：域 01 的摘要归属结论（接口常数：字段名+解析点+消费点）。**启动门：01 结论入 main 前本域实现类孙任务不得启动**。
- **被依赖方**：无（域 03 正交）。
- **接口面**：`OrgHotParams` 公共结构加字段（向后兼容：零值保持现行为）；config schema 加可选字段。
- **禁止事项**：不触碰三禁区；不新增 push 通道（摘要 knob 只走源拉取）；若域 01 裁定摘要归属 fp 面（prompt 模板一部分），本域改判为"仅补契约测"，实现任务缩水为 2.4 一条。

## Capabilities

### Modified Capabilities

- `config-hot-reload`: +1 需求「摘要策略热参」。

# Design: 01 热更矩阵审计与契约化

## 技术要点

- **矩阵取证法**：每维度四问——① 配置落点（schema 字段→config 结构）；② 通道归属（fp 子集？OrgHotParams？两不沾？）；③ 消费点（谁在何时机读）；④ 现有契约测（有/无）。证据=代码行号或测试名，禁止推读。
- **假热更红线测**：对每个"源携带"维度断言"消费点读到的是解析值"（如 `OrgBudgetLine()==maxTokens×threshold` 的既有先例）；对每个"fp 面"维度断言"变更触发代际推进"（fingerprint 变化）。摘要 knob 若两不沾，写一条**现状行为测试**（热变更后摘要行为不变——为域 02 提供改前红基线）。
- **文件清单（预期）**：`agent/org/*_test.go`（fp 维度断言）、`agent/compress/*_test.go` 或 `agent/context_manager_test.go`（源维度断言）、矩阵表入本域 spec delta 与 `docs/wiki/`（若 wiki 已有热更页则补表，无则入 spec 即可）。

## 风险与回退

- 风险：维度枚举遗漏（矩阵不完整）→ 以用户原始清单（prompt/上下文大小/压缩摘要/模型参数/模型ID/路由）+ config schema 全字段反查双重核对。
- 回退：纯增量（测试+文档），无回退面。

## 摘要 knob 取证三条路（按序排查）

1. `config.AgentConfig` / 全局 config 中摘要相关字段（summary/digest/abstract 关键词全仓搜）；
2. `SmartCompressor`/`ContextCompressor` 构造参数中的摘要策略项及其 config 来源；
3. prompt 侧摘要指令（若摘要是 prompt 模板一部分，则归属 fp 面——域 02 随之改判为"已热，仅缺契约测"）。

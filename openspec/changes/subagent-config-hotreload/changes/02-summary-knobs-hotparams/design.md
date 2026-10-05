# Design: 02 摘要策略热参补维

## 技术要点

- 接线五件套（一处加字段的完整闭环，全部沿既有骨架）：
  1. config schema 可选字段（零值=现行为，KnownFields/strictyaml 兼容）；
  2. `hotParamsFor` 解析进 `OrgHotParams` 新字段；
  3. 源自然携带（`SetHotSource`/记录轮转无需改动——字段在 params 结构内自动流转）；
  4. 消费点读取：摘要动作发生处（域 01 结论指定——大概率 `SmartCompressor` 摘要分支或 `ContextCompressor` 摘要回调），拉取形态与 `budget()` 同构；
  5. 诊断回执字段扩展（`OrgAgentApply`/日志行携带新 knob，desired/effective 可见）。
- 生效语义（对齐源面纪律）：变更后**下一次摘要动作**即用新值；在途压缩动作不回溯。
- fail-before：域 1.4 的现状行为测试即改前红（热变更后摘要行为不变→红基线）；补维后同测转绿。

## 风险与回退

- 风险①：摘要 knob 若影响摘要产物的**持久格式**（如分层摘要结构），热换可能造成新旧混排——域 01 取证时确认；若属实，字段语义限定为"下一次摘要生成起生效，存量摘要不重写"。
- 风险②：回滚路径（rollback 闭包走 `applyHotAll` 换源）自动携带新字段——验证测一条回滚后摘要 knob 恢复。
- 回退：字段零值=现行为，回退=revert 单提交。

## 文件清单（预期）

`config`（schema）、`agent/context_manager.go`（OrgHotParams+hotParamsFor）、`agent/compress/context_compressor.go` 与 `agent/compress/smart_compressor.go`（拉取契约）、`tagent.go`（回执字段）、对应三处测试文件。

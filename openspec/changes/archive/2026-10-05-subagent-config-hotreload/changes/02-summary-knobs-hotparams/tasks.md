# Tasks: 02 摘要策略热参补维

> **启动门**：1.x 全部完成且摘要归属结论入 main 后，方可开工 2.1-2.3。

- [ ] 2.1 schema+解析：config 可选字段 + `hotParamsFor` 解析进 `OrgHotParams` 新字段（字段名=域 01 接口常数；零值=现行为） —— 验证：`go build ./... && go test ./agent/ -run 'HotParams' -count=1` exit 0
- [ ] 2.2 消费点拉取：摘要动作处读取新 knob（与 `budget()` 同构拉取）；诊断回执/日志行携带 —— 验证：`go test ./agent/compress/ -count=1` exit 0
- [ ] 2.3 fail-before→绿：域 1.4 现状行为测试转绿（热变更后**经真实压缩动作路径**触发的摘要使用新值，非直调摘要函数）+ 回滚恢复测（rollback 后 knob 回旧值）+ 在途不回溯测 —— 验证：`go test ./agent/... -run 'Summary|Digest|Rollback' -count=1` exit 0（-v 核对三形态）
- [ ] 2.4 矩阵增行 + spec delta + 全量回归：矩阵摘要行更新为"源拉取面+消费点+测试名"；`config-hot-reload` ADDED「摘要策略热参」；全包净 —— 验证：`go test ./... -short -count=1` exit 0
- [ ] 2.5 （条件）若域 01 裁定摘要归属 fp 面：本域缩水为仅 2.4（契约测+矩阵行），2.1-2.3 记 N/A 并注明裁决出处 —— 验证：tasks 注记存在且 2.1-2.3 标注 N/A

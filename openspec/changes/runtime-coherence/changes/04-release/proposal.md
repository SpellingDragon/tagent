# 04 · 放行（release）

## 边界与依赖

- **父变更**：runtime-coherence（依赖 03.304 完成）。
- **依赖**：02、03 的正式入口证据；现行 CI 作业与 `golden/runtime/event_integrity_scenarios.json`；归档 CARRYOVER。
- **被依赖方**：F1–F4。
- **接口面**：无产品接口；只补测试、golden、CI 与证据。
- **禁止**：改判定标准放宽失败、改旧证据对齐新树、跳过作业、以本地绿宣称远端绿。

## 范围

生命周期与故障矩阵；长期成本与完整矩阵；真实 CI 接线（含 archive-preflight 的 OpenSpec CLI 与 evidence-binding）；独立审与 F1–F4。旧账未闭合项在此终裁。

## 完成定义

- 挂载/换代/禁用/卸载/重启均有正式入口证据；贡献与 builtin 同规则。
- 十短、六长、变异、三 module、race、smoke、openspec、归档前拒绝在冻结候选上全绿；CI 与本地口径一致。
- 独立审 frontier 空、扫描范围与未查面具名；S1 红转绿且回滚即红；旧账逐条结论；归档后登记后继指针。

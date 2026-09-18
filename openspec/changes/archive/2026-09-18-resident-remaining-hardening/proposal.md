# Proposal: resident-remaining-hardening

## Why

前序 change `resident-readiness-plan` 已归档（2026-09-17，44/69），剩余 25 项未随主收口落地，它们分为两类：

1. **实测驱动的压缩回收优化（6.7）**：远端 trajectory 实测 task_settled 结算风暴占上下文 67.8%，external_input 不在工具对折叠范围，结构性不可回收——每次压缩回收仅 ~3%，且 24h 基线确认压缩后 floor 持续 72.4%。这是当前常驻运行的最高优先性能债。
2. **发布前置证据与文档（5.7 / 6.1–6.6 / 7.x / 8.x）**：票据守卫、离线 benchmark、综合 E2E/长跑/真实模型/独立审查、中英文档与升级演练——全部为发布候选所必需，且多数只能在其前置（WP1 屏障已落地）之上执行。

此外归档时明确登记的 follow-up：Major 5 的 CheckRedirect 逐跳校验（原降级文档化）在本 change 内补齐实现。

本 change 将上述内容重整为可独立执行的四批次，继承前序 change 的两轮 cold-eyes 审查基线与全部既有测试资产，不重复已闭环工作。

## What Changes

- **批次 A｜压缩回收提升（6.7 展开 + Major 5）**：① reconcile/orphan 批量退役汇总为单条 external_input（事件数 N→1，结算风暴源头限流）；② settled 类 external_input 纳入压缩票据化折叠（卡片行 + [evt_key] 票据，原文冷存可 recall）；③ chars/token 估值器按 ② 后实测另案决策；④ LLM client 安装 CheckRedirect 逐跳 allowlist 校验（消除已文档化的重定向限制）。
- **批次 B｜票据守卫与基准（6.1–6.6）**：卡片反例模型、curateCards key 集合校验、超预算/不可表达状态、组合回归、离线 benchmark（1k/10k/100k 事件 × fsync 两档）、性能回归对照。
- **批次 C｜综合验证（7.1–7.6 可本地执行；7.7–7.9 授权类 BLOCKED 标注）**：soak 骨架子进程化、30 轮快速 E2E、任务完整链、开关组合回归、CI 扩展（双模块 race/Python 负例/OpenSpec strict）、有界 fuzz 与错误注入。
- **批次 D｜准出与文档（5.7 / 7.10 / 8.1–8.6）**：部署夹具安全验证（需环境）、准出对照表、中英文档/wiki 同步、死引用清理、升级/回滚演练、strict 校验与发布候选 checklist。

## Impact

- **Affected specs**: task-skeleton-compression（settled 票据化折叠新语义）、persistent-event-loop（CheckRedirect 逐跳校验约束）。
- **Affected code**: agent/task（批量退役汇总）、agent/compress（settled 折叠）、rl/（CheckRedirect）、scripts/CI、tests/、README/docs/wiki。
- **非目标**：不重做已闭环的 WP0–WP4；不引入 ANN/BM25/数据库替换；7.7/7.8/7.9 授权类任务在本 change 内保持 BLOCKED 并显式标注，不伪造证据。
- **提交/推送/部署**：沿用 dev 分支流程，tag 与真实部署仍等待授权。

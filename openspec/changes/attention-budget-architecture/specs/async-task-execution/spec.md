# async-task-execution Delta

## ADDED Requirements

### Requirement: spawnGate 的审计触发源与豁免放行

spawnGate 判定链 SHALL 支持行为审计触发源（self-telemetry-audit L3 冻结）：语义与 disk 退化 block 完全同构——进行中任务不受影响、新 spawn 返回 Blocked 并携带审计 reason。冻结期间，声明为保护性类别的任务（retention guard / mem_spill 重放 / 重试修复）MUST 被豁免放行；豁免依据任务声明（构造时确定），运行时不可自行标注。

#### Scenario: 审计源与 disk 源并存

- **WHEN** disk 退化与审计冻结同时生效
- **THEN** 两者 reason 分别如实呈现；豁免类任务仅在 disk 源下受检（审计源不拦截保护性任务）

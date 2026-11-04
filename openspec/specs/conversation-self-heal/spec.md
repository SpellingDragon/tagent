# conversation-self-heal Specification

> **复核注记（2026-11-04 逐模块核对）**：本规范对已退役符号（`TagentAgent` / `EventBus` / `SessionProjection` / `PlainTool` / `SwappableModel` 等，全仓 Go 0 命中）的引用均为 MUST NOT 禁止项、历史实现注记或迁移期条款，**无现行 SHALL/MUST 建立在已退役符号上**；现行契约以 `assembly` 组合根 + 每地址常驻消费循环 + 装配期按声明分派为准。逐条映射见 `openspec/changes/archive/2026-11-03-runtime-architecture-convergence/evidence/10-doc-gap-register.md`。

## Purpose
会话投影自愈：投影按 EventKey 幂等追加、压缩替换与键集一致，重复来源可见且不再撕裂上下文。
## Requirements
### Requirement: 投影按 EventKey 幂等追加

`SessionProjection` 追加事件引用时 SHALL 按 `EventKey` 幂等：当一个 `EventKey > 0` 的引用已存在于投影中时，重复追加 SHALL 被跳过（不产生第二份），并 SHALL 记录一条 warning 以便观测重复来源。`EventKey == 0`（未编号事件）SHALL NOT 参与去重。压缩替换（`Replace`）后，已见 key 的判定 SHALL 与替换后的投影一致。

#### Scenario: 同一事件重复投影被跳过

- **WHEN** 一个 `EventKey > 0` 的事件引用被追加两次
- **THEN** 投影中该 key SHALL 只保留一份
- **AND** 第二次追加 SHALL 被跳过并记录 warning

#### Scenario: 未编号事件不去重

- **WHEN** 追加多个 `EventKey == 0` 的引用
- **THEN** 它们 SHALL 全部保留（不因 key 相同而误合并）


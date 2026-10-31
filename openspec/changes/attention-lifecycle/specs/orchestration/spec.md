# orchestration Specification

## Purpose

本规格是一级变更 attention-lifecycle 的编排契约：启动门、波次解锁、共享面契约
守护与变更级 DoD。领域验收契约下沉在各二级域的 specs/ 下（attention-primitive、
approval-attention、attention-clearance）。

## Requirements

### Requirement: 启动门未过不得派发

编排者在启动门（用户裁定：attention 与 PR #33 并行推进、在同一分支汇合后
一并合并；实施基线 = 含 PR #33 内容的合并线）核实通过前，SHALL NOT 派发
任何二级域实施单；门未过时的唯一动作是把门的状态写入 ORCHESTRATION.md
台账。

#### Scenario: 门未过时派发被拒

- **WHEN** ORCHESTRATION.md 台账中启动门仍为未过
- **THEN** 任何二级域的实施派发 SHALL 不发生，且台账 SHALL 记录门的状态与核实命令

### Requirement: 共享面由冻结方单写

`api/event` 的类型与 meta 键 SHALL 由 01 域单写并在其入 next 后冻结；此后任何
二级域对冻结文件的改动 MUST NOT 入链，除非编排者以新派发单显式解冻。

#### Scenario: 冻结后的越界改动被拒

- **WHEN** 01 域已入 next 且某 W2 派发单白名单不含 `api/event`
- **THEN** 该域对 `api/event` 的任何改动 SHALL 不入链，违规单 SHALL 重开

### Requirement: 波次解锁以入 next 为准

波次 SHALL 仅作汇报分组；一个二级域的解锁条件 SHALL 是其全部前置域已合入
next，MUST NOT 以"前置域本地绿"替代。

#### Scenario: 前置仅本地绿不解锁

- **WHEN** 01 域在派发分支上全绿但未合入 next
- **THEN** 02 与 03 域的实施派发 SHALL 不开始

### Requirement: 变更级 DoD 以两证与全量门为准

变更级完成 SHALL 同时满足：两条症状锚红转绿各两证（回滚态必红、落地态必绿）、
全量项目门 exit 0、`openspec archive` 完成；缺任一项 SHALL 视为未完成。

#### Scenario: 缺红锚两证的完成主张被拒

- **WHEN** 某症状锚只有落地态绿证而无回滚态红证
- **THEN** F3 SHALL 不勾选，变更 SHALL NOT 归档

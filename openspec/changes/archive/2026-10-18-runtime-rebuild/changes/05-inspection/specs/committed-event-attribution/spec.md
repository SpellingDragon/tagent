# committed-event-attribution Specification

## Purpose

本 delta 要求 Query **只报告已提交事实**并报告源水位/滞后：未提交事实不得出现在读面，回溯不完整须可辨。

## ADDED Requirements

### Requirement: Query 只报告已提交事实与水位

Query SHALL 只呈现**已成功提交**的事实（可召回票据）；未提交/提交不确定的条目 MUST NOT 出现在读面为"可用"。Query SHALL 报告源水位与滞后，MUST NOT 把多维状态相加、MUST NOT 声称跨所有后端原子。

#### Scenario: 未提交不出现在读面（失败）

- **WHEN** 同因果域一次写失败
- **THEN** 失败键不进入读面投影
- **AND** 下一条成功后其票据可被 Query 取回

#### Scenario: 报告滞后（并发）

- **WHEN** 某源滞后于快照 revision
- **THEN** Query 携带该源水位与滞后，不声称全局原子

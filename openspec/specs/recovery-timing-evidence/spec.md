# recovery-timing-evidence Specification

## Purpose
本规范定义 recovery-timing-evidence 能力：恢复路径的分段计时观测——五次 Scan 与 fold 的计数/耗时在 boot 观测中可读，为恢复耗时诊断与优化提供可判定的测量证据。
## Requirements
### Requirement: recovery-timing-evidence 契约

恢复路径 SHALL 暴露五次 Scan 与 fold 的分段计数/耗时；测量锚 SHALL 在阶段二先行产出并留基线红证。

#### Scenario: 主路径

- **WHEN** 一次重启发生
- **THEN** boot 观测含 5 段 Scan 计数与 1 段 fold 计数


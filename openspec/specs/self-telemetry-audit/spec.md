# self-telemetry-audit Specification

## Purpose
自管遥测审计：滚动窗口占比指标驱动告警、收敛、冻结三级动作，指标与每次动作写入事实链供 recall 审计，判定为确定性计算、零 LLM 参与。
## Requirements
### Requirement: 自管遥测占比指标与分级动作

系统 SHALL 维护滚动窗口内的「自管遥测 / (自管+环境事件)」占比指标，超阈值触发分级动作：L1 告警事件（按未消费遥测的完整形态投递，宿主必见全文理由）→ L2 收敛自管 spawn 频率 → L3 冻结自管 spawn。占比指标与每次分级动作 SHALL 写入事实链（可 recall 审计）。指标判定为确定性工程计算，零 LLM 参与。

#### Scenario: 空转检出与收敛

- **WHEN** 滚动窗口内自管遥测占比超过阈值
- **THEN** 先触发 L1 告警（完整投递）与 L2 频率收敛；收敛有效则占比回落、不升级

#### Scenario: 收敛无效升级冻结

- **WHEN** L2 收敛后占比仍持续超阈
- **THEN** 触发 L3 冻结，动作记录（级别、占比值、窗口样本数）入事实链

### Requirement: L3 冻结与 disk block spawn 同构

审计 L3 冻结 SHALL 复用 spawnGate 判定链（与 `degradation_disk_block_spawn` 同一道闸、新增触发源），自动执行、无人工审批（宿主裁决 2026-09-27）：进行中任务不受影响，新 spawn 被拒且文案如实（区别于 disk 退化的 reason）。冻结解除 SHALL 在占比回落至阈值以下后自动进行。

#### Scenario: 冻结语义与 disk 一致

- **WHEN** 审计冻结生效期间有新 spawn 请求
- **THEN** 返回 Blocked 并携带审计 reason；在飞任务的 settle/轮询/续命不受影响

#### Scenario: 自动解除

- **WHEN** 占比回落至阈值的一半以下（迟滞释放，防止临界窗口反复开合闸门）
- **THEN** 冻结自动解除，新 spawn 恢复，解除记录入事实链

### Requirement: 保护性任务豁免白名单

冻结期间，保护性任务（资源租约/retention guard、mem_spill 重放、重试修复类）SHALL 继续放行——耐久性防线 MUST NOT 因注意力治理而撤除。豁免类别 SHALL 以声明确认（任务声明为保护性），不可由模型运行时自行标注。

#### Scenario: 冻结期的耐久性保全

- **WHEN** 冻结生效且磁盘退化触发 mem_spill 重放需求
- **THEN** 该保护性 spawn 被放行，重照常执行


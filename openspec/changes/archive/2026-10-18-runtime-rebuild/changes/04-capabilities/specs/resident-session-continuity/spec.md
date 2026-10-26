# resident-session-continuity Specification

## Purpose

本 delta 强化 exec/tmux 常驻会话在重建执行路径下的**外部生命周期**：停止凭证、重挂、TTL/续行与资源租约，`unknown≠dead` 且停止未确认不得伪称完成。

## ADDED Requirements

### Requirement: 外部执行停止凭证与真实停止确认

exec/tmux 派生的外部执行 SHALL 具备可判定的**停止凭证**（能证明目标进程/会话已真实停止）；SHALL 区分 `alive`/`dead`/`unknown` 三态（`unknown≠dead`），停止未确认时 MUST NOT 把任务标为已收束。**连续 `unknown` MUST NOT 作为判死/退出依据**：此时未止义务与**资源租约 SHALL 保留且可解释**（谁推进、缺何证据），系统仅可**升级诊断**或发起**显式停止处理**以取得真实停止凭证；MUST NOT 以次数阈值（如连续 3 次）代真实停止凭证。

#### Scenario: 停止凭证确认

- **WHEN** 一个 tmux 会话被请求停止且停止命令成功
- **THEN** 系统 SHALL 生产一份可复核的停止凭证（命令退出状态 + 会话不再列出）
- **AND** 任务是"已停止"的判定必须以此凭证为准

#### Scenario: unknown 不屠杀（失败）

- **WHEN** 存活探测命令返回 error（服务抖动）
- **THEN** 判定为 `unknown`，会话与未止义务/租约保留
- **AND** 连续多个 `unknown` SHALL 只**升级诊断告警**或触发**显式停止处理**（走可复核停止凭证），MUST NOT 以次数阈值（如 N=3、默认 3）直接判 `dead`，MUST NOT 代真实停止凭证

#### Scenario: 停止未确认不得伪称完成（失败）

- **WHEN** 停止请求发出但无法确认目标已退出
- **THEN** 任务 MUST NOT 被标记为完成；须返回未收束原因

### Requirement: 会话生命周期与资源租约

常驻会话的 spawn/重挂/续行 SHALL 与**资源租约**绑定：会话元数据（Name/Mode/Command/Origin/TaskID 等）SHALL 持久入事实链；资源租约在会话真实停止后按既有退役通路释放恰一次。

#### Scenario: 重启后重挂并复用租约身份

- **WHEN** 重启后重挂一个存活的 named 会话及其 active 任务
- **THEN** 会话参数可从事实链回读并重建监控
- **AND** 关联任务经存活探测裁决恢复或走 settle 补偿

#### Scenario: 长驻会话无年龄死刑（并发/重启）

- **WHEN** 一个超过 24 小时、仍有任务引用的 resident 会话经历重启重挂
- **THEN** 该会话 MUST 被收养继续存活，MUST NOT 被当作孤儿清理

#### Scenario: TTL/续行锚点

- **WHEN** 对已脱离前台的会话执行写入型重入（send/resume）
- **THEN** 其 TTL 锚点重置为本次重入时刻
- **AND** 只读 `peek` 不刷新锚点

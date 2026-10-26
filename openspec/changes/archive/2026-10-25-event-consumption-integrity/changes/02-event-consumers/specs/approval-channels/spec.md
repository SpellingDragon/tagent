## MODIFIED Requirements

### Requirement: 审批消息流抽象

慢道审批 MUST 以 ApprovalChannel 接口统一（Deliver(request) error）；审批状态机 requested→responded(approve/reject)→consumed；审批请求仍经 ApprovalManager 落盘（digest 绑定不变）。通道投递失败不阻塞审批门（闸不是墙）。

审批请求 MUST 有可判定的有效期（现行默认 30 分钟，以 ApprovalManager 既有配置为准），回应 SHALL 在有效期边界读取当前有效状态：过期后的回应 MUST 被具名拒绝，MUST NOT 转入 consumed、MUST NOT 执行被审批动作，也 MUST NOT 恢复为待执行义务（Hold）；过期请求 SHALL 以可辨的 expired 有效状态对外可见。审批绑定 MUST 覆盖被执行动作的参数摘要（digest）：批准之后待执行动作的参数发生变化时，旧批准 MUST NOT 被沿用，该动作 MUST 重新走审批并以新 digest 落盘，MUST NOT 以「同一工具/同一目标」复用旧批准。同一 digest 的重复或并发回应 SHALL 幂等，且被批准动作 MUST 只执行一次。

#### Scenario: 状态机流转与幂等

- **WHEN** 同一 digest 被重复回应 approve
- **THEN** 首次回应生效（responded→consumed），后续重复回应为幂等无副作用

#### Scenario: 过期回应不得执行（失败）

- **WHEN** 审批请求超过有效期后收到 approve 回应
- **THEN** 该回应 MUST 被具名拒绝，有效状态 SHALL 为 expired 而非 consumed
- **AND** 被审批动作 MUST 零执行，且 MUST NOT 作为待执行义务被恢复

#### Scenario: 改参不得沿用旧批准（失败）

- **WHEN** 已批准动作在真正执行前其参数（digest 覆盖面）发生变化
- **THEN** 旧批准 MUST NOT 被用于执行，该动作 MUST 重新审批
- **AND** 新审批 SHALL 以新 digest 落盘，MUST NOT 复用旧回应对新参数生效

#### Scenario: 批准后只执行一次（并发）

- **WHEN** 同一 digest 的 approve 回应重复或并发到达
- **THEN** 被批准动作 MUST 只执行一次，后续回应无副作用
- **AND** 执行记录 SHALL 以 digest 可与回应逐项对账

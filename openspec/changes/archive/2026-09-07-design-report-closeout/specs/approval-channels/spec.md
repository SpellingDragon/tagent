# approval-channels 规格增量

## ADDED Requirements

### Requirement: 审批消息流抽象
慢道审批 MUST 以 ApprovalChannel 接口统一（Deliver(request) error）；审批状态机 requested→responded(approve/reject)→consumed；审批请求仍经 ApprovalManager 落盘（digest 绑定不变）。通道投递失败不阻塞审批门（闸不是墙）。

#### Scenario: 状态机流转与幂等
- **WHEN** 同一 digest 被重复回应 approve
- **THEN** 首次回应生效（responded→consumed），后续重复回应为幂等无副作用

### Requirement: CLI 批准入口
`tagent approve <digest> [--reject]` MUST 直接对 approvals 目录 pending 请求写回应文件，零新服务器进程。

#### Scenario: CLI 批准慢道发布
- **WHEN** 慢道发布等待 approve 且运维执行 `tagent approve <digest>`
- **THEN** 下次 Check 扫描到回应文件，发布继续

### Requirement: 微信消息注入入口
pending 审批 MUST 经 EventBus 发布 external_input（source=approval）渗透给用户；用户回复 `approve <digest>` / `reject <digest>` 由 wechat-bot 侧 listener（解析纯函数由框架提供）写回应文件。

#### Scenario: 审批请求送达与回应
- **WHEN** 慢道产生 pending 审批且微信通道在线
- **THEN** 用户收到含短 digest 的审批请求消息；回复 approve/reject 后审批状态流转，重复回复幂等（已 consumed 不重复生效）

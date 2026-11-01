# Design: 02-approval-onto-attention

## 技术要点

- 真源切换：`ApprovalManager` 内部 `index map[string]*ApprovalRequest` 改为
  attention `Present`/`Commit` 的读写适配；`ApprovalRequest` 结构与公共 API
  签名不动（调用方零改动），`ApprovalState`（created/expires/status/decidedBy）
  由 question+answer 事实对推导。
- 兼容文件面：`rebuild()`（rescan）从"读文件为真源"改为"外部裁决入口"——文件
  里的裁决写落为 attention answer 后撤下文件；文件仅作为无人值守 approver 的
  通道，不再参与在场判定。
- governance 门（policy.go）：`p.approvals.Lookup/Request` 的调用点不改，语义
  由 manager 内部承载；digest 匹配从"索引键"变为"question lineage 逐字比对"。
- fuse seam（runtime/agent/fuse.go）与 HoldBook（runtime/task/hold.go）：
  `Request`/`Lookup`/`Resolve` 全部落到 attention；`HoldBook.answers` 的幂等
  （同 ApprovalID 首答生效）由 attention answer 的同键拒二落承载。
- 过期：读侧判定（01 域时钟注入），`effectiveStatus` 语义平移，无后台扫描。

## 文件清单（以启动门时 next 为准，派发单冻结）

- `runtime/task/approval/approval_store.go`、`approval.go`（真源切换）
- `runtime/task/hold.go`（HoldBook 归并）
- `runtime/agent/fuse.go`（seam 注释与语义对齐，签名不动）
- `features/evolution/governance/policy.go`（读面语义核对，不改调用协议）
- `tests/integration/host/attention_approval_gate_test.go`（新，正/负锚）
- `tests/integration/execution/attention_approval_batch_test.go`（新，熔断批）

## 风险与回退

- 回退面：manager 保留"文件直读"旧路径为编译期开关外的并存实现（同 PR #33 熔断
  的并存迁移先例），回退后行为与基线零 diff。
- mutation registry 锚在 approval 文件面的锚点需随真源切换重锚（登记表具名），
  kill 语义不缩水。
- wechat 审批冒烟在 03 人侧接线后回归（本域消费者锚以 integration 为准）。

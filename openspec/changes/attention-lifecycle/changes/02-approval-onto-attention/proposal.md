# Proposal: 02-approval-onto-attention

> 父变更：attention-lifecycle（依赖 01-attention-primitive 入 next）。

## Why

审批是"向人要答案"里最重的一条：文件目录 + 内存索引为真源、自有投递通道、
自有 hold/answer 两张表（`HoldBook`、fuse 的 `ApprovalBook` seam），裁决落定后
不清除。迁入 attention 后，审批只保留"怎么问"（digest 绑定、风险等级、外部
approver 文件入口），"存多久、谁清、怎么读在场"交给原语。

## What Changes

- `ApprovalManager` 的真源切换为 attention 事实：`Request`/`Lookup`/`Pending`/
  `Decide` 全部读写 attention（question kind=approval，lineage 携带
  tool+argsDigest+callID）；`<dir>/approvals/*.json` 保留为**外部 approver 的
  写入口与兼容读面**，不再是真源。
- `features/evolution/governance/policy.go` 的审批门改读 attention 裁决；
  digest 绑定语义不变（answer 的 lineage 与 question 逐字匹配才生效）。
- `runtime/agent/fuse.go` 的 `ApprovalBook` seam 与 `runtime/task/hold.go` 的
  `HoldBook` 归并到 attention：挂起登记 = 提交 question，裁决 = 提交 answer，
  内存表降级为热缓存或删除。
- 三态语义保持：approved 放行、denied 拒绝、过期（读侧判）不作为裁决；无簿
  fail-closed 行为不变。

## 边界与依赖

- **依赖**：01 域的 `Commit`/`Present` 两个方法（接口常数）；approval 既有
  文件面与 digest 计算函数（实现依赖）。
- **被依赖方**：03 域的人侧统一 Pending 面读 attention（本域不改 wechat 显示
  层）；归档后的 mutation registry 若锚在 approval 文件面，需随本域重锚。
- **接口面**：`ApprovalManager` 公共 API 签名不变（调用方零改动），实现内换
  真源；`ApprovalBook` seam 签名不变。
- **与既有能力的重叠裁决（5.9 检索命中）**：主规格 `approval-channels` 为同域能力——本域是**扩展**而非并行：归档时对 `approval-channels` 落 MODIFIED（真源切 attention、裁决落定即清除、文件面降为外部写入口），`approval-attention` 承载新增生命周期验收条款。
- **禁止事项**：不改 `ApprovalChannel.Deliver` 投递协议；不动 wechat/CLI 入口
  文件（归 03）；不引入 attention 之外的第三存储；digest 绑定语义不得放宽。

## Capabilities

见 `specs/approval-attention/spec.md`。

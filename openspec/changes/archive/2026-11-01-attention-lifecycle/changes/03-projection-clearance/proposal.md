# Proposal: 03-projection-clearance

> 父变更：attention-lifecycle（依赖 01-attention-primitive 入 next；与 02 并发，触碰集分侧）。

## Why

原语与审批迁移解决了"怎么存、怎么裁决"，但"谁能看到所有待答、何时清场"还缺
统一面：notice 无 expiry 永挂（症状锚 2 的红）、`deliver_summary` 投出即忘、
人侧 Pending 各自为政。本域给 attention 一个投影读面、写死两类清除，并把三条
路径的人侧入口收到同一 Pending/answer 面上。

## What Changes

- `memory/query` 新增 `ObjectAttention` 维度：在场集（kind/target/lineage/相位
  question→answered|expired|cleared）随事实链推导，进既有 snapshot/Follow 面。
- **notice 挂 expiry（blocked task 同一原语）**：无人消费的通知（含 task 被义务
  阻塞等答案的形态）在读面带 `expires_ms`（读侧判过期，过期即不在场）；
  notice 四元组与 settle 协议不动。
- **工作结束清除**：宿主 `Close` 排空成功后，为全部未决 attention 落
  clearance=work-end 事实；排空失败不清。
- **deliver_summary 接入**：投递落一条 kind=summary 的 attention 在场记录
  （投递路径 `CommitToSession` 不动），目标会话下一轮读到该摘要即视为消费清除。
- **人侧统一入口**：wechat approvalGateway 与 CLI 的 Pending 显示读
  `ObjectAttention`，approve/reject 写 attention answer（经 02 域 manager）。

## 边界与依赖

- **依赖**：01 域 `Commit`/`Present`（接口常数）；`Run.Close` 排空钩子与
  `query.Inventory` 装配点（实现依赖，以启动门时 next 为准）。
- **被依赖方**：一级 F2/F3 的两条症状锚绿态落在本域（锚 1 的清除绿态在 T4，
  锚 2 的绿态在 T3）。
- **接口面**：`ObjectAttention` 进既有 `ObjectKind` 词汇表与 DetailState；wechat
  公共入口（Pending 展示、Resolve）签名不变。
- **与既有能力的重叠裁决（5.9 检索命中）**：主规格 `meditation-delivery-tool` 为同域能力——本域对它是**扩展**而非并行：归档时落 MODIFIED（投递产生 attention 在场记录），投递路径条款不变。notice 相邻能力（async-task-lifetime 等）**明确不重合**：本域不动 notice 四元组与四相读数，attention 在场是新维度，既有相位读数照旧。
- **禁止事项**：不改 notice 四元组、settle 驱动与段语义；不改
  `CommitToSession`/deliver_summary 的投递路径；清除不得删事实（链只增）；
  不得在 02 域文件上落笔（触碰集分侧）。

## Capabilities

见 `specs/attention-clearance/spec.md`。

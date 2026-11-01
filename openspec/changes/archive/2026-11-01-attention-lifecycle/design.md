# Design: attention-lifecycle

## D1 语义决策：attention 是一组冻结的事实族，不是一张表

**采纳**：新增事件类型 `TypeAttentionQuestion` / `TypeAttentionAnswer` /
`TypeAttentionClearance`，配冻结 meta 键：`attention.kind`
（approval|notice|summary，缺省 approval）、`attention.target`
（线程/人身份）、`attention.lineage`（上下文血统：调用 ID/任务 ID/反思来源）、
`attention.expires_ms`（有效期，0=随工作结束清除）、`attention.clearance`
（answer|expiry|work-end）。question 携带问题正文（role=user）；answer 携带
裁决正文与裁决人；clearance 不携带模型可读正文。

**被否**：全复用 `TypeFeedback` + meta 区分（与通知材料/审计反馈同型，投影谓词
需排除法，且"问题正文是模型可见输入"与 feedback 的语义冲突）。
**被否**：单类型 + phase meta（一次要答案的完整周期被压进一条事实，answer 与
question 竞争同键，重启读回无法分侧）。

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | attention-primitive | `attention-primitive` | 事实族、meta 键、落链/读回/清除规则引擎（纯原语，无消费者） | api/event 冻结键登记 |
| 02 | approval-onto-attention | `approval-attention` | 审批全量迁入：governance 门、fuse `ApprovalBook` seam、digest 绑定由 attention 承载 | 01 的提交/读回 API |
| 03 | projection-clearance | `attention-clearance` | `ObjectAttention` 投影、notice expiry 与工作结束清除、summary kind 接入、人侧统一 Pending 面与 answer 写面 | 01 的 API；host Close 排空钩子 |

## D3 波次表（波次仅汇报分组；解锁 = 前置入 next）

| 波 | 内容 | 前置 |
|---|---|---|
| W1 | 01 原语（含两条症状红锚落地：写好、红着、入 tasks.md） | 启动门（见 ORCHESTRATION） |
| W2 | 02 审批迁入 ∥ 03 投影与清除（触碰集不相交，见 D6） | 01 入 next |
| W3 | 编排者接线收尾：消费者锚全量、F1–F4 | W2 全入 next |

## D4 合并规则

- 同文件簇必须合单或串行；`api/event`（meta/types/skeleton_assert）在 W1 由
  01 单写冻结，W2 起只读。
- `examples/wechat-bot` 的人侧改动（Pending 显示与 answer 裁决写面）全部归 03；
  02 不碰 examples/，其消费者锚以 tests/integration 为准。
- `runtime/task/approval` 与 `runtime/agent`（fuse seam）归 02；`memory/query`
  与 `features/evolution/meditation` 与 `assembly`（Close 钩子）归 03。
- git 合并按二级域整域并入（一域一 PR 或一域一 squash），域内不拆分提交。

## D5 三级验收

- 孙任务：exit 0 的可复跑命令（正锚/负锚/消费者锚随任务行冻结）。
- 子域：其 `specs/<capability>/spec.md` 的 scenario 逐条由具名测试覆盖。
- 变更级 DoD：两条症状锚红转绿各两证（完整回滚本变更必红、落地后必绿）；全量
  项目门 exit 0（含事件完整性三档、mutation registry、spec strict、CI 全 job）。

## D6 并发准入（W2）

02 触碰集：`runtime/task/approval/**`、`features/evolution/governance/**`、
`runtime/agent/fuse.go`、`runtime/task/hold.go`、`tests/integration/**`（审批侧）。
03 触碰集：`memory/query/**`、`assembly/host*.go`（Close 排空）、
`features/evolution/meditation/deliver.go`、`examples/wechat-bot/wechat_approval*.go`、
`tests/integration/**`（投影侧）。
交集 = 空（`tests/integration` 按测试文件名分侧：`attention_approval_*` 归 02、
`attention_projection_*` 归 03，派发单里逐文件点名）。

## D7 风险与回退

- **回退面**：02 保留 ApprovalManager 文件面作为只写入口（外部 approver 仍写
  JSON），回退 = gate 直读文件面（并存迁移，回退后与基线零 diff 于行为面）。
- **清除误伤**：clearance 只移除"在场"读数，不删事实（事实链只增）；回滚清除
  规则即恢复在场。工作结束清除必须发生在 Close 排空**成功**之后，失败路径不清。
- **概念预算**：attention 对外接口 ≤2 方法（提交面 + 读回面），遵守
  check_concept_budget 的 S1–S4。

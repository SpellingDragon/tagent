# 导览：attention-lifecycle

> 一次"向目标线程/人要一个答案"的生命周期落为 attention 事实族：审批、通知、摘要
> 三类在场同源推导，清除只写两处（回答后、工作结束排空后）。
> 本导览按域给出**契约落点 → 实施台账 → 证据锚**三段链接。归档件只读，勘误另起文件。

## 总览

| 域 | 契约（主规格） | 域台账 | 症状锚 |
|---|---|---|---|
| 01 attention 原语 | `openspec/specs/attention-primitive/spec.md` | [changes/01-attention-primitive/tasks.md](changes/01-attention-primitive/tasks.md) | 两锚落库当日红（`:85` / `:164`） |
| 02 审批迁移 | `openspec/specs/approval-attention/spec.md` | [changes/02-approval-onto-attention/tasks.md](changes/02-approval-onto-attention/tasks.md) | `TestAttentionClearsApprovalOnAnswer` |
| 03 投影清除 | `openspec/specs/attention-clearance/spec.md` | [changes/03-projection-clearance/tasks.md](changes/03-projection-clearance/tasks.md) | `TestAttentionClearsNoticeOnWorkEnd` + 隔离面 `TestAttentionNoticeQuestionClearsOnWorkEnd` |
| 04 静止收束 | `openspec/specs/async-task-execution/spec.md`、`openspec/specs/persistent-event-loop/spec.md` | [changes/04-quiescent-close/tasks.md](changes/04-quiescent-close/tasks.md) | Q-A–Q-D 四组红转绿 |
| 编排契约 | `openspec/specs/orchestration/spec.md`（启动门／冻结面单写／入 next 解锁／DoD 两证） | 本文件同级 [tasks.md](tasks.md) | — |

逐轮凭据（含 run-id、退出码、提交点）在 [ORCHESTRATION.md](ORCHESTRATION.md) ④ 台账。

## 01 attention 原语

- 契约：`openspec/specs/attention-primitive/spec.md`（4 requirement / 7 scenario）。
- 实施：`runtime/task/attention*.go`、`api/event` 三枚 `TypeAttention*` + 五枚 `MetaKeyAttention*`
  （登记见 `api/event/skeleton_assert.go:103-105` / `:117-121`）。
- 测试（`go test ./runtime/task -run '^TestAttention' -count=1`）：
  `TestAttentionFactRoundTrip`（落链幂等/血统随行）、`TestAttentionAnswerWithoutQuestionRefused`（负锚：孤儿 answer）、
  `TestAttentionClearsOnAnswer`、`TestAttentionSurvivesRestart`、`TestAttentionExpiresOnRead`、
  `TestAttentionRefusalsAreNamed`。
- 台账：`ORCHESTRATION.md` ④「2026-11-01 红锚落库」「W1-01 五查+清偿」（提交点 `c9048cb3`）。

## 02 审批迁移

- 契约：`openspec/specs/approval-attention/spec.md`（4 requirement / 7 scenario）。
- 实施：`runtime/task/approval/**`、`runtime/agent/fuse.go`、`runtime/task/hold.go`。
- 测试：
  - `go test ./runtime/task/approval -run '^TestApprovalManagerReadsAttention$|^TestApprovalDigestChangeStartsNewQuestion$|^TestExternalFileRulingLandsAsAnswer$'`
  - `go test ./tests/integration/host -run '^TestAttentionClearsApprovalOnAnswer$'`（症状锚①）
  - `go test ./tests/integration/host -run '^TestGateReadsAttentionRuling$|^TestGateFailsClosedWithoutAttention$'`（门读面/fail-closed 负锚）
  - `go test ./tests/integration/execution -run '^TestFuseBatchParksOnAttention$'`（fuse seam 消费者锚）
- 台账：`ORCHESTRATION.md` ④「W2-22 五查+两裁」（提交点 `4fc276d2`；两处改锚裁定见 ⑤-①⑤-③）。

## 03 投影清除

- 契约：`openspec/specs/attention-clearance/spec.md`（5 requirement / 8 scenario）。
- 实施：`memory/query/attention.go`（`ObjectAttention`）、`assembly/host_attention.go`、`features/evolution/meditation/deliver.go`。
- 测试：
  - `go test ./tests/integration/inspection -run '^TestAttentionInventoryDimension$'`（三类同面/增量）
  - `go test ./tests/integration/host -run '^TestApprovalAttentionLeavesProjectionOnAnswer$|^TestWorkEndClearsNothingOnFailedDrain$|^TestNoticeExpiryLeavesAttention$|^TestSummaryAttentionClearsWhenRead$'`
  - `go test ./tests/integration/host -run '^TestAttentionClearsNoticeOnWorkEnd$'`（症状锚②，共因清偿口径见 ⑤-⑥）
  - `go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmoke'`（人侧入口消费者锚）
- 台账：`ORCHESTRATION.md` ④「W3-消费者锚全量」（干净克隆 `46408c26`）、「W2-03'」改锚留痕（`d4aa4793`）。

## 04 静止收束

- 契约：`openspec/specs/async-task-execution/spec.md`（未消费通知存活/拒收落事实/静止收束三态）、
  `openspec/specs/persistent-event-loop/spec.md`（循环停止=静止收束）。
- 实施：`runtime/task/unsettled.go`（同源函数 `Unsettled()`）、`assembly/host*.go`（Close 编排）、`runtime/agent/delegate.go`。
- 测试（红转绿四组）：
  - Q-A 拒收即消费：`go test ./tests/integration/host -run '^TestQuiesceRejectedCutAllowed$'`
  - Q-B derived 不自锁：`go test ./tests/integration/host -run '^TestQuiesceDerivedNotSelfLocked$'`
  - Q-C 同源单一判据：`go test ./runtime/task -run '^TestUnsettledSingleSource$'`、`./tests/integration/host -run '^TestQuiesceUnsettledSingleSource$'`
  - Q-D Cut 报告：`go test ./tests/integration/host -run '^TestQuiesceOfflineCutReport$'`
  - 门：`python3 scripts/gates/check_event_integrity_mutations.py --all`（11 变异全 killed）
- 台账：`ORCHESTRATION.md` ④「F3.5 清偿·T1–T7」、⑤-⑩（四条级联红与 mutation anchor 的清偿口径）。

## 归档时的留白（未勾项，按实况保留）

- `F2`（本地四门分项）与 `F2.e`（CI 全 job）：`F2.a–F2.d` 四条本地门绿并逐条记凭据；
  CI 面因「本计划禁 push」从未跑，故父项与 CI 子项不勾（见 ⑤-⑤）。
- `F3.5`（独立只读审者拷问放行）：其清偿批 T1–T7 已落凭据，但「末轮 frontier 空 + 扫描范围」
  的拷问记录未成文入 run notes，故不勾——勾选不得代改事实。

## 复跑要点

```bash
openspec validate --specs --strict            # 141 passed / 0 failed（归档后）
go test ./runtime/task ./runtime/task/approval -count=1
go test ./tests/integration/host -run '^TestAttention|^TestQuiesce' -count=1
go test ./tests/integration/inspection -run '^TestAttentionInventoryDimension$' -count=1
go test ./tests/integration/execution -run '^TestFuseBatchParksOnAttention$' -count=1
```

`tests/integration/host` 的 `TestAttentionClearsNoticeOnWorkEnd` 需与合并线同跑：
其 `ObjectNotice` 相位由合并线的排空消费处置（共因清偿，见 ⑤-⑥），单独回滚本变更时
它不红——隔离面以 `TestAttentionNoticeQuestionClearsOnWorkEnd` 为准。

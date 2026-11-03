# Tasks: 02-approval-onto-attention

> 前置：01 域已入 next。触碰集见一级 design D6；白名单不含 `api/event` 与
> `examples/wechat-bot`。

- [x] T1 审批真源切到 attention —— `Request`/`Lookup`/`Pending`/`Decide` 读写
      attention，公共 API 签名不变，同 (tool,digest) 幂等
      —— 验证（正锚）：`go test ./runtime/task/approval -run '^TestApprovalManagerReadsAttention$' -count=1`
- [x] T2 digest 绑定逐字匹配且改参不复用 —— answer 生效条件 = lineage 逐字相等；
      过期读侧判 —— 验证（负锚）：`go test ./runtime/task/approval -run '^TestApprovalDigestChangeStartsNewQuestion$' -count=1`
- [x] T3 文件面降为外部裁决入口 —— rescan 把文件裁决落为 attention answer 后
      撤下文件；文件不再参与在场判定 —— 验证（正锚）：`go test ./runtime/task/approval -run '^TestExternalFileRulingLandsAsAnswer$' -count=1`
- [x] T4 governance 门读 attention 裁决 —— 门挂起/放行/拒绝的裁决读面切到
      attention，重启后裁决一致 —— 验证（消费者锚）：
      `go test ./tests/integration/host -run '^TestGateReadsAttentionRuling$' -count=1`
- [x] T5 fuse seam 与 HoldBook 归并 —— `Request`/`Lookup`/`Resolve` 落到
      attention，内存表降级为非真源 —— 验证（消费者锚）：
      `go test ./tests/integration/execution -run '^TestFuseBatchParksOnAttention$' -count=1`
- [x] T6 无簿 fail-closed 与 mutation 重锚 —— 提交面不可用时整批拒绝；
      approval 相关 mutation 锚点重锚且 kill 不缩水 —— 验证（负锚）：
      `go test ./tests/integration/host -run '^TestGateFailsClosedWithoutAttention$' -count=1`；
      `python3 scripts/gates/check_event_integrity_mutations.py --all` exit 0

## 执行凭据

T1–T6 均于最终工作树逐条实跑（`-count=1`）：

- T1 `go test ./runtime/task/approval -run '^TestApprovalManagerReadsAttention$' -count=1` → exit 0，`ok ... 0.733s`
- T2 `go test ./runtime/task/approval -run '^TestApprovalDigestChangeStartsNewQuestion$' -count=1` → exit 0，`ok ... 0.176s`
- T3 `go test ./runtime/task/approval -run '^TestExternalFileRulingLandsAsAnswer$' -count=1` → exit 0，`ok ... 0.199s`
- T4 `go test ./tests/integration/host -run '^TestGateReadsAttentionRuling$' -count=1` → exit 0，`ok ... 20.556s`
- T5 `go test ./tests/integration/execution -run '^TestFuseBatchParksOnAttention$' -count=1` → exit 0，`ok ... 0.398s`
- T6 `go test ./tests/integration/host -run '^TestGateFailsClosedWithoutAttention$' -count=1` → exit 0，`ok ... 10.480s`；
  `python3 scripts/gates/check_event_integrity_mutations.py --all` → exit 0，`gate: ok`（无 approval 相关锚点 stale，重锚清单为空）
- 兜底 `go build ./...` exit 0；`go vet ./runtime/... ./features/... ./tests/integration/...` exit 0；`bash scripts/lint.sh` exit 0；白名单 `gofmt -l` 空
- 回归 `go test ./runtime/task/... ./features/... -count=1` exit 0；`go test ./tests/integration/host -run '^TestRiskRules' -count=1` exit 0

症状锚① `TestAttentionClearsApprovalOnAnswer` 只部分转绿：文件残留断言（`attention_lifecycle_test.go:73-80`）已静默通过，`Lookup` 断言行仍红——
该行要求「同一 manager 在 `Decide(approved)` 之后 `Lookup(tool,digest)` 不得报 approved」，与冻结消费者
`tests/integration/capabilities/governance_test.go:224-234`（同一 manager 批准后重试必须经 `policy.go:323` 的 `Lookup` 放行）
在**同一函数、同一状态**上要求相反结论，无实现可同时满足；上报待裁，未改锚①文件，也未勾绿该项。

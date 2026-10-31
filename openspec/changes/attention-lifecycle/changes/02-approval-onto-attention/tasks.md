# Tasks: 02-approval-onto-attention

> 前置：01 域已入 next。触碰集见一级 design D6；白名单不含 `api/event` 与
> `examples/wechat-bot`。

- [ ] T1 审批真源切到 attention —— `Request`/`Lookup`/`Pending`/`Decide` 读写
      attention，公共 API 签名不变，同 (tool,digest) 幂等
      —— 验证（正锚）：`go test ./runtime/task/approval -run '^TestApprovalManagerReadsAttention$' -count=1`
- [ ] T2 digest 绑定逐字匹配且改参不复用 —— answer 生效条件 = lineage 逐字相等；
      过期读侧判 —— 验证（负锚）：`go test ./runtime/task/approval -run '^TestApprovalDigestChangeStartsNewQuestion$' -count=1`
- [ ] T3 文件面降为外部裁决入口 —— rescan 把文件裁决落为 attention answer 后
      撤下文件；文件不再参与在场判定 —— 验证（正锚）：`go test ./runtime/task/approval -run '^TestExternalFileRulingLandsAsAnswer$' -count=1`
- [ ] T4 governance 门读 attention 裁决 —— 门挂起/放行/拒绝的裁决读面切到
      attention，重启后裁决一致 —— 验证（消费者锚）：
      `go test ./tests/integration/host -run '^TestGateReadsAttentionRuling$' -count=1`
- [ ] T5 fuse seam 与 HoldBook 归并 —— `Request`/`Lookup`/`Resolve` 落到
      attention，内存表降级为非真源 —— 验证（消费者锚）：
      `go test ./tests/integration/execution -run '^TestFuseBatchParksOnAttention$' -count=1`
- [ ] T6 无簿 fail-closed 与 mutation 重锚 —— 提交面不可用时整批拒绝；
      approval 相关 mutation 锚点重锚且 kill 不缩水 —— 验证（负锚）：
      `go test ./tests/integration/host -run '^TestGateFailsClosedWithoutAttention$' -count=1`；
      `python3 scripts/gates/check_event_integrity_mutations.py --all` exit 0

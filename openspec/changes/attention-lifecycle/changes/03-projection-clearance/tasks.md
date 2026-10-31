# Tasks: 03-projection-clearance

> 前置：01 域已入 next。触碰集见一级 design D6；白名单不含 `api/event`、
> `runtime/task/approval/**`、`features/evolution/governance/**`。

- [ ] T1 ObjectAttention 投影维度 —— kind/target/lineage/相位进 snapshot 与
      Follow；词汇表与 FACT-VOCABULARY 登记 —— 验证（正锚）：
      `go test ./tests/integration/inspection -run '^TestAttentionInventoryDimension$' -count=1`
- [ ] T2 notice 挂有效期 —— 读面带 `expires_ms`，过期不在场，ObjectNotice 四相
      读数不动 —— 验证（正锚）：
      `go test ./tests/integration/host -run '^TestNoticeExpiryLeavesAttention$' -count=1`
- [ ] T3 工作结束清除 —— Close 排空成功后批量落 work-end（全有或全无），排空
      失败不清，重启保持清空 —— 验证（症状锚 2 绿态）：
      `go test ./tests/integration/host -run '^TestAttentionClearsNoticeOnWorkEnd$' -count=1`；
      （负锚）：`go test ./tests/integration/host -run '^TestWorkEndClearsNothingOnFailedDrain$' -count=1`
- [ ] T4 审批清除与文件撤下 —— answer 落链即清在场并撤下文件残留（与 02 域
      manager 的兼容面联动）—— 验证（症状锚 1 绿态）：
      `go test ./tests/integration/host -run '^TestAttentionClearsApprovalOnAnswer$' -count=1`
- [ ] T5 deliver_summary 按 summary kind 接入 —— 投递路径不动，在场记录随投递
      落，读到即清、未读到随工作结束清 —— 验证（消费者锚）：
      `go test ./tests/integration/host -run '^TestSummaryAttentionClearsWhenRead$' -count=1`
- [ ] T6 人侧统一 Pending/answer 面 —— wechat 与 CLI 的待答显示读
      ObjectAttention，approve/reject 写 attention answer；wechat 审批冒烟回归
      —— 验证（消费者锚）：`go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmoke' -count=1` exit 0

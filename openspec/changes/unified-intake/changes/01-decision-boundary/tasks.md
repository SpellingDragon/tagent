# 孙任务

- [ ] A1 症状锚红测：`TestMessageReachesTheNextDecisionOfAInFlightTurn`（脚本化慢工具轮+turn 中第二条消息→断言同 turn 后续 decision 输入含该消息）—— 验证：`go test ./tests/integration/execution -run '^TestMessageReachesTheNextDecisionOfAInFlightTurn$' -count=1` **规划期红**（负锚：基线轮断言消息不在后续决策）；消费者锚=真 Host.Invoke 公共入口
- [ ] A2 loop 决策边界 ready 增量：ReadyFeed 面+Drive 每 decide 前 observeReady 增量（含去重位点）—— 验证：上锚转绿+`go test ./runtime/agent -count=1`（既有全绿；负锚：ReadyFeed nil 时行为不变）
- [ ] A3 装配注入 ReadyFeed（assembly 实现会话 after-事实读）—— 验证：集成锚同 A1 场景经真装配；消费者锚=assembly.Assemble 公共入口
- [ ] A4 host Invoke 受理语义+gate 降级：在途只落事实即返受理（回执 ok）；无在途起 turn；gate=turn 发起权门 —— 验证：`TestInflightUserMessageIsAcceptedNotQueued`（新：在途 Invoke 返回 ok 且消息事实已落链）+既有 `go test ./tests/integration/host -count=1` 不回归（负锚：ErrSlotHeld/关停拒不变）
- [ ] A5 规格 delta：persistent-event-loop ADDED 外部输入条款（D4 原文）+event-flow.md 锚页随迁+event-integrity 场景锚核对（task_settled 条款不动）—— 验证：`openspec validate unified-intake --strict`+`go test ./tests/integration/history -run 'TestEventIntegrity' -count=1` 不回归

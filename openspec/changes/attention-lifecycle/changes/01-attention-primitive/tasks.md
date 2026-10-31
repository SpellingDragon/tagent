# Tasks: 01-attention-primitive

> 孙任务三要素：产出 + 关键约束 —— 验证：可复跑命令。verify 三锚随行冻结，
> 验收面被追认缩水 = 本叶重开。

- [ ] T1 冻结事件类型与 meta 键并登记契约 —— 三枚 `TypeAttention*` 与五枚
      `MetaKeyAttention*` 进 types/meta/skeleton_assert，FACT-VOCABULARY 加登记行
      —— 验证（正锚）：`go build ./... && go test ./api/event/... -count=1`；
      `bash scripts/lint.sh` exit 0
- [ ] T2 落链与读回两个方法 —— `Commit`/`Present` 走既有事实提交与链读，同身份
      question 幂等 —— 验证（正锚）：`go test ./runtime/task -run '^TestAttentionFactRoundTrip$' -count=1`
- [ ] T3 清除规则引擎（纯函数）—— answer 落链即清、读侧判过期、clearance 三值
      —— 验证（负锚）：`go test ./runtime/task -run '^TestAttentionAnswerWithoutQuestionRefused$' -count=1`；
      （正锚）：`go test ./runtime/task -run '^TestAttentionClearsOnAnswer$' -count=1`
- [ ] T4 重启读回一致 —— 在场集全由事实推导，重建后一致 —— 验证（正锚）：
      `go test ./runtime/task -run '^TestAttentionSurvivesRestart$' -count=1`
- [ ] T5 概念预算自检 —— 对外接口 ≤2 方法，无内部结构导出 —— 验证（消费者锚）：
      `python3 scripts/gates/check_concept_budget.py --json` exit 0 且无新增超标
- [ ] T6 两条症状红锚落库且红着 —— `TestAttentionClearsApprovalOnAnswer` 读现状
      审批面（批准执行后目录与 Lookup 仍有残留即红）、`TestAttentionClearsNoticeOnWorkEnd`
      读现状 notice 投影（Close 后仍 owed 即红）；两锚此后不得改写断言语义
      —— 验证（症状锚）：`go test ./tests/integration/host -run '^TestAttentionClears' -count=1`
      失败，失败输出贴入 ORCHESTRATION 台账

# Tasks: naturalize-meditation-gating

- [ ] 1.1 引擎重构：meditation.go 删 lastTurnEnd/UpdateLastTurnEnd/startedAt，新 pending 状态机 + NoteMeditationBatchOutcome(consumed) + 节奏门（lastMeditation 锚/首次直通）+ 水位执行语义 + 3×interval pending 防御 —— 验证：`go test ./agent -run '^TestMeditation' -count=1` 新用例绿。
- [ ] 1.2 event_loop 接线改造：UpdateLastTurnEnd 回调点替换为冥想批结果通知（消费/让位两态）；dropMeditationFromMixedBatch 通知 deferred；纯冥想批消费通知 consumed —— 验证：同上 + `grep -c UpdateLastTurnEnd` =0。
- [ ] 1.3 锚持久化：persistAnchors/LoadAnchors 收敛为单锚 lastMeditation（旧 LastTurnEnd 键忽略兼容用例钉住）—— 验证：`go test ./agent -run 'Anchor' -count=1`。
- [ ] 2.1 测试矩阵重写：D4 七场景全展开（含冷启动两用例并入、让位后连续再来、pending 超时）；旧 TestMeditationGate_* 28 例清账（删/改清单入 tasks 注记）—— 验证：`go test ./agent -run '^TestMeditation' -count=1 -v` 全 PASS 且 `-count=5` 稳定。
- [ ] 2.2 两形态 e2e：外部（远端 18h 剧本翻转）+ 自察（用户高频让位→走后即思覆盖全程）—— 验证：`go test ./tests -run '^TestExternalMeditation|MeditationE2E' -count=1`。
- [ ] 3.1 文档：wiki §2.14 门控段重写（五不变量+mermaid 状态机）、README 界定句、observability 三态说明 —— 验证：lint=0、旧语义措辞（"空闲门=任意回合"）grep 清零。
- [ ] F1 四查+非触碰：novelty 判据/观察面授权/注入动作 diff=0；远端三场景验收剧本对账。
- [ ] F2 全门禁：short/race(agent 域 ×10)/lint(Go1.24)/check-openspec/bot 模块。
- [ ] F3 真实模型复跑：TestRealModel_ExternalMeditation（门控行为实跑可见面，预算 3 calls 入账）。
- [ ] F4 归档 + 部署知会（min_gap 语义变化写入发布说明与远端通知）。

# Tasks: polish-meditation-runtime

> 只编排说明：0 为前置清账（独立于本 change 域，先清再进）；1–2 两域可并发（文件面不相交：1 在 event_loop/manager 判定路径，2 在 manager 计数与 digest 渲染——**同文件 meditation.go 不同函数**，按 R21 判定需串行或契约冻结，见 D4/D2 契约：deferred 通知签名与计数归属）。

- [ ] 0.1 #17 清账：验证工作树修复（`go test ./agent -race -run '^TestModelOverride_ReentryIsolation$' -count=5` + `Percall|Override count=3` + 全量 `race -count=2`）→ 全绿则 commit+关 #17；任一红则回滚该修复、#17 改派 —— 验证：三连输出附 run 证据。
- [ ] 1.1 总线在场复查选型：按 event_bus 现有 API 定窥视形态（无则按 D6 加非破坏性 API，契约冻结单写）—— 验证：选型注记 + `go build ./agent`。
- [ ] 1.2 执行时刻让位实现：纯冥想批消费前复查→在场则丢弃+deferred 通知 —— 验证：`go test ./agent -run '^TestMeditation|^TestOnEvent' -count=1` 含新用例三态（让位/照常/外部恒执行）。
- [ ] 2.1 欠账计数+digest 渲染：deferredCount/lastDeferredAt + buildMeditationMessage 计数行 —— 验证：`go test ./agent -run 'Digest|Deferred' -count=1`；consumed 清零断言。
- [ ] 3.1 e2e+文档：自察间隙让位 e2e、外部零影响断言、wiki §2.14 补"消费时刻复查"与欠账行、README 界定句 —— 验证：`go test ./tests -run 'Meditation' -count=1`；lint(Go1.24)=0；旧措辞 grep 清零。
- [ ] F1 四查+非触碰：novelty/节奏/pending/水位语义 diff=0；naturalize 用例零改动通过。
- [ ] F2 全门禁：short/race(agent ×2)/lint/check-openspec/bot。
- [ ] F3 真实模型复跑（门控行为面变更）：TestRealModel_ExternalMeditation 预算 3 calls。
- [ ] F4 归档+部署知会（自察形态行为变化说明；外部形态零变化声明）。

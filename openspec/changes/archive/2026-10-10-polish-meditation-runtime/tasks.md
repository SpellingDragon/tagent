# Tasks: polish-meditation-runtime

> 任务 0.1 完成注记（2026-10-10）：跨轮唯一名修复含 4 处残渣一并清（两处 snap.Resolve + 一处标准间距 model_override 字面量——首轮替换漏网致新失败形态，复跑暴露后补全）。三连验证全绿：ReentryIsolation race count=5 ok、Percall|Override count=3 ok、全量 agent race count=2 ok 159s。

> 只编排说明：0 为前置清账（独立于本 change 域，先清再进）；1–2 两域可并发（文件面不相交：1 在 event_loop/manager 判定路径，2 在 manager 计数与 digest 渲染——**同文件 meditation.go 不同函数**，按 R21 判定需串行或契约冻结，见 D4/D2 契约：deferred 通知签名与计数归属）。

- [x] 0.1 #17 清账：验证工作树修复（`go test ./agent -race -run '^TestModelOverride_ReentryIsolation$' -count=5` + `Percall|Override count=3` + 全量 `race -count=2`）→ 全绿则 commit+关 #17；任一红则回滚该修复、#17 改派 —— 验证：三连输出附 run 证据。
- [x] 1.1 总线在场复查选型：按 event_bus 现有 API 定窥视形态（无则按 D6 加非破坏性 API，契约冻结单写）—— 验证：选型注记 + `go build ./agent`。完成注记（2026-10-10）：现有 Pull/TryPull 破坏性、DurablePending 含本批自持 claim（会把"自己"数进在场→恒让位），均不可用→按 D6 新增 `EventBus.PendingCount()`（volatile 数事件本体、durable 数 wake 哨兵，O(1) 只读通道）；build 全绿；新导出面已跑 gen_godoc。
- [x] 1.2 执行时刻让位实现：纯冥想批消费前复查→在场则丢弃+deferred 通知 —— 验证：`go test ./agent -run '^TestMeditation|^TestOnEvent' -count=1` 含新用例三态（让位/照常/外部恒执行）。完成注记（2026-10-10）：processTurn 在 batchCarriesMeditation 后、submit/RunFlow 前复查 PendingCount>0 → yieldMeditationOnPresence（selected 空集走 prepare 门预留 receipt key 后全批 skipped 结算：不 RunFlow、不消费、水位不动、deferred 计数；真实事件留队）；三态包测 TestOnEventLoop_ConsumptionTimeRecheckYieldsToArrivals 全绿（-count=5 稳）。
- [x] 2.1 欠账计数+digest 渲染：deferredCount/lastDeferredAt + buildMeditationMessage 计数行 —— 验证：`go test ./agent -run 'Digest|Deferred' -count=1`；consumed 清零断言。完成注记（2026-10-10）：两 deferred 来源同汇 NoteMeditationBatchOutcome(false) 计数，consumed 清零；欠账行「- 自上次执行以来让位 N 次（最近 …）」样式对齐冒号句，n=0 不渲染（既有卡片零扰动）；TestMeditationDigest_DeferredDebtLine 全绿。
- [x] 3.1 e2e+文档：自察间隙让位 e2e、外部零影响断言、wiki §2.14 补"消费时刻复查"与欠账行、README 界定句 —— 验证：`go test ./tests -run 'Meditation' -count=1`；lint(Go1.24)=0；旧措辞 grep 清零。**测试半边已完成（2026-10-10）**：TestMeditationE2E_GapYieldCarriesDebtLine + TestMeditationE2E_CuratorFormUnbowedByBusinessLoad merge 入 tests/meditation_e2e_test.go，`go test ./tests -run Meditation -count=1` ok、`-count=3` 稳；文档半边已完成（2026-10-10）：wiki §2.14 让位扩为两个时刻（注入=混合批丢弃；消费=纯冥想批启动前 `PendingCount` 在场复查，结构性判据零时间参数）＋形态边界条（回合原子性残余／外部总线隔离恒无在场）＋mermaid 补复查边＋观测与 digest 欠账行同步；README 双语特性句镜像（240/240 行保持）。
- [x] F1 四查+非触碰：novelty/节奏/pending/水位语义 diff=0；naturalize 用例零改动通过。
- [x] F2 全门禁：short/race(agent ×2)/lint/check-openspec/bot。
- [x] F3 真实模型复跑（门控行为面变更）：TestRealModel_ExternalMeditation 预算 3 calls。
- [x] F4 归档+部署知会（自察形态行为变化说明；外部形态零变化声明）。

> 编排者核销（2026-10-10）：四查抽验全复跑（agent 让位面 race ok 6.3s、tests Meditation race ok 4.9s、F1 novelty/节奏判定行 diff grep=0）；全量 agent race count=2 ok 172.6s（#17 修复后该门保持绿）；真实模型 TestRealModel_ExternalMeditation PASS（外部形态反误伤实证——业务密集期策展照常）；SHORT=0、lint(Go1.24)=0、check-openspec 118 项全绿。引擎波四项自行决策核准：PendingCount 排除 claimed 态（防恒让位死循环）、让位信封走 prepare 门与注入时刻终态同构、包测/e2e 分层收窄 TOCTOU 取证面、percall 注释布局归位系 #17 遗留债清偿。

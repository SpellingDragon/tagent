# 孙任务

- [ ] B1 症状锚红测：`TestMeditationYieldsBesideAnActiveSession`（观察面最新用户事件所属 session 在 min_gap 内→不 fire；任一被测 session 决策席占用→不 fire；自管回合注入→不重置冷却照常 fire；**未盖章用户输入（无 trigger_source）仍算活跃**——P1-9；抑制后无新提交→不 fire 且水位不动——P1-6 上界）—— 验证：`go test ./tests/integration/evolution -run '^TestMeditationYieldsBesideAnActiveSession$' -count=1` **规划期红**（负锚：自管回合用例绿=家务不阻塞保留；时钟=driver 注入的 fake clock，禁真实 sleep 断言——R5）；消费者锚=Driver 公共构造+真 pipeline 观察面
- [ ] B2 driver 活跃闸：lastUserEventAt 追踪（归一化判据）+SlotQuery 在途判据（deferred 记账+水位不动+ObserveCommit 重评；无 ticker 上界如实）+`NewDriver` 时钟 seam（`now func() time.Time`，P1-8）—— 验证：上锚转绿+`go test ./features/evolution/... -count=1` 既有全绿（负锚：闸零值行为=基线）
- [ ] B3 规格 delta：meditation-idle-gating MODIFIED 节奏门（保留时间门槛禁令，加两侧判据）+evolution 锚页补水门—— 验证：`openspec validate unified-intake --strict`+既有 meditation 场景锚不回归

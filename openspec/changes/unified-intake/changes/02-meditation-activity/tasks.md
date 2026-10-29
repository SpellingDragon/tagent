# 孙任务

- [ ] B1 症状锚红测：`TestMeditationYieldsBesideAnActiveSession`（同 session 用户事件注入后 min_gap 窗口内→不 fire；决策席占用→不 fire；自管回合注入→不重置冷却照常 fire）—— 验证：`go test ./tests/integration/evolution -run '^TestMeditationYieldsBesideAnActiveSession$' -count=1` **规划期红**（负锚：自管回合用例绿=家务不阻塞保留；时钟=driver 注入的 fake clock，禁真实 sleep 断言——R5）；消费者锚=Driver 公共构造+真 pipeline 观察面
- [ ] B2 driver 活跃闸：lastUserEventAt 追踪（source==user）+SlotQuery 在途判据（推迟非放弃：水位不动 pending 沿用）—— 验证：上锚转绿+`go test ./features/evolution/... -count=1` 既有全绿（负锚：闸零值行为=基线）
- [ ] B3 规格 delta：meditation-idle-gating MODIFIED 节奏门（保留时间门槛禁令，加两侧判据）+evolution 锚页补水门—— 验证：`openspec validate unified-intake --strict`+既有 meditation 场景锚不回归

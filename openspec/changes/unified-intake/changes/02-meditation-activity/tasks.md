# 孙任务

- [ ] B1 症状锚红测：`TestMeditationYieldsBesideAnActiveSession`（最近非自管新鲜事实距 now 不足 min_gap→不 fire；观察面事实所属地址的既有 Occupancy 决策席占用→不 fire；自管回合注入→不重置冷却照常 fire；**未盖章用户输入（hostFacingStamp 空且 owner=宿主地址）仍算活跃**——P2-4；**抑制→既有 interval 节拍到期重评→（若新鲜度已过 min_gap）fire**（P0-1 正锚）；**冥想自身注入（trigger_source==meditation）恒不计入活跃**）—— 验证：`go test ./tests/integration/evolution -run '^TestMeditationYieldsBesideAnActiveSession$' -count=1` **规划期红**（负锚：自管回合用例绿=家务不阻塞保留；时钟=driver 注入的 fake clock，禁真实 sleep 断言——R5）；消费者锚=Driver 公共构造+真 pipeline 观察面
- [ ] B2 driver 活跃闸：lastUserEventAt 追踪（归一化判据）+SlotQuery 在途判据（deferred 记账+水位不动+ObserveCommit 重评；无 ticker 上界如实）+**不造新入口**：B1 测试沿用既有测试设施构造 Driver 并驱动既有 pass 路径（禁真实 sleep；若既有设施不足，在既有构造加可选参数，不新造类型）+抑制期 `pending` 保持为空（沿用既有 retained 语义，不建独立计数器）—— 验证：上锚转绿+`go test ./features/evolution/... -count=1` 既有全绿（负锚：闸零值行为=基线）
- [ ] B3 规格 delta：meditation-idle-gating MODIFIED 节奏门（保留时间门槛禁令，加两侧判据）+evolution 锚页补水门—— 验证：`openspec validate unified-intake --strict`+既有 meditation 场景锚不回归

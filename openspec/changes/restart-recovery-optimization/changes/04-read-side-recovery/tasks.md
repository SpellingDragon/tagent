# 04-read-side-recovery 孙任务

- [ ] 4.1 观察表与 `landedSeq` 持久化 + 增量装载 —— 验证：正锚 `TestBootConsumesOnlyTheDelta`（历史 200 + 增量 5 ⇒ visit=5）+ 一致性锚 `TestObservationStateEqualsFullRebuild`（逐字段 DeepEqual）+ 负锚（删快照/坏 `landedSeq` ⇒ 退全量且等价）
- [ ] 4.2 回放投影与压缩产物持久化 + 增量装载 —— 验证：`TestReplayStateEqualsFullRebuild` + 负锚（投影缺失 ⇒ 退全量）
- [ ] 4.3 完成点扩面：唯一快照含恢复状态+观察表+回放投影+`landedSeq` —— 验证：`TestCompletionPointCarriesStateOnce`（单份、原子序、版本拒收）+ 崩溃窗口退回
- [ ] 4.4 向量缝（取向待裁定） —— 验证：`TestSearchTopKEqualsFullRebuild`（补齐完成后 Top-K 逐条一致）+ 补齐进度可观测
- [ ] 4.5 消费者锚 —— 验证：本地 120 重启 boot_ms 在 gen1/20/120 同量级（记录数字）+ CI long 分片复跑

## verify 三锚（随任务行冻结，R29）

正锚：只消费增量（visit 计数 = 增量）+ 增量 ≡ 全量重建（逐面 DeepEqual）。负锚：状态缺失/`landedSeq` 不可信/版本不符 ⇒ 退全量且逐面等价。消费者锚：真实 120 重启 boot_ms 与历史长度无关。

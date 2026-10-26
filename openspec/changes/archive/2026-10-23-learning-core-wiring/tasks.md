# Tasks：learning-core-wiring（两单并发）

- [x] W1 核心单：model.replacement 事件类型＋config 来源（指纹变化产生事实）＋lease PostCommitObserver 消费＋loop 借版本＋Capture 三点接线＋attempts.jsonl —— 验证：go test ./... -count=1 绿＋TestReplacementFactInstallsNewVersion＋TestCaptureSealsAttemptsFile＋TestFailedReplacementKeepsOldVersion（负锚）
- [x] W2 导出单：rl.ChainExporter（链→SFT 样本）＋S10 场景重写（episode split＋capture＋replacement 全真实）＋golden 同步 —— 验证：go test ./... -count=1 绿＋TestChainExporterProducesSamples＋S10 锚全存在＋golden match
- [x] F1-F3 收尾：37包绿＋golden match（S10 五观测全真实）＋contract ok＋lint ok：全量门禁＋lint ok

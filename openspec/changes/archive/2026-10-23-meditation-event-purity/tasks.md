# Tasks：meditation-event-purity（协议冻结——两单并发）

- [x] M1+M2 核心单：冥想归核（Driver 入 meditation 包，PostCommitObserver 事件驱动计时）＋删 DeliverFunc 链＋deliver_summary 工具＋EventSink 落链口＋host.go 瘦身 —— 验证：go test ./... -count=1 全绿＋负锚 TestMeditationDeliversNothingWithoutModelCall＋正锚 TestDeliverSummaryLandsOnTargetChain＋防循环锚 TestSummaryStampNoNovelty
- [x] M3 wechat 单：删 DeliverReflection＋tagent.yaml 工具面换 deliver_summary＋meditator 提示词更新＋测试适配 —— 验证：cd examples/wechat-bot && go test ./... -count=1 绿＋grep -c DeliverReflection wechat.go =0
- [x] F1-F3 收尾：38包绿＋wechat绿＋golden match＋lint ok

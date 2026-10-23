# Tasks：event-turning（两单并发）

- [x] T1 核心单：SettleDriver（PostCommitObserver）＋Host 随动回合（fireNoticeTurn：空输入 headless Invoke＋deliverFresh）＋Boot 接线 —— 验证：TestSettledNoticeDrivesFollowUpTurn（生产路径：委派 settle→随动回合→sender 收到结果）＋负锚 TestSettleDriverIgnoresNonTerminalFacts
- [x] T2 旅程单：tests/integration/journey 套件 J1-J6（脚本 harness＋记录 sender，完整进程内旅程） —— 验证：go test ./tests/integration/journey/... -count=1 全绿（J2/J6 在 T1 落地前红＝拦截证据，落地后绿）
- [x] F1-F3 收尾：37 包＋wechat 测试＋golden match＋lint ok＋实机复验摘要主动送达

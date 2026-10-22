# Tasks：event-typing（两单并发）

- [x] T1 typing 单：折叠产物去前缀编码（Type 判定）＋fireNoticeTurn 机械 Meta＋MetaKeyEventSummary 机械化＋生产链相关测试适配 —— 验证：go test ./... 全绿＋TestRefoldReadsTypeNotPrefix（负锚：Content 无前缀仍正确再折叠）＋grep 生产链零 "history summary: " 剥离
- [ ] T2 冒烟二波单：九机制场景进 smoke 台架（扩展 harness＋新轮次类型）＋错误路径 —— 验证：go test -tags smoke 全绿（12+9 矩阵项）
- [ ] F1-F3 收尾：全量门禁＋golden match＋lint ok
